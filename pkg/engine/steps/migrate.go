package steps

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
)

// PlanMigration plans the migration of hardn's legacy SSH files into
// 00-hardn.conf (sshd-migrate.md §6, decision §3.17): migrate.Find with
// configFile (the loaded hardn.yml's sshConfigFile, "" for none),
// migrate.PlanFor, migrate.Check (validation and the login-path comparison
// over a copy in in.RunDir), the reload's plan, the rollback's reload
// planned too (migrate.Plan.CheckRollback; the D9 host is a Refusal,
// CodeListenConfNotInEffect, P-39 (a)), for a listener change
// reload.CheckInEffect with the applying session's port and
// reload.CheckFree, and ufw over the listeners that accept connections now
// (Plan.Listening). All before anything changes. Its changes, of lockout
// risk: the firewall when a port is added through an active ufw; the
// migration (StageFiles), whose files the engine backs up and journals
// before migrate.Apply runs, which takes those backups rather than its own
// (migrate.Options.Taken); the reload; and sshd -T after it, which must
// show what Check showed of the copy. None when there is nothing to
// migrate.
func PlanMigration(ctx context.Context, sys host.System, in engine.Input, prefix, configFile string) ([]engine.Change, error) {
	st, err := readSSH(ctx, sys, in, configFile)
	if err != nil {
		return nil, err
	}
	plan, err := migrate.PlanFor(migrate.Input{Found: st.found, SSH: in.Facts.SSH, Platform: in.Facts.Platform, Before: st.before})
	if err != nil || plan.Empty() {
		return nil, err
	}
	opts := migrate.Options{RunDir: in.RunDir, Binary: st.binary}
	after, checked, err := migrate.Check(ctx, sys, plan, opts)
	if err != nil {
		return nil, err
	}
	drops, err := st.socketDropIns(ctx, sys)
	if err != nil {
		return nil, err
	}
	rp, err := reload.PlanFor(reload.Input{SSH: in.Facts.SSH, Change: plan.Reload(after), DropIns: plan.SocketDropIns(drops)})
	if err != nil {
		return nil, err
	}
	if err := plan.CheckRollback(drops); err != nil {
		return nil, notInEffect(err, drops, st.before.Listeners.Socket)
	}
	if err := st.listenerChecks(ctx, sys, rp, plan.Listening); err != nil {
		return nil, err
	}
	fw, err := st.planUFW(ctx, sys, plan.Listening, UFWKeep, portsOf(rp.Adds))
	if err != nil {
		return nil, err
	}
	rb := plan.Rollback()
	var r *revert.Reload
	if len(rb.Keywords) > 0 {
		rr := sshReload(rb.Keywords, rb.After, rb.RestoredDropIns)
		r = &rr
	}
	files := migrationChange(prefix, plan, opts, r)
	files.Findings = append(droppedFindings(plan.Dropped), sshdFindings(checked)...)
	var out []engine.Change
	switch {
	case fw != nil && !fw.Empty():
		out = append(out, firewallChange(prefix+"/firewall", *fw))
	case fw != nil:
		files.Findings = append(files.Findings, ufwFindings(fw.Findings)...)
	}
	out = append(out, files)
	if r != nil {
		out = append(out, reloadChange(prefix, *r, rp, func(ctx context.Context, sys host.System) (reload.Plan, error) {
			drops, err := st.socketDropIns(ctx, sys)
			if err != nil {
				return reload.Plan{}, err
			}
			return reload.PlanFor(reload.Input{SSH: in.Facts.SSH, Change: plan.Reload(after), DropIns: drops})
		}))
	}
	return claim(append(out, st.verifyChange(prefix, "sshd -T shows the migrated configuration", func(_ context.Context, _ host.System, got sshd.Effective) error {
		return sameEffective(after, got)
	}))), nil
}

