//go:build linux

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// On Linux, setup reaches every target directory through directory file
// descriptors: the walk opens the resolved $HOME (a target) or the resolved
// inventory root (a quelle) and then each further component with
// O_DIRECTORY|O_NOFOLLOW relative to the previous one (openat).
// A component swapped for a symlink after the path checks makes the walk fail
// instead of being followed. This is the guarantee openat2 gives with
// RESOLVE_NO_SYMLINKS|RESOLVE_BENEATH, built from openat because
// golang.org/x/sys is not a dependency of this module (go.mod has no require).

const dirOpenFlags = syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC

// openDirBeneath opens dir, which must be base or lie below it, without
// following a symlink at base or in any component below it. With create,
// missing components are created with mode perm.
func openDirBeneath(base, dir string, create bool, perm fs.FileMode) (*os.File, error) {
	base, dir = filepath.Clean(base), filepath.Clean(dir)
	if !filepath.IsAbs(base) || !isWithin(base, dir) {
		return nil, fmt.Errorf("%s liegt nicht unter %s", dir, base)
	}
	fd, err := syscall.Open(base, dirOpenFlags, 0)
	if err != nil {
		return nil, walkError(base, err)
	}
	rel, err := filepath.Rel(base, dir)
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}
	if rel != "." {
		cur := base
		for _, name := range strings.Split(rel, string(filepath.Separator)) {
			cur = filepath.Join(cur, name)
			if name == "" || name == "." || name == ".." {
				syscall.Close(fd)
				return nil, fmt.Errorf("%s: ungültiger Pfadbestandteil %q", dir, name)
			}
			next, err := syscall.Openat(fd, name, dirOpenFlags, 0)
			if errors.Is(err, syscall.ENOENT) && create {
				if merr := syscall.Mkdirat(fd, name, uint32(perm.Perm())); merr != nil && !errors.Is(merr, syscall.EEXIST) {
					syscall.Close(fd)
					return nil, &fs.PathError{Op: "mkdirat", Path: cur, Err: merr}
				}
				next, err = syscall.Openat(fd, name, dirOpenFlags, 0)
			}
			syscall.Close(fd)
			if err != nil {
				return nil, walkError(cur, err)
			}
			fd = next
		}
	}
	return os.NewFile(uintptr(fd), dir), nil
}

func walkError(p string, err error) error {
	if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
		return fmt.Errorf("%s ist ein Symlink oder kein Verzeichnis; setup folgt keinem Symlink", p)
	}
	return &fs.PathError{Op: "openat", Path: p, Err: err}
}

// openAt opens name relative to the directory d, never following a symlink
// at name.
func openAt(d *os.File, name string, flag int, perm fs.FileMode) (*os.File, error) {
	p := filepath.Join(d.Name(), name)
	fd, err := syscall.Openat(int(d.Fd()), name, flag|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, uint32(perm.Perm()))
	runtime.KeepAlive(d)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%s ist ein Symlink; setup folgt keinem Symlink", p)
		}
		return nil, &fs.PathError{Op: "openat", Path: p, Err: err}
	}
	return os.NewFile(uintptr(fd), p), nil
}

// renameAt renames from to to, both relative to the directory d.
func renameAt(d *os.File, from, to string) error {
	err := syscall.Renameat(int(d.Fd()), from, int(d.Fd()), to)
	runtime.KeepAlive(d)
	if err != nil {
		return &fs.PathError{Op: "renameat", Path: filepath.Join(d.Name(), to), Err: err}
	}
	return nil
}

// unlinkAt removes name relative to the directory d.
func unlinkAt(d *os.File, name string) error {
	err := syscall.Unlinkat(int(d.Fd()), name)
	runtime.KeepAlive(d)
	return err
}
