package characterization

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/abbott/hardn/pkg/config"
)

// The DNS menu's items that save hardn.yml (pkg/menu/dns_menu.go): Add
// nameserver, Remove nameserver and the three presets change the list in
// memory, then call config.SaveConfig(m.config, "hardn.yml"), which writes
// the whole configuration to ./hardn.yml in the working directory, whatever
// file was loaded (side-effects.md row 29). Nothing pins that end state
// elsewhere; the DNS parity table (slice 2, unit (b2): pkg/screens/dns
// TestParity) reads these goldens.
//
// The menu itself is not run: DNSMenu.Show runs systemctl and resolvectl and
// reads /etc/resolv.conf for real (getCurrentDnsSettings), and its item
// handlers are unexported. Each case mirrors its item's list edit and
// message (dnsMenuEdit), as hardeningConfigFromCLI mirrors cmd/hardn, and
// the save is the real config.SaveConfig, in a scratch working directory,
// on a configuration config.LoadConfig read from a scratch copy of a fixture
// (hardn -f).

// menuDNSCase is one choice of the DNS menu that may save hardn.yml.
type menuDNSCase struct {
	name    string   // golden stem
	op      string   // the item and its handler, as the golden names them
	fixture string   // the hardn.yml loaded, under testdata/fixtures/config
	typed   []string // the item, then what is typed at its prompt
	dryRun  bool     // cfg.DryRun, as -n sets it (cmd/hardn/main.go:159)
	notes   []string
}

const (
	menuDNSFixture      = "dns-menu.yml"     // nameservers 192.0.2.53, 192.0.2.54
	menuDNSFixtureOne   = "dns-menu-one.yml" // nameservers 192.0.2.53
	menuAdd             = "Add nameserver: addNameserver (pkg/menu/dns_menu.go:162-166, 256-301)"
	menuRemove          = "Remove nameserver: removeNameserver (pkg/menu/dns_menu.go:168-178, 304-342), offered only with a list (:86-92)"
	menuWiped           = "No confirmation. The message is wiped at once by m.Show()'s redraw (interaction.md §5, W1 for Add, W2 for Remove)."
	menuPresetShown     = "No confirmation. The message stays until Enter; the host is not changed."
	menuDNSLoadedMarker = "<loaded>"
	menuDNSWorkMarker   = "<work>"
)

func menuDNSCases() []menuDNSCase {
	preset := func(name, key, label, lines string) menuDNSCase {
		return menuDNSCase{name: name, op: "Use " + label + ": dns_menu.go:" + lines, fixture: menuDNSFixture, typed: []string{key}, notes: []string{menuPresetShown}}
	}
	return []menuDNSCase{
		{name: "add", op: menuAdd, fixture: menuDNSFixture, typed: []string{"2", "192.0.2.55"}, notes: []string{menuWiped}},
		{name: "add-unusable", op: menuAdd, fixture: menuDNSFixture, typed: []string{"2", "999.1.1.1"}, notes: []string{menuWiped,
			"The address check is four dot-separated parts (dns_menu.go:266-272): 999.1.1.1 passes (interaction.md §10)."}},
		{name: "add-ipv6", op: menuAdd, fixture: menuDNSFixture, typed: []string{"2", "2001:db8::55"}, notes: []string{menuWiped,
			"The address check is four dot-separated parts (dns_menu.go:266-272): an IPv6 address is refused."}},
		{name: "remove", op: menuRemove, fixture: menuDNSFixture, typed: []string{"3", "2"}, notes: []string{menuWiped}},
		{name: "remove-last", op: menuRemove, fixture: menuDNSFixtureOne, typed: []string{"3", "1"}, notes: []string{menuWiped,
			"The last nameserver is removed: hardn.yml is saved with none, which hardn -g and Run All then apply (ops/dns_empty_nameservers)."}},
		preset("cloudflare", "4", "Cloudflare DNS", "180-197"),
		preset("google", "5", "Google DNS", "199-216"),
		preset("quad9", "6", "Quad9 DNS", "218-235"),
		{name: "add-dry-run", op: menuAdd, fixture: menuDNSFixture, typed: []string{"2", "192.0.2.55"}, dryRun: true, notes: []string{menuWiped,
			"cfg.DryRun is set, as hardn -n sets it: Add, Remove and the presets do not read it (dns_menu.go:162-235), so the save runs and writes dryRun: true (side-effects.md row 29)."}},
	}
}

// dnsPresets are the menu's presets (dns_menu.go:180-235), by item.
var dnsPresets = map[string]struct {
	label string
	list  []string
}{
	"4": {"Cloudflare DNS", []string{"1.1.1.1", "1.0.0.1"}},
	"5": {"Google DNS", []string{"8.8.8.8", "8.8.4.4"}},
	"6": {"Quad9 DNS", []string{"9.9.9.9", "149.112.112.112"}},
}

// dnsMenuEdit mirrors the list edit of the DNS menu's item on ns: the list
// after it, whether the item then calls config.SaveConfig, and the message
// it prints (pkg/menu/dns_menu.go: addNameserver :256-301, removeNameserver
// :304-342, the presets :180-235).
func dnsMenuEdit(item, entry string, ns []string) (list []string, save bool, message string) {
	switch item {
	case "2":
		switch {
		case entry == "":
			return ns, false, "Nameserver cannot be empty"
		case len(strings.Split(entry, ".")) != 4:
			return ns, false, "Invalid IP address format"
		case slices.Contains(ns, entry):
			return ns, false, "Nameserver " + entry + " is already configured"
		}
		return append(slices.Clone(ns), entry), true, "Nameserver " + entry + " added to configuration"
	case "3":
		num := 0
		if n, err := fmt.Sscanf(entry, "%d", &num); err != nil || n != 1 {
			return ns, false, "Invalid nameserver number: not a valid number"
		}
		if num < 1 || num > len(ns) {
			return ns, false, "Invalid nameserver number: out of range"
		}
		removed := ns[num-1]
		return slices.Delete(slices.Clone(ns), num-1, num), true, "Nameserver " + removed + " removed from configuration"
	}
	p := dnsPresets[item]
	return slices.Clone(p.list), true, "Nameservers set to " + p.label + ": " + strings.Join(p.list, ", ")
}

