package firewall

import (
	"slices"
	"testing"

	firewallctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// TestNoServerWords: on a host with no SSH server (Sockets.NoServer) item 1
// says nothing of SSH, Enable denying by default and an active ufw's policy
// set without letting SSH through, and the SSH section's Listeners row says
// there is no SSH server; with SSH, the words are as before.
func TestNoServerWords(t *testing.T) {
	noSSH := firewallctl.Sockets{NoServer: true}
	active := ufw.State{Status: ufw.Status{Active: true}}
	for _, tc := range []struct {
		name string
		r    firewallctl.Reading
		want screen.FirewallFirst
	}{
		{"inactive, no SSH server", firewallctl.Reading{First: firewallctl.ItemEnable, Sockets: noSSH}, screen.FirewallEnableNoSSH},
		{"active, not deny, no SSH server", firewallctl.Reading{First: firewallctl.ItemEnable, Sockets: noSSH, State: active,
			Does: firewallctl.Does{SetsDeny: true}}, screen.FirewallSetDenyNoSSH},
		{"inactive", firewallctl.Reading{First: firewallctl.ItemEnable}, screen.FirewallEnable},
		{"active, not deny", firewallctl.Reading{First: firewallctl.ItemEnable, State: active, Does: firewallctl.Does{SetsDeny: true}}, screen.FirewallSetDeny},
	} {
		if got := first(tc.r); got != tc.want {
			t.Errorf("%s: item 1 %d, want %d", tc.name, got, tc.want)
		}
	}
	want := []component.KeyValue{{Key: keyListeners, Value: ssh.DetailNoServer, State: component.ValueNotApplicable}}
	if got := sshRows(firewallctl.Reading{Sockets: noSSH}); !slices.Equal(got, want) {
		t.Errorf("SSH rows %+v, want %+v", got, want)
	}
}
