package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConnectURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    *ConnectURLInfo
		wantErr string
	}{
		{
			name: "valid URL",
			url:  "grpc://panel.example.com:31718/AbCdEfGh1234567890",
			want: &ConnectURLInfo{
				Host:     "panel.example.com",
				Port:     31718,
				Address:  "panel.example.com:31718",
				SetupKey: "AbCdEfGh1234567890",
			},
		},
		{
			name: "localhost",
			url:  "grpc://localhost:31718/testkey123",
			want: &ConnectURLInfo{
				Host:     "localhost",
				Port:     31718,
				Address:  "localhost:31718",
				SetupKey: "testkey123",
			},
		},
		{
			name: "IP address",
			url:  "grpc://192.168.1.100:9000/key",
			want: &ConnectURLInfo{
				Host:     "192.168.1.100",
				Port:     9000,
				Address:  "192.168.1.100:9000",
				SetupKey: "key",
			},
		},
		{
			name: "IPv6 address",
			url:  "grpc://[::1]:31718/setupkey",
			want: &ConnectURLInfo{
				Host:     "::1",
				Port:     31718,
				Address:  "[::1]:31718",
				SetupKey: "setupkey",
			},
		},
		{
			name:    "missing scheme",
			url:     "http://panel.example.com:31718/key",
			wantErr: "must start with",
		},
		{
			name:    "no scheme",
			url:     "panel.example.com:31718/key",
			wantErr: "must start with",
		},
		{
			name:    "missing key",
			url:     "grpc://panel.example.com:31718",
			wantErr: "missing setup key",
		},
		{
			name:    "empty key",
			url:     "grpc://panel.example.com:31718/",
			wantErr: "empty setup key",
		},
		{
			name:    "missing port",
			url:     "grpc://panel.example.com/key",
			wantErr: "invalid host:port",
		},
		{
			name:    "empty host",
			url:     "grpc://:31718/key",
			wantErr: "empty host",
		},
		{
			name:    "extra path segments",
			url:     "grpc://panel.example.com:31718/key/extra",
			wantErr: "unexpected path segments",
		},
		{
			name:    "invalid port",
			url:     "grpc://panel.example.com:abc/key",
			wantErr: "invalid port",
		},
		{
			name:    "port zero",
			url:     "grpc://panel.example.com:0/key",
			wantErr: "invalid port",
		},
		{
			name:    "port too large",
			url:     "grpc://panel.example.com:99999/key",
			wantErr: "invalid port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseConnectURL(tt.url)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
