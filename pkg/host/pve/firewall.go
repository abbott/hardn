package pve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// pve-firewall's files (on pmxcfs) and services. HostFW is read through
// pmxcfs's own link local -> nodes/<node>, the node's directory.
const (
	ClusterFW    = "/etc/pve/firewall/cluster.fw"
	HostFW       = "/etc/pve/local/host.fw"
	FirewallUnit = "pve-firewall.service"
	// NFTablesUnit applies the rules instead when host.fw sets nftables: 1
	// (proxmox-firewall, a tech preview in Proxmox VE 9).
	NFTablesUnit = "proxmox-firewall.service"
)

// ErrStateUnknown wraps the reasons a Firewall field is Unknown.
var ErrStateUnknown = errors.New("pve-firewall's state is not established")

// Reach is how far pve-firewall lets new connections to a TCP port of the
// host through.
type Reach uint8

const (
	ReachUnknown Reach = iota
	// ReachNotFiltered: pve-firewall is off for this host and its service
	// is running, so it filters no port (other packet filters are not
	// read).
	ReachNotFiltered
	// ReachAnywhere: the input policy is ACCEPT and no rule may drop it.
	ReachAnywhere
	// ReachManagement: the input policy drops what no rule accepts, and
	// pve-firewall's built-in rule accepts the port from the management IP
	// set (its entries and the cluster's local network); an accept rule
	// may let more sources in.
	ReachManagement

	reachEnd // one past the last value; keep it last
)

// reachCodes are Reach's stable codes; the words a status shows are
// pkg/ui's.
var reachCodes = [...]string{"unknown", "not-filtered", "anywhere", "management"}

// String is the stable code (TestReachCodes pins them).
func (r Reach) String() string {
	if int(r) >= len(reachCodes) {
		return "unknown"
	}
	return reachCodes[r]
}

// Firewall is pve-firewall's state as its files and its service say.
type Firewall struct {
	// Enabled: pve-firewall filters this host: cluster.fw's enable is not 0
	// (it is 0 when unset) and host.fw's enable is not 0 (1 when unset).
	Enabled detect.Tri
	// PolicyIn is cluster.fw's policy_in: ACCEPT, DROP or REJECT, DROP when
	// unset; "" when not established.
	PolicyIn string
	// NFTables: host.fw's nftables is 1, so proxmox-firewall applies the
	// rules (No when unset).
	NFTables detect.Tri
	// Unit is the service that applies the rules, and Service its state
	// (systemctl is-active; Enabled is not read).
	Unit    string
	Service detect.UnitState
	// Management lists cluster.fw's IP set management, as written;
	// pve-firewall adds the cluster's local network to it itself.
	Management []string
	SSH, Web   Reach // ports SSHPort and WebPort
	Findings   []Finding
	// Err is set whenever a field is Unknown, and says why; it wraps
	// ErrStateUnknown.
	Err error
}

// ReadFirewall reads pve-firewall's state on a Proxmox VE host: cluster.fw,
// host.fw and "systemctl is-active" of the unit that applies them. It
// changes nothing, runs nothing else (not "pve-firewall status", which takes
// the firewall's lock), and never touches UFW or the packet filter. Off
// Proxmox VE it returns ErrNotApplicable or ErrProxmoxUnknown; with pmxcfs
// not mounted every field is Unknown, as the files are not there. The only
// other error is a done ctx: what cannot be read, or is not in the form
// pve-firewall reads, is Unknown, with Err.
func ReadFirewall(ctx context.Context, sys host.System, p detect.Proxmox) (Firewall, error) {
	if err := ctx.Err(); err != nil {
		return Firewall{}, fmt.Errorf("pve: %w", err)
	}
	switch ok, err := Applies(p); {
	case err != nil:
		return Firewall{}, err
	case !ok:
		return Firewall{}, fmt.Errorf("pve: pve-firewall: %w", ErrNotApplicable)
	case p.ClusterFS != detect.Yes:
		return Firewall{Err: fmt.Errorf("pve: %w: %w", ErrStateUnknown, ErrClusterFSDown)}, nil
	}
	var errs []error
	read := func(name string, kind fwKind) *fwConf {
		data, err := sys.ReadFile(ctx, name)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			data = nil // a missing file is its defaults
		case err != nil:
			errs = append(errs, err)
			return nil
		}
		return parseFW(name, kind, data)
	}
	cluster, hostfw := read(ClusterFW, clusterFile), read(HostFW, hostFile)
	f := Firewall{Unit: FirewallUnit}
	var clusterOn, hostOn detect.Tri
	if cluster != nil {
		clusterOn = option(cluster, "enable", "0", clusterEnable, &errs)
		if !cluster.skip("policy_in", &errs) {
			f.PolicyIn = strings.ToUpper(cluster.get("policy_in", "DROP"))
		}
		f.Management = cluster.ipsets["management"]
		if bl := cluster.ipsets["blacklist"]; len(bl) > 0 {
			f.Findings = append(f.Findings, Finding{Kind: FindingBlacklist, File: ClusterFW,
				Detail: fmt.Sprintf("the blacklist IP set has %d entries: every host's firewall drops traffic from them", len(bl))})
		}
	}
	if hostfw != nil {
		hostOn = option(hostfw, "enable", "1", boolean, &errs)
		f.NFTables = option(hostfw, "nftables", "0", boolean, &errs)
	}
	switch {
	case clusterOn == detect.No || hostOn == detect.No:
		f.Enabled = detect.No
	case clusterOn == detect.Yes && hostOn == detect.Yes:
		f.Enabled = detect.Yes
	}
	if f.NFTables == detect.Yes {
		f.Unit = NFTablesUnit
	}
	f.Service.Active, errs = isActive(ctx, sys, f.Unit, errs)
	if err := ctx.Err(); err != nil {
		return Firewall{}, fmt.Errorf("pve: %w", err)
	}
	f.SSH = f.reach(SSHPort, cluster, hostfw, &errs)
	f.Web = f.reach(WebPort, cluster, hostfw, &errs)
	if err := errors.Join(errs...); err != nil {
		f.Err = fmt.Errorf("pve: %w: %w", ErrStateUnknown, err)
	}
	return f, nil
}

