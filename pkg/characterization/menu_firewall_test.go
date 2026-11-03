package characterization

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// The Firewall screen (main menu 4) and its Manage UFW Application
// Profiles sub-screen, refactor plan slice 4's evidence: each option on a
// script, in a child process of this test binary, as TestLegacyMenuDryRun
// runs firewall_configure_dry_run, which is not repeated here. The mock
// commander answers ufw as a fwUFW says, and the real file system under a
// temp dir stands for /. What hardn -w runs is recorded by
// TestOperationGoldens (ops/configure_secure_firewall/*.golden), and its
// refusal under -n by cmd/hardn TestDryRunRefusesOperations.

// fwChild and fwDir name, in a child process, the case it runs and the
// temp dir that stands for /.
const (
	fwChild = "HARDN_CHARACTERIZATION_FIREWALL"
	fwDir   = "HARDN_CHARACTERIZATION_FIREWALL_DIR"
)

// fwUFW is the ufw the mock commander stands for. A command that succeeds
// changes it, so a redraw reads what the command did: enable, disable,
// reset (which clears the rules) and allow (which adds them). `ufw reset`
// without --force and with no terminal probably aborts on a real host
// (V9, [I]); the fixture takes it as done.
type fwUFW struct {
	missing     bool     // which ufw fails
	active      bool     // ufw status says active
	rules       []string // the lines under ufw status's header
	statusFails bool     // ufw status fails
	verbatim    string   // what ufw status prints, until a command changes it
}

// status is ufw status as printed by ufw [I, ufw source, not run here]:
// the plain form holds no "Default:" line; only ufw status verbose does.
func (u *fwUFW) status() string {
	if u.verbatim != "" {
		return u.verbatim
	}
	if !u.active {
		return "Status: inactive\n"
	}
	if len(u.rules) == 0 {
		return "Status: active\n"
	}
	return "Status: active\n\n" + fwRuleHeader + strings.Join(u.rules, "\n") + "\n\n"
}

func (u *fwUFW) words() string {
	switch {
	case u.missing:
		return "not installed"
	case u.statusFails:
		return "installed, ufw status failing"
	case u.verbatim != "":
		return "active, rules in ufw status verbose's text"
	case u.active && len(u.rules) > 0:
		return fmt.Sprintf("active, rule lines: %d", len(u.rules))
	case u.active:
		return "active, no rules"
	case len(u.rules) > 0:
		return fmt.Sprintf("inactive, rule lines: %d", len(u.rules))
	}
	return "inactive, no rules"
}

const fwRuleHeader = "To                         Action      From\n--                         ------      ----\n"

func fwRule(to, action, from string) string {
	return fmt.Sprintf("%-27s%-12s%s", to, action, from)
}

// apply changes u as command argv, which succeeded, changes ufw.
func (u *fwUFW) apply(argv []string) {
	joined := strings.Join(argv, " ")
	switch {
	case joined == "sh -c yes | ufw enable":
		u.active = true
	case joined == "ufw disable":
		u.active = false
	case joined == "ufw reset":
		u.rules = nil
	case len(argv) > 3 && argv[0] == "ufw" && argv[1] == "allow" && argv[2] == "from":
		name := argv[len(argv)-1]
		u.rules = append(u.rules, fwRule(name, "ALLOW", "Anywhere"), fwRule(name+" (v6)", "ALLOW", "Anywhere (v6)"))
	case len(argv) > 2 && argv[0] == "ufw" && argv[1] == "allow":
		u.rules = append(u.rules, fwRule(argv[2], "ALLOW", "Anywhere"), fwRule(argv[2]+" (v6)", "ALLOW", "Anywhere (v6)"))
	default:
		return
	}
	u.verbatim = ""
}

// fwCase is one run of the screen: what it shows, the lines typed, and
// where it starts.
type fwCase struct {
	name, screen, script string
	ufw                  fwUFW
	port                 int // the configured SSH port; 2230 if 0
	profiles             bool
	dryRun               bool
	proxmox              bool
	seeds                map[string]string // files under /
	fails                map[string]string // commands that fail, with what they say
	notes, defects       []string
}

