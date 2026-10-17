package host

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// allSteps is every step of WriteFileAtomic, in the order it must run them.
var allSteps = []string{stepOpenDir, stepCheck, stepCreate, stepWrite, stepChown, stepChmod, stepSync, stepClose, stepRecheck, stepRename, stepSyncDir}

// The steps run in the order brief §2.1 and the plan give: temporary file,
// write, owner, mode, fsync, rename, then fsync of the directory.
func TestRealWriteFileAtomicStepOrder(t *testing.T) {
	r := NewReal(Options{})
	var got []string
	r.fail = func(step string) error { got = append(got, step); return nil }
	name := filepath.Join(t.TempDir(), "00-hardn.conf")
	if err := r.WriteFileAtomic(context.Background(), name, []byte("Port 2230\n"), 0o644, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, allSteps) {
		t.Errorf("steps = %q, want %q", got, allSteps)
	}
}

// A failure at any step before the rename leaves the target as it was, or
// absent, and no temporary file; a failure syncing the directory comes after
// the rename and says so with ErrNotDurable.
func TestRealWriteFileAtomicFailsClosed(t *testing.T) {
	injected := errors.New("injected failure")
	for _, existing := range []bool{true, false} {
		for _, step := range allSteps {
			t.Run(map[bool]string{true: "replace", false: "create"}[existing]+"/"+step, func(t *testing.T) {
				dir := t.TempDir()
				name := filepath.Join(dir, "00-hardn.conf")
				if existing {
					writeFile(t, name, "Port 22\n", 0o644)
				}
				r := NewReal(Options{})
				r.fail = func(s string) error {
					if s == step {
						return injected
					}
					return nil
				}
				err := r.WriteFileAtomic(context.Background(), name, []byte("Port 2230\n"), 0o600, -1, -1)
				if !errors.Is(err, injected) {
					t.Fatalf("err = %v, want the injected failure", err)
				}
				if step == stepSyncDir {
					if !errors.Is(err, ErrNotDurable) {
						t.Errorf("err = %v, want ErrNotDurable", err)
					}
					wantContent(t, name, "Port 2230\n", 0o600)
					assertOnly(t, dir, "00-hardn.conf")
					return
				}
				if existing {
					wantContent(t, name, "Port 22\n", 0o644)
					assertOnly(t, dir, "00-hardn.conf")
				} else {
					assertOnly(t, dir)
				}
			})
		}
	}
}

// The temporary file is in the target's directory, never matches a ".conf"
// glob, and exists only with mode 0600 until the requested mode is set.
func TestRealWriteFileAtomicTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "00-hardn.conf")
	r := NewReal(Options{})
	r.fail = func(step string) error {
		if step != stepChmod {
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("at chmod: %v, %v; want one temporary file", entries, err)
		}
		fi, err := entries[0].Info()
		tmp := entries[0].Name()
		if err != nil || fi.Mode().Perm() != 0o600 || !strings.HasPrefix(tmp, ".00-hardn.conf.hardn-") || strings.HasSuffix(tmp, ".conf") {
			t.Errorf("temporary file %q mode %v (%v); want .00-hardn.conf.hardn-*, 0600", tmp, fi.Mode(), err)
		}
		return nil
	}
	if err := r.WriteFileAtomic(context.Background(), name, []byte("x"), 0o644, -1, -1); err != nil {
		t.Fatal(err)
	}
	wantContent(t, name, "x", 0o644)
}

// A symlink planted at the target after the first check is refused by the
// re-check before the rename: the link and its target stay as they were.
func TestRealWriteFileAtomicRecheck(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "authorized_keys")
	cluster := filepath.Join(dir, "cluster_keys")
	writeFile(t, cluster, "ssh-ed25519 KEY root@web-01.example.net\n", 0o600)
	r := NewReal(Options{})
	r.fail = func(step string) error {
		if step == stepRecheck {
			if err := os.Symlink(cluster, name); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}
	err := r.WriteFileAtomic(context.Background(), name, []byte("replaced\n"), 0o600, -1, -1)
	if !errors.Is(err, ErrSymlink) {
		t.Fatalf("err = %v, want ErrSymlink", err)
	}
	if got, err := os.Readlink(name); err != nil || got != cluster {
		t.Errorf("link now %q (%v), want %q", got, err, cluster)
	}
	wantContent(t, cluster, "ssh-ed25519 KEY root@web-01.example.net\n", 0o600)
	assertOnly(t, dir, "authorized_keys", "cluster_keys")
}

