package main

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
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
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Firewall mocks (refactor plan slice 4, unit (a), next/s4-gallery; on the
// real control since unit (b), next/s4-control): the Firewall screen, main menu
// 4, nothing of it wired into hardn. It starts from origin/main's screen
// (pkg/menu/firewall_menu.go) on the design system's sub-menu layout, with no
// new component. Everything it reads and changes is real code on a simulated
// host (firewallhost.go): firewall.Read's Reading, and each change
// firewall.Incoming's plan on the real engine, through the canonical flow
// (screenkit.Session.Change). Refusals are the control's words.
//
// What remains here is the screen, unit (c)'s to move: its words, rows and
// layout, and the profiles' form with its checks.
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
	fwKeyIPv6      = "IPv6"
	fwKeyEnabled   = "Enabled"
	fwKeyService   = "Service"
	fwKeyBound     = "Bound sockets"
	fwKeySSH       = "SSH (22)"
	fwKeyWeb       = "Web UI (8006)"

	fwNoRules    = "No firewall rules configured"
	fwNoProfiles = "No application profiles configured"
	fwNoChange   = "hardn reads pve-firewall and doesn't change it: a wrong rule can cut off the web UI and the cluster."

	fwInstallLabel  = "Install UFW"
	fwInstallAbout  = "Install the ufw package"
	fwEnableLabel   = "Enable firewall"
	fwEnableAbout   = "Let SSH through, then start ufw"
	fwDenyLabel     = "Set deny policy"
	fwDenyAbout     = "Let SSH through, deny by default"
	fwAllowLabel    = "Let SSH through"
	fwAllowAbout    = "Add its rules; other rules stay"
	fwAllowBootText = "Add its rules, start at boot"
	fwBootLabel     = "Start ufw at boot"
	fwBootAbout     = "Add it to the boot services"
	fwRepairLabel   = "Repair firewall"
	fwRepairAbout   = "Refused here: select to see why"
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
)

// The profiles' form: origin/main's fields (firewall_menu.go:addAppProfile),
// and its checks, shortened; a name and ports narrowed to what ufw's profile
// file and hardn's commands can hold.
var (
	fwNameForm = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	fwPortForm = regexp.MustCompile(`^([0-9]{1,5})(?::([0-9]{1,5}))?/(tcp|udp)$`)
)

