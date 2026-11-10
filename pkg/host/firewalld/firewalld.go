// Package firewalld reads firewalld's state and plans the SSH ports a
// change adds let through it, before sshd moves: Rocky Linux's and
// AlmaLinux's firewall (docs/engine/el-ports.md). It changes the host only
// in Execute, which adds ports through firewall-cmd, and in Rollback, which
// takes them away again.
package firewalld

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Cmd is firewalld's client.
const Cmd = "firewall-cmd"

// OfflineCmd edits firewalld's permanent configuration while it is not
// running (firewall-offline-cmd(1)). Its queries answer as firewall-cmd's:
// "yes" with exit 0, "no" with exit 1 (firewalld's command.py, which both
// use).
const OfflineCmd = "firewall-offline-cmd"

// Service is firewalld's systemd unit.
const Service = "firewalld.service"

// Unit names Undo in hardn's journal (revert.OpPlan; plans.Registry).
// Stable: a journal written by one hardn is rolled back by the next.
const Unit = "firewalld-ports"

var (
	// ErrUnknown: firewalld's state, or what it lets through, is not
	// established.
	ErrUnknown = errors.New("firewalld's state is not established")
	// ErrNoSSHPath: firewalld runs, and no zone hardn reads lets the
	// current SSH port through, so how SSH reaches the host (a rich rule,
	// a policy, a direct rule) is not one hardn reads, and a new port's
	// way through cannot be planned like it.
	ErrNoSSHPath = errors.New("no firewalld zone hardn reads lets the current SSH port through")
	// ErrStartsAtBoot: firewalld is stopped and starts at boot. What it
	// lets through then is its permanent configuration, which hardn reads
	// only from a running firewalld, so a new port could be shut at the
	// next boot.
	ErrStartsAtBoot = errors.New("firewalld is stopped and starts at boot")
)

// State is what firewalld runs: whether it is installed and running; its
// default zone and its active zones (those with an interface or a source),
// when running; whether it starts at boot.
type State struct {
	Installed, Running detect.Tri
	Default            string
	Active             []string
	// Enabled is No where systemctl is-enabled says "disabled" or
	// "masked", Unknown where it says nothing, and Yes for any other
	// answer: "static", "indirect" and the rest may start it too.
	Enabled detect.Tri
	// Unit is is-enabled's word as printed ("enabled", "disabled",
	// "masked", "not-found", ...), which Enable and Disable plan from.
	Unit string
}

// Zones are the zones a connection can arrive in: the active ones, and the
// default zone, which takes the interfaces no zone names.
func (s State) Zones() []string {
	zs := slices.Clone(s.Active)
	if s.Default != "" && !slices.Contains(zs, s.Default) {
		zs = append(zs, s.Default)
	}
	return zs
}

// Read reads firewalld's state through systemd first (unit), then, where
// firewalld.service is active, firewall-cmd --state, --get-default-zone and
// --get-active-zones. A stopped firewalld costs no firewall-cmd, which
// waits 10 s for D-Bus while firewalld is down (el-ports.md §4). It
// changes nothing.
func Read(ctx context.Context, sys host.System) (State, error) {
	s, err := state(ctx, sys)
	if err != nil || s.Running != detect.Yes {
		return s, err
	}
	var res host.Result
	if res, err = sys.Run(ctx, Cmd, "--get-default-zone"); err != nil {
		return s, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
	}
	s.Default = strings.TrimSpace(string(res.Stdout))
	if res, err = sys.Run(ctx, Cmd, "--get-active-zones"); err != nil {
		return s, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
	}
	s.Active = activeZones(res.Stdout)
	if s.Default == "" || !zoneName(s.Default) {
		return s, fmt.Errorf("firewalld: default zone %q: %w", s.Default, ErrUnknown)
	}
	return s, nil
}

// unit reads systemctl is-active and is-enabled of firewalld.service: the
// words they print (stdout, whatever their exit). An is-enabled that
// prints nothing is the word "", not an error. A systemctl that didn't
// run is ErrUnknown.
func unit(ctx context.Context, sys host.System) (active, enabled string, err error) {
	for _, c := range []struct {
		verb string
		out  *string
	}{{"is-active", &active}, {"is-enabled", &enabled}} {
		res, err := sys.Run(ctx, "systemctl", c.verb, Service)
		var xe *host.ExitError
		if err != nil && !errors.As(err, &xe) {
			return "", "", fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
		}
		*c.out = strings.TrimSpace(string(res.Stdout))
	}
	return active, enabled, nil
}

// UnitFile is firewalld.service as the package installs it
// (rocky10-test-110's rpm): a masked unit with no such file is a mask of a
// firewalld not installed.
const UnitFile = "/usr/lib/systemd/system/firewalld.service"

