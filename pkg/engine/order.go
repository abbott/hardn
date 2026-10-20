package engine

import (
	"fmt"
	"slices"
	"strings"
)

// order returns cs in the order an apply runs them, which is stable: by
// Stage; within a stage, each change after the changes its After names
// there, and otherwise in the order the controls planned them (Kahn's
// algorithm, always taking the earliest planned change that is ready). A
// prerequisite in an earlier stage is met by the stages; one in a later
// stage, one no control planned, and a cycle are ErrOrder, an ID planned
// twice ErrChange. order is pure.
func order(cs []Change) ([]Change, error) {
	at := make(map[string]int, len(cs)) // ID -> plan index
	for i, c := range cs {
		if _, dup := at[c.ID]; dup {
			return nil, fmt.Errorf("engine: change %q planned twice: %w", c.ID, ErrChange)
		}
		at[c.ID] = i
	}
	for _, c := range cs {
		for _, a := range c.After {
			j, ok := at[a]
			switch {
			case !ok:
				return nil, fmt.Errorf("engine: change %q needs %q, which no control planned: %w", c.ID, a, ErrOrder)
			case cs[j].Stage > c.Stage:
				return nil, fmt.Errorf("engine: change %q (stage %v) needs %q, which runs later (stage %v): %w", c.ID, c.Stage, a, cs[j].Stage, ErrOrder)
			}
		}
	}
	idx := make([]int, len(cs))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return int(cs[a].Stage) - int(cs[b].Stage) })
	out := make([]Change, 0, len(cs))
	for lo := 0; lo < len(idx); {
		hi := lo
		for hi < len(idx) && cs[idx[hi]].Stage == cs[idx[lo]].Stage {
			hi++
		}
		group, err := orderStage(cs, idx[lo:hi], at)
		if err != nil {
			return nil, err
		}
		for _, i := range group {
			out = append(out, cs[i])
		}
		lo = hi
	}
	return out, nil
}

// orderStage orders one stage's changes (plan indexes, ascending) after
// their prerequisites in the stage.
func orderStage(cs []Change, stage []int, at map[string]int) ([]int, error) {
	in := map[int]bool{}
	for _, i := range stage {
		in[i] = true
	}
	waiting := map[int]int{} // plan index -> prerequisites in this stage not yet placed
	for _, i := range stage {
		for _, a := range cs[i].After {
			if in[at[a]] {
				waiting[i]++
			}
		}
	}
	var out []int
	placed := map[int]bool{}
	for len(out) < len(stage) {
		next := -1
		for _, i := range stage { // ascending: the earliest planned ready change
			if !placed[i] && waiting[i] == 0 {
				next = i
				break
			}
		}
		if next < 0 {
			var cycle []string
			for _, i := range stage {
				if !placed[i] {
					cycle = append(cycle, cs[i].ID)
				}
			}
			return nil, fmt.Errorf("engine: changes %s need each other (stage %v): %w", strings.Join(cycle, ", "), cs[stage[0]].Stage, ErrOrder)
		}
		placed[next] = true
		out = append(out, next)
		for _, i := range stage {
			if !placed[i] && slices.Contains(cs[i].After, cs[next].ID) {
				waiting[i]--
			}
		}
	}
	return out, nil
}
