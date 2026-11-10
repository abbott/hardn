package firewall_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/firewalld"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
)

// elHost is Rocky Linux 10 as the Firewall item reads and changes it
// there: firewalld (installed or not, running or not), dnf's simulation of
// firewalld without weak dependencies (rocky10-test-110, probe 3b-2), its
// install, NetworkManager without zones, no other firewall.
type elHost struct {
	installed, ufw  bool
	active, enabled string
	masked          bool
	other           string // an other firewall unit that is active
	nmZone          string // eth0's connection.zone
	noCleanup       bool   // CleanupOnExit=no
	failInstall     bool
	ran             []string
}

const elSimulate = "Dependencies resolved.\n" +
	"================================================================================\n" +
	" Package                   Arch        Version                Repository   Size\n" +
	"================================================================================\n" +
	"Installing:\n firewalld                 noarch      2.4.3-4.el10_2         baseos      471 k\n" +
	"Installing dependencies:\n firewalld-filesystem      noarch      2.4.3-4.el10_2         baseos      7.7 k\n" +
	" nftables                  x86_64      1:1.1.5-6.el10_2       baseos      447 k\n" +
	"\nTransaction Summary\n================================================================================\nInstall  3 Packages\n"

func (e *elHost) answer(argv []string) (hostfake.Response, bool) {
	cmd := strings.Join(argv, " ")
	word := func(w string, ok bool) hostfake.Response {
		if ok {
			return hostfake.Response{Stdout: w + "\n"}
		}
		return hostfake.Response{Stdout: w + "\n", ExitCode: 3}
	}
	switch cmd {
	case "systemctl is-active firewalld.service":
		if !e.installed {
			return word("inactive", false), true
		}
		return word(e.active, e.active == "active"), true
	case "systemctl is-enabled firewalld.service":
		switch {
		case e.masked:
			return word("masked", false), true
		case !e.installed:
			return word("not-found", false), true
		}
		return word(e.enabled, e.enabled == "enabled"), true
	case "systemctl mask firewalld.service":
		e.masked = true
	case "systemctl unmask firewalld.service":
		e.masked = false
	case "systemctl start firewalld.service":
		e.active = "active"
	case "systemctl stop firewalld.service":
		e.active = "inactive"
	case "systemctl enable firewalld.service":
		e.enabled = "enabled"
	case "systemctl disable firewalld.service":
		e.enabled = "disabled"
	case "rpm -q ufw":
		if e.ufw {
			return hostfake.Response{Stdout: "ufw-0.36.2-1.el10.noarch\n"}, true
		}
		return hostfake.Response{Stdout: "package ufw is not installed\n", ExitCode: 1}, true
	case "rpm -q firewalld":
		if e.installed {
			return hostfake.Response{Stdout: "firewalld-2.4.3-4.el10_2.noarch\n"}, true
		}
		return hostfake.Response{Stdout: "package firewalld is not installed\n", ExitCode: 1}, true
	case "dnf install --assumeno --setopt=install_weak_deps=False --setopt=exit_on_lock=True firewalld":
		return hostfake.Response{Stdout: elSimulate, Stderr: "Operation aborted.\n", ExitCode: 1}, true
	case "dnf install -y --setopt=install_weak_deps=False --setopt=exit_on_lock=True firewalld":
		if e.failInstall {
			return hostfake.Response{Stderr: "Error: Transaction test error\n", ExitCode: 1}, true
		}
		e.installed, e.active = true, "inactive"
		if !e.masked {
			e.enabled = "enabled" // the preset
		} else {
			e.enabled = "disabled"
		}
	case "firewall-offline-cmd --get-default-zone":
		return hostfake.Response{Stdout: "public\n"}, true
	case "firewall-offline-cmd --list-all-zones":
		return hostfake.Response{Stdout: "drop\n  target: DROP\n  interfaces: \n  sources: \n  services: \n  ports: \n\n" +
			"public (default)\n  target: default\n  interfaces: \n  sources: \n  services: cockpit dhcpv6-client ssh\n  ports: \n\n"}, true
	case "firewall-offline-cmd --service=ssh --get-ports":
		return hostfake.Response{Stdout: "22/tcp\n"}, true
	case "firewall-offline-cmd --service=cockpit --get-ports":
		return hostfake.Response{Stdout: "9090/tcp\n"}, true
	case "firewall-offline-cmd --service=dhcpv6-client --get-ports":
		return hostfake.Response{Stdout: "546/udp\n"}, true
	case "nmcli -t -f UUID,DEVICE connection show --active":
		return hostfake.Response{Stdout: "1dd9a779-d327-56e1-8454-c65e2556c12c:eth0\n"}, true
	case "nmcli -g connection.zone connection show 1dd9a779-d327-56e1-8454-c65e2556c12c":
		return hostfake.Response{Stdout: e.nmZone + "\n"}, true
	default:
		if len(argv) == 3 && argv[0] == "systemctl" && argv[1] == "is-active" && argv[2] == e.other {
			return word("active", true), true
		}
		if len(argv) == 3 && argv[0] == "systemctl" && (argv[1] == "is-active" || argv[1] == "is-enabled") {
			if argv[1] == "is-active" {
				return word("inactive", false), true
			}
			return hostfake.Response{Stdout: "not-found\n", ExitCode: 4}, true
		}
		return hostfake.Response{}, false
	}
	return hostfake.Response{}, true
}

