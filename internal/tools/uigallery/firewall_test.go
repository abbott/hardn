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
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/packages"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The Firewall mocks on the real control (firewall.go, firewallhost.go; slice 4).
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
		{"firewall-alpine-stock", []string{"firewall/alpine-stock"}, []string{"1", "0"}, nil},
		{"firewall-enable", []string{"firewall/ubuntu2604"}, []string{"1", hostName, "0"}, append(uitest.Core(), asciiColour)},
		{"firewall-disable", []string{"firewall/active22"}, []string{"1", "y", "0"}, nil},
		{"firewall-allow-policy", []string{"firewall/allow-policy"}, []string{"1", hostName, "0"}, nil},
		{"firewall-lockout", []string{"firewall/lockout"}, []string{"1", hostName, "0"}, nil},
		{"firewall-lockout-at-start", []string{"firewall/lockout-at-start"}, []string{"1", "0"}, nil},
		{"firewall-blocklist", []string{"firewall/blocklist"}, []string{"1", "0"}, nil},
		{"firewall-edit", []string{"firewall/edit-2230"}, []string{"1", "0"}, nil},
		{"firewall-moved", []string{"firewall/moved-2230"}, []string{"1", "3", "y", "0"}, nil},
		{"firewall-denied", []string{"firewall/denied"}, []string{"1", "0"}, nil},
		{"firewall-unit-off", []string{"firewall/unit-off"}, []string{"1", "y", "0"}, nil},
		{"firewall-lockout-disable", []string{"firewall/lockout"}, []string{"3", "y", "0"}, nil},
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
		first   firewall.Item
		refused error // errors.Is the plan's; ErrBlocks through BlockError
		risky   bool
	}{
		{host: "debian13", state: component.StatusNotConfigured, detail: "ufw not installed", first: firewall.ItemInstall},
		{host: "ubuntu2604", state: component.StatusNotConfigured, detail: "inactive", first: firewall.ItemEnable, risky: true},
		{host: "active22", state: component.StatusConfigured, detail: "deny policy", first: firewall.ItemDisable},
		{host: "hardened", state: component.StatusConfigured, detail: "deny policy", first: firewall.ItemDisable},
		{host: "allow-policy", state: component.StatusPartial, detail: firewall.DetailAllowPolicy, first: firewall.ItemEnable, risky: true},
		{host: "lockout", state: component.StatusPartial, detail: "ssh blocked", first: firewall.ItemEnable, risky: true},
		{host: "lockout-at-start", state: component.StatusNotConfigured, detail: "enabled, not running", first: firewall.ItemEnable, refused: ufw.ErrEnabledMismatch},
		{host: "allowlist", state: component.StatusPartial, detail: "ssh allowlist", first: firewall.ItemDisable},
		{host: "blocklist", state: component.StatusNotConfigured, detail: "inactive", first: firewall.ItemEnable, refused: ufw.ErrBlocks},
		{host: "edit-2230", state: component.StatusNotConfigured, detail: "inactive", first: firewall.ItemEnable, refused: firewall.ErrNotInEffect},
		{host: "moved-2230", state: component.StatusUnknown, detail: "ssh config not in effect", first: firewall.ItemEnable, refused: firewall.ErrNotInEffect},
		{host: "denied", state: component.StatusPartial, detail: "ssh blocked", first: firewall.ItemEnable, refused: firewall.ErrRefused},
		{host: "alpine324", state: component.StatusNotConfigured, detail: "ufw not installed", first: firewall.ItemInstall},
		{host: "alpine-stock", state: component.StatusNotConfigured, detail: "ufw not installed", first: firewall.ItemInstall, refused: packages.ErrNotInRepos},
		{host: "alpine-runlevel", state: component.StatusPartial, detail: "off after restart", first: firewall.ItemEnable},
		{host: "unit-off", state: component.StatusPartial, detail: "off after restart", first: firewall.ItemEnable},
		{host: "pve9", state: component.StatusNotConfigured, detail: "disabled", first: firewall.ItemNone},
		{host: "pve9-drop", state: component.StatusConfigured, detail: "drop policy", first: firewall.ItemNone},
		{host: "profiles", state: component.StatusConfigured, detail: "deny policy", first: firewall.ItemDisable},
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
		if got := r.First; got != tt.first {
			t.Errorf("%s: item 1 %v, want %v", tt.host, got, tt.first)
		}
		if (r.refusal() != nil) != (tt.refused != nil) || tt.refused != nil && !errors.Is(r.PlanErr, tt.refused) {
			t.Errorf("%s: refused %v, plan's error %v, want %v", tt.host, r.refusal() != nil, r.PlanErr, tt.refused)
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
	if rows[0].Key != "22/tcp from 198.51.100.0/24" || rows[0].Value != "Deny" || rows[0].State != component.ValueBad || rows[0].Note != "Saved" || rows[1].State != component.ValuePlain {
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
func fwPlan(t *testing.T, h *fakeHost, action firewall.Action) (*engine.Engine, *engine.Plan) {
	t.Helper()
	e := engine.New(h.system(), engine.Options{Revert: revert.Options{Now: clock, Hardn: galleryHardn}, Session: fwSession(h),
		Backups: backup.Settings{Path: backup.DefaultPath, Enabled: true}, Hold: (&session{}).hold, PID: applyPID})
	cfg, err := statusConfig(context.Background(), h.system())
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.Plan(context.Background(), []engine.Control{firewall.Incoming{Action: action}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e, p
}

// TestFirewallBootRow: At boot says a masked or missing service as what it is,
// not as one that is only disabled.
func TestFirewallBootRow(t *testing.T) {
	for unit, want := range map[string]string{"disabled": "Service disabled", "linked": "Service disabled", "masked": "Service masked", "masked-runtime": "Service masked", "not-found": "No service"} {
		r := fwReading{Reading: firewall.Reading{Boot: detect.No, BootUnit: unit, State: ufw.State{Installed: true, Enabled: true}}}
		if i := slices.IndexFunc(r.ufwRows(), func(k component.KeyValue) bool { return k.Key == fwKeyBoot }); i < 0 || r.ufwRows()[i].Value != want {
			t.Errorf("%s: %+v", unit, r.ufwRows())
		}
	}
}

// TestFirewallPlans holds each plan's commands: Enable's are the real plan's;
// no ufw disable or ufw reset in any plan but Disable's, whose one command is
// ufw disable; Install by the host's package tool; the runlevel on Alpine;
// the session's own port kept.
func TestFirewallPlans(t *testing.T) {
	want := []struct {
		host   string
		action firewall.Action
		cmds   []string
		risk   engine.RiskClass
	}{
		{"debian13", firewall.ActionInstall, []string{"apt-get install -y ufw"}, engine.RiskPackageInstall},
		{"alpine324", firewall.ActionInstall, []string{"apk add ufw"}, engine.RiskPackageInstall},
		{"ubuntu2604", firewall.ActionEnable, []string{"ufw allow 22/tcp", "ufw --force enable"}, engine.RiskLockout},
		{"allow-policy", firewall.ActionEnable, []string{"ufw allow 22/tcp", "ufw default deny incoming"}, engine.RiskLockout},
		{"lockout", firewall.ActionEnable, []string{"ufw allow 2230/tcp"}, engine.RiskLockout},
		{"alpine-runlevel", firewall.ActionEnable, []string{"rc-update add ufw default"}, engine.RiskPlain},
		{"unit-off", firewall.ActionEnable, []string{"systemctl enable ufw.service"}, engine.RiskPlain},
		{"active22", firewall.ActionDisable, []string{"ufw disable"}, engine.RiskPlain},
		{"profiles", firewall.ActionApplyProfiles, []string{"ufw allow from any to any app LabHTTPS", "ufw allow from any to any app LabDNS"}, engine.RiskPlain},
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
		for _, action := range []firewall.Action{firewall.ActionInstall, firewall.ActionEnable, firewall.ActionApplyProfiles} {
			r, _ := readFWHost(t, fh.name)
			if r.OnPVE {
				continue // TestFirewallProxmoxNoUFW
			}
			h := fh.host()
			e := engine.New(h.system(), engine.Options{Revert: revert.Options{Now: clock, Hardn: galleryHardn}, Session: fwSession(h),
				Backups: backup.Settings{Path: backup.DefaultPath, Enabled: true}, Hold: (&session{}).hold, PID: applyPID})
			cfg, _ := statusConfig(context.Background(), h.system())
			p, err := e.Plan(context.Background(), []engine.Control{firewall.Incoming{Action: action}}, cfg)
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

// TestFirewallApplies applies each real plan through the engine on the model
// and reads the host again: Install leaves ufw inactive, Enable active with
// the listener let through, Disable inactive with its rules kept, the runlevel
// added; Add and Remove save hardn.yml alone; Apply writes the profile file
// and its rules.
func TestFirewallApplies(t *testing.T) {
	apply := func(name string, action firewall.Action) *fakeHost {
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
	if r := read(apply("debian13", firewall.ActionInstall)); !r.State.Installed || r.State.Status.Active || r.First != firewall.ItemEnable {
		t.Errorf("install: %+v, item 1 %v", r.State.Status, r.First)
	}
	if r := read(apply("ubuntu2604", firewall.ActionEnable)); !r.State.Status.Active || r.First != firewall.ItemDisable || r.status.Rows[0].Detail != "deny policy" {
		t.Errorf("enable: %+v, %q", r.State.Status, r.status.Rows[0].Detail)
	}
	if r := read(apply("lockout", firewall.ActionEnable)); r.First != firewall.ItemDisable || r.sshRows()[0].Value != "Allowed" {
		t.Errorf("lockout: %+v", r.sshRows())
	}
	if r := read(apply("active22", firewall.ActionDisable)); r.State.Status.Active || r.State.Enabled || len(r.State.Rules) != 2 || r.First != firewall.ItemEnable {
		t.Errorf("disable: active %v, enabled %v, rules %d", r.State.Status.Active, r.State.Enabled, len(r.State.Rules))
	}
	for _, name := range []string{"alpine-runlevel", "unit-off"} {
		if r := read(apply(name, firewall.ActionEnable)); r.Boot != detect.Yes || r.First != firewall.ItemDisable {
			t.Errorf("%s: %v, item 1 %v", name, r.Boot, r.First)
		}
	}
	h := apply("profiles", firewall.ActionApplyProfiles)
	if !strings.Contains(h.content(ufw.ProfileFile), "[LabHTTPS]\ntitle=Lab Web Server (HTTPS)\n") {
		t.Errorf("apply: %q", h.content(ufw.ProfileFile))
	}
	if r := read(h); !r.Applied("LabHTTPS") || !r.Applied("LabDNS") || r.profileRows()[0].Note != "Applied" {
		t.Errorf("apply: %+v", r.profileRows())
	}
	if _, p := fwPlan(t, h, firewall.ActionApplyProfiles); p.Err() != nil || len(p.Changes) != 0 {
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
	for _, want := range []string{"A profile with this name already exists.", "Use letters, digits", "Give each port, 1 to 65535"} {
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
// of any kind, no read under /etc/ufw but ufw.conf (decision 7), no item but
// Return.
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
				if c.Op == "run" && path.Base(c.Argv[0]) == "ufw" || strings.HasPrefix(c.Path, "/etc/ufw") && c.Path != ufw.ConfFile || strings.HasPrefix(c.Path, "/etc/default/ufw") {
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
	if !errors.Is(r.Proxmox, pve.ErrProxmoxUnknown) || r.refusal() == nil {
		t.Errorf("not established: %v, refusal %v", r.Proxmox, r.refusal())
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
	if got := r(fwNoSS()); !errors.Is(got.PlanErr, ufw.ErrListenersUnknown) || !got.Sockets.NoSS {
		t.Errorf("no ss: %v", got.PlanErr)
	}
	if got := r(fwGarbled()); !errors.Is(got.PlanErr, ufw.ErrStateUnknown) {
		t.Errorf("garbled: %v", got.PlanErr)
	}
	if got := r(fwSavedDeny()); !errors.Is(got.PlanErr, ufw.ErrBlocks) {
		t.Errorf("saved deny: %v", got.PlanErr)
	}
}

// TestFirewallStaleListener pins why the screen refuses (decision 2): on an edit to 2230 not reloaded, sshd still on
// 22, the real PlanUFWEnable reads sshd -T alone. With an SSH session on 22 it
// keeps 22 as the session's port; from a console it does not, so the listener
// that accepts connections now is refused once ufw starts, where the Firewall
// row's reading treats it as a candidate.
func TestFirewallStaleListener(t *testing.T) {
	ctx := context.Background()
	plan := func(s revert.Session) []string {
		h := fwEdit()
		f, err := detect.Detect(ctx, h.system())
		if err != nil {
			t.Fatal(err)
		}
		changes, err := steps.PlanUFWEnable(ctx, h.system(), engine.Input{Facts: f, Session: s}, firewall.ID)
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
	ls, ok := firewall.ReadSockets(ctx, h.system(), f).Listeners()
	if !ok || !slices.Contains(ls.Ports(), 22) || !slices.Contains(ls.Ports(), 2230) {
		t.Errorf("the row's listeners: %v", ls.Ports())
	}
	// The screen plans none of that, with a session or from a console.
	for _, s := range []revert.Session{applySession, {}} {
		in := engine.Input{Facts: f, Session: s}
		if _, err := (firewall.Incoming{Action: firewall.ActionEnable}).Plan(ctx, h.system(), in); !errors.Is(err, firewall.ErrNotInEffect) {
			t.Errorf("session %+v: %v", s, err)
		}
	}
}

// fwRead reads h as the screen does.
func fwRead(t *testing.T, h *fakeHost) fwReading {
	t.Helper()
	r, err := (&session{}).readFW(context.Background(), h.system(), fwSession(h))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// fwRefusalText is the refusal's words, plain.
func fwRefusalText(r fwReading) string {
	return flatText(r.refusal().Render(component.NewCtx(term.Fixed(200, colorprofile.NoTTY, false, true))))
}

// TestFirewallBoundUnread: an ss that fails is refused as a missing one is, on
// every state, and the SSH section says so; Disable is still there when active.
func TestFirewallBoundUnread(t *testing.T) {
	r := fwRead(t, fwSSFailing())
	if !errors.Is(r.PlanErr, firewall.ErrSSFailed) || !strings.Contains(fwRefusalText(r), "ss failed") {
		t.Errorf("inactive: %v", r.PlanErr)
	}
	if got := rowOf(r.sshRows(), fwKeyBound); got.Value != "ss failed" || got.State != component.ValueWarn {
		t.Errorf("row %+v", got)
	}
	h := fwDebian()
	run := h.f.Handler
	h.f.Handler = func(argv []string, in []byte) hostfake.Response {
		if path.Base(argv[0]) == "ss" {
			return hostfake.Response{ExitCode: 1}
		}
		return run(argv, in)
	}
	r = fwRead(t, h)
	if r.First != firewall.ItemEnable || !errors.Is(r.PlanErr, firewall.ErrSSFailed) || !r.DisableBeside {
		t.Errorf("active: item 1 %v, %v", r.First, r.PlanErr)
	}
	if rowOf(r.sshRows(), fwKeyBound).Value != "ss failed" {
		t.Errorf("active: %+v", r.sshRows())
	}
}

// TestFirewallBoundElsewhere: configured 22, sshd bound on 2230, is the stale
// listener the other way round. ss says no socket's program, so 2230 gets no
// row; the 22 rows don't read good, the section says so, and Enable refuses.
func TestFirewallBoundElsewhere(t *testing.T) {
	r, _ := readFWHost(t, "moved-2230")
	rows := r.sshRows()
	if got := rowOf(rows, "0.0.0.0:22"); got.Note != "Not bound" || got.State != component.ValueWarn {
		t.Errorf("22: %+v", got)
	}
	if got := rowOf(rows, fwKeyBound); got.Value != "None on SSH's ports" {
		t.Errorf("bound: %+v", rows)
	}
	if !strings.Contains(fwRefusalText(r), "isn't listening where its config says") || !r.DisableBeside {
		t.Errorf("refusal %q", fwRefusalText(r))
	}
	if r = fwRead(t, fwMovedInactive()); !errors.Is(r.PlanErr, firewall.ErrNotInEffect) {
		t.Errorf("inactive: %v", r.PlanErr)
	}
}

// TestFirewallApplyInactive: Apply saves no rule on an inactive ufw; the
// sub-screen says so before asking, and the plan refuses.
func TestFirewallApplyInactive(t *testing.T) {
	inactive := fwProfilesInactive
	h := inactive()
	e := engine.New(h.system(), engine.Options{Session: fwSession(h)})
	cfg, err := statusConfig(context.Background(), h.system())
	if err != nil {
		t.Fatal(err)
	}
	if p, err := e.Plan(context.Background(), []engine.Control{firewall.Incoming{Action: firewall.ActionApplyProfiles}}, cfg); err != nil || !errors.Is(p.Err(), ufw.ErrInactive) {
		t.Errorf("plan: %v, %v", err, p.Err())
	}
	fh := fwHost{name: "apply-inactive", host: inactive}
	h = inactive()
	n := len(h.f.Calls())
	s, out, code := playFW(t, fh, h, false, "2", "3", "0", "0")
	if code != exitOK || !strings.Contains(flatText(out), "ufw isn't running, so hardn saves no rule for it. Enable the firewall, then apply.") || len(s.engineRuns) != 0 {
		t.Errorf("exit %d, %d engines:\n%s", code, len(s.engineRuns), out)
	}
	if cmds, files := changed(h.f.Calls()[n:]); len(cmds) > 0 || len(files) > 0 {
		t.Errorf("changed %q, %q", cmds, files)
	}
}

// TestFirewallNothingRefused: where Enable has nothing to plan under a row that
// says SSH is refused, the screen refuses and says what to do: "Nothing needs
// changing" is never reached.
func TestFirewallNothingRefused(t *testing.T) {
	for _, tt := range []struct {
		name string
		sim  ufwSim
		want string
	}{
		{"an active deny", ufwSim{rules: []simRule{{action: "deny", port: "22"}}}, "ufw refuses SSH on"},
		{"one family", ufwSim{rules: []simRule{{action: "allow", port: "22", src: "192.0.2.0/24"}}}, "ufw refuses SSH on [::]:22"},
		{"IPv6 off", ufwSim{noIPv6: true, rules: []simRule{{action: "allow", port: "22"}}}, "IPv6 is off"},
	} {
		tt.sim.incoming, tt.sim.active, tt.sim.enabled = "deny", true, true
		h := fwWith(tt.sim)()
		r := fwRead(t, h)
		if !errors.Is(r.PlanErr, firewall.ErrRefused) || !strings.Contains(fwRefusalText(r), tt.want) || r.enableWords().label != fwRepairLabel {
			t.Errorf("%s: %v, %q, %q", tt.name, r.PlanErr, fwRefusalText(r), r.enableWords().label)
		}
		_, out, code := playFW(t, fwHost{name: "nothing", host: fwWith(tt.sim)}, h, false, "1", "0")
		if code != exitOK || strings.Contains(flatText(out), "Nothing needs changing") {
			t.Errorf("%s: exit %d:\n%s", tt.name, code, out)
		}
	}
}

// TestFirewallIPv6Off: IPV6=no is said as such, never as a saved rule to delete.
func TestFirewallIPv6Off(t *testing.T) {
	h := statusOn(ubuntu2604Platform).withHardnYML(fwYML)
	h.ufwAsShipped()
	r := fwRead(t, simulateUFW(h, ufwSim{incoming: "deny", noIPv6: true}))
	if !errors.Is(r.PlanErr, ufw.ErrBlocks) {
		t.Fatalf("plan: %v", r.PlanErr)
	}
	text := fwRefusalText(r)
	if !strings.Contains(text, "IPv6 is off") || strings.Contains(text, "ufw delete") {
		t.Errorf("refusal %q", text)
	}
	if got := rowOf(r.ufwRows(), fwKeyIPv6); got.Value != "Off" {
		t.Errorf("row %+v", r.ufwRows())
	}
}

// TestFirewallProxmoxMarks: no row reads good where the host is less protected
// than it says; the reading's service and findings are drawn; and ufw left
// active by origin/main is neither read nor shown, as decision 7 says.
func TestFirewallProxmoxMarks(t *testing.T) {
	r := fwRead(t, fwPVEStopped())
	rows := r.pveMenu().Sections[0].Rows.Items
	if got := rowOf(rows, fwKeyService); got.Value != "Stopped" || got.State != component.ValueWarn {
		t.Errorf("service %+v", got)
	}
	for _, key := range []string{fwKeyEnabled, fwKeyPolicy} {
		if got := rowOf(rows, key); got.State == component.ValueOK {
			t.Errorf("stopped: %s reads good: %+v", key, got)
		}
	}
	r = fwRead(t, fwPVEMoved())
	if got := rowOf(r.pveMenu().Sections[0].Rows.Items, fwKeySSH); got.State != component.ValueWarn || got.Note != "sshd on 2230" {
		t.Errorf("moved: %+v", got)
	}
	r = fwRead(t, fwPVEBlacklist())
	if got := rowOf(r.pveMenu().Sections[0].Rows.Items, "Blacklist"); got.Value != "Has entries" {
		t.Errorf("blacklist: %+v", r.pveMenu().Sections[0].Rows.Items)
	}
	h := fwPVEUFW()
	n := len(h.f.Calls())
	r = fwRead(t, h)
	if len(r.subMenu().Menu.Items) != 0 {
		t.Errorf("ufw active: items %+v", r.subMenu().Menu.Items)
	}
	for _, c := range h.f.Calls()[n:] {
		if c.Op == "run" && path.Base(c.Argv[0]) == "ufw" {
			t.Errorf("ufw active: a ufw call: %s", c)
		}
	}
}

// TestFirewallAtBoot: At boot says Yes only with ENABLED=yes and the service
// read as starting at boot; a service off or not read says so, and only a
// service read as on is "set".
func TestFirewallAtBoot(t *testing.T) {
	r, _ := readFWHost(t, "unit-off")
	if got := rowOf(r.ufwRows(), fwKeyBoot); got.Value != "Service disabled" || got.State != component.ValueWarn {
		t.Errorf("off: %+v", got)
	}
	if r.First != firewall.ItemEnable || r.enableWords().label != fwBootLabel {
		t.Errorf("off: item 1 %v, %q", r.First, r.enableWords().label)
	}
	unread := func(base func() *fakeHost, hit string) fwReading {
		h := base()
		run := h.f.Handler
		h.f.Handler = func(argv []string, in []byte) hostfake.Response {
			if strings.Contains(strings.Join(argv, " "), hit) {
				return hostfake.Response{Stderr: "failed\n", ExitCode: 2}
			}
			return run(argv, in)
		}
		return fwRead(t, h)
	}
	r = unread(fwDebian, "is-enabled ufw.service")
	if got := rowOf(r.ufwRows(), fwKeyBoot); got.State != component.ValueUnknown || got.Value != "" {
		t.Errorf("unread: %+v", got)
	}
	if (r.First == firewall.ItemDisable) || !strings.Contains(fwRefusalText(r), "can't tell if ufw starts at boot") {
		t.Errorf("unread: set %v, %q", (r.First == firewall.ItemDisable), fwRefusalText(r))
	}
	if rev := rowOf(r.enableReview(), fwKeyBoot); rev.State != component.ValueUnknown {
		t.Errorf("review: %+v", rev)
	}
}

// TestFirewallModel: ufwSim prints what the repo's parser test models, keeps
// every port of a profile, and says a delete as a delete.
func TestFirewallModel(t *testing.T) {
	h := statusOn(debian13Platform).withHardnYML(fwYML + "ufwAppProfiles:\n  - name: Web\n    title: Web\n    description: Web\n    ports:\n      - \"80/tcp\"\n      - \"443/tcp\"\n      - \"53/udp\"\n")
	simulateUFW(h, ufwSim{incoming: "deny", active: true, enabled: true})
	e, p := fwPlan(t, h, firewall.ActionApplyProfiles)
	if p.Err() != nil {
		t.Fatal(p.Err())
	}
	if _, err := e.Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	status, err := h.system().Run(context.Background(), "ufw", "status", "verbose")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"80,443/tcp (Web) ", "80,443/tcp (Web (v6)) ", "53/udp (Web) "} {
		if !strings.Contains(string(status.Stdout), want) {
			t.Errorf("no %q:\n%s", want, status.Stdout)
		}
	}
	got := ufw.ParseStatus(status.Stdout)
	if len(got.Bad) > 0 || !slices.ContainsFunc(got.Rules, func(r ufw.Rule) bool { return r.DPort == "80,443" && r.Family == ufw.IPv6 }) {
		t.Errorf("parsed %+v, bad %q", got.Rules, got.Bad)
	}
	if res, _ := h.system().Run(context.Background(), "ufw", "delete", "allow", "22/tcp"); !strings.Contains(string(res.Stdout), "Rule deleted") {
		t.Errorf("delete says %q", res.Stdout)
	}
}

// TestFirewallAddChecks: the form refuses what ufw's file could not hold.
func TestFirewallAddChecks(t *testing.T) {
	for p, want := range map[string]bool{"80/tcp": true, "1:65535/udp": true, "99999/tcp": false, "0/tcp": false, "90:80/tcp": false, "80": false} {
		if fwPortOK(p) != want {
			t.Errorf("%q: %v", p, !want)
		}
	}
	fh := fwHostNamed(t, "profiles")
	h := fh.host()
	_, out, code := playFW(t, fh, h, false, "2", "1", "Web", "", "Web Server", "", "About", "99999/tcp", "80/tcp", "y", "0", "0")
	text := flatText(out)
	for _, want := range []string{"Give the profile a title.", "Give the profile a description.", "Give each port, 1 to 65535"} {
		if code != exitOK || !strings.Contains(text, want) {
			t.Errorf("exit %d, no %q:\n%s", code, want, text)
		}
	}
}

// TestFirewallItemWords: item 1 says what its plan does on each host, and the
// flow asks the same; "Enable" is said only of a ufw not running.
func TestFirewallItemWords(t *testing.T) {
	for host, want := range map[string]string{"ubuntu2604": fwEnableLabel, "allow-policy": fwDenyLabel, "lockout": fwAllowLabel,
		"alpine-runlevel": fwBootLabel, "unit-off": fwBootLabel, "moved-2230": fwRepairLabel, "denied": fwRepairLabel} {
		r, _ := readFWHost(t, host)
		if got := r.subMenu().Menu.Items[0].Label; got != want {
			t.Errorf("%s: %q, want %q", host, got, want)
		}
	}
}

// TestFirewallDisableBeside: an active ufw always has Disable; a repair beside
// it keeps Profiles at 2 and Disable at 3.
func TestFirewallDisableBeside(t *testing.T) {
	for _, fh := range fwHosts() {
		r, _ := readFWHost(t, fh.name)
		var items []string
		for _, it := range r.subMenu().Menu.Items {
			items = append(items, it.Key+" "+it.Label)
		}
		active := r.State.Status.Known && r.State.Status.Active
		hasOff := slices.Contains(items, "1 "+fwDisableLabel) || slices.Contains(items, "3 "+fwDisableLabel)
		if hasOff != active {
			t.Errorf("%s: active %v, items %q", fh.name, active, items)
		}
	}
	fh := fwHostNamed(t, "lockout")
	h := fh.host()
	if _, out, code := playFW(t, fh, h, false, "3", "y", "0"); code != exitOK || !strings.Contains(flatText(out), fwDisableWarning) {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if r := fwRead(t, h); r.State.Status.Active {
		t.Errorf("still active")
	}
}

// TestFirewallInactiveMarks: an inactive ufw filters nothing, so no policy or
// saved allow reads good, as Proxmox VE's screen treats the same case.
func TestFirewallInactiveMarks(t *testing.T) {
	r, _ := readFWHost(t, "ubuntu2604")
	if got := rowOf(r.ufwRows(), fwKeyPolicy); got.Value != "Deny" || got.State != component.ValuePlain {
		t.Errorf("policy %+v", got)
	}
	r, _ = readFWHost(t, "lockout-at-start")
	for _, row := range r.ruleRows() {
		if row.Value == "Allow" && row.State != component.ValuePlain {
			t.Errorf("saved %+v", row)
		}
	}
}

// TestFirewallDeniedPlansNothing: a deny rule
// on SSH's port under an active ufw is "ssh blocked", and the real plan is
// empty: ufw.PlanFor does not change a rule it finds. The screen refuses.
func TestFirewallDeniedPlansNothing(t *testing.T) {
	h := fwWith(ufwSim{incoming: "deny", active: true, enabled: true, rules: []simRule{{action: "deny", port: "22"}}})()
	r, err := (&session{}).readFW(context.Background(), h.system(), fwSession(h))
	if err != nil {
		t.Fatal(err)
	}
	if r.status.Rows[0].Detail != "ssh blocked" || r.First != firewall.ItemEnable || !errors.Is(r.PlanErr, firewall.ErrRefused) || len(r.Plan) != 0 {
		t.Errorf("row %q, item 1 %v, plan %d changes, %v", r.status.Rows[0].Detail, r.First, len(r.Plan), r.PlanErr)
	}
	changes, err := steps.PlanUFWEnable(context.Background(), h.system(), engine.Input{Facts: r.facts, Session: fwSession(h)}, firewall.ID)
	if err != nil || len(changes) != 0 {
		t.Errorf("the real plan: %d changes, %v", len(changes), err)
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
		case r.OnPVE:
			lines = []string{"0"}
		case !r.State.Installed:
			lines = []string{"1", "0"}
		case r.First == firewall.ItemEnable && r.refusal() != nil:
			lines = []string{"1", "0"}
		default:
			lines = []string{"1", "0"}
		}
		_, out, code := playFW(t, fh, h, true, lines...)
		if code != exitOK {
			t.Errorf("%s: exit %d:\n%s", fh.name, code, out)
		}
		if !r.OnPVE && r.refusal() == nil && !stops(out) {
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
	want := map[string]int{"debian13": 20, "ubuntu2604": 29, "active22": 29, "hardened": 29, "allow-policy": 30, "lockout": 31,
		"lockout-at-start": 29, "allowlist": 31, "blocklist": 33, "edit-2230": 30, "moved-2230": 31, "denied": 30, "alpine324": 20, "alpine-stock": 21, "alpine-runlevel": 30,
		"unit-off": 30, "pve9": 17, "pve9-drop": 17, "profiles": 34, "sub-profiles": 16}
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

// TestFirewallInstallRefused: Install's refusals, the control's, said before
// asking, on the host's own tool; alpine-stock's words are the VM's case.
func TestFirewallInstallRefused(t *testing.T) {
	for _, tt := range []struct {
		name  string
		host  func() *fakeHost
		words string
		want  error
	}{
		{"alpine-stock", fwAlpineStock, "hardn can't find ufw in apk's package lists. Check the network and the community repository, run apk update, then try again.", packages.ErrNotInRepos},
		{"apt", fwNoUFWSim(debian13Platform, fwAPTNotFound), "hardn can't find ufw in apt's package lists. Run apt-get update, then try again.", packages.ErrNotInRepos},
		{"a removal", fwNoUFWSim(debian13Platform, fwAPTRemoves), "Installing ufw would also change other packages: nftables. Install it by hand, then try again.", packages.ErrOtherPackages},
	} {
		h := tt.host()
		r := fwRead(t, h)
		if r.First != firewall.ItemInstall || !errors.Is(r.PlanErr, tt.want) || !strings.Contains(fwRefusalText(r), tt.words) {
			t.Errorf("%s: item 1 %v, %v, %q", tt.name, r.First, r.PlanErr, fwRefusalText(r))
		}
		n := len(h.f.Calls())
		s, out, code := playFW(t, fwHost{name: "refused", host: tt.host}, h, false, "1", "0")
		if code != exitOK || len(s.engineRuns) != 0 || !strings.Contains(flatText(out), tt.words) {
			t.Errorf("%s: exit %d, %d engines:\n%s", tt.name, code, len(s.engineRuns), out)
		}
		if cmds, files := changed(h.f.Calls()[n:]); len(cmds) > 0 || len(files) > 0 {
			t.Errorf("%s: changed %q, %q", tt.name, cmds, files)
		}
	}
	// Decision 1: Install brings ufw alone. Where ss is missing, Enable then refuses.
	sim := fwAPKInstalls
	h := fwWithoutSS(simulateUFW(statusOn(alpine323Platform).withHardnYML(fwYML), ufwSim{incoming: "deny", sim: &sim}))
	e, p := fwPlan(t, h, firewall.ActionInstall)
	if _, err := e.Apply(context.Background(), p); err != nil || p.Err() != nil {
		t.Fatalf("%v, %v", err, p.Err())
	}
	if r := fwRead(t, h); !r.Sockets.NoSS || !errors.Is(r.PlanErr, firewall.ErrNoSS) {
		t.Errorf("after the install: %+v, %v", r.Sockets, r.PlanErr)
	}
}
