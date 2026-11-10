package updates

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dnfauto"
	"github.com/abbott/hardn/pkg/host/packages"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The record's step words for apt; Alpine's are alpine.Step's.
const (
	stepInstall = "install" // the package (alpine.StepInstall's word too)
	stepFile    = "file"    // hardn's apt file
)

// unreadRefusal is either direction refused for a fact not read: the
// check not established, apt's own error where apt-config fails, or
// aptconf's refusal of the files.
func unreadRefusal(ctx context.Context, sys host.System, r Reading) error {
	if !r.Unread() {
		return nil
	}
	why := "hardn can't read how updates run here"
	cause := errors.Join(r.StatusErr, r.AptErr, r.UpdatesErr, r.Updates.Err, r.DnfUpdates.Err())
	switch {
	case r.Dnf:
		why = "hardn can't read dnf-automatic here"
		if err := r.DnfUpdates.Err(); err != nil {
			why += ": " + firstLine(err.Error())
		}
	case !r.Apk:
		if res, err := sys.Run(ctx, "apt-config", dumpArgs...); err != nil {
			why = "apt can't read its settings: " + strings.TrimPrefix(firstLine(string(res.Stderr)), "E: ")
		} else if r.AptErr != nil {
			why += ": " + firstLine(r.AptErr.Error())
		}
	}
	if cause == nil {
		cause = detect.ErrUnrecognised // the check's Unknown with a finding, no error
	}
	return refuse(codeNotRead, ErrNotRead, cause, why, "fix that, then try again")
}

// planOn is on (decision 3): refused on Proxmox VE (decision 7) and with
// the record unreadable; apt's or Alpine's, then the record.
func (c Auto) planOn(ctx context.Context, sys host.System, in engine.Input, r Reading) ([]engine.Change, error) {
	switch {
	case r.PVE:
		return nil, refuse(codeProxmox, ErrProxmox, nil, "Proxmox VE is updated with apt full-upgrade, and unattended-upgrades wouldn't take its packages", "update it by hand")
	case r.RecordErr != nil:
		return nil, c.recordRefusal(r.RecordErr)
	case r.Apk:
		return c.planApkOn(ctx, sys, in, r)
	case r.Dnf:
		return c.planDnfOn(ctx, sys, r)
	}
	return c.planAptOn(ctx, sys, in, r)
}

// recordRefusal is a direction refused for the record unreadable.
func (c Auto) recordRefusal(err error) error {
	p, _ := c.RecordPath()
	return refuse(codeRecord, ErrRecordUnreadable, err, "hardn can't read its record of what it changed ("+p+")", "undo it by hand")
}

// planAptOn is on for Debian and Ubuntu: unattended-upgrades installed
// when missing (packages.PlanInstall), then hardn's own HardnFile with the
// lines whose effective value is "0" or unset, read back with apt-config
// dump; never debconf, 20auto-upgrades or the timers, so Enable "0", a
// timer off, its job masked or missing, and no origin refuse, with words.
// The lines are chosen at plan time, before the install.
func (c Auto) planAptOn(ctx context.Context, sys host.System, in engine.Input, r Reading) ([]engine.Change, error) {
	if err := unreadRefusal(ctx, sys, r); err != nil {
		return nil, err
	}
	if file, off := r.enableOff(); off {
		return nil, refuse(codePeriodicOff, ErrPeriodicOff, nil, fmt.Sprintf("%s \"0\", in %s, turns apt's daily jobs off", keyEnable, file), "remove that line, then try again")
	}
	for _, t := range []Timer{r.Upgrade, r.Lists} {
		switch t.ServiceEnabled {
		case "not-found":
			return nil, refuse(codeJobMissing, ErrJobMissing, nil, t.Service+" is missing", "reinstall apt, then try again")
		case "masked", "masked-runtime":
			return nil, refuse(codeJobMasked, ErrJobMasked, nil, t.Service+" is masked, and hardn leaves apt's timers to you", "run systemctl unmask "+t.Service+", then try again")
		}
		if t.Active != "active" || t.Enabled != "enabled" {
			return nil, refuse(codeTimerOff, ErrTimerOff, nil, t.Timer+" is off, and hardn leaves apt's timers to you", "run systemctl enable --now "+t.Timer+", then try again")
		}
	}
	var cs []engine.Change
	var entry revert.RecordEntry
	switch {
	case r.PackageErr != nil:
		return nil, r.PackageErr
	case r.Package == detect.No:
		tool, err := packages.ToolFor(in.Facts)
		if err != nil {
			return nil, err
		}
		p, err := packages.PlanInstall(ctx, sys, tool, Package)
		if err != nil {
			return nil, installWords(err)
		}
		entry.Packages = p.Adds
		for _, f := range []string{AutoUpgrades, UnattendedConfig} {
			sum, err := fileHash(ctx, sys, f)
			if err != nil {
				return nil, err
			}
			entry.Before = append(entry.Before, revert.FileHash{Path: f, SHA256: sum, By: Package})
		}
		cs = append(cs, installChange(p))
		entry.Steps = append(entry.Steps, stepInstall)
	case !r.Origins:
		return nil, refuse(codeNoOrigins, ErrNoOrigins, nil, "50unattended-upgrades allows no origin, so nothing would be upgraded", "add your distribution's origins there, then try again")
	}
	var keys []string
	for _, k := range hardnLines {
		if v, _, set := r.Apt.Value(k); !set || v == "0" {
			keys = append(keys, k)
		}
	}
	if len(keys) > 0 {
		cs = append(cs, fileChange(keys, changeIDs(cs)))
		entry.Steps = append(entry.Steps, stepFile)
		for _, k := range keys {
			entry.Lines = append(entry.Lines, k+` "1"`)
		}
	}
	if len(cs) == 0 {
		return nil, nil
	}
	rc, err := c.recordOn(entry, cs)
	if err != nil {
		return nil, err
	}
	return append(cs, rc), nil
}

