package main

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	fwctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
	"github.com/charmbracelet/x/ansi"
)

// raCells are the Run All goldens' cells: the core set and LANG=C's at
// 80x24, then Unicode and ASCII at 80x40.
func raCells() []uitest.Cell {
	tall := func(c uitest.Cell) uitest.Cell {
		c.Name += "-40"
		c.Env.Height = 40
		return c
	}
	core := uitest.Core()
	return append(core, asciiColour, tall(core[0]), tall(asciiColour))
}

// raArgs are the flags that pick env, its height too.
func raArgs(t *testing.T, env term.Env) []string {
	t.Helper()
	base := env
	base.Height = term.DefaultHeight
	args := lookArgs(t, base)
	if env.Height != term.DefaultHeight {
		args = append(args, "-height", strconv.Itoa(env.Height))
	}
	return args
}

// TestRunAllGolden pins each mock of the listing at both heights,
// Unicode and ASCII: testdata/golden/runall/<mock>.golden.
func TestRunAllGolden(t *testing.T) {
	for _, m := range raListing.mocks() {
		uitest.Golden(t, raPrefix+"/"+m.name, raCells(), func(env term.Env) string {
			var out, errs bytes.Buffer
			args := append(raArgs(t, env), m.name)
			if code := run(append([]string{raPrefix}, args...), nil, offTerminal, &out, &errs); code != exitOK || errs.Len() > 0 {
				t.Errorf("%s: exit %d, stderr %q", m.name, code, errs.String())
			}
			return out.String()
		})
	}
}

// TestPlayRunAllGolden plays each flow end to end and pins the
// transcript: the combined plan typed and applied, confirm-or-revert where
// it is lockout-risk; a refusal skipped and said; a change pending; nothing
// to change; a wrong phrase, then back; plan only; the hosts' menu; the
// main menu's 7.
func TestPlayRunAllGolden(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"runall-debian13", []string{"runall/debian13"}, []string{"1", hostName, "0"}, raCells()},
		{"runall-ubuntu2604", []string{"runall/ubuntu2604"}, []string{"1", hostName, "0"}, nil},
		{"runall-alpine323", []string{"runall/alpine323"}, []string{"1", hostName, "0"}, nil},
		{"runall-fresh", []string{"runall/fresh"}, []string{"1", hostName, "0"}, raCells()},
		{"runall-hardened", []string{"runall/hardened"}, []string{"0"}, nil},
		{"runall-pve9", []string{"runall/pve9"}, []string{"1", hostName, "0"}, nil},
		{"runall-container", []string{"runall/container"}, []string{"1", hostName, "0"}, nil},
		{"runall-nothing-asked", []string{"runall/nothing-asked"}, []string{"0"}, nil},
		{"runall-refused", []string{"runall/refused"}, []string{"1", hostName, "0"}, nil},
		{"runall-pending", []string{"runall/pending"}, []string{"1", "0"}, nil},
		{"runall-wrong-phrase", []string{"runall/debian13"}, []string{"1", "WEB-01", "0", "0"}, nil},
		{"runall-plan-only", []string{"-sequence", "dry-run", "runall/debian13"}, []string{"1", "0"}, raCells()},
		{"runall-hosts", []string{"runall"}, []string{"4", "0", "0"}, nil},
		{"runall-main-menu", nil, []string{"7", "0", "0"}, nil},
	}
	for _, tt := range tests {
		cells := tt.cells
		if cells == nil {
			cells = uitest.Core()
		}
		uitest.Golden(t, "play/"+tt.name, cells, func(env term.Env) string {
			args := append(raArgs(t, env), tt.args...)
			code, out, errs, unread := playGallery(tt.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread", tt.name, args, code, errs, unread)
			}
			return sessionIDs(t, out)
		})
	}
}

// raWant is what a host's rows must say: the controls Run All plans, those
// it skips as refused or not applicable, and whether the plan is
// lockout-risk.
type raWant struct {
	planned, refused, na []string
	lockout              bool
}