// rocky is Rocky Linux 10.2 for the site builder: sshd.service, the stock
// sysconfig, and e's firewalld in front of the builder's own handler.
func (e *elHost) site() site {
	r := debian13()
	r.facts.OS = detect.OSRelease{ID: "rocky", IDLike: []string{"rhel", "centos", "fedora"}, VersionID: "10.2"}
	r.facts.Platform = detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Supported}
	r.facts.SSH.SSHDUnits = true
	return site{plat: r, ufw: fw{missing: true}, edit: func(h *hostfake.System) {
		h.AddFile(reload.SysconfigSSHD, []byte("OPTIONS=\"\"\n"), 0o644, 0, 0)
		if e.installed {
			h.AddFile(firewalld.UnitFile, []byte("[Unit]\n"), 0o644, 0, 0)
		}
		if e.noCleanup {
			h.AddFile(firewalld.ConfFile, []byte("CleanupOnExit=no\n"), 0o644, 0, 0)
		}
		next := h.Handler
		h.Handler = func(argv []string, stdin []byte) hostfake.Response {
			if r, ok := e.answer(argv); ok {
				e.ran = append(e.ran, strings.Join(argv, " "))
				return r
			}
			return next(argv, stdin)
		}
	}}
}

func noUFWRun(t *testing.T, ran []string) {
	t.Helper()
	for _, r := range ran {
		if strings.HasPrefix(r, "ufw ") || strings.Contains(r, "ufw.service") && !strings.HasPrefix(r, "systemctl is-") {
			t.Errorf("ran %q on Rocky Linux", r)
		}
	}
}

