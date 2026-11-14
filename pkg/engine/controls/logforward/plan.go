package logforward

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/logfwd"
	"github.com/abbott/hardn/pkg/host/revert"
)

// Errors of the refusals, testable with errors.Is, each the Err of a
// *steps.Refusal in the decided words (gallery-logforward.md §5), wrapping
// the cause where there is one.
var (
	// ErrNoServer: hardn.yml names no log server.
	ErrNoServer = errors.New("hardn.yml names no log server")
	// ErrBadServer: hardn.yml's logForwarding is not valid.
	ErrBadServer = errors.New("hardn.yml's logForwarding is not valid")
	// ErrNotRead: the logger, or its settings, are not read.
	ErrNotRead = errors.New("the logger's settings are not read, so no change is planned")
	// ErrOutside: the host forwards by a setting that is not hardn's.
	ErrOutside = errors.New("log forwarding is set outside hardn")
	// ErrUDPOnly: busybox syslogd forwards over UDP only.
	ErrUDPOnly = errors.New("busybox syslogd forwards over UDP only")
	// ErrNoCAFile: hardn.yml's caFile is not a file on the host.
	ErrNoCAFile = errors.New("the CA file is not a file on the host")
	// ErrNoInit: the logger's service is not one hardn restarts here.
	ErrNoInit = errors.New("the logger's service is not one hardn restarts here")
	// ErrCheckFailed: rsyslogd refuses its settings with hardn's file.
	ErrCheckFailed = errors.New("rsyslogd refuses its settings with hardn's file")
	// ErrNothingToStop: hardn did not set log forwarding.
	ErrNothingToStop = errors.New("hardn did not set log forwarding")
	// ErrChangedSince: the logger's settings changed since the plan.
	ErrChangedSince = errors.New("the logger's settings changed since the plan")
)

// The refusals' codes, the registry's for the errors they wrap (pkg/output).
const (
	codeNoServer   = "LOGFWD_NO_SERVER"
	codeBadServer  = "LOGFWD_BAD_SERVER"
	codeNotRead    = "LOGFWD_NOT_READ"
	codeOutside    = "LOGFWD_SET_OUTSIDE"
	codeUDPOnly    = "LOGFWD_UDP_ONLY"
	codeNoCAFile   = "LOGFWD_NO_CA_FILE"
	codeNoInit     = "LOGFWD_NO_INIT"
	codeCheck      = "LOGFWD_CHECK_FAILED"
	codeNothing    = "LOGFWD_NOTHING_TO_STOP"
	codeChanged    = "LOGFWD_CHANGED_SINCE"
	tryAgain       = "fix that, then try again"
	whyTestLine    = "log forwarding set"
	rsyslogService = "rsyslog"
)

// refuse is a refusal: its code, its sentinel wrapping cause where there is
// one, and the Why and Fix screenkit.RefusalWords joins.
func refuse(code string, sentinel, cause error, why, fix string) error {
	err := sentinel
	if cause != nil {
		err = fmt.Errorf("%w: %w", sentinel, cause)
	}
	return &steps.Refusal{Code: code, Why: why, Fix: fix, Err: err}
}

// The changes' ids, under ID.
const (
	ChangeInstall    = ID + "/install"
	ChangeInstallTLS = ID + "/install-tls"
	ChangeFile       = ID + "/file"
	ChangeOptions    = ID + "/options"
	ChangeRestart    = ID + "/restart"
	ChangeTest       = ID + "/test"
)

// The review's notices (gallery-logforward.md §5): what a change does
// beyond its files.
const (
	NoticeAll      = "Every message the host logs goes to the server, as it is logged."
	NoticeJournald = "rsyslog is installed to forward the journal. It also keeps its own log files under /var/log."
	NoticeUDP      = "busybox syslogd forwards over UDP alone: not encrypted, and a lost message is not noticed."
	NoticeStop     = "Logs stay on the host alone. rsyslog stays installed."
	NoticeStopApk  = "Logs stay on the host alone."
)

