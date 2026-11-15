package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The main menu's layout (menulayout.go).

// TestMenuLayoutGolden pins each mock on the core set and under LANG=C.
func TestMenuLayoutGolden(t *testing.T) { goldenListing(t, mlListing) }

// mlDraw is mock name's drawing with no colour, in one string.
func mlDraw(t *testing.T, name string) string {
	t.Helper()
	for _, m := range mlMocks() {
		if m.name == name {
			out, err := m.draw(context.Background(), component.NewCtx(plainOff()))
			if err != nil {
				t.Fatal(err)
			}
			return ansi.Strip(out)
		}
	}
	t.Fatalf("no mock %s", name)
	return ""
}

// mlMenuText is the adopted main menu on the configured host, from the
// risk level to the Exit item: the maintainer's text of 2026-11-14 with
// what he adopted after it: "~" under a row, and the one SSH row and item
// 2's label of 2026-11-15.
const mlMenuText = `   ⛬  Risk Level           Low ≈ strong security

   ⛛  User                 + non-root, sudo
                             ~ No password to become root
   ⛛  SSH                  + key-only, no root, port 2230
   ⛛  Firewall             + deny policy
   ⛛  Kernel               + 6 items applied
   ⛛  Auto Updates         + unattended-upgrades
   ⛛  AppArmor             + 10 profiles enforced

╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌

  Select an option         Mode Live

   1) User                 Create, manage (sudo, SSH keys)
   2) SSH                  Disable root and passwords, set port
   3) Firewall             Manage rules, port listening
   4) Kernel               Harden sysctl (network, kernel)
   5) Auto Updates         Enable automatic updates
   6) Run All              Queue hardening actions
   7) Backup               Configure hardn backup
   8) Configuration        hardn.yml, dns, ntp, syslog
   9) System Details       View system info
  10) Logs                 View hardn logs

   0) Exit                 q to quit
`

// TestMenuLayoutMenu: "menu" is the adopted main menu, cell for cell,
// drawn through hardn's own mapping and items; it says "Configured"
// nowhere and has no DNS, NTP or Syslog row.
func TestMenuLayoutMenu(t *testing.T) {
	c := component.NewCtx(term.Fixed(80, colorprofile.TrueColor, true, true))
	got := ansi.Strip(mlMenu(detect.Facts{}, mlConfigured()).Render(c))
	if !strings.Contains(got, mlMenuText) {
		t.Errorf("menu is not the adopted text:\n%s", got)
	}
	for _, no := range []string{"Configured", "DNS  ", "NTP  ", "Syslog  ", "SSH Auth", "SSH Login", "SSH Port"} {
		if strings.Contains(got, no) {
			t.Errorf("menu says %q:\n%s", no, got)
		}
	}
}

// TestMenuLayoutMixed: a row that is not Configured keeps its word, with
// colour and without; the SSH row is the worse of login and auth, saying
// what is off, with nothing for a port off 22; a line under a row starts two cells in from the value
// column.
func TestMenuLayoutMixed(t *testing.T) {
	draw := mlDraw(t, "mixed")
	for _, want := range []string{
		"  [+] User                 + non-root, sudo\n                             ~ No password to become root\n",
		"  [ ] SSH                  Not Configured password auth on\n  [?] Firewall",
		"  [?] Firewall             Unknown can't read\n                             ~ Counted as not configured\n",
		"  [ ] Kernel               Partial 1 set outside hardn\n",
		"  [ ] Auto Updates         Not Configured no unattended-upgrades\n",
		"  [-] AppArmor             Not Applicable set by the host\n",
	} {
		if !strings.Contains(draw, want) {
			t.Errorf("no %q in\n%s", want, draw)
		}
	}
	if strings.Contains(draw, "SSH Auth") || strings.Contains(draw, "SSH Port") || strings.Contains(draw, strings.Repeat(" ", 5)+"Configured") {
		t.Errorf("an SSH Auth or SSH Port row, or a Configured row's word:\n%s", draw)
	}
}

