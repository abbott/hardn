package main

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	probe "github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Auto Updates mock (refactor plan slice 7, unit (a), next/s7-gallery;
// docs/engine/gallery-updates.md): a proposal, on the sub-menu layout (no
// Figma frame), drawn from what real code reads of a simulated host
// (updateshost.go): the Auto Updates row from the real check as the main
// menu draws it (Engine.Check, risk.Assess, output.StatusViewOf), the
// section from apt-config dump, dpkg and systemctl, or alpine.ReadUpdates.
// Each change runs on the real engine, plain y/N, rolled back on failure.
// Since unit (b) the section reads updates.Auto's Read (aptconf.Read,
// alpine.OtherJobs, the record through revert.ReadRecord), and each
// direction is the real control, updates.Auto with AutoOn or AutoOff: its
// Plan, its refusals and its review lines (Notes). The screen itself is
// still the gallery's, unit (c)'s to build. "uigallery updates" lists
// the mocks; "play updates" is the menu of the hosts, "play
// updates/<host>" one; the main menu's 6 in play leads to debian13's.

// updPrefix names the mock's hosts: "updates" and "updates/<host>".
const updPrefix = "updates"

// updMainHost is the host the main menu's 6 leads to in play.
const updMainHost = updPrefix + "/debian13"

// updMenuItem is the main menu's item proposed for it (decision 1): key 6,
// the key slice 2b left unused, named as the row is.
var updMenuItem = component.MenuItem{Key: "6", Label: "Auto Updates", Description: "Configure automatic updates"}

// The screen's words (gallery-updates.md §5, proposed).
const (
	updTitle   = "Auto Updates"
	updSection = "Update Configuration"
	updHeading = "Select an option"
	updPrompt  = "Enter your choice"

	updRowPackage  = "unattended-upgrades"
	updRowUpgrades = "Upgrades"
	updRowLists    = "Package Lists"
	updRowEnable   = "Periodic Jobs"
	updRowUpTimer  = "Upgrade Timer"
	updRowLsTimer  = "Lists Timer"
	updRowApkCron  = "apk-cron"
	updRowScript   = "Daily Script"
	updRowCrond    = "crond"
	updRowLegacy   = "Old Script"
	updRowOthers   = "Other Jobs"
	updRowPVE      = "Proxmox VE"
	updRowBy       = "Turned on by"

	updEnable      = "Enable automatic updates"
	updDisable     = "Disable automatic updates"
	updDescInstall = "Install unattended-upgrades"
	updDescApt     = "Daily, by unattended-upgrades"
	updDescApk     = "Daily, by apk-cron"
	updDescUndo    = "Undo hardn's changes"
	updDescOutside = "Turned on outside hardn"
	updDescPVE     = "Not on Proxmox VE"
)

// What hardn writes, as the fake's hosts hold it: its own apt file, and
// the record (P-43 (4)) at revert.RecordPath's default.
const (
	hardnAptFile  = updates.HardnFile
	hardnAptHead  = "// Written by hardn (Auto Updates).\n"
	updRecordPath = "/var/lib/hardn/controls/" + updates.ID + ".json"
	keyEnable     = "APT::Periodic::Enable"
	keyUpgrade    = "APT::Periodic::Unattended-Upgrade"
	keyLists      = "APT::Periodic::Update-Package-Lists"
	upgradeTimer  = "apt-daily-upgrade.timer"
	upgradeJob    = "apt-daily-upgrade.service"
)

// hardnLines are the lines hardn's file can hold, in its order.
var hardnLines = []string{keyLists, keyUpgrade}

// hardnAptBytes is hardn's file holding the lines of keys, each "1", in
// hardnLines' order: the fake's copy of what the control writes, which
// updates.IsHardnFile accepts (TestUpdatesHardnBytes).
func hardnAptBytes(keys []string) []byte {
	var b strings.Builder
	b.WriteString(hardnAptHead)
	for _, k := range hardnLines {
		if slices.Contains(keys, k) {
			fmt.Fprintf(&b, "%s \"1\";\n", k)
		}
	}
	return []byte(b.String())
}

// updHost is one simulated host the mock runs on.
type updHost struct {
	name, label, about, note string
	host                     func() *fakeHost
}

