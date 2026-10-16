package component

import "github.com/abbott/hardn/pkg/ui/theme"

// rebootPending is the pending-reboot line's word (proposal P-21; the
// finding itself is P-04).
const rebootPending = "Reboot pending"

// HostBlock identifies the host: the hostname badge at theme.Indent, the
// primary address beside it at theme.ValueCol, the uptime under that and,
// when set, a pending-reboot line under the uptime; here with no colour
// and ASCII glyphs, under the column numbers:
//
//	0 2   6                    27
//	  [BOS-SAN-L01]            192.0.2.10
//	                           182 days, 2 hours, 26 minutes
//	                           [*] Reboot pending
//
// A value hardn could not read shows "Unknown" in status.unknown; so does
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
	// RebootPending adds the pending-reboot line: Glyphs.Partial, the
	// warning marker, and "Reboot pending", in status.warn.
	RebootPending bool
}

// Render renders the block for c. See HostBlock for the layout.
func (vm HostBlock) Render(c Ctx) string {
	t := c.Theme.Host
	label := line{gap(theme.Indent), {unknown, t.Unknown}}
	if name := clean(vm.Hostname); name != "" {
		label[1] = c.badge(name, c.width()-theme.Indent)
	}
	values := []line{
		known(vm.Address, t.Address, t.Unknown),
		known(vm.Uptime, t.Uptime, t.Unknown),
	}
	if vm.RebootPending {
		values = append(values, line{{c.Theme.Glyphs.Partial + " " + rebootPending, t.Reboot}})
	}
	return c.columns(label, values...)
}
