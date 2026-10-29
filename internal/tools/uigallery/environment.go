package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/environment"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Environment mocks (refactor plan slice 9, unit (a), next/s9-gallery):
// a proposal of the Environment screen, main menu 8, for the maintainer's
// review, nothing of it wired into hardn. There is no Figma frame: it
// starts from origin/main's two status lines and two items
// (pkg/menu/environment_settings_menu.go:36-56) on the design system. It
// is drawn from the Environment control's reading of a simulated host
// (fakehost.go; environment.Read, since next/s9-control): the elevation
// tool as sudoers.Choose picks it from detect's facts, who ran hardn
// (sudoers.Invoker), the user's Policy from hardn's files
// (sudoers.PolicyOf), origin/main's own line (sudoers.ReadLegacy), the
// files read after hardn's rule, and why item 1 is refused before it
// asks. Each change is the canonical flow on the real engine
// (screenkit.Session.Change, as the DNS and Backup screens run theirs):
// its plan the control's (environment.Keep), as the engine's change
// steps.Sudoers, journalled as plans.Sudoers' step, the plan's findings
// on it. The screen's words are the gallery's (unit (c)'s); the state's,
// the change's and the refusals' are the control's. The mocks of the
// options the maintainer did not adopt (decisions 1 (b), 3 (b) and 5 (b),
// and the plan without the reader) were retired with them
// (gallery-environment.md §3.1; git keeps them at af5904a).
//
// "environment" lists the mocks with no input; "play environment" is the
// menu of the hosts and "play environment/<host>" the screen on one.

// envPrefix names the mocks: "environment <mock>" in the listing,
// "environment" and "environment/<host>" in play.
const envPrefix = "environment"

// hardnConfig is the one variable the screen keeps (P-07 (c)).
const hardnConfig = environment.Variable

// The screen's words: origin/main's title (environment_settings_menu.go:34),
// Backup's decided heading and prompt (screen-backup.md §5, P-29 (a)), the
// rest decided with decision 7 (P-07 (c) for "Keep HARDN_CONFIG"). The
// state's words (Kept, Not kept, their notes, "Not run with sudo", "<file>
// may override hardn's rule"), the change's summary and the refusals are
// the control's (environment.Words, Reading.NotRun, Reading.Later,
// Reading.Refusal).
const (
	envTitle   = "Environment Variable Settings"
	envSection = hardnConfig
	envHeading = "Select an option"
	envPrompt  = "Enter selection"
	valueKey   = "Value"
	userKey    = "User"
	notSet     = "Not set"
	keepIt     = "Keep HARDN_CONFIG"
	stopIt     = "Stop keeping HARDN_CONFIG"
	guideLabel = "Guide"
	guideAbout = "How to set HARDN_CONFIG"
	// The flows' title bars and questions.
	keepTitle = "Keep HARDN_CONFIG"
	stopTitle = "Stop Keeping HARDN_CONFIG"
	askKeep   = "Keep HARDN_CONFIG under %s?"
	askStop   = "Stop keeping HARDN_CONFIG under %s?"
	// takesEffect is said after a change applied: this hardn was started
	// without it, so its Value row does not change.
	takesEffect = "Takes effect at the next %s hardn."
)

// guideWords are item 2's (origin/main's guide, environment_settings_menu.go:
// 120-139, shortened, its "sudo hardn setup-sudo-env" now item 1, whose
// label names the tool): decided with decision 7.
var guideWords = []string{
	"HARDN_CONFIG names the hardn.yml hardn reads. Set it in your shell profile:",
	"export HARDN_CONFIG=/path/to/hardn.yml",
	"Then keep it with item 1.",
}

// The menu's keys.
const (
	keyKeep  = "1"
	keyGuide = "2"
)

// envHost is one simulated host the Environment mocks run on.
type envHost struct {
	// name is the host's, after envPrefix: "debian13".
	name string
	// label and about are its item in the menu of the hosts.
	label, about string
	// note says what the host is.
	note string
	// host returns the simulated host, fresh.
	host func() *fakeHost
	// env is hardn's environment there, what the screen reads of it:
	// SUDO_USER or DOAS_USER, USER, HARDN_CONFIG.
	env map[string]string
}