// updHosts are the hosts, in the menu's order.
func updHosts() []updHost {
	const (
		deb = hostFQDN + " runs Debian 13. "
		ubu = hostFQDN + " runs Ubuntu 26.04. "
		alp = hostFQDN + " runs Alpine 3.23 (OpenRC). "
	)
	return []updHost{
		{name: "debian13", label: "Debian 13", about: "The VM's shape", host: updDebian13,
			note: deb + "As the cloud image: unattended-upgrades 2.12 installed and on by the package, 20auto-upgrades the only APT::Periodic file, both timers on. Not hardn's: the item is read-only."},
		{name: "ubuntu2604", label: "Ubuntu 26.04", about: "The VM's shape", host: updUbuntu,
			note: ubu + "As the VM: unattended-upgrades on by the package, 10periodic and 20auto-upgrades, Allowed-Origins, both timers on. Not hardn's: the item is read-only."},
		{name: "alpine323", label: "Alpine 3.23", about: "The VM's shape", host: updAlpine,
			note: alp + "As the VM: apk-cron not installed, /etc/periodic empty, root's stock crontab, crond in no runlevel and stopped. Enable plans the install, the runlevel and the start."},
		{name: "debian-installer", label: "Debian installer", about: "No package", host: updInstaller,
			note: deb + "As debian-installer leaves it without the task: no unattended-upgrades, no APT::Periodic file. Enable installs the package, whose postinst turns updates on by itself (debconf's default), then writes hardn's file."},
		{name: "debian-off", label: "Debian, off", about: "20auto-upgrades says 0", host: updDebianOff,
			note: deb + "The package installed with debconf's answer false: 20auto-upgrades-disabled's two \"0\" lines. Enable writes hardn's file alone."},
		{name: "lists-stale", label: "Lists not refreshed", about: "Partial", host: updListsStale,
			note: deb + "20auto-upgrades turns upgrades on and the list refresh off. Enable writes Update-Package-Lists alone."},
		{name: "timer-off", label: "Timer off", about: "Refused", host: updTimerOff,
			note: deb + "apt-daily-upgrade.timer disabled and stopped by the administrator. Enable is offered, saying so, and refuses: hardn leaves apt's timers alone."},
		{name: "by-hardn-debian", label: "On by hardn, Debian", about: "A record", host: updByHardnDebian,
			note: deb + "debian-installer after hardn's Enable: the package, hardn's file and the record. Disable deletes the file, then purges the package."},
		{name: "by-hardn-alpine", label: "On by hardn, Alpine", about: "A record", host: updByHardnAlpine,
			note: alp + "alpine323 after hardn's Enable: apk-cron, crond at boot and running, the record. Disable stops crond, takes it out of the runlevel and removes apk-cron."},
		{name: "edited", label: "Edited since", about: "Refused, named", host: updEdited,
			note: deb + "by-hardn-debian, then hardn's file edited by hand. The row says so (decision 9); Disable refuses, naming it."},
		{name: "crond-shared", label: "crond shared", about: "crond kept", host: updCrondShared,
			note: alp + "crond running and at boot before hardn, logrotate's daily job, then hardn's Enable. Disable removes apk-cron and keeps crond, saying why."},
		{name: "dormant-jobs", label: "Jobs, crond stopped", about: "Also starts", host: updDormantJobs,
			note: alp + "alpine323 with logrotate's daily job, crond stopped. Enable starts crond and says it starts logrotate too; Disable stops it again."},
		{name: "config-files", label: "Files left", about: "Removed earlier", host: updConfigFiles,
			note: deb + "unattended-upgrades removed earlier, its edited 50unattended-upgrades left. Enable keeps it, saying so; Disable removes the package, not purges."},
		{name: "legacy-alpine", label: "origin/main's script", about: "apk-upgrade", host: updLegacyAlpine,
			note: alp + "origin/main's /etc/periodic/daily/apk-upgrade, which crond never ran. Enable also removes it (§3.20)."},
		{name: "pve9", label: "Proxmox VE 9", about: "Not offered", host: updPVE,
			note: deb + "A Proxmox VE 9 node, with no unattended-upgrades. The row is Not Applicable (decision 7); the item is unavailable."},
		{name: "not-read", label: "Not read", about: "apt-config fails", host: updNotRead,
			note: deb + "A file in apt.conf.d with a syntax error: apt-config dump fails, so the row is Unknown; the item is offered, saying so, and refuses."},
	}
}

