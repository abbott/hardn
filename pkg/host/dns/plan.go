package dns

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Step is one change a plan makes, in the order Apply runs them.
type Step uint8

const (
	StepUDHCPC     Step = iota + 1 // RESOLV_CONF="no" in UDHCPCConf (asked for only)
	StepResolvConf                 // write /etc/resolv.conf (a file manager)
	StepDropIn                     // write DropIn (systemd-resolved)
	StepReload                     // systemctl try-reload-or-restart systemd-resolved.service
)

func (s Step) String() string {
	switch s {
	case StepUDHCPC:
		return "udhcpc-no-resolv"
	case StepResolvConf:
		return "write-resolv-conf"
	case StepDropIn:
		return "write-drop-in"
	case StepReload:
		return "reload-resolved"
	}
	return "unknown"
}

// reloadArgv reloads systemd-resolved where its unit supports it
// (systemd 256 and later), restarts it otherwise, and does nothing when it
// is not running, which the check after it then catches.
var reloadArgv = []string{"systemctl", "try-reload-or-restart", ResolvedUnit}

// Change is one step of a plan. A file step writes After to Path, where
// Before was (nil: no file); Argv is StepReload's command. None of these
// steps can lock a user out: a failure is undone, with no
// confirm-or-revert timer (brief 2.4).
type Change struct {
	Step          Step
	Path          string
	Before, After []byte
	Argv          []string
	Why           string
}

// Plan is what PlanDNS decided. From is the state it was planned from and
// Want the validated configuration: Apply refuses a host that no longer
// matches From, and the undo writes From's files back. A Plan kept for
// p2-revert must keep From, Want and every Change; Apply and Rollback
// refuse one that does not follow from them.
type Plan struct {
	From     State
	Want     Config
	Changes  []Change
	Findings []Finding
}

// PlanDNS decides, with no I/O, what makes the host use c. On
// systemd-resolved: write DropIn, then reload; on a file: write
// /etc/resolv.conf with its nameserver lines replaced (and its search
// lines, with c.ReplaceSearch), keeping every other line; on Alpine, first
// set RESOLV_CONF="no" for udhcpc when c.UDHCPCNoResolv asks. A step is
// planned only when needed, so a configured host plans nothing. It
// refuses a manager or fact not established (ErrUnknown), a manager hardn
// does not configure (ErrManaged), a stopped systemd-resolved
// (ErrNotRunning), a later drop-in that would override hardn's
// (ErrOverridden), anything at DropIn that is not a regular file
// (ErrDropInBlocked), and what Validate refuses; with c.UDHCPCNoResolv, a
// host where udhcpc is not the DHCP client (ErrNotApplicable) or where
// that, or its rewrite, is not established (ErrUnknown): the step would
// stop a client that does not run, or one not known to. Its findings are
// s.Findings(), less dns-udhcpc when the plan sets RESOLV_CONF itself.
func PlanDNS(s State, c Config) (Plan, error) {
	switch s.Manager {
	case ManagerUnknown:
		return Plan{}, fmt.Errorf("dns: plan: %s: %w: %w", ResolvConf, ErrUnknown, errOr(s.Err, detect.ErrNoSignal))
	case ManagerResolvconf, ManagerNetworkManager, ManagerForeign, ManagerCloudInit:
		return Plan{}, fmt.Errorf("dns: plan: %s (%s): %s: %w", ResolvConf, describe(s.ResolvConf), s.Manager, ErrManaged)
	}
	if s.Err != nil {
		return Plan{}, fmt.Errorf("dns: plan: %w: %w", ErrUnknown, s.Err)
	}
	want, err := Validate(c, s.Manager)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{From: s, Want: want}
	if want.UDHCPCNoResolv && s.Host.Distro != detect.DistroAlpine {
		return Plan{}, fmt.Errorf("dns: plan: RESOLV_CONF for udhcpc is Alpine's (%s): %w", s.Host.Distro, ErrNotApplicable)
	}
	if s.Manager == ManagerResolved {
		return planResolved(p)
	}
	if want.UDHCPCNoResolv {
		switch u := s.UDHCPC; {
		case u.InUse == detect.No:
			return Plan{}, fmt.Errorf("dns: plan: RESOLV_CONF for udhcpc, which is not the DHCP client here: %w", ErrNotApplicable)
		case u.InUse == detect.Unknown:
			return Plan{}, fmt.Errorf("dns: plan: udhcpc: %w: %w", ErrUnknown, errOr(u.InUseErr, detect.ErrNoSignal))
		case u.Rewrites == detect.Unknown:
			return Plan{}, fmt.Errorf("dns: plan: udhcpc: %w: %w", ErrUnknown, errOr(u.Err, detect.ErrNoSignal))
		case u.Rewrites == detect.Yes:
			p.Changes = append(p.Changes, write(StepUDHCPC, UDHCPCConf, u.Conf, renderUDHCPC(u.Conf.Data),
				"udhcpc rewrites "+ResolvConf+" with the DHCP server's nameservers on each lease"))
		}
	}
	if after := renderResolvConf(s.ResolvConf.Data, want); s.ResolvConf.Kind != EntryFile || !bytes.Equal(s.ResolvConf.Data, after) {
		why := "set the nameservers"
		if want.ReplaceSearch {
			why += " and the search domains"
		}
		p.Changes = append(p.Changes, write(StepResolvConf, ResolvConf, s.ResolvConf, after, why))
	}
	for _, f := range s.Findings() {
		if f.Kind != FindingUDHCPC || f.Path != UDHCPCConf || !want.UDHCPCNoResolv {
			p.Findings = append(p.Findings, f)
		}
	}
	return p, nil
}

