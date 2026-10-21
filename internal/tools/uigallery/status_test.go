package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The status hosts (next/p4-gallery-status): the main menu from the real
// checks, header probe and risk level over the simulated hosts. Nothing
// here runs a command or reads a file outside the fake.

// wantStatus is a status host's main menu as docs/engine/gallery-status.md
// §5 lists it: each row's state and detail in Figma's order, the risk
// level, its gloss and factors, the distribution's bar, the uptime and
// whether a reboot is pending; and the code of each check that failed, by
// control.
type wantStatus struct {
	rows        [7]string // "<state>: <detail>", engine.State's words
	level       string    // "<level> ≈ <gloss>"
	factors     []string
	bar, uptime string
	reboot      bool
	errs        map[string]string
}

// The rows the hosts share, and the bars and uptimes.
const (
	sudoAdmin   = "configured: non-root, sudo"
	rootOnly    = "not-configured: root only"
	denyPolicy  = "configured: deny policy"
	rootKeyOnly = "partial: root key-only"
	passwordOn  = "not-configured: password auth on"
	port22      = "not-configured: default (22)"
	noUnattend  = "not-configured: no unattended-upgrades"
	unattendOn  = "configured: unattended-upgrades"
	debianBar   = "Debian 13 (Trixie)"
	ubuntuBar   = "Ubuntu 26.04 (Resolute)"
	threeDays   = "3 days, 5 hours, 12 minutes"
)

// hardenedRows are the hardened host's rows.
var hardenedRows = [7]string{sudoAdmin, denyPolicy, "configured: root disallowed", "configured: key-only auth",
	"configured: non-default (2230)", "configured: 7 profiles enforced", unattendOn}

// debianWith is the debian13 host's rows, with AppArmor's row apparmor.
func debianWith(apparmor string) [7]string {
	return [7]string{sudoAdmin, denyPolicy, rootKeyOnly, passwordOn, port22, apparmor, noUnattend}
}

// wantStatuses are the status hosts' main menus, by name.
var wantStatuses = map[string]wantStatus{
	"debian13": {rows: debianWith("configured: 7 profiles enforced"), level: "High ≈ weak security", bar: debianBar, uptime: threeDays},
	"ubuntu2604": {rows: [7]string{sudoAdmin, denyPolicy, rootKeyOnly, passwordOn, port22, "configured: 9 profiles enforced", unattendOn},
		level: "Moderate ≈ medium security", bar: ubuntuBar, uptime: threeDays},
	"alpine324": {rows: [7]string{"configured: non-root, doas", "not-configured: ufw not installed", rootKeyOnly, passwordOn, port22,
		"not-applicable: Alpine default", "not-configured: no apk-cron"}, level: "Critical ≈ no security", bar: "Alpine 3.24.2", uptime: threeDays},
	"pve9": {rows: [7]string{rootOnly, "not-configured: disabled", "not-configured: root allowed", passwordOn, "configured: default (22)",
		"configured: 11 profiles enforced", noUnattend}, level: "Critical ≈ no security", factors: []string{"up 41 days, no auto updates"},
		bar: "Proxmox VE 9.0.10 (Trixie)", uptime: "41 days, 1 hour"},
	"fresh": {rows: [7]string{rootOnly, "not-configured: ufw not installed", rootKeyOnly, passwordOn, port22, "configured: 7 profiles enforced", noUnattend},
		level: "Critical ≈ no security", bar: debianBar, uptime: threeDays},
	"hardened": {rows: hardenedRows, level: "Minimal ≈ hardened security", bar: debianBar, uptime: threeDays},
	"figma": {rows: [7]string{sudoAdmin, denyPolicy, "configured: root disallowed", "configured: key-only auth", "configured: non-default (2230)",
		"not-configured: no profiles loaded", noUnattend}, level: "Moderate ≈ medium security", factors: []string{"up 182 days, no auto updates"},
		bar: debianBar, uptime: "182 days, 2 hours, 26 minutes"},
	"lockout": {rows: [7]string{sudoAdmin, "partial: ssh blocked", "configured: root disallowed", "configured: key-only auth",
		"configured: non-default (2230)", "configured: 7 profiles enforced", unattendOn}, level: "Critical ≈ lockout risk", bar: debianBar, uptime: threeDays},
	"reboot": {rows: [7]string{sudoAdmin, denyPolicy, rootKeyOnly, passwordOn, port22, "configured: 9 profiles enforced", unattendOn},
		level: "Moderate ≈ medium security", factors: []string{"reboot pending"}, bar: ubuntuBar, uptime: threeDays, reboot: true},
	"apparmor-logging": {rows: debianWith("partial: logging only"), level: "High ≈ weak security", bar: debianBar, uptime: threeDays},
	"apparmor-restart": {rows: debianWith("partial: off after restart"), level: "High ≈ weak security", bar: debianBar, uptime: threeDays},
	"apparmor-kernel":  {rows: debianWith("not-configured: off in kernel"), level: "High ≈ weak security", bar: debianBar, uptime: threeDays},
	"apparmor-unread": {rows: debianWith("unknown: can't read"), level: "High ≈ weak security", factors: []string{"unknown counted as not configured"},
		bar: debianBar, uptime: threeDays, errs: map[string]string{"apparmor.enforce": "FILE_NOT_FOUND"}},
	"container": {rows: debianWith("not-applicable: set by the host"), level: "High ≈ weak security", bar: debianBar, uptime: "1 hour"},
}