// TestELInstall: Install masks firewalld.service, installs it through dnf
// without weak dependencies, unmasks it and checks it is disabled: the
// preset never enables it. Its undo unmasks, then removes the package.
// Refused beside ufw.
func TestELInstall(t *testing.T) {
	ctx := context.Background()
	e := &elHost{}
	h, facts := e.site().build(t)
	cs, err := firewall.Incoming{Action: firewall.ActionInstall}.Plan(ctx, h, engine.Input{Facts: facts, Config: &config.Config{}})
	if err != nil || len(cs) != 1 {
		t.Fatalf("plan: %+v, %v", cs, err)
	}
	c := cs[0]
	if c.Risk != engine.RiskPackageInstall || c.Stage != engine.StagePackages || !slices.Equal(c.Commands, []string{"systemctl mask firewalld.service",
		"dnf install -y --setopt=install_weak_deps=False --setopt=exit_on_lock=True firewalld", "systemctl unmask firewalld.service"}) || !strings.Contains(c.StateAfter, "not running") {
		t.Errorf("change %+v", c)
	}
	if len(c.Rollback.Steps) != 2 || c.Rollback.Steps[0].Op != revert.OpRun || strings.Join(c.Rollback.Steps[0].Argv, " ") != "systemctl unmask firewalld.service" ||
		strings.Join(c.Rollback.Steps[1].Argv, " ") != "dnf remove -y --noautoremove --setopt=exit_on_lock=True firewalld" {
		t.Errorf("rollback %+v", c.Rollback)
	}
	ran, err := c.Apply(ctx, h, engine.Run{})
	if err != nil || !e.installed || e.masked || e.enabled != "disabled" || ran.Rollback == nil || len(ran.Rollback.Steps) != 2 {
		t.Errorf("apply: %v; installed %v, masked %v, %s; rollback %+v", err, e.installed, e.masked, e.enabled, ran.Rollback)
	}
	noUFWRun(t, e.ran)
	for _, c := range []struct {
		name string
		e    elHost
		code string
	}{
		{"beside ufw", elHost{ufw: true}, "FIREWALLD_OTHER_FIREWALL"},
		{"iptables active", elHost{other: "iptables.service"}, "FIREWALLD_OTHER_FIREWALL"},
		{"the operator's mask", elHost{masked: true}, "FIREWALLD_BOOT_BLOCKED"},
	} {
		e := c.e
		h, facts := e.site().build(t)
		if _, err := (firewall.Incoming{Action: firewall.ActionInstall}).Plan(ctx, h, engine.Input{Facts: facts, Config: &config.Config{}}); output.CodeOf(err) != c.code {
			t.Errorf("%s: %v (%s), want %s", c.name, err, output.CodeOf(err), c.code)
		}
		if slices.ContainsFunc(e.ran, func(r string) bool { return strings.HasPrefix(r, "dnf ") || strings.Contains(r, "mask ") }) {
			t.Errorf("%s: ran %q before the refusal", c.name, e.ran)
		}
	}
	// dnf fails: the unit is unmasked all the same, and the undo holds the
	// unmask and the remove.
	e = &elHost{failInstall: true}
	h, facts = e.site().build(t)
	cs, err = firewall.Incoming{Action: firewall.ActionInstall}.Plan(ctx, h, engine.Input{Facts: facts, Config: &config.Config{}})
	if err != nil || len(cs) != 1 {
		t.Fatalf("plan: %+v, %v", cs, err)
	}
	if ran, err := cs[0].Apply(ctx, h, engine.Run{}); err == nil || e.masked || ran.Rollback == nil || len(ran.Rollback.Steps) != 2 {
		t.Errorf("dnf failed: %v; masked %v; rollback %+v", err, e.masked, ran.Rollback)
	}
}

