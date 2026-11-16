package timesync

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	tshost "github.com/abbott/hardn/pkg/host/timesync"
)

// Errors of the refusals, testable with errors.Is, each the Err of a
// *steps.Refusal, wrapping the cause where there is one.
var (
	// ErrNoServers: hardn.yml names no time servers.
	ErrNoServers = errors.New("hardn.yml names no time servers")
	// ErrBadServers: hardn.yml's ntpServers is not valid.
	ErrBadServers = errors.New("hardn.yml's ntpServers is not valid")
	// ErrNotRead: the time service, or its settings, are not read.
	ErrNotRead = errors.New("the time service's settings are not read, so no change is planned")
	// ErrOutside: a file that is not hardn's sets time servers.
	ErrOutside = errors.New("time servers are set outside hardn")
	// ErrNoService: the host has no time service, or it is not running.
	ErrNoService = errors.New("no time service is running")
	// ErrContainer: a container's clock is its host's.
	ErrContainer = errors.New("a container's host keeps the time")
	// ErrNothingToRemove: hardn did not set time servers.
	ErrNothingToRemove = errors.New("hardn did not set time servers")
	// ErrChangedSince: the time service's settings changed since the plan.
	ErrChangedSince = errors.New("the time service's settings changed since the plan")
)

// The refusals' codes, the registry's for the errors they wrap (pkg/output).
const (
	codeNoServers  = "NTP_NO_SERVERS"
	codeBadServers = "NTP_BAD_SERVERS"
	codeNotRead    = "NTP_NOT_READ"
	codeOutside    = "NTP_SET_OUTSIDE"
	codeNoService  = "NTP_NO_SERVICE"
	codeContainer  = "NTP_CONTAINER"
	codeNothing    = "NTP_NOTHING_TO_REMOVE"
	codeChanged    = "NTP_CHANGED_SINCE"
	tryAgain       = "fix that, then try again"
)

// The changes' ids, under ID. A file of the host's that hardn edits has
// ChangeEdit and its base name ("time.sync/edit/chrony.conf").
const (
	ChangeFile    = ID + "/file"
	ChangeEdit    = ID + "/edit/"
	ChangeRestart = ID + "/restart"
)

// The review's notices (gallery-timesync.md §6, decisions 4 and 18).
const (
	NoticeSet     = "hardn's servers take the place of the host's own. Remove servers puts those back."
	NoticeNTS     = "The host's own servers are authenticated (NTS). hardn's are not."
	NoticeCluster = "On a cluster, set every node the same way: the nodes need synchronised clocks."
	// NoticePiClock: on Raspberry Pi OS (design-custom-distros.md,
	// decision 3 (a)).
	NoticePiClock = "Most Raspberry Pi models have no clock of their own: if no server is reached, the time is wrong after a reboot."
	NoticeRemove  = "The host's own servers are used again."
)

// Notice is the review's notice for action a on the host as r reads it, on
// a host of f: Set's says so where it takes the place of authenticated
// servers, on Proxmox VE, whose cluster needs its nodes' clocks to agree,
// and on Raspberry Pi OS, where most models have no clock of their own.
func (r Reading) Notice(a Action, f detect.Facts) string {
	if a == ActionRemove {
		return NoticeRemove
	}
	words := []string{NoticeSet}
	if r.Host.NTS() {
		words = append(words, NoticeNTS)
	}
	switch {
	case f.Platform.Distro == detect.DistroProxmox:
		words = append(words, NoticeCluster)
	case f.Platform.Variant == detect.VariantRaspiOS:
		words = append(words, NoticePiClock)
	}
	return strings.Join(words, " ")
}

