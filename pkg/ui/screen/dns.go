package screen

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/diff"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// The DNS mocks (refactor plan slice 2, unit (a); docs/engine/gallery-dns.md):
// origin/main's DNS screen (pkg/menu dns_menu.go) on the sub-menu layout,
// one per host state, and its items as workflows through the canonical
// flow, each change planned, reviewed and confirmed y/N, with no timer, as
// a change with no lockout risk is (brief 2.4). pkg/ui cannot import
// pkg/host, so each host's reading, plan, findings, refusal and check are
// data here; the gallery's TestDNS* tests (internal/tools/uigallery
// dns_test.go) build the same hosts on its resolver model and hold each of
// them to what pkg/host/dns, the DNS control (pkg/engine/controls/dns) on
// the engine and pkg/output's codes make of them (next/s2-engine-gallery).
// The words are origin/main's where it has them; every other one is
// proposed, for the maintainer to decide (gallery-dns.md, its words
// table).

// The DNS screen's words: origin/main's title, headings, rows and items.
const (
	dnsTitle      = "DNS Configuration"
	dnsCurrent    = "Current DNS Configuration"
	dnsConfigured = "Configured Nameservers"
	dnsNone       = "No nameservers configured"
)

// The DNS workflows' words, proposed: the plan's changes, the rows of the
// review, and the refusal of Remove nameserver on a host with one.
const (
	dnsChange       = "Set the nameservers"
	dnsChangeUDHCPC = "Set nameservers; stop udhcpc rewriting"
	dnsSave         = "Save the configuration"
	dnsSaveTo       = "/etc/hardn/hardn.yml"
	dnsSearch       = "Search Domains"
	dnsServers      = "Nameservers"
	dnsLastOne      = "The last nameserver cannot be removed."
)

// dnsPreset is one of origin/main's three presets: its item, and the
// IPv4 servers it sets, as origin/main has them.
type dnsPreset struct {
	key, label  string
	nameservers []string
}

var dnsPresets = []dnsPreset{
	{"4", "Use Cloudflare DNS", []string{"1.1.1.1", "1.0.0.1"}},
	{"5", "Use Google DNS", []string{"8.8.8.8", "8.8.4.4"}},
	{"6", "Use Quad9 DNS", []string{"9.9.9.9", "149.112.112.112"}},
}

// dnsConfiguredList is hardn.yml's nameservers on every host but the one
// with none, and the one configured already.
var dnsConfiguredList = []string{"192.0.2.54", "2001:db8::54"}

// dnsHost is a host of the DNS mocks: what pkg/host/dns reads there, as
// its screen shows it, hardn.yml's list, and what it writes for a list of
// nameservers.
type dnsHost struct {
	// name is the host's, and its screen's suffix: sub-menu/03-dns-<name>,
	// or sub-menu/03-dns for the main one, "".
	name string
	// desc names the host in its screen's source note, as the screens of
	// hosts that read alike cannot.
	desc string
	// manager is the "DNS Implementation" row: what manages
	// /etc/resolv.conf, as dns.Read classifies it.
	manager component.KeyValue
	// inUse and search are the nameservers and search domains in use; ""
	// shows Unknown, which is what pkg/host/dns reads of a manager it
	// only reports. A link's own are shown after the global ones, with
	// the link, as dns.Read keeps them on it (Link.Domains since
	// next/s2-control).
	inUse, search string
	configured    []string
	// refusal, for a host whose manager hardn only reports (P-37 (a)), is
	// what every item says in place of a plan.
	refusal string
	// plan is the change for nameservers ns; udhcpc stops udhcpc
	// rewriting the file (Alpine).
	plan func(ns []string, udhcpc bool) component.PlanChange
	// findings are a plan's findings (dns.Plan's), with udhcpc stopped or
	// not, each with its code, which the flow lists before its question.
	findings func(udhcpc bool) []flow.Finding
}

// nameservers is ns as nameserver lines, as dns writes them into a file.
func nameservers(ns []string) string {
	var b strings.Builder
	for _, a := range ns {
		b.WriteString("nameserver " + a + "\n")
	}
	return b.String()
}

