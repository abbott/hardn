package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	probe "github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/packages"
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
// The gallery's stand-ins, labelled below, are unit (b)'s to build: the
// control's Plan both ways (updControl2, planAptOn, planOff), the record
// (updRecord), and which file sets each APT::Periodic value (aptSetBy).
// Alpine's on is the real alpine.PlanUpdates through steps.Alpine, the
// package install the real packages.PlanInstall. "uigallery updates" lists
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
	updDescUnread  = "Can't read"
)

// What hardn writes: its own apt file, and the record (P-43 (4)).
const (
	hardnAptFile  = aptConfDir + "/52hardn-auto-upgrades"
	hardnAptHead  = "// Written by hardn (Auto Updates).\n"
	updRecordDir  = "/var/lib/hardn/controls"
	updRecordPath = updRecordDir + "/" + updates.ID + ".json"
	keyEnable     = "APT::Periodic::Enable"
	keyUpgrade    = "APT::Periodic::Unattended-Upgrade"
	keyLists      = "APT::Periodic::Update-Package-Lists"
	upgradeTimer  = "apt-daily-upgrade.timer"
	listsTimer    = "apt-daily.timer"
	upgradeJob    = "apt-daily-upgrade.service"
	listsJob      = "apt-daily.service"
)

// hardnLines are the lines hardn's file can hold, in its order.
var hardnLines = []string{keyLists, keyUpgrade}

// hardnAptBytes is hardn's file holding the lines of keys, each "1", in
// hardnLines' order.
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

// isHardnApt is data one of the files hardn writes: with no record, off
// deletes hardn's file only while it holds such bytes (P-43 (2)).
func isHardnApt(data []byte) bool {
	for _, ks := range [][]string{{keyLists}, {keyUpgrade}, hardnLines} {
		if bytes.Equal(data, hardnAptBytes(ks)) {
			return true
		}
	}
	return false
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
			note: deb + "apt-daily-upgrade.timer disabled and stopped by the administrator. Enable refuses: hardn leaves apt's timers alone."},
		{name: "by-hardn-debian", label: "On by hardn, Debian", about: "A record", host: updByHardnDebian,
			note: deb + "debian-installer after hardn's Enable: the package, hardn's file and the record. Disable deletes the file, then purges the package."},
		{name: "by-hardn-alpine", label: "On by hardn, Alpine", about: "A record", host: updByHardnAlpine,
			note: alp + "alpine323 after hardn's Enable: apk-cron, crond at boot and running, the record. Disable stops crond, takes it out of the runlevel and removes apk-cron."},
		{name: "edited", label: "Edited since", about: "Refused, named", host: updEdited,
			note: deb + "by-hardn-debian, then hardn's file edited by hand. Disable refuses, naming it."},
		{name: "crond-shared", label: "crond shared", about: "crond kept", host: updCrondShared,
			note: alp + "logrotate's daily job under /etc/periodic, then hardn's Enable. Disable removes apk-cron and keeps crond, saying why."},
		{name: "legacy-alpine", label: "origin/main's script", about: "apk-upgrade", host: updLegacyAlpine,
			note: alp + "origin/main's /etc/periodic/daily/apk-upgrade, which crond never ran. Enable also removes it (§3.20)."},
		{name: "pve9", label: "Proxmox VE 9", about: "Not offered", host: updPVE,
			note: deb + "A Proxmox VE 9 node, with no unattended-upgrades. The row is today's check's; the item is unavailable (decision 7)."},
		{name: "not-read", label: "Not read", about: "apt-config fails", host: updNotRead,
			note: deb + "A file in apt.conf.d with a syntax error: apt-config dump fails, so the row is Unknown and both directions refuse."},
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

