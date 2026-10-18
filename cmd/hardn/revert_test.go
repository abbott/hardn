package main

import (
	"context"
	"os"
	"testing"
	"time"

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
	f    *hostfake.System
	exit int // -1: not called
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

	euidWas, hostWas, getenvWas, pidWas, exitWas, nowWas, dryRunWas := revertEUID, revertHost, revertGetenv, revertPID, revertExit, revertNow, dryRun
	revertEUID = func() int { return euid }
	revertHost = func() host.System { return s.f }
	revertGetenv = func(k string) string {
		if k == "SSH_CONNECTION" {
			return "192.0.2.10 50123 192.0.2.20 2230"
		}
		return ""
	}
	revertPID = func() int { return 4000 }
	revertExit = func(code int) { s.exit = code }
	revertNow = clock
	logging.SetSilentMode(true)
	t.Cleanup(func() {
		revertEUID, revertHost, revertGetenv, revertPID, revertExit, revertNow, dryRun = euidWas, hostWas, getenvWas, pidWas, exitWas, nowWas, dryRunWas
		logging.SetSilentMode(false)
		rootCmd.SetArgs(nil)
	})
	return s
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

// The notice is shown to root only; a subcommand clears the timer's
// rollback notice once shown, the root command (the legacy menu clears the
// screen) does not.
func TestRevertNotices(t *testing.T) {
	s := newRevertSeams(t, 0)
	revertNow = func() time.Time { return revertTestNow.Add(3 * time.Minute) }
	executeHardn(t, "rollback", revertTestID) // by the timer: past the deadline
	reverted := revert.DefaultDir + "/" + revert.RevertedName
	if !s.has(reverted) {
		t.Fatal("no outcome after the timer's rollback")
	}
	showRevertNotices(rootCmd, nil)
	if !s.has(reverted) {
		t.Error("the root command cleared the notice")
	}
	showRevertNotices(setupSudoEnvCmd, nil)
	if s.has(reverted) {
		t.Error("a subcommand did not clear the notice")
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
