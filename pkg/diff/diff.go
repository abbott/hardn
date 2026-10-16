// Package diff computes line diffs and writes them in the unified format
// of diff -u and git diff, with nothing beyond the standard library. The
// plan view shows a file change's diff (brief 1B PlanView), and the
// plan/apply engine's Change.Diff returns one (refactor plan p3-engine).
//
// Every function is pure and deterministic: the same inputs give the same
// output, with no I/O, clock or environment.
package diff

import (
	"slices"
	"strconv"
	"strings"
)

// DefaultContext is the number of unchanged lines kept around each change,
// as diff -u and git diff keep them.
const DefaultContext = 3

// NoNewline is the marker line that follows a line with no newline after
// it, which can only be the last line of a text.
const NoNewline = `\ No newline at end of file`

// maxEdits bounds the edit distance the diff searches for. Past it, the
// lines between the common start and end are shown as all removed, then
// all added: still a correct diff, though not the shortest. It keeps the
// search's memory near maxEdits² ints for texts that share almost nothing.
const maxEdits = 1000

// op is one line of an edit script: kept (' '), removed ('-') or added
// ('+'), with its newline when it has one.
type op struct {
	kind byte
	text string
}

// Unified returns the unified diff that turns oldText into newText, under
// the headers "--- oldName" and "+++ newName", with DefaultContext lines
// of context. Identical texts give the empty string.
func Unified(oldName, newName, oldText, newText string) string {
	return UnifiedContext(oldName, newName, oldText, newText, DefaultContext)
}

// UnifiedContext is Unified with context lines of context around each
// change; a negative context is 0, and one longer than both texts, such
// as math.MaxInt for the whole file, keeps every line. Changes at most
// twice the context apart share a hunk, as in diff -u. Each hunk header is
// "@@ -start,count +start,count @@": a count of 1 is left out, and an
// empty range starts at the line before it, 0 at the top. A line that ends
// its text with no newline is followed by NoNewline.
func UnifiedContext(oldName, newName, oldText, newText string, context int) string {
	if oldText == newText {
		return ""
	}
	ops := edits(split(oldText), split(newText))
	// No hunk reaches past the edit script, so a longer context changes
	// nothing; clamping it keeps next+context and 2*context from
	// overflowing.
	context = min(max(0, context), len(ops))
	var b strings.Builder
	b.WriteString("--- " + oldName + "\n+++ " + newName + "\n")
	// at[i] is the number of old and new lines before ops[i].
	type pos struct{ old, new int }
	at := make([]pos, len(ops)+1)
	for i, o := range ops {
		at[i+1] = at[i]
		if o.kind != '+' {
			at[i+1].old++
		}
		if o.kind != '-' {
			at[i+1].new++
		}
	}
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		// A hunk runs from context lines before its first change to
		// context lines after its last, taking in every change whose gap
		// from the one before is at most twice the context.
		start, end := max(0, i-context), i
		for end < len(ops) {
			next := end
			for next < len(ops) && ops[next].kind != ' ' {
				next++
			}
			gap := next
			for gap < len(ops) && ops[gap].kind == ' ' {
				gap++
			}
			if gap == len(ops) || gap-next > 2*context {
				end = min(len(ops), next+context)
				break
			}
			end = gap
		}
		b.WriteString("@@ -" + span(at[start].old, at[end].old) + " +" + span(at[start].new, at[end].new) + " @@\n")
		for _, o := range ops[start:end] {
			text, newline := strings.CutSuffix(o.text, "\n")
			b.WriteString(string(o.kind) + text + "\n")
			if !newline {
				b.WriteString(NoNewline + "\n")
			}
		}
		i = end
	}
	return b.String()
}

// span returns a hunk header's range for the lines from index from to
// index to: "start,count", with a count of 1 left out, and an empty range
// starting at the line before it.
func span(from, to int) string {
	switch n := to - from; n {
	case 0:
		return strconv.Itoa(from) + ",0"
	case 1:
		return strconv.Itoa(from + 1)
	default:
		return strconv.Itoa(from+1) + "," + strconv.Itoa(n)
	}
}

