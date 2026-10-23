package menu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/version"
)

// The main menu live (refactor plan slice 2, next/s2-menu-live): its
// status drawn through the hook cmd/hardn sets (SetStatus), in place of
// pkg/security's block, which is gated (legacySecurityStatus). Nothing on
// this machine is read or run: the hook is a fake, or pkg/screens/mainmenu
// over hostfake.

// clearScreen is what utils.ClearScreen prints.
const clearScreen = "\033[H\033[2J"

// liveMenu is a main menu as cmd/hardn builds it, its status drawn by
// draw; the end of the input after lines.
func liveMenu(t *testing.T, draw func(ctx context.Context, below int) (string, error), lines ...string) *MainMenu {
	t.Helper()
	if legacySecurityStatus {
		t.Skip("legacySecurityStatus is set: the main menu would read this machine (pkg/security)")
	}
	scripted(t, strings.Join(append(lines, ""), "\n"))
	m := NewMainMenu(nil, config.DefaultConfig(), nil, version.NewService("0.4.0", "", ""))
	if draw != nil {
		m.SetStatus(draw)
	}
	return m
}

// TestLiveStatus: each pass of the main menu draws what the hook returns,
// after the clear and above its items, which are origin/main's, drawn as
// before; the hook is told the lines the items take, heading to prompt,
// and is called again on every pass, with a context that is not done.
func TestLiveStatus(t *testing.T) {
	var belows []int
	m := liveMenu(t, func(ctx context.Context, below int) (string, error) {
		if ctx.Err() != nil {
			t.Error("the hook's context is done")
		}
		belows = append(belows, below)
		return "STATUS\n\n", nil
	}, "x", "", "q")
	out, _, ended := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
	items := m.createMainMenu().Render()
	if ended || !strings.HasPrefix(out, clearScreen+"STATUS\n\n"+items) || strings.Count(out, clearScreen+"STATUS\n\n"+items) != 2 {
		t.Errorf("ended %v; the screen:\n%q", ended, out)
	}
	if n := strings.Count(items, "\n") + 1; !slices.Equal(belows, []int{n, n}) || n != 17 {
		t.Errorf("the hook was told %v, want 17 on each of two passes (the items take %d lines)", belows, n)
	}
	for _, legacy := range []string{"Risk Level", "Sudo Method", "hardn"} {
		if strings.Contains(out, legacy) {
			t.Errorf("%q drawn: the legacy block is gated", legacy)
		}
	}
}

// TestLiveStatusEnds: a hook that returns a cancelled context's error
// ends hardn as Ctrl-C at the menu's prompt does, by SIGINT; another
// error is printed and ends hardn with 1; the items are not drawn. A main
// menu with no hook set panics before it draws anything.
func TestLiveStatusEnds(t *testing.T) {
	failed := errors.New("main menu: write: broken pipe")
	for _, tc := range []struct {
		name string
		err  error
		code int
	}{
		{"ctrl-c", fmt.Errorf("main menu: engine: detect: %w", context.Canceled), int(sigint)},
		{"another error", failed, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := liveMenu(t, func(context.Context, int) (string, error) { return "", tc.err }, "q")
			out, code, ended := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
			if !ended || code != tc.code || strings.Contains(out, "Select an option") {
				t.Errorf("ended %v with %d, want %d; the screen:\n%q", ended, code, tc.code, out)
			}
			if tc.code == 1 && !strings.Contains(out, failed.Error()) {
				t.Errorf("the error is not printed: %q", out)
			}
		})
	}
	t.Run("no hook", func(t *testing.T) {
		m := liveMenu(t, nil, "q")
		out, _, ended := session(t, func() {
			defer func() {
				if recover() == nil {
					t.Error("the main menu drew with no status set")
				}
			}()
			m.ShowMainMenu("0.4.0", "", "")
		})
		if ended || out != clearScreen {
			t.Errorf("ended %v, printed %q", ended, out)
		}
	})
}

