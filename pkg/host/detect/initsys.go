package detect

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// InitKind is the init system.
type InitKind uint8

const (
	InitUnknown InitKind = iota
	Systemd
	OpenRC
)

func (k InitKind) String() string {
	switch k {
	case Systemd:
		return "systemd"
	case OpenRC:
		return "openrc"
	}
	return "unknown"
}

// The init system's signals.
const (
	SystemdRunDir = "/run/systemd/system" // exists when systemd booted the host (sd_booted(3))
	OpenRCRunDir  = "/run/openrc"         // OpenRC's service state directory on Linux (RC_SVCDIR)
	OpenRCRun     = "/sbin/openrc-run"    // installed with OpenRC
)

// Init is the init system.
type Init struct {
	Kind InitKind
	// Running is Yes when the host was booted with Kind (its run directory
	// exists), No when OpenRC is only installed (a container started
	// without it), Unknown when Kind is.
	Running Tri
	Source  string // the path that decided Kind
	Err     error
}

// DetectInit tells systemd from OpenRC by their run directories, then by an
// installed openrc-run.
func DetectInit(ctx context.Context, sys host.System) Init {
	sd, sdErr := isDir(ctx, sys, SystemdRunDir)
	rc, rcErr := isDir(ctx, sys, OpenRCRunDir)
	switch {
	case sd == Yes && rc == Yes:
		return Init{Err: fmt.Errorf("detect: init: both %s and %s exist: %w", SystemdRunDir, OpenRCRunDir, ErrConflict)}
	case sd == Unknown || rc == Unknown:
		return Init{Err: fmt.Errorf("detect: init: %w", errors.Join(sdErr, rcErr))}
	case sd == Yes:
		return Init{Kind: Systemd, Running: Yes, Source: SystemdRunDir}
	case rc == Yes:
		return Init{Kind: OpenRC, Running: Yes, Source: OpenRCRunDir}
	}
	switch fi, t, err := stat(ctx, sys, OpenRCRun); {
	case t == Yes && fi.Mode().IsRegular():
		return Init{Kind: OpenRC, Running: No, Source: OpenRCRun}
	case t == Unknown:
		return Init{Err: fmt.Errorf("detect: init: %w", err)}
	}
	return Init{Err: fmt.Errorf("detect: init: no %s, %s or %s: %w", SystemdRunDir, OpenRCRunDir, OpenRCRun, ErrNoSignal)}
}

// SSHMode is how sshd is started.
type SSHMode uint8

const (
	SSHUnknown SSHMode = iota
	// SSHSocket: systemd's ssh.socket is the unit that listens and starts
	// ssh.service. It is active, or enabled while ssh.service is not
	// running, and ssh.service is not enabled. A Port change needs
	// daemon-reload and a restart of ssh.socket. Ubuntu 24.04 and 26.04 by
	// default; opt-in on Debian.
	SSHSocket
	// SSHService: systemd's ssh.service runs sshd, which listens itself;
	// ssh.socket is neither enabled nor active. Debian 12 and 13, Proxmox
	// VE 9, Ubuntu 22.04.
	SSHService
	// SSHOpenRC: OpenRC booted the host and starts sshd, which is started
	// or added to a runlevel (Alpine).
	SSHOpenRC
)

func (m SSHMode) String() string {
	switch m {
	case SSHSocket:
		return "ssh.socket"
	case SSHService:
		return "ssh.service"
	case SSHOpenRC:
		return "openrc sshd"
	}
	return "unknown"
}

// The SSH signals. hardn never queries a unit named sshd: that alias is
// missing on a fresh Ubuntu 24.04.
const (
	// SocketGenerator is Ubuntu 24.04+'s generator in systemd's vendor
	// generator directory. An entry of the same name in a directory that
	// comes before it overrides or masks it (systemd.generator(7)).
	SocketGenerator = "/usr/lib/systemd/system-generators/" + socketGeneratorName
	OpenRCSSHD      = "/etc/init.d/sshd"
	// OpenRCStarted holds a link for each service OpenRC has started
	// (RC_SVCDIR/started).
	OpenRCStarted = OpenRCRunDir + "/started"
	// OpenRCRunlevels holds a directory per runlevel, with a link for each
	// service added to it (RC_RUNLEVELDIR).
	OpenRCRunlevels = "/etc/runlevels"
)

const socketGeneratorName = "sshd-socket-generator"

