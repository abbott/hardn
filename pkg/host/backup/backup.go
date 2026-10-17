// Package backup keeps copies of the files a run of hardn is about to change,
// with a manifest, so that the change can be undone (refactor plan §3.16,
// next/p2-atomic).
//
// Each run gets its own directory, <backupPath>/<run-id>/, created on the
// first backup taken. It holds the copies and manifest.json, which records
// for every path what it was (a regular file, a symlink, or nothing) with the
// mode, owner, size and SHA-256 of its content. Restore puts the paths back
// from a manifest; p2-revert's rollback calls it.
//
// The enableBackups and backupPath keys are honoured (Settings): with
// enableBackups false an Optional backup is skipped, but a RollbackRequired
// one is taken anyway, and the Result and the manifest say so.
//
// Everything goes through host.System. Files are read with ReadFileNoFollow
// and written with WriteFileAtomic, so a copy's content, mode and owner come
// from one file, and neither backup nor restore writes through a symlink a
// user planted. The backup directory must be one only root (or hardn's own
// user) can change, since a rollback writes its copies back as root.
//
// docs/safety/atomic.md describes the guarantees and what is written but not
// yet run on a Linux host.
package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/abbott/hardn/pkg/host"
)

const (
	// DefaultPath is backupPath's default (config.DefaultConfig).
	DefaultPath = "/var/backups/hardn"
	// ManifestName is the manifest's file name in a run directory.
	ManifestName = "manifest.json"
	// SchemaVersion is the manifest format this package writes and reads.
	SchemaVersion = 1
)

// Errors, testable with errors.Is.
var (
	ErrSettings   = errors.New("invalid backup settings")
	ErrRunExists  = errors.New("backup run directory already exists")
	ErrUnsafeDir  = errors.New("directory another user can change")
	ErrNotRegular = errors.New("neither a regular file nor a symlink")
	ErrManifest   = errors.New("invalid backup manifest")
	ErrCorrupt    = errors.New("backup copy does not match its manifest")
	ErrChanged    = errors.New("changed since the backup in a way restore does not undo")
)

// Seams for the tests in this package.
var (
	now     = time.Now
	geteuid = os.Geteuid
)

// Settings are the backupPath and enableBackups configuration keys.
type Settings struct {
	Path    string // backupPath: an absolute directory
	Enabled bool   // enableBackups
}

// Need says whether a change's rollback reads the backup.
type Need uint8

const (
	// Optional: taken only when enableBackups is true.
	Optional Need = iota
	// RollbackRequired: the change's rollback restores from it, so it is
	// taken whatever enableBackups says (§3.16).
	RollbackRequired
)

// Outcome is what Backup did.
type Outcome uint8

const (
	Skipped     Outcome = iota + 1 // enableBackups is false and the backup was Optional
	Taken                          // enableBackups is true
	TakenAnyway                    // enableBackups is false, but the rollback needs it
)

func (o Outcome) String() string {
	switch o {
	case Skipped:
		return "skipped: enableBackups is false"
	case Taken:
		return "taken"
	case TakenAnyway:
		return "taken although enableBackups is false: the rollback needs it"
	}
	return "unknown"
}

// Kind is what a path was when it was backed up.
type Kind string

const (
	KindFile    Kind = "file"    // a regular file
	KindAbsent  Kind = "absent"  // nothing: restore removes the file the change created
	KindSymlink Kind = "symlink" // a symlink: restore puts back the content it points to
)

// Perm is a mode as chmod(1) writes it, in octal with the setuid (04000),
// setgid (02000) and sticky (01000) bits; "0644" in JSON.
type Perm uint32

// PermOf returns the Perm of m.
func PermOf(m fs.FileMode) Perm {
	p := Perm(m.Perm())
	for bit, mode := range specialBits {
		if m&mode != 0 {
			p |= bit
		}
	}
	return p
}

var specialBits = map[Perm]fs.FileMode{0o4000: fs.ModeSetuid, 0o2000: fs.ModeSetgid, 0o1000: fs.ModeSticky}

// FileMode returns p as an fs.FileMode.
func (p Perm) FileMode() fs.FileMode {
	m := fs.FileMode(p & 0o777)
	for bit, mode := range specialBits {
		if p&bit != 0 {
			m |= mode
		}
	}
	return m
}

