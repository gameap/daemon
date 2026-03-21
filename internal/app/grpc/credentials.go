package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"os"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/pkg/errors"
	"google.golang.org/grpc/credentials"
)

func NewTLSCredentials(cfg *config.Config) (credentials.TransportCredentials, error) {
	caCert, err := os.ReadFile(cfg.CACertificateFile)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read CA certificate")
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, errors.New("failed to add CA certificate to pool")
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertificateChainFile, cfg.PrivateKeyFile)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load client certificate")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		MinVersion:   tls.VersionTLS12,
	}

	return credentials.NewTLS(tlsConfig), nil
}
