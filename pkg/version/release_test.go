package version

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The release check, on hostfake and a fake transport: no test makes a
// network request, and none reads this machine's clock for the check.

const cachePath = revert.DefaultDir + "/" + ReleaseCacheName

var testNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

// fakeHost is a host with hardn's state directory, root's, and the cache
// where content is not "".
func fakeHost(content string) *hostfake.System {
	f := hostfake.New(host.Options{})
	f.AddDir(revert.DefaultDir, 0o700, 0, 0)
	if content != "" {
		f.AddFile(cachePath, []byte(content), 0o600, 0, 0)
	}
	return f
}

// server stands for GitHub: it counts the requests and answers each with
// answer.
type server struct {
	n      atomic.Int32
	answer func(ctx context.Context) (string, error)
}

func (s *server) latest(ctx context.Context) (string, error) {
	s.n.Add(1)
	return s.answer(ctx)
}

func (s *server) count() int { return int(s.n.Load()) }

// tagged answers with tag, and failing with err.
func tagged(tag string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return tag, nil }
}

func failing(err string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return "", errors.New(err) }
}

// stalled is a server that never answers: it returns when ctx is done.
func stalled(ctx context.Context) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

// checker is the check on f at testNow, as v0.4.0, its request answered by
// tr and its log lines kept in *lines.
func checker(f host.System, tr *server) (*Checker, *[]string) {
	lines := &[]string{}
	return &Checker{On: true, Version: "0.4.0", System: f, Latest: tr.latest, Now: func() time.Time { return testNow },
		Log: func(l string) { *lines = append(*lines, l) }}, lines
}

func cacheOf(t *testing.T, f *hostfake.System) string {
	t.Helper()
	e, ok := f.Entry(cachePath)
	if !ok {
		return ""
	}
	return strings.TrimSpace(string(e.Data))
}

func TestParseTag(t *testing.T) {
	for s, want := range map[string]Tag{"v0.4.1": {0, 4, 1}, "v10.0.123456": {10, 0, 123456}, "v01.2.3": {1, 2, 3}} {
		if got, ok := ParseTag(s); !ok || got != want {
			t.Errorf("%q: %v %v", s, got, ok)
		}
	}
	for _, s := range []string{"", "v", "0.4.1", "V0.4.1", "v0.4", "v0.4.1.2", "v0.4.", "v.4.1", "v0..1", "v0.4.1-rc1", "v0.4.1 ", " v0.4.1",
		"v99; rm", "v0.4.1\n", "v0.4.1\x1b[31m", "v-1.0.0", "v+1.0.0", "v1.0.1234567", "v１.0.0", "v1.0.0\x00"} {
		if got, ok := ParseTag(s); ok {
			t.Errorf("%q passed: %v", s, got)
		}
	}
	if !(Tag{0, 10, 0}).newer(Tag{0, 9, 9}) || (Tag{0, 4, 0}).newer(Tag{0, 4, 0}) || (Tag{0, 3, 9}).newer(Tag{0, 4, 0}) || !(Tag{1, 0, 0}).newer(Tag{0, 99, 99}) {
		t.Error("newer compares by number, major first")
	}
}

// TestReleaseFromCache: each state the main menu shows, read from the
// cache alone, with no request.
func TestReleaseFromCache(t *testing.T) {
	for _, tc := range []struct {
		name, cache string
		want        Release
	}{
		{"newer", `{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.4.1"}`, Release{ReleaseNewer, "v0.4.1"}},
		{"current", `{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.4.0"}`, Release{State: ReleaseCurrent}},
		{"an older tag is up to date", `{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.3.9"}`, Release{State: ReleaseCurrent}},
		{"pending: no cache", "", Release{State: ReleasePending}},
		{"failed", `{"checked":"2026-11-13T09:00:00Z","answered":false,"failed":true}`, Release{State: ReleaseFailed}},
		{"pending: an attempt that never ended", `{"checked":"2026-11-13T09:00:00Z","answered":false}`, Release{State: ReleasePending}},
		{"checked at this instant", `{"checked":"2026-11-13T12:00:00Z","answered":true,"tag":"v1.0.0"}`, Release{ReleaseNewer, "v1.0.0"}},
	} {
		tr := &server{answer: tagged("v9.9.9")}
		c, _ := checker(fakeHost(tc.cache), tr)
		if got := c.Release(context.Background()); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
		if tr.count() != 0 {
			t.Errorf("%s: Release made a request", tc.name)
		}
	}
}

