package dns_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/detect"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// with is cfg with its list replaced, as Add nameserver or a preset gives
// Engine.Plan: a copy of hardn.yml as loaded.
func with(cfg *config.Config, list []string) *config.Config {
	c := *cfg
	c.Nameservers = list
	return &c
}

// check is the control's Check on h, through the engine, as the status
// view reads it.
func check(t *testing.T, h *fakeHost, cfg *config.Config) engine.Report {
	t.Helper()
	reps, err := engineOn(h, nil).Check(context.Background(), dns.Controls(cfg))
	if err != nil || len(reps) != 1 {
		t.Fatalf("Check: %v, %v", reps, err)
	}
	return reps[0]
}

// TestCheck is every status line on every host (control-dns.md §5): the
// state, the exact detail, the findings' codes and words, and an error
// exactly for Unknown; Check only reads.
func TestCheck(t *testing.T) {
	type tc struct {
		name, host string
		list       []string
		state      engine.State
		detail     string
		findings   []string // "CODE detail"
		err        string   // the error's code, for Unknown
		setup      func(h *fakeHost)
	}
	dhcp := "DNS_DHCP_CLIENT dhcpcd may rewrite /etc/resolv.conf on a lease"
	perLink := "DNS_PER_LINK eth0 still uses its own nameserver 192.0.2.1"
	for _, tt := range []tc{
		{name: "a file, other nameservers", host: debian13, list: configured, state: engine.NotConfigured, detail: dns.DetailNotHardn, findings: []string{dhcp}},
		{name: "a file, hardn.yml's", host: configuredHost, list: configured, state: engine.Configured, detail: "192.0.2.54, 2001:db8::54", findings: []string{dhcp}},
		{name: "a file, hardn.yml's in another order", host: configuredHost, list: []string{"2001:db8::54", "192.0.2.54"}, state: engine.NotConfigured,
			detail: dns.DetailNotHardn, findings: []string{dhcp}},
		{name: "a file, hardn.yml's as written otherwise", host: configuredHost, list: []string{"192.0.2.54", "2001:DB8:0::54"}, state: engine.Configured,
			detail: "192.0.2.54, 2001:db8::54", findings: []string{dhcp}},
		{name: "hardn.yml lists none", host: configuredHost, state: engine.NotConfigured, detail: dns.DetailNone, findings: []string{dhcp}},
		{name: "hardn.yml's list is not valid", host: configuredHost, list: []string{"999.1.1.1"}, state: engine.NotConfigured, detail: dns.DetailInvalid,
			findings: []string{dhcp}},
		{name: "systemd-resolved, its global servers others", host: ubuntu2604, list: configured, state: engine.NotConfigured, detail: dns.DetailNotHardn,
			findings: []string{perLink}},
		{name: "systemd-resolved, hardn.yml's", host: ubuntu2604, list: configured, state: engine.Configured, detail: "192.0.2.54, 2001:db8::54",
			findings: []string{perLink}, setup: func(h *fakeHost) { h.global = configured }},
		{name: "systemd-resolved stopped", host: ubuntu2604, list: configured, state: engine.NotConfigured, detail: dns.DetailNotRunning,
			setup: func(h *fakeHost) { h.active = "inactive" }},
		{name: "Alpine", host: alpine324, list: configured, state: engine.NotConfigured, detail: dns.DetailNotHardn,
			findings: []string{"DNS_UDHCPC udhcpc rewrites /etc/resolv.conf on each lease"}},
		{name: "Alpine, two udhcpc hooks of one name", host: alpine324, list: configured, state: engine.NotConfigured, detail: dns.DetailNotHardn,
			findings: []string{"DNS_UDHCPC udhcpc rewrites /etc/resolv.conf on each lease",
				"DNS_UDHCPC /etc/udhcpc/pre-bound/10-dns may rewrite /etc/resolv.conf", "DNS_UDHCPC /etc/udhcpc/post-bound/10-dns may rewrite /etc/resolv.conf"},
			setup: func(h *fakeHost) {
				for _, d := range []string{hostdns.UDHCPCDir + "/pre-bound", hostdns.UDHCPCDir + "/post-bound"} {
					h.AddDir(d, 0o755, 0, 0)
					h.AddFile(d+"/10-dns", []byte("#!/bin/sh\n"), 0o755, 0, 0)
				}
			}},
		{name: "Alpine's cloud image: dhcpcd the client, no udhcpc", host: alpineCloud, list: configured, state: engine.NotConfigured,
			detail: dns.DetailNotHardn, findings: []string{"DNS_DHCP_CLIENT dhcpcd may rewrite /etc/resolv.conf on a lease"}},
		{name: "Alpine, dhcp-program udhcpc despite dhcpcd", host: alpineCloud, list: configured, state: engine.NotConfigured, detail: dns.DetailNotHardn,
			findings: []string{"DNS_UDHCPC udhcpc rewrites /etc/resolv.conf on each lease", "DNS_DHCP_CLIENT dhcpcd may rewrite /etc/resolv.conf on a lease"},
			setup: func(h *fakeHost) {
				h.AddFile(hostdns.Interfaces, []byte("auto eth0\niface eth0 inet dhcp\n\tdhcp-program udhcpc\n"), 0o644, 0, 0)
			}},
		{name: "Alpine, whether udhcpc is the client not established", host: alpine324, list: configured, state: engine.NotConfigured,
			detail: dns.DetailNotHardn, findings: []string{"DNS_UDHCPC_UNKNOWN udhcpc rewrites /etc/resolv.conf if it is the DHCP client"},
			setup: func(h *fakeHost) {
				h.AddFile(hostdns.Interfaces, []byte("source-directory /etc/network/interfaces.d\n"), 0o644, 0, 0)
			}},
		{name: "Alpine, neither the client nor the rewrite established", host: alpine324, list: configured, state: engine.NotConfigured,
			detail: dns.DetailNotHardn, findings: []string{"DNS_UDHCPC_UNKNOWN udhcpc may rewrite /etc/resolv.conf if it is the DHCP client"},
			setup: func(h *fakeHost) {
				h.AddFile(hostdns.Interfaces, []byte("source-directory /etc/network/interfaces.d\n"), 0o644, 0, 0)
				h.AddFile(hostdns.UDHCPCConf, []byte("RESOLV_CONF=$(echo no)\n"), 0o644, 0, 0)
			}},
		{name: "Proxmox VE", host: pve9, list: configured, state: engine.NotConfigured, detail: dns.DetailNotHardn,
			findings: []string{"DNS_PROXMOX_NODE Saving Proxmox VE's DNS settings rewrites /etc/resolv.conf"}},
		{name: "NetworkManager", host: networkManager, list: configured, state: engine.NotApplicable, detail: dns.DetailNetworkManager},
		{name: "resolvconf", host: resolvconf, list: configured, state: engine.NotApplicable, detail: dns.DetailResolvconf},
		{name: "another program's link", host: otherLink, list: configured, state: engine.NotApplicable, detail: dns.DetailOther},
		{name: "a file resolved reads as another's", host: debian13, list: configured, state: engine.NotApplicable, detail: dns.DetailOther,
			setup: func(h *fakeHost) { h.active = "active" }},
		{name: "a container", host: container, list: configured, state: engine.NotApplicable, detail: dns.DetailContainer},
		{name: "not a family hardn knows", host: debian13, list: configured, state: engine.NotApplicable, detail: dns.DetailUnsupported,
			setup: func(h *fakeHost) {
				h.facts.Platform = detect.Platform{Distro: detect.DistroOther, Support: detect.Unlisted}
			}},
		{name: "whether a container is not established", host: debian13, list: configured, state: engine.Unknown, detail: dns.DetailCantRead, err: "DNS_UNKNOWN",
			setup: func(h *fakeHost) { h.facts.Container = detect.Container{Detected: detect.Unknown} }},
		{name: "/etc/resolv.conf a directory", host: debian13, list: configured, state: engine.Unknown, detail: dns.DetailCantRead, err: "DNS_UNKNOWN",
			setup: func(h *fakeHost) {
				h.AddDir(hostdns.ResolvConf, 0o755, 0, 0)
			}},
		{name: "resolvectl not answering", host: ubuntu2604, list: configured, state: engine.Unknown, detail: dns.DetailCantRead, err: "COMMAND_FAILED",
			setup: func(h *fakeHost) {
				h.Handler = func(argv []string, stdin []byte) hostfake.Response {
					if strings.Join(argv, " ") == "resolvectl --no-pager domain" {
						return hostfake.Response{Stderr: "Failed to get global data\n", ExitCode: 1}
					}
					return h.answer(argv, stdin)
				}
			}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, cfg := newHost(t, tt.host, tt.list)
			if tt.setup != nil {
				tt.setup(h)
			}
			rep := check(t, h, cfg)
			var fs []string
			for _, f := range rep.Status.Findings {
				fs = append(fs, f.Code+" "+f.Detail)
				if _, ok := output.Lookup(f.Code); !ok {
					t.Errorf("finding code %s not registered", f.Code)
				}
			}
			if rep.Status.State != tt.state || rep.Status.Detail != tt.detail || !slices.Equal(fs, tt.findings) {
				t.Errorf("%v %q %q; want %v %q %q", rep.Status.State, rep.Status.Detail, fs, tt.state, tt.detail, tt.findings)
			}
			if code := output.CodeOf(rep.Err); code != tt.err {
				t.Errorf("error %v (%s), want %s", rep.Err, code, tt.err)
			}
			if w := writes(h); len(w) > 0 {
				t.Errorf("Check wrote: %v", w)
			}
			if len(rep.Status.Evidence) == 0 && !errors.Is(rep.Err, hostdns.ErrUnknown) {
				t.Error("no evidence") // only a host refused before anything is read has none
			}
		})
	}
}

