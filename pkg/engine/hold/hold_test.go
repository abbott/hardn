package hold

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hangup"
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

// TestSignals: SIGINT ignored, and SIGHUP and SIGTERM caught and logged,
// until release; this test process survives each, sent to itself.
// SignalsAs names its lines as asked: cmd/hardn's and p2drive's holds keep
// their words (next/p3-engine-boot).
func TestSignals(t *testing.T) {
	if signal.Ignored(syscall.SIGINT) || signal.Ignored(syscall.SIGHUP) {
		t.Skip("this test process was started with SIGINT or SIGHUP ignored")
	}
	for _, tc := range []struct {
		name string
		hold func(log *syncBuffer) func()
		as   string
	}{
		{"Signals", func(log *syncBuffer) func() { return Signals(log) }, "hardn"},
		{"SignalsAs", func(log *syncBuffer) func() { return SignalsAs("hardn rollback", log) }, "hardn rollback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log syncBuffer
			release := tc.hold(&log)
			if !signal.Ignored(syscall.SIGINT) || signal.Ignored(syscall.SIGHUP) {
				release()
				t.Fatal("SIGINT is not ignored, or SIGHUP is")
			}
			for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM} {
				if err := syscall.Kill(os.Getpid(), sig); err != nil {
					release()
					t.Fatal(err)
				}
			}
			deadline := time.Now().Add(2 * time.Second)
			for strings.Count(log.String(), "\n") < 2 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			release()
			got := strings.Split(strings.TrimSuffix(log.String(), "\n"), "\n")
			slices.Sort(got)
			if want := []string{tc.as + ": SIGHUP received; the rollback finishes first", tc.as + ": SIGTERM received; the rollback finishes first"}; !slices.Equal(got, want) {
				t.Errorf("log %q, want %q", got, want)
			}
			if signal.Ignored(syscall.SIGINT) || signal.Ignored(syscall.SIGHUP) {
				t.Error("release left SIGINT or SIGHUP ignored, as os/signal records it")
			}
		})
	}
}

// markLog records each line with how many times the terminal had been
// marked gone (marks) as it was written.
type markLog struct {
	syncBuffer
	marks *atomic.Int32
}

func (m *markLog) Write(p []byte) (int, error) {
	return m.syncBuffer.Write([]byte(fmt.Sprintf("%d %s", m.marks.Load(), p)))
}

// TestHangupMarksTerminalGone: a hangup during the hold marks hardn's
// terminal gone (terminalGone, hangup.Seen, counted here) before its line
// is logged, so that the line, where the log writes to the console, is
// dropped rather than blocked on; a SIGTERM marks nothing.
func TestHangupMarksTerminalGone(t *testing.T) {
	if reflect.ValueOf(terminalGone).Pointer() != reflect.ValueOf(hangup.Seen).Pointer() {
		t.Error("terminalGone is not hangup.Seen")
	}
	was := terminalGone
	defer func() { terminalGone = was }()
	var marks atomic.Int32
	terminalGone = func() { marks.Add(1) }
	log := &markLog{marks: &marks}
	release := Signals(log)
	lines := func(n int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for strings.Count(log.String(), "\n") < n && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
	}
	relayed(t, syscall.SIGTERM)
	lines(1)
	relayed(t, syscall.SIGHUP)
	lines(2)
	release()
	if want := "0 hardn: SIGTERM received; the rollback finishes first\n1 hardn: SIGHUP received; the rollback finishes first\n"; log.String() != want {
		t.Errorf("log, each line after the marks so far:\n%s\nwant:\n%s", log.String(), want)
	}
	if n := marks.Load(); n != 1 {
		t.Errorf("marked gone %d times, want once", n)
	}
}

// TestHangupReachesCaller: a channel registered for SIGHUP before the hold
// (as cmd/hardn's screens register one around an apply) receives a hangup
// sent during the hold, and one sent after its release; the hold does not
// end its registration, as it ends SIGINT's.
func TestHangupReachesCaller(t *testing.T) {
	if signal.Ignored(syscall.SIGHUP) {
		t.Skip("this test process was started with SIGHUP ignored")
	}
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)
	defer signal.Stop(c)
	hangup := func(when string) {
		t.Helper()
		if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
			t.Fatal(err)
		}
		select {
		case <-c:
		case <-time.After(5 * time.Second):
			t.Errorf("a hangup %s did not reach the caller's channel", when)
		}
	}
	release := Signals(nil)
	hangup("during the hold")
	release()
	hangup("after its release")
}

// gateLog is a log whose first write waits until open is closed,
// entered closed as it begins: the hold's goroutine busy logging.
type gateLog struct {
	syncBuffer
	entered, open chan struct{}
	once          sync.Once
}

func newGateLog() *gateLog { return &gateLog{entered: make(chan struct{}), open: make(chan struct{})} }

