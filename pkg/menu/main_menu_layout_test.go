package menu

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The main menu on its layout (refactor plan slice 2b, unit (c),
// next/s2b-menu; docs/engine/menu-layout.md): origin/main's items on
// screen.MainMenu in its full form, each key to the screen it opened, a
// line no item takes refused under the prompt, the main menu drawn again
// under what an item printed, and the mode indicator in the maintainer's
// words. The legacy screens are recorded, not run, and the status is
// Figma's fixture or pkg/screens/mainmenu over the gallery's hosts:
// nothing on this machine is read or run.

// invalid is what the main menu says under its prompt to a line no item
// takes, origin/main's words (nav.InvalidOption) after the inline
// result's ASCII glyph, as plainEnv draws it.
const invalid = "  (x) Invalid option. Please try again."

// recordScreens records the screens m's items build in place of opening
// them (openScreen), until the test ends: each is named by the screen of
// originScreens it is, the same type built from the same values, the
// test failing on any other, then its name recorded in the list returned
// and printed, as a screen prints over the main menu. The DNS item's
// screen, m's, does the same as "dns".
func recordScreens(t *testing.T, m *MainMenu) *[]string {
	t.Helper()
	was := openScreen
	t.Cleanup(func() { openScreen = was })
	var opened []string
	record := func(name string) {
		opened = append(opened, name)
		fmt.Println(strings.ToUpper(name) + " SCREEN")
	}
	openScreen = func(s legacyScreen) {
		for name, want := range originScreens(m) {
			if sameScreen(s, want) {
				record(name)
				return
			}
		}
		t.Errorf("an item built %#v, none of origin/main's screens", s)
	}
	m.SetDNS(func() error { record("dns"); return nil })
	return &opened
}

// originScreens are the screens origin/main's handleMenuChoice built for
// m's items, by name, transcribed from it as next/s2b-mode had it
// (7347cbe, pkg/menu/main_menu.go:613-675): each key's constructor and
// its arguments, but 6's, the Dry-Run handler, which slice 2b's unit (d)
// removed with its item. The DNS item's screen is m's (SetDNS).
func originScreens(m *MainMenu) map[string]legacyScreen {
	return map[string]legacyScreen{
		"users":         NewUserMenu(m.menuManager, m.config, m.osInfo),
		"sshLogin":      NewDisableRootMenu(m.menuManager, m.config, m.osInfo),
		"firewall":      NewFirewallMenu(m.menuManager, m.config, m.osInfo),
		"backup":        NewBackupMenu(m.menuManager, m.config),
		"runAll":        handled{NewRunAllHandler(m.menuManager, m.config, m.osInfo)},
		"environment":   NewEnvironmentSettingsMenu(m.menuManager, m.config),
		"systemDetails": NewSystemDetailsMenu(m.config, m.osInfo, m.menuManager.GetHostInfoManager()),
		"logs":          NewLogsMenu(m.menuManager, m.config),
	}
}

// sameScreen reports whether a and b are the same screen built from the
// same values: of one type, a handled one's handler too, with fields that
// are the same pointers.
func sameScreen(a, b legacyScreen) bool {
	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return false
	}
	unwrap := func(s legacyScreen) reflect.Value {
		if h, ok := s.(handled); ok {
			return reflect.ValueOf(h.handler)
		}
		return reflect.ValueOf(s)
	}
	va, vb := unwrap(a), unwrap(b)
	return va.Type() == vb.Type() && va.Elem().Interface() == vb.Elem().Interface()
}

// shown counts its Show calls, as a legacy menu.
type shown int

func (s *shown) Show() { *s++ }

// handledCount counts its Handle calls, as a legacy handler.
type handledCount int

func (h *handledCount) Handle() { *h++ }