func fwProfiles() []config.UfwAppProfile {
	return []config.UfwAppProfile{
		{Name: "WebServer", Title: "Web Server", Description: "Public web", Ports: []string{"80/tcp", "443/tcp"}},
		{Name: "Metrics", Title: "Metrics", Description: "Node exporter", Ports: []string{"9100/tcp"}},
	}
}

// fwCases are the screen's options.
func fwCases() []fwCase {
	const (
		ssh22Line  = "Port 22\n"
		wiped      = "The sub-screen redraws at once, with no pause, and its first bytes clear the terminal (firewall_menu.go:373, :386, :398; \\x1b[H\\x1b[2J below): on a terminal this message is gone before it can be read."
		redraw     = "Item 1 draws the screen again by calling Show from inside itself and, with no return after it, falls through (firewall_menu.go:241, :304): leaving by 0 pauses once more, \"Press Enter to return to the main menu\", each item 1 adding a pause. Each draw runs which ufw and ufw status."
		noPlain    = "A rule reaches the list only if ufw status's text holds \"deny (incoming)\" and \"allow (outgoing)\" (ufw_firewall_repository.go:93-94, firewall_menu.go:86). The repository asks `ufw status` (:55); those words are in `ufw status verbose`'s Default line, which nothing here asks for [I, ufw source, not run]."
		configOnly = "The screen's SSH Port is cfg.SshPort (firewall_menu.go:75): nothing reads sshd's configuration or what it listens on; the call log holds no read."
		v9Words    = "V9: ufw default, ufw disable and ufw reset run before the allow rule, and only cfg.SshPort is allowed, not the effective sshd port."
		noConfirm  = "Item 1 and item 2 take no confirmation: disable, reset and the enable run on the one key (firewall_menu.go:210-227, :263). Only disabling asks, and disabling is a stub."
		v10Trunk   = "On origin/main ufw ran here (V10); the trunk's P0.8 gate refuses before any ufw command (ufw_firewall_repository.go:104-106)."
		stub       = "This item prints that it is not implemented and calls nothing (firewall_menu.go:186, :202, :573)."
		noApply    = "Profiles reach ufw only through item 1 or 2 of the screen: adding or removing one runs no ufw command, and removing one leaves its allow rule and /etc/ufw/applications.d/hardn as they were."
		planRefuse = "Refused under plan only since slice 2b (firewall_menu.go:423-431): origin/main saved ./hardn.yml under dry run."
		hidden     = "The menu hides this item here, and the switch still takes it (firewall_menu.go:243, :283)."
	)
	inactive, active := fwUFW{}, fwUFW{active: true}
	withRules := fwUFW{active: true, rules: []string{
		fwRule("22/tcp", "ALLOW", "Anywhere"), fwRule("22/tcp (v6)", "ALLOW", "Anywhere (v6)"),
	}}
	missing := fwUFW{missing: true}
	verbose := fwUFW{active: true, verbatim: "Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n\n" + fwRuleHeader +
		fwRule("22/tcp", "ALLOW IN", "Anywhere") + "\n" + fwRule("23/tcp", "DENY IN", "192.0.2.10") + "\n" + fwRule("80/tcp", "LIMIT IN", "Anywhere") + "\n\n"}
	byComment := fwUFW{active: true, rules: []string{
		fwRule("22/tcp", "ALLOW", "Anywhere") + " # deny (incoming) allow (outgoing)",
	}}
	sshd22 := map[string]string{"/etc/ssh/sshd_config": ssh22Line}
	const add = "3\n1\n"
	return []fwCase{
		// Open and leave.
		{name: "open_not_installed", screen: "open, ufw not installed, q", script: "q\n", ufw: missing,
			notes: []string{"With ufw missing the menu offers 1 (Install UFW) and 0; the status still says Inactive."}},
		{name: "open_inactive", screen: "open, ufw installed and inactive, q", script: "q\n", ufw: inactive},
		{name: "open_active_no_rule", screen: "open, ufw active with no rule, q", script: "q\n", ufw: active},
		{name: "open_active_rules_never_listed", screen: "open, ufw active with rules, as ufw status prints them, q", script: "q\n", ufw: withRules,
			defects: []string{"With rules active the screen says \"No firewall rules configured\". " + noPlain}},
		{name: "open_active_rules_listed_if_status_is_verbose", screen: "open, ufw active with rules, the mock answering ufw status with verbose's text, q", script: "q\n", ufw: verbose,
			notes: []string{"What it takes for rules to reach the list: this text, which `ufw status` does not print [I]. " + noPlain, "Allow is marked with a check, deny with a cross, anything else with a bullet (firewall_menu.go:88-95)."}},
		{name: "open_active_rule_listed_by_its_comment", screen: "open, ufw active with one rule whose comment holds the two phrases, q", script: "q\n", ufw: byComment,
			notes: []string{"The check is a substring of the whole text, so a rule's comment passes it; hardn's own rule's comment is \"SSH access\" (firewall_manager.go:47)."}},
		{name: "open_status_fails_reads_inactive", screen: "open, ufw installed, ufw status failing, q", script: "q\n", ufw: fwUFW{statusFails: true},
			defects: []string{"A failing ufw status reads as Inactive: the repository drops the error and always returns nil (ufw_firewall_repository.go:56, :98), so \"Error getting firewall status\" (firewall_menu.go:45) cannot show."}},
		{name: "open_port_22_warns", screen: "open, configured SSH port 22, q", script: "q\n", ufw: inactive, port: 22},
		{name: "open_port_2230_while_sshd_says_22", screen: "open, configured SSH port 2230, sshd_config holding Port 22, q", script: "q\n", ufw: inactive, seeds: sshd22,
			defects: []string{"\"Using non-standard port (good security)\" while sshd's file says 22. " + configOnly}},
		{name: "open_profiles", screen: "open, two profiles configured, q", script: "q\n", ufw: inactive, profiles: true},

		// Item 1.
		{name: "item1_not_installed_stub", screen: "1 (Install UFW), ufw not installed, then 0, Enter", script: "1\n\n0\n\n", ufw: missing,
			defects: []string{"Install UFW says it is not implemented. " + stub}, notes: []string{redraw}},
		{name: "item1_not_installed_dry_run", screen: "1 (Install UFW) in dry-run mode, ufw not installed, then 0, Enter", script: "1\n\n0\n\n", ufw: missing, dryRun: true,
			defects: []string{"The plan says it would install a package the live path cannot install (firewall_menu.go:182, :186)."}, notes: []string{redraw}},
		{name: "item1_inactive_enable_runs_configure", screen: "1 (Enable firewall), ufw inactive, then 0, Enter", script: "1\n\n0\n\n", ufw: inactive,
			defects: []string{noConfirm, v9Words, "\"Enable firewall\" (\"Start UFW and set to run at boot\") runs the whole Configure sequence, ufw disable and ufw reset among them (firewall_menu.go:227)."},
			notes:   []string{redraw}},
		{name: "item1_inactive_dry_run", screen: "1 (Enable firewall) in dry-run mode, ufw inactive, then 0, Enter", script: "1\n\n0\n\n", ufw: inactive, dryRun: true,
			defects: []string{"\"Would enable UFW\" names none of the commands the live path runs (firewall_menu.go:213, :227)."}, notes: []string{redraw}},
		{name: "item1_active_yes_stub", screen: "1 (Disable firewall), ufw active, y, then 0, Enter", script: "1\ny\n\n0\n\n", ufw: active,
			defects: []string{"Disable firewall warns, asks, then says it is not implemented; ufw stays active. " + stub + " FirewallManager.DisableFirewall exists and nothing here calls it (firewall_manager.go:96)."},
			notes:   []string{redraw}},
		{name: "item1_active_yes_dry_run", screen: "1 (Disable firewall) in dry-run mode, ufw active, y, then 0, Enter", script: "1\ny\n\n0\n\n", ufw: active, dryRun: true,
			notes: []string{redraw}},
		{name: "item1_active_no", screen: "1 (Disable firewall), ufw active, n, then 0, Enter", script: "1\nn\n\n0\n\n", ufw: active,
			notes: []string{redraw}},

		// Item 2. Its dry run is firewall_configure_dry_run.golden (TestLegacyMenuDryRun).
		{name: "item2_allows_configured_port_not_sshds", screen: "2 (Configure firewall), ufw inactive, sshd_config holding Port 22", script: "2\n\n", ufw: inactive, seeds: sshd22,
			defects: []string{v9Words + " The sshd_config seeded here says 22 and is not read: on this host the sequence would leave 22 closed. " + noConfirm},
			notes:   []string{"\"Default policy\" and \"SSH allowed\" under the success line are printed from cfg and constants, not read back from ufw (firewall_menu.go:272-273)."}},
		{name: "item2_active_rules_are_reset", screen: "2 (Configure firewall), ufw active with a rule on 22", script: "2\n\n", ufw: withRules,
			defects: []string{"An active firewall is turned off and its rules are reset before the one allow rule returns. " + v9Words + " " + noConfirm}},
		{name: "item2_profiles", screen: "2 (Configure firewall) with two profiles", script: "2\n\n", ufw: inactive, profiles: true,
			defects: []string{v9Words}},
		{name: "item2_not_installed_hidden_still_runs", screen: "2 (Configure firewall), ufw not installed", script: "2\n\n", ufw: missing,
			notes: []string{hidden}},
		{name: "item2_fails_part_way", screen: "2 (Configure firewall), ufw active with a rule on 22, the allow rule failing", script: "2\n\n", ufw: withRules,
			fails:   map[string]string{`ufw allow 2230/tcp comment SSH access`: "exit status 1"},
			defects: []string{"The allow rule fails after disable and reset: the firewall stays off with no rule, default deny set for when it next starts; the screen says the rule failed and not that the firewall is off. Nothing restores what was there (ufw_firewall_repository.go:123-142). V9."}},

		// Proxmox.
		{name: "proxmox_open_says_nothing", screen: "open on Proxmox VE, ufw installed and inactive, q", script: "q\n", ufw: inactive, proxmox: true,
			defects: []string{"The open screen does not say that ufw is not used here: it offers Enable and Configure and warns Inactive (the screen has no Proxmox branch; firewall_menu.go holds no IsProxmox). V10."}},
		{name: "proxmox_item1_refused", screen: "1 (Enable firewall) on Proxmox VE, ufw inactive, then 0, Enter", script: "1\n\n0\n\n", ufw: inactive, proxmox: true,
			notes: []string{v10Trunk, redraw}},
		{name: "proxmox_item2_refused", screen: "2 (Configure firewall) on Proxmox VE, ufw inactive", script: "2\n\n", ufw: inactive, proxmox: true,
			notes: []string{v10Trunk}},
		{name: "proxmox_active_disable_stub", screen: "1 (Disable firewall) on Proxmox VE, ufw active, y, then 0, Enter", script: "1\ny\n\n0\n\n", ufw: active, proxmox: true,
			defects: []string{"The way out on a host where origin/main enabled ufw is not on the screen: DisableFirewall is not refused on Proxmox (ufw_firewall_repository.go:289-291), and item 1 is the stub. V10."}, notes: []string{redraw}},

		// Item 3, the sub-screen.
		{name: "profiles_none", screen: "3, no profiles, 0, 0", script: "3\n0\n0\n", ufw: inactive},
		{name: "profiles_two", screen: "3, two profiles, 0, 0", script: "3\n0\n0\n", ufw: inactive, profiles: true},
		{name: "profiles_not_installed_hidden_still_runs", screen: "3, ufw not installed, 0, 0", script: "3\n0\n0\n", ufw: missing,
			notes: []string{hidden}},
		{name: "profiles_hidden_items_none_to_act_on", screen: "3, no profiles, 2, 3, 0, 0", script: "3\n2\n3\n0\n0\n", ufw: inactive,
			defects: []string{wiped}},
		{name: "profiles_invalid", screen: "3, 7 (no such option), 0, 0", script: "3\n7\n\n0\n0\n", ufw: inactive},
		{name: "profiles_quit_q", screen: "3, q, then 0", script: "3\nq\n0\n", ufw: inactive,
			notes: []string{"q leaves the sub-screen for the screen, not the program (firewall_menu.go:363, :285-286)."}},
		{name: "profiles_add", screen: "3, 1, four answers, 0, 0", script: add + "Api\nInternal API\nInternal service\n8080/tcp, 8443/tcp\n0\n0\n", ufw: inactive,
			defects: []string{wiped, "The save goes to ./hardn.yml in the working directory, not the file hardn loaded (firewall_menu.go:497), and writes the whole configuration."},
			notes:   []string{noApply}},
		{name: "profiles_add_empty_name", screen: "3, 1, an empty name, 0, 0", script: add + "\n0\n0\n", ufw: inactive,
			defects: []string{wiped}},
		{name: "profiles_add_duplicate_name", screen: "3, 1, a name held already in another case, 0, 0", script: add + "webserver\n0\n0\n", ufw: inactive, profiles: true,
			defects: []string{wiped}},
		{name: "profiles_add_empty_ports", screen: "3, 1, three answers and empty ports, 0, 0", script: add + "Api\nInternal API\nInternal service\n\n0\n0\n", ufw: inactive,
			defects: []string{wiped}},
		{name: "profiles_add_bad_port_after_all_answers", screen: "3, 1, four answers, a port with no protocol, 0, 0", script: add + "Api\nInternal API\nInternal service\n8080\n0\n0\n", ufw: inactive,
			defects: []string{wiped, "The port is checked only after all four answers, and a refusal drops all four (firewall_menu.go:476-483)."}},
		{name: "profiles_add_port_check_is_a_slash", screen: "3, 1, four answers, a port that is not one but holds a slash, 0, 0", script: add + "Api\nInternal API\nInternal service\nnot-a-port/nothing\n0\n0\n", ufw: inactive,
			defects: []string{wiped, "A port is checked for a slash and nothing else (firewall_menu.go:478): this one is saved, and would reach /etc/ufw/applications.d/hardn."}},
		{name: "profiles_remove_yes", screen: "3, 2, 1, y, 0, 0", script: "3\n2\n1\ny\n0\n0\n", ufw: inactive, profiles: true,
			defects: []string{wiped}, notes: []string{noApply}},
		{name: "profiles_remove_no", screen: "3, 2, 1, n, 0, 0", script: "3\n2\n1\nn\n0\n0\n", ufw: inactive, profiles: true,
			defects: []string{wiped}},
		{name: "profiles_remove_bad_number", screen: "3, 2, 9, 0, 0", script: "3\n2\n9\n0\n0\n", ufw: inactive, profiles: true,
			defects: []string{wiped}},
		{name: "profiles_apply_stub", screen: "3, 3 (Apply profiles), 0, 0", script: "3\n3\n0\n0\n", ufw: inactive, profiles: true,
			defects: []string{"Apply profiles says it is not implemented. " + stub, wiped}},
		{name: "profiles_apply_dry_run", screen: "3, 3 (Apply profiles) in dry-run mode, 0, 0", script: "3\n3\n0\n0\n", ufw: inactive, profiles: true, dryRun: true,
			defects: []string{"The plan says it would write /etc/ufw/applications.d/hardn; the live path writes nothing (firewall_menu.go:566, :573).", wiped}},
		{name: "profiles_add_plan_only_refused", screen: "3, 1 in dry-run mode, 0, 0", script: "3\n1\n\n0\n0\n", ufw: inactive, dryRun: true,
			notes: []string{planRefuse}},
		{name: "profiles_remove_plan_only_refused", screen: "3, 2 in dry-run mode, 0, 0", script: "3\n2\n\n0\n0\n", ufw: inactive, profiles: true, dryRun: true,
			notes: []string{planRefuse}},

		// Keys.
		{name: "invalid_key", screen: "7 (no such option), then 0", script: "7\n\n0\n", ufw: inactive, notes: []string{"The invalid key redraws by calling Show and returns (firewall_menu.go:300-301): no extra pause."}},
		{name: "return_0", screen: "0", script: "0\n", ufw: inactive},
		{name: "quit_q", screen: "q", script: "q\n", ufw: inactive,
			notes: []string{"q here does what 0 does: back to the main menu (firewall_menu.go:171-173, :289-291)."}},
	}
}

