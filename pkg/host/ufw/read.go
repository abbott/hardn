package ufw

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"regexp"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Status is "ufw status verbose" read. ufw prints the rules of its saved
// lists (the same ones ParseTuples reads) and whether its chains are loaded.
type Status struct {
	Known  bool // the status line was read, and while active every line
	Active bool // "Status: active": ufw's chains are loaded
	// While active: the Logging, Default and New profiles lines' words.
	Logging                    string
	Incoming, Outgoing, Routed Policy
	NewProfiles                string
	Rules                      []Rule   // IPv4's, then IPv6's, each in its order
	Bad                        []string // lines not read
}

var (
	defaultLine = regexp.MustCompile(`^Default: (\w+) \(incoming\), (\w+) \(outgoing\), (\w+) \(routed\)$`)
	// ruleLine splits a rule at its action: the To column, then
	// "%-12s" of the action and direction, then From and the annotations.
	ruleLine = regexp.MustCompile(`^(\S.*?)\s+(ALLOW|DENY|REJECT|LIMIT) (IN|OUT|FWD)\s+(\S.*)$`)
	policies = map[string]Policy{"allow": PolicyAllow, "deny": PolicyDeny, "reject": PolicyReject, "disabled": PolicyDisabled}
)

// ParseStatus reads "ufw status verbose". Inactive, ufw prints the status
// line alone; active, it prints Logging, Default and New profiles, then a
// table whose rows are "%-26s %-12s%-26s" of To, the action and direction,
// and From, followed by a log type and a "# comment".
func ParseStatus(out []byte) Status {
	var st Status
	table := false
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "" || table && strings.Trim(t, "- ") == "":
		case !st.Known && t == "Status: inactive":
			st.Known = true
		case !st.Known && t == "Status: active":
			st.Known, st.Active = true, true
		case !st.Known:
			st.Bad = append(st.Bad, line)
		case st.Active && strings.HasPrefix(t, "Logging: "):
			st.Logging = strings.TrimPrefix(t, "Logging: ")
		case st.Active && strings.HasPrefix(t, "New profiles: "):
			st.NewProfiles = strings.TrimPrefix(t, "New profiles: ")
		case st.Active && strings.HasPrefix(t, "Default: "):
			m := defaultLine.FindStringSubmatch(t)
			if m == nil || policies[m[1]] == "" || policies[m[2]] == "" || policies[m[3]] == "" {
				st.Bad = append(st.Bad, line)
				break
			}
			st.Incoming, st.Outgoing, st.Routed = policies[m[1]], policies[m[2]], policies[m[3]]
		case st.Active && !table && strings.Fields(t)[0] == "To":
			table = true
		case table:
			if r, ok := statusRule(t); ok {
				st.Rules = append(st.Rules, r)
			} else {
				st.Bad = append(st.Bad, line)
			}
		default:
			st.Bad = append(st.Bad, line)
		}
	}
	if st.Active && (st.Logging == "" || st.Incoming == "") {
		st.Known = false
	}
	return st
}

// statusRule reads one row of the table.
func statusRule(line string) (Rule, bool) {
	m := ruleLine.FindStringSubmatch(line)
	if m == nil {
		return Rule{}, false
	}
	r := Rule{Action: strings.ToLower(m[2]), Dir: strings.ToLower(m[3]), Line: line}
	dst, port, proto, iface, v6, ok := statusTo(m[1])
	if !ok {
		return Rule{}, false
	}
	r.Dst, r.DPort, r.Proto, r.Iface, r.Family = dst, port, proto, iface, IPv4
	// From is padded to 26 columns, then come the log type and the
	// comment.
	from, _, _ := strings.Cut(m[4], " # ")
	from = strings.TrimSpace(from)
	for _, a := range []string{"(log)", "(log-all)"} {
		from = strings.TrimSpace(strings.TrimSuffix(from, a))
	}
	from, src6 := strings.CutSuffix(from, " (v6)")
	if from != "Anywhere" && !strings.HasPrefix(from, "Anywhere/") {
		r.Src = from
		if a, _, _ := strings.Cut(from, " "); isAddr(a) && strings.Contains(a, ":") {
			src6 = true
		}
	}
	if v6 || src6 || strings.Contains(r.Dst, ":") {
		r.Family = IPv6
	}
	return r, true
}