func updCrondShared() *fakeHost {
	h := updAlpine()
	h.f.AddFile("/etc/periodic/daily/logrotate", []byte("#!/bin/sh\n/usr/sbin/logrotate /etc/logrotate.conf\n"), 0o755, 0, 0)
	return updApplied(h)
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
	p, err := e.Plan(ctx, []engine.Control{updOn()}, cfg)
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

// updReading is the host as the screen reads it.
type updReading struct {
	f      detect.Facts
	cfg    *config.Config
	status component.StatusList
	check  engine.Status // the real check's
	apk    bool          // Alpine: apk-cron, not apt
	// apt: the effective values and which file sets each (aptSetBy), the
	// package, the timers' and services' states; aptErr where apt-config
	// is not read.
	values map[string]aptSet
	aptErr error
	pkg    detect.Tri
	units  map[string]string
	// Alpine: alpine.ReadUpdates', and the other jobs crond runs.
	u      alpine.Updates
	uErr   error
	others []string
	// The record, and hardn's own file (holding hardn's bytes).
	rec    *updRecord
	recErr error
	own    bool
	pve    bool
}

// readUpdates reads the host as the screen does, writing nothing.
func readUpdates(ctx context.Context, sys host.System) (updReading, error) {
	var r updReading
	cfg, err := statusConfig(ctx, sys)
	if err != nil {
		return r, err
	}
	r.cfg = cfg
	e := engine.New(sys, engine.Options{
		Detect: func(ctx context.Context, sys host.System) (detect.Facts, error) {
			var err error
			r.f, err = detect.Detect(ctx, sys)
			return r.f, err
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
	view := output.StatusViewOf(reports, risk.Assess(reports, probe.Read(ctx, sys, r.f))).Status
	view.Rows = slices.DeleteFunc(view.Rows, func(row component.StatusRow) bool { return row.Label != (updates.Auto{}).Title() })
	r.status = view
	r.pve = r.f.Platform.Distro == detect.DistroProxmox
	r.rec, r.recErr = readRecord(ctx, sys)
	if r.f.Platform.Distro == detect.DistroAlpine {
		r.apk = true
		r.u, r.uErr = alpine.ReadUpdates(ctx, sys, r.f)
		r.others = otherJobs(ctx, sys)
		return r, nil
	}
	r.values, r.aptErr = aptSetBy(ctx, sys)
	r.pkg, _ = detect.DpkgInstalled(ctx, sys, uuPackage)
	r.units = map[string]string{}
	for _, u := range []string{upgradeTimer, listsTimer} {
		r.units[u+" active"], _ = detect.SystemctlIsActive(ctx, sys, u)
		r.units[u+" enabled"], _ = detect.SystemctlIsEnabled(ctx, sys, u)
	}
	for _, u := range []string{upgradeJob, listsJob} {
		r.units[u+" enabled"], _ = detect.SystemctlIsEnabled(ctx, sys, u)
	}
	if data, _, err := sys.ReadFileNoFollow(ctx, hardnAptFile); err == nil {
		r.own = isHardnApt(data)
	}
	return r, nil
}

// aptSet is an APT::Periodic value and the file that sets it last.
type aptSet struct{ value, file string }

// aptSetBy is a stand-in, unit (b)'s: each scalar of apt's configuration,
// by its name in lower case, with the file that sets it last, read from
// the files as apt reads them (aptConfEntries). apt-config dump says the
// value, never the file; unit (b) reads apt.conf's own syntax (blocks,
// #clear, #include), which the model's one-line files do not exercise.
func aptSetBy(ctx context.Context, sys host.System) (map[string]aptSet, error) {
	if _, err := sys.Run(ctx, "apt-config", "dump", "APT::Periodic"); err != nil {
		return nil, err
	}
	es, err := aptConfEntries(ctx, sys)
	if err != nil {
		return nil, err
	}
	out := map[string]aptSet{}
	for _, e := range es {
		if !e.item {
			out[strings.ToLower(e.name)] = aptSet{e.value, e.file}
		}
	}
	return out, nil
}

// otherJobs are the executables under /etc/periodic/* other than
// apk-cron's script and origin/main's, and root's crontab where it is not
// the stock one: what else needs crond (decision 6).
func otherJobs(ctx context.Context, sys host.System) []string {
	var out []string
	for _, d := range []string{"15min", "hourly", "daily", "weekly", "monthly"} {
		des, _ := sys.ReadDir(ctx, "/etc/periodic/"+d)
		for _, de := range des {
			p := "/etc/periodic/" + d + "/" + de.Name()
			if p == alpine.Script || p == alpine.LegacyScript {
				continue
			}
			if fi, err := sys.Lstat(ctx, p); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
				out = append(out, de.Name())
			}
		}
	}
	if data, err := sys.ReadFile(ctx, crontabRoot); err == nil && string(data) != stockCrontab {
		out = append(out, "root's crontab")
	}
	return out
}

// on is whether the check reads updates as running (Configured or
// Partial).
func (r updReading) on() bool {
	return r.check.State == engine.Configured || r.check.State == engine.Partial
}

// byHardn is P-43 (1)'s "on by hardn": a record, or hardn's own file.
func (r updReading) byHardn() bool { return r.rec != nil || r.own }

// unread is whether a fact either direction needs is not established.
func (r updReading) unread() bool {
	if r.apk {
		return r.uErr != nil || r.u.Err != nil || r.check.State == engine.Unknown
	}
	return r.aptErr != nil || r.check.State == engine.Unknown
}

// interval says an APT::Periodic interval: unset and "0" are off.
func interval(s aptSet, set bool) (string, string) {
	switch v := s.value; {
	case !set:
		return "Off", "unset"
	case v == "0":
		return "Off", "by " + path.Base(s.file)
	case v == "1":
		return "Daily", "by " + path.Base(s.file)
	case v == "always":
		return "Each run", "by " + path.Base(s.file)
	case strings.Trim(v, "0123456789") == "":
		return "Every " + v + " days", "by " + path.Base(s.file)
	default:
		return v, "by " + path.Base(s.file)
	}
}

// timerRow is one of apt's timers: on, off, off after restart, or its
// service masked.
func (r updReading) timerRow(key, timer, job string) component.KeyValue {
	kv := component.KeyValue{Key: key, Note: timer}
	switch active, enabled, svc := r.units[timer+" active"], r.units[timer+" enabled"], r.units[job+" enabled"]; {
	case active == "" || enabled == "":
		kv.Value, kv.State = "Can't read", component.ValueUnknown
	case svc == "masked" || svc == "masked-runtime" || svc == "not-found":
		kv.Value, kv.State, kv.Note = "Masked", component.ValueBad, job
	case active != "active":
		kv.Value, kv.State = "Off", component.ValueBad
	case enabled != "enabled":
		kv.Value, kv.State = "Off after restart", component.ValueWarn
	default:
		kv.Value = "On"
	}
	return kv
}

// rows are the section: what the host has, never hardn.yml.
func (r updReading) rows() []component.KeyValue {
	var rows []component.KeyValue
	if r.apk {
		rows = r.apkRows()
	} else {
		rows = r.aptRows()
	}
	switch {
	case r.rec != nil:
		rows = append(rows, component.KeyValue{Key: updRowBy, Value: "hardn", Note: "record of " + r.rec.Entries[len(r.rec.Entries)-1].Time[:10]})
	case r.own:
		rows = append(rows, component.KeyValue{Key: updRowBy, Value: "hardn", Note: path.Base(hardnAptFile)})
	case r.recErr != nil:
		rows = append(rows, component.KeyValue{Key: updRowBy, Value: "Can't read", State: component.ValueUnknown, Note: "hardn's record"})
	case r.on():
		rows = append(rows, component.KeyValue{Key: updRowBy, Value: "Not hardn"})
	}
	return rows
}

// aptRows are Debian's, Ubuntu's and Proxmox VE's rows.
func (r updReading) aptRows() []component.KeyValue {
	if r.aptErr != nil {
		bad := func(k string) component.KeyValue {
			return component.KeyValue{Key: k, Value: "Can't read", State: component.ValueUnknown}
		}
		return []component.KeyValue{bad(updRowUpgrades), bad(updRowLists)}
	}
	pkg := component.KeyValue{Key: updRowPackage, Value: "Installed", Note: uuVersion}
	switch r.pkg {
	case detect.No:
		pkg = component.KeyValue{Key: updRowPackage, Value: "Not installed", State: component.ValueBad}
	case detect.Unknown:
		pkg = component.KeyValue{Key: updRowPackage, Value: "Can't read", State: component.ValueUnknown}
	}
	rows := []component.KeyValue{pkg}
	if s, ok := r.values[strings.ToLower(keyEnable)]; ok && strings.Trim(s.value, "0") == "" {
		rows = append(rows, component.KeyValue{Key: updRowEnable, Value: "Off", State: component.ValueBad, Note: "by " + path.Base(s.file)})
	}
	for _, kv := range []struct{ key, name string }{{updRowUpgrades, keyUpgrade}, {updRowLists, keyLists}} {
		s, set := r.values[strings.ToLower(kv.name)]
		v, note := interval(s, set)
		row := component.KeyValue{Key: kv.key, Value: v, Note: note}
		if v == "Off" {
			row.State = component.ValueBad
		}
		rows = append(rows, row)
	}
	rows = append(rows, r.timerRow(updRowUpTimer, upgradeTimer, upgradeJob), r.timerRow(updRowLsTimer, listsTimer, listsJob))
	if r.pve {
		rows = append(rows, component.KeyValue{Key: updRowPVE, Value: "Updates by hand", Note: "apt full-upgrade"})
	}
	return rows
}

// apkRows are Alpine's rows, from alpine.ReadUpdates.
func (r updReading) apkRows() []component.KeyValue {
	if r.uErr != nil {
		return []component.KeyValue{{Key: updRowApkCron, Value: "Can't read", State: component.ValueUnknown}}
	}
	u := r.u
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
	if len(r.others) > 0 {
		rows = append(rows, component.KeyValue{Key: updRowOthers, Value: strings.Join(r.others, ", "), Note: "use crond"})
	}
	return rows
}

// updExit is the sub-menu's Return.
var updExit = component.MenuItem{Key: "0", Label: "Return"}

// item is the screen's one item, named by its result (P-12): Enable while
// off, Disable while on by hardn, read-only while on but not by hardn or
// on Proxmox VE (decision 7), every description "Can't read" where a fact
// is not read.
func (r updReading) item() component.MenuItem {
	it := component.MenuItem{Key: "1", Label: updEnable, Description: updDescApt}
	switch {
	case r.apk:
		it.Description = updDescApk
	case r.pkg == detect.No:
		it.Description = updDescInstall
	}
	switch {
	case r.unread():
		it.Description = updDescUnread
	case r.pve:
		it.Description, it.Disabled = updDescPVE, true
	case r.byHardn():
		it.Label, it.Description = updDisable, updDescUndo
	case r.check.State == engine.Configured:
		it.Label, it.Description, it.Disabled = updDisable, updDescOutside, true
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

// updRecord is a stand-in, unit (b)'s: the record of P-43 (4),
// /var/lib/hardn/controls/updates.auto.json, as the gallery writes it: an
// entry per apply that turned updates on, the steps in the order they ran,
// what each changed, and the setting read back after.
type updRecord struct {
	SchemaVersion int        `json:"schemaVersion"`
	Entries       []updEntry `json:"entries"`
}

// updEntry is one apply.
type updEntry struct {
	Direction string `json:"direction"`
	Time      string `json:"time"`
	Hardn     string `json:"hardnVersion"`
	PlanID    string `json:"planId"`
	// Steps are what ran, in order: "install" (the package), "file"
	// (hardn's apt file), or alpine.Step's words.
	Steps []string `json:"steps"`
	// Packages installed, as the simulation listed them.
	Packages []string `json:"packages,omitempty"`
	// Files written or brought by a package, with their SHA-256 after.
	Files []updFile `json:"files,omitempty"`
	// Lines are the APT::Periodic lines hardn wrote.
	Lines []string `json:"lines,omitempty"`
	// ReadBack is the setting as the check read it after.
	ReadBack string `json:"readBack"`
}

// updFile is one file a step left, and its hash.
type updFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	By     string `json:"by"` // "hardn" or the package's name
}

// updRecordVersion is the record's schemaVersion.
const updRecordVersion = 1

// readRecord reads the record, nil where there is none; a record hardn
// cannot read (not JSON, a field it does not know, another version) is
// its error, which refuses off.
func readRecord(ctx context.Context, sys host.System) (*updRecord, error) {
	data, _, err := sys.ReadFileNoFollow(ctx, updRecordPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rec updRecord
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil {
		return nil, fmt.Errorf("%s: %w", updRecordPath, err)
	}
	if rec.SchemaVersion != updRecordVersion || len(rec.Entries) == 0 {
		return nil, fmt.Errorf("%s: schemaVersion %d: %w", updRecordPath, rec.SchemaVersion, detect.ErrUnrecognised)
	}
	return &rec, nil
}

// fileHash is the SHA-256 of name, "" where it is absent.
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

// updControl2 is a stand-in, unit (b)'s: the real Auto Updates control's
// id, title and check (updates.Auto, CheckOnly until this slice), its Plan
// one direction's.
type updControl2 struct {
	engine.Control
	plan func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error)
}

// Plan plans the direction, reading the host where the engine plans.
func (c updControl2) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	return c.plan(ctx, sys, in)
}

// updOn is the control turning updates on.
func updOn() engine.Control {
	return updControl2{Control: updates.Auto{}, plan: func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		if in.Facts.Platform.Distro == detect.DistroAlpine {
			return planApkOn(ctx, sys, in)
		}
		return planAptOn(ctx, sys, in)
	}}
}

// updOff is the control turning off what hardn turned on.
func updOff() engine.Control {
	return updControl2{Control: updates.Auto{}, plan: func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		cs, _, err := planOff(ctx, sys, in)
		return cs, err
	}}
}

// The refusals' codes (stand-ins, unit (b)'s to register in pkg/output).
const (
	codeUpdUnread    = "UPDATES_NOT_READ"
	codeUpdEnableOff = "UPDATES_PERIODIC_OFF"
	codeUpdTimerOff  = "UPDATES_TIMER_OFF"
	codeUpdMasked    = "UPDATES_JOB_MASKED"
	codeUpdOrigins   = "UPDATES_NO_ORIGINS"
	codeUpdPVE       = "UPDATES_PROXMOX"
	codeUpdChanged   = "UPDATES_CHANGED_SINCE"
	codeUpdNothing   = "UPDATES_NOTHING_TO_UNDO"
	codeUpdRecord    = "UPDATES_RECORD_UNREADABLE"
	codeUpdPackages  = "UPDATES_OTHER_PACKAGES"
	codeUpdReadBack  = "UPDATES_NOT_IN_EFFECT"
)

// unreadRefusal is either direction refused for a fact not read.
func unreadRefusal(ctx context.Context, sys host.System, f detect.Facts) error {
	st, err := (updates.Auto{}).Check(ctx, sys, f)
	if err == nil && st.State != engine.Unknown {
		return nil
	}
	why := "hardn can't read how updates run here"
	if res, rerr := sys.Run(ctx, "apt-config", "dump", "APT::Periodic"); f.Platform.Distro != detect.DistroAlpine && rerr != nil {
		why = "apt can't read its settings: " + strings.TrimPrefix(firstLine(string(res.Stderr)), "E: ")
	}
	return &steps.Refusal{Code: codeUpdUnread, Why: why, Fix: "fix that, then try again", Err: errors.Join(err, detect.ErrUnrecognised)}
}

// firstLine is s to its first line break.
func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// planAptOn is a stand-in, unit (b)'s: on for Debian and Ubuntu (P-43 (3),
// decision 3): unattended-upgrades installed when missing
// (packages.PlanInstall), then hardn's own 52hardn-auto-upgrades with the
// lines whose effective value is "0" or unset, read back with apt-config
// dump; never debconf, 20auto-upgrades or the timers, so Enable "0", a
// timer off, its job masked and no origin refuse, with words. Proxmox VE
// refuses (decision 7). The record is the last change.
func planAptOn(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	if in.Facts.Platform.Distro == detect.DistroProxmox {
		return nil, &steps.Refusal{Code: codeUpdPVE, Why: "Proxmox VE is updated with apt full-upgrade, and unattended-upgrades wouldn't take its packages", Fix: "update it by hand", Err: detect.ErrUnrecognised}
	}
	if err := unreadRefusal(ctx, sys, in.Facts); err != nil {
		return nil, err
	}
	vals, err := aptSetBy(ctx, sys)
	if err != nil {
		return nil, err
	}
	if s, ok := vals[strings.ToLower(keyEnable)]; ok && strings.Trim(s.value, "0") == "" {
		return nil, &steps.Refusal{Code: codeUpdEnableOff, Why: fmt.Sprintf("%s \"0\", in %s, turns apt's daily jobs off", keyEnable, s.file), Fix: "remove that line, then try again", Err: detect.ErrUnrecognised}
	}
	for _, t := range []struct{ timer, job string }{{upgradeTimer, upgradeJob}, {listsTimer, listsJob}} {
		svc, _ := detect.SystemctlIsEnabled(ctx, sys, t.job)
		if svc == "masked" || svc == "masked-runtime" || svc == "not-found" {
			return nil, &steps.Refusal{Code: codeUpdMasked, Why: t.job + " is masked, and hardn leaves apt's timers to you", Fix: "run systemctl unmask " + t.job + ", then try again", Err: detect.ErrUnrecognised}
		}
		active, _ := detect.SystemctlIsActive(ctx, sys, t.timer)
		enabled, _ := detect.SystemctlIsEnabled(ctx, sys, t.timer)
		if active != "active" || enabled != "enabled" {
			return nil, &steps.Refusal{Code: codeUpdTimerOff, Why: t.timer + " is off, and hardn leaves apt's timers to you", Fix: "run systemctl enable --now " + t.timer + ", then try again", Err: detect.ErrUnrecognised}
		}
	}
	var cs []engine.Change
	var recSteps, adds []string
	pkg, err := packages.Installed(ctx, sys, packages.Apt, uuPackage)
	if err != nil {
		return nil, err
	}
	if pkg == detect.No {
		p, err := packages.PlanInstall(ctx, sys, packages.Apt, uuPackage)
		if err != nil {
			return nil, installWords(err)
		}
		adds = p.Adds
		after := "installed"
		if also := slices.DeleteFunc(slices.Clone(p.Adds), func(n string) bool { return n == uuPackage }); len(also) > 0 {
			after += ", with " + strings.Join(also, ", ")
		}
		cs = append(cs, engine.Change{ID: updates.ID + "/install", Summary: "Install unattended-upgrades", Title: "Install unattended-upgrades", Target: uuPackage,
			StateBefore: "not installed", StateAfter: after, Commands: []string{host.FormatArgv(p.Argv)},
			Risk: engine.RiskPackageInstall, Stage: engine.StagePackages, Resources: []string{updates.ID},
			// The postinst's files, absent before: a rollback removes them
			// with the package, as apt-get remove would not.
			Backups:  []string{autoUpgrades, uuConfig},
			Rollback: engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: slices.Clone(p.Undo), Why: updates.ID}}},
			Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
				rep, err := packages.Execute(ctx, sys, p)
				u := engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: slices.Clone(rep.Undo), Why: updates.ID}}}
				if len(rep.Undo) == 0 {
					u = engine.Undo{}
				}
				return engine.Ran{Rollback: &u, Detail: "unattended-upgrades: installed"}, err
			}})
		recSteps = append(recSteps, "install")
	} else if st, err := (updates.Auto{}).Check(ctx, sys, in.Facts); err == nil && st.Detail == updates.DetailNoOrigins {
		return nil, &steps.Refusal{Code: codeUpdOrigins, Why: path.Base(uuConfig) + " allows no origin, so nothing would be upgraded", Fix: "add your distribution's origins there, then try again", Err: detect.ErrUnrecognised}
	}
	var keys []string
	for _, k := range hardnLines {
		if s, set := vals[strings.ToLower(k)]; !set || s.value == "0" {
			keys = append(keys, k)
		}
	}
	if len(keys) > 0 {
		data := hardnAptBytes(keys)
		after := string(data)
		var lines []string
		for _, k := range keys {
			lines = append(lines, k+" \"1\"")
		}
		cs = append(cs, engine.Change{ID: updates.ID + "/file", Summary: "Turn on daily upgrades in " + hardnAptFile, Title: "Turn on daily upgrades", Target: hardnAptFile,
			Diff: engine.FileDiff(hardnAptFile, nil, &after), Commands: []string{"apt-config dump APT::Periodic"},
			Stage: engine.StageFiles, Resources: []string{updates.ID}, Backups: []string{hardnAptFile}, After: changeIDs(cs),
			Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
				if err := sys.WriteFileAtomic(ctx, hardnAptFile, data, 0o644, 0, 0); err != nil {
					return engine.Ran{}, err
				}
				return engine.Ran{Detail: "apt: " + strings.Join(lines, ", ") + " in effect"}, readBack(ctx, sys, keys, "1")
			}})
		recSteps = append(recSteps, "file")
	}
	if len(cs) == 0 {
		return nil, nil
	}
	return append(cs, recordOn(in.Facts, recSteps, adds, keys, cs)), nil
}

