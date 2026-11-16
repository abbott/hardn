package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/host/sysctl"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The Kernel Settings screen on simulated hosts (sysctl.go): the control's
// reading through the screen's own mapping, each review the real screen up
// to its question.

// TestSysctlGolden pins each mock on the core set and under LANG=C.
func TestSysctlGolden(t *testing.T) { goldenListing(t, ksListing) }

// TestSysctlMocks holds each mock to what its note says: the rows below
// the baseline and what hardn sets, the rows left alone and why, the item,
// and each review up to its question; every note opens with the source
// label; and no line is cut.
func TestSysctlMocks(t *testing.T) {
	tests := []struct {
		mock      string
		has, lack []string
	}{
		{"menu", []string{"4) Kernel               Harden sysctl (network, kernel)", "8) Configuration        hardn.yml, dns, ntp, syslog"}, []string{"Kernel Settings"}},
		{"default", []string{"Review ~ 4 unset", "[ ] ICMP redirects       1 accepted, hardn sets 0", "[ ] Process tracing      0 unrestricted, hardn sets 1",
			"[+] Reverse-path filter  1 strict", " -  Forwarding           0 off", "1) Apply settings       Set 4 settings", "[0-1 or q]"}, []string{" -  File ", "Remove settings"}},
		{"configured", []string{"+ 4 items applied", "[+] ICMP redirects       0 ignored", "Set by hardn -", " -  File                 " + sysctl.File, "1) Remove settings      Undo hardn's change"},
			[]string{"[ ]", "Apply settings", "At boot"}},
		{"overridden", []string{"Partial ~ 1 set outside hardn", "[ ] ICMP redirects       1 accepted, set in 99-sysctl.conf", "1) Remove settings"}, []string{"Apply settings", "hardn sets"}},
		{"routes", []string{" -  Sending redirects    1 on, left alone: this host routes", " -  Reverse-path filter  2 loose, left alone: this host routes", " -  Forwarding           1 on: this host routes",
			"[ ] IPv6 redirects       1 accepted, hardn sets 0", "[+] Performance events   3 limited", "Set 4 settings"}, []string{"Sending redirects    1 on, hardn sets"}},
		{"pve", []string{" -  Sending redirects    1 on, left alone: Proxmox VE", " -  Forwarding           0 off", "Set 4 settings"}, []string{"this host routes"}},
		{"container", []string{"in a container", " -  Process tracing      0 unrestricted", "In a container: hardn sets none of these.", "Not in a container", "[-] Unavailable", "[0 or q]"},
			[]string{"[ ]", "[+]", ", hardn sets"}},
		{"alpine", []string{"Review ~ 6 unset", "[ ] FIFOs in /tmp        0 not protected, hardn sets 1", "[ ] IPv6 redirects       1 accepted, hardn sets 0", "[+] Unprivileged BPF     1 off"}, nil},
		{"alpine-not-at-boot", []string{"Partial ~ not set at boot", "[ ] At boot              Not set: sysctl service not enabled", " -  To fix               rc-update add sysctl boot",
			"[+] ICMP redirects       0 ignored", "1) Remove settings"}, []string{"Apply settings"}},
		{"review", []string{"# Apply settings #", "+" + strings.TrimSpace(sysctl.Header), "+# before: net.ipv6.conf.eth0.accept_redirects = 1", "+net.ipv4.conf.*.accept_redirects = 0",
			"+net.ipv6.conf.*.accept_redirects = 0", "+net.ipv4.conf.*.send_redirects = 0", "+kernel.yama.ptrace_scope = 1", "$ sysctl -w net.ipv6.conf.eth0.accept_redirects=0", "$ sysctl -w kernel.yama.ptrace_scope=1", "gdb -p", "Apply kernel settings? [y/N, 0 or q]:"},
			[]string{"rp_filter", "ip_forward", "forwarding", "accept_ra", "sysctl -p", "Cancelled", "Successful"}},
		{"review-routes", []string{"Sending redirects and Reverse-path filter", "+kernel.kptr_restrict = 1", "+# before: net.ipv6.conf.eth0.accept_redirects = 1", "+net.ipv6.conf.*.accept_redirects = 0"},
			[]string{"send_redirects", "ip_forward", "forwarding", "rp_filter"}},
		{"review-alpine", []string{"+net.ipv6.conf.eth0.accept_redirects = 0", "+net.ipv4.conf.all.send_redirects = 0", "+fs.protected_fifos = 1", "$ sysctl -w net.ipv6.conf.eth0.accept_redirects=0"},
			[]string{"gdb", "unprivileged_bpf", "*", "rc-update", "sysctl -p"}},
		{"review-alpine-service-off", []string{"The sysctl service isn't enabled, so these are not set", "rc-update add sysctl boot.", "Apply kernel settings? [y/N, 0 or q]:"}, []string{"$ rc-update"}},
		{"review-remove", []string{"# Remove settings #", "-net.ipv4.conf.*.accept_redirects = 0", "$ sysctl -w net.ipv4.conf.all.accept_redirects=1", "$ sysctl -w net.ipv6.conf.eth0.accept_redirects=1",
			"$ sysctl -w kernel.yama.ptrace_scope=0", "Remove hardn's kernel settings? [y/N, 0 or q]:"}, []string{"sysctl -p"}},
		{"refuse-file", []string{sysctl.File + " is not hardn's file, and hardn", "Move that file away, then try again."}, []string{"[y/N"}},
	}
	mocks := ksListing.mocks()
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
		for _, line := range strings.Split(out, "\n") {
			if ansi.StringWidth(line) > 80 {
				t.Errorf("%s: wider than 80 columns: %q", tt.mock, line)
			}
		}
		if note := ksListing.note(m); !strings.HasPrefix(note, ksLabel) {
			t.Errorf("%s: %q", m.name, note)
		}
	}
}

