package flow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/diff"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// typist plays the user at a terminal, as nav's and form's tests do: each
// Read returns the next line, typed and ended with Enter, and echoes it to
// echo as a terminal shows it. With no lines left it reports the end of
// the input.
type typist struct {
	lines   []string
	pending []byte
	echo    io.Writer
}

func (t *typist) Read(p []byte) (int, error) {
	if len(t.pending) == 0 {
		if len(t.lines) == 0 {
			return 0, io.EOF
		}
		line := t.lines[0]
		t.lines = t.lines[1:]
		if _, err := io.WriteString(t.echo, line); err != nil {
			return 0, err
		}
		t.pending = []byte(line + "\n")
	}
	n := copy(p, t.pending)
	t.pending = t.pending[n:]
	return n, nil
}

// buffer is a strings.Builder that a spinner's goroutine can write to
// while a test reads it.
type buffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// play runs f in env with the user typing lines, and returns the outcome,
// the transcript as env.Writer passes it on, the echoed answers included,
// and the error.
func play(env term.Env, f Flow, lines ...string) (Outcome, string, error) {
	return playCtx(context.Background(), env, f, lines...)
}

// playCtx is play with ctx.
func playCtx(ctx context.Context, env term.Env, f Flow, lines ...string) (Outcome, string, error) {
	out := new(buffer)
	p := nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(&typist{lines: lines, echo: out}, false), Out: env.Writer(out)}
	o, err := f.Run(ctx, p)
	return o, out.String(), err
}

// plain80 is plain mode at 80 columns.
var plain80 = term.Fixed(80, colorprofile.NoTTY, false, true)

// The fixtures: Figma's mock host, bos-san-l01, and user, george.
const (
	hostname = "bos-san-l01"
	dropIn   = "/etc/ssh/sshd_config.d/00-hardn.conf"
	sudoers  = "/etc/sudoers.d/hardn-george"
)

// newUser holds the answers of the create-user form.
type newUser struct {
	name                   string
	sudo, password, addKey bool
	key                    string
}

// createUser is Figma's create-user action (USER-CREATE_USER) in the
// canonical flow: its four questions, as pkg/ui/form's fixture asks them;
// its "User Configuration" review, Figma's rows; a fixed plan; and its
// confirmation, "Create user 'george'?", asked after the plan, not as the
// form's fifth field (P-10).
func createUser(e Engine) Flow {
	u := new(newUser)
	return Flow{
		Form: form.Form{Fields: []form.Field{
			&form.Text{Title: "Username", Value: &u.name},
			&form.YesNo{Title: "Grant sudo privilege?", Value: &u.sudo},
			&form.YesNo{Title: "Require sudo password?", Value: &u.password, Skip: func() bool { return !u.sudo }},
			&form.YesNo{Title: "Add SSH key?", Value: &u.addKey},
			&form.Text{Title: "Paste SSH public key", Value: &u.key, Skip: func() bool { return !u.addKey }},
		}},
		Review: func() Review {
			sudo := component.KeyValue{Key: "Sudo", Value: "Enabled, No password", State: component.ValueBad}
			switch {
			case !u.sudo:
				sudo = component.KeyValue{Key: "Sudo", Value: "Disabled"}
			case u.password:
				sudo = component.KeyValue{Key: "Sudo", Value: "Enabled", State: component.ValueOK}
			}
			return Review{Title: "User Configuration", Items: []component.KeyValue{
				sudo, {Key: "SSH Keys", Value: "None", State: component.ValueWarn},
			}}
		},
		Engine:  e,
		Confirm: func(Plan) Confirm { return Confirm{Question: fmt.Sprintf("Create user '%s'?", u.name)} },
	}
}

// createUserPlan is what creating george with sudo and no password plans.
var createUserPlan = Plan{View: component.PlanView{Changes: []component.PlanChange{
	{Title: "Create user george", Commands: []string{"useradd --create-home --shell /bin/bash george"}},
	{Title: "Add george to the sudo group", Commands: []string{"usermod -aG sudo george"}},
	{
		Title: "Let george use sudo without a password", Target: sudoers,
		Diff:     diff.Unified(sudoers, sudoers, "", "george ALL=(ALL:ALL) NOPASSWD: ALL\n"),
		Commands: []string{"visudo -cf " + sudoers},
	},
}}}

