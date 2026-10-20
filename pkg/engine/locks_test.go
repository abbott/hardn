package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The run lock and the journal lock during an apply (next/p3-engine-boot;
// docs/engine/engine-boot.md §2.2): `hardn rollback <id>` (the timer's or
// the user's), `--abandon` and `--boot`, run while an apply holds the run
// lock, take only the journal lock, which the apply does not hold between
// its journal writes, so none waits for the apply; and an apply's Arm waits
// for a rollback's short hold of the journal lock, then arms.

const journalLock = revert.DefaultDir + "/" + revert.JournalLockName

// locksSince are the lock calls from call number from: the file, and
// whether it was found held.
func locksSince(h *testHost, from int) []string {
	var out []string
	for _, c := range h.Calls()[from:] {
		if c.Op == "lock" {
			s := c.Path
			if errors.Is(c.Err, host.ErrLocked) {
				s += " held"
			}
			out = append(out, s)
		}
	}
	return out
}

// TestLocksDuringApply: each command that takes the journal lock, run in
// the middle of a change of a lockout-risk apply, which holds the run lock:
// it takes the journal lock at once, never the run lock but --boot's try,
// and does what it does with no wait; the apply then goes on, or reports
// the change rolled back by the other process.
func TestLocksDuringApply(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(h *testHost, id string)
		other  func(ctx context.Context, h *testHost, id string) error
		locks  string // the other's lock calls
		failed bool   // the apply fails: rolled back by the other
	}{
		{"the timer's rollback", nil, func(ctx context.Context, h *testHost, id string) error {
			_, err := revert.Rollback(ctx, h, id, revert.Options{Now: func() time.Time { return testNow.Add(181 * time.Second) }})
			return err
		}, journalLock, true},
		{"--abandon", nil, func(ctx context.Context, h *testHost, id string) error {
			_, err := revert.Abandon(ctx, h, id, revert.Options{})
			var nr *revert.NoRollbackError
			if !errors.As(err, &nr) || nr.Kind != revert.NoRollbackNotStarted {
				return err
			}
			return nil
		}, journalLock, false},
		{"--boot, the timer there", func(h *testHost, id string) {
			h.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", revert.UnitPrefix+id+".timer")
		}, func(ctx context.Context, h *testHost, id string) error {
			res, err := revert.Boot(ctx, h, debian13.Init, revert.Options{Hardn: testHardn, Now: func() time.Time { return testNow }})
			if err == nil && (res.Action != revert.BootNothing || res.Reason != revert.BootReasonTimerRuns || res.Removed) {
				t.Errorf("Boot = %+v", res)
			}
			return err
		}, lockFile + " held " + journalLock, false},
		{"--boot, the timer not there yet", func(h *testHost, id string) {
			h.Respond(hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, "systemctl", "is-active", revert.UnitPrefix+id+".timer")
		}, func(ctx context.Context, h *testHost, id string) error {
			res, err := revert.Boot(ctx, h, debian13.Init, revert.Options{Hardn: testHardn, Now: func() time.Time { return testNow }})
			if err == nil && (res.Action != revert.BootNothing || res.Reason != revert.BootReasonApplyRuns || res.Removed) {
				t.Errorf("Boot = %+v", res)
			}
			return err
		}, lockFile + " held " + journalLock, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := newHost(t)
			e := New(h, testOptions(h, &holds{}, nil, debian13))
			fw := command("example.port/allow", StageFirewall, RiskLockout, argv(allow2230), argv(delete2230))
			var took time.Duration
			var locks []string
			inner := fw.Apply
			fw.Apply = func(ctx context.Context, sys host.System, run Run) (Ran, error) {
				if tc.setup != nil {
					tc.setup(h, run.ID)
				}
				from, start := len(h.Calls()), time.Now()
				if err := tc.other(ctx, h, run.ID); err != nil {
					t.Errorf("the other command: %v", err)
				}
				took, locks = time.Since(start), locksSince(h, from)
				return inner(ctx, sys, run)
			}
			p, err := e.Plan(ctx, []Control{&stub{id: "example.port", title: "Port", changes: []Change{fw}}}, nil)
			if err != nil || p.Err() != nil {
				t.Fatal(err, p.Err())
			}
			_, err = e.Apply(ctx, p)
			if tc.failed != (err != nil) || tc.failed && !errors.Is(err, revert.ErrRollingBack) {
				t.Errorf("Apply = %v; want it failed %v, rolled back by the other", err, tc.failed)
			}
			if took > time.Second {
				t.Errorf("the other command took %s: it waited for a lock", took)
			}
			if got := strings.Join(locks, " "); got != tc.locks {
				t.Errorf("its locks %q, want %q", got, tc.locks)
			}
			assertReleased(t, h, p.ID)
		})
	}
}

