package mainmenu

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The main menu on its layout (refactor plan slice 2b, unit (c),
// next/s2b-menu; docs/engine/menu-layout.md): origin/main's items on
// screen.MainMenu in its full form, each key to the screen it opened, a
// line no item takes refused under the prompt, the main menu drawn again
// on a cleared screen as an item's screen returns (under it, off a
// terminal), and the mode indicator in the maintainer's
// words. The legacy screens are recorded, not run, and the status is
// Figma's fixture or pkg/screens/mainmenu over the gallery's hosts:
// nothing on this machine is read or run.

// invalid is what the main menu says under its prompt to a line no item
// takes, origin/main's words (nav.InvalidOption) after the inline
// result's ASCII glyph, as plainEnv draws it.
const invalid = "  (x) Invalid option. Please try again."

// recordScreens records the screens m's items open in place of running
// them, until the test ends: each item's screen, which cmd/hardn sets,
// records its name in the list returned and prints it, as a screen prints
// over the main menu: "users", "ssh", "dns", "firewall", "backup",
// "updates", "runAll", "environment", "systemDetails" and "logs".
func recordScreens(t *testing.T, m *Options) *[]string {
	t.Helper()
	var opened []string
	record := func(name string) {
		opened = append(opened, name)
		fmt.Println(strings.ToUpper(name) + " SCREEN")
	}
	m.Users = func() error { record("users"); return nil }
	m.SSH = func() error { record("ssh"); return nil }
	m.DNS = func() error { record("dns"); return nil }
	m.Firewall, m.FirewallPVE = func() error { record("firewall"); return nil }, nil
	m.Backup = func() error { record("backup"); return nil }
	m.Updates = func() error { record("updates"); return nil }
	m.RunAll = func() error { record("runAll"); return nil }
	m.Environment = func() error { record("environment"); return nil }
	m.SystemDetails = func() error { record("systemDetails"); return nil }
	m.Logs = func() error { record("logs"); return nil }
	return &opened
}

// interrupted reads its lines, then reports Ctrl-C as the line reader
// does when the user presses it at a prompt (prompt.ErrInterrupted).
type interrupted struct{ io.Reader }

func (r interrupted) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		err = prompt.ErrInterrupted
	}
	return n, err
}

