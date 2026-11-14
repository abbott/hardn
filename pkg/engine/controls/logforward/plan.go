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
	return steps.Refuse(codeNotRead, ErrNotRead, r.Host.Err, "hardn can't read "+what+": "+hostWords(r.Host.Err), tryAgain)
}

// restartArgv is the command that restarts the logger, by the init system
// that booted the host (steps.ServiceArgv); a refusal where that is not the
// one the logger's service belongs to.
func restartArgv(f detect.Facts, logger logfwd.Logger) ([]string, error) {
	if logger == logfwd.Busybox {
		if argv, ok := steps.ServiceArgv(f, "", logfwd.BusyboxService, "restart"); ok {
			return argv, nil
		}
		return nil, steps.Refuse(codeNoInit, ErrNoInit, nil, "OpenRC did not boot this host, so hardn can't restart syslogd", "set it by hand")
	}
	if argv, ok := steps.ServiceArgv(f, rsyslogService, "", "restart"); ok {
		return argv, nil
	}
	return nil, steps.Refuse(codeNoInit, ErrNoInit, nil, "systemd did not boot this host, so hardn can't restart rsyslog", "set it by hand")
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
		return nil, steps.Refuse(codeBadServer, ErrBadServer, r.WantErr, "hardn.yml's logForwarding is not valid: "+logview.Safe(why), tryAgain)
	case r.Want == nil:
		return nil, steps.Refuse(codeNoServer, ErrNoServer, nil, "hardn.yml names no log server", "add logForwarding's server, then try again")
	}
	if err := unread(r); err != nil {
		return nil, err
	}
	want := *r.Want
	if r.Now != nil && r.Now.Server == want {
		return nil, nil // as asked, whoever set it
	}
	if o := r.Host.Foreign(); o != nil {
		return nil, steps.Refuse(codeOutside, ErrOutside, nil, logview.Safe(o.File)+" already forwards logs, and hardn leaves it as it is", "remove that setting, then try again")
	}
	restart, err := restartArgv(f, r.Host.Logger)
	if err != nil {
		return nil, err
	}
	if r.Host.Logger == logfwd.Busybox {
		if want.Proto != logfwd.UDP {
			return nil, steps.Refuse(codeUDPOnly, ErrUDPOnly, nil, "busybox syslogd forwards over UDP only", "set protocol: udp in hardn.yml, then try again")
		}
		ch, err := optionsChange(ctx, sys, "Set syslogd's server", func(data []byte) ([]byte, error) { return logfwd.SetOpts(data, want) })
		if err != nil {
			return nil, err
		}
		return []engine.Change{ch, restartChange("Restart syslogd", logfwd.BusyboxService, restart, ChangeOptions), testChange()}, nil
	}
	if want.Proto == logfwd.TLS {
		if fi, err := sys.Stat(ctx, want.CAFile); err != nil || !fi.Mode().IsRegular() {
			return nil, steps.Refuse(codeNoCAFile, ErrNoCAFile, err, "hardn.yml's caFile, "+want.CAFile+", is not a file on this host", "put the CA's certificate there, then try again")
		}
	}
	before, err := dropInBefore(ctx, sys)
	if err != nil {
		return nil, err
	}
	var cs []engine.Change
	pkgs := []steps.Package{{ID: ChangeInstall, Name: rsyslogService, After: "installed", Why: ID, Resources: []string{ID}, Proxmox: true}}
	if r.Host.Logger != logfwd.Journald {
		pkgs = nil
		if res, err := sys.Run(ctx, "rsyslogd", "-N1"); err != nil {
			return nil, steps.Refuse(codeNotRead, ErrNotRead, err, "rsyslogd can't read its settings: "+checkWords(res, err), tryAgain)
		}
	}
	if want.Proto == logfwd.TLS {
		pkgs = append(pkgs, steps.Package{ID: ChangeInstallTLS, Name: logfwd.GnuTLSPackage, After: "installed", Why: ID, Resources: []string{ID}, Proxmox: true})
	}
	for _, p := range pkgs {
		pc, err := steps.PlanPackage(ctx, sys, f, p)
		if err != nil {
			return nil, err
		}
		for i := range pc {
			pc[i].After = steps.IDs(cs)
		}
		cs = append(cs, pc...)
	}
	data, err := logfwd.RsyslogFile(want)
	if err != nil {
		return nil, steps.Refuse(codeBadServer, ErrBadServer, err, "hardn.yml's logForwarding is not valid", tryAgain)
	}
	cs = append(cs, fileChange(before, data, steps.IDs(cs)))
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
		return nil, steps.Refuse(codeNotRead, ErrNotRead, err, "hardn can't read "+logfwd.DropIn, tryAgain)
	case !strings.HasPrefix(string(data), logfwd.Header):
		return nil, steps.Refuse(codeOutside, ErrOutside, nil, logfwd.DropIn+" is not hardn's file, and hardn leaves it as it is", "move that file away, then try again")
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
		return nil, steps.Refuse(codeNothing, ErrNothingToStop, nil, "hardn did not set log forwarding here", "")
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
		return nil, steps.Refuse(codeNotRead, ErrNotRead, err, "hardn can't read "+logfwd.DropIn, tryAgain)
	}
	del := steps.DeleteOwnFile(ID, ChangeFile, "Delete hardn's forwarding file", logfwd.DropIn, string(data), "forwarding file: deleted")
	return []engine.Change{del, restartChange("Restart rsyslog", rsyslogService, restart, ChangeFile)}, nil
}

