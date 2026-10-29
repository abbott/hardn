package main

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The Environment mocks (environment.go; slice 9, unit (a)). Nothing here
// runs a command or writes a file outside the fake.

// envCells are the listing goldens' cells: the core set, and LANG=C's.
func envCells() []uitest.Cell { return append(uitest.Core(), asciiColour) }

// TestEnvironmentGolden pins each listing mock on the core set and under
// LANG=C: the screen on each host, origin/main's screen, item 1 under
// each doas treatment, the guide and the refusals. The golden files are
// testdata/golden/environment/<mock>.golden.
func TestEnvironmentGolden(t *testing.T) {
	for _, m := range envMocks() {
		uitest.Golden(t, "environment/"+m.name, envCells(), func(env term.Env) string {
			var out, errs bytes.Buffer
			args := append(lookArgs(t, env), m.name)
			if code := run(append([]string{envPrefix}, args...), nil, offTerminal, &out, &errs); code != exitOK || errs.Len() > 0 {
				t.Errorf("%s: exit %d, stderr %q", m.name, code, errs.String())
			}
			return out.String()
		})
	}
}

// TestPlayEnvironmentGolden plays the Environment mocks end to end and
// pins each transcript: on Debian 13 the guide, then HARDN_CONFIG kept
// (plan, y/N, apply, the screen after), also under LANG=C; the same under
// sudo-rs; already kept, an empty plan; the toggle stopping it; a root
// login, Proxmox VE and doas-refuse refused before asking; plan only,
// stopping at the plan; origin/main's file; and the hosts' menu, to the
// overridden doas host. Each change id's random suffix is written
// xxxxxxxx (sessionIDs).
func TestPlayEnvironmentGolden(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"environment-keep", []string{"environment/debian13"}, []string{"2", "1", "y", "0"}, envCells()},
		{"environment-sudo-rs", []string{"environment/ubuntu2604"}, []string{"1", "y", "0"}, nil},
		{"environment-kept", []string{"environment/kept"}, []string{"1", "0"}, nil},
		{"environment-kept-toggle", []string{"environment/kept-toggle"}, []string{"1", "y", "0"}, nil},
		{"environment-root-login", []string{"environment/root-login"}, []string{"1", "0"}, nil},
		{"environment-pve9", []string{"environment/pve9"}, []string{"1", "0"}, nil},
		{"environment-plan-only", []string{"-sequence", "dry-run", "environment/debian13"}, []string{"1", "0"}, nil},
		{"environment-doas-refuse", []string{"environment/alpine323-refuse"}, []string{"1", "0"}, nil},
		{"environment-legacy", []string{"environment/legacy"}, []string{"1", "y", "0"}, nil},
		{"environment-hosts", []string{"environment"}, []string{"6", "0", "0"}, nil},
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

// envHostNamed is the Environment mocks' host named name.
func envHostNamed(t *testing.T, name string) envHost {
	t.Helper()
	eh, ok := findEnvHost(name)
	if !ok {
		t.Fatalf("no environment host %q", name)
	}
	return eh
}

// readEnvHost reads the host named name, fresh, as the screen does, and
// returns the reading and the host.
func readEnvHost(t *testing.T, name string) (envReading, *fakeHost) {
	t.Helper()
	eh := envHostNamed(t, name)
	h := eh.host()
	return readEnv(context.Background(), h.system(), eh.env, eh.t), h
}

// playEnv plays the Environment screen on h, eh's host, plain, the user
// typing lines, a dry run with dryRun, and returns the session, the
// TestEnvHosts holds each host's screen to what real code reads of it:
// the tool sudoers.Choose picks from detect's facts (sudo, sudo-rs's
// flavour, doas, none), who ran hardn, whether hardn's files keep
// HARDN_CONFIG and origin/main's line does, the files read after hardn's,
// whether item 1 refuses before asking, and whether its plan is
// lockout-risk.
func TestEnvHosts(t *testing.T) {
	tests := []struct {
		host                string
		tool                sudoers.Tool
		user                string
		kept, legacy        bool
		later               []string
		refused, risky, row bool // row: the state row says Kept
	}{
		{host: "debian13", tool: sudoers.ToolSudo, user: "alice"},
		{host: "ubuntu2604", tool: sudoers.ToolSudo, user: "alice"},
		{host: "alpine323", tool: sudoers.ToolDoas, user: "alice", risky: true},
		{host: "alpine323-refuse", tool: sudoers.ToolDoas, user: "alice", refused: true},
		{host: "alpine323-cloud", tool: sudoers.ToolDoas, user: "alice", later: []string{"/etc/doas.d/wheel.conf"}, risky: true},
		{host: "doas-overridden", tool: sudoers.ToolDoas, user: "alice", kept: true, later: []string{"/etc/doas.d/wheel.conf"}, row: true},
		{host: "pve9", tool: sudoers.ToolNone, refused: true},
		{host: "kept", tool: sudoers.ToolSudo, user: "alice", kept: true, row: true},
		{host: "kept-toggle", tool: sudoers.ToolSudo, user: "alice", kept: true, row: true},
		{host: "root-login", tool: sudoers.ToolSudo, refused: true},
		{host: "grant", tool: sudoers.ToolSudo, user: "alice"},
		{host: "grant-no-reader", tool: sudoers.ToolSudo, user: "alice", risky: true},
		{host: "legacy", tool: sudoers.ToolSudo, user: "alice", legacy: true, row: true},
		{host: "legacy-hardn-only", tool: sudoers.ToolSudo, user: "alice", legacy: true},
	}
	if len(tests) != len(envHosts()) {
		t.Fatalf("%d hosts tested, %d hosts", len(tests), len(envHosts()))
	}
	for _, tt := range tests {
		r, _ := readEnvHost(t, tt.host)
		switch {
		case r.choice.Tool != tt.tool || r.user != tt.user:
			t.Errorf("%s: tool %v, user %q; want %v, %q", tt.host, r.choice.Tool, r.user, tt.tool, tt.user)
		case r.kept != tt.kept || r.legacyKeeps != tt.legacy || !slices.Equal(r.later, tt.later):
			t.Errorf("%s: kept %v, origin/main's line %v, later %q", tt.host, r.kept, r.legacyKeeps, r.later)
		case (r.refusal() != nil) != tt.refused || r.risky() != tt.risky:
			t.Errorf("%s: refused %v, lockout risk %v", tt.host, r.refusal() != nil, r.risky())
		}
		var under string
		for _, kv := range r.rows() {
			if kv.Key == r.underKey() {
				under = kv.Value
			}
		}
		if (under == keptWord) != tt.row {
			t.Errorf("%s: the state row says %q", tt.host, under)
		}
	}
	if r, _ := readEnvHost(t, "ubuntu2604"); r.choice.Flavor != detect.SudoRS {
		t.Errorf("ubuntu2604: flavour %v, want sudo-rs", r.choice.Flavor)
	}
}