// fileChange is the change of a host whose /etc/resolv.conf is a file:
// its nameserver lines, the last of it, replaced by ns, every line above
// kept.
func fileChange(head string, now []string) func([]string, bool) component.PlanChange {
	const path = "/etc/resolv.conf"
	return func(ns []string, _ bool) component.PlanChange {
		return component.PlanChange{Title: dnsChange, Target: path,
			Diff: diff.Unified(path, path, head+nameservers(now), head+nameservers(ns))}
	}
}

// The Alpine host's files: udhcpc's resolv.conf and Alpine's stock
// udhcpc.conf, every line of it commented out.
const (
	udhcpcConf  = "/etc/udhcpc/udhcpc.conf"
	searchHead  = "search example.net\n"
	udhcpcStock = "# Do not overwrite /etc/resolv.conf\n#RESOLV_CONF=\"no\"\n\n#NO_DNS=\"eth1 wlan1\"\n"
	udhcpcOff   = "# hardn: keep udhcpc from rewriting /etc/resolv.conf\nRESOLV_CONF=\"no\"\n"
)

// alpineChange is the Alpine host's change: with udhcpc stopped,
// RESOLV_CONF="no" appended to udhcpc.conf first, then the file's
// nameservers, one change of two files, as steps.DNS makes it, titled
// for both.
func alpineChange(ns []string, udhcpc bool) component.PlanChange {
	ch := fileChange(searchHead, []string{"192.0.2.1"})(ns, false)
	if udhcpc {
		ch.Title, ch.Target = dnsChangeUDHCPC, udhcpcConf+", "+ch.Target
		ch.Diff = diff.Unified(udhcpcConf, udhcpcConf, udhcpcStock, udhcpcStock+udhcpcOff) + ch.Diff
	}
	return ch
}

// The Ubuntu host's drop-in, which systemd-resolved reads last, and the
// reload that loads it.
const (
	dropInDNS  = "/etc/systemd/resolved.conf.d/zz-hardn.conf"
	dropInHead = "# Written by hardn: the system DNS servers for systemd-resolved.\n" +
		"# An empty assignment resets the list that earlier files built.\n[Resolve]\nDNS=\n"
	resolvedReload = "systemctl try-reload-or-restart systemd-resolved.service"
)

// resolvedChange is the Ubuntu host's change: hardn's drop-in, a new
// file, then systemd-resolved reloaded.
func resolvedChange(ns []string, _ bool) component.PlanChange {
	return component.PlanChange{Title: dnsChange, Target: dropInDNS,
		Diff:     diff.Unified("/dev/null", dropInDNS, "", dropInHead+"DNS="+strings.Join(ns, " ")+"\n"),
		Commands: []string{resolvedReload}}
}

// warns returns findings fs whatever is asked.
func warns(fs ...flow.Finding) func(bool) []flow.Finding {
	return func(bool) []flow.Finding { return fs }
}

// warning is a finding that warns, in text, of code.
func warning(code, text string) flow.Finding { return flow.Finding{Text: text, Code: code} }

