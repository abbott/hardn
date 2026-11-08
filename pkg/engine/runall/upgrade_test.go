package runall_test

import (
	"context"
	"testing"

	fwctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/runall"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
)

// auth is SSH Auth's row.
var auth = ssh.Auth{}.ID()

// TestUpgradeRow: on Alpine where sshd lacks CVE-2026-123158's fix, the
// SSH Auth row plans the openssh upgrade though hardn.yml asks nothing of
// it (the maintainer, 2026-11-08), and root login off, which claims sshd
// too, waits for the next Run All, as does Create refused for the fix
// (alpine-exposed: passwords on, carol's field would be "*"); the plan is
// then refused for nothing and is not lockout-risk. Applied, the package
// has the fix. Where the fix is there, or not established (alpine323 has no
// installed database), the row asks nothing, and off Alpine too.
func TestUpgradeRow(t *testing.T) {
	ctx := context.Background()
	h := newHost(t, "alpine-unfixed")
	r := h.read(t, "alpine-unfixed")
	rw, _ := r.Row(auth)
	if rw.Kind != runall.Planned || rw.Words != sshchange.UpgradeTitle {
		t.Fatalf("SSH Auth %v %q; want planned, %q", rw.Kind, rw.Words, sshchange.UpgradeTitle)
	}
	if rw, _ := r.Row(login); rw.Kind != runall.Later || rw.Words != runall.WordsAfterUpgrade || rw.Control != nil {
		t.Errorf("SSH Login %v %q; want later, %q", rw.Kind, rw.Words, runall.WordsAfterUpgrade)
	}
	if rw, _ := r.Row(fwctl.ID); rw.Kind == runall.Planned {
		t.Errorf("Firewall planned beside the upgrade")
	}
	if r.Refusal != "" || r.Plan == nil || len(r.Plan.Refused) > 0 || r.Lockout() {
		t.Fatalf("refusal %q, plan %v, lockout %v; want the plan unrefused, not lockout-risk", r.Refusal, r.Plan != nil, r.Lockout())
	}
	if _, err := h.newEngine(nil).Apply(ctx, r.Plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if fix := alpine.OpenSSHFix(ctx, h, alpine.OpenSSHServer, "3.23.6"); fix.Fixed != detect.Yes {
		t.Errorf("after: %+v", fix)
	}
	checkHost(t, "alpine-unfixed", h.sshHost)
	if rw, _ := h.read(t, "alpine-unfixed").Row(auth); rw.Kind != runall.NothingAsked {
		t.Errorf("after the upgrade, SSH Auth %v %q; want nothing asked", rw.Kind, rw.Words)
	}

	ex := newHost(t, "alpine-exposed")
	r = ex.read(t, "alpine-exposed")
	if rw, _ := r.Row(users.ID); rw.Kind != runall.Later || rw.Words != runall.WordsAfterUpgrade {
		t.Errorf("alpine-exposed: User %v %q (%v); want later", rw.Kind, rw.Words, rw.Err)
	}
	if rw, _ := r.Row(auth); rw.Kind != runall.Planned {
		t.Errorf("alpine-exposed: SSH Auth %v %q; want planned", rw.Kind, rw.Words)
	}

	for _, name := range []string{"alpine323", "debian13"} {
		if rw, _ := newHost(t, name).read(t, name).Row(auth); rw.Kind != runall.NothingAsked {
			t.Errorf("%s: SSH Auth %v %q; want nothing asked", name, rw.Kind, rw.Words)
		}
	}
}

// TestUpgradeAsked: Asked's SSH Auth is the upgrade on Alpine alone,
// asked by the host (Unfixed), not hardn.yml.
func TestUpgradeAsked(t *testing.T) {
	in := newHost(t, "alpine-unfixed").input()
	if a := runall.Asked(in, auth); !a.Unfixed || a.Words != sshchange.UpgradeTitle {
		t.Errorf("Alpine: %+v", a)
	} else if _, ok := a.Control.(sshchange.Upgrade); !ok {
		t.Errorf("Alpine: control %T", a.Control)
	}
	if a := runall.Asked(newHost(t, "debian13").input(), auth); a.Asked() {
		t.Errorf("Debian: %+v", a)
	}
}