// fwPortOK: a port or an ascending range, each from 1 to 65535, with its
// protocol. That ufw refuses a profile outside this, or with an empty title or
// description, is from applications.py as remembered, not run.
func fwPortOK(s string) bool {
	m := fwPortForm.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	from, _ := strconv.Atoi(m[1])
	to := from
	if m[2] != "" {
		to, _ = strconv.Atoi(m[2])
		if to <= from {
			return false
		}
	}
	return from >= 1 && to <= 65535
}

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
			note: deb + "sshd on 2230, ufw active allowing 443 alone: a new SSH session is refused. Item 1 lets it through."},
		{name: "lockout-at-start", label: "Lockout at start", about: "Stopped, saved rules refuse SSH", host: func() *fakeHost { return ufwOn(lockoutAtStart().withHardnYML(fwYML)) },
			note: deb + "sshd on 2230, ufw stopped with ENABLED=yes, its saved rules allowing 443 alone: the next boot refuses SSH. hardn refuses to plan over the disagreement."},
		{name: "allowlist", label: "Allowlist", about: "SSH from some sources", host: fwWith(ufwSim{incoming: "deny", active: true, enabled: true, rules: []simRule{{action: "allow", port: "22", src: "192.0.2.0/24"}, {action: "allow", port: "22", src: "2001:db8::/32"}}}),
			note: deb + "ufw active, default deny incoming, SSH let through from 192.0.2.0/24 and 2001:db8::/32 alone."},
		{name: "blocklist", label: "Blocklist", about: "A saved deny before the allow", host: fwWith(ufwSim{incoming: "deny", rules: []simRule{{action: "deny", port: "22", src: "198.51.100.0/24"}, {action: "allow", port: "22"}}}),
			note: deb + "ufw inactive, its saved rules denying 198.51.100.0/24 on 22 before allowing 22: whether the session's own address is among them is not established, so Enable refuses (ufw.ErrBlocks; P-33 (3))."},
		{name: "edit-2230", label: "Edit not reloaded", about: "Configured 2230, sshd on 22", host: fwEdit,
			note: deb + "Port 2230 in hardn's drop-in, sshd not reloaded, so still on 22, ufw inactive. What accepts SSH is not what is configured, so Enable refuses (decision 2; mock stale-listener has what the real plan does)."},
		{name: "moved-2230", label: "Bound elsewhere", about: "Configured 22, sshd on 2230", host: fwMoved,
			note: deb + "ufw active, allowing 22, sshd -T says 22, 2230 bound: ufw refuses what accepts SSH, and ss doesn't say whose 2230 is, so it gets no row. Enable refuses (decision 2)."},
		{name: "denied", label: "Denied", about: "ufw denies 22", host: fwWith(ufwSim{incoming: "deny", active: true, enabled: true, rules: []simRule{{action: "deny", port: "22"}}}),
			note: deb + "ufw active, with deny 22/tcp: hardn doesn't change a rule it finds, so it has nothing to plan and refuses, saying what to do."},
		{name: "alpine324", label: "Alpine 3.24", about: "No ufw", host: func() *fakeHost { return fwNoUFW(alpine324Platform) },
			note: alp + "ufw is not installed (community repository, ufw.md §3)."},
		{name: "alpine-stock", label: "Alpine 3.23, stock", about: "No ufw, no ss, no index", host: fwAlpineStock,
			note: hostFQDN + " runs Alpine 3.23 as a stock VM does (alpine323-vm.log): no ufw, no ss, apk's index empty, so Install refuses."},
		{name: "alpine-runlevel", label: "Alpine, no runlevel", about: "Active, gone after a reboot", host: fwAlpineActive,
			note: alp + "ufw active, deny incoming, allowing 22, ENABLED=yes, in no runlevel, so it does not come back after a reboot (ufw.md §6, [I]). v0.3.2 ran rc-update add ufw default. The Firewall row says off after restart."},
		{name: "unit-off", label: "Service off", about: "Active, gone after a reboot", host: fwWith(ufwSim{incoming: "deny", active: true, enabled: true, unitOff: true, rules: []simRule{{action: "allow", port: "22"}}}),
			note: deb + "ufw active, deny incoming, allowing 22, ENABLED=yes, ufw.service disabled. Whether that stops ufw at boot on Debian is [unverified] (ufw.md)."},
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

