package dns

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
	// Backups takes a backup of each file the plan writes
	// (backup.RollbackRequired, so whatever enableBackups says) before
	// anything changes. A plan that writes a file is refused without it
	// (ErrNoBackup).
	Backups *backup.Run
	// RestoreTimeout bounds the undo after a failed step, which runs under
	// context.WithoutCancel(ctx), so a Ctrl-C does not stop it. Not
	// positive: DefaultRestoreTimeout.
	RestoreTimeout time.Duration
}

// Result is what Apply did.
type Result struct {
	// Done are the steps that acted, in order, including one that failed
	// after acting. A step refused before it acted (its file changed since
	// the plan) is not in it and is not undone.
	Done    []Step
	Backups []backup.Result // one per file step, in plan order
	// Restored: a step failed, every file step in Done was undone and
	// checked, and, when a reload acted, systemd-resolved runs again with
	// p.From's global servers and search domains. A reload with no file
	// step to undo (a plan that only reloads) cannot be taken back: it is
	// Restored only when resolved shows p.From's lists anyway. When the
	// undo did not complete, the error wraps ErrNotRestored.
	Restored bool
}

// Apply runs p. Before anything changes, it refuses a plan that does not
// follow from p.From and p.Want (fs.ErrInvalid), one that writes a file
// without Options.Backups (ErrNoBackup), and a host that no longer matches
// p.From (ErrChanged); it then backs up each file the plan writes,
// refusing a copy that is not what p.From read. Each step is checked: a
// file must read back as written, and a drop-in directory it created as
// 0755; after the reload, resolvectl must list exactly p.Want's
// nameservers as global ones, and with ReplaceSearch its search domains.
// A step whose command succeeds without its effect fails with
// ErrNotEffective. When a step fails, the steps that acted are undone,
// last first, under context.WithoutCancel with Options.RestoreTimeout. An
// empty plan runs nothing.
func Apply(ctx context.Context, sys host.System, p Plan, opts Options) (res Result, err error) {
	if len(p.Changes) == 0 {
		return res, nil
	}
	if err := ctx.Err(); err != nil {
		return res, fmt.Errorf("dns: apply: %w", err)
	}
	if err := valid(p); err != nil {
		return res, err
	}
	files := slices.ContainsFunc(p.Changes, func(c Change) bool { return c.Step != StepReload })
	if files && opts.Backups == nil {
		return res, fmt.Errorf("dns: apply: %w", ErrNoBackup)
	}
	now := read(ctx, sys, p.From.Host)
	if err := ctx.Err(); err != nil {
		return res, fmt.Errorf("dns: apply: %w", err)
	}
	if now.Err != nil {
		return res, fmt.Errorf("dns: apply: re-read: %w", now.Err)
	}
	if !same(now, p.From) {
		return res, fmt.Errorf("dns: apply: %w", ErrChanged)
	}
	for _, c := range p.Changes {
		if c.Step == StepReload {
			continue
		}
		b, err := opts.Backups.Backup(ctx, c.Path, backup.RollbackRequired)
		if err != nil {
			return res, fmt.Errorf("dns: apply: %w", err)
		}
		if !backedUp(b.Entry, from(p, c.Step)) {
			return res, fmt.Errorf("dns: apply: %s: backed up as %s, not as read: %w", c.Path, b.Entry.Kind, ErrChanged)
		}
		res.Backups = append(res.Backups, b)
	}
	acted := 0 // p.Changes[:acted] acted
	for i, c := range p.Changes {
		var ran bool
		ran, err = do(ctx, sys, p, c)
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
// there: a file that still holds what the plan wrote gets p.From's content,
// mode and owner back (or is removed, when there was none; the drop-in
// directory too, when the plan created it and it is empty); a file holding
// anything else is left alone (ErrChanged). With a reload in the plan,
// systemd-resolved is reloaded again after the drop-in is back (started,
// if it no longer runs) and must list p.From's global nameservers and
// search domains; a plan that only reloads cannot be taken back, so its
// rollback fails unless resolved shows them anyway. Every failure, a plan
// that does not follow from its From included, wraps ErrNotRestored; it is
// idempotent. It cannot tell its own change from the same change made by
// someone else since: call it only for a plan whose Apply returned nil.
// After Ctrl-C, run it under context.WithoutCancel with a timeout: every
// host.System method refuses a done context.
func Rollback(ctx context.Context, sys host.System, p Plan) error {
	if len(p.Changes) == 0 {
		return nil
	}
	if err := valid(p); err != nil {
		return fmt.Errorf("dns: rollback refused, nothing undone: %w: %w", ErrNotRestored, err)
	}
	return undo(ctx, sys, p, p.Changes)
}

// valid refuses a plan that is not what PlanDNS makes from its From and
// Want, or whose From names a manager its own entries do not give: a
// journal edited by hand must not write what it names.
func valid(p Plan) error {
	if m, _ := classify(p.From.ResolvConf, p.From.Resolved.Active, p.From.Host); m != p.From.Manager {
		return fmt.Errorf("dns: plan's manager %s does not follow from its state: %w", p.From.Manager, fs.ErrInvalid)
	}
	want, err := PlanDNS(p.From, p.Want)
	if err != nil {
		return fmt.Errorf("dns: plan does not follow from its state: %w: %w", fs.ErrInvalid, err)
	}
	if !slices.EqualFunc(p.Changes, want.Changes, func(a, b Change) bool {
		return a.Step == b.Step && a.Path == b.Path && bytes.Equal(a.Before, b.Before) && bytes.Equal(a.After, b.After) && slices.Equal(a.Argv, b.Argv)
	}) || !slices.Equal(p.Want.Nameservers, want.Want.Nameservers) || !slices.Equal(p.Want.Search, want.Want.Search) ||
		p.Want.ReplaceSearch != want.Want.ReplaceSearch || p.Want.UDHCPCNoResolv != want.Want.UDHCPCNoResolv {
		return fmt.Errorf("dns: plan's steps do not follow from its state: %w", fs.ErrInvalid)
	}
	return nil
}

// same: now matches the state p was planned from, in everything a step
// depends on. Per-link servers and DHCP clients are not compared: they
// change a finding, not a step.
func same(now, from State) bool {
	return now.Manager == from.Manager && now.ResolvConf.equal(from.ResolvConf) &&
		now.Resolved.Active == from.Resolved.Active && now.Resolved.Dir == from.Resolved.Dir &&
		now.Resolved.DropIn.equal(from.Resolved.DropIn) && slices.Equal(now.Resolved.Later, from.Resolved.Later) &&
		slices.Equal(now.Resolved.Global, from.Resolved.Global) && slices.Equal(now.Resolved.Domains, from.Resolved.Domains) &&
		now.UDHCPC.Conf.equal(from.UDHCPC.Conf)
}

// from is what a file step's path held when p was planned.
func from(p Plan, s Step) Entry {
	switch s {
	case StepUDHCPC:
		return p.From.UDHCPC.Conf
	case StepResolvConf:
		return p.From.ResolvConf
	case StepDropIn:
		return p.From.Resolved.DropIn
	}
	return Entry{}
}

// backedUp: the backup recorded e: nothing, or exactly its bytes.
func backedUp(b backup.Entry, e Entry) bool {
	if e.Kind == EntryAbsent {
		return b.Kind == backup.KindAbsent
	}
	sum := sha256.Sum256(e.Data)
	return b.Kind == backup.KindFile && b.File != nil && b.File.SHA256 == hex.EncodeToString(sum[:])
}

// target is the mode and owner a file step writes: the file's own, or
// root's 0644 for a new one.
func target(e Entry) (fs.FileMode, int, int) {
	if e.Kind == EntryFile {
		return e.Mode, e.UID, e.GID
	}
	return 0o644, 0, 0
}

// do runs one step and checks its effect. acted is false when it refused
// before acting: the host is then as it was, and the step is not undone.
func do(ctx context.Context, sys host.System, p Plan, c Change) (acted bool, err error) {
	if c.Step == StepReload {
		if _, err := sys.Run(ctx, c.Argv[0], c.Argv[1:]...); err != nil {
			return true, fmt.Errorf("dns: %s: %w", c.Step, err)
		}
		return true, loaded(ctx, sys, c.Step, p.Want.Nameservers, p.Want.Search, p.Want.ReplaceSearch)
	}
	was := from(p, c.Step)
	switch cur, err := readEntry(ctx, sys, c.Path); {
	case err != nil:
		return false, fmt.Errorf("dns: %s: %w", c.Step, err)
	case !cur.equal(was):
		return false, fmt.Errorf("dns: %s: %s: %w", c.Step, c.Path, ErrChanged)
	}
	if c.Step == StepDropIn && p.From.Resolved.Dir == detect.No {
		if err := mkdir(ctx, sys); err != nil {
			return true, fmt.Errorf("dns: %s: %w", c.Step, err)
		}
	}
	mode, uid, gid := target(was)
	if err := sys.WriteFileAtomic(ctx, c.Path, c.After, mode, uid, gid); err != nil {
		return true, fmt.Errorf("dns: %s: %w", c.Step, err)
	}
	return true, check(ctx, sys, c.Step, c.Path, Entry{Kind: EntryFile, Data: c.After, Mode: mode, UID: uid, GID: gid})
}

// mkdir creates DropInDir 0755 whatever the umask: systemd-resolved runs as
// its own user (User=systemd-resolve) and must read it.
func mkdir(ctx context.Context, sys host.System) error {
	if err := sys.MkdirAll(ctx, DropInDir, 0o755); err != nil {
		return err
	}
	if err := sys.Chmod(ctx, DropInDir, 0o755); err != nil {
		return err
	}
	switch fi, err := sys.Lstat(ctx, DropInDir); {
	case err != nil:
		return err
	case !fi.IsDir() || fi.Mode().Perm() != 0o755:
		return fmt.Errorf("%s is %v: %w", DropInDir, fi.Mode(), ErrNotEffective)
	}
	return nil
}

// check reads path back and wants it to be e.
func check(ctx context.Context, sys host.System, s Step, path string, e Entry) error {
	cur, err := readEntry(ctx, sys, path)
	switch {
	case err != nil:
		return fmt.Errorf("dns: %s: check: %w", s, err)
	case !cur.equal(e):
		return fmt.Errorf("dns: %s: %s: %w", s, path, ErrNotEffective)
	}
	return nil
}

// loaded wants systemd-resolved's global nameservers to be servers and,
// with withDomains, its global search domains to be domains.
func loaded(ctx context.Context, sys host.System, s Step, servers, domains []string, withDomains bool) error {
	g, _, err := resolvectl(ctx, sys, "dns")
	switch {
	case err != nil:
		return fmt.Errorf("dns: %s: check: %w", s, err)
	case !slices.Equal(g, servers):
		return fmt.Errorf("dns: %s: global nameservers %q, want %q: %w", s, g, servers, ErrNotEffective)
	case !withDomains:
		return nil
	}
	d, _, err := resolvectl(ctx, sys, "domain")
	switch {
	case err != nil:
		return fmt.Errorf("dns: %s: check: %w", s, err)
	case !sameDomains(d, domains):
		return fmt.Errorf("dns: %s: global search domains %q, want %q: %w", s, d, domains, ErrNotEffective)
	}
	return nil
}

// restartArgv reloads a running systemd-resolved, or starts a stopped one
// (systemctl(1) reload-or-restart): the undo wants it running, as p.From
// found it.
var restartArgv = []string{"systemctl", "reload-or-restart", ResolvedUnit}

// undo reverses cs, last first. A reload is undone once the files are
// back: systemd-resolved is reloaded, or started if the forward restart
// left it stopped, and must show p.From's lists again. With no file in cs
// (a plan that only reloads) there is nothing to read back: resolved is
// only started again if it stopped, and the check says whether it shows
// p.From's lists anyway.
func undo(ctx context.Context, sys host.System, p Plan, cs []Change) error {
	var errs []error
	reload, files := false, false
	for i := len(cs) - 1; i >= 0; i-- {
		if cs[i].Step == StepReload {
			reload = true
			continue
		}
		files = true
		if err := undoFile(ctx, sys, p, cs[i]); err != nil {
			errs = append(errs, err)
		}
	}
	if reload {
		if err := undoReload(ctx, sys, p, files); err != nil {
			errs = append(errs, fmt.Errorf("undo: %w", err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("dns: %w: %w", ErrNotRestored, err)
	}
	return nil
}

// undoReload brings systemd-resolved back to p.From's lists after files
// (whether cs held a file step) were put back, as undo describes.
func undoReload(ctx context.Context, sys host.System, p Plan, files bool) error {
	run := files
	if !files {
		a, err := resolvedActive(ctx, sys, p.From.Host)
		if err != nil {
			return err
		}
		run = a != detect.Yes
	}
	if run {
		if _, err := sys.Run(ctx, restartArgv[0], restartArgv[1:]...); err != nil {
			return fmt.Errorf("dns: %s: %w", StepReload, err)
		}
	}
	r := p.From.Resolved
	err := loaded(ctx, sys, StepReload, r.Global, r.Domains, true)
	if err != nil && !files {
		return fmt.Errorf("the reload loaded files that were on disk before the plan; what systemd-resolved used before cannot be loaded back: %w", err)
	}
	return err
}

// undoFile puts back what a file step's path held, when it still holds
// what the step wrote.
func undoFile(ctx context.Context, sys host.System, p Plan, c Change) error {
	was := from(p, c.Step)
	cur, err := readEntry(ctx, sys, c.Path)
	switch {
	case err != nil:
	case cur.equal(was):
	case cur.Kind != EntryFile || !bytes.Equal(cur.Data, c.After):
		err = fmt.Errorf("%s: %w", c.Path, ErrChanged)
	case was.Kind == EntryAbsent:
		err = sys.Remove(ctx, c.Path)
	default:
		err = sys.WriteFileAtomic(ctx, c.Path, was.Data, was.Mode, was.UID, was.GID)
	}
	if err == nil {
		err = check(ctx, sys, c.Step, c.Path, was)
	}
	if err == nil && c.Step == StepDropIn && p.From.Resolved.Dir == detect.No {
		err = removeIfEmpty(ctx, sys, DropInDir)
	}
	if err != nil {
		return fmt.Errorf("dns: undo %s: %w", c.Step, err)
	}
	return nil
}

// removeIfEmpty removes the directory the plan created, unless something
// else has been put there since.
func removeIfEmpty(ctx context.Context, sys host.System, dir string) error {
	des, err := sys.ReadDir(ctx, dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case len(des) > 0:
		return nil
	}
	return sys.Remove(ctx, dir)
}
