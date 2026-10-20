package hostfake

import (
	"bytes"
	"context"
	"io/fs"
	"path"
	"syscall"

	"github.com/abbott/hardn/pkg/host"
)

// AppendFile appends data to the regular file at name, creating it with
// perm (root) when absent; see host.System. A directory holding it that is
// not trusted (owned by root, as the fake acts as root, and writable by
// neither group nor others) is refused (host.ErrUntrustedDir), a symlink at
// name (host.ErrSymlink), a directory with EISDIR, anything else that is
// not a regular file with fs.ErrInvalid; it is guarded. The fake has no
// hard links. Recorded as append, with the data appended.
func (s *System) AppendFile(ctx context.Context, name string, data []byte, perm fs.FileMode) error {
	c := Call{Op: "append", Path: name, Data: bytes.Clone(data), Mode: perm}
	return s.do(ctx, c, func() error {
		p, n, err := s.guardedLookup(name)
		if err != nil && n == nil && p == "" {
			return perr("open", name, err)
		}
		if d := s.nodes[path.Dir(p)]; d == nil || d.uid != 0 || d.mode&0o022 != 0 {
			return perr("append", name, host.ErrUntrustedDir)
		}
		switch {
		case n == nil:
			s.nodes[p] = &node{mode: perm.Perm(), data: bytes.Clone(data)}
			return nil
		case n.mode&fs.ModeSymlink != 0:
			return perr("append", name, host.ErrSymlink)
		case n.mode.IsDir():
			return perr("append", name, syscall.EISDIR)
		case !n.mode.IsRegular():
			return perr("append", name, fs.ErrInvalid)
		}
		n.data = append(n.data, data...)
		return nil
	})
}
