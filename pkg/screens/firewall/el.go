package firewall

import (
	"context"
	"fmt"
	"strings"

	firewallctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/firewalld"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The screen on Rocky Linux and AlmaLinux: firewalld's section and items
// (docs/engine/el-firewalld.md §1.6). The rows' words are proposed.
const (
	cantRead = "Can't read"
	noZone   = "-"
)

// firewalldView is r as the screen on Rocky Linux and AlmaLinux draws it,
// under status: firewalld's rows; item 1 by Reading.First, marked where its
// plan is lockout-risk; Disable beside an Enable where firewalld starts at
// boot.
func firewalldView(r firewallctl.Reading, status component.StatusList) screen.Firewalld {
	return screen.Firewalld{Status: status, Rows: firewalldRows(r), First: firewalldFirst(r.First), Risk: risky(r.Plan), DisableBeside: r.DisableBeside}
}

// firewalldFirst is item 1's kind from the control's.
func firewalldFirst(first firewallctl.Item) screen.FirewalldFirst {
	switch first {
	case firewallctl.ItemInstall:
		return screen.FirewalldInstall
	case firewallctl.ItemInstallSS:
		return screen.FirewalldInstallSS
	case firewallctl.ItemEnable:
		return screen.FirewalldEnable
	case firewallctl.ItemDisable:
		return screen.FirewalldDisable
	}
	return screen.FirewalldNoFirst
}

// firewalldRows are the section firewalld's rows: not installed, that alone,
// as ufw's section says it; else Installed, Running, At boot, and the default
// and active zones, read only while it runs. What a failed read left unread
// says so.
func firewalldRows(r firewallctl.Reading) []component.KeyValue {
	st := r.Firewalld
	if r.FirewalldErr != nil && st.Running == detect.Yes {
		st.Running = detect.Unknown // a read that failed after is-active: the zones aren't read
	}
	if st.Installed == detect.No {
		return []component.KeyValue{{Key: screen.FirewalldInstalledKey, Value: "No", State: component.ValueWarn}}
	}
	def := component.KeyValue{Key: screen.FirewalldDefaultKey, Value: noZone}
	active := component.KeyValue{Key: screen.FirewalldActiveKey, Value: noZone}
	switch {
	case st.Running == detect.No:
	case st.Running != detect.Yes || st.Default == "":
		def.Value, def.State = cantRead, component.ValueUnknown
		active.Value, active.State = cantRead, component.ValueUnknown
	default:
		def.Value, active.Value = st.Default, "None"
		if len(st.Active) > 0 {
			active.Value = strings.Join(st.Active, ", ")
		}
	}
	return []component.KeyValue{triRow(screen.FirewalldInstalledKey, st.Installed), triRow(screen.FirewalldRunningKey, st.Running), bootRowOf(st), def, active}
}

// triRow is a yes-or-no row: Yes, No, or not read.
func triRow(key string, t detect.Tri) component.KeyValue {
	switch t {
	case detect.Yes:
		return component.KeyValue{Key: key, Value: "Yes", State: component.ValueOK}
	case detect.No:
		return component.KeyValue{Key: key, Value: "No", State: component.ValueWarn}
	}
	return component.KeyValue{Key: key, Value: cantRead, State: component.ValueUnknown}
}

// bootRowOf is At boot from systemctl is-enabled's word: Yes for enabled,
// No for disabled, Masked, No service, not read; any other word that may
// start it (static, indirect, ...) as it is.
func bootRowOf(st firewalld.State) component.KeyValue {
	row := component.KeyValue{Key: screen.FirewallBootKey}
	switch {
	case st.Unit == "enabled":
		row.Value, row.State = "Yes", component.ValueOK
	case st.Unit == "disabled":
		row.Value, row.State = "No", component.ValueWarn
	case st.Unit == "masked" || st.Unit == "masked-runtime":
		row.Value, row.State = "Masked", component.ValueWarn
	case st.Unit == "not-found":
		row.Value, row.State = "No service", component.ValueWarn
	case st.Enabled == detect.Unknown:
		row.Value, row.State = cantRead, component.ValueUnknown
	default:
		row.Value = st.Unit
	}
	return row
}

// elAction returns what choosing the item keyed key does on the screen on
// Rocky Linux and AlmaLinux: item 1 says its plan's refusal before asking,
// or runs its change, an Enable lockout-risk, the hostname checked first;
// Install, Install iproute and Disable, which the reading doesn't plan,
// planned first (planned); item 2 is Disable, likewise.
func (r *runner) elAction(ctx context.Context, h reading, key string) func() error {
	switch key {
	case screen.FirewalldFirstItem:
		if h.PlanErr != nil {
			return r.s.Say(screenkit.Warnings(screenkit.RefusalWords(h.PlanErr)))
		}
		if h.First != firewallctl.ItemEnable {
			return r.planned(ctx, firstAction(h.First), h.el.FirstChange())
		}
		return r.plain(ctx, firstAction(h.First), h.el.FirstChange(), risky(h.Plan))
	case screen.FirewalldDisableItem:
		return r.planned(ctx, firewallctl.ActionDisable, screen.FirewalldDisableChange(false))
	}
	return func() error { return fmt.Errorf("firewall screen: item %s has no action", key) } // not reached: SubMenu's items are those above
}
