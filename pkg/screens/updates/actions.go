package updates

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	updatesctl "github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// draw reads the host and returns the screen, its item bound: an
// unavailable item has no action, as nav.Menu takes it.
func (r *runner) draw(ctx context.Context) nav.Screen {
	v := mainView(r.read(ctx))
	sub := v.SubMenu()
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		o := nav.Option{Item: it}
		if !it.Disabled {
			o.Action = func() error { return r.change(ctx, v) }
		}
		menu.Options = append(menu.Options, o)
	}
	return sub.Screen(r.o.Prompter.Ctx, menu)
}

// preplan plans c on the engine from a fresh reading of the host, writing
// nothing outside the engine's run directory, which it removes, and returns
// its first refusal in the screen's words (screenkit.RefusalWords), ""
// for none: the control's refusals and the engine's (a change waiting for
// hardn confirm among them) are said before asking.
func (r *runner) preplan(ctx context.Context, c engine.Control) (string, error) {
	p, err := r.o.NewEngine(func(engine.Event) {}).Plan(ctx, []engine.Control{c}, r.cfg)
	switch {
	case err != nil && ctx.Err() != nil:
		return "", err
	case err != nil:
		return screenkit.RefusalWords(err), nil
	case len(p.Refused) > 0:
		return screenkit.RefusalWords(p.Refused[0]), nil
	}
	return "", nil
}

// change runs the item, on or off as v draws it, through the flow
// (screenkit.Session.Change), plain y/N: planned first (preplan) and
// refused before asking; its review, the row as the change leaves it and
// the control's notes; its plan from a fresh reading; the question.
func (r *runner) change(ctx context.Context, v screen.Updates) error {
	action := updatesctl.AutoOn
	if v.Off() {
		action = updatesctl.AutoOff
	}
	c := r.control(action)
	switch words, err := r.preplan(ctx, c); {
	case err != nil:
		return err
	case words != "":
		return r.s.Say(screenkit.Warnings(words))()
	}
	ns, err := c.Notes(ctx, r.o.System, engine.Input{Facts: r.o.Facts, Config: r.cfg})
	if err != nil {
		return err
	}
	ch := v.Change(notes(ns))
	return r.s.Change(ctx, screenkit.Change{
		Step: ch.Step,
		Flow: flow.Flow{Review: func() flow.Review { return ch.Review }, Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: ch.Question} }},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{c}
		},
	})
}
