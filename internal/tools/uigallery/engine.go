package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/confirm"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The engine's scenarios (refactor plan P3, next/p3-gallery-engine): the
// playable mode runs the real plan/apply engine (pkg/engine) and its change
// kinds (pkg/engine/steps) on a simulated host (fakeHost, over hostfake's
// recording host.System), through pkg/output's flow adapter
// (output.FlowEngine) in pkg/ui/flow's canonical flow, so that what the
// maintainer reviews is a real plan, its real steps, the confirm countdown
// and the outcome. The words of each scenario's confirmation question are
// the gallery's, until the slices word their actions (P-10); the DNS
// scenarios' are the DNS mock's (docs/engine/gallery-dns.md §5).

// enginePrefix names the scenarios in play: "engine" is the menu of them,
// "engine/<name>" one.
const enginePrefix = "engine"

// scenario is one of the engine's scenarios: a simulated host and what the
// engine is asked to change on it.
type scenario struct {
	// name is the scenario's, after enginePrefix: "ssh-port-debian13".
	name string
	// label and about are its item in the scenarios' menu.
	label, about string
	// note says what the host is before the change, and what to type.
	note []string
	// host returns the simulated host, fresh, whose facts the engine
	// establishes itself (detect.Detect, engine.Options.Detect's default).
	host func() *fakeHost
	// controls are what the engine plans, with no configuration and no
	// form.
	controls []engine.Control
	// setup, set in place of controls for a scenario whose controls are
	// built on its host (from hardn.yml, as hardn reads it there, or from
	// the answers to a form), returns what they are built from.
	setup func(h *fakeHost) (setup, error)
	// question is the flow's confirmation question.
	question string
	// dryRun stops the flow after the plan.
	dryRun bool
	// confirm, once the change is applied, confirms it from a new SSH
	// connection to port 2230 with hardn confirm, which then cleans up
	// what the change held (P-40).
	confirm bool
	// answers are what the user types to play it as it says: the form's
	// answers, if it has one, then the hostname for a typed confirmation,
	// y for a y/N one, nothing for a plan refused or a dry run; ends is
	// how the flow then ends.
	answers []string
	ends    flow.Outcome
}

// setup is what a scenario plans from on its host: the form the flow asks
// first (with no fields it asks nothing), and plans, which returns the
// controls and the configuration the action plans with. The flow's engine
// calls plans each time the flow plans, after the form, so the controls
// are built from its answers, as a screen of hardn builds them before
// output.FlowEngine plans.
type setup struct {
	form  form.Form
	plans func() ([]engine.Control, *config.Config)
}

// setupOn returns sc's setup on h: its setup's, or its controls, with no
// configuration and no form.
func (sc scenario) setupOn(h *fakeHost) (setup, error) {
	if sc.setup != nil {
		return sc.setup(h)
	}
	return setup{plans: func() ([]engine.Control, *config.Config) { return sc.controls, nil }}, nil
}

// dnsSetup is the setup of a scenario of the DNS control
// (pkg/engine/controls/dns, next/s2-control), as the DNS screen will plan
// (unit (c)): hardn.yml read from the host, as hardn reads it
// (config.Read), the control checking against it as loaded, and the
// action's configuration a copy with list in place of its nameservers when
// list is set (a preset; nil: Configure DNS, hardn.yml's own), which the
// control then saves (steps.PlanSave). Where udhcpc writes
// /etc/resolv.conf, the form asks first "Stop udhcpc rewriting
// /etc/resolv.conf?", y/N, its answer the control's StopUDHCPC
// (gallery-dns.md decision 6).
func dnsSetup(list []string) func(*fakeHost) (setup, error) {
	return func(h *fakeHost) (setup, error) {
		loaded, err := config.Read(context.Background(), h.f, hardnYML)
		if err != nil {
			return setup{}, err
		}
		cfg := loaded
		if list != nil {
			edited := *loaded
			edited.Nameservers = slices.Clone(list)
			cfg = &edited
		}
		stop := new(bool)
		var ask form.Form
		if h.resolver == resolverUDHCPC {
			ask.Fields = []form.Field{&form.YesNo{Title: "Stop udhcpc rewriting /etc/resolv.conf?", Value: stop}}
		}
		return setup{form: ask, plans: func() ([]engine.Control, *config.Config) {
			return []engine.Control{dnsctl.Nameservers{Config: loaded, StopUDHCPC: *stop}}, cfg
		}}, nil
	}
}