// Plan reads the host and plans what Action asks: nothing at ActionNone
// (engine.ErrNotMigrated); Set (planSet) or Remove (planRemove), each
// refusing with a *steps.Refusal before anything is written. A host whose
// service already uses hardn.yml's servers alone plans nothing.
func (c Sync) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	cfg := in.Config
	if cfg == nil {
		cfg = c.Config
	}
	switch c.Action {
	case ActionNone:
		return nil, engine.ErrNotMigrated
	case ActionSet:
		return planSet(ctx, sys, in.Facts, Read(ctx, sys, in.Facts, cfg))
	case ActionRemove:
		return planRemove(ctx, sys, in.Facts, Read(ctx, sys, in.Facts, cfg))
	}
	return nil, fmt.Errorf("timesync: action %d: %w", c.Action, fs.ErrInvalid)
}

// hostWords is a reading's error in one safe line (steps.HostWords).
func hostWords(err error) string { return steps.HostWords(err, "timesync: ", tshost.ErrMalformed) }

// settings names what is read of service s.
func settings(s tshost.Service) string {
	switch s {
	case tshost.Chrony:
		return "chrony's settings"
	case tshost.Timesyncd:
		return "systemd-timesyncd's settings"
	case tshost.Busybox:
		return "ntpd's options"
	}
	return "the time service"
}

// usable refuses a host hardn sets no time servers on: a container; one
// whose service or settings are not read; one with no time service, or
// with one that is not running, which hardn does not start (decision 5).
func usable(r Reading) error {
	h := r.Host
	switch {
	case h.Container:
		return steps.Refuse(codeContainer, ErrContainer, nil, "This is a container: its host keeps the time", "")
	case h.Err != nil:
		return steps.Refuse(codeNotRead, ErrNotRead, h.Err, "hardn can't read "+settings(h.Service)+": "+hostWords(h.Err), tryAgain)
	case h.Service == tshost.NoService:
		return steps.Refuse(codeNoService, ErrNoService, nil, "This host has no time service hardn sets, and hardn installs none", "install one, then try again")
	case h.Running != detect.Yes:
		return steps.Refuse(codeNoService, ErrNoService, nil, h.Service.String()+" is not running, and hardn does not start it", "start it, then try again")
	}
	return nil
}

// restart is the command that restarts the service, by the init system
// that booted the host (steps.ServiceArgv), its name there, and the
// change's title. An init the service has no name under is refused as not
// read: the reading tells whether the service runs by the same init, so
// usable refuses such a host first.
func restart(f detect.Facts, h tshost.Reading) (argv []string, service, title string, err error) {
	service = h.Unit
	if f.Init.Kind == detect.OpenRC {
		service = h.RC
	}
	argv, ok := steps.ServiceArgv(f, h.Unit, h.RC, "restart")
	if !ok {
		return nil, "", "", steps.Refuse(codeNotRead, ErrNotRead, nil, "hardn can't read "+settings(h.Service)+": whether "+h.Service.String()+" runs", tryAgain)
	}
	title = "Restart " + h.Service.String()
	if h.Service == tshost.Busybox {
		title = "Restart ntpd"
	}
	return argv, service, title, nil
}

