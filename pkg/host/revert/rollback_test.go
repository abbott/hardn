package revert

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/netip"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

const (
	dropIn   = "/etc/ssh/sshd_config.d/00-hardn.conf"
	before   = "PasswordAuthentication yes\n"
	applied  = "Port 22\nPort 2230\nPasswordAuthentication no\n"
	ss22     = "LISTEN 0      4096         0.0.0.0:22        0.0.0.0:*\n"
	ufwUndo  = "ufw delete allow 2230/tcp"
	stopUnit = "systemctl stop hardn-revert-" + testID + ".timer"
)

// change is a host with a port change to 2230 applied and journalled, as
// the engine would leave it: 00-hardn.conf backed up and rewritten, a ufw
// rule added, ssh.service reloaded (Debian 13, sshd started by
// ssh.service).
type change struct {
	f    *hostfake.System
	c    *clock
	log  *bytes.Buffer
	j    Journal
	opts Options
}

func newChange(t *testing.T) *change {
	t.Helper()
	ctx := context.Background()
	ch := &change{f: newHost(t), c: &clock{testNow}, log: &bytes.Buffer{}}
	ch.f.AddFile(dropIn, []byte(before), 0o644, 0, 0)
	ch.opts = testOptions(ch.c, ch.log)
	ch.opts.DetectSSH = func(context.Context, host.System) detect.SSH {
		return detect.SSH{Mode: detect.SSHService, Service: detect.UnitState{Enabled: "enabled", Active: "active"}}
	}
	ch.j = armed(t, ch.f, ch.c)

	run, err := backup.Open(ch.f, backup.Settings{Path: backup.DefaultPath, Enabled: true}, testID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := run.Backup(ctx, dropIn, backup.RollbackRequired)
	if err != nil {
		t.Fatal(err)
	}
	ch.f.AddFile(dropIn, []byte(applied), 0o644, 0, 0)
	ch.j.Backup = &Backups{Dir: run.Dir(), Seqs: []int{r.Entry.Seq}}
	ch.j.Undo = []Step{{Op: OpRun, Argv: strings.Fields(ufwUndo), Why: "the rule p2-ufw added"}}
	ch.j.Reload = []Reload{{Service: ReloadSSH, Keywords: []string{"Port", "PasswordAuthentication"},
		After: []Listener{{Addr: netip.IPv4Unspecified(), Port: 22}}}}
	if err := Record(ctx, ch.f, ch.j, ch.opts); err != nil {
		t.Fatal(err)
	}
	ch.f.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", "ssh.service")
	ch.f.Respond(hostfake.Response{}, "systemctl", "reload", "ssh.service")
	ch.f.Respond(hostfake.Response{Stdout: ss22}, "ss", "-Htln")
	ch.f.Respond(hostfake.Response{}, "systemctl", "stop", "hardn-revert-"+testID+".timer")
	return ch
}

func (ch *change) dropIn(t *testing.T) string {
	t.Helper()
	e, ok := ch.f.Entry(dropIn)
	if !ok {
		t.Fatal("00-hardn.conf missing")
	}
	return string(e.Data)
}

func TestRollbackByTheTimer(t *testing.T) {
	ctx := context.Background()
	ch := newChange(t)
	ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\nRule deleted (v6)\n"}, "ufw", "delete", "allow", "2230/tcp")
	ch.c.t = ch.j.Deadline()

	res, err := Rollback(ctx, ch.f, testID, ch.opts)
	if err != nil {
		t.Fatalf("Rollback: %v\nlog:\n%s", err, ch.log)
	}
	if res.Nothing || res.Reason != ReasonTimeout || len(res.Restored) != 1 || res.Restored[0].Action != backup.Rewritten ||
		len(res.Undone) != 1 || len(res.Reloads) != 1 || !res.Reloads[0].Verified {
		t.Errorf("result %+v", res)
	}
	if got := ch.dropIn(t); got != before {
		t.Errorf("00-hardn.conf = %q, want the backup", got)
	}
	got := runs(ch.f)
	order := []string{"run " + ufwUndo, "run systemctl is-active ssh.service", "run systemctl reload ssh.service", "run ss -Htln"}
	at := 0
	for _, want := range order {
		i := strings.Index(got[at:], want)
		if i < 0 {
			t.Fatalf("%q not in order in:\n%s", want, got)
		}
		at += i
	}
	if strings.Contains(got, stopUnit) {
		t.Errorf("the timer that fired was stopped:\n%s", got)
	}
	if exists(ch.f, stateFile(RollbackName)) || exists(ch.f, stateFile(PendingName)) {
		t.Error("journal left behind")
	}
	st, err := ReadState(ctx, ch.f, ch.opts)
	if err != nil || st.Busy() || st.Reverted == nil || st.Reverted.ID != testID || len(st.Reverted.Errors) != 0 {
		t.Fatalf("state after: %+v, %v", st, err)
	}
	for _, want := range []string{"rollback " + testID + " (SSH port 22 to 2230 on web-01.example.net): not confirmed within 180s",
		"restore " + dropIn + ": rewritten", "undo 1 of 1: " + ufwUndo, "reload ssh ([Port PasswordAuthentication])", "rollback " + testID + ": done"} {
		if !strings.Contains(ch.log.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, ch.log)
		}
	}

	// Idempotent: a second run has nothing to do, and says why.
	again, err := Rollback(ctx, ch.f, testID, ch.opts)
	if err != nil || !again.Nothing || !strings.Contains(again.Why, "rolled back at 2026-10-18 12:03:00 UTC") {
		t.Errorf("second Rollback = %+v, %v", again, err)
	}
	// The next run says so, once.
	ns := Notices(st, ch.c.t)
	if len(ns) != 1 || ns[0].Kind != NoticeReverted || !strings.Contains(ns[0].Text, "because it was not confirmed within 180s") {
		t.Errorf("notices %+v", ns)
	}
	if err := Acknowledge(ctx, ch.f, ch.opts); err != nil || exists(ch.f, stateFile(RevertedName)) {
		t.Errorf("Acknowledge = %v", err)
	}
	if err := Acknowledge(ctx, ch.f, ch.opts); err != nil {
		t.Errorf("second Acknowledge = %v", err)
	}
}