// TestEnvironmentReader holds the gallery's reader (currentPolicy, unit
// (b)'s) to what sudoers.PlanFor writes: on each host hardn's files read
// back as the Policy that wrote them, which plans nothing again; a file
// edited by hand is no Policy, and named.
func TestEnvironmentReader(t *testing.T) {
	for host, want := range map[string]sudoers.Policy{
		"debian13":        {User: "alice"},
		"alpine323":       {User: "alice"},
		"kept":            {User: "alice", Env: []string{hardnConfig}},
		"grant":           {User: "alice", Grant: sudoers.GrantNoPassword},
		"doas-overridden": {User: "alice", Grant: doasGrant, Env: []string{hardnConfig}},
		"legacy":          {User: "alice"},
	} {
		r, _ := readEnvHost(t, host)
		if r.err != nil || r.cur.User != want.User || r.cur.Grant != want.Grant || !slices.Equal(r.cur.Env, want.Env) {
			t.Errorf("%s: read %+v (%v), want %+v", host, r.cur, r.err, want)
			continue
		}
		if p, err := sudoers.PlanFor(r.st, []sudoers.Policy{r.cur}); err != nil || len(p.Changes) > 0 {
			t.Errorf("%s: the Policy read plans %d changes (%v)", host, len(p.Changes), err)
		}
	}
	r := readEnv(context.Background(), editedHost().system(), sudoAlice, envTreatment{})
	if !errors.Is(r.err, errEdited) || !strings.Contains(flatText(r.refusal().Render(component.NewCtx(term.Fixed(80, colorprofile.NoTTY, false, true)))),
		"/etc/sudoers.d/hardn-env-alice holds a rule hardn didn't write.") {
		t.Errorf("edited: %v, %q", r.err, r.refusal())
	}
}

// envEngine plans controls on h on the real engine, as the screen's
// TestEnvironmentGrantCarried: on the grant host, the change planned with
// the gallery's reader writes hardn-env-alice alone, no lockout risk, and
// once applied hardn-grant-alice is as it was, the two in separate files
// (V16); planned as real code can today, an env-only Policy, it removes
// TestEnvironmentDoas: on alpine323, item 1's per-user rule (doas-rule) is
// lockout-risk, confirmed by typing the hostname, and says first that it
// grants doas; doas-refuse refuses before asking, building no engine, as
// PlanFor refuses an env-only Policy on doas; on the cloud image the
// change carries the plan's finding of wheel.conf, read after hardn's
// rule, its code registered; and nothing is planned on a host already
// TestEnvironmentNoTmpNoProbe: keeping HARDN_CONFIG on Debian 13 writes
// hardn-env-alice alone outside hardn's state, its copies checked in the
// run directory, nothing under /tmp (V20); and nothing the screen does
// TestEnvironmentPlanOnlyWritesNothing: on every host, drawing the
// screen, the guide, and item 1 under plan only change nothing outside
// hardn's own state (the engine makes the plan's run directory,
// /run/hardn/<id>, and removes it: stateDirs) and run no su and no other
// command that changes the host: a change stops at its plan, an empty
// TestEnvironmentHeights: the screen on every host and origin/main's, in
// their one form (a sub-menu has none other), at 80x24 and at 80x40, in
// Unicode and ASCII: each is as many lines at either height, the prompt
// TestEnvironmentUsage: the listing and play refuse what they do not
// take, with status 2 and nothing on stdout.
func TestEnvironmentUsage(t *testing.T) {
	for _, args := range [][]string{
		{envPrefix, "nope"},
		{envPrefix, "screen-debian13", "-width", "60"},
		{"play", "environment/nope"},
		{"play", "-fail", "1", "environment/debian13"},
		{"play", "-sequence", "failure", "environment/debian13"},
	} {
		var out, errs bytes.Buffer
		if code := run(args, nil, offTerminal, &out, &errs); code != exitUsage || out.Len() > 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errs.String())
		}
	}
	var out bytes.Buffer
	if code := run([]string{envPrefix, "-list"}, nil, offTerminal, &out, &bytes.Buffer{}); code != exitOK || len(strings.Fields(out.String())) != len(envMocks()) {
		t.Errorf("-list: exit %d, %q", code, out.String())
	}
}
