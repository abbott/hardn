package updates

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	updatesctl "github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dnfauto"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The rows' words, the control's reading mapped to what pkg/ui draws:
// decided (gallery-updates.md §5).
const (
	cantRead     = "Can't read"
	installed    = "Installed"
	notInstalled = "Not installed"
	off          = "Off"
	on           = "On"
	daily        = "Daily"
	eachRun      = "Each run"
	everyDays    = "Every %s days"
	offRestart   = "Off after restart"
	masked       = "Masked"
	missing      = "Missing"
	inPlace      = "In place"
	notRun       = "Not run"
	running      = "Running"
	stopped      = "Stopped"
	originMain   = "origin/main's"
	byHand       = "Updates by hand"
	hardn        = "hardn"
	notHardn     = "Not hardn"
	// Rocky Linux and AlmaLinux (proposed, el-updates.md §1.4)
	securityOnly = "Security only"
	allUpdates   = "All updates"

	noteBy        = "by "
	noteUnset     = "unset"
	noteChanged   = "changed since hardn"
	noteBoot      = "starts at boot"
	noteNoBoot    = "not at boot"
	noteUseCrond  = "use crond"
	noteFullUp    = "apt full-upgrade"
	noteRecordOf  = "record of "
	noteRecord    = "hardn's record"
	keyEnable     = "APT::Periodic::Enable"
	keyUpgrade    = "APT::Periodic::Unattended-Upgrade"
	keyLists      = "APT::Periodic::Update-Package-Lists"
	maskedRuntime = "masked-runtime"
)

// statusRow is the Auto Updates row, the main menu's: the control's check
// (Reading.Status), through the main menu's own mapping and its assessment
// over the header the main menu reads (hd: an Unknown row's "Counted as
// not configured", the uptime's line under a host that does not upgrade
// itself); then decision 9 (a)'s line, the control's finding, which
// output.StatusViewOf does not draw (it draws the risk factors). A reading
// cut short is Unknown.
func statusRow(h reading, hd header.Header) component.StatusList {
	rp := engine.Report{Control: updatesctl.ID, Title: updatesctl.Auto{}.Title(), Status: h.Status, Err: h.StatusErr}
	if h.err != nil && rp.Err == nil {
		rp.Err = h.err
	}
	if rp.Err != nil {
		rp.Status.State = engine.Unknown
	}
	reports := []engine.Report{rp}
	view := output.StatusViewOf(reports, risk.Assess(reports, hd)).Status
	view.Rows = slices.DeleteFunc(view.Rows, func(row component.StatusRow) bool { return row.Label != rp.Title })
	if len(view.Rows) == 1 {
		for _, fd := range h.Status.Findings {
			if fd.Code == updatesctl.CodeChangedSince {
				view.Rows[0].Lines = append(view.Rows[0].Lines, component.StatusLine{Text: fd.Detail, Warn: true})
			}
		}
	}
	return view
}

// mainView is h as the screen draws it: the item Enable or Disable by
// ByHardn, On and Unread, as the mock's; Unavailable on Proxmox VE and
// while on not by hardn; its description Blocked where something is in
// the way (R1).
func mainView(h reading) screen.Updates {
	v := screen.Updates{Status: statusRow(h, h.header), Rows: sectionRows(h), Apk: h.Apk, Dnf: h.Dnf, Install: !h.Apk && h.Package == detect.No}
	switch {
	case h.PVE:
		v.Item = screen.UpdatesPVE
	case h.ByHardn():
		v.Item = screen.UpdatesDisable
	case h.Status.State == engine.Configured && !h.Unread():
		v.Item = screen.UpdatesOutside
	}
	v.Blocked = h.Blocked(v.Off())
	if h.err != nil {
		v.Blocked = cantRead
	}
	return v
}

// sectionRows are the Update Configuration section: what the host has,
// never hardn.yml; Can't read where the reading was cut short.
func sectionRows(h reading) []component.KeyValue {
	if h.err != nil {
		key := screen.UpdatesUpgradesKey
		if h.Dnf {
			key = screen.UpdatesTypeKey
		}
		return []component.KeyValue{{Key: key, Value: cantRead, State: component.ValueUnknown}}
	}
	var rows []component.KeyValue
	switch {
	case h.Apk:
		rows = apkRows(h)
	case h.Dnf:
		rows = dnfRows(h)
	default:
		rows = aptRows(h)
	}
	switch {
	case h.Record != nil && len(h.Record.Entries) > 0:
		rows = append(rows, component.KeyValue{Key: screen.UpdatesByKey, Value: hardn, Note: noteRecordOf + h.Record.Entries[len(h.Record.Entries)-1].Time.Format(time.DateOnly)})
	case h.Record != nil, h.Own:
		rows = append(rows, component.KeyValue{Key: screen.UpdatesByKey, Value: hardn, Note: path.Base(updatesctl.HardnFile)})
	case h.RecordErr != nil:
		rows = append(rows, component.KeyValue{Key: screen.UpdatesByKey, Value: cantRead, State: component.ValueUnknown, Note: noteRecord})
	case h.On():
		rows = append(rows, component.KeyValue{Key: screen.UpdatesByKey, Value: notHardn})
	}
	return rows
}

