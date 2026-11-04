package runall_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/header"
	planner "github.com/abbott/hardn/pkg/engine/runall"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	runallscreen "github.com/abbott/hardn/pkg/screens/runall"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The item's words a test looks for: Apply all with its description
// (gallery-runall.md §6), the refused and pending ones the planner's.
const (
	itemPlanned = "1) Apply all Plan, review, confirm"
	itemNothing = "1) Apply all Nothing to change"
	itemRefused = "1) Apply all " + planner.WordsRefused
	itemPending = "1) Apply all " + planner.WordsPending
)

// item is the item's words where r is the host as drawn.
func item(r planner.Reading) string {
	switch {
	case r.Refusal != "" && r.Pending():
		return itemPending
	case r.Refusal != "":
		return itemRefused
	case len(r.Planned()) > 0:
		return itemPlanned
	}
	return itemNothing
}

// TestHosts holds each host's screen to the planner, live and under plan
// only: every row's outcome drawn ("Refused:" where refused), the item as
// the reading says, the title the planner's; drawing writes nothing, every
// call one the fake answers, and makes no engine beyond the planner's own
// reading's.
func TestHosts(t *testing.T) {
	for _, name := range runHosts {
		r := newHost(t, name).read(t, name)
		reads := 0
		alone := newHost(t, name)
		in := alone.input()
		in.NewEngine = func(progress func(engine.Event)) *engine.Engine { reads++; return alone.newEngine(progress) }
		if _, err := planner.Read(context.Background(), in); err != nil {
			t.Fatal(err)
		}
		for _, dryRun := range []bool{false, true} {
			h := newHost(t, name)
			n := len(h.Calls())
			p := play(h, dryRun, "0")
			if p.err != nil || p.unread != 0 || !shows(p.out, "# "+planner.Title+" #") || !shows(p.out, item(r)) {
				t.Errorf("%s, plan only %v: Run = %v, %d unread; want the item %q:\n%s", name, dryRun, p.err, p.unread, item(r), p.out)
			}
			for _, rw := range r.Rows {
				words := rw.Words
				if rw.Kind == planner.Refused {
					words += ":"
				}
				if !shows(p.out, words) {
					t.Errorf("%s: row %s %q not drawn:\n%s", name, rw.ID, words, p.out)
				}
			}
			if w := writes(h, n); len(w) > 0 || p.engines != reads || len(p.runs) > 0 {
				t.Errorf("%s, plan only %v: drawing wrote %v, made %d engines (the planner %d), %d runs", name, dryRun, w, p.engines, reads, len(p.runs))
			}
			checkHost(t, name, h.sshHost)
		}
	}
}

// TestSaidBeforeAsking: on every host, Apply all says each refused row's
// words whole ("<Row> skipped: <refusal>") before its review and question,
// and a change pending or the one plan's refusal in place of them, asking
// nothing; declined at the question, nothing is written.
func TestSaidBeforeAsking(t *testing.T) {
	for _, name := range runHosts {
		r := newHost(t, name).read(t, name)
		lines := []string{"1", "0", "0"}
		if r.Refusal != "" || len(r.Planned()) == 0 {
			lines = lines[1:]
			lines[0] = "1"
		}
		h := newHost(t, name)
		n := len(h.Calls())
		p := play(h, false, lines...)
		if p.err != nil || p.unread != 0 {
			t.Errorf("%s %q: Run = %v, %d unread:\n%s", name, lines, p.err, p.unread, p.out)
		}
		question := at(p.out, planner.Question)
		for _, rw := range r.Refused() {
			if i := at(p.out, " skipped: "+rw.Refusal); i < 0 || question >= 0 && i > question {
				t.Errorf("%s: %s's refusal not said before the question:\n%s", name, rw.ID, p.out)
			}
		}
		switch {
		case r.Refusal != "":
			if !shows(p.out, r.Refusal) || asked(p.out) {
				t.Errorf("%s: the refusal %q not said, or asked:\n%s", name, r.Refusal, p.out)
			}
		case len(r.Planned()) > 0:
			if question < 0 || !strings.Contains(p.out, typeIt) {
				t.Errorf("%s: the hostname not asked:\n%s", name, p.out)
			}
		default:
			if asked(p.out) {
				t.Errorf("%s: nothing planned, but asked:\n%s", name, p.out)
			}
		}
		if w := writes(h, n); len(w) > 0 {
			t.Errorf("%s: wrote %v", name, w)
		}
		checkHost(t, name, h.sshHost)
	}
}

