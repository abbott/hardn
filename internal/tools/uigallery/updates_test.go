package main

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestPlayUpdatesGolden plays each flow end to end and pins the
// transcript: each direction planned, asked y/N and applied on the real
// engine; refusals before asking; the read-only item; plan only; the hosts'
// menu; the main menu's 6.
func TestPlayUpdatesGolden(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"updates-enable-installer", []string{"updates/debian-installer"}, []string{"1", "y", "0"}, append(uitest.Core(), asciiColour)},
		{"updates-enable-off", []string{"updates/debian-off"}, []string{"1", "y", "0"}, nil},
		{"updates-enable-lists", []string{"updates/lists-stale"}, []string{"1", "y", "0"}, nil},
		{"updates-enable-alpine", []string{"updates/alpine323"}, []string{"1", "y", "0"}, append(uitest.Core(), asciiColour)},
		{"updates-enable-legacy", []string{"updates/legacy-alpine"}, []string{"1", "y", "0"}, nil},
		{"updates-disable-debian", []string{"updates/by-hardn-debian"}, []string{"1", "y", "0"}, append(uitest.Core(), asciiColour)},
		{"updates-disable-alpine", []string{"updates/by-hardn-alpine"}, []string{"1", "y", "0"}, nil},
		{"updates-crond-shared", []string{"updates/crond-shared"}, []string{"1", "y", "0"}, nil},
		{"updates-dormant-jobs", []string{"updates/dormant-jobs"}, []string{"1", "y", "1", "y", "0"}, nil},
		{"updates-config-files", []string{"updates/config-files"}, []string{"1", "y", "1", "y", "0"}, nil},
		{"updates-later-file", []string{"updates/later-file"}, []string{"1", "y", "0"}, nil},
		{"updates-decline", []string{"updates/debian-off"}, []string{"1", "n", "0"}, nil},
		{"updates-edited", []string{"updates/edited"}, []string{"1", "0"}, nil},
		{"updates-timer-off", []string{"updates/timer-off"}, []string{"1", "0"}, nil},
		{"updates-not-read", []string{"updates/not-read"}, []string{"1", "0"}, nil},
		{"updates-read-only", []string{"updates/debian13"}, []string{"1", "0"}, nil},
		{"updates-plan-only", []string{"-sequence", "dry-run", "updates/debian-installer"}, []string{"1", "0"}, nil},
		{"updates-hosts", []string{"updates"}, []string{"9", "0", "0"}, nil},
		{"updates-main-menu", nil, []string{"6", "0", "0"}, nil},
	}
	for _, tt := range tests {
		cells := tt.cells
		if cells == nil {
			cells = uitest.Core()
		}
		uitest.Golden(t, "play/"+tt.name, cells, func(env term.Env) string {
			args := append(lookArgs(t, env), tt.args...)
			code, out, errs, unread := playGallery(tt.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread", tt.name, args, code, errs, unread)
			}
			return sessionIDs(t, out)
		})
	}
}