// migrationChange is migrate.Apply as a change: every file of
// Plan.Sources backed up by the engine and journalled, with the rollback's
// reload r (migrate.Plan.Rollback(): on a socket host listen.conf named as
// a drop-in the restore puts back), before Apply writes anything; Apply
// takes those backups (Options.Taken), so the journal names every backup
// of the files it changes, and no unjournalled second copy is made
// (sshd-migrate.md §6).
func migrationChange(prefix string, plan migrate.Plan, opts migrate.Options, r *revert.Reload) engine.Change {
	var paths []string
	for _, s := range plan.Sources {
		paths = append(paths, s.Path)
	}
	// No Commands: the migration runs none; its Diff shows every file it
	// writes or removes.
	c := engine.Change{
		ID: prefix + "/files", Summary: "Migrate hardn's legacy SSH files into " + sshd.HardnDropIn, Title: "Migrate hardn's legacy SSH files", Target: sshd.HardnDropIn,
		Diff: migrationDiff(plan), Risk: engine.RiskLockout, Stage: engine.StageFiles, Backups: paths,
	}
	if r != nil {
		c.Rollback.Reloads = []revert.Reload{*r}
	}
	c.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
		o := opts
		o.RunDir, o.Taken = run.Dir, run.Backups
		rep, err := migrate.Apply(ctx, sys, plan, o)
		return engine.Ran{Detail: fmt.Sprintf("migrated: %d steps", len(rep.Done))}, err
	}
	return c
}

// migrationDiff is each file the migration writes or removes, as a diff.
func migrationDiff(plan migrate.Plan) string {
	var b strings.Builder
	for _, s := range plan.Sources {
		var before, after *string
		if !s.Absent {
			v := string(s.Data)
			before = &v
		}
		for _, op := range plan.Ops {
			switch {
			case op.Kind == migrate.OpWriteDropIn && s.Path == sshd.HardnDropIn:
				v := string(op.Data)
				after = &v
			case op.Kind == migrate.OpRestoreMain && s.Path == sshd.HardnDropIn:
				v := string(op.DropIn)
				after = &v
			case op.Kind == migrate.OpRestoreMain && s.Path == sshd.MainConfig:
				v := string(op.Data)
				after = &v
			}
		}
		b.WriteString(engine.FileDiff(s.Path, before, after))
	}
	return b.String()
}

// notInEffect reports migrate.Plan.CheckRollback's refusal on the D9 host
// as a Refusal (P-39 (a), decided 2026-10-19: keep refusing, with a
// report): v0.3.x wrote ssh.socket's listen.conf and ran no daemon-reload,
// so the socket still listens where it did (port 22) while listen.conf
// names another port. The migration's rollback would restore listen.conf
// and its daemon-reload would move the socket to listen.conf's port alone,
// off the port the user connected on, so the rollback could not finish
// and the migration is not applied. Any other refusal is returned as it
// is.
func notInEffect(err error, drops reload.SocketDropIns, socket []sshd.Listener) error {
	if !errors.Is(err, reload.ErrRestoredListeners) {
		return err
	}
	i := slices.IndexFunc(drops.Files, func(f reload.SocketDropIn) bool { return f.Path == migrate.ListenConf })
	if i < 0 {
		return err
	}
	var ports []string
	for _, v := range drops.Files[i].Listen {
		if v == "" {
			continue
		}
		p := v[strings.LastIndex(v, ":")+1:]
		n, perr := strconv.ParseUint(p, 10, 16)
		if perr != nil || slices.ContainsFunc(socket, func(l sshd.Listener) bool { return uint64(l.Port) == n }) {
			return err // not only a port, or one the socket has: not D9
		}
		ports = append(ports, p)
	}
	if len(ports) == 0 {
		return err
	}
	on := strings.Join(ports, " and ")
	return &Refusal{Code: CodeListenConfNotInEffect, Err: err,
		Why: fmt.Sprintf("%s (hardn v0.3.x) sets ssh.socket to port %s, but it never took effect: ssh.socket is configured for %v (no daemon-reload since it was written, D9). Migrating it now could not be rolled back: the rollback's daemon-reload would move ssh.socket to port %s alone, off the port this host is reached on (P-39)",
			migrate.ListenConf, on, socket, on),
		Fix: fmt.Sprintf("first make sure the firewall lets port %s through: ssh.socket then listens on port %s alone. Then let listen.conf take effect while this session stays open: run `systemctl daemon-reload` and `systemctl restart ssh.socket` (established SSH sessions survive the restart), check that a new SSH connection to port %s works, and run hardn again. A reboot also lets it take effect, but ends this session: reboot only with console access, in case port %s is not reachable",
			on, on, on, on)}
}

// sameEffective checks that sshd -T after the reload shows what
// migrate.Check showed of the migrated copy: the lockout-relevant
// settings, the Port set and sshd's listeners. An Unknown on either side
// is not a pass.
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
		return fmt.Errorf("sshd -T does not show the migrated configuration: %s: %w", strings.Join(bad, "; "), ErrNotInEffect)
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