// changeIDs are the ids of cs.
func changeIDs(cs []engine.Change) []string {
	var ids []string
	for _, c := range cs {
		ids = append(ids, c.ID)
	}
	return ids
}

// readBack checks that each of keys reads want in apt-config dump, as apt
// reads it (APT_CONFIG not modelled); a mismatch is the change failed.
func readBack(ctx context.Context, sys host.System, keys []string, want string) error {
	res, err := sys.Run(ctx, "apt-config", "dump", "APT::Periodic")
	if err != nil {
		return err
	}
	got := map[string]string{}
	for _, l := range strings.Split(string(res.Stdout), "\n") {
		if n, rest, ok := strings.Cut(l, ` "`); ok {
			got[strings.ToLower(n)] = strings.TrimSuffix(rest, `";`)
		}
	}
	for _, k := range keys {
		if v := got[strings.ToLower(k)]; v != want {
			return &steps.Refusal{Code: codeUpdReadBack, Why: fmt.Sprintf("apt reads %s as %q after the change, not %q: a file read later sets it", k, v, want), Fix: "find that file, then try again", Err: detect.ErrUnrecognised}
		}
	}
	return nil
}

// installWords are packages.PlanInstall's refusals in the screen's words.
func installWords(err error) error {
	var other *packages.OtherPackagesError
	switch {
	case errors.Is(err, packages.ErrNotInRepos):
		return &steps.Refusal{Code: "UPDATES_NOT_IN_REPOS", Why: "hardn can't find unattended-upgrades in apt's package lists", Fix: "run apt-get update, then try again", Err: err}
	case errors.As(err, &other):
		return &steps.Refusal{Code: codeUpdPackages, Why: "Installing unattended-upgrades would also change other packages: " + strings.Join(other.Changes, ", "), Fix: "install it by hand, then try again", Err: err}
	}
	return err
}