func TestRollbackRequested(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name   string
		reason Reason
		want   Reason
	}{
		{"by hand, before the deadline", "", ReasonRequested},
		{"by the engine, after a failed step", ReasonFailed, ReasonFailed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ch := newChange(t)
			ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
			ch.c.t = ch.j.Created.Add(MinTimeout)
			ch.opts.Reason = tt.reason
			res, err := Rollback(ctx, ch.f, testID, ch.opts)
			if err != nil || res.Reason != tt.want || res.CancelErr != nil {
				t.Fatalf("Rollback = %+v, %v", res, err)
			}
			if !strings.Contains(runs(ch.f), "run "+stopUnit) {
				t.Errorf("timer not stopped:\n%s", runs(ch.f))
			}
			if exists(ch.f, stateFile(RevertedName)) {
				t.Error("reverted.json written for a rollback the user saw")
			}
		})
	}
}

// A rollback that stops part-way is taken up again where it stopped, and
// blocks new changes meanwhile.
func TestRollbackResumes(t *testing.T) {
	ctx := context.Background()
	ch := newChange(t)
	ch.f.Respond(hostfake.Response{ExitCode: 1, Stderr: "ERROR: Could not delete non-existent rule\n"}, "ufw", "delete", "allow", "2230/tcp")
	ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
	ch.c.t = ch.j.Deadline()

	res, err := Rollback(ctx, ch.f, testID, ch.opts)
	if !errors.Is(err, ErrIncomplete) || !strings.Contains(err.Error(), "sudo hardn rollback "+testID) {
		t.Fatalf("first Rollback = %v", err)
	}
	if len(res.Reloads) != 1 {
		t.Errorf("the ssh reload does not depend on the firewall's undo: %+v", res.Reloads)
	}
	rb := readJournalT(t, ch.f, stateFile(RollbackName))
	if p := rb.Rollback; p == nil || !p.Restored || p.UndoDone != 0 || p.ReloadDone != 1 || p.Reason != ReasonTimeout {
		t.Fatalf("progress %+v", rb.Rollback)
	}
	st, _ := ReadState(ctx, ch.f, ch.opts)
	if st.Reverted == nil || len(st.Reverted.Errors) != 1 {
		t.Errorf("outcome %+v; want the failure recorded", st.Reverted)
	}
	ns := Notices(st, ch.c.t)
	if len(ns) != 2 || ns[0].Kind != NoticeUnfinished || ns[1].Kind != NoticeReverted || !strings.Contains(ns[1].Text, "did not finish") {
		t.Errorf("notices %+v", ns)
	}
	next := lockout()
	next.ID = otherID
	if _, err := Arm(ctx, ch.f, next, systemd, ch.opts); !errors.Is(err, ErrPending) || !strings.Contains(err.Error(), "has not finished") {
		t.Errorf("Arm during an unfinished rollback = %v", err)
	}
	if _, err := Confirm(ctx, ch.f, Session{Remote: true, Conn: newConn}, ch.opts); !errors.Is(err, ErrRollingBack) {
		t.Errorf("Confirm during an unfinished rollback = %v", err)
	}

	ch.c.t = ch.c.t.Add(MinTimeout)
	ch.log.Reset()
	before := len(ch.f.Calls())
	res, err = Rollback(ctx, ch.f, testID, ch.opts)
	if err != nil || !res.Resumed || res.Reason != ReasonTimeout || len(res.Restored) != 0 || len(res.Undone) != 1 || len(res.Reloads) != 0 {
		t.Fatalf("resumed Rollback = %+v, %v\n%s", res, err, ch.log)
	}
	var again []string
	for _, c := range ch.f.Calls()[before:] {
		if c.Op == "run" {
			again = append(again, host.FormatArgv(c.Argv))
		}
	}
	if len(again) != 1 || again[0] != ufwUndo {
		t.Errorf("resumed runs %q; want the failed undo only", again)
	}
	if exists(ch.f, stateFile(RollbackName)) {
		t.Error("rollback.json left after it finished")
	}
}