// TestUpdatesRefusals: each direction the real control's Plan refuses on
// these hosts refuses before asking; on a host hardn turned on, and on
// Proxmox VE, where the control is Not Applicable, on plans nothing.
func TestUpdatesRefusals(t *testing.T) {
	ctx := context.Background()
	for _, rf := range []struct {
		host  string
		off   bool
		empty bool
	}{
		{"timer-off", false, false},      // a timer off: hardn leaves the timers alone
		{"masked", false, false},         // apt-daily-upgrade.service masked
		{"missing", false, false},        // apt-daily-upgrade.service not found
		{"enable-off", false, false},     // APT::Periodic::Enable "0"
		{"no-origin", false, false},      // 50unattended-upgrades allows none
		{"extra-packages", false, false}, // the install would change another package
		{"not-read", false, false},       // apt-config fails
		{"not-read", true, false},        // either direction
		{"edited", true, false},          // hardn's file changed since
		{"edited-alpine", true, false},   // apk's script changed since
		{"no-record", true, false},       // 20auto-upgrades keeps updates on without hardn's file
		{"debian13", true, false},        // not hardn's
		{"pve9", false, true},            // decision 7
		{"by-hardn-debian", false, true}, // on already: an empty plan, not a refusal
		{"own-file", false, true},        // on by hardn with no record: an empty plan too
	} {
		uh, ok := findUpdHost(rf.host)
		if !ok {
			t.Fatalf("no host %q", rf.host)
		}
		h := uh.host()
		r, err := updRead(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if words := updRefusalOf(ctx, h, r, rf.off); (words == "") != rf.empty {
			t.Errorf("%s, off %v: %q", rf.host, rf.off, words)
		}
	}
}

// updRead reads h as the screen does, writing nothing: updates.Auto's Read
// with detect's facts and hardn.yml the host's, or the defaults.
func updRead(ctx context.Context, h *fakeHost) (updates.Reading, error) {
	sys := h.sys()
	cfg, err := statusConfig(ctx, sys)
	if err != nil {
		return updates.Reading{}, err
	}
	f, err := detect.Detect(ctx, sys)
	if err != nil {
		return updates.Reading{}, err
	}
	return updAuto(updates.AutoNone).Read(ctx, sys, f, cfg)
}

// updDirection is the real control turning updates off (off) or on.
func updDirection(off bool) updates.Auto {
	if off {
		return updAuto(updates.AutoOff)
	}
	return updAuto(updates.AutoOn)
}

// updRefusalOf plans the real control in the direction off on h, writing
// nothing, and returns its first refusal in the screen's words, "" for none.
func updRefusalOf(ctx context.Context, h *fakeHost, r updates.Reading, off bool) string {
	p, err := updEngine(h, nil, &updQuiet).Plan(ctx, []engine.Control{updDirection(off)}, r.Config)
	switch {
	case err != nil:
		return screenkit.RefusalWords(err)
	case len(p.Refused) > 0:
		return screenkit.RefusalWords(p.Refused[0])
	}
	return ""
}

// updNotes are the control's review lines (Notes) in the direction off on
// h, as the review draws them, a warning where the control marks one.
func updNotes(ctx context.Context, h *fakeHost, r updates.Reading, off bool) ([]component.KeyValue, error) {
	ns, err := updDirection(off).Notes(ctx, h.system(), engine.Input{Facts: r.Facts, Config: r.Config})
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

// TestUpdatesOffChanged: off refuses each step changed since hardn made it
// (hardn's file, apk-cron's script, a record hardn can't read, a file of
// hardn's name holding other bytes), and where the package's files changed
// removes it in place of the purge, keeping them and saying so.
func TestUpdatesOffChanged(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name string
		host func() *fakeHost
		want string
	}{
		{"hardn's file", updEdited, hardnAptFile + " changed since"},
		{"apk's script", func() *fakeHost {
			h := updByHardnAlpine()
			h.f.AddFile(alpine.Script, []byte("#!/bin/sh\n"), 0o755, 0, 0)
			return h
		}, alpine.Script + " changed since"},
		{"the record", func() *fakeHost {
			h := updByHardnDebian()
			h.f.AddFile(updRecordPath, []byte("{\"schemaVersion\": 2}\n"), 0o600, 0, 0)
			return h
		}, "can't read its record"},
		{"no record, other bytes", func() *fakeHost {
			h := updDebianOff()
			h.f.AddFile(hardnAptFile, []byte("APT::Periodic::Unattended-Upgrade \"1\";\n"), 0o644, 0, 0)
			return h
		}, hardnAptFile + " changed since"},
	} {
		h := tt.host()
		r, err := updRead(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if words := updRefusalOf(ctx, h, r, true); !strings.Contains(words, tt.want) {
			t.Errorf("%s: %q, want %q", tt.name, words, tt.want)
		}
	}
	h := updByHardnDebian()
	h.f.AddFile(autoUpgrades, []byte(autoOff), 0o644, 0, 0)
	r, err := updRead(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	cs, kept, err := updOffPlan(ctx, h, r)
	if err != nil || len(cs) != 3 || cs[1].Commands[0] != "apt-get remove -y "+uuPackage || !hasKept(kept, "20auto-upgrades", "changed since hardn") {
		t.Errorf("20auto-upgrades changed: %v, kept %v, %v", changeIDs(cs), kept, err)
	}
}

// updOffPlan is the real control's off on h as r reads it: its changes,
// and its review lines as the screen draws them.
func updOffPlan(ctx context.Context, h *fakeHost, r updates.Reading) ([]engine.Change, []component.KeyValue, error) {
	cs, err := updAuto(updates.AutoOff).Plan(ctx, h.system(), engine.Input{Facts: r.Facts, Config: r.Config})
	if err != nil {
		return nil, nil, err
	}
	kept, err := updNotes(ctx, h, r, true)
	return cs, kept, err
}

// changeIDs are the ids of cs.
func changeIDs(cs []engine.Change) []string {
	var ids []string
	for _, c := range cs {
		ids = append(ids, c.ID)
	}
	return ids
}

// TestUpdatesHardnBytes: the fake's hosts hold hardn's file and record
// where the control writes and reads them: each of hardnAptBytes' files is
// one updates.IsHardnFile accepts, and updRecordPath is the control's
// RecordPath.
func TestUpdatesHardnBytes(t *testing.T) {
	for _, ks := range [][]string{{keyLists}, {keyUpgrade}, hardnLines} {
		if !updates.IsHardnFile(hardnAptBytes(ks)) {
			t.Errorf("%v: not hardn's file to the control:\n%s", ks, hardnAptBytes(ks))
		}
	}
	if p, err := updAuto(updates.AutoOff).RecordPath(); err != nil || p != updRecordPath {
		t.Errorf("RecordPath %q, %v; want %q", p, err, updRecordPath)
	}
}

// hasKept is whether kept has the line key, note.
func hasKept(kept []component.KeyValue, key, note string) bool {
	for _, kv := range kept {
		if kv.Key == key && kv.Note == note {
			return true
		}
	}
	return false
}

// TestUpdatesCorruptRecord: on Alpine, a record hardn can't read refuses
// off with the record's words (R1).
func TestUpdatesCorruptRecord(t *testing.T) {
	ctx := context.Background()
	h := updByHardnAlpine()
	h.f.AddFile(updRecordPath, []byte("{\"schemaVersion\": 1, \"entries\": [\n"), 0o600, 0, 0)
	r, err := updRead(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	if words := updRefusalOf(ctx, h, r, true); !strings.Contains(words, "can't read its record") {
		t.Errorf("off: %q", words)
	}
}

// TestUpdatesCrondJobs: a job added since Enable keeps crond at off, said,
// whether under /etc/periodic or another user's crontab; jobs there at
// Enable do not (dormant-jobs).
func TestUpdatesCrondJobs(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name string
		add  func(*fakeHost)
		note string
	}{
		{"a periodic job", func(h *fakeHost) {
			h.f.AddFile("/etc/periodic/hourly/backup", []byte("#!/bin/sh\n"), 0o755, 0, 0)
		}, "backup added since"},
		{"carol's crontab", func(h *fakeHost) {
			h.f.AddFile("/etc/crontabs/carol", []byte("0 4 * * * /home/carol/sync\n"), 0o600, 0, 0)
		}, "carol's crontab added since"},
		{"none", func(*fakeHost) {}, ""},
	} {
		h := updApplied(updDormantJobs())
		tt.add(h)
		r, err := updRead(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		cs, kept, err := updOffPlan(ctx, h, r)
		stops := slices.ContainsFunc(cs, func(c engine.Change) bool { return c.Title == "Stop crond" })
		if err != nil || stops != (tt.note == "") || tt.note != "" && !hasKept(kept, "crond", tt.note) {
			t.Errorf("%s: %v, kept %v, %v", tt.name, changeIDs(cs), kept, err)
		}
	}
}

// TestUpdatesConfigFiles: the package's files there before hardn's
// install are said at Enable and kept at Disable, which removes the
// package in place of the purge (R2).
func TestUpdatesConfigFiles(t *testing.T) {
	ctx := context.Background()
	h := updConfigFiles()
	r, err := updRead(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	if on, err := updNotes(ctx, h, r, false); err != nil || !hasKept(on, "50unattended-upgrades", "there before hardn") {
		t.Errorf("Enable's notes %v, %v", on, err)
	}
	updApplied(h)
	if r, err = updRead(ctx, h); err != nil {
		t.Fatal(err)
	}
	cs, kept, err := updOffPlan(ctx, h, r)
	if err != nil || len(cs) < 2 || cs[1].Commands[0] != "apt-get remove -y "+uuPackage || !hasKept(kept, "50unattended-upgrades", "there before hardn") {
		t.Errorf("off: %v, kept %v, %v", changeIDs(cs), kept, err)
	}
}

// TestUpdatesPurgeRechecked: the purge checks both files' hashes again
// when it runs, and fails before purging where either changed.
func TestUpdatesPurgeRechecked(t *testing.T) {
	ctx := context.Background()
	h := updByHardnDebian()
	r, err := updRead(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	cs, _, err := updOffPlan(ctx, h, r)
	i := slices.IndexFunc(cs, func(c engine.Change) bool { return c.ID == updates.ID+"/package" })
	if err != nil || i < 0 || cs[i].Commands[0] != "apt-get purge -y "+uuPackage {
		t.Fatalf("off: %v, %v", changeIDs(cs), err)
	}
	h.f.AddFile(uuConfig, []byte(uuOrigins(false)+"// edited\n"), 0o644, 0, 0)
	if _, err := cs[i].Apply(ctx, h.system(), engine.Run{}); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Errorf("Apply: %v, want changed since", err)
	}
	if _, ok := h.status.packages[uuPackage]; !ok {
		t.Error("purged")
	}
}

// TestUpdatesOwnFile: with no record, hardn's own file holding hardn's
// bytes is on by hardn, and off deletes it and nothing else.
func TestUpdatesOwnFile(t *testing.T) {
	ctx := context.Background()
	uh, _ := findUpdHost("own-file")
	h := uh.host()
	r, err := updRead(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	if !r.ByHardn() {
		t.Error("not on by hardn")
	}
	cs, _, err := updOffPlan(ctx, h, r)
	if err != nil || len(cs) != 1 || cs[0].Target != hardnAptFile {
		t.Errorf("off: %v, %v; want the file alone", changeIDs(cs), err)
	}
}

// TestUpdatesOffRefusals: off refuses on Proxmox VE, and where its
// package simulation would change another package, apt's and apk's alike;
// a file under /etc/periodic without its execute bit is no job of crond's.
func TestUpdatesOffRefusals(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name string
		host func() *fakeHost
		code string
	}{
		{"Proxmox VE", updPVE, "UPDATES_PROXMOX"},
		{"apt's purge", func() *fakeHost {
			h := updByHardnDebian()
			h.upd.offExtra = []string{"Remv python3-apt [3.0.0]"}
			return h
		}, "UPDATES_OTHER_PACKAGES"},
		{"apk del", func() *fakeHost {
			h := updByHardnAlpine()
			h.upd.offExtra = []string{"(2/2) Purging busybox-extras (1.37.0-r0)"}
			return h
		}, "UPDATES_OTHER_PACKAGES"},
	} {
		h := tt.host()
		r, err := updRead(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = updOffPlan(ctx, h, r)
		var rf *steps.Refusal
		if !errors.As(err, &rf) || rf.Code != tt.code {
			t.Errorf("%s: %v, want %s", tt.name, err, tt.code)
		}
	}
	h := updAlpine()
	h.f.AddFile("/etc/periodic/daily/notes", []byte("x\n"), 0o644, 0, 0)
	h.f.AddFile("/etc/periodic/weekly/backup", []byte("#!/bin/sh\n"), 0o755, 0, 0)
	if jobs, err := alpine.OtherJobs(ctx, h.sys()); err != nil || len(jobs) != 1 || jobs[0].Name != "backup" {
		t.Errorf("jobs %v, %v; want [backup]", jobs, err)
	}
}

// TestUpdatesUsage: play refuses what it does not take, and the listing is
// gone, with status 2 and nothing on stdout; the main menu's 6 leads to
// debian13's.
func TestUpdatesUsage(t *testing.T) {
	for _, args := range [][]string{
		{updPrefix},
		{"play", "updates/nope"},
		{"play", "-fail", "1", "updates/debian13"},
		{"play", "-sequence", "failure", "updates/debian13"},
	} {
		var out, errs bytes.Buffer
		if code := run(args, nil, offTerminal, &out, &errs); code != exitUsage || out.Len() > 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errs.String())
		}
	}
	if updMainHost != updPrefix+"/debian13" || updMenuItem.Key != "6" {
		t.Errorf("the main menu's %s leads to %q", updMenuItem.Key, updMainHost)
	}
}
