package updates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dnfauto"
	"github.com/abbott/hardn/pkg/host/packages"
	"github.com/abbott/hardn/pkg/host/revert"
)

// Rocky Linux and AlmaLinux: Auto Updates through dnf-automatic, security
// updates only (the maintainer, 2026-11-10: "Security only";
// docs/engine/el-updates.md).

// The row's details there (proposed wording).
const (
	DetailNoDnfAuto          = "no dnf-automatic"           // NotConfigured: the package not installed
	DetailInstallTimerOff    = "install timer off"          // NotConfigured: no timer installs updates
	DetailDnfSecurity        = "dnf-automatic, security"    // Configured: upgrade_type security
	DetailDnfAll             = "dnf-automatic, all updates" // Configured: upgrade_type default
	DetailStartsAfterRestart = "starts after restart"       // Partial: enabled, not active
)

// The record's step words there (stepInstall is apt's word too).
const (
	stepConf   = "conf"   // automatic.conf's upgrade_type set to security
	stepEnable = "enable" // the install timer enabled
	stepStart  = "start"  // the install timer started
)

// The changes' ids there.
const (
	ChangeConf  = ID + "/conf"
	ChangeTimer = ID + "/timer"
)

// The codes of the dnf refusals packages names (pkg/output registers them
// for packages' sentinels).
const (
	codeKernel = "PACKAGE_KERNEL"
	codeLocked = "PACKAGE_LOCKED"
)

// codeDnfConf is dnfauto.ErrConf's: automatic.conf not in a form hardn
// edits, missing, or there before dnf-automatic.
const codeDnfConf = "UPDATES_DNF_CONF"

// The review's and the screen's words for automatic.conf's reboot where it
// restarts the host (proposed, el-updates.md §1.2).
const (
	KeyRestarts = "Restarts"
	NoteByConf  = "by automatic.conf"
)

// RebootValue is reboot's value in the review's and the screen's words:
// "" for never, which they don't draw.
func RebootValue(reboot string) string {
	switch reboot {
	case dnfauto.RebootWhenNeeded:
		return "When needed"
	case dnfauto.RebootWhenChanged:
		return "When changed"
	}
	return ""
}

// checkDnf is the row there (el-updates.md §1.1): dnf-automatic, the
// timer that installs updates (the install timer, or the plain one with
// apply_updates true), and upgrade_type. download and notifyonly install
// nothing, and don't count.
func checkDnf(ctx context.Context, sys host.System) (engine.Status, error) {
	u := dnfauto.Read(ctx, sys)
	ev := dnfEvidence(u)
	if err := u.Err(); err != nil {
		return unread(ev, err)
	}
	if u.Package == detect.No {
		return status(engine.NotConfigured, DetailNoDnfAuto, ev), nil
	}
	t, ok := u.Applies()
	switch {
	case !ok:
		return status(engine.NotConfigured, DetailInstallTimerOff, ev), nil
	case t.On() && u.UpgradeType == dnfauto.Security:
		return status(engine.Configured, DetailDnfSecurity, ev), nil
	case t.On():
		return status(engine.Configured, DetailDnfAll, ev), nil
	case t.Active == "active":
		return status(engine.Partial, DetailOffAfterRestart, ev), nil
	}
	return status(engine.Partial, DetailStartsAfterRestart, ev), nil
}

// dnfEvidence is what checkDnf read.
func dnfEvidence(u dnfauto.Updates) []string {
	ev := []string{"rpm -q " + dnfauto.Package + ": " + map[detect.Tri]string{detect.Yes: "installed", detect.No: "not installed"}[u.Package]}
	if u.Package != detect.Yes {
		return ev
	}
	for _, t := range []dnfauto.Timer{u.Install, u.Plain} {
		ev = append(ev, t.Name+": "+t.Active+", "+t.Enabled)
	}
	return append(ev, fmt.Sprintf("%s: upgrade_type %s, apply_updates %v", dnfauto.Conf, u.UpgradeType, u.Apply))
}