// stages are p's stages in order, each once, the boot unit's left out.
func stages(p *engine.Plan) []engine.Stage {
	var out []engine.Stage
	for _, c := range p.Changes {
		if c.Stage != engine.StageBoot && (len(out) == 0 || out[len(out)-1] != c.Stage) {
			out = append(out, c.Stage)
		}
	}
	return out
}

// TestApplyAll: on new-admin, carol and root login off in one lockout-risk
// plan, its review by control, the hostname typed, applied, the timer
// said, the journal's undo removing carol and putting root login back; the
// screen drawn again reads Change pending, and choosing it says the
// planner's words. On alpine323 the firewall skipped and the rest applied;
// on fresh a plain plan, the hostname typed all the same, no timer.
func TestApplyAll(t *testing.T) {
	h := newHost(t, "new-admin")
	p := play(h, false, "1", hostName, "1", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil || !shows(p.out, applied) || !shows(p.out, timer) {
		t.Fatalf("new-admin: Run = %v, runs %+v:\n%s", p.err, p.runs, p.out)
	}
	if review := p.out[strings.Index(p.out, "# Apply all"):]; !shows(review, "- User Create carol, 1 key - SSH Login Root login off") || !shows(review, planner.Question) {
		t.Errorf("new-admin: the review by control or the question not shown:\n%s", p.out)
	}
	want := []engine.Stage{engine.StageAccounts, engine.StageFiles, engine.StageReload, engine.StageVerify}
	if got := stages(p.runs[0].Plan); !slices.Equal(got, want) || p.runs[0].Plan.Risk != engine.RiskLockout {
		t.Errorf("new-admin: stages %v, risk %v; want %v, lockout", got, p.runs[0].Plan.Risk, want)
	}
	st, err := revert.ReadState(context.Background(), h, revert.Options{Now: clock, Hardn: hardnBinary})
	if err != nil || st.Pending == nil {
		t.Fatalf("new-admin: no journal: %v", err)
	}
	j := st.Pending
	userdel := slices.ContainsFunc(j.Undo, func(s revert.Step) bool { return slices.Equal(s.Argv, []string{"userdel", "--remove", "carol"}) })
	root := j.Backup != nil && slices.ContainsFunc(j.Reload, func(rl revert.Reload) bool { return slices.Contains(rl.Keywords, "PermitRootLogin") })
	if !userdel || !root || j.Risk != revert.RiskLockout {
		t.Errorf("new-admin: the undo removes carol %v, puts root login back %v, risk %v", userdel, root, j.Risk)
	}
	after := p.out[strings.LastIndex(p.out, "Operation Successful"):]
	if !shows(after, itemPending) || !shows(after, planner.PendingWords) || asked(after) {
		t.Errorf("new-admin: drawn again without Change pending:\n%s", after)
	}
	checkHost(t, "new-admin", h.sshHost)

	h = newHost(t, "alpine323")
	p = play(h, false, "1", hostName, "0")
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err != nil || !shows(p.out, applied) || !shows(p.out, timer) ||
		at(p.out, "Firewall skipped:") < 0 || at(p.out, "Firewall skipped:") > at(p.out, planner.Question) {
		t.Errorf("alpine323: Run = %v, runs %+v:\n%s", p.err, p.runs, p.out)
	}
	checkHost(t, "alpine323", h.sshHost)

	h = newHost(t, "fresh")
	p = play(h, false, "1", hostName, "0")
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err != nil || p.runs[0].Plan.Risk == engine.RiskLockout ||
		!strings.Contains(p.out, typeIt+" [") || !shows(p.out, applied) || shows(p.out, timer) {
		t.Errorf("fresh: Run = %v, runs %+v; want a plain plan typed and applied:\n%s", p.err, p.runs, p.out)
	}
	if _, err := h.LookupUser(context.Background(), "george"); err != nil {
		t.Error("fresh: george not created")
	}
	checkHost(t, "fresh", h.sshHost)
}

// TestPlanOnly: under plan only Apply all stops at its plan on every host
// with something planned, the hostname unread and not asked, and nothing
// outside the engine's run directory changes.
func TestPlanOnly(t *testing.T) {
	for _, name := range runHosts {
		r := newHost(t, name).read(t, name)
		h := newHost(t, name)
		_ = h.Remove(context.Background(), header.HostnameFile)
		n := len(h.Calls())
		p := play(h, true, "1", "0")
		stopped := r.Refusal == "" && len(r.Planned()) > 0
		if p.err != nil || p.unread != 0 || shows(p.out, planned) != stopped || asked(p.out) || shows(p.out, cantConfirm) || len(p.runs) > 0 != stopped {
			t.Errorf("%s: Run = %v, stopped at the plan %v, want %v:\n%s", name, p.err, shows(p.out, planned), stopped, p.out)
		}
		if w := writes(h, n); len(w) > 0 {
			t.Errorf("%s: plan only wrote %v", name, w)
		}
		checkHost(t, name, h.sshHost)
	}
}

// TestHostname: a live lockout-risk plan whose hostname can't be read says
// so before asking anything; a plain one (fresh) says so in place of the
// question; nothing is written.
func TestHostname(t *testing.T) {
	for _, name := range []string{"debian13", "fresh"} {
		h := newHost(t, name)
		_ = h.Remove(context.Background(), header.HostnameFile)
		n := len(h.Calls())
		p := play(h, false, "1", "0")
		if p.err != nil || p.unread != 0 || !shows(p.out, cantConfirm) || asked(p.out) || shows(p.out, applied) {
			t.Errorf("%s: Run = %v:\n%s", name, p.err, p.out)
		}
		if name == "debian13" && shows(p.out, planner.Question) {
			t.Errorf("%s: the plan shown before the refusal:\n%s", name, p.out)
		}
		if w := writes(h, n); len(w) > 0 {
			t.Errorf("%s: wrote %v", name, w)
		}
	}
}

// TestPhrase: a wrong phrase asks again; 0 then declines, nothing written.
func TestPhrase(t *testing.T) {
	h := newHost(t, "debian13")
	n := len(h.Calls())
	p := play(h, false, "1", "WEB-01", "0", "0")
	if p.err != nil || p.unread != 0 || shows(p.out, applied) || strings.Count(p.out, typeIt) != 2 || len(writes(h, n)) > 0 {
		t.Errorf("a wrong phrase, then 0: Run = %v, wrote %v:\n%s", p.err, writes(h, n), p.out)
	}
	if len(p.runs) != 1 || !p.runs[0].Declined {
		t.Errorf("runs %+v, want one declined", p.runs)
	}
}

// TestApplyContext: the apply runs under the context ApplyContext makes,
// stopped once it returns; plan only makes none. A cancelled apply is
// rolled back, and the screen goes on.
func TestApplyContext(t *testing.T) {
	for _, tc := range []struct {
		dryRun bool
		made   int
		lines  []string
	}{{false, 1, []string{"1", hostName, "0"}}, {true, 0, []string{"1", "0"}}} {
		h := newHost(t, "debian13")
		made, stopped := 0, 0
		p := playWith(h, tc.dryRun, func(o *runallscreen.Options) {
			o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
				made++
				c, cancel := context.WithCancel(ctx)
				return c, func() { stopped++; cancel() }
			}
		}, tc.lines...)
		if p.err != nil || made != tc.made || stopped != made {
			t.Errorf("plan only %v: Run = %v, %d contexts made, %d stopped, want %d:\n%s", tc.dryRun, p.err, made, stopped, tc.made, p.out)
		}
	}
	h := newHost(t, "debian13")
	before, _ := h.ReadFile(context.Background(), sshd.HardnDropIn)
	p := playWith(h, false, func(o *runallscreen.Options) {
		o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
			c, cancel := context.WithCancel(ctx)
			cancel()
			return c, cancel
		}
	}, "1", hostName, "0")
	after, _ := h.ReadFile(context.Background(), sshd.HardnDropIn)
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err == nil || !bytes.Equal(before, after) ||
		!strings.Contains(p.out[strings.LastIndex(p.out, planner.Question):], "# Run All #") {
		t.Errorf("cancelled: Run = %v, runs %+v, %q:\n%s", p.err, p.runs, after, p.out)
	}
}

