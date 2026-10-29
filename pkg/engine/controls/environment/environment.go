// Package environment holds the Environment control (refactor plan slice
// 9, Environment, unit (b), next/s9-control): whether the elevation tool
// keeps HARDN_CONFIG for the user who ran hardn through it, as data for
// the Environment screen, and the change that keeps it, or stops keeping
// it.
//
// Read gives the screen its reading, changing nothing: the tool
// (sudoers.Choose), who ran hardn (sudoers.Invoker: SUDO_USER, DOAS_USER;
// decision 2 (a)), the user's Policy as hardn's files hold it
// (sudoers.PolicyOf), origin/main's own env_keep line
// (sudoers.ReadLegacy; decision 3 (a)), the files read after hardn's rule,
// and why item 1 is refused before it asks. Check says the same as a
// status with its evidence; no status model takes it (decision 6 (a)).
// Plan keeps HARDN_CONFIG, or stops keeping it (Keep.Stop; decision 5
// (a)): the user's Policy with the variable added or taken out, the grant
// and every other variable carried as read, through steps.Sudoers. On
// doas it never writes the user's first rule (decision 1 (a)). Every
// refusal comes before any change, with a report (steps.Refusal).
// docs/engine/control-environment.md has every word, refusal and
// decision, and which are decided.
//
// The package reaches the host only through host.System (depguard
// controls-through-host), and hardn's environment only through the
// lookup its caller gives (Keep.Getenv); it renders nothing.
package environment

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// ID is the control's ID, stable once released.
const ID = "environment"

// Variable is the variable the control keeps (P-07 (c)).
const Variable = "HARDN_CONFIG"

// The ids of the change Plan makes: keeping the variable, or stopping.
const (
	ChangeKeep = ID + "/keep"
	ChangeStop = ID + "/stop"
)

// The change's summaries (decided, gallery-environment.md decision 7): the
// plan's heading and the step's line, the same at any length of the
// user's name, which the review's User row says.
const (
	SummaryKeep = "Keep HARDN_CONFIG"
	SummaryStop = "Stop keeping HARDN_CONFIG"
)

// The state's words, as the screen's state row, the review and Check's
// detail say them (Words): Kept, Not kept (decided, decision 7); on doas
// each with the note NoteHardn, as the row reads hardn's rules alone
// (decision 1 (a)'s open item, settled by words on 2026-10-29; proposed).
const (
	WordKept    = "Kept"
	WordNotKept = "Not kept"
	NoteHardn   = "by hardn"
)

// ErrNoUser: hardn was not run through sudo or doas by a user (a root
// login: SUDO_USER or DOAS_USER unset, or root), so there is no user to
// keep HARDN_CONFIG for (decision 2 (a); ENVIRONMENT_NO_USER).
var ErrNoUser = errors.New("no user ran hardn through the elevation tool")

// The codes of the refusals, the registry's for the errors they wrap
// (pkg/output; pinned by the tests).
const (
	codeNoTool = "SUDOERS_NO_TOOL"
	codeNoUser = "ENVIRONMENT_NO_USER"
	codeEdited = "SUDOERS_FILE_EDITED"
	codeNoRule = "SUDOERS_ENV_NEEDS_GRANT"
)

// Controls is the Environment control, keeping HARDN_CONFIG for the user
// getenv names (os.Getenv in hardn).
func Controls(getenv func(string) string) []engine.Control {
	return []engine.Control{Keep{Getenv: getenv}}
}

// Keep is the Environment control: whether the elevation tool keeps
// HARDN_CONFIG for the user who ran hardn, and the change that keeps it
// (Stop false) or stops keeping it (Stop true).
//
// The engine's Check and Plan take no environment, so the control carries
// the lookup (Getenv), as the DNS control carries hardn.yml. Stop is the
// way item 1 turns, which the screen takes from Read (Reading.Kept); -e
// and setup-sudo-env leave it false.
type Keep struct {
	// Getenv looks up hardn's environment: os.Getenv in hardn, a map in
	// the tests and the gallery. nil is an empty environment: no user.
	Getenv func(string) string
	// Stop: the change takes HARDN_CONFIG out of the user's variables;
	// false puts it in. Either way, nothing to change plans nothing.
	Stop bool
}

