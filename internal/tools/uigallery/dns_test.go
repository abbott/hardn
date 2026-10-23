package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The DNS mocks' hosts on the gallery's resolver model (resolver.go), the
// hosts the engine's DNS scenarios run on, read by pkg/host/dns itself, as
// the engine detects them: pkg/ui cannot import pkg/host, so the mocks
// (pkg/ui/screen dns.go) hold their readings, plans, findings, refusals
// and checks as data, and these tests hold that data to what dns.Read,
// dns.Validate, the DNS control (pkg/engine/controls/dns) on the engine
// through output.FlowEngine, and pkg/output's codes make of the same hosts
// (docs/engine/gallery-dns.md §8). Synthetic values only.

// dnsScreenHost returns the host of the DNS screen whose suffix is name:
// Debian 13 for the main one, "", and for "empty", "one" (hardn.yml listing
// none, or 192.0.2.54 alone) and "configured" (the file listing
// hardn.yml's already) and "networkmanager"; Alpine 3.24, Proxmox VE 9 and
// Ubuntu 26.04 by their names.
func dnsScreenHost(t *testing.T, name string) *fakeHost {
	t.Helper()
	switch name {
	case "":
		return dnsHost(debian13Platform)()
	case "empty":
		return noListHost()
	case "one":
		return simulate(debian13Platform).withHardnYML(nameserversYML(dnsList[:1]))
	case "configured":
		return configuredDNSHost()
	case "networkmanager":
		return networkManagerHost()
	case "alpine324":
		return dnsHost(alpine324Platform)()
	case "pve9":
		return dnsHost(pve9Platform)()
	case "ubuntu2604":
		return dnsHost(ubuntu2604Platform)()
	}
	t.Fatalf("no DNS host %q", name)
	return nil
}

// dnsRead is what the engine and dns.Read establish of h: its facts, the
// DNS state, and hardn.yml as loaded.
func dnsRead(t *testing.T, h *fakeHost) (detect.Facts, dns.State, *config.Config) {
	t.Helper()
	ctx := context.Background()
	f, err := detect.Detect(ctx, h.f)
	if err != nil {
		t.Fatal(err)
	}
	s, err := dns.Read(ctx, h.f, f)
	if err != nil || s.Err != nil {
		t.Fatalf("dns.Read: %v, %v", err, s.Err)
	}
	cfg, err := config.Read(ctx, h.f, hardnYML)
	if err != nil {
		t.Fatal(err)
	}
	return f, s, cfg
}

// listed is cfg with list as its nameservers, a copy, as an action edits
// hardn.yml's list before it plans.
func listed(cfg *config.Config, list []string) *config.Config {
	c := *cfg
	c.Nameservers = slices.Clone(list)
	return &c
}

// realPlan plans the DNS control on h for list, udhcpc stopped when asked,
// as the DNS screen will (output.FlowEngine over the engine on h, with
// hardn.yml as loaded and a copy listing list): the adapter, its plan as
// the flow shows it, and its error.
func realPlan(t *testing.T, h *fakeHost, list []string, udhcpc bool) (*output.FlowEngine, flow.Plan, error) {
	t.Helper()
	_, _, loaded := dnsRead(t, h)
	a := &output.FlowEngine{Controls: []engine.Control{dnsctl.Nameservers{Config: loaded, StopUDHCPC: udhcpc}}, Config: listed(loaded, list),
		LogPath: output.LegacyLogPath}
	a.Engine = engine.New(h.system(), engine.Options{
		Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
		Session:  applySession,
		Hold:     func() func() { return func() {} },
		PID:      applyPID,
		Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
		Progress: a.Progress,
	})
	p, err := a.Plan(context.Background())
	return a, p, err
}

// refusalOf is the DNS control's refusal of list on h, planned as the
// engine plans it; nil when it plans.
func refusalOf(t *testing.T, h *fakeHost, list []string) *steps.Refusal {
	t.Helper()
	f, _, loaded := dnsRead(t, h)
	_, err := dnsctl.Nameservers{Config: loaded}.Plan(context.Background(), h.f, engine.Input{Facts: f, Config: listed(loaded, list)})
	var r *steps.Refusal
	if err != nil && !errors.As(err, &r) {
		t.Fatalf("plan of %v: %v, not a refusal", list, err)
	}
	return r
}

