//go:build windows
// +build windows

package gameservercommands

func ChownR(path string, uid, gid int) error {
	return nil
}

func isRoot() bool {
	return false
}