// updVariants are hosts the tests and the refusals mock read that the menu
// does not list.
func updVariants() []updHost {
	return []updHost{
		{name: "edited-alpine", note: "by-hardn-alpine, then apk-cron's daily script edited.", host: func() *fakeHost {
			h := updByHardnAlpine()
			h.f.AddFile(alpine.Script, []byte(apkScript+"echo done\n"), 0o755, 0, 0)
			return h
		}},
		{name: "enable-off", note: "debian-off with APT::Periodic::Enable \"0\" in 99site.", host: func() *fakeHost {
			h := updDebianOff()
			h.f.AddFile(aptConfDir+"/99site", []byte(keyEnable+" \"0\";\n"), 0o644, 0, 0)
			return h
		}},
		{name: "own-file", note: "debian-off with hardn's file and no record: on by hardn all the same.", host: func() *fakeHost {
			h := updDebianOff()
			h.f.AddFile(hardnAptFile, hardnAptBytes(hardnLines), 0o644, 0, 0)
			return h
		}},
		{name: "masked", note: "debian-off with apt-daily-upgrade.service masked.", host: func() *fakeHost {
			h := updDebianOff()
			h.status.units[upgradeJob] = unit{enabled: "masked"}
			return h
		}},
		{name: "missing", note: "debian-off with apt-daily-upgrade.service not found.", host: func() *fakeHost {
			h := updDebianOff()
			h.status.units[upgradeJob] = unit{enabled: "not-found"}
			return h
		}},
		{name: "no-origin", note: "debian-off with a 50unattended-upgrades that allows no origin.", host: func() *fakeHost {
			h := updDebianOff()
			h.f.AddFile(uuConfig, []byte("// No origins.\n"), 0o644, 0, 0)
			return h
		}},
		{name: "extra-packages", note: "debian-installer, the install's simulation also upgrading libc6.", host: func() *fakeHost {
			h := updInstaller()
			h.upd.extra = []string{"Inst libc6 [2.41-11] (2.41-12 Debian:13.1/stable [arm64])"}
			return h
		}},
		{name: "later-file", note: hostFQDN + " runs Debian 13. debian-off with 60site, read after hardn's file, setting Unattended-Upgrade \"0\". Enable's read-back fails, and the change is rolled back.", host: func() *fakeHost {
			h := updDebianOff()
			h.f.AddFile(aptConfDir+"/60site", []byte(keyUpgrade+" \"0\";\n"), 0o644, 0, 0)
			return h
		}},
		{name: "no-record", note: "by-hardn-debian with the record gone: 20auto-upgrades keeps updates on without hardn's file.", host: func() *fakeHost {
			h := updByHardnDebian()
			_ = h.f.Remove(context.Background(), updRecordPath)
			return h
		}},
	}
}

// findUpdHost returns the host named name, after updPrefix: the menu's,
// then the variants.
func findUpdHost(name string) (updHost, bool) {
	all := slices.Concat(updHosts(), updVariants())
	i := slices.IndexFunc(all, func(h updHost) bool { return h.name == name })
	if i < 0 {
		return updHost{}, false
	}
	return all[i], true
}

// The hosts' builders.

func updDebian13() *fakeHost { return updApt(debian13Platform, autoOn) }
func updUbuntu() *fakeHost   { return updApt(ubuntu2604Platform, autoOn) }
func updInstaller() *fakeHost {
	h := updApt(debian13Platform, "")
	h.upd.deps = []string{"python3-dbus", "python3-distro-info"}
	return h
}

func updDebianOff() *fakeHost {
	h := updApt(debian13Platform, autoOff)
	h.upd.debconfOff = true
	return h
}

func updListsStale() *fakeHost { return updApt(debian13Platform, listsStale) }

func updTimerOff() *fakeHost {
	h := updApt(debian13Platform, autoOn)
	h.status.units[upgradeTimer] = unit{active: "inactive", enabled: "disabled"}
	return h
}

func updByHardnDebian() *fakeHost { return updApplied(updInstaller()) }
func updByHardnAlpine() *fakeHost { return updApplied(updAlpine()) }

func updEdited() *fakeHost {
	h := updByHardnDebian()
	h.f.AddFile(hardnAptFile, append(hardnAptBytes(hardnLines), []byte(keyEnable+" \"1\";\n")...), 0o644, 0, 0)
	return h
}

// updLogrotate is h with logrotate's daily job.
func updLogrotate(h *fakeHost) *fakeHost {
	h.f.AddFile("/etc/periodic/daily/logrotate", []byte("#!/bin/sh\n/usr/sbin/logrotate /etc/logrotate.conf\n"), 0o755, 0, 0)
	return h
}

func updCrondShared() *fakeHost {
	h := updLogrotate(updAlpine())
	h.upd.crond = true
	h.f.AddSymlink(alpine.RunlevelLink, alpine.InitScript)
	return updApplied(h)
}

