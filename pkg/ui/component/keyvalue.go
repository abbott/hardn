package component

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// ValueState is what a key-value row's value says about the host, as
// Figma colours it: "Enabled" is red for root SSH login and green for
// backups. The zero value is ValuePlain, a value that says neither.
type ValueState int

// The value states. ValueOK, ValueWarn and ValueBad are Figma's green,
// amber and red; ValueUnknown and ValueNotApplicable are the brief's
// states Figma does not draw (DEV-034).
const (
	ValuePlain ValueState = iota
	ValueOK
	ValueWarn
	ValueBad
	ValueUnknown
	ValueNotApplicable
)

// style returns the state's style in t; a plain value, or a value outside
// the states, takes Value.
func (s ValueState) style(t theme.KeyValueStyles) lipgloss.Style {
	switch s {
	case ValueOK:
		return t.ValueOK
	case ValueWarn:
		return t.ValueWarn
	case ValueBad:
		return t.ValueBad
	case ValueUnknown:
		return t.ValueUnknown
	case ValueNotApplicable:
		return t.ValueNotApplicable
	}
	return t.Value
}

// glyph returns the status marker that shows the state in place of the
// bullet, as StatusRow marks the same verdicts: Configured for ValueOK,
// NotConfigured for ValueBad, Partial for ValueWarn. A plain value has
// none.
func (s ValueState) glyph(g theme.Glyphs) string {
	switch s {
	case ValueOK:
		return g.Configured
	case ValueWarn:
		return g.Partial
	case ValueBad:
		return g.NotConfigured
	case ValueUnknown:
		return g.Unknown
	case ValueNotApplicable:
		return g.NotApplicable
	}
	return ""
}

// word returns what an empty value of the state shows: "Unknown" and
// "Not Applicable", as StatusRow words them (DEV-024), and nothing for
// the other states.
func (s ValueState) word() string {
	switch s {
	case ValueUnknown:
		return unknown
	case ValueNotApplicable:
		return StatusNotApplicable.String()
	}
	return ""
}

// KeyValue is one row of a sub-screen's details: Glyphs.Bullet, the key
// and its value. Here with ASCII glyphs, Figma's rows from 02-SSH_LOGIN
// and 05-BACKUP:
//
//	0 2   6                    27
//	  [ ] Root SSH Login       Enabled
//	   -  SSH Port             2230
//	  [+] Backup Path          /var/backups/hardn Writable
//
// The bullet is centred in the marker column (DEV-020), the key starts at
// theme.LabelCol and the value at theme.ValueCol (DEV-034). The value
// takes its state's colour, as Figma colours it, and a stated value's
// bullet gives way to the state's status marker in that colour, at every
// colour profile: ⛛ [+] for ValueOK, ⭘ [ ] for ValueBad and ValueWarn,
// as Not Configured and Partial share it, ? [?] for ValueUnknown and
// – [-] for ValueNotApplicable. A plain value keeps the bullet. ValueWarn
// and ValueBad differ by colour, and without colour by the value's own
// words alone, so a caller gives them words that say which (the
// maintainer's glyphs of 2026-10-21; DEV-034).
// Below theme.StackBelow, or when the key reaches the value column, or
// when the value column has no room for one letter, the value goes under
// the key at theme.StackCol (DEV-021). A value wider than its line is cut
// with the ellipsis glyph.
//
// With no value the row is a bullet item, its key alone, as in Figma's
// checklist under a warning, and its State shows nothing, except that
// ValueUnknown and ValueNotApplicable show "Unknown" and "Not Applicable".
// A bullet item's text is prose with nothing beside it, so it wraps at
// spaces under itself, at theme.LabelCol, rather than being cut. A row
// with neither key nor value renders as the empty string.
type KeyValue struct {
	// Key names the setting, such as "Root SSH Login"; or, with no value,
	// is the bullet item's text.
	Key string
	// Value is the setting's value, such as "Enabled" or "2230". It takes
	// State's style, unless there is a Note.
	Value string
	// State is what the value says about the host; the zero value is
	// ValuePlain.
	State ValueState
	// Note follows the value in State's style, the value then being plain,
	// as Figma's "/var/backups/hardn Writable" and "Enabled, No password";
	// empty shows none.
	Note string
}

// Render renders the row for c. See KeyValue for the layout.
func (vm KeyValue) Render(c Ctx) string {
	r, ok := vm.row(c)
	if !ok {
		return ""
	}
	return vm.lay(c, r, c.stacked() || r.cramped(c))
}

// lay lays out the row from its parts r, stacked when stack is set. A
// bullet item wraps its text under itself.
func (vm KeyValue) lay(c Ctx, r entry, stack bool) string {
	if r.value != nil {
		return c.layout(stack, r.head, r.value)
	}
	t, w := c.Theme.KeyValue, c.width()
	lead := marker(c.Theme.Glyphs.Bullet, t.Bullet).to(theme.LabelCol)
	var out []string
	for _, l := range wrap(c.text(vm.Key), w-theme.LabelCol) {
		out = append(out, c.fit(append(lead, span{l, t.Key}), w))
		lead = line{gap(theme.LabelCol)}
	}
	return strings.Join(out, "\n")
}

// row returns the row's parts. A row with neither key nor value is not
// shown: ok is false.
func (vm KeyValue) row(c Ctx) (r entry, ok bool) {
	t, g := c.Theme.KeyValue, c.Theme.Glyphs
	state := vm.State.style(t)
	var value line
	switch v, n := c.text(vm.Value), c.text(vm.Note); {
	case n != "" && v != "":
		value = line{{v, t.Value}, gap(1), {n, state}}
	case n != "" || v != "":
		value = line{{v + n, state}}
	case vm.State.word() != "":
		value = line{{vm.State.word(), state}}
	}
	key := c.text(vm.Key)
	if key == "" && value == nil {
		return entry{}, false
	}
	head := marker(g.Bullet, t.Bullet)
	if m := vm.State.glyph(g); m != "" && value != nil {
		head = marker(m, state)
	}
	if key != "" {
		head = append(head.to(theme.LabelCol), span{key, t.Key})
	}
	return entry{head: head, value: value}, true
}

// BulletList is a block of KeyValue rows on one grid, such as Figma's
// "SSH Configuration" details, or bullet items with no values, such as
// the checklist under its warning. Each row is laid out as KeyValue lays
// it out, and the whole list stacks when one of its rows would, so that
// its values share one column, as Menu does (DEV-027). Rows with neither
// key nor value are skipped, and a list with none renders as the empty
// string.
type BulletList struct {
	Items []KeyValue
}

// Render renders the rows for c, one after another.
func (vm BulletList) Render(c Ctx) string {
	var items []KeyValue
	var rows []entry
	stack := c.stacked()
	for _, it := range vm.Items {
		if r, ok := it.row(c); ok {
			items, rows = append(items, it), append(rows, r)
			stack = stack || r.cramped(c)
		}
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = items[i].lay(c, r, stack)
	}
	return strings.Join(out, "\n")
}
