package output_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The flow adapter runs the real engine over the recording fake, with a
// synthetic control: its changes write files under /etc/example, with no
// lockout risk, so no timer and no boot unit; one can be made to fail.

const exampleConf = "/etc/example/web.conf"

type exampleControl struct {
	failSecond bool  // the second change's Apply fails
	refuse     error // Plan refuses
	// findings ride on the first change; the first of them on the second
	// too.
	findings []engine.Finding
}

func (exampleControl) ID() string                                    { return "example.web" }
func (exampleControl) Title() string                                 { return "Example web server" }
func (exampleControl) Applies(detect.Facts) (ok bool, reason string) { return true, "" }
func (exampleControl) Check(context.Context, host.System, detect.Facts) (engine.Status, error) {
	return engine.Status{State: engine.NotConfigured}, nil
}

func (c exampleControl) Plan(_ context.Context, sys host.System, _ engine.Input) ([]engine.Change, error) {
	if c.refuse != nil {
		return nil, c.refuse
	}
	if data, err := sys.ReadFile(context.Background(), exampleConf); err == nil && string(data) == "listen 192.0.2.20:443\n" {
		return nil, nil // as asked: idempotent
	}
	write := func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
		return engine.Ran{Detail: "written"}, sys.WriteFileAtomic(ctx, exampleConf, []byte("listen 192.0.2.20:443\n"), 0o644, 0, 0)
	}
	second := func(context.Context, host.System, engine.Run) (engine.Ran, error) {
		if c.failSecond {
			return engine.Ran{}, fmt.Errorf("example: %w", &host.ExitError{Argv: []string{"nginx", "-t"}, ExitCode: 1, Tail: []string{"nginx: configuration file test failed"}})
		}
		return engine.Ran{Detail: "checked"}, nil
	}
	before, after := "listen 80\n", "listen 192.0.2.20:443\n"
	return []engine.Change{
		{ID: "example.web/files", Summary: "Listen on 192.0.2.20:443 in " + exampleConf, Target: exampleConf,
			Diff: engine.FileDiff(exampleConf, &before, &after), Risk: engine.RiskPlain, Stage: engine.StageFiles,
			Backups: []string{exampleConf}, Apply: write, Findings: c.findings},
		{ID: "example.web/verify", Summary: "Check the configuration with nginx -t", Target: "nginx", Commands: []string{"nginx -t"},
			Risk: engine.RiskPlain, Stage: engine.StageVerify, Apply: second, Findings: c.findings[:min(1, len(c.findings))]},
	}, nil
}

// otherControl is exampleControl under another ID, so that a plan can
// hold both.
type otherControl struct{ exampleControl }

func (otherControl) ID() string { return "example.other" }

// newFlowHost is a fake Debian 13 host with /etc/example/web.conf.
func newFlowHost() *hostfake.System {
	h := hostfake.New(host.Options{})
	for _, d := range []string{"/var/lib", "/var/backups", "/run", "/etc/example", "/var/log"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddFile(exampleConf, []byte("listen 80\n"), 0o644, 0, 0)
	h.AddFile("/proc/self/stat", []byte("5000 (hardn) S 1 5000 5000 34819 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 500000 0 0\n"), 0o444, 0, 0)
	h.AddFile("/proc/sys/kernel/random/boot_id", []byte("8e2a3c4d-1b2c-4d3e-8f40-0123456789ab\n"), 0o444, 0, 0)
	return h
}

// newAdapter is a FlowEngine over sys for c, its engine's Progress its own.
func newAdapter(sys host.System, c exampleControl) *output.FlowEngine {
	a := &output.FlowEngine{Controls: []engine.Control{c}, LogPath: output.LegacyLogPath}
	now := func() time.Time { return time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC) }
	facts := debian13()
	a.Engine = engine.New(sys, engine.Options{
		Revert:   revert.Options{Now: now, Hardn: fixtureHardn},
		Detect:   func(context.Context, host.System) (detect.Facts, error) { return facts, nil },
		Hold:     func() func() { return func() {} },
		PID:      5000,
		Backups:  backup.Settings{Path: "/var/backups/hardn", Enabled: true},
		Progress: a.Progress,
	})
	return a
}

