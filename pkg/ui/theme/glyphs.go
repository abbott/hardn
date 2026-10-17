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
// East Asian ambiguous-width glyphs in the Unicode set: ⛛ ⭘ ⛬ ≈ … █ ─.
// Lip Gloss measures each as one cell, but a terminal set to draw ambiguous
// characters two cells wide draws each as two. A single glyph then shifts
// the rest of its line by one cell. Bar and Rule are drawn in runs, so a run
// doubles in width: a 64-cell rule takes 128 cells and wraps at 80 columns.
// term.Resolve picks the ASCII set in a zh, ja or ko locale for that reason,
// but cannot see an ambiguous-wide terminal under another UTF-8 locale, such
// as an SSH session to a server set to C.UTF-8; HARDN_ASCII=1 avoids it
// there. DEV-015 accepts this cost for Bar and Rule.
type Glyphs struct {
	Configured    string // a control in its hardened state (Figma ⛛)
	NotConfigured string // a control not hardened (Figma ⭘)
	Partial       string // a control partly hardened, or a warning (Figma ⛬)
	Unknown       string // a state hardn could not check (no Figma value)
	NotApplicable string // a control that does not apply here; an unavailable menu item (no Figma value)
	Risk          string // the risk icon; a menu item's lockout-risk marker (Figma ⛬, as Partial)
	Prompt        string // before a prompt's label (Figma ❯)
	Approx        string // before the risk level's gloss (Figma ≈)
	Bullet        string // before a key-value or bullet-list item (Figma -)
	Separator     string // between breadcrumb items
	Ellipsis      string // marks truncated text
	Bar           string // fills a bar when there is no colour to paint it
	Rule          string // draws a divider or a section header's rule
	BadgeOpen     string // opens a badge when there is no colour to paint it
	BadgeClose    string // closes a badge when there is no colour to paint it

	// A one-line message's kind (no Figma value): origin/main's ✓ and ✗,
	// the warning marker of Partial, ℹ, and a cancel sign (DEV-031). A
	// step that is done or failed takes OK or Bad.
	OK        string // an operation that succeeded
	Warn      string // a warning
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

// unicodeGlyphs is the Figma glyph set (DEV-013 to DEV-015, DEV-031 and
// DEV-038 to DEV-041 for the glyphs Figma does not draw, DEV-019 for the
// badge brackets).
var unicodeGlyphs = Glyphs{
	Configured:    "⛛",
	NotConfigured: "⭘",
	Partial:       "⛬",
	Unknown:       "?",
	NotApplicable: "∅",
	Risk:          "⛬",
	Prompt:        "❯",
	Approx:        "≈",
	Bullet:        "-",
	Separator:     "›",
	Ellipsis:      "…",
	Bar:           "█",
	Rule:          "─",
	BadgeOpen:     "[",
	BadgeClose:    "]",
	OK:            "✓",
	Warn:          "⛬",
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
var asciiGlyphs = Glyphs{
	Configured:    "[+]",
	NotConfigured: "[ ]",
	Partial:       "[*]",
	Unknown:       "[?]",
	NotApplicable: "[-]",
	Risk:          "[*]",
	Prompt:        ">",
	Approx:        "~",
	Bullet:        "-",
	Separator:     ">",
	Ellipsis:      "...",
	Bar:           "#",
	Rule:          "-",
	BadgeOpen:     "[",
	BadgeClose:    "]",
	OK:            "[+]",
	Warn:          "[*]",
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
