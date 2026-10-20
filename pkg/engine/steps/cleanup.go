package steps

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// Cleanup is the clean-up after a confirmed port change (brief 2.3 step 5:
// "Confirm the change from a new connection, then remove the old port
// rule"): the ports held during the confirm window dropped from
// 00-hardn.conf, and the ufw rules hardn added for them deleted.
type Cleanup struct {
	// Drop are the ports held during the window.
	Drop []uint16
	// Firewall are the confirmed change's ufw undo commands (Report.Undo,
	// its journal's run steps): a held port's rule is deleted only when
	// one of them deletes it, that is, when hardn added it (ufw.md §6).
	Firewall [][]string
}

// CleanupOf is the clean-up of a confirmed change, from its journal
// (confirmed.json keeps the last one, revert.md §2.5): Drop are the ports
// of the listeners its ssh reload restores (what sshd listened on before
// it) that no listener of ConfirmVia has; Firewall are its ufw delete
// commands. The zero Cleanup for a change that moved no port.
func CleanupOf(j revert.Journal) Cleanup {
	var c Cleanup
	if len(j.ConfirmVia) == 0 {
		return c
	}
	for _, r := range j.Reload {
		for _, l := range r.After {
			if r.Service == revert.ReloadSSH && !slices.ContainsFunc(j.ConfirmVia, func(v revert.Listener) bool { return v.Port == l.Port }) &&
				!slices.Contains(c.Drop, l.Port) {
				c.Drop = append(c.Drop, l.Port)
			}
		}
	}
	slices.Sort(c.Drop)
	for _, s := range j.Undo {
		if s.Op == revert.OpRun && len(s.Argv) == 4 && s.Argv[0] == "ufw" && s.Argv[1] == "delete" && s.Argv[2] == "allow" {
			c.Firewall = append(c.Firewall, slices.Clone(s.Argv))
		}
	}
	return c
}

// PlanCleanup plans c, a change of its own once the port change is
// confirmed: 00-hardn.conf's Port lines without c.Drop's ports, edited
// line by line and validated in context; on Proxmox VE pve.CheckPort over
// the ports left; the reload with reload.Change.Confirmed, which may only
// drop listeners (reload.ErrAddsListener otherwise); for that listener
// change, reload.CheckInEffect with the applying session's port; and
// before them, the deletion of the ufw rules hardn added for the dropped
// ports, those still there. A port another file keeps (a Port line in
// sshd_config) is refused, since hardn edits only 00-hardn.conf. Its
// changes are of lockout risk, confirmed like any other; none once the
// ports are gone. Who plans it, and when (the next run, `hardn confirm`, or
// the session that applied the change), is the maintainer's question
// (engine.md §7).
func PlanCleanup(ctx context.Context, sys host.System, in engine.Input, prefix string, c Cleanup) ([]engine.Change, error) {
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
	if !st.before.Port.Known || !st.before.Listeners.Known {
		return nil, fmt.Errorf("the ports sshd configures now: %w", reload.ErrListenersUnknown)
	}
	var drop []uint16
	var set []migrate.Directive
	for _, v := range st.before.Port.Values {
		n, err := strconv.ParseUint(v, 10, 16)
		switch {
		case err != nil:
			return nil, fmt.Errorf("sshd -T port %q: %w", v, reload.ErrListenersUnknown)
		case slices.Contains(c.Drop, uint16(n)):
			drop = append(drop, uint16(n))
		default:
			set = append(set, migrate.Directive{Keyword: "Port", Args: []string{v}})
		}
	}
	if len(drop) == 0 {
		return nil, nil
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("dropping %v would leave sshd no port: %w", drop, reload.ErrDropsListener)
	}
	var left []uint16
	for _, d := range set {
		n, _ := strconv.ParseUint(d.Args[0], 10, 16)
		left = append(left, uint16(n))
	}
	if err := pve.CheckPort(in.Facts.Proxmox, left); err != nil {
		return nil, err
	}
	e, err := st.edit(ctx, sys, prefix, set)
	if err != nil {
		return nil, err
	}
	if e == nil || slices.ContainsFunc(e.cand.Listeners.SSHD, func(l sshd.Listener) bool { return slices.Contains(drop, l.Port) }) {
		return nil, fmt.Errorf("port %v: still configured outside %s (%v), which hardn does not edit: %w", drop, sshd.HardnDropIn, st.before.Port.Values, ErrNoWriteStrategy)
	}
	rp, err := reload.PlanFor(reload.Input{SSH: in.Facts.SSH, DropIns: e.drops,
		Change: reload.Change{Keywords: e.keywords, Before: st.before.Listeners, After: e.cand.Listeners.SSHD, Confirmed: true}})
	if err != nil {
		return nil, err
	}
	if err := st.listenerChecks(ctx, sys, rp, st.before.Listeners); err != nil {
		return nil, err
	}
	var out []engine.Change
	if fw, err := st.cleanupFirewall(ctx, sys, prefix, drop, c.Firewall); err != nil {
		return nil, err
	} else if fw != nil {
		out = append(out, *fw)
	}
	r := sshReload(e.keywords, st.before.Listeners.SSHD, nil)
	return claim(append(out, e.change(r), reloadChange(prefix, r, rp, nil),
		st.verifyChange(prefix, fmt.Sprintf("sshd -T shows ports %v alone", left), func(_ context.Context, _ host.System, after sshd.Effective) error {
			return wanted(after, e.set, drop)
		}))), nil
}

