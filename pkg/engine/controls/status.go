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
	"github.com/abbott/hardn/pkg/engine/controls/kernelsettings"
	"github.com/abbott/hardn/pkg/engine/controls/logforward"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/timesync"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
)

// Status returns the status controls, the areas' Controls(): the scored
// rows', in risk.Rows' order, the maintainer's (2026-11-07 and
// 2026-11-14): User, SSH Login, SSH Auth, SSH Port, Firewall, Kernel, Auto
// Updates, AppArmor; then DNS (checked against cfg, hardn.yml as loaded:
// dns.Controls), which the score never counts. The report to hand over
// checks these (cmd/hardn).
//
// The main menu draws the scored rows in risk.Rows' order, whatever the
// reports' (output.StatusViewOf), and TestStatus holds this list to it. A
// new row, or another order, changes both: risk's rows, with the row's
// weight, and this list.
func Status(cfg *config.Config) []engine.Control {
	var out []engine.Control
	for _, cs := range [][]engine.Control{users.Controls(), ssh.Controls(), firewall.Controls(), kernelsettings.Controls(), updates.Controls(),
		apparmor.Controls(), dns.Controls(cfg)} {
		out = append(out, cs...)
	}
	return out
}

// Info are the controls the main menu shows only with a problem, in its
// order, the maintainer's (2026-11-14): DNS, NTP, Syslog. The risk level
// never counts them (gallery-logforward.md, decision 10 (a);
// gallery-timesync.md, decision 11 (a)).
var Info = []string{dns.ID, timesync.ID, logforward.ID}

// Menu returns the controls the main menu checks: Status's, then NTP
// Settings' and Syslog Settings' (Info's order). Kernel Settings is among
// Status's on every host, whatever cfg's kernel says: that key is Run
// All's alone (docs/engine/gallery-menu-layout.md, decision 5 (a)). Of
// Info's reports the main menu draws only those with a problem
// (engine.Status.Problem, or a check that failed: output.StatusViewOf).
func Menu(cfg *config.Config) []engine.Control {
	return append(append(Status(cfg), timesync.Controls(cfg)...), logforward.Controls(cfg)...)
}
