package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The NTP Settings screen on simulated hosts (timesync.go): the control's
// reading through the screen's own mapping, each review the real screen up
// to its question.

// TestTimeSyncGolden pins each mock on the core set and under LANG=C.
func TestTimeSyncGolden(t *testing.T) { goldenListing(t, tsListing) }

// TestTimeSyncMocks holds each mock to what its note says: what it shows
// and what it does not; every note opens with the source label; and no line
// is cut.
func TestTimeSyncMocks(t *testing.T) {
	tests := []struct {
		mock      string
		has, lack []string
	}{
		{"default", []string{"+ 2.ntp.ubuntu.com", "Service              chrony\n", "Clock                In sync", "Synced to            2.ntp.ubuntu.com",
			"Server               ntp-bootstrap.ubuntu.com\n", "hardn.yml            No servers", "1) Set servers          No servers in hardn.yml"}, []string{"Unavailable", "Remove servers", "File"}},
		{"off", []string{"hardn.yml            192.0.2.10, 192.0.2.11", "1) Set servers          192.0.2.10, 192.0.2.11"}, []string{"by hardn", "Remove servers"}},
		{"hardn", []string{"+ 192.0.2.10", "Synced to            192.0.2.10", "Server               192.0.2.10 by hardn", "Server               192.0.2.11 by hardn",
			"File                 /etc/chrony/sources.d/hardn.sources", "1) Remove servers       Undo hardn's change"}, []string{"Set servers", "differs", "ubuntu.com"}},
		{"not-synced", []string{"Partial ~ not in sync", "[ ] Clock                Not in sync", "[ ] Synced to            None"}, []string{"In sync"}},
		{"stopped", []string{"Review ~ chrony not running", "[ ] Service              chrony not running", "1) Set servers          chrony is not running", "Unavailable"}, []string{"Synced to"}},
		{"no-service", []string{"Review ~ no time service", "[ ] Service              None", "1) Set servers          No time service", "Unavailable"}, []string{"Server  "}},
		{"foreign", []string{"Server               192.0.2.77 in site.sources", "1) Set servers          Set outside hardn", "Unavailable"}, []string{"Remove servers"}},
		{"dhcp", []string{"Server               192.0.2.1 from DHCP", "1) Set servers          192.0.2.10, 192.0.2.11"}, []string{"Unavailable"}},
		{"mismatch", []string{"Partial ~ not hardn.yml's servers", "[ ] hardn.yml            192.0.2.10, 192.0.2.12 differs", "1) Set servers          192.0.2.10, 192.0.2.12", "2) Remove servers"}, nil},
		{"timesyncd", []string{"Service              systemd-timesyncd", "Server               ntp.ubuntu.com built in", "Synced to            ntp.ubuntu.com"}, []string{"chrony"}},
		{"alpine", []string{"+ in sync", "Service              busybox ntpd", "Server               pool.ntp.org\n"}, []string{"Synced to"}},
		{"container", []string{"in a container", " -  Service              None", "In a container: the host keeps the time.", "1) Set servers          The host keeps the time", "Unavailable"}, []string{"hardn.yml  ", "Server  "}},
		{"not-read", []string{"NTP                  Unknown ~ can't read", "Server               Unknown", "1) Set servers          Can't read chrony's settings"}, []string{"Unavailable", "None"}},
		{"review", []string{"Checking the host\n", "# Set servers ###", "hardn's servers take the place of the host's own.", "Server               192.0.2.10 by hardn",
			"/etc/chrony/sources.d/hardn.sources", "+# Written by hardn (NTP Settings).", "+server 192.0.2.10 iburst\n", "/etc/chrony/sources.d/ubuntu-ntp-pools.sources",
			"-pool 1.ntp.ubuntu.com iburst maxsources 1 nts prefer", "+#hardn: pool ntp-bootstrap.ubuntu.com", "$ chronyc reload sources", "Set time servers? [y/N, 0 or q]:",
			"authenticated (NTS). hardn's are not."},
			[]string{"Lockout risk", "restart", "Server               1.ntp.ubuntu.com"}},
		{"review-rhel", []string{"/etc/chrony.conf\n", "-pool 2.rocky.pool.ntp.org iburst", "+#hardn: pool 2.rocky.pool.ntp.org iburst", "+# Added by hardn (NTP Settings): the 2 lines below.",
			"+server 192.0.2.11 iburst", " sourcedir /run/chrony-dhcp", "$ systemctl restart chronyd"}, []string{"sources.d", "Server               2.rocky"}},
		{"review-pve", []string{"/etc/chrony/sources.d/hardn.sources", "Switch off chrony's own servers", "+#hardn: pool 2.debian.pool.ntp.org iburst", "$ systemctl restart chrony\n",
			"On a cluster, set every node the same"}, []string{"reload sources", "NTS"}},
		{"review-timesyncd", []string{"/etc/systemd/timesyncd.conf.d/60-hardn.conf", "+[Time]", "+NTP=192.0.2.10 192.0.2.11", "$ systemctl restart systemd-timesyncd"}, []string{"ntp.ubuntu.com", "chrony"}},
		{"review-alpine", []string{"/etc/conf.d/ntpd", "-NTPD_OPTS=\"-N -p pool.ntp.org\"", "+# Set by hardn (NTP Settings), in place of: -p", "| pool.ntp.org", "+NTPD_OPTS=\"-N -p 192.0.2.10 -p 192.0.2.11\"",
			"$ rc-service ntpd restart"}, []string{"systemctl", "chrony", "Server               pool.ntp.org"}},
		{"review-remove", []string{"# Remove servers ###", "-server 192.0.2.10 iburst", "+pool 1.ntp.ubuntu.com iburst maxsources 1 nts prefer", "$ chronyc reload sources", "Remove hardn's time servers? [y/N, 0 or q]:",
			"The host's own servers are used again.", "Server               1.ntp.ubuntu.com"}, []string{"192.0.2.10 by hardn", "restart"}},
		{"refuse-no-servers", []string{"hardn.yml names no time servers. Add ntpServers, then try"}, []string{"[y/N"}},
		{"refuse-not-read", []string{"hardn can't read chrony's settings:", "site.sources:1: a server with no name"}, []string{"[y/N"}},
	}
	mocks := tsListing.mocks()
	if len(tests) != len(mocks) {
		t.Fatalf("%d mocks tested, %d mocks", len(tests), len(mocks))
	}
	for i, tt := range tests {
		m := mocks[i]
		if m.name != tt.mock {
			t.Fatalf("mock %d is %s, want %s", i, m.name, tt.mock)
		}
		drawn, err := m.draw(context.Background(), component.NewCtx(plainOff()))
		if err != nil {
			t.Fatalf("%s: %v", tt.mock, err)
		}
		out := strings.ReplaceAll(ansi.Strip(drawn)+"\n", "Checking the host...", "Checking the host")
		for _, want := range tt.has {
			if !strings.Contains(out, want) {
				t.Errorf("%s: no %q in:\n%s", tt.mock, want, out)
			}
		}
		for _, not := range append(tt.lack, "...", "…") {
			if strings.Contains(out, not) {
				t.Errorf("%s: %q in:\n%s", tt.mock, not, out)
			}
		}
		if note := tsListing.note(m); !strings.HasPrefix(note, tsLabel) {
			t.Errorf("%s: %q", m.name, note)
		}
	}
}

