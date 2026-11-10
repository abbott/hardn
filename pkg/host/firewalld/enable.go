package firewalld

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Enable and Disable: the Firewall item on Rocky Linux and AlmaLinux
// (docs/engine/el-firewalld.md §1.3, §1.4).

// Files firewalld reads, and the unit an Enable's undo is journalled under.
const (
	// ConfFile is firewalld's configuration (CleanupOnExit).
	ConfFile = "/etc/firewalld/firewalld.conf"
	// ZoneDir holds the zones firewalld's own were changed into.
	ZoneDir = "/etc/firewalld/zones"
	// EnableUnit names EnableUndo in hardn's journal (revert.OpPlan;
	// plans.Registry). Stable: a journal written by one hardn is rolled
	// back by the next.
	EnableUnit = "firewalld-enable"
)

// OtherFirewalls are the units an Enable refuses beside it, active or
// starting at boot: firewalld.service's Conflicts= (rocky10-test-110's
// unit), whose start it would stop and its undo not start again,
// nftables.service, which loads its own rules, and EPEL's ufw.
var OtherFirewalls = []string{"iptables.service", "ip6tables.service", "ebtables.service", "ipset.service", "nftables.service", "ufw.service"}

var (
	// ErrOtherFirewall: one of OtherFirewalls is active or starts at
	// boot (*OtherFirewallError names them).
	ErrOtherFirewall = errors.New("another firewall is active or starts at boot")
	// ErrNoCleanup: CleanupOnExit is off, so a stopped firewalld leaves
	// its rules loaded, and neither a Disable nor an Enable's undo takes
	// them away.
	ErrNoCleanup = errors.New("firewalld leaves its rules loaded when it stops (CleanupOnExit)")
	// ErrBootBlocked: firewalld.service is masked or missing.
	ErrBootBlocked = errors.New("firewalld.service is masked or missing")
	// ErrBootUnknown: systemctl is-enabled printed a word other than
	// enabled or disabled, which hardn doesn't plan from.
	ErrBootUnknown = errors.New("firewalld.service's start at boot is not one hardn plans from")
	// ErrZoneNoSSH: a zone that will hold an interface or a source, other
	// than the default zone, doesn't let an SSH port through
	// (*ZoneError names it).
	ErrZoneNoSSH = errors.New("a firewalld zone that will hold an interface or a source doesn't let SSH through")
	// ErrNotReady: firewalld didn't reach running after its start.
	ErrNotReady = errors.New("firewalld didn't come up after its start")
)

// OtherFirewallError is CheckOthers' refusal: the units and what
// systemctl said of each.
type OtherFirewallError struct{ Units []string }

func (e *OtherFirewallError) Error() string {
	return ErrOtherFirewall.Error() + ": " + strings.Join(e.Units, ", ")
}

// Is reports target == ErrOtherFirewall.
func (e *OtherFirewallError) Is(target error) bool { return target == ErrOtherFirewall }

// ZoneError is PlanEnable's refusal of a bound zone that doesn't let Port
// through; By is what binds it.
type ZoneError struct {
	Zone, By string
	Port     uint16
}

func (e *ZoneError) Error() string {
	return fmt.Sprintf("%s: zone %s (%s) doesn't let %d/tcp through", ErrZoneNoSSH, e.Zone, e.By, e.Port)
}

// Is reports target == ErrZoneNoSSH.
func (e *ZoneError) Is(target error) bool { return target == ErrZoneNoSSH }

