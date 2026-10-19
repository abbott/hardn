package revert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

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
	ch := newChangeBare(t)
	ch.f.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", "ssh.service")
	ch.f.Respond(hostfake.Response{}, "systemctl", "reload", "ssh.service")
	return ch
}

// newChangeBare is newChange with ssh.service's commands left for the test
// to script; ss and the timer's stop are scripted.
func newChangeBare(t *testing.T) *change {
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
	var got json.RawMessage
	plans := map[string]PlanRollback{
		"dns": func(_ context.Context, _ host.System, plan json.RawMessage) error { got = plan; return nil },
		"sudoers": func(context.Context, host.System, json.RawMessage) error {
			return fmt.Errorf("sudoers: /etc/sudoers.d/90-hardn-alice: %w", fs.ErrNotExist) // a unit's own error, not "already gone"
		},
	}
	tests = append(tests, []struct {
		step Step
		note string
		bad  bool
	}{
		{Step{Op: OpPlan, Unit: "dns", Plan: json.RawMessage(`{"from":"resolved"}`)}, "", false},
		{Step{Op: OpPlan, Unit: "sudoers", Plan: json.RawMessage(`{}`)}, "", true},
		{Step{Op: OpPlan, Unit: "alpine", Plan: json.RawMessage(`{}`)}, "", true}, // none registered
	}...)
	for _, tt := range tests {
		r := runStep(ctx, f, tt.step, plans)
		if r.Note != tt.note || tt.bad != (r.Err != nil) {
			t.Errorf("%s: note %q, err %v", stepWords(tt.step), r.Note, r.Err)
		}
	}
	if string(got) != `{"from":"resolved"}` {
		t.Errorf("the dns plan's rollback got %s", got)
	}
	if r := runStep(ctx, f, Step{Op: OpPlan, Unit: "alpine", Plan: json.RawMessage(`{}`)}, plans); !errors.Is(r.Err, ErrNoPlanRollback) {
		t.Errorf("unregistered plan: %v", r.Err)
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

// The rollback records its process; another rollback leaves a running one
// alone (the timer firing while the user's `hardn rollback` runs), and
// takes up one whose process has gone.
func TestRollbackRunning(t *testing.T) {
	ctx := context.Background()
	ch := newChange(t)
	ch.c.t = ch.j.Deadline().Add(-2 * time.Second)
	// The user's rollback is process selfPID, alive; the timer's, started
	// during the user's ufw delete, is process 6000.
	proc(ch.f, selfPID, 3980, "hardn", ptsTTY)
	var inner RollbackResult
	var innerErr error
	deletes := 0
	ch.f.Handler = func(argv []string, _ []byte) hostfake.Response {
		if host.FormatArgv(argv) != ufwUndo {
			return hostfake.Response{Err: hostfake.ErrUnscripted}
		}
		if deletes++; deletes == 1 {
			setSelf(ch.f, 6000)
			opts := ch.opts
			opts.Log = nil
			inner, innerErr = Rollback(ctx, ch.f, testID, opts)
			setSelf(ch.f, selfPID)
		}
		return hostfake.Response{Stdout: "Rule deleted\n"}
	}
	res, err := Rollback(ctx, ch.f, testID, ch.opts)
	if err != nil || res.Resumed || len(res.Undone) != 1 || len(res.Reloads) != 1 {
		t.Fatalf("the user's Rollback = %+v, %v", res, err)
	}
	if innerErr != nil || !inner.Nothing || !strings.Contains(inner.Why, "being rolled back by process 5000") {
		t.Fatalf("the timer's Rollback = %+v, %v", inner, innerErr)
	}
	if deletes != 1 || strings.Count(runs(ch.f), "systemctl reload ssh.service") != 1 {
		t.Errorf("ufw delete ran %d times; runs:\n%s", deletes, runs(ch.f))
	}
	if exists(ch.f, stateFile(RollbackName)) {
		t.Error("rollback.json left behind")
	}
}

// A rollback that stopped (its process has gone) is reported as unfinished,
// taken up by the next one, and its process is named while it runs.
func TestRollbackRunner(t *testing.T) {
	ctx := context.Background()
	ch := newChange(t)
	ch.f.Respond(hostfake.Response{ExitCode: 1, Stderr: "ERROR: Could not delete non-existent rule\n"}, "ufw", "delete", "allow", "2230/tcp")
	ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
	if _, err := Rollback(ctx, ch.f, testID, ch.opts); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("Rollback = %v", err)
	}
	rb := readJournalT(t, ch.f, stateFile(RollbackName))
	if want := (ProcStart{PID: selfPID, Start: procStart(selfPID), Boot: testBootID}); rb.Rollback.Runner != want {
		t.Errorf("runner %+v, want %+v", rb.Rollback.Runner, want)
	}
	st, err := ReadState(ctx, ch.f, ch.opts)
	if err != nil || st.Running {
		t.Fatalf("ReadState = %+v, %v; want its process gone", st, err)
	}
	// While process 5000 runs (here: it is alive), others leave it be.
	proc(ch.f, selfPID, 3980, "hardn", ptsTTY)
	if st, _ = ReadState(ctx, ch.f, ch.opts); !st.Running {
		t.Fatal("a live runner not seen")
	}
	next := lockout()
	next.ID = otherID
	if _, err := Arm(ctx, ch.f, next, systemd, ch.opts); !errors.Is(err, ErrPending) || !strings.Contains(err.Error(), "is being rolled back (process 5000)") {
		t.Errorf("Arm during a running rollback = %v", err)
	}
	if _, err := Abandon(ctx, ch.f, testID, ch.opts); !errors.Is(err, ErrRollingBack) {
		t.Errorf("Abandon of a running rollback = %v", err)
	}
	// The same process may take it up again (the engine, after an error).
	if res, err := Rollback(ctx, ch.f, testID, ch.opts); err != nil || !res.Resumed {
		t.Errorf("Rollback by its own runner = %+v, %v", res, err)
	}
}

// A reload that fails is retried by the next rollback, after the steps
// that succeeded.
func TestRollbackReloadFails(t *testing.T) {
	ctx := context.Background()
	ch := newChangeBare(t)
	ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
	ch.f.Respond(hostfake.Response{ExitCode: 1, Stderr: "Job for ssh.service failed.\n"}, "systemctl", "reload", "ssh.service")
	ch.f.Respond(hostfake.Response{}, "systemctl", "reload", "ssh.service")
	ch.f.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", "ssh.service")
	res, err := Rollback(ctx, ch.f, testID, ch.opts)
	if !errors.Is(err, ErrIncomplete) || len(res.Reloads) != 1 || !strings.Contains(err.Error(), "reload ssh") {
		t.Fatalf("Rollback = %+v, %v", res, err)
	}
	if p := readJournalT(t, ch.f, stateFile(RollbackName)).Rollback; !p.Restored || p.UndoDone != 1 || p.ReloadDone != 0 {
		t.Fatalf("progress %+v", p)
	}
	before := len(ch.f.Calls())
	res, err = Rollback(ctx, ch.f, testID, ch.opts)
	if err != nil || !res.Resumed || len(res.Undone) != 0 || len(res.Restored) != 0 || len(res.Reloads) != 1 {
		t.Fatalf("resumed Rollback = %+v, %v", res, err)
	}
	for _, c := range ch.f.Calls()[before:] {
		if c.Op == "run" && host.FormatArgv(c.Argv) == ufwUndo {
			t.Error("the undo that succeeded ran again")
		}
	}
}

// A reload journalled before the forward reload (IfUp) runs when the unit
// is up and leaves a unit that is down alone; one journalled after the
// forward reload ran starts a unit the change left down.
func TestRollbackIfUp(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name   string
		ifUp   bool
		active string
		want   string // the command that must run; "" for none
	}{
		{"before the forward reload, unit up: reloaded (the Record after it was lost)", true, "active", "systemctl reload ssh.service"},
		{"before the forward reload, unit down: left down", true, "inactive", ""},
		{"after the forward reload ran, unit down: started", false, "inactive", "systemctl start ssh.service"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ch := newChangeBare(t)
			ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
			ch.j.Reload[0].IfUp = tt.ifUp
			ch.j.Reload[0].Keywords = []string{"PasswordAuthentication"}
			ch.j.Reload[0].After = nil
			if err := Record(ctx, ch.f, ch.j, ch.opts); err != nil {
				t.Fatal(err)
			}
			code := 0
			if tt.active != "active" {
				code = 3
			}
			ch.f.Respond(hostfake.Response{Stdout: tt.active + "\n", ExitCode: code}, "systemctl", "is-active", "ssh.service")
			ch.f.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", "ssh.service")
			ch.f.Respond(hostfake.Response{}, "systemctl", "start", "ssh.service")
			ch.f.Respond(hostfake.Response{}, "systemctl", "reload", "ssh.service")
			res, err := Rollback(ctx, ch.f, testID, ch.opts)
			if err != nil || len(res.Reloads) != 1 {
				t.Fatalf("Rollback = %+v, %v", res, err)
			}
			got := runs(ch.f)
			for _, cmd := range []string{"systemctl reload ssh.service", "systemctl start ssh.service"} {
				if strings.Contains(got, "run "+cmd) != (cmd == tt.want) {
					t.Errorf("%q ran %v; runs:\n%s", cmd, !(cmd == tt.want), got)
				}
			}
		})
	}
}

