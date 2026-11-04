package menu

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The main menu live (refactor plan slice 2, next/s2-menu-live): its
// status read through the hook cmd/hardn sets (SetStatus), and drawn
// with the rest of the main menu on its layout (slice 2b, next/s2b-menu).
// Nothing on this machine is read or run: the hook is a fake, or
// pkg/screens/mainmenu over hostfake.

// clearScreen is what a clear writes on a terminal (term.Env.Clear).
const clearScreen = term.ClearSequence

// ttyEnv is a terminal 80 columns wide, without colour (NO_COLOR): the
// main menu clears on it.
func ttyEnv() term.Env {
	return term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80,
		Environ: []string{"TERM=xterm-256color", "NO_COLOR=1"}})
}

// clearOn is what a clear writes on env: the clear on a terminal, nothing
// off one.
func clearOn(env term.Env) string {
	if env.Plain {
		return ""
	}
	return clearScreen
}

// noClearOff reports a clear written off a terminal, where none is.
func noClearOff(t *testing.T, env term.Env, out string) {
	t.Helper()
	if env.Plain && strings.Contains(out, clearScreen) {
		t.Errorf("cleared off a terminal:\n%q", out)
	}
}

// liveMenu is a main menu as cmd/hardn builds it, its status read by read
// and drawn on terminal env; the end of the input after lines.
func liveMenu(t *testing.T, env term.Env, read func(ctx context.Context) (screen.MainMenu, error), lines ...string) *MainMenu {
	t.Helper()
	scripted(t, strings.Join(append(lines, ""), "\n"))
	m := NewMainMenu(config.DefaultConfig())
	if read != nil {
		m.SetStatus(read)
	}
	m.SetTerminal(func() term.Env { return env })
	return m
}

// plainEnv is output that is not a terminal, 80 columns wide: plain text,
// ASCII glyphs.
func plainEnv() term.Env { return term.Resolve(term.Options{}, term.Facts{Width: 80}) }

// fixtureStatus reads Figma's fixture as the status, the blocks
// pkg/screens/mainmenu fills and nothing else, counting its calls in n.
func fixtureStatus(n *int) func(context.Context) (screen.MainMenu, error) {
	return func(context.Context) (screen.MainMenu, error) {
		*n++
		f := screen.MainMenuFixture()
		return screen.MainMenu{Header: f.Header, Context: f.Context, Host: f.Host, Risk: f.Risk, Status: f.Status}, nil
	}
}

// TestLiveStatus: each pass of the main menu reads the status through
// the hook, with a context that is not done, and draws the whole main
// menu on its layout, exactly what screen.MainMenu renders for the
// status, "Select an option", the mode, origin/main's items and "Enter
// selection", in its full form, on the terminal set; the screen is
// cleared once, as hardn starts, and once as it ends; no legacy line.
func TestLiveStatus(t *testing.T) {
	n := 0
	read := fixtureStatus(&n)
	m := liveMenu(t, plainEnv(), func(ctx context.Context) (screen.MainMenu, error) {
		if ctx.Err() != nil {
			t.Error("the hook's context is done")
		}
		return read(ctx)
	}, "q")
	out, _, ended := session(t, func() { m.ShowMainMenu() })
	vm, _ := fixtureStatus(new(int))(context.Background())
	vm.Heading, vm.Mode, vm.Prompt, vm.Menu = mainHeading, m.mode(), mainPrompt, m.mainMenu().View()
	want := written(plainEnv(), vm.Render(component.NewCtx(plainEnv()))) + " \n" // off a terminal, no clear
	if ended || out != want || n != 1 {
		t.Errorf("ended %v, the hook called %d times; the screen:\n%q\nwant:\n%q", ended, n, out, want)
	}
	for _, legacy := range []string{"Sudo Method", "Dry-run Mode", "Press Enter"} {
		if strings.Contains(out, legacy) {
			t.Errorf("%q drawn: the legacy block and menu are gone", legacy)
		}
	}
}

// written is s as written through env's writer.
func written(env term.Env, s string) string {
	var b strings.Builder
	_, _ = io.WriteString(env.Writer(&b), s)
	return b.String()
}