// ID returns ID.
func (Keep) ID() string { return ID }

// Title is the control's name in a plan of several controls and the JSON
// document's status: main menu 8's label (origin/main's).
func (Keep) Title() string { return "Environment" }

// Applies is true on every host. A host with neither tool is Not
// Applicable through Check and refused by Plan with a report, so that a
// caller hears why, as the DNS control does for the hosts it does not
// configure.
func (Keep) Applies(detect.Facts) (bool, string) { return true, "" }

// Reading is what the control reads of a host: the screen's rows, item
// 1's way and its refusal before it asks.
type Reading struct {
	// Choice is the elevation tool, as sudoers.Choose picks it from the
	// facts: sudo (and sudo-rs), doas; none (ToolNone); or not
	// established (Choice.Err).
	Choice sudoers.Choice
	// User is who ran hardn through the tool (sudoers.Invoker); "" for no
	// one: a root login (decision 2 (a)).
	User string
	// Value is HARDN_CONFIG in hardn's environment, as the tool passed it
	// on; "" not set. A change cannot reach it: this hardn was started
	// without it.
	Value string
	// Err is why the state was not read, so that item 1 is refused before
	// it asks: no tool, or no user (each a *steps.Refusal), the tool's
	// choice not established, a user name hardn does not write into a
	// rule, sudoers' read, a file of hardn's edited by hand (a
	// *steps.Refusal naming it), origin/main's file not read. nil when
	// the fields below are the host's.
	Err error
	// Policy is User's as hardn's files hold it (sudoers.PolicyOf): the
	// grant and the variables kept, which the change carries.
	Policy sudoers.Policy
	// Kept: hardn's files keep HARDN_CONFIG for User (Policy.Env).
	Kept bool
	// Legacy is origin/main's /etc/sudoers.d/<user> (sudoers.ReadLegacy),
	// where sudo reads one: Keeps, its env_keep line there (decision 3
	// (a)). Not hardn's to change: slice 5 migrates it.
	Legacy sudoers.Legacy
	// Findings are sudoers' findings of hardn's rules as they are (the
	// plan of Policy, sudoers.PlanFor), coded and worded by
	// steps.SudoersFinding: the files read after hardn's rule (Later),
	// lines of the main file after its includedir, origin/main's file.
	Findings []engine.Finding
	// Refused is why item 1's change is refused, the state read: on doas,
	// keeping HARDN_CONFIG where hardn has no rule for User (decision 1
	// (a); a *steps.Refusal). nil when item 1 asks.
	Refused error
	// Risk: item 1's change is lockout-risk (sudoers.RiskLockout: on doas,
	// a rule of hardn's that asks the user's password, rewritten).
	Risk bool
}

// Refusal is why item 1 is refused before it asks: Err, else Refused;
// nil when it asks.
func (r Reading) Refusal() error {
	if r.Err != nil {
		return r.Err
	}
	return r.Refused
}

// Later are the findings of the files the tool reads after hardn's rule
// for the user (SUDOERS_LATER_FILE), each "<file> may override hardn's
// rule" (decided, decision 7): whether their rules match the user is not
// read (sudoers.md §6), so Kept may not be the tool's effect.
func (r Reading) Later() []engine.Finding {
	var out []engine.Finding
	for _, f := range r.Findings {
		if f.Code == laterCode {
			out = append(out, f)
		}
	}
	return out
}

// laterCode is the code of sudoers' later-file finding.
var laterCode = steps.FindingCode("SUDOERS", sudoers.FindingLater.String())

// ToolName is the tool in the screen's words: "doas" on doas, else "sudo",
// where none is installed too (decided with decision 7, as drawn).
func (r Reading) ToolName() string {
	if r.Choice.Tool == sudoers.ToolDoas {
		return "doas"
	}
	return "sudo"
}

