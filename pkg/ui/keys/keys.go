// Package keys holds the inputs a prompt accepts, as data. It borrows the
// Bubbles key.Binding pattern (brief 1C) without importing it: a Binding
// names the inputs that select one choice and says what the choice does,
// and a Set of bindings generates the hint a prompt shows, such as Figma's
// "0-10 or q", and a help line, such as "q to quit", and matches a typed
// line to its binding. The one Set is the source of truth for all three,
// so neither the hint nor the help is written by hand: Match accepts every
// input the hint shows and, besides them, only a question's other answer
// keys, such as "yes", and an empty line for its default.
//
// Every input is a whole line, typed and confirmed with Enter (brief §2),
// so a key is a string such as "1", "10" or "q", not a keypress. The
// navigation grammar's own inputs are defined here once (Back, Quit,
// Navigation), with the answers of a y/N question (YesNo), so that the
// components and the prompt kit share them; pkg/ui/nav binds each input
// to what it does. This package does no I/O, styles nothing and imports
// only the standard library: pkg/ui/component styles the text it returns.
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
	// help line, as for a numbered menu item, whose label says it, or a
	// question's answer, which the question says.
	Help string
	// Default marks the answer that an empty line selects, such as the n
	// of a y/N question (brief 1C: the capital letter marks the default).
	// A Set with a default is a question, and Hint shows its answers as
	// one term, this one's key in capitals. Only the first default of a
	// Set counts.
	Default bool
}

// Set is the bindings a prompt accepts.
type Set []Binding

// The navigation grammar (brief 1C) is the same at every prompt: 0 or b
// goes back and q quits, besides the prompt's own inputs. On a menu 0 is
// also the number of the Exit or Return item, shown as "0)" (pkg/ui/nav).

// Back returns the binding of 0 and b, which go back one level from any
// prompt, and at the top level exit: its help reads "0 or b to go back".
func Back() Binding { return Binding{Keys: []string{"0", "b"}, Help: "go back"} }

// Quit returns the binding of q, which quits hardn from any prompt: its
// help reads "q to quit", the Exit row's words (DEV-030).
func Quit() Binding { return Binding{Keys: []string{"q"}, Help: "quit"} }

// Navigation returns the inputs every prompt accepts besides its own,
// Back and Quit, which a hint shows as "0, b or q", or "0-10, b or q"
// after a menu's items.
func Navigation() Set { return Set{Back(), Quit()} }

// YesNo returns the answers of a y/N question, in this order: y or yes,
// then n or no. The default, n unless defaultYes is set, is what an empty
// line selects, and the hint shows it in capitals: "y/N", or "Y/n" with
// defaultYes.
func YesNo(defaultYes bool) Set {
	return Set{
		{Keys: []string{"y", "yes"}, Default: defaultYes},
		{Keys: []string{"n", "no"}, Default: !defaultYes},
	}
}

// Hint returns the inputs s accepts, as a prompt's hint shows them:
// "0-10 or q" for Figma's main menu. The prompt's own inputs come first:
// the bindings with no Help, such as a menu's items or a phrase to type.
// The inputs every prompt shares follow, the bindings with Help such as
// Navigation's, so that they end every hint alike: "0-10, b or q", or
// "web-01, 0, b or q" for a phrase. Each input appears once, where it
// first appears in that order: a menu's exit item, 0, is one of its own
// inputs.
// In each part numbers come first, in order, and a run of consecutive
// numbers collapses into a range, so 1, 2, 3 and 5 read "1-3 or 5"; the
// other inputs follow in the order they first appear. The terms are
// joined as "x, y or z". A number is a key in canonical decimal form; "01"
// or "+1" is kept as it is.
//
// A Set with a Default binding is a question. Its answers, the bindings
// with no Help and the Default ones, are its own inputs, shown as one
// term: the first key of each, in order, joined by "/", the first
// default's in capitals, as in "y/N"; only that default counts
// (Binding.Default). Their other keys, such as "yes", are accepted (Match)
// but not shown, as the y/N convention has it. The shared inputs follow as
// above: "y/N, 0, b or q".
//
// An empty Set, or one with no keys, has no hint: "".
func (s Set) Hint() string {
	question := slices.ContainsFunc(s, func(b Binding) bool { return b.Default })
	var answers, own, shared, seen []string
	capital := true // the first default is still to come
	for _, b := range s {
		switch {
		case b.Help != "" && !b.Default:
			shared = append(shared, b.Keys...)
			continue
		case !question:
			own = append(own, b.Keys...)
		default:
			if i := slices.IndexFunc(b.Keys, func(k string) bool { return k != "" }); i >= 0 {
				k := b.Keys[i]
				if b.Default && capital {
					k, capital = strings.ToUpper(k), false
				}
				answers = append(answers, k)
			}
		}
		seen = append(seen, b.Keys...)
	}
	shared = slices.DeleteFunc(shared, func(k string) bool { return slices.Contains(seen, k) })
	t := terms(own)
	if len(answers) > 0 {
		t = append([]string{strings.Join(answers, "/")}, t...)
	}
	return join(append(t, terms(shared)...))
}

// Help returns the help line for s: each binding that has Help, as its
// own keys joined as in Hint, "to" and the help, in order, joined by
// commas: "b to go back, q to quit". A binding with no keys or no help is
// left out.
func (s Set) Help() string {
	var out []string
	for _, b := range s {
		if k := join(terms(b.Keys)); k != "" && b.Help != "" {
			out = append(out, k+" to "+b.Help)
		}
	}
	return strings.Join(out, ", ")
}

// Match returns the index in s of the binding that the typed line
// selects, and whether one does. The line is compared with each key with
// its surrounding white space removed and regardless of case, so " Q "
// selects q; numbers compare as typed, so "01" does not select "1". A key
// that two bindings share selects the first. A line with nothing but
// white space selects the first Default binding, and no other. So a
// prompt accepts the keys its hint shows, a question's other answer keys
// such as "yes", and Enter alone for its default, and nothing else.
func (s Set) Match(line string) (int, bool) {
	line = strings.TrimSpace(line)
	for i, b := range s {
		if line == "" && b.Default ||
			line != "" && slices.ContainsFunc(b.Keys, func(k string) bool { return strings.EqualFold(k, line) }) {
			return i, true
		}
	}
	return -1, false
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