// sudoFails fails createUserPlan at its second step, as on a host with no
// sudo group: george is rolled back, and the last step skipped.
var sudoFails = Fixture{Planned: createUserPlan, FailAt: 2, Detail: "exit status 6", Failure: Failure{
	Err:     errors.New("Adding george to the sudo group failed: exit status 6"),
	Command: "usermod -aG sudo george",
	Output:  "usermod: group 'sudo' does not exist\n",
	LogPath: "/var/log/hardn.log",
	Next:    "Check that the sudo group exists, then try again.",
}}

// disableRoot is Figma's root SSH login action (SSH_LOGIN-TOGGLE_CONFIRMATION)
// in the canonical flow: nothing to collect, its "SSH Configuration"
// review, with alice for Figma's allowed user, and its question, "Disable
// SSH login for 'root'?", asked as y/N, or above a typed confirmation of
// the hostname when the plan has lockout risk.
func disableRoot(e Engine) Flow {
	return Flow{
		Review: func() Review {
			return Review{Title: "SSH Configuration", Items: []component.KeyValue{
				{Key: "Root SSH Login", Value: "Enabled", State: component.ValueBad},
				{Key: "SSH Port", Value: "2230"},
				{Key: "Allowed Users", Value: "alice"},
			}}
		},
		Engine: e,
		Confirm: func(p Plan) Confirm {
			c := Confirm{Question: "Disable SSH login for 'root'?"}
			if p.Typed() {
				c.Label, c.Phrase = "Type the hostname", hostname
			}
			return c
		},
	}
}

// longHost is disableRoot on a host whose name, 57 cells, is too long to
// show whole beside "Type the hostname" under the 64-cell content cap,
// with its first label as the short phrase.
func longHost(e Engine) Flow {
	f := disableRoot(e)
	f.Confirm = func(Plan) Confirm {
		return Confirm{Question: "Disable SSH login for 'root'?", Label: "Type the hostname",
			Phrase: hostname + ".a-subdomain-long-enough-to-be-cut.example.net", Short: hostname}
	}
	return f
}

// rootDisabled is disableRoot on a host where root SSH login is disabled
// already, so that its engine plans nothing.
func rootDisabled(e Engine) Flow {
	f := disableRoot(e)
	f.Review = func() Review {
		return Review{Title: "SSH Configuration", Items: []component.KeyValue{
			{Key: "Root SSH Login", Value: "Disabled", State: component.ValueOK},
			{Key: "SSH Port", Value: "2230"},
			{Key: "Allowed Users", Value: "alice"},
		}}
	}
	return f
}

// rootLogin is the change disableRoot plans.
var rootLogin = component.PlanChange{
	Title: "Set PermitRootLogin no", Target: dropIn, LockoutRisk: true,
	Diff: diff.Unified(dropIn, dropIn,
		"# Managed by hardn\nPort 2230\nPermitRootLogin yes\n",
		"# Managed by hardn\nPort 2230\nPermitRootLogin no\n"),
	Commands: []string{"sshd -t", "systemctl reload ssh"},
}

// planOf returns a plan of changes at risk.
func planOf(risk Risk, changes ...component.PlanChange) Plan {
	return Plan{View: component.PlanView{Changes: changes}, Risk: risk}
}

// spy is an Engine around a Fixture that records whether Apply ran, and
// can fail to plan, or call hook as it plans.
type spy struct {
	Fixture
	planErr error
	hook    func()
	applied *bool
}

func (s spy) Plan(ctx context.Context) (Plan, error) {
	if s.hook != nil {
		s.hook()
	}
	if s.planErr != nil {
		return Plan{}, s.planErr
	}
	return s.Fixture.Plan(ctx)
}

func (s spy) Apply(ctx context.Context, report Report) error {
	*s.applied = true
	return s.Fixture.Apply(ctx, report)
}

// untitled is an action with nothing to collect or review, and an engine
// whose plan's first change has title, failing at its second change: for
// a title that shows nothing, the steps and the report name the change
// "Unknown", as PlanView does.
func untitled(title string) (func(Engine) Flow, spy) {
	f := func(e Engine) Flow {
		return Flow{Engine: e, Confirm: func(Plan) Confirm { return Confirm{Question: "Apply?"} }}
	}
	plan := planOf(RiskLow, component.PlanChange{Title: title}, component.PlanChange{Title: "Second change"})
	return f, spy{Fixture: Fixture{Planned: plan, FailAt: 2}}
}