// dnfBlocked is what is in the way of on there, in the item's words.
func dnfBlocked(r Reading) string {
	switch r.DnfUpdates.Install.Enabled {
	case "masked", "masked-runtime":
		return "Install timer is masked"
	}
	return ""
}

// confEdit is what the conf change did, for the record written after it
// in the same plan: the file's hash before, the line it replaced ("" for
// one added).
type confEdit struct {
	done             bool
	before, replaced string
}

// fill adds the edit to the record's entry: the hash before (by the
// package), and Lines, the line hardn wrote then the one it replaced
// (none, for one added), which off puts back.
func (e *confEdit) fill(entry *revert.RecordEntry) {
	if !e.done {
		return
	}
	entry.Before = append(entry.Before, revert.FileHash{Path: dnfauto.Conf, SHA256: e.before, By: dnfauto.Package})
	entry.Lines = []string{dnfauto.Line}
	if e.replaced != "" {
		entry.Lines = append(entry.Lines, e.replaced)
	}
}

// planDnfOn is on there (el-updates.md §1.2): refused where a fact is
// unread or the install timer is masked; nothing where a timer installs
// updates already (on outside hardn, or by it); else dnf-automatic
// installed where missing, upgrade_type set to security, the install
// timer enabled and started, each where it isn't, then the record.
func (c Auto) planDnfOn(ctx context.Context, sys host.System, r Reading) ([]engine.Change, error) {
	if err := unreadRefusal(ctx, sys, r); err != nil {
		return nil, err
	}
	u := r.DnfUpdates
	if r.Status.State == engine.Configured {
		return nil, nil
	}
	if b := dnfBlocked(r); b != "" {
		return nil, refuse(codeJobMasked, ErrJobMasked, nil, dnfauto.InstallTimer+" is masked, and hardn leaves a mask it didn't make",
			"run systemctl unmask "+dnfauto.InstallTimer+", then try again")
	}
	var cs []engine.Change
	var entry revert.RecordEntry
	installing := u.Package == detect.No
	if installing {
		switch _, err := sys.Lstat(ctx, dnfauto.Conf); {
		case err == nil:
			return nil, refuse(codeDnfConf, dnfauto.ErrConf, nil, "automatic.conf is there before dnf-automatic: rpm would keep it, and Off's remove would move it aside",
				"install dnf-automatic yourself, then turn updates on here")
		case !errors.Is(err, fs.ErrNotExist):
			return nil, refuse(codeNotRead, ErrNotRead, err, "hardn can't read "+dnfauto.Conf+": "+firstLine(err.Error()), "fix that, then try again")
		}
		p, err := packages.PlanInstall(ctx, sys, packages.Dnf, dnfauto.Package)
		if err != nil {
			return nil, dnfInstallWords(err)
		}
		entry.Packages = p.Adds
		cs = append(cs, dnfInstallChange(p))
		entry.Steps = append(entry.Steps, stepInstall)
	}
	edit := &confEdit{}
	if installing || u.UpgradeType != dnfauto.Security {
		if !installing {
			if err := confSettable(ctx, sys); err != nil {
				return nil, err
			}
		}
		cs = append(cs, confChange(edit, changeIDs(cs)))
		entry.Steps = append(entry.Steps, stepConf)
	}
	enable, start := installing || u.Install.Enabled != "enabled", installing || u.Install.Active != "active"
	if enable || start {
		cs = append(cs, timerChange(enable, start, changeIDs(cs)))
		if enable {
			entry.Steps = append(entry.Steps, stepEnable)
		}
		if start {
			entry.Steps = append(entry.Steps, stepStart)
		}
	}
	if len(cs) == 0 {
		return nil, nil
	}
	rc, err := c.recordOn(entry, cs, edit.fill)
	if err != nil {
		return nil, err
	}
	return append(cs, rc), nil
}

