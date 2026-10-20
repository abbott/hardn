// Package hostfake is a recording, in-memory host.System for tests, in the
// style of the legacy interfaces.MockCommander and MockFileSystem: commands
// answer from a script keyed by argv, and every call is recorded in order.
// Unlike MockCommander, a command nothing scripted fails with ErrUnscripted,
// so a check a test forgot to script cannot read as a pass.
//
// Its file system keeps modes, owners and symlinks, and follows the os
// package's rules for them (a contract test runs the same cases against
// host.Real in a temporary directory), so tests of the safety primitives see
// the hazards a real host has: a write through a symlink, a rename over one,
// a chown or chmod that would follow one. Its guarded operations (see
// host.System) follow a symlink on the way only from a directory owned by
// root, the user the fake acts as, and writable by neither group nor others.
// Nothing here touches the machine.
//
// The fake has no umask, no hard links, no permission checks (it acts as
// root) and no setuid, setgid or sticky bits; ".." in a symlink target is
// resolved lexically. Paths must be absolute and must not end in a slash:
// either is refused with fs.ErrInvalid, where a host may accept them. Where
// the fake departs from a host, it fails where the host would not, never the
// other way round.
package hostfake

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"sync"

	"github.com/abbott/hardn/pkg/host"
)

// ErrUnscripted: Run was given a command no Respond entry matches and the
// fake has no Handler.
var ErrUnscripted = errors.New("unscripted command")

// Response is a scripted outcome of one command.
type Response struct {
	Stdout   string
	Stderr   string
	ExitCode int // non-zero makes Run return *host.ExitError
	// Err, when set, is returned as it is, as the legacy mocks return
	// theirs; it models a command that could not run (ExitCode -1 unless
	// set).
	Err error
	// PID is the process id StartDetached returns; 0 gives the next of a
	// counter from 1000. Stdout, Stderr and ExitCode mean nothing there: a
	// detached process is not waited for.
	PID int
}

// Call is one recorded call.
type Call struct {
	Op       string   // run, start-detached, read, read-nofollow, stat, lstat, readdir, mkdir, remove, removeall, rename, readlink, resolve-symlink, chmod, chown, write-atomic, write-in-place, append, lock, unlock, lookup-user, lookup-group
	Argv     []string // run, start-detached
	Stdin    []byte   // run: nil for Run, the input for RunInput
	ExitCode int      // run
	PID      int      // start-detached
	Path     string   // file operations and lookups
	To       string   // rename
	Data     []byte   // writes, append's data, lock's holder
	Mode     fs.FileMode
	UID, GID int // chown, write-atomic
	Err      error
}

// String renders c on one line, for test failure messages.
func (c Call) String() string {
	var b strings.Builder
	b.WriteString(c.Op + " ")
	switch c.Op {
	case "run", "start-detached":
		b.WriteString(host.FormatArgv(c.Argv))
		if c.Stdin != nil {
			fmt.Fprintf(&b, " <stdin %d bytes>", len(c.Stdin))
		}
	case "rename":
		b.WriteString(c.Path + " -> " + c.To)
	case "write-atomic":
		fmt.Fprintf(&b, "%s %04o %d:%d %d bytes", c.Path, uint32(c.Mode.Perm()), c.UID, c.GID, len(c.Data))
	case "write-in-place", "append":
		fmt.Fprintf(&b, "%s %04o %d bytes", c.Path, uint32(c.Mode.Perm()), len(c.Data))
	case "mkdir", "chmod":
		fmt.Fprintf(&b, "%s %04o", c.Path, uint32(c.Mode.Perm()))
	case "chown":
		fmt.Fprintf(&b, "%s %d:%d", c.Path, c.UID, c.GID)
	default:
		b.WriteString(c.Path)
	}
	switch {
	case c.Err != nil:
		b.WriteString(": error: " + c.Err.Error())
	case c.Op == "run":
		fmt.Fprintf(&b, ": exit %d", c.ExitCode)
	case c.Op == "start-detached":
		fmt.Fprintf(&b, ": pid %d", c.PID)
	}
	return b.String()
}