// answers are Figma's answers to the create-user form.
var answers = []string{"george", "y", "n", "n"}

// TestPaths runs each path of the canonical flow and checks how it ends,
// whether anything was applied, and a line of its transcript.
func TestPaths(t *testing.T) {
	low, lockout := planOf(RiskLow, rootLogin), planOf(RiskLockout, rootLogin)
	low.View.Changes[0].LockoutRisk = false
	// A title of a zero-width space, a bell or a NUL alone shows nothing.
	zeroWidth, zeroWidthEngine := untitled("\u200b")
	bell, bellEngine := untitled("\a")
	null, nullEngine := untitled("\x00")
	labelless := func(e Engine) Flow {
		f := disableRoot(e)
		f.Confirm = func(Plan) Confirm { return Confirm{Question: "Type the hostname", Phrase: hostname} }
		return f
	}
	unknownSteps := "  Apply? [y/N, 0, b or q]: y\n\n  [+] Done: Unknown\n  [!] Failed: Second change\n\n# [!] Operation Failed\n\n  [<] Rolled back: Unknown\n"
	tests := []struct {
		name    string
		flow    func(Engine) Flow
		engine  spy
		dryRun  bool
		lines   []string
		want    Outcome
		applied bool
		holds   string
	}{
		{"applied", createUser, spy{Fixture: Fixture{Planned: createUserPlan}}, false, slices.Concat(answers, []string{"y"}), Applied, true,
			"  [+] Done: Let george use sudo without a password\n\n# [+] Operation Successful\n"},
		{"cancelled", createUser, spy{Fixture: Fixture{Planned: createUserPlan}}, false, slices.Concat(answers, []string{"n"}), Cancelled, false,
			"'george'? [y/N, 0, b or q]: n\n\n# [-] Operation Cancelled\n"},
		{"cancelled by the default", createUser, spy{Fixture: Fixture{Planned: createUserPlan}}, false, slices.Concat(answers, []string{""}), Cancelled, false,
			"  Create user 'george'? [y/N]: n\n\n# [-] Operation Cancelled\n"},
		{"back at the confirmation", createUser, spy{Fixture: Fixture{Planned: createUserPlan}}, false, slices.Concat(answers, []string{"0"}), Cancelled, false,
			"Operation Cancelled"},
		{"dry-run", createUser, spy{Fixture: Fixture{Planned: createUserPlan}}, true, answers, Planned, false,
			"  [i] Info: No actual changes were made to your system.\n"},
		{"failed", createUser, spy{Fixture: sudoFails}, false, slices.Concat(answers, []string{"y"}), Failed, true,
			"  [-] Skipped: Let george use sudo without a password\n"},
		{"failed at the last step", createUser, spy{Fixture: Fixture{Planned: createUserPlan, FailAt: 3}}, false, slices.Concat(answers, []string{"y"}), Failed, true,
			"  [<] Rolled back: Add george to the sudo group\n  [<] Rolled back: Create user george\n"},
		{"failed to plan", createUser, spy{planErr: errors.New("Reading /etc/passwd failed")}, false, answers, Failed, false,
			"# Reading /etc/passwd failed\n"},
		{"unchanged", rootDisabled, spy{}, false, nil, Unchanged, false,
			"\n\n  [i] Info: Nothing needs changing.\n"},
		{"typed", disableRoot, spy{Fixture: Fixture{Planned: lockout}}, false, []string{"y", hostname}, Applied, true,
			"  [!] Error: That does not match exactly. Please try again.\n"},
		{"typed for a change at lockout risk", disableRoot, spy{Fixture: Fixture{Planned: planOf(RiskLow, rootLogin)}}, false, []string{hostname}, Applied, true,
			"$ systemctl reload ssh\n\n  Disable SSH login for 'root'?\n\n  Type the hostname [bos-san-l01, 0, b or q]: bos-san-l01\n"},
		{"typed with the question for its label", labelless, spy{Fixture: Fixture{Planned: lockout}}, false, []string{hostname}, Applied, true,
			"$ systemctl reload ssh\n\n  Type the hostname [bos-san-l01, 0, b or q]: bos-san-l01\n"},
		{"y/N with no lockout risk", disableRoot, spy{Fixture: Fixture{Planned: low}}, false, []string{"y"}, Applied, true,
			"  Disable SSH login for 'root'? [y/N, 0, b or q]: y\n"},
		{"back at a typed confirmation", disableRoot, spy{Fixture: Fixture{Planned: lockout}}, false, []string{"b"}, Cancelled, false,
			"Operation Cancelled"},
		{"typed by the first label of a long hostname", longHost, spy{Fixture: Fixture{Planned: lockout}}, false, []string{hostname}, Applied, true,
			"  Type the hostname [bos-san-l01, 0, b or q]: bos-san-l01\n"},
		{"a zero-width title", zeroWidth, zeroWidthEngine, false, []string{"y"}, Failed, true, unknownSteps},
		{"a bell for a title", bell, bellEngine, false, []string{"y"}, Failed, true, unknownSteps},
		{"a NUL for a title", null, nullEngine, false, []string{"y"}, Failed, true, unknownSteps},
	}
	for _, tt := range tests {
		applied := false
		tt.engine.applied = &applied
		f := tt.flow(tt.engine)
		f.DryRun = tt.dryRun
		o, out, err := play(plain80, f, tt.lines...)
		if o != tt.want || err != nil || applied != tt.applied || !strings.Contains(out, tt.holds) {
			t.Errorf("%s: %d, %v, applied %t; want %d, nil, %t and the transcript to hold %q:\n%s", tt.name, o, err, applied, tt.want, tt.applied, tt.holds, out)
		}
	}
	if rep := failure(&Failure{}, nil); rep.Failed != "" {
		t.Errorf("a Failure with no Err reports %q as what failed, want nothing", rep.Failed)
	}
}