// statusReads reports whether argv only reads the host: the scenarios'
// reads, and the status checks' and the header probe's: apt-config's
// dump, ip's addresses, doas's check of its files, apk's info and crond's
// status.
func statusReads(argv []string) bool {
	switch cmd := strings.Join(argv, " "); {
	case reads(argv), strings.HasPrefix(cmd, "apt-config dump "), strings.HasPrefix(cmd, "ip -4 addr show dev "),
		strings.HasPrefix(cmd, "doas -C "), strings.HasPrefix(cmd, "apk info -e "), cmd == "rc-service crond status":
		return true
	}
	return false
}

// TestStatusHosts reads every status host as the main menu does and
// checks its main menu against gallery-status.md §5: each row, the risk
// level and factors, the header; detect's platform as the host's note
// names it; no check failed but where the host says; and nothing but
// reads, every one of them answered by the model.
func TestStatusHosts(t *testing.T) {
	if got, want := slices.Sorted(slices.Values(statusNames())), slices.Sorted(maps.Keys(wantStatuses)); !slices.Equal(got, want) {
		t.Fatalf("hosts %v, want %v", got, want)
	}
	for _, sh := range statusHosts() {
		t.Run(sh.name, func(t *testing.T) {
			want, h, s := wantStatuses[sh.name], sh.host(), &session{}
			st, err := s.readStatus(h)
			if err != nil {
				t.Fatal(err)
			}
			for i, r := range st.reports {
				if got := fmt.Sprintf("%s: %s", r.Status.State, r.Status.Detail); i >= len(want.rows) || got != want.rows[i] {
					t.Errorf("row %d, %s: %q, want %q", i, r.Title, got, want.rows[min(i, len(want.rows)-1)])
				}
				if code := output.CodeOf(r.Err); r.Err != nil && code != want.errs[r.Control] || r.Err == nil && want.errs[r.Control] != "" {
					t.Errorf("%s: error %v (%s), want %q", r.Control, r.Err, code, want.errs[r.Control])
				}
			}
			if len(st.reports) != len(want.rows) {
				t.Errorf("%d rows, want %d", len(st.reports), len(want.rows))
			}
			vm := st.mainMenu()
			if got := vm.Risk.Level + " ≈ " + vm.Risk.Gloss; got != want.level {
				t.Errorf("risk %q, want %q", got, want.level)
			}
			if !slices.Equal(vm.Risk.Factors, want.factors) {
				t.Errorf("factors %q, want %q", vm.Risk.Factors, want.factors)
			}
			bar := strings.TrimSpace(vm.Context.Name + " " + vm.Context.Version)
			if vm.Context.Codename != "" {
				bar += " (" + vm.Context.Codename + ")"
			}
			hb := vm.Host
			if bar != want.bar || hb.Hostname != hostName || hb.Address != "192.0.2.20" || hb.Uptime != want.uptime || hb.RebootPending != want.reboot {
				t.Errorf("header %q, host %+v; want %q, %s at 192.0.2.20, up %q, reboot pending %t", bar, hb, want.bar, hostName, want.uptime, want.reboot)
			}
			// The main menu's rows are the reports', in Figma's order.
			for i, r := range vm.Status.Rows {
				if r.Label != st.reports[i].Title || r.Detail != st.reports[i].Status.Detail {
					t.Errorf("main menu row %d: %+v, want %s %q", i, r, st.reports[i].Title, st.reports[i].Status.Detail)
				}
			}
			f := st.facts
			container := sh.name == "container"
			switch {
			case f.Platform.Support != detect.Supported:
				t.Errorf("platform %+v: not a supported release", f.Platform)
			case (f.Proxmox.Installed == detect.Yes) != (sh.name == "pve9"), (f.Container.Detected == detect.Yes) != container, f.Container.Detected == detect.Unknown:
				t.Errorf("Proxmox VE %v, container %v", f.Proxmox.Installed, f.Container.Detected)
			case (f.Init.Kind == detect.OpenRC) != (sh.name == "alpine324"), f.Init.Running != detect.Yes:
				t.Errorf("init %+v", f.Init)
			}
			if u := h.unanswered(); len(u) > 0 {
				t.Errorf("commands the model does not answer: %q", u)
			}
			for _, c := range h.f.Calls() {
				switch c.Op {
				case "read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group":
				case "run":
					if !statusReads(c.Argv) {
						t.Errorf("ran %q, which does not only read", strings.Join(c.Argv, " "))
					}
				default:
					t.Errorf("a call that is not a read: %v", c)
				}
			}
			if s.holds != 0 {
				t.Errorf("the engine took its hold %d times, for a check", s.holds)
			}
		})
	}
}

