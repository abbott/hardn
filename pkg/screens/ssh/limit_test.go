package ssh_test

import (
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/sshd"
)

// Brute-Force Limit, the SSH Configuration section's row
// (docs/engine/gallery-bruteforce.md, adopted 2026-11-13;
// docs/engine/screen-bruteforce.md): sshd's PerSourcePenalties as sshd -T
// prints it, and the file that decides it.

// TestLimitRow: every state of the row, each after Allowed Users; the SSH
// Auth row is the same whatever it says (decision 3), and the screen has no
// item for it (decision 4).
func TestLimitRow(t *testing.T) {
	site := sshd.DropInDir + "/20-site.conf"
	str := func(s string) *string { return &s }
	base := play(ready(t, "debian13"), false, "0")
	for _, tc := range []struct {
		name string
		set  func(*sshHost)
		want string
		warn bool
	}{
		{"no file sets it: sshd's default", func(*sshHost) {}, "Brute-Force Limit On sshd's default", false},
		{"a file says yes", func(h *sshHost) { h.sshFile(site, "PerSourcePenalties yes\n", 0o644) }, "Brute-Force Limit On by 20-site.conf", false},
		{"a file sets a penalty", func(h *sshHost) { h.sshFile(site, "PerSourcePenalties authfail:10s\n", 0o644) }, "Brute-Force Limit On by 20-site.conf", false},
		{"a file says no", func(h *sshHost) { h.sshFile(site, "PerSourcePenalties no\n", 0o644) }, "Brute-Force Limit Off by 20-site.conf", true},
		{"no, then a later file's penalty, which turns them on", func(h *sshHost) {
			h.sshFile(sshd.DropInDir+"/10-off.conf", "PerSourcePenalties no\n", 0o644)
			h.sshFile(site, "PerSourcePenalties crash:2m\n", 0o644)
		}, "Brute-Force Limit On by 20-site.conf", false},
		{"no in hardn's own file: no note", func(h *sshHost) { h.sshFile(sshd.HardnDropIn, "PerSourcePenalties no\n", 0o644) }, "Brute-Force Limit Off", true},
		{"off, and no line hardn read says so: no file named", func(h *sshHost) { h.penalties = str("persourcepenalties no\n") }, "Brute-Force Limit Off", true},
		{"an sshd before 9.8: no line", func(h *sshHost) { h.oldSSHD = true }, "Brute-Force Limit Not available needs OpenSSH 9.8", false},
		{"a line hardn can't read", func(h *sshHost) { h.penalties = str("persourcepenalties maybe\n") }, "Brute-Force Limit Can't read", false},
		{"the line twice", func(h *sshHost) { h.penalties = str("persourcepenalties no\npersourcepenalties crash:90\n") }, "Brute-Force Limit Can't read", false},
		{"a file name with control characters", func(h *sshHost) {
			h.sshFile(sshd.DropInDir+"/20-\x1b[31msite.conf", "PerSourcePenalties no\n", 0o644)
		}, "Brute-Force Limit Off by 20-", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := ready(t, "debian13")
			tc.set(h)
			p := play(h, false, "0")
			if p.err != nil || p.unread != 0 {
				t.Fatalf("Run = %v, %d unread:\n%s", p.err, p.unread, p.out)
			}
			got := line(p.out, false, "Brute-Force Limit")
			if got != tc.want && !(strings.HasSuffix(tc.want, "by 20-") && strings.HasPrefix(got, tc.want)) {
				t.Errorf("row %q, want %q", got, tc.want)
			}
			if strings.Contains(p.out, "\x1b[31m") {
				t.Errorf("the file's name drawn as it is:\n%q", p.out)
			}
			_, after, _ := strings.Cut(p.out, "Allowed Users")
			if i, j := strings.Index(after, "Brute-Force Limit"), strings.Index(after, "Logged in as"); i < 0 || j < i {
				t.Errorf("the row is not between Allowed Users and Logged in as:\n%s", p.out)
			}
			if warn := strings.Contains(p.out, "[ ] Brute-Force Limit"); warn != tc.warn {
				t.Errorf("a warning %t, want %t:\n%s", warn, tc.warn, p.out)
			}
			for _, row := range []string{"SSH", "~"} {
				if a, b := line(p.out, true, row), line(base.out, true, row); a != b {
					t.Errorf("%s reads %q, with the default %q", row, a, b)
				}
			}
			if a, b := menuOf(p.out), menuOf(base.out); a != b {
				t.Errorf("items %q, with the default %q", a, b)
			}
		})
	}
}

// TestLimitRowNotRead: where sshd -T fails nothing is read, and no row
// claims a limit.
func TestLimitRowNotRead(t *testing.T) {
	p := play(ready(t, "not-read"), false, "0")
	if p.err != nil || strings.Contains(p.out, "Brute-Force Limit On") || strings.Contains(p.out, "Brute-Force Limit Off") {
		t.Errorf("Run = %v:\n%s", p.err, p.out)
	}
}

// menuOf is the screen's menu, from "Select an option" on.
func menuOf(out string) string {
	_, m, _ := strings.Cut(out, "Select an option")
	return m
}
