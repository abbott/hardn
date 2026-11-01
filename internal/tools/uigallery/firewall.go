package main

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Firewall mocks (refactor plan slice 4, unit (a), next/s4-gallery): a
// proposal of the Firewall screen, main menu 4, for the maintainer's review,
// nothing of it wired into hardn. It starts from origin/main's screen
// (pkg/menu/firewall_menu.go) on the design system's sub-menu layout, with no
// new component. It is drawn from what real code reads of a simulated host
// (firewallhost.go): the Firewall row as firewall.Incoming.Check makes it,
// ufw's state (ufw.Read, ufw.Evaluate), pve-firewall's (pve.ReadFirewall),
// the SSH listeners (sshd.ReadEffective, reload.ReadBound), never hardn.yml's
// sshPort. Enable is steps.PlanUFWEnable on the real engine, a lockout-risk
// change, through the canonical flow (screenkit.Session.Change).
//
// What real code lacks is the gallery's, each a stand-in for unit (b)'s work,
// labelled so here and in the page (docs/engine/gallery-firewall.md §7):
//
//   - fwControl: firewall.Incoming's Plan is still engine.ErrNotMigrated.
//   - fwSockets: the SSH listener reading of pkg/engine/controls/firewall's
//     sockets.go, which is unexported; a third copy, written again.
//   - fwEnable's refusal with no ss: PlanUFWEnable reads no bound socket.
//   - fwCommand's plans: Install (apt-get, apk), Disable (ufw disable), the
//     runlevel on OpenRC (rc-update) and the profiles' Apply, written from
//     each tool's own commands, for want of a real plan.
//   - The profiles' Add and Remove: steps.PlanSave, on a control of the
//     gallery's.
//
// "firewall" lists the mocks with no input; "play firewall" is the menu of the
// hosts and "play firewall/<host>" the screen on one.

// fwPrefix names the mocks: "firewall <mock>" in the listing, "firewall" and
// "firewall/<host>" in play.
const fwPrefix = "firewall"

// fwMainHost is the host the main menu's 4 leads to.
const fwMainHost = fwPrefix + "/debian13"

// fwLinks lead from a menu mock's item to the Firewall mocks on a host, beside
// links: the main menu's 4.
var fwLinks = map[string]string{mainMenuMock + "/4": fwMainHost}

// The screen's words: origin/main's titles, labels and "No firewall rules
// configured" (firewall_menu.go), Backup's decided heading and prompt, the
// rest proposed (gallery-firewall.md §6).
const (
	fwTitle         = "UFW Firewall Configuration"
	fwPVETitle      = "Firewall Configuration"
	fwProfilesTitle = "Manage UFW Application Profiles"
	fwHeading       = "Select an option"
	fwPrompt        = "Enter selection"

	fwSectionUFW      = "ufw"
	fwSectionPVE      = "pve-firewall"
	fwSectionSSH      = "SSH"
	fwSectionRules    = "Rules"
	fwSectionProfiles = "Application Profiles"

	fwKeyInstalled = "Installed"
	fwKeyStatus    = "Status"
	fwKeyPolicy    = "Incoming policy"
	fwKeyBoot      = "At boot"
	fwKeyEnabled   = "Enabled"
	fwKeySSH       = "SSH (22)"
	fwKeyWeb       = "Web UI (8006)"

	fwNoRules    = "No firewall rules configured"
	fwNoProfiles = "No application profiles configured"
	fwNoChange   = "hardn reads pve-firewall and doesn't change it: a wrong rule can cut off the web UI and the cluster."

	fwInstallLabel  = "Install UFW"
	fwInstallAbout  = "Install the ufw package"
	fwEnableLabel   = "Enable firewall"
	fwEnableAbout   = "Let SSH through, deny the rest"
	fwDisableLabel  = "Disable firewall"
	fwDisableAbout  = "Stop ufw (not recommended)"
	fwProfilesLabel = "Application Profiles"
	fwProfilesAbout = "Add, remove, apply"

	fwAddLabel    = "Add profile"
	fwAddAbout    = "Save a profile in hardn.yml"
	fwRemoveLabel = "Remove profile"
	fwRemoveAbout = "Take one out of hardn.yml"
	fwApplyLabel  = "Apply profiles"
	fwApplyAbout  = "Let their ports through ufw"

	fwDisableWarning = "This stops the firewall and leaves every port open."
	fwRemoveNotice   = "This takes the profile out of hardn.yml. A rule already in ufw stays."
	fwInstallInstead = "To install ufw, run: %s"

	// The refusals, said before asking anything, from the real errors.
	fwNoSSWords       = "hardn can't tell which ports SSH uses: ss isn't installed. Install iproute2, then try again."
	fwNoListeners     = "hardn can't tell which ports SSH uses."
	fwStateWords      = "hardn can't read ufw's state. Check ufw status verbose, then try again."
	fwNotRunningWords = "ufw is set to start at boot but isn't running. Check that its rules let SSH through, then run ufw enable."
	fwNotBootWords    = "ufw is running but isn't set to start at boot. Run ufw enable."
	fwBlocksWords     = "A saved ufw rule would block SSH on port %d, for some addresses or all. Remove it with ufw delete, then try again."
	fwProxmoxWords    = "hardn can't tell if this host runs Proxmox VE, where it leaves the firewall alone."
)

// The profiles' form: origin/main's fields (firewall_menu.go:addAppProfile),
// and its checks, shortened; a name and ports narrowed to what ufw's profile
// file and hardn's commands can hold.
var (
	fwNameForm = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	fwPortForm = regexp.MustCompile(`^[0-9]{1,5}(:[0-9]{1,5})?/(tcp|udp)$`)
)

// fwProfileFile is the file the profiles are written to, as origin/main writes
// it (ops/configure_secure_firewall_profiles/*.golden) and
// docs/configuration.md documents.
const fwProfileFile = "/etc/ufw/applications.d/hardn"

// fwProfileSetting is hardn.yml's key.
const fwProfileSetting = "ufwAppProfiles"

// The change ids: fwID is firewall.Incoming's.
const fwID = "firewall.incoming"

// fwHost is one simulated host the Firewall mocks run on.
type fwHost struct {
	// name is the host's, after fwPrefix: "debian13".
	name string
	// label and about are its item in the menu of the hosts.
	label, about string
	// note says what the host is.
	note string
	// host returns the simulated host, fresh.
	host func() *fakeHost
}