// The environments hardn runs in: alice's sudo or doas, HARDN_CONFIG
// dropped by it or kept; root's login.
var (
	sudoAlice = map[string]string{"SUDO_USER": "alice", "USER": "root"}
	doasAlice = map[string]string{"DOAS_USER": "alice", "USER": "root"}
	keptAlice = map[string]string{"SUDO_USER": "alice", "USER": "root", hardnConfig: "/home/alice/hardn.yml"}
	doasKept  = map[string]string{"DOAS_USER": "alice", "USER": "root", hardnConfig: "/home/alice/hardn.yml"}
	rootLogin = map[string]string{"USER": "root"}
)

// envHosts are the hosts, in the menu's order, each drawing the options
// the maintainer adopted (gallery-environment.md §5).
func envHosts() []envHost {
	const (
		deb    = hostFQDN + " runs Debian 13, sudo 1.9.16p2. "
		viaSud = "alice ran sudo hardn; "
		drop   = "sudo dropped HARDN_CONFIG."
		setup  = hostFQDN + " runs Alpine 3.24 after setup-alpine, doas; alice is in wheel. alice ran doas hardn. "
		wheel  = setup + "/etc/doas.d/20-wheel.conf says permit persist :wheel. "
		cloud  = hostFQDN + " runs Alpine 3.23's cloud image, doas, as the maintainer's VM; alice is in wheel, and the image's /etc/doas.d/wheel.conf says permit nopass :wheel. alice ran doas hardn. "
	)
	return []envHost{
		{name: "debian13", label: "Debian 13", about: "sudo, not kept", host: newHost, env: sudoAlice,
			note: deb + viaSud + drop},
		{name: "ubuntu2604", label: "Ubuntu 26.04", about: "sudo-rs, not kept", host: socketHost, env: sudoAlice,
			note: hostFQDN + " runs Ubuntu 26.04, sudo-rs 0.2.13. " + viaSud + drop},
		{name: "alpine324", label: "Alpine 3.24", about: "doas through wheel", host: alpineWheel, env: doasAlice,
			note: wheel + "hardn has no rule for alice, so item 1 refuses before asking."},
		{name: "alpine324-setenv", label: "Alpine, setenv on wheel", about: "The refusal followed", host: wheelSetenv, env: doasKept,
			note: setup + "alice did what item 1's refusal says: 20-wheel.conf says permit persist setenv { HARDN_CONFIG } :wheel, and doas kept /home/alice/hardn.yml. hardn has no rule for alice, so the screen says Not kept by hardn and item 1 refuses again, as its refusal said."},
		{name: "doas-overridden", label: "doas, overridden", about: "hardn's rule, wheel.conf after", host: doasOverridden, env: doasAlice,
			note: cloud + "hardn's rule for alice keeps HARDN_CONFIG, but wheel.conf's wins, so doas dropped it. Stopping rewrites a rule that asks for alice's password."},
		{name: "pve9", label: "Proxmox VE 9", about: "no sudo", host: func() *fakeHost { return simulate(pve9Platform) }, env: rootLogin,
			note: hostFQDN + " is a Proxmox VE 9 node: no sudo, no doas. root logged in."},
		{name: "kept", label: "Kept", about: "hardn-env-alice there", host: keptHost, env: keptAlice,
			note: deb + "hardn's /etc/sudoers.d/hardn-env-alice keeps HARDN_CONFIG; " + viaSud + "it kept /home/alice/hardn.yml."},
		{name: "root-login", label: "Root login", about: "Not run with sudo", host: newHost, env: rootLogin,
			note: deb + "root logged in and ran hardn: no SUDO_USER."},
		{name: "grant", label: "hardn's grant", about: "hardn-grant-alice there", host: grantHost, env: sudoAlice,
			note: deb + "hardn's /etc/sudoers.d/hardn-grant-alice lets alice use sudo without a password; " + viaSud + drop},
		{name: "legacy", label: "Legacy file", about: "/etc/sudoers.d/alice", host: legacyHost, env: keptAlice,
			note: deb + "origin/main's /etc/sudoers.d/alice holds alice's grant and its env_keep line; " + viaSud + "it kept /home/alice/hardn.yml."},
	}
}