// reports records what Apply reported, as "index state detail".
type reports struct {
	mu    sync.Mutex
	lines []string
}

func (r *reports) report(step int, state component.StepState, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, strings.TrimSpace(fmt.Sprintf("%d %s %s", step, state, detail)))
}

func TestFlowEngineApplies(t *testing.T) {
	ctx := context.Background()
	sys := newFlowHost()
	a := newAdapter(sys, exampleControl{})
	plan, err := a.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Risk != flow.RiskLow || len(plan.View.Changes) != 2 || plan.View.Changes[0].Title != "Listen on 192.0.2.20:443 in "+exampleConf ||
		plan.View.Changes[0].Target != exampleConf || !strings.Contains(plan.View.Changes[0].Diff, "+listen 192.0.2.20:443") ||
		plan.View.Changes[1].Commands[0] != "nginx -t" {
		t.Fatalf("plan = %+v", plan)
	}
	var r reports
	if err := a.Apply(ctx, r.report); err != nil {
		t.Fatal(err)
	}
	if want := []string{"0 Running", "0 Done", "1 Running", "1 Done"}; strings.Join(r.lines, "|") != strings.Join(want, "|") {
		t.Errorf("reports %q, want %q", r.lines, want)
	}
	run := a.Run(flow.Applied, nil)
	d := output.Build(run)
	if d.Result.Outcome != output.OutcomeApplied || output.ExitCode(run) != output.ExitOK || d.Changes[0].Status != output.StatusApplied ||
		d.Changes[1].Detail != "checked" {
		t.Errorf("document %+v", d)
	}
	// A report after Apply returned goes nowhere.
	a.Progress(engine.Event{Index: 0, State: engine.EventDone})
	if len(r.lines) != 4 {
		t.Errorf("a report after Apply: %q", r.lines)
	}
}

func TestFlowEngineFailure(t *testing.T) {
	ctx := context.Background()
	sys := newFlowHost()
	a := newAdapter(sys, exampleControl{failSecond: true})
	if _, err := a.Plan(ctx); err != nil {
		t.Fatal(err)
	}
	var r reports
	err := a.Apply(ctx, r.report)
	var f *flow.Failure
	if !errors.As(err, &f) {
		t.Fatalf("Apply = %v, want a *flow.Failure", err)
	}
	if want := []string{"0 Running", "0 Done", "1 Running", "1 Failed COMMAND_FAILED", "0 Rolled back"}; strings.Join(r.lines, "|") != strings.Join(want, "|") {
		t.Errorf("reports %q, want %q", r.lines, want)
	}
	if !strings.HasPrefix(f.Error(), "Check the configuration with nginx -t: example: host: nginx -t: exit status 1 (COMMAND_FAILED)") ||
		f.Command != "nginx -t" || f.Output != "nginx: configuration file test failed" || f.LogPath != output.LegacyLogPath {
		t.Errorf("failure %q, command %q, output %q, log %q", f.Error(), f.Command, f.Output, f.LogPath)
	}
	if !errors.Is(err, engine.ErrRolledBack) {
		t.Errorf("the failure does not unwrap to the engine's: %v", err)
	}
	if e, _ := sys.Entry(exampleConf); string(e.Data) != "listen 80\n" {
		t.Errorf("not restored: %q", e.Data)
	}
	run := a.Run(flow.Failed, nil)
	d := output.Build(run)
	if d.Result.Outcome != output.OutcomeRolledBack || output.ExitCode(run) != output.ExitError || d.Changes[0].Status != output.StatusRolledBack ||
		d.Changes[1].Status != output.StatusFailed || len(d.Errors) != 1 || d.Errors[0].Code != "COMMAND_FAILED" || d.Errors[0].Control != "example.web" {
		t.Errorf("document %+v", d)
	}
}

