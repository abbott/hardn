package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// playUsage is the playable mode's usage line.
const playUsage = "usage: go run ./internal/tools/uigallery play [flags] [screen | workflow/sequence | engine | engine/scenario | status | status/host | dns | dns/host | backup | backup/host | backup-restore | environment | environment/host | system-details | system-details/host]"

// links say where each menu item leads in the playable mode, by the menu
// mock's name and the item's key: the mock it opens, as origin/main's
// menus lead (docs/refactor/phase0/interaction.md §9): the main menu to
// its sub-menus, User Management's Manage User to the user list and on to
// george's menu, its Sudo Method to the sudo change, and SSH Login's first
// item to the root-login toggle. The main menu's 3 leads to the real DNS
// screen (dnsLinks, dnsscreen.go), its 5 to the real Backup screen
// (backupLinks, backupscreen.go), where it led to Figma's 05-BACKUP frame,
// kept among the mocks as Figma's record (figmaRecords), and its 8 to the
// real Environment screen (envLinks, environment.go), and its 9 to the
// System Details mock (sdLinks, systemdetails.go). An item with no
// link, and no refusal (screen.Mock.Refusals), says that the gallery has
// no screen for it.
var links = map[string]string{
	"main-menu/00-tui-main/1":                 "sub-menu/01-user",
	"main-menu/00-tui-main/2":                 "sub-menu/02-ssh-login",
	"sub-menu/01-user/1":                      "sub-menu/user-manage-user-select-user",
	"sub-menu/01-user/2":                      "workflow-step/user-create-user",
	"sub-menu/02-ssh-login/1":                 "workflow-step/ssh-login-toggle-confirmation",
	"sub-menu/user-manage-user-select-user/1": "sub-menu/user-manage-user-management",
	"sub-menu/user-manage-user-management/1":  "workflow-step/user-manage-sudo-confirmation",
	"sub-menu/user-manage-user-management/2":  "sub-menu/user-ssh-keys",
}

