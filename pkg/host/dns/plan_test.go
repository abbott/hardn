package dns_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestPlanFile: the nameserver lines are replaced where the first one was
// and every other line is kept: search and domain lines too, unless asked
// to replace them. A host already configured plans nothing.
func TestPlanFile(t *testing.T) {
	tests := []struct {
		name string
		old  string // "" for no file
		c    dns.Config
		want string
	}{
		{"keep the rest", "# installer\ndomain example.net\nsearch example.net\nnameserver 192.0.2.1\noptions rotate\nnameserver 192.0.2.2\n",
			dns.Config{Nameservers: []string{ns1, ns6}},
			"# installer\ndomain example.net\nsearch example.net\nnameserver 192.0.2.53\nnameserver 2001:db8::53\noptions rotate\n"},
		{"replace search", "domain example.net\nnameserver 192.0.2.1\nsearch a.example.net\n",
			dns.Config{Nameservers: []string{ns1}, Search: []string{"example.net", "lan"}, ReplaceSearch: true},
			"search example.net lan\nnameserver 192.0.2.53\n"},
		{"remove search", "search example.net\nnameserver 192.0.2.1\n",
			dns.Config{Nameservers: []string{ns1}, ReplaceSearch: true},
			"nameserver 192.0.2.53\n"},
		{"no nameserver line", "options ndots:2", dns.Config{Nameservers: []string{ns1}, Search: []string{"lan"}, ReplaceSearch: true},
			"options ndots:2\nsearch lan\nnameserver 192.0.2.53\n"},
		{"no file", "", dns.Config{Nameservers: []string{ns1}}, "nameserver 192.0.2.53\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t, "debian-13")
			w.h.AddFile(dns.ResolvConf, []byte(tt.old), 0o644, 0, 0)
			if tt.old == "" {
				_ = w.h.Remove(t.Context(), dns.ResolvConf)
			}
			p := w.plan(t, tt.c)
			if !slices.Equal(steps(p), []string{"write-resolv-conf"}) || string(p.Changes[0].After) != tt.want {
				t.Fatalf("steps %q, after %q", steps(p), p.Changes[0].After)
			}
			if (p.Changes[0].Before == nil) != (tt.old == "") {
				t.Errorf("before %q", p.Changes[0].Before)
			}
			w.h.AddFile(dns.ResolvConf, []byte(tt.want), 0o644, 0, 0)
			if p := w.plan(t, tt.c); len(p.Changes) != 0 {
				t.Errorf("second plan %q", steps(p))
			}
		})
	}
}

// TestPlanResolved: the drop-in resets both lists before setting them, so
// the main resolved.conf and earlier drop-ins cannot add to them; the
// search domains of the drop-in hardn wrote before are kept unless asked
// otherwise; per-link servers are a finding.
func TestPlanResolved(t *testing.T) {
	w := newWorld(t, "ubuntu-26.04")
	p := w.plan(t, dns.Config{Nameservers: []string{ns1, ns6}, Search: []string{"example.net"}, ReplaceSearch: true})
	const want = "# Written by hardn: the system DNS servers for systemd-resolved.\n" +
		"# An empty assignment resets the list that earlier files built.\n" +
		"[Resolve]\nDNS=\nDNS=192.0.2.53 2001:db8::53\nDomains=\nDomains=example.net\n"
	if !slices.Equal(steps(p), []string{"write-drop-in", "reload-resolved"}) || string(p.Changes[0].After) != want || p.Changes[0].Path != dns.DropIn {
		t.Fatalf("steps %q, after %q", steps(p), p.Changes[0].After)
	}
	if !slices.Equal(kinds(p), []string{"dns-per-link"}) {
		t.Errorf("findings %q", kinds(p))
	}
	w.h.AddFile(dns.DropIn, []byte(want), 0o644, 0, 0)
	p = w.plan(t, dns.Config{Nameservers: []string{ns2}})
	const kept = "# Written by hardn: the system DNS servers for systemd-resolved.\n" +
		"# An empty assignment resets the list that earlier files built.\n" +
		"[Resolve]\nDNS=\nDNS=192.0.2.54\nDomains=\nDomains=example.net\n"
	if string(p.Changes[0].After) != kept || string(p.Changes[0].Before) != want {
		t.Errorf("after %q", p.Changes[0].After)
	}
	// Written but not loaded: only the reload.
	w.h.AddFile(dns.DropIn, []byte(kept), 0o644, 0, 0)
	if p := w.plan(t, dns.Config{Nameservers: []string{ns2}}); !slices.Equal(steps(p), []string{"reload-resolved"}) {
		t.Errorf("steps %q", steps(p))
	}
	// Loaded but for the search domains, which the plan sets: the reload.
	w.load()
	w.domains = []string{"lan"}
	if p := w.plan(t, dns.Config{Nameservers: []string{ns2}, Search: []string{"Example.net"}, ReplaceSearch: true}); !slices.Equal(steps(p), []string{"write-drop-in", "reload-resolved"}) {
		t.Errorf("steps %q", steps(p))
	}
	if p := w.plan(t, dns.Config{Nameservers: []string{ns2}, Search: []string{"example.net"}, ReplaceSearch: true}); !slices.Equal(steps(p), []string{"reload-resolved"}) {
		t.Errorf("steps %q", steps(p))
	}
	w.domains = []string{"EXAMPLE.net"}
	if p := w.plan(t, dns.Config{Nameservers: []string{ns2}, Search: []string{"example.net"}, ReplaceSearch: true}); len(p.Changes) != 0 {
		t.Errorf("steps %q", steps(p))
	}
	// hardn's file edited with blanks around "=": its Domains are kept.
	w.h.AddFile(dns.DropIn, []byte("[Resolve]\nDNS=\nDNS=192.0.2.54\nDomains = \nDomains = example.net\n"), 0o644, 0, 0)
	if p := w.plan(t, dns.Config{Nameservers: []string{ns2}}); string(p.Changes[0].After) != kept {
		t.Errorf("after %q", p.Changes[0].After)
	}
}

