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
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// The service names. On systemd it is ssh.service on Debian, Ubuntu and
// Proxmox VE (its sshd.service alias is missing on a fresh Ubuntu 24.04),
// and sshd.service on Rocky Linux and AlmaLinux (detect.SSH.ServiceUnit,
// which the steps use); on OpenRC (Alpine) it is sshd.
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
	ErrSocketDropIn     = errors.New("a drop-in overrides the ssh.socket listeners sshd_config sets")
	ErrListenersUnknown = errors.New("the listeners before or after the change are not established")
	ErrDropsListener    = errors.New("the change drops a listener before it is confirmed")
	ErrAddsListener     = errors.New("a confirmed change adds a listener")
	ErrFlavour          = errors.New("the change may switch the sshd binary OpenRC runs")
	ErrUnitState        = errors.New("the unit's state is not established")
	ErrUnitDown         = errors.New("the unit is not up after the step")
	ErrInUse            = errors.New("a listener the change adds is already bound")
	ErrSocketConfig     = errors.New("ssh.socket is not configured for every listener")
	ErrNotBound         = errors.New("not every listener is bound")
	// ErrRestoredListeners: a rollback's restored ssh.socket drop-ins
	// (Change.RestoredDropIns) would not give the socket exactly the
	// listeners it had (Change.After) once daemon-reload reads them.
	ErrRestoredListeners = errors.New("the restored ssh.socket drop-ins do not give the socket the listeners it had")
	// ErrNotInEffect: the listeners the configuration names are not the
	// ones that accept connections now (CheckInEffect).
	ErrNotInEffect = errors.New("the configured listeners are not the ones in effect")
)

// Change is what the installed configuration changed.
type Change struct {
	// Keywords are the sshd_config keywords the change sets, adds or
	// removes, in any case. None: nothing to reload.
	Keywords []string
	// Before and After are needed when a keyword moves listeners (Port,
	// ListenAddress, AddressFamily). Before is sshd.ReadEffective's
	// Listeners before the change; it must be Known. After is the set the
	// installed files configure: Listeners.SSHD of sshd.Evaluate over the
	// validated candidate.
	Before sshd.Listeners
	After  []sshd.Listener
	// Confirmed: the change follows the confirmation of an earlier one and
	// drops listeners that change kept (brief 2.3 step 5); it may not add
	// one (ErrAddsListener). Otherwise After must keep every listener of
	// Before.
	Confirmed bool
	// Rollback: the change restores the files of one that failed or was not
	// confirmed (p2-revert). It may drop listeners, Before is not used, and
	// a unit that is not up is started (ssh.socket is restarted), so set it
	// only when the unit was up before the change it undoes: that change's
	// gated step ran. Not with Confirmed.
	Rollback bool
	// RestoredDropIns, for a Rollback only (fs.ErrInvalid otherwise), are
	// the ssh.socket drop-ins, as ReadSocketDropIns names them, that the
	// rollback has put back and the change it undoes had removed: v0.3.x's
	// listen.conf, which p2-sshd-migrate removes. Their ListenStream lines
	// decide the socket's listeners again, so on a socket host PlanFor does
	// not refuse them (ErrSocketDropIn); After must then be the listeners
	// the socket had before the change, which the check after daemon-reload
	// compares with the socket's configuration before the socket is
	// restarted, and ss after it. Any other drop-in with ListenStream, and
	// one named as the generator's, is refused as for a forward change. So
	// is a rollback whose restored drop-ins, read again, would not give the
	// socket exactly After (ErrRestoredListeners), before daemon-reload
	// runs: plan the same rollback at plan time over the drop-ins as they
	// are before the change (migrate.Plan.CheckRollback), so that a change
	// whose rollback could not finish is not applied.
	RestoredDropIns []string
	// Binary: the change replaced sshd's binaries (an openssh upgrade),
	// not its files, so no keyword changed; the reload makes the running
	// listener execute the new sshd, as each new connection already runs
	// the new sshd-session (docs/safety/alpine.md §7, the 2026-11-08
	// probe). Not with Keywords, Confirmed or RestoredDropIns
	// (fs.ErrInvalid); with Rollback, a unit that is not up is started.
	Binary bool
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
	// next starts. With Step.Start, a unit that is not up is started.
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
	// It is asked again once the step ran: a unit that is down then is
	// ErrUnitDown, unless Activated.
	Unit   string
	OpenRC bool
	// Start runs instead of Argv when Unit is not up (a Rollback's step);
	// without it, a unit that is not up skips the step (StepReport.NotUp).
	Start []string
	// Activated: Unit is ssh.service on a socket host, which ssh.socket
	// starts at the next connection, so a Unit that is down after Argv, even
	// after a failed Argv, is StepReport.NotUp, not an error.
	Activated bool
	Why       string // one line for the plan view
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
	// these listeners (Change.Before.All) stay configured, and so bound,
	// until the change is confirmed. Write the old Port lines beside the
	// new ones (HoldPorts): Port and ListenAddress accumulate, so sshd, and
	// on a socket host the generator, configure both. Then plan the
	// clean-up with Confirmed.
	Hold []sshd.Listener
	// Adds is the listeners of Verify that Before does not cover. Execute
	// refuses (ErrInUse) before any step when a socket other than Hold's is
	// bound on one of their ports.
	Adds []sshd.Listener
	// DropIns are ssh.socket's pre-existing drop-ins, to report; hardn
	// never edits them here.
	DropIns []SocketDropIn
}