// CheckOthers refuses where a unit of OtherFirewalls is active, or where
// is-enabled says "enabled" (or another word that starts it at boot);
// "not-found" (systemd 252's answer for a unit it knows no file of too,
// detect.MissingUnit), "disabled", "masked" and "static" are fine. An
// is-enabled that prints nothing else is ErrUnknown.
func CheckOthers(ctx context.Context, sys host.System) error {
	var on []string
	for _, u := range OtherFirewalls {
		for _, verb := range []string{"is-active", "is-enabled"} {
			res, err := sys.Run(ctx, "systemctl", verb, u)
			var xe *host.ExitError
			if err != nil && !errors.As(err, &xe) {
				return fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
			}
			w := strings.TrimSpace(string(res.Stdout))
			if verb == "is-enabled" && detect.MissingUnit(res, u) {
				w = "not-found"
			}
			switch {
			case verb == "is-active" && (w == "active" || w == "activating" || w == "reloading"):
				on = append(on, u+" "+w)
			case verb == "is-enabled" && w == "":
				return fmt.Errorf("firewalld: systemctl is-enabled %s printed nothing (exit %d): %w", u, res.ExitCode, ErrUnknown)
			case verb == "is-enabled" && w != "not-found" && w != "disabled" && w != "masked" && w != "static":
				on = append(on, u+" "+w)
			}
		}
	}
	if len(on) > 0 {
		return &OtherFirewallError{Units: on}
	}
	return nil
}

// CleanupOnExit reads ConfFile's CleanupOnExit: off only for "no" or
// "false", in any case (firewalld's fw.py); on where the file or the key
// is missing, firewalld's default.
func CleanupOnExit(ctx context.Context, sys host.System) (bool, error) {
	data, err := sys.ReadFile(ctx, ConfFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
	}
	on := true
	for _, l := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if ok && strings.TrimSpace(k) == "CleanupOnExit" {
			v = strings.ToLower(strings.Trim(strings.TrimSpace(v), `"'`))
			on = v != "no" && v != "false"
		}
	}
	return on, nil
}

// Zone is a zone's permanent configuration, as firewall-offline-cmd
// --list-all-zones prints it.
type Zone struct {
	Target                               string
	Interfaces, Sources, Services, Ports []string
}

// Offline is firewalld's permanent configuration, read while it is down.
type Offline struct {
	Default string
	Zones   map[string]Zone
}

// ReadOffline reads the permanent configuration with firewall-offline-cmd
// (--get-default-zone, --list-all-zones), never firewall-cmd.
func ReadOffline(ctx context.Context, sys host.System) (Offline, error) {
	res, err := sys.Run(ctx, OfflineCmd, "--get-default-zone")
	if err != nil {
		return Offline{}, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
	}
	o := Offline{Default: strings.TrimSpace(string(res.Stdout))}
	if !zoneName(o.Default) {
		return Offline{}, fmt.Errorf("firewalld: default zone %q: %w", o.Default, ErrUnknown)
	}
	if res, err = sys.Run(ctx, OfflineCmd, "--list-all-zones"); err != nil {
		return Offline{}, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
	}
	if o.Zones, err = parseZones(res.Stdout); err != nil {
		return Offline{}, err
	}
	if _, ok := o.Zones[o.Default]; !ok {
		return Offline{}, fmt.Errorf("firewalld: default zone %s not listed: %w", o.Default, ErrUnknown)
	}
	return o, nil
}

// parseZones reads --list-all-zones: each zone's name at the margin (the
// default's followed by " (default)"), then its "  key: values" lines;
// a rich rule's own lines start with a tab. A zone without a target line
// is ErrUnknown.
func parseZones(out []byte) (map[string]Zone, error) {
	zs := map[string]Zone{}
	cur, seen := "", false
	done := func() error {
		if cur != "" && !seen {
			return fmt.Errorf("firewalld: zone %s printed no target: %w", cur, ErrUnknown)
		}
		return nil
	}
	for _, l := range strings.Split(string(out), "\n") {
		switch {
		case l == "" || l[0] == '\t':
		case l[0] != ' ':
			if err := done(); err != nil {
				return nil, err
			}
			name, _, _ := strings.Cut(l, " ")
			if !zoneName(name) {
				return nil, fmt.Errorf("firewalld: zone %q: %w", name, ErrUnknown)
			}
			cur, seen = name, false
			zs[cur] = Zone{}
		case cur != "":
			k, v, ok := strings.Cut(strings.TrimSpace(l), ":")
			if !ok {
				continue
			}
			z, f := zs[cur], strings.Fields(v)
			switch k {
			case "target":
				z.Target, seen = strings.TrimSpace(v), true
			case "interfaces":
				z.Interfaces = f
			case "sources":
				z.Sources = f
			case "services":
				z.Services = f
			case "ports":
				z.Ports = f
			}
			zs[cur] = z
		}
	}
	if err := done(); err != nil {
		return nil, err
	}
	if len(zs) == 0 {
		return nil, fmt.Errorf("firewalld: --list-all-zones listed no zone: %w", ErrUnknown)
	}
	return zs, nil
}

