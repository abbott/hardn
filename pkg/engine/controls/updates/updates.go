// Package updates holds the Auto Updates control (refactor plan Phase 4,
// next/p4-check-updates): Figma's "Auto Updates" row. Check reads whether
// the host upgrades its packages by itself; Plan turns that on or off
// (slice 7).
//
// On Debian, Ubuntu and Proxmox VE that is apt's daily job running
// unattended-upgrade, read as apt.systemd.daily decides it: the effective
// APT::Periodic values and unattended-upgrades' origins (apt-config
// dump, never the files alone), the unattended-upgrades package, and the
// two systemd timers that run the job (apt-daily-upgrade.timer upgrades,
// apt-daily.timer refreshes the package lists). On Alpine it is
// apk-cron's daily script run by crond, read by alpine.ReadUpdates. On
// Rocky Linux and AlmaLinux it is dnf-automatic's timer that installs
// updates, read by dnfauto.Read (docs/engine/el-updates.md). Its
// Plan turns updates on or off, as Action asks (control.go; slice 7, unit
// (b), docs/engine/control-updates.md). The line describes the host now: NotConfigured when no upgrade runs,
// Partial when upgrades run but the lists are not refreshed or the job
// stops at the next restart, Unknown, "can't read", when what decides is
// not established, never a pass.
// docs/engine/check-updates.md has every line and its sources.
package updates

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// ID is the control's ID, stable once released.
const ID = "updates.auto"

// The row's details (proposed wording, check-updates.md §5). Exported so
// that the risk level (next/p4-risk) and the gallery tell apart two lines
// of one state without a copy of the wording.
const (
	DetailUnattended      = "unattended-upgrades"    // Configured: apt's daily job runs unattended-upgrade
	DetailApkCron         = "apk-cron"               // Configured: crond runs apk-cron's daily script
	DetailListsStale      = "lists not refreshed"    // Partial: upgrades run, apt's lists are not refreshed
	DetailOffAfterRestart = "off after restart"      // Partial: runs now, not after the next boot
	DetailNoUnattended    = "no unattended-upgrades" // NotConfigured: the package not installed
	DetailUpgradesOff     = "upgrades off"           // NotConfigured: APT::Periodic turns them off
	DetailNoOrigins       = "no origins allowed"     // NotConfigured: unattended-upgrades takes no origin's packages
	DetailTimerOff        = "upgrade timer off"      // NotConfigured: apt-daily-upgrade.timer not running
	DetailJobMasked       = "upgrade job masked"     // NotConfigured: apt-daily-upgrade.service masked
	DetailNoApkCron       = "no apk-cron"            // NotConfigured: the package not installed
	DetailScriptNotRun    = "daily script not run"   // NotConfigured: apk-cron's script missing or not executable
	DetailCrondOff        = "crond not running"      // NotConfigured: no crond running
	DetailCantRead        = "can't read"             // Unknown
	DetailByHand          = "updated by hand"        // NotApplicable: Proxmox VE (decision 7)
	DetailChangedSince    = "changed since hardn"    // CodeChangedSince's detail (decision 9)
)

// The finding codes of this package (pkg/output's registry holds them).
const (
	// CodeAptValueUnknown: an APT::Periodic value in a form
	// apt.systemd.daily's reading is not ranked for ("1w", "yes", ""), so
	// whether its job runs is not established.
	CodeAptValueUnknown = "UPDATES_APT_VALUE_UNKNOWN"
	// CodeScheduleUnknown: whether anything starts the daily job is not
	// established: an apt timer in a state that does not say or that
	// could not be read, a host systemd did not boot (cron's apt-compat
	// is not read), or an Alpine OpenRC did not boot (no crond).
	CodeScheduleUnknown = "UPDATES_SCHEDULE_UNKNOWN"
	// CodeProxmoxNotUpgraded: on Proxmox VE, no unattended-upgrades origin
	// matches Proxmox VE's repository, so Proxmox VE's own packages are
	// not upgraded (Debian's default origins never match them; P-43 (6)
	// decision 5).
	CodeProxmoxNotUpgraded = "UPDATES_PROXMOX_NOT_UPGRADED"
	// CodeOriginsUnknown: a distribution hardn does not list, built from
	// Debian or Ubuntu, whose apt job runs: whether its origins match the
	// distribution's own archives is not established.
	CodeOriginsUnknown = "UPDATES_ORIGINS_UNKNOWN"
	// CodeChangedSince: hardn's record of turning updates on names a file
	// the host no longer holds as recorded (hardn's apt file, apk-cron's
	// script), or a package it installed that is not installed now
	// (decision 9 (a)).
	CodeChangedSince = "UPDATES_CHANGED_SINCE_HARDN"
)

// Controls is the Auto Updates row, the only one of its area.
func Controls() []engine.Control { return []engine.Control{Auto{}} }

// Auto is Auto Updates: whether the host upgrades its packages by itself.
// Configured is the upgrade job running now and after the next boot, with
// apt's package lists refreshed (on Alpine, apk -U refreshes its own).
// Action is what Plan does; its zero value plans nothing
// (engine.ErrNotMigrated), so the status list's Auto{} is check-only.
// Records are the record's directory and clock (revert.Options' Dir and
// Now; the zero value is /var/lib/hardn and time.Now), and Version is
// hardn's version, written in the record.
type Auto struct {
	Action  AutoAction
	Records revert.Options
	Version string
}

// ID returns ID.
func (Auto) ID() string { return ID }

// Title is Figma's row label.
func (Auto) Title() string { return "Auto Updates" }