// A port rollback on a socket-activated host (Ubuntu 24.04 and 26.04), with
// the SSH facts detected at rollback time: the socket's drop-ins are read,
// the generator rerun and ssh.socket restarted.
func TestRollbackSocketHost(t *testing.T) {
	ctx := context.Background()
	ch := newChange(t)
	ch.opts.DetectSSH = nil
	f := ch.f
	f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
	f.AddDir(detect.SystemdRunDir, 0o755, 0, 0)
	f.AddFile(detect.SocketGenerator, []byte("ELF"), 0o755, 0, 0)
	f.Respond(hostfake.Response{Stdout: "enabled\n"}, "systemctl", "is-enabled", "ssh.socket")
	f.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", "ssh.socket")
	f.Respond(hostfake.Response{Stdout: "disabled\n", ExitCode: 1}, "systemctl", "is-enabled", "ssh.service")
	f.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", "ssh.service")
	f.Respond(hostfake.Response{}, "systemctl", "daemon-reload")
	f.Respond(hostfake.Response{Stdout: "Listen=0.0.0.0:22 (Stream)\nListen=[::]:22 (Stream)\n"}, "systemctl", "show", "-p", "Listen", "ssh.socket")
	f.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", "ssh.socket")
	f.Respond(hostfake.Response{}, "systemctl", "restart", "ssh.socket")
	f.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", "ssh.socket")
	res, err := Rollback(ctx, f, testID, ch.opts)
	if err != nil || len(res.Reloads) != 1 || !res.Reloads[0].Verified {
		t.Fatalf("Rollback = %+v, %v\n%s", res, err, runs(f))
	}
	got := runs(f)
	at := 0
	for _, want := range []string{"run systemctl daemon-reload", "run systemctl show -p Listen ssh.socket", "run systemctl restart ssh.socket", "run ss -Htln"} {
		i := strings.Index(got[at:], want)
		if i < 0 {
			t.Fatalf("%q not in order in:\n%s", want, got)
		}
		at += i
	}
	var readDropIns bool
	for _, c := range f.Calls() {
		readDropIns = readDropIns || c.Op == "readdir" && strings.HasSuffix(c.Path, "/ssh.socket.d")
	}
	if !readDropIns {
		t.Error("the socket's drop-ins were not read")
	}
}

