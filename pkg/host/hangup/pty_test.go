//go:build darwin || linux

package hangup

import (
	"bytes"
	"testing"
	"time"
)

// TestWriterOnAnUnreadTerminal: hardn's case under sudo-rs once the SSH
// session has dropped, on a real pseudo-terminal: its master never read,
// writes to its slave through a writer block, in the kernel, once the
// pty's buffer is full; the hangup seen, the blocked Write returns at
// once, and a later one too.
func TestWriterOnAnUnreadTerminal(t *testing.T) {
	_, slave := openPTY(t)
	if err := slave.SetWriteDeadline(time.Time{}); err == nil {
		t.Fatal("the slave is in Go's poller, its writes parked there; hardn's stdout blocks in the kernel")
	}
	term := New()
	w := term.Writer(slave)
	chunk := bytes.Repeat([]byte("x"), 1024)
	for i := 0; ; i++ {
		if i == 4096 {
			t.Fatal("4 MiB written to a pty nothing reads, and no write blocked")
		}
		returned := make(chan error, 1)
		go func() { _, err := w.Write(chunk); returned <- err }()
		select {
		case err := <-returned:
			if err != nil {
				t.Fatalf("write %d, before any blocked: %v", i, err)
			}
			continue
		case <-time.After(time.Second):
		}
		// Blocked: the buffer is full.
		t.Logf("write %d blocked, %d KiB written before it", i, i)
		term.Seen()
		select {
		case err := <-returned:
			if err != nil {
				t.Errorf("the Write given up on: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("write %d, blocked on the pty, was not given up on after the hangup", i)
		}
		break
	}
	later := make(chan struct{})
	go func() { _, _ = w.Write(chunk); close(later) }()
	select {
	case <-later:
	case <-time.After(5 * time.Second):
		t.Fatal("a Write after the hangup blocked")
	}
}
