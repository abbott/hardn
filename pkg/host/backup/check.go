package backup

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/abbott/hardn/pkg/host"
)

// Check reports, changing nothing, what Open(sys, s, runID) and a Backup of
// each of paths in that run would refuse that can be seen beforehand
// (refactor plan P3, next/p3-engine: the engine refuses at plan time, so
// that a dry run shows these rather than an apply that fails once its
// revert timer is armed):
//
//   - the settings Open refuses (ErrSettings);
//   - a run directory that exists already (ErrRunExists);
//   - the deepest directory of the run directory's path that exists, which
//     prepare would create the rest under, refused by checkTrusted: another
//     user can change it (ErrUnsafeDir), or it is a file (syscall.ENOTDIR),
//     each a DirError naming it;
//   - a path that is not absolute and clean (fs.ErrInvalid), or that
//     Backup's inspection refuses: a directory or a special file
//     (ErrNotRegular), a symlink in a directory another user can change
//     (host.ErrSymlinkParent).
//
// It reads each path's content, as Backup does. What changes between Check
// and the Backup is refused by Backup then.
func Check(ctx context.Context, sys host.System, s Settings, runID string, paths []string) error {
	r, err := Open(sys, s, runID)
	if err != nil {
		return err
	}
	top, _, err := deepest(ctx, sys, r.m.Dir)
	if err != nil {
		return err
	}
	if top == r.m.Dir {
		return fmt.Errorf("backup: %s: %w", r.m.Dir, ErrRunExists)
	}
	if err := checkTrusted(ctx, sys, top); err != nil {
		return err
	}
	for _, p := range paths {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return fmt.Errorf("backup: %q: %w", p, fs.ErrInvalid)
		}
		if _, _, err := inspect(ctx, sys, p); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}
	return nil
}