// TestRunAllHosts holds each host's rows to the real controls: a planned
// row's control plans changes alone and is in the one plan; a refused row's
// control refuses alone with the row's words; a Not Applicable row's check
// says so; an "already as asked" row's control plans nothing alone; every
// row's state is its control's own check, as the main menu maps it.
func TestRunAllHosts(t *testing.T) {
	login := ssh.Login{}.ID()
	want := map[string]raWant{
		"debian13":      {planned: []string{login, dnsctl.ID}, lockout: true},
		"ubuntu2604":    {planned: []string{login, dnsctl.ID}, lockout: true},
		"alpine323":     {planned: []string{login, updates.ID, dnsctl.ID}, refused: []string{fwctl.ID}, lockout: true},
		"fresh":         {planned: []string{users.ID, updates.ID, dnsctl.ID}, refused: []string{fwctl.ID, login}},
		"hardened":      {},
		"partial":       {planned: []string{login}, lockout: true},
		"port":          {planned: []string{login, dnsctl.ID}, lockout: true},
		"pve9":          {planned: []string{login, dnsctl.ID}, refused: []string{users.ID, fwctl.ID}, na: []string{updates.ID}, lockout: true},
		"container":     {planned: []string{login}, na: []string{dnsctl.ID}, lockout: true},
		"nothing-asked": {},
		"refused":       {planned: []string{login, dnsctl.ID}, refused: []string{updates.ID}, lockout: true},
		"pending":       {},
	}
	ctx := context.Background()
	for _, rh := range raHosts() {
		w, ok := want[rh.name]
		if !ok {
			t.Errorf("%s: no row expected", rh.name)
			continue
		}
		r, h, err := raReadNamed(ctx, rh.name)
		if err != nil {
			t.Fatalf("%s: %v", rh.name, err)
		}
		var planned, refused, na []string
		for _, rw := range r.rows {
			rep, _ := raReport(r.reports, rw.id)
			switch {
			case rw.planned:
				planned = append(planned, rw.id)
			case rw.refusal != "" && rw.does != raPending:
				refused = append(refused, rw.id)
			case rep.Status.State == engine.NotApplicable && rw.does != raNothing && raLeft[rw.id] == "":
				na = append(na, rw.id)
			}
		}
		if !slices.Equal(planned, w.planned) || !slices.Equal(refused, w.refused) || !slices.Equal(na, w.na) || r.lockout() != w.lockout {
			t.Errorf("%s: planned %q, refused %q, not applicable %q, lockout %v; want %q, %q, %q, %v", rh.name, planned, refused, na, r.lockout(), w.planned, w.refused, w.na, w.lockout)
		}
		raHeldToControls(ctx, t, rh.name, r, h)
	}
}

// raHeldToControls re-derives each row of r on h from the controls
// themselves, outside the mock's own reading.
func raHeldToControls(ctx context.Context, t *testing.T, name string, r raReading, h *fakeHost) {
	t.Helper()
	sys := h.sys()
	e := engine.New(sys, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return r.facts, nil }})
	getenv := func(k string) string { return raEnvOf(name)[k] }
	status := r.status()
	for i, rw := range r.rows {
		c, _ := raAsk(rw.id, r.cfg, getenv)
		checked := raChecked(r.cfg, getenv)
		j := slices.IndexFunc(checked, func(x engine.Control) bool { return x.ID() == rw.id })
		reps, err := e.Check(ctx, checked[j:j+1])
		if err != nil {
			t.Fatalf("%s %s: %v", name, rw.id, err)
		}
		if got := status.Rows[i].State; got != raState(reps[0]) {
			t.Errorf("%s %s: row state %v, the check's %v", name, rw.id, got, raState(reps[0]))
		}
		if c == nil || rw.id == (ssh.Port{}).ID() || reps[0].Status.State == engine.NotApplicable || rw.does == raPending {
			continue
		}
		p, err := raEngine(h, r.sess, nil, &updQuiet).Plan(ctx, []engine.Control{c}, r.cfg)
		if err == nil && len(p.Refused) > 0 {
			err = p.Refused[0]
		}
		switch {
		case rw.planned:
			if err != nil || len(p.Changes) == 0 || !slices.ContainsFunc(r.plan.Changes, func(ch engine.Change) bool { return ch.Control == rw.id }) {
				t.Errorf("%s %s: planned, but alone %v, %d changes, or not in the one plan", name, rw.id, err, len(p.Changes))
			}
		case rw.refusal != "":
			if err == nil || screenkit.RefusalWords(err) != rw.refusal {
				t.Errorf("%s %s: refused %q, alone %v", name, rw.id, rw.refusal, err)
			}
		case rw.does == raAsked && rw.id != users.ID:
			if err != nil || len(p.Changes) > 0 {
				t.Errorf("%s %s: already as asked, but alone %v, %d changes", name, rw.id, err, len(p.Changes))
			}
		case rw.does == raAsked:
			if !errors.Is(err, users.ErrNameTaken) {
				t.Errorf("%s %s: already there, but Create %v", name, rw.id, err)
			}
		}
	}
}

