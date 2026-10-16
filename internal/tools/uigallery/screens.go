package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// selectDrawings returns the screen drawings args name, in the catalogue's
// order (screen.Mocks): every drawing for no argument or "all"; for a
// name, the drawing of that name and every drawing under it, so that
// "sub-menu" is every sub-menu, a workflow mock's name all its sequences,
// and a sequence's name its steps. An argument that starts with "-" is a
// flag the flag package left unparsed, after a name.
func selectDrawings(mocks []screen.Mock, args []string) ([]string, error) {
	var all []string
	for _, m := range mocks {
		all = append(all, m.Names()...)
	}
	if len(args) == 0 || slices.Contains(args, "all") {
		return all, nil
	}
	want := map[string]bool{}
	for _, a := range args {
		found := false
		for _, n := range all {
			if n == a || strings.HasPrefix(n, a+"/") {
				want[n], found = true, true
			}
		}
		switch {
		case !found && strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("flag %s comes after a screen name; flags go before them", a)
		case !found:
			return nil, fmt.Errorf("no screen %q; -screens -list shows them", a)
		}
	}
	return slices.DeleteFunc(all, func(n string) bool { return !want[n] }), nil
}

// source is the gallery's note of where a mock comes from: an info
// Callout holding its source label, the origin/main-vs-v0.3.2 delta and
// what waits on a proposal.
func source(ctx component.Ctx, s screen.Source) string {
	return component.Callout{Kind: component.MessageInfo, Body: []string{"Source: " + s.Label, s.Delta, s.Pending}}.Render(ctx)
}

// diverged is the gallery's note over a sequence that ends otherwise in
// cell c than it says (a *screen.Diverged), as one whose typed
// confirmation the terminal is too narrow to ask: the run is shown whole,
// since its steps are not the sequence's.
func diverged(ctx component.Ctx, d *screen.Diverged, c cell) string {
	return component.Callout{Kind: component.MessageInfo, Body: []string{fmt.Sprintf(
		"In %s the %s sequence ends %s, not %s, so its run is shown whole, not step by step.",
		c.name, d.Sequence, screen.Ending(d.Ended), screen.Ending(d.Want))}}.Render(ctx)
}

// renderMock returns the drawings of m that want holds, in cell c: under
// the gallery's header naming m, its source note, then each drawing,
// after a header naming its sequence and step for a workflow's, the
// step's own blank lines before it, which set it apart from the step
// before, giving way to the header's. Each is followed by a blank line. A
// sequence that ends otherwise in c than it says, as one whose typed
// confirmation the terminal is too narrow to ask, is shown whole under a
// header naming it and a note saying so (diverged), so that the other
// mocks still show. It returns "" when want holds none of m's.
func (g gallery) renderMock(m screen.Mock, want []string, c cell) (string, error) {
	if !slices.ContainsFunc(m.Names(), func(n string) bool { return slices.Contains(want, n) }) {
		return "", nil
	}
	ctx, err := g.ctx(c.env)
	if err != nil {
		return "", err
	}
	gap := strings.Repeat("\n", theme.SectionGap+1)
	var b strings.Builder
	b.WriteString(header(ctx, m.Name) + gap + source(ctx, m.Source) + gap)
	if m.Screen != nil {
		b.WriteString(m.Screen.Render(ctx) + gap)
		return b.String(), nil
	}
	for _, s := range m.Sequences {
		var names []string
		for _, step := range s.Steps() {
			if slices.Contains(want, m.Name+"/"+s.Name+"/"+step) {
				names = append(names, step)
			}
		}
		if len(names) == 0 {
			continue
		}
		steps, err := s.Play(ctx)
		var d *screen.Diverged
		switch {
		case errors.As(err, &d):
			b.WriteString(header(ctx, s.Name) + gap + diverged(ctx, d, c) + gap + strings.TrimLeft(d.Transcript, "\n") + gap)
			continue
		case err != nil:
			return "", err
		}
		for i, step := range s.Steps() {
			if slices.Contains(names, step) {
				b.WriteString(header(ctx, s.Name+"/"+step) + gap + strings.TrimLeft(steps[i], "\n") + gap)
			}
		}
	}
	return b.String(), nil
}
