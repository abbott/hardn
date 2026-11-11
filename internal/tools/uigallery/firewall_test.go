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

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/packages"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The Firewall screen on the simulated hosts (firewall.go, firewallhost.go;
// slice 4): the hosts and the plans the screen makes on them. What the screen
// draws and asks is pkg/screens/firewall's and pkg/ui/screen's to test. Nothing
// here runs a command or writes a file outside the fake.

// TestPlayFirewallGolden plays the mocks end to end and pins each transcript.
// Each change id's random suffix is written xxxxxxxx (sessionIDs).
func TestPlayFirewallGolden(t *testing.T) {
	addAnswers := []string{"2", "1", "WebServer", "Web Server", "A web server", "80/tcp,443/tcp", "", "y", "0", "0"}
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"firewall-install", []string{"firewall/debian13"}, []string{"1", "y", "0"}, append(uitest.Core(), asciiColour)},
		{"firewall-install-alpine", []string{"firewall/alpine324"}, []string{"1", "y", "0"}, nil},
		{"firewall-alpine-stock", []string{"firewall/alpine-stock"}, []string{"1", "0"}, nil},
		{"firewall-alpine-lxc", []string{"firewall/alpine-lxc"}, []string{"0"}, nil},
		{"firewall-enable", []string{"firewall/ubuntu2604"}, []string{"1", hostName, "0"}, append(uitest.Core(), asciiColour)},
		{"firewall-disable", []string{"firewall/active22"}, []string{"1", "y", "0"}, nil},
		{"firewall-allow-policy", []string{"firewall/allow-policy"}, []string{"1", hostName, "0"}, nil},
		{"firewall-lockout", []string{"firewall/lockout"}, []string{"1", hostName, "0"}, nil},
		{"firewall-lockout-at-start", []string{"firewall/lockout-at-start"}, []string{"1", "0"}, nil},
		{"firewall-blocklist", []string{"firewall/blocklist"}, []string{"1", "0"}, nil},
		{"firewall-edit", []string{"firewall/edit-2230"}, []string{"1", "0"}, nil},
		{"firewall-moved", []string{"firewall/moved-2230"}, []string{"1", "4", "y", "0"}, nil},
		{"firewall-denied", []string{"firewall/denied"}, []string{"1", "0"}, nil},
		{"firewall-unit-off", []string{"firewall/unit-off"}, []string{"1", "y", "0"}, nil},
		{"firewall-lockout-disable", []string{"firewall/lockout"}, []string{"4", "y", "0"}, nil},
		{"firewall-runlevel", []string{"firewall/alpine-runlevel"}, []string{"1", "y", "0"}, nil},
		{"firewall-pve9", []string{"firewall/pve9"}, []string{"0"}, nil},
		{"firewall-pve9-drop", []string{"firewall/pve9-drop"}, []string{"0"}, nil},
		{"firewall-pve9-ufw", []string{"firewall/pve9-ufw"}, []string{"0"}, nil},
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

// fwReading is one reading of a host, as the screen reads it: firewall.Read's,
// with the Firewall row as the main menu draws it, and what the screen's changes
// plan with.
type fwReading struct {
	firewall.Reading
	status component.StatusList
	facts  detect.Facts
	cfg    *config.Config
}

// readFW reads sys as the screen does (pkg/screens/firewall's read), writing
// nothing: detect's facts, the Firewall row, and firewall.Read with hardn.yml,
// which on Proxmox VE is not used.
func (s *session) readFW(ctx context.Context, sys host.System, session revert.Session) (fwReading, error) {
	var r fwReading
	var err error
	if r.facts, err = detect.Detect(ctx, sys); err != nil {
		return r, err
	}
	e := engine.New(sys, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return r.facts, nil },
		Hold: s.hold})
	reports, err := e.Check(ctx, firewall.Controls())
	if err != nil {
		return r, err
	}
	view := output.StatusViewOf(reports, risk.Level{}).Status
	r.status.Rows = slices.DeleteFunc(view.Rows, func(row component.StatusRow) bool { return row.Label != firewall.Incoming.Title(firewall.Incoming{}) })
	if onPVE, perr := pve.Applies(r.facts.Proxmox); !onPVE && perr == nil {
		if r.cfg, err = statusConfig(ctx, sys); err != nil {
			return r, err
		}
	}
	r.Reading = firewall.Read(ctx, sys, r.facts, r.cfg, session)
	return r, r.PVEErr
}