// generatorDirs are systemd's system generator directories, highest
// priority first (systemd.generator(7)); SocketGenerator is in the last.
var generatorDirs = [...]string{
	"/run/systemd/system-generators",
	"/etc/systemd/system-generators",
	"/usr/local/lib/systemd/system-generators",
	"/usr/lib/systemd/system-generators",
}

// UnitState is what systemctl printed for a unit; "" when not established.
type UnitState struct {
	Enabled string // is-enabled: enabled, disabled, masked, static, not-found, ...
	Active  string // is-active: active, inactive, failed, activating, ...
}

// IsEnabled is Enabled as a fact: Yes for enabled and enabled-runtime; No
// for disabled, masked, linked and not-found (and their -runtime forms);
// Unknown for static, indirect, generated, transient, alias, bad and "",
// which do not say whether the unit is started at boot.
func (u UnitState) IsEnabled() Tri { return unitStates["is-enabled"][u.Enabled] }

// IsActive is Active as a fact: Yes for active, reloading and refreshing;
// No for inactive and failed; Unknown for activating, deactivating,
// maintenance and "".
func (u UnitState) IsActive() Tri { return unitStates["is-active"][u.Active] }

// OpenRCService is what OpenRC's own state says about a service.
type OpenRCService struct {
	// Started is whether OpenRC has started the service: the link it makes
	// in OpenRCStarted when it marks a service started exists.
	Started Tri
	// InRunlevel is whether the service is added to a runlevel (rc-update
	// add): an OpenRCRunlevels/<runlevel>/<service> entry exists. Runlevels
	// names those runlevels.
	InRunlevel Tri
	Runlevels  []string
}

// SSH is how sshd is started.
type SSH struct {
	Mode    SSHMode
	Socket  UnitState     // ssh.socket (systemd)
	Service UnitState     // ssh.service (systemd)
	OpenRC  OpenRCService // sshd (OpenRC)
	// Generator is whether Ubuntu's sshd-socket-generator runs: it derives
	// ssh.socket's ListenStream from sshd_config's Port and ListenAddress
	// at boot and on daemon-reload (systemd only). No when it is not
	// installed or is masked; Unknown when an entry of its name that is
	// not a mask overrides it.
	Generator     Tri
	GeneratorPath string // the entry that decided Generator; "" when there is none
	Source        string // what decided Mode
	// Err says why Mode is Unknown or, with Mode established, why a
	// sub-fact (Generator, OpenRC.Started, OpenRC.InRunlevel) is.
	Err error
}

// DetectSSH asks systemctl about ssh.socket and ssh.service on systemd, and
// reads OpenRC's state for sshd on OpenRC. Unless in is an init system that
// booted the host, it runs and reads nothing: an init system that is only
// installed does not start sshd.
func DetectSSH(ctx context.Context, sys host.System, in Init) SSH {
	switch {
	case in.Kind != Systemd && in.Kind != OpenRC:
		return SSH{Err: fmt.Errorf("detect: ssh: init system not established: %w", errOr(in.Err, ErrNoSignal))}
	case in.Running != Yes:
		return SSH{Err: fmt.Errorf("detect: ssh: %s is installed but did not boot the host (%s): %w", in.Kind, in.Source, ErrNoSignal)}
	case in.Kind == OpenRC:
		return openRCSSH(ctx, sys)
	}
	return systemdSSH(ctx, sys)
}