// dnsHosts are the hosts. Debian 13 is the main one, whose screen the main
// menu leads to (that menu is Figma's frame, its host block Figma's Debian
// 12 host): a file dhcpcd wrote, with dhcpcd installed. The others are
// Ubuntu 26.04 on systemd-resolved, Alpine 3.24 with udhcpc, a Proxmox VE
// 9 node, Debian 13 with NetworkManager, which hardn only reports, and
// the main host with no nameserver configured ("empty"), with one
// ("one"), and using the configured ones already ("configured").
func dnsHosts() []dnsHost {
	direct := component.KeyValue{Key: "DNS Implementation", Value: "direct (/etc/resolv.conf)"}
	debianHead := "# Generated by dhcpcd from eth0.dhcp\nsearch example.net\n"
	dhcpcd := warns(warning("DNS_DHCP_CLIENT", "dhcpcd may rewrite /etc/resolv.conf on a lease"))
	debian := dnsHost{desc: "Debian 13, its /etc/resolv.conf a file dhcpcd wrote", manager: direct, inUse: "192.0.2.1",
		search: "example.net", configured: dnsConfiguredList, plan: fileChange(debianHead, []string{"192.0.2.1"}), findings: dhcpcd}
	empty, one, configured := debian, debian, debian
	empty.name, empty.desc, empty.configured = "empty", "Debian 13 as the main host, hardn.yml listing no nameserver", nil
	one.name, one.desc, one.configured = "one", "Debian 13 as the main host, hardn.yml listing one nameserver", dnsConfiguredList[:1:1]
	configured.name, configured.desc, configured.inUse = "configured", "Debian 13, its file listing hardn.yml's nameservers already",
		strings.Join(dnsConfiguredList, ", ")
	configured.plan = fileChange(debianHead, dnsConfiguredList)
	return []dnsHost{
		debian,
		{name: "alpine324", desc: "Alpine 3.24, udhcpc rewriting its /etc/resolv.conf", manager: direct, inUse: "192.0.2.1",
			search: "example.net", configured: dnsConfiguredList, plan: alpineChange, findings: func(stopped bool) []flow.Finding {
				if stopped {
					return nil
				}
				return []flow.Finding{warning("DNS_UDHCPC", "udhcpc rewrites /etc/resolv.conf on each lease")}
			}},
		configured,
		empty,
		{name: "networkmanager", desc: "Debian 13, its /etc/resolv.conf NetworkManager's link", configured: dnsConfiguredList,
			manager: component.KeyValue{Key: "DNS Implementation", Value: "NetworkManager,", State: component.ValueWarn, Note: "reported only"},
			refusal: "NetworkManager manages DNS here. Set the nameservers with nmcli."},
		one,
		{name: "pve9", desc: "a Proxmox VE 9 node, its installer's /etc/resolv.conf",
			manager: component.KeyValue{Key: "DNS Implementation", Value: "Proxmox VE (/etc/resolv.conf)"},
			inUse:   "192.0.2.1", search: "example.net", configured: dnsConfiguredList,
			plan:     fileChange(searchHead, []string{"192.0.2.1"}),
			findings: warns(warning("DNS_PROXMOX_NODE", "Saving Proxmox VE's DNS settings rewrites /etc/resolv.conf"))},
		{name: "ubuntu2604", desc: "Ubuntu 26.04 on systemd-resolved, eth0 configured by DHCP",
			manager: component.KeyValue{Key: "DNS Implementation", Value: "systemd-resolved"},
			inUse:   "192.0.2.1 (eth0)", search: "example.net (eth0)", configured: dnsConfiguredList,
			plan: resolvedChange, findings: warns(warning("DNS_PER_LINK", "eth0 still uses its own nameserver 192.0.2.1"))},
	}
}

// mock returns the mock name of h's screen.
func (h dnsHost) mock() string {
	if h.name == "" {
		return "sub-menu/03-dns"
	}
	return "sub-menu/03-dns-" + h.name
}

// workflow returns the mock name of h's workflow for item label.
func (h dnsHost) workflow(label string) string {
	name := "workflow-step/dns-" + strings.ToLower(strings.ReplaceAll(label, " ", "-"))
	if h.name != "" {
		name += "-" + h.name
	}
	return name
}

// row is a row of the current state: its value, or Unknown for none.
func row(key, value string) component.KeyValue {
	if value == "" {
		return component.KeyValue{Key: key, State: component.ValueUnknown}
	}
	return component.KeyValue{Key: key, Value: value}
}