// NMZones are the zones of NetworkManager's active connections that name
// one (nmcli -g connection.zone), by connection: NetworkManager binds the
// interfaces it manages there when firewalld comes up. No nmcli, or
// NetworkManager not running (exit 8), is none.
func NMZones(ctx context.Context, sys host.System) (map[string]string, error) {
	res, err := sys.Run(ctx, "nmcli", "-t", "-f", "UUID,DEVICE", "connection", "show", "--active")
	var xe *host.ExitError
	switch {
	case errors.Is(err, host.ErrNotFound):
		return nil, nil
	case errors.As(err, &xe) && xe.ExitCode == 8: // NetworkManager is not running (nmcli(1)): it binds nothing
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
	}
	zs := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(string(res.Stdout)), "\n") {
		uuid, dev, ok := strings.Cut(l, ":")
		if !ok || strings.Trim(uuid, "0123456789abcdef-") != "" {
			continue
		}
		r, err := sys.Run(ctx, "nmcli", "-g", "connection.zone", "connection", "show", uuid)
		if err != nil {
			return nil, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
		}
		if z := strings.TrimSpace(string(r.Stdout)); z != "" {
			if !zoneName(z) {
				return nil, fmt.Errorf("firewalld: connection %s's zone %q: %w", uuid, z, ErrUnknown)
			}
			zs["NetworkManager's "+dev] = z
		}
	}
	return zs, nil
}

// EnablePlan is an Enable from a stopped firewalld: the SSH ports added to
// the default zone's permanent configuration offline, the start, the
// check, the enable.
type EnablePlan struct {
	Default string   // the default zone
	Zones   []string // the zones checked after the start, the default first
	Ports   []uint16 // the SSH ports
	Steps   []Step   // offline adds to Default, each Permanent
	// Enable: is-enabled said "disabled", so the change enables it.
	Enable bool
	// ZoneFile is the zone file the first add creates (none before), which
	// the undo removes; "" where it exists or nothing is added. ZoneOld:
	// its .old (firewalld's writer's copy) wasn't there either.
	ZoneFile string
	ZoneOld  bool
}

