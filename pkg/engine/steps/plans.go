package steps

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/revert/plans"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// The changes of the units that keep a checked rollback of their own are
// journalled as that unit's registered plan step (pkg/host/revert/plans),
// made with its step function, which refuses a plan its rollback would
// refuse, before anything changes (revert-plans.md §2.4, §6): the engine
// journals the step before the unit's apply (write-ahead), and the
// rollback, in this process or from the timer, is the unit's own. Each
// apply that a unit refused before it wrote anything drops the step again
// (engine.Ran.Rollback, empty), by that unit's own "wrote nothing" signal,
// so that the rollback only ends the change: DNS's and Alpine's
// rollbacks cannot tell their change from the same one made since,
// sudoers' would fail on the refusal's cause, and Proxmox VE's reverses
// the edit, not the write. Their own backups go into the plan's backup run
// (engine.Run.Store), unjournalled: the undo is the plan step (pve.md §6
// keeps the keys' backup out of the journal on purpose).

// emptied is the undo of an apply that changed nothing: none.
func emptied() *engine.Undo { return &engine.Undo{} }

// Sudoers is a sudoers.Plan (sudo, sudo-rs or doas rules) as a change
// (StageAccounts; brief 2.3 step 1, and grants before any change that
// disables root's SSH login, sudoers.md §6), journalled as plans.Sudoers'
// step. Lockout risk when a change of the plan is (a grant removed, or
// one that asks a password: sudoers.RiskLockout), so that a new SSH
// connection, where elevation is checked, confirms it; plain otherwise.
// Apply backs up every file it writes or removes into the plan's backup
// run (RollbackRequired), then sudoers.Apply with the plan's run
// directory. The step is dropped when Apply wrote nothing: an error with
// nothing installed, nothing restored and not sudoers.ErrNotRestored, or a
// backup that failed before it.
func Sudoers(id string, p sudoers.Plan, summary string) (engine.Change, error) {
	step, err := plans.Sudoers(p, summary)
	if err != nil {
		return engine.Change{}, err
	}
	c := engine.Change{ID: id, Summary: summary, Target: strings.Join(sudoersPaths(p), ", "), Stage: engine.StageAccounts,
		Rollback: engine.Undo{Steps: []revert.Step{step}}, Resources: []string{plans.UnitSudoers}}
	if slices.ContainsFunc(p.Changes, func(ch sudoers.Change) bool { return ch.Risk == sudoers.RiskLockout }) {
		c.Risk = engine.RiskLockout
	}
	for _, ch := range p.Changes {
		var before, after *string
		if ch.Old.Exists {
			s := string(ch.Old.Data)
			before = &s
		}
		if !ch.Remove {
			s := string(ch.Data)
			after = &s
		}
		c.Diff += engine.FileDiff(ch.Path, before, after)
	}
	c.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
		for _, ch := range p.Changes {
			if _, err := run.Store.Backup(ctx, ch.Path, backup.RollbackRequired); err != nil {
				return engine.Ran{Rollback: emptied()}, err
			}
		}
		res, err := sudoers.Apply(ctx, sys, p, sudoers.Options{RunDir: run.Dir})
		if err != nil && len(res.Installed) == 0 && !res.Restored && !errors.Is(err, sudoers.ErrNotRestored) {
			return engine.Ran{Rollback: emptied()}, err
		}
		return engine.Ran{Detail: fmt.Sprintf("sudoers: installed %v", res.Installed)}, err
	}
	return c, nil
}

func sudoersPaths(p sudoers.Plan) []string {
	var out []string
	for _, ch := range p.Changes {
		out = append(out, ch.Path)
	}
	return out
}

// DNS is a dns.Plan as a change (StageFiles, no lockout risk: brief 2.4),
// journalled as plans.DNS' step, which the rollback runs only for a plan
// whose Apply returned nil (dns.md §6): an Apply that failed, having
// changed nothing or undone its own steps (Result.Restored), drops it, and
// one that undid them says so (engine.Ran.Undone), so that the engine
// reports the change rolled back. One whose own undo did not finish
// (dns.ErrNotRestored) keeps it, so that the journal's rollback tries again
// what is left of the plan's own writes (revert-plans.md §6), and says
// that what it did is left to that undo (Ran.Undone), so that the engine
// reports the change rolled back once that rollback finished. A reload
// that ran without effect is a Failure in a few words, undone or not. Its
// backups go into the plan's backup run. A file the plan creates is
// diffed against /dev/null, and the plan's findings ride on the change
// (DNSFinding). The support level (detect.Support) is the engine's gate
// for every plan.
func DNS(id string, p dns.Plan, summary string) (engine.Change, error) {
	step, err := plans.DNS(p, summary)
	if err != nil {
		return engine.Change{}, err
	}
	c := engine.Change{ID: id, Summary: summary, Stage: engine.StageFiles, Rollback: engine.Undo{Steps: []revert.Step{step}},
		Resources: []string{plans.UnitDNS}, Findings: dnsFindings(p.From, p.Findings)}
	var targets []string
	for _, ch := range p.Changes {
		if ch.Path != "" {
			targets = append(targets, ch.Path)
			after := string(ch.After)
			var before *string // nil: the step creates the file
			if dnsBefore(p, ch.Step).Kind == dns.EntryFile {
				b := string(ch.Before)
				before = &b
			}
			c.Diff += engine.FileDiff(ch.Path, before, &after)
		}
		if len(ch.Argv) > 0 {
			c.Commands = append(c.Commands, host.FormatArgv(ch.Argv))
		}
	}
	c.Target = strings.Join(targets, ", ")
	c.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
		res, err := dns.Apply(ctx, sys, p, dns.Options{Backups: run.Store})
		switch {
		case err == nil:
			return engine.Ran{Detail: "dns: applied"}, nil
		case errors.Is(err, dns.ErrNotRestored): // the step stays, for the journal's rollback
			return engine.Ran{Undone: true}, dnsFailure(res, err)
		}
		return engine.Ran{Rollback: emptied(), Undone: res.Restored}, dnsFailure(res, err)
	}
	return c, nil
}

