package firewalld_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/firewalld"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

func respond(h *hostfake.System, r hostfake.Response, argv ...string) {
	h.Respond(r, argv[0], argv[1:]...)
}

var (
	yes = hostfake.Response{Stdout: "yes\n"}
	no  = hostfake.Response{Stdout: "no\n", ExitCode: 1}
)

// unitIs scripts systemctl is-active and is-enabled of firewalld.service,
// with systemctl's exits.
func unitIs(h *hostfake.System, active, enabled string) {
	respond(h, hostfake.Response{Stdout: active + "\n", ExitCode: map[bool]int{false: 3}[active == "active"]}, "systemctl", "is-active", "firewalld.service")
	respond(h, hostfake.Response{Stdout: enabled + "\n", ExitCode: map[bool]int{false: 1}[enabled == "enabled"]}, "systemctl", "is-enabled", "firewalld.service")
}

// missing252 scripts firewalld.service as systemd 252 answers for a unit it
// knows no file of (alma9-test-111, firewalld not installed): is-active
// inactive, exit 3; is-enabled nothing on stdout, exit 1, the line on stderr.
func missing252(h *hostfake.System) {
	respond(h, hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, "systemctl", "is-active", "firewalld.service")
	respond(h, hostfake.Response{Stderr: "Failed to get unit file state for firewalld.service: No such file or directory\n", ExitCode: 1}, "systemctl", "is-enabled", "firewalld.service")
}

// running scripts firewalld running, public the default zone.
func running(h *hostfake.System, enabled, active string) {
	unitIs(h, "active", enabled)
	respond(h, hostfake.Response{Stdout: "running\n"}, "firewall-cmd", "--state")
	respond(h, hostfake.Response{Stdout: "public\n"}, "firewall-cmd", "--get-default-zone")
	respond(h, hostfake.Response{Stdout: active}, "firewall-cmd", "--get-active-zones")
}