// TestOpenScreen: openScreen, which the other tests replace, opens the
// screen an item built: a menu's Show, once; a handler's Handle, once.
func TestOpenScreen(t *testing.T) {
	var m shown
	var h handledCount
	openScreen(&m)
	openScreen(handled{&h})
	if m != 1 || h != 1 {
		t.Errorf("Show called %d times and Handle %d, want once each", m, h)
	}
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

// TestMainMenuKeys is the main menu's dispatch, every key: 1 to 5 and 7
// to 10 open exactly the screen origin/main's handleMenuChoice opened for
// them, of the same constructor with the same arguments (recordScreens),
// and the main menu is drawn again when it returns; 6, whose item,
// Dry-Run, slice 2b's unit (d) removed, its key left unused (decision 1),
// is refused as any key no item has, opening nothing; 0 (Exit) and q, in
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
		{"2 opens SSH Login", []string{"2", "q"}, false, []string{"sshLogin"}, 0, 0},
		{"3 opens DNS", []string{"3", "q"}, false, []string{"dns"}, 0, 0},
		{"4 opens Firewall", []string{"4", "q"}, false, []string{"firewall"}, 0, 0},
		{"5 opens Backup", []string{"5", "q"}, false, []string{"backup"}, 0, 0},
		{"6 is refused, its item removed", []string{"6", " 6 ", "q"}, false, nil, 2, 0},
		{"7 opens Run All", []string{"7", "q"}, false, []string{"runAll"}, 0, 0},
		{"8 opens Environment", []string{"8", "q"}, false, []string{"environment"}, 0, 0},
		{"9 opens System Details", []string{"9", "q"}, false, []string{"systemDetails"}, 0, 0},
		{"10 opens Logs", []string{"10", "q"}, false, []string{"logs"}, 0, 0},
		{"every item in turn", []string{"1", "2", "3", "4", "5", "7", "8", "9", "10", "0"}, false,
			[]string{"users", "sshLogin", "dns", "firewall", "backup", "runAll", "environment", "systemDetails", "logs"}, 0, 0},
		{"every key in turn, 6 refused", []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "0"}, false,
			[]string{"users", "sshLogin", "dns", "firewall", "backup", "runAll", "environment", "systemDetails", "logs"}, 1, 0},
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
		t.Run(tc.name, func(t *testing.T) {
			passes := 0
			m := liveMenu(t, plainEnv(), fixtureStatus(&passes))
			script := strings.Join(tc.lines, "\n")
			if len(tc.lines) > 0 {
				script += "\n"
			}
			useReader(t, prompt.New(strings.NewReader(script), false))
			if tc.ctrlC {
				useReader(t, prompt.New(interrupted{strings.NewReader(script)}, false))
			}
			opened := recordScreens(t, m)
			out, code, ended := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
			if ended != (tc.code != 0) || code != tc.code {
				t.Fatalf("ended %v with %d, want %d:\n%s", ended, code, tc.code, out)
			}
			if !slices.Equal(*opened, tc.opened) {
				t.Errorf("opened %q, want %q", *opened, tc.opened)
			}
			if n := strings.Count(out, invalid+"\n"); n != tc.refused {
				t.Errorf("%d lines refused, want %d:\n%s", n, tc.refused, out)
			}
			if passes != len(tc.opened)+1 || strings.Count(out, "hardn  v0.4.0") != passes {
				t.Errorf("the main menu read %d times and drawn %d, want %d: once, then again after each screen", passes, strings.Count(out, "hardn  v0.4.0"), len(tc.opened)+1)
			}
			clears := strings.Count(out, clearScreen)
			want := 1 + map[bool]int{false: 1, true: 0}[ended] // as hardn starts, and as the main menu returns
			if slices.Contains(tc.opened, "dns") {
				want++ // the DNS item opens its screen on a cleared screen, as every legacy screen does
			}
			if !strings.HasPrefix(out, clearScreen) || clears != want || strings.Contains(out, "Press Enter") {
				t.Errorf("%d clears, want %d, the first as hardn starts; or a pause:\n%q", clears, want, out)
			}
		})
	}
}

