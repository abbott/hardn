package steps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
)

// SSH is one change of hardn's sshd settings, as a control asks for it.
type SSH struct {
	// Prefix is the changes' id prefix, the control's id ("ssh"):
	// <prefix>/firewall, /files, /reload and /verify.
	Prefix string
	// Set are the directives 00-hardn.conf sets, besides the Port lines a
	// port change writes (PermitRootLogin prohibit-password,
	// PasswordAuthentication no).
	Set []migrate.Directive
	// Port, when not 0, is a port to add: 00-hardn.conf gets a Port line
	// for it beside the ports sshd listens on now (reload.HoldPorts), held
	// until the change is confirmed and the clean-up drops them
	// (PlanCleanup). A port sshd already has, in its Port set and on a
	// listener, is not added again.
	Port uint16
	// UFW says whether ufw is enabled too (UFWEnable) or only lets the
	// added port through (UFWKeep).
	UFW UFW
}

// PlanSSH plans s: on a host whose sshd_config starts with the stock
// Include and has no legacy hardn file (else ErrNoWriteStrategy,
// ErrMigrateFirst), 00-hardn.conf edited line by line (setDirectives), on
// Proxmox VE through pve.PlanSSH over the whole file it leaves (pveSet)
// with pve.CheckPort before a port change; the candidate validated in
// context (sshd.Validate over a copy in in.RunDir); its reload planned
// (reload.PlanFor); for a port change, the new port among the listeners
// the reload adds (ErrNoNewListener otherwise); for every listener change,
// what is configured must be what is bound and cover the applying
// session's port (reload.CheckInEffect); the added listeners free
// (reload.CheckFree); and ufw's part (UFW). All of it before anything
// changes. Its changes, all of lockout risk: the firewall when ufw has
// something to do, the write of 00-hardn.conf, the reload, and the check
// of sshd -T after it. None when 00-hardn.conf already says so
// (idempotency).
func PlanSSH(ctx context.Context, sys host.System, in engine.Input, s SSH) ([]engine.Change, error) {
	if len(s.Set) == 0 && s.Port == 0 {
		return nil, nil
	}
	st, err := readSSH(ctx, sys, in, "")
	if err != nil {
		return nil, err
	}
	if err := checkInclude(ctx, sys); err != nil {
		return nil, err
	}
	if err := st.noArtifacts(); err != nil {
		return nil, err
	}
	set := slices.Clone(s.Set)
	port := s.Port
	if port != 0 && st.before.Port.Has(strconv.Itoa(int(port))) &&
		slices.ContainsFunc(st.before.Listeners.SSHD, func(l sshd.Listener) bool { return l.Port == port }) {
		port = 0 // sshd listens there already: nothing to add (idempotency)
	}
	if port != 0 {
		if err := pve.CheckPort(in.Facts.Proxmox, []uint16{port}); err != nil {
			return nil, err
		}
		if !st.before.Listeners.Known {
			return nil, fmt.Errorf("the listeners sshd configures now: %w", reload.ErrListenersUnknown)
		}
		var ports []migrate.Directive
		for _, p := range reload.HoldPorts(st.before.Listeners.All, []uint16{port}) {
			ports = append(ports, migrate.Directive{Keyword: "Port", Args: []string{strconv.Itoa(int(p))}})
		}
		set = append(ports, set...)
	}
	if len(set) == 0 {
		return nil, nil
	}
	e, err := st.edit(ctx, sys, s.Prefix, set)
	if err != nil || e == nil {
		return nil, err
	}
	rp, err := reload.PlanFor(reload.Input{SSH: in.Facts.SSH, DropIns: e.drops,
		Change: reload.Change{Keywords: e.keywords, Before: st.before.Listeners, After: e.cand.Listeners.SSHD}})
	if err != nil {
		return nil, err
	}
	if port != 0 && !slices.ContainsFunc(rp.Adds, func(l sshd.Listener) bool { return l.Port == port }) {
		return nil, fmt.Errorf("port %d: sshd would listen on %v, no new listener on %d: a ListenAddress with its own port ignores the Port lines (sshd_config(5))%s: %w",
			port, e.cand.Listeners.SSHD, port, pinnedAt(st.found), ErrNoNewListener)
	}
	if err := st.listenerChecks(ctx, sys, rp, st.before.Listeners.All); err != nil {
		return nil, err
	}
	fw, err := st.planUFW(ctx, sys, st.before.Listeners, s.UFW, portsOf(rp.Adds))
	if err != nil {
		return nil, err
	}
	var out []engine.Change
	if fw != nil && !fw.Empty() {
		out = append(out, firewallChange(s.Prefix+"/firewall", *fw))
	}
	r := sshReload(e.keywords, st.before.Listeners.SSHD, nil)
	out = append(out, e.change(r),
		reloadChange(s.Prefix, r, rp, nil),
		st.verifyChange(s.Prefix, "sshd -T shows "+directiveWords(e.set), func(ctx context.Context, sys host.System, after sshd.Effective) error {
			if err := wanted(after, e.set, nil); err != nil || !st.pve {
				return err
			}
			return st.checkPVE(ctx, sys, after, e.groups)
		}))
	return out, nil
}

