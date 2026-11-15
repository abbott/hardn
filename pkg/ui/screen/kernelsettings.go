package screen

import (
	"fmt"
	"strconv"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// The Kernel Settings screen (docs/roadmap.md, "sysctl";
// docs/engine/gallery-sysctl.md, adopted 2026-11-14,
// built in its §9): main menu 8, then 4. One row a setting,
// in plain words: its value, what the value means and, below hardn's
// baseline, what hardn sets or which file holds it. A row stands for its
// keys, every interface's among them, and shows the weakest. One or two
// items named by their result. On the sub-menu layout: there is no Figma
// frame. pkg/screens/kernelsettings fills KernelSettings from what the
// control reads.

// The screen's words (gallery-sysctl.md, "Words").
const (
	kernelTitle = "Kernel Settings"

	kernelOwnSection = "Set by hardn"
	kernelFileKey    = "File"
	kernelBootKey    = "At boot"
	kernelFixKey     = "To fix"
	kernelNotAtBoot  = "Not set: sysctl service not enabled"

	kernelSets      = ", hardn sets "
	kernelSetIn     = ", set in "
	kernelLeftAlone = ", left alone: "
	kernelRoutes    = "this host routes"
	kernelProxmox   = "Proxmox VE"
	kernelAbsent    = "Not Applicable"

	kernelApply       = "Apply settings"
	kernelRemove      = "Remove settings"
	kernelDescRemove  = "Undo hardn's change"
	kernelDescHost    = "Not in a container"
	kernelDescNothing = "Nothing to set"

	kernelContainerNote = "In a container: hardn sets none of these."

	kernelReview    = "Settings"
	kernelAskApply  = "Apply kernel settings?"
	kernelAskRemove = "Remove hardn's kernel settings?"
)

// kernelSections are the sections' titles, by KernelRow.Section.
var kernelSections = []string{"Network", "Kernel", "Files"}

// The items' keys: Apply settings is 1 wherever it shows, and Remove
// settings is 1 alone and 2 beside it.
const (
	KernelFirstKey  = setUndoFirstKey
	KernelSecondKey = setUndoSecondKey
)

// KernelState is a row's verdict.
type KernelState uint8

const (
	// KernelPass: at the baseline.
	KernelPass KernelState = iota
	// KernelBelow: below it; Apply settings sets it.
	KernelBelow
	// KernelHeld: below it, and a file read after hardn's sets it so.
	KernelHeld
	// KernelLeftAlone: left as it is on a host that routes, and on Proxmox
	// VE.
	KernelLeftAlone
	// KernelNoVerdict: shown only.
	KernelNoVerdict
	// KernelAbsent: the kernel has no such setting.
	KernelAbsent
	// KernelUnread: not read.
	KernelUnread
)

// KernelRow is one setting.
type KernelRow struct {
	// Section is 0 for Network, 1 for Kernel, 2 for Files.
	Section int
	// Label is the setting in plain words.
	Label string
	// Value is the weakest of its keys' values, and Words what it means.
	Value int
	Words string
	State KernelState
	// Sets is the value hardn sets, and SetsWords what it means: for
	// KernelBelow.
	Sets      int
	SetsWords string
	// By is the file that holds the key, by its base name: for KernelHeld.
	By string
	// Routing: this is the Forwarding row, and it reads on.
	Routing bool
}

// KernelSettings is the Kernel Settings screen's reading of the host, which
// its caller fills.
type KernelSettings struct {
	// Status is the Kernel row.
	Status component.StatusList
	// Rows are the settings, in order.
	Rows []KernelRow
	// Container: the host is a container, whose kernel is its host's.
	Container bool
	// Proxmox: the rows left alone are left because this is a Proxmox VE
	// node; else because the host routes.
	Proxmox bool
	// File is hardn's file; "" with none.
	File string
	// NotAtBoot: hardn's file is there and nothing applies it at boot.
	// BootFix is the command that would.
	NotAtBoot bool
	BootFix   string
	// Unread is why the settings cannot be read, in a few words; "" where
	// they are.
	Unread string
	// Restores are the rows Remove settings sets back, each with its
	// earlier value.
	Restores []KernelRow
}

// todo are the rows Apply settings sets.
func (k KernelSettings) todo() []KernelRow {
	var out []KernelRow
	for _, r := range k.Rows {
		if r.State == KernelBelow {
			out = append(out, r)
		}
	}
	return out
}

// row is r as the screen draws it.
func (k KernelSettings) row(r KernelRow) component.KeyValue {
	kv := component.KeyValue{Key: r.Label, Value: strconv.Itoa(r.Value), Note: r.Words, KeepNote: true}
	switch r.State {
	case KernelPass:
		kv.State = component.ValueOK
	case KernelBelow:
		kv.State, kv.Note = component.ValueWarn, kv.Note+kernelSets+strconv.Itoa(r.Sets)
	case KernelHeld:
		kv.State, kv.Note = component.ValueWarn, kv.Note+kernelSetIn+r.By
	case KernelLeftAlone:
		why := kernelRoutes
		if k.Proxmox {
			why = kernelProxmox
		}
		kv.Note += kernelLeftAlone + why
	case KernelNoVerdict:
		if r.Routing && !k.Container {
			kv.Note += ": " + kernelRoutes
		}
	case KernelAbsent:
		kv = component.KeyValue{Key: r.Label, Value: kernelAbsent}
	case KernelUnread:
		kv = component.KeyValue{Key: r.Label, State: component.ValueUnknown}
	}
	return kv
}

// Sections are the screen's sections: Network, Kernel and Files, a row a
// setting; in a container a muted line under the last; and "Set by hardn"
// with hardn's file, and a warning where nothing applies it at boot.
func (k KernelSettings) Sections() []Section {
	var out []Section
	for _, r := range k.Rows {
		title := kernelSections[r.Section]
		if n := len(out); n == 0 || out[n-1].Header.Title != title {
			out = append(out, Section{Header: component.SectionHeader{Title: title}})
		}
		s := &out[len(out)-1]
		s.Rows.Items = append(s.Rows.Items, k.row(r))
	}
	switch {
	case len(out) == 0:
	case k.Container:
		s := &out[len(out)-1]
		s.Rows.Items = append(s.Rows.Items, component.KeyValue{Key: kernelContainerNote, State: component.ValueNone})
	case k.File != "":
		rows := []component.KeyValue{{Key: kernelFileKey, Value: k.File}}
		if k.NotAtBoot {
			rows = append(rows, component.KeyValue{Key: kernelBootKey, Value: kernelNotAtBoot, State: component.ValueWarn},
				component.KeyValue{Key: kernelFixKey, Value: k.BootFix})
		}
		out = append(out, Section{Header: component.SectionHeader{Title: kernelOwnSection}, Rows: component.BulletList{Items: rows}})
	}
	return out
}

// kernelCount is "Set 4 settings", "Set 1 setting".
func kernelCount(n int) string {
	if n == 1 {
		return "Set 1 setting"
	}
	return fmt.Sprintf("Set %d settings", n)
}

// MenuItems are the screen's items, named by their result: Apply settings
// while a row is below the baseline, Remove settings while hardn's file is
// there, and Apply settings shown as unavailable where there is neither,
// and in a container. Where the settings are not read, Apply settings
// alone says so.
func (k KernelSettings) MenuItems() []component.MenuItem {
	apply := component.MenuItem{Key: KernelFirstKey, Label: kernelApply, Description: kernelCount(len(k.todo()))}
	remove := component.MenuItem{Key: KernelFirstKey, Label: kernelRemove, Description: kernelDescRemove}
	off := func(why string) []component.MenuItem {
		return []component.MenuItem{{Key: KernelFirstKey, Label: kernelApply, Description: why, Disabled: true}}
	}
	todo, own := len(k.todo()) > 0, k.File != ""
	switch {
	case k.Container:
		return off(kernelDescHost)
	case k.Unread != "":
		apply.Description = k.Unread
		return []component.MenuItem{apply}
	case todo && own:
		remove.Key = KernelSecondKey
		return []component.MenuItem{apply, remove}
	case todo:
		return []component.MenuItem{apply}
	case own:
		return []component.MenuItem{remove}
	}
	return off(kernelDescNothing)
}

// Removes reports whether it, one of MenuItems, is Remove settings; Apply
// settings otherwise.
func (KernelSettings) Removes(it component.MenuItem) bool { return it.Label == kernelRemove }

// SubMenu returns the Kernel Settings screen: the title bar; the row; the
// sections; the items; Return; "Select an option"; "Option".
func (k KernelSettings) SubMenu() SubMenu {
	return SubMenu{
		Title:    trail(kernelTitle),
		Status:   k.Status,
		Sections: k.Sections(),
		Heading:  fixtureHeading,
		Menu:     component.Menu{Items: k.MenuItems(), Exit: returnItem()},
		Prompt:   menuPrompt,
	}
}

// ApplyChange is Apply settings' change: its review each row it sets, as
// the change leaves it. With no row to set there is nothing to review, and
// ok is false.
func (k KernelSettings) ApplyChange() (ch Change, ok bool) {
	var items []component.KeyValue
	for _, r := range k.todo() {
		items = append(items, component.KeyValue{Key: r.Label, Value: strconv.Itoa(r.Sets), Note: r.SetsWords})
	}
	if len(items) == 0 {
		return Change{}, false
	}
	return Change{Step: WorkflowStep{Title: trail(kernelTitle, kernelApply)}, Question: kernelAskApply,
		Review: flow.Review{Title: kernelReview, Items: items}}, true
}

// RemoveChange is Remove settings' change: its review each row it sets
// back, at its earlier value.
func (k KernelSettings) RemoveChange() Change {
	var items []component.KeyValue
	for _, r := range k.Restores {
		items = append(items, component.KeyValue{Key: r.Label, Value: strconv.Itoa(r.Value), Note: r.Words})
	}
	return Change{Step: WorkflowStep{Title: trail(kernelTitle, kernelRemove)}, Question: kernelAskRemove,
		Review: flow.Review{Title: kernelReview, Items: items}}
}