// fileChange writes hardn's file, then has rsyslogd check its settings
// (rsyslogd -N1): a refusal there is the change failed, before the restart,
// and the engine puts the file back as it was (steps.WriteOwnFile).
func fileChange(before *string, data []byte, after []string) engine.Change {
	return steps.WriteOwnFile(steps.OwnFile{Control: ID, ID: ChangeFile, Title: "Write hardn's forwarding file", Path: logfwd.DropIn,
		Before: before, Data: data, After: after,
		Check: &steps.FileCheck{Argv: []string{"rsyslogd", "-N1"}, Detail: "rsyslogd -N1: accepted", Refused: func(res host.Result, err error) error {
			return steps.Refuse(codeCheck, ErrCheckFailed, err, "rsyslogd refuses its settings with hardn's file: "+checkWords(res, err), "")
		}}})
}

// optionsChange rewrites /etc/conf.d/syslog with edit (logfwd.SetOpts or
// ClearOpts), keeping its mode and owner (steps.RewriteFile). edit's
// refusals are said at plan time; the apply edits the file as it is then,
// and refuses one that is no longer as planned.
func optionsChange(ctx context.Context, sys host.System, title string, edit func([]byte) ([]byte, error)) (engine.Change, error) {
	return steps.RewriteFile(ctx, sys, steps.Rewrite{Control: ID, ID: ChangeOptions, Title: title, Path: logfwd.ConfD, Detail: "syslogd's options: written",
		Unread: func(err error) error {
			return steps.Refuse(codeNotRead, ErrNotRead, err, "hardn can't read "+logfwd.ConfD, tryAgain)
		},
		Changed: steps.Refuse(codeChanged, ErrChangedSince, nil, logfwd.ConfD+" changed since the plan", "try again"),
	}, func(before []byte) ([]byte, error) {
		after, err := edit(before)
		switch {
		case errors.Is(err, logfwd.ErrNotOwn):
			return nil, steps.Refuse(codeNothing, ErrNothingToStop, err, "syslogd's options no longer hold what hardn added", "edit "+logfwd.ConfD+" by hand")
		case errors.Is(err, logfwd.ErrForeign):
			return nil, steps.Refuse(codeOutside, ErrOutside, err, logfwd.ConfD+" already forwards logs, and hardn leaves it as it is", "remove that setting, then try again")
		case err != nil:
			return nil, steps.Refuse(codeNotRead, ErrNotRead, err, "hardn can't read syslogd's options: "+hostWords(err), tryAgain)
		}
		return after, nil
	})
}

// restartChange restarts the logger, which reads its settings at start
// alone (rsyslogd's HUP only closes its files; syslogd has no reload), and
// restarts it again on a rollback (steps.RestartService).
func restartChange(title, service string, argv []string, after string) engine.Change {
	return steps.RestartService(ID, ChangeRestart, title, service, argv, after)
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
		if l := strings.TrimSpace(steps.FirstLine(string(out))); l != "" {
			return logview.Safe(l)
		}
	}
	return logview.Safe(steps.FirstLine(err.Error()))
}

// hostWords is a reading's error in one safe line, without logfwd's own
// prefix and its sentinel's words (steps.HostWords): it names files of the
// host.
func hostWords(err error) string { return steps.HostWords(err, "logfwd: ", logfwd.ErrMalformed) }