// planSet is Set servers (decisions 1 to 8). Refused: no servers in
// hardn.yml, or ones that are not valid; a host hardn sets nothing on
// (usable); servers set by a file that is neither the service's own nor
// hardn's, or cleared by one read after hardn's. Then, by service: chrony's servers (setChrony); hardn's drop-in
// for systemd-timesyncd, and its restart; ntpd's peers in /etc/conf.d/ntpd,
// and its restart.
func planSet(ctx context.Context, sys host.System, f detect.Facts, r Reading) ([]engine.Change, error) {
	switch {
	case r.WantErr != nil:
		why := strings.TrimPrefix(r.WantErr.Error(), config.ErrNTPServers.Error()+": ")
		return nil, steps.Refuse(codeBadServers, ErrBadServers, r.WantErr, "hardn.yml's ntpServers is not valid: "+logview.Safe(why), tryAgain)
	case len(r.Want) == 0:
		return nil, steps.Refuse(codeNoServers, ErrNoServers, nil, "hardn.yml names no time servers", "add ntpServers, then try again")
	}
	if err := usable(r); err != nil {
		return nil, err
	}
	h := r.Host
	if o := h.Outside(); o != nil {
		return nil, steps.Refuse(codeOutside, ErrOutside, nil, logview.Safe(o.File)+" already sets time servers, and hardn leaves it as it is", "remove that setting, then try again")
	}
	if h.Cleared != "" {
		return nil, steps.Refuse(codeOutside, ErrOutside, nil, logview.Safe(h.Cleared)+" clears the time servers hardn sets, and hardn leaves it as it is", "remove that setting, then try again")
	}
	alone := !slices.ContainsFunc(h.Servers, func(s tshost.Server) bool { return s.From == tshost.FromFile })
	if alone && slices.Equal(h.Own(), r.Want) {
		return nil, nil // as asked
	}
	argv, service, title, err := restart(f, h)
	if err != nil {
		return nil, err
	}
	var cs []engine.Change
	switch h.Service {
	case tshost.Chrony:
		return setChrony(ctx, sys, h, r.Want, func(after string) engine.Change {
			return steps.RestartService(ID, ChangeRestart, title, service, argv, after)
		})
	case tshost.Timesyncd:
		data, err := tshost.TimesyncdFile(r.Want)
		if err != nil {
			return nil, badServers(err)
		}
		ch, err := ownFile(ctx, sys, tshost.TimesyncdDropIn, data)
		if err != nil {
			return nil, err
		}
		cs = []engine.Change{ch}
	case tshost.Busybox:
		ch, changed, err := rewrite(ctx, sys, tshost.ConfD, "Set ntpd's servers", "ntpd's options: written", func(_ string, data []byte) ([]byte, error) {
			return tshost.SetPeers(data, r.Want)
		})
		if err != nil || !changed {
			return nil, err
		}
		cs = []engine.Change{ch}
	}
	return append(cs, steps.RestartService(ID, ChangeRestart, title, service, argv, cs[0].ID)), nil
}

// badServers refuses servers the host package does not write: the second
// check (docs/engine/screen-timesync.md §2).
func badServers(err error) error {
	return steps.Refuse(codeBadServers, ErrBadServers, err, "hardn.yml's ntpServers is not valid", tryAgain)
}

// ownFile writes hardn's own file at name (steps.WriteOwnFile), its
// directory made where it is not there. A file there that does not open
// with hardn's first line is not hardn's, whatever it holds, and is a
// refusal: hardn does not write over it.
func ownFile(ctx context.Context, sys host.System, name string, data []byte) (engine.Change, error) {
	var before *string
	now, err := sys.ReadFile(ctx, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return engine.Change{}, steps.Refuse(codeNotRead, ErrNotRead, err, "hardn can't read "+name, tryAgain)
	case !strings.HasPrefix(string(now), tshost.Header):
		return engine.Change{}, steps.Refuse(codeOutside, ErrOutside, nil, name+" is not hardn's file, and hardn leaves it as it is", "move that file away, then try again")
	default:
		text := string(now)
		before = &text
	}
	return steps.WriteOwnFile(steps.OwnFile{Control: ID, ID: ChangeFile, Title: "Write hardn's time servers", Path: name, Before: before, Data: data, MkDir: true}), nil
}

// rewrite edits a file of the host's with edit (steps.RewriteFile): read,
// edited, shown as a diff, read again at apply and refused where it changed
// since, its mode and owner kept. changed is false where edit leaves it as
// it is: there is no change to make. edit's errors are said in the
// control's words.
func rewrite(ctx context.Context, sys host.System, name, title, detail string, edit func(name string, data []byte) ([]byte, error)) (ch engine.Change, changed bool, err error) {
	ch, err = steps.RewriteFile(ctx, sys, steps.Rewrite{Control: ID, ID: ChangeEdit + path.Base(name), Title: title, Path: name, Detail: detail,
		Unread: func(err error) error {
			return steps.Refuse(codeNotRead, ErrNotRead, err, "hardn can't read "+name, tryAgain)
		},
		Changed: steps.Refuse(codeChanged, ErrChangedSince, nil, name+" changed since the plan", "try again"),
	}, func(before []byte) ([]byte, error) {
		after, err := edit(name, before)
		switch {
		case errors.Is(err, tshost.ErrNotOwn):
			return nil, steps.Refuse(codeNothing, ErrNothingToRemove, err, name+" no longer holds what hardn set", "edit it by hand")
		case errors.Is(err, tshost.ErrServer):
			return nil, badServers(err)
		case err != nil:
			return nil, steps.Refuse(codeNotRead, ErrNotRead, err, "hardn can't read "+name+": "+hostWords(err), tryAgain)
		}
		changed = string(after) != string(before)
		return after, nil
	})
	return ch, changed, err
}

