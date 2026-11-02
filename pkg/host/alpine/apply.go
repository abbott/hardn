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
	"strings"
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
	// Done are the steps whose action ran (a command, or the removal), in
	// order, including one that failed after acting and those an undo then
	// reversed. A step refused before it acted (apk's simulation listing
	// other packages, the legacy file changed on its re-read) is not in
	// it and is not undone.
	Done []Step
	// Legacy is the backup of LegacyScript, when the plan removes it: its
	// Entry names the copy in the run directory, for the note.
	Legacy *backup.Result
	// Restored: a step failed, and every step in Done was undone and
	// checked; false when Done is empty, since nothing changed. When the
	// undo did not complete, the error wraps ErrNotRestored.
	Restored bool
}

// Apply runs p. Before anything changes, it refuses a plan that is not
// what PlanUpdates makes from p.From (fs.ErrInvalid) and one that removes
// LegacyScript without Options.Backups (ErrNoBackup), re-reads the host
// and refuses one that no longer matches p.From (ErrChanged), and takes
// the backup of LegacyScript when p removes it, refusing a copy that is
// not exactly hardn's file. Then each step runs and is checked:
// apk-cron installed with its daily script (after apk add --simulate
// lists that one change and no other, else ErrOtherPackages),
// RunlevelLink present, crond started, LegacyScript (re-read and still
// exactly hardn's, else ErrChanged) gone; a step whose command succeeds
// without its effect fails with ErrNotEffective. When a step fails, the
// steps that acted, the failed one included, are undone last first under
// context.WithoutCancel with Options.RestoreTimeout: Result.Restored, or
// an error wrapping ErrNotRestored. An empty plan runs nothing.
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
	acted := 0 // p.Changes[:acted] acted
	for i, c := range p.Changes {
		var ran bool
		ran, err = do(ctx, sys, p, c.Step)
		if ran {
			res.Done, acted = append(res.Done, c.Step), i+1
		}
		if err != nil {
			break
		}
	}
	if err == nil || acted == 0 {
		return res, err
	}
	d := opts.RestoreTimeout
	if d <= 0 {
		d = DefaultRestoreTimeout
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d)
	defer cancel()
	rerr := undo(rctx, sys, p, p.Changes[:acted])
	res.Restored = rerr == nil
	return res, errors.Join(err, rerr)
}

// Rollback undoes p's steps, last first, each only when its effect is
// there: LegacyScript, when absent, is written back as LegacyContent with
// p.From.LegacyFile's mode and owner (anything else there is left alone:
// ErrChanged); crond is stopped when not stopped (a crashed crond, which
// restart replaced, reads stopped after); removed from the default
// runlevel when in it; apk-cron is removed when installed, after apk del
// --simulate lists that one change and no other (else ErrOtherPackages).
// Each undo is checked. A rollback cannot tell whether someone else made
// the same change since (started crond, say), and undoes it too: call it
// only for a plan whose Apply returned nil. A plan that does not follow
// from its From is refused, nothing undone. Every failure, that refusal
// included, wraps ErrNotRestored; it is idempotent. After Ctrl-C, run it
// under context.WithoutCancel with a timeout: every host.System method
// refuses a done context.
func Rollback(ctx context.Context, sys host.System, p Plan) error {
	if len(p.Changes) == 0 {
		return nil
	}
	if err := valid(p); err != nil {
		return fmt.Errorf("alpine: rollback refused, nothing undone: %w: %w", ErrNotRestored, err)
	}
	return undo(ctx, sys, p, p.Changes)
}

// Unit names an Alpine updates plan in p2-revert's change journal
// (revert.Step.Unit of its one OpPlan step, which pkg/host/revert/plans
// makes and whose Rollback it registers). Stable: a journal written by one
// hardn is rolled back by the next.
const Unit = "alpine-updates"

// Check is the guard Apply and Rollback run first (fs.ErrInvalid): p must
// be what PlanUpdates makes from its From, and From.LegacyFile, which a
// rollback writes back, must be hardn's file. It is pure, for p2-revert's
// journal: a plan is checked before it is journalled and again once it is
// read back (pkg/host/revert/plans).
func (p Plan) Check() error { return valid(p) }

