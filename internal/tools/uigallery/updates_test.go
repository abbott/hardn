package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestUpdatesGolden pins each listing mock on the core set and under
// LANG=C: the screen on every host, and the refusals.
func TestUpdatesGolden(t *testing.T) { goldenListing(t, updListing) }

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

// TestUpdatesHosts holds each host's row to the real check, and its item
// to the rule: Enable while off, Disable while on by hardn, read-only
// while on not by hardn and on Proxmox VE; every call one the model
// answers.
func TestUpdatesHosts(t *testing.T) {
	type want struct{ detail, item string }
	rows := map[string]string{} // decision 7's row is the check's own since unit (b)
	const ro = "read-only"
	wants := map[string]want{
		"debian13":         {"unattended-upgrades", ro},
		"ubuntu2604":       {"unattended-upgrades", ro},
		"alpine323":        {"no apk-cron", updEnable},
		"debian-installer": {"no unattended-upgrades", updEnable},
		"debian-off":       {"upgrades off", updEnable},
		"lists-stale":      {"lists not refreshed", updEnable},
		"timer-off":        {"upgrade timer off", updEnable},
		"by-hardn-debian":  {"unattended-upgrades", updDisable},
		"by-hardn-alpine":  {"apk-cron", updDisable},
		"edited":           {"unattended-upgrades", updDisable},
		"crond-shared":     {"apk-cron", updDisable},
		"dormant-jobs":     {"no apk-cron", updEnable},
		"config-files":     {"no unattended-upgrades", updEnable},
		"legacy-alpine":    {"no apk-cron", updEnable},
		"pve9":             {"updated by hand", ro},
		"not-read":         {"can't read", updEnable},
	}
	if len(wants) != len(updHosts()) {
		t.Fatalf("%d hosts, %d wanted", len(updHosts()), len(wants))
	}
	ctx := context.Background()
	for _, uh := range updHosts() {
		h := uh.host()
		r, err := readUpdates(ctx, h.sys())
		if err != nil {
			t.Fatalf("%s: %v", uh.name, err)
		}
		w := wants[uh.name]
		row, ok := rows[uh.name]
		if !ok {
			row = w.detail
		}
		if len(r.status.Rows) != 1 || r.status.Rows[0].Detail != row || r.check.Detail != w.detail {
			t.Errorf("%s: rows %+v, check %q; want %q", uh.name, r.status.Rows, r.check.Detail, w.detail)
		}
		it := r.item()
		got := it.Label
		if it.Disabled {
			got = ro
		}
		if got != w.item {
			t.Errorf("%s: item %q, want %q", uh.name, got, w.item)
		}
		if u := h.unanswered(); len(u) > 0 {
			t.Errorf("%s: unanswered %q", uh.name, u)
		}
	}
}

// TestUpdatesRefusals: each refusal of updRefusals refuses before asking,
// but on on a host hardn turned on, an empty plan.
func TestUpdatesRefusals(t *testing.T) {
	ctx := context.Background()
	for _, rf := range updRefusals() {
		words, err := updRefusalWords(ctx, rf)
		// pve9: since unit (b) the control is Not Applicable on Proxmox VE, so
		// the engine plans nothing for it, and the item is unavailable.
		empty := rf.host == "by-hardn-debian" || rf.host == "own-file" || rf.host == "pve9"
		if err != nil || (words == "") != empty {
			t.Errorf("%v: %q, %v", rf, words, err)
		}
	}
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
		r, err := readUpdates(ctx, h.sys())
		if err != nil {
			t.Fatal(err)
		}
		if words := (&updRunner{h: h}).refusalOf(ctx, r, true); !strings.Contains(words, tt.want) {
			t.Errorf("%s: %q, want %q", tt.name, words, tt.want)
		}
	}
	h := updByHardnDebian()
	h.f.AddFile(autoUpgrades, []byte(autoOff), 0o644, 0, 0)
	r, err := readUpdates(ctx, h.sys())
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
func updOffPlan(ctx context.Context, h *fakeHost, r updReading) ([]engine.Change, []component.KeyValue, error) {
	cs, err := updAuto(updates.AutoOff).Plan(ctx, h.system(), engine.Input{Facts: r.Facts, Config: r.Config})
	if err != nil {
		return nil, nil, err
	}
	kept, err := (&updRunner{h: h}).notes(ctx, r, true)
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

// TestUpdatesRefusalWords: each refusal says what is in the way and the
// fix, and the item, still offered, says it in a few words (R1).
func TestUpdatesRefusalWords(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		host      string
		off       bool
		words, it string
	}{
		{"timer-off", false, "apt-daily-upgrade.timer is off", "Upgrade timer is off"},
		{"not-read", false, "apt can't read its settings", "Can't read apt's settings"},
		{"masked", false, "apt-daily-upgrade.service is masked", "Upgrade job is masked"},
		{"missing", false, "apt-daily-upgrade.service is missing. Reinstall apt, then try again.", "Upgrade job is missing"},
		{"no-origin", false, "allows no origin", "No origin allowed"},
		{"extra-packages", false, "would also change other packages: Inst libc6", updDescInstall},
		{"enable-off", false, "turns apt's daily jobs off", "Periodic jobs are off"},
		{"edited", true, "changed since hardn wrote it", "52hardn-auto-upgrades changed"},
		{"no-record", true, "Updates stay on without hardn's file: 20auto-upgrades turns them on, and hardn has no record that it installed unattended-upgrades. Turn them off by hand.", "20auto-upgrades keeps updates on"},
	} {
		r, h, err := updReadNamed(ctx, tt.host)
		if err != nil {
			t.Fatal(err)
		}
		words := (&updRunner{h: h}).refusalOf(ctx, r, tt.off)
		if it := r.item(); !strings.Contains(words, tt.words) || it.Description != tt.it || it.Disabled {
			t.Errorf("%s: %q, item %+v; want %q, %q", tt.host, words, it, tt.words, tt.it)
		}
	}
}