// play is the playable mode (brief 1D): it parses args, plays the screens
// from the one they name, reading the user's answers through stdin's line
// reader, writing to stdout and reporting errors on stderr, and returns
// the exit status: exitOK when the user goes back from the first screen
// or quits, prompt.ExitInterrupted (130) when the input ends or Ctrl-C
// interrupts a prompt. Where -width or -height is not given, it takes the
// size of the terminal stdout is from size (look.sizeTo).
func play(args []string, stdin func() *prompt.Reader, size sizer, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uigallery play", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, playUsage)
		fs.PrintDefaults()
	}
	look := lookFlags(fs, true)
	sequence := fs.String("sequence", "normal", "the `name` of the sequence each workflow plays, where it has one, such as dry-run, failure or typed; else its first")
	failAt := fs.Int("fail", 0, "fail step `N` of each plan applied, to show the rollback; 0 fails none")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	usage := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "uigallery: play: "+format+"\n", a...)
		return exitUsage
	}
	mocks := screen.Mocks()
	if name, ok, err := engineStart(fs.Args()); ok || err != nil {
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
		switch {
		case err != nil:
			return usage("%v", err)
		case set["sequence"] || set["fail"]:
			return usage("-sequence and -fail are for the fixture plans of the screen mocks; each engine scenario plays one path, failing where it says (engine/failed-reload-debian13)")
		}
		return startPlay(look, fs, size, stdin, stdout, stderr, func(s *session) int {
			return s.play("play "+name, []string{engineNote}, s.openEngine(name), stderr)
		})
	}
	if name, ok, err := dnsStart(fs.Args()); ok || err != nil {
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
		switch {
		case err != nil:
			return usage("%v", err)
		case set["fail"]:
			return usage("-fail is for the fixture plans of the screen mocks; the DNS screen fails where its host does (dns/failed-reload, dns/failed-save)")
		case *sequence != "normal" && *sequence != "dry-run":
			return usage("-sequence %s: the DNS screen plays normal or dry-run", *sequence)
		}
		return startPlay(look, fs, size, stdin, stdout, stderr, func(s *session) int {
			s.sequence = *sequence
			return s.play("play "+name, []string{engineNote}, s.openDNS(name), stderr)
		})
	}
	if name, ok, err := backupStart(fs.Args()); ok || err != nil {
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
		switch {
		case err != nil:
			return usage("%v", err)
		case set["fail"]:
			return usage("-fail is for the fixture plans of the screen mocks; the Backup screen fails where its host does (backup/readonly)")
		case *sequence != "normal" && *sequence != "dry-run":
			return usage("-sequence %s: the Backup screen plays normal or dry-run", *sequence)
		}
		note := engineNote
		if name == restoreMock {
			note = restoreNote
		}
		return startPlay(look, fs, size, stdin, stdout, stderr, func(s *session) int {
			s.sequence = *sequence
			return s.play("play "+name, []string{note}, s.openBackup(name), stderr)
		})
	}
	if name, ok, err := envStart(fs.Args()); ok || err != nil {
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
		switch {
		case err != nil:
			return usage("%v", err)
		case set["fail"]:
			return usage("-fail is for the fixture plans of the screen mocks; the Environment screen fails where its host does")
		case *sequence != "normal" && *sequence != "dry-run":
			return usage("-sequence %s: the Environment screen plays normal or dry-run", *sequence)
		}
		return startPlay(look, fs, size, stdin, stdout, stderr, func(s *session) int {
			s.sequence = *sequence
			return s.play("play "+name, []string{engineNote}, s.openEnv(name), stderr)
		})
	}
	if name, ok, err := sdStart(fs.Args()); ok || err != nil {
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
		switch {
		case err != nil:
			return usage("%v", err)
		case set["fail"]:
			return usage("-fail is for the fixture plans of the screen mocks; the System Details mock applies nothing")
		case *sequence != "normal" && *sequence != "dry-run":
			return usage("-sequence %s: the System Details mock plays normal or dry-run", *sequence)
		}
		return startPlay(look, fs, size, stdin, stdout, stderr, func(s *session) int {
			s.sequence = *sequence
			return s.play("play "+name, []string{sdNote}, s.openSD(name), stderr)
		})
	}
	status, isStatus, err := statusStart(fs.Args())
	if err != nil {
		return usage("%v", err)
	}
	var (
		start screen.Mock
		named string
	)
	if !isStatus {
		if start, named, err = startAt(mocks, fs.Args()); err != nil {
			return usage("%v", err)
		}
	}
	if named != "" {
		set := false
		fs.Visit(func(f *flag.Flag) { set = set || f.Name == "sequence" })
		if set && *sequence != named {
			return usage("%s names the %s sequence, and -sequence %s", fs.Arg(0), named, *sequence)
		}
		*sequence = named
	}
	has := func(ok func(screen.Sequence) bool) bool {
		return slices.ContainsFunc(mocks, func(m screen.Mock) bool { return slices.ContainsFunc(m.Sequences, ok) })
	}
	switch {
	case !has(func(q screen.Sequence) bool { return q.Name == *sequence }):
		return usage("-sequence %q: no workflow has that sequence; -screens -list shows them", *sequence)
	case !has(func(q screen.Sequence) bool { return q.Name == *sequence && !declines(q) }):
		return usage("-sequence %s: that sequence ends as the user declines, which play leaves to the user; decline the confirmation to play it", *sequence)
	case *failAt < 0:
		return usage("-fail %d is not a step", *failAt)
	}
	return startPlay(look, fs, size, stdin, stdout, stderr, func(s *session) int {
		s.sequence, s.sim.failAt = *sequence, *failAt
		for _, m := range mocks {
			s.mocks[m.Name] = m
		}
		if isStatus {
			// The main menu's mock: the note says only what -sequence
			// and -fail set, after the status hosts' own.
			note := append([]string{statusNote}, s.mockNote(s.mocks[mainMenuMock])[1:]...)
			return s.play("play "+status, note, s.openStatus(status), stderr)
		}
		return s.play("play "+start.Name, s.mockNote(start), s.open(start), stderr)
	})
}

