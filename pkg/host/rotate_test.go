package host

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// rotateDir is a directory holding a log of size bytes and the copies
// given, each holding its own name.
func rotateDir(t *testing.T, size int, copies ...int) (dir, log string) {
	t.Helper()
	dir = t.TempDir()
	log = filepath.Join(dir, "hardn.log")
	if err := os.WriteFile(log, []byte(strings.Repeat("x", size)), 0o640); err != nil {
		t.Fatal(err)
	}
	for _, i := range copies {
		name := fmt.Sprintf("%s.%d", log, i)
		if err := os.WriteFile(name, []byte(filepath.Base(name)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, log
}

// held is what dir holds: each name and its content, a log's own content
// as its length.
func held(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got[e.Name()] = string(data)
		if strings.Trim(string(data), "x") == "" {
			got[e.Name()] = fmt.Sprintf("%d bytes", len(data))
		}
	}
	return got
}

// TestRotateFile: a file at the limit stays; one byte over, it becomes .1,
// each copy moves up, the oldest is deleted, and every copy keeps its mode.
func TestRotateFile(t *testing.T) {
	ctx := context.Background()
	r := NewReal(Options{})

	dir, log := rotateDir(t, 100, 1, 2, 3)
	before := held(t, dir)
	if rotated, err := r.RotateFile(ctx, log, 100, 3, false); rotated || err != nil {
		t.Fatalf("at the limit: rotated %v, %v", rotated, err)
	}
	if got := held(t, dir); fmt.Sprint(got) != fmt.Sprint(before) {
		t.Errorf("at the limit: %v, was %v", got, before)
	}

	dir, log = rotateDir(t, 101, 1, 2, 3)
	if rotated, err := r.RotateFile(ctx, log, 100, 3, false); !rotated || err != nil {
		t.Fatalf("over the limit: rotated %v, %v", rotated, err)
	}
	want := map[string]string{"hardn.log.1": "101 bytes", "hardn.log.2": "hardn.log.1", "hardn.log.3": "hardn.log.2"}
	if got := held(t, dir); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("over the limit: %v, want %v", got, want)
	}
	for name, mode := range map[string]fs.FileMode{"hardn.log.1": 0o640, "hardn.log.2": 0o600} {
		if fi, err := os.Lstat(filepath.Join(dir, name)); err != nil || fi.Mode().Perm() != mode {
			t.Errorf("%s: mode %v, %v, want %v", name, fi.Mode(), err, mode)
		}
	}

	// No copies yet, a gap among them, and no file at all.
	dir, log = rotateDir(t, 101, 2)
	if rotated, err := r.RotateFile(ctx, log, 100, 4, false); !rotated || err != nil {
		t.Fatalf("a gap: rotated %v, %v", rotated, err)
	}
	want = map[string]string{"hardn.log.1": "101 bytes", "hardn.log.3": "hardn.log.2"}
	if got := held(t, dir); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("a gap: %v, want %v", got, want)
	}
	if rotated, err := r.RotateFile(ctx, log, 100, 4, false); rotated || err != nil {
		t.Errorf("no file: rotated %v, %v", rotated, err)
	}
}

