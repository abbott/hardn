// Package backup holds Backup's control (refactor plan slice 3, Backup,
// unit (b), next/s3-control): the backup settings hardn.yml gives,
// backupPath and enableBackups, as data for the Backup screen, and the
// change that saves them.
//
// Check says what pkg/host/backup's Check makes of the settings, writing
// nothing (decision 3): the path's note, "Root only", "Not created yet",
// "Unsafe", "Not absolute", "Not a directory", "Can't tell"; Read gives the
// screen the reading whole. Plan is the save of the two keys through
// steps.PlanSave and config.Edit, the keys that changed alone (§3.19 as
// amended), a new path refused first if it is not one backups can go in
// (decisions 5 and 6). The control saves the backup settings
// (engine.BackupsSaver), so the engine plans and applies its change under
// the settings it saves (decision 7 (a)). Field is the path field's check
// for the screen, Refusal the words of a refused path, which pkg/output
// gives the engine's refusal of any change while backupPath is refused.
// docs/engine/control-backup.md has every note, refusal and word, and
// which are decided.
//
// The package reaches the host only through host.System (depguard
// controls-through-host) and renders nothing.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
)

// ID is the control's ID, stable once released.
const ID = "backup.settings"

// ChangeSave is the id of the change Plan makes: the save of hardn.yml.
const ChangeSave = ID + "/save"

// The hardn.yml settings the control saves, the ones that changed alone
// (config.Edit).
const (
	SettingEnabled = "enableBackups"
	SettingPath    = "backupPath"
)

// The path's notes (gallery-backup.md decision 3, decided 2026-10-27): what
// backup.Check found of backupPath, as Status.Detail.
const (
	DetailRootOnly     = "Root only"       // Configured: a directory only root (or hardn's user) can change
	DetailNotCreated   = "Not created yet" // Configured: not there; Check passed on the directory above it, and the first backup makes it
	DetailUnsafe       = "Unsafe"          // NotConfigured: another user can change it, or a directory above it
	DetailNotAbsolute  = "Not absolute"    // NotConfigured: not an absolute path
	DetailNotDirectory = "Not a directory" // NotConfigured: it, or a path above it, is a file
	DetailCantTell     = "Can't tell"      // Unknown: Check failed otherwise
)

// Controls is Backup's settings, read from cfg, hardn.yml as loaded.
func Controls(cfg *config.Config) []engine.Control { return []engine.Control{Settings{Config: cfg}} }

// Settings is Backup's control: the backup settings hardn.yml gives, and
// the change that saves them.
//
// The engine's Check takes no configuration, so the control carries what
// Check reads (Config), as the DNS control does. Plan reads in.Config, the
// engine's: the caller gives Engine.Plan a copy of hardn.yml with the
// setting edited (backups turned off, a new path), and the engine plans
// and applies the change under the settings that copy gives
// (SavedBackups).
type Settings struct {
	// Config is hardn.yml as loaded; nil reads as hardn's defaults.
	Config *config.Config
}

var _ engine.BackupsSaver = Settings{}

// ID returns ID.
func (Settings) ID() string { return ID }

// Title is the control's name in a plan of several controls and the JSON
// document's status (proposed).
func (Settings) Title() string { return "Backups" }

// Applies is true on every host: backups are hardn's own, wherever it runs.
func (Settings) Applies(detect.Facts) (bool, string) { return true, "" }

// Reading is what Check reads of the backup settings: the screen's Backup
// State and Backup Path rows (decision 1 (a)), Verify's report and item
// 1's refusal before it asks (decision 7 (a)).
type Reading struct {
	// Settings are backupPath and enableBackups, as Config gives them.
	Settings hostbackup.Settings
	// Err is backup.Check's refusal of them, with no file to back up:
	// what every change's plan refuses, but one that saves another path
	// (decision 7 (a)); nil when it passes.
	Err error
	// Made: backupPath is a directory; when not, Check passed on the
	// directory above it, and the first backup makes it.
	Made bool
}

// Read reads the backup settings of cfg on sys, changing nothing
// (decision 3: backup.Check, no write test): Check's verdict on the path,
// and whether it is there yet. A nil cfg is hardn's defaults.
func Read(ctx context.Context, sys host.System, cfg *config.Config) Reading {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	r := Reading{Settings: cfg.BackupSettings()}
	r.Err = hostbackup.Check(ctx, sys, r.Settings, hostbackup.NewRunID(time.Now()), nil)
	if r.Err == nil {
		fi, err := sys.Lstat(ctx, filepath.Clean(r.Settings.Path))
		r.Made = err == nil && fi.IsDir()
	}
	return r
}

// Note is the path's note (decision 3).
func (r Reading) Note() string {
	switch {
	case r.Err == nil && r.Made:
		return DetailRootOnly
	case r.Err == nil:
		return DetailNotCreated
	}
	return refusedNote(r.Err)
}

