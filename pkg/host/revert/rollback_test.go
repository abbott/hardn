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
	"github.com/abbott/hardn/pkg/host/reload"
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

// listenConf is v0.3.x's ssh.socket drop-in, as v0.3.2 writes it for port
// 2230.
const (
	listenConf     = "/etc/systemd/system/ssh.socket.d/listen.conf"
	listenConfData = "[Socket]\nListenStream=\nListenStream=2230\n"
)

// migratedSocketHost is an Ubuntu host (ssh.socket, the generator) whose
// listen.conf a migration removed, with 00-hardn.conf written in its place
// and ssh.socket restarted: the journal names both backups and the reload,
// whose After is the socket's listener before the migration ([::]:2230,
// listen.conf's). socketDropIns is the reload's SocketDropIns.
func migratedSocketHost(t *testing.T, socketDropIns []string) *change {
	t.Helper()
	ctx := context.Background()
	ch := &change{f: newHost(t), c: &clock{testNow}, log: &bytes.Buffer{}}
	f := ch.f
	f.AddFile(listenConf, []byte(listenConfData), 0o644, 0, 0)
	ch.opts = testOptions(ch.c, ch.log)
	ch.opts.DetectSSH = func(context.Context, host.System) detect.SSH {
		return detect.SSH{Mode: detect.SSHSocket, Generator: detect.Yes, GeneratorPath: detect.SocketGenerator,
			Socket: detect.UnitState{Enabled: "enabled", Active: "active"}, Service: detect.UnitState{Enabled: "disabled", Active: "active"}}
	}
	ch.j = armed(t, f, ch.c)
	run, err := backup.Open(f, backup.Settings{Path: backup.DefaultPath, Enabled: true}, testID)
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int
	for _, p := range []string{listenConf, dropIn} {
		r, err := run.Backup(ctx, p, backup.RollbackRequired)
		if err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, r.Entry.Seq)
	}
	ch.j.Backup = &Backups{Dir: run.Dir(), Seqs: seqs}
	ch.j.Reload = []Reload{{Service: ReloadSSH, Keywords: []string{"port", "listenaddress", "authorizedkeysfile"},
		After: []Listener{{Addr: netip.IPv6Unspecified(), Port: 2230}}, SocketDropIns: socketDropIns}}
	if err := Record(ctx, f, ch.j, ch.opts); err != nil {
		t.Fatal(err)
	}
	f.AddFile(dropIn, []byte("ListenAddress 0.0.0.0:2230\nListenAddress [::]:2230\n"), 0o644, 0, 0)
	if err := f.Remove(ctx, listenConf); err != nil {
		t.Fatal(err)
	}
	f.Respond(hostfake.Response{}, "systemctl", "stop", "hardn-revert-"+testID+".timer")
	return ch
}