// fwHosts are the hosts, in the menu's order.
func fwHosts() []fwHost {
	const (
		deb = hostFQDN + " runs Debian 13. "
		alp = hostFQDN + " runs Alpine 3.24. "
		pxm = hostFQDN + " is a Proxmox VE 9 node, no ufw. "
	)
	return []fwHost{
		{name: "debian13", label: "Debian 13", about: "No ufw", host: func() *fakeHost { return fwNoUFW(debian13Platform) },
			note: deb + "ufw is not installed (check-firewall.md §3: Debian installs it only on request, [unverified])."},
		{name: "ubuntu2604", label: "Ubuntu 26.04", about: "ufw inactive, ssh.socket", host: fwUbuntu,
			note: hostFQDN + " runs Ubuntu 26.04 as shipped: ufw installed and inactive, ENABLED=no, no rule, sshd socket-activated on [::]:22 (vmtest.md §4.9)."},
		{name: "active22", label: "Active on 22", about: "Deny policy, SSH let through", host: fwDebian,
			note: deb + "ufw active, default deny incoming, allowing 22, where sshd listens."},
		{name: "hardened", label: "Hardened", about: "Deny policy, sshd on 2230", host: func() *fakeHost { return ufwOn(fwHardened()) },
			note: deb + "After hardn: sshd reloaded onto 2230, ufw active, default deny incoming, allowing 2230 alone."},
		{name: "allow-policy", label: "Allow policy", about: "ufw on, refusing nothing", host: fwWith(ufwSim{incoming: "allow", active: true, enabled: true}),
			note: deb + "ufw active, default allow incoming, no rule: it refuses nothing."},
		{name: "lockout", label: "Lockout", about: "ufw refuses SSH", host: func() *fakeHost { return ufwOn(lockout().withHardnYML(fwYML)) },
			note: deb + "sshd on 2230, ufw active allowing 443 alone: a new SSH session is refused. Enable lets it through."},
		{name: "lockout-at-start", label: "Lockout at start", about: "Stopped, saved rules refuse SSH", host: func() *fakeHost { return ufwOn(lockoutAtStart().withHardnYML(fwYML)) },
			note: deb + "sshd on 2230, ufw stopped with ENABLED=yes, its saved rules allowing 443 alone: the next boot refuses SSH. hardn refuses to plan over the disagreement."},
		{name: "allowlist", label: "Allowlist", about: "SSH from some sources", host: fwWith(ufwSim{incoming: "deny", active: true, enabled: true, rules: []simRule{{action: "allow", port: "22", src: "192.0.2.0/24"}, {action: "allow", port: "22", src: "2001:db8::/32"}}}),
			note: deb + "ufw active, default deny incoming, SSH let through from 192.0.2.0/24 and 2001:db8::/32 alone."},
		{name: "blocklist", label: "Blocklist", about: "A saved deny before the allow", host: fwWith(ufwSim{incoming: "deny", rules: []simRule{{action: "deny", port: "22", src: "198.51.100.0/24"}, {action: "allow", port: "22"}}}),
			note: deb + "ufw inactive, its saved rules denying 198.51.100.0/24 on 22 before allowing 22: whether the session's own address is among them is not established, so Enable refuses (ufw.ErrBlocks; P-33 (3))."},
		{name: "edit-2230", label: "Edit not reloaded", about: "Configured 2230, sshd on 22", host: fwEdit,
			note: deb + "Port 2230 in hardn's drop-in, sshd not reloaded, so still on 22, ufw inactive. PlanUFWEnable reads sshd -T alone, so it lets 2230 through, and 22 only as the session's port (§7's finding)."},
		{name: "alpine324", label: "Alpine 3.24", about: "No ufw", host: func() *fakeHost { return fwNoUFW(alpine324Platform) },
			note: alp + "ufw is not installed (community repository, ufw.md §3)."},
		{name: "alpine-runlevel", label: "Alpine, no runlevel", about: "Active, gone after a reboot", host: fwAlpineActive,
			note: alp + "ufw active, deny incoming, allowing 22, ENABLED=yes, in no runlevel, so it does not come back after a reboot (ufw.md §6, [I]). v0.3.2 ran rc-update add ufw default. The Firewall row cannot see this (check-firewall.md §6): the At boot row can."},
		{name: "pve9", label: "Proxmox VE 9", about: "pve-firewall disabled", host: pveNode,
			note: pxm + "pve-firewall running and disabled, as installed."},
		{name: "pve9-drop", label: "Proxmox VE 9, drop", about: "pve-firewall on, DROP", host: fwPVEDrop,
			note: pxm + "The datacenter firewall on, policy_in DROP, the management IP set written."},
		{name: "profiles", label: "Two profiles", about: "hardn.yml has two", host: func() *fakeHost {
			h := statusOn(debian13Platform).withHardnYML(fwProfileYML)
			return ufwOn(h)
		},
			note: deb + "ufw active on 22, hardn.yml with two application profiles, not applied."},
	}
}

// fwHardened is Debian 13 after hardn, with hardn.yml (hardened's: unattended
// upgrades and DNS too, none of which this screen reads).
func fwHardened() *fakeHost {
	h := statusOn(debian13Platform)
	h.harden()
	return h.withHardnYML(fwYML)
}

// findFWHost returns the host named name, after fwPrefix.
func findFWHost(name string) (fwHost, bool) {
	all := fwHosts()
	i := slices.IndexFunc(all, func(h fwHost) bool { return h.name == name })
	if i < 0 {
		return fwHost{}, false
	}
	return all[i], true
}

// fwSession is the session that applies a change on h: alice's connection to
// the port sshd is bound on (applySession's, whose port is 22; on a host moved
// to 2230 a session on 22 would keep a port nothing listens on).
func fwSession(h *fakeHost) revert.Session {
	s := applySession
	if len(h.bound) > 0 {
		if a, err := netip.ParseAddrPort(h.bound[0]); err == nil {
			s.Conn = fmt.Sprintf("192.0.2.10 50022 192.0.2.20 %d", a.Port())
		}
	}
	return s
}

// fwSockets is the SSH listeners as firewall's sockets.go reads them, a
// stand-in for that reading exported (unit (b)): the configured set, sshd -T's
// with ssh.socket's, and the sockets ss shows bound. Established when the
// configured set is known and every listener of it is bound.
type fwSockets struct {
	conf    sshd.Listeners
	read    bool
	bound   []reload.Bound
	unbound []sshd.Listener
	noSS    bool
	why     string // why they are not established; "" when they are
}

// readFWSockets reads them, as readSockets does.
func readFWSockets(ctx context.Context, sys host.System, f detect.Facts) fwSockets {
	var s fwSockets
	var bin string
	if f.Init.Kind == detect.OpenRC {
		b, err := reload.OpenRCFlavour(ctx, sys)
		if err != nil {
			s.why = "can't tell ssh ports"
			return s
		}
		bin = b
	}
	eff, _, err := sshd.ReadEffective(ctx, sys, sshd.EffectiveRequest{SSH: f.SSH, Binary: bin})
	if err != nil {
		s.why = "can't tell ssh ports"
		return s
	}
	s.conf, s.read = eff.Listeners, true
	bound, _, err := reload.ReadBound(ctx, sys)
	switch {
	case errors.Is(err, host.ErrNotFound):
		s.noSS, s.why = true, "ss not installed"
	case err != nil:
		s.why = "can't tell ssh ports"
	default:
		s.bound, s.unbound = bound, reload.Unbound(s.conf.All, bound)
		switch {
		case !s.conf.Known || len(s.conf.All) == 0:
			s.why = "can't tell ssh ports"
		case len(s.unbound) > 0:
			s.why = "ssh config not in effect"
		}
	}
	return s
}

// ports are the ports sshd's sockets may be on: the configured ones and 22.
func (s fwSockets) ports() []uint16 {
	ps := s.conf.Ports()
	if !slices.Contains(ps, 22) {
		ps = append(ps, 22)
	}
	return ps
}

