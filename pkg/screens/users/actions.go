package users

import (
	"context"
	"errors"
	"fmt"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	usersctl "github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// Create's defaults' descriptions: this unit's, proposed
// (screen-users.md §5). A default is hardn.yml's, never the user the rows
// are about (decision 7).
const (
	nameDefault = "Default: %s"
	keyDefault  = "Default: hardn.yml's key"
)

// draw reads the host and returns the users' screen, its items bound: 1
// opens the user list, 2 Create's form, which says first why the users were
// not read where they were not, and installs shadow first where useradd is
// missing (the control's UseraddRefusal, looked up live).
func (r *runner) draw(ctx context.Context) nav.Screen {
	h := r.read(ctx)
	view := mainView(h)
	view.NoUseradd = errors.Is(h.UseraddRefusal(ctx, r.o.System), usersctl.ErrNoUseradd)
	sub := view.SubMenu()
	create := func() error { return r.create(ctx, h) }
	if h.err != nil {
		create = r.refuse(h.err)
	}
	return sub.Screen(r.o.Prompter.Ctx, bind(sub.Menu.Exit, sub.Menu.Items, map[string]func() error{
		screen.UsersManageItem: func() error { return r.o.Prompter.Run(func() nav.Screen { return r.drawSelect(ctx) }) },
		screen.UsersCreateItem: create,
	}))
}

// refuse returns the action that says err, the control's refusal, before
// asking anything (screenkit.RefusalWords).
func (r *runner) refuse(err error) func() error {
	return r.s.Say(screenkit.Warnings(screenkit.RefusalWords(err)))
}

// drawSelect reads the host and returns the user list: an item per user,
// each opening its screen by name.
func (r *runner) drawSelect(ctx context.Context) nav.Screen {
	h := r.read(ctx)
	view := screen.UsersSelect{}
	if h.err != nil {
		view.NotRead = screenkit.RefusalWords(h.err)
	}
	actions := map[string]func() error{}
	for i, u := range h.Users {
		name := u.Name
		view.Names = append(view.Names, name)
		actions[fmt.Sprint(i+1)] = func() error { return r.o.Prompter.Run(func() nav.Screen { return r.drawManage(ctx, name) }) }
	}
	sub := view.SubMenu()
	return sub.Screen(r.o.Prompter.Ctx, bind(sub.Menu.Exit, sub.Menu.Items, actions))
}

// notListed is why the user named name has no screen in h: the accounts not
// read, or the control's refusal of a user it does not list; "" where it is
// listed.
func (r *runner) notListed(ctx context.Context, h reading, name string) string {
	if h.err != nil {
		return screenkit.RefusalWords(h.err)
	}
	if _, ok := h.user(name); ok {
		return ""
	}
	// A key action's refusal names the user before anything else
	// (Reading.Refusal).
	return screenkit.RefusalWords(h.Refusal(ctx, r.o.System, usersctl.Admin{Action: usersctl.ActionAddKey, User: name}))
}

// drawManage reads the host and returns the screen of the user named name:
// its rights, then its keys. A user no longer listed says why, Return alone.
func (r *runner) drawManage(ctx context.Context, name string) nav.Screen {
	h := r.read(ctx)
	u, _ := h.user(name)
	view := screen.UsersManage{Name: name, Rows: userRows(h.Reading, u), Admin: u.Admin == detect.Yes, NoPassword: u.NoPassword,
		NotRead: r.notListed(ctx, h, name)}
	sub := view.SubMenu()
	return sub.Screen(r.o.Prompter.Ctx, bind(sub.Menu.Exit, sub.Menu.Items, map[string]func() error{
		screen.UsersAdminItem:    r.admin(ctx, h, u),
		screen.UsersPasswordItem: r.password(ctx, h, u),
		screen.UsersKeysItem:     func() error { return r.o.Prompter.Run(func() nav.Screen { return r.drawKeys(ctx, name) }) },
	}))
}

// drawKeys reads the host and returns the keys of the user named name: Add,
// and with any Remove.
func (r *runner) drawKeys(ctx context.Context, name string) nav.Screen {
	h := r.read(ctx)
	u, _ := h.user(name)
	view := screen.UsersKeys{Name: name, Keys: keyRows(u), NotRead: r.notListed(ctx, h, name)}
	sub := view.SubMenu()
	return sub.Screen(r.o.Prompter.Ctx, bind(sub.Menu.Exit, sub.Menu.Items, map[string]func() error{
		screen.UsersAddKeyItem:    func() error { return r.addKey(ctx, h, u) },
		screen.UsersRemoveKeyItem: func() error { return r.removeKey(ctx, h, u) },
	}))
}

// control is the User control planning a, with hardn's environment.
func (r *runner) control(a usersctl.Admin) usersctl.Admin {
	a.Getenv = r.o.Getenv
	return a
}

// admin is item 1: Grant admin rights for a user who is not an admin, its
// review's row the grant the control plans, which asks the user's password,
// set or not (the maintainer, 2026-11-11); Remove for one who is.
func (r *runner) admin(ctx context.Context, h reading, u usersctl.User) func() error {
	if u.Admin == detect.Yes {
		return r.grant(ctx, usersctl.ActionRemove, u, screen.UsersRemoveChange(u.Name, disabledRow(h.Reading)))
	}
	row := enabledRow(h.Reading)
	if u.Password != accounts.PasswordSet {
		row = notSetRow(h.Reading)
	}
	return r.grant(ctx, usersctl.ActionGrant, u, screen.UsersGrantChange(u.Name, row))
}

// password is item 2, for an admin: Ask for password where its rights ask
// none, else Don't ask password.
func (r *runner) password(ctx context.Context, h reading, u usersctl.User) func() error {
	if u.NoPassword {
		return r.grant(ctx, usersctl.ActionAskPassword, u, screen.UsersAskChange(u.Name, enabledRow(h.Reading)))
	}
	return r.grant(ctx, usersctl.ActionNoPassword, u, screen.UsersStopAskingChange(u.Name, nopassRow(h.Reading, noPasswordToAsk(u))))
}

// grant returns the action of an admin rights change for u: its plan,
// planned first from a fresh reading, its refusal (Reading.Refusal's, the
// plan's first) said before asking, which also says whether it is
// lockout-risk; then the change, its words c's.
func (r *runner) grant(ctx context.Context, action usersctl.Action, u usersctl.User, c screen.UsersChange) func() error {
	a := r.control(usersctl.Admin{Action: action, User: u.Name})
	return func() error {
		cs, err := a.Plan(ctx, r.o.System, engine.Input{Facts: r.o.Facts})
		if err != nil {
			return r.refuse(err)()
		}
		return r.change(ctx, risky(cs), func() string { return c.Question }, screenkit.Change{
			Step:     c.Step,
			Flow:     flow.Flow{Review: func() flow.Review { return c.Review }},
			Controls: func(*config.Config) []engine.Control { return []engine.Control{a} },
		})
	}
}

// change runs ch through the flow (screenkit.Session.Change), as Firewall's
// does: the question's words the draw's; y/N, or the hostname typed where
// the plan made at the key is lockout-risk (flow decides from it), and a
// hostname that can't be read then says so in place of the question
// (nav.ErrPhrase, as CantConfirm). With lockout, a plan planned before
// asking being lockout-risk, a live change whose hostname can't be read says
// so before asking anything; plan only confirms nothing, so it needs no
// hostname.
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

// keyField is a pasted key's question, refused at the field by the
// control's check: check, users.ParseKey or, for a user's, its CheckKey.
func keyField(value *string, def, about string, skip func() bool, check func(string) error) *form.Text {
	return &form.Text{Title: screen.UsersKeyField, Description: about, Default: def, Value: value, Skip: skip, Validate: func(s string) error {
		return refusal(check(s))
	}}
}

// parseKey is the key check of a new user, who has no key yet.
func parseKey(s string) error {
	_, err := usersctl.ParseKey(s)
	return err
}

// refusal is a form's refusal of err at its field, in the control's words;
// nil for none. A key the user has already is a warning, as the mock drew
// it.
func refusal(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, usersctl.ErrKeyThere):
		return nav.Warn(screenkit.RefusalWords(err))
	}
	return nav.Refuse(screenkit.RefusalWords(err))
}