// PlanEnable plans an Enable of firewalld st, stopped, with ports let
// through: refused where firewalld runs or isn't installed (fs.ErrInvalid:
// the caller's to offer it), its unit masked or missing (ErrBootBlocked)
// or another word than enabled or disabled (ErrBootUnknown), CleanupOnExit
// off (ErrNoCleanup), another firewall on (ErrOtherFirewall), or a bound
// zone other than the default not letting a port through (ErrZoneNoSSH).
// The default zone gets the ports it doesn't let through. It reads only,
// and never runs firewall-cmd.
func PlanEnable(ctx context.Context, sys host.System, st State, ports []uint16) (EnablePlan, error) {
	if st.Installed != detect.Yes || st.Running != detect.No || len(ports) == 0 {
		return EnablePlan{}, fmt.Errorf("firewalld: enable from installed %v, running %v, ports %v: %w", st.Installed, st.Running, ports, fs.ErrInvalid)
	}
	p := EnablePlan{Ports: slices.Clone(ports)}
	switch st.Unit {
	case "enabled":
	case "disabled":
		p.Enable = true
	case "masked", "masked-runtime", "not-found":
		return EnablePlan{}, fmt.Errorf("firewalld: systemctl is-enabled %s: %s: %w", Service, st.Unit, ErrBootBlocked)
	default:
		return EnablePlan{}, fmt.Errorf("firewalld: systemctl is-enabled %s: %q: %w", Service, st.Unit, ErrBootUnknown)
	}
	switch on, err := CleanupOnExit(ctx, sys); {
	case err != nil:
		return EnablePlan{}, err
	case !on:
		return EnablePlan{}, fmt.Errorf("firewalld: %s: %w", ConfFile, ErrNoCleanup)
	}
	if err := CheckOthers(ctx, sys); err != nil {
		return EnablePlan{}, err
	}
	o, err := ReadOffline(ctx, sys)
	if err != nil {
		return EnablePlan{}, err
	}
	nm, err := NMZones(ctx, sys)
	if err != nil {
		return EnablePlan{}, err
	}
	p.Default = o.Default
	by := map[string]string{o.Default: "the default zone"}
	p.Zones = []string{o.Default}
	add := func(z, why string) {
		if !slices.Contains(p.Zones, z) {
			p.Zones = append(p.Zones, z)
			by[z] = why
		}
	}
	names := make([]string, 0, len(o.Zones))
	for z := range o.Zones {
		names = append(names, z)
	}
	slices.Sort(names)
	for _, z := range names {
		if zc := o.Zones[z]; len(zc.Interfaces) > 0 || len(zc.Sources) > 0 {
			add(z, "bound to "+strings.Join(append(slices.Clone(zc.Interfaces), zc.Sources...), ", "))
		}
	}
	conns := make([]string, 0, len(nm))
	for c := range nm {
		conns = append(conns, c)
	}
	slices.Sort(conns)
	for _, c := range conns {
		add(nm[c], c)
	}
	services := map[string][]string{}
	for _, z := range p.Zones {
		zc, ok := o.Zones[z]
		if !ok {
			return EnablePlan{}, fmt.Errorf("firewalld: zone %s (%s) not listed: %w", z, by[z], ErrUnknown)
		}
		if zc.Target == "ACCEPT" {
			continue
		}
		for _, port := range ports {
			ok, err := offlineAllows(ctx, sys, zc, port, services)
			if err != nil {
				return EnablePlan{}, err
			}
			switch {
			case ok:
			case z == o.Default:
				p.Steps = append(p.Steps, Step{Zone: z, Port: port, Permanent: true})
			default:
				return EnablePlan{}, &ZoneError{Zone: z, By: by[z], Port: port}
			}
		}
	}
	if len(p.Steps) > 0 {
		f := path.Join(ZoneDir, o.Default+".xml")
		for _, c := range []struct {
			name string
			set  func()
		}{{f, func() { p.ZoneFile = f }}, {f + ".old", func() { p.ZoneOld = p.ZoneFile != "" }}} {
			switch _, err := sys.Stat(ctx, c.name); {
			case errors.Is(err, fs.ErrNotExist):
				c.set()
			case err != nil:
				return EnablePlan{}, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
			}
		}
	}
	return p, nil
}

// offlineAllows reports whether zone z's permanent configuration lets TCP
// port through by a port or a service's ports (firewall-offline-cmd
// --service=<s> --get-ports, read once per service into services).
func offlineAllows(ctx context.Context, sys host.System, z Zone, port uint16, services map[string][]string) (bool, error) {
	if covers(z.Ports, port) {
		return true, nil
	}
	for _, svc := range z.Services {
		if !zoneName(svc) {
			return false, fmt.Errorf("firewalld: service %q: %w", svc, ErrUnknown)
		}
		specs, ok := services[svc]
		if !ok {
			res, err := sys.Run(ctx, OfflineCmd, "--service="+svc, "--get-ports")
			if err != nil {
				return false, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
			}
			specs = strings.Fields(string(res.Stdout))
			services[svc] = specs
		}
		if covers(specs, port) {
			return true, nil
		}
	}
	return false, nil
}