// TestPlantedCacheNeverDrawn: a cache that fails any check, of its
// directory, its file or its content, counts as no attempt: nothing from
// it is returned, whatever it claims.
func TestPlantedCacheNeverDrawn(t *testing.T) {
	const claim = `{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v99.0.0"}`
	plant := func(set func(f *hostfake.System)) *hostfake.System {
		f := hostfake.New(host.Options{})
		f.AddDir(revert.DefaultDir, 0o700, 0, 0)
		set(f)
		return f
	}
	file := func(content string, perm fs.FileMode, uid int) func(*hostfake.System) {
		return func(f *hostfake.System) { f.AddFile(cachePath, []byte(content), perm, uid, uid) }
	}
	for name, f := range map[string]*hostfake.System{
		"another user's file": plant(file(claim, 0o600, 1000)),
		"group-writable":      plant(file(claim, 0o660, 0)),
		"world-writable":      plant(file(claim, 0o606, 0)),
		"a symlink": plant(func(f *hostfake.System) {
			f.AddFile("/tmp/planted.json", []byte(claim), 0o600, 0, 0)
			f.AddSymlink(cachePath, "/tmp/planted.json")
		}),
		"a directory": plant(func(f *hostfake.System) { f.AddDir(cachePath, 0o700, 0, 0) }),
		"another user's directory": plant(func(f *hostfake.System) {
			f.AddDir(revert.DefaultDir, 0o700, 1000, 1000)
			f.AddFile(cachePath, []byte(claim), 0o600, 0, 0)
		}),
		"a group-writable directory": plant(func(f *hostfake.System) {
			f.AddDir(revert.DefaultDir, 0o770, 0, 0)
			f.AddFile(cachePath, []byte(claim), 0o600, 0, 0)
		}),
		"larger than 4 KiB":     plant(file(claim+strings.Repeat(" ", ReleaseMaxCache), 0o600, 0)),
		"a future time":         plant(file(`{"checked":"2026-11-13T12:00:01Z","answered":true,"tag":"v99.0.0"}`, 0o600, 0)),
		"no time":               plant(file(`{"answered":true,"tag":"v99.0.0"}`, 0o600, 0)),
		"a tag that is not one": plant(file(`{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v99; rm"}`, 0o600, 0)),
		"a tag with an escape":  plant(file(`{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v99.0.0\u001b[31m"}`, 0o600, 0)),
		"answered with no tag":  plant(file(`{"checked":"2026-11-13T09:00:00Z","answered":true}`, 0o600, 0)),
		"answered and failed":   plant(file(`{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v99.0.0","failed":true}`, 0o600, 0)),
		"a tag, not answered":   plant(file(`{"checked":"2026-11-13T09:00:00Z","answered":false,"tag":"v99.0.0"}`, 0o600, 0)),
		"an unknown field":      plant(file(`{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v99.0.0","url":"https://attacker.example"}`, 0o600, 0)),
		"two values":            plant(file(claim+claim, 0o600, 0)),
		"not JSON":              plant(file("v99.0.0\n", 0o600, 0)),
		"empty":                 plant(file(" ", 0o600, 0)),
		"origin/main's format":  plant(file(`{"last_check":"2026-11-13T09:00:00Z","latest_release":{"tag_name":"v99.0.0"}}`, 0o600, 0)),
	} {
		c, _ := checker(f, &server{answer: failing("status 500")})
		if got := c.Release(context.Background()); got != (Release{State: ReleasePending}) {
			t.Errorf("%s: %+v, want pending and nothing from the file", name, got)
		}
	}
}

// unreadable fails every read of a file.
type unreadable struct{ *hostfake.System }

func (unreadable) ReadFileNoFollow(context.Context, string) ([]byte, fs.FileInfo, error) {
	return nil, nil, fs.ErrPermission
}

// TestCacheUnreadable: a cache that cannot be read counts as no attempt.
func TestCacheUnreadable(t *testing.T) {
	c, _ := checker(unreadable{fakeHost(`{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v0.4.1"}`)}, &server{answer: failing("status 500")})
	if got := c.Release(context.Background()); got != (Release{State: ReleasePending}) {
		t.Errorf("%+v", got)
	}
}

