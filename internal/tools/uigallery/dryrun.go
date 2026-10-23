package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The Dry-Run mocks (refactor plan slice 2b, unit (a), next/s2b-gallery;
// docs/engine/gallery-dryrun.md): the main menu's items on its layout,
// with the live status block of a status host above them, in its full and
// compact forms; its mode indicator in origin/main's words and in the
// proposed ones; origin/main's Dry-Run item (6) on the sub-menu layout and
// its toggle; and origin/main's Firewall screen under dry run, as pkg/menu
// prints it. Each is a mock for the maintainer's review: nothing here is
// wired into hardn. "dryrun" lists them with no input; "play dryrun"
// plays the main menu with origin/main's items, its 6 toggling dry run
// for the session, "play dryrun/saved" the same with the toggle saved to
// hardn.yml through the canonical flow, and "play dryrun/proposed" the
// main menu the page recommends; -sequence dry-run starts them in dry run,
// as hardn -n does.

// dryRunPrefix names the mocks: "dryrun <mock>" in the listing, "dryrun"
// and "dryrun/<variant>" in play.
const dryRunPrefix = "dryrun"

// dryRunHost is the status host whose main menu the mocks draw: Debian 13
// as the engine's scenarios start from it, eight rows with no line under
// any, as docs/engine/menu-live.md §6 measures the heights.
const dryRunHost = "hardened"

// dryRunKey is the key of origin/main's Dry-Run item on the main menu.
const dryRunKey = "6"

// origin/main's words for the mode, as style.Menu writes them beside the
// menu heading (pkg/style style.go:787-825). They are pinned here, not
// taken from component.ModeIndicator, so that the mocks labelled
// origin/main's keep them once the component takes the decided words
// (unit (c)).
const (
	originLabel  = "Dry-run Mode"
	originLive   = "Disabled"
	originDryRun = "Enabled"
)

// The indicator's proposed words (decisions 2 and 3): the label, the word
// for each mode, and where plan only came from, after the word.
const (
	proposedLabel    = "Mode"
	proposedLive     = "Live"
	proposedPlanOnly = "Plan only"
	fromFlag         = "-n"
	fromConfig       = "hardn.yml"
)

// modeView is how a mock shows the mode beside the menu heading, in
// origin/main's words or the proposed ones, with where plan only came
// from, drawn here on the components' grid in the theme's mode slots.
type modeView struct {
	proposed bool
	planOnly bool
	// from is fromFlag or fromConfig, shown after a proposed plan only;
	// empty shows none.
	from string
}

// text is mv's whole text: the label, the word and the source.
func (mv modeView) text(c component.Ctx) (label piece, word piece, from []piece) {
	t := c.Theme
	label, word = piece{originLabel, t.Mode.Label.Render}, piece{originLive, t.Mode.Live.Render}
	if mv.planOnly {
		word = piece{originDryRun, t.Mode.DryRun.Render}
	}
	if mv.proposed {
		label.text, word.text = proposedLabel, proposedLive
		if mv.planOnly {
			word.text = proposedPlanOnly
		}
	}
	if mv.from != "" {
		from = []piece{pad(1), {"(" + mv.from + ")", t.Text.Faint.Render}}
	}
	return label, word, from
}

// fit is mv's text in at most room cells: whole where it fits; else the
// label gives way first, as the component's does, then the source
// (decision 3), and the word alone is cut only when it does not fit.
func (mv modeView) fit(c component.Ctx, room int) []piece {
	label, word, from := mv.text(c)
	for _, l := range [][]piece{append([]piece{label, pad(1), word}, from...), append([]piece{word}, from...), {word}} {
		if widthOf(l) <= room {
			return l
		}
	}
	return []piece{{cut(c, word.text, room), word.paint}}
}

// heading renders the menu heading titled title with the mode beside it,
// as component.MenuHeading places it: at theme.ValueCol on the heading's
// line, or under it at theme.StackCol below theme.StackBelow columns, or
// when the line cannot hold the whole text (fit).
func (mv modeView) heading(c component.Ctx, title string) string {
	head := component.MenuHeading{Title: title}.Render(c)
	w, whole := contentWidth(c.Env), mv.fit(c, math.MaxInt)
	if termWidth(c.Env) >= theme.StackBelow && ansi.StringWidth(head) < theme.ValueCol && theme.ValueCol+widthOf(whole) <= w {
		return head + draw(append([]piece{pad(theme.ValueCol - ansi.StringWidth(head))}, whole...)...)
	}
	return head + "\n" + draw(append([]piece{pad(theme.StackCol)}, mv.fit(c, w-theme.StackCol)...)...)
}

// drawMenu renders main menu vm as its layout draws it, its heading line
// in mv's words: the layout's own heading, with the component's
// indicator, is replaced, as it must appear once.
func (mv modeView) drawMenu(c component.Ctx, vm screen.MainMenu) (string, error) {
	return mv.swap(c, vm.Render(c), vm.Heading)
}

// swap replaces in out the menu heading titled title, with mv's mode as
// component.ModeIndicator draws it, by mv's heading.
func (mv modeView) swap(c component.Ctx, out, title string) (string, error) {
	mode := component.ModeIndicator{DryRun: mv.planOnly}
	old := component.MenuHeading{Title: title, Mode: &mode}.Render(c)
	if n := strings.Count(out, old); n != 1 {
		return "", fmt.Errorf("%s: the menu heading appears %d times, want once", dryRunPrefix, n)
	}
	return strings.Replace(out, old, mv.heading(c, title), 1), nil
}

// mainMenu is the main menu of st, as hardn's would be on the layout:
// st's status above the main-menu mock's heading, items and prompt, the
// mode plan only when planOnly is set; with proposed set, origin/main's
// item 6 is left out, its key with it (decision 1).
func mainMenu(st hostStatus, planOnly, proposed bool) screen.MainMenu {
	vm := st.mainMenu()
	vm.Mode = component.ModeIndicator{DryRun: planOnly}
	if proposed {
		vm.Menu.Items = slices.DeleteFunc(slices.Clone(vm.Menu.Items), func(it component.MenuItem) bool { return it.Key == dryRunKey })
	}
	return vm
}

// dryRunScreen is origin/main's Dry-Run item on the sub-menu layout, in
// its words: "Dry-Run Mode Settings", the mode and what it means, and the
// one item that turns it the other way, as its menu has it in each state
// (pkg/menu dry_run_menu.go), printed in both.
func dryRunScreen(planOnly bool) screen.SubMenu {
	mode := component.KeyValue{Key: "Dry-run Mode", Value: "Disabled", State: component.ValueWarn}
	about := "In this mode, changes will be applied to the system. Proceed with caution."
	item := component.MenuItem{Key: "1", Label: "Enable dry-run mode", Description: "Preview changes without applying them"}
	keep := "Keep dry-run mode disabled"
	if planOnly {
		mode.Value, mode.State = "Enabled", component.ValueOK
		about = "In this mode, the script will preview changes without applying them."
		item = component.MenuItem{Key: "1", Label: "Disable dry-run mode", Description: "Apply changes to the system for real"}
		keep = "Keep dry-run mode enabled"
	}
	return screen.SubMenu{
		Title: component.TitleBar{Title: "Dry-Run Mode Settings"},
		Sections: []screen.Section{{
			Header: component.SectionHeader{Title: "Dry-Run Mode Configuration"},
			Rows:   component.BulletList{Items: []component.KeyValue{mode, {Key: about, State: component.ValueNone}}},
		}},
		Heading: "Select an option",
		Menu:    component.Menu{Items: []component.MenuItem{item}, Exit: component.MenuItem{Key: "0", Label: "Return to main menu", Description: keep}},
		Prompt:  "Enter selection",
	}
}

// nowWords is what origin/main's Dry-Run item says once the mode is set
// (pkg/menu dry_run_handler.go:63-71), the session toggle's result.
func nowWords(planOnly bool) string {
	if planOnly {
		return "Dry-run mode is now ENABLED - Changes will only be simulated"
	}
	return "Dry-run mode is now DISABLED - Changes will be applied to the system"
}

// toggleYML is the simulated host's hardn.yml for the saved toggle: the
// DNS hosts' file with the dryRun key in it, as hardn.yml.example has it,
// set to dryRun.
func toggleYML(dryRun bool) string {
	list := nameserversYML(dnsList)
	user, rest, _ := strings.Cut(list, "\n")
	return fmt.Sprintf("%s\ndryRun: %t\n%s", user, dryRun, rest)
}

// toggleFlow is the saved toggle (alternative (a) of decision 4), setting
// hardn.yml's dryRun, which file holds, to want: the canonical flow, its
// review the mode it sets, its plan the real save of the key, planned by
// steps.PlanSave on a simulated host with toggleYML(file), edited in place
// (config.Edit), and a y/N question; with the key already want, nothing
// to change. It is never a dry run itself: it is the change that ends one.
func toggleFlow(ctx context.Context, file, want bool) (screen.WorkflowStep, flow.Flow, error) {
	h := simulate(debian13Platform).withHardnYML(toggleYML(file))
	sys := h.system()
	cfg, err := config.Read(ctx, sys, hardnYML)
	if err != nil {
		return screen.WorkflowStep{}, flow.Flow{}, err
	}
	cfg.DryRun = want
	changes, err := steps.PlanSave(ctx, sys, "dryrun.mode", cfg, []string{"dryRun"})
	if err != nil {
		return screen.WorkflowStep{}, flow.Flow{}, err
	}
	var plan flow.Plan
	for _, ch := range changes {
		plan.View.Changes = append(plan.View.Changes, component.PlanChange{Title: ch.Title, Target: ch.Target, Diff: ch.Diff})
	}
	label, value, state := "Enable dry-run mode", "Enabled", component.ValueOK
	if !want {
		label, value, state = "Disable dry-run mode", "Disabled", component.ValueWarn
	}
	f := flow.Flow{
		Review: func() flow.Review {
			return flow.Review{Title: "Dry-Run Mode Configuration", Items: []component.KeyValue{{Key: "Dry-run Mode", Value: value, State: state}}}
		},
		Engine:  flow.Fixture{Planned: plan},
		Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: label + "?"} },
	}
	return screen.WorkflowStep{Title: component.TitleBar{Title: label}}, f, nil
}