// TestMainMenuKeys is the main menu's dispatch, every key: 1 to 10 open
// the User Management, SSH Login, DNS, Firewall, Backup, Auto Updates, Run
// All, Environment, System Details and Logs screens cmd/hardn sets
// (recordScreens), 6 where origin/main's Dry-Run was (slice 7, unit (d)),
// 7 the Run All screen on the planner (slice 8, unit (d)), and the main
// menu is drawn again when it returns; 0 (Exit) and q, in
// either case, clear the screen and return, opening nothing; spaces
// around a key are ignored, as origin/main's read trimmed them; an empty
// line and a key no item has are refused under the prompt, "Invalid
// option. Please try again.", the main menu not drawn again and no screen
// opened; the end of the input exits 130, and Ctrl-C at the prompt ends
// hardn by SIGINT.
func TestMainMenuKeys(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lines   []string // typed, then the end of the input
		ctrlC   bool     // Ctrl-C at the prompt after the lines, in place of the end of the input
		opened  []string // the screens opened, in order
		refused int      // the lines refused
		code    int      // the exit status hardn ended with; 0: the main menu returned
	}{
		{"1 opens User Management", []string{"1", "q"}, false, []string{"users"}, 0, 0},
		{"2 opens SSH Login", []string{"2", "q"}, false, []string{"ssh"}, 0, 0},
		{"3 opens Firewall", []string{"3", "q"}, false, []string{"firewall"}, 0, 0},
		{"4 opens DNS", []string{"4", "q"}, false, []string{"dns"}, 0, 0},
		{"5 opens Auto Updates", []string{"5", "q"}, false, []string{"updates"}, 0, 0},
		{"6 opens Run All", []string{"6", "q"}, false, []string{"runAll"}, 0, 0},
		{"7 opens Backup", []string{"7", "q"}, false, []string{"backup"}, 0, 0},
		{"8 opens Environment", []string{"8", "q"}, false, []string{"environment"}, 0, 0},
		{"9 opens System Details", []string{"9", "q"}, false, []string{"systemDetails"}, 0, 0},
		{"10 opens Logs", []string{"10", "q"}, false, []string{"logs"}, 0, 0},
		{"every item in turn", []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "0"}, false,
			[]string{"users", "ssh", "firewall", "dns", "updates", "runAll", "backup", "environment", "systemDetails", "logs"}, 0, 0},
		{"0 exits", []string{"0"}, false, nil, 0, 0},
		{"q quits", []string{"q"}, false, nil, 0, 0},
		{"Q quits", []string{"Q"}, false, nil, 0, 0},
		{"spaces around a key", []string{" 10 ", " q "}, false, []string{"logs"}, 0, 0},
		{"an empty line is refused", []string{"", "q"}, false, nil, 1, 0},
		{"keys no item has are refused", []string{"11", "01", "x", "b", "-1", "1 0", "100", "q"}, false, nil, 7, 0},
		{"the end of the input", nil, false, nil, 0, prompt.ExitInterrupted},
		{"the end of the input after a refusal", []string{"x"}, false, nil, 1, prompt.ExitInterrupted},
		{"Ctrl-C at the prompt", []string{"x"}, true, nil, 1, int(sigint)},
	} {
		for _, env := range []term.Env{ttyEnv(), plainEnv()} {
			t.Run(tc.name+map[bool]string{false: ", on a terminal", true: ", off one"}[env.Plain], func(t *testing.T) {
				passes := 0
				m := liveMenu(t, env, fixtureStatus(&passes))
				script := strings.Join(tc.lines, "\n")
				if len(tc.lines) > 0 {
					script += "\n"
				}
				useReader(t, prompt.New(strings.NewReader(script), false))
				if tc.ctrlC {
					useReader(t, prompt.New(interrupted{strings.NewReader(script)}, false))
				}
				opened := recordScreens(t, m)
				out, code, ended := session(t, func() { show(m) })
				if ended != (tc.code != 0) || code != tc.code {
					t.Fatalf("ended %v with %d, want %d:\n%s", ended, code, tc.code, out)
				}
				if !slices.Equal(*opened, tc.opened) {
					t.Errorf("opened %q, want %q", *opened, tc.opened)
				}
				text := stripANSI(out) // the bold a terminal keeps without colour aside
				if n := strings.Count(text, invalid+"\n"); n != tc.refused {
					t.Errorf("%d lines refused, want %d:\n%s", n, tc.refused, out)
				}
				if passes != len(tc.opened)+1 || strings.Count(text, "hardn  v0.4.0") != passes {
					t.Errorf("the main menu read %d times and drawn %d, want %d: once, then again after each screen", passes, strings.Count(text, "hardn  v0.4.0"), len(tc.opened)+1)
				}
				clears := strings.Count(out, clearScreen)
				want := 1 + map[bool]int{false: 1, true: 0}[ended] // as hardn starts, and as the main menu returns
				// every item's screen opens on a cleared screen, and the screen is
				// cleared again as it returns (show)
				want += 2 * len(tc.opened)
				if env.Plain {
					want = 0 // off a terminal nothing clears
				}
				top := clearTopOn(env) // two blank lines under the first clear (clearTop)
				if !strings.HasPrefix(out, top+"\x1b[1mhardn") && !strings.HasPrefix(out, top+"hardn") || clears != want || strings.Contains(out, "Press Enter") {
					t.Errorf("%d clears, want %d, the first as hardn starts, two blank lines under it; or a pause:\n%q", clears, want, out)
				}
			})
		}
	}
}