// write is a file step from before to after.
func write(st Step, path string, before Entry, after []byte, why string) Change {
	c := Change{Step: st, Path: path, After: after, Why: why}
	if before.Kind == EntryFile {
		c.Before = before.Data
	}
	return c
}

func planResolved(p Plan) (Plan, error) {
	r := p.From.Resolved
	var over []string
	for _, l := range r.Later {
		if l.DNS || l.Domains && p.Want.ReplaceSearch {
			over = append(over, l.Path)
		}
	}
	switch {
	case r.Active != detect.Yes:
		return Plan{}, fmt.Errorf("dns: plan: %s links to its file: %w", ResolvConf, ErrNotRunning)
	case len(over) > 0:
		return Plan{}, fmt.Errorf("dns: plan: %s: %w", strings.Join(over, ", "), ErrOverridden)
	case r.DropIn.Kind != EntryAbsent && r.DropIn.Kind != EntryFile:
		return Plan{}, fmt.Errorf("dns: plan: %s is %s, which hardn does not replace: %w", DropIn, describe(r.DropIn), ErrDropInBlocked)
	}
	after := renderDropIn(r.DropIn, p.Want)
	wrote := r.DropIn.Kind != EntryFile || !bytes.Equal(r.DropIn.Data, after)
	if wrote {
		p.Changes = append(p.Changes, write(StepDropIn, DropIn, r.DropIn, after, "set systemd-resolved's global nameservers"))
	}
	if wrote || !slices.Equal(r.Global, p.Want.Nameservers) || p.Want.ReplaceSearch && !sameDomains(r.Domains, p.Want.Search) {
		p.Changes = append(p.Changes, Change{Step: StepReload, Argv: slices.Clone(reloadArgv), Why: "systemd-resolved reads its configuration when reloaded"})
	}
	p.Findings = p.From.Findings()
	return p, nil
}

// sameDomains: the search domain lists match, ignoring case, as DNS names
// compare.
func sameDomains(a, b []string) bool {
	return slices.EqualFunc(a, b, strings.EqualFold)
}

