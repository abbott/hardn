package steps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
)

// The save of hardn.yml as a change (refactor plan §3.19, approved
// 2026-10-17: to the loaded path, by one atomic save function; amended by
// the maintainer on 2026-10-22, for every slice: "edit only the keys that
// changed, keeping the rest of the file and its comments; drop the
// comments-lost warning" (next/s2-config-edit); slice 2 decision 1,
// adopted 2026-10-22: an edit of the configuration is saved after the
// change it asks for has applied, and rolled back with it). Every slice
// that edits hardn.yml plans it after the change it records
// (next/s2-control).

// CodeConfigChanged is the save's refusal of a hardn.yml that changed since
// it was read (ErrConfigChanged, config.ErrChanged).
const CodeConfigChanged = "CONFIG_CHANGED"

// ResourceConfig is hardn.yml, the resource every save names
// (engine.Change.Resources): one plan saves it once.
const ResourceConfig = "hardn.yml"

// ErrConfigChanged: hardn.yml's bytes are not the ones the configuration
// was read from (config.Config.LoadedFrom) when the save is planned, or not
// the ones the plan read when it runs: saving would undo what changed them.
var ErrConfigChanged = errors.New("hardn.yml changed since it was read")

// Changed is the refusal of hardn.yml at path, changed since it was read:
// other bytes, gone, or no longer decoding (cause, when set), so that
// saving the configuration read before would undo what changed it, and
// what was read is not what the file holds (CodeConfigChanged).
func Changed(path string, cause error) *Refusal {
	err := fmt.Errorf("steps: save: %s: %w", path, ErrConfigChanged)
	if cause != nil {
		err = fmt.Errorf("steps: save: %s: %w: %w", path, ErrConfigChanged, cause)
	}
	return &Refusal{Code: CodeConfigChanged, Why: path + " changed since it was read", Fix: "start again, so that the file is read as it is now", Err: err}
}

// notEdited is the refusal of an edit of hardn.yml at path that
// config.Edit refuses (config.EditError): what about the file stops it,
// and the settings to set by hand (proposed words, config-edit.md §5).
func notEdited(path string, err error) error {
	var e *config.EditError
	if !errors.As(err, &e) {
		return fmt.Errorf("steps: save: %w", err)
	}
	code, at := "CONFIG_NOT_EDITABLE", path
	if errors.Is(err, config.ErrEditUnsafe) {
		code = "CONFIG_EDIT_UNSAFE"
	}
	if e.Key != "" {
		at = e.Key + " in " + path
	}
	return &Refusal{Code: code, Why: at + " " + e.Why, Fix: "set " + strings.Join(e.Keys, ", ") + " in " + path + " by hand, then try again",
		Err: fmt.Errorf("steps: save: %w", err)}
}

// PlanSave is the change that saves cfg to the file it was loaded from,
// cfg.Path(), through config.Save, after the changes after names
// (StageFiles, plain); none when the file already gives cfg's settings.
// It reads the file, not through a symlink, and refuses, before anything
// changes, each with a report (Refusal): a configuration not loaded from a
// file (config.ErrNotLoaded), a symlink, which config.Save would refuse
// (host.ErrSymlink), a file gone or whose bytes are not the ones cfg was
// read from (ErrConfigChanged; Changed gives the report), and an edit
// config.Edit refuses (CONFIG_NOT_EDITABLE, CONFIG_EDIT_UNSAFE). Its diff
// is the file's bytes against config.Edit's: the settings cfg changes,
// edited in place, the rest of the file and its comments as they are. The
// engine backs the file up and journals it before the save, so the
// rollback restores the bytes the plan read; if the save fails (a Failure:
// "Saving <path> failed"), the engine rolls back the changes before it
// too, so that the host and hardn.yml agree. Its Apply refuses, before it
// writes, a file that changed since the plan (ErrConfigChanged) and a
// configuration that no longer gives the bytes shown
// (engine.ErrPlanChanged). One plan saves hardn.yml once: a second save in
// it is ErrConflict.
func PlanSave(ctx context.Context, sys host.System, id string, cfg *config.Config, after ...string) ([]engine.Change, error) {
	if cfg == nil || cfg.Path() == "" {
		return nil, &Refusal{Code: "CONFIG_NOT_LOADED", Why: "No hardn.yml was read, so there is none to save to",
			Fix: "create /etc/hardn/hardn.yml, then try again", Err: fmt.Errorf("steps: save: %w", config.ErrNotLoaded)}
	}
	path := cfg.Path()
	data, _, err := sys.ReadFileNoFollow(ctx, path)
	switch {
	case errors.Is(err, host.ErrSymlink):
		return nil, &Refusal{Code: "SYMLINK_REFUSED", Why: path + " is a link", Fix: "edit the file it links to by hand",
			Err: fmt.Errorf("steps: save: %w", err)}
	case errors.Is(err, fs.ErrNotExist):
		return nil, Changed(path, err)
	case err != nil:
		return nil, fmt.Errorf("steps: save: %w", err)
	case !cfg.LoadedFrom(data):
		return nil, Changed(path, nil)
	}
	saved, err := config.Edit(data, cfg)
	if err != nil {
		return nil, notEdited(path, err)
	}
	if bytes.Equal(saved, data) {
		return nil, nil
	}
	was, will := string(data), string(saved)
	c := engine.Change{ID: id, Summary: "Save the configuration to " + path, Title: "Save the configuration", Target: path,
		Diff: engine.FileDiff(path, &was, &will), Stage: engine.StageFiles, After: after, Backups: []string{path},
		Resources: []string{ResourceConfig}}
	sum := sha256.Sum256(data)
	read := hex.EncodeToString(sum[:])
	c.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
		if e, ok := run.Backups[path]; !ok || e.Kind != backup.KindFile || e.File == nil || e.File.SHA256 != read {
			return engine.Ran{}, fmt.Errorf("steps: save: %s, backed up just now, is not as the plan read it: %w", path, ErrConfigChanged)
		}
		if now, err := config.Edit(data, cfg); err != nil || !bytes.Equal(now, saved) {
			// What would be written is not the diff shown.
			return engine.Ran{}, fmt.Errorf("steps: save: the configuration is not the one planned: %w", errors.Join(engine.ErrPlanChanged, err))
		}
		if err := config.Save(ctx, sys, cfg); err != nil {
			return engine.Ran{}, &Failure{Text: "Saving " + path + " failed", Err: err}
		}
		return engine.Ran{Detail: "saved " + path}, nil
	}
	return []engine.Change{c}, nil
}