// legacyFirewall is origin/main's Firewall screen under dry run, 2 chosen
// (Configure firewall), as pkg/menu prints it on Debian 13 with ufw
// installed and inactive and the SSH port 2230, its colours left out and
// the lines typed not echoed: pkg/characterization's
// menu/firewall_configure_dry_run.golden, which TestLegacyFirewallIsReal
// holds it to. Its glyphs are origin/main's, which it prints whatever the
// terminal.
var legacyFirewall = []string{
	strings.Repeat("~", 71),
	"UFW Firewall Configuration",
	"",
	"Current Firewall Status:",
	"⛛  UFW Installed       Yes Uncomplicated Firewall is available",
	"⭘  UFW Status          Inactive Firewall is not running",
	"⛛  SSH Port            Port 2230/tcp Using non-standard port (good security)",
	"",
	"⭘ No firewall rules configured",
	"Select an option",
	"",
	" 1) Enable firewall                 Start UFW and set to run at boot",
	" 2) Configure firewall              Set up default policies and SSH rules",
	" 3) Manage application profiles     Configure custom application rules",
	"",
	" 0) Return to main menu             ",
	"",
	"",
	"❯ Enter selection [0-3 or q]: ",
	"",
	"Configuring UFW firewall...",
	"-  [DRY-RUN] Would configure UFW with default policies and SSH rules",
	"-  [DRY-RUN] SSH port: 2230/tcp",
	"",
	"-  Press Enter to return to the main menu...",
}

