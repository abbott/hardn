package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
)

// The Syslog Settings screen on the simulated hosts (logforward.go): the
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
		{"off", []string{"Not Configured not forwarding", "Server               None", "1) Set servers          192.0.2.50:514 TCP"}, []string{"Stop forwarding", "Set by"}},
		{"no-server", []string{"hardn.yml            No server", "1) Set servers          No server in hardn.yml"}, []string{"Unavailable"}},
		{"tcp", []string{"+ 192.0.2.50:514 TCP", "[+] Server               192.0.2.50:514 TCP Open",
			"File                 /etc/rsyslog.d/60-hardn-forward.conf", "Set by               hardn", "1) Stop forwarding      Undo hardn's change"}, []string{"Set servers", "Protocol", "Connection", "not encrypted"}},
		{"tcp-down", []string{"Partial server unreachable", "[ ] Server               192.0.2.50:514 TCP unreachable"}, []string{"Open"}},
		{"udp", []string{"[ ] Server               192.0.2.50:514 UDP losses unseen"}, []string{"Connection", "not encrypted"}},
		{"tls", []string{"[+] Server               logs.example.net:6514 TLS Open\n   -  CA File              /etc/ssl/lab-ca.pem"}, []string{"not encrypted"}},
		// The design's screen (design-syslog-servers.md §2.4).
		{"several", []string{"[+] Syslog               + 3 servers", "[+] Server               logs.example.net:6514 TLS Open\n   -  CA File              /etc/ssl/lab-ca.pem\n" +
			"  [+] Server               192.0.2.51:514 TCP Open\n  [ ] Server               192.0.2.52:514 UDP losses unseen\n", "hardn.yml            3 servers\n", "1) Stop forwarding"}, []string{"Set servers", "differs"}},
		{"several-down", []string{"[ ] Syslog               Partial 1 of 3 unreachable", "[+] Server               logs.example.net:6514 TLS Open", "[ ] Server               192.0.2.51:514 TCP unreachable",
			"[ ] Server               192.0.2.52:514 UDP losses unseen", "hardn.yml            3 servers\n"}, []string{"Set servers"}},
		{"several-differ", []string{"Partial not hardn.yml's servers", "hardn.yml            3 servers differs", "1) Set servers          3 servers", "2) Stop forwarding"}, nil},
		{"long-name", []string{"[+] Server\n      syslog-collector.prod.example.com:6514 TLS Open\n   -  CA File              /etc/ssl/lab-ca.pem",
			"hardn.yml\n      syslog-collector.prod.example.com:6514 TLS\n"}, nil},
		{"names", []string{"Partial 1 of 2 unreachable", "logs-a.example.net:514 TCP 1 of 2 open", "logs-b.example.net:514 TCP 1 of 2 open"}, []string{"Open"}},
		{"foreign", []string{"+ 192.0.2.50:514 TCP", "File                 /etc/rsyslog.d/50-remote.conf", "Set by               Not hardn", "1) Set servers          Set outside hardn", "Unavailable"}, []string{"Stop forwarding"}},
		{"mismatch", []string{"Partial not hardn.yml's servers", "hardn.yml            192.0.2.60:514 TCP differs", "1) Set servers          192.0.2.60:514 TCP", "2) Stop forwarding"}, nil},
		{"journald", []string{"Logger               journald alone no syslog forwarder", "1) Set servers          Installs rsyslog"}, []string{"Set by"}},
		{"alpine", []string{"Logger               busybox syslogd", "File                 /etc/conf.d/syslog", "192.0.2.50:514 UDP losses unseen"}, []string{"Connection"}},
		{"alpine-several", []string{"+ 2 servers", "[ ] Server               192.0.2.51:514 UDP losses unseen\n  [ ] Server               192.0.2.52:514 UDP losses unseen", "File                 /etc/conf.d/syslog",
			"1) Stop forwarding"}, []string{"Set servers"}},
		{"alpine-tcp", []string{"1) Set servers          busybox syslogd is UDP only"}, []string{"Unavailable"}},
		{"not-read", []string{"Syslog               Unknown", "Server               Unknown", "1) Set servers          Can't read rsyslog's settings"}, []string{"None"}},
		{"review", []string{"# Set servers ###", "/etc/rsyslog.d/60-hardn-forward.conf", "+*.* action(type=\"omfwd\" protocol=\"tcp\"", "+    target=\"192.0.2.50\" port=\"514\"",
			"+    queue.type=\"linkedList\" queue.timeoutEnqueue=\"0\"\n      +    action.resumeRetryCount=\"-1\")",
			"$ rsyslogd -N1", "$ systemctl restart rsyslog", "$ logger -t hardn", "Forward logs to 192.0.2.50? [y/N, 0 or q]:"}, []string{"Lockout risk", "apt-get"}},
		{"review-tls", []string{"$ apt-get install -y rsyslog-gnutls", "StreamDriverMode=\"1\"", "StreamDriverPermittedPeers=\"logs.example.net\"",
			"StreamDriver.CAFile=\"/etc/ssl/lab-ca.pem\"", "Forward logs to logs.example.net?"}, []string{"Lockout risk"}},
		{"review-several", []string{"Every message the host logs goes to each server", "-  Server               logs.example.net:6514 TLS\n   -  CA File              /etc/ssl/lab-ca.pem\n   -  Server               192.0.2.51:514 TCP\n" +
			"   -  Server               192.0.2.52:514 UDP losses unseen", "@@ -0,0 +1,17 @@", "+*.* action(type=\"omfwd\" protocol=\"udp\"", "Forward logs to 3 servers? [y/N, 0 or q]:"}, []string{"apt-get", "Lockout risk"}},
		// The design's review (§2.5): the third server is the diff.
		{"review-add", []string{"-  Server               192.0.2.53:514 TCP", "      +*.* action(type=\"omfwd\" protocol=\"tcp\"\n      +    target=\"192.0.2.53\" port=\"514\"\n" +
			"      +    queue.type=\"linkedList\" queue.timeoutEnqueue=\"0\"\n      +    action.resumeRetryCount=\"-1\")\n      $ rsyslogd -N1", "Forward logs to 3 servers?"},
			[]string{"-*.*", "+    target=\"192.0.2.51\"", "-    target"}},
		{"review-journald", []string{"rsyslog is installed to forward the journal.", "$ apt-get install -y rsyslog\n", "$ systemctl restart rsyslog"}, nil},
		{"review-alpine", []string{"/etc/conf.d/syslog", "-SYSLOGD_OPTS=\"-t\"", "+# Added by hardn (Syslog Settings): -R 192.0.2.50:514 -L", "+SYSLOGD_OPTS=\"-t -R 192.0.2.50:514 -L\"", "$ rc-service syslog restart",
			"busybox syslogd forwards over UDP alone"}, []string{"rsyslog", "systemctl"}},
		{"review-alpine-several", []string{"Set syslogd's servers", "+SYSLOGD_OPTS=\"-t -R 192.0.2.51:514 -R 192.0.2.52:514 -L\"", "+# Added by hardn (Syslog Settings): -R 192.0.2.51:514 -R", "Forward logs to 2 servers?"}, []string{"rsyslog"}},
		{"review-stop", []string{"# Stop forwarding ###", "Server               None", "-*.* action(type=\"omfwd\"", "$ systemctl restart rsyslog", "Stop forwarding logs? [y/N, 0 or q]:",
			"Logs stay on the host alone. rsyslog stays installed."}, []string{"logger -t"}},
		{"review-stop-several", []string{"# Stop forwarding ###", "Server               None", "@@ -1,17 +0,0 @@", "-    target=\"192.0.2.51\" port=\"514\"", "-    target=\"192.0.2.52\" port=\"514\"", "Stop forwarding logs?"}, []string{"logger -t"}},
		{"review-move", []string{"-    target=\"192.0.2.50\" port=\"514\"", "+    target=\"192.0.2.60\" port=\"514\"", "Forward logs to 192.0.2.60?"}, []string{"apt-get"}},
		{"refuse-no-server", []string{"hardn.yml names no syslog server. Add syslogServers, then try"}, []string{"[y/N"}},
		{"refuse-twice", []string{"hardn.yml's syslogServers is not valid: server 2: the same", "address and port as server 1. Fix that, then try again."}, []string{"[y/N"}},
		{"refuse-alpine-tcp", []string{"busybox syslogd forwards over UDP only. Set protocol: udp for", "192.0.2.50 in hardn.yml, then try again."}, []string{"[y/N"}},
		// The design's refusal (§2.3).
		{"refuse-alpine-mixed", []string{"# busybox syslogd forwards over UDP only. Set protocol: udp for\n# logs.example.net in hardn.yml, then try again.", "1) Set servers          busybox syslogd is UDP only"}, []string{"[y/N", "-R"}},
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
		if tt.mock == "long-name" { // the status row has 37 cells, and cuts the name
			out = strings.Replace(out, "+ syslog-collector.prod.example.co...", "+", 1)
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

// TestPlayLogForward plays the screen on the engine: the main menu's 8,
// then 4, leads to it; Set servers applied on Ubuntu, the screen then Configured
// with Stop forwarding; Stop forwarding applied; rsyslog installed on
// Debian 13; and a dry run stops at the plan. No command goes unanswered.
func TestPlayLogForward(t *testing.T) {
	code, out, errs, unread := playGallery([]string{"8", "4", "1", "y", "1", "y", "0", "0", "0"}, "-profile", "plain")
	for _, want := range []string{"Info: " + lfMainHost, "Forward logs to 192.0.2.50? [y/N, 0 or q]:", "+ 192.0.2.50:514 TCP", "192.0.2.50:514 TCP Open",
		"1) Stop forwarding      Undo hardn's change", "Stop forwarding logs? [y/N, 0 or q]:", "$ systemctl restart rsyslog", "$ logger -t hardn"} {
		if !strings.Contains(out, want) {
			t.Errorf("play, 8, 4: no %q", want)
		}
	}
	if code != exitOK || errs != "" || unread != 0 || strings.Contains(out, "does not model") {
		t.Errorf("play, 8, 4: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	code, out, errs, unread = playGallery([]string{"1", "y", "0"}, "-profile", "plain", lfPrefix+"/journald")
	if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, "$ apt-get install -y rsyslog\n") || !strings.Contains(out, "Logger               rsyslog") || strings.Contains(out, "does not model") {
		t.Errorf("play logforward/journald: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	code, out, errs, unread = playGallery([]string{"1", "0"}, "-profile", "plain", "-sequence", "dry-run", lfPrefix+"/off")
	if code != exitOK || errs != "" || unread != 0 || strings.Contains(out, "[y/N") || !strings.Contains(out, "Write hardn's forwarding file") {
		t.Errorf("play -sequence dry-run logforward/off: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	// Three servers set, each reached; then one fewer, the same one change.
	code, out, errs, unread = playGallery([]string{"1", "y", "0"}, "-profile", "plain", lfPrefix+"/several-off")
	if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, "Forward logs to 3 servers? [y/N, 0 or q]:") || !strings.Contains(out, "+ 3 servers") ||
		strings.Count(out, "$ systemctl restart rsyslog") != 2 || strings.Contains(out, "does not model") { // the plan's, and the apply's
		t.Errorf("play logforward/several-off: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	code, out, errs, unread = playGallery([]string{"1", "y", "0"}, "-profile", "plain", lfPrefix+"/several-differ")
	if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, "+ 3 servers") || !strings.Contains(out, "192.0.2.53:514 TCP Open") {
		t.Errorf("play logforward/several-differ: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	code, out, errs, unread = playGallery([]string{"1", "y", "1", "y", "0"}, "-profile", "plain", lfPrefix+"/alpine-several-off")
	if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, "+ 2 servers") || !strings.Contains(out, "Remove syslogd's servers") || !strings.Contains(out, "+SYSLOGD_OPTS=\"-t\"") {
		t.Errorf("play logforward/alpine-several-off: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	last := len(lfHosts())
	code, out, _, unread = playGallery([]string{fmt.Sprint(last), "0", "0"}, "-profile", "plain", lfPrefix)
	if code != exitOK || unread != 0 || !strings.Contains(out, "Info: "+lfPrefix+"/not-read") {
		t.Errorf("play logforward: exit %d, %d unread:\n%s", code, unread, out)
	}
}