// The guarded operations refuse a symlink on the way in a directory another
// user can change (V21: ~alice/.ssh replaced by a link to /root/.ssh), and
// follow one in a trusted directory.
func TestRealGuardedWalk(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	rootSSH := filepath.Join(base, "root", ".ssh")
	mkdir(t, rootSSH, 0o700)
	keys := filepath.Join(rootSSH, "authorized_keys")
	writeFile(t, keys, "root-key\n", 0o600)
	mkdir(t, filepath.Join(base, "home", "alice"), 0o755)
	if err := os.Symlink(rootSSH, filepath.Join(base, "home", "alice", ".ssh")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rootSSH, filepath.Join(base, "trusted-link")); err != nil {
		t.Fatal(err)
	}
	via := filepath.Join(base, "home", "alice", ".ssh", "authorized_keys")
	ops := map[string]func(*Real) error{
		"write":    func(r *Real) error { return r.WriteFileAtomic(ctx, via, []byte("alice-key\n"), 0o600, -1, -1) },
		"chown":    func(r *Real) error { return r.Chown(ctx, via, os.Getuid(), -1) },
		"chmod":    func(r *Real) error { return r.Chmod(ctx, via, 0o644) },
		"remove":   func(r *Real) error { return r.Remove(ctx, via) },
		"read":     func(r *Real) error { _, _, err := r.ReadFileNoFollow(ctx, via); return err },
		"relative": func(r *Real) error { return r.WriteFileAtomic(ctx, "authorized_keys", nil, 0o600, -1, -1) },
	}

	owners := []struct {
		name  string
		setup func(r *Real)
	}{
		// home/alice is writable by others.
		{"world-writable", func(*Real) { chmod(t, filepath.Join(base, "home", "alice"), 0o777) }},
		// home/alice belongs to a user who is neither root nor hardn's.
		{"another user's", func(r *Real) {
			chmod(t, filepath.Join(base, "home", "alice"), 0o755)
			if os.Geteuid() == 0 {
				t.Skip("as root, the test's directories are root's and so trusted")
			}
			r.euid = os.Geteuid() + 1
		}},
	}
	for _, o := range owners {
		t.Run(o.name, func(t *testing.T) {
			for op, do := range ops {
				r := NewReal(Options{})
				o.setup(r)
				err := do(r)
				want := ErrSymlinkParent
				if op == "relative" {
					want = fs.ErrInvalid
				}
				if !errors.Is(err, want) {
					t.Errorf("%s: err = %v, want %v", op, err, want)
				}
			}
			wantContent(t, keys, "root-key\n", 0o600)
		})
	}

	t.Run("trusted directory", func(t *testing.T) {
		r := NewReal(Options{})
		name := filepath.Join(base, "trusted-link", "authorized_keys")
		if err := r.WriteFileAtomic(ctx, name, []byte("new\n"), 0o600, -1, -1); err != nil {
			t.Fatal(err)
		}
		wantContent(t, keys, "new\n", 0o600)
	})

	t.Run("a file on the way, and a symlink loop", func(t *testing.T) {
		r := NewReal(Options{})
		if err := r.WriteFileAtomic(ctx, filepath.Join(keys, "x"), nil, 0o600, -1, -1); !errors.Is(err, syscall.ENOTDIR) {
			t.Errorf("under a file: err = %v, want ENOTDIR", err)
		}
		loop := filepath.Join(base, "loop")
		if err := os.Symlink(loop, loop); err != nil {
			t.Fatal(err)
		}
		if err := r.WriteFileAtomic(ctx, filepath.Join(loop, "x"), nil, 0o600, -1, -1); !errors.Is(err, syscall.ELOOP) {
			t.Errorf("loop: err = %v, want ELOOP", err)
		}
	})
}

