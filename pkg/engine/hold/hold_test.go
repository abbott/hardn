package hold

import (
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// syncBuffer is a log the hold's goroutine writes while the test reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestSignals: SIGINT and SIGHUP ignored and SIGTERM caught and logged until
// release; this test process survives each, sent to itself, and release
// gives the defaults back.
func TestSignals(t *testing.T) {
	var log syncBuffer
	release := Signals(&log)
	if !signal.Ignored(syscall.SIGINT) || !signal.Ignored(syscall.SIGHUP) {
		release()
		t.Fatal("SIGINT and SIGHUP are not ignored")
	}
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM} {
		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			release()
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(log.String(), "SIGTERM received") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	release()
	if got := log.String(); got != "hardn: SIGTERM received; the rollback finishes first\n" {
		t.Errorf("log %q", got)
	}
	if signal.Ignored(syscall.SIGINT) || signal.Ignored(syscall.SIGHUP) {
		t.Error("release left SIGINT or SIGHUP ignored")
	}
}
