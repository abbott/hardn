package output_test

import (
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// TestMainMenuOf checks that MainMenuOf fills the five blocks the status
// model owns as HeaderViewOf and StatusViewOf map them, with base's
// version, and keeps the rest of base, which it does not change.
func TestMainMenuOf(t *testing.T) {
	h := upFor(182*24*time.Hour + 2*time.Hour + 26*time.Minute)
	h.OS = header.OS{Name: "Debian", Version: "13", Codename: "trixie"}
	h.Hostname = header.Hostname{Name: "web-01"}
	h.Address.IPv4 = header.Family{Route: detect.Yes, Iface: "eth0", Addr: netip.MustParseAddr("192.0.2.20")}
	rs := figmaReports()
	l := risk.Assess(rs, h)
	base := screen.MainMenuFixture()
	base.Header.Version = "v9.9.9"

	got := output.MainMenuOf(base, h, rs, l)
	hv, sv := output.HeaderViewOf("v9.9.9", h), output.StatusViewOf(rs, l)
	want := base
	want.Header, want.Context, want.Host, want.Risk, want.Status = hv.Header, hv.Context, hv.Host, sv.Risk, sv.Status
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MainMenuOf = %+v\nwant %+v", got, want)
	}
	if got.Host.Hostname != "web-01" || got.Context.Codename != "Trixie" || got.Header.Version != "v9.9.9" || len(got.Status.Rows) != len(risk.Rows()) {
		t.Errorf("header %+v, bar %+v, host %+v, %d rows", got.Header, got.Context, got.Host, len(got.Status.Rows))
	}
	again := screen.MainMenuFixture()
	again.Header.Version = "v9.9.9"
	if !reflect.DeepEqual(base, again) {
		t.Error("MainMenuOf changed its base")
	}
}