func updDormantJobs() *fakeHost { return updLogrotate(updAlpine()) }

func updConfigFiles() *fakeHost {
	h := updApt(debian13Platform, "")
	h.f.AddFile(uuConfig, []byte(uuOrigins(false)+"Unattended-Upgrade::Mail \"root\";\n"), 0o644, 0, 0)
	return h
}

func updLegacyAlpine() *fakeHost {
	h := updAlpine()
	h.f.AddFile(alpine.LegacyScript, []byte(alpine.LegacyContent), 0o755, 0, 0)
	return h
}

func updPVE() *fakeHost { return updApt(pve9Platform, "") }

func updNotRead() *fakeHost {
	h := updApt(debian13Platform, autoOn)
	h.f.AddFile(aptConfDir+"/99site", []byte("APT::Periodic::Verbose 1\n"), 0o644, 0, 0)
	return h
}

// updApplied is h after hardn's Enable, planned and applied on the real
// engine as the screen applies it.
func updApplied(h *fakeHost) *fakeHost {
	ctx := context.Background()
	cfg, err := statusConfig(ctx, h.sys())
	if err != nil {
		return h
	}
	e := updEngine(h, nil, &updQuiet)
	p, err := e.Plan(ctx, []engine.Control{updAuto(updates.AutoOn)}, cfg)
	if err == nil && len(p.Refused) == 0 {
		_, _ = e.Apply(ctx, p)
	}
	return h
}

// updQuiet is the session whose hold the engines outside play take: the
// gallery's, which holds nothing.
var updQuiet session

// updEngine is the engine over h: the gallery's clock and s's hold.
func updEngine(h *fakeHost, progress func(engine.Event), s *session) *engine.Engine {
	return engine.New(h.system(), engine.Options{
		Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
		Session:  applySession,
		Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
		Hold:     s.hold,
		PID:      applyPID,
		Progress: progress,
	})
}

// updReading is the host as the screen reads it: the real control's
// Reading (updates.Auto's Read), and the row as the main menu draws it.
type updReading struct {
	updates.Reading
	status component.StatusList
	check  engine.Status // the real check's, through the engine
}

// readUpdates reads the host as the screen does, writing nothing: the row
// from the engine's check, the rest from updates.Auto's Read.
func readUpdates(ctx context.Context, sys host.System) (updReading, error) {
	var r updReading
	cfg, err := statusConfig(ctx, sys)
	if err != nil {
		return r, err
	}
	var f detect.Facts
	e := engine.New(sys, engine.Options{
		Detect: func(ctx context.Context, sys host.System) (detect.Facts, error) {
			var err error
			f, err = detect.Detect(ctx, sys)
			return f, err
		},
		Hold: updQuiet.hold,
	})
	reports, err := e.Check(ctx, controls.Status(cfg))
	if err != nil {
		return r, err
	}
	for _, rp := range reports {
		if rp.Control == updates.ID {
			r.check = rp.Status
		}
	}
	view := output.StatusViewOf(reports, risk.Assess(reports, probe.Read(ctx, sys, f))).Status
	view.Rows = slices.DeleteFunc(view.Rows, func(row component.StatusRow) bool { return row.Label != (updates.Auto{}).Title() })
	r.status = view
	r.Reading, err = updAuto(updates.AutoNone).Read(ctx, sys, f, cfg)
	if len(r.status.Rows) == 1 {
		// Decision 9 (a)'s line under the row: the control's finding, which
		// output.StatusViewOf does not draw (it draws the risk factors).
		for _, fd := range r.Status.Findings {
			if fd.Code == updates.CodeChangedSince {
				r.status.Rows[0].Lines = append(r.status.Rows[0].Lines, component.StatusLine{Text: fd.Detail, Warn: true})
			}
		}
	}
	return r, err
}

// updAuto is the real control doing a, its record on the gallery's clock.
func updAuto(a updates.AutoAction) updates.Auto {
	return updates.Auto{Action: a, Records: revert.Options{Now: clock, Hardn: galleryHardn}, Version: updVersion}
}

// updVersion is the hardn version the gallery's records carry.
const updVersion = "v0.4.0"

// interval says an APT::Periodic interval: unset and "0" are off.
func interval(v, file string, set bool) (string, string) {
	by := "by " + path.Base(file)
	switch {
	case !set:
		return "Off", "unset"
	case v == "0":
		return "Off", by
	case v == "1":
		return "Daily", by
	case v == "always":
		return "Each run", by
	case strings.Trim(v, "0123456789") == "":
		return "Every " + v + " days", by
	default:
		return v, by
	}
}

