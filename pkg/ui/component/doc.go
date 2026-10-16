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
//     word for an address it could not read.
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
//     trailing spaces. Header, HostBlock and RiskSummary place their columns
//     with spaces up to the grid column instead, which never pads after the
//     last cell.
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
//   - tree is not used: nothing in this group nests.
package component
