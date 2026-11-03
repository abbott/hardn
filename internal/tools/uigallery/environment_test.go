package main

import (
	"bytes"
	"context"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/environment"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The Environment screen on the simulated hosts (environment.go; slice 9,
// unit (c)): hardn's own screen, pkg/screens/environment. Nothing here
// runs a command or writes a file outside the fake.

// TestPlayEnvironmentGolden plays the Environment screen end to end and
// pins each transcript, the sequences the mocks had
// (gallery-environment.md §3.1) on the real screen: on Debian 13 the
// guide, then HARDN_CONFIG kept (plan, y/N, apply, the screen after),
// also under LANG=C; the same under sudo-rs; already kept, item 1
// stopping it; a root login and Proxmox VE refused before asking; plan
// only, stopping at the plan; hardn's grant carried; on doas the refusal,
// and again once the user followed it; stopping on doas, lockout-risk,
// the cloud image's wheel.conf reported, the hostname typed; origin/main's
// file; the hosts' menu, to the overridden doas host; and the main menu's
// 8, "Keep HARDN_CONFIG", live, the guide, and under plan only, item 1
// stopping at its plan (as hardn's main menu 8 runs the screen since
// next/s9-replace; the gallery's main menu is the fixture, its indicator
// "Mode Live" either way). Each change id's random suffix is written
// xxxxxxxx (sessionIDs).
func TestPlayEnvironmentGolden(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"environment", nil, []string{"8", "2", "0", "0"}, nil},
		{"environment-main-plan-only", []string{"-sequence", "dry-run"}, []string{"8", "1", "0", "0"}, nil},
		{"environment-keep", []string{"environment/debian13"}, []string{"2", "1", "y", "0"}, append(uitest.Core(), asciiColour)},
		{"environment-sudo-rs", []string{"environment/ubuntu2604"}, []string{"1", "y", "0"}, nil},
		{"environment-kept", []string{"environment/kept"}, []string{"1", "y", "0"}, nil},
		{"environment-root-login", []string{"environment/root-login"}, []string{"1", "0"}, nil},
		{"environment-pve9", []string{"environment/pve9"}, []string{"1", "0"}, nil},
		{"environment-plan-only", []string{"-sequence", "dry-run", "environment/debian13"}, []string{"1", "0"}, nil},
		{"environment-grant", []string{"environment/grant"}, []string{"1", "y", "0"}, nil},
		{"environment-doas-refuse", []string{"environment/alpine324"}, []string{"1", "0"}, nil},
		{"environment-doas-setenv", []string{"environment/alpine324-setenv"}, []string{"1", "0"}, nil},
		{"environment-doas-stop", []string{"environment/doas-overridden"}, []string{"1", hostName, "0"}, nil},
		{"environment-legacy", []string{"environment/legacy"}, []string{"1", "y", "0"}, nil},
		{"environment-hosts", []string{"environment"}, []string{"5", "0", "0"}, nil},
	}
	for _, tt := range tests {
		cells := tt.cells
		if cells == nil {
			cells = uitest.Core()
		}
		uitest.Golden(t, "play/"+tt.name, cells, func(env term.Env) string {
			args := append(lookArgs(t, env), tt.args...)
			code, out, errs, unread := playGallery(tt.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread", tt.name, args, code, errs, unread)
			}
			return sessionIDs(t, out)
		})
	}
}

// envHostNamed is the Environment screen's host named name.
func envHostNamed(t *testing.T, name string) envHost {
	t.Helper()
	eh, ok := findEnvHost(name)
	if !ok {
		t.Fatalf("no environment host %q", name)
	}
	return eh
}

// playEnv plays the Environment screen on h, eh's host, plain, the user
// typing lines, a dry run with dryRun, and returns the session, the
// transcript and the exit status.
func playEnv(t *testing.T, eh envHost, h *fakeHost, dryRun bool, lines ...string) (*session, string, int) {
	t.Helper()
	return playWith(t, envPrefix+"/"+eh.name, func(s *session) func() error {
		if dryRun {
			s.sequence = "dry-run"
		}
		return func() error { return s.envScreen(context.Background(), eh, h) }
	}, lines...)
}

