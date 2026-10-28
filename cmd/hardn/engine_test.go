package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine/hold"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// samePointer reports whether a and b are the same function or map.
func samePointer(a, b any) bool { return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer() }

// TestEngineOptions holds the engine's options, assembled once for -g and
// the DNS screen, each field to its source: revert's options as hardn
// rollback and hardn rollback --boot build theirs, with the log file
// alone; the session as hardn confirm reads it; hardn.yml's backup
// settings; hardn's pid; detection and the hold the seams' (the engine's
// own in production, TestEngineSeamsAreReal); no Progress. Without
// hardn's own path Hardn is empty, and a session not established is kept
// as ReadSession leaves it. Nothing real is read: the host is the fake.
func TestEngineOptions(t *testing.T) {
	h, cfg := newDNSHost(t, "debian13", dnsTestList)
	useDNSHost(t, h)
	cfg.BackupPath, cfg.EnableBackups = "/srv/backups/hardn", false
	// alice's SSH session: sshd, its session, a shell, sudo, hardn.
	revertProc(h.System, 900, 1, "sshd")
	revertProc(h.System, 3970, 900, "sshd-session")
	revertProc(h.System, 3980, 3970, "bash", "SSH_CONNECTION=192.0.2.10 50123 192.0.2.20 2230")
	revertProc(h.System, 3990, 3980, "sudo")
	revertProc(h.System, dnsTestPID, 3990, "hardn")
	ctx := context.Background()

	o := engineOptions(ctx, h, cfg)
	if _, ok := o.Revert.Log.(revertFileLog); !ok {
		t.Errorf("Revert.Log is %T, want revertFileLog: the log file alone", o.Revert.Log)
	}
	if o.Revert.Now == nil || !o.Revert.Now().Equal(dnsTestNow) {
		t.Error("Revert.Now is not revertNow")
	}
	if !samePointer(o.Revert.Plans, revertPlans) {
		t.Error("Revert.Plans is not revertPlans, the registry hardn rollback undoes the journal with")
	}
	if o.Revert.Hardn != revertTestHardn {
		t.Errorf("Revert.Hardn = %q, want revertExecutable's %q", o.Revert.Hardn, revertTestHardn)
	}
	want, err := revert.ReadSession(ctx, h, revertGetenv, dnsTestPID)
	if err != nil || !want.Remote || want.Conn == "" {
		t.Fatalf("the fixture's session: %+v, %v", want, err)
	}
	if o.Session != want {
		t.Errorf("Session = %+v, want revert.ReadSession's %+v", o.Session, want)
	}
	if o.Backups != (backup.Settings{Path: "/srv/backups/hardn", Enabled: false}) {
		t.Errorf("Backups = %+v, want hardn.yml's backupPath and enableBackups", o.Backups)
	}
	if o.PID != dnsTestPID {
		t.Errorf("PID = %d, want revertPID's %d", o.PID, dnsTestPID)
	}
	if !samePointer(o.Detect, engineDetect) || !samePointer(o.Hold, engineHold) {
		t.Error("Detect and Hold are not the seams'")
	}
	if o.Progress != nil || o.Timeout != 0 || o.Margin != 0 || o.JournalTimeout != 0 || o.Revert.Dir != "" || o.Revert.RunDir != "" {
		t.Errorf("a field is set that is the engine's or the caller's: %+v", o)
	}

	revertExecutable = func() (string, error) { return "", errors.New("readlink /proc/self/exe: no such file") }
	revertPID = func() int { return 4321 } // no such process: the session not established
	o = engineOptions(ctx, h, cfg)
	if o.Revert.Hardn != "" {
		t.Errorf("Revert.Hardn = %q without hardn's own path", o.Revert.Hardn)
	}
	if !o.Session.Remote || o.Session.Conn != "" {
		t.Errorf("Session = %+v, want ReadSession's for a session not established: Remote, no connection", o.Session)
	}
}