// screen returns h's DNS screen: origin/main's, with the search domains
// in use added (the host keeps them, gallery-dns.md decision 2) and, with
// no nameserver configured, P-30 (b)'s bullet item, muted, origin/main's
// words; item 3 only when there is one to remove.
func (h dnsHost) screen() SubMenu {
	var listed []component.KeyValue
	for i, ns := range h.configured {
		listed = append(listed, component.KeyValue{Key: fmt.Sprintf("Nameserver %d", i+1), Value: ns})
	}
	if len(listed) == 0 {
		listed = []component.KeyValue{{Key: dnsNone, State: component.ValueNone}}
	}
	items := []component.MenuItem{
		{Key: "1", Label: "Configure DNS", Description: "Apply nameserver settings from configuration"},
		{Key: "2", Label: "Add nameserver", Description: "Add a new DNS server to configuration"},
	}
	if len(h.configured) > 0 {
		items = append(items, component.MenuItem{Key: "3", Label: "Remove nameserver", Description: "Remove a DNS server from configuration"})
	}
	for _, p := range dnsPresets {
		items = append(items, component.MenuItem{Key: p.key, Label: p.label, Description: "Set nameservers to " + strings.Join(p.nameservers, ", ")})
	}
	return SubMenu{
		Title: trail(dnsTitle),
		Sections: []Section{{
			Header: component.SectionHeader{Title: dnsCurrent},
			Rows:   component.BulletList{Items: []component.KeyValue{h.manager, row(dnsServers, h.inUse), row(dnsSearch, h.search)}},
		}, {
			Header: component.SectionHeader{Title: dnsConfigured},
			Rows:   component.BulletList{Items: listed},
		}},
		Heading: fixtureHeading,
		Menu:    component.Menu{Items: items, Exit: returnItem()},
		Prompt:  mainPrompt,
	}
}

// refusals are what h's items say in place of a plan: on a host whose
// manager hardn only reports, every item; with no nameserver configured,
// Configure DNS, in origin/main's words without the line the screen
// already shows ("No nameservers configured."); with one, Remove
// nameserver, whose plan would leave none (proposed words).
func (h dnsHost) refusals() map[string]component.Message {
	warn := func(text string) component.Message { return component.Message{Kind: component.MessageWarn, Text: text} }
	switch {
	case h.refusal != "":
		out := map[string]component.Message{}
		for _, it := range h.screen().Menu.Items {
			out[it.Key] = warn(h.refusal)
		}
		return out
	case len(h.configured) == 0:
		return map[string]component.Message{"1": warn("Please add nameservers first.")}
	case len(h.configured) == 1:
		return map[string]component.Message{"3": warn(dnsLastOne)}
	}
	return nil
}

// review is the review of a change to ns on h: the nameservers the host
// will use and the search domains it keeps. The flow ends it with the
// plan's findings, a warning line each (planned; since next/s2-flow-review,
// as the real flow lists them). Saving hardn.yml keeps its comments, and
// says nothing of them (refactor plan §3.19, amended by the maintainer on
// 2026-10-22; next/s2-config-edit).
func (h dnsHost) review(ns []string) flow.Review {
	return flow.Review{Title: dnsTitle, Items: []component.KeyValue{{Key: dnsServers, Value: strings.Join(ns, ", ")}, {Key: dnsSearch, Value: h.search}}}
}

// planned is the plan for ns on h: its change, none when the host uses ns
// already, then the save of hardn.yml when its list changes, after the
// change has applied; and the host's findings, udhcpc's gone when udhcpc
// is stopped, also when nothing changes (decision 5's reading: the real
// control's plan then carries none, gallery-dns.md §8).
func (h dnsHost) planned(ns []string, udhcpc bool) flow.Plan {
	var changes []component.PlanChange
	if ch := h.plan(ns, udhcpc); ch.Diff != "" || len(ch.Commands) > 0 {
		changes = append(changes, ch)
	}
	if !slices.Equal(ns, h.configured) {
		changes = append(changes, component.PlanChange{Title: dnsSave, Target: dnsSaveTo})
	}
	return flow.Plan{View: component.PlanView{Changes: changes}, Findings: h.findings(udhcpc)}
}

// dnsFailure is how a DNS workflow's change fails: the report, and
// whether the change is reported rolled back after it failed.
type dnsFailure struct {
	flow.Failure
	undone bool
}

// undone is the Engine of a change whose own unit undoes it when it
// fails, reported as proposed for unit (b): the step failed, then rolled
// back (gallery-dns.md §8, item 3).
type undone struct{ answered }