// MarshalText writes p in octal.
func (p Perm) MarshalText() ([]byte, error) { return fmt.Appendf(nil, "%04o", uint32(p)), nil }

// UnmarshalText reads an octal mode no greater than 07777.
func (p *Perm) UnmarshalText(b []byte) error {
	v, err := strconv.ParseUint(string(b), 8, 32)
	if err != nil || v > 0o7777 {
		return fmt.Errorf("mode %q: %w", b, ErrManifest)
	}
	*p = Perm(v)
	return nil
}

// Content is a backed-up file: its copy in the run directory and what it was.
type Content struct {
	Backup string `json:"backup"` // file name in the run directory
	Mode   Perm   `json:"mode"`
	UID    int    `json:"uid"`
	GID    int    `json:"gid"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"` // hex
}

// Entry is one backed-up path.
type Entry struct {
	Path string `json:"path"` // absolute, as given to Backup
	Kind Kind   `json:"kind"`
	// SymlinkTarget is a symlink's target, as stored.
	SymlinkTarget string `json:"symlinkTarget,omitempty"`
	// File is the content: the file at Path, or the regular file a symlink
	// points to. Nil for KindAbsent and for a symlink to nothing.
	File             *Content `json:"file,omitempty"`
	RollbackRequired bool     `json:"rollbackRequired"`
	// TakenAnyway: taken although enableBackups was false.
	TakenAnyway bool `json:"takenAnyway,omitempty"`
}

// Manifest is a run's manifest.json.
type Manifest struct {
	SchemaVersion  int       `json:"schemaVersion"`
	RunID          string    `json:"runId"`
	Created        time.Time `json:"created"`
	BackupsEnabled bool      `json:"backupsEnabled"`
	Entries        []Entry   `json:"entries"` // in the order taken
	// Dir is the run directory the manifest is in; not stored.
	Dir string `json:"-"`
}

// Result is what Backup did, and the entry it recorded when it took one.
type Result struct {
	Outcome Outcome
	Entry   Entry
}

// Run is one run's backups. It is not safe for concurrent use.
type Run struct {
	sys   host.System
	s     Settings
	m     Manifest
	ready bool // the run directory exists and has been checked
}

// NewRunID returns a run id: t in UTC, sortable, and a random suffix.
func NewRunID(t time.Time) string {
	var b [4]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read does not fail (Go 1.24)
	return t.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:])
}

// Open starts the backups of run runID under s.Path. It does no I/O: the run
// directory is created by the first backup taken. s.Path must be absolute,
// and runID 1 to 64 letters, digits, '-' or '_'.
func Open(sys host.System, s Settings, runID string) (*Run, error) {
	if !filepath.IsAbs(s.Path) {
		return nil, fmt.Errorf("backup: backupPath %q is not an absolute directory (the default is %s): %w", s.Path, DefaultPath, ErrSettings)
	}
	if !validRunID(runID) {
		return nil, fmt.Errorf("backup: run id %q: %w", runID, ErrSettings)
	}
	s.Path = filepath.Clean(s.Path)
	return &Run{sys: sys, s: s, m: Manifest{
		SchemaVersion:  SchemaVersion,
		RunID:          runID,
		Created:        now().UTC().Truncate(time.Second),
		BackupsEnabled: s.Enabled,
		Entries:        []Entry{},
		Dir:            filepath.Join(s.Path, runID),
	}}, nil
}

