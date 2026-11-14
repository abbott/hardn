package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
)

// The Log Forwarding screen on the simulated hosts (logforward.go): the
// real screen, drawn from what the real control reads of each host, each
// review planned by the real engine. Nothing here reads this machine.

// TestLogForwardGolden pins each mock on the core set and under LANG=C.
func TestLogForwardGolden(t *testing.T) { goldenListing(t, lfListing) }

// TestLogForwardMocks holds each mock to what its note says: what it shows
// and what it does not; every note opens with the source label; and no line
// is cut.
func TestLogForwardMocks(t *testing.T) {
	tests := []struct {
		mock      string
		has, lack []string
	}{
		{"off", []string{"Not Configured not forwarding", "Server               None", "1) Set server           192.0.2.50:514 TCP"}, []string{"Stop forwarding", "Set by"}},
		{"no-server", []string{"hardn.yml            No server", "1) Set server           No server in hardn.yml"}, []string{"Unavailable"}},
		{"tcp", []string{"Configured 192.0.2.50:514 TCP", "Protocol             TCP not encrypted", "Connection           Open",
			"File                 /etc/rsyslog.d/60-hardn-forward.conf", "Set by               hardn", "1) Stop forwarding      Undo hardn's change"}, []string{"Set server"}},
		{"tcp-down", []string{"Partial server not reached", "Connection           None"}, []string{"Open"}},
		{"udp", []string{"Protocol             UDP not encrypted, losses unseen"}, []string{"Connection"}},
		{"tls", []string{"Server               logs.example.net:6514", "Protocol             TLS\n", "CA File              /etc/ssl/lab-ca.pem"}, []string{"not encrypted"}},
		{"foreign", []string{"Configured 192.0.2.50:514 TCP", "File                 /etc/rsyslog.d/50-remote.conf", "Set by               Not hardn", "1) Set server           Set outside hardn", "Unavailable"}, []string{"Stop forwarding"}},
		{"mismatch", []string{"Partial not hardn.yml's server", "hardn.yml            192.0.2.60:514 TCP differs", "1) Set server           192.0.2.60:514 TCP", "2) Stop forwarding"}, nil},
		{"journald", []string{"Logger               journald alone no syslog forwarder", "1) Set server           Installs rsyslog"}, []string{"Set by"}},
		{"alpine", []string{"Logger               busybox syslogd", "File                 /etc/conf.d/syslog", "UDP not encrypted, losses unseen"}, []string{"Connection"}},
		{"alpine-tcp", []string{"1) Set server           busybox syslogd is UDP only"}, []string{"Unavailable"}},
		{"not-read", []string{"Log Forwarding       Unknown", "Server               Unknown", "1) Set server           Can't read rsyslog's settings"}, []string{"None"}},
		{"review", []string{"# Set server ###", "/etc/rsyslog.d/60-hardn-forward.conf", "+*.* action(type=\"omfwd\" protocol=\"tcp\"", "+    target=\"192.0.2.50\" port=\"514\"",
			"$ rsyslogd -N1", "$ systemctl restart rsyslog", "$ logger -t hardn", "Forward logs to 192.0.2.50? [y/N, 0 or q]:"}, []string{"Lockout risk", "apt-get"}},
		{"review-tls", []string{"$ apt-get install -y rsyslog-gnutls", "StreamDriverMode=\"1\"", "StreamDriverPermittedPeers=\"logs.example.net\"",
			"StreamDriver.CAFile=\"/etc/ssl/lab-ca.pem\"", "Forward logs to logs.example.net?"}, []string{"Lockout risk"}},
		{"review-journald", []string{"rsyslog is installed to forward the journal.", "$ apt-get install -y rsyslog\n", "$ systemctl restart rsyslog"}, nil},
		{"review-alpine", []string{"/etc/conf.d/syslog", "-SYSLOGD_OPTS=\"-t\"", "+# Added by hardn (Log Forwarding): -R 192.0.2.50:514 -L", "+SYSLOGD_OPTS=\"-t -R 192.0.2.50:514 -L\"", "$ rc-service syslog restart",
			"busybox syslogd forwards over UDP alone"}, []string{"rsyslog", "systemctl"}},
		{"review-stop", []string{"# Stop forwarding ###", "Server               None", "-*.* action(type=\"omfwd\"", "$ systemctl restart rsyslog", "Stop forwarding logs? [y/N, 0 or q]:",
			"Logs stay on the host alone. rsyslog stays installed."}, []string{"logger -t"}},
		{"review-move", []string{"-    target=\"192.0.2.50\" port=\"514\"", "+    target=\"192.0.2.60\" port=\"514\"", "Forward logs to 192.0.2.60?"}, []string{"apt-get"}},
		{"refuse-no-server", []string{"hardn.yml names no log server. Add logForwarding's server,"}, []string{"[y/N"}},
		{"refuse-alpine-tcp", []string{"busybox syslogd forwards over UDP only. Set protocol: udp in"}, []string{"[y/N"}},
		{"refuse-not-read", []string{"hardn can't read rsyslog's settings:", "/etc/rsyslog.d/50-remote.conf:1: an action that does not", "Fix that, then try"}, []string{"[y/N"}},
		{"refuse-rsyslogd", []string{"rsyslogd can't read its settings: rsyslogd: error during", "Fix that, then try again."}, []string{"[y/N"}},
	}
	mocks := lfListing.mocks()
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
		if note := lfListing.note(m); !strings.HasPrefix(note, lfLabel) {
			t.Errorf("%s: %q", m.name, note)
		}
	}
}

