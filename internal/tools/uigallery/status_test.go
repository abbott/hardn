package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/engine"
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
// §5 lists it: each row's state and detail in the rows' order, the risk
// level, its gloss and score, the lines under the rows, the alert, the
// distribution's bar, the uptime and the host block's reboot line; the
// code of each check that failed, and a finding §5 names, by control.
type wantStatus struct {
	rows        [8]string // "<state>: <detail>", engine.State's words
	level       string    // "<level> ≈ <gloss>"
	score       string    // the score, to one decimal place
	lines       []string  // "<row>: <line>", "(muted)" after one not in status.warn
	alert       bool      // the CVE-2026-123158 alert (output.AlertOf), which draws its factor
	bar, uptime string
	reboot      component.RebootLine
	errs, finds map[string]string
}

// The rows the hosts share, and the bars and uptimes.
const (
	sudoAdmin   = "configured: non-root, sudo"
	rootOnly    = "not-configured: root only"
	denyPolicy  = "configured: deny policy"
	rootKeyOnly = "partial: root key-only"
	passwordOn  = "not-configured: password auth on"
	port22      = "not-configured: default (22)"
	ufwInactive = "not-configured: inactive"
	ubuntuAA    = "configured: 9 profiles enforced"
	noUnattend  = "not-configured: no unattended-upgrades"
	unattendOn  = "configured: unattended-upgrades"
	dnsNoFile   = "not-applicable: no hardn.yml" // no hardn.yml: hardn's defaults list no nameserver
	dnsHardn    = "configured: 192.0.2.54, 2001:db8::54"
	debianBar   = "Debian 13 (Trixie)"
	ubuntuBar   = "Ubuntu 26.04 (Resolute)"
	threeDays   = "3 days, 5 hours, 12 minutes"
)

// hardenedRows are the hardened host's rows.
var hardenedRows = [8]string{sudoAdmin, "configured: root disallowed", "configured: key-only auth", "configured: non-default (2230)",
	denyPolicy, dnsHardn, unattendOn, "configured: 7 profiles enforced"}

// debianWith is the debian13 host's rows, with AppArmor's row apparmor.
func debianWith(apparmor string) [8]string {
	return [8]string{sudoAdmin, rootKeyOnly, passwordOn, port22, denyPolicy, dnsNoFile, noUnattend, apparmor}
}

// containerRows are the container's: the debian13 host's, AppArmor and
// DNS set by the container's host.
var containerRows = func() [8]string {
	rows := debianWith("not-applicable: set by the host")
	rows[5] = "not-applicable: container"
	return rows
}()

// hardenedWith is the hardened host's rows, with the Firewall's row
// firewall.
func hardenedWith(firewall string) [8]string {
	rows := hardenedRows
	rows[4] = firewall
	return rows
}

