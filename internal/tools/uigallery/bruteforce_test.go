package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// Brute-force limiting's mocks (bruteforce.go), listed: hardn's screens on
// the simulated hosts. Nothing here reads this machine.

// TestBruteForceGolden pins each mock on the core set and under LANG=C.
func TestBruteForceGolden(t *testing.T) { goldenListing(t, bfListing) }

// TestBruteForceMocks holds each mock to what its note says: the row's words
// on the SSH Login screen, item 5 only where ufw allows SSH with no limit, the
// review up to its question, and firewalld's screen with nothing added.
func TestBruteForceMocks(t *testing.T) {
	tests := []struct {
		mock     string
		has, not []string
	}{
		{"ssh-on", []string{"Brute-Force Limit    On sshd's default"}, []string{"[ ] Brute-Force"}},
		{"ssh-off", []string{"[ ] Brute-Force Limit    Off by 20-site.conf"}, nil},
		{"ssh-unavailable", []string{"Brute-Force Limit    Not available needs OpenSSH 9.8"}, []string{"MaxAuthTries 6"}},
		{"firewall-offer", []string{"5) Limit SSH            Block repeated connections", "1) Disable firewall", "[0-3, 5 or q]"}, []string{"Rate limited"}},
		{"firewall-review", []string{"# Limit SSH #", "6 or more SSH connections in", "[+] 22/tcp (v6)          Limit", "$ ufw limit 22/tcp", "Limit SSH? [y/N, 0 or q]:"}, []string{"Cancelled", "Successful"}},
		{"firewall-limited", []string{"[+] 0.0.0.0:22           Rate limited", "[0-3 or q]"}, []string{"Limit SSH"}},
		{"firewalld", []string{"1) Disable firewalld", "[0-1 or q]"}, []string{"Limit", "limit"}},
	}
	mocks := bfMocks()
	if len(tests) != len(mocks) {
		t.Fatalf("%d mocks tested, %d mocks", len(tests), len(mocks))
	}
	c := component.NewCtx(term.Fixed(80, colorprofile.NoTTY, false, true))
	for i, tt := range tests {
		m := mocks[i]
		out, err := m.draw(context.Background(), c)
		if err != nil || m.name != tt.mock {
			t.Fatalf("%s: mock %s, %v", tt.mock, m.name, err)
		}
		out = ansi.Strip(out)
		for _, w := range tt.has {
			if !strings.Contains(out, w) {
				t.Errorf("%s: no %q in\n%s", tt.mock, w, out)
			}
		}
		for _, w := range tt.not {
			if strings.Contains(out, w) {
				t.Errorf("%s: %q in\n%s", tt.mock, w, out)
			}
		}
		if note := bfListing.note(m); !strings.HasPrefix(note, bfLabel) {
			t.Errorf("%s: %q", m.name, note)
		}
	}
}

// TestBruteForceListed: the mode lists its mocks' names, and refuses one that
// is not a mock.
func TestBruteForceListed(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{bfPrefix, "-list"}, nil, offTerminal, &out, &errs); code != exitOK || errs.Len() > 0 {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}
	if got := strings.Fields(out.String()); len(got) != len(bfMocks()) || got[0] != "ssh-on" || got[len(got)-1] != "firewalld" {
		t.Errorf("listed %v", got)
	}
	if code := run([]string{bfPrefix, "fail2ban"}, nil, offTerminal, &out, &errs); code != exitUsage {
		t.Errorf("exit %d for a mock that is not one", code)
	}
}