// reach decides port's Reach. Only a running service has applied the files
// (pve-firewall rereads them every 10 seconds), so a stopped one leaves
// whatever rules it loaded before, which are not read. pve-firewall puts the
// blacklist's drop, then the host's rules, then the cluster's, before its
// built-in management rules (Firewall.pm, enable_host_firewall and
// ruleset_chain_add_input_filters), so a non-empty blacklist, or any rule
// that may drop the port, makes it Unknown. Every Unknown adds to errs.
func (f *Firewall) reach(port int, cluster, hostfw *fwConf, errs *[]error) Reach {
	switch {
	case f.Enabled == detect.Unknown:
		*errs = append(*errs, fmt.Errorf("port %d: whether pve-firewall filters the host is not established", port))
		return ReachUnknown
	case f.Service.IsActive() != detect.Yes:
		*errs = append(*errs, fmt.Errorf("port %d: %s is %q, so which rules are loaded is not established", port, f.Unit, f.Service.Active))
		return ReachUnknown
	case f.Enabled == detect.No:
		return ReachNotFiltered
	case f.NFTables == detect.Yes:
		if port == SSHPort {
			f.Findings = append(f.Findings, Finding{Kind: FindingNotModelled, File: HostFW, Detail: "nftables: 1: proxmox-firewall (tech preview) applies the rules; hardn does not read them"})
		}
		*errs = append(*errs, fmt.Errorf("port %d: proxmox-firewall applies the rules, which are not read", port))
		return ReachUnknown
	case f.NFTables == detect.Unknown || f.PolicyIn == "":
		return ReachUnknown // errs has the option's reason
	}
	r := ReachManagement
	if f.PolicyIn == "ACCEPT" {
		r = ReachAnywhere
	}
	if bl := cluster.ipsets["blacklist"]; len(bl) > 0 {
		*errs = append(*errs, fmt.Errorf("port %d: the blacklist IP set (%d entries) is dropped before the management rule, and its entries are not compared with the sources that reach it", port, len(bl)))
		r = ReachUnknown
	}
	for _, c := range []*fwConf{hostfw, cluster} {
		for _, rl := range c.rules {
			if rl.mayBlock(port) {
				f.Findings = append(f.Findings, Finding{Kind: FindingRuleMayBlock, File: c.file, Line: rl.line,
					Detail: fmt.Sprintf("%q may drop port %d before pve-firewall's management rule accepts it", rl.text, port)})
				*errs = append(*errs, fmt.Errorf("port %d: %s line %d may drop it first", port, c.file, rl.line))
				r = ReachUnknown
			}
		}
	}
	return r
}