// addKey is Add SSH key: the control's refusal before asking; one key,
// refused at its field (CheckKey: not a key, or one the user has, so the
// review never counts a key not added); then the control's plan. A locked user's plan says
// sshd refuses its keys (the control's finding).
func (r *runner) addKey(ctx context.Context, h reading, u usersctl.User) error {
	if err := h.Refusal(ctx, r.o.System, r.control(usersctl.Admin{Action: usersctl.ActionAddKey, User: u.Name})); err != nil {
		return r.refuse(err)()
	}
	var line string
	have := func(s string) error {
		_, err := h.CheckKey(u.Name, s)
		return err
	}
	c := screen.UsersAddKeyChange(u.Name, len(u.Keys))
	return r.change(ctx, false, func() string { return c.Question }, screenkit.Change{
		Step: c.Step,
		Flow: flow.Flow{
			Form:   form.Form{Fields: []form.Field{keyField(&line, "", "", nil, have)}},
			Review: func() flow.Review { return c.Review },
		},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{r.control(usersctl.Admin{Action: usersctl.ActionAddKey, User: u.Name, Key: line})}
		},
	})
}

// removeKey is Remove SSH key: the control's refusal before asking (the
// session user's last key among them), and a key drawn that has left the
// file since (USERS_KEY_GONE), after which the keys are drawn again from a
// fresh reading; which, by fingerprint, each refused at the Select by its
// plan's refusal, planned again at the pick; then the control's plan of that
// fingerprint from a fresh reading. Where removing any key is lockout-risk,
// a live change whose hostname can't be read says so before asking.
func (r *runner) removeKey(ctx context.Context, h reading, u usersctl.User) error {
	if err := h.Refusal(ctx, r.o.System, r.control(usersctl.Admin{Action: usersctl.ActionRemoveKey, User: u.Name})); err != nil {
		return r.refuse(err)()
	}
	if len(u.Keys) == 0 {
		return nil // not reached: Remove SSH key is offered only with a key
	}
	plan := func(i int) ([]engine.Change, error) {
		return r.control(usersctl.Admin{Action: usersctl.ActionRemoveKey, User: u.Name, Fingerprint: u.Keys[i].Fingerprint}).Plan(ctx, r.o.System, engine.Input{Facts: r.o.Facts})
	}
	lockout := false
	opts := make([]form.Option[int], len(u.Keys))
	for i, k := range u.Keys {
		opts[i] = form.Option[int]{Label: k.Fingerprint + " " + k.Comment, Value: i}
		cs, err := plan(i)
		if errors.Is(err, usersctl.ErrKeyGone) {
			return r.refuse(err)() // the keys drawn are stale: said, then drawn again
		}
		lockout = lockout || risky(cs)
	}
	gone := 0
	c := screen.UsersRemoveKeyChange(u.Name, len(u.Keys))
	pick := &form.Select[int]{Title: screen.UsersWhichKeyField, Options: opts, Value: &gone, Validate: func(i int) error {
		_, err := plan(i)
		return refusal(err)
	}}
	return r.change(ctx, lockout, func() string { return c.Question }, screenkit.Change{
		Step: c.Step,
		Flow: flow.Flow{
			Form:   form.Form{Fields: []form.Field{pick}},
			Review: func() flow.Review { return c.Review },
		},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{r.control(usersctl.Admin{Action: usersctl.ActionRemoveKey, User: u.Name, Fingerprint: u.Keys[gone].Fingerprint})}
		},
	})
}

