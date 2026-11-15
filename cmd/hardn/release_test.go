package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
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
// main menu's alone: no flag and no subcommand builds a check that can ask
// (--version reads the cache alone: newerRelease).
func TestReleaseCheckDefaultOff(t *testing.T) {
	if config.DefaultConfig().CheckReleases {
		t.Error("checkReleases is on by default")
	}
	if _, ok := prodReleaseHost().(*host.Real); !ok || releaseTransport != nil {
		t.Error("the release check's seams are not production's")
	}
	// The notice's hardn.yml is read by the load that writes nothing, offers
	// nothing and never waits (docs/engine/gallery-release-notice.md, 3 (a)).
	if reflect.ValueOf(prodReleaseConfig).Pointer() != reflect.ValueOf(config.LoadExistingQuick).Pointer() {
		t.Error("releaseConfig is not config.LoadExistingQuick")
	}
}

// prodReleaseConfig and prodReleaseHost are production's releaseConfig and
// releaseHost: set before init, below, replaces them.
var (
	prodReleaseConfig = releaseConfig
	prodReleaseHost   = releaseHost
)

// TestReleaseNoticeHostileConfig: --version neither waits on nor grows
// with a hardn.yml that is a FIFO, a device or too large: nothing is said,
// and it answers as ever.
func TestReleaseNoticeHostileConfig(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo.yml")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(dir, "large.yml")
	if err := os.WriteFile(large, append([]byte("checkReleases: true\n"), bytes.Repeat([]byte("# x\n"), config.MaxQuickRead/4)...), 0o600); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "good.yml")
	if err := os.WriteFile(good, []byte("checkReleases: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
		says       bool
	}{
		{"a FIFO", fifo, false},
		{"a device", "/dev/zero", false},
		{"too large", large, false},
		{"a hardn.yml", good, true},
	} {
		{
			const arg = "--version"
			t.Run(tc.name, func(t *testing.T) {
				was, verWas := releaseConfig, Version
				releaseConfig, Version = prodReleaseConfig, "0.4.0"
				t.Cleanup(func() { releaseConfig, Version = was, verWas })
				f := hostfake.New(host.Options{})
				f.AddDir(revert.DefaultDir, 0o700, 0, 0)
				f.AddFile(revert.DefaultDir+"/"+version.ReleaseCacheName,
					[]byte(`{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.4.1"}`+"\n"), 0o600, 0, 0)
				useReleaseHost(t, f, &releaseServer{tag: "v0.4.1"})

				done := make(chan startRun, 1)
				go func() { done <- runStart(t, hostfake.New(host.Options{}), "-f", tc.path, arg) }()
				select {
				case r := <-done:
					if r.ended || r.stderr != "" || tc.says != strings.Contains(r.stdout, "v0.4.1 available") {
						t.Errorf("says it, want %t; ended %v; stderr %q:\n%s", tc.says, r.ended, r.stderr, r.stdout)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("still waiting on hardn.yml")
				}
			})
		}
	}
}

// No test reads this machine's hardn.yml for the release notice: the tests
// that want one say so (useReleaseConfig).
// Nor this machine's /var/lib/hardn for the question's state, which a run
// with no hardn.yml reads (releaseask.go): an empty host, unless the test
// gives one (useReleaseHost).
func init() {
	releaseConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
	releaseHost = func() host.System { return hostfake.New(host.Options{}) }
}

// useReleaseConfig makes the notice's hardn.yml one that sets
// checkReleases, on or off, or err, until the test ends, and returns where
// the path it was asked for (-f) is kept.
func useReleaseConfig(t *testing.T, on bool, err error) *string {
	t.Helper()
	was := releaseConfig
	t.Cleanup(func() { releaseConfig = was })
	asked := new(string)
	releaseConfig = func(path string) (*config.Config, error) {
		*asked = path
		if err != nil {
			return nil, err
		}
		yml := filepath.Join(t.TempDir(), "hardn.yml")
		if err := os.WriteFile(yml, fmt.Appendf(nil, "checkReleases: %t\n", on), 0o600); err != nil {
			t.Fatal(err)
		}
		return config.LoadExisting(yml)
	}
	return asked
}

// userHost is a host as a user other than root reads it: nothing under
// hardn's state directory, which is root's, 0700.
type userHost struct{ host.System }

func (h userHost) refusal(name string) error {
	if strings.HasPrefix(name, revert.DefaultDir+"/") {
		return &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return nil
}

func (h userHost) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	if err := h.refusal(name); err != nil {
		return nil, err
	}
	return h.System.Lstat(ctx, name)
}

func (h userHost) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	if err := h.refusal(name); err != nil {
		return nil, nil, err
	}
	return h.System.ReadFileNoFollow(ctx, name)
}

