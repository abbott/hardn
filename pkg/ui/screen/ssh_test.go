package screen

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The SSH Login screen as pkg/screens/ssh fills it; the hosts behind each are
// that package's goldens. Synthetic values only.
var (
	sshTestStatus = component.StatusList{Rows: []component.StatusRow{{Label: "SSH Login", State: component.StatusPartial, Detail: "root key-only"},
		{Label: "SSH Auth", State: component.StatusConfigured, Detail: "key-only auth"}, {Label: "SSH Port", State: component.StatusPartial, Detail: "default (22)"}}}
	sshTestRows = []component.KeyValue{{Key: SSHRootKey, Value: "Key only", Note: "sshd's default"}, {Key: SSHPasswordKey, Value: "Off", Note: "by sshd_config"},
		{Key: SSHPortKey, Value: "22"}, {Key: SSHUsersKey, Value: "Anyone"}, {Key: SSHYouKey, Value: "george, by key"}}
	sshTestDebian = SSH{Status: sshTestStatus, Rows: sshTestRows, PasswordsOff: true, Ports: "22"}
	sshTestLegacy = SSH{Status: sshTestStatus, Rows: append([]component.KeyValue{{Key: SSHMatchKey, Value: "Lets more in", Note: "20-site.conf"},
		{Key: SSHOldKey, Value: "manage.conf, listen.conf"}}, sshTestRows...), Root: SSHAllowRootKeyOnly, Ports: "22 and 2230", OldFiles: true}
	sshTestNotRead = SSH{Status: sshTestStatus, Rows: sshTestRows[:3], NotRead: true}
)

// TestSSHHeights (from the gallery's, gallery-ssh.md §4): the screen at 80x24
// and at 80x40, in Unicode and ASCII, is as many lines at either height, the
// prompt on its last, no line over 80 cells: over 24 rows the title scrolls
// off (Firewall's decision 9). pkg/screens/ssh's holds every host's.
func TestSSHHeights(t *testing.T) {
	draws := map[string]struct {
		vm    SubMenu
		lines int
	}{"debian13": {sshTestDebian.SubMenu(), 23}, "legacy": {sshTestLegacy.SubMenu(), 26}, "not read": {sshTestNotRead.SubMenu(), 21}}
	for name, d := range draws {
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
				env.Height = rows
				lines := strings.Split(ansi.Strip(d.vm.Render(component.NewCtx(env))), "\n")
				if len(lines) != d.lines || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s at 80x%d, unicode %v: %d lines, the last %q; want %d, the prompt", name, rows, unicode, len(lines), lines[len(lines)-1], d.lines)
				}
				for _, l := range lines {
					if w := ansi.StringWidth(l); w > 80 {
						t.Errorf("%s at 80x%d: %d cells: %q", name, rows, w, l)
					}
				}
			}
		}
	}
}

// TestSSHItems: each item named by its result, keeping its key; Move old
// settings only with old files; every description Can't read where sshd's
// settings are not read.
func TestSSHItems(t *testing.T) {
	items := func(vm SubMenu) string {
		var out []string
		for _, it := range vm.Menu.Items {
			out = append(out, it.Key+" "+it.Label+": "+it.Description)
		}
		return strings.Join(out, ", ")
	}
	for _, tt := range []struct {
		name string
		vm   SubMenu
		want string
	}{
		{"debian13", sshTestDebian.SubMenu(), "1 Disallow root login: Root can't log in over SSH, 2 Password login on: Let passwords log in again, 3 Change SSH port: Now 22"},
		{"legacy", sshTestLegacy.SubMenu(), "1 Allow root key-only: Root logs in by key alone, 2 Key-only login: Turn password login off, 3 Change SSH port: Now 22 and 2230, " +
			"4 Move old settings: Into 00-hardn.conf"},
		{"proxmox", SSH{Root: SSHPVERoot, Ports: "22"}.SubMenu(), "1 Root key-only: Kept for the Proxmox VE cluster, 2 Key-only login: Turn password login off, 3 Change SSH port: Now 22"},
		{"not read", sshTestNotRead.SubMenu(), "1 Disallow root login: Can't read, 2 Key-only login: Can't read, 3 Change SSH port: Can't read"},
		{"no ports", SSH{}.SubMenu(), "1 Disallow root login: Root can't log in over SSH, 2 Key-only login: Turn password login off, 3 Change SSH port: Now not read"},
	} {
		if got := items(tt.vm); got != tt.want {
			t.Errorf("%s: %s, want %s", tt.name, got, tt.want)
		}
	}
}

// TestSSHAlert: where sshd lacks CVE-2026-123158's fix, the alert stands
// between the rows and the section, pointing to item 5, as the maintainer
// drew it (2026-11-08), sshd's settings read or not; its route is the
// menu's that nav.Run draws; none where sshd has the fix. Plain, at 80
// columns.
func TestSSHAlert(t *testing.T) {
	c := component.NewCtx(term.Fixed(80, colorprofile.NoTTY, false, true))
	unfixed := sshTestDebian
	unfixed.Unfixed = true
	alert := "  [ ] SSH Port             Partial default (22)\n\n   # Warning: CVE-2026-123158\n   # Select option: 5) Upgrade openssh\n\n  SSH Configuration "
	vm := unfixed.SubMenu()
	if out := ansi.Strip(vm.Render(c)); !strings.Contains(out, alert) {
		t.Errorf("SSH Login, sshd unfixed:\n%s\nwant\n%s", out, alert)
	}
	m := menuOf(vm.Menu)
	vm.Menu = component.Menu{}
	if out := ansi.Strip(vm.Screen(c, m).Above); !strings.Contains(out, alert) {
		t.Errorf("Screen's Above:\n%s\nwant the route from the menu nav.Run draws", out)
	}
	notRead := sshTestNotRead
	notRead.Unfixed = true
	if out := ansi.Strip(notRead.SubMenu().Render(c)); !strings.Contains(out, "   # Select option: 5) Upgrade openssh\n") ||
		!strings.Contains(out, "   5) Upgrade openssh") {
		t.Errorf("SSH Login, sshd unfixed, its settings not read:\n%s\nwant the alert and item 5", out)
	}
	if out := ansi.Strip(sshTestDebian.SubMenu().Render(c)); strings.Contains(out, "Warning") {
		t.Errorf("SSH Login, sshd fixed, raises an alert:\n%s", out)
	}
}
