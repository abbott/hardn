package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/ui/component"
)

// RepoURL is the repository link in the main menu's header, as Figma's
// 00-TUI-MAIN shows it.
const RepoURL = "https://github.com/abbott/hardn"

// The host block's words for an address the probe established there is
// none of (proposed wording, docs/engine/header.md §5.2). An address not
// established is the component's "Unknown".
const (
	NoDefaultRoute = "no default route"
	NoAddress      = "no address"
)

// HeaderView is the main menu's first three blocks (refactor plan Phase 4,
// next/p4-header), the fields of screen.MainMenu that carry them: the
// wordmark's line, the distribution's bar and the host.
type HeaderView struct {
	Header  component.Header
	Context component.ContextBar
	Host    component.HostBlock
}

// HeaderViewOf maps the header probe's h to the main menu's header
// blocks. version is hardn's, shown as given ("v0.4.0"). A fact the probe
// did not establish is empty, which the components show as "Unknown"; a
// pending reboot is set only when the probe established one (P-21 holds
// its wording and place). It is pure.
func HeaderViewOf(version string, h header.Header) HeaderView {
	v := HeaderView{
		Header:  component.Header{Version: version, URL: RepoURL},
		Context: component.ContextBar{Name: h.OS.Name, Version: h.OS.Version, Codename: capitalised(h.OS.Codename)},
		Host: component.HostBlock{Hostname: h.Hostname.Name, Address: addressWords(h.Address),
			RebootPending: h.Reboot.Pending == detect.Yes},
	}
	if h.Uptime.Err == nil {
		v.Host.Uptime = FormatUptime(h.Uptime.Duration)
	}
	return v
}

// capitalised is a codename as Figma shows it: "trixie" as "Trixie".
func capitalised(s string) string {
	if s != "" && s[0] >= 'a' && s[0] <= 'z' {
		return strings.ToUpper(s[:1]) + s[1:]
	}
	return s
}

// addressWords is the primary address, NoDefaultRoute or NoAddress when
// the probe established there is none, and "" when it did not establish
// it.
func addressWords(a header.Address) string {
	addr, known := a.Primary()
	switch {
	case addr.IsValid():
		return addr.String()
	case !known:
		return ""
	case a.IPv4.Iface == "" && a.IPv6.Iface == "":
		return NoDefaultRoute
	}
	return NoAddress
}

// FormatUptime is an uptime in Figma's form, "182 days, 2 hours, 26
// minutes", made as short as it says the same (design-system README
// §9.15): whole minutes, a unit that is 0 left out, "1 day", "1 hour" and
// "1 minute" for one, and "0 minutes" under a minute.
func FormatUptime(d time.Duration) string {
	m := int64(d / time.Minute)
	var parts []string
	for _, u := range []struct {
		n         int64
		one, many string
	}{{m / (24 * 60), "day", "days"}, {m / 60 % 24, "hour", "hours"}, {m % 60, "minute", "minutes"}} {
		switch {
		case u.n == 1:
			parts = append(parts, "1 "+u.one)
		case u.n > 1:
			parts = append(parts, fmt.Sprintf("%d %s", u.n, u.many))
		}
	}
	if len(parts) == 0 {
		return "0 minutes"
	}
	return strings.Join(parts, ", ")
}
