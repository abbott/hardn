// Package detect establishes the platform facts the Phase 2 safety
// primitives branch on: the distribution and its place in the support
// matrix, the init system, how sshd is started, Proxmox VE, containers and
// the elevation tool (refactor plan P2, next/p2-detect).
//
// Every detector is a function of what a host.System shows it. It reads files
// and runs read-only queries (systemctl is-enabled and is-active, dpkg-query
// --show, sudo --version); it writes nothing and starts or stops nothing. A
// fact that cannot be established is Unknown, the zero value of every fact
// type here, with the reason in its Err. Nothing falls back to a guess (the
// legacy osdetect assumed "debian 11"): a caller treats Unknown as "do not
// make a change that depends on this".
//
// A caller decides on a fact's enum and Tri fields, never on its Err alone:
// Err explains whichever of the fact's fields is Unknown, which may be a
// sub-fact (SSH.Generator, Proxmox.ClusterFS) beside an established headline
// fact.
//
// docs/safety/detect.md lists each fact's signals, the primary sources they
// rest on and what is written but not yet run on a Linux host.
package detect

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"syscall"

	"github.com/abbott/hardn/pkg/host"
)

// Tri is a fact that is true, false, or not established.
type Tri uint8

const (
	Unknown Tri = iota // not established; the zero value
	No
	Yes
)

func (t Tri) String() string {
	switch t {
	case No:
		return "no"
	case Yes:
		return "yes"
	}
	return "unknown"
}

// Errors in the Err fields, testable with errors.Is. A file or command error
// (fs.ErrPermission, host.ErrNotFound, a *host.ExitError, ctx.Err()) is
// wrapped as it came.
var (
	// ErrConflict: two signals for one fact disagree.
	ErrConflict = errors.New("signals disagree")
	// ErrUnrecognised: a file or a command's output is not in a form the
	// detector knows.
	ErrUnrecognised = errors.New("unrecognised")
	// ErrNoSignal: none of the fact's signals is present.
	ErrNoSignal = errors.New("no signal")
)

// Facts is everything Detect establishes about a host.
type Facts struct {
	OS        OSRelease
	Platform  Platform
	Init      Init
	SSH       SSH
	Proxmox   Proxmox
	Container Container
	Elevation Elevation
}

// Detect establishes every fact. Each fact that cannot be established is
// Unknown with its reason; Detect itself fails only when ctx is done, since
// then any fact may be Unknown for that reason alone.
func Detect(ctx context.Context, sys host.System) (Facts, error) {
	var f Facts
	f.OS = ReadOSRelease(ctx, sys)
	f.Proxmox = DetectProxmox(ctx, sys)
	f.Platform = Classify(f.OS, f.Proxmox)
	f.Init = DetectInit(ctx, sys)
	f.SSH = DetectSSH(ctx, sys, f.Init)
	f.Container = DetectContainer(ctx, sys)
	f.Elevation = DetectElevation(ctx, sys)
	if err := ctx.Err(); err != nil {
		return f, fmt.Errorf("detect: %w", err)
	}
	return f, nil
}

// stat reports whether name exists: Yes with its FileInfo, No when it or a
// parent is missing, Unknown with the error otherwise (a permission error, a
// done ctx).
func stat(ctx context.Context, sys host.System, name string) (fs.FileInfo, Tri, error) {
	return exists(sys.Stat(ctx, name))
}

// lstat is stat without following a symlink at name.
func lstat(ctx context.Context, sys host.System, name string) (fs.FileInfo, Tri, error) {
	return exists(sys.Lstat(ctx, name))
}

func exists(fi fs.FileInfo, err error) (fs.FileInfo, Tri, error) {
	switch {
	case err == nil:
		return fi, Yes, nil
	case missing(err):
		return nil, No, nil
	}
	return nil, Unknown, err
}

// isDir is stat for a directory: an existing non-directory is No.
func isDir(ctx context.Context, sys host.System, name string) (Tri, error) {
	fi, t, err := stat(ctx, sys, name)
	if t == Yes && !fi.IsDir() {
		return No, nil
	}
	return t, err
}

// missing reports an error that means the path does not exist: ENOENT, or a
// parent that is not a directory.
func missing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// firstLine is the first line of out, without surrounding white space.
func firstLine(out []byte) string {
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line)
}
