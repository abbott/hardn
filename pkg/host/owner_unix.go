//go:build unix

package host

import (
	"io/fs"
	"syscall"
)

// sysOwner reads the owner from the real file system's stat data.
func sysOwner(fi fs.FileInfo) (uid, gid int, ok bool) {
	if st, isStat := fi.Sys().(*syscall.Stat_t); isStat && st != nil {
		return int(st.Uid), int(st.Gid), true
	}
	return 0, 0, false
}
