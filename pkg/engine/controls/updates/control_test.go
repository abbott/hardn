package updates_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
)

// plan plans a on m with the control alone, as the screen's preplan does.
func plan(t *testing.T, m *model, a updates.AutoAction, cfg *config.Config) ([]engine.Change, error) {
	t.Helper()
	return control(a).Plan(context.Background(), m.f, engine.Input{Facts: m.facts, Config: cfg})
}

func ids(cs []engine.Change) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func record(t *testing.T, m *model) *revert.ControlRecord {
	t.Helper()
	rec, err := revert.ReadRecord(context.Background(), m.f, revert.Options{}, updates.ID)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func content(t *testing.T, m *model, name string) (string, bool) {
	t.Helper()
	data, err := m.f.ReadFile(context.Background(), name)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// TestRefusals: every refusal, each before anything is written, with its
// code, its sentinel and the decided words (gallery-updates.md §5, the
// mock's updRefusals and more); the two empty plans.
func TestRefusals(t *testing.T) {
	for _, c := range []struct {
		name     string
		host     func(*testing.T) *model
		off      bool
		code     string
		sentinel error
		words    string // the Why, then ". ", the Fix
	}{
		{"timer-off", func(t *testing.T) *model {
			m := aptHost(t, debian13(), autoOn)
			m.units["is-active apt-daily-upgrade.timer"], m.units["is-enabled apt-daily-upgrade.timer"] = "inactive", "disabled"
			return m
		}, false, "UPDATES_TIMER_OFF", updates.ErrTimerOff, "apt-daily-upgrade.timer is off, and hardn leaves apt's timers to you. run systemctl enable --now apt-daily-upgrade.timer, then try again"},
		{"lists timer off after restart", func(t *testing.T) *model {
			m := debianOff(t)
			m.units["is-enabled apt-daily.timer"] = "disabled"
			return m
		}, false, "UPDATES_TIMER_OFF", updates.ErrTimerOff, "apt-daily.timer is off, and hardn leaves apt's timers to you. run systemctl enable --now apt-daily.timer, then try again"},
		{"masked", func(t *testing.T) *model {
			m := debianOff(t)
			m.units["is-enabled apt-daily-upgrade.service"] = "masked"
			return m
		}, false, "UPDATES_JOB_MASKED", updates.ErrJobMasked, "apt-daily-upgrade.service is masked, and hardn leaves apt's timers to you. run systemctl unmask apt-daily-upgrade.service, then try again"},
		{"missing", func(t *testing.T) *model {
			m := debianOff(t)
			m.units["is-enabled apt-daily-upgrade.service"] = "not-found"
			return m
		}, false, "UPDATES_JOB_MISSING", updates.ErrJobMissing, "apt-daily-upgrade.service is missing. reinstall apt, then try again"},
		{"enable-off", func(t *testing.T) *model {
			m := debianOff(t)
			m.file(confDir+"/99site", "APT::Periodic::Enable \"0\";\n")
			return m
		}, false, "UPDATES_PERIODIC_OFF", updates.ErrPeriodicOff, "APT::Periodic::Enable \"0\", in /etc/apt/apt.conf.d/99site, turns apt's daily jobs off. remove that line, then try again"},
		{"no-origin", func(t *testing.T) *model {
			m := debianOff(t)
			m.file(uuConf, "// No origins.\n")
			return m
		}, false, "UPDATES_NO_ORIGINS", updates.ErrNoOrigins, "50unattended-upgrades allows no origin, so nothing would be upgraded. add your distribution's origins there, then try again"},
		{"extra-packages", func(t *testing.T) *model {
			m := installer(t)
			m.extra = []string{"Inst libc6 [2.41-11] (2.41-12 Debian:13.1/stable [arm64])"}
			return m
		}, false, "UPDATES_OTHER_PACKAGES", updates.ErrOtherPackages, "Installing unattended-upgrades would also change other packages: Inst libc6 [2.41-11] (2.41-12 Debian:13.1/stable [arm64]). install it by hand, then try again"},
		{"not in the lists", func(t *testing.T) *model {
			m := installer(t)
			m.notInRepos = true
			return m
		}, false, "UPDATES_NOT_IN_REPOS", updates.ErrNotInRepos, "hardn can't find unattended-upgrades in apt's package lists. run apt-get update, then try again"},
		{"not-read on", notRead, false, "UPDATES_NOT_READ", updates.ErrNotRead, "apt can't read its settings: Syntax error /etc/apt/apt.conf.d/99site:1: Extra junk at end of file. fix that, then try again"},
		{"not-read off", notRead, true, "UPDATES_NOT_READ", updates.ErrNotRead, "apt can't read its settings: Syntax error /etc/apt/apt.conf.d/99site:1: Extra junk at end of file. fix that, then try again"},
		{"edited", func(t *testing.T) *model {
			m := installer(t).applied(updates.AutoOn)
			m.file(hardnFile, bothLines+"APT::Periodic::Enable \"1\";\n")
			return m
		}, true, "UPDATES_CHANGED_SINCE", updates.ErrChangedSince, "Not undone: /etc/apt/apt.conf.d/52hardn-auto-upgrades changed since hardn wrote it. put it back as it was or undo the rest by hand"},
		{"edited-alpine", func(t *testing.T) *model {
			m := alpineHost(t).applied(updates.AutoOn)
			m.f.AddFile(alpine.Script, []byte(apkDaily+"echo done\n"), 0o755, 0, 0)
			return m
		}, true, "UPDATES_CHANGED_SINCE", updates.ErrChangedSince, "Not undone: /etc/periodic/daily/apk changed since hardn installed it. put it back as it was or undo the rest by hand"},
		{"no-record", func(t *testing.T) *model {
			m := installer(t).applied(updates.AutoOn)
			if err := m.f.Remove(context.Background(), recordPath); err != nil {
				t.Fatal(err)
			}
			return m
		}, true, "UPDATES_STAY_ON", updates.ErrStaysOn, "Updates stay on without hardn's file: 20auto-upgrades turns them on, and hardn has no record that it installed unattended-upgrades. turn them off by hand"},
		{"own file edited, no record", func(t *testing.T) *model {
			m := debianOff(t)
			m.file(hardnFile, bothLines+"APT::Periodic::Verbose \"1\";\n")
			return m
		}, true, "UPDATES_CHANGED_SINCE", updates.ErrChangedSince, "Not undone: /etc/apt/apt.conf.d/52hardn-auto-upgrades changed since hardn wrote it. put it back as it was or undo the rest by hand"},
		{"upgrade timer enabled, not active", func(t *testing.T) *model {
			m := debianOff(t)
			m.units["is-active apt-daily-upgrade.timer"] = "inactive"
			return m
		}, false, "UPDATES_TIMER_OFF", updates.ErrTimerOff, "apt-daily-upgrade.timer is off, and hardn leaves apt's timers to you. run systemctl enable --now apt-daily-upgrade.timer, then try again"},
		{"lists timer enabled, not active", func(t *testing.T) *model {
			m := debianOff(t)
			m.units["is-active apt-daily.timer"] = "inactive"
			return m
		}, false, "UPDATES_TIMER_OFF", updates.ErrTimerOff, "apt-daily.timer is off, and hardn leaves apt's timers to you. run systemctl enable --now apt-daily.timer, then try again"},
		{"remove would install others", func(t *testing.T) *model {
			m := installer(t).applied(updates.AutoOn)
			m.offExtra = []string{"Inst python3-gi [3.50.0-4] (3.50.0-5 Debian:13.1/stable [arm64])"}
			return m
		}, true, "UPDATES_OTHER_PACKAGES", updates.ErrOtherPackages, "Removing unattended-upgrades would also change other packages: Inst python3-gi. remove it by hand"},
		{"alpine jobs not read", func(t *testing.T) *model {
			m := alpineHost(t).applied(updates.AutoOn)
			m.f.AddSymlink("/etc/periodic/hourly/loop", "/etc/periodic/hourly/loop") // stat fails, not "absent"
			return m
		}, true, "UPDATES_NOT_READ", updates.ErrNotRead, "hardn can't read how updates run here: alpine: crond's jobs: stat /etc/periodic/hourly/loop: too many levels of symbolic links. fix that, then try again"},
		{"record unreadable, off", unreadableRecord, true, "UPDATES_RECORD_UNREADABLE", updates.ErrRecordUnreadable, "hardn can't read its record of what it changed (/var/lib/hardn/controls/updates.auto.json). undo it by hand"},
		{"record unreadable, on", unreadableRecord, false, "UPDATES_RECORD_UNREADABLE", updates.ErrRecordUnreadable, "hardn can't read its record of what it changed (/var/lib/hardn/controls/updates.auto.json). undo it by hand"},
		{"pve9 on", func(t *testing.T) *model { return aptHost(t, pve9(), "") }, false, "UPDATES_PROXMOX", updates.ErrProxmox, "Proxmox VE is updated with apt full-upgrade, and unattended-upgrades wouldn't take its packages. update it by hand"},
		{"pve9 off", func(t *testing.T) *model { return aptHost(t, pve9(), autoOn) }, true, "UPDATES_PROXMOX", updates.ErrProxmox, "hardn doesn't change updates on Proxmox VE. change them by hand"},
		{"debian13 off", func(t *testing.T) *model { return aptHost(t, debian13(), autoOn) }, true, "UPDATES_NOTHING_TO_UNDO", updates.ErrNothingToUndo, "Nothing to undo: hardn did not turn this on. turn it off by hand"},
		{"purge would remove others", func(t *testing.T) *model {
			m := installer(t).applied(updates.AutoOn)
			m.offExtra = []string{"Purg python3-dbus [1.3.2]"}
			return m
		}, true, "UPDATES_OTHER_PACKAGES", updates.ErrOtherPackages, "Removing unattended-upgrades would also change other packages: Purg python3-dbus. remove it by hand"},
		{"apk del would remove others", func(t *testing.T) *model {
			m := alpineHost(t).applied(updates.AutoOn)
			m.offExtra = []string{"(2/2) Purging busybox-extras (1.37.0-r30)"}
			return m
		}, true, "UPDATES_OTHER_PACKAGES", updates.ErrOtherPackages, "Removing apk-cron would also change other packages: Purging apk-cron (1.0-r3), Purging busybox-extras (1.37.0-r30). remove it by hand"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := c.host(t)
			n := len(m.f.Calls())
			a := updates.AutoOn
			if c.off {
				a = updates.AutoOff
			}
			cs, err := plan(t, m, a, nil)
			var ref *steps.Refusal
			if !errors.As(err, &ref) || cs != nil {
				t.Fatalf("Plan %v, %v: want a refusal", ids(cs), err)
			}
			if got := ref.Why + ". " + ref.Fix; got != c.words {
				t.Errorf("words\n %q\nwant\n %q", got, c.words)
			}
			if ref.Code != c.code || output.CodeOf(err) != c.code || !errors.Is(err, c.sentinel) {
				t.Errorf("code %s, registry's %s, want %s (%v)", ref.Code, output.CodeOf(err), c.code, err)
			}
			if w := writes(m.f, n); len(w) > 0 {
				t.Errorf("the plan wrote: %q", w)
			}
		})
	}
}

func notRead(t *testing.T) *model {
	m := aptHost(t, debian13(), autoOn)
	m.file(confDir+"/99site", "APT::Periodic::Verbose 1\n")
	return m
}

func unreadableRecord(t *testing.T) *model {
	m := debianOff(t)
	m.f.AddDir("/var/lib/hardn", 0o700, 0, 0)
	m.f.AddDir("/var/lib/hardn/controls", 0o700, 0, 0)
	m.f.AddFile(recordPath, []byte(`{"schemaVersion": 2, "entries": []}`), 0o600, 0, 0)
	return m
}

// TestEmptyPlans: on where updates run already plans nothing, by the
// package's file and by hardn's own with no record.
func TestEmptyPlans(t *testing.T) {
	for name, host := range map[string]func(*testing.T) *model{
		"debian13":        func(t *testing.T) *model { return aptHost(t, debian13(), autoOn) },
		"by-hardn-debian": func(t *testing.T) *model { return installer(t).applied(updates.AutoOn) },
		"own-file": func(t *testing.T) *model {
			m := debianOff(t)
			m.file(hardnFile, bothLines)
			return m
		},
		"by-hardn-alpine": func(t *testing.T) *model { return alpineHost(t).applied(updates.AutoOn) },
	} {
		t.Run(name, func(t *testing.T) {
			if cs, err := plan(t, host(t), updates.AutoOn, nil); cs != nil || err != nil {
				t.Errorf("Plan %v, %v", ids(cs), err)
			}
		})
	}
}

// TestNotMigrated: the zero value plans nothing, so the status list's
// control stays check-only.
func TestNotMigrated(t *testing.T) {
	m := debianOff(t)
	if _, err := (updates.Auto{}).Plan(context.Background(), m.f, engine.Input{Facts: m.facts}); !errors.Is(err, engine.ErrNotMigrated) {
		t.Errorf("Plan: %v", err)
	}
	if _, err := (updates.Auto{Action: 9}).Plan(context.Background(), m.f, engine.Input{Facts: m.facts}); err == nil {
		t.Error("Plan of an unknown action: no error")
	}
}

// TestOnApt: on for Debian and Ubuntu, on the engine: the plan's changes,
// hardn's file as read back, the record written and read again, the
// check after.
func TestOnApt(t *testing.T) {
	for _, c := range []struct {
		name    string
		host    func(*testing.T) *model
		ids     []string
		file    string
		steps   []string
		pkgs    []string
		readBak string
	}{
		{"debian-installer", installer, []string{"updates.auto/install", "updates.auto/file", "updates.auto/record"}, bothLines,
			[]string{"install", "file"}, []string{"python3-dbus", "python3-distro-info", uuName}, "configured: unattended-upgrades"},
		{"debian-off", debianOff, []string{"updates.auto/file", "updates.auto/record"}, bothLines, []string{"file"}, nil, "configured: unattended-upgrades"},
		{"lists-stale", func(t *testing.T) *model { return aptHost(t, debian13(), listsStale) }, []string{"updates.auto/file", "updates.auto/record"},
			hardnHead + "APT::Periodic::Update-Package-Lists \"1\";\n", []string{"file"}, nil, "configured: unattended-upgrades"},
		{"ubuntu, debconf false", func(t *testing.T) *model {
			m := aptHost(t, ubuntu2604(), autoOff)
			m.debconfOff = true
			return m
		}, []string{"updates.auto/file", "updates.auto/record"}, bothLines, []string{"file"}, nil, "configured: unattended-upgrades"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := c.host(t)
			got, err := m.apply(updates.AutoOn, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, c.ids) {
				t.Errorf("changes %q, want %q", got, c.ids)
			}
			if data, _ := content(t, m, hardnFile); data != c.file {
				t.Errorf("hardn's file %q, want %q", data, c.file)
			}
			rec := record(t, m)
			if rec == nil || len(rec.Entries) != 1 {
				t.Fatalf("record %+v", rec)
			}
			e := rec.Entries[0]
			if e.Direction != "on" || !e.Time.Equal(clock) || e.Hardn != "v0.4.0" || !revert.ValidID(e.PlanID) ||
				!slices.Equal(e.Steps, c.steps) || !slices.Equal(e.Packages, c.pkgs) || e.ReadBack != c.readBak {
				t.Errorf("entry %+v", e)
			}
			if fi, _ := m.f.Stat(context.Background(), recordPath); fi == nil || fi.Mode().Perm() != 0o600 {
				t.Errorf("record's mode: %v", fi)
			}
			wantFiles := []string{hardnFile}
			if slices.Contains(c.steps, "install") {
				wantFiles = []string{autoUp, uuConf, hardnFile}
				if len(e.Before) != 2 || e.Before[0].SHA256 != "" || e.Before[1].SHA256 != "" {
					t.Errorf("before %+v: both absent", e.Before)
				}
			}
			var paths []string
			for _, f := range e.Files {
				paths = append(paths, f.Path)
				if f.SHA256 == "" {
					t.Errorf("%s: no hash", f.Path)
				}
			}
			if !slices.Equal(paths, wantFiles) {
				t.Errorf("files %q, want %q", paths, wantFiles)
			}
			r, err := control(updates.AutoNone).Read(context.Background(), m.f, m.facts, nil)
			if err != nil || r.Status.State != engine.Configured || !r.ByHardn() || r.Record == nil || r.Changed != "" || len(r.Status.Findings) != 0 {
				t.Errorf("after: %s %q, by hardn %v, changed %q, findings %v, %v", r.Status.State, r.Status.Detail, r.ByHardn(), r.Changed, r.Status.Findings, err)
			}
			if cs, err := plan(t, m, updates.AutoOn, nil); cs != nil || err != nil {
				t.Errorf("again: %v, %v", ids(cs), err)
			}
		})
	}
}

