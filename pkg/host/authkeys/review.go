package authkeys

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"syscall"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
)

// What the SSH Keys screen reads (docs/engine/screen-keys.md): every
// account's files, and in each every key, those Read leaves out as no way to
// log in among them. It reads only.

// DSA is DSA's key type, which kinds leaves out: ParseLine refuses it, and
// Review lists it.
const DSA = "ssh-dss"

// errNoHome (unexported: no command returns it, the screen shows the file
// as not read): an AuthorizedKeysFile entry does not expand to an absolute path
// for the account, whose home passwd leaves empty or relative, so the file is
// not read.
var errNoHome = errors.New("authkeys: the account's home is not an absolute path")

// reviewKinds are kinds and DSA.
var reviewKinds = append(slices.Clone(kinds), DSA)

// Review is f's lines for a review of its keys: every key in the file's
// order, each once (Same), with the two kinds Read leaves out, a DSA key and
// an RSA key under MinRSABits; and bad, how many lines are no key, blank line
// or # comment. A file not there, or not read, has neither.
func Review(f File) (keys []Key, bad int) {
	if f.Data == nil {
		return nil, 0
	}
	for _, l := range strings.Split(*f.Data, "\n") {
		k, err := parseLine(l, reviewKinds, 1)
		switch t := strings.TrimSpace(l); {
		case err != nil && t != "" && !strings.HasPrefix(t, "#"):
			bad++
		case err == nil && !slices.ContainsFunc(keys, k.Same):
			keys = append(keys, k)
		}
	}
	return keys, bad
}

// Account is one account and the files sshd reads for it.
type Account struct {
	User  accounts.User
	Files []File
}

// MaxFile is the most of one file ReadAccounts reads, in bytes: 1 MiB.
const MaxFile = 1 << 20

// maxLinks is how many symlinks readGuarded follows from one path.
const maxLinks = 8

// errTooBig (unexported, as errNoHome is): a file past MaxFile, or with a
// line past host.MaxLine, is not read.
var errTooBig = errors.New("authkeys: the file is too big to read")

// readGuarded is Read's File for p, read so that no account's file can hold
// hardn or fill its memory: only a regular file is opened, without blocking
// (host.System's ScanLines), so a FIFO or a device there is not read, and no
// more than MaxFile of it is kept. A symlink is followed only where
// ResolveSymlink follows it, from a directory that is root's and that no one
// else writes (Proxmox VE's /root/.ssh/authorized_keys). So it reads less
// than sshd does: a link in a directory the account owns is not read, and
// the File has the error.
func readGuarded(ctx context.Context, sys host.System, p string) File {
	f := File{Path: p}
	var b strings.Builder
	var err error
	for at, links := p, 0; ; links++ {
		big := false
		b.Reset()
		_, err = sys.ScanLines(ctx, at, func(l host.Line) bool {
			if big = l.Cut || b.Len()+len(l.Text)+1 > MaxFile; big {
				return false
			}
			b.Write(l.Text)
			b.WriteByte('\n')
			return true
		})
		if err == nil && big {
			err = errTooBig
		}
		if !errors.Is(err, host.ErrSymlink) || links == maxLinks {
			break
		}
		var rerr error
		if _, at, rerr = sys.ResolveSymlink(ctx, at); rerr != nil {
			err = rerr
			break
		}
	}
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		// No file. ENOTDIR: a home that is no directory, as /dev/null is
		// for Alpine's system accounts, holds no key file either.
	case err != nil:
		f.Err = fmt.Errorf("authkeys: %w", err)
	default:
		s := b.String()
		f.Data = &s
		for _, l := range strings.Split(s, "\n") {
			if k, err := ParseLine(l); err == nil {
				f.Keys = append(f.Keys, k)
			}
		}
	}
	return f
}

// ReadAccounts reads, for every account of /etc/passwd (the first line of
// each name, in the file's order), the files entries name, each as
// readGuarded reads it: Read's File, but a file that is not a regular one, is
// past MaxFile, or is behind a symlink in a directory someone other than
// root writes, is not read and has an error. An entry that is no absolute
// path for an account is not read: its File has errNoHome. An error where /etc/passwd is not read or not in its form, and
// the context's. Accounts only NSS knows are not listed.
func ReadAccounts(ctx context.Context, sys host.System, entries []string) ([]Account, error) {
	users, err := accounts.ReadPasswd(ctx, sys)
	if err != nil {
		return nil, err
	}
	var out []Account
	seen := map[string]bool{}
	for _, u := range users {
		if seen[u.Name] {
			continue
		}
		seen[u.Name] = true
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		a := Account{User: u}
		for _, e := range entries {
			switch p := Path(e, u); {
			case e == "none":
			case !path.IsAbs(p):
				a.Files = append(a.Files, File{Path: p, Err: errNoHome})
			default:
				a.Files = append(a.Files, readGuarded(ctx, sys, p))
			}
		}
		out = append(out, a)
	}
	return out, nil
}
