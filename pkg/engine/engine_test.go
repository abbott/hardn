package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// TestIdempotent: a control that observes the host plans a change, which
// Apply makes; planned again, the host already as asked, it plans nothing,
// and Apply of that plan does nothing at all: no lock, no journal, no
// command.
func TestIdempotent(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	h.AddFile(confPath, []byte(oldConf), 0o644, 0, 0)
	e := New(h, testOptions(h, nil, nil, debian13))
	ctl := fileControl{id: "example.file", path: confPath, want: newConf}
	p, err := e.Plan(ctx, []Control{ctl}, nil)
	if err != nil || p.Err() != nil || len(p.Changes) != 1 {
		t.Fatalf("first plan: %v, %v, %d changes", err, p.Err(), len(p.Changes))
	}
	want := "--- /etc/example/web.conf\n+++ /etc/example/web.conf\n@@ -1 +1,2 @@\n listen 22\n+listen 2230\n"
	if p.Changes[0].Diff != want {
		t.Errorf("diff:\n%s\nwant:\n%s", p.Changes[0].Diff, want)
	}
	res, err := e.Apply(ctx, p)
	if err != nil || res.Confirm != nil || res.Nothing {
		t.Fatalf("Apply: %+v, %v", res, err)
	}
	// Without lockout risk: journalled with no timer, no command, and the
	// journal ended by Done.
	if runs := h.Runs(); len(runs) != 0 {
		t.Errorf("commands %v; a plain change has no timer", runs)
	}
	if _, ok := h.Entry(journal); ok {
		t.Error("the journal is left after Done")
	}
	if got, _ := h.content(t, confPath); got != newConf {
		t.Errorf("%s: %q", confPath, got)
	}
	again, err := e.Plan(ctx, []Control{ctl}, nil)
	if err != nil || again.Err() != nil || !again.Empty() || len(again.Items[0].Changes) != 0 {
		t.Fatalf("second plan: %v, %v, %d changes; want none", err, again.Err(), len(again.Changes))
	}
	calls := len(h.Calls())
	res, err = e.Apply(ctx, again)
	if err != nil || !res.Nothing {
		t.Errorf("Apply of the empty plan: %+v, %v", res, err)
	}
	if n := len(h.Calls()) - calls; n != 0 {
		t.Errorf("Apply of the empty plan made %d calls", n)
	}
}

