package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	backupctl "github.com/abbott/hardn/pkg/engine/controls/backup"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	fwctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/engine/runall"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
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

// TestPlayRunAllGolden plays each flow end to end and pins the
// transcript: the combined plan typed and applied, confirm-or-revert where
// it is lockout-risk; a refusal skipped and said; a change pending; nothing
// to change; a wrong phrase, then back; plan only; the hosts' menu; the
// main menu's 6.
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
		{"runall-ufw-inactive", []string{"runall/ufw-inactive"}, []string{"1", hostName, "0"}, nil},
		{"runall-new-admin", []string{"runall/new-admin"}, []string{"1", hostName, "0"}, raCells()},
		{"runall-george-no-sudo", []string{"runall/george-no-sudo"}, []string{"1", hostName, "0"}, nil},
		{"runall-wrong-phrase", []string{"runall/debian13"}, []string{"1", "WEB-01", "0", "0"}, nil},
		{"runall-plan-only", []string{"-sequence", "dry-run", "runall/debian13"}, []string{"1", "0"}, raCells()},
		{"runall-hosts", []string{"runall"}, []string{"4", "0", "0"}, nil},
		{"runall-main-menu", nil, []string{"6", "0", "0"}, nil},
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

// raReading is a host read for Run All: the planner's Reading
// (pkg/engine/runall), and what it was read with.
type raReading struct {
	runall.Reading
	in runall.Input
}

// readRA reads h for Run All with the planner, as the screen does:
// hardn.yml and the facts as play reads them, the plans made on the
// gallery's engine (raEngine).
func readRA(ctx context.Context, h *fakeHost, env map[string]string) (raReading, error) {
	cfg, err := statusConfig(ctx, h.sys())
	if err != nil {
		return raReading{}, err
	}
	facts, err := detect.Detect(ctx, h.sys())
	if err != nil {
		return raReading{}, err
	}
	sess := sshSession(env["SSH_CONNECTION"])
	in := runall.Input{
		System: h.system(), Facts: facts, Config: cfg, Session: sess,
		Getenv: func(k string) string { return env[k] },
		Revert: raRevert, Version: updVersion,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			return raEngine(h, sess, cfg, progress, &updQuiet)
		},
	}
	rd, err := runall.Read(ctx, in)
	return raReading{Reading: rd, in: in}, err
}

// raReadNamed reads the host named name, fresh.
func raReadNamed(ctx context.Context, name string) (raReading, *fakeHost, error) {
	rh, ok := findRAHost(name)
	if !ok {
		return raReading{}, nil, fmt.Errorf("no runall host %q", name)
	}
	h := rh.host()
	r, err := readRA(ctx, h, raEnvOf(name))
	return r, h, err
}

// raWant is what a host's rows must say: the controls Run All plans, those
// it skips as refused or not applicable, and whether the plan is
// lockout-risk.
type raWant struct {
	planned, refused, na, later []string
	lockout                     bool
}