// split returns s's lines, each with its newline; the last one has none
// when s does not end with one. An empty s has no lines.
func split(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// edits returns the edit script that turns a into b: the lines they share
// at the start and end are kept, and the rest is the shortest script
// Myers' O(ND) algorithm finds, or everything removed then added when that
// would take more than maxEdits edits.
func edits(a, b []string) []op {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var out []op
	for _, l := range a[:pre] {
		out = append(out, op{' ', l})
	}
	out = append(out, myers(a[pre:len(a)-suf], b[pre:len(b)-suf])...)
	for _, l := range a[len(a)-suf:] {
		out = append(out, op{' ', l})
	}
	return out
}

// myers returns the shortest edit script from a to b (E. Myers, "An O(ND)
// Difference Algorithm and Its Variations", 1986). On each diagonal k it
// prefers the path from k+1, an addition, when that one reaches further,
// so that removals come before additions in a changed run.
func myers(a, b []string) []op {
	n, m := len(a), len(b)
	// v[off+k] is the furthest x reached on diagonal k = x - y. Each round
	// d saves the diagonals -d-1 to d+1 of v as they stood before it, all
	// that backtracking from round d reads.
	off := n + m + 1
	v := make([]int, 2*off+1)
	var trace [][]int
	for d := 0; ; d++ {
		if d > maxEdits {
			return replace(a, b)
		}
		trace = append(trace, slices.Clone(v[off-d-1:off+d+2]))
		for k := -d; k <= d; k += 2 {
			x := v[off+k-1] + 1
			if k == -d || k != d && v[off+k-1] < v[off+k+1] {
				x = v[off+k+1]
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			v[off+k] = x
			if x >= n && y >= m {
				return backtrack(a, b, trace)
			}
		}
	}
}

// backtrack walks the saved rounds of myers back from the end of both
// texts, and returns the edit script in order.
func backtrack(a, b []string, trace [][]int) []op {
	var out []op
	x, y := len(a), len(b)
	for d := len(trace) - 1; d >= 0; d-- {
		v := func(k int) int { return trace[d][k+d+1] }
		k := x - y
		prev := k - 1
		if k == -d || k != d && v(k-1) < v(k+1) {
			prev = k + 1
		}
		px := v(prev)
		py := px - prev
		for x > px && y > py {
			x, y = x-1, y-1
			out = append(out, op{' ', a[x]})
		}
		if d > 0 {
			if x == px {
				out = append(out, op{'+', b[py]})
			} else {
				out = append(out, op{'-', a[px]})
			}
		}
		x, y = px, py
	}
	slices.Reverse(out)
	return out
}

// replace returns the edit script that removes every line of a, then adds
// every line of b.
func replace(a, b []string) []op {
	out := make([]op, 0, len(a)+len(b))
	for _, l := range a {
		out = append(out, op{'-', l})
	}
	for _, l := range b {
		out = append(out, op{'+', l})
	}
	return out
}

// Kind is what one line of a unified diff is.
type Kind int

// The kinds of line in a unified diff. Header is any line outside a hunk:
// the "---" and "+++" file headers, and anything else a tool writes there.
const (
	Header Kind = iota
	Hunk
	Context
	Removed
	Added
	Marker
)

// Line is one line of a unified diff, without its newline.
type Line struct {
	Kind Kind
	Text string
}

// Classify returns the lines of the unified diff text u, each with its
// kind. A hunk's lines are told apart by its header's counts, so a removed
// line that starts "--" is Removed, not a header. A line in a hunk with no
// marker, as some tools leave an empty context line, is Context; a "\"
// line is Marker. A final newline ends the last line rather than starting
// another.
func Classify(u string) []Line {
	if u == "" {
		return nil
	}
	var out []Line
	// olds and news are the lines the current hunk has still to show.
	olds, news := 0, 0
	for _, t := range strings.Split(strings.TrimSuffix(u, "\n"), "\n") {
		kind := Header
		switch {
		case olds > 0 || news > 0:
			switch {
			case strings.HasPrefix(t, `\`):
				kind = Marker
			case strings.HasPrefix(t, "-"):
				kind, olds = Removed, olds-1
			case strings.HasPrefix(t, "+"):
				kind, news = Added, news-1
			default:
				kind, olds, news = Context, olds-1, news-1
			}
		case strings.HasPrefix(t, `\`) && len(out) > 0 && out[len(out)-1].Kind != Header:
			kind = Marker
		default:
			if o, n, ok := hunkLens(t); ok {
				kind, olds, news = Hunk, o, n
			}
		}
		out = append(out, Line{kind, t})
	}
	return out
}

// hunkLens returns the old and new line counts of a hunk header,
// "@@ -l[,s] +l[,s] @@" with any text after it; ok is false when t is not
// one.
func hunkLens(t string) (olds, news int, ok bool) {
	f := strings.Fields(t)
	if len(f) < 4 || f[0] != "@@" || f[3] != "@@" || !strings.HasPrefix(f[1], "-") || !strings.HasPrefix(f[2], "+") {
		return 0, 0, false
	}
	count := func(r string) (int, bool) {
		start, n, found := strings.Cut(r[1:], ",")
		if _, err := strconv.Atoi(start); err != nil {
			return 0, false
		}
		if !found {
			return 1, true
		}
		c, err := strconv.Atoi(n)
		return c, err == nil && c >= 0
	}
	olds, ok1 := count(f[1])
	news, ok2 := count(f[2])
	return olds, news, ok1 && ok2
}
