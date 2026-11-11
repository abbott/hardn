package firewall_test

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// lxc is detect's container as an LXC's PID 1 environment establishes it.
var lxc = detect.Container{Detected: detect.Yes, Signal: detect.InitEnviron, Manager: "lxc"}

// inLXC sets the facts to lxc's container.
func inLXC(f *detect.Facts) { f.Container = lxc }

// TestContainer: in a container (the maintainer, 2026-11-11), ufw's and
// firewalld's rows keep their state, detail and findings, and the evidence
// ends with the signal that established it, in DNS's form; a container not
// established adds nothing, and on Proxmox VE nothing is added; the risk
// level is the same.
func TestContainer(t *testing.T) {
	const line = "container: " + detect.InitEnviron
	for _, c := range []row{
		{name: "alpine, ufw not installed", site: site{plat: alpine324(), ufw: fw{missing: true}}, state: engine.NotConfigured, want: "ufw not installed"},
		{name: "debian, deny policy, 22 allowed", site: site{plat: debian13(), ufw: allow22}, state: engine.Configured, want: "deny policy"},
		{name: "debian, inactive", site: site{plat: debian13()}, state: engine.NotConfigured, want: "inactive"},
	} {
		t.Run(c.name, func(t *testing.T) {
			plain := run(t, c)
			in := c
			in.site.facts = inLXC
			got := run(t, in)
			if want := append(slices.Clone(plain.Evidence), line); !slices.Equal(got.Evidence, want) {
				t.Errorf("evidence\n%q\nwant\n%q", got.Evidence, want)
			}
			if !reflect.DeepEqual(got.Findings, plain.Findings) {
				t.Errorf("findings %+v, want %+v", got.Findings, plain.Findings)
			}
			unknown := c
			unknown.site.facts = func(f *detect.Facts) { f.Container = detect.Container{Detected: detect.Unknown} }
			if st := run(t, unknown); !slices.Equal(st.Evidence, plain.Evidence) {
				t.Errorf("container not established: evidence %q, want %q", st.Evidence, plain.Evidence)
			}
			report := func(st engine.Status) []engine.Report {
				return []engine.Report{{Control: firewall.ID, Title: "Firewall", Status: st}}
			}
			if a, b := risk.Assess(report(plain), header.Header{}), risk.Assess(report(got), header.Header{}); !reflect.DeepEqual(a, b) {
				t.Errorf("risk %+v in a container, %+v out of one", b, a)
			}
		})
	}
	t.Run("proxmox", func(t *testing.T) {
		st := run(t, row{site: site{plat: pve9(), pve: pf{cluster: pveOn}, facts: inLXC}, state: engine.Configured, want: "drop policy"})
		if slices.Contains(st.Evidence, line) {
			t.Errorf("Proxmox VE: evidence %q", st.Evidence)
		}
	})
	t.Run("firewalld", func(t *testing.T) {
		f := debian13().facts
		f.Platform = detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Supported}
		f.Container = lxc
		h := hostfake.New(host.Options{})
		fwUnit(h, "inactive", "disabled")
		st, err := firewall.Incoming{}.Check(context.Background(), h, f)
		if err != nil || st.State != engine.NotConfigured || st.Detail != firewall.DetailFirewalldOff || len(st.Evidence) == 0 || st.Evidence[len(st.Evidence)-1] != line {
			t.Errorf("%s %q, %v, evidence %q", st.State, st.Detail, err, st.Evidence)
		}
	})
}
