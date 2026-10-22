package output

import (
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// MainMenuOf is base with the blocks the status model owns filled from one
// reading of a host (refactor plan Phase 4, next/p4-vmcheck): the header,
// the distribution's bar and the host block from the header probe's h
// (HeaderViewOf, with base's version), and the risk summary and the status
// rows from Engine.Check's reports and their risk level l (StatusViewOf).
// base keeps its menu items, heading, mode and prompt, which are not the
// status model's: until the main menu goes live (slice 2) they are the
// mock's, screen.MainMenuFixture. The gallery's status hosts and
// internal/tools/p4status draw the main menu with it, so the two cannot
// differ. It is pure.
func MainMenuOf(base screen.MainMenu, h header.Header, reports []engine.Report, l risk.Level) screen.MainMenu {
	hv := HeaderViewOf(base.Header.Version, h)
	sv := StatusViewOf(reports, l)
	base.Header, base.Context, base.Host = hv.Header, hv.Context, hv.Host
	base.Risk, base.Status = sv.Risk, sv.Status
	return base
}
