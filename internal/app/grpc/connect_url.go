package grpc

import (
	"fmt"
	"net"
	"strconv"
	"strings"
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
		return nil, fmt.Errorf("connect URL must start with %q", scheme)
	}

	rest := strings.TrimPrefix(rawURL, scheme)

	// Split host:port from path (setup key)
	slashIdx := strings.Index(rest, "/")
	if slashIdx < 0 {
		return nil, fmt.Errorf("connect URL missing setup key: expected grpc://host:port/key")
	}

	authority := rest[:slashIdx]
	setupKey := rest[slashIdx+1:]

	if setupKey == "" {
		return nil, fmt.Errorf("connect URL has empty setup key")
	}

	// Reject unexpected path segments
	if strings.Contains(setupKey, "/") {
		return nil, fmt.Errorf("connect URL has unexpected path segments")
	}

	host, portStr, err := net.SplitHostPort(authority)
	if err != nil {
		return nil, fmt.Errorf("invalid host:port in connect URL: %w", err)
	}

	if host == "" {
		return nil, fmt.Errorf("connect URL has empty host")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("connect URL has invalid port: %s", portStr)
	}

	return &ConnectURLInfo{
		Host:     host,
		Port:     port,
		Address:  authority,
		SetupKey: setupKey,
	}, nil
}