// interval says an APT::Periodic interval: unset and "0" are off.
func interval(v, file string, set bool) (string, string) {
	by := noteBy + path.Base(file)
	switch {
	case !set:
		return off, noteUnset
	case v == "0":
		return off, by
	case v == "1":
		return daily, by
	case v == "always":
		return eachRun, by
	case strings.Trim(v, "0123456789") == "":
		return fmt.Sprintf(everyDays, v), by
	default:
		return v, by
	}
}

// timerRow is one of apt's timers: on, off, off after restart, or its
// service masked or missing.
func timerRow(key string, t updatesctl.Timer) component.KeyValue {
	kv := component.KeyValue{Key: key, Note: t.Timer}
	switch {
	case t.Active == "" || t.Enabled == "":
		kv.Value, kv.State = cantRead, component.ValueUnknown
	case t.ServiceEnabled == "not-found":
		kv.Value, kv.State, kv.Note = missing, component.ValueBad, t.Service
	case t.ServiceEnabled == "masked" || t.ServiceEnabled == maskedRuntime:
		kv.Value, kv.State, kv.Note = masked, component.ValueBad, t.Service
	case t.Active != "active":
		kv.Value, kv.State = off, component.ValueBad
	case t.Enabled != "enabled":
		kv.Value, kv.State = offRestart, component.ValueWarn
	default:
		kv.Value = on
	}
	return kv
}

// aptRows are Debian's, Ubuntu's and Proxmox VE's rows: the package, each
// APT::Periodic value and the file that sets it (aptconf), the timers.
func aptRows(h reading) []component.KeyValue {
	if h.AptErr != nil {
		return []component.KeyValue{{Key: screen.UpdatesUpgradesKey, Value: cantRead, State: component.ValueUnknown},
			{Key: screen.UpdatesListsKey, Value: cantRead, State: component.ValueUnknown}}
	}
	pkg := component.KeyValue{Key: screen.UpdatesPackageKey, Value: installed}
	switch h.Package {
	case detect.No:
		pkg.Value, pkg.State = notInstalled, component.ValueBad
	case detect.Unknown:
		pkg.Value, pkg.State = cantRead, component.ValueUnknown
	}
	rows := []component.KeyValue{pkg}
	if v, file, ok := h.Apt.Value(keyEnable); ok && v != "" && strings.Trim(v, "0") == "" {
		rows = append(rows, component.KeyValue{Key: screen.UpdatesPeriodicKey, Value: off, State: component.ValueBad, Note: noteBy + path.Base(file)})
	}
	for _, kv := range []struct{ key, name string }{{screen.UpdatesUpgradesKey, keyUpgrade}, {screen.UpdatesListsKey, keyLists}} {
		v, file, set := h.Apt.Value(kv.name)
		val, note := interval(v, file, set)
		row := component.KeyValue{Key: kv.key, Value: val, Note: note}
		if val == off {
			row.State = component.ValueBad
		}
		if set && h.Changed != "" && file == h.Changed {
			row.Note, row.State = noteChanged, component.ValueWarn // the item names the file
		}
		rows = append(rows, row)
	}
	rows = append(rows, timerRow(screen.UpdatesUpTimerKey, h.Upgrade), timerRow(screen.UpdatesLsTimerKey, h.Lists))
	if h.PVE {
		rows = append(rows, component.KeyValue{Key: screen.UpdatesPVEKey, Value: byHand, Note: noteFullUp})
	}
	return rows
}

// tri says t: yes, no, or Can't read.
func tri(t detect.Tri, yes, no string) (string, component.ValueState) {
	switch t {
	case detect.Yes:
		return yes, component.ValuePlain
	case detect.No:
		return no, component.ValueBad
	}
	return cantRead, component.ValueUnknown
}

