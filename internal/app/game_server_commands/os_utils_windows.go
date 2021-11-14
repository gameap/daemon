//go:build windows
// +build windows

package game_server_commands

func ChownR(path string, uid, gid int) error {
	return nil
}

func isRoot() bool {
	return false
}
