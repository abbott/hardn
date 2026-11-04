package main

// The surface changes of refactor plan §3.11 and §3.14 (Phase 7, step 5;
// adopted by the maintainer 2026-11-04: "adopt all").

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/ui/term"
)

// TestDisableRootWarning: hardn.yml's disableRoot, v0.3.2's name, is read
// as disableRootSSH and warned about on stderr where the configuration
// loads, with the file and line, naming disableRootSSH; with both set,
// disableRootSSH wins, and the warning says so.
func TestDisableRootWarning(t *testing.T) {
	for _, tc := range []struct {
		name, file, words string
		want              bool
	}{
		{"disableRoot alone", "username: alice\ndisableRoot: true\n", "hardn.yml: disableRoot (line 2) is now disableRootSSH", true},
		{"both", "disableRoot: true\ndisableRootSSH: false\n",
			"hardn.yml: disableRoot (line 1) is now disableRootSSH, which this file sets too; disableRootSSH is used", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runHardnWith(t, asRoot, func() {
				onDNSHost(t)
				loadConfig = func(string) (*config.Config, error) {
					path := filepath.Join(t.TempDir(), "hardn.yml")
					if err := os.WriteFile(path, []byte(tc.file), 0o600); err != nil {
						t.Fatal(err)
					}
					c, err := config.LoadConfigWithEnvPriority(path)
					if err == nil {
						cfg = c
					}
					return c, err
				}
			}, "-n", "-r")
			if got := flat(r.stderr); !strings.Contains(got, tc.words) || strings.Count(got, "disableRoot (line") != 1 {
				t.Errorf("stderr lacks %q once:\n%s", tc.words, r.stderr)
			}
			if cfg == nil || cfg.DisableRootSSH != tc.want {
				t.Errorf("disableRootSSH read %v, want %t", cfg, tc.want)
			}
		})
	}
}

// TestVersionFlag: --version and -v print the version, the build date and
// the commit the ldflags set (main.Version, main.BuildDate,
// main.GitCommit), as v0.3.2 did, each "unknown" when the build left it
// empty, on stdout, before the root check, exit status 0; cobra's own
// "hardn version" line never.
func TestVersionFlag(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		version, date, commit string
		want                  string
	}{
		{"a release build", "0.4.0", "2026-11-04T12:00:00Z", "0123456789abcdef0123456789abcdef01234567",
			"hardn - Linux hardening tool\nVersion:    0.4.0\nBuild Date: 2026-11-04T12:00:00Z\nGit Commit: 0123456789abcdef0123456789abcdef01234567\n"},
		{"go build, no ldflags", "", "", "",
			"hardn - Linux hardening tool\nVersion:    unknown\nBuild Date: unknown\nGit Commit: unknown\n"},
	} {
		for _, flag := range []string{"--version", "-v"} {
			t.Run(tc.name+" "+flag, func(t *testing.T) {
				was := [3]string{Version, BuildDate, GitCommit}
				Version, BuildDate, GitCommit = tc.version, tc.date, tc.commit
				t.Cleanup(func() { Version, BuildDate, GitCommit = was[0], was[1], was[2] })
				r := runHardn(t, hostUnreachable, flag)
				if r.ended || r.stdout != tc.want || r.stderr != "" || len(r.reached) != 0 {
					t.Errorf("ended %v (%d), reached %v; stdout:\n%s\nstderr:\n%s", r.ended, r.code, r.reached, r.stdout, r.stderr)
				}
			})
		}
	}
}

