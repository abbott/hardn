package ufw

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host/detect"
)

// ProfileFile is the file hardn writes the application profiles to.
const ProfileFile = "/etc/ufw/applications.d/hardn"

// Profile is one application profile, as ufw reads it from
// /etc/ufw/applications.d.
type Profile struct {
	Name, Title, Description string
	Ports                    []string // "80/tcp", "6000:6007/udp"
	// Interface limits the profile's rule to what arrives on one network
	// interface ("ufw allow in on tailscale0 ... app <name>"); "" is every
	// interface. ufw's profile file has no such field: it is the rule's.
	Interface string
}

var (
	// profileName is ufw 0.36.2's valid_profile_name.
	profileName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 _\-.+]*$`)
	// pythonInt is a name Python's int() takes, which valid_profile_name
	// refuses as a port: digits, single underscores between them, spaces
	// around them.
	pythonInt   = regexp.MustCompile(`^[0-9]+(_[0-9]+)*$`)
	profilePort = regexp.MustCompile(`^([0-9]{1,5})(?::([0-9]{1,5}))?/(tcp|udp)$`)
	// interfaceName is the characters ufw 0.36.2 takes in an interface's
	// name (common.py, UFWRule.set_interface).
	interfaceName = regexp.MustCompile(`^[a-zA-Z0-9_\-.+,=%@]+$`)
)

// ValidInterface says whether ufw takes name as an interface, by ufw
// 0.36.2's UFWRule.set_interface (common.py, read 2026-11-07): no "!",
// which it keeps for negation, no ":", an alias such as eth0:1, not "."
// or "..", 1 to 15 characters, each a letter, a digit or one of _-.+,=%@;
// and not a word its rule parser reads as a clause (parser.py), so that
// "ufw allow in on <name>" parses: in, out, from, to, port, proto, app,
// log, log-all, comment.
func ValidInterface(name string) bool {
	return name != "." && name != ".." && len(name) >= 1 && len(name) <= 15 && interfaceName.MatchString(name) && !slices.Contains(ruleWords, name)
}

// ruleWords are the words ufw 0.36.2's rule parser takes as clauses, which
// an interface named so would be read as.
var ruleWords = []string{"in", "out", "from", "to", "port", "proto", "app", "log", "log-all", "comment"}

// ValidProfile says whether ufw reads p, by ufw 0.36.2's own rules
// (applications.py, valid_profile_name and verify_profile; docs/safety/ufw.md
// §3): a name of its characters, not "all" and not an integer; a title and a
// description not empty; no line break in any field, which would write another
// key; and each port a number or an ascending range from 1 to 65535 with its
// protocol, the form the Firewall screen's Add takes. ufw splits ports on "|"
// (applications.py:187), so a comma is not read here as it is in origin/main's
// file. Errors Is(ErrBadProfile).
func ValidProfile(p Profile) error {
	bad := func(why string) error { return fmt.Errorf("ufw: profile %q: %s: %w", p.Name, why, ErrBadProfile) }
	switch {
	case !validProfileName(p.Name):
		return bad("its name")
	case strings.TrimSpace(p.Title) == "":
		return bad("no title")
	case strings.TrimSpace(p.Description) == "":
		return bad("no description")
	case strings.ContainsAny(p.Title+p.Description, "\r\n"):
		return bad("a line break")
	case len(p.Ports) == 0:
		return bad("no port")
	case p.Interface != "" && !ValidInterface(p.Interface):
		return bad(fmt.Sprintf("interface %q", p.Interface))
	}
	for _, port := range p.Ports {
		if !validProfilePort(port) {
			return bad(fmt.Sprintf("port %q", port))
		}
	}
	return nil
}

func validProfileName(name string) bool {
	return name != "all" && profileName.MatchString(name) && !pythonInt.MatchString(strings.TrimSpace(name))
}

func validProfilePort(s string) bool {
	m := profilePort.FindStringSubmatch(s)
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

// ProfileBytes is ProfileFile's content for profiles, each as ufw's own
// profile files are written (openssh-server's), its ports joined by "|".
func ProfileBytes(profiles []Profile) []byte {
	var b strings.Builder
	for _, p := range profiles {
		fmt.Fprintf(&b, "[%s]\ntitle=%s\ndescription=%s\nports=%s\n\n", p.Name, p.Title, p.Description, strings.Join(p.Ports, "|"))
	}
	return []byte(b.String())
}

// PlanApps plans, with no I/O, "ufw allow from any to any app <name>" for each
// profile whose rule no saved rule is (AppApplied), in the order given, or
// "ufw allow in on <interface> from any to any app <name>" for a profile
// limited to an interface, undone by the same words after "delete" (ufw
// deletes an interface's rule only so) where ufw's answer shows it added
// the rule (Execute). Rules already there are never repeated or changed: a
// rule for the profile on another interface, or on every interface, stays.
// No rule is saved on an inactive ufw (decision 5, ErrInactive). Errors:
// ErrProxmoxUnknown, ErrNotInstalled, ErrStateUnknown, ErrInactive,
// ErrEnabledMismatch, ErrBadProfile for a name or interface ufw would not
// read.
func PlanApps(st State, profiles []Profile, pve detect.Proxmox) (Plan, error) {
	if err := Applies(pve); errors.Is(err, ErrNotApplicable) {
		return Plan{NotApplicable: true}, nil
	} else if err != nil {
		return Plan{}, err
	}
	switch {
	case !st.Installed:
		return Plan{}, fmt.Errorf("ufw: %w", ErrNotInstalled)
	case !st.Status.Known:
		return Plan{}, fmt.Errorf("ufw: %w (unread lines %q)", ErrStateUnknown, st.Status.Bad)
	case !st.Status.Active:
		return Plan{}, fmt.Errorf("ufw: %w", ErrInactive)
	case !st.Known():
		return Plan{}, fmt.Errorf("ufw: %w (unread lines %q)", ErrStateUnknown, st.Bad)
	case !st.Enabled:
		return Plan{}, fmt.Errorf("ufw: %w: ENABLED=no (%s), status active", ErrEnabledMismatch, ConfFile)
	}
	p := Plan{Active: true, Incoming: st.Status.Incoming, Outgoing: st.Status.Outgoing, IPv6: st.IPv6, NoKernelIPv6: st.NoKernelIPv6}
	var planned []string
	for _, pr := range profiles {
		name, iface := pr.Name, pr.Interface
		switch {
		case !validProfileName(name):
			return Plan{}, fmt.Errorf("ufw: profile %q: its name: %w", name, ErrBadProfile)
		case iface != "" && !ValidInterface(iface):
			return Plan{}, fmt.Errorf("ufw: profile %q: interface %q: %w", name, iface, ErrBadProfile)
		}
		key := name + "\x00" + iface
		if slices.Contains(planned, key) || AppApplied(st, name, iface) {
			continue
		}
		planned = append(planned, key)
		rule, why := []string{"allow", "from", "any", "to", "any", "app", name}, "let application "+name+" through"
		if iface != "" {
			rule, why = append([]string{"allow", "in", "on", iface}, rule[1:]...), why+" on "+iface
		}
		p.Steps = append(p.Steps, Step{Kind: StepAllowApp, Argv: append([]string{"ufw"}, rule...),
			Undo: append([]string{"ufw", "delete"}, rule...), Why: why})
	}
	return p, nil
}

// AppApplied: st's saved rules hold the profile's own rule: allow, in, from
// any source to any address, application name, on interface iface (""
// every interface), in IPv4 and, with ufw's IPv6 on, in IPv6 too, as "ufw
// allow" saves it (inFamilies). A rule of the application otherwise (a
// deny, out, from some sources, on another interface or every one, or in
// one family alone) is not. A saved rule's name has its spaces as "%20", so
// that the tuple's fields stay split (ufw 0.36.2's backend_iptables.py,
// read 2026-11-07).
func AppApplied(st State, name, iface string) bool {
	return inFamilies(st, func(r Rule) bool {
		return (r.App == name || r.App == strings.ReplaceAll(name, " ", "%20")) &&
			r.Action == "allow" && r.Dir == "in" && r.Src == "" && r.Dst == "" && r.Iface == iface
	})
}

// inFamilies: a saved rule of st matches in IPv4, and, where ufw's IPv6 is
// on (State.IPv6), one in IPv6 too: a rule saved in one family alone (from
// an address of one, or while IPV6 was no) leaves the other refused.
func inFamilies(st State, match func(Rule) bool) bool {
	has := func(f Family) bool {
		return slices.ContainsFunc(st.Rules, func(r Rule) bool { return r.Family == f && match(r) })
	}
	return has(IPv4) && (!st.IPv6 || has(IPv6))
}

// PlanInterfaces plans, with no I/O, "ufw allow in on <interface>" for each
// trusted interface whose rule no saved rule is (InterfaceAllowed), in the
// order given: everything arriving on it gets through, every port and
// protocol (the maintainer, 2026-11-07). Each is undone by "ufw delete allow
// in on <interface>" where ufw's answer shows it added the rule (Execute).
// Rules already there are never repeated or changed. No rule is saved on an
// inactive ufw (ErrInactive), as for PlanApps. Errors: ErrProxmoxUnknown,
// ErrNotInstalled, ErrStateUnknown, ErrInactive, ErrEnabledMismatch,
// ErrBadInterface for a name ufw would not take.
func PlanInterfaces(st State, ifaces []string, pve detect.Proxmox) (Plan, error) {
	if err := Applies(pve); errors.Is(err, ErrNotApplicable) {
		return Plan{NotApplicable: true}, nil
	} else if err != nil {
		return Plan{}, err
	}
	switch {
	case !st.Installed:
		return Plan{}, fmt.Errorf("ufw: %w", ErrNotInstalled)
	case !st.Status.Known:
		return Plan{}, fmt.Errorf("ufw: %w (unread lines %q)", ErrStateUnknown, st.Status.Bad)
	case !st.Status.Active:
		return Plan{}, fmt.Errorf("ufw: %w", ErrInactive)
	case !st.Known():
		return Plan{}, fmt.Errorf("ufw: %w (unread lines %q)", ErrStateUnknown, st.Bad)
	case !st.Enabled:
		return Plan{}, fmt.Errorf("ufw: %w: ENABLED=no (%s), status active", ErrEnabledMismatch, ConfFile)
	}
	p := Plan{Active: true, Incoming: st.Status.Incoming, Outgoing: st.Status.Outgoing, IPv6: st.IPv6, NoKernelIPv6: st.NoKernelIPv6}
	var planned []string
	for _, iface := range ifaces {
		if !ValidInterface(iface) {
			return Plan{}, fmt.Errorf("ufw: interface %q: %w", iface, ErrBadInterface)
		}
		if slices.Contains(planned, iface) || InterfaceAllowed(st, iface) {
			continue
		}
		planned = append(planned, iface)
		rule := []string{"allow", "in", "on", iface}
		p.Steps = append(p.Steps, Step{Kind: StepAllowIface, Argv: append([]string{"ufw"}, rule...),
			Undo: append([]string{"ufw", "delete"}, rule...), Why: "let everything on " + iface + " through"})
	}
	return p, nil
}

// InterfaceAllowed: st's saved rules let everything arriving on iface
// through: allow, in, on iface, any protocol, from anywhere, to any address
// and port, no application, as "ufw allow in on <iface>" saves it, in each
// family it saves it in (inFamilies).
func InterfaceAllowed(st State, iface string) bool {
	return inFamilies(st, func(r Rule) bool {
		return r.Action == "allow" && r.Dir == "in" && r.Iface == iface && r.Proto == "any" && r.DPort == "" && r.Src == "" && r.Dst == "" && r.App == ""
	})
}