// listenerChecks are the refusals of a reload that moves listeners, before
// anything changes: configured must be what is bound and cover the
// applying session (CheckInEffect), and the listeners it adds free
// (CheckFree, the check reload.Execute makes before its first step).
func (st sshState) listenerChecks(ctx context.Context, sys host.System, rp reload.Plan, configured []sshd.Listener) error {
	if rp.Listen {
		if err := st.inEffect(ctx, sys, configured); err != nil {
			return err
		}
	}
	return reload.CheckFree(ctx, sys, rp)
}

// edit is a planned write of 00-hardn.conf.
type edit struct {
	id string
	// set is what the file sets, as pve.PlanSSH left it on Proxmox VE
	// (PermitRootLogin prohibit-password, never no; root added to a kept
	// AllowUsers): what sshd -T must show after the reload.
	set      []migrate.Directive
	old      []byte
	absent   bool
	data     []byte
	keywords []string
	cand     sshd.Effective // sshd -T over the candidate, by Evaluate
	drops    reload.SocketDropIns
	groups   pve.Groups
}

// edit plans 00-hardn.conf with set in force, through pve.PlanSSH on
// Proxmox VE, and validates it in context; nil when the file says so
// already.
func (st sshState) edit(ctx context.Context, sys host.System, prefix string, set []migrate.Directive) (*edit, error) {
	old, absent, err := readOptional(ctx, sys, sshd.HardnDropIn)
	if err != nil {
		return nil, err
	}
	e := &edit{id: prefix + "/files", old: old, absent: absent, keywords: keywordsOf(set)}
	if st.pve {
		if e.groups, err = pve.RootGroups(ctx, sys); err != nil {
			return nil, err
		}
		if set, _, err = pveSet(st.in.Facts.Proxmox, old, set, e.groups); err != nil {
			return nil, err
		}
		e.keywords = keywordsOf(set)
	}
	e.set = set
	if e.data, err = setDirectives(old, set); err != nil {
		return nil, err
	}
	if !absent && bytes.Equal(e.data, old) {
		return nil, nil
	}
	rep, err := sshd.Validate(ctx, sys, sshd.Request{Changes: []sshd.Change{{Path: sshd.HardnDropIn, Data: e.data}},
		RunDir: st.in.RunDir, Platform: st.in.Facts.Platform, Binary: st.binary})
	if err != nil {
		return nil, noRunDir(fmt.Errorf("validation in context: %w", err))
	}
	e.cand, _ = sshd.Evaluate(sshd.EffectiveInputs{Effective: rep.Effective})
	if e.drops, err = st.socketDropIns(ctx, sys); err != nil {
		return nil, err
	}
	return e, nil
}