// Apply applies the fixture, then reports its failed step rolled back.
func (u undone) Apply(ctx context.Context, report flow.Report) error {
	f := u.answered()
	err := f.Apply(ctx, report)
	var failed *flow.Failure
	if errors.As(err, &failed) {
		report(f.FailAt-1, component.StepRolledBack, "")
	}
	return err
}

// dnsFlow is one DNS workflow on h: its title bar, the item's label, and a
// flow that asks fields, on Alpine whether to stop udhcpc rewriting the
// file too (y/N, the explicit step dns.Config.UDHCPCNoResolv asks for),
// then reviews and plans the list list returns, and asks question about
// it, y/N. dryRun stops it at the plan; fail fails the plan's first step
// with DNS_NOT_EFFECTIVE.
func (h dnsHost) dnsFlow(label string, fields []form.Field, list func() []string, question func() string, fail *dnsFailure, dryRun bool) (WorkflowStep, flow.Flow) {
	stop := new(bool)
	if h.name == "alpine324" {
		fields = append(fields, &form.YesNo{Title: "Stop udhcpc rewriting /etc/resolv.conf?", Value: stop})
	}
	fixture := answered(func() flow.Fixture {
		f := flow.Fixture{Planned: h.planned(list(), *stop)}
		if fail != nil {
			f.FailAt, f.Detail, f.Failure = 1, "DNS_NOT_EFFECTIVE", fail.Failure
		}
		return f
	})
	var engine flow.Engine = fixture
	if fail != nil && fail.undone {
		engine = undone{fixture}
	}
	return WorkflowStep{Title: trail(dnsTitle, label)}, flow.Flow{
		Form:    form.Form{Fields: fields},
		Review:  func() flow.Review { return h.review(list()) },
		Engine:  engine,
		Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: question()} },
		DryRun:  dryRun,
	}
}

// The validity checks of a nameserver, dns.Validate's.
var (
	thisNetwork = netip.MustParsePrefix("0.0.0.0/8")
	broadcast   = netip.MustParseAddr("255.255.255.255")
)

// Add nameserver's inline errors: origin/main's where it has them
// (pkg/menu dns_menu.go addNameserver), and one proposed for the
// addresses dns.Validate refuses that origin/main took.
const (
	addEmpty    = "Nameserver cannot be empty"
	addFormat   = "Invalid IP address format"
	addTwice    = "Nameserver %s is already configured"
	addUnusable = "Not a usable nameserver address"
)

// addCheck is Add nameserver's check of entry after the configured list on
// a host whose /etc/resolv.conf is a file: dns.Validate's per-address
// rules and its duplicate check, each refusal in the screen's words, which
// the gallery's test holds to dns.Validate's refusals entry by entry, each
// of their kinds to its words. A nameserver already configured is refused
// with a warning (nav.Warn), as origin/main warns of it, the others as
// failed. The list stays under dns.Validate's count for a file here, so
// its count rule is not copied. The real screen calls dns.Validate.
func addCheck(configured []string) func(string) error {
	return func(entry string) error {
		a, err := netip.ParseAddr(entry)
		switch {
		case entry == "":
			return errors.New(addEmpty)
		case err != nil:
			return errors.New(addFormat)
		case a.Zone() != "", a.Is4In6(), a.IsUnspecified(), a.IsMulticast(),
			thisNetwork.Contains(a), a == broadcast, a.Is6() && a.IsLinkLocalUnicast():
			return errors.New(addUnusable)
		case slices.Contains(configured, a.String()):
			return nav.Warn(fmt.Sprintf(addTwice, a))
		}
		return nil
	}
}

// canonical is entry as netip prints it, as dns.Validate returns it: the
// address hardn writes and saves.
func canonical(entry string) string {
	if a, err := netip.ParseAddr(entry); err == nil {
		return a.String()
	}
	return entry
}