// fwReading is one reading of a host, as the screen draws it: firewall.Read's,
// with the Firewall row, as the main menu draws it, and what the screen's
// changes plan with. It changes nothing.
type fwReading struct {
	firewall.Reading
	status component.StatusList
	facts  detect.Facts
	cfg    *config.Config
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
	if onPVE, perr := pve.Applies(r.facts.Proxmox); !onPVE && perr == nil {
		if r.cfg, err = statusConfig(ctx, sys); err != nil {
			return r, err
		}
	}
	r.Reading = firewall.Read(ctx, sys, r.facts, r.cfg, session)
	return r, r.PVEErr
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
	if !r.Sockets.Read {
		return []component.KeyValue{{Key: "Listeners", State: component.ValueUnknown}}
	}
	active := r.State.Status.Known && r.State.Status.Active
	var unbound, configured []string
	for _, l := range r.Sockets.Unbound {
		unbound = append(unbound, l.String())
	}
	for _, l := range r.Sockets.Conf.All {
		configured = append(configured, l.String())
	}
	var rows []component.KeyValue
	for _, c := range r.Coverage() {
		key := strings.TrimPrefix(c.Need.Why, "listener ")
		row := component.KeyValue{Key: key}
		switch {
		case active:
			row.Value, row.State = fwVerdict(c.Verdict)
		case r.State.Installed && r.State.Enabled && r.State.Known() && !c.Verdict.OK():
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
		if row.Note != "" && (row.State == component.ValuePlain || row.State == component.ValueOK) {
			row.State = component.ValueWarn
		}
		rows = append(rows, row)
	}
	switch {
	case r.Sockets.NoSS:
		rows = append(rows, component.KeyValue{Key: fwKeyBound, Value: "ss not installed", State: component.ValueWarn})
	case r.Sockets.SSFailed:
		rows = append(rows, component.KeyValue{Key: fwKeyBound, Value: "ss failed", State: component.ValueWarn})
	case r.Sockets.NoneBound():
		rows = append(rows, component.KeyValue{Key: fwKeyBound, Value: "None on SSH's ports", State: component.ValueWarn})
	}
	return rows
}

// ufwRows are the section ufw's rows.
func (r fwReading) ufwRows() []component.KeyValue {
	if r.StateErr != nil {
		return []component.KeyValue{{Key: fwKeyInstalled, State: component.ValueUnknown}}
	}
	if !r.State.Installed {
		return []component.KeyValue{{Key: fwKeyInstalled, Value: "No", State: component.ValueWarn}}
	}
	rows := []component.KeyValue{{Key: fwKeyInstalled, Value: "Yes", State: component.ValueOK}}
	s := r.State.Status
	status := component.KeyValue{Key: fwKeyStatus, Value: "Inactive", State: component.ValueWarn}
	switch {
	case !s.Known || len(s.Bad) > 0:
		status = component.KeyValue{Key: fwKeyStatus, State: component.ValueUnknown}
	case s.Active:
		status = component.KeyValue{Key: fwKeyStatus, Value: "Active", State: component.ValueOK}
	}
	in := r.State.Incoming
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
	if !s.Active && policy.State == component.ValueOK {
		policy.State = component.ValuePlain // it filters nothing: not a state to mark good
	}
	boot := component.KeyValue{Key: fwKeyBoot, Value: "Yes", State: component.ValueOK}
	switch {
	case !r.State.Enabled:
		boot.Value, boot.State = "No", component.ValueWarn
	case r.Boot == detect.Unknown:
		boot = component.KeyValue{Key: fwKeyBoot, State: component.ValueUnknown}
	case r.Boot == detect.No && r.facts.Init.Kind == detect.OpenRC:
		boot.Value, boot.State = "In no runlevel", component.ValueWarn
	case ufw.BootMasked(r.BootUnit):
		boot.Value, boot.State = "Service masked", component.ValueWarn
	case r.BootUnit == "not-found":
		boot.Value, boot.State = "No service", component.ValueWarn
	case r.Boot == detect.No:
		boot.Value, boot.State = "Service disabled", component.ValueWarn
	}
	rows = append(rows, status, policy, boot)
	if !r.State.IPv6 && !r.State.NoKernelIPv6 {
		rows = append(rows, component.KeyValue{Key: fwKeyIPv6, Value: "Off", State: component.ValueWarn})
	}
	return rows
}

// ruleRows are the section Rules' rows: as ufw status verbose prints them
// while active, and while not, from its rule files, said as saved; each
// rule's state by a value state's marker, never colour alone: saved, an allow
// marks nothing good, ufw filtering nothing. None is a muted item (P-30 (b), as
// DNS decided).
func (r fwReading) ruleRows() []component.KeyValue {
	rules, note := r.State.Rules, "Saved"
	if r.State.Status.Active {
		rules, note = r.State.Status.Rules, ""
	}
	if len(rules) == 0 {
		return []component.KeyValue{{Key: fwNoRules, State: component.ValueNone}}
	}
	rows := make([]component.KeyValue, len(rules))
	for i, rl := range rules {
		rows[i] = component.KeyValue{Key: fwRuleKey(rl), Value: fwActionWord(rl.Action), State: component.ValueBad, Note: note}
		if rl.Action == "allow" || rl.Action == "limit" {
			rows[i].State = component.ValueOK
			if note != "" {
				rows[i].State = component.ValuePlain
			}
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

// profileRows are the profiles' rows: each one's name and ports, and whether
// it is in ufw. None is a muted item.
func (r fwReading) profileRows() []component.KeyValue {
	if len(r.Profiles) == 0 {
		return []component.KeyValue{{Key: fwNoProfiles, State: component.ValueNone}}
	}
	var rows []component.KeyValue
	for _, p := range r.Profiles {
		row := component.KeyValue{Key: p.Name, Value: strings.Join(p.Ports, ", "), Note: "Not applied", State: component.ValueWarn}
		if r.Applied(p.Name) {
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
	if r.OnPVE {
		return r.pveMenu()
	}
	vm := screen.SubMenu{Title: component.TitleBar{Title: fwTitle}, Status: r.status, Heading: fwHeading, Prompt: fwPrompt,
		Menu: component.Menu{Exit: fwExit}}
	if r.Proxmox != nil {
		vm.Menu.Items = []component.MenuItem{{Key: "1", Label: fwEnableLabel, Description: fwEnableAbout}}
		return vm
	}
	vm.Sections = []screen.Section{fwSection(fwSectionUFW, r.ufwRows()), fwSection(fwSectionSSH, r.sshRows())}
	if r.State.Installed {
		vm.Sections = append(vm.Sections, fwSection(fwSectionRules, r.ruleRows()))
	}
	if r.State.Installed && len(r.Profiles) > 0 {
		vm.Sections = append(vm.Sections, fwSection(fwSectionProfiles, r.profileRows()))
	}
	switch r.First {
	case firewall.ItemInstall:
		vm.Menu.Items = []component.MenuItem{{Key: "1", Label: fwInstallLabel, Description: fwInstallAbout}}
	case firewall.ItemEnable:
		w := r.enableWords()
		vm.Menu.Items = []component.MenuItem{{Key: "1", Label: w.label, Description: w.about, Risk: r.risky()}}
	case firewall.ItemDisable:
		vm.Menu.Items = []component.MenuItem{{Key: "1", Label: fwDisableLabel, Description: fwDisableAbout}}
	}
	if r.State.Installed {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: "2", Label: fwProfilesLabel, Description: fwProfilesAbout})
	}
	if r.DisableBeside {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: "3", Label: fwDisableLabel, Description: fwDisableAbout})
	}
	return vm
}

// fwItemWords are item 1's words and its flow's, for what its plan does.
type fwItemWords struct{ label, about, title, question string }

// enableWords says what Enable's plan does on this host: starting ufw only
// while it is not running, and on an active ufw the steps it plans, so the words
// hold wherever they are drawn.
func (r fwReading) enableWords() fwItemWords {
	if !r.State.Status.Active {
		return fwItemWords{fwEnableLabel, fwEnableAbout, "Enable Firewall", "Enable the firewall?"}
	}
	switch d := r.Does; {
	case r.PlanErr != nil:
		return fwItemWords{fwRepairLabel, fwRepairAbout, "", ""}
	case d.SetsDeny:
		return fwItemWords{fwDenyLabel, fwDenyAbout, "Set Deny Policy", "Set the deny policy?"}
	case d.Allows && d.Boot:
		return fwItemWords{fwAllowLabel, fwAllowBootText, "Let SSH Through", "Let SSH through?"}
	case d.Allows:
		return fwItemWords{fwAllowLabel, fwAllowAbout, "Let SSH Through", "Let SSH through?"}
	}
	return fwItemWords{fwBootLabel, fwBootAbout, "Start at Boot", "Start ufw at boot?"}
}

// risky is whether Enable's plan is lockout-risk.
func (r fwReading) risky() bool {
	return r.PlanErr == nil && slices.ContainsFunc(r.Plan, func(c engine.Change) bool { return c.Risk == engine.RiskLockout })
}

// pveMenu is the screen on Proxmox VE, read only (brief 2.5): the Firewall
// row; whether pve-firewall is on, its default incoming policy, how far SSH
// and the web UI are let through; why hardn does not change it; Return alone.
// origin/main's title says UFW, which is false here: hence its own.
func (r fwReading) pveMenu() screen.SubMenu {
	enabled := component.KeyValue{Key: fwKeyEnabled, State: component.ValueUnknown}
	switch r.PVE.Enabled {
	case detect.Yes:
		enabled.Value, enabled.State = "Yes", component.ValueOK
	case detect.No:
		enabled.Value, enabled.State = "No", component.ValueWarn
	}
	policy := component.KeyValue{Key: fwKeyPolicy, State: component.ValueUnknown}
	switch r.PVE.PolicyIn {
	case "DROP":
		policy.Value, policy.State = "Drop", component.ValueOK
	case "REJECT":
		policy.Value, policy.State = "Reject", component.ValueOK
	case "ACCEPT":
		policy.Value, policy.State = "Accept", component.ValueWarn
	}
	service := component.KeyValue{Key: fwKeyService, State: component.ValueUnknown}
	switch r.PVE.Service.IsActive() {
	case detect.Yes:
		service.Value, service.State = "Running", component.ValueOK
	case detect.No:
		service.Value, service.State = "Stopped", component.ValueWarn
	}
	ssh, web := fwReach(fwKeySSH, r.PVE.SSH), fwReach(fwKeyWeb, r.PVE.Web)
	// Disabled or stopped it filters nothing that is read: not a state to mark
	// good, whatever its files say.
	if r.PVE.Enabled != detect.Yes || service.State != component.ValueOK {
		for _, kv := range []*component.KeyValue{&enabled, &policy, &ssh, &web} {
			if kv.State == component.ValueOK {
				kv.State = component.ValuePlain
			}
		}
	}
	if ls, ok := r.Sockets.Listeners(); ok {
		var others []string
		for _, p := range ls.Ports() {
			if p != pve.SSHPort {
				others = append(others, strconv.Itoa(int(p)))
			}
		}
		if len(others) > 0 {
			ssh.Note = "sshd on " + strings.Join(others, ", ")
			if ssh.State == component.ValueOK || ssh.State == component.ValuePlain {
				ssh.State = component.ValueWarn
			}
		}
	}
	rows := []component.KeyValue{service, enabled, policy, ssh, web}
	for _, f := range r.PVE.Findings {
		rows = append(rows, fwPVEFinding(f))
	}
	rows = append(rows, component.KeyValue{Key: fwNoChange, State: component.ValueNone})
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

// fwPVEFinding is what pve-firewall's reading found, as a row.
func fwPVEFinding(f pve.Finding) component.KeyValue {
	row := component.KeyValue{Key: "Finding", Value: f.Kind.String(), State: component.ValueWarn}
	switch f.Kind {
	case pve.FindingRuleMayBlock:
		row.Key, row.Value = "Rules", "One may block"
	case pve.FindingBlacklist:
		row.Key, row.Value = "Blacklist", "Has entries"
	case pve.FindingNotModelled:
		row.Key, row.Value = "proxmox-firewall", "Not read"
	}
	return row
}

// refusal is what item 1 says before asking, nil when it asks: the control's
// words.
func (r fwReading) refusal() component.Messages {
	if r.PlanErr == nil {
		return nil
	}
	return screenkit.Warnings(screenkit.RefusalWords(r.PlanErr))
}

// applyRefusal is what Apply says before asking, nil when it asks: the
// control's words for its plan's refusal.
func (r fwReading) applyRefusal(ctx context.Context, sys host.System) component.Messages {
	in := engine.Input{Facts: r.facts, Config: r.cfg}
	if _, err := (firewall.Incoming{Action: firewall.ActionApplyProfiles}).Plan(ctx, sys, in); err != nil {
		return screenkit.Warnings(screenkit.RefusalWords(err))
	}
	return nil
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
		"3": func() error { return e.disable(ctx, r) },
	}
	if msgs := r.refusal(); msgs != nil {
		actions["1"] = e.kit(r).Say(msgs)
	}
	return vm.Screen(e.s.p.Ctx, fwBind(vm, actions))
}

// first is item 1 through the flow.
func (e *fwRunner) first(ctx context.Context, r fwReading) error {
	switch r.First {
	case firewall.ItemInstall:
		return e.change(ctx, r, firewall.ActionInstall, "Install UFW", "Install ufw?", flow.Review{Title: fwSectionUFW,
			Items: []component.KeyValue{{Key: "Package", Value: "ufw"}, {Key: fwKeyStatus, Value: "Inactive"}}})
	case firewall.ItemEnable:
		w := r.enableWords()
		return e.change(ctx, r, firewall.ActionEnable, w.title, w.question, flow.Review{Title: fwSectionUFW, Items: r.enableReview()})
	case firewall.ItemDisable:
		return e.disable(ctx, r)
	}
	return nil
}

// disable is Disable through the flow, from item 1 or its own item.
func (e *fwRunner) disable(ctx context.Context, r fwReading) error {
	return e.change(ctx, r, firewall.ActionDisable, "Disable Firewall", "Disable the firewall?", flow.Review{
		Notice: &flow.Notice{Callout: component.Callout{Kind: component.MessageWarn, Body: []string{fwDisableWarning}}},
		Title:  fwSectionUFW, Items: []component.KeyValue{{Key: fwKeyStatus, Value: "Inactive", State: component.ValueWarn}}})
}

// enableReview is ufw as Enable's plan leaves it: active, its policy, and at
// boot. Its service not read, Enable refuses before this.
func (r fwReading) enableReview() []component.KeyValue {
	policy := "Deny"
	if !r.Does.SetsDeny && r.State.Status.Incoming == ufw.PolicyReject {
		policy = "Reject"
	}
	return []component.KeyValue{{Key: fwKeyStatus, Value: "Active", State: component.ValueOK}, {Key: fwKeyPolicy, Value: policy, State: component.ValueOK},
		{Key: fwKeyBoot, Value: "Yes", State: component.ValueOK}}
}

// change runs one plain change of action through the flow.
func (e *fwRunner) change(ctx context.Context, r fwReading, action firewall.Action, title, question string, review flow.Review) error {
	return e.kit(r).Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: title}},
		Flow: flow.Flow{Review: func() flow.Review { return review }, Confirm: fwConfirm(question)},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{firewall.Incoming{Action: action}}
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
	apply := func() error {
		if msgs := r.applyRefusal(ctx, e.h.system()); msgs != nil {
			return e.kit(r).Say(msgs)()
		}
		return e.change(ctx, r, firewall.ActionApplyProfiles, "Apply Profiles", "Apply the profiles?", flow.Review{Title: fwSectionProfiles, Items: r.profileRows()})
	}
	return vm.Screen(e.s.p.Ctx, fwBind(vm, map[string]func() error{
		"1": func() error { return e.addProfile(ctx, r) },
		"2": func() error { return e.removeProfile(ctx, r) },
		"3": apply,
	}))
}

