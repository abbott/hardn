package characterization

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/menu"
)

// The Environment screen (main menu 8), refactor plan slice 9's evidence
// (docs/engine/gallery-environment.md §2): each of its options on a
// script, in a child process of this test binary, as TestLegacyMenuBackup
// ran the Backup screen. Its repository (pkg/adapter/secondary
// file_environment_repository.go) is given the real file system under
// the case's temp dir, which stands for / (rootedFS, environment_calls
// _test.go), each call recorded with hardn's path, and the mock
// commander. The screen reads HARDN_CONFIG itself, and the repository
// SUDO_USER, else USER (file_environment_repository.go:130-138); each case
// sets them. The child runs as the test's user, not root: the screen
// reads the euid only in item 1 (environment_settings_menu.go:82), whose
// root branch TestLegacyEnvironmentKeep records at the call it makes.

// envChild and envDir name, in a child process, the Environment case it
// runs and the temp dir that stands for /, as the parent wrote it.
const (
	envChild = "HARDN_CHARACTERIZATION_ENVIRONMENT"
	envDir   = "HARDN_CHARACTERIZATION_ENVIRONMENT_DIR"
)

// envCase is one run of the Environment screen: what it shows and why,
// the lines typed, the variables and the root it starts from, and the
// mode.
type envCase struct {
	name, screen, script, start string
	vars                        envVars
	seeds                       []envSeed
	dryRun                      bool
	// item1 marks a case that chooses item 1, whose branch depends on the
	// euid: the golden holds the branch for a user other than root.
	item1          bool
	notes, defects []string
}

