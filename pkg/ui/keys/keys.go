// Package keys holds the inputs a prompt accepts, as data. It borrows the
// Bubbles key.Binding pattern (brief 1C) without importing it: a Binding
// names the inputs that select one choice and says what the choice does,
// and a Set of bindings generates both the hint a prompt shows, such as
// Figma's "0-10 or q", and a help line, such as "q to quit". The one Set
// is the source of truth for both, so neither is written by hand and
// neither can drift from the inputs a screen accepts.
//
// Every input is a whole line, typed and confirmed with Enter (brief §2),
// so a key is a string such as "1", "10" or "q", not a keypress. Matching
// a typed line to a binding, and the navigation grammar's own bindings
// (numbers select, 0 or b goes back, q quits), belong to the prompt kit
// (refactor plan next/p1c-nav); this package holds only what rendering
// needs. It does no I/O and styles nothing: pkg/ui/component styles the
// text it returns.
package keys

import (
	"slices"
	"strconv"
	"strings"
)

// Binding is one choice a prompt accepts.
type Binding struct {
	// Keys are the inputs that select the choice, such as "1" or "q".
	// Empty keys are ignored.
	Keys []string
	// Help says what the choice does, as the words after "to" in the help
	// line: "quit" reads "q to quit". Empty leaves the binding out of the
	// help line, as for a numbered menu item, whose label says it.
	Help string
}

// Set is the bindings a prompt accepts.
type Set []Binding

// Hint returns the inputs s accepts, as a prompt's hint shows them:
// "0-10 or q" for Figma's main menu. Each input appears once. Numbers
// come first, in order, and a run of consecutive numbers collapses into a
// range, so 1, 2, 3 and 5 read "1-3 or 5". The other inputs follow in the
// order they first appear. The terms are joined as "x, y or z". A number
// is a key in canonical decimal form; "01" or "+1" is kept as it is. An
// empty Set, or one with no keys, has no hint: "".
func (s Set) Hint() string {
	var all []string
	for _, b := range s {
		all = append(all, b.Keys...)
	}
	return join(terms(all))
}

// Help returns the help line for s: each binding that has Help, as its
// own keys joined as in Hint, "to" and the help, in order, joined by
// commas: "0 or b to go back, q to quit". A binding with no keys or no
// help is left out.
func (s Set) Help() string {
	var out []string
	for _, b := range s {
		if k := join(terms(b.Keys)); k != "" && b.Help != "" {
			out = append(out, k+" to "+b.Help)
		}
	}
	return strings.Join(out, ", ")
}

// terms lists keys once each: the numbers in order, consecutive ones
// collapsed into a range such as "0-10", then the other keys in the order
// they first appear. Empty keys are skipped.
func terms(keys []string) []string {
	var nums []int
	var out []string
	for _, k := range keys {
		if n, ok := number(k); ok {
			if !slices.Contains(nums, n) {
				nums = append(nums, n)
			}
		} else if k != "" && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	slices.Sort(nums)
	var ranges []string
	for i := 0; i < len(nums); {
		j := i
		for j+1 < len(nums) && nums[j+1] == nums[j]+1 {
			j++
		}
		r := strconv.Itoa(nums[i])
		if j > i {
			r += "-" + strconv.Itoa(nums[j])
		}
		ranges = append(ranges, r)
		i = j + 1
	}
	return append(ranges, out...)
}

// number reports whether k is a whole number in canonical decimal form,
// and its value.
func number(k string) (int, bool) {
	n, err := strconv.Atoi(k)
	return n, err == nil && n >= 0 && strconv.Itoa(n) == k
}

// join joins terms as "x", "x or y" or "x, y or z".
func join(terms []string) string {
	if len(terms) < 2 {
		return strings.Join(terms, "")
	}
	last := len(terms) - 1
	return strings.Join(terms[:last], ", ") + " or " + terms[last]
}