// listeners are what the firewall must let through, as sockets.listeners has
// them: the configured set, and, while not established, or for a socket on 22
// no configured listener is on, the bound sockets on ports.
func (s fwSockets) listeners() (sshd.Listeners, bool) {
	if !s.read {
		return sshd.Listeners{}, false
	}
	ls := sshd.Listeners{SSHD: slices.Clone(s.conf.SSHD), Socket: slices.Clone(s.conf.Socket), All: slices.Clone(s.conf.All)}
	for _, b := range s.bound {
		stray := s.why == "" && b.Port == 22 && !slices.Contains(s.conf.Ports(), 22)
		if !slices.Contains(s.ports(), b.Port) || s.why == "" && !stray {
			continue
		}
		l := sshd.Listener{Addr: b.Addr, Port: b.Port}
		if b.Dual {
			ls.Socket = append(ls.Socket, l)
		} else {
			ls.SSHD = append(ls.SSHD, l)
		}
		ls.All = append(ls.All, l)
	}
	return ls, true
}

// fwReading is one reading of a host, as the screen draws it. It changes
// nothing.
type fwReading struct {
	status component.StatusList // the Firewall row, as the main menu draws it
	facts  detect.Facts
	// proxmox is why it is not established that the host does not run
	// Proxmox VE; onPVE, that it does.
	proxmox error
	onPVE   bool
	pf      pve.Firewall
	st      ufw.State
	stErr   error
	sk      fwSockets
	cfg     *config.Config
	// noRunlevel: on OpenRC, ufw is in no runlevel, so it does not start at
	// the next boot.
	noRunlevel bool
	// plan is what Enable plans, planErr its refusal; read only where item
	// 1 is Enable.
	plan    []engine.Change
	planErr error
	// noInstall draws decision 1's alternative: no Install item.
	noInstall bool
}

// readFW reads what the screen draws of sys, as the engine detects it.
func (s *session) readFW(ctx context.Context, sys host.System, session revert.Session) (fwReading, error) {
	var r fwReading
	var err error
	if r.facts, err = detect.Detect(ctx, sys); err != nil {
		return r, err
	}
	e := engine.New(sys, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return r.facts, nil },
		Hold: s.hold})
	reports, err := e.Check(ctx, firewall.Controls())
	if err != nil {
		return r, err
	}
	view := output.StatusViewOf(reports, risk.Level{}).Status
	r.status.Rows = slices.DeleteFunc(view.Rows, func(row component.StatusRow) bool { return row.Label != firewall.Incoming.Title(firewall.Incoming{}) })
	if r.onPVE, r.proxmox = pve.Applies(r.facts.Proxmox); r.proxmox != nil {
		return r, nil
	}
	if r.onPVE {
		r.pf, err = pve.ReadFirewall(ctx, sys, r.facts.Proxmox)
		return r, err
	}
	r.sk = readFWSockets(ctx, sys, r.facts)
	if r.cfg, err = statusConfig(ctx, sys); err != nil {
		return r, err
	}
	r.st, r.stErr = ufw.Read(ctx, sys, r.facts.Proxmox)
	if r.facts.Init.Kind == detect.OpenRC && r.st.Installed {
		_, in, _ := detect.ServiceRunlevels(ctx, sys, "ufw")
		r.noRunlevel = in == detect.No
	}
	if r.first() == fwDoEnable {
		r.plan, r.planErr = fwEnable(ctx, sys, r.facts, session)
	}
	return r, nil
}

// fwAction is what a change does.
type fwAction uint8

const (
	fwDoNone fwAction = iota
	fwDoInstall
	fwDoEnable
	fwDoDisable
	fwDoSave  // the profiles, saved in hardn.yml
	fwDoApply // the profiles, written for ufw and let through
)

// first is what item 1 does: Install with no ufw; Disable once ufw is active
// and set (it refuses what no rule accepts, lets every SSH listener through,
// and starts at the next boot); Enable otherwise. None on Proxmox VE.
func (r fwReading) first() fwAction {
	switch {
	case r.onPVE:
		return fwDoNone
	case r.proxmox == nil && r.stErr == nil && !r.st.Installed:
		if r.noInstall {
			return fwDoNone
		}
		return fwDoInstall
	case r.set():
		return fwDoDisable
	}
	return fwDoEnable
}

// set: ufw is active and set.
func (r fwReading) set() bool {
	s := r.st.Status
	if r.proxmox != nil || r.stErr != nil || !s.Known || len(s.Bad) > 0 || !s.Active || !r.st.Enabled || r.noRunlevel ||
		s.Incoming != ufw.PolicyDeny && s.Incoming != ufw.PolicyReject {
		return false
	}
	return !slices.ContainsFunc(r.coverage(), func(c ufw.Coverage) bool { return c.Verdict == ufw.Blocked || c.Verdict == ufw.Denied })
}

// coverage is ufw's verdict on each SSH listener: while active under what ufw
// shows, else under what its saved rules do once it starts.
func (r fwReading) coverage() []ufw.Coverage {
	ls, ok := r.sk.listeners()
	if !ok {
		return nil
	}
	needs := ufw.Needs(ls, nil, nil)
	if r.st.Status.Active {
		return ufw.Evaluate(r.st.Status.Rules, ufw.Firewall{Active: true, IPv6: r.st.IPv6, NoKernelIPv6: r.st.NoKernelIPv6, Incoming: r.st.Status.Incoming}, needs)
	}
	return ufw.Evaluate(r.st.Rules, ufw.Firewall{Active: true, IPv6: r.st.IPv6, NoKernelIPv6: r.st.NoKernelIPv6, Incoming: r.st.Incoming}, needs)
}

// fwVerdict is a verdict's row while ufw is active.
func fwVerdict(v ufw.Verdict) (string, component.ValueState) {
	switch v {
	case ufw.Allowed:
		return "Allowed", component.ValueOK
	case ufw.Limited:
		return "Rate limited", component.ValueOK
	case ufw.Restricted:
		return "Some sources", component.ValueWarn
	case ufw.Excepted:
		return "Blocked for some", component.ValueWarn
	case ufw.Blocked, ufw.Denied:
		return "Refused", component.ValueBad
	case ufw.Unreachable:
		return "No IPv6", component.ValuePlain
	}
	return "", component.ValueUnknown
}

// sshRows are the section SSH's rows: each listener, as sshd accepts on it or
// is configured to, and ufw's verdict for it. Not active, ufw filters nothing:
// "Not filtered"; ENABLED=yes, "Refused at start" for what its saved rules
// would refuse. A listener configured and not bound, or bound and not
// configured, says so beside it.
func (r fwReading) sshRows() []component.KeyValue {
	if !r.sk.read {
		return []component.KeyValue{{Key: "Listeners", State: component.ValueUnknown}}
	}
	active := r.st.Status.Known && r.st.Status.Active
	var unbound, configured []string
	for _, l := range r.sk.unbound {
		unbound = append(unbound, l.String())
	}
	for _, l := range r.sk.conf.All {
		configured = append(configured, l.String())
	}
	var rows []component.KeyValue
	for _, c := range r.coverage() {
		key := strings.TrimPrefix(c.Need.Why, "listener ")
		row := component.KeyValue{Key: key}
		switch {
		case active:
			row.Value, row.State = fwVerdict(c.Verdict)
		case r.st.Installed && r.st.Enabled && r.st.Known() && !c.Verdict.OK():
			row.Value, row.State = "Refused at start", component.ValueBad
		default:
			row.Value = "Not filtered"
		}
		base := strings.TrimSuffix(key, " (dual-stack)")
		switch {
		case slices.Contains(unbound, base):
			row.Note = "Not bound"
		case !slices.Contains(configured, base):
			row.Note = "Not configured"
		}
		if row.Note != "" && row.State == component.ValuePlain {
			row.State = component.ValueWarn
		}
		rows = append(rows, row)
	}
	if r.sk.noSS {
		rows = append(rows, component.KeyValue{Key: "Bound sockets", Value: "ss not installed", State: component.ValueWarn})
	}
	return rows
}

