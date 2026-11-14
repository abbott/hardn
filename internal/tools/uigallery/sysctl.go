package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	ksctl "github.com/abbott/hardn/pkg/engine/controls/kernelsettings"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sysctl"
	"github.com/abbott/hardn/pkg/host/sysctl/sysctlfake"
	"github.com/abbott/hardn/pkg/output"
	ksscreen "github.com/abbott/hardn/pkg/screens/kernelsettings"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Kernel Settings screen (docs/roadmap.md, "sysctl";
// docs/engine/gallery-sysctl.md, adopted 2026-11-14,
// built in its §9): "uigallery sysctl" lists it on simulated
// hosts, and "play sysctl" runs it. Each drawing is hardn's own: the
// control's reading of a simulated host through the screen's mapping
// (ksscreen.View), and each review the real screen run up to its question,
// its plan the engine's. The hosts' kernels and their sysctl tools are a
// model (pkg/host/sysctl/sysctlfake), their values the ones read on hosts
// on 2026-11-14 (gallery-sysctl.md §3): nothing here ran on Linux.

// ksPrefix names the listing, its goldens and play's hosts.
const ksPrefix = "sysctl"

// ksLabel is the source label every mock's note opens with.
const ksLabel = "Proposed: no Figma frame."

// ksMainHost is the host the Configuration group's 4 leads to in play (main
// menu 8, then 4; config.go).
const ksMainHost = ksPrefix + "/default"

// The hosts' values over the model's baseline, as read on 2026-11-14:
// every interface's own value among them (F1).
var (
	// ksRocky: rocky10-test-110's all and default, and the interfaces as
	// centos10-test-120 and oracle9-test-117 read theirs.
	ksRocky = map[string]int{"net.ipv4.conf.*.accept_redirects": 1, "net.ipv6.conf.*.accept_redirects": 1,
		"net.ipv4.conf.all.rp_filter": 0, sysctl.PtraceKey: 0}
	// ksDebian: deb13-vm. eth0 accepts IPv6 redirects though all and
	// default do not.
	ksDebian = map[string]int{"net.ipv4.conf.lo.accept_redirects": 1, "net.ipv6.conf.eth0.accept_redirects": 1, "net.ipv6.conf.lo.accept_redirects": 1,
		"net.ipv4.conf.*.rp_filter": 2, "net.ipv4.conf.all.rp_filter": 0,
		"kernel.kptr_restrict": 0, sysctl.PtraceKey: 0, "kernel.perf_event_paranoid": 3, "fs.protected_regular": 2}
	// ksAlpine: alpine323-vm.
	ksAlpine = map[string]int{"net.ipv4.conf.eth0.accept_redirects": 1, "net.ipv4.conf.lo.accept_redirects": 1,
		"net.ipv6.conf.eth0.accept_redirects": 1, "net.ipv6.conf.lo.accept_redirects": 1,
		"kernel.kptr_restrict": 0, "kernel.unprivileged_bpf_disabled": 1, "fs.protected_fifos": 0, "fs.protected_regular": 0}
)

// ksOn is a status host of platform p with the kernel values over on its
// interfaces, and its release's sysctl: procps's, or busybox's on Alpine,
// the sysctl service in the boot runlevel where boot.
func ksOn(p platform, ifaces []string, boot bool, over ...map[string]int) *fakeHost {
	h := statusOn(p)
	return h.kernel(p, ifaces, boot, over...)
}

// kernel gives h its kernel values and its sysctl tool (ksOn).
func (h *fakeHost) kernel(p platform, ifaces []string, boot bool, over ...map[string]int) *fakeHost {
	all := map[string]int{}
	for _, m := range over {
		for k, v := range m {
			all[k] = v
		}
	}
	for name, data := range sysctlfake.Files(sysctlfake.Values(ifaces, all)) {
		h.f.AddFile(name, []byte(data), 0o644, 0, 0)
	}
	h.f.AddDir("/etc/sysctl.d", 0o755, 0, 0)
	h.ks = &sysctlfake.Sim{Sys: h.f, Busybox: p.openRC}
	if !p.openRC {
		h.f.AddFile(sysctlfake.ProcpsPath, nil, 0o755, 0, 0)
		return h
	}
	h.f.AddFile(sysctlfake.BusyboxPath, nil, 0o755, 0, 0)
	h.f.AddSymlink(sysctlfake.BusyboxLink, sysctlfake.BusyboxPath)
	if boot {
		h.f.AddFile("/etc/init.d/"+sysctl.Service, nil, 0o755, 0, 0)
		h.f.AddSymlink(detect.OpenRCRunlevels+"/boot/"+sysctl.Service, "/etc/init.d/"+sysctl.Service)
	}
	return h
}

// ksApplied is h after Apply settings: hardn's file as the control would
// write it, and its values set by the host's own sysctl.
func ksApplied(h *fakeHost) *fakeHost {
	ctx := context.Background()
	sys := h.sys()
	facts, err := detect.Detect(ctx, sys)
	if err != nil {
		panic(err) // the gallery's own host
	}
	h.f.AddFile(sysctl.File, ksctl.Read(ctx, sys, facts).Next().Bytes(), 0o644, 0, 0)
	if r, ok := h.ks.Answer([]string{sysctl.Command, "-p", sysctl.File}); !ok || r.ExitCode != 0 || r.Stderr != "" {
		panic("sysctl: the model's own file is refused: " + r.Stderr)
	}
	return h
}

// ksHost is one simulated host the Kernel Settings screen runs on.
type ksHost struct {
	name, label, about, note string
	host                     func() *fakeHost
}

// ksHosts are the hosts, in the listing's and the menu's order
// (gallery-sysctl.md §2).
func ksHosts() []ksHost {
	eth := []string{"eth0"}
	rocky := func() *fakeHost { return ksOn(rocky10Platform, eth, true, ksRocky) }
	alpine := func(boot bool) *fakeHost { return ksOn(alpine323Platform, eth, boot, ksAlpine) }
	with := func(h *fakeHost, edit func(*fakeHost)) *fakeHost { edit(h); return h }
	return []ksHost{
		{name: "default", label: "Rocky Linux 10", about: "4 to set", host: rocky,
			note: hostFQDN + " runs Rocky Linux 10, its values rocky10-test-110's of 2026-11-14: four rows are below the baseline, each with its weakest value, what that means and what hardn sets. Forwarding is shown and never set."},
		{name: "configured", label: "Set by hardn", about: "Configured", host: func() *fakeHost { return ksApplied(rocky()) },
			note: hostFQDN + " after Apply settings: every row at the baseline, hardn's file named, and the one item undoes it."},
		{name: "overridden", label: "Set back by a later file", about: "Partial", host: func() *fakeHost {
			return with(ksApplied(rocky()), func(h *fakeHost) {
				h.f.AddFile("/etc/sysctl.d/99-sysctl.conf", []byte("# the administrator's own\nnet.ipv4.conf.all.accept_redirects = 1\n"), 0o644, 0, 0)
				h.f.AddFile(sysctl.ProcPath("net.ipv4.conf.all.accept_redirects"), []byte("1\n"), 0o644, 0, 0)
			})
		}, note: hostFQDN + " after Apply settings and a boot: 99-sysctl.conf, read after hardn's file, sets ICMP redirects back. The row names the file, and hardn does not edit it."},
		{name: "routes", label: "A host that routes", about: "Two rows left alone", host: func() *fakeHost {
			return ksOn(debian13Platform, []string{"docker0", "eth0"}, true, ksDebian, map[string]int{"net.ipv4.ip_forward": 1, "net.ipv4.conf.*.forwarding": 1})
		}, note: hostFQDN + " runs Debian 13 and Docker, which turned forwarding on: the host routes, so Sending redirects and Reverse-path filter are left alone, and say why. The other rows are set as on any host. Its values are deb13-vm's: eth0 accepts IPv6 redirects though all and default do not."},
		{name: "pve", label: "Proxmox VE 9", about: "Two rows left alone", host: func() *fakeHost { return ksOn(pve9Platform, []string{"eno1", "vmbr0"}, true, ksDebian) },
			note: hostFQDN + " is a Proxmox VE 9 node, forwarding off today: it may route for its guests at any time, so Sending redirects and Reverse-path filter are left alone. No Proxmox VE node was read: the values are Debian 13's."},
		{name: "container", label: "Container", about: "Not Applicable", host: func() *fakeHost { return container().kernel(debian13Platform, eth, true, ksDebian) },
			note: hostFQDN + " is a container: Kernel and Files rows are its host's, Network rows its own. All are shown with no verdict, and hardn sets nothing."},
		{name: "alpine", label: "Alpine 3.23", about: "busybox sysctl", host: func() *fakeHost { return alpine(true) },
			note: hostFQDN + " runs Alpine 3.23, its values alpine323-vm's of 2026-11-14: six rows below the baseline, eth0's redirects among them."},
		{name: "alpine-not-at-boot", label: "Alpine, service off", about: "Partial", host: func() *fakeHost { return ksApplied(alpine(false)) },
			note: hostFQDN + " runs Alpine with the sysctl service in no runlevel, after Apply settings: the values are set now, and no file in /etc/sysctl.d is applied at boot. A warning, the command that fixes it, and Partial. hardn does not enable the service."},
	}
}

// ksVariants are hosts play opens by name ("sysctl/<host>") and the reviews
// are drawn on, which the menu does not list.
func ksVariants() []ksHost {
	return []ksHost{
		{name: "alpine-service-off", host: func() *fakeHost { return ksOn(alpine323Platform, []string{"eth0"}, false, ksAlpine) },
			note: hostFQDN + " runs Alpine 3.23 with the sysctl service in no runlevel, before Apply settings."},
		{name: "foreign", host: func() *fakeHost {
			h := ksOn(rocky10Platform, []string{"eth0"}, true, ksRocky)
			h.f.AddFile(sysctl.File, []byte("# the administrator's own\nkernel.sysrq = 0\n"), 0o644, 0, 0)
			return h
		}, note: hostFQDN + " has a file at hardn's path that hardn did not write."},
	}
}

func findKSHost(name string) (ksHost, bool) {
	all := slices.Concat(ksHosts(), ksVariants())
	i := slices.IndexFunc(all, func(h ksHost) bool { return h.name == name })
	if i < 0 {
		return ksHost{}, false
	}
	return all[i], true
}

// ksDraw draws the screen on a host as hardn draws it: the control's
// reading through the screen's own mapping (ksscreen.View).
func ksDraw(kh ksHost) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		sys := kh.host().sys()
		facts, err := detect.Detect(ctx, sys)
		if err != nil {
			return "", err
		}
		return ksscreen.View(ksctl.Read(ctx, sys, facts)).SubMenu().Render(c), nil
	}
}