// cleanupFirewall deletes the rules hardn added for the dropped ports
// (those firewall names), where ufw still has them: `ufw delete allow
// <port>/tcp`, with `ufw allow <port>/tcp` journalled before it, which
// keeps an existing rule when the delete never ran; then ufw is read back,
// as ufw.Execute does for every other ufw change, and a rule still there
// fails the step (ufw.ErrVerify). nil for none.
func (st sshState) cleanupFirewall(ctx context.Context, sys host.System, prefix string, drop []uint16, firewall [][]string) (*engine.Change, error) {
	var dels [][]string
	for _, p := range drop {
		del := []string{"ufw", "delete", "allow", fmt.Sprintf("%d/tcp", p)}
		if slices.ContainsFunc(firewall, func(a []string) bool { return slices.Equal(a, del) }) {
			dels = append(dels, del)
		}
	}
	if len(dels) == 0 {
		return nil, nil
	}
	state, err := ufw.Read(ctx, sys, st.in.Facts.Proxmox)
	if err != nil || !state.Installed {
		return nil, err
	}
	dels = slices.DeleteFunc(dels, func(del []string) bool { return !allows(state, del[3]) })
	if len(dels) == 0 {
		return nil, nil
	}
	var undo [][]string
	for _, d := range dels {
		undo = append([][]string{{"ufw", "allow", d[3]}}, undo...)
	}
	c := engine.Change{ID: prefix + "/firewall", Summary: "Firewall: delete the held ports' rules hardn added", Target: "ufw",
		Commands: formatArgvs(dels), Risk: engine.RiskLockout, Stage: engine.StageFirewall,
		Rollback: engine.Undo{Steps: runSteps(firewallWhy, undo)}}
	px := st.in.Facts.Proxmox
	c.Apply = func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
		for _, d := range dels {
			if _, err := sys.Run(ctx, d[0], d[1:]...); err != nil {
				return engine.Ran{}, fmt.Errorf("%s: %w", host.FormatArgv(d), err)
			}
		}
		after, err := ufw.Read(ctx, sys, px)
		if err != nil {
			return engine.Ran{}, fmt.Errorf("ufw after the deletes: %w", err)
		}
		var left []string
		for _, d := range dels {
			if allows(after, d[3]) {
				left = append(left, d[3])
			}
		}
		if len(left) > 0 {
			return engine.Ran{}, fmt.Errorf("ufw still allows %s after `ufw delete allow`: %w", strings.Join(left, ", "), ufw.ErrVerify)
		}
		return engine.Ran{Detail: "firewall: held ports' rules deleted, verified"}, nil
	}
	return &c, nil
}

// allows reports whether ufw's saved rules hold `allow <spec>` as `ufw
// allow <port>/tcp` writes it: in, from anywhere, to any address, on any
// interface.
func allows(st ufw.State, spec string) bool {
	return slices.ContainsFunc(st.Rules, func(r ufw.Rule) bool {
		return r.Action == "allow" && r.Dir == "in" && r.Proto == "tcp" && r.DPort+"/tcp" == spec && r.Dst == "" && r.Src == "" && r.App == "" && r.Iface == ""
	})
}
