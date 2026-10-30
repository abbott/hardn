package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"sync"

	"github.com/abbott/hardn/pkg/host"
)

// errRefused is a call the guard refused: a method that changes the host,
// or a command that is not allowed. It is p4status's own, not hardn's: no
// code of pkg/output's registry names it (a report's error that wraps it
// shows as UNCLASSIFIED_ERROR), and the evidence lists every refusal.
// Its words are proposed (docs/engine/vmcheck.md §5.2): the method and
// the reason before it say the rest.
var errRefused = errors.New("refused")

// decision is what the guard does with one method of host.System.
type decision uint8

const (
	// pass: the method only reads; it is passed through, but for a
	// special file given to ReadFile or ReadDir (guard.special).
	pass decision = iota
	// refuse: the method changes the host, or may (a lock creates its
	// file, a detached process outlives the run); it is refused, with no
	// call made.
	refuse
	// allow: the method runs a command; only an allowed one runs
	// (allowed, allow.go).
	allow
)

// decisions is every method of host.System and what the guard does with
// it. TestGuardDecidesEveryMethod fails for a method of host.System this
// table does not name, and for a method that does not do what it says;
// the guard does not embed a System, so a method added to host.System
// does not build until the guard has it.
var decisions = map[string]decision{
	"ReadFile": pass, "ReadFileNoFollow": pass, "ScanLines": pass, "Stat": pass, "Lstat": pass, "ReadDir": pass,
	"Readlink": pass, "ResolveSymlink": pass, "LookupUser": pass, "LookupGroup": pass,
	"Run": allow,
	// RunInput: no check or probe gives a command input; StartDetached:
	// nothing they start may outlive the run.
	"RunInput": refuse, "StartDetached": refuse,
	"MkdirAll": refuse, "Remove": refuse, "RemoveAll": refuse, "Rename": refuse, "Chmod": refuse,
	"Chown": refuse, "WriteFileAtomic": refuse, "AppendFile": refuse, "Lock": refuse,
}

// guard is the host as p4status reaches it: a host.System over another
// (host.Real on a host, hostfake in the tests) that passes the reads
// through, refuses every method that changes anything, and runs only the
// allowed commands. It records every command it runs and every call it
// refuses, in order, each with the part of the reading that made it, for
// the evidence. It is safe for concurrent use.
type guard struct {
	sys  host.System
	mu   sync.Mutex
	log  []call
	part string // the part of the reading under way (within)
}

var _ host.System = (*guard)(nil)

// call is one command run, or one call refused.
type call struct {
	part   string   // the part of the reading that made it (guard.within)
	method string   // the host.System method
	argv   []string // a command's
	path   string   // a refused file operation's first path
	exit   int      // a command's exit status, -1 when it did not run or ended by a signal
	err    error    // the command's error, or the refusal
	why    string   // why it was refused
}

// refused reports whether the guard refused c.
func (c call) refused() bool { return errors.Is(c.err, errRefused) }

func newGuard(sys host.System) *guard { return &guard{sys: sys} }

// The parts of a reading that are not a status row's control, whose part
// is its ID (read).
const (
	partConfig = "hardn.yml"
	partDetect = "detect"
	partHeader = "header"
)

// within makes part, a control's ID, partConfig, partDetect or partHeader, the part
// of the reading the calls from now on are recorded as. The reading is
// sequential (Engine.Check checks one control at a time), so a refused
// call is the part's whose check made it, even when the check carries on
// without the answer and does not report the error (a check that
// tolerates a failing ss, check-ssh.md §2.8a).
func (g *guard) within(part string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.part = part
}

func (g *guard) record(c call) {
	g.mu.Lock()
	defer g.mu.Unlock()
	c.part = g.part
	g.log = append(g.log, c)
}

// calls returns every command run and every call refused, in order.
func (g *guard) calls() []call {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.log)
}

// refusals counts the calls refused.
func (g *guard) refusals() int {
	n := 0
	for _, c := range g.calls() {
		if c.refused() {
			n++
		}
	}
	return n
}

// deny records method on path refused, for why, and returns its error.
func (g *guard) deny(method, path, why string) error {
	err := fmt.Errorf("p4status: %s %s: %s: %w", method, path, why, errRefused)
	g.record(call{method: method, path: path, exit: -1, err: err, why: why})
	return err
}

// denyRun records argv refused and returns its result and error, as
// host.Real returns a command that did not start.
func (g *guard) denyRun(method string, argv []string, why string) (host.Result, error) {
	err := fmt.Errorf("p4status: %s: %s: %w", host.FormatArgv(argv), why, errRefused)
	g.record(call{method: method, argv: argv, exit: -1, err: err, why: why})
	return host.Result{Argv: argv, ExitCode: -1}, err
}

// hold is the engine's signal hold (engine.Options.Hold), which only an
// apply takes: taken, it is a refusal.
func (g *guard) hold() (release func()) {
	_ = g.deny("Hold", "the engine's signal hold", "only an apply takes it")
	return func() {}
}

