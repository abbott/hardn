// Package authkeys reads a user's SSH keys in the files sshd reads for
// them (sshd -T's AuthorizedKeysFile, sshd.Effective) and plans adding and
// removing one in ~/.ssh/authorized_keys, the one file hardn writes
// (refactor plan slice 5, unit (b)). It reads only, through host.System;
// the write is the users control's.
//
// A key is parsed as sshd(8)'s AUTHORIZED_KEYS FILE FORMAT gives it, with
// no x/crypto: the type one OpenSSH 9 and 10 take, the base64 blob's first
// string the same type. The blob's key material is not checked.
//
// docs/safety/authkeys.md describes what it reads and plans, and what is
// written but not yet run on a Linux host.
package authkeys

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
)

// Planned is the one file hardn writes keys in, relative to the home.
const Planned = ".ssh/authorized_keys"

var (
	// ErrNotKey: a line or a pasted text that is not one OpenSSH public key
	// of a type OpenSSH takes; blank lines and # comments are ErrNotKey too.
	ErrNotKey = errors.New("authkeys: not a key")
	// ErrStrictModes: sshd's StrictModes would ignore the user's keys.
	ErrStrictModes = errors.New("sshd's StrictModes would ignore the keys")
	// ErrNotPlanned: the files sshd reads leave out ~/.ssh/authorized_keys,
	// so a key hardn wrote there would not be read.
	ErrNotPlanned = errors.New("authkeys: sshd does not read " + Planned)
)

// kinds are the key types OpenSSH 9 and 10 take in authorized_keys
// (sshd(8), AUTHORIZED_KEYS FILE FORMAT), DSA gone.
var kinds = []string{"ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521",
	"sk-ssh-ed25519@openssh.com", "sk-ecdsa-sha2-nistp256@openssh.com"}

// Key is one public key.
type Key struct {
	Type    string
	Blob    string // base64, as written
	Comment string // the rest of the line after the blob, surrounding space trimmed
	Options string // as written, "" for none
	Line    string // the line, surrounding space and a CR trimmed
	// Fingerprint is as ssh-keygen -l prints it: "SHA256:" and the
	// unpadded standard base64 of the blob's SHA-256.
	Fingerprint string
}

// Same reports whether k and o are the same key: type and blob.
func (k Key) Same(o Key) bool { return k.Type == o.Type && k.Blob == o.Blob }

// ParseLine parses one authorized_keys line: options before the type are
// read past, commas and spaces inside double quotes (a \" there kept)
// among them. Blank and # lines are ErrNotKey.
func ParseLine(line string) (Key, error) {
	l := strings.TrimSpace(line)
	if l == "" || strings.HasPrefix(l, "#") {
		return Key{}, ErrNotKey
	}
	var opts string
	if !slices.Contains(kinds, strings.Fields(l)[0]) {
		end, quoted := len(l), false
		for i := 0; i < len(l); i++ {
			switch c := l[i]; {
			case c == '\\' && quoted && i+1 < len(l) && l[i+1] == '"':
				i++
			case c == '"':
				quoted = !quoted
			case (c == ' ' || c == '\t') && !quoted:
				end = i
			}
			if end < len(l) {
				break
			}
		}
		if quoted || end == len(l) {
			return Key{}, ErrNotKey
		}
		opts, l = l[:end], strings.TrimSpace(l[end:])
	}
	k, err := parse(l)
	if err != nil {
		return Key{}, err
	}
	k.Options, k.Line = opts, strings.TrimSpace(line)
	return k, nil
}

// ParsePublic parses a pasted key: one line with no options, a type
// OpenSSH 9 and 10 take, the blob's first string the same type; surrounding space and a
// CR are trimmed.
func ParsePublic(s string) (Key, error) {
	l := strings.TrimSpace(s)
	if strings.ContainsAny(l, "\r\n") {
		return Key{}, fmt.Errorf("%w: more than one line", ErrNotKey)
	}
	k, err := parse(l)
	if err != nil {
		return Key{}, err
	}
	k.Line = l
	return k, nil
}