// confSettable refuses, at plan, an automatic.conf hardn can't set
// upgrade_type in: missing, or SetSecurity's refusal (no [commands], a key
// twice). The conf change reads it again at apply.
func confSettable(ctx context.Context, sys host.System) error {
	data, err := sys.ReadFile(ctx, dnfauto.Conf)
	if err == nil {
		_, _, err = dnfauto.SetSecurity(data)
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return refuse(codeDnfConf, dnfauto.ErrConf, nil, dnfauto.Conf+" is missing, and hardn sets upgrade_type there", "run dnf reinstall dnf-automatic, then try again")
	case errors.Is(err, dnfauto.ErrConf):
		why := strings.TrimPrefix(strings.TrimSuffix(err.Error(), ": "+dnfauto.ErrConf.Error()), "dnfauto: ")
		return refuse(codeDnfConf, dnfauto.ErrConf, err, "hardn can't set upgrade_type in "+dnfauto.Conf+": "+why, "fix that, then try again")
	case err != nil:
		return refuse(codeNotRead, ErrNotRead, err, "hardn can't read "+dnfauto.Conf+": "+firstLine(err.Error()), "fix that, then try again")
	}
	return nil
}

// dnfInstallChange is dnf-automatic's install: a package of its own, no
// dependency on rocky10-test-110 (probe 5), its timers left disabled by
// the preset.
func dnfInstallChange(p packages.Plan) engine.Change {
	return engine.Change{ID: ChangeInstall, Summary: "Install dnf-automatic", Title: "Install dnf-automatic", Target: dnfauto.Package,
		StateBefore: "not installed", StateAfter: "installed", Commands: []string{host.FormatArgv(p.Argv)},
		Risk: engine.RiskPackageInstall, Stage: engine.StagePackages, Resources: []string{ID},
		Rollback: engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: slices.Clone(p.Undo), Why: ID}}},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			rep, err := packages.Execute(ctx, sys, p)
			u := engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: slices.Clone(rep.Undo), Why: ID}}}
			if len(rep.Undo) == 0 {
				u = engine.Undo{}
			}
			return engine.Ran{Rollback: &u, Detail: "dnf-automatic: installed"}, err
		}}
}

// confChange sets upgrade_type to security in automatic.conf, read at
// apply (the install before it may bring the file), backed up first,
// written keeping its mode and owner, then read back; edit keeps what it
// did for the record.
func confChange(edit *confEdit, after []string) engine.Change {
	return engine.Change{ID: ChangeConf, Summary: "Install security updates alone: " + dnfauto.Line + " in " + dnfauto.Conf, Title: "Security updates only",
		Target: dnfauto.Conf, Commands: []string{"set [commands] " + dnfauto.Line + " in " + dnfauto.Conf},
		Stage: engine.StageFiles, Resources: []string{ID}, Backups: []string{dnfauto.Conf}, After: after,
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			data, err := sys.ReadFile(ctx, dnfauto.Conf)
			if err != nil {
				return engine.Ran{}, err
			}
			out, replaced, err := dnfauto.SetSecurity(data)
			if err != nil {
				return engine.Ran{}, refuse(codeNotRead, ErrNotRead, err, "hardn can't set upgrade_type in "+dnfauto.Conf+": "+firstLine(err.Error()), "fix that, then try again")
			}
			if err := host.WriteFileAtomicKeep(ctx, sys, dnfauto.Conf, out, 0o644, 0, 0); err != nil {
				return engine.Ran{}, err
			}
			edit.done, edit.before, edit.replaced = true, sum(data), replaced
			back, err := sys.ReadFile(ctx, dnfauto.Conf)
			if err != nil {
				return engine.Ran{}, err
			}
			if s, err := dnfauto.Parse(back); err != nil || s.UpgradeType != dnfauto.Security {
				return engine.Ran{}, refuse(codeNotInEffect, ErrNotInEffect, err, fmt.Sprintf("dnf-automatic reads upgrade_type %q after the change", s.UpgradeType), "find what sets it, then try again")
			}
			return engine.Ran{Detail: "dnf-automatic: security updates only"}, nil
		}}
}