// installChange is the package's install. It backs up the postinst's
// files, absent before, so a rollback removes them with the package, as
// apt-get remove would not (gallery-updates.md §6's finding).
func installChange(p packages.Plan) engine.Change {
	after := "installed"
	if also := slices.DeleteFunc(slices.Clone(p.Adds), func(n string) bool { return n == Package }); len(also) > 0 {
		after += ", with " + strings.Join(also, ", ")
	}
	return engine.Change{ID: ChangeInstall, Summary: "Install unattended-upgrades", Title: "Install unattended-upgrades", Target: Package,
		StateBefore: "not installed", StateAfter: after, Commands: []string{host.FormatArgv(p.Argv)},
		Risk: engine.RiskPackageInstall, Stage: engine.StagePackages, Resources: []string{ID},
		Backups:  []string{AutoUpgrades, UnattendedConfig},
		Rollback: engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: slices.Clone(p.Undo), Why: ID}}},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			rep, err := packages.Execute(ctx, sys, p)
			u := engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: slices.Clone(rep.Undo), Why: ID}}}
			if len(rep.Undo) == 0 {
				u = engine.Undo{}
			}
			return engine.Ran{Rollback: &u, Detail: "unattended-upgrades: installed"}, err
		}}
}

// fileChange writes hardn's file with keys' lines, then reads them back.
func fileChange(keys, after []string) engine.Change {
	data := hardnBytes(keys)
	text := string(data)
	var lines []string
	for _, k := range keys {
		lines = append(lines, k+` "1"`)
	}
	return engine.Change{ID: ChangeFile, Summary: "Turn on daily upgrades in " + HardnFile, Title: "Turn on daily upgrades", Target: HardnFile,
		Diff: engine.FileDiff(HardnFile, nil, &text), Commands: []string{"apt-config " + strings.Join(dumpArgs, " ")},
		Stage: engine.StageFiles, Resources: []string{ID}, Backups: []string{HardnFile}, After: after,
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			if err := sys.WriteFileAtomic(ctx, HardnFile, data, 0o644, 0, 0); err != nil {
				return engine.Ran{}, err
			}
			return engine.Ran{Detail: "apt: " + strings.Join(lines, ", ") + " in effect"}, readBack(ctx, sys, keys, "1")
		}}
}

// readBack checks that each of keys reads want in apt-config dump, as apt
// reads it (APT_CONFIG unset: host.Real's environment); a mismatch is the
// change failed, and the engine rolls it back.
func readBack(ctx context.Context, sys host.System, keys []string, want string) error {
	d, err := dump(ctx, sys)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if v := d.values[strings.ToLower(k)]; v != want {
			return refuse(codeNotInEffect, ErrNotInEffect, nil, fmt.Sprintf("apt reads %s as %q after the change, not %q: a file read later sets it", k, v, want), "find that file, then try again")
		}
	}
	return nil
}

// installWords are packages.PlanInstall's refusals in the decided words.
func installWords(err error) error {
	var other *packages.OtherPackagesError
	switch {
	case errors.Is(err, packages.ErrNotInRepos):
		return refuse(codeNotInRepos, ErrNotInRepos, err, "hardn can't find unattended-upgrades in apt's package lists", "run apt-get update, then try again")
	case errors.As(err, &other):
		return refuse(codeOtherPackages, ErrOtherPackages, err, "Installing unattended-upgrades would also change other packages: "+strings.Join(other.Changes, ", "), "install it by hand, then try again")
	}
	return err
}