// TestMainMenuInvalid: a line no item takes is refused under the prompt,
// the inline result's glyph and origin/main's words, "⨳ Invalid option.
// Please try again." in Unicode and "(x)" in ASCII, then the prompt asked
// again on the next line; the screen is not cleared, nothing pauses, and
// the main menu is not read or drawn again, where origin/main cleared the
// screen, printed its header and paused (main_menu.go:538-544).
func TestMainMenuInvalid(t *testing.T) {
	const prompt = "    > Enter selection [0-10 or q]: "
	for _, tc := range []struct {
		name string
		env  term.Env
		line string
	}{
		{"ASCII", plainEnv(), invalid},
		{"Unicode", term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80,
			Environ: []string{"TERM=xterm-256color", "LANG=C.UTF-8", "NO_COLOR=1"}}), "   ⨳  Invalid option. Please try again."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			passes := 0
			m := liveMenu(t, tc.env, fixtureStatus(&passes), "x", "q")
			opened := recordScreens(t, m)
			out, _, ended := session(t, func() { show(m) })
			ask := strings.Replace(prompt, ">", component.NewCtx(tc.env).Theme.Glyphs.Prompt, 1)
			clr := clearOn(tc.env) // on a terminal as it starts and as it returns; off one none
			noClearOff(t, tc.env, out)
			if ended || passes != 1 || len(*opened) != 0 || !strings.HasSuffix(out, clearTopOn(tc.env)) ||
				!strings.HasSuffix(stripANSI(strings.TrimSuffix(out, clearTopOn(tc.env))), ask+"\n"+tc.line+"\n"+ask+"\n") ||
				clr != "" && strings.Count(out, clr) != 2 || strings.Contains(out, "Press Enter") {
				t.Errorf("ended %v, read %d times, opened %q; the screen:\n%q\nwant it to end %q", ended, passes, *opened, out, ask+"\n"+tc.line+"\n"+ask+"\n")
			}
		})
	}
}

// TestMainMenuAgain: an item's screen opens on a cleared screen, and as it
// returns the screen is cleared again (show), so that the main menu is read
// and drawn again on a cleared screen, not under the item's; two blank
// lines at its top, one of them nav's, as on the first drawing (clearTop),
// and the drawing is the first's. Off a terminal nothing clears and the
// main menu is drawn under the screen, nav's blank line between.
func TestMainMenuAgain(t *testing.T) {
	for _, env := range []term.Env{ttyEnv(), plainEnv()} {
		passes := 0
		m := liveMenu(t, env, fixtureStatus(&passes), "6", "q")
		recordScreens(t, m)
		out, _, ended := session(t, func() { show(m) })
		clr := clearOn(env)
		noClearOff(t, env, out)
		body := strings.TrimSuffix(strings.TrimPrefix(out, clr), clearTopOn(env))
		first, second, ok := strings.Cut(body, " \n"+clearGapOn(env)+"RUNALL SCREEN\n"+clearGapOn(env)+"\n")
		if !env.Plain {
			first = strings.TrimPrefix(first, strings.Repeat("\n", topRows)) // the blank lines under the first clear (clearTop)
		}
		if ended || passes != 2 || !ok || first+" \n" != second || clr != "" && strings.Contains(second, clr) {
			t.Errorf("ended %v, read %d times; want the main menu, the screen cleared on a terminal, the item's screen, the screen cleared again, a blank line and the main menu again:\n%q", ended, passes, out)
		}
	}
}