// A backup that cannot be restored stops the reloads: they would make sshd
// act on files no check has passed. The inverse steps still run.
func TestRollbackRestoreFails(t *testing.T) {
	ctx := context.Background()
	ch := newChange(t)
	ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
	m, err := backup.Load(ctx, ch.f, ch.j.Backup.Dir)
	if err != nil {
		t.Fatal(err)
	}
	ch.f.AddFile(m.Dir+"/"+m.Entries[0].File.Backup, []byte("tampered\n"), 0o600, 0, 0)

	res, err := Rollback(ctx, ch.f, testID, ch.opts)
	if !errors.Is(err, ErrIncomplete) || !errors.Is(err, backup.ErrCorrupt) {
		t.Fatalf("Rollback = %v", err)
	}
	if res.Skipped == "" || len(res.Reloads) != 0 || len(res.Undone) != 1 {
		t.Errorf("result %+v", res)
	}
	if strings.Contains(runs(ch.f), "reload ssh.service") {
		t.Errorf("reloaded after a failed restore:\n%s", runs(ch.f))
	}
	if got := ch.dropIn(t); got != applied {
		t.Errorf("00-hardn.conf = %q, want it left as applied", got)
	}
	if rb := readJournalT(t, ch.f, stateFile(RollbackName)); rb.Rollback.Restored || rb.Rollback.ReloadDone != 0 {
		t.Errorf("progress %+v", rb.Rollback)
	}
}

