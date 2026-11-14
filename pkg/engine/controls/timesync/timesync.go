// Package timesync holds the Time Sync control (docs/roadmap.md, "Time
// sync"; docs/engine/gallery-timesync.md, adopted 2026-11-14;
// docs/engine/screen-timesync.md): which service keeps the host's time,
// whether the clock is in sync, and the two changes that set hardn.yml's
// servers on that service or take them out again.
//
// Check and Read tell the service, the clock and the servers
// (pkg/host/timesync), and the servers hardn.yml's ntpServers names. Plan,
// by Action, makes the service use those in place of its own (Set) or puts
// its own back (Remove: hardn's own setting only). hardn installs, starts
// and swaps no time service, and neither change saves hardn.yml.
package timesync

import (
	"context"
	"slices"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	tshost "github.com/abbott/hardn/pkg/host/timesync"
)

// ID is the control's ID, stable once released.
const ID = "time.sync"

// Title is the row's label, the screen's title and the menu item's.
const Title = "Time Sync"

// The row's details (gallery-timesync.md §6); a Configured row's is the
// server the clock follows, or DetailInSync where the service does not say.
const (
	DetailInSync     = "in sync"                 // Configured: busybox ntpd
	DetailNotInSync  = "not in sync"             // Partial
	DetailNotWanted  = "not hardn.yml's servers" // Partial: hardn's own setting names others
	DetailNoService  = "no time service"         // NotConfigured
	DetailNotRunning = " not running"            // NotConfigured, after the service's name
	DetailContainer  = "in a container"          // NotApplicable
	DetailCantRead   = "can't read"              // Unknown
)

// Action is what Sync's Plan does.
type Action uint8

// The actions.
const (
	// ActionNone is the status row's: Plan is engine.ErrNotMigrated.
	ActionNone Action = iota
	// ActionSet makes the time service use hardn.yml's servers.
	ActionSet
	// ActionRemove takes hardn's own setting out.
	ActionRemove
)

// Sync is Time Sync. Config is hardn.yml as loaded, whose ntpServers Check
// compares the host with; Plan reads the engine's (engine.Input.Config),
// which is the same file. The risk level never counts the row (decision
// 11): it is no row of risk.Rows.
type Sync struct {
	Action Action
	Config *config.Config
}

// Controls is the Time Sync row against cfg.
func Controls(cfg *config.Config) []engine.Control { return []engine.Control{Sync{Config: cfg}} }

// ID returns ID.
func (Sync) ID() string { return ID }

// Title returns Title.
func (Sync) Title() string { return Title }

// Applies is true on every host: a container's row is Not Applicable with
// its words, and the main menu leaves it out (Shown).
func (Sync) Applies(detect.Facts) (bool, string) { return true, "" }

// Reading is the host read for Time Sync.
type Reading struct {
	// Host is the service, the clock and the servers. Its text is the
	// host's: Status and the screen make it safe to show.
	Host tshost.Reading
	// Want are the servers hardn.yml names, checked; nil for none. WantErr
	// is why ntpServers is not valid (config.ErrNTPServers): nothing is set
	// from it.
	Want    []string
	WantErr error
}

// Read reads the host, writing nothing, and cfg's servers.
func Read(ctx context.Context, sys host.System, f detect.Facts, cfg *config.Config) Reading {
	r := Reading{Host: tshost.Read(ctx, sys, f)}
	if cfg != nil {
		r.Want, r.WantErr = cfg.NTPServers.Resolve()
	}
	return r
}

// Differs reports whether hardn.yml names servers and hardn's own setting
// names others, or in another order.
func (r Reading) Differs() bool {
	own := r.Host.Own()
	return len(r.Want) > 0 && len(own) > 0 && !slices.Equal(r.Want, own)
}

// Shown reports whether the main menu draws the row whose check gave st
// (decision 11 (a)): on every host but a container.
func Shown(st engine.Status) bool { return st.State != engine.NotApplicable }

// Status is the row: Not Applicable in a container; Unknown where the
// service, its settings or the clock are not read, never a pass; Not
// Configured with no time service or one that is not running; Partial
// where the clock is not in sync, or hardn's own setting is not hardn.yml's
// servers; else Configured, with the server the clock follows.
func (r Reading) Status() engine.Status {
	h := r.Host
	unknown := func(err error) engine.Status {
		st := engine.Status{State: engine.Unknown, Detail: DetailCantRead}
		if err != nil {
			st.Evidence = []string{logview.Safe(err.Error())}
		}
		return st
	}
	switch {
	case h.Container:
		return engine.Status{State: engine.NotApplicable, Detail: DetailContainer}
	case h.Err != nil:
		return unknown(h.Err)
	case h.Service == tshost.NoService:
		return engine.Status{State: engine.NotConfigured, Detail: DetailNoService}
	case h.Running != detect.Yes:
		return engine.Status{State: engine.NotConfigured, Detail: h.Service.String() + DetailNotRunning}
	case h.Clock == tshost.ClockUnknown:
		return unknown(h.ClockErr)
	case h.Clock == tshost.NotInSync:
		return engine.Status{State: engine.Partial, Detail: DetailNotInSync}
	case r.Differs():
		return engine.Status{State: engine.Partial, Detail: DetailNotWanted, Evidence: []string{logview.Safe(h.File)}}
	case h.Synced == "":
		return engine.Status{State: engine.Configured, Detail: DetailInSync}
	}
	return engine.Status{State: engine.Configured, Detail: logview.Safe(h.Synced)}
}

// Check is the row (Reading.Status). What is not read is Unknown with no
// error: nothing failed that the row does not say.
func (c Sync) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	return Read(ctx, sys, f, c.Config).Status(), ctx.Err()
}
