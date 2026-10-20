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
	// (PlanCleanup). A port sshd already has is not added again: one that
	// is configured (in sshd -T's Port set and on one of its listeners) and
	// in effect (bound, and covering the applying session's port:
	// reload.CheckInEffect). One configured and not in effect is refused.
	Port uint16
	// UFW says whether ufw is enabled too (UFWEnable) or only lets the
	// added port through (UFWKeep). UFWEnable is planned when nothing else
	// is left to do, too, so that one SSH change serves a plan that asks
	// for SSH settings and ufw's enable together.
	UFW UFW
}

// PlanSSH plans s: on a host whose sshd_config starts with the stock
// Include and has no legacy hardn file (else ErrNoWriteStrategy,
// ErrMigrateFirst), 00-hardn.conf edited line by line (setDirectives), on
// Proxmox VE through pve.PlanSSH over the whole file it leaves (pveSet)
// with pve.CheckPort before a port change; the candidate validated in
// context (sshd.Validate over a copy in in.RunDir); its reload planned
// (reload.PlanFor); for a port change, the new port among the listeners
// the reload adds (ErrNoNewListener otherwise); for every reload that
// makes sshd bind what its files configure (a listener change, and any
// reload of ssh.service or OpenRC's sshd), what is configured must be what
// is bound and cover the applying session's port (reload.CheckInEffect);
// the added listeners free (reload.CheckFree); and ufw's part (UFW). All
// of it before anything changes. Its changes, all of lockout risk: the
// firewall when ufw has something to do, the write of 00-hardn.conf, the
// reload, and the check of sshd -T after it. None when sshd -T over the
// installed files already shows what is asked and 00-hardn.conf says so
// (idempotency); when only the file says so, it is refused (inForce).
func PlanSSH(ctx context.Context, sys host.System, in engine.Input, s SSH) ([]engine.Change, error) {
	if len(s.Set) == 0 && s.Port == 0 {
		if s.UFW == UFWEnable {
			return PlanUFWEnable(ctx, sys, in, s.Prefix)
		}
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
	if port != 0 {
		if err := pve.CheckPort(in.Facts.Proxmox, []uint16{port}); err != nil {
			return nil, err
		}
		if !st.before.Listeners.Known {
			return nil, fmt.Errorf("the listeners sshd configures now: %w", reload.ErrListenersUnknown)
		}
		if err := st.portInEffect(ctx, sys, port); err != nil {
			return nil, err
		}
		if st.configures(port) {
			port = 0 // configured and in effect already: nothing to add (idempotency)
		}
	}
	if port != 0 {
		var ports []migrate.Directive
		for _, p := range reload.HoldPorts(st.before.Listeners.All, []uint16{port}) {
			ports = append(ports, migrate.Directive{Keyword: "Port", Args: []string{strconv.Itoa(int(p))}})
		}
		set = append(ports, set...)
	}
	if len(set) == 0 {
		return st.enableUFW(ctx, sys, s.Prefix, s.UFW)
	}
	e, err := st.edit(ctx, sys, s.Prefix, set)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return st.enableUFW(ctx, sys, s.Prefix, s.UFW)
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
	if err := st.listenerChecks(ctx, sys, rp, st.before.Listeners); err != nil {
		return nil, err
	}
	fw, err := st.planUFW(ctx, sys, st.before.Listeners, s.UFW, portsOf(rp.Adds))
	if err != nil {
		return nil, err
	}
	var out []engine.Change
	r := sshReload(e.keywords, st.before.Listeners.SSHD, nil)
	files := e.change(r)
	if port != 0 {
		files.Title = addPortTitle(port, len(e.keywords)-1)
	}
	switch {
	case fw != nil && !fw.Empty():
		out = append(out, firewallChange(s.Prefix+"/firewall", *fw))
	case fw != nil:
		files.Findings = append(files.Findings, ufwFindings(fw.Findings)...)
	}
	out = append(out, files,
		reloadChange(s.Prefix, r, rp, nil),
		st.verifyChange(s.Prefix, "sshd -T shows "+directiveWords(e.set), func(ctx context.Context, sys host.System, after sshd.Effective) error {
			if err := wanted(after, e.set, nil); err != nil || !st.pve {
				return err
			}
			return st.checkPVE(ctx, sys, after, e.groups)
		}))
	out[len(out)-1].Title = engine.FitTitle(checkTitle(e.set), "Check the values with sshd -T")
	return claim(out), nil
}

// listenerChecks are the refusals of a reload, before anything changes.
// A reload that makes sshd bind what its files configure (rebinds) needs
// that set established (reload.ErrListenersUnknown) and what accepts
// connections now: bound, and covering the applying session
// (CheckInEffect). Otherwise a change that sets no listener keyword
// (keyonly) on a host whose files name a port sshd has not bound yet would
// move sshd there, every later check would pass, and the rollback, which
// restores hardn's file alone, would leave sshd there. The listeners a
// listener change adds must be free (CheckFree, the check reload.Execute
// makes before its first step).
func (st sshState) listenerChecks(ctx context.Context, sys host.System, rp reload.Plan, configured sshd.Listeners) error {
	if rebinds(rp) {
		if !configured.Known {
			return fmt.Errorf("the listeners sshd configures now, which the reload (%s) makes it bind: %w", unitOf(rp), reload.ErrListenersUnknown)
		}
		if err := st.inEffect(ctx, sys, configured.All); err != nil {
			return err
		}
	}
	return reload.CheckFree(ctx, sys, rp)
}

// rebinds reports whether rp makes sshd, or ssh.socket, bind what the
// files configure: a listener change, and any reload of ssh.service or of
// OpenRC's sshd, which re-executes on SIGHUP and reads every file again
// (reload.md §3). On ssh.socket a reload with no listener keyword is a
// reload of ssh.service, whose sshd keeps the sockets systemd passed it
// (reload.md §3), so it binds nothing.
func rebinds(rp reload.Plan) bool {
	return rp.Listen || len(rp.Steps) > 0 && (rp.Mode == detect.SSHService || rp.Mode == detect.SSHOpenRC)
}

// configures reports whether port is configured: in sshd -T's Port set and on
// one of its listeners.
func (st sshState) configures(port uint16) bool {
	return st.before.Port.Has(strconv.Itoa(int(port))) &&
		slices.ContainsFunc(st.before.Listeners.SSHD, func(l sshd.Listener) bool { return l.Port == port })
}

// portInEffect refuses a port that is configured (configures) but not what
// accepts connections now: sshd -T and ssh.socket read the files, which an
// edit not reloaded, or a daemon-reload not followed by a restart of the
// socket, leaves ahead of what is bound (sshd-effective.md §2.5). Such a
// port is not "already" there: planning nothing would report the host as
// asked, and planning around it would let the reload move sshd off the
// port the session uses. CheckInEffect over the established configured set
// decides it, from ss and the applying session's port.
func (st sshState) portInEffect(ctx context.Context, sys host.System, port uint16) error {
	if !st.configures(port) {
		return nil
	}
	if err := st.inEffect(ctx, sys, st.before.Listeners.All); err != nil {
		return fmt.Errorf("port %d is configured, not in effect: %w", port, err)
	}
	return nil
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
	// findings are pve.PlanSSH's and the validation's, for the write.
	findings []engine.Finding
}

// edit plans 00-hardn.conf with set in force, through pve.PlanSSH on
// Proxmox VE, and validates it in context. When the file says so already
// (setDirectives leaves it as it is, or it has set's lines in another
// layout: saysAlready), there is nothing to write, and edit returns nil
// once sshd -T shows them too (inForce).
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
		var pf []pve.Finding
		if set, pf, err = pveSet(st.in.Facts.Proxmox, old, set, e.groups); err != nil {
			return nil, err
		}
		e.findings = pveFindings(pf)
		e.keywords = keywordsOf(set)
	}
	e.set = set
	if e.data, err = setDirectives(old, set); err != nil {
		return nil, err
	}
	if !absent && (bytes.Equal(e.data, old) || saysAlready(old, set)) {
		return nil, st.inForce(set)
	}
	rep, err := sshd.Validate(ctx, sys, sshd.Request{Changes: []sshd.Change{{Path: sshd.HardnDropIn, Data: e.data}},
		RunDir: st.in.RunDir, Platform: st.in.Facts.Platform, Binary: st.binary})
	if err != nil {
		return nil, noRunDir(st.in.Facts.SSH, fmt.Errorf("validation in context: %w", err))
	}
	e.cand, _ = sshd.Evaluate(sshd.EffectiveInputs{Effective: rep.Effective})
	e.findings = append(e.findings, sshdFindings(rep.Findings)...)
	if st.pve {
		if err := st.checkPVECandidate(ctx, sys, e, rep); err != nil {
			return nil, err
		}
	}
	if e.drops, err = st.socketDropIns(ctx, sys); err != nil {
		return nil, err
	}
	return e, nil
}

