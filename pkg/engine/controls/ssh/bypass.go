package ssh

import (
	"context"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// CVE-2026-123158 (docs/engine/check-ssh.md §11, docs/safety/alpine.md):
// on musl without PAM, sshd's sys_auth_passwd borrows another account's
// salt where the stored field is shorter than 2 characters, and musl's
// SHA-256/512 crypt returns "*" for a key over 256 bytes, so a password of
// 257 to 1024 bytes matches a field of exactly "*". A host is exposed when
// sshd checks passwords without PAM, a password path is on (globally, or
// for some account by a Match block), a login account's field is exactly
// "*" (root only with PermitRootLogin yes: auth_password sets ok = 0
// otherwise), at least one account has a $5$ or $6$ hash (Alpine's rule),
// and the openssh-server package lacks Alpine's fix. Fields of "!" are
// locked: not affected. hardn reads the package, not the running daemon,
// so an upgrade reads as fixed at once: each new connection runs the new
// sshd-session from disk (docs/safety/alpine.md §7.1, the 2026-11-08 probe).

// The CVE's finding codes (pkg/output's registry holds them).
const (
	// CodePasswordBypass: the host is exposed now: anyone can log in as
	// the accounts the finding names with a long password.
	CodePasswordBypass = "SSH_PASSWORD_BYPASS"
	// CodeSSHDUnfixed: sshd lacks the fix, checks passwords without PAM
	// with a password path on, but no login account's field is "*" or no
	// account has a $5$ or $6$ hash yet.
	CodeSSHDUnfixed = "SSH_SSHD_UNFIXED"
	// CodeSSHDUnfixedLatent: sshd lacks the fix, with no password path to
	// it established: password login off, sshd checking passwords
	// through PAM, or either not established. The upgrade is still
	// offered (SSH Login's item 5, Run All), so the row says why.
	CodeSSHDUnfixedLatent = "SSH_SSHD_UNFIXED_LATENT"
	// CodeFixUnknown: whether the openssh package has the fix, or with it
	// unfixed which accounts sshd would let in, is not established.
	CodeFixUnknown = "SSH_FIX_UNKNOWN"
)

// CVE is the advisory, as the words name it.
const CVE = "CVE-2026-123158"

// Bypass is what ReadBypass read of CVE-2026-123158 on a host. Applies is
// false off Alpine, where the rest is zero.
type Bypass struct {
	Applies bool
	// NoSSHD is whether no sshd can run here (noSSHD): its binary not
	// installed, or no sshd_config. The advisory then has no daemon to act
	// through; the rest is as read.
	NoSSHD bool
	// PAM is whether sshd uses PAM, as SSH Auth reads it (pam).
	PAM PAM
	// Password is whether a password path is on: PasswordAuthentication
	// yes where AuthenticationMethods lets a password in alone, globally
	// or in a Match block, which counts whatever it matches (matchAllows,
	// byMethods); Unknown where sshd -T or a block's value is not read.
	Password detect.Tri
	// Fix is the package's fix state; Fixed Unknown with Why where it is
	// not established, which sshd runs among them.
	Fix alpine.Fix
}

// ReadBypass reads sshd as SSH Auth does, and InstalledDB for the package
// of the sshd OpenRC's script runs (opensshPackage), for the User
// Management screen's Create (pkg/engine/controls/users, which imports
// this package; never the other way). A read of sshd that fails is its
// error, with PAM's Why the SSH rows' detail for it, Password Unknown and
// NoSSHD where the error establishes no sshd; Fix is read wherever which
// sshd runs is.
func ReadBypass(ctx context.Context, sys host.System, f detect.Facts) (Bypass, error) {
	if f.Platform.Distro != detect.DistroAlpine {
		return Bypass{}, nil
	}
	b := Bypass{Applies: true, Password: detect.Unknown}
	r, err := read(ctx, sys, f)
	b.Fix = r.fix(ctx, sys, f)
	if err == nil {
		b.Password = r.passwordPath()
		if b.PAM, err = r.pam(ctx, sys, f); err == nil {
			return b, nil
		}
	}
	st, err := failed(f.SSH, err)
	b.PAM, b.NoSSHD = PAM{Why: st.Detail}, noSSHD(err)
	return b, err
}

// opensshPackage is the package of the sshd read ran: on OpenRC the
// binary its script runs (reload.OpenRCFlavour), elsewhere sshd on the
// path, /usr/sbin/sshd, openssh-server's.
func (r reading) opensshPackage() string { return ServerPackage(r.bin) }

// ServerPackage is the openssh-server package whose sshd bin is: the -pam
// or -krb5 build for their binaries (reload.BinaryPAM, reload.BinaryKrb5),
// openssh-server's for any other.
func ServerPackage(bin string) string {
	switch bin {
	case reload.BinaryPAM:
		return alpine.OpenSSHServerPAM
	case reload.BinaryKrb5:
		return alpine.OpenSSHServerKrb5
	}
	return alpine.OpenSSHServer
}

// fix is the package's fix state (alpine.OpenSSHFix), Unknown where OpenRC
// starts sshd and which binary its script runs was not read.
func (r reading) fix(ctx context.Context, sys host.System, f detect.Facts) alpine.Fix {
	if f.Init.Kind == detect.OpenRC && r.bin == "" {
		return alpine.Fix{Wanted: alpine.FixedVersion(f.OS.VersionID), Why: "can't tell which sshd runs"}
	}
	return alpine.OpenSSHFix(ctx, sys, r.opensshPackage(), f.OS.VersionID)
}

// passwordPath is Bypass.Password from this reading.
func (r reading) passwordPath() detect.Tri {
	pw, kbd := r.eff.PasswordAuthentication, r.eff.KbdInteractiveAuthentication
	lists := r.methods()
	kbdOn := !kbd.Known || kbd.Value == "yes" // errs toward a path, as passwordAlone does
	if pw.Known && pw.Value == "yes" && passwordAlone(lists, kbdOn) {
		return detect.Yes
	}
	more, odd := r.matchAllows("passwordauthentication", "no")
	if more, _ = r.byMethods(lists, pw.Value == "yes", kbdOn, more, nil); len(more) > 0 {
		return detect.Yes
	}
	if !pw.Known || len(odd) > 0 {
		return detect.Unknown
	}
	return detect.No
}

// withBypass is SSH Auth's status st, with a password path on, and the
// CVE's findings added on Alpine where sshd's PAM (p, read here where nil)
// is established No: the row's state and detail stay as they are. PAM not
// established, or its read failing, adds nothing: the row says what it
// read.
func (r reading) withBypass(ctx context.Context, sys host.System, f detect.Facts, st engine.Status, p *PAM) engine.Status {
	if f.Platform.Distro != detect.DistroAlpine {
		return st
	}
	read := p == nil
	if read {
		q, err := r.pam(ctx, sys, f)
		if err != nil {
			return st
		}
		p = &q
	}
	if p.Uses != detect.No {
		return st
	}
	found, ev := r.bypass(ctx, sys, f)
	if len(found) == 0 {
		return st
	}
	if read {
		st.Evidence = append(st.Evidence, p.Evidence...)
	}
	st.Evidence, st.Findings = append(st.Evidence, ev...), append(st.Findings, found...)
	return st
}

// cveCodes are the CVE's findings.
var cveCodes = []string{CodePasswordBypass, CodeSSHDUnfixed, CodeSSHDUnfixedLatent, CodeFixUnknown}

// withUnfixed is SSH Auth's status st with CodeSSHDUnfixedLatent added on
// Alpine where the package of the sshd that runs is established to lack
// the fix and st carries none of the CVE's findings (withBypass adds
// those where a password path reaches the flaw): the upgrade SSH Login
// and Run All offer then has its reason on the row (the maintainer,
// 2026-11-08: a line, no cost). Its state and detail stay as they are; a
// fix not established adds nothing.
func (r reading) withUnfixed(ctx context.Context, sys host.System, f detect.Facts, st engine.Status) engine.Status {
	if f.Platform.Distro != detect.DistroAlpine || slices.ContainsFunc(st.Findings, func(x engine.Finding) bool { return slices.Contains(cveCodes, x.Code) }) {
		return st
	}
	fx := r.fix(ctx, sys, f)
	if fx.Fixed != detect.No {
		return st
	}
	st.Evidence = append(st.Evidence, fixEvidence(fx))
	st.Findings = append(st.Findings, engine.Finding{Code: CodeSSHDUnfixedLatent, Detail: lacksFix(fx), Source: alpine.InstalledDB})
	return st
}

// lacksFix is the words of an unfixed package's finding.
func lacksFix(fx alpine.Fix) string {
	return fx.Package + " " + fx.Installed + " lacks the fix for " + CVE + " (" + fx.Wanted + ")"
}

// fixEvidence is an unfixed package's evidence line.
func fixEvidence(fx alpine.Fix) string {
	return alpine.InstalledDB + ": " + fx.Package + " " + fx.Installed + ", fixed in " + fx.Wanted
}

// bypass is the CVE's findings, and their evidence, on a host where sshd
// checks passwords without PAM with a password path on: none where the
// package has the fix; CodeFixUnknown where that is not established;
// CodePasswordBypass where a target and a donor exist (targets); else
// CodeSSHDUnfixed, with CodeFixUnknown too where the accounts are not
// established.
func (r reading) bypass(ctx context.Context, sys host.System, f detect.Facts) ([]engine.Finding, []string) {
	fx := r.fix(ctx, sys, f)
	switch fx.Fixed {
	case detect.Yes:
		return nil, nil
	case detect.Unknown:
		pkg := fx.Package
		if pkg == "" {
			pkg = "openssh"
		}
		return []engine.Finding{{Code: CodeFixUnknown, Detail: "can't tell if " + pkg + " has the fix for " + CVE + ": " + fx.Why, Source: alpine.InstalledDB}},
			[]string{alpine.InstalledDB + ": " + fx.Why}
	}
	ev := []string{fixEvidence(fx)}
	unfixed := engine.Finding{Code: CodeSSHDUnfixed, Detail: lacksFix(fx), Source: alpine.InstalledDB}
	t := r.targets(ctx, sys)
	ev = append(ev, t.ev...)
	switch {
	case t.donor && len(t.names) > 0:
		return []engine.Finding{{Code: CodePasswordBypass, Detail: "anyone can log in as " + strings.Join(t.names, ", ") + " with a password of 257 to 1024 bytes (" + CVE + ")", Source: alpine.InstalledDB}}, ev
	case t.open != "" && (t.donor || !t.read):
		return []engine.Finding{unfixed, {Code: CodeFixUnknown, Detail: "can't tell who can log in with a long password (" + CVE + "): " + t.open}}, ev
	}
	return []engine.Finding{unfixed}, ev
}

// exposure is what targets read of the accounts.
type exposure struct {
	names []string // the targets: established
	donor bool     // an account's field is a $5$ or $6$ hash
	read  bool     // PasswdFile and ShadowFile were read
	open  string   // why an account, or the files, are not established; "" for none
	ev    []string
}

// targets reads the accounts sshd would let in with a long password:
// each name's first PasswdFile line (getpwnam's [I]) whose field, as sshd
// reads it (accounts.Shadow.ShapeOf), is exactly "*", with a shell sshd
// lets in (accounts.SSHDShell: nologin and false too, which leave
// forwarding to the account once it authenticates), and with uid 0 only
// where PermitRootLogin yes is in effect (rootPasswords); and whether a
// donor exists: any PasswdFile
// line whose field is a $5$ or $6$ hash (pick_salt's getpwent walk, by
// Alpine's rule of any such account).
func (r reading) targets(ctx context.Context, sys host.System) exposure {
	var x exposure
	us, err := accounts.ReadPasswd(ctx, sys)
	if err != nil {
		x.open = words(err)
		return x
	}
	sh, err := accounts.ReadShadow(ctx, sys)
	if err != nil {
		x.open = words(err)
		return x
	}
	x.read = true
	root := r.rootPasswords()
	seen := map[string]bool{}
	for _, u := range us {
		shape := sh.ShapeOf(u)
		_, at := sh.LockOf(u)
		if shape == accounts.ShapeSHACrypt && !x.donor {
			x.donor = true
			x.ev = append(x.ev, at+": "+u.Name+" "+shape.String())
		}
		first := !seen[u.Name]
		seen[u.Name] = true
		if !first || shape != accounts.ShapeStar {
			continue
		}
		login, err := accounts.SSHDShell(ctx, sys, u.Shell)
		switch {
		case login == detect.No:
			continue
		case login == detect.Unknown && x.open == "":
			x.open = u.Name + "'s shell: " + words(err)
			continue
		case login == detect.Unknown:
			continue
		case u.UID == 0 && root == detect.No:
			continue
		case u.UID == 0 && root == detect.Unknown:
			if x.open == "" {
				x.open = "PermitRootLogin not read"
			}
			continue
		}
		x.names = append(x.names, u.Name)
		x.ev = append(x.ev, at+": "+u.Name+" "+shape.String())
	}
	return x
}

// rootPasswords is whether PermitRootLogin yes is in effect, which an
// account with uid 0 needs for a password (auth_password): globally, or in
// a Match block, which counts whatever it matches; Unknown where sshd -T's
// value or a block's is not read.
func (r reading) rootPasswords() detect.Tri {
	g := r.eff.PermitRootLogin
	if !g.Known {
		return detect.Unknown
	}
	if g.Value == "yes" {
		return detect.Yes
	}
	more, odd := r.matchAllows("permitrootlogin", g.Value)
	if slices.ContainsFunc(more, func(l sshd.Line) bool { return len(l.Args) > 0 && strings.EqualFold(l.Args[0], "yes") }) {
		return detect.Yes
	}
	if len(odd) > 0 {
		return detect.Unknown
	}
	return detect.No
}
