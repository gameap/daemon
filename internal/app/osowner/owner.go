// Package osowner resolves and applies file ownership for paths created by
// the daemon on behalf of API requests (file uploads, mkdir, etc).
//
// The daemon typically runs as root and writes files into game-server
// directories owned by an unprivileged user. Without chown, those files
// become readable only by root and the game server (running as e.g. gameap)
// cannot read them. This package wraps user.Lookup + Lchown with the
// existing isRootUser/Windows no-op build constraints.
package osowner

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

// Options carries the owner information sent over the wire from the API.
// User takes precedence; UID/GID are an escape hatch for callers that
// already know numeric IDs.
type Options struct {
	User string
	UID  int32
	GID  int32
}

// IsZero reports whether no ownership info was supplied. Callers can short
// circuit on this before doing any work.
func (o Options) IsZero() bool {
	return o.User == "" && o.UID == 0 && o.GID == 0
}

// Resolve converts Options into numeric uid/gid. Returns ok=false when
// Options is zero or the daemon is not running as root (chown is a no-op
// for non-root daemons — same gate as the existing install_server.go path).
func Resolve(opts Options) (uid, gid int, ok bool, err error) {
	if opts.IsZero() {
		return 0, 0, false, nil
	}
	if !isRootUser() {
		return 0, 0, false, nil
	}

	if opts.User != "" {
		systemUser, lookupErr := user.Lookup(opts.User)
		if lookupErr != nil {
			return 0, 0, false, lookupErr
		}

		uid, err = strconv.Atoi(systemUser.Uid)
		if err != nil {
			return 0, 0, false, err
		}
		gid, err = strconv.Atoi(systemUser.Gid)
		if err != nil {
			return 0, 0, false, err
		}

		return uid, gid, true, nil
	}

	return int(opts.UID), int(opts.GID), true, nil
}

// ApplyToPath chowns a single path (Lchown — symlink-safe). No-op on
// daemon not running as root or when Options is zero.
func ApplyToPath(path string, opts Options) error {
	uid, gid, ok, err := Resolve(opts)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	return lchown(path, uid, gid)
}

// ApplyRecursive chowns path and all entries below it. No-op when chown
// is not applicable (non-root daemon, empty Options, Windows).
func ApplyRecursive(path string, opts Options) error {
	uid, gid, ok, err := Resolve(opts)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	return chownTree(path, uid, gid)
}

// MissingSegments returns the path prefixes that do not exist on disk yet,
// from the shallowest missing ancestor down to target itself. Callers
// invoke this BEFORE MkdirAll, then chown the returned paths after
// MkdirAll succeeds — this preserves ownership of parents that already
// existed (often shared, e.g. a Node WorkPath).
func MissingSegments(target string) ([]string, error) {
	target = filepath.Clean(target)
	if _, err := os.Lstat(target); err == nil {
		return nil, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	var missing []string
	for p := target; p != "." && p != "/" && p != filepath.Dir(p); p = filepath.Dir(p) {
		if _, err := os.Lstat(p); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		missing = append([]string{p}, missing...)
	}

	return missing, nil
}
