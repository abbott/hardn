package screen

import (
	"strings"
	"testing"
)

// TestConfigGroup: the Configuration group is its title bar, the three items in
// order with their words, Return and "Option", with no heading, status or section;
// 9 lines at every height, the prompt last; and Screen draws what Render renders.
func TestConfigGroup(t *testing.T) {
	sub := ConfigGroup{}.SubMenu()
	if sub.Title.Title != "Configuration" || len(sub.Title.Parents) != 0 || sub.Heading != "" || len(sub.Sections) != 0 ||
		len(sub.Status.Rows) != 0 || sub.Alert != nil || sub.Menu.Exit.Label != "Return" || sub.Prompt != "Option" {
		t.Errorf("the group: %+v", sub)
	}
	const want = `# Configuration ################################################

   1) hardn.yml            View the file hardn reads
   2) Log Forwarding       Send logs to a syslog server
   3) Time Sync            Keep the clock in sync

   0) Return

    > Option [0-3 or q]:`
	for _, h := range []int{24, 40} {
		out := strings.Join(drawn(sub.Render(plainAt(h, false))), "\n")
		if strings.TrimRight(out, " ") != want || lines(out) != 9 {
			t.Errorf("at %d rows, %d lines:\n%s\nwant:\n%s", h, lines(out), out, want)
		}
	}
	for i, key := range []string{ConfigFileItem, ConfigLogForwardItem, ConfigTimeSyncItem} {
		if sub.Menu.Items[i].Key != key {
			t.Errorf("item %d has the key %q, want %q", i+1, sub.Menu.Items[i].Key, key)
		}
	}
}
