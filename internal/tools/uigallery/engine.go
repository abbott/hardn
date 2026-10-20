package main

import (
	"context"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/confirm"
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
// the gallery's, until the slices word their actions (P-10).

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
	// host returns the simulated host, fresh; facts are what detect
	// would find there.
	host  func() *fakeHost
	facts detect.Facts
	// controls are what the engine plans.
	controls []engine.Control
	// question is the flow's confirmation question.
	question string
	// dryRun stops the flow after the plan.
	dryRun bool
	// confirm, once the change is applied, confirms it from a new SSH
	// connection to port 2230 with hardn confirm, which then cleans up
	// what the change held (P-40).
	confirm bool
	// answers are what the user types to play it as it says: the
	// hostname for a typed confirmation, y for a y/N one, nothing for a
	// plan refused or a dry run; ends is how the flow then ends.
	answers []string
	ends    flow.Outcome
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

// The platforms of the scenarios, as detect would find them on the
// simulated hosts.
var (
	// debian13 is a stock Debian 13: ssh.service, sudo.
	debian13 = detect.Facts{
		OS:       detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie"},
		Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init:     detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
		SSH: detect.SSH{Mode: detect.SSHService, Generator: detect.No,
			Socket:  detect.UnitState{Enabled: "disabled", Active: "inactive"},
			Service: detect.UnitState{Enabled: "enabled", Active: "active"}},
		Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
		Container: detect.Container{Detected: detect.No},
		Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, SudoFlavor: detect.SudoOriginal,
			Doas: detect.Tool{Present: detect.No}},
	}
	// ubuntu2604 is a stock Ubuntu 26.04: ssh.socket from the generator.
	ubuntu2604 = func() detect.Facts {
		f := debian13
		f.OS = detect.OSRelease{ID: "ubuntu", VersionID: "26.04"}
		f.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}
		f.SSH = detect.SSH{Mode: detect.SSHSocket, Generator: detect.Yes, GeneratorPath: detect.SocketGenerator,
			Socket:  detect.UnitState{Enabled: "enabled", Active: "active"},
			Service: detect.UnitState{Enabled: "disabled", Active: "active"}}
		return f
	}()
	// ubuntu2204 is Ubuntu 22.04, a best-effort release (brief §2): its
	// sshd started by ssh.service.
	ubuntu2204 = func() detect.Facts {
		f := debian13
		f.OS = detect.OSRelease{ID: "ubuntu", VersionID: "22.04"}
		f.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "22.04", Support: detect.BestEffort}
		return f
	}()
	// alpine324 is a stock Alpine 3.24: OpenRC's sshd, doas, no ufw.
	alpine324 = func() detect.Facts {
		f := debian13
		f.OS = detect.OSRelease{ID: "alpine", VersionID: "3.24.0"}
		f.Platform = detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported}
		f.Init = detect.Init{Kind: detect.OpenRC, Running: detect.Yes}
		f.SSH = detect.SSH{Mode: detect.SSHOpenRC, OpenRC: detect.OpenRCService{Started: detect.Yes}}
		f.Elevation = detect.Elevation{Sudo: detect.Tool{Present: detect.No}, Doas: detect.Tool{Present: detect.Yes, Path: "/usr/bin/doas"}}
		return f
	}()
)

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
	return []scenario{
		{name: "ssh-port-debian13", label: "SSH port, Debian 13", about: "Port 2230 through ssh.service",
			note: []string{hostFQDN + " runs Debian 13; sshd is started by ssh.service. " + stock, "The change: SSH port 2230, port 22 held until confirmed. " + confirm},
			host: newHost, facts: debian13, controls: []engine.Control{port},
			question: "Change the SSH port to 2230?", answers: typed, ends: flow.Applied},
		{name: "ssh-port-ubuntu2604", label: "SSH port, Ubuntu 26.04", about: "Port 2230 through ssh.socket",
			note: []string{hostFQDN + " runs Ubuntu 26.04; ssh.socket listens, from sshd-socket-generator. " + stock, "The change: SSH port 2230, port 22 held until confirmed. " + confirm},
			host: socketHost, facts: ubuntu2604, controls: []engine.Control{port},
			question: "Change the SSH port to 2230?", answers: typed, ends: flow.Applied},
		{name: "ssh-port-alpine324", label: "SSH port, Alpine 3.24", about: "Port 2230 through OpenRC",
			note: []string{hostFQDN + " runs Alpine 3.24; sshd is OpenRC's, and ufw is not installed. Elevation is doas.", "The change: SSH port 2230, port 22 held until confirmed. " + confirm},
			host: openRCHost, facts: alpine324, controls: []engine.Control{port},
			question: "Change the SSH port to 2230?", answers: typed, ends: flow.Applied},
		{name: "keyonly-debian13", label: "Key-only login", about: "No password login, Debian 13",
			note: []string{hostFQDN + " runs Debian 13. " + stock, "The change: SSH login by key only. " + confirm},
			host: newHost, facts: debian13, controls: []engine.Control{keyOnly()},
			question: "Allow SSH login by key only?", answers: typed, ends: flow.Applied},
		{name: "ufw-enable-debian13", label: "Enable ufw", about: "From an inactive ufw, Debian 13",
			note: []string{hostFQDN + " runs Debian 13; sshd listens on port 22, and ufw is installed, inactive, with no rules.", "The change: ufw enabled, sshd's listeners allowed first. " + confirm},
			host: inactiveUFW, facts: debian13, controls: []engine.Control{ufwEnable()},
			question: "Enable ufw?", answers: typed, ends: flow.Applied},
		{name: "sudoers-grant-debian13", label: "Sudo grant", about: "alice without a password, Debian 13",
			note: []string{hostFQDN + " runs Debian 13, with sudo and its stock /etc/sudoers.d.", "The change: alice may use sudo without a password, which locks no one out, so it is confirmed y/N."},
			host: newHost, facts: debian13, controls: []engine.Control{sudoersGrant()},
			question: "Let alice use sudo without a password?", answers: []string{"y"}, ends: flow.Applied},
		{name: "refused-d9-ubuntu2604", label: "Refused: v0.3.2 listen.conf", about: "The migration on a D9 host (P-39)",
			note: []string{hostFQDN + " runs Ubuntu 26.04, hardened by hardn v0.3.2: its manage.conf, and a listen.conf for port 2230 written with no daemon-reload, so ssh.socket still listens on 22.",
				"The change: hardn's v0.3.2 SSH files moved into 00-hardn.conf. The plan refuses it, with a report (P-39 (a))."},
			host: d9Host, facts: ubuntu2604, controls: []engine.Control{migration()},
			question: "Move hardn's v0.3.2 SSH files into 00-hardn.conf?", ends: flow.Failed},
		{name: "failed-reload-debian13", label: "Failed, rolled back", about: "Port 2230, the reload refused",
			note: []string{hostFQDN + " runs Debian 13. " + stock + " Its next reload of ssh.service fails.", "The change: SSH port 2230. " + confirm},
			host: failingReload, facts: debian13, controls: []engine.Control{port},
			question: "Change the SSH port to 2230?", answers: typed, ends: flow.Failed},
		{name: "confirm-cleanup-debian13", label: "Confirmed, cleaned up", about: "Port 2230, then hardn confirm (P-40)",
			note: []string{hostFQDN + " runs Debian 13; sshd listens on port 22, and ufw is installed, inactive, with no rules.",
				"The change: SSH port 2230 with ufw enabled, port 22 and its rule held until confirmed. " + confirm,
				"Then a new SSH connection to port 2230 confirms it with hardn confirm, which drops port 22 and the rule hardn added for it (P-40)."},
			host: inactiveUFW, facts: debian13, controls: []engine.Control{sshControl(steps.SSH{Port: 2230, UFW: steps.UFWEnable})},
			question: "Change the SSH port to 2230 and enable ufw?", confirm: true, answers: typed, ends: flow.Applied},
		{name: "best-effort-ubuntu2204", label: "Best-effort release", about: "The plan's warning, Ubuntu 22.04 (P-41)",
			note: []string{hostFQDN + " runs Ubuntu 22.04, a best-effort release: hardn changes it, but is not tested on it. " + stock, "The plan alone, for key-only login: it carries the release's warning (P-41)."},
			host: newHost, facts: ubuntu2204, controls: []engine.Control{keyOnly()},
			question: "Allow SSH login by key only?", dryRun: true, ends: flow.Planned},
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
	rec := &recorder{host: h, f: h.f, title: enginePrefix + "/" + sc.name}
	fe := &output.FlowEngine{Controls: sc.controls, DryRun: sc.dryRun, LogPath: output.LegacyLogPath}
	fe.Engine = engine.New(h.f, engine.Options{
		Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
		Session:  revert.Session{Remote: true, Conn: applyConn, Source: "SSH_CONNECTION"},
		Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
		Detect:   detected(sc.facts),
		Hold:     s.hold,
		PID:      5000,
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
		Engine: recording{fe, rec},
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
	if after := output.AfterFlow(c.Env, fe.Run(o, err), galleryNow); after != "" {
		if _, err := io.WriteString(s.p.Out, after); err != nil {
			return err
		}
	}
	if sc.confirm && o == flow.Applied {
		return s.confirmFromNewConnection(h, sc.facts)
	}
	return nil
}

// confirmFromNewConnection is hardn confirm from a new SSH connection to
// port 2230, as cmd/hardn runs it (confirmAndCleanUp, in a main package
// the gallery cannot import): the claim, then the clean-up of what the
// change held through the engine, its run rendered by pkg/output and its
// report (confirm.Held.Report) as messages, as cmd/hardn's showCleanup
// renders them. The claim's line, which hardn confirm prints through the
// legacy logger, is a pkg/ui message here, the gallery having no legacy
// logger.
func (s *session) confirmFromNewConnection(h *fakeHost, facts detect.Facts) error {
	c, gap := s.p.Ctx, strings.Repeat("\n", theme.SectionGap)
	rec := &recorder{host: h, f: h.f, title: "hardn confirm, from a new connection to port 2230", opening: "Confirmed: the revert timer stopped"}
	s.engineRuns = append(s.engineRuns, rec)
	// conn is alice's new connection: its sshd-session started after the
	// apply, so it was not running when MarkApplied listed them.
	conn := revert.Session{Remote: true, Conn: confirmConn, SSHD: revert.ProcStart{PID: 4100, Start: 410000}, Source: "SSH_CONNECTION"}
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
		o = confirm.Cleanup(context.Background(), h.f, held, conn, confirm.Options{Revert: ro, Detect: detected(facts), Hold: s.hold, PID: 4000, Progress: rec.event})
		rec.setPlan(o.Plan)
		if run := (output.Run{Mode: output.ModeApply, Plan: o.Plan, Result: o.Result, Err: o.Err}); !o.Done() || !o.Result.Nothing {
			blocks = append(blocks, strings.TrimSuffix(output.Human(c.Env, run, galleryNow), "\n"))
		}
	}
	rec.end()
	r := held.Report(o)
	var lines []string
	for _, d := range r.Done {
		lines = append(lines, component.Message{Kind: component.MessageOK, Text: d}.Render(c))
	}
	for _, l := range r.Left {
		lines = append(lines, component.Message{Kind: component.MessageWarn, Text: l}.Render(c))
	}
	if r.Finish != "" {
		lines = append(lines, component.Message{Text: r.Finish}.Render(c))
	}
	if len(lines) > 0 {
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	_, werr := io.WriteString(s.p.Out, gap+strings.Join(blocks, "\n"+gap)+"\n")
	return werr
}

// clock is the simulated host's clock (galleryNow).
func clock() time.Time { return galleryNow }

// detected returns detect's facts for a scenario's host: facts, as detect
// would find them there.
func detected(facts detect.Facts) func(context.Context, host.System) (detect.Facts, error) {
	return func(context.Context, host.System) (detect.Facts, error) { return facts, nil }
}

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

// recording is a scenario's flow.Engine: output.FlowEngine, with the
// fake's record marked where the apply begins and ends.
type recording struct {
	*output.FlowEngine
	rec *recorder
}

// Plan plans as FlowEngine does, and keeps the plan for the record.
func (r recording) Plan(ctx context.Context) (flow.Plan, error) {
	p, err := r.FlowEngine.Plan(ctx)
	r.rec.setPlan(r.FlowEngine.Run(flow.NotRun, nil).Plan)
	return p, err
}

// Apply applies as FlowEngine does, the record taken from its start.
func (r recording) Apply(ctx context.Context, report flow.Report) error {
	r.rec.begin()
	defer r.rec.end()
	return r.FlowEngine.Apply(ctx, report)
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
// -T and sshd -t, ss, ufw's status, systemctl's queries, visudo's checks,
// and the boot unit's checks (systemd-analyze verify, sh -n) on its copy.
func reads(argv []string) bool {
	switch path.Base(argv[0]) {
	case "sshd", "ss", "systemd-analyze", "visudo":
		return true
	case "systemctl":
		return slices.Contains([]string{"is-active", "is-enabled", "show", "list-timers"}, argv[1])
	case "sh":
		return len(argv) > 1 && argv[1] == "-n"
	}
	return strings.Join(argv, " ") == "ufw status verbose"
}

// changed is what calls changed on the host, outside hardn's own state
// directories: the commands run or started that do not only read, a
// command that failed among them, and the files written, removed or
// renamed, each once, in order.
func changed(calls []hostfake.Call) (commands, files []string) {
	for _, c := range calls {
		switch c.Op {
		case "run", "start-detached":
			if !reads(c.Argv) {
				commands = append(commands, host.FormatArgv(c.Argv))
			}
		case "write-atomic", "write-in-place", "append", "remove", "removeall", "rename":
			if c.Err == nil && !slices.ContainsFunc(stateDirs, func(d string) bool { return c.Path == d || strings.HasPrefix(c.Path, d+"/") }) &&
				!slices.Contains(files, c.Path) {
				files = append(files, c.Path)
			}
		}
	}
	return commands, files
}

// steps returns the record of the apply as plan view changes: for each
// change of the plan that ran, what it ran and the files it changed, under
// how it ended ("Applied: ", "Failed: "); between them, what the engine
// itself did (the revert timer armed, the rollback); nothing for an apply
// that never began.
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
			title = r.plan.Changes[m.ev.Index].Summary
		}
		switch m.ev.State {
		case engine.EventRunning:
			gap := r.opening
			switch {
			case last > r.from:
				gap = "Armed the revert timer"
			case gap == "":
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
	case failed:
		add("Rolled back from the journal", last, to)
	default:
		add("After the last change", last, to)
	}
	return out
}
