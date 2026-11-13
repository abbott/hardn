package version

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The release check (proposal P-15; docs/engine/screen-release-check.md).
// It replaces origin/main's checker, which asked at every start and
// trusted a cache in /tmp that anyone could plant (V26).
const (
	// ReleaseEvery is the least time between two requests, counted from
	// the last attempt, answered or not.
	ReleaseEvery = 24 * time.Hour
	// ReleaseCacheName is the cache, in hardn's state directory
	// (revert.DefaultDir), and ReleaseMaxCache the largest one read.
	ReleaseCacheName = "release.json"
	ReleaseMaxCache  = 4 << 10
)

// Tag is a release's version, <major>.<minor>.<patch>.
type Tag [3]int

// ParseTag reads s, "v<major>.<minor>.<patch>", each one to six digits and
// nothing else, as install.sh's check_version asks of a release's tag.
// Anything else is not a tag.
func ParseTag(s string) (Tag, bool) {
	rest, ok := strings.CutPrefix(s, "v")
	if !ok {
		return Tag{}, false
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 3 {
		return Tag{}, false
	}
	var t Tag
	for i, p := range parts {
		if len(p) == 0 || len(p) > 6 || strings.Trim(p, "0123456789") != "" {
			return Tag{}, false
		}
		t[i], _ = strconv.Atoi(p)
	}
	return t, true
}

// newer reports whether t is a later version than u.
func (t Tag) newer(u Tag) bool {
	for i := range t {
		if t[i] != u[i] {
			return t[i] > u[i]
		}
	}
	return false
}

// ReleaseState is what the check knows.
type ReleaseState int

// The states.
const (
	// ReleaseOff: no check is made: checkReleases is off, or this build
	// has no release version.
	ReleaseOff ReleaseState = iota
	// ReleaseCurrent: no release is newer than this build.
	ReleaseCurrent
	// ReleasePending: the check is on and has not answered yet.
	ReleasePending
	// ReleaseNewer: Release.Latest is a newer release.
	ReleaseNewer
	// ReleaseFailed: the last check got no answer.
	ReleaseFailed
)

// Release is what the check knows: its state and, for ReleaseNewer, the
// newer release's tag, which passed ParseTag.
type Release struct {
	State  ReleaseState
	Latest string
}

// cache is release.json: the last attempt.
type cache struct {
	// Checked is when the last request was made.
	Checked time.Time `json:"checked"`
	// Answered: it got a tag. Tag is then that tag, and "" otherwise.
	Answered bool   `json:"answered"`
	Tag      string `json:"tag,omitempty"`
	// Failed: it ended with no answer. Neither Answered nor Failed is an
	// attempt that never ended (hardn left while it was out): no check
	// has answered or failed yet.
	Failed bool `json:"failed,omitempty"`
}

// errTag is an answer whose tag is not a release's version.
var errTag = errors.New("the answer's tag is not v<major>.<minor>.<patch>")

// decodeCache reads data strictly: one JSON object with no unknown field
// and nothing after it, a time that is set and not after now, and a tag
// that passes ParseTag exactly when the attempt answered, and not both
// answered and failed.
func decodeCache(data []byte, now time.Time) (cache, error) {
	var c cache
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return cache{}, fmt.Errorf("not valid: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return cache{}, errors.New("not valid: more than one value")
	}
	_, tagged := ParseTag(c.Tag)
	switch {
	case c.Checked.IsZero():
		return cache{}, errors.New("not valid: no time")
	case c.Checked.After(now):
		return cache{}, errors.New("not valid: its time is in the future")
	case c.Answered != tagged || (!c.Answered && c.Tag != ""):
		return cache{}, errors.New("not valid: its tag")
	case c.Answered && c.Failed:
		return cache{}, errors.New("not valid: answered and failed")
	}
	return c, nil
}

// Checker is the release check on one host. Its zero value checks nothing.
// It is safe for concurrent use: the main menu asks Release on each pass
// while Check runs beside it.
type Checker struct {
	// On is hardn.yml's checkReleases. Off, nothing is requested and the
	// cache is neither read nor written.
	On bool
	// Version is this build's version, "0.9.0" or "v0.9.0". A build with
	// any other (a development build) never checks.
	Version string
	// System is the host the cache is on.
	System host.System
	// Dir is hardn's state directory; "" is revert.DefaultDir.
	Dir string
	// Latest makes the request and returns the latest release's tag:
	// pkg/version/github's Client.Latest, which alone reaches the
	// network. What it returns is untrusted and passes ParseTag or is a
	// failed check. Nil, Check asks nothing. Tests give a fake: no test
	// makes a network request.
	Latest func(ctx context.Context) (tag string, err error)
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Log, where set, is given one line for a check that failed, safe to
	// write (logview.Safe).
	Log func(line string)

	mu      sync.Mutex
	running bool   // a request is in flight
	last    *cache // this process's check, once it has ended
}

// own is this build's version, and whether the check runs at all.
func (c *Checker) own() (Tag, bool) {
	if c == nil || !c.On || c.System == nil {
		return Tag{}, false
	}
	return ParseTag("v" + strings.TrimPrefix(c.Version, "v"))
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) opts() revert.Options { return revert.Options{Dir: c.Dir} }

// read is the cache, where it passes every check: the state directory's
// and the file's (revert.ReadStateFile), then its content (decodeCache).
func (c *Checker) read(ctx context.Context) (cache, error) {
	data, err := revert.ReadStateFile(ctx, c.System, c.opts(), ReleaseCacheName, ReleaseMaxCache)
	if err != nil {
		return cache{}, err
	}
	return decodeCache(data, c.now())
}

func (c *Checker) write(ctx context.Context, v cache) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return revert.WriteStateFile(ctx, c.System, c.opts(), ReleaseCacheName, append(data, '\n'))
}