// ErrFailed: firewalld.service failed. A daemon that died ran no
// clean-up, so its rules may still be loaded.
var ErrFailed = errors.New("firewalld.service failed, and its rules may still be loaded")

// state is firewalld's unit and daemon. is-enabled "not-found", or
// "masked" with no UnitFile, is not installed. is-active "inactive" is not running; "active" is running
// once firewall-cmd --state says "running" (exit 0), and Unknown
// otherwise: starting, or in its failsafe (exit 251). "failed" is Unknown:
// a daemon that died ran no clean-up, so its rules may be loaded. Any
// other word (activating, deactivating, reloading) is Unknown.
func state(ctx context.Context, sys host.System) (State, error) {
	active, word, err := unit(ctx, sys)
	if err != nil {
		return State{}, err
	}
	s := State{Installed: detect.Yes, Unit: word, Enabled: enabledOf(word)}
	missing := word == "not-found"
	if word == "masked" {
		switch _, err := sys.Stat(ctx, UnitFile); {
		case errors.Is(err, fs.ErrNotExist):
			missing = true
		case err != nil:
			return s, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
		}
	}
	switch active {
	case "inactive":
		if missing {
			return State{Installed: detect.No, Running: detect.No, Unit: word, Enabled: detect.No}, nil
		}
		s.Running = detect.No
		return s, nil
	case "failed":
		return s, fmt.Errorf("firewalld: %w: %w", ErrFailed, ErrUnknown)
	case "active":
	default:
		return s, fmt.Errorf("firewalld: systemctl is-active %s printed %q: %w", Service, active, ErrUnknown)
	}
	res, err := sys.Run(ctx, Cmd, "--state")
	if out := strings.TrimSpace(string(res.Stdout)); err != nil || out != "running" {
		return s, fmt.Errorf("firewalld: %s active, firewall-cmd --state printed %q (exit %d): %w: %w", Service, out, res.ExitCode, ErrUnknown, errOr(err, detect.ErrUnrecognised))
	}
	s.Running = detect.Yes
	return s, nil
}

// enabledOf is State.Enabled from is-enabled's word: No for "disabled" and
// "masked", Unknown for none, Yes for any other.
func enabledOf(word string) detect.Tri {
	switch word {
	case "disabled", "masked":
		return detect.No
	case "":
		return detect.Unknown
	}
	return detect.Yes
}

// activeZones reads --get-active-zones: each zone's name on a line of its
// own, the newer firewalld's " (default)" after it, its interfaces and
// sources on indented lines after it.
func activeZones(out []byte) []string {
	var zs []string
	for _, l := range strings.Split(string(out), "\n") {
		if l == "" || l[0] == ' ' || l[0] == '\t' {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimSpace(l), " ")
		if zoneName(name) {
			zs = append(zs, name)
		}
	}
	return zs
}

// zoneName is a zone's name as firewalld allows one: letters, digits, "_"
// and "-", so that it stands in an argument as it is.
func zoneName(s string) bool {
	return s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") == ""
}

// Allows reports whether zone lets TCP port through, at runtime or in the
// permanent configuration: its target ACCEPT, the port added, or one of its
// services naming it. Rich rules, policies and direct rules are not read.
func Allows(ctx context.Context, sys host.System, zone string, port uint16, permanent bool) (bool, error) {
	if ok, err := accepts(ctx, sys, zone); err != nil || ok {
		return ok, err
	}
	return byRule(ctx, sys, zone, port, permanent)
}

// accepts reports whether zone's target is ACCEPT: it lets everything in.
func accepts(ctx context.Context, sys host.System, zone string) (bool, error) {
	if !zoneName(zone) {
		return false, fmt.Errorf("firewalld: zone %q: %w", zone, ErrUnknown)
	}
	res, err := sys.Run(ctx, Cmd, "--permanent", "--zone="+zone, "--get-target")
	if err != nil {
		return false, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
	}
	return strings.TrimSpace(string(res.Stdout)) == "ACCEPT", nil
}

