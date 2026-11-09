package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls"
	probe "github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The status hosts (refactor plan P4, next/p4-gallery-status): the main
// menu drawn from the real status checks (pkg/engine/controls), header
// probe (pkg/engine/header) and risk level (pkg/engine/risk) over a
// simulated host (statushost.go), mapped by pkg/output as hardn's live
// main menu will map them, so that the maintainer reviews each status
// line, the risk summary and the header as hardn would show them. "play
// status" and "play status/<host>" play it; "status" lists it, with no
// input.

// statusPrefix names the status hosts: in play, "status" is the menu of
// them and "status/<host>" one.
const statusPrefix = "status"

// mainMenuMock is the main menu's mock, whose items, menu heading, mode
// and prompt a status host's main menu keeps, and whose links its items
// follow.
const mainMenuMock = "main-menu/00-tui-main"

// statusHost is one simulated host the main menu reads.
type statusHost struct {
	// name is the host's, after statusPrefix: "debian13".
	name string
	// label and about are its item in play's menu of the hosts.
	label, about string
	// note says what the host is.
	note string
	// host returns the simulated host, fresh.
	host func() *fakeHost
}

// statusHosts returns the status hosts, in the order the menu lists them:
// the platforms as the engine's scenarios start from them (not as
// installed: ufw is set up where it is installed), then hosts as
// installed (a Proxmox VE node, Debian 13 with root alone, Ubuntu 26.04's
// cloud image and an install from its ISO), then the host states that
// show the other lines.
func statusHosts() []statusHost {
	debian := func() *fakeHost { return statusOn(debian13Platform) }
	hardened := func() *fakeHost { h := debian(); h.harden(); return h }
	onDebian := func(aa apparmorState) func() *fakeHost {
		return func() *fakeHost { return statusWith(debian13Platform, aa) }
	}
	const debianNote = "Debian 13, as the engine's scenarios start from it: sshd on 22 through ssh.service, " +
		"ufw active allowing 22, alice in sudo, no unattended-upgrades; up 3 days."
	return []statusHost{
		{name: "debian13", label: "Debian 13", about: "ssh.service, ufw allowing 22", host: debian,
			note: debianNote + " AppArmor enforces its profiles."},
		{name: "ubuntu2604", label: "Ubuntu 26.04", about: "ssh.socket, unattended-upgrades", host: func() *fakeHost { return statusOn(ubuntu2604Platform) },
			note: "Ubuntu 26.04, as the engine's scenarios start from it: ssh.socket on 22, sudo-rs, ufw active allowing 22, " +
				"alice in sudo, AppArmor enforcing, unattended-upgrades on; up 3 days."},
		{name: "alpine324", label: "Alpine 3.24", about: "OpenRC, doas, no ufw", host: func() *fakeHost { return statusOn(alpine324Platform) },
			note: "Alpine 3.24, as the engine's scenarios start from it: OpenRC's sshd on 22, doas with alice in wheel, " +
				"no ufw, a kernel without AppArmor, crond running and no apk-cron; up 3 days."},
		{name: "pve9", label: "Proxmox VE 9", about: "A node as installed", host: pveNode,
			note: "A Proxmox VE 9 node as installed: root alone, PermitRootLogin yes, sshd on 22, pve-firewall running " +
				"and disabled, AppArmor enforcing LXC's profiles, no unattended-upgrades; up 41 days."},
		{name: "fresh", label: "Fresh Debian 13", about: "Root alone, nothing set up", host: fresh,
			note: "Debian 13 installed with root alone, no first user, before anything is set up: no ufw, sshd's defaults on 22, " +
				"AppArmor enforcing, no unattended-upgrades; up 3 days."},
		{name: "ubuntu2604-cloud", label: "Ubuntu 26.04 cloud image", about: "ufw inactive, key-only", host: ubuntuCloud,
			note: "Ubuntu 26.04 as its cloud image ships: ssh.socket on 22, ufw installed and inactive, 60-cloudimg-settings.conf " +
				"refusing passwords, alice the default user, whom cloud-init lets use sudo without a password, AppArmor enforcing, " +
				"unattended-upgrades on; up 3 days."},
		{name: "ubuntu2604-iso", label: "Ubuntu 26.04 from the ISO", about: "ufw inactive, passwords", host: ubuntuISO,
			note: "Ubuntu 26.04 installed from the server ISO with no SSH key imported: ssh.socket on 22, ufw installed and inactive, " +
				"cloud-init's 50-cloud-init.conf allowing passwords, alice in sudo, AppArmor enforcing, unattended-upgrades on; up 3 days."},
		{name: "hardened", label: "Hardened Debian 13", about: "Every row as hardn wants it", host: hardened,
			note: "Debian 13 after hardn: alice in sudo, ufw allowing 2230 alone, sshd on 2230 refusing root's login " +
				"and passwords, AppArmor enforcing, unattended-upgrades on, hardn.yml's nameservers in use; up 3 days."},
		{name: "figma", label: "Figma's host", about: "00-TUI-MAIN's rows", host: figmaHost,
			note: "Debian 13 with 00-TUI-MAIN's rows: the hardened host, but AppArmor loads no profile " +
				"and unattended-upgrades is not installed; up 182 days."},
		{name: "lockout", label: "Lockout", about: "ufw refuses SSH", host: lockout,
			note: "The hardened host, but ufw allows 443 alone, so it refuses SSH on 2230: a new SSH session cannot log in."},
		{name: "lockout-at-start", label: "Lockout at ufw's start", about: "ufw stopped, its rules refuse SSH", host: lockoutAtStart,
			note: "The hardened host, but ufw is stopped, enabled at boot (ENABLED=yes), and its saved rules allow 443 alone: " +
				"SSH on 2230 works until ufw starts, then a new SSH session cannot log in."},
		{name: "reboot", label: "Reboot pending", about: "A newer kernel, Ubuntu 26.04", host: rebooting,
			note: "The ubuntu2604 host, with a newer kernel installed than the one running and " +
				"/var/run/reboot-required written."},
		{name: "apparmor-logging", label: "AppArmor logging only", about: "Every profile in complain mode",
			host: onDebian(apparmorComplain), note: debianNote + " AppArmor's profiles are in complain mode."},
		{name: "apparmor-restart", label: "AppArmor off after restart", about: "apparmor.service disabled", host: apparmorDisabled,
			note: debianNote + " AppArmor enforces its profiles, and apparmor.service is disabled, so the next boot loads none."},
		{name: "apparmor-kernel", label: "AppArmor off in kernel", about: "apparmor=0", host: onDebian(apparmorKernelOff),
			note: debianNote + " The kernel booted with apparmor=0."},
		{name: "apparmor-unread", label: "AppArmor unread", about: "securityfs not mounted", host: onDebian(apparmorNoFS),
			note: debianNote + " AppArmor is on, and securityfs is not mounted, so its profiles cannot be read."},
		{name: "container", label: "Container", about: "Debian 13 in LXC", host: container,
			note: "Debian 13 in an LXC container, whose host sets AppArmor: sshd on 22 through ssh.service, " +
				"ufw active allowing 22, alice in sudo, no unattended-upgrades; up an hour."},
		{name: "alpine-unpatched", label: "Alpine, sshd unpatched", about: "Key-only, openssh 10.3_p1-r1", host: alpineUnpatched,
			note: "The alpine324 host with password login off and openssh at 10.3_p1-r1, which lacks " +
				"CVE-2026-123158's fix."},
		{name: "alpine-exposed", label: "Alpine, password bypass", about: "CVE-2026-123158 exposed", host: alpineExposed,
			note: "The alpine-unpatched host with password login on and bob given a password: " +
				"anyone can log in as alice."},
	}
}