// ksReview runs the real screen on a host with key typed and nothing after
// it, and returns what it drew after the screen itself (reviewOf): the
// item's refusal, or its review, the engine's plan and the y/N question,
// left unanswered, so nothing is applied.
func ksReview(kh ksHost, key string) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		h := kh.host()
		sys := h.sys()
		facts, err := detect.Detect(ctx, sys)
		if err != nil {
			return "", err
		}
		cfg, err := statusConfig(ctx, sys)
		if err != nil {
			return "", err
		}
		first := ksscreen.View(ksctl.Read(ctx, kh.host().sys(), facts)).SubMenu().Render(c)
		return reviewOf(c, key, first, func(p nav.Prompter) error {
			return ksscreen.Run(ctx, ksscreen.Options{
				Prompter: p, System: sys, Facts: facts, Config: cfg, LogPath: output.LegacyLogPath, Now: clock,
				NewEngine: func(progress func(engine.Event)) *engine.Engine { return updEngine(h, progress, &updQuiet) },
			})
		})
	}
}

// ksGroup is the Configuration group, Kernel Settings its item 4.
func ksGroup(_ context.Context, c component.Ctx) (string, error) {
	return screen.ConfigGroup{}.SubMenu().Render(c), nil
}

// ksMocks are the listing's mocks: the group's menu, each host's screen,
// then each change's review up to its question, and a refusal.
func ksMocks() []listMock {
	out := []listMock{{name: "menu", note: "Main menu 8, the Configuration group, with Kernel Settings as item 4.", draw: ksGroup}}
	for _, kh := range ksHosts() {
		out = append(out, listMock{name: kh.name, note: kh.note, draw: ksDraw(kh)})
	}
	host := func(name string) ksHost {
		kh, ok := findKSHost(name)
		if !ok {
			panic("sysctl: no host " + name)
		}
		return kh
	}
	return append(out,
		listMock{name: "review", note: "default's Apply settings, up to its question: the rows as they will be, hardn's file, one line a row with * for every interface under each key's earlier value, and the one command that sets them. y/N: none of these can cut off SSH.",
			draw: ksReview(host("default"), screen.KernelFirstKey)},
		listMock{name: "review-routes", note: "routes's Apply settings: four rows, and the notice says what is left alone.",
			draw: ksReview(host("routes"), screen.KernelFirstKey)},
		listMock{name: "review-alpine", note: "alpine's Apply settings: busybox sysctl reads no *, so the file names each interface that is below the baseline.",
			draw: ksReview(host("alpine"), screen.KernelFirstKey)},
		listMock{name: "review-alpine-service-off", note: "Apply settings on Alpine with the sysctl service in no runlevel: the same change, and the notice says the values are not set at boot, with the command that fixes it.",
			draw: ksReview(host("alpine-service-off"), screen.KernelFirstKey)},
		listMock{name: "review-remove", note: "configured's Remove settings: hardn's file deleted, and each value set back to the one the file recorded, every interface's own.",
			draw: ksReview(host("configured"), screen.KernelFirstKey)},
		listMock{name: "refuse-file", note: "Apply settings where a file that is not hardn's is at hardn's path: refused with words, before anything is asked.",
			draw: ksReview(host("foreign"), screen.KernelFirstKey)},
	)
}