// dnsBefore is what a step's path held when p was planned: absent for a
// file the step creates.
func dnsBefore(p dns.Plan, s dns.Step) dns.Entry {
	switch s {
	case dns.StepUDHCPC:
		return p.From.UDHCPC.Conf
	case dns.StepResolvConf:
		return p.From.ResolvConf
	case dns.StepDropIn:
		return p.From.Resolved.DropIn
	}
	return dns.Entry{}
}

// dnsFailure is dns.Apply's error in the user's words where they are
// known: a reload of systemd-resolved that ran without effect, whether
// dns.Apply's own undo finished or left the rest to the journal
// (gallery-dns.md, the failure-proposed sequence; proposed words). Any
// other error is err.
func dnsFailure(res dns.Result, err error) error {
	if errors.Is(forward(err), dns.ErrNotEffective) && len(res.Done) > 0 && res.Done[len(res.Done)-1] == dns.StepReload {
		return &Failure{Text: "Reloading systemd-resolved had no effect", Next: "Run resolvectl dns to see the servers in use.", Err: err}
	}
	return err
}

// forward is the step's own error of dns.Apply's err, which joins it with
// its undo's (errors.Join): an undo's reload without effect is not the
// step's.
func forward(err error) error {
	if j, ok := err.(interface{ Unwrap() []error }); ok && len(j.Unwrap()) > 0 {
		return j.Unwrap()[0]
	}
	return err
}

// Alpine is an alpine.Plan (apk-cron and crond for Alpine's updates) as a
// change (StagePackages; the highest class of its steps, whatever their
// order: RiskServiceRestart when crond is started or restarted, else
// RiskPackageInstall for apk-cron's install, else plain),
// journalled as plans.Alpine's step, rolled back by it only after an
// Apply that returned nil (alpine.md §6), with the same rule as DNS for an
// Apply that failed. Its backup of the legacy script goes into the plan's
// backup run.
func Alpine(id string, p alpine.Plan, summary string) (engine.Change, error) {
	step, err := plans.Alpine(p, summary)
	if err != nil {
		return engine.Change{}, err
	}
	c := engine.Change{ID: id, Summary: summary, Target: alpine.Service, Stage: engine.StagePackages,
		Rollback: engine.Undo{Steps: []revert.Step{step}}, Resources: []string{plans.UnitAlpine}}
	for _, ch := range p.Changes {
		switch ch.Step {
		case alpine.StepInstall:
			c.Risk = max(c.Risk, engine.RiskPackageInstall)
		case alpine.StepStart, alpine.StepRestart:
			c.Risk = max(c.Risk, engine.RiskServiceRestart)
		}
		if len(ch.Argv) > 0 {
			c.Commands = append(c.Commands, host.FormatArgv(ch.Argv))
		}
	}
	c.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
		_, err := alpine.Apply(ctx, sys, p, alpine.Options{Backups: run.Store})
		return unitRan(err, alpine.ErrNotRestored, "alpine: applied")
	}
	return c, nil
}

// unitRan is what an Apply of Alpine leaves to the journal: the plan
// step, unless it failed and its own undo finished, or it changed nothing
// (an error not wrapping notRestored). DNS's says more (DNS).
func unitRan(err, notRestored error, detail string) (engine.Ran, error) {
	if err != nil && !errors.Is(err, notRestored) {
		return engine.Ran{Rollback: emptied()}, err
	}
	return engine.Ran{Detail: detail}, err
}

// PVEKeys is a change of root's authorized keys on Proxmox VE, through
// the cluster-wide file's link (pve.ApplyKeys), as a change
// (StageAccounts), journalled as plans.PVEKeys' step (pve.md §6): lockout
// risk when it removes a key (KeysPlan.Narrows), plain otherwise; the
// cluster-wide warning is in the plan's findings. ApplyKeys' own backup
// goes into the plan's backup run and stays out of the journal, whose
// restore would drop the keys other nodes wrote meanwhile (pve.md §2.12).
// An ApplyKeys refused before its backup (a zero backup.Result) drops the
// step.
func PVEKeys(id string, px detect.Proxmox, p pve.KeysPlan, summary string) (engine.Change, error) {
	step, err := plans.PVEKeys(p, summary)
	if err != nil {
		return engine.Change{}, err
	}
	before, after := string(p.Old), string(p.New)
	c := engine.Change{ID: id, Summary: summary, Target: pve.RootKeys, Diff: engine.FileDiff(pve.RootKeys, &before, &after),
		Stage: engine.StageAccounts, Rollback: engine.Undo{Steps: []revert.Step{step}}, Resources: []string{plans.UnitPVEKeys}}
	if p.Narrows() {
		c.Risk = engine.RiskLockout
	}
	c.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
		r, err := pve.ApplyKeys(ctx, sys, px, run.Store, p)
		if err != nil && r == (backup.Result{}) {
			return engine.Ran{Rollback: emptied()}, err
		}
		return engine.Ran{Detail: "root's keys written through " + pve.RootKeys}, err
	}
	return c, nil
}
