package output

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// Run is what one run of hardn did, as the engine returned it: the input of
// Build, ExitCode, Human and the audit log. engine.Plan, Report and Result
// are not the wire format (engine.md §7): Build maps them.
type Run struct {
	Mode         Mode
	HardnVersion string
	Hostname     string
	// Facts are the host's; nil takes Plan.Facts, and with no plan the
	// host is all "unknown".
	Facts   *detect.Facts
	Reports []engine.Report // ModeCheck: Engine.Check's
	Plan    *engine.Plan    // ModePlan, ModeApply: Engine.Plan's
	// RiskLevel is a check's risk level (risk.Assess over Reports and the
	// header probe), not a plan's risk class: its cross-checks' findings,
	// which no report carries, go in findings[] (next/p4-risk). The level
	// itself is the main menu's (StatusViewOf), not the document's.
	RiskLevel *risk.Level
	// Result is Engine.Apply's, once it ran.
	Result *engine.Result
	// Declined: the user declined the plan at its confirmation, so
	// nothing was applied (an interactive apply). A run whose Err is
	// nav.ErrQuit or nav.ErrBack (q or 0 at one of the flow's prompts) is
	// declined too, and that is not an error.
	Declined bool
	// Err is the run's error: Engine.Check's, Plan's or Apply's, a usage
	// error, a cancellation. A plan's refusals need not be in it: Build
	// reads Plan.Refused. An ApplyError may be wrapped: it is reported
	// once, as Result.Failed.
	Err error
	// Deprecations are the configuration's (Config.Deprecations), read
	// from ConfigPath.
	Deprecations []config.Deprecation
	ConfigPath   string
	// Reviewed: pkg/ui/flow's Plan step listed the plan's findings
	// before its confirmation (FlowEngine.Run), so that AfterFlow does
	// not repeat them. It changes nothing in the document.
	Reviewed bool
}

// facts are r's host facts.
func (r Run) facts() detect.Facts {
	switch {
	case r.Facts != nil:
		return *r.Facts
	case r.Plan != nil:
		return r.Plan.Facts
	}
	return detect.Facts{}
}

// declined reports whether the user declined the run's changes: at the
// confirmation (Declined), or by quitting (q) or going back (0) at one of
// the flow's prompts before anything was applied (pkg/ui/flow returns
// nav.ErrQuit or nav.ErrBack with NotRun).
func (r Run) declined() bool { return r.Declined || navigated(r.Err) }

// navigated reports whether err is the user quitting or going back.
func navigated(err error) bool { return errors.Is(err, nav.ErrQuit) || errors.Is(err, nav.ErrBack) }

// policyMoot reports whether r's error is the non-interactive policy's
// refusal (ErrConfirmationRequired) for a plan with nothing to change:
// nothing would have been applied, so it is not an error (Confirmation).
func (r Run) policyMoot() bool {
	return errors.Is(r.Err, ErrConfirmationRequired) && r.Plan != nil && r.Plan.Empty() && len(r.Plan.Refused) == 0
}

// planFindings are plan p's findings, as the document lists them: the
// plan's as a whole first (P-41: a best-effort release), then each
// change's, in the plan's order.
func planFindings(p *engine.Plan) []Finding {
	if p == nil {
		return nil
	}
	var out []Finding
	for _, f := range p.Findings {
		out = append(out, Finding{Code: f.Code, Detail: f.Detail, Source: f.Source})
	}
	for _, c := range p.Changes {
		for _, f := range c.Findings {
			out = append(out, Finding{Code: f.Code, Detail: f.Detail, Source: f.Source, Control: c.Control, Change: c.ID})
		}
	}
	return out
}

