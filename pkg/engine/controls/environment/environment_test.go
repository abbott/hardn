package environment_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/environment"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
)

// grant is a Policy of alice's with grant g and variables env.
func grant(g sudoers.Grant, env ...string) sudoers.Policy {
	return sudoers.Policy{User: "alice", Grant: g, Env: env}
}

// legacyHost is Debian 13 with origin/main's /etc/sudoers.d/alice holding
// data.
func legacyHost(t *testing.T, data string) *testHost {
	t.Helper()
	h := newHost(t, debian13)
	h.AddFile(sudoers.DropInDir+"/alice", []byte(data), 0o440, 0, 0)
	return h
}

// editedEnv is hardn-env-alice edited by hand to grant alice sudo as well,
// as the gallery's editedHost.
const editedEnv = sudoers.Header + "Defaults:alice env_keep += \"HARDN_CONFIG\"\nalice ALL=(ALL) NOPASSWD: ALL\n"

// reads reports whether every call after the first n read only.
func reads(t *testing.T, h *testHost, n int) {
	t.Helper()
	if w := writesAfter(h, n); len(w) > 0 {
		t.Errorf("wrote: %v", w)
	}
	for _, c := range h.Calls()[n:] {
		if c.Op == "run" {
			t.Errorf("ran: %v", c.Argv)
		}
	}
}