// checkPVECandidate is pve.CheckSSH over the candidate, before anything
// changes: sshd -T over the validated copy, evaluated with detect's SSH
// facts (and on a socket host ssh.socket's listeners) so that its
// listeners are established as the check after the reload reads them, and
// the installed files with the candidate 00-hardn.conf, for their Match
// blocks. Root key-only, root by key from the other nodes and port 22 must
// each be Yes (pve.md §6): what the candidate already shows keeping root
// out (another drop-in's AllowUsers) is refused here, with ErrProxmoxRoot,
// rather than after the reload. The check after the reload stays
// (checkPVE).
func (st sshState) checkPVECandidate(ctx context.Context, sys host.System, e *edit, rep sshd.Report) error {
	drops := slices.DeleteFunc(slices.Clone(st.found.DropIns), func(f sshd.File) bool { return f.Path == sshd.HardnDropIn })
	drops = append(drops, sshd.File{Path: sshd.HardnDropIn, Data: e.data})
	in := sshd.EffectiveInputs{Effective: rep.Effective, Probes: rep.Probes, SSH: st.in.Facts.SSH, Main: st.found.Main, DropIns: drops}
	if sshd.SocketMayListen(st.in.Facts.SSH) {
		res, err := sys.Run(ctx, "systemctl", "show", "-p", "Listen", sshd.SocketUnit)
		if err != nil {
			return fmt.Errorf("%s listeners: %w", sshd.SocketUnit, err)
		}
		in.Socket = res.Stdout
	}
	cand, _ := sshd.Evaluate(in)
	chk, err := pve.CheckSSH(st.in.Facts.Proxmox, pve.SSHInput{Effective: cand, Main: st.found.Main, DropIns: drops, Root: e.groups})
	if err != nil {
		return err
	}
	if chk.RootKeyOnly == detect.Yes && chk.RootByKey == detect.Yes && chk.Port22 == detect.Yes {
		return nil
	}
	var why []string
	for _, f := range chk.Findings {
		if f.Kind == pve.FindingRootUnreachable || f.Kind == pve.FindingNotKeyOnly || f.Kind == pve.FindingPortNot22 {
			why = append(why, f.Detail)
		}
	}
	return fmt.Errorf("Proxmox VE: with %s as planned, root key-only %v, root by key %v, port 22 %v (%s); each must be yes: %w",
		sshd.HardnDropIn, chk.RootKeyOnly, chk.RootByKey, chk.Port22, strings.Join(why, "; "), ErrProxmoxRoot)
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
		ID: e.id, Summary: "Set " + strings.Join(e.keywords, ", ") + " in " + sshd.HardnDropIn, Title: setTitle(e.set, e.keywords), Target: sshd.HardnDropIn,
		Diff: engine.FileDiff(sshd.HardnDropIn, before, &after), Risk: engine.RiskLockout, Stage: engine.StageFiles,
		Backups: []string{sshd.HardnDropIn}, Rollback: engine.Undo{Reloads: []revert.Reload{r}}, Findings: slices.Clone(e.findings),
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
		ID: prefix + "/reload", Summary: "Reload sshd (" + rp.Mode.String() + ")", Title: engine.FitTitle("Reload sshd ("+rp.Mode.String()+")", "Reload sshd"), Target: unitOf(rp), Commands: cmds,
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
		ID: prefix + "/verify", Summary: summary, Title: engine.FitTitle(summary, "Check the values with sshd -T"), Target: "sshd -T", Commands: []string{"sshd -T"},
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

// inForce is the check of a change whose lines 00-hardn.conf has already:
// sshd -T over the installed files must show them (mismatch), as the check
// after the reload would. Otherwise another file sets the keyword first,
// or the value is Unknown, and a plan with nothing in it would report the
// host as asked while sshd says otherwise (brief §3 principle 3: the
// effective value, not the file). Refused with ErrNotInEffect, naming the
// lines sshd reads first.
func (st sshState) inForce(set []migrate.Directive) error {
	bad := mismatch(st.before, set, nil)
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("%s already says %s, but sshd -T over the installed files shows %s%s: %w",
		sshd.HardnDropIn, directiveWords(set), strings.Join(bad, "; "), setFirstBy(st.found, set), ErrNotInEffect)
}

// wanted checks that sshd -T shows what set asked for (mismatch), after
// the reload: ErrNotInEffect otherwise.
func wanted(after sshd.Effective, set []migrate.Directive, gone []uint16) error {
	if bad := mismatch(after, set, gone); len(bad) > 0 {
		return fmt.Errorf("sshd -T after the reload: %s: %w", strings.Join(bad, "; "), ErrNotInEffect)
	}
	return nil
}

// mismatch is what sshd -T, after, does not show of what set asks for:
// each Port in the Port set and on one of sshd's listeners (a port change
// must reach a listener: a ListenAddress with its own port ignores the
// Port lines), no listener left on a port of gone, and the
// lockout-relevant settings (brief 2.2). An Unknown value is not a pass.
func mismatch(after sshd.Effective, set []migrate.Directive, gone []uint16) []string {
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
	return bad
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

// settingWord is a setting in the user's words (design-system README
// §9.15): the title of the change that sets it, and what its check says
// sshd -T shows.
type settingWord struct{ set, shows string }

// settingWords are the settings with words of their own, keyed by the
// directive in lower case. KbdInteractiveAuthentication asks for the
// password by another prompt, so it shares PasswordAuthentication's words.
var settingWords = map[string]settingWord{
	"permitrootlogin no":                {"Disallow root login", "no root login"},
	"permitrootlogin prohibit-password": {"Disallow root password login", "no root password login"},
	"passwordauthentication no":         {"Disallow password login", "no password login"},
	"kbdinteractiveauthentication no":   {"Disallow password login", "no password login"},
}

// wordsOf is set's words when every directive has words and they are the
// same, as PasswordAuthentication no and KbdInteractiveAuthentication no
// are; ok is false otherwise.
func wordsOf(set []migrate.Directive) (w settingWord, ok bool) {
	for i, d := range set {
		dw, found := settingWords[strings.ToLower(d.Keyword+" "+strings.Join(d.Args, " "))]
		if !found || (i > 0 && dw != w) {
			return settingWord{}, false
		}
		w = dw
	}
	return w, len(set) > 0
}

// checkTitle is the Title of the check of set after the reload: "sshd -T
// shows no password login", "sshd -T shows ports 22, 2230"; the Summary
// keeps the directives.
func checkTitle(set []migrate.Directive) string {
	if w, ok := wordsOf(set); ok {
		return "sshd -T shows " + w.shows
	}
	return "sshd -T shows " + directiveWords(set)
}

// setTitle is a set change's Title: its settings' own words
// (settingWords), else its directives ("Set AllowUsers alice"), else its
// keywords, else the first keyword and how many more (engine.TitleMax);
// the plan's diff shows every line.
func setTitle(set []migrate.Directive, keywords []string) string {
	if w, ok := wordsOf(set); ok {
		return w.set
	}
	if t := engine.FitTitle("Set "+directiveWords(set), "Set "+strings.Join(keywords, ", ")); t != "" || len(keywords) == 0 {
		return t
	}
	return engine.FitTitle(fmt.Sprintf("Set %s and %d more", keywords[0], len(keywords)-1), "Set "+strconv.Itoa(len(keywords))+" SSH settings")
}

// addPortTitle is a port change's Title: the port it adds, as "Add SSH
// port 2230", and how many other settings it sets. The port it holds until
// the confirmation is not named: nothing happens to it until the clean-up
// drops it (P-40). The plan's diff shows both Port lines.
func addPortTitle(port uint16, more int) string {
	if more <= 0 {
		return fmt.Sprintf("Add SSH port %d", port)
	}
	return fmt.Sprintf("Add SSH port %d and %d more", port, more)
}

// directiveWords is set as one line, short: its Port lines as one group,
// "ports 22, 2230" where the first stands, the other directives as
// written, "PasswordAuthentication no".
func directiveWords(set []migrate.Directive) string {
	var out, ports []string
	at := -1
	for _, d := range set {
		if strings.EqualFold(d.Keyword, "Port") {
			if at < 0 {
				at = len(out)
				out = append(out, "")
			}
			ports = append(ports, strings.Join(d.Args, " "))
			continue
		}
		out = append(out, d.Keyword+" "+strings.Join(d.Args, " "))
	}
	if at >= 0 {
		out[at] = portWords(ports)
	}
	return strings.Join(out, ", ")
}

// portWords is ports as a title says them: "port 22", "ports 22, 2230".
func portWords(ports []string) string {
	if len(ports) == 1 {
		return "port " + ports[0]
	}
	return "ports " + strings.Join(ports, ", ")
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