// Build maps run r to its document. It is pure.
func Build(r Run) Document {
	d := Document{
		SchemaVersion: SchemaVersion,
		HardnVersion:  r.HardnVersion,
		Host:          hostOf(r.Hostname, r.facts()),
		Mode:          r.Mode,
		Changes:       []Change{},
		Status:        []Status{},
		Findings:      []Finding{},
		Errors:        errorsOf(r),
	}
	o := outcomeOf(r)
	d.Result = Result{Outcome: o, ExitCode: o.Exit()}
	for _, rep := range r.Reports {
		d.Status = append(d.Status, Status{Control: rep.Control, Title: rep.Title, State: stateWord(rep.Status.State),
			Detail: rep.Status.Detail, Evidence: nonNil(rep.Status.Evidence)})
		for _, f := range rep.Status.Findings {
			d.Findings = append(d.Findings, Finding{Code: f.Code, Detail: f.Detail, Source: f.Source, Control: rep.Control})
		}
	}
	if r.RiskLevel != nil {
		for _, f := range r.RiskLevel.Findings {
			d.Findings = append(d.Findings, Finding{Code: f.Code, Detail: f.Detail, Source: f.Source})
		}
	}
	if p := r.Plan; p != nil {
		d.Result.ID, d.Result.Risk = p.ID, riskWord(p.Risk)
		d.Findings = append(d.Findings, planFindings(p)...)
		status := changeStatus(r)
		for _, c := range p.Changes {
			d.Changes = append(d.Changes, changeOf(c, status))
		}
	}
	for _, dep := range r.Deprecations {
		src := r.ConfigPath
		if dep.Line > 0 {
			src += ":" + strconv.Itoa(dep.Line)
		}
		d.Findings = append(d.Findings, Finding{Code: CodeDeprecated, Detail: dep.String(), Source: src})
	}
	if res := r.Result; res != nil {
		if res.ID != "" {
			d.Result.ID = res.ID
		}
		if c := res.Confirm; c != nil {
			d.Result.Confirm = confirmOf(*c, r.facts())
		}
		if res.Failed != nil {
			d.Result.Rollback = rollbackOf(res.Failed, r.facts())
		}
		if b := res.Boot; b != nil {
			d.Result.Boot = &Boot{Installed: b.Err == nil, Detail: b.Detail}
		}
	}
	return d
}

// changeStatus returns each change's status by its id, from the run's
// mode and result: planned for a check or plan; for an apply, from
// Result.Applied, Result.Boot and Result.Failed, as the engine's progress
// events report them (engine.md §7, §2.15). A change done is rolled-back
// when the rollback in this run finished (RollbackErr nil: the engine then
// reports each done change but the unjournalled one rolled back), and
// unknown when it did not finish or another process had the change, since
// whether it still stands is then not established. The change an apply
// stopped at before it started (StepStart, StepWindow) is not-run, with
// why.
func changeStatus(r Run) func(id string) (status, detail string) {
	res := r.Result
	if r.Mode != ModeApply || res == nil || res.Nothing {
		return func(string) (string, string) { return StatusPlanned, "" }
	}
	applied := map[string]string{}
	for _, a := range res.Applied {
		applied[a.Change] = a.Detail
	}
	f := res.Failed
	return func(id string) (string, string) {
		if id == engine.BootChangeID && res.Boot != nil {
			if res.Boot.Err != nil {
				return StatusFailed, res.Boot.Err.Error()
			}
			return StatusApplied, res.Boot.Detail
		}
		detail, ok := applied[id]
		switch {
		case f != nil && f.Unjournalled != nil && f.Unjournalled.Change == id:
			return StatusUnjournalled, errText(f.Unjournalled.Err)
		case f != nil && f.Change == id && (f.Step == engine.StepStart || f.Step == engine.StepWindow):
			return StatusNotRun, errText(f.Err)
		case f != nil && f.Change == id:
			return StatusFailed, errText(f.Err)
		case ok && f != nil && f.RollbackErr == nil:
			return StatusRolledBack, detail
		case ok && f != nil:
			return StatusUnknown, detail
		case ok:
			return StatusApplied, detail
		}
		return StatusNotRun, ""
	}
}

// errText is err's text; "" for nil.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func changeOf(c engine.Change, status func(string) (string, string)) Change {
	st, detail := status(c.ID)
	return Change{ID: c.ID, Control: c.Control, Summary: c.Summary, Target: c.Target, Risk: riskWord(c.Risk),
		Stage: stageWord(c.Stage), After: nonNil(c.After), Diff: c.Diff, StateBefore: c.StateBefore, StateAfter: c.StateAfter,
		Commands: nonNil(c.Commands), Backups: nonNil(c.Backups), Status: st, Detail: detail}
}

