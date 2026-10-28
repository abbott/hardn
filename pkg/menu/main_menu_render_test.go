package menu

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/security"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/version"
)

var update = flag.Bool("update", false, "rewrite golden files under testdata/ instead of comparing")

// Sudo Method row values; in production the row comes from a live user lookup.
const (
	sudoNoPassword       = "no password"
	sudoPasswordRequired = "password required"
	sudoUnknown          = "unknown"
)

type mainScreenFixture struct {
	name            string
	about           string
	osInfo          *osdetect.OSInfo
	sshPort         int
	noColor         bool
	updateAvailable string // version offered through SetTestUpdateAvailable (--test-update), or ""
	host            model.HostInfo
	status          security.SecurityStatus
	sudoMethod      string
	notes           []string
	// gate is a P0.8 hazard gate's fix that this screen shows, if any.
	gate *screenGate
}

// screenGate is a P0.8 fix as a screen golden shows it: a FIXED line
// (without the prefix), printed only when the screen shows the fix, so
// that flipping the gate's constant and running -update drops the line
// along with the restored screen.
type screenGate struct {
	fixed string
	shows func(screen string) bool
}

var exampleHost = model.HostInfo{
	Hostname:    "example-host",
	IPAddresses: []string{"192.0.2.10"},
	Uptime:      182*24*time.Hour + 2*time.Hour + 26*time.Minute,
}

// figmaStatus matches the design export's main menu: everything configured
// except AppArmor and automatic updates.
var figmaStatus = security.SecurityStatus{
	RootLoginEnabled: false, FirewallEnabled: true, FirewallConfigured: true, SecureUsers: true,
	AppArmorEnabled: false, UnattendedUpgrades: false, SudoConfigured: true,
	SshPortNonDefault: true, PasswordAuthDisabled: true,
}

func mainScreenFixtures() []mainScreenFixture {
	return []mainScreenFixture{
		{
			name:    "ubuntu-fresh",
			about:   "a fresh host: nothing configured, port 22, sudo method unknown",
			osInfo:  &osdetect.OSInfo{OsType: "ubuntu", OsVersion: "24.04", OsCodename: "noble"},
			sshPort: 22, host: exampleHost, sudoMethod: sudoUnknown,
			status: security.SecurityStatus{RootLoginEnabled: true},
		},
		{
			name:    "debian-figma",
			about:   "the design export's state: all configured except AppArmor and Auto Updates",
			osInfo:  &osdetect.OSInfo{OsType: "debian", OsVersion: "13", OsCodename: "trixie"},
			sshPort: 2230, host: exampleHost, status: figmaStatus, sudoMethod: sudoPasswordRequired,
		},
		{
			name:    "debian-figma-no-color",
			about:   "debian-figma with style.UseColors=false (what NO_COLOR or --no-color sets)",
			osInfo:  &osdetect.OSInfo{OsType: "debian", OsVersion: "13", OsCodename: "trixie"},
			sshPort: 2230, noColor: true, host: exampleHost, status: figmaStatus, sudoMethod: sudoPasswordRequired,
			notes: []string{"style.UseColors=false only affects style.Colored; Bolded, Dimmed and Underlined still emit escapes (pkg/style/style.go:189-241)."},
		},
		{
			name:    "alpine-update-available",
			about:   "Alpine, root login allowed, UFW on without policies, passwordless sudo, update v0.4.1 offered through SetTestUpdateAvailable (--test-update)",
			osInfo:  &osdetect.OSInfo{OsType: "alpine", OsVersion: "3.23.0", OsCodename: "3.23.0"},
			sshPort: 2230, updateAvailable: "0.4.1", host: exampleHost, sudoMethod: sudoNoPassword,
			gate: &screenGate{
				fixed: "(P0.8, gated by updateCheckEnabled): V26 — no update check runs, and --test-update offers nothing, so the screen shows no update notice; origin/main showed \"v0.4.1 available\" and the repository URL on a second line",
				shows: func(screen string) bool { return !strings.Contains(screen, "0.4.1 available") },
			},
			status: security.SecurityStatus{
				RootLoginEnabled: true, FirewallEnabled: true, FirewallConfigured: false, SecureUsers: true,
				SudoConfigured: true, SshPortNonDefault: true, PasswordAuthDisabled: true,
			},
		},
	}
}