func validRunID(id string) bool {
	if id == "" || len(id) > 64 || id[0] == '-' || id[0] == '_' {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// Dir returns the run directory, <backupPath>/<run-id>.
func (r *Run) Dir() string { return r.m.Dir }

// Manifest returns a copy of the manifest so far.
func (r *Run) Manifest() Manifest {
	m := r.m
	m.Entries = append([]Entry(nil), r.m.Entries...)
	return m
}

// Backup records what the absolute path is now, before a change replaces or
// creates it: a regular file's content, mode and owner; a symlink's target
// and the content of the regular file it points to; or that nothing is
// there. Directories and other file types are refused (ErrNotRegular). The
// copy is written before the manifest, and the manifest is rewritten
// atomically after each backup, so it always lists every copy a crash left.
// Any error means no backup was recorded: the change must not go ahead.
func (r *Run) Backup(ctx context.Context, path string, need Need) (Result, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return Result{}, fmt.Errorf("backup: %q: %w", path, fs.ErrInvalid)
	}
	outcome := Taken
	if !r.s.Enabled {
		if need != RollbackRequired {
			return Result{Outcome: Skipped}, nil
		}
		outcome = TakenAnyway
	}
	if err := r.prepare(ctx); err != nil {
		return Result{}, err
	}
	e, data, err := inspect(ctx, r.sys, path)
	if err != nil {
		return Result{}, fmt.Errorf("backup: %w", err)
	}
	e.RollbackRequired, e.TakenAnyway = need == RollbackRequired, outcome == TakenAnyway
	if e.File != nil {
		e.File.Backup = fmt.Sprintf("%04d-%s", len(r.m.Entries)+1, filepath.Base(path))
		if err := r.sys.WriteFileAtomic(ctx, filepath.Join(r.m.Dir, e.File.Backup), data, 0o600, -1, -1); err != nil {
			return Result{}, fmt.Errorf("backup: %w", err)
		}
	}
	r.m.Entries = append(r.m.Entries, e)
	if err := r.save(ctx); err != nil {
		r.m.Entries = r.m.Entries[:len(r.m.Entries)-1]
		return Result{}, fmt.Errorf("backup: %w", err)
	}
	return Result{Outcome: outcome, Entry: e}, nil
}

// prepare creates the run directory, which must not exist yet, and checks it
// and every directory above it (checkTrusted).
func (r *Run) prepare(ctx context.Context) error {
	if r.ready {
		return nil
	}
	switch _, err := r.sys.Lstat(ctx, r.m.Dir); {
	case err == nil:
		return fmt.Errorf("backup: %s: %w", r.m.Dir, ErrRunExists)
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("backup: %w", err)
	}
	if err := r.sys.MkdirAll(ctx, r.m.Dir, 0o700); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := checkTrusted(ctx, r.sys, r.m.Dir); err != nil {
		return err
	}
	r.ready = true
	return nil
}

func (r *Run) save(ctx context.Context) error {
	b, err := json.MarshalIndent(r.m, "", "  ")
	if err != nil {
		return err
	}
	return r.sys.WriteFileAtomic(ctx, filepath.Join(r.m.Dir, ManifestName), append(b, '\n'), 0o600, -1, -1)
}

// inspect describes path as Backup records it, with the content to copy.
func inspect(ctx context.Context, sys host.System, path string) (Entry, []byte, error) {
	e := Entry{Path: path}
	fi, err := sys.Lstat(ctx, path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		e.Kind = KindAbsent
		return e, nil, nil
	case err != nil:
		return e, nil, err
	case fi.Mode()&fs.ModeSymlink != 0:
		e.Kind = KindSymlink
		if e.SymlinkTarget, err = sys.Readlink(ctx, path); err != nil {
			return e, nil, err
		}
		c, data, err := content(ctx, sys, linkTarget(path, e.SymlinkTarget))
		if errors.Is(err, fs.ErrNotExist) {
			return e, nil, nil // a symlink to nothing
		}
		e.File = c
		return e, data, err
	case fi.Mode().IsRegular():
		e.Kind = KindFile
		c, data, err := content(ctx, sys, path)
		e.File = c
		return e, data, err
	}
	return e, nil, fmt.Errorf("%s: %w", path, ErrNotRegular)
}

// content reads the regular file name and describes it, from one open file.
func content(ctx context.Context, sys host.System, name string) (*Content, []byte, error) {
	data, fi, err := sys.ReadFileNoFollow(ctx, name)
	if err != nil {
		return nil, nil, err
	}
	uid, gid, ok := host.Owner(fi)
	if !ok {
		return nil, nil, fmt.Errorf("%s: no owner: %w", name, fs.ErrInvalid)
	}
	sum := sha256.Sum256(data)
	return &Content{Mode: PermOf(fi.Mode()), UID: uid, GID: gid, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}, data, nil
}

// linkTarget is where the symlink path, holding target, points.
func linkTarget(path, target string) string {
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Join(filepath.Dir(path), target)
}

// checkTrusted walks dir from "/" with Lstat, following symlinks, and
// requires every directory on the way, dir included, to be owned by root or
// by the user hardn runs as and to be writable by neither its group nor
// others. Then no other user can replace what is stored under dir, which a
// rollback writes back as root.
func checkTrusted(ctx context.Context, sys host.System, dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("backup: run directory %q: %w", dir, fs.ErrInvalid)
	}
	euid := geteuid()
	cur, rest := "/", strings.Split(strings.TrimPrefix(filepath.Clean(dir), "/"), "/")
	fi, err := sys.Lstat(ctx, cur)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := trusted(cur, fi, euid); err != nil {
		return err
	}
	for links := 0; len(rest) > 0; {
		next := filepath.Join(cur, rest[0])
		rest = rest[1:]
		if next == cur {
			continue
		}
		fi, err := sys.Lstat(ctx, next)
		if err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			if links++; links > 40 { // Linux's limit, path_resolution(7)
				return fmt.Errorf("backup: %s: %w", dir, syscall.ELOOP)
			}
			target, err := sys.Readlink(ctx, next)
			if err != nil {
				return fmt.Errorf("backup: %w", err)
			}
			target = linkTarget(next, target)
			cur, rest = "/", append(strings.Split(strings.TrimPrefix(target, "/"), "/"), rest...)
			continue
		}
		if err := trusted(next, fi, euid); err != nil {
			return err
		}
		cur = next
	}
	return nil
}

