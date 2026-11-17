package component

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// FramedEntry is one entry of a FramedList: a name in a badge, what is
// said beside it, and its rows.
type FramedEntry struct {
	// Name is shown in the badge, such as "george"; empty shows no badge
	// line.
	Name string
	// Aside is said beside the badge at theme.ValueCol, such as a last
	// login, "Nov 16 22:31 from 192.0.2.10"; empty says nothing.
	Aside string
	// Rows are the entry's rows, laid out as a BulletList.
	Rows BulletList
}

// FramedList is a list of entries in a frame open at the left, as
// the maintainer's User Management mock draws its users (DEV-077); here
// with no colour and ASCII glyphs, at 64 columns:
//
//	0 2   6                    27                                  63
//	  -------------------------------------------------------------+
//	                                                               |
//	  [george]                 Oct 20 08:14 from 192.0.2.10        |
//	                                                               |
//	  [ ] Sudo                 Enabled, passwordless               |
//	   -  SSH Keys             2 keys                              |
//	                                                               |
//	  [alice]                  None recorded                       |
//	                                                               |
//	  [+] Sudo                 Enabled                             |
//	                                                               |
//	  -------------------------------------------------------------+
//
// The top and bottom rules are Glyphs.FrameRule (─, - in ASCII) from
// column 2, theme.Indent, where a badge starts, to the corner, Glyphs.CornerTop and
// CornerBottom (┐ ┘, + in ASCII), in the frame's last content column; every
// line between them ends with Glyphs.Edge (│, | in ASCII) in that column,
// padded to it with spaces.
// Rules, corners and edge are in Section.Rule. Each entry is its badge at
// theme.Indent with Aside beside it at theme.ValueCol, as HostBlock places
// its address, in KeyValue.Value, then a blank line and its rows; a blank
// line opens and closes the frame and parts the entries. Inside, a line
// takes at most the cells up to one before the edge (Ctx.within): its rows
// keep to the grid and are cut there with the ellipsis glyph, and stack as
// they do outside it, below theme.StackBelow alone. On a terminal too
// narrow for a frame, under frameMin columns, the entries are drawn as they
// would be inside, with no frame. Entries with neither name nor rows are
// skipped, and a list with none renders as the empty string.
type FramedList struct {
	Entries []FramedEntry
}

// frameCol is where a frame's rules start: column 2, theme.Indent, where a
// badge starts (the maintainer, 2026-11-17; first column 3, under a state
// glyph).
const frameCol = theme.Indent

// frameMin is the narrowest content width a frame is drawn at: its rule
// from frameCol, a cell of rule and the corner.
const frameMin = frameCol + 2

// Render renders the list for c. See FramedList for the layout.
func (vm FramedList) Render(c Ctx) string {
	w := c.barWidth()
	framed := w >= frameMin
	ic := c
	if framed {
		ic = c.within(w - 2)
	}
	var body []string
	for _, e := range vm.Entries {
		lines := e.render(ic)
		if len(lines) == 0 {
			continue
		}
		if len(body) > 0 {
			body = append(body, "")
		}
		body = append(body, lines...)
	}
	if len(body) == 0 {
		return ""
	}
	if !framed {
		return strings.Join(body, "\n")
	}
	t, g := c.Theme.Section, c.Theme.Glyphs
	edge := span{g.Edge, t.Rule}.String()
	rule := func(corner string) string {
		r := run(g.FrameRule, t.Rule, w-1-frameCol)
		return line{gap(frameCol), {r.text + corner, t.Rule}}.String()
	}
	out := []string{rule(g.CornerTop)}
	for _, l := range append(append([]string{""}, body...), "") {
		out = append(out, l+strings.Repeat(" ", max(0, w-1-lipgloss.Width(l)))+edge)
	}
	return strings.Join(append(out, rule(g.CornerBottom)), "\n")
}

// render is the entry's lines for c, the Ctx inside the frame: its badge
// with Aside beside it, a blank line and its rows; none for an entry with
// neither name nor rows.
func (e FramedEntry) render(c Ctx) []string {
	var blocks []string
	if name := c.text(e.Name); name != "" {
		label := line{gap(theme.Indent), c.badge(name, c.width()-theme.Indent)}
		var aside line
		if a := c.text(e.Aside); a != "" {
			aside = line{{a, c.Theme.KeyValue.Value}}
		}
		blocks = append(blocks, c.columns(label, aside))
	}
	if rows := e.Rows.Render(c); rows != "" {
		blocks = append(blocks, rows)
	}
	if len(blocks) == 0 {
		return nil
	}
	return strings.Split(strings.Join(blocks, strings.Repeat("\n", theme.SectionGap+1)), "\n")
}
