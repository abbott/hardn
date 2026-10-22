package output_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
	"syscall"
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

// The real engine on a fake Debian 13 host it arms on, applies to and rolls
// back from (hostfake: every command succeeds, nothing outside the fake is
// touched), for what data fixtures cannot vouch for: what the engine
// reports, and what the document and the reports then say of it.

// journalPath is the change journal, whose next write armHost can fail.
const journalPath = revert.DefaultDir + "/" + revert.PendingName

// armHost is the fake host, with a journal write that can be made to fail
// (a full disk), as a step's journal after it would.
type armHost struct {
	*hostfake.System
	mu          sync.Mutex
	failJournal bool
}

func (h *armHost) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	h.mu.Lock()
	fail := name == journalPath && h.failJournal
	h.failJournal = h.failJournal && !fail
	h.mu.Unlock()
	if fail {
		return &fs.PathError{Op: "write", Path: name, Err: syscall.ENOSPC}
	}
	return h.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

// failNextJournal makes the next journal write fail.
func (h *armHost) failNextJournal() {
	h.mu.Lock()
	h.failJournal = true
	h.mu.Unlock()
}

func procStat(pid, ppid int, comm string) string {
	return fmt.Sprintf("%d (%s) S %d %d %d 34819 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 %d 0 0\n", pid, comm, ppid, pid, pid, pid*100)
}

// newArmHost is the fake host: hardn's binary, its boot unit installed
// already (so a lockout-risk plan has no boot unit change), an SSH session
// from 192.0.2.10, sshd's drop-in with Port 22.
func newArmHost(t *testing.T) *armHost {
	t.Helper()
	h := &armHost{System: hostfake.New(host.Options{})}
	h.AddFile(fixtureHardn, []byte("ELF"), 0o755, 0, 0)
	for _, d := range []string{"/var/lib", "/var/backups", "/run", "/etc/ssh/sshd_config.d", revert.SystemdUnitDir, "/var/log"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddFile(fixtureDropIn, []byte("# Managed by hardn\nPort 22\n"), 0o644, 0, 0)
	h.AddFile("/proc/self/stat", []byte(procStat(5000, 1, "hardn")), 0o444, 0, 0)
	h.AddFile("/proc/sys/kernel/random/boot_id", []byte("8e2a3c4d-1b2c-4d3e-8f40-0123456789ab\n"), 0o444, 0, 0)
	h.AddFile("/proc/900/stat", []byte(procStat(900, 1, "sshd")), 0o444, 0, 0)
	h.AddFile("/proc/3970/stat", []byte(procStat(3970, 900, "sshd-session")), 0o444, 0, 0)
	u, err := revert.PlanBootUnit(debian13().Init, fixtureHardn)
	if err != nil {
		t.Fatal(err)
	}
	h.AddFile(u.Path, u.Data, u.Mode, 0, 0)
	h.AddSymlink(u.Link, u.Path)
	h.Handler = func([]string, []byte) hostfake.Response { return hostfake.Response{} }
	return h
}

// portControl plans the changes it is given, as the SSH port control.
type portControl struct{ changes []engine.Change }

func (portControl) ID() string                                    { return "ssh.port" }
func (portControl) Title() string                                 { return "SSH Port" }
func (portControl) Applies(detect.Facts) (ok bool, reason string) { return true, "" }
func (portControl) Check(context.Context, host.System, detect.Facts) (engine.Status, error) {
	return engine.Status{State: engine.NotConfigured}, nil
}
func (c portControl) Plan(context.Context, host.System, engine.Input) ([]engine.Change, error) {
	return c.changes, nil
}

// runChange is a change that runs argv, undone by undo.
func runChange(id, summary string, stage engine.Stage, risk engine.RiskClass, argv []string, undo revert.Step) engine.Change {
	return engine.Change{ID: id, Control: "ssh.port", Summary: summary, Target: argv[0], Risk: risk, Stage: stage,
		Commands: []string{strings.Join(argv, " ")}, Rollback: engine.Undo{Steps: []revert.Step{undo}},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			_, err := sys.Run(ctx, argv[0], argv[1:]...)
			return engine.Ran{Detail: "ran"}, err
		}}
}