// byRule reports whether zone lets TCP port through by a port added or a
// service naming it, at runtime or in the permanent configuration.
func byRule(ctx context.Context, sys host.System, zone string, port uint16, permanent bool) (bool, error) {
	if !zoneName(zone) {
		return false, fmt.Errorf("firewalld: zone %q: %w", zone, ErrUnknown)
	}
	perm := func(args ...string) []string {
		if permanent {
			return append([]string{"--permanent"}, args...)
		}
		return args
	}
	spec := strconv.Itoa(int(port)) + "/tcp"
	if ok, err := query(ctx, sys, Cmd, perm("--zone="+zone, "--query-port="+spec)...); err != nil || ok {
		return ok, err
	}
	res, err := sys.Run(ctx, Cmd, perm("--zone="+zone, "--list-services")...)
	if err != nil {
		return false, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
	}
	for _, svc := range strings.Fields(string(res.Stdout)) {
		if !zoneName(svc) {
			return false, fmt.Errorf("firewalld: service %q: %w", svc, ErrUnknown)
		}
		res, err := sys.Run(ctx, Cmd, "--permanent", "--service="+svc, "--get-ports")
		if err != nil {
			return false, fmt.Errorf("firewalld: %w: %w", ErrUnknown, err)
		}
		if covers(strings.Fields(string(res.Stdout)), port) {
			return true, nil
		}
	}
	return false, nil
}

// query runs a --query-* of cmd (firewall-cmd or firewall-offline-cmd):
// yes (exit 0) or no (exit 1).
func query(ctx context.Context, sys host.System, cmd string, args ...string) (bool, error) {
	res, err := sys.Run(ctx, cmd, args...)
	var xe *host.ExitError
	switch out := strings.TrimSpace(string(res.Stdout)); {
	case err == nil && out == "yes":
		return true, nil
	case errors.As(err, &xe) && xe.ExitCode == 1 && out == "no":
		return false, nil
	}
	return false, fmt.Errorf("firewalld: %s %s printed %q (exit %d): %w", cmd, strings.Join(args, " "), strings.TrimSpace(string(res.Stdout)), res.ExitCode, ErrUnknown)
}

// covers reports whether a service's ports ("22/tcp", "60000-61000/udp")
// hold TCP port.
func covers(specs []string, port uint16) bool {
	for _, s := range specs {
		ports, proto, ok := strings.Cut(s, "/")
		if !ok || proto != "tcp" {
			continue
		}
		lo, hi, rng := strings.Cut(ports, "-")
		a, err1 := strconv.Atoi(lo)
		b, err2 := a, error(nil)
		if rng {
			b, err2 = strconv.Atoi(hi)
		}
		if err1 == nil && err2 == nil && int(port) >= a && int(port) <= b {
			return true
		}
	}
	return false
}

// Step is one port added to a zone, at runtime or in the permanent
// configuration.
type Step struct {
	Zone      string `json:"zone"`
	Port      uint16 `json:"port"`
	Permanent bool   `json:"permanent,omitempty"`
}

// Argv is the firewall-cmd that makes s.
func (s Step) Argv() []string { return s.argv(Cmd, "--add-port=") }

func (s Step) argv(cmd, op string) []string {
	a := []string{cmd, "--zone=" + s.Zone, op + strconv.Itoa(int(s.Port)) + "/tcp"}
	if s.Permanent && cmd == Cmd {
		a = slices.Insert(a, 1, "--permanent")
	}
	return a
}

// Plan is what lets the added ports through: every zone that lets one of
// the current SSH ports through by a port or a service gets each added
// port, at runtime and in the permanent configuration, wherever it doesn't
// already let it through. A zone whose target is ACCEPT needs nothing.
type Plan struct {
	Zones []string // the zones that let SSH through by a port or a service now
	Steps []Step
}

// Undo is p's undo, for the journal before Execute runs (Rollback).
func (p Plan) Undo() Undo { return Undo{Ports: slices.Clone(p.Steps)} }

