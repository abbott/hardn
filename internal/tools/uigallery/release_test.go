package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The release check's mocks (release.go): the main menu's one line.

// TestReleaseCheckGolden pins each mock on the core set and under LANG=C.
func TestReleaseCheckGolden(t *testing.T) { goldenListing(t, releaseListing) }

// TestReleaseCheckMocks holds each mock to its note: newer and failed add one
// line under the main menu's first; current, pending and off draw the fixture's menu
// byte for byte. Every note opens with the source label.
func TestReleaseCheckMocks(t *testing.T) {
	c := component.NewCtx(plainOff())
	plain := screen.MainMenuFixture().Render(c)
	_, rest, _ := strings.Cut(plain, "\n")
	want := map[string]string{
		"newer":  "                           v0.4.1 available",
		"failed": "                           Release check failed",
	}
	mocks := releaseMocks()
	if len(mocks) != 5 {
		t.Fatalf("%d mocks", len(mocks))
	}
	for _, m := range mocks {
		got := m.view(context.Background()).Render(c)
		head, after, _ := strings.Cut(got, "\n")
		under, gotRest, _ := strings.Cut(after, "\n")
		switch line, changed := want[m.name]; {
		case changed && (head+"\n" != plain[:len(head)+1] || ansi.Strip(under) != line || gotRest != rest):
			t.Errorf("%s: second line %q, want %q; rest unchanged %t", m.name, under, line, gotRest == rest)
		case !changed && got != plain:
			t.Errorf("%s: not the fixture's main menu", m.name)
		}
	}
	for _, m := range releaseListing.mocks() {
		if note := releaseListing.note(m); !strings.HasPrefix(note, releaseLabel) {
			t.Errorf("%s: %q", m.name, note)
		}
	}
}

// TestReleaseCheckList: -list names the mocks, in order.
func TestReleaseCheckList(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{releasePrefix, "-list"}, nil, offTerminal, &out, &errs); code != exitOK || errs.Len() > 0 {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}
	if got := out.String(); got != "newer\ncurrent\npending\nfailed\noff\n" {
		t.Errorf("%q", got)
	}
}
