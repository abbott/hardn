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
	for _, it := range vm.Items {
		if r, ok := it.row(c, false); ok {
			rows = append(rows, r)
		}
	}
	exit, hasExit := vm.Exit.row(c, true)
	stack := c.stacked() || exit.cramped(c)
	for _, r := range rows {
		stack = stack || r.cramped(c)
	}
	var out []string
	for _, r := range rows {
		out = append(out, c.layout(stack, r.head, r.value, r.note))
	}
	if hasExit {
		if len(out) > 0 {
			out = append(out, make([]string, theme.SectionGap)...)
		}
		out = append(out, c.layout(stack, exit.head, exit.value, exit.note))
	}
	return strings.Join(out, "\n")
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

// row returns the parts of one item, or of the exit item when exit is set.
// An item with no label is not shown: ok is false.
func (it MenuItem) row(c Ctx, exit bool) (r entry, ok bool) {
	m, g := c.Theme.Menu, c.Theme.Glyphs
	if !it.Shown() {
		return entry{}, false
	}
	label := clean(it.Label)
	keyStyle, labelStyle, descStyle, asideStyle := m.Key, m.Label, m.Description, m.Aside
	var note line
	switch {
	case exit:
		keyStyle, labelStyle, descStyle = m.ExitKey, m.Exit, m.Exit
	case it.Disabled:
		keyStyle, labelStyle, descStyle, asideStyle = m.Disabled, m.Disabled, m.Disabled, m.Disabled
		note = line{{g.NotApplicable + " " + unavailable, m.Disabled}}
	case it.Risk:
		note = line{{g.Risk + " " + lockoutRisk, m.Risk}}
	}
	var head line
	if k := clean(it.Key); k != "" {
		k += ")"
		head = line{gap(theme.MarkerCol + theme.MarkerWidth - lipgloss.Width(k)), {k, keyStyle}}
	}
	head = append(head.to(max(theme.LabelCol, head.width()+1)), span{label, labelStyle})
	var value line
	if d := clean(it.Description); d != "" {
		value = line{{d, descStyle}}
	}
	if a := clean(it.Aside); a != "" {
		if len(value) > 0 {
			value = append(value, gap(1))
		}
		value = append(value, span{"(" + a + ")", asideStyle})
	}
	return entry{head, value, note}, true
}
