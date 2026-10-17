package interfaces

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/abbott/hardn/pkg/host"
)

// legacyThroughHost routes OSCommander and OSFileSystem through host.Real
// (refactor plan P2, next/p2-system), so legacy and new code share one
// execution path: commands resolved only from host.SearchPath, with the
// environment pinned to PATH and LC_ALL=C, from the working directory "/".
// Flip to false to restore the os/exec and os calls they made before, kept in
// their method bodies, if a host run shows a regression from the pinned
// environment.
const legacyThroughHost = true

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

// ExecuteWithInput runs command with args and input on stdin.
func (c SystemCommander) ExecuteWithInput(input string, command string, args ...string) ([]byte, error) {
	res, err := c.Sys.RunInput(context.Background(), []byte(input), command, args...)
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

func (f SystemFileSystem) WriteFile(filename string, data []byte, perm fs.FileMode) error {
	ctx := context.Background()
	if dir := filepath.Dir(filename); dir != "." {
		if err := f.Sys.MkdirAll(ctx, dir, 0755); err != nil {
			return err
		}
	}
	return f.Sys.WriteFileInPlace(ctx, filename, data, perm)
}

func (f SystemFileSystem) MkdirAll(path string, perm fs.FileMode) error {
	return f.Sys.MkdirAll(context.Background(), path, perm)
}

func (f SystemFileSystem) Stat(name string) (os.FileInfo, error) {
	return f.Sys.Stat(context.Background(), name)
}

func (f SystemFileSystem) Remove(name string) error {
	return f.Sys.Remove(context.Background(), name)
}

func (f SystemFileSystem) RemoveAll(path string) error {
	return f.Sys.RemoveAll(context.Background(), path)
}
