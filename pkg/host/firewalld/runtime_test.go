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

// listAll is firewall-cmd --list-all-zones in command.py's form
// (print_zone_policy_info): the name and its attributes at the margin, each
// key two spaces in, a rich rule's lines a tab in. Not run on a host.
const listAll = `block
  target: %%REJECT%%
  interfaces: 
  sources: 
  services: 
  ports: 
  rich rules: 

internal (active)
  target: default
  interfaces: 
  sources: 192.0.2.0/24
  services: ssh
  ports: 9090/tcp
  rich rules: 

public (default, active)
  target: default
  icmp-block-inversion: no
  interfaces: eth0
  sources: 
  services: cockpit dhcpv6-client ssh
  ports: 80/tcp 443/tcp 6000-6007/udp
  protocols: 
  forward: yes
  rich rules: 

trusted
  target: ACCEPT
  interfaces: 
  sources: 
  services: 
  ports: 
  rich rules: 
`

func runtimeHost(zones string) *hostfake.System {
	h := hostfake.New(host.Options{})
	h.Respond(hostfake.Response{Stdout: zones}, "firewall-cmd", "--list-all-zones")
	for svc, ports := range map[string]string{"ssh": "22/tcp\n", "cockpit": "9090/tcp\n", "dhcpv6-client": "546/udp\n"} {
		h.Respond(hostfake.Response{Stdout: ports}, "firewall-cmd", "--permanent", "--service="+svc, "--get-ports")
	}
	return h
}

// TestReadRuntime: the zones a connection can arrive in, the active ones
// then the default, each with what it lets in by a port or a service, a
// service read once; a zone that accepts everything; a rich rule marking its
// zone. Only reads run.
func TestReadRuntime(t *testing.T) {
	ctx := context.Background()
	st := firewalld.State{Installed: detect.Yes, Running: detect.Yes, Default: "public", Active: []string{"internal", "public"}}
	h := runtimeHost(listAll)
	rt, err := firewalld.ReadRuntime(ctx, h, st)
	if err != nil || len(rt.Zones) != 2 || rt.Rich() {
		t.Fatalf("%+v, %v", rt, err)
	}
	in, pub := rt.Zones[0], rt.Zones[1]
	if in.Name != "internal" || in.Default || !slices.Equal(in.Sources, []string{"192.0.2.0/24"}) || len(in.Interfaces) != 0 ||
		!slices.Equal(in.Specs, []string{"9090/tcp", "22/tcp"}) {
		t.Errorf("internal: %+v", in)
	}
	if pub.Name != "public" || !pub.Default || !slices.Equal(pub.Interfaces, []string{"eth0"}) ||
		!slices.Equal(pub.Specs, []string{"80/tcp", "443/tcp", "6000-6007/udp", "9090/tcp", "546/udp", "22/tcp"}) {
		t.Errorf("public: %+v", pub)
	}
	for _, tc := range []struct {
		port  uint16
		proto string
		want  bool
	}{{22, "tcp", true}, {22, "udp", false}, {443, "tcp", true}, {6003, "udp", true}, {6003, "tcp", false}, {6008, "udp", false}, {8080, "tcp", false}} {
		if got := pub.Lets(tc.port, tc.proto); got != tc.want {
			t.Errorf("public lets %d/%s: %v", tc.port, tc.proto, got)
		}
	}
	if got := h.Runs(); len(got) != 4 {
		t.Errorf("ran %q: each service once", got)
	}

	st.Active, st.Default = []string{"trusted"}, "trusted"
	if rt, err = firewalld.ReadRuntime(ctx, runtimeHost(listAll), st); err != nil || !rt.Zones[0].Accepts() || !rt.Zones[0].Lets(1, "udp") {
		t.Errorf("trusted: %+v, %v", rt, err)
	}

	rich := "public (default, active)\n  target: default\n  interfaces: eth0\n  services: ssh\n  ports: \n  rich rules: \n\trule family=\"ipv4\" source address=\"192.0.2.0/24\" port port=\"9090\" protocol=\"tcp\" accept\n"
	st.Active, st.Default = []string{"public"}, "public"
	if rt, err = firewalld.ReadRuntime(ctx, runtimeHost(rich), st); err != nil || !rt.Rich() {
		t.Errorf("a rich rule: %+v, %v", rt, err)
	}
}