// create is Create User: Figma's questions, the password one in Figma's
// polarity (P-11), asked only after yes to admin rights, its default yes,
// or no where hardn.yml's adminNoPassword is true (the maintainer,
// 2026-11-11: admin rights without a password are opt-in); after yes,
// whether to set the password now, default yes, which runs passwd once the
// plan is applied (setPassword); each answer refused at its field by the
// control (CheckName, ToolRefusal, CheckNew, KeysRefusal, ParseKey); the
// defaults hardn.yml's username and first of sshKeys (decision 7), "Grant
// admin rights?" no as the mocks drew it; one plan for the account, the
// grant and the key, the control's.
// Where useradd is not installed (Alpine without shadow), shadow's install
// first (installShadow); where the lookup fails, its error before asking
// anything, as Add key's refusal.
func (r *runner) create(ctx context.Context, h reading) error {
	switch err := h.UseraddRefusal(ctx, r.o.System); {
	case errors.Is(err, usersctl.ErrNoUseradd):
		return r.installShadow(ctx)
	case err != nil:
		return r.refuse(err)()
	}
	return r.createForm(ctx, h)
}

// installShadow is Create User where useradd is not installed (the
// maintainer, 2026-11-11), as Firewall's Enable firewall installs iproute2
// first: shadow's install planned first, its refusal said before asking;
// its review saying why, y/N; then, the host read again with useradd there,
// Create's own questions and plan. Declined, under plan only (the install
// stops at its plan), or with useradd still missing, it ends there, the
// screen drawn again. -c, -u and Run All refuse instead (UseraddRefusal).
func (r *runner) installShadow(ctx context.Context) error {
	sys := r.o.System
	a := r.control(usersctl.Admin{Action: usersctl.ActionInstallShadow})
	cs, err := a.Plan(ctx, sys, engine.Input{Facts: r.o.Facts})
	if err != nil {
		return r.refuse(err)()
	}
	if len(cs) > 0 {
		c := screen.UsersShadowChange()
		if err := r.change(ctx, false, func() string { return c.Question }, screenkit.Change{
			Step:     c.Step,
			Flow:     flow.Flow{Review: func() flow.Review { return c.Review }},
			Controls: func(*config.Config) []engine.Control { return []engine.Control{a} },
		}); err != nil {
			return err
		}
	}
	after := r.read(ctx)
	switch err := after.UseraddRefusal(ctx, sys); {
	case errors.Is(err, usersctl.ErrNoUseradd):
		return nil
	case err != nil:
		return r.refuse(err)()
	case after.err != nil:
		return r.refuse(after.err)()
	}
	return r.createForm(ctx, after)
}

