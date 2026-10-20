package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/revert"
)

const (
	allow2230  = "ufw allow 2230/tcp"
	delete2230 = "ufw delete allow 2230/tcp"
	reloadWeb  = "systemctl reload example.service"
	reloadBack = "systemctl try-reload-or-restart example.service"
	oldConf    = "listen 22\n"
	newConf    = "listen 22\nlisten 2230\n"
)

func argv(s string) []string { return strings.Fields(s) }

// probe wraps c's Apply so that it notes the journal as it stood when the
// change started: written ahead of it.
func probe(c Change, at map[string]revert.Journal) Change {
	inner := c.Apply
	c.Apply = func(ctx context.Context, sys host.System, run Run) (Ran, error) {
		if st, err := revert.ReadState(ctx, sys, revert.Options{}); err == nil && st.Pending != nil {
			at[c.ID] = *st.Pending
		}
		return inner(ctx, sys, run)
	}
	return c
}

// portPlan is a synthetic port change in brief 2.3's order, each change
// probed: the firewall first (its undo journalled before and after it, as
// ufw's PreUndo and Report.Undo), the file after its backup, the reload,
// then the check of what the reload made effective.
type portPlan struct {
	at      map[string]revert.Journal
	failVer bool               // the effective-value check fails
	panics  bool               // the effective-value check panics
	written bool               // the write fails once it has written (ErrNotDurable)
	cancel  context.CancelFunc // set: Ctrl-C once the firewall change has run
}

func (pp *portPlan) controls() []Control {
	fw := command("example.port/allow", StageFirewall, RiskLockout, argv(allow2230), argv(delete2230), argv(delete2230))
	if pp.cancel != nil {
		inner := fw.Apply
		fw.Apply = func(ctx context.Context, sys host.System, run Run) (Ran, error) {
			ran, err := inner(ctx, sys, run)
			pp.cancel()
			return ran, err
		}
	}
	old := oldConf
	w := writeChange("example.port/write", confPath, &old, newConf, RiskLockout)
	w.ConfirmVia = []revert.Listener{any2230}
	if pp.written {
		inner := w.Apply
		w.Apply = func(ctx context.Context, sys host.System, run Run) (Ran, error) {
			if _, err := inner(ctx, sys, run); err != nil {
				return Ran{}, err
			}
			return Ran{}, fmt.Errorf("fixture: %w", host.ErrNotDurable)
		}
	}
	reload := command("example.port/reload", StageReload, RiskLockout, argv(reloadWeb), argv(reloadBack))
	verify := Change{ID: "example.port/verify", Summary: "Check that 2230 is listened on", Risk: RiskLockout, Stage: StageVerify,
		Apply: func(ctx context.Context, sys host.System, _ Run) (Ran, error) {
			if pp.panics {
				panic("fixture: a bug in the check")
			}
			data, err := sys.ReadFile(ctx, confPath)
			if err != nil || string(data) != newConf || pp.failVer {
				return Ran{}, errors.New("fixture: 2230 is not among the effective listeners")
			}
			return Ran{Detail: "2230 effective"}, nil
		}}
	var cs []Change
	for _, c := range []Change{verify, reload, w, fw} { // planned out of order on purpose
		cs = append(cs, probe(c, pp.at))
	}
	return []Control{&stub{id: "example.port", title: "Example port", changes: cs}}
}

