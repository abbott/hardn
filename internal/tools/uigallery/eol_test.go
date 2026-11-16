package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The end-of-support notice's mocks (eol.go), drawn through hardn's own
// mapping (output.NoticeOf, whose rule and dates pkg/output's and
// pkg/host/detect's tests hold). Nothing here reads a host or the clock.

// TestEOLGolden pins each mock on the core set and under LANG=C.
func TestEOLGolden(t *testing.T) { goldenListing(t, eolListing) }

// TestEOLMocks holds each mock to its note: the release's support level,
// and the notice's lines, plain at 80 columns, between the status rows and
// the divider.
func TestEOLMocks(t *testing.T) {
	const cve = "   # Warning: CVE-2026-123158\n   # Upgrade openssh.\n   # Select option: 2) SSH\n\n"
	tests := []struct {
		mock    string
		support detect.Support
		notice  string
	}{
		{"soon", detect.BestEffort, "   # Warning: Alpine 3.22 support ends 2027-05-01\n   # Upgrade to Alpine 3.24."},
		{"soon-month", detect.BestEffort, "   # Warning: Ubuntu 22.04 support ends May 2027\n   # Upgrade to Ubuntu 26.04."},
		{"ended", detect.BestEffort, "   # Warning: Alpine 3.22 support ended 2027-05-01\n   # Upgrade to Alpine 3.24."},
		{"out-of-scope", detect.OutOfScope, "   # Warning: Alpine 3.21 support ended 2026-11-01\n   # Out of scope: hardn refuses every change.\n   # Upgrade to Alpine 3.24."},
		{"supported", detect.Supported, "   # Warning: Debian 12 support ends 2028-06-30\n   # Upgrade to Debian 13."},
		{"with-cve", detect.BestEffort, cve + "   # Warning: Alpine 3.22 support ends 2027-05-01\n   # Upgrade to Alpine 3.24."},
	}
	mocks := eolMocks()
	if len(tests) != len(mocks) {
		t.Fatalf("%d mocks tested, %d mocks", len(tests), len(mocks))
	}
	c := component.NewCtx(plainOff())
	for i, tt := range tests {
		m := mocks[i]
		if m.name != tt.mock || m.platform().Support != tt.support {
			t.Errorf("%s: mock %s, %v", tt.mock, m.name, m.platform().Support)
		}
		out := ansi.Strip(m.view().Render(c))
		if !strings.Contains(out, "Review\n\n"+tt.notice+"\n\n----") {
			t.Errorf("%s:\n%s\nwant\n%s", tt.mock, out, tt.notice)
		}
		if n := strings.Count(out, "Warning:"); n != strings.Count(tt.notice, "Warning:") {
			t.Errorf("%s: %d callouts", tt.mock, n)
		}
	}
}

// TestEOLUsage: the listing refuses a mock that is not one, and play has no
// eol screen: status 2, nothing on stdout.
func TestEOLUsage(t *testing.T) {
	for _, args := range [][]string{{"eol", "nope"}, {"play", "eol"}} {
		var out, errs bytes.Buffer
		code := run(args, func() *prompt.Reader { return noInput() }, offTerminal, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errs.String())
		}
	}
}
