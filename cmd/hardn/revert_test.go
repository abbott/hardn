package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/logging"
)

// Synthetic values: web-01.example.net, 192.0.2.0/24, port 2230.
const (
	revertTestID    = "20261018T120000Z-1a2b3c4d"
	revertTestHardn = "/usr/local/bin/hardn"
)

var revertTestNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// revertSeams points this file's seams at a fake host with a change armed
// on systemd, an SSH session from a new connection on port 2230, and an exit
// that records its code; nothing on this machine is read or changed.
type revertSeams struct {
	f              *hostfake.System
	exit           int // -1: not called
	stderr         bytes.Buffer
	seen           bool // whether a person sees stderr
	held, released int  // revertHoldSignals's calls, and its release's
}

func newRevertSeams(t *testing.T, euid int) *revertSeams {
	t.Helper()
	s := &revertSeams{f: hostfake.New(host.Options{}), exit: -1}
	s.f.AddFile(revertTestHardn, []byte("ELF"), 0o755, 0, 0)
	s.f.AddDir("/var/lib", 0o755, 0, 0)
	argv := []string{"systemd-run", "--unit=hardn-revert-" + revertTestID, "--description=hardn: roll back change " + revertTestID + " unless confirmed",
		"--on-active=180s", "--timer-property=AccuracySec=1s", revertTestHardn, "rollback", revertTestID}
	s.f.Respond(hostfake.Response{}, argv[0], argv[1:]...)
	s.f.Respond(hostfake.Response{}, "systemctl", "stop", "hardn-revert-"+revertTestID+".timer")
	j := revert.Journal{ID: revertTestID, Risk: revert.RiskLockout, TimeoutSeconds: 180, Summary: "SSH port 22 to 2230 on web-01.example.net",
		Session: revert.Session{Remote: true, Conn: "192.0.2.10 50022 192.0.2.20 22"}}
	clock := func() time.Time { return revertTestNow }
	if _, err := revert.Arm(context.Background(), s.f, j, detect.Init{Kind: detect.Systemd, Running: detect.Yes}, revert.Options{Now: clock, Hardn: revertTestHardn}); err != nil {
		t.Fatal(err)
	}
	// The apply finishes with the applying connection's sshd-session (3970)
	// running; then alice connects again on 2230 (4100) and runs sudo
	// hardn, process 4000.
	revertProc(s.f, 900, 1, "sshd")
	revertProc(s.f, 3970, 900, "sshd-session")
	if _, err := revert.MarkApplied(context.Background(), s.f, revertTestID, revert.Options{Now: clock}); err != nil {
		t.Fatal(err)
	}
	revertProc(s.f, 4100, 900, "sshd-session")
	revertProc(s.f, 3980, 4100, "bash", "SSH_CONNECTION=192.0.2.10 50123 192.0.2.20 2230")
	revertProc(s.f, 3990, 3980, "sudo")
	revertProc(s.f, 4000, 3990, "hardn")

	euidWas, hostWas, getenvWas, pidWas, exitWas, nowWas, dryRunWas := revertEUID, revertHost, revertGetenv, revertPID, revertExit, revertNow, dryRun
	stderrWas, seenWas, holdWas := revertStderr, revertNoticeSeen, revertHoldSignals
	revertEUID = func() int { return euid }
	revertHost = func() host.System { return s.f }
	revertGetenv = func(string) string { return "" } // sudo reset it
	revertPID = func() int { return 4000 }
	revertExit = func(code int) { s.exit = code }
	revertNow = clock
	revertStderr = &s.stderr
	revertNoticeSeen = func() bool { return s.seen }
	revertHoldSignals = func() func() { s.held++; return func() { s.released++ } }
	s.seen = true
	logging.SetSilentMode(true)
	t.Cleanup(func() {
		revertEUID, revertHost, revertGetenv, revertPID, revertExit, revertNow, dryRun = euidWas, hostWas, getenvWas, pidWas, exitWas, nowWas, dryRunWas
		revertStderr, revertNoticeSeen, revertHoldSignals = stderrWas, seenWas, holdWas
		logging.SetSilentMode(false)
		rootCmd.SetArgs(nil)
	})
	return s
}

// revertProc adds process pid to the fake's /proc, as revert reads it.
func revertProc(f *hostfake.System, pid, ppid int, comm string, env ...string) {
	block := ""
	for _, kv := range env {
		block += kv + "\x00"
	}
	f.AddFile(fmt.Sprintf("/proc/%d/environ", pid), []byte(block), 0o400, 0, 0)
	f.AddFile(fmt.Sprintf("/proc/%d/stat", pid), []byte(fmt.Sprintf("%d (%s) S %d %d %d 34819 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 %d 0 0\n", pid, comm, ppid, pid, pid, pid*100)), 0o444, 0, 0)
}

func (s *revertSeams) has(name string) bool {
	_, ok := s.f.Entry(name)
	return ok
}

func executeHardn(t *testing.T, args ...string) {
	t.Helper()
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("hardn %v: %v", args, err)
	}
}

func TestRevertConfirmCommand(t *testing.T) {
	s := newRevertSeams(t, 0)
	executeHardn(t, "confirm")
	if s.exit != -1 || s.has(revert.DefaultDir+"/"+revert.PendingName) || !s.has(revert.DefaultDir+"/"+revert.ConfirmedName) {
		t.Fatalf("exit %d; pending %v, confirmed %v", s.exit, s.has(revert.DefaultDir+"/"+revert.PendingName), s.has(revert.DefaultDir+"/"+revert.ConfirmedName))
	}
	// Nothing left to confirm: exit 1.
	executeHardn(t, "confirm")
	if s.exit != 1 {
		t.Errorf("second confirm exit %d, want 1", s.exit)
	}
}