// galleryControl is one control of a scenario: its Plan calls a planner
// of pkg/engine/steps, or makes a unit's change with one.
type galleryControl struct {
	id, title string
	plan      func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error)
}

func (c galleryControl) ID() string                          { return c.id }
func (c galleryControl) Title() string                       { return c.title }
func (c galleryControl) Applies(detect.Facts) (bool, string) { return true, "" }

// Check is not used: the scenarios plan and apply, and Phase 4 builds the
// checks.
func (c galleryControl) Check(context.Context, host.System, detect.Facts) (engine.Status, error) {
	return engine.Status{}, nil
}

func (c galleryControl) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	return c.plan(ctx, sys, in)
}

// sshControl plans s through steps.PlanSSH, the one planner of hardn's sshd
// settings.
func sshControl(s steps.SSH) engine.Control {
	s.Prefix = "ssh"
	return galleryControl{"ssh", "SSH", func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		return steps.PlanSSH(ctx, sys, in, s)
	}}
}

// keyOnly is the SSH settings of key-only login: no password, no
// keyboard-interactive.
func keyOnly() engine.Control {
	return sshControl(steps.SSH{Set: []migrate.Directive{
		{Keyword: "PasswordAuthentication", Args: []string{"no"}},
		{Keyword: "KbdInteractiveAuthentication", Args: []string{"no"}},
	}})
}

// ufwEnable enables ufw, its SSH listeners allowed first
// (steps.PlanUFWEnable).
func ufwEnable() engine.Control {
	return galleryControl{"ufw", "Firewall", func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		return steps.PlanUFWEnable(ctx, sys, in, "ufw")
	}}
}

// migration moves hardn's legacy SSH files into 00-hardn.conf
// (steps.PlanMigration).
func migration() engine.Control {
	return galleryControl{"ssh.migrate", "SSH files of hardn v0.3.x", func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		return steps.PlanMigration(ctx, sys, in, "ssh.migrate", "")
	}}
}

// sudoersGrant lets alice use sudo without a password: sudoers' plan for
// the host's tool, as its registered plan step (steps.Sudoers).
func sudoersGrant() engine.Control {
	return galleryControl{"sudoers", "Sudo", func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		pol := sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword}
		c := sudoers.Choose(in.Facts, sudoers.ToolUnknown)
		if c.Err != nil {
			return nil, c.Err
		}
		st, err := sudoers.Read(ctx, sys, c, []string{pol.User})
		if err != nil {
			return nil, err
		}
		p, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
		if err != nil || len(p.Changes) == 0 {
			return nil, err
		}
		ch, err := steps.Sudoers("sudoers/alice", p, "Let alice use sudo without a password")
		return []engine.Change{ch}, err
	}}
}

// inactiveUFW is a host whose ufw is installed and inactive, with no rules.
func inactiveUFW() *fakeHost {
	h := newHost()
	h.allowed, h.inactive = nil, true
	h.saveRules()
	return h
}

// failingReload is a host whose next reload of ssh.service fails, as
// systemctl's does when sshd -t fails in ExecReload.
func failingReload() *fakeHost {
	h := newHost()
	h.failReloads = 1
	return h
}

// d9Host is an Ubuntu 26.04 host hardn v0.3.2 hardened, not rebooted
// since: v0.3.2's manage.conf and listen.conf, written with no
// daemon-reload, so ssh.socket is still configured for, and bound on, port
// 22 (D9, docs/refactor/phase0/side-effects.md). The manage.conf is
// shortened from the verbatim v0.3.2 file migrate's tests hold
// (testdata/legacy/v032-manage.conf) to the lines that change a login.
func d9Host() *fakeHost {
	h := socketHost()
	h.f.AddFile(migrate.ManageConf, []byte("Protocol 2\nStrictModes yes\n\nListenAddress 0.0.0.0:2230\n\n"+
		"AuthenticationMethods publickey\nPubkeyAuthentication yes\n\nPermitRootLogin no\nAllowUsers alice\n\n"+
		"PasswordAuthentication no\nPermitEmptyPasswords no\n\nAuthorizedKeysFile    .ssh/authorized_keys    .ssh_%u/authorized_keys\n"), 0o644, 0, 0)
	h.f.AddFile(migrate.ListenConf, []byte("[Socket]\nListenStream=\nListenStream=2230\n"), 0o644, 0, 0)
	h.allowed = []uint16{22, 2230}
	h.saveRules()
	h.configured = []string{"[::]:22", "0.0.0.0:22"}
	h.bound = slices.Clone(h.configured)
	return h
}

