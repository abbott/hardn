package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/revert/plans"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

func setupMigrate(fs *flag.FlagSet) func(context.Context, *driver) error {
	o := lockoutFlags(fs)
	apply := fs.Bool("apply", false, "carry the migration out under confirm-or-revert (without it: plan and check only)")
	configFile := fs.String("config-file", "", "the sshConfigFile a v0.3.x hardn.yml named, if any")
	return func(ctx context.Context, d *driver) error {
		o.planOnly = o.planOnly || !*apply
		return d.lockoutCommand(ctx, o, func(b base) (lockout, error) { return d.planMigrate(ctx, b, *configFile, *apply) })
	}
}

// planMigrate plans the migration of the legacy SSH files into
// 00-hardn.conf (docs/safety/sshd-migrate.md §6, the engine's order): Find,
// PlanFor, Check (sshd.Validate and the login-path comparison over a copy)
// and the reload's plan, all before anything changes. Once reloaded, sshd
// -T must show what Check showed of the copy (sameEffective). The
// rollback's reload restores the listeners that accept connections now,
// Plan.Listening.All (on a socket host, ssh.socket's), with the socket
// drop-ins the plan removes named as restored. On a socket host where
// listen.conf never took effect (D9) that rollback cannot give the socket
// back what it listens on, so --apply is refused there.
func (d *driver) planMigrate(ctx context.Context, b base, configFile string, apply bool) (lockout, error) {
	lc := lockout{base: b, summary: "legacy hardn SSH files migrated into 00-hardn.conf"}
	found, err := migrate.Find(ctx, d.sys, migrate.FindRequest{ConfigFile: configFile})
	if err != nil {
		return lc, err
	}
	for _, a := range found.Artifacts {
		d.say("legacy: %v (%v) %s", a.Kind, a.Gen, a.Path)
	}
	for _, l := range found.Left {
		d.say("left as it is: %s: %s", l.Path, l.Why)
	}
	plan, err := migrate.PlanFor(migrate.Input{Found: found, SSH: b.facts.SSH, Platform: b.facts.Platform, Before: b.before})
	if err != nil || plan.Empty() {
		return lc, err
	}
	for _, op := range plan.Ops {
		d.say("step %v %s: %s", op.Kind, op.Path, op.Note)
	}
	for _, c := range plan.Dropped {
		d.say("dropped %s:%d %q: %s", c.File, c.Line, c.Text, c.Why)
	}
	opts := migrate.Options{RunDir: b.runDir, Binary: b.binary}
	after, findings, err := migrate.Check(ctx, d.sys, plan, opts)
	if err != nil {
		return lc, err
	}
	d.sayEffective("migrated (a copy)", after, findings)
	drops, err := d.socketDropIns(ctx, b)
	if err != nil {
		return lc, err
	}
	rp, err := reload.PlanFor(reload.Input{SSH: b.facts.SSH, Change: plan.Reload(after), DropIns: plan.SocketDropIns(drops)})
	if err != nil {
		return lc, err
	}
	// The socket drop-ins the migration removes (listen.conf) come back
	// with the rollback's restore; its reload expects them
	// (revert.Reload.SocketDropIns), and checks the socket against the
	// listeners it has now, Plan.Listening.All (docs/safety/reload.md).
	var dropIns []string
	for _, op := range plan.Ops {
		if op.Kind == migrate.OpRemoveSocketDropIn {
			dropIns = append(dropIns, op.Path)
		}
	}
	if stale := notListening(plan.Hold, plan.Listening.All); apply && b.facts.SSH.Mode == detect.SSHSocket && len(dropIns) > 0 && len(stale) > 0 {
		return lc, fmt.Errorf("--apply on a socket-activated host whose ssh.socket does not listen on %v, which %s sets: "+
			"v0.3.x never ran daemon-reload after writing it (D9), so the rollback's daemon-reload, with %s restored, would move the socket "+
			"off %v, where it listens now, so p2drive does not apply it (docs/safety/vmtest.md)",
			stale, migrate.ListenConf, migrate.ListenConf, plan.Listening.All)
	}
	if err := d.checkFree(ctx, rp); err != nil {
		return lc, err
	}
	lc.adds = rp.Adds
	if lc.ufw, err = d.planUFW(ctx, b, plan.Listening, ufwKeep, portsOf(rp.Adds)); err != nil {
		return lc, err
	}
	var paths []string
	for _, s := range plan.Sources {
		paths = append(paths, s.Path)
	}
	lc.files = &files{
		paths: paths, keywords: plan.Keywords, restored: plan.Listening.All, socketDropIns: dropIns, reload: rp,
		write: func(ctx context.Context, run *backup.Run) (reload.Plan, error) {
			opts.Backup = run
			rep, err := migrate.Apply(ctx, d.sys, plan, opts)
			if err != nil {
				return reload.Plan{}, err
			}
			drops, err := d.socketDropIns(ctx, b)
			if err != nil {
				return reload.Plan{}, err
			}
			return reload.PlanFor(reload.Input{SSH: b.facts.SSH, Change: plan.Reload(rep.After), DropIns: drops})
		},
		want: func(_ context.Context, got sshd.Effective) error { return sameEffective(after, got) },
	}
	return lc, nil
}

