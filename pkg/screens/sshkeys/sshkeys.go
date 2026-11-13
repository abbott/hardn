// Package sshkeys runs hardn's SSH Keys screen (docs/roadmap.md, "SSH key
// review"; next/key-review): SSH Login's item 6, as the maintainer adopted the
// mock on 2026-11-13 (docs/engine/gallery-keys.md §3, every recommended
// answer, with §4's words). It lists every key sshd reads, by account, drawn by
// pkg/ui/screen (screen.SSHKeys), and marks the weak, the shared and those
// sshd refuses. It only shows: no engine, nothing written, nothing run but what
// its caller's Settings runs, under plan only too
// (docs/engine/screen-keys.md).
//
// The package reaches the host only through host.System (depguard rule
// screens-through-host), and renders only through pkg/ui.
package sshkeys

import (
	"context"
	"fmt"
	"io/fs"
	"slices"

	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// WeakRSABits is the weak-key rule's size (gallery-keys.md, decision 2): an
// RSA key under it is weak, and a DSA key whatever its size. No other type is.
const WeakRSABits = 2048

// Settings reads what sshd says of the keys it takes: ssh.ReadKeySettings in
// hardn, or ssh.KeySettingsOf an sshd -T already read. An error is sshd's
// settings not read.
type Settings func(context.Context) (ssh.KeySettings, error)

// Options is what the SSH Keys screen runs with: what the SSH Login screen
// has when its SSH keys item is chosen.
type Options struct {
	// Prompter asks and writes the screen through the one line reader,
	// rendering with its Ctx.
	Prompter nav.Prompter
	// System is the host: the screen reads /etc/passwd and the key files
	// from it, and changes nothing.
	System host.System
	// Settings is read on every draw. Required.
	Settings Settings
}

// Reading is one reading of the host: sshd's settings, and every account with
// the files sshd reads for it. Err is why neither is known: the settings not
// read, or /etc/passwd.
type Reading struct {
	Settings ssh.KeySettings
	Accounts []authkeys.Account
	Err      error
}

// Read reads the host for the screen, changing nothing: settings, then every
// account's files (authkeys.ReadAccounts) as AuthorizedKeysFile names them.
// Match blocks are not read, as ssh.KeySettings says: a file a block names for
// one user is not listed.
func Read(ctx context.Context, sys host.System, settings Settings) Reading {
	s, err := settings(ctx)
	if err != nil {
		return Reading{Err: err}
	}
	as, err := authkeys.ReadAccounts(ctx, sys, s.Files)
	if err != nil {
		return Reading{Err: err}
	}
	return Reading{Settings: s, Accounts: as}
}

// types are a key type's words, ssh-keygen -l's.
var types = map[string]string{
	"ssh-ed25519": "ED25519", "ssh-rsa": "RSA", authkeys.DSA: "DSA",
	"ecdsa-sha2-nistp256": "ECDSA", "ecdsa-sha2-nistp384": "ECDSA", "ecdsa-sha2-nistp521": "ECDSA",
	"sk-ssh-ed25519@openssh.com": "ED25519-SK", "sk-ecdsa-sha2-nistp256@openssh.com": "ECDSA-SK",
}

// bits is the size shown for k: an RSA key's modulus, an ECDSA key's curve; 0
// for a type whose size is not shown.
func bits(k authkeys.Key) int {
	switch k.Type {
	case "ssh-rsa":
		return k.RSABits()
	case "ecdsa-sha2-nistp256":
		return 256
	case "ecdsa-sha2-nistp384":
		return 384
	case "ecdsa-sha2-nistp521":
		return 521
	}
	return 0
}

// View is the screen's view of r, the one mapping from a reading to
// screen.SSHKeys. With r.Err the keys are not read, and none is listed.
//
// An account is listed where it has a key, a file that could not be read, or a
// file with lines that are no key; its keys are every file's in order
// (authkeys.Review), each once. Weak is the adopted rule: DSA, or RSA under
// WeakRSABits. Refused is sshd's: a type its PubkeyAcceptedAlgorithms leave
// out (none where sshd -T prints no such line), or an RSA key under its
// RequiredRSASize, authkeys.MinRSABits where sshd -T prints none. Shared is
// the same type and blob in another account; the comment is not compared. A
// key's options are not shown, and StrictModes is not read. Every account
// name, comment and path is made safe to show (logview.Safe).
func View(r Reading) screen.SSHKeys {
	if r.Err != nil {
		return screen.SSHKeys{NotRead: true}
	}
	rsaMin := r.Settings.RSAMin
	if rsaMin == 0 {
		rsaMin = authkeys.MinRSABits
	}
	keys := make([][]authkeys.Key, len(r.Accounts))
	accounts := make([]screen.SSHKeyAccount, len(r.Accounts))
	for i, a := range r.Accounts {
		accounts[i].Name = logview.Safe(a.User.Name)
		for _, f := range a.Files {
			p := logview.Safe(f.Path)
			if f.Err != nil && !slices.Contains(accounts[i].Unread, p) {
				accounts[i].Unread = append(accounts[i].Unread, p)
			}
			ks, bad := authkeys.Review(f)
			if bad > 0 && !slices.ContainsFunc(accounts[i].NotKeys, func(l screen.SSHKeyLines) bool { return l.Path == p }) {
				accounts[i].NotKeys = append(accounts[i].NotKeys, screen.SSHKeyLines{Path: p, Lines: bad})
			}
			for _, k := range ks {
				if !slices.ContainsFunc(keys[i], k.Same) {
					keys[i] = append(keys[i], k)
				}
			}
		}
	}
	var v screen.SSHKeys
	for i, a := range accounts {
		for _, k := range keys[i] {
			rsa := k.Type == "ssh-rsa"
			row := screen.SSHKey{Type: types[k.Type], Bits: bits(k), Comment: logview.Safe(k.Comment), ID: k.Type + " " + k.Blob,
				Weak:    k.Type == authkeys.DSA || rsa && k.RSABits() < WeakRSABits,
				Refused: !authkeys.TypeAccepted(k.Type, r.Settings.Algorithms) || rsa && k.RSABits() < rsaMin}
			for j := range accounts {
				if j != i && slices.ContainsFunc(keys[j], k.Same) {
					row.Shared = append(row.Shared, accounts[j].Name)
				}
			}
			a.Keys = append(a.Keys, row)
		}
		if len(a.Keys) > 0 || len(a.Unread) > 0 || len(a.NotKeys) > 0 {
			v.Accounts = append(v.Accounts, a)
		}
	}
	return v
}

// Run shows the SSH Keys screen until 0 (nil) or q (nav.ErrQuit); the end of
// the input and Ctrl-C abort (prompt.ErrAborted). Each draw reads the host
// again (Read) and writes nothing, under plan only too, as Options has no
// mode. With more keys than screen.SSHKeysFold it opens folded, and item 1
// lists every key, then folds them again. Run returns an error wrapping
// fs.ErrInvalid, writing nothing, without a Prompter's In and Out, a System or
// Settings.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil || o.Settings == nil {
		return fmt.Errorf("ssh keys screen: %w: it needs a Prompter with In and Out, a System and Settings", fs.ErrInvalid)
	}
	all := false
	return o.Prompter.Run(func() nav.Screen {
		v := View(Read(ctx, o.System, o.Settings))
		v.All = all
		sub := v.SubMenu()
		menu := nav.Menu{Exit: sub.Menu.Exit}
		for _, it := range sub.Menu.Items {
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { all = !all; return nil }})
		}
		return sub.Screen(o.Prompter.Ctx, menu)
	})
}
