package dns_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The DNS parity table (refactor plan §2, slice 2's unit (b2),
// next/s2-parity; docs/engine/parity-dns.md): origin/main's DNS path, read
// from its characterization goldens, against the new path on the same
// distributions with the same hardn.yml.

// parityScenario is a row of the table, run on every distribution.
type parityScenario struct {
	name    string
	golden  string // origin/main's, under legacyGoldens; {d} is the distribution
	state   string // the host state the new path runs on; "" the stock install's
	fixture string // hardn.yml, under fixtures
	// lines are a DNS screen item's answers, "{u}" udhcpc's question (n on
	// Alpine's file, left out elsewhere); nil for (a), hardn -g's plan and
	// apply, which the screen's Configure DNS must match.
	lines  []string
	dryRun bool
	v032   string // v0.3.2 where it differs, from v032-reference.md
}

// v032Backups is v0.3.2 for the strategies: v032-reference.md §3 rows
// 13-15, §4.5, and §5.1 (V18, V19: "backups only" regressed).
const v032Backups = "the same file, but backed up first: the backups regressed (V18, V19)"

// v032Menu is v0.3.2 for the menu's items: it had no DNS screen
// (gallery-dns.md §7), so no Add, Remove or presets.
const v032Menu = "no such item: v0.3.2 had no DNS screen"

var parityScenarios = []parityScenario{
	// (a) DNSManager.ConfigureDNS(cfg.Nameservers, "lan"): hardn -g, the
	// menu's Configure DNS, Run All. The strategy is the legacy harness's
	// forced probes (setDNSStrategy); the empty list its stock ones.
	{name: "resolved", golden: "ops/dns_systemd_resolved/{d}.golden", state: stateResolved, fixture: "dns-menu.yml", v032: v032Backups},
	{name: "resolvconf", golden: "ops/dns_resolvconf/{d}.golden", state: stateResolvconf, fixture: "dns-menu.yml",
		v032: "the same, the first 2 nameservers only, backed up first: the backups regressed (V18, V19)"},
	{name: "direct", golden: "ops/dns_direct/{d}.golden", state: stateFile, fixture: "dns-menu.yml",
		v032: "the same 4 lines, the first 2 nameservers only, backed up first: the backups regressed (V18, V19)"},
	{name: "empty", golden: "ops/dns_empty_nameservers/{d}.golden", fixture: "dns-menu-empty.yml",
		v032: "resolved: DNS= and a restart; a file: a backup, then a panic. v0.3.2's own defect, not carried over (§6 item 3)"},
	// (b) The menu's items that save hardn.yml (pkg/characterization
	// TestMenuDNSGoldens), on each distribution's stock install.
	{name: "add", golden: "menu_dns/add.golden", fixture: "dns-menu.yml", lines: []string{"2", "192.0.2.55", "{u}", "y"}, v032: v032Menu},
	{name: "add-unusable", golden: "menu_dns/add-unusable.golden", fixture: "dns-menu.yml", lines: []string{"2", "999.1.1.1", "0"}, v032: v032Menu},
	{name: "add-ipv6", golden: "menu_dns/add-ipv6.golden", fixture: "dns-menu.yml", lines: []string{"2", "2001:db8::55", "{u}", "y"}, v032: v032Menu},
	{name: "remove", golden: "menu_dns/remove.golden", fixture: "dns-menu.yml", lines: []string{"3", "2", "{u}", "y"}, v032: v032Menu},
	{name: "remove-last", golden: "menu_dns/remove-last.golden", fixture: "dns-menu-one.yml", lines: []string{"3"}, v032: v032Menu},
	{name: "cloudflare", golden: "menu_dns/cloudflare.golden", fixture: "dns-menu.yml", lines: []string{"4", "{u}", "y"}, v032: v032Menu},
	{name: "google", golden: "menu_dns/google.golden", fixture: "dns-menu.yml", lines: []string{"5", "{u}", "y"}, v032: v032Menu},
	{name: "quad9", golden: "menu_dns/quad9.golden", fixture: "dns-menu.yml", lines: []string{"6", "{u}", "y"}, v032: v032Menu},
	{name: "add-dry-run", golden: "menu_dns/add-dry-run.golden", fixture: "dns-menu.yml", lines: []string{"2", "192.0.2.55", "{u}"}, dryRun: true, v032: v032Menu},
}

// tie is an expected difference: every (scenario, distribution) it names
// differs in aspect, for the reason its source gives. No entry may name a
// difference that does not occur.
type tie struct {
	scenarios []string
	distros   []string // nil: every distribution
	aspect    string
	source    string // V-numbers, decisions, proposals, plan rows (the page's legend)
	why       string // one short line
}

// The scenarios and distributions the table names together.
var (
	menuSaves     = []string{"add", "remove", "cloudflare", "google", "quad9"}
	screenPlans   = append(slices.Clone(menuSaves), "add-ipv6", "add-dry-run") // the menu's items the screen plans
	fileHosts     = append([]string{"direct"}, screenPlans...)                 // on a file: direct, and the menu's items on a stock install
	resolvedHosts = []string{"debian", "ubuntu", "proxmox"}                    // where systemd-resolved can run
	all           []string                                                     // every distribution
	strategies    = []string{"resolvconf", "direct", "empty"}                  // (a) on every distribution
)

// parityTable is every difference, with its source: V-numbers
// (side-effects.md §8), decisions (gallery-dns.md §4, adopted 2026-10-22),
// proposals, plan rows. A difference no source explains has no entry: the
// test reports it UNEXPLAINED.
var parityTable = []tie{
	// (a) hardn -g, the menu's Configure DNS and Run All.
	{[]string{"resolved"}, resolvedHosts, "files", "V19", "resolved.conf overwritten whole; now a drop-in of hardn's own, resolved.conf untouched"},
	{[]string{"resolved"}, resolvedHosts, "search", "V19; decision 2", "Domains=lan forced; now none set, the host's search domains kept"},
	{[]string{"resolved"}, resolvedHosts, "directories", "V19", "the drop-in's directory, set 0755 whatever the umask (dns.md §2.12)"},
	{[]string{"resolved"}, resolvedHosts, "commands", "V19", "a restart; now a reload where resolved has one (systemd 256 and later: Debian 13), a restart before (Ubuntu 24.04, systemd 255; dns.md §3)"},
	{[]string{"resolved"}, resolvedHosts, "write", "V18", "written in place; now replaced whole by a rename"},
	{[]string{"direct"}, all, "write", "V18", "written in place; now replaced whole by a rename"},
	{[]string{"resolved"}, resolvedHosts, "backup", "V18", "nothing recorded; now the drop-in is recorded absent, so a rollback removes it"},
	{[]string{"direct"}, all, "backup", "V18", "nothing backed up; now the file is, first"},
	{[]string{"resolved"}, resolvedHosts, "journal", "V18", "no rollback; now journalled first, so a failure rolls it back"},
	{[]string{"direct"}, all, "journal", "V18", "no rollback; now journalled first, so a failure rolls it back"},
	{[]string{"resolved"}, resolvedHosts, "findings", "P-37 (a); decision 5", "eth0's own DHCP server, which resolved still queries, reported"},
	{[]string{"direct"}, all, "search", "V19; decision 2", "domain and search lan forced; now the file's search line kept"},
	{[]string{"direct"}, []string{"debian"}, "other lines", "V19", "the file rewritten whole; now its other lines kept (dns.md §2.8)"},
	{[]string{"resolvconf"}, all, "refusal", "P-37 (a); decision 4", "resolvconf manages DNS here: reported, not written"},
	{[]string{"resolvconf"}, all, "files", "P-37 (a); decision 4", "resolvconf's head written; now nothing"},
	{[]string{"resolvconf"}, all, "directories", "P-37 (a); decision 4", "resolvconf's directory made; now nothing"},
	{[]string{"resolvconf"}, all, "commands", "P-37 (a); decision 4", "resolvconf -u run; now nothing"},
	{[]string{"empty"}, all, "refusal", "V19", "an empty list applied; now refused before any change"},
	{[]string{"empty"}, all, "files", "V19", "DNS= or a file with no nameserver written; now nothing"},
	{[]string{"empty"}, []string{"ubuntu"}, "commands", "V19", "resolved restarted with no servers; now nothing"},
	{[]string{"resolved"}, resolvedHosts, "dry run", "V1", "below the menus cfg.DryRun is ignored (A1), and hardn -n -g is refused (exit 2); a dry run is now the plan alone"},
	{strategies, all, "dry run", "V1", "below the menus cfg.DryRun is ignored (A1), and hardn -n -g is refused (exit 2); a dry run is now the plan alone"},

	// The findings: reported (decision 5: on the screen, before the question).
	{fileHosts, []string{"debian"}, "findings", "decision 5", "dhcpcd may rewrite the file on a lease: reported"},
	{fileHosts, []string{"alpine"}, "findings", "decision 5; decision 6", "udhcpc rewrites the file on each lease (not stopped): reported"},
	{fileHosts, []string{"proxmox"}, "findings", "decision 5", "Proxmox VE's DNS settings rewrite the file when saved: reported"},
	{screenPlans, []string{"ubuntu"}, "findings", "P-37 (a); decision 5", "eth0's own DHCP server, which resolved still queries, reported"},

	// (b) The menu's items that save hardn.yml.
	{menuSaves, all, "files", "decision 1", "hardn.yml alone; now the host's nameservers too, then hardn.yml"},
	{menuSaves, []string{"ubuntu"}, "directories", "decision 1", "nothing on the host; now resolved's drop-in directory"},
	{menuSaves, []string{"ubuntu"}, "commands", "decision 1", "nothing on the host; now resolved reloaded, or restarted on systemd 255 (dns.md §3)"},
	{menuSaves, all, "write", "V18; plan §3.19", "./hardn.yml written in place; now each file replaced whole by a rename"},
	{menuSaves, []string{"debian", "alpine", "proxmox"}, "backup", "V18", "nothing backed up; now the host's file and hardn.yml are, first"},
	{menuSaves, []string{"ubuntu"}, "backup", "V18", "nothing backed up; now hardn.yml is, first, and the drop-in recorded absent"},
	{menuSaves, all, "journal", "V18", "no rollback; now journalled, so a failed save rolls the host back too (decision 1 (iii))"},
	{menuSaves, all, "hardn.yml", "side-effects row 29; plan §3.19", "./hardn.yml, last in hardn's search order (interaction.md §10); now the file loaded"},
	{menuSaves, all, "hardn.yml rest", "side-effects row 29; plan §3.19", "every setting written, comments dropped; now the list's lines alone"},
	{[]string{"add-unusable"}, all, "refusal", "decision 9", "999.1.1.1 passed the four-part check; dns.Validate refuses it"},
	{[]string{"add-unusable"}, all, "hardn.yml", "decision 9", "saved with 999.1.1.1; now nothing saved"},
	{[]string{"add-ipv6"}, all, "refusal", "decision 9", "the four-part check refused IPv6; dns.Validate takes it"},
	{[]string{"add-ipv6"}, all, "files", "decision 9; decision 1", "nothing; now the host's nameservers, then hardn.yml"},
	{[]string{"add-ipv6"}, []string{"ubuntu"}, "directories", "decision 9; decision 1", "nothing; now resolved's drop-in directory"},
	{[]string{"add-ipv6"}, []string{"ubuntu"}, "commands", "decision 9; decision 1", "nothing; now resolved reloaded, or restarted on systemd 255 (dns.md §3)"},
	{[]string{"add-ipv6"}, all, "hardn.yml", "decision 9", "nothing saved; now saved at the file loaded"},
	{[]string{"remove-last"}, all, "refusal", "decision 1 (ii)", "the last nameserver removed; now refused before asking"},
	{[]string{"remove-last"}, all, "hardn.yml", "decision 1 (ii); V19", "saved with no nameservers, which -g and Run All apply; now nothing saved"},
	{[]string{"add-dry-run"}, all, "hardn.yml", "side-effects row 29; screen-dns.md §2.8", "saved under a dry run, dryRun: true in it; now a dry run saves nothing"},
}

// parityCell is a scenario on a distribution, as both paths left it.
type parityCell struct {
	sc       parityScenario
	d        parityDistro
	state    string
	shipped  string // whether the stock install has state
	compared bool   // false: state cannot exist on d (why)
	why      string
	legacy   legacyRun
	live     endState // the new path
	dry      string   // (a): what the new path's dry run leaves
	diffs    []aspect
	agree    []aspect
}

// aspect is one compared aspect: origin/main's value and the new path's.
type aspect struct{ name, legacy, new string }

// TestParity is the parity table: each scenario on each distribution,
// origin/main's end state read from its golden and the new path's run on
// hostfake, compared aspect by aspect. It fails on a difference parityTable
// does not list (UNEXPLAINED), on an entry whose difference no longer
// occurs, and on an entry tied to a V-number origin/main's golden for the
// cell does not name. The whole table is testdata/golden/parity.golden; its
// summary is docs/engine/parity-dns.md's region (parity_render_test.go).
func TestParity(t *testing.T) {
	var cells []parityCell
	for _, sc := range parityScenarios {
		for _, d := range parityDistros {
			t.Run(sc.name+"/"+d.name, func(t *testing.T) { cells = append(cells, runCell(t, sc, d)) })
		}
	}
	if t.Failed() {
		return
	}
	checkTable(t, cells)
	golden(t, "golden/parity.golden", []byte(renderCells(cells)))
	page(t, renderPage(cells))
}

// runCell runs sc on d: origin/main's end state from its golden, the new
// path's on hostfake, and their comparison.
func runCell(t *testing.T, sc parityScenario, d parityDistro) parityCell {
	t.Helper()
	c := parityCell{sc: sc, d: d, state: sc.state, compared: true}
	if c.state == "" {
		c.state = d.stock
	}
	c.shipped = shipped(c.state, d)
	c.legacy = readLegacy(t, strings.ReplaceAll(sc.golden, "{d}", d.name))
	if c.state == stateResolved {
		if hd, err := hostdns.HostOf(d.facts); err != nil || hd.Systemd != detect.Yes {
			c.compared, c.why = false, fmt.Sprintf("systemd-resolved cannot run on %s, whose init is %s (dns.HostOf: Systemd %v; docs/safety/dns.md §2.1); there origin/main takes the direct strategy (direct on %s)", d.label, d.facts.Init.Kind, hd.Systemd, d.label)
			return c
		}
	}
	src, err := os.ReadFile(filepath.Join(fixtures, sc.fixture))
	if err != nil {
		t.Fatal(err)
	}
	loaded := strings.Split(strings.TrimSuffix(string(src), "\n"), "\n")
	if sc.lines == nil {
		c.live, c.dry = configure(t, c)
	} else {
		c.live = menuItem(t, c)
	}
	c.legacy.live.loaded, c.live.loaded = loaded, loaded
	c.diffs, c.agree = compare(c.legacy.live, c.live)
	if sc.lines == nil {
		c.diffs = append(c.diffs, aspect{"dry run", legacyDry(c.legacy), c.dry})
	}
	return c
}

// shipped says whether d's stock install has state (docs/safety/dns.md
// §3), separating the hosts that exist from those the harness forced.
func shipped(state string, d parityDistro) string {
	switch {
	case state == d.stock:
		return "real: " + d.label + "'s stock install"
	case state == stateResolved && d.name == "alpine":
		return "cannot exist: Alpine has no systemd"
	case state == stateResolved && d.name == "debian":
		return "forced: a stock Debian 13 keeps a regular file; Debian 12's cloud image links resolved's file"
	case state == stateResolvconf && d.name == "debian":
		return "forced: real only with Debian's resolvconf package installed"
	}
	return "forced: not " + d.label + "'s stock install"
}

// parityEngine is the engine on h, as the screen's tests build it.
func parityEngine(h *fakeHost) *engine.Engine {
	return engine.New(h, engine.Options{
		Revert:  revert.Options{Now: func() time.Time { return now }, Hardn: "/usr/local/bin/hardn"},
		Detect:  func(context.Context, host.System) (detect.Facts, error) { return h.facts, nil },
		Hold:    func() func() { return func() {} },
		PID:     5000,
		Backups: backup.Settings{Path: backup.DefaultPath, Enabled: true},
	})
}

// configure is (a) on the new path: the DNS control's plan, then its apply,
// as hardn -g will run it (screen-dns.md §7), and its dry run, the plan
// alone. The screen's Configure DNS must leave the host as the apply does,
// and its dry run nothing.
func configure(t *testing.T, c parityCell) (live endState, dry string) {
	t.Helper()
	ctx := context.Background()
	h, cfg := parityHost(t, c.d, c.state, c.sc.fixture)
	n := len(h.Calls())
	p, err := parityEngine(h).Plan(ctx, dnsctl.Controls(cfg), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ds := planned(newState(t, h, n), p)
	if ds.wrote() || len(ds.actions) > 0 || len(ds.dirs) > 0 {
		t.Errorf("the dry run changed the host: %+v", ds)
	}
	dry = "the plan alone: nothing written"
	if ds.refusal != "" {
		dry = "refused (" + ds.refusal + "): nothing written"
	}

	h, cfg = parityHost(t, c.d, c.state, c.sc.fixture)
	n = len(h.Calls())
	e := parityEngine(h)
	p, err = e.Plan(ctx, dnsctl.Controls(cfg), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p.Err() == nil {
		if _, err := e.Apply(ctx, p); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	live = planned(newState(t, h, n), p)
	if ds.refusal != live.refusal || !slices.Equal(ds.findings, live.findings) {
		t.Errorf("the dry run's refusal and findings %q %q, the apply's %q %q", ds.refusal, ds.findings, live.refusal, live.findings)
	}

	for _, dryRun := range []bool{false, true} {
		h, cfg = parityHost(t, c.d, c.state, c.sc.fixture)
		n = len(h.Calls())
		lines := answers(c, []string{"1", "{u}", "y"})
		if live.refusal != "" { // refused before asking, or by the plan: nothing to answer
			lines = []string{"1"}
		} else if dryRun {
			lines = lines[:len(lines)-1]
		}
		pl := play(h, cfg, wide, dryRun, append(lines, "0")...)
		if pl.err != nil || pl.unread != 0 {
			t.Fatalf("the screen's Configure DNS: %v, %d unread:\n%s", pl.err, pl.unread, pl.out)
		}
		got, want := hostText(newState(t, h, n)), hostText(live)
		if dryRun {
			want = hostText(endState{})
		}
		if got != want {
			t.Errorf("the screen's Configure DNS (dry run %t) left\n%s\nhardn -g's plan and apply\n%s", dryRun, got, want)
		}
	}
	return live, dry
}

// menuItem is (b) on the new path: the DNS screen's item, answered as
// sc.lines, then 0, on d's stock install.
func menuItem(t *testing.T, c parityCell) endState {
	t.Helper()
	h, cfg := parityHost(t, c.d, c.state, c.sc.fixture)
	n := len(h.Calls())
	pl := play(h, cfg, wide, c.sc.dryRun, append(answers(c, c.sc.lines), "0")...)
	if pl.err != nil || pl.unread != 0 || len(pl.runs) > 1 {
		t.Fatalf("the screen: %v, %d unread, %d runs:\n%s", pl.err, pl.unread, len(pl.runs), pl.out)
	}
	s := newState(t, h, n)
	if len(pl.runs) == 1 {
		return planned(s, pl.runs[0].Plan)
	}
	for _, w := range screenRefusals {
		if strings.Contains(pl.out, w) {
			s.refusal = w
			return s
		}
	}
	t.Fatalf("nothing planned, and no refusal the table knows:\n%s", pl.out)
	return s
}

// screenRefusals are the screen's words for an item it stops before any
// plan (screen-dns.md §2.8): Add's inline errors, Remove's last nameserver.
var screenRefusals = []string{"Invalid IP address format", "Not a usable nameserver address", "The last nameserver cannot be removed."}

// answers is lines for c's host: udhcpc's question answered n where the
// screen asks it (Alpine's file: screen-dns.md §2.8), dropped elsewhere.
func answers(c parityCell, lines []string) []string {
	var out []string
	for _, l := range lines {
		switch {
		case l != "{u}":
			out = append(out, l)
		case c.d.name == "alpine" && c.state == stateFile:
			out = append(out, "n")
		}
	}
	return out
}

// hostText is what s changed: the files with their content, the
// directories, the commands and hardn.yml.
func hostText(s endState) string {
	var b strings.Builder
	for _, f := range append(slices.Clone(s.files), ymlOf(s)...) {
		fmt.Fprintf(&b, "%s\n%s\n", f, strings.Join(f.lines, "\n"))
	}
	fmt.Fprintf(&b, "dirs %q\nactions %q\n", s.dirs, s.actions)
	return b.String()
}

func ymlOf(s endState) []fileWrite {
	if s.yml == nil {
		return nil
	}
	return []fileWrite{*s.yml}
}

// legacyDry is what origin/main's cfg.DryRun=true section did.
func legacyDry(r legacyRun) string {
	if hostText(r.dry) == hostText(r.live) {
		return "writes and runs as the live run (no layer below the menus reads cfg.DryRun)"
	}
	return "writes " + joined(names(r.dry.files)) + "; runs " + joined(r.dry.actions)
}

// compare is l and n, origin/main's end state and the new path's, aspect by
// aspect: the aspects that differ and those that agree, each with both
// values. An aspect of what was written is compared only when both wrote.
func compare(l, n endState) (diffs, agree []aspect) {
	add := func(name, lv, nv string) {
		if lv == nv {
			agree = append(agree, aspect{name, lv, nv})
		} else {
			diffs = append(diffs, aspect{name, lv, nv})
		}
	}
	add("refusal", orElse(l.refusal, "none"), orElse(n.refusal, "none"))
	add("files", joined(names(l.files)), joined(names(n.files)))
	if len(l.files) > 0 && len(n.files) > 0 {
		add("nameservers", joined(servers(l.files)), joined(servers(n.files)))
		add("search", orElse(strings.Join(search(l.files), ", "), "none set"), orElse(strings.Join(search(n.files), ", "), "none set"))
		for _, lf := range l.files {
			if i := slices.IndexFunc(n.files, func(f fileWrite) bool { return f.path == lf.path }); i >= 0 {
				add("other lines", joined(others(lf)), joined(others(n.files[i])))
				add("mode", fmt.Sprintf("%04o", uint32(lf.mode)), fmt.Sprintf("%04o", uint32(n.files[i].mode)))
			}
		}
	}
	add("directories", joined(l.dirs), joined(n.dirs))
	add("commands", joined(l.actions), joined(n.actions))
	if l.wrote() && n.wrote() {
		add("write", method(l), method(n))
		add("backup", joined(l.backups), joined(n.backups))
		add("journal", journal(l), journal(n))
	}
	add("findings", joined(l.findings), joined(n.findings))
	add("hardn.yml", saved(l), saved(n))
	if l.yml != nil && n.yml != nil {
		add("hardn.yml list", joined(l.list), joined(n.list))
		add("hardn.yml rest", rest(*l.yml, l.loaded), rest(*n.yml, n.loaded))
		add("hardn.yml mode", fmt.Sprintf("%04o", uint32(l.yml.mode)), fmt.Sprintf("%04o", uint32(n.yml.mode)))
	}
	return diffs, agree
}

func orElse(s, none string) string {
	if s == "" {
		return none
	}
	return s
}

func joined(l []string) string { return orElse(strings.Join(l, ", "), "none") }

func names(fs []fileWrite) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.String())
	}
	return out
}

// servers are the nameservers fs set: a file's nameserver lines, or the
// DNS= lists of a resolved configuration, an empty one resetting them.
func servers(fs []fileWrite) []string {
	var out []string
	for _, f := range fs {
		for _, l := range f.lines {
			if v, ok := strings.CutPrefix(l, "nameserver "); ok {
				out = append(out, v)
			}
			if v, ok := strings.CutPrefix(l, "DNS="); ok {
				if v == "" {
					out = nil
				}
				out = append(out, strings.Fields(v)...)
			}
		}
	}
	return out
}

// search are the search domain lines fs hold: domain and search, or
// Domains=.
func search(fs []fileWrite) []string {
	var out []string
	for _, f := range fs {
		for _, l := range f.lines {
			if strings.HasPrefix(l, "domain ") || strings.HasPrefix(l, "search ") || strings.HasPrefix(l, "Domains=") {
				out = append(out, l)
			}
		}
	}
	return out
}

// others are f's lines that set neither nameservers nor search domains.
func others(f fileWrite) []string {
	var out []string
	for _, l := range f.lines {
		if len(servers([]fileWrite{{lines: []string{l}}})) == 0 && len(search([]fileWrite{{lines: []string{l}}})) == 0 && l != "DNS=" {
			out = append(out, l)
		}
	}
	return out
}

func method(s endState) string {
	for _, f := range append(slices.Clone(s.files), ymlOf(s)...) {
		if !f.atomic {
			return "in place"
		}
	}
	return "atomic"
}

func journal(s endState) string {
	if s.journal {
		return "journalled first"
	}
	return "none"
}

func saved(s endState) string {
	switch {
	case s.yml == nil:
		return "not saved"
	case s.yml.path == "./hardn.yml":
		return "./hardn.yml, in the working directory"
	}
	return s.yml.path + ", the file loaded"
}

var topLevelKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*:`)

// rest is what a saved hardn.yml holds besides the list: its settings,
// and how many of the comment lines of loaded, the file loaded, it kept.
func rest(f fileWrite, loaded []string) string {
	keys, kept, comments := 0, 0, 0
	for _, l := range f.lines {
		if topLevelKey.MatchString(l) {
			keys++
		}
	}
	for _, l := range loaded {
		if strings.HasPrefix(l, "#") {
			comments++
			if slices.Contains(f.lines, l) {
				kept++
			}
		}
	}
	switch kept {
	case 0:
		return fmt.Sprintf("%d settings; the file's %d comment lines dropped", keys, comments)
	case comments:
		return fmt.Sprintf("%d settings; the file's %d comment lines kept", keys, comments)
	}
	return fmt.Sprintf("%d settings; %d of the file's %d comment lines kept", keys, kept, comments)
}

// key names one difference.
type key struct{ scenario, distro, aspect string }

// checkTable holds every difference to parityTable: none unlisted, every
// entry occurring, each V-number of a source one origin/main's golden for
// the cell names.
func checkTable(t *testing.T, cells []parityCell) {
	t.Helper()
	want, twice := expand()
	for _, k := range twice {
		t.Errorf("the table lists %s on %s, %s twice", k.scenario, k.distro, k.aspect)
	}
	seen := map[key]bool{}
	for _, c := range cells {
		for _, d := range c.diffs {
			k := key{c.sc.name, c.d.name, d.name}
			seen[k] = true
			e, ok := want[k]
			if !ok {
				t.Errorf("UNEXPLAINED %s on %s, %s: origin/main %q, new %q", c.sc.name, c.d.name, d.name, d.legacy, d.new)
				continue
			}
			for _, v := range vNumber.FindAllString(e.source, -1) {
				if !slices.Contains(c.legacy.vs, v) {
					t.Errorf("%s on %s, %s: tied to %s, which %s does not name (it names %v)", c.sc.name, c.d.name, d.name, v, c.legacy.golden, c.legacy.vs)
				}
			}
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("the table lists %s on %s, %s, which no longer differs", k.scenario, k.distro, k.aspect)
		}
	}
}

var vNumber = regexp.MustCompile(`\bV\d+\b`)

// expand is parityTable by key, and the keys it lists more than once.
func expand() (map[key]tie, []key) {
	out := map[key]tie{}
	var twice []key
	for _, e := range parityTable {
		for _, s := range e.scenarios {
			for _, d := range distrosOf(e) {
				k := key{s, d, e.aspect}
				if _, dup := out[k]; dup {
					twice = append(twice, k)
				}
				out[k] = e
			}
		}
	}
	return out, twice
}