// dnsMock returns the mock named name.
func dnsMock(t *testing.T, name string) screen.Mock {
	t.Helper()
	for _, m := range screen.Mocks() {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("no mock %s", name)
	return screen.Mock{}
}

// dnsScreens are the DNS screens' hosts, by mock.
var dnsScreens = map[string]string{"sub-menu/03-dns": "", "sub-menu/03-dns-alpine324": "alpine324",
	"sub-menu/03-dns-configured": "configured", "sub-menu/03-dns-empty": "empty", "sub-menu/03-dns-networkmanager": "networkmanager",
	"sub-menu/03-dns-one": "one", "sub-menu/03-dns-pve9": "pve9", "sub-menu/03-dns-ubuntu2604": "ubuntu2604"}

// searchInUse is the search domains in use on the host s describes, as
// the screens and the reviews show them: the file's, or resolved's global
// ones and then each link's (Link.Domains), named by its link,
// "example.net (eth0)".
func searchInUse(s dns.State) []string {
	if s.Manager != dns.ManagerResolved {
		return s.Search
	}
	in := slices.Clone(s.Resolved.Domains)
	for _, l := range s.Resolved.Links {
		for _, d := range l.Domains {
			in = append(in, d+" ("+l.Name+")")
		}
	}
	return in
}

// TestDNSScreensAreReal checks that each DNS screen shows what dns.Read
// establishes of its host, its facts the engine's (detect.Detect): the
// manager, in the screen's proposed words; the nameservers in use, the
// file's, or systemd-resolved's global ones and each link's after it, and
// Unknown where pkg/host/dns reads none, as of a manager it only reports;
// the search domains in use, the file's, or resolved's global ones and
// then each link's (searchInUse; "None" for none); and hardn.yml's list as
// config.Read loads it, or P-30 (b)'s muted item for none, item 3 then
// left out. Every DNS screen is one of these.
func TestDNSScreensAreReal(t *testing.T) {
	managers := map[dns.Manager]component.KeyValue{
		dns.ManagerFile:     {Key: "DNS Implementation", Value: "direct (/etc/resolv.conf)"},
		dns.ManagerProxmox:  {Key: "DNS Implementation", Value: "Proxmox VE (/etc/resolv.conf)"},
		dns.ManagerResolved: {Key: "DNS Implementation", Value: "systemd-resolved"},
		dns.ManagerNetworkManager: {Key: "DNS Implementation", Value: "NetworkManager,", State: component.ValueWarn,
			Note: "reported only"},
	}
	shown := func(key string, values []string, read bool) component.KeyValue {
		switch {
		case !read:
			return component.KeyValue{Key: key, State: component.ValueUnknown}
		case len(values) == 0:
			return component.KeyValue{Key: key, Value: "None"}
		}
		return component.KeyValue{Key: key, Value: strings.Join(values, ", ")}
	}
	for _, m := range screen.Mocks() {
		if strings.HasPrefix(m.Name, "sub-menu/03-dns") {
			if _, ok := dnsScreens[m.Name]; !ok {
				t.Errorf("%s: a DNS screen with no host here", m.Name)
			}
		}
	}
	for name, hostName := range dnsScreens {
		_, s, cfg := dnsRead(t, dnsScreenHost(t, hostName))
		inUse, search, read := s.Nameservers, searchInUse(s), s.Manager == dns.ManagerFile || s.Manager == dns.ManagerProxmox
		if s.Manager == dns.ManagerResolved {
			inUse, read = slices.Clone(s.Resolved.Global), true
			for _, l := range s.Resolved.Links {
				for _, a := range l.Servers {
					inUse = append(inUse, a+" ("+l.Name+")")
				}
			}
		}
		rows := []component.KeyValue{{Key: "No nameservers configured", State: component.ValueNone}}
		if len(cfg.Nameservers) > 0 {
			rows = nil
			for i, ns := range cfg.Nameservers {
				rows = append(rows, component.KeyValue{Key: fmt.Sprintf("Nameserver %d", i+1), Value: ns})
			}
		}
		vm, _ := dnsMock(t, name).Screen.(screen.SubMenu)
		want := [][]component.KeyValue{{managers[s.Manager], shown("Nameservers", inUse, read), shown("Search Domains", search, read)}, rows}
		if len(vm.Sections) != 2 || !slices.Equal(vm.Sections[0].Rows.Items, want[0]) || !slices.Equal(vm.Sections[1].Rows.Items, want[1]) {
			t.Errorf("%s: shows %+v; dns.Read gives %s, %v, %v, hardn.yml %v, want %+v", name, vm.Sections, s.Manager, inUse, search, cfg.Nameservers, want)
		}
		removes := slices.ContainsFunc(vm.Menu.Items, func(it component.MenuItem) bool { return it.Key == "3" })
		if removes != (len(cfg.Nameservers) > 0) {
			t.Errorf("%s: item 3 shown %v, with %d nameservers configured", name, removes, len(cfg.Nameservers))
		}
	}
}

// TestDNSRefusalsAreReal checks each DNS screen's refusals against the
// DNS control's: on the NetworkManager host every item refuses, where the
// control refuses DNS_MANAGED, in its words, its Why and its Fix as one
// sentence each; with no nameserver configured, Configure DNS refuses,
// where the control refuses DNS_NO_NAMESERVERS, in origin/main's sentence
// around the control's Fix ("Please add nameservers first.", slice 2
// decision 3); with one, Remove nameserver refuses, where the control
// plans hardn.yml's list and refuses the list without it with
// DNS_NO_NAMESERVERS, in the mock's own words (decision 1 (ii)); no other
// screen refuses anything, and the control plans hardn.yml's list on every
// other host.
func TestDNSRefusalsAreReal(t *testing.T) {
	sentence := func(s string) string { return strings.ToUpper(s[:1]) + s[1:] + "." }
	for name, hostName := range dnsScreens {
		h := dnsScreenHost(t, hostName)
		_, s, cfg := dnsRead(t, h)
		m := dnsMock(t, name)
		vm, _ := m.Screen.(screen.SubMenu)
		r := refusalOf(t, h, cfg.Nameservers)
		var want []string
		words := map[string]string{} // each refusal's words, by item
		switch hostName {
		case "networkmanager":
			if r == nil || output.CodeOf(r) != "DNS_MANAGED" || s.Manager != dns.ManagerNetworkManager {
				t.Fatalf("%s: the control refuses %v; want DNS_MANAGED for NetworkManager (%s)", name, r, s.Manager)
			}
			for _, it := range vm.Menu.Items {
				want, words[it.Key] = append(want, it.Key), sentence(r.Why)+" "+sentence(r.Fix)
			}
		case "empty":
			if r == nil || output.CodeOf(r) != "DNS_NO_NAMESERVERS" {
				t.Fatalf("%s: the control refuses %v; want DNS_NO_NAMESERVERS", name, r)
			}
			want, words["1"] = []string{"1"}, "Please "+r.Fix+"."
		case "one":
			rr := refusalOf(t, h, cfg.Nameservers[1:])
			if r != nil || len(cfg.Nameservers) != 1 || rr == nil || output.CodeOf(rr) != "DNS_NO_NAMESERVERS" {
				t.Fatalf("%s: the control refuses %v; less its one nameserver %v; want it to plan, then DNS_NO_NAMESERVERS", name, r, rr)
			}
			want, words["3"] = []string{"3"}, "The last nameserver cannot be removed."
		default:
			if r != nil {
				t.Errorf("%s: the control refuses %v", name, r)
			}
		}
		var keys []string
		for k, msg := range m.Refusals {
			keys = append(keys, k)
			if msg.Text != words[k] || msg.Kind != component.MessageWarn {
				t.Errorf("%s/%s: refuses with %+v; want a warning, %q", name, k, msg, words[k])
			}
		}
		slices.Sort(keys)
		if !slices.Equal(keys, want) {
			t.Errorf("%s: refuses %v, want %v", name, keys, want)
		}
	}
}

// dnsCase is a DNS sequence and what it asks of its host.
type dnsCase struct {
	mock, sequence, host string
	nameservers          []string
	udhcpc               bool
}

var dnsCases = []dnsCase{
	{"workflow-step/dns-configure-dns", "normal", "", dnsList, false},
	{"workflow-step/dns-add-nameserver", "normal", "", []string{"192.0.2.54", "2001:db8::54", "192.0.2.53"}, false},
	{"workflow-step/dns-remove-nameserver", "normal", "", []string{"192.0.2.54"}, false},
	{"workflow-step/dns-use-cloudflare-dns", "normal", "", []string{"1.1.1.1", "1.0.0.1"}, false},
	{"workflow-step/dns-use-cloudflare-dns", "dry-run", "", []string{"1.1.1.1", "1.0.0.1"}, false},
	{"workflow-step/dns-use-cloudflare-dns", "cancelled", "", []string{"1.1.1.1", "1.0.0.1"}, false},
	{"workflow-step/dns-use-google-dns", "normal", "", []string{"8.8.8.8", "8.8.4.4"}, false},
	{"workflow-step/dns-use-quad9-dns", "normal", "", []string{"9.9.9.9", "149.112.112.112"}, false},
	{"workflow-step/dns-configure-dns-alpine324", "normal", "alpine324", dnsList, true},
	{"workflow-step/dns-configure-dns-alpine324", "kept", "alpine324", dnsList, false},
	{"workflow-step/dns-configure-dns-configured", "unchanged", "configured", dnsList, false},
	{"workflow-step/dns-configure-dns-pve9", "normal", "pve9", dnsList, false},
	{"workflow-step/dns-configure-dns-ubuntu2604", "normal", "ubuntu2604", dnsList, false},
	{"workflow-step/dns-configure-dns-ubuntu2604", "failure", "ubuntu2604", dnsList, false},
	{"workflow-step/dns-configure-dns-ubuntu2604", "failure-proposed", "ubuntu2604", dnsList, false},
}

// collected is sequence seq's flow with its form answered, all but the
// confirmation, as the flow collects them before it reviews and plans.
func collected(t *testing.T, seq screen.Sequence) flow.Flow {
	t.Helper()
	_, f := seq.Build()
	var lines []string
	for _, a := range seq.Answers {
		if a.Step != "confirm" {
			lines = append(lines, a.Line)
		}
	}
	env := term.Fixed(80, colorprofile.NoTTY, false, true)
	p := nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(strings.NewReader(strings.Join(lines, "\n")+"\n"), false), Out: io.Discard}
	if err := f.Form.Run(p); err != nil {
		t.Fatalf("%s: the form: %v", seq.Name, err)
	}
	return f
}

