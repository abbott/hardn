// Package dns holds the DNS control (refactor plan slice 2, the DNS pilot,
// unit (b), next/s2-control): the DNS row of the status model, and the plan
// that brings the host's nameservers to hardn.yml's.
//
// Check reads what manages /etc/resolv.conf and the nameservers in use
// (pkg/host/dns's Read): Configured when they are hardn.yml's, in order;
// Not Configured otherwise, or when hardn.yml lists none; Not Applicable
// in a container, off the four families, and where a manager hardn only
// reports owns the file (resolvconf, NetworkManager, another program:
// P-37 (a)); Unknown, "can't read", when it cannot be read. Plan is
// dns.PlanDNS for in.Config's list through steps.DNS, then, when that list
// is not the one hardn.yml holds, the save of hardn.yml after it
// (steps.PlanSave); every refusal comes before any change, with a report
// (steps.Refusal). The host's search domains are kept (decision 2).
// docs/engine/control-dns.md has every status line, refusal and word, all
// proposed.
//
// The package reaches the host only through host.System (depguard
// controls-through-host and dns-control-through-host) and renders nothing.
package dns

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
)

// ID is the control's ID, stable once released.
const ID = "dns.nameservers"

// The ids of the changes Plan makes.
const (
	ChangeSet  = ID + "/set"  // the host's nameservers (steps.DNS)
	ChangeSave = ID + "/save" // hardn.yml's list (steps.PlanSave)
)

// Setting is the hardn.yml setting the control saves, alone: the save
// writes no other, whatever in.Config holds (steps.PlanSave).
const Setting = "nameservers"

// The changes' titles (gallery-dns.md §5; proposed), by the steps the
// change holds: the nameservers set (the file, or resolved's drop-in or
// reload), udhcpc stopped (Alpine, StopUDHCPC), or both.
const (
	TitleSet     = "Set the nameservers"
	TitleSetStop = "Set nameservers; stop udhcpc rewriting"
	TitleStop    = "Stop udhcpc rewriting resolv.conf"
)

// The row's details (proposed wording, control-dns.md §5). A Configured
// row's detail is the list in use, "192.0.2.54, 2001:db8::54".
const (
	DetailNone           = "none in hardn.yml"      // NotConfigured: hardn.yml lists no nameserver
	DetailNotHardn       = "not hardn.yml's"        // NotConfigured: the nameservers in use are others, or in another order
	DetailInvalid        = "not valid in hardn.yml" // NotConfigured: hardn.yml's list is one dns.Validate refuses here
	DetailNotRunning     = "resolved not running"   // NotConfigured: /etc/resolv.conf is systemd-resolved's link, and it does not run
	DetailContainer      = "container"              // NotApplicable: the container's manager provides the file
	DetailUnsupported    = "not supported"          // NotApplicable: not Debian, Ubuntu, Alpine or Proxmox VE
	DetailNetworkManager = "NetworkManager"         // NotApplicable: reported only (P-37 (a))
	DetailResolvconf     = "resolvconf"             // NotApplicable: reported only
	DetailOther          = "another program"        // NotApplicable: a link hardn does not know, or a file resolved reads as another's
	DetailCantRead       = "can't read"             // Unknown
)

// The codes of the refusals, the registry's for the sentinels they wrap
// (pkg/output; pinned by the tests): a Refusal's Code is informative, its
// code is its sentinel's.
const (
	codeContainer   = "DNS_CONTAINER"
	codeUnsupported = "DNS_UNSUPPORTED"
	codeManaged     = "DNS_MANAGED"
	codeNotRunning  = "DNS_RESOLVED_NOT_RUNNING"
	codeOverridden  = "DNS_OVERRIDDEN"
	codeBlocked     = "DNS_DROPIN_BLOCKED"
	codeNone        = "DNS_NO_NAMESERVERS"
	codeOption      = "DNS_OPTION_NOT_APPLICABLE"
	codeInvalid     = "DNS_INVALID"
	codeUnknown     = "DNS_UNKNOWN"
)

