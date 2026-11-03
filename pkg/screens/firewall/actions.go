package firewall

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	firewallctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// Add's and Remove's form: origin/main's fields (firewall_menu.go:addAppProfile),
// its checks the control's (ufw.ValidProfile). The title's, the description's and
// the ports' refusals are decided (gallery-firewall.md §6); the rest are the
// mocks', or this unit's, proposed (screen-firewall.md).
const (
	nameTitle   = "Profile name"
	nameAbout   = "Such as WebServer."
	titleTitle  = "Title"
	aboutTitle  = "Description"
	portsTitle  = "Ports"
	portsAbout  = "Such as 80/tcp,443/tcp."
	removeTitle = "Remove which profile?"

	nameRefused  = "ufw can't use that name. Use letters, digits, dots, dashes and underscores."
	nameHeld     = "A profile with this name already exists."
	titleRefused = "Give the profile a title."
	aboutRefused = "Give the profile a description."
	portsRefused = "Give each port, 1 to 65535, with its protocol, such as 80/tcp."
)

// saveChange is the control's save of the application profiles, as NotLoaded
// names it; the words of its refusal do not depend on it.
const saveChange = firewallctl.ID + "/profiles"

// action returns what choosing the item keyed key does with the reading h: item 1
// says its plan's refusal before asking, or runs its change; item 2 opens the
// application profiles; item 3 is Disable.
func (r *runner) action(ctx context.Context, h reading, key string) func() error {
	switch key {
	case screen.FirewallFirstItem:
		if h.PlanErr != nil {
			return r.s.Say(screenkit.Warnings(screenkit.RefusalWords(h.PlanErr)))
		}
		return r.plain(ctx, firstAction(h.First), h.view.FirstChange(), risky(h.Plan))
	case screen.FirewallProfilesItem:
		return func() error { return r.o.Prompter.Run(func() nav.Screen { return r.drawProfiles(ctx) }) }
	case screen.FirewallDisableItem:
		return r.plain(ctx, firewallctl.ActionDisable, screen.FirewallDisableChange(), false)
	}
	return func() error { return fmt.Errorf("firewall screen: item %s has no action", key) } // not reached: SubMenu's items are those above
}

// firstAction is what item 1 plans, by Reading.First.
func firstAction(first firewallctl.Item) firewallctl.Action {
	switch first {
	case firewallctl.ItemInstall:
		return firewallctl.ActionInstall
	case firewallctl.ItemDisable:
		return firewallctl.ActionDisable
	}
	return firewallctl.ActionEnable
}

// plain returns the action of one change with no form: action planned under c's
// words, as change runs it.
func (r *runner) plain(ctx context.Context, action firewallctl.Action, c screen.FirewallChange, lockout bool) func() error {
	return func() error {
		return r.change(ctx, lockout, func() string { return c.Question }, screenkit.Change{
			Step: c.Step,
			Flow: flow.Flow{Review: func() flow.Review { return c.Review }},
			Controls: func(*config.Config) []engine.Control {
				return []engine.Control{firewallctl.Incoming{Action: action}}
			},
		})
	}
}

// change runs ch through the flow (screenkit.Session.Change). The question's
// words are the draw's, from the form's answers. Only the confirmation follows
// the plan made at the key: y/N, or the hostname typed where that plan is
// lockout-risk (flow decides from it), and a hostname that can't be read then
// says so in place of the question (nav.ErrPhrase, as CantConfirm). With
// lockout, the reading's plan being lockout-risk, a live change whose hostname
// can't be read says so before asking anything; plan only confirms nothing, so
// it needs no hostname.
func (r *runner) change(ctx context.Context, lockout bool, question func() string, ch screenkit.Change) error {
	if lockout && !r.o.DryRun {
		if _, _, err := screenkit.Hostname(ctx, r.o.System); err != nil {
			return r.s.CantConfirm(err)
		}
	}
	var phraseErr error
	ch.Flow.Confirm = func(p flow.Plan) flow.Confirm {
		c := flow.Confirm{Question: question()}
		if p.Typed() {
			c.Label = screenkit.TypeTheHostname
			c.Phrase, c.Short, phraseErr = screenkit.Hostname(ctx, r.o.System)
		}
		return c
	}
	err := r.s.Change(ctx, ch)
	if phraseErr != nil && errors.Is(err, nav.ErrPhrase) {
		return r.s.CantConfirm(phraseErr)
	}
	return err
}

// drawProfiles reads the host and returns the application profiles' screen: Add,
// and with any profile Remove and Apply, each through the flow; under plan only
// each stops at its plan (P-03), where origin/main refuses Add and Remove. With no
// hardn.yml read, Add and Remove say so before asking anything.
func (r *runner) drawProfiles(ctx context.Context) nav.Screen {
	h := r.read(ctx)
	rows := profileRows(h.Reading)
	view := screen.FirewallProfiles{Rows: rows, Any: len(h.Profiles) > 0}
	sub := view.SubMenu()
	notLoaded := screenkit.NotLoaded(ctx, r.o.System, r.o.Config, saveChange, firewallctl.ProfileSetting)
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		var act func() error
		switch {
		case it.Key == screen.FirewallApplyItem:
			act = func() error { return r.apply(ctx, view) }
		case notLoaded != nil:
			act = r.s.Say(screenkit.Warnings(notLoaded...))
		case it.Key == screen.FirewallAddItem:
			act = func() error { return r.add(ctx, h) }
		default:
			act = func() error { return r.remove(ctx, h) }
		}
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: act})
	}
	return sub.Screen(r.o.Prompter.Ctx, menu)
}

