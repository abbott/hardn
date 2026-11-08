package ssh

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The engine's refusals in the screen's words: a change waiting for hardn
// confirm, decided (gallery-ssh.md §6); a port in use, the adopted mock's.
// The control's refusals are its own, in the same section's words.
const (
	wordsPending = "A change is waiting for hardn confirm. Confirm it from a new connection, or let it roll back, then try again."
	wordsInUse   = "A port the change adds is in use. Pick another."
)

// planWords are a refusal of the engine's plan in the screen's words: a
// change waiting for hardn confirm (an applied lockout-risk change,
// revert.State.Waiting), a port in use; any other change journalled (an
// unfinished rollback, a change that did not finish) and the control's
// refusals as they are (screenkit.RefusalWords), revert's naming its
// hardn rollback.
func planWords(err error) string {
	var pe *revert.PendingError
	switch {
	case errors.As(err, &pe) && pe.State.Waiting():
		return wordsPending
	case errors.Is(err, reload.ErrInUse):
		return wordsInUse
	}
	return screenkit.RefusalWords(err)
}

// draw reads the host and returns the screen, its items bound.
func (r *runner) draw(ctx context.Context) nav.Screen {
	h := r.read(ctx)
	sub := mainView(h).SubMenu()
	actions := map[string]func() error{
		screen.SSHRootItem:     func() error { return r.root(ctx, h) },
		screen.SSHPasswordItem: func() error { return r.password(ctx, h) },
		screen.SSHPortItem:     func() error { return r.port(ctx, h) },
		screen.SSHMigrateItem:  func() error { return r.migrate(ctx, h) },
		screen.SSHUpgradeItem:  func() error { return r.upgrade(ctx, h) },
	}
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: actions[it.Key]})
	}
	return sub.Screen(r.o.Prompter.Ctx, menu)
}

// refuse says words, a refusal, before asking anything.
func (r *runner) refuse(words string) error {
	return r.s.Say(screenkit.Warnings(words))()
}

// preplan plans c on the engine from a fresh reading of the host, writing
// nothing outside the engine's run directory, which it removes, and returns
// its first refusal in the screen's words, "" for none, and the plan, nil
// where the engine refused to plan. The control's refusals and the
// engine's (a change waiting for hardn confirm among them) are said before
// asking.
func (r *runner) preplan(ctx context.Context, c engine.Control) (string, *engine.Plan, error) {
	p, err := r.o.NewEngine(func(engine.Event) {}).Plan(ctx, []engine.Control{c}, r.cfg)
	switch {
	case err != nil && ctx.Err() != nil:
		return "", nil, err
	case err != nil:
		return planWords(err), nil, nil
	case len(p.Refused) > 0:
		return planWords(p.Refused[0]), p, nil
	}
	return "", p, nil
}

// change runs c through the flow (screenkit.Session.Change), as Firewall's
// does: an item with no field planned first (preplan) and refused before
// asking; its review; its plan from a fresh reading; the question, the
// hostname typed where the plan is lockout-risk (flow decides from it), and
// a hostname that can't be read then says so in place of the question
// (nav.ErrPhrase, as CantConfirm). With lockout, a live change whose
// hostname can't be read says so before asking anything; plan only
// confirms nothing, so it needs no hostname.
func (r *runner) change(ctx context.Context, lockout bool, c screen.SSHChange, fields []form.Field, control func() engine.Control) error {
	if len(fields) == 0 {
		words, p, err := r.preplan(ctx, control())
		switch {
		case err != nil:
			return err
		case words != "":
			return r.refuse(words)
		}
		lockout = p.Risk == engine.RiskLockout
	}
	if lockout && !r.o.DryRun {
		if _, _, err := screenkit.Hostname(ctx, r.o.System); err != nil {
			return r.s.CantConfirm(err)
		}
	}
	var phraseErr error
	confirm := func(p flow.Plan) flow.Confirm {
		cf := flow.Confirm{Question: c.Question}
		if p.Typed() {
			cf.Label = screenkit.TypeTheHostname
			cf.Phrase, cf.Short, phraseErr = screenkit.Hostname(ctx, r.o.System)
		}
		return cf
	}
	err := r.s.Change(ctx, screenkit.Change{
		Step:     c.Step,
		Flow:     flow.Flow{Form: form.Form{Fields: fields}, Review: func() flow.Review { return c.Review }, Confirm: confirm},
		Controls: func(*config.Config) []engine.Control { return []engine.Control{control()} },
	})
	if phraseErr != nil && errors.Is(err, nav.ErrPhrase) {
		return r.s.CantConfirm(phraseErr)
	}
	return err
}