// timerChange enables and starts the install timer, each where asked,
// then reads it back on; undone by stop and disable, each where it ran.
func timerChange(enable, start bool, after []string) engine.Change {
	var argvs [][]string
	var undo []revert.Step
	if enable {
		argvs = append(argvs, []string{"systemctl", "enable", dnfauto.InstallTimer})
		undo = append(undo, revert.Step{Op: revert.OpRun, Argv: []string{"systemctl", "disable", dnfauto.InstallTimer}, Why: ID})
	}
	if start {
		argvs = append(argvs, []string{"systemctl", "start", dnfauto.InstallTimer})
		undo = append([]revert.Step{{Op: revert.OpRun, Argv: []string{"systemctl", "stop", dnfauto.InstallTimer}, Why: ID}}, undo...)
	}
	var cmds []string
	for _, a := range argvs {
		cmds = append(cmds, host.FormatArgv(a))
	}
	return engine.Change{ID: ChangeTimer, Summary: "Turn on dnf-automatic's daily install", Title: "Turn on daily security updates", Target: dnfauto.InstallTimer,
		StateBefore: "off", StateAfter: "on, daily", Commands: cmds, Stage: engine.StageReload, Resources: []string{ID}, After: after,
		Rollback: engine.Undo{Steps: undo},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			for _, a := range argvs {
				if _, err := sys.Run(ctx, a[0], a[1:]...); err != nil {
					return engine.Ran{}, err
				}
			}
			active, e1 := detect.SystemctlIsActive(ctx, sys, dnfauto.InstallTimer)
			enabled, e2 := detect.SystemctlIsEnabled(ctx, sys, dnfauto.InstallTimer)
			if err := errors.Join(e1, e2); err != nil || active != "active" || enabled != "enabled" {
				return engine.Ran{}, refuse(codeNotInEffect, ErrNotInEffect, err, fmt.Sprintf("%s is %s and %s after the change", dnfauto.InstallTimer, active, enabled), "find what stops it, then try again")
			}
			return engine.Ran{Detail: dnfauto.InstallTimer + ": on"}, nil
		}}
}

// dnfInstallWords are packages.PlanInstall's refusals there.
func dnfInstallWords(err error) error {
	var other *packages.OtherPackagesError
	var kernel *packages.KernelError
	switch {
	case errors.Is(err, packages.ErrNotInRepos):
		return refuse(codeNotInRepos, ErrNotInRepos, err, "hardn can't find dnf-automatic in dnf's repositories", "check the network and the repositories (dnf repolist), then try again")
	case errors.As(err, &other):
		return refuse(codeOtherPackages, ErrOtherPackages, err, "Installing dnf-automatic would also change other packages: "+strings.Join(other.Changes, ", "), "install it by hand, then try again")
	case errors.As(err, &kernel):
		return refuse(codeKernel, packages.ErrKernel, err, "Installing dnf-automatic would also install a new kernel ("+strings.Join(kernel.Packages, ", ")+")", "run dnf upgrade and restart, then try again")
	case errors.Is(err, packages.ErrLocked):
		return refuse(codeLocked, packages.ErrLocked, err, "another dnf is running", "wait for it to finish, then try again")
	}
	return err
}

// sum is data's SHA-256.
func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// confEntry is the record as its latest entry that edited automatic.conf
// (stepConf) alone: the hash before, the lines and the hash after are that
// one edit's.
func confEntry(rec *revert.ControlRecord) *revert.ControlRecord {
	for i := len(rec.Entries) - 1; i >= 0; i-- {
		if slices.Contains(rec.Entries[i].Steps, stepConf) {
			return &revert.ControlRecord{Entries: rec.Entries[i : i+1]}
		}
	}
	return &revert.ControlRecord{}
}

