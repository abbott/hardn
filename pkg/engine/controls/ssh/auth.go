package ssh

import (
	"context"
	"slices"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Auth is SSH Auth: key-only authentication. The baseline, Proxmox VE's
// too, is no password path and PubkeyAuthentication yes. A password path
// is PasswordAuthentication yes, or KbdInteractiveAuthentication yes where
// sshd uses PAM: on Linux PAM is keyboard-interactive's only device
// (auth2-chall.c), so with UsePAM no, or an sshd built without PAM
// (Alpine's), keyboard-interactive asks nothing and is refused at once
// (check-ssh.md §2.11).
type Auth struct{ engine.CheckOnly }

// ID is "ssh.auth".
func (Auth) ID() string { return "ssh.auth" }

// Title is Figma's row label.
func (Auth) Title() string { return "SSH Auth" }

// Applies is true everywhere, as Login's.
func (Auth) Applies(detect.Facts) (bool, string) { return true, "" }

// noPrompt is the evidence of why keyboard-interactive on is not a
// password path.
const noPrompt = "keyboard-interactive: no password prompt without PAM"

// Check reads the three settings and UsePAM as sshd -T prints them, and
// every Match block that turns password or keyboard-interactive
// authentication on. Keyboard-interactive on, globally or in a Match block
// (UsePAM may not be set in one), counts as a password path when UsePAM is
// yes, not when it is no or sshd is built without PAM, and is Unknown when
// UsePAM is not established. docs/engine/check-ssh.md lists each status
// line.
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
	pw, kbd, key, pam := r.eff.PasswordAuthentication, r.eff.KbdInteractiveAuthentication, r.eff.PubkeyAuthentication, r.eff.UsePAM
	st := engine.Status{Evidence: ev, Findings: r.conflicts(keys...)}
	switch {
	case pw.Known && pw.Value == "yes":
		st.State, st.Detail = engine.NotConfigured, "password auth on"
		return st, nil
	case kbd.Known && kbd.Value == "yes" && pam.Known && pam.Value == "yes":
		st.Evidence = append(st.Evidence, r.pamEvidence()...)
		st.State, st.Detail = engine.NotConfigured, "kbd-interactive on"
		return st, nil
	case !pw.Known || !kbd.Known || !key.Known:
		return unknown("can't read", ev, r.unknowns(append(keys, "usepam")...)), nil
	}
	morePW, oddPW := r.matchAllows("passwordauthentication", "no")
	moreKbd, oddKbd := r.matchAllows("kbdinteractiveauthentication", "no") // every block that turns it on
	if odd := slices.Concat(oddPW, oddKbd); len(odd) > 0 {
		found, mev := matchFindings(odd, CodeMatchValueUnknown)
		return unknown("can't read", slices.Concat(ev, mev), found), nil
	}
	if kbd.Value == "yes" || len(moreKbd) > 0 {
		st.Evidence = append(st.Evidence, r.pamEvidence()...)
		switch _, kev := matchFindings(moreKbd, CodeMatchAllowsMore); {
		case !pam.Known:
			return unknown("can't read", slices.Concat(st.Evidence, kev), r.unknowns("usepam")), nil
		case pam.Value == "no": // no device: neither the global value nor a block's asks anything
			st.Evidence = slices.Concat(st.Evidence, kev, []string{noPrompt})
			moreKbd = nil
		}
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

// pamEvidence is UsePAM's evidence: sshd -T's usepam lines as printed,
// or that it printed none because the build has no PAM.
func (r reading) pamEvidence() []string {
	if r.eff.BuiltWithoutPAM {
		return []string{"sshd -T: no usepam (built without PAM)"}
	}
	return r.said("usepam")
}