// TestEnvironmentScreenHosts: every host the Environment screen plays on
// draws there, reading only, live and under plan only, and the guide
// writes nothing either: no engine built, no hold taken, nothing changed.
// Each is held to what the Environment control reads of it (environment.
// Read on detect's facts): the tool sudoers.Choose picks (sudo, sudo-rs's
// flavour, doas, none), who ran hardn, whether hardn's files keep
// HARDN_CONFIG and origin/main's line does, the files read after
// hardn's, whether item 1 refuses before asking and whether its plan is
// lockout-risk; and the screen's state row says it in the control's
// words, on doas as hardn's own state (the settlement of 2026-10-29).
func TestEnvironmentScreenHosts(t *testing.T) {
	tests := []struct {
		host          string
		tool          sudoers.Tool
		user          string
		kept, legacy  bool
		later         []string
		refused, risk bool
		row           string // the screen's state row, or its User row with no user
	}{
		{host: "debian13", tool: sudoers.ToolSudo, user: "alice", row: "Under sudo Not kept by hardn"},
		{host: "ubuntu2604", tool: sudoers.ToolSudo, user: "alice", row: "Under sudo Not kept by hardn"},
		{host: "alpine324", tool: sudoers.ToolDoas, user: "alice", refused: true, row: "Under doas Not kept by hardn"},
		{host: "alpine324-setenv", tool: sudoers.ToolDoas, user: "alice", refused: true, row: "Under doas Not kept by hardn"},
		{host: "doas-overridden", tool: sudoers.ToolDoas, user: "alice", kept: true, later: []string{"/etc/doas.d/zz-local.conf"}, risk: true, row: "Under doas Kept by hardn"},
		{host: "pve9", tool: sudoers.ToolNone, refused: true, row: "Under sudo Not Applicable"},
		{host: "kept", tool: sudoers.ToolSudo, user: "alice", kept: true, row: "Under sudo Kept by hardn"},
		{host: "root-login", tool: sudoers.ToolSudo, refused: true, row: "User Not run with sudo"},
		{host: "grant", tool: sudoers.ToolSudo, user: "alice", row: "Under sudo Not kept by hardn"},
		{host: "legacy", tool: sudoers.ToolSudo, user: "alice", legacy: true, row: "Under sudo Kept by /etc/sudoers.d/alice"},
	}
	if len(tests) != len(envHosts()) {
		t.Fatalf("%d hosts tested, %d hosts", len(tests), len(envHosts()))
	}
	for _, tt := range tests {
		eh := envHostNamed(t, tt.host)
		h := eh.host()
		f, err := detect.Detect(context.Background(), h.system())
		if err != nil {
			t.Fatal(err)
		}
		r := environment.Read(context.Background(), h.system(), f, lookup(eh.env))
		var later []string
		for _, fd := range r.Later() {
			later = append(later, fd.Source)
		}
		switch {
		case r.Choice.Tool != tt.tool || r.User != tt.user:
			t.Errorf("%s: tool %v, user %q; want %v, %q", tt.host, r.Choice.Tool, r.User, tt.tool, tt.user)
		case r.Kept != tt.kept || r.Legacy.Keeps != tt.legacy || !slices.Equal(later, tt.later):
			t.Errorf("%s: kept %v, origin/main's line %v, later %q", tt.host, r.Kept, r.Legacy.Keeps, later)
		case (r.Refusal() != nil) != tt.refused || r.Risk != tt.risk:
			t.Errorf("%s: refused %v, lockout risk %v", tt.host, r.Refusal() != nil, r.Risk)
		}
		if tt.host == "ubuntu2604" && r.Choice.Flavor != detect.SudoRS {
			t.Errorf("ubuntu2604: flavour %v, want sudo-rs", r.Choice.Flavor)
		}
		for _, dryRun := range []bool{false, true} {
			h := eh.host()
			s, out, code := playEnv(t, eh, h, dryRun, "2", "0")
			if code != exitOK || len(s.engineRuns) != 0 || s.holds != 0 || !strings.Contains(out, "Info: "+envPrefix+"/"+tt.host) {
				t.Errorf("%s, plan only %t: exit %d, %d engines, the hold taken %d times:\n%s", tt.host, dryRun, code, len(s.engineRuns), s.holds, out)
			}
			flat := flatText(out)
			if !strings.Contains(flat, tt.row) || !strings.Contains(flat, "Then keep it with item 1.") {
				t.Errorf("%s: the screen does not show %q, or the guide:\n%s", tt.host, tt.row, out)
			}
			if got := writesAfter(h, 0); len(got) > 0 {
				t.Errorf("%s, plan only %t: drawing the screen and the guide changed the host: %q", tt.host, dryRun, got)
			}
		}
	}
}

