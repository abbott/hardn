package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	tsctl "github.com/abbott/hardn/pkg/engine/controls/timesync"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/timesync"
	"github.com/abbott/hardn/pkg/host/timesync/timefake"
	"github.com/abbott/hardn/pkg/output"
	tsscreen "github.com/abbott/hardn/pkg/screens/timesync"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Time Sync screen (docs/roadmap.md, "Time sync";
// docs/engine/gallery-timesync.md, adopted 2026-11-14;
// docs/engine/screen-timesync.md): "uigallery timesync" lists it on
// simulated hosts, and "play timesync" runs it. Each drawing is hardn's
// own: the control's reading of a simulated host through the screen's
// mapping (tsscreen.View), and each review the real screen run up to its
// question, its plan the engine's. The hosts' time services are a model
// (pkg/host/timesync/timefake): nothing here ran on Linux.

// tsPrefix names the listing, its goldens and play's hosts.
const tsPrefix = "timesync"

// tsLabel is the source label every mock's note opens with.
const tsLabel = "Proposed: no Figma frame."

// tsMainHost is the host the main menu's 7 leads to in play.
const tsMainHost = tsPrefix + "/off"

// tsMenuItem is the main menu's item (decision 9 (a)).
var tsMenuItem = component.MenuItem{Key: "7", Label: "Time Sync", Description: "Keep the clock in sync"}

// The hosts' hardn.yml, and the files that are not the distribution's.
const (
	tsYML      = "ntpServers:\n  - \"192.0.2.10\"\n  - \"192.0.2.11\"\n"
	tsYMLMoved = "ntpServers:\n  - \"192.0.2.10\"\n  - \"192.0.2.12\"\n"
	tsSite     = timesync.SourcesDir + "/site.sources"
	tsDHCP     = timesync.ChronyDHCP + "/eth0.sources"
	tsVersion  = "4.8-1"
)

// tsReachable are the servers of the gallery's network that answer.
var tsReachable = []string{"192.0.2.10", "192.0.2.12"}

// rocky10Platform is Rocky Linux 10, for the Time Sync screen alone: Debian
// 13's model with Rocky's os-release, as far as detect and the time service
// read it.
var rocky10Platform = func() platform {
	p := debian13Platform
	p.osRelease = "NAME=\"Rocky Linux\"\nVERSION=\"10.2 (Red Quartz)\"\nID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\nVERSION_ID=\"10.2\"\nPRETTY_NAME=\"Rocky Linux 10.2 (Red Quartz)\"\n"
	return p
}()

// stockTime gives h the time service its release comes with
// (gallery-timesync.md §3.1), in sync on its own servers: chrony on Ubuntu
// 26.04, with Ubuntu's pools, on Proxmox VE, with Debian's pool in the main
// file, and on Rocky Linux; busybox ntpd on Alpine, in the default
// runlevel; systemd-timesyncd elsewhere, on the servers built into it.
func (h *fakeHost) stockTime(p platform) {
	file := func(name, data string) { h.f.AddFile(name, []byte(data), 0o644, 0, 0) }
	sim := &timefake.Sim{Reachable: tsReachable, Read: func(name string) ([]byte, error) { return h.f.ReadFile(context.Background(), name) }}
	h.ts = sim
	switch {
	case p.openRC:
		sim.Service, sim.Stock = timesync.BusyboxService, "pool.ntp.org"
		file(timesync.ConfD, timefake.AlpineConfD)
		h.f.AddSymlink(detect.OpenRCRunlevels+"/default/"+timesync.BusyboxService, "/etc/init.d/"+timesync.BusyboxService)
	case strings.Contains(p.osRelease, "ID=\"rocky\""):
		sim.Service, sim.Stock, sim.Packages = "chronyd", "2.rocky.pool.ntp.org", map[string]bool{"chrony": true}
		file(timesync.ChronyConfEL, timefake.ELConf)
	case p.proxmox != "":
		sim.Service, sim.Stock = "chrony", "2.debian.pool.ntp.org"
		h.status.packages["chrony"] = tsVersion
		h.f.AddDir(timesync.SourcesDir, 0o755, 0, 0)
		file(timesync.ChronyConfDebian, timefake.DebianConf)
	case strings.Contains(p.osRelease, "VERSION_ID=\"26.04\""):
		sim.Service, sim.Stock = "chrony", "2.ntp.ubuntu.com"
		h.status.packages["chrony"] = tsVersion
		file(timesync.ChronyConfDebian, timefake.UbuntuConf)
		file(timefake.UbuntuPoolsFile, timefake.UbuntuPools)
	case strings.Contains(p.osRelease, "ID=ubuntu"):
		sim.Service, sim.Stock, sim.Fallback = "systemd-timesyncd", "ntp.ubuntu.com", "ntp.ubuntu.com"
		h.status.packages["systemd-timesyncd"] = "255.4-1ubuntu8"
	default:
		sim.Service, sim.Stock = "systemd-timesyncd", "0.debian.pool.ntp.org"
		sim.Fallback = "0.debian.pool.ntp.org 1.debian.pool.ntp.org 2.debian.pool.ntp.org 3.debian.pool.ntp.org"
		h.status.packages["systemd-timesyncd"] = "257.8-1"
	}
	sim.Synced = sim.Stock
}

// tsOn is a status host of platform p, hardn.yml with yml, or none for "".
func tsOn(p platform, yml string) *fakeHost {
	h := statusOn(p)
	if yml != "" {
		h.withHardnYML(yml)
	}
	return h
}

// tsUbuntu is Ubuntu 26.04, whose time service is chrony.
func tsUbuntu(yml string) *fakeHost { return tsOn(ubuntu2604Platform, yml) }

// tsSet is h with hardn's servers set as Set servers leaves an Ubuntu
// 26.04 host: hardn's own file, Ubuntu's pools switched off, and the clock
// on the first server that answers.
func tsSet(h *fakeHost, servers ...string) *fakeHost {
	own, err := timesync.SourcesFile(servers)
	if err != nil {
		panic(err) // the gallery's own servers
	}
	off, err := timesync.ChronyOff(timefake.UbuntuPoolsFile, []byte(timefake.UbuntuPools), nil)
	if err != nil {
		panic(err)
	}
	h.f.AddFile(timesync.ChronySources, own, 0o644, 0, 0)
	h.f.AddFile(timefake.UbuntuPoolsFile, off, 0o644, 0, 0)
	h.ts.Synced = ""
	if slices.Contains(tsReachable, servers[0]) {
		h.ts.Synced = servers[0]
	}
	return h
}

// tsHost is one simulated host the Time Sync screen runs on.
type tsHost struct {
	name, label, about, note string
	host                     func() *fakeHost
}

// tsHosts are the hosts, in the listing's and the menu's order: the mock's
// (gallery-timesync.md §2).
func tsHosts() []tsHost {
	const u2604 = hostFQDN + " runs Ubuntu 26.04, whose time service is chrony. "
	with := func(h *fakeHost, edit func(*fakeHost)) *fakeHost { edit(h); return h }
	return []tsHost{
		{name: "default", label: "Ubuntu 26.04", about: "hardn.yml names none", host: func() *fakeHost { return tsUbuntu("") },
			note: u2604 + "The clock is in sync on Ubuntu's own pools, and hardn.yml names no servers: the item stays and says what is missing."},
		{name: "off", label: "Servers named", about: "Set servers", host: func() *fakeHost { return tsUbuntu(tsYML) },
			note: u2604 + "The same host, and hardn.yml names 192.0.2.10 and 192.0.2.11: Set servers, its description the servers."},
		{name: "hardn", label: "Set by hardn", about: "In sync", host: func() *fakeHost { return tsSet(tsUbuntu(tsYML), "192.0.2.10", "192.0.2.11") },
			note: u2604 + "hardn's file names the two servers, Ubuntu's pools are switched off, and the clock follows the first. The one item undoes it."},
		{name: "not-synced", label: "Not in sync", about: "Partial", host: func() *fakeHost {
			return with(tsSet(tsUbuntu(tsYML), "192.0.2.10", "192.0.2.11"), func(h *fakeHost) { h.ts.Synced = "" })
		}, note: u2604 + "hardn's servers are set and neither answers: the clock is not in sync, and the row is Partial."},
		{name: "stopped", label: "Not running", about: "Unavailable", host: func() *fakeHost { return with(tsUbuntu(tsYML), func(h *fakeHost) { h.ts.Stopped = true }) },
			note: u2604 + "chrony is installed and not running: a warning, and Set servers is unavailable. hardn starts no time service."},
		{name: "no-service", label: "No time service", about: "Unavailable", host: func() *fakeHost {
			return with(tsOn(debian13Platform, tsYML), func(h *fakeHost) { delete(h.status.packages, "systemd-timesyncd"); h.ts.Service, h.ts.Synced = "", "" })
		}, note: hostFQDN + " runs Debian 13 with no time service: a warning, and Set servers is unavailable. hardn installs none."},
		{name: "foreign", label: "Set outside hardn", about: "Unavailable", host: func() *fakeHost {
			return with(tsUbuntu(tsYML), func(h *fakeHost) {
				h.f.AddFile(tsSite, []byte("# the site's own\nserver 192.0.2.77 iburst\n"), 0o644, 0, 0)
				h.ts.Synced = "192.0.2.77"
			})
		}, note: u2604 + "site.sources, not hardn's, names 192.0.2.77: the row says where it is set, and Set servers is unavailable."},
		{name: "dhcp", label: "A server from DHCP", about: "Left alone", host: func() *fakeHost {
			return with(tsUbuntu(tsYML), func(h *fakeHost) {
				h.f.AddFile(tsDHCP, []byte("server 192.0.2.1 iburst\n"), 0o644, 0, 0)
				h.ts.Synced = "192.0.2.1"
			})
		}, note: u2604 + "The DHCP lease handed out 192.0.2.1, which chrony uses beside the pools. Set servers stays offered: it leaves DHCP's server."},
		{name: "mismatch", label: "Other servers", about: "hardn.yml differs", host: func() *fakeHost { return tsSet(tsUbuntu(tsYMLMoved), "192.0.2.10", "192.0.2.11") },
			note: u2604 + "hardn's file names 192.0.2.10 and 192.0.2.11, and hardn.yml now names 192.0.2.12 second: the hardn.yml row warns, Set servers rewrites the file, Remove servers is item 2."},
		{name: "timesyncd", label: "Ubuntu 24.04", about: "systemd-timesyncd", host: func() *fakeHost { return tsOn(ubuntu2404Platform, tsYML) },
			note: hostFQDN + " runs Ubuntu 24.04, whose time service is systemd-timesyncd, in sync on the server built into it."},
		{name: "alpine", label: "Alpine 3.23", about: "busybox ntpd", host: func() *fakeHost { return tsOn(alpine323Platform, tsYML) },
			note: hostFQDN + " runs Alpine 3.23 with busybox ntpd. It does not say which server it follows, so there is no Synced to row; the Clock row is the kernel's word."},
		{name: "container", label: "Container", about: "The host keeps the time", host: func() *fakeHost { return container().withHardnYML(tsYML) },
			note: hostFQDN + " is a container: its clock is its host's, no time service is looked for, and hardn sets nothing."},
		{name: "not-read", label: "Not read", about: "A file hardn can't make out", host: func() *fakeHost {
			return with(tsUbuntu(tsYML), func(h *fakeHost) { h.f.AddFile(tsSite, []byte("server\n"), 0o644, 0, 0) })
		}, note: u2604 + "A line of site.sources names no server: the row and the servers are Unknown, and the item refuses with words."},
	}
}

// tsVariants are hosts play opens by name ("timesync/<host>") and the
// reviews are drawn on, which the menu does not list.
func tsVariants() []tsHost {
	return []tsHost{
		{name: "rocky", host: func() *fakeHost { return tsOn(rocky10Platform, tsYML) },
			note: hostFQDN + " runs Rocky Linux 10, whose chrony reads no directory of the administrator's."},
		{name: "debian-chrony", host: func() *fakeHost { return tsOn(pve9Platform, tsYML) },
			note: hostFQDN + " is a Proxmox VE 9 node: Debian's chrony, its pool in the main file."},
	}
}

func findTSHost(name string) (tsHost, bool) {
	all := slices.Concat(tsHosts(), tsVariants())
	i := slices.IndexFunc(all, func(h tsHost) bool { return h.name == name })
	if i < 0 {
		return tsHost{}, false
	}
	return all[i], true
}

// tsDraw draws the screen on a host as hardn draws it: the control's
// reading through the screen's own mapping (tsscreen.View).
func tsDraw(th tsHost) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		sys := th.host().sys()
		facts, err := detect.Detect(ctx, sys)
		if err != nil {
			return "", err
		}
		cfg, err := statusConfig(ctx, sys)
		if err != nil {
			return "", err
		}
		return tsscreen.View(tsctl.Read(ctx, sys, facts, cfg)).SubMenu().Render(c), nil
	}
}

