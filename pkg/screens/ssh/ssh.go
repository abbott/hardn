// Package ssh runs hardn's SSH Login screen on a host (refactor plan slice
// 6, unit (c), next/s6-screen): the mocks the maintainer adopted on
// 2026-11-03 (docs/engine/gallery-ssh.md §3 and §5), drawn by pkg/ui/screen
// from what the SSH Login control (pkg/engine/controls/sshchange) reads of
// the host, writing nothing, each of its changes the control's plan through
// the plan/apply engine in pkg/ui's canonical flow: the review, the hostname
// typed, as every SSH change is lockout-risk, the apply, confirmed from a
// new connection or reverted, then the screen drawn again from a fresh
// reading. Every refusal is the control's or the engine's, said before
// anything is asked. Run is the one entry point, which cmd/hardn's main
// menu item 2 is to call (unit (d)); docs/engine/screen-ssh.md has its API
// and what it decided.
//
// Each change runs through the glue every area's screen shares
// (pkg/screens/screenkit). The package reaches the host only through
// host.System, the controls and the engine (depguard rule
// screens-through-host), and renders only through pkg/ui and pkg/output.
package ssh

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	sshctl "github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/screens/sshkeys"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// Options is what the SSH Login screen runs with: what cmd/hardn has when
// its SSH Login menu item is chosen. It has User Management's, and the
// Session Firewall's has.
type Options struct {
	// Prompter asks the questions and writes the screen through the one line
	// reader, rendering with its Ctx, whose Env is the terminal's.
	Prompter nav.Prompter
	// System is the host: the screen reads it, and the engine changes it.
	System host.System
	// Facts are detect's, from which the control takes the init system,
	// sshd's mode and Proxmox VE; the engine establishes its own when it
	// plans.
	Facts detect.Facts
	// Session is the session hardn runs in, as the caller reads it
	// (revert.ReadSession), for what sshchange.Read reads of it: its line in
	// sshd's log, or the console. The engine a change plans with holds its
	// own, from which the control reads the same, which should be this one.
	Session revert.Session
	// Getenv looks up hardn's environment (os.Getenv in hardn), from which
	// the control takes the session user (SUDO_USER, DOAS_USER). Required.
	Getenv func(string) string
	// Config is hardn.yml as loaded: its sshPort the port form's default,
	// where sshd does not listen on it already (decision 9 (a)), and its
	// sshConfigFile the old file Move old settings moves; no other SSH
	// setting is read. Nil has none. No change saves it.
	Config *config.Config
	// NewEngine returns the engine a change plans and applies with, built as
	// the caller builds it (engine.New over System with its options: its
	// backups, its session, hardn's path for the revert timer), progress as
	// its Options.Progress: one per item chosen, whose plan before asking
	// says a refusal first, one per port the port field checks, and one per
	// change, made when the change is planned. Drawing makes none. Required.
	NewEngine func(progress func(engine.Event)) *engine.Engine
	// ApplyContext, when set, returns the context a change's apply runs
	// under, made from the screen's, and its stop, called once the apply has
	// returned; one per apply (screenkit.Planning.ApplyContext). Nil applies
	// under the screen's context.
	ApplyContext func(ctx context.Context) (context.Context, context.CancelFunc)
	// DryRun stops each change at its plan: nothing applied.
	DryRun bool
	// LogPath is the log a failure's report names; "" names none.
	LogPath string
	// Now is the clock output.AfterFlow counts from; nil is time.Now.
	Now func() time.Time
	// Ran, when set, is given what each change did once its flow has ended,
	// for a change that was planned: for the audit log.
	Ran func(output.Run)
}