// findEnvHost returns the host named name, after envPrefix.
func findEnvHost(name string) (envHost, bool) {
	i := slices.IndexFunc(envHosts(), func(h envHost) bool { return h.name == name })
	if i < 0 {
		return envHost{}, false
	}
	return envHosts()[i], true
}

// withDoasFiles gives h doas's configuration as Alpine's package installs
// it, /etc/doas.conf 0640 with its rule commented out and /etc/doas.d 0750
// (sudoers.md §3: aports 3.24-stable's APKBUILD and doas.conf; on the 3.23
// cloud image, /etc/doas.conf's content was not read [I]), and the
// drop-in name holding rule (the model's mode, 0640 [I]).
func withDoasFiles(h *fakeHost, name, rule string) *fakeHost {
	h.f.AddFile(sudoers.DoasConfig, []byte("# Uncomment to allow group \"wheel\" to become root.\n# permit persist :wheel\n"), 0o640, 0, 0)
	h.f.AddDir(sudoers.DoasDir, 0o750, 0, 0)
	h.f.AddFile(sudoers.DoasDir+"/"+name, []byte(rule), 0o640, 0, 0)
	return h
}

// alpineWheel is Alpine 3.24 after setup-alpine: 20-wheel.conf, "permit
// persist :wheel" (setup-user, sudoers.md §3; the release aports'
// 3.24-stable sources cover).
func alpineWheel() *fakeHost {
	return withDoasFiles(simulate(alpine324Platform), "20-wheel.conf", "permit persist :wheel\n")
}

// wheelSetenv is alpineWheel after its user did what item 1's refusal
// says (the control's): setenv on 20-wheel.conf's rule, a group's, which
// keeps the variable for every member (doas.conf(5), sudoers.md:94 [I]:
// doas's setenv has run on no host).
func wheelSetenv() *fakeHost {
	return withDoasFiles(simulate(alpine324Platform), "20-wheel.conf", "permit persist setenv { HARDN_CONFIG } :wheel\n")
}

// alpineCloud is Alpine 3.23's cloud image as the maintainer's VM: only
// wheel.conf, "permit nopass :wheel" (docs/safety/vmtest.md:562, :570),
// which ships in the image (:580) and sorts after hardn-<stem>.conf.
func alpineCloud() *fakeHost {
	return withDoasFiles(simulate(alpine323CloudPlatform), "wheel.conf", "permit nopass :wheel\n")
}

// withHardnFiles writes hardn's files for pol on h as sudoers.PlanFor
// plans them, so that their bytes are the package's own.
func withHardnFiles(h *fakeHost, pol sudoers.Policy) *fakeHost {
	ctx := context.Background()
	f, err := detect.Detect(ctx, h.system())
	if err != nil {
		panic(err) // not reached: the simulated hosts are detected (TestEnvHosts)
	}
	st, err := sudoers.Read(ctx, h.system(), sudoers.Choose(f, sudoers.ToolUnknown), []string{pol.User})
	if err != nil {
		panic(err)
	}
	p, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
	if err != nil {
		panic(err)
	}
	for _, c := range p.Changes {
		h.f.AddFile(c.Path, c.Data, c.Mode, 0, 0)
	}
	return h
}

// keptHost is Debian 13 with hardn's env_keep file for alice.
func keptHost() *fakeHost {
	return withHardnFiles(newHost(), sudoers.Policy{User: "alice", Env: []string{hardnConfig}})
}

// grantHost is Debian 13 with hardn's grant for alice, without a password.
func grantHost() *fakeHost {
	return withHardnFiles(newHost(), sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword})
}