// planDnfOff is off there (el-updates.md §1.3): what the record says hardn
// did, last first, each where the host is still as recorded: the install
// timer stopped and disabled where hardn started and enabled it; the line
// put back where the file holds the bytes hardn left, the bytes written
// back hashing to the bytes before; dnf-automatic removed where hardn
// installed it, alone, after the file is the package's own again. The
// last step reads the row back; where a timer that was on before hardn
// still installs updates after it (stillOn), it reads back hardn's own
// steps undone instead, and the review says updates stay on.
func planDnfOff(ctx context.Context, sys host.System, r Reading, done []string, add func(engine.Change), notes *[]Note) error {
	var mine []engine.Change
	u := r.DnfUpdates
	var argvs, undos [][]string
	if slices.Contains(done, stepStart) && u.Install.Active == "active" {
		argvs, undos = append(argvs, []string{"systemctl", "stop", dnfauto.InstallTimer}), append(undos, []string{"systemctl", "start", dnfauto.InstallTimer})
	}
	if slices.Contains(done, stepEnable) && u.Install.Enabled == "enabled" {
		argvs, undos = append(argvs, []string{"systemctl", "disable", dnfauto.InstallTimer}), append(undos, []string{"systemctl", "enable", dnfauto.InstallTimer})
	}
	if len(argvs) > 0 {
		mine = append(mine, timerOffChange(argvs, undos))
	}
	before := ""
	if slices.Contains(done, stepConf) && u.Package == detect.Yes {
		ch, b, err := confRevertChange(ctx, sys, r.Record)
		if err != nil {
			return err
		}
		mine, before = append(mine, ch), b
	}
	if installedBy(r.Record) && u.Package == detect.Yes {
		rm, err := packages.PlanRemove(ctx, sys, packages.Dnf, dnfauto.Package)
		var other *packages.OtherPackagesError
		switch {
		case errors.As(err, &other):
			return refuse(codeOtherPackages, ErrOtherPackages, err, "Removing dnf-automatic would also remove other packages: "+strings.Join(other.Changes, ", "), "remove it by hand")
		case errors.Is(err, packages.ErrLocked):
			return refuse(codeLocked, packages.ErrLocked, err, "another dnf is running", "wait for it to finish, then try again")
		case err != nil:
			return err
		}
		if !rm.Empty() {
			mine = append(mine, removeChange(rm))
			*notes = append(*notes, rollbackNotes(false)...)
		}
	}
	_, on := stillOn(u, done)
	if !on {
		withReadBack(mine, add)
		return nil
	}
	*notes = append(*notes, Note{Key: "Updates", Value: "Stay on", Note: "timer on before hardn", Warn: true})
	withCheck(mine, add, func(ctx context.Context, sys host.System, _ engine.Run) error {
		return ownUndone(ctx, sys, done, before)
	})
	return nil
}

// stillOn is the timer that installs updates after off, and whether one
// does: the install timer as read, inactive where hardn started it and
// disabled where hardn enabled it; the plain timer and apply_updates as
// read, which hardn never changes.
func stillOn(u dnfauto.Updates, done []string) (dnfauto.Timer, bool) {
	if slices.Contains(done, stepStart) && u.Install.Active == "active" {
		u.Install.Active = "inactive"
	}
	if slices.Contains(done, stepEnable) && u.Install.Enabled == "enabled" {
		u.Install.Enabled = "disabled"
	}
	return u.Applies()
}

// ownUndone is off's read-back where updates stay on: the install timer
// not active where hardn started it, not enabled where hardn enabled it,
// and automatic.conf the bytes before hardn's line (before: its hash,
// "" where off doesn't put it back) or gone with the package.
func ownUndone(ctx context.Context, sys host.System, done []string, before string) error {
	var still []string
	if slices.Contains(done, stepStart) {
		active, err := detect.SystemctlIsActive(ctx, sys, dnfauto.InstallTimer)
		if err != nil {
			return err
		}
		if active == "active" {
			still = append(still, dnfauto.InstallTimer+" active")
		}
	}
	if slices.Contains(done, stepEnable) {
		enabled, err := detect.SystemctlIsEnabled(ctx, sys, dnfauto.InstallTimer)
		if err != nil {
			return err
		}
		if enabled == "enabled" {
			still = append(still, dnfauto.InstallTimer+" enabled")
		}
	}
	if before != "" {
		got, err := fileHash(ctx, sys, dnfauto.Conf)
		if err != nil {
			return err
		}
		if got != "" && got != before {
			still = append(still, "hardn's line in "+dnfauto.Conf)
		}
	}
	if len(still) > 0 {
		return refuse(codeNotInEffect, ErrNotInEffect, nil, "hardn's changes still read back after the change: "+strings.Join(still, ", "), "find what turns them on, then try again")
	}
	return nil
}