// planApkOn is Alpine's on: alpine.ReadUpdates' reading and PlanUpdates,
// through steps.Alpine, then the record, which holds crond's state and the
// other jobs before (decision 6, R2).
func (c Auto) planApkOn(_ context.Context, _ host.System, _ engine.Input, r Reading) ([]engine.Change, error) {
	u := r.Updates
	if r.UpdatesErr != nil {
		return nil, refuse(codeNotRead, ErrNotRead, r.UpdatesErr, "hardn can't read how updates run here: "+firstLine(r.UpdatesErr.Error()), "fix that, then try again")
	}
	p, err := alpine.PlanUpdates(u)
	if err != nil {
		return nil, refuse(codeNotRead, ErrNotRead, err, "hardn can't plan this: "+firstLine(err.Error()), "fix that, then try again")
	}
	if len(p.Changes) == 0 {
		return nil, nil
	}
	if r.JobsErr != nil {
		return nil, refuse(codeNotRead, ErrNotRead, r.JobsErr, "hardn can't read how updates run here: "+firstLine(r.JobsErr.Error()), "fix that, then try again")
	}
	ch, err := steps.Alpine(ChangeApk, p, "Turn on daily upgrades")
	if err != nil {
		return nil, err
	}
	ch.Title = "Turn on daily upgrades"
	ch.StateBefore = "apk-cron " + map[detect.Tri]string{detect.Yes: "installed", detect.No: "not installed"}[u.Package] + ", crond " + u.Crond.String()
	ch.StateAfter = "apk-cron installed, crond started at boot"
	entry := revert.RecordEntry{Services: []revert.ServiceState{{Name: alpine.Service, State: u.Crond.String(), AtBoot: map[detect.Tri]string{detect.Yes: "yes", detect.No: "no"}[u.Runlevel]}}}
	for _, j := range r.Jobs {
		entry.Jobs = append(entry.Jobs, j.Path)
	}
	for _, s := range p.Changes {
		entry.Steps = append(entry.Steps, s.Step.String())
	}
	rc, err := c.recordOn(entry, []engine.Change{ch})
	if err != nil {
		return nil, err
	}
	return []engine.Change{ch, rc}, nil
}

// recordOn is the record's change (decision 4): written last, after the
// read-back, backed up and journalled as any write, so a rollback of the
// apply puts the record back as it was, or removes it. planned is what the
// plan read: the steps, the packages and what existed before (R2); the
// change adds the time, the plan's id, the state read back and each file's
// hash after.
// fill adds, at apply, what an earlier change of the plan did (the conf
// change's hashes and lines there).
func (c Auto) recordOn(planned revert.RecordEntry, before []engine.Change, fill ...func(*revert.RecordEntry)) (engine.Change, error) {
	name, err := c.RecordPath()
	if err != nil {
		return engine.Change{}, err
	}
	return engine.Change{ID: ChangeRecord, Summary: "Record what hardn changed in " + name, Title: "Record hardn's changes", Target: name,
		Stage: engine.StageVerify, Resources: []string{ID}, Backups: []string{name}, After: changeIDs(before),
		Apply: func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
			st, err := check(ctx, sys, run.Facts)
			if err != nil {
				return engine.Ran{}, err
			}
			e := planned
			for _, f := range fill {
				f(&e)
			}
			now := time.Now
			if c.Records.Now != nil {
				now = c.Records.Now
			}
			e.Direction, e.Time, e.Hardn, e.PlanID = revert.DirectionOn, now().UTC(), c.Version, run.ID
			e.ReadBack = st.State.String() + ": " + st.Detail
			files := map[string]string{}
			if slices.Contains(e.Steps, stepFile) {
				files[HardnFile] = "hardn"
			}
			if slices.Contains(e.Steps, stepConf) {
				files[dnfauto.Conf] = dnfauto.Package
			}
			switch {
			case run.Facts.Platform.Distro.EL():
			case run.Facts.Platform.Distro == detect.DistroAlpine && slices.Contains(e.Steps, alpine.StepInstall.String()):
				files[alpine.Script] = alpine.Package
			case slices.Contains(e.Steps, stepInstall):
				files[AutoUpgrades], files[UnattendedConfig] = Package, Package
			}
			paths := make([]string, 0, len(files))
			for p := range files {
				paths = append(paths, p)
			}
			slices.Sort(paths)
			for _, p := range paths {
				sum, err := fileHash(ctx, sys, p)
				if err != nil {
					return engine.Ran{}, err
				}
				e.Files = append(e.Files, revert.FileHash{Path: p, SHA256: sum, By: files[p]})
			}
			rec, err := c.readRecord(ctx, sys)
			if err != nil {
				return engine.Ran{}, err
			}
			if rec == nil {
				rec = &revert.ControlRecord{SchemaVersion: revert.RecordVersion}
			}
			rec.Entries = append(rec.Entries, e)
			path, data, err := revert.EncodeRecord(c.Records, ID, *rec)
			if err != nil {
				return engine.Ran{}, err
			}
			if err := revert.PrepareRecordDir(ctx, sys, c.Records); err != nil {
				return engine.Ran{}, err
			}
			return engine.Ran{Detail: "record: written"}, sys.WriteFileAtomic(ctx, path, data, 0o600, 0, 0)
		}}, nil
}

// changeIDs are the ids of cs.
func changeIDs(cs []engine.Change) []string {
	var ids []string
	for _, c := range cs {
		ids = append(ids, c.ID)
	}
	return ids
}