// legacyNote heads the Firewall transcript, in the listing and in play.
const legacyNote = "origin/main's Firewall screen under dry run, 2 chosen, as pkg/menu prints it on a host with ufw installed " +
	"and inactive, not the menu's host; colours left out, what was typed (2, Enter) not echoed, the pause in the trunk's words " +
	"(DEV-050). No plan: no file, rule or command is named."

// drawLegacy renders the Firewall transcript in c, each line wrapped at
// the terminal's width as a terminal wraps it.
func drawLegacy(c component.Ctx) string {
	var out []string
	for _, l := range legacyFirewall {
		out = append(out, strings.Split(ansi.Hardwrap(l, termWidth(c.Env), false), "\n")...)
	}
	return strings.Join(out, "\n")
}

// dryMock is one mock the listing shows: its name, after dryRunPrefix,
// its note, which names it a mock, and its drawing, of the status host st
// where it draws a main menu (main: it draws the main menu or its mode,
// and its goldens take the main menu's cells).
type dryMock struct {
	name, note string
	main       bool
	draw       func(c component.Ctx, st hostStatus) (string, error)
}

// dryMocks are the listing's mocks, in order.
func dryMocks() []dryMock {
	menu := func(planOnly, proposed bool, from string) func(component.Ctx, hostStatus) (string, error) {
		return func(c component.Ctx, st hostStatus) (string, error) {
			return modeView{proposed: proposed, planOnly: planOnly, from: from}.drawMenu(c, mainMenu(st, planOnly, proposed))
		}
	}
	const host = "The main menu on its layout (00-TUI-MAIN), its status the gallery's hardened Debian 13 host's, as hardn's live menu reads it; "
	return []dryMock{
		{name: "menu", main: true, draw: menu(false, false, ""),
			note: "Mock. " + host + "origin/main's items, numbering, prompt and indicator, live."},
		{name: "menu-dry-run", main: true, draw: menu(true, false, ""),
			note: "Mock. " + host + "origin/main's items and indicator in dry run, as hardn -n starts."},
		{name: "menu-proposed", main: true, draw: menu(false, true, ""),
			note: "Mock, proposed (decisions 1 to 3). " + host + "item 6 left out, its key unused; the indicator's proposed words, live."},
		{name: "menu-proposed-plan-only", main: true, draw: menu(true, true, fromConfig),
			note: "Mock, proposed (decisions 1 to 3). " + host + "item 6 left out; plan only, set by hardn.yml's dryRun."},
		{name: "mode", main: true, draw: func(c component.Ctx, _ hostStatus) (string, error) { return drawModes(c), nil },
			note: "Mock. The mode indicator beside the menu heading, in each state: origin/main's words, then the proposed ones."},
		{name: "screen", draw: func(c component.Ctx, _ hostStatus) (string, error) { return dryRunScreen(false).Render(c), nil },
			note: "Mock (decision 1, alternative (a)), inferred from origin/main: its Dry-Run item, 6, on the sub-menu layout, in its words; live."},
		{name: "screen-enabled", draw: func(c component.Ctx, _ hostStatus) (string, error) { return dryRunScreen(true).Render(c), nil },
			note: "Mock (decision 1, alternative (a)). The same in dry run, its menu printed, which origin/main's never is."},
		{name: "toggle", draw: drawToggle,
			note: "Mock (decision 4, alternative (a)). Enable dry-run mode saved through the canonical flow: hardn.yml's dryRun edited in place, then y."},
		{name: "legacy-firewall", draw: func(c component.Ctx, _ hostStatus) (string, error) { return drawLegacy(c), nil },
			note: "Not a mock: " + legacyNote},
	}
}

