// Package kernelsettings holds the Kernel Settings control
// (docs/roadmap.md, "sysctl"; docs/engine/gallery-sysctl.md, adopted
// 2026-11-14, built in its §9): the kernel's network and
// kernel settings against hardn's baseline, and the two changes that set
// the ones below it or put the earlier values back.
//
// Check and Read tell each setting's value, every interface's among them
// (pkg/host/sysctl). Plan, by Action, writes hardn's one file in
// /etc/sysctl.d and sets its values now (Apply), or deletes it and sets
// the recorded values back (Remove). hardn never writes a forwarding key,
// accept_ra or the "all" reverse-path filter, never lowers a value, edits
// no file but its own, and enables no service.
package kernelsettings

import (
	"context"
	"fmt"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	khost "github.com/abbott/hardn/pkg/host/sysctl"
)

// ID is the control's ID, stable once released.
const ID = "kernel.settings"

// Title is the row's label, the screen's title and the menu item's.
const Title = "Kernel Settings"

// The row's details (gallery-sysctl.md §6).
const (
	DetailToSet     = "%d to set"            // NotConfigured, or Partial beside hardn's file
	DetailOutside   = "%d set outside hardn" // Partial
	DetailNotAtBoot = "not set at boot"      // Partial: OpenRC's sysctl service is in no runlevel
	DetailByHardn   = "by hardn"             // Configured, with hardn's file
	DetailContainer = "in a container"       // NotApplicable
	DetailCantRead  = "can't read"           // Unknown
)

// Action is what Settings' Plan does.
type Action uint8

// The actions.
const (
	// ActionNone is the status row's: Plan is engine.ErrNotMigrated.
	ActionNone Action = iota
	// ActionApply sets the settings below the baseline.
	ActionApply
	// ActionRemove deletes hardn's file and sets the earlier values back.
	ActionRemove
)

// Settings is Kernel Settings. The risk level never counts the row
// (decision 18): it is no row of risk.Rows.
type Settings struct {
	Action Action
}

// Controls is the Kernel Settings row.
func Controls() []engine.Control { return []engine.Control{Settings{}} }

// ID returns ID.
func (Settings) ID() string { return ID }

// Title returns Title.
func (Settings) Title() string { return Title }

// Applies is true on every host: a container's row is Not Applicable with
// its words, and the main menu leaves it out (Shown).
func (Settings) Applies(detect.Facts) (bool, string) { return true, "" }

// Read reads the host, writing nothing.
func Read(ctx context.Context, sys host.System, f detect.Facts) khost.Reading {
	return khost.Read(ctx, sys, f)
}

// Shown reports whether the main menu draws the row whose check gave st
// (decision 16): on every host but a container.
func Shown(st engine.Status) bool { return st.State != engine.NotApplicable }

// NotAtBoot reports whether hardn's file is there and nothing applies it
// at boot: under OpenRC, with the sysctl service in no runlevel (F5).
func NotAtBoot(r khost.Reading) bool { return r.Own != nil && r.OpenRC && r.Boot == detect.No }

// Status is the row: Not Applicable in a container; Unknown where a key or
// a file is not read, never a pass; Partial where a later file holds a key
// below the baseline, where a key is below it beside hardn's file, or
// where hardn's file is not applied at boot; Not Configured with keys to
// set and no file of hardn's; else Configured.
func Status(r khost.Reading) engine.Status {
	unknown := func(err error) engine.Status {
		st := engine.Status{State: engine.Unknown, Detail: DetailCantRead}
		if err != nil {
			st.Evidence = []string{logview.Safe(err.Error())}
		}
		return st
	}
	held, below := r.Count(khost.Held), r.Count(khost.Below)
	row, unread := r.Unread()
	switch {
	case r.Container:
		return engine.Status{State: engine.NotApplicable, Detail: DetailContainer}
	case r.Err != nil:
		return unknown(r.Err)
	case unread:
		return unknown(row.Err)
	case held > 0:
		return engine.Status{State: engine.Partial, Detail: fmt.Sprintf(DetailOutside, held)}
	case below > 0 && r.Own != nil:
		return engine.Status{State: engine.Partial, Detail: fmt.Sprintf(DetailToSet, below)}
	case below > 0:
		return engine.Status{State: engine.NotConfigured, Detail: fmt.Sprintf(DetailToSet, below)}
	case NotAtBoot(r):
		return engine.Status{State: engine.Partial, Detail: DetailNotAtBoot}
	case r.Own != nil:
		return engine.Status{State: engine.Configured, Detail: DetailByHardn}
	}
	return engine.Status{State: engine.Configured}
}

// Check is the row (Status). What is not read is Unknown with no error:
// nothing failed that the row does not say.
func (Settings) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	return Status(Read(ctx, sys, f)), ctx.Err()
}
