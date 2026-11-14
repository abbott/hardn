package main

import (
	"context"
	"io/fs"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/version"
)

// The release notice's mocks (docs/engine/gallery-release-notice.md):
// "v0.4.1 available" as hardn and its scripts say it, as adopted there
// (2026-11-14). The menu's and --version's are drawn through hardn's own
// check and code; the scripts' are fixed text. No mock makes a
// request.

// noticePrefix names the listing and its goldens.
const noticePrefix = "release-notice"

// The caches the mocks read: a newer release, and a failed check.
const (
	noticeNewerCache  = `{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.4.1"}`
	noticeFailedCache = `{"checked":"2026-11-13T09:00:00Z","answered":false,"failed":true}`
)

// notRootSys is a host as a user other than root reads it: nothing under
// hardn's state directory, which is root's, 0700.
type notRootSys struct{ host.System }

func (s notRootSys) refusal(name string) error {
	if strings.HasPrefix(name, revert.DefaultDir+"/") {
		return &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return nil
}

// Lstat is the host's, but for what is under the state directory.
func (s notRootSys) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	if err := s.refusal(name); err != nil {
		return nil, err
	}
	return s.System.Lstat(ctx, name)
}

// ReadFileNoFollow is the host's, but for what is under the state directory.
func (s notRootSys) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	if err := s.refusal(name); err != nil {
		return nil, nil, err
	}
	return s.System.ReadFileNoFollow(ctx, name)
}

// noticeRelease is what hardn's own check (version.Checker) knows from
// cache, "" for none, on an in-memory host, with the check on: read by
// root, or by a user other than root. It asks nothing: Latest is nil.
func noticeRelease(ctx context.Context, cache string, root bool) version.Release {
	fake := hostfake.New(host.Options{})
	fake.AddDir(revert.DefaultDir, 0o700, 0, 0)
	if cache != "" {
		fake.AddFile(revert.DefaultDir+"/"+version.ReleaseCacheName, []byte(cache+"\n"), 0o600, 0, 0)
	}
	var sys host.System = fake
	if !root {
		sys = notRootSys{fake}
	}
	check := &version.Checker{On: true, Version: releaseVersion, System: sys, Now: func() time.Time { return releaseNow }}
	return check.Release(ctx)
}

// noticeMenu is the main-menu fixture with what the check knows from
// cache: the notice is the line under the repository link.
func noticeMenu(ctx context.Context, cache string) screen.MainMenu {
	vm := screen.MainMenuFixture()
	vm.Release = output.ReleaseOf(noticeRelease(ctx, cache, true))
	return vm
}

// noticeHeads draws the main menu's header alone, for a newer release and
// for a failed check.
func noticeHeads(ctx context.Context, c component.Ctx) (string, error) {
	var out []string
	for _, h := range []struct{ label, cache string }{
		{"A newer release:", noticeNewerCache},
		{"A failed check:", noticeFailedCache},
	} {
		head, _, _ := strings.Cut(noticeMenu(ctx, h.cache).Render(c), sectionBreak)
		out = append(out, h.label+"\n"+head)
	}
	return apart(out...), nil
}

// noticeText draws lines as hardn or a script writes them off a terminal:
// from column 0, in no style, a line longer than the terminal left whole.
func noticeText(lines ...string) string { return strings.Join(lines, "\n") }

// noticeVersion draws hardn --version: the real PrintVersionInfo, with the
// newer release the check knows, read from cache by root or not.
func noticeVersion(cache string, root bool) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, _ component.Ctx) (string, error) {
		var b strings.Builder
		svc := version.NewService(releaseVersion, "2026-11-04T12:00:00Z", "0123456789abcdef0123456789abcdef01234567")
		if r := noticeRelease(ctx, cache, root); r.State == version.ReleaseNewer {
			svc.Newer = r.Latest
		}
		if err := svc.PrintVersionInfo(&b); err != nil {
			return "", err
		}
		return strings.TrimSuffix(b.String(), "\n"), nil
	}
}

// The fixed text: the last lines as hardn and the scripts write them, off a
// terminal (verify.sh's and install.sh's closing printf lines), then the
// notice. The lines above them are left out: some are wider than 80
// columns, which a golden refuses.
var (
	noticeLine      = "hardn v0.4.1 available (" + version.RepoURL + ")"
	noticeVerifyEnd = []string{
		"# Verified ./hardn-linux-amd64 (hardn v0.4.0)",
		"",
		noticeLine,
	}
	noticeInstallEnd = []string{
		"hardn comes with no warranty: GNU AGPL v3, sections 15 and 16.",
		"",
		"# Installed hardn v0.4.0 as /usr/local/bin/hardn",
		"",
		noticeLine,
		"",
		"Run hardn --help or sudo hardn",
	}
)

// noticeFixed returns a drawing of fixed text.
func noticeFixed(lines []string) func(context.Context, component.Ctx) (string, error) {
	return func(context.Context, component.Ctx) (string, error) { return noticeText(lines...), nil }
}

// noticeMocks are the listing's mocks, in order.
func noticeMocks() []listMock {
	menu := func(cache string) func(context.Context, component.Ctx) (string, error) {
		return func(ctx context.Context, c component.Ctx) (string, error) {
			return noticeMenu(ctx, cache).Render(c), nil
		}
	}
	return []listMock{
		{name: "menu-newer", draw: menu(noticeNewerCache),
			note: "The main menu with v0.4.1 out: the notice is the line under the repository link. The menu is one row taller while a notice shows."},
		{name: "menu-failed", draw: menu(noticeFailedCache),
			note: "The same for a check that got no answer: muted, under the link."},
		{name: "header", draw: noticeHeads,
			note: "The header alone. Below 64 columns the link is already under the version, and the notice goes under the link."},
		{name: "version-newer", draw: noticeVersion(noticeNewerCache, true),
			note: "hardn --version as root with v0.4.1 in the cache: the Version line says so. Read from the cache; no request. The Repo line is always there."},
		{name: "version-none", draw: noticeVersion(`{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.4.0"}`, true),
			note: "hardn --version with nothing newer in the cache. The same with no cache, a failed check or the check off."},
		{name: "version-not-root", draw: noticeVersion(noticeNewerCache, false),
			note: "hardn --version as a user other than root, v0.4.1 in the cache: nothing said. /var/lib/hardn is root's alone, so the cache cannot be read."},
		{name: "verify", draw: noticeFixed(noticeVerifyEnd),
			note: "The last line of sh verify.sh 0.4.0 linux-amd64, then the notice, v0.4.1 being out. Fixed text, as written off a terminal: verify.sh is a shell script."},
		{name: "install", wide: theme.ContentWidth, draw: noticeFixed(noticeInstallEnd),
			note: "The end of install.sh with HARDN_VERSION=0.4.0 and v0.4.1 out. Fixed text, as written off a terminal: install.sh is a shell script."},
	}
}

// noticeUsage is the listing's usage line.
const noticeUsage = "usage: go run ./internal/tools/uigallery release-notice [flags] [mock | all]..."

// noticeListing is the release notice's listing.
var noticeListing = listing{prefix: noticePrefix, usage: noticeUsage, label: releaseLabel, mocks: noticeMocks}
