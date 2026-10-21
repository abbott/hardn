package ssh

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/sshd"
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
// UsePAM, or which build runs (otherPAM), is not established and the state
// depends on it. docs/engine/check-ssh.md lists each status line.
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
		why := keys
		if !kbd.Known || kbd.Value != "no" { // UsePAM decides nothing with keyboard-interactive off
			why = slices.Concat(keys, []string{"usepam"})
		}
		return unknown("can't read", ev, r.unknowns(why...)), nil
	}
	morePW, oddPW := r.matchAllows("passwordauthentication", "no")
	moreKbd, oddKbd := r.matchAllows("kbdinteractiveauthentication", "no") // every block that turns it on
	if odd := slices.Concat(oddPW, oddKbd); len(odd) > 0 {
		found, mev := matchFindings(odd, CodeMatchValueUnknown)
		return unknown("can't read", slices.Concat(ev, mev), found), nil
	}
	var notPAM []engine.Finding // why the blocks' keyboard-interactive does not count, PAM not established
	if kbd.Value == "yes" || len(moreKbd) > 0 {
		p, err := r.pam(ctx, sys, f) // otherPAM where which build runs is not established
		if err != nil {
			return failed(f.SSH, err)
		}
		_, kev := matchFindings(moreKbd, CodeMatchAllowsMore)
		st.Evidence = slices.Concat(st.Evidence, p.Evidence)
		switch {
		case p.Uses == detect.Yes: // a password path: a block counts below
		case p.Uses == detect.No: // no device: neither the global value nor a block's asks anything
			st.Evidence = slices.Concat(st.Evidence, kev, []string{noPrompt})
			moreKbd = nil
		case kbd.Value == "yes" || len(morePW) == 0: // PAM not established, and the state depends on it
			return unknown(p.Why, slices.Concat(st.Evidence, kev), p.Findings), nil
		default: // a block turns passwords on: Partial with PAM or without
			st.Evidence, notPAM, moreKbd = append(st.Evidence, kev...), r.unknowns("usepam"), nil
		}
	}
	found, mev := matchFindings(slices.Concat(morePW, moreKbd), CodeMatchAllowsMore)
	st.Evidence, st.Findings = append(st.Evidence, mev...), slices.Concat(st.Findings, found, notPAM)
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

// pamBuilds are the sshd builds with PAM that Alpine installs beside its
// /usr/sbin/sshd, built without (openssh-server-pam, openssh-server-krb5;
// check-ssh.md §3).
var pamBuilds = []string{reload.BinaryPAM, reload.BinaryKrb5}

// otherPAM is the evidence of each build of pamBuilds installed and
// executable whose own -G does not print usepam no: it would use PAM with
// these files. Check asks it where sshd -T's build has no PAM and OpenRC's
// init script does not start sshd (detect's SSH mode), so which build runs
// is not established: the init script picks one by the files
// (reload.OpenRCFlavour, which read used), but a container's entrypoint,
// or sshd started by hand, may run any of them (check-ssh.md §2.11).
func otherPAM(ctx context.Context, sys host.System) ([]string, error) {
	var ev []string
	for _, bin := range pamBuilds {
		fi, err := sys.Stat(ctx, bin)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return nil, fmt.Errorf("sshd's builds: %w", err)
		case fi.IsDir() || fi.Mode().Perm()&0o111 == 0:
			continue
		}
		res, err := sys.Run(ctx, bin, "-f", sshd.MainConfig, "-G")
		if err != nil {
			return nil, fmt.Errorf("%s -f %s -G: %w", bin, sshd.MainConfig, err)
		}
		var said []string
		for _, l := range strings.Split(string(res.Stdout), "\n") {
			if k, v, _ := strings.Cut(strings.TrimSpace(l), " "); strings.EqualFold(k, "usepam") {
				said = append(said, bin+" -G: usepam "+strings.ToLower(v))
			}
		}
		switch {
		case len(said) == 0:
			ev = append(ev, bin+" -G: no usepam")
		case !slices.Equal(said, []string{bin + " -G: usepam no"}):
			ev = append(ev, said...)
		}
	}
	return ev, nil
}