// ufwRows are the section ufw's rows.
func (r fwReading) ufwRows() []component.KeyValue {
	if r.stErr != nil {
		return []component.KeyValue{{Key: fwKeyInstalled, State: component.ValueUnknown}}
	}
	if !r.st.Installed {
		rows := []component.KeyValue{{Key: fwKeyInstalled, Value: "No", State: component.ValueWarn}}
		if r.noInstall {
			rows = append(rows, component.KeyValue{Key: fmt.Sprintf(fwInstallInstead, host.FormatArgv(fwInstallArgv(r.facts))), State: component.ValueNone})
		}
		return rows
	}
	rows := []component.KeyValue{{Key: fwKeyInstalled, Value: "Yes", State: component.ValueOK}}
	s := r.st.Status
	status := component.KeyValue{Key: fwKeyStatus, Value: "Inactive", State: component.ValueWarn}
	switch {
	case !s.Known || len(s.Bad) > 0:
		status = component.KeyValue{Key: fwKeyStatus, State: component.ValueUnknown}
	case s.Active:
		status = component.KeyValue{Key: fwKeyStatus, Value: "Active", State: component.ValueOK}
	}
	in := r.st.Incoming
	if s.Active {
		in = s.Incoming
	}
	policy := component.KeyValue{Key: fwKeyPolicy, State: component.ValueUnknown}
	switch in {
	case ufw.PolicyDeny:
		policy.Value, policy.State = "Deny", component.ValueOK
	case ufw.PolicyReject:
		policy.Value, policy.State = "Reject", component.ValueOK
	case ufw.PolicyAllow:
		policy.Value, policy.State = "Allow", component.ValueWarn
	}
	boot := component.KeyValue{Key: fwKeyBoot, Value: "Yes", State: component.ValueOK}
	switch {
	case !r.st.Enabled:
		boot.Value, boot.State = "No", component.ValueWarn
	case r.noRunlevel:
		boot.Value, boot.State = "In no runlevel", component.ValueWarn
	}
	return append(rows, status, policy, boot)
}

// ruleRows are the section Rules' rows: as ufw status verbose prints them
// while active, and while not, from its rule files, said as saved; each
// rule's state by a value state's marker, never colour alone. None is a muted
// item (P-30 (b), as DNS decided).
func (r fwReading) ruleRows() []component.KeyValue {
	rules, note := r.st.Rules, "Saved"
	if r.st.Status.Active {
		rules, note = r.st.Status.Rules, ""
	}
	if len(rules) == 0 {
		return []component.KeyValue{{Key: fwNoRules, State: component.ValueNone}}
	}
	rows := make([]component.KeyValue, len(rules))
	for i, rl := range rules {
		rows[i] = component.KeyValue{Key: fwRuleKey(rl), Value: fwActionWord(rl.Action), State: component.ValueBad, Note: note}
		if rl.Action == "allow" || rl.Action == "limit" {
			rows[i].State = component.ValueOK
		}
	}
	return rows
}

// fwActionWord is a rule's action, capitalised.
func fwActionWord(a string) string {
	if a == "" {
		return a
	}
	return strings.ToUpper(a[:1]) + a[1:]
}

// fwRuleKey is a rule as the row names it: its port, or Anywhere, then who
// from, in ufw's own forms.
func fwRuleKey(r ufw.Rule) string {
	to := "Anywhere"
	if r.DPort != "" {
		to = r.DPort
		if r.Proto != "any" && r.Proto != "" {
			to += "/" + r.Proto
		}
	}
	if r.Dst != "" {
		to = r.Dst + " " + to
	}
	if r.Iface != "" {
		to += " on " + r.Iface
	}
	switch {
	case r.Src != "":
		to += " from " + r.Src
	case r.Family == ufw.IPv6 && r.Dst == "":
		to += " (v6)"
	}
	if r.Dir != "" && r.Dir != "in" {
		to += " " + r.Dir
	}
	return to
}

// applied reports whether ufw's saved rules hold application name's rule.
func (r fwReading) applied(name string) bool {
	return slices.ContainsFunc(r.st.Rules, func(rl ufw.Rule) bool { return rl.App == name })
}

// profileRows are the profiles' rows: each one's name and ports, and whether
// it is in ufw. None is a muted item.
func (r fwReading) profileRows() []component.KeyValue {
	if len(r.cfg.UfwAppProfiles) == 0 {
		return []component.KeyValue{{Key: fwNoProfiles, State: component.ValueNone}}
	}
	var rows []component.KeyValue
	for _, p := range r.cfg.UfwAppProfiles {
		row := component.KeyValue{Key: p.Name, Value: strings.Join(p.Ports, ", "), Note: "Not applied", State: component.ValueWarn}
		if r.applied(p.Name) {
			row.Note, row.State = "Applied", component.ValueOK
		}
		rows = append(rows, row)
	}
	return rows
}

// fwSection is a section of rows.
func fwSection(title string, rows []component.KeyValue) screen.Section {
	return screen.Section{Header: component.SectionHeader{Title: title}, Rows: component.BulletList{Items: rows}}
}

// fwExit is every menu's Return.
var fwExit = component.MenuItem{Key: "0", Label: "Return"}

// subMenu is the screen of r, on the sub-menu layout, with no new component.
func (r fwReading) subMenu() screen.SubMenu {
	if r.onPVE {
		return r.pveMenu()
	}
	vm := screen.SubMenu{Title: component.TitleBar{Title: fwTitle}, Status: r.status, Heading: fwHeading, Prompt: fwPrompt,
		Menu: component.Menu{Exit: fwExit}}
	if r.proxmox != nil {
		vm.Menu.Items = []component.MenuItem{{Key: "1", Label: fwEnableLabel, Description: fwEnableAbout}}
		return vm
	}
	vm.Sections = []screen.Section{fwSection(fwSectionUFW, r.ufwRows()), fwSection(fwSectionSSH, r.sshRows())}
	if r.st.Installed {
		vm.Sections = append(vm.Sections, fwSection(fwSectionRules, r.ruleRows()))
	}
	if r.st.Installed && len(r.cfg.UfwAppProfiles) > 0 {
		vm.Sections = append(vm.Sections, fwSection(fwSectionProfiles, r.profileRows()))
	}
	switch r.first() {
	case fwDoInstall:
		vm.Menu.Items = []component.MenuItem{{Key: "1", Label: fwInstallLabel, Description: fwInstallAbout}}
	case fwDoEnable:
		vm.Menu.Items = []component.MenuItem{{Key: "1", Label: fwEnableLabel, Description: fwEnableAbout, Risk: r.risky()}}
	case fwDoDisable:
		vm.Menu.Items = []component.MenuItem{{Key: "1", Label: fwDisableLabel, Description: fwDisableAbout}}
	}
	if r.st.Installed {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: "2", Label: fwProfilesLabel, Description: fwProfilesAbout})
	}
	return vm
}

