package pve_test

import (
	"context"
	"errors"
	"io/fs"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/ufw"
)

func dir(kw string, args ...string) migrate.Directive {
	return migrate.Directive{Keyword: kw, Args: args}
}

func lines(ds []migrate.Directive) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Keyword+" "+strings.Join(d.Args, " "))
	}
	return out
}

// groups is root's groups as RootGroups found them.
func groups(names ...string) pve.Groups { return pve.Groups{Names: names, Known: true} }

// TestPlanSSHFixture: PlanSSH's output for the fixture's settings is the
// fixture's 00-hardn.conf, line for line.
func TestPlanSSHFixture(t *testing.T) {
	desired := []migrate.Directive{dir("PasswordAuthentication", "no"), dir("AllowUsers", "alice")}
	out, found, err := pve.PlanSSH(pveUp, desired, groups("root"))
	if err != nil {
		t.Fatal(err)
	}
	file := strings.Split(strings.TrimSpace(string(readFixture(t, "root/etc/ssh/sshd_config.d/00-hardn.conf"))), "\n")[1:]
	if got := lines(out); !slices.Equal(got, file) {
		t.Errorf("PlanSSH = %q, want the fixture's %q", got, file)
	}
	kinds := []pve.FindingKind{}
	for _, f := range found {
		kinds = append(kinds, f.Kind)
	}
	if !slices.Equal(kinds, []pve.FindingKind{pve.FindingRootKeyOnly, pve.FindingRootAdded}) || !strings.Contains(found[0].Detail, "root: key-only (required by Proxmox clustering)") {
		t.Errorf("findings = %+v", found)
	}
	if desired[1].Args[0] != "alice" || len(desired[1].Args) != 1 {
		t.Errorf("PlanSSH changed its input: %+v", desired)
	}
}