// sequenceNamed returns mock m's sequence name.
func sequenceNamed(t *testing.T, m screen.Mock, name string) screen.Sequence {
	t.Helper()
	i := slices.IndexFunc(m.Sequences, func(s screen.Sequence) bool { return s.Name == name })
	if i < 0 {
		t.Fatalf("%s: no sequence %s", m.Name, name)
	}
	return m.Sequences[i]
}

// TestDNSPlansAreReal checks every DNS sequence's plan and review against
// the DNS control's plan for its host and list, through the flow's adapter
// (realPlan), as the screen will plan it: the same changes, in order, each
// with the control's title, target and commands, and the host's change
// with its diff (a created file against /dev/null); the save of hardn.yml
// last, exactly when the list is not hardn.yml's, its diff the control's
// alone (the mock shows none: a fixture, gallery-dns.md §8); every change
// of the files stage and plain (brief 2.4); no change where the control
// plans none. The review: the nameservers, as dns.Validate keeps them, and
// the search domains the host keeps. The plan's findings, which the flow
// lists as the review's last lines (next/s2-flow-review): those of the
// host the plan keeps (dns.State.Findings, less udhcpc's on udhcpc.conf
// when the plan stops it, as dns.PlanDNS keeps them), in the control's
// words (steps.DNSFinding), each a warning, its code in pkg/output's
// registry; with a change, the host change's own, then the save's, which
// has none since its comments-lost warning was dropped
// (next/s2-config-edit), exactly as the real flow is handed them
// (output.FlowEngine); with none, the real plan carries none, where the
// mock keeps the host's (decision 5's reading, gallery-dns.md §8).
func TestDNSPlansAreReal(t *testing.T) {
	for _, tt := range dnsCases {
		name := tt.mock + "/" + tt.sequence
		h := dnsScreenHost(t, tt.host)
		_, s, cfg := dnsRead(t, h)
		a, real, err := realPlan(t, h, tt.nameservers, tt.udhcpc)
		if err != nil {
			t.Fatalf("%s: the control's plan: %v", name, err)
		}
		f := collected(t, sequenceNamed(t, dnsMock(t, tt.mock), tt.sequence))
		mock, err := f.Engine.Plan(context.Background())
		if err != nil {
			t.Fatalf("%s: plan: %v", name, err)
		}
		p := a.Run(flow.NotRun, nil).Plan
		saved := !slices.Equal(tt.nameservers, cfg.Nameservers)
		if n := len(p.Changes); len(mock.View.Changes) != n || saved != (n > 0 && p.Changes[n-1].ID == dnsctl.ChangeSave) {
			t.Errorf("%s: plans %+v\nthe control %+v; the save %t", name, mock.View.Changes, real.View.Changes, saved)
			continue
		}
		for i, c := range p.Changes {
			got, want := mock.View.Changes[i], real.View.Changes[i]
			diff := got.Diff == want.Diff
			if c.ID == dnsctl.ChangeSave {
				diff = got.Diff == "" && want.Diff != ""
			}
			if got.Title != want.Title || got.Target != want.Target || !slices.Equal(got.Commands, want.Commands) || !diff {
				t.Errorf("%s: change %d plans\n%+v\nthe control\n%+v", name, i, got, want)
			}
			if c.Stage != engine.StageFiles || c.Risk != engine.RiskPlain {
				t.Errorf("%s: %s's stage %v, risk %v; want files, plain (brief 2.4)", name, c.ID, c.Stage, c.Risk)
			}
		}
		v, err := dns.Validate(dns.Config{Nameservers: tt.nameservers}, s.Manager)
		if err != nil {
			t.Fatal(err)
		}
		search := "None"
		if in := searchInUse(s); len(in) > 0 {
			search = strings.Join(in, ", ")
		}
		var host []flow.Finding
		for _, fd := range s.Findings() {
			if fd.Kind != dns.FindingUDHCPC || fd.Path != dns.UDHCPCConf || !tt.udhcpc {
				ef := steps.DNSFinding(s, fd)
				host = append(host, flow.Finding{Text: ef.Detail, Code: ef.Code})
			}
		}
		var found []flow.Finding
		for _, c := range p.Changes {
			var own []flow.Finding
			for _, fd := range c.Findings {
				if _, ok := output.Lookup(fd.Code); !ok {
					t.Errorf("%s: %s's finding %s is not in pkg/output's registry", name, c.ID, fd.Code)
				}
				own = append(own, flow.Finding{Text: fd.Detail, Code: fd.Code})
			}
			if c.ID == dnsctl.ChangeSet && !slices.Equal(own, host) {
				t.Errorf("%s: the change's findings %+v, the host's %+v", name, own, host)
			}
			found = append(found, own...)
		}
		if !slices.Equal(real.Findings, found) {
			t.Errorf("%s: the flow is handed %+v, the plan's findings %+v", name, real.Findings, found)
		}
		if len(p.Changes) == 0 {
			found = host
		}
		if !slices.Equal(mock.Findings, found) {
			t.Errorf("%s: the mock's findings\n%+v\nwant\n%+v", name, mock.Findings, found)
		}
		want := []component.KeyValue{{Key: "Nameservers", Value: strings.Join(v.Nameservers, ", ")}, {Key: "Search Domains", Value: search}}
		if r := f.Review(); !slices.Equal(r.Items, want) {
			t.Errorf("%s: reviews\n%+v\nwant\n%+v", name, r.Items, want)
		}
	}
}