// TestMainMenuInvalid: a line no item takes is refused under the prompt,
// the inline result's glyph and origin/main's words, "⨳ Invalid option.
// Please try again." in Unicode and "(x)" in ASCII, then the prompt asked
// again on the next line; the screen is not cleared, nothing pauses, and
// the main menu is not read or drawn again, where origin/main cleared the
// screen, printed its header and paused (main_menu.go:538-544).
func TestMainMenuInvalid(t *testing.T) {
	const prompt = "  > Enter selection [0-5, 7-10 or q]: "
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
			out, _, ended := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
			ask := strings.Replace(prompt, ">", component.NewCtx(tc.env).Theme.Glyphs.Prompt, 1)
			if ended || passes != 1 || len(*opened) != 0 || !strings.HasSuffix(out, clearScreen) ||
				!strings.HasSuffix(style.StripAnsi(strings.TrimSuffix(out, clearScreen)), ask+"\n"+tc.line+"\n"+ask+"\n") ||
				strings.Count(out, clearScreen) != 2 || strings.Contains(out, "Press Enter") {
				t.Errorf("ended %v, read %d times, opened %q; the screen:\n%q\nwant it to end %q", ended, passes, *opened, out, ask+"\n"+tc.line+"\n"+ask+"\n")
			}
		})
	}
}

// TestMainMenuAgain: after an item's screen returns, the main menu is read
// and drawn again under what the screen printed, a blank line between,
// the screen not cleared (brief 1C); the drawing is the first's.
func TestMainMenuAgain(t *testing.T) {
	passes := 0
	m := liveMenu(t, plainEnv(), fixtureStatus(&passes), "4", "q")
	recordScreens(t, m)
	out, _, ended := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
	body := strings.TrimSuffix(strings.TrimPrefix(out, clearScreen), clearScreen)
	first, second, ok := strings.Cut(body, " \nFIREWALL SCREEN\n\n")
	if ended || passes != 2 || !ok || first+" \n" != second || strings.Contains(body, clearScreen) {
		t.Errorf("ended %v, read %d times; want the main menu, the screen, a blank line and the main menu again, uncleared:\n%q", ended, passes, out)
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
			m.config = tc.start(t)
			out, _, ended := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
			if want := "  Select an option         " + tc.want + "\n"; ended || strings.Count(out, "Select an option") != 1 || !strings.Contains(out, want) {
				t.Errorf("ended %v; want the heading %q:\n%s", ended, want, out)
			}
		})
	}
}

// TestMainMenuItems: the items on the layout are origin/main's, unchanged
// in content, as the parity record holds origin/main's main menu
// (testdata/legacy/main_menu, createMainMenu's style.Menu until slice 2b's
// unit (d)): its numbers, labels and descriptions, item 1's parenthesis
// its aside, and its Exit row, but item 6, Dry-Run, removed with its key
// (decision 1); and the same as the main-menu mock's
// (screen.MainMenuFixture), which the gallery draws its main menus with.
// The heading and prompt are origin/main's too, the hint generated from
// the items, "[0-5, 7-10 or q]" where origin/main's was "[0-10 or q]".
func TestMainMenuItems(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(legacyDir, "main_menu", "ubuntu-fresh.golden"))
	if err != nil {
		t.Fatal(err)
	}
	_, record, _ := strings.Cut(string(data), "# ----\n")
	record = regexp.MustCompile(`\\x1b\[[0-9;]*m`).ReplaceAllString(record, "")
	row := regexp.MustCompile(`^\s*(\d+)\) (\S.*?)\s{2,}(.*?)\s*\$$`)
	var legacy [][]string
	for _, l := range strings.Split(record, "\n") {
		if r := row.FindStringSubmatch(l); r != nil && r[1] != "6" {
			legacy = append(legacy, r[1:])
		}
	}
	m := NewMainMenu(nil, config.DefaultConfig(), nil, nil)
	view := m.mainMenu().View()
	var layout [][]string
	for _, it := range append(slices.Clone(view.Items), view.Exit) {
		desc := it.Description
		if it.Aside != "" {
			desc += " (" + it.Aside + ")"
		}
		layout = append(layout, []string{it.Key, it.Label, desc})
	}
	if len(legacy) != 10 || !slices.EqualFunc(legacy, layout, slices.Equal[[]string]) {
		t.Errorf("the layout's items\n%q\nare not origin/main's but 6\n%q", layout, legacy)
	}
	fixture := screen.MainMenuFixture()
	if !slices.Equal(view.Items, fixture.Menu.Items) || view.Exit != fixture.Menu.Exit || fixture.Heading != mainHeading || fixture.Prompt != mainPrompt {
		t.Errorf("the items are not the main-menu mock's:\n%+v\n%+v", view, fixture.Menu)
	}
	if !strings.Contains(record, mainHeading) || !strings.Contains(record, mainPrompt+" [0-10 or q]:") ||
		m.mainMenu().Accepts().Hint() != "0-5, 7-10 or q" {
		t.Errorf("the heading, prompt or hint differ from origin/main's, or the hint is %q:\n%s", m.mainMenu().Accepts().Hint(), record)
	}
}