// TestELEnable: from stopped and disabled, one lockout-risk change whose
// Rollback (what the engine journals before Apply) is the firewalld-enable
// plan, planned with reads alone: no start, no add, no firewall-cmd;
// running and enabled is set, nothing; running and not at boot, not
// installed, masked, CleanupOnExit off, a bound zone without SSH and
// another firewall are refused, each with nothing run but reads.
func TestELEnable(t *testing.T) {
	ctx := context.Background()
	in := func(facts detect.Facts) engine.Input {
		return engine.Input{Facts: facts, Config: &config.Config{}, Session: revert.Session{Remote: true, Conn: "10.2.8.2 61567 10.2.8.140 22"}}
	}
	e := &elHost{installed: true, active: "inactive", enabled: "disabled"}
	h, facts := e.site().build(t)
	cs, err := firewall.Incoming{Action: firewall.ActionEnable}.Plan(ctx, h, in(facts))
	if err != nil || len(cs) != 1 {
		t.Fatalf("plan: %+v, %v", cs, err)
	}
	c := cs[0]
	if c.Risk != engine.RiskLockout || c.Stage != engine.StageFirewall || len(c.Rollback.Steps) != 1 || c.Rollback.Steps[0].Op != revert.OpPlan ||
		c.Rollback.Steps[0].Unit != "firewalld-enable" || !slices.Equal(c.Commands, []string{"systemctl start firewalld.service", "systemctl enable firewalld.service"}) ||
		!slices.Contains(c.Resources, "sshd") || !slices.Contains(c.Resources, "firewalld") {
		t.Errorf("change %+v", c)
	}
	for _, r := range e.ran {
		if strings.HasPrefix(r, "firewall-cmd ") || strings.Contains(r, " start ") || strings.Contains(r, " enable ") || strings.Contains(r, "--add-port") {
			t.Errorf("planning ran %q", r)
		}
	}
	noUFWRun(t, e.ran)
	for _, c := range []struct {
		name string
		e    elHost
		code string
	}{
		{"running, enabled", elHost{installed: true, active: "active", enabled: "enabled"}, ""},
		{"running, not at boot", elHost{installed: true, active: "active", enabled: "disabled"}, "FIREWALLD_NOT_AT_BOOT"},
		{"not installed", elHost{}, "FIREWALLD_NOT_INSTALLED"},
		{"masked", elHost{installed: true, active: "inactive", masked: true}, "FIREWALLD_BOOT_BLOCKED"},
		{"CleanupOnExit off", elHost{installed: true, active: "inactive", enabled: "disabled", noCleanup: true}, "FIREWALLD_NO_CLEANUP"},
		{"NetworkManager binds eth0 to drop", elHost{installed: true, active: "inactive", enabled: "disabled", nmZone: "drop"}, "FIREWALLD_ZONE_NO_SSH"},
		{"another firewall active", elHost{installed: true, active: "inactive", enabled: "disabled", other: "nftables.service"}, "FIREWALLD_OTHER_FIREWALL"},
		{"failed", elHost{installed: true, active: "failed", enabled: "enabled"}, "FIREWALLD_FAILED"},
	} {
		e := c.e
		h, facts := e.site().build(t)
		if c.e.active == "active" {
			h.Respond(hostfake.Response{Stdout: "running\n"}, "firewall-cmd", "--state")
			h.Respond(hostfake.Response{Stdout: "public\n"}, "firewall-cmd", "--get-default-zone")
			h.Respond(hostfake.Response{Stdout: "public (default)\n  interfaces: eth0\n"}, "firewall-cmd", "--get-active-zones")
		}
		cs, err := firewall.Incoming{Action: firewall.ActionEnable}.Plan(ctx, h, in(facts))
		if output.CodeOf(err) != c.code || c.code == "" && (err != nil || len(cs) != 0) {
			t.Errorf("%s: %+v, %v (%s), want %q", c.name, cs, err, output.CodeOf(err), c.code)
		}
	}
	e = &elHost{installed: true, active: "inactive", enabled: "disabled"}
	h, facts = e.site().build(t)
	f := in(facts)
	f.Session.Conn = ""
	if _, err := (firewall.Incoming{Action: firewall.ActionEnable}).Plan(ctx, h, f); output.CodeOf(err) != "UFW_LISTENERS_UNKNOWN" {
		t.Errorf("a remote session's port unread: %v (%s)", err, output.CodeOf(err))
	}
}

// TestELDisable: running and enabled, a plain change that disables and
// stops, its undo an enable only; not installed, nothing.
func TestELDisable(t *testing.T) {
	ctx := context.Background()
	e := &elHost{installed: true, active: "active", enabled: "enabled"}
	h, facts := e.site().build(t)
	h.Respond(hostfake.Response{Stdout: "running\n"}, "firewall-cmd", "--state")
	h.Respond(hostfake.Response{Stdout: "public\n"}, "firewall-cmd", "--get-default-zone")
	h.Respond(hostfake.Response{Stdout: "public (default)\n  interfaces: eth0\n"}, "firewall-cmd", "--get-active-zones")
	cs, err := firewall.Incoming{Action: firewall.ActionDisable}.Plan(ctx, h, engine.Input{Facts: facts, Config: &config.Config{}})
	if err != nil || len(cs) != 1 || cs[0].Risk != engine.RiskPlain || len(cs[0].Rollback.Steps) != 1 ||
		strings.Join(cs[0].Rollback.Steps[0].Argv, " ") != "systemctl enable firewalld.service" {
		t.Fatalf("plan %+v, %v", cs, err)
	}
	if _, err := cs[0].Apply(ctx, h, engine.Run{}); err != nil || e.active != "inactive" || e.enabled != "disabled" {
		t.Errorf("apply: %v; %s, %s", err, e.active, e.enabled)
	}
	e = &elHost{}
	h, facts = e.site().build(t)
	if cs, err := (firewall.Incoming{Action: firewall.ActionDisable}).Plan(ctx, h, engine.Input{Facts: facts, Config: &config.Config{}}); err != nil || len(cs) != 0 {
		t.Errorf("not installed: %+v, %v", cs, err)
	}
}