// systemdSSH tells ssh.socket from ssh.service. One unit's boot-time state
// (is-enabled) must not contradict the other's runtime state (is-active): a
// socket that is enabled but inactive while ssh.service runs, or a socket
// that is active while ssh.service is enabled to start sshd by itself, is
// ErrConflict rather than a guess at which unit listens.
func systemdSSH(ctx context.Context, sys host.System) SSH {
	var s SSH
	var errs [5]error
	s.Generator, s.GeneratorPath, errs[0] = generator(ctx, sys)
	s.Socket.Enabled, errs[1] = systemctl(ctx, sys, "is-enabled", "ssh.socket")
	s.Socket.Active, errs[2] = systemctl(ctx, sys, "is-active", "ssh.socket")
	s.Service.Enabled, errs[3] = systemctl(ctx, sys, "is-enabled", "ssh.service")
	s.Service.Active, errs[4] = systemctl(ctx, sys, "is-active", "ssh.service")
	if err := errors.Join(errs[1:]...); err != nil {
		s.Err = fmt.Errorf("detect: ssh: %w", err)
		return s
	}
	sockOn, sockUp := s.Socket.IsEnabled(), s.Socket.IsActive()
	svcOn, svcUp := s.Service.IsEnabled(), s.Service.IsActive()
	switch {
	case sockOn == Yes && svcOn == Yes:
		s.Err = s.unitsErr("ssh.socket and ssh.service are both enabled", ErrConflict)
	case sockUp == Yes && svcOn == Yes:
		s.Err = s.unitsErr("ssh.socket is active while ssh.service is enabled to start sshd by itself", ErrConflict)
	case sockOn == Yes && sockUp == No && svcUp == Yes:
		s.Err = s.unitsErr("ssh.socket is enabled but inactive while ssh.service runs", ErrConflict)
	case sockUp == Yes, sockOn == Yes && svcUp == No:
		s.Mode, s.Source = SSHSocket, "ssh.socket "+s.Socket.Enabled+", "+s.Socket.Active
	case sockOn == No && sockUp == No && (svcOn == Yes || svcUp == Yes):
		s.Mode, s.Source = SSHService, "ssh.service "+s.Service.Enabled+", "+s.Service.Active
	case sockOn == No && sockUp == No && svcOn == No && svcUp == No:
		s.Err = fmt.Errorf("detect: ssh: neither ssh.socket nor ssh.service is enabled or active: %w", ErrNoSignal)
	default:
		s.Err = s.unitsErr("no rule decides", ErrUnrecognised)
	}
	if s.Generator == Unknown && s.Err == nil {
		s.Err = fmt.Errorf("detect: ssh: %w", errs[0]) // Mode stands; the generator is not established
	}
	return s
}

func (s SSH) unitsErr(what string, sentinel error) error {
	return fmt.Errorf("detect: ssh: %s (ssh.socket %s/%s, ssh.service %s/%s): %w",
		what, s.Socket.Enabled, s.Socket.Active, s.Service.Enabled, s.Service.Active, sentinel)
}

// generator resolves sshd-socket-generator as systemd.generator(7) says
// systemd does: the first generator directory holding an entry of that name
// decides. A symlink to /dev/null or an empty file there masks it (No); the
// vendor's executable is Yes; anything else, such as an administrator's own
// program of that name, is Unknown. No entry anywhere is No.
func generator(ctx context.Context, sys host.System) (Tri, string, error) {
	for _, dir := range generatorDirs {
		p := dir + "/" + socketGeneratorName
		lfi, t, err := lstat(ctx, sys, p)
		switch {
		case t == No:
			continue
		case t == Unknown:
			return Unknown, p, err
		case lfi.Mode()&fs.ModeSymlink != 0:
			target, err := sys.Readlink(ctx, p)
			if err != nil {
				return Unknown, p, err
			}
			if target == "/dev/null" {
				return No, p, nil
			}
		}
		fi, t, err := stat(ctx, sys, p)
		switch {
		case t == Unknown:
			return Unknown, p, err
		case t == Yes && fi.Mode().IsRegular() && fi.Size() == 0:
			return No, p, nil
		case t == Yes && p == SocketGenerator && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0:
			return Yes, p, nil
		}
		return Unknown, p, fmt.Errorf("%s is neither a mask nor the vendor's generator: %w", p, ErrUnrecognised)
	}
	return No, "", nil
}

// openRCSSH reads OpenRC's state for sshd: its init script, whether OpenRC
// has started it, and the runlevels it is added to. Either of the last two
// is enough, as an active or an enabled unit is on systemd.
func openRCSSH(ctx context.Context, sys host.System) SSH {
	switch fi, t, err := stat(ctx, sys, OpenRCSSHD); {
	case t == Unknown:
		return SSH{Err: fmt.Errorf("detect: ssh: %w", err)}
	case t == No || !fi.Mode().IsRegular():
		return SSH{Err: fmt.Errorf("detect: ssh: no %s: %w", OpenRCSSHD, ErrNoSignal)}
	}
	var s SSH
	var errs [2]error
	_, s.OpenRC.Started, errs[0] = stat(ctx, sys, OpenRCStarted+"/sshd")
	s.OpenRC.Runlevels, s.OpenRC.InRunlevel, errs[1] = runlevels(ctx, sys, "sshd")
	switch o := s.OpenRC; {
	case o.Started == No && o.InRunlevel == No:
		s.Err = fmt.Errorf("detect: ssh: %s is neither started nor in a runlevel: %w", OpenRCSSHD, ErrNoSignal)
		return s
	case o.Started == Yes || o.InRunlevel == Yes:
		levels := strings.Join(o.Runlevels, ",")
		if levels == "" {
			levels = "none"
		}
		s.Mode, s.Source = SSHOpenRC, fmt.Sprintf("%s: started %s, runlevels %s", OpenRCSSHD, o.Started, levels)
	}
	if err := errors.Join(errs[:]...); err != nil {
		s.Err = fmt.Errorf("detect: ssh: %w", err)
	}
	return s
}

