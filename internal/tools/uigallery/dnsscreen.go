package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	dnsscreen "github.com/abbott/hardn/pkg/screens/dns"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The DNS screen on the simulated hosts (refactor plan slice 2, unit (c),
// next/s2-screen): hardn's DNS screen itself (pkg/screens/dns), drawn
// from each host's resolver model (resolver.go) and hardn.yml through the
// DNS control, every change on the real engine, on the hosts the DNS mocks
// were drawn for, which it replaces (docs/engine/gallery-dns.md §11). The
// main menu's 4 leads to it on Debian 13, as it led to the mock; "play
// dns" is the menu of the hosts and "play dns/<host>" one.

// dnsPrefix names the DNS screen's hosts in play: "dns" is the menu of
// them and "dns/<host>" one.
const dnsPrefix = "dns"

// dnsMainHost is the host the main menu's 4 leads to: Debian 13, as the
// mock's main host was.
const dnsMainHost = dnsPrefix + "/debian13"

// dnsLinks lead from a menu mock's item to the DNS screen on a host, by
// the mock's name and the item's key, beside links.
var dnsLinks = map[string]string{mainMenuMock + "/4": dnsMainHost}

// dnsScreenHost is one simulated host the DNS screen runs on.
type dnsScreenHost struct {
	// name is the host's, after dnsPrefix: "debian13".
	name string
	// label and about are its item in the menu of the hosts.
	label, about string
	// note says what the host is, and what its items do there.
	note string
	// host returns the simulated host, fresh, with its hardn.yml.
	host func() *fakeHost
}

// dnsScreenHosts are the hosts, in the menu's order: the DNS mocks' eight
// (Debian 13, Ubuntu 26.04, Alpine 3.24, Proxmox VE 9, NetworkManager,
// hardn.yml listing none or one, a host configured already), then the two
// failures of the engine's DNS scenarios: a reload without effect (D3)
// and a save refused.
func dnsScreenHosts() []dnsScreenHost {
	const (
		listed = " hardn.yml lists 192.0.2.54, 2001:db8::54."
		file   = "/etc/resolv.conf is a file dhcpcd wrote from eth0's lease, naming 192.0.2.1 and example.net."
		stub   = "/etc/resolv.conf links to systemd-resolved's stub, and eth0 has 192.0.2.1 and example.net from DHCP."
		each   = " Each item is a change on the real engine, confirmed y/N; then the screen is read again."
	)
	debian := hostFQDN + " runs Debian 13; " + file
	return []dnsScreenHost{
		{name: "debian13", label: "Debian 13", about: "A file dhcpcd wrote", host: dnsHost(debian13Platform), note: debian + listed + each},
		{name: "ubuntu2604", label: "Ubuntu 26.04", about: "systemd-resolved", host: dnsHost(ubuntu2604Platform),
			note: hostFQDN + " runs Ubuntu 26.04; " + stub + listed + each},
		{name: "alpine324", label: "Alpine 3.24", about: "udhcpc rewrites the file", host: dnsHost(alpine324Platform),
			note: hostFQDN + " runs Alpine 3.24; /etc/resolv.conf is a file udhcpc writes on each lease, naming 192.0.2.1, and udhcpc.conf is as installed." +
				listed + " Each change asks first whether to stop udhcpc rewriting it."},
		{name: "pve9", label: "Proxmox VE 9", about: "The installer's file", host: dnsHost(pve9Platform),
			note: hostFQDN + " is a Proxmox VE 9 node; /etc/resolv.conf is its installer's file, naming 192.0.2.1 and example.net." + listed + each},
		{name: "networkmanager", label: "NetworkManager", about: "Reported only (P-37)", host: networkManagerHost,
			note: hostFQDN + " runs Debian 13 with NetworkManager; /etc/resolv.conf links to NetworkManager's file." + listed + " Every item is refused (P-37 (a))."},
		{name: "empty", label: "No nameservers", about: "hardn.yml lists none", host: noListHost,
			note: debian + " hardn.yml lists no nameserver, so Configure DNS is refused and item 3 is not shown."},
		{name: "one", label: "One nameserver", about: "hardn.yml lists one", host: oneListHost,
			note: debian + " hardn.yml lists 192.0.2.54 alone, so Remove nameserver is refused."},
		{name: "configured", label: "Configured", about: "The file lists hardn.yml's", host: configuredDNSHost,
			note: hostFQDN + " runs Debian 13; /etc/resolv.conf lists 192.0.2.54 and 2001:db8::54 already." + listed + " Configure DNS has nothing to change."},
		{name: "failed-reload", label: "Reload without effect", about: "Ubuntu 26.04, rolled back (D3)", host: inertReload,
			note: hostFQDN + " runs Ubuntu 26.04; " + stub + listed + " Its next reload of systemd-resolved loads nothing new, so a change fails and is rolled back."},
		{name: "failed-save", label: "Save refused", about: "Debian 13, hardn.yml immutable", host: immutableHardnYML,
			note: debian + listed + " hardn.yml is immutable (chattr +i), so a change that saves it fails, and the change is rolled back."},
	}
}

// oneListHost is Debian 13 whose hardn.yml lists one nameserver.
func oneListHost() *fakeHost {
	return simulate(debian13Platform).withHardnYML(nameserversYML(dnsList[:1]))
}

// findDNSHost returns the DNS screen's host named name, after dnsPrefix.
func findDNSHost(name string) (dnsScreenHost, bool) {
	for _, h := range dnsScreenHosts() {
		if h.name == name {
			return h, true
		}
	}
	return dnsScreenHost{}, false
}

// dnsStart returns the DNS screen's hosts' menu, or the host, that args
// name ("dns", "dns/<host>"), and whether they name one; an error for a
// host that is not one.
func dnsStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != dnsPrefix && !strings.HasPrefix(args[0], dnsPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], dnsPrefix+"/"); args[0] != dnsPrefix {
		if _, ok := findDNSHost(name); !ok {
			var names []string
			for _, h := range dnsScreenHosts() {
				names = append(names, dnsPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no DNS host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}

// dnsHostMenu is the menu of the DNS screen's hosts: the sub-menu layout,
// an item per host.
func dnsHostMenu() screen.SubMenu {
	var items []component.MenuItem
	for i, h := range dnsScreenHosts() {
		items = append(items, component.MenuItem{Key: fmt.Sprint(i + 1), Label: h.label, Description: h.about})
	}
	return screen.SubMenu{
		Title:   component.TitleBar{Title: "DNS hosts"},
		Heading: "Select an option",
		Menu:    component.Menu{Items: items, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:  "Option",
	}
}

// openDNS returns the action that opens what name names: the hosts' menu
// for dnsPrefix alone, the DNS screen on one host for "dns/<host>".
func (s *session) openDNS(name string) func() error {
	if dh, ok := findDNSHost(strings.TrimPrefix(name, dnsPrefix+"/")); ok && name != dnsPrefix {
		return func() error { return s.runDNS(context.Background(), dh) }
	}
	return func() error {
		vm, all := dnsHostMenu(), dnsScreenHosts()
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, it := range vm.Menu.Items {
			dh := all[i]
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.runDNS(context.Background(), dh) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// runDNS runs the DNS screen on dh's host, fresh (dnsScreen).
func (s *session) runDNS(ctx context.Context, dh dnsScreenHost) error {
	return s.dnsScreen(ctx, dh, dh.host())
}

// dnsScreen runs the DNS screen on h, dh's host, until the user goes back
// from it: its note above it, set apart as a scenario's is (runScenario);
// the facts detect's, as the engine establishes them on the host; hardn.yml
// read as hardn reads it (config.Read); each change's engine the gallery's,
// over the host as the engine reaches it, with the gallery's hold, its
// apply recorded for the end of play. A dry run when play plays the
// dry-run sequence.
func (s *session) dnsScreen(ctx context.Context, dh dnsScreenHost, h *fakeHost) error {
	sys, title := h.system(), dnsPrefix+"/"+dh.name
	facts, err := detect.Detect(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	cfg, err := config.Read(ctx, sys, hardnYML)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	c, gap := s.p.Ctx, strings.Repeat("\n", theme.SectionGap)
	note := component.Callout{Kind: component.MessageInfo, Title: title, Body: []string{dh.note}}.Render(c) + "\n" + gap
	if s.p.In.Asked() {
		note = gap + strings.TrimSuffix(note, gap)
	}
	if _, err := io.WriteString(s.p.Out, note); err != nil {
		return err
	}
	var rec *recorder
	return dnsscreen.Run(ctx, dnsscreen.Options{
		Prompter: s.p, System: sys, Facts: facts, Config: cfg, DryRun: s.sequence == "dry-run",
		LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			r := &recorder{host: h, f: h.f, title: title}
			rec = r
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, r)
			s.mu.Unlock()
			r.begin()
			return engine.New(sys, engine.Options{
				Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
				Session:  applySession,
				Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
				Hold:     s.hold,
				PID:      applyPID,
				Progress: func(ev engine.Event) { r.event(ev); progress(ev) },
			})
		},
		Ran: func(run output.Run) {
			rec.end()
			rec.setPlan(run.Plan)
			rec.failedWith(run.Err)
		},
	})
}
