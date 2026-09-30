//go:build !unix

package main

import "io/fs"

// oNoFollow is 0 where the platform has no O_NOFOLLOW; the Lstat checks in
// openAt still refuse a symlink target.
const oNoFollow = 0

// oNonBlock is 0 where the platform has no O_NONBLOCK.
const oNonBlock = 0

// nlinkOf reports 1 where the platform exposes no link count.
func nlinkOf(fs.FileInfo) uint64 { return 1 }