// timerRow is one of apt's timers: on, off, off after restart, or its
// service masked or missing.
func timerRow(key string, t updates.Timer) component.KeyValue {
	kv := component.KeyValue{Key: key, Note: t.Timer}
	switch {
	case t.Active == "" || t.Enabled == "":
		kv.Value, kv.State = "Can't read", component.ValueUnknown
	case t.ServiceEnabled == "not-found":
		kv.Value, kv.State, kv.Note = "Missing", component.ValueBad, t.Service
	case t.ServiceEnabled == "masked" || t.ServiceEnabled == "masked-runtime":
		kv.Value, kv.State, kv.Note = "Masked", component.ValueBad, t.Service
	case t.Active != "active":
		kv.Value, kv.State = "Off", component.ValueBad
	case t.Enabled != "enabled":
		kv.Value, kv.State = "Off after restart", component.ValueWarn
	default:
		kv.Value = "On"
	}
	return kv
}

// rows are the section: what the host has, never hardn.yml.
func (r updReading) rows() []component.KeyValue {
	var rows []component.KeyValue
	if r.Apk {
		rows = r.apkRows()
	} else {
		rows = r.aptRows()
	}
	switch {
	case r.Record != nil:
		rows = append(rows, component.KeyValue{Key: updRowBy, Value: "hardn", Note: "record of " + r.Record.Entries[len(r.Record.Entries)-1].Time.Format(time.DateOnly)})
	case r.Own:
		rows = append(rows, component.KeyValue{Key: updRowBy, Value: "hardn", Note: path.Base(updates.HardnFile)})
	case r.RecordErr != nil:
		rows = append(rows, component.KeyValue{Key: updRowBy, Value: "Can't read", State: component.ValueUnknown, Note: "hardn's record"})
	case r.On():
		rows = append(rows, component.KeyValue{Key: updRowBy, Value: "Not hardn"})
	}
	return rows
}

// aptRows are Debian's, Ubuntu's and Proxmox VE's rows.
func (r updReading) aptRows() []component.KeyValue {
	if r.AptErr != nil {
		bad := func(k string) component.KeyValue {
			return component.KeyValue{Key: k, Value: "Can't read", State: component.ValueUnknown}
		}
		return []component.KeyValue{bad(updRowUpgrades), bad(updRowLists)}
	}
	pkg := component.KeyValue{Key: updRowPackage, Value: "Installed", Note: uuVersion}
	switch r.Package {
	case detect.No:
		pkg = component.KeyValue{Key: updRowPackage, Value: "Not installed", State: component.ValueBad}
	case detect.Unknown:
		pkg = component.KeyValue{Key: updRowPackage, Value: "Can't read", State: component.ValueUnknown}
	}
	rows := []component.KeyValue{pkg}
	if v, file, ok := r.Apt.Value(keyEnable); ok && v != "" && strings.Trim(v, "0") == "" {
		rows = append(rows, component.KeyValue{Key: updRowEnable, Value: "Off", State: component.ValueBad, Note: "by " + path.Base(file)})
	}
	for _, kv := range []struct{ key, name string }{{updRowUpgrades, keyUpgrade}, {updRowLists, keyLists}} {
		v, file, set := r.Apt.Value(kv.name)
		val, note := interval(v, file, set)
		row := component.KeyValue{Key: kv.key, Value: val, Note: note}
		if val == "Off" {
			row.State = component.ValueBad
		}
		if set && r.Changed != "" && file == r.Changed {
			row.Note, row.State = "changed since hardn", component.ValueWarn // the item names the file
		}
		rows = append(rows, row)
	}
	rows = append(rows, timerRow(updRowUpTimer, r.Upgrade), timerRow(updRowLsTimer, r.Lists))
	if r.PVE {
		rows = append(rows, component.KeyValue{Key: updRowPVE, Value: "Updates by hand", Note: "apt full-upgrade"})
	}
	return rows
}