func (g *gateLog) Write(p []byte) (int, error) {
	first := false
	g.once.Do(func() { first = true })
	if first {
		close(g.entered)
		<-g.open
	}
	return g.syncBuffer.Write(p)
}

// relayed sends sig to this process and waits until os/signal has relayed
// it (to a probe registered meanwhile).
func relayed(t *testing.T, sig syscall.Signal) {
	t.Helper()
	probe := make(chan os.Signal, 1)
	signal.Notify(probe, sig)
	defer signal.Stop(probe)
	if err := syscall.Kill(os.Getpid(), sig); err != nil {
		t.Fatal(err)
	}
	select {
	case <-probe:
	case <-time.After(5 * time.Second):
		t.Fatalf("%v was not relayed", sig)
	}
}

// TestSignalsWaitWhileLogging: while the hold logs one signal, a hangup
// and a SIGTERM both wait for it, and each is logged: the hold's channel
// has room for both.
func TestSignalsWaitWhileLogging(t *testing.T) {
	log := newGateLog()
	release := Signals(log)
	relayed(t, syscall.SIGTERM)
	<-log.entered
	relayed(t, syscall.SIGHUP)
	relayed(t, syscall.SIGTERM)
	close(log.open)
	deadline := time.Now().Add(2 * time.Second)
	for strings.Count(log.String(), "\n") < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	release()
	if n := strings.Count(log.String(), "\n"); n != 3 {
		t.Errorf("%d lines, want 3:\n%s", n, log.String())
	}
}

// TestReleaseWaitsForLog: release returns only once the line being logged
// is written, so that no line of the hold's comes after it.
func TestReleaseWaitsForLog(t *testing.T) {
	log := newGateLog()
	release := Signals(log)
	relayed(t, syscall.SIGTERM)
	<-log.entered
	returned := make(chan struct{})
	go func() { release(); close(returned) }()
	select {
	case <-returned:
		t.Error("release returned while the hold was still logging")
	case <-time.After(200 * time.Millisecond):
	}
	close(log.open)
	<-returned
	if log.String() == "" {
		t.Error("no line")
	}
}

// commandGroup runs this test binary through host.Real, as the rollback
// runs its commands, and returns the process group it ran in.
func commandGroup() (int, error) {
	self, err := os.Executable()
	if err != nil {
		return 0, err
	}
	res, err := host.NewReal(host.Options{}).Run(context.Background(), self, "-test.run=^TestHelperProcess$", "-test.count=1", "--", "pgrp")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(res.Stdout)))
	if err != nil {
		return 0, fmt.Errorf("the command said %q: %w", res.Combined, err)
	}
	return n, nil
}

// TestReleaseGivesUpSessions: a command host.Real starts during the hold
// runs outside this process's group (a session of its own), and one it
// starts after the release in it again, as before the hold; so it does
// when the release returns early, SIGINT ignored from the start
// (TestReleaseRestoresDefaults' ignored-from-the-start).
func TestReleaseGivesUpSessions(t *testing.T) {
	ours := syscall.Getpgrp()
	release := Signals(nil)
	during, err := commandGroup()
	release()
	if err != nil || during == ours {
		t.Errorf("during the hold: process group %d (%v), want one not this process's, %d", during, err, ours)
	}
	if after, err := commandGroup(); err != nil || after != ours {
		t.Errorf("after the release: process group %d (%v), want this process's, %d", after, err, ours)
	}
}

// helperEnv names what TestHelperProcess does, and markEnv the file its
// group-hangup modes' command creates as it runs.
const (
	helperEnv = "HARDN_TEST_HOLD_HELPER"
	markEnv   = "HARDN_TEST_HOLD_MARK"
)