func TestFlowEngineRefused(t *testing.T) {
	a := newAdapter(newFlowHost(), exampleControl{refuse: fmt.Errorf("example: %w", engine.ErrUnsupported)})
	_, err := a.Plan(context.Background())
	var f *flow.Failure
	if !errors.As(err, &f) || !strings.Contains(f.Error(), "(PLATFORM_UNSUPPORTED)") {
		t.Fatalf("Plan = %v", err)
	}
	run := a.Run(flow.Failed, nil)
	if d := output.Build(run); d.Result.Outcome != output.OutcomeRefused || output.ExitCode(run) != output.ExitError ||
		len(d.Errors) != 1 || d.Errors[0].Control != "example.web" {
		t.Errorf("document %+v", d)
	}
}

// TestFlowEngineInAFlow runs pkg/ui/flow's canonical flow on the adapter:
// a dry run, then planned, confirmed with y, applied, as a person sees it;
// then the same action again, which has nothing to change, as an apply
// and as a dry run; then a run aborted at the confirmation (the end of the
// input), which is 130; then declined with n, and quit with q, which are 3
// and no error.
func TestFlowEngineInAFlow(t *testing.T) {
	sys := newFlowHost()
	env := plainEnv()
	confirm := func(flow.Plan) flow.Confirm { return flow.Confirm{Question: "Apply these changes?"} }
	run := func(dryRun bool, lines ...string) (output.Run, string) {
		a := newAdapter(sys, exampleControl{})
		a.DryRun = dryRun
		var out bytes.Buffer
		p := nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, lines...), false), Out: env.Writer(&out)}
		o, err := flow.Flow{Engine: a, Confirm: confirm, DryRun: dryRun}.Run(context.Background(), p)
		return a.Run(o, err), out.String()
	}
	r, _ := run(true)
	if d := output.Build(r); d.Mode != output.ModePlan || d.Result.Outcome != output.OutcomeChangesNeeded || output.ExitCode(r) != output.ExitChanges {
		t.Errorf("dry run: %s %+v", d.Mode, d.Result)
	}
	r, out := run(false, "y")
	if output.ExitCode(r) != output.ExitOK || !strings.Contains(squash(out), squash("Listen on 192.0.2.20:443 in /etc/example/web.conf Done")) || !strings.Contains(out, "Operation Successful") {
		t.Errorf("exit %d:\n%s", output.ExitCode(r), out)
	}
	r, out = run(false)
	if d := output.Build(r); d.Mode != output.ModeApply || d.Result.Outcome != output.OutcomeNothingToDo || !strings.Contains(out, "Nothing needs changing.") {
		t.Errorf("again: %s %+v\n%s", d.Mode, d.Result, out)
	}
	r, _ = run(true)
	if d := output.Build(r); d.Mode != output.ModePlan || d.Result.Outcome != output.OutcomeNothingToDo {
		t.Errorf("again, a dry run: %s %+v", d.Mode, d.Result)
	}
	sys.AddFile(exampleConf, []byte("listen 80\n"), 0o644, 0, 0)
	r, _ = run(false)
	if output.ExitCode(r) != output.ExitInterrupted || output.Build(r).Errors[0].Code != "CANCELLED" {
		t.Errorf("aborted: exit %d, %+v", output.ExitCode(r), output.Build(r).Errors)
	}
	for _, answer := range []string{"n", "q"} {
		r, _ = run(false, answer)
		if d := output.Build(r); output.ExitCode(r) != output.ExitChanges || d.Result.Outcome != output.OutcomeDeclined || len(d.Errors) != 0 {
			t.Errorf("%s: exit %d, %+v, errors %+v", answer, output.ExitCode(r), d.Result, d.Errors)
		}
	}
	if e, _ := sys.Entry(exampleConf); string(e.Data) != "listen 80\n" {
		t.Errorf("declined, yet changed: %q", e.Data)
	}
	// Back (0) from the form's first field ends the flow before the plan.
	r = newAdapter(sys, exampleControl{}).Run(flow.NotRun, nav.ErrBack)
	if d := output.Build(r); output.ExitCode(r) != output.ExitChanges || d.Result.Outcome != output.OutcomeDeclined || len(d.Errors) != 0 {
		t.Errorf("back: exit %d, %+v, errors %+v", output.ExitCode(r), d.Result, d.Errors)
	}
}

