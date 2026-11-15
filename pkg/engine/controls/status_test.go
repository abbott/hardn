package controls_test

import (
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/controls"
	"github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/engine/controls/kernelsettings"
	"github.com/abbott/hardn/pkg/engine/controls/logforward"
	"github.com/abbott/hardn/pkg/engine/controls/timesync"
	"github.com/abbott/hardn/pkg/engine/risk"
)

// TestStatus: the status controls are the score's rows, by ID and title,
// in its order (risk's TestRows holds it), each once, Kernel among them
// whatever hardn.yml's kernel says; then DNS, which the score never
// counts, checking against the configuration it was given.
func TestStatus(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Nameservers = []string{"192.0.2.53"}
	var got, want []string
	for _, c := range controls.Status(cfg) {
		got = append(got, c.ID()+" "+c.Title())
		if n, ok := c.(dns.Nameservers); ok && n.Config != cfg {
			t.Errorf("the DNS control checks against %p, not the configuration given, %p", n.Config, cfg)
		}
	}
	for _, r := range risk.Rows() {
		want = append(want, r.ID+" "+r.Title)
	}
	want = append(want, dns.ID+" DNS")
	if !slices.Equal(got, want) {
		t.Errorf("controls %q, want the rows and DNS %q", got, want)
	}
	if !slices.Contains(got, kernelsettings.ID+" "+kernelsettings.Title) || cfg.KernelSettings {
		t.Errorf("Kernel is not checked with kernel unset: %q", got)
	}
}

// TestMenu: the main menu checks Status's controls, then NTP Settings and
// Syslog Settings, which are no rows of risk's (the risk level never
// counts them), in Info's order with DNS: DNS, NTP, Syslog; whatever
// hardn.yml's kernel says. Which reports the main menu draws is
// pkg/output's (TestInfoRows).
func TestMenu(t *testing.T) {
	cfg := config.DefaultConfig()
	menu, status := controls.Menu(cfg), controls.Status(cfg)
	n := len(menu)
	if n != len(status)+2 || !slices.Equal([]string{menu[n-3].ID(), menu[n-2].ID(), menu[n-1].ID()}, controls.Info) ||
		!slices.Equal(controls.Info, []string{dns.ID, timesync.ID, logforward.ID}) {
		t.Fatalf("Menu ends %q, %q, %q, of %d", menu[n-3].ID(), menu[n-2].ID(), menu[n-1].ID(), n)
	}
	for _, r := range risk.Rows() {
		if slices.Contains(controls.Info, r.ID) {
			t.Errorf("%s is a row of the risk level", r.ID)
		}
	}
	asked := config.DefaultConfig()
	asked.KernelSettings = true
	if m := controls.Menu(asked); len(m) != n {
		t.Errorf("kernel true: Menu has %d controls, want the same %d", len(m), n)
	}
}