// TestRunLock: one engine apply per host at a time. A second apply while
// the first runs is refused before it plans or arms anything, naming the
// holder; once the first has finished the second goes ahead. A lock file
// left by a holder that died does not block.
func TestRunLock(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	inside, release := make(chan struct{}), make(chan struct{})
	slow := Change{ID: "example.slow/apply", Summary: "Write slowly", Risk: RiskPlain, Stage: StageFiles,
		Apply: func(ctx context.Context, sys host.System, _ Run) (Ran, error) {
			close(inside)
			<-release
			return Ran{}, nil
		}}
	first := New(h, testOptions(h, nil, nil, debian13))
	p1, err := first.Plan(ctx, []Control{&stub{id: "example.slow", title: "Slow", changes: []Change{slow}}}, nil)
	if err != nil || p1.Err() != nil {
		t.Fatal(err, p1.Err())
	}
	done := make(chan error, 1)
	go func() { _, err := first.Apply(ctx, p1); done <- err }()
	<-inside

	second := New(h, testOptions(h, nil, nil, debian13))
	other := &stub{id: "example.other", title: "Other", changes: []Change{command("example.other/run", StageFiles, RiskPlain, argv("example-tool --once"), nil)}}
	p2, err := second.Plan(ctx, []Control{other}, nil)
	if err != nil || p2.Err() == nil || !errors.Is(p2.Err(), revert.ErrPending) {
		t.Fatalf("second plan while the first is journalled: %v, %v; want ErrPending", err, p2.Err())
	}
	// The journal says the first is running; the lock refuses before it
	// even reads that.
	p2.Refused = nil
	plansBefore := other.plans
	_, err = second.Apply(ctx, p2)
	var le *revert.LockedError
	if !errors.Is(err, host.ErrLocked) || !strings.Contains(err.Error(), "pid 5000, change "+p1.ID) ||
		!errors.As(err, &le) || !le.Run || le.Holder != "pid 5000, change "+p1.ID+", since 2026-10-19T12:00:00Z" {
		t.Errorf("second Apply: %v; want ErrLocked, the run lock's *revert.LockedError, naming pid 5000 and change %s", err, p1.ID)
	}
	if other.plans != plansBefore {
		t.Error("the second apply planned again without the lock")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	p2, err = second.Plan(ctx, []Control{other}, nil)
	if err != nil || p2.Err() != nil {
		t.Fatal(err, p2.Err())
	}
	if _, err := second.Apply(ctx, p2); err != nil {
		t.Errorf("second Apply once the first finished: %v", err)
	}

	stale := newHost(t)
	stale.AddDir(runRoot, 0o700, 0, 0)
	stale.AddFile(lockFile, []byte("pid 3900, change 20261019T110000Z-5e6f7a8b, since 2026-10-19T11:00:00Z\n"), 0o600, 0, 0)
	var held string
	look := Change{ID: "example.look/apply", Summary: "Look at the lock", Risk: RiskPlain, Stage: StageFiles,
		Apply: func(ctx context.Context, sys host.System, _ Run) (Ran, error) {
			data, _, err := sys.ReadFileNoFollow(ctx, lockFile)
			held = string(data)
			return Ran{}, err
		}}
	e := New(stale, testOptions(stale, nil, nil, debian13))
	p, err := e.Plan(ctx, []Control{&stub{id: "example.look", title: "Look", changes: []Change{look}}}, nil)
	if err != nil || p.Err() != nil {
		t.Fatal(err, p.Err())
	}
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatalf("Apply over a dead holder's lock file: %v", err)
	}
	if want := fmt.Sprintf("pid 5000, change %s, since 2026-10-19T12:00:00Z\n", p.ID); held != want {
		t.Errorf("holder during the apply %q, want %q", held, want)
	}
	assertReleased(t, stale, p.ID)
}

// TestConfirmDeadline: the deadline is the systemd timer's own trigger time
// when list-timers shows it near the journal's; otherwise the journal's,
// marked approximate (and always on OpenRC, whose sleeper is not read).
func TestConfirmDeadline(t *testing.T) {
	trig := func(id string, at time.Time) string {
		return fmt.Sprintf(`[{"next":%d,"unit":"hardn-revert-%s.timer"}]`, at.UnixMicro(), id)
	}
	openrc := detect.Facts{Platform: detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported},
		Init: detect.Init{Kind: detect.OpenRC, Running: detect.Yes, Source: detect.OpenRCRunDir}}
	for _, c := range []struct {
		name    string
		facts   detect.Facts
		trigger func(id string) string
		exact   time.Duration // from testNow; 0: approximate
	}{
		{"read from the timer", debian13, func(id string) string { return trig(id, testNow.Add(182*time.Second)) }, 182 * time.Second},
		{"list-timers fails", debian13, func(string) string { return "" }, 0},
		{"another timer's row", debian13, func(string) string { return trig("20261019T110000Z-5e6f7a8b", testNow.Add(181*time.Second)) }, 0},
		{"not JSON (an older systemctl)", debian13, func(string) string { return "NEXT LEFT LAST PASSED UNIT ACTIVATES" }, 0},
		{"far from the deadline", debian13, func(id string) string { return trig(id, testNow.Add(time.Hour)) }, 0},
		{"OpenRC", openrc, func(string) string { return "" }, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			h := newHost(t)
			e := New(h, testOptions(h, nil, nil, c.facts))
			fw := command("example.port/allow", StageFirewall, RiskLockout, argv(allow2230), argv(delete2230))
			p, err := e.Plan(ctx, []Control{&stub{id: "example.port", title: "Port", changes: []Change{fw}}}, nil)
			if err != nil || p.Err() != nil {
				t.Fatal(err, p.Err())
			}
			h.trigger = c.trigger(p.ID)
			res, err := e.Apply(ctx, p)
			if err != nil || res.Confirm == nil {
				t.Fatalf("Apply: %+v, %v", res, err)
			}
			want, approx := testNow.Add(180*time.Second), true
			if c.exact != 0 {
				want, approx = testNow.Add(c.exact), false
			}
			if !res.Confirm.Deadline.Equal(want) || res.Confirm.Approximate != approx {
				t.Errorf("deadline %s, approximate %v; want %s, %v", res.Confirm.Deadline, res.Confirm.Approximate, want, approx)
			}
		})
	}
}

