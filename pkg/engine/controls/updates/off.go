package updates

import (
	"context"
	"crypto/md5" //nolint:gosec // dpkg lists its conffiles' MD5s
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/aptconf"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// aptconfDir is apt.conf.d, where ucf keeps its side files.
const aptconfDir = aptconf.PartsDir

// changedSince is a refusal naming a file changed since hardn left it.
func changedSince(name, since string) error {
	return refuse(codeChangedSince, ErrChangedSince, nil, fmt.Sprintf("Not undone: %s changed since hardn %s it", name, since), "put it back as it was or undo the rest by hand")
}

// runChange is a change that runs argv, undone by undo.
func runChange(id, title, target string, argv, undo []string, risk engine.RiskClass) engine.Change {
	return engine.Change{ID: id, Summary: title, Title: title, Target: target, Commands: []string{host.FormatArgv(argv)},
		Risk: risk, Stage: engine.StageFiles, Resources: []string{ID},
		Rollback: engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: undo, Why: ID}}},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			_, err := sys.Run(ctx, argv[0], argv[1:]...)
			return engine.Ran{Detail: title}, err
		}}
}

// planOff is off (decisions 2, 5, 6, R2): the inverse of each recorded
// step, last first, each only while the host is as hardn left it, a step
// changed since refusing, naming the file; with no record, hardn's own
// file deleted while it holds hardn's bytes and no other file keeps
// updates on without it, and nothing else. Off counts only what the record
// says hardn did. Then in.Config saved as given (steps.PlanSave) where its
// enableUnattendedUpgrades is not the file's: the caller sets it false
// where the loaded hardn.yml says true; and the record deleted.
// notes are what stays and why, for the review.
func (c Auto) planOff(ctx context.Context, sys host.System, in engine.Input, r Reading) (cs []engine.Change, notes []Note, err error) {
	switch {
	case r.PVE:
		return nil, nil, refuse(codeProxmox, ErrProxmox, nil, "hardn doesn't change updates on Proxmox VE", "change them by hand")
	case r.RecordErr != nil:
		return nil, nil, c.recordRefusal(r.RecordErr)
	}
	if err := unreadRefusal(ctx, sys, r); err != nil {
		return nil, nil, err
	}
	done := stepsDone(r.Record)
	add := func(ch engine.Change) { ch.After = changeIDs(cs); cs = append(cs, ch) }
	if r.Apk {
		err = planApkOff(ctx, sys, r, done, add, &notes)
	} else {
		err = planAptOff(ctx, sys, r, done, add, &notes)
	}
	if err != nil {
		return nil, nil, err
	}
	if len(cs) == 0 && r.Record == nil {
		return nil, nil, refuse(codeNothing, ErrNothingToUndo, nil, "Nothing to undo: hardn did not turn this on", "turn it off by hand")
	}
	if cfg := r.Config; cfg != nil && cfg.Path() != "" && settingChanged(ctx, sys, cfg) {
		save, err := steps.PlanSave(ctx, sys, ChangeSave, cfg, []string{Setting}, changeIDs(cs)...)
		if err != nil {
			return nil, nil, err
		}
		cs = append(cs, save...)
	}
	if r.Record != nil {
		name, err := c.RecordPath()
		if err != nil {
			return nil, nil, err
		}
		cs = append(cs, engine.Change{ID: ChangeRecord, Summary: "Delete hardn's record " + name, Title: "Delete hardn's record", Target: name,
			Stage: engine.StageVerify, Resources: []string{ID}, Backups: []string{name}, After: changeIDs(cs),
			Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
				return engine.Ran{Detail: "record: deleted"}, sys.Remove(ctx, name)
			}})
	}
	return cs, notes, nil
}

// settingChanged reports whether cfg's enableUnattendedUpgrades is not the
// one hardn.yml at its path gives, or whether the file cannot be read
// there, so that steps.PlanSave says why it cannot be saved. cfg is saved
// as given, so that the save's config.Save records the bytes it wrote in
// it, as screenkit.Adopt reads them.
func settingChanged(ctx context.Context, sys host.System, cfg *config.Config) bool {
	loaded, err := config.Read(ctx, sys, cfg.Path())
	return err != nil || loaded.EnableUnattendedUpgrades != cfg.EnableUnattendedUpgrades
}

