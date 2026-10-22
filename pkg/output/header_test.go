package output_test

import (
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// figmaUptime is Figma's 00-TUI-MAIN uptime.
const figmaUptime = 182*24*time.Hour + 2*time.Hour + 26*time.Minute + 42*time.Second

// TestFormatUptime: Figma's form, shortened only by units that are 0 and
// the singular.
func TestFormatUptime(t *testing.T) {
	for d, want := range map[time.Duration]string{
		figmaUptime:                    "182 days, 2 hours, 26 minutes",
		0:                              "0 minutes",
		59 * time.Second:               "0 minutes",
		time.Minute:                    "1 minute",
		time.Hour:                      "1 hour",
		24 * time.Hour:                 "1 day",
		25*time.Hour + time.Minute:     "1 day, 1 hour, 1 minute",
		3*24*time.Hour + 5*time.Minute: "3 days, 5 minutes",
		2*time.Hour + 59*time.Second:   "2 hours",
		9999*24*time.Hour + 23*time.Hour + 59*time.Minute: "9999 days, 23 hours, 59 minutes",
		-time.Minute: "0 minutes",
	} {
		if got := output.FormatUptime(d); got != want {
			t.Errorf("FormatUptime(%v) = %q, want %q", d, got, want)
		}
	}
}

// The header fixtures: the probe's data as each platform gives it, built
// as data (the probe's own tests cover how it reads them). Synthetic
// values only.
var errDenied = &fs.PathError{Op: "open", Path: "/proc/uptime", Err: fs.ErrPermission}

// routed is a family with a default route on iface and addr there.
func routed(iface, addr string) header.Family {
	return header.Family{Route: detect.Yes, Iface: iface, Addr: netip.MustParseAddr(addr)}
}

// noRoute is a family established to have no default route.
var noRoute = header.Family{Route: detect.No}

// up is an uptime read.
func up(d time.Duration) header.Uptime { return header.Uptime{Duration: d, Known: true} }

func headerFixtures() []struct {
	name string
	h    header.Header
} {
	debian := header.Header{
		OS:       header.OS{Name: "Debian", Version: "13", Codename: "trixie"},
		Hostname: header.Hostname{Name: "web-01.example.net"},
		Address:  header.Address{IPv4: routed("eth0", "192.0.2.10"), IPv6: routed("eth0", "2001:db8:1::10")},
		Uptime:   up(figmaUptime),
		Reboot: header.Reboot{Pending: detect.Yes, Running: "6.12.43+deb13-amd64",
			Findings: []engine.Finding{{Code: header.CodeNewerKernel, Detail: header.DetailNewerKernel, Source: "/boot/vmlinuz-6.12.48+deb13-amd64"}}},
	}
	ubuntu := header.Header{
		OS:       header.OS{Name: "Ubuntu", Version: "26.04", Codename: "resolute"},
		Hostname: header.Hostname{Name: "web-01.example.net"},
		Address:  header.Address{IPv4: noRoute, IPv6: routed("eth0", "2001:db8:2::20")},
		Uptime:   up(3*24*time.Hour + 5*time.Minute),
		Reboot:   header.Reboot{Pending: detect.No},
	}
	pve := header.Header{
		OS:       header.OS{Name: "Proxmox VE", Version: "9.0.10", Codename: "trixie"},
		Hostname: header.Hostname{Name: "web-01"},
		Address:  header.Address{IPv4: routed("vmbr0", "192.0.2.30"), IPv6: noRoute},
		Uptime:   up(41*24*time.Hour + time.Hour),
		Reboot:   header.Reboot{Pending: detect.Unknown, Err: errors.New("no vmlinuz")},
	}
	alpine := header.Header{
		OS:       header.OS{Name: "Alpine", Version: "3.24.0"},
		Hostname: header.Hostname{Name: "web-01.example.net"},
		Address:  header.Address{IPv4: routed("eth0", "192.0.2.20"), IPv6: noRoute},
		Uptime:   up(59 * time.Second),
		Reboot:   header.Reboot{Pending: detect.No},
	}
	isolated := header.Header{
		OS:       header.OS{Name: "Debian", Version: "12", Codename: "bookworm"},
		Hostname: header.Hostname{Name: "web-01.example.net"},
		Address:  header.Address{IPv4: noRoute, IPv6: noRoute},
		Uptime:   up(24 * time.Hour),
	}
	unknown := header.Header{
		OS:       header.OS{Err: errDenied},
		Hostname: header.Hostname{Err: errDenied},
		Address:  header.Address{IPv4: header.Family{Err: errDenied}, IPv6: header.Family{Err: errDenied}},
		Uptime:   header.Uptime{Duration: time.Hour, Err: errDenied},
		Reboot:   header.Reboot{Pending: detect.Unknown, Err: errDenied},
	}
	return []struct {
		name string
		h    header.Header
	}{{"debian13", debian}, {"ubuntu2604", ubuntu}, {"pve9", pve}, {"alpine324", alpine}, {"no-default-route", isolated}, {"unknown", unknown}}
}

// TestHeaderViewOf: each fact to its block's field.
func TestHeaderViewOf(t *testing.T) {
	want := map[string]output.HeaderView{
		"debian13": {Context: component.ContextBar{Name: "Debian", Version: "13", Codename: "Trixie"},
			Host: component.HostBlock{Hostname: "web-01.example.net", Address: "192.0.2.10", Uptime: "182 days, 2 hours, 26 minutes", Reboot: component.RebootPending}},
		"ubuntu2604": {Context: component.ContextBar{Name: "Ubuntu", Version: "26.04", Codename: "Resolute"},
			Host: component.HostBlock{Hostname: "web-01.example.net", Address: "2001:db8:2::20", Uptime: "3 days, 5 minutes"}},
		"pve9": {Context: component.ContextBar{Name: "Proxmox VE", Version: "9.0.10", Codename: "Trixie"},
			Host: component.HostBlock{Hostname: "web-01", Address: "192.0.2.30", Uptime: "41 days, 1 hour", Reboot: component.RebootUnknown}},
		"alpine324": {Context: component.ContextBar{Name: "Alpine", Version: "3.24.0"},
			Host: component.HostBlock{Hostname: "web-01.example.net", Address: "192.0.2.20", Uptime: "0 minutes"}},
		"no-default-route": {Context: component.ContextBar{Name: "Debian", Version: "12", Codename: "Bookworm"},
			Host: component.HostBlock{Hostname: "web-01.example.net", Address: output.NoDefaultRoute, Uptime: "1 day", Reboot: component.RebootUnknown}},
		"unknown": {Host: component.HostBlock{Reboot: component.RebootUnknown}},
	}
	for _, fx := range headerFixtures() {
		t.Run(fx.name, func(t *testing.T) {
			w := want[fx.name]
			w.Header = component.Header{Version: "v0.4.0", URL: "https://github.com/abbott/hardn"}
			if got := output.HeaderViewOf("v0.4.0", fx.h); got != w {
				t.Errorf("got  %+v\nwant %+v", got, w)
			}
		})
	}
}

// TestHeaderAddress: the address shown for each state of the two
// families.
func TestHeaderAddress(t *testing.T) {
	bare := header.Family{Route: detect.Yes, Iface: "eth0"}
	failed := header.Family{Route: detect.Yes, Iface: "eth0", Err: errDenied}
	unread := header.Family{Err: errDenied}
	for _, c := range []struct {
		name string
		a    header.Address
		want string
	}{
		{"IPv4 before IPv6", header.Address{IPv4: routed("eth0", "192.0.2.10"), IPv6: routed("eth0", "2001:db8:1::10")}, "192.0.2.10"},
		{"IPv6 where IPv4 has no route", header.Address{IPv4: noRoute, IPv6: routed("eth0", "2001:db8:1::10")}, "2001:db8:1::10"},
		{"IPv6 where IPv4 has no address", header.Address{IPv4: bare, IPv6: routed("eth0", "2001:db8:1::10")}, "2001:db8:1::10"},
		{"IPv4's address not read, an IPv6 one", header.Address{IPv4: failed, IPv6: routed("eth0", "2001:db8:1::10")}, ""},
		{"IPv4's route not read, an IPv6 address", header.Address{IPv4: unread, IPv6: routed("eth0", "2001:db8:1::10")}, ""},
		{"no route in either", header.Address{IPv4: noRoute, IPv6: noRoute}, output.NoDefaultRoute},
		{"a route, no address", header.Address{IPv4: bare, IPv6: noRoute}, output.NoAddress},
		{"IPv4 not read, no IPv6 route", header.Address{IPv4: failed, IPv6: noRoute}, ""},
		{"no IPv4 route, IPv6 not read", header.Address{IPv4: noRoute, IPv6: unread}, ""},
		{"neither read (the zero Address)", header.Address{}, ""},
	} {
		if got := output.HeaderViewOf("", header.Header{Address: c.a}).Host.Address; got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// TestHeaderViewOfZero: a Header Read did not fill shows every fact as
// not established, the version and the link alone, and the reboot line
// says it could not tell, as the risk level counts it.
func TestHeaderViewOfZero(t *testing.T) {
	want := output.HeaderView{Header: component.Header{Version: "v0.4.0", URL: output.RepoURL},
		Host: component.HostBlock{Reboot: component.RebootUnknown}}
	if got := output.HeaderViewOf("v0.4.0", header.Header{}); got != want {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// TestRebootLine: the host block's reboot line and the risk level's
// reboot factor are one rule, for every header.Reboot: a line exactly
// where the level counts the reboot, the warning's where it is
// established, and the factor's words on it; the factor drawn nowhere
// else (StatusViewOf has no row for it).
func TestRebootLine(t *testing.T) {
	c := component.NewCtx(plain80(t))
	for _, p := range []detect.Tri{detect.Unknown, detect.Yes, detect.No, detect.Tri(9)} {
		for _, hostKernel := range []bool{false, true} {
			r := header.Reboot{Pending: p, HostKernel: hostKernel}
			name := fmt.Sprintf("pending %v, host kernel %t", p, hostKernel)
			var counted []risk.Factor
			l := risk.Assess(nil, header.Header{Reboot: r})
			for _, f := range l.Factors {
				if f.Kind == risk.KindReboot {
					counted = append(counted, f)
				}
			}
			host := output.HeaderViewOf("", header.Header{Reboot: r}).Host
			want := component.RebootNone
			if len(counted) == 1 {
				want = component.RebootUnknown
				if counted[0].Established {
					want = component.RebootPending
				}
				if out := host.Render(c); !strings.Contains(out, "- "+counted[0].Detail+"\n") && !strings.HasSuffix(out, "- "+counted[0].Detail) {
					t.Errorf("%s: the host block\n%s\ndoes not say %q", name, out, counted[0].Detail)
				}
			}
			if host.Reboot != want || len(counted) > 1 {
				t.Errorf("%s: line %d, want %d for the factors %+v", name, host.Reboot, want, counted)
			}
			for _, row := range output.StatusViewOf(nil, l).Status.Rows {
				for _, line := range row.Lines {
					if len(counted) == 1 && line.Text == counted[0].Detail {
						t.Errorf("%s: the reboot under %s too", name, row.Label)
					}
				}
			}
		}
	}
}

// plain80 is the core set's plain cell at 80 columns: no escape sequence.
func plain80(t *testing.T) term.Env {
	t.Helper()
	for _, c := range uitest.Core() {
		if c.Name == "plain-80" {
			return c.Env
		}
	}
	t.Fatal("no plain-80 cell in uitest.Core()")
	return term.Env{}
}

// TestHeaderBlocks pins the header blocks of each fixture, as the main
// menu lays them out (screen.MainMenu: the blocks a section gap apart),
// on the core cells.
func TestHeaderBlocks(t *testing.T) {
	gap := strings.Repeat("\n", theme.SectionGap+1)
	uitest.Golden(t, "human/header", uitest.Core(), func(env term.Env) string {
		c := component.NewCtx(env)
		var out []string
		for _, fx := range headerFixtures() {
			v := output.HeaderViewOf("v0.4.0", fx.h)
			out = append(out, "["+fx.name+"]\n"+strings.Join([]string{v.Header.Render(c), v.Context.Render(c), v.Host.Render(c)}, gap))
		}
		return strings.Join(out, "\n\n") + "\n"
	})
}
