package main

import (
	"context"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// TestDrawStatus: the main menu's status is read through statusHost with
// engineDetect's facts and drawn for stdoutEnv's terminal, its header
// showing hardn's version, its DNS row checked against the configuration
// as it is on each pass (the DNS screen adopts each list it saves into
// it); it only reads, and nothing is written. (pkg/menu's tests draw the
// main menu with this status over the gallery's hosts.)
func TestDrawStatus(t *testing.T) {
	h, cfg := newDNSHost(t, "configured", dnsTestList)
	useDNSHost(t, h) // engineDetect, stdoutEnv: plain at 80 columns
	hostWas := statusHost
	t.Cleanup(func() { statusHost = hostWas })
	statusHost = func() host.System { return h }
	engineHost = func() host.System { t.Error("the status read engineHost, whose run log takes every command"); return h }

	out, err := drawStatus(context.Background(), cfg, "0.4.0", 17)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "hardn  v0.4.0") || !strings.Contains(out, "  [+] DNS                  Configured 192.0.2.53, 192.0.2.54\n") || !strings.HasSuffix(out, "\n\n") {
		t.Errorf("not the status with the DNS row Configured:\n%s", out)
	}
	cfg.Nameservers = []string{"192.0.2.55"} // a list the DNS screen saved
	out, err = drawStatus(context.Background(), cfg, "0.4.0", 17)
	if err != nil || !strings.Contains(out, "  [ ] DNS                  Not Configured not hardn.yml's\n") {
		t.Errorf("the DNS row does not follow the configuration (%v):\n%s", err, out)
	}
	if w := h.writes(); len(w) > 0 {
		t.Errorf("the status wrote %q", w)
	}
	for _, c := range h.Calls() {
		if c.Op == "run" && !statusQuery(c.Argv) {
			t.Errorf("ran %q", c.Argv)
		}
	}
}

// statusQuery reports whether argv is a query of the status's checks on
// the DNS hosts: systemctl's state of a unit, sshd's test mode, and the
// like.
func statusQuery(argv []string) bool {
	cmd := strings.Join(argv, " ")
	for _, q := range []string{"systemctl is-active ", "systemctl is-enabled ", "sshd -T", "ss -Htln", "ufw status verbose", "dpkg-query --show",
		"sudo --version", "visudo -c", "apt-config dump", "ip -4 addr show dev ", "resolvectl --no-pager "} {
		if strings.HasPrefix(cmd, q) {
			return true
		}
	}
	return false
}

// TestStatusSeams: the status reads host.Real and detects with
// detect.Detect.
func TestStatusSeams(t *testing.T) {
	if _, ok := statusHost().(*host.Real); !ok {
		t.Error("statusHost is not host.Real")
	}
	if !samePointer(engineDetect, detect.Detect) {
		t.Error("engineDetect is not detect.Detect")
	}
}