// TestRead: systemd's words first, and firewall-cmd only where
// firewalld.service is active; the zones of a running firewalld, with and
// without newer firewalld's " (default)".
func TestRead(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New(host.Options{})
	unitIs(h, "inactive", "not-found")
	if st, err := firewalld.Read(ctx, h); err != nil || st.Installed != detect.No || st.Running != detect.No || len(h.Runs()) != 2 {
		t.Errorf("missing: %+v, %v, ran %q", st, err, h.Runs())
	}
	h = hostfake.New(host.Options{})
	missing252(h)
	if st, err := firewalld.Read(ctx, h); err != nil || st.Installed != detect.No || st.Running != detect.No || st.Unit != "not-found" || len(h.Runs()) != 2 {
		t.Errorf("missing, systemd 252: %+v, %v, ran %q", st, err, h.Runs())
	}
	h = hostfake.New(host.Options{})
	unitIs(h, "inactive", "masked") // a mask of a firewalld not installed
	if st, err := firewalld.Read(ctx, h); err != nil || st.Installed != detect.No || st.Unit != "masked" {
		t.Errorf("masked, no unit file: %+v, %v", st, err)
	}
	for out, want := range map[string]detect.Tri{"disabled": detect.No, "masked": detect.No, "enabled": detect.Yes, "static": detect.Yes, "indirect": detect.Yes, "": detect.Unknown} {
		h = hostfake.New(host.Options{})
		h.AddFile(firewalld.UnitFile, []byte("[Unit]\n"), 0o644, 0, 0)
		unitIs(h, "inactive", out)
		if st, err := firewalld.Read(ctx, h); err != nil || st.Installed != detect.Yes || st.Running != detect.No || st.Enabled != want || st.Unit != out {
			t.Errorf("stopped, %q: %+v, %v, ran %q", out, st, err, h.Runs())
		}
	}
	for _, active := range []string{"public (default)\n  interfaces: eth0\ninternal\n  sources: 10.0.0.0/8\n", "public\n  interfaces: eth0\ninternal\n  sources: 10.0.0.0/8\n"} {
		h = hostfake.New(host.Options{})
		running(h, "enabled", active)
		st, err := firewalld.Read(ctx, h)
		if err != nil || st.Running != detect.Yes || st.Enabled != detect.Yes || st.Default != "public" || !slices.Equal(st.Active, []string{"public", "internal"}) || !slices.Equal(st.Zones(), []string{"public", "internal"}) {
			t.Errorf("running: %+v, %v", st, err)
		}
	}
	h = hostfake.New(host.Options{})
	unitIs(h, "failed", "enabled")
	if _, err := firewalld.Read(ctx, h); !errors.Is(err, firewalld.ErrFailed) || !errors.Is(err, firewalld.ErrUnknown) {
		t.Errorf("failed: %v", err)
	}
	for name, setup := range map[string]func(h *hostfake.System){
		"activating": func(h *hostfake.System) { unitIs(h, "activating", "enabled") },
		"active, its failsafe": func(h *hostfake.System) {
			unitIs(h, "active", "enabled")
			respond(h, hostfake.Response{ExitCode: 251, Stdout: "failed\n"}, "firewall-cmd", "--state")
		},
		"active, not yet on D-Bus": func(h *hostfake.System) {
			unitIs(h, "active", "enabled")
			respond(h, hostfake.Response{ExitCode: 252, Stdout: "not running\n"}, "firewall-cmd", "--state")
		},
	} {
		h = hostfake.New(host.Options{})
		setup(h)
		if _, err := firewalld.Read(ctx, h); !errors.Is(err, firewalld.ErrUnknown) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// zone scripts public: target, ports and services at runtime and in the
// permanent configuration.
func zone(h *hostfake.System, target string, ports map[string]bool, services string) {
	zoneOf(h, "public", target, ports, ports, services, services)
}

// zoneOf scripts a zone: its target, and its ports and services at runtime
// and permanently; firewalld's ssh and cockpit services.
func zoneOf(h *hostfake.System, name, target string, ports, permPorts map[string]bool, services, permServices string) {
	z := "--zone=" + name
	respond(h, hostfake.Response{Stdout: target + "\n"}, "firewall-cmd", "--permanent", z, "--get-target")
	for _, p := range []string{"22/tcp", "2230/tcp"} {
		for _, c := range []struct {
			m    map[string]bool
			argv []string
		}{{ports, []string{"firewall-cmd", z, "--query-port=" + p}}, {permPorts, []string{"firewall-cmd", "--permanent", z, "--query-port=" + p}}} {
			r := no
			if c.m[p] {
				r = yes
			}
			respond(h, r, c.argv...)
		}
	}
	respond(h, hostfake.Response{Stdout: services + "\n"}, "firewall-cmd", z, "--list-services")
	respond(h, hostfake.Response{Stdout: permServices + "\n"}, "firewall-cmd", "--permanent", z, "--list-services")
	respond(h, hostfake.Response{Stdout: "22/tcp\n"}, "firewall-cmd", "--permanent", "--service=ssh", "--get-ports")
	respond(h, hostfake.Response{Stdout: "9090/tcp\n"}, "firewall-cmd", "--permanent", "--service=cockpit", "--get-ports")
}

var public = firewalld.State{Installed: detect.Yes, Running: detect.Yes, Default: "public", Active: []string{"public"}}

func step(zone string, permanent bool) firewalld.Step {
	return firewalld.Step{Zone: zone, Port: 2230, Permanent: permanent}
}

// TestPlanFor: the new port is added, at runtime and permanently, to each
// zone whose ports or services let 22 through, at runtime or permanently;
// nothing where the zone lets it through already, or accepts everything. A
// zone that accepts everything carries no SSH: beside one where SSH gets in
// by a rule hardn doesn't read, it is ErrNoSSHPath. Stopped, nothing where
// firewalld doesn't start at boot, ErrStartsAtBoot where it does.
func TestPlanFor(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New(host.Options{})
	zone(h, "default", nil, "cockpit ssh")
	p, err := firewalld.PlanFor(ctx, h, public, []uint16{22}, []uint16{2230})
	if err != nil || !slices.Equal(p.Steps, []firewalld.Step{step("public", false), step("public", true)}) ||
		!slices.Equal(p.Steps[1].Argv(), []string{"firewall-cmd", "--permanent", "--zone=public", "--add-port=2230/tcp"}) ||
		!slices.Equal(p.Undo().Ports, p.Steps) {
		t.Errorf("plan: %+v, %v", p, err)
	}
	h = hostfake.New(host.Options{})
	zone(h, "default", map[string]bool{"2230/tcp": true}, "ssh")
	if p, err := firewalld.PlanFor(ctx, h, public, []uint16{22}, []uint16{2230}); err != nil || len(p.Steps) != 0 || !slices.Equal(p.Zones, []string{"public"}) {
		t.Errorf("already let through: %+v, %v", p, err)
	}
	h = hostfake.New(host.Options{})
	zone(h, "ACCEPT", nil, "")
	if p, err := firewalld.PlanFor(ctx, h, public, []uint16{22}, []uint16{2230}); err != nil || len(p.Steps) != 0 || len(p.Zones) != 0 {
		t.Errorf("zone accepts everything: %+v, %v", p, err)
	}
	h = hostfake.New(host.Options{})
	zone(h, "default", nil, "cockpit")
	if _, err := firewalld.PlanFor(ctx, h, public, []uint16{22}, []uint16{2230}); !errors.Is(err, firewalld.ErrNoSSHPath) {
		t.Errorf("no zone carries SSH: %v", err)
	}
	// docker's trusted zone accepts everything; SSH reaches public by a
	// rich rule hardn doesn't read.
	two := firewalld.State{Installed: detect.Yes, Running: detect.Yes, Default: "public", Active: []string{"trusted", "public"}}
	h = hostfake.New(host.Options{})
	zoneOf(h, "trusted", "ACCEPT", nil, nil, "", "")
	zoneOf(h, "public", "default", nil, nil, "cockpit", "cockpit")
	if _, err := firewalld.PlanFor(ctx, h, two, []uint16{22}, []uint16{2230}); !errors.Is(err, firewalld.ErrNoSSHPath) {
		t.Errorf("a zone accepting all beside one SSH reaches by a rule hardn doesn't read: %v", err)
	}
	h = hostfake.New(host.Options{})
	zoneOf(h, "trusted", "ACCEPT", nil, nil, "", "")
	zoneOf(h, "public", "default", nil, nil, "ssh", "ssh")
	if p, err := firewalld.PlanFor(ctx, h, two, []uint16{22}, []uint16{2230}); err != nil || !slices.Equal(p.Zones, []string{"public"}) || len(p.Steps) != 2 {
		t.Errorf("a zone accepting all beside public: %+v, %v", p, err)
	}
	// internal lets SSH through only in its permanent configuration (a
	// runtime change not saved): after a reboot it does again, so it gets
	// the new port too; dmz carries none.
	three := firewalld.State{Installed: detect.Yes, Running: detect.Yes, Default: "public", Active: []string{"public", "internal", "dmz"}}
	h = hostfake.New(host.Options{})
	zoneOf(h, "public", "default", nil, nil, "ssh", "ssh")
	zoneOf(h, "internal", "default", nil, map[string]bool{"22/tcp": true}, "", "")
	zoneOf(h, "dmz", "default", nil, nil, "", "")
	p, err = firewalld.PlanFor(ctx, h, three, []uint16{22}, []uint16{2230})
	if err != nil || !slices.Equal(p.Zones, []string{"public", "internal"}) ||
		!slices.Equal(p.Steps, []firewalld.Step{step("public", false), step("public", true), step("internal", false), step("internal", true)}) {
		t.Errorf("three zones: %+v, %v", p, err)
	}
	for _, c := range []struct {
		enabled detect.Tri
		want    error
	}{{detect.No, nil}, {detect.Yes, firewalld.ErrStartsAtBoot}, {detect.Unknown, firewalld.ErrUnknown}} {
		h = hostfake.New(host.Options{})
		p, err := firewalld.PlanFor(ctx, h, firewalld.State{Installed: detect.Yes, Running: detect.No, Enabled: c.enabled}, []uint16{22}, []uint16{2230})
		if !errors.Is(err, c.want) || (c.want == nil) != (err == nil) || len(p.Steps) != 0 || len(h.Runs()) != 0 {
			t.Errorf("stopped, enabled %v: %+v, %v, ran %q", c.enabled, p, err, h.Runs())
		}
	}
	if p, err := firewalld.PlanFor(ctx, h, firewalld.State{Installed: detect.No, Running: detect.No}, []uint16{22}, []uint16{2230}); err != nil || len(p.Steps) != 0 {
		t.Errorf("not installed: %+v, %v", p, err)
	}
}

// TestExecute: the steps run, and a port still not let through afterwards
// is ErrNotLetThrough.
func TestExecute(t *testing.T) {
	ctx := context.Background()
	p := firewalld.Plan{Zones: []string{"public"}, Steps: []firewalld.Step{step("public", false), step("public", true)}}
	for _, after := range []bool{true, false} {
		h := hostfake.New(host.Options{})
		respond(h, hostfake.Response{Stdout: "success\n"}, p.Steps[0].Argv()...)
		respond(h, hostfake.Response{Stdout: "success\n"}, p.Steps[1].Argv()...)
		zone(h, "default", map[string]bool{"2230/tcp": after}, "ssh")
		err := firewalld.Execute(ctx, h, p, []uint16{2230})
		if after != (err == nil) || !after && !errors.Is(err, firewalld.ErrNotLetThrough) {
			t.Errorf("let through after: %v; %v", after, err)
		}
	}
	h := hostfake.New(host.Options{})
	respond(h, hostfake.Response{Stdout: "Error: INVALID_ZONE\n", ExitCode: 112}, p.Steps[0].Argv()...)
	if err := firewalld.Execute(ctx, h, p, []uint16{2230}); err == nil || len(h.Runs()) != 1 {
		t.Errorf("a step fails: %v, ran %q", err, h.Runs())
	}
}

// TestRollback: firewalld.service active, each port there is taken away,
// at runtime and permanently, last first, and one not there is left; not
// active (the boot unit, before firewalld starts), firewall-offline-cmd
// takes away the permanent ones, and no firewall-cmd runs; not installed,
// nothing. A failure doesn't stop the rest.
func TestRollback(t *testing.T) {
	ctx := context.Background()
	u := firewalld.Undo{Ports: []firewalld.Step{step("public", false), step("public", true), step("internal", true)}}
	h := hostfake.New(host.Options{})
	unitIs(h, "active", "enabled")
	respond(h, hostfake.Response{Stdout: "running\n"}, "firewall-cmd", "--state")
	respond(h, yes, "firewall-cmd", "--zone=public", "--query-port=2230/tcp")
	respond(h, yes, "firewall-cmd", "--permanent", "--zone=public", "--query-port=2230/tcp")
	respond(h, no, "firewall-cmd", "--permanent", "--zone=internal", "--query-port=2230/tcp")
	respond(h, hostfake.Response{Stdout: "success\n"}, "firewall-cmd", "--zone=public", "--remove-port=2230/tcp")
	respond(h, hostfake.Response{Stdout: "Error: x\n", ExitCode: 1}, "firewall-cmd", "--permanent", "--zone=public", "--remove-port=2230/tcp")
	err := firewalld.Rollback(ctx, h, u)
	var removed []string
	for _, r := range h.Runs() {
		if slices.Contains(r, "--remove-port=2230/tcp") {
			removed = append(removed, host.FormatArgv(r))
		}
	}
	if err == nil || !slices.Equal(removed, []string{"firewall-cmd --permanent --zone=public --remove-port=2230/tcp", "firewall-cmd --zone=public --remove-port=2230/tcp"}) {
		t.Errorf("running: %v, removed %q", err, removed)
	}
	for _, active := range []string{"inactive", "active"} { // active: forked, not yet on D-Bus (no Type=)
		h = hostfake.New(host.Options{})
		unitIs(h, active, "enabled")
		respond(h, hostfake.Response{Stdout: "not running\n", ExitCode: 252}, "firewall-cmd", "--state")
		respond(h, yes, "firewall-offline-cmd", "--zone=public", "--query-port=2230/tcp")
		respond(h, yes, "firewall-offline-cmd", "--zone=internal", "--query-port=2230/tcp")
		respond(h, hostfake.Response{}, "firewall-offline-cmd", "--zone=public", "--remove-port=2230/tcp")
		respond(h, hostfake.Response{}, "firewall-offline-cmd", "--zone=internal", "--remove-port=2230/tcp")
		want := 6
		if active == "active" {
			want = 7 // and firewall-cmd --state
		}
		if err := firewalld.Rollback(ctx, h, u); err != nil || len(h.Runs()) != want {
			t.Errorf("%s: %v, ran %q", active, err, h.Runs())
		}
	}
	h = hostfake.New(host.Options{})
	unitIs(h, "inactive", "not-found")
	if err := firewalld.Rollback(ctx, h, u); err != nil || len(h.Runs()) != 2 {
		t.Errorf("not installed: %v, ran %q", err, h.Runs())
	}
	h = hostfake.New(host.Options{})
	missing252(h)
	if err := firewalld.Rollback(ctx, h, u); err != nil || len(h.Runs()) != 2 {
		t.Errorf("not installed, systemd 252: %v, ran %q", err, h.Runs())
	}
	if err := firewalld.Rollback(ctx, h, firewalld.Undo{Ports: []firewalld.Step{{Zone: "a b", Port: 2230}}}); err == nil {
		t.Errorf("a zone with a space: nil")
	}
}
