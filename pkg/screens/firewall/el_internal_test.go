package firewall

import (
	"testing"

	firewallctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// TestMainViewEL: on Rocky Linux and AlmaLinux the screen is the Firewall
// row and Return, even over an installed ufw (EPEL's): no item the control
// would refuse there (el-ports.md §1.6).
func TestMainViewEL(t *testing.T) {
	var r firewallctl.Reading
	r.State.Installed = true
	for _, d := range []detect.Distro{detect.DistroRocky, detect.DistroAlma} {
		v := mainView(r, component.StatusList{}, detect.Facts{Platform: detect.Platform{Distro: d}})
		if v.First != screen.FirewallNoFirst || v.Installed || v.Risk || v.DisableBeside {
			t.Errorf("%s: %+v", d, v)
		}
	}
}