// workflows returns h's workflows, by mock name: Configure DNS on every
// host hardn writes, and on the main one Add, Remove and the presets too,
// each with the sequences gallery-dns.md lists. The host with one
// nameserver configured shows its screen and Remove's refusal alone.
func (h dnsHost) workflows() []Mock {
	if h.refusal != "" || len(h.configured) == 0 || h.name == "one" {
		return nil
	}
	src := func(delta string) Source {
		return Source{Label: "inferred from origin/main", Delta: delta, Pending: dnsPending}
	}
	confirm := func(lines ...string) []Answer {
		var out []Answer
		if h.name == "alpine324" {
			out = append(out, Answer{"udhcpc", lines[0]})
			lines = lines[1:]
		}
		for _, l := range lines {
			out = append(out, Answer{"confirm", l})
		}
		return out
	}
	configure := func(fail *dnsFailure, dryRun bool) func() (WorkflowStep, flow.Flow) {
		return func() (WorkflowStep, flow.Flow) {
			return h.dnsFlow("Configure DNS", nil, func() []string { return h.configured },
				func() string { return "Configure DNS?" }, fail, dryRun)
		}
	}
	seqs := []Sequence{{Name: "normal", Build: configure(nil, false), Answers: confirm("y"), Ends: flow.Applied}}
	switch h.name {
	case "alpine324":
		seqs[0].Answers = confirm("y", "y")
		seqs = append(seqs, Sequence{Name: "kept", Build: configure(nil, false), Answers: confirm("n", "y"), Ends: flow.Applied})
	case "configured":
		seqs = []Sequence{{Name: "unchanged", Build: configure(nil, false), Ends: flow.Unchanged}}
	case "ubuntu2604":
		seqs = append(seqs,
			Sequence{Name: "failure", Build: configure(&dnsNotEffective, false), Answers: confirm("y"), Ends: flow.Failed},
			Sequence{Name: "failure-proposed", Build: configure(&dnsUndone, false), Answers: confirm("y"), Ends: flow.Failed})
	}
	out := []Mock{{Name: h.workflow("Configure DNS"), Source: src(dnsConfigureDelta), Sequences: seqs}}
	if h.name != "" {
		return out
	}
	add := func() (WorkflowStep, flow.Flow) {
		var entry string
		field := &form.Text{Title: "Enter nameserver IP address", Validate: addCheck(h.configured), Value: &entry}
		list := func() []string { return append(slices.Clone(h.configured), canonical(entry)) }
		return h.dnsFlow("Add nameserver", []form.Field{field}, list,
			func() string { return "Add nameserver " + canonical(entry) + "?" }, nil, false)
	}
	remove := func() (WorkflowStep, flow.Flow) {
		var gone string
		var opts []form.Option[string]
		for _, ns := range h.configured {
			opts = append(opts, form.Option[string]{Label: ns, Value: ns})
		}
		field := &form.Select[string]{Title: "Select a nameserver to remove", Options: opts, Value: &gone}
		list := func() []string {
			return slices.DeleteFunc(slices.Clone(h.configured), func(ns string) bool { return ns == gone })
		}
		return h.dnsFlow("Remove nameserver", []form.Field{field}, list,
			func() string { return "Remove nameserver " + gone + "?" }, nil, false)
	}
	out = append(out,
		Mock{Name: h.workflow("Add nameserver"), Source: src(dnsAddDelta), Sequences: []Sequence{{Name: "normal", Build: add,
			Answers: []Answer{{"nameserver", "999.1.1.1"}, {"nameserver", "192.0.2.54"}, {"nameserver", "192.0.2.53"}, {"confirm", "y"}},
			Ends:    flow.Applied}}},
		Mock{Name: h.workflow("Remove nameserver"), Source: src(dnsAddDelta), Sequences: []Sequence{{Name: "normal", Build: remove,
			Answers: []Answer{{"nameserver", "2"}, {"confirm", "y"}}, Ends: flow.Applied}}})
	for _, p := range dnsPresets {
		preset := func(dryRun bool) func() (WorkflowStep, flow.Flow) {
			return func() (WorkflowStep, flow.Flow) {
				return h.dnsFlow(p.label, nil, func() []string { return p.nameservers },
					func() string { return p.label + "?" }, nil, dryRun)
			}
		}
		seqs := []Sequence{{Name: "normal", Build: preset(false), Answers: confirm("y"), Ends: flow.Applied}}
		if p.key == "4" {
			seqs = append(seqs, Sequence{Name: "dry-run", Build: preset(true), Ends: flow.Planned},
				Sequence{Name: "cancelled", Build: preset(false), Answers: confirm("n"), Ends: flow.Cancelled})
		}
		out = append(out, Mock{Name: h.workflow(p.label), Source: src(dnsPresetDelta), Sequences: seqs})
	}
	return out
}