// tsReview runs the real screen on a host with key typed and nothing after
// it, and returns what it drew after the screen itself (reviewOf): the
// item's refusal, or its review, the engine's plan and the y/N question,
// left unanswered, so nothing is applied.
func tsReview(th tsHost, key string) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		h := th.host()
		sys := h.sys()
		facts, err := detect.Detect(ctx, sys)
		if err != nil {
			return "", err
		}
		cfg, err := statusConfig(ctx, sys)
		if err != nil {
			return "", err
		}
		first := tsscreen.View(tsctl.Read(ctx, th.host().sys(), facts, cfg)).SubMenu().Render(c)
		return reviewOf(c, key, first, func(p nav.Prompter) error {
			return tsscreen.Run(ctx, tsscreen.Options{
				Prompter: p, System: sys, Facts: facts, Config: cfg, LogPath: output.LegacyLogPath, Now: clock,
				NewEngine: func(progress func(engine.Event)) *engine.Engine { return updEngine(h, progress, &updQuiet) },
			})
		})
	}
}

// tsMocks are the listing's mocks: each host's screen, then each change's
// review up to its question, and two refusals.
func tsMocks() []listMock {
	var out []listMock
	for _, th := range tsHosts() {
		out = append(out, listMock{name: th.name, note: th.note, draw: tsDraw(th)})
	}
	host := func(name string) tsHost {
		th, ok := findTSHost(name)
		if !ok {
			panic("timesync: no host " + name)
		}
		return th
	}
	return append(out,
		listMock{name: "review", note: "off's Set servers, up to its question: hardn's file, Ubuntu's five pools commented out under hardn's mark, and chrony told to read both again. No restart. The notice says Ubuntu's own servers are authenticated. y/N: it cannot lock anyone out.",
			draw: tsReview(host("off"), screen.TimeSyncFirstKey)},
		listMock{name: "review-rhel", note: "Set servers on Rocky Linux 10, whose chrony reads no directory of the administrator's: in /etc/chrony.conf the pool commented out and two lines added under a comment naming them, then a restart.",
			draw: tsReview(host("rocky"), screen.TimeSyncFirstKey)},
		listMock{name: "review-pve", note: "Set servers on a Proxmox VE 9 node: hardn's file, Debian's pool commented out in chrony's main file, a restart, and a line about the cluster.",
			draw: tsReview(host("debian-chrony"), screen.TimeSyncFirstKey)},
		listMock{name: "review-timesyncd", note: "timesyncd's Set servers: a drop-in with NTP=, which systemd-timesyncd uses in place of the servers built into it, and a restart.",
			draw: tsReview(host("timesyncd"), screen.TimeSyncFirstKey)},
		listMock{name: "review-alpine", note: "alpine's Set servers: no drop-in, hardn's words in /etc/conf.d/ntpd under a comment naming the ones they replace, and the restart that reads them.",
			draw: tsReview(host("alpine"), screen.TimeSyncFirstKey)},
		listMock{name: "review-remove", note: "hardn's Remove servers: hardn's file deleted, Ubuntu's pools as they were, and chrony told to read again.",
			draw: tsReview(host("hardn"), screen.TimeSyncFirstKey)},
		listMock{name: "refuse-no-servers", note: "default's Set servers: refused with words, before anything is asked.",
			draw: tsReview(host("default"), screen.TimeSyncFirstKey)},
		listMock{name: "refuse-not-read", note: "not-read's Set servers: refused, naming the file and the line.",
			draw: tsReview(host("not-read"), screen.TimeSyncFirstKey)},
	)
}