// TestRotateFileRefuses: a symlink or anything else that is not a regular
// file, at the log's path or at a copy's, is never followed and nothing is
// renamed; nor in a directory another user can change, when asked; nor
// with a done context.
func TestRotateFileRefuses(t *testing.T) {
	ctx := context.Background()
	r := NewReal(Options{})
	for _, tc := range []struct {
		name    string
		spoil   func(t *testing.T, dir, log string)
		trusted bool
		want    error
	}{
		{"the log is a symlink", func(t *testing.T, dir, log string) {
			must(t, os.Rename(log, filepath.Join(dir, "target")))
			must(t, os.Symlink(filepath.Join(dir, "target"), log))
		}, false, ErrSymlink},
		{"the log is a directory", func(t *testing.T, _, log string) {
			must(t, os.Remove(log))
			must(t, os.Mkdir(log, 0o755))
		}, false, fs.ErrInvalid},
		{"a copy is a symlink", func(t *testing.T, dir, log string) {
			must(t, os.Remove(log+".2"))
			must(t, os.Symlink(filepath.Join(dir, "nowhere"), log+".2"))
		}, false, ErrSymlink},
		{"the oldest copy is a FIFO", func(t *testing.T, _, log string) {
			must(t, os.Remove(log+".3"))
			must(t, unix.Mkfifo(log+".3", 0o600))
		}, false, fs.ErrInvalid},
		{"a copy is a directory", func(t *testing.T, _, log string) {
			must(t, os.Remove(log+".1"))
			must(t, os.Mkdir(log+".1", 0o755))
		}, false, fs.ErrInvalid},
		{"a directory others can write", func(t *testing.T, dir, _ string) {
			must(t, os.Chmod(dir, 0o777))
		}, true, ErrUntrustedDir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, log := rotateDir(t, 101, 1, 2, 3)
			tc.spoil(t, dir, log)
			before := lnames(t, dir)
			if rotated, err := r.RotateFile(ctx, log, 100, 3, tc.trusted); rotated || !errors.Is(err, tc.want) {
				t.Errorf("rotated %v, err = %v, want %v", rotated, err, tc.want)
			}
			if got := lnames(t, dir); got != before {
				t.Errorf("renamed: %s, was %s", got, before)
			}
		})
	}

	// The same directory, not asked to be trusted: /var/log is its group's
	// to write on some releases.
	dir, log := rotateDir(t, 101)
	must(t, os.Chmod(dir, 0o777))
	if rotated, err := r.RotateFile(ctx, log, 100, 3, false); !rotated || err != nil {
		t.Errorf("a directory others can write, trust not asked: rotated %v, %v", rotated, err)
	}

	_, log = rotateDir(t, 101)
	done, cancel := context.WithCancel(ctx)
	cancel()
	if rotated, err := r.RotateFile(done, log, 100, 3, false); rotated || !errors.Is(err, context.Canceled) {
		t.Errorf("done context: rotated %v, %v", rotated, err)
	}
	for _, bad := range [][2]int{{0, 3}, {100, 0}} {
		if rotated, err := r.RotateFile(ctx, log, int64(bad[0]), bad[1], false); rotated || !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("limit %d, keep %d: rotated %v, %v", bad[0], bad[1], rotated, err)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// lnames is each name in dir with its type, unfollowed.
func lnames(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	must(t, err)
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%s(%v) ", e.Name(), e.Type())
	}
	return b.String()
}

// TestRotateFileFails: a rename that fails (a read-only or full disk) is
// the error, and the log is still there to append to.
func TestRotateFileFails(t *testing.T) {
	r := NewReal(Options{})
	dir, log := rotateDir(t, 101, 1)
	r.fail = func(step string) error {
		if step == "rotate-rename" {
			return unix.EROFS
		}
		return nil
	}
	if rotated, err := r.RotateFile(context.Background(), log, 100, 3, false); rotated || !errors.Is(err, unix.EROFS) {
		t.Errorf("rotated %v, err = %v, want EROFS", rotated, err)
	}
	want := map[string]string{"hardn.log": "101 bytes", "hardn.log.1": "hardn.log.1"}
	if got := held(t, dir); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("after the failure: %v, want %v", got, want)
	}
}