// TestLiveStatusEnds: a hook that returns a cancelled context's error
// ends hardn as Ctrl-C at the menu's prompt does, by SIGINT; another
// error is printed and ends hardn with 1; the main menu is not drawn. A
// main menu with no hook set, or no terminal, panics before it draws
// anything.
func TestLiveStatusEnds(t *testing.T) {
	failed := errors.New("main menu: engine: write: broken pipe")
	for _, tc := range []struct {
		name string
		err  error
		code int
	}{
		{"ctrl-c", fmt.Errorf("main menu: engine: detect: %w", context.Canceled), int(sigint)},
		{"another error", failed, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := liveMenu(t, plainEnv(), func(context.Context) (screen.MainMenu, error) { return screen.MainMenu{}, tc.err }, "q")
			out, code, ended := session(t, func() { m.ShowMainMenu() })
			if !ended || code != tc.code || strings.Contains(out, "Select an option") {
				t.Errorf("ended %v with %d, want %d; the screen:\n%q", ended, code, tc.code, out)
			}
			if tc.code == 1 && !strings.Contains(out, failed.Error()) {
				t.Errorf("the error is not printed: %q", out)
			}
		})
	}
	for _, tc := range []struct {
		name     string
		status   bool
		terminal bool
	}{{"no hook", false, true}, {"no terminal", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			var read func(context.Context) (screen.MainMenu, error)
			if tc.status {
				read = fixtureStatus(new(int))
			}
			m := liveMenu(t, plainEnv(), read, "q")
			if !tc.terminal {
				m.terminal = nil
			}
			out, _, ended := session(t, func() {
				defer func() {
					if recover() == nil {
						t.Error("the main menu drew with nothing to draw it with")
					}
				}()
				m.ShowMainMenu()
			})
			if ended || out != "" {
				t.Errorf("ended %v, printed %q", ended, out)
			}
		})
	}
}

// The status hosts: five of the gallery's (internal/tools/uigallery,
// statusHosts), as internal/tools/p4status's fixtures hold them, exported
// from the gallery's model (its testdata/README.md), and the gallery's
// own drawing of each in its golden file, plain at 80 columns.
const (
	fixtureDir = "../../internal/tools/p4status/testdata/hosts"
	goldenDir  = "../../internal/tools/uigallery/testdata/golden/status"
	hardnYML   = "/etc/hardn/hardn.yml"
)

// statusRuns are the commands each host's status runs, per pass of the
// main menu: detect's, the eight rows' checks' and the header probe's.
var statusRuns = map[string]int{"debian13": 29, "ubuntu2604": 32, "alpine324": 13, "pve9": 15, "hardened": 29}

// queries are the commands a status may run, by their first words: each
// only reads (docs/engine/vmcheck.md §2.5 has each one's arguments).
var queries = []string{"sshd -T", "/usr/sbin/sshd -T", "systemctl is-active", "systemctl is-enabled", "systemctl show",
	"ss -Htln", "ufw status", "apt-config dump", "dpkg-query --show", "sudo --version", "visudo -c", "doas -C",
	"ip -4", "apk info", "rc-service crond", "resolvectl --no-pager"}

// TestLiveStatusOnHosts: the main menu with cmd/hardn's status
// (pkg/screens/mainmenu over the host, hardn.yml loaded from it as hardn
// loads it) on Debian 13, Ubuntu 26.04, Alpine 3.24, Proxmox VE 9 and
// Debian 13 hardened: the whole main menu, its header, host block, risk
// level and rows, the DNS row included, its heading, "Mode Live",
// items and prompt, is line for line the gallery's status host's main
// menu down to its prompt; the status only reads, its commands every one
// a query the host answers, as many on every pass (statusRuns), and
// nothing is written.
func TestLiveStatusOnHosts(t *testing.T) {
	ctx := context.Background()
	for name, runs := range statusRuns {
		t.Run(name, func(t *testing.T) {
			f, sys := loadStatusHost(t, name)
			cfg, err := config.Read(ctx, sys, hardnYML)
			if errors.Is(err, fs.ErrNotExist) {
				cfg, err = config.DefaultConfig(), nil
			}
			if err != nil {
				t.Fatal(err)
			}
			before := len(f.Calls())
			m := liveMenu(t, plainEnv(), func(ctx context.Context) (screen.MainMenu, error) {
				return mainmenu.Status(ctx, mainmenu.Options{System: sys, Detect: detect.Detect, Config: cfg, Version: "v0.4.0"})
			}, "q")
			out, _, ended := session(t, func() { m.ShowMainMenu() })
			want := galleryStatus(t, name) + " \n" // off a terminal, no clear
			if ended || out != want {
				t.Errorf("ended %v; the main menu:\n%s\nwant the gallery's status:\n%s", ended, out, want)
			}
			n := 0
			for _, c := range f.Calls()[before:] {
				switch {
				case c.Op == "run":
					n++
					if errors.Is(c.Err, hostfake.ErrUnscripted) || !slices.ContainsFunc(queries, func(q string) bool { return strings.HasPrefix(strings.Join(c.Argv, " "), q) }) {
						t.Errorf("ran %q (%v), which is not a query the host answers", c.Argv, c.Err)
					}
				case !slices.Contains([]string{"read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group"}, c.Op):
					t.Errorf("a call that does not only read: %v", c)
				}
			}
			if n != runs {
				t.Errorf("%d commands on one pass, want %d", n, runs)
			}
		})
	}
}