// TestRenderFlags: --color, --ascii, --plain and --theme are the root
// command's, every command's, and give term.Detect their options
// (renderOptions), which term.Resolve weighs with HARDN_ASCII, HARDN_PLAIN,
// HARDN_THEME, NO_COLOR and TERM as its table says; --no-color is
// --color=never where --color is not given, and is warned about where
// the configuration loads, in the deprecation warnings' callout.
func TestRenderFlags(t *testing.T) {
	tty := term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Environ: []string{"TERM=xterm-256color", "LANG=en_US.UTF-8"}}
	for _, tc := range []struct {
		name    string
		args    []string
		environ []string
		check   func(term.Env) bool
	}{
		{"none: colour, Unicode, dark", nil, nil, func(e term.Env) bool { return e.HasColor() && e.Unicode && e.Dark && !e.Plain }},
		{"--color=never", []string{"--color=never"}, nil, func(e term.Env) bool { return !e.HasColor() && e.Unicode }},
		{"--no-color", []string{"--no-color"}, nil, func(e term.Env) bool { return !e.HasColor() }},
		{"--color=always over --no-color", []string{"--no-color", "--color=always"}, nil, func(e term.Env) bool { return e.HasColor() }},
		{"--color=always over NO_COLOR", []string{"--color=always"}, []string{"NO_COLOR=1"}, func(e term.Env) bool { return e.HasColor() }},
		{"NO_COLOR", nil, []string{"NO_COLOR=1"}, func(e term.Env) bool { return !e.HasColor() }},
		{"--ascii", []string{"--ascii"}, nil, func(e term.Env) bool { return !e.Unicode && e.HasColor() }},
		{"HARDN_ASCII", nil, []string{"HARDN_ASCII=1"}, func(e term.Env) bool { return !e.Unicode }},
		{"--plain", []string{"--plain"}, nil, func(e term.Env) bool { return e.Plain && !e.Unicode && !e.HasColor() && !e.Animate }},
		{"HARDN_PLAIN", nil, []string{"HARDN_PLAIN=1"}, func(e term.Env) bool { return e.Plain }},
		{"--theme=light", []string{"--theme=light"}, nil, func(e term.Env) bool { return !e.Dark && e.Theme == term.ThemeLight }},
		{"--theme=dark over HARDN_THEME", []string{"--theme=dark"}, []string{"HARDN_THEME=light"}, func(e term.Env) bool { return e.Dark }},
		{"HARDN_THEME", nil, []string{"HARDN_THEME=light"}, func(e term.Env) bool { return !e.Dark }},
		{"TERM=dumb", nil, []string{"TERM=dumb"}, func(e term.Env) bool { return !e.HasColor() && !e.Animate }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(resetFlags)
			resetFlags()
			if err := rootCmd.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			f := tty
			f.Environ = append(append([]string{}, tty.Environ...), tc.environ...)
			if e := term.Resolve(renderOptions(), f); !tc.check(e) {
				t.Errorf("%v with %v: %+v", tc.args, tc.environ, e)
			}
		})
	}
	t.Run("--no-color warned", func(t *testing.T) {
		r := runHardnWith(t, asRoot, func() { onDNSHost(t) }, "--no-color", "-n", "-r")
		if got := flat(r.stderr); strings.Count(got, "Warning ") != 1 || strings.Count(got, noColorDeprecated) != 1 {
			t.Errorf("stderr:\n%s", r.stderr)
		}
		if r := runHardnWith(t, asRoot, func() { onDNSHost(t) }, "--color=never", "-n", "-r"); strings.Contains(r.stderr, noColorDeprecated) {
			t.Errorf("--color=never warned:\n%s", r.stderr)
		}
	})
}

// TestBadRenderFlags: a --color or --theme that is not one of its values
// is a usage error, exit status 2, before anything runs.
func TestBadRenderFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--color=sometimes", "-g"}, `invalid argument "sometimes" for "--color" flag: invalid color mode "sometimes": want auto, always or never`},
		{[]string{"--theme=blue", "-g"}, `invalid argument "blue" for "--theme" flag: invalid theme "blue": want dark, light or auto`},
		{[]string{"system-details", "--color=sometimes"}, `invalid argument "sometimes" for "--color" flag`},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			r := runStart(t, logHost(readerLog), tc.args...)
			if !r.ended || r.code != 2 || r.stdout != "" || strings.Count(r.stderr, tc.want) != 1 {
				t.Errorf("ended %v with %d, stdout %q; stderr:\n%s", r.ended, r.code, r.stdout, r.stderr)
			}
		})
	}
}

// TestStreamEnvAsksOnce: with the auto theme, the terminal is asked for
// its background by the first Env that can ask, and every later Env takes
// its answer, never asking again (themeAnswered).
func TestStreamEnvAsksOnce(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()
	themeAnswered = term.ThemeLight // as a first Env's answer leaves it
	themeMode = term.ThemeAuto
	if e := streamEnv(os.Stderr); e.Dark || e.Theme != term.ThemeLight {
		t.Errorf("a later Env: %+v, want the light answer kept", e)
	}
}

