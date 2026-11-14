package main

// The release check (proposal P-15; docs/engine/screen-release-check.md):
// off unless hardn.yml's checkReleases turns it on, and then at the main
// menu alone, never for a flag or a subcommand. A dry run changes nothing
// about it, as adopted (docs/engine/gallery-release-check.md §6).
// --version says what its cache holds (newerRelease) and asks nothing (docs/engine/gallery-release-notice.md).

import (
	"context"
	"net/http"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/version"
	"github.com/abbott/hardn/pkg/version/github"
)

// Seams for the tests: the host the check's cache is on, with no run log,
// as the status's (statusHost); the transport of its one request, nil for
// github.Transport; and its clock. No test makes a network request.
var (
	releaseHost      = func() host.System { return host.NewReal(host.Options{}) }
	releaseTransport http.RoundTripper // nil: github.Transport()
	releaseNow       = time.Now
)

// releaseConfig is hardn.yml as --version reads it, for
// checkReleases alone: config.LoadExistingQuick, which writes nothing,
// offers nothing, and never waits on the file or reads more than 1 MiB of
// it. A seam for the tests.
var releaseConfig = config.LoadExistingQuick

// newerRelease is the newer release's tag the release check's cache holds,
// such as "v0.4.1", for --version; "" where there is none to
// say. It makes no request (Latest is nil) and writes nothing. It says
// nothing of why: hardn.yml missing, not valid, not a regular file or
// larger than 1 MiB (-f, HARDN_CONFIG, then the search path), checkReleases off, a development build, a cache that
// is missing, fails a check or cannot be read (a user other than root:
// /var/lib/hardn is root's alone), a failed check, or no newer release.
func newerRelease(ctx context.Context) string {
	c, err := releaseConfig(configFile)
	if err != nil || c == nil || !c.CheckReleases {
		return ""
	}
	check := &version.Checker{On: true, Version: Version, System: releaseHost(), Now: releaseNow}
	if r := check.Release(ctx); r.State == version.ReleaseNewer {
		return r.Latest
	}
	return ""
}

// withReleaseCheck returns m with the release check, where c's
// checkReleases is on: what it knows, drawn on each pass
// (mainmenu.Options.Release), and its request, run once beside the main
// menu (mainmenu.Options.CheckRelease). Otherwise m as it is: no request,
// and the cache neither read nor written. In a dry run it checks as ever:
// the check changes nothing on the host but its cache. A failed check is
// one line of hardn's log, where the log is open: a dry run opens none
// (liveWrites).
func withReleaseCheck(m mainmenu.Options, c *config.Config, ver string) mainmenu.Options {
	if !c.CheckReleases {
		return m
	}
	check := &version.Checker{On: true, Version: ver, System: releaseHost(), Now: releaseNow,
		Latest: github.Client{Version: ver, Transport: releaseTransport}.Latest,
		Log:    func(line string) { hardnLog.Printf(output.LogWarning, "%s", line) }}
	m.Release = func(ctx context.Context) screen.Release { return output.ReleaseOf(check.Release(ctx)) }
	m.CheckRelease = check.Check
	return m
}
