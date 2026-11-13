package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/version"
)

// releaseServer counts the requests it is given and answers each with tag.
type releaseServer struct {
	n   atomic.Int32
	tag string
}

func (s *releaseServer) RoundTrip(r *http.Request) (*http.Response, error) {
	s.n.Add(1)
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"tag_name":"` + s.tag + `"}`)), Request: r}, nil
}

// useReleaseHost points the release check at f, server and a fixed clock
// until the test ends. No test makes a network request.
func useReleaseHost(t *testing.T, f host.System, server http.RoundTripper) {
	t.Helper()
	hostWas, transportWas, nowWas := releaseHost, releaseTransport, releaseNow
	t.Cleanup(func() { releaseHost, releaseTransport, releaseNow = hostWas, transportWas, nowWas })
	releaseHost = func() host.System { return f }
	releaseTransport = server
	releaseNow = func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) }
}

// TestWithReleaseCheck: the main menu gets the release check only where
// hardn.yml's checkReleases is on, in a dry run (-n or hardn.yml's
// dryRun) as in a live session. Off, the default, it gets neither hook:
// no request is made and no file is read or written.
func TestWithReleaseCheck(t *testing.T) {
	for _, tc := range []struct {
		name       string
		on, dryRun bool
		checks     bool
	}{
		{name: "the default"},
		{name: "on", on: true, checks: true},
		{name: "on, a dry run", on: true, dryRun: true, checks: true},
		{name: "off, a dry run", dryRun: true},
	} {
		f := hostfake.New(host.Options{})
		f.AddDir(revert.DefaultDir, 0o700, 0, 0)
		server := &releaseServer{tag: "v0.4.1"}
		useReleaseHost(t, f, server)
		c := config.DefaultConfig()
		c.CheckReleases, c.DryRun = tc.on, tc.dryRun
		m := withReleaseCheck(mainmenu.Options{}, c, "0.4.0")
		if (m.Release != nil) != tc.checks || (m.CheckRelease != nil) != tc.checks {
			t.Errorf("%s: Release set %t, CheckRelease set %t, want both %t", tc.name, m.Release != nil, m.CheckRelease != nil, tc.checks)
		}
		if !tc.checks {
			if server.n.Load() != 0 || len(f.Calls()) != 0 {
				t.Errorf("%s: %d requests, host calls %v", tc.name, server.n.Load(), f.Calls())
			}
			continue
		}
		ctx := context.Background()
		if got := m.Release(ctx); got != (screen.Release{State: screen.ReleasePending}) || server.n.Load() != 0 {
			t.Errorf("%s, before the check: %+v, %d requests", tc.name, got, server.n.Load())
		}
		m.CheckRelease(ctx)
		if got := m.Release(ctx); got != (screen.Release{State: screen.ReleaseNewer, Latest: "v0.4.1"}) || server.n.Load() != 1 {
			t.Errorf("%s, after it: %+v, %d requests", tc.name, got, server.n.Load())
		}
		if _, ok := f.Entry(revert.DefaultDir + "/" + version.ReleaseCacheName); !ok {
			t.Errorf("%s: no cache written", tc.name)
		}
	}
}

// TestReleaseCheckDefaultOff: checkReleases is off unless hardn.yml says
// otherwise, and menuOptions, the one caller of withReleaseCheck, is the
// main menu's alone: no flag and no subcommand builds a check.
func TestReleaseCheckDefaultOff(t *testing.T) {
	if config.DefaultConfig().CheckReleases {
		t.Error("checkReleases is on by default")
	}
	if _, ok := releaseHost().(*host.Real); !ok || releaseTransport != nil {
		t.Error("the release check's seams are not production's")
	}
}