// confirmOf is how change c is kept: hardn confirm with the host's
// elevation tool, from a new connection to one of c.Via.
func confirmOf(c engine.Confirm, f detect.Facts) *Confirm {
	via := []string{}
	for _, l := range c.Via {
		via = append(via, l.String())
	}
	elevate := elevation(f)
	return &Confirm{Deadline: c.Deadline.UTC(), Approximate: c.Approximate, Via: via,
		Command: elevate + " hardn confirm", Undo: elevate + " hardn rollback " + c.ID}
}

// elevation is the command that runs hardn as root on the host: doas where
// sudo is not installed and doas is (Alpine's default, sudoers.md), else
// sudo, the brief's words.
func elevation(f detect.Facts) string {
	if f.Elevation.Sudo.Present == detect.No && f.Elevation.Doas.Present == detect.Yes {
		return "doas"
	}
	return "sudo"
}

// rollbackOf is what rolling back the failed apply ae did, on a host with
// facts f: the command that finishes an unfinished rollback is in the
// host's elevation tool, as the confirm command is.
func rollbackOf(ae *engine.ApplyError, f detect.Facts) *Rollback {
	rr := ae.Rollback
	rb := &Rollback{Finished: errors.Is(ae, engine.ErrRolledBack), ByAnotherProcess: errors.Is(ae.RollbackErr, revert.ErrRollingBack),
		Step: stepWord(ae.Step), Change: ae.Change, ChangeUndone: ae.Undone, Restored: []string{}, Undone: []string{}}
	for _, r := range rr.Restored {
		if r.Err == nil {
			rb.Restored = append(rb.Restored, r.Path)
		}
	}
	for _, u := range rr.Undone {
		if u.Err == nil {
			rb.Undone = append(rb.Undone, u.Step.Why)
		}
	}
	if u := ae.Unjournalled; u != nil {
		rb.Unjournalled = &Unjournalled{Change: u.Change, Undo: undoWords(u.Undo), Backups: []Backup{}, Dir: u.Dir, Why: errText(u.Err)}
		for _, e := range u.Backups {
			rb.Unjournalled.Backups = append(rb.Unjournalled.Backups, Backup{Path: e.Path, Seq: e.Seq})
		}
	}
	if errors.Is(ae.RollbackErr, revert.ErrIncomplete) {
		rb.Command = elevation(f) + " hardn rollback " + ae.ID
	}
	return rb
}

// undoWords are u's steps and reloads in words, in the order to run them,
// as the engine names them (ApplyError's text): a command as its argv, a
// unit's kept plan as its rollback, any other step as its operation and
// path, then each reload.
func undoWords(u engine.Undo) []string {
	out := []string{}
	for _, s := range u.Steps {
		switch s.Op {
		case revert.OpRun:
			out = append(out, host.FormatArgv(s.Argv))
		case revert.OpPlan:
			out = append(out, "the "+s.Unit+" plan's rollback")
		default:
			out = append(out, string(s.Op)+" "+s.Path)
		}
	}
	for _, r := range u.Reloads {
		out = append(out, "reload "+string(r.Service))
	}
	return out
}

// unjournalledWords is what undoes the unjournalled change u in one line:
// its undo, then each backup to restore.
func unjournalledWords(u Unjournalled) string {
	parts := slices.Clone(u.Undo)
	for _, b := range u.Backups {
		parts = append(parts, fmt.Sprintf("restore %s from backup %d in %s", b.Path, b.Seq, u.Dir))
	}
	if len(parts) == 0 {
		return "nothing journalled undoes it"
	}
	return strings.Join(parts, "; ")
}

