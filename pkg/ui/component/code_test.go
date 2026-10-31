package component_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	. "github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// TestCodeBlock: a CodeBlock's lines are code: no bullet, from theme.LabelCol,
// typographic characters kept in both glyph sets where a row's text maps them,
// what a terminal would act on dropped, a line too long breaking after
// Glyphs.Wrap with nothing cut; Warn in the warning's colour, the rest in the
// output's.
func TestCodeBlock(t *testing.T) {
	const typographic = "don’t “quote” ≥ 5 → … x"
	indent := strings.Repeat(" ", theme.LabelCol)
	for _, unicode := range []bool{true, false} {
		env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
		c := NewCtx(env)
		got := ansi.Strip(CodeBlock{Lines: []CodeLine{{Text: typographic}, {Text: "a\x1b[31mb\x07c"}, {}}}.Render(c))
		if want := indent + typographic + "\n" + indent + "abc\n"; got != want {
			t.Errorf("unicode %t: %q, want %q", unicode, got, want)
		}
		long := "word " + strings.Repeat("x", 100) + " tail"
		rows := CodeBlock{Lines: []CodeLine{{Text: long}}}.Rows(c)
		joined := ""
		for i, r := range rows {
			r = ansi.Strip(r)
			if w := ansi.StringWidth(r); w > theme.ContentWidth {
				t.Errorf("unicode %t: row %d is %d cells: %q", unicode, i, w, r)
			}
			r, ok := strings.CutPrefix(r, indent)
			if !ok {
				t.Errorf("unicode %t: row %d not at the label column: %q", unicode, i, r)
			}
			if i > 0 {
				if r, ok = strings.CutPrefix(r, c.Theme.Glyphs.Wrap); !ok {
					t.Errorf("unicode %t: row %d does not open with the wrap glyph: %q", unicode, i, r)
				}
			}
			joined += r
		}
		if joined != long {
			t.Errorf("unicode %t: %q, want every character of %q", unicode, joined, long)
		}
		if got, want := (CodeBlock{Lines: []CodeLine{{Text: "x"}}}).Render(c), indent+c.Theme.Output.Render("x"); got != want {
			t.Errorf("unicode %t: %q, want the output's colour, %q", unicode, got, want)
		}
		if got, want := (CodeBlock{Lines: []CodeLine{{Text: "x", Warn: true}}}).Render(c), indent+c.Theme.KeyValue.ValueWarn.Render("x"); got != want {
			t.Errorf("unicode %t: %q, want the warning's colour, %q", unicode, got, want)
		}
	}
	if got := (CodeBlock{}).Render(NewCtx(term.Fixed(80, colorprofile.TrueColor, true, true))); got != "" {
		t.Errorf("no lines: %q", got)
	}
}

// TestCodeLongLine: a line of any length breaks where ansi.Truncate says, every
// character kept, wide and combining characters too, and lays out in time its
// length: 64 KiB of bytes that are not UTF-8, as logview.Safe shows them, is 393,216
// cells, a hung draw when each row read all that was left of it.
func TestCodeLongLine(t *testing.T) {
	c := NewCtx(term.Fixed(80, colorprofile.TrueColor, true, true))
	indent := strings.Repeat(" ", theme.LabelCol)
	for _, unit := range []string{"x", "<0xff>", "界", "e\u0301", "\U0001F44D\U0001F3FD", "a" + strings.Repeat("\u0301", 200)} {
		s := strings.Repeat(unit, 500)
		rows := CodeBlock{Lines: []CodeLine{{Text: s}}}.Rows(c)
		if got, want := ansi.Strip(rows[0]), indent+ansi.Truncate(s, theme.ContentWidth-theme.LabelCol, ""); got != want {
			t.Errorf("%q: the first row %q, want %q", unit, got, want)
		}
		joined := ""
		for i, r := range rows {
			r = strings.TrimPrefix(ansi.Strip(r), indent)
			if i > 0 {
				r = strings.TrimPrefix(r, c.Theme.Glyphs.Wrap)
			}
			joined += r
		}
		if joined != s {
			t.Errorf("%q: not every character, %d bytes of %d", unit, len(joined), len(s))
		}
	}
	start := time.Now()
	rows := CodeBlock{Lines: []CodeLine{{Text: strings.Repeat("<0xff>", 64<<10)}}}.Rows(c)
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("%d rows took %s", len(rows), took)
	}
}

// TestWhole: under OffTerminal a value, a key and a bullet item stay whole on
// their line, however long, where NewCtx's, as every screen has it, cuts or wraps
// them; what it renders, through its Env's Writer, holds no escape sequence.
func TestWhole(t *testing.T) {
	long := "Intel(R) Xeon(R) Gold 6338 CPU @ 2.00GHz and a name that goes on past any row"
	bullet := "Can't read /a/path/that/goes/on/and/on/past/the/row/and/past/the/content/width/of/any/screen"
	key := "a-user-name-that-goes-on-and-on-past-the-value-column-and-past-the-content-width"
	rows := BulletList{Items: []KeyValue{{Key: "Model", Value: long}}}
	bullets := BulletList{Items: []KeyValue{{Key: bullet, State: ValueWarn}}}
	keys := BulletList{Items: []KeyValue{{Key: key, Value: "sudo"}}}
	off := OffTerminal()
	on := NewCtx(term.Fixed(80, colorprofile.TrueColor, false, true))
	if on.Whole {
		t.Error("NewCtx sets Whole")
	}
	var out bytes.Buffer
	if _, err := off.Env.Writer(&out).Write([]byte(rows.Render(off))); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); strings.ContainsRune(s, '\x1b') || !strings.Contains(s, long) || strings.Contains(s, "\n") {
		t.Errorf("off a terminal: %q", s)
	}
	for _, tt := range []struct {
		vm   BulletList
		text string
	}{{rows, long}, {bullets, bullet}, {keys, key}} {
		if s := ansi.Strip(tt.vm.Render(off)); !strings.Contains(s, tt.text) {
			t.Errorf("off a terminal, not whole: %q", s)
		}
		if s := ansi.Strip(tt.vm.Render(on)); strings.Contains(s, tt.text) {
			t.Errorf("on a terminal, whole: %q", s)
		}
	}
	if s := ansi.Strip(bullets.Render(off)); strings.Contains(s, "\n") {
		t.Errorf("a bullet item off a terminal wraps: %q", s)
	}
	badge := Badge{Text: long}
	if s := ansi.Strip(badge.Render(off)); !strings.Contains(s, long) {
		t.Errorf("a badge off a terminal, not whole: %q", s)
	}
	if s := ansi.Strip(badge.Render(on)); strings.Contains(s, long) {
		t.Errorf("a badge on a terminal, whole: %q", s)
	}
	if env := off.Env; env.Plain != true || env.TTY || env.HasColor() || env.Unicode || env.Width != term.DefaultWidth {
		t.Errorf("OffTerminal's Env: %+v", env)
	}
}
