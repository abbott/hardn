package backup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	backupctl "github.com/abbott/hardn/pkg/engine/controls/backup"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The items' words (docs/engine/screen-backup.md §5): the path field's
// question, origin/main's without its colon; the rest the adopted mock's,
// proposed (gallery-backup.md §6), but the refused path's first sentence,
// the control's Why, decided (control-backup.md §6).
const (
	pathTitle = "Enter new backup path"
	// The questions.
	askOff  = "Turn backups off?"
	askOn   = "Turn backups on?"
	askPath = "Use %s?"
	// Verify's report: Check took the path, there or not yet, and wrote
	// nothing.
	verifyMade     = "Only root can change %s."
	verifyNotYet   = "Only root can change %s, where the first backup makes %s."
	nothingWritten = "Nothing was written."
	// The second sentence of a refused path's words, after the control's
	// Why, by Check's refusal; and Check failing otherwise.
	startWithSlash = "Start it with /."
	onlyRoot       = "Choose a directory only root can change."
	another        = "Choose another path."
	cantCheck      = "Can't check %s: %s"
	// Backup Runs with no run, and runs that could not be read.
	noRuns       = "No change has been backed up yet."
	runsNotRead  = "Can't read the runs in %s: %s"
	someNotRead  = "Some runs can't be read: %s"
	runTimestamp = "20060102T150405Z"
)

// action returns what choosing the item keyed key does with the reading
// h: with no hardn.yml read, items 1 and 2 say that nothing can be saved;
// item 1, where Check refuses the path, says why (decision 7 (a)); else
// each item's change, Verify's report or the runs.
func (r *runner) action(ctx context.Context, h reading, key string) func() error {
	switch key {
	case screen.BackupStateItem:
		switch {
		case h.notLoaded != nil:
			return r.say(h.notLoaded...)
		case h.Err != nil:
			return r.say(refused(h.Settings.Path, h.Err))
		}
		return func() error { return r.turn(ctx, h) }
	case screen.BackupPathItem:
		if h.notLoaded != nil {
			return r.say(h.notLoaded...)
		}
		return func() error { return r.changePath(ctx, h) }
	case screen.BackupVerifyItem:
		return r.s.Say(verify(ctx, r.o.System, h.Reading))
	case screen.BackupRunsItem:
		return r.openRuns(h)
	}
	return func() error { return fmt.Errorf("backup screen: item %s has no action", key) } // not reached: SubMenu's items are those above
}

// say returns an action that writes paragraphs as one warning, set apart
// as an action's result is, and goes back to the screen.
func (r *runner) say(paragraphs ...string) func() error {
	return r.s.Say(screenkit.Warnings(paragraphs...))
}

// turn is item 1: backups turned the other way, through the flow
// (screenkit.Session.Change): the review, Backup State as the change leaves
// it, and turning them off, that rollback backups are still taken
// (decision 8); the plan, Backup's control's, the save of enableBackups
// from a copy of hardn.yml with it turned over, which the engine plans and
// applies under the settings it saves (decision 7 (a)); the question, y/N;
// the apply.
func (r *runner) turn(ctx context.Context, h reading) error {
	on, question := !h.Settings.Enabled, askOff
	if on {
		question = askOn
	}
	return r.s.Change(ctx, screenkit.Change{
		Step: h.view.TurnStep(),
		Flow: flow.Flow{
			Review:  func() flow.Review { return h.view.TurnReview() },
			Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: question} },
		},
		Controls: func(edited *config.Config) []engine.Control {
			edited.EnableBackups = on
			return backupctl.Controls(r.o.Config)
		},
	})
}

// changePath is item 2: a path asked and checked by Backup's control
// (backupctl.Field: decisions 5 and 6), each refusal under the field and
// the field asked again, the path in use a warning; then, through the
// flow, the review, Backup Path as the change leaves it with Check's
// note; the plan, the save of backupPath, planned and applied under the
// path it saves (decision 7 (a)), so that a host whose path Check refuses
// can take another; the question, y/N; the apply.
func (r *runner) changePath(ctx context.Context, h reading) error {
	sys := r.o.System
	var entry string
	field := &form.Text{Title: pathTitle, Validate: pathCheck(ctx, sys, h.Settings.Path), Value: &entry}
	return r.s.Change(ctx, screenkit.Change{
		Step: h.view.PathStep(),
		Flow: flow.Flow{
			Form: form.Form{Fields: []form.Field{field}},
			Review: func() flow.Review {
				edited := *r.o.Config
				edited.BackupPath = entry
				n := backupctl.Read(ctx, sys, &edited)
				return h.view.PathReview(entry, n.Note(), pathState(n))
			},
			Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: fmt.Sprintf(askPath, entry)} },
		},
		Controls: func(edited *config.Config) []engine.Control {
			edited.BackupPath = entry
			return backupctl.Controls(r.o.Config)
		},
	})
}