// TestOnReadBackFails: a file read after hardn's that sets a value back
// fails the read-back, and the apply is rolled back: hardn's file and the
// record gone.
func TestOnReadBackFails(t *testing.T) {
	m := debianOff(t)
	m.file(confDir+"/60site", "APT::Periodic::Unattended-Upgrade \"0\";\n")
	_, err := m.apply(updates.AutoOn, nil)
	if output.CodeOf(err) != "UPDATES_NOT_IN_EFFECT" || !strings.Contains(err.Error(), `apt reads APT::Periodic::Unattended-Upgrade as "0" after the change, not "1": a file read later sets it`) {
		t.Fatalf("apply: %v (%s)", err, output.CodeOf(err))
	}
	if _, ok := content(t, m, hardnFile); ok {
		t.Error("hardn's file left")
	}
	if _, ok := content(t, m, recordPath); ok {
		t.Error("record written")
	}
}

// TestOffApt: off on Debian, on the engine: the purge where both files were
// absent before and hold the recorded bytes; a removal, files kept, where
// one was there before; the purge's re-check just before it runs; hardn's
// own file alone with no record.
func TestOffApt(t *testing.T) {
	t.Run("purge", func(t *testing.T) {
		m := installer(t).applied(updates.AutoOn)
		notes, err := control(updates.AutoOff).Notes(context.Background(), m.f, engine.Input{Facts: m.facts})
		if err != nil {
			t.Fatal(err)
		}
		wantNotes := []updates.Note{
			{Key: "python3-dbus", Value: "Kept", Note: "apt autoremove removes it"},
			{Key: "python3-distro-info", Value: "Kept", Note: "apt autoremove removes it"},
			{Key: "Rollback", Value: "Reinstalls from the repositories"},
			{Key: "Rollback", Value: "Turns updates on", Note: "by 20auto-upgrades", Warn: true},
		}
		if !slices.Equal(notes, wantNotes) {
			t.Errorf("notes %+v", notes)
		}
		cs, err := plan(t, m, updates.AutoOff, nil)
		if err != nil {
			t.Fatal(err)
		}
		if i := slices.IndexFunc(cs, func(c engine.Change) bool { return c.ID == updates.ChangePackage }); i < 0 || cs[i].StateAfter != "purged, with 20auto-upgrades" {
			t.Errorf("changes %q", ids(cs))
		}
		got, err := m.apply(updates.AutoOff, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, []string{"updates.auto/file", "updates.auto/package", "updates.auto/record"}) {
			t.Errorf("changes %q", got)
		}
		for _, p := range []string{hardnFile, autoUp, uuConf, recordPath} {
			if _, ok := content(t, m, p); ok {
				t.Errorf("%s left", p)
			}
		}
		if m.installed {
			t.Error("package left")
		}
	})
	t.Run("config-files: removed, kept", func(t *testing.T) {
		m := installer(t)
		m.file(uuConf, origins+"Unattended-Upgrade::Mail \"root\";\n")
		m.applied(updates.AutoOn)
		notes, err := control(updates.AutoOff).Notes(context.Background(), m.f, engine.Input{Facts: m.facts})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(notes, updates.Note{Key: "50unattended-upgrades", Value: "Kept", Note: "there before hardn", Warn: true}) ||
			!slices.Contains(notes, updates.Note{Key: "20auto-upgrades", Value: "Kept", Note: "apt-get remove keeps it"}) {
			t.Errorf("notes %+v", notes)
		}
		if _, err := m.apply(updates.AutoOff, nil); err != nil {
			t.Fatal(err)
		}
		if _, ok := content(t, m, uuConf); !ok || m.installed {
			t.Errorf("50unattended-upgrades kept %v, installed %v", ok, m.installed)
		}
	})
	t.Run("re-check before the purge", func(t *testing.T) {
		// The engine plans again at apply, so a file changed before the
		// apply is PLAN_CHANGED; the purge's own check covers a change
		// after that, run here on the change itself.
		m := installer(t).applied(updates.AutoOn)
		ctx := context.Background()
		cs, err := plan(t, m, updates.AutoOff, nil)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(cs, func(c engine.Change) bool { return c.ID == updates.ChangePackage })
		if i < 0 {
			t.Fatalf("changes %q", ids(cs))
		}
		m.file(autoUp, autoOn+"APT::Periodic::Verbose \"1\";\n") // edited just before the purge
		n := len(m.f.Calls())
		_, err = cs[i].Apply(ctx, m.f, engine.Run{Facts: m.facts})
		if output.CodeOf(err) != "UPDATES_CHANGED_SINCE" || !strings.Contains(err.Error(), "Not undone: /etc/apt/apt.conf.d/20auto-upgrades changed since hardn installed it") {
			t.Fatalf("apply: %v (%s)", err, output.CodeOf(err))
		}
		if w := writes(m.f, n); len(w) > 0 || !m.installed {
			t.Errorf("purged anyway: %q", w)
		}
		m2 := installer(t).applied(updates.AutoOn)
		e := m2.engine()
		p, err := e.Plan(ctx, []engine.Control{control(updates.AutoOff)}, nil)
		if err != nil || len(p.Refused) > 0 {
			t.Fatal(err, p.Refused)
		}
		m2.file(autoUp, autoOn+"APT::Periodic::Verbose \"1\";\n")
		if _, err := e.Apply(ctx, p); output.CodeOf(err) != "PLAN_CHANGED" {
			t.Errorf("engine apply: %v", err)
		}
	})
	t.Run("own file, no record", func(t *testing.T) {
		m := debianOff(t)
		m.file(hardnFile, bothLines)
		got, err := m.apply(updates.AutoOff, nil)
		if err != nil || !slices.Equal(got, []string{"updates.auto/file"}) {
			t.Fatalf("%q, %v", got, err)
		}
		if _, ok := content(t, m, hardnFile); ok {
			t.Error("hardn's file left")
		}
	})
	t.Run("read back on", func(t *testing.T) {
		m := debianOff(t).applied(updates.AutoOn)
		m.file(autoUp, autoOn) // 20auto-upgrades turned on since: off cannot turn updates off
		_, err := m.apply(updates.AutoOff, nil)
		if output.CodeOf(err) != "UPDATES_NOT_IN_EFFECT" || !strings.Contains(err.Error(), "apt still reads updates as on after the change (unattended-upgrades)") {
			t.Fatalf("apply: %v", err)
		}
		if _, ok := content(t, m, hardnFile); !ok || record(t, m) == nil {
			t.Error("not rolled back")
		}
	})
}