// Applies is true on every host but Proxmox VE (slice 7 decision 7 (a)):
// Proxmox VE is updated by hand with apt full-upgrade, so its row is Not
// Applicable, "updated by hand", and leaves the risk level. A container
// upgrades its own packages too, and a host whose update tool hardn does
// not read is Unknown, not left out of the risk level.
func (Auto) Applies(f detect.Facts) (bool, string) {
	if f.Platform.Distro == detect.DistroProxmox {
		return false, DetailByHand
	}
	return true, ""
}

// Check reads apt's daily job on a host apt manages, apk-cron on Alpine,
// and dnf-automatic on Rocky Linux and AlmaLinux (check-updates.md §2). A read that fails is Unknown with the
// error; a documented value it does not rank is Unknown with a finding
// and no error; a state the facts read establish is reported whatever
// else could not be read. Where hardn's record names a file the host no
// longer holds as recorded, or a package it installed that is gone, the
// line carries CodeChangedSince, "changed since hardn" (decision 9 (a)).
func (c Auto) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	st, err := check(ctx, sys, f)
	if why, ok := c.stale(ctx, sys, f); ok {
		st.Findings = append(st.Findings, why)
	}
	return st, err
}

// check is the line, without the record's finding.
func check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	switch d := f.Platform.Distro; {
	case d == detect.DistroAlpine:
		return checkApk(ctx, sys, f)
	case d == detect.DistroDebian, d == detect.DistroUbuntu, d == detect.DistroProxmox,
		d == detect.DistroOther && (f.Platform.Like == detect.DistroDebian || f.Platform.Like == detect.DistroUbuntu),
		d == detect.DistroUnknown && f.OS.Known() && f.OS.ID == string(detect.DistroDebian): // Debian or Proxmox VE: apt either way
		return checkApt(ctx, sys, f)
	case d.EL():
		return checkDnf(ctx, sys)
	case d == detect.DistroOther:
		return unread(nil, fmt.Errorf("updates: %q, like %q: no update tool hardn reads: %w", f.OS.ID, f.Platform.Like, detect.ErrUnrecognised))
	}
	return unread(nil, fmt.Errorf("updates: distribution: %w", errOr(f.Platform.Err, detect.ErrNoSignal)))
}

// fact is one thing a line depends on: Yes, No, or Unknown with why: a
// read that failed (err), or a documented value not ranked (why). A read
// that failed may carry why too: what a line decided without it says of
// it.
type fact struct {
	t   detect.Tri
	err error
	why engine.Finding
}

func known(yes bool) fact {
	if yes {
		return fact{t: detect.Yes}
	}
	return fact{t: detect.No}
}

// cond is a fact with the detail it gives the line when it is No.
type cond struct {
	f      fact
	detail string
}

// decide is the line of conditions in two tiers, each in precedence
// order: the first No of off is NotConfigured; with none, a fact of off
// not established is Unknown; else the first No of partial is Partial,
// one not established is Unknown, and with all Yes the line is
// Configured with ok. A Partial line carries the findings of the facts
// of partial left open: its two details mean different things to the
// risk level (check-updates.md §6), and a fact left open may be the
// other one.
func decide(ev []string, off, partial []cond, ok string) (engine.Status, error) {
	for _, tier := range []struct {
		cs    []cond
		state engine.State
	}{{off, engine.NotConfigured}, {partial, engine.Partial}} {
		if i := slices.IndexFunc(tier.cs, func(c cond) bool { return c.f.t == detect.No }); i >= 0 {
			st := status(tier.state, tier.cs[i].detail, ev)
			if tier.state == engine.Partial {
				st.Findings = open(tier.cs)
			}
			return st, nil
		}
		if st, undecided, err := unknowns(ev, tier.cs); undecided {
			return st, err
		}
	}
	return status(engine.Configured, ok, ev), nil
}

// unknowns is Unknown, "can't read", with the errors and findings of the
// conditions not established, each once; undecided is false when every
// condition is.
func unknowns(ev []string, cs []cond) (st engine.Status, undecided bool, err error) {
	var errs []error
	var why []engine.Finding
	for _, c := range cs {
		switch {
		case c.f.t != detect.Unknown:
			continue
		case c.f.err == nil && c.f.why.Code != "":
			if !slices.Contains(why, c.f.why) {
				why = append(why, c.f.why)
			}
		default: // the same error, read once for several facts, said once
			e := errOr(c.f.err, detect.ErrNoSignal)
			if !slices.ContainsFunc(errs, func(o error) bool { return o.Error() == e.Error() }) {
				errs = append(errs, e)
			}
		}
		undecided = true
	}
	if !undecided {
		return engine.Status{}, false, nil
	}
	st = engine.Status{State: engine.Unknown, Detail: DetailCantRead, Evidence: ev, Findings: why}
	switch len(errs) {
	case 0:
	case 1:
		err = errs[0]
	default: // each is the package's already (wrap): joined, not wrapped again
		err = errors.Join(errs...)
	}
	return st, true, err
}

// open is the findings of the conditions not established, each once.
func open(cs []cond) []engine.Finding {
	var why []engine.Finding
	for _, c := range cs {
		if c.f.t == detect.Unknown && c.f.why.Code != "" && !slices.Contains(why, c.f.why) {
			why = append(why, c.f.why)
		}
	}
	return why
}

// status is a status with no error and no findings.
func status(s engine.State, detail string, ev []string) engine.Status {
	return engine.Status{State: s, Detail: detail, Evidence: ev}
}

// unread is the status of a check that could not establish the line:
// Unknown, "can't read", with what was read and the error, which
// Engine.Check reports (its code is pkg/output's).
func unread(ev []string, err error) (engine.Status, error) {
	return engine.Status{State: engine.Unknown, Detail: DetailCantRead, Evidence: ev}, err
}

func errOr(err, def error) error {
	if err != nil {
		return err
	}
	return def
}