// apkRows are Alpine's rows, from alpine.ReadUpdates and alpine.OtherJobs.
func (r updReading) apkRows() []component.KeyValue {
	if r.UpdatesErr != nil {
		return []component.KeyValue{{Key: updRowApkCron, Value: "Can't read", State: component.ValueUnknown}}
	}
	u := r.Updates
	tri := func(t detect.Tri, yes, no string) (string, component.ValueState) {
		switch t {
		case detect.Yes:
			return yes, component.ValuePlain
		case detect.No:
			return no, component.ValueBad
		}
		return "Can't read", component.ValueUnknown
	}
	pv, ps := tri(u.Package, "Installed", "Not installed")
	rows := []component.KeyValue{{Key: updRowApkCron, Value: pv, State: ps, Note: map[bool]string{true: apkCronVer}[u.Package == detect.Yes]}}
	switch u.Script {
	case alpine.ScriptInstalled:
		rows = append(rows, component.KeyValue{Key: updRowScript, Value: "In place", Note: alpine.Script})
	case alpine.ScriptOther:
		rows = append(rows, component.KeyValue{Key: updRowScript, Value: "Not run", State: component.ValueBad, Note: alpine.Script})
	}
	crond := component.KeyValue{Key: updRowCrond}
	switch u.Crond {
	case alpine.CrondStarted:
		crond.Value = "Running"
	case alpine.CrondStopped, alpine.CrondCrashed:
		crond.Value, crond.State = "Stopped", component.ValueBad
	case alpine.CrondMissing:
		crond.Value, crond.State = "Not installed", component.ValueBad
	default:
		crond.Value, crond.State = "Can't read", component.ValueUnknown
	}
	switch u.Runlevel {
	case detect.Yes:
		crond.Note = "starts at boot"
	case detect.No:
		crond.Note = "not at boot"
	}
	rows = append(rows, crond)
	if u.Legacy == alpine.LegacyHardn {
		rows = append(rows, component.KeyValue{Key: updRowLegacy, Value: "origin/main's", State: component.ValueWarn, Note: path.Base(alpine.LegacyScript)})
	}
	switch {
	case r.JobsErr != nil:
		rows = append(rows, component.KeyValue{Key: updRowOthers, Value: "Can't read", State: component.ValueUnknown})
	case len(r.Jobs) > 0:
		rows = append(rows, component.KeyValue{Key: updRowOthers, Value: strings.Join(jobNames(r.Jobs), ", "), Note: "use crond"})
	}
	return rows
}

// jobNames are js' names, in order.
func jobNames(js []alpine.Job) []string {
	var out []string
	for _, j := range js {
		out = append(out, j.Name)
	}
	return out
}

// updExit is the sub-menu's Return.
var updExit = component.MenuItem{Key: "0", Label: "Return"}

// item is the screen's one item, named by its result (P-12): Enable while
// off, Disable while on by hardn, unavailable (P-22) for a state of the
// host itself: on but not by hardn, or Proxmox VE (decision 7). R1: a
// state the user can fix keeps the item offered, its description saying
// what is in the way (Reading.Blocked), and choosing it refuses before
// asking.
func (r updReading) item() component.MenuItem {
	it := component.MenuItem{Key: "1", Label: updEnable, Description: updDescApt}
	switch {
	case r.Apk:
		it.Description = updDescApk
	case r.Package == detect.No:
		it.Description = updDescInstall
	}
	switch {
	case r.PVE:
		it.Description, it.Disabled = updDescPVE, true
	case r.ByHardn():
		it.Label, it.Description = updDisable, updDescUndo
	case r.Status.State == engine.Configured && !r.Unread():
		it.Label, it.Description, it.Disabled = updDisable, updDescOutside, true
	}
	if why := r.Blocked(it.Label == updDisable); why != "" && !it.Disabled {
		it.Description = why
	}
	return it
}

// screen is the Auto Updates screen: the row, the section, the item.
func (r updReading) screen() screen.SubMenu {
	return screen.SubMenu{
		Title:    component.TitleBar{Title: updTitle},
		Status:   r.status,
		Sections: []screen.Section{{Header: component.SectionHeader{Title: updSection}, Rows: component.BulletList{Items: r.rows()}}},
		Heading:  updHeading,
		Menu:     component.Menu{Items: []component.MenuItem{r.item()}, Exit: updExit},
		Prompt:   updPrompt,
	}
}

type updRunner struct {
	s     *session
	uh    updHost
	h     *fakeHost
	title string
}