// TestAbort checks that the flow applies nothing when it is left before
// Apply: q and the end of the input at every prompt, back from the first
// field, and a context done before the run, while planning and before
// applying.
func TestAbort(t *testing.T) {
	type end struct {
		line string // "" ends the input
		want error
	}
	prompts := slices.Concat(answers, []string{"y"})
	check := func(ctx context.Context, name string, f func(Engine) Flow, s spy, lines []string, want error) {
		t.Helper()
		applied := false
		s.applied = &applied
		if o, out, err := playCtx(ctx, plain80, f(s), lines...); o != NotRun || !errors.Is(err, want) || applied {
			t.Errorf("%s: %d, %v, applied %t; want NotRun, %v, nothing applied:\n%s", name, o, err, applied, want, out)
		}
	}
	for k := range prompts {
		for _, e := range []end{{"q", nav.ErrQuit}, {"", prompt.ErrEndOfInput}} {
			lines := slices.Clone(prompts[:k])
			if e.line != "" {
				lines = append(lines, e.line)
			}
			check(context.Background(), fmt.Sprintf("%q at prompt %d", e.line, k+1), createUser, spy{Fixture: Fixture{Planned: createUserPlan}}, lines, e.want)
		}
	}
	check(context.Background(), "b at the first field", createUser, spy{Fixture: Fixture{Planned: createUserPlan}}, []string{"b"}, nav.ErrBack)
	lockout := spy{Fixture: Fixture{Planned: planOf(RiskLockout, rootLogin)}}
	check(context.Background(), "q at a typed confirmation", disableRoot, lockout, []string{"q"}, nav.ErrQuit)
	check(context.Background(), "the end of the input at a typed confirmation", disableRoot, lockout, nil, prompt.ErrEndOfInput)

	done, cancel := context.WithCancel(context.Background())
	cancel()
	check(done, "a context done before the run", createUser, spy{Fixture: Fixture{Planned: createUserPlan}}, nil, context.Canceled)
	ctx, cancel := context.WithCancel(context.Background())
	check(ctx, "a context done while planning", createUser, spy{Fixture: Fixture{Planned: createUserPlan}, hook: cancel}, answers, context.Canceled)
	ctx, cancel = context.WithCancel(context.Background())
	beforeApply := func(e Engine) Flow {
		f := createUser(e)
		confirm := f.Confirm
		f.Confirm = func(p Plan) Confirm { cancel(); return confirm(p) }
		return f
	}
	check(ctx, "a context done before applying", beforeApply, spy{Fixture: Fixture{Planned: createUserPlan}}, prompts, context.Canceled)
}

