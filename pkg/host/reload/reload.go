// Package reload makes the running sshd use an installed configuration: the
// commands that make it reread its files, in the order its init system needs,
// and the checks that show its listeners came up (refactor plan P2,
// next/p2-reload). It is the mechanics only: which change goes first (brief
// 2.3: the firewall before the port) is p3-engine's.
//
// PlanFor is pure: from detect's SSH facts and what changed, it returns the
// steps, the listeners to verify, and the listeners a listener change must
// keep until it is confirmed (Plan.Hold). Execute runs a Plan over
// host.System. On systemd only ssh.service and ssh.socket are named, never
// the sshd alias, which a fresh Ubuntu 24.04 lacks.
//
// docs/safety/reload.md describes the guarantees, the platform facts they
// rest on and what is written but not yet run on a Linux host.
package reload

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// The service names. On systemd it is ssh.service on Debian, Ubuntu and
// Proxmox VE (its sshd.service alias is missing on a fresh Ubuntu 24.04); on
// OpenRC (Alpine) it is sshd.
const (
	ServiceUnit   = "ssh.service"
	SocketUnit    = sshd.SocketUnit
	OpenRCService = "sshd"
)

// listenKeywords move sshd's listeners; Ubuntu's stock sshd_config names
// these three as needing daemon-reload and a restart of ssh.socket.
var listenKeywords = []string{"port", "listenaddress", "addressfamily"}

// flavourKeywords decide which sshd binary Alpine's init script runs
// (OpenRCFlavour); a reload signals the binary the new files call for.
var flavourKeywords = []string{"usepam", "kerberosauthentication", "gssapiauthentication"}

// Errors PlanFor and Execute return, testable with errors.Is.
var (
	ErrModeUnknown      = errors.New("how sshd is started is not established")
	ErrNoGenerator      = errors.New("ssh.socket's listeners are not derived from sshd_config")
	ErrSocketDropIn     = errors.New("a drop-in sets ssh.socket's ListenStream")
	ErrListenersUnknown = errors.New("the listeners before or after the change are not established")
	ErrDropsListener    = errors.New("the change drops a listener before it is confirmed")
	ErrFlavour          = errors.New("the change may switch the sshd binary OpenRC runs")
	ErrUnitState        = errors.New("the unit's state is not established")
	ErrSocketConfig     = errors.New("ssh.socket is not configured for every listener")
	ErrNotBound         = errors.New("not every listener is bound")
)

// Change is what the installed configuration changed.
type Change struct {
	// Keywords are the sshd_config keywords the change sets, adds or
	// removes, in any case. None: nothing to reload.
	Keywords []string
	// Before and After are needed when a keyword moves listeners (Port,
	// ListenAddress, AddressFamily). Before is the configured listener set
	// before the change, established: sshd.ReadEffective's Listeners.All
	// with Known. After is the set the installed files configure:
	// Listeners.SSHD of sshd.Evaluate over the validated candidate.
	Before, After []sshd.Listener
	// Confirmed: the change follows the confirmation of an earlier one and
	// drops the listeners that change kept (brief 2.3 step 5). Otherwise
	// After must keep every listener of Before.
	Confirmed bool
}

// Input is what PlanFor plans from.
type Input struct {
	SSH    detect.SSH // detect.Detect's SSH
	Change Change
	// DropIns is ReadSocketDropIns's answer; a listener change on a
	// socket-activated host needs it read.
	DropIns SocketDropIns
}

// StepKind is what a Step does.
type StepKind uint8

const (
	// StepRun runs Argv; a non-zero exit fails the plan.
	StepRun StepKind = iota + 1
	// StepIfUp runs Argv only when Unit is up at that moment, asked then,
	// not taken from detect: a unit that is not up reads the files when it
	// next starts.
	StepIfUp
	// StepCheckSocket runs Argv (systemctl show -p Listen ssh.socket) and
	// fails unless the socket is configured for every listener of
	// Plan.Verify.
	StepCheckSocket
)

func (k StepKind) String() string {
	switch k {
	case StepRun:
		return "run"
	case StepIfUp:
		return "run-if-up"
	case StepCheckSocket:
		return "check-socket"
	}
	return "unknown"
}

// Step is one action of a Plan.
type Step struct {
	Kind StepKind
	Argv []string
	// Unit gates a StepIfUp: the systemd unit that systemctl is-active must
	// show active, or, with OpenRC, the service OpenRC must have started.
	Unit   string
	OpenRC bool
	Why    string // one line for the plan view
}

// Plan is how to make sshd use an installed change. On an error from
// PlanFor it holds Mode and DropIns only.
type Plan struct {
	Mode   detect.SSHMode
	Listen bool // a keyword moves listeners
	Steps  []Step
	// Verify is the listeners that must be bound once the steps are done
	// (Change.After, for a listener change).
	Verify []sshd.Listener
	// Hold is the caller's requirement for an unconfirmed listener change:
	// these listeners (Change.Before) stay configured, and so bound, until
	// the change is confirmed. Write the old Port lines beside the new ones
	// (HoldPorts): Port and ListenAddress accumulate, so sshd, and on a
	// socket host the generator, configure both. Then plan the clean-up
	// with Confirmed.
	Hold []sshd.Listener
	// DropIns are ssh.socket's pre-existing drop-ins, to report; hardn
	// never edits them here.
	DropIns []SocketDropIn
}

