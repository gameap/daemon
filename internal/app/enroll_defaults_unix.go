//go:build linux || darwin

package app

const (
	defaultEnrollConfigPath = "/etc/gameap-daemon/gameap-daemon.yaml"
	defaultEnrollCertsDir   = "/etc/gameap-daemon/certs"
	defaultEnrollWorkPath   = "/srv/gameap"
)
