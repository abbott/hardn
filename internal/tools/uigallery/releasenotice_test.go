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

// The release notice's mocks (releasenotice.go).

// TestReleaseNoticeGolden pins each mock on the core set and under LANG=C.
func TestReleaseNoticeGolden(t *testing.T) { goldenListing(t, noticeListing) }

// TestReleaseNoticeMenu: under the link, the menu is the fixture's with one
// line added after its first, at the link's column; below 64 columns, after
// its second, at column 0.
func TestReleaseNoticeMenu(t *testing.T) {
	for _, tc := range []struct {
		width int
		after int
		want  string
	}{
		{80, 1, "                           v0.4.1 available"},
		{60, 2, "v0.4.1 available"},
	} {
		env := plainOff()
		env.Width = tc.width
		c := component.NewCtx(env)
		plain := strings.Split(screen.MainMenuFixture().Render(c), "\n")
		got := strings.Split(noticeMenu(context.Background(), noticeNewerCache).Render(c), "\n")
		want := append(append(append([]string{}, plain[:tc.after]...), tc.want), plain[tc.after:]...)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("width %d: line %d is %q, %d lines for the fixture's %d", tc.width, tc.after+1, got[tc.after], len(got), len(plain))
		}
	}
}

// TestReleaseNoticeVersion: --version's mock says the newer release on its
// Version line for root with one in the cache, and nothing otherwise.
func TestReleaseNoticeVersion(t *testing.T) {
	c := component.NewCtx(plainOff())
	draw := func(name string) string {
		for _, m := range noticeMocks() {
			if m.name == name {
				out, err := m.draw(context.Background(), c)
				if err != nil {
					t.Fatal(err)
				}
				return ansi.Strip(out)
			}
		}
		t.Fatalf("no mock %s", name)
		return ""
	}
	none := draw("version-none")
	if strings.Contains(none, "available") || !strings.Contains(none, "License:    GNU AGPL v3, no warranty") {
		t.Errorf("version-none: %q", none)
	}
	if got := draw("version-not-root"); got != none {
		t.Errorf("version-not-root: %q", got)
	}
	want := strings.Replace(none, "Version:    0.4.0\n", "Version:    0.4.0 (v0.4.1 available)\n", 1)
	if got := draw("version-newer"); got == none || got != want {
		t.Errorf("version-newer: %q", got)
	}
	for _, name := range []string{"verify", "install"} {
		if got := draw(name); !strings.Contains(got, "\n\nhardn v0.4.1 available (https://github.com/abbott/hardn)") {
			t.Errorf("%s: %q", name, got)
		}
	}
}

// TestReleaseNoticeList: -list names the mocks, in order, and every note
// opens with the source label.
func TestReleaseNoticeList(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{noticePrefix, "-list"}, nil, offTerminal, &out, &errs); code != exitOK || errs.Len() > 0 {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}
	if got := out.String(); got != "menu-newer\nmenu-failed\nheader\nversion-newer\nversion-none\nversion-not-root\nverify\ninstall\n" {
		t.Errorf("%q", got)
	}
	for _, m := range noticeListing.mocks() {
		if note := noticeListing.note(m); !strings.HasPrefix(note, releaseLabel) {
			t.Errorf("%s: %q", m.name, note)
		}
	}
}