// parse is a key with no options: type, blob and comment.
func parse(l string) (Key, error) {
	f := strings.Fields(l)
	if len(f) < 2 || !slices.Contains(kinds, f[0]) {
		return Key{}, ErrNotKey
	}
	blob, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil || len(blob) < 4 {
		return Key{}, fmt.Errorf("%w: its blob is not base64", ErrNotKey)
	}
	if n := binary.BigEndian.Uint32(blob); uint64(n)+4 > uint64(len(blob)) || string(blob[4:4+n]) != f[0] {
		return Key{}, fmt.Errorf("%w: its blob is not a %s key", ErrNotKey, f[0])
	}
	comment := strings.TrimSpace(l[strings.Index(l, f[1])+len(f[1]):])
	sum := sha256.Sum256(blob)
	return Key{Type: f[0], Blob: f[1], Comment: comment, Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])}, nil
}

// Path is AuthorizedKeysFile's entry for u: %h, %u and %% expanded
// (sshd_config(5) TOKENS), relative to the home.
func Path(entry string, u accounts.User) string {
	p := strings.NewReplacer("%%", "%", "%h", u.Home, "%u", u.Name).Replace(entry)
	if !strings.HasPrefix(p, "/") {
		p = path.Join(u.Home, p)
	}
	return p
}

// File is one file sshd reads for a user.
type File struct {
	Path string
	Data *string // nil: not there
	Keys []Key
	Err  error // it could not be read, other than not being there
}

// Read reads the files entries name for u, in order, "none" skipped. A
// file is read as sshd reads it, through a symlink.
func Read(ctx context.Context, sys host.System, entries []string, u accounts.User) []File {
	var out []File
	for _, e := range entries {
		if e == "none" {
			continue
		}
		f := File{Path: Path(e, u)}
		data, err := sys.ReadFile(ctx, f.Path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			f.Err = fmt.Errorf("authkeys: %w", err)
		default:
			s := string(data)
			f.Data = &s
			for _, l := range strings.Split(s, "\n") {
				if k, err := ParseLine(l); err == nil {
					f.Keys = append(f.Keys, k)
				}
			}
		}
		out = append(out, f)
	}
	return out
}

// Keys is the files' keys, in order, each once (Same).
func Keys(files []File) []Key {
	var out []Key
	for _, f := range files {
		for _, k := range f.Keys {
			if !slices.ContainsFunc(out, k.Same) {
				out = append(out, k)
			}
		}
	}
	return out
}

// Plannable reports whether entries hold the file hardn writes, as
// .ssh/authorized_keys or %h/.ssh/authorized_keys.
func Plannable(entries []string) bool {
	return slices.Contains(entries, Planned) || slices.Contains(entries, "%h/"+Planned)
}

// CheckStrict is nil where sshd's StrictModes takes u's keys in ~/.ssh: the
// home u's or root's, ~/.ssh u's or absent, neither writable by group or
// others (sshd_config(5) StrictModes). Both are read with Lstat, so a
// symlink at either refuses. Otherwise Is(ErrStrictModes), naming the path
// and why.
func CheckStrict(ctx context.Context, sys host.System, u accounts.User) error {
	for _, d := range []string{u.Home, path.Join(u.Home, ".ssh")} {
		fi, err := sys.Lstat(ctx, d)
		if errors.Is(err, fs.ErrNotExist) && d != u.Home {
			continue
		}
		var why string
		var uid int
		ok := false
		if err == nil {
			uid, _, ok = host.Owner(fi)
		}
		switch {
		case errors.Is(err, fs.ErrNotExist):
			why = d + " isn't there"
		case err != nil || !ok:
			why = d + " can't be read"
		case fi.Mode()&fs.ModeSymlink != 0:
			why = d + " is a symlink"
		case !fi.IsDir():
			why = d + " isn't a directory"
		case uid != u.UID && (uid != 0 || d != u.Home):
			why = fmt.Sprintf("%s is uid %d's, not %s's", d, uid, u.Name)
		case fi.Mode().Perm()&0o020 != 0:
			why = fmt.Sprintf("%s is writable by its group (%04o)", d, fi.Mode().Perm())
		case fi.Mode().Perm()&0o002 != 0:
			why = fmt.Sprintf("%s is writable by anyone (%04o)", d, fi.Mode().Perm())
		default:
			continue
		}
		return fmt.Errorf("%s: %w", why, ErrStrictModes)
	}
	return nil
}