func TestRollbackNothing(t *testing.T) {
	ctx := context.Background()
	ch := newChange(t)
	for _, tt := range []struct {
		id, why string
	}{
		{otherID, "the journalled change is " + testID + ", not this one"},
	} {
		res, err := Rollback(ctx, ch.f, tt.id, ch.opts)
		if err != nil || !res.Nothing || res.Why != tt.why {
			t.Errorf("Rollback(%s) = %+v, %v", tt.id, res, err)
		}
	}
	if _, err := Rollback(ctx, ch.f, "../pending", ch.opts); !errors.Is(err, ErrID) {
		t.Errorf("Rollback(a path) = %v", err)
	}
	res, err := Rollback(ctx, newHost(t), testID, ch.opts)
	if err != nil || !res.Nothing || !strings.Contains(res.Why, "not journalled") {
		t.Errorf("Rollback with no journal = %+v, %v", res, err)
	}
	if !exists(ch.f, stateFile(PendingName)) {
		t.Error("a rollback of another id touched the pending change")
	}
}

// Ctrl-C (a done context) does not stop a rollback: it runs under its own
// deadline.
func TestRollbackAfterCancel(t *testing.T) {
	ch := newChange(t)
	ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Rollback(ctx, ch.f, testID, ch.opts); err != nil {
		t.Fatalf("Rollback with a done context = %v", err)
	}
	if got := ch.dropIn(t); got != before {
		t.Errorf("00-hardn.conf = %q", got)
	}
}

func TestRunStep(t *testing.T) {
	ctx := context.Background()
	f := newHost(t)
	f.AddDir("/home/alice/.ssh", 0o755, 1000, 1000)
	f.AddDir("/etc/hardn-made", 0o755, 0, 0)
	f.AddFile("/etc/notadir", nil, 0o644, 0, 0)
	tests := []struct {
		step Step
		note string
		bad  bool
	}{
		{Step{Op: OpChmod, Path: "/home/alice/.ssh", Mode: permT(0o700)}, "", false},
		{Step{Op: OpChown, Path: "/home/alice/.ssh", Owner: &Owner{UID: 1000, GID: -1}}, "", false},
		{Step{Op: OpRemoveDir, Path: "/etc/hardn-made"}, "", false},
		{Step{Op: OpRemoveDir, Path: "/etc/hardn-made"}, "already gone", false},
		{Step{Op: OpChmod, Path: "/home/bob/.ssh", Mode: permT(0o700)}, "already gone", false},
		{Step{Op: OpRemoveDir, Path: "/etc/notadir"}, "", true},
		{Step{Op: OpRun, Argv: []string{"ufw", "disable"}}, "", true}, // unscripted: fails closed
	}
	for _, tt := range tests {
		r := runStep(ctx, f, tt.step)
		if r.Note != tt.note || tt.bad != (r.Err != nil) {
			t.Errorf("%s: note %q, err %v", stepWords(tt.step), r.Note, r.Err)
		}
	}
	if e, _ := f.Entry("/home/alice/.ssh"); e.Mode.Perm() != 0o700 {
		t.Errorf("chmod: %v", e.Mode)
	}
	if !exists(f, "/etc/notadir") {
		t.Error("rmdir removed a file")
	}
}

func TestClaim(t *testing.T) {
	ctx := context.Background()
	f := newHost(t)
	armed(t, f, &clock{testNow})
	if _, err := claim(ctx, f, stateFile(PendingName), stateFile(RollbackName), otherID); !errors.Is(err, ErrClaim) {
		t.Errorf("claim of another id = %v", err)
	}
	if !exists(f, stateFile(PendingName)) || exists(f, stateFile(RollbackName)) {
		t.Error("the other change was not put back")
	}
	if _, err := claim(ctx, f, stateFile("gone.json"), stateFile(RollbackName), testID); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("claim of a missing file = %v", err)
	}
	if j, err := claim(ctx, f, stateFile(PendingName), stateFile(RollbackName), testID); err != nil || j.ID != testID {
		t.Errorf("claim = %+v, %v", j, err)
	}
}
