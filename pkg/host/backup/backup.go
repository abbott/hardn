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
// Everything goes through host.System, and a path is seen only through its
// guarded operations (see host.System): files are read with
// ReadFileNoFollow, so a copy's content, mode and owner come from one file,
// written with WriteFileAtomic and removed with Remove, and a symlink is
// resolved with ResolveSymlink. So neither backup nor restore follows a
// symlink a user planted, at the path or on the way to it, in a directory
// that user can change: the backup is refused instead. The backup directory
// must be one only root (or hardn's own user) can change, since a rollback
// writes its copies back as root.
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
	// trustedTop is where checkTrusted starts checking: the directories
	// above it are walked but not checked. The tests set it to their
	// t.TempDir(), which is under /tmp on Linux, a directory checkTrusted
	// rightly refuses.
	trustedTop = "/"
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
	// Seq numbers the entries of a run from 1, in the order taken: it
	// names an entry for a rollback journal, and its copy.
	Seq  int    `json:"seq"`
	Path string `json:"path"` // absolute, as given to Backup
	Kind Kind   `json:"kind"`
	// SymlinkTarget is a symlink's target, as stored.
	SymlinkTarget string `json:"symlinkTarget,omitempty"`
	// SymlinkResolved is the absolute path the symlink pointed to
	// (host.System.ResolveSymlink); restore puts that file back.
	SymlinkResolved string `json:"symlinkResolved,omitempty"`
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