// apply is Apply: its plan's refusal, the control's, said before asking, as
// Reading does not hold it; else its change. Its plan is plain
// (steps.PlanUFWApps), so no hostname is checked before asking; one that turned
// lockout-risk would be confirmed as change confirms any plan.
func (r *runner) apply(ctx context.Context, view screen.FirewallProfiles) error {
	in := engine.Input{Facts: r.o.Facts, Config: r.o.Config, Session: r.o.Session}
	apply := firewallctl.Incoming{Action: firewallctl.ActionApplyProfiles}
	if _, err := apply.Plan(ctx, r.o.System, in); err != nil {
		return r.s.Say(screenkit.Warnings(screenkit.RefusalWords(err)))()
	}
	return r.plain(ctx, firewallctl.ActionApplyProfiles, view.ApplyChange(), false)()
}

// add is Add: origin/main's four questions, each refused at its field by the
// control's check of a profile (ufw.ValidProfile), a name in any case already
// held a warning; then the save.
func (r *runner) add(ctx context.Context, h reading) error {
	var name, title, about, ports string
	check := func(words string, ok func(string) bool) func(string) error {
		return func(s string) error {
			if !ok(s) {
				return nav.Refuse(words)
			}
			return nil
		}
	}
	fields := []form.Field{
		&form.Text{Title: nameTitle, Description: nameAbout, Value: &name, Validate: func(s string) error {
			switch {
			case !profileOK(s, "x", "x", "1/tcp"):
				return nav.Refuse(nameRefused)
			case slices.ContainsFunc(h.Profiles, func(p config.UfwAppProfile) bool { return strings.EqualFold(p.Name, s) }):
				return nav.Warn(nameHeld)
			}
			return nil
		}},
		&form.Text{Title: titleTitle, Value: &title, Validate: check(titleRefused, func(s string) bool { return profileOK("x", s, "x", "1/tcp") })},
		&form.Text{Title: aboutTitle, Value: &about, Validate: check(aboutRefused, func(s string) bool { return profileOK("x", "x", s, "1/tcp") })},
		&form.Text{Title: portsTitle, Description: portsAbout, Value: &ports, Validate: check(portsRefused, func(s string) bool { return profileOK("x", "x", "x", s) })},
	}
	return r.change(ctx, false, func() string { return screen.FirewallAddQuestion(name) }, screenkit.Change{
		Step: screen.FirewallAddStep(),
		Flow: flow.Flow{
			Form:   form.Form{Fields: fields},
			Review: func() flow.Review { return screen.FirewallAddReview(name, splitPorts(ports)) },
		},
		Controls: func(edited *config.Config) []engine.Control {
			edited.UfwAppProfiles = append(slices.Clone(edited.UfwAppProfiles), config.UfwAppProfile{Name: name, Title: title, Description: about, Ports: splitPorts(ports)})
			return []engine.Control{firewallctl.Incoming{Action: firewallctl.ActionSaveProfiles}}
		},
	})
}

// profileOK reports whether ufw's check takes the profile of these answers, its
// ports as the form takes them; each field is checked with the others filled
// with what it takes, so that its refusal speaks of that field alone.
func profileOK(name, title, about, ports string) bool {
	return ufw.ValidProfile(ufw.Profile{Name: name, Title: title, Description: about, Ports: splitPorts(ports)}) == nil
}

// splitPorts is the form's ports, split on commas as its example shows them;
// each is a port of the profile, which ufw's file joins by "|".
func splitPorts(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// remove is Remove: which one, at its Select, a number no profile has refused
// there; then the save. A rule already in ufw stays, and the review says so.
func (r *runner) remove(ctx context.Context, h reading) error {
	gone := 0
	opts := make([]form.Option[int], len(h.Profiles))
	for i, p := range h.Profiles {
		opts[i] = form.Option[int]{Label: p.Name, Value: i}
	}
	return r.change(ctx, false, func() string { return screen.FirewallRemoveQuestion(h.Profiles[gone].Name) }, screenkit.Change{
		Step: screen.FirewallRemoveStep(),
		Flow: flow.Flow{
			Form:   form.Form{Fields: []form.Field{&form.Select[int]{Title: removeTitle, Options: opts, Value: &gone}}},
			Review: screen.FirewallRemoveReview,
		},
		Controls: func(edited *config.Config) []engine.Control {
			edited.UfwAppProfiles = slices.Delete(slices.Clone(edited.UfwAppProfiles), gone, gone+1)
			return []engine.Control{firewallctl.Incoming{Action: firewallctl.ActionSaveProfiles}}
		},
	})
}
