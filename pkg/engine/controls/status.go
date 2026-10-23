// Package controls holds the main menu's one list of status controls
// (refactor plan slice 2, next/s2-menu-live): the controls whose checks
// make its status rows, in their order, for hardn's main menu
// (cmd/hardn), the gallery's status hosts and internal/tools/p4status
// alike. Each area's control is its own package under this directory.
package controls

import (
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/apparmor"
	"github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
)

// Status returns the main menu's status controls, in its rows' order: the
// five areas' Controls() in Figma's order (User, Firewall, SSH Login, SSH
// Auth, SSH Port, AppArmor, Auto Updates), then DNS, checked against cfg,
// hardn.yml as loaded (dns.Controls). DNS comes last so that Figma's
// seven rows keep their places.
//
// The order is risk.Rows': the main menu draws its rows in that order,
// whatever the reports' (output.StatusViewOf), and TestStatus holds this
// list to it. A new row, or another order (P-01's one list of rows and
// menu items, still proposed), changes both: risk's rows, with the row's
// weight, and this list.
func Status(cfg *config.Config) []engine.Control {
	var out []engine.Control
	for _, cs := range [][]engine.Control{users.Controls(), firewall.Controls(), ssh.Controls(), apparmor.Controls(), updates.Controls(), dns.Controls(cfg)} {
		out = append(out, cs...)
	}
	return out
}
