package theme

// Glyphs is one glyph set, named by role. New picks unicodeGlyphs or
// asciiGlyphs from term.Env.Unicode, which is false for TERM=linux or dumb,
// a non-UTF-8 or CJK locale, HARDN_ASCII, term.Options.ASCII (the future
// --ascii, §3.14) and plain mode.
//
// The status markers sit in the three-cell marker column (MarkerCol,
// MarkerWidth), so an ASCII marker such as "[+]" fits it exactly.
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
}

// unicodeGlyphs is the Figma glyph set (DEV-013 to DEV-015 for the glyphs
// Figma does not draw, DEV-019 for the badge brackets).
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
}

// asciiGlyphs is the ASCII set (DEV-012; DEV-019 for the badge brackets).
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
}
