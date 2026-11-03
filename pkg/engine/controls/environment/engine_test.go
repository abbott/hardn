package environment_test

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/environment"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// planOn plans the Environment control on h through the engine as
// cmd/hardn builds it, with hardn's environment env, keeping or stopping.
func planOn(t *testing.T, h *testHost, env map[string]string, stop bool) (*engine.Engine, *engine.Plan) {
	t.Helper()
	e := engineOn(h, nil)
	p, err := e.Plan(context.Background(), []engine.Control{environment.Keep{Getenv: lookup(env), Stop: stop}}, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return e, p
}

// applyOn plans and applies the control on h, failing the test on a
// refusal or a failed apply, and returns the files under /etc the apply
// wrote or removed.
func applyOn(t *testing.T, h *testHost, env map[string]string, stop bool) []string {
	t.Helper()
	e, p := planOn(t, h, env, stop)
	if p.Err() != nil || p.Empty() {
		t.Fatalf("plan: %v, %d changes", p.Err(), len(p.Changes))
	}
	n := len(h.Calls())
	if _, err := e.Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return changedFiles(h, n)
}

// TestApply keeps HARDN_CONFIG on Debian 13 and on Ubuntu 26.04 (sudo-rs)
// through the engine, then stops keeping it: each apply writes or removes
// /etc/sudoers.d/hardn-env-alice alone, its copy checked in the run
// directory (sudoers.Apply), nothing under /tmp (V20); and the plan made
// after each apply is empty (idempotency), which Read says as the screen
// would, item 1 turning the other way.
func TestApply(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{debian13, ubuntu2604} {
		t.Run(name, func(t *testing.T) {
			h := newHost(t, name)
			n := len(h.Calls())
			if got := applyOn(t, h, sudoAlice, false); !slices.Equal(got, []string{"write-atomic /etc/sudoers.d/hardn-env-alice"}) {
				t.Errorf("keeping changed %q", got)
			}
			var runs []string
			for _, c := range h.Calls()[n:] {
				if c.Op == "run" {
					runs = append(runs, regexp.MustCompile(`/run/hardn/[^/]+/`).ReplaceAllString(strings.Join(c.Argv, " "), "/run/hardn/<id>/"))
				}
			}
			if want := []string{"visudo -c", "visudo -cf /run/hardn/<id>/sudoers-validate/hardn-env-alice", "visudo -c"}; !slices.Equal(runs, want) {
				t.Errorf("ran %q, want %q", runs, want)
			}
			if got := content(h, "/etc/sudoers.d/hardn-env-alice"); got != sudoers.Header+"Defaults:alice env_keep += \"HARDN_CONFIG\"\n" {
				t.Errorf("hardn-env-alice: %q", got)
			}
			if e, _ := h.Entry("/etc/sudoers.d/hardn-env-alice"); e.Mode.Perm() != 0o440 || e.UID != 0 || e.GID != 0 {
				t.Errorf("hardn-env-alice: %v %d:%d", e.Mode, e.UID, e.GID)
			}
			if _, p := planOn(t, h, sudoAlice, false); p.Err() != nil || !p.Empty() {
				t.Errorf("keeping again: %v, %d changes", p.Err(), len(p.Changes))
			}
			if r := environment.Read(ctx, h, h.facts, lookup(sudoAlice)); !r.Kept || r.Refusal() != nil {
				t.Errorf("read after keeping: kept %v, %v", r.Kept, r.Refusal())
			}
			if got := applyOn(t, h, sudoAlice, true); !slices.Equal(got, []string{"remove /etc/sudoers.d/hardn-env-alice"}) {
				t.Errorf("stopping changed %q", got)
			}
			if _, p := planOn(t, h, sudoAlice, true); p.Err() != nil || !p.Empty() {
				t.Errorf("stopping again: %v, %d changes", p.Err(), len(p.Changes))
			}
			for _, c := range h.Calls()[n:] {
				if strings.HasPrefix(c.Path, "/tmp") {
					t.Errorf("a call under /tmp: %v", c)
				}
			}
		})
	}
}

// TestGrantCarried: on a host where hardn grants alice sudo, with a
// password or without, keeping HARDN_CONFIG and stopping write
// hardn-env-alice alone, no lockout risk, and hardn-grant-alice is byte
// for byte as it was, its mode and owner too (V16: the grant and the line
// in separate files; gallery-environment.md §7 item 20, which an env-only
// Policy would have removed), at hardn's mode and at another, which the
// grant's own change would repair, not this one.
func TestGrantCarried(t *testing.T) {
	for _, tt := range []struct {
		g    sudoers.Grant
		mode fs.FileMode
	}{
		{sudoers.GrantPassword, sudoers.SudoMode}, {sudoers.GrantNoPassword, sudoers.SudoMode},
		{sudoers.GrantPassword, 0o640}, {sudoers.GrantNoPassword, 0o400},
	} {
		t.Run(fmt.Sprintf("%v, %v", tt.g, tt.mode), func(t *testing.T) {
			h := newHost(t, debian13).with(t, grant(tt.g)).moded(t, "/etc/sudoers.d/hardn-grant-alice", tt.mode, 0, 0)
			before, _ := h.Entry("/etc/sudoers.d/hardn-grant-alice")
			for _, stop := range []bool{false, true} {
				if _, p := planOn(t, h, sudoAlice, stop); p.Risk == engine.RiskLockout {
					t.Errorf("stop %v: lockout-risk", stop)
				}
				if got := applyOn(t, h, sudoAlice, stop); len(got) != 1 || !strings.HasSuffix(got[0], " /etc/sudoers.d/hardn-env-alice") {
					t.Errorf("stop %v: changed %q", stop, got)
				}
				after, _ := h.Entry("/etc/sudoers.d/hardn-grant-alice")
				if string(after.Data) != string(before.Data) || after.Mode != before.Mode || after.UID != 0 || after.GID != 0 {
					t.Errorf("stop %v: hardn-grant-alice %q %v, was %q %v", stop, after.Data, after.Mode, before.Data, before.Mode)
				}
			}
		})
	}
}

// TestPlanOnlyWritesNothing: on every host, planning either way through
// the engine, as plan only does (-n, the screen's dry run), changes
// nothing outside hardn's run directory, which the engine makes and
// removes, and runs nothing.
func TestPlanOnlyWritesNothing(t *testing.T) {
	for _, tt := range []struct {
		name string
		host func(t *testing.T) *testHost
		env  map[string]string
	}{
		{debian13, func(t *testing.T) *testHost { return newHost(t, debian13) }, sudoAlice},
		{ubuntu2604, func(t *testing.T) *testHost { return newHost(t, ubuntu2604) }, sudoAlice},
		{"kept", func(t *testing.T) *testHost {
			return newHost(t, debian13).with(t, grant(sudoers.GrantNoPassword, "HARDN_CONFIG"))
		}, sudoKept},
		{"legacy", func(t *testing.T) *testHost { return legacyHost(t, legacyLines) }, sudoKept},
		{alpine324, func(t *testing.T) *testHost { return newHost(t, alpine324) }, doasAlice},
		{"doas, overridden", func(t *testing.T) *testHost {
			return newHost(t, alpineCloud).with(t, grant(sudoers.GrantPassword, "HARDN_CONFIG"))
		}, doasAlice},
		{pve9, func(t *testing.T) *testHost { return newHost(t, pve9) }, rootLogin},
		{"root login", func(t *testing.T) *testHost { return newHost(t, debian13) }, rootLogin},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.host(t)
			n := len(h.Calls())
			for _, stop := range []bool{false, true} {
				planOn(t, h, tt.env, stop)
			}
			reads(t, h, n)
		})
	}
}

