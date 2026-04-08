package grpc

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"time"

	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
)

const (
	defaultProxyTimeout    = 30 * time.Second
	defaultMaxResponseBody = 2 * 1024 * 1024 // 2MB
)

type GRPCHTTPProxyHandler struct {
	maxResponseBody int64
}

func NewGRPCHTTPProxyHandler() *GRPCHTTPProxyHandler {
	return &GRPCHTTPProxyHandler{
		maxResponseBody: defaultMaxResponseBody,
	}
}

func (h *GRPCHTTPProxyHandler) HandleHTTPProxy(
	ctx context.Context,
	requestID string,
	req *pb.HTTPProxyRequest,
) (*pb.HTTPProxyResponse, error) {
	timeout := defaultProxyTimeout
	if req.Timeout != nil {
		timeout = req.Timeout.AsDuration()
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: h.buildTransport(req.UnixSocket),
	}
	if !req.FollowRedirects {
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	var bodyReader io.Reader
	if len(req.Body) > 0 {
		bodyReader = bytes.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.Url, bodyReader)
	if err != nil {
		return &pb.HTTPProxyResponse{
			RequestId: requestID,
			Error:     errors.Wrap(err, "build http request").Error(),
		}, nil
	}

	for _, header := range req.Headers {
		httpReq.Header.Add(header.Name, header.Value)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return &pb.HTTPProxyResponse{
			RequestId: requestID,
			Error:     errors.Wrap(err, "execute http request").Error(),
		}, nil
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, h.maxResponseBody))
	if err != nil {
		return &pb.HTTPProxyResponse{
			RequestId: requestID,
			Error:     errors.Wrap(err, "read response body").Error(),
		}, nil
	}

	headers := make([]*pb.HeaderEntry, 0, len(resp.Header))
	for name, values := range resp.Header {
		for _, value := range values {
			headers = append(headers, &pb.HeaderEntry{
				Name:  name,
				Value: value,
			})
		}
	}

	return &pb.HTTPProxyResponse{
		RequestId:  requestID,
		Success:    true,
		StatusCode: int32(resp.StatusCode),
		Headers:    headers,
		Body:       body,
	}, nil
}

func (h *GRPCHTTPProxyHandler) buildTransport(unixSocket string) *http.Transport {
	if unixSocket != "" {
		return &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", unixSocket)
			},
		}
	}

	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, errors.Wrap(err, "invalid address")
			}

			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, errors.Wrap(err, "dns lookup")
			}

			for _, ip := range ips {
				if isPrivateIP(ip.IP) {
					var d net.Dialer
					return d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				}
			}

			return nil, errors.Errorf(
				"address %s resolves to non-private IP, blocked by SSRF protection",
				host,
			)
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
}

func isPrivateIP(ip net.IP) bool {
	privateRanges := []net.IPNet{
		{IP: net.IPv4(127, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
		{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
		{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},
		{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},
		{IP: net.IPv4(169, 254, 0, 0), Mask: net.CIDRMask(16, 32)},
	}

	if ip.IsLoopback() {
		return true
	}

	for _, cidr := range privateRanges {
		if cidr.Contains(ip) {
			return true
		}
	}

	return false
}
