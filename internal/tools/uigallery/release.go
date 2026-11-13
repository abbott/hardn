package main

import (
	"context"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/version"
)

// The release check's mocks (proposal P-15; docs/engine/gallery-release-check.md,
// docs/engine/screen-release-check.md): the main menu's one line, in each
// state of the check. Each is the main-menu fixture with what hardn's own
// check (version.Checker) reads from the mock's cache on an in-memory host,
// mapped as hardn maps it (output.ReleaseOf). No mock makes a request.

// releasePrefix names the listing and its goldens.
const releasePrefix = "release-check"

// releaseLabel is the source label every mock's note opens with.
const releaseLabel = "Proposed: no Figma frame."

// releaseVersion is the fixture's hardn, and releaseNow the mocks' clock.
const releaseVersion = "0.4.0"

var releaseNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

// releaseMock is one state of the check: hardn.yml's checkReleases and the
// cache the last check left, "" for none.
type releaseMock struct {
	name, note string
	on         bool
	cache      string
}

// releaseMocks are the mocks, in the listing's order.
func releaseMocks() []releaseMock {
	return []releaseMock{
		{name: "newer", on: true, cache: `{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.4.1"}`,
			note: "The check is on and v0.4.1 is out: the first line says so, in the link's place. The menu is no taller and nothing waits on it."},
		{name: "current", on: true, cache: `{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.4.0"}`,
			note: "The check is on and this is the latest release: nothing is shown."},
		{name: "pending", on: true,
			note: "The check is on and has not answered yet, as at the first start after turning it on: nothing is shown."},
		{name: "failed", on: true, cache: `{"checked":"2026-11-13T09:00:00Z","answered":false,"failed":true}`,
			note: "The check is on and the last one got no answer: a muted line in the link's place, until a check answers."},
		{name: "off", cache: `{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.4.1"}`,
			note: "The check is off, the default: no request is made and nothing is shown."},
	}
}

// release is what hardn's check knows on the mock's host.
func (m releaseMock) release(ctx context.Context) screen.Release {
	sys := hostfake.New(host.Options{})
	sys.AddDir(revert.DefaultDir, 0o700, 0, 0)
	if m.cache != "" {
		sys.AddFile(revert.DefaultDir+"/"+version.ReleaseCacheName, []byte(m.cache+"\n"), 0o600, 0, 0)
	}
	check := &version.Checker{On: m.on, Version: releaseVersion, System: sys, Now: func() time.Time { return releaseNow }}
	return output.ReleaseOf(check.Release(ctx))
}

// view is the mock's main menu.
func (m releaseMock) view(ctx context.Context) screen.MainMenu {
	vm := screen.MainMenuFixture()
	vm.Release = m.release(ctx)
	return vm
}

// releaseListMocks are the listing's mocks: each state's main menu.
func releaseListMocks() []listMock {
	var out []listMock
	for _, m := range releaseMocks() {
		out = append(out, listMock{name: m.name, note: m.note, draw: func(ctx context.Context, c component.Ctx) (string, error) {
			return m.view(ctx).Render(c), nil
		}})
	}
	return out
}

// releaseUsage is the listing's usage line.
const releaseUsage = "usage: go run ./internal/tools/uigallery release-check [flags] [mock | all]..."

// releaseListing is the release check's listing.
var releaseListing = listing{prefix: releasePrefix, usage: releaseUsage, label: releaseLabel, mocks: releaseListMocks}
