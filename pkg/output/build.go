package output

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
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
	// Result is Engine.Apply's, once it ran.
	Result *engine.Result
	// Declined: the user declined the plan at its confirmation, so
	// nothing was applied (an interactive apply).
	Declined bool
	// Err is the run's error: Engine.Check's, Plan's or Apply's, a usage
	// error, a cancellation. A plan's refusals need not be in it: Build
	// reads Plan.Refused.
	Err error
	// Deprecations are the configuration's (Config.Deprecations), read
	// from ConfigPath.
	Deprecations []config.Deprecation
	ConfigPath   string
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
	if p := r.Plan; p != nil {
		d.Result.ID, d.Result.Risk = p.ID, riskWord(p.Risk)
		status := changeStatus(r)
		for _, c := range p.Changes {
			d.Changes = append(d.Changes, changeOf(c, status))
			for _, f := range c.Findings {
				d.Findings = append(d.Findings, Finding{Code: f.Code, Detail: f.Detail, Source: f.Source, Control: c.Control, Change: c.ID})
			}
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
			d.Result.Rollback = rollbackOf(res.Failed)
		}
		if b := res.Boot; b != nil {
			d.Result.Boot = &Boot{Installed: b.Err == nil, Detail: b.Detail}
		}
	}
	return d
}

// changeStatus returns each change's status by its id, from the run's
// mode and result: planned for a check or plan; for an apply, from
// Result.Applied, Result.Boot and Result.Failed (engine.md §7).
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
			return StatusUnjournalled, f.Unjournalled.Err.Error()
		case f != nil && f.Change == id:
			return StatusFailed, f.Err.Error()
		case ok && f != nil && errors.Is(f, engine.ErrRolledBack):
			return StatusRolledBack, detail
		case ok:
			return StatusApplied, detail
		}
		return StatusNotRun, ""
	}
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

// rollbackOf is what rolling back the failed apply ae did.
func rollbackOf(ae *engine.ApplyError) *Rollback {
	rr := ae.Rollback
	rb := &Rollback{Finished: errors.Is(ae, engine.ErrRolledBack), ByAnotherProcess: errors.Is(ae.RollbackErr, revert.ErrRollingBack),
		Step: stepWord(ae.Step), Change: ae.Change, Restored: []string{}, Undone: []string{}}
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
		rb.Unjournalled = u.Change
	}
	if errors.Is(ae.RollbackErr, revert.ErrIncomplete) {
		rb.Command = "sudo hardn rollback " + ae.ID
	}
	return rb
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
			e := errorOf(ae.Err, "", ae.Change)
			e.Step = stepWord(ae.Step)
			add(e, ae.Err)
			if ae.RollbackErr != nil {
				e := errorOf(ae.RollbackErr, "", "")
				e.Step = "rollback"
				add(e, ae.RollbackErr)
			}
			if u := ae.Unjournalled; u != nil {
				add(Error{Code: "CHANGE_UNJOURNALLED", Change: u.Change, Step: stepWord(ae.Step),
					Message: fmt.Sprintf("%s ran, and the journal does not hold what undoes it (%v), so what it did may be left", u.Change, u.Err)},
					fmt.Errorf("unjournalled: %s", u.Change))
			}
		}
	}
	if err := r.Err; err != nil && !slices.ContainsFunc(seen, func(s error) bool { return sameErr(s, err) }) {
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

// sameErr reports whether a and b are the same error value; never
// comparing two values of a type that cannot be compared.
func sameErr(a, b error) bool {
	if a == nil || b == nil {
		return false
	}
	t := reflect.TypeOf(a)
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