// TestSysctlUsage: the listing lists its mocks' names, and the listing and
// play refuse a mock or a host that is not one: status 2, nothing on stdout.
func TestSysctlUsage(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{ksPrefix, "-list"}, nil, offTerminal, &out, &errs); code != exitOK || errs.Len() > 0 {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}
	if got := strings.Fields(out.String()); len(got) != len(ksMocks()) || got[0] != "menu" || got[len(got)-1] != "refuse-file" {
		t.Errorf("listed %v", got)
	}
	for _, args := range [][]string{{ksPrefix, "nope"}, {"play", ksPrefix + "/nope"}, {"play", "-fail", "1", ksPrefix + "/default"}} {
		var out, errs bytes.Buffer
		code := run(args, func() *prompt.Reader { return noInput() }, offTerminal, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errs.String())
		}
	}
}

// TestPlaySysctl plays the screen on the engine: the main menu's 4 leads
// to it; Apply settings applied on Rocky Linux 10, the screen then
// Configured, "4 items applied", with Remove settings; Remove settings applied, the
// earlier values back; applied and removed on Debian 13 that routes, on
// Proxmox VE and on Alpine, whose file names each interface; Alpine with
// the sysctl service off is Partial once applied; and a dry run stops at
// the plan. No command goes unanswered.
func TestPlaySysctl(t *testing.T) {
	code, out, errs, unread := playGallery([]string{"4", "1", "y", "1", "y", "0", "0"}, "-profile", "plain")
	for _, want := range []string{"Info: " + ksMainHost, "Review ~ 4 unset", "Apply kernel settings? [y/N, 0 or q]:", "$ sysctl -w kernel.yama.ptrace_scope=1", "+ 4 items applied",
		"[+] ICMP redirects       0 ignored", "1) Remove settings      Undo hardn's change", "Remove hardn's kernel settings? [y/N, 0 or q]:", "$ sysctl -w kernel.yama.ptrace_scope=0"} {
		if !strings.Contains(out, want) {
			t.Errorf("play, 4: no %q", want)
		}
	}
	if code != exitOK || errs != "" || unread != 0 || strings.Contains(out, "does not model") || strings.Count(out, "Review ~ 4 unset") != 2 {
		t.Errorf("play, 4: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	// The row's count is the settings hardn's file sets, not its lines:
	// Alpine's file has a line for each interface.
	applied := map[string]string{"routes": "+ 4 items applied", "pve": "+ 4 items applied", "alpine": "+ 6 items applied"}
	for host, want := range map[string]string{"routes": "+net.ipv6.conf.*.accept_redirects = 0", "pve": "+kernel.kptr_restrict = 1", "alpine": "+net.ipv6.conf.eth0.accept_redirects = 0"} {
		code, out, errs, unread = playGallery([]string{"1", "y", "1", "y", "0"}, "-profile", "plain", ksPrefix+"/"+host)
		if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, want) || !strings.Contains(out, applied[host]) || !strings.Contains(out, "Remove hardn's kernel settings?") ||
			strings.Contains(out, "does not model") || strings.Contains(out, "Rolled back") {
			t.Errorf("play sysctl/%s: exit %d, stderr %q, %d unread:\n%s", host, code, errs, unread, out)
		}
	}
	code, out, errs, unread = playGallery([]string{"1", "y", "0"}, "-profile", "plain", ksPrefix+"/alpine-service-off")
	if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, "Partial ~ not set at boot") || !strings.Contains(out, "To fix               rc-update add sysctl boot") || strings.Contains(out, "$ rc-update") {
		t.Errorf("play sysctl/alpine-service-off: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	code, out, errs, unread = playGallery([]string{"1", "0"}, "-profile", "plain", "-sequence", "dry-run", ksMainHost)
	if code != exitOK || errs != "" || unread != 0 || strings.Contains(out, "[y/N") || !strings.Contains(out, "Write hardn's kernel settings") {
		t.Errorf("play -sequence dry-run %s: exit %d, stderr %q, %d unread:\n%s", ksMainHost, code, errs, unread, out)
	}
	code, out, _, unread = playGallery([]string{"8", "0", "0"}, "-profile", "plain", ksPrefix)
	if code != exitOK || unread != 0 || !strings.Contains(out, "Info: "+ksPrefix+"/alpine-not-at-boot") {
		t.Errorf("play sysctl: exit %d, %d unread:\n%s", code, unread, out)
	}
}
