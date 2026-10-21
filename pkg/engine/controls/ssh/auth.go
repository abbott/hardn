package ssh

import (
	"context"
	"slices"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Auth is SSH Auth: key-only authentication. The baseline, Proxmox VE's
// too, is PasswordAuthentication and KbdInteractiveAuthentication no, and
// PubkeyAuthentication yes. Keyboard-interactive on is Not Configured
// whether or not sshd uses PAM (with PAM it asks for the password; without
// it, what it asks is not read): a decision of this unit, pessimistic,
// never a host shown safe (check-ssh.md §2.11).
type Auth struct{ engine.CheckOnly }

// ID is "ssh.auth".
func (Auth) ID() string { return "ssh.auth" }

// Title is Figma's row label.
func (Auth) Title() string { return "SSH Auth" }

// Applies is true everywhere, as Login's.
func (Auth) Applies(detect.Facts) (bool, string) { return true, "" }

// Check reads the three settings as sshd -T prints them and every Match
// block that turns password or keyboard-interactive authentication on.
// docs/engine/check-ssh.md lists each status line.
func (Auth) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	r, err := read(ctx, sys, f)
	if err != nil {
		return failed(f.SSH, err)
	}
	keys := []string{"passwordauthentication", "kbdinteractiveauthentication", "pubkeyauthentication"}
	ev := r.said(keys...)
	if st, ok := r.stale(ctx, sys, f, ev); ok {
		return st, nil
	}
	pw, kbd, key := r.eff.PasswordAuthentication, r.eff.KbdInteractiveAuthentication, r.eff.PubkeyAuthentication
	st := engine.Status{Evidence: ev, Findings: r.conflicts(keys...)}
	switch {
	case pw.Known && pw.Value == "yes":
		st.State, st.Detail = engine.NotConfigured, "password auth on"
		return st, nil
	case kbd.Known && kbd.Value == "yes":
		st.State, st.Detail = engine.NotConfigured, "kbd-interactive on"
		return st, nil
	case !pw.Known || !kbd.Known || !key.Known:
		return unknown("can't read", ev, r.unknowns(keys...)), nil
	}
	morePW, oddPW := r.matchAllows("passwordauthentication", "no")
	moreKbd, oddKbd := r.matchAllows("kbdinteractiveauthentication", "no")
	if odd := slices.Concat(oddPW, oddKbd); len(odd) > 0 {
		found, mev := matchFindings(odd, CodeMatchValueUnknown)
		return unknown("can't read", slices.Concat(ev, mev), found), nil
	}
	found, mev := matchFindings(slices.Concat(morePW, moreKbd), CodeMatchAllowsMore)
	st.Evidence, st.Findings = append(st.Evidence, mev...), append(st.Findings, found...)
	switch {
	case len(found) > 0:
		st.State, st.Detail = engine.Partial, "match block allows passwords"
	case key.Value == "no":
		st.State, st.Detail = engine.Partial, "key auth off"
	default:
		st.State, st.Detail = engine.Configured, "key-only auth"
	}
	return st, nil
}