// wantStatuses are the status hosts' main menus, by name.
var wantStatuses = map[string]wantStatus{
	"debian13": {rows: debianWith("configured: 7 profiles enforced"), level: "High ≈ weak security", score: "47.5", bar: debianBar, uptime: threeDays},
	"ubuntu2604": {rows: [8]string{sudoAdmin, rootKeyOnly, passwordOn, port22, denyPolicy, dnsNoFile, unattendOn, ubuntuAA},
		level: "Moderate ≈ medium security", score: "67.5", bar: ubuntuBar, uptime: threeDays},
	"alpine324": wantStatusAlpine324,
	"pve9": {rows: [8]string{rootOnly, "not-configured: root allowed", passwordOn, "configured: default (22)", "not-configured: disabled",
		dnsNoFile, "not-applicable: updated by hand", "configured: 10 profiles enforced"}, level: "Critical ≈ no security", score: "18.8",
		bar: "Proxmox VE 9.0.10 (Trixie)", uptime: "41 days, 1 hour"},
	"fresh": {rows: [8]string{rootOnly, rootKeyOnly, passwordOn, port22, "not-configured: ufw not installed", dnsNoFile, noUnattend, "configured: 7 profiles enforced"},
		level: "Critical ≈ no security", score: "17.5", bar: debianBar, uptime: threeDays},
	"ubuntu2604-cloud": {rows: [8]string{sudoAdmin, rootKeyOnly, "configured: key-only auth", port22, ufwInactive, dnsNoFile, unattendOn, ubuntuAA},
		level: "Moderate ≈ medium security", score: "62.5", lines: []string{"User: No password to become root"}, bar: ubuntuBar, uptime: threeDays,
		finds: map[string]string{"users.admin": "USERS_NOPASSWD"}},
	"ubuntu2604-iso": {rows: [8]string{sudoAdmin, rootKeyOnly, passwordOn, port22, ufwInactive, dnsNoFile, unattendOn, ubuntuAA},
		level: "High ≈ weak security", score: "47.5", bar: ubuntuBar, uptime: threeDays},
	"hardened": {rows: hardenedRows, level: "Minimal ≈ hardened security", score: "100.0", bar: debianBar, uptime: threeDays},
	"figma": {rows: [8]string{sudoAdmin, "configured: root disallowed", "configured: key-only auth", "configured: non-default (2230)", denyPolicy,
		dnsHardn, noUnattend, "not-configured: no profiles loaded"}, level: "Moderate ≈ medium security", score: "60.0", lines: []string{"Auto Updates: Up 182 days, no updates"},
		bar: debianBar, uptime: "182 days, 2 hours, 26 minutes"},
	"lockout": {rows: hardenedWith("partial: ssh blocked"), level: "Critical ≈ lockout risk", score: "90.0", bar: debianBar, uptime: threeDays},
	"lockout-at-start": {rows: hardenedWith("not-configured: enabled, not running"), level: "Moderate ≈ medium security", score: "70.0",
		lines: []string{"Firewall: SSH may be blocked after restart"}, bar: debianBar, uptime: threeDays,
		finds: map[string]string{"firewall.incoming": "UFW_SSH_NOT_ALLOWED_AT_START"}},
	"reboot": {rows: [8]string{sudoAdmin, rootKeyOnly, passwordOn, port22, denyPolicy, dnsNoFile, unattendOn, ubuntuAA},
		level: "Moderate ≈ medium security", score: "57.5", bar: ubuntuBar, uptime: threeDays, reboot: component.RebootPending},
	"apparmor-logging": {rows: debianWith("partial: logging only"), level: "High ≈ weak security", score: "37.5", bar: debianBar, uptime: threeDays},
	"apparmor-restart": {rows: debianWith("partial: off after restart"), level: "High ≈ weak security", score: "42.5", bar: debianBar, uptime: threeDays},
	"apparmor-kernel":  {rows: debianWith("not-configured: off in kernel"), level: "High ≈ weak security", score: "37.5", bar: debianBar, uptime: threeDays},
	"apparmor-unread": {rows: debianWith("unknown: can't read"), level: "High ≈ weak security", score: "37.5", lines: []string{"AppArmor: Counted as not configured (muted)"},
		bar: debianBar, uptime: threeDays, errs: map[string]string{"apparmor.enforce": "FILE_NOT_FOUND"}},
	"container": {rows: containerRows, level: "High ≈ weak security", score: "41.7", bar: debianBar, uptime: "1 hour"},
	"alpine-unpatched": {rows: [8]string{"configured: non-root, doas", "configured: root locked", "configured: key-only auth", port22,
		"not-configured: ufw not installed", dnsNoFile, "not-configured: no apk-cron", "not-applicable: Alpine default"},
		level: "Moderate ≈ medium security", score: "50.0", alert: true, bar: "Alpine 3.24.2", uptime: threeDays,
		finds: map[string]string{"ssh.auth": "SSH_SSHD_UNFIXED_LATENT"}},
	"alpine-exposed": {rows: wantStatusAlpine324.rows, level: "Critical ≈ password bypass", score: "27.8", alert: true, bar: "Alpine 3.24.2", uptime: threeDays,
		finds: map[string]string{"ssh.auth": "SSH_PASSWORD_BYPASS"}},
}

