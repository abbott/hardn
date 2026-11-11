package firewall

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

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
	ifaceTitle  = "Interface"
	ifaceAbout  = "Such as tailscale0. Empty for every interface."
	removeTitle = "Remove which profile?"

	nameRefused  = "ufw can't use that name. Start it with a letter or digit, then use letters, digits, spaces, dots, dashes, underscores or +."
	nameReserved = "ufw can't use that name. It keeps all and numbers for itself."
	nameHeld     = "A profile with this name already exists."
	titleRefused = "Give the profile a title."
	aboutRefused = "Give the profile a description."
	portsRefused = "Give each port, 1 to 65535, with its protocol, such as 80/tcp."
	ifaceRefused = "ufw can't use that interface name. Use up to 15 letters, digits and _-.+,=%@."

	// The trusted interfaces' form, and the profile's interface above it:
	// approved by the maintainer, 2026-11-07 (gallery-firewall.md §6).
	trustAbout   = "Such as tailscale0."
	trustHeld    = "This interface is trusted already."
	untrustTitle = "Remove which interface?"
)

// saveChange is the control's save of the application profiles, as NotLoaded
// names it; the words of its refusal do not depend on it. saveIfaceChange is
// the trusted interfaces'.
const (
	saveChange      = firewallctl.ID + "/profiles"
	saveIfaceChange = firewallctl.ID + "/trusted"
)

// action returns what choosing the item keyed key does with the reading h: item 1
// says its plan's refusal before asking, or runs its change, planned on the
// engine first (planned); item 2 opens the application profiles; item 3 the
// trusted interfaces; item 4 is Disable, planned first.
func (r *runner) action(ctx context.Context, h reading, key string) func() error {
	switch key {
	case screen.FirewallFirstItem:
		if h.PlanErr != nil {
			return r.s.Say(screenkit.Warnings(screenkit.RefusalWords(h.PlanErr)))
		}
		if h.view.First == screen.FirewallEnableSS {
			return r.enableWithSS(ctx, h)
		}
		return r.planned(ctx, firstAction(h.First), h.view.FirstChange(), risky(h.Plan))
	case screen.FirewallProfilesItem:
		return func() error { return r.o.Prompter.Run(func() nav.Screen { return r.drawProfiles(ctx) }) }
	case screen.FirewallInterfacesItem:
		return func() error { return r.o.Prompter.Run(func() nav.Screen { return r.drawInterfaces(ctx) }) }
	case screen.FirewallDisableItem:
		return r.planned(ctx, firewallctl.ActionDisable, screen.FirewallDisableChange(), false)
	}
	return func() error { return fmt.Errorf("firewall screen: item %s has no action", key) } // not reached: SubMenu's items are those above
}

// enableWithSS is item 1 where ufw is not running and ss is missing, "Enable
// firewall" (the maintainer, 2026-11-07): the hostname, which Enable will ask,
// checked first, live; then iproute2's install, planned on the engine first
// (planned), its review saying why, y/N; then, the host read again with ss
// there, Enable's own change. Declined, under plan only (the install stops at its plan), or with ss
// still missing, it ends there, the screen drawn again.
func (r *runner) enableWithSS(ctx context.Context, h reading) func() error {
	return func() error {
		if !r.o.DryRun {
			if _, _, err := screenkit.Hostname(ctx, r.o.System); err != nil {
				return r.s.CantConfirm(err)
			}
		}
		if err := r.planned(ctx, firewallctl.ActionInstallSS, h.view.FirstChange(), false)(); err != nil {
			return err
		}
		after := r.read(ctx)
		switch {
		case after.Sockets.NoSS || after.First != firewallctl.ItemEnable:
			return nil
		case after.PlanErr != nil:
			return r.s.Say(screenkit.Warnings(screenkit.RefusalWords(after.PlanErr)))()
		}
		return r.plain(ctx, firewallctl.ActionEnable, after.view.FirstChange(), risky(after.Plan))()
	}
}

