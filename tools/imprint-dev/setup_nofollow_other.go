//go:build !unix

package main

// oNoFollow is 0 where the platform has no O_NOFOLLOW; the Lstat checks in
// readRegularFile and writeRegularFile still refuse a symlink target.
const oNoFollow = 0