// notListening lists the listeners of want that are not in now, by address
// and port.
func notListening(want, now []sshd.Listener) []sshd.Listener {
	var out []sshd.Listener
	for _, w := range want {
		if !slices.ContainsFunc(now, func(n sshd.Listener) bool { return n.Addr == w.Addr && n.Port == w.Port }) {
			out = append(out, w)
		}
	}
	return out
}

// sameEffective checks that sshd -T after the reload shows what
// migrate.Check showed of the migrated copy: the lockout-relevant settings,
// the Port set and sshd's listeners. An Unknown on either side is not a
// pass.
func sameEffective(want, got sshd.Effective) error {
	var bad []string
	for _, s := range []struct {
		kw        string
		want, got sshd.Setting
	}{
		{"permitrootlogin", want.PermitRootLogin, got.PermitRootLogin},
		{"passwordauthentication", want.PasswordAuthentication, got.PasswordAuthentication},
		{"kbdinteractiveauthentication", want.KbdInteractiveAuthentication, got.KbdInteractiveAuthentication},
		{"pubkeyauthentication", want.PubkeyAuthentication, got.PubkeyAuthentication},
	} {
		if !s.want.Known || !s.got.Known || s.want.Value != s.got.Value {
			bad = append(bad, fmt.Sprintf("%s %q (known %v), migrated copy %q (known %v)", s.kw, s.got.Value, s.got.Known, s.want.Value, s.want.Known))
		}
	}
	if !want.Port.Known || !got.Port.Known || !sameStrings(want.Port.Values, got.Port.Values) {
		bad = append(bad, fmt.Sprintf("port %v (known %v), migrated copy %v (known %v)", got.Port.Values, got.Port.Known, want.Port.Values, want.Port.Known))
	}
	str := func(ls []sshd.Listener) []string {
		var out []string
		for _, l := range ls {
			out = append(out, l.String())
		}
		return out
	}
	if !got.Listeners.Known || !sameStrings(str(want.Listeners.SSHD), str(got.Listeners.SSHD)) {
		bad = append(bad, fmt.Sprintf("listeners %v (known %v), migrated copy %v", got.Listeners.SSHD, got.Listeners.Known, want.Listeners.SSHD))
	}
	if len(bad) > 0 {
		return fmt.Errorf("sshd -T does not show the migrated configuration: %s", strings.Join(bad, "; "))
	}
	return nil
}