func TestPlanSSH(t *testing.T) {
	keyOnly := dir("PermitRootLogin", "prohibit-password")
	tests := []struct {
		name    string
		p       detect.Proxmox
		in      []migrate.Directive
		groups  pve.Groups
		want    []string // nil: the input as it is
		finding []pve.FindingKind
		err     error
	}{
		{name: "not Proxmox VE: unchanged", p: debian, in: []migrate.Directive{dir("PermitRootLogin", "no"), dir("Port", "2230")}},
		{name: "unknown", p: unknown, in: []migrate.Directive{dir("Port", "22")}, err: pve.ErrProxmoxUnknown},
		{name: "no becomes prohibit-password", p: pveUp, in: []migrate.Directive{dir("PermitRootLogin", "no")},
			want: []string{"PermitRootLogin prohibit-password"}, finding: []pve.FindingKind{pve.FindingRootKeyOnly}},
		{name: "yes too", p: pveUp, in: []migrate.Directive{dir("permitrootlogin", "yes")},
			want: []string{"permitrootlogin prohibit-password"}, finding: []pve.FindingKind{pve.FindingRootKeyOnly}},
		{name: "without-password is the same value", p: pveUp, in: []migrate.Directive{dir("PermitRootLogin", "without-password")},
			want: []string{"PermitRootLogin prohibit-password"}},
		{name: "added when missing", p: pveUp, in: []migrate.Directive{dir("PasswordAuthentication", "no")},
			want: []string{"PasswordAuthentication no", "PermitRootLogin prohibit-password"}, finding: []pve.FindingKind{pve.FindingRootKeyOnly}},
		{name: "Port 22 kept", p: pveUp, in: []migrate.Directive{dir("Port", "22"), keyOnly}},
		{name: "Port 2230 refused", p: pveUp, in: []migrate.Directive{dir("Port", "2230")}, err: pve.ErrPortChange},
		{name: "Port 022 is 22", p: pveUp, in: []migrate.Directive{dir("Port", "022"), dir("ListenAddress", "0.0.0.0:022"), dir("ListenAddress", "[::]:22"), keyOnly}},
		{name: "a second Port refused", p: pveUp, in: []migrate.Directive{dir("Port", "22"), dir("Port", "2230")}, err: pve.ErrPortChange},
		{name: "a port name refused", p: pveUp, in: []migrate.Directive{dir("Port", "ssh")}, err: pve.ErrPortChange},
		{name: "ListenAddress with port 2230", p: pveUp, in: []migrate.Directive{dir("ListenAddress", "0.0.0.0:2230")}, err: pve.ErrPortChange},
		{name: "ListenAddress [::]:2230", p: pveUp, in: []migrate.Directive{dir("ListenAddress", "[::]:2230")}, err: pve.ErrPortChange},
		{name: "ListenAddress, every address", p: pveUp, in: []migrate.Directive{dir("ListenAddress", "0.0.0.0"), dir("ListenAddress", "::"), keyOnly}},
		{name: "ListenAddress on one address", p: pveUp, in: []migrate.Directive{dir("ListenAddress", "192.0.2.10"), dir("ListenAddress", "[::]:22"), keyOnly},
			finding: []pve.FindingKind{pve.FindingRootUnreachable}},
		{name: "ListenAddress IPv4 only", p: pveUp, in: []migrate.Directive{dir("ListenAddress", "0.0.0.0"), keyOnly}, finding: []pve.FindingKind{pve.FindingRootUnreachable}},
		{name: "ListenAddress in a routing domain", p: pveUp, in: []migrate.Directive{dir("ListenAddress", "0.0.0.0", "rdomain", "mgmt"), dir("ListenAddress", "::"), keyOnly},
			finding: []pve.FindingKind{pve.FindingRootUnreachable}},
		{name: "ListenAddress 127.0.0.1 refused", p: pveUp, in: []migrate.Directive{dir("ListenAddress", "127.0.0.1")}, err: pve.ErrRootExcluded},
		{name: "ListenAddress loopback only refused", p: pveUp, in: []migrate.Directive{dir("ListenAddress", "[::1]:22"), dir("ListenAddress", "localhost")}, err: pve.ErrRootExcluded},
		{name: "AddressFamily inet6", p: pveUp, in: []migrate.Directive{dir("AddressFamily", "inet6"), keyOnly}, finding: []pve.FindingKind{pve.FindingRootUnreachable}},
		{name: "AddressFamily any", p: pveUp, in: []migrate.Directive{dir("AddressFamily", "any"), keyOnly}},
		{name: "AllowUsers without root", p: pveUp, in: []migrate.Directive{dir("AllowUsers", "alice", "bob"), keyOnly},
			want: []string{"AllowUsers alice bob root", "PermitRootLogin prohibit-password"}, finding: []pve.FindingKind{pve.FindingRootAdded}},
		{name: "AllowUsers root from one network only", p: pveUp, in: []migrate.Directive{dir("AllowUsers", "alice", "root@192.0.2.0/24"), keyOnly},
			want: []string{"AllowUsers alice root@192.0.2.0/24 root", "PermitRootLogin prohibit-password"}, finding: []pve.FindingKind{pve.FindingRootAdded}},
		{name: "AllowUsers with a pattern for root", p: pveUp, in: []migrate.Directive{dir("AllowUsers", "alice"), dir("AllowUsers", "r??t"), keyOnly}},
		{name: "DenyUsers root", p: pveUp, in: []migrate.Directive{dir("DenyUsers", "bob", "root")}, err: pve.ErrRootExcluded},
		{name: "DenyUsers *@host", p: pveUp, in: []migrate.Directive{dir("DenyUsers", "*@192.0.2.99")}, err: pve.ErrRootExcluded},
		{name: "DenyUsers bob", p: pveUp, in: []migrate.Directive{dir("DenyUsers", "bob"), keyOnly}},
		{name: "AllowGroups without root's", p: pveUp, groups: groups("root"), in: []migrate.Directive{dir("AllowGroups", "sshusers")}, err: pve.ErrRootExcluded},
		{name: "AllowGroups with root's", p: pveUp, groups: groups("root", "sshusers"), in: []migrate.Directive{dir("AllowGroups", "sshusers"), keyOnly}},
		{name: "AllowGroups root, groups unknown", p: pveUp, in: []migrate.Directive{dir("AllowGroups", "root"), keyOnly}},
		{name: "DenyGroups, root's groups unknown", p: pveUp, in: []migrate.Directive{dir("DenyGroups", "nossh")}, err: pve.ErrRootExcluded},
		{name: "DenyGroups matching root's", p: pveUp, groups: groups("root", "nossh"), in: []migrate.Directive{dir("DenyGroups", "nossh")}, err: pve.ErrRootExcluded},
		{name: "DenyGroups not root's", p: pveUp, groups: groups("root"), in: []migrate.Directive{dir("DenyGroups", "nossh"), keyOnly}},
		{name: "DenyGroups, root in no group", p: pveUp, groups: groups(), in: []migrate.Directive{dir("DenyGroups", "nossh"), keyOnly}},
		{name: "a Match line", p: pveUp, in: []migrate.Directive{dir("Match", "User", "bob"), dir("PermitRootLogin", "yes")}, err: fs.ErrInvalid},
		{name: "an Include line", p: pveUp, in: []migrate.Directive{dir("Include", "/etc/ssh/extra.conf")}, err: fs.ErrInvalid},
		{name: "PubkeyAuthentication no", p: pveUp, in: []migrate.Directive{dir("PubkeyAuthentication", "no")}, err: pve.ErrRootExcluded},
		{name: "AuthenticationMethods key and password", p: pveUp, in: []migrate.Directive{dir("AuthenticationMethods", "publickey,password")}, err: pve.ErrRootExcluded},
		{name: "AuthenticationMethods two keys", p: pveUp, in: []migrate.Directive{dir("AuthenticationMethods", "publickey,publickey")}, err: pve.ErrRootExcluded},
		{name: "AuthenticationMethods with a key-only list", p: pveUp, in: []migrate.Directive{dir("AuthenticationMethods", "publickey,password", "publickey"), keyOnly}},
		{name: "AuthorizedKeysFile without root's (V5)", p: pveUp, in: []migrate.Directive{dir("AuthorizedKeysFile", ".ssh_%u")}, err: pve.ErrRootExcluded},
		{name: "AuthorizedKeysFile with root's", p: pveUp, in: []migrate.Directive{dir("AuthorizedKeysFile", ".ssh/authorized_keys", ".ssh_%u/authorized_keys"), keyOnly}},
		{name: "PubkeyAcceptedAlgorithms without RSA", p: pveUp, in: []migrate.Directive{dir("PubkeyAcceptedAlgorithms", "ssh-ed25519,ecdsa-sha2-nistp256")}, err: pve.ErrRootExcluded},
		{name: "PubkeyAcceptedKeyTypes, its alias", p: pveUp, in: []migrate.Directive{dir("PubkeyAcceptedKeyTypes", "ssh-ed25519")}, err: pve.ErrRootExcluded},
		{name: "PubkeyAcceptedAlgorithms without RSA, by negation", p: pveUp, in: []migrate.Directive{dir("PubkeyAcceptedAlgorithms", "rsa-sha2-*,!rsa-sha2-512,!rsa-sha2-256")}, err: pve.ErrRootExcluded},
		{name: "PubkeyAcceptedAlgorithms removing RSA", p: pveUp, in: []migrate.Directive{dir("PubkeyAcceptedAlgorithms", "-rsa-sha2-*")}, err: pve.ErrRootExcluded},
		{name: "PubkeyAcceptedAlgorithms removing one RSA", p: pveUp, in: []migrate.Directive{dir("PubkeyAcceptedAlgorithms", "-rsa-sha2-256,ssh-ed25519"), keyOnly}},
		{name: "PubkeyAcceptedAlgorithms adding", p: pveUp, in: []migrate.Directive{dir("PubkeyAcceptedAlgorithms", "+ssh-ed25519"), keyOnly}},
		{name: "PubkeyAcceptedAlgorithms first", p: pveUp, in: []migrate.Directive{dir("PubkeyAcceptedAlgorithms", "^ssh-ed25519"), keyOnly}},
		{name: "PubkeyAcceptedAlgorithms with a pattern for RSA", p: pveUp, in: []migrate.Directive{dir("PubkeyAcceptedAlgorithms", "ssh-ed25519,rsa-sha2-5*"), keyOnly}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, found, err := pve.PlanSSH(tt.p, tt.in, tt.groups)
			if !errors.Is(err, tt.err) || (tt.err == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err != nil {
				return
			}
			want := tt.want
			if want == nil {
				want = lines(tt.in)
			}
			if got := lines(out); !slices.Equal(got, want) {
				t.Errorf("PlanSSH = %q, want %q", got, want)
			}
			var kinds []pve.FindingKind
			for _, f := range found {
				kinds = append(kinds, f.Kind)
			}
			if !slices.Equal(kinds, tt.finding) {
				t.Errorf("findings = %+v, want %v", found, tt.finding)
			}
		})
	}
}