// firstAction is what item 1 plans, by Reading.First.
func firstAction(first firewallctl.Item) firewallctl.Action {
	switch first {
	case firewallctl.ItemInstall:
		return firewallctl.ActionInstall
	case firewallctl.ItemInstallSS:
		return firewallctl.ActionInstallSS
	case firewallctl.ItemDisable:
		return firewallctl.ActionDisable
	}
	return firewallctl.ActionEnable
}

// planned is plain's action planned on the engine first, its refusal said
// before asking (screenkit.Session.Preplan): the control's, and the engine's
// (a change waiting for hardn confirm among them), which a reading's plan,
// the control's alone, cannot see. Every change with no form takes it. A
// lockout-risk one, live, checks the hostname before anything is planned, as
// change does.
func (r *runner) planned(ctx context.Context, action firewallctl.Action, c screen.FirewallChange, lockout bool) func() error {
	run := r.plain(ctx, action, c, false)
	return func() error {
		if lockout && !r.o.DryRun {
			if _, _, err := screenkit.Hostname(ctx, r.o.System); err != nil {
				return r.s.CantConfirm(err)
			}
		}
		switch words, err := r.s.Preplan(ctx, r.s.Config, firewallctl.Incoming{Action: action}); {
		case err != nil:
			return err
		case words != "":
			return r.s.Say(screenkit.Warnings(words))()
		}
		return run()
	}
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
	view := screen.FirewallProfiles{Rows: rows, Any: len(h.Profiles) > 0, Inactive: stopped(h.Reading)}
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

// apply is Apply: planned on the engine first, its refusal said before asking
// (planned), as Reading does not hold it; else its change. Its plan is plain
// (steps.PlanUFWApps), so no hostname is checked before asking; one that turned
// lockout-risk would be confirmed as change confirms any plan.
func (r *runner) apply(ctx context.Context, view screen.FirewallProfiles) error {
	return r.planned(ctx, firewallctl.ActionApplyProfiles, view.ApplyChange(), false)()
}

// add is Add: origin/main's four questions, each refused at its field by the
// control's check of a profile (ufw.ValidProfile), a name in any case already
// held a warning; then the interface, optional, an empty line every interface
// (ufw.ValidInterface; the maintainer, 2026-11-07); then the save.
func (r *runner) add(ctx context.Context, h reading) error {
	var name, title, about, ports, iface string
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
			case !profileOK(s, "x", "x", "1/tcp") && nameChars(s):
				return nav.Refuse(nameReserved)
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
		&form.Text{Title: ifaceTitle, Description: ifaceAbout, Value: &iface, Validate: check(ifaceRefused, func(s string) bool { return s == "" || ufw.ValidInterface(s) })},
	}
	return r.change(ctx, false, func() string { return screen.FirewallAddQuestion(name) }, screenkit.Change{
		Step: screen.FirewallAddStep(),
		Flow: flow.Flow{
			Form:   form.Form{Fields: fields},
			Review: func() flow.Review { return screen.FirewallAddReview(name, splitPorts(ports), iface) },
		},
		Controls: func(edited *config.Config) []engine.Control {
			edited.UfwAppProfiles = append(slices.Clone(edited.UfwAppProfiles), config.UfwAppProfile{Name: name, Title: title, Description: about, Ports: splitPorts(ports), Interface: iface})
			return []engine.Control{firewallctl.Incoming{Action: firewallctl.ActionSaveProfiles}}
		},
	})
}