// sameStrings reports whether a and b hold the same values, in any order.
func sameStrings(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

func setupSudoers(fs *flag.FlagSet) func(context.Context, *driver) error {
	user := fs.String("user", "", "the user to grant, such as alice")
	grant := fs.String("grant", "nopasswd", "nopasswd, password, or none (remove hardn's files for the user)")
	vars := fs.String("env", "", "variables to keep, comma-separated, such as HARDN_CONFIG (sudo env_keep, doas setenv)")
	tool := fs.String("tool", "", "sudo or doas when both are installed (default: the one installed)")
	o := lockoutFlags(fs)
	return func(ctx context.Context, d *driver) error {
		pol := sudoers.Policy{User: *user}
		switch *grant {
		case "nopasswd":
			pol.Grant = sudoers.GrantNoPassword
		case "password":
			pol.Grant = sudoers.GrantPassword
		case "none":
			pol.Grant = sudoers.GrantNone
		default:
			return fmt.Errorf("--grant %q: nopasswd, password or none: %w", *grant, errUsage)
		}
		if *vars != "" {
			pol.Env = strings.Split(*vars, ",")
		}
		want := sudoers.ToolUnknown
		switch *tool {
		case "":
		case "sudo":
			want = sudoers.ToolSudo
		case "doas":
			want = sudoers.ToolDoas
		default:
			return fmt.Errorf("--tool %q: sudo or doas: %w", *tool, errUsage)
		}
		if *user == "" {
			return fmt.Errorf("--user is required: %w", errUsage)
		}
		return d.sudoers(ctx, pol, want, o)
	}
}

// sudoers grants pol through the tool detect's facts name (sudoers.Choose):
// the whole configuration and each candidate are checked first (visudo -c,
// visudo -cf on a copy, doas -C), every file it writes or removes is backed
// up, and a failed check after the write puts the old files back
// (sudoers.Apply). It is journalled as one plan step (plans.Sudoers),
// recorded before Apply, so that `hardn rollback <id>` undoes it with
// sudoers.Rollback, which puts back only a file that holds what the plan
// wrote. A plan with a lockout-risk change (a grant removed, or one that
// asks for a password, sudoers.RiskLockout) is a lockout-risk change under
// confirm-or-revert: the timer runs `hardn rollback <id>` unless a new SSH
// connection confirms, which also shows that elevation still works there.
// Any other is journalled without a timer and cleared once applied
// (revert.Done). A failure after Arm rolls it back at once; when Apply
// refused before it wrote anything, the plan step leaves the journal
// first, so that the rollback only ends the change.
func (d *driver) sudoers(ctx context.Context, pol sudoers.Policy, want sudoers.Tool, o *lockoutOpts) error {
	f, err := d.detect(ctx, d.sys)
	if err != nil {
		return err
	}
	c := sudoers.Choose(f, want)
	d.say("elevation: %v (flavour %v)", c.Tool, c.Flavor)
	if c.Err != nil {
		return c.Err
	}
	st, err := sudoers.Read(ctx, d.sys, c, []string{pol.User})
	if err != nil {
		return err
	}
	plan, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
	if err != nil {
		return err
	}
	for _, ch := range plan.Changes {
		verb := "write"
		if ch.Remove {
			verb = "remove"
		}
		d.say("plan: %s %s (%v, risk %v); checked with %s", verb, ch.Path, ch.Mode, ch.Risk, host.FormatArgv(ch.Check))
	}
	for _, fd := range plan.Findings {
		d.say("finding %v: %s:%d %s", fd.Kind, fd.File, fd.Line, fd.Detail)
	}
	lockout := slices.ContainsFunc(plan.Changes, func(ch sudoers.Change) bool { return ch.Risk == sudoers.RiskLockout })
	switch {
	case len(plan.Changes) == 0:
		d.say("nothing to change")
		return nil
	case o.planOnly:
		d.say("--plan-only: nothing changed")
		return nil
	}
	summary := fmt.Sprintf("%v rules for %s: grant %v", c.Tool, pol.User, pol.Grant)
	if len(pol.Env) > 0 {
		summary += ", keep " + strings.Join(pol.Env, " ")
	}
	step, err := plans.Sudoers(plan, summary)
	if err != nil {
		return fmt.Errorf("refused before anything changed: %w", err)
	}
	opts := d.revertOptions(o.hardn)
	j := revert.Journal{ID: revert.NewID(d.now()), Risk: revert.RiskOther, Summary: summary}
	runDir := path.Join(runRoot, j.ID)
	if err := d.sys.MkdirAll(ctx, runDir, 0o700); err != nil {
		return err
	}
	defer d.end(ctx, base{runDir: runDir})
	if lockout {
		if j, err = d.lockoutJournal(ctx, j, o); err != nil {
			return err
		}
	}
	if j, err = revert.Arm(ctx, d.sys, j, f.Init, opts); err != nil {
		return fmt.Errorf("not applied: %w", err)
	}
	fail := func(step string, err error) error { return d.rollBack(ctx, j.ID, opts, step, err) }
	// Write-ahead: the plan step before any file changes. Rollback undoes
	// only what holds the plan's content, so it is harmless if Apply never
	// wrote.
	j.Undo = []revert.Step{step}
	if err := revert.Record(ctx, d.sys, j, opts); err != nil {
		return fail("journal the sudoers plan", err)
	}
	run, err := backup.Open(d.sys, backup.Settings{Path: o.backupPath, Enabled: true}, j.ID)
	if err != nil {
		return fail("backups", err)
	}
	for _, ch := range plan.Changes {
		r, err := run.Backup(ctx, ch.Path, backup.RollbackRequired)
		if err != nil {
			return fail("backup of "+ch.Path, err)
		}
		d.say("backup %d: %s (%s) in %s", r.Entry.Seq, ch.Path, r.Entry.Kind, run.Dir())
	}
	res, err := sudoers.Apply(ctx, d.sys, plan, sudoers.Options{RunDir: runDir})
	if res.Cleanup != nil {
		d.say("the copies were not all removed: %v", res.Cleanup)
	}
	switch {
	case err != nil && len(res.Installed) == 0 && !res.Restored && !errors.Is(err, sudoers.ErrNotRestored):
		// Apply refused before it wrote anything (a configuration visudo
		// rejects already, a file changed since Read, Ctrl-C): the plan's
		// inverse is not needed, and would fail on the same cause, leaving
		// the rollback unfinished. It leaves the journal first, under a
		// context of its own; the rollback then only ends the change.
		j.Undo = nil
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), journalTimeout)
		rerr := revert.Record(rctx, d.sys, j, opts)
		cancel()
		return fail("sudoers", errors.Join(err, rerr))
	case err != nil && res.Restored:
		return fail("sudoers", fmt.Errorf("%w (the old files are back and checked)", err))
	case err != nil:
		return fail("sudoers", fmt.Errorf("%w; the backups are in %s", err, run.Dir()))
	}
	d.say("installed %v; check it with `sudo -l -U %s` (or `doas -C`), and as %s, `sudo -n true`", res.Installed, pol.User, pol.User)
	if !lockout {
		if err := revert.Done(ctx, d.sys, j.ID, opts); err != nil {
			return fail("clear the journal", err)
		}
		return nil
	}
	if j, err = revert.MarkApplied(ctx, d.sys, j.ID, opts); err != nil {
		return fail("mark applied", err)
	}
	d.say("change %s applied; it is rolled back at %s unless confirmed.", j.ID, j.Deadline().Format(time.RFC3339))
	d.say("to keep it: from another machine, open a new SSH connection as %s, check `sudo -n true` or `sudo -l` (doas on Alpine), and run `sudo %s confirm`.", pol.User, o.hardn)
	d.say("to undo it now: sudo %s rollback %s", o.hardn, j.ID)
	return nil
}

// lockoutJournal makes j a lockout-risk change for Arm: the confirm window
// (--timeout), the session applying it (revert.ReadSession), and the hardn
// binary the timer runs (--hardn), whose SHA-256 is printed.
func (d *driver) lockoutJournal(ctx context.Context, j revert.Journal, o *lockoutOpts) (revert.Journal, error) {
	timeout, err := o.window()
	if err != nil {
		return j, err
	}
	j.Risk, j.TimeoutSeconds = revert.RiskLockout, int(timeout/time.Second)
	if j.Session, err = d.session(ctx, d.sys); err != nil {
		d.say("session: %v (only a new SSH connection can confirm)", err)
	}
	d.sayHardn(ctx, o.hardn)
	return j, nil
}