func trusted(name string, fi fs.FileInfo, euid int) error {
	uid, _, ok := host.Owner(fi)
	switch {
	case !fi.IsDir():
		return fmt.Errorf("backup: %s: %w", name, syscall.ENOTDIR)
	case !ok || uid != 0 && uid != euid || fi.Mode().Perm()&0o022 != 0:
		return fmt.Errorf("backup: %s: %w", name, ErrUnsafeDir)
	}
	return nil
}

// Load reads and checks the manifest of the run directory dir, after
// checking that only root (or hardn's own user) can change dir. A manifest
// of another SchemaVersion, or with an entry Restore could not act on
// safely, is refused with ErrManifest.
func Load(ctx context.Context, sys host.System, dir string) (Manifest, error) {
	if !filepath.IsAbs(dir) {
		return Manifest{}, fmt.Errorf("backup: %q: %w", dir, fs.ErrInvalid)
	}
	dir = filepath.Clean(dir)
	if err := checkTrusted(ctx, sys, dir); err != nil {
		return Manifest{}, err
	}
	name := filepath.Join(dir, ManifestName)
	data, _, err := sys.ReadFileNoFollow(ctx, name)
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("backup: %s: %w: %w", name, ErrManifest, err)
	}
	if m.SchemaVersion != SchemaVersion {
		return Manifest{}, fmt.Errorf("backup: %s: schemaVersion %d, want %d: %w", name, m.SchemaVersion, SchemaVersion, ErrManifest)
	}
	for _, e := range m.Entries {
		if err := e.check(); err != nil {
			return Manifest{}, fmt.Errorf("backup: %s: %w", name, err)
		}
	}
	m.Dir = dir
	return m, nil
}

// check refuses an entry Restore cannot act on safely.
func (e Entry) check() error {
	bad := func(why string) error { return fmt.Errorf("entry %q: %s: %w", e.Path, why, ErrManifest) }
	switch {
	case !filepath.IsAbs(e.Path) || filepath.Clean(e.Path) != e.Path:
		return bad("path not absolute and clean")
	case e.Kind == KindAbsent && (e.File != nil || e.SymlinkTarget != ""):
		return bad("absent, yet with content or a target")
	case e.Kind == KindFile && (e.File == nil || e.SymlinkTarget != ""):
		return bad("a file without content")
	case e.Kind == KindSymlink && e.SymlinkTarget == "":
		return bad("a symlink without a target")
	case e.Kind != KindAbsent && e.Kind != KindFile && e.Kind != KindSymlink:
		return bad("unknown kind")
	case e.File == nil:
		return nil
	}
	c := e.File
	if b := c.Backup; b == "" || b == "." || b == ".." || b == ManifestName || strings.ContainsRune(b, '/') {
		return bad("backup is not a file name in the run directory")
	}
	if s, err := hex.DecodeString(c.SHA256); err != nil || len(s) != sha256.Size || c.Size < 0 {
		return bad("sha256 or size malformed")
	}
	return nil
}

// Action is what Restore did to an entry's path.
type Action uint8

const (
	Unchanged Action = iota + 1 // already as backed up
	Rewritten                   // the copy written back, with its mode and owner
	Removed                     // the file the change created removed
)