// Commands an Enable and a Disable run.
func offlineAdd(s Step) []string { return s.argv(OfflineCmd, "--add-port=") }

func systemctl(verb string) []string { return []string{"systemctl", verb, Service} }

// Commands is p's commands, in order, for the plan's words.
func (p EnablePlan) Commands() [][]string {
	var out [][]string
	for _, s := range p.Steps {
		out = append(out, offlineAdd(s))
	}
	out = append(out, systemctl("start"))
	if p.Enable {
		out = append(out, systemctl("enable"))
	}
	return out
}

// Undo is p's undo, for the journal before ExecuteEnable runs.
func (p EnablePlan) Undo() EnableUndo {
	return EnableUndo{Start: true, Ports: slices.Clone(p.Steps), ZoneFile: p.ZoneFile, ZoneOld: p.ZoneOld, Enable: p.Enable}
}

// ReadyTimeout bounds the wait for firewalld after its start: the unit
// has no Type=, so systemctl start returns before firewalld is on D-Bus.
const ReadyTimeout = 60 * time.Second

// readyTimeout is ReadyTimeout; tests shorten it.
var readyTimeout = ReadyTimeout

// pause waits d, or until ctx is done. Tests replace it.
var pause = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ExecuteEnable runs p: the offline adds, the start, a wait for firewalld
// to run (ErrNotReady), then, at runtime, every zone of p and every active
// zone must let each port through (ErrNotLetThrough), and last the enable.
// It stops at the first failure; the undo (p.Undo, journalled before)
// takes back what ran.
func ExecuteEnable(ctx context.Context, sys host.System, p EnablePlan) error {
	for _, s := range p.Steps {
		a := offlineAdd(s)
		if _, err := sys.Run(ctx, a[0], a[1:]...); err != nil {
			return fmt.Errorf("firewalld: %s: %w", host.FormatArgv(a), err)
		}
	}
	if _, err := sys.Run(ctx, "systemctl", "start", Service); err != nil {
		return fmt.Errorf("firewalld: systemctl start %s: %w", Service, err)
	}
	st, err := waitReady(ctx, sys)
	if err != nil {
		return err
	}
	zones := slices.Clone(p.Zones)
	for _, z := range st.Zones() {
		if !slices.Contains(zones, z) {
			zones = append(zones, z)
		}
	}
	for _, z := range zones {
		for _, port := range p.Ports {
			ok, err := Allows(ctx, sys, z, port, false)
			switch {
			case err != nil:
				return err
			case !ok:
				return fmt.Errorf("firewalld: zone %s, %d/tcp: %w", z, port, ErrNotLetThrough)
			}
		}
	}
	if p.Enable {
		if _, err := sys.Run(ctx, "systemctl", "enable", Service); err != nil {
			return fmt.Errorf("firewalld: systemctl enable %s: %w", Service, err)
		}
	}
	return nil
}

// waitReady polls Read until firewalld runs with a default zone, a second
// apart, for ReadyTimeout of wall time at most (a firewall-cmd may itself
// wait 10 s for D-Bus). is-active "failed" ends it at once.
func waitReady(ctx context.Context, sys host.System) (State, error) {
	wctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	var last error
	for {
		st, err := Read(wctx, sys)
		switch {
		case err == nil && st.Running == detect.Yes && st.Default != "":
			return st, nil
		case err == nil:
			last = fmt.Errorf("firewalld: running %v after the start", st.Running)
		default:
			last = err
		}
		if active, _, _ := unit(wctx, sys); active == "failed" {
			return State{}, fmt.Errorf("firewalld: %s failed: %w: %v", Service, ErrNotReady, last)
		}
		if err := pause(wctx, time.Second); err != nil {
			return State{}, fmt.Errorf("firewalld: %w: %v", ErrNotReady, last)
		}
	}
}

