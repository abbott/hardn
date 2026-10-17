package alpine

import (
	"fmt"

	"github.com/abbott/hardn/pkg/host/detect"
)

// Step is one change a plan makes, in the order Apply runs them.
type Step uint8

const (
	StepInstall      Step = iota + 1 // apk add apk-cron
	StepRunlevel                     // rc-update add crond default
	StepStart                        // rc-service crond start
	StepRestart                      // rc-service crond restart: marked started, its daemon gone
	StepRemoveLegacy                 // remove origin/main's LegacyScript, after a backup
)

func (s Step) String() string {
	switch s {
	case StepInstall:
		return "install"
	case StepRunlevel:
		return "runlevel"
	case StepStart:
		return "start"
	case StepRestart:
		return "restart"
	case StepRemoveLegacy:
		return "remove-legacy"
	}
	return "unknown"
}

// argv is the command a step runs, and undo the one that reverses it; the
// legacy removal runs none (Remove, and WriteFileAtomic to undo it).
func (s Step) argv() (do, undo []string) {
	switch s {
	case StepInstall:
		return []string{"apk", "add", Package}, []string{"apk", "del", Package}
	case StepRunlevel:
		return []string{"rc-update", "add", Service, Runlevel}, []string{"rc-update", "del", Service, Runlevel}
	case StepStart:
		return []string{"rc-service", Service, "start"}, []string{"rc-service", Service, "stop"}
	case StepRestart:
		return []string{"rc-service", Service, "restart"}, []string{"rc-service", Service, "stop"}
	}
	return nil, nil
}

// Change is one step of a plan. Argv and Undo are for the plan view; Apply
// and Rollback run the step's own commands and refuse a Change whose Argv
// is not its step's. None of these steps can lock a user out: a failure is
// undone, with no confirm-or-revert timer (brief 2.4).
type Change struct {
	Step Step
	Argv []string // nil for StepRemoveLegacy
	Undo []string // nil for StepRemoveLegacy, which writes LegacyContent back as Plan.From.LegacyFile was
	Path string   // StepRemoveLegacy: LegacyScript
	Why  string
}

// Plan is what PlanUpdates decided. From is the state it was planned from:
// Apply refuses a host that no longer matches it, and the legacy file's
// rollback writes LegacyContent back with From.LegacyFile's mode and
// owner. A Plan kept for p2-revert must keep From and every Change; Apply
// and Rollback refuse one that does not follow from its From.
type Plan struct {
	From     Updates
	Changes  []Change
	Findings []Finding
}

// PlanUpdates decides, with no I/O, what makes Status Configured: install
// apk-cron, add crond to the default runlevel, start crond (restart it when
// it crashed), then remove origin/main's own LegacyScript. A host already
// configured plans nothing. It refuses when a fact it needs is Unknown
// (ErrUnknown), when crond's service is not installed (ErrNoCrond), and
// when apk-cron and its daily script disagree (ErrScript).
func PlanUpdates(u Updates) (Plan, error) {
	p := Plan{From: u, Findings: u.findings()}
	if u.Package == detect.Unknown || u.Script == ScriptUnknown || u.Runlevel == detect.Unknown ||
		u.Crond == CrondUnknown || u.Legacy == LegacyUnknown {
		return Plan{}, fmt.Errorf("alpine: plan: %w: %w", ErrUnknown, errOr(u.Err, detect.ErrNoSignal))
	}
	if u.Crond == CrondMissing {
		return Plan{}, fmt.Errorf("alpine: plan: %s: %w", InitScript, ErrNoCrond)
	}
	switch {
	case u.Package == detect.No && u.Script != ScriptAbsent:
		return Plan{}, fmt.Errorf("alpine: plan: %s is there but %s is not installed: %w", Script, Package, ErrScript)
	case u.Package == detect.Yes && u.Script != ScriptInstalled:
		return Plan{}, fmt.Errorf("alpine: plan: %s is installed but %s is missing or not executable (apk fix %s reinstalls it): %w", Package, Script, Package, ErrScript)
	}
	add := func(s Step, why string) {
		do, undo := s.argv()
		c := Change{Step: s, Argv: do, Undo: undo, Why: why}
		if s == StepRemoveLegacy {
			c.Path = LegacyScript
		}
		p.Changes = append(p.Changes, c)
	}
	if u.Package == detect.No {
		add(StepInstall, Package+" installs "+Script+", which runs apk -U upgrade daily")
	}
	if u.Runlevel == detect.No {
		add(StepRunlevel, "crond runs the daily jobs; in the default runlevel it starts at boot")
	}
	switch u.Crond {
	case CrondStopped:
		add(StepStart, "crond is not running")
	case CrondCrashed:
		add(StepRestart, "crond is marked started but its daemon is gone")
	}
	if u.Legacy == LegacyHardn {
		add(StepRemoveLegacy, LegacyScript+" was written by an earlier hardn and would upgrade a second time each day; "+
			Script+" replaces it, and a copy is kept in the backup run")
	}
	return p, nil
}
