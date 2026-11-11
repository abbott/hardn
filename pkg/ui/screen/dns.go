package screen

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// The DNS screen (refactor plan slice 2, unit (c), next/s2-screen):
// origin/main's DNS screen (pkg/menu dns_menu.go) on the sub-menu layout,
// as the maintainer adopted its mock on 2026-10-22 (docs/engine/
// gallery-dns.md §4, decisions 2, 3, 4 and 8), drawn from a reading of the
// host and of hardn.yml (DNS). pkg/ui cannot reach the host: pkg/screens/dns
// reads it, through the DNS control and pkg/host/dns, fills DNS and runs
// the items (docs/engine/screen-dns.md). The words are origin/main's where
// it has them; every other one is proposed (screen-dns.md, its words
// table).

// The DNS screen's words: origin/main's title, headings, rows and empty
// list; "Search Domains", "None" and "reported only" proposed.
const (
	dnsTitle          = "DNS Configuration"
	dnsCurrent        = "Current DNS Configuration"
	dnsConfigured     = "Configured Nameservers"
	dnsNoneConfigured = "No nameservers configured"
	dnsImplementation = "DNS Implementation"
	dnsServers        = "Nameservers"
	dnsSearch         = "Search Domains"
	dnsNone           = "None"
	dnsReportedOnly   = "reported only"
)

// The DNS screen's items that are not presets, by key, origin/main's.
const (
	DNSConfigure = "1" // Configure DNS: hardn.yml's list
	DNSAdd       = "2" // Add nameserver
	DNSRemove    = "3" // Remove nameserver, shown when hardn.yml lists one
)

// DNSPreset is one of origin/main's presets: its item, and the
// nameservers it sets in place of hardn.yml's list.
type DNSPreset struct {
	Key, Label  string
	Nameservers []string
}

// DNSPresets returns origin/main's three presets, IPv4 only, as the
// maintainer kept them (decision 7): Cloudflare, Google and Quad9.
func DNSPresets() []DNSPreset {
	return []DNSPreset{
		{"4", "Use Cloudflare DNS", []string{"1.1.1.1", "1.0.0.1"}},
		{"5", "Use Google DNS", []string{"8.8.8.8", "8.8.4.4"}},
		{"6", "Use Quad9 DNS", []string{"9.9.9.9", "149.112.112.112"}},
	}
}

// DNSManager is what manages /etc/resolv.conf, as the screen's "DNS
// Implementation" row names it. The zero value is not established.
type DNSManager uint8

// The managers. A manager hardn only reports (P-37 (a), decision 4) is
// named "<manager>, reported only", in status.warn.
const (
	DNSManagerUnknown DNSManager = iota // not established: Unknown
	DNSFile                             // a file hardn writes: "direct (/etc/resolv.conf)" (origin/main)
	DNSProxmox                          // Proxmox VE's file: "Proxmox VE (/etc/resolv.conf)"
	DNSResolved                         // "systemd-resolved" (origin/main)
	DNSNetworkManager                   // "NetworkManager, reported only"
	DNSResolvconf                       // "resolvconf, reported only"
	DNSOther                            // a program hardn does not know: "another program, reported only"
	DNSContainer                        // the container's manager: "container, reported only"
	DNSCloudInit                        // "cloud-init, reported only"
)

// managerWords are each manager's words, and whether hardn only reports
// it.
var managerWords = map[DNSManager]struct {
	words    string
	reported bool
}{
	DNSFile: {"direct (/etc/resolv.conf)", false}, DNSProxmox: {"Proxmox VE (/etc/resolv.conf)", false},
	DNSResolved: {"systemd-resolved", false}, DNSNetworkManager: {"NetworkManager", true},
	DNSResolvconf: {"resolvconf", true}, DNSOther: {"another program", true}, DNSContainer: {"container", true},
	DNSCloudInit: {"cloud-init", true},
}

// DNSEntry is one nameserver or search domain the host uses: Value, and
// Link, the link or delegate that has it, such as "eth0", or "" for the
// host's own list (a file's, or systemd-resolved's global one).
type DNSEntry struct {
	Value, Link string
}

// String is the entry as the screen shows it: "192.0.2.1", or
// "192.0.2.1 (eth0)" for a link's own (proposed).
func (e DNSEntry) String() string {
	if e.Link == "" {
		return e.Value
	}
	return e.Value + " (" + e.Link + ")"
}

// DNS is the DNS screen's reading of a host and of hardn.yml, which
// pkg/screens/dns fills: what manages /etc/resolv.conf, the nameservers
// and search domains in use, and hardn.yml's list.
type DNS struct {
	// Manager names what manages /etc/resolv.conf.
	Manager DNSManager
	// InUse and Search are the nameservers and the search domains the
	// host uses, its own first, then each link's (decision 2: the host's
	// search domains are kept, so the screen shows them). With Read
	// false both rows say Unknown, as for a manager hardn only reports.
	// Read, no search domain is "None"; but no nameserver is Unknown:
	// given none, a resolver uses a default of its own, which nothing
	// read (systemd-resolved its FallbackDNS= servers, which resolvectl
	// dns does not list; glibc the local machine's, resolv.conf(5)).
	InUse, Search []DNSEntry
	Read          bool
	// Configured are hardn.yml's nameservers, as written there: the
	// list the screen's items act on.
	Configured []string
}

