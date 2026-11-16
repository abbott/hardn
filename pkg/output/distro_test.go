package output

import (
	"encoding/json"
	"strings"
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
		detect.DistroRHEL: "rhel", detect.DistroCentOS: "centos", detect.DistroOracle: "ol",
		detect.DistroOther: "other", detect.DistroUnknown: "unknown",
	} {
		if got := hostOf("web-01", detect.Facts{Platform: detect.Platform{Distro: d}}).Platform.Distro; got != want {
			t.Errorf("platform.distro of %q = %q, want %q", d, got, want)
		}
	}
}

// TestSupportEnds: the document's platform carries the matrix's date as
// the matrix writes it, a day or a month, and "" for none; never a word
// counted from the clock.
func TestSupportEnds(t *testing.T) {
	for id, tt := range map[string]struct{ version, want string }{
		"alpine": {"3.22.4", "2027-05-01"},
		"ubuntu": {"22.04", "2027-05"},
		"fedora": {"43", ""},
	} {
		p := detect.Classify(detect.OSRelease{ID: id, VersionID: tt.version}, detect.Proxmox{Installed: detect.No}, detect.Custom{TrueNAS: detect.No, RaspiOS: detect.No})
		if got := hostOf("web-01", detect.Facts{Platform: p}).Platform.SupportEnds; got != tt.want {
			t.Errorf("%s %s: supportEnds %q, want %q", id, tt.version, got, tt.want)
		}
	}
}

// TestVariantWords: the JSON's platform.variant, as docs/reference/output.md
// lists them: "" for none.
func TestVariantWords(t *testing.T) {
	for v, want := range map[detect.Variant]string{
		detect.VariantNone: "", detect.VariantTrueNAS: "truenas", detect.VariantRaspiOS: "raspios",
	} {
		got := hostOf("web-01", detect.Facts{Platform: detect.Platform{Distro: detect.DistroDebian, Variant: v}}).Platform
		if got.Variant != want || got.Distro != "debian" {
			t.Errorf("platform of %q = %+v, want debian, variant %q", v, got, want)
		}
	}
	data, err := json.Marshal(hostOf("web-01", detect.Facts{Platform: detect.Platform{Distro: detect.DistroDebian, Variant: detect.VariantRaspiOS}}).Platform)
	if err != nil || !strings.Contains(string(data), `"variant":"raspios"`) {
		t.Errorf("platform JSON %s, %v; want variant raspios", data, err)
	}
}