// TestStatusSys checks what the checks read the setuid bit on: the tools
// the platform installs, and nothing else.
func TestStatusSys(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		h    *fakeHost
		tool string
	}{{statusOn(debian13Platform), "/usr/bin/sudo"}, {statusOn(alpine324Platform), "/usr/bin/doas"}} {
		sys := tc.h.sys()
		for _, p := range []string{"/usr/bin/sudo", "/usr/bin/doas", "/bin/bash", "/bin/sh"} {
			fi, err := sys.Stat(ctx, p)
			if err != nil {
				continue
			}
			if got := fi.Mode()&fs.ModeSetuid != 0; got != (p == tc.tool) {
				t.Errorf("%s: setuid %t", p, got)
			}
		}
		if _, err := sys.Stat(ctx, tc.tool); err != nil {
			t.Errorf("%s: %v", tc.tool, err)
		}
	}
}

// TestStatusGolden pins each host's listing on the core set: the
// gallery's header, the host's note, then its main menu down to its
// prompt, compact at 24 rows on a terminal, whole in plain mode.
func TestStatusGolden(t *testing.T) {
	for _, name := range statusNames() {
		uitest.Golden(t, statusPrefix+"/"+name, uitest.Core(), func(env term.Env) string {
			args := append(append([]string{statusPrefix}, lookArgs(t, env)...), name)
			code, out, errs := runGallery(args...)
			if code != exitOK || errs != "" {
				t.Errorf("%v: exit %d, stderr %q; want exit 0", args, code, errs)
			}
			return out
		})
	}
}

// TestPlayStatusGolden pins play over the status hosts on the core set:
// the hosts' menu, Figma's host chosen from it, its main menu, then back
// to the menu and out; and the lockout host played directly, then q.
func TestPlayStatusGolden(t *testing.T) {
	for _, r := range []struct {
		name  string
		args  []string
		lines []string
	}{
		{"menu", []string{statusPrefix}, []string{"7", "0", "0"}},
		{"lockout", []string{statusPrefix + "/lockout"}, []string{"q"}},
	} {
		uitest.Golden(t, "play/status/"+r.name, uitest.Core(), func(env term.Env) string {
			args := append(lookArgs(t, env), r.args...)
			code, out, errs, unread := playGallery(r.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread; want exit 0", r.name, args, code, errs, unread)
			}
			return out
		})
	}
}

// TestPlayStatus plays a status host's main menu: its items lead where
// the main menu's mock's do, back to the host's main menu, drawn again
// from the same reading; -sequence and -fail reach the mocks' workflows;
// the end of the input aborts.
func TestPlayStatus(t *testing.T) {
	code, out, errs, unread := playGallery([]string{"1", "0", "q"}, "-profile", "plain", statusPrefix+"/hardened")
	if code != exitOK || errs != "" || unread != 0 {
		t.Fatalf("exit %d, stderr %q, %d lines unread", code, errs, unread)
	}
	for _, s := range []string{"Info: status/hardened", "Minimal ~ hardened security", "User Management", "Nothing was applied."} {
		if !strings.Contains(out, s) {
			t.Errorf("no %q in the transcript", s)
		}
	}
	if n := strings.Count(out, "Configured key-only auth"); n != 2 {
		t.Errorf("the host's main menu drawn %d times, want twice", n)
	}
	code, out, _, _ = playGallery([]string{"q"}, "-profile", "plain", "-sequence", "typed", "-fail", "2", statusPrefix+"/figma")
	if code != exitOK || !strings.Contains(out, "typed sequence") || !strings.Contains(out, "(-fail 2)") {
		t.Errorf("-sequence typed -fail 2: exit %d, the note does not say so:\n%s", code, out)
	}
	if code, _, _, _ = playGallery(nil, statusPrefix); code != prompt.ExitInterrupted {
		t.Errorf("the end of the input at the hosts' menu: exit %d, want %d", code, prompt.ExitInterrupted)
	}
}