// TestLegacyMenuFirewall records origin/main's Firewall screen, each option
// in a child process of this test binary whose stdin is the script, not a
// terminal, and whose working directory and HOME are a temp dir; a second
// temp dir stands for /. Each golden holds the screen as printed (ESC as
// \x1b), the calls the repository made in order, ufw as the fixture ends,
// and ./hardn.yml as written. The trunk differs from origin/main in these
// files by three things: the pauses say "Press Enter" (DEV-050), add and
// remove are refused under plan only (slice 2b), and ufw changes are
// refused on Proxmox VE (V10). The screen is built as main_menu.go:748 and
// service_factory.go:113-121 build it.
func TestLegacyMenuFirewall(t *testing.T) {
	if name := os.Getenv(fwChild); name != "" {
		runFirewallChild(name, os.Getenv(fwDir))
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range fwCases() {
		t.Run(tc.name, func(t *testing.T) {
			dir, root := t.TempDir(), t.TempDir()
			for name, content := range tc.seeds {
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.proxmox {
				if err := os.MkdirAll(filepath.Join(root, "etc", "pve"), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestLegacyMenuFirewall$")
			cmd.Env = append(os.Environ(), fwChild+"="+tc.name, fwDir+"="+root, "NO_COLOR=", "RUNEWIDTH_EASTASIAN=0", "HARDN_CONFIG=", "HOME="+filepath.Join(dir, "home"))
			cmd.Dir = dir
			cmd.Stdin = strings.NewReader(tc.script)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("child: %v\n%s", err, out)
			}
			screen, trailer, _ := strings.Cut(string(out), childTrailer)

			port := tc.port
			if port == 0 {
				port = 2230
			}
			var b strings.Builder
			b.WriteString(baselineHeader + "\n")
			b.WriteString("# Generated by TestLegacyMenuFirewall (pkg/characterization); regenerate with -update. Do not edit.\n")
			fmt.Fprintf(&b, "# Screen: Firewall (main menu 4) %s (pkg/menu), run in a child process\n", tc.screen)
			fmt.Fprintf(&b, "# Script: stdin %q, not a terminal, so nothing typed is echoed\n", tc.script)
			host := "Debian 13"
			if tc.proxmox {
				host = "Proxmox VE on Debian 13 (/etc/pve there, osdetect saying so)"
			}
			fmt.Fprintf(&b, "# Start: %s; ufw %s; cfg.SshPort %d; %d profiles; dryRun %t\n", host, tc.ufw.words(), port, len(tc.startProfiles()), tc.dryRun)
			fmt.Fprintf(&b, "# Pauses: %d (\"Press Enter to ...\"; origin/main says \"Press any key to ...\", DEV-050)\n", strings.Count(screen, "Press Enter to"))
			for _, d := range tc.defects {
				fmt.Fprintf(&b, "# KNOWN-DEFECT: %s\n", d)
			}
			for _, n := range tc.notes {
				fmt.Fprintf(&b, "# NOTE: %s\n", n)
			}
			b.WriteString("# The screen as printed, ESC as the four characters \\x1b, each line ending in $; <no-newline> marks text with no final newline.\n")
			b.WriteString("# ----\n")
			b.WriteString(visible(screen))
			b.WriteString("# ----\n")
			b.WriteString(trailer)
			fmt.Fprintf(&b, "# Files under / afterwards: %s\n", fwFiles(t, root))
			if saved, err := os.ReadFile(filepath.Join(dir, "hardn.yml")); err != nil {
				b.WriteString("# ./hardn.yml: not written\n")
			} else {
				b.WriteString("# ./hardn.yml as written:\n")
				b.Write(saved)
			}
			got := strings.ReplaceAll(strings.ReplaceAll(b.String(), root, "$ROOT"), dir, "$CWD")
			for _, d := range []string{dir, root} {
				if resolved, err := filepath.EvalSymlinks(d); err == nil && strings.Contains(got, resolved) {
					t.Fatalf("golden would contain the temp dir %s", resolved)
				}
			}
			assertGolden(t, "menu/firewall_"+tc.name+".golden", got)
		})
	}
}

func (tc fwCase) startProfiles() []config.UfwAppProfile {
	if tc.profiles {
		return fwProfiles()
	}
	return nil
}

// fwFiles lists the files under root, by hardn's paths.
func fwFiles(t *testing.T, root string) string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, strings.TrimPrefix(path, root))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		return "none"
	}
	slices.Sort(files)
	return strings.Join(files, ", ")
}