// renderStatusRows mirrors security.DisplaySecurityStatusWithCustomPrinter
// (pkg/security/status.go:84-174) row for row. The real function cannot run
// in a test: it builds an OSUserRepository on the real file system and
// commander and reloads the config from the real search path to fill the
// Sudo Method row (status.go:98-129). Here that row is fixture input.
func renderStatusRows(cfg *config.Config, status *security.SecurityStatus, sudoMethod string, formatter *style.StatusFormatter, printFn func(string)) {
	if !status.SecureUsers {
		printFn(formatter.FormatWarning("Users", "Not Configured", "root user only", "dark"))
	} else {
		printFn(formatter.FormatConfigured("Users", "Configured", "non-root, sudo", "dark"))
	}
	if !status.SudoConfigured {
		printFn(formatter.FormatWarning("Sudo", "Not Installed", "", "dark"))
	}
	switch sudoMethod {
	case sudoNoPassword:
		printFn(formatter.FormatWarning("Sudo Method", "Configured", "no password", "dark"))
	case sudoPasswordRequired:
		printFn(formatter.FormatConfigured("Sudo Method", "Configured", "password required", "dark"))
	default:
		printFn(formatter.FormatWarning("Sudo Method", "Not Configured", "unknown", "dark"))
	}
	if !status.FirewallEnabled {
		printFn(formatter.FormatWarning("Firewall", "Not Configured", "vulnerable", "dark"))
	} else if !status.FirewallConfigured {
		printFn(formatter.FormatWarning("Firewall", "Enabled", "configure policies", "dark"))
	} else {
		printFn(formatter.FormatConfigured("Firewall", "Configured", "deny policy", "dark"))
	}
	if status.RootLoginEnabled {
		printFn(formatter.FormatWarning("SSH Login", "Not Configured", "root allowed", "dark"))
	} else {
		printFn(formatter.FormatConfigured("SSH Login", "Configured", "root disallowed", "dark"))
	}
	if !status.PasswordAuthDisabled {
		printFn(formatter.FormatWarning("SSH Auth", "Not Configured", "password auth enabled", "dark"))
	} else {
		printFn(formatter.FormatConfigured("SSH Auth", "Configured", "key-only auth", "dark"))
	}
	if !status.SshPortNonDefault {
		printFn(formatter.FormatWarning("SSH Port", "Not Configured", "default (22)", "dark"))
	} else {
		printFn(formatter.FormatConfigured("SSH Port", "Configured", "non-default ("+strconv.Itoa(cfg.SshPort)+")", "dark"))
	}
	if !status.AppArmorEnabled {
		printFn(formatter.FormatWarning("AppArmor", "Not Configured", "", "dark"))
	} else {
		printFn(formatter.FormatConfigured("AppArmor", "Configured", "", "dark"))
	}
	if !status.UnattendedUpgrades {
		printFn(formatter.FormatWarning("Auto Updates", "Not Configured", "", "dark"))
	} else {
		printFn(formatter.FormatConfigured("Auto Updates", "Configured", "", "dark"))
	}
}