// Release is what the check knows now, without a request and without
// waiting for one: this process's check where it has ended, else the
// cache. A cache that is missing or fails a check counts as no attempt
// (ReleasePending), and nothing from it is returned; so does an attempt
// that never ended. Only a tag that passed ParseTag is ever in Latest.
func (c *Checker) Release(ctx context.Context) Release {
	own, ok := c.own()
	if !ok {
		return Release{}
	}
	c.mu.Lock()
	last, running := c.last, c.running
	c.mu.Unlock()
	if last == nil {
		v, err := c.read(ctx)
		if err != nil {
			return Release{State: ReleasePending}
		}
		last = &v
	}
	switch tag, _ := ParseTag(last.Tag); {
	case last.Answered && tag.newer(own):
		return Release{State: ReleaseNewer, Latest: last.Tag}
	case last.Answered:
		return Release{State: ReleaseCurrent}
	case running || !last.Failed:
		return Release{State: ReleasePending}
	}
	return Release{State: ReleaseFailed}
}

// Check makes the request (Latest) where one is due: the check on, and no
// attempt in the cache within ReleaseEvery. It is meant for one goroutine
// beside the main menu, once a start; a second call while one runs returns
// at once. It returns when the request has ended, within its timeout, or
// as soon as ctx is done.
//
// The attempt is written to the cache before the request, so that at most
// one request is made in ReleaseEvery whatever ends hardn; where the cache
// cannot be written, no request is made. That record keeps what the last
// check that ended said, an answer or a failure. A request that ends with
// ctx leaves it alone: a cancel is no failed check. Any other failure is
// recorded, and said in one line to Log.
func (c *Checker) Check(ctx context.Context) {
	if _, ok := c.own(); !ok || c.Latest == nil {
		return
	}
	now := c.now()
	prev, err := c.read(ctx)
	if err == nil && now.Sub(prev.Checked) < ReleaseEvery {
		return
	}
	if err != nil {
		prev = cache{}
	}
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()

	result := cache{Checked: now}
	attempt := cache{Checked: now, Answered: prev.Answered, Tag: prev.Tag, Failed: prev.Failed}
	if err = c.write(ctx, attempt); err == nil {
		var tag string
		if tag, err = c.Latest(ctx); err == nil {
			if _, ok := ParseTag(tag); ok {
				result.Answered, result.Tag = true, tag
			} else {
				err = errTag
			}
		}
	}
	result.Failed = !result.Answered
	cancelled := ctx.Err() != nil
	if !cancelled {
		if werr := c.write(ctx, result); werr != nil && err == nil {
			c.logf("the answer was not saved: %v", werr)
		}
		if err != nil {
			c.logf("%v", err)
		}
	}
	c.mu.Lock()
	c.running = false
	if !cancelled {
		c.last = &result
	}
	c.mu.Unlock()
}

func (c *Checker) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log(logview.Safe("Release check failed: " + fmt.Sprintf(format, args...)))
	}
}