// doasOverridden is the cloud image with hardn's rule for alice keeping
// HARDN_CONFIG, a rule that asks her password, persist, as Alpine's own
// 20-wheel.conf (sudoers.md §3): nothing in hardn writes a first rule
// for a user today (decision 1 (a)); a later unit may.
func doasOverridden() *fakeHost {
	return withHardnFiles(alpineCloud(), sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword, Env: []string{hardnConfig}})
}

// legacyLines are origin/main's /etc/sudoers.d/<user> once both of its
// writers ran: ConfigureSudo's grant, then setup-sudo-env's line appended
// (pkg/characterization's ops/setup_sudo_preservation/debian.golden, step
// 9).
const legacyLines = "alice ALL=(ALL) NOPASSWD: ALL\nDefaults:alice env_keep += \"HARDN_CONFIG\"\n"

// legacyHost is Debian 13 with origin/main's /etc/sudoers.d/alice.
func legacyHost() *fakeHost {
	h := newHost()
	h.f.AddFile(sudoers.DropInDir+"/alice", []byte(legacyLines), 0o440, 0, 0)
	return h
}

// lookup is getenv over env, hardn's environment on a host.
func lookup(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

// envReading is one reading of a host's environment, as the screen draws
// it: the Environment control's (environment.Reading).
type envReading struct {
	environment.Reading
}

// readEnv reads what the screen draws of sys, hardn's environment env, as
// the engine detects it (detect.Detect), with the Environment control
// (environment.Read). It changes nothing.
func readEnv(ctx context.Context, sys host.System, env map[string]string) envReading {
	f, err := detect.Detect(ctx, sys)
	if err != nil {
		// The tool is then not established: said as Unknown, and item 1
		// refuses with the error.
		return envReading{environment.Reading{Choice: sudoers.Choice{Err: err}, Value: env[hardnConfig], Err: err}}
	}
	return envReading{environment.Read(ctx, sys, f, lookup(env))}
}

// keeps is the way item 1 turns: keep, unless hardn's files keep it
// already (decision 5 (a)).
func (r envReading) keeps() bool { return !r.Kept }

// refusal is what item 1 says before asking, nil when it asks: the
// control's refusal (Reading.Refusal), in a screen's words
// (screenkit.RefusalWords: its Why, then its Fix).
func (r envReading) refusal() component.Messages {
	if err := r.Refusal(); err != nil {
		return screenkit.Warnings(screenkit.RefusalWords(err))
	}
	return nil
}

// underKey is the state row's key: "Under sudo", "Under doas".
func (r envReading) underKey() string { return "Under " + r.ToolName() }

// stateRow is the state row for hardn's files keeping HARDN_CONFIG or
// not, in the control's words (environment.Words): Not kept in
// status.warn; Kept, in status.warn where a file the tool reads after
// hardn's may override it, as what wins is not read (sudoers.md §6). The
// screen lists each such file under it (rows), the review the plan's
// finding of it (review).
func (r envReading) stateRow(kept bool) component.KeyValue {
	value, note := environment.Words(r.Choice.Tool, kept)
	row := component.KeyValue{Key: r.underKey(), Value: value, Note: note, State: component.ValueOK}
	if !kept || len(r.Later()) > 0 {
		row.State = component.ValueWarn
	}
	return row
}

// rows are the section's rows: the user who ran hardn through the tool,
// whether HARDN_CONFIG is kept for them, with a line for each file read
// after hardn's, and its value as hardn sees it.
func (r envReading) rows() []component.KeyValue {
	var rows []component.KeyValue
	switch {
	case r.Choice.Tool == sudoers.ToolNone:
		rows = append(rows, component.KeyValue{Key: r.underKey(), State: component.ValueNotApplicable})
	case r.Choice.Err != nil, r.User == "" && !errors.Is(r.Err, environment.ErrNoUser):
		rows = append(rows, component.KeyValue{Key: r.underKey(), State: component.ValueUnknown})
	case r.User == "":
		rows = append(rows, component.KeyValue{Key: userKey, Value: r.NotRun(), State: component.ValueWarn})
	default:
		rows = append(rows, component.KeyValue{Key: userKey, Value: r.User})
		under := component.KeyValue{Key: r.underKey(), State: component.ValueUnknown}
		if r.Err == nil {
			// The control's words (Reading.Words): kept by hardn's files,
			// by origin/main's line, "Kept by /etc/sudoers.d/alice"
			// (decision 3 (a)), which no later file's finding concerns, or
			// not kept.
			value, note := r.Words()
			under = r.stateRow(value == environment.WordKept)
			under.Value, under.Note = value, note
			if !r.Kept && r.Legacy.Keeps {
				under.State = component.ValueOK
			}
		}
		rows = append(rows, under)
		if r.Kept && r.Err == nil {
			for _, f := range r.Later() {
				rows = append(rows, component.KeyValue{Key: f.Detail, State: component.ValueWarn})
			}
		}
	}
	value := component.KeyValue{Key: valueKey, Value: r.Value}
	if r.Value == "" {
		value.Value = notSet
	}
	return append(rows, value)
}

// subMenu is the screen of r: the title bar; the section HARDN_CONFIG and
// its rows; item 1, labelled with the state row's key, as Backup's item 1
// is, keeping HARDN_CONFIG, or once kept stopping, marked Lockout risk
// where its plan is (Reading.Risk); item 2, the guide; Return; no status
// row.
func (r envReading) subMenu() screen.SubMenu {
	about := keepIt
	if !r.keeps() {
		about = stopIt
	}
	return screen.SubMenu{
		Title:    component.TitleBar{Title: envTitle},
		Sections: []screen.Section{{Header: component.SectionHeader{Title: envSection}, Rows: component.BulletList{Items: r.rows()}}},
		Heading:  envHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: keyKeep, Label: r.underKey(), Description: about, Risk: r.refusal() == nil && r.Risk},
			{Key: keyGuide, Label: guideLabel, Description: guideAbout},
		}, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt: envPrompt,
	}
}