// TestRead holds Read to each host the gallery draws: the tool, who ran
// hardn, HARDN_CONFIG as hardn sees it, whether hardn's files keep it and
// origin/main's line does (a comment does not count: gallery-environment.md
// §7 item 4), the state row's words (on doas as hardn's own state, the
// settlement), the files read after hardn's rule, why item 1 is refused,
// and whether its plan is lockout-risk. Read writes and runs nothing.
func TestRead(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name        string
		host        func(t *testing.T) *testHost
		env         map[string]string
		tool        sudoers.Tool
		user, value string
		kept        bool
		legacy      string // Legacy.Path
		value2      string // the state row: value and note
		later       []string
		code        string // Refusal's code
		risk        bool
	}{
		{name: "debian13", host: func(t *testing.T) *testHost { return newHost(t, debian13) }, env: sudoAlice, tool: sudoers.ToolSudo, user: "alice",
			value2: "Not kept"},
		{name: "ubuntu2604", host: func(t *testing.T) *testHost { return newHost(t, ubuntu2604) }, env: sudoAlice, tool: sudoers.ToolSudo, user: "alice",
			value2: "Not kept"},
		{name: "kept", host: func(t *testing.T) *testHost {
			return newHost(t, debian13).with(t, grant(sudoers.GrantNone, "HARDN_CONFIG"))
		},
			env: sudoKept, tool: sudoers.ToolSudo, user: "alice", value: keptValue, kept: true, value2: "Kept"},
		{name: "grant", host: func(t *testing.T) *testHost { return newHost(t, debian13).with(t, grant(sudoers.GrantPassword)) },
			env: sudoAlice, tool: sudoers.ToolSudo, user: "alice", value2: "Not kept"},
		{name: "EDITOR only", host: func(t *testing.T) *testHost { return newHost(t, debian13).with(t, grant(sudoers.GrantNone, "EDITOR")) },
			env: sudoAlice, tool: sudoers.ToolSudo, user: "alice", value2: "Not kept"},
		{name: "grant at 0640", host: func(t *testing.T) *testHost {
			return newHost(t, debian13).with(t, grant(sudoers.GrantPassword)).moded(t, "/etc/sudoers.d/hardn-grant-alice", 0o640, 0, 0)
		}, env: sudoAlice, tool: sudoers.ToolSudo, user: "alice", value2: "Not kept"},
		{name: "legacy", host: func(t *testing.T) *testHost { return legacyHost(t, legacyLines) }, env: sudoKept, tool: sudoers.ToolSudo, user: "alice",
			value: keptValue, legacy: "/etc/sudoers.d/alice", value2: "Kept by /etc/sudoers.d/alice"},
		{name: "legacy, commented", host: func(t *testing.T) *testHost {
			return legacyHost(t, "alice ALL=(ALL) NOPASSWD: ALL\n# Defaults:alice env_keep += \"HARDN_CONFIG\"\n")
		}, env: sudoAlice, tool: sudoers.ToolSudo, user: "alice", legacy: "/etc/sudoers.d/alice", value2: "Not kept"},
		{name: "legacy and hardn's", host: func(t *testing.T) *testHost {
			return legacyHost(t, legacyLines).with(t, grant(sudoers.GrantNone, "HARDN_CONFIG"))
		}, env: sudoKept, tool: sudoers.ToolSudo, user: "alice", value: keptValue, kept: true, legacy: "/etc/sudoers.d/alice", value2: "Kept"},
		{name: "alpine324", host: func(t *testing.T) *testHost { return newHost(t, alpine324) }, env: doasAlice, tool: sudoers.ToolDoas, user: "alice",
			value2: "Not kept by hardn", code: "SUDOERS_ENV_NEEDS_GRANT"},
		{name: "doas, hardn's password rule", host: func(t *testing.T) *testHost { return newHost(t, alpine324).with(t, grant(sudoers.GrantPassword)) },
			env: doasAlice, tool: sudoers.ToolDoas, user: "alice", value2: "Not kept by hardn", risk: true},
		{name: "doas, hardn's nopass rule", host: func(t *testing.T) *testHost { return newHost(t, alpine324).with(t, grant(sudoers.GrantNoPassword)) },
			env: doasAlice, tool: sudoers.ToolDoas, user: "alice", value2: "Not kept by hardn"},
		{name: "doas, overridden", host: func(t *testing.T) *testHost {
			return newHost(t, alpineCloud).with(t, grant(sudoers.GrantPassword, "HARDN_CONFIG"))
		}, env: doasAlice, tool: sudoers.ToolDoas, user: "alice", kept: true, value2: "Kept by hardn", later: []string{"/etc/doas.d/wheel.conf"}, risk: true},
		{name: "pve9", host: func(t *testing.T) *testHost { return newHost(t, pve9) }, env: rootLogin, tool: sudoers.ToolNone, code: "SUDOERS_NO_TOOL"},
		{name: "root login", host: func(t *testing.T) *testHost { return newHost(t, debian13) }, env: rootLogin, tool: sudoers.ToolSudo,
			code: "ENVIRONMENT_NO_USER"},
		{name: "SUDO_USER root", host: func(t *testing.T) *testHost { return newHost(t, debian13) }, env: map[string]string{"SUDO_USER": "root"},
			tool: sudoers.ToolSudo, code: "ENVIRONMENT_NO_USER"},
		{name: "edited", host: func(t *testing.T) *testHost {
			h := newHost(t, debian13)
			h.AddFile(sudoers.DropInDir+"/hardn-env-alice", []byte(editedEnv), 0o440, 0, 0)
			return h
		}, env: sudoAlice, tool: sudoers.ToolSudo, user: "alice", code: "SUDOERS_FILE_EDITED"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.host(t)
			n := len(h.Calls())
			r := environment.Read(ctx, h, h.facts, lookup(tt.env))
			value, note := r.Words()
			var later []string
			for _, f := range r.Later() {
				later = append(later, f.Source)
			}
			switch {
			case r.Choice.Tool != tt.tool || r.User != tt.user || r.Value != tt.value:
				t.Errorf("tool %v, user %q, value %q", r.Choice.Tool, r.User, r.Value)
			case r.Kept != tt.kept || r.Legacy.Path != tt.legacy:
				t.Errorf("kept %v, legacy %+v", r.Kept, r.Legacy)
			case r.Refusal() == nil && strings.TrimSpace(value+" "+note) != tt.value2:
				t.Errorf("the state row %q %q, want %q", value, note, tt.value2)
			case !slices.Equal(later, tt.later):
				t.Errorf("later %q", later)
			case output.CodeOf(r.Refusal()) != tt.code && (tt.code != "" || r.Refusal() != nil):
				t.Errorf("refused %v (%s), want %s", r.Refusal(), output.CodeOf(r.Refusal()), tt.code)
			case r.Risk != tt.risk:
				t.Errorf("risk %v", r.Risk)
			}
			if tt.code == "SUDOERS_ENV_NEEDS_GRANT" && (r.Err != nil || r.Refused == nil) {
				t.Errorf("doas without a rule: the state read (%v), item 1 refused (%v)", r.Err, r.Refused)
			}
			reads(t, h, n)
		})
	}
	h := newHost(t, ubuntu2604)
	if r := environment.Read(ctx, h, h.facts, lookup(sudoAlice)); r.Choice.Flavor != detect.SudoRS {
		t.Errorf("ubuntu2604: flavour %v, want sudo-rs", r.Choice.Flavor)
	}
	// The Policy Read gives is hardn's files' as they are, after item 1's
	// change was built from it (stopping, here, takes HARDN_CONFIG out of
	// a copy of its variables).
	h = newHost(t, debian13).with(t, grant(sudoers.GrantPassword, "HARDN_CONFIG", "EDITOR"))
	if r := environment.Read(ctx, h, h.facts, lookup(sudoKept)); r.Policy.User != "alice" || r.Policy.Grant != sudoers.GrantPassword ||
		!slices.Equal(r.Policy.Env, []string{"HARDN_CONFIG", "EDITOR"}) || !r.Kept {
		t.Errorf("Policy %+v, kept %v", r.Policy, r.Kept)
	}
}