// risky is whether Enable's plan is lockout-risk.
func (r fwReading) risky() bool {
	return r.planErr == nil && slices.ContainsFunc(r.plan, func(c engine.Change) bool { return c.Risk == engine.RiskLockout })
}

// pveMenu is the screen on Proxmox VE, read only (brief 2.5): the Firewall
// row; whether pve-firewall is on, its default incoming policy, how far SSH
// and the web UI are let through; why hardn does not change it; Return alone.
// origin/main's title says UFW, which is false here: hence its own.
func (r fwReading) pveMenu() screen.SubMenu {
	enabled := component.KeyValue{Key: fwKeyEnabled, State: component.ValueUnknown}
	switch r.pf.Enabled {
	case detect.Yes:
		enabled.Value, enabled.State = "Yes", component.ValueOK
	case detect.No:
		enabled.Value, enabled.State = "No", component.ValueWarn
	}
	policy := component.KeyValue{Key: fwKeyPolicy, State: component.ValueUnknown}
	switch r.pf.PolicyIn {
	case "DROP":
		policy.Value, policy.State = "Drop", component.ValueOK
	case "REJECT":
		policy.Value, policy.State = "Reject", component.ValueOK
	case "ACCEPT":
		policy.Value, policy.State = "Accept", component.ValueWarn
	}
	if r.pf.Enabled == detect.No && policy.State == component.ValueOK {
		policy.State = component.ValuePlain // it filters nothing: not a state to mark good
	}
	rows := []component.KeyValue{enabled, policy, fwReach(fwKeySSH, r.pf.SSH), fwReach(fwKeyWeb, r.pf.Web), {Key: fwNoChange, State: component.ValueNone}}
	return screen.SubMenu{Title: component.TitleBar{Title: fwPVETitle}, Status: r.status,
		Sections: []screen.Section{fwSection(fwSectionPVE, rows)},
		Menu:     component.Menu{Exit: fwExit}, Prompt: fwPrompt}
}

// fwReach is how far pve-firewall lets a port through, in the row's words.
func fwReach(key string, reach pve.Reach) component.KeyValue {
	switch reach {
	case pve.ReachNotFiltered:
		return component.KeyValue{Key: key, Value: "Not filtered"}
	case pve.ReachAnywhere:
		return component.KeyValue{Key: key, Value: "From anywhere"}
	case pve.ReachManagement:
		return component.KeyValue{Key: key, Value: "Management network", State: component.ValueOK}
	}
	return component.KeyValue{Key: key, State: component.ValueUnknown}
}

// refusal is what item 1 says before asking, nil when it asks; from the real
// errors, in plain words.
func (r fwReading) refusal() component.Messages {
	var words string
	var be *ufw.BlockError
	switch {
	case r.proxmox != nil:
		words = fwProxmoxWords
	case r.first() != fwDoEnable || r.planErr == nil:
		return nil
	case errors.As(r.planErr, &be):
		words = fmt.Sprintf(fwBlocksWords, be.Coverage[0].Need.Port)
	case errors.Is(r.planErr, errFWNoSS):
		words = fwNoSSWords
	case errors.Is(r.planErr, ufw.ErrListenersUnknown):
		words = fwNoListeners
	case errors.Is(r.planErr, ufw.ErrStateUnknown):
		words = fwStateWords
	case errors.Is(r.planErr, ufw.ErrEnabledMismatch) && !r.st.Status.Active:
		words = fwNotRunningWords
	case errors.Is(r.planErr, ufw.ErrEnabledMismatch):
		words = fwNotBootWords
	default:
		words = screenkit.RefusalWords(r.planErr)
	}
	return screenkit.Warnings(words)
}

// errFWNoSS: no ss, so the listeners are not established. Gallery-local:
// steps.PlanUFWEnable reads sshd -T alone (§7's finding).
var errFWNoSS = fmt.Errorf("ss not installed: %w", ufw.ErrListenersUnknown)

// fwEnable is what Enable plans: steps.PlanUFWEnable, the real plan; refused
// first where ss cannot show what is bound, which that plan does not read
// (the stand-in); and on OpenRC, ufw in no runlevel, the stand-in's
// rc-update, after it, which v0.3.2 ran.
func fwEnable(ctx context.Context, sys host.System, f detect.Facts, session revert.Session) ([]engine.Change, error) {
	if _, _, err := reload.ReadBound(ctx, sys); errors.Is(err, host.ErrNotFound) {
		return nil, errFWNoSS
	}
	changes, err := steps.PlanUFWEnable(ctx, sys, engine.Input{Facts: f, Session: session}, fwID)
	if err != nil {
		return nil, err
	}
	if f.Init.Kind == detect.OpenRC {
		if _, in, _ := detect.ServiceRunlevels(ctx, sys, "ufw"); in == detect.No {
			c := fwCommand(fwID+"/runlevel", "Start ufw at boot", engine.StageFirewall, []string{"rc-update", "add", "ufw", "default"}, []string{"rc-update", "del", "ufw", "default"})
			for _, b := range changes {
				c.After = append(c.After, b.ID)
			}
			changes = append(changes, c)
		}
	}
	return changes, nil
}

// fwInstallArgv is the command that installs ufw there, and fwRemoveArgv its
// undo: apk's on Alpine, apt-get's elsewhere (Debian and Ubuntu; never asked
// on Proxmox VE). A stand-in: pkg/host/alpine has apk's, nothing has apt's
// (decision 1).
func fwInstallArgv(f detect.Facts) []string {
	if f.Platform.Distro == detect.DistroAlpine {
		return []string{"apk", "add", "ufw"}
	}
	return []string{"apt-get", "install", "-y", "ufw"}
}

func fwRemoveArgv(f detect.Facts) []string {
	if f.Platform.Distro == detect.DistroAlpine {
		return []string{"apk", "del", "ufw"}
	}
	return []string{"apt-get", "remove", "-y", "ufw"}
}

// fwCommand is a stand-in's change: one command and its undo, plain.
func fwCommand(id, title string, stage engine.Stage, argv, undo []string) engine.Change {
	return engine.Change{ID: id, Summary: title, Title: title, Target: "ufw", Commands: []string{host.FormatArgv(argv)}, Stage: stage, Resources: []string{"ufw"},
		Rollback: engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: undo, Why: "firewall"}}},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			_, err := sys.Run(ctx, argv[0], argv[1:]...)
			return engine.Ran{Detail: host.FormatArgv(argv)}, err
		}}
}

// fwControl is the gallery-local control, unit (b)'s: firewall.Incoming's id,
// title and check, its Plan what action does.
type fwControl struct {
	firewall.Incoming
	action fwAction
}