// fwLockout is whether item 1's plan is lockout-risk, which the screen asks the
// hostname typed for.
func fwLockout(r fwReading) bool {
	return r.PlanErr == nil && slices.ContainsFunc(r.Plan, func(c engine.Change) bool { return c.Risk == engine.RiskLockout })
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
		{host: "ipv4-allowlist", state: component.StatusPartial, detail: "ssh allowlist", first: firewall.ItemDisable},
		{host: "ipv6-off", state: component.StatusPartial, detail: "ssh IPv4 only", first: firewall.ItemDisable},
		{host: "blocklist", state: component.StatusNotConfigured, detail: "inactive", first: firewall.ItemEnable, refused: ufw.ErrBlocks},
		{host: "edit-2230", state: component.StatusNotConfigured, detail: "inactive", first: firewall.ItemEnable, refused: firewall.ErrNotInEffect},
		{host: "moved-2230", state: component.StatusUnknown, detail: "ssh config not in effect", first: firewall.ItemEnable, refused: firewall.ErrNotInEffect},
		{host: "denied", state: component.StatusPartial, detail: "ssh blocked", first: firewall.ItemEnable, refused: firewall.ErrRefused},
		{host: "alpine324", state: component.StatusNotConfigured, detail: "ufw not installed", first: firewall.ItemInstall},
		{host: "alpine-stock", state: component.StatusNotConfigured, detail: "ufw not installed", first: firewall.ItemInstall, refused: packages.ErrNotInRepos},
		{host: "alpine-lxc", state: component.StatusNotConfigured, detail: "ufw not installed", first: firewall.ItemInstall, refused: packages.ErrNotInRepos},
		{host: "alpine-runlevel", state: component.StatusPartial, detail: "off after restart", first: firewall.ItemEnable},
		{host: "unit-off", state: component.StatusPartial, detail: "off after restart", first: firewall.ItemEnable},
		{host: "pve9", state: component.StatusNotConfigured, detail: "disabled", first: firewall.ItemNone},
		{host: "pve9-drop", state: component.StatusConfigured, detail: "drop policy", first: firewall.ItemNone},
		{host: "pve9-ufw", state: component.StatusNotConfigured, detail: "disabled", first: firewall.ItemNone},
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
		if (r.PlanErr != nil) != (tt.refused != nil) || tt.refused != nil && !errors.Is(r.PlanErr, tt.refused) {
			t.Errorf("%s: refused %v, plan's error %v, want %v", tt.host, r.PlanErr != nil, r.PlanErr, tt.refused)
		}
		if fwLockout(r) != tt.risky {
			t.Errorf("%s: lockout risk %v", tt.host, fwLockout(r))
		}
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
	if r := read(apply("lockout", firewall.ActionEnable)); r.First != firewall.ItemDisable || r.Coverage()[0].Verdict != ufw.Allowed {
		t.Errorf("lockout: %+v", r.Coverage())
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
	if r := read(h); !r.Applied(config.UfwAppProfile{Name: "LabHTTPS"}) || !r.Applied(config.UfwAppProfile{Name: "LabDNS"}) {
		t.Errorf("apply: %+v", r.State.Rules)
	}
	if _, p := fwPlan(t, h, firewall.ActionApplyProfiles); p.Err() != nil || len(p.Changes) != 0 {
		t.Errorf("apply again: %v, %d changes", p.Err(), len(p.Changes))
	}
}

// TestFirewallTrustedInterfaces: on the gallery's simulated ufw, a trusted
// interface added and applied runs "ufw allow in on <interface>", saved for
// both IP versions, and its row reads Applied; a profile limited to it runs
// "ufw allow in on <interface> from any to any app <name>".
func TestFirewallTrustedInterfaces(t *testing.T) {
	fh := fwHostNamed(t, "profiles")
	h := fh.host()
	_, out, code := playFW(t, fh, h, false, "3", "1", "tailscale0", "y", "3", "y", "0",
		"2", "1", "Tailnet", "Tailnet", "Tailscale", "41641/udp", "tailscale0", "y", "3", "y", "0", "0")
	if code != exitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	text := flatText(out)
	for _, want := range []string{"tailscale0 Every port Applied", "41641/udp on tailscale0 Applied"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q:\n%s", want, text)
		}
	}
	var ran []string
	for _, c := range h.f.Calls() {
		if c.Op == "run" && path.Base(c.Argv[0]) == "ufw" {
			ran = append(ran, strings.Join(c.Argv, " "))
		}
	}
	runs := strings.Join(ran, "\n")
	for _, want := range []string{"ufw allow in on tailscale0", "ufw allow in on tailscale0 from any to any app Tailnet"} {
		if !strings.Contains(runs, want) {
			t.Errorf("did not run %q:\n%s", want, runs)
		}
	}
}