// TestDNSCheckIsValidate checks Add nameserver's check against
// dns.Validate for the main host's list and one more entry, on each kind
// of address dns.Validate refuses, a duplicate, and some it takes: the
// check refuses exactly what dns.Validate refuses, each kind of refusal in
// its own words, origin/main's where it has them (an empty entry, one
// that is no address, a duplicate), else the proposed one for an address
// a resolver cannot use.
func TestDNSCheckIsValidate(t *testing.T) {
	f := collected(t, sequenceNamed(t, dnsMock(t, "workflow-step/dns-add-nameserver"), "normal"))
	var check func(string) error
	for _, fl := range f.Form.Fields {
		if tf, ok := fl.(*form.Text); ok {
			check = tf.Validate
		}
	}
	if check == nil {
		t.Fatal("Add nameserver asks no text with a check")
	}
	for _, entry := range []string{"999.1.1.1", "192.0.2.54", "192.0.2.53", "2001:db8::53", "2001:DB8::54", "web-01.example.net",
		"192.0.2.53:53", "", "fe80::1", "fe80::1%eth0", "::ffff:192.0.2.53", "::", "0.0.0.0", "0.1.2.3", "255.255.255.255",
		"224.0.0.251", "ff02::1", "127.0.0.53", "169.254.0.53", "192.0.2.054"} {
		_, verr := dns.Validate(dns.Config{Nameservers: append(slices.Clone(dnsList), entry)}, dns.ManagerFile)
		var want string
		switch {
		case verr == nil:
		case !errors.Is(verr, dns.ErrInvalid):
			t.Fatalf("dns.Validate(%q): %v, not ErrInvalid", entry, verr)
		case entry == "":
			want = "Nameserver cannot be empty"
		case strings.Contains(verr.Error(), "not an IPv4 or IPv6 address"):
			want = "Invalid IP address format"
		case strings.Contains(verr.Error(), "given twice"):
			want = "Nameserver " + canonicalOf(entry) + " is already configured"
		default:
			want = "Not a usable nameserver address"
		}
		got := check(entry)
		if (got == nil) != (verr == nil) || got != nil && got.Error() != want {
			t.Errorf("check(%q) = %v; want %q, as dns.Validate: %v", entry, got, want, verr)
		}
	}
}

