package menu

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"

	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
)

const confirmPrompt = "Type 'yes' to confirm you want to apply real changes: "

// newRunAllMenu builds the Run All screen on a MenuManager whose managers
// run on the repo's mock file system and commander, for a Debian 13 host
// and a config for alice on port 2230: nothing is read from or run on this
// host.
func newRunAllMenu(t *testing.T, dryRun bool) (*RunAllMenu, *interfaces.MockFileSystem, *interfaces.MockCommander) {
	t.Helper()
	return newRunAllMenuOn(t, dryRun, false)
}

// newRunAllMenuOn is newRunAllMenu on a host that is Proxmox VE when pve
// is set (/etc/pve exists), with one Proxmox check shared by the SSH and
// firewall repositories and the MenuManager, as the ServiceFactory wires
// it.
func newRunAllMenuOn(t *testing.T, dryRun, pve bool) (*RunAllMenu, *interfaces.MockFileSystem, *interfaces.MockCommander) {
	t.Helper()
	fs := interfaces.NewMockFileSystem()
	mc := interfaces.NewMockCommander()
	if pve {
		fs.Directories["/etc/pve"] = true
	}
	host := secondary.NewProxmoxHost(fs, mc, "debian", false)
	osInfo := model.OSInfo{Type: "debian", Version: "13", Codename: "trixie"}
	users := application.NewUserManager(service.NewUserServiceImpl(secondary.NewOSUserRepository(fs, mc, "debian")))
	ssh := application.NewSSHManager(service.NewSSHServiceImpl(secondary.NewFileSSHRepository(fs, mc, "debian", host), osInfo))
	firewall := application.NewFirewallManager(service.NewFirewallServiceImpl(secondary.NewUFWFirewallRepository(fs, mc, host), osInfo))
	dns := application.NewDNSManager(service.NewDNSServiceImpl(secondary.NewFileDNSRepository(fs, mc, "debian"), osInfo))
	security := application.NewSecurityManager(users, ssh, firewall, dns)
	mm := application.NewMenuManager(users, ssh, firewall, dns, nil, security, nil)
	mm.SetProxmoxCheck(host.IsProxmox)

	cfg := config.DefaultConfig()
	cfg.Username = "alice"
	cfg.SshPort = 2230
	cfg.SshAllowedUsers = []string{"alice"}
	cfg.DryRun = dryRun
	return NewRunAllMenu(mm, cfg, &osdetect.OSInfo{OsType: "debian", OsVersion: "13", OsCodename: "trixie"}), fs, mc
}

// TestRunAllConfirmation drives Run All with scripted stdin (A4's gate):
// option 1 in live mode applies nothing unless the answer is "yes".
func TestRunAllConfirmation(t *testing.T) {
	if runAllWithoutConfirmation {
		t.Fatal("runAllWithoutConfirmation is set: Run All option 1 runs live with no confirmation (A4)")
	}
	for _, tc := range []struct {
		name    string
		dryRun  bool
		script  string // up to and including the final pause
		prompt  bool   // whether the confirmation is asked
		applied bool
	}{
		{name: "live, no", script: "1\nno\n\n", prompt: true},
		{name: "live, empty answer", script: "1\n\n\n", prompt: true},
		{name: "live, y is not yes", script: "1\ny\n\n", prompt: true},
		{name: "live, yes with spaces", script: "1\n  yes \n\n", prompt: true, applied: true},
		{name: "live, YES", script: "1\nYES\n\n", prompt: true, applied: true},
		{name: "dry-run simulates without asking", dryRun: true, script: "1\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A legacy save would write ./hardn.yml here: none may.
			t.Chdir(t.TempDir())
			m, fs, mc := newRunAllMenu(t, tc.dryRun)
			scripted(t, tc.script+"next\n")
			out, code, ended := session(t, m.Show)
			if ended {
				t.Fatalf("ended hardn with %d:\n%s", code, out)
			}
			if got := strings.Contains(out, confirmPrompt); got != tc.prompt {
				t.Errorf("asked for confirmation: %v, want %v:\n%s", got, tc.prompt, out)
			}
			applied := len(fs.Files) > 0 || len(mc.ExecutedCommands) > 0
			if applied != tc.applied {
				t.Errorf("applied: %v (files %d, commands %v), want %v:\n%s", applied, len(fs.Files), mc.ExecutedCommands, tc.applied, out)
			}
			if tc.prompt && !tc.applied && !strings.Contains(out, "Operation cancelled. No changes were made.") {
				t.Errorf("no cancellation message:\n%s", out)
			}
			if tc.applied && !strings.Contains(out, "System hardening") {
				t.Errorf("hardening did not run:\n%s", out)
			}
			if _, err := os.Stat("hardn.yml"); err == nil {
				t.Errorf("the config was saved to ./hardn.yml")
			}
			var next string
			if _, _, ended := session(t, func() { next = ReadInput() }); ended || next != "next" {
				t.Errorf("the line after the screen's = %q (ended %v): the screen read too few or too many lines", next, ended)
			}
		})
	}
}