// TestMainMenuMode: the mode indicator beside "Select an option" on every
// pass, in the maintainer's words, from the configuration the legacy
// screens read: "Mode Live"; "Mode Plan only (-n)" from the flag;
// "Mode Plan only (hardn.yml)" from the file's dryRun; live again with
// --dry-run=false over the file. Plan only with no source, which item 6
// (Dry-Run) and Run All's option 2 set inside the session until slice
// 2b's unit (d) removed them, is still drawn "Mode Plan only" for a
// DryRun written after the start, which nothing in hardn now does.
func TestMainMenuMode(t *testing.T) {
	file := func(t *testing.T) *config.Config {
		f, _ := loadStatusHost(t, "debian13")
		f.AddFile(hardnYML, []byte("dryRun: true\n"), 0o644, 0, 0)
		c, err := config.Read(context.Background(), f, hardnYML)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	for _, tc := range []struct {
		name  string
		start func(t *testing.T) *config.Config
		want  string
	}{
		{"live", func(*testing.T) *config.Config { return config.DefaultConfig() }, "Mode Live"},
		{"-n", func(*testing.T) *config.Config { c := config.DefaultConfig(); c.ResolveDryRun(true, true); return c }, "Mode Plan only (-n)"},
		{"hardn.yml", func(t *testing.T) *config.Config { c := file(t); c.ResolveDryRun(false, false); return c }, "Mode Plan only (hardn.yml)"},
		{"--dry-run=false over hardn.yml", func(t *testing.T) *config.Config { c := file(t); c.ResolveDryRun(true, false); return c }, "Mode Live"},
		{"DryRun written after the start", func(*testing.T) *config.Config {
			c := config.DefaultConfig()
			c.ResolveDryRun(false, false)
			c.DryRun = true
			return c
		}, "Mode Plan only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := liveMenu(t, plainEnv(), fixtureStatus(new(int)), "q")
			m.Config = tc.start(t)
			out, _, ended := session(t, func() { show(m) })
			if want := "  Select an option         " + tc.want + "\n"; ended || strings.Count(out, "Select an option") != 1 || !strings.Contains(out, want) {
				t.Errorf("ended %v; want the heading %q:\n%s", ended, want, out)
			}
		})
	}
}

// TestMainMenuItems: the items on the layout are the main-menu mock's
// (screen.MainMenuFixture), which the gallery draws its main menus with,
// with item 5, Auto Updates, "Configure automatic updates", between 4 and
// 6 (withUpdatesItem): its numbers, labels and descriptions, item 1's
// aside, item 2's "Root login, passwords, port" (legacy-left.md §3,
// decision 7 (a)), item 8's "Keep HARDN_CONFIG", and its Exit row, "q to
// quit" (DEV-030). The heading and prompt are origin/main's, the hint
// generated from the items, "[0-10 or q]".
func TestMainMenuItems(t *testing.T) {
	m := &Options{Config: config.DefaultConfig()}
	view := m.items().View()
	if view.Items[1] != (component.MenuItem{Key: "2", Label: "SSH Login", Description: "Root login, passwords, port"}) {
		t.Errorf("item 2 is %+v", view.Items[1])
	}
	fixture := screen.MainMenuFixture()
	if !slices.Equal(view.Items, withUpdatesItem(fixture.Menu.Items)) || view.Exit != fixture.Menu.Exit || fixture.Heading != mainHeading || fixture.Prompt != mainPrompt {
		t.Errorf("the items are not the main-menu mock's:\n%+v\n%+v", view, fixture.Menu)
	}
	if hint := m.items().Accepts().Hint(); hint != "0-10 or q" {
		t.Errorf("the hint is %q", hint)
	}
}

// updatesItem is item 5, Auto Updates (gallery-updates.md §4, decision 1).
var updatesItem = component.MenuItem{Key: "5", Label: "Auto Updates", Description: "Configure automatic updates"}

// withUpdatesItem is the main-menu mock's items (screen.MainMenuFixture)
// with item 5 between 4 and 6, as the main menu has them since slice 7's
// unit (d): the mock, pkg/ui/screen's, has no Auto Updates item yet.
func withUpdatesItem(items []component.MenuItem) []component.MenuItem {
	i := slices.IndexFunc(items, func(it component.MenuItem) bool { return it.Key == "6" })
	return slices.Insert(slices.Clone(items), i, updatesItem)
}

