package characterization

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/osdetect"
)

// menuChild names, in a child process of this test binary, the legacy
// screen it runs (TestLegacyMenuDryRun).
const menuChild = "HARDN_CHARACTERIZATION_MENU"

// menuCase is one legacy screen run on a script: the case's name, which
// the child runs, what it shows and why, and the lines typed.
type menuCase struct {
	name, screen, script string
	notes, defects       []string
	fixed                []string // a defect fixed since, the screen and the file as they are now
}

// menuCases are the Dry-Run item (main menu 6) from each state, and the
// Firewall screen's Configure under dry run: refactor plan slice 2b's
// evidence (docs/engine/gallery-dryrun.md §1).
func menuCases() []menuCase {
	const (
		pauses = "3 pauses for one toggle (dry_run_handler.go:52, dry_run_menu.go:124, dry_run_handler.go:73; interaction.md §4.3)"
		saved  = "the whole configuration saved to ./hardn.yml in the working directory, not to the file loaded (dry_run_menu.go:118; side-effects row 29)"
		// Fixed by refactor plan slice 2b, next/s2b-mode: origin/main saved
		// the session's dryRun, which the next start ignored.
		asLoaded = "decision 8 (slice 2b, next/s2b-mode): the save writes dryRun as loaded, here the defaults' false, never the session's mode (config.SaveConfig), so the toggle is the session's alone"
		v3       = "V3 (slice 2b, next/s2b-mode): the next start takes the dryRun of the file it loads unless -n/--dry-run is given (§3.19; config/full-flag.golden)"
	)
	return []menuCase{
		{name: "dry_run_enable", screen: "Dry-Run (main menu 6) in live mode, 1 to enable", script: "\n1\n\n\n",
			defects: []string{pauses, saved}, fixed: []string{asLoaded, v3}},
		{name: "dry_run_disable", screen: "Dry-Run (main menu 6) in dry-run mode, 1 to disable", script: "\n1\n\n\n",
			defects: []string{
				"the Enabled state builds its menu and never prints it: no item and no prompt, though a choice is read (dry_run_menu.go:44-59)",
				pauses, saved,
			}, fixed: []string{asLoaded, v3}},
		{name: "dry_run_return", screen: "Dry-Run (main menu 6) in live mode, 0 to keep it", script: "\n0\n\n\n",
			notes:   []string{"0 changes nothing and still saves."},
			defects: []string{pauses, saved}},
		{name: "dry_run_quit", screen: "Dry-Run (main menu 6) in live mode, q at its menu", script: "\nq\n\n",
			notes: []string{"q leaves the settings screen unsaved and returns to the handler's pause, not out of hardn."}},
		{name: "firewall_configure_dry_run", screen: "Firewall (main menu 4) in dry-run mode, 2 (Configure firewall)",
			script: "2\n\n",
			notes: []string{
				"Debian 13, SSH port 2230; ufw installed and inactive (the mock commander answers \"which ufw\" and an empty \"ufw status\").",
				"Under dry run the screen prints what it would do, with no plan: no file, rule or command named.",
			}},
	}
}

