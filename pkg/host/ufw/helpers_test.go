package ufw_test

import (
	"encoding/hex"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// The answers below follow ufw 0.36.2's sources (backend_iptables.py
// get_status and set_rule, frontend.py set_enabled and set_default_policy,
// ufw-init-functions; docs/safety/ufw.md §3), not output captured from a
// host.

var notPVE = detect.Proxmox{Installed: detect.No}

// stockDefaults is ufw's conf/ufw.defaults, comments left out.
const stockDefaults = "IPV6=yes\nDEFAULT_INPUT_POLICY=\"DROP\"\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n" +
	"DEFAULT_FORWARD_POLICY=\"DROP\"\nDEFAULT_APPLICATION_POLICY=\"SKIP\"\nMANAGE_BUILTINS=no\n"

func listener(addr string, port uint16) sshd.Listener {
	return sshd.Listener{Addr: netip.MustParseAddr(addr), Port: port}
}

// stock22 is sshd's own 0.0.0.0:22 and [::]:22 (Debian, Alpine).
func stock22() sshd.Listeners {
	ls := []sshd.Listener{listener("0.0.0.0", 22), listener("::", 22)}
	return sshd.Listeners{SSHD: ls, All: ls, Known: true}
}

// modelRule is one saved rule of the model: an allow, limit, deny or
// reject of <port>/tcp from anywhere, or a free-form tuple and status row.
type modelRule struct {
	fam    ufw.Family
	action string
	port   string
	tuple  string // overrides the tuple written from the fields
	row    string // overrides the status row
	// key is, for a rule a "from" command added (fromRule), its words
	// without the action, log type and comment: ufw's tuple.
	key, verb string
}

// model is a small ufw: saved rules per IP version, the defaults, whether
// it is active (and ENABLED=yes in ufw.conf, unless mismatch), and the
// answers ufw 0.36.2 gives.
type model struct {
	t        *testing.T
	active   bool
	mismatch bool // ENABLED in ufw.conf is the opposite of active
	ipv6     bool
	incoming string // allow, deny, reject
	outgoing string
	rules    []modelRule
	conf     string // ufw.conf's content, when set
	// enableOut, when set, is enable's answer, and ufw stays inactive (an
	// "Aborted" exit 0).
	enableOut string
	fail      string // an argv, joined by spaces, that exits 1 and does nothing
	// partial is an argv that does its work, then exits 1, as ufw does
	// when the restart after a saved policy, or the start after
	// ENABLED=yes, fails: the policy stays saved; the chains stay loaded
	// with ENABLED put back to no.
	partial   string
	noop      string // an argv answered as done that changes nothing
	forget    string // a port whose allow is answered as added but not saved
	garbage   bool   // the status is followed by a line ufw does not print
	noKernel6 bool   // no /proc/sys/net/ipv6
}

func newModel(t *testing.T) *model {
	return &model{t: t, ipv6: true, incoming: "deny", outgoing: "allow"}
}

var policyWord = map[string]string{"allow": "ACCEPT", "deny": "DROP", "reject": "REJECT"}

// host builds a fake host whose ufw is the model.
func (m *model) host() *hostfake.System {
	h := hostfake.New(host.Options{})
	if !m.noKernel6 {
		h.AddDir("/proc/sys/net/ipv6", 0o555, 0, 0)
	}
	m.files(h)
	h.Handler = func(argv []string, _ []byte) hostfake.Response { return m.run(h, argv) }
	return h
}

// files writes the defaults and the saved rule files.
func (m *model) files(h *hostfake.System) {
	v6 := "no"
	if m.ipv6 {
		v6 = "yes"
	}
	h.AddFile(ufw.DefaultsFile, []byte(fmt.Sprintf("# /etc/default/ufw\nIPV6=%s\nDEFAULT_INPUT_POLICY=%q\nDEFAULT_OUTPUT_POLICY=%q\n",
		v6, policyWord[m.incoming], policyWord[m.outgoing])), 0o644, 0, 0)
	conf := m.conf
	if conf == "" {
		enabled := "no"
		if m.active != m.mismatch {
			enabled = "yes"
		}
		conf = "# /etc/ufw/ufw.conf\nENABLED=" + enabled + "\nLOGLEVEL=low\n"
	}
	h.AddFile(ufw.ConfFile, []byte(conf), 0o644, 0, 0)
	for _, f := range []struct {
		path string
		fam  ufw.Family
		any  string
	}{{ufw.UserRules, ufw.IPv4, "0.0.0.0/0"}, {ufw.User6Rules, ufw.IPv6, "::/0"}} {
		var b strings.Builder
		b.WriteString("*filter\n:ufw-user-input - [0:0]\n### RULES ###\n")
		for _, r := range m.rules {
			if r.fam != f.fam {
				continue
			}
			if r.tuple != "" {
				b.WriteString("\n" + r.tuple + "\n")
				continue
			}
			fmt.Fprintf(&b, "\n### tuple ### %s tcp %s %s any %s in\n-A ufw-user-input -p tcp --dport %s -j ACCEPT\n",
				r.action, r.port, f.any, f.any, r.port)
		}
		b.WriteString("\n### END RULES ###\nCOMMIT\n")
		h.AddFile(f.path, []byte(b.String()), 0o640, 0, 0)
	}
}

func (m *model) status() string {
	if m.garbage {
		return "Status: active\ngarbage\n"
	}
	if !m.active {
		return "Status: inactive\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Status: active\nLogging: on (low)\nDefault: %s (incoming), %s (outgoing), disabled (routed)\nNew profiles: skip\n",
		m.incoming, m.outgoing)
	if len(m.rules) > 0 {
		fmt.Fprintf(&b, "\n%-26s %-12s%s\n%-26s %-12s%s\n", "To", "Action", "From", "--", "------", "----")
	}
	for _, fam := range []ufw.Family{ufw.IPv4, ufw.IPv6} {
		for _, r := range m.rules {
			if r.fam != fam {
				continue
			}
			if r.row != "" {
				b.WriteString(r.row + "\n")
				continue
			}
			to, from := r.port+"/tcp", "Anywhere"
			if fam == ufw.IPv6 {
				to, from = to+" (v6)", from+" (v6)"
			}
			fmt.Fprintf(&b, "%-26s %-12s%-26s\n", to, strings.ToUpper(r.action)+" IN", from)
		}
	}
	return b.String()
}

func (m *model) run(h *hostfake.System, argv []string) hostfake.Response {
	cmd := strings.Join(argv, " ")
	if cmd == m.fail {
		return hostfake.Response{Stderr: "ERROR: Could not update running firewall\n", ExitCode: 1}
	}
	if cmd == m.noop {
		return hostfake.Response{Stdout: "Rule added\n"}
	}
	var out string
	switch {
	case cmd == "ufw status verbose":
		out = m.status()
	case slices.Contains(argv, "proto") && slices.Contains(argv, "port"):
		out = m.fromRule(argv[1:])
	case len(argv) == 3 && (argv[1] == "limit" || argv[1] == "allow" && m.has("limit", argv[2])):
		// set_rule replaces a saved rule of the same tuple and another
		// action in its place, "Rule updated" while active (ufw.md §3).
		port := strings.TrimSuffix(argv[2], "/tcp")
		fams := []ufw.Family{ufw.IPv4}
		if m.ipv6 {
			fams = append(fams, ufw.IPv6)
		}
		for _, fam := range fams {
			i := slices.IndexFunc(m.rules, func(r modelRule) bool { return r.fam == fam && r.port == port && r.tuple == "" })
			sfx := ""
			if fam == ufw.IPv6 {
				sfx = " (v6)"
			}
			switch {
			case i >= 0 && m.rules[i].action == argv[1]:
				out += "Skipping adding existing rule" + sfx + "\n"
			case i >= 0:
				m.rules[i].action = argv[1]
				out += m.word("Rule updated", sfx)
			default:
				m.rules = append(m.rules, modelRule{fam: fam, action: argv[1], port: port})
				out += m.word("Rule added", sfx)
			}
		}
	case len(argv) == 3 && argv[1] == "allow", len(argv) == 4 && argv[1] == "delete":
		port := strings.TrimSuffix(argv[len(argv)-1], "/tcp")
		fams := []ufw.Family{ufw.IPv4}
		if m.ipv6 {
			fams = append(fams, ufw.IPv6)
		}
		for _, fam := range fams {
			i := slices.IndexFunc(m.rules, func(r modelRule) bool { return r.fam == fam && r.action == "allow" && r.port == port })
			sfx := ""
			if fam == ufw.IPv6 {
				sfx = " (v6)"
			}
			switch {
			case argv[1] == "allow" && i >= 0:
				out += "Skipping adding existing rule" + sfx + "\n"
			case argv[1] == "allow":
				if port != m.forget {
					m.rules = append(m.rules, modelRule{fam: fam, action: "allow", port: port})
				}
				out += m.word("Rule added", sfx)
			case i >= 0:
				m.rules = slices.Delete(m.rules, i, i+1)
				out += m.word("Rule deleted", sfx)
			default:
				out += "Could not delete non-existent rule" + sfx + "\n"
			}
		}
	case len(argv) == 4 && argv[1] == "default":
		if argv[3] == "incoming" {
			m.incoming = argv[2]
		} else {
			m.outgoing = argv[2]
		}
		out = fmt.Sprintf("Default %s policy changed to '%s'\n(be sure to update your rules accordingly)\n", argv[3], argv[2])
	case cmd == "ufw --force enable" && m.enableOut != "":
		out = m.enableOut
	case cmd == "ufw --force enable":
		m.active = true
		out = "Firewall is active and enabled on system startup\n"
	case cmd == "ufw disable":
		m.active, m.mismatch = false, false
		out = "Firewall stopped and disabled on system startup\n"
	default:
		m.t.Errorf("unexpected command %q", cmd)
		return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
	}
	if cmd == m.partial {
		m.mismatch = cmd == "ufw --force enable"
		m.files(h)
		return hostfake.Response{Stderr: "ERROR: problem running ufw-init\n", ExitCode: 1}
	}
	m.files(h)
	return hostfake.Response{Stdout: out}
}

// has reports whether the model holds a plain rule of action on spec
// ("22/tcp") in some IP version.
func (m *model) has(action, spec string) bool {
	port := strings.TrimSuffix(spec, "/tcp")
	return slices.ContainsFunc(m.rules, func(r modelRule) bool { return r.action == action && r.port == port && r.tuple == "" })
}

// word is set_rule's answer: "Rules updated" while inactive.
func (m *model) word(active, sfx string) string {
	if !m.active {
		return "Rules updated" + sfx + "\n"
	}
	return active + sfx + "\n"
}

func runs(h *hostfake.System) []string {
	var out []string
	for _, r := range h.Runs() {
		out = append(out, strings.Join(r, " "))
	}
	return out
}

// saved is a saved rule from its tuple line, the status row made from what
// ufw.ParseTuples reads of it: "[<to> ]<ports>[/<proto>][ on <iface>]",
// the action, and the source as read.
func saved(fam ufw.Family, tuple string) modelRule {
	rs, bad := ufw.ParseTuples([]byte("### tuple ### "+tuple+"\n"), fam)
	if len(rs) != 1 || len(bad) > 0 {
		panic("tuple " + tuple)
	}
	r := rs[0]
	to, from := r.DPort, r.Src
	if r.Proto != "any" {
		to += "/" + r.Proto
	}
	if r.DPort == "" {
		to = "Anywhere"
	}
	if r.Dst != "" {
		to = r.Dst + " " + to
	}
	if from == "" {
		from = "Anywhere"
		if fam == ufw.IPv6 {
			to, from = to+" (v6)", from+" (v6)"
		}
	}
	if r.Iface != "" {
		to += " on " + r.Iface
	}
	mr := modelRule{fam: fam, tuple: "### tuple ### " + tuple, row: fmt.Sprintf("%-26s %-12s%s", to, strings.ToUpper(r.Action)+" IN", from)}
	if r.Proto == "tcp" && r.App == "" && !strings.Contains(r.Src, " ") {
		mr.key, mr.verb = fmt.Sprintf("%d tcp %s %s %s %s", fam, r.DPort, r.Dst, r.Src, r.Iface), strings.Fields(tuple)[0]
	}
	return mr
}

// fromRule answers a rule with a source, a "to" address or an interface,
// or its delete, as ufw 0.36.2's set_rule does (read, not run): "<action>
// [in on <iface>] [log|log-all] [from <src>] to <dst> port <port> proto tcp
// [comment <text>]". The addresses say the IP version; with none, both. A
// saved rule of the same tuple is skipped where its action, log type and
// comment are the same, and replaced where not; a delete compares the
// action and log type, never the comment.
func (m *model) fromRule(words []string) string {
	del := words[0] == "delete"
	if del {
		words = words[1:]
	}
	action, rest := words[0], words[1:]
	var iface, logType, src, dst, port, comment string
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "in":
		case "on":
			i++
			iface = rest[i]
		case "log", "log-all":
			logType = rest[i]
		case "from":
			i++
			src = rest[i]
		case "to":
			i++
			dst = rest[i]
		case "port":
			i++
			port = rest[i]
		case "proto":
			i++
		case "comment":
			i++
			comment = rest[i]
		default:
			m.t.Errorf("unexpected word %q in %q", rest[i], words)
		}
	}
	if src == "any" {
		src = ""
	}
	if dst == "any" {
		dst = ""
	}
	fams := []ufw.Family{ufw.IPv4, ufw.IPv6}
	switch {
	case strings.Contains(src+dst, ":"):
		fams = fams[1:]
	case src != "" || dst != "":
		fams = fams[:1]
	}
	verb := action
	if logType != "" {
		verb += "_" + logType
	}
	var out string
	for _, fam := range fams {
		sfx, wild := "", "0.0.0.0/0"
		if fam == ufw.IPv6 {
			sfx, wild = " (v6)", "::/0"
		}
		if fam == ufw.IPv6 && !m.ipv6 {
			continue
		}
		s, d := src, dst
		if s == wild {
			s = ""
		}
		key := fmt.Sprintf("%d tcp %s %s %s %s", fam, port, d, s, iface)
		i := slices.IndexFunc(m.rules, func(r modelRule) bool { return r.key == key })
		tuple := fmt.Sprintf("%s tcp %s %s any %s in", verb, port, orWild(d, wild), orWild(s, wild))
		if iface != "" {
			tuple += "_" + iface
		}
		if comment != "" {
			tuple += " comment=" + hex.EncodeToString([]byte(comment))
		}
		r := saved(fam, tuple)
		r.key, r.verb = key, verb
		if logType != "" {
			r.row += " (" + logType + ")"
		}
		if comment != "" {
			r.row += " # " + comment
		}
		switch {
		case del && i >= 0 && m.rules[i].verb == verb:
			m.rules = slices.Delete(m.rules, i, i+1)
			out += m.word("Rule deleted", sfx)
		case del:
			out += "Could not delete non-existent rule" + sfx + "\n"
		case i >= 0 && m.rules[i].tuple == r.tuple:
			out += "Skipping adding existing rule" + sfx + "\n"
		case i >= 0:
			m.rules[i] = r
			out += m.word("Rule updated", sfx)
		default:
			m.rules = append(m.rules, r)
			out += m.word("Rule added", sfx)
		}
	}
	return out
}

func orWild(a, wild string) string {
	if a == "" {
		return wild
	}
	return a
}