// TestIncompleteRollback: a rollback that cannot finish is reported, not
// hidden: the ApplyError carries revert.ErrIncomplete (which names `hardn
// rollback <id>`), is not ErrRolledBack, and the run lock is released; the
// journal keeps the rollback for `hardn rollback <id>` to take up.
func TestIncompleteRollback(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	h.failOn(delete2230)
	h.failOn("example-check")
	var log bytes.Buffer
	hs := &holds{}
	e := New(h, testOptions(h, hs, &log, debian13))
	p, err := e.Plan(ctx, []Control{&stub{id: "example.port", title: "Port", changes: []Change{
		command("example.port/allow", StageFirewall, RiskLockout, argv(allow2230), argv(delete2230)),
		command("example.port/check", StageVerify, RiskLockout, argv("example-check"), nil),
	}}}, nil)
	if err != nil || p.Err() != nil {
		t.Fatal(err, p.Err())
	}
	res, err := e.Apply(ctx, p)
	var fe *ApplyError
	if !errors.As(err, &fe) || errors.Is(err, ErrRolledBack) || !errors.Is(err, revert.ErrIncomplete) || res.Failed != fe {
		t.Fatalf("Apply: %v; want an ApplyError with revert.ErrIncomplete, not rolled back", err)
	}
	if !strings.Contains(err.Error(), "sudo hardn rollback "+p.ID) {
		t.Errorf("error does not name the command that takes the rollback up: %v", err)
	}
	if _, ok := h.Entry(revert.DefaultDir + "/" + revert.RollbackName); !ok {
		t.Error("rollback.json is gone: nothing would take the rollback up")
	}
	if !strings.Contains(log.String(), "engine: change "+p.ID+": example.port/check: apply failed") {
		t.Errorf("log:\n%s", log.String())
	}
	assertReleased(t, h, p.ID)
}

// TestRolledBackElsewhere: when another process takes the change while a
// step runs (the user's `hardn rollback <id>` finished meanwhile), the
// journal written after the step finds it gone: the apply stops there, runs
// nothing more, does not report the change as rolled back by itself, and
// names the step that ran as one the journal no longer covered.
func TestRolledBackElsewhere(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	hs := &holds{}
	e := New(h, testOptions(h, hs, nil, debian13))
	gone := command("example.port/allow", StageFirewall, RiskLockout, argv(allow2230), argv(delete2230))
	inner := gone.Apply
	gone.Apply = func(ctx context.Context, sys host.System, run Run) (Ran, error) {
		ran, err := inner(ctx, sys, run)
		_ = sys.Remove(ctx, journal) // as a finished `hardn rollback <id>` leaves it
		return ran, err
	}
	next := command("example.port/next", StageFiles, RiskLockout, argv("example-tool"), nil)
	p, err := e.Plan(ctx, []Control{&stub{id: "example.port", title: "Port", changes: []Change{gone, next}}}, nil)
	if err != nil || p.Err() != nil {
		t.Fatal(err, p.Err())
	}
	_, err = e.Apply(ctx, p)
	var fe *ApplyError
	if !errors.As(err, &fe) || errors.Is(err, ErrRolledBack) || !errors.Is(err, revert.ErrRollingBack) ||
		!errors.Is(err, revert.ErrNothingPending) || !errors.Is(err, ErrUnjournalled) {
		t.Fatalf("Apply: %v; want an ApplyError, not rolled back here, the journal gone, a step not covered", err)
	}
	if fe.Change != "example.port/allow" || fe.Step != StepJournalAfter || !fe.Rollback.Nothing {
		t.Errorf("failed at %q %q, rollback %+v", fe.Change, fe.Step, fe.Rollback)
	}
	if u := fe.Unjournalled; u == nil || u.Change != "example.port/allow" || journalWords(revert.Journal{Undo: u.Undo.Steps}) != "undo [ufw delete allow 2230/tcp] backup []" {
		t.Errorf("unjournalled %+v", fe.Unjournalled)
	}
	if !strings.Contains(err.Error(), "example.port/allow ran, and the journal does not hold what undoes it") || !strings.Contains(err.Error(), "ufw delete allow 2230/tcp") {
		t.Errorf("error does not name what may be left: %v", err)
	}
	if strings.Contains(h.runs(p.ID), "example-tool") {
		t.Error("the next change ran without its journal")
	}
	assertReleased(t, h, p.ID)
}