// ksUsage is the listing's usage line.
const ksUsage = "usage: go run ./internal/tools/uigallery sysctl [flags] [mock | all]..."

// ksListing is the kernel settings listing.
var ksListing = listing{prefix: ksPrefix, usage: ksUsage, label: ksLabel, mocks: ksMocks}

// openKernel returns the action that opens what name names: the hosts'
// menu for ksPrefix alone, the screen on one host for "sysctl/<host>".
func (s *session) openKernel(name string) func() error {
	if kh, ok := findKSHost(strings.TrimPrefix(name, ksPrefix+"/")); ok && name != ksPrefix {
		return func() error { return s.ksScreen(context.Background(), kh, kh.host()) }
	}
	return func() error {
		exit := component.MenuItem{Key: "0", Label: "Return"}
		vm := screen.SubMenu{Title: component.TitleBar{Title: "Kernel Settings hosts"}, Heading: "Select an option", Prompt: "Option", Menu: component.Menu{Exit: exit}}
		menu := nav.Menu{Exit: exit}
		for i, kh := range ksHosts() {
			it := component.MenuItem{Key: fmt.Sprint(i + 1), Label: kh.label, Description: kh.about}
			vm.Menu.Items = append(vm.Menu.Items, it)
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.ksScreen(context.Background(), kh, kh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// ksScreen runs the Kernel Settings screen on h, kh's host, until the user
// goes back from it, as tsScreen runs the Time Sync screen.
func (s *session) ksScreen(ctx context.Context, kh ksHost, h *fakeHost) error {
	sys, title := h.sys(), ksPrefix+"/"+kh.name
	facts, err := detect.Detect(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	cfg, err := statusConfig(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	if err := s.writeNote(title, kh.note); err != nil {
		return err
	}
	var rec *recorder
	return ksscreen.Run(ctx, ksscreen.Options{
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

// ksStart returns the hosts' menu, or the host, that args name, and
// whether they name one; an error for a host that is not one.
func ksStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != ksPrefix && !strings.HasPrefix(args[0], ksPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], ksPrefix+"/"); args[0] != ksPrefix {
		if _, ok := findKSHost(name); !ok {
			var names []string
			for _, h := range ksHosts() {
				names = append(names, ksPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no sysctl host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}

// ksAnswer answers the sysctl tool's commands on a host with a kernel
// model; ok is false on any other host, and for any other command.
func (h *fakeHost) ksAnswer(argv []string) (hostfake.Response, bool) {
	if h.ks == nil {
		return hostfake.Response{}, false
	}
	r, ok := h.ks.Answer(argv)
	return hostfake.Response{Stdout: r.Stdout, Stderr: r.Stderr, ExitCode: r.ExitCode}, ok
}
