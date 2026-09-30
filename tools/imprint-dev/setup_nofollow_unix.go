//go:build unix

package main

import "syscall"

// oNoFollow makes open fail on a symlink at the final path component, so a
// target swapped for a symlink after the Lstat check is still not followed.
const oNoFollow = syscall.O_NOFOLLOW