// TestRefusals is every refusal's words, as a screen says them
// (screenkit.RefusalWords: the Why, then the Fix): each decided word for
// word but the plural of the edited files (proposed), each a
// *steps.Refusal whose code is the registry's for the error it wraps; on
// doas with no rule of hardn's, what a screen says after it, that the row
// will not change (Reading.Stays: the settlement's, proposed), which no
// other refusal has; the errors with no words of the control's as they
// are.
func TestRefusals(t *testing.T) {
	ctx := context.Background()
	both := func(t *testing.T) *testHost {
		h := newHost(t, debian13)
		h.AddFile(sudoers.DropInDir+"/hardn-env-alice", []byte(editedEnv), 0o440, 0, 0)
		h.AddFile(sudoers.DropInDir+"/hardn-grant-alice", []byte(sudoers.Header+"alice ALL=(ALL:ALL) ALL\n"), 0o440, 0, 0)
		return h
	}
	for _, tt := range []struct {
		name  string
		host  func(t *testing.T) *testHost
		env   map[string]string
		words string
		is    error
		stays string
	}{
		{"no tool", func(t *testing.T) *testHost { return newHost(t, pve9) }, rootLogin, "sudo isn't installed here.", sudoers.ErrNoTool, ""},
		{"no user, sudo", func(t *testing.T) *testHost { return newHost(t, debian13) }, rootLogin,
			"hardn wasn't run with sudo, so there's no user to keep it for. Run sudo hardn as that user.", environment.ErrNoUser, ""},
		{"no user, doas", func(t *testing.T) *testHost { return newHost(t, alpine324) }, rootLogin,
			"hardn wasn't run with doas, so there's no user to keep it for. Run doas hardn as that user.", environment.ErrNoUser, ""},
		{"edited", func(t *testing.T) *testHost {
			h := newHost(t, debian13)
			h.AddFile(sudoers.DropInDir+"/hardn-env-alice", []byte(editedEnv), 0o440, 0, 0)
			return h
		}, sudoAlice, "/etc/sudoers.d/hardn-env-alice holds a rule hardn didn't write. Put hardn's back, or remove it.", sudoers.ErrEdited, ""},
		{"both edited", both, sudoAlice,
			"/etc/sudoers.d/hardn-grant-alice and /etc/sudoers.d/hardn-env-alice hold rules hardn didn't write. Put hardn's back, or remove them.", sudoers.ErrEdited, ""},
		{"doas, no rule of hardn's", func(t *testing.T) *testHost { return newHost(t, alpine324) }, doasAlice,
			"hardn has no doas rule for alice to keep HARDN_CONFIG on. Add setenv { HARDN_CONFIG } to the rule that lets alice use doas.",
			sudoers.ErrEnvNeedsGrant, "Under doas stays Not kept by hardn"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.host(t)
			r := environment.Read(ctx, h, h.facts, lookup(tt.env))
			var rf *steps.Refusal
			switch err := r.Refusal(); {
			case !errors.As(err, &rf) || !errors.Is(err, tt.is):
				t.Fatalf("%v, want a Refusal of %v", err, tt.is)
			case rf.Code != output.CodeOf(err):
				t.Errorf("code %s, the registry's %s", rf.Code, output.CodeOf(err))
			case screenkit.RefusalWords(err) != tt.words:
				t.Errorf("words %q\nwant  %q", screenkit.RefusalWords(err), tt.words)
			case r.Stays() != tt.stays:
				t.Errorf("Stays %q, want %q", r.Stays(), tt.stays)
			}
			// Plan refuses it alike, before anything changes: the keep, and
			// the stop but where the state was read.
			n := len(h.Calls())
			_, err := environment.Keep{Getenv: lookup(tt.env)}.Plan(ctx, h, engine.Input{Facts: h.facts})
			if !errors.Is(err, tt.is) || screenkit.RefusalWords(err) != tt.words {
				t.Errorf("Plan: %v", err)
			}
			_, err = environment.Keep{Getenv: lookup(tt.env), Stop: true}.Plan(ctx, h, engine.Input{Facts: h.facts})
			if stopped := err == nil; stopped != errors.Is(tt.is, sudoers.ErrEnvNeedsGrant) {
				t.Errorf("Plan, stopping: %v", err)
			}
			reads(t, h, n)
		})
	}
	for _, tt := range []struct {
		name string
		host func(t *testing.T) *testHost
		env  map[string]string
		is   error
	}{
		{"a user name not written into a rule", func(t *testing.T) *testHost { return newHost(t, debian13) }, map[string]string{"SUDO_USER": "Alice"}, sudoers.ErrUserName},
		{"no includedir", func(t *testing.T) *testHost {
			h := newHost(t, debian13)
			h.AddFile(sudoers.MainConfig, []byte("root ALL=(ALL:ALL) ALL\n"), 0o440, 0, 0)
			return h
		}, sudoAlice, sudoers.ErrNotIncluded},
		{"both tools on Alpine", func(t *testing.T) *testHost {
			h := newHost(t, alpine324)
			h.facts.Elevation.Sudo = detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}
			return h
		}, doasAlice, sudoers.ErrAmbiguous},
		{"a symlink at origin/main's file", func(t *testing.T) *testHost {
			h := newHost(t, debian13)
			h.AddSymlink(sudoers.DropInDir+"/alice", "/etc/os-release")
			return h
		}, sudoAlice, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.host(t)
			r := environment.Read(ctx, h, h.facts, lookup(tt.env))
			var rf *steps.Refusal
			if r.Err == nil || errors.As(r.Err, &rf) || tt.is != nil && !errors.Is(r.Err, tt.is) {
				t.Errorf("%v, want %v as it is", r.Err, tt.is)
			}
			if _, err := (environment.Keep{Getenv: lookup(tt.env)}).Plan(ctx, h, engine.Input{Facts: h.facts}); !errors.Is(err, r.Err) && err.Error() != r.Err.Error() {
				t.Errorf("Plan: %v, Read: %v", err, r.Err)
			}
		})
	}
	// hardn-env-alice with hardn's bytes, but not a file only root can
	// write: the state read, Kept, and item 1's change refused before it
	// asks, as Plan refuses it (the journal's check of the plan,
	// sudoers.Plan.Check through steps.Sudoers), the error as it is.
	for _, tt := range []struct {
		name     string
		perm     fs.FileMode
		uid, gid int
	}{
		{"another owner", 0o440, 1000, 1000},
		{"group-writable", 0o660, 0, 0},
	} {
		t.Run("hardn-env-alice, "+tt.name, func(t *testing.T) {
			h := newHost(t, debian13).with(t, grant(sudoers.GrantNone, "HARDN_CONFIG")).moded(t, "/etc/sudoers.d/hardn-env-alice", tt.perm, tt.uid, tt.gid)
			n := len(h.Calls())
			r := environment.Read(ctx, h, h.facts, lookup(sudoKept))
			var rf *steps.Refusal
			if r.Err != nil || !r.Kept || r.Refused == nil || errors.As(r.Refused, &rf) || output.CodeOf(r.Refused) != "PLAN_INVALID" {
				t.Fatalf("err %v, kept %v, refused %v (%s)", r.Err, r.Kept, r.Refused, output.CodeOf(r.Refused))
			}
			if _, err := (environment.Keep{Getenv: lookup(sudoKept), Stop: true}).Plan(ctx, h, engine.Input{Facts: h.facts}); err == nil || err.Error() != r.Refused.Error() {
				t.Errorf("Plan: %v, Read: %v", err, r.Refused)
			}
			reads(t, h, n)
		})
	}
	// hardn-grant-alice with hardn's bytes, but not a file only root can
	// write: the change leaves the grant's file out (decision 16), not out
	// of the journal's check, so item 1 is refused before it asks, as it
	// was while the change rewrote the grant's file.
	for _, tt := range []struct {
		name     string
		perm     fs.FileMode
		uid, gid int
	}{
		{"another owner", 0o440, 1000, 0},
		{"group-writable", 0o460, 0, 0},
	} {
		t.Run("hardn-grant-alice, "+tt.name, func(t *testing.T) {
			h := newHost(t, debian13).with(t, grant(sudoers.GrantPassword)).moded(t, "/etc/sudoers.d/hardn-grant-alice", tt.perm, tt.uid, tt.gid)
			n := len(h.Calls())
			r := environment.Read(ctx, h, h.facts, lookup(sudoAlice))
			var rf *steps.Refusal
			if r.Err != nil || r.Kept || r.Refused == nil || errors.As(r.Refused, &rf) || output.CodeOf(r.Refused) != "PLAN_INVALID" {
				t.Fatalf("err %v, kept %v, refused %v (%s)", r.Err, r.Kept, r.Refused, output.CodeOf(r.Refused))
			}
			if _, err := (environment.Keep{Getenv: lookup(sudoAlice)}).Plan(ctx, h, engine.Input{Facts: h.facts}); err == nil || err.Error() != r.Refused.Error() {
				t.Errorf("Plan: %v, Read: %v", err, r.Refused)
			}
			reads(t, h, n)
		})
	}
}

