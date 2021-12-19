//go:build windows
// +build windows

package config

const (
	configPath = "C:\\gameap\\daemon.cfg"

	caCertificateFilePath    = "C:\\gameap\\certs\\ca.crt"
	certificateChainFilePath = "C:\\gameap\\certs\\server.crt"
	privateKeyFilePath       = "C:\\gameap\\certs\\server.key"
	dhFilePathPath           = "C:\\gameap\\certs\\dh2048.pem"
)
