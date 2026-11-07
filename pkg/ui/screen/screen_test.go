package screen

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// buffer is a strings.Builder safe for the flow's reports from any
// goroutine.
type buffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// prompter returns a Prompter in env with the user typing lines, and the
// transcript it writes, the echoed answers included.
func prompter(env term.Env, lines ...string) (nav.Prompter, *buffer) {
	out := new(buffer)
	return nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(out, lines...), false), Out: env.Writer(out)}, out
}

// sized returns a cell of the Figma target, truecolor and Unicode glyphs
// at width columns, height rows high, on the dark or the light theme.
func sized(width, height int, dark bool) uitest.Cell {
	bg := "light"
	if dark {
		bg = "dark"
	}
	env := term.Fixed(width, colorprofile.TrueColor, true, dark)
	env.Height = height
	return uitest.Cell{Name: fmt.Sprintf("truecolor-%s-unicode-%dx%d", bg, width, height), Env: env}
}

// compactGolden ends the name of a main menu's golden drawn with the
// compact form allowed (TestGolden).
const compactGolden = "-compact"

// mainCells are the main menu's cells: the core set, whose terminals are
// term.DefaultHeight (24) rows high, so that, with the compact form
// allowed, its 80- and 60-column cells show the compact form and plain
// mode, no terminal, the full one; 80x40, on both themes, where the full
// form fits; and 60x40, where the full form's stacked rows take 57 lines
// and the compact form, allowed, is drawn.
func mainCells() []uitest.Cell {
	return append(uitest.Core(), sized(80, 40, true), sized(80, 40, false), sized(60, 40, true))
}

// menuOf binds every item of m to an action that does nothing, as a
// screen's options would be bound to theirs.
func menuOf(m component.Menu) nav.Menu {
	out := nav.Menu{Exit: m.Exit}
	for _, it := range m.Items {
		out.Options = append(out.Options, nav.Option{Item: it, Action: func() error { return nil }})
	}
	return out
}

// TestGolden pins every mock (Mocks) on the core set, a file per drawing:
// a menu screen, the main menu also at 80x40 and 60x40; and each step of
// each workflow sequence. The main menu draws its full form in every cell
// (CompactMainMenu); its drawing with the compact form allowed, as before
// the gate, is kept beside it, <name>-compact (compactGolden): the
// compact form in the core set's 24-row terminal cells and at 60x40, the
// full form at 80x40 and with no terminal.
func TestGolden(t *testing.T) {
	for _, m := range Mocks() {
		cells := uitest.Core()
		if vm, ok := m.Screen.(MainMenu); ok {
			cells = mainCells()
			uitest.Golden(t, m.Name+compactGolden, cells, func(env term.Env) string { return vm.render(component.NewCtx(env), true) })
		}
		for i, name := range m.Names() {
			uitest.Golden(t, name, cells, func(env term.Env) string {
				out, err := m.Render(component.NewCtx(env))
				if err != nil {
					t.Fatalf("%s, %v: %v", name, env.Profile, err)
				}
				return out[i]
			})
		}
	}
}