// TestMainMenuHeights pins hardn's main menu's heights, one pass drawn on
// a terminal over the gallery's hardened Debian 13 host (eight rows, no
// line under any), in its full form at every height, Unicode (C.UTF-8)
// and ASCII (C): 35 lines at 80x24 and 80x40, 57 at 60 columns, where the
// rows stack (DEV-021), one line less, and two at 60 columns, than with
// item 6, which slice 2b's unit (d) removed. The prompt is the terminal's
// last line, so on 24 rows the first 11 lines scroll off at 80 columns,
// the header down to the Firewall row, the risk level among them, and at
// 60 columns 33, all the status, the heading and the mode; on 40 rows none
// at 80 columns and 17 at 60.
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
		{80, 24, 35, "SSH Login            Configured root disallowed"},
		{80, 40, 35, "hardn  v0.4.0              https://github.com/abbott/hardn"},
		{60, 24, 57, ""},
		{60, 40, 57, "Configured root disallowed"},
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
				return mainmenu.Status(ctx, mainmenu.Options{System: sys, Detect: detect.Detect, Config: cfg, Version: "v0.4.0"})
			}, "q")
			out, _, _ := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
			drawn, _, _ := strings.Cut(strings.TrimPrefix(out, clearScreen), clearScreen)
			ls := strings.Split(style.StripAnsi(strings.TrimSuffix(drawn, " \n")), "\n")
			first := strings.TrimLeft(ls[max(0, len(ls)-tc.h)], " ⛛[+]")
			risk := slices.IndexFunc(ls, func(l string) bool { return strings.Contains(l, "Risk Level") })
			if len(ls) != tc.lines || first != tc.visible || (risk >= len(ls)-tc.h) != (len(ls) <= tc.h) ||
				!strings.HasPrefix(ls[len(ls)-1], "  "+component.NewCtx(env).Theme.Glyphs.Prompt+" Enter selection") {
				t.Errorf("%dx%d, %s: %d lines, the first on screen %q, the risk level on line %d; want %d, %q:\n%s",
					tc.w, tc.h, lang, len(ls), first, risk+1, tc.lines, tc.visible, strings.Join(ls, "\n"))
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
	m := liveMenu(t, env, fixtureStatus(new(int)), "q")
	out, _, ended := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
	vm := screen.MainMenuFixture()
	vm.Mode = component.ModeIndicator{}
	want := clearScreen + written(env, vm.Render(component.NewCtx(env))) + " \n" + clearScreen
	if ended || out != want {
		t.Errorf("ended %v; the screen:\n%s\nwant the main-menu mock's, Figma's fixture with origin/main's items, in full:\n%s", ended, out, want)
	}
}
