package alpine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
)

// DefaultRestoreTimeout bounds Apply's undo when Options.RestoreTimeout is
// not positive.
const DefaultRestoreTimeout = 2 * time.Minute

// Options configure Apply.
type Options struct {
	// Backups takes the backup of LegacyScript (backup.RollbackRequired, so
	// it is taken whatever enableBackups says) before anything changes. A
	// plan that removes LegacyScript is refused without it (ErrNoBackup).
	Backups *backup.Run
	// RestoreTimeout bounds the undo after a failed step, which runs under
	// context.WithoutCancel(ctx), so a Ctrl-C does not stop it. Not
	// positive: DefaultRestoreTimeout.
	RestoreTimeout time.Duration
}

// Result is what Apply did.
type Result struct {
	// Done are the steps that ran, in order, including the one that failed
	// and those an undo then reversed.
	Done []Step
	// Legacy is the backup of LegacyScript, when the plan removes it: its
	// Entry names the copy in the run directory, for the note.
	Legacy *backup.Result
	// Restored: a step failed, and every step that ran was undone and
	// checked. When that undo did not complete, the error wraps
	// ErrNotRestored.
	Restored bool
}

// Apply runs p. Before anything changes, it refuses a plan that is not
// what PlanUpdates makes from p.From (fs.ErrInvalid) and one that removes
// LegacyScript without Options.Backups (ErrNoBackup), re-reads the host
// and refuses one that no longer matches p.From (ErrChanged), and takes
// the backup of LegacyScript when p removes it, refusing a copy that is
// not exactly hardn's file. Then each step runs and is checked:
// apk-cron installed with its daily script, RunlevelLink present, crond
// started, LegacyScript (re-read and still exactly hardn's) gone; a step
// whose command succeeds without its effect fails with ErrNotEffective.
// When a step fails, the steps that ran, the failed one included, are
// undone last first under context.WithoutCancel with
// Options.RestoreTimeout: Result.Restored, or an error wrapping
// ErrNotRestored. An empty plan runs nothing.
func Apply(ctx context.Context, sys host.System, p Plan, opts Options) (res Result, err error) {
	if len(p.Changes) == 0 {
		return res, nil
	}
	if err := ctx.Err(); err != nil {
		return res, fmt.Errorf("alpine: apply: %w", err)
	}
	if err := valid(p); err != nil {
		return res, err
	}
	if removesLegacy(p) && opts.Backups == nil {
		return res, fmt.Errorf("alpine: apply: %s: %w", LegacyScript, ErrNoBackup)
	}
	now := read(ctx, sys)
	if err := ctx.Err(); err != nil {
		return res, fmt.Errorf("alpine: apply: %w", err)
	}
	if now.Err != nil {
		return res, fmt.Errorf("alpine: apply: re-read: %w", now.Err)
	}
	if !same(now, p.From) {
		return res, fmt.Errorf("alpine: apply: %w", ErrChanged)
	}
	if removesLegacy(p) {
		b, err := opts.Backups.Backup(ctx, LegacyScript, backup.RollbackRequired)
		if err != nil {
			return res, fmt.Errorf("alpine: apply: %w", err)
		}
		sum := sha256.Sum256([]byte(LegacyContent))
		if b.Entry.Kind != backup.KindFile || b.Entry.File == nil || b.Entry.File.SHA256 != hex.EncodeToString(sum[:]) {
			return res, fmt.Errorf("alpine: apply: %s: backed up as %s, not hardn's file: %w", LegacyScript, b.Entry.Kind, ErrChanged)
		}
		res.Legacy = &b
	}
	var i int
	for i = range p.Changes {
		res.Done = append(res.Done, p.Changes[i].Step)
		if err = do(ctx, sys, p, p.Changes[i].Step); err != nil {
			break
		}
	}
	if err == nil {
		return res, nil
	}
	d := opts.RestoreTimeout
	if d <= 0 {
		d = DefaultRestoreTimeout
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d)
	defer cancel()
	rerr := undo(rctx, sys, p, p.Changes[:i+1])
	res.Restored = rerr == nil
	return res, errors.Join(err, rerr)
}

// Rollback undoes p's steps, last first, each only when its effect is
// there: LegacyScript, when absent, is written back as p.From.LegacyFile
// (anything else there is left alone: ErrChanged); crond is stopped when
// running; removed from the default runlevel when in it; apk-cron is
// removed when installed. Each undo is checked. A rollback cannot tell
// whether someone else made the same change since (started crond, say),
// and undoes it too. Every failure wraps ErrNotRestored; it is idempotent.
// After Ctrl-C, run it under context.WithoutCancel with a timeout: every
// host.System method refuses a done context.
func Rollback(ctx context.Context, sys host.System, p Plan) error {
	if len(p.Changes) == 0 {
		return nil
	}
	if err := valid(p); err != nil {
		return err
	}
	return undo(ctx, sys, p, p.Changes)
}

// valid refuses a plan that is not what PlanUpdates makes from its From:
// steps added, dropped, reordered or naming other commands (a journal
// edited by hand would otherwise run what it names, or remove the legacy
// script before apk-cron is in). Why is not compared.
func valid(p Plan) error {
	want, err := PlanUpdates(p.From)
	if err != nil {
		return fmt.Errorf("alpine: plan does not follow from its state: %w: %w", fs.ErrInvalid, err)
	}
	if !slices.EqualFunc(p.Changes, want.Changes, func(a, b Change) bool {
		return a.Step == b.Step && a.Path == b.Path && slices.Equal(a.Argv, b.Argv) && slices.Equal(a.Undo, b.Undo)
	}) {
		return fmt.Errorf("alpine: plan's steps do not follow from its state: %w", fs.ErrInvalid)
	}
	return nil
}

