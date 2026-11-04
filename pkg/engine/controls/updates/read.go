package updates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/aptconf"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// Timer is one of apt's timers and the service it starts, as systemctl
// prints their states ("" where not read, with Err).
type Timer struct {
	Timer, Service                  string
	Active, Enabled, ServiceEnabled string
	Err                             error
}

// Reading is the host as the Auto Updates screen draws it (unit (c)), as
// facts: everything the mock's updReading held, so that the screen draws
// from it alone.
type Reading struct {
	Facts  detect.Facts
	Config *config.Config // hardn.yml as loaded; read only for off's save
	// Status is the row's line: the check's with the record's finding,
	// NotApplicable "updated by hand" on Proxmox VE; StatusErr the
	// check's error.
	Status    engine.Status
	StatusErr error
	PVE       bool // Proxmox VE (decision 7)
	Apk       bool // Alpine: apk-cron, not apt
	// apt: each value and the file that sets it (aptconf.Read over the
	// check's roots), or AptErr; the package; the timers; whether an origin
	// is allowed (the check's own reading); hardn's own file holding bytes
	// hardn writes (Own); the package's files there while it is not
	// installed (Present); and, with no record, the file that keeps
	// updates on without hardn's (StayOn).
	Apt            aptconf.Config
	AptErr         error
	Package        detect.Tri
	PackageErr     error
	Upgrade, Lists Timer
	Origins        bool
	Own            bool
	Present        []string
	StayOn         string
	// Alpine: alpine.ReadUpdates', and the other jobs crond runs.
	Updates    alpine.Updates
	UpdatesErr error
	Jobs       []alpine.Job
	JobsErr    error
	// The record (nil for none) or why it is not read; the file of hardn's
	// the host no longer holds as recorded (Changed), and the package
	// hardn installed gone (PackageGone): decision 9's finding.
	Record      *revert.ControlRecord
	RecordErr   error
	Changed     string
	PackageGone bool
}

// Read reads the host as the screen draws it, writing nothing: the check,
// apt's configuration (aptconf.Read), dpkg-query, systemctl, or
// alpine.ReadUpdates and alpine.OtherJobs, the record and hardn's file. A
// read that fails is kept beside what it would have given; Read itself
// returns no error but ctx's.
func (c Auto) Read(ctx context.Context, sys host.System, f detect.Facts, cfg *config.Config) (Reading, error) {
	r := Reading{Facts: f, Config: cfg, PVE: f.Platform.Distro == detect.DistroProxmox, Apk: f.Platform.Distro == detect.DistroAlpine}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	r.Record, r.RecordErr = c.readRecord(ctx, sys)
	if ok, why := c.Applies(f); !ok {
		r.Status = engine.Status{State: engine.NotApplicable, Detail: why}
	} else {
		r.Status, r.StatusErr = check(ctx, sys, f)
	}
	if r.Apk {
		r.Updates, r.UpdatesErr = alpine.ReadUpdates(ctx, sys, f)
		r.Jobs, r.JobsErr = alpine.OtherJobs(ctx, sys)
		r.Changed = changedFile(ctx, sys, r.Record, alpine.Script)
		r.PackageGone = r.Record != nil && installedBy(r.Record) && r.Updates.Package == detect.No
	} else {
		r.Apt, r.AptErr = aptconf.Read(ctx, sys, dumpArgs[1:]...)
		r.Package, r.PackageErr = detect.DpkgInstalled(ctx, sys, Package)
		r.Upgrade = readTimer(ctx, sys, upgradeTimer, upgradeSvc)
		r.Lists = readTimer(ctx, sys, listsTimer, listsSvc)
		if d, err := dump(ctx, sys); err == nil {
			r.Origins = len(d.children(keyAllowed))+len(d.children(keyPattern)) > 0
		}
		if data, _, err := sys.ReadFileNoFollow(ctx, HardnFile); err == nil {
			r.Own = IsHardnFile(data)
		}
		for _, p := range []string{AutoUpgrades, UnattendedConfig} {
			if sum, err := fileHash(ctx, sys, p); err == nil && sum != "" && r.Package == detect.No {
				r.Present = append(r.Present, p)
			}
		}
		r.Changed = changedFile(ctx, sys, r.Record, HardnFile)
		r.PackageGone = r.Record != nil && installedBy(r.Record) && r.Package == detect.No
		if r.Record == nil && r.RecordErr == nil && r.Own {
			r.StayOn = r.stayOnWithout()
		}
	}
	if why, ok := staleFinding(r.Changed, r.PackageGone, r.Apk); ok && r.Status.State != engine.NotApplicable {
		r.Status.Findings = append(r.Status.Findings, why)
	}
	return r, ctx.Err()
}