// offReadBack is off's read-back after its last step: the check; updates
// still on fail the change, rolled back.
func offReadBack(ctx context.Context, sys host.System, f detect.Facts) error {
	st, err := check(ctx, sys, f)
	if err != nil {
		return err
	}
	if st.State == engine.Configured || st.State == engine.Partial {
		why := "apt still reads updates as on after the change (" + st.Detail + ")"
		if f.Platform.Distro == detect.DistroAlpine {
			why = "updates still run after the change (" + st.Detail + ")"
		}
		return refuse(codeNotInEffect, ErrNotInEffect, nil, why, "find what turns them on, then try again")
	}
	return nil
}

// withReadBack adds mine, its last change reading the setting back after
// its own step (offReadBack), so that an off still on fails, rolled back.
func withReadBack(mine []engine.Change, add func(engine.Change)) {
	if len(mine) == 0 {
		return
	}
	last := &mine[len(mine)-1]
	apply := last.Apply
	last.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
		ran, err := apply(ctx, sys, run)
		if err != nil {
			return ran, err
		}
		return ran, offReadBack(ctx, sys, run.Facts)
	}
	for _, c := range mine {
		add(c)
	}
}

// rollbackNotes are the review's lines on rolling back an off that
// removes a package: the reinstall is from the package repositories, and
// on Debian it turns updates on through 20auto-upgrades (debconf's
// default).
func rollbackNotes(debian bool) []Note {
	out := []Note{{Key: "Rollback", Value: "Reinstalls from the repositories"}}
	if debian {
		out = append(out, Note{Key: "Rollback", Value: "Turns updates on", Note: "by 20auto-upgrades", Warn: true})
	}
	return out
}

// planAptOff is Debian's and Ubuntu's off: hardn's file deleted, then the
// package hardn installed (planAptPackageOff); the last step reads the
// setting back.
func planAptOff(ctx context.Context, sys host.System, r Reading, done []string, add func(engine.Change), notes *[]Note) error {
	var mine []engine.Change
	data, _, err := sys.ReadFileNoFollow(ctx, HardnFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		h := sha256.Sum256(data)
		sum := hex.EncodeToString(h[:])
		want, ok := "", false
		if r.Record != nil {
			want, ok = recordedHash(r.Record, HardnFile)
		}
		if ok && want != sum || !ok && !IsHardnFile(data) {
			return changedSince(HardnFile, "wrote")
		}
		if r.Record == nil && r.StayOn != "" {
			return refuse(codeStaysOn, ErrStaysOn, nil, "Updates stay on without hardn's file: "+path.Base(r.StayOn)+" turns them on, and hardn has no record that it installed unattended-upgrades", "turn them off by hand")
		}
		before := string(data)
		mine = append(mine, engine.Change{ID: ChangeFile, Summary: "Turn off daily upgrades: delete " + HardnFile, Title: "Turn off daily upgrades", Target: HardnFile,
			Diff: engine.FileDiff(HardnFile, &before, nil), Stage: engine.StageFiles, Resources: []string{ID}, Backups: []string{HardnFile},
			Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
				if got, err := fileHash(ctx, sys, HardnFile); err != nil || got != sum {
					return engine.Ran{}, errors.Join(err, changedSince(HardnFile, "wrote"))
				}
				if err := sys.Remove(ctx, HardnFile); err != nil {
					return engine.Ran{}, err
				}
				return engine.Ran{Detail: "apt: hardn's lines gone"}, nil
			}})
	}
	ch, err := planAptPackageOff(ctx, sys, r, done, notes)
	if err != nil {
		return err
	}
	if ch != nil {
		mine = append(mine, *ch)
	}
	withReadBack(mine, add)
	return nil
}