// Plan plans the action, reading the host where the engine plans.
func (c fwControl) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	switch c.action {
	case fwDoInstall:
		if err := ufw.Applies(in.Facts.Proxmox); err != nil {
			return nil, err
		}
		ch := fwCommand(fwID+"/install", "Install ufw", engine.StagePackages, fwInstallArgv(in.Facts), fwRemoveArgv(in.Facts))
		ch.StateBefore, ch.StateAfter = "not installed", "installed, inactive"
		return []engine.Change{ch}, nil
	case fwDoEnable:
		return fwEnable(ctx, sys, in.Facts, in.Session)
	case fwDoDisable:
		ch := fwCommand(fwID+"/disable", "Disable ufw", engine.StageFirewall, []string{"ufw", "disable"}, []string{"ufw", "--force", "enable"})
		ch.StateBefore, ch.StateAfter = "active", "inactive"
		return []engine.Change{ch}, nil
	case fwDoSave:
		return steps.PlanSave(ctx, sys, fwID+"/profiles", in.Config, []string{fwProfileSetting})
	case fwDoApply:
		return fwApply(ctx, sys, in)
	}
	return nil, nil
}

// fwProfileBytes is hardn's profile file for profiles, as origin/main writes
// it (ops/configure_secure_firewall_profiles/*.golden), ports joined as
// ufw-framework(8) has them.
func fwProfileBytes(profiles []config.UfwAppProfile) string {
	var b strings.Builder
	for _, p := range profiles {
		fmt.Fprintf(&b, "[%s]\ntitle=%s\ndescription=%s\nports=%s\n\n", p.Name, p.Title, p.Description, strings.Join(p.Ports, "|"))
	}
	return b.String()
}

// fwApply is the profiles' Apply, a stand-in: hardn's profile file written,
// with a diff and a backup, then origin/main's command for each profile not in
// ufw yet. Nothing to do plans nothing.
func fwApply(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	if len(in.Config.UfwAppProfiles) == 0 {
		return nil, nil
	}
	st, err := ufw.Read(ctx, sys, in.Facts.Proxmox)
	if err != nil {
		return nil, err
	}
	want := fwProfileBytes(in.Config.UfwAppProfiles)
	var before *string
	if data, err := sys.ReadFile(ctx, fwProfileFile); err == nil {
		was := string(data)
		before = &was
	}
	var argvs [][]string
	for _, p := range in.Config.UfwAppProfiles {
		if !slices.ContainsFunc(st.Rules, func(r ufw.Rule) bool { return r.App == p.Name }) {
			argvs = append(argvs, []string{"ufw", "allow", "from", "any", "to", "any", "app", p.Name})
		}
	}
	if before != nil && *before == want && len(argvs) == 0 {
		return nil, nil
	}
	ch := engine.Change{ID: fwID + "/apply", Summary: "Apply the application profiles", Title: "Apply the application profiles", Target: fwProfileFile,
		Diff: engine.FileDiff(fwProfileFile, before, &want), Stage: engine.StageFirewall, Backups: []string{fwProfileFile}, Resources: []string{"ufw"}}
	var undo []revert.Step
	for _, a := range argvs {
		ch.Commands = append(ch.Commands, host.FormatArgv(a))
		undo = append(undo, revert.Step{Op: revert.OpRun, Argv: append([]string{"ufw", "delete", "allow", "from", "any", "to", "any", "app"}, a[len(a)-1]), Why: "firewall"})
	}
	ch.Rollback = engine.Undo{Steps: undo}
	ch.Apply = func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
		if err := sys.MkdirAll(ctx, "/etc/ufw/applications.d", 0o755); err != nil {
			return engine.Ran{}, err
		}
		if err := sys.WriteFileAtomic(ctx, fwProfileFile, []byte(want), 0o644, 0, 0); err != nil {
			return engine.Ran{}, err
		}
		for _, a := range argvs {
			if _, err := sys.Run(ctx, a[0], a[1:]...); err != nil {
				return engine.Ran{}, err
			}
		}
		return engine.Ran{Detail: "applied"}, nil
	}
	return []engine.Change{ch}, nil
}

// fwRunner is one run of the Firewall screen on a simulated host.
type fwRunner struct {
	s     *session
	fh    fwHost
	h     *fakeHost
	title string
}

// openFW returns the action that opens what name names: the hosts' menu for
// fwPrefix alone, the screen on one host for "firewall/<host>".
func (s *session) openFW(name string) func() error {
	if fh, ok := findFWHost(strings.TrimPrefix(name, fwPrefix+"/")); ok && name != fwPrefix {
		return func() error { return s.fwScreen(context.Background(), fh, fh.host()) }
	}
	return func() error {
		all := fwHosts()
		vm := screen.SubMenu{Title: component.TitleBar{Title: "Firewall hosts"}, Heading: "Select a host", Prompt: "Enter your choice", Menu: component.Menu{Exit: fwExit}}
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, fh := range all {
			it := component.MenuItem{Key: fmt.Sprint(i + 1), Label: fh.label, Description: fh.about}
			vm.Menu.Items = append(vm.Menu.Items, it)
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.fwScreen(context.Background(), fh, fh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// fwScreen runs the Firewall screen on h, fh's host, until the user goes back
// from it: its note above it, then each round the screen read again.
func (s *session) fwScreen(ctx context.Context, fh fwHost, h *fakeHost) error {
	e := &fwRunner{s: s, fh: fh, h: h, title: fwPrefix + "/" + fh.name}
	if err := s.writeNote(e.title, fh.note); err != nil {
		return err
	}
	return s.p.Run(func() nav.Screen { return e.draw(ctx) })
}

// fwBind is vm's menu with actions by key.
func fwBind(vm screen.SubMenu, actions map[string]func() error) nav.Menu {
	menu := nav.Menu{Exit: vm.Menu.Exit}
	for _, it := range vm.Menu.Items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: actions[it.Key]})
	}
	return menu
}

// draw reads the host and returns the screen, its items bound: item 1 says
// why it cannot run before asking anything, or runs its change; item 2 opens
// the profiles' sub-screen.
func (e *fwRunner) draw(ctx context.Context) nav.Screen {
	r, err := e.s.readFW(ctx, e.h.system(), fwSession(e.h))
	if err != nil {
		return screen.SubMenu{Title: component.TitleBar{Title: fwTitle}, Menu: component.Menu{Exit: fwExit}, Prompt: fwPrompt}.
			Screen(e.s.p.Ctx, nav.Menu{Exit: fwExit})
	}
	vm := r.subMenu()
	actions := map[string]func() error{
		"1": func() error { return e.first(ctx, r) },
		"2": func() error { return e.s.p.Run(func() nav.Screen { return e.drawProfiles(ctx) }) },
	}
	if msgs := r.refusal(); msgs != nil {
		actions["1"] = e.kit(r).Say(msgs)
	}
	return vm.Screen(e.s.p.Ctx, fwBind(vm, actions))
}

// first is item 1 through the flow.
func (e *fwRunner) first(ctx context.Context, r fwReading) error {
	switch r.first() {
	case fwDoInstall:
		return e.change(ctx, r, fwDoInstall, "Install UFW", "Install ufw?", flow.Review{Title: fwSectionUFW,
			Items: []component.KeyValue{{Key: "Package", Value: "ufw"}, {Key: fwKeyStatus, Value: "Inactive"}}})
	case fwDoEnable:
		return e.change(ctx, r, fwDoEnable, "Enable Firewall", "Enable the firewall?", flow.Review{Title: fwSectionUFW,
			Items: []component.KeyValue{{Key: fwKeyStatus, Value: "Active", State: component.ValueOK}, {Key: fwKeyPolicy, Value: "Deny", State: component.ValueOK}, {Key: fwKeyBoot, Value: "Yes", State: component.ValueOK}}})
	case fwDoDisable:
		return e.change(ctx, r, fwDoDisable, "Disable Firewall", "Disable the firewall?", flow.Review{
			Notice: &flow.Notice{Callout: component.Callout{Kind: component.MessageWarn, Body: []string{fwDisableWarning}}},
			Title:  fwSectionUFW, Items: []component.KeyValue{{Key: fwKeyStatus, Value: "Inactive", State: component.ValueWarn}}})
	}
	return nil
}

// change runs one plain change of action through the flow.
func (e *fwRunner) change(ctx context.Context, r fwReading, action fwAction, title, question string, review flow.Review) error {
	return e.kit(r).Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: title}},
		Flow: flow.Flow{Review: func() flow.Review { return review }, Confirm: fwConfirm(question)},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{fwControl{action: action}}
		},
	})
}