// allow2230 is ufw's change of the 2230 scenario.
func allow2230(risk engine.RiskClass) engine.Change {
	return runChange("ssh.port/firewall", "Allow 2230/tcp in ufw before sshd listens on it", engine.StageFirewall, risk,
		[]string{"ufw", "allow", "2230/tcp"},
		revert.Step{Op: revert.OpRun, Argv: []string{"ufw", "delete", "allow", "2230/tcp"}, Why: "delete the ufw rule for 2230/tcp that hardn added"})
}

// armAdapter is a FlowEngine over sys for changes, the engine's clock at
// 12:00:00 and its session an SSH connection.
func armAdapter(sys host.System, changes []engine.Change) *output.FlowEngine {
	a := &output.FlowEngine{Controls: []engine.Control{portControl{changes: changes}}, LogPath: output.LegacyLogPath}
	facts := debian13()
	a.Engine = engine.New(sys, engine.Options{
		Revert: revert.Options{Now: func() time.Time { return time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC) }, Hardn: fixtureHardn},
		Session: revert.Session{Remote: true, Conn: "192.0.2.10 50022 192.0.2.20 22",
			SSHD: revert.ProcStart{PID: 3970, Start: 3970 * 100}, Source: "SSH_CONNECTION"},
		Detect:   func(context.Context, host.System) (detect.Facts, error) { return facts, nil },
		Hold:     func() func() { return func() {} },
		PID:      5000,
		Backups:  backup.Settings{Path: "/var/backups/hardn", Enabled: true},
		Progress: a.Progress,
	})
	return a
}

// applyAll plans and applies through a, recording the progress reports;
// it returns the run, the reports, and Apply's error (a *flow.Failure).
func applyAll(ctx context.Context, t *testing.T, a *output.FlowEngine) (output.Run, []string, error) {
	t.Helper()
	if _, err := a.Plan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var r reports
	err := a.Apply(ctx, r.report)
	o := flow.Applied
	if err != nil {
		o = flow.Failed
	}
	return a.Run(o, err), r.lines, err
}

// humanText is Human's report of run as the plain cell writes it: no
// escape sequences.
func humanText(t *testing.T, run output.Run) string {
	t.Helper()
	env := plainEnv()
	var b bytes.Buffer
	if _, err := io.WriteString(env.Writer(&b), output.Human(env, run, fixtureNow)); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// squash is s with no white space, and no callout bar at the start of a
// line ("# " in plain mode), so that a sentence matches however it is
// wrapped (Lip Gloss wraps after a hyphen too), as prose or in a
// callout's body, and a step's row as its title and state.
func squash(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, "# ")
	}
	return strings.Join(strings.Fields(strings.Join(lines, "\n")), "")
}

// stable is run with its change id, random in its last part, the
// fixtures' instead, wherever the document takes it from (the plan, the
// result, the apply's error, the unjournalled change's backup directory),
// so that the goldens do not change from one run to the next. The run's
// error stays the result's.
func stable(run output.Run) output.Run {
	id, p := run.Plan.ID, run.Plan
	run.Plan = &engine.Plan{ID: fixtureID, Facts: p.Facts, Items: p.Items, Changes: p.Changes, Risk: p.Risk, Refused: p.Refused}
	res := *run.Result
	res.ID = fixtureID
	if f := res.Failed; f != nil {
		fc := *f
		fc.ID = fixtureID
		if u := fc.Unjournalled; u != nil {
			uc := *u
			uc.Dir = strings.ReplaceAll(uc.Dir, id, fixtureID)
			fc.Unjournalled = &uc
		}
		if run.Err == error(f) {
			run.Err = &fc
		}
		res.Failed = &fc
	}
	run.Result = &res
	return run
}

