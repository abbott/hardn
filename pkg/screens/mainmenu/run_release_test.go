package mainmenu

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/abbott/hardn/pkg/ui/screen"
)

// The release check on the main menu (docs/engine/screen-release-check.md):
// its one line, and its request beside the menu. The hooks are fakes:
// nothing here makes a request.

// firstLines are the first lines of each drawing of the main menu in out.
func firstLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "hardn  v0.4.0") {
			lines = append(lines, strings.TrimRight(l, " "))
		}
	}
	return lines
}

// TestReleaseLine: what the check knows is asked on every pass and drawn
// in the header, in the link's place; with no hook the header is as ever.
func TestReleaseLine(t *testing.T) {
	const link = "hardn  v0.4.0              https://github.com/abbott/hardn"
	m := liveMenu(t, plainEnv(), fixtureStatus(new(int)), "q")
	if out, _, _ := session(t, func() { show(m) }); len(firstLines(out)) != 1 || firstLines(out)[0] != link {
		t.Errorf("no hook: %q", firstLines(out))
	}

	// Pending on the first pass, answered by the second.
	asked := 0
	states := []screen.Release{{State: screen.ReleasePending}, {State: screen.ReleaseNewer, Latest: "v0.4.1"}, {State: screen.ReleaseFailed}}
	m = liveMenu(t, plainEnv(), fixtureStatus(new(int)), "10", "10", "q")
	setLogs(m, func() error { return nil })
	m.Release = func(ctx context.Context) screen.Release {
		if ctx.Err() != nil {
			t.Error("the hook's context is done")
		}
		asked++
		return states[asked-1]
	}
	out, _, ended := session(t, func() { show(m) })
	want := []string{link, "hardn  v0.4.0              v0.4.1 available", "hardn  v0.4.0              Release check failed"}
	if got := firstLines(out); ended || asked != 3 || strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("ended %v, asked %d times; first lines %q, want %q", ended, asked, got, want)
	}
}

// TestReleaseCheckBesideMenu: the request runs once a start, beside the
// main menu: a check that never answers delays no drawing and no key, and
// leaving the main menu cancels it.
func TestReleaseCheckBesideMenu(t *testing.T) {
	var started, cancelled atomic.Int32
	m := liveMenu(t, plainEnv(), fixtureStatus(new(int)), "10", "q")
	setLogs(m, func() error { return nil })
	m.CheckRelease = func(ctx context.Context) {
		started.Add(1)
		<-ctx.Done() // a server that never answers
		cancelled.Add(1)
	}
	out, _, ended := session(t, func() { show(m) })
	if ended || len(firstLines(out)) != 2 {
		t.Errorf("ended %v; the main menu was drawn %d times, want 2", ended, len(firstLines(out)))
	}
	if started.Load() != 1 || cancelled.Load() != 1 {
		t.Errorf("the check started %d times and was cancelled %d times, want once each", started.Load(), cancelled.Load())
	}
}

// TestReleaseCheckEndsWithInput: the end of the input, as Ctrl-C, cancels
// the check too.
func TestReleaseCheckEndsWithInput(t *testing.T) {
	var cancelled atomic.Bool
	m := liveMenu(t, plainEnv(), fixtureStatus(new(int)))
	m.CheckRelease = func(ctx context.Context) { <-ctx.Done(); cancelled.Store(true) }
	if _, _, ended := session(t, func() { show(m) }); !ended || !cancelled.Load() {
		t.Errorf("ended %v, cancelled %v", ended, cancelled.Load())
	}
}
