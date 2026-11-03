package sshd

import (
	"context"
	"fmt"
	"io/fs"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// UserView is what sshd -T -C prints for one user connecting from one
// address, once sshd has applied the Match blocks: the settings that
// decide whether that user can still log in (refactor plan slice 6, unit
// (b); gallery-ssh.md §5 decision 2). Values are as sshd -T prints them,
// but for PermitRootLogin's without-password (Debian 13 prints it), read as
// prohibit-password, one value (servconf.c multistate_permitrootlogin). A
// keyword sshd does not print is "", nil or 0: UsePAM where the build has
// no PAM (Alpine 3.23), RequiredRSASize before OpenSSH 9.1 [I].
type UserView struct {
	User, Addr string
	// AllowUsers, DenyUsers, AllowGroups, DenyGroups: one pattern per
	// entry as written, USER@HOST kept; sshd -T prints one per line.
	AllowUsers, DenyUsers, AllowGroups, DenyGroups []string
	// yes or no; PermitRootLogin yes, prohibit-password,
	// forced-commands-only or no.
	PermitRootLogin, PubkeyAuthentication, PasswordAuthentication, KbdInteractiveAuthentication, UsePAM, StrictModes string
	// AuthenticationMethods: its lists, each a comma chain; ["any"] as
	// printed for the default.
	AuthenticationMethods []string
	// AuthorizedKeysFile: its entries as printed, tokens unexpanded.
	AuthorizedKeysFile []string
	// PubkeyAcceptedAlgorithms: the algorithms, the comma list split.
	PubkeyAcceptedAlgorithms []string
	RequiredRSASize          int
}

// ReadUser runs <bin> -T -f MainConfig -C user=<user>,host=<addr>,addr=<addr>
// (bin "" is DefaultBinary) and reads UserView from it. The client's name
// is not resolved: host is the address, as sshd has it with UseDNS no, its
// default. A user with a comma, white space or "=", or an addr netip does
// not parse, is fs.ErrInvalid before anything runs. A failed run is
// Validate's errors (*ConfigError, *EnvError, *UnexpectedError, the
// host's); a setting printed with a value hardn does not read, or a
// RequiredRSASize not a number, is an error naming it. It needs root as
// sshd -T does. It writes nothing.
func ReadUser(ctx context.Context, sys host.System, bin, user, addr string) (UserView, error) {
	if user == "" || strings.ContainsAny(user, ", \t\r\n=") {
		return UserView{}, fmt.Errorf("sshd: user %q: %w", user, fs.ErrInvalid)
	}
	if _, err := netip.ParseAddr(addr); err != nil {
		return UserView{}, fmt.Errorf("sshd: address %q: %w", addr, fs.ErrInvalid)
	}
	if bin == "" {
		bin = DefaultBinary
	}
	c := Conn{User: user, Host: addr, Addr: addr}
	res, err := sys.Run(ctx, bin, "-T", "-f", MainConfig, "-C", c.Spec())
	if err = classify(res, err, strings.NewReplacer(), false); err != nil {
		return UserView{}, err
	}
	kw, err := parseT(res.Stdout)
	if err != nil {
		return UserView{}, fmt.Errorf("sshd: %w", err)
	}
	v := UserView{User: user, Addr: addr,
		AllowUsers: kw["allowusers"], DenyUsers: kw["denyusers"], AllowGroups: kw["allowgroups"], DenyGroups: kw["denygroups"]}
	for k, dst := range map[string]*string{
		"permitrootlogin": &v.PermitRootLogin, "pubkeyauthentication": &v.PubkeyAuthentication,
		"passwordauthentication": &v.PasswordAuthentication, "kbdinteractiveauthentication": &v.KbdInteractiveAuthentication,
		"usepam": &v.UsePAM, "strictmodes": &v.StrictModes,
	} {
		vs := kw[k]
		if len(vs) == 0 {
			continue
		}
		if *dst = normalize(k, vs[0]); !slices.Contains(settingValues[k], *dst) {
			return UserView{}, fmt.Errorf("sshd: sshd -T printed %s %q, a value hardn does not know", k, vs[0])
		}
	}
	first := func(k string) string {
		if vs := kw[k]; len(vs) > 0 {
			return vs[0]
		}
		return ""
	}
	v.AuthenticationMethods = strings.Fields(first("authenticationmethods"))
	v.AuthorizedKeysFile = strings.Fields(first("authorizedkeysfile"))
	if a := first("pubkeyacceptedalgorithms"); a != "" {
		v.PubkeyAcceptedAlgorithms = strings.Split(a, ",")
	}
	if s := first("requiredrsasize"); s != "" {
		if v.RequiredRSASize, err = strconv.Atoi(s); err != nil || v.RequiredRSASize < 0 {
			return UserView{}, fmt.Errorf("sshd: sshd -T printed requiredrsasize %q, not a number", s)
		}
	}
	return v, nil
}

// Listed matches user, connecting from addr, against AllowUsers' or
// DenyUsers' patterns (sshd_config(5)): ok is true when one names them.
// A pattern is USER or USER@HOST, split at the last "@" [I]. USER is
// matched as ssh_config(5) PATTERNS gives: "*" any run, "?" one character;
// a leading "!" negates it, and as a negated match never yields a positive
// one by itself, it names no one [I: sshd's match_user not read; a "!" read
// literally names no one either]. HOST is a pattern-list (commas, each
// entry negatable): an address pattern or a CIDR (netip) matched against
// addr, a negated entry that matches refusing the pattern. A host name is
// never matched, as the client's name is not read (sshd's UseDNS no, its
// default, compares HOST with the address). When no pattern names them,
// host is the HOST of the first that names the user from elsewhere, and
// named is true where that HOST has a host name in it.
func Listed(patterns []string, user, addr string) (ok bool, host string, named bool) {
	a, aerr := netip.ParseAddr(addr)
	for _, p := range strings.Fields(strings.Join(patterns, " ")) {
		up, hp := p, ""
		at := strings.LastIndexByte(p, '@')
		if at >= 0 {
			up, hp = p[:at], p[at+1:]
		}
		if strings.HasPrefix(up, "!") || !wildcard(user, up) {
			continue
		}
		if at < 0 {
			return true, "", false
		}
		in, hasName := false, false
		for _, e := range strings.Split(hp, ",") {
			e, neg := strings.CutPrefix(e, "!")
			var m bool
			switch pr, err := netip.ParsePrefix(e); {
			case err == nil:
				m = aerr == nil && pr.Contains(a)
			case addrPattern(e):
				m = wildcard(addr, e)
			default:
				hasName = true
			}
			if m && neg {
				in = false
				break
			}
			in = in || m
		}
		if in {
			return true, "", false
		}
		if host == "" {
			host, named = hp, hasName
		}
	}
	return false, host, named
}

// addrPattern reports whether e can only match an address: digits, ".",
// "*" and "?" (IPv4), or with a ":" hex digits too (IPv6). A host name has
// a letter outside those, or no ":".
func addrPattern(e string) bool {
	if strings.Trim(e, "0123456789.*?") == "" {
		return true
	}
	return strings.Contains(e, ":") && strings.Trim(strings.ToLower(e), "0123456789abcdef.:*?") == ""
}

// wildcard is ssh_config(5)'s pattern match: "*" any run of characters, "?"
// exactly one, any other character itself.
func wildcard(s, p string) bool {
	for len(p) > 0 {
		switch p[0] {
		case '*':
			for i := len(s); i >= 0; i-- {
				if wildcard(s[i:], p[1:]) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 {
				return false
			}
		default:
			if len(s) == 0 || s[0] != p[0] {
				return false
			}
		}
		s, p = s[1:], p[1:]
	}
	return len(s) == 0
}

// MethodsMet reports whether AuthenticationMethods' lists, as ReadUser
// gives them, let in a user holding the methods have marks true: one list
// (a comma chain, each method's ":submethod" ignored) made only of them.
// "any", or no list, is met by any one method.
func MethodsMet(lists []string, have map[string]bool) bool {
	if len(lists) == 0 {
		lists = []string{"any"}
	}
	for _, l := range lists {
		if l == "any" {
			for _, ok := range have {
				if ok {
					return true
				}
			}
			continue
		}
		all := true
		for _, m := range strings.Split(l, ",") {
			m, _, _ = strings.Cut(m, ":")
			all = all && have[m]
		}
		if all {
			return true
		}
	}
	return false
}
