// Package component holds hardn's design-system components: pure renderers
// that turn a view-model into styled text for one render environment.
// Screens are assembled from them and style nothing themselves.
//
// # Conventions
//
// Every component follows these rules; the first P1B unit set them and the
// later ones follow them.
//
//   - Render context. A Ctx bundles the term.Env and the theme.Theme built
//     once from it (NewCtx). The brief's Render(vm, env) is Render(vm, ctx).
//   - View-model. A component is an exported struct of plain data: strings,
//     numbers, bools, slices and small enums; no funcs, actions or I/O
//     handles. Its value method Render(c Ctx) string is a pure function of
//     the struct and c: no I/O, no clock, no environment reads.
//   - Output. No line wider than the content width, min(Env.Width,
//     theme.ContentWidth) under theme.CapContent; a bar fills it, or the
//     terminal's width when theme.BarsFillContent is off. No trailing
//     spaces outside a painted background; no trailing newline. The caller
//     joins blocks with theme.SectionGap blank lines.
//   - Grid. Rows start at theme.Indent, the header and bars at column 0. A
//     state glyph is centred in the marker column (DEV-020), a label starts
//     at theme.LabelCol and a value at theme.ValueCol. Below
//     theme.StackBelow a two-column layout stacks: the label on its own
//     line, each value under it at theme.StackCol (DEV-021).
//   - Text. View-model strings are shown as one line of plain text: escape
//     sequences are removed, tabs and line breaks become spaces, other
//     control characters are dropped and surrounding spaces trimmed. Widths
//     are measured with lipgloss.Width, never len. Text that does not fit is
//     cut and ends with theme.Glyphs.Ellipsis; nothing wraps.
//   - Degrading. Colours, glyphs and columns come only from the theme. When
//     Env.HasColor is false there is no background to paint, so a bar is a
//     run of Glyphs.Bar (DEV-015, DEV-018) and a badge is bracketed by
//     Glyphs.BadgeOpen and BadgeClose (DEV-019). A state is never shown by
//     colour alone: it has a word, and a glyph where Figma draws one.
//   - States. Each component documents and tests the states that apply to
//     it: normal, empty, unknown, not applicable and error. A value hardn
//     could not read shows the word "Unknown" in status.unknown, origin/main's
//     word for an address it could not read. A list with no items renders
//     as the empty string, so the caller leaves out the block and its gap.
//   - Goldens. Each state renders on uitest.Full() into
//     testdata/golden/<component>/<state>.golden.
//
// # Lip Gloss helpers
//
// Evaluated before hand-rolling, as brief 1B asks; outcomes by component:
//
//   - JoinHorizontal pads every line of a block to the block's width with
//     plain spaces (join.go:88 in lipgloss v2.0.6), and PlaceHorizontal pads
//     to the full width it is given (position.go:43-64), so both leave
//     trailing spaces. Header, HostBlock, RiskSummary, StatusRow,
//     ModeIndicator, MenuHeading and Menu place their columns with spaces
//     up to the grid column instead, which never pads after the last cell.
//   - list (the RiskSummary factors) renders through the tree renderer and
//     defaults to the • bullet (list/list.go:50-54); the bullet can come
//     from Glyphs, but each line still needs the value-column indent and
//     the ellipsis cut, which leaves list nothing to do for one line per
//     factor.
//   - table (the label and value rows of HostBlock and RiskSummary, and
//     the columns helper that later two-column rows share): a borderless
//     table pads every cell to its column's width (table/table.go:512-518)
//     and joins the cells with JoinHorizontal (table.go:534), so each value
//     shorter than the last column leaves trailing spaces. columns places
//     values with spaces up to the grid column instead.
//   - tree is not used: nothing in the header, status or menu groups
//     nests.
//   - StatusList: list could draw each row's glyph as its enumerator,
//     styled per row with EnumeratorStyleFunc (list/list.go:143). The tree
//     renderer pads an enumerator on the left only up to a wider sibling's
//     (tree/renderer.go:79-81, 100-103), and the status markers of one
//     glyph set share a width, so an Enumerator returning the marker
//     already centred (DEV-020) puts the glyph at column 3, once
//     EnumeratorStyle replaces the default PaddingRight(1)
//     (tree/renderer.go:24-26). list still adds nothing: each item would
//     need the label and value columns, the stacking and the cut. table is
//     ruled out as for HostBlock, and its own cut is a fixed "…"
//     (table/table.go:567), not Glyphs.Ellipsis. Each row is placed with
//     columns instead.
//   - Divider is one run of text; no helper applies.
//   - Menu: list would right-align the keys as the menu wants them, since
//     the tree renderer pads each enumerator on the left to the widest
//     sibling's (tree/renderer.go:79-81, 100-103), so an Enumerator
//     returning "1)" or "10)" lines them up. But an item with a note line,
//     or stacked, is several lines, and the renderer joins enumerator and
//     item with JoinHorizontal (tree/renderer.go:126-131), which pads each
//     line of the item to its widest (join.go:88): trailing spaces. The
//     exit item also sits apart, after a blank line. So columns places
//     each item, and a key is right-aligned with one gap. table is ruled
//     out as for HostBlock, and its cut is a fixed "…".
//   - MenuHeading: an underlined style renders each rune with its own SGR
//     sequence, spaces included (style.go:310, 318 and 428-436), so
//     "Select an option" costs 16 sequences. Accepted: the terminal draws
//     one unbroken underline, as Figma's PNG does, and only the bytes and
//     the goldens grow; one sequence for the run would have to be built by
//     hand, which is raw SGR in a component. UnderlineSpaces(false) would
//     not help: the spaces would lose their underline and every rune still
//     be styled alone (style.go:318).
//   - PromptLine is one line of spans; no helper applies.
package component
