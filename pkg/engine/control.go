package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// State is a control's state on the host (brief Phase 4).
type State uint8

// The states. Unknown is the zero value: a check that cannot establish the
// state says so rather than guess (Phase 4 counts it as not configured,
// with a factor).
const (
	Unknown       State = iota // not established
	Configured                 // as the configuration asks
	NotConfigured              // not as the configuration asks
	Partial                    // some of it
	NotApplicable              // not on this host (Control.Applies)
)

var stateWords = [...]string{"unknown", "configured", "not-configured", "partial", "not-applicable"}

func (s State) String() string {
	if int(s) < len(stateWords) {
		return stateWords[s]
	}
	return fmt.Sprintf("state(%d)", uint8(s))
}

// MarshalText writes the state's word, so a persisted or printed state is
// stable whatever the constants' order (p3-output's JSON).
func (s State) MarshalText() ([]byte, error) {
	if int(s) >= len(stateWords) {
		return nil, fmt.Errorf("engine: state %d", uint8(s))
	}
	return []byte(stateWords[s]), nil
}

// Finding is one thing a check found that a user should know: a drop-in
// that shadows hardn's, a rule ufw lacks.
type Finding struct {
	// Code is stable once released, upper case with underscores
	// (SSHD_CONFIG_INVALID); p3-output's registry holds every code.
	Code   string
	Detail string // one line
	Source string // where it was seen (a file and line, a command), when known
}

// Status is what a control's Check established.
type Status struct {
	State  State
	Detail string // one line, for the status view
	// Evidence is what the state was read from: the effective value, not
	// the file (brief §3 principle 3); Phase 4 fills in the sources.
	Evidence []string
	Findings []Finding
}

// Input is what a control plans from.
type Input struct {
	// ID is the plan's id: its journal's, its backup run's and its
	// timer's (revert.NewID).
	ID string
	// RunDir is the plan's run directory, /run/hardn/<id>, root's alone:
	// for copies a validation reads (sshd -t over a copy of the whole
	// configuration). The engine removes it when the plan or the apply
	// ends.
	RunDir string
	Facts  detect.Facts
	Config *config.Config
	// Session is the session applying the change (Options.Session), the
	// one the journal names: a listener change must keep the port it is
	// connected to (reload.CheckInEffect, ufw's Keep), and a control
	// that refuses a change it cannot show keeps it reads it here, so the
	// plan and the journal cannot name two sessions (next/p3-steps).
	Session revert.Session
}

// Control is one setting hardn checks and changes ("ssh.port").
//
// The brief's sketch is adjusted to the codebase: Applies takes detect's
// facts and says why not; Check takes the facts too, so that a check does
// not detect the platform again; Plan takes Input, the configuration with
// the facts and the plan's id and run directory.
type Control interface {
	// ID is stable and unique among the controls: lower case letters,
	// digits and ".-_/".
	ID() string
	// Title is the control's name in the views ("SSH Port").
	Title() string
	// Applies reports whether the control is for this host; when it is
	// not, reason says why (it is NotApplicable's Detail), and neither
	// Check nor Plan is called.
	Applies(f detect.Facts) (ok bool, reason string)
	// Check establishes the control's state, changing nothing.
	Check(ctx context.Context, sys host.System, f detect.Facts) (Status, error)
	// Plan returns the changes that bring the host to what in.Config
	// asks, changing nothing but in.RunDir. It is a function of the host
	// and in: run twice on an unchanged host, it returns the same
	// changes, and none once the host is as asked (idempotency: brief
	// Phase 3). Every refusal is an error here, before anything changes:
	// a candidate configuration sshd refuses, a port change on Proxmox
	// VE.
	Plan(ctx context.Context, sys host.System, in Input) ([]Change, error)
}

// Report is one control's Check, as Engine.Check returns it.
type Report struct {
	Control string
	Title   string
	Status  Status
	// Err is the check's error. Status is then what the check returned
	// with it (its detail, evidence and findings), its State Unknown: a
	// check that failed did not establish one (brief §3: prefer Unknown to
	// guessing).
	Err error
}

// Check runs every control's Check on the host's facts, in order; a
// control that does not apply is NotApplicable with its reason. The error
// is ctx's or detection's alone: a control's error is in its Report.
func (e *Engine) Check(ctx context.Context, controls []Control) ([]Report, error) {
	facts, err := e.opts.Detect(ctx, e.sys)
	if err != nil {
		return nil, fmt.Errorf("engine: detect: %w", err)
	}
	out := make([]Report, 0, len(controls))
	for _, c := range controls {
		r := Report{Control: c.ID(), Title: c.Title()}
		if ok, why := c.Applies(facts); !ok {
			r.Status = Status{State: NotApplicable, Detail: notApplicable(why)}
		} else {
			r.Status, r.Err = c.Check(ctx, e.sys, facts)
			if r.Err != nil {
				r.Status.State = Unknown
			}
		}
		out = append(out, r)
	}
	return out, ctx.Err()
}

func notApplicable(why string) string {
	if why == "" {
		return "not applicable on this host"
	}
	return why
}

// ErrNotMigrated is a check-only control's Plan (CheckOnly): the control
// reports the host's state, and planning its changes waits for the slice
// that migrates it (refactor plan Phase 4, next/p4-check-ssh). Engine.Plan
// refuses a plan with such a control, as any control's error.
var ErrNotMigrated = errors.New("the control only checks the host; its changes are not planned yet")

// CheckOnly gives a check-only control its Plan: embedded in a control
// that implements ID, Title, Applies and Check, it makes an engine.Control
// whose Plan returns ErrNotMigrated and changes nothing. For such a
// control, Configured is the host as hardn's baseline wants it, not "as
// the configuration asks": Check takes no configuration
// (docs/engine/checks.md §1.3).
type CheckOnly struct{}

// Plan returns ErrNotMigrated.
func (CheckOnly) Plan(context.Context, host.System, Input) ([]Change, error) {
	return nil, ErrNotMigrated
}