// TestLogForwardUsage: the listing and play refuse a mock or a host that
// is not one, with status 2 and nothing on stdout.
func TestLogForwardUsage(t *testing.T) {
	for _, args := range [][]string{{"logforward", "nope"}, {"play", "logforward/nope"}, {"play", "-fail", "1", "logforward/off"}} {
		var out, errs bytes.Buffer
		if code := run(args, noInput, offTerminal, &out, &errs); code != exitUsage || out.Len() != 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errs.String())
		}
	}
}

// TestPlayLogForward plays the screen on the engine: the main menu's 6
// leads to it; Set server applied on Ubuntu, the screen then Configured
// with Stop forwarding; Stop forwarding applied; rsyslog installed on
// Debian 13; and a dry run stops at the plan. No command goes unanswered.
func TestPlayLogForward(t *testing.T) {
	code, out, errs, unread := playGallery([]string{"6", "1", "y", "1", "y", "0", "0"}, "-profile", "plain")
	for _, want := range []string{"Info: " + lfMainHost, "Forward logs to 192.0.2.50? [y/N, 0 or q]:", "Configured 192.0.2.50:514 TCP", "Connection",
		"1) Stop forwarding      Undo hardn's change", "Stop forwarding logs? [y/N, 0 or q]:", "$ systemctl restart rsyslog", "$ logger -t hardn"} {
		if !strings.Contains(out, want) {
			t.Errorf("play, 6: no %q", want)
		}
	}
	if code != exitOK || errs != "" || unread != 0 || strings.Contains(out, "does not model") {
		t.Errorf("play, 6: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	code, out, errs, unread = playGallery([]string{"1", "y", "0"}, "-profile", "plain", lfPrefix+"/journald")
	if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, "$ apt-get install -y rsyslog\n") || !strings.Contains(out, "Logger               rsyslog") || strings.Contains(out, "does not model") {
		t.Errorf("play logforward/journald: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	code, out, errs, unread = playGallery([]string{"1", "0"}, "-profile", "plain", "-sequence", "dry-run", lfPrefix+"/off")
	if code != exitOK || errs != "" || unread != 0 || strings.Contains(out, "[y/N") || !strings.Contains(out, "Write hardn's forwarding file") {
		t.Errorf("play -sequence dry-run logforward/off: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	code, out, _, unread = playGallery([]string{"12", "0", "0"}, "-profile", "plain", lfPrefix)
	if code != exitOK || unread != 0 || !strings.Contains(out, "Info: "+lfPrefix+"/not-read") {
		t.Errorf("play logforward: exit %d, %d unread:\n%s", code, unread, out)
	}
}
