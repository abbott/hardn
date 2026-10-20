package hostfake_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/abbott/hardn/pkg/host"
)

// TestLockContract runs Lock's cases against host.Real in a temporary
// directory and against the fake (p3-engine's run lock).
func TestLockContract(t *testing.T) {
	ctx := context.Background()
	read := func(t *testing.T, w world, rel string) string {
		t.Helper()
		data, _, err := w.sys.ReadFileNoFollow(ctx, w.path(rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(data)
	}
	cases := []struct {
		name string
		run  func(t *testing.T, w world)
	}{
		{"one holder at a time; unlock empties and frees it", func(t *testing.T, w world) {
			unlock, err := w.sys.Lock(ctx, w.path("engine.lock"), []byte("pid 4100\n"))
			if err != nil {
				t.Fatalf("Lock: %v", err)
			}
			fi, err := w.sys.Lstat(ctx, w.path("engine.lock"))
			if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 {
				t.Errorf("lock file: %v, %v; want a regular file, 0600 or narrower", fi, err)
			}
			if got := read(t, w, "engine.lock"); got != "pid 4100\n" {
				t.Errorf("holder %q", got)
			}
			if _, err := w.sys.Lock(ctx, w.path("engine.lock"), []byte("pid 4200\n")); !errors.Is(err, host.ErrLocked) {
				t.Errorf("second Lock: %v, want ErrLocked", err)
			}
			if got := read(t, w, "engine.lock"); got != "pid 4100\n" {
				t.Errorf("holder after the refused Lock: %q", got)
			}
			if err := unlock(); err != nil {
				t.Fatalf("unlock: %v", err)
			}
			if err := unlock(); err != nil {
				t.Errorf("second unlock: %v", err)
			}
			if got := read(t, w, "engine.lock"); got != "" {
				t.Errorf("after unlock: %q, want empty", got)
			}
			again, err := w.sys.Lock(ctx, w.path("engine.lock"), []byte("pid 4200\n"))
			if err != nil {
				t.Fatalf("Lock after unlock: %v", err)
			}
			_ = again()
		}},
		{"a lock file a dead holder left is not held", func(t *testing.T, w world) {
			w.file("engine.lock", "pid 3900, change 20261018T120000Z-1a2b3c4d\n", 0o600)
			unlock, err := w.sys.Lock(ctx, w.path("engine.lock"), []byte("pid 4100\n"))
			if err != nil {
				t.Fatalf("Lock: %v", err)
			}
			defer func() { _ = unlock() }()
			if got := read(t, w, "engine.lock"); got != "pid 4100\n" {
				t.Errorf("holder %q, want the new one", got)
			}
		}},
		{"a file replaced since is a new file, not locked", func(t *testing.T, w world) {
			unlock, err := w.sys.Lock(ctx, w.path("engine.lock"), nil)
			if err != nil {
				t.Fatalf("Lock: %v", err)
			}
			defer func() { _ = unlock() }()
			if err := w.sys.WriteFileAtomic(ctx, w.path("engine.lock"), nil, 0o600, -1, -1); err != nil {
				t.Fatalf("WriteFileAtomic: %v", err)
			}
			other, err := w.sys.Lock(ctx, w.path("engine.lock"), nil)
			if err != nil {
				t.Fatalf("Lock of the replacement: %v", err)
			}
			_ = other()
		}},
		{"refuses a symlink, a directory, a missing parent and a done context", func(t *testing.T, w world) {
			w.file("target", "kept", 0o600)
			w.symlink("link.lock", "target")
			if _, err := w.sys.Lock(ctx, w.path("link.lock"), []byte("x")); !errors.Is(err, host.ErrSymlink) {
				t.Errorf("symlink: %v, want ErrSymlink", err)
			}
			if got := read(t, w, "target"); got != "kept" {
				t.Errorf("symlink target written: %q", got)
			}
			w.dir("dir.lock")
			if _, err := w.sys.Lock(ctx, w.path("dir.lock"), nil); !errors.Is(err, fs.ErrInvalid) {
				t.Errorf("directory: %v, want fs.ErrInvalid", err)
			}
			if _, err := w.sys.Lock(ctx, w.path("missing/engine.lock"), nil); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("missing parent: %v, want fs.ErrNotExist", err)
			}
			done, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := w.sys.Lock(done, w.path("cancelled.lock"), nil); !errors.Is(err, context.Canceled) {
				t.Errorf("done context: %v, want context.Canceled", err)
			}
			if _, err := w.sys.Lstat(ctx, w.path("cancelled.lock")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("done context created the file: %v", err)
			}
		}},
	}
	for _, c := range cases {
		for _, mk := range []struct {
			name string
			new  func(*testing.T) world
		}{{"real", realWorld}, {"fake", fakeWorld}} {
			t.Run(c.name+"/"+mk.name, func(t *testing.T) { c.run(t, mk.new(t)) })
		}
	}
}