// The OpenRC sleeper that ran the rollback is not signalled; its pidfile is
// removed.
func TestRollbackBySleeper(t *testing.T) {
	ctx := context.Background()
	f := newHost(t)
	c := &clock{testNow}
	respond(f, hostfake.Response{PID: 4321}, []string{"/bin/sh", "-c", SleeperScript, testHardn, "180", testID})
	j, err := Arm(ctx, f, lockout(), openrc, testOptions(c, nil))
	if err != nil {
		t.Fatal(err)
	}
	pidfile := "/run/hardn/revert-" + testID + ".pid"
	c.t = j.Deadline()
	res, err := Rollback(ctx, f, testID, testOptions(c, nil))
	if err != nil || res.Reason != ReasonTimeout || res.CancelErr != nil {
		t.Fatalf("Rollback = %+v, %v", res, err)
	}
	if exists(f, pidfile) || strings.Contains(runs(f), "kill") {
		t.Errorf("pidfile left %v; runs:\n%s", exists(f, pidfile), runs(f))
	}
}

// A confirmation that renames the journal first: the rollback has nothing
// to do.
func TestRollbackClaimLost(t *testing.T) {
	ctx := context.Background()
	ch := newChange(t)
	f := &claimOnRename{System: ch.f, from: stateFile(PendingName), other: stateFile(ConfirmedName)}
	res, err := Rollback(ctx, f, testID, ch.opts)
	if err != nil || !res.Nothing || !strings.Contains(res.Why, "claimed it first") {
		t.Fatalf("Rollback = %+v, %v", res, err)
	}
	if got := ch.dropIn(t); got != applied || strings.Contains(runs(ch.f), "ufw") {
		t.Errorf("a rollback that lost the claim acted:\n%s", runs(ch.f))
	}
}

