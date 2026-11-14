package controls_test

import (
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls"
	"github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/engine/controls/kernelsettings"
	"github.com/abbott/hardn/pkg/engine/controls/logforward"
	"github.com/abbott/hardn/pkg/engine/controls/timesync"
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

// TestMenu: the main menu checks Status's controls, then Log Forwarding
// and Time Sync, which are no rows of risk's (the risk level never counts
// them); Time Sync's report is drawn on every host but a container
// (gallery-timesync.md, decision 11 (a)), and Log Forwarding's
// report is drawn only where hardn.yml names a server or the host forwards
// (gallery-logforward.md, decision 10 (a)).
func TestMenu(t *testing.T) {
	cfg := config.DefaultConfig()
	menu, status := controls.Menu(cfg), controls.Status(cfg)
	if n := len(menu); n != len(status)+2 || menu[n-2].ID() != logforward.ID || menu[n-1].ID() != timesync.ID {
		t.Fatalf("Menu's last control is %q, of %d", menu[len(menu)-1].ID(), len(menu))
	}
	for _, r := range risk.Rows() {
		if r.ID == logforward.ID || r.ID == timesync.ID {
			t.Errorf("%s is a row of the risk level", r.ID)
		}
	}
	// Kernel Settings is checked only where hardn.yml's kernelSettings is
	// true (gallery-sysctl.md, decision 18 (a)), and never drawn in a
	// container (decision 16 (a)).
	asked := config.DefaultConfig()
	asked.KernelSettings = true
	if m := controls.Menu(asked); len(m) != len(menu)+1 || m[len(m)-1].ID() != kernelsettings.ID {
		t.Errorf("kernelSettings true: Menu's last control is %q, of %d", m[len(m)-1].ID(), len(m))
	}
	toSet := engine.Report{Control: kernelsettings.ID, Status: engine.Status{State: engine.NotConfigured, Detail: "4 to set"}}
	boxed := engine.Report{Control: kernelsettings.ID, Status: engine.Status{State: engine.NotApplicable, Detail: kernelsettings.DetailContainer}}
	if got := controls.MenuReports(asked, []engine.Report{toSet, boxed}); len(got) != 1 || got[0].Status.State != engine.NotConfigured {
		t.Errorf("Kernel Settings: reports %+v, want every host's but a container's", got)
	}
	clock := engine.Report{Control: timesync.ID, Status: engine.Status{State: engine.Partial, Detail: timesync.DetailNotInSync}}
	inside := engine.Report{Control: timesync.ID, Status: engine.Status{State: engine.NotApplicable, Detail: timesync.DetailContainer}}
	if got := controls.MenuReports(cfg, []engine.Report{clock, inside}); len(got) != 1 || got[0].Status.State != engine.Partial {
		t.Errorf("Time Sync: reports %+v, want every host's but a container's", got)
	}
	off := engine.Report{Control: logforward.ID, Status: engine.Status{State: engine.NotConfigured, Detail: logforward.DetailOff}}
	on := engine.Report{Control: logforward.ID, Status: engine.Status{State: engine.Configured, Detail: "192.0.2.50:514 TCP"}}
	other := engine.Report{Control: status[0].ID()}
	if got := controls.MenuReports(cfg, []engine.Report{other, off}); len(got) != 1 || got[0].Control != other.Control {
		t.Errorf("no server, not forwarding: reports %+v", got)
	}
	if got := controls.MenuReports(cfg, []engine.Report{other, on}); len(got) != 2 {
		t.Errorf("the host forwards: reports %+v", got)
	}
	named := config.DefaultConfig()
	named.LogForwarding.Server = "192.0.2.50"
	if got := controls.MenuReports(named, []engine.Report{other, off}); len(got) != 2 {
		t.Errorf("hardn.yml names a server: reports %+v", got)
	}
}