// typeTheHostname is what the scenarios' plans of lockout risk are
// confirmed by: the hostname, typed.
const typeTheHostname = "Type the hostname"

// scenarios returns the engine's scenarios, in the order the menu lists
// them.
func scenarios() []scenario {
	port, typed := sshControl(steps.SSH{Port: 2230}), []string{hostName}
	const (
		stock   = "sshd listens on port 22, and ufw is active, allowing 22/tcp."
		confirm = "Type the hostname, " + hostName + ", to apply it; 0 declines."
	)
	return append([]scenario{
		{name: "ssh-port-debian13", label: "SSH port, Debian 13", about: "Port 2230 through ssh.service",
			note: []string{hostFQDN + " runs Debian 13; sshd is started by ssh.service. " + stock, "The change: SSH port 2230, port 22 held until confirmed. " + confirm},
			host: newHost, controls: []engine.Control{port},
			question: "Change the SSH port to 2230?", answers: typed, ends: flow.Applied},
		{name: "ssh-port-ubuntu2604", label: "SSH port, Ubuntu 26.04", about: "Port 2230 through ssh.socket",
			note: []string{hostFQDN + " runs Ubuntu 26.04; ssh.socket listens, from sshd-socket-generator. " + stock, "The change: SSH port 2230, port 22 held until confirmed. " + confirm},
			host: socketHost, controls: []engine.Control{port},
			question: "Change the SSH port to 2230?", answers: typed, ends: flow.Applied},
		{name: "ssh-port-alpine324", label: "SSH port, Alpine 3.24", about: "Port 2230 through OpenRC",
			note: []string{hostFQDN + " runs Alpine 3.24; sshd is OpenRC's, and ufw is not installed. Elevation is doas.", "The change: SSH port 2230, port 22 held until confirmed. " + confirm},
			host: openRCHost, controls: []engine.Control{port},
			question: "Change the SSH port to 2230?", answers: typed, ends: flow.Applied},
		{name: "keyonly-debian13", label: "Key-only login", about: "No password login, Debian 13",
			note: []string{hostFQDN + " runs Debian 13. " + stock, "The change: SSH login by key only. " + confirm},
			host: newHost, controls: []engine.Control{keyOnly()},
			question: "Allow SSH login by key only?", answers: typed, ends: flow.Applied},
		{name: "ufw-enable-debian13", label: "Enable ufw", about: "From an inactive ufw, Debian 13",
			note: []string{hostFQDN + " runs Debian 13; sshd listens on port 22, and ufw is installed, inactive, with no rules.", "The change: ufw enabled, sshd's listeners allowed first. " + confirm},
			host: inactiveUFW, controls: []engine.Control{ufwEnable()},
			question: "Enable ufw?", answers: typed, ends: flow.Applied},
		{name: "sudoers-grant-debian13", label: "Sudo grant", about: "alice without a password, Debian 13",
			note: []string{hostFQDN + " runs Debian 13, with sudo and its stock /etc/sudoers.d.", "The change: alice may use sudo without a password, which locks no one out, so it is confirmed y/N."},
			host: newHost, controls: []engine.Control{sudoersGrant()},
			question: "Let alice use sudo without a password?", answers: []string{"y"}, ends: flow.Applied},
		{name: "refused-d9-ubuntu2604", label: "Refused: v0.3.2 listen.conf", about: "The migration on a D9 host (P-39)",
			note: []string{hostFQDN + " runs Ubuntu 26.04, hardened by hardn v0.3.2: its manage.conf, and a listen.conf for port 2230 written with no daemon-reload, so ssh.socket still listens on 22.",
				"The change: hardn's v0.3.2 SSH files moved into 00-hardn.conf. The plan refuses it, with a report (P-39 (a))."},
			host: d9Host, controls: []engine.Control{migration()},
			question: "Move hardn's v0.3.2 SSH files into 00-hardn.conf?", ends: flow.Failed},
		{name: "failed-reload-debian13", label: "Failed, rolled back", about: "Port 2230, the reload refused",
			note: []string{hostFQDN + " runs Debian 13. " + stock + " Its next reload of ssh.service fails.", "The change: SSH port 2230. " + confirm},
			host: failingReload, controls: []engine.Control{port},
			question: "Change the SSH port to 2230?", answers: typed, ends: flow.Failed},
		{name: "confirm-cleanup-debian13", label: "Confirmed, cleaned up", about: "Port 2230, then hardn confirm (P-40)",
			note: []string{hostFQDN + " runs Debian 13; sshd listens on port 22, and ufw is installed, inactive, with no rules.",
				"The change: SSH port 2230 with ufw enabled, port 22 and its rule held until confirmed. " + confirm,
				"Then a new SSH connection to port 2230 confirms it with hardn confirm, which drops port 22 and the rule hardn added for it (P-40)."},
			host: inactiveUFW, controls: []engine.Control{sshControl(steps.SSH{Port: 2230, UFW: steps.UFWEnable})},
			question: "Change the SSH port to 2230 and enable ufw?", confirm: true, answers: typed, ends: flow.Applied},
		{name: "best-effort-ubuntu2204", label: "Best-effort release", about: "The plan's warning, Ubuntu 22.04 (P-41)",
			note: []string{hostFQDN + " runs Ubuntu 22.04, a best-effort release: hardn changes it, but is not tested on it. " + stock, "The plan alone, for key-only login: it carries the release's warning (P-41)."},
			host: ubuntu2204Host, controls: []engine.Control{keyOnly()},
			question: "Allow SSH login by key only?", dryRun: true, ends: flow.Planned},
	}, dnsScenarios()...)
}

