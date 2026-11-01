package main

import (
	"bytes"
	"context"
	"errors"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The Firewall mocks (firewall.go, firewallhost.go; slice 4, unit (a)).
// Nothing here runs a command or writes a file outside the fake.

// TestFirewallGolden pins each listing mock on the core set and under
// LANG=C. The golden files are testdata/golden/firewall/<mock>.golden.
func TestFirewallGolden(t *testing.T) { goldenListing(t, fwListing) }

// TestPlayFirewallGolden plays the mocks end to end and pins each transcript.
// Each change id's random suffix is written xxxxxxxx (sessionIDs).
func TestPlayFirewallGolden(t *testing.T) {
	addAnswers := []string{"2", "1", "WebServer", "Web Server", "A web server", "80/tcp,443/tcp", "y", "0", "0"}
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"firewall-install", []string{"firewall/debian13"}, []string{"1", "y", "0"}, append(uitest.Core(), asciiColour)},
		{"firewall-install-alpine", []string{"firewall/alpine324"}, []string{"1", "y", "0"}, nil},
		{"firewall-enable", []string{"firewall/ubuntu2604"}, []string{"1", hostName, "0"}, append(uitest.Core(), asciiColour)},
		{"firewall-disable", []string{"firewall/active22"}, []string{"1", "y", "0"}, nil},
		{"firewall-allow-policy", []string{"firewall/allow-policy"}, []string{"1", hostName, "0"}, nil},
		{"firewall-lockout", []string{"firewall/lockout"}, []string{"1", hostName, "0"}, nil},
		{"firewall-lockout-at-start", []string{"firewall/lockout-at-start"}, []string{"1", "0"}, nil},
		{"firewall-blocklist", []string{"firewall/blocklist"}, []string{"1", "0"}, nil},
		{"firewall-edit", []string{"firewall/edit-2230"}, []string{"1", hostName, "0"}, nil},
		{"firewall-runlevel", []string{"firewall/alpine-runlevel"}, []string{"1", "y", "0"}, nil},
		{"firewall-pve9", []string{"firewall/pve9"}, []string{"0"}, nil},
		{"firewall-pve9-drop", []string{"firewall/pve9-drop"}, []string{"0"}, nil},
		{"firewall-profile-add", []string{"firewall/profiles"}, addAnswers, nil},
		{"firewall-profile-remove", []string{"firewall/profiles"}, []string{"2", "2", "1", "y", "0", "0"}, nil},
		{"firewall-profile-apply", []string{"firewall/profiles"}, []string{"2", "3", "y", "0", "0"}, nil},
		{"firewall-plan-only-enable", []string{"-sequence", "dry-run", "firewall/ubuntu2604"}, []string{"1", "0"}, nil},
		{"firewall-plan-only-install", []string{"-sequence", "dry-run", "firewall/debian13"}, []string{"1", "0"}, nil},
		{"firewall-plan-only-disable", []string{"-sequence", "dry-run", "firewall/active22"}, []string{"1", "0"}, nil},
		{"firewall-plan-only-profile", []string{"-sequence", "dry-run", "firewall/profiles"}, []string{"2", "3", "0", "0"}, nil},
		{"firewall-hosts", []string{"firewall"}, []string{"3", "0", "0"}, nil},
	}
	for _, tt := range tests {
		cells := tt.cells
		if cells == nil {
			cells = uitest.Core()
		}
		uitest.Golden(t, "play/"+tt.name, cells, func(env term.Env) string {
			args := append(lookArgs(t, env), tt.args...)
			code, out, errs, unread := playGallery(tt.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread", tt.name, args, code, errs, unread)
			}
			return sessionIDs(t, out)
		})
	}
}

// fwHostNamed is the host named name.
func fwHostNamed(t *testing.T, name string) fwHost {
	t.Helper()
	fh, ok := findFWHost(name)
	if !ok {
		t.Fatalf("no firewall host %q", name)
	}
	return fh
}

// readFWHost reads the host named name, fresh, as the screen does, and
// returns the reading and the host.
func readFWHost(t *testing.T, name string) (fwReading, *fakeHost) {
	t.Helper()
	h := fwHostNamed(t, name).host()
	r, err := (&session{}).readFW(context.Background(), h.system(), fwSession(h))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return r, h
}

