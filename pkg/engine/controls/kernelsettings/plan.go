package kernelsettings

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	khost "github.com/abbott/hardn/pkg/host/sysctl"
)

// Errors of the refusals and of a failed apply, testable with errors.Is,
// each the Err of a *steps.Refusal, wrapping the cause where there is one.
var (
	// ErrNotRead: a kernel setting, or a file that sets one, is not read.
	ErrNotRead = errors.New("the kernel's settings are not read, so no change is planned")
	// ErrOutside: a file that is not hardn's is where hardn's file goes.
	ErrOutside = errors.New("a file that is not hardn's is where hardn's kernel settings go")
	// ErrNoTool: the host has no sysctl.
	ErrNoTool = errors.New("the host has no sysctl")
	// ErrContainer: a container's kernel is its host's.
	ErrContainer = errors.New("a container's kernel is its host's")
	// ErrNothingToRemove: hardn set no kernel settings.
	ErrNothingToRemove = errors.New("hardn set no kernel settings")
	// ErrChangedSince: hardn's file changed since the plan.
	ErrChangedSince = errors.New("hardn's kernel settings file changed since the plan")
	// ErrNotSet: a setting did not take the value hardn set. The change is
	// rolled back: the file and every value as they were.
	ErrNotSet = errors.New("a kernel setting did not take its value")
)

// The refusals' codes, the registry's for the errors they wrap (pkg/output).
const (
	codeNotRead   = "KERNEL_SETTINGS_NOT_READ"
	codeOutside   = "KERNEL_SETTINGS_FILE_NOT_HARDNS"
	codeNoTool    = "KERNEL_SETTINGS_NO_SYSCTL"
	codeContainer = "KERNEL_SETTINGS_CONTAINER"
	codeNothing   = "KERNEL_SETTINGS_NOTHING_TO_REMOVE"
	codeChanged   = "KERNEL_SETTINGS_CHANGED_SINCE"
	codeNotSet    = "KERNEL_SETTINGS_NOT_SET"
	tryAgain      = "fix that, then try again"
)

// The changes' ids, under ID.
const (
	ChangeFile    = ID + "/file"
	ChangeApply   = ID + "/apply"
	ChangeRestore = ID + "/restore"
)

// The review's notices (gallery-sysctl.md §6; F5).
const (
	NoticeApply  = "hardn sets these now. Remove settings puts the earlier values back."
	NoticeTrace  = "With Process tracing set, gdb -p and strace -p need root."
	NoticeRoutes = "Forwarding, Sending redirects and Reverse-path filter are left as they are."
	NoticeBoot   = "The sysctl service isn't enabled, so these are not set at boot. To fix: " + BootFix + "."
	NoticeRemove = "The earlier values are set again."
	// BootFix is the command that makes OpenRC apply the files at boot.
	// hardn does not run it.
	BootFix = "rc-update add sysctl boot"
)

// Notice is the review's notice for action a on the host as r reads it.
func Notice(r khost.Reading, a Action) string {
	if a == ActionRemove {
		return NoticeRemove
	}
	words := []string{NoticeApply}
	for _, row := range r.Todo() {
		if row.Spec.Kind == khost.Plain && row.Spec.Keys[0] == khost.PtraceKey {
			words = append(words, NoticeTrace)
		}
	}
	if r.LeftAlone() {
		words = append(words, NoticeRoutes)
	}
	if r.OpenRC && r.Boot == detect.No {
		words = append(words, NoticeBoot)
	}
	return strings.Join(words, " ")
}

// Plan reads the host and plans what Action asks: nothing at ActionNone
// (engine.ErrNotMigrated); Apply (planApply) or Remove (planRemove), each
// refusing with a *steps.Refusal before anything is written. A host with
// nothing below the baseline plans nothing.
func (c Settings) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	switch c.Action {
	case ActionNone:
		return nil, engine.ErrNotMigrated
	case ActionApply:
		return planApply(Read(ctx, sys, in.Facts))
	case ActionRemove:
		return planRemove(Read(ctx, sys, in.Facts))
	}
	return nil, fmt.Errorf("kernelsettings: action %d: %w", c.Action, fs.ErrInvalid)
}

