//go:build !linux

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Outside Linux, setup falls back to path-based access: the directory is
// resolved again right before use and must not have changed, and the final
// name is checked with Lstat and opened with O_NOFOLLOW where the platform
// has it. The window between that check and the open stays; the Linux build
// closes it with directory file descriptors (setup_fs_linux.go).

func openDirBeneath(base, dir string, create bool, perm fs.FileMode) (*os.File, error) {
	base, dir = filepath.Clean(base), filepath.Clean(dir)
	if !filepath.IsAbs(base) || !isWithin(base, dir) {
		return nil, fmt.Errorf("%s liegt nicht unter %s", dir, base)
	}
	if create {
		if err := os.MkdirAll(dir, perm); err != nil {
			return nil, err
		}
	}
	again, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	if again != dir {
		return nil, fmt.Errorf("%s enthält einen Symlink; setup folgt keinem Symlink", dir)
	}
	return os.Open(dir)
}

func openAt(d *os.File, name string, flag int, perm fs.FileMode) (*os.File, error) {
	p := filepath.Join(d.Name(), name)
	if fi, err := os.Lstat(p); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s ist ein Symlink; setup folgt keinem Symlink", p)
	}
	return os.OpenFile(p, flag|oNoFollow, perm)
}

func renameAt(d *os.File, from, to string) error {
	return os.Rename(filepath.Join(d.Name(), from), filepath.Join(d.Name(), to))
}

func unlinkAt(d *os.File, name string) error {
	return os.Remove(filepath.Join(d.Name(), name))
}