// dnsNotEffective is how the engine reports the Ubuntu host's change
// failing when systemd-resolved reloads with no effect: dns.Apply's error
// with its code, after the change's title (pkg/output's ErrorReport;
// gallery-dns.md records that it names nothing rolled back, though
// dns.Apply put the drop-in back).
var dnsNotEffective = dnsFailure{Failure: flow.Failure{
	Err: errors.New(dnsChange + `: dns: reload-resolved: global nameservers [], want ["192.0.2.54" "2001:db8::54"]: ` +
		"the step ran, but the host does not show its effect (DNS_NOT_EFFECTIVE)"),
	LogPath: "/var/log/hardn.log",
}}

// dnsUndone is the same failure as proposed for unit (b): the change
// reported rolled back, as dns.Apply undid it, and the report in a few
// words, its code on the step's line alone (proposed words).
var dnsUndone = dnsFailure{Failure: flow.Failure{
	Err:     errors.New("Reloading systemd-resolved had no effect"),
	LogPath: "/var/log/hardn.log",
	Next:    "Run resolvectl dns to see the servers in use.",
}, undone: true}

// The DNS mocks' sources: origin/main's screen and actions, their delta
// from v0.3.2 and what waits on the maintainer.
const (
	dnsScreenDelta = "v0.3.2's DNS item warns and asks \"Do you want to continue? (y/n)\", then applies the configured " +
		"nameservers (pkg/menu/menu.go:519-541). origin/main shows the nameservers in use and those configured, " +
		"and six items, item 1 applying them with no confirmation (pkg/menu/dns_menu.go)."
	dnsConfigureDelta = "v0.3.2 asks \"Do you want to continue? (y/n)\", then applies the configured nameservers, with lan " +
		"forced (pkg/menu/menu.go:519-541). origin/main applies them on selection, with no plan and no confirmation."
	dnsAddDelta = "v0.3.2 has no such action. origin/main changes hardn.yml only, in ./hardn.yml, and wipes its message " +
		"by redrawing the screen (W1, W2; side-effects.md row 29)."
	dnsPresetDelta = "v0.3.2 has no presets. origin/main sets them in ./hardn.yml only, applying nothing."
	dnsPending     = "Every change planned, reviewed and confirmed y/N, and saved only after it applies, proposed; " +
		"the words new to origin/main proposed (docs/engine/gallery-dns.md)."
)

// dnsMocks returns the DNS mocks: each host's screen, with its refusals,
// and its workflows.
func dnsMocks() []Mock {
	var out []Mock
	for _, h := range dnsHosts() {
		label := "inferred from origin/main; the host: " + h.desc
		if h.name == "" {
			label += "; the Figma export holds a screenshot of its legacy screen, images/03-dns-legacy-tui.png"
		}
		pending := "Its items through the plan, the search domains in use and the hosts hardn only reports, proposed (docs/engine/gallery-dns.md)."
		switch len(h.configured) {
		case 0:
			pending = "Its empty list as P-30 (b) has it, muted, and item 3 left out, proposed (docs/engine/gallery-dns.md)."
		case 1:
			pending = "Remove nameserver refused while one is configured, proposed (docs/engine/gallery-dns.md)."
		}
		out = append(out, Mock{Name: h.mock(), Screen: h.screen(), Refusals: h.refusals(),
			Source: Source{Label: label, Delta: dnsScreenDelta, Pending: pending}})
		out = append(out, h.workflows()...)
	}
	return out
}
