package logview

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
)

// Command is what runs to show the whole log: Argv and Env for
// host.System.RunInteractive, which adds Run's PATH.
type Command struct {
	Argv []string
	Env  []string
}

// errNoPager is why Run runs nothing where less is no longer what Pager found.
var errNoPager = errors.New("logview: less is not GNU less")

// Run runs c through sys: it has the terminal until it ends. Pager was asked at
// a draw, and RunInteractive finds less again by its name, so a package changed
// under an open menu could otherwise run BusyBox's as root: the check runs again
// here, and fails closed.
func (c Command) Run(ctx context.Context, sys host.System) error {
	if !gnuLess(ctx, sys, lessCheck, lessGrace) {
		return errNoPager
	}
	return sys.RunInteractive(ctx, c.Env, c.Argv[0], c.Argv[1:]...)
}

// The bounds on less --version, which a draw waits for: past lessGrace it is
// left behind (host.RunBounded), and a timeout is no pager.
const (
	lessCheck = 5 * time.Second
	lessGrace = time.Second
)

// lessVersion is the first line of GNU less's --version: "less 668 (GNU regular
// expressions)" on Debian 13 and Ubuntu 26.04, where BusyBox's says "less:
// unrecognized option: version" on Alpine 3.23
// (_support/hardn-audit-2026-10-16/vm-runs/s1/01-tools.log). The digits alone:
// its words change with how it was built.
var lessVersion = regexp.MustCompile(`^less [0-9]+( |$)`)

// Pager returns the command that shows the whole log, or false for none
// (decision 7 (a), gallery-system-details.md §5): GNU less alone, the one less
// with a secure mode. less --version, which Run finds on host.SearchPath as an
// executable file, must exit 0 and print what lessVersion matches. Anything else,
// any error, is no pager: it fails closed, as BusyBox's less writes any path at
// its s key (the page, §5, decision 7). It reads no variable: $PAGER names a
// program a root process would run.
//
// term is the value of TERM to pass: it comes from whoever started a root
// process, and names a file in the terminfo database, so it is passed only
// when safeTerm passes it.
func Pager(ctx context.Context, sys host.System, term string) (Command, bool) {
	return pager(ctx, sys, term, lessCheck, lessGrace)
}

func pager(ctx context.Context, sys host.System, term string, limit, grace time.Duration) (Command, bool) {
	if !gnuLess(ctx, sys, limit, grace) {
		return Command{}, false
	}
	return Command{
		// "--" ends less's options: the path is hardn's own, and this
		// keeps it from ever being one (less(1); run on this Mac's less 581.2,
		// TestRunInteractiveLess; written, not run on GNU less 668).
		Argv: []string{"less", "--", File},
		Env: []string{
			// Turns off !, |, E, :e and v (vm-runs/s1/04-less2-deb13-vm.log).
			"LESSSECURE=1",
			// No history file: what is typed at less's prompts stays out of
			// root's home, and "-" says so whatever less takes for that
			// without HOME [I, less(1): not run].
			"LESSHISTFILE=-",
			"TERM=" + safeTerm(term),
			// So a log's non-ASCII shows as itself: where the locale is
			// missing, less shows its bytes as <C3><A9>, which is safe. C.UTF-8
			// is built into glibc from 2.35 and any name is UTF-8 to musl
			// [I: not run on a VM].
			"LC_ALL=C.UTF-8",
		},
	}, true
}

// gnuLess is Pager's check.
func gnuLess(ctx context.Context, sys host.System, limit, grace time.Duration) bool {
	res, err := host.RunBounded(ctx, sys, limit, grace, "less", "--version")
	if err != nil || res.ExitCode != 0 {
		return false
	}
	first, _, _ := strings.Cut(string(res.Combined), "\n")
	return lessVersion.MatchString(first)
}

// fallbackTerm is TERM where the value given is not safe to pass: the
// VMs' less ran under it (vm-runs/s1/04-less2-*.log).
const fallbackTerm = "vt100"

// safeTerm returns term where it can only be a terminal's name, else
// fallbackTerm. The name reaches the terminfo lookup of a root process, so no
// "/" or "..", and no control character.
func safeTerm(term string) string {
	if len(term) == 0 || len(term) > 64 {
		return fallbackTerm
	}
	for i := 0; i < len(term); i++ {
		c := term[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case i > 0 && (c == '-' || c == '_' || c == '+' || c == '.') && term[i-1] != '.':
		default:
			return fallbackTerm
		}
	}
	return term
}