// reportedOnly are the managers hardn reports and does not write, with the
// row's detail (P-37 (a), decided; slice 2 decision 8: Not Applicable).
var reportedOnly = map[hostdns.Manager]string{hostdns.ManagerNetworkManager: DetailNetworkManager,
	hostdns.ManagerResolvconf: DetailResolvconf, hostdns.ManagerForeign: DetailOther}

// Controls is the DNS row, checked against cfg, hardn.yml as loaded.
func Controls(cfg *config.Config) []engine.Control { return []engine.Control{Nameservers{Config: cfg}} }

// Nameservers is DNS: whether the host uses hardn.yml's nameservers, and
// the change that makes it.
//
// The engine's Check takes no configuration, so the control carries what
// Check compares with and what the flow asked: no engine API changes for
// either. Plan reads in.Config, the engine's, which the caller gives
// Engine.Plan from the same hardn.yml, or as a copy with the list edited
// (Add nameserver, a preset); the engine plans again under the run lock
// with the same control values, so the answer shown is the one applied.
type Nameservers struct {
	// Config is hardn.yml as loaded, whose list Check compares the host's
	// with; nil lists none.
	Config *config.Config
	// StopUDHCPC is the answer to "Stop udhcpc rewriting
	// /etc/resolv.conf?", asked on Alpine only (slice 2 decision 6, no
	// configuration key): yes adds RESOLV_CONF="no" to udhcpc.conf in the
	// same change (dns.Config.UDHCPCNoResolv). Refused on another host
	// (DNS_OPTION_NOT_APPLICABLE).
	StopUDHCPC bool
}

// ID returns ID.
func (Nameservers) ID() string { return ID }

// Title is the row's label.
func (Nameservers) Title() string { return "DNS" }

// Applies is true on every host. A container, a family hardn does not
// configure and the managers hardn only reports are Not Applicable through
// Check (decision 8) and refused by Plan with a report (P-37 (a)): a
// control that does not apply plans nothing, and an empty plan reads
// "Nothing needs changing." on a host whose nameservers are not
// hardn.yml's.
func (Nameservers) Applies(detect.Facts) (bool, string) { return true, "" }

// list is cfg's nameservers; none for no configuration.
func list(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	return cfg.Nameservers
}

// Check reads the host's DNS and compares the nameservers in use (the
// file's, or systemd-resolved's global ones) with hardn.yml's, in order,
// as dns.Validate writes them: resolvers query them in that order, and the
// plan changes the host when the order differs. Each finding of the host
// (dns.State.Findings) explains the line; none sets it.
func (n Nameservers) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	s, err := hostdns.Read(ctx, sys, f)
	switch {
	case errors.Is(err, hostdns.ErrContainer):
		return engine.Status{State: engine.NotApplicable, Detail: DetailContainer, Evidence: []string{"container: " + f.Container.Signal}}, nil
	case errors.Is(err, hostdns.ErrUnsupported): // detect's family, established
		return engine.Status{State: engine.NotApplicable, Detail: DetailUnsupported, Evidence: []string{"distribution: " + string(f.Platform.Distro)}}, nil
	case err != nil:
		return engine.Status{State: engine.Unknown, Detail: DetailCantRead}, err
	}
	ev := evidence(s)
	if d, ok := reportedOnly[s.Manager]; ok {
		return engine.Status{State: engine.NotApplicable, Detail: d, Evidence: ev}, nil
	}
	if s.Manager == hostdns.ManagerUnknown || s.Err != nil {
		why := s.Err
		if why == nil {
			why = hostdns.ErrUnknown
		}
		return engine.Status{State: engine.Unknown, Detail: DetailCantRead, Evidence: ev}, fmt.Errorf("dns: %w", why)
	}
	st := engine.Status{State: engine.NotConfigured, Evidence: ev}
	for _, fd := range s.Findings() {
		st.Findings = append(st.Findings, steps.DNSFinding(s, fd))
	}
	want := list(n.Config)
	if n.Config != nil && n.Config.Path() != "" {
		st.Evidence = append(st.Evidence, n.Config.Path()+": nameservers "+words(want))
	}
	inUse := s.Nameservers
	if s.Manager == hostdns.ManagerResolved {
		inUse = s.Resolved.Global
	}
	switch v, err := hostdns.Validate(hostdns.Config{Nameservers: want}, s.Manager); {
	case len(want) == 0:
		st.Detail = DetailNone
	case err != nil:
		st.Detail = DetailInvalid
	case s.Manager == hostdns.ManagerResolved && s.Resolved.Active != detect.Yes:
		st.Detail = DetailNotRunning
	case slices.Equal(inUse, v.Nameservers):
		st.State, st.Detail = engine.Configured, strings.Join(v.Nameservers, ", ")
	default:
		st.Detail = DetailNotHardn
	}
	return st, nil
}

