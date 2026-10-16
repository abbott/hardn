package screen

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// typist plays the user at a terminal, as nav's and flow's tests do: each
// Read returns the next line, typed and ended with Enter, and echoes it to
// echo as a terminal shows it. With no lines left it reports the end of
// the input.
type typist struct {
	lines   []string
	pending []byte
	echo    io.Writer
}

func (t *typist) Read(p []byte) (int, error) {
	if len(t.pending) == 0 {
		if len(t.lines) == 0 {
			return 0, io.EOF
		}
		line := t.lines[0]
		t.lines = t.lines[1:]
		if _, err := io.WriteString(t.echo, line); err != nil {
			return 0, err
		}
		t.pending = []byte(line + "\n")
	}
	n := copy(p, t.pending)
	t.pending = t.pending[n:]
	return n, nil
}

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
	return nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(&typist{lines: lines, echo: out}, false), Out: env.Writer(out)}, out
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

// mainCells are the main menu's cells: the core set, whose terminals are
// term.DefaultHeight (24) rows high, so its 80- and 60-column cells show
// the compact form and plain mode, no terminal, the full one; and the
// full form at 80x40, on both themes.
func mainCells() []uitest.Cell {
	return append(uitest.Core(), sized(80, 40, true), sized(80, 40, false))
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

// TestGolden pins each layout's fixture on the core set: the main menu
// also at 80x40, where it shows its full form; the workflow step as the
// transcript of its sequence, its question answered y.
func TestGolden(t *testing.T) {
	uitest.Golden(t, "main-menu/00-tui-main", mainCells(), func(env term.Env) string {
		return MainMenuFixture().Render(component.NewCtx(env))
	})
	uitest.Golden(t, "sub-menu/01-user", uitest.Core(), func(env term.Env) string {
		return UserFixture().Render(component.NewCtx(env))
	})
	uitest.Golden(t, "workflow-step/user-manage-sudo-confirmation", uitest.Core(), func(env term.Env) string {
		step, f := ManageSudoFixture()
		p, out := prompter(env, "y")
		if o, err := step.Run(context.Background(), p, f); o != flow.Applied || err != nil {
			t.Errorf("%s: Run() = %v, %v; want Applied, nil", env.Profile, o, err)
		}
		return strings.TrimSuffix(out.String(), "\n")
	})
}

// TestScreen checks that nav.Run, given a layout's Screen, draws what the
// layout's Render renders, in every golden cell: the drawing, then the
// prompt and the one space the line reader writes after it.
func TestScreen(t *testing.T) {
	main, user := MainMenuFixture(), UserFixture()
	tests := []struct {
		name   string
		render func(component.Ctx) string
		screen func(component.Ctx) nav.Screen
	}{
		{"main menu", main.Render, func(c component.Ctx) nav.Screen { return main.Screen(c, menuOf(main.Menu)) }},
		{"sub-menu", user.Render, func(c component.Ctx) nav.Screen { return user.Screen(c, menuOf(user.Menu)) }},
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

// lines counts the lines of a drawing.
func lines(s string) int { return strings.Count(s, "\n") + 1 }

// TestCompact checks the main menu's height budget (refactor plan §3.15):
// on a terminal shorter than theme.CompactBelow, Figma's main menu takes
// at most theme.CompactBudget lines, prompt included, at every width from
// theme.StackBelow up, where its rows sit side by side; from
// theme.CompactBelow rows, and with no terminal at any height, it takes
// its full form, which fits theme.CompactBelow rows.
func TestCompact(t *testing.T) {
	vm := MainMenuFixture()
	for w := max(theme.StackBelow, 1); w <= 130; w++ {
		for _, h := range []int{0, 1, term.DefaultHeight, theme.CompactBelow - 1} {
			env := term.Fixed(w, colorprofile.TrueColor, true, true)
			env.Height = h
			if n := lines(vm.Render(component.NewCtx(env))); n > theme.CompactBudget {
				t.Errorf("%dx%d: the compact main menu takes %d lines, over the budget of %d", w, h, n, theme.CompactBudget)
			}
		}
		full := term.Fixed(w, colorprofile.TrueColor, true, true)
		full.Height = theme.CompactBelow
		piped := term.Fixed(w, colorprofile.NoTTY, false, true)
		piped.Height = 1
		for _, env := range []term.Env{full, piped} {
			out := ansi.Strip(vm.Render(component.NewCtx(env)))
			if n := lines(out); n >= theme.CompactBelow || !strings.HasPrefix(out, "hardn") || !strings.Contains(out, "View log file") {
				t.Errorf("%dx%d, %v: want the full form in under %d lines, got %d:\n%s", w, env.Height, env.Profile, theme.CompactBelow, n, out)
			}
		}
	}
}

// TestCompactForm checks what the compact form keeps and leaves out, on
// plain text at 80x24.
func TestCompactForm(t *testing.T) {
	env := term.Fixed(80, colorprofile.ASCII, false, true)
	env.Height = term.DefaultHeight
	out := ansi.Strip(MainMenuFixture().Render(component.NewCtx(env)))
	want := `# Debian 12 (Bookworm) #########################################
  [BOS-SAN-L01]            192.0.2.10
                           182 days, 2 hours, 26 minutes

  [*] Risk Level           Moderate ~ medium security
  [+] User                 Configured non-root, sudo
  [+] Firewall             Configured deny policy
  [+] SSH Login            Configured root disallowed
  [+] SSH Auth             Configured key-only auth
  [+] SSH Port             Configured non-default (2230)
  [ ] AppArmor             Not Configured
  [ ] Auto Updates         Not Configured

  Select an option         Dry-run Mode Disabled

   1) User Management       6) Dry-Run
   2) SSH Login             7) Run All
   3) DNS                   8) Environment
   4) Firewall              9) System Details
   5) Backup               10) Logs

   0) Exit                 q to quit

  > Enter your choice [0-10, b or q]:`
	if out != want {
		t.Errorf("compact main menu:\n%s\nwant\n%s", out, want)
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
		Breadcrumb: component.Breadcrumb{Items: []string{"hardn", "Firewall"}},
		Subject:    component.SubjectLine{Label: "Managing"},
		Sections:   []Section{{Header: component.SectionHeader{Title: "Rules"}}},
		Menu:       component.Menu{Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:     "Enter your choice",
	}
	want := "hardn\n\n  hardn > Firewall\n\n   0) Return\n\n  > Enter your choice [0, b or q]:"
	if out := ansi.Strip(sub.Render(c)); out != want {
		t.Errorf("sub-menu with empty blocks:\n%s\nwant\n%s", out, want)
	}
	if out := ansi.Strip((SubMenu{}).Render(c)); out != "hardn\n\n  > [0, b or q]:" {
		t.Errorf("empty sub-menu: %q", out)
	}
	if out := (Section{Header: component.SectionHeader{Title: "Rules"}}).Render(c); out != "" {
		t.Errorf("a section with nothing under its header renders %q, want nothing", out)
	}
}

// TestRunGap checks where the workflow step's gap goes: after the screen
// before any prompt, whether the flow opens with its review or with a
// form, and before it once a prompt was asked, as when a menu's action
// opens the workflow. Each block stands one blank line from the next.
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
		{"before any prompt, the review first", sudo, false, []string{"y"}, screen + "\n\n# [*] Warning\n"},
		{"before any prompt, a form first", withForm, false, []string{"george", "y"}, screen + "\n\n  Username [0, b or q]: george\n\n# [*] Warning\n"},
		{"after a prompt", sudo, true, []string{"1", "y"}, "  > Choose [1, 0, b or q]: 1\n\n" + screen + "\n\n# [*] Warning\n"},
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
}

// failing fails every Write.
type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// TestOutputRules renders each layout at every width from 1 to 130, with
// colour, without it and in plain mode, on a 24-row terminal and a 40-row
// one, and checks the components' output rules hold for the screen: no
// line wider than the terminal, no line holding a tab, no trailing space
// outside a painted background, and no trailing newline.
func TestOutputRules(t *testing.T) {
	step, _ := ManageSudoFixture()
	layouts := map[string]func(component.Ctx) string{
		"main menu":     MainMenuFixture().Render,
		"sub-menu":      UserFixture().Render,
		"workflow step": step.Render,
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
						if lw := lipgloss.Width(l); lw > w || strings.Contains(l, "\t") {
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
