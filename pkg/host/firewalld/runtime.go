package firewalld

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Runtime is what a running firewalld lets in, in the zones a connection can
// arrive in (State.Zones): the Firewall screen's Ports section sets the
// listening ports against it (docs/engine/screen-ports.md). Rich rules mark
// their zone (Zone.Rich) and are not read; a port forward marks the ports it
// takes and lands on (Forwards); policies and direct rules are not read at
// all.
type Runtime struct {
	Zones []RuntimeZone
}

// RuntimeZone is one zone at runtime.
type RuntimeZone struct {
	Name string
	Zone
	// Default: the default zone, which takes the interfaces no zone names.
	Default bool
	// Specs are what the zone lets in by a port or a service: its ports,
	// then each service's, as firewalld writes them ("8080/tcp",
	// "60000-61000/udp").
	Specs []string
}

// ReadRuntime reads what a running firewalld st lets in: firewall-cmd
// --list-all-zones, one call for every zone's target, interfaces, sources,
// services, ports, port forwards and rich rules (command.py's print_zone_policy_info, which
// parseZones reads), then --permanent --service=<s> --get-ports once for each
// service of a zone of st.Zones, as Allows reads a service. It changes
// nothing. A firewalld not running is fs.ErrInvalid's kin, ErrUnknown: the
// caller reads it only where st.Running is Yes.
func ReadRuntime(ctx context.Context, sys host.System, st State) (Runtime, error) {
	if st.Running != detect.Yes {
		return Runtime{}, fmt.Errorf("firewalld: not running: %w", ErrUnknown)
	}
	res, err := sys.Run(ctx, Cmd, "--list-all-zones")
	if err != nil {
		return Runtime{}, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
	}
	zones, err := parseZones(res.Stdout)
	if err != nil {
		return Runtime{}, err
	}
	var rt Runtime
	services := map[string][]string{}
	for _, name := range st.Zones() {
		z, ok := zones[name]
		if !ok {
			return Runtime{}, fmt.Errorf("firewalld: zone %s not listed: %w", name, ErrUnknown)
		}
		rz := RuntimeZone{Name: name, Zone: z, Default: name == st.Default, Specs: slices.Clone(z.Ports)}
		for _, svc := range z.Services {
			if !zoneName(svc) {
				return Runtime{}, fmt.Errorf("firewalld: service %q: %w", svc, ErrUnknown)
			}
			specs, ok := services[svc]
			if !ok {
				res, err := sys.Run(ctx, Cmd, "--permanent", "--service="+svc, "--get-ports")
				if err != nil {
					return Runtime{}, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
				}
				specs = strings.Fields(string(res.Stdout))
				services[svc] = specs
			}
			rz.Specs = append(rz.Specs, specs...)
		}
		rt.Zones = append(rt.Zones, rz)
	}
	return rt, nil
}

// Accepts: the zone's target is ACCEPT, so it lets everything in.
func (z RuntimeZone) Accepts() bool { return z.Target == "ACCEPT" }

// Lets reports whether the zone lets port of proto ("tcp", "udp") in: its
// target ACCEPT, a port added, or a service's.
func (z RuntimeZone) Lets(port uint16, proto string) bool {
	return z.Accepts() || coversProto(z.Specs, port, proto)
}

// Rich: a zone of rt has a rich rule, so what rt lets in is not the whole of
// what firewalld does.
func (rt Runtime) Rich() bool {
	return slices.ContainsFunc(rt.Zones, func(z RuntimeZone) bool { return z.Rich })
}

// Forwards reports whether a port forward of a zone of rt touches port of
// proto: it takes the port's connections elsewhere, or lands another port's
// on it on this host (no toaddr), where the zone's ports do not decide. A
// forward that is not read touches every port.
func (rt Runtime) Forwards(port uint16, proto string) bool {
	for _, z := range rt.Zones {
		for _, f := range z.Forwards {
			from, to, ok := forward(f)
			if !ok || coversProto(append(to, from), port, proto) {
				return true
			}
		}
	}
	return false
}

// forward reads a forward as firewall-cmd prints it,
// "port=<p>:proto=<proto>:toport=<p>:toaddr=<addr>", into the spec it takes
// ("80/tcp") and, where it stays on this host, the one it lands on.
func forward(f string) (from string, to []string, ok bool) {
	rest, okPort := strings.CutPrefix(f, "port=")
	ports, rest, okProto := strings.Cut(rest, ":proto=")
	proto, rest, okTo := strings.Cut(rest, ":toport=")
	toport, toaddr, okAddr := strings.Cut(rest, ":toaddr=")
	if !okPort || !okProto || !okTo || !okAddr || !portSpec(ports) || toport != "" && !portSpec(toport) {
		return "", nil, false
	}
	if toaddr == "" && toport != "" {
		to = []string{toport + "/" + proto}
	}
	return ports + "/" + proto, to, true
}

// portSpec: s is a port or a range of two ("6000-6007").
func portSpec(s string) bool {
	lo, hi, rng := strings.Cut(s, "-")
	_, err := strconv.ParseUint(lo, 10, 16)
	if err == nil && rng {
		_, err = strconv.ParseUint(hi, 10, 16)
	}
	return err == nil
}
