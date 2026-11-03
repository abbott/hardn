package ufw_test

import (
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/host/ufw"
)

// TestOnlyFrom: the sources of the saved rules that let a port in, from
// user.rules' and user6.rules' tuples, a rule's interface with them; nil
// where any rule lets it in from anywhere on any interface, none does, or
// ufw is inactive.
func TestOnlyFrom(t *testing.T) {
	state := func(active bool, v4, v6 string) ufw.State {
		r4, _ := ufw.ParseTuples([]byte(v4), ufw.IPv4)
		r6, _ := ufw.ParseTuples([]byte(v6), ufw.IPv6)
		return ufw.State{Installed: true, Status: ufw.Status{Known: true, Active: active}, Rules: append(r4, r6...)}
	}
	const (
		from24   = "### tuple ### allow tcp 22 0.0.0.0/0 any 192.0.2.0/24 in\n"
		fromHost = "### tuple ### limit any 22 0.0.0.0/0 any 198.51.100.7 in\n"
		anywhere = "### tuple ### allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in\n"
		other    = "### tuple ### allow tcp 2230 0.0.0.0/0 any 0.0.0.0/0 in\n"
		v6From   = "### tuple ### allow tcp 22 ::/0 any 2001:db8::/32 in\n"
		v6Any    = "### tuple ### allow tcp 22 ::/0 any ::/0 in\n"
		denyAny  = "### tuple ### deny tcp 22 0.0.0.0/0 any 0.0.0.0/0 in\n"
		rangeOut = "### tuple ### allow tcp 20:21,23 0.0.0.0/0 any 0.0.0.0/0 in\n"
		outbound = "### tuple ### allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 out\n"
		onEth0   = "### tuple ### allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in_eth0\n"
		from24On = "### tuple ### allow tcp 22 0.0.0.0/0 any 192.0.2.0/24 in_eth1\n"
		udpAny   = "### tuple ### allow udp 22 0.0.0.0/0 any 0.0.0.0/0 in\n"
	)
	for _, tc := range []struct {
		name string
		st   ufw.State
		want []string
	}{
		{"a source rule", state(true, other+from24, ""), []string{"192.0.2.0/24"}},
		{"two, in rule order", state(true, fromHost+from24+from24+rangeOut+outbound, ""), []string{"198.51.100.7", "192.0.2.0/24"}},
		{"beside an anywhere rule", state(true, from24+anywhere, ""), nil},
		{"v6's source too", state(true, from24, v6From), []string{"192.0.2.0/24", "2001:db8::/32"}},
		{"v6 from anywhere", state(true, from24, v6Any), nil},
		{"deny rules not weighed", state(true, denyAny+from24, ""), []string{"192.0.2.0/24"}},
		{"no rule for it", state(true, other+rangeOut, ""), nil},
		{"inactive", state(false, from24, ""), nil},
		{"from anywhere on an interface", state(true, onEth0, ""), []string{"anywhere on eth0"}},
		{"a source on an interface", state(true, from24On+from24, ""), []string{"192.0.2.0/24 on eth1", "192.0.2.0/24"}},
		{"22/udp from anywhere beside 22/tcp from a source", state(true, udpAny+from24, ""), []string{"192.0.2.0/24"}},
	} {
		if got := ufw.OnlyFrom(tc.st, 22); !slices.Equal(got, tc.want) {
			t.Errorf("%s: OnlyFrom = %q, want %q", tc.name, got, tc.want)
		}
	}
}