// TestAfterFlow pins what AfterFlow adds below a flow that applied the
// port plan, waiting for confirmation, having listed the plan's finding
// before its confirmation (Run.Reviewed): the countdown, when it is
// rolled back ("at about") and how to undo it, and not the finding again.
// A run whose findings the flow did not list (a refused plan's, or a flow
// that ended before listing them) has them after the countdown, as a
// warning with its code and source (after-flow-not-listed).
func TestAfterFlow(t *testing.T) {
	fx := fixtures()[3]
	reviewed := fx.run
	reviewed.Reviewed = true
	uitest.Golden(t, "human/after-flow", uitest.Core(), func(env term.Env) string {
		return output.AfterFlow(env, reviewed, fixtureNow)
	})
	uitest.Golden(t, "human/after-flow-not-listed", uitest.Core(), func(env term.Env) string {
		return output.AfterFlow(env, fx.run, fixtureNow)
	})
	const finding = "PasswordAuthentication yes in 50-cloud-init.conf is read before 00-hardn.conf, so sshd uses it (SSHD_OVERRIDDEN"
	for _, tc := range []struct {
		name string
		run  output.Run
		want bool
	}{{"listed", reviewed, false}, {"not listed", fx.run, true}} {
		got := squash(output.AfterFlow(plainEnv(), tc.run, fixtureNow))
		if strings.Contains(got, squash(finding)) != tc.want || !strings.Contains(got, squash("reverts automatically")) {
			t.Errorf("%s: the countdown and the finding (want %t):\n%s", tc.name, tc.want, got)
		}
	}
	if got := output.AfterFlow(plainEnv(), output.Run{Mode: output.ModeApply, Plan: &engine.Plan{ID: fixtureID},
		Result: &engine.Result{Nothing: true}}, fixtureNow); got != "" {
		t.Errorf("nothing to add: %q", got)
	}
	if d, dr := output.Build(fx.run), output.Build(reviewed); !reflect.DeepEqual(d, dr) {
		t.Errorf("Reviewed changes the document:\n%+v\n%+v", d, dr)
	}
}

