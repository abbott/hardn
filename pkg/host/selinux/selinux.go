// Package selinux reads SELinux's mode and compares files' labels with the
// policy's defaults, for the checks hardn makes before a change on a host
// where SELinux enforces (Rocky Linux, AlmaLinux: docs/engine/el-changes.md).
// It changes nothing.
package selinux

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// EnforceFile is selinuxfs's mode: "1" enforcing, "0" permissive; absent
// when SELinux is disabled.
const EnforceFile = "/sys/fs/selinux/enforce"

// Matchpathcon is the command Mislabeled runs: -V compares each path's
// label with the policy's default and says "<path> verified." or "<path>
// has context <got>, should be <want>" (libselinux-utils; read on
// rocky10-test-110).
var Matchpathcon = []string{"matchpathcon", "-V"}

// ErrMislabeled: a file's label is not the policy's default, so a confined
// program (sshd) may be refused it.
var ErrMislabeled = errors.New("SELinux labels differ from the policy's")

// ErrNotRunnable: hardn's binary is of an SELinux type systemd does not
// run, so the revert timer and the boot unit would never start it.
var ErrNotRunnable = errors.New("systemd may not run a file of this SELinux type")

// RunnableType is the type of hardn's binary that systemd ran on
// rocky10-test-110 (a copy of /usr/bin/true installed in /usr/local/bin);
// admin_home_t (in /root, the policy's own label there) and user_tmp_t
// (moved from /tmp) failed with exit 203. Only it is let through.
const RunnableType = "bin_t"

// Stat is the command Type runs: coreutils' stat, the file's security
// context as "user:role:type:level".
var Stat = []string{"stat", "-c", "%C"}

// Type is path's SELinux type, the third field of its context (Stat).
func Type(ctx context.Context, sys host.System, path string) (string, error) {
	res, err := sys.Run(ctx, Stat[0], append(Stat[1:], "--", path)...)
	if err != nil {
		return "", fmt.Errorf("selinux: %w", err)
	}
	c := strings.TrimSpace(string(res.Stdout))
	f := strings.Split(c, ":")
	if len(f) < 4 || f[2] == "" {
		return "", fmt.Errorf("selinux: stat -c %%C %s printed %q: %w", path, c, detect.ErrUnrecognised)
	}
	return f[2], nil
}

// CheckRunnable is nil where SELinux does not enforce, or where path's
// type is RunnableType; ErrNotRunnable naming the type otherwise, and the
// error where the mode or the type is not established. It asks what the
// file is, not what the policy would label it: the policy labels /root's
// files admin_home_t, which systemd does not run.
func CheckRunnable(ctx context.Context, sys host.System, path string) error {
	switch on, err := Enforcing(ctx, sys); {
	case err != nil:
		return err
	case on != detect.Yes:
		return nil
	}
	t, err := Type(ctx, sys, path)
	switch {
	case err != nil:
		return err
	case t != RunnableType:
		return fmt.Errorf("selinux: %s is %s, not %s: %w", path, t, RunnableType, ErrNotRunnable)
	}
	return nil
}

// Enforcing reports whether SELinux enforces: Yes for EnforceFile "1", No
// for "0" (permissive: denials are logged, not made) or no selinuxfs, and
// Unknown with the error otherwise.
func Enforcing(ctx context.Context, sys host.System) (detect.Tri, error) {
	data, err := sys.ReadFile(ctx, EnforceFile)
	switch v := strings.TrimSpace(string(data)); {
	case errors.Is(err, fs.ErrNotExist):
		return detect.No, nil
	case err != nil:
		return detect.Unknown, fmt.Errorf("selinux: %w", err)
	case v == "1":
		return detect.Yes, nil
	case v == "0":
		return detect.No, nil
	default:
		return detect.Unknown, fmt.Errorf("selinux: %s: %q: %w", EnforceFile, data, detect.ErrUnrecognised)
	}
}

// Mislabeled returns matchpathcon -V's line for each of paths whose label
// differs from the policy's default. A path the policy gives no default
// ("should be <<none>>") is not mislabeled. A line in no form it knows, or
// matchpathcon failing to run, is an error.
func Mislabeled(ctx context.Context, sys host.System, paths ...string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	res, err := sys.Run(ctx, Matchpathcon[0], append(Matchpathcon[1:], paths...)...)
	var xe *host.ExitError
	if err != nil && !errors.As(err, &xe) {
		return nil, fmt.Errorf("selinux: %w", err)
	}
	var bad []string
	seen := 0
	for _, l := range strings.Split(string(res.Stdout), "\n") {
		switch l = strings.TrimSpace(l); {
		case l == "":
			continue
		case strings.HasSuffix(l, " verified."):
		case strings.Contains(l, " has context ") && strings.HasSuffix(l, ", should be <<none>>"):
		case strings.Contains(l, " has context ") && strings.Contains(l, ", should be "):
			bad = append(bad, l)
		default:
			return nil, fmt.Errorf("selinux: matchpathcon -V printed %q: %w", l, detect.ErrUnrecognised)
		}
		seen++
	}
	if seen != len(paths) {
		return nil, fmt.Errorf("selinux: matchpathcon -V answered %d of %d paths (exit %d, %q): %w", seen, len(paths), res.ExitCode,
			strings.TrimSpace(string(res.Stderr)), detect.ErrUnrecognised)
	}
	return bad, nil
}

// Check is nil where SELinux does not enforce, or where every path has the
// policy's label; ErrMislabeled naming the paths otherwise. Where whether
// SELinux enforces, or a label, is not established, it is that error: the
// caller refuses, as for a mislabeled file.
func Check(ctx context.Context, sys host.System, paths ...string) error {
	switch on, err := Enforcing(ctx, sys); {
	case err != nil:
		return err
	case on != detect.Yes:
		return nil
	}
	bad, err := Mislabeled(ctx, sys, paths...)
	if err != nil {
		return err
	}
	if len(bad) > 0 {
		return fmt.Errorf("selinux: %s: %w", strings.Join(bad, "; "), ErrMislabeled)
	}
	return nil
}