func removesLegacy(p Plan) bool {
	return slices.ContainsFunc(p.Changes, func(c Change) bool { return c.Step == StepRemoveLegacy })
}

// same: now matches the state p was planned from, in every field a step
// depends on.
func same(now, from Updates) bool {
	return now.Package == from.Package && now.Script == from.Script && now.Runlevel == from.Runlevel &&
		now.Crond == from.Crond && now.Legacy == from.Legacy && sameFile(now.LegacyFile, from.LegacyFile)
}

func sameFile(a, b File) bool {
	return bytes.Equal(a.Data, b.Data) && a.Mode == b.Mode && a.UID == b.UID && a.GID == b.GID
}

// do runs one step and checks its effect.
func do(ctx context.Context, sys host.System, p Plan, s Step) error {
	if s == StepRemoveLegacy {
		switch l, f, err := legacy(ctx, sys); {
		case err != nil:
			return fmt.Errorf("alpine: %s: %w", s, err)
		case l != LegacyHardn || !sameFile(f, p.From.LegacyFile):
			return fmt.Errorf("alpine: %s: %s: %w", s, LegacyScript, ErrChanged)
		}
		if err := sys.Remove(ctx, LegacyScript); err != nil {
			return fmt.Errorf("alpine: %s: %w", s, err)
		}
		return check(ctx, sys, p, s, true)
	}
	argv, _ := s.argv()
	if _, err := sys.Run(ctx, argv[0], argv[1:]...); err != nil {
		return fmt.Errorf("alpine: %s: %w", s, err)
	}
	return check(ctx, sys, p, s, true)
}

// undo reverses cs, last first; see Rollback.
func undo(ctx context.Context, sys host.System, p Plan, cs []Change) error {
	var errs []error
	for i := len(cs) - 1; i >= 0; i-- {
		if err := undoStep(ctx, sys, p, cs[i].Step); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("alpine: %w: %w", ErrNotRestored, err)
	}
	return nil
}

// undoStep reverses s when its effect is there, and checks that it is
// gone.
func undoStep(ctx context.Context, sys host.System, p Plan, s Step) error {
	var err error
	switch s {
	case StepRemoveLegacy:
		var l Legacy
		var f File
		switch l, f, err = legacy(ctx, sys); {
		case err != nil:
		case l == LegacyHardn && sameFile(f, p.From.LegacyFile):
			return nil
		case l != LegacyAbsent:
			err = fmt.Errorf("%s: %w", LegacyScript, ErrChanged)
		default:
			f = p.From.LegacyFile
			err = sys.WriteFileAtomic(ctx, LegacyScript, f.Data, f.Mode, f.UID, f.GID)
		}
		if err != nil {
			return fmt.Errorf("undo %s: %w", s, err)
		}
		return check(ctx, sys, p, s, false)
	case StepInstall:
		var t detect.Tri
		if t, err = installed(ctx, sys); err == nil && t == detect.No {
			return nil
		}
	case StepRunlevel:
		if _, err = sys.Stat(ctx, RunlevelLink); errors.Is(err, fs.ErrNotExist) {
			return nil
		}
	case StepStart, StepRestart:
		var st CrondState
		if st, err = status(ctx, sys); err == nil && st == CrondStopped {
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("undo %s: %w", s, err)
	}
	_, argv := s.argv()
	if _, err := sys.Run(ctx, argv[0], argv[1:]...); err != nil {
		return fmt.Errorf("undo %s: %w", s, err)
	}
	return check(ctx, sys, p, s, false)
}

// check reads whether step s is in effect, and wants it to be (forward)
// or to be undone: apk-cron installed with its daily script, or not
// installed; RunlevelLink present or not (rc-update add exits 0 for a
// runlevel that does not exist, so the link is what counts); crond started
// or stopped; LegacyScript absent, or back as it was.
func check(ctx context.Context, sys host.System, p Plan, s Step, forward bool) error {
	var ok bool
	var err error
	switch s {
	case StepInstall:
		var t detect.Tri
		var sc ScriptState
		if t, err = installed(ctx, sys); err == nil {
			sc, err = script(ctx, sys)
			ok = forward && t == detect.Yes && sc == ScriptInstalled || !forward && t == detect.No
		}
	case StepRunlevel:
		_, err = sys.Stat(ctx, RunlevelLink)
		ok = forward == (err == nil)
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
	case StepStart, StepRestart:
		var st CrondState
		st, err = status(ctx, sys)
		ok = forward && st == CrondStarted || !forward && st == CrondStopped
	case StepRemoveLegacy:
		var l Legacy
		var f File
		l, f, err = legacy(ctx, sys)
		ok = forward && l == LegacyAbsent || !forward && l == LegacyHardn && sameFile(f, p.From.LegacyFile)
	}
	verb := ""
	if !forward {
		verb = "undo "
	}
	switch {
	case err != nil:
		return fmt.Errorf("%s%s: check: %w", verb, s, err)
	case !ok:
		return fmt.Errorf("%s%s: %w", verb, s, ErrNotEffective)
	}
	return nil
}