// words is a list for the evidence: its items, or "none".
func words(l []string) string {
	if len(l) == 0 {
		return "none"
	}
	return strings.Join(l, ", ")
}

// evidence is what the line was read from, one line per source.
func evidence(s hostdns.State) []string {
	rc := hostdns.ResolvConf
	var ev []string
	switch e := s.ResolvConf; e.Kind {
	case hostdns.EntrySymlink:
		ev = append(ev, rc+" -> "+e.Target)
	case hostdns.EntryAbsent:
		ev = append(ev, rc+": absent")
	case hostdns.EntryOther:
		ev = append(ev, rc+": neither a file nor a link")
	case hostdns.EntryFile:
		if _, ok := reportedOnly[s.Manager]; ok {
			first, _, _ := strings.Cut(string(e.Data), "\n")
			ev = append(ev, fmt.Sprintf("%s: %q", rc, first))
		}
	}
	switch s.Manager {
	case hostdns.ManagerFile, hostdns.ManagerProxmox:
		ev = append(ev, rc+": nameservers "+words(s.Nameservers), rc+": search "+words(s.Search))
	case hostdns.ManagerResolved:
		active := map[detect.Tri]string{detect.Yes: "active", detect.No: "not active", detect.Unknown: "not established"}
		ev = append(ev, "systemctl is-active "+hostdns.ResolvedUnit+": "+active[s.Resolved.Active])
		if s.Resolved.Active != detect.Yes {
			break
		}
		ev = append(ev, "resolvectl dns: Global: "+words(s.Resolved.Global))
		for _, l := range s.Resolved.Links {
			if len(l.Servers) > 0 {
				ev = append(ev, fmt.Sprintf("resolvectl dns: %s: %s", l, words(l.Servers)))
			}
		}
		ev = append(ev, "resolvectl domain: Global: "+words(s.Resolved.Domains))
		for _, l := range s.Resolved.Links {
			if len(l.Domains) > 0 {
				ev = append(ev, fmt.Sprintf("resolvectl domain: %s: %s", l, words(l.Domains)))
			}
		}
	}
	return ev
}

// Plan reads the host's DNS, then plans in.Config's list with the host's
// search domains kept (dns.PlanDNS, decision 2) and, on Alpine, udhcpc
// stopped when asked (StopUDHCPC): the change that brings the host to it
// (steps.DNS), none when the host uses it already; then, when the list is
// not the one hardn.yml holds at in.Config.Path(), the save of in.Config
// there after it (steps.PlanSave, decision 1); with no file loaded, a host
// already as asked plans nothing, and a change, which could not be saved,
// is refused (CONFIG_NOT_LOADED). Every refusal is made before any
// change, with a report: a container, a manager hardn only reports, a
// fact not established, systemd-resolved stopped, a drop-in after hardn's,
// hardn.yml listing none or one dns.Validate refuses (refusal), hardn.yml
// changed since it was read (steps.Changed), and the save's.
func (n Nameservers) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	want := list(in.Config)
	s, err := hostdns.Read(ctx, sys, in.Facts)
	if err != nil {
		return nil, refusal(s, want, in.Config, err)
	}
	p, err := hostdns.PlanDNS(s, hostdns.Config{Nameservers: want, UDHCPCNoResolv: n.StopUDHCPC})
	if err != nil {
		return nil, refusal(s, want, in.Config, err)
	}
	var cs []engine.Change
	if len(p.Changes) > 0 {
		title, summary := heading(p)
		c, err := steps.DNS(ChangeSet, p, summary)
		if err != nil {
			return nil, err
		}
		c.Title = title
		cs = append(cs, c)
	}
	saved, err := savedList(ctx, sys, in.Config)
	switch {
	case err != nil:
		return nil, err
	case saved == nil && len(cs) == 0, saved != nil && slices.Equal(*saved, want):
		return cs, nil
	}
	var after []string
	if len(cs) > 0 {
		after = []string{ChangeSet}
	}
	save, err := steps.PlanSave(ctx, sys, ChangeSave, in.Config, []string{Setting}, after...)
	if err != nil {
		return nil, err
	}
	return append(cs, save...), nil
}