// Notice is the review's notice for action a on the host as r reads it.
func (r Reading) Notice(a Action) string {
	switch {
	case a == ActionStop && r.Host.Logger == logfwd.Busybox:
		return NoticeStopApk
	case a == ActionStop:
		return NoticeStop
	case r.Host.Logger == logfwd.Busybox:
		return NoticeUDP
	case r.Host.Logger == logfwd.Journald:
		return NoticeJournald
	}
	return NoticeAll
}

// Plan reads the host and plans what Action asks: nothing at ActionNone
// (engine.ErrNotMigrated); Set (planSet) or Stop (planStop), each refusing
// with a *steps.Refusal, in the decided words, before anything is written.
// A host already forwarding to hardn.yml's server plans nothing.
func (c Forward) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	cfg := in.Config
	if cfg == nil {
		cfg = c.Config
	}
	switch c.Action {
	case ActionNone:
		return nil, engine.ErrNotMigrated
	case ActionSet:
		return planSet(ctx, sys, in.Facts, Read(ctx, sys, in.Facts, cfg))
	case ActionStop:
		return planStop(ctx, sys, in.Facts, Read(ctx, sys, in.Facts, cfg))
	}
	return nil, fmt.Errorf("logforward: action %d: %w", c.Action, fs.ErrInvalid)
}

// unread refuses a host whose logger or settings are not read.
func unread(r Reading) error {
	if r.Host.Err == nil {
		return nil
	}
	what := "the logger's settings"
	switch r.Host.Logger {
	case logfwd.Rsyslog:
		what = "rsyslog's settings"
	case logfwd.Busybox:
		what = "syslogd's options"
	}
	return refuse(codeNotRead, ErrNotRead, r.Host.Err, "hardn can't read "+what+": "+hostWords(r.Host.Err), tryAgain)
}

// restartArgv is the command that restarts the logger, by the init system
// that booted the host; a refusal where that is not the one the logger's
// service belongs to.
func restartArgv(f detect.Facts, logger logfwd.Logger) ([]string, error) {
	switch {
	case logger == logfwd.Busybox && f.Init.Kind == detect.OpenRC && f.Init.Running == detect.Yes:
		return []string{"rc-service", logfwd.BusyboxService, "restart"}, nil
	case logger != logfwd.Busybox && f.Init.Kind == detect.Systemd && f.Init.Running == detect.Yes:
		return []string{"systemctl", "restart", rsyslogService}, nil
	case logger == logfwd.Busybox:
		return nil, refuse(codeNoInit, ErrNoInit, nil, "OpenRC did not boot this host, so hardn can't restart syslogd", "set it by hand")
	}
	return nil, refuse(codeNoInit, ErrNoInit, nil, "systemd did not boot this host, so hardn can't restart rsyslog", "set it by hand")
}