// planApkOn is Alpine's on as built: alpine.ReadUpdates and PlanUpdates,
// through steps.Alpine, then the record.
func planApkOn(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	u, err := alpine.ReadUpdates(ctx, sys, in.Facts)
	if err != nil {
		return nil, &steps.Refusal{Code: codeUpdUnread, Why: "hardn can't read how updates run here: " + firstLine(err.Error()), Fix: "fix that, then try again", Err: err}
	}
	p, err := alpine.PlanUpdates(u)
	if err != nil {
		return nil, &steps.Refusal{Code: codeUpdUnread, Why: "hardn can't plan this: " + firstLine(err.Error()), Fix: "fix that, then try again", Err: err}
	}
	if len(p.Changes) == 0 {
		return nil, nil
	}
	c, err := steps.Alpine(updates.ID+"/apk", p, "Turn on daily upgrades")
	if err != nil {
		return nil, err
	}
	c.Title, c.StateBefore, c.StateAfter = "Turn on daily upgrades", "apk-cron "+map[detect.Tri]string{detect.Yes: "installed", detect.No: "not installed"}[u.Package]+", crond "+u.Crond.String(), "apk-cron installed, crond started at boot"
	var recSteps []string
	for _, ch := range p.Changes {
		recSteps = append(recSteps, ch.Step.String())
	}
	return []engine.Change{c, recordOn(in.Facts, recSteps, nil, nil, []engine.Change{c})}, nil
}

