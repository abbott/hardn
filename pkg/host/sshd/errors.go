package sshd

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// Errors Validate returns, testable with errors.Is.
var (
	// ErrConfig: sshd rejected the configuration (exit 255); see ConfigError.
	ErrConfig = errors.New("sshd rejects the configuration")
	// ErrEnvironment: sshd could not check it on this host; see EnvError.
	ErrEnvironment = errors.New("sshd cannot check the configuration on this host")
	// ErrUnexpected: sshd failed in a way Validate does not classify.
	ErrUnexpected = errors.New("unexpected sshd result")
	// ErrUnsupportedInclude: an Include other than IncludeLine, in
	// sshd_config or in a drop-in. The copy could not reproduce what it
	// reads, so nothing is checked.
	ErrUnsupportedInclude = errors.New("an Include the validation copy cannot reproduce")
	// ErrNotIncluded: a drop-in written while sshd_config has no Include,
	// so sshd would never read it. Removing one is allowed (and changes
	// nothing sshd reads).
	ErrNotIncluded = errors.New("sshd_config does not Include sshd_config.d")
	// ErrUnsafeRunDir: the run directory, or a directory above it, is a
	// symlink or can be changed by a user other than root.
	ErrUnsafeRunDir = errors.New("run directory another user can change, or reached through a symlink")
	// ErrNotRegular: a configuration file is not a regular file.
	ErrNotRegular = errors.New("not a regular file")
)

// Problem is one complaint sshd made about a line of a file.
type Problem struct {
	File    string // the real path
	Line    int
	Message string
}

// ConfigError is a configuration sshd rejected: it exited 255 (fatal) for
// something other than the environment. errors.Is(err, ErrConfig) holds, and
// Unwrap gives the *host.ExitError.
type ConfigError struct {
	Argv     []string
	Problems []Problem // the lines sshd named, in its order
	Tail     []string  // the last lines of its output
	Err      error
}

func (e *ConfigError) Error() string {
	detail := "exit 255"
	switch {
	case len(e.Problems) > 0:
		p := e.Problems[0]
		detail = fmt.Sprintf("%s line %d: %s", p.File, p.Line, p.Message)
	case len(e.Tail) > 0:
		detail = e.Tail[len(e.Tail)-1]
	}
	return "sshd: configuration rejected: " + detail
}

// Is makes errors.Is(err, ErrConfig) hold.
func (e *ConfigError) Is(target error) bool { return target == ErrConfig }

// Unwrap returns the *host.ExitError.
func (e *ConfigError) Unwrap() error { return e.Err }

// EnvKind names what the host lacks.
type EnvKind uint8

const (
	// EnvPrivsepDir: the privilege separation directory is missing, not a
	// directory, or not root's alone (sshd: "Missing privilege separation
	// directory", "must be owned by root and not group or world-writable").
	EnvPrivsepDir EnvKind = iota + 1
	// EnvHostKeys: sshd loaded no host key (exit 1, "no hostkeys
	// available"), as when it runs without root.
	EnvHostKeys
)

func (k EnvKind) String() string {
	switch k {
	case EnvPrivsepDir:
		return "privilege separation directory"
	case EnvHostKeys:
		return "host keys"
	}
	return "unknown"
}

// EnvError is a check sshd could not make on this host; the configuration
// itself may be fine. errors.Is(err, ErrEnvironment) holds.
type EnvError struct {
	Kind EnvKind
	Path string   // EnvPrivsepDir: the directory
	Argv []string // nil when Validate's own check found it, before running sshd
	Tail []string
	Err  error
}

func (e *EnvError) Error() string {
	msg := "sshd: cannot check the configuration: "
	switch {
	case e.Kind == EnvPrivsepDir && e.Argv == nil:
		return msg + fmt.Sprintf("privilege separation directory %s: %v", e.Path, e.Err)
	case e.Kind == EnvPrivsepDir:
		return msg + "privilege separation directory " + e.Path + " is missing or not root's alone"
	case e.Kind == EnvHostKeys:
		return msg + "no host key could be loaded"
	}
	return msg + fmt.Sprint(e.Err)
}

// Is makes errors.Is(err, ErrEnvironment) hold.
func (e *EnvError) Is(target error) bool { return target == ErrEnvironment }

// Unwrap returns the error behind it.
func (e *EnvError) Unwrap() error { return e.Err }

var (
	// problemLine is sshd's "<file> line <n>: <message>", and the
	// "<file>: line <n>: Bad configuration option: <keyword>" form
	// (servconf.c).
	problemLine = regexp.MustCompile(`^(.+?):? line ([0-9]+): (.+)$`)
	privsepLine = regexp.MustCompile(`Missing privilege separation directory: (\S+)|(\S+) must be owned by root and not group or world-writable`)
)

// problems lists the lines of out that name a file and a line.
func problems(out string) []Problem {
	var ps []Problem
	for _, l := range strings.Split(out, "\n") {
		if m := problemLine.FindStringSubmatch(strings.TrimRight(l, "\r")); m != nil {
			n, _ := strconv.Atoi(m[2])
			ps = append(ps, Problem{File: m[1], Line: n, Message: m[3]})
		}
	}
	return ps
}

// classify turns a failed sshd run into Validate's errors, with the copy's
// paths replaced by the real ones. The environment is checked before exit
// 255, since sshd's fatal() exits 255 for a missing privsep directory too.
func classify(res host.Result, err error, paths *strings.Replacer) error {
	if err == nil {
		return nil
	}
	var xe *host.ExitError
	if !errors.As(err, &xe) {
		return fmt.Errorf("sshd: %w", err)
	}
	out := paths.Replace(string(res.Combined))
	tail := make([]string, len(res.Tail))
	for i, l := range res.Tail {
		tail[i] = paths.Replace(l)
	}
	switch m := privsepLine.FindStringSubmatch(out); {
	case m != nil:
		return &EnvError{Kind: EnvPrivsepDir, Path: m[1] + m[2], Argv: xe.Argv, Tail: tail, Err: err}
	case xe.ExitCode == 1 && strings.Contains(out, "no hostkeys available"):
		return &EnvError{Kind: EnvHostKeys, Argv: xe.Argv, Tail: tail, Err: err}
	case xe.ExitCode == 255:
		return &ConfigError{Argv: xe.Argv, Problems: problems(out), Tail: tail, Err: err}
	}
	return fmt.Errorf("%w: %w", ErrUnexpected, err)
}
