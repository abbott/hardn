package firewall_test

import (
	"context"
	"path"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/revert"
)

// withPending leaves on h a change waiting for hardn confirm: Enable applied
// on another host of the screen's, its timer armed, and its journal
// (revert.DefaultDir) copied onto h. h's own state is left as it was.
func withPending(t *testing.T, h *fwHost) {
	t.Helper()
	from, cfg := screenHost(t, "ubuntu2604")
	if p := play(from, from, cfg, false, "1", hostName, "0"); p.err != nil || !armed(from, 0) {
		t.Fatalf("arming a change: Run = %v:\n%s", p.err, p.out)
	}
	copyDir(t, from, h, revert.DefaultDir)
}

// copyDir copies dir and what it holds from one host to another.
func copyDir(t *testing.T, from, to *fwHost, dir string) {
	t.Helper()
	ctx := context.Background()
	es, err := from.ReadDir(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	to.AddDir(dir, 0o700, 0, 0)
	for _, e := range es {
		name := path.Join(dir, e.Name())
		if e.IsDir() {
			copyDir(t, from, to, name)
			continue
		}
		data, err := from.ReadFile(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		to.AddFile(name, data, 0o600, 0, 0)
	}
}

// TestPendingBeforeAsking: with a change waiting for hardn confirm, every
// change with no form, the ones the reading planned with the control alone
// among them, is refused by the engine before anything is asked, as a
// warning in the screens' words (screenkit.WordsPending; planned,
// screenkit.Session.Preplan): no review, no question, nothing run or changed. Before, those drew their review and then said
// Operation Failed (CHANGE_PENDING).
func TestPendingBeforeAsking(t *testing.T) {
	for _, tc := range []struct {
		name, host, title string
		keys              []string
	}{
		{"install ufw", "debian13", "UFW Firewall Configuration #", []string{"1", "0"}},
		{"enable", "ubuntu2604", "UFW Firewall Configuration #", []string{"1", "0"}},
		{"install iproute2", "no-ss-active", "UFW Firewall Configuration #", []string{"1", "0"}},
		{"enable without ss", "no-ss", "UFW Firewall Configuration #", []string{"1", "0"}},
		{"apply profiles", "profiles", "Manage UFW Application Profiles #", []string{"2", "3", "0", "0"}},
		{"install firewalld", "rocky-missing", "Firewall Configuration #", []string{"1", "0"}},
		{"enable firewalld", "rocky-stopped", "Firewall Configuration #", []string{"1", "0"}},
	} {
		h, cfg := screenHost(t, tc.host)
		withPending(t, h)
		n := len(h.Calls())
		changed := func() []string { return writes(h, n) }
		if h.fwd != nil { // Rocky Linux: dnf's and firewall-offline-cmd's reads aren't changes
			changed = func() []string { return elChanges(h, n) }
		}
		p := play(h, h, cfg, false, tc.keys...)
		if p.err != nil || p.unread != 0 || len(p.runs) != 0 || len(changed()) > 0 {
			t.Errorf("%s: Run = %v, %d unread, runs %+v, changed %v:\n%s", tc.name, p.err, p.unread, p.runs, changed(), p.out)
			continue
		}
		if w := inOrder(p.out, "Enter selection", "Checking the host", "Warning", "A change is waiting for hardn confirm.", "or let it roll back, then try again.", tc.title); w != "" {
			t.Errorf("%s: %q not shown after what came before it:\n%s", tc.name, w, p.out)
		}
		for _, not := range []string{"[y/N", "Type the hostname", "Operation Failed", "CHANGE_PENDING", "is already journalled"} {
			if strings.Contains(p.out, not) {
				t.Errorf("%s: %q shown:\n%s", tc.name, not, p.out)
			}
		}
	}
}
