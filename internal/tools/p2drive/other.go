package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
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
// -T must show what Check showed of the copy (sameEffective).
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
	removesListenConf := slices.ContainsFunc(plan.Ops, func(op migrate.Op) bool { return op.Kind == migrate.OpRemoveSocketDropIn })
	if apply && b.facts.SSH.Mode == detect.SSHSocket && removesListenConf {
		// The rollback would restore listen.conf and then plan the reload
		// with it in place, which reload.PlanFor refuses
		// (reload.ErrSocketDropIn): the rollback could not finish.
		return lc, fmt.Errorf("--apply on a socket-activated host whose plan removes %s: its rollback's reload would be refused (%w), so p2drive does not apply it (docs/safety/vmtest.md)", migrate.ListenConf, reload.ErrSocketDropIn)
	}
	if err := d.checkFree(ctx, rp); err != nil {
		return lc, err
	}
	lc.adds = rp.Adds
	if lc.ufw, err = d.planUFW(ctx, b, ufwKeep, portsOf(rp.Adds)); err != nil {
		return lc, err
	}
	var paths []string
	for _, s := range plan.Sources {
		paths = append(paths, s.Path)
	}
	lc.files = &files{
		paths: paths, keywords: plan.Keywords, restored: b.before.Listeners.SSHD, reload: rp,
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
	backupPath := fs.String("backup-path", backup.DefaultPath, "where the backups go, <path>/<id>/")
	planOnly := fs.Bool("plan-only", false, "stop after the plan; change nothing")
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
		return d.sudoers(ctx, pol, want, *backupPath, *planOnly)
	}
}

// sudoers grants pol through the tool detect's facts name (sudoers.Choose):
// the whole configuration and each candidate are checked first (visudo -c,
// visudo -cf on a copy, doas -C), every file it writes or removes is backed
// up, and a failed check after the write puts the old files back
// (sudoers.Apply). It is not journalled: cmd/hardn's revertPlans registers
// no plan rollback yet, so `hardn rollback` could not undo it; --grant none
// is its undo (docs/safety/vmtest.md).
func (d *driver) sudoers(ctx context.Context, pol sudoers.Policy, want sudoers.Tool, backupPath string, planOnly bool) error {
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
	switch {
	case len(plan.Changes) == 0:
		d.say("nothing to change")
		return nil
	case planOnly:
		d.say("--plan-only: nothing changed")
		return nil
	}
	id := revert.NewID(d.now())
	runDir := path.Join(runRoot, id)
	if err := d.sys.MkdirAll(ctx, runDir, 0o700); err != nil {
		return err
	}
	defer d.end(ctx, base{runDir: runDir})
	run, err := backup.Open(d.sys, backup.Settings{Path: backupPath, Enabled: true}, id)
	if err != nil {
		return err
	}
	for _, ch := range plan.Changes {
		r, err := run.Backup(ctx, ch.Path, backup.RollbackRequired)
		if err != nil {
			return err
		}
		d.say("backup %d: %s (%s) in %s", r.Entry.Seq, ch.Path, r.Entry.Kind, run.Dir())
	}
	res, err := sudoers.Apply(ctx, d.sys, plan, sudoers.Options{RunDir: runDir})
	if res.Cleanup != nil {
		d.say("the copies were not all removed: %v", res.Cleanup)
	}
	switch {
	case err != nil && res.Restored:
		return fmt.Errorf("%w (the old files are back and checked)", err)
	case errors.Is(err, sudoers.ErrNotRestored):
		return fmt.Errorf("%w; the backups are in %s", err, run.Dir())
	case err != nil:
		return err
	}
	d.say("installed %v; check it with `sudo -l -U %s` (or `doas -C`), and as %s, `sudo -n true`", res.Installed, pol.User, pol.User)
	return nil
}
