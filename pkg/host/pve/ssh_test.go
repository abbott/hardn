package pve_test

import (
	"context"
	"errors"
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

// TestPlanSSHFixture: PlanSSH's output for the fixture's settings is the
// fixture's 00-hardn.conf, line for line.
func TestPlanSSHFixture(t *testing.T) {
	desired := []migrate.Directive{dir("PasswordAuthentication", "no"), dir("AllowUsers", "alice")}
	out, found, err := pve.PlanSSH(pveUp, desired, []string{"root"})
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
	tests := []struct {
		name    string
		p       detect.Proxmox
		in      []migrate.Directive
		groups  []string
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
		{name: "Port 22 kept", p: pveUp, in: []migrate.Directive{dir("Port", "22"), dir("PermitRootLogin", "prohibit-password")},
			want: []string{"Port 22", "PermitRootLogin prohibit-password"}},
		{name: "Port 2230 refused", p: pveUp, in: []migrate.Directive{dir("Port", "2230")}, err: pve.ErrPortChange},
		{name: "Port 022 is 22", p: pveUp, in: []migrate.Directive{dir("Port", "022"), dir("ListenAddress", "0.0.0.0:022"), dir("PermitRootLogin", "prohibit-password")},
			want: []string{"Port 022", "ListenAddress 0.0.0.0:022", "PermitRootLogin prohibit-password"}},
		{name: "a second Port refused", p: pveUp, in: []migrate.Directive{dir("Port", "22"), dir("Port", "2230")}, err: pve.ErrPortChange},
		{name: "a port name refused", p: pveUp, in: []migrate.Directive{dir("Port", "ssh")}, err: pve.ErrPortChange},
		{name: "ListenAddress with port 2230", p: pveUp, in: []migrate.Directive{dir("ListenAddress", "0.0.0.0:2230")}, err: pve.ErrPortChange},
		{name: "ListenAddress [::]:2230", p: pveUp, in: []migrate.Directive{dir("ListenAddress", "[::]:2230")}, err: pve.ErrPortChange},
		{name: "ListenAddress without a port", p: pveUp, in: []migrate.Directive{dir("ListenAddress", "192.0.2.10"), dir("ListenAddress", "::"), dir("ListenAddress", "[::]:22"), dir("PermitRootLogin", "prohibit-password")},
			want: []string{"ListenAddress 192.0.2.10", "ListenAddress ::", "ListenAddress [::]:22", "PermitRootLogin prohibit-password"}},
		{name: "AllowUsers without root", p: pveUp, in: []migrate.Directive{dir("AllowUsers", "alice", "bob"), dir("PermitRootLogin", "prohibit-password")},
			want: []string{"AllowUsers alice bob root", "PermitRootLogin prohibit-password"}, finding: []pve.FindingKind{pve.FindingRootAdded}},
		{name: "AllowUsers root from one network only", p: pveUp, in: []migrate.Directive{dir("AllowUsers", "alice", "root@192.0.2.0/24"), dir("PermitRootLogin", "prohibit-password")},
			want: []string{"AllowUsers alice root@192.0.2.0/24 root", "PermitRootLogin prohibit-password"}, finding: []pve.FindingKind{pve.FindingRootAdded}},
		{name: "AllowUsers with a pattern for root", p: pveUp, in: []migrate.Directive{dir("AllowUsers", "alice"), dir("AllowUsers", "r??t"), dir("PermitRootLogin", "prohibit-password")},
			want: []string{"AllowUsers alice", "AllowUsers r??t", "PermitRootLogin prohibit-password"}},
		{name: "DenyUsers root", p: pveUp, in: []migrate.Directive{dir("DenyUsers", "bob", "root")}, err: pve.ErrRootExcluded},
		{name: "DenyUsers *@host", p: pveUp, in: []migrate.Directive{dir("DenyUsers", "*@192.0.2.99")}, err: pve.ErrRootExcluded},
		{name: "DenyUsers bob", p: pveUp, in: []migrate.Directive{dir("DenyUsers", "bob"), dir("PermitRootLogin", "prohibit-password")},
			want: []string{"DenyUsers bob", "PermitRootLogin prohibit-password"}},
		{name: "AllowGroups without root's", p: pveUp, groups: []string{"root"}, in: []migrate.Directive{dir("AllowGroups", "sshusers")}, err: pve.ErrRootExcluded},
		{name: "AllowGroups with root's", p: pveUp, groups: []string{"root", "sshusers"}, in: []migrate.Directive{dir("AllowGroups", "sshusers"), dir("PermitRootLogin", "prohibit-password")},
			want: []string{"AllowGroups sshusers", "PermitRootLogin prohibit-password"}},
		{name: "DenyGroups, root's groups unknown", p: pveUp, in: []migrate.Directive{dir("DenyGroups", "nossh")}, err: pve.ErrRootExcluded},
		{name: "DenyGroups matching root's", p: pveUp, groups: []string{"root", "nossh"}, in: []migrate.Directive{dir("DenyGroups", "nossh")}, err: pve.ErrRootExcluded},
		{name: "DenyGroups not root's", p: pveUp, groups: []string{"root"}, in: []migrate.Directive{dir("DenyGroups", "nossh"), dir("PermitRootLogin", "prohibit-password")},
			want: []string{"DenyGroups nossh", "PermitRootLogin prohibit-password"}},
		{name: "PubkeyAuthentication no", p: pveUp, in: []migrate.Directive{dir("PubkeyAuthentication", "no")}, err: pve.ErrRootExcluded},
		{name: "AuthenticationMethods key and password", p: pveUp, in: []migrate.Directive{dir("AuthenticationMethods", "publickey,password")}, err: pve.ErrRootExcluded},
		{name: "AuthenticationMethods with a key-only list", p: pveUp, in: []migrate.Directive{dir("AuthenticationMethods", "publickey,password", "publickey"), dir("PermitRootLogin", "prohibit-password")},
			want: []string{"AuthenticationMethods publickey,password publickey", "PermitRootLogin prohibit-password"}},
		{name: "AuthorizedKeysFile without root's (V5)", p: pveUp, in: []migrate.Directive{dir("AuthorizedKeysFile", ".ssh_%u")}, err: pve.ErrRootExcluded},
		{name: "AuthorizedKeysFile with root's", p: pveUp, in: []migrate.Directive{dir("AuthorizedKeysFile", ".ssh/authorized_keys", ".ssh_%u/authorized_keys"), dir("PermitRootLogin", "prohibit-password")},
			want: []string{"AuthorizedKeysFile .ssh/authorized_keys .ssh_%u/authorized_keys", "PermitRootLogin prohibit-password"}},
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

// evaluated is sshd.Evaluate over the fixture, as ReadEffective gives it on
// an ssh.service host.
func evaluated(t *testing.T, withHardn bool) sshd.Effective {
	t.Helper()
	in := sshd.EffectiveInputs{
		Effective: readFixture(t, "sshd-T.out"),
		SSH:       detect.SSH{Mode: detect.SSHService, Service: detect.UnitState{Enabled: "enabled", Active: "active"}},
		Main:      readFixture(t, "root/etc/ssh/sshd_config"),
	}
	if withHardn {
		in.DropIns = []sshd.File{{Path: sshd.HardnDropIn, Data: readFixture(t, "root/etc/ssh/sshd_config.d/00-hardn.conf")}}
	} else {
		in.Effective = []byte(strings.NewReplacer("permitrootlogin without-password", "permitrootlogin yes",
			"passwordauthentication no", "passwordauthentication yes", "allowusers alice\nallowusers root\n", "").Replace(string(in.Effective)))
	}
	e, _ := sshd.Evaluate(in)
	return e
}

// TestCheckSSHFixture: on the fixture, root is key-only and reachable on
// 22, and Proxmox VE's own PermitRootLogin yes is reported as expected.
func TestCheckSSHFixture(t *testing.T) {
	c := pve.CheckSSH(evaluated(t, true), []string{"root"})
	if c.RootKeyOnly != detect.Yes || c.RootByKey != detect.Yes || c.Port22 != detect.Yes {
		t.Errorf("CheckSSH = %+v", c)
	}
	want := []pve.Finding{{Kind: pve.FindingProxmoxRootLogin, File: sshd.MainConfig, Line: 5,
		Detail: "Proxmox VE's own PermitRootLogin yes, shadowed by " + sshd.HardnDropIn + ": expected, not drift"}}
	if !slices.Equal(c.Findings, want) {
		t.Errorf("findings = %+v, want %+v", c.Findings, want)
	}
	// Only a shadowed yes in sshd_config is Proxmox VE's own line.
	e := evaluated(t, true)
	pveLine := e.Conflicts[0]
	other := pveLine
	other.Other = sshd.Origin{File: sshd.DropInDir + "/50-cloud-init.conf", Line: 1, Text: "PermitRootLogin yes"}
	no := pveLine
	no.Other.Text = "PermitRootLogin no"
	over := pveLine
	over.Kind = sshd.FindingOverridden
	for _, cf := range []sshd.Conflict{other, no, over} {
		e.Conflicts = []sshd.Conflict{cf}
		if c := pve.CheckSSH(e, []string{"root"}); slices.ContainsFunc(c.Findings, func(f pve.Finding) bool { return f.Kind == pve.FindingProxmoxRootLogin }) {
			t.Errorf("%+v read as Proxmox VE's own line", cf)
		}
	}
	// Before hardn: Proxmox VE's yes is the value, which is not key-only.
	c = pve.CheckSSH(evaluated(t, false), []string{"root"})
	if c.RootKeyOnly != detect.No || c.RootByKey != detect.Yes || len(c.Findings) != 0 {
		t.Errorf("stock: CheckSSH = %+v", c)
	}
}

func TestCheckSSH(t *testing.T) {
	known := func(v string) sshd.Setting { return sshd.Setting{Value: v, Known: true} }
	set := func(vs ...string) sshd.Set { return sshd.Set{Values: vs, Known: true} }
	tests := []struct {
		name           string
		edit           func(e *sshd.Effective)
		groups         []string
		keyOnly, byKey detect.Tri
		port22         detect.Tri
		reason         string // in a finding
	}{
		{name: "the fixture", edit: func(*sshd.Effective) {}, keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.Yes},
		{name: "PermitRootLogin no", edit: func(e *sshd.Effective) { e.PermitRootLogin = known("no") }, keyOnly: detect.No, byKey: detect.No, port22: detect.Yes, reason: "PermitRootLogin no"},
		{name: "forced-commands-only", edit: func(e *sshd.Effective) { e.PermitRootLogin = known("forced-commands-only") }, keyOnly: detect.No, byKey: detect.No, port22: detect.Yes},
		{name: "PermitRootLogin unknown", edit: func(e *sshd.Effective) { e.PermitRootLogin = sshd.Setting{} }, keyOnly: detect.Unknown, byKey: detect.Unknown, port22: detect.Yes},
		{name: "PubkeyAuthentication no", edit: func(e *sshd.Effective) { e.PubkeyAuthentication = known("no") }, keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "AllowUsers alice", edit: func(e *sshd.Effective) { e.AllowUsers = set("alice") }, keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes, reason: "does not match root"},
		{name: "AllowUsers root from one network", edit: func(e *sshd.Effective) { e.AllowUsers = set("alice", "root@192.0.2.0/24") }, keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "AllowUsers empty", edit: func(e *sshd.Effective) { e.AllowUsers = set() }, keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.Yes},
		{name: "DenyUsers root", edit: func(e *sshd.Effective) { e.DenyUsers = set("r*") }, keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "DenyUsers from a host", edit: func(e *sshd.Effective) { e.DenyUsers = set("*@192.0.2.99") }, keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "DenyUsers unknown", edit: func(e *sshd.Effective) { e.DenyUsers = sshd.Set{} }, keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "AllowGroups, groups unknown", edit: func(e *sshd.Effective) { e.AllowGroups = set("sshusers") }, keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "AllowGroups not root's", edit: func(e *sshd.Effective) { e.AllowGroups = set("sshusers") }, groups: []string{"root"}, keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "AllowGroups root's", edit: func(e *sshd.Effective) { e.AllowGroups = set("sshusers") }, groups: []string{"root", "sshusers"}, keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.Yes},
		{name: "DenyGroups root's", edit: func(e *sshd.Effective) { e.DenyGroups = set("wheel") }, groups: []string{"root", "wheel"}, keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "DenyGroups, groups unknown", edit: func(e *sshd.Effective) { e.DenyGroups = set("wheel") }, keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "AuthenticationMethods two factors", edit: func(e *sshd.Effective) { e.Keywords["authenticationmethods"] = []string{"publickey,password"} }, keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "AuthenticationMethods missing", edit: func(e *sshd.Effective) { delete(e.Keywords, "authenticationmethods") }, keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "AuthorizedKeysFile .ssh_%u (V5)", edit: func(e *sshd.Effective) { e.Keywords["authorizedkeysfile"] = []string{".ssh_%u"} }, keyOnly: detect.Yes, byKey: detect.No, port22: detect.Yes},
		{name: "AuthorizedKeysCommand may supply keys", edit: func(e *sshd.Effective) {
			e.Keywords["authorizedkeysfile"] = []string{"none"}
			e.Keywords["authorizedkeyscommand"] = []string{"/usr/sbin/keys %u"}
		}, keyOnly: detect.Yes, byKey: detect.Unknown, port22: detect.Yes},
		{name: "a Match block allows root's password", edit: func(e *sshd.Effective) {
			e.Overrides = []sshd.Override{{Block: sshd.Block{File: sshd.MainConfig, Line: 20, Text: "Match Address 192.0.2.0/24"}, Conn: sshd.DefaultConn(), Keyword: "permitrootlogin", Value: "yes"}}
		}, keyOnly: detect.No, byKey: detect.Unknown, port22: detect.Yes, reason: "Match block at /etc/ssh/sshd_config:20"},
		{name: "a Match block on AcceptEnv", edit: func(e *sshd.Effective) {
			e.Overrides = []sshd.Override{{Block: sshd.Block{File: sshd.MainConfig, Line: 20}, Keyword: "acceptenv", Value: "LANG"}}
		}, keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.Yes},
		{name: "Port 2230", edit: func(e *sshd.Effective) { e.Port = set("22", "2230") }, keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.No, reason: "Port 2230"},
		{name: "ListenAddress on 2230", edit: func(e *sshd.Effective) { e.Listeners.SSHD[0].Port = 2230 }, keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.No, reason: "ListenAddress [::]:2230"},
		{name: "Port unknown", edit: func(e *sshd.Effective) { e.Port = sshd.Set{} }, keyOnly: detect.Yes, byKey: detect.Yes, port22: detect.Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := evaluated(t, true)
			tt.edit(&e)
			c := pve.CheckSSH(e, tt.groups)
			if c.RootKeyOnly != tt.keyOnly || c.RootByKey != tt.byKey || c.Port22 != tt.port22 {
				t.Errorf("CheckSSH = key-only %v, by key %v, port 22 %v; want %v %v %v (%+v)", c.RootKeyOnly, c.RootByKey, c.Port22, tt.keyOnly, tt.byKey, tt.port22, c.Findings)
			}
			if tt.byKey != detect.Yes && !slices.ContainsFunc(c.Findings, func(f pve.Finding) bool { return f.Kind == pve.FindingRootUnreachable }) {
				t.Errorf("no root-unreachable finding: %+v", c.Findings)
			}
			if tt.reason != "" && !slices.ContainsFunc(c.Findings, func(f pve.Finding) bool { return strings.Contains(f.Detail, tt.reason) }) {
				t.Errorf("no finding says %q: %+v", tt.reason, c.Findings)
			}
		})
	}
}

func TestRootGroups(t *testing.T) {
	h := hostfake.New(host.Options{})
	h.AddUser(host.User{Name: "root", UID: 0, GID: 0, HomeDir: "/root"})
	h.AddFile(pve.GroupFile, []byte("root:x:0:\nwww-data:x:33:\nsshusers:x:1001:alice,root\nnossh:x:1002:bob\n"), 0o644, 0, 0)
	gs, err := pve.RootGroups(context.Background(), h)
	if err != nil || !slices.Equal(gs, []string{"root", "sshusers"}) {
		t.Errorf("RootGroups = %q, %v", gs, err)
	}
	checkReadOnly(t, h)
	if _, err := pve.RootGroups(context.Background(), hostfake.New(host.Options{})); !errors.Is(err, host.ErrUnknownUser) {
		t.Errorf("no root: %v", err)
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

func TestFindingCodes(t *testing.T) {
	want := map[pve.FindingKind]string{
		pve.FindingRootKeyOnly: "root-key-only", pve.FindingRootAdded: "root-allowed", pve.FindingProxmoxRootLogin: "proxmox-root-login",
		pve.FindingPortNot22: "port-not-22", pve.FindingRootUnreachable: "root-unreachable", pve.FindingClusterWide: "cluster-wide-keys",
		pve.FindingRuleMayBlock: "rule-may-block", pve.FindingBlacklist: "blacklist", pve.FindingNotModelled: "firewall-not-modelled",
	}
	for k, code := range want {
		if k.String() != code {
			t.Errorf("%d.String() = %q, want %q", k, k.String(), code)
		}
	}
	if len(want) != 9 || pve.FindingKind(0).String() != "FindingKind(0)" {
		t.Errorf("codes changed")
	}
}
