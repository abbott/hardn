package main

// The plan/apply engine in cmd/hardn (refactor plan slice 2, the DNS
// pilot, unit (d), next/s2-replace): its options, assembled in one place
// for every path that changes the host through it, -g and the main menu's
// DNS screen now, the next slices' flags and screens after them, from what
// hardn confirm and hardn rollback already build (revert.go, confirm.go).
// docs/engine/cmd-engine.md says where each option comes from.

import (
	"context"
	"io"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// Seams for the tests: the host the engine changes and the screens read,
// with its run log; detection, the engine's and the screens'; the hold
// around an in-process rollback (nil: the engine's own); the line reader
// and the stdout the screens use.
var (
	engineHost   = func() host.System { return host.NewReal(host.Options{RunLog: revertFileLog{}}) }
	engineDetect = detect.Detect
	engineHold   func() (release func())
	engineInput            = prompt.Stdin
	engineStdout io.Writer = os.Stdout
)

// engineOptions are the engine's options for the changes cmd/hardn makes
// on sys with the configuration c, all but Progress, which is the caller's:
//
//   - Revert as hardn rollback builds it (Now, Plans) and hardn rollback
//     --boot (Hardn, this hardn's path, resolved), with Log the log file
//     alone (revertFileLog), as hardn confirm's clean-up has it: the
//     engine's and revert's lines are the log's, and what a person reads
//     is the report (output.Human, or a screen's flow). Plans is needed:
//     a DNS change journals its undo as the dns unit's kept plan
//     (revert.OpPlan), which revert.Record refuses without its rollback,
//     and it is the registry a later hardn rollback <id> undoes the
//     journal with. Hardn is not, for DNS: the engine reads it only to
//     arm a lockout-risk plan's revert timer, which a DNS change never
//     arms (brief 2.4); it is set for the next slices' changes, and left
//     empty when this hardn's path cannot be read, so that the engine
//     refuses such a plan ("the revert timer") rather than arm a timer
//     that runs nothing.
//   - Session as hardn confirm reads it (revert.ReadSession, with hardn's
//     environment and pid): journalled with the change, and required of a
//     lockout-risk plan, which the engine refuses when it was not
//     established. Its error goes to the log file.
//   - Backups from hardn.yml (backupPath, enableBackups; §3.16: the
//     engine's backups are taken whatever enableBackups says).
//   - PID, hardn's, for the run lock's holder line.
//   - Detect and Hold the engine's own: detect.Detect, and hold.Signals
//     over Log around an in-process rollback.
func engineOptions(ctx context.Context, sys host.System, c *config.Config) engine.Options {
	o := engine.Options{
		Revert:  revert.Options{Log: revertFileLog{}, Now: revertNow, Plans: revertPlans},
		Backups: c.BackupSettings(),
		Detect:  engineDetect,
		Hold:    engineHold,
		PID:     revertPID(),
	}
	if self, err := revertExecutable(); err == nil {
		o.Revert.Hardn = self
	}
	s, err := revert.ReadSession(ctx, sys, revertGetenv, revertPID())
	if err != nil {
		logging.LogFileOnly("%v", err)
	}
	o.Session = s
	return o
}

// newEngine returns a screen's NewEngine: an engine over sys with o, its
// Progress the flow's, one per change.
func newEngine(sys host.System, o engine.Options) func(progress func(engine.Event)) *engine.Engine {
	return func(progress func(engine.Event)) *engine.Engine {
		o := o
		o.Progress = progress
		return engine.New(sys, o)
	}
}