// TestRunAllInTheSessionsMode: Run All's option 2, which turned dry run
// on or off, ran, and saved ./hardn.yml, is removed (refactor plan slice
// 2b, unit (d), decision 7): live and under plan only, from -n or from
// hardn.yml, the menu lists option 1 and Return alone, "[0-1 or q]"; 2 is
// refused as any other choice, "Invalid option. Please try again.", and
// the menu drawn again; the mode, and what set it, stay as hardn started;
// nothing is applied, asked or saved.
func TestRunAllInTheSessionsMode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		start  func(t *testing.T) *config.Config
		dryRun bool
		source config.DryRunSource
	}{
		{"live", func(*testing.T) *config.Config { c := config.DefaultConfig(); c.ResolveDryRun(false, false); return c }, false, config.DryRunDefault},
		{"-n", func(*testing.T) *config.Config { c := config.DefaultConfig(); c.ResolveDryRun(true, true); return c }, true, config.DryRunFlag},
		{"hardn.yml", func(t *testing.T) *config.Config {
			f := hostfake.New(host.Options{})
			f.AddFile(hardnYML, []byte("dryRun: true\n"), 0o644, 0, 0)
			c, err := config.Read(context.Background(), f, hardnYML)
			if err != nil {
				t.Fatal(err)
			}
			c.ResolveDryRun(false, false)
			return c
		}, true, config.DryRunFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			m, fs, mc := newRunAllMenu(t, false)
			m.config = tc.start(t)
			m.config.Username, m.config.SshPort = "alice", 2230
			scripted(t, "2\n\n0\nnext\n")
			out, code, ended := session(t, m.Show)
			if ended {
				t.Fatalf("ended hardn with %d:\n%s", code, out)
			}
			plain := style.StripAnsi(out)
			if strings.Contains(plain, "dry-run mode and run") || strings.Count(plain, "Enter selection [0-1 or q]:") != 2 ||
				!strings.Contains(plain, "Invalid option. Please try again.") || strings.Contains(plain, confirmPrompt) {
				t.Errorf("want the menu without option 2, 2 refused and the menu again, no question:\n%s", plain)
			}
			if m.config.DryRun != tc.dryRun || m.config.DryRunSource() != tc.source {
				t.Errorf("the mode is %v from %d, want %v from %d, as hardn started", m.config.DryRun, m.config.DryRunSource(), tc.dryRun, tc.source)
			}
			if len(fs.Files) > 0 || len(mc.ExecutedCommands) > 0 {
				t.Errorf("applied: files %d, commands %v", len(fs.Files), mc.ExecutedCommands)
			}
			if _, err := os.Stat("hardn.yml"); err == nil {
				t.Errorf("the config was saved to ./hardn.yml")
			}
			var next string
			if _, _, ended := session(t, func() { next = ReadInput() }); ended || next != "next" {
				t.Errorf("the line after the screen's = %q (ended %v): the screen read too few or too many lines", next, ended)
			}
		})
	}
}

// TestRunAllConfirmationGolden records the Run All screen when option 1 is
// chosen in live mode and the confirmation is answered "no": the prompt,
// the cancellation and the pause, with nothing applied (A4's gate).
func TestRunAllConfirmationGolden(t *testing.T) {
	prevEastAsian := runewidth.DefaultCondition.EastAsianWidth
	runewidth.DefaultCondition.EastAsianWidth = false
	prevColors := style.UseColors
	style.UseColors = false
	t.Cleanup(func() {
		runewidth.DefaultCondition.EastAsianWidth = prevEastAsian
		style.UseColors = prevColors
	})

	m, fs, mc := newRunAllMenu(t, false)
	scripted(t, "1\nno\n\n")
	out, code, ended := session(t, m.Show)
	if ended {
		t.Fatalf("ended hardn with %d:\n%s", code, out)
	}
	if len(fs.Files) > 0 || len(mc.ExecutedCommands) > 0 {
		t.Fatalf("applied: files %d, commands %v", len(fs.Files), mc.ExecutedCommands)
	}

	var b strings.Builder
	b.WriteString("# origin/main behaviour (baseline per maintainer decision 6), with the P0.8 hazard gate below\n")
	b.WriteString("# Generated by TestRunAllConfirmationGolden (pkg/menu); regenerate with -update. Do not edit.\n")
	b.WriteString("# Screen: Run All (pkg/menu/run_all_menu.go), option 1 in live mode, confirmation answered \"no\"\n")
	b.WriteString("# Fixture: Debian 13, username alice, SSH port 2230, dry-run off, style.UseColors=false; stdin \"1\\nno\\n\\n\" (not a terminal, so no echo)\n")
	b.WriteString("# FIXED (P0.8, gated by runAllWithoutConfirmation): A4 — option 1 asks for a typed 'yes' before a live run; origin/main ran every step on \"1\" with no question\n")
	b.WriteString("# FIXED (slice 2b, unit (d), decision 7): option 2, \"Enable dry-run mode and run\", which flipped the mode, saved ./hardn.yml and left it flipped when its yes was declined, is removed; origin/main's menu: testdata/legacy/run_all\n")
	b.WriteString("# Nothing reaches the mock file system or commander (checked).\n")
	b.WriteString("# ESC bytes are shown as the four characters \\x1b, and each screen line ends with $ (as cat -e shows it)\n")
	b.WriteString("# so trailing spaces stay visible; <no-newline> marks text printed without a final newline.\n")
	b.WriteString("# Everything after the line below is the screen.\n")
	b.WriteString("# ----\n")
	b.WriteString(visibleScreen(out))
	assertRenderGolden(t, "run_all/option1-live-cancel.golden", b.String())
}