// apkRows are Alpine's rows, from alpine.ReadUpdates and alpine.OtherJobs.
func apkRows(h reading) []component.KeyValue {
	if h.UpdatesErr != nil {
		return []component.KeyValue{{Key: screen.UpdatesApkCronKey, Value: cantRead, State: component.ValueUnknown}}
	}
	u := h.Updates
	pv, ps := tri(u.Package, installed, notInstalled)
	rows := []component.KeyValue{{Key: screen.UpdatesApkCronKey, Value: pv, State: ps}}
	switch u.Script {
	case alpine.ScriptInstalled:
		rows = append(rows, component.KeyValue{Key: screen.UpdatesScriptKey, Value: inPlace, Note: alpine.Script})
	case alpine.ScriptOther:
		rows = append(rows, component.KeyValue{Key: screen.UpdatesScriptKey, Value: notRun, State: component.ValueBad, Note: alpine.Script})
	}
	crond := component.KeyValue{Key: screen.UpdatesCrondKey}
	switch u.Crond {
	case alpine.CrondStarted:
		crond.Value = running
	case alpine.CrondStopped, alpine.CrondCrashed:
		crond.Value, crond.State = stopped, component.ValueBad
	case alpine.CrondMissing:
		crond.Value, crond.State = notInstalled, component.ValueBad
	default:
		crond.Value, crond.State = cantRead, component.ValueUnknown
	}
	switch u.Runlevel {
	case detect.Yes:
		crond.Note = noteBoot
	case detect.No:
		crond.Note = noteNoBoot
	}
	rows = append(rows, crond)
	if u.Legacy == alpine.LegacyHardn {
		rows = append(rows, component.KeyValue{Key: screen.UpdatesLegacyKey, Value: originMain, State: component.ValueWarn, Note: path.Base(alpine.LegacyScript)})
	}
	switch {
	case h.JobsErr != nil:
		rows = append(rows, component.KeyValue{Key: screen.UpdatesOthersKey, Value: cantRead, State: component.ValueUnknown})
	case len(h.Jobs) > 0:
		var names []string
		for _, j := range h.Jobs {
			names = append(names, j.Name)
		}
		rows = append(rows, component.KeyValue{Key: screen.UpdatesOthersKey, Value: strings.Join(names, ", "), Note: noteUseCrond})
	}
	return rows
}

// dnfRows are Rocky Linux's and AlmaLinux's rows, from dnfauto.Read: the
// package, then, where it is installed, the install timer, the updates
// automatic.conf's upgrade_type asks for, and its restarts where it asks
// for any; Can't read for each fact not read.
func dnfRows(h reading) []component.KeyValue {
	u := h.DnfUpdates
	pv, ps := tri(u.Package, installed, notInstalled)
	rows := []component.KeyValue{{Key: screen.UpdatesDnfAutoKey, Value: pv, State: ps}}
	if u.Package != detect.Yes {
		return rows
	}
	tm, ok := u.Applies() // the timer the check reads: dnf-automatic.timer where it installs updates
	if !ok {
		tm = u.Install
	}
	t := component.KeyValue{Key: screen.UpdatesInTimerKey, Note: tm.Name}
	switch {
	case tm.Err != nil || tm.Active == "" || tm.Enabled == "":
		t.Value, t.State = cantRead, component.ValueUnknown
	case tm.Enabled == "masked" || tm.Enabled == maskedRuntime:
		t.Value, t.State = masked, component.ValueBad
	case tm.On():
		t.Value = on
	case tm.Active == "active":
		t.Value, t.State = offRestart, component.ValueWarn
	case tm.Enabled == "enabled":
		// Stopped, not "Starts after restart": the status row above says
		// that, and the unit's name would not fit beside it.
		t.Value, t.State = stopped, component.ValueWarn
	default:
		t.Value, t.State = off, component.ValueBad
	}
	typ := component.KeyValue{Key: screen.UpdatesTypeKey, Note: noteBy + path.Base(dnfauto.Conf)}
	switch {
	case u.ConfErr != nil:
		typ.Value, typ.State, typ.Note = cantRead, component.ValueUnknown, ""
	case u.UpgradeType == dnfauto.Security:
		typ.Value = securityOnly
	default:
		typ.Value = allUpdates
	}
	if u.ConfErr == nil && h.Changed == dnfauto.Conf {
		typ.Note, typ.State = noteChanged, component.ValueWarn // the item names the file
	}
	rows = append(rows, t, typ)
	if v := updatesctl.RebootValue(u.Reboot); v != "" {
		rows = append(rows, component.KeyValue{Key: screen.UpdatesRebootKey, Value: v, State: component.ValueWarn, Note: updatesctl.NoteByConf})
	}
	return rows
}

// notes are the review's lines beyond the change, the control's (Notes),
// a warning where the control marks one.
func notes(ns []updatesctl.Note) []component.KeyValue {
	var out []component.KeyValue
	for _, n := range ns {
		kv := component.KeyValue{Key: n.Key, Value: n.Value, Note: n.Note}
		if n.Warn {
			kv.State = component.ValueWarn
		}
		out = append(out, kv)
	}
	return out
}