// runFirewallChild runs the screen of case name, in the child process,
// over root: hardn's defaults for alice, the FirewallManager built as
// infrastructure.ServiceFactory builds it, over the real file system under
// root and the mock commander, both recorded. Then it reports, after
// childTrailer, what the repository did and ufw as it ended.
func runFirewallChild(name, root string) {
	var tc fwCase
	for _, c := range fwCases() {
		if c.name == name {
			tc = c
		}
	}
	if tc.name == "" || root == "" {
		fmt.Fprintf(os.Stderr, "no firewall case %q in %q\n", name, root)
		os.Exit(2)
	}
	cfg := config.DefaultConfig()
	cfg.Username, cfg.SshPort, cfg.DryRun = "alice", 2230, tc.dryRun
	if tc.port != 0 {
		cfg.SshPort = tc.port
	}
	cfg.UfwAppProfiles = tc.startProfiles()

	ufw := tc.ufw
	mc := interfaces.NewMockCommander()
	if ufw.missing {
		mc.CommandErrors["which ufw"] = fmt.Errorf("exit status 1")
	}
	if ufw.statusFails {
		mc.CommandErrors["ufw status"] = fmt.Errorf("exit status 1")
	}
	for command, message := range tc.fails {
		mc.CommandErrors[command] = fmt.Errorf("%s", message)
	}
	mc.CommandOutputs["ufw status"] = []byte(ufw.status())

	log := &trace{}
	commander := &recordingCommander{inner: mc, log: log, afterExec: func(argv []string) {
		if mc.CommandErrors[strings.Join(argv, " ")] == nil {
			ufw.apply(argv)
			mc.CommandOutputs["ufw status"] = []byte(ufw.status())
		}
	}}
	fsys := recordingFS{inner: fwRoot(root), log: log}
	pve := secondary.NewProxmoxHost(fsys, commander, "debian", tc.proxmox)
	osInfo := model.OSInfo{Type: "debian", Version: "13", Codename: "trixie", IsProxmox: tc.proxmox}
	firewall := application.NewFirewallManager(service.NewFirewallServiceImpl(secondary.NewUFWFirewallRepository(fsys, commander, pve), osInfo))
	mm := application.NewMenuManager(nil, nil, firewall, nil, nil, nil, nil)
	menu.NewFirewallMenu(mm, cfg, &osdetect.OSInfo{OsType: "debian", OsVersion: "13", OsCodename: "trixie", IsProxmox: tc.proxmox}).Show()

	fmt.Print(childTrailer)
	fmt.Printf("# The repository's calls, in order:\n%s", log)
	fmt.Printf("# ufw as the fixture ends: %s\n", ufw.words())
}

// fwRoot is the real file system under a temp dir standing for /.
type fwRoot string

var _ interfaces.FileSystem = fwRoot("")

func (r fwRoot) at(name string) string { return filepath.Join(string(r), name) }

func (r fwRoot) ReadFile(name string) ([]byte, error) { return os.ReadFile(r.at(name)) }
func (r fwRoot) WriteFile(name string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(r.at(name), data, perm)
}
func (r fwRoot) MkdirAll(path string, perm fs.FileMode) error { return os.MkdirAll(r.at(path), perm) }
func (r fwRoot) Stat(name string) (os.FileInfo, error)        { return os.Stat(r.at(name)) }
func (r fwRoot) Remove(name string) error                     { return os.Remove(r.at(name)) }
func (r fwRoot) RemoveAll(path string) error                  { return os.RemoveAll(r.at(path)) }