// refusedNote is the note of err, backup.Check's refusal.
func refusedNote(err error) string {
	var de *hostbackup.DirError
	switch {
	case errors.Is(err, hostbackup.ErrSettings):
		return DetailNotAbsolute
	case errors.Is(err, hostbackup.ErrUnsafeDir):
		return DetailUnsafe
	case errors.As(err, &de): // a DirError is ErrUnsafeDir or ENOTDIR
		return DetailNotDirectory
	}
	return DetailCantTell
}

// Check reads the backup settings (Read): Configured when backup.Check
// passes, the note "Root only" or "Not created yet", whether backups are on
// or off (the engine's backups are rollback-required, and taken either
// way: §3.16); Not Configured when it refuses the path, "Unsafe", "Not
// absolute" or "Not a directory"; Unknown, "Can't tell", with Check's
// error, when it could not tell. It writes nothing.
func (s Settings) Check(ctx context.Context, sys host.System, _ detect.Facts) (engine.Status, error) {
	r := Read(ctx, sys, s.Config)
	st := engine.Status{State: engine.Configured, Detail: r.Note(), Evidence: evidence(s.Config, r)}
	switch st.Detail {
	case DetailRootOnly, DetailNotCreated:
	case DetailCantTell:
		st.State = engine.Unknown
		return st, r.Err
	default:
		st.State = engine.NotConfigured
	}
	return st, nil
}

// evidence is what r was read from: hardn.yml's two keys, with its path
// (none when no hardn.yml was read), and what backup.Check found of the
// path.
func evidence(cfg *config.Config, r Reading) []string {
	var ev []string
	if cfg != nil && cfg.Path() != "" {
		ev = append(ev, cfg.Path()+": "+SettingEnabled+" "+strconv.FormatBool(r.Settings.Enabled)+", "+SettingPath+" "+r.Settings.Path)
	}
	switch {
	case r.Err != nil:
		ev = append(ev, r.Err.Error())
	case r.Made:
		ev = append(ev, r.Settings.Path+": a directory")
	default:
		ev = append(ev, r.Settings.Path+": absent")
	}
	return ev
}

// SavedBackups is the backup settings in.Config gives, which the plan
// saves: the engine plans and applies the change under them (decision 7
// (a)), so a new path takes the change's own backup, and turning backups
// off takes it with them off.
func (Settings) SavedBackups(in engine.Input) hostbackup.Settings {
	if in.Config == nil {
		return hostbackup.Settings{}
	}
	return in.Config.BackupSettings()
}

// Plan is the save of in.Config's backupPath and enableBackups to the
// hardn.yml it was loaded from (steps.PlanSave): the keys whose values
// differ from the file's alone, edited in place; none when neither does.
// A backupPath other than the file's is refused first, with a report, when
// backups cannot go there (Refusal; decisions 5 and 6): not absolute, not
// written clean, a directory another user can change, or a file. The
// save's own refusals follow (no hardn.yml read, CONFIG_NOT_LOADED; a file
// changed since it was read, CONFIG_CHANGED; an edit config.Edit refuses).
// Every refusal comes before any change.
func (Settings) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	cfg := in.Config
	if cfg != nil && cfg.Path() != "" {
		saved, err := config.Read(ctx, sys, cfg.Path())
		switch {
		case errors.Is(err, fs.ErrNotExist), errors.Is(err, config.ErrInvalidFile), errors.Is(err, config.ErrRootLoginNotApplied):
			return nil, steps.Changed(cfg.Path(), err)
		case err != nil:
			return nil, fmt.Errorf("backup: hardn.yml: %w", err)
		case cfg.BackupPath != saved.BackupPath:
			if err := checkPath(ctx, sys, cfg.BackupPath); err != nil {
				return nil, err
			}
		}
	}
	return steps.PlanSave(ctx, sys, ChangeSave, cfg, []string{SettingEnabled, SettingPath})
}

// checkPath refuses p, a new backupPath, with a report, when backups
// cannot go there: backup.Check's refusals (Refusal), and a path not
// written clean, which Open would clean unsaid (decision 5). Another
// failure of Check is its own error.
func checkPath(ctx context.Context, sys host.System, p string) error {
	err := hostbackup.Check(ctx, sys, hostbackup.Settings{Path: p, Enabled: true}, hostbackup.NewRunID(time.Now()), nil)
	switch {
	case errors.Is(err, hostbackup.ErrSettings):
	case filepath.Clean(p) != p:
		clean := filepath.Clean(p)
		return &steps.Refusal{Code: codeSettings, Why: strconv.Quote(p) + " is not written as " + clean, Fix: "write it as " + clean,
			Err: fmt.Errorf("backup: backupPath %q is not written clean: %w", p, hostbackup.ErrSettings)}
	case err == nil:
		return nil
	}
	if r := Refusal(p, err); r != nil {
		return r
	}
	return err
}

