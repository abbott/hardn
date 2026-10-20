package hostfake

import (
	"bytes"
	"context"
	"io/fs"

	"github.com/abbott/hardn/pkg/host"
)

// Lock holds the file at name, creating it 0600 (root) when absent, and
// writes holder into it; see host.System. The lock belongs to the file
// itself, as the kernel's belongs to the open file: a file replaced since
// (WriteFileAtomic) is a new one, not locked, and a lock file a test seeds
// with AddFile, as a process that died leaves one, is not held. Lock and
// the unlock it returns are recorded (lock, unlock).
func (s *System) Lock(ctx context.Context, name string, holder []byte) (func() error, error) {
	var held *node
	err := s.do(ctx, Call{Op: "lock", Path: name, Data: bytes.Clone(holder)}, func() error {
		p, n, err := s.guardedLookup(name)
		switch {
		case err != nil && n == nil && p == "":
			return perr("open", name, err)
		case n != nil && n.mode&fs.ModeSymlink != 0:
			return perr("lock", name, host.ErrSymlink)
		case n != nil && !n.mode.IsRegular():
			return perr("lock", name, fs.ErrInvalid)
		case n != nil && s.locks[n]:
			return perr("lock", name, host.ErrLocked)
		case n == nil:
			n = &node{mode: 0o600}
			s.nodes[p] = n
		}
		n.data = bytes.Clone(holder)
		s.locks[n], held = true, n
		return nil
	})
	if err != nil {
		return nil, err
	}
	return func() error {
		s.mu.Lock()
		was := s.locks[held]
		if was {
			delete(s.locks, held)
			held.data = nil
		}
		s.mu.Unlock()
		if was {
			s.record(Call{Op: "unlock", Path: name})
		}
		return nil
	}, nil
}
