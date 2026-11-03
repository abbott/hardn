package menu

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
)

// newDisableRootMenu builds the SSH Login screen on a MenuManager whose SSH
// manager runs on the repo's mock file system and commander, for a Debian
// 13 host whose hardn.conf holds seed, and that is Proxmox VE when pve is
// set (/etc/pve exists). The MenuManager gets the Proxmox check the SSH
// repository decides by, as ServiceFactory.CreateMenuManager wires it.
// Colours are off until the test ends.
func newDisableRootMenu(t *testing.T, pve, dryRun bool, seed string) (*DisableRootMenu, *interfaces.MockFileSystem, *interfaces.MockCommander) {
	t.Helper()
	prevColors := style.UseColors
	style.UseColors = false
	t.Cleanup(func() { style.UseColors = prevColors })
	fs := interfaces.NewMockFileSystem()
	mc := interfaces.NewMockCommander()
	if pve {
		fs.Directories["/etc/pve"] = true
	}
	fs.Files["/etc/ssh/sshd_config.d/hardn.conf"] = []byte(seed)
	host := secondary.NewProxmoxHost(fs, mc, "debian", false)
	osInfo := model.OSInfo{Type: "debian", Version: "13", Codename: "trixie"}
	ssh := application.NewSSHManager(service.NewSSHServiceImpl(secondary.NewFileSSHRepository(fs, mc, "debian", host), osInfo))
	mm := application.NewMenuManager(ssh, nil, nil, nil, nil)
	mm.SetProxmoxCheck(host.IsProxmox)

	cfg := config.DefaultConfig()
	cfg.DryRun = dryRun
	return NewDisableRootMenu(mm, cfg, &osdetect.OSInfo{OsType: "debian", OsVersion: "13", OsCodename: "trixie"}), fs, mc
}

// sgr matches an SGR escape sequence (colour, bold, dim, reset).
var sgr = regexp.MustCompile("\x1b\\[[0-9;]*m")

// TestDisableRootOnProxmoxSaysKeyOnly checks what option 1 does on a
// Proxmox VE host (gate 6, proxmoxTreatedAsDebian): it sets root login to
// key-only and says so, never "disabled", and restarts sshd only through
// the edit, when the file changed. The Show path that leads there is not
// driven here: its status check reads this machine's /etc/ssh.
func TestDisableRootOnProxmoxSaysKeyOnly(t *testing.T) {
	const seed = "Port 2230\nPermitRootLogin yes\nAllowUsers root alice\n"
	for _, tc := range []struct {
		name     string
		dryRun   bool
		seed     string
		want     string // printed
		conf     string // hardn.conf afterwards
		commands []string
	}{
		{
			name:     "live",
			seed:     seed,
			want:     "Root SSH login set to key-only (PermitRootLogin prohibit-password), not disabled",
			conf:     "Port 2230\nPermitRootLogin prohibit-password\nAllowUsers root alice\n",
			commands: []string{"systemctl restart ssh"},
		},
		{
			name: "already key-only",
			seed: "Port 2230\nPermitRootLogin prohibit-password\nAllowUsers root alice\n",
			want: "Root SSH login set to key-only (PermitRootLogin prohibit-password), not disabled",
			conf: "Port 2230\nPermitRootLogin prohibit-password\nAllowUsers root alice\n",
		},
		{
			name:   "dry-run",
			dryRun: true,
			seed:   seed,
			want:   "[DRY-RUN] Would set root SSH login to key-only (PermitRootLogin prohibit-password), as Proxmox VE clustering needs",
			conf:   seed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, fs, mc := newDisableRootMenu(t, true, tc.dryRun, tc.seed)
			if !m.menuManager.IsProxmoxHost() {
				t.Fatal("the fixture with /etc/pve is not taken for Proxmox VE (is proxmoxTreatedAsDebian set?)")
			}
			// Bolded and Dimmed emit SGR codes even with colours off.
			out := sgr.ReplaceAllString(captureStdout(t, m.setRootKeyOnly), "")
			if !strings.Contains(out, tc.want) {
				t.Errorf("printed\n%s\nwant %q", out, tc.want)
			}
			for _, wrong := range []string{"has been disabled", "Restarting SSH service"} {
				if strings.Contains(out, wrong) {
					t.Errorf("printed %q:\n%s", wrong, out)
				}
			}
			if got := string(fs.Files["/etc/ssh/sshd_config.d/hardn.conf"]); got != tc.conf {
				t.Errorf("hardn.conf:\n%s\nwant\n%s", got, tc.conf)
			}
			if got := mc.ExecutedCommands; len(got) != len(tc.commands) || (len(got) > 0 && !reflect.DeepEqual(got, tc.commands)) {
				t.Errorf("commands %q, want %q", mc.ExecutedCommands, tc.commands)
			}
		})
	}

	t.Run("not Proxmox", func(t *testing.T) {
		m, _, _ := newDisableRootMenu(t, false, false, seed)
		if m.menuManager.IsProxmoxHost() {
			t.Fatal("a Debian host without /etc/pve or pve-manager is taken for Proxmox VE")
		}
	})
}