// playFW plays the screen on h, fh's host, plain, the user typing lines, a dry
// run with dryRun.
func playFW(t *testing.T, fh fwHost, h *fakeHost, dryRun bool, lines ...string) (*session, string, int) {
	t.Helper()
	return playWith(t, fwPrefix+"/"+fh.name, func(s *session) func() error {
		if dryRun {
			s.sequence = "dry-run"
		}
		return func() error { return s.fwScreen(context.Background(), fh, h) }
	}, lines...)
}

// rowOf is the row whose key is key among rows.
func rowOf(rows []component.KeyValue, key string) component.KeyValue {
	for _, kv := range rows {
		if kv.Key == key {
			return kv
		}
	}
	return component.KeyValue{}
}

// TestFirewallHosts holds each host's screen to the real readers: the row
// firewall.Incoming.Check makes, on the main menu's own mapping; ufw.Read's
// state or pve.ReadFirewall's; item 1; whether it refuses before asking, and
// with which real error; and whether its plan is lockout-risk.
func TestFirewallHosts(t *testing.T) {
	tests := []struct {
		host    string
		state   component.StatusState
		detail  string
		first   fwAction
		refused error // errors.Is the plan's; ErrBlocks through BlockError
		risky   bool
	}{
		{host: "debian13", state: component.StatusNotConfigured, detail: "ufw not installed", first: fwDoInstall},
		{host: "ubuntu2604", state: component.StatusNotConfigured, detail: "inactive", first: fwDoEnable, risky: true},
		{host: "active22", state: component.StatusConfigured, detail: "deny policy", first: fwDoDisable},
		{host: "hardened", state: component.StatusConfigured, detail: "deny policy", first: fwDoDisable},
		{host: "allow-policy", state: component.StatusPartial, detail: firewall.DetailAllowPolicy, first: fwDoEnable, risky: true},
		{host: "lockout", state: component.StatusPartial, detail: "ssh blocked", first: fwDoEnable, risky: true},
		{host: "lockout-at-start", state: component.StatusNotConfigured, detail: "enabled, not running", first: fwDoEnable, refused: ufw.ErrEnabledMismatch},
		{host: "allowlist", state: component.StatusPartial, detail: "ssh allowlist", first: fwDoDisable},
		{host: "blocklist", state: component.StatusNotConfigured, detail: "inactive", first: fwDoEnable, refused: ufw.ErrBlocks},
		{host: "edit-2230", state: component.StatusNotConfigured, detail: "inactive", first: fwDoEnable, risky: true},
		{host: "alpine324", state: component.StatusNotConfigured, detail: "ufw not installed", first: fwDoInstall},
		{host: "alpine-runlevel", state: component.StatusConfigured, detail: "deny policy", first: fwDoEnable},
		{host: "pve9", state: component.StatusNotConfigured, detail: "disabled", first: fwDoNone},
		{host: "pve9-drop", state: component.StatusConfigured, detail: "drop policy", first: fwDoNone},
		{host: "profiles", state: component.StatusConfigured, detail: "deny policy", first: fwDoDisable},
	}
	if len(tests) != len(fwHosts()) {
		t.Fatalf("%d hosts tested, %d hosts", len(tests), len(fwHosts()))
	}
	for _, tt := range tests {
		r, h := readFWHost(t, tt.host)
		if len(r.status.Rows) != 1 || r.status.Rows[0].State != tt.state || r.status.Rows[0].Detail != tt.detail {
			t.Errorf("%s: the row %+v, want %v %q", tt.host, r.status.Rows, tt.state, tt.detail)
		}
		// The row is the check's, as the main menu reads it.
		st, err := (&session{}).readStatus(context.Background(), h)
		if err != nil {
			t.Fatalf("%s: %v", tt.host, err)
		}
		if got := st.mainMenu().Status.Rows; !slices.ContainsFunc(got, func(row component.StatusRow) bool {
			return row.Label == "Firewall" && row.State == tt.state && row.Detail == tt.detail
		}) {
			t.Errorf("%s: the main menu's rows %+v", tt.host, got)
		}
		if got := r.first(); got != tt.first {
			t.Errorf("%s: item 1 %v, want %v", tt.host, got, tt.first)
		}
		if (r.refusal() != nil) != (tt.refused != nil) || tt.refused != nil && !errors.Is(r.planErr, tt.refused) {
			t.Errorf("%s: refused %v, plan's error %v, want %v", tt.host, r.refusal() != nil, r.planErr, tt.refused)
		}
		if r.risky() != tt.risky {
			t.Errorf("%s: lockout risk %v", tt.host, r.risky())
		}
	}
}