// TestOffTouchesNothing: with checkReleases off, or on a build with no
// release version, nothing is requested and no file is read or written.
func TestOffTouchesNothing(t *testing.T) {
	for name, set := range map[string]func(*Checker){
		"off":                 func(c *Checker) { c.On = false },
		"a development build": func(c *Checker) { c.Version = "" },
		"a build off a tag":   func(c *Checker) { c.Version = "0.4.0-3-gabc1234" },
	} {
		f := fakeHost(`{"checked":"2026-11-01T09:00:00Z","answered":true,"tag":"v0.4.1"}`)
		tr := &server{answer: tagged("v0.4.1")}
		c, lines := checker(f, tr)
		set(c)
		c.Check(context.Background())
		if got := c.Release(context.Background()); got != (Release{}) {
			t.Errorf("%s: %+v, want off", name, got)
		}
		if tr.count() != 0 || len(f.Calls()) != 0 || len(*lines) != 0 {
			t.Errorf("%s: %d requests, host calls %v, log %v", name, tr.count(), f.Calls(), *lines)
		}
	}
	var none *Checker
	none.Check(context.Background())
	if got := none.Release(context.Background()); got != (Release{}) {
		t.Errorf("nil: %+v", got)
	}
}

// TestCheckAnswers: a due check makes one request and records its tag;
// nothing is asked again for 24 hours, counted from the attempt.
func TestCheckAnswers(t *testing.T) {
	f := fakeHost("")
	tr := &server{answer: tagged("v0.4.1")}
	c, lines := checker(f, tr)
	c.Check(context.Background())
	if tr.count() != 1 {
		t.Fatalf("%d requests", tr.count())
	}
	if got := c.Release(context.Background()); got != (Release{ReleaseNewer, "v0.4.1"}) {
		t.Errorf("%+v", got)
	}
	if got, want := cacheOf(t, f), `{"checked":"2026-11-13T12:00:00Z","answered":true,"tag":"v0.4.1"}`; got != want {
		t.Errorf("cache %s, want %s", got, want)
	}
	if e, _ := f.Entry(cachePath); e.Mode.Perm() != 0o600 || e.UID != 0 {
		t.Errorf("cache mode %v uid %d", e.Mode, e.UID)
	}
	if len(*lines) != 0 {
		t.Errorf("log: %v", *lines)
	}
	// A new process reads the same from the cache, and asks nothing for 24 hours.
	for _, later := range []time.Duration{0, time.Hour, ReleaseEvery - time.Second} {
		c2, _ := checker(f, tr)
		c2.Now = func() time.Time { return testNow.Add(later) }
		c2.Check(context.Background())
		if got := c2.Release(context.Background()); got != (Release{ReleaseNewer, "v0.4.1"}) || tr.count() != 1 {
			t.Errorf("after %s: %+v, %d requests", later, got, tr.count())
		}
	}
	c3, _ := checker(f, tr)
	c3.Now = func() time.Time { return testNow.Add(ReleaseEvery) }
	tr.answer = tagged("v0.4.0")
	c3.Check(context.Background())
	if got := c3.Release(context.Background()); got != (Release{State: ReleaseCurrent}) || tr.count() != 2 {
		t.Errorf("after 24 hours: %+v, %d requests", got, tr.count())
	}
}

// TestCheckCreatesStateDir: the first check makes the state directory,
// 0700, where hardn has not yet.
func TestCheckCreatesStateDir(t *testing.T) {
	f := hostfake.New(host.Options{})
	f.AddDir("/var/lib", 0o755, 0, 0)
	c, _ := checker(f, &server{answer: tagged("v0.4.1")})
	c.Check(context.Background())
	if e, ok := f.Entry(revert.DefaultDir); !ok || e.Mode.Perm() != 0o700 {
		t.Errorf("state directory: %v %v", ok, e.Mode)
	}
	if got := c.Release(context.Background()); got.State != ReleaseNewer {
		t.Errorf("%+v", got)
	}
}

