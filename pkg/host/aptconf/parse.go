package aptconf

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/host/detect"
)

type kind uint8

const (
	word   kind = iota + 1 // a name: A::B
	quoted                 // "a value"
	open                   // {
	closer                 // }
	semi                   // ;
)

type token struct {
	kind kind
	text string
	line int
}

// lex splits an apt.conf file into tokens (apt.conf(5) SYNTAX). Outside a
// quoted value, `//` and `#` start a comment to the end of the line and
// `/* */` one to its close; inside one, they are the value's. A value is
// one line, with no quote in it. #clear and #include are refused: no VM's
// file uses either (02-inspect-c-*.log), so their reading is not built.
func lex(file, text string) ([]token, error) {
	var toks []token
	line := 1
	bad := func(what string) error {
		return fmt.Errorf("%s:%d: %s: %w", file, line, what, detect.ErrUnrecognised)
	}
	for i := 0; i < len(text); {
		c, rest := text[i], text[i:]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			i++
		case strings.HasPrefix(rest, "#clear") || strings.HasPrefix(rest, "#include"):
			return nil, bad("#clear and #include are not read")
		case c == '#' || strings.HasPrefix(rest, "//"):
			if j := strings.IndexByte(rest, '\n'); j >= 0 {
				i += j
			} else {
				i = len(text)
			}
		case strings.HasPrefix(rest, "/*"):
			j := strings.Index(rest[2:], "*/")
			if j < 0 {
				return nil, bad("a /* comment is not closed")
			}
			line += strings.Count(rest[:j+4], "\n")
			i += j + 4
		case c == '"':
			j := strings.IndexAny(rest[1:], "\"\n")
			if j < 0 || rest[1+j] == '\n' {
				return nil, bad("a quoted value is not closed on its line")
			}
			toks = append(toks, token{quoted, rest[1 : 1+j], line})
			i += j + 2
		case c == '{':
			toks = append(toks, token{open, "{", line})
			i++
		case c == '}':
			toks = append(toks, token{closer, "}", line})
			i++
		case c == ';':
			toks = append(toks, token{semi, ";", line})
			i++
		default:
			j := 0
			for j < len(rest) && !strings.ContainsRune(" \t\r\n\f\v\"{};#", rune(rest[j])) &&
				!strings.HasPrefix(rest[j:], "//") && !strings.HasPrefix(rest[j:], "/*") {
				j++
			}
			toks = append(toks, token{word, rest[:j], line})
			i += j
		}
	}
	return toks, nil
}

// parse reads a file's statements (apt.conf(5) SYNTAX): `A::B "v";`, a
// block `A { ... };` whose names are under A, and, in a block, a list item
// `"v";`. A value must be quoted. Anything else (an unquoted value, a name
// with an empty tag, a name ending "::", an item outside a block, a brace
// out of place, a block not closed) is refused: the VMs' files use none of
// it (02-inspect-c-*.log), and how apt reads it is [unverified].
func parse(file, text string) ([]stmt, error) {
	toks, err := lex(file, text)
	if err != nil {
		return nil, err
	}
	var (
		stmts []stmt
		stack [][]string // each open block's full path
	)
	bad := func(t token, what string) error {
		return fmt.Errorf("%s:%d: %s: %w", file, t.line, what, detect.ErrUnrecognised)
	}
	next := func(i int) token {
		if i < len(toks) {
			return toks[i]
		}
		return token{line: toks[len(toks)-1].line}
	}
	prefix := func() []string {
		if len(stack) == 0 {
			return nil
		}
		return stack[len(stack)-1]
	}
	for i := 0; i < len(toks); {
		t := toks[i]
		switch t.kind {
		case word:
			segs, err := split(t.text)
			if err != nil {
				return nil, bad(t, fmt.Sprintf("name %q", t.text))
			}
			full := append(append([]string{}, prefix()...), segs...)
			switch n := next(i + 1); n.kind {
			case quoted:
				if next(i+2).kind != semi {
					return nil, bad(n, fmt.Sprintf("%s's value is not followed by ;", t.text))
				}
				stmts = append(stmts, stmt{path: full, value: n.text})
				i += 3
			case open:
				stack = append(stack, full)
				i += 2
			default:
				return nil, bad(t, fmt.Sprintf("%s is not followed by a quoted value or {", t.text))
			}
		case quoted:
			if len(stack) == 0 {
				return nil, bad(t, "a list item outside a block")
			}
			if next(i+1).kind != semi {
				return nil, bad(t, "a list item is not followed by ;")
			}
			stmts = append(stmts, stmt{path: prefix(), value: t.text, item: true})
			i += 2
		case closer:
			if len(stack) == 0 {
				return nil, bad(t, "} with no block open")
			}
			stack = stack[:len(stack)-1]
			i++
			if next(i).kind == semi {
				i++
			}
		default:
			return nil, bad(t, fmt.Sprintf("%q out of place", t.text))
		}
	}
	if len(stack) > 0 {
		return nil, fmt.Errorf("%s: a block is not closed: %w", file, detect.ErrUnrecognised)
	}
	return stmts, nil
}