// A reverted.json this version cannot read (another version's, edited,
// alice's) is a notice, and stops nothing: the timer's rollback, a
// confirmation and a new change go ahead.
func TestBadOutcome(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name string
		add  func(f *hostfake.System)
	}{
		{"a schemaVersion 2", func(f *hostfake.System) {
			f.AddFile(stateFile(RevertedName), []byte(`{"schemaVersion":2,"id":"20261001T000000Z-00000000"}`), 0o600, 0, 0)
		}},
		{"not JSON", func(f *hostfake.System) { f.AddFile(stateFile(RevertedName), []byte("{"), 0o600, 0, 0) }},
		{"alice's", func(f *hostfake.System) {
			f.AddFile(stateFile(RevertedName), []byte(`{"schemaVersion":1,"id":"20261001T000000Z-00000000"}`), 0o600, 1000, 1000)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ch := newChange(t)
			ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
			tt.add(ch.f)
			st, err := ReadState(ctx, ch.f, ch.opts)
			if err != nil || st.OutcomeErr == nil || st.Reverted != nil || st.Pending == nil {
				t.Fatalf("ReadState = %+v, %v", st, err)
			}
			if ns := Notices(st, ch.c.t); ns[len(ns)-1].Kind != NoticeBadOutcome {
				t.Errorf("notices %+v", ns)
			}
			ch.c.t = ch.j.Deadline()
			if _, err := Rollback(ctx, ch.f, testID, ch.opts); err != nil {
				t.Fatalf("the timer's Rollback = %v", err)
			}
			if got := ch.dropIn(t); got != before {
				t.Errorf("00-hardn.conf = %q, want restored", got)
			}
		})
	}
	// Confirm is not stopped either.
	f := newHost(t)
	c := &clock{testNow}
	armed(t, f, c)
	markApplied(t, f, c)
	f.AddFile(stateFile(RevertedName), []byte(`{"schemaVersion":2}`), 0o600, 0, 0)
	respond(f, hostfake.Response{}, []string{"systemctl", "stop", "hardn-revert-" + testID + ".timer"})
	if _, err := Confirm(ctx, f, newSession, testOptions(c, nil)); err != nil {
		t.Errorf("Confirm with a bad reverted.json = %v", err)
	}
	if err := Acknowledge(ctx, f, Options{}); err != nil || exists(f, stateFile(RevertedName)) {
		t.Errorf("Acknowledge of a bad outcome = %v", err)
	}
}

