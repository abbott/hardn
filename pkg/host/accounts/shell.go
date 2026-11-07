package accounts

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"slices"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// NoShells are the base names of the shells that give no shell to run a
// command from: nologin, false and true exit, git-shell runs git's
// commands only.
var NoShells = []string{"nologin", "false", "true", "git-shell"}

// LoginShell is whether shell lets its user log in: not one of NoShells,
// and a shell sshd lets in (SSHDShell). An empty shell is /bin/sh
// (passwd(5)). Any other program is taken for a shell. A Stat that fails
// otherwise than for the file's absence is Unknown, with Stat's error as
// it is (the User row asks it).
func LoginShell(ctx context.Context, sys host.System, shell string) (detect.Tri, error) {
	if shell == "" {
		shell = "/bin/sh"
	}
	if slices.Contains(NoShells, path.Base(shell)) {
		return detect.No, nil
	}
	return SSHDShell(ctx, sys, shell)
}

// SSHDShell is whether sshd lets in an account with shell: an executable
// regular file (auth.c allowed_user(): stat, S_ISREG and an execute bit
// [I]), NoShells' among them, which run no command but leave sshd's
// forwarding to an account that authenticated (SSH Auth's CVE-2026-123158
// check asks it). An empty shell is /bin/sh (passwd(5)); a relative one is
// not let in. A Stat that fails otherwise than for the file's absence is
// Unknown, with Stat's error as it is.
func SSHDShell(ctx context.Context, sys host.System, shell string) (detect.Tri, error) {
	if shell == "" {
		shell = "/bin/sh"
	}
	if !path.IsAbs(shell) {
		return detect.No, nil
	}
	fi, err := sys.Stat(ctx, shell)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return detect.No, nil
	case err != nil:
		return detect.Unknown, err
	case fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0:
		return detect.Yes, nil
	}
	return detect.No, nil
}