// nameChars reports whether s has only the characters ufw takes in a profile's
// name, a letter or digit first; it picks the words of a name refused, so that one
// ufw still refuses is one it keeps for itself ("all", a number). ufw.ValidProfile
// refuses it.
func nameChars(s string) bool {
	for i, c := range s {
		alnum := c < utf8.RuneSelf && (unicode.IsLetter(c) || unicode.IsDigit(c))
		if !alnum && (i == 0 || !strings.ContainsRune(" _-.+", c)) {
			return false
		}
	}
	return s != ""
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

// drawInterfaces reads the host and returns the trusted interfaces' screen, as
// drawProfiles returns the profiles': Add, and with any interface Remove and
// Apply, each through the flow; under plan only each stops at its plan. With
// no hardn.yml read, Add and Remove say so before asking anything.
func (r *runner) drawInterfaces(ctx context.Context) nav.Screen {
	h := r.read(ctx)
	view := screen.FirewallInterfaces{Rows: interfaceRows(h.Reading), Any: len(h.Interfaces) > 0, Inactive: stopped(h.Reading)}
	sub := view.SubMenu()
	notLoaded := screenkit.NotLoaded(ctx, r.o.System, r.o.Config, saveIfaceChange, firewallctl.InterfaceSetting)
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		var act func() error
		switch {
		case it.Key == screen.FirewallApplyItem:
			act = func() error { return r.applyInterfaces(ctx, view) }
		case notLoaded != nil:
			act = r.s.Say(screenkit.Warnings(notLoaded...))
		case it.Key == screen.FirewallAddItem:
			act = func() error { return r.trust(ctx, h) }
		default:
			act = func() error { return r.untrust(ctx, h) }
		}
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: act})
	}
	return sub.Screen(r.o.Prompter.Ctx, menu)
}

// applyInterfaces is the trusted interfaces' Apply, as apply is the
// profiles': planned first, its refusal said before asking; else its plain
// change.
func (r *runner) applyInterfaces(ctx context.Context, view screen.FirewallInterfaces) error {
	return r.planned(ctx, firewallctl.ActionApplyInterfaces, view.ApplyChange(), false)()
}

// trust is the trusted interfaces' Add: the interface, refused where ufw would
// not take it (ufw.ValidInterface), one trusted already a warning; the review
// says what it lets through; then the save.
func (r *runner) trust(ctx context.Context, h reading) error {
	var iface string
	field := &form.Text{Title: ifaceTitle, Description: trustAbout, Value: &iface, Validate: func(s string) error {
		switch {
		case !ufw.ValidInterface(s):
			return nav.Refuse(ifaceRefused)
		case slices.Contains(h.Interfaces, s):
			return nav.Warn(trustHeld)
		}
		return nil
	}}
	return r.change(ctx, false, func() string { return screen.FirewallTrustQuestion(iface) }, screenkit.Change{
		Step: screen.FirewallTrustStep(),
		Flow: flow.Flow{
			Form:   form.Form{Fields: []form.Field{field}},
			Review: func() flow.Review { return screen.FirewallTrustReview(iface) },
		},
		Controls: func(edited *config.Config) []engine.Control {
			edited.UfwTrustedInterfaces = append(slices.Clone(edited.UfwTrustedInterfaces), iface)
			return []engine.Control{firewallctl.Incoming{Action: firewallctl.ActionSaveInterfaces}}
		},
	})
}

// untrust is the trusted interfaces' Remove: which one, then the save. A rule
// already in ufw stays, and the review says so.
func (r *runner) untrust(ctx context.Context, h reading) error {
	gone := 0
	opts := make([]form.Option[int], len(h.Interfaces))
	for i, iface := range h.Interfaces {
		opts[i] = form.Option[int]{Label: iface, Value: i}
	}
	return r.change(ctx, false, func() string { return screen.FirewallUntrustQuestion(h.Interfaces[gone]) }, screenkit.Change{
		Step: screen.FirewallUntrustStep(),
		Flow: flow.Flow{
			Form:   form.Form{Fields: []form.Field{&form.Select[int]{Title: untrustTitle, Options: opts, Value: &gone}}},
			Review: screen.FirewallUntrustReview,
		},
		Controls: func(edited *config.Config) []engine.Control {
			edited.UfwTrustedInterfaces = slices.Delete(slices.Clone(edited.UfwTrustedInterfaces), gone, gone+1)
			return []engine.Control{firewallctl.Incoming{Action: firewallctl.ActionSaveInterfaces}}
		},
	})
}

// stopped: ufw is installed, its status read, and not running: Apply then says
// to enable the firewall first.
func stopped(r firewallctl.Reading) bool {
	return r.StateErr == nil && r.State.Installed && r.State.Status.Known && !r.State.Status.Active
}
