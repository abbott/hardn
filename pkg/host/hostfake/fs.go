package hostfake

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/abbott/hardn/pkg/host"
)

// maxSymlinks is Linux's limit on symlinks followed in one lookup (ELOOP).
const maxSymlinks = 40

type node struct {
	mode     fs.FileMode // type bits and permissions
	data     []byte
	target   string // symlinks
	uid, gid int
}

// Entry is a file system entry as the fake holds it, for assertions.
type Entry struct {
	Mode     fs.FileMode // type bits and permissions
	Data     []byte
	Target   string // a symlink's target
	UID, GID int
}

// Entry returns the entry at name, without following a symlink at name and
// without recording a call. ok is false when there is none.
func (s *System) Entry(name string) (e Entry, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.resolve(name, false)
	if err != nil {
		return Entry{}, false
	}
	n := s.nodes[p]
	if n == nil {
		return Entry{}, false
	}
	return Entry{Mode: n.mode, Data: bytes.Clone(n.data), Target: n.target, UID: n.uid, GID: n.gid}, true
}

// AddDir adds a directory and any missing parents (0755, root), unrecorded.
func (s *System) AddDir(name string, perm fs.FileMode, uid, gid int) {
	s.add(name, &node{mode: fs.ModeDir | perm.Perm(), uid: uid, gid: gid})
}

// AddFile adds a regular file and any missing parents (0755, root), unrecorded.
func (s *System) AddFile(name string, data []byte, perm fs.FileMode, uid, gid int) {
	s.add(name, &node{mode: perm.Perm(), data: bytes.Clone(data), uid: uid, gid: gid})
}

// AddSymlink adds a symlink to target and any missing parents, unrecorded.
func (s *System) AddSymlink(name, target string) {
	s.add(name, &node{mode: fs.ModeSymlink | 0o777, target: target})
}

// add is for seeding: a bad path is a broken test, so it panics.
func (s *System) add(name string, n *node) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !path.IsAbs(name) || path.Clean(name) == "/" {
		panic("hostfake: seed path must be absolute and not /: " + name)
	}
	p := path.Clean(name)
	if err := s.mkdirAll(path.Dir(p), 0o755); err != nil {
		panic("hostfake: seed " + name + ": " + err.Error())
	}
	parent, err := s.resolve(path.Dir(p), true)
	if err != nil {
		panic("hostfake: seed " + name + ": " + err.Error())
	}
	s.nodes[path.Join(parent, path.Base(p))] = n
}

// resolve returns the path name refers to, following symlinks in every
// component but the last, and in the last too when follow is set. The result
// may not exist; a missing or non-directory parent is an error.
func (s *System) resolve(name string, follow bool) (string, error) {
	if !path.IsAbs(name) {
		return "", fs.ErrInvalid
	}
	rest := split(path.Clean(name))
	cur, links := "/", 0
	for i := 0; i < len(rest); i++ {
		next := path.Join(cur, rest[i])
		last := i == len(rest)-1
		n := s.nodes[next]
		switch {
		case n == nil && last:
			return next, nil
		case n == nil:
			return "", syscall.ENOENT
		case n.mode&fs.ModeSymlink != 0 && (!last || follow):
			if links++; links > maxSymlinks {
				return "", syscall.ELOOP
			}
			t := n.target
			if !path.IsAbs(t) {
				t = path.Join(cur, t)
			}
			rest = append(split(path.Clean(t)), rest[i+1:]...)
			cur, i = "/", -1
		case !last && !n.mode.IsDir():
			return "", syscall.ENOTDIR
		default:
			cur = next
		}
	}
	return cur, nil
}

func split(p string) []string {
	if p == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(p, "/"), "/")
}

// lookup resolves name and returns the node there; ENOENT when there is none.
func (s *System) lookup(name string, follow bool) (string, *node, error) {
	p, err := s.resolve(name, follow)
	if err != nil {
		return "", nil, err
	}
	n := s.nodes[p]
	if n == nil {
		return p, nil, syscall.ENOENT
	}
	return p, n, nil
}

