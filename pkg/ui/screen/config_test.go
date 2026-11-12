package screen

import (
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/component"
)

// configValues are every key at a default, but username, which the file sets.
func configValues() map[string]ConfigValue {
	v := map[string]ConfigValue{}
	for _, k := range ConfigKeys() {
		v[k] = ConfigValue{Text: "false"}
	}
	v["username"] = ConfigValue{Text: "george", Set: true}
	v["sshConfigFile"] = ConfigValue{}
	return v
}

// TestConfigFile: the File section by how the file was looked for: found, none
// anywhere, and one that -f or HARDN_CONFIG names and is not there.
func TestConfigFile(t *testing.T) {
	defaults := component.KeyValue{Key: "hardn is using its defaults.", State: component.ValueNone}
	for _, tt := range []struct {
		name string
		c    Config
		rows []component.KeyValue
	}{
		{"search", Config{Path: "/etc/hardn/hardn.yml", By: ConfigBySearch},
			[]component.KeyValue{{Key: "Path", Value: "/etc/hardn/hardn.yml"}, {Key: "From", Value: "Search path"}}},
		{"flag", Config{Path: "/root/site.yml", By: ConfigByFlag},
			[]component.KeyValue{{Key: "Path", Value: "/root/site.yml"}, {Key: "From", Value: "-f"}}},
		{"none", Config{},
			[]component.KeyValue{{Key: "Path", Value: "None found"}, defaults}},
		{"missing", Config{Path: "/home/george/hardn.yml", By: ConfigByEnv, Missing: true},
			[]component.KeyValue{{Key: "Path", Value: "/home/george/hardn.yml", State: component.ValueBad, Note: "not found", KeepNote: true}, {Key: "From", Value: "HARDN_CONFIG"}, defaults}},
	} {
		sub := tt.c.SubMenu()
		if got := sub.Sections[0]; got.Header.Title != "File" || !slices.Equal(got.Rows.Items, tt.rows) {
			t.Errorf("%s: %q, rows %+v", tt.name, got.Header.Title, got.Rows.Items)
		}
		if sub.Title.Title != "Configuration" || len(sub.Menu.Items) != 0 || sub.Menu.Exit.Label != "Return" || sub.Prompt != "Option" {
			t.Errorf("%s: %+v", tt.name, sub)
		}
	}
}

// TestConfigRows: a row for each key, under its group, in order; a value the file
// does not set carries "default", an empty one says "Not set"; "Not used" shows
// only where there is a key for it, each a warning.
func TestConfigRows(t *testing.T) {
	c := Config{Path: "/etc/hardn/hardn.yml", By: ConfigBySearch, Values: configValues()}
	out := strings.Join(drawn(c.SubMenu().Render(plainAt(24, false))), "\n")
	at := 0
	for _, want := range append([]string{"File", "Users", "username", "SSH", "Firewall", "DNS", "Auto Updates", "Backup", "General"}, "0) Return") {
		i := strings.Index(out[at:], want)
		if i < 0 {
			t.Fatalf("no %q after offset %d in:\n%s", want, at, out)
		}
		at += i
	}
	for _, k := range ConfigKeys() {
		if !strings.Contains(out, k) {
			t.Errorf("no row for %s", k)
		}
	}
	users := c.SubMenu().Sections[1].Rows.Items
	if want := (component.KeyValue{Key: "username", Value: "george"}); users[0] != want {
		t.Errorf("a key the file sets: %+v", users[0])
	}
	if want := (component.KeyValue{Key: "adminNoPassword", Value: "false", Note: "(default)", KeepNote: true}); users[1] != want {
		t.Errorf("a default: %+v", users[1])
	}
	if !strings.Contains(out, "Not set (default)") || strings.Contains(out, "Not used") {
		t.Errorf("an empty value, or Not used with no key:\n%s", out)
	}
	// A default too wide to sit beside its key keeps its note, under the key.
	long := c
	long.Values = configValues()
	long.Values["sshConfigFile"] = ConfigValue{Text: "/etc/ssh/sshd_config.d/hardn.conf"}
	if out := strings.Join(drawn(long.SubMenu().Render(plainAt(24, false))), "\n"); !strings.Contains(out, "   -  sshConfigFile\n      /etc/ssh/sshd_config.d/hardn.conf (default)\n") {
		t.Errorf("a long default lost its note:\n%s", out)
	}
	c.Unused = []ConfigUnused{{Key: "enableLynis", Value: "true"}, {Key: "ufwAllowedPorts"}}
	sections := c.SubMenu().Sections
	want := []component.KeyValue{
		{Key: "enableLynis", Value: "true", State: component.ValueWarn},
		{Key: "ufwAllowedPorts", State: component.ValueWarn},
	}
	if last := sections[len(sections)-1]; last.Header.Title != "Not used" || !slices.Equal(last.Rows.Items, want) {
		t.Errorf("Not used: %+v", last)
	}
}

// TestConfigReport: the report is the screen less its menu and prompt: the title
// bar and the same sections, drawn the same, "Not used" left out with no key for
// it; off a terminal a long value stays whole.
func TestConfigReport(t *testing.T) {
	v := Config{Path: "/etc/hardn/hardn.yml", By: ConfigBySearch, Values: configValues(), Unused: []ConfigUnused{{Key: "enableLynis", Value: "true"}}}
	for _, unicode := range []bool{true, false} {
		c := plainAt(24, unicode)
		report, drawing := v.Report(c), v.SubMenu().Render(c)
		if !strings.HasPrefix(drawing, report+"\n") {
			t.Errorf("unicode %t: the screen does not open with the report:\n%s\n--\n%s", unicode, report, drawing)
		}
		if strings.Contains(report, "Return") || strings.Contains(report, "Option") || !strings.Contains(report, "Not used") {
			t.Errorf("unicode %t: a menu, a prompt or no Not used:\n%s", unicode, report)
		}
	}
	v.Unused = nil
	long := "/" + strings.Repeat("a", 90)
	v.Values["backupPath"] = ConfigValue{Text: long, Set: true}
	if s := v.Report(plainAt(24, false)); strings.Contains(s, "Not used") || strings.Contains(s, long) {
		t.Errorf("on a terminal:\n%s", s)
	}
	if s := v.Report(component.OffTerminal()); !strings.Contains(s, long) {
		t.Errorf("off a terminal:\n%s", s)
	}
}