// TestHelperProcess is not a test: TestReleaseRestoresDefaults runs this
// test binary again with it alone, as a process whose signal disposition
// it checks from outside. It sends signals only to itself, starts only
// this binary again and sleep, and is sent signals only by its parent.
func TestHelperProcess(t *testing.T) {
	if flag.Arg(0) == "pgrp" { // commandGroup's, through host.Real, which passes no environment
		_, _ = os.Stdout.WriteString(strconv.Itoa(syscall.Getpgrp()) + "\n")
		os.Exit(0)
	}
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
	case "notified-hup-then-released": // as cmd/hardn's screens, around an apply
		_, stop := signal.NotifyContext(t.Context(), syscall.SIGHUP)
		Signals(nil)()
		stop()
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
		signal.Ignore(syscall.SIGINT) // as a non-interactive shell leaves it for a job it starts with &, for the children
		child("released-int")
		child("sessions-after-release")
		os.Exit(0)
	case "sessions-after-release": // release's early return when SIGINT was ignored from the start
		Signals(nil)()
		group, err := commandGroup()
		words := map[bool]string{true: "in this group", false: "not in this group"}
		_, _ = os.Stdout.WriteString(words[err == nil && group == syscall.Getpgrp()] + "\n")
		os.Exit(0)
	case "hup-ignored-from-the-start":
		signal.Ignore(syscall.SIGHUP) // as nohup leaves it, for the children
		child("released-hup")
		child("notified-hup-then-released")
		os.Exit(0)
	case "group-hangup-caller", "group-hangup-no-caller": // the parent sends SIGHUP to this process's group
		var seen chan os.Signal
		if mode == "group-hangup-caller" {
			seen = make(chan os.Signal, 1)
			signal.Notify(seen, syscall.SIGHUP) // as cmd/hardn's screens, around an apply
		}
		var log syncBuffer
		release := SignalsAs("hardn rollback", &log) // as `hardn rollback`, which registers nothing before
		// The command marks that it runs, then is sleep (exec).
		_, err := host.NewReal(host.Options{}).Run(context.Background(), "sh", "-c", `: > "$1" && exec sleep 3`, "sh", os.Getenv(markEnv))
		release()
		out := "the command: " + exitWords(err) + "\n"
		if seen != nil {
			words := map[bool]string{true: "seen", false: "not seen"}
			select {
			case <-seen:
				out += "the caller's channel: " + words[true] + "\n"
			default:
				out += "the caller's channel: " + words[false] + "\n"
			}
		}
		_, _ = os.Stdout.WriteString(out + log.String())
		os.Exit(0)
	default:
		os.Exit(3)
	}
	if strings.HasSuffix(mode, "hup") || strings.Contains(mode, "-hup-") {
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
// SIGINT SIG_IGN, Go 1.27 runtime/signal_unix.go), for the process and for
// a command it starts afterwards; SIGINT ignored during the hold, for a
// command started then; ignored still for a process started with SIGINT
// ignored; a caller's NotifyContext of SIGINT from before the hold no
// longer receives it. SIGHUP ignored from the start (nohup) stays ignored
// after a hold, with a caller's NotifyContext of it around the hold too.
// After the release a command host.Real starts is in this process's group
// again, also where the release returns early, SIGINT ignored from the
// start.
func TestReleaseRestoresDefaults(t *testing.T) {
	if signal.Ignored(syscall.SIGINT) || signal.Ignored(syscall.SIGHUP) {
		t.Skip("this test process was started with SIGINT or SIGHUP ignored, and so would its helpers be")
	}
	for _, c := range []struct{ mode, want string }{
		{"int", "killed by interrupt"},
		{"released-int", "killed by interrupt"},
		{"released-hup", "killed by hangup"},
		{"notified-then-released", "killed by interrupt"},
		{"notified-hup-then-released", "killed by hangup"},
		{"child-during-hold", "child: survived exit 0\nexit 0"},
		{"child-after-release", "child: killed by interrupt\nexit 0"},
		{"ignored-from-the-start", "child: survived exit 0\nchild: in this group exit 0\nexit 0"},
		{"sessions-after-release", "in this group\nexit 0"},
		{"hup-ignored-from-the-start", "child: survived exit 0\nchild: survived exit 0\nexit 0"},
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

// TestGroupHangupDuringHold: a hangup sent to hardn's whole process group
// during the hold, as a shell, sudo or the kernel sends it when the
// session drops, while the rollback runs a command through host.Real (a
// shell that marks it runs, then is sleep): hardn survives it and logs
// it, and the command, in a session of its own (host.OwnSessions), runs
// to its end; with a channel registered for SIGHUP before the hold (as
// cmd/hardn's screens register one), that channel receives it, and with
// none (as `hardn rollback`, its --boot and p2drive) the same holds. The
// helper runs in a process group of its own, the one signalled, once the
// command's mark is there.
func TestGroupHangupDuringHold(t *testing.T) {
	if signal.Ignored(syscall.SIGHUP) {
		t.Skip("this test process was started with SIGHUP ignored, and so would its helper be")
	}
	const logged = "hardn rollback: SIGHUP received; the rollback finishes first"
	for _, tc := range []struct{ mode, want string }{
		{"group-hangup-caller", "the command: exit 0\nthe caller's channel: seen\n" + logged + "\nexit 0"},
		{"group-hangup-no-caller", "the command: exit 0\n" + logged + "\nexit 0"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			mark := filepath.Join(t.TempDir(), "running")
			cmd := helper(tc.mode)
			cmd.Env = append(cmd.Env, markEnv+"="+mark)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			var out strings.Builder
			cmd.Stdout = &out
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
				if _, err := os.Stat(mark); err == nil {
					break
				} else if time.Now().After(deadline) {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					t.Fatalf("the command did not start: %v; output:\n%s", err, out.String())
				}
			}
			if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP); err != nil {
				t.Error(err)
			}
			werr := cmd.Wait()
			if got := strings.TrimSpace(out.String()) + "\n" + exitWords(werr); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}