// TestApplyLockout: a lockout-risk plan is applied in the plan's order
// under confirm-or-revert: the timer first, each change journalled before
// it runs, MarkApplied last, and the deadline taken from the timer.
func TestApplyLockout(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	h.AddFile(confPath, []byte(oldConf), 0o644, 0, 0)
	hs := &holds{}
	e := New(h, testOptions(h, hs, nil, debian13))
	pp := &portPlan{at: map[string]revert.Journal{}}
	p, err := e.Plan(ctx, pp.controls(), nil)
	if err != nil || p.Err() != nil {
		t.Fatalf("Plan: %v, %v", err, p.Err())
	}
	if p.Risk != RiskLockout || ids(p.Changes) != "example.port/allow example.port/write example.port/reload example.port/verify" {
		t.Fatalf("plan %v %q", p.Risk, ids(p.Changes))
	}
	trigger := testNow.Add(181*time.Second + 500*time.Millisecond)
	h.trigger = fmt.Sprintf(`[{"next":%d,"left":%d,"last":null,"passed":null,"unit":"hardn-revert-%s.timer","activates":"hardn-revert-%s.service"}]`,
		trigger.UnixMicro(), trigger.UnixMicro(), p.ID, p.ID)

	res, err := e.Apply(ctx, p)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := `systemd-run --unit=hardn-revert-<id> "--description=hardn: roll back change <id> unless confirmed" --on-active=180s --timer-property=AccuracySec=1s /usr/local/bin/hardn rollback <id>
ufw allow 2230/tcp
systemctl reload example.service
systemctl list-timers --all --output=json --no-pager hardn-revert-<id>.timer
`
	if got := h.runs(p.ID); got != want {
		t.Errorf("commands:\n%s\nwant:\n%s", got, want)
	}
	// Written ahead: each change found its own undo, and every earlier
	// one's, in the journal, last first; the write found its backup there.
	journalled := map[string]string{
		"example.port/allow":  "undo [ufw delete allow 2230/tcp] backup []",
		"example.port/write":  "undo [ufw delete allow 2230/tcp] backup [1]",
		"example.port/reload": "undo [systemctl try-reload-or-restart example.service ufw delete allow 2230/tcp] backup [1]",
		"example.port/verify": "undo [systemctl try-reload-or-restart example.service ufw delete allow 2230/tcp] backup [1]",
	}
	for id, w := range journalled {
		j, ok := pp.at[id]
		if !ok {
			t.Errorf("%s: no journal when it started", id)
			continue
		}
		if got := journalWords(j); got != w {
			t.Errorf("%s started with %s, want %s", id, got, w)
		}
		if j.Risk != revert.RiskLockout || j.Timer == nil || j.Applied != nil {
			t.Errorf("%s: journal risk %s, timer %v, applied %v", id, j.Risk, j.Timer, j.Applied)
		}
	}
	st, err := revert.ReadState(ctx, h, revert.Options{})
	if err != nil || st.Pending == nil || st.Pending.Applied == nil {
		t.Fatalf("after Apply: %+v, %v; want the change applied, waiting", st, err)
	}
	if !slices.Equal(st.Pending.ConfirmVia, []revert.Listener{any2230}) {
		t.Errorf("confirmVia %v", st.Pending.ConfirmVia)
	}
	if got, _ := h.content(t, confPath); got != newConf {
		t.Errorf("%s: %q", confPath, got)
	}
	c := res.Confirm
	if c == nil || c.ID != p.ID || c.Approximate || !c.Deadline.Equal(trigger.Truncate(time.Microsecond)) || !slices.Equal(c.Via, []revert.Listener{any2230}) {
		t.Errorf("confirm %+v; want the timer's %s, exact, via 2230", c, trigger)
	}
	if len(res.Applied) != 4 || res.Applied[3] != (Applied{Change: "example.port/verify", Detail: "2230 effective"}) || res.Failed != nil {
		t.Errorf("result %+v", res)
	}
	if hs.held != 0 {
		t.Errorf("signals held %d times on success", hs.held)
	}
	assertReleased(t, h, p.ID)
}

// journalWords renders a journal's undo commands and backup seqs.
func journalWords(j revert.Journal) string {
	var undo []string
	for _, s := range j.Undo {
		undo = append(undo, strings.Join(s.Argv, " "))
	}
	var seqs []int
	if j.Backup != nil {
		seqs = j.Backup.Seqs
	}
	return fmt.Sprintf("undo [%s] backup %v", strings.Join(undo, " "), seqs)
}

// assertReleased: the run lock was released, emptied, after the apply's
// last command, and the plan's run directory is gone.
func assertReleased(t *testing.T, h *testHost, id string) {
	t.Helper()
	calls := h.Calls()
	unlock, last := -1, -1
	for i, c := range calls {
		switch c.Op {
		case "unlock":
			unlock = i
		case "run":
			last = i
		}
	}
	if unlock < 0 || unlock < last {
		t.Errorf("run lock released at call %d, the last command at %d", unlock, last)
	}
	if got, ok := h.content(t, lockFile); !ok || got != "" {
		t.Errorf("lock file %q (exists %v), want it left, empty", got, ok)
	}
	if _, ok := h.Entry(runRoot + "/" + id); ok {
		t.Errorf("the run directory %s is left", id)
	}
}