// heading is the change's title and summary, from the steps p holds: the
// nameservers set (the file, or resolved's drop-in or reload), udhcpc
// stopped, or both (proposed).
func heading(p hostdns.Plan) (title, summary string) {
	stop := slices.ContainsFunc(p.Changes, func(c hostdns.Change) bool { return c.Step == hostdns.StepUDHCPC })
	set := slices.ContainsFunc(p.Changes, func(c hostdns.Change) bool { return c.Step != hostdns.StepUDHCPC })
	setWords, stopWords := "Set the nameservers to "+strings.Join(p.Want.Nameservers, ", "), "stop udhcpc rewriting "+hostdns.ResolvConf
	switch {
	case set && stop:
		return TitleSetStop, setWords + ", and " + stopWords
	case stop:
		return TitleStop, "Stop udhcpc rewriting " + hostdns.ResolvConf
	}
	return TitleSet, setWords
}

// savedList is the list hardn.yml holds at cfg's path, read now; nil when
// cfg was not loaded from a file. A file gone, or no longer decoding,
// changed since it was read: refused with a report (steps.Changed).
func savedList(ctx context.Context, sys host.System, cfg *config.Config) (*[]string, error) {
	if cfg == nil || cfg.Path() == "" {
		return nil, nil
	}
	f, err := config.Read(ctx, sys, cfg.Path())
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, config.ErrInvalidFile), errors.Is(err, config.ErrRootLoginNotApplied):
		return nil, steps.Changed(cfg.Path(), err)
	case err != nil:
		return nil, fmt.Errorf("dns: hardn.yml: %w", err)
	}
	return &f.Nameservers, nil
}

// HostRefusal is the refusal, with its report (steps.Refusal), that every
// plan on the host makes whatever list it is given, or nil when there is
// none: readErr, dns.Read's error (a container, a family hardn does not
// configure, the family not established), or what dns.PlanDNS refuses of
// state s before it reads a list (a manager hardn only reports, P-37 (a);
// a fact not established). A screen asks it before asking anything, so
// that no item asks for an answer every plan would refuse (slice 2,
// decision 4; next/s2-screen). The refusals a plan makes of a list (none,
// one dns.Validate refuses) and of a resolved host that takes no list
// (stopped, a later drop-in, its path blocked) are the plan's, after the
// question. Pure.
func HostRefusal(s hostdns.State, readErr error) error {
	err := readErr
	if err == nil {
		// PlanDNS refuses the manager and the facts before it validates
		// the list, so an empty one leaves those refusals alone.
		if _, err = hostdns.PlanDNS(s, hostdns.Config{}); !errors.Is(err, hostdns.ErrManaged) && !errors.Is(err, hostdns.ErrUnknown) {
			return nil
		}
	}
	return refusal(s, nil, nil, err)
}