// TestCheckGolden pins the status lines a reviewer reads, through
// pkg/output's human renderer: one host's DNS row each.
func TestCheckGolden(t *testing.T) {
	type line struct {
		host string
		list []string
		set  func(*fakeHost)
	}
	lines := []line{{configuredHost, configured, nil}, {debian13, configured, nil}, {configuredHost, nil, nil},
		{ubuntu2604, configured, func(h *fakeHost) { h.active = "inactive" }}, {networkManager, configured, nil}, {container, configured, nil},
		{debian13, configured, func(h *fakeHost) { h.facts.Container = detect.Container{Detected: detect.Unknown} }}}
	var runs []output.Run
	for _, l := range lines {
		h, cfg := newHost(t, l.host, l.list)
		if l.set != nil {
			l.set(h)
		}
		r := run(h, output.ModeCheck)
		r.Reports = []engine.Report{check(t, h, cfg)}
		r.Err = r.Reports[0].Err
		runs = append(runs, r)
	}
	uitest.Golden(t, "human/check", uitest.Core(), func(env term.Env) string {
		var b strings.Builder
		for i, r := range runs {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(output.Human(env, r, now))
		}
		return b.String()
	})
}

// refusalCase is a host whose plan is refused, and the report it gets.
type refusalCase struct {
	name, host string
	list       []string
	yml        string   // hardn.yml's bytes, when not hardnYMLData(list)
	edit       []string // in.Config's list, when not hardn.yml's
	stop       bool
	noFile     bool // in.Config not loaded from a file
	setup      func(h *fakeHost)
	code       string
	why, fix   string
}