// System is the fake. The zero value is not usable; call New. It is safe for
// concurrent use.
type System struct {
	// Handler, when set, answers a command no script entry matches; stdin is
	// nil for Run. Set it before the first call. It runs without the fake's
	// lock held, so it may call the fake (Entry, AddFile) to model the
	// command's effect; calls it makes are recorded before the run, which is
	// recorded when it finishes. Without a Handler an unscripted command
	// fails with ErrUnscripted; a Handler returning Response{} gives
	// MockCommander's empty success instead.
	Handler func(argv []string, stdin []byte) Response

	mu     sync.Mutex
	nodes  map[string]*node
	users  map[string]host.User
	groups map[string]host.Group
	script map[string][]Response
	calls  []Call
	log    *host.RunLog
	tail   int
	pid    int            // the last process id StartDetached made up
	locks  map[*node]bool // the files Lock holds, as the kernel holds an open file's lock
}

var _ host.LegacySystem = (*System)(nil)

// New returns a fake with an empty root directory and no accounts.
func New(opts host.Options) *System {
	return &System{
		nodes:  map[string]*node{"/": {mode: fs.ModeDir | 0o755}},
		users:  map[string]host.User{},
		groups: map[string]host.Group{},
		script: map[string][]Response{},
		locks:  map[*node]bool{},
		log:    host.NewRunLog(opts.RunLog),
		tail:   opts.Tail(),
	}
}

func argvKey(argv []string) string { return strings.Join(argv, "\x00") }

// Respond scripts the outcome of the command name args. Responses for one
// argv are used in order; the last one repeats.
func (s *System) Respond(r Response, name string, args ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := argvKey(append([]string{name}, args...))
	s.script[k] = append(s.script[k], r)
}

// AddUser adds a user account for LookupUser.
func (s *System) AddUser(u host.User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[u.Name] = u
}

// AddGroup adds a group for LookupGroup.
func (s *System) AddGroup(g host.Group) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groups[g.Name] = g
}

// Calls returns every call so far, in order.
func (s *System) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// Runs returns the argv of every command run so far, in order.
func (s *System) Runs() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var runs [][]string
	for _, c := range s.calls {
		if c.Op == "run" {
			runs = append(runs, c.Argv)
		}
	}
	return runs
}

func (s *System) record(c Call) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, c)
}

// Run answers from the script, then the Handler; see host.System.
func (s *System) Run(ctx context.Context, name string, args ...string) (host.Result, error) {
	return s.run(ctx, nil, name, args)
}

// RunInput is Run with stdin, which is recorded and passed to the Handler.
func (s *System) RunInput(ctx context.Context, stdin []byte, name string, args ...string) (host.Result, error) {
	if stdin == nil {
		stdin = []byte{}
	}
	return s.run(ctx, stdin, name, args)
}

func (s *System) run(ctx context.Context, stdin []byte, name string, args []string) (host.Result, error) {
	argv := append([]string{name}, args...)
	stdinLen := -1
	if stdin != nil {
		stdinLen = len(stdin)
	}
	id := s.log.Begin(argv, stdinLen)
	res, err := s.answer(ctx, argv, bytes.Clone(stdin))
	s.log.End(id, res, err)
	s.record(Call{Op: "run", Argv: argv, Stdin: bytes.Clone(stdin), ExitCode: res.ExitCode, Err: err})
	return res, err
}

func (s *System) answer(ctx context.Context, argv []string, stdin []byte) (host.Result, error) {
	res := host.Result{Argv: argv, ExitCode: -1}
	if err := ctx.Err(); err != nil {
		return res, fmt.Errorf("host: run %s: %w", host.FormatArgv(argv), err)
	}
	if err := checkName(argv[0]); err != nil {
		return res, err
	}
	r, ok := s.next(argv)
	switch {
	case !ok && s.Handler == nil:
		return res, fmt.Errorf("hostfake: %s: %w", host.FormatArgv(argv), ErrUnscripted)
	case !ok:
		r = s.Handler(append([]string(nil), argv...), stdin)
	}
	res.Stdout, res.Stderr = []byte(r.Stdout), []byte(r.Stderr)
	res.Combined = []byte(r.Stdout + r.Stderr)
	res.ExitCode = r.ExitCode
	res.Tail = host.TailLines(res.Combined, s.tail)
	switch {
	case r.Err != nil:
		if r.ExitCode == 0 {
			res.ExitCode = -1
		}
		return res, r.Err
	case r.ExitCode != 0:
		return res, &host.ExitError{Argv: argv, ExitCode: r.ExitCode, Tail: res.Tail, Err: exitStatus(r.ExitCode)}
	}
	return res, nil
}

