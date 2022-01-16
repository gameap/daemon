//go:build linux
// +build linux

package config

const (
	configPath = "/etc/gameap-daemon/gameap-daemon.cfg"

	caCertificateFilePath    = "/etc/gameap-daemon/certs/ca.crt"
	certificateChainFilePath = "/etc/gameap-daemon/certs/server.crt"
	privateKeyFilePath       = "/etc/gameap-daemon/certs/server.key"
	dhFilePathPath           = "/etc/gameap-daemon/certs/dh2048.pem"
)