// planAptPackageOff is the package hardn installed, at off (decision 5,
// R2): purged only where nothing purge deletes holds what hardn would
// lose; otherwise removed, the files kept and said. Purge deletes, edited
// or not: 20auto-upgrades and 50unattended-upgrades (the postrm), so both
// must have been absent before hardn's install and still hold the bytes
// recorded after it; every dpkg conffile of the package, so each must
// still hold the MD5 dpkg lists for it (a line "obsolete" or
// "newconffile", or one in another form, never matches); and ucf's side
// files, so no 20auto-upgrades.ucf-* or 50unattended-upgrades.ucf-* may
// exist. Every file the purge would delete is in the change's Backups,
// and the same set is checked again before the purge runs. The
// dependencies the install added stay, said.
func planAptPackageOff(ctx context.Context, sys host.System, r Reading, done []string, notes *[]Note) (*engine.Change, error) {
	if !slices.Contains(done, stepInstall) {
		return nil, nil
	}
	if r.PackageErr != nil || r.Package != detect.Yes {
		return nil, r.PackageErr
	}
	rec := r.Record
	purge := true
	want := map[string]string{}
	var files []Note
	for _, p := range []string{AutoUpgrades, UnattendedConfig} {
		want[p], _ = recordedHash(rec, p)
		was, _ := recordedBefore(rec, p)
		got, err := fileHash(ctx, sys, p)
		var why string
		switch {
		case err == nil && got == "": // nothing to keep, or to lose
			want[p] = ""
			continue
		case err != nil:
			why = "can't read it"
		case was != "":
			why = "there before hardn"
		case got != want[p]:
			why = "changed since hardn"
		default:
			files = append(files, Note{Key: path.Base(p), Value: "Kept", Note: "apt-get remove keeps it"})
			continue
		}
		purge = false
		files = append(files, Note{Key: path.Base(p), Value: "Kept", Note: why, Warn: true})
	}
	conf, err := conffiles(ctx, sys)
	if err != nil {
		return nil, err
	}
	for _, c := range conf {
		if !c.intact(ctx, sys) {
			purge = false
			files = append(files, Note{Key: path.Base(c.path), Value: "Kept", Note: "changed since installed", Warn: true})
		}
	}
	sides, err := ucfSides(ctx, sys)
	if err != nil {
		return nil, err
	}
	for _, s := range sides {
		purge = false
		files = append(files, Note{Key: path.Base(s), Value: "Kept", Note: "changed since installed", Warn: true})
	}
	if !purge {
		*notes = append(*notes, files...)
	}
	verb := "purge"
	if !purge {
		verb = "remove"
	}
	res, err := sys.Run(ctx, "apt-get", verb, "-s", "-y", Package)
	if err != nil {
		return nil, wrap(err)
	}
	var other []string
	for _, l := range strings.Split(string(res.Stdout), "\n") {
		if f := strings.Fields(l); len(f) > 1 && (f[0] == "Remv" || f[0] == "Purg" || f[0] == "Inst") && f[1] != Package {
			other = append(other, strings.Join(f[:2], " "))
		}
	}
	if len(other) > 0 {
		return nil, refuse(codeOtherPackages, ErrOtherPackages, nil, "Removing unattended-upgrades would also change other packages: "+strings.Join(other, ", "), "remove it by hand")
	}
	for _, e := range rec.Entries {
		for _, d := range e.Packages {
			if d != Package { // a row each: the review's rows are 64 cells
				*notes = append(*notes, Note{Key: d, Value: "Kept", Note: "apt autoremove removes it"})
			}
		}
	}
	*notes = append(*notes, rollbackNotes(r.Facts.Platform.Distro == detect.DistroDebian)...)
	c := runChange(ChangePackage, "Remove unattended-upgrades", Package, []string{"apt-get", verb, "-y", Package}, []string{"apt-get", "install", "-y", Package}, engine.RiskPackageInstall)
	c.StateBefore, c.StateAfter = "installed", map[bool]string{true: "purged, with 20auto-upgrades", false: "removed, its files kept"}[purge]
	if purge {
		c.Backups = []string{AutoUpgrades, UnattendedConfig}
		for _, cf := range conf {
			c.Backups = append(c.Backups, cf.path)
		}
		apply := c.Apply
		c.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
			for _, p := range []string{AutoUpgrades, UnattendedConfig} {
				if got, err := fileHash(ctx, sys, p); err != nil || got != want[p] {
					return engine.Ran{}, errors.Join(err, changedSince(p, "installed"))
				}
			}
			for _, cf := range conf {
				if !cf.intact(ctx, sys) {
					return engine.Ran{}, changedSince(cf.path, "installed")
				}
			}
			sides, err := ucfSides(ctx, sys)
			if err != nil || len(sides) > 0 {
				return engine.Ran{}, errors.Join(err, changedSince(strings.Join(sides, ", "), "installed"))
			}
			return apply(ctx, sys, run)
		}
	}
	return &c, nil
}

