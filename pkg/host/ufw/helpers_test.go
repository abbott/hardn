package ufw_test

import (
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
// get_status and set_rule, frontend.py, ufw-init-functions; docs/safety/
// ufw.md §3), not output captured from a host.

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
}

// model is a small ufw: saved rules per IP version, the defaults, whether
// it is active, and the answers ufw 0.36.2 gives.
type model struct {
	t        *testing.T
	active   bool
	ipv6     bool
	incoming string // allow, deny, reject
	outgoing string
	rules    []modelRule
	// enableOut, when set, is enable's answer, and ufw stays inactive (an
	// "Aborted" exit 0).
	enableOut string
	fail      string // an argv, joined by spaces, that exits 1
	forget    string // a port whose allow is answered as added but not saved
}

func newModel(t *testing.T) *model {
	return &model{t: t, ipv6: true, incoming: "deny", outgoing: "allow"}
}

var policyWord = map[string]string{"allow": "ACCEPT", "deny": "DROP", "reject": "REJECT"}

// host builds a fake host whose ufw is the model.
func (m *model) host() *hostfake.System {
	h := hostfake.New(host.Options{})
	h.AddDir("/proc/sys/net/ipv6", 0o555, 0, 0)
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
	var out string
	switch {
	case cmd == "ufw status verbose":
		out = m.status()
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
		m.active = false
		out = "Firewall stopped and disabled on system startup\n"
	default:
		m.t.Errorf("unexpected command %q", cmd)
		return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
	}
	m.files(h)
	return hostfake.Response{Stdout: out}
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
