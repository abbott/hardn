// Package sudoers writes the elevation rules hardn manages, for sudo (and
// sudo-rs, Ubuntu 26.04's default) and for doas (Alpine's default), so that
// a rule is validated before it takes effect and undone when the whole
// configuration does not validate after it (refactor plan P2,
// next/p2-sudoers; decisions §3.17 and §3.21).
//
// sudo: a user's grant and its env_keep line are separate files in
// /etc/sudoers.d, mode 0440, root:root, named so that sudo and sudo-rs read
// them (no '.', no trailing '~'). doas: one rule per user in
// /etc/doas.d/<name>.conf, because doas takes its options (setenv among
// them) from the last matching rule alone.
//
// Choose picks the tool from detect's facts, Read reads what the plan needs,
// PlanFor is pure, and Apply installs a plan over host.System: the whole
// configuration is checked before anything is written, each candidate is
// checked as a copy before it is renamed into place, and the whole
// configuration again after, with the old files put back if it fails.
//
// docs/safety/sudoers.md describes the guarantees, the platform facts they
// rest on and what is written but not yet run on a Linux host.
package sudoers

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host/detect"
)

// Paths and modes.
const (
	MainConfig = "/etc/sudoers"
	// SudoersRS is the file sudo-rs reads instead of MainConfig when it
	// exists; the sudo project's sudo never reads it.
	SudoersRS  = "/etc/sudoers-rs"
	DropInDir  = "/etc/sudoers.d"
	DoasConfig = "/etc/doas.conf"
	DoasDir    = "/etc/doas.d"
	// SudoMode is the mode of hardn's sudoers files, root:root.
	SudoMode fs.FileMode = 0o440
	// DoasMode is the mode of hardn's doas files, root:root: the mode
	// Alpine's doas package gives /etc/doas.conf.
	DoasMode fs.FileMode = 0o640
	// WorkDir is the directory Apply creates in Options.RunDir for the
	// copies it checks, and removes when it is done.
	WorkDir = "sudoers-validate"
)

// Name prefixes: every file hardn manages starts with "hardn-", which sorts
// after the digit-led and upper-case names distributions and cloud-init use
// (90-cloud-init-users, README, 20-wheel.conf), so its rules come later and
// win (the last matching rule wins).
const (
	grantPrefix = "hardn-grant-"
	envPrefix   = "hardn-env-"
	doasPrefix  = "hardn-"
	doasSuffix  = ".conf"
)

// Header is the first line of every file hardn writes.
const Header = "# Managed by hardn\n"

// Errors, testable with errors.Is.
var (
	ErrNoTool        = errors.New("no elevation tool is installed")
	ErrAmbiguous     = errors.New("both sudo and doas are installed; name the one to configure")
	ErrUnsupported   = errors.New("doas drop-ins are established on Alpine only")
	ErrUserName      = errors.New("a user name hardn does not write into an elevation rule")
	ErrVarName       = errors.New("not an environment variable name")
	ErrNotIncluded   = errors.New("the elevation tool does not read the drop-in directory")
	ErrEnvNeedsGrant = errors.New("doas keeps variables only on the rule that permits the user")
	ErrRejected      = errors.New("the elevation tool rejects the configuration")
	ErrPerms         = errors.New("not owned by root, or writable by group or others")
	ErrChanged       = errors.New("the file changed since it was read")
	ErrUnsafeRunDir  = errors.New("run directory another user can change, or reached through a symlink")
	// ErrNotRestored: a restore did not complete. A file could not be put
	// back, or holds neither what Read found nor what the plan wrote
	// (ErrChanged, left alone), or the configuration put back did not
	// check (a StageRestored *CheckError).
	ErrNotRestored = errors.New("the files were not all put back, or do not check")
)

// Tool is the elevation tool a plan configures. The zero value is Unknown.
type Tool uint8

const (
	ToolUnknown Tool = iota
	ToolNone         // NotApplicable: neither sudo nor doas is installed
	ToolSudo         // sudo or sudo-rs
	ToolDoas
)

func (t Tool) String() string {
	switch t {
	case ToolNone:
		return "none"
	case ToolSudo:
		return "sudo"
	case ToolDoas:
		return "doas"
	}
	return "unknown"
}

// Choice is the tool Choose picked. Branch on Tool: Err is set exactly
// when Tool is neither ToolSudo nor ToolDoas.
type Choice struct {
	Tool Tool
	// Flavor tells sudo from sudo-rs (ToolSudo). Both read the rules hardn
	// writes and are checked with the same visudo flags, so an Unknown
	// flavour does not stop a plan; FlavorErr then says why it is Unknown.
	Flavor    detect.SudoFlavor
	FlavorErr error
	Err       error // why Tool is ToolUnknown or ToolNone
}

