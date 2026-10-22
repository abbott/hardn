package config

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// The editor behind Edit (edit.go): hardn.yml's bytes indexed by line,
// each value found from the position yaml.v3 gives it and scanned to its
// end, and changed by splices; and the writing of a value Edit adds or
// replaces (next/s2-config-edit).

// editor edits src, hardn.yml's bytes without a byte order mark, by
// splices made at the positions yaml.v3 gives (Line, and Column in
// characters) and found by scanning the bytes from there.
type editor struct {
	src        []byte
	start, end []int  // each line's first byte, and the end of its text, before its line break
	nl         string // the file's line break: its first one's, "\n" for none
	keys       []string
	list       list // how the file writes a list, for a list it does not have yet
	splices    []splice
}

// splice replaces src[from:to] with text.
type splice struct {
	from, to int
	text     string
}

// span is where a value's text is: src[from:to], and, for a block
// scalar, the lines after its header (tail), and its last line; comment
// says that a flow collection holds a comment.
type span struct {
	from, to int
	last     int
	tail     splice
	comment  bool
}

// newEditor indexes src's lines, refusing what Edit does not take apart.
func newEditor(src []byte, keys []string) (*editor, error) {
	e := &editor{src: src, nl: "\n", keys: keys, list: list{offset: 2, space: 2}}
	if !utf8.Valid(src) {
		return nil, e.refuse("", "is not UTF-8 text")
	}
	if bytes.Contains(src, []byte("\u0085")) || bytes.Contains(src, []byte("\u2028")) || bytes.Contains(src, []byte("\u2029")) {
		return nil, e.refuse("", "has line breaks other than LF and CRLF")
	}
	first := 0
	for i, b := range src {
		switch {
		case b == '\r' && (i+1 == len(src) || src[i+1] != '\n'):
			return nil, e.refuse("", "has line breaks other than LF and CRLF")
		case b == '\n':
			end := i
			if i > 0 && src[i-1] == '\r' {
				end = i - 1
				if len(e.start) == 0 {
					e.nl = "\r\n"
				}
			}
			e.start, e.end = append(e.start, first), append(e.end, end)
			first = i + 1
		}
	}
	e.start, e.end = append(e.start, first), append(e.end, len(src))
	if n, err := documents(src); err != nil || n > 1 {
		return nil, e.refuse("", "holds more than one YAML document")
	}
	for l := range e.start {
		if t := e.text(l); strings.HasPrefix(t, "...") && (len(t) == 3 || t[3] == ' ' || t[3] == '\t') {
			return nil, e.refuse("", `has a "..." line`)
		}
	}
	return e, nil
}

// refuse is the EditError of the setting key ("" for the file) for why.
func (e *editor) refuse(key, why string) error {
	return &EditError{Key: key, Why: why, Keys: e.keys, Err: ErrNotEditable}
}

// unusual is the refusal of a value Edit cannot find the end of, or
// cannot write on one line: key's, named when it is a setting the save
// changes, else the file's.
func (e *editor) unusual(key string) error {
	if !slices.Contains(e.keys, key) {
		key = ""
	}
	return e.refuse(key, "cannot be edited in place")
}

// document edits the file from was, the configuration it gives, to want.
func (e *editor) document(was, want *yaml.Node) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(e.src, &doc); err != nil {
		return fmt.Errorf("config: %w: %w", ErrInvalidFile, err)
	}
	var root *yaml.Node
	if len(doc.Content) > 0 {
		root = doc.Content[0]
	}
	switch {
	case root == nil, root.Kind == yaml.ScalarNode && root.Tag == "!!null" && root.Value == "":
		// No setting: comments alone, a lone ---, or nothing.
		return e.appendKeys(was, want)
	case root.Kind != yaml.MappingNode:
		return e.refuse("", "does not hold its settings as key: value")
	}
	e.list = e.listStyle(root)
	return e.mapping(root, was, want)
}