// EnableUndo is an Enable's undo, journalled before it runs: what it
// started, the ports it added, the zone file it created, and whether it
// enabled the unit.
type EnableUndo struct {
	Start    bool   `json:"start,omitempty"`
	Ports    []Step `json:"ports,omitempty"`
	ZoneFile string `json:"zoneFile,omitempty"`
	// ZoneOld: ZoneFile+".old" wasn't there either, and goes with it.
	ZoneOld bool `json:"zoneOld,omitempty"`
	Enable  bool `json:"enable,omitempty"`
}

// Check refuses an EnableUndo RollbackEnable would not run: a port Undo
// refuses, or not permanent (an Enable adds offline), or a zone file
// other than ZoneDir/<zone>.xml of the ports' zone (fs.ErrInvalid).
func (u EnableUndo) Check() error {
	if err := (Undo{Ports: u.Ports}).Check(); err != nil {
		return err
	}
	for _, s := range u.Ports {
		if !s.Permanent {
			return fmt.Errorf("firewalld: zone %s, port %d at runtime: %w", s.Zone, s.Port, fs.ErrInvalid)
		}
	}
	if u.ZoneFile != "" {
		dir, file := path.Split(u.ZoneFile)
		zone, ok := strings.CutSuffix(file, ".xml")
		if dir != ZoneDir+"/" || !ok || !zoneName(zone) || len(u.Ports) == 0 ||
			slices.ContainsFunc(u.Ports, func(s Step) bool { return s.Zone != zone }) {
			return fmt.Errorf("firewalld: zone file %q for ports %v: %w", u.ZoneFile, u.Ports, fs.ErrInvalid)
		}
	} else if u.ZoneOld {
		return fmt.Errorf("firewalld: .old without a zone file: %w", fs.ErrInvalid)
	}
	return nil
}

// RollbackEnable undoes an Enable, in this order:
//  1. systemctl stop, where the Enable started it, checked: is-active must
//     say "inactive" (a "failed" unit is reset first: what it may have left
//     loaded came after the Enable's adds);
//  2. systemctl disable, where the Enable enabled it;
//  3. once firewalld is stopped and won't start at boot, the ports: the
//     zone file the add created removed (with the .old firewalld's writer
//     leaves), or each port queried and taken away offline.
//
// The ports go last, and only where both steps before them held: an undo
// that stops between them leaves at worst a disabled firewalld with the
// SSH ports let through, never an enabled one that shuts sshd's port at
// the next boot. Where the Enable found firewalld enabled (stopped and
// starting at boot), the ports stay for the same reason: firewalld still
// starts at boot, and they are what lets SSH through then. It returns
// every error, never starts a unit, and never runs firewall-cmd: the same
// from the timer and from the boot unit.
func RollbackEnable(ctx context.Context, sys host.System, u EnableUndo) error {
	if err := u.Check(); err != nil {
		return err
	}
	var errs []error
	run := func(argv []string) bool {
		if _, err := sys.Run(ctx, argv[0], argv[1:]...); err != nil {
			errs = append(errs, fmt.Errorf("firewalld: %s: %w", host.FormatArgv(argv), err))
			return false
		}
		return true
	}
	down := true
	if u.Start {
		down = run(systemctl("stop"))
		active, _, err := unit(ctx, sys)
		if err == nil && active == "failed" {
			// A unit that failed (the Enable's own start, or a kill at its
			// stop) stays "failed" through a stop. What it may have left
			// loaded was loaded after the Enable's adds, so it lets the SSH
			// ports through; clearing the state lets the undo finish.
			if run([]string{"systemctl", "reset-failed", Service}) {
				active, _, err = unit(ctx, sys)
			}
		}
		if err != nil || active != "inactive" {
			errs = append(errs, fmt.Errorf("firewalld: is-active %q after the stop, %v: its rules may still be loaded", active, err))
			down = false
		}
	}
	off := true
	if u.Enable {
		off = run(systemctl("disable"))
	}
	switch {
	case len(u.Ports) == 0:
	case !down || !off || !u.Enable:
		if u.Enable {
			errs = append(errs, fmt.Errorf("firewalld: the SSH ports hardn added are left let through: firewalld isn't both stopped and disabled"))
		}
	case u.ZoneFile != "":
		for _, f := range []string{u.ZoneFile, u.ZoneFile + ".old"} {
			if f == u.ZoneFile+".old" && !u.ZoneOld {
				continue
			}
			if err := sys.Remove(ctx, f); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, fmt.Errorf("firewalld: %w", err))
			}
		}
	default:
		if err := offlineRemove(ctx, sys, u.Ports); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// offlineRemove takes away each permanent port that firewall-offline-cmd