// modeStates are the indicator's states the mode mock shows, each under
// its label.
var modeStates = []struct {
	label string
	mv    modeView
}{
	{"origin/main and Figma: live", modeView{}},
	{"origin/main: dry run, from -n", modeView{planOnly: true}},
	{"origin/main: dryRun true in hardn.yml, ignored (V3)", modeView{}},
	{"Proposed: live", modeView{proposed: true}},
	{"Proposed: plan only, from -n", modeView{proposed: true, planOnly: true, from: fromFlag}},
	{"Proposed: plan only, from hardn.yml", modeView{proposed: true, planOnly: true, from: fromConfig}},
}

// drawModes renders each state of modeStates: its label as prose, then
// the heading line.
func drawModes(c component.Ctx) string {
	var blocks []string
	for _, s := range modeStates {
		blocks = append(blocks, prose(c, s.label)+"\n"+s.mv.heading(c, "Select an option"))
	}
	return apart(blocks...)
}

// drawToggle renders the saved toggle's sequence, step by step, each
// under the gallery's header naming it, as -screens shows a workflow's.
func drawToggle(c component.Ctx, _ hostStatus) (string, error) {
	step, f, err := toggleFlow(context.Background(), false, true)
	if err != nil {
		return "", err
	}
	seq := screen.Sequence{
		Name:    "saved",
		Build:   func() (screen.WorkflowStep, flow.Flow) { return step, f },
		Answers: []screen.Answer{{Step: "confirm", Line: "y"}},
		Ends:    flow.Applied,
	}
	steps, err := seq.Play(c)
	if err != nil {
		return "", err
	}
	var blocks []string
	for i, name := range seq.Steps() {
		blocks = append(blocks, header(c, "toggle/"+name), strings.TrimLeft(steps[i], "\n"))
	}
	return apart(blocks...), nil
}

