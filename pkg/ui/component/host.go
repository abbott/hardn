package component

import "github.com/abbott/hardn/pkg/ui/theme"

// RebootLine is the host block's pending-reboot line: none, a reboot
// pending, or whether one is pending not established. The status model
// decides it by the rule its risk level counts the reboot by
// (pkg/engine/risk RebootFactor).
type RebootLine int

// The reboot lines. The zero value is RebootNone: no line.
const (
	RebootNone    RebootLine = iota
	RebootPending            // "- Reboot pending", in status.warn
	RebootUnknown            // "- Can't tell if reboot pending", in text.muted
)

// The reboot lines' words (the maintainer, 2026-10-21; P-21 for the line,
// P-04 for the finding).
const (
	rebootPending = "Reboot pending"
	rebootUnknown = "Can't tell if reboot pending"
)

// HostBlock identifies the host: the hostname badge at theme.Indent, the
// primary address beside it at theme.ValueCol, the uptime under that and,
// when set, the reboot line under the uptime; here with no colour, under
// the column numbers:
//
//	0 2   6                    27
//	  [BOS-SAN-L01]            192.0.2.10
//	                           182 days, 2 hours, 26 minutes
//	                           - Reboot pending
//
// A value hardn could not read shows "Unknown" in text.muted; so does
// an empty hostname, in place of the badge. A badge that reaches the value
// column stacks the block (DEV-021), and one wider than the content is cut
// with the ellipsis glyph.
type HostBlock struct {
	// Hostname is shown in the badge as given.
	Hostname string
	// Address is the primary IP address.
	Address string
	// Uptime is shown as given, such as "182 days, 2 hours, 26 minutes".
	Uptime string
	// Reboot adds the reboot line, the pending reboot's only place on the
	// main menu (it has no status row): Glyphs.Bullet and "Reboot
	// pending", in status.warn (Host.Reboot), or "Can't tell if reboot
	// pending", in text.muted (Host.Unknown), dash-led as the lines under
	// the status rows are (the maintainer, 2026-10-21). P-04 places the
	// flag next to the uptime, which a long uptime leaves no room for
	// beside the label, so it goes under it.
	Reboot RebootLine
}

// Render renders the block for c. See HostBlock for the layout.
func (vm HostBlock) Render(c Ctx) string {
	t := c.Theme.Host
	label := line{gap(theme.Indent), {unknown, t.Unknown}}
	if name := c.text(vm.Hostname); name != "" {
		label[1] = c.badge(name, c.width()-theme.Indent)
	}
	values := []line{
		c.known(vm.Address, t.Address, t.Unknown),
		c.known(vm.Uptime, t.Uptime, t.Unknown),
	}
	switch b := c.Theme.Glyphs.Bullet + " "; vm.Reboot {
	case RebootPending:
		values = append(values, line{{b + rebootPending, t.Reboot}})
	case RebootUnknown:
		values = append(values, line{{b + rebootUnknown, t.Unknown}})
	}
	return c.columns(label, values...)
}
