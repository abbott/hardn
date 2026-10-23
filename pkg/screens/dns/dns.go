// Package dns runs hardn's DNS screen on a host (refactor plan slice 2,
// the DNS pilot, unit (c), next/s2-screen): origin/main's screen as the
// maintainer adopted its mock on 2026-10-22 (docs/engine/gallery-dns.md
// §4), drawn by pkg/ui/screen (screen.DNS) from what the DNS control
// (pkg/engine/controls/dns) and pkg/host/dns read of the host, and each of
// its items a change through the plan/apply engine in pkg/ui's canonical
// flow (output.FlowEngine): the form, the review with the plan's findings
// before the confirmation, y/N, the apply, hardn.yml saved after the
// change in the same plan, and the screen drawn again under the result
// from a fresh reading of the host, with hardn.yml's list as the
// configuration holds it, the saved one once a change saved it. Run is
// the one entry point, which cmd/hardn's DNS menu item calls (unit (d));
// docs/engine/screen-dns.md has its states, its words and its API.
//
// It reaches the host only through host.System, the control, pkg/host/dns
// and the engine (depguard rules screens-dns-through-host and
// screens-through-host), and renders only through pkg/ui and pkg/output.
package dns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// Options is what the DNS screen runs with: what cmd/hardn has when its
// DNS menu item is chosen.
type Options struct {
	// Prompter asks the questions and writes the screen through the one
	// line reader, rendering with its Ctx, whose Env is the terminal's.
	Prompter nav.Prompter
	// System is the host: the screen reads it, and the engine changes it.
	System host.System
	// Facts are detect's, which the screen reads the host with
	// (dns.Read, the control's Check); the engine establishes its own
	// when it plans.
	Facts detect.Facts
	// Config is hardn.yml as loaded, whose list the screen shows and its
	// items act on. A change plans a copy with the list it asks for; once
	// the change has applied and hardn.yml holds the bytes it saved,
	// Config adopts the copy (the list, and the bytes it records), so
	// that the next change's save is not refused as CONFIG_CHANGED. Its
	// other settings stay as they are in memory: the save writes the list
	// alone (dnsctl.Setting), so a flag's value is never saved. Required.
	Config *config.Config
	// NewEngine returns the engine a change plans and applies with, built
	// as the caller builds it (engine.New over System with its options),
	// progress as its Options.Progress, so that the flow shows each step;
	// one per change, made when the change is planned. Required.
	NewEngine func(progress func(engine.Event)) *engine.Engine
	// DryRun stops each change at its plan: nothing applied or saved.
	DryRun bool
	// LogPath is the log a failure's report names; "" names none.
	LogPath string
	// Now is the clock output.AfterFlow counts from; nil is time.Now.
	Now func() time.Time
	// Ran, when set, is given what each change did once its flow has
	// ended, for a change that was planned: for the audit log
	// (output.NewRecord, output.Append), whose caller adds the host's
	// facts and hardn's version, or for a record of the applies.
	Ran func(output.Run)
}

// Run shows the DNS screen and runs the items the user chooses, until
// they go back with 0 (nil) or quit with q (nav.ErrQuit); the end of the
// input and Ctrl-C abort (prompt.ErrAborted). Each round reads the host
// again and draws the screen under what the last item wrote, so that a
// change's result stays above the screen and the screen shows the host as
// it is after it (W1, W2; nav.Prompter.Run). hardn.yml's list is the one
// o.Config holds: the file is read back only after a change, to adopt
// what it saved (Options.Config), so an edit made to it elsewhere while
// the screen is open is not shown, and a change that saves the list is
// then refused as CONFIG_CHANGED. Every change is planned, reviewed,
// confirmed y/N and applied by the engine, hardn.yml saved after it in
// the same plan; n changes and saves nothing, and a dry run stops at the
// plan. An item every plan would refuse (dnsctl.HostRefusal: a manager
// hardn only reports, a container, a family hardn does not configure, a
// fact not established, systemd-resolved stopped or overridden), an item
// that saves the list with no hardn.yml read, Configure DNS with no
// nameserver configured and Remove nameserver with one are refused
// before anything is asked, with a warning. Run returns an error wrapping
// fs.ErrInvalid, writing nothing, without a Prompter's In and Out, a
// System, a Config or NewEngine.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil || o.Config == nil || o.NewEngine == nil {
		return fmt.Errorf("dns screen: %w: it needs a Prompter with In and Out, a System, a Config and NewEngine", fs.ErrInvalid)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	r := &runner{o: o}
	return o.Prompter.Run(func() nav.Screen { return r.draw(ctx) })
}

// runner is one run of the DNS screen.
type runner struct {
	o Options
}

// draw reads the host and returns the screen, with hardn.yml's list as
// the configuration holds it, its items bound to their actions.
func (r *runner) draw(ctx context.Context) nav.Screen {
	h := r.read(ctx)
	sub := h.view.SubMenu()
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: r.action(ctx, h, it.Key, it.Label)})
	}
	return sub.Screen(r.o.Prompter.Ctx, menu)
}