// Manifest returns a copy of the manifest so far, sharing nothing with r.
func (r *Run) Manifest() Manifest {
	m := r.m
	m.Entries = append([]Entry(nil), r.m.Entries...)
	for i, e := range m.Entries {
		if e.File != nil {
			c := *e.File
			m.Entries[i].File = &c
		}
	}
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
	e.Seq = len(r.m.Entries) + 1
	e.RollbackRequired, e.TakenAnyway = need == RollbackRequired, outcome == TakenAnyway
	if e.File != nil {
		e.File.Backup = fmt.Sprintf("%04d-%s", e.Seq, filepath.Base(path))
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

// prepare creates the run directory, which must not exist yet. Nothing is
// created before the deepest directory of its path that exists has passed
// checkTrusted: then only root or hardn's user can create an entry there, so
// no other user can win a race to the name. The missing directories are then
// created one at a time, 0700, each checked in turn; if a creation or a
// check fails, the ones this call created are removed, so a later Backup of
// the run tries again rather than finding ErrRunExists.
func (r *Run) prepare(ctx context.Context) (err error) {
	if r.ready {
		return nil
	}
	var missing []string // r.m.Dir first, then its missing parents
	top := r.m.Dir
	for {
		_, err := r.sys.Lstat(ctx, top)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) || top == "/" {
			return fmt.Errorf("backup: %w", err)
		}
		missing = append(missing, top)
		top = filepath.Dir(top)
	}
	if len(missing) == 0 {
		return fmt.Errorf("backup: %s: %w", r.m.Dir, ErrRunExists)
	}
	if err := checkTrusted(ctx, r.sys, top); err != nil {
		return err
	}
	var created []string
	defer func() {
		for i := len(created) - 1; err != nil && i >= 0; i-- {
			_ = r.sys.Remove(ctx, created[i])
		}
	}()
	for i := len(missing) - 1; i >= 0; i-- {
		if err := r.sys.MkdirAll(ctx, missing[i], 0o700); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		created = append(created, missing[i])
		if err := checkTrusted(ctx, r.sys, missing[i]); err != nil {
			return err
		}
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

// inspect describes path as Backup records it, with the content to copy. It
// sees path only through guarded operations (ReadFileNoFollow,
// ResolveSymlink), so a symlink in a directory another user can change, at
// path or on the way, is refused with host.ErrSymlinkParent.
func inspect(ctx context.Context, sys host.System, path string) (Entry, []byte, error) {
	e := Entry{Path: path}
	c, data, err := content(ctx, sys, path)
	switch {
	case err == nil:
		e.Kind, e.File = KindFile, c
		return e, data, nil
	case errors.Is(err, fs.ErrNotExist):
		e.Kind = KindAbsent
		return e, nil, nil
	case errors.Is(err, host.ErrSymlinkParent) || !errors.Is(err, host.ErrSymlink):
		return e, nil, err
	}
	e.Kind = KindSymlink
	if e.SymlinkTarget, e.SymlinkResolved, err = sys.ResolveSymlink(ctx, path); err != nil {
		return e, nil, err
	}
	c, data, err = content(ctx, sys, e.SymlinkResolved)
	switch {
	case err == nil:
		e.File = c
		return e, data, nil
	case errors.Is(err, fs.ErrNotExist):
		return e, nil, nil // a symlink to nothing
	}
	return e, nil, fmt.Errorf("%s points to %s: %w", path, e.SymlinkResolved, err)
}

// content reads the regular file name and describes it, from one open file.
// A directory or a special file is ErrNotRegular.
func content(ctx context.Context, sys host.System, name string) (*Content, []byte, error) {
	data, fi, err := sys.ReadFileNoFollow(ctx, name)
	switch {
	case errors.Is(err, syscall.EISDIR) || errors.Is(err, fs.ErrInvalid):
		return nil, nil, fmt.Errorf("%s: %w (%w)", name, ErrNotRegular, err)
	case err != nil:
		return nil, nil, err
	}
	uid, gid, ok := host.Owner(fi)
	if !ok {
		return nil, nil, fmt.Errorf("%s: its FileInfo carries no owner", name)
	}
	sum := sha256.Sum256(data)
	return &Content{Mode: PermOf(fi.Mode()), UID: uid, GID: gid, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}, data, nil
}

// checkTrusted walks dir from "/" with Lstat, following symlinks, and
// requires every directory on the way, dir included, to be owned by root or
// by the user hardn runs as and to be writable by neither its group nor
// others. Then no other user can replace what is stored under dir, which a
// rollback writes back as root. A directory with the sticky bit is no
// exception (/tmp): root writes nothing there (plan §3.12).
func checkTrusted(ctx context.Context, sys host.System, dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("backup: run directory %q: %w", dir, fs.ErrInvalid)
	}
	euid := geteuid()
	trusted := func(name string, fi fs.FileInfo) error {
		if name != trustedTop && (name == "/" || strings.HasPrefix(trustedTop, name+"/")) {
			return nil // above trustedTop: the tests' seam
		}
		return trustedDir(name, fi, euid)
	}
	cur, rest := "/", strings.Split(strings.TrimPrefix(filepath.Clean(dir), "/"), "/")
	fi, err := sys.Lstat(ctx, cur)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := trusted(cur, fi); err != nil {
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
			// cur, which holds the link, has passed trusted and has no
			// symlink in it.
			target, err := sys.Readlink(ctx, next)
			if err != nil {
				return fmt.Errorf("backup: %w", err)
			}
			if target, err = host.JoinLinkTarget(cur, target); err != nil {
				return fmt.Errorf("backup: %s: %w", next, err)
			}
			cur, rest = "/", append(strings.Split(strings.TrimPrefix(target, "/"), "/"), rest...)
			continue
		}
		if err := trusted(next, fi); err != nil {
			return err
		}
		cur = next
	}
	return nil
}

func trustedDir(name string, fi fs.FileInfo, euid int) error {
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
	prev := 0
	for _, e := range m.Entries {
		if err := e.check(); err != nil {
			return Manifest{}, fmt.Errorf("backup: %s: %w", name, err)
		}
		if e.Seq <= prev {
			return Manifest{}, fmt.Errorf("backup: %s: entry %q: seq %d after %d: %w", name, e.Path, e.Seq, prev, ErrManifest)
		}
		prev = e.Seq
	}
	m.Dir = dir
	return m, nil
}

// check refuses an entry Restore cannot act on safely.
func (e Entry) check() error {
	bad := func(why string) error { return fmt.Errorf("entry %q: %s: %w", e.Path, why, ErrManifest) }
	clean := func(p string) bool { return filepath.IsAbs(p) && filepath.Clean(p) == p }
	switch {
	case e.Seq < 1:
		return bad("seq not a positive number")
	case !clean(e.Path):
		return bad("path not absolute and clean")
	case e.Kind != KindSymlink && (e.SymlinkTarget != "" || e.SymlinkResolved != ""):
		return bad("a symlink's target on what was not a symlink")
	case e.Kind == KindAbsent && e.File != nil:
		return bad("absent, yet with content")
	case e.Kind == KindFile && e.File == nil:
		return bad("a file without content")
	case e.Kind == KindSymlink && (e.SymlinkTarget == "" || !clean(e.SymlinkResolved)):
		return bad("a symlink without a target, or where it points not absolute and clean")
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

// Restored is what Restore did for one entry. Action is zero when Err is
// set, but for one case: Rewritten with an Err wrapping host.ErrNotDurable,
// when the copy is back in place but its directory could not be synced, so
// it may not survive a crash.
type Restored struct {
	Seq    int // the entry's
	Path   string
	Action Action
	Err    error
}

// Restore puts every entry of m back, last taken first, so a path backed up
// twice ends as it was before its first change. It is idempotent: an entry
// already as backed up is Unchanged. It goes on past a failed entry, and
// returns every failure joined. It sees and changes paths only through
// guarded operations (see the package doc); it writes only with
// WriteFileAtomic and removes only a regular file:
//
//   - KindFile: the copy, after checking its size and SHA-256 (ErrCorrupt),
//     is written back with the recorded mode and owner;
//   - KindAbsent: a regular file now at the path is removed; anything else
//     there is ErrChanged;
//   - KindSymlink: the path must still be a symlink to the same target,
//     pointing to the same file (ErrChanged otherwise), in a directory only
//     root or hardn's user can change (host.ErrSymlinkParent otherwise), and
//     the file it points to is restored as a KindFile or KindAbsent one.
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
			err = fmt.Errorf("restore %s: %w", e.Path, err)
			errs = append(errs, err)
		}
		out = append(out, Restored{Seq: e.Seq, Path: e.Path, Action: a, Err: err})
	}
	return out, errors.Join(errs...)
}