// TestTimeSyncUsage: the listing lists its mocks' names, and the listing
// and play refuse a mock or a host that is not one: status 2, nothing on
// stdout.
func TestTimeSyncUsage(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{tsPrefix, "-list"}, nil, offTerminal, &out, &errs); code != exitOK || errs.Len() > 0 {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}
	if got := strings.Fields(out.String()); len(got) != len(tsMocks()) || got[0] != "default" || got[len(got)-1] != "refuse-not-read" {
		t.Errorf("listed %v", got)
	}
	for _, args := range [][]string{{tsPrefix, "nope"}, {"play", tsPrefix + "/nope"}, {"play", "-fail", "1", tsPrefix + "/off"}} {
		var out, errs bytes.Buffer
		code := run(args, func() *prompt.Reader { return noInput() }, offTerminal, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errs.String())
		}
	}
}

// TestPlayTimeSync plays the screen on the engine: the main menu's 8, then
// 3, leads to it; Set servers applied on Ubuntu 26.04, the screen then Configured on
// hardn's first server with Remove servers; Remove servers applied, the
// pools back; set and removed on Rocky Linux, Alpine and Ubuntu 24.04; and
// a dry run stops at the plan. No command goes unanswered.
func TestPlayTimeSync(t *testing.T) {
	code, out, errs, unread := playGallery([]string{"8", "3", "1", "y", "1", "y", "0", "0", "0"}, "-profile", "plain")
	for _, want := range []string{"Info: " + tsMainHost, "Set time servers? [y/N, 0 or q]:", "+ 192.0.2.10", "Server               192.0.2.10 by hardn",
		"1) Remove servers       Undo hardn's change", "Remove hardn's time servers? [y/N, 0 or q]:", "$ chronyc reload sources", "Server               ntp-bootstrap.ubuntu.com\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("play, 8, 3: no %q", want)
		}
	}
	if code != exitOK || errs != "" || unread != 0 || strings.Contains(out, "does not model") {
		t.Errorf("play, 8, 3: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	for host, want := range map[string]string{"rocky": "$ systemctl restart chronyd", "alpine": "$ rc-service ntpd restart", "timesyncd": "$ systemctl restart systemd-timesyncd", "debian-chrony": "$ systemctl restart chrony\n"} {
		code, out, errs, unread = playGallery([]string{"1", "y", "1", "y", "0"}, "-profile", "plain", tsPrefix+"/"+host)
		if code != exitOK || errs != "" || unread != 0 || strings.Count(out, want) < 2 || !strings.Contains(out, "192.0.2.10 by hardn") || strings.Contains(out, "does not model") || strings.Contains(out, "Rolled back") {
			t.Errorf("play timesync/%s: exit %d, stderr %q, %d unread:\n%s", host, code, errs, unread, out)
		}
	}
	code, out, errs, unread = playGallery([]string{"1", "0"}, "-profile", "plain", "-sequence", "dry-run", tsPrefix+"/off")
	if code != exitOK || errs != "" || unread != 0 || strings.Contains(out, "[y/N") || !strings.Contains(out, "Write hardn's time servers") {
		t.Errorf("play -sequence dry-run timesync/off: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	code, out, _, unread = playGallery([]string{"13", "0", "0"}, "-profile", "plain", tsPrefix)
	if code != exitOK || unread != 0 || !strings.Contains(out, "Info: "+tsPrefix+"/not-read") {
		t.Errorf("play timesync: exit %d, %d unread:\n%s", code, unread, out)
	}
}