// ownTitles are the titles of the changes that switch a file's own servers
// off and back on, by the file's base name; any other file is chrony's main
// one.
var ownTitles = map[string][2]string{
	"ubuntu-ntp-pools.sources": {"Switch off Ubuntu's pools", "Switch Ubuntu's pools back on"},
	"":                         {"Switch off chrony's own servers", "Switch chrony's own servers back on"},
}

// chronyOwn are the files whose own servers hardn switches off: chrony's
// main file, and each file a distribution's package installs in the
// directory hardn's own file goes in, where the main file reads it.
func chronyOwn(h tshost.Reading) []string {
	files := []string{h.Main}
	if h.DropIn {
		for _, base := range tshost.DistroSources {
			files = append(files, path.Join(tshost.SourcesDir, base))
		}
	}
	return files
}

// chronyEdits plans edit on each of chronyOwn's files that is there and
// that it changes, titled by title; main says the main file is one of
// them, which chronyd reads at start alone.
func chronyEdits(ctx context.Context, sys host.System, h tshost.Reading, title func(name string) string, edit func(name string, data []byte) ([]byte, error)) (cs []engine.Change, main bool, err error) {
	for _, name := range chronyOwn(h) {
		if _, ok, err := host.ReadFileIfThere(ctx, sys, name); err != nil {
			return nil, false, steps.Refuse(codeNotRead, ErrNotRead, err, "hardn can't read "+name, tryAgain)
		} else if !ok {
			continue
		}
		ch, changed, err := rewrite(ctx, sys, name, title(name), path.Base(name)+": written", edit)
		switch {
		case err != nil:
			return nil, false, err
		case changed:
			cs, main = append(cs, ch), main || name == h.Main
		}
	}
	return cs, main, nil
}

// applied orders cs one after another and makes chronyd read them: by a
// restart where its main file is among them, else by "chronyc reload
// sources", which the last change runs once its file is written, and a
// rollback runs again once the files are back.
func applied(cs []engine.Change, main bool, restart func(after string) engine.Change) []engine.Change {
	if len(cs) == 0 {
		return nil
	}
	for i := 1; i < len(cs); i++ {
		cs[i].After = append(cs[i].After, cs[i-1].ID)
	}
	if main {
		return append(cs, restart(cs[len(cs)-1].ID))
	}
	last := &cs[len(cs)-1]
	write, reload := last.Apply, tshost.ReloadSources
	last.Commands = append(last.Commands, host.FormatArgv(reload))
	last.Risk = engine.RiskServiceRestart
	again := revert.Step{Op: revert.OpRun, Argv: reload, Why: ID}
	last.Rollback.Steps = append(last.Rollback.Steps, again)
	last.Apply = func(ctx context.Context, sys host.System, run engine.Run) (engine.Ran, error) {
		ran, err := write(ctx, sys, run)
		if ran.Rollback != nil { // the write's own undo, the reload after it
			ran.Rollback.Steps = append(ran.Rollback.Steps, again)
		}
		if err != nil {
			return ran, err
		}
		_, err = sys.Run(ctx, reload[0], reload[1:]...)
		return ran, err
	}
	return cs
}

