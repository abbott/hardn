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
// (check-ssh.md §2.11). Either is a path only where one of
// AuthenticationMethods' lists lets it in alone (check-ssh.md §10): a list
// such as publickey,password asks for a key first.
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
	lists := r.methods()
	restricted := !slices.Equal(lists, []string{"any"}) // "any": the rows as before §10
	if restricted {
		keys = append(keys, "authenticationmethods")
	}
	ev := r.said(keys...)
	if st, ok := r.stale(ctx, sys, f, ev); ok {
		return st, nil
	}
	pw, kbd, key, pam := r.eff.PasswordAuthentication, r.eff.KbdInteractiveAuthentication, r.eff.PubkeyAuthentication, r.eff.UsePAM
	st := engine.Status{Evidence: ev, Findings: r.conflicts(keys...)}
	switch {
	case pw.Known && pw.Value == "yes" && passwordAlone(lists, kbd.Value == "yes"):
		st.State, st.Detail = engine.NotConfigured, "password auth on"
		return st, nil
	case kbd.Known && kbd.Value == "yes" && kbdAlone(lists) && pam.Known && pam.Value == "yes":
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
	morePW, moreKbd = r.byMethods(lists, pw.Value == "yes", kbd.Value == "yes", morePW, moreKbd)
	kbdOn := kbd.Value == "yes" && kbdAlone(lists) // keyboard-interactive a path if sshd uses PAM
	var notPAM []engine.Finding                    // why the blocks' keyboard-interactive does not count, PAM not established
	if kbdOn || len(moreKbd) > 0 {
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
		case kbdOn || len(morePW) == 0: // PAM not established, and the state depends on it
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
	case restricted && keyAndPassword(lists, pw.Value == "yes", kbd.Value == "yes" && pam.Known && pam.Value == "yes"):
		st.State, st.Detail = engine.Configured, "key and password"
	default:
		st.State, st.Detail = engine.Configured, "key-only auth"
	}
	return st, nil
}

// methods are sshd -T's AuthenticationMethods lists, which it prints on one
// line apart by spaces ("authenticationmethods publickey,password
// publickey"; "any" where none is set, as on every VM of 2026-11-03). No
// line reads as "any", the check as before it read the keyword.
func (r reading) methods() []string {
	if ls := strings.Fields(strings.Join(r.eff.Keywords["authenticationmethods"], " ")); len(ls) > 0 {
		return ls
	}
	return []string{"any"}
}

// madeOf reports whether every method of list (a comma-separated chain, a
// method's ":submethod" ignored) is one of in, and which of in it names.
func madeOf(list string, in ...string) (ok bool, named map[string]bool) {
	named = map[string]bool{}
	for _, m := range strings.Split(list, ",") {
		m, _, _ = strings.Cut(m, ":")
		if !slices.Contains(in, m) {
			return false, nil
		}
		named[m] = true
	}
	return true, named
}

// passwordAlone: one of lists lets a password login in with password
// authentication on: "any", or a list of password alone, or of password
// and keyboard-interactive with keyboard-interactive on (counted whether or
// not sshd uses PAM: it errs toward a password path).
func passwordAlone(lists []string, kbd bool) bool {
	return slices.ContainsFunc(lists, func(l string) bool {
		if l == "any" {
			return true
		}
		in := []string{"password"}
		if kbd {
			in = append(in, "keyboard-interactive")
		}
		ok, named := madeOf(l, in...)
		return ok && named["password"]
	})
}

// kbdAlone: one of lists lets keyboard-interactive in alone: "any", or a
// list of keyboard-interactive alone.
func kbdAlone(lists []string) bool {
	return slices.ContainsFunc(lists, func(l string) bool {
		ok, _ := madeOf(l, "keyboard-interactive")
		return l == "any" || ok
	})
}

// keyAndPassword: no list lets a key in alone, and one asks for a key and a
// password (password on, or keyboard-interactive on where sshd uses PAM).
func keyAndPassword(lists []string, pw, kbd bool) bool {
	both := false
	for _, l := range lists {
		if ok, _ := madeOf(l, "publickey"); ok || l == "any" {
			return false
		}
		ok, named := madeOf(l, "publickey", "password", "keyboard-interactive")
		both = both || ok && named["publickey"] && (pw && named["password"] || kbd && named["keyboard-interactive"])
	}
	return both
}

// byMethods reads the Match blocks' AuthenticationMethods (check-ssh.md
// §10): of morePW and moreKbd (matchAllows') it keeps the lines whose
// block's lists (its own AuthenticationMethods, else global, sshd -T's) let
// the method in alone, and adds a block's AuthenticationMethods line whose
// lists let in alone password authentication, or keyboard-interactive, that
// is on for the block (its own line, else pw or kbd, sshd -T's) where no
// line of the block is counted already. Each block is read on its own:
// which connections it matches is not evaluated (§2.5).
func (r reading) byMethods(global []string, pw, kbd bool, morePW, moreKbd []sshd.Line) (pwOut, kbdOut []sshd.Line) {
	type says struct {
		am      *sshd.Line
		pw, kbd string // the block's first value, lower case; "" where it sets none
	}
	blocks, order := map[sshd.Block]*says{}, []sshd.Block{}
	for _, l := range sshd.ReadOrder(r.files.Main, r.files.DropIns) {
		if l.Match == nil {
			continue
		}
		b := blocks[*l.Match]
		if b == nil {
			b = &says{}
			blocks[*l.Match], order = b, append(order, *l.Match)
		}
		v := ""
		if len(l.Args) > 0 {
			v = strings.ToLower(l.Args[0])
		}
		switch {
		case l.Keyword == "authenticationmethods" && b.am == nil:
			b.am = &l
		case l.Keyword == "passwordauthentication" && b.pw == "":
			b.pw = v
		case l.Keyword == "kbdinteractiveauthentication" && b.kbd == "":
			b.kbd = v
		}
	}
	lists := func(b *says) []string {
		if b.am == nil {
			return global
		}
		return b.am.Args
	}
	on := func(v string, global bool) bool { return v == "yes" || v == "" && global }
	counted := map[sshd.Block]bool{}
	for _, l := range morePW {
		if b := blocks[*l.Match]; passwordAlone(lists(b), on(b.kbd, kbd)) {
			pwOut, counted[*l.Match] = append(pwOut, l), true
		}
	}
	for _, l := range moreKbd {
		if kbdAlone(lists(blocks[*l.Match])) {
			kbdOut, counted[*l.Match] = append(kbdOut, l), true
		}
	}
	for _, m := range order {
		switch b := blocks[m]; {
		case b.am == nil || counted[m]:
		case on(b.pw, pw) && passwordAlone(b.am.Args, on(b.kbd, kbd)):
			pwOut = append(pwOut, *b.am)
		case on(b.kbd, kbd) && kbdAlone(b.am.Args):
			kbdOut = append(kbdOut, *b.am)
		}
	}
	return pwOut, kbdOut
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
