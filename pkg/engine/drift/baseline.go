package drift

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/revert"
)

// Format is the version of the baseline's document. A change to what a
// fact means, to an id, or to how an item is written is a new one: a
// baseline of another format is never compared (ErrFormat), since every
// difference it showed would be the format's.
const Format = 1

// BaselineName is the baseline's file in hardn's state directory
// (revert.DefaultDir): /var/lib/hardn/baseline.json.
const BaselineName = "baseline.json"

// MaxBaseline is the most of the file Load reads, in bytes: 4 MiB.
const MaxBaseline = 4 << 20

// Path is the baseline's path in production.
var Path = filepath.Join(revert.DefaultDir, BaselineName)

// The refusals of Load.
var (
	// ErrNoBaseline: no baseline was saved on this host.
	ErrNoBaseline = errors.New("no baseline")
	// ErrUnsafe: the baseline, or a directory above it, is not root's
	// alone, or is no regular file: it is not read.
	ErrUnsafe = errors.New("the baseline is not root's alone")
	// ErrInvalid: the file is not a baseline hardn wrote: too large, not
	// its JSON, or facts it does not hold as Save writes them.
	ErrInvalid = errors.New("the baseline is not one hardn wrote")
	// ErrFormat: the baseline is of another version of the format.
	ErrFormat = errors.New("the baseline is of another version of its format")
)

// Baseline is the saved document.
type Baseline struct {
	Format       int       `json:"format"`
	HardnVersion string    `json:"hardnVersion"`
	Saved        time.Time `json:"saved"` // UTC
	// User saved it: who ran hardn check --accept, as the audit log has it.
	User  string `json:"user"`
	Facts Facts  `json:"facts"`
}

// Save writes b as the baseline of the state directory of opts, the
// production one for the zero value: the directory made 0700 where
// missing and checked as the journal's is, the file root's, 0600, written
// to a temporary file and renamed (revert.WriteStateFile). It writes
// nothing else.
func Save(ctx context.Context, sys host.System, opts revert.Options, b Baseline) error {
	b.Format = Format
	if err := b.check(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("drift: %w", err)
	}
	if err := revert.WriteStateFile(ctx, sys, opts, BaselineName, append(data, '\n')); err != nil {
		return fmt.Errorf("drift: %w", err)
	}
	return nil
}

// Load reads the baseline under the journal's trust checks
// (revert.ReadStateFile: the directories root's and writable by no one
// else, the file a regular file, not a symlink, root's and writable by no
// one else, at most MaxBaseline), then decodes it strictly: one JSON
// value, no field it does not know, every fact of Specs once and in
// order, each in its form. No file is ErrNoBaseline; an unsafe one
// ErrUnsafe; another format's ErrFormat, told before anything else of it
// is read; any other content ErrInvalid. It writes nothing.
func Load(ctx context.Context, sys host.System, opts revert.Options) (Baseline, error) {
	data, err := revert.ReadStateFile(ctx, sys, opts, BaselineName, MaxBaseline)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Baseline{}, fmt.Errorf("drift: %w", ErrNoBaseline)
	case errors.Is(err, revert.ErrUnsafe):
		return Baseline{}, fmt.Errorf("drift: %w: %w", ErrUnsafe, err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return Baseline{}, fmt.Errorf("drift: %w", err)
	case err != nil:
		return Baseline{}, fmt.Errorf("drift: %w: %w", ErrInvalid, err)
	}
	var v struct {
		Format *int `json:"format"`
	}
	if err := json.Unmarshal(data, &v); err != nil || v.Format == nil {
		return Baseline{}, fmt.Errorf("drift: %w: not its JSON", ErrInvalid)
	}
	if *v.Format != Format {
		return Baseline{}, fmt.Errorf("drift: %w: it has format %d, this hardn reads format %d", ErrFormat, *v.Format, Format)
	}
	var b Baseline
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return Baseline{}, fmt.Errorf("drift: %w: %w", ErrInvalid, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Baseline{}, fmt.Errorf("drift: %w: more than one JSON value", ErrInvalid)
	}
	if err := b.check(); err != nil {
		return Baseline{}, err
	}
	return b, nil
}

// check is b's form: its format, a time, and the facts of Specs, each
// once and in order, a value with no items, a set's items sorted and
// each once (each once alone for a set with an Order), a fact not
// established with neither.
func (b Baseline) check() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("drift: %w: %s", ErrInvalid, fmt.Sprintf(format, args...))
	}
	switch {
	case b.Format != Format:
		return bad("format %d", b.Format)
	case b.Saved.IsZero():
		return bad("no time saved")
	case len(b.Facts) != len(Specs):
		return bad("%d facts, not %d", len(b.Facts), len(Specs))
	}
	for i, sp := range Specs {
		f := b.Facts[i]
		switch {
		case f.ID != sp.ID:
			return bad("fact %d is not %s", i+1, sp.ID)
		case !f.Known && (f.Value != "" || len(f.Items) > 0):
			return bad("%s is not established and has a value", sp.ID)
		case f.NotCompared && f.Known:
			return bad("%s is established and not compared", sp.ID)
		case sp.Set() && f.Value != "", !sp.Set() && len(f.Items) > 0:
			return bad("%s is not in its form", sp.ID)
		case sp.Order != "" && len(once(f.Items)) != len(f.Items):
			return bad("%s has an item twice", sp.ID)
		case sp.Order == "" && (!slices.IsSorted(f.Items) || len(slices.Compact(slices.Clone(f.Items))) != len(f.Items)):
			return bad("%s's items are not sorted, each once", sp.ID)
		}
	}
	return nil
}

// Kept reports whether the host has a baseline: anything at its path in
// the state directory of opts. It only looks (one Lstat), so that a
// change's report can say to accept the host again (decision 2).
func Kept(ctx context.Context, sys host.System, opts revert.Options) bool {
	dir := opts.Dir
	if dir == "" {
		dir = revert.DefaultDir
	}
	_, err := sys.Lstat(ctx, filepath.Join(dir, BaselineName))
	return err == nil
}
