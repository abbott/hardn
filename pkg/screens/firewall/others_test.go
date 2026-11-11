package firewall_test

import (
	"bytes"
	"regexp"
	"slices"
	"strings"
	"testing"

	fwscreen "github.com/abbott/hardn/pkg/screens/firewall"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// vaultwarden is a web app on 8000, in ss -Htlnp's form (deb13-vm,
// 2026-11-11), the program and its numbers synthetic.
const vaultwarden = "LISTEN 0      4096         0.0.0.0:8000 0.0.0.0:* users:((\"vaultwarden\",pid=812,fd=11))\n" +
	"LISTEN 0      4096            [::]:8000    [::]:* users:((\"vaultwarden\",pid=812,fd=12))\n"

// runID is a run directory's name (revert.NewID: the time, then four random
// bytes), which the golden holds as runDir.
var runID = regexp.MustCompile(`[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}`)

const runDir = "20261102T120000Z-00000000"

// changing are the commands an Enable runs that change the host.
var changing = []string{"ufw allow", "ufw default", "ufw --force enable", "systemctl start", "systemctl enable", "firewall-offline-cmd --zone="}

// TestEnableOtherPortGolden pins Enable's review with a web app on 8000 that
// the firewall it starts would block, on the core set: the warning names the
// port, its program and the command that keeps it open, before the typed
// hostname, and refuses nothing; declined with 0, nothing changes (the
// maintainer, 2026-11-11). ufw on Ubuntu 26.04 and firewalld on Rocky Linux
// 10; the files are testdata/golden/enable/other-port-<host>.golden.
func TestEnableOtherPortGolden(t *testing.T) {
	for _, tc := range []struct{ host, warning string }{
		{"ubuntu2604", "ufw will block 8000/tcp (vaultwarden). To keep it open, run ufw allow 8000/tcp first."},
		{"rocky-stopped", "firewalld will block 8000/tcp (vaultwarden). To keep it open, run firewall-offline-cmd --add-port=8000/tcp first."},
	} {
		uitest.Golden(t, "enable/other-port-"+tc.host, uitest.Core(), func(env term.Env) string {
			h, cfg := screenHost(t, tc.host)
			h.tcp = vaultwarden
			n := len(h.Runs())
			var out bytes.Buffer
			p := playWith(h, h, cfg, false, func(o *fwscreen.Options) {
				o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, "1", "0", "0"), false), Out: env.Writer(&out)}
			})
			if p.err != nil || p.unread != 0 {
				t.Errorf("%s: Run = %v, %d unread", tc.host, p.err, p.unread)
			}
			for _, r := range h.Runs()[n:] {
				if cmd := strings.Join(r, " "); slices.ContainsFunc(changing, func(c string) bool { return strings.HasPrefix(cmd, c) }) {
					t.Errorf("%s: declined, ran %q", tc.host, cmd)
				}
			}
			if w := inOrder(out.String(), tc.warning, "Type the hostname"); w != "" && env.Plain {
				t.Errorf("%s: %q not shown after what came before it:\n%s", tc.host, w, out.String())
			}
			return runID.ReplaceAllString(out.String(), runDir)
		})
	}
}