// TestDiff: a file's diff through pkg/diff, with /dev/null for the missing
// side; before and after for a command.
func TestDiff(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := []struct {
		name          string
		before, after *string
		want          string
	}{
		{"changed", s("a\nb\n"), s("a\nc\n"), "--- /etc/x.conf\n+++ /etc/x.conf\n@@ -1,2 +1,2 @@\n a\n-b\n+c\n"},
		{"created", nil, s("Port 2230\n"), "--- /dev/null\n+++ /etc/x.conf\n@@ -0,0 +1 @@\n+Port 2230\n"},
		{"removed", s("Port 2230\n"), nil, "--- /etc/x.conf\n+++ /dev/null\n@@ -1 +0,0 @@\n-Port 2230\n"},
		{"created empty", nil, s(""), "--- /dev/null\n+++ /etc/x.conf\n"},
		{"unchanged", s("a\n"), s("a\n"), ""},
		{"neither", nil, nil, ""},
	}
	for _, c := range cases {
		if got := FileDiff("/etc/x.conf", c.before, c.after); got != c.want {
			t.Errorf("%s:\n%s\nwant:\n%s", c.name, got, c.want)
		}
	}
	got := CommandDiff("ufw status", "Status: inactive\n", "Status: active\n22/tcp ALLOW Anywhere\n")
	want := "--- ufw status (before)\n+++ ufw status (after)\n@@ -1 +1,2 @@\n-Status: inactive\n+Status: active\n+22/tcp ALLOW Anywhere\n"
	if got != want {
		t.Errorf("command:\n%s\nwant:\n%s", got, want)
	}
}

// TestLockRefuses: a run root another user can write since the plan, and a
// lock file that is not one, stop Apply before the lock, with nothing
// changed outside the run root; the plan is not spent, so once the host is
// put right it applies.
func TestLockRefuses(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name        string
		break_, fix func(h *testHost)
		want        error
	}{
		{"a run root another user can write", func(h *testHost) { h.AddDir(runRoot, 0o777, 0, 0) },
			func(h *testHost) { h.AddDir(runRoot, 0o700, 0, 0) }, revert.ErrUnsafe},
		{"a lock file that is a directory", func(h *testHost) { h.AddDir(lockFile, 0o700, 0, 0) },
			func(h *testHost) { _ = h.System.Remove(ctx, lockFile) }, fs.ErrInvalid},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHost(t)
			e := New(h, testOptions(h, nil, nil, debian13))
			p, err := e.Plan(ctx, []Control{&stub{id: "example.x", title: "X", changes: []Change{
				command("example.x/run", StageFiles, RiskPlain, argv("example-tool"), nil)}}}, nil)
			if err != nil || p.Err() != nil {
				t.Fatal(err, p.Err())
			}
			c.break_(h)
			if _, err := e.Apply(ctx, p); !errors.Is(err, c.want) || errors.Is(err, host.ErrLocked) || !strings.HasPrefix(err.Error(), "engine: run lock: ") {
				t.Fatalf("Apply: %v, want the run lock refused with %v", err, c.want)
			}
			if got := h.changedOutside(runRoot); len(got) > 0 {
				t.Errorf("changed outside %s: %q", runRoot, got)
			}
			c.fix(h)
			if _, err := e.Apply(ctx, p); err != nil {
				t.Errorf("Apply once put right: %v", err)
			}
		})
	}
}
