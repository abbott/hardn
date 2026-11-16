package detect_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host/detect"
)

func classify(id, version string) detect.Platform {
	return detect.Classify(detect.OSRelease{ID: id, VersionID: version}, detect.Proxmox{Installed: detect.No}, detect.Custom{TrueNAS: detect.No, RaspiOS: detect.No})
}

func day(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestEnd: a release's end of support is near from 90 days before its last
// day, that day included, and ended from the day after; a month the vendor
// names alone lasts to its last day and is worded as the month; no row, no
// date and a date in neither form are none.
func TestEnd(t *testing.T) {
	pve := func(v string) detect.Platform {
		return detect.Classify(detect.OSRelease{ID: "debian", VersionID: "13"}, detect.Proxmox{Installed: detect.Yes, Version: v}, detect.Custom{})
	}
	for _, tt := range []struct {
		name  string
		p     detect.Platform
		today string
		state detect.EndState
		date  string
		words string
	}{
		{"91 days before", classify("alpine", "3.22.4"), "2027-01-30", detect.EndLater, "2027-05-01", "2027-05-01"},
		{"90 days before", classify("alpine", "3.22.4"), "2027-01-31", detect.EndSoon, "2027-05-01", "2027-05-01"},
		{"the last day", classify("alpine", "3.22.4"), "2027-05-01", detect.EndSoon, "2027-05-01", "2027-05-01"},
		{"the day after", classify("alpine", "3.22.4"), "2027-05-02", detect.Ended, "2027-05-01", "2027-05-01"},
		{"years after", classify("alpine", "3.22.4"), "2031-01-01", detect.Ended, "2027-05-01", "2027-05-01"},
		{"a month: 91 days before its last day", classify("ubuntu", "22.04"), "2027-03-01", detect.EndLater, "2027-05", "May 2027"},
		{"a month: 90 days before", classify("ubuntu", "22.04"), "2027-03-02", detect.EndSoon, "2027-05", "May 2027"},
		{"a month: its last day", classify("ubuntu", "22.04"), "2027-05-31", detect.EndSoon, "2027-05", "May 2027"},
		{"a month: the next month", classify("ubuntu", "22.04"), "2027-06-01", detect.Ended, "2027-05", "May 2027"},
		{"Proxmox VE 8", pve("8.4.1"), "2026-11-13", detect.Ended, "2026-08", "August 2026"},
		{"Proxmox VE 9: no date", pve("9.2.22"), "2040-01-01", detect.EndNone, "", ""},
		{"no row", classify("alpine", "3.19.9"), "2040-01-01", detect.EndNone, "", ""},
		{"an Ubuntu interim release", classify("ubuntu", "25.10"), "2040-01-01", detect.EndNone, "", ""},
		{"a listed branch's unstable build", classify("alpine", "3.22.0_rc1"), "2040-01-01", detect.EndNone, "", ""},
		{"not established", detect.Platform{}, "2040-01-01", detect.EndNone, "", ""},
		{"a date in neither form", detect.Platform{Distro: detect.DistroAlpine, Version: "3.22", Ends: "May 2027"}, "2040-01-01", detect.EndNone, "", ""},
		{"a day that is no day", detect.Platform{Distro: detect.DistroAlpine, Version: "3.22", Ends: "2027-02-30"}, "2040-01-01", detect.EndNone, "", ""},
	} {
		got := tt.p.End(day(t, tt.today))
		if want := (detect.End{State: tt.state, Date: tt.date, Words: tt.words}); got != want {
			t.Errorf("%s: %+v, want %+v", tt.name, got, want)
		}
		if due := tt.state == detect.EndSoon || tt.state == detect.Ended; got.Due() != due {
			t.Errorf("%s: Due %v", tt.name, got.Due())
		}
	}
}

// TestEndIsTheClocksOwnDay: the day is the clock's calendar date where the
// clock is, whatever the hour: late on the last day is not ended, and the
// first minute of the next is.
func TestEndIsTheClocksOwnDay(t *testing.T) {
	p := classify("alpine", "3.22.4")
	for _, zone := range []*time.Location{time.UTC, time.FixedZone("east", 13*3600), time.FixedZone("west", -11*3600)} {
		if got := p.End(time.Date(2027, 5, 1, 23, 59, 0, 0, zone)).State; got != detect.EndSoon {
			t.Errorf("%v: 23:59 on the last day is %v", zone, got)
		}
		if got := p.End(time.Date(2027, 5, 2, 0, 1, 0, 0, zone)).State; got != detect.Ended {
			t.Errorf("%v: 00:01 the day after is %v", zone, got)
		}
		if got := p.End(time.Date(2027, 1, 30, 23, 59, 0, 0, zone)).State; got != detect.EndLater {
			t.Errorf("%v: 23:59, 91 days before, is %v", zone, got)
		}
	}
}

// TestMatrixEnds: every row but Proxmox VE 9's has a date, a day or a
// month; Classify hands it on; each distribution has a name; and a
// distribution's supported rows are newest first, which Newest and Upgrade
// rest on.
func TestMatrixEnds(t *testing.T) {
	want := map[string]string{
		"debian 13": "2030-06-30", "debian 12": "2028-06-30",
		"ubuntu 26.04": "2031-05", "ubuntu 24.04": "2029-05", "ubuntu 22.04": "2027-05",
		"alpine 3.24": "2028-06-01", "alpine 3.23": "2027-11-01", "alpine 3.22": "2027-05-01",
		"alpine 3.21": "2026-11-01", "alpine 3.20": "2026-04-01",
		"proxmox 9": "", "proxmox 8": "2026-08",
		"rocky 10": "2035-05-31", "rocky 9": "2032-05-31", "almalinux 10": "2035-05-31", "almalinux 9": "2032-05-31",
		"rhel 10": "2035-05-31", "rhel 9": "2032-05-31",
		"centos 10": "2030-05-31", "centos 9": "2027-05-31", "ol 10": "2035-06", "ol 9": "2032-06",
	}
	far := day(t, "2100-01-01")
	last := map[detect.Distro]float64{}
	for _, r := range detect.Matrix() {
		key := string(r.Distro) + " " + r.Version
		if got, ok := want[key]; !ok || got != r.Ends {
			t.Errorf("%s ends %q, want %q", key, r.Ends, got)
		}
		delete(want, key)
		p := detect.Platform{Distro: r.Distro, Version: r.Version, Support: r.Support, Ends: r.Ends}
		if state := p.End(far).State; (state == detect.Ended) != (r.Ends != "") {
			t.Errorf("%s: %q read as state %v", key, r.Ends, state)
		}
		if r.Distro.Name() == "" {
			t.Errorf("%s has no name", key)
		}
		if r.Support != detect.Supported {
			continue
		}
		v, err := strconv.ParseFloat(r.Version, 64)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if before, ok := last[r.Distro]; ok && v >= before {
			t.Errorf("%s is after an older supported row", key)
		}
		last[r.Distro] = v
	}
	if len(want) != 0 {
		t.Errorf("rows the matrix lacks: %v", want)
	}
	if got := classify("alpine", "3.22.4").Ends; got != "2027-05-01" {
		t.Errorf("Classify(Alpine 3.22.4).Ends = %q", got)
	}
	for _, d := range []detect.Distro{detect.DistroUnknown, detect.DistroOther, "fedora"} {
		if d.Name() != "" {
			t.Errorf("%q has the name %q", d, d.Name())
		}
	}
}

// TestUpgrade: the release to upgrade to is the newest supported one of
// the distribution, and none from that release itself or for a
// distribution with no supported row.
func TestUpgrade(t *testing.T) {
	for _, tt := range []struct {
		p    detect.Platform
		want string
	}{
		{classify("alpine", "3.20.9"), "alpine 3.24"},
		{classify("alpine", "3.23.1"), "alpine 3.24"},
		{classify("alpine", "3.24.1"), ""},
		{classify("ubuntu", "22.04"), "ubuntu 26.04"},
		{classify("debian", "12"), "debian 13"},
		{classify("rocky", "9.8"), "rocky 10"},
		{classify("almalinux", "9.8"), "almalinux 10"},
		{classify("rhel", "9.9"), "rhel 10"},
		{classify("centos", "9"), "centos 10"},
		{classify("ol", "9.8"), "ol 10"},
		{detect.Platform{Distro: detect.DistroProxmox, Version: "8"}, "proxmox 9"},
		{detect.Platform{Distro: detect.DistroProxmox, Version: "9"}, ""},
		{classify("fedora", "43"), ""},
		{detect.Platform{}, ""},
	} {
		r, ok := tt.p.Upgrade()
		if got := strings.TrimSpace(string(r.Distro) + " " + r.Version); got != tt.want || ok != (tt.want != "") {
			t.Errorf("%s %s: upgrade to %q (%v), want %q", tt.p.Distro, tt.p.Version, got, ok, tt.want)
		}
	}
}
