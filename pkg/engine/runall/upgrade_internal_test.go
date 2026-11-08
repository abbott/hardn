package runall

import (
	"testing"

	fwctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/engine/controls/users"
)

// TestAfterUpgrade: with the openssh upgrade planned, Firewall Enable and
// root login off, planned beside it, wait for the next Run All, as does a
// Create refused for the fix; a Create refused otherwise, and every row
// without the upgrade, stay as they were.
func TestAfterUpgrade(t *testing.T) {
	rows := func(upgrade bool, createErr error) []Row {
		auth := Row{ID: ssh.Auth{}.ID(), Kind: NothingAsked}
		if upgrade {
			auth = Row{ID: ssh.Auth{}.ID(), Kind: Planned, Control: sshchange.Upgrade{}}
		}
		return []Row{
			{ID: users.ID, Kind: Refused, Err: createErr},
			{ID: ssh.Login{}.ID(), Kind: Planned, Control: sshchange.Disallow{}},
			auth,
			{ID: fwctl.ID, Kind: Planned, Control: fwctl.Incoming{Action: fwctl.ActionEnable}},
		}
	}
	r := Reading{Rows: rows(true, users.ErrPasswordBypass)}
	r.afterUpgrade()
	for _, id := range []string{users.ID, ssh.Login{}.ID(), fwctl.ID} {
		if rw, _ := r.Row(id); rw.Kind != Later || rw.Words != WordsAfterUpgrade || rw.Control != nil || rw.Err != nil {
			t.Errorf("%s: %v %q, control %v, err %v; want later", id, rw.Kind, rw.Words, rw.Control, rw.Err)
		}
	}
	r = Reading{Rows: rows(true, users.ErrNameTaken)}
	r.afterUpgrade()
	if rw, _ := r.Row(users.ID); rw.Kind != Refused {
		t.Errorf("Create refused otherwise: %v, want refused", rw.Kind)
	}
	r = Reading{Rows: rows(false, users.ErrPasswordBypass)}
	r.afterUpgrade()
	for _, rw := range r.Rows {
		if rw.Kind == Later {
			t.Errorf("no upgrade: %s later", rw.ID)
		}
	}
}