// change is the write of 00-hardn.conf (StageFiles), after the engine has
// backed it up, and only while the backup shows it as planned. The ssh
// reload r is journalled with it, IfUp, before the write: an sshd that
// starts in between reads the new file, and the rollback then reloads it
// (vmtest.md §2.6). A write that fails once renamed (host.ErrNotDurable)
// fails the step, which the engine rolls back.
func (e *edit) change(r revert.Reload) engine.Change {
	var before *string
	if !e.absent {
		s := string(e.old)
		before = &s
	}
	after := string(e.data)
	sum := sha256.Sum256(e.old)
	data, absent := e.data, e.absent
	c := engine.Change{
		ID: e.id, Summary: "Set " + strings.Join(e.keywords, ", ") + " in " + sshd.HardnDropIn, Target: sshd.HardnDropIn,
		Diff: engine.FileDiff(sshd.HardnDropIn, before, &after), Risk: engine.RiskLockout, Stage: engine.StageFiles,
		Backups: []string{sshd.HardnDropIn}, Rollback: engine.Undo{Reloads: []revert.Reload{r}},
	}
	c.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
		b, ok := run.Backups[sshd.HardnDropIn]
		if err := sameAsRead(b, ok, sshd.HardnDropIn, absent, hex.EncodeToString(sum[:])); err != nil {
			return engine.Ran{}, err
		}
		return engine.Ran{Detail: "written"}, host.WriteFileAtomicKeep(ctx, sys, sshd.HardnDropIn, data, 0o644, 0, 0)
	}
	return c
}

// reloadChange is the reload that makes the written files take effect
// (StageReload), after <prefix>/files: reload.Execute of rp, or of the plan
// replan makes when set (after a migration, from the drop-ins read again).
// Its undo is the ssh reload r, IfUp, which the write journalled already
// (the engine journals it once); once the forward reload's gated step ran
// (reload.Report.Steps), also when Execute then failed, r without IfUp, so
// that the rollback starts a unit the change left down (revert.md §7 item
// 3). A lockout-risk change: a confirmation must reach one of the
// listeners it adds (rp.Adds).
func reloadChange(prefix string, r revert.Reload, rp reload.Plan, replan func(ctx context.Context, sys host.System) (reload.Plan, error)) engine.Change {
	var cmds []string
	for _, s := range rp.Steps {
		cmds = append(cmds, host.FormatArgv(s.Argv))
	}
	c := engine.Change{
		ID: prefix + "/reload", Summary: "Reload sshd (" + rp.Mode.String() + ")", Target: unitOf(rp), Commands: cmds,
		Risk: engine.RiskLockout, Stage: engine.StageReload, After: []string{prefix + "/files"},
		Rollback: engine.Undo{Reloads: []revert.Reload{r}}, ConfirmVia: revert.ListenersOf(rp.Adds),
	}
	c.Apply = func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
		p := rp
		if replan != nil {
			var err error
			if p, err = replan(ctx, sys); err != nil {
				return engine.Ran{}, err
			}
		}
		rep, err := reload.Execute(ctx, sys, p, reload.Options{})
		ran := engine.Ran{Detail: fmt.Sprintf("reload: verified %v%s", rep.Verified, because(rep.Unverified))}
		if slices.ContainsFunc(rep.Steps, func(s reload.StepReport) bool { return s.Step.Kind == reload.StepIfUp && s.Ran }) {
			up := r
			up.IfUp = false
			ran.Rollback = &engine.Undo{Reloads: []revert.Reload{up}}
		}
		return ran, err
	}
	return c
}

// unitOf names what rp reloads, for the plan's view.
func unitOf(rp reload.Plan) string {
	for _, s := range rp.Steps {
		if s.Unit != "" {
			return s.Unit
		}
	}
	return rp.Mode.String()
}

// verifyChange reads sshd -T again once the reload ran (StageVerify) and
// fails, so that the engine rolls the change back, unless want accepts it
// (brief 2.2: confirm the effective values). It changes nothing.
func (st sshState) verifyChange(prefix, summary string, want func(ctx context.Context, sys host.System, after sshd.Effective) error) engine.Change {
	binary, ssh := st.binary, st.in.Facts.SSH
	c := engine.Change{
		ID: prefix + "/verify", Summary: summary, Target: "sshd -T", Commands: []string{"sshd -T"},
		Risk: engine.RiskLockout, Stage: engine.StageVerify,
	}
	c.Apply = func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
		after, _, err := sshd.ReadEffective(ctx, sys, sshd.EffectiveRequest{SSH: ssh, Binary: binary})
		if err != nil {
			return engine.Ran{}, fmt.Errorf("sshd -T after the reload: %w", err)
		}
		if err := want(ctx, sys, after); err != nil {
			return engine.Ran{}, err
		}
		return engine.Ran{Detail: fmt.Sprintf("sshd -T: port %v, listeners %v", after.Port.Values, after.Listeners.SSHD)}, nil
	}
	return c
}