// TestMainMenuTopRows: every clear on a terminal leaves topRows blank
// lines above what follows it, nav.Prompter.Run's gap counted: the main
// menu's first drawing, with no prompt answered yet and after hardn.yml's
// first-run question (a prompt answered before the main menu); an item's
// screen, drawn by nav as every item's is; the main menu again; and, as
// hardn ends, the shell's prompt.
func TestMainMenuTopRows(t *testing.T) {
	for _, asked := range []bool{false, true} {
		env := term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: 40,
			Environ: []string{"TERM=xterm-256color", "NO_COLOR=1"}})
		m := liveMenu(t, env, fixtureStatus(new(int)), "6", "0", "q")
		recordScreens(t, m)
		m.RunAll = func() error {
			p := nav.Prompter{Ctx: component.NewCtx(env), In: m.Input(), Out: env.Writer(stdout{})}
			return p.Run(func() nav.Screen {
				return nav.Screen{Above: "RUN ALL", Label: "Enter your choice", Menu: nav.Menu{Exit: component.MenuItem{Key: "0", Label: "Return"}}}
			})
		}
		if asked {
			r := prompt.New(strings.NewReader("y\n6\n0\nq\n"), false)
			useReader(t, r)
			if _, err := r.Ask(io.Discard, "Create hardn.yml? "); err != nil {
				t.Fatal(err)
			}
		}
		out, _, ended := session(t, func() { show(m) })
		parts := strings.Split(out, clearOn(env))
		if ended || len(parts) != 5 || parts[0] != "" {
			t.Fatalf("asked %v: ended %v, %d clears, want 4, the first at once:\n%q", asked, ended, len(parts)-1, out)
		}
		top := strings.Repeat("\n", topRows)
		for i, next := range []string{"hardn ", "RUN ALL", "hardn ", ""} {
			if got := stripANSI(parts[i+1]); got != top+next && !strings.HasPrefix(got, top+next) || next == "" && got != top ||
				next != "" && strings.HasPrefix(got, top+"\n") {
				t.Errorf("asked %v, after clear %d: %q; want %d blank lines, then %q", asked, i+1, got[:min(len(got), 40)], topRows, next)
			}
		}
	}
}

// TestMainMenuHeights pins hardn's main menu's heights, one pass drawn on
// a terminal over the gallery's hardened Debian 13 host (eight rows, no
// line under any), in its full form at every height, Unicode (C.UTF-8)
// and ASCII (C): 38 lines at 80x24 and 80x40, the two blank lines under
// the clear (clearTop) first, 61 at 60 columns, where the rows stack
// (DEV-021), item 5, Auto Updates, one line, and two at 60 columns (slice
// 7, unit (d)). The prompt is the terminal's last line, so on 24 rows the
// first 14 lines scroll off at 80 columns, the blank lines and the header
// down to the SSH Auth row, the risk level among them, and at 60 columns
// 37, all the status, the heading, the mode and item 1's label; on 40 rows
// none at 80 columns, the blank lines on the top two rows and the header
// under them, and 21 at 60.
// With screen.CompactMainMenu flipped it skips: pkg/ui/screen's
// TestHeights and TestCompact hold the forms then.
func TestMainMenuHeights(t *testing.T) {
	if screen.CompactMainMenu {
		t.Skip("screen.CompactMainMenu is set: pkg/ui/screen's TestHeights and TestCompact hold the forms")
	}
	for _, tc := range []struct {
		w, h, lines int
		visible     string // the first line on screen, stripped of colour and glyphs
	}{
		{80, 24, 38, "SSH Port             Configured non-default (2230)"},
		{80, 40, 38, ""}, // the blank lines under the clear, the header under them
		{60, 24, 61, "Create, Configure (sudo, SSH keys)"},
		{60, 40, 61, "Configured non-default (2230)"},
	} {
		for _, lang := range []string{"C.UTF-8", "C"} {
			_, sys := loadStatusHost(t, "hardened")
			cfg, err := config.Read(context.Background(), sys, hardnYML)
			if err != nil {
				t.Fatal(err)
			}
			env := term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: tc.w, Height: tc.h,
				Environ: []string{"TERM=xterm-256color", "LANG=" + lang}})
			m := liveMenu(t, env, func(ctx context.Context) (screen.MainMenu, error) {
				return Status(ctx, StatusOptions{System: sys, Detect: detect.Detect, Config: cfg, Version: "v0.4.0"})
			}, "q")
			out, _, _ := session(t, func() { show(m) })
			drawn, _, _ := strings.Cut(strings.TrimPrefix(out, clearOn(env)), clearOn(env))
			ls := strings.Split(stripANSI(strings.TrimSuffix(drawn, " \n")), "\n")
			first := strings.TrimLeft(ls[max(0, len(ls)-tc.h)], " ⛛[+]")
			risk := slices.IndexFunc(ls, func(l string) bool { return strings.Contains(l, "Risk Level") })
			if len(ls) != tc.lines || first != tc.visible || ls[0] != "" || ls[1] != "" || (risk >= len(ls)-tc.h) != (len(ls) <= tc.h) ||
				!strings.HasPrefix(ls[len(ls)-1], strings.Repeat(" ", theme.PromptCol)+component.NewCtx(env).Theme.Glyphs.Prompt+" Enter selection") {
				t.Errorf("%dx%d, %s: %d lines, the first on screen %q, the risk level on line %d; want %d, %q:\n%s",
					tc.w, tc.h, lang, len(ls), first, risk+1, tc.lines, tc.visible, strings.Join(ls, "\n"))
			}
		}
	}
}