// TestLiveMainMenuGolden records hardn's live main menu, one pass before
// input, on the gallery's Debian 13 and hardened hosts, on a 256-colour
// UTF-8 terminal of 80 columns, at 24 and 40 rows: the main menu on its
// layout in its full form, the same at both heights, its compact form
// gated (screen.CompactMainMenu; docs/engine/menu-layout.md).
func TestLiveMainMenuGolden(t *testing.T) {
	prevEastAsian, prevColors := runewidth.DefaultCondition.EastAsianWidth, style.UseColors
	runewidth.DefaultCondition.EastAsianWidth, style.UseColors = false, true
	t.Cleanup(func() { runewidth.DefaultCondition.EastAsianWidth, style.UseColors = prevEastAsian, prevColors })
	ctx := context.Background()
	for _, name := range []string{"debian13", "hardened"} {
		for _, rows := range []int{24, 40} {
			t.Run(fmt.Sprintf("%s-80x%d", name, rows), func(t *testing.T) {
				_, sys := loadStatusHost(t, name)
				cfg, err := config.Read(ctx, sys, hardnYML)
				if errors.Is(err, fs.ErrNotExist) {
					cfg, err = config.DefaultConfig(), nil
				}
				if err != nil {
					t.Fatal(err)
				}
				env := term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: rows,
					Environ: []string{"TERM=xterm-256color", "LANG=C.UTF-8"}})
				m := liveMenu(t, env, func(ctx context.Context) (screen.MainMenu, error) {
					return mainmenu.Status(ctx, mainmenu.Options{System: sys, Detect: detect.Detect, Config: cfg, Version: "v0.4.0"})
				}, "q")
				out, _, _ := session(t, func() { m.ShowMainMenu() })
				screen, _, _ := strings.Cut(strings.TrimPrefix(out, clearScreen), clearScreen)
				var b strings.Builder
				b.WriteString("# hardn's live main menu (refactor plan slice 2, next/s2-menu-live), on its layout (slice 2b, next/s2b-menu)\n")
				b.WriteString("# Generated by TestLiveMainMenuGolden (pkg/menu); regenerate with -update. Do not edit.\n")
				fmt.Fprintf(&b, "# Host: the gallery's status host %s (internal/tools/p4status/testdata/hosts); terminal: 80x%d, 256 colours, UTF-8\n", name, rows)
				b.WriteString("# The status: pkg/screens/mainmenu; the whole screen: screen.MainMenu, full form, origin/main's items (mainMenu)\n")
				b.WriteString("# ESC bytes are shown as the four characters \\x1b, and each screen line ends with $ (as cat -e shows it).\n")
				b.WriteString("# ----\n")
				b.WriteString(visibleScreen(screen))
				assertRenderGolden(t, fmt.Sprintf("main_menu_live/%s-80x%d.golden", name, rows), b.String())
			})
		}
	}
}

// galleryStatus is the gallery's main menu of host name, in the plain-80
// cell of its golden file: from the header's line to the prompt's, each
// line's "$" taken off; with item 6, Auto Updates, under item 5 in its
// columns and the hint "[0-10 or q]", as the main menu has them since
// slice 7's unit (d), the gallery's main menus, the mock's items
// (screen.MainMenuFixture), having no item 6 yet (withUpdatesItem).
func galleryStatus(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(goldenDir, name+".golden"))
	if err != nil {
		t.Fatal(err)
	}
	_, cell, ok := strings.Cut(string(data), "\n=== plain-80:")
	if !ok {
		t.Fatalf("%s: no plain-80 cell", name)
	}
	var lines []string
	for _, l := range strings.Split(cell, "\n")[1:] {
		l = strings.ReplaceAll(strings.TrimSuffix(l, "$"), "[0-5, 7-10 or q]", "[0-10 or q]")
		if strings.HasPrefix(l, "hardn ") || len(lines) > 0 {
			lines = append(lines, l)
		}
		if desc := strings.Index(l, "Configure Hardn backup settings"); strings.HasPrefix(l, "   5) Backup ") && desc > 0 {
			item := "   6) " + updatesItem.Label
			lines = append(lines, item+strings.Repeat(" ", desc-len(item))+updatesItem.Description)
		}
		if strings.HasPrefix(l, "  > ") && len(lines) > 0 {
			return strings.Join(lines, "\n")
		}
	}
	t.Fatalf("%s: no main menu in the plain-80 cell", name)
	return ""
}

