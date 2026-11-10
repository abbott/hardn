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
//     numbers, bools, slices and small enums, or a pointer to another
//     component's view-model, nil for none (MenuHeading.Mode); no funcs,
//     actions or I/O handles. Its value method Render(c Ctx) string is a
//     pure function of the struct and c: no I/O, no clock, no environment
//     reads.
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
//     cut and ends with theme.Glyphs.Ellipsis. Only prose wraps, at spaces,
//     under its own first line: an info Message, a Callout's body, an
//     InlineResult's text and a bullet item, which have the rest of their
//     line to themselves. Typographic quotes in view-model text, ‘ ’ and
//     “ ”, are shown as ASCII ' and " (C12, DEV-036); components add none.
//     With the ASCII glyph set (term.Env.Unicode false) view-model text's
//     ≥ ≤ ≠ … – — × → and • are shown as >= <= != ... - - x -> and * too,
//     before it is measured (DEV-036). A menu's keys, a prompt's hint, which
//     lists the inputs it accepts, and a report's log path keep their
//     characters, as what must be typed or opened shows as it is; an answer
//     AnsweredField echoes is mapped, as it says what was typed or taken.
//   - Code. A diff's lines, a plan's target and commands, and captured
//     output are code: shown as they are, quotes (typographic ones too)
//     and indent kept, escape and control characters dropped, a tab four
//     spaces, and a character that draws as nothing or as a blank other
//     than the space shown as its code point, such as <U+200B>. A command
//     keeps its line breaks. Code is never cut, since the user confirms
//     every character of a plan: a line too long breaks at its last space,
//     else inside a word, and each further piece starts with Glyphs.Wrap,
//     then the spaces at the break (DEV-038).
//   - Off a terminal. OffTerminal is the render context of what is written
//     off a terminal: plain mode at the default width, and Ctx.Whole, under
//     which nothing is cut and a bullet item does not wrap, past the content
//     width if it must (DEV-067). NewCtx leaves Whole false, so every screen
//     keeps to the rule above.
//   - Degrading. Colours, glyphs and columns come only from the theme. A
//     bar's stub and fill are Glyphs.Shade at every profile, uncoloured
//     when Env.HasColor is false (DEV-018). With no colour there is no
//     background to paint, so a callout's accent bar is Glyphs.Bar
//     (DEV-015) and a badge is bracketed by Glyphs.BadgeOpen and
//     BadgeClose (DEV-019). A state is never shown by colour alone, at
//     any colour profile: it has a glyph and a word, as a StatusRow or a
//     step's row has them; a word, as a Callout's title has it, its bar
//     alone being colour, and so a Message of any kind but info; or, on a
//     key-value row, a marker beside the value that says it, with the
//     value's own words where a warn and a bad value share ⭘. Where Figma
//     draws a state in colour only, the word or the marker is added
//     (DEV-032, DEV-034). The one exception is the maintainer's: an
//     InlineResult has no word, its text saying what it is (DEV-062).
//   - States. Each component documents and tests the states that apply to
//     it: normal, empty, unknown, not applicable and error. A value hardn
//     could not read shows the word "Unknown" in text.muted, origin/main's
//     word for an address it could not read. A list with no items renders
//     as the empty string, so the caller leaves out the block and its gap.
//     A component that animates, such as StepProgress, takes its frame from
//     the view-model and Env.Animate, never from a clock.
//   - Goldens. Each state renders on uitest.Full() into
//     testdata/golden/<component>/<state>.golden. The states are defined
//     once, in pkg/ui/component/fixture (fixture.States), which the
//     dev-only gallery (internal/tools/uigallery) shows too.
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
//   - list (a StatusRow's lines, and the RiskSummary's factors until
//     next/p4-risk-lines) renders through the tree renderer and defaults
//     to the • bullet (list/list.go:50-54); the bullet can come from
//     Glyphs, but each line still needs the value-column indent and the
//     ellipsis cut, which leaves list nothing to do for one line each.
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
//     exit item also sits apart, after a blank line. So each item is
//     placed as columns places a row, once the menu has decided whether
//     all of its items stack, and a key is right-aligned with one gap.
//     table is ruled out as for HostBlock, and its cut is a fixed "…".
//     A compact menu's two columns are placed the same way, the second
//     after spaces up to its key: JoinHorizontal would pad the shorter
//     first column into trailing spaces (join.go:88), and a borderless
//     table would pad every last cell (table/table.go:512-518).
//   - MenuHeading: an underlined style renders each rune with its own SGR
//     sequence, spaces included (style.go:310, 318 and 428-436), so
//     "Select an option" costs 16 sequences. Accepted: the terminal draws
//     one unbroken underline, as Figma's PNG does, and only the bytes and
//     the goldens grow; one sequence for the run would have to be built by
//     hand, which is raw SGR in a component. UnderlineSpaces(false) would
//     not help: the spaces would lose their underline and every rune still
//     be styled alone (style.go:318).
//   - PromptLine is one line of spans; no helper applies.
//   - Badge, SubjectLine, Breadcrumb and AnsweredField are lines of spans
//     placed with spaces, as JoinHorizontal would pad them (join.go:88);
//     AnsweredField shares PromptLine's question and hint. SectionHeader
//     is laid out as a bar, its fill a rule.
//   - FieldHead is a line of spans, then its description wrapped as a
//     Callout's body is (wrap), then its options rendered by
//     Menu, joined with line breaks: list is ruled out as for Menu, and no
//     join or place helper is needed.
//   - KeyValue and BulletList: list could draw the bullet as its
//     enumerator from Glyphs, in place of its default • (list/list.go:
//     50-54), and switch it for a state's marker per row with
//     EnumeratorStyleFunc and an Enumerator (list/list.go:143). But a
//     stacked row, or a bullet item that wraps, is several lines, which the
//     tree renderer joins to the enumerator with JoinHorizontal
//     (tree/renderer.go:126-131), padding each to the item's widest line
//     (join.go:88): trailing spaces. The list shares Menu's stacking rule,
//     so its rows are placed as the menu's items are. table is ruled out as
//     for HostBlock.
//   - Message, Messages, InlineResult and Callout wrap their prose with
//     wrap, at spaces only: lipgloss.Wrap (wrap.go:12-19), over x/ansi's
//     Wrap, also breaks after every hyphen, splitting a package name or a
//     path, so wrap hands it only a word wider than the line. A command
//     in the text, in backticks or from a "$" word to the end of its
//     line, is laid out as one word unless wider than the line, so it
//     copies as one line. A style's Width wraps as lipgloss.Wrap does
//     (style.go:412-414) but then pads every line to the width
//     (style.go:489-497, align.go:43-48): trailing spaces again. The
//     callout's bar is painted cell by cell beside each line, so no join
//     is needed.
//   - PlanView: list could draw each change's bullet or risk glyph as its
//     enumerator, but a change is many lines, which the tree renderer
//     joins to the enumerator with JoinHorizontal, padding each to the
//     widest (tree/renderer.go:126-131, join.go:88); tree could hang a
//     diff under its change, but with box-drawing branches
//     (tree/enumerator.go:23-28) and the same padding; a diff is not a
//     table. Code wraps with Ctx.code, a loop over ansi.Truncate: x/ansi's
//     Hardwrap and Wrap break every line at one limit (x/ansi wrap.go:
//     21-31, 276-278), while a piece after the first has one cell less,
//     for Glyphs.Wrap, and Wrap also breaks at every hyphen
//     (wrap.go:270-271), which in a command such as "--unit=" reads as
//     two words.
//   - StepProgress: list with an Enumerator returning the state's marker
//     would pad a narrower marker on the left to the widest sibling's
//     (tree/renderer.go:79-81, 100-103), so in ASCII the one-cell bullet
//     or spinner frame among three-cell markers would sit at column 4, not
//     centred at column 3, and a row still needs its status column and
//     the cut; table could hold the status column, but stacks no row and
//     pads its cells into trailing spaces, as for HostBlock. Each row is a
//     line of spans placed with marker, line.to and fit.
//   - ErrorReport is built from Callout, StepProgress rows on the steps'
//     status column, code and prose, joined with the section gap;
//     ConfirmCountdown is a callout's lines of spans, its sentences
//     wrapped with wrap as a Callout's body, and code. Neither
//     needs a join or place helper.
package component
