package hangup

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// shortWriter writes n bytes of each p and fails with err.
type shortWriter struct {
	n   int
	err error
}

func (s shortWriter) Write([]byte) (int, error) { return s.n, s.err }

// TestWriter: before the hangup a writer writes to w, in order, and
// returns what w's Write returned, a failure included; after it, each
// Write, many of them, a new writer's too, writes nothing and returns
// len(p) and no error. A second Seen is harmless; another Terminal's
// writers write on.
func TestWriter(t *testing.T) {
	term := New()
	var out bytes.Buffer
	w := term.Writer(&out)
	for _, s := range []string{"one ", "two"} {
		if n, err := w.Write([]byte(s)); n != len(s) || err != nil {
			t.Fatalf("Write(%q) = %d, %v before the hangup", s, n, err)
		}
	}
	if n, err := term.Writer(shortWriter{2, io.ErrShortWrite}).Write([]byte("abc")); n != 2 || err != io.ErrShortWrite {
		t.Errorf("a failing w's Write = %d, %v; want 2, %v", n, err, io.ErrShortWrite)
	}
	term.Seen()
	term.Seen()
	// Many: a Write that takes its turn as the hangup is seen (the two
	// ready together, select picks either) writes nothing either.
	for i := range 200 {
		for _, ww := range []io.Writer{w, term.Writer(&out)} {
			if n, err := ww.Write([]byte("lost")); n != 4 || err != nil {
				t.Fatalf("Write %d after the hangup = %d, %v; want 4, nil", i, n, err)
			}
		}
	}
	if got := out.String(); got != "one two" {
		t.Errorf("w got %q, want %q: nothing after the hangup", got, "one two")
	}
	other := New()
	var kept bytes.Buffer
	if _, err := other.Writer(&kept).Write([]byte("kept")); err != nil || kept.String() != "kept" {
		t.Errorf("another Terminal's writer wrote %q, %v", kept.String(), err)
	}
}

// blocker is a w whose Write signals entered, waits for release, and then
// records p as it is by then.
type blocker struct {
	entered, release chan struct{}
	mu               sync.Mutex
	got              []string
}

func newBlocker() *blocker {
	return &blocker{entered: make(chan struct{}, 8), release: make(chan struct{})}
}

func (b *blocker) Write(p []byte) (int, error) {
	b.entered <- struct{}{}
	<-b.release
	b.mu.Lock()
	defer b.mu.Unlock()
	b.got = append(b.got, string(p))
	return len(p), nil
}

func (b *blocker) writes() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.got...)
}

// TestWriterGivesUpOnABlockedWrite: a Write blocked on w (a terminal
// nothing reads) waits, with no hangup; once the hangup is seen it
// returns len(p) and no error at once, w's Write still blocked, and so
// does a later Write, which never reaches w. w was given a copy of p:
// the caller's reuse of p after its Write returned does not change what
// w writes once it finishes.
func TestWriterGivesUpOnABlockedWrite(t *testing.T) {
	term := New()
	b := newBlocker()
	w := term.Writer(b)
	p := []byte("stuck")
	type result struct {
		n   int
		err error
	}
	returned := make(chan result, 1)
	go func() { n, err := w.Write(p); returned <- result{n, err} }()
	<-b.entered
	select {
	case r := <-returned:
		t.Fatalf("Write returned %+v while w's Write was blocked, with no hangup", r)
	case <-time.After(100 * time.Millisecond):
	}
	term.Seen()
	select {
	case r := <-returned:
		if r.n != len(p) || r.err != nil {
			t.Errorf("the Write given up on = %d, %v; want %d, nil", r.n, r.err, len(p))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the Write blocked on w was not given up on after the hangup")
	}
	copy(p, "XXXXX")
	later := make(chan struct{})
	go func() { _, _ = w.Write([]byte("later")); close(later) }()
	select {
	case <-later:
	case <-time.After(5 * time.Second):
		t.Fatal("a Write after the hangup waited for w's blocked Write")
	}
	close(b.release)
	deadline := time.Now().Add(5 * time.Second)
	for len(b.writes()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := b.writes(); len(got) != 1 || got[0] != "stuck" {
		t.Errorf("w wrote %q, want [\"stuck\"]: the one Write, a copy of p", got)
	}
}

// overlapWriter fails the test when two Writes to it overlap.
type overlapWriter struct {
	t       *testing.T
	in      atomic.Int32
	mu      sync.Mutex
	written strings.Builder
}

func (o *overlapWriter) Write(p []byte) (int, error) {
	if o.in.Add(1) != 1 {
		o.t.Error("two Writes to w overlapped")
	}
	defer o.in.Add(-1)
	time.Sleep(time.Millisecond)
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.written.Write(p)
}

// TestWriterOneAtATime: Writes from many goroutines reach w one at a
// time, each whole, none lost.
func TestWriterOneAtATime(t *testing.T) {
	o := &overlapWriter{t: t}
	w := New().Writer(o)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 10 {
				if n, err := w.Write([]byte("0123456789")); n != 10 || err != nil {
					t.Errorf("Write = %d, %v", n, err)
				}
			}
		})
	}
	wg.Wait()
	if got := o.written.String(); len(got) != 800 || strings.Count(got, "0123456789") != 80 {
		t.Errorf("w got %d bytes, %d whole writes; want 800, 80", len(got), strings.Count(got, "0123456789"))
	}
}

// TestStd: hardn's own Terminal, the package's Seen and Writer, a fresh
// one for the test.
func TestStd(t *testing.T) {
	was := std
	std = New()
	t.Cleanup(func() { std = was })
	var out bytes.Buffer
	w := Writer(&out)
	if _, err := w.Write([]byte("before")); err != nil {
		t.Fatal(err)
	}
	Seen()
	if n, err := w.Write([]byte("after")); n != 5 || err != nil {
		t.Errorf("Write after Seen = %d, %v", n, err)
	}
	if got := out.String(); got != "before" {
		t.Errorf("w got %q, want %q", got, "before")
	}
}
