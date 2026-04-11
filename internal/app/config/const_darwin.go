//go:build darwin

package config

func detectDefaultProcessManager() string {
	return "tmux"
}