// NotRun is the User row where hardn was not run through the tool by a
// user, "Not run with sudo" or "Not run with doas" (decided, decision 7),
// and Check's detail there.
func (r Reading) NotRun() string { return "Not run with " + r.ToolName() }

// Words is the state of tool t's rules for the user, kept or not, as the
// state row and the review say it: Kept or Not kept, with the note
// NoteHardn on doas, where the row reads hardn's rules alone and the rule
// doas applies may be another's (the settlement of 2026-10-29; proposed).
func Words(t sudoers.Tool, kept bool) (value, note string) {
	value = WordNotKept
	if kept {
		value = WordKept
	}
	if t == sudoers.ToolDoas {
		note = NoteHardn
	}
	return value, note
}

// Words is r's state row (Words): where hardn's files do not keep
// HARDN_CONFIG and origin/main's file does, Kept with the note "by
// <file>" (decision 3 (a), decided).
func (r Reading) Words() (value, note string) {
	if !r.Kept && r.Legacy.Keeps {
		return WordKept, "by " + r.Legacy.Path
	}
	return Words(r.Choice.Tool, r.Kept)
}

// Read reads the Environment of sys, changing nothing: the tool f gives,
// the user getenv names for it and HARDN_CONFIG there, hardn's files for
// that user, origin/main's line, and item 1's plan, keeping HARDN_CONFIG,
// or stopping once hardn's files keep it (decision 5 (a)): whether it is
// refused (Refused) and whether it is lockout-risk (Risk). A screen draws
// its rows from it and refuses item 1 before it asks with Refusal.
func Read(ctx context.Context, sys host.System, f detect.Facts, getenv func(string) string) Reading {
	r, st := read(ctx, sys, f, getenv)
	if r.Err != nil {
		return r
	}
	p, err := planFor(r, st, r.Kept)
	r.Refused = err
	r.Risk = slices.ContainsFunc(p.Changes, func(c sudoers.Change) bool { return c.Risk == sudoers.RiskLockout })
	return r
}

// read is Read before item 1's plan, and sudoers' State for the user, read
// once r.Err is nil.
func read(ctx context.Context, sys host.System, f detect.Facts, getenv func(string) string) (Reading, sudoers.State) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	r := Reading{Choice: sudoers.Choose(f, sudoers.ToolUnknown), Value: getenv(Variable)}
	switch {
	case r.Choice.Tool == sudoers.ToolNone:
		r.Err = noTool(r.Choice.Err)
		return r, sudoers.State{}
	case r.Choice.Err != nil:
		r.Err = r.Choice.Err
		return r, sudoers.State{}
	}
	var err error
	if r.User, err = sudoers.Invoker(r.Choice.Tool, getenv); err != nil {
		r.Err = err
		return r, sudoers.State{}
	}
	if r.User == "" {
		r.Err = r.noUser()
		return r, sudoers.State{}
	}
	st, err := sudoers.Read(ctx, sys, r.Choice, []string{r.User})
	if err != nil {
		r.Err = err
		return r, st
	}
	if r.Policy, err = sudoers.PolicyOf(st, r.User); err != nil {
		r.Err = edited(err)
		return r, st
	}
	if r.Legacy, err = sudoers.ReadLegacy(ctx, sys, st, r.User); err != nil {
		r.Err = err
		return r, st
	}
	cur, err := sudoers.PlanFor(st, []sudoers.Policy{r.Policy})
	if err != nil {
		r.Err = err // not reached: PolicyOf gives a Policy PlanFor takes
		return r, st
	}
	for _, fd := range cur.Findings {
		r.Findings = append(r.Findings, steps.SudoersFinding(fd))
	}
	r.Kept = slices.Contains(r.Policy.Env, Variable)
	return r, st
}