// Run shows the SSH Login screen and runs the items the user chooses, until
// they go back with 0 (nil) or quit with q (nav.ErrQuit); the end of the
// input and Ctrl-C abort (prompt.ErrAborted), from the screen, each question
// and the form. Each round reads the host with the control
// (sshchange.Read) and the SSH Login, SSH Auth and SSH Port rows (their
// checks), writing nothing, under plan only too, and draws the screen from
// what it read.
//
// Item 1 is Disallow root login, Allow root key-only where root login is
// disallowed, or on Proxmox VE Root key-only; item 2 Key-only login, or
// Password login on where passwords don't log in; item 3 Change SSH port;
// item 4, with hardn's old files, Move old settings; item 5, where sshd lacks
// the fix, Upgrade openssh; item 6 SSH keys, the read-only SSH Keys screen
// (pkg/screens/sshkeys), which plans nothing. Each change is planned by the
// engine first, from a fresh reading, and its refusal (the control's, or
// the engine's, such as a change waiting for hardn confirm) said before
// anything is asked; Change SSH port plans the port sshd has for it, before
// its field, from a reading of its own. Then the change: the port's form,
// refused at its field outside 1 to 65535, as the engine refuses the port's
// plan, or on a port sshd has (an empty plan); the review; the plan, planned again from
// a fresh reading; the hostname typed, as every SSH change is lockout-risk;
// the apply. 0 changes nothing, and a dry run stops at the plan. A live
// change whose hostname can't be read says so before asking. Run returns an
// error wrapping fs.ErrInvalid, writing nothing, without a Prompter's In and
// Out, a System, Getenv or NewEngine.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil || o.Getenv == nil || o.NewEngine == nil {
		return fmt.Errorf("ssh screen: %w: it needs a Prompter with In and Out, a System, Getenv and NewEngine", fs.ErrInvalid)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	cfg := o.Config
	if cfg == nil {
		cfg = &config.Config{} // no change saves it: screenkit plans each with a copy
	}
	r := &runner{o: o, cfg: cfg, s: screenkit.Session{Name: "ssh screen", Prompter: o.Prompter, System: o.System, Config: cfg,
		NewEngine: o.NewEngine, ApplyContext: o.ApplyContext, DryRun: o.DryRun, LogPath: o.LogPath, Now: o.Now, Ran: o.Ran}}
	return o.Prompter.Run(func() nav.Screen { return r.draw(ctx) })
}

// runner is one run of the SSH Login screen: its options, hardn.yml as the
// engine plans with it, and the session its changes run with.
type runner struct {
	o   Options
	cfg *config.Config
	s   screenkit.Session
}

// reading is one reading of the host: the control's, why it was cut short
// (err: the context's), the three rows, and the SSH keys item's description
// (keys; "" in a reading that did not read them).
type reading struct {
	sshchange.Reading
	err     error
	status  component.StatusList
	checked checked
	keys    string
}

// read reads the host as the control reads it (sshchange.Read, writing
// nothing, running only reads), the three rows from their checks, and every
// account's keys (sshkeys.Read) in the files the sshd -T just read names, for
// the SSH keys item: Can't read where sshd's settings are not read.
func (r *runner) read(ctx context.Context) reading {
	rd, err := sshchange.Read(ctx, r.o.System, r.o.Facts, r.o.Config, r.o.Session, r.o.Getenv)
	h := reading{Reading: rd, err: err}
	h.status, h.checked = statusRows(ctx, r.o.System, r.o.Facts)
	h.keys = sshkeys.View(sshkeys.Read(ctx, r.o.System, func(context.Context) (sshctl.KeySettings, error) {
		if h.notRead() {
			return sshctl.KeySettings{}, errors.Join(h.EffErr, h.err)
		}
		return sshctl.KeySettingsOf(h.Effective)
	})).Entry()
	return h
}

// keysScreen is item 6, SSH keys: the SSH Keys screen (pkg/screens/sshkeys),
// which reads sshd's settings and the keys again on each draw and changes
// nothing, under plan only too. It opens and closes on a cleared screen, a
// sub-menu (nav.Prompter.Open).
func (r *runner) keysScreen(ctx context.Context) error {
	return r.o.Prompter.Open(func() error {
		return sshkeys.Run(ctx, sshkeys.Options{Prompter: r.o.Prompter, System: r.o.System,
			Settings: func(ctx context.Context) (sshctl.KeySettings, error) {
				return sshctl.ReadKeySettings(ctx, r.o.System, r.o.Facts)
			}})
	})
}

// notRead reports whether sshd's settings are not read: sshd -T failed, or
// the reading was cut short.
func (h reading) notRead() bool { return h.EffErr != nil || h.err != nil }
