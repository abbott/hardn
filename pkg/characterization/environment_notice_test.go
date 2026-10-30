package characterization

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
)

// config.LoadConfig's notice after its `su -` probe, refactor plan slice
// 9's evidence (docs/engine/gallery-environment.md §2): when the probe
// runs, what it runs, and when the notice prints (TestLegacyEnvLossNotice).
// The goldens are testdata/golden/environment/notice_*.

// The `su` stand-in: in TestLegacyEnvLossNotice's child, PATH holds only
// a directory where su is this test binary, which, run by that name with
// suStub set, records how it was run and answers as the case says,
// before the testing package parses its arguments.
const (
	suStub   = "HARDN_CHARACTERIZATION_SU"      // the file the stand-in appends its argv to
	suOut    = "HARDN_CHARACTERIZATION_SU_OUT"  // what it prints on stdout
	suErr    = "HARDN_CHARACTERIZATION_SU_ERR"  // what it prints on stderr
	suExit   = "HARDN_CHARACTERIZATION_SU_EXIT" // its exit status
	envNotes = "HARDN_CHARACTERIZATION_NOTICE"  // in the child, the case it runs
	envFile  = "HARDN_CHARACTERIZATION_NOTICE_FILE"
	envPeek  = "HARDN_CHARACTERIZATION_NOTICE_PEEK" // run FileDryRun, not LoadConfig
)

func init() {
	if os.Getenv(suStub) == "" || filepath.Base(os.Args[0]) != "su" {
		return
	}
	f, err := os.OpenFile(os.Getenv(suStub), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = fmt.Fprintf(f, "%s\n", formatArgv(append([]string{"su"}, os.Args[1:]...)))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(99)
	}
	fmt.Print(os.Getenv(suOut))
	fmt.Fprint(os.Stderr, os.Getenv(suErr))
	code, _ := strconv.Atoi(os.Getenv(suExit))
	os.Exit(code)
}

// noticeCase is one start of config.LoadConfig under its probe: hardn's
// variables, how the file is named, and what the probe's su answers.
type noticeCase struct {
	name, start string
	// env are the variables set; every other of noticeVars is unset.
	env map[string]string
	// viaEnv names the file in HARDN_CONFIG (env's value is replaced by
	// it), not as -f.
	viaEnv bool
	// peek runs config.FileDryRun, the only read of hardn.yml the
	// setup-sudo-env subcommand makes (cmd/hardn/main.go:455, :536), in
	// place of LoadConfig.
	peek           bool
	suOut, suErr   string
	suExit         int
	notes, defects []string
}

// noticeVars are the variables the probe reads, or that name who
// elevated hardn: unset in the child unless the case sets them.
var noticeVars = []string{"SUDO_UID", "SUDO_USER", "HARDN_CONFIG", "DOAS_USER", "USER", "TMPDIR"}

