package output

import (
	"fmt"
	"io"
	"time"

	"github.com/abbott/hardn/pkg/engine/drift"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// CodeNotCompared is the finding of a fact hardn check left out of its
// comparison: the listening ports on a host with no ss.
const CodeNotCompared = "CHECK_NOT_COMPARED"

// Check is hardn check's part of its document, Document.Check, on that
// command's documents alone (docs/reference/output.md §1.11): the baseline
// compared with, or just saved, and each difference from it.
type Check struct {
	// Accepted: the run was hardn check --accept, and saved Baseline;
	// false for one that saved nothing.
	Accepted bool `json:"accepted"`
	// Baseline is null where none was read or saved: none there, one
	// refused, or an error before it.
	Baseline *CheckBaseline `json:"baseline"`
	// Differences are what differs from the baseline; [] for none, and
	// for --accept, which compares nothing.
	Differences []Difference `json:"differences"`
}

// CheckBaseline is a baseline's own facts.
type CheckBaseline struct {
	Path string `json:"path"`
	// Format is the version of the baseline's format (drift.Format).
	Format int `json:"format"`
	// Saved is when it was saved, in UTC; User who saved it, as Revert.User;
	// HardnVersion the hardn that saved it.
	Saved        time.Time `json:"saved"`
	User         string    `json:"user"`
	HardnVersion string    `json:"hardnVersion"`
}

// Difference is one difference from the baseline.
type Difference struct {
	// Fact is the fact's id (drift.Specs), such as firewall.rules.
	Fact string `json:"fact"`
	// Change is changed, added or removed (open): a value, or whether the
	// fact is established, changed; a set gained or lost an item.
	Change string `json:"change"`
	// Was and Is are what the baseline has and what the host has now:
	// null where the fact is not established; for an item added Was is "",
	// for one removed Is is "".
	Was *string `json:"was"`
	Is  *string `json:"is"`
}

// CheckRun is one run of hardn check or hardn check --accept: the input of
// BuildCheck and HumanCheck.
type CheckRun struct {
	HardnVersion string
	Hostname     string
	Facts        *detect.Facts
	// Accept: the run saved Baseline; otherwise it compared the host with
	// it. Baseline is nil where none was read or saved; Path is its file.
	Accept      bool
	Path        string
	Baseline    *drift.Baseline
	Differences []drift.Difference
	Notes       []drift.Note
	// Err ended the run: no baseline, one refused, a read that failed.
	Err error
}

// BuildCheck maps run r to its document: mode check, the nine fields with
// no change and no status, a finding for each fact not compared, and
// Check. Its outcome is one of the existing ones: failed (or usage-error,
// cancelled) by r.Err's code; applied for a baseline saved; changes-needed
// with a difference; nothing-to-do with none. It is pure.
func BuildCheck(r CheckRun) Document {
	d := Build(Run{Mode: ModeCheck, HardnVersion: r.HardnVersion, Hostname: r.Hostname, Facts: r.Facts, Err: r.Err})
	c := &Check{Accepted: r.Accept && r.Err == nil && r.Baseline != nil, Differences: []Difference{}}
	if b := r.Baseline; b != nil {
		c.Baseline = &CheckBaseline{Path: r.Path, Format: b.Format, Saved: b.Saved.UTC(), User: b.User, HardnVersion: b.HardnVersion}
	}
	for _, x := range r.Differences {
		c.Differences = append(c.Differences, Difference{Fact: x.Fact, Change: string(x.Change), Was: x.Was, Is: x.Is})
	}
	for _, n := range r.Notes {
		d.Findings = append(d.Findings, Finding{Code: CodeNotCompared, Detail: n.Why, Source: n.Fact})
	}
	d.Check = c
	if r.Err == nil {
		o := OutcomeNothingToDo
		switch {
		case r.Accept:
			o = OutcomeApplied
		case len(r.Differences) > 0:
			o = OutcomeChangesNeeded
		}
		d.Result.Outcome, d.Result.ExitCode = o, o.Exit()
	}
	return d
}

// CheckExit is run r's exit status, the one BuildCheck writes.
func CheckExit(r CheckRun) int { return BuildCheck(r).Result.ExitCode }

// NewCheckRecord is the audit record of a hardn check --accept, at time at
// (UTC): its document, which says who saved the baseline and when
// (docs/engine/design-check.md, decision 7). A plain hardn check has none.
func NewCheckRecord(r CheckRun, at time.Time) Record {
	return Record{Time: at.UTC(), Document: BuildCheck(r)}
}

// The words of hardn check's report.
const (
	checkSame     = "No difference from the baseline of %s."
	checkOne      = "1 difference from the baseline of %s"
	checkMany     = "%d differences from the baseline of %s"
	checkSaved    = "Baseline saved: %s"
	checkKeep     = "Run `%s check --accept` to keep the host as it is now."
	checkNotComp  = "%s not compared: %s"
	checkNotKnown = "not established"
	checkNone     = "none"
	checkStamp    = "2006-01-02 15:04 MST"
)

// HumanCheck renders run r for a person, at env, through pkg/ui's
// components: a baseline saved; no difference; or the differences as one
// warning, a line each with what it was and what it is, then how to accept
// them. A fact not compared is said after either. Every string read from
// the host is made safe first (logview.Safe). A run that ended with an
// error renders as "": its caller says the error on stderr. It is pure.
func HumanCheck(env term.Env, r CheckRun) string {
	if r.Err != nil || r.Baseline == nil {
		return ""
	}
	var run component.Messages
	when := r.Baseline.Saved.UTC().Format(checkStamp)
	switch {
	case r.Accept:
		run = append(run, component.Message{Kind: component.MessageOK, Text: fmt.Sprintf(checkSaved, r.Path)})
	case len(r.Differences) == 0:
		run = append(run, component.Message{Text: fmt.Sprintf(checkSame, when)})
	default:
		head := fmt.Sprintf(checkOne, when)
		if n := len(r.Differences); n > 1 {
			head = fmt.Sprintf(checkMany, n, when)
		}
		run = append(run, component.Message{Kind: component.MessageWarn, Text: head})
		for _, x := range r.Differences {
			run = append(run, component.Message{Kind: component.MessageWarn, Text: DifferenceLine(x)})
		}
		run = append(run, component.Message{Text: fmt.Sprintf(checkKeep, hardnAsRoot(r.facts()))})
	}
	for _, n := range r.Notes {
		run = append(run, component.Message{Text: fmt.Sprintf(checkNotComp, n.Title, logview.Safe(n.Why))})
	}
	return run.Render(component.NewCtx(env)) + "\n"
}

// WriteCheck writes HumanCheck's report of r on w, through env's writer.
func WriteCheck(w io.Writer, env term.Env, r CheckRun) error {
	if h := HumanCheck(env, r); h != "" {
		if _, err := io.WriteString(env.Writer(w), h); err != nil {
			return fmt.Errorf("output: stdout: %w", err)
		}
	}
	return nil
}

// facts are r's host facts; the zero value where it has none.
func (r CheckRun) facts() detect.Facts {
	if r.Facts != nil {
		return *r.Facts
	}
	return detect.Facts{}
}

// DifferenceLine is one difference as a line: "SSH root login: was no, is
// yes", "Firewall rule added: allow in 8080/tcp". What the host holds is
// made safe to print.
func DifferenceLine(x drift.Difference) string {
	word := func(s *string) string {
		switch {
		case s == nil:
			return checkNotKnown
		case *s == "":
			return checkNone
		}
		return logview.Safe(*s)
	}
	switch x.Change {
	case drift.Added:
		return x.Title + " added: " + word(x.Is)
	case drift.Removed:
		return x.Title + " removed: " + word(x.Was)
	}
	return x.Title + ": was " + word(x.Was) + ", is " + word(x.Is)
}
