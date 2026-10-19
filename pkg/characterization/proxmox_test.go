package characterization

import (
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/domain/model"
)

// TestProxmoxGateThroughSeams runs the operations that write
// PermitRootLogin or touch UFW through the real ServiceFactory on Proxmox
// hosts that osdetect did not flag (IsProxmox false), found only through
// the seams: by /etc/pve, or by the pve-manager package. On each, no ufw
// command runs, no file says "PermitRootLogin no", and the SSH writes say
// prohibit-password (V10, V11; brief §2). A plain Debian host is the
// control.
func TestProxmoxGateThroughSeams(t *testing.T) {
	t.Setenv("SUDO_USER", fixtureUser)
	t.Setenv("HARDN_CONFIG", "")
	debian := osCell{name: "debian", osType: "debian", version: "13", codename: "trixie"}
	ops := map[string]func(e *opEnv) error{
		"hardn -r": func(e *opEnv) error {
			return e.factory.CreateMenuManager().HardenSystem(hardeningConfigFromCLI(e.cfg))
		},
		"hardn -d": func(e *opEnv) error { return e.factory.CreateSSHManager().DisableRootSSH() },
		"hardn -c": func(e *opEnv) error {
			return e.factory.CreateSSHManager().ConfigureSSH(e.cfg.SshPort, []string{e.cfg.SshListenAddress}, false, e.cfg.SshAllowedUsers, nil)
		},
		"hardn -w": func(e *opEnv) error {
			return e.factory.CreateFirewallManager().ConfigureSecureFirewall(e.cfg.SshPort, nil, []model.FirewallProfile{})
		},
	}
	for _, host := range []struct {
		name    string
		proxmox bool
		seed    func(e *opEnv)
	}{
		{"by /etc/pve", true, func(e *opEnv) { e.fs.Directories["/etc/pve"] = true }},
		{"by pve-manager", true, func(e *opEnv) {
			e.mc.CommandOutputs["dpkg-query -W --showformat=${Status} pve-manager"] = []byte("install ok installed")
		}},
		{"plain Debian", false, func(*opEnv) {}},
	} {
		for name, op := range ops {
			t.Run(host.name+"/"+name, func(t *testing.T) {
				e := newOpEnv(debian, false)
				e.newUser()
				host.seed(e)
				err := op(e)
				ufw := false
				for _, c := range e.mc.ExecutedCommands {
					ufw = ufw || strings.Contains(c, "ufw ")
				}
				var conf []string
				for p, b := range e.fs.Files {
					if strings.HasPrefix(p, "/etc/ssh/") {
						conf = append(conf, string(b))
					}
				}
				joined := strings.Join(conf, "\n")
				switch {
				case host.proxmox:
					if ufw {
						t.Errorf("ufw ran on Proxmox: %v", e.mc.ExecutedCommands)
					}
					if strings.Contains(joined, "PermitRootLogin no") {
						t.Errorf("PermitRootLogin no written on Proxmox:\n%s", joined)
					}
					if name != "hardn -w" && !strings.Contains(joined, "PermitRootLogin prohibit-password") {
						t.Errorf("no prohibit-password (err %v):\n%s", err, joined)
					}
					if (name == "hardn -w" || name == "hardn -r") && (err == nil || !strings.Contains(err.Error(), "UFW is not used on Proxmox")) {
						t.Errorf("err %v, want the UFW refusal", err)
					}
				default:
					if err != nil {
						t.Fatal(err)
					}
					if (name == "hardn -w" || name == "hardn -r") && !ufw {
						t.Errorf("no ufw on plain Debian: %v", e.mc.ExecutedCommands)
					}
					if name != "hardn -w" && !strings.Contains(joined, "PermitRootLogin no") {
						t.Errorf("no PermitRootLogin no on plain Debian:\n%s", joined)
					}
				}
			})
		}
	}
}
