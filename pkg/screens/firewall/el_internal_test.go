package firewall

import (
	"errors"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	firewallctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/firewalld"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// TestMainViewEL: on Rocky Linux and AlmaLinux the screen is firewalld's
// (el-firewalld.md §1.6): its rows, item 1 by Reading.First, marked where
// Enable's plan is lockout-risk, Disable beside an Enable where firewalld
// starts at boot; never ufw's items, even over an installed ufw (EPEL's),
// whose fields the control doesn't read there.
func TestMainViewEL(t *testing.T) {
	rows := func(v screen.Firewalld) string {
		var out []string
		for _, kv := range v.Rows {
			out = append(out, kv.Key+" "+kv.Value)
		}
		return strings.Join(out, ", ")
	}
	lockout := []engine.Change{{Risk: engine.RiskLockout}}
	for _, tc := range []struct {
		name   string
		r      firewallctl.Reading
		first  screen.FirewalldFirst
		risk   bool
		beside bool
		rows   string
	}{
		{"missing", firewallctl.Reading{EL: true, First: firewallctl.ItemInstall,
			Firewalld: firewalld.State{Installed: detect.No, Running: detect.No, Enabled: detect.No, Unit: "not-found"}},
			screen.FirewalldInstall, false, false, "Installed No"},
		{"no ss", firewallctl.Reading{EL: true, First: firewallctl.ItemInstallSS,
			Firewalld: firewalld.State{Installed: detect.Yes, Running: detect.No, Enabled: detect.No, Unit: "disabled"}},
			screen.FirewalldInstallSS, false, false, "Installed Yes, Running No, At boot No, Default zone -, Active zones -"},
		{"stopped, disabled", firewallctl.Reading{EL: true, First: firewallctl.ItemEnable, Plan: lockout,
			Firewalld: firewalld.State{Installed: detect.Yes, Running: detect.No, Enabled: detect.No, Unit: "disabled"}},
			screen.FirewalldEnable, true, false, "Installed Yes, Running No, At boot No, Default zone -, Active zones -"},
		{"stopped, at boot", firewallctl.Reading{EL: true, First: firewallctl.ItemEnable, Plan: lockout, DisableBeside: true,
			Firewalld: firewalld.State{Installed: detect.Yes, Running: detect.No, Enabled: detect.Yes, Unit: "enabled"}},
			screen.FirewalldEnable, true, true, "Installed Yes, Running No, At boot Yes, Default zone -, Active zones -"},
		{"masked, refused", firewallctl.Reading{EL: true, First: firewallctl.ItemEnable, PlanErr: errors.New("masked"),
			Firewalld: firewalld.State{Installed: detect.Yes, Running: detect.No, Enabled: detect.No, Unit: "masked"}},
			screen.FirewalldEnable, false, false, "Installed Yes, Running No, At boot Masked, Default zone -, Active zones -"},
		{"running", firewallctl.Reading{EL: true, First: firewallctl.ItemDisable,
			Firewalld: firewalld.State{Installed: detect.Yes, Running: detect.Yes, Enabled: detect.Yes, Unit: "enabled", Default: "public", Active: []string{"public", "trusted"}}},
			screen.FirewalldDisable, false, false, "Installed Yes, Running Yes, At boot Yes, Default zone public, Active zones public, trusted"},
		{"running, no active zone", firewallctl.Reading{EL: true, First: firewallctl.ItemDisable,
			Firewalld: firewalld.State{Installed: detect.Yes, Running: detect.Yes, Enabled: detect.No, Unit: "disabled", Default: "public"}},
			screen.FirewalldDisable, false, false, "Installed Yes, Running Yes, At boot No, Default zone public, Active zones None"},
		{"unread", firewallctl.Reading{EL: true, First: firewallctl.ItemEnable, FirewalldErr: firewalld.ErrUnknown, PlanErr: firewalld.ErrUnknown},
			screen.FirewalldEnable, false, false, "Installed Can't read, Running Can't read, At boot Can't read, Default zone Can't read, Active zones Can't read"},
	} {
		tc.r.State.Installed = true
		v := firewalldView(tc.r, component.StatusList{})
		if v.First != tc.first || v.Risk != tc.risk || v.DisableBeside != tc.beside || rows(v) != tc.rows {
			t.Errorf("%s: first %d, risk %v, beside %v, rows %q; want %d, %v, %v, %q", tc.name, v.First, v.Risk, v.DisableBeside, rows(v), tc.first, tc.risk, tc.beside, tc.rows)
		}
		for _, it := range v.SubMenu().Menu.Items {
			if it.Key != screen.FirewalldFirstItem && it.Key != screen.FirewalldDisableItem {
				t.Errorf("%s: item %s %s", tc.name, it.Key, it.Label)
			}
		}
	}
}
