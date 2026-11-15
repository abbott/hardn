package main

import (
	"context"
	"time"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The end-of-support notice's mocks (docs/roadmap.md, "End-of-support
// notice"; docs/engine/gallery-eol.md): "uigallery eol" lists them. Each is
// the main menu's fixture on a release and a day, both the mock's own, with
// the notice hardn's main menu draws for them: output.NoticeOf, the one
// mapping from a platform and a day (docs/engine/screen-eol.md), over the
// support matrix's own dates. Nothing is read from a host or a clock.

// eolPrefix names the listing and its goldens.
const eolPrefix = "eol"

// eolLabel is the source label every mock's note opens with.
const eolLabel = "Proposed: no Figma frame."

// eolDay is a day, "2006-01-02", in UTC.
func eolDay(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err) // the mocks' own dates
	}
	return t
}

// eolMock is one release on one day.
type eolMock struct {
	name, note string
	// osr is the host's os-release, classified by detect.Classify; bar its
	// distribution bar.
	osr detect.OSRelease
	bar component.ContextBar
	// today is the day the notice is made for.
	today string
	// cve adds the CVE alert, as output.AlertOf words it.
	cve bool
}

// eolMocks are the mocks, in the listing's order.
func eolMocks() []eolMock {
	alpine := func(v string) (detect.OSRelease, component.ContextBar) {
		return detect.OSRelease{ID: "alpine", VersionID: v}, component.ContextBar{Name: "Alpine", Version: v}
	}
	a322, a322bar := alpine("3.22.4")
	a321, a321bar := alpine("3.21.7")
	jammy := detect.OSRelease{ID: "ubuntu", VersionID: "22.04"}
	jammyBar := component.ContextBar{Name: "Ubuntu", Version: "22.04", Codename: "Jammy"}
	return []eolMock{
		{name: "soon", osr: a322, bar: a322bar, today: "2027-03-15",
			note: "Alpine 3.22, Best effort, on 2027-03-15: 47 days before its support ends. hardn still changes it."},
		{name: "soon-month", osr: jammy, bar: jammyBar, today: "2027-04-01",
			note: "Ubuntu 22.04, Best effort, on 2027-04-01. Ubuntu names a month, so the notice does too, and counts from its last day."},
		{name: "ended", osr: a322, bar: a322bar, today: "2027-05-10",
			note: "Alpine 3.22 on 2027-05-10, in a hardn that still calls it Best effort: the notice says ended, and hardn still changes it."},
		{name: "out-of-scope", osr: a321, bar: a321bar, today: "2026-11-13",
			note: "Alpine 3.21, Out of scope, on 2026-11-13: the notice also says hardn refuses every change."},
		{name: "supported", osr: detect.OSRelease{ID: "debian", VersionID: "12"}, bar: component.ContextBar{Name: "Debian", Version: "12", Codename: "Bookworm"}, today: "2028-05-01",
			note: "Debian 12, Supported, on 2028-05-01: 60 days before its LTS ends. The notice is the same at every support level."},
		{name: "with-cve", osr: a322, bar: a322bar, today: "2027-03-15", cve: true,
			note: "The soon mock with the CVE alert: two callouts, the CVE's first, a blank line between."},
	}
}

// platform is the mock's release as hardn classifies it.
func (m eolMock) platform() detect.Platform {
	return detect.Classify(m.osr, detect.Proxmox{Installed: detect.No})
}

// view is the mock's main menu: the fixture's, on the mock's release, with
// the notice hardn makes for it on the mock's day and, for cve, the alert.
func (m eolMock) view() screen.MainMenu {
	vm := screen.MainMenuFixture()
	vm.Context = m.bar
	vm.Notice = output.NoticeOf(m.platform(), eolDay(m.today))
	if m.cve {
		vm.Alert = &screen.Alert{Title: output.AlertCVE, Fix: output.AlertFix, Item: screen.SSHTitle}
	}
	return vm
}

// eolListMocks are the listing's mocks: each mock's main menu.
func eolListMocks() []listMock {
	var out []listMock
	for _, m := range eolMocks() {
		out = append(out, listMock{name: m.name, note: m.note, draw: func(_ context.Context, c component.Ctx) (string, error) {
			return m.view().Render(c), nil
		}})
	}
	return out
}

// eolUsage is the listing's usage line.
const eolUsage = "usage: go run ./internal/tools/uigallery eol [flags] [mock | all]..."

// eolListing is the end-of-support listing.
var eolListing = listing{prefix: eolPrefix, usage: eolUsage, label: eolLabel, mocks: eolListMocks}