// root is item 1: Disallow root login, or Allow root key-only where root
// login is disallowed; on Proxmox VE, Root key-only (sshchange.Root, which
// refuses).
func (r *runner) root(ctx context.Context, h reading) error {
	v := mainView(h)
	want, value := "no", disallowed
	switch v.Root {
	case screen.SSHPVERoot:
		want, value = pve.RootLogin, keyOnly
	case screen.SSHAllowRootKeyOnly:
		want, value = "prohibit-password", keyOnly
	}
	return r.change(ctx, true, screen.SSHRootChange(v.Root, value), nil, func() engine.Control {
		return sshchange.Root{Want: want, Getenv: r.o.Getenv}
	})
}

// password is item 2: Key-only login, or Password login on where
// passwords don't log in (sshchange.Auth).
func (r *runner) password(ctx context.Context, h reading) error {
	action, value := sshchange.AuthKeyOnly, off
	if mainView(h).PasswordsOff {
		action, value = sshchange.AuthPasswordsOn, on
	}
	return r.change(ctx, true, screen.SSHPasswordChange(action == sshchange.AuthPasswordsOn, value), nil, func() engine.Control {
		return sshchange.Auth{Action: action, Getenv: r.o.Getenv}
	})
}

// upgrade is item 5, Upgrade openssh, on Alpine where sshd lacks
// CVE-2026-123158's fix (sshchange.Upgrade): planned first and refused
// before asking, then a plain question, as nothing of it is lockout-risk.
func (r *runner) upgrade(ctx context.Context, h reading) error {
	fix := h.Fix
	return r.change(ctx, false, screen.SSHUpgradeChange(fix.Package, fix.Installed, fix.Wanted), nil, func() engine.Control {
		return sshchange.Upgrade{}
	})
}

// port is item 3, from a fresh reading of the host, not the draw's:
// refused before its field as sshchange.Port refuses any port (planned for
// the port sshd has, which plans nothing else), then the new port, refused
// at its field outside 1 to 65535 and as the engine refuses its plan
// (planWords: a port in use, a change waiting for hardn confirm, the
// control's), where sshd listens on it already an empty plan; its default
// hardn.yml's sshPort where sshd does not listen on it (decision 9 (a));
// then its plan from a fresh reading, the ports sshd has held until
// confirmed, of lockout risk.
func (r *runner) port(ctx context.Context, _ reading) error {
	rd, err := sshchange.Read(ctx, r.o.System, r.o.Facts, r.o.Config, r.o.Session, r.o.Getenv)
	if err != nil {
		return err
	}
	h := reading{Reading: rd}
	have := h.Effective.Listeners.Ports()
	now := uint16(22)
	if len(have) > 0 {
		now = have[0]
	}
	words, _, err := r.preplan(ctx, sshchange.Port{To: now})
	switch {
	case err != nil:
		return err
	case words != "":
		return r.refuse(words)
	}
	var value, def, about string
	if p := r.cfg.SshPort; p >= 1 && p <= 65535 && !slices.Contains(have, uint16(p)) {
		def, about = strconv.Itoa(p), screen.SSHPortDefault(p)
	}
	field := &form.Text{Title: screen.SSHPortField, Description: about, Default: def, Value: &value, Validate: func(s string) error {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 1 || n > 65535 {
			return nav.Refuse(screen.SSHPortOutOfRange)
		}
		words, p, err := r.preplan(ctx, sshchange.Port{To: uint16(n)})
		switch {
		case err != nil:
			return err
		case words != "":
			return nav.Refuse(words)
		case len(p.Changes) == 0:
			return nav.Warn(screen.SSHPortHave(uint16(n)))
		}
		return nil
	}}
	return r.change(ctx, true, screen.SSHPortChange(portWords(h)), []form.Field{field}, func() engine.Control {
		n, _ := strconv.Atoi(strings.TrimSpace(value))
		return sshchange.Port{To: uint16(n)}
	})
}

// migrate is item 4: hardn's old files moved into 00-hardn.conf
// (sshchange.Migrate).
func (r *runner) migrate(ctx context.Context, h reading) error {
	return r.change(ctx, true, screen.SSHMigrateChange(oldNames(h)), nil, func() engine.Control {
		return sshchange.Migrate{ConfigFile: r.cfg.SshConfigFile}
	})
}
