//go:build linux || darwin
// +build linux darwin

package config

const (
	configPath = "/etc/gameap-daemon/daemon.cfg"

	caCertificateFilePath    = "/etc/gameap-daemon/certs/ca.crt"
	certificateChainFilePath = "/etc/gameap-daemon/certs/server.crt"
	privateKeyFilePath       = "/etc/gameap-daemon/certs/server.key"
	dhFilePathPath           = "/etc/gameap-daemon/certs/dh2048.pem"
)