// TestRunAllHosts holds each host's rows to the real controls: a planned
// row's control plans changes alone and is in the one plan; a refused row's
// control refuses alone with the row's words; a Not Applicable row's check
// says so; an "already as asked" row's control plans nothing alone; every
// row's state is its control's own check, as the main menu maps it.
func TestRunAllHosts(t *testing.T) {
	login := ssh.Login{}.ID()
	want := map[string]raWant{
		"debian13":       {planned: []string{login, dnsctl.ID}, lockout: true},
		"ubuntu2604":     {planned: []string{login, dnsctl.ID}, lockout: true},
		"alpine323":      {planned: []string{login, dnsctl.ID, updates.ID}, refused: []string{fwctl.ID}, lockout: true},
		"fresh":          {planned: []string{users.ID, dnsctl.ID, updates.ID}, refused: []string{login, fwctl.ID}},
		"hardened":       {},
		"partial":        {planned: []string{login}, lockout: true},
		"port":           {planned: []string{login, dnsctl.ID}, lockout: true},
		"pve9":           {planned: []string{login, dnsctl.ID}, refused: []string{users.ID, fwctl.ID}, na: []string{updates.ID}, lockout: true},
		"container":      {planned: []string{login}, na: []string{dnsctl.ID}, lockout: true},
		"nothing-asked":  {},
		"refused":        {planned: []string{login, dnsctl.ID}, refused: []string{updates.ID}, lockout: true},
		"pending":        {},
		"ufw-inactive":   {planned: []string{fwctl.ID}, later: []string{login}, lockout: true},
		"new-admin":      {planned: []string{users.ID, login}, lockout: true},
		"george-no-sudo": {planned: []string{dnsctl.ID}, refused: []string{login}},
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
		var planned, refused, na, later []string
		for _, rw := range r.Rows {
			switch rw.Kind {
			case runall.Planned:
				planned = append(planned, rw.ID)
			case runall.Refused:
				refused = append(refused, rw.ID)
			case runall.NotApplicable:
				na = append(na, rw.ID)
			case runall.Later:
				later = append(later, rw.ID)
			}
		}
		if !slices.Equal(planned, w.planned) || !slices.Equal(refused, w.refused) || !slices.Equal(na, w.na) || !slices.Equal(later, w.later) || r.Lockout() != w.lockout {
			t.Errorf("%s: planned %q, refused %q, not applicable %q, later %q, lockout %v; want %q, %q, %q, %q, %v", rh.name, planned, refused, na, later, r.Lockout(), w.planned, w.refused, w.na, w.later, w.lockout)
		}
		raHeldToControls(ctx, t, rh.name, r, h)
	}
}

