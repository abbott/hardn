package theme

// Glyphs is one glyph set, named by role. New picks unicodeGlyphs or
// asciiGlyphs from term.Env.Unicode, which is false for TERM=linux or dumb,
// a non-UTF-8 or CJK locale, HARDN_ASCII, term.Options.ASCII (the future
// --ascii, §3.14) and plain mode.
//
// The status markers, a step's marker and an inline result's glyph sit in
// the three-cell marker column (MarkerCol, MarkerWidth), so an ASCII
// marker such as "[+]" fits it exactly.
//
// Partial takes Not Configured's ⭘ ([ ] in ASCII), and so does a warning:
// they are told apart by colour and by the word always on the line, never
// by colour alone (the maintainer, 2026-10-21). The risk icon ⛬ ([*]) is
// no other role's, and RiskSummary alone draws it: a lockout-risk line is a
// warning, and takes Warn (the maintainer, 2026-10-21).
//
// A step's state takes the status markers (P-42, the maintainer,
// 2026-10-22): done Configured's ⛛, failed Not Configured's ⭘ and rolled
// back Partial's ⭘, each with its word; pending, running and skipped the
// key-value Bullet. A message has no glyph: info is prose, the other kinds
// are callouts. origin/main's ✓ ✗ ℹ and the first build's ⊘ ◦ ▸ ↺ ↷ ! are
// gone.
//
// East Asian ambiguous-width glyphs in the Unicode set: ⛛ ⭘ ⛬ ≫ – ≈ … █ ─.
// Lip Gloss measures each as one cell, but a terminal set to draw ambiguous
// characters two cells wide draws each as two. A single glyph then shifts
// the rest of its line by one cell. Rule is drawn in runs, so a run doubles
// in width: a 64-cell rule takes 128 cells and wraps at 80 columns.
// term.Resolve picks the ASCII set in a zh, ja or ko locale for that reason,
// but cannot see an ambiguous-wide terminal under another UTF-8 locale, such
// as an SSH session to a server set to C.UTF-8; HARDN_ASCII=1 avoids it
// there. DEV-015 accepts this cost for Rule. Shade, the bars' run, is ░
// U+2591, neutral width, so a bar keeps its width there (DEV-018).
type Glyphs struct {
	Configured    string // a control in its hardened state (Figma ⛛)
	NotConfigured string // a control not hardened (Figma ⭘)
	Partial       string // a control partly hardened (no Figma value; Not Configured's ⭘, in status.warn: DEV-024)
	Unknown       string // a state hardn could not check (no Figma value)
	NotApplicable string // a control that does not apply here; an unavailable menu item (no Figma value)
	Risk          string // the risk level's icon (Figma ⛬), and nothing else's
	Prompt        string // before a prompt's label (Figma ❯)
	Approx        string // before the risk level's gloss (Figma ≈)
	Bullet        string // before a key-value or bullet-list item (Figma -)
	Separator     string // between the items of a breadcrumb or a title bar's trail
	Ellipsis      string // marks truncated text
	Bar           string // a callout's accent bar when there is no colour to paint it
	Shade         string // the stub and fill of the context and title bars, and a section header's stub, in bar.shade
	Rule          string // draws a divider or a section header's rule
	BadgeOpen     string // opens a badge when there is no colour to paint it
	BadgeClose    string // closes a badge when there is no colour to paint it

	// Warn is a warning's glyph: the menu's and the plan's lockout-risk
	// lines (DEV-028, DEV-038). Inline is a result written under a prompt,
	// a form field's or a refused line's, glyph and text in the state's
	// colour with no word (P-42, the maintainer, 2026-10-22; DEV-062).
	Warn   string // a warning: a lockout-risk line
	Inline string // an inline result under a prompt (≫)

	// Spinner holds the frames a running step's marker cycles through
	// when the terminal can animate, one per rune (DEV-039); without
	// animation a running step shows Bullet.
	Spinner string

	// A plan, an error report and the confirm countdown (no Figma value;
	// DEV-038, DEV-041). Wrap opens each further line of code too long
	// for one, which may be a command the user types, so its ASCII form is
	// "|": a continuation pasted into a shell is then a syntax error, where
	// ">" would redirect into the file it names, and no diff starts a line
	// with it, as classic diff starts an added line with ">".
	Command string // before a command hardn runs
	Wrap    string // starts the rest of a code line too long for its line
}

// unicodeGlyphs is the Figma glyph set (DEV-013 to DEV-015, DEV-024,
// DEV-031, DEV-038, DEV-039 and DEV-062 for the glyphs Figma does not
// draw, DEV-019 for the badge brackets, DEV-018 for the bars' shade).
// Partial, Warn, Unknown and NotApplicable are the maintainer's of
// 2026-10-21, Inline the maintainer's of 2026-10-22.
var unicodeGlyphs = Glyphs{
	Configured:    "⛛",
	NotConfigured: "⭘",
	Partial:       "⭘",
	Unknown:       "?",
	NotApplicable: "–",
	Risk:          "⛬",
	Prompt:        "❯",
	Approx:        "≈",
	Bullet:        "-",
	Separator:     "›",
	Ellipsis:      "…",
	Bar:           "█",
	Shade:         "░",
	Rule:          "─",
	BadgeOpen:     "[",
	BadgeClose:    "]",
	Warn:          "⭘",
	Inline:        "≫",
	Spinner:       "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏",
	Command:       "$",
	Wrap:          "↪",
}

// asciiGlyphs is the ASCII set (DEV-012; DEV-019 for the badge brackets,
// DEV-031 for a warning, DEV-038 and DEV-039 for the plan group, DEV-062
// for the inline results' "»", U+00BB, the one glyph of this set outside
// ASCII: the maintainer's "Use ≫ and » for inline field results, with an
// exception in the ASCII test for »" (2026-10-22; "x" with ⨳ and "<>" with
// ◇ before it), at column 3; on a terminal that is not UTF-8 its two bytes
// show as other characters). Partial and Warn are NotConfigured's, as in
// the Unicode set, so "[*]" is the risk icon's alone.
var asciiGlyphs = Glyphs{
	Configured:    "[+]",
	NotConfigured: "[ ]",
	Partial:       "[ ]",
	Unknown:       "[?]",
	NotApplicable: "[-]",
	Risk:          "[*]",
	Prompt:        ">",
	Approx:        "~",
	Bullet:        "-",
	Separator:     ">",
	Ellipsis:      "...",
	Bar:           "#",
	Shade:         "#",
	Rule:          "-",
	BadgeOpen:     "[",
	BadgeClose:    "]",
	Warn:          "[ ]",
	Inline:        "»",
	Spinner:       `|/-\`,
	Command:       "$",
	Wrap:          "|",
}