// TestNavigation: 0 returns, q quits, and the end of the input aborts, from
// the screen and the question, writing nothing; an unavailable item and a
// key that is none are refused and asked again.
func TestNavigation(t *testing.T) {
	for _, tt := range []struct {
		host  string
		lines []string
		want  error
	}{
		{"debian13", []string{"0"}, nil}, {"debian13", []string{"q"}, nav.ErrQuit}, {"debian13", []string{"1", "q"}, nav.ErrQuit},
		{"debian13", nil, prompt.ErrAborted}, {"debian13", []string{"1"}, prompt.ErrAborted}, {"debian13", []string{"9", "0"}, nil},
		{"debian13", []string{"1", "0", "0"}, nil}, {"hardened", []string{"1", "0"}, nil}, {"pending", []string{"1", "q"}, nav.ErrQuit},
	} {
		h := newHost(t, tt.host)
		n := len(h.Calls())
		p := play(h, false, tt.lines...)
		if !errors.Is(p.err, tt.want) || (tt.want == nil) != (p.err == nil) || p.unread != 0 {
			t.Errorf("%s %q: Run = %v, want %v, %d unread:\n%s", tt.host, tt.lines, p.err, tt.want, p.unread, p.out)
		}
		if w := writes(h, n); len(w) > 0 {
			t.Errorf("%s %q: wrote %v", tt.host, tt.lines, w)
		}
	}
}