// errorsOf is every error of run r, each once: the check's, the plan's
// refusals one by one, and the apply's (its step's error, its rollback's,
// a change it did not cover, the boot unit's).
func errorsOf(r Run) []Error {
	out := []Error{}
	var seen []error
	add := func(e Error, err error) {
		if err == nil || slices.ContainsFunc(seen, func(s error) bool { return sameErr(s, err) }) {
			return
		}
		seen = append(seen, err)
		out = append(out, e)
	}
	for _, rep := range r.Reports {
		add(errorOfReport(rep), rep.Err)
	}
	owner := map[string]string{} // a refusal's text, and its control
	if p := r.Plan; p != nil {
		for _, it := range p.Items {
			if it.Err != nil {
				owner[it.Err.Error()] = it.Control
			}
		}
		for _, err := range p.Refused {
			add(errorOf(err, owner[err.Error()], ""), err)
		}
	}
	if res := r.Result; res != nil {
		if b := res.Boot; b != nil && b.Err != nil {
			e := errorOf(b.Err, "", engine.BootChangeID)
			e.Step = stepWord(engine.StepBoot)
			add(e, b.Err)
			if errors.Is(r.Err, b.Err) {
				seen = append(seen, r.Err)
			}
		}
		if ae := res.Failed; ae != nil {
			seen = append(seen, ae)
			var wrapped *engine.ApplyError
			if errors.As(r.Err, &wrapped) && wrapped == ae {
				seen = append(seen, r.Err) // the run's error is the apply's, wrapped
			}
			e := errorOf(ae.Err, "", ae.Change)
			e.Step = stepWord(ae.Step)
			add(e, ae.Err)
			if ae.RollbackErr != nil {
				e := errorOf(ae.RollbackErr, "", "")
				e.Step = "rollback"
				add(e, ae.RollbackErr)
			}
			if rb := rollbackOf(ae, r.facts()); rb.Unjournalled != nil {
				u := rb.Unjournalled
				add(Error{Code: "CHANGE_UNJOURNALLED", Change: u.Change, Step: stepWord(ae.Step),
					Message: fmt.Sprintf("%s ran, and the journal does not hold what undoes it (%s), so what it did may be left: %s", u.Change, u.Why, unjournalledWords(*u))},
					fmt.Errorf("unjournalled: %s", u.Change))
			}
		}
	}
	if err := r.Err; err != nil && !navigated(err) && !r.policyMoot() && !slices.ContainsFunc(seen, func(s error) bool { return sameErr(s, err) }) {
		for _, one := range refusals(err) {
			add(errorOf(one, owner[one.Error()], ""), one)
		}
	}
	if p := r.Plan; p != nil {
		for i, e := range out {
			for _, c := range p.Changes {
				if e.Control == "" && e.Change != "" && c.ID == e.Change {
					out[i].Control = c.Control
				}
			}
		}
	}
	return out
}

func errorOfReport(rep engine.Report) Error {
	if rep.Err == nil {
		return Error{}
	}
	return errorOf(rep.Err, rep.Control, "")
}