// TestFirewallRows holds what the sections say to the readers: never
// hardn.yml's sshPort, every listener with ufw's verdict from ufw.Evaluate,
// the rules from ufw status verbose or, inactive, its files; none a muted item.
func TestFirewallRows(t *testing.T) {
	r, _ := readFWHost(t, "hardened")
	rows := r.sshRows()
	if len(rows) != 2 || rows[0].Key != "[::]:2230" || rows[0].Value != "Allowed" || rows[1].Key != "0.0.0.0:2230" {
		t.Errorf("hardened: SSH rows %+v", rows)
	}
	r, _ = readFWHost(t, "lockout")
	if rows := r.sshRows(); rows[0].Value != "Refused" || rows[0].State != component.ValueBad {
		t.Errorf("lockout: SSH rows %+v", rows)
	}
	r, _ = readFWHost(t, "lockout-at-start")
	if rows := r.sshRows(); rows[0].Value != "Refused at start" {
		t.Errorf("lockout-at-start: SSH rows %+v", rows)
	}
	r, _ = readFWHost(t, "edit-2230")
	rows = r.sshRows()
	if rowOf(rows, "0.0.0.0:2230").Note != "Not bound" || rowOf(rows, "0.0.0.0:22").Note != "Not configured" {
		t.Errorf("edit-2230: SSH rows %+v", rows)
	}
	r, _ = readFWHost(t, "ubuntu2604")
	if rows := r.ruleRows(); len(rows) != 1 || rows[0].Key != fwNoRules || rows[0].State != component.ValueNone {
		t.Errorf("no rule: %+v", rows)
	}
	r, _ = readFWHost(t, "blocklist")
	rows = r.ruleRows()
	if rows[0].Key != "22/tcp from 198.51.100.0/24" || rows[0].Value != "Deny" || rows[0].State != component.ValueBad || rows[0].Note != "Saved" || rows[1].State != component.ValueOK {
		t.Errorf("blocklist: rule rows %+v", rows)
	}
	r, _ = readFWHost(t, "alpine-runlevel")
	if boot := rowOf(r.ufwRows(), fwKeyBoot); boot.Value != "In no runlevel" || boot.State != component.ValueWarn {
		t.Errorf("alpine-runlevel: at boot %+v", boot)
	}
	r, _ = readFWHost(t, "pve9-drop")
	vm := r.subMenu()
	if len(vm.Menu.Items) != 0 || !strings.Contains(flatText(vm.Render(component.NewCtx(term.Fixed(200, colorprofile.NoTTY, false, true)))), fwNoChange) {
		t.Errorf("pve9-drop: items %+v", vm.Menu.Items)
	}
	// hardn.yml's sshPort is not read: a port there changes no row.
	h := statusOn(debian13Platform).withHardnYML(fwYML + "sshPort: 2230\n")
	ufwOn(h)
	r, err := (&session{}).readFW(context.Background(), h.system(), fwSession(h))
	if err != nil {
		t.Fatal(err)
	}
	if rows := r.sshRows(); len(rows) != 2 || rows[1].Key != "0.0.0.0:22" {
		t.Errorf("sshPort 2230: SSH rows %+v", rows)
	}
}

// fwCommands are the commands of a plan's changes, the controls': the engine's
// own, the boot unit's, come with any lockout-risk plan (P-34).
func fwCommands(changes []engine.Change) []string {
	var out []string
	for _, c := range changes {
		if c.Stage != engine.StageBoot {
			out = append(out, c.Commands...)
		}
	}
	return out
}

