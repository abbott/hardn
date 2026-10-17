package detect

import (
	"context"
	"errors"
	"fmt"

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
	// SSHSocket: systemd's ssh.socket holds the listening sockets and starts
	// ssh.service; a Port change needs daemon-reload and a restart of
	// ssh.socket. Ubuntu 24.04 and 26.04 by default; opt-in on Debian.
	SSHSocket
	// SSHService: systemd's ssh.service runs sshd, which listens itself.
	// Debian 12 and 13, Proxmox VE 9, Ubuntu 22.04.
	SSHService
	// SSHOpenRC: OpenRC's sshd service (Alpine).
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

// The SSH unit signals. hardn never queries a unit named sshd: that alias is
// missing on a fresh Ubuntu 24.04.
const (
	SocketGenerator = "/usr/lib/systemd/system-generators/sshd-socket-generator" // Ubuntu 24.04+
	OpenRCSSHD      = "/etc/init.d/sshd"
)

// UnitState is what systemctl printed for a unit; "" when not established.
type UnitState struct {
	Enabled string // is-enabled: enabled, disabled, masked, static, not-found, ...
	Active  string // is-active: active, inactive, failed, activating, ...
}

// SSH is how sshd is started.
type SSH struct {
	Mode    SSHMode
	Socket  UnitState // ssh.socket (systemd)
	Service UnitState // ssh.service (systemd)
	// Generator is whether Ubuntu's sshd-socket-generator is installed: it
	// derives ssh.socket's ListenStream from sshd_config's Port and
	// ListenAddress at boot and on daemon-reload (systemd only).
	Generator Tri
	Source    string // what decided Mode
	Err       error  // why Mode, or Generator, is Unknown
}

// DetectSSH asks systemctl about ssh.socket and ssh.service on systemd, and
// looks for the sshd init script on OpenRC. Without a known init system it
// runs nothing.
func DetectSSH(ctx context.Context, sys host.System, in Init) SSH {
	switch in.Kind {
	case OpenRC:
		switch fi, t, err := stat(ctx, sys, OpenRCSSHD); {
		case t == Yes && fi.Mode().IsRegular():
			return SSH{Mode: SSHOpenRC, Source: OpenRCSSHD}
		case t == Unknown:
			return SSH{Err: fmt.Errorf("detect: ssh: %w", err)}
		}
		return SSH{Err: fmt.Errorf("detect: ssh: no %s: %w", OpenRCSSHD, ErrNoSignal)}
	case Systemd:
	default:
		return SSH{Err: fmt.Errorf("detect: ssh: init system not established: %w", errOr(in.Err, ErrNoSignal))}
	}
	var s SSH
	var errs [5]error
	_, s.Generator, errs[0] = stat(ctx, sys, SocketGenerator)
	s.Socket.Enabled, errs[1] = systemctl(ctx, sys, "is-enabled", "ssh.socket")
	s.Socket.Active, errs[2] = systemctl(ctx, sys, "is-active", "ssh.socket")
	s.Service.Enabled, errs[3] = systemctl(ctx, sys, "is-enabled", "ssh.service")
	s.Service.Active, errs[4] = systemctl(ctx, sys, "is-active", "ssh.service")
	if err := errors.Join(errs[1:]...); err != nil {
		s.Err = fmt.Errorf("detect: ssh: %w", err)
		return s
	}
	sockEnabled, svcEnabled := enabledOn(s.Socket.Enabled), enabledOn(s.Service.Enabled)
	sock := either(sockEnabled, activeOn(s.Socket.Active))
	svc := either(svcEnabled, activeOn(s.Service.Active))
	switch {
	case sockEnabled == Yes && svcEnabled == Yes:
		s.Err = fmt.Errorf("detect: ssh: ssh.socket and ssh.service are both enabled: %w", ErrConflict)
	case sock == Yes:
		s.Mode, s.Source = SSHSocket, "ssh.socket "+s.Socket.Enabled+", "+s.Socket.Active
	case sock == No && svc == Yes:
		s.Mode, s.Source = SSHService, "ssh.service "+s.Service.Enabled+", "+s.Service.Active
	case sock == No && svc == No:
		s.Err = fmt.Errorf("detect: ssh: neither ssh.socket nor ssh.service is enabled or active: %w", ErrNoSignal)
	default:
		s.Err = fmt.Errorf("detect: ssh: ssh.socket %s/%s, ssh.service %s/%s: %w",
			s.Socket.Enabled, s.Socket.Active, s.Service.Enabled, s.Service.Active, ErrUnrecognised)
	}
	if s.Generator == Unknown && s.Err == nil {
		s.Err = fmt.Errorf("detect: ssh: %w", errs[0]) // Mode stands; the generator is not established
	}
	return s
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

func enabledOn(state string) Tri { return unitStates["is-enabled"][state] }
func activeOn(state string) Tri  { return unitStates["is-active"][state] }

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
