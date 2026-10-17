package component

// ContextBar is the main menu's distribution bar, here as drawn with no
// colour:
//
//	█ Debian 12 (Bookworm) █████████████████████████████████████████
//
// It spans the content width, or the terminal's when
// theme.BarsFillContent is off: a one-cell stub, the text on the
// terminal's background with a cell either side, then the fill, painted in
// bar.bg, or drawn with Glyphs.Bar when there is no colour (C13, DEV-015,
// DEV-018). Text too long for the bar is cut with the ellipsis glyph.
type ContextBar struct {
	// Name is the distribution, in bold, such as "Debian". Empty shows
	// "Unknown".
	Name string
	// Version is the release, such as "12"; empty shows none.
	Version string
	// Codename is shown in parentheses, as given, such as "Bookworm"; empty
	// shows none.
	Codename string
}

// Render renders the bar for c. See ContextBar for the layout.
func (vm ContextBar) Render(c Ctx) string {
	t := c.Theme.ContextBar
	text := c.known(vm.Name, t.Name, t.Unknown)
	if v := c.text(vm.Version); v != "" {
		text = append(text, gap(1), span{v, t.Version})
	}
	if cn := c.text(vm.Codename); cn != "" {
		text = append(text, gap(1), span{"(" + cn + ")", t.Codename})
	}
	return c.bar(t.Fill, text)
}

// TitleBar is a sub-screen's title bar, such as "User Management": a
// ContextBar with a bold title in place of the distribution. Proposal P-08
// decides whether sub-screens use it or the Header and a Breadcrumb.
type TitleBar struct {
	// Title is the screen's title. Empty draws the bar with no text.
	Title string
}

// Render renders the bar for c. See ContextBar for the layout.
func (vm TitleBar) Render(c Ctx) string {
	var text line
	if s := c.text(vm.Title); s != "" {
		text = line{{s, c.Theme.TitleBar.Title}}
	}
	return c.bar(c.Theme.TitleBar.Fill, text)
}
