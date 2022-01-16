//go:build windows
// +build windows

package config

var cfgPaths = []string{
	"./gameap-daemon.cfg",
	"./gameap-daemon.yml",
	"./gameap-daemon.yaml",
	"C:/gameap/gameap-daemon.cfg",
	"C:/gameap/gameap-daemon.yaml",
	"C:/gameap/gameap-daemon.yml",
	"C:/gameap/daemon/gameap-daemon.yml",
	"C:/gameap/daemon/gameap-daemon.yaml",
	"C:/gameap/daemon/daemon.cfg",
	"C:/gameap/daemon/daemon.yml",
	"C:/gameap/daemon/daemon.yaml",
	"C:/gameap/daemon.cfg",
	"C:/gameap/daemon.yml",
	"C:/gameap/daemon.yaml",
}