// appendKeys adds the settings that changed, from no mapping, at the end
// of the file.
func (e *editor) appendKeys(was, want *yaml.Node) error {
	var lines []string
	for i := 0; i+1 < len(want.Content); i += 2 {
		name, v := want.Content[i].Value, want.Content[i+1]
		if same(valueOf(was, name), v) {
			continue
		}
		l, err := e.list.pair(name, v, 0)
		if err != nil {
			return err
		}
		lines = append(lines, l...)
	}
	text := strings.Join(lines, e.nl) + e.nl
	if n := len(e.src); n > 0 && e.src[n-1] != '\n' {
		text = e.nl + text
	}
	e.splices = append(e.splices, splice{len(e.src), len(e.src), text})
	return nil
}

// mapping edits m, a mapping of the file, from was to want: each key the
// file has whose value differs, then the keys it lacks, added at its end.
func (e *editor) mapping(m, was, want *yaml.Node) error {
	flow := m.Style&yaml.FlowStyle != 0
	at, merges := map[string]int{}, false
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		if k.Tag == "!!merge" {
			merges = true
		}
		if !flow && k.Column != m.Column {
			if before := e.src[e.start[k.Line-1]:e.pos(k)]; strings.TrimSpace(string(before)) == "?" {
				return e.refuse(k.Value, "uses an explicit key (?)")
			}
			return e.unusual(k.Value)
		}
		at[k.Value] = i
	}
	var added []int
	for i := 0; i+1 < len(want.Content); i += 2 {
		name, v := want.Content[i].Value, want.Content[i+1]
		w := valueOf(was, name)
		if same(w, v) {
			continue
		}
		if merges {
			return e.refuse("", "merges settings with <<")
		}
		j, ok := at[name]
		if !ok {
			added = append(added, i)
			continue
		}
		if err := e.pair(m.Content[j], m.Content[j+1], w, v, m.Column-1, flow); err != nil {
			return err
		}
	}
	if len(added) > 0 {
		return e.add(m, want, added, flow)
	}
	return nil
}

// pair edits the value val of key, at column k of a block mapping or in a
// flow one, from was to want.
func (e *editor) pair(key, val, was, want *yaml.Node, k int, flow bool) error {
	if anchored(key) || anchored(val) {
		return e.refuse(key.Value, "uses a YAML anchor or alias")
	}
	if want.Kind == yaml.MappingNode && val.Kind == yaml.MappingNode {
		return e.mapping(val, was, want)
	}
	if !flow {
		switch {
		case block(val) && val.Kind == yaml.SequenceNode && want.Kind == yaml.SequenceNode && len(want.Content) > 0:
			return e.items(key, val, was, want, k)
		case block(val):
			return e.collapse(key, val, want, k)
		case !e.list.flow && len(want.Content) > 0 && (val.Kind == yaml.ScalarNode && val.Tag == "!!null" ||
			val.Kind == yaml.SequenceNode && len(val.Content) == 0):
			return e.expand(key, val, want, k)
		}
	}
	sp, err := e.value(key, val, k, flow)
	if err != nil {
		return err
	}
	if sp.comment {
		return e.refuse(key.Value, "has comments inside its brackets")
	}
	text, err := inline(want, hintOf(val), flow)
	if err != nil {
		return e.unusual(key.Value)
	}
	if sp.from == sp.to { // nothing written after the colon
		text = " " + text
	}
	e.splices = append(e.splices, splice{sp.from, sp.to, text})
	if sp.tail.to > sp.tail.from {
		e.splices = append(e.splices, sp.tail)
	}
	return nil
}

