package sshchange

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// Admin is one admin and whether sshd lets them in from the session's
// address after the change (LoginPaths).
type Admin struct {
	Name string
	// Key: by key alone. Password: not by key alone, but with a password,
	// where passwords stay on.
	Key, Password bool
	// Why is why not by key, in a sentence's middle (gallery-ssh.md §6's
	// whys); "" where Key.
	Why string
}

// consoleAddr is the address sshd is asked about from a console, where the
// session has none [I]: the unspecified IPv4 address, which a Match
// Address block or an AllowUsers HOST matches only where it matches every
// address, so an admin let in from some places alone is not shown able.
const consoleAddr = "0.0.0.0"

// addr is the session's client address, consoleAddr where there is none.
func (r Reading) addr() string {
	if c := strings.Fields(r.Session.Conn); len(c) == 4 {
		return c[0]
	}
	return consoleAddr
}

// LoginPaths are the admins besides root (users.Read's, uid not 0), each
// with whether they could log in from the session's address once passwords
// are off (keyOnly) or as they are (loginPath), and then become root: a
// grant that asks no password, or one the admin has a password for. An
// admin who cannot do both is not shown able.
func (r Reading) LoginPaths(ctx context.Context, sys host.System, keyOnly bool) []Admin {
	var out []Admin
	for _, u := range r.Users.Users {
		if u.Admin != detect.Yes || u.UID == 0 {
			continue
		}
		a := r.loginPath(ctx, sys, u.User, u.Password, u.Locked, keyOnly)
		if (a.Key || a.Password) && !u.NoPassword && u.Password != accounts.PasswordSet {
			tool := u.Tool
			if tool == "" {
				tool = "sudo"
			}
			a.Key, a.Password = false, false
			a.Why = u.Name + " can't become root: " + tool + " asks a password " + u.Name + " hasn't got"
		}
		out = append(out, a)
	}
	return out
}

// RootPath is root's own path on Proxmox VE, where root stays key-only:
// whether root could log in by key from the session's address
// (loginPath, keyOnly).
func (r Reading) RootPath(ctx context.Context, sys host.System) Admin {
	return r.loginPath(ctx, sys, r.Root, r.RootPassword, r.RootLocked, true)
}

// loginPath is whether sshd lets u in from the session's address by key
// alone, or else with a password where passwords stay on (not keyOnly).
// It reads sshd's own answer for u (sshd.ReadUser: Match blocks applied by
// sshd), then matches DenyUsers and AllowUsers itself against the address
// (sshd.Listed; from a console every DenyUsers pattern that names u counts,
// whatever its HOST); AllowGroups and DenyGroups are not read, so a user
// under either is not shown able. The methods: publickey is a key in the
// files AuthorizedKeysFile names (authkeys.Read, a restricted key not
// counted) whose type PubkeyAcceptedAlgorithms accepts
// (authkeys.TypeAccepted) and, for RSA, whose modulus is at least
// RequiredRSASize bits (authkeys.MinRSABits where not printed), StrictModes
// passed where it is on (authkeys.CheckStrict), PubkeyAuthentication yes;
// password is PasswordAuthentication yes and a password set;
// keyboard-interactive is KbdInteractiveAuthentication yes, PAM in use and
// a password set. One of AuthenticationMethods' lists must be made of
// those alone (sshd.MethodsMet). The account's lock counts where sshd has
// no PAM (locked).
func (r Reading) loginPath(ctx context.Context, sys host.System, u accounts.User, pw accounts.Password, locked, keyOnly bool) Admin {
	a := Admin{Name: u.Name}
	addr := r.addr()
	v, err := sshd.ReadUser(ctx, sys, r.Binary, u.Name, addr)
	if err != nil {
		a.Why = "sshd's answer for " + u.Name + " not read"
		return a
	}
	deny := v.DenyUsers
	if r.Console() {
		deny = nil
		for _, p := range v.DenyUsers {
			if i := strings.LastIndexByte(p, '@'); i >= 0 {
				p = p[:i]
			}
			deny = append(deny, p)
		}
	}
	if ok, _, _ := sshd.Listed(deny, u.Name, addr); ok {
		a.Why = u.Name + " is in DenyUsers"
		return a
	}
	if len(v.AllowUsers) > 0 {
		if ok, host, named := sshd.Listed(v.AllowUsers, u.Name, addr); !ok {
			switch {
			case named:
				a.Why = "AllowUsers names " + u.Name + "@" + host + ", a host name hardn doesn't match"
			case host != "":
				a.Why = "AllowUsers lets " + u.Name + " in from " + host + " alone"
			default:
				a.Why = u.Name + " isn't in AllowUsers"
			}
			return a
		}
	}
	switch {
	case len(v.AllowGroups) > 0 || len(v.DenyGroups) > 0:
		a.Why = "AllowGroups and DenyGroups aren't read"
		return a
	case locked:
		a.Why = u.Name + "'s account is locked (!) and sshd has no PAM"
		return a
	}
	keys := slices.DeleteFunc(authkeys.Keys(authkeys.Read(ctx, sys, v.AuthorizedKeysFile, u)), func(k authkeys.Key) bool { return k.Restricted() })
	minRSA := v.RequiredRSASize
	if minRSA == 0 {
		minRSA = authkeys.MinRSABits
	}
	var typed, sized []authkeys.Key
	for _, k := range keys {
		if !authkeys.TypeAccepted(k.Type, v.PubkeyAcceptedAlgorithms) {
			continue
		}
		typed = append(typed, k)
		if k.Type != "ssh-rsa" || k.RSABits() >= minRSA {
			sized = append(sized, k)
		}
	}
	var keyWhy string
	switch {
	case v.PubkeyAuthentication != "yes":
		keyWhy = "PubkeyAuthentication is off"
	case len(keys) == 0:
		keyWhy = u.Name + " has no key sshd reads"
	case len(typed) == 0:
		var ts []string
		for _, k := range keys {
			ts = append(ts, k.Type)
		}
		keyWhy = "sshd doesn't accept " + u.Name + "'s key type (" + strings.Join(slices.Compact(ts), ", ") + ")"
	case len(sized) == 0:
		keyWhy = fmt.Sprintf("%s's RSA key is %d bits, under RequiredRSASize %d", u.Name, typed[0].RSABits(), minRSA)
	case v.StrictModes != "no" && authkeys.CheckStrict(ctx, sys, u) != nil:
		keyWhy = "StrictModes ignores " + u.Name + "'s keys"
	}
	have := map[string]bool{
		"publickey":            keyWhy == "",
		"password":             v.PasswordAuthentication == "yes" && pw == accounts.PasswordSet,
		"keyboard-interactive": v.KbdInteractiveAuthentication == "yes" && v.UsePAM == "yes" && pw == accounts.PasswordSet,
	}
	a.Key = sshd.MethodsMet(v.AuthenticationMethods, map[string]bool{"publickey": have["publickey"]})
	a.Password = !a.Key && !keyOnly && sshd.MethodsMet(v.AuthenticationMethods, have)
	switch {
	case a.Key:
	case keyWhy != "":
		a.Why = keyWhy
	default:
		a.Why = "AuthenticationMethods " + strings.Join(v.AuthenticationMethods, " ") + " asks more than " + u.Name + " has"
	}
	return a
}