// TestPlayEnvironment plays item 1 on its hosts and checks what the
// transcript shows, in order, and what changed on the host outside
// hardn's own directories: keeping writes hardn-env-alice alone, the
// grant's file as it was; stopping removes it; on doas stopping installs
// hardn's boot unit, arms the revert timer and rewrites the rule, the
// hostname typed; a refusal before asking, and plan only, change nothing.
func TestPlayEnvironment(t *testing.T) {
	const env = "write-atomic /etc/sudoers.d/hardn-env-alice"
	for _, tc := range []struct {
		name, host string
		dryRun     bool
		lines      []string
		screen     []string // what the transcript shows, in order
		order      []string // what changed on the host, outside hardn's directories, in order
	}{
		{"keep", "debian13", false, []string{"1", "y", "0"},
			[]string{"Keep HARDN_CONFIG under sudo? [y/N, 0 or q]: y", "Operation Successful", "Takes effect at the next sudo hardn.", "Under sudo Kept by hardn"},
			[]string{env}},
		{"stop", "kept", false, []string{"1", "y", "0"},
			[]string{"Stop keeping HARDN_CONFIG under sudo? [y/N, 0 or q]: y", "Backup of /etc/sudoers.d/hardn-env-alice kept in", "Takes effect at the next sudo hardn.",
				"Under sudo Not kept by hardn", "Value /home/alice/hardn.yml"},
			[]string{"remove /etc/sudoers.d/hardn-env-alice"}},
		{"grant", "grant", false, []string{"1", "y", "0"}, []string{"Keep HARDN_CONFIG under sudo? [y/N, 0 or q]: y", "Operation Successful"}, []string{env}},
		{"doas stop", "doas-overridden", false, []string{"1", hostName, "0"},
			[]string{"Stop keeping HARDN_CONFIG under doas?", "Type the hostname [web-01, 0 or q]: web-01", "Operation Successful", "doas hardn confirm",
				"Takes effect at the next doas hardn.", "Under doas Not kept by hardn"},
			[]string{"write-atomic /etc/init.d/hardn-boot", "rc-update add hardn-boot default", `/bin/sh -c "sleep \"$1\" && \"$0\" rollback \"$2\"" /usr/local/bin/hardn 180 ` +
				galleryNow.Format("20060102T150405Z") + "-", "write-atomic /etc/doas.d/zz-hardn-alice.conf"}},
		{"refused", "alpine324", false, []string{"1", "0"}, []string{"Warning hardn has no doas rule for alice", "Under doas stays Not kept by hardn."}, nil},
		{"plan only", "debian13", true, []string{"1", "0"}, []string{"Keep HARDN_CONFIG", "No actual changes were made to your system.", "Under sudo Not kept by hardn"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eh := envHostNamed(t, tc.host)
			h := eh.host()
			s, out, code := playEnv(t, eh, h, tc.dryRun, tc.lines...)
			if w := notShown(out, tc.screen); w != "" {
				t.Errorf("the transcript does not show %q after what came before it:\n%s", w, out)
			}
			if code != exitOK || s.holds != 0 {
				t.Errorf("exit %d, the gallery's hold taken %d times", code, s.holds)
			}
			all := hostChanges(h)
			for i, c := range all {
				if i < len(tc.order) && strings.HasSuffix(tc.order[i], "-") && strings.HasPrefix(c, tc.order[i]) {
					all[i] = tc.order[i]
				}
			}
			if !slices.Equal(all, tc.order) {
				t.Errorf("the host changed %q, want %q", all, tc.order)
			}
			if tc.host == "grant" && h.content("/etc/sudoers.d/hardn-grant-alice") != eh.host().content("/etc/sudoers.d/hardn-grant-alice") {
				t.Errorf("hardn-grant-alice changed: %q", h.content("/etc/sudoers.d/hardn-grant-alice"))
			}
		})
	}
}

// TestEnvironmentNoTmpNoProbe: keeping HARDN_CONFIG on Debian 13 writes
// hardn-env-alice alone outside hardn's state, its copies checked in the
// run directory, nothing under /tmp (V20); and nothing the screen does
// runs su, origin/main's probe of the user's environment
// (config.go:336).
func TestEnvironmentNoTmpNoProbe(t *testing.T) {
	eh := envHostNamed(t, "debian13")
	h := eh.host()
	n := len(h.f.Calls())
	_, out, code := playEnv(t, eh, h, false, "2", "1", "y", "0")
	if code != exitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, c := range h.f.Calls()[n:] {
		switch {
		case c.Op == "run" && path.Base(c.Argv[0]) == "su":
			t.Errorf("su ran: %s", c)
		case strings.HasPrefix(c.Path, "/tmp/"):
			t.Errorf("a call under /tmp: %s", c)
		}
	}
	if _, written := changed(h.f.Calls()[n:]); !slices.Equal(written, []string{"/etc/sudoers.d/hardn-env-alice"}) {
		t.Errorf("changed %q, want hardn-env-alice alone", written)
	}
}