// Plan is one change to ~/.ssh/authorized_keys.
type Plan struct {
	Path   string
	Before *string // nil: not there
	After  string
	// MakeDir is the install argv that makes ~/.ssh first, 0700 and u's,
	// with numeric ids; nil where it is there.
	MakeDir []string
}

// Empty reports whether the plan changes nothing.
func (p Plan) Empty() bool { return p.Path == "" }

// PlanAdd plans k.Line appended to u's ~/.ssh/authorized_keys, every other
// line kept byte for byte and a missing final newline added. k already in
// a file sshd reads is an empty Plan. Errors: ErrNotPlanned, a file that
// can't be read, and the host's for ~/.ssh.
func PlanAdd(ctx context.Context, sys host.System, u accounts.User, entries []string, k Key) (Plan, error) {
	files, planned, err := read(ctx, sys, u, entries)
	if err != nil {
		return Plan{}, err
	}
	if slices.ContainsFunc(Keys(files), k.Same) {
		return Plan{}, nil
	}
	p := Plan{Path: planned.Path, Before: planned.Data}
	if b := planned.Data; b != nil {
		p.After = *b
		if p.After != "" && !strings.HasSuffix(p.After, "\n") {
			p.After += "\n"
		}
	}
	p.After += k.Line + "\n"
	dir := path.Dir(p.Path)
	switch _, err := sys.Lstat(ctx, dir); {
	case errors.Is(err, fs.ErrNotExist):
		p.MakeDir = []string{"install", "-d", "-m", "700", "-o", strconv.Itoa(u.UID), "-g", strconv.Itoa(u.GID), dir}
	case err != nil:
		return Plan{}, fmt.Errorf("authkeys: %w", err)
	}
	return p, nil
}

// PlanRemove plans every line of u's ~/.ssh/authorized_keys holding the
// key with fingerprint dropped, the rest kept byte for byte. Errors:
// ErrNotPlanned; the key also in another file sshd reads, which removing
// it here would leave working (fs.ErrExist, naming that file); not found,
// fs.ErrNotExist; a file that can't be read.
func PlanRemove(ctx context.Context, sys host.System, u accounts.User, entries []string, fingerprint string) (Plan, error) {
	files, planned, err := read(ctx, sys, u, entries)
	if err != nil {
		return Plan{}, err
	}
	for _, f := range files {
		if f.Path != planned.Path && slices.ContainsFunc(f.Keys, func(k Key) bool { return k.Fingerprint == fingerprint }) {
			return Plan{}, fmt.Errorf("authkeys: %s is also in %s, which sshd reads: %w", fingerprint, f.Path, fs.ErrExist)
		}
	}
	if planned.Data == nil {
		return Plan{}, fmt.Errorf("authkeys: %s is not in %s: %w", fingerprint, planned.Path, fs.ErrNotExist)
	}
	var kept strings.Builder
	dropped := false
	for _, l := range strings.SplitAfter(*planned.Data, "\n") {
		if k, err := ParseLine(l); err == nil && k.Fingerprint == fingerprint {
			dropped = true
			continue
		}
		kept.WriteString(l)
	}
	if !dropped {
		return Plan{}, fmt.Errorf("authkeys: %s is not in %s: %w", fingerprint, planned.Path, fs.ErrNotExist)
	}
	return Plan{Path: planned.Path, Before: planned.Data, After: kept.String()}, nil
}

// read is Read's files for u and the planned one among them, or an error:
// ErrNotPlanned, or the first file that can't be read.
func read(ctx context.Context, sys host.System, u accounts.User, entries []string) ([]File, File, error) {
	if !Plannable(entries) {
		return nil, File{}, fmt.Errorf("%w (AuthorizedKeysFile %s)", ErrNotPlanned, strings.Join(entries, " "))
	}
	files := Read(ctx, sys, entries, u)
	want := Path(Planned, u)
	var planned File
	for _, f := range files {
		if f.Err != nil {
			return nil, File{}, f.Err
		}
		if f.Path == want {
			planned = f
		}
	}
	return files, planned, nil
}