var refusals = []refusalCase{
	{name: "NetworkManager", host: networkManager, list: configured, code: "DNS_MANAGED",
		why: "NetworkManager manages DNS here", fix: "set the nameservers with nmcli"},
	{name: "resolvconf", host: resolvconf, list: configured, code: "DNS_MANAGED",
		why: "resolvconf manages DNS here", fix: "set the nameservers through resolvconf"},
	{name: "another program's link", host: otherLink, list: configured, code: "DNS_MANAGED",
		why: "/etc/resolv.conf links to /run/connman/resolv.conf, which another program writes", fix: "set the nameservers with that program"},
	{name: "a file resolved reads as another's", host: debian13, list: configured, code: "DNS_MANAGED", setup: func(h *fakeHost) { h.active = "active" },
		why: "systemd-resolved runs, and /etc/resolv.conf is not its link",
		fix: "link /etc/resolv.conf to /run/systemd/resolve/stub-resolv.conf"},
	{name: "a copy of resolved's stub file", host: ubuntu2604, list: configured, code: "DNS_MANAGED",
		setup: func(h *fakeHost) {
			h.AddFile(hostdns.ResolvConf, []byte("# This is /run/systemd/resolve/stub-resolv.conf managed by man:systemd-resolved(8).\n"+
				"nameserver 127.0.0.53\noptions edns0 trust-ad\nsearch example.net\n"), 0o644, 0, 0)
		},
		why: "systemd-resolved runs, and /etc/resolv.conf is not its link", fix: "link /etc/resolv.conf to /run/systemd/resolve/stub-resolv.conf"},
	{name: "a container", host: container, list: configured, code: "DNS_CONTAINER",
		why: "This is a container: its manager provides /etc/resolv.conf", fix: "set the nameservers in the container's settings on its host"},
	{name: "hardn.yml lists none", host: debian13, code: "DNS_NO_NAMESERVERS", why: "No nameservers configured", fix: "add nameservers first"},
	{name: "not an address", host: debian13, list: []string{"192.0.2.54", "999.1.1.1"}, code: "DNS_INVALID",
		why: "Nameserver 999.1.1.1 is not an IP address", fix: "correct the nameservers in /etc/hardn/hardn.yml"},
	{name: "listed twice", host: debian13, list: []string{"2001:db8::54", "2001:DB8::54"}, code: "DNS_INVALID",
		why: "Nameserver 2001:DB8::54 is listed twice", fix: "correct the nameservers in /etc/hardn/hardn.yml"},
	{name: "resolved's stub", host: ubuntu2604, list: []string{"127.0.0.53"}, code: "DNS_INVALID",
		why: "Nameserver 127.0.0.53 is not a usable address", fix: "correct the nameservers in /etc/hardn/hardn.yml"},
	{name: "four for a file", host: debian13, list: []string{"192.0.2.51", "192.0.2.52", "192.0.2.53", "192.0.2.54"}, code: "DNS_INVALID",
		why: "/etc/resolv.conf takes 3 nameservers at most", fix: "correct the nameservers in /etc/hardn/hardn.yml"},
	{name: "systemd-resolved stopped", host: ubuntu2604, list: configured, code: "DNS_RESOLVED_NOT_RUNNING", setup: func(h *fakeHost) { h.active = "inactive" },
		why: "systemd-resolved is not running", fix: "start it with systemctl start systemd-resolved, then try again"},
	{name: "a later drop-in", host: ubuntu2604, list: configured, code: "DNS_OVERRIDDEN",
		setup: func(h *fakeHost) {
			h.AddFile("/etc/systemd/resolved.conf.d/zzz-site.conf", []byte("[Resolve]\nDNS=192.0.2.7\n"), 0o644, 0, 0)
		},
		why: "/etc/systemd/resolved.conf.d/zzz-site.conf sets DNS= after zz-hardn.conf", fix: "remove DNS= from it, then try again"},
	{name: "two later drop-ins", host: ubuntu2604, list: configured, code: "DNS_OVERRIDDEN",
		setup: func(h *fakeHost) {
			h.AddFile("/etc/systemd/resolved.conf.d/zzz-a.conf", []byte("[Resolve]\nDNS=192.0.2.7\n"), 0o644, 0, 0)
			h.AddFile("/etc/systemd/resolved.conf.d/zzz-b.conf", []byte("[Resolve]\nDNS=192.0.2.8\n"), 0o644, 0, 0)
		},
		why: "/etc/systemd/resolved.conf.d/zzz-a.conf, /etc/systemd/resolved.conf.d/zzz-b.conf set DNS= after zz-hardn.conf",
		fix: "remove DNS= from them, then try again"},
	{name: "the drop-in masked", host: ubuntu2604, list: configured, code: "DNS_DROPIN_BLOCKED",
		setup: func(h *fakeHost) { h.AddSymlink(hostdns.DropIn, "/dev/null") },
		why:   "/etc/systemd/resolved.conf.d/zz-hardn.conf is not a file", fix: "remove it, then try again"},
	{name: "udhcpc stopped off Alpine", host: debian13, list: configured, stop: true, code: "DNS_OPTION_NOT_APPLICABLE",
		why: "This host is not Alpine", fix: "leave udhcpc as it is"},
	{name: "udhcpc stopped where dhcpcd is the client", host: alpineCloud, list: configured, stop: true, code: "DNS_OPTION_NOT_APPLICABLE",
		why: "udhcpc is not this host's DHCP client", fix: "leave udhcpc as it is"},
	{name: "udhcpc stopped where the client is not established", host: alpine324, list: configured, stop: true, code: "DNS_UNKNOWN",
		setup: func(h *fakeHost) { _ = h.Remove(context.Background(), hostdns.DHCPExecutor) },
		why:   "Whether udhcpc is this host's DHCP client is not established", fix: "leave udhcpc as it is"},
	{name: "udhcpc stopped where its rewrite is not established", host: alpine324, list: configured, stop: true, code: "DNS_UNKNOWN",
		setup: func(h *fakeHost) { h.AddFile(hostdns.UDHCPCConf, []byte("RESOLV_CONF=$(echo no)\n"), 0o644, 0, 0) },
		why:   "Whether udhcpc rewrites /etc/resolv.conf is not established", fix: "leave udhcpc as it is"},
	{name: "a fact not established", host: debian13, list: configured, code: "DNS_UNKNOWN",
		setup: func(h *fakeHost) { h.facts.Container = detect.Container{Detected: detect.Unknown} },
		why:   "The host's DNS settings could not all be read", fix: "see the log for what failed, then try again"},
	{name: "hardn.yml changed since it was read", host: debian13, list: configured, edit: cloudflare, code: "CONFIG_CHANGED",
		setup: func(h *fakeHost) { h.AddFile(hardnYML, hardnYMLData([]string{"192.0.2.53"}), 0o600, 0, 0) },
		why:   "/etc/hardn/hardn.yml changed since it was read", fix: "start again, so that the file is read as it is now"},
	{name: "hardn.yml gone since it was read", host: debian13, list: configured, code: "CONFIG_CHANGED",
		setup: func(h *fakeHost) { _ = h.Remove(context.Background(), hardnYML) }, // left, the plan is not refused: the test fails
		why:   "/etc/hardn/hardn.yml changed since it was read", fix: "start again, so that the file is read as it is now"},
	{name: "hardn.yml no longer decoding", host: debian13, list: configured, code: "CONFIG_CHANGED",
		setup: func(h *fakeHost) { h.AddFile(hardnYML, []byte("nameservers: [\n"), 0o600, 0, 0) },
		why:   "/etc/hardn/hardn.yml changed since it was read", fix: "start again, so that the file is read as it is now"},
	{name: "hardn.yml a link, a preset", host: debian13, list: configured, edit: cloudflare, code: "SYMLINK_REFUSED",
		setup: func(h *fakeHost) {
			e, _ := h.Entry(hardnYML)
			h.AddFile("/etc/hardn/site.yml", e.Data, 0o600, 0, 0)
			h.AddSymlink(hardnYML, "/etc/hardn/site.yml")
		},
		why: "/etc/hardn/hardn.yml is a link", fix: "edit the file it links to by hand"},
	{name: "hardn.yml of two documents, a preset", host: debian13, edit: cloudflare, code: "CONFIG_NOT_EDITABLE",
		yml: "nameservers:\n  - \"192.0.2.54\"\n---\nnameservers: []\n",
		why: "/etc/hardn/hardn.yml holds more than one YAML document", fix: "set nameservers in /etc/hardn/hardn.yml by hand, then try again"},
	{name: "hardn.yml that would not read back, a preset", host: debian13, edit: cloudflare, code: "CONFIG_EDIT_UNSAFE",
		yml: "username: |\n  alice", // the list added after it would give the user name a line break
		why: "/etc/hardn/hardn.yml cannot be edited safely", fix: "set nameservers in /etc/hardn/hardn.yml by hand, then try again"},
	{name: "no hardn.yml read, a change", host: debian13, list: configured, noFile: true, code: "CONFIG_NOT_LOADED",
		why: "No hardn.yml was read, so there is none to save to", fix: "create /etc/hardn/hardn.yml, then try again"},
	{name: "not a family hardn knows", host: debian13, list: configured, code: "DNS_UNSUPPORTED",
		setup: func(h *fakeHost) {
			h.facts.Platform = detect.Platform{Distro: detect.DistroOther, Support: detect.Unlisted}
		},
		why: "This host is not Debian, Ubuntu, Alpine or Proxmox VE", fix: "set the nameservers by hand"},
}