func TestRealReadFileNoFollow(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "sshd_config")
	writeFile(t, file, "Port 22\n", 0o640)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewReal(Options{})
	data, fi, err := r.ReadFileNoFollow(ctx, file)
	if err != nil || string(data) != "Port 22\n" || fi.Mode() != 0o640 || fi.Size() != 8 || fi.Name() != "sshd_config" {
		t.Errorf("file = %q, %v, %v", data, fi, err)
	}
	if uid, _, ok := Owner(fi); !ok || uid != os.Getuid() {
		t.Errorf("owner = %d (%v), want %d", uid, ok, os.Getuid())
	}
	for name, want := range map[string]error{link: ErrSymlink, dir: syscall.EISDIR, fifo: fs.ErrInvalid, filepath.Join(dir, "missing"): fs.ErrNotExist} {
		if _, _, err := r.ReadFileNoFollow(ctx, name); !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", filepath.Base(name), err, want)
		}
	}
}

// WriteFileAtomicKeep keeps an existing file's permission bits (never its
// setuid bit) and uses the given mode for a new file.
func TestWriteFileAtomicKeepMode(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	r := NewReal(Options{})
	name := filepath.Join(dir, "hardn.yml")
	writeFile(t, name, "old\n", 0o640)
	chmod(t, name, 0o640|fs.ModeSetuid)
	if err := WriteFileAtomicKeep(ctx, r, name, []byte("new\n"), 0o600, -1, -1); err != nil {
		t.Fatal(err)
	}
	wantContent(t, name, "new\n", 0o640)
	fresh := filepath.Join(dir, "fresh.yml")
	if err := WriteFileAtomicKeep(ctx, r, fresh, []byte("x"), 0o600, -1, -1); err != nil {
		t.Fatal(err)
	}
	wantContent(t, fresh, "x", 0o600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(name, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomicKeep(ctx, r, link, nil, 0o600, -1, -1); !errors.Is(err, ErrSymlink) {
		t.Errorf("symlink: err = %v, want ErrSymlink", err)
	}
}

// Owners need root: as root, the requested owner is set, and Keep keeps it.
func TestRealWriteFileAtomicOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("setting another user's ownership needs root")
	}
	ctx := context.Background()
	r := NewReal(Options{})
	name := filepath.Join(t.TempDir(), "authorized_keys")
	if err := r.WriteFileAtomic(ctx, name, []byte("k\n"), 0o600, 1234, 5678); err != nil {
		t.Fatal(err)
	}
	wantOwner(t, name, 1234, 5678)
	if err := WriteFileAtomicKeep(ctx, r, name, []byte("k2\n"), 0o644, 0, 0); err != nil {
		t.Fatal(err)
	}
	wantOwner(t, name, 1234, 5678)
	wantContent(t, name, "k2\n", 0o600)
}

func wantOwner(t *testing.T, name string, uid, gid int) {
	t.Helper()
	fi, err := os.Lstat(name)
	if err != nil {
		t.Fatal(err)
	}
	if u, g, ok := Owner(fi); !ok || u != uid || g != gid {
		t.Errorf("%s owner %d:%d (%v), want %d:%d", name, u, g, ok, uid, gid)
	}
}

func wantContent(t *testing.T, name, data string, mode fs.FileMode) {
	t.Helper()
	got, err := os.ReadFile(name)
	fi, serr := os.Lstat(name)
	if err != nil || serr != nil || string(got) != data || fi.Mode() != mode {
		t.Errorf("%s = %q (%v), mode %v (%v); want %q, mode %v", filepath.Base(name), got, err, modeOrNil(fi), serr, data, mode)
	}
}

func writeFile(t *testing.T, name, data string, perm fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(name, []byte(data), perm); err != nil {
		t.Fatal(err)
	}
	chmod(t, name, perm) // past the umask
}

func mkdir(t *testing.T, name string, perm fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(name, perm); err != nil {
		t.Fatal(err)
	}
}

func chmod(t *testing.T, name string, perm fs.FileMode) {
	t.Helper()
	if err := os.Chmod(name, perm); err != nil {
		t.Fatal(err)
	}
}