// items edits seq, a block list of the file, from was to want, which is
// not empty: the items want keeps keep their lines, the comments above
// and beside them included; the others go, with theirs; the new ones take
// seq's dash, its indent and its first item's quoting, and a new mapping
// its first item's list style. The lines between key and the first dash,
// which speak of the list, stay.
func (e *editor) items(key, seq, was, want *yaml.Node, k int) error {
	n, h, dash := len(seq.Content), key.Line-1, seq.Column-1
	if len(was.Content) != n {
		return e.unusual(key.Value)
	}
	dashes, floor := make([]int, n), h
	for i, it := range seq.Content {
		l := it.Line - 1
		for l > floor && !e.dashAt(l, dash) {
			l--
		}
		if l <= floor {
			return e.unusual(key.Value)
		}
		dashes[i], floor = l, l
	}
	last := e.blockLast(h, k, dash == k)
	first, lasts := make([]int, n), make([]int, n)
	for i := range n {
		first[i], lasts[i] = dashes[0], last
		if i > 0 {
			first[i] = lasts[i-1] + 1
		}
		if i+1 < n {
			lasts[i] = e.contentBefore(dashes[i+1], dashes[i])
		}
	}
	style := list{offset: 2, space: 2, hint: hintOf(seq)}
	if it := seq.Content[0]; it.Line-1 == dashes[0] {
		style.space = it.Column - seq.Column
	}
	if it := seq.Content[0]; it.Kind == yaml.MappingNode {
		inner := e.listStyle(it)
		style.inner = &inner
	}
	var parts []string
	for _, o := range script(was.Content, want.Content) {
		if o.from >= 0 {
			parts = append(parts, string(e.src[e.start[first[o.from]]:e.end[lasts[o.from]]]))
			continue
		}
		lines, err := style.item(want.Content[o.to], dash)
		if err != nil {
			return e.unusual(key.Value)
		}
		parts = append(parts, strings.Join(lines, e.nl))
	}
	e.splices = append(e.splices, splice{e.start[dashes[0]], e.end[last], strings.Join(parts, e.nl)})
	return nil
}

// collapse replaces val, a block collection, with want written inline
// after key's colon ([] for an empty list), removing val's lines: the
// comments among them go with it; those after it stay.
func (e *editor) collapse(key, val, want *yaml.Node, k int) error {
	c, err := e.colon(key)
	if err != nil {
		return err
	}
	text, err := inline(want, nil, false)
	if err != nil {
		return e.unusual(key.Value)
	}
	last := e.blockLast(key.Line-1, k, val.Kind == yaml.SequenceNode && val.Column-1 == k)
	e.splices = append(e.splices, splice{c + 1, c + 1, " " + text}, splice{e.end[key.Line-1], e.end[last], ""})
	return nil
}

// expand replaces val, nothing or [], with want in the file's block list
// style, on the lines after it; a comment beside it stays on its line.
func (e *editor) expand(key, val, want *yaml.Node, k int) error {
	c, err := e.colon(key)
	if err != nil {
		return err
	}
	sp, err := e.value(key, val, k, false)
	if err != nil {
		return err
	}
	lines, err := e.list.block(want, k)
	if err != nil {
		return e.unusual(key.Value)
	}
	at := e.end[sp.last]
	e.splices = append(e.splices, splice{c + 1, sp.to, ""}, splice{at, at, e.nl + strings.Join(lines, e.nl)})
	return nil
}