func TestCheckPort(t *testing.T) {
	tests := []struct {
		p     detect.Proxmox
		ports []uint16
		err   error
	}{
		{pveUp, nil, nil},
		{pveUp, []uint16{22}, nil},
		{pveUp, []uint16{2230}, pve.ErrPortChange},
		{pveUp, []uint16{22, 2230}, pve.ErrPortChange},
		{unknown, []uint16{22}, nil},
		{unknown, []uint16{2230}, pve.ErrProxmoxUnknown},
		{debian, []uint16{2230}, nil},
	}
	for _, tt := range tests {
		if err := pve.CheckPort(tt.p, tt.ports); !errors.Is(err, tt.err) || (tt.err == nil) != (err == nil) {
			t.Errorf("CheckPort(%v, %v) = %v, want %v", tt.p.Installed, tt.ports, err, tt.err)
		}
	}
	if err := pve.CheckPort(pveUp, []uint16{2230}); !strings.Contains(err.Error(), "cluster SSH and pve-firewall's management rule use port 22") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// service is detect's SSH fact for the fixture: ssh.service, as on Proxmox
// VE 9.
var service = detect.SSH{Mode: detect.SSHService, Service: detect.UnitState{Enabled: "enabled", Active: "active"}}

// input is CheckSSH's input for the fixture: sshd.Evaluate over its files
// and sshd -T output, as ReadEffective gives them on an ssh.service host,
// and root in the group root. Without hardn's drop-in, sshd -T prints
// Proxmox VE's own values.
func input(t *testing.T, withHardn bool) pve.SSHInput {
	t.Helper()
	in := sshd.EffectiveInputs{
		Effective: readFixture(t, "sshd-T.out"),
		SSH:       service,
		Main:      readFixture(t, "root/etc/ssh/sshd_config"),
	}
	if withHardn {
		in.DropIns = []sshd.File{{Path: sshd.HardnDropIn, Data: readFixture(t, "root/etc/ssh/sshd_config.d/00-hardn.conf")}}
	} else {
		in.Effective = []byte(strings.NewReplacer("permitrootlogin without-password", "permitrootlogin yes",
			"passwordauthentication no", "passwordauthentication yes", "allowusers alice\nallowusers root\n", "").Replace(string(in.Effective)))
	}
	e, _ := sshd.Evaluate(in)
	return pve.SSHInput{Effective: e, Main: in.Main, DropIns: in.DropIns, Root: groups("root")}
}

func check(t *testing.T, in pve.SSHInput) pve.SSHCheck {
	t.Helper()
	c, err := pve.CheckSSH(pveUp, in)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestCheckSSHFixture: on the fixture, root is key-only and reachable on
// 22, and Proxmox VE's own PermitRootLogin yes is reported as expected.
func TestCheckSSHFixture(t *testing.T) {
	c := check(t, input(t, true))
	if c.RootKeyOnly != detect.Yes || c.RootByKey != detect.Yes || c.Port22 != detect.Yes {
		t.Errorf("CheckSSH = %+v", c)
	}
	want := []pve.Finding{{Kind: pve.FindingProxmoxRootLogin, File: sshd.MainConfig, Line: 5,
		Detail: "Proxmox VE's own PermitRootLogin yes, shadowed by " + sshd.HardnDropIn + ": expected, not drift"}}
	if !slices.Equal(c.Findings, want) {
		t.Errorf("findings = %+v, want %+v", c.Findings, want)
	}
	// Only a shadowed yes in sshd_config is Proxmox VE's own line.
	in := input(t, true)
	pveLine := in.Effective.Conflicts[0]
	other := pveLine
	other.Other = sshd.Origin{File: sshd.DropInDir + "/50-cloud-init.conf", Line: 1, Text: "PermitRootLogin yes"}
	no := pveLine
	no.Other.Text = "PermitRootLogin no"
	over := pveLine
	over.Kind = sshd.FindingOverridden
	for _, cf := range []sshd.Conflict{other, no, over} {
		in.Effective.Conflicts = []sshd.Conflict{cf}
		if c := check(t, in); slices.ContainsFunc(c.Findings, func(f pve.Finding) bool { return f.Kind == pve.FindingProxmoxRootLogin }) {
			t.Errorf("%+v read as Proxmox VE's own line", cf)
		}
	}
	// Before hardn: Proxmox VE's yes is the value, which is not key-only.
	c = check(t, input(t, false))
	if c.RootKeyOnly != detect.No || c.RootByKey != detect.Yes ||
		!slices.Equal(c.Findings, []pve.Finding{{Kind: pve.FindingNotKeyOnly, Detail: "PermitRootLogin yes"}}) {
		t.Errorf("stock: CheckSSH = %+v", c)
	}
}

// TestCheckSSHApplies: CheckSSH decides nothing off Proxmox VE.
func TestCheckSSHApplies(t *testing.T) {
	for _, tt := range []struct {
		p   detect.Proxmox
		err error
	}{{debian, pve.ErrNotApplicable}, {unknown, pve.ErrProxmoxUnknown}} {
		if c, err := pve.CheckSSH(tt.p, input(t, true)); !errors.Is(err, tt.err) || len(c.Findings) != 0 {
			t.Errorf("CheckSSH(%v) = %+v, %v; want %v", tt.p.Installed, c, err, tt.err)
		}
	}
}

func TestCheckSSH(t *testing.T) {
	known := func(v string) sshd.Setting { return sshd.Setting{Value: v, Known: true} }
	set := func(vs ...string) sshd.Set { return sshd.Set{Values: vs, Known: true} }
	listen := func(addrs ...string) func(*pve.SSHInput) {
		return func(in *pve.SSHInput) {
			var ls []sshd.Listener
			for _, a := range addrs {
				ap := netip.MustParseAddrPort(a)
				ls = append(ls, sshd.Listener{Addr: ap.Addr(), Port: ap.Port()})
			}
			in.Effective.Listeners.SSHD, in.Effective.Listeners.All = ls, ls
		}
	}
	effective := func(f func(e *sshd.Effective)) func(*pve.SSHInput) {
		return func(in *pve.SSHInput) { f(&in.Effective) }
	}
	// block adds a Match block at the end of sshd_config (line 13 on).
	block := func(lines ...string) func(*pve.SSHInput) {
		return func(in *pve.SSHInput) {
			in.Main = append(in.Main, strings.Join(lines, "\n\t")+"\n"...)
		}
	}
	both := func(fs ...func(*pve.SSHInput)) func(*pve.SSHInput) {
		return func(in *pve.SSHInput) {
			for _, f := range fs {
				f(in)
			}
		}
	}
	tests := []struct {
		name           string
		edit           func(in *pve.SSHInput)
		keyOnly, byKey detect.Tri
		port22         detect.Tri
		reason         string // in a finding
	}{
		{name: "the fixture", edit: func(*pve.SSHInput) {}, keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.Yes},
		{name: "PermitRootLogin no", edit: effective(func(e *sshd.Effective) { e.PermitRootLogin = known("no") }), keyOnly: detect.No, byKey: detect.No, port22: detect.Yes, reason: "PermitRootLogin no"},
		{name: "forced-commands-only", edit: effective(func(e *sshd.Effective) { e.PermitRootLogin = known("forced-commands-only") }), keyOnly: detect.No, byKey: detect.No, port22: detect.Yes},
		{name: "PermitRootLogin unknown", edit: effective(func(e *sshd.Effective) { e.PermitRootLogin = sshd.Setting{} }), keyOnly: detect.Unknown, byKey: detect.Unknown, port22: detect.Yes},
		{name: "PubkeyAuthentication no", edit: effective(func(e *sshd.Effective) { e.PubkeyAuthentication = known("no") }), keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "AllowUsers alice", edit: effective(func(e *sshd.Effective) { e.AllowUsers = set("alice") }), keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes, reason: "does not match root"},
		{name: "AllowUsers root from one network", edit: effective(func(e *sshd.Effective) { e.AllowUsers = set("alice", "root@192.0.2.0/24") }), keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "AllowUsers empty", edit: effective(func(e *sshd.Effective) { e.AllowUsers = set() }), keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.Yes},
		{name: "DenyUsers root", edit: effective(func(e *sshd.Effective) { e.DenyUsers = set("r*") }), keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "DenyUsers from a host", edit: effective(func(e *sshd.Effective) { e.DenyUsers = set("*@192.0.2.99") }), keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "DenyUsers unknown", edit: effective(func(e *sshd.Effective) { e.DenyUsers = sshd.Set{} }), keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "AllowUsers unknown", edit: effective(func(e *sshd.Effective) { e.AllowUsers = sshd.Set{} }), keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "AllowGroups unknown", edit: effective(func(e *sshd.Effective) { e.AllowGroups = sshd.Set{} }), keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "DenyGroups unknown", edit: effective(func(e *sshd.Effective) { e.DenyGroups = sshd.Set{} }), keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "AllowGroups, groups unknown", edit: func(in *pve.SSHInput) { in.Effective.AllowGroups, in.Root = set("sshusers"), pve.Groups{} }, keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "AllowGroups not root's", edit: effective(func(e *sshd.Effective) { e.AllowGroups = set("sshusers") }), keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "AllowGroups root's", edit: func(in *pve.SSHInput) {
			in.Effective.AllowGroups, in.Root = set("sshusers"), groups("root", "sshusers")
		}, keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.Yes},
		{name: "DenyGroups root's", edit: func(in *pve.SSHInput) { in.Effective.DenyGroups, in.Root = set("wheel"), groups("root", "wheel") }, keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "DenyGroups, groups unknown", edit: func(in *pve.SSHInput) { in.Effective.DenyGroups, in.Root = set("wheel"), pve.Groups{} }, keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "AuthenticationMethods two factors", edit: effective(func(e *sshd.Effective) { e.Keywords["authenticationmethods"] = []string{"publickey,password"} }), keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "AuthenticationMethods two keys", edit: effective(func(e *sshd.Effective) { e.Keywords["authenticationmethods"] = []string{"publickey,publickey"} }), keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "AuthenticationMethods missing", edit: effective(func(e *sshd.Effective) { delete(e.Keywords, "authenticationmethods") }), keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "AuthorizedKeysFile .ssh_%u (V5)", edit: effective(func(e *sshd.Effective) { e.Keywords["authorizedkeysfile"] = []string{".ssh_%u"} }), keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "AuthorizedKeysCommand may supply keys", edit: effective(func(e *sshd.Effective) {
			e.Keywords["authorizedkeysfile"] = []string{"none"}
			e.Keywords["authorizedkeyscommand"] = []string{"/usr/sbin/keys %u"}
		}), keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "PubkeyAcceptedAlgorithms without RSA", edit: effective(func(e *sshd.Effective) {
			e.Keywords["pubkeyacceptedalgorithms"] = []string{"ssh-ed25519,ecdsa-sha2-nistp256"}
		}),
			keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes, reason: "neither rsa-sha2-512 nor rsa-sha2-256"},
		{name: "PubkeyAcceptedAlgorithms missing", edit: effective(func(e *sshd.Effective) { delete(e.Keywords, "pubkeyacceptedalgorithms") }), keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "loopback only", edit: listen("127.0.0.1:22", "[::1]:22"), keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes, reason: "loopback only"},
		{name: "one address", edit: listen("192.0.2.10:22", "[::]:22"), keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes, reason: "192.0.2.10:22"},
		{name: "IPv4 only (AddressFamily inet)", edit: listen("0.0.0.0:22"), keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "listeners not established", edit: effective(func(e *sshd.Effective) { e.Listeners.Known = false }), keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Unknown},
		{name: "Port 2230", edit: effective(func(e *sshd.Effective) { e.Port = set("22", "2230") }), keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.No, reason: "Port 2230"},
		{name: "ListenAddress on 2230", edit: effective(func(e *sshd.Effective) { e.Listeners.SSHD[0].Port = 2230 }), keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.No, reason: "ListenAddress [::]:2230"},
		{name: "Port unknown", edit: effective(func(e *sshd.Effective) { e.Port = sshd.Set{} }), keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.Unknown},
		// Match blocks, from the files: sshd -T and its probes show neither
		// these values for root's connections from the nodes.
		{name: "a Match block allows root's password", edit: block("Match Address 192.0.2.0/24", "PermitRootLogin yes"),
			keyOnly: detect.No, byKey: detect.Yes, port22: detect.Yes, reason: `under "Match Address 192.0.2.0/24" (/etc/ssh/sshd_config line 13): PermitRootLogin yes`},
		{name: "a Match block the probe cannot target", edit: block("Match Group root", "PermitRootLogin no"),
			keyOnly: detect.No, byKey: detect.Unknown, port22: detect.Yes, reason: "Match Group root"},
		{name: "a Match block asks root for a password too", edit: block("Match User root", "AuthenticationMethods publickey,password"),
			keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes, reason: "no list is any or publickey alone"},
		{name: "a Match block without RSA", edit: block("Match Address 192.0.2.0/24", "PubkeyAcceptedAlgorithms ssh-ed25519"), keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "a Match block in a drop-in", edit: func(in *pve.SSHInput) {
			in.DropIns = append(in.DropIns, sshd.File{Path: sshd.DropInDir + "/50-root.conf", Data: []byte("Match User root\n\tPubkeyAuthentication no\n")})
		}, keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes, reason: "50-root.conf"},
		{name: "a Match block that keeps root's access", edit: block("Match User root", "PubkeyAuthentication yes", "PermitRootLogin without-password", "AllowUsers root alice"),
			keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.Yes},
		{name: "a Match block on AcceptEnv", edit: block("Match User bob", "AcceptEnv LANG"), keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.Yes},
		{name: "a Match block lets root in where the global value does not", edit: both(
			effective(func(e *sshd.Effective) { e.PermitRootLogin = known("no") }), block("Match Address 192.0.2.0/24", "PermitRootLogin prohibit-password")),
			keyOnly: detect.No, byKey: detect.Unknown, port22: detect.Yes, reason: "may let root in where the global value does not"},
		{name: "a Match block's AuthorizedKeysCommand", edit: both(
			effective(func(e *sshd.Effective) { e.Keywords["authorizedkeysfile"] = []string{".ssh_%u"} }), block("Match User root", "AuthorizedKeysCommand /usr/sbin/keys %u")),
			keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "a Match block's AuthorizedKeysCommand none", edit: both(
			effective(func(e *sshd.Effective) { e.Keywords["authorizedkeysfile"] = []string{".ssh_%u"} }), block("Match User root", "AuthorizedKeysCommand none")),
			keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "the files not read", edit: func(in *pve.SSHInput) { in.Main, in.DropIns = nil, nil }, keyOnly: detect.Unknown, byKey: detect.Unknown, port22: detect.Yes, reason: "Match blocks"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := input(t, true)
			tt.edit(&in)
			c := check(t, in)
			if c.RootKeyOnly != tt.keyOnly || c.RootByKey != tt.byKey || c.Port22 != tt.port22 {
				t.Errorf("CheckSSH = key-only %v, by key %v, port 22 %v; want %v %v %v (%+v)", c.RootKeyOnly, c.RootByKey, c.Port22, tt.keyOnly, tt.byKey, tt.port22, c.Findings)
			}
			if has := slices.ContainsFunc(c.Findings, func(f pve.Finding) bool { return f.Kind == pve.FindingRootUnreachable }); has != (tt.byKey != detect.Yes) {
				t.Errorf("a root-unreachable finding is %v for by key %v: %+v", has, tt.byKey, c.Findings)
			}
			if has := slices.ContainsFunc(c.Findings, func(f pve.Finding) bool { return f.Kind == pve.FindingNotKeyOnly }); has != (tt.keyOnly == detect.No) {
				t.Errorf("a not-key-only finding is %v for key-only %v: %+v", has, tt.keyOnly, c.Findings)
			}
			if tt.reason != "" && !slices.ContainsFunc(c.Findings, func(f pve.Finding) bool { return strings.Contains(f.Detail, tt.reason) }) {
				t.Errorf("no finding says %q: %+v", tt.reason, c.Findings)
			}
		})
	}
}

// TestCheckSSHSocket: on a socket-activated host ssh.socket's listeners
// decide (on Proxmox VE the socket is opt-in, with no generator): a socket
// on 2230 while sshd -T says 22 is not established, never "port 22".
func TestCheckSSHSocket(t *testing.T) {
	socketHost := detect.SSH{Mode: detect.SSHSocket, Socket: detect.UnitState{Enabled: "enabled", Active: "active"}}
	for _, tt := range []struct {
		listen        string
		byKey, port22 detect.Tri
	}{
		{"Listen=[::]:22 (Stream)\n", detect.Yes, detect.Yes},
		{"Listen=[::]:2230 (Stream)\n", detect.Unknown, detect.Unknown},
		{"Listen=0.0.0.0:22 (Stream)\n", detect.Unknown, detect.Yes},
	} {
		in := input(t, true)
		in.Effective, _ = sshd.Evaluate(sshd.EffectiveInputs{Effective: readFixture(t, "sshd-T.out"), SSH: socketHost, Socket: []byte(tt.listen), Main: in.Main, DropIns: in.DropIns})
		if c := check(t, in); c.RootByKey != tt.byKey || c.Port22 != tt.port22 {
			t.Errorf("%q: by key %v, port 22 %v; want %v %v (%+v)", tt.listen, c.RootByKey, c.Port22, tt.byKey, tt.port22, c.Findings)
		}
	}
}

func TestRootGroups(t *testing.T) {
	root := host.User{Name: "root", UID: 0, GID: 0, HomeDir: "/root"}
	tests := []struct {
		name  string
		user  bool
		group string // "": no /etc/group
		want  pve.Groups
		err   error
	}{
		{name: "groups", user: true, group: "root:x:0:\nwww-data:x:33:\nsshusers:x:1001:alice,root\nnossh:x:1002:bob\n", want: groups("root", "sshusers")},
		{name: "the primary group lists root too", user: true, group: "root:x:0:root\n", want: groups("root")},
		{name: "no root", group: "root:x:0:\n", err: host.ErrUnknownUser},
		{name: "no group file", user: true, err: fs.ErrNotExist},
		{name: "no line for root's group", user: true, group: "sshusers:x:1001:root\n", err: fs.ErrNotExist},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			if tt.user {
				h.AddUser(root)
			}
			if tt.group != "" {
				h.AddFile(pve.GroupFile, []byte(tt.group), 0o644, 0, 0)
			}
			g, err := pve.RootGroups(context.Background(), h)
			if !errors.Is(err, tt.err) || (tt.err == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if !slices.Equal(g.Names, tt.want.Names) || g.Known != tt.want.Known {
				t.Errorf("RootGroups = %+v, want %+v", g, tt.want)
			}
			checkReadOnly(t, h)
		})
	}
}