func (s *System) hasChildren(dir string) bool {
	prefix := strings.TrimSuffix(dir, "/") + "/"
	for k := range s.nodes {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// do runs op under the lock, after the ctx check, and records c with its error.
func (s *System) do(ctx context.Context, c Call, op func() error) error {
	var err error
	if err = ctx.Err(); err == nil {
		s.mu.Lock()
		err = op()
		s.mu.Unlock()
	}
	c.Err = err
	s.record(c)
	return err
}

func perr(op, name string, err error) error {
	if err == nil {
		return nil
	}
	return &fs.PathError{Op: op, Path: name, Err: err}
}

// ReadFile follows symlinks, as os.ReadFile does.
func (s *System) ReadFile(ctx context.Context, name string) ([]byte, error) {
	var data []byte
	err := s.do(ctx, Call{Op: "read", Path: name}, func() error {
		_, n, err := s.lookup(name, true)
		switch {
		case err != nil:
			return perr("open", name, err)
		case n.mode.IsDir():
			return perr("read", name, syscall.EISDIR)
		}
		data = bytes.Clone(n.data)
		return nil
	})
	return data, err
}

// Stat follows a symlink at name.
func (s *System) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	return s.stat(ctx, "stat", name, true)
}

// Lstat does not follow a symlink at name.
func (s *System) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	return s.stat(ctx, "lstat", name, false)
}

func (s *System) stat(ctx context.Context, op, name string, follow bool) (fs.FileInfo, error) {
	var fi fs.FileInfo
	err := s.do(ctx, Call{Op: op, Path: name}, func() error {
		_, n, err := s.lookup(name, follow)
		if err != nil {
			return perr(op, name, err)
		}
		fi = info(path.Base(name), n)
		return nil
	})
	return fi, err
}

// ReadDir lists a directory sorted by name, following a symlink at name.
func (s *System) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	var entries []fs.DirEntry
	err := s.do(ctx, Call{Op: "readdir", Path: name}, func() error {
		p, n, err := s.lookup(name, true)
		switch {
		case err != nil:
			return perr("open", name, err)
		case !n.mode.IsDir():
			return perr("readdirent", name, syscall.ENOTDIR)
		}
		var names []string
		for k := range s.nodes {
			if k != "/" && path.Dir(k) == p {
				names = append(names, path.Base(k))
			}
		}
		sort.Strings(names)
		for _, b := range names {
			entries = append(entries, fs.FileInfoToDirEntry(info(b, s.nodes[path.Join(p, b)])))
		}
		return nil
	})
	return entries, err
}

// MkdirAll creates missing directories with perm, owned by root.
func (s *System) MkdirAll(ctx context.Context, name string, perm fs.FileMode) error {
	return s.do(ctx, Call{Op: "mkdir", Path: name, Mode: perm}, func() error {
		return s.mkdirAll(name, perm)
	})
}

func (s *System) mkdirAll(name string, perm fs.FileMode) error {
	if !path.IsAbs(name) {
		return perr("mkdir", name, fs.ErrInvalid)
	}
	p, n, err := s.lookup(name, true)
	switch {
	case err == nil && n.mode.IsDir():
		return nil
	case err == nil:
		return perr("mkdir", name, syscall.ENOTDIR)
	case p == "":
		// A parent is missing: create the parents first.
		if err := s.mkdirAll(path.Dir(path.Clean(name)), perm); err != nil {
			return err
		}
		if p, err = s.resolve(name, true); err != nil {
			return perr("mkdir", name, err)
		}
	}
	if _, link, _ := s.lookup(name, false); link != nil {
		// A dangling symlink: os.MkdirAll's mkdir(2) fails on it.
		return perr("mkdir", name, syscall.EEXIST)
	}
	s.nodes[p] = &node{mode: fs.ModeDir | perm.Perm()}
	return nil
}

// Remove removes a file, a symlink or an empty directory.
func (s *System) Remove(ctx context.Context, name string) error {
	return s.do(ctx, Call{Op: "remove", Path: name}, func() error {
		p, n, err := s.lookup(name, false)
		switch {
		case err != nil:
			return perr("remove", name, err)
		case p == "/" || n.mode.IsDir() && s.hasChildren(p):
			return perr("remove", name, syscall.ENOTEMPTY)
		}
		delete(s.nodes, p)
		return nil
	})
}

// RemoveAll removes name and everything under it; it does not follow a
// symlink at name. A missing name is not an error.
func (s *System) RemoveAll(ctx context.Context, name string) error {
	return s.do(ctx, Call{Op: "removeall", Path: name}, func() error {
		p, n, err := s.lookup(name, false)
		switch {
		case err == syscall.ENOENT:
			return nil
		case err != nil:
			return perr("unlinkat", name, err)
		case p == "/":
			return perr("unlinkat", name, syscall.EINVAL)
		}
		if n.mode.IsDir() {
			for k := range s.nodes {
				if strings.HasPrefix(k, p+"/") {
					delete(s.nodes, k)
				}
			}
		}
		delete(s.nodes, p)
		return nil
	})
}