// TestDoasLockout: on the cloud image, where hardn's rule for alice asks
// alice's password and keeps HARDN_CONFIG, stopping is a lockout-risk
// plan on the engine (the rule rewritten without setenv: sudoers.RiskLockout),
// hardn's boot unit planned before it, the plan's finding of zz-local.conf
// on the change; with a nopass rule, keeping is plain. Units (c) and (d)
// read p.Risk: the screen confirms by the hostname typed, and -e and
// setup-sudo-env refuse it (gallery-environment.md §5, Made here).
func TestDoasLockout(t *testing.T) {
	h := newHost(t, alpineCloud).with(t, grant(sudoers.GrantPassword, "HARDN_CONFIG"))
	_, p := planOn(t, h, doasAlice, true)
	if p.Err() != nil || p.Risk != engine.RiskLockout || len(p.Changes) != 2 || p.Changes[0].ID != engine.BootChangeID ||
		p.Changes[1].ID != environment.ChangeStop {
		t.Fatalf("stopping: %v, risk %v, %+v", p.Err(), p.Risk, p.Changes)
	}
	if fs := p.Changes[1].Findings; len(fs) != 1 || fs[0].Detail != "/etc/doas.d/zz-local.conf may override hardn's rule" {
		t.Errorf("findings %+v", fs)
	}
	h = newHost(t, alpine324).with(t, grant(sudoers.GrantNoPassword))
	if _, p = planOn(t, h, doasAlice, false); p.Err() != nil || p.Risk == engine.RiskLockout || len(p.Changes) != 1 {
		t.Errorf("keeping on a nopass rule: %v, risk %v, %d changes", p.Err(), p.Risk, len(p.Changes))
	}
}