// fwPlan plans action on h on the real engine, as the screen's changes are
// planned, with h's session.
func fwPlan(t *testing.T, h *fakeHost, action fwAction) (*engine.Engine, *engine.Plan) {
	t.Helper()
	e := engine.New(h.system(), engine.Options{Revert: revert.Options{Now: clock, Hardn: galleryHardn}, Session: fwSession(h),
		Backups: backup.Settings{Path: backup.DefaultPath, Enabled: true}, Hold: (&session{}).hold, PID: applyPID})
	cfg, err := statusConfig(context.Background(), h.system())
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.Plan(context.Background(), []engine.Control{fwControl{action: action}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e, p
}

// TestFirewallPlans holds each plan's commands: Enable's are the real plan's;
// no ufw disable or ufw reset in any plan but Disable's, whose one command is
// ufw disable; Install by the host's package tool; the runlevel on Alpine;
// the session's own port kept.
func TestFirewallPlans(t *testing.T) {
	want := []struct {
		host   string
		action fwAction
		cmds   []string
		risk   engine.RiskClass
	}{
		{"debian13", fwDoInstall, []string{"apt-get install -y ufw"}, engine.RiskPlain},
		{"alpine324", fwDoInstall, []string{"apk add ufw"}, engine.RiskPlain},
		{"ubuntu2604", fwDoEnable, []string{"ufw allow 22/tcp", "ufw --force enable"}, engine.RiskLockout},
		{"allow-policy", fwDoEnable, []string{"ufw allow 22/tcp", "ufw default deny incoming"}, engine.RiskLockout},
		{"lockout", fwDoEnable, []string{"ufw allow 2230/tcp"}, engine.RiskLockout},
		{"alpine-runlevel", fwDoEnable, []string{"rc-update add ufw default"}, engine.RiskPlain},
		{"active22", fwDoDisable, []string{"ufw disable"}, engine.RiskPlain},
		{"profiles", fwDoApply, []string{"ufw allow from any to any app LabHTTPS", "ufw allow from any to any app LabDNS"}, engine.RiskPlain},
	}
	for _, tt := range want {
		h := fwHostNamed(t, tt.host).host()
		_, p := fwPlan(t, h, tt.action)
		if p.Err() != nil || !slices.Equal(fwCommands(p.Changes), tt.cmds) || p.Risk != tt.risk {
			t.Errorf("%s: %v, commands %q, risk %v; want %q, %v", tt.host, p.Err(), fwCommands(p.Changes), p.Risk, tt.cmds, tt.risk)
		}
	}
	// Every plan the screen can make, on every host: ufw disable in Disable's
	// alone, ufw reset in none.
	for _, fh := range fwHosts() {
		for _, action := range []fwAction{fwDoInstall, fwDoEnable, fwDoApply} {
			r, _ := readFWHost(t, fh.name)
			if r.onPVE {
				continue // TestFirewallProxmoxNoUFW
			}
			h := fh.host()
			e := engine.New(h.system(), engine.Options{Revert: revert.Options{Now: clock, Hardn: galleryHardn}, Session: fwSession(h),
				Backups: backup.Settings{Path: backup.DefaultPath, Enabled: true}, Hold: (&session{}).hold, PID: applyPID})
			cfg, _ := statusConfig(context.Background(), h.system())
			p, err := e.Plan(context.Background(), []engine.Control{fwControl{action: action}}, cfg)
			if err != nil {
				continue // refused
			}
			for _, c := range fwCommands(p.Changes) {
				if strings.Contains(c, "ufw disable") || strings.Contains(c, "ufw reset") {
					t.Errorf("%s, action %v: %q", fh.name, action, c)
				}
			}
		}
	}
}

// TestFirewallApplies applies each stand-in through the engine on the model
// and reads the host again: Install leaves ufw inactive, Enable active with
// the listener let through, Disable inactive with its rules kept, the runlevel
// added; Add and Remove save hardn.yml alone; Apply writes the profile file
// and its rules.
func TestFirewallApplies(t *testing.T) {
	apply := func(name string, action fwAction) *fakeHost {
		t.Helper()
		h := fwHostNamed(t, name).host()
		e, p := fwPlan(t, h, action)
		if p.Err() != nil {
			t.Fatalf("%s: %v", name, p.Err())
		}
		if _, err := e.Apply(context.Background(), p); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return h
	}
	read := func(h *fakeHost) fwReading {
		t.Helper()
		r, err := (&session{}).readFW(context.Background(), h.system(), fwSession(h))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := read(apply("debian13", fwDoInstall)); !r.st.Installed || r.st.Status.Active || r.first() != fwDoEnable {
		t.Errorf("install: %+v, item 1 %v", r.st.Status, r.first())
	}
	if r := read(apply("ubuntu2604", fwDoEnable)); !r.st.Status.Active || r.first() != fwDoDisable || r.status.Rows[0].Detail != "deny policy" {
		t.Errorf("enable: %+v, %q", r.st.Status, r.status.Rows[0].Detail)
	}
	if r := read(apply("lockout", fwDoEnable)); r.first() != fwDoDisable || r.sshRows()[0].Value != "Allowed" {
		t.Errorf("lockout: %+v", r.sshRows())
	}
	if r := read(apply("active22", fwDoDisable)); r.st.Status.Active || r.st.Enabled || len(r.st.Rules) != 2 || r.first() != fwDoEnable {
		t.Errorf("disable: active %v, enabled %v, rules %d", r.st.Status.Active, r.st.Enabled, len(r.st.Rules))
	}
	if r := read(apply("alpine-runlevel", fwDoEnable)); r.noRunlevel || r.first() != fwDoDisable {
		t.Errorf("runlevel: %v, item 1 %v", r.noRunlevel, r.first())
	}
	h := apply("profiles", fwDoApply)
	if !strings.Contains(h.content(fwProfileFile), "[LabHTTPS]\ntitle=Lab Web Server (HTTPS)\n") {
		t.Errorf("apply: %q", h.content(fwProfileFile))
	}
	if r := read(h); !r.applied("LabHTTPS") || !r.applied("LabDNS") || r.profileRows()[0].Note != "Applied" {
		t.Errorf("apply: %+v", r.profileRows())
	}
	if _, p := fwPlan(t, h, fwDoApply); p.Err() != nil || len(p.Changes) != 0 {
		t.Errorf("apply again: %v, %d changes", p.Err(), len(p.Changes))
	}
}

// TestFirewallProfilesSave: Add and Remove, through the flow, change hardn.yml
// alone; Add saves all four fields; bad answers are asked again.
func TestFirewallProfilesSave(t *testing.T) {
	fh := fwHostNamed(t, "profiles")
	h := fh.host()
	n := len(h.f.Calls())
	_, out, code := playFW(t, fh, h, false, "2", "1", "LabDNS", "bad name", "Web", "Web Server", "A web server", "80", "80/tcp,443/tcp", "y", "0", "0")
	if code != exitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	text := flatText(out)
	for _, want := range []string{"A profile with this name already exists.", "Use letters, digits", "Give each port with its protocol"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q:\n%s", want, text)
		}
	}
	if _, files := changed(h.f.Calls()[n:]); !slices.Equal(files, []string{hardnYML}) {
		t.Errorf("changed %q", files)
	}
	cfg, err := config.Read(context.Background(), h.system(), hardnYML)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.UfwAppProfiles) != 3 || cfg.UfwAppProfiles[2].Name != "Web" || cfg.UfwAppProfiles[2].Description != "A web server" || !slices.Equal(cfg.UfwAppProfiles[2].Ports, []string{"80/tcp", "443/tcp"}) {
		t.Errorf("profiles %+v", cfg.UfwAppProfiles)
	}

	h = fh.host()
	if _, out, code = playFW(t, fh, h, false, "2", "2", "1", "y", "0", "0"); code != exitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(flatText(out), fwRemoveNotice) {
		t.Errorf("no notice:\n%s", out)
	}
	cfg, err = config.Read(context.Background(), h.system(), hardnYML)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.UfwAppProfiles) != 1 || cfg.UfwAppProfiles[0].Name != "LabDNS" {
		t.Errorf("after remove: %+v", cfg.UfwAppProfiles)
	}
}

