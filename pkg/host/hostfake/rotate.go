package hostfake

import (
	"context"
	"io/fs"
	"path"
	"strconv"

	"github.com/abbott/hardn/pkg/host"
)

// RotateFile renames the regular file at name to name.1 once it holds more
// than limit bytes, each copy moved up and the oldest replaced; see
// host.System. It refuses what host.Real's refuses: a symlink at name or
// at a copy (host.ErrSymlink), anything else that is not a regular file
// (fs.ErrInvalid) and, with trusted, a directory holding it that is not
// trusted (host.ErrUntrustedDir). The fake has one process, so nothing
// races it. Recorded as rotate.
func (s *System) RotateFile(ctx context.Context, name string, limit int64, keep int, trusted bool) (bool, error) {
	rotated := false
	err := s.do(ctx, Call{Op: "rotate", Path: name}, func() error {
		if limit <= 0 || keep < 1 {
			return perr("rotate", name, fs.ErrInvalid)
		}
		p, n, err := s.guardedLookup(name)
		if err != nil && n == nil && p == "" {
			return perr("open", name, err)
		}
		if d := s.nodes[path.Dir(p)]; trusted && (d == nil || d.uid != 0 || d.mode&0o022 != 0) {
			return perr("rotate", name, host.ErrUntrustedDir)
		}
		refused := func(n *node, name string) error {
			switch {
			case n == nil:
			case n.mode&fs.ModeSymlink != 0:
				return perr("rotate", name, host.ErrSymlink)
			case !n.mode.IsRegular():
				return perr("rotate", name, fs.ErrInvalid)
			}
			return nil
		}
		if err := refused(n, name); err != nil || n == nil || int64(len(n.data)) <= limit {
			return err
		}
		copyOf := func(i int) string { return "." + strconv.Itoa(i) }
		for i := 1; i <= keep; i++ {
			if err := refused(s.nodes[p+copyOf(i)], name+copyOf(i)); err != nil {
				return err
			}
		}
		for i := keep - 1; i >= 1; i-- {
			if c := s.nodes[p+copyOf(i)]; c != nil {
				s.nodes[p+copyOf(i+1)] = c
				delete(s.nodes, p+copyOf(i))
			}
		}
		s.nodes[p+".1"] = n
		delete(s.nodes, p)
		rotated = true
		return nil
	})
	return rotated, err
}