// openUpdates returns the action that opens what name names: the hosts'
// menu for updPrefix alone, the mock on one host for "updates/<host>".
func (s *session) openUpdates(name string) func() error {
	if uh, ok := findUpdHost(strings.TrimPrefix(name, updPrefix+"/")); ok && name != updPrefix {
		return func() error { return s.updScreen(context.Background(), uh, uh.host()) }
	}
	return func() error {
		vm := screen.SubMenu{Title: component.TitleBar{Title: "Auto Updates hosts"}, Heading: "Select a host", Prompt: updPrompt, Menu: component.Menu{Exit: updExit}}
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, uh := range updHosts() {
			it := component.MenuItem{Key: fmt.Sprint(i + 1), Label: uh.label, Description: uh.about}
			vm.Menu.Items = append(vm.Menu.Items, it)
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.updScreen(context.Background(), uh, uh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// updScreen runs the mock on h until the user goes back: its note, then
// each round the host read again.
func (s *session) updScreen(ctx context.Context, uh updHost, h *fakeHost) error {
	e := &updRunner{s: s, uh: uh, h: h, title: updPrefix + "/" + uh.name}
	if err := s.writeNote(e.title, uh.note); err != nil {
		return err
	}
	return s.p.Run(func() nav.Screen { return e.draw(ctx) })
}

// draw is the screen, its item bound.
func (e *updRunner) draw(ctx context.Context) nav.Screen {
	r, err := readUpdates(ctx, e.h.sys())
	if err != nil {
		vm := screen.SubMenu{Title: component.TitleBar{Title: updTitle}, Menu: component.Menu{Exit: updExit}, Prompt: updPrompt}
		return vm.Screen(e.s.p.Ctx, nav.Menu{Exit: updExit})
	}
	vm := r.screen()
	it := vm.Menu.Items[0]
	menu := nav.Menu{Exit: vm.Menu.Exit, Options: []nav.Option{{Item: it, Action: func() error { return e.change(ctx, r, it.Label == updDisable) }}}}
	return vm.Screen(e.s.p.Ctx, menu)
}

// preplan plans c on the host, writing nothing, and returns the plan or
// the first refusal in the screen's words.
func (e *updRunner) preplan(ctx context.Context, r updReading, c engine.Control) (*engine.Plan, string) {
	p, err := updEngine(e.h, nil, &updQuiet).Plan(ctx, []engine.Control{c}, r.Config)
	switch {
	case err != nil:
		return p, screenkit.RefusalWords(err)
	case len(p.Refused) > 0:
		return p, screenkit.RefusalWords(p.Refused[0])
	}
	return p, ""
}

// updDirection is the real control turning updates off (off) or on.
func updDirection(off bool) updates.Auto {
	if off {
		return updAuto(updates.AutoOff)
	}
	return updAuto(updates.AutoOn)
}

// refusalOf is the words the item refuses with on r's host, "" for none.
func (e *updRunner) refusalOf(ctx context.Context, r updReading, off bool) string {
	_, words := e.preplan(ctx, r, updDirection(off))
	return words
}

// notes are the review's lines beyond the change, the control's (Notes)
// as the review draws them, a warning where the control marks one.
func (e *updRunner) notes(ctx context.Context, r updReading, off bool) ([]component.KeyValue, error) {
	ns, err := updDirection(off).Notes(ctx, e.h.system(), engine.Input{Facts: r.Facts, Config: r.Config})
	var out []component.KeyValue
	for _, n := range ns {
		kv := component.KeyValue{Key: n.Key, Value: n.Value, Note: n.Note}
		if n.Warn {
			kv.State = component.ValueWarn
		}
		out = append(out, kv)
	}
	return out, err
}

// change runs the item: refused before asking in the screen's words where
// the planner refuses, else its review, its plan and y/N.
func (e *updRunner) change(ctx context.Context, r updReading, off bool) error {
	if words := e.refusalOf(ctx, r, off); words != "" {
		return e.kit(r).Say(screenkit.Warnings(words))()
	}
	c, title, question, value := updDirection(off), updEnable, "Turn automatic updates on?", "On"
	if off {
		title, question, value = updDisable, "Turn automatic updates off?", "Off"
	}
	items, err := e.notes(ctx, r, off)
	if err != nil {
		return err
	}
	review := flow.Review{Title: updSection, Items: append([]component.KeyValue{{Key: updTitle, Value: value}}, items...)}
	return e.kit(r).Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: title, Parents: []string{updTitle}}},
		Flow: flow.Flow{Review: func() flow.Review { return review }, Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: question} }},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{c}
		},
	})
}

// kit is the screen kit's session on the host, the engine's runs recorded
// for the end of play.
func (e *updRunner) kit(r updReading) screenkit.Session {
	s, h := e.s, e.h
	var rec *recorder
	cfg := r.Config
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	return screenkit.Session{
		Name: "updates mock", Prompter: s.p, System: h.system(), Config: cfg,
		DryRun: s.sequence == "dry-run", LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			rc := &recorder{host: h, f: h.f, title: e.title}
			rec = rc
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, rc)
			s.mu.Unlock()
			rc.begin()
			return updEngine(h, func(ev engine.Event) { rc.event(ev); progress(ev) }, s)
		},
		Ran: func(run output.Run) {
			rec.end()
			rec.setPlan(run.Plan)
			rec.failedWith(run.Err)
		},
	}
}