// originScreen is origin/main's Environment screen, its words as they are
// (environment_settings_menu.go:34-64), on the design system: its two
// status lines as bullet items, under the proposed section's header (a
// section has one; origin/main's has none), its two items and Return. Its
// sudo line reads origin/main's file alone
// (file_environment_repository.go:104-125), here as the control reads its
// line (sudoers.ReadLegacy), which differs from origin/main's substring
// check for a line commented out alone (§7 item 4); the mock is drawn on
// debian13, which has no such file, so the two agree.
func (r envReading) originScreen() screen.SubMenu {
	cfg := component.KeyValue{Key: "HARDN_CONFIG environment variable is not set"}
	if r.Value != "" {
		cfg = component.KeyValue{Key: "Current HARDN_CONFIG", Value: r.Value, State: component.ValueOK}
	}
	pres := component.KeyValue{Key: "Sudo preservation", Value: "Disabled", State: component.ValueBad}
	if r.Legacy.Keeps {
		pres.Value, pres.State = "Enabled", component.ValueOK
	}
	return screen.SubMenu{
		Title:    component.TitleBar{Title: envTitle},
		Sections: []screen.Section{{Header: component.SectionHeader{Title: envSection}, Rows: component.BulletList{Items: []component.KeyValue{cfg, pres}}}},
		Heading:  envHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: "1", Label: "Setup sudo environment preservation", Description: "Configure sudo to preserve HARDN_CONFIG"},
			{Key: "2", Label: "Show environment variables guide", Description: "Learn how to set up environment variables"},
		}, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt: envPrompt,
	}
}

// envRunner is one run of the Environment screen on a simulated host.
type envRunner struct {
	s     *session
	eh    envHost
	h     *fakeHost
	title string
}