// fwConfirm is the flow's question: y/N, or the hostname typed for a
// lockout-risk plan.
func fwConfirm(question string) func(flow.Plan) flow.Confirm {
	return func(p flow.Plan) flow.Confirm {
		if p.Typed() {
			return flow.Confirm{Question: question, Label: typeTheHostname, Phrase: hostName}
		}
		return flow.Confirm{Question: question}
	}
}

// drawProfiles is the sub-screen: the profiles; Add, and with any, Remove and
// Apply, each through the flow, under plan only stopping at its plan (P-03),
// where origin/main refuses Add and Remove.
func (e *fwRunner) drawProfiles(ctx context.Context) nav.Screen {
	r, err := e.s.readFW(ctx, e.h.system(), fwSession(e.h))
	if err != nil {
		return screen.SubMenu{Title: component.TitleBar{Title: fwProfilesTitle}, Menu: component.Menu{Exit: fwExit}, Prompt: fwPrompt}.
			Screen(e.s.p.Ctx, nav.Menu{Exit: fwExit})
	}
	vm := r.profilesMenu()
	return vm.Screen(e.s.p.Ctx, fwBind(vm, map[string]func() error{
		"1": func() error { return e.addProfile(ctx, r) },
		"2": func() error { return e.removeProfile(ctx, r) },
		"3": func() error {
			return e.change(ctx, r, fwDoApply, "Apply Profiles", "Apply the profiles?", flow.Review{Title: fwSectionProfiles, Items: r.profileRows()})
		},
	}))
}

// profilesMenu is the sub-screen's layout.
func (r fwReading) profilesMenu() screen.SubMenu {
	vm := screen.SubMenu{Title: component.TitleBar{Title: fwProfilesTitle}, Sections: []screen.Section{fwSection(fwSectionProfiles, r.profileRows())},
		Heading: fwHeading, Prompt: fwPrompt, Menu: component.Menu{Exit: fwExit, Items: []component.MenuItem{{Key: "1", Label: fwAddLabel, Description: fwAddAbout}}}}
	if len(r.cfg.UfwAppProfiles) > 0 {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: "2", Label: fwRemoveLabel, Description: fwRemoveAbout},
			component.MenuItem{Key: "3", Label: fwApplyLabel, Description: fwApplyAbout})
	}
	return vm
}

// addProfile is Add: origin/main's four questions, then the save.
func (e *fwRunner) addProfile(ctx context.Context, r fwReading) error {
	var name, title, about, ports string
	fields := []form.Field{
		&form.Text{Title: "Profile name", Description: "Such as WebServer.", Value: &name, Validate: func(s string) error {
			switch {
			case !fwNameForm.MatchString(s):
				return errors.New("Use letters, digits, dots, dashes and underscores.")
			case slices.ContainsFunc(r.cfg.UfwAppProfiles, func(p config.UfwAppProfile) bool { return p.Name == s }):
				return nav.Warn("A profile with this name already exists.")
			}
			return nil
		}},
		&form.Text{Title: "Title", Value: &title},
		&form.Text{Title: "Description", Value: &about},
		&form.Text{Title: "Ports", Description: "Such as 80/tcp,443/tcp.", Value: &ports, Validate: func(s string) error {
			for _, p := range strings.Split(s, ",") {
				if !fwPortForm.MatchString(strings.TrimSpace(p)) {
					return errors.New("Give each port with its protocol, such as 80/tcp.")
				}
			}
			return nil
		}},
	}
	return e.kit(r).Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: "Add Profile"}},
		Flow: flow.Flow{
			Form: form.Form{Fields: fields},
			Review: func() flow.Review {
				return flow.Review{Title: fwSectionProfiles, Items: []component.KeyValue{{Key: name, Value: strings.Join(fwPorts(ports), ", ")}}}
			},
			Confirm: func(p flow.Plan) flow.Confirm { return flow.Confirm{Question: "Add " + name + "?"} },
		},
		Controls: func(edited *config.Config) []engine.Control {
			edited.UfwAppProfiles = append(slices.Clone(edited.UfwAppProfiles), config.UfwAppProfile{Name: name, Title: title, Description: about, Ports: fwPorts(ports)})
			return []engine.Control{fwControl{action: fwDoSave}}
		},
	})
}

// fwPorts is the form's ports, split.
func fwPorts(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// removeProfile is Remove: which one, then the save. A rule already in ufw
// stays, as on origin/main, and the review says so.
func (e *fwRunner) removeProfile(ctx context.Context, r fwReading) error {
	gone := 0
	opts := make([]form.Option[int], len(r.cfg.UfwAppProfiles))
	for i, p := range r.cfg.UfwAppProfiles {
		opts[i] = form.Option[int]{Label: p.Name, Value: i}
	}
	return e.kit(r).Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: "Remove Profile"}},
		Flow: flow.Flow{
			Form: form.Form{Fields: []form.Field{&form.Select[int]{Title: "Remove which profile?", Options: opts, Value: &gone}}},
			Review: func() flow.Review {
				return flow.Review{Notice: &flow.Notice{Callout: component.Callout{Kind: component.MessageWarn, Body: []string{fwRemoveNotice}}}}
			},
			Confirm: func(p flow.Plan) flow.Confirm {
				return flow.Confirm{Question: "Remove " + r.cfg.UfwAppProfiles[gone].Name + "?"}
			},
		},
		Controls: func(edited *config.Config) []engine.Control {
			edited.UfwAppProfiles = slices.Delete(slices.Clone(edited.UfwAppProfiles), gone, gone+1)
			return []engine.Control{fwControl{action: fwDoSave}}
		},
	})
}

// kit is the screen's screenkit.Session: the change's engine the gallery's,
// over the host as the engine reaches it, its backups the defaults, the
// gallery's hold, its apply recorded for the end of play; a dry run when play
// plays the dry-run sequence. Its session is on the port sshd is bound on
// (fwSession).
func (e *fwRunner) kit(r fwReading) screenkit.Session {
	s, h := e.s, e.h
	var rec *recorder
	cfg := r.cfg
	if cfg == nil {
		cfg = &config.Config{}
	}
	return screenkit.Session{
		Name: "firewall screen", Prompter: s.p, System: h.system(), Config: cfg,
		DryRun: s.sequence == "dry-run", LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			rc := &recorder{host: h, f: h.f, title: e.title}
			rec = rc
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, rc)
			s.mu.Unlock()
			rc.begin()
			return engine.New(h.system(), engine.Options{
				Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
				Session:  fwSession(h),
				Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
				Hold:     s.hold,
				PID:      applyPID,
				Progress: func(ev engine.Event) { rc.event(ev); progress(ev) },
			})
		},
		Ran: func(run output.Run) {
			rec.end()
			rec.setPlan(run.Plan)
			rec.failedWith(run.Err)
		},
	}
}