// TestSecurityAlertLive: the security-update alert, which needs update
// checks (off while pkg/version's updateCheckEnabled is false, V26), still
// replaces the status as origin/main drew it, and ends the main menu
// after Enter; the hook is not called.
func TestSecurityAlertLive(t *testing.T) {
	called := false
	m := liveMenu(t, func(context.Context, int) (string, error) { called = true; return "", nil }, "")
	m.securityUpdateAvailable, m.securityUpdateDetails, m.latestVersion = true, "CVE-2023-1234 fixed", "0.4.1"
	out, _, ended := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
	if ended || called || !strings.Contains(out, "Update Binary") || !strings.Contains(out, "CVE-2023-1234 fixed") || strings.Contains(out, "Select an option") {
		t.Errorf("ended %v, hook called %v; the screen:\n%q", ended, called, out)
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
var statusRuns = map[string]int{"debian13": 28, "ubuntu2604": 31, "alpine324": 13, "pve9": 23, "hardened": 28}

// queries are the commands a status may run, by their first words: each
// only reads (docs/engine/vmcheck.md §2.5 has each one's arguments).
var queries = []string{"sshd -T", "/usr/sbin/sshd -T", "systemctl is-active", "systemctl is-enabled", "systemctl show",
	"ss -Htln", "ufw status", "apt-config dump", "dpkg-query --show", "sudo --version", "visudo -c", "doas -C",
	"ip -4", "apk info", "rc-service crond", "resolvectl --no-pager"}

// TestLiveStatusOnHosts: the main menu with cmd/hardn's status
// (pkg/screens/mainmenu over the host, hardn.yml loaded from it as hardn
// loads it) on Debian 13, Ubuntu 26.04, Alpine 3.24, Proxmox VE 9 and
// Debian 13 hardened: its header, host block, risk level and rows, the
// DNS row included, are line for line the gallery's status host's; the
// status only reads, its commands every one a query the host answers, as
// many on every pass (statusRuns), and nothing is written.
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
			m := liveMenu(t, func(ctx context.Context, below int) (string, error) {
				return mainmenu.Status(ctx, mainmenu.Options{System: sys, Detect: detect.Detect, Config: cfg, Version: "v0.4.0",
					Env: term.Resolve(term.Options{}, term.Facts{Width: 80})}, below)
			}, "q")
			out, _, ended := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
			want := clearScreen + galleryStatus(t, name) + "\n\n" + m.createMainMenu().Render()
			if ended || !strings.HasPrefix(out, want) {
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
// UTF-8 terminal of 80 columns: at 24 rows, the status in its compact form
// above origin/main's menu; at 40, in full (docs/engine/menu-live.md §6).
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
				m := liveMenu(t, func(ctx context.Context, below int) (string, error) {
					return mainmenu.Status(ctx, mainmenu.Options{System: sys, Detect: detect.Detect, Config: cfg, Version: "v0.4.0", Env: env}, below)
				}, "q")
				out, _, _ := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
				screen, _, _ := strings.Cut(strings.TrimPrefix(out, clearScreen), clearScreen)
				var b strings.Builder
				b.WriteString("# hardn's live main menu (refactor plan slice 2, next/s2-menu-live)\n")
				b.WriteString("# Generated by TestLiveMainMenuGolden (pkg/menu); regenerate with -update. Do not edit.\n")
				fmt.Fprintf(&b, "# Host: the gallery's status host %s (internal/tools/p4status/testdata/hosts); terminal: 80x%d, 256 colours, UTF-8\n", name, rows)
				b.WriteString("# The status: pkg/screens/mainmenu (screen.MainMenu.StatusBlocks); under it, origin/main's menu (createMainMenu)\n")
				b.WriteString("# ESC bytes are shown as the four characters \\x1b, and each screen line ends with $ (as cat -e shows it).\n")
				b.WriteString("# ----\n")
				b.WriteString(visibleScreen(screen))
				assertRenderGolden(t, fmt.Sprintf("main_menu_live/%s-80x%d.golden", name, rows), b.String())
			})
		}
	}
}

// galleryStatus is the gallery's status of host name, in the plain-80
// cell of its golden file: from the header's line to the divider's, each
// line's "$" taken off.
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
		l = strings.TrimSuffix(l, "$")
		if strings.HasPrefix(l, "hardn ") || len(lines) > 0 {
			lines = append(lines, l)
		}
		if strings.HasPrefix(l, "-----") && len(lines) > 0 {
			return strings.Join(lines, "\n")
		}
	}
	t.Fatalf("%s: no status in the plain-80 cell", name)
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