// pveNode is a Proxmox VE 9 node, up 41 days.
func pveNode() *fakeHost {
	h := statusOn(pve9Platform)
	h.f.AddFile(probe.UptimeFile, []byte(upWeeks), 0o444, 0, 0)
	return h
}

// fresh is Debian 13 with root alone and no ufw.
func fresh() *fakeHost {
	p := debian13Platform
	p.noUFW = true
	h := statusOn(p)
	h.accounts(false)
	return h
}

// figmaHost is the hardened host with no AppArmor profile loaded, no
// unattended-upgrades, up 182 days, 2 hours and 26 minutes: Figma's rows.
func figmaHost() *fakeHost {
	h := statusWith(debian13Platform, apparmorNone)
	h.harden()
	delete(h.status.packages, "unattended-upgrades")
	h.status.aptDump = ""
	h.f.AddFile(probe.UptimeFile, []byte(upFigma), 0o444, 0, 0)
	return h
}

// lockout is the hardened host, ufw allowing 443 alone.
func lockout() *fakeHost {
	h := statusOn(debian13Platform)
	h.harden()
	h.allowed = []uint16{443}
	h.saveRules()
	return h
}

// lockoutAtStart is the hardened host, ufw stopped and enabled at boot,
// its saved rules allowing 443 alone.
func lockoutAtStart() *fakeHost {
	h := lockout()
	h.stopUFW()
	return h
}