// statusFixture is one p4status fixture: what one reading of the host
// used of it (internal/tools/p4status/testdata/README.md).
type statusFixture struct {
	Setuid []string `json:"setuid"`
	Users  []struct {
		Name     string `json:"name"`
		UID, GID int
		Home     string `json:"home"`
	} `json:"users"`
	Groups []struct {
		Name string `json:"name"`
		GID  int    `json:"gid"`
	} `json:"groups"`
	Commands []struct {
		Argv           []string `json:"argv"`
		Stdout, Stderr string
		Exit           int    `json:"exit"`
		Err            string `json:"err"`
	} `json:"commands"`
	Entries []struct {
		Path, Type, Mode, Data, Target string
		UID, GID                       int
	} `json:"entries"`
}

// loadStatusHost returns the fixture name as a recording fake, its
// commands scripted, and the host the checks read: the fake, with the
// setuid bit on the fixture's setuid files, which hostfake does not keep
// (as p4status's loadHost).
func loadStatusHost(t *testing.T, name string) (*hostfake.System, host.System) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var x statusFixture
	if err := json.Unmarshal(data, &x); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	f := hostfake.New(host.Options{})
	for _, e := range x.Entries {
		mode, _ := strconv.ParseUint(e.Mode, 8, 32)
		switch e.Type {
		case "dir":
			f.AddDir(e.Path, fs.FileMode(mode), e.UID, e.GID)
		case "file":
			f.AddFile(e.Path, []byte(e.Data), fs.FileMode(mode), e.UID, e.GID)
		case "symlink":
			f.AddSymlink(e.Path, e.Target)
		default:
			t.Fatalf("%s: %s: type %q", name, e.Path, e.Type)
		}
	}
	for _, u := range x.Users {
		f.AddUser(host.User{Name: u.Name, UID: u.UID, GID: u.GID, HomeDir: u.Home})
	}
	for _, g := range x.Groups {
		f.AddGroup(host.Group{Name: g.Name, GID: g.GID})
	}
	for _, c := range x.Commands {
		r := hostfake.Response{Stdout: c.Stdout, Stderr: c.Stderr, ExitCode: c.Exit}
		switch text, other := strings.CutPrefix(c.Err, "other: "); {
		case c.Err == "notfound":
			r.Err = fmt.Errorf("fixture: %s: %w", c.Argv[0], host.ErrNotFound)
		case other:
			r.Err = errors.New(text)
		case c.Err != "":
			t.Fatalf("%s: %v: err %q", name, c.Argv, c.Err)
		}
		f.Respond(r, c.Argv[0], c.Argv[1:]...)
	}
	return f, setuidHost{f, x.Setuid}
}

// setuidHost is a fake whose Stat shows the setuid bit on the files a
// fixture names: the users check counts only an elevation tool that is
// setuid root.
type setuidHost struct {
	*hostfake.System
	setuid []string
}

func (s setuidHost) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	fi, err := s.System.Stat(ctx, name)
	if err == nil && slices.Contains(s.setuid, name) {
		fi = setuidInfo{fi}
	}
	return fi, err
}

// setuidInfo is a file's information with its setuid bit set.
type setuidInfo struct{ fs.FileInfo }

func (i setuidInfo) Mode() fs.FileMode { return i.FileInfo.Mode() | fs.ModeSetuid }

var update = flag.Bool("update", false, "rewrite golden files under testdata/ instead of comparing")

// visibleScreen escapes ESC and marks each line end with $.
func visibleScreen(screen string) string {
	screen = strings.ReplaceAll(screen, "\x1b", `\x1b`)
	lines := strings.Split(screen, "\n")
	var b strings.Builder
	for i, l := range lines {
		if i == len(lines)-1 {
			// Text after the last newline: the prompt, printed without one.
			if l != "" {
				b.WriteString(l + "<no-newline>\n")
			}
			break
		}
		b.WriteString(l + "$\n")
	}
	return b.String()
}

func assertRenderGolden(t *testing.T, rel, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", filepath.FromSlash(rel))
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", path, err)
	}
	if got == string(want) {
		return
	}
	wl, gl := strings.Split(string(want), "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			t.Fatalf("golden %s differs at line %d\nwant: %q\ngot:  %q\n(if the change is intended, rerun with -update and explain the diff in the review description)", path, i+1, w, g)
		}
	}
}