// refusals splits a plan's refusals (Plan.Err: ErrRefused with every
// refusal joined) into each; any other error is itself.
func refusals(err error) []error {
	var joined interface{ Unwrap() []error }
	if !errors.Is(err, engine.ErrRefused) || !errors.As(err, &joined) {
		return []error{err}
	}
	var out []error
	for _, e := range joined.Unwrap() {
		if e == engine.ErrRefused {
			continue
		}
		if j, ok := e.(interface{ Unwrap() []error }); ok {
			out = append(out, j.Unwrap()...)
		} else {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return []error{err}
	}
	return out
}

// sameErr reports whether a and b are the same error value. Values of a
// struct or array type are never compared: such a type is comparable even
// when it holds an interface whose dynamic value is not, and comparing two
// of those panics.
func sameErr(a, b error) bool {
	if a == nil || b == nil {
		return false
	}
	t := reflect.TypeOf(a)
	if k := t.Kind(); k == reflect.Struct || k == reflect.Array {
		return false
	}
	return t == reflect.TypeOf(b) && t.Comparable() && a == b
}

// errorOf is err as a document's error, about control and change.
func errorOf(err error, control, change string) Error {
	e := Error{Code: CodeOf(err), Message: err.Error(), Control: control, Change: change}
	var ref *steps.Refusal
	if errors.As(err, &ref) {
		e.Why, e.Fix = ref.Why, ref.Fix
	}
	return e
}

// The schema's own words for the engine's and detect's values (detect.md
// §6, engine.md §7): tables, so that a change of a String method is not a
// change of the schema.
var (
	stateWords = map[engine.State]string{engine.Unknown: "unknown", engine.Configured: "configured",
		engine.NotConfigured: "not-configured", engine.Partial: "partial", engine.NotApplicable: "not-applicable"}
	riskWords = map[engine.RiskClass]string{engine.RiskPlain: "plain", engine.RiskPackageInstall: "package-install",
		engine.RiskServiceRestart: "service-restart", engine.RiskLockout: "lockout"}
	stageWords = map[engine.Stage]string{engine.StageBoot: "boot", engine.StagePackages: "packages", engine.StageAccounts: "accounts",
		engine.StageFirewall: "firewall", engine.StageFiles: "files", engine.StageReload: "reload", engine.StageVerify: "verify"}
	stepWords = map[string]string{engine.StepBoot: "boot-unit", engine.StepStart: "start", engine.StepWindow: "window",
		engine.StepBackup: "backup", engine.StepJournal: "journal", engine.StepApply: "apply",
		engine.StepJournalAfter: "journal-after", engine.StepMarkApplied: "mark-applied", engine.StepDone: "done"}
	triWords     = map[detect.Tri]string{detect.Unknown: "unknown", detect.No: "no", detect.Yes: "yes"}
	initWords    = map[detect.InitKind]string{detect.Systemd: "systemd", detect.OpenRC: "openrc"}
	sshWords     = map[detect.SSHMode]string{detect.SSHSocket: "ssh.socket", detect.SSHService: "ssh.service", detect.SSHOpenRC: "openrc"}
	supportWords = map[detect.Support]string{detect.Supported: "supported", detect.BestEffort: "best-effort",
		detect.OutOfScope: "out-of-scope", detect.Unlisted: "unlisted"}
	distroWords = map[detect.Distro]string{detect.DistroDebian: "debian", detect.DistroUbuntu: "ubuntu",
		detect.DistroAlpine: "alpine", detect.DistroProxmox: "proxmox", detect.DistroOther: "other"}
	flavourWords = map[detect.SudoFlavor]string{detect.SudoOriginal: "sudo", detect.SudoRS: "sudo-rs"}
)

// word is m[k], or "unknown" for a value the table does not name.
func word[K comparable](m map[K]string, k K) string {
	if w, ok := m[k]; ok {
		return w
	}
	return "unknown"
}

func stateWord(s engine.State) string     { return word(stateWords, s) }
func riskWord(r engine.RiskClass) string  { return word(riskWords, r) }
func stageWord(s engine.Stage) string     { return word(stageWords, s) }
func stepWord(s string) string            { return word(stepWords, s) }
func triWord(t detect.Tri) string         { return word(triWords, t) }
func supportWord(s detect.Support) string { return word(supportWords, s) }

// hostOf maps detect's facts to the schema's Host.
func hostOf(hostname string, f detect.Facts) Host {
	return Host{
		Hostname:  hostname,
		OS:        OS{ID: f.OS.ID, VersionID: f.OS.VersionID, Codename: f.OS.VersionCodename, PrettyName: f.OS.PrettyName},
		Platform:  Platform{Distro: word(distroWords, f.Platform.Distro), Version: f.Platform.Version, Support: supportWord(f.Platform.Support)},
		Init:      word(initWords, f.Init.Kind),
		SSH:       word(sshWords, f.SSH.Mode),
		Proxmox:   Proxmox{Installed: triWord(f.Proxmox.Installed), Version: f.Proxmox.Version},
		Container: Container{Detected: triWord(f.Container.Detected), Manager: f.Container.Manager},
		Elevation: Elevation{Sudo: triWord(f.Elevation.Sudo.Present), SudoFlavor: word(flavourWords, f.Elevation.SudoFlavor),
			Doas: triWord(f.Elevation.Doas.Present)},
	}
}

// nonNil is s, or an empty slice for nil, so that JSON writes [].
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