// valid refuses a plan that is not what PlanUpdates makes from its From:
// steps added, dropped, reordered or naming other commands (a journal
// edited by hand would otherwise run what it names, or remove the legacy
// script before apk-cron is in), and a From whose LegacyFile, which a
// rollback writes back into DailyDir, is not one legacy could have read
// (isLegacy). Why is not compared.
func valid(p Plan) error {
	if p.From.Legacy == LegacyHardn && !isLegacy(p.From.LegacyFile) {
		return fmt.Errorf("alpine: plan's %s is not hardn's file: %w", LegacyScript, fs.ErrInvalid)
	}
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

// do runs one step and checks its effect. acted is false when it refused
// before its action: then the host is as it was, and the step is not
// undone.
func do(ctx context.Context, sys host.System, p Plan, s Step) (acted bool, err error) {
	argv, _ := s.argv()
	switch s {
	case StepRemoveLegacy:
		switch l, f, err := legacy(ctx, sys); {
		case err != nil:
			return false, fmt.Errorf("alpine: %s: %w", s, err)
		case l != LegacyHardn || !sameFile(f, p.From.LegacyFile):
			return false, fmt.Errorf("alpine: %s: %s: %w", s, LegacyScript, ErrChanged)
		}
		if err := sys.Remove(ctx, LegacyScript); err != nil {
			return true, fmt.Errorf("alpine: %s: %w", s, err)
		}
		return true, check(ctx, sys, p, s, true)
	case StepInstall:
		if err := simulate(ctx, sys, argv, "Installing"); err != nil {
			return false, fmt.Errorf("alpine: %s: %w", s, err)
		}
	}
	if _, err := sys.Run(ctx, argv[0], argv[1:]...); err != nil {
		return true, fmt.Errorf("alpine: %s: %w", s, err)
	}
	return true, check(ctx, sys, p, s, true)
}

// simulate runs argv (apk add or apk del apk-cron) with --simulate, which
// changes nothing (apk(8)), and refuses unless the one package change it
// lists is "<verb> apk-cron": apk commits the whole world, so a world file
// edited by hand and not yet applied would change other packages in the
// same commit (apk-add(8), apk-world(5)), openssh-server among them.
func simulate(ctx context.Context, sys host.System, argv []string, verb string) error {
	sim := append([]string{argv[1], "--simulate"}, argv[2:]...)
	res, err := sys.Run(ctx, argv[0], sim...)
	if err != nil {
		return err
	}
	cs := Changes(res.Combined)
	switch {
	case len(cs) == 1 && strings.HasPrefix(cs[0], verb+" "+Package+" ("):
		return nil
	case len(cs) == 0:
		return fmt.Errorf("%s: no package change listed: %w", host.FormatArgv(res.Argv), detect.ErrUnrecognised)
	}
	return fmt.Errorf("%s: %s: %w", host.FormatArgv(res.Argv), strings.Join(cs, "; "), ErrOtherPackages)
}

// Changes returns the package changes apk's commit lists, one per
// line as "(<i>/<n>) <verb> <name> (<version>)", the counter padded with
// spaces (apk-tools v3.0.8 src/commit.c, print_change), without the
// counter. Other lines (fetches, warnings, the OK: summary) are skipped.
func Changes(out []byte) []string {
	var cs []string
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		counter, rest, ok := strings.Cut(strings.TrimPrefix(l, "("), ") ")
		i, n, slash := strings.Cut(strings.TrimSpace(counter), "/")
		if ok && strings.HasPrefix(l, "(") && slash && digits(i) && digits(n) {
			cs = append(cs, strings.TrimSpace(rest))
		}
	}
	return cs
}

func digits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
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
			// The bytes are the constant; valid has checked the mode and
			// owner the plan carries.
			f = p.From.LegacyFile
			err = sys.WriteFileAtomic(ctx, LegacyScript, []byte(LegacyContent), f.Mode, f.UID, f.GID)
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
		if err == nil {
			_, argv := s.argv()
			err = simulate(ctx, sys, argv, "Purging")
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
