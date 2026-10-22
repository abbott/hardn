package steps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
)

// The save of hardn.yml as a change (refactor plan §3.19, approved
// 2026-10-17: to the loaded path, by one atomic save function, warning that
// comments are lost; slice 2 decision 1, adopted 2026-10-22: an edit of the
// configuration is saved after the change it asks for has applied, and
// rolled back with it). Every slice that edits hardn.yml plans it after the
// change it records (next/s2-control).

// CodeCommentsLost is the save's finding when hardn.yml holds comments,
// which config.Save does not keep.
const CodeCommentsLost = "CONFIG_COMMENTS_LOST"

// CodeConfigChanged is the save's refusal of a hardn.yml that changed since
// it was read (ErrConfigChanged).
const CodeConfigChanged = "CONFIG_CHANGED"

// ResourceConfig is hardn.yml, the resource every save names
// (engine.Change.Resources): one plan saves it once.
const ResourceConfig = "hardn.yml"

// ErrConfigChanged: hardn.yml's bytes are not the ones the configuration
// was read from (config.Config.LoadedFrom) when the save is planned, or not
// the ones the plan read when it runs: saving would undo what changed them.
var ErrConfigChanged = errors.New("hardn.yml changed since it was read")

// PlanSave is the change that saves cfg to the file it was loaded from,
// cfg.Path(), through config.Save, after the changes after names
// (StageFiles, plain). It reads the file and refuses, before anything
// changes, a configuration not loaded from a file (config.ErrNotLoaded) and
// a file whose bytes are not the ones cfg was read from (ErrConfigChanged),
// each with a report (Refusal). The engine backs the file up and journals
// it before the save, so the rollback restores the bytes the plan read; if
// the save fails (a Failure: "Saving <path> failed"), the engine rolls back
// the changes before it too, so that the host and hardn.yml agree. Its
// Apply refuses a file that changed since
// the plan (ErrConfigChanged), before it writes. With comments in the
// file, its finding says that saving loses them (CodeCommentsLost). One
// plan saves hardn.yml once: a second save in it is ErrConflict.
func PlanSave(ctx context.Context, sys host.System, id string, cfg *config.Config, after ...string) (engine.Change, error) {
	if cfg == nil || cfg.Path() == "" {
		return engine.Change{}, &Refusal{Code: "CONFIG_NOT_LOADED", Why: "No hardn.yml was read, so there is none to save to",
			Fix: "create /etc/hardn/hardn.yml, then try again", Err: fmt.Errorf("steps: save: %w", config.ErrNotLoaded)}
	}
	path := cfg.Path()
	data, _, err := sys.ReadFileNoFollow(ctx, path)
	if err != nil {
		return engine.Change{}, fmt.Errorf("steps: save: %w", err)
	}
	if !cfg.LoadedFrom(data) {
		return engine.Change{}, &Refusal{Code: CodeConfigChanged, Why: path + " changed since hardn read it",
			Fix: "run hardn again, so that it reads the file as it is now", Err: fmt.Errorf("steps: save: %s: %w", path, ErrConfigChanged)}
	}
	c := engine.Change{ID: id, Summary: "Save the configuration to " + path, Title: "Save the configuration", Target: path,
		Stage: engine.StageFiles, After: after, Backups: []string{path}, Resources: []string{ResourceConfig}}
	if config.HasComments(data) {
		c.Findings = []engine.Finding{{Code: CodeCommentsLost, Detail: "Saving hardn.yml loses its comments", Source: path}}
	}
	sum := sha256.Sum256(data)
	read := hex.EncodeToString(sum[:])
	c.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
		if e, ok := run.Backups[path]; !ok || e.Kind != backup.KindFile || e.File == nil || e.File.SHA256 != read {
			return engine.Ran{}, fmt.Errorf("steps: save: %s, backed up just now, is not as the plan read it: %w", path, ErrConfigChanged)
		}
		if err := config.Save(ctx, sys, cfg); err != nil {
			return engine.Ran{}, &Failure{Text: "Saving " + path + " failed", Err: err}
		}
		return engine.Ran{Detail: "saved " + path}, nil
	}
	return c, nil
}