// Findings are what may bypass or later undo a DNS change on the host s
// describes, as a check reports them; pure. On systemd-resolved: each link
// or delegate with its own servers. For a file: Proxmox VE's node
// settings; where udhcpc is the DHCP client (UDHCPC.InUse Yes), udhcpc
// rewriting the file and its hooks; where that is not established, one
// finding saying so, its path the file that left it so, unless udhcpc
// would leave the file alone anyway (RESOLV_CONF stops it and no hook);
// and DHCP clients. A host whose manager hardn does not configure has
// none: its refusal says why.
func (s State) Findings() []Finding {
	var fs []Finding
	if s.Manager == ManagerResolved {
		uplink := ""
		if t, _ := host.JoinLinkTarget(path.Dir(ResolvConf), s.ResolvConf.Target); t == uplinkFile {
			uplink = "; " + ResolvConf + " links to " + uplinkFile + ", which programs read directly, using its first three nameservers"
		}
		for _, l := range s.Resolved.Links {
			if len(l.Servers) > 0 {
				fs = append(fs, Finding{Kind: FindingPerLink, Path: DropIn, Link: &l, Detail: fmt.Sprintf(
					"%s has its own nameservers (%s), from DHCP, netplan, networkd or a delegate; systemd-resolved queries them in parallel to the global ones%s",
					l, strings.Join(l.Servers, " "), uplink)})
			}
		}
		return fs
	}
	if s.Manager != ManagerFile && s.Manager != ManagerProxmox {
		return nil
	}
	if s.Manager == ManagerProxmox {
		fs = append(fs, Finding{Kind: FindingProxmox, Path: ResolvConf, Detail: "Proxmox VE's node DNS settings (System > DNS, /nodes/<node>/dns) read this file " +
			"and rewrite it on save, keeping the first search domain and three nameservers"})
	}
	switch u := s.UDHCPC; {
	case s.Host.Distro != detect.DistroAlpine, u.InUse == detect.No:
	case u.InUse == detect.Unknown:
		if u.Rewrites == detect.No && len(u.Hooks) == 0 {
			break
		}
		then := "it may rewrite " + ResolvConf
		if u.Rewrites == detect.Yes {
			then = "it rewrites " + ResolvConf + " on each DHCP lease"
		}
		src := Interfaces
		if e := (*inUseError)(nil); errors.As(u.InUseErr, &e) {
			src = e.path
		}
		fs = append(fs, Finding{Kind: FindingUDHCPCUnknown, Path: src, Detail: fmt.Sprintf(
			"whether udhcpc is the DHCP client ifupdown-ng runs is not established (%v); if it is, %s", errOr(u.InUseErr, detect.ErrNoSignal), then)})
	default:
		switch u.Rewrites {
		case detect.Yes:
			fs = append(fs, Finding{Kind: FindingUDHCPC, Path: UDHCPCConf, Detail: "udhcpc rewrites " + ResolvConf + " on each DHCP lease (except for interfaces in NO_DNS=" +
				fmt.Sprintf("%q", u.NoDNS) + `); RESOLV_CONF="no" there stops it, as an explicit plan step`})
		case detect.Unknown:
			fs = append(fs, Finding{Kind: FindingUDHCPC, Path: UDHCPCConf, Detail: fmt.Sprintf("whether udhcpc rewrites %s is not established: %v", ResolvConf, errOr(u.Err, detect.ErrNoSignal))})
		}
		for _, h := range u.Hooks {
			fs = append(fs, Finding{Kind: FindingUDHCPC, Path: h, Detail: "udhcpc runs this hook around each DHCP event; it may rewrite " + ResolvConf + `, whatever RESOLV_CONF says`})
		}
	}
	for _, c := range s.DHCPClients {
		fs = append(fs, Finding{Kind: FindingDHCPClient, Path: c, Detail: "a DHCP client is installed; for an interface it configures, it may rewrite " + ResolvConf + " on each lease"})
	}
	return fs
}

// UDHCPCStoppable reports whether, by s, udhcpc rewrites /etc/resolv.conf
// on each lease and a plan can stop it (Config.UDHCPCNoResolv): Alpine, a
// file, udhcpc the DHCP client (UDHCPC.InUse Yes) and its rewrite
// established (Rewrites Yes). Only then is a user asked whether to stop it
// (slice 2 decision 6, next/s2-udhcpc).
func (s State) UDHCPCStoppable() bool {
	return s.Host.Distro == detect.DistroAlpine && s.Manager == ManagerFile &&
		s.UDHCPC.InUse == detect.Yes && s.UDHCPC.Rewrites == detect.Yes
}

func describe(e Entry) string {
	switch e.Kind {
	case EntrySymlink:
		return "a symlink to " + e.Target
	case EntryFile:
		first, _, _ := strings.Cut(string(e.Data), "\n")
		return fmt.Sprintf("a file starting %q", first)
	case EntryAbsent:
		return "absent"
	case EntryOther:
		return "neither a file nor a symlink"
	}
	return "not read"
}