// TestRotateFileRace: many rotators at once, while others append, rotate
// the file once; every copy moves up once, and no line is lost, those of a
// writer that had the file open before the rename included.
func TestRotateFileRace(t *testing.T) {
	ctx := context.Background()
	r := NewReal(Options{})
	for round := range 20 {
		dir, log := rotateDir(t, 101, 1, 2)
		early, err := os.OpenFile(log, os.O_WRONLY|os.O_APPEND, 0)
		must(t, err)
		const rotators, writers = 8, 4
		var wg sync.WaitGroup
		rotations := make(chan bool, rotators)
		start := make(chan struct{})
		for range rotators {
			wg.Go(func() {
				<-start
				rotated, err := r.RotateFile(ctx, log, 100, 4, false)
				if err != nil {
					t.Errorf("round %d: %v", round, err)
				}
				rotations <- rotated
			})
		}
		for w := range writers {
			wg.Go(func() {
				<-start
				if err := r.AppendFile(ctx, log, fmt.Appendf(nil, "\nline %d", w), 0o640); err != nil {
					t.Errorf("round %d, writer %d: %v", round, w, err)
				}
			})
		}
		close(start)
		wg.Wait()
		close(rotations)
		_, err = early.WriteString("\nline early")
		must(t, err)
		must(t, early.Close())
		n := 0
		for rotated := range rotations {
			if rotated {
				n++
			}
		}
		// The limit is 100 bytes: a new file the writers filled past it
		// before the last rotator looked may be rotated too, and is no loss.
		if n < 1 {
			t.Errorf("round %d: %d rotations, want one at least", round, n)
		}
		all := ""
		for name, data := range heldRaw(t, dir) {
			all += name + ":" + data + "\n"
		}
		for _, want := range []string{"hardn.log.1", "hardn.log.2", strings.Repeat("x", 101), "line early", "line 0", "line 1", "line 2", "line 3"} {
			if strings.Count(all, want) < 1 {
				t.Errorf("round %d: %q lost, with %d rotations, in %v", round, want, n, lnames(t, dir))
			}
		}
	}
}

// heldRaw is each file in dir with its content.
func heldRaw(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	must(t, err)
	got := map[string]string{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		must(t, err)
		got[e.Name()] = string(data)
	}
	return got
}

// TestRotateFileOnce: with nothing appending, rotators at once rotate the
// file exactly once: the others find it done.
func TestRotateFileOnce(t *testing.T) {
	ctx := context.Background()
	r := NewReal(Options{})
	for round := range 50 {
		dir, log := rotateDir(t, 101, 1, 2, 3)
		var wg sync.WaitGroup
		var mu sync.Mutex
		n := 0
		start := make(chan struct{})
		for range 8 {
			wg.Go(func() {
				<-start
				rotated, err := r.RotateFile(ctx, log, 100, 4, false)
				if err != nil {
					t.Errorf("round %d: %v", round, err)
				}
				if rotated {
					mu.Lock()
					n++
					mu.Unlock()
				}
			})
		}
		close(start)
		wg.Wait()
		want := map[string]string{"hardn.log.1": "101 bytes", "hardn.log.2": "hardn.log.1", "hardn.log.3": "hardn.log.2", "hardn.log.4": "hardn.log.3"}
		if got := held(t, dir); n != 1 || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("round %d: %d rotations, %v, want one and %v", round, n, got, want)
		}
	}
}

// TestRotateFileLocked: a lock anyone who can read the file may take, here
// on a read-only descriptor, is waited for: held throughout, the file is
// not rotated and the error says so (ErrLocked); let go within the
// wait, it is rotated.
func TestRotateFileLocked(t *testing.T) {
	ctx := context.Background()
	r := NewReal(Options{})
	lock := func(log string) *os.File {
		t.Helper()
		f, err := os.OpenFile(log, os.O_RDONLY, 0)
		must(t, err)
		must(t, unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB))
		return f
	}

	dir, log := rotateDir(t, 101, 1)
	before := held(t, dir)
	f := lock(log)
	rotated, err := r.RotateFile(ctx, log, 100, 3, false)
	var pe *fs.PathError
	if rotated || !errors.Is(err, ErrLocked) || !errors.As(err, &pe) || pe.Path != log {
		t.Errorf("held: rotated %v, %v, want ErrLocked at the log", rotated, err)
	}
	if got := held(t, dir); fmt.Sprint(got) != fmt.Sprint(before) {
		t.Errorf("held: %v, was %v", got, before)
	}
	must(t, f.Close())

	dir, log = rotateDir(t, 101)
	f = lock(log)
	timer := time.AfterFunc(rotateLockWait/6, func() { _ = f.Close() })
	defer timer.Stop()
	if rotated, err := r.RotateFile(ctx, log, 100, 3, false); !rotated || err != nil {
		t.Errorf("let go: rotated %v, %v", rotated, err)
	}
	if got, want := held(t, dir), map[string]string{"hardn.log.1": "101 bytes"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("let go: %v, want %v", got, want)
	}
}