// TestELRead: item 1 follows firewalld: Install where it is missing (not
// planned on the read: dnf may refresh its metadata), Enable where it is
// stopped, with Disable beside it where it starts at boot, Disable where it
// runs.
func TestELRead(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name    string
		e       elHost
		first   firewall.Item
		beside  bool
		planned bool
	}{
		{"missing", elHost{}, firewall.ItemInstall, false, false},
		{"stopped, disabled", elHost{installed: true, active: "inactive", enabled: "disabled"}, firewall.ItemEnable, false, true},
		{"stopped, starts at boot", elHost{installed: true, active: "inactive", enabled: "enabled"}, firewall.ItemEnable, true, true},
		{"running", elHost{installed: true, active: "active", enabled: "enabled"}, firewall.ItemDisable, false, false},
	} {
		e := c.e
		h, facts := e.site().build(t)
		if e.active == "active" {
			h.Respond(hostfake.Response{Stdout: "running\n"}, "firewall-cmd", "--state")
			h.Respond(hostfake.Response{Stdout: "public\n"}, "firewall-cmd", "--get-default-zone")
			h.Respond(hostfake.Response{Stdout: "public (default)\n  interfaces: eth0\n"}, "firewall-cmd", "--get-active-zones")
		}
		r := firewall.Read(ctx, h, facts, &config.Config{}, revert.Session{Remote: true, Conn: "10.2.8.2 61567 10.2.8.140 22"})
		if !r.EL || r.First != c.first || r.DisableBeside != c.beside || (len(r.Plan) > 0) != c.planned || r.PlanErr != nil {
			t.Errorf("%s: first %v, beside %v, plan %+v, %v", c.name, r.First, r.DisableBeside, r.Plan, r.PlanErr)
		}
		for _, cmd := range e.ran {
			if strings.HasPrefix(cmd, "dnf ") || strings.HasPrefix(cmd, "systemctl start") {
				t.Errorf("%s: the read ran %q", c.name, cmd)
			}
		}
		noUFWRun(t, e.ran)
	}
}

// TestELReadEdges: ss missing while firewalld runs leaves item 1 Disable,
// which reads no sockets; stopped, it is Install iproute, with Disable
// beside it where firewalld starts at boot; a failed firewalld is item 1
// Enable with FIREWALLD_FAILED's refusal, said before anything is asked.
func TestELReadEdges(t *testing.T) {
	ctx := context.Background()
	session := revert.Session{Remote: true, Conn: "10.2.8.2 61567 10.2.8.140 22"}
	for _, c := range []struct {
		name   string
		e      elHost
		noSS   bool
		first  firewall.Item
		beside bool
		code   string
	}{
		{"running, no ss", elHost{installed: true, active: "active", enabled: "enabled"}, true, firewall.ItemDisable, false, ""},
		{"stopped at boot, no ss", elHost{installed: true, active: "inactive", enabled: "enabled"}, true, firewall.ItemInstallSS, true, ""},
		{"failed", elHost{installed: true, active: "failed", enabled: "enabled"}, false, firewall.ItemEnable, false, "FIREWALLD_FAILED"},
	} {
		e := c.e
		st := e.site()
		st.plat.noSS = c.noSS
		h, facts := st.build(t)
		if e.active == "active" {
			h.Respond(hostfake.Response{Stdout: "running\n"}, "firewall-cmd", "--state")
			h.Respond(hostfake.Response{Stdout: "public\n"}, "firewall-cmd", "--get-default-zone")
			h.Respond(hostfake.Response{Stdout: "public (default)\n  interfaces: eth0\n"}, "firewall-cmd", "--get-active-zones")
		}
		r := firewall.Read(ctx, h, facts, &config.Config{}, session)
		if r.First != c.first || r.DisableBeside != c.beside || output.CodeOf(r.PlanErr) != c.code && c.code != "" {
			t.Errorf("%s: first %v, beside %v, %v (%s)", c.name, r.First, r.DisableBeside, r.PlanErr, output.CodeOf(r.PlanErr))
		}
	}
}