// ubuntuCloud is Ubuntu 26.04 as its cloud image ships.
func ubuntuCloud() *fakeHost {
	h := statusOn(ubuntu2604Platform)
	h.ufwAsShipped()
	h.cloudImage()
	return h
}

// ubuntuISO is Ubuntu 26.04 installed from its server ISO, no SSH key
// imported.
func ubuntuISO() *fakeHost {
	h := statusOn(ubuntu2604Platform)
	h.ufwAsShipped()
	h.isoInstall()
	return h
}

// rebooting is Ubuntu 26.04 with a reboot pending.
func rebooting() *fakeHost {
	h := statusOn(ubuntu2604Platform)
	h.rebootPending()
	return h
}

// unfixedOpenSSH is openssh-server one revision before Alpine 3.24's fix
// for CVE-2026-123158 (10.3_p1-r2).
const unfixedOpenSSH = "10.3_p1-r1"

// alpineUnpatched is Alpine 3.24, key-only, its sshd unfixed.
func alpineUnpatched() *fakeHost {
	p := alpine324Platform
	p.openssh = unfixedOpenSSH
	h := statusOn(p)
	h.keyOnly()
	return h
}

// alpineExposed is Alpine 3.24, its sshd unfixed, password login on, and
// bob's SHA-512 hash the salt a long password borrows to log in as alice.
func alpineExposed() *fakeHost {
	p := alpine324Platform
	p.openssh = unfixedOpenSSH
	h := statusOn(p)
	h.passwordUser()
	return h
}

// apparmorDisabled is Debian 13 with apparmor.service disabled.
func apparmorDisabled() *fakeHost {
	h := statusOn(debian13Platform)
	h.status.units["apparmor.service"] = unit{enabled: "disabled"}
	return h
}

// container is Debian 13 in an LXC container.
func container() *fakeHost {
	h := statusOn(debian13Platform)
	h.inContainer()
	return h
}

// findStatusHost returns the status host named name.
func findStatusHost(name string) (statusHost, bool) {
	all := statusHosts()
	i := slices.IndexFunc(all, func(h statusHost) bool { return h.name == name })
	if i < 0 {
		return statusHost{}, false
	}
	return all[i], true
}

// statusNames returns the status hosts' names, in order.
func statusNames() []string {
	var names []string
	for _, h := range statusHosts() {
		names = append(names, h.name)
	}
	return names
}

// hostStatus is what the main menu reads of a host: detect's facts, the
// checks' reports, the header probe's header and the risk level over
// both.
type hostStatus struct {
	facts   detect.Facts
	reports []engine.Report
	header  probe.Header
	level   risk.Level
}