// TestRollbackRestoresListenConf: the rollback of a listen.conf migration
// on a socket-activated host restores listen.conf and 00-hardn.conf's
// absence, then reloads with listen.conf in place: daemon-reload, the
// socket's configuration checked against the listener it had before,
// ssh.socket restarted, the listener bound. Without the journal naming
// listen.conf as restored, the reload is refused as before
// (reload.ErrSocketDropIn, next/p2-vmtest's probe) and the rollback stays
// incomplete, with nothing run.
func TestRollbackRestoresListenConf(t *testing.T) {
	ctx := context.Background()
	ch := migratedSocketHost(t, []string{listenConf})
	f := ch.f
	f.Respond(hostfake.Response{}, "systemctl", "daemon-reload")
	f.Respond(hostfake.Response{Stdout: "Listen=[::]:2230 (Stream)\n"}, "systemctl", "show", "-p", "Listen", "ssh.socket")
	f.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", "ssh.socket")
	f.Respond(hostfake.Response{}, "systemctl", "restart", "ssh.socket")
	f.Respond(hostfake.Response{Stdout: "LISTEN 0      4096                *:2230              *:*\n"}, "ss", "-Htln")
	res, err := Rollback(ctx, f, testID, ch.opts)
	if err != nil || len(res.Reloads) != 1 || !res.Reloads[0].Verified {
		t.Fatalf("Rollback = %+v, %v\n%s", res, err, runs(f))
	}
	if e, ok := f.Entry(listenConf); !ok || string(e.Data) != listenConfData || exists(f, dropIn) {
		t.Errorf("listen.conf restored %v (%q), 00-hardn.conf left %v", ok, e.Data, exists(f, dropIn))
	}
	got, at := runs(f), 0
	for _, want := range []string{"run systemctl daemon-reload", "run systemctl show -p Listen ssh.socket", "run systemctl restart ssh.socket", "run ss -Htln"} {
		i := strings.Index(got[at:], want)
		if i < 0 {
			t.Fatalf("%q not in order in:\n%s", want, got)
		}
		at += i
	}
	if exists(f, stateFile(RollbackName)) || exists(f, stateFile(PendingName)) {
		t.Error("the journal is left behind")
	}

	ch = migratedSocketHost(t, nil)
	_, err = Rollback(ctx, ch.f, testID, ch.opts)
	if !errors.Is(err, ErrIncomplete) || !errors.Is(err, reload.ErrSocketDropIn) || strings.Contains(runs(ch.f), "daemon-reload") {
		t.Errorf("Rollback without socketDropIns = %v; runs:\n%s", err, runs(ch.f))
	}
	if !exists(ch.f, listenConf) || !exists(ch.f, stateFile(RollbackName)) {
		t.Error("the restore did not run, or the incomplete rollback was cleared")
	}

	// A journal naming a drop-in its backups did not restore (an
	// administrator's, with a ListenStream line of its own) is refused
	// before the reload: only what the rollback put back is exempt.
	admin := "/etc/systemd/system/ssh.socket.d/zz-admin.conf"
	ch = migratedSocketHost(t, []string{listenConf, admin})
	ch.f.AddFile(admin, []byte("[Socket]\nListenStream=2231\n"), 0o644, 0, 0)
	_, err = Rollback(ctx, ch.f, testID, ch.opts)
	if !errors.Is(err, ErrIncomplete) || !errors.Is(err, ErrJournal) || !strings.Contains(err.Error(), admin) || strings.Contains(runs(ch.f), "daemon-reload") {
		t.Errorf("Rollback naming %s = %v; runs:\n%s", admin, err, runs(ch.f))
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
		_, err := Rollback(ctx, ch.f, testID, ch.opts)
		if !errors.Is(err, ErrIncomplete) {
			t.Fatalf("Rollback = %v", err)
		}
		// The error names the flag, not the manual step (P-35).
		if !strings.Contains(err.Error(), "give it up, leaving the host as it is, with `sudo hardn rollback --abandon "+testID+"`") || strings.Contains(err.Error(), "aside") {
			t.Errorf("Rollback's error = %v", err)
		}
	}
	if _, err := Abandon(ctx, ch.f, otherID, ch.opts); !errors.Is(err, ErrNothingPending) {
		t.Errorf("Abandon(other id) = %v", err)
	}
	if _, err := Abandon(ctx, ch.f, "../x", ch.opts); !errors.Is(err, ErrID) {
		t.Errorf("Abandon(a path) = %v", err)
	}
	calls := len(ch.f.Calls())
	j, err := Abandon(ctx, ch.f, testID, ch.opts)
	if err != nil || j.ID != testID {
		t.Fatalf("Abandon = %+v, %v", j, err)
	}
	if exists(ch.f, stateFile(RollbackName)) || !exists(ch.f, stateFile(AbandonedName(testID))) {
		t.Error("rollback.json was not moved aside")
	}
	if did := changes(ch.f.Calls()[calls:]); !reflect.DeepEqual(did, []string{"rename " + stateFile(RollbackName) + " " + stateFile(AbandonedName(testID))}) {
		t.Errorf("Abandon did %q; want its rename alone", did)
	}
	// The record is kept, with the rollback's progress, and is what Abandon
	// returned: what the rollback did and did not undo.
	if kept := readJournalT(t, ch.f, stateFile(AbandonedName(testID))); !reflect.DeepEqual(kept, j) || kept.Rollback == nil ||
		kept.Rollback.Restored || kept.Rollback.UndoDone != 1 || kept.Rollback.ReloadDone != 0 {
		t.Errorf("kept %+v, returned %+v", kept, j)
	}
	done, left := Unfinished(j)
	if want := []string{"undo 1 of 1: " + ufwUndo}; !reflect.DeepEqual(done, want) {
		t.Errorf("done %q, want %q", done, want)
	}
	if want := []string{"restore the backups in " + ch.j.Backup.Dir + " (entry 1)", "reload ssh (Port, PasswordAuthentication)"}; !reflect.DeepEqual(left, want) {
		t.Errorf("left %q, want %q", left, want)
	}
	if !strings.Contains(ch.log.String(), "rollback "+testID+" (SSH port 22 to 2230 on web-01.example.net) abandoned: backups restored false, 1 of 1 undo steps and 0 of 1 reloads done; kept in "+stateFile(AbandonedName(testID))) {
		t.Errorf("log:\n%s", ch.log)
	}
	// Once abandoned, there is nothing left to give up, and it says where
	// the record is.
	if _, err := Abandon(ctx, ch.f, testID, ch.opts); !errors.Is(err, ErrNothingPending) || !strings.Contains(err.Error(), "abandoned already, and its record is kept in "+stateFile(AbandonedName(testID))) {
		t.Errorf("Abandon again = %v", err)
	}
	if res, err := Rollback(ctx, ch.f, testID, ch.opts); err != nil || !res.Nothing {
		t.Errorf("Rollback after Abandon = %+v, %v", res, err)
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

// changes lists what calls changed or ran: commands run or started, and
// paths written, removed, renamed, created, or given a mode or owner.
func changes(calls []hostfake.Call) []string {
	var out []string
	for _, c := range calls {
		switch c.Op {
		case "run", "start-detached":
			out = append(out, c.Op+" "+host.FormatArgv(c.Argv))
		case "rename":
			out = append(out, c.Op+" "+c.Path+" "+c.To)
		case "write-atomic", "write-in-place", "remove", "removeall", "mkdir", "chmod", "chown":
			out = append(out, c.Op+" "+c.Path)
		}
	}
	return out
}

// unfinishedRollback is newChange whose rollback did not finish and never
// can: its backup run directory is gone, so the backup is not restored and
// ssh not reloaded; ufw's rule was deleted. Its runner, process 5000, has
// gone.
func unfinishedRollback(t *testing.T) *change {
	t.Helper()
	ch := newChange(t)
	ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
	if err := ch.f.RemoveAll(context.Background(), ch.j.Backup.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Rollback(context.Background(), ch.f, testID, ch.opts); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("Rollback = %v", err)
	}
	return ch
}

// Abandon gives up nothing but an unfinished rollback of the change it
// names, one whose process has gone, and each refusal says what to do
// instead; a refusal changes nothing.
func TestAbandonRefused(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T) (*hostfake.System, Options)
		id    string
		is    error
		says  string
	}{
		{"nothing journalled", func(t *testing.T) (*hostfake.System, Options) {
			return newHost(t), testOptions(&clock{testNow}, nil)
		}, testID, ErrNothingPending, "there is no unfinished rollback of it to give up: it is not journalled: confirmed, rolled back already"},
		{"the change waits for confirmation", func(t *testing.T) (*hostfake.System, Options) {
			f, c := newHost(t), &clock{testNow}
			armed(t, f, c)
			return f, testOptions(c, nil)
		}, testID, ErrNothingPending, "change " + testID + " is journalled and its rollback has not started; to undo it, run `sudo hardn rollback " + testID + "`"},
		{"another change waits", func(t *testing.T) (*hostfake.System, Options) {
			f, c := newHost(t), &clock{testNow}
			armed(t, f, c)
			return f, testOptions(c, nil)
		}, otherID, ErrNothingPending, "the journalled change is " + testID + ", not this one, and its rollback has not started"},
		{"the unfinished rollback is another change's", func(t *testing.T) (*hostfake.System, Options) {
			ch := unfinishedRollback(t)
			return ch.f, ch.opts
		}, otherID, ErrNothingPending, "the unfinished rollback is of change " + testID + ", not this one; finish it with `sudo hardn rollback " + testID +
			"`, or, if it cannot finish, give it up with `sudo hardn rollback --abandon " + testID + "`"},
		{"the timer rolled it back", func(t *testing.T) (*hostfake.System, Options) {
			ch := newChange(t)
			ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
			ch.c.t = ch.j.Deadline()
			if _, err := Rollback(ctx, ch.f, testID, ch.opts); err != nil {
				t.Fatal(err)
			}
			return ch.f, ch.opts
		}, testID, ErrNothingPending, "it was rolled back at 2026-10-18 12:03:00 UTC"},
		{"its rollback is running", func(t *testing.T) (*hostfake.System, Options) {
			ch := unfinishedRollback(t)
			proc(ch.f, selfPID, 3980, "hardn", ptsTTY) // process 5000 runs on
			return ch.f, ch.opts
		}, testID, ErrRollingBack, "process 5000 is rolling it back now (started 2026-10-18 12:00:00 UTC); wait for it to end, and if it does not finish, run `sudo hardn rollback " +
			testID + "` once more before giving it up"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, opts := tt.setup(t)
			calls := len(f.Calls())
			_, err := Abandon(ctx, f, tt.id, opts)
			if !errors.Is(err, tt.is) || !strings.Contains(err.Error(), tt.says) {
				t.Fatalf("Abandon = %v; want %v saying %q", err, tt.is, tt.says)
			}
			if did := changes(f.Calls()[calls:]); len(did) != 0 {
				t.Errorf("a refused Abandon did %q", did)
			}
			if exists(f, stateFile(AbandonedName(tt.id))) {
				t.Error("an abandoned record was written")
			}
		})
	}
}

