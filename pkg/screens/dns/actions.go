package dns

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/abbott/hardn/pkg/engine"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/engine/steps"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The items' words: origin/main's where it has them (pkg/menu
// dns_menu.go), the others proposed (docs/engine/screen-dns.md §5).
const (
	// addFirst is Configure DNS's refusal with no nameserver configured:
	// origin/main's second sentence, its first ("No nameservers
	// configured.") left out, as the screen's muted item says it
	// (decision 3).
	addFirst = "Please add nameservers first."
	// lastOne is Remove nameserver's refusal with one configured, whose
	// plan would leave none (decision 1 (ii); proposed).
	lastOne = "The last nameserver cannot be removed."
	// Add nameserver's field (origin/main's) and its inline errors:
	// origin/main's, "already configured" a warning; addUnusable for an
	// address dns.Validate refuses that origin/main took (decision 9),
	// and addMany for a list a file cannot take, in the DNS control's
	// words (DNS_INVALID's Why), both proposed.
	addTitle    = "Enter nameserver IP address"
	addEmpty    = "Nameserver cannot be empty"
	addFormat   = "Invalid IP address format"
	addTwice    = "Nameserver %s is already configured"
	addUnusable = "Not a usable nameserver address"
	addMany     = "%s takes %d nameservers at most"
	// removeTitle is Remove nameserver's field (proposed, after
	// origin/main's "Select a key to remove").
	removeTitle = "Select a nameserver to remove"
	// stopUDHCPC is Alpine's question, on every change's form where
	// udhcpc rewrites the file (decision 6; proposed).
	stopUDHCPC = "Stop udhcpc rewriting /etc/resolv.conf?"
	// unreadFix and whatFailed are the refusal of a host whose DNS
	// settings could not all be read (DNS_UNKNOWN) on the screen, in
	// place of the control's Fix, "see the log for what failed": before a
	// plan nothing records what failed, so the screen says it (proposed).
	unreadFix  = "Correct what failed, then try again."
	whatFailed = "What failed: "
)

// action returns what choosing the item keyed key, labelled label, does
// on the host h read: on a host every plan refuses, say why; with no
// hardn.yml read, every item that saves the list says it cannot; else the
// item's change, or its refusal before it asks.
func (r *runner) action(ctx context.Context, h reading, key, label string) func() error {
	if h.refused != nil {
		return r.say(h.refused...)
	}
	if h.notLoaded != nil && key != screen.DNSConfigure {
		return r.say(h.notLoaded...)
	}
	configured := h.view.Configured
	asked := func() string { return label + "?" } // "Configure DNS?", "Use Cloudflare DNS?"
	switch key {
	case screen.DNSConfigure:
		if len(configured) == 0 {
			return r.say(addFirst)
		}
		return func() error {
			return r.change(ctx, h, label, nil, func() []string { return slices.Clone(configured) }, asked)
		}
	case screen.DNSAdd:
		return func() error {
			var entry string
			field := &form.Text{Title: addTitle, Validate: addCheck(configured, h.manager), Value: &entry}
			list := func() []string { return append(slices.Clone(configured), canonical(entry)) }
			return r.change(ctx, h, label, []form.Field{field}, list, func() string { return label + " " + canonical(entry) + "?" })
		}
	case screen.DNSRemove:
		if len(configured) == 1 {
			return r.say(lastOne)
		}
		return func() error {
			var gone int
			opts := make([]form.Option[int], len(configured))
			for i, ns := range configured {
				opts[i] = form.Option[int]{Label: ns, Value: i}
			}
			field := &form.Select[int]{Title: removeTitle, Options: opts, Value: &gone}
			list := func() []string { return slices.Delete(slices.Clone(configured), gone, gone+1) }
			return r.change(ctx, h, label, []form.Field{field}, list, func() string { return label + " " + configured[gone] + "?" })
		}
	}
	for _, p := range screen.DNSPresets() {
		if p.Key == key {
			return func() error {
				return r.change(ctx, h, label, nil, func() []string { return slices.Clone(p.Nameservers) }, asked)
			}
		}
	}
	return func() error { return fmt.Errorf("dns screen: item %s has no action", key) } // not reached: SubMenu's items are those above
}

// change runs the change of the item labelled label on the host h read,
// under its title bar: the form, fields then, where udhcpc rewrites the
// file, Alpine's question; the review of the list list returns and the
// plan's findings; the plan, the DNS control's on the engine
// (output.FlowEngine), from a copy of hardn.yml with that list, built
// once the form is answered; question, y/N; the apply. Then what the flow
// does not draw (output.AfterFlow), what the change did given to Ran,
// and, once it applied, hardn.yml adopted as saved (adopt). It returns
// the flow's error: nav.ErrBack from the form's first field, which goes
// back to the screen, nav.ErrQuit, an abort, or a write that failed.
func (r *runner) change(ctx context.Context, h reading, label string, fields []form.Field, list func() []string, question func() string) error {
	o := r.o
	stop := new(bool)
	if h.udhcpc {
		fields = append(fields, &form.YesNo{Title: stopUDHCPC, Value: stop})
	}
	edited := *o.Config
	fe := &output.FlowEngine{DryRun: o.DryRun, LogPath: o.LogPath}
	e := &planning{FlowEngine: fe, build: func() {
		if fe.Engine == nil {
			fe.Engine = o.NewEngine(fe.Progress)
		}
		edited.Nameservers = list()
		fe.Controls, fe.Config = []engine.Control{dnsctl.Nameservers{Config: o.Config, StopUDHCPC: *stop}}, &edited
	}}
	f := flow.Flow{
		Form:    form.Form{Fields: fields},
		Review:  func() flow.Review { return h.view.Review(canonicalList(list(), h.manager)) },
		Engine:  e,
		Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: question()} },
		DryRun:  o.DryRun,
	}
	out, err := h.view.Step(label).Run(ctx, o.Prompter, f)
	if !e.planned {
		return err
	}
	run := fe.Run(out, err)
	if after := output.AfterFlow(o.Prompter.Ctx.Env, run, o.Now()); after != "" {
		if werr := r.write(after); err == nil {
			err = werr
		}
	}
	if o.Ran != nil {
		o.Ran(run)
	}
	if out == flow.Applied && !o.DryRun {
		r.adopt(ctx, &edited)
	}
	return err
}

