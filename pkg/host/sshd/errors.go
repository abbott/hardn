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
	// ErrUnexpected: sshd failed in a way Validate does not classify; see
	// UnexpectedError.
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
	Argv     []string  // the argv that ran, naming the copy
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
	// available"), as when it runs without root. sshd falls back to its
	// default key files only when the configuration names none, so with
	// EnvError.HostKeysSet the configuration's own HostKey lines may be the
	// cause (a missing file).
	EnvHostKeys
	// EnvPrivsepUser: the privilege separation user does not exist (sshd:
	// "Privilege separation user %s does not exist", fatal, so exit 255).
	EnvPrivsepUser
)

func (k EnvKind) String() string {
	switch k {
	case EnvPrivsepDir:
		return "privilege separation directory"
	case EnvHostKeys:
		return "host keys"
	case EnvPrivsepUser:
		return "privilege separation user"
	}
	return "unknown"
}

// EnvError is a check sshd could not make on this host; the configuration
// itself may be fine. errors.Is(err, ErrEnvironment) holds.
type EnvError struct {
	Kind EnvKind
	Path string // EnvPrivsepDir: the directory
	User string // EnvPrivsepUser: the user sshd names
	// HostKeysSet (EnvHostKeys): the configuration has HostKey lines, so
	// sshd tried those files and not its defaults.
	HostKeysSet bool
	Argv        []string // the argv that ran, naming the copy; nil when Validate's own check found it
	Tail        []string
	Err         error
}

func (e *EnvError) Error() string {
	msg := "sshd: cannot check the configuration: "
	switch {
	case e.Kind == EnvPrivsepDir && e.Argv == nil:
		return msg + fmt.Sprintf("privilege separation directory %s: %v", e.Path, e.Err)
	case e.Kind == EnvPrivsepDir:
		return msg + "privilege separation directory " + e.Path + " is missing or not root's alone"
	case e.Kind == EnvPrivsepUser:
		return msg + "privilege separation user " + e.User + " does not exist"
	case e.Kind == EnvHostKeys && e.HostKeysSet:
		return msg + "no host key could be loaded from the configuration's HostKey files"
	case e.Kind == EnvHostKeys:
		return msg + "no host key could be loaded"
	}
	return msg + fmt.Sprint(e.Err)
}

// Is makes errors.Is(err, ErrEnvironment) hold.
func (e *EnvError) Is(target error) bool { return target == ErrEnvironment }

// Unwrap returns the error behind it.
func (e *EnvError) Unwrap() error { return e.Err }

// UnexpectedError is a run of sshd that exited in a way Validate does not
// classify: an exit status other than those above, or a signal (ExitCode
// -1). errors.Is(err, ErrUnexpected) holds, and Unwrap gives the
// *host.ExitError.
type UnexpectedError struct {
	Argv     []string // the argv that ran, naming the copy
	ExitCode int
	Tail     []string // the last lines of its output, with the real paths
	Err      error
}

func (e *UnexpectedError) Error() string {
	detail := fmt.Sprintf("exit %d", e.ExitCode)
	if e.ExitCode == -1 {
		detail = "ended by a signal"
	}
	if len(e.Tail) > 0 {
		detail += ": " + e.Tail[len(e.Tail)-1]
	}
	return "sshd: " + ErrUnexpected.Error() + ": " + detail
}

// Is makes errors.Is(err, ErrUnexpected) hold.
func (e *UnexpectedError) Is(target error) bool { return target == ErrUnexpected }

// Unwrap returns the *host.ExitError.
func (e *UnexpectedError) Unwrap() error { return e.Err }

// mappedError is a host error from running sshd (it did not start, or the
// context ended), with the copy's paths in its text replaced by the real
// ones; Unwrap gives it unchanged.
type mappedError struct {
	msg string
	err error
}

func (e *mappedError) Error() string { return e.msg }
func (e *mappedError) Unwrap() error { return e.err }

var (
	// problemLine is sshd's "<file> line <n>: <message>", and the
	// "<file>: line <n>: Bad configuration option: <keyword>" form
	// (servconf.c).
	problemLine = regexp.MustCompile(`^(.+?):? line ([0-9]+): (.+)$`)
	// envLines are sshd's fatal() messages about the host, each a whole
	// line of its own (sshd.c; log.c writes them without a prefix).
	envLines = []struct {
		kind EnvKind
		re   *regexp.Regexp
	}{
		{EnvPrivsepDir, regexp.MustCompile(`^Missing privilege separation directory: (\S+)$`)},
		{EnvPrivsepDir, regexp.MustCompile(`^(\S+) must be owned by root and not group or world-writable\.$`)},
		{EnvPrivsepUser, regexp.MustCompile(`^Privilege separation user (\S+) does not exist$`)},
	}
)

// lines splits sshd's output, whose lines log.c ends with "\r\n".
func lines(out string) []string {
	ls := strings.Split(out, "\n")
	for i, l := range ls {
		ls[i] = strings.TrimSuffix(l, "\r")
	}
	return ls
}

// problems lists the lines of out that name a file and a line.
func problems(out string) []Problem {
	var ps []Problem
	for _, l := range lines(out) {
		if m := problemLine.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[2])
			ps = append(ps, Problem{File: m[1], Line: n, Message: m[3]})
		}
	}
	return ps
}

// environment finds a line of out that is one of envLines, skipping lines
// that name a file and a line: a configuration message can quote a value
// that looks like one.
func environment(out string) (EnvKind, string) {
	for _, l := range lines(out) {
		if problemLine.MatchString(l) {
			continue
		}
		for _, e := range envLines {
			if m := e.re.FindStringSubmatch(l); m != nil {
				return e.kind, m[1]
			}
		}
	}
	return 0, ""
}

// classify turns a failed sshd run into Validate's errors, with the copy's
// paths replaced by the real ones in every message and Tail (Argv keeps
// them: it is what ran). The environment is checked before exit 255, since
// sshd's fatal() exits 255 for a missing privsep directory too. hostKeys
// reports whether the configuration has HostKey lines.
func classify(res host.Result, err error, paths *strings.Replacer, hostKeys bool) error {
	if err == nil {
		return nil
	}
	var xe *host.ExitError
	if !errors.As(err, &xe) {
		return &mappedError{msg: "sshd: " + paths.Replace(err.Error()), err: err}
	}
	out := paths.Replace(string(res.Combined))
	tail := make([]string, len(res.Tail))
	for i, l := range res.Tail {
		tail[i] = paths.Replace(l)
	}
	if kind, name := environment(out); kind != 0 {
		e := &EnvError{Kind: kind, Path: name, Argv: xe.Argv, Tail: tail, Err: err}
		if kind == EnvPrivsepUser {
			e.Path, e.User = "", name
		}
		return e
	}
	switch {
	case xe.ExitCode == 1 && strings.Contains(out, "no hostkeys available"):
		return &EnvError{Kind: EnvHostKeys, HostKeysSet: hostKeys, Argv: xe.Argv, Tail: tail, Err: err}
	case xe.ExitCode == 255:
		return &ConfigError{Argv: xe.Argv, Problems: problems(out), Tail: tail, Err: err}
	}
	return &UnexpectedError{Argv: xe.Argv, ExitCode: xe.ExitCode, Tail: tail, Err: err}
}