// createForm is Create's form and plan, useradd there.
func (r *runner) createForm(ctx context.Context, h reading) error {
	sys := r.o.System
	var name, key, nameDef, nameAbout, keyDef, keyAbout string
	var admin, password, setNow, addKey bool
	askDef := true
	if cfg := r.o.Config; cfg != nil {
		askDef = !cfg.AdminNoPassword
		nameDef = cfg.Username
		if nameDef != "" {
			nameAbout = fmt.Sprintf(nameDefault, nameDef)
		}
		if len(cfg.SshKeys) > 0 {
			keyDef, keyAbout = cfg.SshKeys[0], keyDefault
		}
	}
	fields := []form.Field{
		&form.Text{Title: screen.UsersNameField, Description: nameAbout, Default: nameDef, Value: &name, Validate: func(s string) error { return refusal(h.CheckName(ctx, sys, s)) }},
		&form.YesNo{Title: screen.UsersAdminField, Value: &admin, Validate: func(yes bool) error {
			if yes {
				return refusal(h.ToolRefusal())
			}
			return nil
		}},
		&form.YesNo{Title: screen.UsersPasswordField, Default: askDef, Value: &password, Skip: func() bool { return !admin }, Validate: func(yes bool) error {
			return refusal(h.CheckNew(ctx, sys, usersctl.New{Name: name, Admin: admin, NoPassword: !yes}))
		}},
		// Plan only applies nothing, so there is no user to set one for.
		&form.YesNo{TitleFunc: func() string { return screen.UsersSetPasswordField(name) }, Default: true, Value: &setNow,
			Skip: func() bool { return !admin || !password || r.o.DryRun }},
		&form.YesNo{Title: screen.UsersAddKeyField, Default: keyDef != "", Value: &addKey, Validate: func(yes bool) error {
			if yes {
				return refusal(h.KeysRefusal())
			}
			return nil
		}},
		keyField(&key, keyDef, keyAbout, func() bool { return !addKey }, parseKey),
	}
	tool := disabledRow(h.Reading)
	err := r.change(ctx, false, func() string { return screen.UsersCreateQuestion(name) }, screenkit.Change{
		Step: screen.UsersCreateStep(),
		Flow: flow.Flow{
			Form: form.Form{Fields: fields},
			Review: func() flow.Review {
				t := tool
				switch {
				case admin && password:
					t = notSetRow(h.Reading) // a new user has no password yet
				case admin:
					t = nopassRow(h.Reading, true) // nor one to ask (decision 3)
				}
				return screen.UsersCreateReview(t, addKey)
			},
		},
		Controls: func(*config.Config) []engine.Control {
			n := usersctl.New{Name: name, Admin: admin, NoPassword: admin && !password}
			if addKey {
				n.Keys = []string{key}
			}
			return []engine.Control{r.control(usersctl.Admin{Action: usersctl.ActionCreate, New: n})}
		},
	})
	if err != nil || !admin || !password || !setNow || r.o.DryRun {
		return err
	}
	return r.setPassword(ctx, name)
}

// setPassword is Create's last step, where a password is to be set now
// (the maintainer, 2026-11-11): the host's own passwd for name has the
// terminal until it ends (host.System.RunInteractive, as the Logs screen's
// pager), so the password is typed to passwd, never to hardn, and no log
// of hardn's can hold it. Only for a user Create made, read again: one
// there, with no password. Create declined or failed leaves none, and
// nothing runs. passwd failing, or no terminal, is said with how to set it
// later; the user stays, its admin rights waiting for the password.
func (r *runner) setPassword(ctx context.Context, name string) error {
	u, ok := r.read(ctx).user(name)
	if !ok || u.Password != accounts.PasswordNone {
		return nil
	}
	if err := r.s.Say(component.Messages{{Text: screen.UsersSettingPassword(name)}})(); err != nil {
		return err
	}
	if err := r.o.System.RunInteractive(ctx, nil, "passwd", name); err != nil {
		if ctx.Err() != nil {
			return err
		}
		return r.s.Say(screenkit.Warnings(screen.UsersPasswordNotSet(name)))()
	}
	return nil
}
