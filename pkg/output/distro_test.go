package output

import (
	"testing"

	"github.com/abbott/hardn/pkg/host/detect"
)

// TestDistroWords: the JSON's platform.distro for each distribution detect
// tells apart, as docs/reference/output.md lists them; one the table
// doesn't name is "unknown".
func TestDistroWords(t *testing.T) {
	for d, want := range map[detect.Distro]string{
		detect.DistroDebian: "debian", detect.DistroUbuntu: "ubuntu", detect.DistroAlpine: "alpine",
		detect.DistroProxmox: "proxmox", detect.DistroRocky: "rocky", detect.DistroAlma: "almalinux",
		detect.DistroRHEL: "rhel", detect.DistroOther: "other", detect.DistroUnknown: "unknown",
	} {
		if got := hostOf("web-01", detect.Facts{Platform: detect.Platform{Distro: d}}).Platform.Distro; got != want {
			t.Errorf("platform.distro of %q = %q, want %q", d, got, want)
		}
	}
}