// planFor is sudoers' plan of item 1's change on r's host, read as st:
// r.Policy with HARDN_CONFIG taken out of its variables (stop) or added
// after them, its grant and every other variable carried as read, so that
// on sudo the grant's file is not planned for its content and the change
// is to hardn-env-<stem> alone, with no lockout risk. An empty plan when
// the variables would not change. On doas, keeping it where hardn has no
// rule for the user is refused (decision 1 (a): the rule that lets the
// user use doas is not hardn's to write). Pure.
func planFor(r Reading, st sudoers.State, stop bool) (sudoers.Plan, error) {
	pol := r.Policy
	pol.Env = slices.Clone(pol.Env)
	switch {
	case stop:
		pol.Env = slices.DeleteFunc(pol.Env, func(v string) bool { return v == Variable })
	case !slices.Contains(pol.Env, Variable):
		pol.Env = append(pol.Env, Variable)
	}
	if slices.Equal(pol.Env, r.Policy.Env) {
		return sudoers.Plan{Tool: st.Choice.Tool, Main: st.Main}, nil
	}
	p, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
	if errors.Is(err, sudoers.ErrEnvNeedsGrant) {
		return p, r.noRule(err)
	}
	return p, err
}

// Check reads the Environment of sys (Read, without item 1's plan):
// Configured where hardn's files keep HARDN_CONFIG for the user who ran
// hardn ("Kept", on doas "Kept by hardn"), or origin/main's file does
// ("Kept by /etc/sudoers.d/<user>"); Not Configured where neither does
// ("Not kept", on doas "Not kept by hardn"); Not Applicable where neither
// tool is installed ("sudo isn't installed here") or no user ran hardn
// through the tool ("Not run with sudo"); Unknown, with the error, where
// the tool or hardn's files could not be read, or a file of hardn's was
// edited by hand. The findings are sudoers' of hardn's rules as they are.
// It writes nothing.
//
// No status model takes it: Environment has no main-menu row and no
// weight in the risk level (decision 6 (a)); the screen draws Read. It is
// the engine.Control's, for engine.Check: the JSON document's status of
// the control, for a caller that asks for it.
func (k Keep) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	r, _ := read(ctx, sys, f, k.Getenv)
	ev := evidence(r, k.Getenv)
	switch {
	case r.Choice.Tool == sudoers.ToolNone:
		return engine.Status{State: engine.NotApplicable, Detail: noToolWhy, Evidence: ev}, nil
	case errors.Is(r.Err, ErrNoUser):
		return engine.Status{State: engine.NotApplicable, Detail: r.NotRun(), Evidence: ev}, nil
	case r.Err != nil:
		return engine.Status{State: engine.Unknown, Evidence: ev}, r.Err
	}
	value, note := r.Words()
	st := engine.Status{State: engine.NotConfigured, Detail: strings.TrimSpace(value + " " + note), Evidence: ev, Findings: r.Findings}
	if value == WordKept {
		st.State = engine.Configured
	}
	return st, nil
}

// evidence is what r was read from (proposed words): the tool's choice
// where it chose none; else the variable the tool names its user in, as
// hardn's environment has it; hardn's file that keeps variables for the
// user (sudo's hardn-env-<stem>, doas's rule) and what it keeps; and
// origin/main's file, where sudo reads one, and whether it keeps
// HARDN_CONFIG.
func evidence(r Reading, getenv func(string) string) []string {
	if r.Choice.Err != nil {
		return []string{r.Choice.Err.Error()}
	}
	name := "SUDO_USER"
	if r.Choice.Tool == sudoers.ToolDoas {
		name = "DOAS_USER"
	}
	v := ""
	if getenv != nil {
		v = getenv(name)
	}
	if v == "" {
		v = "unset"
	}
	ev := []string{name + ": " + v}
	n, err := sudoers.FileNames(r.Choice.Tool, r.User)
	if r.User == "" || err != nil || r.Err != nil {
		return ev
	}
	file, kept := n.Env, "absent"
	if r.Choice.Tool == sudoers.ToolDoas {
		file = n.Grant
	}
	switch {
	case len(r.Policy.Env) > 0:
		kept = "keeps " + strings.Join(r.Policy.Env, " ")
	case r.Choice.Tool == sudoers.ToolDoas && r.Policy.Grant != sudoers.GrantNone:
		kept = "keeps nothing"
	}
	ev = append(ev, file+": "+kept)
	switch {
	case r.Legacy.Path == "":
	case r.Legacy.Keeps:
		ev = append(ev, r.Legacy.Path+": keeps "+Variable)
	default:
		ev = append(ev, r.Legacy.Path+": does not keep "+Variable)
	}
	return ev
}