// dryRunUsage is the listing's usage line.
const dryRunUsage = "usage: go run ./internal/tools/uigallery dryrun [flags] [mock | all]..."

// dryRunList is the listing: each mock args name (none, or "all", is
// every mock) under the gallery's header, "dryrun <mock>", its note, then
// its drawing, a write each; -list prints their names. It sizes itself as
// play does (look.sizeTo); the main menus take their full form at every
// height (screen.CompactMainMenu).
func dryRunList(args []string, size sizer, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uigallery dryrun", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, dryRunUsage)
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
	mocks, err := selectDryMocks(fs.Args())
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", dryRunPrefix, err)
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
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", dryRunPrefix, err)
		return exitUsage
	}
	g, ok := look.gallery(stderr)
	if !ok {
		return exitError
	}
	rc, err := g.ctx(c.env)
	if err == nil {
		err = listDryRun(context.Background(), rc, c.env.Writer(stdout), mocks)
	}
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", dryRunPrefix, err)
		return exitError
	}
	return exitOK
}

// listDryRun writes each of mocks to w, rendered in rc, a write each: the
// gallery's header, the mock's note, then its drawing. The status host is
// read once, fresh.
func listDryRun(ctx context.Context, rc component.Ctx, w io.Writer, mocks []dryMock) error {
	sh, ok := findStatusHost(dryRunHost)
	if !ok {
		return fmt.Errorf("no status host %q", dryRunHost)
	}
	st, err := (&session{}).readStatus(ctx, sh.host())
	if err != nil {
		return err
	}
	for _, m := range mocks {
		out, err := m.draw(rc, st)
		if err == nil {
			_, err = io.WriteString(w, apart(header(rc, dryRunPrefix+" "+m.name),
				component.Callout{Kind: component.MessageInfo, Body: []string{m.note}}.Render(rc), out)+sectionBreak)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
	}
	return nil
}

// selectDryMocks returns the mocks args name, in order: every mock for
// none or "all". An argument that starts with "-" is a flag the flag
// package left unparsed, after a name.
func selectDryMocks(args []string) ([]dryMock, error) {
	all := dryMocks()
	if len(args) == 0 || slices.Contains(args, "all") {
		return all, nil
	}
	for _, a := range args {
		switch found := slices.ContainsFunc(all, func(m dryMock) bool { return m.name == a }); {
		case !found && strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("flag %s comes after a mock's name; flags go before them", a)
		case !found:
			return nil, fmt.Errorf("no mock %q; -list shows them", a)
		}
	}
	return slices.DeleteFunc(all, func(m dryMock) bool { return !slices.Contains(args, m.name) }), nil
}

// The play variants: origin/main's main menu, its 6 toggling dry run for
// the session or saved; and the proposed one.
const (
	variantSaved    = "saved"
	variantProposed = "proposed"
)

// dryRunStart returns what args name for play, "dryrun" or
// "dryrun/<variant>", and whether they name one; an error for a variant
// that is not one.
func dryRunStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != dryRunPrefix && !strings.HasPrefix(args[0], dryRunPrefix+"/")) {
		return "", false, nil
	}
	switch v := strings.TrimPrefix(args[0], dryRunPrefix+"/"); {
	case args[0] == dryRunPrefix, v == variantSaved, v == variantProposed:
		return args[0], true, nil
	}
	return "", false, fmt.Errorf("no %s variant %q; play %s, %s/%s or %s/%s", dryRunPrefix, args[0], dryRunPrefix, dryRunPrefix, variantSaved, dryRunPrefix, variantProposed)
}

