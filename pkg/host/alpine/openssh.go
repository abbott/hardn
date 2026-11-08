package alpine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// CVE-2026-123158 (CVSS 8.1), published 2026-11-05 with OpenSSH 10.6p2 and
// Alpine's post (https://alpinelinux.org/posts/2026-11-05-openssh-password-authentication-bypass/):
// on musl without PAM, sshd's sys_auth_passwd borrows another account's
// salt when the stored field is shorter than 2 characters, and musl's
// SHA-256/512 crypt returns "*" for a key over 256 bytes, so a password of
// 257 to 1024 bytes matches a stored field of exactly "*". Alpine
// backported the fix with a pkgrel bump, so sshd -V still prints the old
// upstream version: only apk's package version tells. This file reads that
// version; pkg/engine/controls/ssh decides what it means for a host
// (docs/safety/alpine.md §7).

// InstalledDB is apk's installed database: one record per installed
// package, records apart by a blank line, each line "<letter>:<value>"
// ("P:" the name, "V:" the version), as apk-tools v2 and v3 both write it
// (v3 read on the Alpine 3.23 VM, apk-tools 3.0.8, 2026-11-07).
const InstalledDB = "/lib/apk/db/installed"

// ParseInstalled parses InstalledDB's content into each package's name and
// version, and nothing more. A record with a name and no version, or a
// version and no name, or a name twice, is an error naming the line: which
// version is installed is then not established.
func ParseInstalled(data []byte) (map[string]string, error) {
	out := map[string]string{}
	var name, version string
	start := 0 // the record's first line, from 1; 0: no record open
	end := func() error {
		switch {
		case start == 0:
		case name == "" || version == "":
			return fmt.Errorf("alpine: %s: the record at line %d has no P: or no V:", InstalledDB, start)
		default:
			if _, twice := out[name]; twice {
				return fmt.Errorf("alpine: %s: the record at line %d: %s twice", InstalledDB, start, name)
			}
			out[name] = version
		}
		name, version, start = "", "", 0
		return nil
	}
	for i, l := range strings.Split(string(data), "\n") {
		if l == "" {
			if err := end(); err != nil {
				return nil, err
			}
			continue
		}
		if start == 0 {
			start = i + 1
		}
		switch {
		case strings.HasPrefix(l, "P:"):
			name = l[2:]
		case strings.HasPrefix(l, "V:"):
			version = l[2:]
		}
	}
	if err := end(); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadInstalled reads and parses InstalledDB. It changes nothing.
func ReadInstalled(ctx context.Context, sys host.System) (map[string]string, error) {
	data, err := sys.ReadFile(ctx, InstalledDB)
	if err != nil {
		return nil, fmt.Errorf("alpine: %w", err)
	}
	return ParseInstalled(data)
}

// versionForm is apk's version grammar, as hardn compares it:
// digits(.digits)*[letter](_suffix[digits])*(-rN)? (apk-tools' version.c);
// a "~" commit hash, or any other form, is not compared.
var versionForm = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)*)([a-z]?)((?:_(?:alpha|beta|pre|rc|cvs|svn|git|hg|p)[0-9]*)*)(?:-r([0-9]+))?$`)

// suffixRank is apk's suffix order: the pre-releases below a version with
// no suffix (0 here), the post-releases above it.
var suffixRank = map[string]int{"alpha": -4, "beta": -3, "pre": -2, "rc": -1, "cvs": 1, "svn": 2, "git": 3, "hg": 4, "p": 5}

// The token kinds of a version, in apk's order: where two versions part,
// the one whose next token is of the later kind is the lesser (an extra
// component wins), except that a pre-release suffix is always the lesser.
const (
	tokDigit = iota
	tokLetter
	tokSuffix
	tokSuffixNo
	tokRevision
	tokEnd
)

type token struct {
	kind int
	v    int64
}

// tokens splits v into apk's tokens. A component after the first with a
// leading zero and more digits ("1.05") is refused: apk compares those as
// a decimal fraction, which hardn does not model, never a guess.
func tokens(v string) ([]token, error) {
	m := versionForm.FindStringSubmatch(v)
	if m == nil {
		return nil, fmt.Errorf("alpine: version %q: not one hardn compares", v)
	}
	num := func(s string) (int64, error) {
		if s == "" {
			return 0, nil
		}
		return strconv.ParseInt(s, 10, 64)
	}
	var ts []token
	for i, c := range strings.Split(m[1], ".") {
		if i > 0 && len(c) > 1 && c[0] == '0' {
			return nil, fmt.Errorf("alpine: version %q: a leading zero in %q, which hardn does not compare", v, c)
		}
		n, err := num(c)
		if err != nil {
			return nil, fmt.Errorf("alpine: version %q: %w", v, err)
		}
		ts = append(ts, token{tokDigit, n})
	}
	if m[2] != "" {
		ts = append(ts, token{tokLetter, int64(m[2][0])})
	}
	for _, s := range strings.Split(m[3], "_")[1:] {
		word := strings.TrimRight(s, "0123456789")
		n, err := num(s[len(word):])
		if err != nil {
			return nil, fmt.Errorf("alpine: version %q: %w", v, err)
		}
		ts = append(ts, token{tokSuffix, int64(suffixRank[word])}, token{tokSuffixNo, n})
	}
	if m[4] != "" {
		n, err := num(m[4])
		if err != nil {
			return nil, fmt.Errorf("alpine: version %q: %w", v, err)
		}
		ts = append(ts, token{tokRevision, n})
	}
	return ts, nil
}

// CompareVersion compares two apk versions: -1 when a is older than b, 0
// when they are the same, 1 when a is newer, as apk-tools' version.c
// orders them (1.2 < 1.2a < 1.2.1; 1.0_rc1 < 1.0 < 1.0_p1; 1.0 < 1.0-r0 <
// 1.0-r1). A version not in versionForm is an error, never a guess. [I]
// The order across kinds (a letter against an extra component, an extra
// suffix against a revision) is version.c's as read, not run against apk;
// the fixed table's comparisons differ in a revision or a suffix's number
// alone.
func CompareVersion(a, b string) (int, error) {
	ta, err := tokens(a)
	if err != nil {
		return 0, err
	}
	tb, err := tokens(b)
	if err != nil {
		return 0, err
	}
	at := func(ts []token, i int) token {
		if i < len(ts) {
			return ts[i]
		}
		return token{kind: tokEnd}
	}
	for i := 0; ; i++ {
		x, y := at(ta, i), at(tb, i)
		switch {
		case x.kind == tokEnd && y.kind == tokEnd:
			return 0, nil
		case x.kind == y.kind && x.v < y.v:
			return -1, nil
		case x.kind == y.kind && x.v > y.v:
			return 1, nil
		case x.kind == y.kind:
			continue
		case x.kind == tokSuffix && x.v < 0:
			return -1, nil
		case y.kind == tokSuffix && y.v < 0:
			return 1, nil
		case x.kind > y.kind:
			return -1, nil
		}
		return 1, nil
	}
}

// The openssh-server packages: Alpine's sshd without PAM, and the builds
// with PAM it installs beside it (reload.BinaryPAM, reload.BinaryKrb5),
// which the same fix covers.
const (
	OpenSSHServer     = "openssh-server"
	OpenSSHServerPAM  = "openssh-server-pam"
	OpenSSHServerKrb5 = "openssh-server-krb5"
)

// opensshFixed is the first release of each branch with CVE-2026-123158's
// fix, for openssh-server, -pam and -krb5 alike (Alpine's post of
// 2026-11-05); opensshFixedElse is edge's, which any other branch, and a
// VERSION_ID that is not a stable release's, is compared with.
var opensshFixed = map[string]string{
	"3.24": "10.3_p1-r2",
	"3.23": "10.2_p1-r1",
	"3.22": "10.0_p1-r11",
	"3.21": "9.9_p2-r1",
	// Branches whose support ended, which the post lists too and hardn
	// changes nothing on, so that their status reads the fix as Alpine
	// shipped it (each branch's openssh APKBUILD secfixes, read 2026-11-08).
	"3.20": "9.7_p1-r6",
	"3.19": "9.6_p1-r3",
	"3.18": "9.3_p2-r4",
	"3.17": "9.1_p1-r7",
	"3.16": "9.0_p1-r6",
}

const opensshFixedElse = "10.6_p2-r0"

// FixedVersion is the first version of the openssh-server packages with
// the fix on the branch of versionID, os-release's VERSION_ID: "3.24" for
// "3.24.2", from opensshFixed; edge's for a branch it does not list, and
// for a VERSION_ID that is not x.y.z (edge's "3.25.0_alpha20261001").
func FixedVersion(versionID string) string {
	parts := strings.Split(versionID, ".")
	if len(parts) == 3 && digitsOnly(parts[0]) && digitsOnly(parts[1]) && digitsOnly(parts[2]) {
		if v, ok := opensshFixed[parts[0]+"."+parts[1]]; ok {
			return v
		}
	}
	return opensshFixedElse
}

func digitsOnly(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// Fix is whether an openssh-server package has CVE-2026-123158's fix:
// Fixed Yes when the installed version is the branch's first fixed one or
// later, No when earlier, Unknown with Why when that is not established
// (InstalledDB missing or not read, no entry for the package, a version
// not compared). Installed is apk's version, "" where not read; Wanted the
// branch's first fixed one, or upstream's (opensshFixedElse) for a package
// of a later release than the branch's.
type Fix struct {
	Package, Installed, Wanted string
	Fixed                      detect.Tri
	Why                        string
}

// OpenSSHFix reads InstalledDB for pkg, on the branch of versionID (detect's
// OS.VersionID). It changes nothing. It reads the package, not the running
// daemon: an upgrade reads Yes at once, which holds for every new
// connection, as each runs the sshd-session (and sshd-auth) on disk; the
// listener keeps the old binary until sshd is reloaded (ListenerBinary;
// docs/safety/alpine.md §7.1, the 2026-11-08 probe).
func OpenSSHFix(ctx context.Context, sys host.System, pkg, versionID string) Fix {
	f := Fix{Package: pkg, Wanted: FixedVersion(versionID)}
	db, err := ReadInstalled(ctx, sys)
	var pe *fs.PathError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		f.Why = InstalledDB + " is missing"
		return f
	case errors.As(err, &pe):
		f.Why = pe.Error()
		return f
	case err != nil:
		f.Why = strings.TrimPrefix(err.Error(), "alpine: ")
		return f
	}
	v, ok := db[pkg]
	if !ok {
		f.Why = pkg + " isn't in " + InstalledDB
		return f
	}
	f.Installed = v
	c, err := CompareVersion(v, f.Wanted)
	if err != nil {
		f.Why = "version " + v + " isn't one hardn compares"
		return f
	}
	if c >= 0 && upstream(v) != upstream(f.Wanted) { // another release than the branch's (edge's pinned on 3.24, say): not the branch's backport
		f.Wanted = opensshFixedElse
		if c, err = CompareVersion(v, f.Wanted); err != nil {
			f.Why = "version " + v + " isn't one hardn compares"
			return f
		}
	}
	f.Fixed = detect.No
	if c >= 0 {
		f.Fixed = detect.Yes
	}
	return f
}

// upstream is a version without its -rN, the upstream release a branch's
// package builds ("10.3_p1" of "10.3_p1-r2"). A stable branch's fix is a
// backport into its own release, so a package of a later release, from
// another branch, has the fix only from upstream's, opensshFixedElse.
func upstream(v string) string {
	if i := strings.LastIndex(v, "-r"); i >= 0 {
		return v[:i]
	}
	return v
}