// Rename follows rename(2): a symlink at either name is moved or replaced
// itself; a directory replaces only an empty directory.
func (s *System) Rename(ctx context.Context, oldname, newname string) error {
	return s.do(ctx, Call{Op: "rename", Path: oldname, To: newname}, func() error {
		lerr := func(err error) error { return &os.LinkError{Op: "rename", Old: oldname, New: newname, Err: err} }
		po, no, err := s.lookup(oldname, false)
		if err != nil {
			return lerr(err)
		}
		pn, err := s.resolve(newname, false)
		if err != nil {
			return lerr(err)
		}
		if parent := s.nodes[path.Dir(pn)]; parent == nil || !parent.mode.IsDir() {
			return lerr(syscall.ENOENT)
		}
		if po == pn {
			return nil
		}
		if strings.HasPrefix(pn, po+"/") {
			return lerr(syscall.EINVAL)
		}
		if nn := s.nodes[pn]; nn != nil {
			switch {
			case nn.mode.IsDir() && !no.mode.IsDir():
				return lerr(syscall.EISDIR)
			case !nn.mode.IsDir() && no.mode.IsDir():
				return lerr(syscall.ENOTDIR)
			case nn.mode.IsDir() && s.hasChildren(pn):
				return lerr(syscall.ENOTEMPTY)
			}
		}
		moved := map[string]*node{pn: no}
		for k, v := range s.nodes {
			if strings.HasPrefix(k, po+"/") {
				moved[pn+strings.TrimPrefix(k, po)] = v
			}
		}
		for k := range s.nodes {
			if k == po || strings.HasPrefix(k, po+"/") || strings.HasPrefix(k, pn+"/") {
				delete(s.nodes, k)
			}
		}
		for k, v := range moved {
			s.nodes[k] = v
		}
		return nil
	})
}

// Chown refuses a symlink at name; -1 leaves the uid or gid unchanged.
func (s *System) Chown(ctx context.Context, name string, uid, gid int) error {
	return s.do(ctx, Call{Op: "chown", Path: name, UID: uid, GID: gid}, func() error {
		_, n, err := s.lookup(name, false)
		switch {
		case err != nil:
			return perr("lchown", name, err)
		case n.mode&fs.ModeSymlink != 0:
			return perr("chown", name, host.ErrSymlink)
		}
		if uid != -1 {
			n.uid = uid
		}
		if gid != -1 {
			n.gid = gid
		}
		return nil
	})
}

// WriteFileAtomic replaces name with a new file of mode perm owned by uid:gid
// (-1: root). It refuses a symlink or a directory at name.
func (s *System) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	c := Call{Op: "write-atomic", Path: name, Data: bytes.Clone(data), Mode: perm, UID: uid, GID: gid}
	return s.do(ctx, c, func() error {
		p, n, err := s.lookup(name, false)
		switch {
		case err != nil && n == nil && p == "":
			return perr("open", name, err)
		case n != nil && n.mode&fs.ModeSymlink != 0:
			return perr("write", name, host.ErrSymlink)
		case n != nil && n.mode.IsDir():
			return perr("write", name, syscall.EISDIR)
		}
		s.nodes[p] = &node{mode: perm.Perm(), data: bytes.Clone(data), uid: max(uid, 0), gid: max(gid, 0)}
		return nil
	})
}

// WriteFileInPlace is os.WriteFile: it follows a symlink at name, keeps an
// existing file's mode and owner, and creates a missing one (root).
func (s *System) WriteFileInPlace(ctx context.Context, name string, data []byte, perm fs.FileMode) error {
	c := Call{Op: "write-in-place", Path: name, Data: bytes.Clone(data), Mode: perm}
	return s.do(ctx, c, func() error {
		p, n, err := s.lookup(name, true)
		switch {
		case err != nil && n == nil && p == "":
			return perr("open", name, err)
		case n != nil && n.mode.IsDir():
			return perr("open", name, syscall.EISDIR)
		case n != nil:
			n.data = bytes.Clone(data)
			return nil
		}
		s.nodes[p] = &node{mode: perm.Perm(), data: bytes.Clone(data)}
		return nil
	})
}

// fileInfo is the fake's fs.FileInfo; Sys() is a *host.Ownership.
type fileInfo struct {
	name string
	n    node
}

func info(name string, n *node) fs.FileInfo { return fileInfo{name: name, n: *n} }

func (f fileInfo) Name() string       { return f.name }
func (f fileInfo) Size() int64        { return int64(len(f.n.data) + len(f.n.target)) }
func (f fileInfo) Mode() fs.FileMode  { return f.n.mode }
func (f fileInfo) ModTime() time.Time { return time.Time{} }
func (f fileInfo) IsDir() bool        { return f.n.mode.IsDir() }
func (f fileInfo) Sys() any           { return &host.Ownership{UID: f.n.uid, GID: f.n.gid} }