// TestRefusals: every refusal of pkg/host/dns is the plan's, before any
// change, with its registered code and a report (steps.Refusal), its words
// proposed (control-dns.md §6); the plan changes nothing on the host.
func TestRefusals(t *testing.T) {
	ctx := context.Background()
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			h, cfg := newHost(t, tt.host, tt.list)
			if tt.yml != "" {
				h.AddFile(hardnYML, []byte(tt.yml), 0o600, 0, 0)
				var err error
				if cfg, err = config.Read(ctx, h, hardnYML); err != nil {
					t.Fatal(err)
				}
			}
			if tt.setup != nil {
				tt.setup(h)
			}
			own := len(h.Calls()) // the setup's
			in := cfg
			if tt.edit != nil {
				in = with(cfg, tt.edit)
			}
			if tt.noFile {
				cfg = &config.Config{Nameservers: tt.list}
				in = cfg
			}
			p, err := engineOn(h, nil).Plan(ctx, []engine.Control{dns.Nameservers{Config: cfg, StopUDHCPC: tt.stop}}, in)
			if err != nil {
				t.Fatal(err)
			}
			var r *steps.Refusal
			if !errors.As(p.Err(), &r) || len(p.Changes) != 0 {
				t.Fatalf("plan %d changes, error %v; want a refusal", len(p.Changes), p.Err())
			}
			if code := output.CodeOf(p.Refused[0]); code != tt.code || r.Code != tt.code || r.Why != tt.why || r.Fix != tt.fix {
				t.Errorf("%s (Refusal %s): %q / %q; want %s: %q / %q", code, r.Code, r.Why, r.Fix, tt.code, tt.why, tt.fix)
			}
			if w := writesAfter(h, own); len(w) > 0 {
				t.Errorf("the plan wrote: %v", w)
			}
		})
	}
}