// conffile is one of the package's dpkg conffiles: its path and the MD5
// dpkg lists for it, "" where the line can't match (obsolete,
// newconffile, another form).
type conffile struct{ path, md5 string }

// conffiles are the package's dpkg conffiles, as dpkg-query lists them:
// " <path> <md5>" a line (00-inspect-deb13-vm.log, "conffiles").
func conffiles(ctx context.Context, sys host.System) ([]conffile, error) {
	res, err := sys.Run(ctx, "dpkg-query", "-W", `-f=${Conffiles}\n`, Package)
	if err != nil {
		return nil, wrap(err)
	}
	var out []conffile
	for _, l := range strings.Split(string(res.Stdout), "\n") {
		f := strings.Fields(l)
		if len(f) == 0 {
			continue
		}
		c := conffile{path: f[0]}
		if len(f) == 2 && len(f[1]) == 32 && strings.Trim(f[1], "0123456789abcdef") == "" {
			c.md5 = f[1]
		}
		out = append(out, c)
	}
	return out, nil
}

// intact is whether the conffile still holds the bytes dpkg lists: read
// without following a link, its MD5 the listed one.
func (c conffile) intact(ctx context.Context, sys host.System) bool {
	if c.md5 == "" {
		return false
	}
	data, _, err := sys.ReadFileNoFollow(ctx, c.path)
	if err != nil {
		return false
	}
	sum := md5.Sum(data) //nolint:gosec // dpkg's conffile hash, compared, not a security boundary
	return hex.EncodeToString(sum[:]) == c.md5
}

// ucfSides are ucf's side files of 20auto-upgrades and
// 50unattended-upgrades (".ucf-dist", ".ucf-old" and the like), which
// purge deletes with them.
func ucfSides(ctx context.Context, sys host.System) ([]string, error) {
	des, err := sys.ReadDir(ctx, aptconfDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, wrap(err)
	}
	var out []string
	for _, de := range des {
		n := de.Name()
		if strings.HasPrefix(n, path.Base(AutoUpgrades)+".ucf-") || strings.HasPrefix(n, path.Base(UnattendedConfig)+".ucf-") {
			out = append(out, path.Join(aptconfDir, n))
		}
	}
	return out, nil
}

// addedSince are the jobs crond runs now, by name, that it did not run at
// Enable (before, by path).
func addedSince(jobs []alpine.Job, before []string) []string {
	var added []string
	for _, j := range jobs {
		if !slices.Contains(before, j.Path) {
			added = append(added, j.Name)
		}
	}
	return added
}

// jobsUnchanged wraps a crond change's Apply: alpine.OtherJobs read again
// first, failing where a job has appeared since the plan.
func jobsUnchanged(c engine.Change, before []string) engine.Change {
	apply := c.Apply
	c.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
		jobs, err := alpine.OtherJobs(ctx, sys)
		if err != nil {
			return engine.Ran{}, refuse(codeNotRead, ErrNotRead, err, "hardn can't read how updates run here: "+firstLine(err.Error()), "fix that, then try again")
		}
		if added := addedSince(jobs, before); len(added) > 0 {
			return engine.Ran{}, refuse(codeChangedSince, ErrChangedSince, nil, "crond: "+strings.Join(added, ", ")+" added since", "try again, and hardn keeps crond running")
		}
		return apply(ctx, sys, run)
	}
	return c
}