// TestMenuDNSGoldens records what each DNS menu item that saves hardn.yml
// leaves: the list, ./hardn.yml as config.SaveConfig writes it, and the file
// loaded, untouched. The same on every distribution: nothing on this path
// reads the host.
func TestMenuDNSGoldens(t *testing.T) {
	for _, c := range menuDNSCases() {
		t.Run(c.name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("testdata", "fixtures", "config", c.fixture))
			if err != nil {
				t.Fatal(err)
			}
			// The item runs in a subtest, whose t.Chdir is undone when it
			// ends, so that the golden is read and written here.
			var got string
			t.Run("in a scratch working directory", func(t *testing.T) { got = menuDNSRun(t, c, src) })
			if got != "" {
				assertGolden(t, "menu_dns/"+c.name+".golden", got)
			}
		})
	}
}

// menuDNSRun runs c's item on a scratch copy of src, its fixture, in a
// scratch working directory, and returns the golden's text.
func menuDNSRun(t *testing.T, c menuDNSCase, src []byte) string {
	dir := t.TempDir()
	// DetectEnvVarLoss runs `su` when SUDO_UID is set (pkg/config/config.go:310-342).
	for k, v := range map[string]string{"HOME": dir, "SUDO_UID": "", "SUDO_USER": "", "HARDN_CONFIG": ""} {
		t.Setenv(k, v)
	}
	loaded := filepath.Join(dir, "etc", "hardn", "hardn.yml")
	work := filepath.Join(dir, "work")
	for _, d := range []string{filepath.Dir(loaded), work} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := syscall.Umask(0o022) // os.WriteFile's mode is the umask's to cut
	defer syscall.Umask(old)
	if err := os.WriteFile(loaded, src, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)

	cfg, err := config.LoadConfig(loaded) // hardn -f <file> (cmd/hardn/main.go:151)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DryRun = c.dryRun
	entry := ""
	if len(c.typed) > 1 {
		entry = c.typed[1]
	}
	list, save, message := dnsMenuEdit(c.typed[0], entry, cfg.Nameservers)
	saveResult := "not called"
	if save {
		cfg.Nameservers = list
		saveResult = errText(config.SaveConfig(cfg, "hardn.yml"))
	}

	var b strings.Builder
	b.WriteString(baselineHeader + "\n")
	b.WriteString("# Generated by TestMenuDNSGoldens (pkg/characterization); regenerate with -update. Do not edit.\n")
	fmt.Fprintf(&b, "# Operation: the DNS menu's %s, then config.SaveConfig(m.config, \"hardn.yml\")\n", c.op)
	b.WriteString("# Reached by: hardn's menu, 3 (DNS), then the item; on every distribution alike\n")
	if save {
		fmt.Fprintf(&b, "# KNOWN-DEFECT: V18 — %s\n", vText["V18"])
	}
	if save && len(list) == 0 {
		b.WriteString("# KNOWN-DEFECT: V19 — DNS: the list is saved empty, and an empty list is accepted and written by hardn -g and Run All\n")
	}
	notes := append([]string{
		"The menu is not run (its Show runs systemctl and resolvectl and reads /etc/resolv.conf for real): the list edit and the message mirror the item's code (dnsMenuEdit); the save is the real config.SaveConfig.",
		"config.SaveConfig writes the whole configuration, cfg.DryRun included, to ./hardn.yml in the working directory, whatever file was loaded (side-effects.md row 29).",
	}, c.notes...)
	for _, n := range notes {
		fmt.Fprintf(&b, "# NOTE: %s\n", n)
	}
	b.WriteString("\n== fixture\n")
	fmt.Fprintf(&b, "loaded: %s mode=0644, testdata/fixtures/config/%s, passed as -f (cmd/hardn/main.go:151)\n", menuDNSLoadedMarker, c.fixture)
	fmt.Fprintf(&b, "working directory: %s, empty; umask 022\n", menuDNSWorkMarker)
	fmt.Fprintf(&b, "cfg.DryRun: %t\n", c.dryRun)
	fmt.Fprintf(&b, "typed: %s\n", strings.Join(c.typed, ", "))

	b.WriteString("\n== result\n")
	fmt.Fprintf(&b, "message: %s\n", message)
	if len(list) == 0 {
		b.WriteString("list: none\n")
	} else {
		fmt.Fprintf(&b, "list: %s\n", strings.Join(list, ", "))
	}
	fmt.Fprintf(&b, "config.SaveConfig: %s\n", saveResult)
	after, err := os.ReadFile(loaded)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "loaded file unchanged: %t\n", bytes.Equal(after, src))
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		b.WriteString("written: none\n")
	} else {
		b.WriteString("written:\n")
	}
	log := &trace{}
	for _, e := range entries {
		name := filepath.Join(work, e.Name())
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		log.add("write  ./%s mode=%s", e.Name(), mode(info.Mode()))
		log.addContent(data)
	}
	if len(entries) > 0 {
		b.WriteString(log.String())
	}

	got := b.String()
	if strings.Contains(got, dir) {
		t.Fatalf("golden would contain the temp dir %s", dir)
	}
	return got
}
