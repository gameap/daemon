package fsutil

import (
	"os"
	"path"
	"slices"

	"github.com/pkg/errors"
)

// parentDirPerm is the mode of the directories OpenDir creates on the way to
// the one it was asked for. They stay with the daemon and have to be passable
// for the user the game server runs as.
const parentDirPerm os.FileMode = 0o755

// OpenDir opens the directory dir names below workDir as the root everything
// done inside it goes through. A directory that is not there yet is created
// with perm, and the directories leading to it with 0755.
//
// The files of a game server belong to its tenant, and so does, where the work
// directory is owned by the user the servers run as, the name the directory
// itself is known by. Whoever opens that directory by its path follows a
// symlink put in its place wherever it leads, and whatever the daemon does next
// with its own privileges — unpacking an archive, changing owners — is done to
// the tree the link names. OpenDir reaches the directory through the work
// directory instead, and through every boundary enclosing it, one directory fd
// at a time: a link is followed only as far as it stays inside the directory it
// stands in. One that leads out of the work directory is refused, and so is one
// that leads nowhere.
//
// The directory is a boundary of its own, next to the ones opts name.
func OpenDir(workDir, dir string, perm os.FileMode, opts ...ConfineOption) (*os.Root, error) {
	return openDir(workDir, dir, opts, func(root *os.Root, rel string) error {
		return errors.Wrapf(makeDir(root, rel, perm), "failed to create directory %q", dir)
	})
}

// OpenExistingDir is OpenDir for a directory that has to be there already.
func OpenExistingDir(workDir, dir string, opts ...ConfineOption) (*os.Root, error) {
	return openDir(workDir, dir, opts, nil)
}

// openDir places dir and opens the root standing in it. prepare, when given,
// is called for a directory the placement did not open, with the root
// enclosing it.
func openDir(
	workDir, dir string, opts []ConfineOption, prepare func(root *os.Root, rel string) error,
) (*os.Root, error) {
	confiner := NewConfiner(workDir, append(slices.Clip(opts), WithRoot(dir))...)

	placed, err := confiner.Resolve(dir, TargetTree)
	if err != nil {
		return nil, err
	}

	if placed.Rel == "." {
		return placed.Root, nil
	}
	defer placed.Close()

	if prepare != nil {
		if err := prepare(placed.Root, placed.Rel); err != nil {
			return nil, err
		}
	}

	root, err := placed.Root.OpenRoot(placed.Rel)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open directory %q", dir)
	}

	return root, nil
}

// makeDir creates rel inside root unless something stands there already. What
// that is, is left to whoever opens it next.
func makeDir(root *os.Root, rel string, perm os.FileMode) error {
	if parent := path.Dir(rel); parent != "." {
		if err := root.MkdirAll(parent, parentDirPerm); err != nil {
			return err
		}
	}

	if err := root.Mkdir(rel, perm); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}

	return nil
}
