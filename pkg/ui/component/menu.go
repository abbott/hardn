package component

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The words of a menu item's note line, for the item states Figma does not
// draw (DEV-028; proposal P-22 asks the maintainer to approve them).
const (
	lockoutRisk = "Lockout risk"
	unavailable = "Unavailable"
)

// MenuHeading is the line above a menu: "Select an option", underlined, as
// Figma draws it, and on the main menu the ModeIndicator beside it at the
// value column. Here with no colour (the underline is not shown):
//
//	0 2   6                    27
//	  Select an option         Dry-run Mode Disabled
//
// The title starts at theme.Indent (C11, DEV-029). Below theme.StackBelow,
// when the title reaches the value column, or when not even the mode's
// word fits beside it, the mode goes under it at theme.StackCol (DEV-021),
// placed as the ModeIndicator places itself, so its word stays whole. A
// title wider than the content is cut with the ellipsis glyph. With no
// title the heading is the ModeIndicator alone, and with neither it
// renders as the empty string.
type MenuHeading struct {
	// Title is the heading, such as "Select an option".
	Title string
	// Mode is the dry-run mode shown beside the title; nil shows none, as
	// on Figma's sub-menus.
	Mode *ModeIndicator
}

// Render renders the heading for c. See MenuHeading for the layout.
func (vm MenuHeading) Render(c Ctx) string {
	title := clean(vm.Title)
	switch {
	case title == "" && vm.Mode == nil:
		return ""
	case title == "":
		return vm.Mode.Render(c)
	}
	w := c.width()
	head := line{gap(theme.Indent), {title, c.Theme.Menu.Heading}}
	if vm.Mode == nil {
		return c.fit(head, w)
	}
	beside := append(head.to(theme.ValueCol), vm.Mode.spans(c, w-theme.ValueCol)...)
	if c.stacked() || head.width() >= theme.ValueCol || beside.width() > w {
		return c.fit(head, w) + "\n" + vm.Mode.at(c, theme.StackCol)
	}
	return beside.String()
}

// MenuItem is one numbered choice of a Menu, as plain data: what the user
// types to choose it and what it says. What choosing it does is bound to
// the item elsewhere (pkg/ui/nav's Option), never here.
type MenuItem struct {
	// Key is what the user types to choose the item, such as "1" or "10";
	// it shows as "1)". Empty shows none, and the item is not accepted.
	Key string
	// Label names the choice, such as "User Management". An item with no
	// label renders nothing and is not accepted.
	Label string
	// Description says what the choice does, such as "Create, Configure";
	// empty shows none.
	Description string
	// Aside is shown in parentheses after the description, in text.faint,
	// such as "sudo, SSH keys"; give it without the parentheses.
	Aside string
	// Disabled marks an item that cannot be chosen now: every part of it
	// takes the Disabled style and a note line says "Unavailable". It is the
	// brief's "enabled" field inverted, so that the zero value is an item
	// that can be chosen.
	Disabled bool
	// Risk marks an item whose changes could lock the user out (brief
	// Phase 3's LockoutRisk): a note line says "Lockout risk" in
	// Menu.Risk. A disabled item shows no risk, since it cannot be chosen.
	Risk bool
}