// Plan is item 1's change on the host in.Facts describe: keeping
// HARDN_CONFIG for the user who ran hardn, or stopping (Stop), through
// steps.Sudoers, summarised "Keep HARDN_CONFIG" or "Stop keeping
// HARDN_CONFIG" (decided), sudoers' findings on it. The user's Policy is
// read from hardn's files and changed in its variables alone, the grant
// and every other variable carried as read (planFor), so on sudo the
// change writes or removes hardn-env-<stem>, or rewrites its list, and is
// never lockout-risk for that; on doas it rewrites hardn's rule for the
// user, lockout-risk for a rule that asks a password (steps.Sudoers'
// risk), and never writes the user's first rule (decision 1 (a)). Nothing
// to change plans nothing. Every refusal comes before any change, with a
// report where it has words: no tool, no user, a file of hardn's edited
// by hand, doas with no rule of hardn's for the user (Reading.Refusal);
// the others as they are.
func (k Keep) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	r, st := read(ctx, sys, in.Facts, k.Getenv)
	if r.Err != nil {
		return nil, r.Err
	}
	p, err := planFor(r, st, k.Stop)
	if err != nil || len(p.Changes) == 0 {
		return nil, err
	}
	id, summary := ChangeKeep, SummaryKeep
	if k.Stop {
		id, summary = ChangeStop, SummaryStop
	}
	c, err := steps.Sudoers(id, p, summary)
	if err != nil {
		return nil, err
	}
	return []engine.Change{c}, nil
}

// noToolWhy is the refusal where neither tool is installed (decided,
// decision 7), and Check's detail there.
const noToolWhy = "sudo isn't installed here"

// noTool is err, Choose's ToolNone (sudoers.ErrNoTool), with its report:
// the decided words, which have no Fix.
func noTool(err error) error {
	return &steps.Refusal{Code: codeNoTool, Why: noToolWhy, Err: err}
}

// noUser is the refusal where no user ran hardn through the tool (decided,
// decision 2 (a)'s words).
func (r Reading) noUser() error {
	t := r.ToolName()
	return &steps.Refusal{Code: codeNoUser, Why: "hardn wasn't run with " + t + ", so there's no user to keep it for",
		Fix: "run " + t + " hardn as that user", Err: ErrNoUser}
}

// noRule is err, PlanFor's refusal of variables on doas with no rule of
// hardn's for the user (sudoers.ErrEnvNeedsGrant), with its report: the
// decided words, and that the row stays as it is once the user follows
// them, as it reads hardn's rules alone (the settlement of 2026-10-29;
// proposed).
func (r Reading) noRule(err error) error {
	value, note := Words(sudoers.ToolDoas, false)
	return &steps.Refusal{Code: codeNoRule, Why: "hardn has no doas rule for " + r.User + " to keep HARDN_CONFIG on",
		Fix: "add setenv { HARDN_CONFIG } to the rule that lets " + r.User + " use doas. Under doas stays " + value + " " + note, Err: err}
}

// edited is err, PolicyOf's, with its report where it names a file of
// hardn's edited by hand (sudoers.EditedError), each one, the grant's
// first: decided words for one file, the plural for two (proposed). Any
// other error is err.
func edited(err error) error {
	errs := []error{err}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		errs = j.Unwrap()
	}
	var paths []string
	for _, e := range errs {
		var ee *sudoers.EditedError
		if errors.As(e, &ee) {
			paths = append(paths, ee.Path)
		}
	}
	switch len(paths) {
	case 0:
		return err
	case 1:
		return &steps.Refusal{Code: codeEdited, Why: paths[0] + " holds a rule hardn didn't write", Fix: "put hardn's back, or remove it", Err: err}
	}
	return &steps.Refusal{Code: codeEdited, Why: strings.Join(paths, " and ") + " hold rules hardn didn't write", Fix: "put hardn's back, or remove them", Err: err}
}
