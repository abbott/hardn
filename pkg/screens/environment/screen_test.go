package environment_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	envscreen "github.com/abbott/hardn/pkg/screens/environment"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The words the tests look for: decided ones (gallery-environment.md §6;
// control-environment.md §6, §7) and the screen's proposed one.
const (
	noRuleWords = "hardn has no doas rule for alice to keep HARDN_CONFIG on. Add setenv { HARDN_CONFIG } to the rule that lets alice use doas. Under doas stays Not kept by hardn."
	noToolWords = "sudo isn't installed here."
	noUserWords = "hardn wasn't run with sudo, so there's no user to keep it for. Run sudo hardn as that user."
	editedWords = "/etc/sudoers.d/hardn-env-alice holds a rule hardn didn't write. Put hardn's back, or remove it."
	// journalWords are the journal's refusal of a file of hardn's not
	// root's alone, as it is (control-environment.md §6).
	journalWords = "is not a file only root can write"
	keepSudo     = "Takes effect at the next sudo hardn."
	keepDoas     = "Takes effect at the next doas hardn."
	planOnly     = "No actual changes were made to your system."
	keepLineText = `Defaults:alice env_keep += "HARDN_CONFIG"`
)

// TestScreen: the screen on each host, as the Environment control reads
// it (decisions 1 (a), 2 (a), 3 (a), 5 (a), 6 (a), 7): the User row, or
// "Not run with sudo"; the state row in the control's words, a file that
// may override hardn's rule under a Kept row, Not Applicable with
// neither tool, Unknown where not read; Value as hardn sees it, or "Not
// set"; item 1 labelled by the tool, keeping or stopping, marked Lockout
// risk where it asks and its plan is. Drawing reads only, live and under
// plan only: nothing written or run, no engine made, nothing planned.
func TestScreen(t *testing.T) {
	tests := []struct {
		host, user, under, value, item1 string
		later                           bool
		risk                            bool
	}{
		{hostDebian, "alice", "Under sudo Not kept by hardn", "Not set", "Under sudo Keep HARDN_CONFIG", false, false},
		{hostUbuntu, "alice", "Under sudo Not kept by hardn", "Not set", "Under sudo Keep HARDN_CONFIG", false, false},
		{hostAlpine, "alice", "Under doas Not kept by hardn", "Not set", "Under doas Keep HARDN_CONFIG", false, false},
		{hostSetenv, "alice", "Under doas Not kept by hardn", keptPath, "Under doas Keep HARDN_CONFIG", false, false},
		{hostOverridden, "alice", "Under doas Kept by hardn", "Not set", "Under doas Stop keeping HARDN_CONFIG", true, true},
		{hostPVE, "", "Under sudo Not Applicable", "Not set", "Under sudo Keep HARDN_CONFIG", false, false},
		{hostKept, "alice", "Under sudo Kept by hardn", keptPath, "Under sudo Stop keeping HARDN_CONFIG", false, false},
		{hostRootLogin, "Not run with sudo", "", "Not set", "Under sudo Keep HARDN_CONFIG", false, false},
		{hostGrant, "alice", "Under sudo Not kept by hardn", "Not set", "Under sudo Keep HARDN_CONFIG", false, false},
		{hostLegacy, "alice", "Under sudo Kept by /etc/sudoers.d/alice", keptPath, "Under sudo Keep HARDN_CONFIG", false, false},
		{hostEdited, "alice", "Under sudo Unknown", "Not set", "Under sudo Keep HARDN_CONFIG", false, false},
		{hostNotRoots, "alice", "Under sudo Kept by hardn", keptPath, "Under sudo Stop keeping HARDN_CONFIG", false, false},
		{hostVisudo, "alice", "Under sudo Not kept by hardn", "Not set", "Under sudo Keep HARDN_CONFIG", false, false},
	}
	for _, tt := range tests {
		for _, dryRun := range []bool{false, true} {
			h := newHost(t, tt.host)
			n := len(h.Calls())
			p := play(h, dryRun, "0")
			s := screens(p.out)
			if p.err != nil || p.unread != 0 || len(s) != 1 {
				t.Fatalf("%s: Run = %v, %d unread, %d screens:\n%s", tt.host, p.err, p.unread, len(s), p.out)
			}
			under := "Under " + strings.Fields(tt.item1)[1]
			got := [4]string{row(s[0], "User"), "", row(s[0], "Value"), item(s[0], "1")}
			if v := row(s[0], under); v != "" {
				got[1] = under + " " + v
			}
			if got != [4]string{tt.user, tt.under, tt.value, tt.item1} {
				t.Errorf("%s: User, state, Value and item 1 %q, want %q", tt.host, got, [4]string{tt.user, tt.under, tt.value, tt.item1})
			}
			if strings.Contains(s[0], "/etc/doas.d/wheel.conf may override hardn's rule") != tt.later || strings.Contains(s[0], "Lockout risk") != tt.risk {
				t.Errorf("%s: the later file shown %t, Lockout risk %t:\n%s", tt.host, !tt.later, !tt.risk, s[0])
			}
			if !strings.Contains(s[0], "2) Guide How to set HARDN_CONFIG") && !strings.Contains(flat(s[0]), "2) Guide How to set HARDN_CONFIG") ||
				!strings.Contains(p.out, "[0-2 or q]") || strings.Contains(s[0], "Mode ") {
				t.Errorf("%s: not items 1 and 2, or a status row:\n%s", tt.host, p.out)
			}
			if w := writes(h, n); len(w) > 0 || p.engines != 0 || len(p.runs) != 0 {
				t.Errorf("%s, plan only %t: drawing wrote %v, made %d engines and %d runs", tt.host, dryRun, w, p.engines, len(p.runs))
			}
		}
	}
}

// TestKeep: item 1 keeps HARDN_CONFIG through the plan on sudo and
// sudo-rs: the review, User alice and the state the change makes, Kept by
// hardn; the plan, hardn-env-alice; "Keep HARDN_CONFIG under sudo?", y;
// the apply, hardn-env-alice alone written, 0440 root's, its copy checked
// in the run directory, nothing under /tmp, su never run; then that it
// takes effect at the next sudo hardn, and the screen drawn again, Kept
// by hardn, Value still Not set, item 1 stopping.
func TestKeep(t *testing.T) {
	for _, name := range []string{hostDebian, hostUbuntu} {
		h := newHost(t, name)
		p := play(h, false, "1", "y", "0")
		if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil {
			t.Fatalf("%s: Run = %v, %d unread, runs %+v:\n%s", name, p.err, p.unread, p.runs, p.out)
		}
		if w := inOrder(p.out, "Keep HARDN_CONFIG #", "HARDN_CONFIG", "User alice", "Under sudo Kept by hardn", "Keep HARDN_CONFIG /etc/sudoers.d/hardn-env-alice",
			"+"+keepLineText, "Keep HARDN_CONFIG under sudo? [y/N, 0 or q]: y", "Keep HARDN_CONFIG Done", "Operation Successful", keepSudo,
			"Environment Variable Settings #", "Under sudo Kept by hardn", "Value Not set", "1) Under sudo Stop keeping HARDN_CONFIG"); w != "" {
			t.Errorf("%s: %q not shown after what came before it:\n%s", name, w, p.out)
		}
		if got := changed(h, 0); !slices.Equal(got, []string{"write-atomic " + envFile}) {
			t.Errorf("%s: changed %q, want hardn-env-alice alone", name, got)
		}
		if perm, uid, gid := entry(h, envFile); !strings.Contains(content(h, envFile), keepLineText) || perm != 0o440 || uid != 0 || gid != 0 {
			t.Errorf("%s: hardn-env-alice %q, %04o %d:%d", name, content(h, envFile), perm, uid, gid)
		}
		for _, c := range h.Calls() {
			if strings.HasPrefix(c.Path, "/tmp") || c.Op == "run" && path.Base(c.Argv[0]) == "su" {
				t.Errorf("%s: %s", name, c)
			}
		}
		if strings.Count(p.out, keepSudo) != 1 {
			t.Errorf("%s: %q said %d times", name, keepSudo, strings.Count(p.out, keepSudo))
		}
	}
}

// TestStop: once hardn's files keep HARDN_CONFIG, item 1 stops keeping it
// (decision 5 (a)): "Stop Keeping HARDN_CONFIG", the review Not kept by
// hardn, hardn-env-alice removed, its backup kept and said, then that it
// takes effect at the next sudo hardn; the screen drawn again, Not kept,
// Value still as hardn sees it, item 1 keeping.
func TestStop(t *testing.T) {
	h := newHost(t, hostKept)
	p := play(h, false, "1", "y", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil {
		t.Fatalf("Run = %v, %d unread, runs %+v:\n%s", p.err, p.unread, p.runs, p.out)
	}
	if w := inOrder(p.out, "Stop Keeping HARDN_CONFIG #", "User alice", "Under sudo Not kept by hardn", "-"+keepLineText,
		"Stop keeping HARDN_CONFIG under sudo? [y/N, 0 or q]: y", "Operation Successful", "Backup of /etc/sudoers.d/hardn-env-alice kept in", keepSudo,
		"Environment Variable Settings #", "Under sudo Not kept by hardn", "Value "+keptPath, "1) Under sudo Keep HARDN_CONFIG"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if got := changed(h, 0); !slices.Equal(got, []string{"remove " + envFile}) {
		t.Errorf("changed %q, want hardn-env-alice removed", got)
	}
}

// TestGrantCarried: on the grant host item 1 writes hardn-env-alice alone,
// no lockout risk, and hardn-grant-alice is byte for byte as it was, its
// mode and owner too (V16; gallery-environment.md §7 item 20).
func TestGrantCarried(t *testing.T) {
	h := newHost(t, hostGrant)
	grant := content(h, grantFile)
	perm, uid, gid := entry(h, grantFile)
	p := play(h, false, "1", "y", "0")
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err != nil || p.runs[0].Plan.Risk == engine.RiskLockout || strings.Contains(p.out, "Type the hostname") {
		t.Fatalf("Run = %v, runs %+v:\n%s", p.err, p.runs, p.out)
	}
	if got := changed(h, 0); !slices.Equal(got, []string{"write-atomic " + envFile}) {
		t.Errorf("changed %q, want hardn-env-alice alone", got)
	}
	if p2, u2, g2 := entry(h, grantFile); content(h, grantFile) != grant || p2 != perm || u2 != uid || g2 != gid {
		t.Errorf("hardn-grant-alice after: %q %04o %d:%d", content(h, grantFile), p2, u2, g2)
	}
}

// TestLegacy: origin/main's /etc/sudoers.d/alice keeps HARDN_CONFIG, so
// the row says "Kept by /etc/sudoers.d/alice" (decision 3 (a)), and item 1
// still keeps it in hardn-env-alice, the review listing the legacy-file
// finding before the question; origin/main's file is left as it was, and
// the screen drawn again says Kept by hardn.
func TestLegacy(t *testing.T) {
	h := newHost(t, hostLegacy)
	p := play(h, false, "1", "y", "0")
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err != nil {
		t.Fatalf("Run = %v, runs %+v:\n%s", p.err, p.runs, p.out)
	}
	if w := inOrder(p.out, "Under sudo Kept by /etc/sudoers.d/alice", "1) Under sudo Keep HARDN_CONFIG", "Keep HARDN_CONFIG #", "Under sudo Kept by hardn",
		"/etc/sudoers.d/alice may also let alice use sudo", "Keep HARDN_CONFIG under sudo? [y/N, 0 or q]: y", "Operation Successful", keepSudo,
		"Under sudo Kept by hardn", "1) Under sudo Stop keeping HARDN_CONFIG"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if content(h, sudoers.DropInDir+"/alice") != legacyLines || !slices.Equal(changed(h, 0), []string{"write-atomic " + envFile}) {
		t.Errorf("origin/main's file %q; changed %q", content(h, sudoers.DropInDir+"/alice"), changed(h, 0))
	}
}

// TestLockoutRisk: on doas, stopping where hardn's rule asks alice's
// password is lockout-risk: the screen marks item 1 so, and lists the file
// read after hardn's rule under the Kept row; item 1's review lists the
// plan's finding of it, the plan hardn's boot unit first; the question is
// confirmed by typing the hostname, never y/N, a y refused and asked
// again; once typed, the rule rewritten without setenv, the revert timer
// armed and "doas hardn confirm" said, then that it takes effect at the
// next doas hardn. 0 at the hostname declines, nothing written.
func TestLockoutRisk(t *testing.T) {
	h := newHost(t, hostOverridden)
	p := play(h, false, "1", "y", shortName, "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil || p.runs[0].Plan.Risk != engine.RiskLockout {
		t.Fatalf("Run = %v, %d unread, runs %+v:\n%s", p.err, p.unread, p.runs, p.out)
	}
	if w := inOrder(p.out, "Under doas Kept by hardn", "/etc/doas.d/wheel.conf may override hardn's rule", "1) Under doas Stop keeping HARDN_CONFIG", "Lockout risk",
		"Stop Keeping HARDN_CONFIG #", "User alice", "Under doas Not kept by hardn", "/etc/doas.d/wheel.conf may override hardn's rule",
		"Install hardn's boot unit", "Lockout risk Stop keeping HARDN_CONFIG", "+permit persist alice", "Stop keeping HARDN_CONFIG under doas?",
		"Type the hostname [web-01, 0 or q]: y", "That does not match exactly.", "Type the hostname [web-01, 0 or q]: web-01",
		"Operation Successful", "This change reverts automatically", "doas hardn confirm", "Backup of /etc/doas.d/hardn-alice.conf kept in", keepDoas,
		"Environment Variable Settings #", "Under doas Not kept by hardn", "1) Under doas Keep HARDN_CONFIG", "Lockout risk"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if strings.Contains(p.out, "[y/N") {
		t.Errorf("a lockout-risk plan asked y/N:\n%s", p.out)
	}
	if got := content(h, ruleFile); got != sudoers.Header+"permit persist alice\n" {
		t.Errorf("the rule after: %q", got)
	}

	h = newHost(t, hostOverridden)
	before := content(h, ruleFile)
	p = play(h, false, "1", "0", "0")
	if p.err != nil || len(p.runs) != 1 || !p.runs[0].Declined || !strings.Contains(p.out, "Operation Cancelled") || strings.Contains(p.out, keepDoas) {
		t.Fatalf("declined: Run = %v, runs %+v:\n%s", p.err, p.runs, p.out)
	}
	if w := writes(h, 0); len(w) > 0 || content(h, ruleFile) != before {
		t.Errorf("declined: wrote %v", w)
	}
}

// TestHostname: the phrase is the kernel's name; a name too long to show
// whole beside the label is asked by its first label; one that cannot be
// read, or typed, is said when item 1 is chosen, before anything is
// planned or asked, nothing written, and the screen goes on.
func TestHostname(t *testing.T) {
	long := "web-01.a-subdomain-long-enough-not-to-show.example.net"
	h := newHost(t, hostOverridden)
	h.AddFile(header.HostnameFile, []byte(long+"\n"), 0o644, 0, 0)
	p := play(h, false, "1", "0", "0")
	if p.err != nil || !strings.Contains(p.out, "Type the hostname [web-01, 0 or q]: 0") {
		t.Errorf("a long name: Run = %v:\n%s", p.err, p.out)
	}
	for name, data := range map[string]*string{"absent": nil, "empty": new(string), "a comma": ptr("web,01\n")} {
		h := newHost(t, hostOverridden)
		if data == nil {
			if err := h.Remove(context.Background(), header.HostnameFile); err != nil {
				t.Fatal(err)
			}
		} else {
			h.AddFile(header.HostnameFile, []byte(*data), 0o644, 0, 0)
		}
		n := len(h.Calls())
		p := play(h, false, "1", "0")
		if p.err != nil || p.unread != 0 || p.engines != 0 || len(p.runs) != 0 || len(screens(p.out)) != 2 || strings.Contains(p.out, "Type the hostname") {
			t.Errorf("%s: Run = %v, %d unread, %d engines, %d runs:\n%s", name, p.err, p.unread, p.engines, len(p.runs), p.out)
		}
		if !strings.Contains(flat(p.out), "Warning Can't read the hostname to confirm with: ") {
			t.Errorf("%s: not said:\n%s", name, p.out)
		}
		if w := writes(h, n); len(w) > 0 {
			t.Errorf("%s: wrote %v", name, w)
		}
	}
	// Plan only confirms nothing, so it needs no hostname: item 1 stops at
	// its plan, as on every other host.
	h = newHost(t, hostOverridden)
	if err := h.Remove(context.Background(), header.HostnameFile); err != nil {
		t.Fatal(err)
	}
	n := len(h.Calls())
	p = play(h, true, "1", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Mode != output.ModePlan || !strings.Contains(p.out, planOnly) ||
		strings.Contains(p.out, "Can't") || strings.Contains(p.out, "Type the hostname") {
		t.Errorf("plan only, absent: Run = %v, %d unread, runs %+v:\n%s", p.err, p.unread, p.runs, p.out)
	}
	if w := writes(h, n); len(w) > 0 {
		t.Errorf("plan only, absent: wrote %v", w)
	}
}

// ptr is a pointer to s.
func ptr(s string) *string { return &s }

// TestRefusedBeforeAsking: wherever the reading refuses item 1
// (Reading.Refusal), item 1 says why before asking anything, live and
// under plan only, in the refusal's words (screenkit.RefusalWords), on
// doas then that the row stays Not kept by hardn (Reading.Stays): no
// engine made, nothing planned, written or run, and the screen drawn
// again as it was.
func TestRefusedBeforeAsking(t *testing.T) {
	for _, tt := range []struct{ host, words string }{
		{hostAlpine, noRuleWords}, {hostSetenv, noRuleWords}, {hostPVE, noToolWords}, {hostRootLogin, noUserWords},
		{hostEdited, editedWords}, {hostNotRoots, journalWords},
	} {
		for _, dryRun := range []bool{false, true} {
			h := newHost(t, tt.host)
			n := len(h.Calls())
			p := play(h, dryRun, "1", "0")
			if p.err != nil || p.unread != 0 || p.engines != 0 || len(p.runs) != 0 || len(screens(p.out)) != 2 {
				t.Fatalf("%s: Run = %v, %d unread, %d engines, %d runs:\n%s", tt.host, p.err, p.unread, p.engines, len(p.runs), p.out)
			}
			if !strings.Contains(flat(p.out), "Warning ") || !strings.Contains(flat(p.out), tt.words) || strings.Contains(p.out, "[y/N") || strings.Contains(p.out, "Type the hostname") {
				t.Errorf("%s: not refused before asking in %q:\n%s", tt.host, tt.words, p.out)
			}
			if s := screens(p.out); s[0] != s[1] {
				t.Errorf("%s: the screen drawn again differs:\n%s\n%s", tt.host, s[0], s[1])
			}
			if w := writes(h, n); len(w) > 0 {
				t.Errorf("%s, plan only %t: wrote %v", tt.host, dryRun, w)
			}
		}
	}
	// The doas refusal's last sentence is the screen's alone: once, after
	// the refusal's own words.
	p := play(newHost(t, hostAlpine), false, "1", "0")
	if strings.Count(p.out, "Under doas stays Not kept by hardn.") != 1 {
		t.Errorf("Stays said %d times:\n%s", strings.Count(p.out, "Under doas stays Not kept by hardn."), p.out)
	}
}

// TestFailedApply: visudo refusing the configuration fails the change at
// its check, before hardn-env-alice is written, the journal rolled back:
// the flow's report with its code, nothing said of the next sudo hardn,
// the host as it was, and the screen drawn again, Not kept.
func TestFailedApply(t *testing.T) {
	h := newHost(t, hostVisudo)
	p := play(h, false, "1", "y", "0")
	if p.err != nil || len(p.runs) != 1 || output.CodeOf(p.runs[0].Err) != "SUDOERS_INVALID_BEFORE" {
		t.Fatalf("Run = %v, runs %+v:\n%s", p.err, p.runs, p.out)
	}
	if w := inOrder(p.out, "Keep HARDN_CONFIG under sudo? [y/N, 0 or q]: y", "Failed SUDOERS_INVALID_BEFORE", "Operation Failed",
		"Environment Variable Settings #", "Under sudo Not kept by hardn", "1) Under sudo Keep HARDN_CONFIG"); w != "" || strings.Contains(p.out, keepSudo) {
		t.Errorf("%q not shown after what came before it, or said it takes effect:\n%s", w, p.out)
	}
	if got := changed(h, 0); len(got) > 0 || content(h, envFile) != "<absent>" {
		t.Errorf("changed %q; hardn-env-alice %q", got, content(h, envFile))
	}
}

// TestNothingWritten: n and 0 at the question decline, a dry run stops at
// the plan saying no change was made, and nothing is written but the
// engine's run directory, nor said of the next sudo hardn.
func TestNothingWritten(t *testing.T) {
	for _, tt := range []struct {
		name   string
		host   string
		dryRun bool
		lines  []string
		mode   output.Mode
	}{
		{"n", hostDebian, false, []string{"1", "n", "0"}, output.ModeApply},
		{"0 at the question", hostDebian, false, []string{"1", "0", "0"}, output.ModeApply},
		{"dry run, keep", hostDebian, true, []string{"1", "0"}, output.ModePlan},
		{"dry run, stop", hostKept, true, []string{"1", "0"}, output.ModePlan},
		{"dry run, lockout-risk", hostOverridden, true, []string{"1", "0"}, output.ModePlan},
	} {
		h := newHost(t, tt.host)
		p := play(h, tt.dryRun, tt.lines...)
		if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Mode != tt.mode {
			t.Fatalf("%s: Run = %v, %d unread, runs %+v:\n%s", tt.name, p.err, p.unread, p.runs, p.out)
		}
		if w := writes(h, 0); len(w) > 0 {
			t.Errorf("%s: wrote %v", tt.name, w)
		}
		if tt.dryRun != strings.Contains(p.out, planOnly) || !tt.dryRun && !strings.Contains(p.out, "Operation Cancelled") ||
			strings.Contains(p.out, "Takes effect") || tt.dryRun && (strings.Contains(p.out, "[y/N") || strings.Contains(p.out, "Type the hostname")) {
			t.Errorf("%s: the footer, or asked:\n%s", tt.name, p.out)
		}
	}
}

// TestGuide: item 2 says the guide, live and under plan only, writing
// nothing, and the screen is drawn again.
func TestGuide(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		h := newHost(t, hostDebian)
		p := play(h, dryRun, "2", "0")
		if p.err != nil || p.engines != 0 || len(screens(p.out)) != 2 || len(writes(h, 0)) > 0 {
			t.Fatalf("Run = %v, %d engines:\n%s", p.err, p.engines, p.out)
		}
		if w := inOrder(p.out, "Enter selection [0-2 or q]: 2", "HARDN_CONFIG names the hardn.yml hardn reads. Set it in your shell profile:",
			"export HARDN_CONFIG=/path/to/hardn.yml", "Then keep it with item 1.", "Environment Variable Settings #"); w != "" {
			t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
		}
	}
}

// TestApplyContext: the change's apply, and nothing else, runs under the
// context Options.ApplyContext makes, stopped once the apply returned; a
// cancelled one is rolled back, and the screen goes on. A declined change,
// a dry run, a refusal and the guide make none.
func TestApplyContext(t *testing.T) {
	for _, tc := range []struct {
		host   string
		dryRun bool
		lines  []string
		made   int
	}{
		{hostDebian, false, []string{"1", "n", "2", "1", "y", "0"}, 1},
		{hostDebian, true, []string{"1", "0"}, 0},
		{hostAlpine, false, []string{"1", "0"}, 0},
	} {
		h := newHost(t, tc.host)
		made, stopped := 0, 0
		p := playWith(h, tc.dryRun, func(o *envscreen.Options) {
			o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
				made++
				c, cancel := context.WithCancel(ctx)
				return c, func() { stopped++; cancel() }
			}
		}, tc.lines...)
		if p.err != nil || made != tc.made || stopped != made {
			t.Errorf("%s %q: Run = %v, %d contexts made, %d stopped, want %d:\n%s", tc.host, tc.lines, p.err, made, stopped, tc.made, p.out)
		}
	}
	h := newHost(t, hostDebian)
	p := playWith(h, false, func(o *envscreen.Options) {
		o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
			c, cancel := context.WithCancel(ctx)
			cancel()
			return c, cancel
		}
	}, "1", "y", "0")
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err == nil || strings.Contains(p.out, keepSudo) || content(h, envFile) != "<absent>" {
		t.Errorf("cancelled: Run = %v, runs %+v, hardn-env-alice %q:\n%s", p.err, p.runs, content(h, envFile), p.out)
	}
	if s := screens(p.out); len(s) != 2 || row(s[1], "Under sudo") != "Not kept by hardn" {
		t.Errorf("cancelled: the screen not drawn again as it was:\n%s", p.out)
	}
}

// TestNavigation: 0 goes back, q quits from the screen and the question,
// typed or y/N, and the end of the input aborts at the screen and at the
// question; nothing written.
func TestNavigation(t *testing.T) {
	for _, tt := range []struct {
		host  string
		lines []string
		want  error
	}{
		{hostDebian, []string{"0"}, nil}, {hostDebian, []string{"q"}, nav.ErrQuit}, {hostDebian, []string{"1", "q"}, nav.ErrQuit},
		{hostOverridden, []string{"1", "q"}, nav.ErrQuit}, {hostDebian, []string{"2", "q"}, nav.ErrQuit},
		{hostDebian, nil, prompt.ErrAborted}, {hostDebian, []string{"1"}, prompt.ErrAborted}, {hostOverridden, []string{"1"}, prompt.ErrAborted},
	} {
		h := newHost(t, tt.host)
		n := len(h.Calls())
		p := play(h, false, tt.lines...)
		if !errors.Is(p.err, tt.want) || (tt.want == nil) != (p.err == nil) {
			t.Errorf("%s %q: Run = %v, want %v", tt.host, tt.lines, p.err, tt.want)
		}
		if w := writes(h, n); len(w) > 0 {
			t.Errorf("%s %q: wrote %v", tt.host, tt.lines, w)
		}
	}
}

// TestRunNeeds: Run refuses options it cannot run with, writing nothing:
// no Prompter's In or Out, no System, Getenv or NewEngine.
func TestRunNeeds(t *testing.T) {
	h := newHost(t, hostDebian)
	n := len(h.Calls())
	var out bytes.Buffer
	in := prompt.New(strings.NewReader("0\n"), false)
	getenv := lookup(h.env)
	newEngine := func(func(engine.Event)) *engine.Engine { return nil }
	for _, o := range []envscreen.Options{
		{}, {System: h, Getenv: getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in}, System: h, Getenv: getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{Out: &out}, System: h, Getenv: getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, Getenv: getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, System: h, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, System: h, Getenv: getenv},
	} {
		if err := envscreen.Run(context.Background(), o); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Run(%+v) = %v, want fs.ErrInvalid", o, err)
		}
	}
	if out.Len() > 0 || len(h.Calls()) != n {
		t.Errorf("wrote %q, %v", out.String(), h.Calls()[n:])
	}
}