// hostWords is a reading's error in one safe line (steps.HostWords).
func hostWords(err error) string { return steps.HostWords(err, "sysctl: ", khost.ErrMalformed) }

// usable refuses a host hardn sets no kernel settings on: a container; one
// with a setting or a file that is not read; one with a file that is not
// hardn's where hardn's goes; one with no sysctl.
func usable(r khost.Reading) error {
	row, unread := r.Unread()
	switch {
	case r.Container:
		return steps.Refuse(codeContainer, ErrContainer, nil, "This is a container: its kernel is its host's", "")
	case r.Err != nil:
		return steps.Refuse(codeNotRead, ErrNotRead, r.Err, "hardn can't read the kernel's settings: "+hostWords(r.Err), tryAgain)
	case unread:
		return steps.Refuse(codeNotRead, ErrNotRead, row.Err, "hardn can't read "+row.Spec.Name+": "+hostWords(row.Err), tryAgain)
	case r.Foreign:
		return steps.Refuse(codeOutside, ErrOutside, nil, khost.File+" is not hardn's file, and hardn leaves it as it is", "move that file away, then try again")
	case r.Tool == khost.NoTool:
		return steps.Refuse(codeNoTool, ErrNoTool, nil, "This host has no sysctl, and hardn installs none", "install it, then try again")
	}
	return nil
}

// set is the command that sets key to n now.
func set(kv khost.KV) []string {
	return []string{khost.Command, "-w", kv.Key + "=" + strconv.Itoa(kv.N)}
}

// unchanged refuses, at the apply, a file of hardn's that is not as the
// plan read it: before is its text then, nil where it was not there.
func unchanged(ctx context.Context, sys host.System, before *string) error {
	now, ok, err := host.ReadFileIfThere(ctx, sys, khost.File)
	switch {
	case err != nil:
		return err
	case ok != (before != nil), ok && string(now) != *before:
		return steps.Refuse(codeChanged, ErrChangedSince, nil, khost.File+" changed since the plan", "try again")
	}
	return nil
}

// guarded is ch, its apply refused where hardn's file is not as the plan
// read it; nothing is then written, and nothing is left to undo.
func guarded(ch engine.Change, before *string) engine.Change {
	apply := ch.Apply
	ch.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
		if err := unchanged(ctx, sys, before); err != nil {
			return engine.Ran{Rollback: &engine.Undo{}}, err
		}
		return apply(ctx, sys, run)
	}
	return ch
}

// notSet is a failed apply in the user's words.
func notSet(what string, cause error) error {
	return steps.Refuse(codeNotSet, ErrNotSet, cause, what, "")
}

// said is what the tool said of a failure: its first line that names one.
// busybox's sysctl exits 0 whether or not a key was set (F4), so its
// stderr is read for the word.
func said(res host.Result, busybox bool) string {
	for _, line := range strings.Split(string(res.Stderr), "\n") {
		if line = strings.TrimSpace(line); line != "" && (!busybox || strings.Contains(line, "error")) {
			return logview.Safe(strings.TrimPrefix(line, khost.Command+": "))
		}
	}
	return ""
}

// verify reads each of want's keys back; a key that is gone is left out,
// and one that does not read its value is ErrNotSet, named.
func verify(ctx context.Context, sys host.System, want []khost.KV) error {
	for _, kv := range want {
		switch v := khost.ReadValue(ctx, sys, kv.Key); {
		case errors.Is(v.Err, fs.ErrNotExist):
		case v.Err != nil:
			return notSet("hardn can't read "+kv.Key+" back", v.Err)
		case v.N != kv.N:
			return notSet(fmt.Sprintf("%s is %d, not %d", kv.Key, v.N, kv.N), nil)
		}
	}
	return nil
}

// run runs the commands of a change that sets values, then reads want
// back: by the exit status, by busybox's stderr, and by the values
// themselves. Where the tool is not found nothing ran, and nothing is left
// to undo.
func run(ctx context.Context, sys host.System, busybox bool, argvs [][]string, want []khost.KV, detail string) (engine.Ran, error) {
	for _, argv := range argvs {
		res, err := sys.Run(ctx, argv[0], argv[1:]...)
		words := said(res, busybox)
		switch {
		case errors.Is(err, host.ErrNotFound):
			return engine.Ran{Rollback: &engine.Undo{}}, err
		case err != nil && words != "":
			return engine.Ran{}, notSet("sysctl: "+words, err)
		case err != nil:
			return engine.Ran{}, notSet("sysctl failed", err)
		case busybox && words != "":
			return engine.Ran{}, notSet("sysctl: "+words, nil)
		}
	}
	return engine.Ran{Detail: detail}, verify(ctx, sys, want)
}