// recordOn is the record's change: written last, after the read-back,
// backed up and journalled as any write (P-43 (4)), so a rollback of the
// apply puts the record back as it was, or removes it.
func recordOn(f detect.Facts, recSteps, adds, keys []string, before []engine.Change) engine.Change {
	return engine.Change{ID: updates.ID + "/record", Summary: "Record what hardn changed in " + updRecordPath, Title: "Record hardn's changes", Target: updRecordPath,
		Stage: engine.StageVerify, Resources: []string{updates.ID}, Backups: []string{updRecordPath}, After: changeIDs(before),
		Apply: func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
			st, err := (updates.Auto{}).Check(ctx, sys, f)
			if err != nil {
				return engine.Ran{}, err
			}
			e := updEntry{Direction: "on", Time: clock().Format("2006-01-02T15:04:05Z07:00"), Hardn: "v0.4.0", PlanID: run.ID,
				Steps: recSteps, Packages: adds, ReadBack: st.State.String() + ": " + st.Detail}
			for _, k := range keys {
				e.Lines = append(e.Lines, k+" \"1\"")
			}
			files := map[string]string{}
			if slices.Contains(recSteps, "file") {
				files[hardnAptFile] = "hardn"
			}
			if slices.Contains(recSteps, "install") {
				files[autoUpgrades], files[uuConfig] = uuPackage, uuPackage
			}
			if f.Platform.Distro == detect.DistroAlpine && slices.Contains(recSteps, alpine.StepInstall.String()) {
				files[alpine.Script] = alpine.Package
			}
			for _, p := range slices.Sorted(maps.Keys(files)) {
				sum, err := fileHash(ctx, sys, p)
				if err != nil {
					return engine.Ran{}, err
				}
				e.Files = append(e.Files, updFile{Path: p, SHA256: sum, By: files[p]})
			}
			rec, err := readRecord(ctx, sys)
			if err != nil {
				return engine.Ran{}, err
			}
			if rec == nil {
				rec = &updRecord{SchemaVersion: updRecordVersion}
			}
			rec.Entries = append(rec.Entries, e)
			data, err := json.MarshalIndent(rec, "", "  ")
			if err != nil {
				return engine.Ran{}, err
			}
			if err := sys.MkdirAll(ctx, updRecordDir, 0o700); err != nil {
				return engine.Ran{}, err
			}
			return engine.Ran{Detail: "record: written"}, sys.WriteFileAtomic(ctx, updRecordPath, append(data, '\n'), 0o600, 0, 0)
		}}
}