// profilesMenu is the sub-screen's layout.
func (r fwReading) profilesMenu() screen.SubMenu {
	vm := screen.SubMenu{Title: component.TitleBar{Title: fwProfilesTitle}, Sections: []screen.Section{fwSection(fwSectionProfiles, r.profileRows())},
		Heading: fwHeading, Prompt: fwPrompt, Menu: component.Menu{Exit: fwExit, Items: []component.MenuItem{{Key: "1", Label: fwAddLabel, Description: fwAddAbout}}}}
	if len(r.Profiles) > 0 {
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
				return nav.Refuse("Use letters, digits, dots, dashes and underscores.")
			case slices.ContainsFunc(r.Profiles, func(p config.UfwAppProfile) bool { return p.Name == s }):
				return nav.Warn("A profile with this name already exists.")
			}
			return nil
		}},
		&form.Text{Title: "Title", Value: &title, Validate: fwRequired("Give the profile a title.")},
		&form.Text{Title: "Description", Value: &about, Validate: fwRequired("Give the profile a description.")},
		&form.Text{Title: "Ports", Description: "Such as 80/tcp,443/tcp.", Value: &ports, Validate: func(s string) error {
			for _, p := range strings.Split(s, ",") {
				if !fwPortOK(strings.TrimSpace(p)) {
					return nav.Refuse("Give each port, 1 to 65535, with its protocol, such as 80/tcp.")
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
			return []engine.Control{firewall.Incoming{Action: firewall.ActionSaveProfiles}}
		},
	})
}

