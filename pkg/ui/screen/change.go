package screen

import (
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// Change is one change's words: the title bar over its flow, its question,
// and its review. The Firewall, Syslog Settings and NTP Settings screens' items
// each return one.
type Change struct {
	Step     WorkflowStep
	Question string
	Review   flow.Review
}

// What the screens that set hardn.yml's value on a service share (Syslog
// Settings, NTP Settings; docs/engine/gallery-timesync.md §5): the hardn.yml
// row and the two-item menu. Whose setting the host's is, and what makes
// Set unavailable, are each screen's own.
const (
	wantKey     = "hardn.yml"
	wantBad     = "Not valid"
	wantDiffers = "differs"

	// The items' keys: Set is 1 wherever it shows, and the undo is 1 alone
	// and 2 beside it.
	setUndoFirstKey  = "1"
	setUndoSecondKey = "2"
)

// wantRow is the hardn.yml row: value, what hardn.yml names; none ("No
// server") where it names nothing; "Not valid", a warning, where bad; and a
// warning noted "differs" where the host's setting is another.
func wantRow(value, none string, bad, differs bool) component.KeyValue {
	want := component.KeyValue{Key: wantKey, Value: none}
	if bad {
		want.Value, want.State = wantBad, component.ValueWarn
	}
	if value != "" {
		want.Value = value
		if differs {
			want.State, want.Note, want.KeepNote = component.ValueWarn, wantDiffers, true
		}
	}
	return want
}

// setUndoItems are a screen's one or two items, named by their result: set
// shown as unavailable with off as its description, where off says why;
// set alone while nothing on the host is hardn's own; set as 1 and undo as
// 2 while hardn's own setting differs from hardn.yml's; else undo alone,
// as 1.
func setUndoItems(set, undo component.MenuItem, off string, own, differs bool) []component.MenuItem {
	set.Key, undo.Key = setUndoFirstKey, setUndoFirstKey
	switch {
	case off != "":
		return []component.MenuItem{{Key: setUndoFirstKey, Label: set.Label, Description: off, Disabled: true}}
	case !own:
		return []component.MenuItem{set}
	case differs:
		undo.Key = setUndoSecondKey
		return []component.MenuItem{set, undo}
	}
	return []component.MenuItem{undo}
}