// add adds to m the keys of want at added, at its end: in a block mapping
// on the lines after its last value and the comment lines indented past
// its keys that follow it (a list's commented-out items), at its keys'
// column; in a flow one after its last value, or inside its braces when
// it has none.
func (e *editor) add(m, want *yaml.Node, added []int, flow bool) error {
	if flow {
		var parts []string
		for _, i := range added {
			text, err := e.list.inline(want.Content[i+1], true)
			if err != nil {
				return e.unusual(want.Content[i].Value)
			}
			parts = append(parts, want.Content[i].Value+": "+text)
		}
		text := strings.Join(parts, ", ")
		if len(m.Content) == 0 {
			p := e.pos(m)
			if p >= len(e.src) || e.src[p] != '{' {
				return e.unusual(want.Content[added[0]].Value)
			}
			e.splices = append(e.splices, splice{p + 1, p + 1, text})
			return nil
		}
		sp, err := e.value(m.Content[len(m.Content)-2], m.Content[len(m.Content)-1], 0, true)
		if err != nil {
			return err
		}
		e.splices = append(e.splices, splice{sp.to, sp.to, ", " + text})
		return nil
	}
	k := m.Column - 1
	sp, err := e.value(m.Content[len(m.Content)-2], m.Content[len(m.Content)-1], k, false)
	if err != nil {
		return err
	}
	var lines []string
	for _, i := range added {
		l, err := e.list.pair(want.Content[i].Value, want.Content[i+1], k)
		if err != nil {
			return e.unusual(want.Content[i].Value)
		}
		lines = append(lines, l...)
	}
	last := sp.last
	for l := last + 1; l < len(e.start); l++ {
		if e.comment(l) && e.indent(l) > k {
			last = l
		} else if !e.blank(l) {
			break
		}
	}
	at := e.end[last]
	e.splices = append(e.splices, splice{at, at, e.nl + strings.Join(lines, e.nl)})
	return nil
}

// value is where val, key's value at column k (or in a flow mapping), is
// written.
func (e *editor) value(key, val *yaml.Node, k int, flow bool) (span, error) {
	p := e.pos(val)
	switch {
	case block(val):
		last := e.blockLast(key.Line-1, k, val.Kind == yaml.SequenceNode && val.Column-1 == k)
		return span{from: e.end[key.Line-1], to: e.end[last], last: last}, nil
	case val.Kind == yaml.SequenceNode || val.Kind == yaml.MappingNode:
		to, comment, err := e.flowEnd(p)
		if err != nil {
			return span{}, e.unusual(key.Value)
		}
		return span{from: p, to: to, last: e.lineOf(to - 1), comment: comment}, nil
	case val.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		h := val.Line - 1
		at := p
		if at < e.end[h] && e.src[at] == '!' { // a tag before the header goes with it
			at = e.token(at, e.end[h])
			for at < e.end[h] && (e.src[at] == ' ' || e.src[at] == '\t') {
				at++
			}
		}
		to := e.token(at, e.end[h])
		header := string(e.src[at:to])
		if !strings.HasPrefix(header, "|") && !strings.HasPrefix(header, ">") {
			return span{}, e.unusual(key.Value)
		}
		last := e.scalarLast(h, k, header)
		return span{from: p, to: to, last: last, tail: splice{e.end[h], e.end[last], ""}}, nil
	case val.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0:
		q := p
		for q < len(e.src) && e.src[q] != '"' && e.src[q] != '\'' {
			q++
		}
		to, err := e.quoted(q)
		if err != nil {
			return span{}, e.unusual(key.Value)
		}
		return span{from: p, to: to, last: e.lineOf(to - 1)}, nil
	case val.Tag == "!!null" && val.Value == "":
		return span{from: p, to: p, last: e.lineOf(p)}, nil
	}
	to := e.plainEnd(p, k, flow)
	return span{from: p, to: to, last: e.lineOf(max(to-1, p))}, nil
}

// colon is the offset of the colon after key.
func (e *editor) colon(key *yaml.Node) (int, error) {
	p, l := e.pos(key), key.Line-1
	i := p + len(key.Value)
	switch {
	case key.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0:
		end, err := e.quoted(p)
		if err != nil {
			return 0, e.unusual(key.Value)
		}
		i = end
	case key.Style != 0 || !bytes.HasPrefix(e.src[p:], []byte(key.Value)):
		return 0, e.unusual(key.Value)
	}
	for i < e.end[l] && (e.src[i] == ' ' || e.src[i] == '\t') {
		i++
	}
	if i >= e.end[l] || e.src[i] != ':' {
		return 0, e.unusual(key.Value)
	}
	return i, nil
}