// TestPlanRefuses: managers hardn does not configure, and facts that are
// not established, are refused, naming why.
func TestPlanRefuses(t *testing.T) {
	c := dns.Config{Nameservers: []string{ns1}}
	tests := []struct {
		name    string
		fixture string
		seed    func(w *world)
		c       dns.Config
		want    error
	}{
		{"resolvconf", "debian-13", func(w *world) { w.h.AddSymlink(dns.ResolvConf, "../run/resolvconf/resolv.conf") }, c, dns.ErrManaged},
		{"NetworkManager", "debian-13", func(w *world) { w.h.AddSymlink(dns.ResolvConf, "/run/NetworkManager/resolv.conf") }, c, dns.ErrManaged},
		{"unknown link", "debian-13", func(w *world) { w.h.AddSymlink(dns.ResolvConf, "/etc/resolv.conf.local") }, c, dns.ErrManaged},
		{"resolved stopped", "ubuntu-26.04", func(w *world) { w.active = "inactive" }, c, dns.ErrNotRunning},
		{"drop-in is a link", "ubuntu-26.04", func(w *world) { w.h.AddSymlink(dns.DropIn, "/dev/null") }, c, dns.ErrDropInBlocked},
		{"resolv.conf a directory", "debian-13", func(w *world) {
			_ = w.h.RemoveAll(t.Context(), dns.ResolvConf)
			w.h.AddDir(dns.ResolvConf, 0o755, 0, 0)
		}, c, dns.ErrUnknown},
		{"resolvectl fails", "ubuntu-26.04", func(w *world) {
			w.h.Respond(hostfake.Response{Stderr: "Failed to get global data\n", ExitCode: 1}, "resolvectl", "--no-pager", "dns")
		}, c, dns.ErrUnknown},
		{"empty list (V19)", "ubuntu-26.04", func(*world) {}, dns.Config{}, dns.ErrNoNameservers},
		{"udhcpc off Alpine", "debian-13", func(*world) {}, dns.Config{Nameservers: []string{ns1}, UDHCPCNoResolv: true}, dns.ErrNotApplicable},
		{"udhcpc with resolved", "ubuntu-26.04", func(*world) {}, dns.Config{Nameservers: []string{ns1}, UDHCPCNoResolv: true}, dns.ErrNotApplicable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t, tt.fixture)
			tt.seed(w)
			p, err := dns.PlanDNS(w.read(t), tt.c)
			if !errors.Is(err, tt.want) || len(p.Changes) != 0 {
				t.Fatalf("err %v, want %v; steps %q", err, tt.want, steps(p))
			}
		})
	}
	if _, err := dns.PlanDNS(dns.State{}, c); !errors.Is(err, dns.ErrUnknown) || !errors.Is(err, detect.ErrNoSignal) {
		t.Errorf("zero state: %v", err)
	}
}