// Menu is a numbered menu: the items, then the exit or return item after a
// blank line. Here with no colour and ASCII glyphs, Figma's main menu cut
// short, its item 2 marked risky and its item 3 disabled:
//
//	0 2   6                    27
//	   1) User Management      Create, Configure (sudo, SSH keys)
//	   2) SSH Login            Toggle root SSH access
//	                           [*] Lockout risk
//	   3) DNS                  Configure Nameservers
//	                           [-] Unavailable
//	  10) Logs                 View log file
//
//	   0) Exit                 q to quit
//
// Keys are right-aligned in the marker column, so "1)" and "10)" end at
// the same column (DEV-020), and labels start at theme.LabelCol: a key of
// up to five cells, such as "100)", grows left into the indent to keep its
// label there, and a wider one pushes its label right. Descriptions start
// at theme.ValueCol (DEV-027). An item's states never rest on
// colour alone: under its description a note line gives a glyph and a word,
// Glyphs.NotApplicable and "Unavailable" in Menu.Disabled for a disabled
// item, or Glyphs.Risk and "Lockout risk" in Menu.Risk for a risky one
// (DEV-028).
//
// The whole menu stacks, every item's description and note under its
// label at theme.StackCol (DEV-021), so that one description column holds
// for the menu: below theme.StackBelow; when the label of an item with a
// description or note reaches the value column (DEV-027); and when the
// value column has no room for a whole note or for one letter of a
// description. A label is never cut beside the description, since it
// names the choice; text wider than the content is cut with the ellipsis
// glyph.
//
// A compact menu (Compact) lays its items out in two columns, down then
// across, each its key and label alone, so that ten items take five
// lines; here Figma's main menu, as the main menu's compact form shows it
// on a short terminal (DEV-052):
//
//	0 2   6                    27  31
//	   1) User Management       6) Dry-Run
//	   2) SSH Login             7) Run All
//	   3) DNS                   8) Environment
//	   4) Firewall              9) System Details
//	   5) Backup               10) Logs
//
//	   0) Exit                 q to quit
//
// The first column is laid out as above; the second mirrors it from the
// value column, its keys right-aligned to end at column 29 and its labels
// at column 31. Descriptions and asides are left out; the exit item keeps
// its own. A menu that two columns cannot show whole is laid out in one,
// as above: when an item has a note line, whose glyph and word would not
// fit beside it; when a label of the first column reaches the value
// column; or when a key of the second column is wider than the marker
// column, or a label of it runs past the content width.
//
// The view-model holds no actions: pkg/ui/nav binds each item to what it
// does (nav.Menu). Bindings gives the keys the menu accepts, from which a
// PromptLine generates its hint. A menu with no items and no exit renders as the
// empty string.
type Menu struct {
	// Items are the numbered choices, in order.
	Items []MenuItem
	// Exit is the exit or return item, such as "0) Exit" with the
	// description "q to quit" (the brief's words, generated by
	// keys.Set.Help; DEV-030), or "0) Return": its key in Menu.ExitKey and
	// the rest in Menu.Exit. Its Disabled and Risk do not apply. With no
	// label it is not shown.
	Exit MenuItem
	// Compact lays the items out in two columns, keys and labels alone,
	// where they fit (see above): the main menu's compact form on a
	// terminal shorter than theme.CompactBelow (pkg/ui/screen).
	Compact bool
}

// Shown reports whether the menu shows the item: one whose label is
// empty once its escape sequences, control characters and surrounding
// spaces are removed, or measures no cells, renders nothing and is not
// accepted (Bindings). pkg/ui/nav decides with it which items it checks
// and dispatches, so that it agrees with the menu drawn.
func (it MenuItem) Shown() bool { return clean(it.Label) != "" }

// Render renders the menu for c. See Menu for the layout.
func (vm Menu) Render(c Ctx) string {
	var rows []entry
	var shown []MenuItem
	for _, it := range vm.Items {
		if r, ok := it.row(c, false); ok {
			rows = append(rows, r)
			shown = append(shown, it)
		}
	}
	exit, hasExit := vm.Exit.row(c, true)
	stack := c.stacked() || exit.cramped(c)
	out, paired := []string(nil), false
	if vm.Compact {
		out, paired = pairs(c, shown)
	}
	if !paired {
		for _, r := range rows {
			stack = stack || r.cramped(c)
		}
		for _, r := range rows {
			out = append(out, c.layout(stack, r.head, r.value, r.note))
		}
	}
	if hasExit {
		if len(out) > 0 {
			out = append(out, make([]string, theme.SectionGap)...)
		}
		out = append(out, c.layout(stack, exit.head, exit.value, exit.note))
	}
	return strings.Join(out, "\n")
}

// pairs lays items out in two columns, down then across, each its key and
// label (keyed): the first half, rounded up, from theme.MarkerCol, and the
// rest beside them from theme.ValueCol. ok is false when two columns
// cannot show every item whole and on its own (see Menu).
func pairs(c Ctx, items []MenuItem) (out []string, ok bool) {
	type cell struct{ key, label span }
	cells := make([]cell, len(items))
	for i, it := range items {
		key, label, _, note := it.parts(c, false)
		if len(note) > 0 {
			return nil, false
		}
		cells[i] = cell{key, label}
	}
	half := (len(cells) + 1) / 2
	for i := range half {
		l := keyed(nil, theme.MarkerCol, cells[i].key, cells[i].label)
		if l.width() >= theme.ValueCol {
			return nil, false
		}
		if j := i + half; j < len(cells) {
			if lipgloss.Width(cells[j].key.text) > theme.MarkerWidth {
				return nil, false
			}
			l = keyed(l, theme.ValueCol, cells[j].key, cells[j].label)
		}
		if l.width() > c.width() {
			return nil, false
		}
		out = append(out, l.String())
	}
	return out, true
}

