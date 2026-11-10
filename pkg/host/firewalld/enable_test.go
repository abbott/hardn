package firewalld_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/firewalld"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
)

// model is firewalld on Rocky Linux as the Enable and Disable read and
// change it: its unit, its permanent zones (public the default, the
// package's stock services), the runtime it loads at its start, and
// NetworkManager's binding of eth0.
type model struct {
	h               *hostfake.System
	active, enabled string // systemctl's words
	readyAfter      int    // firewall-cmd --state's "not running" after a start (no Type=: active from the fork)
	failStart       bool   // the start ends "failed"
	failStop        bool
	stuckStop       bool // the stop answers 0, and firewalld stays active
	failDisable     bool
	failedStop      bool              // the stop leaves the unit "failed" (a kill at TimeoutStopSec)
	failReset       bool              // reset-failed fails
	nmDown          bool              // nmcli: NetworkManager is not running (exit 8)
	zones           map[string]*mzone // permanent
	runtime         map[string]*mzone // loaded at the start
	nm              string            // eth0's connection.zone
	others          map[string]string // other units: "active" or is-enabled's word
	sshPorts        string            // the ssh service's ports
	ran             []string
}

type mzone struct {
	target             string
	services, ports    []string
	interfaces, source []string
}

func (z *mzone) clone() *mzone {
	c := *z
	c.services, c.ports = slices.Clone(z.services), slices.Clone(z.ports)
	return &c
}

func newModel(t *testing.T) *model {
	m := &model{h: hostfake.New(host.Options{}), active: "inactive", enabled: "disabled", sshPorts: "22/tcp",
		zones: map[string]*mzone{
			"public":   {target: "default", services: []string{"cockpit", "dhcpv6-client", "ssh"}},
			"trusted":  {target: "ACCEPT"},
			"drop":     {target: "DROP"},
			"internal": {target: "default", services: []string{"ssh"}},
		}}
	m.h.Handler = m.answer
	m.h.AddFile(firewalld.ConfFile, []byte("DefaultZone=public\nCleanupOnExit=yes\n"), 0o644, 0, 0)
	t.Cleanup(firewalld.NoPause(50 * time.Millisecond))
	return m
}

func yesNo(ok bool) hostfake.Response {
	if ok {
		return hostfake.Response{Stdout: "yes\n"}
	}
	return hostfake.Response{Stdout: "no\n", ExitCode: 1}
}

