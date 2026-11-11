package ufw_test

import (
	"fmt"
	"net/netip"
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

func needKeys(ns []ufw.Need) []string {
	var out []string
	for _, n := range ns {
		out = append(out, fmt.Sprintf("%s %d %s new=%t", n.Family, n.Port, n.Addr, n.New))
	}
	return out
}

func TestNeeds(t *testing.T) {
	sock := []sshd.Listener{listener("::", 22)}
	for _, tc := range []struct {
		name      string
		ls        sshd.Listeners
		add, keep []uint16
		want      []string
	}{
		{"sshd's two listeners and the new port", stock22(), []uint16{2230}, nil, []string{
			"IPv4 22 0.0.0.0 new=false", "IPv6 22 :: new=false", "IPv4 2230 0.0.0.0 new=true", "IPv6 2230 :: new=true"}},
		{"ssh.socket's [::] is dual-stack", sshd.Listeners{SSHD: stock22().SSHD, Socket: sock, SocketRead: true, Known: true}, nil, nil, []string{
			"IPv4 22 0.0.0.0 new=false", "IPv6 22 :: new=false"}},
		{"socket alone: [::] needs IPv4 too", sshd.Listeners{Socket: sock, SocketRead: true, Known: true}, nil, nil, []string{
			"IPv6 22 :: new=false", "IPv4 22 0.0.0.0 new=false"}},
		{"an IPv4 address; the new and kept ports in IPv4 only", sshd.Listeners{SSHD: []sshd.Listener{listener("192.0.2.10", 2230)}, Known: true},
			[]uint16{2222}, []uint16{22}, []string{"IPv4 2230 192.0.2.10 new=false", "IPv4 2222 0.0.0.0 new=true", "IPv4 22 0.0.0.0 new=false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := needKeys(ufw.Needs(tc.ls, tc.add, tc.keep)); !slices.Equal(got, tc.want) {
				t.Errorf("needs\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func rule(fam ufw.Family, action, dport, dst, src, iface string) ufw.Rule {
	return ufw.Rule{Action: action, Dir: "in", Proto: "tcp", DPort: dport, Dst: dst, Src: src, Iface: iface, Family: fam,
		Line: fmt.Sprintf("%s %s %s to %s from %s on %s", fam, action, dport, dst, src, iface)}
}

// TestVerdict: the first match decides, in each family's own list, as
// ufw's chains do; restricted rules only restrict.
func TestVerdict(t *testing.T) {
	v4, v6 := ufw.IPv4, ufw.IPv6
	n22 := ufw.Need{Port: 22, Family: v4, Addr: netip.IPv4Unspecified()}
	n2230v6 := ufw.Need{Port: 2230, Family: v6, Addr: netip.IPv6Unspecified()}
	at := ufw.Need{Port: 2230, Family: v4, Addr: netip.MustParseAddr("192.0.2.10")}
	allow22 := rule(v4, "allow", "22", "", "", "")
	on := func(p ufw.Policy) ufw.Firewall { return ufw.Firewall{Active: true, IPv6: true, Incoming: p} }
	lo := ufw.Need{Port: 22, Family: v4, Addr: netip.MustParseAddr("127.0.0.1")}
	lo6 := ufw.Need{Port: 22, Family: v6, Addr: netip.IPv6Loopback()}
	// udp is a UDP need (coturn's 3478), against ufw allow 3478/udp, 3478
	// (any protocol) and 3478/tcp.
	udp := ufw.Need{Port: 3478, Family: v4, Addr: netip.IPv4Unspecified(), Proto: "udp"}
	proto := func(p string) ufw.Rule {
		return ufw.Rule{Action: "allow", Dir: "in", Proto: p, DPort: "3478", Family: v4}
	}
	for _, tc := range []struct {
		name  string
		rules []ufw.Rule
		fw    ufw.Firewall
		need  ufw.Need
		want  ufw.Verdict
	}{
		{"inactive accepts everything", nil, ufw.Firewall{IPv6: true, Incoming: ufw.PolicyDeny}, n22, ufw.Allowed},
		{"no rule, default deny", nil, on(ufw.PolicyDeny), n22, ufw.Blocked},
		{"no rule, default reject", nil, on(ufw.PolicyReject), n22, ufw.Blocked},
		{"no rule, default allow", nil, on(ufw.PolicyAllow), n22, ufw.Allowed},
		{"no policy", nil, on(""), n22, ufw.VerdictUnknown},
		{"allow", []ufw.Rule{allow22}, on(ufw.PolicyDeny), n22, ufw.Allowed},
		{"limit", []ufw.Rule{rule(v4, "limit", "22", "", "", "")}, on(ufw.PolicyDeny), n22, ufw.Limited},
		{"a range and a list", []ufw.Rule{rule(v4, "allow", "20:21,22", "", "", "")}, on(ufw.PolicyDeny), n22, ufw.Allowed},
		{"another port", []ufw.Rule{rule(v4, "allow", "2222", "", "", "")}, on(ufw.PolicyDeny), n22, ufw.Blocked},
		{"udp only", []ufw.Rule{{Action: "allow", Dir: "in", Proto: "udp", DPort: "22", Family: v4}}, on(ufw.PolicyDeny), n22, ufw.Blocked},
		{"an out rule", []ufw.Rule{{Action: "allow", Dir: "out", Proto: "tcp", DPort: "22", Family: v4}}, on(ufw.PolicyDeny), n22, ufw.Blocked},
		{"the other family's rule", []ufw.Rule{rule(v6, "allow", "22", "", "", "")}, on(ufw.PolicyDeny), n22, ufw.Blocked},
		{"deny first", []ufw.Rule{rule(v4, "deny", "22", "", "", ""), allow22}, on(ufw.PolicyDeny), n22, ufw.Denied},
		{"reject first, default allow", []ufw.Rule{rule(v4, "reject", "22", "", "", "")}, on(ufw.PolicyAllow), n22, ufw.Denied},
		{"allow first, deny after", []ufw.Rule{allow22, rule(v4, "deny", "22", "", "", "")}, on(ufw.PolicyDeny), n22, ufw.Allowed},
		{"source-restricted allow only", []ufw.Rule{rule(v4, "allow", "22", "", "192.0.2.0/24", "")}, on(ufw.PolicyDeny), n22, ufw.Restricted},
		{"interface-restricted allow only", []ufw.Rule{rule(v4, "allow", "22", "", "", "eth0")}, on(ufw.PolicyDeny), n22, ufw.Restricted},
		{"restricted allow, then deny", []ufw.Rule{rule(v4, "allow", "22", "", "192.0.2.0/24", ""), rule(v4, "deny", "22", "", "", "")}, on(ufw.PolicyAllow), n22, ufw.Restricted},
		{"restricted deny, then allow: a blocklist", []ufw.Rule{rule(v4, "deny", "22", "", "198.51.100.0/24", ""), allow22}, on(ufw.PolicyDeny), n22, ufw.Excepted},
		{"restricted deny, then limit", []ufw.Rule{rule(v4, "deny", "22", "", "198.51.100.0/24", ""), rule(v4, "limit", "22", "", "", "")}, on(ufw.PolicyDeny), n22, ufw.Excepted},
		{"restricted deny, default allow", []ufw.Rule{rule(v4, "deny", "22", "", "198.51.100.0/24", "")}, on(ufw.PolicyAllow), n22, ufw.Excepted},
		{"restricted deny, default deny", []ufw.Rule{rule(v4, "deny", "22", "", "198.51.100.0/24", "")}, on(ufw.PolicyDeny), n22, ufw.Blocked},
		{"restricted allow and deny, then deny: an allowlist", []ufw.Rule{rule(v4, "allow", "22", "", "192.0.2.0/24", ""),
			rule(v4, "deny", "22", "", "198.51.100.0/24", ""), rule(v4, "deny", "22", "", "", "")}, on(ufw.PolicyAllow), n22, ufw.Restricted},
		{"to one address, listener on all", []ufw.Rule{rule(v4, "allow", "22", "192.0.2.10", "", "")}, on(ufw.PolicyDeny), n22, ufw.Restricted},
		{"to the listener's address", []ufw.Rule{rule(v4, "allow", "2230", "192.0.2.10", "", "")}, on(ufw.PolicyDeny), at, ufw.Allowed},
		{"to its prefix", []ufw.Rule{rule(v4, "allow", "2230", "192.0.2.0/24", "", "")}, on(ufw.PolicyDeny), at, ufw.Allowed},
		{"to another address", []ufw.Rule{rule(v4, "deny", "2230", "192.0.2.11", "", ""), rule(v4, "allow", "2230", "", "", "")}, on(ufw.PolicyDeny), at, ufw.Allowed},
		{"IPv6 2230 allowed", []ufw.Rule{rule(v6, "allow", "2230", "", "", "")}, on(ufw.PolicyDeny), n2230v6, ufw.Allowed},
		{"IPv6 off: ufw drops IPv6", []ufw.Rule{rule(v6, "allow", "2230", "", "", "")}, ufw.Firewall{Active: true, Incoming: ufw.PolicyDeny}, n2230v6, ufw.Blocked},
		{"no IPv6 in the kernel: nothing arrives", nil, ufw.Firewall{Active: true, NoKernelIPv6: true, Incoming: ufw.PolicyDeny}, n2230v6, ufw.Unreachable},
		{"loopback: before.rules accepts lo", []ufw.Rule{rule(v4, "deny", "22", "", "", "")}, on(ufw.PolicyDeny), lo, ufw.Allowed},
		{"IPv6 loopback, IPv6 off", nil, ufw.Firewall{Active: true, Incoming: ufw.PolicyDeny}, lo6, ufw.Allowed},
		{"an unreadable port", []ufw.Rule{rule(v4, "allow", "ssh", "", "", ""), allow22}, on(ufw.PolicyDeny), n22, ufw.VerdictUnknown},
		{"udp: allow 3478/udp", []ufw.Rule{proto("udp")}, on(ufw.PolicyDeny), udp, ufw.Allowed},
		{"udp: allow 3478, any protocol", []ufw.Rule{proto("any")}, on(ufw.PolicyDeny), udp, ufw.Allowed},
		{"udp: allow 3478/tcp does not match", []ufw.Rule{proto("tcp")}, on(ufw.PolicyDeny), udp, ufw.Blocked},
		{"tcp, Proto empty: allow 3478/udp does not match", []ufw.Rule{proto("udp")}, on(ufw.PolicyDeny),
			ufw.Need{Port: 3478, Family: v4, Addr: netip.IPv4Unspecified()}, ufw.Blocked},
		{"an unreadable address", []ufw.Rule{rule(v4, "allow", "2230", "nowhere", "", "")}, on(ufw.PolicyDeny), at, ufw.VerdictUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := ufw.Evaluate(tc.rules, tc.fw, []ufw.Need{tc.need})[0]
			if c.Verdict != tc.want {
				t.Errorf("verdict %s by %q, want %s", c.Verdict, c.By, tc.want)
			}
		})
	}
}

// TestCheck: the cross-check p4-risk scores, from "ufw status verbose":
// with the default deny in force, the SSH listener on 2230 is not let
// through in IPv6 (only 22 is), and an IPv4 rule restricts it to a subnet.
func TestCheck(t *testing.T) {
	out := `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW IN    Anywhere
2230/tcp                   ALLOW IN    192.0.2.0/24
22/tcp (v6)                ALLOW IN    Anywhere (v6)
`
	ls := []sshd.Listener{listener("0.0.0.0", 2230), listener("::", 2230)}
	needs := ufw.Needs(sshd.Listeners{SSHD: ls, All: ls, Known: true}, nil, nil)
	st := ufw.State{Installed: true, IPv6: true, Status: ufw.ParseStatus([]byte(out))}
	_, fs := ufw.Check(st, needs)
	var got []string
	for _, f := range fs {
		got = append(got, fmt.Sprintf("%s %s %s", f.Kind, f.Coverage.Need.Family, f.Incoming))
	}
	want := []string{"ssh-restricted IPv4 deny", "ssh-not-allowed IPv6 deny"}
	if !slices.Equal(got, want) {
		t.Errorf("findings %q, want %q", got, want)
	}
	if fs[1].String() != `ssh-not-allowed: listener [::]:2230 (IPv6 2230, blocked): default deny (incoming)` {
		t.Errorf("String() = %q", fs[1].String())
	}

	st.Status = ufw.ParseStatus([]byte("Status: inactive\n"))
	if _, fs := ufw.Check(st, needs); len(fs) != 0 {
		t.Errorf("inactive: findings %v, want none", fs)
	}
	st.Status = ufw.ParseStatus([]byte(out + "garbage\n"))
	if _, fs := ufw.Check(st, needs); len(fs) != 1 || fs[0].Kind != ufw.FindingUnknown {
		t.Errorf("unread status: findings %v, want one ufw-unknown", fs)
	}
}

// TestCheckStates: the other states Check reports.
func TestCheckStates(t *testing.T) {
	needs := ufw.Needs(stock22(), nil, nil)
	head := "Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n\n" +
		"To                         Action      From\n--                         ------      ----\n"
	for _, tc := range []struct {
		name string
		st   ufw.State
		want []string
	}{
		{"not installed: not configured, not unknown", ufw.State{}, nil},
		// A port this package does not read: unknown, never a pass.
		{"a rule it cannot read", ufw.State{Installed: true, IPv6: true, Status: ufw.Status{Known: true, Active: true,
			Logging: "on (low)", Incoming: ufw.PolicyDeny, Rules: []ufw.Rule{rule(ufw.IPv4, "allow", "22:", "", "", ""),
				rule(ufw.IPv6, "allow", "22:", "", "", "")}}}, []string{"ufw-unknown IPv4", "ufw-unknown IPv6"}},
		{"a blocklist", ufw.State{Installed: true, IPv6: true, Status: ufw.ParseStatus([]byte(head +
			"22/tcp                     DENY IN     198.51.100.0/24\n" +
			"22/tcp                     ALLOW IN    Anywhere\n" +
			"22/tcp (v6)                ALLOW IN    Anywhere (v6)\n"))}, []string{"ssh-allowed-except IPv4"}},
		{"enabled at boot, not running: the saved rules at start", ufw.State{Installed: true, Enabled: true, IPv6: true,
			Incoming: ufw.PolicyDeny, Outgoing: ufw.PolicyAllow, Status: ufw.ParseStatus([]byte("Status: inactive\n")),
			Rules: []ufw.Rule{rule(ufw.IPv4, "allow", "22", "", "", "")}}, []string{"ssh-not-allowed-at-start IPv6"}},
		{"no IPv6 in the kernel: IPv6 is unreachable", ufw.State{Installed: true, NoKernelIPv6: true, Status: ufw.ParseStatus([]byte(head +
			"22/tcp                     ALLOW IN    Anywhere\n"))}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, fs := ufw.Check(tc.st, needs)
			var got []string
			for _, f := range fs {
				got = append(got, fmt.Sprintf("%s %s", f.Kind, f.Coverage.Need.Family))
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("findings %q, want %q", got, tc.want)
			}
			if !tc.st.Installed && cs != nil {
				t.Errorf("coverage %v, want none", cs)
			}
		})
	}
}

// TestStrings: the stable codes, and an out-of-range value is "unknown",
// never a panic (a value decoded from a journal).
func TestStrings(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{ufw.Excepted.String(), "excepted"},
		{ufw.Unreachable.String(), "unreachable"},
		{ufw.Verdict(99).String(), "unknown"},
		{ufw.FindingExcepted.String(), "ssh-allowed-except"},
		{ufw.FindingAtStart.String(), "ssh-not-allowed-at-start"},
		{ufw.FindingWidened.String(), "ssh-new-port-wider"},
		{ufw.FindingKind(99).String(), "unknown"},
		{ufw.StepAllow.String(), "allow"},
		{ufw.StepEnable.String(), "enable"},
		{ufw.StepKind(99).String(), "unknown"},
	} {
		if tc.got != tc.want {
			t.Errorf("String() = %q, want %q", tc.got, tc.want)
		}
	}
}