// TestMocks checks the catalogue: names unique, each under its layout's
// name, with a source; each sequence's steps played in order make the
// whole transcript, the one a single run writes; and a golden file for
// every drawing and none besides.
func TestMocks(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range Mocks() {
		layout, _, _ := strings.Cut(m.Name, "/")
		want := "workflow-step"
		switch m.Screen.(type) {
		case MainMenu:
			want = "main-menu"
		case SubMenu:
			want = "sub-menu"
		}
		label := m.Source.Label
		if layout != want || m.Source.Delta == "" ||
			!strings.HasPrefix(label, "Figma ") && !strings.HasPrefix(label, "inferred from ") {
			t.Errorf("%s: under %s, label %q, delta %q; want it under %s, a label and a delta", m.Name, layout, label, m.Source.Delta, want)
		}
		if (m.Screen == nil) == (len(m.Sequences) == 0) {
			t.Errorf("%s: want a screen or sequences, not both or neither", m.Name)
		}
		for _, n := range m.Names() {
			if seen[n] {
				t.Errorf("%s is named twice", n)
			}
			seen[n] = true
		}
		if _, ok := m.Screen.(MainMenu); ok {
			seen[m.Name+compactGolden] = true
		}
		for _, s := range m.Sequences {
			env := term.Fixed(80, colorprofile.TrueColor, true, true)
			steps, err := s.Play(component.NewCtx(env))
			step, f := s.Build()
			p, out := prompter(env, answers(s)...)
			o, rerr := step.Run(context.Background(), p, f)
			if err != nil || rerr != nil || o != s.Ends || strings.Join(steps, "\n") != strings.TrimSuffix(out.String(), "\n") {
				t.Errorf("%s/%s: Play() = %v, Run() = %v, %v; want its steps to make the transcript, ending %v", m.Name, s.Name, err, o, rerr, s.Ends)
			}
		}
	}
	err := filepath.WalkDir(filepath.Join("testdata", "golden"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := strings.TrimSuffix(filepath.ToSlash(strings.TrimPrefix(path, filepath.Join("testdata", "golden")+string(filepath.Separator))), ".golden")
		if !seen[name] {
			t.Errorf("golden %s has no mock", path)
		}
		delete(seen, name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for n := range seen {
		t.Errorf("mock %s has no golden", n)
	}
}

// TestDiverged checks that a sequence that ends otherwise than it says,
// with no error, fails with a *Diverged that holds the whole transcript:
// the root-login toggle's normal path on a terminal too narrow for its
// typed confirmation, below 29 columns in plain mode (27 in Unicode
// glyphs), which the flow cancels without asking it. Mock.Render's error
// wraps it.
func TestDiverged(t *testing.T) {
	var toggle Mock
	for _, m := range Mocks() {
		if m.Name == "workflow-step/ssh-login-toggle-confirmation" {
			toggle = m
		}
	}
	if len(toggle.Sequences) == 0 || toggle.Sequences[0].Name != "normal" {
		t.Fatalf("no normal sequence for the root-login toggle: %+v", toggle)
	}
	c := component.NewCtx(term.Fixed(28, colorprofile.NoTTY, false, true))
	steps, err := toggle.Sequences[0].Play(c)
	var d *Diverged
	if !errors.As(err, &d) || steps != nil || d.Ended != flow.Cancelled || d.Want != flow.Applied ||
		!strings.Contains(d.Transcript, "Operation Cancelled") || strings.HasSuffix(d.Transcript, "\n") ||
		err.Error() != "sequence normal ended cancelled, want applied" {
		t.Fatalf("Play() = %q, %v; want a *Diverged, cancelled for applied, with the transcript", steps, err)
	}
	if _, err := toggle.Render(c); !errors.As(err, &d) {
		t.Errorf("Render() = %v, want it to wrap a *Diverged", err)
	}
}

// TestCreateUserPlan checks that the create-user fixture plans what its
// form was answered, so that its review, plan and confirmation agree for
// any answers: a step per answer that needs one, named for the user; and,
// played on the flow, bob's answers give bob's plan and question.
func TestCreateUserPlan(t *testing.T) {
	tests := []struct {
		u    newUser
		want []string
	}{
		{newUser{name: "george", sudo: true}, []string{"Create user george", "Add george to the sudo group", "Let george use sudo without a password"}},
		{newUser{name: "bob"}, []string{"Create user bob"}},
		{newUser{name: "bob", sudo: true, password: true}, []string{"Create user bob", "Add bob to the sudo group"}},
		{newUser{name: "bob", addKey: true, key: "ssh-ed25519 AAAA bob"}, []string{"Create user bob", "Add an SSH key for bob"}},
	}
	for _, tt := range tests {
		var got []string
		for _, ch := range tt.u.plan().View.Changes {
			got = append(got, ch.Title)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%+v: plans %q, want %q", tt.u, got, tt.want)
		}
	}
	env := term.Fixed(80, colorprofile.NoTTY, false, true)
	step, f := createUser(false, false)
	p, out := prompter(env, "bob", "n", "n", "y")
	o, err := step.Run(context.Background(), p, f)
	if got := out.String(); o != flow.Applied || err != nil || !strings.Contains(got, "Create user 'bob'?") ||
		!regexp.MustCompile(`Create user bob +Done\n`).MatchString(got) || strings.Contains(got, "george") || strings.Contains(got, "sudo group") {
		t.Errorf("bob, no sudo, no key: Run() = %v, %v, transcript\n%s", o, err, got)
	}
}

// answers returns the lines s types.
func answers(s Sequence) []string {
	var out []string
	for _, a := range s.Answers {
		out = append(out, a.Line)
	}
	return out
}

// TestScreen checks that nav.Run, given a layout's Screen, draws what the
// layout's Render renders, in every golden cell: the drawing, then the
// prompt and the one space the line reader writes after it; for the main
// menu with its compact form allowed too, as CompactMainMenu, gated,
// would allow it (screen and render), the compact form in the 24-row
// cells and at 60x40. A view-model whose Menu.Compact is set draws as one
// whose is not: the main menu sets it from the terminal's height, and a
// sub-menu has no compact form.
func TestScreen(t *testing.T) {
	main, user := MainMenuFixture(), UserFixture()
	mainCompact, userCompact := main, user
	mainCompact.Menu.Compact, userCompact.Menu.Compact = true, true
	tests := []struct {
		name   string
		render func(component.Ctx) string
		screen func(component.Ctx) nav.Screen
	}{
		{"main menu", main.Render, func(c component.Ctx) nav.Screen { return main.Screen(c, menuOf(main.Menu)) }},
		{"sub-menu", user.Render, func(c component.Ctx) nav.Screen { return user.Screen(c, menuOf(user.Menu)) }},
		{"main menu, Menu.Compact set", mainCompact.Render, func(c component.Ctx) nav.Screen { return main.Screen(c, menuOf(main.Menu)) }},
		{"main menu, the compact form allowed", func(c component.Ctx) string { return main.render(c, true) },
			func(c component.Ctx) nav.Screen { return main.screen(c, menuOf(main.Menu), true) }},
		{"sub-menu, Menu.Compact set", userCompact.Render, func(c component.Ctx) nav.Screen { return user.Screen(c, menuOf(user.Menu)) }},
	}
	for _, tt := range tests {
		for _, cell := range mainCells() {
			p, out := prompter(cell.Env, "q")
			if err := p.Run(func() nav.Screen { return tt.screen(p.Ctx) }); !errors.Is(err, nav.ErrQuit) {
				t.Errorf("%s, %s: Run() = %v, want ErrQuit", tt.name, cell.Name, err)
			}
			var want strings.Builder
			if _, err := io.WriteString(cell.Env.Writer(&want), tt.render(p.Ctx)+" "); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != want.String()+"q\n" {
				t.Errorf("%s, %s: nav.Run drew\n%s\nwant Render's\n%s", tt.name, cell.Name, got, want.String()+"q\n")
			}
		}
	}
}

// TestCompact checks the main menu's height rules (refactor plan §3.15),
// with the compact form allowed, as CompactMainMenu, gated, would allow it
// (render), on a terminal at every width from 1 to 130 columns and at 0,
// 1 and 24 to 60 rows: the full form wherever it fits the terminal, prompt
// included, from theme.CompactBelow rows, and the compact form elsewhere,
// so that the drawing fits the terminal whenever either form does, the
// host badge and the risk line on screen; never the full form on a
// terminal it does not fit. From theme.StackBelow columns, where the rows
// sit side by side, the compact form takes at most theme.CompactBudget
// lines and the full form fits theme.CompactBelow rows. At 60 columns,
// where they stack, the full form takes 55 lines and the compact form 34
// (DEV-052; 57 in full with item 6, before slice 2b's unit (d)). At every width the compact form's context bar keeps the
// blank line under it. With no terminal, the full form whatever the
// height.
func TestCompact(t *testing.T) {
	vm := MainMenuFixture()
	heights := []int{0, 1}
	for h := term.DefaultHeight; h <= 60; h++ {
		heights = append(heights, h)
	}
	for w := 1; w <= 130; w++ {
		env := term.Fixed(w, colorprofile.TrueColor, true, true)
		c := component.NewCtx(env)
		full, short := vm.form(c, false), vm.form(c, true)
		nFull, nShort := lines(full), lines(short)
		if nShort > nFull {
			t.Errorf("%d columns: the compact form takes %d lines, more than the full form's %d", w, nShort, nFull)
		}
		if w >= theme.StackBelow && nFull >= theme.CompactBelow {
			t.Errorf("%d columns: the full form takes %d lines, not under %d", w, nFull, theme.CompactBelow)
		}
		if bar := strings.SplitN(ansi.Strip(short), "\n", 3); len(bar) < 3 || bar[0] != ansi.Strip(vm.Context.Render(c)) || bar[1] != "" {
			t.Errorf("%d columns: want the compact form to open with the context bar and a blank line, got:\n%s", w, ansi.Strip(short))
		}
		for _, h := range heights {
			env.Height = h
			rows := h
			if rows < 1 {
				rows = term.DefaultHeight
			}
			want, form := short, "compact"
			if rows >= theme.CompactBelow && nFull <= rows {
				want, form = full, "full"
			}
			got := vm.render(component.NewCtx(env), true)
			if got != want {
				t.Errorf("%dx%d: want the %s form (full %d lines, compact %d), got:\n%s", w, h, form, nFull, nShort, ansi.Strip(got))
			}
			if n := lines(got); n > rows && (nFull <= rows || nShort <= rows) {
				t.Errorf("%dx%d: %d lines drawn, where the %s form takes %d", w, h, n, form, lines(want))
			}
			if w >= theme.StackBelow && rows < theme.CompactBelow && lines(got) > theme.CompactBudget {
				t.Errorf("%dx%d: the compact main menu takes %d lines, over the budget of %d", w, h, lines(got), theme.CompactBudget)
			}
		}
		piped := term.Fixed(w, colorprofile.NoTTY, false, true)
		whole := vm.form(component.NewCtx(piped), false)
		for _, h := range []int{0, 1, term.DefaultHeight, theme.CompactBelow, 60} {
			piped.Height = h
			if got := vm.render(component.NewCtx(piped), true); got != whole {
				t.Errorf("%dx%d, no terminal: want the full form, got:\n%s", w, h, got)
			}
		}
	}
	env := term.Fixed(60, colorprofile.TrueColor, true, true)
	env.Height = theme.CompactBelow
	c := component.NewCtx(env)
	if nFull, nShort := lines(vm.form(c, false)), lines(vm.form(c, true)); nFull != 55 || nShort != 34 {
		t.Errorf("60 columns: the full form takes %d lines and the compact %d, want 55 and 34 (README §9.12)", nFull, nShort)
	}
	if out := ansi.Strip(vm.render(c, true)); strings.HasPrefix(out, "hardn") || !strings.Contains(out, "BOS-SAN-L01") || !strings.Contains(out, "Risk Level") {
		t.Errorf("60x40: want the compact form, the host badge and the risk line on screen, got:\n%s", out)
	}
}

// TestStatusBlocks checks the blocks a caller's own menu goes under
// (StatusBlocks; hardn's live main menu, next/s2-menu-live), with the
// compact form allowed, as CompactMainMenu, gated, would allow it: at widths
// from 1 to 130, heights 0, 1 and 24 to 60, and menus of several heights,
// the full form's blocks exactly where they fit with the gap and the
// menu, from theme.CompactBelow rows, or with no terminal, else the
// compact form's; each is what Render draws above the heading in its
// form; and for the lines Render's own menu takes, the form Render
// chooses.
func TestStatusBlocks(t *testing.T) {
	vm := MainMenuFixture()
	heights := []int{0, 1}
	for h := term.DefaultHeight; h <= 60; h++ {
		heights = append(heights, h)
	}
	mode := vm.Mode
	for w := 1; w <= 130; w++ {
		env := term.Fixed(w, colorprofile.TrueColor, true, true)
		c := component.NewCtx(env)
		heading := component.MenuHeading{Title: vm.Heading, Mode: &mode}.Render(c)
		full, short := join(vm.blocks(c, false)...), join(vm.blocks(c, true)...)
		gap := strings.Repeat("\n", theme.SectionGap+1)
		if !strings.HasPrefix(vm.form(c, false), full+gap+heading) || !strings.HasPrefix(vm.form(c, true), short+gap+heading) {
			t.Fatalf("%d columns: the blocks are not what Render draws above the heading", w)
		}
		own := lines(vm.form(c, false)) - lines(full) - theme.SectionGap // Render's menu, heading to prompt
		for _, h := range heights {
			env.Height = h
			c := component.NewCtx(env)
			rows := h
			if rows < 1 {
				rows = term.DefaultHeight
			}
			for _, below := range []int{0, 16, 17, own, 40} {
				want := short
				if rows >= theme.CompactBelow && lines(full)+theme.SectionGap+below <= rows {
					want = full
				}
				if got := vm.statusBlocks(c, below, true); got != want {
					t.Errorf("%dx%d, a menu of %d lines: got\n%s", w, h, below, ansi.Strip(got))
				}
			}
			if (vm.statusBlocks(c, own, true) == full) == vm.compact(c, true) {
				t.Errorf("%dx%d: with Render's own menu, the blocks are not in Render's form", w, h)
			}
		}
		piped := term.Fixed(w, colorprofile.NoTTY, false, true)
		piped.Height = term.DefaultHeight
		pc := component.NewCtx(piped)
		if got := vm.statusBlocks(pc, 40, true); got != join(vm.blocks(pc, false)...) {
			t.Errorf("%d columns, no terminal: want the full form's blocks, got\n%s", w, got)
		}
	}
}

// TestHeights records the main menu's heights with Figma's seven status
// rows and with an eighth, the DNS row of next/s2-menu-live
// (docs/engine/menu-live.md §6), on a terminal of 80 and 60 columns, 24
// and 40 rows, Unicode and ASCII, in both forms: the full form, 35 lines
// with the eighth row from 64 columns, 57 at 60 columns where the rows
// stack, so that on 24 rows its top 11 lines scroll off, the risk level
// among them (one more each with item 6, which slice 2b's unit (d)
// removed from the menu); and, with the compact form allowed (render),
// the form it took and its lines: with the eighth row 25 at 80x24, one
// over theme.CompactBudget, its distribution bar scrolled off, and 35 in
// full at 80x40. Live: StatusBlocks above a menu of 17 lines after the gap,
// as hardn's live main menu drew its legacy menu (pkg/menu: heading to
// prompt) until next/s2b-menu put it on this layout, with each form's
// blocks. Drawn, by Render, StatusBlocks and the form chosen: the full
// form's, the compact one gated (CompactMainMenu); flipped, the allowed
// form's. No row has a line under it here: each adds one more. hardn's
// own main menu is pinned on the gallery's hosts by pkg/menu's
// TestMainMenuHeights.
func TestHeights(t *testing.T) {
	const legacyMenu = 17
	eight := MainMenuFixture()
	eight.Status.Rows = append(eight.Status.Rows, component.StatusRow{Label: "DNS", State: component.StatusConfigured, Detail: "192.0.2.54, 2001:db8::54"})
	for _, tc := range []struct {
		vm                MainMenu
		rows, w, h        int
		full, live        int    // the full form
		form              string // with the compact form allowed
		allowed, liveForm int
	}{
		{MainMenuFixture(), 7, 80, 24, 34, 36, "compact", 24, 31},
		{MainMenuFixture(), 7, 80, 40, 34, 36, "full", 34, 36},
		{eight, 8, 80, 24, 35, 37, "compact", 25, 32},
		{eight, 8, 80, 40, 35, 37, "full", 35, 37},
		{eight, 8, 60, 24, 57, 48, "compact", 36, 42},
		{eight, 8, 60, 40, 57, 48, "compact", 36, 42},
	} {
		for _, unicode := range []bool{true, false} {
			env := term.Fixed(tc.w, colorprofile.TrueColor, unicode, true)
			env.Height = tc.h
			c := component.NewCtx(env)
			name := func(compact bool) string {
				if tc.vm.compact(c, compact) {
					return "compact"
				}
				return "full"
			}
			live := func(compact bool) int {
				return lines(tc.vm.statusBlocks(c, legacyMenu, compact)) + theme.SectionGap + legacyMenu
			}
			full, form, allowed := lines(tc.vm.render(c, false)), name(true), lines(tc.vm.render(c, true))
			if full != tc.full || live(false) != tc.live || name(false) != "full" ||
				form != tc.form || allowed != tc.allowed || live(true) != tc.liveForm {
				t.Errorf("%d rows, %dx%d, unicode %t: full %d lines, live %d; allowed, the %s form, %d, live %d; want %d, %d; the %s form, %d, %d",
					tc.rows, tc.w, tc.h, unicode, full, live(false), form, allowed, live(true), tc.full, tc.live, tc.form, tc.allowed, tc.liveForm)
			}
			drawn, drawnLive, drawnForm := tc.full, tc.live, "full"
			if CompactMainMenu {
				drawn, drawnLive, drawnForm = tc.allowed, tc.liveForm, tc.form
			}
			if lines(tc.vm.Render(c)) != drawn || lines(tc.vm.StatusBlocks(c, legacyMenu))+theme.SectionGap+legacyMenu != drawnLive ||
				name(CompactMainMenu) != drawnForm {
				t.Errorf("%d rows, %dx%d, unicode %t: drawn not the %s form (CompactMainMenu %t)", tc.rows, tc.w, tc.h, unicode, drawnForm, CompactMainMenu)
			}
		}
	}
}

// TestFullAtEveryHeight holds the gate (CompactMainMenu, DISABLED): the
// main menu draws its full form at every width from 1 to 130 and every
// height, on a terminal and off it, through Render and through Screen,
// which nav.Run draws, with its menu in one column; and StatusBlocks
// draws the full form's blocks for a menu of any height. Flipped, the
// compact form returns where TestCompact finds it.
func TestFullAtEveryHeight(t *testing.T) {
	if CompactMainMenu {
		t.Skip("CompactMainMenu is set: TestCompact holds the forms")
	}
	vm := MainMenuFixture()
	m := menuOf(vm.Menu)
	for w := 1; w <= 130; w++ {
		for _, tty := range []bool{true, false} {
			profile := colorprofile.TrueColor
			if !tty {
				profile = colorprofile.NoTTY
			}
			env := term.Fixed(w, profile, true, true)
			full := vm.form(component.NewCtx(env), false)
			blocks := join(vm.blocks(component.NewCtx(env), false)...)
			for _, h := range []int{0, 1, term.DefaultHeight, theme.CompactBelow - 1, theme.CompactBelow, 60} {
				env.Height = h
				c := component.NewCtx(env)
				sc := vm.Screen(c, m)
				if vm.Render(c) != full || sc.Compact || sc.Above != vm.above(c, false) || vm.StatusBlocks(c, 17) != blocks {
					t.Errorf("%dx%d, terminal %t: not the full form", w, h, tty)
				}
			}
		}
	}
}

// TestScreenMenu checks that the main menu's Screen, with the compact
// form allowed as CompactMainMenu, gated, would allow it, chooses its form
// by the menu it is given, the one nav.Run draws, rather than by vm.Menu: a
// view-model with no menu, given the fixture's, gets the form the
// fixture takes, at every width from 1 to 130 and at 0, 1 and 24 to 60
// rows. At 60 columns from 40 to 54 rows the full form fits without the
// menu's lines and not with them.
func TestScreenMenu(t *testing.T) {
	vm := MainMenuFixture()
	bare := vm
	bare.Menu = component.Menu{}
	m := menuOf(vm.Menu)
	heights := []int{0, 1}
	for h := term.DefaultHeight; h <= 60; h++ {
		heights = append(heights, h)
	}
	for w := 1; w <= 130; w++ {
		for _, h := range heights {
			env := term.Fixed(w, colorprofile.TrueColor, true, true)
			env.Height = h
			c := component.NewCtx(env)
			if got, want := bare.screen(c, m, true).Compact, vm.compact(c, true); got != want {
				t.Errorf("%dx%d: Screen with no vm.Menu sets Compact %v, want %v as for the menu drawn", w, h, got, want)
			}
		}
	}
}

// TestCompactForm checks what the compact form keeps and leaves out, the
// form allowed as CompactMainMenu, gated, would allow it, on plain text at
// 80x24: the context bar keeps its blank line, and the
// menu's items start on the line under the heading, in 24 lines.
func TestCompactForm(t *testing.T) {
	env := term.Fixed(80, colorprofile.ASCII, false, true)
	env.Height = term.DefaultHeight
	out := ansi.Strip(MainMenuFixture().render(component.NewCtx(env), true))
	want := `# Debian 12 (Bookworm) #########################################

  [BOS-SAN-L01]            192.0.2.10
                           182 days, 2 hours, 26 minutes

  [*] Risk Level           Moderate ~ medium security
  [+] User                 Configured non-root, sudo
  [+] SSH Login            Configured root disallowed
  [+] SSH Auth             Configured key-only auth
  [+] SSH Port             Configured non-default (2230)
  [+] Firewall             Configured deny policy
  [ ] Auto Updates         Not Configured
  [ ] AppArmor             Not Configured

  Select an option         Mode Live
   1) User Management       7) Backup
   2) SSH Login             8) Environment
   3) Firewall              9) System Details
   4) DNS                  10) Logs
   6) Run All

   0) Exit                 q to quit

    > Enter selection [0-4, 6-10 or q]:`
	if out != want || lines(out) != theme.CompactBudget {
		t.Errorf("compact main menu, %d lines:\n%s\nwant, in %d,\n%s", lines(out), out, theme.CompactBudget, want)
	}
}

// TestEmptyBlocks checks that a block that renders nothing is left out
// with its gap: an empty StatusList on the main menu, and a sub-menu's
// subject line, status rows, sections and heading.
func TestEmptyBlocks(t *testing.T) {
	c := component.NewCtx(term.Fixed(80, colorprofile.NoTTY, false, true))
	vm := MainMenuFixture()
	vm.Status = component.StatusList{}
	if out := ansi.Strip(vm.Render(c)); !strings.Contains(out, "medium security\n\n----") {
		t.Errorf("main menu with no status rows:\n%s\nwant the risk line, one blank line, then the divider", out)
	}
	sub := SubMenu{
		Title:    trail("Firewall"),
		Subject:  component.SubjectLine{Label: "Managing"},
		Sections: []Section{{Header: component.SectionHeader{Title: "Rules"}}},
		Menu:     component.Menu{Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:   "Enter your choice",
	}
	want := ansi.Strip(trail("Firewall").Render(c)) + "\n\n   0) Return\n\n    > Enter your choice [0 or q]:"
	if out := ansi.Strip(sub.Render(c)); out != want {
		t.Errorf("sub-menu with empty blocks:\n%s\nwant\n%s", out, want)
	}
	// With no title the bar is all fill, the title bar's empty state.
	if out, want := ansi.Strip((SubMenu{}).Render(c)), ansi.Strip(component.TitleBar{}.Render(c))+"\n\n    > [0 or q]:"; out != want {
		t.Errorf("empty sub-menu: %q, want %q", out, want)
	}
	if out := (Section{Header: component.SectionHeader{Title: "Rules"}}).Render(c); out != "" {
		t.Errorf("a section with nothing under its header renders %q, want nothing", out)
	}
}

// TestSubMenuFrames checks that the sub-menu layout draws Figma's other
// sub-menu frames by leaving out the blocks they have no data for:
// USER-MANAGE_USER-SELECT_USER, a menu of users under its own heading, and
// USER-MANAGE_USER-MANAGEMENT, a subject line over a menu. Plain, at 80
// columns.
func TestSubMenuFrames(t *testing.T) {
	c := component.NewCtx(term.Fixed(80, colorprofile.NoTTY, false, true))
	head := ansi.Strip(titleBar(trail("User Management", "Manage User"), TitleTrail).Render(c)) + "\n\n"
	tests := []struct {
		name string
		vm   SubMenu
		want string
	}{
		{"USER-MANAGE_USER-SELECT_USER", selectUserFixture(),
			head + "  Select a user\n\n   1) george\n   2) alice\n\n   0) Return\n\n    > Enter your choice [0-2 or q]:"},
		{"USER-MANAGE_USER-MANAGEMENT", manageUserFixture(),
			head + "  Managing: [george]\n\n  Select an option\n\n   1) Sudo Method          Toggle password requirement\n" +
				"   2) Manage SSH keys      Add or remove SSH keys\n\n   0) Return\n\n    > Enter your choice [0-2 or q]:"},
	}
	for _, tt := range tests {
		if out := ansi.Strip(tt.vm.Render(c)); out != tt.want {
			t.Errorf("%s:\n%s\nwant\n%s", tt.name, out, tt.want)
		}
	}
}

// TestTitleTrail checks P-08's title bar (DEV-053, DEV-054): every
// sub-screen opens with it, with no Header and no Breadcrumb line; with
// the trail off, as TitleTrail has it by default, the bar shows the
// screen's title alone, as Figma's frames do; with it on, the sections
// above the screen come first, "User Management > Manage User", so that
// flipping the constant is all the trail needs. Render draws TitleTrail's
// form, and no trail names the product. Plain, at 80 columns.
func TestTitleTrail(t *testing.T) {
	c := component.NewCtx(term.Fixed(80, colorprofile.NoTTY, false, true))
	fill := func(text string) string {
		return "# " + text + " " + strings.Repeat("#", lipgloss.Width(ansi.Strip(component.TitleBar{}.Render(c)))-3-len(text))
	}
	step, _ := ManageSudoFixture()
	tests := []struct {
		name     string
		render   func(withTrail bool) string
		off, on  string
		rendered string
	}{
		{"01-USER, the top of its section", func(w bool) string { return UserFixture().render(c, w) },
			fill("User Management"), fill("User Management"), UserFixture().Render(c)},
		{"USER-MANAGE_USER-MANAGEMENT", func(w bool) string { return manageUserFixture().render(c, w) },
			fill("Manage User"), fill("User Management > Manage User"), manageUserFixture().Render(c)},
		{"USER-MANAGE_SUDO-CONFIRMATION", func(w bool) string { return step.render(c, w) },
			fill("Manage User"), fill("User Management > Manage User"), step.Render(c)},
	}
	for _, tt := range tests {
		for _, withTrail := range []bool{false, true} {
			want := tt.off
			if withTrail {
				want = tt.on
			}
			first, _, _ := strings.Cut(ansi.Strip(tt.render(withTrail)), "\n")
			if first != want {
				t.Errorf("%s, trail %t: first line\n%s\nwant\n%s", tt.name, withTrail, first, want)
			}
		}
		if tt.rendered != tt.render(TitleTrail) {
			t.Errorf("%s: Render does not draw TitleTrail's form", tt.name)
		}
	}
	bars := map[string]component.TitleBar{}
	for _, m := range Mocks() {
		switch vm := m.Screen.(type) {
		case SubMenu:
			bars[m.Name] = vm.Title
		case MainMenu:
		default:
			for _, sq := range m.Sequences {
				step, _ := sq.Build()
				bars[m.Name+"/"+sq.Name] = step.Title
			}
		}
	}
	for name, bar := range bars {
		if bar.Title == "" {
			t.Errorf("%s: no title", name)
		}
		for _, it := range append(slices.Clone(bar.Parents), bar.Title) {
			if strings.EqualFold(strings.TrimSpace(it), "hardn") {
				t.Errorf("%s: the trail %q names the product", name, append(bar.Parents, bar.Title))
			}
		}
	}
}

// TestRunGap checks where the workflow step's gap goes: after the screen
// before any prompt, whether the flow opens with its review or with a
// form, and before it once a prompt was asked, as when a menu's action
// opens the workflow. Each block stands one blank line from the next. A
// failed write runs nothing, and a flow the flow refuses before writing
// (no Engine, no Confirm, a context already done) leaves no screen.
func TestRunGap(t *testing.T) {
	env := term.Fixed(80, colorprofile.NoTTY, false, true)
	step, sudo := ManageSudoFixture()
	screen := ansi.Strip(step.Render(component.NewCtx(env)))
	var name string
	withForm := sudo
	withForm.Form = form.Form{Fields: []form.Field{&form.Text{Title: "Username", Value: &name}}}
	tests := []struct {
		name  string
		f     flow.Flow
		asked bool
		lines []string
		want  string // the transcript's start
	}{
		{"before any prompt, the review first", sudo, false, []string{"y"}, screen + "\n\n# Warning\n"},
		{"before any prompt, a form first", withForm, false, []string{"george", "y"}, screen + "\n\n  Username [0 or q]: george\n\n# Warning\n"},
		{"after a prompt", sudo, true, []string{"1", "y"}, "    > Choose [1, 0 or q]: 1\n\n" + screen + "\n\n# Warning\n"},
	}
	for _, tt := range tests {
		p, out := prompter(env, tt.lines...)
		if tt.asked {
			if _, err := p.Choose(nav.Menu{Options: []nav.Option{{Item: component.MenuItem{Key: "1", Label: "Sudo"}, Action: func() error { return nil }}}}, "Choose"); err != nil {
				t.Fatal(err)
			}
		}
		o, err := step.Run(context.Background(), p, tt.f)
		if got := out.String(); o != flow.Applied || err != nil || !strings.HasPrefix(got, tt.want) || strings.Contains(got, "\n\n\n") {
			t.Errorf("%s: Run() = %v, %v, transcript\n%s\nwant Applied, nil, and it to start\n%s\nwith no two blank lines together", tt.name, o, err, got, tt.want)
		}
	}
	p, _ := prompter(env, "y")
	p.Out = failing{}
	if o, err := step.Run(context.Background(), p, sudo); o != flow.NotRun || !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("a failed write: Run() = %v, %v; want NotRun and the write's error", o, err)
	}
	noEngine, noConfirm := sudo, sudo
	noEngine.Engine, noConfirm.Confirm = nil, nil
	done, cancel := context.WithCancel(context.Background())
	cancel()
	refused := []struct {
		name string
		ctx  context.Context
		f    flow.Flow
		want error
	}{
		{"no engine", context.Background(), noEngine, flow.ErrFlow},
		{"no confirm", context.Background(), noConfirm, flow.ErrFlow},
		{"a context done before the run", done, sudo, context.Canceled},
	}
	for _, tt := range refused {
		p, out := prompter(env, "y")
		if o, err := step.Run(tt.ctx, p, tt.f); o != flow.NotRun || !errors.Is(err, tt.want) || out.String() != "" {
			t.Errorf("%s: Run() = %v, %v, transcript %q; want NotRun, %v and nothing written", tt.name, o, err, out.String(), tt.want)
		}
	}
}

// failing fails every Write.
type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// maxWidth is the widest a line of a screen may be at terminal width w,
// worked out from the §3.15 constants: the content width under
// theme.CapContent, unless theme.BarsFillContent is off, when a bar or the
// divider takes the terminal's width.
func maxWidth(w int) int {
	if theme.CapContent && theme.BarsFillContent {
		return min(w, theme.ContentWidth)
	}
	return w
}

// TestOutputRules renders each mock's screen (Mocks), and the screen above
// each workflow sequence, at every width from 1 to 130, with colour,
// without it and in plain mode, on a 24-row terminal and a 40-row
// one, and checks the components' output rules hold for the screen: no
// line wider than the content (maxWidth), no line holding a tab, no
// trailing space outside a painted background, and no trailing newline.
func TestOutputRules(t *testing.T) {
	layouts := map[string]func(component.Ctx) string{}
	for _, m := range Mocks() {
		if m.Screen != nil {
			layouts[m.Name] = m.Screen.Render
			continue
		}
		for _, s := range m.Sequences {
			step, _ := s.Build()
			layouts[m.Name+"/"+s.Name] = step.Render
		}
	}
	for name, render := range layouts {
		for w := 1; w <= 130; w++ {
			for _, h := range []int{term.DefaultHeight, theme.CompactBelow} {
				for _, env := range []term.Env{
					term.Fixed(w, colorprofile.TrueColor, true, true),
					term.Fixed(w, colorprofile.ASCII, true, false),
					term.Fixed(w, colorprofile.NoTTY, false, true),
				} {
					env.Height = h
					out := render(component.NewCtx(env))
					where := fmt.Sprintf("%s, %dx%d, %v", name, w, h, env.Profile)
					if strings.HasSuffix(out, "\n") {
						t.Errorf("%s: ends in a newline", where)
					}
					for _, l := range strings.Split(out, "\n") {
						if lw := lipgloss.Width(l); lw > maxWidth(w) || strings.Contains(l, "\t") {
							t.Errorf("%s: line %q is %d cells or holds a tab", where, ansi.Strip(l), lw)
						}
						if strings.HasSuffix(l, " ") || !env.HasColor() && strings.HasSuffix(ansi.Strip(l), " ") {
							t.Errorf("%s: line %q ends in a space", where, ansi.Strip(l))
						}
					}
				}
			}
		}
	}
}
