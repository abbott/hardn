package engine

import (
	"fmt"
	"slices"
	"strings"
)

// conflict is a refusal of conflicts: the control whose change came
// second, and why.
type conflict struct {
	control string
	err     error
}

// conflicts refuses, with ErrConflict, two changes of cs that back up the
// same file, and two controls whose changes name the same resources, each
// pair of controls once. cs is in the order the controls planned them.
func conflicts(cs []Change) []conflict {
	var out []conflict
	backedUp := map[string]Change{}
	first := map[string]Change{} // a resource's first change
	type pair struct{ a, b string }
	var pairs []pair
	shared := map[pair][]string{}
	for _, c := range cs {
		for _, p := range c.Backups {
			if o, ok := backedUp[p]; ok {
				out = append(out, conflict{c.Control, fmt.Errorf("engine: changes %q and %q both back up %s: %w", o.ID, c.ID, p, ErrConflict)})
				continue
			}
			backedUp[p] = c
		}
		for _, r := range c.Resources {
			o, ok := first[r]
			if !ok {
				first[r] = c
				continue
			}
			k := pair{o.Control, c.Control}
			if o.Control == c.Control || slices.Contains(shared[k], r) {
				continue
			}
			if shared[k] == nil {
				pairs = append(pairs, k)
			}
			shared[k] = append(shared[k], r)
		}
	}
	for _, k := range pairs {
		out = append(out, conflict{k.b, fmt.Errorf("engine: controls %q and %q both plan changes of %s, each from the host as it is now: plan them as one control's changes, or in two plans: %w",
			k.a, k.b, strings.Join(shared[k], " and "), ErrConflict)})
	}
	return out
}