// Choose picks the tool from detect.Detect's facts: want is ToolSudo or
// ToolDoas to name one, ToolUnknown to take the one installed. It installs
// nothing: a host with neither tool (Proxmox VE, which ships without sudo
// [I]) is ToolNone with ErrNoTool, NotApplicable. doas is configured only on
// Alpine, whose package reads /etc/doas.d; where the answer depends on
// whether the host is Alpine and that is not established, the tool is
// ToolUnknown.
func Choose(f detect.Facts, want Tool) Choice {
	e := f.Elevation
	sudo, doas := e.Sudo.Present, e.Doas.Present
	switch d := f.Platform.Distro; {
	case want != ToolUnknown && want != ToolSudo && want != ToolDoas:
		return Choice{Err: fmt.Errorf("sudoers: tool %s: %w", want, fs.ErrInvalid)}
	case d == detect.DistroAlpine, want == ToolSudo, want == ToolUnknown && doas == detect.No:
		// The distribution does not change the answer.
	case d == detect.DistroUnknown:
		return unknown("distribution not established, and doas is configured on Alpine only", f.Platform.Err, e.Sudo.Err, e.Doas.Err)
	case want == ToolDoas, sudo == detect.No && doas == detect.Yes:
		return Choice{Err: fmt.Errorf("sudoers: doas on %q: %w", d, ErrUnsupported)}
	case sudo == detect.No:
		// doas Unknown: installed, it would be ErrUnsupported; absent, ToolNone.
		return unknown("doas not established", e.Doas.Err)
	default:
		doas = detect.No // not configured here, so only sudo counts
	}
	if want == ToolUnknown {
		switch {
		case sudo == detect.Unknown || doas == detect.Unknown:
			return unknown("elevation tools not established", e.Sudo.Err, e.Doas.Err)
		case sudo == detect.Yes && doas == detect.Yes:
			return Choice{Err: fmt.Errorf("sudoers: %w", ErrAmbiguous)}
		case sudo == detect.Yes:
			want = ToolSudo
		case doas == detect.Yes:
			want = ToolDoas
		default:
			return Choice{Tool: ToolNone, Err: fmt.Errorf("sudoers: %w", ErrNoTool)}
		}
	}
	present, cause := sudo, e.Sudo.Err
	if want == ToolDoas {
		present, cause = doas, e.Doas.Err
	}
	switch {
	case present == detect.Unknown:
		return unknown(want.String()+" not established", cause)
	case present != detect.Yes:
		return Choice{Tool: ToolNone, Err: fmt.Errorf("sudoers: %s: %w", want, ErrNoTool)}
	case want == ToolSudo:
		return Choice{Tool: ToolSudo, Flavor: e.SudoFlavor, FlavorErr: e.Err}
	}
	return Choice{Tool: ToolDoas}
}

// unknown is a ToolUnknown choice: what is not established, and why.
func unknown(what string, causes ...error) Choice {
	cause := errors.Join(causes...)
	if cause == nil {
		cause = detect.ErrNoSignal
	}
	return Choice{Err: fmt.Errorf("sudoers: %s: %w", what, cause)}
}

// userName is a lower-case first character, then lower-case letters, digits,
// '_', '.' and '-', and an optional trailing '$': a superset of the legacy
// menu's ^[a-z][a-z0-9_-]*$ and of Debian adduser's default NAME_REGEX. No
// upper case, so a name is never a sudoers alias or ALL; no '#', '%' or
// '+', which sudoers reads as a uid, a group or a netgroup; no '~'.
var userName = regexp.MustCompile(`^[a-z_][a-z0-9_.-]*\$?$`)

// doasKeywords are doas.conf(5)'s keywords; as a bare word in a rule each
// would be read as itself, not as a user.
var doasKeywords = []string{"permit", "deny", "as", "cmd", "args", "nopass", "nolog", "persist", "keepenv", "setenv"}

// CheckUser refuses a name hardn does not write into a rule for tool: see
// userName; at most 32 bytes; for doas, not a keyword.
func CheckUser(t Tool, name string) error {
	if len(name) > 32 || !userName.MatchString(name) || t == ToolDoas && slices.Contains(doasKeywords, name) {
		return fmt.Errorf("sudoers: %q: %w", name, ErrUserName)
	}
	return nil
}

var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func checkVars(vars []string) error {
	for i, v := range vars {
		if !varName.MatchString(v) || slices.Contains(vars[:i], v) {
			return fmt.Errorf("sudoers: variable %q: %w", v, ErrVarName)
		}
	}
	return nil
}

