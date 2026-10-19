//go:build !unix

package host

import "io/fs"

// sysOwner: no owner outside Unix; hardn builds for Linux and macOS only.
func sysOwner(fs.FileInfo) (uid, gid int, ok bool) { return 0, 0, false }