// planning is a change's flow.Engine: output.FlowEngine, its engine,
// controls and configuration built when the flow plans, once the form is
// answered (build), as FlowEngine reads them then.
type planning struct {
	*output.FlowEngine
	build   func()
	planned bool
}

// Plan builds the engine's inputs from the answers, then plans as
// FlowEngine does.
func (p *planning) Plan(ctx context.Context) (flow.Plan, error) {
	p.build()
	p.planned = true
	return p.FlowEngine.Plan(ctx)
}

// say returns an action that writes paragraphs as one warning, set apart
// as an action's result is (nav.Prompter.Run), and goes back to the
// screen.
func (r *runner) say(paragraphs ...string) func() error {
	msgs := make(component.Messages, len(paragraphs))
	for i, p := range paragraphs {
		msgs[i] = component.Message{Kind: component.MessageWarn, Text: p}
	}
	return func() error {
		return r.write(strings.Repeat("\n", theme.SectionGap) + msgs.Render(r.o.Prompter.Ctx) + "\n")
	}
}

// hostWords are the words every item says before asking on the host
// dns.Read read as s, with readErr, when every plan there refuses
// whatever its list (dnsctl.HostRefusal); nil when none does. They are
// the refusal's (refusalWords), but for a fact not established
// (DNS_UNKNOWN): its Why, then the screen's Fix, and what failed, as the
// host's error says it (readErr, else s.Err, else the refusal's own),
// where the control's Fix points at a log that holds nothing before a
// plan.
func hostWords(s hostdns.State, readErr error) []string {
	err := dnsctl.HostRefusal(s, readErr)
	var rf *steps.Refusal
	switch {
	case err == nil:
		return nil
	case !errors.As(err, &rf) || !errors.Is(err, hostdns.ErrUnknown):
		return []string{refusalWords(err)}
	}
	cause := readErr
	if cause == nil {
		cause = s.Err
	}
	if cause == nil {
		cause = rf.Err
	}
	return []string{fullStop(rf.Why) + " " + unreadFix, fullStop(whatFailed + strings.ReplaceAll(cause.Error(), "\n", "; "))}
}