// Run runs name with args when they are allowed (allowed), and refuses
// them otherwise.
func (g *guard) Run(ctx context.Context, name string, args ...string) (host.Result, error) {
	argv := append([]string{name}, args...)
	if !allowed(argv) {
		return g.denyRun("Run", argv, "not a command the checks run")
	}
	res, err := g.sys.Run(ctx, name, args...)
	g.record(call{method: "Run", argv: argv, exit: res.ExitCode, err: err})
	return res, err
}

// RunInput is refused.
func (g *guard) RunInput(_ context.Context, _ []byte, name string, args ...string) (host.Result, error) {
	return g.denyRun("RunInput", append([]string{name}, args...), "a command with input")
}

// StartDetached is refused.
func (g *guard) StartDetached(_ context.Context, name string, args ...string) (int, error) {
	_, err := g.denyRun("StartDetached", append([]string{name}, args...), "a detached process")
	return 0, err
}

// The reads, passed through.

// notOpened is why a special file is refused (proposed words,
// docs/engine/vmcheck.md §5.2).
const notOpened = "a device, FIFO or socket"

// special reports whether name, followed through symlinks as ReadFile and
// ReadDir follow it, exists and is neither a regular file nor a directory:
// a device, a FIFO or a socket. ReadFile and ReadDir open what they are
// given (os.ReadFile, os.ReadDir) and are refused one, since opening it can
// change the host (a watchdog device starts its timer when opened) or
// never return (a FIFO with no writer). The other reads open nothing
// (Stat, Lstat, Readlink), or look before they open (ReadFileNoFollow,
// ScanLines, ResolveSymlink), or read the account files (LookupUser,
// LookupGroup).
// A file swapped for a special one between this look and the open is not
// caught: only root can swap the files the checks read.
func (g *guard) special(ctx context.Context, name string) bool {
	fi, err := g.sys.Stat(ctx, name)
	return err == nil && !fi.Mode().IsRegular() && !fi.IsDir()
}

// ReadFile is the host's, for what is not special.
func (g *guard) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if g.special(ctx, name) {
		return nil, g.deny("ReadFile", name, notOpened)
	}
	return g.sys.ReadFile(ctx, name)
}

func (g *guard) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	return g.sys.ReadFileNoFollow(ctx, name)
}

func (g *guard) ScanLines(ctx context.Context, name string, yield func(host.Line) bool) (fs.FileInfo, error) {
	return g.sys.ScanLines(ctx, name, yield)
}

func (g *guard) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	return g.sys.Stat(ctx, name)
}

func (g *guard) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	return g.sys.Lstat(ctx, name)
}

// ReadDir is the host's, for what is not special.
func (g *guard) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	if g.special(ctx, name) {
		return nil, g.deny("ReadDir", name, notOpened)
	}
	return g.sys.ReadDir(ctx, name)
}

func (g *guard) Readlink(ctx context.Context, name string) (string, error) {
	return g.sys.Readlink(ctx, name)
}

func (g *guard) ResolveSymlink(ctx context.Context, name string) (string, string, error) {
	return g.sys.ResolveSymlink(ctx, name)
}

func (g *guard) LookupUser(ctx context.Context, name string) (host.User, error) {
	return g.sys.LookupUser(ctx, name)
}

func (g *guard) LookupGroup(ctx context.Context, name string) (host.Group, error) {
	return g.sys.LookupGroup(ctx, name)
}

// The changes, refused.

// changes is why a change is refused.
const changes = "it changes the host"

func (g *guard) MkdirAll(_ context.Context, name string, _ fs.FileMode) error {
	return g.deny("MkdirAll", name, changes)
}

func (g *guard) Remove(_ context.Context, name string) error { return g.deny("Remove", name, changes) }

func (g *guard) RemoveAll(_ context.Context, name string) error {
	return g.deny("RemoveAll", name, changes)
}

func (g *guard) Rename(_ context.Context, oldname, newname string) error {
	return g.deny("Rename", oldname+" -> "+newname, changes)
}

func (g *guard) Chmod(_ context.Context, name string, _ fs.FileMode) error {
	return g.deny("Chmod", name, changes)
}

func (g *guard) Chown(_ context.Context, name string, _, _ int) error {
	return g.deny("Chown", name, changes)
}

func (g *guard) WriteFileAtomic(_ context.Context, name string, _ []byte, _ fs.FileMode, _, _ int) error {
	return g.deny("WriteFileAtomic", name, changes)
}

func (g *guard) AppendFile(_ context.Context, name string, _ []byte, _ fs.FileMode) error {
	return g.deny("AppendFile", name, changes)
}

func (g *guard) Lock(_ context.Context, name string, _ []byte) (func() error, error) {
	return nil, g.deny("Lock", name, changes)
}