// envCases are the Environment screen's options: open and leave with
// HARDN_CONFIG set and unset, with the variable kept or not, with
// SUDO_USER set and unset (USER, then nobody), the line commented out and
// in another file; 1, live and in dry run; 2 (the guide); an invalid key;
// 0; q.
func envCases() []envCase {
	const (
		oneFile   = "The status reads one file, /etc/sudoers.d/<SUDO_USER, else USER>, for the text `env_keep += \"HARDN_CONFIG\"` (file_environment_repository.go:110-124, :134-139): not the rules sudo applies."
		twice     = "Each draw reads the file twice: GetEnvironmentConfig checks it for its own PreserveSudo (file_environment_repository.go:141-147), which the service does not use, then checks it again (environment_service.go:53-65)."
		ownEnv    = "\"Current HARDN_CONFIG\" is hardn's own environment (environment_settings_menu.go:37): under sudo, what env_keep let through, not what the user set; nothing on the screen says which file hardn loaded."
		notReach  = "This branch is unreachable in hardn: cmd/hardn refuses a user other than root before the menu (cmd/hardn/main.go:147-152), so \"This operation requires sudo privileges.\" and \"Please run: sudo hardn setup-sudo-env\" (environment_settings_menu.go:82-84) show only here, where the test is not root."
		rootPath  = "As root item 1 calls MenuManager.SetupSudoPreservation (environment_settings_menu.go:90), recorded at that call by TestLegacyEnvironmentKeep (environment/keep_*.golden); the screen's words for its result are read (environment_settings_menu.go:91-95)."
		dryRoot   = "Under dry run, as root, item 1 prints \"[DRY-RUN] Would configure sudo to preserve HARDN_CONFIG environment variable\" and calls nothing (environment_settings_menu.go:86-87, read, not run): the dry-run check is inside the root branch, after the euid check."
		planOnly  = "Under plan only the trunk's main menu opens 8 as live: its item has no refusal (main_menu.go:747), where 1 has (pkg/menu TestPlanOnlyRefusals). hardn -n -e and -n setup-sudo-env are refused before anything runs (ops/setup_sudo_preservation/*.golden; cmd/hardn TestDryRunRefusesOperations)."
		redraw    = "Each option but 0 and q draws the screen again by calling itself (environment_settings_menu.go:101, :106, :115), one call deeper each time."
		grantKeep = "alice's file holds the grant and the line, as setup_sudo_preservation leaves it (ops/setup_sudo_preservation/debian.golden)"
	)
	sudoAlice := envVars{sudoUser: "alice", user: "root"}
	kept := withFile("/etc/sudoers.d/alice", grantLine+keepLine, 0o440)
	grant := withFile("/etc/sudoers.d/alice", grantLine, 0o440)
	return []envCase{
		{name: "environment_open", screen: "Environment (main menu 8), HARDN_CONFIG set and kept, q", script: "q\n",
			vars: envVars{sudoUser: "alice", user: "root", hardnConfig: "/home/alice/hardn.yml"}, seeds: kept, start: grantKeep,
			notes: []string{ownEnv, twice}},
		{name: "environment_open_unset", screen: "Environment (main menu 8), HARDN_CONFIG unset, the line not there, q", script: "q\n",
			vars: sudoAlice, seeds: grant, start: "alice's file holds the grant alone",
			notes: []string{ownEnv}},
		{name: "environment_open_user", screen: "Environment (main menu 8), SUDO_USER unset, USER alice, q", script: "q\n",
			vars: envVars{user: "alice"}, seeds: kept, start: grantKeep,
			notes: []string{"With SUDO_USER unset the user is USER (file_environment_repository.go:135-138), alice here, as under su (no -) from alice, which keeps USER [I, util-linux su(1)]."}},
		{name: "environment_open_root", screen: "Environment (main menu 8), SUDO_USER unset, USER root, q", script: "q\n",
			vars: envVars{user: "root"}, seeds: kept, start: grantKeep,
			defects: []string{"With SUDO_USER unset and USER root the screen reads /etc/sudoers.d/root, and says Disabled while alice's file keeps the variable: as under doas, which sets USER to root and names its caller in DOAS_USER [I, doas(1)], or at a root login. Nothing in origin/main reads DOAS_USER.", oneFile}},
		{name: "environment_open_no_user", screen: "Environment (main menu 8), SUDO_USER and USER unset, q", script: "q\n",
			vars: envVars{}, seeds: kept, start: grantKeep,
			notes: []string{"With no user the service reads nothing and the screen says Disabled (environment_service.go:60-62)."}},
		{name: "environment_open_commented", screen: "Environment (main menu 8), the line commented out, q", script: "q\n",
			vars: sudoAlice, seeds: withFile("/etc/sudoers.d/alice", grantLine+"# "+keepLine, 0o440), start: "alice's file holds the grant and the line commented out",
			defects: []string{"A commented-out line reads Enabled: the check is a substring (file_environment_repository.go:124), and sudo reads the line as a comment [I, sudoers(5)], so the variable is not kept.", oneFile}},
		{name: "environment_open_elsewhere", screen: "Environment (main menu 8), the line in /etc/sudoers.d/hardn-env-alice, q", script: "q\n",
			vars: sudoAlice, start: "alice's file holds the grant alone; /etc/sudoers.d/hardn-env-alice holds the line, as pkg/host/sudoers writes it (sudoers.go:343-346)",
			seeds: append(withFile("/etc/sudoers.d/alice", grantLine, 0o440),
				envSeed{path: "/etc/sudoers.d/hardn-env-alice", content: "# Managed by hardn\n" + keepLine, mode: 0o440}),
			defects: []string{"The variable kept by another file reads Disabled: here the file the trunk's pkg/host/sudoers writes for it (docs/safety/sudoers.md §2); a line in /etc/sudoers or any other drop-in reads the same.", oneFile}},
		{name: "environment_keep_not_root", screen: "Environment (main menu 8), 1 (Setup sudo environment preservation), then q", script: "1\n\nq\n", item1: true,
			vars: sudoAlice, seeds: grant, start: "alice's file holds the grant alone",
			defects: []string{notReach},
			notes:   []string{rootPath, redraw}},
		{name: "environment_keep_dry_run", screen: "Environment (main menu 8) in dry-run mode, 1, then q", script: "1\n\nq\n", item1: true, dryRun: true,
			vars: sudoAlice, seeds: grant, start: "alice's file holds the grant alone",
			defects: []string{notReach},
			notes:   []string{dryRoot, planOnly}},
		{name: "environment_guide", screen: "Environment (main menu 8), 2 (Show environment variables guide), then q", script: "2\n\nq\n",
			vars: sudoAlice, seeds: grant, start: "alice's file holds the grant alone",
			defects: []string{"The guide's three headings print an empty bold span, then their words unstyled: style.Bolded(\"\", style.Blue) is the argument, the heading the format (environment_settings_menu.go:124, :129, :134)."},
			notes:   []string{"The guide's last step appends the export to ~/.bashrc (environment_settings_menu.go:135); the probe asks alice's login shell (su -, config.go:336), and Debian's and Ubuntu's stock ~/.bashrc returns early when not interactive [I, /etc/skel/.bashrc], so the line would not reach it [I, not run].", redraw}},
		{name: "environment_invalid", screen: "Environment (main menu 8), 7 (no such option), then q", script: "7\n\nq\n",
			vars: sudoAlice, seeds: grant, start: "alice's file holds the grant alone"},
		{name: "environment_return", screen: "Environment (main menu 8), 0", script: "0\n",
			vars: sudoAlice, seeds: grant, start: "alice's file holds the grant alone"},
	}
}