// says is there.
func offlineRemove(ctx context.Context, sys host.System, ports []Step) error {
	var errs []error
	for _, s := range slices.Backward(ports) {
		q := s.argv(OfflineCmd, "--query-port=")
		ok, err := query(ctx, sys, q[0], q[1:]...)
		if err == nil && ok {
			r := s.argv(OfflineCmd, "--remove-port=")
			if _, err = sys.Run(ctx, r[0], r[1:]...); err != nil {
				err = fmt.Errorf("firewalld: %s: %w", host.FormatArgv(r), err)
			}
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// DisablePlan is a Disable: the unit disabled where is-enabled says
// enabled, and firewalld stopped where it runs.
type DisablePlan struct{ Disable, Stop bool }

// Empty says there is nothing to disable or stop.
func (p DisablePlan) Empty() bool { return !p.Disable && !p.Stop }

// PlanDisable plans a Disable of st: refused where firewalld would stop
// with CleanupOnExit off (ErrNoCleanup), or isn't read (ErrUnknown).
func PlanDisable(ctx context.Context, sys host.System, st State) (DisablePlan, error) {
	if st.Installed != detect.Yes || (st.Running != detect.Yes && st.Running != detect.No) {
		return DisablePlan{}, fmt.Errorf("firewalld: disable from installed %v, running %v: %w", st.Installed, st.Running, ErrUnknown)
	}
	p := DisablePlan{Disable: st.Unit == "enabled", Stop: st.Running == detect.Yes}
	if p.Stop {
		switch on, err := CleanupOnExit(ctx, sys); {
		case err != nil:
			return DisablePlan{}, err
		case !on:
			return DisablePlan{}, fmt.Errorf("firewalld: %s: %w", ConfFile, ErrNoCleanup)
		}
	}
	return p, nil
}

// Commands is p's commands, in order.
func (p DisablePlan) Commands() [][]string {
	var out [][]string
	if p.Disable {
		out = append(out, systemctl("disable"))
	}
	if p.Stop {
		out = append(out, systemctl("stop"))
	}
	return out
}

// Undo enables the unit again where p disabled it, and never starts it: a
// start loads the saved configuration, not the one that ran.
func (p DisablePlan) Undo() [][]string {
	if p.Disable {
		return [][]string{systemctl("enable")}
	}
	return nil
}

// ExecuteDisable runs p, then checks firewalld is inactive where it
// stopped it and disabled where it disabled it.
func ExecuteDisable(ctx context.Context, sys host.System, p DisablePlan) error {
	for _, a := range p.Commands() {
		if _, err := sys.Run(ctx, a[0], a[1:]...); err != nil {
			return fmt.Errorf("firewalld: %s: %w", host.FormatArgv(a), err)
		}
	}
	active, word, err := unit(ctx, sys)
	switch {
	case err != nil:
		return err
	case p.Stop && active != "inactive":
		return fmt.Errorf("firewalld: is-active %q after systemctl stop, its rules may still be loaded: %w", active, ErrUnknown)
	case p.Disable && word != "disabled":
		return fmt.Errorf("firewalld: is-enabled %q after systemctl disable: %w", word, ErrUnknown)
	}
	return nil
}