func restore(ctx context.Context, sys host.System, dir string, e Entry) (Action, error) {
	if err := e.check(); err != nil {
		return 0, err
	}
	name := e.Path
	if e.Kind == KindSymlink {
		target, resolved, err := sys.ResolveSymlink(ctx, e.Path)
		switch {
		case errors.Is(err, syscall.EINVAL) || errors.Is(err, fs.ErrNotExist):
			return 0, fmt.Errorf("no longer a symlink to %s: %w (%w)", e.SymlinkTarget, ErrChanged, err)
		case err != nil:
			return 0, err
		case target != e.SymlinkTarget || resolved != e.SymlinkResolved:
			return 0, fmt.Errorf("now a symlink to %s (%s), not %s (%s): %w", target, resolved, e.SymlinkTarget, e.SymlinkResolved, ErrChanged)
		}
		name = resolved
	}
	if e.File == nil {
		return removeCreated(ctx, sys, name)
	}
	return rewrite(ctx, sys, filepath.Join(dir, e.File.Backup), name, *e.File)
}

// removeCreated removes the regular file a change created at name. It looks
// with ReadFileNoFollow, which is guarded, as Remove is.
func removeCreated(ctx context.Context, sys host.System, name string) (Action, error) {
	_, _, err := sys.ReadFileNoFollow(ctx, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Unchanged, nil
	case errors.Is(err, host.ErrSymlinkParent):
		return 0, err
	case errors.Is(err, host.ErrSymlink) || errors.Is(err, syscall.EISDIR) || errors.Is(err, fs.ErrInvalid):
		return 0, fmt.Errorf("%s is not a regular file: %w (%w)", name, ErrChanged, err)
	case err != nil:
		return 0, err
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
	switch err := sys.WriteFileAtomic(ctx, name, data, c.Mode.FileMode(), c.UID, c.GID); {
	case errors.Is(err, host.ErrNotDurable):
		return Rewritten, err // in place, not synced: see Restored
	case err != nil:
		return 0, err
	}
	return Rewritten, nil
}
