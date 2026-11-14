package steps

import (
	"context"
	"errors"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The changes a control makes to set a service's setting
// (docs/engine/gallery-timesync.md §5): a file of the host's rewritten, a
// file of hardn's own written, and the service restarted. Each takes the
// control's id, which is the change's resource and the why of its undo, and
// the change's own id and title.

// ServiceArgv is the command that does verb ("restart") to a service, by
// the init system that booted the host: "systemctl verb unit" under
// systemd, "rc-service service verb" under OpenRC. ok is false where the
// init that booted the host is neither, or the service has no name under it
// ("").
func ServiceArgv(f detect.Facts, unit, service, verb string) (argv []string, ok bool) {
	if f.Init.Running != detect.Yes {
		return nil, false
	}
	switch {
	case f.Init.Kind == detect.Systemd && unit != "":
		return []string{"systemctl", verb, unit}, true
	case f.Init.Kind == detect.OpenRC && service != "":
		return []string{"rc-service", service, verb}, true
	}
	return nil, false
}

// RestartService restarts a service with argv (ServiceArgv), after the
// change after. A rollback restarts it again, once the files are back; not
// where the command was not found, as nothing ran.
func RestartService(control, id, title, service string, argv []string, after string) engine.Change {
	undo := engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: argv, Why: control}}}
	return engine.Change{ID: id, Summary: title, Title: title, Target: service, Commands: []string{host.FormatArgv(argv)},
		Risk: engine.RiskServiceRestart, Stage: engine.StageReload, Resources: []string{control}, After: []string{after}, Rollback: undo,
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			_, err := sys.Run(ctx, argv[0], argv[1:]...)
			if errors.Is(err, host.ErrNotFound) {
				return engine.Ran{Rollback: &engine.Undo{}}, err
			}
			return engine.Ran{Detail: service + ": restarted"}, err
		}}
}

// Rewrite is a file of the host's that RewriteFile edits.
type Rewrite struct {
	// Control is the control's id; ID and Title the change's.
	Control, ID, Title string
	// Path is the file.
	Path string
	// Detail is what the apply says it did ("syslogd's options: written").
	Detail string
	// Unread is the plan's refusal of a file that cannot be read, from the
	// read's error; Changed the apply's of a file that is no longer as the
	// plan read it. Both are the control's, with its codes and words.
	Unread  func(err error) error
	Changed error
}

// RewriteFile plans r.Path rewritten by edit: the file is read, edited, and
// shown as a diff. The apply reads it again, refuses one that changed since
// the plan (r.Changed), and writes it keeping its mode and owner. A file
// that cannot be read is r.Unread's refusal, and edit's error is returned
// as it is, for the control to say in its own words.
func RewriteFile(ctx context.Context, sys host.System, r Rewrite, edit func([]byte) ([]byte, error)) (engine.Change, error) {
	before, err := sys.ReadFile(ctx, r.Path)
	if err != nil {
		return engine.Change{}, r.Unread(err)
	}
	after, err := edit(before)
	if err != nil {
		return engine.Change{}, err
	}
	a, b := string(before), string(after)
	return engine.Change{ID: r.ID, Summary: r.Title + " in " + r.Path, Title: r.Title, Target: r.Path,
		Diff: engine.FileDiff(r.Path, &a, &b), Stage: engine.StageFiles, Resources: []string{r.Control}, Backups: []string{r.Path},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			now, err := sys.ReadFile(ctx, r.Path)
			switch {
			case err != nil:
				return engine.Ran{}, err
			case string(now) != a:
				return engine.Ran{}, r.Changed
			}
			return engine.Ran{Detail: r.Detail}, host.WriteFileAtomicKeep(ctx, sys, r.Path, after, 0o644, 0, 0)
		}}, nil
}

// OwnFile is a file of hardn's own that WriteOwnFile writes.
type OwnFile struct {
	// Control is the control's id; ID and Title the change's.
	Control, ID, Title string
	// Path is the file, Before its text now, nil where it is not there,
	// and Data what is written, root's, 0644.
	Path   string
	Before *string
	Data   []byte
	// After are the ids of the changes this one follows.
	After []string
	// Check, when set, is run once the file is written.
	Check *FileCheck
}

// FileCheck is the service's own check of its settings with hardn's file
// in place ("rsyslogd -N1").
type FileCheck struct {
	// Argv is the command.
	Argv []string
	// Refused is the change's failure where the command fails: the
	// control's, in its words. The engine then puts the file back.
	Refused func(res host.Result, err error) error
	// Detail is what the apply says where it passes.
	Detail string
}

// WriteOwnFile writes hardn's own file, backed up first and written
// atomically, then runs o.Check where there is one: a failure there is the
// change failed, before any restart.
func WriteOwnFile(o OwnFile) engine.Change {
	text := string(o.Data)
	ch := engine.Change{ID: o.ID, Summary: o.Title + " " + o.Path, Title: o.Title, Target: o.Path,
		Diff:  engine.FileDiff(o.Path, o.Before, &text),
		Stage: engine.StageFiles, Resources: []string{o.Control}, Backups: []string{o.Path}, After: o.After}
	if o.Check != nil {
		ch.Commands = []string{host.FormatArgv(o.Check.Argv)}
	}
	ch.Apply = func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
		if err := sys.WriteFileAtomic(ctx, o.Path, o.Data, 0o644, 0, 0); err != nil {
			return engine.Ran{}, err
		}
		if o.Check == nil {
			return engine.Ran{}, nil
		}
		if res, err := sys.Run(ctx, o.Check.Argv[0], o.Check.Argv[1:]...); err != nil {
			return engine.Ran{}, o.Check.Refused(res, err)
		}
		return engine.Ran{Detail: o.Check.Detail}, nil
	}
	return ch
}
