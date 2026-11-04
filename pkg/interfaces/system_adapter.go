package interfaces

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/abbott/hardn/pkg/host"
)

// legacyThroughHost, when true, routes OSCommander and OSFileSystem through
// host.Real (refactor plan P2, next/p2-system), so legacy and new code share
// one execution path: commands resolved only from host.SearchPath, with the
// environment pinned to PATH and LC_ALL=C, from the working directory "/".
//
// OFF (2026-10-17): with it on, every legacy production command changes in
// the ways docs/safety/system.md §5 lists, and none of them is approved yet
// (brief §3 principle 2). Proposal P-32 (docs/refactor/proposals.md) asks
// the maintainer; flip to true once the maintainer approves it, after next/p2-vmtest has
// run the legacy operations on the VMs. Off, OSCommander and OSFileSystem
// make the os/exec and os calls they made before, and the adapters below are
// still what the through-System characterization goldens test.
const legacyThroughHost = false

// realSystem is the one host.Real the legacy seams share. Its run log is
// discarded until p3-output gives it a path.
var realSystem = sync.OnceValue(func() *host.Real { return host.NewReal(host.Options{}) })

// SystemCommander is the legacy Commander over a host.System: it returns
// stdout and stderr interleaved, as exec.Cmd.CombinedOutput does, and the
// os/exec error behind a non-zero exit, so legacy callers see what they saw
// before.
type SystemCommander struct {
	Sys host.System
}

var _ Commander = SystemCommander{}

// Execute runs command with args and empty stdin.
func (c SystemCommander) Execute(command string, args ...string) ([]byte, error) {
	res, err := c.Sys.Run(context.Background(), command, args...)
	return res.Combined, legacyError(err)
}

// legacyError unwraps a host.ExitError to the os/exec error it carries, whose
// text ("exit status 1") legacy callers print.
func legacyError(err error) error {
	var xe *host.ExitError
	if errors.As(err, &xe) && xe.Err != nil {
		return xe.Err
	}
	return err
}

// SystemFileSystem is the legacy FileSystem over a host.LegacySystem, with
// OSFileSystem's semantics: WriteFile creates the parent directory (0755)
// and writes in place, through a symlink.
type SystemFileSystem struct {
	Sys host.LegacySystem
}

var _ FileSystem = SystemFileSystem{}

func (f SystemFileSystem) ReadFile(filename string) ([]byte, error) {
	return f.Sys.ReadFile(context.Background(), filename)
}

func (f SystemFileSystem) Stat(name string) (os.FileInfo, error) {
	return f.Sys.Stat(context.Background(), name)
}