// TestUpdatesCorruptRecord: on Alpine, a record hardn can't read keeps
// Disable offered, saying so, and refuses with the record's words (R1).
func TestUpdatesCorruptRecord(t *testing.T) {
	ctx := context.Background()
	h := updByHardnAlpine()
	h.f.AddFile(updRecordPath, []byte("{\"schemaVersion\": 1, \"entries\": [\n"), 0o600, 0, 0)
	r, err := readUpdates(ctx, h.sys())
	if err != nil {
		t.Fatal(err)
	}
	screen := ansi.Strip(r.screen().Render(component.NewCtx(term.Fixed(80, colorprofile.NoTTY, false, true))))
	if !strings.Contains(screen, updDisable+"\n      "+"Can't read hardn's record") || strings.Contains(screen, "Unavailable") {
		t.Errorf("the item:\n%s", screen)
	}
	if words := (&updRunner{h: h}).refusalOf(ctx, r, true); !strings.Contains(words, "can't read its record") {
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
		r, err := readUpdates(ctx, h.sys())
		if err != nil {
			t.Fatal(err)
		}
		cs, kept, err := updOffPlan(ctx, h, r)
		stops := slices.ContainsFunc(cs, func(c engine.Change) bool { return c.Title == "Stop crond" })
		if err != nil || stops != (tt.note == "") || tt.note != "" && !hasKept(kept, updRowCrond, tt.note) {
			t.Errorf("%s: %v, kept %v, %v", tt.name, changeIDs(cs), kept, err)
		}
	}
}

// TestUpdatesConfigFiles: the package's files there before hardn's
// install are said at Enable and kept at Disable, which removes the
// package in place of the purge (R2).
func TestUpdatesConfigFiles(t *testing.T) {
	ctx := context.Background()
	r, h, err := updReadNamed(ctx, "config-files")
	if err != nil {
		t.Fatal(err)
	}
	if on, err := (&updRunner{h: h}).notes(ctx, r, false); err != nil || !hasKept(on, "50unattended-upgrades", "there before hardn") {
		t.Errorf("Enable's notes %v, %v", on, err)
	}
	updApplied(h)
	if r, err = readUpdates(ctx, h.sys()); err != nil {
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
	r, err := readUpdates(ctx, h.sys())
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
	r, h, err := updReadNamed(ctx, "own-file")
	if err != nil {
		t.Fatal(err)
	}
	if it := r.item(); it.Label != updDisable || it.Disabled {
		t.Errorf("item %+v, want Disable", it)
	}
	cs, _, err := updOffPlan(ctx, h, r)
	if err != nil || len(cs) != 1 || cs[0].Target != hardnAptFile {
		t.Errorf("off: %v, %v; want the file alone", changeIDs(cs), err)
	}
}

// TestUpdatesPlanOnlyWritesNothing: under plan only every item opens and
// stops at its plan, nothing applied.
func TestUpdatesPlanOnlyWritesNothing(t *testing.T) {
	for _, host := range []string{"updates/debian-installer", "updates/debian-off", "updates/alpine323", "updates/legacy-alpine", "updates/by-hardn-debian", "updates/by-hardn-alpine"} {
		code, out, errs, unread := playGallery([]string{"1", "0"}, "-profile", "plain", "-sequence", "dry-run", host)
		_, record, _ := strings.Cut(out, "# simulated system ")
		if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, "No actual changes were made") || strings.Contains(record, "Applied: ") {
			t.Errorf("%s: exit %d, stderr %q, %d unread; the record:\n%s", host, code, errs, unread, record)
		}
	}
}

// TestUpdatesHeights: every host's screen is as many lines at 80x24 as at
// 80x40, Unicode and ASCII, the prompt on its last, no line over 80 cells.
func TestUpdatesHeights(t *testing.T) {
	ctx := context.Background()
	for _, uh := range updHosts() {
		r, _, err := updReadNamed(ctx, uh.name)
		if err != nil {
			t.Fatal(err)
		}
		var n []int
		for _, unicode := range []bool{true, false} {
			for _, rows := range []int{24, 40} {
				env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
				env.Height = rows
				lines := strings.Split(ansi.Strip(r.screen().Render(component.NewCtx(env))), "\n")
				for _, l := range lines {
					if w := ansi.StringWidth(l); w > 80 {
						t.Errorf("%s: %d cells: %q", uh.name, w, l)
					}
				}
				if !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s: the last line %q, want the prompt", uh.name, lines[len(lines)-1])
				}
				n = append(n, len(lines))
			}
		}
		t.Logf("%s: %v", uh.name, n)
		if n[0] != n[1] || n[0] != n[2] || n[0] != n[3] {
			t.Errorf("%s: lines %v, want the same at 24 and 40 rows", uh.name, n)
		}
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
		r, err := readUpdates(ctx, h.sys())
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
	if jobs, err := alpine.OtherJobs(ctx, h.sys()); err != nil || !slices.Equal(jobNames(jobs), []string{"backup"}) {
		t.Errorf("jobs %v, %v; want [backup]", jobs, err)
	}
}