// The codes of the refusals, the registry's for the errors they wrap
// (pkg/output; pinned by the tests).
const (
	codeSettings = "BACKUP_SETTINGS_INVALID"
	codeUnsafe   = "UNSAFE_DIRECTORY"
	codeNotDir   = "BACKUP_PATH_NOT_DIRECTORY"
)

// Refusal is err, backup.Check's refusal of the backup path p, with its
// report (proposed words, control-backup.md §6; gallery-backup.md §6):
// why backups cannot go there, and how to go on, with Backup's item 2,
// which changes the path. It is the engine's refusal of any change while
// backupPath is refused, as pkg/output reports it, and of a new path the
// path field did not check. nil for an error that is none of Check's
// refusals of the path: an absolute path (ErrSettings), a directory
// another user can change (ErrUnsafeDir), a file on the path (a
// DirError's ENOTDIR).
func Refusal(p string, err error) *steps.Refusal {
	var de *hostbackup.DirError
	r := &steps.Refusal{Err: err}
	switch {
	case errors.Is(err, hostbackup.ErrSettings):
		r.Code, r.Why, r.Fix = codeSettings, strconv.Quote(p)+" is not an absolute path", "set an absolute path with Backup's item 2"
	case !errors.As(err, &de):
		return nil
	case errors.Is(err, hostbackup.ErrUnsafeDir):
		r.Code, r.Why, r.Fix = codeUnsafe, "Others can change "+de.Dir+", so backups can't go in "+p, "choose a directory only root can change, with Backup's item 2"
	default: // a DirError is ErrUnsafeDir or ENOTDIR
		r.Code, r.Why, r.Fix = codeNotDir, de.Dir+" is not a directory", "choose another path with Backup's item 2"
	}
	return r
}

// FieldError is the path field's verdict on an entry it does not take
// silently (Field): Words, as the field shows them under it, and Warn for
// the path in use, which the field takes after the warning (decision 6),
// as the DNS screen warns of a nameserver already configured. Err is
// backup.Check's error, when it is Check's verdict.
type FieldError struct {
	Words string
	Warn  bool
	Err   error
}

func (e *FieldError) Error() string { return e.Words }

// Unwrap returns Check's error, or nil.
func (e *FieldError) Unwrap() error { return e.Err }

// Field is the path field's check of entry, typed to replace current, the
// backup path in use (decisions 5 and 6): nil when backups can go there,
// a path not there yet among them (the first backup makes it, 0700); a
// *FieldError otherwise, in this order: nothing typed; not absolute (Check's
// ErrSettings); not written clean, "Write it as <path>" (decision 5); the
// path in use, a warning; a directory another user can change, naming it
// (a DirError); a file on the path; Check failing otherwise. It writes
// nothing.
func Field(ctx context.Context, sys host.System, current, entry string) error {
	if entry == "" {
		return &FieldError{Words: "Backup path cannot be empty"}
	}
	err := hostbackup.Check(ctx, sys, hostbackup.Settings{Path: entry, Enabled: true}, hostbackup.NewRunID(time.Now()), nil)
	var de *hostbackup.DirError
	switch {
	case errors.Is(err, hostbackup.ErrSettings):
		return &FieldError{Words: "Not an absolute path", Err: err}
	case filepath.Clean(entry) != entry:
		return &FieldError{Words: "Write it as " + filepath.Clean(entry)}
	case entry == current:
		return &FieldError{Words: entry + " is the backup path already", Warn: true}
	case errors.Is(err, hostbackup.ErrUnsafeDir) && errors.As(err, &de):
		return &FieldError{Words: "Others can change " + de.Dir, Err: err}
	case errors.As(err, &de):
		return &FieldError{Words: "Not a directory", Err: err}
	case err != nil:
		return &FieldError{Words: "Can't check it: " + err.Error(), Err: err}
	}
	return nil
}

// NotWritten is err, a backup that could not be written (a
// hostbackup.WriteError: a read-only file system, a full disk), with its
// report (proposed words, control-backup.md §6): which file could not be
// backed up where, and why, as the failure's Why; how to go on, its Fix.
// nil for any other error. pkg/output words the apply that failed at its
// backup with it, and adds that nothing was changed once the rollback
// finished.
func NotWritten(err error) *steps.Refusal {
	var we *hostbackup.WriteError
	if !errors.As(err, &we) {
		return nil
	}
	cause := we.Err.Error()
	var pe *fs.PathError
	if errors.As(we.Err, &pe) {
		cause = pe.Err.Error()
	}
	return &steps.Refusal{Code: "BACKUP_WRITE_FAILED", Why: "Can't back up " + we.Path + " in " + we.BackupPath + ": " + cause,
		Fix: "choose a backup path hardn can write, with Backup's item 2", Err: err}
}
