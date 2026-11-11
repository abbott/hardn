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

// edit sets in cfg what action saves: enableUnattendedUpgrades false at
// off, which the control saves where the loaded hardn.yml says true; on
// saves nothing.
func edit(action updatesctl.AutoAction, cfg *config.Config) {
	if action == updatesctl.AutoOff {
		cfg.EnableUnattendedUpgrades = false
	}
}

// change runs the item, on or off as v draws it, through the flow
// (screenkit.Session.Change), plain y/N: planned first
// (screenkit.Session.Preplan) and refused before asking; its review, the
// row as the change leaves it and the control's notes; its plan from a
// fresh reading; the question. Each plans with a copy of hardn.yml edited
// as the change saves it (edit).
func (r *runner) change(ctx context.Context, v screen.Updates) error {
	action := updatesctl.AutoOn
	if v.Off() {
		action = updatesctl.AutoOff
	}
	c := r.control(action)
	edited := *r.cfg // as screenkit.Session.Change edits its copy
	edit(action, &edited)
	switch words, err := r.s.Preplan(ctx, &edited, c); {
	case err != nil:
		return err
	case words != "":
		return r.s.Say(screenkit.Warnings(words))()
	}
	ns, err := c.Notes(ctx, r.o.System, engine.Input{Facts: r.o.Facts, Config: &edited})
	if err != nil {
		return err
	}
	ch := v.Change(notes(ns))
	return r.s.Change(ctx, screenkit.Change{
		Step: ch.Step,
		Flow: flow.Flow{Review: func() flow.Review { return ch.Review }, Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: ch.Question} }},
		Controls: func(edited *config.Config) []engine.Control {
			edit(action, edited) // saved as edited, so that Adopt finds the bytes written
			return []engine.Control{c}
		},
	})
}
