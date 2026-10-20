package hold

import (
	"errors"
	"os"
	"os/exec"
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
// release; this test process survives each, sent to itself.
func TestSignals(t *testing.T) {
	if signal.Ignored(syscall.SIGINT) || signal.Ignored(syscall.SIGHUP) {
		t.Skip("this test process was started with SIGINT or SIGHUP ignored")
	}
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
		t.Error("release left SIGINT or SIGHUP ignored, as os/signal records it")
	}
}

// helperEnv names what TestHelperProcess does.
const helperEnv = "HARDN_TEST_HOLD_HELPER"

// TestHelperProcess is not a test: TestReleaseRestoresDefaults runs this
// test binary again with it alone, as a process whose signal disposition
// it checks from outside. It sends signals only to itself and starts only
// this binary again.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		t.Skip("helper process for TestReleaseRestoresDefaults")
	}
	self := func(sig syscall.Signal) {
		_ = syscall.Kill(os.Getpid(), sig)
		time.Sleep(time.Second)
		_, _ = os.Stdout.WriteString("survived\n")
		os.Exit(0)
	}
	child := func(mode string) {
		out, err := helper(mode).CombinedOutput()
		_, _ = os.Stdout.WriteString(strings.Join(strings.Fields("child: "+string(out)+" "+exitWords(err)), " ") + "\n")
	}
	switch mode {
	case "int", "hup": // the default, for reference
	case "released-int", "released-hup":
		Signals(nil)()
	case "notified-then-released": // a caller's NotifyContext before the hold
		_, stop := signal.NotifyContext(t.Context(), syscall.SIGINT)
		defer stop()
		Signals(nil)()
	case "child-during-hold":
		release := Signals(nil)
		child("int")
		release()
		os.Exit(0)
	case "child-after-release":
		Signals(nil)()
		child("int")
		os.Exit(0)
	case "ignored-from-the-start":
		signal.Ignore(syscall.SIGINT) // as nohup leaves it, for the child
		child("released-int")
		os.Exit(0)
	default:
		os.Exit(3)
	}
	if strings.HasSuffix(mode, "hup") {
		self(syscall.SIGHUP)
	}
	self(syscall.SIGINT)
}

func helper(mode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), helperEnv+"="+mode)
	return cmd
}

// exitWords is how a process ended: "killed by <signal>" or "exit <n>".
func exitWords(err error) string {
	var ee *exec.ExitError
	switch {
	case err == nil:
		return "exit 0"
	case !errors.As(err, &ee):
		return err.Error()
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return "killed by " + ws.Signal().String()
	}
	return "exit " + strings.TrimPrefix(ee.Error(), "exit status ")
}

// TestReleaseRestoresDefaults: what a process does with SIGINT and SIGHUP
// after a hold, seen from outside it, as the kernel delivers them: the
// default action again (os/signal's Notify then Stop alone would leave
// SIG_IGN, Go 1.27 runtime/signal_unix.go), for the process and for a
// command it starts afterwards; ignored during the hold, for a command
// started then; ignored still for a process started with SIGINT ignored; a
// caller's NotifyContext from before the hold no longer receives it.
func TestReleaseRestoresDefaults(t *testing.T) {
	if signal.Ignored(syscall.SIGINT) || signal.Ignored(syscall.SIGHUP) {
		t.Skip("this test process was started with SIGINT or SIGHUP ignored, and so would its helpers be")
	}
	for _, c := range []struct{ mode, want string }{
		{"int", "killed by interrupt"},
		{"released-int", "killed by interrupt"},
		{"released-hup", "killed by hangup"},
		{"notified-then-released", "killed by interrupt"},
		{"child-during-hold", "child: survived exit 0\nexit 0"},
		{"child-after-release", "child: killed by interrupt\nexit 0"},
		{"ignored-from-the-start", "child: survived exit 0\nexit 0"},
	} {
		t.Run(c.mode, func(t *testing.T) {
			t.Parallel()
			out, err := helper(c.mode).Output()
			got := strings.TrimSpace(strings.TrimSpace(string(out)) + "\n" + exitWords(err))
			if got != c.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}