// TestMenuLayoutSSH: the one SSH row's cases (the maintainer, 2026-11-15),
// each block whole, plain, at 64 columns: the row, and every line under
// it. No block has an SSH Login, SSH Auth or SSH Port row.
func TestMenuLayoutSSH(t *testing.T) {
	draw := mlDraw(t, "ssh") + "\n\n"
	wants := []string{
		"All three configured:\n  [+] SSH                  + key-only, no root, port 2230\n",
		"The port left on 22:\n  [ ] SSH                  Partial default port (22)\n",
		"Root by key, port 22:\n  [ ] SSH                  Partial root key-only\n                             ~ Default port (22)\n",
		"Passwords on, port 22:\n  [ ] SSH                  Not Configured password auth on\n                             ~ Default port (22)\n",
		"Passwords on, port 2230:\n  [ ] SSH                  Not Configured password auth on\n",
		"Root may log in, port 22:\n  [ ] SSH                  Not Configured root allowed\n                             ~ Default port (22)\n",
		"Login and auth off, one worse:\n  [ ] SSH                  Not Configured password auth on\n                             ~ Default port (22)\n",
		"Login and auth off alike:\n  [ ] SSH                  Not Configured root allowed, passw...\n                             ~ Default port (22)\n",
		"A long detail:\n  [ ] SSH                  Partial match block allows passwords\n",
		"Ports 22 and 2230:\n  [ ] SSH                  Partial ports 22 and 2230\n",
		"Root by key, ports 22 and 2230:\n  [ ] SSH                  Partial root key-only\n                             ~ Ports 22 and 2230\n",
		"Two ports, neither 22:\n  [+] SSH                  + key-only, no root, 2 ports\n",
		"Key and password:\n  [+] SSH                  + key+password, no root, port 2230\n",
		"The port not read:\n  [?] SSH                  Unknown port: not listening on 2230\n                             ~ Counted as not configured\n",
		"Passwords on, the port not read:\n  [ ] SSH                  Not Configured password auth on\n                             ~ Counted as not configured\n",
		"Passwords on, root not read, port 22:\n  [ ] SSH                  Not Configured password auth on\n                             ~ Default port (22)\n                             ~ Counted as not configured\n",
		"sshd not read:\n  [?] SSH                  Unknown can't read\n                             ~ Counted as not configured\n",
		"No SSH server:\n  [-] SSH                  Not Applicable no SSH server\n",
		"Proxmox VE, all three configured:\n  [+] SSH                  + key-only, root key-only, port 22\n",
		"Proxmox VE as installed:\n  [ ] SSH                  Not Configured root allowed, passw...\n",
		"Proxmox VE, off port 22:\n  [ ] SSH                  Partial port 2230, not 22\n",
	}
	if len(wants) != len(mlSSHCases) {
		t.Fatalf("%d cases, %d wanted", len(mlSSHCases), len(wants))
	}
	for _, want := range wants {
		if !strings.Contains(draw, want+"\n") {
			t.Errorf("no %q in\n%s", want, draw)
		}
	}
	for _, no := range []string{"SSH Login", "SSH Auth", "SSH Port"} {
		if strings.Contains(draw, no) {
			t.Errorf("a row %q in\n%s", no, draw)
		}
	}
}

// TestMenuLayoutKernel: the Kernel row's details, between Firewall and
// Auto Updates.
func TestMenuLayoutKernel(t *testing.T) {
	draw := mlDraw(t, "kernel")
	for _, want := range []string{"+ 6 items applied\n", "+ 1 item applied\n", "+ baseline default\n", "Partial 3 to set\n", "Not Configured 6 to set\n",
		"  [-] Kernel               Not Applicable in a container\n",
		"  [+] Firewall             + deny policy\n  [+] Kernel               + 6 items applied\n  [+] Auto Updates         + unattended-upgrades\n"} {
		if !strings.Contains(draw, want) {
			t.Errorf("no %q in\n%s", want, draw)
		}
	}
}

