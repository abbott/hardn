package backup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// Runs lists the runs under dir, a backupPath: each run directory whose
// manifest Load reads, newest first, as a run id starts with the time it
// was taken (NewRunID). It changes nothing (refactor plan slice 3,
// decision 9: Backup Runs, read only).
//
// dir must be absolute, and only root (or hardn's user) can change it and
// every directory above it (checkTrusted, a DirError otherwise), as for
// Load. A dir that is not there has no runs. An entry that is not a
// directory named as a run id is not a run; nor is a run directory with no
// manifest, where no backup was recorded (atomic.md §7). A run whose
// manifest Load refuses is left out, and its error returned, joined with
// any other, beside the runs read.
func Runs(ctx context.Context, sys host.System, dir string) ([]Manifest, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("backup: %q: %w", dir, fs.ErrInvalid)
	}
	dir = filepath.Clean(dir)
	switch err := checkTrusted(ctx, sys, dir); {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	}
	entries, err := sys.ReadDir(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	var runs []Manifest
	var errs []error
	for _, e := range entries {
		if !e.IsDir() || !validRunID(e.Name()) {
			continue
		}
		m, err := Load(ctx, sys, filepath.Join(dir, e.Name()))
		switch {
		case err == nil:
			runs = append(runs, m)
		case errors.Is(err, fs.ErrNotExist):
			// No manifest: the run recorded no backup.
		default:
			errs = append(errs, err)
		}
	}
	slices.SortStableFunc(runs, func(a, b Manifest) int { return strings.Compare(filepath.Base(b.Dir), filepath.Base(a.Dir)) })
	return runs, errors.Join(errs...)
}