// reading is one reading of the host, with hardn.yml's list as the
// configuration holds it: the screen's view of them, and what its items
// need.
type reading struct {
	view screen.DNS
	// manager is what manages /etc/resolv.conf, which dns.Validate checks
	// a list for.
	manager hostdns.Manager
	// refused are the words of the refusal every plan on the host makes
	// whatever its list (dnsctl.HostRefusal, hostWords): every item says
	// them before asking; nil when there is none.
	refused []string
	// notLoaded are the words of the save's refusal with no hardn.yml
	// read (steps.PlanSave, CONFIG_NOT_LOADED): every item that saves the
	// list, all but Configure DNS, says them before asking; nil when
	// hardn.yml was read.
	notLoaded []string
	// udhcpc: Alpine's udhcpc rewrites /etc/resolv.conf on each lease, so
	// every change asks whether to stop it (decision 6).
	udhcpc bool
}

// managers are the screen's names of pkg/host/dns's managers.
var managers = map[hostdns.Manager]screen.DNSManager{
	hostdns.ManagerFile: screen.DNSFile, hostdns.ManagerProxmox: screen.DNSProxmox, hostdns.ManagerResolved: screen.DNSResolved,
	hostdns.ManagerNetworkManager: screen.DNSNetworkManager, hostdns.ManagerResolvconf: screen.DNSResolvconf, hostdns.ManagerForeign: screen.DNSOther,
}

// read reads the host's DNS (dns.Read) and its status (the control's
// Check, against hardn.yml as the configuration holds it): what manages
// /etc/resolv.conf, and the nameservers and search domains in use only
// where they were read, the file's or systemd-resolved's while it runs,
// and the control's Check is not Unknown; Unknown otherwise, never a
// guess (a manager hardn only reports, a fact not established). A file's
// nameservers are its first dns.MaxNameservers, the ones glibc and musl
// use. hardn.yml's list is the configuration's, not read from the file.
func (r *runner) read(ctx context.Context) reading {
	o := r.o
	s, err := hostdns.Read(ctx, o.System, o.Facts)
	st, cerr := dnsctl.Nameservers{Config: o.Config}.Check(ctx, o.System, o.Facts)
	h := reading{manager: s.Manager, refused: hostWords(s, err), notLoaded: notLoaded(ctx, o),
		udhcpc: err == nil && s.Host.Distro == detect.DistroAlpine && s.Manager == hostdns.ManagerFile && s.UDHCPC.Rewrites == detect.Yes}
	h.view = screen.DNS{Manager: managers[s.Manager], Configured: slices.Clone(o.Config.Nameservers)}
	switch {
	case errors.Is(err, hostdns.ErrContainer):
		h.view.Manager = screen.DNSContainer
	case err != nil:
		h.view.Manager = screen.DNSManagerUnknown
	}
	switch {
	case err != nil, cerr != nil, st.State == engine.Unknown, s.Err != nil:
	case s.Manager == hostdns.ManagerFile, s.Manager == hostdns.ManagerProxmox:
		h.view.Read = true
		h.view.InUse, h.view.Search = own(s.Nameservers[:min(len(s.Nameservers), hostdns.MaxNameservers)]), own(s.Search)
	case s.Manager == hostdns.ManagerResolved && s.Resolved.Active == detect.Yes:
		h.view.Read = true
		h.view.InUse, h.view.Search = own(s.Resolved.Global), own(searchOnly(s.Resolved.Domains))
		for _, l := range s.Resolved.Links {
			h.view.InUse = append(h.view.InUse, linked(l, l.Servers)...)
			h.view.Search = append(h.view.Search, linked(l, searchOnly(l.Domains))...)
		}
	}
	return h
}

// own are values as the host's own list's entries.
func own(values []string) []screen.DNSEntry {
	return linked(hostdns.Link{}, values)
}

// linked are values as the entries of link l: its name, or "delegate
// <id>" for a delegate, as the DNS findings name it (steps.DNSFinding);
// the zero Link is the host's own list.
func linked(l hostdns.Link, values []string) []screen.DNSEntry {
	name := l.Name
	if l.Index == 0 && l.Name != "" {
		name = "delegate " + l.Name
	}
	out := make([]screen.DNSEntry, len(values))
	for i, v := range values {
		out[i] = screen.DNSEntry{Value: v, Link: name}
	}
	return out
}

// searchOnly are domains without systemd-resolved's route-only ones
// ("~example.net", "~."), which route queries and are no search domain.
func searchOnly(domains []string) []string {
	return slices.DeleteFunc(slices.Clone(domains), func(d string) bool { return strings.HasPrefix(d, "~") })
}

// write writes s to the screen's writer.
func (r *runner) write(s string) error {
	if _, err := io.WriteString(r.o.Prompter.Out, s); err != nil {
		return fmt.Errorf("dns screen: write: %w", err)
	}
	return nil
}

// adopt makes the screen's configuration edited, the copy a change
// planned, once hardn.yml at its path holds the bytes edited saved there
// and not the ones the configuration records: the change saved it, and
// the engine did not roll the save back. A configuration not loaded from
// a file, or a file that cannot be read, adopts nothing; the next change
// is then planned from the configuration as it was, and its save refused
// if the file changed (CONFIG_CHANGED).
func (r *runner) adopt(ctx context.Context, edited *config.Config) {
	c := r.o.Config
	if c.Path() == "" {
		return
	}
	data, _, err := r.o.System.ReadFileNoFollow(ctx, c.Path())
	if err != nil || c.LoadedFrom(data) || !edited.LoadedFrom(data) {
		return
	}
	*c = *edited
}