// dryRunNote is play's note for the Dry-Run mocks.
const dryRunNote = "Play mode, mocks: the main menu on its layout, its status read from the gallery's hardened Debian 13 host; " +
	"its 3 the real DNS screen, on another Debian 13 host; its 1, 2 and 5 Figma's screen mocks, under dry run after a note " +
	"of what origin/main does there; its 4, under dry run, origin/main's Firewall screen as it prints. " +
	"Nothing on this machine changes."

// planOnly is whether the session plays in dry run: started with
// -sequence dry-run, or toggled by the Dry-Run item. The mocks' workflows
// then play their dry-run sequence, and the DNS screen stops at the plan.
func (s *session) planOnly() bool { return s.sequence == "dry-run" }

// setPlanOnly sets the session's mode.
func (s *session) setPlanOnly(on bool) {
	s.sequence = "normal"
	if on {
		s.sequence = "dry-run"
	}
}

// dryRunMode is where a play of the Dry-Run mocks takes its mode from:
// flag, -sequence dry-run, as hardn -n sets it; and file, the dryRun key
// of the simulated host's hardn.yml, false until the saved toggle edits
// it. The flag wins (plan §3.19): with it, plan only stays whatever the
// file says.
type dryRunMode struct {
	flag, file bool
}

// openDryRun returns the action that opens the main menu of the variant
// name names, until the user goes back from it, the status host read
// fresh. Its heading says the mode in origin/main's words, or in the
// proposed ones, where plan only comes from -n, the one way the proposed
// menu starts in it; it is swapped in each round (modeView.swap), which
// TestPlayDryRunGolden holds, a failed swap leaving the component's
// words.
func (s *session) openDryRun(name string) func() error {
	variant := strings.TrimPrefix(strings.TrimPrefix(name, dryRunPrefix), "/")
	proposed := variant == variantProposed
	return func() error {
		sh, ok := findStatusHost(dryRunHost)
		if !ok {
			return fmt.Errorf("%s: no status host %q", name, dryRunHost)
		}
		st, err := s.readStatus(context.Background(), sh.host())
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		mode := &dryRunMode{flag: s.planOnly()}
		return s.p.Run(func() nav.Screen {
			vm := mainMenu(st, s.planOnly(), proposed)
			sc := vm.Screen(s.p.Ctx, s.dryRunMenu(vm, variant == variantSaved, proposed, mode))
			mv := modeView{proposed: proposed, planOnly: s.planOnly()}
			if proposed && mv.planOnly {
				mv.from = fromFlag
			}
			if out, err := mv.swap(s.p.Ctx, sc.Above, vm.Heading); err == nil {
				sc.Above = out
			}
			return sc
		})
	}
}

// planOnlyRefused is what the proposed menu's 1 and 5 say under plan only
// (decision 6): each writes to the host whatever the mode until its slice
// puts it on the flow.
const planOnlyRefused = "Not available in plan only."

// refusedKeys are the items the proposed menu refuses under plan only:
// User Management (V2) and Backup (side-effects rows 29 and 30).
var refusedKeys = []string{"1", "5"}

// legacyDryRun says, above the screen mock an item leads to under dry
// run, what origin/main does there instead (gallery-dryrun.md §2.4), so
// that the mock's dry-run sequence, which stops at its plan, is not taken
// for origin/main's.
var legacyDryRun = map[string]string{
	"1": "origin/main, under dry run, creates the user, changes sudo and adds or removes keys all the same, " +
		"leaving out only the success line (V2). Below, Figma's mock of the flow slice 5 builds, not origin/main's.",
	"2": "origin/main, under dry run, asks \"Are you sure you want to disable root SSH access? (y/n)\", then prints " +
		"\"[DRY-RUN] Would disable root SSH access\". Below, Figma's mock of the flow slice 6 builds, not origin/main's.",
	"5": "origin/main, under dry run, has no check: with backups on, opening it writes and removes <path>/.write_test, " +
		"and its toggle and path save ./hardn.yml. Below, Figma's mock of the screen slice 3 builds, not origin/main's.",
}

