package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
)

// The listening ports' mocks (ports.go): the Ports and Listening Ports
// sections read from a simulated host by hardn's readers and drawn by its
// screens' mappings. Nothing here reads this machine or runs ss.

// TestPortsGolden pins each mock on the core set and under LANG=C.
func TestPortsGolden(t *testing.T) { goldenListing(t, portsListing) }

// TestPortsMocks holds each mock to what its note says: the rows it shows and
// the rows it does not; and every note opens with the source label.
func TestPortsMocks(t *testing.T) {
	tests := []struct {
		mock      string
		has, lack []string
	}{
		{"ufw", []string{"Ports ---", "22/tcp               Open sshd", "5353/udp             Some sources avahi-daemon", "5432/tcp             Local only postgres", "8080/tcp             Open nothing listening",
			"9090/tcp             Blocked prometheus", "9100/tcp             Blocked node_exporter"}, []string{"Not filtered"}},
		{"ufw-inactive", []string{"22/tcp               Not filtered sshd", "5432/tcp             Local only postgres", "Allow Saved"}, []string{"nothing listening", "Blocked"}},
		{"ufw-no-ss", []string{"Listening ports      ss not installed", "1) Install iproute2"}, []string{"Open"}},
		{"firewalld", []string{"firewalld ---", "Ports ---", "9090/tcp             Some sources prometheus", "8080/tcp             Open nothing listening", "5353/udp             Blocked avahi-daemon"}, []string{"Rules ---"}},
		{"firewalld-no-ss", []string{"Listening ports      ss not installed"}, []string{"Open"}},
		{"details", []string{"Listening Ports ---", "22/tcp               [::] sshd", "5432/tcp             127.0.0.1 postgres", "1) Refresh"}, []string{"Open", "Run hardn as root"}},
		{"details-not-root", []string{"22/tcp               [::]\n", "Run hardn as root to see every process."}, []string{"sshd"}},
		{"details-no-ss", []string{"Listening ports      ss not installed"}, []string{"22/tcp"}},
	}
	mocks := portsMocks()
	if len(tests) != len(mocks) {
		t.Fatalf("%d mocks tested, %d mocks", len(tests), len(mocks))
	}
	for i, tt := range tests {
		m := mocks[i]
		if m.name != tt.mock {
			t.Fatalf("mock %d is %s, want %s", i, m.name, tt.mock)
		}
		out := ansi.Strip(m.view().Render(component.NewCtx(plainOff())))
		for _, want := range tt.has {
			if !strings.Contains(out, want) {
				t.Errorf("%s: no %q in:\n%s", tt.mock, want, out)
			}
		}
		for _, not := range tt.lack {
			if strings.Contains(out, not) {
				t.Errorf("%s: %q in:\n%s", tt.mock, not, out)
			}
		}
	}
	for _, m := range portsListing.mocks() {
		if note := portsListing.note(m); !strings.HasPrefix(note, portsLabel) {
			t.Errorf("%s: %q", m.name, note)
		}
	}
}

// TestPortsUsage: the listing refuses a mock that is not one, with status 2
// and nothing on stdout; and play has no ports screen of its own: its
// Firewall and System Details screens have the sections.
func TestPortsUsage(t *testing.T) {
	for _, args := range [][]string{{"ports", "nope"}, {"play", "ports"}, {"play", "ports/ufw"}} {
		var out, errs bytes.Buffer
		if code := run(args, noInput, offTerminal, &out, &errs); code != exitUsage || out.Len() != 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errs.String())
		}
	}
}