// readTimer reads a timer's is-active and is-enabled, and its service's
// is-enabled; the first read that fails is Err.
func readTimer(ctx context.Context, sys host.System, timer, svc string) Timer {
	t := Timer{Timer: timer, Service: svc}
	var errs [3]error
	t.Active, errs[0] = detect.SystemctlIsActive(ctx, sys, timer)
	t.Enabled, errs[1] = detect.SystemctlIsEnabled(ctx, sys, timer)
	t.ServiceEnabled, errs[2] = detect.SystemctlIsEnabled(ctx, sys, svc)
	t.Err = wrap(errors.Join(errs[:]...))
	return t
}

// On is whether the check reads updates as running (Configured or
// Partial).
func (r Reading) On() bool {
	return r.Status.State == engine.Configured || r.Status.State == engine.Partial
}

// ByHardn is "on by hardn": a record, hardn's own file, or a record hardn
// can't read (R1: Disable offered, refusing when chosen).
func (r Reading) ByHardn() bool { return r.Record != nil || r.Own || r.RecordErr != nil }

// Unread is whether a fact either direction needs is not established.
func (r Reading) Unread() bool {
	if r.Apk {
		return r.UpdatesErr != nil || r.Updates.Err != nil || r.Status.State == engine.Unknown
	}
	return r.AptErr != nil || r.Status.State == engine.Unknown
}

// enableOff is the file that sets APT::Periodic::Enable to a decimal 0,
// and whether one does.
func (r Reading) enableOff() (string, bool) {
	return enableOffIn(r.Apt)
}

// enableOffIn is enableOff of c: the job exits on `[ $AutoAptEnable -eq
// 0 ]` (apt.systemd.daily), which an empty or other non-decimal value
// fails, so only a decimal of zeros turns the jobs off.
func enableOffIn(c aptconf.Config) (string, bool) {
	v, file, ok := c.Value(keyEnable)
	return file, ok && decimal(v) && strings.Trim(v, "0") == ""
}

// The items' descriptions where a direction is in the way (R1; decided,
// gallery-updates.md §5).
const (
	descUnread    = "Can't read"
	descAptUnread = "Can't read apt's settings"
	descRecord    = "Can't read hardn's record"
	descEnableOff = "Periodic jobs are off"
	descNoOrigin  = "No origin allowed"
	descScript    = "apk-cron's script changed"
)

// Blocked is what is in the way of the direction (off true: Disable), in
// the item's decided words, as Plan refuses; "" for nothing.
func (r Reading) Blocked(off bool) string {
	unread := descAptUnread
	if r.Apk {
		unread = descUnread
	}
	switch {
	case off && r.RecordErr != nil:
		return descRecord
	case r.Unread():
		return unread
	case off && r.Changed == alpine.Script:
		return descScript
	case off && r.Changed != "":
		return path.Base(r.Changed) + " changed"
	case off && r.StayOn != "":
		return path.Base(r.StayOn) + " keeps updates on"
	case off || r.Apk:
		return ""
	}
	if _, off := r.enableOff(); off {
		return descEnableOff
	}
	for _, t := range []struct {
		name string
		t    Timer
	}{{"Upgrade", r.Upgrade}, {"Lists", r.Lists}} {
		switch t.t.ServiceEnabled {
		case "masked", "masked-runtime":
			return t.name + " job is masked"
		case "not-found":
			return t.name + " job is missing"
		}
		if t.t.Active != "active" || t.t.Enabled != "enabled" {
			return t.name + " timer is off"
		}
	}
	if r.Package == detect.Yes && !r.Origins {
		return descNoOrigin
	}
	return ""
}

// stayOnWithout is the file that turns upgrades on in apt's configuration
// without hardn's file, "" where none does or the package is not
// installed (aptconf's Without: worked out, not checked against apt).
func (r Reading) stayOnWithout() string {
	if r.Package != detect.Yes || r.AptErr != nil {
		return ""
	}
	w := r.Apt.Without(HardnFile)
	if _, off := enableOffIn(w); off {
		return ""
	}
	if v, file, ok := w.Value(keyUpgrade); ok && strings.Trim(v, "0") != "" {
		return file
	}
	return ""
}

