//go:build windows

package fsutil

import "os"

func renameAcross(srcRoot *os.Root, src string, dstRoot *os.Root, dst string) error {
	return moveByCopy(srcRoot, src, dstRoot, dst)
}