// startPlay builds the session play runs in, in the environment the look
// flags describe, sized to the terminal where they do not say (look.sizeTo),
// and returns what run returns of it; a usage error or a bad override
// file is reported before stdin is read.
func startPlay(l look, fs *flag.FlagSet, size sizer, stdin func() *prompt.Reader, stdout, stderr io.Writer, run func(*session) int) int {
	l.sizeTo(fs, size)
	c, err := l.cell()
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: play: %v\n", err)
		return exitUsage
	}
	g, ok := l.gallery(stderr)
	if !ok {
		return exitError
	}
	ctx, err := g.ctx(c.env)
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: play: %v\n", err)
		return exitError
	}
	return run(&session{
		p:     nav.Prompter{Ctx: ctx, In: stdin(), Out: c.env.Writer(stdout)},
		mocks: map[string]screen.Mock{},
		sim:   &simulator{},
	})
}

// engineStart returns the engine scenario, or the menu of them, that args
// name ("engine", "engine/<scenario>"), and whether they name one; an
// error for a scenario that is not one.
func engineStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != enginePrefix && !strings.HasPrefix(args[0], enginePrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], enginePrefix+"/"); args[0] != enginePrefix {
		if _, ok := findScenario(name); !ok {
			var names []string
			for _, sc := range scenarios() {
				names = append(names, enginePrefix+"/"+sc.name)
			}
			return "", false, fmt.Errorf("no engine scenario %q; the scenarios are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}

// startAt returns the mock where play starts, which args name, and the
// sequence they name, if any: no argument is the main menu; one is a
// mock's name, a layout that has no other mock, taken whole as -screens
// takes it (only "main-menu" is one), or a workflow's sequence,
// "<workflow>/<sequence>", as -screens -list names them, unless the
// sequence declines.
func startAt(mocks []screen.Mock, args []string) (screen.Mock, string, error) {
	if len(args) == 0 {
		args = []string{"main-menu"}
	}
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "-") {
			return screen.Mock{}, "", fmt.Errorf("flag %s comes after the screen's name; flags go before it", a)
		}
	}
	if len(args) > 1 {
		return screen.Mock{}, "", fmt.Errorf("play starts at one screen; %d are named", len(args))
	}
	a := args[0]
	var found []screen.Mock
	for _, m := range mocks {
		for _, s := range m.Sequences {
			if a != m.Name+"/"+s.Name {
				continue
			}
			if declines(s) {
				return screen.Mock{}, "", fmt.Errorf("%s ends as the user declines, which play leaves to the user; start at %s and decline its confirmation", a, m.Name)
			}
			return m, s.Name, nil
		}
		if m.Name == a {
			return m, "", nil
		}
		if strings.HasPrefix(m.Name, a+"/") {
			found = append(found, m)
		}
	}
	switch len(found) {
	case 0:
		return screen.Mock{}, "", fmt.Errorf("no screen or workflow sequence %q; -screens -list shows them", a)
	case 1:
		return found[0], "", nil
	}
	var names []string
	for _, m := range found {
		names = append(names, m.Name)
	}
	return screen.Mock{}, "", fmt.Errorf("%q names %d screens; name one of %s", a, len(found), strings.Join(names, ", "))
}

// declines reports whether sequence s ends as the user declines
// (flow.Cancelled). Play plays a sequence for what Build makes, its screen
// and its fixture plan, and the user types the answers; the flow cancels
// only when the user declines its confirmation, or when the terminal is
// too narrow to ask a typed one. A sequence that ends cancelled is
// therefore made by its answers alone, not by a fixture of its own
// (create-user's cancelled builds as its normal does), so play does not
// offer it: declining the confirmation plays it.
func declines(s screen.Sequence) bool { return s.Ends == flow.Cancelled }

// sequenceOf returns the sequence of workflow mock m named name, unless it
// declines, else its first; the zero Sequence for a menu mock.
func sequenceOf(m screen.Mock, name string) screen.Sequence {
	if len(m.Sequences) == 0 {
		return screen.Sequence{}
	}
	if i := slices.IndexFunc(m.Sequences, func(s screen.Sequence) bool { return s.Name == name && !declines(s) }); i >= 0 {
		return m.Sequences[i]
	}
	return m.Sequences[0]
}

// menuOf returns the menu a menu mock's screen shows, and false for a
// screen of no layout the playable mode knows.
func menuOf(s screen.MenuScreen) (component.Menu, bool) {
	switch v := s.(type) {
	case screen.MainMenu:
		return v.Menu, true
	case screen.SubMenu:
		return v.Menu, true
	}
	return component.Menu{}, false
}

// session is one run of the playable mode: the prompter the user answers
// through, the mocks they can reach, the sequence each workflow plays and
// the simulated system the workflows apply to; for the engine's scenarios,
// the record of each apply on its simulated host and how often the engine
// took its signal hold.
type session struct {
	p        nav.Prompter
	mocks    map[string]screen.Mock
	sequence string
	sim      *simulator

	mu         sync.Mutex
	engineRuns []*recorder
	holds      int
}

// engineNote is play's note for the engine's scenarios.
const engineNote = "Play mode: hardn's own screens and prompts, on the real plan/apply engine " +
	"against a simulated host, hostfake's recording host.System, whose commands a model of the host answers. " +
	"Nothing on this machine changes: every command and file is the fake's, and its record is shown when play ends."

// mockNote is play's note for a start at mock start: the fixture plans,
// the sequence each workflow plays and the step -fail fails.
func (s *session) mockNote(start screen.Mock) []string {
	note := []string{"Play mode: hardn's own screens and prompts, on the gallery's fixture plans. " +
		"Nothing on this machine changes: the simulated system only records each step it would apply, " +
		"and the record is shown when play ends."}
	switch {
	case start.Screen == nil:
		note = append(note, fmt.Sprintf("The workflow plays its %s sequence.", sequenceOf(start, s.sequence).Name))
	case s.sequence != "normal":
		note = append(note, fmt.Sprintf("Each workflow plays its %s sequence, where it has one, else its first.", s.sequence))
	}
	if n := s.sim.failAt; n > 0 {
		note = append(note, fmt.Sprintf("Step %d of each plan applied fails, where the plan has a step %d (-fail %d), and the steps before it are rolled back.", n, n, n))
	}
	return note
}

// play plays from the screen open opens: the gallery's header, titled
// title, and note on the simulated system, the screens as the user
// answers, then what the simulated system recorded. It returns the exit
// status, writing any error but an abort's to stderr, a failed write of
// the record included.
func (s *session) play(title string, note []string, open func() error, stderr io.Writer) int {
	c, gap := s.p.Ctx, strings.Repeat("\n", theme.SectionGap+1)
	opening := header(c, title) + gap + component.Callout{Kind: component.MessageInfo, Body: note}.Render(c) + gap
	if _, err := io.WriteString(s.p.Out, opening); err != nil {
		fmt.Fprintf(stderr, "uigallery: play: %v\n", err)
		return exitError
	}
	err := open()
	// left is whether the user left play, by going back from the first
	// screen or quitting, rather than by an abort or an error.
	left := err == nil || errors.Is(err, nav.ErrQuit) || errors.Is(err, nav.ErrBack)
	var record []string
	for _, r := range s.sim.applied() {
		record = append(record, component.Callout{Kind: component.MessageInfo, Title: r.title, Body: []string{
			"Recorded, not done: the file each step would write or restore, and the commands it would run.",
		}}.Render(c), component.PlanView{Changes: r.steps}.Render(c))
	}
	for _, r := range s.engineRuns {
		if steps := r.steps(); steps != nil {
			record = append(record, component.Callout{Kind: component.MessageInfo, Title: r.title, Body: []string{
				"Recorded by the fake, not done: what each step ran on " + hostFQDN + ", and the files it changed, " +
					"in order; hardn's own journal, run directory and backups left out.",
			}}.Render(c), component.PlanView{Changes: steps}.Render(c))
		}
	}
	if len(record) == 0 {
		record = []string{component.Callout{Kind: component.MessageInfo, Body: []string{"Nothing was applied."}}.Render(c)}
	}
	closing := "\n" + header(c, "simulated system") + gap + strings.Join(record, gap) + "\n"
	// A failed write of the record is the error play reports, however the
	// user left it; an abort keeps its exit status, and an earlier error
	// is the one reported.
	if _, werr := io.WriteString(s.p.Out, closing); werr != nil && left {
		err, left = werr, false
	}
	if left {
		return exitOK
	}
	if code, aborted := prompt.ExitCode(err); aborted {
		return code
	}
	fmt.Fprintf(stderr, "uigallery: play: %v\n", err)
	return exitError
}

// open returns the action that opens mock m: a menu screen run until the
// user goes back from it (nav.Prompter.Run), its items leading where links
// say; or a workflow, its sequence (sequenceOf) run once on the simulated
// system.
func (s *session) open(m screen.Mock) func() error {
	if m.Screen == nil {
		return func() error {
			seq := sequenceOf(m, s.sequence)
			step, f := seq.Build()
			f.Engine = s.sim.engine(strings.TrimPrefix(m.Name, "workflow-step/")+", "+seq.Name, f.Engine)
			_, err := step.Run(context.Background(), s.p, f)
			return err
		}
	}
	return func() error {
		return s.p.Run(func() nav.Screen { return m.Screen.Screen(s.p.Ctx, s.menu(m)) })
	}
}

// menu returns the menu of menu mock m bound to its actions: each item
// opens the mock links name for it, or the DNS, Backup, Environment or
// System Details screen on the host dnsLinks, backupLinks, envLinks or
// sdLinks name, an item m
// refuses says why, and an item with none of these says that the gallery
// has no screen for it.
func (s *session) menu(m screen.Mock) nav.Menu {
	view, _ := menuOf(m.Screen)
	out := nav.Menu{Exit: view.Exit}
	for _, it := range view.Items {
		action := s.missing(it.Label)
		if msg, ok := m.Refusals[it.Key]; ok {
			action = s.say(msg)
		}
		if to, ok := links[m.Name+"/"+it.Key]; ok {
			action = s.open(s.mocks[to])
		}
		if to, ok := dnsLinks[m.Name+"/"+it.Key]; ok {
			action = s.openDNS(to)
		}
		if to, ok := backupLinks[m.Name+"/"+it.Key]; ok {
			action = s.openBackup(to)
		}
		if to, ok := envLinks[m.Name+"/"+it.Key]; ok {
			action = s.openEnv(to)
		}
		if to, ok := sdLinks[m.Name+"/"+it.Key]; ok {
			action = s.openSD(to)
		}
		out.Options = append(out.Options, nav.Option{Item: it, Action: action})
	}
	return out
}

// missing returns the action of an item the gallery has no screen for: it
// says so in an info Message.
func (s *session) missing(label string) func() error {
	return s.say(component.Message{Text: fmt.Sprintf("The gallery has no screen for %s yet.", strings.TrimSpace(label))})
}

// say returns an action that writes msg, after theme.SectionGap blank
// lines, as an action that writes a result does (nav.Prompter.Run).
func (s *session) say(msg component.Message) func() error {
	return func() error {
		_, err := io.WriteString(s.p.Out, strings.Repeat("\n", theme.SectionGap)+msg.Render(s.p.Ctx)+"\n")
		return err
	}
}

// simulator is the playable mode's simulated system for the screen mocks'
// workflows: a workflow plans on its sequence's fixture (flow.Fixture), and
// applying a plan only reports each step to the flow and records what the
// step would do: nothing is run, written or read, and no root is needed.
// With failAt set, step failAt of each plan applied fails, and the fixture
// rolls back the steps before it.
//
// The engine's scenarios (engine.go, next/p3-gallery-engine) do not go
// through it: they run the real engine on a simulated host of their own
// (fakehost.go), and are not the mocks. A mock moves onto the engine with
// its slice, whose control holds the action's answers (flow.Engine: "an
// adapter holding the action's answers"); for a form, such as
// create-user's, whose answers pkg/ui/screen binds inside Build, that
// slice changes how a sequence is built, an exported API of
// pkg/ui/screen, such as Build taking a constructor of the engine over the
// answers in place of the fixture it closes over.
type simulator struct {
	failAt int
	mu     sync.Mutex
	runs   []simRun
}

// simRun is what the simulator recorded of one plan applied: the
// workflow and sequence it was applied by, and each step reported, as a
// PlanChange whose title says how it ended (recorded).
type simRun struct {
	title string
	steps []component.PlanChange
}

// engine returns the Engine one run of a workflow plays on: e's plans,
// applied by the simulator, which records each under title.
func (s *simulator) engine(title string, e flow.Engine) flow.Engine {
	return &simulated{sim: s, title: title, inner: e}
}

// applied returns what the simulator recorded, a run per plan applied, in
// order.
func (s *simulator) applied() []simRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.runs)
}

