package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/revert"
)

// TestWindow: a lockout-risk plan does not run into the last
// Options.Margin of its confirm window. No change, nor MarkApplied, starts
// there, and a change still running there is stopped by its context: each
// time the engine rolls the change back itself (ReasonFailed), before the
// timer's rollback could race a change, and no later change runs.
func TestWindow(t *testing.T) {
	window := revert.DefaultTimeout - DefaultMargin // 160 s from the journal's creation
	cases := []struct {
		name         string
		at, checkAt  time.Duration // where the firewall change, and the check (0: no change), leave the clock
		blocks       bool          // the check waits for its context to end
		change, step string
		cause        error
	}{
		{"the next change would start in the margin", window + time.Second, 0, false, "example.port/write", StepWindow, nil},
		{"MarkApplied would come in the margin", window - time.Second, window, false, "", StepWindow, nil},
		{"a change runs into the margin", window - 100*time.Millisecond, 0, true, "example.port/check", StepApply, context.DeadlineExceeded},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			h := newHost(t)
			h.AddFile(confPath, []byte(oldConf), 0o644, 0, 0)
			hs := &holds{}
			clk := &clock{t: testNow}
			o := testOptions(h, hs, nil, debian13)
			o.Revert.Now = clk.now
			e := New(h, o)
			fw := command("example.port/allow", StageFirewall, RiskLockout, argv(allow2230), argv(delete2230))
			inner := fw.Apply
			fw.Apply = func(ctx context.Context, sys host.System, run Run) (Ran, error) {
				ran, err := inner(ctx, sys, run)
				clk.set(testNow.Add(c.at))
				return ran, err
			}
			old := oldConf
			w := writeChange("example.port/write", confPath, &old, newConf, RiskLockout)
			check := Change{ID: "example.port/check", Summary: "Check 2230", Risk: RiskLockout, Stage: StageVerify,
				Apply: func(ctx context.Context, _ host.System, _ Run) (Ran, error) {
					if c.checkAt != 0 {
						clk.set(testNow.Add(c.checkAt))
					}
					if c.blocks {
						<-ctx.Done()
						return Ran{}, ctx.Err()
					}
					return Ran{}, nil
				}}
			p, err := e.Plan(ctx, []Control{&stub{id: "example.port", title: "Port", changes: []Change{fw, w, check}}}, nil)
			if err != nil || p.Err() != nil {
				t.Fatalf("Plan: %v, %v", err, p.Err())
			}
			start := time.Now()
			res, err := e.Apply(ctx, p)
			var fe *ApplyError
			if !errors.As(err, &fe) || !errors.Is(err, ErrWindow) || !errors.Is(err, ErrRolledBack) || res.Confirm != nil {
				t.Fatalf("Apply: %+v, %v; want ErrWindow, rolled back here, no confirmation", res, err)
			}
			if c.cause != nil && !errors.Is(err, c.cause) {
				t.Errorf("Apply: %v; want %v too", err, c.cause)
			}
			if fe.Change != c.change || fe.Step != c.step {
				t.Errorf("failed at %q %q, want %q %q", fe.Change, fe.Step, c.change, c.step)
			}
			if fe.Rollback.Reason != revert.ReasonFailed || hs.held != 1 {
				t.Errorf("rollback reason %q, signals held %d; want the rollback here, once", fe.Rollback.Reason, hs.held)
			}
			if c.blocks && time.Since(start) > 5*time.Second {
				t.Errorf("the step was stopped after %s", time.Since(start))
			}
			if c.step == StepWindow && c.change != "" {
				if got, _ := h.content(t, confPath); got != oldConf {
					t.Errorf("%s: %q; the write ran in the margin", confPath, got)
				}
			}
			if got, want := h.runs(p.ID), "ufw delete allow 2230/tcp\nsystemctl stop hardn-revert-<id>.timer\n"; !strings.HasSuffix(got, want) {
				t.Errorf("commands:\n%s\nwant them to end with:\n%s", got, want)
			}
			for _, name := range []string{revert.PendingName, revert.RollbackName, revert.RevertedName} {
				if _, ok := h.Entry(revert.DefaultDir + "/" + name); ok {
					t.Errorf("%s left", name)
				}
			}
			assertReleased(t, h, p.ID)
		})
	}
}