// canonicalOf is entry as dns.Validate keeps it, or entry itself when it
// takes none.
func canonicalOf(entry string) string {
	c, err := dns.Validate(dns.Config{Nameservers: []string{entry}}, dns.ManagerFile)
	if err != nil {
		return entry
	}
	return c.Nameservers[0]
}

// reports is what an Apply reported, step by step.
type reports []string

func (r *reports) report(step int, state component.StepState, detail string) {
	*r = append(*r, fmt.Sprint(step, state, detail))
}

// TestDNSFailureIsTheEngines checks the Ubuntu host's failure sequences
// against the DNS control on the real engine, through output.FlowEngine,
// as the flow will apply it, on the Ubuntu 26.04 host whose
// systemd-resolved's reload loads nothing new (inertReload): the change
// fails with DNS_NOT_EFFECTIVE, dns.Apply puts the drop-in back, and since
// unit (b) (next/s2-control, D3) the engine reports the change rolled back
// and the failure in a few words with its next step, which is the
// proposed sequence, failure-proposed: the same steps and the same
// report. The sequence failure, the report as the engine gave it before
// unit (b), differs from it in both, as gallery-dns.md §3 records.
func TestDNSFailureIsTheEngines(t *testing.T) {
	ctx := context.Background()
	h := inertReload()
	a, _, err := realPlan(t, h, dnsList, false)
	if err != nil {
		t.Fatal(err)
	}
	var real reports
	realErr := a.Apply(ctx, real.report)
	var rf *flow.Failure
	if !errors.As(realErr, &rf) || !errors.Is(realErr, dns.ErrNotEffective) {
		t.Fatalf("the engine: %v; want a flow failure, DNS_NOT_EFFECTIVE", realErr)
	}
	apply := func(sequence string) (reports, *flow.Failure) {
		f := collected(t, sequenceNamed(t, dnsMock(t, "workflow-step/dns-configure-dns-ubuntu2604"), sequence))
		if _, err := f.Engine.Plan(ctx); err != nil {
			t.Fatal(err)
		}
		var mock reports
		var mf *flow.Failure
		if err := f.Engine.Apply(ctx, mock.report); !errors.As(err, &mf) {
			t.Fatalf("%s: the mock: %v; want a flow failure", sequence, err)
		}
		return mock, mf
	}
	proposed, pf := apply("failure-proposed")
	if rf.Error() != pf.Error() || rf.Command != pf.Command || rf.Output != pf.Output || rf.LogPath != pf.LogPath || rf.Next != pf.Next {
		t.Errorf("the proposed sequence fails with\n%+v\nthe engine with\n%+v", *pf, *rf)
	}
	// Running, failed, then rolled back: the engine's running comes from
	// the change's start, its rolled back from the rollback's end.
	if want := []string{fmt.Sprint(0, component.StepRunning, ""), fmt.Sprint(0, component.StepFailed, "DNS_NOT_EFFECTIVE"),
		fmt.Sprint(0, component.StepRolledBack, "")}; !slices.Equal(real, proposed) || !slices.Equal(real, want) {
		t.Errorf("the proposed sequence reports %q, the engine %q; want %q", proposed, real, want)
	}
	if old, mf := apply("failure"); slices.Equal(old, real) || mf.Error() == rf.Error() {
		t.Errorf("the sequence of the report before unit (b) is the engine's now: %q, %v", old, mf)
	}
	if _, err := h.f.Lstat(ctx, dns.DropIn); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the drop-in is left after the failure: %v", err)
	}
}