func (a Action) String() string {
	switch a {
	case Unchanged:
		return "unchanged"
	case Rewritten:
		return "rewritten"
	case Removed:
		return "removed"
	}
	return "failed"
}

// Restored is what Restore did for one entry; Action is zero when Err is set.
type Restored struct {
	Path   string
	Action Action
	Err    error
}

// Restore puts every entry of m back, last taken first, so a path backed up
// twice ends as it was before its first change. It is idempotent: an entry
// already as backed up is Unchanged. It goes on past a failed entry, and
// returns every failure joined. It writes only through WriteFileAtomic and
// removes only a regular file, never through a symlink:
//
//   - KindFile: the copy, after checking its size and SHA-256 (ErrCorrupt),
//     is written back with the recorded mode and owner;
//   - KindAbsent: a regular file now at the path is removed; anything else
//     there is ErrChanged;
//   - KindSymlink: the path must still be a symlink to the same target
//     (ErrChanged otherwise), and the file it points to is restored as a
//     KindFile or KindAbsent one.
//
// After Ctrl-C, pass context.WithoutCancel(ctx) with a timeout: every
// host.System method refuses a done context.
func Restore(ctx context.Context, sys host.System, m Manifest) ([]Restored, error) {
	if len(m.Entries) == 0 {
		return nil, nil // nothing was taken; the run directory may not exist
	}
	if err := checkTrusted(ctx, sys, m.Dir); err != nil {
		return nil, err
	}
	var out []Restored
	var errs []error
	for i := len(m.Entries) - 1; i >= 0; i-- {
		e := m.Entries[i]
		a, err := restore(ctx, sys, m.Dir, e)
		if err != nil {
			a, err = 0, fmt.Errorf("restore %s: %w", e.Path, err)
			errs = append(errs, err)
		}
		out = append(out, Restored{Path: e.Path, Action: a, Err: err})
	}
	return out, errors.Join(errs...)
}

func restore(ctx context.Context, sys host.System, dir string, e Entry) (Action, error) {
	if err := e.check(); err != nil {
		return 0, err
	}
	name := e.Path
	if e.Kind == KindSymlink {
		fi, err := sys.Lstat(ctx, e.Path)
		if err != nil {
			return 0, err
		}
		var target string
		if fi.Mode()&fs.ModeSymlink != 0 {
			target, err = sys.Readlink(ctx, e.Path)
		}
		if err != nil || target != e.SymlinkTarget {
			return 0, errors.Join(err, fmt.Errorf("no longer a symlink to %s: %w", e.SymlinkTarget, ErrChanged))
		}
		name = linkTarget(e.Path, target)
	}
	if e.File == nil {
		return removeCreated(ctx, sys, name)
	}
	return rewrite(ctx, sys, filepath.Join(dir, e.File.Backup), name, *e.File)
}

func removeCreated(ctx context.Context, sys host.System, name string) (Action, error) {
	fi, err := sys.Lstat(ctx, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Unchanged, nil
	case err != nil:
		return 0, err
	case !fi.Mode().IsRegular():
		return 0, fmt.Errorf("%s is not a regular file: %w", name, ErrChanged)
	}
	if err := sys.Remove(ctx, name); err != nil {
		return 0, err
	}
	return Removed, nil
}

func rewrite(ctx context.Context, sys host.System, copyName, name string, c Content) (Action, error) {
	data, _, err := sys.ReadFileNoFollow(ctx, copyName)
	if err != nil {
		return 0, err
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != c.SHA256 || int64(len(data)) != c.Size {
		return 0, fmt.Errorf("%s: %w", copyName, ErrCorrupt)
	}
	cur, fi, err := sys.ReadFileNoFollow(ctx, name)
	switch {
	case err == nil:
		uid, gid, ok := host.Owner(fi)
		if ok && uid == c.UID && gid == c.GID && PermOf(fi.Mode()) == c.Mode && bytes.Equal(cur, data) {
			return Unchanged, nil
		}
	case !errors.Is(err, fs.ErrNotExist):
		return 0, err
	}
	if err := sys.WriteFileAtomic(ctx, name, data, c.Mode.FileMode(), c.UID, c.GID); err != nil {
		return 0, err
	}
	return Rewritten, nil
}