// renderMainScreen produces the status block one pass of origin/main's
// ShowMainMenu printed (main_menu.go:387-427) above its menu:
// displayNormalSecurityStatus's (main_menu.go:213-283). Since
// next/s2-menu-live that block is origin/main's alone, behind
// legacySecurityStatus (showLegacyStatus), DISABLED: these goldens keep
// it, the gate's other side (their line numbers are origin/main's
// file's). The menu origin/main printed under it, createMainMenu's
// menu.Print(), went with item 6 in slice 2b's unit (d), and the whole
// screen as these goldens held it until then is the parity record
// (testdata/legacy/main_menu). The live block is main_menu_live_test.go's.
// formatHardnVersionLine, formatHostInfoLines, formatOSTitle, style.Box,
// style.StatusFormatter and security.GetSecurityRiskLevel are the real
// code; the composition and the status rows are mirrored, because
// displayNormalSecurityStatus reads the live host (GetHostInfo, status.go).
func renderMainScreen(t *testing.T, f mainScreenFixture) string {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.SshPort = f.sshPort

	// FormatUptime is the only MenuManager method the render needs; it is pure.
	menuManager := application.NewMenuManager(nil, nil, nil, nil, nil, nil, nil, nil, application.NewHostInfoManager(nil))
	m := NewMainMenu(menuManager, cfg, f.osInfo, version.NewService("0.4.0", "", ""))
	if f.updateAvailable != "" {
		m.SetTestUpdateAvailable(f.updateAvailable)
	}

	return captureStdout(t, func() {
		// Same labels and buffer as ShowMainMenu (main_menu.go:397-409).
		formatter := style.NewStatusFormatter([]string{
			"Host", "OS", "Uptime", "Risk Level", "SSH Root Login", "Firewall",
			"Users", "SSH Port", "SSH Auth", "AppArmor", "Auto Updates",
		}, 2)

		// displayNormalSecurityStatus, main_menu.go:215-234.
		fmt.Println(m.formatHardnVersionLine(formatter))
		if m.updateAvailable {
			fmt.Println(formatter.FormatLine("", "", "", style.Dimmed(repoURL), style.Gray08, "", "no-indent"))
		}
		hostLine, uptimeLine := m.formatHostInfoLines(&f.host, formatter)

		// main_menu.go:240-248.
		box := style.NewBox(style.BoxConfig{
			Width:               64,
			ShowEmptyRow:        true,
			ShowTopShade:        true,
			ShowBottomSeparator: true,
			Indentation:         0,
			Title:               m.formatOSTitle(),
			TitleColor:          "skip",
		})
		// main_menu.go:251-282.
		box.DrawBox(func(printLine func(string)) {
			printLine(hostLine)
			printLine(uptimeLine)
			printLine("")
			indented := style.IndentPrinter(printLine, 2)
			riskLevel, riskDescription, riskColor := security.GetSecurityRiskLevel(&f.status)
			riskLine := formatter.FormatLine(style.SymDotTri, riskColor, style.Bold+"Risk Level"+style.Reset, riskLevel, riskColor, style.SymApprox+" "+riskDescription, "dark")
			indented(riskLine)
			indented("")
			renderStatusRows(cfg, &f.status, f.sudoMethod, formatter, indented)
		})
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stdout = orig }()
	fn()
	os.Stdout = orig
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}

// TestMainMenuRenderGolden records the origin/main main-menu screen's status
// block for fixed status input: no exec, no network, no terminal (stty)
// calls.
func TestMainMenuRenderGolden(t *testing.T) {
	// Ambiguous-width glyphs (for example ⛬ and ≈) count as one cell unless
	// the locale is East Asian; pin that so the box padding is the same everywhere.
	prevEastAsian := runewidth.DefaultCondition.EastAsianWidth
	runewidth.DefaultCondition.EastAsianWidth = false
	prevColors := style.UseColors
	t.Cleanup(func() {
		runewidth.DefaultCondition.EastAsianWidth = prevEastAsian
		style.UseColors = prevColors
	})

	for _, f := range mainScreenFixtures() {
		t.Run(f.name, func(t *testing.T) {
			style.UseColors = !f.noColor
			screen := renderMainScreen(t, f)

			fixed := f.gate != nil && f.gate.shows(screen)
			var b strings.Builder
			if fixed {
				b.WriteString("# origin/main behaviour (baseline per maintainer decision 6), with the P0.8 hazard gate below\n")
			} else {
				b.WriteString("# origin/main behaviour (baseline per maintainer decision 6)\n")
			}
			b.WriteString("# Generated by TestMainMenuRenderGolden (pkg/menu); regenerate with -update. Do not edit.\n")
			b.WriteString("# Screen: main menu, the status block of one pass of ShowMainMenu (pkg/menu/main_menu.go:213-283)\n")
			b.WriteString("# The menu under it went with item 6 (slice 2b, unit (d)); the whole screen is frozen in testdata/legacy/main_menu.\n")
			fmt.Fprintf(&b, "# Fixture: %s\n", f.about)
			fmt.Fprintf(&b, "# Fixture status: %+v; Sudo Method row: %q\n", f.status, f.sudoMethod)
			if fixed {
				fmt.Fprintf(&b, "# FIXED %s\n", f.gate.fixed)
			}
			for _, n := range f.notes {
				fmt.Fprintf(&b, "# NOTE: %s\n", n)
			}
			b.WriteString("# ESC bytes are shown as the four characters \\x1b, and each screen line ends with $ (as cat -e shows it)\n")
			b.WriteString("# so trailing spaces stay visible; <no-newline> marks text printed without a final newline.\n")
			b.WriteString("# Everything after the line below is the screen.\n")
			b.WriteString("# ----\n")
			b.WriteString(visibleScreen(screen))

			assertRenderGolden(t, "main_menu/"+f.name+".golden", b.String())
		})
	}
}

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