// TestCheck is the state on each host, with its detail and evidence:
// Configured where hardn's files keep HARDN_CONFIG, or origin/main's line
// does; Not Configured where neither does; on doas each as hardn's own
// state; Not Applicable where no tool is installed or no user ran hardn
// through it; Unknown, with the error, where the tool or a file of
// hardn's cannot be read. Check writes and runs nothing.
func TestCheck(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name     string
		host     func(t *testing.T) *testHost
		env      map[string]string
		state    engine.State
		detail   string
		evidence []string
		findings []string
		err      error
	}{
		{"debian13", func(t *testing.T) *testHost { return newHost(t, debian13) }, sudoAlice, engine.NotConfigured, "Not kept",
			[]string{"SUDO_USER: alice", "/etc/sudoers.d/hardn-env-alice: absent"}, nil, nil},
		{"kept, two variables", func(t *testing.T) *testHost {
			return newHost(t, debian13).with(t, grant(sudoers.GrantNone, "HARDN_CONFIG", "EDITOR"))
		}, sudoKept, engine.Configured, "Kept", []string{"SUDO_USER: alice", "/etc/sudoers.d/hardn-env-alice: keeps HARDN_CONFIG EDITOR"}, nil, nil},
		{"EDITOR only", func(t *testing.T) *testHost { return newHost(t, debian13).with(t, grant(sudoers.GrantNone, "EDITOR")) }, sudoAlice,
			engine.NotConfigured, "Not kept", []string{"SUDO_USER: alice", "/etc/sudoers.d/hardn-env-alice: keeps EDITOR"}, nil, nil},
		{"legacy", func(t *testing.T) *testHost { return legacyHost(t, legacyLines) }, sudoKept, engine.Configured, "Kept by /etc/sudoers.d/alice",
			[]string{"SUDO_USER: alice", "/etc/sudoers.d/hardn-env-alice: absent", "/etc/sudoers.d/alice: keeps HARDN_CONFIG"},
			[]string{"SUDOERS_LEGACY_FILE /etc/sudoers.d/alice: /etc/sudoers.d/alice may still let alice use sudo"}, nil},
		{"legacy, commented", func(t *testing.T) *testHost {
			return legacyHost(t, "# Defaults:alice env_keep += \"HARDN_CONFIG\"\n")
		}, sudoAlice, engine.NotConfigured, "Not kept",
			[]string{"SUDO_USER: alice", "/etc/sudoers.d/hardn-env-alice: absent", "/etc/sudoers.d/alice: does not keep HARDN_CONFIG"},
			[]string{"SUDOERS_LEGACY_FILE /etc/sudoers.d/alice: /etc/sudoers.d/alice may still let alice use sudo"}, nil},
		{"alpine324", func(t *testing.T) *testHost { return newHost(t, alpine324) }, doasAlice, engine.NotConfigured, "Not kept by hardn",
			[]string{"DOAS_USER: alice", "/etc/doas.d/hardn-alice.conf: absent"}, nil, nil},
		{"doas, hardn's rule", func(t *testing.T) *testHost { return newHost(t, alpine324).with(t, grant(sudoers.GrantPassword)) }, doasAlice,
			engine.NotConfigured, "Not kept by hardn", []string{"DOAS_USER: alice", "/etc/doas.d/hardn-alice.conf: keeps nothing"}, nil, nil},
		{"doas, overridden", func(t *testing.T) *testHost {
			return newHost(t, alpineCloud).with(t, grant(sudoers.GrantPassword, "HARDN_CONFIG"))
		}, doasAlice, engine.Configured, "Kept by hardn", []string{"DOAS_USER: alice", "/etc/doas.d/hardn-alice.conf: keeps HARDN_CONFIG"},
			[]string{"SUDOERS_LATER_FILE /etc/doas.d/wheel.conf: /etc/doas.d/wheel.conf may override hardn's rule"}, nil},
		{"pve9", func(t *testing.T) *testHost { return newHost(t, pve9) }, rootLogin, engine.NotApplicable, "sudo isn't installed here",
			[]string{"sudoers: no elevation tool is installed"}, nil, nil},
		{"root login", func(t *testing.T) *testHost { return newHost(t, debian13) }, rootLogin, engine.NotApplicable, "Not run with sudo",
			[]string{"SUDO_USER: unset"}, nil, nil},
		{"root login, doas", func(t *testing.T) *testHost { return newHost(t, alpine324) }, map[string]string{"DOAS_USER": "root"},
			engine.NotApplicable, "Not run with doas", []string{"DOAS_USER: root"}, nil, nil},
		{"edited", func(t *testing.T) *testHost {
			h := newHost(t, debian13)
			h.AddFile(sudoers.DropInDir+"/hardn-env-alice", []byte(editedEnv), 0o440, 0, 0)
			return h
		}, sudoAlice, engine.Unknown, "", []string{"SUDO_USER: alice"}, nil, sudoers.ErrEdited},
		{"sudo not established", func(t *testing.T) *testHost {
			h := newHost(t, debian13)
			h.facts.Elevation.Sudo = detect.Tool{Present: detect.Unknown, Err: fs.ErrPermission}
			return h
		}, sudoAlice, engine.Unknown, "", nil, nil, fs.ErrPermission},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.host(t)
			n := len(h.Calls())
			st, err := environment.Keep{Getenv: lookup(tt.env)}.Check(ctx, h, h.facts)
			var fds []string
			for _, f := range st.Findings {
				fds = append(fds, f.Code+" "+f.Source+": "+f.Detail)
				if e, ok := output.Lookup(f.Code); !ok || e.Kind != output.KindFinding {
					t.Errorf("finding %s: not a registered finding", f.Code)
				}
			}
			switch {
			case st.State != tt.state || st.Detail != tt.detail:
				t.Errorf("%v %q, want %v %q", st.State, st.Detail, tt.state, tt.detail)
			case (err == nil) != (tt.err == nil) || tt.err != nil && !errors.Is(err, tt.err):
				t.Errorf("error %v, want %v", err, tt.err)
			case tt.evidence != nil && !slices.Equal(st.Evidence, tt.evidence):
				t.Errorf("evidence %q", st.Evidence)
			case len(st.Evidence) == 0:
				t.Error("no evidence")
			case !slices.Equal(fds, tt.findings):
				t.Errorf("findings %q", fds)
			}
			if r := environment.Read(ctx, h, h.facts, lookup(tt.env)); r.State() != st.State {
				t.Errorf("Reading.State %v, Check's %v", r.State(), st.State)
			}
			reads(t, h, n)
		})
	}
}

