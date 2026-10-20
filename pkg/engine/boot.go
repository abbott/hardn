package engine

// The boot unit before the first Arm (refactor plan P3, next/p3-engine-boot;
// P-34 (c), decided by the maintainer 2026-10-19). A reboot inside the
// confirm window loses the revert timer; the boot unit runs `hardn rollback
// --boot` at the next boot, which re-arms it or rolls the change back
// (docs/safety/revert-boot.md). The engine plans it as a change of its own,
// shown in the plan, in StageBoot, and installs it under the run lock
// before revert.Arm. docs/engine/engine-boot.md has the guarantees.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/revert"
)

// BootChangeID is the ID of the boot unit's change in a plan.
const BootChangeID = "hardn.boot-unit"

// bootChange plans the boot unit for lockout-risk plan p: revert.PlanBootUnit
// over detect's init and the hardn binary the revert timer runs, and
// revert.ReadBootUnit. It plans nothing when the unit is installed and
// enabled as planned (idempotent), and refuses what cannot be planned or
// read (an init system with no timer is the revert timer's refusal
// already, refuseApply). The caller has read the journal, and plans this
// only when nothing is journalled: the unit is a journalled change's
// boot-time recovery, which a second change must not rewrite.
//
// The change is RiskPlain, in StageBoot, with no Backups and an empty
// Rollback: it is made before revert.Arm, so there is no journal to hold
// it, and it is not undone with the change it came with. The boot unit
// removes itself at the first boot that finds nothing journalled
// (revert.RemoveBootUnit), and a stale or foreign file it replaces is
// backed up, unjournalled, into the change's backup run (backup.Optional,
// through Run.Store), as revert.EnsureBootUnit does
// (docs/engine/engine-boot.md §2.1).
func (e *Engine) bootChange(ctx context.Context, p *Plan, refuse func(error)) (Change, bool) {
	u, err := revert.PlanBootUnit(p.Facts.Init, e.opts.Revert.Hardn)
	switch {
	case errors.Is(err, revert.ErrNoTimer):
		return Change{}, false
	case err != nil:
		refuse(fmt.Errorf("engine: the boot unit: %w", err))
		return Change{}, false
	}
	st, err := revert.ReadBootUnit(ctx, e.sys, u)
	if err != nil {
		refuse(fmt.Errorf("engine: the boot unit: %w", err))
		return Change{}, false
	}
	if st.Installed() {
		return Change{}, false
	}
	c := Change{ID: BootChangeID, Target: u.Path, Risk: RiskPlain, Stage: StageBoot,
		StateBefore: bootState(u, st, e.opts.Backups.Enabled), StateAfter: "installed and enabled"}
	switch {
	case !st.File:
		c.Summary = fmt.Sprintf("Install hardn's boot unit, %s: after a reboot inside the confirm window it re-arms the revert timer or rolls the change back", u.Path)
	case !st.Current:
		c.Summary = fmt.Sprintf("Replace %s with hardn's boot unit: after a reboot inside the confirm window it re-arms the revert timer or rolls the change back", u.Path)
	default:
		c.Summary = fmt.Sprintf("Enable hardn's boot unit, %s: after a reboot inside the confirm window it re-arms the revert timer or rolls the change back", u.Path)
	}
	if !st.Current {
		after := string(u.Data)
		var before *string
		if st.File {
			b := string(st.Data)
			before = &b
		}
		c.Diff = FileDiff(u.Path, before, &after)
		copyName := filepath.Join(e.runRoot(), p.ID, filepath.Base(u.Path))
		c.Commands = append(c.Commands, host.FormatArgv(append(slices.Clone(u.Check), copyName)))
	}
	if !st.Linked {
		c.Commands = append(c.Commands, host.FormatArgv(u.Enable))
	}
	ro := e.opts.Revert
	c.Apply = func(ctx context.Context, sys host.System, run Run) (Ran, error) {
		r, err := revert.EnsureBootUnit(ctx, sys, u, run.Dir, run.Store, ro)
		return Ran{Detail: bootDetail(r)}, err
	}
	return c, true
}

// bootState is what the plan shows the host has of boot unit u. A file
// that is replaced is backed up first when backups are enabled
// (EnsureBootUnit's backup is backup.Optional: nothing restores it).
func bootState(u revert.BootUnit, st revert.BootUnitState, backups bool) string {
	kept := "backed up, then replaced"
	if !backups {
		kept = "replaced, not backed up: enableBackups is false"
	}
	switch {
	case !st.File:
		return "not installed"
	case !bytes.Equal(st.Data, u.Data):
		return "a file that is not the unit this hardn installs (" + kept + ")"
	case !st.Current:
		return fmt.Sprintf("mode %04o, owner uid %d, where hardn installs it %04o, root's (%s)", uint32(st.Mode.Perm()), st.UID, uint32(u.Mode.Perm()), kept)
	}
	return "installed, not enabled"
}

// bootDetail is the result's line for what EnsureBootUnit did.
func bootDetail(r revert.BootUnitResult) string {
	if r.Already {
		return "installed and enabled already"
	}
	var did []string
	switch {
	case r.Backup == nil:
	case r.Backup.Outcome == backup.Skipped:
		did = append(did, "the file it replaced not backed up (enableBackups is false)")
	default:
		did = append(did, fmt.Sprintf("the file it replaced backed up (entry %d)", r.Backup.Entry.Seq))
	}
	if r.Wrote {
		did = append(did, "written")
	}
	if r.Enabled {
		did = append(did, "enabled")
	}
	return strings.Join(did, ", ")
}
