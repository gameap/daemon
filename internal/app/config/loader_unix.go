//go:build !windows && !plan9
// +build !windows,!plan9

package config

var cfgPaths = []string{
	"./gameap-daemon.cfg",
	"./gameap-daemon.yml",
	"./gameap-daemon.yaml",
	"/etc/gameap-daemon/gameap-daemon.cfg",
	"/etc/gameap-daemon/gameap-daemon.yml",
	"/etc/gameap-daemon/gameap-daemon.yaml",
	"/etc/gameap-daemon/daemon.cfg",
	"/etc/gameap-daemon/daemon.yml",
	"/etc/gameap-daemon/daemon.yaml",
	"/etc/gameap/daemon.cfg",
	"/etc/gameap/daemon.yaml",
	"/etc/gameap/daemon.yml",
	"/etc/gameap/gameap-daemon.cfg",
	"/etc/gameap/gameap-daemon.yaml",
	"/etc/gameap/gameap-daemon.yml",
	"/etc/gameap-daemon.cfg",
	"/etc/gameap-daemon.yml",
	"/etc/gameap-daemon.yaml",
}
