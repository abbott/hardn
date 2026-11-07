package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	updscreen "github.com/abbott/hardn/pkg/screens/updates"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Auto Updates screen on the simulated hosts (refactor plan slice 7,
// unit (c), next/s7-screen): hardn's Auto Updates screen itself
// (pkg/screens/updates), drawn from what the Auto Updates control reads of
// each host (updateshost.go), each change on the real engine, on the hosts
// the Auto Updates mock was drawn for, which it replaces
// (docs/engine/screen-updates.md). The main menu's 5 leads to it on Debian
// 13; "play updates" is the menu of the hosts and "play updates/<host>"
// one. The listing of the mock, "uigallery updates", was retired with it:
// git keeps it (updates.go, next/s7-control), and gallery-updates.md
// records what each showed.

// updPrefix names the Auto Updates screen's hosts in play: "updates" is
// the menu of them and "updates/<host>" one.
const updPrefix = "updates"

// updMainHost is the host the main menu's 5 leads to in play.
const updMainHost = updPrefix + "/debian13"

// updMenuItem is play's main menu's item for it (decision 1): key 5, named
// as the row is, before Run All; unit (d) wires hardn's.
var updMenuItem = component.MenuItem{Key: "5", Label: "Auto Updates", Description: "Configure automatic updates"}

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

// updHost is one simulated host the Auto Updates screen runs on.
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

// updVariants are hosts play opens by name ("updates/<host>") and the
// tests read that the menu does not list.
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

// updAuto is the real control doing a, its record on the gallery's clock.
func updAuto(a updates.AutoAction) updates.Auto {
	return updates.Auto{Action: a, Records: revert.Options{Now: clock, Hardn: galleryHardn}, Version: updVersion}
}

// updVersion is the hardn version the gallery's records carry.
const updVersion = "v0.4.0"

// openUpdates returns the action that opens what name names: the hosts'
// menu for updPrefix alone, the screen on one host for "updates/<host>".
func (s *session) openUpdates(name string) func() error {
	if uh, ok := findUpdHost(strings.TrimPrefix(name, updPrefix+"/")); ok && name != updPrefix {
		return func() error { return s.updScreen(context.Background(), uh, uh.host()) }
	}
	return func() error {
		exit := component.MenuItem{Key: "0", Label: "Return"}
		vm := screen.SubMenu{Title: component.TitleBar{Title: "Auto Updates hosts"}, Heading: "Select a host", Prompt: "Enter your choice", Menu: component.Menu{Exit: exit}}
		menu := nav.Menu{Exit: exit}
		for i, uh := range updHosts() {
			it := component.MenuItem{Key: fmt.Sprint(i + 1), Label: uh.label, Description: uh.about}
			vm.Menu.Items = append(vm.Menu.Items, it)
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.updScreen(context.Background(), uh, uh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// updScreen runs the Auto Updates screen on h, uh's host, until the user
// goes back from it: its note above it; the facts detect's; hardn.yml the
// host's, or the defaults where it has none (statusConfig); the engine the
// gallery's over the host (updEngine) with the session's hold, its apply
// recorded for the end of play: a change's engine alone, kept once its flow
// has ended (Ran), not the one the item's plan before asking makes, which
// Ran never ends; the control's record on the gallery's clock. A dry run
// when play plays the dry-run sequence.
func (s *session) updScreen(ctx context.Context, uh updHost, h *fakeHost) error {
	sys, title := h.sys(), updPrefix+"/"+uh.name
	facts, err := detect.Detect(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	cfg, err := statusConfig(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	if err := s.writeNote(title, uh.note); err != nil {
		return err
	}
	var rec *recorder
	return updscreen.Run(ctx, updscreen.Options{
		Prompter: s.p, System: sys, Facts: facts, Config: cfg, DryRun: s.sequence == "dry-run",
		LogPath: output.LegacyLogPath, Now: clock, Version: updVersion,
		Records: revert.Options{Now: clock, Hardn: galleryHardn},
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			r := &recorder{host: h, f: h.f, title: title}
			rec = r
			r.begin()
			return updEngine(h, func(ev engine.Event) { r.event(ev); progress(ev) }, s)
		},
		Ran: func(run output.Run) {
			rec.end()
			rec.setPlan(run.Plan)
			rec.failedWith(run.Err)
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, rec)
			s.mu.Unlock()
		},
	})
}

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
