package firewall_test

import (
	"slices"
	"strings"
	"testing"

	firewallctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	fwscreen "github.com/abbott/hardn/pkg/screens/firewall"
	"github.com/abbott/hardn/pkg/ui/component"
)

// TestPortRows: the Ports section's rows from the control's reading
// (docs/engine/screen-ports.md): each standing in its word, a port let in
// with nothing listening the one warning, one not established "Unknown"; no
// ss, an ss that failed, a line not read and nothing listening each in its
// words; and a program's name, the host's word, made safe.
func TestPortRows(t *testing.T) {
	unknown := func(value string) component.KeyValue {
		return component.KeyValue{Key: "Listening ports", Value: value, State: component.ValueUnknown}
	}
	for _, tt := range []struct {
		name string
		in   firewallctl.Ports
		want []component.KeyValue
	}{
		{"each standing", firewallctl.Ports{List: []firewallctl.Port{
			{Spec: "22", Proto: "tcp", Process: "sshd", Reach: firewallctl.ReachOpen}, {Spec: "9090", Proto: "tcp", Process: "prometheus", Reach: firewallctl.ReachSome},
			{Spec: "9100", Proto: "tcp", Reach: firewallctl.ReachBlocked}, {Spec: "5432", Proto: "tcp", Process: "postgres", Reach: firewallctl.ReachLocal},
			{Spec: "6000:6007", Proto: "tcp", Reach: firewallctl.ReachUnused}, {Spec: "53", Reach: firewallctl.ReachUnused},
			{Spec: "80", Proto: "tcp", Process: "nginx", Reach: firewallctl.ReachUnfiltered}, {Spec: "8000", Proto: "tcp", Process: "docker-proxy", Reach: firewallctl.ReachUnknown}}},
			[]component.KeyValue{{Key: "22/tcp", Value: "Open", Extra: "sshd"}, {Key: "9090/tcp", Value: "Some sources", Extra: "prometheus"},
				{Key: "9100/tcp", Value: "Blocked"}, {Key: "5432/tcp", Value: "Local only", Extra: "postgres"},
				{Key: "6000:6007/tcp", Value: "Open", Note: "nothing listening", State: component.ValueWarn}, {Key: "53", Value: "Open", Note: "nothing listening", State: component.ValueWarn},
				{Key: "80/tcp", Value: "Not filtered", Extra: "nginx"}, {Key: "8000/tcp", Extra: "docker-proxy", State: component.ValueUnknown}}},
		{"no ss", firewallctl.Ports{NoSS: true}, []component.KeyValue{unknown("ss not installed")}},
		{"ss failed", firewallctl.Ports{Failed: true}, []component.KeyValue{unknown("ss failed")}},
		{"a line not read", firewallctl.Ports{Partial: true, List: []firewallctl.Port{{Spec: "22", Proto: "tcp", Reach: firewallctl.ReachOpen}}},
			[]component.KeyValue{{Key: "22/tcp", Value: "Open"}, unknown("Some not read")}},
		{"only lines not read", firewallctl.Ports{Partial: true}, []component.KeyValue{unknown("Some not read")}},
		{"nothing listening", firewallctl.Ports{}, []component.KeyValue{{Key: "Nothing is listening", State: component.ValueNone}}},
	} {
		if got := fwscreen.PortRows(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %+v, want %+v", tt.name, got, tt.want)
		}
	}
	got := fwscreen.PortRows(firewallctl.Ports{List: []firewallctl.Port{{Spec: "8000", Proto: "tcp", Process: "\x1b[2J\x1b]0;x\x07ev\nil", Reach: firewallctl.ReachOpen}}})
	if len(got) != 1 || got[0].Extra == "" || strings.ContainsAny(got[0].Extra, "\x1b\x07\n") {
		t.Errorf("a hostile name: %+v", got)
	}
}