// wanted checks that sshd -T shows what set asked for: each Port in the
// Port set and on one of sshd's listeners (a port change must reach a
// listener: a ListenAddress with its own port ignores the Port lines), no
// listener left on a port of gone, and the lockout-relevant settings
// (brief 2.2). An Unknown value is not a pass.
func wanted(after sshd.Effective, set []migrate.Directive, gone []uint16) error {
	var bad []string
	on := func(p int) bool {
		return slices.ContainsFunc(after.Listeners.SSHD, func(l sshd.Listener) bool { return int(l.Port) == p })
	}
	for _, dir := range set {
		v := strings.ToLower(dir.Args[0])
		switch strings.ToLower(dir.Keyword) {
		case "port":
			n, _ := strconv.Atoi(v)
			switch {
			case !after.Port.Known || !after.Port.Has(v):
				bad = append(bad, fmt.Sprintf("port %s not in %v (known %v)", v, after.Port.Values, after.Port.Known))
			case !after.Listeners.Known || !on(n):
				bad = append(bad, fmt.Sprintf("no listener on port %s in %v (known %v)", v, after.Listeners.SSHD, after.Listeners.Known))
			}
		case "permitrootlogin":
			bad = appendSetting(bad, "permitrootlogin", v, after.PermitRootLogin)
		case "passwordauthentication":
			bad = appendSetting(bad, "passwordauthentication", v, after.PasswordAuthentication)
		case "kbdinteractiveauthentication":
			bad = appendSetting(bad, "kbdinteractiveauthentication", v, after.KbdInteractiveAuthentication)
		case "pubkeyauthentication":
			bad = appendSetting(bad, "pubkeyauthentication", v, after.PubkeyAuthentication)
		}
	}
	for _, p := range gone {
		if !after.Listeners.Known || on(int(p)) || after.Port.Has(strconv.Itoa(int(p))) {
			bad = append(bad, fmt.Sprintf("port %d still in %v, listeners %v (known %v)", p, after.Port.Values, after.Listeners.SSHD, after.Listeners.Known))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("sshd -T after the reload: %s: %w", strings.Join(bad, "; "), ErrNotInEffect)
	}
	return nil
}

func appendSetting(bad []string, kw, want string, s sshd.Setting) []string {
	if want == "without-password" {
		want = "prohibit-password"
	}
	if !s.Known || s.Value != want {
		return append(bad, fmt.Sprintf("%s %q, want %q (known %v)", kw, s.Value, want, s.Known))
	}
	return bad
}

// directiveWords is set as one line.
func directiveWords(set []migrate.Directive) string {
	var out []string
	for _, d := range set {
		out = append(out, d.Keyword+" "+strings.Join(d.Args, " "))
	}
	return strings.Join(out, ", ")
}

// checkPVE reads, after the reload, what Proxmox VE needs of sshd
// (pve.CheckSSH over sshd -T and the files): root key-only, root by key
// from the other nodes and port 22 must each be Yes. A No in RootByKey or
// Port22 locks the cluster out (pve.md §6), and an Unknown is not a pass.
func (st sshState) checkPVE(ctx context.Context, sys host.System, after sshd.Effective, groups pve.Groups) error {
	found, err := migrate.Find(ctx, sys, migrate.FindRequest{})
	if err != nil {
		return err
	}
	chk, err := pve.CheckSSH(st.in.Facts.Proxmox, pve.SSHInput{Effective: after, Main: found.Main, DropIns: found.DropIns, Root: groups})
	if err != nil {
		return err
	}
	if chk.RootKeyOnly != detect.Yes || chk.RootByKey != detect.Yes || chk.Port22 != detect.Yes {
		return fmt.Errorf("Proxmox VE: root key-only %v, root by key %v, port 22 %v; each must be yes: %w", chk.RootKeyOnly, chk.RootByKey, chk.Port22, ErrNotInEffect)
	}
	return nil
}