// PlanFor plans the reload of the change in in. It runs nothing, so it
// belongs to the plan, before anything changes. A change with no keywords
// plans nothing, unless Binary, which plans the reload of a change that
// moves no listener. Errors: ErrModeUnknown;
// fs.ErrInvalid for Confirmed with Rollback, for RestoredDropIns
// without Rollback, and for a Binary change with keywords, Confirmed or
// RestoredDropIns; for a listener change ErrListenersUnknown, a
// *HoldError (ErrDropsListener), ErrAddsListener, and on a socket host
// ErrNoGenerator, ErrSocketDropIn (a drop-in with ListenStream that is not
// one of a Rollback's RestoredDropIns), ErrRestoredListeners (those do not
// give the socket exactly After), or fs.ErrInvalid when the drop-ins were
// not read; on OpenRC ErrFlavour.
func PlanFor(in Input) (Plan, error) {
	s, c := in.SSH, in.Change
	base := Plan{Mode: s.Mode, DropIns: slices.Clone(in.DropIns.Files)}
	kws := make([]string, len(c.Keywords))
	for i, k := range c.Keywords {
		kws[i] = strings.ToLower(k)
	}
	switch {
	case c.Binary && (len(kws) > 0 || c.Confirmed || len(c.RestoredDropIns) > 0):
		return base, fmt.Errorf("reload: a Binary change has no keywords, is not Confirmed and restores no drop-ins: %w", fs.ErrInvalid)
	case len(kws) == 0 && !c.Binary:
		return base, nil
	case c.Confirmed && c.Rollback:
		return base, fmt.Errorf("reload: a change is Confirmed or a Rollback, not both: %w", fs.ErrInvalid)
	case len(c.RestoredDropIns) > 0 && !c.Rollback:
		return base, fmt.Errorf("reload: only a Rollback restores ssh.socket drop-ins (%s): %w", strings.Join(c.RestoredDropIns, ", "), fs.ErrInvalid)
	case s.Mode != detect.SSHSocket && s.Mode != detect.SSHService && s.Mode != detect.SSHOpenRC:
		err := s.Err
		if err == nil {
			err = detect.ErrNoSignal
		}
		return base, fmt.Errorf("reload: %w: %w", ErrModeUnknown, err)
	}
	p := base
	p.Listen = slices.ContainsFunc(kws, func(k string) bool { return slices.Contains(listenKeywords, k) })
	if p.Listen {
		if err := planListeners(&p, c); err != nil {
			return base, err
		}
	}
	switch s.Mode {
	case detect.SSHSocket:
		if !p.Listen {
			st := reloadService(s) // ssh.service: sshd.socket is refused by detect
			st.Activated = true
			st.Why += "; when it is not up, the next connection starts sshd from the files on disk"
			p.Steps = []Step{st}
			break
		}
		if err := socketReady(s, in.DropIns, c.RestoredDropIns); err != nil {
			return base, err
		}
		if len(c.RestoredDropIns) > 0 {
			if err := restoredReady(in.DropIns, c.RestoredDropIns, c.After); err != nil {
				return base, err
			}
		}
		restart := Step{Kind: StepIfUp, Unit: SocketUnit, Argv: []string{"systemctl", "restart", SocketUnit},
			Why: "bind the listeners; " + ServiceUnit + ", which requires the socket, restarts with it"}
		if c.Rollback {
			restart.Start = slices.Clone(restart.Argv)
		}
		reread := "rerun sshd-socket-generator, which sets " + SocketUnit + "'s listeners from the files' Port and ListenAddress"
		if len(c.RestoredDropIns) > 0 {
			reread += ", and read the restored " + strings.Join(c.RestoredDropIns, ", ") + ", whose ListenStream lines replace them"
		}
		p.Steps = []Step{
			{Kind: StepRun, Argv: []string{"systemctl", "daemon-reload"}, Why: reread},
			{Kind: StepCheckSocket, Argv: []string{"systemctl", "show", "-p", "Listen", SocketUnit},
				Why: SocketUnit + " is configured for every listener the files name, before any socket is rebound"},
			restart,
		}
	case detect.SSHService:
		st := reloadService(s)
		if c.Rollback {
			st.Start = []string{"systemctl", "start", s.ServiceUnit()}
		}
		p.Steps = []Step{st}
	default: // detect.SSHOpenRC
		if i := slices.IndexFunc(kws, func(k string) bool { return slices.Contains(flavourKeywords, k) }); i >= 0 {
			return base, fmt.Errorf("reload: %s: rc-service sshd reload signals the binary the new files call for (sshd, sshd.pam, sshd.krb5), which may not be the one running: %w", c.Keywords[i], ErrFlavour)
		}
		st := Step{Kind: StepIfUp, Unit: OpenRCService, OpenRC: true, Argv: []string{"rc-service", OpenRCService, "reload"},
			Why: "the init script checks the files (checkconfig: sshd -t) and signals sshd to reread them"}
		if c.Rollback {
			st.Start = []string{"rc-service", OpenRCService, "start"}
		}
		p.Steps = []Step{st}
	}
	return p, nil
}

