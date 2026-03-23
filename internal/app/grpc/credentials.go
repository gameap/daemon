package grpc

import (
	"crypto/tls"
	"crypto/x509"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/pkg/errors"
	"google.golang.org/grpc/credentials"
)

func NewTLSCredentials(cfg *config.Config) (credentials.TransportCredentials, error) {
	caCert, err := cfg.CACertificatePEM()
	if err != nil {
		return nil, errors.Wrap(err, "failed to read CA certificate")
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, errors.New("failed to add CA certificate to pool")
	}

	certPEM, err := cfg.CertificateChainPEM()
	if err != nil {
		return nil, errors.Wrap(err, "failed to read certificate chain")
	}

	keyPEM, err := cfg.PrivateKeyPEM()
	if err != nil {
		return nil, errors.Wrap(err, "failed to read private key")
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
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
