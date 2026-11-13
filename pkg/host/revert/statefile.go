package revert

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/abbott/hardn/pkg/host"
)

// errTooLarge is a state file larger than its reader allows. It reaches no
// engine run, only the release check, so it has no output code.
var errTooLarge = errors.New("larger than allowed")

// ownFile is name in opts' state directory. name is a file's bare name,
// such as "release.json"; another is fs.ErrInvalid.
func ownFile(opts Options, name string) (string, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return "", fmt.Errorf("revert: state file %q: %w", name, fs.ErrInvalid)
	}
	return filepath.Join(opts.dir(), name), nil
}

// ReadStateFile reads name, a file of the state directory (Options.Dir)
// that is hardn's own and not the journal's, such as the release check's
// cache, under the journal's trust checks: the directory and every one
// above it root's and writable by no one else (stateDir), and the file a
// regular file, not a symlink, root's and writable by no one else
// (ErrUnsafe). A file larger than limit bytes is errTooLarge, told from
// its size before it is read. No directory or no file is fs.ErrNotExist.
// What it returns is still untrusted content: the caller decodes it
// strictly.
func ReadStateFile(ctx context.Context, sys host.System, opts Options, name string, limit int64) ([]byte, error) {
	path, err := ownFile(opts, name)
	if err != nil {
		return nil, err
	}
	if err := stateDir(ctx, sys, opts.dir(), false); err != nil {
		return nil, err
	}
	fi, err := sys.Lstat(ctx, path)
	switch {
	case err != nil:
		return nil, fmt.Errorf("revert: %w", err)
	case !fi.Mode().IsRegular() || !trusted(fi):
		return nil, fmt.Errorf("revert: %s: %w", path, ErrUnsafe)
	case fi.Size() > limit:
		return nil, fmt.Errorf("revert: %s: %w", path, errTooLarge)
	}
	// The file as opened, in case it was replaced since the Lstat.
	data, fi, err := sys.ReadFileNoFollow(ctx, path)
	switch {
	case err != nil:
		return nil, fmt.Errorf("revert: %w", err)
	case !trusted(fi):
		return nil, fmt.Errorf("revert: %s: %w", path, ErrUnsafe)
	case int64(len(data)) > limit:
		return nil, fmt.Errorf("revert: %s: %w", path, errTooLarge)
	}
	return data, nil
}

// WriteStateFile writes data to name in the state directory, made 0700
// where missing and checked as ReadStateFile checks it: atomically (a
// temporary file in the directory, renamed), 0600, hardn's user's (root).
// It replaces whatever is at name, a symlink or an empty directory too.
func WriteStateFile(ctx context.Context, sys host.System, opts Options, name string, data []byte) error {
	path, err := ownFile(opts, name)
	if err != nil {
		return err
	}
	if err := stateDir(ctx, sys, opts.dir(), true); err != nil {
		return err
	}
	// What is there and is no regular file, a symlink say, is removed
	// itself, never followed: WriteFileAtomic refuses to replace one.
	if fi, err := sys.Lstat(ctx, path); err == nil && !fi.Mode().IsRegular() {
		if err := sys.Remove(ctx, path); err != nil {
			return fmt.Errorf("revert: %w", err)
		}
	}
	if err := sys.WriteFileAtomic(ctx, path, data, 0o600, -1, -1); err != nil {
		return fmt.Errorf("revert: %w", err)
	}
	return nil
}