// planOn plans ctl with cfg on h.
func planOn(t *testing.T, h *fakeHost, ctl dns.Nameservers, cfg *config.Config) *engine.Plan {
	t.Helper()
	p, err := engineOn(h, nil).Plan(context.Background(), []engine.Control{ctl}, cfg)
	if err != nil || p.Err() != nil {
		t.Fatalf("Plan: %v, %v", err, p.Err())
	}
	return p
}

// summary is a plan's changes in a line each: the id, the title, the
// target, the commands, the diff's first header.
func summary(p *engine.Plan) []string {
	var out []string
	for _, c := range p.Changes {
		head, _, _ := strings.Cut(c.Diff, "\n")
		out = append(out, fmt.Sprintf("%s %q %s %q %q", c.ID, c.Heading(), c.Target, c.Commands, head))
	}
	return out
}

// TestPlans is each platform's plan: the change, its target, diff and
// commands, its findings, and the save of hardn.yml after it exactly when
// the list is not the one hardn.yml holds (decision 1). The host's search
// domains are kept (decision 2), and nothing is written.
func TestPlans(t *testing.T) {
	const set, save = dns.ChangeSet, dns.ChangeSave
	resolvedSet := func(created string) string {
		return set + ` "Set the nameservers" ` + hostdns.DropIn + ` ["systemctl try-reload-or-restart systemd-resolved.service"] "--- ` + created + `"`
	}
	fileSet := func(title string) string {
		return set + ` "` + title + `" /etc/resolv.conf [] "--- /etc/resolv.conf"`
	}
	saveLine := save + ` "Save the configuration" /etc/hardn/hardn.yml [] "--- /etc/hardn/hardn.yml"`
	for _, tt := range []struct {
		name, host string
		yml        []string // hardn.yml's; nil for configured
		list       []string // in.Config's
		stop       bool
		noFile     bool // in.Config not loaded from a file
		setup      func(h *fakeHost)
		changes    []string
		summary    string // the first change's, when set
		findings   []string
		search     string // the line of /etc/resolv.conf after the change; "" for none
	}{
		{name: "Ubuntu 26.04: the drop-in created, a reload", host: ubuntu2604, list: configured, changes: []string{resolvedSet("/dev/null")},
			findings: []string{"DNS_PER_LINK"}},
		{name: "Ubuntu 24.04: the same command, which restarts", host: ubuntu2404, list: configured, changes: []string{resolvedSet("/dev/null")},
			findings: []string{"DNS_PER_LINK"}},
		{name: "Debian 12 cloud: the uplink link", host: debian12, list: configured, changes: []string{resolvedSet("/dev/null")},
			findings: []string{"DNS_PER_LINK"}},
		{name: "Debian 13: a preset, saved", host: debian13, list: cloudflare, changes: []string{fileSet("Set the nameservers"), saveLine},
			findings: []string{"DNS_DHCP_CLIENT"}, search: "search example.net"},
		{name: "Alpine 3.24: udhcpc stopped", host: alpine324, list: configured, stop: true,
			changes: []string{set + ` "Set nameservers; stop udhcpc rewriting" /etc/udhcpc/udhcpc.conf, /etc/resolv.conf [] "--- /etc/udhcpc/udhcpc.conf"`},
			summary: "Set the nameservers to 192.0.2.54, 2001:db8::54, and stop udhcpc rewriting /etc/resolv.conf", search: "search example.net"},
		{name: "Alpine 3.24: udhcpc stopped, the nameservers already hardn.yml's", host: alpine324, list: configured, stop: true,
			setup: func(h *fakeHost) {
				h.AddFile(hostdns.ResolvConf, []byte("search example.net\nnameserver 192.0.2.54\nnameserver 2001:db8::54\n"), 0o644, 0, 0)
			},
			changes: []string{set + ` "Stop udhcpc rewriting resolv.conf" /etc/udhcpc/udhcpc.conf [] "--- /etc/udhcpc/udhcpc.conf"`},
			summary: "Stop udhcpc rewriting /etc/resolv.conf"},
		{name: "Alpine 3.24: udhcpc kept", host: alpine324, list: configured, changes: []string{fileSet("Set the nameservers")},
			findings: []string{"DNS_UDHCPC"}, search: "search example.net"},
		{name: "Alpine's cloud image: dhcpcd's finding, none of udhcpc's", host: alpineCloud, list: configured, changes: []string{fileSet("Set the nameservers")},
			findings: []string{"DNS_DHCP_CLIENT"}, search: "search example.net"},
		{name: "Proxmox VE 9: its file, one search domain", host: pve9, list: configured, changes: []string{fileSet("Set the nameservers")},
			findings: []string{"DNS_PROXMOX_NODE"}, search: "search example.net"},
		{name: "configured: nothing", host: configuredHost, list: configured},
		{name: "configured, no hardn.yml read: nothing", host: configuredHost, list: configured, noFile: true},
		{name: "configured, hardn.yml's list another: the save alone", host: configuredHost, yml: []string{"192.0.2.53"}, list: configured,
			changes: []string{saveLine}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			yml := tt.yml
			if yml == nil {
				yml = configured
			}
			h, cfg := newHost(t, tt.host, yml)
			if tt.setup != nil {
				tt.setup(h)
			}
			in := with(cfg, tt.list)
			if tt.noFile {
				cfg = &config.Config{Nameservers: tt.list}
				in = cfg
			}
			p := planOn(t, h, dns.Nameservers{Config: cfg, StopUDHCPC: tt.stop}, in)
			if got := summary(p); !slices.Equal(got, tt.changes) {
				t.Errorf("changes\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.changes, "\n"))
			}
			if tt.summary != "" && (len(p.Changes) == 0 || p.Changes[0].Summary != tt.summary) {
				t.Errorf("summary %q, want %q", summary(p), tt.summary)
			}
			var codes []string
			for _, c := range p.Changes {
				for _, f := range c.Findings {
					codes = append(codes, f.Code)
				}
			}
			if !slices.Equal(codes, tt.findings) {
				t.Errorf("findings %q, want %q", codes, tt.findings)
			}
			for _, c := range p.Changes {
				if c.Risk != engine.RiskPlain || c.Stage != engine.StageFiles {
					t.Errorf("%s: risk %v, stage %v; want plain, files (brief 2.4)", c.ID, c.Risk, c.Stage)
				}
				if after := []string{set}; c.ID == save && (len(p.Changes) == 1 && c.After != nil || len(p.Changes) == 2 && !slices.Equal(c.After, after) ||
					!slices.Equal(c.Backups, []string{hardnYML})) {
					t.Errorf("save: after %q, backups %q", c.After, c.Backups)
				}
				if c.ID == set && tt.search != "" && !strings.Contains(c.Diff, "\n "+tt.search+"\n") {
					t.Errorf("the search line %q not kept:\n%s", tt.search, c.Diff)
				}
			}
			if w := writes(h); len(w) > 0 {
				t.Errorf("the plan wrote: %v", w)
			}
		})
	}
}