// TestOffSave: off saves enableUnattendedUpgrades false where the loaded
// hardn.yml says true (decision 2), and only there.
func TestOffSave(t *testing.T) {
	for _, on := range []bool{true, false} {
		m := debianOff(t).applied(updates.AutoOn)
		yml := "# hardn's configuration (web-01.example.net)\nusername: alice\nenableUnattendedUpgrades: " + map[bool]string{true: "true", false: "false"}[on] + "\n"
		m.f.AddFile(hardnYML, []byte(yml), 0o600, 0, 0)
		cfg, err := config.Read(context.Background(), m.f, hardnYML)
		if err != nil {
			t.Fatal(err)
		}
		got, err := m.apply(updates.AutoOff, cfg)
		if err != nil {
			t.Fatal(err)
		}
		saved := slices.Contains(got, updates.ChangeSave)
		data, _ := content(t, m, hardnYML)
		if saved != on || !strings.Contains(data, "enableUnattendedUpgrades: false") {
			t.Errorf("hardn.yml true %v: changes %q, file %q", on, got, data)
		}
	}
}

// TestApk: Alpine both ways, and crond's three cases at off (decision 6):
// started by hardn, stopped again; running before hardn, kept; a job added
// since, kept.
func TestApk(t *testing.T) {
	t.Run("on, then off", func(t *testing.T) {
		m := alpineHost(t)
		got, err := m.apply(updates.AutoOn, nil)
		if err != nil || !slices.Equal(got, []string{"updates.auto/apk", "updates.auto/record"}) {
			t.Fatalf("%q, %v", got, err)
		}
		rec := record(t, m)
		e := rec.Entries[0]
		if !slices.Equal(e.Steps, []string{"install", "runlevel", "start"}) || len(e.Jobs) != 0 ||
			!slices.Equal(e.Services, []revert.ServiceState{{Name: "crond", State: "stopped", AtBoot: "no"}}) ||
			len(e.Files) != 1 || e.Files[0].Path != alpine.Script || e.ReadBack != "configured: apk-cron" {
			t.Errorf("entry %+v", e)
		}
		got, err = m.apply(updates.AutoOff, nil)
		if err != nil || !slices.Equal(got, []string{"updates.auto/crond-stop", "updates.auto/crond-boot", "updates.auto/apk", "updates.auto/record"}) {
			t.Fatalf("%q, %v", got, err)
		}
		if m.crond || m.apkCron {
			t.Errorf("crond %v, apk-cron %v", m.crond, m.apkCron)
		}
		if _, err := m.f.Lstat(context.Background(), alpine.RunlevelLink); err == nil {
			t.Error("crond still at boot")
		}
	})
	for _, c := range []struct {
		name  string
		host  func(*testing.T) *model
		ids   []string
		notes []updates.Note
	}{
		{"running before hardn", func(t *testing.T) *model {
			m := alpineHost(t).logrotate()
			m.crond = true
			m.f.AddSymlink(alpine.RunlevelLink, alpine.InitScript)
			return m.applied(updates.AutoOn)
		}, []string{"updates.auto/apk", "updates.auto/record"},
			[]updates.Note{{Key: "crond", Value: "Kept", Note: "running before hardn"}, {Key: "Rollback", Value: "Reinstalls from the repositories"}}},
		{"a job added since", func(t *testing.T) *model {
			return alpineHost(t).applied(updates.AutoOn).logrotate()
		}, []string{"updates.auto/apk", "updates.auto/record"},
			[]updates.Note{{Key: "crond", Value: "Kept", Note: "logrotate added since"}, {Key: "Rollback", Value: "Reinstalls from the repositories"}}},
		{"started by hardn, its jobs there before", func(t *testing.T) *model {
			return alpineHost(t).logrotate().applied(updates.AutoOn)
		}, []string{"updates.auto/crond-stop", "updates.auto/crond-boot", "updates.auto/apk", "updates.auto/record"},
			[]updates.Note{{Key: "Rollback", Value: "Reinstalls from the repositories"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := c.host(t)
			notes, err := control(updates.AutoOff).Notes(context.Background(), m.f, engine.Input{Facts: m.facts})
			if err != nil || !slices.Equal(notes, c.notes) {
				t.Errorf("notes %+v, %v", notes, err)
			}
			got, err := m.apply(updates.AutoOff, nil)
			if err != nil || !slices.Equal(got, c.ids) {
				t.Errorf("%q, %v", got, err)
			}
		})
	}
	t.Run("dormant jobs: also starts", func(t *testing.T) {
		m := alpineHost(t).logrotate()
		notes, err := control(updates.AutoOn).Notes(context.Background(), m.f, engine.Input{Facts: m.facts})
		if err != nil || !slices.Equal(notes, []updates.Note{{Key: "Also starts", Value: "logrotate"}}) {
			t.Errorf("notes %+v, %v", notes, err)
		}
	})
}

// TestPlanWritesNothing: planning either direction on each host reads
// only (the commands are queries and simulations).
func TestPlanWritesNothing(t *testing.T) {
	hosts := map[string]func(*testing.T) *model{
		"debian-installer": installer, "debian-off": debianOff, "alpine": alpineHost,
		"by-hardn-debian": func(t *testing.T) *model { return installer(t).applied(updates.AutoOn) },
		"by-hardn-alpine": func(t *testing.T) *model { return alpineHost(t).applied(updates.AutoOn) },
	}
	for name, host := range hosts {
		for _, a := range []updates.AutoAction{updates.AutoOn, updates.AutoOff} {
			m := host(t)
			n := len(m.f.Calls())
			_, _ = plan(t, m, a, nil)
			_, _ = control(a).Notes(context.Background(), m.f, engine.Input{Facts: m.facts})
			if w := writes(m.f, n); len(w) > 0 {
				t.Errorf("%s, action %d: %q", name, a, w)
			}
		}
	}
}

// TestRowAndRead: decisions 7 and 9 on the row, and what Read gives the
// screen.
func TestRowAndRead(t *testing.T) {
	ctx := context.Background()
	if ok, why := (updates.Auto{}).Applies(pve9().facts); ok || why != "updated by hand" {
		t.Errorf("Applies on Proxmox VE: %v %q", ok, why)
	}
	if ok, _ := (updates.Auto{}).Applies(debian13().facts); !ok {
		t.Error("Applies on Debian: false")
	}
	stale := func(m *model) []engine.Finding {
		st, err := (updates.Auto{}).Check(ctx, m.f, m.facts)
		if err != nil {
			t.Fatal(err)
		}
		return st.Findings
	}
	want := []engine.Finding{{Code: updates.CodeChangedSince, Detail: "changed since hardn", Source: hardnFile}}
	m := installer(t).applied(updates.AutoOn)
	m.file(hardnFile, bothLines+"APT::Periodic::Enable \"1\";\n")
	if got := stale(m); !slices.Equal(got, want) {
		t.Errorf("edited: %+v", got)
	}
	r, _ := control(updates.AutoNone).Read(ctx, m.f, m.facts, nil)
	if r.Changed != hardnFile || r.Blocked(true) != "52hardn-auto-upgrades changed" || !slices.Equal(r.Status.Findings, want) {
		t.Errorf("edited: changed %q, blocked %q, findings %+v", r.Changed, r.Blocked(true), r.Status.Findings)
	}
	m = installer(t).applied(updates.AutoOn)
	m.installed = false
	if got := stale(m); len(got) != 1 || got[0].Code != updates.CodeChangedSince {
		t.Errorf("package gone: %+v", got)
	}
	m = aptHost(t, pve9(), "")
	r, _ = control(updates.AutoNone).Read(ctx, m.f, m.facts, nil)
	if r.Status.State != engine.NotApplicable || r.Status.Detail != "updated by hand" || !r.PVE {
		t.Errorf("pve9: %s %q", r.Status.State, r.Status.Detail)
	}
	for _, c := range []struct {
		name    string
		m       *model
		blocked string
		off     bool
	}{
		{"debian13", aptHost(t, debian13(), autoOn), "", false},
		{"enable-off", func() *model {
			m := debianOff(t)
			m.file(confDir+"/99site", "APT::Periodic::Enable \"0\";\n")
			return m
		}(), "Periodic jobs are off", false},
		{"not-read", notRead(t), "Can't read apt's settings", false},
		{"record", unreadableRecord(t), "Can't read hardn's record", true},
		{"no-origin", func() *model { m := debianOff(t); m.file(uuConf, "// No origins.\n"); return m }(), "No origin allowed", false},
	} {
		r, err := control(updates.AutoNone).Read(ctx, c.m.f, c.m.facts, nil)
		if err != nil || r.Blocked(c.off) != c.blocked {
			t.Errorf("%s: blocked %q, want %q (%v)", c.name, r.Blocked(c.off), c.blocked, err)
		}
	}
	m = debianOff(t)
	r, _ = control(updates.AutoNone).Read(ctx, m.f, m.facts, nil)
	if v, file, ok := r.Apt.Value("APT::Periodic::Unattended-Upgrade"); !ok || v != "0" || file != autoUp || r.Package.String() != "yes" || r.Upgrade.Active != "active" || r.On() || r.ByHardn() {
		t.Errorf("debian-off: %q %q %v, package %s, timer %q, on %v", v, file, ok, r.Package, r.Upgrade.Active, r.On())
	}
}

// changeOf is the change id in a fresh plan of a on m.
func changeOf(t *testing.T, m *model, a updates.AutoAction, id string) engine.Change {
	t.Helper()
	cs, err := plan(t, m, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(cs, func(c engine.Change) bool { return c.ID == id })
	if i < 0 {
		t.Fatalf("no %s in %q", id, ids(cs))
	}
	return cs[i]
}

// TestPurgeConffiles: purge only where every dpkg conffile still holds
// the MD5 dpkg lists and no ucf side file exists; otherwise a remove, the
// file kept and said. Every file purge deletes is backed up.
func TestPurgeConffiles(t *testing.T) {
	ctx := context.Background()
	m := installer(t).applied(updates.AutoOn)
	if c := changeOf(t, m, updates.AutoOff, updates.ChangePackage); !slices.Equal(c.Backups, []string{autoUp, uuConf, logrotate}) {
		t.Errorf("purge's backups %q", c.Backups)
	}
	for _, tc := range []struct {
		name string
		edit func(*model)
		note updates.Note
	}{
		{"logrotate edited", func(m *model) { m.file(logrotate, logrotateConf+"# rotate weekly\n") },
			updates.Note{Key: "unattended-upgrades", Value: "Kept", Note: "changed since installed", Warn: true}},
		{"ucf side file", func(m *model) { m.file(autoUp+".ucf-dist", autoOn) },
			updates.Note{Key: "20auto-upgrades.ucf-dist", Value: "Kept", Note: "changed since installed", Warn: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := installer(t).applied(updates.AutoOn)
			tc.edit(m)
			notes, err := control(updates.AutoOff).Notes(ctx, m.f, engine.Input{Facts: m.facts})
			if err != nil || !slices.Contains(notes, tc.note) {
				t.Errorf("notes %+v, %v", notes, err)
			}
			c := changeOf(t, m, updates.AutoOff, updates.ChangePackage)
			if c.StateAfter != "removed, its files kept" || !slices.Equal(c.Commands, []string{"apt-get remove -y unattended-upgrades"}) {
				t.Errorf("%q %q", c.StateAfter, c.Commands)
			}
		})
		t.Run(tc.name+", after the plan", func(t *testing.T) {
			m := installer(t).applied(updates.AutoOn)
			c := changeOf(t, m, updates.AutoOff, updates.ChangePackage)
			tc.edit(m)
			n := len(m.f.Calls())
			if _, err := c.Apply(ctx, m.f, engine.Run{Facts: m.facts}); output.CodeOf(err) != "UPDATES_CHANGED_SINCE" {
				t.Fatalf("apply: %v", err)
			}
			if w := writes(m.f, n); len(w) > 0 || !m.installed {
				t.Errorf("purged anyway: %q", w)
			}
		})
	}
}

// TestApplyRechecks: each off change checks again, just before it runs,
// what its plan read: hardn's file's hash, apk-cron's script's, and
// crond's jobs.
func TestApplyRechecks(t *testing.T) {
	ctx := context.Background()
	t.Run("hardn's file", func(t *testing.T) {
		m := installer(t).applied(updates.AutoOn)
		c := changeOf(t, m, updates.AutoOff, updates.ChangeFile)
		m.file(hardnFile, bothLines+"APT::Periodic::Verbose \"1\";\n")
		_, err := c.Apply(ctx, m.f, engine.Run{Facts: m.facts})
		if output.CodeOf(err) != "UPDATES_CHANGED_SINCE" || !strings.Contains(err.Error(), "52hardn-auto-upgrades changed since hardn wrote it") {
			t.Fatalf("apply: %v", err)
		}
		if _, ok := content(t, m, hardnFile); !ok {
			t.Error("deleted anyway")
		}
	})
	t.Run("apk-cron's script", func(t *testing.T) {
		m := alpineHost(t).applied(updates.AutoOn)
		c := changeOf(t, m, updates.AutoOff, updates.ChangeApk)
		m.f.AddFile(alpine.Script, []byte(apkDaily+"echo done\n"), 0o755, 0, 0)
		_, err := c.Apply(ctx, m.f, engine.Run{Facts: m.facts})
		if output.CodeOf(err) != "UPDATES_CHANGED_SINCE" || !strings.Contains(err.Error(), "/etc/periodic/daily/apk changed since hardn installed it") || !m.apkCron {
			t.Fatalf("apply: %v, apk-cron %v", err, m.apkCron)
		}
	})
	for _, id := range []string{updates.ChangeCrondStop, updates.ChangeCrondBoot} {
		t.Run(id, func(t *testing.T) {
			m := alpineHost(t).applied(updates.AutoOn)
			c := changeOf(t, m, updates.AutoOff, id)
			m.logrotate()
			n := len(m.f.Calls())
			_, err := c.Apply(ctx, m.f, engine.Run{Facts: m.facts})
			var ref *steps.Refusal
			if !errors.As(err, &ref) || ref.Why != "crond: logrotate added since" || output.CodeOf(err) != "UPDATES_CHANGED_SINCE" {
				t.Fatalf("apply: %v", err)
			}
			if w := writes(m.f, n); len(w) > 0 || !m.crond {
				t.Errorf("crond changed anyway: %q", w)
			}
		})
	}
}

// TestRestartedCrond: a crond hardn restarted (marked started, its daemon
// gone) was running before hardn, so off keeps it running, said.
func TestRestartedCrond(t *testing.T) {
	m := alpineHost(t)
	m.crashed = true
	m.applied(updates.AutoOn)
	if e := record(t, m).Entries[0]; !slices.Contains(e.Steps, "restart") {
		t.Fatalf("steps %q", e.Steps)
	}
	notes, err := control(updates.AutoOff).Notes(context.Background(), m.f, engine.Input{Facts: m.facts})
	if err != nil || !slices.Contains(notes, updates.Note{Key: "crond", Value: "Kept", Note: "running before hardn"}) {
		t.Errorf("notes %+v, %v", notes, err)
	}
	got, err := m.apply(updates.AutoOff, nil)
	if err != nil || slices.Contains(got, updates.ChangeCrondStop) || !m.crond {
		t.Errorf("%q, %v, crond %v", got, err, m.crond)
	}
}

// TestStayOnEnableEmpty: Enable "" does not turn apt's jobs off
// (apt.systemd.daily's `[ $AutoAptEnable -eq 0 ]` fails on it), so
// 20auto-upgrades still keeps updates on without hardn's file.
func TestStayOnEnableEmpty(t *testing.T) {
	m := installer(t).applied(updates.AutoOn)
	if err := m.f.Remove(context.Background(), recordPath); err != nil {
		t.Fatal(err)
	}
	m.file(confDir+"/99site", "APT::Periodic::Enable \"\";\n")
	r, err := control(updates.AutoNone).Read(context.Background(), m.f, m.facts, nil)
	if err != nil || r.StayOn != autoUp {
		t.Errorf("stay on %q, %v", r.StayOn, err)
	}
}

// TestApkOffReadBack: off on Alpine reads the setting back after its last
// step, as apt's does: still on fails, rolled back.
func TestApkOffReadBack(t *testing.T) {
	m := alpineHost(t)
	m.crond = true
	m.f.AddSymlink(alpine.RunlevelLink, alpine.InitScript)
	m.applied(updates.AutoOn)
	m.apkKept = true
	_, err := m.apply(updates.AutoOff, nil)
	if output.CodeOf(err) != "UPDATES_NOT_IN_EFFECT" || !strings.Contains(err.Error(), "updates still run after the change (apk-cron)") {
		t.Fatalf("apply: %v", err)
	}
	if !m.apkCron || record(t, m) == nil {
		t.Error("not rolled back")
	}
}