// PlanFor plans the reload of the change in in. It runs nothing. Errors:
// ErrModeUnknown; for a listener change ErrListenersUnknown, a *HoldError
// (ErrDropsListener), and on a socket host ErrNoGenerator, ErrSocketDropIn,
// or fs.ErrInvalid when the drop-ins were not read; on OpenRC ErrFlavour.
func PlanFor(in Input) (Plan, error) {
	s, c := in.SSH, in.Change
	base := Plan{Mode: s.Mode, DropIns: slices.Clone(in.DropIns.Files)}
	kws := make([]string, len(c.Keywords))
	for i, k := range c.Keywords {
		kws[i] = strings.ToLower(k)
	}
	if len(kws) == 0 {
		return base, nil
	}
	if s.Mode != detect.SSHSocket && s.Mode != detect.SSHService && s.Mode != detect.SSHOpenRC {
		err := s.Err
		if err == nil {
			err = detect.ErrNoSignal
		}
		return base, fmt.Errorf("reload: %w: %w", ErrModeUnknown, err)
	}
	p := base
	p.Listen = slices.ContainsFunc(kws, func(k string) bool { return slices.Contains(listenKeywords, k) })
	if p.Listen {
		if len(c.Before) == 0 || len(c.After) == 0 {
			return base, fmt.Errorf("reload: listeners before %v, after %v: %w", c.Before, c.After, ErrListenersUnknown)
		}
		if !c.Confirmed {
			if missing := uncovered(c.Before, c.After, false); len(missing) > 0 {
				return base, &HoldError{Missing: missing}
			}
			p.Hold = slices.Clone(c.Before)
		}
		p.Verify = slices.Clone(c.After)
	}
	switch s.Mode {
	case detect.SSHSocket:
		if !p.Listen {
			p.Steps = []Step{reloadService()}
			break
		}
		if err := socketReady(s, in.DropIns); err != nil {
			return base, err
		}
		p.Steps = []Step{
			{Kind: StepRun, Argv: []string{"systemctl", "daemon-reload"},
				Why: "rerun sshd-socket-generator, which sets " + SocketUnit + "'s listeners from the files' Port and ListenAddress"},
			{Kind: StepCheckSocket, Argv: []string{"systemctl", "show", "-p", "Listen", SocketUnit},
				Why: SocketUnit + " is configured for every listener the files name, before any socket is rebound"},
			{Kind: StepIfUp, Unit: SocketUnit, Argv: []string{"systemctl", "restart", SocketUnit},
				Why: "bind the listeners; " + ServiceUnit + ", which requires the socket, restarts with it"},
		}
	case detect.SSHService:
		p.Steps = []Step{reloadService()}
	default: // detect.SSHOpenRC
		if i := slices.IndexFunc(kws, func(k string) bool { return slices.Contains(flavourKeywords, k) }); i >= 0 {
			return base, fmt.Errorf("reload: %s: rc-service sshd reload signals the binary the new files call for (sshd, sshd.pam, sshd.krb5), which may not be the one running: %w", c.Keywords[i], ErrFlavour)
		}
		p.Steps = []Step{{Kind: StepIfUp, Unit: OpenRCService, OpenRC: true, Argv: []string{"rc-service", OpenRCService, "reload"},
			Why: "the init script checks the files (checkconfig: sshd -t) and signals sshd to reread them"}}
	}
	return p, nil
}

func reloadService() Step {
	return Step{Kind: StepIfUp, Unit: ServiceUnit, Argv: []string{"systemctl", "reload", ServiceUnit},
		Why: ServiceUnit + " checks the files (sshd -t) and signals sshd to reread them"}
}

// socketReady refuses a listener change the socket would not follow: one
// whose listeners do not come from the generator, or that a drop-in
// overrides.
func socketReady(s detect.SSH, d SocketDropIns) error {
	switch {
	case s.Generator != detect.Yes:
		return fmt.Errorf("reload: sshd-socket-generator %s (%s): %w", s.Generator, s.GeneratorPath, ErrNoGenerator)
	case !d.Read:
		return fmt.Errorf("reload: a listener change on a socket-activated host needs ReadSocketDropIns's answer: %w", fs.ErrInvalid)
	}
	var over []string
	for _, f := range d.Files {
		if len(f.Listen) > 0 {
			over = append(over, f.Path)
		}
	}
	if len(over) > 0 {
		return fmt.Errorf("reload: %s: the generator's listeners would not reach %s: %w", strings.Join(over, ", "), SocketUnit, ErrSocketDropIn)
	}
	return nil
}

// HoldError is ErrDropsListener with the listeners After does not keep.
type HoldError struct {
	Missing []sshd.Listener
}

func (e *HoldError) Error() string {
	return fmt.Sprintf("reload: %v: write the old Port lines beside the new ones until the change is confirmed: %s", e.Missing, ErrDropsListener)
}

// Is makes errors.Is(err, ErrDropsListener) true.
func (e *HoldError) Is(target error) bool { return target == ErrDropsListener }

// HoldPorts is the Port lines a listener change writes for its confirm
// window: the ports of hold (Plan.Hold, or Change.Before) and want,
// ascending, each once.
func HoldPorts(hold []sshd.Listener, want []uint16) []uint16 {
	ps := slices.Clone(want)
	for _, l := range hold {
		ps = append(ps, l.Port)
	}
	slices.Sort(ps)
	return slices.Compact(ps)
}
