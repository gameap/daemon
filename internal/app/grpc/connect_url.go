package grpc

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

var (
	errConnectURLScheme        = errors.New("connect URL must start with \"grpc://\"")
	errConnectURLMissingKey    = errors.New("connect URL missing setup key: expected grpc://host:port/key")
	errConnectURLEmptyKey      = errors.New("connect URL has empty setup key")
	errConnectURLExtraSegments = errors.New("connect URL has unexpected path segments")
	errConnectURLEmptyHost     = errors.New("connect URL has empty host")
	errConnectURLInvalidPort   = errors.New("connect URL has invalid port")
)

type ConnectURLInfo struct {
	Host     string
	Port     int
	Address  string
	SetupKey string
}

func ParseConnectURL(rawURL string) (*ConnectURLInfo, error) {
	const scheme = "grpc://"

	if !strings.HasPrefix(rawURL, scheme) {
		return nil, errConnectURLScheme
	}

	rest := strings.TrimPrefix(rawURL, scheme)

	// Split host:port from path (setup key)
	slashIdx := strings.Index(rest, "/")
	if slashIdx < 0 {
		return nil, errConnectURLMissingKey
	}

	authority := rest[:slashIdx]
	setupKey := rest[slashIdx+1:]

	if setupKey == "" {
		return nil, errConnectURLEmptyKey
	}

	// Reject unexpected path segments
	if strings.Contains(setupKey, "/") {
		return nil, errConnectURLExtraSegments
	}

	host, portStr, err := net.SplitHostPort(authority)
	if err != nil {
		return nil, fmt.Errorf("invalid host:port in connect URL: %w", err)
	}

	if host == "" {
		return nil, errConnectURLEmptyHost
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("%w: %s", errConnectURLInvalidPort, portStr)
	}

	return &ConnectURLInfo{
		Host:     host,
		Port:     port,
		Address:  authority,
		SetupKey: setupKey,
	}, nil
}