// TestMenuLayoutProblems: no row while all is fine; a row only for what
// has a problem, after a blank line, in the order DNS, NTP, Syslog: the
// bullet, no state word, the host's value and the problem under it, or
// the problem alone.
func TestMenuLayoutProblems(t *testing.T) {
	draw := mlDraw(t, "problems")
	const appArmor = "  [+] AppArmor             + 10 profiles enforced\n"
	for _, want := range []string{
		"All fine: no row.\n" + appArmor + "\nThe syslog server",
		appArmor + "\n   -  Syslog               10.2.8.240:514 TCP\n                             ~ Unreachable\n",
		appArmor + "\n   -  DNS                  192.168.1.1\n                             ~ Not hardn.yml's\n" +
			"   -  NTP                  10.2.8.1\n                             ~ Not in sync\n" +
			"   -  Syslog               3 servers\n                             ~ 1 unreachable\n",
		appArmor + "\n   -  NTP                  No time service\n   -  Syslog               Not forwarding\n",
		appArmor + "\n   -  DNS                  Can't read\n   -  NTP                  chrony not running",
	} {
		if !strings.Contains(draw, want) {
			t.Errorf("no %q in\n%s", want, draw)
		}
	}
}

// TestMenuLayoutLong: a detail of 35 cells fills the row after "+ ", and a
// longer one is cut.
func TestMenuLayoutLong(t *testing.T) {
	if len(mlDetail35) != 35 || len(mlDetail40) != 40 {
		t.Fatalf("details of %d and %d cells", len(mlDetail35), len(mlDetail40))
	}
	draw := mlDraw(t, "long-detail")
	for _, want := range []string{"  [+] Firewall             + " + mlDetail35 + "\n", "  [+] Firewall             + deny policy, 14 rules, 3 profile..."} {
		if !strings.Contains(draw, want) {
			t.Errorf("no %q in\n%s", want, draw)
		}
	}
	for _, l := range strings.Split(draw, "\n") {
		if len(l) > theme.ContentWidth {
			t.Errorf("%d cells: %q", len(l), l)
		}
	}
}

// TestMenuLayoutHosts: items 3 and 5 on Rocky Linux and on Proxmox VE, and
// the Configuration group's items.
func TestMenuLayoutHosts(t *testing.T) {
	for name, want := range map[string][]string{
		"el":          {"   3) Firewall             Manage firewalld, port listening\n", "   5) Auto Updates         Enable automatic updates\n", "  [-] AppArmor             Not Applicable uses SELinux\n"},
		"pve":         {"   3) Firewall             View pve-firewall\n", "   5) Auto Updates         Configure automatic updates\n", "  [+] Kernel               + 4 items applied\n"},
		"config-menu": {"   1) hardn.yml ", "   2) DNS Settings         Configure nameservers\n", "   3) NTP Settings ", "   4) Syslog Settings ", "[0-4 or q]"},
	} {
		draw := mlDraw(t, name)
		for _, w := range want {
			if !strings.Contains(draw, w) {
				t.Errorf("%s: no %q in\n%s", name, w, draw)
			}
		}
	}
}

// TestMenuLayoutFits: every item's words fit the value column on every
// host, and item 2's alone comes within a cell of it.
func TestMenuLayoutFits(t *testing.T) {
	const room = theme.ContentWidth - theme.ValueCol
	rocky, _ := mlRocky()
	pve, _ := mlPVE()
	for _, f := range []detect.Facts{{}, rocky, pve} {
		if drawn := mlMenu(f, mlConfigured()).Menu.Items; !slices.Equal(drawn, mainmenu.MenuOf(f).Items) {
			t.Errorf("%s: the mock's items are not hardn's:\n%+v\n%+v", f.Platform.Distro, drawn, mainmenu.MenuOf(f).Items)
		}
		for _, it := range mainmenu.MenuOf(f).Items {
			n := len(it.Description)
			if it.Aside != "" {
				n += len(" (" + it.Aside + ")")
			}
			if n > room || (it.Key == "2") != (n == room-1) {
				t.Errorf("%s: %d cells of %d", it.Label, n, room)
			}
		}
	}
}

// TestMenuLayoutList: -list names the mocks, in order, and every note
// opens with the source label.
func TestMenuLayoutList(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{mlPrefix, "-list"}, nil, offTerminal, &out, &errs); code != exitOK || errs.Len() > 0 {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}
	if got := out.String(); got != "menu\nmixed\nssh\nkernel\nproblems\nlong-detail\nconfig-menu\nel\npve\n" {
		t.Errorf("%q", got)
	}
	for _, m := range mlListing.mocks() {
		if note := mlListing.note(m); !strings.HasPrefix(note, mlLabel) {
			t.Errorf("%s: %q", m.name, note)
		}
	}
}