// quoted is the offset after the quoted scalar whose opening quote is at
// q.
func (e *editor) quoted(q int) (int, error) {
	if q >= len(e.src) {
		return 0, ErrNotEditable
	}
	c := e.src[q]
	for i := q + 1; i < len(e.src); i++ {
		switch {
		case c == '"' && e.src[i] == '\\':
			i++
		case e.src[i] == c && c == '\'' && i+1 < len(e.src) && e.src[i+1] == '\'':
			i++
		case e.src[i] == c:
			return i + 1, nil
		}
	}
	return 0, ErrNotEditable
}

// flowEnd is the offset after the flow collection that starts at p, and
// whether a comment is inside it.
func (e *editor) flowEnd(p int) (int, bool, error) {
	depth, comment := 0, false
	for i := p; i < len(e.src); i++ {
		switch c := e.src[i]; {
		case c == '#' && i > 0 && isBlank(e.src[i-1]):
			comment = true
			for i+1 < len(e.src) && e.src[i+1] != '\n' {
				i++
			}
		case (c == '"' || c == '\'') && depth > 0 && e.tokenStart(i):
			end, err := e.quoted(i)
			if err != nil {
				return 0, false, err
			}
			i = end - 1
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			if depth--; depth == 0 {
				return i + 1, comment, nil
			}
		}
	}
	return 0, false, ErrNotEditable
}

// tokenStart reports whether the byte at i starts a token in a flow
// collection: the byte before it, blanks aside, is [ { , or :.
func (e *editor) tokenStart(i int) bool {
	for i--; i >= 0 && isBlank(e.src[i]); i-- {
	}
	return i >= 0 && strings.IndexByte("[{,:", e.src[i]) >= 0
}

// isBlank reports whether b is a space, a tab or a line break.
func isBlank(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }

// plainEnd is the offset after the plain scalar that starts at p, the
// value of a key at column k: before a comment, in a flow collection
// before , ] or }, and in a block over the lines indented past k that
// carry it on.
func (e *editor) plainEnd(p, k int, flow bool) int {
	l := e.lineOf(p)
	end, more := e.plainLine(p, e.end[l], flow)
	if flow || !more {
		return end
	}
	for l++; l < len(e.start); l++ {
		if e.blank(l) {
			continue
		}
		if e.comment(l) || e.indent(l) <= k {
			break
		}
		end, more = e.plainLine(e.start[l]+e.indent(l), e.end[l], false)
		if !more {
			break
		}
	}
	return end
}

// plainLine is where a plain scalar's text from p ends on its line, which
// ends at stop, and whether the scalar may go on on the next line: not
// after a comment, nor in a flow collection after , ] or }.
func (e *editor) plainLine(p, stop int, flow bool) (int, bool) {
	end := p
	for i := p; i < stop; i++ {
		c := e.src[i]
		if c == '#' && i > p && (e.src[i-1] == ' ' || e.src[i-1] == '\t') || flow && strings.IndexByte(",]}", c) >= 0 {
			return end, false
		}
		if c != ' ' && c != '\t' {
			end = i + 1
		}
	}
	return end, true
}

// blockLast is the last line of the block collection that is the value of
// a key on line h at column k: the lines after h indented past k, or for
// a list whose dashes are at k (dashes) its items, until a line that is
// not; blank and comment lines are skipped.
func (e *editor) blockLast(h, k int, dashes bool) int {
	last := h
	for l := h + 1; l < len(e.start); l++ {
		switch {
		case e.blank(l), e.comment(l):
		case e.indent(l) > k, dashes && e.dashAt(l, k):
			last = l
		default:
			return last
		}
	}
	return last
}

