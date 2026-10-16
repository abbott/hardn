package theme

// Glyphs is one glyph set, named by role. New picks unicodeGlyphs or
// asciiGlyphs from term.Env.Unicode, which is false for TERM=linux or dumb,
// a non-UTF-8 or CJK locale, --ascii, HARDN_ASCII and plain mode.
//
// The status markers sit in the three-cell marker column (MarkerCol,
// MarkerWidth), so an ASCII marker such as "[+]" fits it exactly.
//
// East Asian ambiguous-width glyphs in the Unicode set: ⛛ ⭘ ⛬ ≈ … █ ─.
// Lip Gloss measures each as one cell, so a terminal that draws ambiguous
// characters two cells wide shifts the rest of their line. term.Resolve
// already picks the ASCII set in a zh, ja or ko locale for that reason.
type Glyphs struct {
	Configured    string // a control in its hardened state (Figma ⛛)
	NotConfigured string // a control not hardened (Figma ⭘)
	Partial       string // a control partly hardened, or a warning (Figma ⛬)
	Unknown       string // a state hardn could not check (no Figma value)
	NotApplicable string // a control that does not apply to this host (no Figma value)
	Risk          string // the risk summary's icon (Figma ⛬, as Partial)
	Prompt        string // before a prompt's label (Figma ❯)
	Approx        string // before the risk level's gloss (Figma ≈)
	Bullet        string // before a key-value or bullet-list item (Figma -)
	Separator     string // between breadcrumb items
	Ellipsis      string // marks truncated text
	Bar           string // fills a bar when there is no colour to paint it
	Rule          string // draws a divider or a section header's rule
}

// unicodeGlyphs is the Figma glyph set (DEV-013 to DEV-015 for the glyphs
// Figma does not draw).
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
}

// asciiGlyphs is the ASCII set (DEV-012).
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
}
