package screen

import (
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/component"
)

// TestFirewalld: firewalld's screen (Rocky Linux and AlmaLinux): its title,
// its section, item 1 by what it does, marked where lockout-risk, Disable
// beside it as 2, never ufw's items; each change's title, question and
// review.
func TestFirewalld(t *testing.T) {
	items := func(vm SubMenu) string {
		var out []string
		for _, it := range vm.Menu.Items {
			out = append(out, it.Key+" "+it.Label)
		}
		return strings.Join(out, ", ")
	}
	rows := []component.KeyValue{{Key: FirewalldInstalledKey, Value: "Yes", State: component.ValueOK}}
	for _, tt := range []struct {
		name string
		vm   Firewalld
		want string
	}{
		{"missing", Firewalld{First: FirewalldInstall}, "1 Install firewalld"},
		{"no ss", Firewalld{First: FirewalldInstallSS}, "1 Install iproute"},
		{"stopped", Firewalld{First: FirewalldEnable, Risk: true}, "1 Enable firewalld"},
		{"stopped, at boot", Firewalld{First: FirewalldEnable, Risk: true, DisableBeside: true}, "1 Enable firewalld, 2 Disable firewalld"},
		{"running", Firewalld{First: FirewalldDisable}, "1 Disable firewalld"},
		{"none", Firewalld{}, ""},
	} {
		tt.vm.Status, tt.vm.Rows = fwTestStatus, rows
		vm := tt.vm.SubMenu()
		if got := items(vm); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
		if vm.Title.Title != firewalldTitle || len(vm.Sections) != 1 || vm.Sections[0].Header.Title != firewalldSection {
			t.Errorf("%s: title %q, sections %+v", tt.name, vm.Title.Title, vm.Sections)
		}
		if len(vm.Menu.Items) > 0 && vm.Menu.Items[0].Risk != tt.vm.Risk {
			t.Errorf("%s: item 1 marked %v, want %v", tt.name, vm.Menu.Items[0].Risk, tt.vm.Risk)
		}
	}
	for first, want := range map[FirewalldFirst][3]string{
		FirewalldInstall:   {"Install firewalld", "Install firewalld?", "Package firewalld, Status Not running, At boot No"},
		FirewalldInstallSS: {"Install iproute", "Install iproute?", "Package iproute"},
		FirewalldEnable:    {"Enable firewalld", "Enable firewalld?", "Status Running, At boot Yes, SSH Let through"},
		FirewalldDisable:   {"Disable firewalld", "Disable firewalld?", "Status Not running, At boot No"},
	} {
		c := Firewalld{First: first}.FirstChange()
		var review []string
		for _, kv := range c.Review.Items {
			review = append(review, kv.Key+" "+kv.Value)
		}
		if c.Step.Title.Title != want[0] || c.Question != want[1] || strings.Join(review, ", ") != want[2] {
			t.Errorf("%d: %q, %q, %q; want %q", first, c.Step.Title.Title, c.Question, review, want)
		}
	}
	if c := FirewalldDisableChange(false); c.Review.Notice == nil || c.Review.Notice.Callout.Body[0] != firewalldBootOffWarning || len(c.Review.Items) != 1 || c.Review.Items[0].Key != FirewallBootKey {
		t.Errorf("Disable at boot alone: %+v", c.Review)
	}
	if c := FirewalldDisableChange(true); c.Review.Notice == nil || c.Review.Notice.Callout.Body[0] != firewalldDisableWarning || len(c.Review.Items) != 2 {
		t.Errorf("Disable's warning: %+v", c.Review)
	}
	if c := (Firewalld{}).FirstChange(); c.Question != "" {
		t.Errorf("no item has a flow: %+v", c)
	}
}