// TestLegacyMenuDryRun records origin/main's Dry-Run item and a legacy
// screen under dry run, each in a child process of this test binary
// whose stdin is the script, not a terminal, and whose working directory
// is a temp dir: the legacy menus read the one line reader over os.Stdin
// (pkg/ui/prompt.Stdin) and print to os.Stdout, and the Dry-Run screen
// saves ./hardn.yml. Each golden holds the screen as printed (ESC as
// \x1b), then ./hardn.yml as the screen left it; the Firewall's, what
// the mocks were asked and wrote. The pauses say "Press Enter", where
// origin/main says "Press any key" (DEV-050); nothing else differs from
// origin/main in these files.
func TestLegacyMenuDryRun(t *testing.T) {
	if name := os.Getenv(menuChild); name != "" {
		runMenuChild(name)
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range menuCases() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestLegacyMenuDryRun$")
			cmd.Env = append(os.Environ(), menuChild+"="+tc.name, "NO_COLOR=", "RUNEWIDTH_EASTASIAN=0", "HARDN_CONFIG=")
			cmd.Dir = dir
			cmd.Stdin = strings.NewReader(tc.script)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("child: %v\n%s", err, out)
			}
			screen, trailer, _ := strings.Cut(string(out), childTrailer)
			saved, err := os.ReadFile(filepath.Join(dir, "hardn.yml"))
			switch {
			case errors.Is(err, fs.ErrNotExist):
				saved = nil
			case err != nil:
				t.Fatal(err)
			}

			var b strings.Builder
			b.WriteString(baselineHeader + "\n")
			b.WriteString("# Generated by TestLegacyMenuDryRun (pkg/characterization); regenerate with -update. Do not edit.\n")
			fmt.Fprintf(&b, "# Screen: %s (pkg/menu), run in a child process\n", tc.screen)
			fmt.Fprintf(&b, "# Script: stdin %q, not a terminal, so nothing typed is echoed\n", tc.script)
			fmt.Fprintf(&b, "# Pauses: %d (\"Press Enter to ...\"; origin/main says \"Press any key to ...\", DEV-050)\n", strings.Count(screen, "Press Enter to"))
			for _, d := range tc.defects {
				fmt.Fprintf(&b, "# KNOWN-DEFECT: %s\n", d)
			}
			for _, f := range tc.fixed {
				fmt.Fprintf(&b, "# FIXED: %s\n", f)
			}
			for _, n := range tc.notes {
				fmt.Fprintf(&b, "# NOTE: %s\n", n)
			}
			b.WriteString("# The screen as printed, ESC as the four characters \\x1b, each line ending in $; <no-newline> marks text with no final newline.\n")
			b.WriteString("# ----\n")
			b.WriteString(visible(screen))
			b.WriteString("# ----\n")
			switch {
			case trailer != "":
				b.WriteString(trailer)
			case saved == nil:
				b.WriteString("# ./hardn.yml: not written\n")
			default:
				b.WriteString("# ./hardn.yml as written:\n")
				b.Write(saved)
			}
			if strings.Contains(b.String(), dir) {
				t.Fatalf("golden would contain the temp dir %s", dir)
			}
			assertGolden(t, "menu/"+tc.name+".golden", b.String())
		})
	}
}

// childTrailer sets what a child reports of its mocks apart from the
// screen.
const childTrailer = "\x00trailer\n"

// runMenuChild runs the legacy screen of case name, in the child process,
// on a configuration of hardn's defaults for alice on port 2230.
func runMenuChild(name string) {
	cfg := config.DefaultConfig()
	cfg.Username = "alice"
	cfg.SshPort = 2230
	switch name {
	case "dry_run_enable", "dry_run_return", "dry_run_quit":
		menu.NewDryRunHandler(nil, cfg).Handle()
	case "dry_run_disable":
		cfg.DryRun = true
		menu.NewDryRunHandler(nil, cfg).Handle()
	case "firewall_configure_dry_run":
		cfg.DryRun = true
		fsys, mc := interfaces.NewMockFileSystem(), interfaces.NewMockCommander()
		pve := secondary.NewProxmoxHost(fsys, mc, "debian", false)
		osInfo := model.OSInfo{Type: "debian", Version: "13", Codename: "trixie"}
		firewall := application.NewFirewallManager(service.NewFirewallServiceImpl(secondary.NewUFWFirewallRepository(fsys, mc, pve), osInfo))
		mm := application.NewMenuManager(nil, nil, firewall, nil, nil, nil, nil, nil, nil, nil)
		menu.NewFirewallMenu(mm, cfg, &osdetect.OSInfo{OsType: "debian", OsVersion: "13", OsCodename: "trixie"}).Show()
		fmt.Print(childTrailer)
		fmt.Printf("# Commands the screen ran (mock commander): %s\n", strings.Join(mc.ExecutedCommands, "; "))
		fmt.Printf("# Files it wrote (mock file system): %d\n", len(fsys.Files))
	default:
		fmt.Fprintf(os.Stderr, "no menu case %q\n", name)
		os.Exit(2)
	}
}

// visible shows a screen line by line: ESC as \x1b, each line ending in
// $, and text after the last line break marked <no-newline>.
func visible(screen string) string {
	lines := strings.Split(strings.ReplaceAll(screen, "\x1b", `\x1b`), "\n")
	var b strings.Builder
	for i, l := range lines {
		switch {
		case i < len(lines)-1:
			b.WriteString(l + "$\n")
		case l != "":
			b.WriteString(l + "<no-newline>\n")
		}
	}
	return b.String()
}