// statusTo reads the To column: [address] [ports[/proto]] [(application)]
// [(v6)] [on interface], or Anywhere[/proto].
func statusTo(s string) (dst, port, proto, iface string, v6, ok bool) {
	if i := strings.LastIndex(s, " on "); i >= 0 && !strings.ContainsAny(s[i+4:], " ()") {
		s, iface = s[:i], s[i+4:]
	}
	if i := strings.Index(s, " ("); i >= 0 {
		tail := s[i:]
		s = s[:i]
		v6 = strings.HasSuffix(tail, "(v6)") || strings.HasSuffix(tail, "(v6))")
		if !strings.HasSuffix(tail, ")") {
			return "", "", "", "", false, false
		}
	}
	f := strings.Fields(s)
	if len(f) == 0 || len(f) > 2 {
		return "", "", "", "", false, false
	}
	proto = "any"
	if a, p, ok := strings.Cut(f[0], "/"); a == "Anywhere" {
		if ok {
			proto = p
		}
		return "", "", proto, iface, v6, len(f) == 1
	}
	if a, p := splitProto(f[0]); isAddr(a) {
		dst, proto = a, p
		f = f[1:]
	}
	if len(f) == 1 {
		port, proto = splitProto(f[0])
		if strings.Trim(port, "0123456789,:") != "" || port == "" {
			return "", "", "", "", false, false
		}
	}
	return dst, port, proto, iface, v6, true
}

// splitProto splits "x/tcp" into "x" and "tcp"; a suffix that is not a
// name (a prefix length) stays.
func splitProto(s string) (string, string) {
	i := strings.LastIndex(s, "/")
	if i < 0 || strings.Trim(s[i+1:], "abcdefghijklmnopqrstuvwxyz") != "" || i == len(s)-1 {
		return s, "any"
	}
	return s[:i], s[i+1:]
}

func isAddr(s string) bool {
	if _, err := netip.ParsePrefix(s); err == nil {
		return true
	}
	_, err := netip.ParseAddr(s)
	return err == nil
}

// ParseTuples reads the "### tuple ###" lines of a saved rule file, the
// form ufw keeps its rules in: action, protocol, destination port and
// address, source port and address, [destination and source application,]
// then the direction with an optional "_<interface>", and an optional
// " comment=". An action may carry "_<logtype>" and, for a routed rule,
// "route:".
func ParseTuples(data []byte, fam Family) (rules []Rule, bad []string) {
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(line, "### tuple ###")
		if !ok {
			continue
		}
		rest, _, _ = strings.Cut(rest, " comment=")
		f := strings.Fields(rest)
		if len(f) != 7 && len(f) != 9 {
			bad = append(bad, line)
			continue
		}
		action, route := strings.CutPrefix(f[0], "route:")
		action, _, _ = strings.Cut(action, "_")
		dir, iface, _ := strings.Cut(f[len(f)-1], "_")
		if route {
			dir = "fwd"
		}
		if !validAction(action) || dir != "in" && dir != "out" && dir != "fwd" {
			bad = append(bad, line)
			continue
		}
		var src []string
		if a := anyAddr(f[5]); a != "" {
			src = append(src, a)
		}
		if f[4] != "any" {
			src = append(src, "port "+f[4])
		}
		if len(f) == 9 && f[7] != "-" {
			src = append(src, "app "+f[7])
		}
		rules = append(rules, Rule{
			Action: action, Dir: dir, Proto: f[1], Dst: anyAddr(f[3]), DPort: anyPort(f[2]),
			Src: strings.Join(src, " "), Iface: iface, Family: fam, Line: line,
		})
	}
	return rules, bad
}

func validAction(a string) bool {
	return a == "allow" || a == "deny" || a == "reject" || a == "limit"
}