// Unfinished splits the rollback's parts, in its order, by its record.
func TestUnfinished(t *testing.T) {
	j := Journal{ID: testID, Summary: "SSH port 22 to 2230 on web-01.example.net",
		Backup: &Backups{Dir: "/var/backups/hardn/" + testID, Seqs: []int{1, 2}},
		Undo: []Step{{Op: OpRun, Argv: strings.Fields(ufwUndo)}, {Op: OpPlan, Unit: "sudoers", Plan: json.RawMessage(`{}`)},
			{Op: OpRemoveDir, Path: "/home/alice/.ssh"}},
		Reload: []Reload{{Service: ReloadSSH, Keywords: []string{"Port", "PasswordAuthentication"}}, {Service: ReloadSSH}}}
	var (
		restore = "restore the backups in /var/backups/hardn/" + testID + " (entries 1, 2)"
		undo1   = "undo 1 of 3: " + ufwUndo
		undo2   = "undo 2 of 3: roll back the sudoers plan"
		undo3   = "undo 3 of 3: rmdir /home/alice/.ssh"
		reload1 = "reload ssh (Port, PasswordAuthentication)"
		reload2 = "reload ssh"
	)
	for _, tt := range []struct {
		name       string
		progress   *Progress
		done, left []string
	}{
		{"no progress recorded", nil, nil, []string{restore, undo1, undo2, undo3, reload1, reload2}},
		{"nothing done", &Progress{}, nil, []string{restore, undo1, undo2, undo3, reload1, reload2}},
		{"the backups not restored, one undo done", &Progress{UndoDone: 1}, []string{undo1}, []string{restore, undo2, undo3, reload1, reload2}},
		{"stopped at the second undo", &Progress{Restored: true, UndoDone: 1}, []string{restore, undo1}, []string{undo2, undo3, reload1, reload2}},
		{"stopped at the second reload", &Progress{Restored: true, UndoDone: 3, ReloadDone: 1}, []string{restore, undo1, undo2, undo3, reload1}, []string{reload2}},
		{"every part done", &Progress{Restored: true, UndoDone: 3, ReloadDone: 2}, []string{restore, undo1, undo2, undo3, reload1, reload2}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			j := j
			j.Rollback = tt.progress
			done, left := Unfinished(j)
			if !reflect.DeepEqual(done, tt.done) || !reflect.DeepEqual(left, tt.left) {
				t.Errorf("Unfinished = %q, %q; want %q, %q", done, left, tt.done, tt.left)
			}
		})
	}
	// A change with no backups has no restore to list.
	if done, left := Unfinished(Journal{ID: testID, Undo: j.Undo[:1], Rollback: &Progress{UndoDone: 1}}); !reflect.DeepEqual(done, []string{"undo 1 of 1: " + ufwUndo}) || left != nil {
		t.Errorf("Unfinished with no backups = %q, %q", done, left)
	}
}
