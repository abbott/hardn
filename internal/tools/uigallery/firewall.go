package main

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	fwscreen "github.com/abbott/hardn/pkg/screens/firewall"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Firewall screen on the simulated hosts (refactor plan slice 4, unit (c),
// next/s4-screen): hardn's Firewall screen itself (pkg/screens/firewall), drawn
// from what the Firewall control reads of each host (firewallhost.go), each of
// its changes on the real engine, on the hosts the Firewall mocks were drawn
// for, which it replaces (docs/engine/screen-firewall.md). The main menu's 3
// leads to it on Debian 13; "play firewall" is the menu of the hosts and "play
// firewall/<host>" one. The listing of the mocks, "uigallery firewall", was
// retired with them: git keeps it (firewall.go, next/s4-control), and
// gallery-firewall.md records what each showed.

// fwPrefix names the Firewall screen's hosts in play: "firewall" is the menu of
// them and "firewall/<host>" one.
const fwPrefix = "firewall"

// fwMainHost is the host the main menu's 3 leads to.
const fwMainHost = fwPrefix + "/debian13"

// fwLinks lead from a menu mock's item to the Firewall screen on a host, beside
// links: the main menu's 3.
var fwLinks = map[string]string{mainMenuMock + "/3": fwMainHost}

// fwHost is one simulated host the Firewall screen runs on.
type fwHost struct {
	// name is the host's, after fwPrefix: "debian13".
	name string
	// label and about are its item in the menu of the hosts.
	label, about string
	// note says what the host is.
	note string
	// host returns the simulated host, fresh.
	host func() *fakeHost
}

// fwHosts are the hosts, in the menu's order.
func fwHosts() []fwHost {
	const (
		deb = hostFQDN + " runs Debian 13. "
		alp = hostFQDN + " runs Alpine 3.24. "
		pxm = hostFQDN + " is a Proxmox VE 9 node, no ufw. "
	)
	return []fwHost{
		{name: "debian13", label: "Debian 13", about: "No ufw", host: func() *fakeHost { return fwNoUFW(debian13Platform) },
			note: deb + "ufw is not installed (check-firewall.md §3: Debian installs it only on request, [unverified])."},
		{name: "ubuntu2604", label: "Ubuntu 26.04", about: "ufw inactive, ssh.socket", host: fwUbuntu,
			note: hostFQDN + " runs Ubuntu 26.04 as shipped: ufw installed and inactive, ENABLED=no, no rule, sshd socket-activated on [::]:22 (vmtest.md §4.9)."},
		{name: "active22", label: "Active on 22", about: "Deny policy, SSH let through", host: fwDebian,
			note: deb + "ufw active, default deny incoming, allowing 22, where sshd listens."},
		{name: "hardened", label: "Hardened", about: "Deny policy, sshd on 2230", host: func() *fakeHost { return ufwOn(fwHardened()) },
			note: deb + "After hardn: sshd reloaded onto 2230, ufw active, default deny incoming, allowing 2230 alone."},
		{name: "allow-policy", label: "Allow policy", about: "ufw on, refusing nothing", host: fwWith(ufwSim{incoming: "allow", active: true, enabled: true}),
			note: deb + "ufw active, default allow incoming, no rule: it refuses nothing."},
		{name: "lockout", label: "Lockout", about: "ufw refuses SSH", host: func() *fakeHost { return ufwOn(lockout().withHardnYML(fwYML)) },
			note: deb + "sshd on 2230, ufw active allowing 443 alone: a new SSH session is refused. Item 1 lets it through."},
		{name: "lockout-at-start", label: "Lockout at start", about: "Stopped, saved rules refuse SSH", host: func() *fakeHost { return ufwOn(lockoutAtStart().withHardnYML(fwYML)) },
			note: deb + "sshd on 2230, ufw stopped with ENABLED=yes, its saved rules allowing 443 alone: the next boot refuses SSH. hardn refuses to plan over the disagreement."},
		{name: "allowlist", label: "Allowlist", about: "SSH from some sources", host: fwWith(ufwSim{incoming: "deny", active: true, enabled: true, rules: []simRule{{action: "allow", port: "22", src: "192.0.2.0/24"}, {action: "allow", port: "22", src: "2001:db8::/32"}}}),
			note: deb + "ufw active, default deny incoming, SSH let through from 192.0.2.0/24 and 2001:db8::/32 alone."},
		{name: "ipv4-allowlist", label: "IPv4 allowlist", about: "SSH refused on IPv6", host: fwWith(ufwSim{incoming: "deny", active: true, enabled: true, rules: []simRule{{action: "allow", port: "22", src: "192.0.2.0/24"}, {action: "allow", port: "22", src: "198.51.100.0/24"}}}),
			note: deb + "ufw active, default deny incoming, SSH let through from 192.0.2.0/24 and 198.51.100.0/24 alone, no IPv6 rule, sshd on [::]:22 too: refused on IPv6 alone while IPv4 gets in, no lockout risk (the maintainer, 2026-11-08). Item 1 is Disable."},
		{name: "ipv6-off", label: "IPv6 off in ufw", about: "SSH on IPv4 only", host: fwWith(ufwSim{incoming: "deny", active: true, enabled: true, noIPv6: true, rules: []simRule{{action: "allow", port: "22"}}}),
			note: deb + "ufw active, default deny incoming, allowing 22, IPV6=no, sshd on [::]:22 too: ufw drops all IPv6, SSH gets in over IPv4 alone (Partial ssh IPv4 only). Item 1 is Disable."},
		{name: "blocklist", label: "Blocklist", about: "A saved deny before the allow", host: fwWith(ufwSim{incoming: "deny", rules: []simRule{{action: "deny", port: "22", src: "198.51.100.0/24"}, {action: "allow", port: "22"}}}),
			note: deb + "ufw inactive, its saved rules denying 198.51.100.0/24 on 22 before allowing 22: whether the session's own address is among them is not established, so Enable refuses (ufw.ErrBlocks; P-33 (3))."},
		{name: "edit-2230", label: "Edit not reloaded", about: "Configured 2230, sshd on 22", host: fwEdit,
			note: deb + "Port 2230 in hardn's drop-in, sshd not reloaded, so still on 22, ufw inactive. What accepts SSH is not what is configured, so Enable refuses (decision 2; mock stale-listener has what the real plan does)."},
		{name: "moved-2230", label: "Bound elsewhere", about: "Configured 22, sshd on 2230", host: fwMoved,
			note: deb + "ufw active, allowing 22, sshd -T says 22, 2230 bound: ufw refuses what accepts SSH, and ss doesn't say whose 2230 is, so it gets no row. Enable refuses (decision 2)."},
		{name: "denied", label: "Denied", about: "ufw denies 22", host: fwWith(ufwSim{incoming: "deny", active: true, enabled: true, rules: []simRule{{action: "deny", port: "22"}}}),
			note: deb + "ufw active, with deny 22/tcp: hardn doesn't change a rule it finds, so it has nothing to plan and refuses, saying what to do."},
		{name: "alpine324", label: "Alpine 3.24", about: "No ufw", host: func() *fakeHost { return fwNoUFW(alpine324Platform) },
			note: alp + "ufw is not installed (community repository, ufw.md §3)."},
		{name: "alpine-stock", label: "Alpine 3.23, stock", about: "No ufw, no ss, no index", host: fwAlpineStock,
			note: hostFQDN + " runs Alpine 3.23 as a stock VM does (alpine323-vm.log): no ufw, no ss, apk's index empty, so Install refuses."},
		{name: "alpine-runlevel", label: "Alpine, no runlevel", about: "Active, gone after a reboot", host: fwAlpineActive,
			note: alp + "ufw active, deny incoming, allowing 22, ENABLED=yes, in no runlevel, so it does not come back after a reboot (ufw.md §6, [I]). v0.3.2 ran rc-update add ufw default. The Firewall row says off after restart."},
		{name: "unit-off", label: "Service off", about: "Active, gone after a reboot", host: fwWith(ufwSim{incoming: "deny", active: true, enabled: true, unitOff: true, rules: []simRule{{action: "allow", port: "22"}}}),
			note: deb + "ufw active, deny incoming, allowing 22, ENABLED=yes, ufw.service disabled. Whether that stops ufw at boot on Debian is [unverified] (ufw.md)."},
		{name: "pve9", label: "Proxmox VE 9", about: "pve-firewall disabled", host: pveNode,
			note: pxm + "pve-firewall running and disabled, as installed."},
		{name: "pve9-drop", label: "Proxmox VE 9, drop", about: "pve-firewall on, DROP", host: fwPVEDrop,
			note: pxm + "The datacenter firewall on, policy_in DROP, the management IP set written."},
		{name: "pve9-ufw", label: "Proxmox VE 9, ufw on", about: "ufw left on", host: fwPVEUFW,
			note: hostFQDN + " is a Proxmox VE 9 node where origin/main enabled ufw: pve-firewall disabled, ufw active with ENABLED=yes. hardn reads ufw.conf alone and makes no ufw call (decision 7)."},
		{name: "profiles", label: "Two profiles", about: "hardn.yml has two", host: func() *fakeHost {
			h := statusOn(debian13Platform).withHardnYML(fwProfileYML)
			return ufwOn(h)
		},
			note: deb + "ufw active on 22, hardn.yml with two application profiles, not applied."},
	}
}

// fwHardened is Debian 13 after hardn, with hardn.yml (hardened's: unattended
// upgrades and DNS too, none of which this screen reads).
func fwHardened() *fakeHost {
	h := statusOn(debian13Platform)
	h.harden()
	return h.withHardnYML(fwYML)
}

// findFWHost returns the host named name, after fwPrefix.
func findFWHost(name string) (fwHost, bool) {
	all := fwHosts()
	i := slices.IndexFunc(all, func(h fwHost) bool { return h.name == name })
	if i < 0 {
		return fwHost{}, false
	}
	return all[i], true
}

// fwSession is the session that applies a change on h: alice's connection to
// the port sshd is bound on (applySession's, whose port is 22; on a host moved
// to 2230 a session on 22 would keep a port nothing listens on).
func fwSession(h *fakeHost) revert.Session {
	s := applySession
	if len(h.bound) > 0 {
		if a, err := netip.ParseAddrPort(h.bound[0]); err == nil {
			s.Conn = fmt.Sprintf("192.0.2.10 50022 192.0.2.20 %d", a.Port())
		}
	}
	return s
}

// fwHostMenu is the menu of the Firewall screen's hosts: the sub-menu layout,
// an item per host.
func fwHostMenu() screen.SubMenu {
	var items []component.MenuItem
	for i, fh := range fwHosts() {
		items = append(items, component.MenuItem{Key: fmt.Sprint(i + 1), Label: fh.label, Description: fh.about})
	}
	return screen.SubMenu{
		Title:   component.TitleBar{Title: "Firewall hosts"},
		Heading: "Select a host",
		Menu:    component.Menu{Items: items, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:  "Enter your choice",
	}
}

// openFW returns the action that opens what name names: the hosts' menu for
// fwPrefix alone, the screen on one host for "firewall/<host>".
func (s *session) openFW(name string) func() error {
	if fh, ok := findFWHost(strings.TrimPrefix(name, fwPrefix+"/")); ok && name != fwPrefix {
		return func() error { return s.fwScreen(context.Background(), fh, fh.host()) }
	}
	return func() error {
		vm, all := fwHostMenu(), fwHosts()
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, it := range vm.Menu.Items {
			fh := all[i]
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.fwScreen(context.Background(), fh, fh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// fwScreen runs the Firewall screen on h, fh's host, until the user goes back
// from it: its note above it; the facts detect's, as the engine establishes
// them on the host; the session on the port sshd is bound on (fwSession);
// hardn.yml the host's, or the defaults where it has none; the change's engine
// the gallery's, over the host as the engine reaches it, its backups the
// defaults, with the gallery's hold, its apply recorded for the end of play. A
// dry run when play plays the dry-run sequence.
func (s *session) fwScreen(ctx context.Context, fh fwHost, h *fakeHost) error {
	sys, title := h.system(), fwPrefix+"/"+fh.name
	facts, err := detect.Detect(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	cfg, err := statusConfig(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	if err := s.writeNote(title, fh.note); err != nil {
		return err
	}
	var rec *recorder
	return fwscreen.Run(ctx, fwscreen.Options{
		Prompter: s.p, System: sys, Facts: facts, Session: fwSession(h), Config: cfg, DryRun: s.sequence == "dry-run",
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
				Session:  fwSession(h),
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

// fwStart returns the hosts' menu, or the host, that args name ("firewall",
// "firewall/<host>"), and whether they name one; an error for a host that is
// not one.
func fwStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != fwPrefix && !strings.HasPrefix(args[0], fwPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], fwPrefix+"/"); args[0] != fwPrefix {
		if _, ok := findFWHost(name); !ok {
			var names []string
			for _, h := range fwHosts() {
				names = append(names, fwPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no firewall host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}