// TestCheckFails: no answer, or an answer whose tag does not pass, is a
// failed check: recorded, one log line with the reason, safe to write,
// nothing of the answer shown, and no retry for 24 hours.
func TestCheckFails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(context.Context) (string, error)
		log    string
	}{
		{"no route", failing("no answer from api.github.com: dial tcp: no route to host"), "no route to host"},
		{"status 403", failing("api.github.com answered status 403"), "status 403"},
		{"an error with an escape", failing("malformed HTTP response \"\x1b[31m\n\""), "malformed HTTP response"},
		{"no tag", tagged(""), "tag is not"},
		{"a tag that is not one", tagged("v99; rm"), "tag is not"},
		{"a tag with an escape", tagged("v99.0.0\x1b[31m"), "tag is not"},
		{"a pre-release tag", tagged("v99.0.0-rc1"), "tag is not"},
	} {
		f := fakeHost(`{"checked":"2026-11-01T09:00:00Z","answered":true,"tag":"v0.4.1"}`)
		tr := &server{answer: tc.answer}
		c, lines := checker(f, tr)
		c.Check(context.Background())
		if got := c.Release(context.Background()); got != (Release{State: ReleaseFailed}) {
			t.Errorf("%s: %+v, want failed", tc.name, got)
		}
		if len(*lines) != 1 || !strings.HasPrefix((*lines)[0], "Release check failed: ") || !strings.Contains((*lines)[0], tc.log) ||
			strings.Contains((*lines)[0], "v99") || strings.ContainsAny((*lines)[0], "\x1b\n") {
			t.Errorf("%s: log %q", tc.name, *lines)
		}
		if got, want := cacheOf(t, f), `{"checked":"2026-11-13T12:00:00Z","answered":false,"failed":true}`; got != want {
			t.Errorf("%s: cache %s", tc.name, got)
		}
		c2, _ := checker(f, tr)
		c2.Now = func() time.Time { return testNow.Add(ReleaseEvery - time.Second) }
		c2.Check(context.Background())
		if got := c2.Release(context.Background()); got != (Release{State: ReleaseFailed}) || tr.count() != 1 {
			t.Errorf("%s, a later start: %+v, %d requests", tc.name, got, tr.count())
		}
	}
}

// TestCheckNoLatest: a Checker given no way to ask, as the gallery's,
// reads the cache and asks nothing.
func TestCheckNoLatest(t *testing.T) {
	f := fakeHost("")
	c, _ := checker(f, &server{})
	c.Latest = nil
	c.Check(context.Background())
	if got := c.Release(context.Background()); got != (Release{State: ReleasePending}) || cacheOf(t, f) != "" {
		t.Errorf("%+v, cache %q", got, cacheOf(t, f))
	}
}

// TestCheckPendingAndCancel: while the request is out, Release answers at
// once, pending, and a second Check makes no second request; leaving the
// main menu cancels it, which is no failed check: the attempt stays
// recorded, so the next start within 24 hours asks nothing, and shows
// nothing, as no check has answered or failed.
func TestCheckPendingAndCancel(t *testing.T) {
	f := fakeHost("")
	tr := &server{answer: stalled}
	c, lines := checker(f, tr)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); c.Check(ctx) }()
	for tr.count() == 0 {
		time.Sleep(time.Millisecond)
	}
	if got := c.Release(context.Background()); got != (Release{State: ReleasePending}) {
		t.Errorf("while it runs: %+v", got)
	}
	c.Check(context.Background()) // not due: the attempt is recorded already
	if tr.count() != 1 {
		t.Errorf("%d requests", tr.count())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the check did not end when cancelled")
	}
	if len(*lines) != 0 {
		t.Errorf("log: %v", *lines)
	}
	if got, want := cacheOf(t, f), `{"checked":"2026-11-13T12:00:00Z","answered":false}`; got != want {
		t.Errorf("cache %s", got)
	}
	c2, _ := checker(f, tr)
	c2.Check(context.Background())
	if tr.count() != 1 {
		t.Errorf("a second start asked again: %d requests", tr.count())
	}
	if got := c.Release(context.Background()); got != (Release{State: ReleasePending}) {
		t.Errorf("cancelled: %+v, want pending", got)
	}
	if got := c2.Release(context.Background()); got != (Release{State: ReleasePending}) {
		t.Errorf("a second start: %+v, want pending", got)
	}
}