// TestRefused checks the flows that cannot be run, or not confirmed: no
// Engine, no Confirm, and a lockout-risk plan with no phrase to type,
// with a short phrase nav refuses, or with neither label nor question to
// ask it by. None applies anything, and none writes anything for the
// Confirm step, not even the gap: the transcript ends with the plan.
func TestRefused(t *testing.T) {
	applied := false
	s := spy{Fixture: Fixture{Planned: planOf(RiskLockout, rootLogin)}, applied: &applied}
	confirming := func(c Confirm) Flow {
		f := disableRoot(s)
		f.Confirm = func(Plan) Confirm { return c }
		return f
	}
	noConfirm := disableRoot(s)
	noConfirm.Confirm = nil
	tests := []struct {
		name  string
		flow  Flow
		want  error
		wrote bool
	}{
		{"no engine", Flow{Confirm: func(Plan) Confirm { return Confirm{} }}, ErrFlow, false},
		{"no confirm", noConfirm, ErrFlow, false},
		{"no phrase", confirming(Confirm{Question: "Disable SSH login for 'root'?", Label: "Type the hostname"}), nav.ErrPhrase, true},
		{"a short phrase that navigates", confirming(Confirm{Label: "Type the hostname", Phrase: hostname, Short: "b"}), nav.ErrPhrase, true},
		{"no label or question", confirming(Confirm{Phrase: hostname}), ErrFlow, true},
	}
	for _, tt := range tests {
		o, out, err := play(plain80, tt.flow)
		if o != NotRun || !errors.Is(err, tt.want) || applied || (out != "") != tt.wrote || tt.wrote && !strings.HasSuffix(out, "$ systemctl reload ssh\n") {
			t.Errorf("%s: %d, %v, applied %t, transcript %q", tt.name, o, err, applied, out)
		}
	}
}

// TestNarrow checks a typed confirmation at the widths around the
// narrowest that shows "bos-san-l01" whole beside "Type the hostname":
// 30 columns in Unicode glyphs and 32 in ASCII ones, plain mode's.
// Narrower, the flow says "Operation Cancelled" and that the terminal is
// too narrow, after one gap, with nothing asked or applied and no error,
// so that a menu carries on (TestReturn). A dotted phrase is never cut to
// fit: only a short phrase the action gives is asked in its place.
func TestNarrow(t *testing.T) {
	dotted := func(e Engine) Flow {
		f := disableRoot(e)
		f.Confirm = func(Plan) Confirm {
			return Confirm{Question: "Allow 2230/tcp?", Label: "Type the address", Phrase: "192.0.2.123"}
		}
		return f
	}
	tests := []struct {
		name  string
		env   term.Env
		flow  func(Engine) Flow
		lines []string
		want  Outcome
	}{
		{"Unicode, 29 columns", term.Fixed(29, colorprofile.ASCII, true, true), disableRoot, []string{hostname}, Cancelled},
		{"Unicode, 30 columns", term.Fixed(30, colorprofile.ASCII, true, true), disableRoot, []string{hostname}, Applied},
		{"plain, 31 columns", term.Fixed(31, colorprofile.NoTTY, false, true), disableRoot, []string{hostname}, Cancelled},
		{"plain, 32 columns", term.Fixed(32, colorprofile.NoTTY, false, true), disableRoot, []string{hostname}, Applied},
		{"a dotted phrase, 29 columns", term.Fixed(29, colorprofile.ASCII, true, true), dotted, []string{"192"}, Cancelled},
		{"a long hostname's short phrase, 30 columns", term.Fixed(30, colorprofile.ASCII, true, true), longHost, []string{hostname}, Applied},
	}
	for _, tt := range tests {
		applied := false
		o, out, err := play(tt.env, tt.flow(spy{Fixture: Fixture{Planned: planOf(RiskLockout, rootLogin)}, applied: &applied}), tt.lines...)
		out = ansi.Strip(out)
		asked := strings.Contains(out, ", 0, b or q]: ")
		// What follows the plan and one blank line: the callout's title,
		// then its body, each line after the bar and a space.
		_, after, said := strings.Cut(out, "$ systemctl reload ssh\n\n")
		lines := strings.Split(strings.TrimSuffix(after, "\n"), "\n")
		var body []string
		for _, l := range lines[1:] {
			_, text, _ := strings.Cut(l, " ")
			body = append(body, text)
		}
		said = said && strings.HasSuffix(lines[0], " Operation Cancelled") && strings.Join(body, " ") == tooNarrow
		if o != tt.want || err != nil || applied != (tt.want == Applied) || asked != (tt.want == Applied) || said == asked {
			t.Errorf("%s: %d, %v, applied %t, asked %t, said too narrow %t; want %d, nil:\n%s", tt.name, o, err, applied, asked, said, tt.want, out)
		}
	}
}