// fwReadNamed reads the host named name, fresh, as the screen does.
func fwReadNamed(ctx context.Context, name string) (fwReading, error) {
	fh, ok := findFWHost(name)
	if !ok {
		return fwReading{}, fmt.Errorf("no firewall host %q", name)
	}
	h := fh.host()
	return (&session{}).readFW(ctx, h.system(), fwSession(h))
}

// fwOnHost returns a drawing of the host named name's reading.
func fwOnHost(name string, view func(fwReading, component.Ctx) string) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		r, err := fwReadNamed(ctx, name)
		if err != nil {
			return "", err
		}
		return view(r, c), nil
	}
}

// fwMocks are the listing's mocks, in order: the screen on each host; the
// profiles' sub-screen, with two and with none; decision 1's alternative; the
// refusals; and what PlanUFWEnable does on the unreloaded edit, with an SSH
// session and from a console.
func fwMocks() []listMock {
	var mocks []listMock
	for _, fh := range fwHosts() {
		mocks = append(mocks, listMock{name: "screen-" + fh.name, note: "The Firewall screen. " + fh.note,
			draw: fwOnHost(fh.name, func(r fwReading, c component.Ctx) string { return r.subMenu().Render(c) })})
	}
	return append(mocks,
		listMock{name: "profiles", note: "Application Profiles, the sub-screen, on the profiles host: Add, Remove, Apply.",
			draw: fwOnHost("profiles", func(r fwReading, c component.Ctx) string { return r.profilesMenu().Render(c) })},
		listMock{name: "profiles-none", note: "Application Profiles on a host whose hardn.yml has none: Add alone.",
			draw: fwOnHost("active22", func(r fwReading, c component.Ctx) string { return r.profilesMenu().Render(c) })},
		listMock{name: "no-install", note: "Decision 1's alternative, not recommended: no Install item on the debian13 host; the screen says what to run.",
			draw: fwOnHost("debian13", func(r fwReading, c component.Ctx) string {
				r.noInstall = true
				return r.subMenu().Render(c)
			})},
		listMock{name: "refusals", note: "Item 1's refusals before asking, each from the real error: no ss, ufw's state not read, ENABLED and the chains disagreeing, a saved blocklist, a saved deny, Proxmox VE not established.",
			draw: fwRefusalsMock},
		listMock{name: "stale-listener", note: "The edit-2230 host: Port 2230 configured, not reloaded, sshd accepting on 22. What the real steps.PlanUFWEnable plans, with an SSH session on 22, and from a console, no session: a finding for unit (b).",
			draw: fwStaleMock},
	)
}

// fwSavedDeny is ufw inactive with a deny on SSH's port saved first.
func fwSavedDeny() *fakeHost {
	return fwWith(ufwSim{incoming: "deny", rules: []simRule{{action: "deny", port: "22"}}})()
}

// fwRefusalHosts are the refusals' hosts, by what each shows.
func fwRefusalHosts() []struct {
	name string
	host func() *fakeHost
} {
	return []struct {
		name string
		host func() *fakeHost
	}{
		{"no ss", fwNoSS},
		{"ufw's state not read", fwGarbled},
		{"lockout-at-start", func() *fakeHost { return ufwOn(lockoutAtStart().withHardnYML(fwYML)) }},
		{"blocklist", fwWith(ufwSim{incoming: "deny", rules: []simRule{{action: "deny", port: "22", src: "198.51.100.0/24"}, {action: "allow", port: "22"}}})},
		{"a saved deny", fwSavedDeny},
		{"Proxmox VE not established", fwProxmoxUnknown},
	}
}

// fwRefusalsMock draws each refusal under a header naming its host.
func fwRefusalsMock(ctx context.Context, c component.Ctx) (string, error) {
	var out []string
	for _, rh := range fwRefusalHosts() {
		h := rh.host()
		r, err := (&session{}).readFW(ctx, h.system(), fwSession(h))
		if err != nil {
			return "", fmt.Errorf("%s: %w", rh.name, err)
		}
		msgs := r.refusal()
		if msgs == nil {
			return "", fmt.Errorf("%s: no refusal", rh.name)
		}
		out = append(out, header(c, "item 1, "+rh.name), msgs.Render(c))
	}
	return apart(out...), nil
}

// fwStaleMock draws the real plan on the edit-2230 host, with and without a
// session, and the port sshd accepts on against it.
func fwStaleMock(ctx context.Context, c component.Ctx) (string, error) {
	var out []string
	for _, v := range []struct {
		title   string
		session revert.Session
	}{{"With an SSH session on 22", applySession}, {"From a console", revert.Session{}}} {
		h := fwEdit()
		f, err := detect.Detect(ctx, h.system())
		if err != nil {
			return "", err
		}
		changes, err := steps.PlanUFWEnable(ctx, h.system(), engine.Input{Facts: f, Session: v.session}, fwID)
		if err != nil {
			return "", err
		}
		var rows []component.KeyValue
		allowed22 := false
		for _, ch := range changes {
			for _, cmd := range ch.Commands {
				rows = append(rows, component.KeyValue{Key: cmd})
				allowed22 = allowed22 || cmd == "ufw allow 22/tcp"
			}
		}
		verdict := component.KeyValue{Key: "Port 22, sshd now", Value: "Let through", State: component.ValueOK}
		if !allowed22 {
			verdict.Value, verdict.State = "Refused", component.ValueBad
		}
		out = append(out, component.SectionHeader{Title: v.title}.Render(c), component.BulletList{Items: append(rows, verdict)}.Render(c))
	}
	return apart(out...), nil
}

// fwUsage is the listing's usage line.
const fwUsage = "usage: go run ./internal/tools/uigallery firewall [flags] [mock | all]..."

// fwListing is the Firewall listing.
var fwListing = listing{prefix: fwPrefix, usage: fwUsage, label: "Mock:", mocks: fwMocks}

// fwStart returns the hosts' menu, or the host, that args name ("firewall",
// "firewall/<host>"), and whether they name one; an error for a host that is
// not one.
func fwStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != fwPrefix && !strings.HasPrefix(args[0], fwPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], fwPrefix+"/"); args[0] != fwPrefix {
		if _, ok := findFWHost(name); !ok {
			var names []string
			for _, h := range fwHosts() {
				names = append(names, fwPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no firewall host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}

// fwNote is play's note for the Firewall mocks.
const fwNote = "Play mode, mocks: the Firewall screen proposed for slice 4, drawn from what real code reads of a simulated host " +
	"(firewall.Incoming.Check, ufw.Read, pve.ReadFirewall, sshd.ReadEffective, reload.ReadBound), Enable the real steps.PlanUFWEnable on the real plan/apply engine. " +
	"Install, Disable, the runlevel and the profiles' plans are the gallery's stand-ins, unit (b)'s to build. Nothing on this machine changes: every command and file is the fake's, " +
	"and its record is shown when play ends."
