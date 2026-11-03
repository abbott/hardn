// Package users runs hardn's User Management screen on a host (refactor
// plan slice 5, unit (c), next/s5-screen): the mocks the maintainer adopted
// on 2026-11-03 (docs/engine/gallery-users.md §3 and §5), drawn by
// pkg/ui/screen from what the User control (pkg/engine/controls/users)
// reads of the host, writing nothing, each of its changes the control's plan
// through the plan/apply engine in pkg/ui's canonical flow: the review, y/N,
// or the hostname typed for a lockout-risk plan, the apply, then the screen
// drawn again from a fresh reading. Every refusal is the control's, said
// before anything is asked. Run is the one entry point, which cmd/hardn's
// main menu item 1 is to call (unit (d)); docs/engine/screen-users.md has
// its API and what it decided.
//
// Each change runs through the glue every area's screen shares
// (pkg/screens/screenkit). The package reaches the host only through
// host.System, the control and the engine (depguard rule screens-through-host),
// and renders only through pkg/ui and pkg/output.
package users

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	usersctl "github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// Options is what the User Management screen runs with: what cmd/hardn has
// when its User Management menu item is chosen. It has Firewall's, with the
// environment the control reads the session user from in place of a
// Session, as the screen plans nothing itself.
type Options struct {
	// Prompter asks the questions and writes the screen through the one line
	// reader, rendering with its Ctx, whose Env is the terminal's.
	Prompter nav.Prompter
	// System is the host: the screen reads it, and the engine changes it.
	System host.System
	// Facts are detect's, from which the control takes the tool and the
	// platform; the engine establishes its own when it plans.
	Facts detect.Facts
	// Getenv looks up hardn's environment (os.Getenv in hardn), from which
	// users.Read takes the session user (SUDO_USER, DOAS_USER), and each
	// change's plan too. Required.
	Getenv func(string) string
	// Config is hardn.yml as loaded, whose username, sudoNoPassword and
	// sshKeys are Create's defaults alone, never the user the rows are about
	// (decision 7). Nil has none. No change saves it.
	Config *config.Config
	// NewEngine returns the engine a change plans and applies with, built as
	// the caller builds it (engine.New over System with its options: its
	// backups, its session, hardn's path for the revert timer), progress as
	// its Options.Progress; one per change, made when the change is planned.
	// Drawing makes none. Required.
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

// Run shows the User Management screen and runs the items the user chooses,
// until they go back with 0 (nil) or quit with q (nav.ErrQuit); the end of
// the input and Ctrl-C abort (prompt.ErrAborted), from the screen, each
// sub-screen, each question and the form. Each round reads the host with the
// control (users.Read), the User row its Check, writing nothing, under plan
// only too, and draws the screen from what it read.
//
// Item 1, with a user besides root, chooses one: Grant or Remove admin
// rights, with them Ask or Don't ask password, and SSH Keys: Add, and with
// any Remove. Item 2 is Create's form. Each refusal is the control's, said
// before asking (Reading.Refusal, ToolRefusal, KeysRefusal, CheckName,
// CheckNew, ParseKey), the form's at its field. Each change is the control's
// plan from a fresh reading, reviewed, confirmed y/N, or by the hostname
// typed for a lockout-risk plan, and applied; n and 0 change nothing, and a
// dry run stops at the plan. A lockout-risk plan whose hostname can't be
// read says so before asking. Run returns an error wrapping fs.ErrInvalid,
// writing nothing, without a Prompter's In and Out, a System, Getenv or
// NewEngine.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil || o.Getenv == nil || o.NewEngine == nil {
		return fmt.Errorf("users screen: %w: it needs a Prompter with In and Out, a System, Getenv and NewEngine", fs.ErrInvalid)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	cfg := o.Config
	if cfg == nil {
		cfg = &config.Config{} // no change saves it: screenkit plans each with a copy
	}
	r := &runner{o: o, s: screenkit.Session{Name: "users screen", Prompter: o.Prompter, System: o.System, Config: cfg,
		NewEngine: o.NewEngine, ApplyContext: o.ApplyContext, DryRun: o.DryRun, LogPath: o.LogPath, Now: o.Now, Ran: o.Ran}}
	return o.Prompter.Run(func() nav.Screen { return r.draw(ctx) })
}

// runner is one run of the User Management screen: its options, and the
// session its changes run with.
type runner struct {
	o Options
	s screenkit.Session
}

// reading is one reading of the host: the control's, why the accounts were
// not read (err), and the User row.
type reading struct {
	usersctl.Reading
	err    error
	status component.StatusList
}

// read reads the host as the control reads it (users.Read, writing nothing,
// running only the tools' checks and sshd -T), and the User row from it.
func (r *runner) read(ctx context.Context) reading {
	rd, err := usersctl.Read(ctx, r.o.System, r.o.Facts, r.o.Getenv)
	return reading{Reading: rd, err: err, status: statusRow(rd)}
}

// user is the user named name in h, and whether it is listed.
func (h reading) user(name string) (usersctl.User, bool) {
	for _, u := range h.Users {
		if u.Name == name {
			return u, true
		}
	}
	return usersctl.User{}, false
}

// bind is the menu of items, each bound to its action by key.
func bind(exit component.MenuItem, items []component.MenuItem, actions map[string]func() error) nav.Menu {
	menu := nav.Menu{Exit: exit}
	for _, it := range items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: actions[it.Key]})
	}
	return menu
}