// recordedHash is the hash the record holds for name, and whether it holds
// one.
func (rec *updRecord) recordedHash(name string) (string, bool) {
	for i := len(rec.Entries) - 1; i >= 0; i-- {
		for _, f := range rec.Entries[i].Files {
			if f.Path == name {
				return f.SHA256, true
			}
		}
	}
	return "", false
}

// changedSince is a refusal naming a file changed since hardn left it.
func changedSince(name, since string) error {
	return &steps.Refusal{Code: codeUpdChanged, Why: fmt.Sprintf("Not undone: %s changed since hardn %s it", name, since), Fix: "put it back as it was or undo the rest by hand", Err: detect.ErrUnrecognised}
}

// runChange is a change that runs argv, undone by undo, after the ids
// given.
func runChange(id, title, target string, argv, undo []string, risk engine.RiskClass, after []string) engine.Change {
	return engine.Change{ID: id, Summary: title, Title: title, Target: target, Commands: []string{host.FormatArgv(argv)},
		Risk: risk, Stage: engine.StageFiles, Resources: []string{updates.ID}, After: after,
		Rollback: engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: undo, Why: updates.ID}}},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			_, err := sys.Run(ctx, argv[0], argv[1:]...)
			return engine.Ran{Detail: title}, err
		}}
}

