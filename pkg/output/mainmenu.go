package output

import (
	"slices"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// MainMenuOf is base with the blocks the status model owns filled from one
// reading of a host (refactor plan Phase 4, next/p4-vmcheck): the header,
// the distribution's bar and the host block from the header probe's h
// (HeaderViewOf, with base's version), the risk summary and the status
// rows from Engine.Check's reports and their risk level l (StatusViewOf),
// and the alert from l (AlertOf), whose factors are then no line under a
// row. base keeps its menu items, heading, mode and prompt, which are not
// the status model's: until the main menu goes live (slice 2) they are the
// mock's, screen.MainMenuFixture. The gallery's status hosts and
// internal/tools/p4status draw the main menu with it, so the two cannot
// differ. It is pure.
func MainMenuOf(base screen.MainMenu, h header.Header, reports []engine.Report, l risk.Level) screen.MainMenu {
	hv := HeaderViewOf(base.Header.Version, h)
	alert, rest := AlertOf(l)
	sv := StatusViewOf(reports, rest)
	base.Header, base.Context, base.Host = hv.Header, hv.Context, hv.Host
	base.Risk, base.Status, base.Alert = sv.Risk, sv.Status, alert
	return base
}

// The CVE alert's words (the maintainer, 2026-11-08).
const (
	AlertCVE = "CVE-2026-123158"
	AlertFix = "Upgrade openssh."
)

// alertKinds are the factors the main menu raises as its alert: an sshd
// that lacks CVE-2026-123158's fix, established, whether anyone can log
// in (risk.KindPasswordBypass), no account is exposed yet
// (risk.KindSSHDUnfixed) or no password path to it is established
// (risk.KindUnfixedLatent). Whether sshd has the fix not established
// (risk.KindFixUnknown) stays a muted line under SSH Auth: an alert is for
// a fact hardn established.
var alertKinds = []risk.Kind{risk.KindPasswordBypass, risk.KindSSHDUnfixed, risk.KindUnfixedLatent}

// AlertOf is the main menu's alert from l, nil for none, and l without
// the factors it raises, so that each is drawn once: the CVE's, its fix
// and the route to SSH Login, where item 5 upgrades openssh. The level,
// its band and glosses are l's; only the factors differ. Other screens'
// rows, SSH Login's among them, keep the lines (StatusViewOf over l). It
// is pure.
func AlertOf(l risk.Level) (*screen.Alert, risk.Level) {
	var alert *screen.Alert
	rest := l
	rest.Factors = nil
	for _, f := range l.Factors {
		if slices.Contains(alertKinds, f.Kind) {
			alert = &screen.Alert{Title: AlertCVE, Fix: AlertFix, Item: ssh.Login{}.Title()}
			continue
		}
		rest.Factors = append(rest.Factors, f)
	}
	return alert, rest
}
