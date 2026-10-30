package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// scanLines is ScanLines on a goroutine, so that a test fails where it would
// hang: every refusal below must come without blocking.
func scanLines(t *testing.T, r *Real, ctx context.Context, name string, yield func(Line) bool) (fs.FileInfo, error) {
	t.Helper()
	type result struct {
		fi  fs.FileInfo
		err error
	}
	done := make(chan result, 1)
	go func() {
		fi, err := r.ScanLines(ctx, name, yield)
		done <- result{fi, err}
	}()
	select {
	case res := <-done:
		return res.fi, res.err
	case <-time.After(10 * time.Second):
		t.Fatalf("ScanLines(%s) did not return", name)
		return nil, nil
	}
}

// TestScanLinesRefusesWithoutBlocking runs on real files in t.TempDir(): a
// FIFO with no writer, which an open or a read without O_NONBLOCK would hang
// on for good, and a device, each refused with fs.ErrInvalid and no line; a
// file with no permission, for a user other than root, with its reason.
func TestScanLinesRefusesWithoutBlocking(t *testing.T) {
	ctx := context.Background()
	r := NewReal(Options{})
	noLine := func(Line) bool { t.Error("a line"); return true }

	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, err := scanLines(t, r, ctx, fifo, noLine); !errors.Is(err, fs.ErrInvalid) || fi != nil {
		t.Errorf("FIFO: fi = %v, err = %v, want fs.ErrInvalid", fi, err)
	}
	if fi, err := scanLines(t, r, ctx, "/dev/null", noLine); !errors.Is(err, fs.ErrInvalid) || fi != nil {
		t.Errorf("device: fi = %v, err = %v, want fs.ErrInvalid", fi, err)
	}
	if os.Geteuid() != 0 {
		closed := filepath.Join(t.TempDir(), "closed")
		if err := os.WriteFile(closed, []byte("x\n"), 0o000); err != nil {
			t.Fatal(err)
		}
		if _, err := scanLines(t, r, ctx, closed, noLine); !errors.Is(err, fs.ErrPermission) {
			t.Errorf("mode 0000: err = %v, want fs.ErrPermission", err)
		}
	}
}

// TestScanLinesNoRunLog: a scan is no command and writes no line, on a host
// that has a run log.
func TestScanLinesNoRunLog(t *testing.T) {
	var log bytes.Buffer
	r := NewReal(Options{RunLog: &log})
	name := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(name, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ScanLines(context.Background(), name, func(Line) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if log.Len() != 0 {
		t.Errorf("run log: %q", log.String())
	}
}

// TestScanLinesStopsAtTheSize: lines added after the open are not read, so
// the size and the lines describe one moment and a writer that keeps
// appending does not keep the read going.
func TestScanLinesStopsAtTheSize(t *testing.T) {
	name := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(name, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var got []string
	fi, err := NewReal(Options{}).ScanLines(context.Background(), name, func(l Line) bool {
		if len(got) == 0 {
			if _, err := f.WriteString("three\n"); err != nil {
				t.Error(err)
			}
		}
		got = append(got, string(l.Text))
		return true
	})
	if err != nil || fi.Size() != 8 || strings.Join(got, ",") != "one,two" {
		t.Errorf("lines %q, size %d, err %v", got, fi.Size(), err)
	}
}

// TestScanLinesContextDuringTheRead: ctx is asked between reads, not only on
// entry: done during the read, the rest is not read.
func TestScanLinesContextDuringTheRead(t *testing.T) {
	name := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(name, bytes.Repeat([]byte("a line\n"), 100_000), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines := 0
	fi, err := NewReal(Options{}).ScanLines(ctx, name, func(Line) bool {
		lines++
		cancel()
		return true
	})
	if !errors.Is(err, context.Canceled) || fi == nil {
		t.Errorf("fi = %v, err = %v, want context.Canceled and the FileInfo", fi, err)
	}
	if lines == 0 || lines > scanChunk/len("a line\n")+1 {
		t.Errorf("%d lines after the context was done", lines)
	}
}

// countingReader counts what is read from it, and fails after the end.
type countingReader struct {
	r    io.Reader
	read int
	err  error
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += n
	if err == io.EOF && c.err != nil {
		err = c.err
	}
	return n, err
}

// TestScanReaderIsBounded: a large file with no newline, as a binary file is
// (wtmp, 34 to 36 MB on the VMs, vm-runs/s1/08-lslogins.log), is given up on
// at its first MaxLine bytes when yield returns false, so "can this file be
// read and does it hold anything" costs that; read to its end it is one cut
// line, however long.
func TestScanReaderIsBounded(t *testing.T) {
	ctx := context.Background()
	const size = 8 << 20
	in := &countingReader{r: strings.NewReader(strings.Repeat("x", size))}
	lines := 0
	err := ScanReader(ctx, in, func(l Line) bool {
		lines++
		if !l.Cut || len(l.Text) != MaxLine {
			t.Errorf("cut %t, %d bytes", l.Cut, len(l.Text))
		}
		return false
	})
	if err != nil || lines != 1 || in.read > MaxLine+2*scanChunk {
		t.Errorf("lines %d, read %d of %d, err %v", lines, in.read, size, err)
	}
	lines = 0
	in = &countingReader{r: strings.NewReader(strings.Repeat("x", size))}
	if err := ScanReader(ctx, in, func(Line) bool { lines++; return true }); err != nil || lines != 1 || in.read != size {
		t.Errorf("to the end: lines %d, read %d, err %v", lines, in.read, err)
	}
}

// TestScanReaderError: a read that fails is returned, the partial line it
// ended on not given as a line.
func TestScanReaderError(t *testing.T) {
	boom := errors.New("input/output error")
	var got []string
	err := ScanReader(context.Background(), &countingReader{r: strings.NewReader("one\ntw"), err: boom}, func(l Line) bool {
		got = append(got, string(l.Text))
		return true
	})
	if !errors.Is(err, boom) || strings.Join(got, ",") != "one" {
		t.Errorf("lines %q, err %v", got, err)
	}
}