// openEnv returns the action that opens what name names: the hosts' menu
// for envPrefix alone, the screen on one host for "environment/<host>".
func (s *session) openEnv(name string) func() error {
	if eh, ok := findEnvHost(strings.TrimPrefix(name, envPrefix+"/")); ok && name != envPrefix {
		return func() error { return s.envScreen(context.Background(), eh, eh.host()) }
	}
	return func() error {
		all := envHosts()
		vm := screen.SubMenu{Title: component.TitleBar{Title: "Environment hosts"}, Heading: "Select a host", Prompt: "Enter your choice",
			Menu: component.Menu{Exit: component.MenuItem{Key: "0", Label: "Return"}}}
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, eh := range all {
			it := component.MenuItem{Key: fmt.Sprint(i + 1), Label: eh.label, Description: eh.about}
			vm.Menu.Items = append(vm.Menu.Items, it)
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.envScreen(context.Background(), eh, eh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// envScreen runs the Environment screen on h, eh's host, until the user
// goes back from it: its note above it, then each round the screen read
// again from the host.
func (s *session) envScreen(ctx context.Context, eh envHost, h *fakeHost) error {
	e := &envRunner{s: s, eh: eh, h: h, title: envPrefix + "/" + eh.name}
	if err := s.writeNote(e.title, eh.note); err != nil {
		return err
	}
	return s.p.Run(func() nav.Screen { return e.draw(ctx) })
}

// draw reads the host and returns the screen, its items bound: item 1
// says why it cannot run before asking anything, as the DNS and Backup
// screens do, or runs its change; item 2 says the guide.
func (e *envRunner) draw(ctx context.Context) nav.Screen {
	r := readEnv(ctx, e.h.system(), e.eh.env)
	vm := r.subMenu()
	ks := e.kit()
	item1 := func() error { return e.change(ctx, r) }
	if msgs := r.refusal(); msgs != nil {
		item1 = ks.Say(msgs)
	}
	actions := map[string]func() error{keyKeep: item1, keyGuide: ks.Say(guide())}
	menu := nav.Menu{Exit: vm.Menu.Exit}
	for _, it := range vm.Menu.Items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: actions[it.Key]})
	}
	return vm.Screen(e.s.p.Ctx, menu)
}

// guide is item 2's words.
func guide() component.Messages {
	var msgs component.Messages
	for _, w := range guideWords {
		msgs = append(msgs, component.Message{Text: w})
	}
	return msgs
}

// kit is the screen's screenkit.Session: the change's engine the gallery's,
// over the host as the engine reaches it, its backups the defaults, the
// gallery's hold, its apply recorded for the end of play; a dry run when
// play plays the dry-run sequence. The Environment screen saves no
// hardn.yml: its configuration is read from nowhere, so nothing is adopted.
func (e *envRunner) kit() screenkit.Session {
	s, h := e.s, e.h
	var rec *recorder
	return screenkit.Session{
		Name: "environment screen", Prompter: s.p, System: h.system(), Config: &config.Config{},
		DryRun: s.sequence == "dry-run", LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			r := &recorder{host: h, f: h.f, title: e.title}
			rec = r
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, r)
			s.mu.Unlock()
			r.begin()
			return engine.New(h.system(), engine.Options{
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
	}
}

// review is item 1's review: the section HARDN_CONFIG, the user and the
// state the change makes, in the control's words (stateRow), the plan's
// findings listed under them by the flow.
func (r envReading) review() flow.Review {
	return flow.Review{Title: envSection, Items: []component.KeyValue{{Key: userKey, Value: r.User}, r.stateRow(r.keeps())}}
}

// change is item 1 through the flow, under its title bar: the review
// (review); the plan, the engine's, of the Environment control
// (environment.Keep, with the host's environment), its findings listed;
// the question, y/N, or the hostname typed for a lockout-risk plan; the
// apply. Then, once applied, that it takes effect at the next sudo: this
// hardn was started without it.
func (e *envRunner) change(ctx context.Context, r envReading) error {
	keep, tool := r.keeps(), r.ToolName()
	title, question := keepTitle, fmt.Sprintf(askKeep, tool)
	if !keep {
		title, question = stopTitle, fmt.Sprintf(askStop, tool)
	}
	review := r.review()
	ks := e.kit()
	ran := ks.Ran
	ks.Ran = func(run output.Run) {
		ran(run)
		if run.Mode == output.ModeApply && run.Err == nil && run.Result != nil && !run.Result.Nothing && run.Result.Failed == nil {
			_ = ks.Say(component.Messages{{Text: fmt.Sprintf(takesEffect, tool)}})()
		}
	}
	return ks.Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: title}},
		Flow: flow.Flow{
			Review: func() flow.Review { return review },
			Confirm: func(p flow.Plan) flow.Confirm {
				if p.Typed() {
					return flow.Confirm{Question: question, Label: typeTheHostname, Phrase: hostName}
				}
				return flow.Confirm{Question: question}
			},
		},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{environment.Keep{Getenv: lookup(e.eh.env), Stop: !keep}}
		},
	})
}

