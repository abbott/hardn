package users_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
)

// shadowSim is apk add --simulate shadow on an Alpine without shadow, its
// dependencies those apk info -R lists on the Alpine 3.23 VM (2026-11-11);
// the form is apk's, as pkg/host/alpine's tests have it ([I]: not a
// simulation run on a VM without shadow).
var shadowSim = hostfake.Response{Stdout: "(1/6) Installing skalibs-libs (2.14.4.0-r0)\n(2/6) Installing utmps-libs (0.1.3.1-r0)\n" +
	"(3/6) Installing linux-pam (1.7.1-r2)\n(4/6) Installing libmd (1.1.0-r0)\n(5/6) Installing libbsd (0.12.2-r0)\n" +
	"(6/6) Installing shadow (4.18.0-r0)\nOK: 12 MiB in 32 packages\n"}

// shadowOn is s with apk answering for shadow: apk info -e as installed
// says, the simulation as sim, apk add shadow installing it, useradd with
// it, and apk del shadow removing both.
func shadowOn(t *testing.T, s site, sim hostfake.Response, installed bool) (host.System, *hostfake.System) {
	t.Helper()
	sys, h := live(t, s)
	inner := h.Handler
	h.Handler = func(argv []string, stdin []byte) hostfake.Response {
		switch strings.Join(argv, " ") {
		case "apk info -e shadow":
			if installed {
				return hostfake.Response{Stdout: "shadow\n"}
			}
			return hostfake.Response{ExitCode: 1}
		case "apk add --simulate shadow":
			return sim
		case "apk add shadow":
			installed = true
			h.AddFile("/usr/sbin/useradd", []byte("ELF"), 0o755, 0, 0)
			return hostfake.Response{Stdout: shadowSim.Stdout}
		case "apk del shadow":
			installed = false
			_ = h.Remove(context.Background(), "/usr/sbin/useradd")
			return hostfake.Response{}
		}
		return inner(argv, stdin)
	}
	return sys, h
}

// planShadow plans Install shadow on sys through the engine, checking that
// planning installed nothing.
func planShadow(t *testing.T, sys host.System, h *hostfake.System, s site) (*engine.Engine, *engine.Plan) {
	t.Helper()
	e := engineOn(sys, s.plat.facts)
	p, err := e.Plan(context.Background(), []engine.Control{users.Admin{Action: users.ActionInstallShadow}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range h.Calls() {
		if c.Op == "run" && slices.Equal(c.Argv, []string{"apk", "add", "shadow"}) {
			t.Error("planning ran apk add shadow")
		}
	}
	return e, p
}

// TestInstallShadow: on Alpine without useradd, one package-install change,
// apk's command at the packages stage, its state after naming the other new
// packages, its undo apk del shadow; applied, useradd is there and Create
// plans.
func TestInstallShadow(t *testing.T) {
	s := site{plat: alpine324(), noUseradd: true}
	sys, h := shadowOn(t, s, shadowSim, false)
	e, p := planShadow(t, sys, h, s)
	if p.Err() != nil || len(p.Changes) != 1 || p.Risk != engine.RiskPackageInstall {
		t.Fatalf("plan: %d changes, %v, risk %v", len(p.Changes), p.Err(), p.Risk)
	}
	c := p.Changes[0]
	const after = "installed, with skalibs-libs, utmps-libs, linux-pam, libmd, libbsd"
	if c.ID != users.ChangeShadow || c.Title != "Install shadow" || c.Target != "shadow" || c.Stage != engine.StagePackages ||
		c.StateBefore != "not installed" || c.StateAfter != after || !slices.Equal(c.Commands, []string{"apk add shadow"}) {
		t.Errorf("change %+v", c)
	}
	if len(c.Rollback.Steps) != 1 || !slices.Equal(c.Rollback.Steps[0].Argv, []string{"apk", "del", "shadow"}) {
		t.Errorf("undo %+v", c.Rollback.Steps)
	}
	if _, err := e.Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	r, err := users.Read(context.Background(), sys, s.plat.facts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.UseraddRefusal(context.Background(), sys); err != nil {
		t.Errorf("after the install: %v", err)
	}
}

// TestInstallShadowNothing: with useradd there, and off Alpine, where Create
// does not look for it, nothing to do, and apk is not asked.
func TestInstallShadowNothing(t *testing.T) {
	for _, s := range []site{{plat: alpine324()}, {plat: debian13(), noUseradd: true}} {
		sys, h := shadowOn(t, s, shadowSim, false)
		if _, p := planShadow(t, sys, h, s); p.Err() != nil || !p.Empty() {
			t.Errorf("%s: %d changes, %v", s.plat.dir, len(p.Changes), p.Err())
		}
		if ran(h, "apk") {
			t.Errorf("%s: apk ran", s.plat.dir)
		}
	}
}

// TestInstallShadowRefusals: each in its words before anything changes,
// with the registry's code: apk finds no shadow (community), the
// simulation would change another package, and shadow installed without
// its useradd.
func TestInstallShadowRefusals(t *testing.T) {
	noIndex := hostfake.Response{Stderr: "ERROR: unable to select packages:\n  shadow (no such package):\n    required by: world[shadow]\n", ExitCode: 1}
	upgrade := hostfake.Response{Stdout: "(1/2) Upgrading musl (1.2.5-r0 -> 1.2.5-r1)\n(2/2) Installing shadow (4.18.0-r0)\n"}
	for _, tc := range []struct {
		name        string
		sim         hostfake.Response
		installed   bool
		code, words string
	}{
		{"apk finds none", noIndex, false, "PACKAGE_NOT_IN_REPOS", "hardn can't find shadow in apk's package lists. Check the network and the community repository, run apk update, then try again."},
		{"an upgrade", upgrade, false, "PACKAGE_OTHER_CHANGES", "Installing shadow would also change other packages: musl. Install it by hand, then try again."},
		{"installed, no useradd", shadowSim, true, "USERS_NO_USERADD", "shadow is installed, but its useradd isn't there. Reinstall shadow, then try again."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := site{plat: alpine324(), noUseradd: true}
			sys, h := shadowOn(t, s, tc.sim, tc.installed)
			_, p := planShadow(t, sys, h, s)
			rf := refusalOf(t, p)
			if rf.Code != tc.code || output.CodeOf(p.Err()) != tc.code {
				t.Errorf("code %s, registry's %s, want %s", rf.Code, output.CodeOf(p.Err()), tc.code)
			}
			if got := screenkit.RefusalWords(rf); got != tc.words {
				t.Errorf("words\n %q\nwant\n %q", got, tc.words)
			}
		})
	}
}
