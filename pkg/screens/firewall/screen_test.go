package firewall_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/ufw"
	fwscreen "github.com/abbott/hardn/pkg/screens/firewall"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The words the tests look for: decided (gallery-firewall.md §6;
// control-firewall.md's proposed ones where marked) and the screen's.
const (
	noChange      = "hardn reads pve-firewall and doesn't change it: a wrong rule can cut off the web UI and the cluster."
	disableWarns  = "This stops the firewall and leaves every port open."
	removeNotice  = "This takes the profile out of hardn.yml. A rule already in ufw stays."
	notLoaded     = "No hardn.yml was read, so there is none to save to. Create /etc/hardn/hardn.yml, then try again."
	applyInactive = "ufw isn't running, so hardn saves no rule for it. Enable the firewall (Firewall, option 1), then apply."
	cantConfirm   = "Can't confirm with the hostname:"
	planned       = "No actual changes were made"
)

// TestScreen: the screen on each of the gallery's hosts, as read: the Firewall
// row, the main menu's; item 1 by what its plan does, refused or not; item 2
// where ufw is installed; Disable beside on an active ufw whose item 1 is not
// it; Proxmox VE's Return alone. Drawing reads only, live and under plan only:
// nothing written, no engine made, nothing planned.
func TestScreen(t *testing.T) {
	const (
		p2 = ", 2 Application Profiles, 3 Trusted Interfaces"
		d3 = ", 4 Disable firewall"
	)
	for _, tt := range []struct {
		host, status, items string
		risk                bool
	}{
		{"debian13", "Not Configured ufw not installed", "1 Install UFW", false},
		{"ubuntu2604", "Not Configured inactive", "1 Enable firewall" + p2, true},
		{"active22", "Configured deny policy", "1 Disable firewall" + p2, false},
		{"hardened", "Configured deny policy", "1 Disable firewall" + p2, false},
		{"allow-policy", "Partial allow policy", "1 Set deny policy" + p2 + d3, true},
		{"lockout", "Partial ssh blocked", "1 Let SSH through" + p2 + d3, true},
		{"lockout-at-start", "Not Configured enabled, not running", "1 Enable firewall" + p2, false},
		{"allowlist", "Partial ssh allowlist", "1 Disable firewall" + p2, false},
		{"blocklist", "Not Configured inactive", "1 Enable firewall" + p2, false},
		{"edit-2230", "Not Configured inactive", "1 Enable firewall" + p2, false},
		{"moved-2230", "Unknown ssh config not in effect", "1 Repair firewall" + p2 + d3, false},
		{"denied", "Partial ssh blocked", "1 Repair firewall" + p2 + d3, false},
		{"alpine324", "Not Configured ufw not installed", "1 Install UFW", false},
		{"alpine-stock", "Not Configured ufw not installed", "1 Install UFW", false},
		{"alpine-runlevel", "Partial off after restart", "1 Start ufw at boot" + p2 + d3, false},
		{"unit-off", "Partial off after restart", "1 Start ufw at boot" + p2 + d3, false},
		{"pve9", "Not Configured disabled", "", false},
		{"pve9-drop", "Configured drop policy", "", false},
		{"profiles", "Configured deny policy", "1 Disable firewall" + p2, false},
	} {
		for _, dryRun := range []bool{false, true} {
			h, cfg := screenHost(t, tt.host)
			n := len(h.Calls())
			p := play(h, h, cfg, dryRun, "0")
			title := "UFW Firewall Configuration"
			if strings.HasPrefix(tt.host, "pve") {
				title = "Firewall Configuration"
			}
			s := screens(p.out, title)
			if p.err != nil || p.unread != 0 || len(s) != 1 {
				t.Fatalf("%s: Run = %v, %d unread, %d screens:\n%s", tt.host, p.err, p.unread, len(s), p.out)
			}
			if got := rowOf(s[0], "Firewall"); got != tt.status {
				t.Errorf("%s: the Firewall row %q, want %q", tt.host, got, tt.status)
			}
			if got := items(s[0]); got != tt.items {
				t.Errorf("%s: items %q, want %q", tt.host, got, tt.items)
			}
			if strings.Contains(s[0], "Lockout risk") != tt.risk {
				t.Errorf("%s: item 1 marked Lockout risk %t:\n%s", tt.host, !tt.risk, s[0])
			}
			if w := writes(h, n); len(w) > 0 || p.engines != 0 || len(p.runs) != 0 {
				t.Errorf("%s, plan only %t: drawing wrote %v, made %d engines and %d runs", tt.host, dryRun, w, p.engines, len(p.runs))
			}
		}
	}
}

// TestRows holds what the sections say to the readers (the gallery's
// TestFirewallRows, BootRow, AtBoot, InactiveMarks, IPv6Off): every listener with
// ufw's verdict, never hardn.yml's sshPort (2230 in none.yml); the rules from ufw
// status verbose or, inactive, its files, said as saved; none a muted item;
// nothing marked good while ufw filters nothing. ASCII's markers tell ok, plain,
// unknown and muted apart; warn and bad differ by colour, which the goldens'
// truecolor cell holds.
func TestRows(t *testing.T) {
	for _, tt := range []struct {
		host, key, value, marker string
	}{
		{"hardened", "[::]:2230", "Allowed", "[+]"},
		{"hardened", "0.0.0.0:2230", "Allowed", "[+]"},
		{"active22", "[::]:22", "Allowed", "[+]"}, // hardn.yml says 2230
		{"lockout", "[::]:2230", "Refused", "[ ]"},
		{"lockout-at-start", "[::]:2230", "Refused at start", "[ ]"},
		{"edit-2230", "0.0.0.0:2230", "Not filtered Not bound", "[ ]"},
		{"edit-2230", "0.0.0.0:22", "Not filtered Not configured", "[ ]"},
		{"ubuntu2604", "No firewall rules configured", "", ""},
		{"ubuntu2604", "Incoming policy", "Deny", ""},
		{"blocklist", "22/tcp from 198.51.100.0/24", "Deny Saved", "[ ]"},
		{"blocklist", "22/tcp", "Allow Saved", ""},
		{"lockout-at-start", "443/tcp", "Allow Saved", ""},
		{"alpine-runlevel", "At boot", "In no runlevel", "[ ]"},
		{"unit-off", "At boot", "Service disabled", "[ ]"},
		{"boot-masked", "At boot", "Service masked", "[ ]"},
		{"boot-not-found", "At boot", "No service", "[ ]"},
		{"boot-unread", "At boot", "Unknown", "[?]"},
		{"ipv6-off", "IPv6", "Off", "[ ]"},
		{"no-ss", "Bound sockets", "ss not installed", "[ ]"},
		{"ss-failed", "Bound sockets", "ss failed", "[ ]"},
		{"garbled", "Status", "Unknown", "[?]"},
		{"profiles", "WebServer", "80/tcp, 443/tcp Not applied", "[ ]"},
		{"pve-stopped", "Service", "Stopped", "[ ]"},
		{"pve-stopped", "Enabled", "Yes", ""},
		{"pve-stopped", "Incoming policy", "Drop", ""},
		{"pve-moved", "SSH (22)", "Management network sshd on 2230", "[ ]"},
		{"pve-blacklist", "Blacklist", "Has entries", "[ ]"},
		{"pve-ufw-left", "ufw", "Left on Run ufw disable", "[ ]"},
		{"pve-ufw-unread", "ufw", "Not read", "[ ]"},
		{"pve-unmounted", "Service", "Unknown", "[?]"},
	} {
		h, cfg := screenHost(t, tt.host)
		p := play(system(tt.host, h), h, cfg, false, "0")
		if got := rowOf(p.out, tt.key); got != tt.value || marker(p.out, tt.key) != tt.marker {
			t.Errorf("%s: %s %q %q, want %q %q:\n%s", tt.host, tt.key, marker(p.out, tt.key), got, tt.marker, tt.value, p.out)
		}
	}
	for _, unit := range []string{"linked", "masked-runtime"} {
		h, cfg := screenHost(t, "active22")
		h.unit = unit
		want := map[string]string{"linked": "Service disabled", "masked-runtime": "Service masked"}[unit]
		if got := rowOf(play(h, h, cfg, false, "0").out, "At boot"); got != want {
			t.Errorf("%s: At boot %q, want %q", unit, got, want)
		}
	}
	// hardened's SSH section is its two listeners alone, [::] first.
	h, cfg := screenHost(t, "hardened")
	out := play(h, h, cfg, false, "0").out
	var ssh []string
	if _, after, ok := strings.Cut(out, "\n  SSH -"); ok {
		_, after, _ = strings.Cut(after, "\n") // the rest of the header's line
		if next := sectionHeader.FindStringIndex(after); next != nil {
			after = after[:next[0]] // down to the next section header
		}
		for _, l := range strings.Split(after, "\n") {
			if f := strings.Fields(l); len(f) > 0 {
				ssh = append(ssh, strings.Join(f, " "))
			}
		}
	}
	if want := []string{"[+] [::]:2230 Allowed", "[+] 0.0.0.0:2230 Allowed"}; !slices.Equal(ssh, want) {
		t.Errorf("hardened: SSH section %q, want %q:\n%s", ssh, want, out)
	}
	// pve9 draws no ufw row: ufw.conf is not there.
	h, cfg = screenHost(t, "pve9")
	if p := play(h, h, cfg, false, "0"); rowOf(p.out, "ufw") != "" || !strings.Contains(flat(p.out), noChange) {
		t.Errorf("pve9:\n%s", p.out)
	}
}

// TestRefusedBeforeAsking: item 1, where its plan refuses, keeps its words and
// says the control's refusal when selected, before asking anything: no engine,
// nothing written, nothing asked.
func TestRefusedBeforeAsking(t *testing.T) {
	for host, words := range map[string]string{
		"lockout-at-start":         "ufw is set to start at boot but isn't running. Check that its rules let SSH through, then run ufw enable.",
		"blocklist":                "A saved ufw rule would block SSH on port 22, for some addresses or all. Remove it with ufw delete, then try again.",
		"edit-2230":                "sshd isn't listening where its config says, so hardn can't tell which ports SSH uses. Reload sshd or restart ssh.socket, then try again.",
		"moved-2230":               "sshd isn't listening where its config says",
		"bound-elsewhere-inactive": "sshd isn't listening where its config says",
		"denied":                   "ufw refuses SSH on [::]:22, and hardn doesn't change a rule it finds. Fix it with ufw allow or ufw delete, then try again.",
		"alpine-stock":             "hardn can't find ufw in apk's package lists. Check the network and the community repository, run apk update, then try again.",
		"apt-not-found":            "hardn can't find ufw in apt's package lists. Run apt-get update, then try again.",
		"apt-removes":              "Installing ufw would also change other packages: nftables. Install it by hand, then try again.",
		"ss-failed":                "hardn can't tell which ports SSH uses: ss failed. Check that it runs, then try again.",
		"garbled":                  "hardn can't read ufw's state. Check ufw status verbose, then try again.",
		"status-fails":             "hardn can't read ufw's state. Check ufw status verbose, then try again.",
		"proxmox-unknown":          "hardn can't tell if this host runs Proxmox VE, where it leaves the firewall alone.",
		"ipv6-off":                 "ufw's IPv6 is off in /etc/default/ufw, so it would refuse SSH on port 22. Set IPV6=yes, then try again.",
		"boot-unread":              "hardn can't tell if ufw starts at boot. Check its service, then try again.",
		"boot-masked":              "ufw's service is masked, so it can't start at boot. Run systemctl unmask ufw.service, then try again.",
		"boot-not-found":           "ufw's service isn't on this system, so it can't start at boot. Reinstall ufw, then try again.",
	} {
		for _, dryRun := range []bool{false, true} {
			h, cfg := screenHost(t, host)
			n := len(h.Calls())
			p := play(h, h, cfg, dryRun, "1", "0")
			if p.err != nil || p.unread != 0 || !strings.Contains(flat(p.out), words) || len(mainScreens(p.out)) != 2 {
				t.Errorf("%s: Run = %v, %d unread, %q not said:\n%s", host, p.err, p.unread, words, p.out)
			}
			if strings.Contains(p.out, "[y/N") || strings.Contains(p.out, "Type the hostname") || strings.Contains(flat(p.out), "ufw delete") && host == "ipv6-off" {
				t.Errorf("%s: asked, or said what IPV6=no does not need:\n%s", host, p.out)
			}
			if w := writes(h, n); len(w) > 0 || p.engines != 0 {
				t.Errorf("%s: wrote %v, made %d engines", host, w, p.engines)
			}
		}
	}
}

// TestInstall: Install through the flow, y/N, by the host's own tool; the screen
// drawn again with ufw installed and inactive. n changes nothing.
func TestInstall(t *testing.T) {
	for host, cmd := range map[string]string{"debian13": "apt-get install -y ufw", "alpine324": "apk add ufw"} {
		h, cfg := screenHost(t, host)
		p := play(h, h, cfg, false, "1", "y", "0")
		if p.err != nil || len(p.runs) != 1 || p.runs[0].Err != nil || h.missing {
			t.Fatalf("%s: Run = %v, runs %+v:\n%s", host, p.err, p.runs, p.out)
		}
		if w := inOrder(p.out, "Install UFW #", "Package ufw", "Status Inactive", "Install ufw? [y/N", "UFW Firewall Configuration #", "Installed Yes", "1) Enable firewall"); w != "" {
			t.Errorf("%s: %q not shown after what came before it:\n%s", host, w, p.out)
		}
		if !strings.Contains(strings.Join(writes(h, 0), "\n"), "run "+cmd) {
			t.Errorf("%s: %s not run: %v", host, cmd, writes(h, 0))
		}
	}
	h, cfg := screenHost(t, "debian13")
	n := len(h.Calls())
	if p := play(h, h, cfg, false, "1", "n", "0"); p.err != nil || !h.missing || len(writes(h, n)) > 0 {
		t.Errorf("declined: Run = %v, installed %t, wrote %v", p.err, !h.missing, writes(h, n))
	}
}

// TestEnableSS: ufw installed, not running, and no ss: item 1 is "Enable
// firewall", "Installs iproute2, then starts ufw", marked lockout-risk;
// chosen, it installs iproute2, its review saying why, y/N, then goes on to
// Enable's own change, the hostname typed, the timer armed. Declined at the
// install, nothing changes and Enable is not asked. The install refused (no
// iproute2 in apt's package lists): said as a warning before anything is
// asked, from the reading's plan of it, as Install iproute's is on Rocky
// Linux, AlmaLinux and RHEL: no review, no question, nothing installed.
func TestEnableSS(t *testing.T) {
	h, cfg := screenHost(t, "no-ss")
	p := play(h, h, cfg, false, "1", "y", hostName, "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 2 || h.noSS || !h.active || !armed(h, 0) {
		t.Fatalf("Run = %v, %d unread, %d runs, ss missing %t, active %t:\n%s", p.err, p.unread, len(p.runs), h.noSS, h.active, p.out)
	}
	if w := inOrder(p.out, "1) Enable firewall", "Installs iproute2, then starts ufw", "Enable Firewall #",
		"hardn needs ss, from iproute2, to check which ports SSH uses before it starts ufw.", "Package iproute2", "Install iproute2? [y/N",
		"Enable Firewall #", "Enable the firewall?", "Type the hostname", "hardn confirm", "UFW Firewall Configuration #", "Status Active"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if !strings.Contains(strings.Join(writes(h, 0), "\n"), "run apt-get install -y iproute2") {
		t.Errorf("apt-get install -y iproute2 not run: %v", writes(h, 0))
	}
	h, cfg = screenHost(t, "no-ss")
	n := len(h.Calls())
	if p := play(h, h, cfg, false, "1", "n", "0"); p.err != nil || !h.noSS || h.active || len(writes(h, n)) > 0 || strings.Contains(p.out, "Enable the firewall?") {
		t.Errorf("declined: Run = %v, ss missing %t, active %t, wrote %v:\n%s", p.err, h.noSS, h.active, writes(h, n), p.out)
	}
	h, cfg = screenHost(t, "no-ss")
	h.ssSim = &hostfake.Response{Stdout: "Reading package lists...\n", Stderr: "E: Unable to locate package iproute2\n", ExitCode: 100}
	n = len(h.Calls())
	p = play(h, h, cfg, false, "1", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 0 || !h.noSS || h.active || len(writes(h, n)) > 0 {
		t.Errorf("not in apt's lists: Run = %v, %d unread, runs %+v, ss missing %t, active %t, wrote %v:\n%s", p.err, p.unread, p.runs, h.noSS, h.active, writes(h, n), p.out)
	}
	if w := inOrder(p.out, "Enter selection", "Warning", "hardn can't find iproute2 in apt's package lists", "UFW Firewall Configuration #"); w != "" {
		t.Errorf("not in apt's lists: %q not shown after what came before it:\n%s", w, p.out)
	}
	for _, not := range []string{"Install iproute2? [y/N", "Operation Failed", "To go on", "Enable the firewall?"} {
		if strings.Contains(p.out, not) {
			t.Errorf("not in apt's lists: %q shown:\n%s", not, p.out)
		}
	}
	// The hostname Enable will ask can't be read: said before the install, nothing installed.
	h, cfg = screenHost(t, "no-ss")
	if err := h.Remove(context.Background(), header.HostnameFile); err != nil {
		t.Fatal(err)
	}
	n = len(h.Calls())
	if p := play(h, h, cfg, false, "1", "0"); p.err != nil || !strings.Contains(flat(p.out), cantConfirm) || p.engines != 0 || !h.noSS || len(writes(h, n)) > 0 {
		t.Errorf("no hostname: Run = %v, %d engines, ss missing %t:\n%s", p.err, p.engines, h.noSS, p.out)
	}
}

// TestInstallSS: ufw running and no ss: item 1 is Install iproute2, "Needed to
// check that ufw lets SSH through", y/N, by the host's own tool; the screen drawn
// again with ss there, item 1 no longer the install.
func TestInstallSS(t *testing.T) {
	h, cfg := screenHost(t, "no-ss-active")
	p := play(h, h, cfg, false, "1", "y", "0")
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err != nil || h.noSS {
		t.Fatalf("Run = %v, runs %d, ss missing %t:\n%s", p.err, len(p.runs), h.noSS, p.out)
	}
	if w := inOrder(p.out, "1) Install iproute2", "Needed to check SSH gets through", "Install iproute2 #", "Package iproute2", "Install iproute2? [y/N", "UFW Firewall Configuration #"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if screens := mainScreens(p.out); len(screens) < 2 || strings.Contains(screens[len(screens)-1], "Install iproute2") {
		t.Errorf("item 1 is still the install once ss is there:\n%s", p.out)
	}
	// ufw stopped but its state unread: Enable would refuse, so item 1 doesn't promise it.
	h, cfg = screenHost(t, "no-ss")
	if p := play(unreadable{h, ufw.DefaultsFile}, h, cfg, false, "0"); p.err != nil || !strings.Contains(flat(p.out), "1) Install iproute2 Needed to check SSH gets through") || strings.Contains(p.out, "then starts ufw") {
		t.Errorf("ufw unread: Run = %v:\n%s", p.err, p.out)
	}
}

// TestEnable: Enable and its kinds through the flow, each lockout-risk: the
// hostname typed, a wrong one refused and asked again, the timer armed; declined
// with 0, nothing changed. Start at boot is plain, y/N.
func TestEnable(t *testing.T) {
	h, cfg := screenHost(t, "ubuntu2604")
	p := play(h, h, cfg, false, "1", "host-02", hostName, "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil || !h.active || !hasRule(h, "22") || !armed(h, 0) {
		t.Fatalf("Run = %v, %d unread, active %t, rule %t, armed %t:\n%s", p.err, p.unread, h.active, hasRule(h, "22"), armed(h, 0), p.out)
	}
	if w := inOrder(p.out, "Enable Firewall #", "Status Active", "Incoming policy Deny", "At boot Yes", "Enable the firewall?", "Type the hostname [host-01, 0 or q]: host-02",
		"Type the hostname [host-01, 0 or q]: host-01", "hardn confirm", "UFW Firewall Configuration #", "Status Active"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	h, cfg = screenHost(t, "ubuntu2604")
	n := len(h.Calls())
	if p := play(h, h, cfg, false, "1", "0", "0"); p.err != nil || h.active || len(writes(h, n)) > 0 || len(p.runs) != 1 {
		t.Errorf("declined with 0: Run = %v, active %t, wrote %v:\n%s", p.err, h.active, writes(h, n), p.out)
	}
	for _, tt := range []struct{ host, answer, title, check string }{
		{"allow-policy", hostName, "Set Deny Policy #", "deny"}, {"lockout", hostName, "Let SSH Through #", "2230"},
		{"unit-off", "y", "Start at Boot #", "unit"}, {"alpine-runlevel", "y", "Start at Boot #", "runlevel"},
	} {
		h, cfg := screenHost(t, tt.host)
		p := play(h, h, cfg, false, "1", tt.answer, "0")
		if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil || !strings.Contains(flat(p.out), tt.title) {
			t.Errorf("%s: Run = %v, %d unread:\n%s", tt.host, p.err, p.unread, p.out)
			continue
		}
		ok := map[string]bool{"deny": h.incoming == "deny", "2230": hasRule(h, "2230"), "unit": h.unit == "enabled",
			"runlevel": strings.Contains(strings.Join(writes(h, 0), "\n"), "rc-update add ufw default")}[tt.check]
		if !ok {
			t.Errorf("%s: %s not done: %v", tt.host, tt.check, writes(h, 0))
		}
	}
}

// TestHostname: where Enable's plan is lockout-risk and the hostname can't be
// read, item 1 says so before asking or planning; plan only confirms nothing,
// so it needs none.
func TestHostname(t *testing.T) {
	for _, data := range []string{"<absent>", "\n"} {
		h, cfg := screenHost(t, "ubuntu2604")
		if data == "<absent>" {
			if err := h.Remove(context.Background(), header.HostnameFile); err != nil {
				t.Fatal(err)
			}
		} else {
			h.AddFile(header.HostnameFile, []byte(data), 0o444, 0, 0)
		}
		n := len(h.Calls())
		p := play(h, h, cfg, false, "1", "0")
		if p.err != nil || !strings.Contains(flat(p.out), cantConfirm) || p.engines != 0 || strings.Contains(p.out, "Type the hostname") || len(writes(h, n)) > 0 {
			t.Errorf("%q: Run = %v, %d engines:\n%s", data, p.err, p.engines, p.out)
		}
		p = play(h, h, cfg, true, "1", "0")
		if p.err != nil || strings.Contains(flat(p.out), cantConfirm) || !strings.Contains(flat(p.out), planned) {
			t.Errorf("%q, plan only: Run = %v:\n%s", data, p.err, p.out)
		}
	}
}

// TestHostnameAtTheQuestion: a plan read as plain that is lockout-risk by the
// key, the hostname gone meanwhile, says so in place of the question: unit-off's
// item 1 is read as Start at boot alone, and ufw stops before the change's
// engine plans, so the engine's plan is typed. Nothing is applied.
func TestHostnameAtTheQuestion(t *testing.T) {
	h, cfg := screenHost(t, "unit-off")
	var n int
	p := playWith(h, h, cfg, false, func(o *fwscreen.Options) {
		build := o.NewEngine
		o.NewEngine = func(progress func(engine.Event)) *engine.Engine {
			h.active = false
			h.syncFiles()
			if err := h.Remove(context.Background(), header.HostnameFile); err != nil {
				t.Fatal(err)
			}
			n = len(h.Calls())
			return build(progress)
		}
	}, "1", "0")
	if p.err != nil || p.engines != 1 || !strings.Contains(flat(p.out), cantConfirm) || strings.Contains(p.out, "[y/N") || len(p.runs) != 1 || h.active {
		t.Errorf("Run = %v, %d engines, %d runs, active %t:\n%s", p.err, p.engines, len(p.runs), h.active, p.out)
	}
	if w := inOrder(p.out, "Start at Boot #", cantConfirm, "UFW Firewall Configuration #"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if w := writes(h, n); len(w) > 0 {
		t.Errorf("wrote %v", w)
	}
}

// TestDisable: Disable, from item 1 or beside a repair, plain y/N under its
// warning; ufw stopped, its rules kept; n changes nothing.
func TestDisable(t *testing.T) {
	for host, key := range map[string]string{"active22": "1", "lockout": "4", "moved-2230": "4"} {
		h, cfg := screenHost(t, host)
		p := play(h, h, cfg, false, key, "y", "0")
		if p.err != nil || len(p.runs) != 1 || p.runs[0].Err != nil || h.active || len(h.rules) == 0 {
			t.Fatalf("%s: Run = %v, active %t, rules %v:\n%s", host, p.err, h.active, h.rules, p.out)
		}
		if w := inOrder(p.out, "Disable Firewall #", disableWarns, "Status Inactive", "Disable the firewall? [y/N", "UFW Firewall Configuration #", "Status Inactive", "1) Enable firewall"); w != "" {
			t.Errorf("%s: %q not shown after what came before it:\n%s", host, w, p.out)
		}
	}
	h, cfg := screenHost(t, "active22")
	if p := play(h, h, cfg, false, "1", "n", "0"); p.err != nil || !h.active {
		t.Errorf("declined: Run = %v, active %t", p.err, h.active)
	}
}

// TestAdd: Add's four questions, each refused at its field by ufw's own check, a
// name held in another case a warning; then the save, hardn.yml changed in
// place, the configuration adopting it, the profiles' screen drawn again with it.
func TestAdd(t *testing.T) {
	h, cfg := screenHost(t, "profiles")
	p := play(h, h, cfg, false, "2", "1", "-web", "all", "webserver", "Web", "", "Web Server", "", "A web server", "99999/tcp", "80/tcp|443/tcp", "80", "8080/tcp, 8443/tcp", "eth0:1", "", "y", "0", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil {
		t.Fatalf("Run = %v, %d unread, runs %+v:\n%s", p.err, p.unread, p.runs, p.out)
	}
	if w := inOrder(p.out, "Manage UFW Application Profiles #", "Add Profile #", "Profile name", "Start it with a letter or digit", "It keeps all and numbers for itself.", "A profile with this name already exists.",
		"Give the profile a title.", "Give the profile a description.", "Give each port, 1 to 65535", "Give each port", "Give each port",
		"Interface", "Such as tailscale0. Empty for every interface.", "ufw can't use that interface name.", "Web 8080/tcp, 8443/tcp", "Add Web? [y/N", "Manage UFW Application Profiles #", "Web 8080/tcp, 8443/tcp Not applied"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if len(cfg.UfwAppProfiles) != 3 || cfg.UfwAppProfiles[2].Name != "Web" || !strings.Contains(content(h), "name: Web\n") {
		t.Errorf("adopted %+v; hardn.yml:\n%s", cfg.UfwAppProfiles, content(h))
	}
}

// TestTrust: the trusted interfaces' Add, from item 3: a name ufw would not take
// refused, one trusted already a warning; the review says what it lets through;
// then the save, hardn.yml's key written; then Apply on an active ufw runs "ufw
// allow in on <interface>", and the row says Applied.
func TestTrust(t *testing.T) {
	h, cfg := screenHost(t, "active22")
	cfg.UfwTrustedInterfaces = []string{"wg0"}
	p := play(h, h, cfg, false, "3", "1", "eth0:1", "wg0", "tailscale0", "y", "3", "y", "0", "0")
	if p.err != nil || p.unread != 0 {
		t.Fatalf("Run = %v, %d unread:\n%s", p.err, p.unread, p.out)
	}
	if w := inOrder(p.out, "Manage UFW Trusted Interfaces #", "Add Interface #", "Interface", "Such as tailscale0.", "ufw can't use that interface name.",
		"This interface is trusted already.", "Everything arriving on this interface gets through ufw", "tailscale0 Every port", "Trust tailscale0? [y/N",
		"Apply the interfaces? [y/N", "tailscale0 Every port Applied"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if !slices.Equal(cfg.UfwTrustedInterfaces, []string{"wg0", "tailscale0"}) || !slices.ContainsFunc(h.rules, func(r fwRule) bool { return r.iface == "tailscale0" }) {
		t.Errorf("adopted %q; rules %+v", cfg.UfwTrustedInterfaces, h.rules)
	}
}

// TestRemove: Remove's Select, a number no profile has refused there; the review's
// notice; the save of the one picked, first or second; the configuration adopting
// it. Back with 0 changes nothing.
func TestRemove(t *testing.T) {
	h, cfg := screenHost(t, "profiles")
	p := play(h, h, cfg, false, "2", "2", "9", "1", "y", "0", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil {
		t.Fatalf("Run = %v, %d unread:\n%s", p.err, p.unread, p.out)
	}
	if w := inOrder(p.out, "Remove Profile #", "Remove which profile?", "9", "Remove which profile?", removeNotice, "Remove WebServer? [y/N", "Manage UFW Application Profiles #", "Metrics"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if len(cfg.UfwAppProfiles) != 1 || cfg.UfwAppProfiles[0].Name != "Metrics" || strings.Contains(content(h), "WebServer") {
		t.Errorf("adopted %+v; hardn.yml:\n%s", cfg.UfwAppProfiles, content(h))
	}
	h, cfg = screenHost(t, "profiles")
	p = play(h, h, cfg, false, "2", "2", "2", "y", "0", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil || !strings.Contains(p.out, "Remove Metrics? [y/N") {
		t.Fatalf("second: Run = %v, %d unread:\n%s", p.err, p.unread, p.out)
	}
	if len(cfg.UfwAppProfiles) != 1 || cfg.UfwAppProfiles[0].Name != "WebServer" || !strings.Contains(content(h), "WebServer") || strings.Contains(content(h), "Metrics") {
		t.Errorf("second: adopted %+v; hardn.yml:\n%s", cfg.UfwAppProfiles, content(h))
	}
	h, cfg = screenHost(t, "profiles")
	n := len(h.Calls())
	if p := play(h, h, cfg, false, "2", "2", "0", "0", "0"); p.err != nil || p.engines != 0 || len(writes(h, n)) > 0 {
		t.Errorf("back: Run = %v, %d engines, wrote %v", p.err, p.engines, writes(h, n))
	}
}

// TestApply: Apply's plan, y/N: a rule for each profile; on an inactive ufw its
// refusal said before asking, no engine made.
func TestApply(t *testing.T) {
	h, cfg := screenHost(t, "profiles")
	p := play(h, h, cfg, false, "2", "3", "y", "0", "0")
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err != nil || !strings.Contains(p.out, "Apply the profiles? [y/N") {
		t.Fatalf("Run = %v:\n%s", p.err, p.out)
	}
	if got := strings.Join(ufwCalls(h, 0), "\n"); !strings.Contains(got, "app WebServer") || !strings.Contains(got, "app Metrics") {
		t.Errorf("ufw ran:\n%s", got)
	}
	if w := inOrder(p.out, "Apply Profiles #", "WebServer 80/tcp, 443/tcp Not applied", "Manage UFW Application Profiles #", "WebServer 80/tcp, 443/tcp Applied"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	h, cfg = screenHost(t, "profiles-inactive")
	n := len(h.Calls())
	if p := play(h, h, cfg, false, "2", "3", "0", "0"); p.err != nil || p.engines != 0 || !strings.Contains(flat(p.out), applyInactive) || len(writes(h, n)) > 0 ||
		!strings.Contains(flat(p.out), "3) Apply profiles Enable the firewall first") {
		t.Errorf("inactive: Run = %v, %d engines:\n%s", p.err, p.engines, p.out)
	}
}

// TestNotLoaded: with no hardn.yml read, Add and Remove say so before asking
// anything; the profiles' screen hides Remove and Apply with no profile.
func TestNotLoaded(t *testing.T) {
	h, _ := screenHost(t, "active22")
	cfg := &config.Config{UfwAppProfiles: []config.UfwAppProfile{{Name: "Web", Title: "Web", Description: "Web", Ports: []string{"80/tcp"}}}}
	n := len(h.Calls())
	p := play(h, h, cfg, false, "2", "1", "2", "0", "0")
	if p.err != nil || p.unread != 0 || strings.Count(flat(p.out), notLoaded) != 2 || strings.Contains(p.out, "Profile name") || p.engines != 0 || len(writes(h, n)) > 0 {
		t.Errorf("Run = %v, %d unread, %d engines:\n%s", p.err, p.unread, p.engines, p.out)
	}
	h, cfg = screenHost(t, "active22")
	s := screens(play(h, h, cfg, false, "2", "0", "0").out, "Manage UFW Application Profiles")
	if len(s) != 1 || items(s[0]) != "1 Add profile" || rowOf(s[0], "No application profiles configured") != "" && !strings.Contains(s[0], "No application profiles configured") {
		t.Errorf("no profile: %q", s)
	}
}

// TestPlanOnly: under plan only every item stops at its plan, Add and Remove
// too, nothing written outside hardn's run directory.
func TestPlanOnly(t *testing.T) {
	for _, tt := range []struct {
		host  string
		lines []string
	}{
		{"debian13", []string{"1", "0"}}, {"ubuntu2604", []string{"1", "0"}}, {"active22", []string{"1", "0"}}, {"lockout", []string{"4", "0"}},
		{"alpine-runlevel", []string{"1", "0"}},
		{"profiles", []string{"2", "1", "Web", "Web Server", "A web server", "80/tcp", "", "0", "0"}},
		{"profiles", []string{"2", "2", "1", "0", "0"}}, {"profiles", []string{"2", "3", "0", "0"}},
	} {
		h, cfg := screenHost(t, tt.host)
		n := len(h.Calls())
		p := play(h, h, cfg, true, tt.lines...)
		if p.err != nil || p.unread != 0 || !strings.Contains(flat(p.out), planned) || len(p.runs) != 1 {
			t.Errorf("%s %q: Run = %v, %d unread:\n%s", tt.host, tt.lines, p.err, p.unread, p.out)
		}
		if w := writes(h, n); len(w) > 0 {
			t.Errorf("%s %q: wrote %v", tt.host, tt.lines, w)
		}
	}
}

// TestProxmox: on Proxmox VE, Return alone, no ufw call of any kind, hardn.yml
// not read; a key for an item that is not there is refused, and asked again.
func TestProxmox(t *testing.T) {
	for _, host := range []string{"pve9", "pve9-drop", "pve-ufw-left", "pve-stopped", "pve-unmounted"} {
		h, cfg := screenHost(t, host)
		n := len(h.Calls())
		p := play(h, h, cfg, false, "1", "2", "0")
		if p.err != nil || p.unread != 0 || len(screens(p.out, "Firewall Configuration")) != 1 || strings.Count(p.out, "Invalid option") != 2 || !strings.Contains(p.out, "[0 or q]") {
			t.Errorf("%s: Run = %v, %d unread:\n%s", host, p.err, p.unread, p.out)
		}
		if u := ufwCalls(h, n); len(u) > 0 {
			t.Errorf("%s: ufw ran %v", host, u)
		}
		for _, c := range h.Calls()[n:] {
			if c.Path == yml {
				t.Errorf("%s: hardn.yml %s", host, c.Op)
			}
		}
		if w := writes(h, n); len(w) > 0 || p.engines != 0 {
			t.Errorf("%s: wrote %v, %d engines", host, w, p.engines)
		}
	}
	// A reading that fails (a done context: pve.ReadFirewall's one error) draws
	// each row Unknown, never an empty screen.
	h, cfg := screenHost(t, "pve9-drop")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	err := fwscreen.Run(ctx, fwscreen.Options{Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(strings.NewReader("0\n"), false), Out: env.Writer(&out)},
		System: h, Facts: h.facts, Config: cfg, NewEngine: newEngine(h)})
	if err != nil || rowOf(out.String(), "Service") != "Unknown" || !strings.Contains(flat(out.String()), noChange) {
		t.Errorf("a done context: Run = %v:\n%s", err, out.String())
	}
}

// TestHeights (the gallery's TestFirewallHeights, its counts): the screen on
// every host and the profiles' screen at 80x24 and at 80x40, in Unicode and
// ASCII: as many lines at either height, the prompt on its last.
func TestHeights(t *testing.T) {
	want := map[string]int{"debian13": 20, "ubuntu2604": 30, "active22": 30, "hardened": 30, "allow-policy": 31, "lockout": 32,
		"lockout-at-start": 30, "allowlist": 32, "blocklist": 34, "edit-2230": 31, "moved-2230": 32, "denied": 31, "alpine324": 20, "alpine-stock": 21,
		"alpine-runlevel": 31, "unit-off": 31, "pve9": 17, "pve9-drop": 17, "profiles": 35, "sub-profiles": 16} // ufw installed: item 3, Trusted Interfaces, a line more
	for name, lines := range want {
		host, keys, at, title := name, []string{"0"}, 0, "Firewall Configuration"
		if name == "sub-profiles" {
			host, keys, at, title = "profiles", []string{"2", "0", "0"}, 1, "Manage UFW Application Profiles"
		}
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				h, cfg := screenHost(t, host)
				lang := map[bool]string{true: "LANG=en_US.UTF-8", false: "LANG=C"}[unicode]
				env := term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: rows, Environ: []string{"TERM=xterm-256color", lang}})
				var out bytes.Buffer
				p := playWith(h, h, cfg, false, func(o *fwscreen.Options) {
					o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(strings.NewReader(strings.Join(keys, "\n")+"\n"), false), Out: env.Writer(&out)}
				})
				drawn := strings.Split(escapes.ReplaceAllString(out.String(), ""), " or q]:")[at]
				got := strings.Split(drawn, "\n")
				for len(got) > 0 && !strings.Contains(got[0], title) {
					got = got[1:]
				}
				if p.err != nil || len(got) != lines || strings.Contains(drawn, "░") != unicode {
					t.Errorf("%s at 80x%d, unicode %v: Run = %v, %d lines, want %d:\n%s", name, rows, unicode, p.err, len(got), lines, drawn)
				}
			}
		}
	}
}

// TestApplyContext: each apply runs under the context Options.ApplyContext
// makes, stopped once the apply returned; a cancelled one is rolled back, ufw
// not left installed, and the screen goes on. A declined change and a dry run
// make none.
func TestApplyContext(t *testing.T) {
	for _, tc := range []struct {
		dryRun bool
		lines  []string
		made   int
	}{{false, []string{"1", "n", "1", "y", "0"}, 1}, {true, []string{"1", "0"}, 0}} {
		h, cfg := screenHost(t, "debian13")
		made, stopped := 0, 0
		p := playWith(h, h, cfg, tc.dryRun, func(o *fwscreen.Options) {
			o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
				made++
				c, cancel := context.WithCancel(ctx)
				return c, func() { stopped++; cancel() }
			}
		}, tc.lines...)
		if p.err != nil || made != tc.made || stopped != made {
			t.Errorf("%q: Run = %v, %d contexts made, %d stopped, want %d:\n%s", tc.lines, p.err, made, stopped, tc.made, p.out)
		}
	}
	h, cfg := screenHost(t, "debian13")
	p := playWith(h, h, cfg, false, func(o *fwscreen.Options) {
		o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
			c, cancel := context.WithCancel(ctx)
			cancel()
			return c, cancel
		}
	}, "1", "y", "0")
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err == nil || !h.missing || len(mainScreens(p.out)) != 2 {
		t.Errorf("cancelled: Run = %v, runs %+v, installed %t:\n%s", p.err, p.runs, !h.missing, p.out)
	}
}

// TestNavigation: 0 returns, q quits, and the end of the input aborts, from the
// screen, the profiles' screen, each question and Add's form, writing nothing; a
// key no item takes, item 2 with no ufw (origin/main's hidden item that still
// ran) and the profiles' hidden Remove redraw.
func TestNavigation(t *testing.T) {
	for _, tt := range []struct {
		host  string
		lines []string
		want  error
	}{
		{"debian13", []string{"0"}, nil}, {"debian13", []string{"q"}, nav.ErrQuit}, {"debian13", []string{"1", "q"}, nav.ErrQuit},
		{"ubuntu2604", []string{"1", "q"}, nav.ErrQuit}, {"profiles", []string{"2", "q"}, nav.ErrQuit}, {"profiles", []string{"2", "1", "q"}, nav.ErrQuit},
		{"profiles", []string{"2", "2", "q"}, nav.ErrQuit}, {"pve9", []string{"q"}, nav.ErrQuit},
		{"debian13", nil, prompt.ErrAborted}, {"debian13", []string{"1"}, prompt.ErrAborted}, {"ubuntu2604", []string{"1"}, prompt.ErrAborted},
		{"profiles", []string{"2"}, prompt.ErrAborted}, {"profiles", []string{"2", "1"}, prompt.ErrAborted}, {"profiles", []string{"2", "2"}, prompt.ErrAborted},
		{"pve9", nil, prompt.ErrAborted},
		{"debian13", []string{"9", "2", "0"}, nil}, {"active22", []string{"9", "2", "2", "3", "0", "0"}, nil}, {"profiles", []string{"2", "1", "0", "0", "0"}, nil},
	} {
		h, cfg := screenHost(t, tt.host)
		n := len(h.Calls())
		p := play(h, h, cfg, false, tt.lines...)
		if !errors.Is(p.err, tt.want) || (tt.want == nil) != (p.err == nil) || p.unread != 0 {
			t.Errorf("%s %q: Run = %v, want %v, %d unread:\n%s", tt.host, tt.lines, p.err, tt.want, p.unread, p.out)
		}
		if w := writes(h, n); len(w) > 0 {
			t.Errorf("%s %q: wrote %v", tt.host, tt.lines, w)
		}
	}
}

// TestRunNeeds: Run refuses options it cannot run with, writing nothing: no
// Prompter's In or Out, no System, Config or NewEngine.
func TestRunNeeds(t *testing.T) {
	h, cfg := screenHost(t, "debian13")
	n := len(h.Calls())
	var out bytes.Buffer
	in := prompt.New(strings.NewReader("0\n"), false)
	newEngine := func(func(engine.Event)) *engine.Engine { return nil }
	for _, o := range []fwscreen.Options{
		{}, {System: h, Config: cfg, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in}, System: h, Config: cfg, NewEngine: newEngine},
		{Prompter: nav.Prompter{Out: &out}, System: h, Config: cfg, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, Config: cfg, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, System: h, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, System: h, Config: cfg},
	} {
		if err := fwscreen.Run(context.Background(), o); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Run(%+v) = %v, want fs.ErrInvalid", o, err)
		}
	}
	if out.Len() > 0 || len(h.Calls()) != n {
		t.Errorf("wrote %q, %v", out.String(), h.Calls()[n:])
	}
}

// escapes are the terminal's colour and style sequences.
var escapes = regexp.MustCompile("\x1b\\[[0-9;]*m")

// content is hardn.yml's content on h.
func content(h *fwHost) string {
	e, ok := h.Entry(yml)
	if !ok {
		return "<absent>"
	}
	return string(e.Data)
}