// clusterEnable reads cluster.fw's enable, a number (parse_clusterfw_option):
// 0 off, 1 on, and anything greater a time stamp, which keeps the firewall
// on until 60 seconds after it and then off.
func clusterEnable(v string) (detect.Tri, error) {
	switch n, err := strconv.Atoi(v); {
	case err != nil:
		return detect.Unknown, fmt.Errorf("%q: %w", v, err)
	case n > 1:
		return detect.Unknown, fmt.Errorf("%d is a time stamp: pve-firewall keeps the firewall on until 60 seconds after it", n)
	default:
		return tri(n == 1), nil
	}
}

// boolean reads a host.fw option, which pve-firewall reads as 0 or 1.
func boolean(v string) (detect.Tri, error) { return tri(v == "1"), nil }

// option reads key from c (def when unset) with read, adding to errs and
// returning Unknown when read refuses the value or a line for key is one
// pve-firewall skips.
func option(c *fwConf, key, def string, read func(string) (detect.Tri, error), errs *[]error) detect.Tri {
	if c.skip(key, errs) {
		return detect.Unknown
	}
	t, err := read(c.get(key, def))
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: %s %w", c.file, key, err))
		return detect.Unknown
	}
	return t
}

// isActive asks systemctl whether unit is active. is-active exits non-zero
// for a unit that is not, so an exit error with a state is an answer.
func isActive(ctx context.Context, sys host.System, unit string, errs []error) (string, []error) {
	res, err := sys.Run(ctx, "systemctl", "is-active", unit)
	var xe *host.ExitError
	if err != nil && !errors.As(err, &xe) {
		return "", append(errs, err)
	}
	state, _, _ := strings.Cut(strings.TrimSpace(string(res.Stdout)), "\n")
	if (detect.UnitState{Active: state}).IsActive() == detect.Unknown {
		errs = append(errs, fmt.Errorf("systemctl is-active %s printed %q (exit %d)", unit, state, res.ExitCode))
	}
	return state, errs
}

// fwKind is which file a configuration is: they have different sections
// and options.
type fwKind uint8

const (
	clusterFile fwKind = iota
	hostFile
)

