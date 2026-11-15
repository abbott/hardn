package screen

import (
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/component"
)

// TestConfigEdit: "Edit a setting" numbers the keys in their order, each
// with its value beside it, an empty one "Not set"; a key that reaches the
// value column keeps its value on its line, so that the menu does not
// stack; and no key that stays read-only is there.
func TestConfigEdit(t *testing.T) {
	values := map[string]string{}
	for _, k := range ConfigEditKeys() {
		values[k] = "false"
	}
	values["username"], values["sshPort"] = "", "2222"
	sub := ConfigEdit{Values: values}.SubMenu()
	out := strings.Join(drawn(sub.Render(plainAt(24, false))), "\n")
	for _, want := range []string{"# Edit a setting ###", "   1) username             Not set\n", "   3) sshPort              2222\n",
		"  12) enableUnattendedUpgrades  false\n", "   0) Return", "[0-12 or q]:"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if len(sub.Menu.Items) != len(ConfigEditKeys()) || len(strings.Split(out, "\n")) > 20 {
		t.Errorf("%d items, or a stacked menu:\n%s", len(sub.Menu.Items), out)
	}
	for _, k := range []string{"dryRun", "adminNoPassword", "sshConfigFile", "nameservers", "backupPath"} {
		if strings.Contains(out, k) {
			t.Errorf("%s in:\n%s", k, out)
		}
	}
}

// TestConfigList: a list key's screen: a row for each entry, None with
// none; Remove only where there is an entry; the note a warning row under
// the entries; a key's flow under the key's title.
func TestConfigList(t *testing.T) {
	sub := ConfigList{Key: "ntpServers"}.SubMenu()
	if rows := sub.Sections[0].Rows.Items; len(rows) != 1 || rows[0] != (component.KeyValue{Key: "Server", Value: "None"}) || len(sub.Menu.Items) != 1 || sub.Menu.Items[0].Label != "Add server" {
		t.Errorf("an empty list: %+v", sub)
	}
	sub = ConfigList{Key: "syslogServers", Entries: []string{"192.0.2.50:514 TCP", "192.0.2.51:514 UDP"}, Note: "busybox syslogd is UDP only."}.SubMenu()
	rows := sub.Sections[0].Rows.Items
	if len(rows) != 3 || rows[1].Value != "192.0.2.51:514 UDP" || rows[2] != (component.KeyValue{Key: "busybox syslogd is UDP only.", State: component.ValueWarn, Finding: true}) {
		t.Errorf("rows: %+v", rows)
	}
	if len(sub.Menu.Items) != 2 || sub.Menu.Items[1] != (component.MenuItem{Key: ConfigListRemove, Label: "Remove server"}) || sub.Title.Title != "syslogServers" {
		t.Errorf("items: %+v", sub)
	}
	if keys := (ConfigList{Key: "sshKeys", Entries: []string{"ssh-ed25519 george@laptop-1 SHA256:abc"}}).SubMenu(); keys.Sections[0].Header.Title != "Keys" || keys.Menu.Items[0].Label != "Add key" {
		t.Errorf("sshKeys: %+v", keys)
	}
	if step := ConfigStep("sshPort"); step.Title.Title != "sshPort" || len(step.Title.Parents) != 2 {
		t.Errorf("the step: %+v", step)
	}
}

// TestConfigNote: a key's note is a warning row under the key's own.
func TestConfigNote(t *testing.T) {
	v := Config{Path: "/etc/hardn/hardn.yml", By: ConfigBySearch, Values: configValues(), Notes: map[string]string{"syslogServers": "busybox syslogd is UDP only."}}
	for _, s := range v.SubMenu().Sections {
		if s.Header.Title != "Syslog" {
			continue
		}
		if rows := s.Rows.Items; len(rows) != 2 || rows[1] != (component.KeyValue{Key: "busybox syslogd is UDP only.", State: component.ValueWarn, Finding: true}) {
			t.Errorf("rows: %+v", rows)
		}
		return
	}
	t.Error("no Syslog section")
}