// tsUsage is the listing's usage line.
const tsUsage = "usage: go run ./internal/tools/uigallery timesync [flags] [mock | all]..."

// tsListing is the time sync listing.
var tsListing = listing{prefix: tsPrefix, usage: tsUsage, label: tsLabel, mocks: tsMocks}

// openTimeSync returns the action that opens what name names: the hosts'
// menu for tsPrefix alone, the screen on one host for "timesync/<host>".
func (s *session) openTimeSync(name string) func() error {
	if th, ok := findTSHost(strings.TrimPrefix(name, tsPrefix+"/")); ok && name != tsPrefix {
		return func() error { return s.tsScreen(context.Background(), th, th.host()) }
	}
	return func() error {
		exit := component.MenuItem{Key: "0", Label: "Return"}
		vm := screen.SubMenu{Title: component.TitleBar{Title: "Time Sync hosts"}, Heading: "Select an option", Prompt: "Option", Menu: component.Menu{Exit: exit}}
		menu := nav.Menu{Exit: exit}
		for i, th := range tsHosts() {
			it := component.MenuItem{Key: fmt.Sprint(i + 1), Label: th.label, Description: th.about}
			vm.Menu.Items = append(vm.Menu.Items, it)
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.tsScreen(context.Background(), th, th.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// tsScreen runs the Time Sync screen on h, th's host, until the user goes
// back from it, as lfScreen runs the Log Forwarding screen.
func (s *session) tsScreen(ctx context.Context, th tsHost, h *fakeHost) error {
	sys, title := h.sys(), tsPrefix+"/"+th.name
	facts, err := detect.Detect(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	cfg, err := statusConfig(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	if err := s.writeNote(title, th.note); err != nil {
		return err
	}
	var rec *recorder
	return tsscreen.Run(ctx, tsscreen.Options{
		Prompter: s.p, System: sys, Facts: facts, Config: cfg, DryRun: s.sequence == "dry-run", LogPath: output.LegacyLogPath, Now: clock,
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

// tsStart returns the hosts' menu, or the host, that args name, and
// whether they name one; an error for a host that is not one.
func tsStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != tsPrefix && !strings.HasPrefix(args[0], tsPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], tsPrefix+"/"); args[0] != tsPrefix {
		if _, ok := findTSHost(name); !ok {
			var names []string
			for _, h := range tsHosts() {
				names = append(names, tsPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no timesync host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}