func anyAddr(a string) string {
	if a == "0.0.0.0/0" || a == "::/0" {
		return ""
	}
	return a
}

func anyPort(p string) string {
	if p == "any" {
		return ""
	}
	return p
}

// readDefaults reads /etc/default/ufw as ufw does: lines starting
// `\w+="?\w+"?`, split at "=", key and value lower-cased, quotes stripped;
// a later line wins.
func readDefaults(data []byte) map[string]string {
	pat := regexp.MustCompile(`^\w+="?\w+"?`)
	d := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if !pat.MatchString(line) {
			continue
		}
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		v, _, _ = strings.Cut(v, "=")
		d[strings.ToLower(k)] = strings.Trim(strings.ToLower(v), `"'`)
	}
	return d
}

// defaultPolicy maps a DEFAULT_*_POLICY value as ufw does: accept is allow,
// reject is reject, anything else deny; a missing key is not established.
func defaultPolicy(v string, ok bool) Policy {
	switch {
	case !ok:
		return ""
	case v == "accept":
		return PolicyAllow
	case v == "reject":
		return PolicyReject
	}
	return PolicyDeny
}

// State is what Read established.
type State struct {
	Installed bool   // "ufw status verbose" ran
	Status    Status // its answer: what is in force
	// IPv6: IPV6=yes in /etc/default/ufw and the kernel has IPv6; without
	// it ufw adds IPv4 rules only and, once started, drops all IPv6.
	IPv6               bool
	Incoming, Outgoing Policy // DEFAULT_INPUT_POLICY, DEFAULT_OUTPUT_POLICY
	// Rules are the saved rules, IPv4's then IPv6's (IPv6's only with
	// IPv6): what ufw loads when it starts, in force only while active.
	Rules []Rule
	Bad   []string // lines of the status or the rule files not read
}

// Known: ufw is installed and its status, defaults and rules were all read.
func (s State) Known() bool {
	return s.Installed && s.Status.Known && len(s.Bad) == 0 && s.Incoming != "" && s.Outgoing != ""
}

// Read establishes UFW's state. On Proxmox VE it runs nothing and returns
// ErrNotApplicable (ErrProxmoxUnknown when that is not established). ufw
// missing is State{} with no error. It runs "ufw status verbose" and reads
// DefaultsFile, KernelIPv6, UserRules and, with IPv6, User6Rules, changing
// nothing; an I/O failure is an error, content it cannot read is in Bad.
func Read(ctx context.Context, sys host.System, pve detect.Proxmox) (State, error) {
	var st State
	if err := Applies(pve); err != nil {
		return st, err
	}
	res, err := sys.Run(ctx, "ufw", "status", "verbose")
	if errors.Is(err, host.ErrNotFound) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("ufw: %w", err)
	}
	st.Installed = true
	st.Status = ParseStatus(res.Stdout)
	st.Bad = append(st.Bad, st.Status.Bad...)
	data, err := sys.ReadFile(ctx, DefaultsFile)
	if err != nil {
		return st, fmt.Errorf("ufw: %w", err)
	}
	d := readDefaults(data)
	in, inOK := d["default_input_policy"]
	out, outOK := d["default_output_policy"]
	st.Incoming, st.Outgoing = defaultPolicy(in, inOK), defaultPolicy(out, outOK)
	_, err = sys.Stat(ctx, KernelIPv6)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return st, fmt.Errorf("ufw: %w", err)
	}
	st.IPv6 = d["ipv6"] == "yes" && err == nil
	files := []struct {
		path string
		fam  Family
	}{{UserRules, IPv4}, {User6Rules, IPv6}}
	if !st.IPv6 {
		files = files[:1]
	}
	for _, f := range files {
		data, err := sys.ReadFile(ctx, f.path)
		if err != nil {
			return st, fmt.Errorf("ufw: %w", err)
		}
		rules, bad := ParseTuples(data, f.fam)
		st.Rules = append(st.Rules, rules...)
		st.Bad = append(st.Bad, bad...)
	}
	return st, nil
}