// TestMainMenuKeepsLoadWarning: a warning at load stays above the first draw; without
// one the screen is cleared first, on a terminal; off one nothing clears.
func TestMainMenuKeepsLoadWarning(t *testing.T) {
	for _, env := range []term.Env{ttyEnv(), plainEnv()} {
		for _, warned := range []bool{false, true} {
			m := liveMenu(t, env, fixtureStatus(new(int)), "q")
			m.Warned = warned
			out, _, ended := session(t, func() { show(m) })
			if ended {
				t.Fatalf("ended hardn:\n%s", out)
			}
			noClearOff(t, env, out)
			if env.Plain {
				continue
			}
			if got := strings.HasPrefix(out, clearScreen); got == warned {
				t.Errorf("warned %v: cleared first %v:\n%q", warned, got, out)
			}
			if strings.Count(out, clearScreen) != map[bool]int{false: 2, true: 1}[warned] {
				t.Errorf("warned %v: %d clears, the one on leaving aside:\n%q", warned, strings.Count(out, clearScreen), out)
			}
		}
	}
}

// TestMainMenuDraw: the main menu hardn draws is exactly the layout's
// Render for the view-model it fills, on a terminal of 24 rows too, in
// colour and Unicode: the full form, one column of items, nothing of
// pkg/style's menu; and on the same pass, the hook's view-model is the
// one drawn, its heading, mode, items and prompt filled by the main menu.
func TestMainMenuDraw(t *testing.T) {
	env := term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: 24,
		Environ: []string{"TERM=xterm-256color", "LANG=C.UTF-8"}})
	// The clear itself, as written, not as env.Clear computes it: on a terminal of 24
	// measured rows, 23 newlines scroll the screen away, then the cursor goes home
	// (next/fix-clear-gaps), with no ESC[2J.
	if got, lit := clearOn(env), strings.Repeat("\n", 23)+"\x1b[H"; got != lit {
		t.Fatalf("the clear on a terminal of 24 measured rows: %q, want %q", got, lit)
	}
	m := liveMenu(t, env, fixtureStatus(new(int)), "q")
	out, _, ended := session(t, func() { show(m) })
	vm := screen.MainMenuFixture()
	vm.Mode = component.ModeIndicator{}
	vm.Menu.Items = withUpdatesItem(vm.Menu.Items)
	want := clearTopOn(env) + written(env, vm.Render(component.NewCtx(env))) + " \n" + clearTopOn(env) // two blank lines under each clear (clearTop)
	if ended || out != want {
		t.Errorf("ended %v; the screen:\n%s\nwant the main-menu mock's, Figma's fixture with origin/main's items, in full:\n%s", ended, out, want)
	}
}