// TestFirewallProxmoxNoUFW: on Proxmox VE, however it is played, no ufw call
// of any kind, no read under /etc/ufw, no item but Return.
func TestFirewallProxmoxNoUFW(t *testing.T) {
	for _, name := range []string{"pve9", "pve9-drop"} {
		for _, dry := range []bool{false, true} {
			fh := fwHostNamed(t, name)
			h := fh.host()
			n := len(h.f.Calls())
			_, out, code := playFW(t, fh, h, dry, "1", "2", "0")
			if code != exitOK {
				t.Fatalf("%s: exit %d:\n%s", name, code, out)
			}
			for _, c := range h.f.Calls()[n:] {
				if c.Op == "run" && path.Base(c.Argv[0]) == "ufw" || strings.HasPrefix(c.Path, "/etc/ufw") || strings.HasPrefix(c.Path, "/etc/default/ufw") {
					t.Errorf("%s: a ufw call: %s", name, c)
				}
				if c.Op == "run" && (path.Base(c.Argv[0]) == "apt-get" || path.Base(c.Argv[0]) == "pve-firewall") {
					t.Errorf("%s: %s", name, c)
				}
			}
			if cmds, files := changed(h.f.Calls()[n:]); len(cmds) > 0 || len(files) > 0 {
				t.Errorf("%s: changed %q, %q", name, cmds, files)
			}
		}
	}
	// Whether it runs Proxmox VE not established: no ufw call either.
	h := fwProxmoxUnknown()
	n := len(h.f.Calls())
	r, err := (&session{}).readFW(context.Background(), h.system(), fwSession(h))
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(r.proxmox, pve.ErrProxmoxUnknown) || r.refusal() == nil {
		t.Errorf("not established: %v, refusal %v", r.proxmox, r.refusal())
	}
	for _, c := range h.f.Calls()[n:] {
		if c.Op == "run" && path.Base(c.Argv[0]) == "ufw" {
			t.Errorf("not established: a ufw call: %s", c)
		}
	}
}