func (m *model) answer(argv []string, _ []byte) hostfake.Response {
	cmd := strings.Join(argv, " ")
	m.ran = append(m.ran, cmd)
	word := func(w string, ok bool) hostfake.Response {
		if ok {
			return hostfake.Response{Stdout: w + "\n"}
		}
		return hostfake.Response{Stdout: w + "\n", ExitCode: 3}
	}
	switch cmd {
	case "systemctl is-active firewalld.service":
		if m.active == "starting" {
			if m.failStart {
				m.active = "failed"
				return word(m.active, false)
			}
			return word("active", true)
		}
		return word(m.active, m.active == "active")
	case "systemctl is-enabled firewalld.service":
		return word(m.enabled, m.enabled == "enabled")
	case "systemctl start firewalld.service":
		m.active = "starting"
		return hostfake.Response{}
	case "systemctl stop firewalld.service":
		if m.failStop {
			return hostfake.Response{Stderr: "Job failed\n", ExitCode: 1}
		}
		if m.stuckStop {
			return hostfake.Response{}
		}
		if m.failedStop || m.active == "failed" { // systemd keeps "failed" through a stop
			m.active = "failed"
			return hostfake.Response{}
		}
		m.active, m.runtime = "inactive", nil
		return hostfake.Response{}
	case "systemctl reset-failed firewalld.service":
		if m.failReset {
			return hostfake.Response{Stderr: "Failed\n", ExitCode: 1}
		}
		if m.active == "failed" {
			m.active = "inactive"
		}
		return hostfake.Response{}
	case "systemctl enable firewalld.service":
		m.enabled = "enabled"
		return hostfake.Response{}
	case "systemctl disable firewalld.service":
		if m.failDisable {
			return hostfake.Response{Stderr: "Failed to disable\n", ExitCode: 1}
		}
		m.enabled = "disabled"
		return hostfake.Response{}
	case "firewall-offline-cmd --get-default-zone":
		return hostfake.Response{Stdout: "public\n"}
	case "firewall-offline-cmd --list-all-zones":
		var b strings.Builder
		for _, n := range slices.Sorted(maps.Keys(m.zones)) {
			z := m.zones[n]
			head := n
			if n == "public" {
				head += " (default)"
			}
			fmt.Fprintf(&b, "%s\n  target: %s\n  interfaces: %s\n  sources: %s\n  services: %s\n  ports: %s\n  rich rules: \n\n",
				head, z.target, strings.Join(z.interfaces, " "), strings.Join(z.source, " "), strings.Join(z.services, " "), strings.Join(z.ports, " "))
		}
		return hostfake.Response{Stdout: b.String()}
	case "firewall-cmd --state":
		if m.active == "starting" && !m.failStart {
			if m.readyAfter > 0 { // not yet on D-Bus
				m.readyAfter--
				return hostfake.Response{Stdout: "not running\n", ExitCode: 252}
			}
			m.active, m.runtime = "active", map[string]*mzone{}
			for n, z := range m.zones {
				m.runtime[n] = z.clone()
			}
		}
		if m.active == "active" {
			return hostfake.Response{Stdout: "running\n"}
		}
		return hostfake.Response{Stdout: "not running\n", ExitCode: 252}
	case "firewall-cmd --get-default-zone":
		return hostfake.Response{Stdout: "public\n"}
	case "firewall-cmd --get-active-zones":
		z := "public"
		if m.nm != "" {
			z = m.nm
		}
		if z == "public" {
			return hostfake.Response{Stdout: "public (default)\n  interfaces: eth0\n"}
		}
		return hostfake.Response{Stdout: z + "\n  interfaces: eth0\npublic (default)\n"}
	case "nmcli -t -f UUID,DEVICE connection show --active":
		if m.nmDown {
			return hostfake.Response{Stderr: "Error: NetworkManager is not running.\n", ExitCode: 8}
		}
		return hostfake.Response{Stdout: "1dd9a779-d327-56e1-8454-c65e2556c12c:eth0\nb68b82fb-125b-430d-9e06-78b20406182d:lo\n"}
	case "nmcli -g connection.zone connection show 1dd9a779-d327-56e1-8454-c65e2556c12c":
		return hostfake.Response{Stdout: m.nm + "\n"}
	case "nmcli -g connection.zone connection show b68b82fb-125b-430d-9e06-78b20406182d":
		return hostfake.Response{Stdout: "\n"}
	}
	if svc, ok := strings.CutPrefix(cmd, "firewall-offline-cmd --service="); ok {
		svc, _ = strings.CutSuffix(svc, " --get-ports")
		return hostfake.Response{Stdout: map[string]string{"ssh": m.sshPorts, "cockpit": "9090/tcp", "dhcpv6-client": "546/udp"}[svc] + "\n"}
	}
	if svc, ok := strings.CutPrefix(cmd, "firewall-cmd --permanent --service="); ok {
		svc, _ = strings.CutSuffix(svc, " --get-ports")
		return hostfake.Response{Stdout: map[string]string{"ssh": m.sshPorts, "cockpit": "9090/tcp", "dhcpv6-client": "546/udp"}[svc] + "\n"}
	}
	if len(argv) == 3 && argv[0] == "systemctl" && m.others != nil {
		w, ok := m.others[argv[2]]
		if argv[1] == "is-active" && ok && w == "active" {
			return word("active", true)
		}
		if argv[1] == "is-enabled" && ok && w != "active" {
			return word(w, w == "enabled")
		}
	}
	if len(argv) == 3 && argv[0] == "systemctl" && (argv[1] == "is-active" || argv[1] == "is-enabled") {
		if argv[1] == "is-active" {
			return word("inactive", false)
		}
		return hostfake.Response{Stdout: "not-found\n", ExitCode: 4}
	}
	// Zone commands: firewall-offline-cmd (permanent), firewall-cmd
	// [--permanent] (runtime unless --permanent).
	args, zones := argv[1:], m.zones
	if argv[0] == "firewall-cmd" {
		zones = m.runtime
		if len(args) > 0 && args[0] == "--permanent" {
			args, zones = args[1:], m.zones
		}
	}
	if len(args) == 2 && strings.HasPrefix(args[0], "--zone=") && zones != nil {
		z := zones[strings.TrimPrefix(args[0], "--zone=")]
		if z == nil {
			return hostfake.Response{Stdout: "Error: INVALID_ZONE\n", ExitCode: 112}
		}
		op, val, _ := strings.Cut(args[1], "=")
		switch op {
		case "--get-target":
			return hostfake.Response{Stdout: z.target + "\n"}
		case "--list-services":
			return hostfake.Response{Stdout: strings.Join(z.services, " ") + "\n"}
		case "--query-port":
			return yesNo(slices.Contains(z.ports, val))
		case "--add-port":
			z.ports = append(z.ports, val)
			if argv[0] == "firewall-offline-cmd" {
				m.h.AddFile(firewalld.ZoneDir+"/"+strings.TrimPrefix(args[0], "--zone=")+".xml", []byte("<zone/>\n"), 0o644, 0, 0)
			}
			return hostfake.Response{Stdout: "success\n"}
		case "--remove-port":
			z.ports = slices.DeleteFunc(z.ports, func(p string) bool { return p == val })
			return hostfake.Response{Stdout: "success\n"}
		}
	}
	return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
}