// fwRequired refuses an empty answer with words.
func fwRequired(words string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return nav.Refuse(words)
		}
		return nil
	}
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
	opts := make([]form.Option[int], len(r.Profiles))
	for i, p := range r.Profiles {
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
				return flow.Confirm{Question: "Remove " + r.Profiles[gone].Name + "?"}
			},
		},
		Controls: func(edited *config.Config) []engine.Control {
			edited.UfwAppProfiles = slices.Delete(slices.Clone(edited.UfwAppProfiles), gone, gone+1)
			return []engine.Control{firewall.Incoming{Action: firewall.ActionSaveProfiles}}
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
// profiles' sub-screen, with two and with none; the refusals; the Proxmox VE
// states the hosts leave out; and what PlanUFWEnable does on the unreloaded
// edit, with an SSH session and from a console.
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
		listMock{name: "refusals", note: "The refusals before asking, each the control's: Install finding no package or changing others, no ss, ss failing, sshd's configuration not in effect, ufw's state not read, ENABLED and the chains disagreeing, a saved blocklist, a saved deny, IPv6 off in ufw, a one-family allowlist, an active deny, Proxmox VE not established; then Apply on an inactive ufw.",
			draw: fwRefusalsMock},
		listMock{name: "pve-states", note: "Proxmox VE's screen where it says less than the two hosts: pve-firewall stopped, sshd on another port, a blacklist, and a node where origin/main left ufw active, which nothing here shows (decision 7).",
			draw: fwPVEStatesMock},
		listMock{name: "stale-listener", note: "The edit-2230 host: Port 2230 configured, not reloaded, sshd accepting on 22. The screen refuses (decision 2 (a)). What the real steps.PlanUFWEnable plans without that refusal, with an SSH session on 22, and from a console, no session.",
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
		{"apt finds no ufw", fwNoUFWSim(debian13Platform, fwAPTNotFound)},
		{"apk finds no ufw", fwAlpineStock},
		{"other packages change", fwNoUFWSim(debian13Platform, fwAPTRemoves)},
		{"no ss", fwNoSS},
		{"ss failing", fwSSFailing},
		{"sshd's configuration not in effect", fwEdit},
		{"bound elsewhere, inactive", fwMovedInactive},
		{"ufw's state not read", fwGarbled},
		{"lockout-at-start", func() *fakeHost { return ufwOn(lockoutAtStart().withHardnYML(fwYML)) }},
		{"blocklist", fwWith(ufwSim{incoming: "deny", rules: []simRule{{action: "deny", port: "22", src: "198.51.100.0/24"}, {action: "allow", port: "22"}}})},
		{"a saved deny", fwSavedDeny},
		{"IPv6 off in ufw", fwWith(ufwSim{incoming: "deny", noIPv6: true})},
		{"IPv6 off in ufw, active", fwWith(ufwSim{incoming: "deny", active: true, enabled: true, noIPv6: true, rules: []simRule{{action: "allow", port: "22"}}})},
		{"an allowlist of one family", fwWith(ufwSim{incoming: "deny", active: true, enabled: true, rules: []simRule{{action: "allow", port: "22", src: "192.0.2.0/24"}}})},
		{"an active deny", fwWith(ufwSim{incoming: "deny", active: true, enabled: true, rules: []simRule{{action: "deny", port: "22"}}})},
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
	h := fwProfilesInactive()
	r, err := (&session{}).readFW(ctx, h.system(), fwSession(h))
	if err != nil {
		return "", err
	}
	out = append(out, header(c, "Apply, ufw inactive"), r.applyRefusal(ctx, h.system()).Render(c))
	return apart(out...), nil
}

// fwPVEStates are the Proxmox VE states pve-states draws.
func fwPVEStates() []struct {
	name string
	host func() *fakeHost
} {
	return []struct {
		name string
		host func() *fakeHost
	}{
		{"pve-firewall stopped", fwPVEStopped},
		{"sshd on 2230", fwPVEMoved},
		{"a blacklist", fwPVEBlacklist},
		{"ufw left active by origin/main", fwPVEUFW},
	}
}

// fwPVEStatesMock draws the screen on each.
func fwPVEStatesMock(ctx context.Context, c component.Ctx) (string, error) {
	var out []string
	for _, st := range fwPVEStates() {
		h := st.host()
		r, err := (&session{}).readFW(ctx, h.system(), fwSession(h))
		if err != nil {
			return "", fmt.Errorf("%s: %w", st.name, err)
		}
		out = append(out, header(c, st.name), r.subMenu().Render(c))
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
		changes, err := steps.PlanUFWEnable(ctx, h.system(), engine.Input{Facts: f, Session: v.session}, firewall.ID)
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
	"(firewall.Incoming.Check, firewall.Read), on the real plan/apply engine. " +
	"Install, Disable, the runlevel and the profiles' plans are firewall.Incoming's. Nothing on this machine changes: every command and file is the fake's, " +
	"and its record is shown when play ends."
