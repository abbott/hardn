package screen

import (
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/component"
)

// The Kernel Settings view (kernelsettings.go): its rows, its items and its
// changes, by each row's verdict, hardn's file and whether it is applied at
// boot.

var (
	ksPass  = KernelRow{Label: "SYN cookies", Value: 1, Words: "on"}
	ksBelow = KernelRow{Label: "ICMP redirects", Value: 1, Words: "accepted", State: KernelBelow, SetsWords: "ignored"}
	ksTrace = KernelRow{Section: 1, Label: "Process tracing", Words: "unrestricted", State: KernelBelow, Sets: 1, SetsWords: "children only"}
	ksHeld  = KernelRow{Label: "IPv6 redirects", Value: 1, Words: "accepted", State: KernelHeld, By: "99-sysctl.conf"}
	ksLeft  = KernelRow{Label: "Sending redirects", Value: 1, Words: "on", State: KernelLeftAlone}
	ksFwd   = KernelRow{Label: "Forwarding", Value: 1, Words: "on", State: KernelNoVerdict, Routing: true}
	ksLinks = KernelRow{Section: 2, Label: "Hard links", Value: 1, Words: "protected"}
)

// TestKernelSections: a row is its value, what it means and, by its
// verdict, what hardn sets, which file holds it or why it is left alone; a
// container's rows have no verdict and a muted line; hardn's file has a
// section of its own, with a warning where nothing applies it at boot.
func TestKernelSections(t *testing.T) {
	row := func(st component.ValueState, key, value, note string) component.KeyValue {
		return component.KeyValue{Key: key, Value: value, Note: note, State: st, KeepNote: true}
	}
	k := KernelSettings{Rows: []KernelRow{ksPass, ksBelow, ksHeld, ksLeft, ksFwd,
		{Label: "Source routing", State: KernelAbsent}, {Label: "Kernel log", Section: 1, State: KernelUnread}, ksTrace, ksLinks}}
	got := k.Sections()
	want := [][]component.KeyValue{
		{row(component.ValueOK, "SYN cookies", "1", "on"), row(component.ValueWarn, "ICMP redirects", "1", "accepted, hardn sets 0"),
			row(component.ValueWarn, "IPv6 redirects", "1", "accepted, set in 99-sysctl.conf"), row(0, "Sending redirects", "1", "on, left alone: this host routes"),
			row(0, "Forwarding", "1", "on: this host routes"), {Key: "Source routing", Value: "Not Applicable"}},
		{{Key: "Kernel log", State: component.ValueUnknown}, row(component.ValueWarn, "Process tracing", "0", "unrestricted, hardn sets 1")},
		{row(component.ValueOK, "Hard links", "1", "protected")},
	}
	if len(got) != 3 || got[0].Header.Title != "Network" || got[1].Header.Title != "Kernel" || got[2].Header.Title != "Files" {
		t.Fatalf("sections: %+v", got)
	}
	for i := range want {
		if !slices.Equal(got[i].Rows.Items, want[i]) {
			t.Errorf("section %d: %+v", i, got[i].Rows.Items)
		}
	}
	k.Proxmox = true
	if note := k.Sections()[0].Rows.Items[3].Note; note != "on, left alone: Proxmox VE" {
		t.Errorf("Proxmox VE: %q", note)
	}

	inside := KernelSettings{Container: true, File: "f", Rows: []KernelRow{ksFwd, {Section: 2, Label: "Hard links", Value: 1, Words: "protected", State: KernelNoVerdict}}}
	got = inside.Sections()
	last := got[len(got)-1].Rows.Items
	if len(got) != 2 || got[0].Rows.Items[0].Note != "on" || last[0].State != 0 ||
		last[1] != (component.KeyValue{Key: "In a container: hardn sets none of these.", State: component.ValueNone}) {
		t.Errorf("container: %+v", got)
	}

	own := KernelSettings{Rows: []KernelRow{ksPass}, File: "/etc/sysctl.d/90-hardn.conf"}.Sections()
	if len(own) != 2 || own[1].Header.Title != "Set by hardn" || !slices.Equal(own[1].Rows.Items, []component.KeyValue{{Key: "File", Value: "/etc/sysctl.d/90-hardn.conf"}}) {
		t.Errorf("hardn's file: %+v", own)
	}
	off := KernelSettings{Rows: []KernelRow{ksPass}, File: "f", NotAtBoot: true, BootFix: "rc-update add sysctl boot"}.Sections()[1].Rows.Items
	if len(off) != 3 || off[1] != (component.KeyValue{Key: "At boot", Value: "Not set: sysctl service not enabled", State: component.ValueWarn}) ||
		off[2] != (component.KeyValue{Key: "To fix", Value: "rc-update add sysctl boot"}) {
		t.Errorf("not at boot: %+v", off)
	}
}