// planApkOff is Alpine's off: crond stopped and out of the runlevel where
// the record says hardn started it (StepStart: a StepRestart's crond was
// running before hardn) and added it and no job has appeared since
// (decision 6), else kept and said, each checked again before it runs;
// apk-cron removed behind apk del --simulate, refused while its script
// differs from the record, checked again before apk del runs;
// origin/main's apk-upgrade never written back. The last step reads the
// setting back.
func planApkOff(ctx context.Context, sys host.System, r Reading, done []string, add func(engine.Change), notes *[]Note) error {
	rec := r.Record
	if rec == nil {
		return nil
	}
	u := r.Updates
	if r.JobsErr != nil {
		return refuse(codeNotRead, ErrNotRead, r.JobsErr, "hardn can't read how updates run here: "+firstLine(r.JobsErr.Error()), "fix that, then try again")
	}
	if slices.Contains(done, alpine.StepRemoveLegacy.String()) {
		*notes = append(*notes, Note{Key: "Old Script", Value: "Stays removed", Note: path.Base(alpine.LegacyScript)})
	}
	before := jobsBefore(rec)
	added := addedSince(r.Jobs, before)
	started := slices.Contains(done, alpine.StepStart.String())
	booted := slices.Contains(done, alpine.StepRunlevel.String())
	switch {
	case (started || booted) && len(added) > 0:
		*notes = append(*notes, Note{Key: alpine.Service, Value: "Kept", Note: strings.Join(added, ", ") + " added since"})
		started, booted = false, false
	case !started && u.Crond == alpine.CrondStarted:
		*notes = append(*notes, Note{Key: alpine.Service, Value: "Kept", Note: "running before hardn"})
	}
	var mine []engine.Change
	if started && u.Crond == alpine.CrondStarted {
		mine = append(mine, jobsUnchanged(runChange(ChangeCrondStop, "Stop crond", alpine.Service, []string{"rc-service", alpine.Service, "stop"}, []string{"rc-service", alpine.Service, "start"}, engine.RiskServiceRestart), before))
	}
	if booted && u.Runlevel == detect.Yes {
		mine = append(mine, jobsUnchanged(runChange(ChangeCrondBoot, "Stop crond at boot", alpine.Service, []string{"rc-update", "del", alpine.Service, alpine.Runlevel}, []string{"rc-update", "add", alpine.Service, alpine.Runlevel}, engine.RiskPlain), before))
	}
	if slices.Contains(done, alpine.StepInstall.String()) && u.Package == detect.Yes {
		want, _ := recordedHash(rec, alpine.Script)
		if got, err := fileHash(ctx, sys, alpine.Script); err != nil || got != want {
			return changedSince(alpine.Script, "installed")
		}
		res, err := sys.Run(ctx, "apk", "del", "--simulate", alpine.Package)
		if err != nil {
			return wrap(err)
		}
		if cs := alpine.Changes(res.Combined); len(cs) != 1 || !strings.HasPrefix(cs[0], "Purging "+alpine.Package+" (") {
			return refuse(codeOtherPackages, ErrOtherPackages, nil, "Removing apk-cron would also change other packages: "+strings.Join(cs, ", "), "remove it by hand")
		}
		*notes = append(*notes, rollbackNotes(false)...)
		c := runChange(ChangeApk, "Remove apk-cron", alpine.Package, []string{"apk", "del", alpine.Package}, []string{"apk", "add", alpine.Package}, engine.RiskPackageInstall)
		apply := c.Apply
		c.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
			if got, err := fileHash(ctx, sys, alpine.Script); err != nil || got != want {
				return engine.Ran{}, errors.Join(err, changedSince(alpine.Script, "installed"))
			}
			return apply(ctx, sys, run)
		}
		mine = append(mine, c)
	}
	withReadBack(mine, add)
	return nil
}