// PlanFor plans ports added let through firewalld as the current ports
// (hold) are. A zone carries SSH where a current port is let through by a
// port or a service, at runtime or permanently; a zone that accepts
// everything needs nothing, and carries nothing either: SSH arriving
// through some other zone by a rich rule must not be passed over because a
// zone of containers' interfaces accepts all. firewalld disabled and not
// running plans nothing: it filters nothing, now or after a reboot. That
// rests on firewalld's CleanupOnExit=yes, its default and Rocky Linux's
// (rocky10-test-110): with it off, a firewalld stopped after running
// leaves its rules loaded until the next boot, and confirm-or-revert
// catches the new port they block (el-firewalld.md §2).
// Errors: ErrStartsAtBoot where it is stopped and enabled; ErrNoSSHPath
// where it runs, not every zone accepts everything, and no zone carries
// SSH; ErrUnknown where a read fails.
func PlanFor(ctx context.Context, sys host.System, st State, hold, add []uint16) (Plan, error) {
	var p Plan
	switch {
	case len(add) == 0 || st.Installed == detect.No:
		return p, nil
	case st.Running == detect.No && st.Enabled == detect.No:
		return p, nil
	case st.Running == detect.No && st.Enabled == detect.Unknown:
		return Plan{}, fmt.Errorf("firewalld: whether %s starts at boot isn't read: %w", Service, ErrUnknown)
	case st.Running == detect.No:
		return Plan{}, fmt.Errorf("firewalld: systemctl is-enabled %s: %w", Service, ErrStartsAtBoot)
	case st.Running != detect.Yes:
		return Plan{}, fmt.Errorf("firewalld: %w", ErrUnknown)
	}
	all := true // every zone accepts everything
	for _, z := range st.Zones() {
		acc, err := accepts(ctx, sys, z)
		if err != nil {
			return Plan{}, err
		}
		if acc {
			continue
		}
		all = false
		carries := false
		for _, h := range hold {
			for _, permanent := range []bool{false, true} {
				ok, err := byRule(ctx, sys, z, h, permanent)
				if err != nil {
					return Plan{}, err
				}
				carries = carries || ok
			}
		}
		if !carries {
			continue
		}
		p.Zones = append(p.Zones, z)
		for _, a := range add {
			for _, permanent := range []bool{false, true} {
				ok, err := byRule(ctx, sys, z, a, permanent)
				if err != nil {
					return Plan{}, err
				}
				if !ok {
					p.Steps = append(p.Steps, Step{Zone: z, Port: a, Permanent: permanent})
				}
			}
		}
	}
	if len(p.Zones) == 0 && !all {
		return Plan{}, fmt.Errorf("firewalld: zones %v, current ports %v: %w", st.Zones(), hold, ErrNoSSHPath)
	}
	return p, nil
}

// ErrNotLetThrough: after its steps, a zone still doesn't let an SSH port
// through at runtime: a port change's new port (Execute), or one of
// Enable's after the start (ExecuteEnable).
var ErrNotLetThrough = errors.New("firewalld doesn't let an SSH port through")

// Execute runs p's steps in order, stopping at the first that fails, then
// checks that every zone of p lets each added port through at runtime
// (ErrNotLetThrough). Its undo is p.Undo, journalled before it runs.
func Execute(ctx context.Context, sys host.System, p Plan, add []uint16) error {
	for _, s := range p.Steps {
		a := s.Argv()
		if _, err := sys.Run(ctx, a[0], a[1:]...); err != nil {
			return fmt.Errorf("firewalld: %s: %w", host.FormatArgv(a), err)
		}
	}
	for _, z := range p.Zones {
		for _, a := range add {
			ok, err := Allows(ctx, sys, z, a, false)
			switch {
			case err != nil:
				return err
			case !ok:
				return fmt.Errorf("firewalld: zone %s, %d/tcp: %w", z, a, ErrNotLetThrough)
			}
		}
	}
	return nil
}

// Undo is the ports a Plan adds, for Rollback to take away.
type Undo struct {
	Ports []Step `json:"ports"`
}

// Check refuses an Undo Rollback would not run: a zone's name outside
// firewalld's characters, or port 0 (fs.ErrInvalid).
func (u Undo) Check() error {
	for _, s := range u.Ports {
		if !zoneName(s.Zone) || s.Port == 0 {
			return fmt.Errorf("firewalld: zone %q, port %d: %w", s.Zone, s.Port, fs.ErrInvalid)
		}
	}
	return nil
}

// Rollback takes away each port of u that is there, last first: through
// firewall-cmd where firewalld.service is active, at runtime and
// permanently as u says; through firewall-offline-cmd where it isn't (the
// boot unit may run before firewalld starts), permanently only, there
// being no runtime to undo. A port not there is left: PlanFor plans only ports that are not
// there, so one there now is the change's, and a rollback taken up again,
// or one of a step that never ran, takes away nothing else. firewall-cmd
// not installed is nothing to undo. It goes on past a port that fails, and
// returns every error.
func Rollback(ctx context.Context, sys host.System, u Undo) error {
	if err := u.Check(); err != nil {
		return err
	}
	active, word, err := unit(ctx, sys)
	if err != nil || word == "not-found" {
		return err
	}
	cmd := OfflineCmd
	if active == "active" {
		// Active is from the fork (the unit has no Type=): firewall-cmd
		// only once firewalld says it runs; until then the files, which a
		// starting firewalld may have read already (el-ports.md §1.4).
		if res, err := sys.Run(ctx, Cmd, "--state"); err == nil && strings.TrimSpace(string(res.Stdout)) == "running" {
			cmd = Cmd
		}
	}
	var errs []error
	for _, s := range slices.Backward(u.Ports) {
		if cmd == OfflineCmd && !s.Permanent {
			continue
		}
		q := s.argv(cmd, "--query-port=")
		ok, err := query(ctx, sys, q[0], q[1:]...)
		if err == nil && ok {
			r := s.argv(cmd, "--remove-port=")
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

func errOr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}