// stem encodes a checked user name for a file name, one-to-one: lower-case
// letters, digits and '-' as they are, '_' as "__", any other byte as '_'
// and two lower-case hex digits ("alice.smith" is "alice_2esmith"). The
// result has no '.' and no '~'.
func stem(user string) string {
	var b strings.Builder
	for i := 0; i < len(user); i++ {
		switch c := user[i]; {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
			b.WriteByte(c)
		case c == '_':
			b.WriteString("__")
		default:
			fmt.Fprintf(&b, "_%02x", c)
		}
	}
	return b.String()
}

// unstem is stem's inverse: ok only when s is exactly stem(user) of a name
// CheckUser accepts for sudo.
func unstem(s string) (user string, ok bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '_' {
			b.WriteByte(s[i])
			continue
		}
		var c byte
		switch {
		case i+1 < len(s) && s[i+1] == '_':
			c, i = '_', i+1
		case i+2 < len(s):
			n, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil {
				return "", false
			}
			c, i = byte(n), i+2
		default:
			return "", false
		}
		b.WriteByte(c)
	}
	user = b.String()
	return user, CheckUser(ToolSudo, user) == nil && stem(user) == s
}

// Names are the files hardn manages for a user.
type Names struct {
	Grant string // sudo: the grant; doas: the user's one rule
	Env   string // sudo: the env_keep line; "" for doas
}

// FileNames returns the paths hardn manages for user under tool.
func FileNames(t Tool, user string) (Names, error) {
	if err := CheckUser(t, user); err != nil {
		return Names{}, err
	}
	s := stem(user)
	switch t {
	case ToolSudo:
		return Names{Grant: DropInDir + "/" + grantPrefix + s, Env: DropInDir + "/" + envPrefix + s}, nil
	case ToolDoas:
		return Names{Grant: DoasDir + "/" + doasPrefix + s + doasSuffix}, nil
	}
	return Names{}, fmt.Errorf("sudoers: tool %s: %w", t, fs.ErrInvalid)
}

// owned reports whether name (a base name in the drop-in directory) is one
// of hardn's per-user files for tool.
func owned(t Tool, name string) bool {
	var s string
	switch {
	case t == ToolSudo && strings.HasPrefix(name, grantPrefix):
		s = name[len(grantPrefix):]
	case t == ToolSudo && strings.HasPrefix(name, envPrefix):
		s = name[len(envPrefix):]
	case t == ToolDoas && strings.HasPrefix(name, doasPrefix) && strings.HasSuffix(name, doasSuffix):
		s = strings.TrimSuffix(name[len(doasPrefix):], doasSuffix)
	default:
		return false
	}
	user, ok := unstem(s)
	return ok && CheckUser(t, user) == nil
}

// Grant is the rule a policy gives a user.
type Grant uint8

const (
	GrantNone       Grant = iota // no rule from hardn: hardn's file is removed
	GrantPassword                // the user's password is asked
	GrantNoPassword              // NOPASSWD / nopass
)

func (g Grant) String() string {
	switch g {
	case GrantNone:
		return "none"
	case GrantPassword:
		return "password"
	case GrantNoPassword:
		return "no-password"
	}
	return "unknown"
}

// Policy is what hardn manages for one user.
type Policy struct {
	User  string
	Grant Grant
	// Env are variables the elevated command keeps (sudo env_keep, doas
	// setenv), such as HARDN_CONFIG; none removes hardn's env_keep file.
	// On doas they go on the user's rule, so Env needs a Grant.
	Env []string
}

// sudoGrant is origin/main's grant line (pkg/adapter/secondary,
// ConfigureSudo), kept as it was (brief §3 principle 2).
func sudoGrant(user string, g Grant) []byte {
	rule := user + " ALL=(ALL) ALL\n"
	if g == GrantNoPassword {
		rule = user + " ALL=(ALL) NOPASSWD: ALL\n"
	}
	return []byte(Header + rule)
}

// sudoEnv is origin/main's env_keep line (setup-sudo-env), in its own file.
func sudoEnv(user string, vars []string) []byte {
	return []byte(Header + "Defaults:" + user + " env_keep += \"" + strings.Join(vars, " ") + "\"\n")
}

// doasRule is the user's one doas rule, in the shape of Alpine's
// 20-wheel.conf ("permit persist :wheel").
func doasRule(user string, g Grant, vars []string) []byte {
	opt := "persist"
	if g == GrantNoPassword {
		opt = "nopass"
	}
	if len(vars) > 0 {
		opt += " setenv { " + strings.Join(vars, " ") + " }"
	}
	return []byte(Header + "permit " + opt + " " + user + "\n")
}