// TestReturn runs a flow as a menu's action: it starts apart from the
// answer that chose it, and once it returns the menu is drawn again under
// its result, which stays on screen, as brief 1C's Return step has it;
// so too when the terminal is too narrow for a typed confirmation.
func TestReturn(t *testing.T) {
	tests := []struct {
		name  string
		env   term.Env
		flow  Flow
		label string
		lines []string
		holds []string
	}{
		{"applied", plain80, createUser(Fixture{Planned: createUserPlan}), "Create User", slices.Concat(answers, []string{"y"}), []string{
			"  > Enter your choice [0-1, b or q]: 1\n\n   1. Username [0, b or q]: george\n",
			"# [+] Operation Successful\n\n   1) Create User\n",
		}},
		{"too narrow to confirm", term.Fixed(31, colorprofile.NoTTY, false, true), disableRoot(Fixture{Planned: planOf(RiskLockout, rootLogin)}), "Root SSH Login", nil, []string{
			"# again.\n\n   1) Root SSH Login\n",
		}},
	}
	for _, tt := range tests {
		out := new(buffer)
		lines := slices.Concat([]string{"1"}, tt.lines, []string{"q"})
		p := nav.Prompter{Ctx: component.NewCtx(tt.env), In: prompt.New(&typist{lines: lines, echo: out}, false), Out: tt.env.Writer(out)}
		menu := nav.Menu{Options: []nav.Option{{
			Item: component.MenuItem{Key: "1", Label: tt.label},
			Action: func() error {
				_, err := tt.flow.Run(context.Background(), p)
				return err
			},
		}}, Exit: component.MenuItem{Key: "0", Label: "Return"}}
		err := p.Run(func() nav.Screen { return nav.Screen{Menu: menu, Label: "Enter your choice"} })
		for _, want := range tt.holds {
			if !errors.Is(err, nav.ErrQuit) || !strings.Contains(out.String(), want) {
				t.Errorf("%s: Run: %v; transcript\n%s\nwant it to hold\n%s", tt.name, err, out, want)
			}
		}
	}
}

// failWriter fails each write once left writes have succeeded.
type failWriter struct{ left *int }

var errDiskFull = errors.New("disk full")

func (w failWriter) Write(b []byte) (int, error) {
	if *w.left == 0 {
		return 0, errDiskFull
	}
	*w.left--
	return len(b), nil
}

// TestWriteFails checks that a failed write ends the flow with the
// write's error: the review, before anything is applied, and a step's
// line, after which the apply still ends as it would have. The writes of
// the y/N path of disableRoot are the review, the plan, the gap before the
// question, the question, the reader's line break, then the step.
func TestWriteFails(t *testing.T) {
	low := planOf(RiskLow, rootLogin)
	low.View.Changes[0].LockoutRisk = false
	tests := []struct {
		name    string
		left    int
		want    Outcome
		applied bool
	}{
		{"the review", 0, NotRun, false},
		{"a step", 5, Applied, true},
	}
	for _, tt := range tests {
		applied := false
		p := nav.Prompter{Ctx: component.NewCtx(plain80), In: prompt.New(&typist{lines: []string{"y"}, echo: io.Discard}, false), Out: failWriter{&tt.left}}
		o, err := disableRoot(spy{Fixture: Fixture{Planned: low}, applied: &applied}).Run(context.Background(), p)
		if o != tt.want || !errors.Is(err, errDiskFull) || !strings.HasPrefix(err.Error(), "flow: write: ") || applied != tt.applied {
			t.Errorf("%s: %d, %v, applied %t; want %d, flow: write, %t", tt.name, o, err, applied, tt.want, tt.applied)
		}
	}
}

// csi matches the control sequences the flow writes: SGR, CPL and EL.
var csi = regexp.MustCompile(`^\x1b\[([0-9;]*)([A-Za-z])`)