// raState is a report's row state, as the main menu maps it
// (output.StatusViewOf over that report alone): its risk row, or the row
// after them.
func raState(r engine.Report) component.StatusState {
	rows := output.StatusViewOf([]engine.Report{r}, risk.Level{}).Status.Rows
	i := slices.IndexFunc(risk.Rows(), func(rw risk.Row) bool { return rw.ID == r.Control })
	if i < 0 {
		i = len(rows) - 1
	}
	return rows[i].State
}

// TestRunAllPlanOnlyWritesNothing: on every host, item 1 under plan only
// stops at the plan, or refuses before it, and changes nothing outside
// hardn's own state.
func TestRunAllPlanOnlyWritesNothing(t *testing.T) {
	for _, rh := range raHosts() {
		h := rh.host()
		r, err := readRA(context.Background(), h, raEnvOf(rh.name))
		if err != nil {
			t.Fatal(err)
		}
		n := len(h.f.Calls())
		lines := []string{"1", "0"}
		if r.item().Disabled {
			lines = []string{"0"}
		}
		_, out, code := playWith(t, raPrefix+"/"+rh.name, func(s *session) func() error {
			s.sequence = "dry-run"
			return func() error { return s.raScreen(context.Background(), rh, h) }
		}, lines...)
		if code != exitOK {
			t.Errorf("%s: exit %d:\n%s", rh.name, code, out)
		}
		if len(r.planned()) > 0 && r.refusal == "" && !strings.Contains(flatText(out), "No actual changes were made") {
			t.Errorf("%s: plan only did not stop at the plan:\n%s", rh.name, out)
		}
		cmds, files := changed(h.f.Calls()[n:])
		cmds = slices.DeleteFunc(cmds, func(c string) bool {
			return strings.HasPrefix(c, "apt-config dump ") || c == "rc-service crond status" // the Auto Updates control's reads
		})
		if len(cmds) > 0 || len(files) > 0 {
			t.Errorf("%s: plan only changed the host: %q, %q", rh.name, cmds, files)
		}
	}
}

// TestRunAllTyped: the hostname is asked whatever the plan's risk
// (decision 6), fresh's plain plan too.
func TestRunAllTyped(t *testing.T) {
	r, _, err := raReadNamed(context.Background(), "fresh")
	if err != nil || r.lockout() || len(r.planned()) == 0 {
		t.Fatalf("fresh: %v, lockout %v, %d planned; want a plain plan", err, r.lockout(), len(r.planned()))
	}
	code, out, errs, unread := playGallery([]string{"1", "0", "0"}, "-profile", "none", "runall/fresh")
	if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, typeTheHostname+" [web-01") {
		t.Errorf("fresh: exit %d, stderr %q, %d unread; want the hostname asked:\n%s", code, errs, unread, out)
	}
}

// TestRunAllHeights: every host's screen fits 80x24, no line over 80
// cells, Unicode and ASCII.
func TestRunAllHeights(t *testing.T) {
	for _, unicode := range []bool{true, false} {
		env := term.Fixed(80, 0, unicode, true)
		c := component.NewCtx(env)
		for _, rh := range raHosts() {
			r, _, err := raReadNamed(context.Background(), rh.name)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(r.screen().Render(c), "\n")
			if len(lines) > term.DefaultHeight {
				t.Errorf("%s, unicode %v: %d lines", rh.name, unicode, len(lines))
			}
			for _, l := range lines {
				if w := ansi.StringWidth(l); w > 80 {
					t.Errorf("%s, unicode %v: %d cells: %q", rh.name, unicode, w, l)
				}
			}
		}
	}
}

// TestRunAllUsage: play refuses what it does not take, with status 2 and
// nothing on stdout.
func TestRunAllUsage(t *testing.T) {
	for _, args := range [][]string{
		{"play", "runall/nope"},
		{"play", "-fail", "1", "runall/debian13"},
		{"play", "-sequence", "failure", "runall/debian13"},
	} {
		var out, errs bytes.Buffer
		if code := run(args, noInput, offTerminal, &out, &errs); code != exitUsage || out.Len() != 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q; want %d and a usage error", args, code, out.String(), errs.String(), exitUsage)
		}
	}
}
