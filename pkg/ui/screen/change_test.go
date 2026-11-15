package screen

import (
	"reflect"
	"testing"

	"github.com/abbott/hardn/pkg/ui/component"
)

// TestWantRow: what hardn.yml names; the screen's words where it names
// nothing; "Not valid" where it is not; and "differs" where the host's
// setting is another.
func TestWantRow(t *testing.T) {
	for _, tc := range []struct {
		name         string
		value        string
		bad, differs bool
		want         component.KeyValue
	}{
		{"none", "", false, false, component.KeyValue{Key: "hardn.yml", Value: "No server"}},
		{"not valid", "", true, false, component.KeyValue{Key: "hardn.yml", Value: "Not valid", State: component.ValueWarn}},
		{"named", "192.0.2.50:514 TCP", false, false, component.KeyValue{Key: "hardn.yml", Value: "192.0.2.50:514 TCP"}},
		{"differs", "192.0.2.50:514 TCP", false, true, component.KeyValue{Key: "hardn.yml", Value: "192.0.2.50:514 TCP", State: component.ValueWarn, Note: "differs", KeepNote: true}},
		{"nothing named differs from nothing", "", false, true, component.KeyValue{Key: "hardn.yml", Value: "No server"}},
	} {
		if got := wantRow(tc.value, "No server", tc.bad, tc.differs); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: wantRow = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestSetUndoItems: Set is 1 wherever it shows; the undo is 1 alone and 2
// beside it; and Set unavailable, with its reason, is the only item.
func TestSetUndoItems(t *testing.T) {
	set := component.MenuItem{Key: "1", Label: "Set servers", Description: "192.0.2.50:514 TCP"}
	undo := component.MenuItem{Label: "Stop forwarding", Description: "Undo hardn's change"}
	keyed := func(it component.MenuItem, key string) component.MenuItem { it.Key = key; return it }
	for _, tc := range []struct {
		name         string
		off          string
		own, differs bool
		want         []component.MenuItem
	}{
		{"nothing of hardn's", "", false, false, []component.MenuItem{set}},
		{"nothing of hardn's, whatever differs", "", false, true, []component.MenuItem{set}},
		{"hardn's, as hardn.yml", "", true, false, []component.MenuItem{keyed(undo, "1")}},
		{"hardn's, another", "", true, true, []component.MenuItem{set, keyed(undo, "2")}},
		{"unavailable", "Set outside hardn", false, false, []component.MenuItem{{Key: "1", Label: "Set servers", Description: "Set outside hardn", Disabled: true}}},
		{"unavailable, whatever is hardn's", "No time service", true, true, []component.MenuItem{{Key: "1", Label: "Set servers", Description: "No time service", Disabled: true}}},
	} {
		if got := setUndoItems(set, undo, tc.off, tc.own, tc.differs); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: setUndoItems = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
