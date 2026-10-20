package main

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/revert/plans"
	"github.com/abbott/hardn/pkg/logging"
)

// Synthetic values: web-01.example.net, 192.0.2.0/24, port 2230.
const (
	revertTestID     = "20261018T120000Z-1a2b3c4d"
	revertTestHardn  = "/usr/local/bin/hardn"
	revertBootIDFile = "/proc/sys/kernel/random/boot_id"
	revertBootID     = "8e2a3c4d-1b2c-4d3e-8f40-0123456789ab"
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
	return newRevertSeamsWith(t, euid, nil)
}

// newRevertSeamsWith is newRevertSeams with record, when not nil, making
// the journal the engine records (revert.Record) before the apply ends.
func newRevertSeamsWith(t *testing.T, euid int, record func(revert.Journal) revert.Journal) *revertSeams {
	t.Helper()
	s := &revertSeams{f: hostfake.New(host.Options{}), exit: -1}
	s.f.AddFile(revertTestHardn, []byte("ELF"), 0o755, 0, 0)
	s.f.AddDir("/var/lib", 0o755, 0, 0)
	s.f.AddFile(revertBootIDFile, []byte(revertBootID+"\n"), 0o444, 0, 0) // the timer records it (revert-boot.md)
	argv := []string{"systemd-run", "--unit=hardn-revert-" + revertTestID, "--description=hardn: roll back change " + revertTestID + " unless confirmed",
		"--on-active=180s", "--timer-property=AccuracySec=1s", revertTestHardn, "rollback", revertTestID}
	s.f.Respond(hostfake.Response{}, argv[0], argv[1:]...)
	s.f.Respond(hostfake.Response{}, "systemctl", "stop", "hardn-revert-"+revertTestID+".timer")
	j := revert.Journal{ID: revertTestID, Risk: revert.RiskLockout, TimeoutSeconds: 180, Summary: "SSH port 22 to 2230 on web-01.example.net",
		Session: revert.Session{Remote: true, Conn: "192.0.2.10 50022 192.0.2.20 22"}}
	clock := func() time.Time { return revertTestNow }
	j, err := revert.Arm(context.Background(), s.f, j, detect.Init{Kind: detect.Systemd, Running: detect.Yes}, revert.Options{Now: clock, Hardn: revertTestHardn})
	if err != nil {
		t.Fatal(err)
	}
	if record != nil {
		if err := revert.Record(context.Background(), s.f, record(j), revert.Options{Now: clock, Plans: revertPlans}); err != nil {
			t.Fatal(err)
		}
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
	executableWas, initWas := revertExecutable, revertInit
	revertEUID = func() int { return euid }
	revertHost = func() host.System { return s.f }
	revertGetenv = func(string) string { return "" } // sudo reset it
	revertPID = func() int { return 4000 }
	revertExit = func(code int) { s.exit = code }
	revertNow = clock
	revertStderr = &s.stderr
	revertNoticeSeen = func() bool { return s.seen }
	revertHoldSignals = func() func() { s.held++; return func() { s.released++ } }
	revertExecutable = func() (string, error) { return revertTestHardn, nil }
	revertInit = func(context.Context, host.System) detect.Init {
		return detect.Init{Kind: detect.Systemd, Running: detect.Yes}
	}
	s.seen = true
	logging.SetSilentMode(true)
	t.Cleanup(func() {
		revertEUID, revertHost, revertGetenv, revertPID, revertExit, revertNow, dryRun = euidWas, hostWas, getenvWas, pidWas, exitWas, nowWas, dryRunWas
		revertStderr, revertNoticeSeen, revertHoldSignals = stderrWas, seenWas, holdWas
		revertExecutable, revertInit = executableWas, initWas
		rollbackAbandon, rollbackBoot = false, false // a flag's value outlives the run that set it
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

// output runs hardn with args, as executeHardn, and returns what it
// printed through the legacy logger, without colour (the root command turns
// it on); s.exit is reset first.
func (s *revertSeams) output(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	outWas := color.Output
	color.Output = &out
	logging.SetSilentMode(false)
	defer func() { color.Output = outWas; logging.SetSilentMode(true) }()
	s.exit = -1
	executeHardn(t, args...)
	return regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(out.String(), "")
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

// unfinishable is the journal of a change whose rollback can never finish:
// its backup run directory is gone, so the rollback deletes ufw's rule,
// cannot restore the backup and so does not reload ssh.
func unfinishable(j revert.Journal) revert.Journal {
	j.Backup = &revert.Backups{Dir: "/var/backups/hardn/" + j.ID, Seqs: []int{1}}
	j.Undo = []revert.Step{{Op: revert.OpRun, Argv: []string{"ufw", "delete", "allow", "2230/tcp"}}}
	j.Reload = []revert.Reload{{Service: revert.ReloadSSH, Keywords: []string{"Port"}, After: []revert.Listener{{Addr: netip.IPv4Unspecified(), Port: 22}}}}
	return j
}

// `hardn rollback --abandon <id>` (P-35 (a)) gives up an unfinished
// rollback whose process has gone, says that the host stays as it is and
// what was and was not undone, and keeps the record; it is refused while
// the rollback runs, for another id, for an id that is not one, under -n
// and for a user other than root, each changing nothing.
func TestRevertAbandonCommand(t *testing.T) {
	ctx := context.Background()
	s := newRevertSeamsWith(t, 0, unfinishable)
	s.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
	// The rollback runs as process 7000, which keeps running for now.
	revertProc(s.f, 7000, 1, "hardn")
	stat, _ := s.f.Entry("/proc/7000/stat")
	s.f.AddFile("/proc/self/stat", stat.Data, 0o444, 0, 0)
	s.f.AddFile("/proc/sys/kernel/random/boot_id", []byte("8e2a3c4d-1b2c-4d3e-8f40-0123456789ab\n"), 0o444, 0, 0)
	rollbackJSON := revert.DefaultDir + "/" + revert.RollbackName
	kept := revert.DefaultDir + "/" + revert.AbandonedName(revertTestID)

	if out := s.output(t, "rollback", revertTestID); s.exit != 1 || !s.has(rollbackJSON) || !strings.Contains(out, "`sudo hardn rollback --abandon "+revertTestID+"`") {
		t.Fatalf("rollback: exit %d, rollback.json %v; want 1, kept, naming the flag:\n%s", s.exit, s.has(rollbackJSON), out)
	}
	for _, tt := range []struct {
		name string
		euid int
		args []string
		exit int
		says string
		host bool // whether the host is reached at all
	}{
		{"while it runs", 0, []string{"rollback", "--abandon", revertTestID}, 1,
			"process 7000 is rolling it back now (started 2026-10-18 12:00:00 UTC); wait for it to end", true},
		{"as alice", 1000, []string{"rollback", "--abandon", revertTestID}, 1,
			"hardn rollback --abandon must be run as root", false},
		{"with -n", 0, []string{"rollback", "--abandon", "-n", revertTestID}, 2,
			"-n only previews, and hardn rollback --abandon changes the host: run it without -n", false},
		{"another change", 0, []string{"rollback", "--abandon", "20261018T130000Z-5e6f7a8b"}, 1,
			"the unfinished rollback is of change " + revertTestID + ", not this one; finish it with `sudo hardn rollback " + revertTestID + "`", true},
		{"not an id, the flag after it", 0, []string{"rollback", "../etc", "--abandon"}, 2, `revert: abandon "../etc": not a change id`, false},
	} {
		if tt.name == "as alice" {
			// From here on, the rollback's process has gone.
			if err := s.f.Remove(ctx, "/proc/7000/stat"); err != nil {
				t.Fatal(err)
			}
		}
		revertEUID = func() int { return tt.euid }
		calls := len(s.f.Calls())
		out := s.output(t, tt.args...)
		if s.exit != tt.exit || !strings.Contains(out, "[ERROR] ") || !strings.Contains(out, tt.says) || !s.has(rollbackJSON) || s.has(kept) {
			t.Errorf("%s: exit %d, want %d; rollback.json %v, abandoned %v; output:\n%s", tt.name, s.exit, tt.exit, s.has(rollbackJSON), s.has(kept), out)
		}
		for _, c := range s.f.Calls()[calls:] {
			if !tt.host || changed(c) {
				t.Errorf("%s: %s", tt.name, c)
			}
		}
		dryRun = false
	}
	revertEUID = func() int { return 0 }
	// The journal lock that cannot be taken (here a directory in its
	// place): nothing given up, and what to do instead.
	lock := revert.DefaultDir + "/" + revert.JournalLockName
	lockWas, hadLock := s.f.Entry(lock)
	s.f.AddDir(lock, 0o700, 0, 0)
	calls := len(s.f.Calls())
	if out := s.output(t, "rollback", "--abandon", revertTestID); s.exit != 1 || !strings.Contains(out, "nothing given up") ||
		!strings.Contains(out, "The journal lock "+lock+" could not be taken") || !s.has(rollbackJSON) || s.has(kept) {
		t.Errorf("abandon, the journal lock a directory: exit %d; output:\n%s", s.exit, out)
	}
	for _, c := range s.f.Calls()[calls:] {
		if changed(c) {
			t.Errorf("abandon, the journal lock a directory: %s", c)
		}
	}
	if err := s.f.Remove(ctx, lock); err != nil {
		t.Fatal(err)
	}
	if hadLock {
		s.f.AddFile(lock, lockWas.Data, lockWas.Mode, lockWas.UID, lockWas.GID)
	}
	// Arguments are cobra's: an error, and nothing run.
	rootCmd.SetArgs([]string{"rollback", "--abandon"})
	if err := rootCmd.Execute(); err == nil || !s.has(rollbackJSON) {
		t.Errorf("--abandon with no id: %v", err)
	}
	// The next run's notice names the flag.
	s.stderr.Reset()
	showRevertNotices(setupSudoEnvCmd, nil)
	if !strings.Contains(s.stderr.String(), "to give it up, leaving the host as it is, run `sudo hardn rollback --abandon "+revertTestID+"`") {
		t.Errorf("notice: %q", s.stderr.String())
	}

	out := s.output(t, "rollback", "--abandon", revertTestID)
	if s.exit != -1 || s.has(rollbackJSON) || !s.has(kept) {
		t.Fatalf("abandon: exit %d; rollback.json %v, abandoned %v\n%s", s.exit, s.has(rollbackJSON), s.has(kept), out)
	}
	for _, want := range []string{
		"[SUCCESS] Gave up the rollback of change " + revertTestID + " (SSH port 22 to 2230 on web-01.example.net).\n" +
			"[WARNING] The host stays as the rollback left it: hardn will not undo what the rollback did not.\n" +
			"[INFO] Undone by the rollback, by its record:\n" +
			"[INFO]   - undo 1 of 1: ufw delete allow 2230/tcp\n" +
			"[WARNING] Not undone, or not finished:\n" +
			"[WARNING]   - restore the backups in /var/backups/hardn/" + revertTestID + " (entry 1)\n" +
			"[WARNING]   - reload ssh (Port)\n" +
			"[WARNING] The backups were not all restored, so sshd's files on disk may be a mix of the change's and the ones before it, which no check has passed, and sshd reads them the next time it starts (a restart, a package upgrade, a reboot). " +
			"Check them now with `sudo sshd -t` and fix what it reports: the rollback's restore lines in /var/log/hardn.log say which files came back.\n" +
			"[INFO] The rollback's record is kept in " + kept + ", and /var/log/hardn.log has what each of its steps did. New changes can be applied again.\n",
		"revert: rollback " + revertTestID + " (SSH port 22 to 2230 on web-01.example.net) abandoned: backups restored false, 1 of 1 undo steps and 0 of 1 reloads done; kept in " + kept,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if s.held != 1 {
		t.Errorf("signals held %d times; want once, by the rollback alone", s.held)
	}

	// Nothing is left to give up, and nothing left to roll back.
	if out := s.output(t, "rollback", "--abandon", revertTestID); s.exit != 1 || !strings.Contains(out, "abandoned already, and its record is kept in "+kept) {
		t.Errorf("abandon again: exit %d, want 1:\n%s", s.exit, out)
	}
	rollbackAbandon = false
	if out := s.output(t, "rollback", revertTestID); s.exit != -1 || !strings.Contains(out, "[INFO] Nothing to roll back for "+revertTestID+
		": its rollback did not finish and was given up (`hardn rollback --abandon`), leaving the host as that rollback left it; its record is kept in "+kept+".\n") {
		t.Errorf("rollback after abandon: exit %d, want none:\n%s", s.exit, out)
	}
}

// changed reports whether c changed the host or ran a command.
func changed(c hostfake.Call) bool {
	switch c.Op {
	case "run", "start-detached", "rename", "write-atomic", "write-in-place", "remove", "removeall", "mkdir", "chmod", "chown":
		return true
	}
	return false
}

// After the timer's own rollback did not finish and was given up, `hardn
// rollback <id>` and `hardn confirm` say so, not that it was rolled back,
// and leave reverted.json for the next run's notice.
func TestRevertAbandonTimersRollback(t *testing.T) {
	s := newRevertSeamsWith(t, 0, unfinishable)
	s.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
	revertProc(s.f, 7000, 1, "hardn")
	stat, _ := s.f.Entry("/proc/7000/stat")
	s.f.AddFile("/proc/self/stat", stat.Data, 0o444, 0, 0)
	s.f.AddFile("/proc/sys/kernel/random/boot_id", []byte("8e2a3c4d-1b2c-4d3e-8f40-0123456789ab\n"), 0o444, 0, 0)
	deadline := revertTestNow.Add(180 * time.Second)
	revertNow = func() time.Time { return deadline }
	reverted := revert.DefaultDir + "/" + revert.RevertedName
	kept := revert.DefaultDir + "/" + revert.AbandonedName(revertTestID)
	given := "its rollback did not finish and was given up (`hardn rollback --abandon`), leaving the host as that rollback left it; its record is kept in " + kept

	if out := s.output(t, "rollback", revertTestID); s.exit != 1 || !s.has(reverted) { // the timer's
		t.Fatalf("the timer's rollback: exit %d, reverted.json %v:\n%s", s.exit, s.has(reverted), out)
	}
	if err := s.f.Remove(context.Background(), "/proc/7000/stat"); err != nil {
		t.Fatal(err)
	}
	if out := s.output(t, "rollback", "--abandon", revertTestID); s.exit != -1 || !s.has(kept) {
		t.Fatalf("abandon: exit %d:\n%s", s.exit, out)
	}
	rollbackAbandon = false
	if out := s.output(t, "rollback", revertTestID); s.exit != -1 || !strings.Contains(out, "Nothing to roll back for "+revertTestID+": "+given+".") || strings.Contains(out, "was rolled back at") || !s.has(reverted) {
		t.Errorf("rollback after abandon: exit %d, reverted.json %v:\n%s", s.exit, s.has(reverted), out)
	}
	if out := s.output(t, "confirm"); s.exit != 1 || !strings.Contains(out, "was not confirmed within 180s, and "+given) || !s.has(reverted) {
		t.Errorf("confirm after abandon: exit %d, reverted.json %v:\n%s", s.exit, s.has(reverted), out)
	}
}

// failRename makes the first rename of from fail with err: another hardn
// acting between Abandon's read and its rename.
type failRename struct {
	*hostfake.System
	from string
	err  error
}

func (f *failRename) Rename(ctx context.Context, oldname, newname string) error {
	if err := f.err; err != nil && oldname == f.from {
		f.err = nil
		return err
	}
	return f.System.Rename(ctx, oldname, newname)
}

// A journal Abandon cannot read, or a rename it loses, exits 1; the first
// names the manual fallback.
func TestRevertAbandonFails(t *testing.T) {
	rollbackJSON := revert.DefaultDir + "/" + revert.RollbackName
	fallback := "[INFO] If the file it names is " + rollbackJSON + " and this hardn cannot act on it, moving that file aside by hand, as root, gives its rollback up the same way, without this report, and leaves the host as it is.\n"
	for _, tt := range []struct {
		name     string
		setup    func(t *testing.T, s *revertSeams)
		says     string
		fallback bool
	}{
		{"another version's rollback.json", func(t *testing.T, s *revertSeams) {
			s.f.AddFile(rollbackJSON, []byte(`{"schemaVersion":99}`), 0o600, 0, 0)
		}, rollbackJSON + ": revert: journal \"\": schemaVersion 99, want 1: journal not valid", true},
		{"a rollback.json others can write", func(t *testing.T, s *revertSeams) {
			s.f.AddFile(rollbackJSON, []byte(`{}`), 0o666, 0, 0)
		}, rollbackJSON + ": a user other than root can change it", true},
		{"the journal changes during the rename", func(t *testing.T, s *revertSeams) {
			if err := s.f.Rename(context.Background(), revert.DefaultDir+"/"+revert.PendingName, rollbackJSON); err != nil {
				t.Fatal(err)
			}
			f := &failRename{System: s.f, from: rollbackJSON, err: &os.LinkError{Op: "rename", Old: rollbackJSON, New: "x", Err: syscall.ENOENT}}
			revertHost = func() host.System { return f }
		}, rollbackJSON + " changed while it was being given up; run `sudo hardn rollback --abandon " + revertTestID + "` again", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newRevertSeams(t, 0)
			tt.setup(t, s)
			calls := len(s.f.Calls())
			out := s.output(t, "rollback", "--abandon", revertTestID)
			if s.exit != 1 || !strings.Contains(out, "[ERROR] ") || !strings.Contains(out, tt.says) || strings.Contains(out, fallback) != tt.fallback {
				t.Errorf("exit %d, want 1, saying %q (the fallback: %v):\n%s", s.exit, tt.says, tt.fallback, out)
			}
			for _, c := range s.f.Calls()[calls:] {
				if changed(c) {
					t.Errorf("changed: %s", c)
				}
			}
		})
	}
}

// A rollback claimed and stopped before it recorded any progress is given
// up too, and the report says that it may have done more than it shows.
func TestRevertAbandonNoProgress(t *testing.T) {
	s := newRevertSeams(t, 0)
	if err := s.f.Rename(context.Background(), revert.DefaultDir+"/"+revert.PendingName, revert.DefaultDir+"/"+revert.RollbackName); err != nil {
		t.Fatal(err)
	}
	text := s.output(t, "rollback", "--abandon", revertTestID)
	want := "[WARNING] The rollback stopped before it recorded its progress, so it may have done more than its record shows.\n" +
		"[WARNING] Its record does not name the process that ran it, so hardn cannot tell whether a `hardn rollback " + revertTestID + "` is still running it: check with `ps`. " +
		"One that still runs writes its record again as it goes, and the next run of hardn reports it.\n" +
		"[INFO] Undone by the rollback, by its record:\n[INFO]   - nothing\n" +
		"[WARNING] Not undone, or not finished:\n[WARNING]   - nothing\n"
	if s.exit != -1 || !s.has(revert.DefaultDir+"/"+revert.AbandonedName(revertTestID)) || !strings.Contains(text, want) {
		t.Errorf("exit %d; output:\n%s", s.exit, text)
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
	if p, err := revertExecutable(); err != nil || !filepath.IsAbs(p) {
		t.Errorf("revertExecutable = %q, %v: want this process's binary, resolved", p, err)
	}
}

// TestRevertPlansRegistered: `hardn rollback` and the timer undo the plan
// steps of the four units whose rollback is their own
// (docs/safety/revert-plans.md), and of no other.
func TestRevertPlansRegistered(t *testing.T) {
	for _, u := range []string{plans.UnitPVEKeys, plans.UnitSudoers, plans.UnitDNS, plans.UnitAlpine} {
		if revertPlans[u] == nil {
			t.Errorf("no rollback registered for %s", u)
		}
	}
	if len(revertPlans) != 4 {
		t.Errorf("%d units registered, want 4", len(revertPlans))
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
// is released: this process survives each, sent to itself. The SIGTERM is
// logged as a warning, in the words `hardn rollback` has always used
// (revertWarnLog, pkg/engine/hold's SignalsAs).
func TestHoldSignals(t *testing.T) {
	var out syncBuffer
	outWas := color.Output
	color.Output = &out
	logging.SetSilentMode(false)
	defer func() { color.Output = outWas; logging.SetSilentMode(false) }()
	release := holdSignals()
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
	const warning = "[WARNING] hardn rollback: SIGTERM received; the rollback finishes first"
	deadline := time.Now().Add(2 * time.Second) // the process is still here
	for !strings.Contains(out.String(), warning) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	release()
	if got := regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(out.String(), ""); strings.Count(got, warning) != 1 {
		t.Errorf("output %q; want one %q", got, warning)
	}
	if signal.Ignored(syscall.SIGINT) || signal.Ignored(syscall.SIGHUP) {
		t.Error("release left SIGINT or SIGHUP ignored")
	}
}

// syncBuffer is an output the hold's goroutine writes while the test reads
// it.
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
