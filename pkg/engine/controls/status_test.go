package controls_test

import (
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/controls"
	"github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/engine/risk"
)

// TestStatus: the main menu's status controls are the score's rows, by ID
// and title, in its order (risk's TestRows holds it), each once; the DNS
// control checks against the configuration it was given.
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
	if !slices.Equal(got, want) {
		t.Errorf("controls %q, want the rows %q", got, want)
	}
}