// TestFirewallProfilesSave: Add and Remove, through the flow, change hardn.yml
// alone; Add saves all four fields; bad answers are asked again.
func TestFirewallProfilesSave(t *testing.T) {
	fh := fwHostNamed(t, "profiles")
	h := fh.host()
	n := len(h.f.Calls())
	_, out, code := playFW(t, fh, h, false, "2", "1", "LabDNS", "all", "Web", "Web Server", "A web server", "80", "80/tcp,443/tcp", "", "y", "0", "0")
	if code != exitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	text := flatText(out)
	for _, want := range []string{"A profile with this name already exists.", "ufw can't use that name.", "Give each port, 1 to 65535"} {
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
	if !strings.Contains(flatText(out), "A rule already in ufw stays.") {
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
	for _, name := range []string{"pve9", "pve9-drop", "pve9-ufw"} {
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
	if !errors.Is(r.Proxmox, pve.ErrProxmoxUnknown) || r.PlanErr == nil {
		t.Errorf("not established: %v, plan's error %v", r.Proxmox, r.PlanErr)
	}
	for _, c := range h.f.Calls()[n:] {
		if c.Op == "run" && path.Base(c.Argv[0]) == "ufw" {
			t.Errorf("not established: a ufw call: %s", c)
		}
	}
}

// fwSavedDeny is ufw inactive with a deny on SSH's port saved first.
func fwSavedDeny() *fakeHost {
	return fwWith(ufwSim{incoming: "deny", rules: []simRule{{action: "deny", port: "22"}}})()
}

// fwRefusalHosts are the hosts on which item 1 refuses before asking, by what
// each shows, beside the menu's.
func fwRefusalHosts() []struct {
	name string
	host func() *fakeHost
} {
	return []struct {
		name string
		host func() *fakeHost
	}{
		{"apt finds no ufw", fwNoUFWSim(debian13Platform, fwAPTNotFound)},
		{"apk finds no ufw", fwAlpineStock},
		{"other packages change", fwNoUFWSim(debian13Platform, fwAPTRemoves)},
		{"ss failing", fwSSFailing},
		{"sshd's configuration not in effect", fwEdit},
		{"bound elsewhere, inactive", fwMovedInactive},
		{"ufw's state not read", fwGarbled},
		{"lockout-at-start", func() *fakeHost { return ufwOn(lockoutAtStart().withHardnYML(fwYML)) }},
		{"blocklist", fwWith(ufwSim{incoming: "deny", rules: []simRule{{action: "deny", port: "22", src: "198.51.100.0/24"}, {action: "allow", port: "22"}}})},
		{"a saved deny", fwSavedDeny},
		{"IPv6 off in ufw", fwWith(ufwSim{incoming: "deny", noIPv6: true})},
		// Refused on IPv6 alone is set (hosts ipv4-allowlist, ipv6-off) but for a
		// change Enable makes, here ufw.service enabled: refused, as before.
		{"IPv6 off in ufw, active, service off", fwWith(ufwSim{incoming: "deny", active: true, enabled: true, unitOff: true, noIPv6: true, rules: []simRule{{action: "allow", port: "22"}}})},
		{"an allowlist of one family, service off", fwWith(ufwSim{incoming: "deny", active: true, enabled: true, unitOff: true, rules: []simRule{{action: "allow", port: "22", src: "192.0.2.0/24"}}})},
		{"an active deny", fwWith(ufwSim{incoming: "deny", active: true, enabled: true, rules: []simRule{{action: "deny", port: "22"}}})},
		{"Proxmox VE not established", fwProxmoxUnknown},
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
		if r.PlanErr == nil {
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
	if got := r(fwNoSS()); got.First != firewall.ItemInstallSS || got.PlanErr != nil || !got.Sockets.NoSS || len(got.Plan) != 1 || got.Plan[0].Target != "iproute2" {
		t.Errorf("no ss: item 1 %v, %v, plan %+v", got.First, got.PlanErr, got.Plan)
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
	return flatText(screenkit.Warnings(screenkit.RefusalWords(r.PlanErr)).Render(component.NewCtx(term.Fixed(200, colorprofile.NoTTY, false, true))))
}

// TestFirewallBoundUnread: an ss that fails is refused as a missing one is, on
// every state, and the reading says ss failed; Disable is still there when
// active.
func TestFirewallBoundUnread(t *testing.T) {
	r := fwRead(t, fwSSFailing())
	if !errors.Is(r.PlanErr, firewall.ErrSSFailed) || !strings.Contains(fwRefusalText(r), "ss failed") {
		t.Errorf("inactive: %v", r.PlanErr)
	}
	if !r.Sockets.SSFailed {
		t.Errorf("sockets %+v", r.Sockets)
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
	if !r.Sockets.SSFailed {
		t.Errorf("active: sockets %+v", r.Sockets)
	}
}

// TestFirewallBoundElsewhere: configured 22, sshd bound on 2230, is the stale
// listener the other way round. ss says no socket's program, so 2230 is not
// sshd's: 22 is unbound, none is bound on SSH's ports, and Enable refuses.
func TestFirewallBoundElsewhere(t *testing.T) {
	r, _ := readFWHost(t, "moved-2230")
	if !slices.ContainsFunc(r.Sockets.Unbound, func(l sshd.Listener) bool { return l.String() == "0.0.0.0:22" }) || !r.Sockets.NoneBound() {
		t.Errorf("sockets %+v", r.Sockets)
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
	if code != exitOK || !strings.Contains(flatText(out), "ufw isn't running, so hardn saves no rule for it. Enable the firewall (Firewall, option 1), then apply.") || len(s.engineRuns) != 0 {
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
		// One family's allowlist and IPV6=no, refused on IPv6 alone while IPv4
		// gets in, no longer say SSH is refused: hosts ipv4-allowlist and
		// ipv6-off (TestFirewallHosts), item 1 Disable (2026-11-08).
		{"an active deny", ufwSim{rules: []simRule{{action: "deny", port: "22"}}}, "ufw refuses SSH on"},
	} {
		tt.sim.incoming, tt.sim.active, tt.sim.enabled = "deny", true, true
		h := fwWith(tt.sim)()
		r := fwRead(t, h)
		if !errors.Is(r.PlanErr, firewall.ErrRefused) || !strings.Contains(fwRefusalText(r), tt.want) {
			t.Errorf("%s: %v, %q", tt.name, r.PlanErr, fwRefusalText(r))
		}
		_, out, code := playFW(t, fwHost{name: "nothing", host: fwWith(tt.sim)}, h, false, "1", "0")
		if code != exitOK || strings.Contains(flatText(out), "Nothing needs changing") {
			t.Errorf("%s: exit %d:\n%s", tt.name, code, out)
		}
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
		case r.First == firewall.ItemEnable && r.PlanErr != nil:
			lines = []string{"1", "0"}
		default:
			lines = []string{"1", "0"}
		}
		_, out, code := playFW(t, fh, h, true, lines...)
		if code != exitOK {
			t.Errorf("%s: exit %d:\n%s", fh.name, code, out)
		}
		if !r.OnPVE && r.PlanErr == nil && !stops(out) {
			t.Errorf("%s: plan only did not stop at the plan:\n%s", fh.name, out)
		}
		if cmds, files := changed(h.f.Calls()[n:]); len(cmds) > 0 || len(files) > 0 {
			t.Errorf("%s: plan only changed the host: %q, %q", fh.name, cmds, files)
		}
	}
	fh := fwHostNamed(t, "profiles")
	for _, lines := range [][]string{
		{"2", "1", "Web", "Web Server", "A web server", "80/tcp", "", "0", "0"},
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

// TestFirewallUsage: play refuses what it does not take, with status 2 and
// nothing on stdout; the listing of the mocks is gone; the main menu's 3 leads
// to debian13's.
func TestFirewallUsage(t *testing.T) {
	for _, args := range [][]string{
		{fwPrefix},
		{"play", "firewall/nope"},
		{"play", "-fail", "1", "firewall/debian13"},
		{"play", "-sequence", "failure", "firewall/debian13"},
	} {
		var out, errs bytes.Buffer
		if code := run(args, nil, offTerminal, &out, &errs); code != exitUsage || out.Len() > 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errs.String())
		}
	}
	if fwLinks[mainMenuMock+"/3"] != fwMainHost {
		t.Errorf("the main menu's 3 leads to %q", fwLinks[mainMenuMock+"/3"])
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
	// Decision 1: Install brings ufw alone. Where ss is missing, item 1 is then
	// Install iproute2 (the maintainer, 2026-11-07), and once that runs, Enable.
	sim := fwAPKInstalls
	h := fwWithoutSS(simulateUFW(statusOn(alpine323Platform).withHardnYML(fwYML), ufwSim{incoming: "deny", sim: &sim}))
	e, p := fwPlan(t, h, firewall.ActionInstall)
	if _, err := e.Apply(context.Background(), p); err != nil || p.Err() != nil {
		t.Fatalf("%v, %v", err, p.Err())
	}
	r := fwRead(t, h)
	if !r.Sockets.NoSS || r.First != firewall.ItemInstallSS || r.PlanErr != nil {
		t.Fatalf("after the install: item 1 %v, %+v, %v", r.First, r.Sockets, r.PlanErr)
	}
	e, p = fwPlan(t, h, firewall.ActionInstallSS)
	if _, err := e.Apply(context.Background(), p); err != nil || p.Err() != nil {
		t.Fatalf("iproute2: %v, %v", err, p.Err())
	}
	if r := fwRead(t, h); r.Sockets.NoSS || r.First != firewall.ItemEnable {
		t.Errorf("after iproute2: item 1 %v, ss missing %v, %v", r.First, r.Sockets.NoSS, r.PlanErr)
	}
}