// updReadNamed reads the host named name, fresh.
func updReadNamed(ctx context.Context, name string) (updReading, *fakeHost, error) {
	uh, ok := findUpdHost(name)
	if !ok {
		return updReading{}, nil, fmt.Errorf("no updates host %q", name)
	}
	h := uh.host()
	r, err := readUpdates(ctx, h.sys())
	return r, h, err
}

// updMocks are the listing's mocks: the screen on each host, then the
// refusals.
func updMocks() []listMock {
	var mocks []listMock
	for _, uh := range updHosts() {
		mocks = append(mocks, listMock{name: "screen-" + uh.name, note: "Auto Updates. " + uh.note,
			draw: func(ctx context.Context, c component.Ctx) (string, error) {
				r, _, err := updReadNamed(ctx, uh.name)
				if err != nil {
					return "", err
				}
				return r.screen().Render(c), nil
			}})
	}
	return append(mocks, listMock{name: "refusals", note: "The refusals before asking, each from the real control's Plan: the direction, the host.", draw: updRefusalsMock})
}

// updRefusal is one refusal the refusals mock draws and the tests hold.
type updRefusal struct {
	host string
	off  bool
}

// updRefusals are the refusals, by host and direction.
func updRefusals() []updRefusal {
	return []updRefusal{
		{"timer-off", false},       // a timer off: hardn leaves the timers alone
		{"masked", false},          // apt-daily-upgrade.service masked
		{"missing", false},         // apt-daily-upgrade.service not found
		{"enable-off", false},      // APT::Periodic::Enable "0"
		{"no-origin", false},       // 50unattended-upgrades allows none
		{"extra-packages", false},  // the install would change another package
		{"not-read", false},        // apt-config fails
		{"not-read", true},         // either direction
		{"edited", true},           // hardn's file changed since
		{"edited-alpine", true},    // apk's script changed since
		{"no-record", true},        // 20auto-upgrades keeps updates on without hardn's file
		{"pve9", false},            // decision 7
		{"debian13", true},         // not hardn's
		{"by-hardn-debian", false}, // on already: an empty plan, not a refusal
		{"own-file", false},        // on by hardn with no record: an empty plan too
	}
}

// updRefusalWords is rf's words, "" for none.
func updRefusalWords(ctx context.Context, rf updRefusal) (string, error) {
	r, h, err := updReadNamed(ctx, rf.host)
	if err != nil {
		return "", err
	}
	return (&updRunner{h: h}).refusalOf(ctx, r, rf.off), nil
}

// updRefusalsMock draws each refusal under a header naming it; an empty
// plan says so.
func updRefusalsMock(ctx context.Context, c component.Ctx) (string, error) {
	var out []string
	for _, rf := range updRefusals() {
		words, err := updRefusalWords(ctx, rf)
		if err != nil {
			return "", err
		}
		dir := "on"
		if rf.off {
			dir = "off"
		}
		if words == "" {
			words = "Nothing needs changing."
		}
		out = append(out, header(c, dir+", "+rf.host), screenkit.Warnings(words).Render(c))
	}
	return apart(out...), nil
}

// updUsage is the listing's usage line.
const updUsage = "usage: go run ./internal/tools/uigallery updates [flags] [mock | all]..."

// updListing is the Auto Updates listing.
var updListing = listing{prefix: updPrefix, usage: updUsage, label: "Mock:", mocks: updMocks}

// updStart returns the hosts' menu, or the host, that args name, and
// whether they name one; an error for a host that is not one.
func updStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != updPrefix && !strings.HasPrefix(args[0], updPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], updPrefix+"/"); args[0] != updPrefix {
		if _, ok := findUpdHost(name); !ok {
			var names []string
			for _, h := range updHosts() {
				names = append(names, updPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no updates host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}

// updNote is play's note for the mock.
const updNote = "Play mode, mocks: the Auto Updates screen proposed for slice 7, drawn from what real code reads of a simulated host " +
	"(updates.Auto's Read: the check, aptconf, dpkg, systemctl, alpine.ReadUpdates and OtherJobs, hardn's record), " +
	"each change the real control's (updates.Auto, on and off) on the real plan/apply engine. " +
	"Nothing on this machine changes: every command and file is the fake's."