// TestReadRuntimeForwards: the lines after "forward-ports:" are port
// forwards, not rich rules (command.py prints both a tab in): a forward
// touches the port it takes and, staying on this host, the one it lands on,
// and one that is not read touches every port.
func TestReadRuntimeForwards(t *testing.T) {
	ctx := context.Background()
	st := firewalld.State{Installed: detect.Yes, Running: detect.Yes, Default: "public", Active: []string{"public"}}
	zone := func(forwards, rules string) string {
		return "public (default, active)\n  target: default\n  interfaces: eth0\n  sources: \n  services: ssh\n  ports: \n  protocols: \n  forward: yes\n  masquerade: no\n" +
			"  forward-ports: " + forwards + "\n  source-ports: \n  icmp-blocks: \n  rich rules: " + rules + "\n"
	}
	fw := "\n\tport=80:proto=tcp:toport=8080:toaddr=\n\tport=8443:proto=tcp:toport=443:toaddr=192.0.2.9\n\tport=6000-6007:proto=udp:toport=:toaddr=2001:db8::9"
	rt, err := firewalld.ReadRuntime(ctx, runtimeHost(zone(fw, "")), st)
	if err != nil || rt.Rich() || len(rt.Zones[0].Forwards) != 3 {
		t.Fatalf("%+v, %v", rt, err)
	}
	for _, tc := range []struct {
		port  uint16
		proto string
		want  bool
	}{{80, "tcp", true}, {8080, "tcp", true}, {80, "udp", false}, {8443, "tcp", true}, {443, "tcp", false}, {6003, "udp", true}, {6003, "tcp", false}, {22, "tcp", false}} {
		if got := rt.Forwards(tc.port, tc.proto); got != tc.want {
			t.Errorf("forwards %d/%s: %v", tc.port, tc.proto, got)
		}
	}
	if rt, err = firewalld.ReadRuntime(ctx, runtimeHost(zone(fw, "\n\trule family=\"ipv4\" accept")), st); err != nil || !rt.Rich() || len(rt.Zones[0].Forwards) != 3 {
		t.Errorf("forwards and a rich rule: %+v, %v", rt, err)
	}
	for _, bad := range []string{"port=http:proto=tcp:toport=:toaddr=", "port=80:proto=tcp", "80/tcp", "port=80:proto=tcp:toport=x:toaddr="} {
		if rt, err = firewalld.ReadRuntime(ctx, runtimeHost(zone("\n\t"+bad, "")), st); err != nil || rt.Rich() || !rt.Forwards(22, "tcp") {
			t.Errorf("%q: %+v, %v", bad, rt, err)
		}
	}
	if rt, err = firewalld.ReadRuntime(ctx, runtimeHost(zone("", "")), st); err != nil || rt.Rich() || rt.Forwards(80, "tcp") {
		t.Errorf("no forward: %+v, %v", rt, err)
	}
}

// TestReadRuntimeNotRead: a firewalld that isn't running, a firewall-cmd
// that fails or isn't there, output that is no zone listing, a zone of the
// state that isn't listed, a service that isn't a name and one whose ports
// aren't read are each ErrUnknown, with nothing let in.
func TestReadRuntimeNotRead(t *testing.T) {
	ctx := context.Background()
	st := firewalld.State{Installed: detect.Yes, Running: detect.Yes, Default: "public", Active: []string{"public"}}
	for name, tc := range map[string]struct {
		st  firewalld.State
		set func(h *hostfake.System)
	}{
		"not running": {firewalld.State{Installed: detect.Yes, Running: detect.No}, func(*hostfake.System) {}},
		"fails": {st, func(h *hostfake.System) {
			h.Respond(hostfake.Response{Stderr: "FirewallD is not running\n", ExitCode: 252}, "firewall-cmd", "--list-all-zones")
		}},
		"missing": {st, func(h *hostfake.System) {
			h.Respond(hostfake.Response{Err: host.ErrNotFound}, "firewall-cmd", "--list-all-zones")
		}},
		"garbage": {st, func(h *hostfake.System) {
			h.Respond(hostfake.Response{Stdout: "Traceback (most recent call last):\n"}, "firewall-cmd", "--list-all-zones")
		}},
		"empty": {st, func(h *hostfake.System) { h.Respond(hostfake.Response{}, "firewall-cmd", "--list-all-zones") }},
		"no target": {st, func(h *hostfake.System) {
			h.Respond(hostfake.Response{Stdout: "public (default)\n  services: ssh\n"}, "firewall-cmd", "--list-all-zones")
		}},
		"zone not listed": {st, func(h *hostfake.System) {
			h.Respond(hostfake.Response{Stdout: "work\n  target: default\n"}, "firewall-cmd", "--list-all-zones")
		}},
		"service not a name": {st, func(h *hostfake.System) {
			h.Respond(hostfake.Response{Stdout: "public\n  target: default\n  services: ssh;reboot\n"}, "firewall-cmd", "--list-all-zones")
		}},
		"service not read": {st, func(h *hostfake.System) {
			h.Respond(hostfake.Response{Stdout: "public\n  target: default\n  services: ssh\n"}, "firewall-cmd", "--list-all-zones")
			h.Respond(hostfake.Response{Stderr: "Error: INVALID_SERVICE\n", ExitCode: 101}, "firewall-cmd", "--permanent", "--service=ssh", "--get-ports")
		}},
	} {
		h := hostfake.New(host.Options{})
		tc.set(h)
		rt, err := firewalld.ReadRuntime(ctx, h, tc.st)
		if !errors.Is(err, firewalld.ErrUnknown) || len(rt.Zones) != 0 {
			t.Errorf("%s: %+v, %v", name, rt, err)
		}
	}
}