// runlevels lists the runlevels service is added to, looking for
// OpenRCRunlevels/<runlevel>/<service> as rc_service_in_runlevel does. An
// entry of OpenRCRunlevels that is not a directory holds nothing.
func runlevels(ctx context.Context, sys host.System, service string) ([]string, Tri, error) {
	entries, err := sys.ReadDir(ctx, OpenRCRunlevels)
	switch {
	case missing(err):
		return nil, No, nil
	case err != nil:
		return nil, Unknown, err
	}
	var in []string
	var errs []error
	for _, e := range entries {
		switch _, t, err := stat(ctx, sys, OpenRCRunlevels+"/"+e.Name()+"/"+service); t {
		case Yes:
			in = append(in, e.Name())
		case Unknown:
			errs = append(errs, err)
		}
	}
	err = errors.Join(errs...)
	switch {
	case len(in) > 0:
		return in, Yes, err
	case err != nil:
		return nil, Unknown, err
	}
	return nil, No, nil
}

// The states systemctl(1) documents for is-enabled, and the unit active
// states (org.freedesktop.systemd1(5), ActiveState) is-active prints.
var unitStates = map[string]map[string]Tri{
	"is-enabled": {
		"enabled": Yes, "enabled-runtime": Yes,
		"disabled": No, "masked": No, "masked-runtime": No, "linked": No, "linked-runtime": No, "not-found": No,
		"static": Unknown, "indirect": Unknown, "generated": Unknown, "transient": Unknown, "alias": Unknown, "bad": Unknown,
	},
	"is-active": {
		"active": Yes, "reloading": Yes, "refreshing": Yes,
		"inactive": No, "failed": No,
		"activating": Unknown, "deactivating": Unknown, "maintenance": Unknown,
	},
}

// systemctl runs systemctl verb unit and returns the state it printed. Both
// verbs exit non-zero for a state that is not "on", so an exit error with a
// documented state is an answer, not a failure.
func systemctl(ctx context.Context, sys host.System, verb, unit string) (string, error) {
	res, err := sys.Run(ctx, "systemctl", verb, unit)
	var xe *host.ExitError
	if err != nil && !errors.As(err, &xe) {
		return "", err
	}
	state := firstLine(res.Stdout)
	if _, ok := unitStates[verb][state]; !ok {
		return "", fmt.Errorf("systemctl %s %s printed %q (exit %d): %w", verb, unit, state, res.ExitCode, ErrUnrecognised)
	}
	return state, nil
}

// The readers above, exported for the status controls (next/p4-check-apparmor:
// apparmor.service, and OpenRC's apparmor), so that a check reads a unit's
// or a service's state by the rules DetectSSH uses rather than a copy of
// them.

// SystemctlIsEnabled runs systemctl is-enabled unit, and no other verb,
// and returns the state it printed: a state systemctl(1) documents is an
// answer whatever the exit status, any other is ErrUnrecognised. What the
// state means is the caller's to decide: UnitState.IsEnabled reads
// enabled-runtime as Yes, which a question about the next boot cannot.
func SystemctlIsEnabled(ctx context.Context, sys host.System, unit string) (string, error) {
	return systemctl(ctx, sys, "is-enabled", unit)
}

// ServiceRunlevels lists the OpenRC runlevels service is added to, as
// DetectSSH reads sshd's: Yes with them, No for none, Unknown with the
// error when an entry cannot be checked and none was found.
func ServiceRunlevels(ctx context.Context, sys host.System, service string) ([]string, Tri, error) {
	return runlevels(ctx, sys, service)
}

// SystemctlIsActive runs systemctl is-active unit, and no other verb, and
// returns the state it printed, by SystemctlIsEnabled's rule
// (next/p4-check-updates: whether apt's timers are running now). What the
// state means is the caller's: UnitState.IsActive is one reading.
func SystemctlIsActive(ctx context.Context, sys host.System, unit string) (string, error) {
	return systemctl(ctx, sys, "is-active", unit)
}
