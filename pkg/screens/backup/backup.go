// Package backup runs hardn's Backup screen on a host (refactor plan slice
// 3, Backup, unit (c), next/s3-screen): Figma's 05-BACKUP as the
// maintainer adopted its mock on 2026-10-27 (docs/engine/gallery-backup.md
// §5), drawn by pkg/ui/screen (screen.Backup) from what Backup's control
// (pkg/engine/controls/backup) and pkg/host/backup read, writing nothing,
// and its two changes each a change through the plan/apply engine in
// pkg/ui's canonical flow: the review, y/N, the apply, hardn.yml's one
// setting saved in place, under the settings it saves (decision 7 (a)),
// then the screen drawn again under the result from a fresh reading, the
// configuration adopting what was saved. Verify and Backup Runs read only.
// Run is the one entry point, which cmd/hardn's main menu item 7 calls
// (unit (d), next/s3-replace); docs/engine/screen-backup.md has its
// states, its words and its API.
//
// Each change runs through the glue every area's screen shares
// (pkg/screens/screenkit). The package reaches the host only through
// host.System, the control, pkg/host/backup and the engine (depguard rule
// screens-through-host), and renders only through pkg/ui and pkg/output.
package backup

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	backupctl "github.com/abbott/hardn/pkg/engine/controls/backup"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// Options is what the Backup screen runs with: what cmd/hardn has when its
// Backup menu item is chosen. It has no Facts, which the DNS screen's
// Options has: Backup's control reads no host fact (its Check ignores
// them, and it applies on every host), and the engine establishes its own
// when it plans.
type Options struct {
	// Prompter asks the questions and writes the screen through the one
	// line reader, rendering with its Ctx, whose Env is the terminal's.
	Prompter nav.Prompter
	// System is the host: the screen reads it, and the engine changes it.
	System host.System
	// Config is hardn.yml as loaded, whose backup settings the screen
	// shows and its items change. A change plans a copy with the setting
	// it changes; once the change has applied and hardn.yml holds the bytes
	// it saved, Config adopts the copy, so that the next change's save is
	// not refused as CONFIG_CHANGED. Its other settings stay as they are in
	// memory: the save writes the backup settings alone. Required.
	Config *config.Config
	// NewEngine returns the engine a change plans and applies with, built
	// as the caller builds it (engine.New over System with its options),
	// progress as its Options.Progress; one per change, made when the
	// change is planned. Its backups may be hardn.yml's as loaded: Backup's
	// change is planned under the settings it saves (decision 7 (a)).
	// Required.
	NewEngine func(progress func(engine.Event)) *engine.Engine
	// ApplyContext, when set, returns the context a change's apply runs
	// under, made from the screen's, and its stop, called once the apply
	// has returned; one per apply (screenkit.Planning.ApplyContext). Nil
	// applies under the screen's context.
	ApplyContext func(ctx context.Context) (context.Context, context.CancelFunc)
	// DryRun stops each change at its plan: nothing applied or saved.
	// Verify and Backup Runs read only either way.
	DryRun bool
	// LogPath is the log a failure's report names; "" names none.
	LogPath string
	// Now is the clock output.AfterFlow counts from; nil is time.Now.
	Now func() time.Time
	// Ran, when set, is given what each change did once its flow has
	// ended, for a change that was planned: for the audit log.
	Ran func(output.Run)
}

// Run shows the Backup screen and runs the items the user chooses, until
// they go back with 0 (nil) or quit with q (nav.ErrQuit); the end of the
// input and Ctrl-C abort (prompt.ErrAborted). Each round reads the backup
// settings with Backup's control (backupctl.Read, against o.Config) and
// the runs under the path (backup.Runs), writing nothing, and draws the
// screen under what the last item wrote. Items 1 and 2 are changes on the
// engine, planned, reviewed, confirmed y/N and applied, hardn.yml's one
// setting saved; n changes nothing, and a dry run stops at the plan. Item
// 1 is refused before it asks where Check refuses the path (decision 7
// (a)); items 1 and 2 with no hardn.yml read. Verify (3) and Backup Runs
// (4) read only. Run returns an error wrapping fs.ErrInvalid, writing
// nothing, without a Prompter's In and Out, a System, a Config or
// NewEngine.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil || o.Config == nil || o.NewEngine == nil {
		return fmt.Errorf("backup screen: %w: it needs a Prompter with In and Out, a System, a Config and NewEngine", fs.ErrInvalid)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	r := &runner{o: o, s: screenkit.Session{Name: "backup screen", Prompter: o.Prompter, System: o.System, Config: o.Config,
		NewEngine: o.NewEngine, ApplyContext: o.ApplyContext, DryRun: o.DryRun, LogPath: o.LogPath, Now: o.Now, Ran: o.Ran}}
	return o.Prompter.Run(func() nav.Screen { return r.draw(ctx) })
}

// runner is one run of the Backup screen: its options, and the session
// its changes run with.
type runner struct {
	o Options
	s screenkit.Session
}

// draw reads the backup settings and returns the screen, its items bound
// to their actions.
func (r *runner) draw(ctx context.Context) nav.Screen {
	h := r.read(ctx)
	sub := h.view.SubMenu()
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: r.action(ctx, h, it.Key)})
	}
	return sub.Screen(r.o.Prompter.Ctx, menu)
}

// reading is one reading of the backup settings, as the configuration
// holds them: the control's, the runs under the path, and the screen's
// view of them.
type reading struct {
	backupctl.Reading
	// runs are the runs under the path, newest first, read only where
	// Check takes the path and it is there; runsErr is backup.Runs'
	// error, beside the runs it could read.
	runs    []hostbackup.Manifest
	runsErr error
	// notLoaded are the words of the save's refusal with no hardn.yml
	// read (CONFIG_NOT_LOADED), which items 1 and 2 say before asking;
	// nil when hardn.yml was read.
	notLoaded []string
	view      screen.Backup
}

// read reads the backup settings as Backup's control reads them, against
// the configuration the screen holds (backupctl.Read: Check, writing
// nothing, an empty path the default), and the runs under the path where
// Check takes it and it is there (backup.Runs).
func (r *runner) read(ctx context.Context) reading {
	sys, cfg := r.o.System, r.o.Config
	h := reading{Reading: backupctl.Read(ctx, sys, cfg),
		notLoaded: screenkit.NotLoaded(ctx, sys, cfg, backupctl.ChangeSave, backupctl.SettingEnabled, backupctl.SettingPath)}
	if h.Err == nil && h.Made {
		h.runs, h.runsErr = hostbackup.Runs(ctx, sys, h.Settings.Path)
	}
	h.view = screen.Backup{Enabled: h.Settings.Enabled, Path: h.Settings.Path, Note: h.Note(), PathState: pathState(h.Reading),
		Runs: len(h.runs), RunsRead: h.Err == nil && (h.runsErr == nil || len(h.runs) > 0)}
	return h
}

// pathState is Check's verdict on the path r read, as the screen draws it.
func pathState(r backupctl.Reading) screen.BackupPathState {
	switch {
	case r.Err == nil:
		return screen.BackupPathTaken
	case r.Note() == backupctl.DetailCantTell:
		return screen.BackupPathUnknown
	}
	return screen.BackupPathRefused
}