// readStatus reads h as hardn's main menu does: hardn.yml as hardn loads
// it (statusConfig), then detect's facts, once, Engine.Check over the main
// menu's status controls (controls.Status) on them, the header probe on
// the same facts, and the risk level over both. The error is hardn.yml's,
// ctx's or detection's alone, as Engine.Check's: a check's error is in its
// report.
func (s *session) readStatus(ctx context.Context, h *fakeHost) (hostStatus, error) {
	sys := h.sys()
	cfg, err := statusConfig(ctx, sys)
	if err != nil {
		return hostStatus{}, err
	}
	var facts detect.Facts
	e := engine.New(sys, engine.Options{
		Detect: func(ctx context.Context, sys host.System) (detect.Facts, error) {
			var err error
			facts, err = detect.Detect(ctx, sys)
			return facts, err
		},
		Hold: s.hold,
	})
	reports, err := e.Check(ctx, controls.Status(cfg))
	if err != nil {
		return hostStatus{}, err
	}
	hd := probe.Read(ctx, sys, facts)
	return hostStatus{facts: facts, reports: reports, header: hd, level: risk.Assess(reports, hd)}, nil
}

// statusConfig is the simulated host's hardn.yml as hardn loads it, from
// the system-wide path (hardnYML, read through sys: config.Read), or the
// defaults where there is none, as hardn has them with no file found,
// listing no nameserver.
func statusConfig(ctx context.Context, sys host.System) (*config.Config, error) {
	cfg, err := config.Read(ctx, sys, hardnYML)
	if errors.Is(err, fs.ErrNotExist) {
		return config.DefaultConfig(), nil
	}
	return cfg, err
}

// mainMenu is the main menu of st: the main menu's mock with its header,
// distribution bar, host block, risk summary and status rows as pkg/output
// maps the host's, its version the mock's (output.MainMenuOf, which
// internal/tools/p4status draws a real host's with).
func (st hostStatus) mainMenu() screen.MainMenu {
	return output.MainMenuOf(screen.MainMenuFixture(), st.header, st.reports, st.level)
}

// statusNote is play's note for the status hosts.
const statusNote = "Play mode: hardn's main menu, its header, risk level and status rows read by the real checks, " +
	"header probe and risk level from a simulated host, hostfake's host.System, whose commands a model of the host answers. " +
	"Its items lead to the screen mocks, which show Figma's host, not this one, on the gallery's fixture plans. " +
	"Nothing on this machine changes."

// statusStart returns the status hosts' menu, or the host, that args name
// ("status", "status/<host>"), and whether they name one; an error for a
// host that is not one.
func statusStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != statusPrefix && !strings.HasPrefix(args[0], statusPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], statusPrefix+"/"); args[0] != statusPrefix {
		if _, ok := findStatusHost(name); !ok {
			return "", false, fmt.Errorf("no status host %q; the hosts are %s/%s", args[0], statusPrefix, strings.Join(statusNames(), ", "+statusPrefix+"/"))
		}
	}
	return args[0], true, nil
}