// pathCheck is the path field's check of the entry typed, with current the
// path in use: Backup's control's (backupctl.Field), each refusal in its
// words, the path in use a warning (nav.Warn), as the DNS screen warns of
// a nameserver configured already.
func pathCheck(ctx context.Context, sys host.System, current string) func(string) error {
	return func(entry string) error {
		var fe *backupctl.FieldError
		switch err := backupctl.Field(ctx, sys, current, entry); {
		case err == nil:
			return nil
		case !errors.As(err, &fe):
			return err // not reached: Field returns a *FieldError or nil
		case fe.Warn:
			return nav.Warn(fe.Words)
		}
		return errors.New(fe.Words)
	}
}

// refused are the words of err, Check's refusal of the backup path p, as
// Verify and item 1 say them: the control's Why (control-backup.md §6,
// decided), then the screen's own second sentence (gallery-backup.md §6,
// proposed), where the control's Fix names the item that changes the
// path; Check failing otherwise, "Can't check <path>: <error>.".
func refused(p string, err error) string {
	rf := backupctl.Refusal(p, err)
	if rf == nil {
		return screenkit.FullStop(fmt.Sprintf(cantCheck, p, oneLine(err)))
	}
	next := another
	switch {
	case errors.Is(err, hostbackup.ErrSettings):
		next = startWithSlash
	case errors.Is(err, hostbackup.ErrUnsafeDir):
		next = onlyRoot
	}
	return screenkit.FullStop(rf.Why) + " " + next
}

// verify is item 3's report on r, the reading (decision 3: Check, writing
// nothing, under plan only too): where Check refuses the path, its words
// as a warning, then that nothing was written; else who can change the
// path, or, not made yet, the directory the first backup makes it in,
// and that nothing was written.
func verify(ctx context.Context, sys host.System, r backupctl.Reading) component.Messages {
	p := r.Settings.Path
	switch {
	case r.Err != nil:
		return component.Messages{{Kind: component.MessageWarn, Text: refused(p, r.Err)}, {Text: nothingWritten}}
	case r.Made:
		return component.Messages{{Text: fmt.Sprintf(verifyMade, p) + " " + nothingWritten}}
	}
	return component.Messages{{Text: fmt.Sprintf(verifyNotYet, deepest(ctx, sys, p), p) + " " + nothingWritten}}
}

// deepest is the deepest directory above p that is there, which Check
// checked where p is not there yet: the one the first backup makes p in.
func deepest(ctx context.Context, sys host.System, p string) string {
	d := filepath.Dir(filepath.Clean(p))
	for d != filepath.Dir(d) {
		if fi, err := sys.Stat(ctx, d); err == nil && fi.IsDir() {
			return d
		}
		d = filepath.Dir(d)
	}
	return d
}

// openRuns is item 4 (decision 9, read only): the runs under the path in
// use, newest first, each leading to its files; where Check refuses the
// path, its words, as for item 1; where none could be read, why; with
// none, a line saying so. Runs that could not be read, beside others, are
// said above the list.
func (r *runner) openRuns(h reading) func() error {
	p := h.Settings.Path
	switch {
	case h.Err != nil:
		return r.say(refused(p, h.Err))
	case h.runsErr != nil && len(h.runs) == 0:
		return r.say(screenkit.FullStop(fmt.Sprintf(runsNotRead, p, oneLine(h.runsErr))))
	case len(h.runs) == 0:
		return r.s.Say(component.Messages{{Text: noRuns}})
	}
	runs := make([]screen.BackupRun, len(h.runs))
	for i, m := range h.runs {
		runs[i] = runView(m)
	}
	return func() error {
		if h.runsErr != nil {
			if err := r.say(screenkit.FullStop(fmt.Sprintf(someNotRead, oneLine(h.runsErr))))(); err != nil {
				return err
			}
		}
		return r.o.Prompter.Run(func() nav.Screen {
			vm := screen.BackupRunsMenu(runs)
			menu := nav.Menu{Exit: vm.Menu.Exit}
			for i, it := range vm.Menu.Items {
				menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return r.openRun(runs[i]) }})
			}
			return vm.Screen(r.o.Prompter.Ctx, menu)
		})
	}
}

// openRun is one run's screen, read only, until the user goes back to the
// list.
func (r *runner) openRun(run screen.BackupRun) error {
	p := r.o.Prompter
	return p.Run(func() nav.Screen {
		vm := run.SubMenu()
		return vm.Screen(p.Ctx, nav.Menu{Exit: vm.Menu.Exit})
	})
}

// runView is the run m as Backup Runs shows it: its id, when it was taken,
// read from the id (backup.NewRunID: UTC, to the second), and each path
// it backed up and what it was.
func runView(m hostbackup.Manifest) screen.BackupRun {
	v := screen.BackupRun{ID: m.RunID}
	if t, err := time.Parse(runTimestamp, strings.SplitN(m.RunID, "-", 2)[0]); err == nil {
		v.Taken = t
	}
	for _, e := range m.Entries {
		f := screen.BackupFile{Path: e.Path, Absent: e.Kind == hostbackup.KindAbsent, Link: e.SymlinkTarget, TakenAnyway: e.TakenAnyway}
		if e.File != nil {
			f.Size = e.File.Size
		}
		v.Files = append(v.Files, f)
	}
	return v
}

// oneLine is err's text on one line: errors.Join's lines joined with "; ".
func oneLine(err error) string {
	return strings.ReplaceAll(err.Error(), "\n", "; ")
}