// The lines pve-firewall reads (Firewall.pm, generic_fw_config_parser,
// parse_clusterfw_option, parse_hostfw_option): the section headers, and
// the forms of the options ReadFirewall reads, value in group 1. A line
// naming one of those options in another form, or outside a section,
// pve-firewall skips with a warning.
var (
	optionsHeader = regexp.MustCompile(`(?i)^\[options\]$`)
	rulesHeader   = regexp.MustCompile(`(?i)^\[rules\]$`)
	aliasesHeader = regexp.MustCompile(`(?i)^\[aliases\]$`)
	namedHeader   = regexp.MustCompile(`(?i)^\[(group|ipset)\s+(\S+)\]\s*(?:#.*)?$`)
	setName       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9\-_]+$`)
	ipsetEntry    = regexp.MustCompile(`^(!)?\s*(\S+)\s*$`)
	optionForms   = map[fwKind]map[string]*regexp.Regexp{
		clusterFile: {
			"enable":    regexp.MustCompile(`(?i)^enable:\s*(\d+)\s*$`),
			"policy_in": regexp.MustCompile(`(?i)^policy_in:\s*(ACCEPT|DROP|REJECT)\s*$`),
		},
		hostFile: {
			"enable":   regexp.MustCompile(`(?i)^enable:\s*([01])\s*$`),
			"nftables": regexp.MustCompile(`(?i)^nftables:\s*([01])\s*$`),
		},
	}
)

// fwConf is a pve-firewall configuration file as pve-firewall reads it
// (pve-docs, Firewall, "Configuration Files").
type fwConf struct {
	file    string
	options map[string]string   // the options ReadFirewall reads, by lower-case name
	skipped map[string]int      // the first line, 1-based, of an option pve-firewall skips
	rules   []fwRule            // [RULES]
	ipsets  map[string][]string // [IPSET name] entries, by lower-case name (cluster.fw)
}

// skip reports whether a line for key is one pve-firewall skips, adding
// that to errs: what pve-firewall then applies is not established here.
func (c *fwConf) skip(key string, errs *[]error) bool {
	line, ok := c.skipped[key]
	if ok {
		*errs = append(*errs, fmt.Errorf("%s line %d: %s: not in a form pve-firewall reads, so it skips the line", c.file, line, key))
	}
	return ok
}

func (c *fwConf) get(key, def string) string {
	if v, ok := c.options[key]; ok {
		return v
	}
	return def
}

// fwRule is an enabled rule line: DIRECTION ACTION [OPTIONS],
// DIRECTION MACRO(ACTION) [OPTIONS] or GROUP name.
type fwRule struct {
	line int
	text string
	f    []string
}

// parseFW reads data as generic_fw_config_parser does: each line trimmed of
// spaces and tabs, "#" lines and blank ones skipped; a header only in the
// exact form of a section the file has ([OPTIONS] and [RULES]; cluster.fw
// also [ALIASES], [GROUP name] and [IPSET name], a valid name); any other
// line belongs to the section before it. A trailing "#" comment is cut from
// rules and IP set entries, never from options.
func parseFW(file string, kind fwKind, data []byte) *fwConf {
	c := &fwConf{file: file, options: map[string]string{}, skipped: map[string]int{}, ipsets: map[string][]string{}}
	section, name := "", ""
	for i, raw := range strings.Split(string(data), "\n") {
		l := strings.Trim(raw, " \t")
		if strings.TrimSpace(l) == "" || l[0] == '#' {
			continue
		}
		switch m := namedHeader.FindStringSubmatch(l); {
		case optionsHeader.MatchString(l):
			section, name = "options", ""
			continue
		case rulesHeader.MatchString(l):
			section, name = "rules", ""
			continue
		case kind == clusterFile && aliasesHeader.MatchString(l):
			section, name = "aliases", ""
			continue
		case kind == clusterFile && m != nil:
			section, name = strings.ToLower(m[1]), strings.ToLower(m[2])
			if !setName.MatchString(m[2]) {
				section, name = "", ""
			}
			continue
		}
		switch section {
		case "", "options":
			k, _, ok := strings.Cut(l, ":")
			if !ok {
				k = strings.Fields(l)[0]
			}
			k = strings.ToLower(strings.TrimSpace(k))
			form, ours := optionForms[kind][k]
			if !ours {
				break
			}
			if m := form.FindStringSubmatch(l); section != "" && m != nil {
				c.options[k] = m[1]
			} else if _, ok := c.skipped[k]; !ok {
				c.skipped[k] = i + 1
			}
		case "rules":
			r, _, _ := strings.Cut(l, "#")
			if r = strings.TrimSpace(r); r != "" && !strings.HasPrefix(r, "|") {
				c.rules = append(c.rules, fwRule{line: i + 1, text: r, f: strings.Fields(r)})
			}
		case "ipset":
			e, _, _ := strings.Cut(l, "#")
			e = strings.TrimSpace(e)
			if m := ipsetEntry.FindStringSubmatch(e); m != nil {
				e = m[1] + m[2]
			}
			if e != "" {
				c.ipsets[name] = append(c.ipsets[name], e)
			}
		}
	}
	return c
}

// mayBlock: the rule may drop or reject new TCP connections to port. An
// accept, or an OUT or FORWARD rule, cannot; a GROUP (security groups are
// not read), a macro other than (ACCEPT), and a drop or reject without a
// protocol other than TCP or a numeric port list leaving port out, may.
func (r fwRule) mayBlock(port int) bool {
	if len(r.f) == 0 || strings.EqualFold(r.f[0], "GROUP") {
		return true
	}
	switch strings.ToUpper(r.f[0]) {
	case "OUT", "FORWARD":
		return false
	case "IN":
	default:
		return true // not a direction hardn reads
	}
	if len(r.f) < 2 {
		return true
	}
	action := strings.ToUpper(r.f[1])
	if m, a, ok := strings.Cut(action, "("); ok && m != "" {
		return a != "ACCEPT)"
	}
	if action == "ACCEPT" {
		return false
	}
	for i := 2; i+1 < len(r.f); i++ {
		switch strings.TrimLeft(r.f[i], "-") {
		case "p", "proto":
			if p := strings.ToLower(r.f[i+1]); p != "tcp" && p != "6" {
				return false
			}
		case "dport":
			if ports, ok := portList(r.f[i+1]); ok && !ports(port) {
				return false
			}
		}
	}
	return true
}

// portList reads a numeric --dport: "22", "80:85", "22,8006". ok is false for
// anything else (a service name), which may name any port.
func portList(s string) (func(int) bool, bool) {
	type span struct{ lo, hi int }
	var spans []span
	for _, part := range strings.Split(s, ",") {
		a, b, isRange := strings.Cut(part, ":")
		lo, err1 := strconv.Atoi(a)
		hi, err2 := lo, error(nil)
		if isRange {
			hi, err2 = strconv.Atoi(b)
		}
		if err1 != nil || err2 != nil {
			return nil, false
		}
		spans = append(spans, span{lo, hi})
	}
	return func(p int) bool {
		for _, sp := range spans {
			if p >= sp.lo && p <= sp.hi {
				return true
			}
		}
		return false
	}, true
}