// wantStatusAlpine324 is the alpine324 host's main menu.
var wantStatusAlpine324 = wantStatus{rows: [8]string{"configured: non-root, doas", "configured: root locked", passwordOn, port22, "not-configured: ufw not installed",
	dnsNoFile, "not-configured: no apk-cron", "not-applicable: Alpine default"}, level: "High ≈ weak security", score: "27.8", bar: "Alpine 3.24.2", uptime: threeDays}

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
// level, the lines under the rows, the header and its reboot line, the
// risk level's one line; detect's platform as the host's note
// names it; no check failed but where the host says; and nothing but
// reads, every one of them answered by the model.
func TestStatusHosts(t *testing.T) {
	if got, want := slices.Sorted(slices.Values(statusNames())), slices.Sorted(maps.Keys(wantStatuses)); !slices.Equal(got, want) {
		t.Fatalf("hosts %v, want %v", got, want)
	}
	for _, sh := range statusHosts() {
		t.Run(sh.name, func(t *testing.T) {
			want, h, s := wantStatuses[sh.name], sh.host(), &session{}
			st, err := s.readStatus(context.Background(), h)
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
				if code, ok := want.finds[r.Control]; ok && !slices.ContainsFunc(r.Status.Findings, func(f engine.Finding) bool { return f.Code == code }) {
					t.Errorf("%s: findings %+v, want %s", r.Control, r.Status.Findings, code)
				}
			}
			if len(st.reports) != len(want.rows) {
				t.Errorf("%d rows, want %d", len(st.reports), len(want.rows))
			}
			vm := st.mainMenu()
			if got := vm.Risk.Level + " ≈ " + vm.Risk.Gloss; got != want.level {
				t.Errorf("risk %q, want %q", got, want.level)
			}
			if got := fmt.Sprintf("%.1f", st.level.Score); got != want.score {
				t.Errorf("score %s, want %s", got, want.score)
			}
			var lines []string
			for _, r := range vm.Status.Rows {
				for _, l := range r.Lines {
					muted := ""
					if !l.Warn {
						muted = " (muted)"
					}
					lines = append(lines, r.Label+": "+l.Text+muted)
				}
			}
			drawn := len(lines) // and the host block's reboot line and the alert: one per factor
			if vm.Host.Reboot != component.RebootNone {
				drawn++
			}
			if (vm.Alert != nil) != want.alert {
				t.Errorf("alert %+v, want one: %t", vm.Alert, want.alert)
			}
			if vm.Alert != nil {
				drawn++
			}
			if !slices.Equal(lines, want.lines) || drawn != len(st.level.Factors) {
				t.Errorf("lines %q, want %q; %d drawn for the factors %+v", lines, want.lines, drawn, st.level.Factors)
			}
			bar := strings.TrimSpace(vm.Context.Name + " " + vm.Context.Version)
			if vm.Context.Codename != "" {
				bar += " (" + vm.Context.Codename + ")"
			}
			hb := vm.Host
			if bar != want.bar || hb.Hostname != hostName || hb.Address != "192.0.2.20" || hb.Uptime != want.uptime || hb.Reboot != want.reboot {
				t.Errorf("header %q, host %+v; want %q, %s at 192.0.2.20, up %q, reboot line %d", bar, hb, want.bar, hostName, want.uptime, want.reboot)
			}
			// The main menu's rows are the reports', in risk.Rows' order.
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
			case (f.Init.Kind == detect.OpenRC) != strings.HasPrefix(sh.name, "alpine"), f.Init.Running != detect.Yes:
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
// prompt, in its full form (screen.CompactMainMenu).
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
		{"menu", []string{statusPrefix}, []string{"9", "0", "0"}},
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
		{statusPrefix, "-nope"},
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
	if code, out, errs := runGallery(statusPrefix, "-h"); code != exitOK || out != "" || !strings.Contains(errs, statusUsage) {
		t.Errorf("-h: exit %d, stdout %q, stderr %q; want exit 0 and the usage", code, out, errs)
	}
	if code, out, errs := runGallery(statusPrefix, "-override", writeFile(t, "not json"), "hardened"); code != exitError || out != "" || !strings.Contains(errs, "override file") {
		t.Errorf("a bad -override: exit %d, stdout %q, stderr %q; want exit %d and a message", code, out, errs, exitError)
	}
}

// TestStatusWriteFails: a failed write of a host's note in play, the host
// played directly or chosen from the hosts' menu, and of a host in the
// listing, exits 1 with the error. A host that cannot be read stops the
// listing there, its name in the error, after the hosts before it, and
// play's host with the host's name: the reading fails only with its
// context (detect.Detect), cancelled here.
func TestStatusWriteFails(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		lines []string
		at    string
		errs  string
	}{
		{[]string{"play", "-profile", "plain", statusPrefix + "/hardened"}, nil, "Info: status/hardened", "uigallery: play: broken pipe\n"},
		{[]string{"play", "-profile", "plain", statusPrefix}, []string{"8"}, "Info: status/hardened", "uigallery: play: broken pipe\n"},
		{[]string{statusPrefix, "-profile", "plain", "debian13", "hardened"}, nil, "status hardened", "uigallery: status: hardened: broken pipe\n"},
	} {
		var out, errs bytes.Buffer
		w := &breaks{w: &out, at: tc.at}
		ty := uitest.NewTypist(&out, tc.lines...)
		code := run(tc.args, func() *prompt.Reader { return prompt.New(ty, false) }, offTerminal, w, &errs)
		if code != exitError || errs.String() != tc.errs || !w.broke {
			t.Errorf("%v, at %q: exit %d, stderr %q, the write failed %t; want exit %d, stderr %q", tc.args, tc.at, code, errs.String(), w.broke, exitError, tc.errs)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	debian, _ := findStatusHost("debian13")
	hardened, _ := findStatusHost("hardened")
	unread := statusHost{name: "unread", host: func() *fakeHost { cancel(); return hardened.host() }}
	var out bytes.Buffer
	err := listStatus(ctx, component.NewCtx(uitest.Core()[1].Env), &out, []statusHost{debian, unread, hardened})
	if !errors.Is(err, context.Canceled) || !strings.HasPrefix(err.Error(), "unread: ") ||
		!strings.Contains(out.String(), "status debian13") || strings.Contains(out.String(), "status hardened") {
		t.Errorf("listing a host that cannot be read: %v, the listing:\n%s", err, out.String())
	}
	if err := (&session{}).openHost(ctx, hardened); !errors.Is(err, context.Canceled) || !strings.HasPrefix(err.Error(), statusPrefix+"/hardened: ") {
		t.Errorf("playing a host that cannot be read: %v", err)
	}
}

// TestReadingsAnswer checks the model's answers beside the ones the hosts
// ask for: a unit not active or not enabled, a unit it does not know, an
// interface with no address, crond stopped, apk-cron missing, and a host
// with no readings, which answers nothing.
func TestReadingsAnswer(t *testing.T) {
	r := &readings{
		units: map[string]unit{"a.timer": {active: "inactive", enabled: "not-found"}, "b.timer": {active: "active", enabled: "masked"}},
		addrs: map[string]string{"eth0": ipAddr("eth0", false)},
	}
	for _, tc := range []struct {
		cmd    string
		ok     bool
		stdout string
		code   int
	}{
		{"systemctl is-active a.timer", true, "inactive\n", 3},
		{"systemctl is-active b.timer", true, "active\n", 0},
		{"systemctl is-enabled a.timer", true, "not-found\n", 4},
		{"systemctl is-enabled b.timer", true, "masked\n", 1},
		{"systemctl is-active c.timer", false, "", 0},
		{"ip -4 addr show dev eth0", true, ipAddr("eth0", false), 0},
		{"ip -4 addr show dev eth1", true, "", 1},
		{"rc-service crond status", true, " * status: stopped\n", 3},
		{"apk info -e apk-cron", true, "", 1},
		{"doas -C /etc/doas.conf", true, "", 0},
		{"systemctl restart b.timer", false, "", 0},
	} {
		got, ok := r.answer(strings.Fields(tc.cmd))
		if ok != tc.ok || got.Stdout != tc.stdout || got.ExitCode != tc.code {
			t.Errorf("%s: answered %t, %q, exit %d; want %t, %q, exit %d", tc.cmd, ok, got.Stdout, got.ExitCode, tc.ok, tc.stdout, tc.code)
		}
	}
	var none *readings
	if _, ok := none.answer([]string{"rc-service", "crond", "status"}); ok || none.installed("unattended-upgrades") != "" {
		t.Error("a host with no readings answered")
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
