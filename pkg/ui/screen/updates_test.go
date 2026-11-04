package screen

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The Auto Updates screen as pkg/screens/updates fills it, in the shapes of
// the gallery's hosts (gallery-updates.md §3); the hosts behind each are
// that package's goldens. Synthetic values only.
var (
	updTestOn  = component.StatusList{Rows: []component.StatusRow{{Label: "Auto Updates", State: component.StatusConfigured, Detail: "unattended-upgrades"}}}
	updTestApt = []component.KeyValue{{Key: UpdatesPackageKey, Value: "Installed"}, {Key: UpdatesUpgradesKey, Value: "Daily", Note: "by 20auto-upgrades"},
		{Key: UpdatesListsKey, Value: "Daily", Note: "by 20auto-upgrades"}, {Key: UpdatesUpTimerKey, Value: "On", Note: "apt-daily-upgrade.timer"},
		{Key: UpdatesLsTimerKey, Value: "On", Note: "apt-daily.timer"}}
	updTestDebian13 = Updates{Status: updTestOn, Rows: append(updTestApt[:5:5], component.KeyValue{Key: UpdatesByKey, Value: "Not hardn"}), Item: UpdatesOutside}
	updTestEdited   = Updates{Status: component.StatusList{Rows: []component.StatusRow{{Label: "Auto Updates", State: component.StatusConfigured, Detail: "unattended-upgrades",
		Lines: []component.StatusLine{{Text: "changed since hardn", Warn: true}}}}},
		Rows: append(updTestApt[:5:5], component.KeyValue{Key: UpdatesByKey, Value: "hardn", Note: "record of 2026-10-20"}), Item: UpdatesDisable, Blocked: "52hardn-auto-upgrades changed"}
	updTestPVE = Updates{Status: component.StatusList{Rows: []component.StatusRow{{Label: "Auto Updates", State: component.StatusNotApplicable, Detail: "updated by hand"}}},
		Rows: append(updTestApt[:5:5], component.KeyValue{Key: UpdatesPVEKey, Value: "Updates by hand", Note: "apt full-upgrade"}), Item: UpdatesPVE, Install: true}
	updTestAlpine = Updates{Status: component.StatusList{Rows: []component.StatusRow{{Label: "Auto Updates", State: component.StatusNotConfigured, Detail: "no apk-cron"}}},
		Rows: []component.KeyValue{{Key: UpdatesApkCronKey, Value: "Not installed", State: component.ValueBad}, {Key: UpdatesCrondKey, Value: "Stopped", State: component.ValueBad, Note: "not at boot"}}, Apk: true}
	updTestNotRead = Updates{Status: component.StatusList{Rows: []component.StatusRow{{Label: "Auto Updates", State: component.StatusUnknown, Detail: "can't read",
		Lines: []component.StatusLine{{Text: "Counted as not configured"}}}}},
		Rows:    []component.KeyValue{{Key: UpdatesUpgradesKey, Value: "Can't read", State: component.ValueUnknown}, {Key: UpdatesListsKey, Value: "Can't read", State: component.ValueUnknown}},
		Blocked: "Can't read apt's settings"}
)

// TestUpdatesHeights (from the gallery's, gallery-updates.md §3): the screen
// at 80x24 and at 80x40, in Unicode and ASCII, is as many lines at either
// height, the prompt on its last, no line over 80 cells: every section in
// full at every height (Firewall's decision 9). pkg/screens/updates' holds
// every host's.
func TestUpdatesHeights(t *testing.T) {
	draws := map[string]struct {
		vm    SubMenu
		lines int
	}{
		"debian13": {updTestDebian13.SubMenu(), 22}, "edited": {updTestEdited.SubMenu(), 22}, "pve9": {updTestPVE.SubMenu(), 22},
		"alpine323": {updTestAlpine.SubMenu(), 17}, "not read": {updTestNotRead.SubMenu(), 18},
	}
	for name, d := range draws {
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
				env.Height = rows
				lines := strings.Split(ansi.Strip(d.vm.Render(component.NewCtx(env))), "\n")
				if len(lines) != d.lines || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s at 80x%d, unicode %v: %d lines, the last %q; want %d, the prompt", name, rows, unicode, len(lines), lines[len(lines)-1], d.lines)
				}
				for _, l := range lines {
					if w := ansi.StringWidth(l); w > 80 {
						t.Errorf("%s at 80x%d: %d cells: %q", name, rows, w, l)
					}
				}
			}
		}
	}
}

// TestUpdatesItem: the one item named by its result, Unavailable (P-22) on
// Proxmox VE and where on but not by hardn, its description what is in the
// way where it is offered (R1).
func TestUpdatesItem(t *testing.T) {
	for _, tt := range []struct {
		name string
		u    Updates
		want string
	}{
		{"off", Updates{}, "1 Enable automatic updates: Daily, by unattended-upgrades"},
		{"no package", Updates{Install: true}, "1 Enable automatic updates: Install unattended-upgrades"},
		{"alpine", Updates{Apk: true}, "1 Enable automatic updates: Daily, by apk-cron"},
		{"by hardn", Updates{Item: UpdatesDisable}, "1 Disable automatic updates: Undo hardn's changes"},
		{"outside", updTestDebian13, "1 Disable automatic updates: Turned on outside hardn, unavailable"},
		{"outside, blocked", Updates{Item: UpdatesOutside, Blocked: "Can't read"}, "1 Disable automatic updates: Turned on outside hardn, unavailable"},
		{"pve9", updTestPVE, "1 Enable automatic updates: Not on Proxmox VE, unavailable"},
		{"edited", updTestEdited, "1 Disable automatic updates: 52hardn-auto-upgrades changed"},
		{"not read", updTestNotRead, "1 Enable automatic updates: Can't read apt's settings"},
	} {
		it := tt.u.MenuItem()
		got := it.Key + " " + it.Label + ": " + it.Description
		if it.Disabled {
			got += ", unavailable"
		}
		if got != tt.want {
			t.Errorf("%s: %s, want %s", tt.name, got, tt.want)
		}
	}
}

// TestUpdatesChange: each direction's title bar, question and review, the
// control's notes after the row as the change leaves it.
func TestUpdatesChange(t *testing.T) {
	notes := []component.KeyValue{{Key: "python3-dbus", Value: "Kept", Note: "apt autoremove removes it"}}
	for _, tt := range []struct {
		u                     Updates
		title, question, item string
	}{
		{Updates{}, "Enable automatic updates", "Turn automatic updates on?", "Auto Updates On"},
		{Updates{Item: UpdatesDisable}, "Disable automatic updates", "Turn automatic updates off?", "Auto Updates Off"},
	} {
		c := tt.u.Change(notes)
		r := c.Review
		if c.Step.Title.Title != tt.title || strings.Join(c.Step.Title.Parents, ",") != "Auto Updates" || c.Question != tt.question ||
			r.Title != "Update Configuration" || len(r.Items) != 2 || r.Items[0].Key+" "+r.Items[0].Value != tt.item || r.Items[1] != notes[0] {
			t.Errorf("%s: %+v", tt.title, c)
		}
	}
}