// TestFlowEngineFindings runs the canonical flow on the adapter over a
// control whose two changes carry one finding, the first change another
// too: the flow is handed each once, in the plan's order, each a warning
// with its code; its Plan step lists them before the question, and
// AfterFlow does not say them again, after an apply, a decline, a dry run,
// a failure, or q and the end of the input at the confirmation. A plan
// refused by a second control lists none, and AfterFlow reports them
// after the refusal, as warnings with their codes, as it did every
// finding before; so it does for a flow that ended before listing them
// (a context done as it planned). The document's findings are the same
// whichever.
func TestFlowEngineFindings(t *testing.T) {
	fs := []engine.Finding{
		{Code: "EXAMPLE_INCLUDE", Detail: "conf.d/old.conf also listens on 443", Source: "/etc/example/conf.d/old.conf"},
		{Code: "EXAMPLE_TLS", Detail: "No certificate for 192.0.2.20 yet"},
	}
	want := []flow.Finding{{Text: fs[0].Detail, Code: fs[0].Code}, {Text: fs[1].Detail, Code: fs[1].Code}}
	env := plainEnv()
	type ran struct {
		run              output.Run
		screen, after    string
		listed, repeated int
	}
	play := func(c exampleControl, refused, dryRun bool, lines ...string) ran {
		sys := newFlowHost()
		a := newAdapter(sys, c)
		if refused {
			a.Controls = append(a.Controls, otherControl{exampleControl{refuse: fmt.Errorf("example: %w", engine.ErrUnsupported)}})
		}
		a.DryRun = dryRun
		var out bytes.Buffer
		p := nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, lines...), false), Out: env.Writer(&out)}
		o, err := flow.Flow{Engine: a, Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: "Listen on 443?"} }, DryRun: dryRun}.Run(context.Background(), p)
		r := ran{run: a.Run(o, err), screen: out.String()}
		r.after = output.AfterFlow(env, r.run, fixtureNow)
		question := strings.Index(r.screen, "Listen on 443?")
		for _, f := range fs {
			i := strings.Index(r.screen, f.Detail)
			if i >= 0 && (question < 0 || i < question) {
				r.listed++
			}
			r.repeated += strings.Count(squash(r.after), squash(f.Detail+" ("+f.Code))
		}
		return r
	}
	a := newAdapter(newFlowHost(), exampleControl{findings: fs})
	if p, err := a.Plan(context.Background()); err != nil || !slices.Equal(p.Findings, want) {
		t.Fatalf("Plan = %+v, %v; want the findings %+v", p.Findings, err, want)
	}
	for _, tc := range []struct {
		name             string
		c                exampleControl
		refused, dryRun  bool
		lines            []string
		listed, repeated int
	}{
		{"applied", exampleControl{findings: fs}, false, false, []string{"y"}, 2, 0},
		{"declined", exampleControl{findings: fs}, false, false, []string{"n"}, 2, 0},
		{"a dry run", exampleControl{findings: fs}, false, true, nil, 2, 0},
		{"failed", exampleControl{findings: fs, failSecond: true}, false, false, []string{"y"}, 2, 0},
		{"q at the confirmation", exampleControl{findings: fs}, false, false, []string{"q"}, 2, 0},
		{"the end of the input at the confirmation", exampleControl{findings: fs}, false, false, nil, 2, 0},
		{"refused", exampleControl{findings: fs}, true, false, nil, 0, 2},
	} {
		r := play(tc.c, tc.refused, tc.dryRun, tc.lines...)
		if r.listed != tc.listed || r.repeated != tc.repeated {
			t.Errorf("%s: listed before the question %d, after the flow %d; want %d, %d:\n%s%s", tc.name, r.listed, r.repeated, tc.listed, tc.repeated, r.screen, r.after)
		}
		if n := len(output.Build(r.run).Findings); n != 3 {
			t.Errorf("%s: the document holds %d findings, want 3 (one twice, on two changes)", tc.name, n)
		}
	}
	a = newAdapter(newFlowHost(), exampleControl{findings: fs})
	if _, err := a.Plan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if after := squash(output.AfterFlow(env, a.Run(flow.NotRun, context.Canceled), fixtureNow)); !strings.Contains(after, squash(fs[1].Detail+" ("+fs[1].Code)) {
		t.Errorf("a flow done as it planned: AfterFlow does not report the findings:\n%s", after)
	}
	// A finding whose detail shows nothing, empty, blank or escape
	// sequences alone, is no line of the flow's; AfterFlow reports it once,
	// by its code and source, applied or declined, and the one the flow
	// listed not again: never missed.
	for _, detail := range []string{"", "   ", "\x1b[2J"} {
		blank := engine.Finding{Code: "EXAMPLE_EMPTY", Detail: detail, Source: "/etc/example/conf.d/x.conf"}
		for _, line := range []string{"y", "n"} {
			r := play(exampleControl{findings: []engine.Finding{blank, fs[1]}}, false, false, line)
			after := squash(r.after)
			if !r.run.Reviewed || strings.Contains(r.screen, blank.Code) || strings.Count(after, squash("("+blank.Code+", "+blank.Source+")")) != 1 ||
				strings.Contains(after, squash(fs[1].Detail)) {
				t.Errorf("a detail %q, answered %s: reviewed %t; want the blank finding after the flow once, the other not:\n%s%s", detail, line, r.run.Reviewed, r.screen, r.after)
			}
		}
	}
}
