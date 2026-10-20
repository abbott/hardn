package host

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestRealAppendRefuses covers what the contract test (hostfake) cannot,
// on real files in t.TempDir(): a FIFO is refused without blocking; a
// directory another user could change is refused, whatever it holds (here
// a file with a second hard link, which may be a root file that user
// linked there), and the same file in that directory once trusted again is
// appended to (the rule Chmod and Chown keep); a done context is refused
// before anything is opened.
func TestRealAppendRefuses(t *testing.T) {
	ctx := context.Background()
	r := NewReal(Options{})
	dir := t.TempDir()

	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendFile(ctx, fifo, []byte("x\n"), 0o600); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("FIFO: err = %v, want fs.ErrInvalid", err)
	}

	open := filepath.Join(dir, "open")
	if err := os.Mkdir(open, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shadow"), []byte("root:x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(dir, "shadow"), filepath.Join(open, "audit.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o777); err != nil { // writable by others: not trusted
		t.Fatal(err)
	}
	if err := r.AppendFile(ctx, filepath.Join(open, "audit.jsonl"), []byte("x\n"), 0o600); !errors.Is(err, ErrUntrustedDir) {
		t.Errorf("an untrusted directory: err = %v, want ErrUntrustedDir", err)
	}
	if err := r.AppendFile(ctx, filepath.Join(open, "new.jsonl"), []byte("x\n"), 0o600); !errors.Is(err, ErrUntrustedDir) {
		t.Errorf("a new file in an untrusted directory: err = %v, want ErrUntrustedDir", err)
	}
	if _, err := os.Lstat(filepath.Join(open, "new.jsonl")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("created in an untrusted directory: %v", err)
	}
	if err := os.Chmod(open, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendFile(ctx, filepath.Join(open, "audit.jsonl"), []byte("x\n"), 0o600); err != nil {
		t.Errorf("a hard link in a trusted directory: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "shadow")); err != nil || string(data) != "root:x\nx\n" {
		t.Errorf("after the trusted append: %q, %v", data, err)
	}

	done, cancel := context.WithCancel(ctx)
	cancel()
	if err := r.AppendFile(done, filepath.Join(dir, "new"), []byte("x\n"), 0o600); !errors.Is(err, context.Canceled) {
		t.Errorf("done context: err = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "new")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a done context created the file: %v", err)
	}
}