// An OpPlan step is undone by the unit's registered rollback, with the
// plan as journalled.
func TestRollbackPlan(t *testing.T) {
	ctx := context.Background()
	ch := newChange(t)
	ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
	var got []byte
	ch.opts.Plans = map[string]PlanRollback{"sudoers": func(_ context.Context, _ host.System, plan json.RawMessage) error {
		got = plan
		return nil
	}}
	ch.j.Undo = append([]Step{{Op: OpPlan, Unit: "sudoers", Plan: json.RawMessage(`{"tool":"sudo","changes":[{"path":"/etc/sudoers.d/90-hardn-alice"}]}`), Why: "the grant for alice"}}, ch.j.Undo...)
	if err := Record(ctx, ch.f, ch.j, ch.opts); err != nil {
		t.Fatal(err)
	}
	res, err := Rollback(ctx, ch.f, testID, ch.opts)
	if err != nil || len(res.Undone) != 2 {
		t.Fatalf("Rollback = %+v, %v", res, err)
	}
	var plan struct {
		Tool    string `json:"tool"`
		Changes []struct {
			Path string `json:"path"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(got, &plan); err != nil || plan.Tool != "sudo" || len(plan.Changes) != 1 {
		t.Errorf("the plan reached the rollback as %s (%v)", got, err)
	}
	if !strings.Contains(ch.log.String(), "undo 1 of 2: roll back the sudoers plan") {
		t.Errorf("log:\n%s", ch.log)
	}
	// In a hardn that has no rollback for the unit, the undo stops there.
	ch = newChange(t)
	opts := ch.opts
	opts.Plans = map[string]PlanRollback{"sudoers": func(context.Context, host.System, json.RawMessage) error { return nil }}
	ch.j.Undo = []Step{{Op: OpPlan, Unit: "sudoers", Plan: json.RawMessage(`{}`)}}
	if err := Record(ctx, ch.f, ch.j, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := Rollback(ctx, ch.f, testID, ch.opts); !errors.Is(err, ErrIncomplete) || !errors.Is(err, ErrNoPlanRollback) {
		t.Errorf("Rollback with no rollback for the plan = %v", err)
	}
}

// A rollback that cannot ever finish (its backups are gone) blocks every
// change until it is abandoned, which keeps it, and frees the journal.
func TestAbandon(t *testing.T) {
	ctx := context.Background()
	ch := newChange(t)
	ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
	if err := ch.f.RemoveAll(ctx, ch.j.Backup.Dir); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := Rollback(ctx, ch.f, testID, ch.opts); !errors.Is(err, ErrIncomplete) {
			t.Fatalf("Rollback = %v", err)
		}
	}
	if _, err := Abandon(ctx, ch.f, otherID, ch.opts); !errors.Is(err, ErrNothingPending) {
		t.Errorf("Abandon(other id) = %v", err)
	}
	if _, err := Abandon(ctx, ch.f, "../x", ch.opts); !errors.Is(err, ErrID) {
		t.Errorf("Abandon(a path) = %v", err)
	}
	j, err := Abandon(ctx, ch.f, testID, ch.opts)
	if err != nil || j.ID != testID {
		t.Fatalf("Abandon = %+v, %v", j, err)
	}
	if exists(ch.f, stateFile(RollbackName)) || !exists(ch.f, stateFile(AbandonedName(testID))) {
		t.Error("rollback.json was not moved aside")
	}
	if !strings.Contains(ch.log.String(), "rollback "+testID+" (SSH port 22 to 2230 on web-01.example.net) abandoned: backups restored false, 1 of 1 undo steps") {
		t.Errorf("log:\n%s", ch.log)
	}
	next := lockout()
	next.ID = otherID
	respond(ch.f, hostfake.Response{}, systemdRunArgv(otherID))
	if _, err := Arm(ctx, ch.f, next, systemd, ch.opts); err != nil {
		t.Errorf("Arm after Abandon = %v", err)
	}
	if st, err := ReadState(ctx, ch.f, ch.opts); err != nil || !reflect.DeepEqual(st.Pending.ID, otherID) {
		t.Errorf("state after: %+v, %v", st, err)
	}
}
