package secondary

import (
	"errors"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/interfaces"
)

const pveManagerQuery = "dpkg-query -W --showformat=${Status} pve-manager"

// pveHost is a mock host: Proxmox by /etc/pve, by the pve-manager
// package, by both or by neither.
func pveHost(etcPve, pveManager bool) (*interfaces.MockFileSystem, *interfaces.MockCommander) {
	fs := interfaces.NewMockFileSystem()
	mc := interfaces.NewMockCommander()
	if etcPve {
		fs.Directories["/etc/pve"] = true
	}
	if pveManager {
		mc.CommandOutputs[pveManagerQuery] = []byte("install ok installed")
	} else {
		mc.CommandErrors[pveManagerQuery] = errors.New("fixture: dpkg-query: no packages found matching pve-manager")
	}
	return fs, mc
}

func TestProxmoxHost(t *testing.T) {
	if proxmoxTreatedAsDebian {
		t.Fatal("proxmoxTreatedAsDebian is set: Proxmox hosts get UFW and PermitRootLogin no (V10, V11)")
	}
	for _, tc := range []struct {
		name               string
		osType             string
		detected, etc, pkg bool
		want               bool
		calls              []string // commands run
	}{
		{name: "osdetect found /etc/pve", osType: "debian", detected: true, want: true},
		{name: "/etc/pve", osType: "debian", etc: true, want: true},
		{name: "pve-manager installed", osType: "debian", pkg: true, want: true, calls: []string{pveManagerQuery}},
		{name: "plain Debian", osType: "debian", want: false, calls: []string{pveManagerQuery}},
		{name: "Alpine has no dpkg to ask", osType: "alpine", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, mc := pveHost(tc.etc, tc.pkg)
			h := NewProxmoxHost(fs, mc, tc.osType, tc.detected)
			if got := h.IsProxmox(); got != tc.want {
				t.Errorf("IsProxmox() = %v, want %v", got, tc.want)
			}
			h.IsProxmox() // asked again: no new check
			if strings.Join(mc.ExecutedCommands, "; ") != strings.Join(tc.calls, "; ") {
				t.Errorf("commands %v, want %v", mc.ExecutedCommands, tc.calls)
			}
		})
	}
	t.Run("an /etc/pve that cannot be checked counts as present", func(t *testing.T) {
		fs, mc := pveHost(false, false)
		fs.StatError["/etc/pve"] = errors.New("fixture: permission denied")
		if !NewProxmoxHost(fs, mc, "debian", false).IsProxmox() {
			t.Error("IsProxmox() = false")
		}
	})
}

// TestSaveSSHConfigOnProxmox checks that ConfigureSSH's writer (hardn -c,
// -r and Run All) puts prohibit-password where it would put no.
func TestSaveSSHConfigOnProxmox(t *testing.T) {
	for _, tc := range []struct {
		proxmox, permitRoot bool
		want                string
	}{
		{true, false, "PermitRootLogin prohibit-password\n"},
		{true, true, "PermitRootLogin yes\n"},
		{false, false, "PermitRootLogin no\n"},
	} {
		fs, mc := pveHost(tc.proxmox, false)
		repo := NewFileSSHRepository(fs, mc, "debian", nil)
		if err := repo.SaveSSHConfig(model.SSHConfig{Port: 2230, PermitRootLogin: tc.permitRoot, AllowedUsers: []string{"alice"}}); err != nil {
			t.Fatal(err)
		}
		conf := string(fs.Files["/etc/ssh/sshd_config.d/hardn.conf"])
		if !strings.Contains(conf, "\n"+tc.want) || strings.Count(conf, "PermitRootLogin") != 1 {
			t.Errorf("proxmox %v, PermitRootLogin %v: hardn.conf\n%s\nwant %q", tc.proxmox, tc.permitRoot, conf, tc.want)
		}
	}
}

// TestUFWOnProxmox checks that no ufw command runs on Proxmox: configuring
// and enabling are refused, and a plain Debian host still gets them.
func TestUFWOnProxmox(t *testing.T) {
	cfg := model.FirewallConfig{Enabled: true, DefaultIncoming: "deny", DefaultOutgoing: "allow",
		Rules: []model.FirewallRule{{Action: "allow", Protocol: "tcp", Port: 2230}}}
	for _, tc := range []struct {
		name     string
		etc, pkg bool
	}{{"by /etc/pve", true, false}, {"by pve-manager", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			fs, mc := pveHost(tc.etc, tc.pkg)
			repo := NewUFWFirewallRepository(fs, mc, nil)
			if err := repo.SaveFirewallConfig(cfg); !errors.Is(err, errUFWOnProxmox) {
				t.Errorf("SaveFirewallConfig: %v", err)
			}
			if err := repo.EnableFirewall(); !errors.Is(err, errUFWOnProxmox) {
				t.Errorf("EnableFirewall: %v", err)
			}
			// No caller in cmd or pkg/menu yet; refused all the same.
			if err := repo.AddRule(cfg.Rules[0]); !errors.Is(err, errUFWOnProxmox) {
				t.Errorf("AddRule: %v", err)
			}
			if err := repo.RemoveRule(cfg.Rules[0]); !errors.Is(err, errUFWOnProxmox) {
				t.Errorf("RemoveRule: %v", err)
			}
			if err := repo.AddProfile(model.FirewallProfile{Name: "ExampleHTTPS", Ports: []string{"8443/tcp"}}); !errors.Is(err, errUFWOnProxmox) {
				t.Errorf("AddProfile: %v", err)
			}
			for _, c := range mc.ExecutedCommands {
				if strings.Contains(c, "ufw") {
					t.Errorf("ran %q", c)
				}
			}
			if len(fs.Files) != 0 {
				t.Errorf("wrote %v", keys(fs.Files))
			}
		})
	}
	t.Run("plain Debian", func(t *testing.T) {
		fs, mc := pveHost(false, false)
		if err := NewUFWFirewallRepository(fs, mc, nil).SaveFirewallConfig(cfg); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(mc.ExecutedCommands, "; "); !strings.HasSuffix(got, "sh -c yes | ufw enable") {
			t.Errorf("commands: %s", got)
		}
	})
	t.Run("plain Debian, the rule methods", func(t *testing.T) {
		fs, mc := pveHost(false, false)
		repo := NewUFWFirewallRepository(fs, mc, nil)
		if err := repo.AddRule(cfg.Rules[0]); err != nil {
			t.Fatal(err)
		}
		if err := repo.RemoveRule(cfg.Rules[0]); err != nil {
			t.Fatal(err)
		}
		if err := repo.AddProfile(model.FirewallProfile{Name: "ExampleHTTPS", Ports: []string{"8443/tcp"}}); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(mc.ExecutedCommands, "; "); !strings.HasSuffix(got, "ufw allow 2230/tcp; ufw delete allow 2230/tcp; ufw allow from any to any app ExampleHTTPS") {
			t.Errorf("commands: %s", got)
		}
	})
	t.Run("disable stays possible on Proxmox", func(t *testing.T) {
		fs, mc := pveHost(true, false)
		if err := NewUFWFirewallRepository(fs, mc, nil).DisableFirewall(); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(mc.ExecutedCommands, "; "); got != "ufw disable" {
			t.Errorf("commands: %s", got)
		}
	})
}
