package ssh_test

import (
	"testing"

	"github.com/abbott/hardn/pkg/host/sshd"
	sshscreen "github.com/abbott/hardn/pkg/screens/ssh"
)

// TestMigrateConfigFile: Move old settings plans hardn.yml's sshConfigFile
// too, a v0.3.x drop-in at a path of its own: its lines go into
// 00-hardn.conf.
func TestMigrateConfigFile(t *testing.T) {
	h := ready(t, "debian13").sshFile(sshd.DropInDir+"/site-hardn.conf", "# SSH configuration managed by Hardn\nPasswordAuthentication no\n", 0o644)
	cfg := loadYML(t, h, "sshConfigFile: /etc/ssh/sshd_config.d/site-hardn.conf\n")
	p := playWith(h, true, func(o *sshscreen.Options) { o.Config = cfg }, "4", "0")
	if p.err != nil || p.unread != 0 || !shows(p.out, "From /etc/ssh/sshd_config.d/site-hardn.conf:") || !shows(p.out, planned) {
		t.Errorf("Move old settings with sshConfigFile: Run = %v:\n%s", p.err, p.out)
	}
}

// TestPortBeforeField: Change SSH port is refused before its field only
// where any port would be: the check plans the port sshd listens on, not
// 22, so another program on 22 of a host moved to 2230 does not refuse it.
func TestPortBeforeField(t *testing.T) {
	h := ready(t, "hardened")
	h.bound = append(h.bound, "0.0.0.0:22")
	p := play(h, true, "3", "0", "0")
	if p.err != nil || p.unread != 0 || !shows(p.out, "New SSH port") || shows(p.out, "in use") {
		t.Errorf("hardened, 22 bound by another program: Run = %v:\n%s", p.err, p.out)
	}
}
