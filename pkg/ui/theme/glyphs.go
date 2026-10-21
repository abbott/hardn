package theme

// Glyphs is one glyph set, named by role. New picks unicodeGlyphs or
// asciiGlyphs from term.Env.Unicode, which is false for TERM=linux or dumb,
// a non-UTF-8 or CJK locale, HARDN_ASCII, term.Options.ASCII (the future
// --ascii, §3.14) and plain mode.
//
// The status markers, a message's kind, a step's state and the urgent
// mark sit in the three-cell marker column (MarkerCol, MarkerWidth), so an
// ASCII marker such as "[+]" fits it exactly.
//
// Partial takes Not Configured's ⭘ ([ ] in ASCII), and so does a warning:
// they are told apart by colour and by the word always on the line, never
// by colour alone (the maintainer, 2026-10-21). The risk icon ⛬ ([*]) is
// no other role's, and RiskSummary alone draws it: a lockout-risk line is a
// warning, and takes Warn (the maintainer, 2026-10-21).
//
// East Asian ambiguous-width glyphs in the Unicode set: ⛛ ⭘ ⛬ – ≈ … █ ─.
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

	// A one-line message's kind (no Figma value): origin/main's ✓ and ✗,
	// Partial's ⭘ for a warning, ℹ, and a cancel sign (DEV-031). A step
	// that is done or failed takes OK or Bad.
	OK        string // an operation that succeeded
	Warn      string // a warning, a lockout-risk line (DEV-028, DEV-038), and the confirm countdown while there is time
	Bad       string // an operation that failed
	Info      string // information
	Cancelled string // an operation the user stopped

	// A step's state in StepProgress (no Figma value; DEV-039), beside OK
	// and Bad. Spinner holds the frames a running step cycles through when
	// the terminal can animate, one per rune; Running is its still glyph.
	Pending    string // a step not started
	Running    string // the step under way, when nothing animates
	Spinner    string // the running step's frames, one per rune
	RolledBack string // a step undone after a later one failed
	Skipped    string // a step left out, such as one already done

	// A plan, an error report and the confirm countdown (no Figma value;
	// DEV-038, DEV-041). Wrap opens each further line of code too long
	// for one, which may be a command the user types, so its ASCII form is
	// "|": a continuation pasted into a shell is then a syntax error, where
	// ">" would redirect into the file it names, and no diff starts a line
	// with it, as classic diff starts an added line with ">".
	Command string // before a command hardn runs
	Wrap    string // starts the rest of a code line too long for its line
	Urgent  string // the confirm countdown's last seconds
}

// unicodeGlyphs is the Figma glyph set (DEV-013 to DEV-015, DEV-024,
// DEV-031 and DEV-038 to DEV-041 for the glyphs Figma does not draw,
// DEV-019 for the badge brackets, DEV-018 for the bars' shade). Partial,
// Warn, Unknown and NotApplicable are the maintainer's of 2026-10-21.
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
	OK:            "✓",
	Warn:          "⭘",
	Bad:           "✗",
	Info:          "ℹ",
	Cancelled:     "⊘",
	Pending:       "◦",
	Running:       "▸",
	Spinner:       "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏",
	RolledBack:    "↺",
	Skipped:       "↷",
	Command:       "$",
	Wrap:          "↪",
	Urgent:        "!",
}

// asciiGlyphs is the ASCII set (DEV-012; DEV-019 for the badge brackets,
// DEV-031 for the message kinds, DEV-038 to DEV-041 for the plan group).
// Partial and Warn are NotConfigured's, as in the Unicode set, so "[*]" is
// the risk icon's alone.
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
	OK:            "[+]",
	Warn:          "[ ]",
	Bad:           "[!]",
	Info:          "[i]",
	Cancelled:     "[-]",
	Pending:       "[ ]",
	Running:       "[>]",
	Spinner:       `|/-\`,
	RolledBack:    "[<]",
	Skipped:       "[-]",
	Command:       "$",
	Wrap:          "|",
	Urgent:        "[!]",
}
