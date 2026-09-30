//go:build unix

package main

import (
	"io/fs"
	"syscall"
)

// oNoFollow makes open fail on a symlink at the final path component, so a
// target swapped for a symlink after the Lstat check is still not followed.
const oNoFollow = syscall.O_NOFOLLOW

// oNonBlock keeps opening a FIFO planted at a target from blocking.
const oNonBlock = syscall.O_NONBLOCK

// nlinkOf returns the file's hard link count.
func nlinkOf(fi fs.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink)
	}
	return 1
}