// scalarLast is the last line of the block scalar whose header, on line h,
// is header, the value of a key at column k: the lines after h indented to
// its text's column at least, which the header's indentation indicator
// gives (k and its digit) or else its first line that is not blank, until
// a line that is not; blank lines are skipped. A comment indented past k
// but less than its text is not its text: yaml.v3 gives it to the next key.
func (e *editor) scalarLast(h, k int, header string) int {
	in := -1
	if i := strings.IndexAny(header, "123456789"); i >= 0 {
		in = k + int(header[i]-'0')
	}
	last := h
	for l := h + 1; l < len(e.start); l++ {
		switch {
		case e.blank(l):
		case in < 0 && e.indent(l) > k:
			in, last = e.indent(l), l
		case in >= 0 && e.indent(l) >= in:
			last = l
		default:
			return last
		}
	}
	return last
}

// token is the offset after the bytes from p, before stop, that are not a
// space or a tab.
func (e *editor) token(p, stop int) int {
	for p < stop && e.src[p] != ' ' && e.src[p] != '\t' {
		p++
	}
	return p
}

// contentBefore is the last line before l, after floor, that is neither
// blank nor a comment; floor for none.
func (e *editor) contentBefore(l, floor int) int {
	for l--; l > floor; l-- {
		if !e.blank(l) && !e.comment(l) {
			return l
		}
	}
	return floor
}

// text is line l's text.
func (e *editor) text(l int) string { return string(e.src[e.start[l]:e.end[l]]) }

// indent is the number of spaces line l starts with.
func (e *editor) indent(l int) int {
	t := e.text(l)
	return len(t) - len(strings.TrimLeft(t, " "))
}

// blank reports whether line l holds blanks alone.
func (e *editor) blank(l int) bool { return strings.TrimLeft(e.text(l), " \t") == "" }

// comment reports whether line l holds a comment alone.
func (e *editor) comment(l int) bool {
	return strings.HasPrefix(strings.TrimLeft(e.text(l), " \t"), "#")
}

// dashAt reports whether line l holds a list item's dash at column c.
func (e *editor) dashAt(l, c int) bool {
	t := e.text(l)
	return e.indent(l) == c && len(t) > c && t[c] == '-' && (len(t) == c+1 || t[c+1] == ' ' || t[c+1] == '\t')
}

// lineOf is the line of offset p.
func (e *editor) lineOf(p int) int {
	l, ok := slices.BinarySearch(e.start, p)
	if !ok {
		l--
	}
	return max(l, 0)
}

// pos is the offset of n, from its line and its column in characters.
func (e *editor) pos(n *yaml.Node) int {
	l := min(max(n.Line-1, 0), len(e.start)-1)
	p := e.start[l]
	for c := 1; c < n.Column && p < e.end[l]; c++ {
		_, size := utf8.DecodeRune(e.src[p:])
		p += size
	}
	return p
}

// apply is src with every splice made, refusing two that overlap.
func (e *editor) apply() ([]byte, error) {
	slices.SortStableFunc(e.splices, func(a, b splice) int { return cmp.Compare(a.from, b.from) })
	var out bytes.Buffer
	at := 0
	for _, s := range e.splices {
		if s.from < at || s.to < s.from {
			return nil, &EditError{Why: "cannot be edited safely", Keys: e.keys, Err: ErrEditUnsafe}
		}
		out.Write(e.src[at:s.from])
		out.WriteString(s.text)
		at = s.to
	}
	out.Write(e.src[at:])
	return out.Bytes(), nil
}

// listStyle is how root's first list is written: in block style, its dash
// offset from its key and its items' quoting; or in brackets, when the
// file has no block list and a list in brackets with an item. A file with
// neither gets two spaces before the dash and one after (hardn.yml.example's).
func (e *editor) listStyle(root *yaml.Node) list {
	var flow *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		v := root.Content[i+1]
		switch {
		case v.Kind != yaml.SequenceNode || len(v.Content) == 0:
		case block(v):
			s := list{offset: v.Column - root.Content[i].Column, space: 2, hint: hintOf(v)}
			if it := v.Content[0]; it.Line == v.Line {
				s.space = it.Column - v.Column
			}
			return s
		case flow == nil:
			flow = v
		}
	}
	if flow != nil {
		return list{flow: true, offset: 2, space: 2, hint: hintOf(flow)}
	}
	if root.Style&yaml.FlowStyle != 0 {
		return list{flow: true, offset: 2, space: 2}
	}
	return list{offset: 2, space: 2}
}