// envMock is one mock the listing shows: its name, after envPrefix, its
// note, and its drawing.
type envMock struct {
	name, note string
	draw       func(ctx context.Context, c component.Ctx) (string, error)
}

// readHostNamed reads the host named name, fresh, as the screen does.
func readHostNamed(ctx context.Context, name string) (envReading, error) {
	eh, ok := findEnvHost(name)
	if !ok {
		return envReading{}, fmt.Errorf("no environment host %q", name)
	}
	return readEnv(ctx, eh.host().system(), eh.env), nil
}

// onEnvHost returns a drawing of the host named name's reading.
func onEnvHost(name string, view func(envReading, component.Ctx) string) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		r, err := readHostNamed(ctx, name)
		if err != nil {
			return "", err
		}
		return view(r, c), nil
	}
}

// envMocks are the listing's mocks, in order: the screen on each host,
// origin/main's screen on the design system, item 1 refused on
// alpine324, the guide, and the refusals.
func envMocks() []envMock {
	var mocks []envMock
	for _, eh := range envHosts() {
		mocks = append(mocks, envMock{name: "screen-" + eh.name, note: "Mock: the Environment screen. " + eh.note,
			draw: onEnvHost(eh.name, func(r envReading, c component.Ctx) string { return r.subMenu().Render(c) })})
	}
	return append(mocks,
		envMock{name: "origin", note: "origin/main's Environment screen, its words as they are (environment_settings_menu.go:34-64), on the design system, on the debian13 host: its two lines, under the proposed section's header (origin/main has none), and its two items, whose labels push the menu's descriptions under them.",
			draw: onEnvHost("debian13", func(r envReading, c component.Ctx) string { return r.originScreen().Render(c) })},
		envMock{name: "item1-doas-refuse", note: "Decision 1 (a), adopted: item 1 on alpine324 refuses before asking, as hardn has no rule for alice and writes none (sudoers.ErrEnvNeedsGrant); the refusal says the row stays Not kept by hardn.",
			draw: onEnvHost("alpine324", itemOne)},
		envMock{name: "guide", note: "Mock: Guide (2), origin/main's guide shortened, under the screen's prompt.",
			draw: func(_ context.Context, c component.Ctx) (string, error) { return guide().Render(c), nil }},
		envMock{name: "refusals", note: "Mock: item 1's refusals before asking, on the root-login, pve9 and alpine324 hosts, and on a host whose hardn file was edited by hand.",
			draw: refusalsMock},
	)
}

// itemOne draws what item 1 does first on r's host: its refusal, or the
// review the flow shows above its plan (review).
func itemOne(r envReading, c component.Ctx) string {
	if msgs := r.refusal(); msgs != nil {
		return apart(r.subMenu().Render(c), msgs.Render(c))
	}
	rv := r.review()
	return apart(r.subMenu().Render(c), component.SectionHeader{Title: rv.Title}.Render(c), component.BulletList{Items: rv.Items}.Render(c))
}

// editedHost is Debian 13 whose hardn-env-alice was edited by hand to
// grant alice sudo as well: a file sudoers.PlanFor writes for no Policy.
func editedHost() *fakeHost {
	h := keptHost()
	h.f.AddFile(sudoers.DropInDir+"/hardn-env-alice", []byte(sudoers.Header+"Defaults:alice env_keep += \"HARDN_CONFIG\"\nalice ALL=(ALL) NOPASSWD: ALL\n"), 0o440, 0, 0)
	return h
}