// checkName refuses the command names host.Real refuses before it looks for
// a file: an empty name and a relative path. The fake has no executables, so
// it cannot refuse a name host.Real would not find in host.SearchPath.
func checkName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("host: empty command name: %w", host.ErrNotFound)
	case strings.Contains(name, "/") && !strings.HasPrefix(name, "/"):
		return fmt.Errorf("host: %q: relative command path refused: %w", name, host.ErrNotFound)
	}
	return nil
}

// exitStatus is the error behind a scripted non-zero exit. Its text is
// os/exec's for an exit code, so the legacy adapter, which unwraps a
// host.ExitError, shows the same text over the fake as over host.Real.
type exitStatus int

func (e exitStatus) Error() string { return "exit status " + strconv.Itoa(int(e)) }

// next pops the scripted response for argv; the last one stays.
func (s *System) next(argv []string) (Response, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := argvKey(argv)
	q := s.script[k]
	if len(q) == 0 {
		return Response{}, false
	}
	if len(q) > 1 {
		s.script[k] = q[1:]
	}
	return q[0], true
}

// StartDetached answers from the script, then the Handler, as Run does, and
// starts nothing: a Response's Err is returned; otherwise it returns the
// Response's PID, or the next of a counter from 1000. The Handler gets stdin
// nil. Recorded as start-detached, and logged as host.Real logs it.
func (s *System) StartDetached(ctx context.Context, name string, args ...string) (int, error) {
	argv := append([]string{name}, args...)
	id := s.log.Begin(argv, -1)
	pid, err := s.detach(ctx, argv)
	s.log.Detached(id, pid, err)
	s.record(Call{Op: "start-detached", Argv: argv, PID: pid, Err: err})
	return pid, err
}

func (s *System) detach(ctx context.Context, argv []string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("host: start %s: %w", host.FormatArgv(argv), err)
	}
	if err := checkName(argv[0]); err != nil {
		return 0, err
	}
	r, ok := s.next(argv)
	switch {
	case !ok && s.Handler == nil:
		return 0, fmt.Errorf("hostfake: %s: %w", host.FormatArgv(argv), ErrUnscripted)
	case !ok:
		r = s.Handler(append([]string(nil), argv...), nil)
	}
	if r.Err != nil {
		return 0, r.Err
	}
	if r.PID != 0 {
		return r.PID, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pid < 1000 {
		s.pid = 999
	}
	s.pid++
	return s.pid, nil
}

// LookupUser finds an account added with AddUser.
func (s *System) LookupUser(ctx context.Context, name string) (host.User, error) {
	s.mu.Lock()
	u, ok := s.users[name]
	s.mu.Unlock()
	var err error
	switch {
	case ctx.Err() != nil:
		err = fmt.Errorf("host: lookup user %q: %w", name, ctx.Err())
	case !ok:
		err = fmt.Errorf("host: lookup user %q: %w", name, host.ErrUnknownUser)
	}
	s.record(Call{Op: "lookup-user", Path: name, Err: err})
	if err != nil {
		return host.User{}, err
	}
	return u, nil
}

// LookupGroup finds a group added with AddGroup.
func (s *System) LookupGroup(ctx context.Context, name string) (host.Group, error) {
	s.mu.Lock()
	g, ok := s.groups[name]
	s.mu.Unlock()
	var err error
	switch {
	case ctx.Err() != nil:
		err = fmt.Errorf("host: lookup group %q: %w", name, ctx.Err())
	case !ok:
		err = fmt.Errorf("host: lookup group %q: %w", name, host.ErrUnknownGroup)
	}
	s.record(Call{Op: "lookup-group", Path: name, Err: err})
	if err != nil {
		return host.Group{}, err
	}
	return g, nil
}