// list is how a list is written: in brackets (flow), or in block style,
// its dash offset columns right of its key's and space columns before
// its item; its string items quoted as hint is; and the lists of its
// items that are mappings as inner is (nil: two spaces, a dash, one).
type list struct {
	flow          bool
	offset, space int
	hint          *yaml.Node
	inner         *list
}

// inline is v on one line, a list's items quoted as the style's; inside
// a flow mapping (flow) as a value there.
func (s list) inline(v *yaml.Node, flow bool) (string, error) {
	if v.Kind == yaml.SequenceNode {
		return inline(v, s.hint, flow)
	}
	return inline(v, nil, flow)
}

// pair is name: v as lines, the key at column k.
func (s list) pair(name string, v *yaml.Node, k int) ([]string, error) {
	pad := strings.Repeat(" ", k)
	if s.flow || v.Kind == yaml.ScalarNode || len(v.Content) == 0 {
		text, err := s.inline(v, false)
		return []string{pad + name + ": " + text}, err
	}
	lines, err := s.block(v, k)
	return append([]string{pad + name + ":"}, lines...), err
}

// block is v's lines as the block value of a key at column k: a list's
// items, a mapping's keys two columns in.
func (s list) block(v *yaml.Node, k int) ([]string, error) {
	var out []string
	if v.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(v.Content); i += 2 {
			lines, err := s.pair(v.Content[i].Value, v.Content[i+1], k+2)
			if err != nil {
				return nil, err
			}
			out = append(out, lines...)
		}
		return out, nil
	}
	for _, it := range v.Content {
		lines, err := s.item(it, k+s.offset)
		if err != nil {
			return nil, err
		}
		out = append(out, lines...)
	}
	return out, nil
}

// item is it as a list item whose dash is at column d; a mapping without
// its keys whose values are empty, false or 0, which an item read without
// them has (a default is not written), and its lists as s.inner.
func (s list) item(it *yaml.Node, d int) ([]string, error) {
	dash := strings.Repeat(" ", d) + "-" + strings.Repeat(" ", max(s.space-1, 1))
	if it.Kind == yaml.MappingNode {
		m := *it
		m.Content = nil
		for i := 0; i+1 < len(it.Content); i += 2 {
			if !zero(it.Content[i+1]) {
				m.Content = append(m.Content, it.Content[i], it.Content[i+1])
			}
		}
		it = &m
	}
	if it.Kind == yaml.MappingNode && len(it.Content) > 0 {
		inner := list{offset: 2, space: 2}
		if s.inner != nil {
			inner = *s.inner
		}
		lines, err := inner.block(it, d+max(s.space, 2)-2)
		if err != nil {
			return nil, err
		}
		lines[0] = dash + strings.TrimLeft(lines[0], " ")
		return lines, nil
	}
	text, err := inline(it, s.hint, false)
	return []string{dash + text}, err
}

// zero reports whether n, a canonical value, is its type's zero value: an
// empty string, false, 0, null, or an empty list or mapping.
func zero(n *yaml.Node) bool {
	if n.Kind != yaml.ScalarNode {
		return len(n.Content) == 0
	}
	switch n.Tag {
	case "!!str", "!!null":
		return n.Value == ""
	case "!!bool":
		return n.Value == "false"
	case "!!int":
		return n.Value == "0"
	}
	return false
}