// TestRunAllDryRunOnProxmox checks that Run All's dry run says what the
// live run does on a Proxmox VE host with the firewall step enabled (gate
// 6, proxmoxTreatedAsDebian): the live run refuses UFW and stops there, so
// the simulation says the step is refused and that nothing after it runs,
// and simulates neither the firewall nor DNS. On Debian it simulates both.
func TestRunAllDryRunOnProxmox(t *testing.T) {
	const (
		refused    = "Would refuse this step: UFW is not used on Proxmox VE hosts, where pve-firewall manages the packet filter"
		stops      = "Hardening would stop here, so the steps after it would not run"
		firewallOK = "Would configure firewall to allow SSH on port 2230"
		dnsOK      = "Would configure nameservers: 192.0.2.53"
		liveFailed = "System hardening failed: UFW is not used on Proxmox VE hosts"
	)
	for _, tc := range []struct {
		name    string
		pve     bool
		dryRun  bool
		script  string
		want    []string
		notWant []string
	}{
		{name: "proxmox dry run", pve: true, dryRun: true, script: "1\n\n", want: []string{refused, stops}, notWant: []string{firewallOK, dnsOK}},
		{name: "proxmox live run, as the dry run says", pve: true, script: "1\nyes\n\n", want: []string{liveFailed}, notWant: []string{"DNS settings applied"}},
		{name: "debian dry run", dryRun: true, script: "1\n\n", want: []string{firewallOK, dnsOK}, notWant: []string{refused, stops}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, fs, mc := newRunAllMenuOn(t, tc.dryRun, tc.pve)
			m.config.EnableUfwSshPolicy = true
			m.config.ConfigureDns = true
			m.config.Nameservers = []string{"192.0.2.53"}
			scripted(t, tc.script)
			out, code, ended := session(t, m.Show)
			if ended {
				t.Fatalf("ended hardn with %d:\n%s", code, out)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("printed no %q:\n%s", w, out)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(out, w) {
					t.Errorf("printed %q:\n%s", w, out)
				}
			}
			if !tc.dryRun {
				return
			}
			// The dry run only reads: the Proxmox check's probes, nothing else.
			if len(fs.Files) > 0 {
				t.Errorf("the dry run wrote %d files", len(fs.Files))
			}
			for _, c := range mc.ExecutedCommands {
				if !strings.HasPrefix(c, "dpkg-query -W ") {
					t.Errorf("the dry run ran %q", c)
				}
			}
		})
	}
}

// TestRunAllNamesHardnsLog: Run All's closing line names the file hardn writes,
// whatever hardn.yml's logFile says (decision 6 (a); defect 21).
func TestRunAllNamesHardnsLog(t *testing.T) {
	t.Chdir(t.TempDir())
	m, _, _ := newRunAllMenu(t, false)
	m.config.LogFile = "/srv/log/elsewhere.log"
	scripted(t, "1\nyes\n\n")
	out, code, ended := session(t, m.Show)
	if ended {
		t.Fatalf("ended hardn with %d:\n%s", code, out)
	}
	plain := style.StripAnsi(out)
	if !strings.Contains(plain, "Check the log file at /var/log/hardn.log for details") {
		t.Errorf("does not name /var/log/hardn.log:\n%s", plain)
	}
	if strings.Contains(plain, m.config.LogFile) {
		t.Errorf("names hardn.yml's logFile:\n%s", plain)
	}
}