// onNotes are Enable's review lines beyond the change: on Alpine the jobs
// a stopped crond also starts (decision 6); on apt, the package's files
// there before its install, which it keeps (R2).
func (r Reading) onNotes() []Note {
	var out []Note
	switch {
	case r.Apk && r.Updates.Crond != alpine.CrondStarted && len(r.Jobs) > 0:
		var names []string
		for _, j := range r.Jobs {
			names = append(names, j.Name)
		}
		out = append(out, Note{Key: "Also starts", Value: strings.Join(names, ", ")})
	case !r.Apk && r.Package == detect.No:
		for _, p := range r.Present {
			out = append(out, Note{Key: path.Base(p), Value: "Kept", Note: "there before hardn"})
		}
	}
	return out
}

// RecordPath is the control's record under c.Records' state directory:
// /var/lib/hardn/controls/updates.auto.json by default.
func (c Auto) RecordPath() (string, error) { return revert.RecordPath(c.Records, ID) }

// readRecord is revert.ReadRecord of the control's.
func (c Auto) readRecord(ctx context.Context, sys host.System) (*revert.ControlRecord, error) {
	return revert.ReadRecord(ctx, sys, c.Records, ID)
}

// stale is the record's finding on the line, read for Check.
func (c Auto) stale(ctx context.Context, sys host.System, f detect.Facts) (engine.Finding, bool) {
	rec, err := c.readRecord(ctx, sys)
	if err != nil || rec == nil {
		return engine.Finding{}, false
	}
	apk := f.Platform.Distro == detect.DistroAlpine
	name, gone := HardnFile, false
	if apk {
		name = alpine.Script
		if installedBy(rec) {
			t, err := alpine.Installed(ctx, sys, alpine.Package)
			gone = err == nil && t == detect.No
		}
	} else if installedBy(rec) {
		t, err := detect.DpkgInstalled(ctx, sys, Package)
		gone = err == nil && t == detect.No
	}
	return staleFinding(changedFile(ctx, sys, rec, name), gone, apk)
}

// staleFinding is CodeChangedSince for a file changed (its path the
// source) or the package gone.
func staleFinding(changed string, gone, apk bool) (engine.Finding, bool) {
	switch {
	case changed != "":
		return engine.Finding{Code: CodeChangedSince, Detail: DetailChangedSince, Source: changed}, true
	case gone && apk:
		return engine.Finding{Code: CodeChangedSince, Detail: DetailChangedSince, Source: "apk info -e " + alpine.Package}, true
	case gone:
		return engine.Finding{Code: CodeChangedSince, Detail: DetailChangedSince, Source: "dpkg-query --show " + Package}, true
	}
	return engine.Finding{}, false
}

// changedFile is name where the record holds its hash and the file no
// longer matches it (or cannot be read), "" otherwise.
func changedFile(ctx context.Context, sys host.System, rec *revert.ControlRecord, name string) string {
	if rec == nil {
		return ""
	}
	want, ok := recordedHash(rec, name)
	if !ok {
		return ""
	}
	if got, err := fileHash(ctx, sys, name); err != nil || got != want {
		return name
	}
	return ""
}

// fileHash is the SHA-256 of name, read without following a link, "" where
// it is absent.
func fileHash(ctx context.Context, sys host.System, name string) (string, error) {
	data, _, err := sys.ReadFileNoFollow(ctx, name)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// recordedHash is the hash the record holds for name after an apply, the
// latest entry's, and whether it holds one.
func recordedHash(rec *revert.ControlRecord, name string) (string, bool) {
	for i := len(rec.Entries) - 1; i >= 0; i-- {
		for _, f := range rec.Entries[i].Files {
			if f.Path == name {
				return f.SHA256, true
			}
		}
	}
	return "", false
}

// recordedBefore is the hash the record holds for name before hardn's
// install ("" absent), and whether it holds one.
func recordedBefore(rec *revert.ControlRecord, name string) (string, bool) {
	for _, e := range rec.Entries {
		for _, f := range e.Before {
			if f.Path == name {
				return f.SHA256, true
			}
		}
	}
	return "", false
}

// stepsDone are the steps the record's entries ran, in order.
func stepsDone(rec *revert.ControlRecord) []string {
	if rec == nil {
		return nil
	}
	var out []string
	for _, e := range rec.Entries {
		out = append(out, e.Steps...)
	}
	return out
}

// jobsBefore are the other jobs crond would run at Enable, by path.
func jobsBefore(rec *revert.ControlRecord) []string {
	var out []string
	for _, e := range rec.Entries {
		out = append(out, e.Jobs...)
	}
	return out
}

// installedBy is whether the record's entries installed the package
// ("install": apt's step and alpine.StepInstall's word alike).
func installedBy(rec *revert.ControlRecord) bool {
	return slices.Contains(stepsDone(rec), stepInstall)
}