// TestArmWaitsForJournalLock: an apply's Arm waits for the journal lock
// another process holds for a moment (a rollback's claim), then arms; it
// never holds the run lock's waiters, as nothing waits for the run lock.
func TestArmWaitsForJournalLock(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	h.AddDir(revert.DefaultDir, 0o700, 0, 0)
	unlock, err := h.System.Lock(ctx, journalLock, []byte("pid 7000, hardn rollback 20261019T110000Z-5e6f7a8b, since 2026-10-19T11:59:59Z\n"))
	if err != nil {
		t.Fatal(err)
	}
	e := New(h, testOptions(h, nil, nil, debian13))
	p, err := e.Plan(ctx, []Control{portControl()}, nil)
	if err != nil || p.Err() != nil {
		t.Fatal(err, p.Err())
	}
	const hold = 100 * time.Millisecond
	go func() {
		time.Sleep(hold)
		_ = unlock()
	}()
	start := time.Now()
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < hold {
		t.Errorf("Arm armed after %s, before the lock was released", took)
	}
	if st, _ := revert.ReadState(ctx, h, revert.Options{}); st.Pending == nil || st.Pending.ID != p.ID {
		t.Errorf("not armed: %+v", st)
	}
}

// TestBootUnitUnderRunLock: `hardn rollback --boot`, run by hand between
// the boot unit's install and Arm, finds nothing journalled, and keeps the
// unit: the apply holds the run lock (revert.RemoveBootUnit, revert.Boot);
// neither waits for it. The change is then armed with its boot unit.
func TestBootUnitUnderRunLock(t *testing.T) {
	ctx := context.Background()
	h := newBareHost(t)
	u, _ := revert.PlanBootUnit(debian13.Init, testHardn)
	ro := revert.Options{Hardn: testHardn, Now: func() time.Time { return testNow }}
	var removeErr error
	var boot revert.BootResult
	done := false
	h.onRun = func(argv []string) {
		if done || argv[0] != "systemctl" || !slices.Contains(argv, "enable") {
			return
		}
		done = true
		_, removeErr = revert.RemoveBootUnit(ctx, h.System, u, ro)
		boot, _ = revert.Boot(ctx, h.System, debian13.Init, ro)
	}
	e := New(h, testOptions(h, nil, nil, debian13))
	p, err := e.Plan(ctx, []Control{portControl()}, nil)
	if err != nil || p.Err() != nil {
		t.Fatal(err, p.Err())
	}
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	if !done || !errors.Is(removeErr, host.ErrLocked) {
		t.Errorf("RemoveBootUnit during the install: %v, want host.ErrLocked", removeErr)
	}
	if boot.Action != revert.BootNothing || boot.Removed || boot.RemoveErr != nil {
		t.Errorf("Boot during the install: %+v; want nothing done, the unit kept", boot)
	}
	if st, err := revert.ReadBootUnit(ctx, h, u); err != nil || !st.Installed() {
		t.Errorf("the boot unit after the apply: installed %v, %v", st.Installed(), err)
	}
	if st, _ := revert.ReadState(ctx, h, revert.Options{}); st.Pending == nil || st.Pending.ID != p.ID {
		t.Errorf("the change is not armed: %+v", st)
	}
}