// TestRealUnjournalled: the journal of what a step ran fails (a full
// disk), so the change is not covered and the rollback, which finished,
// ran the rest. As the engine reports it: the change before rolled back,
// the change itself unjournalled, with what undoes it (its undo and its
// backups) in the document, the CHANGE_UNJOURNALLED error, the human
// report and the flow's failure; one code for the failed step everywhere.
func TestRealUnjournalled(t *testing.T) {
	sys := newArmHost(t)
	before, after := "# Managed by hardn\nPort 22\n", "# Managed by hardn\nPort 22\nPort 2230\n"
	files := engine.Change{ID: "ssh.port/files", Control: "ssh.port", Summary: "Write 00-hardn.conf",
		Target: fixtureDropIn, Diff: engine.FileDiff(fixtureDropIn, &before, &after), Risk: engine.RiskPlain, Stage: engine.StageFiles,
		Backups: []string{fixtureDropIn},
		Apply: func(ctx context.Context, s host.System, _ engine.Run) (engine.Ran, error) {
			if err := s.WriteFileAtomic(ctx, fixtureDropIn, []byte(after), 0o644, 0, 0); err != nil {
				return engine.Ran{}, err
			}
			sys.failNextJournal()
			undo := revert.Step{Op: revert.OpRun, Argv: []string{"systemctl", "reload", "ssh.service"}, Why: "reload sshd with the drop-in put back"}
			return engine.Ran{Detail: "written", Rollback: &engine.Undo{Steps: []revert.Step{undo}}}, nil
		}}
	a := armAdapter(sys, []engine.Change{allow2230(engine.RiskPlain), files})
	run, events, applyErr := applyAll(context.Background(), t, a)
	if run.Result == nil || run.Result.Failed == nil || run.Result.Failed.Unjournalled == nil || run.Result.Failed.RollbackErr != nil {
		t.Fatalf("not the unjournalled case: %v", run.Err)
	}
	d := output.Build(run)
	code := d.Errors[0].Code
	if want := []string{"0 Running", "0 Done", "1 Running", "1 Failed " + code, "0 Rolled back"}; strings.Join(events, "|") != strings.Join(want, "|") {
		t.Errorf("progress %q, want %q", events, want)
	}
	if d.Changes[0].Status != output.StatusRolledBack || d.Changes[1].Status != output.StatusUnjournalled {
		t.Errorf("statuses %s, %s; want rolled-back (as the engine reported it), unjournalled", d.Changes[0].Status, d.Changes[1].Status)
	}
	u := d.Result.Rollback.Unjournalled
	if u == nil || u.Change != "ssh.port/files" || strings.Join(u.Undo, "|") != "systemctl reload ssh.service" || len(u.Backups) != 1 ||
		u.Backups[0].Path != fixtureDropIn || u.Dir != "/var/backups/hardn/"+run.Plan.ID || !strings.Contains(u.Why, "no space left on device") {
		t.Fatalf("rollback.unjournalled %+v", u)
	}
	undo := fmt.Sprintf("systemctl reload ssh.service; restore %s from backup %d in /var/backups/hardn/%s", fixtureDropIn, u.Backups[0].Seq, run.Plan.ID)
	last := d.Errors[len(d.Errors)-1]
	if last.Code != "CHANGE_UNJOURNALLED" || !strings.Contains(last.Message, undo) {
		t.Errorf("errors %+v, want CHANGE_UNJOURNALLED with %q", d.Errors, undo)
	}
	h := squash(humanText(t, run))
	for _, want := range []string{"Allow 2230/tcp in ufw before sshd listens on it Rolled back", undo, "Write 00-hardn.conf Failed " + code} {
		if !strings.Contains(h, squash(want)) {
			t.Errorf("the human report has no %q:\n%s", want, humanText(t, run))
		}
	}
	var f *flow.Failure
	if !errors.As(applyErr, &f) || !strings.Contains(f.Next, undo) || !strings.Contains(f.Error(), "("+code+")") {
		t.Errorf("the flow's failure %v, next step %q; want %q and the code %s", applyErr, f.Next, undo, code)
	}
	pinned := stable(run)
	var b bytes.Buffer
	if err := output.WriteJSON(&b, output.Build(pinned)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), run.Plan.ID) {
		t.Fatalf("the change id is left in the document:\n%s", b.String())
	}
	golden(t, "json/unjournalled.json", b.Bytes())
	uitest.Golden(t, "human/unjournalled", uitest.Core(), func(env term.Env) string {
		return output.Human(env, pinned, fixtureNow)
	})
}

