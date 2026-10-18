package pve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
)

func (r Reach) String() string {
	switch r {
	case ReachNotFiltered:
		return "not filtered by pve-firewall"
	case ReachAnywhere:
		return "allowed from anywhere"
	case ReachManagement:
		return "allowed from the management IP set"
	}
	return "unknown"
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
	// Err says why a field is Unknown; it wraps ErrStateUnknown.
	Err error
}

// ReadFirewall reads pve-firewall's state on a Proxmox VE host: cluster.fw,
// host.fw and "systemctl is-active" of the unit that applies them. It
// changes nothing, runs nothing else (not "pve-firewall status", which takes
// the firewall's lock), and never touches UFW or the packet filter. Off
// Proxmox VE it returns ErrNotApplicable or ErrProxmoxUnknown; with pmxcfs
// not mounted every field is Unknown, as the files are not there. The only
// other error is a done ctx: what cannot be read or parsed is Unknown, with
// Err.
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
	read := func(name string) *fwConf {
		data, err := sys.ReadFile(ctx, name)
		if errors.Is(err, fs.ErrNotExist) {
			return &fwConf{file: name, options: map[string]string{}, ipsets: map[string][]string{}}
		} else if err != nil {
			errs = append(errs, err)
			return nil
		}
		return parseFW(name, data)
	}
	cluster, hostfw := read(ClusterFW), read(HostFW)
	f := Firewall{Unit: FirewallUnit}
	var clusterOn, hostOn detect.Tri
	if cluster != nil {
		clusterOn = option(cluster, "enable", "0", func(v string) (detect.Tri, bool) {
			n, err := strconv.Atoi(v)
			return tri(n != 0), err == nil && n >= 0
		}, &errs)
		switch v := strings.ToUpper(cluster.get("policy_in", "DROP")); v {
		case "ACCEPT", "DROP", "REJECT":
			f.PolicyIn = v
		default:
			errs = append(errs, fmt.Errorf("%s: policy_in %q", ClusterFW, v))
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
// host's rules, then the cluster's, before its built-in management rules
// (Firewall.pm, enable_host_firewall), so any rule that may drop the port
// makes it Unknown.
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
		return ReachUnknown
	case f.NFTables == detect.Unknown || f.PolicyIn == "":
		return ReachUnknown
	}
	r := ReachManagement
	if f.PolicyIn == "ACCEPT" {
		r = ReachAnywhere
	}
	for _, c := range []*fwConf{hostfw, cluster} {
		for _, rl := range c.rules {
			if rl.mayBlock(port) {
				f.Findings = append(f.Findings, Finding{Kind: FindingRuleMayBlock, File: c.file, Line: rl.line,
					Detail: fmt.Sprintf("%q may drop port %d before pve-firewall's management rule accepts it", rl.text, port)})
				r = ReachUnknown
			}
		}
	}
	return r
}

// boolean reads a host.fw boolean, which Firewall.pm accepts as 0 or 1.
func boolean(v string) (detect.Tri, bool) {
	return tri(v == "1"), v == "0" || v == "1"
}

// option reads key from c (def when unset) with parse, adding an error when
// the value is not one parse accepts.
func option(c *fwConf, key, def string, parse func(string) (detect.Tri, bool), errs *[]error) detect.Tri {
	v := c.get(key, def)
	t, ok := parse(v)
	if !ok {
		*errs = append(*errs, fmt.Errorf("%s: %s %q", c.file, key, v))
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

// fwConf is a pve-firewall configuration file: sections of lines, "#"
// starting a comment (pve-docs, Firewall, "Configuration Files").
type fwConf struct {
	file    string
	options map[string]string   // [OPTIONS] "key: value", keys in lower case
	rules   []fwRule            // [RULES]
	ipsets  map[string][]string // [IPSET name] entries, by lower-case name
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

func parseFW(file string, data []byte) *fwConf {
	c := &fwConf{file: file, options: map[string]string{}, ipsets: map[string][]string{}}
	section, name := "", ""
	for i, raw := range strings.Split(string(data), "\n") {
		l, _, _ := strings.Cut(raw, "#")
		l = strings.TrimSpace(l)
		switch {
		case l == "":
		case strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]"):
			f := strings.Fields(strings.Trim(l, "[]"))
			section, name = "", ""
			if len(f) > 0 {
				section = strings.ToLower(f[0])
			}
			if len(f) > 1 {
				name = strings.ToLower(f[1])
			}
		case section == "options":
			if k, v, ok := strings.Cut(l, ":"); ok {
				c.options[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
			}
		case section == "rules" && !strings.HasPrefix(l, "|"):
			c.rules = append(c.rules, fwRule{line: i + 1, text: l, f: strings.Fields(l)})
		case section == "ipset":
			c.ipsets[name] = append(c.ipsets[name], strings.Fields(l)[0])
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