// TestConfirmTimeout: --confirm-timeout, else hardn.yml's confirmTimeout,
// else 180 s, is the engine's confirm window (engineOptions' Timeout); a
// value that is not one, or below 60 s, is a usage error, exit status 2,
// before anything runs: the flag's before the root check, the key's once
// hardn.yml loads, before the OS is detected.
func TestConfirmTimeout(t *testing.T) {
	for _, tc := range []struct {
		name, flag, key string
		want            time.Duration
		reached         int    // the host steps reached before the refusal
		refused         string // "": not refused
	}{
		{name: "the default", want: revert.DefaultTimeout},
		{name: "the key", key: "4m", want: 4 * time.Minute},
		{name: "the flag, in seconds", flag: "300", want: 5 * time.Minute},
		{name: "the flag over the key", flag: "2m", key: "4m", want: 2 * time.Minute},
		{name: "the flag below the floor", flag: "30s",
			refused: "hardn: --confirm-timeout 30s: at least 1m0s: confirm timeout not accepted (CONFIRM_TIMEOUT_INVALID)"},
		{name: "the flag not a duration", flag: "soon",
			refused: `hardn: confirm timeout "soon": seconds or a duration such as 3m: confirm timeout not accepted (CONFIRM_TIMEOUT_INVALID)`},
		{name: "the flag empty", flag: "=",
			refused: `hardn: confirm timeout "": seconds or a duration such as 3m: confirm timeout not accepted (CONFIRM_TIMEOUT_INVALID)`},
		{name: "the key not a duration", key: "soon", reached: 2,
			refused: `hardn: confirmTimeout: confirm timeout "soon": seconds or a duration such as 3m: confirm timeout not accepted (CONFIRM_TIMEOUT_INVALID)`},
		{name: "the key below the floor", key: "0", reached: 2,
			refused: "hardn: confirmTimeout 0s: at least 1m0s: confirm timeout not accepted (CONFIRM_TIMEOUT_INVALID)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"-n", "-g"}
			switch tc.flag {
			case "":
			case "=":
				args = append(args, "--confirm-timeout=")
			default:
				args = append(args, "--confirm-timeout", tc.flag)
			}
			var got time.Duration
			r := runHardnWith(t, asRoot, func() {
				h, c := newDNSHost(t, "debian13", dnsTestList)
				useDNSHost(t, h)
				c.ConfirmTimeout = tc.key
				load := loadConfig
				loadConfig = func(p string) (*config.Config, error) {
					if _, err := load(p); err != nil {
						return nil, err
					}
					return c, nil
				}
				detect := detectOS
				detectOS = func() error {
					got = engineOptions(context.Background(), h, c).Timeout
					return detect()
				}
			}, args...)
			if tc.refused != "" {
				if !r.ended || r.code != 2 || len(r.reached) != tc.reached || !strings.Contains(r.stderr, tc.refused) || r.stdout != "" {
					t.Errorf("ended %v with %d after %v, want 2 after %d steps saying %q; stderr:\n%s", r.ended, r.code, r.reached, tc.reached, tc.refused, r.stderr)
				}
				return
			}
			if got != tc.want {
				t.Errorf("Timeout = %v, want %v; stderr:\n%s", got, tc.want, r.stderr)
			}
		})
	}
}

// TestConfirmTimeoutArmed: -w's lockout-risk plan, typed on a terminal,
// arms the revert timer for --confirm-timeout's window, and the countdown
// after it: 5m, the timer's 300 s and the engine's margin; without it, the
// default 180 s.
func TestConfirmTimeoutArmed(t *testing.T) {
	for _, tc := range []struct {
		args []string
		secs int
	}{
		{nil, 180},
		{[]string{"--confirm-timeout", "5m"}, 300},
	} {
		t.Run(strings.Join(append([]string{"-w"}, tc.args...), " "), func(t *testing.T) {
			h := newFWHost(t, "debian13")
			r, s := runW(t, h, onTTY, []string{"web-01"}, nil, append([]string{"-w"}, tc.args...)...)
			if r.ended {
				t.Fatalf("ended with %d; stderr:\n%s", r.code, r.stderr)
			}
			var onActive string
			for _, c := range h.Calls() {
				for _, a := range c.Argv {
					if strings.HasPrefix(a, "--on-active=") {
						onActive = a
					}
				}
			}
			want := 0
			if _, err := fmt.Sscanf(onActive, "--on-active=%ds", &want); err != nil || want < tc.secs || want >= tc.secs+60 {
				t.Errorf("the timer armed with %q, want %ds and the margin", onActive, tc.secs)
			}
			if out := stripANSI(s.out.String()); !strings.Contains(out, "sudo hardn confirm") {
				t.Errorf("no countdown:\n%s", out)
			}
		})
	}
}
