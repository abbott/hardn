package detect

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// The os-release files, in the order os-release(5) gives: the first that
// exists is used alone.
const (
	EtcOSRelease = "/etc/os-release"
	UsrOSRelease = "/usr/lib/os-release"
)

// OSRelease is the operating system identification os-release(5) defines.
type OSRelease struct {
	Path            string   // the file read; "" when none could be
	ID              string   // e.g. debian, ubuntu, alpine (Proxmox VE says debian)
	IDLike          []string // ID_LIKE, closest first
	VersionID       string   // e.g. 13, 24.04, 3.24.2; "" on Debian testing and sid
	VersionCodename string   // e.g. trixie, noble; Alpine sets none
	PrettyName      string
	Err             error // why no file could be read or it names no ID
}

// Known reports whether the release was read and names an ID.
func (r OSRelease) Known() bool { return r.Err == nil && r.ID != "" }

// ReadOSRelease reads /etc/os-release, or /usr/lib/os-release when the first
// is missing, as os-release(5) says: "exclusively use its data if it exists".
// An /etc/os-release that exists but cannot be read is not passed over; the
// release is then not Known.
func ReadOSRelease(ctx context.Context, sys host.System) OSRelease {
	for _, p := range []string{EtcOSRelease, UsrOSRelease} {
		data, err := sys.ReadFile(ctx, p)
		switch {
		case missing(err):
			continue
		case err != nil:
			return OSRelease{Err: fmt.Errorf("detect: os-release: %w", err)}
		}
		v := ParseOSRelease(data)
		r := OSRelease{
			Path:            p,
			ID:              v["ID"],
			IDLike:          strings.Fields(v["ID_LIKE"]),
			VersionID:       v["VERSION_ID"],
			VersionCodename: v["VERSION_CODENAME"],
			PrettyName:      v["PRETTY_NAME"],
		}
		if r.ID == "" {
			r.Err = fmt.Errorf("detect: os-release: %s sets no ID: %w", p, ErrUnrecognised)
		}
		return r
	}
	return OSRelease{Err: fmt.Errorf("detect: os-release: neither %s nor %s: %w", EtcOSRelease, UsrOSRelease, fs.ErrNotExist)}
}

// ParseOSRelease parses os-release(5) assignments: KEY=value, with the value
// unquoted, in single quotes or in double quotes, and a backslash escaping
// the next character (inside double quotes only $, ", \ and `), as a shell
// reads them. Blank lines and lines starting with # are skipped; a line that
// is not such an assignment is dropped; a key given twice keeps its last
// value, as a shell sourcing the file would. No variable is expanded.
func ParseOSRelease(data []byte) map[string]string {
	vars := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, raw, ok := strings.Cut(line, "=")
		if !ok || !validKey(k) {
			continue
		}
		if v, err := unquote(raw); err == nil {
			vars[k] = v
		}
	}
	return vars
}

func validKey(k string) bool {
	for i, c := range k {
		alpha := c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		if !alpha && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return k != ""
}

var errSyntax = errors.New("os-release: not a shell assignment")

// unquote reads one shell word: quoted parts, escapes, and nothing after
// unquoted white space.
func unquote(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return "", errSyntax
			}
			b.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case '"':
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("$\"\\`", s[i+1]) >= 0 {
					i++
				}
				b.WriteByte(s[i])
			}
			if i == len(s) {
				return "", errSyntax
			}
		case '\\':
			if i++; i == len(s) {
				return "", errSyntax
			}
			b.WriteByte(s[i])
		case ' ', '\t':
			return "", errSyntax // a second word: a command, not an assignment
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}