// dryRunMenu binds the items of main menu vm: each where the main menu's
// mock leads (session.menu), but 4, which under dry run shows origin/main's
// Firewall screen, and 6, the Dry-Run screen, its toggle saved when saved
// is set; under dry run, 1, 2 and 5 say first what origin/main does there,
// and the proposed menu refuses 1 and 5.
func (s *session) dryRunMenu(vm screen.MainMenu, saved, proposed bool, mode *dryRunMode) nav.Menu {
	m := s.menu(screen.Mock{Name: mainMenuMock, Screen: vm})
	for i, o := range m.Options {
		switch key := o.Item.Key; {
		case key == "4":
			m.Options[i].Action = s.legacy(o.Action)
		case key == dryRunKey:
			m.Options[i].Action = s.openDryRunScreen(saved, mode)
		case proposed && slices.Contains(refusedKeys, key):
			m.Options[i].Action = s.unlessPlanOnly(s.say(component.Message{Kind: component.MessageWarn, Text: planOnlyRefused}), o.Action)
		case legacyDryRun[key] != "":
			m.Options[i].Action = s.unlessPlanOnly(s.noted(legacyDryRun[key], o.Action), o.Action)
		}
	}
	return m
}

// unlessPlanOnly returns an action that runs dry under plan only, live
// otherwise.
func (s *session) unlessPlanOnly(dry, live func() error) func() error {
	return func() error {
		if s.planOnly() {
			return dry()
		}
		return live()
	}
}

// noted returns an action that writes note, as a result is written
// (session.say), then runs then.
func (s *session) noted(note string, then func() error) func() error {
	return func() error {
		if err := s.say(component.Message{Text: note})(); err != nil {
			return err
		}
		return then()
	}
}

// legacy returns item 4's action: origin/main's Firewall transcript under
// dry run, after its note; live, missing, as the gallery has no Firewall
// screen.
func (s *session) legacy(missing func() error) func() error {
	return s.unlessPlanOnly(func() error {
		gap := strings.Repeat("\n", theme.SectionGap)
		_, err := io.WriteString(s.p.Out, gap+component.Message{Text: legacyNote}.Render(s.p.Ctx)+"\n"+gap+drawLegacy(s.p.Ctx)+"\n")
		return err
	}, missing)
}

// flagHolds is what the saved toggle says when the file is saved, or
// already says live, while -n keeps plan only (plan §3.19).
const flagHolds = "Plan only stays on: -n set it, and a flag given wins over hardn.yml (plan §3.19)."

// openDryRunScreen returns the action of the Dry-Run item: its screen
// until the user goes back, drawn each round in the session's mode, its
// item 1 the toggle: for the session, with origin/main's sentence, or
// saved, through toggleFlow on mode's file, the session's mode then the
// flag's, else the file's.
func (s *session) openDryRunScreen(saved bool, mode *dryRunMode) func() error {
	toggle := func() error {
		on := !s.planOnly()
		if !saved {
			s.setPlanOnly(on)
			return s.say(component.Message{Text: nowWords(on)})()
		}
		step, f, err := toggleFlow(context.Background(), mode.file, on)
		if err != nil {
			return err
		}
		f.Engine = s.sim.engine(dryRunPrefix+"/"+variantSaved+", "+step.Title.Title, f.Engine)
		o, err := step.Run(context.Background(), s.p, f)
		if err != nil || (o != flow.Applied && o != flow.Unchanged) {
			return err
		}
		mode.file = on
		s.setPlanOnly(mode.flag || mode.file)
		if mode.flag && !on {
			return s.say(component.Message{Text: flagHolds})()
		}
		return nil
	}
	return func() error {
		return s.p.Run(func() nav.Screen {
			vm := dryRunScreen(s.planOnly())
			return vm.Screen(s.p.Ctx, nav.Menu{Exit: vm.Menu.Exit, Options: []nav.Option{{Item: vm.Menu.Items[0], Action: toggle}}})
		})
	}
}
