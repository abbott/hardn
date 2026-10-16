package diff

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

const head = "--- a\n+++ b\n"

// TestUnified pins the output on the cases the format has rules for.
func TestUnified(t *testing.T) {
	// ten numbered lines, each with its newline
	var ten []string
	for i := 1; i <= 10; i++ {
		ten = append(ten, "l"+strconv.Itoa(i)+"\n")
	}
	lines := func(l []string) string { return strings.Join(l, "") }
	with := func(i int, s string) string {
		l := append([]string(nil), ten...)
		l[i] = s
		return lines(l)
	}
	tests := []struct {
		name, from, to string
		context        int
		want           string
	}{
		{"identical", "a\nb\n", "a\nb\n", 3, ""},
		{"both empty", "", "", 3, ""},
		{"from empty", "", "a\nb\n", 3, head + "@@ -0,0 +1,2 @@\n+a\n+b\n"},
		{"to empty", "a\nb\n", "", 3, head + "@@ -1,2 +0,0 @@\n-a\n-b\n"},
		{"one line", "a\n", "b\n", 3, head + "@@ -1 +1 @@\n-a\n+b\n"},
		{"three lines of context", lines(ten), with(4, "five\n"), 3,
			head + "@@ -2,7 +2,7 @@\n l2\n l3\n l4\n-l5\n+five\n l6\n l7\n l8\n"},
		{"changes six apart share a hunk", lines(ten), strings.Replace(with(8, "nine\n"), "l2\n", "two\n", 1), 3,
			head + "@@ -1,10 +1,10 @@\n l1\n-l2\n+two\n l3\n l4\n l5\n l6\n l7\n l8\n-l9\n+nine\n l10\n"},
		{"changes seven apart split", lines(ten), strings.Replace(with(9, "ten\n"), "l2\n", "two\n", 1), 3,
			head + "@@ -1,5 +1,5 @@\n l1\n-l2\n+two\n l3\n l4\n l5\n@@ -7,4 +7,4 @@\n l7\n l8\n l9\n-l10\n+ten\n"},
		{"added at the end", "a\nb\nc\n", "a\nb\nc\nd\n", 3, head + "@@ -1,3 +1,4 @@\n a\n b\n c\n+d\n"},
		{"no context: removed", "a\nb\nc\n", "a\nc\n", 0, head + "@@ -2 +1,0 @@\n-b\n"},
		{"no context: added", "a\nc\n", "a\nb\nc\n", 0, head + "@@ -1,0 +2 @@\n+b\n"},
		{"negative context is none", "a\nb\nc\n", "a\nc\n", -1, head + "@@ -2 +1,0 @@\n-b\n"},
		{"old has no newline", "a", "a\n", 3, head + "@@ -1 +1 @@\n-a\n" + NoNewline + "\n+a\n"},
		{"new has no newline", "a\n", "a", 3, head + "@@ -1 +1 @@\n-a\n+a\n" + NoNewline + "\n"},
		{"neither has one", "a\nb", "a\nc", 3,
			head + "@@ -1,2 +1,2 @@\n a\n-b\n" + NoNewline + "\n+c\n" + NoNewline + "\n"},
		{"a kept last line with no newline", "a\nb", "z\nb", 3,
			head + "@@ -1,2 +1,2 @@\n-a\n+z\n b\n" + NoNewline + "\n"},
		{"removals before additions", "a\nb\n", "c\nd\n", 3, head + "@@ -1,2 +1,2 @@\n-a\n-b\n+c\n+d\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnifiedContext("a", "b", tt.from, tt.to, tt.context)
			if got != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
			if tt.context == DefaultContext && Unified("a", "b", tt.from, tt.to) != got {
				t.Errorf("Unified differs from UnifiedContext with DefaultContext")
			}
			if out, err := apply(tt.from, got); err != nil || out != tt.to {
				t.Errorf("applying the diff gave %q, %v; want %q", out, err, tt.to)
			}
		})
	}
}

// TestApplyYieldsNew is the property check: for many generated pairs of
// texts, applying the diff to the old text gives the new one, at several
// context sizes; identical texts give no diff; and the diff is a shortest
// one, removing and adding only the lines outside a longest common
// subsequence. The generator is seeded, so every run checks the same
// pairs.
func TestApplyYieldsNew(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	text := func() string {
		var b strings.Builder
		for range r.IntN(12) {
			b.WriteString(string(rune('a' + r.IntN(4)))) // few letters, so lines repeat
			b.WriteString("\n")
		}
		s := b.String()
		if s != "" && r.IntN(3) == 0 {
			s = strings.TrimSuffix(s, "\n")
		}
		return s
	}
	for i := range 3000 {
		from, to := text(), text()
		if i%7 == 0 {
			to = from
		}
		for _, n := range []int{0, 1, 3} {
			u := UnifiedContext("a", "b", from, to, n)
			if (u == "") != (from == to) {
				t.Fatalf("pair %d, context %d: diff %q for %q and %q", i, n, u, from, to)
			}
			if got, err := apply(from, u); err != nil || got != to {
				t.Fatalf("pair %d, context %d: applying\n%s\nto %q gave %q, %v; want %q", i, n, u, from, got, err, to)
			}
		}
		a, b := split(from), split(to)
		edits := 0
		for _, l := range Classify(Unified("a", "b", from, to)) {
			if l.Kind == Removed || l.Kind == Added {
				edits++
			}
		}
		if want := len(a) + len(b) - 2*lcs(a, b); edits != want {
			t.Fatalf("pair %d: %d lines removed or added, want %d:\n%s", i, edits, want, Unified("a", "b", from, to))
		}
	}
}