// TestPlanFindings: what may undo a file manager's write later: on every
// distribution, a DHCP client; on Alpine, udhcpc's script unless the plan
// sets RESOLV_CONF, and its hooks whatever the plan does.
func TestPlanFindings(t *testing.T) {
	hook := "/etc/udhcpc/post-renew/50-resolv"
	for _, tt := range []struct {
		fixture string
		add     []string // executables added to the stock layout
		c       dns.Config
		steps   []string
		kinds   []string
	}{
		{"debian-13", nil, dns.Config{Nameservers: []string{ns1}}, []string{"write-resolv-conf"}, []string{"dns-dhcp-client"}},
		{"alpine-3.24", nil, dns.Config{Nameservers: []string{ns1}}, []string{"write-resolv-conf"}, []string{"dns-udhcpc"}},
		{"alpine-3.24", nil, dns.Config{Nameservers: []string{ns1}, UDHCPCNoResolv: true}, []string{"udhcpc-no-resolv", "write-resolv-conf"}, nil},
		{"alpine-3.24", []string{"/sbin/dhcpcd", hook}, dns.Config{Nameservers: []string{ns1}, UDHCPCNoResolv: true},
			[]string{"udhcpc-no-resolv", "write-resolv-conf"}, []string{"dns-udhcpc", "dns-dhcp-client"}},
		{"proxmox", nil, dns.Config{Nameservers: []string{ns1}}, []string{"write-resolv-conf"}, []string{"dns-proxmox-node"}},
		{"proxmox", []string{"/usr/sbin/dhclient"}, dns.Config{Nameservers: []string{ns1}}, []string{"write-resolv-conf"}, []string{"dns-proxmox-node", "dns-dhcp-client"}},
	} {
		t.Run(tt.fixture, func(t *testing.T) {
			w := newWorld(t, tt.fixture)
			for _, a := range tt.add {
				w.h.AddFile(a, nil, 0o755, 0, 0)
			}
			p := w.plan(t, tt.c)
			if !slices.Equal(steps(p), tt.steps) || !slices.Equal(kinds(p), tt.kinds) {
				t.Errorf("steps %q, findings %q", steps(p), kinds(p))
			}
			if slices.Contains(tt.add, hook) && p.Findings[0].Path != hook {
				t.Errorf("findings %+v", p.Findings)
			}
		})
	}
}

// TestStateFindings: a host whose plan is refused still reports what a
// check shows; one whose manager hardn does not configure has none.
func TestStateFindings(t *testing.T) {
	w := newWorld(t, "ubuntu-26.04")
	w.h.AddFile("/etc/systemd/resolved.conf.d/~site.conf", []byte("[Resolve]\nDNS=192.0.2.7\n"), 0o644, 0, 0)
	s := w.read(t)
	if _, err := dns.PlanDNS(s, dns.Config{Nameservers: []string{ns1}}); !errors.Is(err, dns.ErrOverridden) {
		t.Fatalf("plan: %v", err)
	}
	if f := s.Findings(); len(f) != 1 || f[0].Kind != dns.FindingPerLink {
		t.Errorf("findings %+v", f)
	}
	w = newWorld(t, "debian-13")
	w.h.AddSymlink(dns.ResolvConf, "../run/resolvconf/resolv.conf")
	if f := w.read(t).Findings(); len(f) != 0 {
		t.Errorf("resolvconf: findings %+v", f)
	}
}

// TestPlanUDHCPC: RESOLV_CONF="no" takes the place of the first
// RESOLV_CONF line, later ones are dropped, every other line is kept.
func TestPlanUDHCPC(t *testing.T) {
	w := newWorld(t, "alpine-3.24")
	w.h.AddFile(dns.UDHCPCConf, []byte("# site\nRESOLV_CONF=/etc/resolv.conf\nNO_DNS=eth1\n  RESOLV_CONF=\"/etc/resolv.conf\"\n"), 0o644, 0, 0)
	p := w.plan(t, dns.Config{Nameservers: []string{ns1}, UDHCPCNoResolv: true})
	if want := "# site\nRESOLV_CONF=\"no\"\nNO_DNS=eth1\n"; len(p.Changes) != 2 || string(p.Changes[0].After) != want {
		t.Errorf("steps %q, udhcpc.conf %q", steps(p), p.Changes[0].After)
	}
}