func argvsEqual(a, b [][]string) bool {
	return slices.EqualFunc(a, b, func(x, y []string) bool { return slices.Equal(x, y) })
}

var stopped = firewalld.State{Installed: detect.Yes, Running: detect.No, Enabled: detect.No, Unit: "disabled"}

// TestPlanEnable: from a stopped firewalld, port 22 needs nothing where
// the default zone has ssh; 2230 is added to the default zone offline,
// and the zone file it creates is the undo's; a bound zone is checked,
// and one that doesn't let SSH through is refused, as are a masked unit,
// an unknown word, CleanupOnExit off and another firewall. It reads only,
// and runs no firewall-cmd.
func TestPlanEnable(t *testing.T) {
	ctx := context.Background()
	m := newModel(t)
	p, err := firewalld.PlanEnable(ctx, m.h, stopped, []uint16{22})
	if err != nil || len(p.Steps) != 0 || !p.Enable || p.ZoneFile != "" || !slices.Equal(p.Zones, []string{"public"}) ||
		!argvsEqual(p.Commands(), [][]string{{"systemctl", "start", "firewalld.service"}, {"systemctl", "enable", "firewalld.service"}}) {
		t.Errorf("22: %+v, %v", p, err)
	}
	p, err = firewalld.PlanEnable(ctx, m.h, stopped, []uint16{22, 2230})
	if err != nil || !slices.Equal(p.Steps, []firewalld.Step{{Zone: "public", Port: 2230, Permanent: true}}) || p.ZoneFile != "/etc/firewalld/zones/public.xml" ||
		!slices.Equal(p.Commands()[0], []string{"firewall-offline-cmd", "--zone=public", "--add-port=2230/tcp"}) {
		t.Errorf("2230: %+v, %v", p, err)
	}
	if u := p.Undo(); !u.Start || !u.Enable || u.ZoneFile != p.ZoneFile || !slices.Equal(u.Ports, p.Steps) {
		t.Errorf("undo %+v", u)
	}
	m.h.AddFile("/etc/firewalld/zones/public.xml", []byte("<zone/>\n"), 0o644, 0, 0)
	if p, err := firewalld.PlanEnable(ctx, m.h, firewalld.State{Installed: detect.Yes, Running: detect.No, Unit: "enabled"}, []uint16{2230}); err != nil || p.ZoneFile != "" || p.Enable {
		t.Errorf("the zone file there, enabled: %+v, %v", p, err)
	}
	for _, r := range m.ran {
		if strings.HasPrefix(r, "firewall-cmd ") || strings.Contains(r, " start ") || strings.Contains(r, "--add-port") {
			t.Errorf("planning ran %q", r)
		}
	}
	m.sshPorts = "2222/tcp" // an ssh service of the operator's
	if p, err := firewalld.PlanEnable(ctx, m.h, stopped, []uint16{22}); err != nil || len(p.Steps) != 1 {
		t.Errorf("ssh is 2222: %+v, %v", p, err)
	}
	m.sshPorts = "22/tcp"
	for _, c := range []struct {
		name string
		st   firewalld.State
		edit func(m *model)
		want error
	}{
		{"running", firewalld.State{Installed: detect.Yes, Running: detect.Yes, Unit: "enabled"}, nil, fs.ErrInvalid},
		{"masked", firewalld.State{Installed: detect.Yes, Running: detect.No, Unit: "masked"}, nil, firewalld.ErrBootBlocked},
		{"static", firewalld.State{Installed: detect.Yes, Running: detect.No, Unit: "static"}, nil, firewalld.ErrBootUnknown},
		{"no clean-up", stopped, func(m *model) {
			m.h.AddFile(firewalld.ConfFile, []byte("CleanupOnExit=No\n"), 0o644, 0, 0)
		}, firewalld.ErrNoCleanup},
		{"iptables active", stopped, func(m *model) { m.others = map[string]string{"iptables.service": "active"} }, firewalld.ErrOtherFirewall},
		{"nftables enabled", stopped, func(m *model) { m.others = map[string]string{"nftables.service": "enabled"} }, firewalld.ErrOtherFirewall},
		{"NetworkManager binds eth0 to drop", stopped, func(m *model) { m.nm = "drop" }, firewalld.ErrZoneNoSSH},
		{"a zone file binds a source to internal, no 2230", stopped, func(m *model) { m.zones["internal"].source = []string{"10.0.0.0/8"} }, firewalld.ErrZoneNoSSH},
	} {
		m := newModel(t)
		if c.edit != nil {
			c.edit(m)
		}
		if _, err := firewalld.PlanEnable(ctx, m.h, c.st, []uint16{22, 2230}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	m = newModel(t)
	m.nmDown, m.nm = true, "drop" // NetworkManager down binds nothing
	if p, err := firewalld.PlanEnable(ctx, m.h, stopped, []uint16{22}); err != nil || !slices.Equal(p.Zones, []string{"public"}) {
		t.Errorf("NetworkManager not running: %+v, %v", p, err)
	}
	m = newModel(t)
	m.nm = "trusted" // accepts everything: nothing to add, nothing refused
	if p, err := firewalld.PlanEnable(ctx, m.h, stopped, []uint16{2230}); err != nil || !slices.Equal(p.Zones, []string{"public", "trusted"}) {
		t.Errorf("trusted: %+v, %v", p, err)
	}
}

// TestExecuteEnable: the adds, the start, a wait while firewalld comes up,
// the runtime check of every zone, the enable; never up is ErrNotReady,
// and a failed start ends the wait at once; a zone not letting SSH
// through after the start is ErrNotLetThrough, with no enable after it.
func TestExecuteEnable(t *testing.T) {
	ctx := context.Background()
	m := newModel(t)
	m.readyAfter = 3
	p, err := firewalld.PlanEnable(ctx, m.h, stopped, []uint16{22, 2230})
	if err != nil {
		t.Fatal(err)
	}
	if err := firewalld.ExecuteEnable(ctx, m.h, p); err != nil || m.active != "active" || m.enabled != "enabled" || !slices.Contains(m.runtime["public"].ports, "2230/tcp") {
		t.Fatalf("enable: %v; %s, %s, runtime public %+v", err, m.active, m.enabled, m.runtime["public"])
	}
	m = newModel(t)
	m.readyAfter = 1 << 30
	p, _ = firewalld.PlanEnable(ctx, m.h, stopped, []uint16{22})
	if err := firewalld.ExecuteEnable(ctx, m.h, p); !errors.Is(err, firewalld.ErrNotReady) || output.CodeOf(err) != "FIREWALLD_NOT_READY" || m.enabled != "disabled" {
		t.Errorf("never up: %v (%s), %s", err, output.CodeOf(err), m.enabled)
	}
	m = newModel(t)
	m.failStart = true
	defer firewalld.NoPause(10 * time.Second)() // a wait that ran to its end would take this long
	p, _ = firewalld.PlanEnable(ctx, m.h, stopped, []uint16{22})
	start := time.Now()
	if err := firewalld.ExecuteEnable(ctx, m.h, p); !errors.Is(err, firewalld.ErrNotReady) || !strings.Contains(err.Error(), "failed") || time.Since(start) > 2*time.Second {
		t.Errorf("failed start: %v after %v", err, time.Since(start))
	}
	// The failed start rolled back: the unit reset, then stopped and
	// disabled, the undo finished.
	if err := firewalld.RollbackEnable(ctx, m.h, p.Undo()); err != nil || m.active != "inactive" || m.enabled != "disabled" || !slices.Contains(m.ran, "systemctl reset-failed firewalld.service") {
		t.Errorf("rollback of a failed start: %v; %s, %s, ran %q", err, m.active, m.enabled, m.ran)
	}
	m = newModel(t)
	p, _ = firewalld.PlanEnable(ctx, m.h, stopped, []uint16{22})
	m.nm = "drop" // NetworkManager binds eth0 after the plan read it
	if err := firewalld.ExecuteEnable(ctx, m.h, p); !errors.Is(err, firewalld.ErrNotLetThrough) || m.enabled != "disabled" {
		t.Errorf("drop after the plan: %v, %s", err, m.enabled)
	}
}

// TestRollbackEnable: the stop, then the disable, then the ports: the
// zone file the add created removed with its .old, or, where the file was
// there before, each port taken away offline. Where the stop or the
// disable fails, or firewalld was enabled before, the ports stay: they are
// what lets SSH through at the next boot. Nothing is ever started, and no
// firewall-cmd runs.
func TestRollbackEnable(t *testing.T) {
	ctx := context.Background()
	enable := func(m *model, st firewalld.State) firewalld.EnablePlan {
		t.Helper()
		p, err := firewalld.PlanEnable(ctx, m.h, st, []uint16{22, 2230})
		if err != nil {
			t.Fatal(err)
		}
		if err := firewalld.ExecuteEnable(ctx, m.h, p); err != nil {
			t.Fatal(err)
		}
		if p.ZoneFile != "" {
			m.h.AddFile(p.ZoneFile+".old", []byte("<zone/>\n"), 0o644, 0, 0) // a second write's copy
		}
		m.ran = nil
		return p
	}
	noStartNoCmd := func(name string, ran []string) {
		for _, r := range ran {
			if strings.Contains(r, " start ") || strings.HasPrefix(r, "firewall-cmd ") {
				t.Errorf("%s: the rollback ran %q", name, r)
			}
		}
	}
	m := newModel(t)
	p := enable(m, stopped)
	if err := firewalld.RollbackEnable(ctx, m.h, p.Undo()); err != nil || p.ZoneFile == "" || !p.ZoneOld {
		t.Fatal(err, p.ZoneFile, p.ZoneOld)
	}
	_, file := m.h.Entry(p.ZoneFile)
	_, old := m.h.Entry(p.ZoneFile + ".old")
	if file || old || m.active != "inactive" || m.enabled != "disabled" {
		t.Errorf("created zone file: %s, %s, zone file left %v, .old left %v", m.active, m.enabled, file, old)
	}
	noStartNoCmd("created zone file", m.ran)

	m = newModel(t)
	m.h.AddFile(firewalld.ZoneDir+"/public.xml", []byte("<zone/>\n"), 0o644, 0, 0) // the operator's
	p = enable(m, stopped)
	if err := firewalld.RollbackEnable(ctx, m.h, p.Undo()); err != nil || p.ZoneFile != "" {
		t.Fatal(err, p.ZoneFile)
	}
	if _, ok := m.h.Entry(firewalld.ZoneDir + "/public.xml"); !ok || slices.Contains(m.zones["public"].ports, "2230/tcp") || m.enabled != "disabled" {
		t.Errorf("the operator's zone file: kept %v, public %+v, %s", ok, m.zones["public"], m.enabled)
	}
	noStartNoCmd("the operator's zone file", m.ran)

	for _, c := range []struct {
		name string
		edit func(m *model)
		st   firewalld.State
		err  bool
	}{
		{"stop failed", func(m *model) { m.failStop = true }, stopped, true},
		{"stop left it active", func(m *model) { m.stuckStop = true }, stopped, true},
		{"stop left it failed, reset-failed fails", func(m *model) { m.failedStop, m.failReset = true, true }, stopped, true},
		{"disable failed", func(m *model) { m.failDisable = true }, stopped, true},
		{"enabled before", func(m *model) {}, firewalld.State{Installed: detect.Yes, Running: detect.No, Enabled: detect.Yes, Unit: "enabled"}, false},
	} {
		m := newModel(t)
		if c.st.Unit == "enabled" {
			m.enabled = "enabled"
		}
		p := enable(m, c.st)
		c.edit(m)
		err := firewalld.RollbackEnable(ctx, m.h, p.Undo())
		_, file := m.h.Entry(p.ZoneFile)
		if (err != nil) != c.err || !file || !slices.Contains(m.zones["public"].ports, "2230/tcp") {
			t.Errorf("%s: %v; zone file kept %v, public %+v", c.name, err, file, m.zones["public"])
		}
		noStartNoCmd(c.name, m.ran)
	}
	for _, u := range []firewalld.EnableUndo{
		{Ports: []firewalld.Step{{Zone: "public", Port: 2230}}},
		{ZoneFile: "/etc/passwd"},
		{ZoneFile: "/etc/firewalld/zones/a b.xml"},
		{ZoneFile: "/etc/firewalld/zones/public.xml"}, // no ports
		{ZoneFile: "/etc/firewalld/zones/drop.xml", Ports: []firewalld.Step{{Zone: "public", Port: 2230, Permanent: true}}},
		{ZoneOld: true},
	} {
		if err := u.Check(); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("%+v: %v", u, err)
		}
	}
}

// TestDisable: running and enabled disables and stops, its undo enables
// and never starts; stopped and starting at boot only disables;
// CleanupOnExit off refuses a stop.
func TestDisable(t *testing.T) {
	ctx := context.Background()
	m := newModel(t)
	m.active, m.enabled = "active", "enabled"
	p, err := firewalld.PlanDisable(ctx, m.h, firewalld.State{Installed: detect.Yes, Running: detect.Yes, Unit: "enabled"})
	if err != nil || !p.Disable || !p.Stop || !argvsEqual(p.Undo(), [][]string{{"systemctl", "enable", "firewalld.service"}}) {
		t.Fatalf("plan %+v, %v", p, err)
	}
	if err := firewalld.ExecuteDisable(ctx, m.h, p); err != nil || m.active != "inactive" || m.enabled != "disabled" {
		t.Errorf("execute: %v; %s, %s", err, m.active, m.enabled)
	}
	if p, err := firewalld.PlanDisable(ctx, m.h, firewalld.State{Installed: detect.Yes, Running: detect.No, Unit: "enabled"}); err != nil || !p.Disable || p.Stop {
		t.Errorf("stopped, enabled: %+v, %v", p, err)
	}
	m.h.AddFile(firewalld.ConfFile, []byte("CleanupOnExit=false\n"), 0o644, 0, 0)
	if _, err := firewalld.PlanDisable(ctx, m.h, firewalld.State{Installed: detect.Yes, Running: detect.Yes, Unit: "enabled"}); !errors.Is(err, firewalld.ErrNoCleanup) {
		t.Errorf("no clean-up: %v", err)
	}
	for name, edit := range map[string]func(m *model){
		"the stop failed":         func(m *model) { m.failStop = true },
		"the stop left it active": func(m *model) { m.stuckStop = true },
		"the stop left it failed": func(m *model) { m.failedStop = true }, // a kill at TimeoutStopSec: no clean-up
	} {
		m = newModel(t)
		m.active, m.enabled = "active", "enabled"
		edit(m)
		p, _ = firewalld.PlanDisable(ctx, m.h, firewalld.State{Installed: detect.Yes, Running: detect.Yes, Unit: "enabled"})
		if err := firewalld.ExecuteDisable(ctx, m.h, p); err == nil {
			t.Errorf("%s: nil", name)
		}
	}
}

// TestCleanupOnExit: no file, the key missing, yes, and no or false in any
// case or quoted.
func TestCleanupOnExit(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New(host.Options{})
	if on, err := firewalld.CleanupOnExit(ctx, h); !on || err != nil {
		t.Errorf("no file: %v, %v", on, err)
	}
	for conf, want := range map[string]bool{"DefaultZone=public\n": true, "CleanupOnExit=yes\n": true, "CleanupOnExit=no\n": false,
		"CleanupOnExit = FALSE\n": false, "CleanupOnExit=\"no\"\n": false, "#CleanupOnExit=no\nCleanupOnExit=yes\n": true} {
		h.AddFile(firewalld.ConfFile, []byte(conf), 0o644, 0, 0)
		if on, err := firewalld.CleanupOnExit(ctx, h); on != want || err != nil {
			t.Errorf("%q: %v, %v", conf, on, err)
		}
	}
}