// lcs returns the length of a longest common subsequence of a and b.
func lcs(a, b []string) int {
	row := make([]int, len(b)+1)
	for i := range a {
		prev := 0 // the row above, one column left
		for j := range b {
			cur := row[j+1]
			if a[i] == b[j] {
				row[j+1] = prev + 1
			} else {
				row[j+1] = max(row[j+1], row[j])
			}
			prev = cur
		}
	}
	return row[len(b)]
}

// TestManyEdits checks the fallback past maxEdits: texts with nothing in
// common give a correct diff, every old line removed and every new one
// added between the shared first and last lines.
func TestManyEdits(t *testing.T) {
	var a, b strings.Builder
	a.WriteString("first\n")
	b.WriteString("first\n")
	for i := range maxEdits {
		fmt.Fprintf(&a, "old %d\n", i)
		fmt.Fprintf(&b, "new %d\n", i)
	}
	a.WriteString("last\n")
	b.WriteString("last\n")
	u := Unified("a", "b", a.String(), b.String())
	if got, err := apply(a.String(), u); err != nil || got != b.String() {
		t.Fatalf("applying the diff failed: %v", err)
	}
	if want := fmt.Sprintf("@@ -1,%d +1,%d @@\n first\n-old 0\n", maxEdits+2, maxEdits+2); !strings.HasPrefix(u, head+want) {
		t.Errorf("diff starts %q, want %q", u[:min(len(u), 60)], head+want)
	}
}

// TestClassify checks each kind, including a removed line that looks like
// a file header and the markers after a hunk's last line.
func TestClassify(t *testing.T) {
	u := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,3 +1,2 @@ Match\n keep\n--- dashes\n-x\n\\ No newline at end of file\n+y\n\\ No newline at end of file\n"
	want := []Line{
		{Header, "diff --git a/f b/f"},
		{Header, "--- a/f"},
		{Header, "+++ b/f"},
		{Hunk, "@@ -1,3 +1,2 @@ Match"},
		{Context, " keep"},
		{Removed, "--- dashes"},
		{Removed, "-x"},
		{Marker, NoNewline},
		{Added, "+y"},
		{Marker, NoNewline},
	}
	got := Classify(u)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
	if got := Classify(""); got != nil {
		t.Errorf("Classify(\"\") = %v, want nil", got)
	}
	for _, s := range []string{"@@ -1 +1", "@@ -x +1 @@", "@@ -1,-1 +1 @@", "@@ 1 +1 @@"} {
		if got := Classify(s); got[0].Kind != Header {
			t.Errorf("Classify(%q) = %v, want a header", s, got)
		}
	}
}

// apply applies the unified diff u to text, checking every kept and
// removed line against text, newline included, and each hunk's counts, as
// patch would.
func apply(text, u string) (string, error) {
	src := split(text)
	var out []string
	next := 0 // the index in src of the next line not yet copied
	ls := Classify(u)
	for i := 0; i < len(ls); i++ {
		if ls[i].Kind != Hunk {
			continue
		}
		f := strings.Fields(ls[i].Text)
		start, _, _ := strings.Cut(f[1][1:], ",")
		from, _ := strconv.Atoi(start)
		olds, news, _ := hunkLens(ls[i].Text)
		if olds > 0 {
			from-- // a non-empty range names its first line, an empty one the line before
		}
		if from < next || from > len(src) {
			return "", fmt.Errorf("hunk %q starts at %d, after %d of %d lines", ls[i].Text, from, next, len(src))
		}
		out, next = append(out, src[next:from]...), from
		gotOld, gotNew := 0, 0
		for i+1 < len(ls) && ls[i+1].Kind != Hunk && ls[i+1].Kind != Header {
			i++
			l := ls[i]
			if l.Kind == Marker || l.Text == "" {
				continue
			}
			line := l.Text[1:] + "\n"
			if i+1 < len(ls) && ls[i+1].Kind == Marker {
				line = l.Text[1:] // the marker says the line has no newline
			}
			if l.Kind != Added {
				if next >= len(src) || src[next] != line {
					return "", fmt.Errorf("line %q does not match old line %d", l.Text, next+1)
				}
				gotOld, next = gotOld+1, next+1
			}
			if l.Kind != Removed {
				out, gotNew = append(out, line), gotNew+1
			}
		}
		if gotOld != olds || gotNew != news {
			return "", fmt.Errorf("hunk %q has %d old and %d new lines", ls[i].Text, gotOld, gotNew)
		}
	}
	return strings.Join(append(out, src[next:]...), ""), nil
}
