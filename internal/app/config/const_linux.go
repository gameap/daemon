//go:build linux

package config

import (
	"os"
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"
)

func detectDefaultProcessManager() string {
	if isInsideDocker() {
		log.Info("Docker container detected, skipping systemd detection")
		return detectFallbackProcessManager()
	}

	if isSystemdAvailable() {
		log.Info("Auto-detected process manager: systemd")
		return "systemd"
	}

	log.Info("systemd is not available, falling back")
	return detectFallbackProcessManager()
}

func isInsideDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	content := string(data)

	return strings.Contains(content, "docker") || strings.Contains(content, "containerd")
}

func isSystemdAvailable() bool {
	_, err := exec.LookPath("systemctl")
	if err != nil {
		return false
	}

	data, err := os.ReadFile("/proc/1/comm")
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(data)) == "systemd"
}

func detectFallbackProcessManager() string {
	if _, err := exec.LookPath("tmux"); err == nil {
		log.Info("Auto-detected process manager: tmux")
		return "tmux"
	}

	log.Warn("Neither systemd nor tmux found, falling back to simple process manager")

	return "simple"
}