// planSet is Set server (decisions 3 to 7, 11 and 12). Refused: no server
// in hardn.yml, or one that is not valid; settings not read; forwarding
// set outside hardn, which hardn neither edits nor adds to, or a file at
// hardn's path that hardn did not write; on Alpine,
// TCP or TLS, which busybox syslogd has not; for TLS, a CA file that is
// not there. Then, on Alpine, syslogd's options and its restart; elsewhere
// rsyslog installed where the host has journald alone, rsyslog-gnutls for
// TLS, hardn's file checked with rsyslogd -N1, and the restart. Last, one
// line through the logger for the server to show.
func planSet(ctx context.Context, sys host.System, f detect.Facts, r Reading) ([]engine.Change, error) {
	switch {
	case r.WantErr != nil:
		why := strings.TrimPrefix(r.WantErr.Error(), config.ErrLogForwarding.Error()+": ")
		return nil, refuse(codeBadServer, ErrBadServer, r.WantErr, "hardn.yml's logForwarding is not valid: "+logview.Safe(why), tryAgain)
	case r.Want == nil:
		return nil, refuse(codeNoServer, ErrNoServer, nil, "hardn.yml names no log server", "add logForwarding's server, then try again")
	}
	if err := unread(r); err != nil {
		return nil, err
	}
	want := *r.Want
	if r.Now != nil && r.Now.Server == want {
		return nil, nil // as asked, whoever set it
	}
	if o := r.Host.Foreign(); o != nil {
		return nil, refuse(codeOutside, ErrOutside, nil, logview.Safe(o.File)+" already forwards logs, and hardn leaves it as it is", "remove that setting, then try again")
	}
	restart, err := restartArgv(f, r.Host.Logger)
	if err != nil {
		return nil, err
	}
	if r.Host.Logger == logfwd.Busybox {
		if want.Proto != logfwd.UDP {
			return nil, refuse(codeUDPOnly, ErrUDPOnly, nil, "busybox syslogd forwards over UDP only", "set protocol: udp in hardn.yml, then try again")
		}
		ch, err := optionsChange(ctx, sys, "Set syslogd's server", func(data []byte) ([]byte, error) { return logfwd.SetOpts(data, want) })
		if err != nil {
			return nil, err
		}
		return []engine.Change{ch, restartChange("Restart syslogd", logfwd.BusyboxService, restart, ChangeOptions), testChange()}, nil
	}
	if want.Proto == logfwd.TLS {
		if fi, err := sys.Stat(ctx, want.CAFile); err != nil || !fi.Mode().IsRegular() {
			return nil, refuse(codeNoCAFile, ErrNoCAFile, err, "hardn.yml's caFile, "+want.CAFile+", is not a file on this host", "put the CA's certificate there, then try again")
		}
	}
	before, err := dropInBefore(ctx, sys)
	if err != nil {
		return nil, err
	}
	var cs []engine.Change
	pkgs := []steps.Package{{ID: ChangeInstall, Name: rsyslogService, After: "installed", Why: ID, Resources: []string{ID}}}
	if r.Host.Logger != logfwd.Journald {
		pkgs = nil
		if res, err := sys.Run(ctx, "rsyslogd", "-N1"); err != nil {
			return nil, refuse(codeNotRead, ErrNotRead, err, "rsyslogd can't read its settings: "+checkWords(res, err), tryAgain)
		}
	}
	if want.Proto == logfwd.TLS {
		pkgs = append(pkgs, steps.Package{ID: ChangeInstallTLS, Name: logfwd.GnuTLSPackage, After: "installed", Why: ID, Resources: []string{ID}})
	}
	for _, p := range pkgs {
		pc, err := steps.PlanPackage(ctx, sys, f, p)
		if err != nil {
			return nil, err
		}
		for i := range pc {
			pc[i].After = ids(cs)
		}
		cs = append(cs, pc...)
	}
	data, err := logfwd.RsyslogFile(want)
	if err != nil {
		return nil, refuse(codeBadServer, ErrBadServer, err, "hardn.yml's logForwarding is not valid", tryAgain)
	}
	cs = append(cs, fileChange(before, data, ids(cs)))
	return append(cs, restartChange("Restart rsyslog", rsyslogService, restart, ChangeFile), testChange()), nil
}

// dropInBefore is hardn's file as it is now, for the review's diff; nil
// where there is none. A file at hardn's path that does not open with
// hardn's first line is not hardn's, whatever it holds, and is a refusal:
// hardn does not write over it.
func dropInBefore(ctx context.Context, sys host.System) (*string, error) {
	data, err := sys.ReadFile(ctx, logfwd.DropIn)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, refuse(codeNotRead, ErrNotRead, err, "hardn can't read "+logfwd.DropIn, tryAgain)
	case !strings.HasPrefix(string(data), logfwd.Header):
		return nil, refuse(codeOutside, ErrOutside, nil, logfwd.DropIn+" is not hardn's file, and hardn leaves it as it is", "move that file away, then try again")
	}
	text := string(data)
	return &text, nil
}