// TestFailureRollsBack: a failure at each stage, and Ctrl-C between two
// changes, rolls back every change started, in reverse, from the journal,
// with signals held: the file is as it was, ufw's rule deleted, the timer
// stopped, the journal gone; the journal at the moment of the rollback
// held exactly the started changes' undo.
func TestFailureRollsBack(t *testing.T) {
	cases := []struct {
		name         string
		setup        func(h *testHost, pp *portPlan, cancel context.CancelFunc)
		change, step string
		journalled   string // the journal the rollback started from
		rollback     string // the commands after the failure
	}{
		{"the firewall", func(h *testHost, _ *portPlan, _ context.CancelFunc) { h.failOn(allow2230) },
			"example.port/allow", StepApply, "undo [ufw delete allow 2230/tcp] backup []", delete2230},
		{"a backup", func(h *testHost, _ *portPlan, _ context.CancelFunc) {
			_ = h.Remove(context.Background(), confPath)
			h.AddDir(confPath, 0o755, 0, 0) // a backup refuses a directory
		}, "example.port/write", StepBackup, "undo [ufw delete allow 2230/tcp] backup []", delete2230},
		{"the write", func(_ *testHost, pp *portPlan, _ context.CancelFunc) { pp.written = true },
			"example.port/write", StepApply, "undo [ufw delete allow 2230/tcp] backup [1]", delete2230},
		{"the reload", func(h *testHost, _ *portPlan, _ context.CancelFunc) { h.failOn(reloadWeb) },
			"example.port/reload", StepApply, "undo [systemctl try-reload-or-restart example.service ufw delete allow 2230/tcp] backup [1]",
			reloadBack + "\n" + delete2230},
		{"the check of the effective values", func(_ *testHost, pp *portPlan, _ context.CancelFunc) { pp.failVer = true },
			"example.port/verify", StepApply, "undo [systemctl try-reload-or-restart example.service ufw delete allow 2230/tcp] backup [1]",
			reloadBack + "\n" + delete2230},
		{"a panic in the check", func(_ *testHost, pp *portPlan, _ context.CancelFunc) { pp.panics = true },
			"example.port/verify", StepApply, "undo [systemctl try-reload-or-restart example.service ufw delete allow 2230/tcp] backup [1]",
			reloadBack + "\n" + delete2230},
		{"MarkApplied", func(h *testHost, _ *portPlan, _ context.CancelFunc) {
			h.AddDir("/proc/4100", 0o555, 0, 0) // a process whose stat cannot be read
		}, "", StepMarkApplied, "undo [systemctl try-reload-or-restart example.service ufw delete allow 2230/tcp] backup [1]",
			reloadBack + "\n" + delete2230},
		{"Ctrl-C after the firewall", func(_ *testHost, pp *portPlan, cancel context.CancelFunc) { pp.cancel = cancel },
			"example.port/write", StepStart, "undo [ufw delete allow 2230/tcp] backup []", delete2230},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h := newHost(t)
			h.AddFile(confPath, []byte(oldConf), 0o644, 0, 0)
			hs := &holds{}
			e := New(h, testOptions(h, hs, nil, debian13))
			pp := &portPlan{at: map[string]revert.Journal{}}
			c.setup(h, pp, cancel)
			p, err := e.Plan(ctx, pp.controls(), nil)
			if err != nil || p.Err() != nil {
				t.Fatalf("Plan: %v, %v", err, p.Err())
			}
			before := len(h.Runs())
			res, err := e.Apply(ctx, p)
			var fe *ApplyError
			if !errors.As(err, &fe) || !errors.Is(err, ErrRolledBack) || res.Failed != fe {
				t.Fatalf("Apply: %v; want an ApplyError, rolled back", err)
			}
			if fe.Change != c.change || fe.Step != c.step || fe.ID != p.ID {
				t.Errorf("failed at %q %q, want %q %q", fe.Change, fe.Step, c.change, c.step)
			}
			if hs.held != 1 || hs.released != 1 {
				t.Errorf("signals held %d, released %d; want once each", hs.held, hs.released)
			}
			if hs.pending == nil || hs.rolling != nil {
				t.Fatalf("at the rollback: pending %v, rolling back %v", hs.pending, hs.rolling)
			}
			if got := journalWords(*hs.pending); got != c.journalled {
				t.Errorf("journal at the rollback: %s, want %s", got, c.journalled)
			}
			runs := h.Runs()
			var after []string
			for _, r := range runs[before:] {
				after = append(after, strings.Join(r, " "))
			}
			got := strings.Join(after, "\n")
			wantTail := c.rollback + "\nsystemctl stop hardn-revert-" + p.ID + ".timer"
			if !strings.HasSuffix(got, wantTail) {
				t.Errorf("commands after Arm:\n%s\nwant them to end with the rollback:\n%s", got, wantTail)
			}
			if c.step != StepBackup {
				if got, _ := h.content(t, confPath); got != oldConf {
					t.Errorf("%s after the rollback: %q, want %q", confPath, got, oldConf)
				}
			}
			for _, name := range []string{revert.PendingName, revert.RollbackName} {
				if _, ok := h.Entry(revert.DefaultDir + "/" + name); ok {
					t.Errorf("%s left after the rollback", name)
				}
			}
			assertReleased(t, h, p.ID)
		})
	}
}