// screen returns the text a terminal shows once it has received out: CPL
// moves up to the start of a line, EL 2 erases the line, and SGR is
// dropped. Text is written at the end of its line, as the flow writes it.
func screen(out string) string {
	lines, row := []string{""}, 0
	for out != "" {
		if m := csi.FindStringSubmatch(out); m != nil {
			n, _ := strconv.Atoi(m[1])
			switch m[2] {
			case "F":
				row -= max(n, 1)
			case "K":
				lines[row] = ""
			}
			out = out[len(m[0]):]
			continue
		}
		r, size := utf8.DecodeRuneInString(out)
		out = out[size:]
		if r != '\n' {
			lines[row] += string(r)
		} else if row++; row == len(lines) {
			lines = append(lines, "")
		}
	}
	return strings.Join(lines, "\n")
}

// TestProgress checks the Apply step where it animates: the steps drawn
// in place, the spinner turning while a step runs, and the screen left as
// the steps' lines without animation leave it; and, when the steps would
// not fit the terminal's height, one line per step with no cursor moved.
func TestProgress(t *testing.T) {
	defer func(d time.Duration) { spinEvery = d }(spinEvery)
	spinEvery = time.Millisecond
	env := term.Fixed(80, colorprofile.TrueColor, true, true)
	lines := slices.Concat(answers, []string{"y"})
	_, still, _ := play(env, createUser(Fixture{Planned: createUserPlan}), lines...)

	env.Animate = true
	out := new(buffer)
	turned := false
	slow := waiting{Fixture: Fixture{Planned: createUserPlan}, wait: func() {
		// The second frame shows that the spinner turned on its own.
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
			if turned = strings.Contains(out.String(), "⠙"); turned {
				return
			}
		}
	}}
	p := nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(&typist{lines: lines, echo: out}, false), Out: out}
	if o, err := createUser(slow).Run(context.Background(), p); o != Applied || err != nil {
		t.Fatalf("animated: %d, %v", o, err)
	}
	moved := strings.Count(out.String(), ansi.CursorPreviousLine(3))
	if got, want := screen(out.String()), ansi.Strip(still); !turned || moved < 7 || got != want {
		t.Errorf("animated: the spinner turned %t, drawn again %d times; the screen\n%s\nwant\n%s", turned, moved, got, want)
	}

	env.Height = 3
	if _, got, _ := play(env, createUser(Fixture{Planned: createUserPlan}), lines...); got != still {
		t.Errorf("3 steps on 3 lines: transcript\n%s\nwant\n%s", got, still)
	}
}

// waiting is a Fixture whose first step runs until wait returns.
type waiting struct {
	Fixture
	wait func()
}

func (w waiting) Apply(ctx context.Context, report Report) error {
	return w.Fixture.Apply(ctx, func(step int, state component.StepState, detail string) {
		report(step, state, detail)
		if step == 0 && state == component.StepRunning {
			w.wait()
		}
	})
}

// TestGolden pins each path's full transcript on the core set, the
// answers echoed as a terminal shows them: Figma's create-user action
// applied, cancelled as Figma draws it, planned in dry-run mode and failed
// and rolled back; and Figma's root SSH login action, at lockout risk,
// confirmed by the hostname after a refused line, and with nothing to
// change.
func TestGolden(t *testing.T) {
	yes, no := slices.Concat(answers, []string{"y"}), slices.Concat(answers, []string{"n"})
	dryRun := func(e Engine) Flow { f := createUser(e); f.DryRun = true; return f }
	tests := []struct {
		name   string
		flow   func(Engine) Flow
		engine Engine
		lines  []string
	}{
		{"applied", createUser, Fixture{Planned: createUserPlan}, yes},
		{"cancelled", createUser, Fixture{Planned: createUserPlan}, no},
		{"dry-run", dryRun, Fixture{Planned: createUserPlan}, answers},
		{"failed", createUser, sudoFails, yes},
		{"typed", disableRoot, Fixture{Planned: planOf(RiskLockout, rootLogin)}, []string{"y", hostname}},
		{"unchanged", rootDisabled, Fixture{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uitest.Golden(t, "flow/"+tt.name, uitest.Core(), func(env term.Env) string {
				_, out, err := play(env, tt.flow(tt.engine), tt.lines...)
				if err != nil {
					t.Errorf("%s: %v", env.Profile, err)
				}
				return strings.TrimSuffix(out, "\n")
			})
		})
	}
}