// refusalsMock draws each refusal under a header naming its host.
func refusalsMock(ctx context.Context, c component.Ctx) (string, error) {
	var out []string
	for _, name := range []string{"root-login", "pve9", "alpine324"} {
		r, err := readHostNamed(ctx, name)
		if err != nil {
			return "", err
		}
		out = append(out, header(c, "item 1 on "+name), r.refusal().Render(c))
	}
	r := readEnv(ctx, editedHost().system(), sudoAlice)
	return apart(append(out, header(c, "item 1, hardn-env-alice edited"), r.refusal().Render(c))...), nil
}

// envUsage is the listing's usage line.
const envUsage = "usage: go run ./internal/tools/uigallery environment [flags] [mock | all]..."

// envList is the listing: each mock args name (none, or "all", is every
// mock) under the gallery's header, "environment <mock>", its note, then
// its drawing, a write each; -list prints their names. It sizes itself as
// play does (look.sizeTo).
func envList(args []string, size sizer, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uigallery environment", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, envUsage)
		fs.PrintDefaults()
	}
	list := fs.Bool("list", false, "print the names of the mocks it would list, and exit")
	look := lookFlags(fs, true)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	mocks, err := selectEnvMocks(fs.Args())
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", envPrefix, err)
		return exitUsage
	}
	if *list {
		for _, m := range mocks {
			fmt.Fprintln(stdout, m.name)
		}
		return exitOK
	}
	look.sizeTo(fs, size)
	c, err := look.cell()
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", envPrefix, err)
		return exitUsage
	}
	g, ok := look.gallery(stderr)
	if !ok {
		return exitError
	}
	rc, err := g.ctx(c.env)
	if err == nil {
		err = listEnv(context.Background(), rc, c.env.Writer(stdout), mocks)
	}
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", envPrefix, err)
		return exitError
	}
	return exitOK
}

// listEnv writes each of mocks to w, rendered in rc, a write each: the
// gallery's header, the mock's note, then its drawing.
func listEnv(ctx context.Context, rc component.Ctx, w io.Writer, mocks []envMock) error {
	for _, m := range mocks {
		out, err := m.draw(ctx, rc)
		if err == nil {
			_, err = io.WriteString(w, apart(header(rc, envPrefix+" "+m.name),
				component.Callout{Kind: component.MessageInfo, Body: []string{m.note}}.Render(rc), out)+sectionBreak)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
	}
	return nil
}

// selectEnvMocks returns the mocks args name, in order: every mock for
// none or "all". An argument that starts with "-" is a flag the flag
// package left unparsed, after a name.
func selectEnvMocks(args []string) ([]envMock, error) {
	all := envMocks()
	if len(args) == 0 || slices.Contains(args, "all") {
		return all, nil
	}
	for _, a := range args {
		switch found := slices.ContainsFunc(all, func(m envMock) bool { return m.name == a }); {
		case !found && strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("flag %s comes after a mock's name; flags go before them", a)
		case !found:
			return nil, fmt.Errorf("no mock %q; -list shows them", a)
		}
	}
	return slices.DeleteFunc(all, func(m envMock) bool { return !slices.Contains(args, m.name) }), nil
}

// envStart returns the hosts' menu, or the host, that args name
// ("environment", "environment/<host>"), and whether they name one; an
// error for a host that is not one.
func envStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != envPrefix && !strings.HasPrefix(args[0], envPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], envPrefix+"/"); args[0] != envPrefix {
		if _, ok := findEnvHost(name); !ok {
			var names []string
			for _, h := range envHosts() {
				names = append(names, envPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no environment host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}

// envNote is play's note for the Environment mocks.
const envNote = "Play mode, mocks: the Environment screen proposed for slice 9, drawn from the Environment control's reading of a simulated host " +
	"(environment.Read), each change the control's on the real plan/apply engine (steps.Sudoers). The screen is the gallery's, unit (c)'s to build. " +
	"Nothing on this machine changes: every command and file is the fake's, and its record is shown when play ends."