// TestEngineSeamsAreReal: in production the engine's host is host.Real,
// detection is detect.Detect, the hold is the engine's own (nil), the
// signals' context is a live one its stop ends, the screens read the one
// line reader every prompt shares and write to stdout, and a hangup
// during a screen's apply ends hardn by endByHangup.
// TestEngineSignals holds the signals.
func TestEngineSeamsAreReal(t *testing.T) {
	if _, ok := engineHost().(*host.Real); !ok {
		t.Error("engineHost is not host.Real")
	}
	if !samePointer(engineDetect, detect.Detect) {
		t.Error("engineDetect is not detect.Detect")
	}
	if engineHold != nil {
		t.Error("engineHold is set: the engine's own hold is nil")
	}
	if !samePointer(engineInput, prompt.Stdin) {
		t.Error("engineInput is not prompt.Stdin")
	}
	if engineStdout != os.Stdout {
		t.Error("engineStdout is not os.Stdout")
	}
	if !samePointer(endHangup, endByHangup) {
		t.Error("endHangup is not endByHangup")
	}
	ctx, stop := engineSignals(context.Background())
	if ctx.Err() != nil {
		t.Error("engineSignals' context is done before any signal")
	}
	stop()
	if ctx.Err() == nil {
		t.Error("engineSignals' stop does not end its context")
	}
}

// TestEngineSignals: the signals that cancel an apply are Ctrl-C, SIGTERM
// and a hangup (engine.md §2.14), each, sent to this test process as the
// kernel delivers it, caught by engineSignals' context, which it cancels:
// one left out would end the test process by its default action.
func TestEngineSignals(t *testing.T) {
	if want := []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}; !slices.Equal(engineSignalSet, want) {
		t.Fatalf("engineSignalSet = %v, want %v", engineSignalSet, want)
	}
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		ctx, stop := engineSignals(context.Background())
		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			stop()
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			t.Errorf("%v did not cancel engineSignals' context", sig)
		}
		stop()
	}
}

// TestScreenApplyContext: the screens' ApplyContext is cancelled by
// Ctrl-C, SIGTERM and a hangup, each sent to this test process as the
// kernel delivers it, as engineSignals' is; then the engine's hold around
// the rollback (hold.Signals), taken and released, as the engine does
// after a cancelled apply; then its stop. After the hangup the stop ends
// hardn (endHangup, recorded here), once, though the hold ended every
// registration of SIGHUP; so it does after a Ctrl-C then a hangup (which
// a context's cause, naming the first, would miss), and after a hangup
// as engineSignals registers, before or after its registration: the
// screens' own channel is registered first. A hangup before it does not
// cancel the apply, which finishes. After Ctrl-C and SIGTERM alone, with
// no signal, and after a hangup during the hold, which ignores it, the
// stop does not end hardn. A second stop ends nothing more.
func TestScreenApplyContext(t *testing.T) {
	hangupWas, signalsWas := endHangup, engineSignals
	defer func() { endHangup, engineSignals = hangupWas, signalsWas }()
	// hangup sends SIGHUP to this process and waits until os/signal has
	// relayed it (to probe, registered for it meanwhile), which it does to
	// every channel then registered, under the lock the next registration
	// or hold waits for.
	hangup := func(t *testing.T) {
		t.Helper()
		probe := make(chan os.Signal, 1)
		signal.Notify(probe, syscall.SIGHUP)
		defer signal.Stop(probe)
		if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
			t.Fatal(err)
		}
		select {
		case <-probe:
		case <-time.After(5 * time.Second):
			t.Error("the hangup was not relayed")
		}
	}
	for _, tc := range []struct {
		name   string
		during []syscall.Signal // sent during the apply, before the hold
		made   string           // a hangup as engineSignals registers: "before" or "after" its registration
		inHold bool             // a hangup during the hold
		end    bool             // hardn ended, once
		cancel bool             // the apply's context cancelled before the stop
	}{
		{name: "no signal"},
		{name: "SIGINT", during: []syscall.Signal{syscall.SIGINT}, cancel: true},
		{name: "SIGTERM", during: []syscall.Signal{syscall.SIGTERM}, cancel: true},
		{name: "SIGHUP", during: []syscall.Signal{syscall.SIGHUP}, end: true, cancel: true},
		{name: "SIGINT then SIGHUP", during: []syscall.Signal{syscall.SIGINT, syscall.SIGHUP}, end: true, cancel: true},
		{name: "SIGHUP after engineSignals registers", made: "after", end: true, cancel: true},
		{name: "SIGHUP before engineSignals registers", made: "before", end: true},
		{name: "SIGHUP during the hold", inHold: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ended := 0
			endHangup = func() { ended++ }
			engineSignals = func(ctx context.Context) (context.Context, context.CancelFunc) {
				if tc.made == "before" {
					hangup(t)
				}
				c, stop := signalsWas(ctx)
				if tc.made == "after" {
					hangup(t)
				}
				return c, stop
			}
			ctx, stop := screenApplyContext(context.Background())
			defer stop()
			for _, sig := range tc.during {
				if sig == syscall.SIGHUP {
					hangup(t)
					continue
				}
				if err := syscall.Kill(os.Getpid(), sig); err != nil {
					t.Fatal(err)
				}
				select {
				case <-ctx.Done():
				case <-time.After(5 * time.Second):
					t.Errorf("%v did not cancel the context", sig)
				}
			}
			if tc.cancel {
				select {
				case <-ctx.Done():
				case <-time.After(5 * time.Second):
				}
			}
			if cancelled := ctx.Err() != nil; cancelled != tc.cancel {
				t.Errorf("the apply's context cancelled %t, want %t", cancelled, tc.cancel)
			}
			release := hold.Signals(nil)
			if tc.inHold {
				if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
					t.Fatal(err)
				}
			}
			release()
			stop()
			stop()
			if want := map[bool]int{true: 1}[tc.end]; ended != want {
				t.Errorf("hardn ended %d times, want %d", ended, want)
			}
			if ctx.Err() == nil {
				t.Error("the stop did not end the context")
			}
		})
	}
}