// hostMenu is the status hosts' menu: the sub-menu layout, an item per
// host.
func hostMenu() screen.SubMenu {
	var items []component.MenuItem
	for i, h := range statusHosts() {
		items = append(items, component.MenuItem{Key: fmt.Sprint(i + 1), Label: h.label, Description: h.about})
	}
	return screen.SubMenu{
		Title:   component.TitleBar{Title: "Status hosts"},
		Heading: "Select a host",
		Menu:    component.Menu{Items: items, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:  "Enter your choice",
	}
}

// openStatus returns the action that opens what name names: the hosts'
// menu for statusPrefix alone, one host for "status/<host>".
func (s *session) openStatus(name string) func() error {
	if sh, ok := findStatusHost(strings.TrimPrefix(name, statusPrefix+"/")); ok && name != statusPrefix {
		return func() error { return s.openHost(context.Background(), sh) }
	}
	return func() error {
		vm, all := hostMenu(), statusHosts()
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, it := range vm.Menu.Items {
			sh := all[i]
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.openHost(context.Background(), sh) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// openHost reads sh's host, fresh, and runs its main menu until the user
// goes back from it, its note above it, set apart as a scenario's is
// (runScenario). Its items lead where the main menu's mock's do.
func (s *session) openHost(ctx context.Context, sh statusHost) error {
	st, err := s.readStatus(ctx, sh.host())
	if err != nil {
		return fmt.Errorf("%s/%s: %w", statusPrefix, sh.name, err)
	}
	c, gap := s.p.Ctx, strings.Repeat("\n", theme.SectionGap)
	note := component.Callout{Kind: component.MessageInfo, Title: statusPrefix + "/" + sh.name, Body: []string{sh.note}}.Render(c) + "\n" + gap
	if s.p.In.Asked() {
		note = gap + strings.TrimSuffix(note, gap)
	}
	if _, err := io.WriteString(s.p.Out, note); err != nil {
		return err
	}
	return s.open(screen.Mock{Name: mainMenuMock, Screen: st.mainMenu()})()
}

// statusUsage is the status listing's usage line.
const statusUsage = "usage: go run ./internal/tools/uigallery status [flags] [host | all]..."

// statusList is the status listing: each host args name (none, or "all",
// is every host) under the gallery's header, "status <host>", its note,
// then its main menu as hardn would draw it down to its prompt, with no
// input, a write each. -list prints the hosts' names. It sizes itself as
// play does (look.sizeTo); the main menu takes its full form at every
// height, as 00-TUI-MAIN.png shows it (screen.CompactMainMenu).
func statusList(args []string, size sizer, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uigallery status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, statusUsage)
		fs.PrintDefaults()
	}
	list := fs.Bool("list", false, "print the names of the hosts it would list, and exit")
	look := lookFlags(fs, true)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	usage := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "uigallery: status: "+format+"\n", a...)
		return exitUsage
	}
	hosts, err := selectHosts(fs.Args())
	if err != nil {
		return usage("%v", err)
	}
	if *list {
		for _, h := range hosts {
			fmt.Fprintln(stdout, h.name)
		}
		return exitOK
	}
	look.sizeTo(fs, size)
	c, err := look.cell()
	if err != nil {
		return usage("%v", err)
	}
	g, ok := look.gallery(stderr)
	if !ok {
		return exitError
	}
	rc, err := g.ctx(c.env)
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: status: %v\n", err)
		return exitError
	}
	if err := listStatus(context.Background(), rc, c.env.Writer(stdout), hosts); err != nil {
		fmt.Fprintf(stderr, "uigallery: status: %v\n", err)
		return exitError
	}
	return exitOK
}

// listStatus writes each of hosts to w, rendered in rc, a write each: the
// gallery's header "status <host>", its note, then its main menu down to
// its prompt. It stops at the first host it cannot read or write, its
// error naming the host.
func listStatus(ctx context.Context, rc component.Ctx, w io.Writer, hosts []statusHost) error {
	s, gap := &session{}, strings.Repeat("\n", theme.SectionGap+1)
	for _, h := range hosts {
		st, err := s.readStatus(ctx, h.host())
		if err == nil {
			out := header(rc, statusPrefix+" "+h.name) + gap +
				component.Callout{Kind: component.MessageInfo, Body: []string{h.note}}.Render(rc) + gap +
				st.mainMenu().Render(rc) + gap
			_, err = io.WriteString(w, out)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", h.name, err)
		}
	}
	return nil
}

// selectHosts returns the hosts args name, in the catalogue's order: every
// host for none or "all". An argument that starts with "-" is a flag the
// flag package left unparsed, after a name.
func selectHosts(args []string) ([]statusHost, error) {
	all := statusHosts()
	if len(args) == 0 || slices.Contains(args, "all") {
		return all, nil
	}
	for _, a := range args {
		switch _, ok := findStatusHost(a); {
		case !ok && strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("flag %s comes after a host's name; flags go before them", a)
		case !ok:
			return nil, fmt.Errorf("no status host %q; -list shows them", a)
		}
	}
	return slices.DeleteFunc(all, func(h statusHost) bool { return !slices.Contains(args, h.name) }), nil
}