// timerOffChange runs argvs, undone by undos, last first.
func timerOffChange(argvs, undos [][]string) engine.Change {
	var cmds []string
	var undo []revert.Step
	for i, a := range argvs {
		cmds = append(cmds, host.FormatArgv(a))
		undo = append([]revert.Step{{Op: revert.OpRun, Argv: undos[i], Why: ID}}, undo...)
	}
	return engine.Change{ID: ChangeTimer, Summary: "Turn off dnf-automatic's daily install", Title: "Turn off daily updates", Target: dnfauto.InstallTimer,
		StateBefore: "on", StateAfter: "off", Commands: cmds, Stage: engine.StageFiles, Resources: []string{ID},
		Rollback: engine.Undo{Steps: undo},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			for _, a := range argvs {
				if _, err := sys.Run(ctx, a[0], a[1:]...); err != nil {
					return engine.Ran{}, err
				}
			}
			return engine.Ran{Detail: dnfauto.InstallTimer + ": off"}, nil
		}}
}

// confRevertChange puts automatic.conf back as hardn found it, by the
// latest entry that edited it (confEntry): refused where the file isn't
// the bytes hardn left (its recorded hash), or where the bytes put back
// wouldn't be the ones recorded before; checked again at apply. before is
// that hash.
func confRevertChange(ctx context.Context, sys host.System, rec *revert.ControlRecord) (_ engine.Change, before string, _ error) {
	e := confEntry(rec)
	after, okA := recordedHash(e, dnfauto.Conf)
	before, okB := recordedBefore(e, dnfauto.Conf)
	var lines []string
	if len(e.Entries) > 0 {
		lines = e.Entries[0].Lines
	}
	data, _, err := sys.ReadFileNoFollow(ctx, dnfauto.Conf)
	if errors.Is(err, fs.ErrNotExist) || err == nil && (!okA || sum(data) != after) {
		return engine.Change{}, "", changedSince(dnfauto.Conf, "edited")
	}
	if err != nil {
		return engine.Change{}, "", err
	}
	replaced := ""
	if len(lines) > 1 {
		replaced = lines[1]
	}
	out, err := dnfauto.Revert(data, replaced)
	if err != nil || !okB || sum(out) != before {
		return engine.Change{}, "", changedSince(dnfauto.Conf, "edited")
	}
	b, a := string(data), string(out)
	return engine.Change{ID: ChangeConf, Summary: "Put back upgrade_type in " + dnfauto.Conf, Title: "Put back the update type", Target: dnfauto.Conf,
		Diff: engine.FileDiff(dnfauto.Conf, &b, &a), Stage: engine.StageFiles, Resources: []string{ID}, Backups: []string{dnfauto.Conf},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			if got, err := fileHash(ctx, sys, dnfauto.Conf); err != nil || got != after {
				return engine.Ran{}, errors.Join(err, changedSince(dnfauto.Conf, "edited"))
			}
			if err := host.WriteFileAtomicKeep(ctx, sys, dnfauto.Conf, out, 0o644, 0, 0); err != nil {
				return engine.Ran{}, err
			}
			return engine.Ran{Detail: "dnf-automatic: upgrade_type put back"}, nil
		}}, before, nil
}

// removeChange removes dnf-automatic, which hardn installed; undone by its
// install.
func removeChange(rm packages.Removal) engine.Change {
	return engine.Change{ID: ChangePackage, Summary: "Remove dnf-automatic", Title: "Remove dnf-automatic", Target: dnfauto.Package,
		StateBefore: "installed", StateAfter: "not installed", Commands: []string{host.FormatArgv(rm.Argv)},
		Risk: engine.RiskPackageInstall, Stage: engine.StageFiles, Resources: []string{ID},
		Rollback: engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: slices.Clone(rm.Undo), Why: ID}}},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			return engine.Ran{Detail: "dnf-automatic: removed"}, packages.ExecuteRemove(ctx, sys, rm)
		}}
}