// TestPlan is item 1's change on each host, both ways (decision 5 (a)):
// the user's Policy with HARDN_CONFIG added or taken out, the grant and
// the other variables carried; on sudo hardn-env-alice alone, never
// lockout-risk; on doas hardn's rule rewritten, lockout-risk for one that
// asks a password; nothing to change plans nothing; sudoers' findings on
// the change. Plan writes and runs nothing.
func TestPlan(t *testing.T) {
	ctx := context.Background()
	env := func(list string) string {
		return sudoers.Header + "Defaults:alice env_keep += \"" + list + "\"\n"
	}
	for _, tt := range []struct {
		name     string
		host     func(t *testing.T) *testHost
		env      map[string]string
		stop     bool
		target   string // "" plans nothing
		after    string // the target's content, its last line added in the diff; "" removed
		risk     engine.RiskClass
		findings []string
	}{
		{"keep, debian13", func(t *testing.T) *testHost { return newHost(t, debian13) }, sudoAlice, false,
			"/etc/sudoers.d/hardn-env-alice", env("HARDN_CONFIG"), engine.RiskPlain, nil},
		{"keep, ubuntu2604", func(t *testing.T) *testHost { return newHost(t, ubuntu2604) }, sudoAlice, false,
			"/etc/sudoers.d/hardn-env-alice", env("HARDN_CONFIG"), engine.RiskPlain, nil},
		{"stop, debian13: nothing", func(t *testing.T) *testHost { return newHost(t, debian13) }, sudoAlice, true, "", "", engine.RiskPlain, nil},
		{"stop, kept", func(t *testing.T) *testHost {
			return newHost(t, debian13).with(t, grant(sudoers.GrantNone, "HARDN_CONFIG"))
		}, sudoKept, true,
			"/etc/sudoers.d/hardn-env-alice", "", engine.RiskPlain, nil},
		{"keep, kept: nothing", func(t *testing.T) *testHost {
			return newHost(t, debian13).with(t, grant(sudoers.GrantNone, "HARDN_CONFIG"))
		}, sudoKept, false,
			"", "", engine.RiskPlain, nil},
		{"keep, kept, the file's mode another: nothing", func(t *testing.T) *testHost {
			h := newHost(t, debian13).with(t, grant(sudoers.GrantNone, "HARDN_CONFIG"))
			h.AddFile("/etc/sudoers.d/hardn-env-alice", []byte(env("HARDN_CONFIG")), 0o644, 0, 0)
			return h
		}, sudoKept, false, "", "", engine.RiskPlain, nil},
		{"keep, a password grant carried", func(t *testing.T) *testHost { return newHost(t, debian13).with(t, grant(sudoers.GrantPassword)) }, sudoAlice, false,
			"/etc/sudoers.d/hardn-env-alice", env("HARDN_CONFIG"), engine.RiskPlain, nil},
		// The grant's file at another mode or owner, its bytes hardn's: the
		// grant is carried, so its file is not the change's (slice 5's).
		{"keep, a password grant at 0640: hardn-env-alice alone", func(t *testing.T) *testHost {
			return newHost(t, debian13).with(t, grant(sudoers.GrantPassword)).moded(t, "/etc/sudoers.d/hardn-grant-alice", 0o640, 0, 0)
		}, sudoAlice, false, "/etc/sudoers.d/hardn-env-alice", env("HARDN_CONFIG"), engine.RiskPlain, nil},
		{"stop, a NOPASSWD grant at 0644: hardn-env-alice alone", func(t *testing.T) *testHost {
			return newHost(t, debian13).with(t, grant(sudoers.GrantNoPassword, "HARDN_CONFIG")).moded(t, "/etc/sudoers.d/hardn-grant-alice", 0o644, 0, 0)
		}, sudoKept, true, "/etc/sudoers.d/hardn-env-alice", "", engine.RiskPlain, nil},
		{"stop, a NOPASSWD grant carried", func(t *testing.T) *testHost {
			return newHost(t, debian13).with(t, grant(sudoers.GrantNoPassword, "HARDN_CONFIG"))
		}, sudoKept, true, "/etc/sudoers.d/hardn-env-alice", "", engine.RiskPlain, nil},
		{"stop, another variable carried", func(t *testing.T) *testHost {
			return newHost(t, debian13).with(t, grant(sudoers.GrantNone, "HARDN_CONFIG", "EDITOR"))
		}, sudoKept, true, "/etc/sudoers.d/hardn-env-alice", env("EDITOR"), engine.RiskPlain, nil},
		{"keep, another variable carried", func(t *testing.T) *testHost {
			return newHost(t, debian13).with(t, grant(sudoers.GrantPassword, "EDITOR"))
		}, sudoAlice, false, "/etc/sudoers.d/hardn-env-alice", env("EDITOR HARDN_CONFIG"), engine.RiskPlain, nil},
		{"keep, legacy", func(t *testing.T) *testHost { return legacyHost(t, legacyLines) }, sudoKept, false,
			"/etc/sudoers.d/hardn-env-alice", env("HARDN_CONFIG"), engine.RiskPlain,
			[]string{"SUDOERS_LEGACY_FILE /etc/sudoers.d/alice: /etc/sudoers.d/alice may still let alice use sudo"}},
		{"keep, doas, hardn's password rule", func(t *testing.T) *testHost { return newHost(t, alpine324).with(t, grant(sudoers.GrantPassword)) },
			doasAlice, false, "/etc/doas.d/hardn-alice.conf", sudoers.Header + "permit persist setenv { HARDN_CONFIG } alice\n", engine.RiskLockout, nil},
		{"keep, doas, hardn's nopass rule", func(t *testing.T) *testHost { return newHost(t, alpine324).with(t, grant(sudoers.GrantNoPassword)) },
			doasAlice, false, "/etc/doas.d/hardn-alice.conf", sudoers.Header + "permit nopass setenv { HARDN_CONFIG } alice\n", engine.RiskPlain, nil},
		{"stop, doas, overridden", func(t *testing.T) *testHost {
			return newHost(t, alpineCloud).with(t, grant(sudoers.GrantPassword, "HARDN_CONFIG"))
		}, doasAlice, true, "/etc/doas.d/hardn-alice.conf", sudoers.Header + "permit persist alice\n", engine.RiskLockout,
			[]string{"SUDOERS_LATER_FILE /etc/doas.d/wheel.conf: /etc/doas.d/wheel.conf may override hardn's rule"}},
		{"stop, alpine324: nothing", func(t *testing.T) *testHost { return newHost(t, alpine324) }, doasAlice, true, "", "", engine.RiskPlain, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.host(t)
			n := len(h.Calls())
			cs, err := environment.Keep{Getenv: lookup(tt.env), Stop: tt.stop}.Plan(ctx, h, engine.Input{Facts: h.facts})
			if err != nil {
				t.Fatal(err)
			}
			reads(t, h, n)
			if tt.target == "" {
				if len(cs) > 0 {
					t.Errorf("planned %+v", cs)
				}
				return
			}
			if len(cs) != 1 {
				t.Fatalf("%d changes", len(cs))
			}
			c := cs[0]
			id, summary := environment.ChangeKeep, "Keep HARDN_CONFIG"
			if tt.stop {
				id, summary = environment.ChangeStop, "Stop keeping HARDN_CONFIG"
			}
			after := "+++ /dev/null"
			if tt.after != "" {
				lines := strings.Split(strings.TrimSuffix(tt.after, "\n"), "\n")
				after = "+" + lines[len(lines)-1] + "\n"
			}
			var fds []string
			for _, f := range c.Findings {
				fds = append(fds, f.Code+" "+f.Source+": "+f.Detail)
			}
			switch {
			case c.ID != id || c.Summary != summary || c.Heading() != summary:
				t.Errorf("id %q, summary %q, heading %q", c.ID, c.Summary, c.Heading())
			case c.Target != tt.target || c.Risk != tt.risk || c.Stage != engine.StageAccounts:
				t.Errorf("target %q, risk %v, stage %v", c.Target, c.Risk, c.Stage)
			case !strings.Contains(c.Diff, after):
				t.Errorf("diff:\n%s\nwant %q", c.Diff, after)
			case !slices.Equal(fds, tt.findings):
				t.Errorf("findings %q", fds)
			}
		})
	}
}