// planStop is Stop forwarding (decision 11): hardn's own setting only.
// Refused where the settings are not read, or nothing forwarding is
// hardn's. On Alpine hardn's words come out of syslogd's options; elsewhere
// hardn's file is deleted; then the logger restarts. rsyslog stays
// installed, and hardn.yml stays as it is.
func planStop(ctx context.Context, sys host.System, f detect.Facts, r Reading) ([]engine.Change, error) {
	if err := unread(r); err != nil {
		return nil, err
	}
	if r.Host.Own() == nil {
		return nil, refuse(codeNothing, ErrNothingToStop, nil, "hardn did not set log forwarding here", "")
	}
	restart, err := restartArgv(f, r.Host.Logger)
	if err != nil {
		return nil, err
	}
	if r.Host.Logger == logfwd.Busybox {
		ch, err := optionsChange(ctx, sys, "Remove syslogd's server", logfwd.ClearOpts)
		if err != nil {
			return nil, err
		}
		return []engine.Change{ch, restartChange("Restart syslogd", logfwd.BusyboxService, restart, ChangeOptions)}, nil
	}
	data, err := sys.ReadFile(ctx, logfwd.DropIn)
	if err != nil {
		return nil, refuse(codeNotRead, ErrNotRead, err, "hardn can't read "+logfwd.DropIn, tryAgain)
	}
	text := string(data)
	del := engine.Change{ID: ChangeFile, Summary: "Delete hardn's forwarding file " + logfwd.DropIn, Title: "Delete hardn's forwarding file", Target: logfwd.DropIn,
		Diff: engine.FileDiff(logfwd.DropIn, &text, nil), Stage: engine.StageFiles, Resources: []string{ID}, Backups: []string{logfwd.DropIn},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			return engine.Ran{Detail: "forwarding file: deleted"}, sys.Remove(ctx, logfwd.DropIn)
		}}
	return []engine.Change{del, restartChange("Restart rsyslog", rsyslogService, restart, ChangeFile)}, nil
}

// fileChange writes hardn's file, then has rsyslogd check its settings
// (rsyslogd -N1): a refusal there is the change failed, before the restart,
// and the engine puts the file back as it was.
func fileChange(before *string, data []byte, after []string) engine.Change {
	text := string(data)
	return engine.Change{ID: ChangeFile, Summary: "Write hardn's forwarding file " + logfwd.DropIn, Title: "Write hardn's forwarding file", Target: logfwd.DropIn,
		Diff: engine.FileDiff(logfwd.DropIn, before, &text), Commands: []string{"rsyslogd -N1"},
		Stage: engine.StageFiles, Resources: []string{ID}, Backups: []string{logfwd.DropIn}, After: after,
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			if err := sys.WriteFileAtomic(ctx, logfwd.DropIn, data, 0o644, 0, 0); err != nil {
				return engine.Ran{}, err
			}
			if res, err := sys.Run(ctx, "rsyslogd", "-N1"); err != nil {
				return engine.Ran{}, refuse(codeCheck, ErrCheckFailed, err, "rsyslogd refuses its settings with hardn's file: "+checkWords(res, err), "")
			}
			return engine.Ran{Detail: "rsyslogd -N1: accepted"}, nil
		}}
}