// TestTimerRollbackOverlaps: the timer's rollback runs while a change is in
// flight (here before the change's write: the timer fired, the clock having
// jumped). The write lands after the rollback restored the file. The
// engine cannot undo that without a journal, so it says so: the journal
// written after the step finds the change rolled back by the timer, and the
// ApplyError names the write as not covered, with the backup that restores
// it; the host is left as that says.
func TestTimerRollbackOverlaps(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	h.AddFile(confPath, []byte(oldConf), 0o644, 0, 0)
	hs := &holds{}
	e := New(h, testOptions(h, hs, nil, debian13))
	fw := command("example.port/allow", StageFirewall, RiskLockout, argv(allow2230), argv(delete2230))
	old := oldConf
	w := writeChange("example.port/write", confPath, &old, newConf, RiskLockout)
	inner := w.Apply
	w.Apply = func(ctx context.Context, sys host.System, run Run) (Ran, error) {
		// The timer's `hardn rollback <id>`, past the deadline.
		if _, err := revert.Rollback(ctx, sys, run.ID, revert.Options{Now: func() time.Time { return testNow.Add(181 * time.Second) }}); err != nil {
			t.Errorf("the timer's rollback: %v", err)
		}
		return inner(ctx, sys, run)
	}
	reload := command("example.port/reload", StageReload, RiskLockout, argv(reloadWeb), argv(reloadBack))
	p, err := e.Plan(ctx, []Control{&stub{id: "example.port", title: "Port", changes: []Change{fw, w, reload}}}, nil)
	if err != nil || p.Err() != nil {
		t.Fatalf("Plan: %v, %v", err, p.Err())
	}
	_, err = e.Apply(ctx, p)
	var fe *ApplyError
	if !errors.As(err, &fe) || errors.Is(err, ErrRolledBack) || !errors.Is(err, ErrUnjournalled) || !errors.Is(err, revert.ErrRollingBack) {
		t.Fatalf("Apply: %v; want the write named as not covered, not rolled back here", err)
	}
	if fe.Change != "example.port/write" || fe.Step != StepJournalAfter {
		t.Errorf("failed at %q %q", fe.Change, fe.Step)
	}
	u := fe.Unjournalled
	if u == nil || u.Change != "example.port/write" || len(u.Backups) != 1 || u.Backups[0].Path != confPath || u.Backups[0].Seq != 1 ||
		u.Dir != "/var/backups/hardn/"+p.ID {
		t.Fatalf("unjournalled %+v", u)
	}
	if want := "restore " + confPath + " from backup 1 in /var/backups/hardn/" + p.ID; !strings.Contains(err.Error(), want) {
		t.Errorf("error does not say %q: %v", want, err)
	}
	// The host as the error says: the write left, the firewall's rule
	// deleted by the timer's rollback, the reload never run.
	if got, _ := h.content(t, confPath); got != newConf {
		t.Errorf("%s: %q", confPath, got)
	}
	want := "ufw allow 2230/tcp\nufw delete allow 2230/tcp\n"
	if got := h.runs(p.ID); !strings.HasSuffix(got, want) || strings.Contains(got, reloadWeb) {
		t.Errorf("commands:\n%s\nwant them to end with:\n%s", got, want)
	}
	for name, left := range map[string]bool{revert.PendingName: false, revert.RollbackName: false, revert.RevertedName: true} {
		if _, ok := h.Entry(revert.DefaultDir + "/" + name); ok != left {
			t.Errorf("%s left: %v, want %v", name, ok, left)
		}
	}
	assertReleased(t, h, p.ID)
}