// values is a change that runs argvs and reads want back (run), after the
// change after where there is one; undo are the commands a rollback runs,
// once the file is back.
func values(id, title string, busybox bool, argvs, undo [][]string, want []khost.KV, after []string, detail string) engine.Change {
	ch := engine.Change{ID: id, Summary: title, Title: title, Stage: engine.StageReload,
		Resources: []string{ID}, After: after,
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			return run(ctx, sys, busybox, argvs, want, detail)
		}}
	for _, argv := range argvs {
		ch.Commands = append(ch.Commands, host.FormatArgv(argv))
	}
	for _, argv := range undo {
		ch.Rollback.Steps = append(ch.Rollback.Steps, revert.Step{Op: revert.OpRun, Argv: argv, Why: ID})
	}
	return ch
}

// planApply is Apply settings (decisions 4 to 13). Refused on a host hardn
// sets nothing on (usable). hardn's file is written with a line for each
// key below the baseline that no later file holds, under the value it has
// now; then "sysctl -w" sets each of those keys, and no other, and each is
// read back. The file is not loaded: that would set every line it has
// again, a held key and a row left alone among them. A value that did not
// take fails the change: the engine puts the file back, and sets back each
// value. A row a later file holds, a row left alone and a value at or
// above the baseline are not written.
func planApply(r khost.Reading) ([]engine.Change, error) {
	if err := usable(r); err != nil {
		return nil, err
	}
	if len(r.Todo()) == 0 {
		return nil, nil // nothing below the baseline that hardn sets
	}
	data := r.Next().Bytes()
	var cs []engine.Change
	var before *string
	if r.Own != nil {
		before = &r.Own.Text
	}
	var after []string
	if before == nil || *before != string(data) {
		file := steps.WriteOwnFile(steps.OwnFile{Control: ID, ID: ChangeFile, Title: "Write hardn's kernel settings", Path: khost.File, Before: before, Data: data, MkDir: true})
		cs, after = append(cs, guarded(file, before)), []string{ChangeFile}
	}
	var argvs, undo [][]string
	var want []khost.KV
	for _, c := range r.Changes() {
		kv := khost.KV{Key: c.Key, N: c.To}
		argvs, want = append(argvs, set(kv)), append(want, kv)
		undo = append(undo, set(khost.KV{Key: c.Key, N: c.From}))
	}
	return append(cs, values(ChangeApply, "Set the values now", r.Tool == khost.Busybox, argvs, undo, want, after, "kernel settings: set and read back")), nil
}

// planRemove is Remove settings (decision 14): hardn's file deleted, and
// each key set back to the value the file recorded, where the kernel still
// has the key; then each is read back. A rollback puts the file back and
// sets each value back to what it read. Refused on a host hardn sets nothing on, and where
// there is no file of hardn's.
func planRemove(r khost.Reading) ([]engine.Change, error) {
	if err := usable(r); err != nil {
		return nil, err
	}
	if r.Own == nil {
		return nil, steps.Refuse(codeNothing, ErrNothingToRemove, nil, "hardn set no kernel settings here", "")
	}
	text := r.Own.Text
	del := guarded(steps.DeleteOwnFile(ID, ChangeFile, "Delete hardn's kernel settings", khost.File, text, "kernel settings: deleted"), &text)
	var back, undo [][]string
	var want []khost.KV
	for _, kv := range r.Own.Restore() {
		if now, ok := r.Current(kv.Key); ok {
			back, want = append(back, set(kv)), append(want, kv)
			undo = append(undo, set(khost.KV{Key: kv.Key, N: now}))
		}
	}
	if len(back) == 0 {
		return []engine.Change{del}, nil
	}
	return []engine.Change{del, values(ChangeRestore, "Set the earlier values", r.Tool == khost.Busybox, back, undo, want, []string{ChangeFile}, "kernel settings: earlier values set")}, nil
}