// planListeners sets p's Verify, Hold and Adds for a listener change: a
// forward change keeps Before, a confirmed one only drops, and a rollback
// restores After whatever Before was.
func planListeners(p *Plan, c Change) error {
	before := c.Before.All
	switch {
	case len(c.After) == 0 || !c.Rollback && (!c.Before.Known || len(before) == 0):
		return fmt.Errorf("reload: listeners before %v (known %v), after %v: %w", before, c.Before.Known, c.After, ErrListenersUnknown)
	case c.Rollback:
	case c.Confirmed:
		if extra := uncovered(c.After, before, false); len(extra) > 0 {
			return fmt.Errorf("reload: %v: plan a change that adds listeners unconfirmed, holding the old ones: %w", extra, ErrAddsListener)
		}
	default:
		if missing := uncovered(before, c.After, false); len(missing) > 0 {
			return &HoldError{Missing: missing}
		}
		p.Hold = slices.Clone(before)
		p.Adds = uncovered(c.After, before, false)
	}
	p.Verify = slices.Clone(c.After)
	return nil
}

// reloadService reloads the unit detect found running sshd: Debian's
// ssh.service runs sshd -t before it signals sshd; Rocky Linux's and
// AlmaLinux's sshd.service only signals it (ExecReload=/bin/kill -HUP
// $MAINPID), so Validate's sshd -t over the copy is the only check of the
// files before the reload there (docs/engine/el-changes.md).
func reloadService(s detect.SSH) Step {
	unit := s.ServiceUnit()
	why := unit + " checks the files (sshd -t) and signals sshd to reread them"
	if s.SSHDUnits {
		why = unit + " signals sshd to reread the files, which hardn checked before installing them"
	}
	return Step{Kind: StepIfUp, Unit: unit, Argv: []string{"systemctl", "reload", unit}, Why: why}
}

// generatorDropIn is the drop-in sshd-socket-generator writes.
const generatorDropIn = "addresses.conf"

// socketReady refuses a listener change the socket would not follow: one
// whose listeners do not come from the generator, or that a drop-in
// overrides, other than one a rollback restored (Change.RestoredDropIns).
func socketReady(s detect.SSH, d SocketDropIns, restored []string) error {
	switch {
	case s.Generator != detect.Yes:
		return fmt.Errorf("reload: sshd-socket-generator %s (%s): %w", s.Generator, s.GeneratorPath, ErrNoGenerator)
	case !d.Read:
		return fmt.Errorf("reload: a listener change on a socket-activated host needs ReadSocketDropIns's answer: %w", fs.ErrInvalid)
	}
	var over []string
	for _, f := range d.Files {
		// A drop-in named as the generator's, in a directory read here,
		// replaces it or masks it (systemd.unit(5)).
		if path.Base(f.Path) == generatorDropIn || len(f.Listen) > 0 && !slices.Contains(restored, f.Path) {
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
