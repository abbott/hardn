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
}

var (
	// profileName is ufw 0.36.2's valid_profile_name.
	profileName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 _\-.+]*$`)
	// pythonInt is a name Python's int() takes, which valid_profile_name
	// refuses as a port: digits, single underscores between them, spaces
	// around them.
	pythonInt   = regexp.MustCompile(`^[0-9]+(_[0-9]+)*$`)
	profilePort = regexp.MustCompile(`^([0-9]{1,5})(?::([0-9]{1,5}))?/(tcp|udp)$`)
)

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
// name no saved rule has as its application, in the order given, undone by
// "ufw delete allow from any to any app <name>" where ufw's answer shows it
// added the rule (Execute). Rules already there are never repeated or changed.
// No rule is saved on an inactive ufw (decision 5, ErrInactive). Errors:
// ErrProxmoxUnknown, ErrNotInstalled, ErrStateUnknown, ErrInactive,
// ErrEnabledMismatch, ErrBadProfile for a name ufw would not read.
func PlanApps(st State, names []string, pve detect.Proxmox) (Plan, error) {
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
	for _, name := range names {
		if !validProfileName(name) {
			return Plan{}, fmt.Errorf("ufw: profile %q: its name: %w", name, ErrBadProfile)
		}
		if slices.Contains(planned, name) || appRule(st.Rules, name) {
			continue
		}
		planned = append(planned, name)
		p.Steps = append(p.Steps, Step{Kind: StepAllowApp, Argv: []string{"ufw", "allow", "from", "any", "to", "any", "app", name},
			Undo: []string{"ufw", "delete", "allow", "from", "any", "to", "any", "app", name}, Why: "let application " + name + " through"})
	}
	return p, nil
}

// appRule: a saved rule has application name. A saved rule's name has its
// spaces as "%20", so that the tuple's fields stay split ([I], from memory of
// ufw's backend, not run; ParseTuples splits on spaces).
func appRule(rules []Rule, name string) bool {
	return slices.ContainsFunc(rules, func(r Rule) bool { return r.App == name || r.App == strings.ReplaceAll(name, " ", "%20") })
}
