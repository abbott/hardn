package users_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	usrscreen "github.com/abbott/hardn/pkg/screens/users"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// Install shadow's words (proposed, screen-users.md §5).
const (
	shadowAbout  = "Installs shadow first"
	shadowNotice = "hardn needs useradd, from shadow, to create users."
	shadowAsk    = "Install shadow?"
)

// The apk simulations of shadow: one that adds it with the dependencies apk
// info -R lists on the Alpine 3.23 VM (2026-11-11), in apk's form [I: not
// a simulation run on a VM without shadow]; one that finds none.
var (
	shadowSim     = hostfake.Response{Stdout: "(1/3) Installing linux-pam (1.7.1-r2)\n(2/3) Installing libbsd (0.12.2-r0)\n(3/3) Installing shadow (4.18.0-r0)\nOK: 12 MiB in 32 packages\n"}
	shadowNoIndex = hostfake.Response{Stderr: "ERROR: unable to select packages:\n  shadow (no such package):\n    required by: world[shadow]\n", ExitCode: 1}
)

// noShadow is the Alpine VM without shadow, as the LXC of 2026-11-07: no
// useradd, and apk answering for shadow as sim says, apk add shadow
// installing it with useradd; no doas too with noDoas.
func noShadow(t *testing.T, sim hostfake.Response, noDoas bool) *usrHost {
	t.Helper()
	h := newHost(t, "alpine323")
	if err := h.Remove(context.Background(), "/usr/sbin/useradd"); err != nil {
		t.Fatal(err)
	}
	if noDoas {
		h.facts.Elevation.Doas = detect.Tool{Present: detect.No}
	}
	installed := false
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
		}
		return h.answer(argv, stdin)
	}
	return h
}

// changed are writes(h, n) but apk's reads of shadow: apk info -e and the
// simulation.
func changed(h *usrHost, n int) []string {
	var out []string
	for _, w := range writes(h, n) {
		if w != "run apk info -e shadow" && w != "run apk add --simulate shadow" {
			out = append(out, w)
		}
	}
	return out
}

// TestCreateInstallsShadow: on Alpine without useradd, Create User says it
// installs shadow first; chosen, the review says why, y/N; yes installs it,
// then Create's own questions and plan, the user made with useradd.
func TestCreateInstallsShadow(t *testing.T) {
	h := noShadow(t, shadowSim, false)
	p := play(h, false, "2", "y", "carol", "n", "n", "y", "0")
	if p.err != nil || !shows(p.out, shadowAbout) || !shows(p.out, shadowNotice) || !strings.Contains(p.out, shadowAsk) || !shows(p.out, "Package shadow") {
		t.Fatalf("Run = %v:\n%s", p.err, p.out)
	}
	if !ran(h, 0, "apk add shadow") || !ran(h, 0, "useradd --home-dir /home/carol") || strings.Count(p.out, applied) != 2 {
		t.Errorf("install, then Create, each applied:\n%s", p.out)
	}
	if u, _ := userOf(t, readHost(t, h), "carol"); u.Admin != detect.No {
		t.Errorf("carol read back %+v", u)
	}
	checkHost(t, "no shadow", h)
}

// TestCreateShadowDeclined: n to Install shadow, and plan only, install
// nothing and ask nothing of Create, the screen drawn again; with doas
// missing too, shadow is still what is offered first.
func TestCreateShadowDeclined(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dryRun bool
		noDoas bool
		lines  []string
	}{
		{"no", false, false, []string{"2", "n", "0"}},
		{"no, no doas", false, true, []string{"2", "n", "0"}},
		{"plan only", true, false, []string{"2", "0"}},
	} {
		h := noShadow(t, shadowSim, tc.noDoas)
		n := len(h.Calls())
		p := play(h, tc.dryRun, tc.lines...)
		if p.err != nil || !shows(p.out, shadowNotice) || strings.Contains(p.out, "Username") || p.unread != 0 {
			t.Errorf("%s: Run = %v, %d unread:\n%s", tc.name, p.err, p.unread, p.out)
		}
		if tc.dryRun && !shows(p.out, planned) {
			t.Errorf("%s: no plan:\n%s", tc.name, p.out)
		}
		if w := changed(h, n); len(w) > 0 {
			t.Errorf("%s: wrote %v", tc.name, w)
		}
		if strings.Count(p.out, shadowAbout) != 2 {
			t.Errorf("%s: the screen not drawn again:\n%s", tc.name, p.out)
		}
		checkHost(t, tc.name, h)
	}
}

// TestCreateShadowRefused: where apk can't find shadow, its refusal is
// said before asking anything, nothing planned or written.
func TestCreateShadowRefused(t *testing.T) {
	const words = "hardn can't find shadow in apk's package lists. Check the network and the community repository, run apk update, then try again."
	h := noShadow(t, shadowNoIndex, false)
	n := len(h.Calls())
	p := play(h, false, "2", "0")
	if p.err != nil || !shows(p.out, words) || asked(p.out) || p.engines != 0 {
		t.Errorf("Run = %v, %d engines, not refused before asking:\n%s", p.err, p.engines, p.out)
	}
	if w := changed(h, n); len(w) > 0 {
		t.Errorf("wrote %v", w)
	}
}

// TestShadowGoldens pins the screen on the Alpine VM without shadow, opened
// and left with 0, and Install shadow's review, answered n, on the core
// set: testdata/golden/screen/no-shadow.golden and no-shadow-review.golden.
func TestShadowGoldens(t *testing.T) {
	for _, g := range []struct {
		name  string
		lines []string
	}{{"no-shadow", []string{"0"}}, {"no-shadow-review", []string{"2", "n", "0"}}} {
		uitest.Golden(t, "screen/"+g.name, uitest.Core(), func(env term.Env) string {
			h := noShadow(t, shadowSim, false)
			var out bytes.Buffer
			p := playWith(h, false, func(o *usrscreen.Options) {
				o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, g.lines...), false), Out: env.Writer(&out)}
			})
			if p.err != nil {
				t.Errorf("%s: Run = %v", g.name, p.err)
			}
			return out.String()
		})
	}
}