// dnsScenarios are the DNS control's scenarios (slice 2,
// next/s2-engine-gallery), each a sequence of the DNS mock
// (docs/engine/gallery-dns.md §3) on the real control: hardn.yml, at
// /etc/hardn/hardn.yml under a comment, lists 192.0.2.54 and 2001:db8::54
// but where a note says otherwise, and the change is confirmed y/N, as
// none of DNS's locks anyone out (brief 2.4).
func dnsScenarios() []scenario {
	const (
		listed    = " hardn.yml lists 192.0.2.54, 2001:db8::54."
		yn        = " y applies it; n declines."
		configure = "The change: Configure DNS, hardn.yml's nameservers."
		file      = "/etc/resolv.conf is a file dhcpcd wrote from eth0's lease, naming 192.0.2.1."
		stub      = "/etc/resolv.conf links to systemd-resolved's stub, and eth0 has 192.0.2.1 and example.net from DHCP."
		reloads   = " systemd-resolved reloads its files"
		preset    = "The change: Use Cloudflare DNS, 1.1.1.1 and 1.0.0.1, then hardn.yml saved with that list."
		yes       = "y"
	)
	debian, cloudflare, configureDNS := hostFQDN+" runs Debian 13; "+file+listed, dnsSetup([]string{"1.1.1.1", "1.0.0.1"}), dnsSetup(nil)
	return []scenario{
		{name: "dns-configure-ubuntu2604", label: "DNS, Ubuntu 26.04", about: "Configure DNS: a drop-in, a reload",
			note: []string{hostFQDN + " runs Ubuntu 26.04; " + stub + listed, configure + reloads + " (systemd 259)." + yn},
			host: dnsHost(ubuntu2604Platform), setup: configureDNS, question: "Configure DNS?", answers: []string{yes}, ends: flow.Applied},
		{name: "dns-cloudflare-debian13", label: "DNS preset, Debian 13", about: "Use Cloudflare DNS, then the save",
			note: []string{debian, preset + yn}, host: dnsHost(debian13Platform), setup: cloudflare,
			question: "Use Cloudflare DNS?", answers: []string{yes}, ends: flow.Applied},
		{name: "dns-configure-ubuntu2404", label: "DNS, Ubuntu 24.04", about: "Configure DNS: a restart",
			note: []string{hostFQDN + " runs Ubuntu 24.04; " + stub + listed, configure + " systemd 255 cannot reload systemd-resolved, so the same command restarts it." + yn},
			host: dnsHost(ubuntu2404Platform), setup: configureDNS, question: "Configure DNS?", answers: []string{yes}, ends: flow.Applied},
		{name: "dns-udhcpc-alpine324", label: "DNS, Alpine 3.24", about: "Configure DNS, udhcpc stopped",
			note: []string{hostFQDN + " runs Alpine 3.24; /etc/resolv.conf is a file udhcpc writes on each lease, naming 192.0.2.1, and udhcpc.conf is as installed." + listed,
				configure + " First \"Stop udhcpc rewriting /etc/resolv.conf?\": y stops it in the same change. Then" + yn},
			host: dnsHost(alpine324Platform), setup: configureDNS, question: "Configure DNS?", answers: []string{yes, yes}, ends: flow.Applied},
		{name: "dns-configure-pve9", label: "DNS, Proxmox VE 9", about: "Configure DNS on a node",
			note: []string{hostFQDN + " is a Proxmox VE 9 node; /etc/resolv.conf is its installer's file, naming 192.0.2.1." + listed, configure + yn},
			host: dnsHost(pve9Platform), setup: configureDNS, question: "Configure DNS?", answers: []string{yes}, ends: flow.Applied},
		{name: "dns-refused-networkmanager", label: "Refused: NetworkManager", about: "NetworkManager's DNS (P-37)",
			note: []string{hostFQDN + " runs Debian 13 with NetworkManager; /etc/resolv.conf links to NetworkManager's file." + listed,
				configure + " The plan refuses it, with a report (P-37 (a))."},
			host: networkManagerHost, setup: configureDNS, question: "Configure DNS?", ends: flow.Failed},
		{name: "dns-refused-empty", label: "Refused: no nameservers", about: "Configure DNS, hardn.yml lists none",
			note: []string{hostFQDN + " runs Debian 13; " + file + " hardn.yml lists no nameserver.", configure + " The plan refuses it, with a report."},
			host: noListHost, setup: configureDNS, question: "Configure DNS?", ends: flow.Failed},
		{name: "dns-configured-debian13", label: "DNS, nothing to change", about: "Configure DNS on a configured host",
			note: []string{hostFQDN + " runs Debian 13; /etc/resolv.conf lists 192.0.2.54 and 2001:db8::54 already." + listed, configure + " There is nothing to change (P-27 (b))."},
			host: configuredDNSHost, setup: configureDNS, question: "Configure DNS?", ends: flow.Unchanged},
		{name: "dns-dry-run-debian13", label: "DNS preset, dry run", about: "Use Cloudflare DNS, the plan alone",
			note: []string{debian, preset + " The plan alone: nothing changes or is saved."}, host: dnsHost(debian13Platform), setup: cloudflare,
			question: "Use Cloudflare DNS?", dryRun: true, ends: flow.Planned},
		{name: "dns-failed-reload-ubuntu2604", label: "DNS failed, rolled back", about: "The reload has no effect",
			note: []string{hostFQDN + " runs Ubuntu 26.04; " + stub + listed + " Its next reload of systemd-resolved loads nothing new.",
				configure + yn},
			host: inertReload, setup: configureDNS, question: "Configure DNS?", answers: []string{yes}, ends: flow.Failed},
		{name: "dns-failed-save-debian13", label: "DNS save failed", about: "The change rolled back, Debian 13",
			note: []string{debian + " hardn.yml is immutable (chattr +i), so saving it fails.", preset + yn},
			host: immutableHardnYML, setup: cloudflare, question: "Use Cloudflare DNS?", answers: []string{yes}, ends: flow.Failed},
	}
}