// TestStatusUsage: names that are not hosts, a flag after a name, a bad
// size, in the listing and in play: exit 2 before anything is read or
// written; and -list.
func TestStatusUsage(t *testing.T) {
	for _, args := range [][]string{
		{statusPrefix, "nope"},
		{statusPrefix, "hardened", "-list"},
		{statusPrefix, "-width", "0", "hardened"},
		{"play", statusPrefix + "/nope"},
		{"play", statusPrefix, "hardened"},
	} {
		var out, errs bytes.Buffer
		read := false
		code := run(args, func() *prompt.Reader { read = true; return noInput() }, offTerminal, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 || read {
			t.Errorf("%v: exit %d, stdout %q, stderr %q, stdin read %t; want exit %d, a message, no output", args, code, out.String(), errs.String(), read, exitUsage)
		}
	}
	code, out, errs := runGallery(statusPrefix, "-list")
	if code != exitOK || errs != "" || out != strings.Join(statusNames(), "\n")+"\n" {
		t.Errorf("-list: exit %d, stdout %q, stderr %q", code, out, errs)
	}
	code, out, _ = runGallery(statusPrefix, "-list", "lockout", "debian13")
	if code != exitOK || out != "debian13\nlockout\n" {
		t.Errorf("-list lockout debian13: exit %d, stdout %q; want the catalogue's order", code, out)
	}
}

// TestStatusSmoke lists every host in every profile, both themes and both
// glyph sets, at 60 and 80 columns: exit status 0, each host's header, and
// no line wider than the terminal but a header.
func TestStatusSmoke(t *testing.T) {
	for _, p := range profiles {
		for _, th := range []string{"dark", "light"} {
			for _, ascii := range []bool{false, true} {
				for _, w := range []int{60, 80} {
					args := []string{statusPrefix, "-profile", p.name, "-theme", th, fmt.Sprintf("-ascii=%t", ascii), fmt.Sprintf("-width=%d", w)}
					code, out, errs := runGallery(args...)
					where := strings.Join(args, " ")
					if code != exitOK || errs != "" {
						t.Fatalf("%s: exit %d, stderr %q", where, code, errs)
					}
					ctx := component.NewCtx(newCell(p, th == "dark", !ascii, w).env)
					var headers []string
					for _, n := range statusNames() {
						headers = append(headers, ansi.Strip(header(ctx, statusPrefix+" "+n)))
					}
					lines := strings.Split(strings.TrimSuffix(ansi.Strip(out), "\n"), "\n")
					for _, h := range headers {
						if !slices.Contains(lines, h) {
							t.Errorf("%s: no header %q", where, h)
						}
					}
					for _, l := range lines {
						if !slices.Contains(headers, l) && ansi.StringWidth(l) > w {
							t.Errorf("%s: line %q is wider than %d", where, l, w)
						}
					}
				}
			}
		}
	}
}

// TestScenarioHostsKeepTheirs: the engine's scenarios' hosts carry none
// of the status hosts' readings, so the commands their engine runs are
// answered as they were (TestFakeHost, TestPlayEngine), and a command
// only a status host answers is kept as unanswered there.
func TestScenarioHostsKeepTheirs(t *testing.T) {
	for _, sc := range scenarios() {
		h := sc.host()
		if h.status != nil {
			t.Errorf("%s: its host has the status hosts' readings", sc.name)
		}
		if h.openRC {
			continue
		}
		if _, err := h.f.Run(context.Background(), "dpkg-query", "--show", "unattended-upgrades"); err == nil || len(h.unanswered()) != 1 {
			t.Errorf("%s: dpkg-query for unattended-upgrades: error %v, unanswered %q; want it kept", sc.name, err, h.unanswered())
		}
	}
}