func TestCheckAccount(t *testing.T) {
	for _, op := range []pve.AccountOp{pve.OpLockPassword, pve.OpDeletePassword, pve.OpExpireAccount} {
		if err := pve.CheckAccount(pveUp, "root", op); !errors.Is(err, pve.ErrRootPassword) {
			t.Errorf("%v root on Proxmox VE: %v", op, err)
		}
		if err := pve.CheckAccount(pveDown, "root", op); !errors.Is(err, pve.ErrRootPassword) {
			t.Errorf("%v root, pmxcfs down: %v", op, err)
		}
		if err := pve.CheckAccount(unknown, "root", op); !errors.Is(err, pve.ErrProxmoxUnknown) {
			t.Errorf("%v root, unknown: %v", op, err)
		}
		for _, tt := range []struct {
			p    detect.Proxmox
			user string
		}{{pveUp, "alice"}, {unknown, "bob"}, {debian, "root"}} {
			if err := pve.CheckAccount(tt.p, tt.user, op); err != nil {
				t.Errorf("%v %s: %v", op, tt.user, err)
			}
		}
	}
}

// TestNoUFW holds the brief's rule from the ufw unit's side: on Proxmox VE
// UFW is not applicable, and an unknown Proxmox fact is not "not Proxmox".
func TestNoUFW(t *testing.T) {
	if err := ufw.Applies(pveUp); !errors.Is(err, ufw.ErrNotApplicable) {
		t.Errorf("ufw.Applies(Proxmox VE) = %v", err)
	}
	if err := ufw.Applies(unknown); !errors.Is(err, ufw.ErrProxmoxUnknown) {
		t.Errorf("ufw.Applies(unknown) = %v", err)
	}
}