// TestControl: the control's identity, its words, and the lookup it is
// given: Controls(getenv) keeps; a nil lookup is no user.
func TestControl(t *testing.T) {
	cs := environment.Controls(lookup(sudoAlice))
	k, ok := cs[0].(environment.Keep)
	if len(cs) != 1 || !ok || k.Stop || k.ID() != "environment.keep" || environment.ChangeKeep != "environment.keep/keep" || environment.ChangeStop != "environment.keep/stop" || k.Title() != "Environment" {
		t.Fatalf("%+v", cs)
	}
	if ok, why := k.Applies(detect.Facts{}); !ok || why != "" {
		t.Errorf("Applies: %v %q", ok, why)
	}
	h := newHost(t, debian13)
	if r := environment.Read(context.Background(), h, h.facts, nil); !errors.Is(r.Err, environment.ErrNoUser) {
		t.Errorf("nil getenv: %v", r.Err)
	}
	for _, tt := range []struct {
		tool        sudoers.Tool
		kept        bool
		value, note string
	}{
		{sudoers.ToolSudo, true, "Kept", ""}, {sudoers.ToolSudo, false, "Not kept", ""},
		{sudoers.ToolDoas, true, "Kept", "by hardn"}, {sudoers.ToolDoas, false, "Not kept", "by hardn"},
	} {
		if v, n := environment.Words(tt.tool, tt.kept); v != tt.value || n != tt.note {
			t.Errorf("Words(%v, %v) = %q %q", tt.tool, tt.kept, v, n)
		}
	}
	for title, max := range map[string]int{environment.SummaryKeep: engine.TitleMax, environment.SummaryStop: engine.TitleMax} {
		if len(title) > max {
			t.Errorf("%q is longer than %d", title, max)
		}
	}
}