// optionsChange rewrites /etc/conf.d/syslog with edit (logfwd.SetOpts or
// ClearOpts), keeping its mode and owner. edit's refusals are said at plan
// time; the apply edits the file as it is then, and refuses one that is no
// longer as planned.
func optionsChange(ctx context.Context, sys host.System, title string, edit func([]byte) ([]byte, error)) (engine.Change, error) {
	before, err := sys.ReadFile(ctx, logfwd.ConfD)
	if err != nil {
		return engine.Change{}, refuse(codeNotRead, ErrNotRead, err, "hardn can't read "+logfwd.ConfD, tryAgain)
	}
	after, err := edit(before)
	switch {
	case errors.Is(err, logfwd.ErrNotOwn):
		return engine.Change{}, refuse(codeNothing, ErrNothingToStop, err, "syslogd's options no longer hold what hardn added", "edit "+logfwd.ConfD+" by hand")
	case errors.Is(err, logfwd.ErrForeign):
		return engine.Change{}, refuse(codeOutside, ErrOutside, err, logfwd.ConfD+" already forwards logs, and hardn leaves it as it is", "remove that setting, then try again")
	case err != nil:
		return engine.Change{}, refuse(codeNotRead, ErrNotRead, err, "hardn can't read syslogd's options: "+hostWords(err), tryAgain)
	}
	a, b := string(before), string(after)
	return engine.Change{ID: ChangeOptions, Summary: title + " in " + logfwd.ConfD, Title: title, Target: logfwd.ConfD,
		Diff: engine.FileDiff(logfwd.ConfD, &a, &b), Stage: engine.StageFiles, Resources: []string{ID}, Backups: []string{logfwd.ConfD},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			now, err := sys.ReadFile(ctx, logfwd.ConfD)
			switch {
			case err != nil:
				return engine.Ran{}, err
			case string(now) != a:
				return engine.Ran{}, refuse(codeChanged, ErrChangedSince, nil, logfwd.ConfD+" changed since the plan", "try again")
			}
			return engine.Ran{Detail: "syslogd's options: written"}, host.WriteFileAtomicKeep(ctx, sys, logfwd.ConfD, after, 0o644, 0, 0)
		}}, nil
}

// restartChange restarts the logger, which reads its settings at start
// alone (rsyslogd's HUP only closes its files; syslogd has no reload). A
// rollback restarts it again, once the files are back; not where the
// command was not found, as nothing ran.
func restartChange(title, service string, argv []string, after string) engine.Change {
	undo := engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: argv, Why: ID}}}
	return engine.Change{ID: ChangeRestart, Summary: title, Title: title, Target: service, Commands: []string{host.FormatArgv(argv)},
		Risk: engine.RiskServiceRestart, Stage: engine.StageReload, Resources: []string{ID}, After: []string{after}, Rollback: undo,
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			_, err := sys.Run(ctx, argv[0], argv[1:]...)
			if errors.Is(err, host.ErrNotFound) {
				return engine.Ran{Rollback: &engine.Undo{}}, err
			}
			return engine.Ran{Detail: service + ": restarted"}, err
		}}
}

// TestArgv is the test line (decision 12): one message through the logger,
// for the administrator to find on the server. The host cannot know what
// the server received.
var TestArgv = []string{"logger", "-t", "hardn", whyTestLine}

// testChange sends the test line, last.
func testChange() engine.Change {
	return engine.Change{ID: ChangeTest, Summary: "Send a test line", Title: "Send a test line", Commands: []string{host.FormatArgv(TestArgv)},
		Stage: engine.StageVerify, Resources: []string{ID}, After: []string{ChangeRestart},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			_, err := sys.Run(ctx, TestArgv[0], TestArgv[1:]...)
			return engine.Ran{Detail: "test line: sent"}, err
		}}
}

// checkWords is what rsyslogd -N1 said, in one safe line: its first line of
// output, else the error.
func checkWords(res host.Result, err error) string {
	for _, out := range [][]byte{res.Stderr, res.Combined} {
		if l := strings.TrimSpace(firstLine(string(out))); l != "" {
			return logview.Safe(l)
		}
	}
	return logview.Safe(firstLine(err.Error()))
}

// hostWords is a reading's error in one safe line, without the package's
// own prefix and its sentinel's words: it names files of the host.
func hostWords(err error) string {
	words := strings.TrimPrefix(firstLine(err.Error()), "logfwd: ")
	return logview.Safe(strings.TrimSuffix(words, ": "+logfwd.ErrMalformed.Error()))
}

// ids are the ids of cs.
func ids(cs []engine.Change) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

// firstLine is s to its first line break.
func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}
