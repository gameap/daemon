package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	grpcclient "github.com/gameap/daemon/internal/app/grpc"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func enrollAction(c *cli.Context) error {
	connectURL := c.String("connect")
	configPath := c.String("config-path")
	certsDir := c.String("certs-dir")
	listenIP := c.String("listen-ip")
	listenPort := c.Int("listen-port")
	workPath := c.String("work-path")

	urlInfo, err := grpcclient.ParseConnectURL(connectURL)
	if err != nil {
		return errors.Wrap(err, "invalid connect URL")
	}

	host := listenIP
	if host == "0.0.0.0" || host == "" {
		detected, err := detectOutboundIP(urlInfo.Host)
		if err != nil {
			return errors.Wrap(err, "failed to detect outbound IP, specify --listen-ip explicitly")
		}
		host = detected
		log.Infof("Detected outbound IP: %s", host)
	}

	log.Infof("Enrolling with panel at %s", urlInfo.Address)

	ctx, cancel := context.WithTimeout(c.Context, 30*time.Second)
	defer cancel()

	tlsCfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	creds := credentials.NewTLS(tlsCfg)

	conn, err := grpc.NewClient(
		urlInfo.Address,
		grpc.WithTransportCredentials(creds),
	)
	if err != nil {
		return errors.Wrap(err, "failed to create gRPC connection")
	}
	defer conn.Close()

	result, err := grpcclient.Enroll(ctx, conn, urlInfo.SetupKey, host, int32(listenPort))
	if err != nil {
		return errors.Wrap(err, "enrollment failed")
	}

	if result.RootCertificate == "" || result.ServerCertificate == "" || result.ServerPrivateKey == "" {
		return errors.New("panel returned empty certificates")
	}

	if err := os.MkdirAll(certsDir, 0700); err != nil {
		return errors.Wrap(err, "failed to create certs directory")
	}

	certFiles := []struct {
		name    string
		content string
	}{
		{"ca.crt", result.RootCertificate},
		{"server.crt", result.ServerCertificate},
		{"server.key", result.ServerPrivateKey},
	}

	for _, cf := range certFiles {
		path := filepath.Join(certsDir, cf.name)
		if err := os.WriteFile(path, []byte(cf.content), 0600); err != nil {
			return errors.Wrapf(err, "failed to write %s", cf.name)
		}
	}

	enrollCfg := &config.EnrollConfig{
		NodeID:               uint(result.NodeID),
		APIKey:               result.APIKey,
		ListenIP:             listenIP,
		ListenPort:           listenPort,
		CACertificateFile:    filepath.Join(certsDir, "ca.crt"),
		CertificateChainFile: filepath.Join(certsDir, "server.crt"),
		PrivateKeyFile:       filepath.Join(certsDir, "server.key"),
		WorkPath:             workPath,
		LogLevel:             "info",
		GRPC: config.EnrollGRPC{
			Enabled: true,
			Address: urlInfo.Address,
		},
	}

	if err := config.WriteEnrollConfig(configPath, enrollCfg); err != nil {
		return errors.Wrap(err, "failed to write config")
	}

	fmt.Printf("Enrollment successful. Node ID: %d\n", result.NodeID)

	return nil
}

func detectOutboundIP(panelHost string) (string, error) {
	conn, err := net.DialTimeout("udp", net.JoinHostPort(panelHost, "80"), 5*time.Second)
	if err != nil {
		return "", errors.Wrap(err, "failed to detect outbound IP")
	}
	defer conn.Close()

	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", errors.New("unexpected local address type")
	}

	return localAddr.IP.String(), nil
}