// entries are es as a row's value: "None" for none.
func entries(es []DNSEntry) string {
	if len(es) == 0 {
		return dnsNone
	}
	words := make([]string, len(es))
	for i, e := range es {
		words[i] = e.String()
	}
	return strings.Join(words, ", ")
}

// row is the row key of what d read, its value, or Unknown when d has
// not read it.
func (d DNS) row(key string, es []DNSEntry) component.KeyValue {
	if !d.Read {
		return component.KeyValue{Key: key, State: component.ValueUnknown}
	}
	return component.KeyValue{Key: key, Value: entries(es)}
}

// inUse is the "Nameservers" row: the nameservers in use, or Unknown when
// d did not read them or read none (DNS.InUse).
func (d DNS) inUse() component.KeyValue {
	if len(d.InUse) == 0 {
		return component.KeyValue{Key: dnsServers, State: component.ValueUnknown}
	}
	return d.row(dnsServers, d.InUse)
}

// implementation is the "DNS Implementation" row: the manager's words, a
// manager hardn only reports followed by "reported only" in status.warn,
// or Unknown.
func (d DNS) implementation() component.KeyValue {
	w, ok := managerWords[d.Manager]
	switch {
	case !ok:
		return component.KeyValue{Key: dnsImplementation, State: component.ValueUnknown}
	case w.reported:
		return component.KeyValue{Key: dnsImplementation, Value: w.words + ",", State: component.ValueWarn, Note: dnsReportedOnly}
	}
	return component.KeyValue{Key: dnsImplementation, Value: w.words}
}

// SubMenu returns the DNS screen: the title bar "DNS Configuration"; the
// section "Current DNS Configuration", what manages /etc/resolv.conf and
// the nameservers and search domains in use; the section "Configured
// Nameservers", hardn.yml's list, "Nameserver 1" and on, or with none
// P-30 (b)'s muted item, "No nameservers configured" (decision 3); then
// origin/main's six items and Return, item 3 only when the list is not
// empty, so that it is neither shown nor taken with none, and its prompt
// (P-29 (a)).
func (d DNS) SubMenu() SubMenu {
	listed := make([]component.KeyValue, len(d.Configured))
	for i, ns := range d.Configured {
		listed[i] = component.KeyValue{Key: fmt.Sprintf("Nameserver %d", i+1), Value: ns}
	}
	if len(listed) == 0 {
		listed = []component.KeyValue{{Key: dnsNoneConfigured, State: component.ValueNone}}
	}
	items := []component.MenuItem{
		{Key: DNSConfigure, Label: "Configure DNS", Description: "Apply nameserver settings from configuration"},
		{Key: DNSAdd, Label: "Add nameserver", Description: "Add a new DNS server to configuration"},
	}
	if len(d.Configured) > 0 {
		items = append(items, component.MenuItem{Key: DNSRemove, Label: "Remove nameserver", Description: "Remove a DNS server from configuration"})
	}
	for _, p := range DNSPresets() {
		items = append(items, component.MenuItem{Key: p.Key, Label: p.Label, Description: "Set nameservers to " + strings.Join(p.Nameservers, ", ")})
	}
	return SubMenu{
		Title: trail(dnsTitle),
		Sections: []Section{{
			Header: component.SectionHeader{Title: dnsCurrent},
			Rows:   component.BulletList{Items: []component.KeyValue{d.implementation(), d.inUse(), d.row(dnsSearch, d.Search)}},
		}, {
			Header: component.SectionHeader{Title: dnsConfigured},
			Rows:   component.BulletList{Items: listed},
		}},
		Heading: fixtureHeading,
		Menu:    component.Menu{Items: items, Exit: returnItem()},
		Prompt:  mainPrompt,
	}
}

// Step returns the screen above the workflow of the item labelled label,
// such as "Add nameserver": its title bar, the DNS screen's as its parent
// (shown only with TitleTrail).
func (DNS) Step(label string) WorkflowStep {
	return WorkflowStep{Title: trail(dnsTitle, label)}
}

// Review is the review of a change to nameservers on the host d read: the
// nameservers it will use and the search domains it keeps (decision 2),
// under "DNS Configuration"; the flow ends it with the plan's findings
// (decision 5).
func (d DNS) Review(nameservers []string) flow.Review {
	return flow.Review{Title: dnsTitle, Items: []component.KeyValue{
		{Key: dnsServers, Value: strings.Join(nameservers, ", ")},
		d.row(dnsSearch, d.Search),
	}}
}