// TestReleaseNotice: --version says a newer release on its Version line,
// from the check's cache alone (docs/engine/gallery-release-notice.md,
// R1): never a request, never a write, and nothing said where
// checkReleases is off, hardn.yml is missing or not valid, the cache holds
// nothing newer or cannot be read, or the build has no release version.
func TestReleaseNotice(t *testing.T) {
	const (
		newer  = `{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.4.1"}`
		same   = `{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.4.0"}`
		older  = `{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.3.9"}`
		failed = `{"checked":"2026-11-13T09:00:00Z","answered":false,"failed":true}`
		line   = "hardn v0.4.1 available (https://github.com/abbott/hardn)"
	)
	for _, tc := range []struct {
		name    string
		version string
		on      bool
		loadErr error
		cache   string
		user    bool // not root
		planted bool // the cache is another user's
		says    bool
	}{
		{name: "a newer release", version: "0.4.0", on: true, cache: newer, says: true},
		{name: "the same release", version: "0.4.0", on: true, cache: same},
		{name: "an older release", version: "0.4.0", on: true, cache: older},
		{name: "a failed check", version: "0.4.0", on: true, cache: failed},
		{name: "no cache", version: "0.4.0", on: true},
		{name: "a cache that is not valid", version: "0.4.0", on: true, cache: `{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v9; rm"}`},
		{name: "another user's cache", version: "0.4.0", on: true, cache: newer, planted: true},
		{name: "checkReleases off", version: "0.4.0", cache: newer},
		{name: "hardn.yml missing", version: "0.4.0", on: true, loadErr: fs.ErrNotExist, cache: newer},
		{name: "hardn.yml not valid", version: "0.4.0", on: true, loadErr: errors.New("yaml: line 1"), cache: newer},
		{name: "not root", version: "0.4.0", on: true, cache: newer, user: true},
		{name: "a development build", version: "dev", on: true, cache: newer},
	} {
		for _, args := range [][]string{{"--version"}, {"-f", "/x/hardn.yml", "--version"}, {"-v"}} {
			t.Run(tc.name+" "+strings.Join(args, " "), func(t *testing.T) {
				was := Version
				Version = tc.version
				t.Cleanup(func() { Version = was })
				f := hostfake.New(host.Options{})
				f.AddDir(revert.DefaultDir, 0o700, 0, 0)
				path := revert.DefaultDir + "/" + version.ReleaseCacheName
				if tc.cache != "" {
					uid := 0
					if tc.planted {
						uid = 1000
					}
					f.AddFile(path, []byte(tc.cache+"\n"), 0o600, uid, uid)
				}
				var sys host.System = f
				if tc.user {
					sys = userHost{f}
				}
				server := &releaseServer{tag: "v0.4.1"}
				useReleaseHost(t, sys, server)
				asked := useReleaseConfig(t, tc.on, tc.loadErr)
				before, _ := f.Entry(path)

				r := runStart(t, hostfake.New(host.Options{}), args...)

				says := tc.says
				switch {
				case r.ended || r.stderr != "":
					t.Errorf("ended %v (%d); stderr:\n%s", r.ended, r.code, r.stderr)
				case says != strings.Contains(r.stdout, "\nVersion:    0.4.0 (v0.4.1 available)\nBuild Date: "):
					t.Errorf("says it %t, want %t:\n%s", !says, says, r.stdout)
				case !says && !strings.Contains(r.stdout, "\nVersion:    "+tc.version+"\n") || strings.Contains(r.stdout, line):
					t.Errorf("the Version line:\n%s", r.stdout)
				}
				if args[0] == "-f" && *asked != "/x/hardn.yml" {
					t.Errorf("hardn.yml asked for at %q, want -f's", *asked)
				}
				if after, _ := f.Entry(path); server.n.Load() != 0 || string(after.Data) != string(before.Data) {
					t.Errorf("%d requests; the cache was %q, is %q", server.n.Load(), before.Data, after.Data)
				}
				if (!tc.on || tc.loadErr != nil) && len(f.Calls()) != 0 {
					t.Errorf("the check off, and the host was read: %v", f.Calls())
				}
				if len(r.written(t)) != 0 {
					t.Errorf("written: %v", r.written(t))
				}
			})
		}
	}
}

// TestHelpNoReleaseNotice: the help is cobra's fixed text. It never says a
// newer release, and reads neither hardn.yml nor the check's cache
// (docs/engine/gallery-release-notice.md, decision 4 (a)).
func TestHelpNoReleaseNotice(t *testing.T) {
	was := Version
	Version = "0.4.0"
	t.Cleanup(func() { Version = was })
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"config", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			plain := runStart(t, hostfake.New(host.Options{}), args...)

			f := hostfake.New(host.Options{})
			f.AddDir(revert.DefaultDir, 0o700, 0, 0)
			f.AddFile(revert.DefaultDir+"/"+version.ReleaseCacheName,
				[]byte(`{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.4.1"}`+"\n"), 0o600, 0, 0)
			server := &releaseServer{tag: "v0.4.1"}
			useReleaseHost(t, f, server)
			asked := useReleaseConfig(t, true, nil)
			r := runStart(t, hostfake.New(host.Options{}), args...)

			if r.ended || r.stderr != "" || r.stdout != plain.stdout || !strings.Contains(r.stdout, "Usage:") || strings.Contains(r.stdout, "available") {
				t.Errorf("ended %v; stderr %q; with a newer release in the cache:\n%s\nwithout:\n%s", r.ended, r.stderr, r.stdout, plain.stdout)
			}
			if *asked != "" || len(f.Calls()) != 0 || server.n.Load() != 0 {
				t.Errorf("hardn.yml asked for at %q; host calls %v; %d requests", *asked, f.Calls(), server.n.Load())
			}
		})
	}
}
