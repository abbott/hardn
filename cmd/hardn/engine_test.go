package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
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
	// alice's SSH session: sshd, its session, her shell, sudo, hardn.
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
// signals' context is a live one its stop ends, and the screens read the
// one line reader every prompt shares and write to stdout.
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
	ctx, stop := engineSignals(context.Background())
	if ctx.Err() != nil {
		t.Error("engineSignals' context is done before any signal")
	}
	stop()
	if ctx.Err() == nil {
		t.Error("engineSignals' stop does not end its context")
	}
}