// TestRealCancelBetweenSteps: Ctrl-C lands as the first change finishes, so
// the engine stops before the next starts (StepStart) and rolls the first
// back. The next change never ran: not-run, with why, and never "failed";
// the run is cancelled (130), its rollback finished.
func TestRealCancelBetweenSteps(t *testing.T) {
	sys := newArmHost(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := allow2230(engine.RiskPlain)
	apply := first.Apply
	first.Apply = func(c context.Context, s host.System, r engine.Run) (engine.Ran, error) {
		ran, err := apply(c, s, r)
		cancel()
		return ran, err
	}
	reload := runChange("ssh.port/reload", "Reload ssh.service", engine.StageReload, engine.RiskPlain,
		[]string{"systemctl", "reload", "ssh.service"}, revert.Step{Op: revert.OpRun, Argv: []string{"systemctl", "reload", "ssh.service"}, Why: "reload ssh.service"})
	run, events, _ := applyAll(ctx, t, armAdapter(sys, []engine.Change{first, reload}))
	for _, argv := range sys.Runs() {
		if strings.Contains(strings.Join(argv, " "), "systemctl reload") {
			t.Fatalf("the reload ran: %v", argv)
		}
	}
	if want := []string{"0 Running", "0 Done", "0 Rolled back"}; strings.Join(events, "|") != strings.Join(want, "|") {
		t.Errorf("progress %q, want %q", events, want)
	}
	d := output.Build(run)
	if c := d.Changes[1]; c.Status != output.StatusNotRun || c.Detail != context.Canceled.Error() {
		t.Errorf("%s: %s %q, want not-run, with why", c.ID, c.Status, c.Detail)
	}
	if d.Changes[0].Status != output.StatusRolledBack || d.Result.Outcome != output.OutcomeCancelled || d.Result.ExitCode != output.ExitInterrupted ||
		!d.Result.Rollback.Finished || d.Result.Rollback.Step != "start" || d.Result.Rollback.Change != "ssh.port/reload" {
		t.Errorf("document %+v, rollback %+v", d.Changes, d.Result.Rollback)
	}
	h := humanText(t, run)
	if !strings.Contains(squash(h), squash("Reload ssh.service Skipped CANCELLED")) || strings.Contains(squash(h), squash("Reload ssh.service Failed")) {
		t.Errorf("human report:\n%s", h)
	}
}

// TestRealLockoutFlow: a lockout-risk change applied through the canonical
// flow, typed confirmation and all, waits for confirmation. The flow ends
// with "Operation Successful"; AfterFlow, written after it as FlowEngine
// says, shows the countdown, the confirm command, when the change is
// rolled back ("at about": the timer's own trigger time is not read on the
// fake) and how to undo it now.
func TestRealLockoutFlow(t *testing.T) {
	sys := newArmHost(t)
	a := armAdapter(sys, []engine.Change{allow2230(engine.RiskLockout)})
	env := plainEnv()
	var out bytes.Buffer
	p := nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, "web-01"), false), Out: env.Writer(&out)}
	confirm := func(flow.Plan) flow.Confirm {
		return flow.Confirm{Question: "Change the SSH port?", Label: "Type the hostname", Phrase: "web-01"}
	}
	o, err := flow.Flow{Engine: a, Confirm: confirm}.Run(context.Background(), p)
	run := a.Run(o, err)
	if _, werr := io.WriteString(p.Out, output.AfterFlow(env, run, time.Date(2026, 10, 19, 12, 0, 15, 0, time.UTC))); werr != nil {
		t.Fatal(werr)
	}
	d := output.Build(run)
	if o != flow.Applied || d.Result.Outcome != output.OutcomeAwaiting || d.Result.Confirm == nil || !d.Result.Confirm.Approximate {
		t.Fatalf("outcome %v (%v), document %+v", o, err, d.Result)
	}
	screen := squash(out.String())
	ok := strings.Index(screen, squash("Operation Successful"))
	for _, want := range []string{"reverts automatically in 2:45", "sudo hardn confirm", "rolled back at about 12:03:00 UTC",
		"To undo it now, run sudo hardn rollback " + run.Plan.ID} {
		if i := strings.Index(screen, squash(want)); i < 0 || i < ok {
			t.Errorf("no %q after the flow's result:\n%s", want, out.String())
		}
	}
}