// TestReadFails: a reading that fails (the change journal unread) draws
// Run All's row refused, in the error's words, never an empty screen, and
// the item refused, which says them, writing nothing.
func TestReadFails(t *testing.T) {
	h := newHost(t, "debian13")
	n := len(h.Calls())
	p := playWith(h, false, func(o *runallscreen.Options) { o.System = brokenJournal{h} }, "1", "0")
	if p.err != nil || !shows(p.out, "Run All Refused:") || !shows(p.out, itemRefused) || !shows(p.out, "permission denied.") || asked(p.out) {
		t.Errorf("Run = %v:\n%s", p.err, p.out)
	}
	if w := writes(h, n); len(w) > 0 {
		t.Errorf("wrote %v", w)
	}
}

// brokenJournal is a host whose change journal's directory can't be read.
type brokenJournal struct{ *runHost }

func (b brokenJournal) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	if name == revert.DefaultDir {
		return nil, fs.ErrPermission
	}
	return b.runHost.Lstat(ctx, name)
}

// TestRunNeeds: Run refuses, wrapping fs.ErrInvalid and writing nothing, no
// Prompter's In or Out, no System, Getenv or NewEngine. A nil Config asks
// nothing.
func TestRunNeeds(t *testing.T) {
	h := newHost(t, "debian13")
	n := len(h.Calls())
	var out bytes.Buffer
	in := prompt.New(strings.NewReader("0\n"), false)
	newEngine := func(func(engine.Event)) *engine.Engine { return nil }
	for _, o := range []runallscreen.Options{
		{}, {System: h, Getenv: h.getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in}, System: h, Getenv: h.getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{Out: &out}, System: h, Getenv: h.getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, Getenv: h.getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, System: h, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, System: h, Getenv: h.getenv},
	} {
		if err := runallscreen.Run(context.Background(), o); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Run(%+v) = %v, want fs.ErrInvalid", o, err)
		}
	}
	if len(h.Calls()) > n || out.Len() > 0 {
		t.Errorf("refused options made %d calls, wrote %q", len(h.Calls())-n, out.String())
	}
	p := playWith(h, false, func(o *runallscreen.Options) { o.Config = nil }, "0")
	if p.err != nil || !shows(p.out, itemNothing) || !shows(p.out, planner.WordsNothing) {
		t.Errorf("no hardn.yml: Run = %v:\n%s", p.err, p.out)
	}
}