// refusal is err, a refusal of pkg/host/dns, with its report: why the host
// is refused and how to go on, in a few words (proposed; control-dns.md
// §6). s is the state read, want hardn.yml's list, cfg its file. An error
// that is not a refusal is err.
func refusal(s hostdns.State, want []string, cfg *config.Config, err error) error {
	rc := hostdns.ResolvConf
	file := "hardn.yml"
	if cfg != nil && cfg.Path() != "" {
		file = cfg.Path()
	}
	r := &steps.Refusal{Err: err}
	switch {
	case errors.Is(err, hostdns.ErrContainer):
		r.Code, r.Why, r.Fix = codeContainer, "This is a container: its manager provides "+rc, "set the nameservers in the container's settings on its host"
	case errors.Is(err, hostdns.ErrUnsupported):
		r.Code, r.Why, r.Fix = codeUnsupported, "This host is not Debian, Ubuntu, Alpine or Proxmox VE", "set the nameservers by hand"
	case errors.Is(err, hostdns.ErrManaged):
		r.Code = codeManaged
		r.Why, r.Fix = managed(s)
	case errors.Is(err, hostdns.ErrNotRunning):
		r.Code, r.Why, r.Fix = codeNotRunning, "systemd-resolved is not running", "start it with systemctl start systemd-resolved, then try again"
	case errors.Is(err, hostdns.ErrOverridden):
		var later []string
		for _, l := range s.Resolved.Later {
			if l.DNS {
				later = append(later, l.Path)
			}
		}
		verb, it := " sets", "it"
		if len(later) > 1 {
			verb, it = " set", "them"
		}
		r.Code, r.Why, r.Fix = codeOverridden, strings.Join(later, ", ")+verb+" DNS= after "+hostdns.DropInName, "remove DNS= from "+it+", then try again"
	case errors.Is(err, hostdns.ErrDropInBlocked):
		r.Code, r.Why, r.Fix = codeBlocked, hostdns.DropIn+" is not a file", "remove it, then try again"
	case errors.Is(err, hostdns.ErrNoNameservers):
		r.Code, r.Why, r.Fix = codeNone, "No nameservers configured", "add nameservers first"
	case errors.Is(err, hostdns.ErrNotApplicable):
		r.Code, r.Why, r.Fix = codeOption, "This host is not Alpine", "leave udhcpc as it is"
	case errors.Is(err, hostdns.ErrInvalid):
		r.Code, r.Why, r.Fix = codeInvalid, invalid(want, s.Manager), "correct the nameservers in "+file
	case errors.Is(err, hostdns.ErrUnknown):
		r.Code, r.Why, r.Fix = codeUnknown, "The host's DNS settings could not all be read", "see the log for what failed, then try again"
	default:
		return err
	}
	return r
}

// managed is the report of a manager hardn only reports: what manages DNS,
// and how to change it by hand (P-37 (a)).
func managed(s hostdns.State) (why, fix string) {
	switch {
	case s.Manager == hostdns.ManagerNetworkManager:
		return "NetworkManager manages DNS here", "set the nameservers with nmcli"
	case s.Manager == hostdns.ManagerResolvconf:
		return "resolvconf manages DNS here", "set the nameservers through resolvconf"
	case s.ResolvConf.Kind == hostdns.EntrySymlink:
		return hostdns.ResolvConf + " links to " + s.ResolvConf.Target + ", which another program writes", "set the nameservers with that program"
	}
	// Not "or stop systemd-resolved": a copy of resolved's stub file lists
	// 127.0.0.53 alone, which would then answer nothing.
	return "systemd-resolved runs, and " + hostdns.ResolvConf + " is not its link",
		"link " + hostdns.ResolvConf + " to /run/systemd/resolve/stub-resolv.conf"
}

// invalid says which entry of want dns.Validate refuses for manager m, and
// why, in a few words.
func invalid(want []string, m hostdns.Manager) string {
	var seen []string
	for _, e := range want {
		a, err := netip.ParseAddr(e)
		switch {
		case err != nil:
			return "Nameserver " + e + " is not an IP address"
		case slices.Contains(seen, a.String()):
			return "Nameserver " + e + " is listed twice"
		}
		if _, err := hostdns.Validate(hostdns.Config{Nameservers: []string{e}}, m); err != nil {
			return "Nameserver " + e + " is not a usable address"
		}
		seen = append(seen, a.String())
	}
	if file := m == hostdns.ManagerFile || m == hostdns.ManagerProxmox; file && len(want) > hostdns.MaxNameservers {
		return fmt.Sprintf("%s takes %d nameservers at most", hostdns.ResolvConf, hostdns.MaxNameservers)
	}
	return "The nameservers are not valid here"
}