// TestCancelledDuringApply: Ctrl-C during a step. The step's journal of
// what it did (ufw's Report.Undo, here two rules) is written after the
// cancellation, under a context of its own, so the rollback undoes both.
func TestCancelledDuringApply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newHost(t)
	hs := &holds{}
	e := New(h, testOptions(h, hs, nil, debian13))
	fw := Change{ID: "example.port/allow", Summary: "Allow 22 and 2230", Risk: RiskLockout, Stage: StageFirewall,
		Rollback: Undo{Steps: runSteps(argv(delete2230))},
		Apply: func(ctx context.Context, sys host.System, _ Run) (Ran, error) {
			_, err := sys.Run(ctx, "ufw", "allow", "22/tcp")
			if err == nil {
				_, err = sys.Run(ctx, "ufw", "allow", "2230/tcp")
			}
			cancel() // Ctrl-C while ufw ran: it added both rules
			u := Undo{Steps: runSteps(argv(delete2230), argv("ufw delete allow 22/tcp"))}
			return Ran{Rollback: &u}, errors.Join(err, ctx.Err())
		}}
	p, err := e.Plan(ctx, []Control{&stub{id: "example.port", title: "Example port", changes: []Change{fw}}}, nil)
	if err != nil || p.Err() != nil {
		t.Fatalf("Plan: %v, %v", err, p.Err())
	}
	_, err = e.Apply(ctx, p)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrRolledBack) {
		t.Fatalf("Apply: %v; want cancelled and rolled back", err)
	}
	if hs.pending == nil {
		t.Fatal("no journal at the rollback")
	}
	if got, want := journalWords(*hs.pending), "undo [ufw delete allow 2230/tcp ufw delete allow 22/tcp] backup []"; got != want {
		t.Errorf("journal at the rollback %s, want %s (written after the cancellation)", got, want)
	}
	if got := h.runs(p.ID); !strings.Contains(got, "ufw delete allow 2230/tcp\nufw delete allow 22/tcp\nsystemctl stop hardn-revert-<id>.timer\n") {
		t.Errorf("commands:\n%s", got)
	}
}

// TestPlanStepDropped: a unit's plan step is journalled before its apply;
// an apply refused before it wrote anything drops it (an empty
// Ran.Rollback), so the unit's rollback does not run; an apply that failed
// with the step kept has it run (revert-plans.md §6).
func TestPlanStepDropped(t *testing.T) {
	for _, c := range []struct {
		name     string
		drop     bool
		rollback int
	}{{"refused before writing: dropped", true, 0}, {"failed after writing: kept", false, 1}} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			h := newHost(t)
			hs := &holds{}
			o := testOptions(h, hs, nil, debian13)
			rolled := 0
			o.Revert.Plans = map[string]revert.PlanRollback{"example": func(context.Context, host.System, json.RawMessage) error {
				rolled++
				return nil
			}}
			e := New(h, o)
			ch := Change{ID: "example.dns/apply", Summary: "Set the resolvers", Risk: RiskPlain, Stage: StageFiles,
				Rollback: Undo{Steps: []revert.Step{{Op: revert.OpPlan, Unit: "example", Plan: []byte(`{"want":["192.0.2.53"]}`)}}},
				Apply: func(context.Context, host.System, Run) (Ran, error) {
					var ran Ran
					if c.drop {
						ran.Rollback = &Undo{}
					}
					return ran, errors.New("fixture: the unit refused")
				}}
			p, err := e.Plan(ctx, []Control{&stub{id: "example.dns", title: "DNS", changes: []Change{ch}}}, nil)
			if err != nil || p.Err() != nil {
				t.Fatalf("Plan: %v, %v", err, p.Err())
			}
			if _, err := e.Apply(ctx, p); !errors.Is(err, ErrRolledBack) {
				t.Fatalf("Apply: %v", err)
			}
			if rolled != c.rollback {
				t.Errorf("the unit's rollback ran %d times, want %d", rolled, c.rollback)
			}
			if len(h.Runs()) != 0 {
				t.Errorf("commands: %v; a plan without lockout risk has no timer", h.Runs())
			}
		})
	}
}