// inline is v written on one line: a collection in brackets, a string in
// hint's quotes when hint is a quoted string, or in double quotes when it
// would take more than one line; inside a flow mapping (flow), as yaml.v3
// writes it there, where a plain string may not hold , [ ] { }.
func inline(v, hint *yaml.Node, flow bool) (string, error) {
	c := flowCopy(v, hint)
	n := c
	if flow {
		n = &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "k"}, c}}
	}
	text, err := encode(n)
	if err == nil && c.Kind == yaml.ScalarNode && strings.Contains(text, "\n") {
		c.Style = yaml.DoubleQuotedStyle
		text, err = encode(n)
	}
	if err == nil && strings.Contains(text, "\n") {
		err = ErrNotEditable
	}
	if err == nil && flow {
		t, ok := strings.CutPrefix(text, "{k: ")
		t, end := strings.CutSuffix(t, "}")
		if !ok || !end {
			return "", ErrNotEditable
		}
		text = t
	}
	return text, err
}

// flowCopy is a copy of v with its collections in flow style and its
// strings quoted as hint is.
func flowCopy(v, hint *yaml.Node) *yaml.Node {
	c := *v
	if v.Kind == yaml.ScalarNode {
		if q := quotes(hint); q != 0 && c.Tag == "!!str" {
			c.Style = q
		}
		return &c
	}
	c.Style, c.Content = yaml.FlowStyle, make([]*yaml.Node, len(v.Content))
	for i, x := range v.Content {
		h := hint
		if v.Kind == yaml.MappingNode {
			h = nil
		}
		c.Content[i] = flowCopy(x, h)
	}
	return &c
}

// encode is n in YAML, without its last line break.
func encode(n *yaml.Node) (string, error) {
	out, err := yaml.Marshal(n)
	return strings.TrimSuffix(string(out), "\n"), err
}

// quotes is hint's quoting: yaml.DoubleQuotedStyle, SingleQuotedStyle, or
// 0 for none or no hint.
func quotes(hint *yaml.Node) yaml.Style {
	if hint == nil || hint.Kind != yaml.ScalarNode || hint.Tag != "!!str" {
		return 0
	}
	return hint.Style & (yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle)
}

// hintOf is the scalar whose quoting a value replacing n takes: n, or a
// list's first item.
func hintOf(n *yaml.Node) *yaml.Node {
	if n.Kind == yaml.SequenceNode && len(n.Content) > 0 {
		return hintOf(n.Content[0])
	}
	if quotes(n) == 0 {
		return nil
	}
	return n
}

// block reports whether n is a collection in block style.
func block(n *yaml.Node) bool {
	return (n.Kind == yaml.SequenceNode || n.Kind == yaml.MappingNode) && n.Style&yaml.FlowStyle == 0
}

// anchored reports whether n, or a node under it, has an anchor or is an
// alias.
func anchored(n *yaml.Node) bool {
	if n.Anchor != "" || n.Kind == yaml.AliasNode {
		return true
	}
	return slices.ContainsFunc(n.Content, anchored)
}

// step is one step of a list's edit: item from of the file's list kept as
// item to of the new one, or (from < 0) item to added.
type step struct{ from, to int }

// script is the edit of list a into list b that keeps the most items, in
// order (a longest common subsequence): b's items, each kept from a or
// added.
func script(a, b []*yaml.Node) []step {
	eq := make([][]bool, len(a))
	for i := range a {
		eq[i] = make([]bool, len(b))
		for j := range b {
			eq[i][j] = same(a[i], b[j])
		}
	}
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if eq[i][j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []step
	for i, j := 0, 0; j < len(b); {
		switch {
		case i < len(a) && eq[i][j]:
			out = append(out, step{i, j})
			i, j = i+1, j+1
		case i < len(a) && lcs[i+1][j] >= lcs[i][j+1]:
			i++
		default:
			out = append(out, step{-1, j})
			j++
		}
	}
	return out
}