// planOff is a stand-in, unit (b)'s: off, P-43 (2) and (3): the inverse of
// each recorded step, last first, each run only while the host is as
// hardn left it, a step changed since refusing, naming the file; with no
// record, hardn's own file deleted while it holds hardn's bytes, and
// nothing else. Debian: the file deleted and read back off, then the
// package hardn installed purged while its files hold the recorded bytes,
// else removed (decision 5). Alpine: crond stopped and out of the runlevel
// only when hardn put it there and nothing else needs it (decision 6),
// else kept and said; apk-cron removed behind apk del --simulate, refused
// while its script differs from the record; origin/main's apk-upgrade
// never written back. kept are what is left and why, for the review.
func planOff(ctx context.Context, sys host.System, in engine.Input) (cs []engine.Change, kept []component.KeyValue, err error) {
	f := in.Facts
	if f.Platform.Distro == detect.DistroProxmox {
		return nil, nil, &steps.Refusal{Code: codeUpdPVE, Why: "hardn doesn't change updates on Proxmox VE", Fix: "change them by hand", Err: detect.ErrUnrecognised}
	}
	if err := unreadRefusal(ctx, sys, f); err != nil {
		return nil, nil, err
	}
	rec, err := readRecord(ctx, sys)
	if err != nil {
		return nil, nil, &steps.Refusal{Code: codeUpdRecord, Why: "hardn can't read its record of what it changed (" + updRecordPath + ")", Fix: "undo it by hand", Err: err}
	}
	var stepsDone []string
	if rec != nil {
		for _, e := range rec.Entries {
			stepsDone = append(stepsDone, e.Steps...)
		}
	}
	add := func(c engine.Change) { c.After = changeIDs(cs); cs = append(cs, c) }
	if f.Platform.Distro == detect.DistroAlpine {
		if err := planApkOff(ctx, sys, f, rec, stepsDone, add, &kept); err != nil {
			return nil, nil, err
		}
	} else if err := planAptOff(ctx, sys, rec, stepsDone, add, &kept); err != nil {
		return nil, nil, err
	}
	if len(cs) == 0 && rec == nil {
		return nil, nil, &steps.Refusal{Code: codeUpdNothing, Why: "Nothing to undo: hardn did not turn this on", Fix: "turn it off by hand", Err: detect.ErrUnrecognised}
	}
	if rec != nil {
		cs = append(cs, engine.Change{ID: updates.ID + "/record", Summary: "Delete hardn's record " + updRecordPath, Title: "Delete hardn's record", Target: updRecordPath,
			Stage: engine.StageVerify, Resources: []string{updates.ID}, Backups: []string{updRecordPath}, After: changeIDs(cs),
			Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
				return engine.Ran{Detail: "record: deleted"}, sys.Remove(ctx, updRecordPath)
			}})
	}
	return cs, kept, nil
}

// planAptOff is Debian's and Ubuntu's off.
func planAptOff(ctx context.Context, sys host.System, rec *updRecord, done []string, add func(engine.Change), kept *[]component.KeyValue) error {
	data, _, err := sys.ReadFileNoFollow(ctx, hardnAptFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		sum := sha256.Sum256(data)
		want, ok := "", false
		if rec != nil {
			want, ok = rec.recordedHash(hardnAptFile)
		}
		if ok && want != hex.EncodeToString(sum[:]) || !ok && !isHardnApt(data) {
			return changedSince(hardnAptFile, "wrote")
		}
		before := string(data)
		add(engine.Change{ID: updates.ID + "/file", Summary: "Turn off daily upgrades: delete " + hardnAptFile, Title: "Turn off daily upgrades", Target: hardnAptFile,
			Diff: engine.FileDiff(hardnAptFile, &before, nil), Stage: engine.StageFiles, Resources: []string{updates.ID}, Backups: []string{hardnAptFile},
			Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
				if err := sys.Remove(ctx, hardnAptFile); err != nil {
					return engine.Ran{}, err
				}
				return engine.Ran{Detail: "apt: hardn's lines gone"}, nil
			}})
	}
	if !slices.Contains(done, "install") {
		return nil
	}
	if t, err := packages.Installed(ctx, sys, packages.Apt, uuPackage); err != nil || t != detect.Yes {
		return err
	}
	purge := true
	for _, p := range []string{autoUpgrades, uuConfig} {
		want, _ := rec.recordedHash(p)
		if got, err := fileHash(ctx, sys, p); err != nil || got != want {
			purge = false
			*kept = append(*kept, component.KeyValue{Key: path.Base(p), Value: "Kept", State: component.ValueWarn, Note: "changed since"})
		}
	}
	verb := "purge"
	if !purge {
		verb = "remove"
	}
	res, err := sys.Run(ctx, "apt-get", verb, "-s", "-y", uuPackage)
	if err != nil {
		return err
	}
	var other []string
	for _, l := range strings.Split(string(res.Stdout), "\n") {
		if f := strings.Fields(l); len(f) > 1 && (f[0] == "Remv" || f[0] == "Purg" || f[0] == "Inst") && f[1] != uuPackage {
			other = append(other, strings.Join(f[:2], " "))
		}
	}
	if len(other) > 0 {
		return &steps.Refusal{Code: codeUpdPackages, Why: "Removing unattended-upgrades would also change other packages: " + strings.Join(other, ", "), Fix: "remove it by hand", Err: detect.ErrUnrecognised}
	}
	c := runChange(updates.ID+"/package", "Remove unattended-upgrades", uuPackage, []string{"apt-get", verb, "-y", uuPackage}, []string{"apt-get", "install", "-y", uuPackage}, engine.RiskPackageInstall, nil)
	c.StateBefore, c.StateAfter = "installed", map[bool]string{true: "purged, with 20auto-upgrades", false: "removed, its files kept"}[purge]
	if purge {
		c.Backups = []string{autoUpgrades, uuConfig}
	}
	add(c)
	return nil
}

