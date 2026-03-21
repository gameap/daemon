package grpc

import (
	"context"
	"runtime"

	"github.com/gameap/daemon/internal/app/build"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
)

type EnrollResult struct {
	NodeID            uint64
	APIKey            string
	RootCertificate   string
	ServerCertificate string
	ServerPrivateKey  string
}

func Enroll(ctx context.Context, conn *grpc.ClientConn, setupKey, host string, port int32) (*EnrollResult, error) {
	client := pb.NewDaemonGatewayClient(conn)

	resp, err := client.Enroll(ctx, &pb.EnrollRequest{
		SetupKey:     setupKey,
		Host:         host,
		Port:         port,
		Os:           runtime.GOOS,
		Version:      build.Version,
		Capabilities: []string{"grpc", "file_transfer", "server_status"},
	})
	if err != nil {
		return nil, errors.Wrap(err, "enroll RPC failed")
	}

	if !resp.Success {
		return nil, errors.Errorf("enrollment failed: %s", resp.ErrorMessage)
	}

	return &EnrollResult{
		NodeID:            resp.NodeId,
		APIKey:            resp.ApiKey,
		RootCertificate:   resp.RootCertificate,
		ServerCertificate: resp.ServerCertificate,
		ServerPrivateKey:  resp.ServerPrivateKey,
	}, nil
}
