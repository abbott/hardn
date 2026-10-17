package keys

import (
	"strconv"
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
	quit = Binding{Keys: []string{"q"}, Help: "quit"}
	back = Binding{Keys: []string{"0", "b"}, Help: "go back"}
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
		{"letters in the order they first appear", Set{quit, back}, "0, q or b"},
		{"the navigation grammar's example", append(append(numbered(1, 10), back), quit), "0-10, b or q"},
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
		{"two bindings", Set{back, quit}, "0 or b to go back, q to quit"},
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