// TestCancelKeepsLastFailure: a check cancelled after one that failed is
// no new failure and no answer: the earlier failure, whose reason the log
// has, still shows, at this start and the next.
func TestCancelKeepsLastFailure(t *testing.T) {
	f := fakeHost(`{"checked":"2026-11-01T09:00:00Z","answered":false,"failed":true}`)
	tr := &server{answer: stalled}
	c, lines := checker(f, tr)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); c.Check(ctx) }()
	for tr.count() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if got, want := cacheOf(t, f), `{"checked":"2026-11-13T12:00:00Z","answered":false,"failed":true}`; got != want {
		t.Errorf("cache %s", got)
	}
	c2, _ := checker(f, tr)
	for _, ck := range []*Checker{c, c2} {
		if got := ck.Release(context.Background()); got != (Release{State: ReleaseFailed}) {
			t.Errorf("%+v, want failed", got)
		}
	}
	if len(*lines) != 0 || tr.count() != 1 {
		t.Errorf("log %v, %d requests", *lines, tr.count())
	}
}

// TestCheckKeepsLastAnswer: while a later request is out, and after one
// that is cancelled, the menu still shows what the last answer said.
func TestCheckKeepsLastAnswer(t *testing.T) {
	f := fakeHost(`{"checked":"2026-11-01T09:00:00Z","answered":true,"tag":"v0.4.1"}`)
	tr := &server{answer: stalled}
	c, _ := checker(f, tr)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); c.Check(ctx) }()
	for tr.count() == 0 {
		time.Sleep(time.Millisecond)
	}
	want := Release{ReleaseNewer, "v0.4.1"}
	if got := c.Release(context.Background()); got != want {
		t.Errorf("while it runs: %+v", got)
	}
	cancel()
	<-done
	if got := c.Release(context.Background()); got != want {
		t.Errorf("cancelled: %+v", got)
	}
}

// TestCheckCacheNotWritable: where the attempt cannot be recorded, the
// state directory another user's here, no request is made, so that hardn
// never asks more than once a day; the check failed, and the log says why.
func TestCheckCacheNotWritable(t *testing.T) {
	f := hostfake.New(host.Options{})
	f.AddDir(revert.DefaultDir, 0o777, 0, 0)
	tr := &server{answer: tagged("v0.4.1")}
	c, lines := checker(f, tr)
	c.Check(context.Background())
	if got := c.Release(context.Background()); got != (Release{State: ReleaseFailed}) || tr.count() != 0 {
		t.Errorf("%+v, %d requests", got, tr.count())
	}
	if len(*lines) != 1 || !strings.Contains((*lines)[0], revert.ErrUnsafe.Error()) {
		t.Errorf("log %q", *lines)
	}
	if _, ok := f.Entry(cachePath); ok {
		t.Error("a cache was written into a directory another user can change")
	}
}

// TestCheckReplacesPlantedCache: a planted cache is not used, and the next
// check replaces it with its own.
func TestCheckReplacesPlantedCache(t *testing.T) {
	f := fakeHost("")
	f.AddFile("/tmp/planted.json", []byte(`{"checked":"2026-11-13T09:00:00Z","answered":true,"tag":"v99.0.0"}`), 0o666, 1000, 1000)
	f.AddSymlink(cachePath, "/tmp/planted.json")
	tr := &server{answer: tagged("v0.4.0")}
	c, _ := checker(f, tr)
	c.Check(context.Background())
	if got := c.Release(context.Background()); got != (Release{State: ReleaseCurrent}) || tr.count() != 1 {
		t.Errorf("%+v, %d requests", got, tr.count())
	}
	if e, _ := f.Entry(cachePath); e.Target != "" || !strings.Contains(string(e.Data), `"tag":"v0.4.0"`) {
		t.Errorf("the cache: %+v", e)
	}
	if e, _ := f.Entry("/tmp/planted.json"); strings.Contains(string(e.Data), "v0.4.0") {
		t.Error("written through the planted link")
	}
}

func TestPrintVersionInfo(t *testing.T) {
	var b strings.Builder
	if err := NewService("0.4.0", "", "abc").PrintVersionInfo(&b); err != nil {
		t.Fatal(err)
	}
	want := "hardn - Linux hardening tool\nVersion:    0.4.0\nBuild Date: unknown\nGit Commit: abc\nRepo:       https://github.com/abbott/hardn\nLicense:    GNU AGPL v3, no warranty\n"
	if b.String() != want {
		t.Errorf("%q", b.String())
	}
	// A newer release is said on the Version line, and nowhere else.
	b.Reset()
	s := NewService("0.4.0", "", "abc")
	s.Newer = "v0.4.1"
	if err := s.PrintVersionInfo(&b); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != strings.Replace(want, "Version:    0.4.0\n", "Version:    0.4.0 (v0.4.1 available)\n", 1) {
		t.Errorf("%q", got)
	}
}
