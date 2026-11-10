package firewall_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
)

// elChanges are the commands run on h from the nth call on that change
// firewalld or its package: every one but firewalld's, rpm's, dnf's, sshd's
// and ss's reads, and the revert timer's.
func elChanges(h *fwHost, n int) []string {
	var out []string
	for _, c := range h.Calls()[n:] {
		if c.Op != "run" {
			continue
		}
		f := host.FormatArgv(c.Argv)
		read := isRead(f) || timerCommand(f) || slices.ContainsFunc([]string{"rpm -q ", "dnf install --assumeno ", "firewall-offline-cmd --get-", "firewall-offline-cmd --list-",
			"firewall-offline-cmd --service=", "nmcli ", "firewall-cmd --state", "firewall-cmd --get-", "firewall-cmd --permanent --zone=public --get-"}, func(p string) bool { return strings.HasPrefix(f, p) })
		if !read {
			out = append(out, f)
		}
	}
	return out
}

// TestELInstall: on Rocky Linux without firewalld, item 1 is Install
// firewalld: its review (the package, not running, not at boot), y/N; then
// firewalld.service masked, dnf's install without weak dependencies, the
// unmask, and the screen drawn again, firewalld installed and disabled,
// item 1 Enable. n changes nothing.
func TestELInstall(t *testing.T) {
	h, cfg := screenHost(t, "rocky-missing")
	p := play(h, h, cfg, false, "1", "y", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil {
		t.Fatalf("Run = %v, %d unread, runs %+v:\n%s", p.err, p.unread, p.runs, p.out)
	}
	if !h.fwd.installed || h.fwd.masked || h.fwd.enabled != "disabled" {
		t.Errorf("firewalld installed %v, masked %v, %q", h.fwd.installed, h.fwd.masked, h.fwd.enabled)
	}
	if w := inOrder(p.out, "1) Install firewalld", "Install the firewalld package", "Install firewalld #", "Package firewalld", "Status Not running", "At boot No",
		"Install firewalld? [y/N", "Firewall Configuration #", "Installed Yes", "Running No", "At boot No", "1) Enable firewalld"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	want := []string{"systemctl mask firewalld.service", "dnf install -y --setopt=install_weak_deps=False --setopt=exit_on_lock=True firewalld", "systemctl unmask firewalld.service"}
	if got := elChanges(h, 0); !slices.Equal(got, want) {
		t.Errorf("ran %q, want %q", got, want)
	}
	if strings.Contains(p.out, "UFW") {
		t.Errorf("ufw's words on Rocky Linux:\n%s", p.out)
	}
	h, cfg = screenHost(t, "rocky-missing")
	n := len(h.Calls())
	if p := play(h, h, cfg, false, "1", "n", "0"); p.err != nil || h.fwd.installed || len(elChanges(h, n)) > 0 {
		t.Errorf("declined: Run = %v, installed %v, ran %q", p.err, h.fwd.installed, elChanges(h, n))
	}
}

// TestELDisable: running, item 1 is Disable firewalld, plain y/N under its
// warning: disabled, then stopped. Stopped and starting at boot, item 2
// beside Enable is Disable: disabled alone, and item 2 gone after it. n
// changes nothing.
func TestELDisable(t *testing.T) {
	for _, tc := range []struct {
		host, key      string
		about, warning string
		review         []string
		want           []string
	}{
		{"rocky-running", "1", "Stop firewalld (not recommended)", "This stops firewalld and leaves the host with no firewall.",
			[]string{"Status Not running", "At boot No"}, []string{"systemctl disable firewalld.service", "systemctl stop firewalld.service"}},
		{"rocky-at-boot", "2", "Don't start firewalld at boot", "firewalld won't start at the next boot, which leaves the host with no firewall then.",
			[]string{"At boot No"}, []string{"systemctl disable firewalld.service"}},
	} {
		h, cfg := screenHost(t, tc.host)
		p := play(h, h, cfg, false, tc.key, "y", "0")
		if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil {
			t.Fatalf("%s: Run = %v, %d unread, runs %+v:\n%s", tc.host, p.err, p.unread, p.runs, p.out)
		}
		if h.fwd.active != "inactive" || h.fwd.enabled != "disabled" {
			t.Errorf("%s: firewalld %s, %s", tc.host, h.fwd.active, h.fwd.enabled)
		}
		said := append([]string{tc.key + ") Disable firewalld", tc.about, "Disable firewalld #", tc.warning}, tc.review...)
		said = append(said, "Disable firewalld? [y/N", "Firewall Configuration #", "Running No", "At boot No", "1) Enable firewalld")
		if w := inOrder(p.out, said...); w != "" {
			t.Errorf("%s: %q not shown after what came before it:\n%s", tc.host, w, p.out)
		}
		if s := screens(p.out, "Firewall Configuration"); len(s) < 2 || strings.Contains(s[len(s)-1], "2) Disable firewalld") {
			t.Errorf("%s: Disable still beside Enable once disabled:\n%s", tc.host, p.out)
		}
		if got := elChanges(h, 0); !slices.Equal(got, tc.want) {
			t.Errorf("%s: ran %q, want %q", tc.host, got, tc.want)
		}
		h, cfg = screenHost(t, tc.host)
		n := len(h.Calls())
		if p := play(h, h, cfg, false, tc.key, "n", "0"); p.err != nil || len(elChanges(h, n)) > 0 {
			t.Errorf("%s declined: Run = %v, ran %q", tc.host, p.err, elChanges(h, n))
		}
	}
}

// TestELEnableBeforeAsking: Enable is lockout-risk: with no hostname to
// read it says so before asking or planning; refused by its plan (a masked
// firewalld.service), item 1 says why before asking. Nothing runs.
func TestELEnableBeforeAsking(t *testing.T) {
	h, cfg := screenHost(t, "rocky-stopped")
	if err := h.Remove(context.Background(), header.HostnameFile); err != nil {
		t.Fatal(err)
	}
	n := len(h.Calls())
	if p := play(h, h, cfg, false, "1", "0"); p.err != nil || !strings.Contains(flat(p.out), cantConfirm) || p.engines != 0 || len(elChanges(h, n)) > 0 {
		t.Errorf("no hostname: Run = %v, %d engines, ran %q:\n%s", p.err, p.engines, elChanges(h, n), p.out)
	}
	h, cfg = screenHost(t, "rocky-stopped")
	h.fwd.masked = true
	n = len(h.Calls())
	p := play(h, h, cfg, false, "1", "0")
	if p.err != nil || !strings.Contains(flat(p.out), "firewalld's service is masked or missing, so it can't start") || p.engines != 0 || len(elChanges(h, n)) > 0 {
		t.Errorf("masked: Run = %v, %d engines, ran %q:\n%s", p.err, p.engines, elChanges(h, n), p.out)
	}
	if w := inOrder(p.out, "At boot Masked", "1) Enable firewalld"); w != "" {
		t.Errorf("masked: %q not shown:\n%s", w, p.out)
	}
}