// TestEvidence: what each line was read from, one line per source, the
// links' servers and search domains among them (D4).
func TestEvidence(t *testing.T) {
	for _, tt := range []struct {
		host string
		want []string
	}{
		{ubuntu2604, []string{"/etc/resolv.conf -> ../run/systemd/resolve/stub-resolv.conf", "systemctl is-active systemd-resolved.service: active",
			"resolvectl dns: Global: none", "resolvectl dns: link 2 (eth0): 192.0.2.1", "resolvectl domain: Global: none",
			"resolvectl domain: link 2 (eth0): example.net", "/etc/hardn/hardn.yml: nameservers 192.0.2.54, 2001:db8::54"}},
		{debian13, []string{"/etc/resolv.conf: nameservers 192.0.2.1", "/etc/resolv.conf: search example.net",
			"/etc/hardn/hardn.yml: nameservers 192.0.2.54, 2001:db8::54"}},
		{networkManager, []string{"/etc/resolv.conf -> /run/NetworkManager/resolv.conf"}},
	} {
		t.Run(tt.host, func(t *testing.T) {
			h, cfg := newHost(t, tt.host, configured)
			if got := check(t, h, cfg).Status.Evidence; !slices.Equal(got, tt.want) {
				t.Errorf("evidence\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

// TestHostRefusal: HostRefusal is the plan's refusal, the same report
// word for word, exactly where every plan on the host refuses whatever its
// list (a container, a family hardn does not configure, a manager hardn
// only reports, a fact not established, systemd-resolved stopped, a later
// drop-in, hardn's drop-in path blocked: next/s2-screen), and nil for
// every other refusal, which the plan makes of its list, of udhcpc's
// answer or of hardn.yml, and on every host that plans.
func TestHostRefusal(t *testing.T) {
	ctx := context.Background()
	host := map[string]bool{"DNS_CONTAINER": true, "DNS_UNSUPPORTED": true, "DNS_MANAGED": true, "DNS_UNKNOWN": true,
		"DNS_RESOLVED_NOT_RUNNING": true, "DNS_OVERRIDDEN": true, "DNS_DROPIN_BLOCKED": true}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newHost(t, tt.host, tt.list)
			if tt.setup != nil {
				tt.setup(h)
			}
			own := len(h.Calls())
			s, err := hostdns.Read(ctx, h, h.facts)
			got := dns.HostRefusal(s, err)
			var r *steps.Refusal
			switch {
			case !host[tt.code], tt.stop: // with stop, a refusal of udhcpc's answer
				if got != nil {
					t.Errorf("HostRefusal = %v; want nil, the plan's %s being of the list or the file", got, tt.code)
				}
			case !errors.As(got, &r) || output.CodeOf(got) != tt.code || r.Code != tt.code || r.Why != tt.why || r.Fix != tt.fix:
				t.Errorf("HostRefusal = %v (%s); want the plan's %s: %q / %q", got, output.CodeOf(got), tt.code, tt.why, tt.fix)
			}
			if w := writesAfter(h, own); len(w) > 0 {
				t.Errorf("it wrote: %v", w)
			}
		})
	}
	for _, name := range []string{debian13, configuredHost, ubuntu2604, ubuntu2404, debian12, alpine324, alpineCloud, pve9} {
		h, _ := newHost(t, name, configured)
		if s, err := hostdns.Read(ctx, h, h.facts); dns.HostRefusal(s, err) != nil {
			t.Errorf("%s: HostRefusal = %v; want nil", name, dns.HostRefusal(s, err))
		}
	}
}
