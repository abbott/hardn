package ssh_test

import (
	"context"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
)

// The upgrade's words (proposed, 2026-11-08).
const (
	upgradeItem     = "5) Upgrade openssh Fix CVE-2026-123158"
	upgradeQuestion = "Upgrade openssh?"
	upgradeRow      = "openssh-server 10.2_p1-r0, to 10.2_p1-r1 or later"
	// upgradeAlert is the alert over the section, pointing to item 5 (the
	// maintainer's words, 2026-11-08), flat as shows compares it.
	upgradeAlert = "SSH Port Not Configured default (22) # Warning: CVE-2026-123158 # Select option: 5) Upgrade openssh SSH Configuration"
)

// TestUpgradeItem: item 5 is offered on Alpine where sshd lacks
// CVE-2026-123158's fix, and only there (the Alpine VM with no installed
// database reads the fix Unknown), with the alert pointing to it between
// the rows and the section; chosen, a plain question (nothing of it is
// lockout-risk), then the upgrade, sshd reloaded onto the new binary; the
// screen then offers no item 5 and raises no alert. Under plan only,
// nothing is upgraded.
func TestUpgradeItem(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"alpine323", "debian13"} {
		h := ready(t, name)
		if p := play(h, true, "0"); shows(p.out, "Upgrade openssh") || shows(p.out, "Warning: CVE-2026-123158") {
			t.Errorf("%s offers the upgrade:\n%s", name, p.out)
		}
	}
	t.Run("plan only", func(t *testing.T) {
		h := ready(t, "alpine-unfixed")
		p := play(h, true, "5", "0")
		if p.err != nil || !shows(p.out, upgradeItem) || !shows(p.out, upgradeAlert) || !shows(p.out, upgradeRow) || !shows(p.out, "apk upgrade openssh openssh-client-common") ||
			!shows(p.out, planned) || p.unread != 0 {
			t.Errorf("Run = %v:\n%s", p.err, p.out)
		}
		if fix := alpine.OpenSSHFix(ctx, h, alpine.OpenSSHServer, "3.23.6"); fix.Fixed != detect.No {
			t.Errorf("plan only upgraded: %+v", fix)
		}
		checkHost(t, "alpine-unfixed", h)
	})
	t.Run("applied", func(t *testing.T) {
		h := ready(t, "alpine-unfixed")
		p := play(h, false, "5", "y", "0")
		if p.err != nil || !shows(p.out, upgradeQuestion) || strings.Contains(p.out, typeIt) || !shows(p.out, applied) || p.unread != 0 {
			t.Errorf("Run = %v:\n%s", p.err, p.out)
		}
		if fix := alpine.OpenSSHFix(ctx, h, alpine.OpenSSHServer, "3.23.6"); fix.Fixed != detect.Yes {
			t.Errorf("after: %+v", fix)
		}
		if _, stale, err := alpine.ListenerBinary(ctx, h); err != nil || stale {
			t.Errorf("listener stale %v, %v", stale, err)
		}
		if i := strings.LastIndex(p.out, "SSH Login ###"); i < 0 || shows(p.out[i:], "Upgrade openssh") || shows(p.out[i:], "Warning: CVE-2026-123158") {
			t.Errorf("the screen after the upgrade still offers it:\n%s", p.out)
		}
		checkHost(t, "alpine-unfixed", h)
	})
}
