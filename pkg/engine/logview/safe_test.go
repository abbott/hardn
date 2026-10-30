package logview_test

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
)

// TestSafe: what a terminal would act on never reaches it, and what a line
// held still shows.
func TestSafe(t *testing.T) {
	for in, want := range map[string]string{
		"plain":              "plain",
		"":                   "",
		"\x1b[31mred\x1b[0m": "^[[31mred^[[0m",
		"a\tb":               "a    b",
		"a\u00a0b":           "a<U+00A0>b",
		"don’t “q” ≥ →":      "don’t “q” ≥ →",
		"a\x7fb":             "a^?b",
		"café":               "café",
		"caf\xe9":            "caf<0xe9>",
		"a\x00b\rc\nd\x1f":   "a^@b^Mc^Jd^_",
		// An OSC ended by BEL and by ST: a title, a clipboard, a link.
		"\x1b]0;t\x07x":                         "^[]0;t^Gx",
		"\x1b]52;c;ZXZpbA==\x07":                "^[]52;c;ZXZpbA==^G",
		"\x1b]8;;http://x\x1b\\y\x1b]8;;\x1b\\": "^[]8;;http://x^[\\y^[]8;;^[\\",
		// A C1 CSI as U+009B and as the raw byte, and C1's others.
		"\u009b31m": "<U+009B>31m",
		"\x9b31m":   "<0x9b>31m",
		"\u009d0;t": "<U+009D>0;t",
		"\u0085":    "<U+0085>",
		// An invalid sequence next to an escape: nothing joins them.
		"\xe2\x1b[31m":     "<0xe2>^[[31m",
		"\xe2\x80\x1b[31m": "<0xe2><0x80>^[[31m",
		"\x1b\xe2\x80[31m": "^[<0xe2><0x80>[31m",
		"\xf0\x9f\x1b]0;t": "<0xf0><0x9f>^[]0;t",
		// Overlong encodings of ESC, of CSI and of "/", and a surrogate.
		"\xc0\x9b":     "<0xc0><0x9b>",
		"\xc0\xaf":     "<0xc0><0xaf>",
		"\xe0\x80\x9b": "<0xe0><0x80><0x9b>",
		"\xed\xa0\x80": "<0xed><0xa0><0x80>",
		// Bidi overrides, embeddings, isolates and marks.
		"abc\u202edef":             "abc<U+202E>def",
		"\u202a\u202b\u202c\u202d": "<U+202A><U+202B><U+202C><U+202D>",
		"\u2066\u2067\u2068\u2069": "<U+2066><U+2067><U+2068><U+2069>",
		"\u200e\u200f\u061c":       "<U+200E><U+200F><U+061C>",
		// What draws as nothing, or as a blank other than the space.
		"a\u200bb\ufeffc\u00add": "a<U+200B>b<U+FEFF>c<U+00AD>d",
		"a\u2028b\u2029c\u3000d": "a<U+2028>b<U+2029>c<U+3000>d",
		"\U000e0041\ufe0f\u034f": "<U+E0041><U+FE0F><U+034F>",
		"\ufffe\uffff":           "<U+FFFE><U+FFFF>",
		// A real replacement character is a character.
		"a\ufffdb": "a\ufffdb",
		// A line that already reads as a mark stays as it is.
		"^[[31m <U+202E> <0xe9>": "^[[31m <U+202E> <0xe9>",
	} {
		if got := logview.Safe(in); got != want {
			t.Errorf("Safe(%q) = %q, want %q", in, got, want)
		}
	}
}

// checkSafe fails unless out could reach a terminal: valid UTF-8, no control or
// format character, and Safe leaves it as it is.
func checkSafe(t *testing.T, in, out string) {
	t.Helper()
	if !utf8.ValidString(out) {
		t.Fatalf("Safe(%q) = %q: not UTF-8", in, out)
	}
	for _, r := range out {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			t.Fatalf("Safe(%q) = %q: holds %U", in, out, r)
		}
	}
	if again := logview.Safe(out); again != out {
		t.Fatalf("Safe(Safe(%q)) = %q, not %q", in, again, out)
	}
}

// TestSafeWhole tries every code point, every byte and every pair of bytes, on
// their own and beside an escape.
func TestSafeWhole(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		s := string(r)
		checkSafe(t, s, logview.Safe(s))
	}
	for a := range 256 {
		for b := range 256 {
			s := string([]byte{byte(a), byte(b)})
			checkSafe(t, s, logview.Safe(s))
			s = string([]byte{byte(a), 0x1b, '[', byte(b)})
			checkSafe(t, s, logview.Safe(s))
		}
	}
}

// TestSafeKeepsTypography: what the log's lines hold of typography, in hardn's
// own words and commands', is kept in both directions of the check.
func TestSafeKeepsTypography(t *testing.T) {
	for _, s := range []string{
		"don’t “quote” ≥ 5 → … x", "— – • ✓ ✗ ⚠ 日本語 ñ é ß Я", "/usr/bin/less -> /bin/busybox", "<tag> ^caret 0x9b U+202E",
	} {
		if got := logview.Safe(s); got != s {
			t.Errorf("Safe(%q) = %q", s, got)
		}
	}
}

// TestShow marks a cut line, after what is safe of it. A cut inside a
// character shows that character's bytes.
func TestShow(t *testing.T) {
	for _, tt := range []struct {
		line host.Line
		want string
	}{
		{host.Line{Text: []byte("one")}, "one"},
		{host.Line{Text: []byte("one"), Cut: true}, "one<cut>"},
		{host.Line{Text: []byte("\x1b[3"), Cut: true}, "^[[3<cut>"},
		{host.Line{Text: []byte("a\xe2\x80"), Cut: true}, "a<0xe2><0x80><cut>"},
		{host.Line{}, ""},
	} {
		if got := logview.Show(tt.line); got != tt.want {
			t.Errorf("Show(%q) = %q, want %q", tt.line.Text, got, tt.want)
		}
	}
	if strings.Contains(logview.Show(host.Line{Text: []byte("\x1b")}), "\x1b") {
		t.Error("an escape")
	}
}