// noticeCases are LoadConfig's starts: not under sudo; under sudo with
// HARDN_CONFIG kept; lost, with alice's login shell giving it, giving
// nothing, printing a greeting with nothing, and failing; SUDO_USER unset;
// under doas; and the read setup-sudo-env makes in its place.
func noticeCases() []noticeCase {
	const (
		sudo     = "under sudo from alice"
		everyRun = "The probe runs on every LoadConfig: at start (cmd/hardn/main.go:156, after the root check) and each time the User Management screens read a user (getConfigForSSHKeyPath, os_user_repository.go:20-35, from user_menu.go:68, :155, :208 and user_options.go:329, :417), so the notice can print mid-session there (read)."
		asRoot   = "`su - alice` from root runs alice's login shell and its profile, as root's child, without a password (side-effects.md row 5) [I]; here su is a stand-in that prints the case's answer."
		flag     = "The notice prints before the file is looked for (config.go:350-356): here hardn was given -f, and the file loads."
	)
	lost := map[string]string{"SUDO_UID": "1000", "SUDO_USER": "alice", "USER": "root"}
	return []noticeCase{
		{name: "notice_not_sudo", start: "not under sudo (a root login)", env: map[string]string{"USER": "root"},
			notes: []string{"With SUDO_UID unset the probe returns at once (config.go:314-318)."}},
		{name: "notice_kept", start: sudo + ", HARDN_CONFIG kept (env_keep)", viaEnv: true, env: map[string]string{"SUDO_UID": "1000", "SUDO_USER": "alice", "USER": "root", "HARDN_CONFIG": ""},
			notes: []string{"With HARDN_CONFIG in hardn's environment the probe returns before su (config.go:321-324)."}},
		{name: "notice_lost", start: sudo + ", HARDN_CONFIG not kept; alice's login shell sets it", env: lost, suOut: "/home/alice/hardn.yml\n",
			defects: []string{asRoot, flag, everyRun},
			notes:   []string{"The notice says to run sudo hardn setup-sudo-env, which writes for SUDO_USER, alice, into the file that holds the grant (ops/setup_sudo_preservation/*.golden; environment/keep_*.golden)."}},
		{name: "notice_lost_unset", start: sudo + ", HARDN_CONFIG not kept; alice's login shell does not set it", env: lost, suOut: "\n",
			defects: []string{asRoot + " It runs although the answer changes nothing.", everyRun}},
		{name: "notice_profile_prints", start: sudo + ", HARDN_CONFIG not kept; alice's profile prints a line, the variable unset", env: lost, suOut: "Welcome to web-01\n\n",
			defects: []string{"Any output reads as the variable set: the probe checks only that su printed something (config.go:344), so a profile that prints a line gives the notice for a variable nobody set.", asRoot}},
		{name: "notice_su_fails", start: sudo + ", HARDN_CONFIG not kept; alice's shell refuses the login", env: lost, suErr: "This account is currently not available.\n", suExit: 1,
			notes: []string{"su failing reads as no loss (config.go:336-341); its stderr is not shown."}},
		{name: "notice_no_sudo_user", start: "SUDO_UID set, SUDO_USER unset", env: map[string]string{"SUDO_UID": "1000", "USER": "root"},
			notes: []string{"With SUDO_USER unset the probe returns before su (config.go:328-331)."}},
		{name: "notice_doas", start: "under doas from alice (DOAS_USER alice, USER root [I, doas(1)]), HARDN_CONFIG not kept", env: map[string]string{"DOAS_USER": "alice", "USER": "root"},
			defects: []string{"Under doas the probe never runs: it reads SUDO_UID and SUDO_USER only (config.go:314-331), so a variable doas dropped gives no notice; nor does anything in origin/main write doas's setenv (side-effects.md row 18)."}},
		{name: "notice_setup_sudo_env", start: sudo + ", HARDN_CONFIG not kept; alice's login shell sets it; the read hardn setup-sudo-env makes", env: lost, suOut: "/home/alice/hardn.yml\n", peek: true,
			notes: []string{"setup-sudo-env never calls LoadConfig: its one read of hardn.yml is config.FileDryRun, for its refusal under dryRun: true (cmd/hardn/main.go:455, :532-541; config/dryrun.go:70-82), which runs no probe and prints nothing. It has no root check either (cmd/hardn/main.go:451-475, read): the root command's is at :141-152, after the same refusal."}},
	}
}