// begin starts the record of a plan applied under title.
func (s *simulator) begin(title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append(s.runs, simRun{title: title})
}

// add records step under the plan last begun.
func (s *simulator) add(step component.PlanChange) {
	s.mu.Lock()
	defer s.mu.Unlock()
	last := &s.runs[len(s.runs)-1]
	last.steps = append(last.steps, step)
}

// simulated is a workflow's Engine on the simulator: it plans as the
// fixture does and applies by recording.
type simulated struct {
	sim   *simulator
	title string
	inner flow.Engine
	plan  flow.Plan
}

// Plan returns the fixture's plan.
func (e *simulated) Plan(ctx context.Context) (flow.Plan, error) {
	p, err := e.inner.Plan(ctx)
	e.plan = p
	return p, err
}

// Apply applies the plan Plan returned on the simulator: the fixture
// reports each step, failing where its sequence fails, or at the
// simulator's failAt when the plan has that step, and each report is
// recorded before the flow gets it.
func (e *simulated) Apply(ctx context.Context, report flow.Report) error {
	changes, applier := e.plan.View.Changes, e.inner
	if n := e.sim.failAt; n > 0 && n <= len(changes) {
		ch := changes[n-1]
		failure := flow.Failure{
			Err:  fmt.Errorf("Step %d failed: exit status 1", n),
			Next: "The gallery failed this step, as -fail asked; nothing on this machine changed.",
		}
		if len(ch.Commands) > 0 {
			failure.Command = ch.Commands[len(ch.Commands)-1]
		}
		applier = flow.Fixture{Planned: e.plan, FailAt: n, Detail: "exit status 1", Failure: failure}
	}
	e.sim.begin(e.title)
	return applier.Apply(ctx, func(step int, state component.StepState, detail string) {
		if step >= 0 && step < len(changes) {
			if r, ok := recorded(changes[step], state, detail); ok {
				e.sim.add(r)
			}
		}
		report(step, state, detail)
	})
}

// recorded returns what the simulator records of step ch, reported in
// state, and whether it records anything: for a step done, the file it
// would write and the commands it would run; for one failed, why; for one
// rolled back, the file it would restore. A step's file is its target
// when it shows a diff, or when the target is a path, as a save of
// hardn.yml, whose diff a plan does not show, names it; a service, such
// as "ufw", is not one. Its title says how the step ended. Other states,
// such as running, record nothing.
func recorded(ch component.PlanChange, state component.StepState, detail string) (component.PlanChange, bool) {
	file := ""
	if ch.Diff != "" || path.IsAbs(ch.Target) {
		file = ch.Target
	}
	switch state {
	case component.StepDone:
		return component.PlanChange{Title: "Applied: " + ch.Title, Target: file, Commands: ch.Commands}, true
	case component.StepFailed:
		title := "Failed: " + ch.Title
		if detail != "" {
			title += " (" + detail + ")"
		}
		return component.PlanChange{Title: title}, true
	case component.StepRolledBack:
		return component.PlanChange{Title: "Rolled back: " + ch.Title, Target: file}, true
	}
	return component.PlanChange{}, false
}