// keyed appends to l a menu item's key, right-aligned to end where a
// marker column starting at col ends, and its label one cell after that
// column, or one cell after a key too wide for it. An empty key is left
// out, and the label keeps its column.
func keyed(l line, col int, key, label span) line {
	if key.text != "" {
		l = append(l.to(col+theme.MarkerWidth-lipgloss.Width(key.text)), key)
	}
	return append(l.to(max(col+theme.MarkerWidth+1, l.width()+1)), label)
}

// Bindings returns the inputs the menu accepts: the key of each item that
// is shown and can be chosen, then the exit item's. A PromptLine generates
// its hint from them, with any other bindings of the screen, such as q to
// quit. They carry no help: each item's label says what it does.
func (vm Menu) Bindings() keys.Set {
	var out keys.Set
	add := func(it MenuItem, exit bool) {
		if k := clean(it.Key); k != "" && it.Shown() && (exit || !it.Disabled) {
			out = append(out, keys.Binding{Keys: []string{k}})
		}
	}
	for _, it := range vm.Items {
		add(it, false)
	}
	add(vm.Exit, true)
	return out
}

// entry is one entry of a list on the grid, in its parts: the head (a menu
// item's key and label, a key-value row's bullet and key), the value (the
// description with its aside, the value) and a note line under the value.
// Menu and BulletList share it, so both decide their stacking alike.
type entry struct{ head, value, note line }

// cramped reports whether r cannot sit beside its head: it has a value or
// note, and its head reaches the value column, or the value column has no
// room for the whole note or for one letter of the value.
func (r entry) cramped(c Ctx) bool {
	if r.value.width() == 0 && r.note.width() == 0 {
		return false
	}
	room := c.width() - theme.ValueCol
	return r.head.width() >= theme.ValueCol || r.note.width() > room ||
		(r.value.width() > 0 && room <= lipgloss.Width(c.Theme.Glyphs.Ellipsis))
}

// row returns the parts of one item, or of the exit item when exit is set:
// its key and label laid out in the marker and label columns (keyed), its
// description with the aside, and its note. An item with no label is not
// shown: ok is false.
func (it MenuItem) row(c Ctx, exit bool) (r entry, ok bool) {
	if !it.Shown() {
		return entry{}, false
	}
	key, label, value, note := it.parts(c, exit)
	return entry{keyed(nil, theme.MarkerCol, key, label), value, note}, true
}

// parts returns an item's key ("1)", or empty with no key) and label, its
// description with the aside, and its note line, each in its style; for
// the exit item when exit is set. The item is shown (Shown).
func (it MenuItem) parts(c Ctx, exit bool) (key, label span, value, note line) {
	m, g := c.Theme.Menu, c.Theme.Glyphs
	keyStyle, labelStyle, descStyle, asideStyle := m.Key, m.Label, m.Description, m.Aside
	switch {
	case exit:
		keyStyle, labelStyle, descStyle = m.ExitKey, m.Exit, m.Exit
	case it.Disabled:
		keyStyle, labelStyle, descStyle, asideStyle = m.Disabled, m.Disabled, m.Disabled, m.Disabled
		note = line{{g.NotApplicable + " " + unavailable, m.Disabled}}
	case it.Risk:
		note = line{{g.Risk + " " + lockoutRisk, m.Risk}}
	}
	if k := clean(it.Key); k != "" {
		key = span{k + ")", keyStyle}
	}
	label = span{clean(it.Label), labelStyle}
	if d := clean(it.Description); d != "" {
		value = line{{d, descStyle}}
	}
	if a := clean(it.Aside); a != "" {
		if len(value) > 0 {
			value = append(value, gap(1))
		}
		value = append(value, span{"(" + a + ")", asideStyle})
	}
	return key, label, value, note
}