// TestLegacyEnvLossNotice records config.LoadConfig's notice (config.go
// :348-356) and the `su -` probe before it (DetectEnvVarLoss, :312-345),
// each case in a child process of this test binary, whose PATH holds only
// a directory where su is this test binary standing in for su (init
// above), so that nothing else runs and what the probe ran is recorded.
// The child loads a hardn.yml in its temp dir, named by -f, or by
// HARDN_CONFIG when the case keeps it. Each golden holds the child's
// output as printed (the notice and the loader's lines), then what su was
// asked.
func TestLegacyEnvLossNotice(t *testing.T) {
	if name := os.Getenv(envNotes); name != "" {
		runNoticeChild(os.Getenv(envFile), os.Getenv(envPeek) != "")
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range noticeCases() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(exe, filepath.Join(bin, "su")); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, "hardn.yml")
			if err := os.WriteFile(file, []byte("username: alice\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			suLog := filepath.Join(dir, "su.log")

			env := []string{}
			for _, kv := range os.Environ() {
				k, _, _ := strings.Cut(kv, "=")
				if k != "PATH" && !slices.Contains(noticeVars, k) {
					env = append(env, kv)
				}
			}
			flagPath := file
			for _, k := range sortedKeys(tc.env) {
				v := tc.env[k]
				if k == "HARDN_CONFIG" && tc.viaEnv {
					v, flagPath = file, ""
				}
				env = append(env, k+"="+v)
			}
			env = append(env, "PATH="+bin, "HOME="+filepath.Join(dir, "home"), "NO_COLOR=", "RUNEWIDTH_EASTASIAN=0",
				envNotes+"="+tc.name, envFile+"="+flagPath, envPeek+"="+map[bool]string{true: "1"}[tc.peek],
				suStub+"="+suLog, suOut+"="+tc.suOut, suErr+"="+tc.suErr, suExit+"="+strconv.Itoa(tc.suExit))

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestLegacyEnvLossNotice$")
			cmd.Env = env
			cmd.Dir = dir
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("child: %v\n%s%s", err, out, stderr.String())
			}
			asked, err := os.ReadFile(suLog)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				t.Fatal(err)
			}

			var b strings.Builder
			fmt.Fprintf(&b, backupHeader, "TestLegacyEnvLossNotice")
			if tc.peek {
				b.WriteString("# Operation: config.FileDryRun (pkg/config/dryrun.go:70-82), run in a child process\n")
			} else {
				b.WriteString("# Operation: config.LoadConfig (pkg/config/config.go:348-367), its notice after DetectEnvVarLoss (:312-345), run in a child process\n")
			}
			fmt.Fprintf(&b, "# Start: %s\n", tc.start)
			fmt.Fprintf(&b, "# Environment: %s; PATH only the stand-in's directory\n", noticeEnvWords(tc))
			switch {
			case tc.peek:
				fmt.Fprintf(&b, "# Config: %s/hardn.yml, named by -f (FileDryRun's argument, cmd/hardn/main.go:536)\n", envTmp)
			case flagPath != "":
				fmt.Fprintf(&b, "# Config: %s/hardn.yml, named by -f (LoadConfig's argument, cmd/hardn/main.go:156)\n", envTmp)
			default:
				fmt.Fprintf(&b, "# Config: %s/hardn.yml, named by HARDN_CONFIG, no -f\n", envTmp)
			}
			if tc.suExit != 0 || tc.suErr != "" || tc.suOut != "" {
				fmt.Fprintf(&b, "# su answers (the stand-in): stdout %q, stderr %q, exit status %d\n", tc.suOut, tc.suErr, tc.suExit)
			}
			for _, d := range tc.defects {
				fmt.Fprintf(&b, "# KNOWN-DEFECT: %s\n", d)
			}
			for _, n := range tc.notes {
				fmt.Fprintf(&b, "# NOTE: %s\n", n)
			}
			printed, result, _ := strings.Cut(string(out), childTrailer)
			b.WriteString("# The output as printed, ESC as the four characters \\x1b, each line ending in $; <no-newline> marks text with no final newline.\n")
			b.WriteString("# ----\n")
			b.WriteString(visible(printed))
			b.WriteString("# ----\n")
			if s := stderr.String(); s != "" {
				fmt.Fprintf(&b, "# stderr:\n%s", visible(s))
			}
			b.WriteString(result)
			if len(asked) == 0 {
				b.WriteString("# su: not run\n")
			} else {
				fmt.Fprintf(&b, "# su as run (argv):\n%s", asked)
			}
			got := strings.ReplaceAll(b.String(), dir, envTmp)
			if resolved, err := filepath.EvalSymlinks(dir); err == nil && strings.Contains(got, resolved) {
				t.Fatalf("golden would contain the temp dir %s", resolved)
			}
			assertGolden(t, "environment/"+tc.name+".golden", got)
		})
	}
}

// noticeEnvWords says which of noticeVars a case sets, the rest unset.
func noticeEnvWords(tc noticeCase) string {
	var parts []string
	for _, k := range noticeVars {
		v, ok := tc.env[k]
		switch {
		case !ok:
			parts = append(parts, k+" unset")
		case k == "HARDN_CONFIG" && tc.viaEnv:
			parts = append(parts, k+" "+envTmp+"/hardn.yml")
		default:
			parts = append(parts, k+" "+v)
		}
	}
	return strings.Join(parts, ", ")
}

// runNoticeChild loads the configuration as cmd/hardn does, named by
// flagPath ("" for none), or with peek reads its dryRun as setup-sudo-env
// does, and says how it went.
func runNoticeChild(flagPath string, peek bool) {
	if peek {
		path, dryRun, err := config.FileDryRun(flagPath)
		fmt.Print(childTrailer)
		fmt.Printf("# FileDryRun: %s, dryRun %t, error %v\n", path, dryRun, err)
		return
	}
	cfg, err := config.LoadConfig(flagPath)
	fmt.Print(childTrailer)
	if err != nil {
		fmt.Printf("# LoadConfig: error: %v\n", err)
		return
	}
	fmt.Printf("# LoadConfig: ok, loaded %s, username %s\n", cfg.Path(), cfg.Username)
}