// TestEnvironmentPlanOnlyWritesNothing: on every host, drawing the
// screen, the guide, and item 1 under plan only change nothing outside
// hardn's own state (the engine makes the plan's run directory,
// /run/hardn/<id>, and removes it: stateDirs) and run no su and no other
// command that changes the host: the change stops at its plan, and a
// refusal builds no engine.
func TestEnvironmentPlanOnlyWritesNothing(t *testing.T) {
	for _, eh := range envHosts() {
		h := eh.host()
		n := len(h.f.Calls())
		s, out, code := playEnv(t, eh, h, true, "2", "1", "0")
		if code != exitOK {
			t.Errorf("%s: exit %d:\n%s", eh.name, code, out)
		}
		if cmds, files := changed(h.f.Calls()[n:]); len(cmds) > 0 || len(files) > 0 {
			t.Errorf("%s: plan only changed the host: %q, %q", eh.name, cmds, files)
		}
		for _, w := range writesAfter(h, n) {
			if !strings.Contains(w, " /run/hardn/") {
				t.Errorf("%s: plan only wrote %s", eh.name, w)
			}
		}
		for _, c := range h.f.Calls()[n:] {
			if c.Op == "run" && path.Base(c.Argv[0]) == "su" {
				t.Errorf("%s: su ran", eh.name)
			}
		}
		if planned := len(s.engineRuns) > 0; planned != strings.Contains(flatText(out), "No actual changes were made") {
			t.Errorf("%s: plan only did not stop at the plan, or planned a refusal:\n%s", eh.name, out)
		}
	}
}

// TestPlayEnvironmentUsage: a host that is not one, -fail and a sequence
// other than normal or dry-run stop play with exit status 2 before stdin
// is read; the Environment listing is gone; the main menu's 8 leads to
// the screen on Debian 13.
func TestPlayEnvironmentUsage(t *testing.T) {
	for _, args := range [][]string{
		{"play", "environment/nope"},
		{"play", "-fail", "1", "environment/debian13"},
		{"play", "-sequence", "failure", "environment/debian13"},
		{envPrefix},
		{envPrefix, "screen-debian13"},
	} {
		var out, errs bytes.Buffer
		read := false
		code := run(args, func() *prompt.Reader { read = true; return noInput() }, offTerminal, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 || read {
			t.Errorf("%v: exit %d, stdout %q, stderr %q, stdin read %t; want exit %d, a message, no output", args, code, out.String(), errs.String(), read, exitUsage)
		}
	}
	if envLinks[mainMenuMock+"/8"] != envMainHost {
		t.Errorf("the main menu's 8 leads to %q", envLinks[mainMenuMock+"/8"])
	}
	code, out, errs, unread := playGallery([]string{"8", "0", "0"}, "-profile", "plain")
	if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, "Info: "+envMainHost) || !strings.Contains(out, "# Environment Variable Settings #") ||
		strings.Contains(out, "The gallery has no screen for Environment") {
		t.Errorf("play, 8: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
}

// TestPlayEnvironmentStepsNotCut: no step line of an Environment sequence
// is cut (engine.TitleMax): the control's summaries fit it.
func TestPlayEnvironmentStepsNotCut(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "golden", "play", "environment*.golden"))
	if err != nil || len(files) == 0 {
		t.Fatalf("goldens: %v, %d files", err, len(files))
	}
	escape := regexp.MustCompile(`␛\[[0-9;]*m`)
	step := regexp.MustCompile(`(Pending|Running|Done|Failed|Rolled back|Skipped)$`)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			l := strings.TrimSuffix(escape.ReplaceAllString(line, ""), "$")
			if step.MatchString(l) && (strings.Contains(l, "...") || strings.Contains(l, "…")) {
				t.Errorf("%s:%d: a step line is cut: %q", filepath.Base(f), i+1, l)
			}
		}
	}
	if len(environment.SummaryStop) > engine.TitleMax {
		t.Errorf("%q is longer than engine.TitleMax", environment.SummaryStop)
	}
}