// setChrony is Set servers on chrony (decisions 1 and 6). Where the main
// file reads /etc/chrony/sources.d: hardn's own file there, and every
// server, pool and peer line of the main file and of the distribution's
// files switched off under hardn's mark. Where it reads no such directory:
// the main file edited in place, its own lines switched off and hardn's
// added under a comment naming them.
func setChrony(ctx context.Context, sys host.System, h tshost.Reading, want []string, restart func(after string) engine.Change) ([]engine.Change, error) {
	var cs []engine.Change
	add := want
	title := func(string) string { return "Set chrony's servers" }
	if h.DropIn {
		data, err := tshost.SourcesFile(want)
		if err != nil {
			return nil, badServers(err)
		}
		ch, err := ownFile(ctx, sys, tshost.ChronySources, data)
		if err != nil {
			return nil, err
		}
		if ch.Diff != "" {
			cs = append(cs, ch)
		}
		add = nil
		title = func(name string) string { return ownTitle(name, 0) }
	}
	edits, main, err := chronyEdits(ctx, sys, h, title, func(name string, data []byte) ([]byte, error) {
		if name != h.Main {
			return tshost.ChronyOff(name, data, nil)
		}
		return tshost.ChronyOff(name, data, add)
	})
	if err != nil {
		return nil, err
	}
	return applied(append(cs, edits...), main, restart), nil
}

// ownTitle is ownTitles' title for name: 0 off, 1 back on.
func ownTitle(name string, i int) string {
	if t, ok := ownTitles[path.Base(name)]; ok {
		return t[i]
	}
	return ownTitles[""][i]
}

// planRemove is Remove servers (decision 13): hardn's own setting only.
// Refused on a host hardn sets nothing on (usable), and where nothing in
// the service's files is hardn's. hardn's own file is deleted, its lines
// taken out, and every line it switched off put back as it was; then the
// service reads its settings again. hardn.yml stays as it is.
func planRemove(ctx context.Context, sys host.System, f detect.Facts, r Reading) ([]engine.Change, error) {
	if err := usable(r); err != nil {
		return nil, err
	}
	h := r.Host
	if !h.Mine {
		return nil, steps.Refuse(codeNothing, ErrNothingToRemove, nil, "hardn did not set time servers here", "")
	}
	argv, service, title, err := restart(f, h)
	if err != nil {
		return nil, err
	}
	again := func(after string) engine.Change {
		return steps.RestartService(ID, ChangeRestart, title, service, argv, after)
	}
	switch h.Service {
	case tshost.Chrony:
		cs, err := deleteOwn(ctx, sys, tshost.ChronySources)
		if err != nil {
			return nil, err
		}
		edits, main, err := chronyEdits(ctx, sys, h, func(name string) string { return ownTitle(name, 1) }, tshost.ChronyOn)
		if err != nil {
			return nil, err
		}
		return applied(append(cs, edits...), main, again), nil
	case tshost.Timesyncd:
		cs, err := deleteOwn(ctx, sys, tshost.TimesyncdDropIn)
		if err != nil || len(cs) == 0 {
			return nil, err
		}
		return append(cs, again(ChangeFile)), nil
	}
	ch, _, err := rewrite(ctx, sys, tshost.ConfD, "Put ntpd's servers back", "ntpd's options: written", func(_ string, data []byte) ([]byte, error) {
		return tshost.ClearPeers(data)
	})
	if err != nil {
		return nil, err
	}
	return []engine.Change{ch, again(ch.ID)}, nil
}

// deleteOwn deletes hardn's own file at name; no change where it is not
// there, or is not hardn's.
func deleteOwn(ctx context.Context, sys host.System, name string) ([]engine.Change, error) {
	data, ok, err := host.ReadFileIfThere(ctx, sys, name)
	switch {
	case err != nil:
		return nil, steps.Refuse(codeNotRead, ErrNotRead, err, "hardn can't read "+name, tryAgain)
	case !ok || !strings.HasPrefix(string(data), tshost.Header):
		return nil, nil
	}
	return []engine.Change{steps.DeleteOwnFile(ID, ChangeFile, "Delete hardn's time servers", name, string(data), "time servers: deleted")}, nil
}