// findScenario returns the scenario named name, after enginePrefix.
func findScenario(name string) (scenario, bool) {
	for _, s := range scenarios() {
		if s.name == name {
			return s, true
		}
	}
	return scenario{}, false
}

// scenarioMenu is the scenarios' menu: the sub-menu layout, an item per
// scenario.
func scenarioMenu() screen.SubMenu {
	var items []component.MenuItem
	for i, s := range scenarios() {
		items = append(items, component.MenuItem{Key: fmt.Sprint(i + 1), Label: s.label, Description: s.about})
	}
	return screen.SubMenu{
		Title:   component.TitleBar{Title: "Engine scenarios"},
		Heading: "Select a scenario",
		Menu:    component.Menu{Items: items, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:  "Enter your choice",
	}
}

// openEngine returns the action that opens what name names: the menu of
// the scenarios for enginePrefix alone, one scenario for
// "engine/<name>".
func (s *session) openEngine(name string) func() error {
	if sc, ok := findScenario(strings.TrimPrefix(name, enginePrefix+"/")); ok && name != enginePrefix {
		return func() error { return s.runScenario(sc) }
	}
	return func() error {
		vm, all := scenarioMenu(), scenarios()
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, it := range vm.Menu.Items {
			sc := all[i]
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.runScenario(sc) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// runScenario runs sc once: its note, then the canonical flow on the real
// engine over a fresh simulated host (output.FlowEngine), then what the
// flow does not draw (output.AfterFlow: the countdown, the plan's
// findings), and for a scenario that confirms, hardn confirm from a new
// connection. What the fake recorded is kept for the end of play.
func (s *session) runScenario(sc scenario) error {
	h := sc.host()
	st, err := sc.setupOn(h)
	if err != nil {
		return err
	}
	rec := &recorder{host: h, f: h.f, title: enginePrefix + "/" + sc.name}
	fe := &output.FlowEngine{DryRun: sc.dryRun, LogPath: output.LegacyLogPath}
	fe.Engine = engine.New(h.system(), engine.Options{
		Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
		Session:  applySession,
		Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
		Hold:     s.hold,
		PID:      applyPID,
		Progress: func(ev engine.Event) { rec.event(ev); fe.Progress(ev) },
	})
	s.engineRuns = append(s.engineRuns, rec)
	c, gap := s.p.Ctx, strings.Repeat("\n", theme.SectionGap)
	// The note is set apart as a workflow screen is (screen.WorkflowStep):
	// after a prompt, the gap goes before it and the flow writes its own
	// under it; before any, the gap follows it.
	note := component.Callout{Kind: component.MessageInfo, Title: rec.title, Body: sc.note}.Render(c) + "\n" + gap
	if s.p.In.Asked() {
		note = gap + strings.TrimSuffix(note, gap)
	}
	if _, err := io.WriteString(s.p.Out, note); err != nil {
		return err
	}
	f := flow.Flow{
		Form:   st.form,
		Engine: recording{fe, rec, st.plans},
		Confirm: func(p flow.Plan) flow.Confirm {
			if p.Typed() {
				return flow.Confirm{Question: sc.question, Label: typeTheHostname, Phrase: hostName}
			}
			return flow.Confirm{Question: sc.question}
		},
		DryRun: sc.dryRun,
	}
	o, err := f.Run(context.Background(), s.p)
	if err != nil {
		return err
	}
	rec.ended(o)
	if after := output.AfterFlow(c.Env, fe.Run(o, err), galleryNow); after != "" {
		if _, err := io.WriteString(s.p.Out, after); err != nil {
			return err
		}
	}
	if sc.confirm && o == flow.Applied {
		return s.confirmFromNewConnection(h)
	}
	return nil
}

// confirmFromNewConnection is hardn confirm from a new SSH connection to
// port 2230, as cmd/hardn runs it (confirmAndCleanUp, in a main package
// the gallery cannot import): the claim, then the clean-up of what the
// change held through the engine, its run rendered by pkg/output and its
// report (confirm.Held.Report) as messages, as cmd/hardn's showCleanup
// renders them. It departs from confirmAndCleanUp where no scenario reaches
// (docs/engine/gallery-engine.md §2.3): the claim's line, which hardn
// confirm prints through the legacy logger, is a pkg/ui message here, the
// gallery having no legacy logger; a claim's error is shown, with no
// acknowledgement of a rollback the timer made; the console's warning and
// the line before a carried or finishing clean-up are not drawn; and play
// exits 0 where hardn confirm would exit 1.
func (s *session) confirmFromNewConnection(h *fakeHost) error {
	c, gap := s.p.Ctx, strings.Repeat("\n", theme.SectionGap)
	rec := &recorder{host: h, f: h.f, title: "hardn confirm, from a new connection to port 2230", opening: "Confirmed: the revert timer stopped"}
	s.engineRuns = append(s.engineRuns, rec)
	// conn is alice's new connection: its sshd process starts after the
	// apply, so it was not running when MarkApplied listed them.
	conn := h.connect()
	ro := revert.Options{Now: clock}
	blocks := []string{component.Callout{Kind: component.MessageInfo, Title: rec.title, Body: []string{
		"alice opens a new SSH connection to " + hostFQDN + " on port 2230, from 192.0.2.10, and runs sudo hardn confirm.",
	}}.Render(c)}
	rec.begin()
	held, err := confirm.Claim(context.Background(), h.f, conn, ro)
	var o confirm.Outcome
	switch {
	case err != nil:
		blocks = append(blocks, component.Message{Kind: component.MessageBad, Text: err.Error()}.Render(c))
	default:
		blocks = append(blocks, component.Message{Kind: component.MessageOK, Text: fmt.Sprintf("Change %s confirmed (%s): it will not be rolled back.",
			held.Confirmed.Journal.ID, held.Confirmed.Journal.Summary)}.Render(c))
	}
	if held.Run() {
		o = confirm.Cleanup(context.Background(), h.f, held, conn, confirm.Options{Revert: ro, Hold: s.hold, PID: confirmPID, Progress: rec.event})
		rec.setPlan(o.Plan)
		rec.failedWith(o.Err)
		if run := (output.Run{Mode: output.ModeApply, Plan: o.Plan, Result: o.Result, Err: o.Err}); !o.Done() || !o.Result.Nothing {
			blocks = append(blocks, strings.TrimSuffix(output.Human(c.Env, run, galleryNow), "\n"))
		}
	}
	rec.end()
	r := held.Report(o)
	var run component.Messages
	for _, d := range r.Done {
		run = append(run, component.Message{Kind: component.MessageOK, Text: d})
	}
	for _, l := range r.Left {
		run = append(run, component.Message{Kind: component.MessageWarn, Text: l})
	}
	run = append(run, component.Message{Text: r.Finish})
	if out := run.Render(c); out != "" {
		blocks = append(blocks, out)
	}
	_, werr := io.WriteString(s.p.Out, gap+strings.Join(blocks, "\n"+gap)+"\n")
	return werr
}

// clock is the simulated host's clock (galleryNow).
func clock() time.Time { return galleryNow }

// hold is the signal hold the engine takes around an in-process rollback
// (engine.Options.Hold): in the gallery it holds nothing, so that play's
// own SIGINT handling (the line reader's) stays as it is, and counts how
// often it was taken.
func (s *session) hold() func() {
	s.mu.Lock()
	s.holds++
	s.mu.Unlock()
	return func() {}
}

// recording is a scenario's flow.Engine: output.FlowEngine, its controls
// and configuration built by plans when the flow plans, with the fake's
// record marked where the apply begins and ends.
type recording struct {
	*output.FlowEngine
	rec   *recorder
	plans func() ([]engine.Control, *config.Config)
}

// Plan plans as FlowEngine does, what plans builds from the form's
// answers, and keeps the plan for the record.
func (r recording) Plan(ctx context.Context) (flow.Plan, error) {
	r.Controls, r.Config = r.plans()
	p, err := r.FlowEngine.Plan(ctx)
	r.rec.setPlan(r.FlowEngine.Run(flow.NotRun, nil).Plan)
	return p, err
}

// Apply applies as FlowEngine does, the record taken from its start, and
// keeps how the engine's apply failed.
func (r recording) Apply(ctx context.Context, report flow.Report) error {
	r.rec.begin()
	defer r.rec.end()
	err := r.FlowEngine.Apply(ctx, report)
	r.rec.failedWith(r.FlowEngine.Run(flow.NotRun, nil).Err)
	return err
}

// recorder keeps what the fake recorded of one apply: the calls it made
// from the apply's start to its end, and where each of the engine's
// events came among them, so that the record can say what each step ran
// and wrote on the simulated host.
type recorder struct {
	host  *fakeHost
	f     *hostfake.System
	title string
	// opening titles what the fake recorded before the first change:
	// "Before the first change" when empty.
	opening string

	mu       sync.Mutex
	plan     *engine.Plan // the plan whose changes the events name
	from, to int          // the fake's call count when the apply began and ended; to < 0 while it runs
	marks    []mark
	began    bool
	outcome  flow.Outcome // how the scenario's flow ended
	// failure is how the apply failed, as the engine returned it: nil
	// when it did not fail, or failed with no rollback (the boot unit's
	// install, before anything is armed).
	failure *engine.ApplyError
}

// mark is one of the engine's events and the fake's call count when it
// came.
type mark struct {
	ev engine.Event
	at int
}

func (r *recorder) begin() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.from, r.to, r.began = len(r.f.Calls()), -1, true
}

func (r *recorder) end() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.to = len(r.f.Calls())
}

// ended keeps how the scenario's flow ended.
func (r *recorder) ended(o flow.Outcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outcome = o
}

// failedWith keeps how the apply failed, from its error: the record's last
// entry says whether its rollback finished.
func (r *recorder) failedWith(err error) {
	var failure *engine.ApplyError
	errors.As(err, &failure)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failure = failure
}

// setPlan keeps the plan whose changes the events name by index.
func (r *recorder) setPlan(p *engine.Plan) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plan = p
}

func (r *recorder) event(ev engine.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.marks = append(r.marks, mark{ev, len(r.f.Calls())})
}

// stateDirs are hardn's own: the journal, the run directory and its lock,
// and the backups. The record leaves their writes out, and says so.
var stateDirs = []string{revert.DefaultDir, "/run/hardn", backup.DefaultPath}

// reads are the commands that only read the host or check a file: sshd
// -T and sshd -t, ss, ufw's status, systemctl's queries, resolvectl's
// lists as pkg/host/dns asks them (dns and domain with nothing after:
// given a link and values they set the link's), visudo's checks, the
// boot unit's checks (systemd-analyze verify, sh -n) on its copy, and
// detect's questions (sudo --version, dpkg-query).
func reads(argv []string) bool {
	switch path.Base(argv[0]) {
	case "sshd", "ss", "systemd-analyze", "visudo", "dpkg-query":
		return true
	case "resolvectl":
		return len(argv) == 3 && argv[1] == "--no-pager" && (argv[2] == "dns" || argv[2] == "domain")
	case "sudo":
		return len(argv) == 2 && argv[1] == "--version"
	case "systemctl":
		return slices.Contains([]string{"is-active", "is-enabled", "show", "list-timers"}, argv[1])
	case "sh":
		return len(argv) > 1 && argv[1] == "-n"
	}
	return strings.Join(argv, " ") == "ufw status verbose"
}

// changed is what calls changed on the host, outside hardn's own state
// directories: the commands run or started that do not only read, a
// command that failed among them, and the files and directories made,
// written, chmodded, chowned, removed or renamed, each once, in order (a
// directory made: the DNS drop-in's).
func changed(calls []hostfake.Call) (commands, files []string) {
	for _, c := range calls {
		switch c.Op {
		case "run", "start-detached":
			if !reads(c.Argv) {
				commands = append(commands, host.FormatArgv(c.Argv))
			}
		case "write-atomic", "write-in-place", "append", "remove", "removeall", "rename", "mkdir", "chmod", "chown":
			if c.Err == nil && !slices.ContainsFunc(stateDirs, func(d string) bool { return c.Path == d || strings.HasPrefix(c.Path, d+"/") }) &&
				!slices.Contains(files, c.Path) {
				files = append(files, c.Path)
			}
		}
	}
	return commands, files
}

// arms reports whether calls start the revert timer: systemd-run, or
// OpenRC's sleeper, started detached (revert.Arm).
func arms(calls []hostfake.Call) bool {
	return slices.ContainsFunc(calls, func(c hostfake.Call) bool {
		return c.Op == "start-detached" || c.Op == "run" && path.Base(c.Argv[0]) == "systemd-run"
	})
}

// steps returns the record of the apply as plan view changes: for each
// change of the plan that ran, what it ran and the files it changed, under
// how it ended ("Applied: ", "Failed: "); between them, what the engine
// itself did (the revert timer armed); after a failure, the rollback,
// said finished only when the engine's was (failure); nothing for an
// apply that never began.
func (r *recorder) steps() []component.PlanChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.began {
		return nil
	}
	calls := r.f.Calls()
	to := r.to
	if to < 0 {
		to = len(calls)
	}
	var out []component.PlanChange
	add := func(title string, from, upto int) {
		cmds, files := changed(calls[from:upto])
		if len(cmds) == 0 && len(files) == 0 {
			return
		}
		out = append(out, component.PlanChange{Title: title, Target: strings.Join(files, ", "), Commands: cmds})
	}
	last, failed, started := r.from, false, -1
	for _, m := range r.marks {
		title := m.ev.Change
		if r.plan != nil && m.ev.Index >= 0 && m.ev.Index < len(r.plan.Changes) {
			title = r.plan.Changes[m.ev.Index].Heading()
		}
		switch m.ev.State {
		case engine.EventRunning:
			gap := "Between the changes"
			switch {
			case arms(calls[last:m.at]):
				gap = "Armed the revert timer"
			case last == r.from && r.opening != "":
				gap = r.opening
			case last == r.from:
				gap = "Before the first change"
			}
			add(gap, last, m.at)
			last, started = m.at, m.ev.Index
		case engine.EventDone:
			add("Applied: "+title, last, m.at)
			last, started = m.at, -1
		case engine.EventFailed:
			what := "Failed: " + title
			if started < 0 {
				what = "Failed: the apply, at its " + m.ev.Step + " step"
			}
			add(what, last, m.at)
			last, failed = m.at, true
		case engine.EventRolledBack:
		}
	}
	switch {
	case !failed:
		add("After the last change", last, to)
	case r.failure == nil:
		add("After the failure", last, to)
	case r.failure.RollbackErr != nil:
		add("Rollback not finished", last, to)
	default:
		add("Rolled back from the journal", last, to)
	}
	return out
}