// planApkOff is Alpine's off.
func planApkOff(ctx context.Context, sys host.System, f detect.Facts, rec *updRecord, done []string, add func(engine.Change), kept *[]component.KeyValue) error {
	if rec == nil {
		return nil
	}
	u, err := alpine.ReadUpdates(ctx, sys, f)
	if err != nil {
		return err
	}
	if slices.Contains(done, alpine.StepRemoveLegacy.String()) {
		*kept = append(*kept, component.KeyValue{Key: updRowLegacy, Value: "Stays removed", Note: path.Base(alpine.LegacyScript)})
	}
	others := otherJobs(ctx, sys)
	started := slices.Contains(done, alpine.StepStart.String()) || slices.Contains(done, alpine.StepRestart.String())
	booted := slices.Contains(done, alpine.StepRunlevel.String())
	if (started || booted) && len(others) > 0 {
		*kept = append(*kept, component.KeyValue{Key: updRowCrond, Value: "Kept", Note: strings.Join(others, ", ") + " needs it"})
		started, booted = false, false
	}
	if started && u.Crond == alpine.CrondStarted {
		add(runChange(updates.ID+"/crond-stop", "Stop crond", alpine.Service, []string{"rc-service", alpine.Service, "stop"}, []string{"rc-service", alpine.Service, "start"}, engine.RiskServiceRestart, nil))
	}
	if booted && u.Runlevel == detect.Yes {
		add(runChange(updates.ID+"/crond-boot", "Stop crond at boot", alpine.Service, []string{"rc-update", "del", alpine.Service, alpine.Runlevel}, []string{"rc-update", "add", alpine.Service, alpine.Runlevel}, engine.RiskPlain, nil))
	}
	if !slices.Contains(done, alpine.StepInstall.String()) || u.Package != detect.Yes {
		return nil
	}
	want, _ := rec.recordedHash(alpine.Script)
	if got, err := fileHash(ctx, sys, alpine.Script); err != nil || got != want {
		return changedSince(alpine.Script, "installed")
	}
	res, err := sys.Run(ctx, "apk", "del", "--simulate", alpine.Package)
	if err != nil {
		return err
	}
	if cs := alpine.Changes(res.Combined); len(cs) != 1 || !strings.HasPrefix(cs[0], "Purging "+alpine.Package+" (") {
		return &steps.Refusal{Code: codeUpdPackages, Why: "Removing apk-cron would also change other packages: " + strings.Join(cs, ", "), Fix: "remove it by hand", Err: detect.ErrUnrecognised}
	}
	add(runChange(updates.ID+"/apk", "Remove apk-cron", alpine.Package, []string{"apk", "del", alpine.Package}, []string{"apk", "add", alpine.Package}, engine.RiskPackageInstall, nil))
	return nil
}

// updRunner runs the mock on one host.
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
	p, err := updEngine(e.h, nil, &updQuiet).Plan(ctx, []engine.Control{c}, r.cfg)
	switch {
	case err != nil:
		return p, screenkit.RefusalWords(err)
	case len(p.Refused) > 0:
		return p, screenkit.RefusalWords(p.Refused[0])
	}
	return p, ""
}

// refusalOf is the words the item refuses with on r's host, "" for none.
func (e *updRunner) refusalOf(ctx context.Context, r updReading, off bool) string {
	c := updOn()
	if off {
		c = updOff()
	}
	_, words := e.preplan(ctx, r, c)
	return words
}

// change runs the item: refused before asking in the screen's words where
// the planner refuses, else its review, its plan and y/N.
func (e *updRunner) change(ctx context.Context, r updReading, off bool) error {
	if words := e.refusalOf(ctx, r, off); words != "" {
		return e.kit(r).Say(screenkit.Warnings(words))()
	}
	c, title, question, value := updOn(), updEnable, "Turn automatic updates on?", "On"
	items := []component.KeyValue{}
	if off {
		c, title, question, value = updOff(), updDisable, "Turn automatic updates off?", "Off"
		_, items, _ = planOff(ctx, e.h.system(), engine.Input{Facts: r.f})
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
	cfg := r.cfg
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
	return append(mocks, listMock{name: "refusals", note: "The refusals before asking, each from the stand-in planner on the real reading: the direction, the host.", draw: updRefusalsMock})
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
		{"enable-off", false},      // APT::Periodic::Enable "0"
		{"not-read", false},        // apt-config fails
		{"not-read", true},         // either direction
		{"edited", true},           // hardn's file changed since
		{"edited-alpine", true},    // apk's script changed since
		{"pve9", false},            // decision 7
		{"debian13", true},         // not hardn's
		{"by-hardn-debian", false}, // on already: an empty plan, not a refusal
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
	"(the Auto Updates check, apt-config, dpkg, systemctl, alpine.ReadUpdates), each change on the real plan/apply engine: " +
	"alpine.PlanUpdates and packages.PlanInstall as built, the rest of each direction and the record the gallery's stand-ins, unit (b)'s to build. " +
	"Nothing on this machine changes: every command and file is the fake's."