// TestFirewallRefusals: each refusal comes from the real error, is said before
// asking, and builds no engine.
func TestFirewallRefusals(t *testing.T) {
	for _, rh := range fwRefusalHosts() {
		h := rh.host()
		r, err := (&session{}).readFW(context.Background(), h.system(), fwSession(h))
		if err != nil {
			t.Fatalf("%s: %v", rh.name, err)
		}
		if r.refusal() == nil {
			t.Errorf("%s: no refusal", rh.name)
		}
		s, out, code := playFW(t, fwHost{name: "refusal", host: rh.host}, h, false, "1", "0")
		if code != exitOK || len(s.engineRuns) != 0 {
			t.Errorf("%s: exit %d, %d engines:\n%s", rh.name, code, len(s.engineRuns), out)
		}
	}
	r := func(h *fakeHost) fwReading {
		t.Helper()
		got, err := (&session{}).readFW(context.Background(), h.system(), fwSession(h))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := r(fwNoSS()); !errors.Is(got.planErr, ufw.ErrListenersUnknown) || !got.sk.noSS {
		t.Errorf("no ss: %v", got.planErr)
	}
	if got := r(fwGarbled()); !errors.Is(got.planErr, ufw.ErrStateUnknown) {
		t.Errorf("garbled: %v", got.planErr)
	}
	if got := r(fwSavedDeny()); !errors.Is(got.planErr, ufw.ErrBlocks) {
		t.Errorf("saved deny: %v", got.planErr)
	}
}

// TestFirewallStaleListener is the finding for unit (b): on an edit to 2230 not
// reloaded, sshd still on 22, the real PlanUFWEnable reads sshd -T alone. With
// an SSH session on 22 it keeps 22 as the session's port; from a console it
// does not, so the listener that accepts connections now is refused once ufw
// starts, where the Firewall row's reading treats it as a candidate.
func TestFirewallStaleListener(t *testing.T) {
	ctx := context.Background()
	plan := func(s revert.Session) []string {
		h := fwEdit()
		f, err := detect.Detect(ctx, h.system())
		if err != nil {
			t.Fatal(err)
		}
		changes, err := steps.PlanUFWEnable(ctx, h.system(), engine.Input{Facts: f, Session: s}, fwID)
		if err != nil {
			t.Fatal(err)
		}
		return fwCommands(changes)
	}
	if got := plan(applySession); !slices.Equal(got, []string{"ufw allow 22/tcp", "ufw allow 2230/tcp", "ufw --force enable"}) {
		t.Errorf("with a session: %q", got)
	}
	if got := plan(revert.Session{}); !slices.Equal(got, []string{"ufw allow 2230/tcp", "ufw --force enable"}) {
		t.Errorf("from a console: %q", got)
	}
	// The row's reading has 22 among what must be let through.
	h := fwEdit()
	f, err := detect.Detect(ctx, h.system())
	if err != nil {
		t.Fatal(err)
	}
	ls, ok := readFWSockets(ctx, h.system(), f).listeners()
	if !ok || !slices.Contains(ls.Ports(), 22) || !slices.Contains(ls.Ports(), 2230) {
		t.Errorf("the row's listeners: %v", ls.Ports())
	}
}

// TestFirewallPlanOnlyWritesNothing: on every host, each item under plan only
// opens as live and stops at its plan, profiles too: nothing changes outside
// hardn's own state, no command runs that changes the host.
func TestFirewallPlanOnlyWritesNothing(t *testing.T) {
	stops := func(out string) bool {
		text := flatText(out)
		return strings.Contains(text, "No actual changes were made") || strings.Contains(text, "Nothing needs changing.")
	}
	for _, fh := range fwHosts() {
		h := fh.host()
		n := len(h.f.Calls())
		r, _ := readFWHost(t, fh.name)
		var lines []string
		switch {
		case r.onPVE:
			lines = []string{"0"}
		case !r.st.Installed:
			lines = []string{"1", "0"}
		case r.first() == fwDoEnable && r.refusal() != nil:
			lines = []string{"1", "0"}
		default:
			lines = []string{"1", "0"}
		}
		_, out, code := playFW(t, fh, h, true, lines...)
		if code != exitOK {
			t.Errorf("%s: exit %d:\n%s", fh.name, code, out)
		}
		if !r.onPVE && r.refusal() == nil && !stops(out) {
			t.Errorf("%s: plan only did not stop at the plan:\n%s", fh.name, out)
		}
		if cmds, files := changed(h.f.Calls()[n:]); len(cmds) > 0 || len(files) > 0 {
			t.Errorf("%s: plan only changed the host: %q, %q", fh.name, cmds, files)
		}
	}
	fh := fwHostNamed(t, "profiles")
	for _, lines := range [][]string{
		{"2", "1", "Web", "Web Server", "A web server", "80/tcp", "0", "0"},
		{"2", "2", "1", "0", "0"},
		{"2", "3", "0", "0"},
	} {
		h := fh.host()
		n := len(h.f.Calls())
		_, out, code := playFW(t, fh, h, true, lines...)
		if code != exitOK || !stops(out) {
			t.Errorf("%v: exit %d:\n%s", lines, code, out)
		}
		if cmds, files := changed(h.f.Calls()[n:]); len(cmds) > 0 || len(files) > 0 {
			t.Errorf("%v: changed %q, %q", lines, cmds, files)
		}
	}
}

// TestFirewallHeights: the screen on every host and the sub-screen, at 80x24
// and at 80x40, in Unicode and ASCII: each is as many lines at either height,
// the prompt on its last. Those over 24 rows are the page's §4.
func TestFirewallHeights(t *testing.T) {
	want := map[string]int{"debian13": 20, "ubuntu2604": 29, "active22": 29, "hardened": 29, "allow-policy": 29, "lockout": 30,
		"lockout-at-start": 29, "allowlist": 31, "blocklist": 33, "edit-2230": 31, "alpine324": 20, "alpine-runlevel": 29,
		"pve9": 16, "pve9-drop": 16, "profiles": 34, "sub-profiles": 16}
	draws := map[string]func(component.Ctx) string{}
	for _, fh := range fwHosts() {
		r, _ := readFWHost(t, fh.name)
		draws[fh.name] = func(c component.Ctx) string { return r.subMenu().Render(c) }
	}
	r, _ := readFWHost(t, "profiles")
	draws["sub-profiles"] = func(c component.Ctx) string { return r.profilesMenu().Render(c) }
	if len(draws) != len(want) {
		t.Fatalf("%d drawings, %d heights", len(draws), len(want))
	}
	for name, draw := range draws {
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
				env.Height = rows
				lines := strings.Split(ansi.Strip(draw(component.NewCtx(env))), "\n")
				if len(lines) != want[name] || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s at 80x%d, unicode %v: %d lines, the last %q; want %d, the prompt", name, rows, unicode, len(lines), lines[len(lines)-1], want[name])
				}
			}
		}
	}
}

// TestFirewallUsage: the listing and play refuse what they do not take, with
// status 2 and nothing on stdout; the listing lists with no input; the main
// menu's 4 leads to debian13's.
func TestFirewallUsage(t *testing.T) {
	for _, args := range [][]string{
		{fwPrefix, "nope"},
		{fwPrefix, "screen-debian13", "-width", "60"},
		{"play", "firewall/nope"},
		{"play", "-fail", "1", "firewall/debian13"},
		{"play", "-sequence", "failure", "firewall/debian13"},
	} {
		var out, errs bytes.Buffer
		if code := run(args, nil, offTerminal, &out, &errs); code != exitUsage || out.Len() > 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errs.String())
		}
	}
	var out bytes.Buffer
	if code := run([]string{fwPrefix, "-list"}, nil, offTerminal, &out, &bytes.Buffer{}); code != exitOK || len(strings.Fields(out.String())) != len(fwMocks()) {
		t.Errorf("-list: exit %d, %q", code, out.String())
	}
	if fwLinks[mainMenuMock+"/4"] != fwMainHost {
		t.Errorf("the main menu's 4 leads to %q", fwLinks[mainMenuMock+"/4"])
	}
}