// TestLegacyMenuEnvironment records origin/main's Environment screen, each
// option in a child process of this test binary whose stdin is the
// script, not a terminal, and whose working directory and HOME are a temp
// dir, as TestLegacyMenuDryRun does; a second temp dir stands for /. Each
// golden holds the screen as printed (ESC as \x1b), then what the
// repository asked of the file system and the commander, and / as the
// screen left it. The pauses say "Press Enter", where origin/main says
// "Press any key" (DEV-050; environment_settings_menu.go:99, :113,
// :137); nothing else differs from origin/main in
// environment_settings_menu.go, environment_manager.go,
// environment_service.go and file_environment_repository.go, which are
// origin/main's on the trunk, on the same lines.
// main_menu.go:694 builds the screen as here, NewEnvironmentSettingsMenu
// with the main menu's MenuManager and configuration, the MenuManager's
// EnvironmentManager as service_factory.go:219-228 builds it.
func TestLegacyMenuEnvironment(t *testing.T) {
	if name := os.Getenv(envChild); name != "" {
		runEnvironmentChild(name, os.Getenv(envDir))
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range envCases() {
		t.Run(tc.name, func(t *testing.T) {
			if tc.item1 && os.Geteuid() == 0 {
				t.Skip("the golden holds item 1's branch for a user other than root (environment_settings_menu.go:82)")
			}
			dir, root := t.TempDir(), t.TempDir()
			seedRoot(t, root, tc.seeds)
			before := snapshot(t, root)

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestLegacyMenuEnvironment$")
			var env []string
			for _, kv := range os.Environ() {
				k, _, _ := strings.Cut(kv, "=")
				switch k {
				case "SUDO_USER", "SUDO_UID", "USER", "HARDN_CONFIG", "TMPDIR", "DOAS_USER":
				default:
					env = append(env, kv)
				}
			}
			for k, v := range map[string]string{"SUDO_USER": tc.vars.sudoUser, "USER": tc.vars.user, "HARDN_CONFIG": tc.vars.hardnConfig} {
				if v != "" {
					env = append(env, k+"="+v)
				}
			}
			cmd.Env = append(env, envChild+"="+tc.name, envDir+"="+root, "NO_COLOR=", "RUNEWIDTH_EASTASIAN=0", "HOME="+filepath.Join(dir, "home"))
			cmd.Dir = dir
			cmd.Stdin = strings.NewReader(tc.script)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("child: %v\n%s", err, out)
			}
			screen, trailer, _ := strings.Cut(string(out), childTrailer)

			var b strings.Builder
			b.WriteString(baselineHeader + "\n")
			b.WriteString("# Generated by TestLegacyMenuEnvironment (pkg/characterization); regenerate with -update. Do not edit.\n")
			fmt.Fprintf(&b, "# Screen: %s (pkg/menu), run in a child process\n", tc.screen)
			fmt.Fprintf(&b, "# Script: stdin %q, not a terminal, so nothing typed is echoed\n", tc.script)
			fmt.Fprintf(&b, "# Environment: %s; run as a user other than root\n", tc.vars.words())
			fmt.Fprintf(&b, "# Start: %s; dryRun %t\n", tc.start, tc.dryRun)
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
			b.WriteString("# / (the temp dir that stands for it) before:\n")
			b.WriteString(seedLines(tc.seeds))
			fmt.Fprintf(&b, "# / afterwards: %s", rootChanges(before, snapshot(t, root)))
			if _, err := os.Stat(filepath.Join(dir, "hardn.yml")); err == nil {
				b.WriteString("# ./hardn.yml: written\n")
			} else {
				b.WriteString("# ./hardn.yml: not written\n")
			}
			got := strings.ReplaceAll(strings.ReplaceAll(b.String(), root, envTmp), dir, "$CWD")
			for _, d := range []string{dir, root} {
				if resolved, err := filepath.EvalSymlinks(d); err == nil && strings.Contains(got, resolved) {
					t.Fatalf("golden would contain the temp dir %s", resolved)
				}
			}
			assertGolden(t, "menu/"+tc.name+".golden", got)
		})
	}
}

// runEnvironmentChild runs the Environment screen of case name, in the
// child process, over root: hardn's defaults for alice on port 2230, the
// EnvironmentManager built as infrastructure.ServiceFactory builds it,
// over the real file system under root, recorded, and the mock commander,
// recorded. Then it reports, after childTrailer, what the repository
// asked.
func runEnvironmentChild(name, root string) {
	var tc envCase
	for _, c := range envCases() {
		if c.name == name {
			tc = c
		}
	}
	if tc.name == "" || root == "" {
		fmt.Fprintf(os.Stderr, "no environment case %q in %q\n", name, root)
		os.Exit(2)
	}
	cfg := config.DefaultConfig()
	cfg.Username, cfg.SshPort, cfg.DryRun = "alice", 2230, tc.dryRun

	log := &trace{}
	repo := secondary.NewFileEnvironmentRepository(recordingFS{inner: rootedFS{root: root}, log: log}, &recordingCommander{inner: interfaces.NewMockCommander(), log: log})
	env := application.NewEnvironmentManager(service.NewEnvironmentServiceImpl(repo))
	mm := application.NewMenuManager(nil, nil, nil, nil, nil, nil, env, nil, nil)
	menu.NewEnvironmentSettingsMenu(mm, cfg).Show()

	fmt.Print(childTrailer)
	fmt.Printf("# The repository's calls:\n%s", log)
}