// TestKernelItems: Apply settings while a row is below the baseline, its
// description how many; Remove settings for hardn's file, item 2 beside
// Apply settings; and Apply settings unavailable with neither, and in a
// container.
func TestKernelItems(t *testing.T) {
	apply := func(desc string) component.MenuItem {
		return component.MenuItem{Key: "1", Label: "Apply settings", Description: desc}
	}
	off := func(desc string) component.MenuItem {
		return component.MenuItem{Key: "1", Label: "Apply settings", Description: desc, Disabled: true}
	}
	remove := func(key string) component.MenuItem {
		return component.MenuItem{Key: key, Label: "Remove settings", Description: "Undo hardn's change"}
	}
	tests := []struct {
		name string
		view KernelSettings
		want []component.MenuItem
	}{
		{"two below", KernelSettings{Rows: []KernelRow{ksPass, ksBelow, ksTrace, ksHeld, ksLeft}}, []component.MenuItem{apply("Set 2 settings")}},
		{"one below", KernelSettings{Rows: []KernelRow{ksBelow}}, []component.MenuItem{apply("Set 1 setting")}},
		{"at the baseline", KernelSettings{Rows: []KernelRow{ksPass}}, []component.MenuItem{off("Nothing to set")}},
		{"held alone", KernelSettings{Rows: []KernelRow{ksHeld}}, []component.MenuItem{off("Nothing to set")}},
		{"hardn's file", KernelSettings{Rows: []KernelRow{ksPass, ksHeld}, File: "f"}, []component.MenuItem{remove("1")}},
		{"hardn's file, and one below", KernelSettings{Rows: []KernelRow{ksBelow}, File: "f"}, []component.MenuItem{apply("Set 1 setting"), remove("2")}},
		{"not read", KernelSettings{Rows: []KernelRow{ksBelow}, File: "f", Unread: "Can't read the settings"}, []component.MenuItem{apply("Can't read the settings")}},
		{"container", KernelSettings{Container: true, Rows: []KernelRow{ksBelow}, File: "f"}, []component.MenuItem{off("Not in a container")}},
	}
	for _, tt := range tests {
		if got := tt.view.MenuItems(); !slices.Equal(got, tt.want) {
			t.Errorf("%s: items %+v", tt.name, got)
		}
	}
	items := KernelSettings{Rows: []KernelRow{ksBelow}, File: "f"}.MenuItems()
	if (KernelSettings{}).Removes(items[0]) || !(KernelSettings{}).Removes(items[1]) {
		t.Errorf("Removes over %+v", items)
	}
}

// TestKernelChanges: Apply settings' review is each row it sets, as the
// change leaves it; with none there is no change; Remove settings' review is
// each row at its earlier value.
func TestKernelChanges(t *testing.T) {
	if _, ok := (KernelSettings{Rows: []KernelRow{ksPass, ksHeld, ksLeft}}).ApplyChange(); ok {
		t.Error("a change with nothing to set")
	}
	ch, ok := KernelSettings{Rows: []KernelRow{ksPass, ksBelow, ksHeld, ksTrace}}.ApplyChange()
	want := []component.KeyValue{{Key: "ICMP redirects", Value: "0", Note: "ignored"}, {Key: "Process tracing", Value: "1", Note: "children only"}}
	if !ok || ch.Question != "Apply kernel settings?" || ch.Step.Title.Title != "Apply settings" || ch.Review.Title != "Settings" || !slices.Equal(ch.Review.Items, want) {
		t.Errorf("apply: %+v", ch)
	}
	rm := KernelSettings{Restores: []KernelRow{{Label: "ICMP redirects", Value: 1, Words: "accepted"}}}.RemoveChange()
	if rm.Question != "Remove hardn's kernel settings?" || rm.Step.Title.Title != "Remove settings" ||
		!slices.Equal(rm.Review.Items, []component.KeyValue{{Key: "ICMP redirects", Value: "1", Note: "accepted"}}) {
		t.Errorf("remove: %+v", rm)
	}
}

// TestKernelSubMenu: the screen is the row, the sections and the items over
// Return, and no line of it is wider than 80 columns.
func TestKernelSubMenu(t *testing.T) {
	long := KernelRow{Label: "Reverse-path filter", Value: 1, Words: "strict", State: KernelLeftAlone}
	vm := KernelSettings{Rows: []KernelRow{ksBelow, long, ksTrace, ksLinks}, File: "/etc/sysctl.d/90-hardn.conf", NotAtBoot: true, BootFix: "rc-update add sysctl boot"}.SubMenu()
	if got := sectionTitles(vm); !slices.Equal(got, []string{"Network", "Kernel", "Files", "Set by hardn"}) || vm.Title.Title != "Kernel Settings" || len(vm.Menu.Items) != 2 || vm.Menu.Exit.Label != "Return" {
		t.Errorf("screen: %v, %+v", got, vm.Menu)
	}
	out := strings.Join(drawn(vm.Render(plainAt(40, false))), "\n")
	for _, w := range []string{"[ ] ICMP redirects       1 accepted, hardn sets 0", " -  Reverse-path filter\n      1 strict, left alone: this host routes",
		"[ ] At boot              Not set: sysctl service not enabled", " -  To fix               rc-update add sysctl boot"} {
		if !strings.Contains(out, w) {
			t.Errorf("no %q in:\n%s", w, out)
		}
	}
}