func TestRevertRollbackCommand(t *testing.T) {
	s := newRevertSeams(t, 0)
	executeHardn(t, "rollback", revertTestID)
	if s.exit != -1 || s.has(revert.DefaultDir+"/"+revert.PendingName) || s.has(revert.DefaultDir+"/"+revert.RollbackName) {
		t.Fatalf("exit %d; journal left", s.exit)
	}
	if s.held != 1 || s.released != 1 {
		t.Errorf("signals held %d, released %d; want once each, around the rollback", s.held, s.released)
	}
	// Idempotent: nothing to do is success.
	executeHardn(t, "rollback", revertTestID)
	if s.exit != -1 {
		t.Errorf("second rollback exit %d, want none", s.exit)
	}
	executeHardn(t, "rollback", "../etc")
	if s.exit != 2 {
		t.Errorf("rollback of a path: exit %d, want 2", s.exit)
	}
}

func TestRevertCommandsRefuse(t *testing.T) {
	for _, tt := range []struct {
		name string
		euid int
		args []string
		exit int
	}{
		{"confirm as alice", 1000, []string{"confirm"}, 1},
		{"rollback as alice", 1000, []string{"rollback", revertTestID}, 1},
		{"confirm with -n", 0, []string{"-n", "confirm"}, 2},
		{"rollback with -n", 0, []string{"rollback", "-n", revertTestID}, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newRevertSeams(t, tt.euid)
			executeHardn(t, tt.args...)
			if s.exit != tt.exit || !s.has(revert.DefaultDir+"/"+revert.PendingName) {
				t.Fatalf("exit %d, want %d, with the change still pending", s.exit, tt.exit)
			}
			dryRun = false
		})
	}
	// Arguments are cobra's: an error, and nothing run.
	s := newRevertSeams(t, 0)
	rootCmd.SetArgs([]string{"rollback"})
	if err := rootCmd.Execute(); err == nil || !s.has(revert.DefaultDir+"/"+revert.PendingName) {
		t.Errorf("rollback with no id: %v", err)
	}
	rootCmd.SetArgs([]string{"confirm", "now"})
	if err := rootCmd.Execute(); err == nil {
		t.Error("confirm with an argument accepted")
	}
}

// The notice is shown to root only, on stderr; a subcommand clears the
// timer's rollback notice once a person has seen it, the root command (the
// legacy menu clears the screen) does not.
func TestRevertNotices(t *testing.T) {
	s := newRevertSeams(t, 0)
	revertNow = func() time.Time { return revertTestNow.Add(3 * time.Minute) }
	executeHardn(t, "rollback", revertTestID) // by the timer: past the deadline
	reverted := revert.DefaultDir + "/" + revert.RevertedName
	if !s.has(reverted) {
		t.Fatal("no outcome after the timer's rollback")
	}
	var stdout bytes.Buffer
	outWas := color.Output
	color.Output = &stdout
	defer func() { color.Output = outWas }()
	showRevertNotices(rootCmd, nil)
	if !s.has(reverted) {
		t.Error("the root command cleared the notice")
	}
	// system-details --json with stderr a pipe: its stdout stays its own,
	// and the notice stays for a person to see.
	s.seen = false
	s.stderr.Reset()
	showRevertNotices(systemDetailsCmdForTest(), nil)
	if !strings.Contains(s.stderr.String(), "[WARNING] Change "+revertTestID) || !strings.Contains(s.stderr.String(), "because it was not confirmed within 180s") {
		t.Errorf("stderr = %q", s.stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("the notice reached stdout: %q", stdout.String())
	}
	if !s.has(reverted) {
		t.Error("a notice nobody saw was cleared")
	}
	s.seen = true
	showRevertNotices(setupSudoEnvCmd, nil)
	if s.has(reverted) {
		t.Error("a subcommand did not clear the notice once seen")
	}

	s = newRevertSeams(t, 1000)
	calls := len(s.f.Calls())
	showRevertNotices(setupSudoEnvCmd, nil)
	if len(s.f.Calls()) != calls {
		t.Error("the notice read the journal for a user other than root")
	}
}

func TestRevertSeamsAreReal(t *testing.T) {
	if revertEUID() != os.Geteuid() || revertPID() != os.Getpid() {
		t.Error("the production seams are not the os package's")
	}
	if _, ok := revertHost().(*host.Real); !ok {
		t.Error("revertHost is not host.Real")
	}
}

// systemDetailsCmdForTest is the registered system-details subcommand.
func systemDetailsCmdForTest() *cobra.Command {
	for _, c := range rootCmd.Commands() {
		if c.Name() == "system-details" {
			return c
		}
	}
	panic("no system-details subcommand")
}

// holdSignals keeps SIGINT and SIGHUP ignored, and SIGTERM caught, until it
// is released: this process survives each, sent to itself.
func TestHoldSignals(t *testing.T) {
	logging.SetSilentMode(true)
	defer logging.SetSilentMode(false)
	release := holdSignals()
	if !signal.Ignored(syscall.SIGINT) || !signal.Ignored(syscall.SIGHUP) {
		t.Fatal("SIGINT and SIGHUP are not ignored")
	}
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM} {
		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(50 * time.Millisecond) // the process is still here
	release()
	if signal.Ignored(syscall.SIGINT) || signal.Ignored(syscall.SIGHUP) {
		t.Error("release left SIGINT or SIGHUP ignored")
	}
}