// raHeldToControls re-derives each row of r on h from the controls
// themselves, outside the planner's reading.
func raHeldToControls(ctx context.Context, t *testing.T, name string, r raReading, h *fakeHost) {
	t.Helper()
	sys := h.sys()
	e := engine.New(sys, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return r.Facts, nil }})
	getenv := func(k string) string { return raEnvOf(name)[k] }
	status := output.StatusViewOf(r.Reports, risk.Level{}).Status
	for i, rw := range r.Rows {
		c := runall.Asked(r.in, rw.ID).Control
		checked := runall.Checked(r.Config, getenv)
		j := slices.IndexFunc(checked, func(x engine.Control) bool { return x.ID() == rw.ID })
		reps, err := e.Check(ctx, checked[j:j+1])
		if err != nil {
			t.Fatalf("%s %s: %v", name, rw.ID, err)
		}
		if got := status.Rows[i].State; got != raState(reps[0]) {
			t.Errorf("%s %s: row state %v, the check's %v", name, rw.ID, got, raState(reps[0]))
		}
		if c == nil || reps[0].Status.State == engine.NotApplicable || rw.Kind == runall.Pending {
			continue
		}
		p, err := raEngine(h, r.in.Session, r.Config, nil, &updQuiet).Plan(ctx, []engine.Control{c}, r.Config)
		if err == nil && len(p.Refused) > 0 {
			err = p.Refused[0]
		}
		switch {
		case rw.Kind == runall.Planned:
			if err != nil || len(p.Changes) == 0 || !slices.ContainsFunc(r.Plan.Changes, func(ch engine.Change) bool { return ch.Control == rw.ID }) {
				t.Errorf("%s %s: planned, but alone %v, %d changes, or not in the one plan", name, rw.ID, err, len(p.Changes))
			}
		case rw.Kind == runall.Later:
			if err != nil || len(p.Changes) == 0 || slices.ContainsFunc(r.Plan.Changes, func(ch engine.Change) bool { return ch.Control == rw.ID }) {
				t.Errorf("%s %s: after the firewall, but alone %v, %d changes, or in the one plan", name, rw.ID, err, len(p.Changes))
			}
		case rw.Kind == runall.Refused:
			if err == nil || screenkit.RefusalWords(err) != rw.Refusal {
				t.Errorf("%s %s: refused %q, alone %v", name, rw.ID, rw.Refusal, err)
			}
		case rw.Kind == runall.AsAsked && rw.ID != users.ID:
			if err != nil || len(p.Changes) > 0 {
				t.Errorf("%s %s: already as asked, but alone %v, %d changes", name, rw.ID, err, len(p.Changes))
			}
		case rw.Kind == runall.AsAsked:
			if !errors.Is(err, users.ErrNameTaken) {
				t.Errorf("%s %s: already there, but Create %v", name, rw.ID, err)
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

// TestRunAllUserThere: a user already there is as asked only as an admin
// with the asked keys; george with keys and no sudo is left to User
// Management, unchanged.
func TestRunAllUserThere(t *testing.T) {
	for name, want := range map[string]string{"debian13": runall.WordsAsked, "george-no-sudo": "User Management's: not an admin"} {
		r, _, err := raReadNamed(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := r.Row(users.ID); got.Words != want || got.Kind == runall.Planned {
			t.Errorf("%s: User row %q, %v; want %q, not planned", name, got.Words, got.Kind, want)
		}
	}
}

// TestRunAllPendingAfterApply: after a lockout-risk Apply all, not yet
// confirmed, the host read again plans nothing: every row asked of says
// "Change pending", as on pending.
func TestRunAllPendingAfterApply(t *testing.T) {
	ctx := context.Background()
	r, h, err := raReadNamed(ctx, "debian13")
	if err != nil || !r.Lockout() {
		t.Fatalf("debian13: %v, lockout %v", err, r.Lockout())
	}
	if _, err := raEngine(h, r.in.Session, r.Config, nil, &updQuiet).Apply(ctx, r.Plan); err != nil {
		t.Fatal(err)
	}
	again, err := readRA(ctx, h, raEnvOf("debian13"))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Busy || len(again.Planned()) > 0 || again.Plan != nil || again.Refusal != runall.PendingWords {
		t.Errorf("after the apply: busy %v, %d planned, plan %v, refusal %q; want Change pending", again.Busy, len(again.Planned()), again.Plan != nil, again.Refusal)
	}
	for _, rw := range again.Rows {
		if runall.Asked(again.in, rw.ID).Control != nil && rw.Kind != runall.Pending {
			t.Errorf("after the apply: %s %q, want %q", rw.ID, rw.Words, runall.WordsPending)
		}
	}
}

// TestRunAllNoSave: no host's combined plan saves hardn.yml: neither DNS's
// save nor a backup saver's change is in it (decision 5).
func TestRunAllNoSave(t *testing.T) {
	for _, rh := range raHosts() {
		r, _, err := raReadNamed(context.Background(), rh.name)
		if err != nil {
			t.Fatal(err)
		}
		if r.Plan == nil {
			continue
		}
		for _, c := range r.Plan.Changes {
			if c.ID == dnsctl.ChangeSave || c.ID == backupctl.ChangeSave || c.Control == backupctl.ID {
				t.Errorf("%s: the plan has %s", rh.name, c.ID)
			}
		}
	}
}

// raStages are the plan's stages in order, each once, the boot unit's
// left out.
func raStages(p *engine.Plan) []engine.Stage {
	var out []engine.Stage
	for _, c := range p.Changes {
		if c.Stage != engine.StageBoot && (len(out) == 0 || out[len(out)-1] != c.Stage) {
			out = append(out, c.Stage)
		}
	}
	return out
}

// TestRunAllCombined holds the two combined plans: on new-admin, Create
// carol and root login off in one lockout-risk plan, accounts before files
// before the reload, its undo, journalled, removing carol and putting root
// login back, the timer said; on ufw-inactive, Enable alone, root login
// off after the firewall (decision 5 (a)): the two in one plan are refused
// whole by the engine as two controls changing sshd and ufw
// (engine.ErrConflict).
func TestRunAllCombined(t *testing.T) {
	ctx := context.Background()
	r, h, err := raReadNamed(ctx, "new-admin")
	if err != nil || r.Refusal != "" || !r.Lockout() {
		t.Fatalf("new-admin: %v, refusal %q, lockout %v", err, r.Refusal, r.Lockout())
	}
	want := []engine.Stage{engine.StageAccounts, engine.StageFiles, engine.StageReload, engine.StageVerify}
	if got := raStages(r.Plan); !slices.Equal(got, want) {
		t.Errorf("new-admin: stages %v, want %v", got, want)
	}
	if _, err := raEngine(h, r.in.Session, r.Config, nil, &updQuiet).Apply(ctx, r.Plan); err != nil {
		t.Fatal(err)
	}
	st, err := revert.ReadState(ctx, h.system(), raRevert)
	if err != nil || st.Pending == nil {
		t.Fatalf("new-admin: no journal: %v", err)
	}
	j := st.Pending
	userdel := slices.ContainsFunc(j.Undo, func(s revert.Step) bool { return slices.Equal(s.Argv, []string{"userdel", "--remove", "carol"}) })
	root := j.Backup != nil && slices.ContainsFunc(j.Reload, func(rl revert.Reload) bool { return slices.Contains(rl.Keywords, "PermitRootLogin") })
	if !userdel || !root || j.Risk != revert.RiskLockout {
		t.Errorf("new-admin: the undo removes carol %v, puts root login back %v, risk %v", userdel, root, j.Risk)
	}
	code, out, errs, unread := playGallery([]string{"1", hostName, "0"}, "-profile", "none", "runall/new-admin")
	if code != exitOK || errs != "" || unread != 0 || !strings.Contains(flatText(out), "This change reverts automatically in") {
		t.Errorf("new-admin: exit %d, stderr %q, %d unread; want the timer said:\n%s", code, errs, unread, out)
	}

	r, h, err = raReadNamed(ctx, "ufw-inactive")
	if err != nil || r.Plan == nil || r.Refusal != "" || len(r.Plan.Refused) > 0 {
		t.Fatalf("ufw-inactive: %v, refusal %q; want the firewall's plan", err, r.Refusal)
	}
	want = []engine.Stage{engine.StageFirewall}
	login, _ := r.Row(ssh.Login{}.ID())
	if got := raStages(r.Plan); !slices.Equal(got, want) || !r.Lockout() || login.Kind != runall.Later || login.Words != runall.WordsAfterFirewall {
		t.Errorf("ufw-inactive: stages %v, lockout %v, root login %v %q; want %v, lockout, later", got, r.Lockout(), login.Kind, login.Words, want)
	}
	both := []engine.Control{runall.Asked(r.in, fwctl.ID).Control, runall.Asked(r.in, ssh.Login{}.ID()).Control}
	p, err := raEngine(h, r.in.Session, r.Config, nil, &updQuiet).Plan(ctx, both, r.Config)
	if err != nil || len(p.Refused) == 0 || !errors.Is(p.Refused[0], engine.ErrConflict) {
		t.Errorf("ufw-inactive: Enable and root login off in one plan: %v; want refused as a conflict", err)
	}
}

// TestRunAllKeyGates: hardn.yml's gates as decision 4 (a) maps them:
// configureDns false asks DNS for nothing whatever nameservers lists, and
// an admin already there who lacks a key hardn.yml asks for is User
// Management's, not as asked.
func TestRunAllKeyGates(t *testing.T) {
	ctx := context.Background()
	r, h, err := raReadNamed(ctx, "debian13")
	if err != nil {
		t.Fatal(err)
	}
	in := r.in
	cfg := *r.Config
	cfg.ConfigureDns = false
	in.Config = &cfg
	if c := runall.Asked(in, dnsctl.ID).Control; c != nil || len(cfg.Nameservers) == 0 {
		t.Errorf("configureDns false with %d nameservers: DNS asked %v", len(cfg.Nameservers), c)
	}
	cfg = *r.Config
	cfg.SshKeys = append(slices.Clone(cfg.SshKeys), synthKey(9, "george@laptop-2"))
	in.Config = &cfg
	in.NewEngine = func(progress func(engine.Event)) *engine.Engine {
		return raEngine(h, in.Session, &cfg, progress, &updQuiet)
	}
	rd, err := runall.Read(ctx, in)
	const want = "User Management's: 1 key missing"
	if got, _ := rd.Row(users.ID); err != nil || got.Words != want {
		t.Errorf("an admin lacking an asked key: %q, %v; want %q", got.Words, err, want)
	}
}