// hangupChildEnv names what TestEndByHangup's child does, and
// hangupLogEnv the log file it writes.
const (
	hangupChildEnv = "HARDN_TEST_HANGUP_CHILD"
	hangupLogEnv   = "HARDN_TEST_HANGUP_LOG"
)

// TestEndByHangup runs this test binary again as a child that does what
// a screen's apply does after a hangup, with the real endHangup: the
// screens' ApplyContext, SIGHUP sent to itself, the context cancelled,
// the engine's hold taken and released, the stop. The child dies of
// SIGHUP, the wait status a waiting parent (sudo, a shell) sees, its log
// file holding the line written first; so it does with another channel
// registered for SIGHUP as it ends (signal.Reset drops it). Where SIGHUP
// is ignored (as from nohup, which the child sets itself), and no hold
// restored it, it exits 130 (prompt.ExitInterrupted). After an apply
// with no hangup, the stop drops the screens' channel: a hangup then
// kills the child by Go's default, nothing logged. The child sends
// signals only to itself.
func TestEndByHangup(t *testing.T) {
	if mode := os.Getenv(hangupChildEnv); mode != "" {
		logging.InitLogging(os.Getenv(hangupLogEnv))
		if mode == "ignored" {
			signal.Ignore(syscall.SIGHUP)
		}
		ctx, stop := screenApplyContext(context.Background())
		if mode == "after" {
			stop() // no hangup during the apply: it returns
			_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
			time.Sleep(hangupWait)
			os.Exit(5)
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			os.Exit(3)
		}
		if mode != "ignored" {
			hold.Signals(nil)()
		}
		if mode == "registered" {
			signal.Notify(make(chan os.Signal, 1), syscall.SIGHUP)
		}
		stop() // never returns
		os.Exit(4)
	}
	for _, tc := range []struct {
		mode     string
		signaled bool // killed by SIGHUP; else the exit status 130
		logged   bool // the log's line written
	}{
		{"hangup", true, true},
		{"registered", true, true},
		{"ignored", false, true},
		{"after", true, false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "hardn.log")
			cmd := exec.Command(os.Args[0], "-test.run=^TestEndByHangup$", "-test.count=1")
			cmd.Env = append(os.Environ(), hangupChildEnv+"="+tc.mode, hangupLogEnv+"="+log)
			out, err := cmd.CombinedOutput()
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				t.Fatalf("the child: %v, want it ended by SIGHUP or 130; output:\n%s", err, out)
			}
			ws, _ := ee.Sys().(syscall.WaitStatus)
			if signaled := ws.Signaled() && ws.Signal() == syscall.SIGHUP; signaled != tc.signaled || !signaled && ws.ExitStatus() != prompt.ExitInterrupted {
				t.Errorf("the child: %v; want killed by SIGHUP %t, else exit status 130; output:\n%s", err, tc.signaled, out)
			}
			data, err := os.ReadFile(log)
			if want := map[bool]int{true: 1}[tc.logged]; err != nil || strings.Count(string(data), "INFO: hangup during the apply: hardn ends\n") != want {
				t.Errorf("the child's log (%v), want the line %d times:\n%s", err, want, data)
			}
		})
	}
}