// notLoaded are the words of the save's refusal of o.Config when it was
// not read from a file (steps.PlanSave's, CONFIG_NOT_LOADED), which every
// item that saves the list says before asking; nil when it was. PlanSave
// refuses it before it reads anything: with no path there is no file.
func notLoaded(ctx context.Context, o Options) []string {
	if o.Config.Path() != "" {
		return nil
	}
	_, err := steps.PlanSave(ctx, o.System, dnsctl.ChangeSave, o.Config, []string{dnsctl.Setting})
	if err == nil {
		return nil // not reached: PlanSave refuses a configuration with no path
	}
	return []string{refusalWords(err)}
}

// refusalWords are a refusal's words on the screen: its Why, then its
// Fix, each a sentence ("NetworkManager manages DNS here. Set the
// nameservers with nmcli."), as the adopted mock words NetworkManager's
// (decision 4); its code is left to what shows a run in full. The Why is
// written as it starts, a program's name in its own case ("resolvconf
// manages DNS here."); the Fix, written to follow "To go on: ", starts
// with a capital. An error that is no refusal says itself.
func refusalWords(err error) string {
	var rf *steps.Refusal
	if !errors.As(err, &rf) || rf.Why == "" {
		return fullStop(err.Error())
	}
	if rf.Fix == "" {
		return fullStop(rf.Why)
	}
	first, n := utf8.DecodeRuneInString(rf.Fix)
	return fullStop(rf.Why) + " " + fullStop(string(unicode.ToUpper(first))+rf.Fix[n:])
}

// fullStop is s ending with one full stop.
func fullStop(s string) string {
	return strings.TrimSuffix(strings.TrimSpace(s), ".") + "."
}

// addCheck is Add nameserver's check of the entry typed, after the
// configured list, on a host whose /etc/resolv.conf manager is m: an
// entry dns.Validate refuses, refused in the words of its kind
// (decision 9): none; not an address; one already configured, a warning
// (nav.Warn), as origin/main warns of it; an address a resolver cannot
// use as given; one more than a file takes. A list configured that
// dns.Validate refuses already takes any usable entry: the plan refuses
// the list, naming the entry at fault, with how to correct it.
func addCheck(configured []string, m hostdns.Manager) func(string) error {
	have := make([]string, len(configured))
	for i, ns := range configured {
		have[i] = canonical(ns)
	}
	valid := func(list ...string) bool {
		_, err := hostdns.Validate(hostdns.Config{Nameservers: list}, m)
		return err == nil
	}
	return func(entry string) error {
		a, err := netip.ParseAddr(entry)
		switch {
		case entry == "":
			return errors.New(addEmpty)
		case err != nil:
			return errors.New(addFormat)
		case slices.Contains(have, a.String()):
			return nav.Warn(fmt.Sprintf(addTwice, a))
		case !valid(entry):
			return errors.New(addUnusable)
		case len(configured) > 0 && valid(configured...) && !valid(append(slices.Clone(configured), entry)...):
			return fmt.Errorf(addMany, hostdns.ResolvConf, hostdns.MaxNameservers)
		}
		return nil
	}
}

// canonical is entry as dns.Validate writes it, netip's form ("2001:db8::54"
// for "2001:DB8:0::54"); entry itself when it is no address.
func canonical(entry string) string {
	if a, err := netip.ParseAddr(entry); err == nil {
		return a.String()
	}
	return entry
}

// canonicalList is list as dns.Validate writes it for manager m, the
// nameservers the host will use; list itself when it refuses it, which
// the plan then refuses with its report.
func canonicalList(list []string, m hostdns.Manager) []string {
	if v, err := hostdns.Validate(hostdns.Config{Nameservers: list}, m); err == nil {
		return v.Nameservers
	}
	return list
}
