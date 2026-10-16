package keys

import (
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// numbered returns one binding per key from lo to hi, as a menu's items
// give them: no help, since the label says what each does.
func numbered(lo, hi int) Set {
	var s Set
	for n := lo; n <= hi; n++ {
		s = append(s, Binding{Keys: []string{strconv.Itoa(n)}})
	}
	return s
}

var (
	quit = Quit()
	back = Back()
)

func TestHint(t *testing.T) {
	tests := []struct {
		name string
		set  Set
		want string
	}{
		{"empty", nil, ""},
		{"no keys", Set{{Help: "quit"}, {Keys: []string{""}}}, ""},
		{"a single key", Set{quit}, "q"},
		{"a single number", numbered(1, 1), "1"},
		{"two letters", Set{{Keys: []string{"y"}}, {Keys: []string{"n"}}}, "y or n"},
		{"Figma's main menu", append(numbered(0, 10), quit), "0-10 or q"},
		{"Figma's sub-menus", append(numbered(0, 2), quit), "0-2 or q"},
		{"two numbers make a range", append(numbered(0, 1), quit), "0-1 or q"},
		{"a gap", append(numbered(1, 3), Binding{Keys: []string{"5"}}), "1-3 or 5"},
		{"a gap and a letter", append(append(numbered(1, 3), Binding{Keys: []string{"5"}}), quit), "1-3, 5 or q"},
		{"gaps either side", Set{{Keys: []string{"0"}}, {Keys: []string{"2", "3"}}, {Keys: []string{"7"}}, quit}, "0, 2-3, 7 or q"},
		{"two-digit keys", append(numbered(9, 12), quit), "9-12 or q"},
		{"two-digit keys apart", Set{{Keys: []string{"10"}}, {Keys: []string{"12"}}}, "10 or 12"},
		{"numbers in order, whatever the binding order", Set{quit, {Keys: []string{"10", "2"}}, {Keys: []string{"1", "0"}}}, "0-2, 10 or q"},
		{"letters in the order they first appear", Set{quit, back}, "q or b"},
		{"a prompt's own inputs first", Set{quit, {Keys: []string{"web-01"}}, {Keys: []string{"3"}}}, "3, web-01 or q"},
		{"a shared number after them", Set{{Keys: []string{"web-01"}}, {Keys: []string{"0", "b"}, Help: "go back"}, quit}, "web-01, 0, b or q"},
		{"a shared key the prompt has is shown once", append(numbered(0, 10), Binding{Keys: []string{"0", "b"}, Help: "go back"}), "0-10 or b"},
		{"a question before the shared numbers", append(YesNo(false), Binding{Keys: []string{"0", "b"}, Help: "go back"}, quit), "y/N, 0, b or q"},
		{"the navigation grammar's example", append(numbered(0, 10), Navigation()...), "0-10, b or q"},
		{"the navigation inputs", Navigation(), "b or q"},
		{"a y/N question", YesNo(false), "y/N"},
		{"a Y/n question", YesNo(true), "Y/n"},
		{"a question at a prompt", append(YesNo(false), Navigation()...), "y/N, b or q"},
		{"a question's answers in order", Set{{Keys: []string{"n"}, Default: true}, {Keys: []string{"y"}}}, "N/y"},
		{"an answer's first key", Set{{Keys: []string{"", "yes", "y"}, Default: true}, {Keys: []string{"no"}}}, "YES/no"},
		{"only the first default", Set{{Keys: []string{"a"}, Default: true}, {Keys: []string{"b"}, Default: true}}, "A/b"},
		{"a default with help is an answer", Set{{Keys: []string{"y"}, Help: "confirm", Default: true}, {Keys: []string{"n"}}}, "Y/n"},
		{"only a question joins its answers", Set{{Keys: []string{"y", "yes"}}, {Keys: []string{"n"}}}, "y, yes or n"},
		{"each key once", Set{quit, quit, {Keys: []string{"1", "1"}}}, "1 or q"},
		{"a word", Set{{Keys: []string{"yes"}}, {Keys: []string{"no"}}}, "yes or no"},
		{"not canonical numbers are kept as typed", Set{{Keys: []string{"01", "+2", "-3", "3"}}}, "3, 01, +2 or -3"},
		{"a number too big for int is a word", Set{{Keys: []string{"99999999999999999999", "1"}}}, "1 or 99999999999999999999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.set.Hint(); got != tt.want {
				t.Errorf("Hint() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHelp(t *testing.T) {
	tests := []struct {
		name string
		set  Set
		want string
	}{
		{"empty", nil, ""},
		{"the brief's exit wording", Set{quit}, "q to quit"},
		{"the navigation inputs", Navigation(), "b to go back, q to quit"},
		{"a question's answers have no help", append(YesNo(false), Navigation()...), "b to go back, q to quit"},
		{"two keys", Set{{Keys: []string{"0", "b"}, Help: "go back"}}, "0 or b to go back"},
		{"menu items have no help", append(numbered(0, 10), quit), "q to quit"},
		{"a range of keys", Set{{Keys: []string{"3", "1", "2"}, Help: "choose a key"}}, "1-3 to choose a key"},
		{"no keys", Set{{Help: "quit"}, {Keys: []string{""}, Help: "quit"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.set.Help(); got != tt.want {
				t.Errorf("Help() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatch(t *testing.T) {
	menu := append(numbered(0, 10), Navigation()...)
	ask := append(YesNo(false), Navigation()...)
	tests := []struct {
		name string
		set  Set
		line string
		want int // -1: no match
	}{
		{"a number", menu, "3", 3},
		{"a two-digit number", menu, "10", 10},
		{"surrounding white space", menu, " \t10 ", 10},
		{"a letter", menu, "q", 12},
		{"a capital letter", menu, "B", 11},
		{"numbers compare as typed", menu, "01", -1},
		{"a number not offered", menu, "11", -1},
		{"an empty line with no default", menu, "", -1},
		{"white space with no default", menu, "  ", -1},
		{"a word", menu, "quit", -1},
		{"two letters", menu, "bq", -1},
		{"y", ask, "y", 0},
		{"YES", ask, "YES", 0},
		{"No", ask, " No", 1},
		{"Enter takes the default", ask, "", 1},
		{"white space takes the default", ask, " ", 1},
		{"the default yes", YesNo(true), "", 0},
		{"an answer not offered", ask, "maybe", -1},
		{"a navigation input at a question", ask, "Q", 3},
		{"a key two bindings share selects the first", Set{{Keys: []string{"1"}}, {Keys: []string{"1"}}}, "1", 0},
		{"only the first default", Set{{Keys: []string{"a"}, Default: true}, {Keys: []string{"b"}, Default: true}}, "", 0},
		{"an empty key matches nothing", Set{{Keys: []string{""}}}, "", -1},
		{"an empty set", nil, "q", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i, ok := tt.set.Match(tt.line)
			if ok != (tt.want >= 0) || ok && i != tt.want {
				t.Errorf("Match(%q) = %d, %v; want %d", tt.line, i, ok, tt.want)
			}
		})
	}
}

// TestMatchAgreesWithHint checks that a prompt accepts every input its
// hint shows, in either case, each selecting its own binding.
func TestMatchAgreesWithHint(t *testing.T) {
	sets := map[string]Set{
		"main menu":   append(numbered(0, 10), Navigation()...),
		"a gap":       append(append(numbered(0, 2), Binding{Keys: []string{"4"}}), Navigation()...),
		"y/N":         append(YesNo(false), Navigation()...),
		"Y/n":         YesNo(true),
		"free text":   Navigation(),
		"two keys":    {{Keys: []string{"0", "b"}, Help: "go back"}},
		"word answer": {{Keys: []string{"apply"}, Default: true}, {Keys: []string{"cancel"}}},
	}
	for name, s := range sets {
		hint := strings.NewReplacer(", ", " ", " or ", " ", "/", " ").Replace(s.Hint())
		for _, term := range strings.Fields(hint) {
			for _, key := range expand(term) {
				for _, typed := range []string{key, strings.ToUpper(key), strings.ToLower(key)} {
					i, ok := s.Match(typed)
					if !ok || !slices.ContainsFunc(s[i].Keys, func(k string) bool { return strings.EqualFold(k, key) }) {
						t.Errorf("%s: hint %q shows %q, but Match(%q) = %d, %v", name, s.Hint(), key, typed, i, ok)
					}
				}
			}
		}
	}
}

// expand turns a hint term into the keys it stands for: "0-2" is 0, 1
// and 2.
func expand(term string) []string {
	lo, hi, ok := strings.Cut(term, "-")
	a, errA := strconv.Atoi(lo)
	b, errB := strconv.Atoi(hi)
	if !ok || errA != nil || errB != nil {
		return []string{term}
	}
	var out []string
	for n := a; n <= b; n++ {
		out = append(out, strconv.Itoa(n))
	}
	return out
}

// TestImports keeps the package on the standard library, so that code
// which must not link Lip Gloss, such as the legacy menus, can use it.
func TestImports(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "keys.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		if path, _ := strconv.Unquote(imp.Path.Value); strings.Contains(strings.Split(path, "/")[0], ".") {
			t.Errorf("keys.go imports %s: only the standard library", path)
		}
	}
}