func TestApplies(t *testing.T) {
	for _, tt := range []struct {
		p   detect.Proxmox
		ok  bool
		err error
	}{{pveUp, true, nil}, {pveDown, true, nil}, {debian, false, nil}, {unknown, false, pve.ErrProxmoxUnknown}, {detect.Proxmox{}, false, pve.ErrProxmoxUnknown}} {
		ok, err := pve.Applies(tt.p)
		if ok != tt.ok || !errors.Is(err, tt.err) || (tt.err == nil) != (err == nil) {
			t.Errorf("Applies(%+v) = %v, %v", tt.p, ok, err)
		}
	}
	if _, err := pve.Applies(unknown); !errors.Is(err, detect.ErrConflict) {
		t.Errorf("Applies does not wrap detect's reason: %v", err)
	}
}

// TestFindingCodes pins every FindingKind's code, up to the last kind.
func TestFindingCodes(t *testing.T) {
	want := map[pve.FindingKind]string{
		pve.FindingRootKeyOnly: "root-key-only", pve.FindingRootAdded: "root-allowed", pve.FindingProxmoxRootLogin: "proxmox-root-login",
		pve.FindingPortNot22: "port-not-22", pve.FindingRootUnreachable: "root-unreachable", pve.FindingClusterWide: "cluster-wide-keys",
		pve.FindingRuleMayBlock: "rule-may-block", pve.FindingBlacklist: "blacklist", pve.FindingNotModelled: "firewall-not-modelled",
		pve.FindingNotKeyOnly: "root-not-key-only",
	}
	for k := pve.FindingKind(1); k < pve.FindingEnd; k++ {
		if code, ok := want[k]; !ok || k.String() != code {
			t.Errorf("%d.String() = %q, want %q (a new kind needs its code here)", k, k.String(), code)
		}
	}
	if len(want) != int(pve.FindingEnd)-1 || pve.FindingKind(0).String() != "FindingKind(0)" {
		t.Errorf("codes changed")
	}
}
