// Package controls holds the main menu's one list of status controls
// (refactor plan slice 2, next/s2-menu-live): the controls whose checks
// make its status rows, in their order, for hardn's main menu
// (cmd/hardn), the gallery's status hosts and internal/tools/p4status
// alike. Each area's control is its own package under this directory.
package controls

import (
	"slices"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/apparmor"
	"github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/logforward"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
)

// Status returns the main menu's status controls, in its rows' order, the
// maintainer's (2026-11-07): the areas' Controls(), User, SSH Login, SSH
// Auth, SSH Port, Firewall, DNS (checked against cfg, hardn.yml as loaded:
// dns.Controls), Auto Updates, AppArmor.
//
// The order is risk.Rows': the main menu draws its rows in that order,
// whatever the reports' (output.StatusViewOf), and TestStatus holds this
// list to it. A new row, or another order, changes both: risk's rows, with
// the row's weight, and this list.
func Status(cfg *config.Config) []engine.Control {
	var out []engine.Control
	for _, cs := range [][]engine.Control{users.Controls(), ssh.Controls(), firewall.Controls(), dns.Controls(cfg), updates.Controls(), apparmor.Controls()} {
		out = append(out, cs...)
	}
	return out
}

// Menu returns the controls the main menu checks: Status's, then Log
// Forwarding's, which is no row of risk's: the risk level never counts it
// (gallery-logforward.md, decision 10 (a)).
func Menu(cfg *config.Config) []engine.Control {
	return append(Status(cfg), logforward.Controls(cfg)...)
}

// MenuReports are Menu's reports as the main menu draws them: Log
// Forwarding's kept only where cfg, hardn.yml, names a server or the host
// forwards (logforward.Shown), and then drawn after the others
// (output.StatusViewOf).
func MenuReports(cfg *config.Config, reports []engine.Report) []engine.Report {
	return slices.DeleteFunc(slices.Clone(reports), func(r engine.Report) bool {
		return r.Control == logforward.ID && !logforward.Shown(cfg, r.Status)
	})
}
