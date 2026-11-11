package firewall

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/firewalld"
	"github.com/abbott/hardn/pkg/host/packages"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/revert/plans"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// The Firewall item on Rocky Linux and AlmaLinux: firewalld installed,
// enabled with the SSH ports let through, and disabled
// (docs/engine/el-firewalld.md). ufw's profiles and trusted interfaces
// stay refused there (ErrEL).

// firewalldPackage is the package that brings firewalld, and
// elSSPackage the one that brings ss on Rocky Linux and AlmaLinux (no
// iproute2 provide: rocky10-test-110, probe 3b).
const (
	firewalldPackage = "firewalld"
	elSSPackage      = "iproute"
)

var (
	// ErrFirewalldMissing: Enable or Disable on a host without firewalld.
	ErrFirewalldMissing = errors.New("firewalld is not installed")
	// ErrRunningNotAtBoot: firewalld runs and doesn't start at boot. A
	// confirmation tests what runs now, not what its saved configuration
	// starts at the next boot, so hardn doesn't enable it there.
	ErrRunningNotAtBoot = errors.New("firewalld runs and doesn't start at boot")
	// ErrUFWBeside: ufw is installed where firewalld would go, one case of
	// another firewall.
	ErrUFWBeside = fmt.Errorf("ufw is installed: %w", firewalld.ErrOtherFirewall)
)

// The codes of the Rocky Linux and AlmaLinux refusals (pkg/output).
const (
	codeFirewalldUnknown    = "FIREWALLD_UNKNOWN"
	codeFirewalldMissing    = "FIREWALLD_NOT_INSTALLED"
	codeFirewalldNotAtBoot  = "FIREWALLD_NOT_AT_BOOT"
	codeFirewalldOther      = "FIREWALLD_OTHER_FIREWALL"
	codeFirewalldNoCleanup  = "FIREWALLD_NO_CLEANUP"
	codeFirewalldBootBlock  = "FIREWALLD_BOOT_BLOCKED"
	codeFirewalldBootUnread = "FIREWALLD_BOOT_UNKNOWN"
	codeFirewalldZone       = "FIREWALLD_ZONE_NO_SSH"
)

// planEL is Plan on Rocky Linux and AlmaLinux: Install, Install iproute2
// (iproute there), Enable and Disable drive firewalld; saving hardn.yml's
// lists is as elsewhere; applying ufw's profiles or interfaces is refused.
func (c Incoming) planEL(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, bool, error) {
	switch c.Action {
	case ActionInstall:
		cs, err := installFirewalld(ctx, sys, in.Facts)
		return cs, true, err
	case ActionInstallSS:
		cs, err := installSS(ctx, sys, in.Facts)
		return cs, true, err
	case ActionEnable:
		cs, err := enableFirewalld(ctx, sys, in)
		return cs, true, err
	case ActionDisable:
		cs, err := disableFirewalld(ctx, sys)
		return cs, true, err
	case ActionApplyProfiles, ActionApplyInterfaces:
		return nil, true, &steps.Refusal{Code: codeEL, Why: "hardn doesn't apply ufw's profiles or trusted interfaces on Rocky Linux, AlmaLinux or RHEL, which use firewalld", Err: ErrEL}
	}
	return nil, false, nil
}

// firewalldUnknown is a read of firewalld that failed, as a refusal.
func firewalldUnknown(err error) error {
	if errors.Is(err, firewalld.ErrFailed) {
		return &steps.Refusal{Code: "FIREWALLD_FAILED", Why: "firewalld's service failed, and its rules may still be loaded",
			Fix: "see `journalctl -u firewalld`, fix it, then run `systemctl reset-failed firewalld.service`", Err: err}
	}
	return &steps.Refusal{Code: codeFirewalldUnknown, Why: "hardn can't read firewalld here", Fix: "check that `systemctl status firewalld` answers, then try again", Err: err}
}

// installFirewalld is Install's plan there: dnf's (packages.Dnf), refused
// beside ufw or another firewall unit. The package's preset would enable
// firewalld.service (Rocky's 90-default.preset), so the change masks the
// unit before the install and unmasks it after, then checks it is
// disabled: installed, not running, not started at boot, as Install UFW
// leaves ufw inactive.
func installFirewalld(ctx context.Context, sys host.System, f detect.Facts) ([]engine.Change, error) {
	tool, err := packages.ToolFor(f)
	if err != nil {
		return nil, steps.PackageToolRefusal(err)
	}
	switch tri, err := packages.Installed(ctx, sys, tool, ufwPackage); {
	case err != nil:
		return nil, err
	case tri == detect.Yes:
		return nil, &steps.Refusal{Code: codeFirewalldOther, Why: "ufw is installed here, and hardn won't put firewalld beside it",
			Fix: "remove ufw first, or keep it and leave firewalld out", Err: ErrUFWBeside}
	}
	switch st, err := firewalld.Read(ctx, sys); {
	case err != nil:
		return nil, firewalldUnknown(err)
	case st.Unit == "masked" || st.Unit == "masked-runtime":
		return nil, &steps.Refusal{Code: codeFirewalldBootBlock, Why: "firewalld's service is masked here, and hardn leaves a mask it didn't make",
			Fix: "run `systemctl unmask firewalld.service`, then try again, or leave firewalld out", Err: firewalld.ErrBootBlocked}
	}
	if err := firewalld.CheckOthers(ctx, sys); err != nil {
		return nil, otherRefusal(err)
	}
	p, err := packages.PlanInstall(ctx, sys, tool, firewalldPackage)
	if err != nil {
		return nil, installRefusal(tool, firewalldPackage, err)
	}
	if p.Empty() {
		return nil, nil
	}
	mask, unmask := []string{"systemctl", "mask", firewalld.Service}, []string{"systemctl", "unmask", firewalld.Service}
	after := "installed, not running"
	if also := slices.DeleteFunc(slices.Clone(p.Adds), func(n string) bool { return n == firewalldPackage }); len(also) > 0 {
		after += ", with " + strings.Join(also, ", ")
	}
	undo := func(pkgUndo []string) engine.Undo {
		s := []revert.Step{{Op: revert.OpRun, Argv: unmask, Why: installWhy}}
		return engine.Undo{Steps: append(s, undoSteps(pkgUndo)...)}
	}
	return []engine.Change{{ID: ID + "/install", Summary: "Install firewalld", Title: "Install firewalld", Target: firewalldPackage,
		StateBefore: "not installed", StateAfter: after,
		Commands: []string{host.FormatArgv(mask), host.FormatArgv(p.Argv), host.FormatArgv(unmask)},
		Risk:     engine.RiskPackageInstall, Stage: engine.StagePackages, Resources: []string{steps.ResourceFirewalld},
		Rollback: undo(p.Undo),
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			if _, err := sys.Run(ctx, mask[0], mask[1:]...); err != nil {
				return engine.Ran{}, fmt.Errorf("firewall: %s: %w", host.FormatArgv(mask), err)
			}
			rep, err := packages.Execute(ctx, sys, p)
			u := undo(rep.Undo)
			ran := engine.Ran{Rollback: &u, Detail: fmt.Sprintf("install: verified %v", rep.Installed)}
			if _, uerr := sys.Run(context.WithoutCancel(ctx), unmask[0], unmask[1:]...); uerr != nil && err == nil {
				err = fmt.Errorf("firewall: %s: %w", host.FormatArgv(unmask), uerr)
			}
			if err != nil {
				return ran, err
			}
			st, err := firewalld.Read(ctx, sys)
			if err == nil && st.Unit != "disabled" {
				err = fmt.Errorf("firewall: firewalld.service %q after the install, not disabled: %w", st.Unit, firewalld.ErrUnknown)
			}
			return ran, err
		}}}, nil
}

// otherRefusal is firewalld.CheckOthers' error as a refusal.
func otherRefusal(err error) error {
	var other *firewalld.OtherFirewallError
	if errors.As(err, &other) {
		return &steps.Refusal{Code: codeFirewalldOther, Why: "another firewall is on here: " + strings.Join(other.Units, ", "),
			Fix: "stop and disable it first, or keep it and leave firewalld off", Err: err}
	}
	return firewalldUnknown(err)
}

// sshPorts are the ports Enable lets through: sshd's configured listeners'
// and the applying session's server port, refused as ufw's Enable is
// where they are not read (ss missing or failing, a listener unbound, a
// remote session's port unread).
func sshPorts(ctx context.Context, sys host.System, in engine.Input) ([]uint16, error) {
	sk := ReadSockets(ctx, sys, in.Facts)
	switch {
	case sk.NoSS:
		return nil, &steps.Refusal{Code: codeNoSS, Why: "hardn can't tell which ports SSH uses: ss isn't installed",
			Fix: "install " + elSSPackage + ", then try again", Err: ErrNoSS}
	case sk.SSFailed:
		return nil, &steps.Refusal{Code: codeSSFailed, Why: "hardn can't tell which ports SSH uses: ss failed",
			Fix: "check that it runs, then try again", Err: fmt.Errorf("%w: %v", ErrSSFailed, sk.err)}
	case len(sk.Unbound) > 0:
		return nil, &steps.Refusal{Code: codeNotInEffect, Why: "sshd isn't listening where its config says, so hardn can't tell which ports SSH uses",
			Fix: "reload sshd, then try again", Err: ErrNotInEffect}
	}
	ls, ok := sk.Listeners()
	if !ok {
		return nil, &steps.Refusal{Code: codeListeners, Why: "hardn can't tell which ports SSH uses: " + sk.Why, Err: ufw.ErrListenersUnknown}
	}
	ports := ls.Ports()
	c, err := revert.ParseConn(in.Session.Conn)
	switch {
	case err == nil:
		if p := c.Server.Port(); !slices.Contains(ports, p) {
			ports = append(ports, p)
			slices.Sort(ports)
		}
	case in.Session.Remote:
		return nil, &steps.Refusal{Code: codeListeners, Why: "hardn can't tell which port this SSH session came in on",
			Fix: "run hardn from a session it can read (not inside tmux or screen), then try again", Err: fmt.Errorf("%w: %v", steps.ErrSessionPort, err)}
	}
	if len(ports) == 0 {
		return nil, &steps.Refusal{Code: codeListeners, Why: "hardn can't tell which ports SSH uses", Err: ufw.ErrListenersUnknown}
	}
	return ports, nil
}

// enableFirewalld is Enable's plan there (firewalld.PlanEnable): from a
// stopped firewalld, one lockout-risk change, its undo a checked plan
// (plans.FirewalldEnable) journalled before. Running and starting at boot
// is set: nothing. Running and not starting at boot is refused
// (ErrRunningNotAtBoot).
func enableFirewalld(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	st, err := firewalld.Read(ctx, sys)
	switch {
	case err != nil:
		return nil, firewalldUnknown(err)
	case st.Installed == detect.No:
		return nil, &steps.Refusal{Code: codeFirewalldMissing, Why: "firewalld isn't installed", Fix: "install it from the Firewall screen, then try again", Err: ErrFirewalldMissing}
	case st.Running == detect.Yes && st.Enabled == detect.Yes:
		return nil, nil
	case st.Running == detect.Yes:
		return nil, &steps.Refusal{Code: codeFirewalldNotAtBoot, Why: "firewalld runs and doesn't start at boot, and hardn can't tell if its saved configuration lets SSH through after a restart",
			Fix: "disable it, then enable it from the Firewall screen, which starts it from that configuration and checks SSH", Err: ErrRunningNotAtBoot}
	}
	ports, err := sshPorts(ctx, sys, in)
	if err != nil {
		return nil, err
	}
	p, err := firewalld.PlanEnable(ctx, sys, st, ports)
	if err != nil {
		return nil, enableRefusal(err)
	}
	step, err := plans.FirewalldEnable(p.Undo(), "firewalld, which hardn started")
	if err != nil {
		return nil, err
	}
	var cmds []string
	for _, a := range p.Commands() {
		cmds = append(cmds, host.FormatArgv(a))
	}
	after := "running, at boot"
	if !p.Enable {
		after = "running"
	}
	return []engine.Change{{ID: ID + "/firewalld", Summary: "Enable firewalld", Title: "Enable firewalld", Target: "firewalld",
		StateBefore: "not running", StateAfter: after, Commands: cmds,
		Risk: engine.RiskLockout, Stage: engine.StageFirewall, Resources: []string{steps.ResourceSSHD, steps.ResourceFirewalld},
		Rollback: engine.Undo{Steps: []revert.Step{step}},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			err := firewalld.ExecuteEnable(ctx, sys, p)
			return engine.Ran{Detail: "firewalld: zones " + strings.Join(p.Zones, ", ")}, err
		}}}, nil
}

// enableRefusal is firewalld.PlanEnable's error as a refusal.
func enableRefusal(err error) error {
	var zone *firewalld.ZoneError
	switch {
	case errors.Is(err, firewalld.ErrOtherFirewall):
		return otherRefusal(err)
	case errors.Is(err, firewalld.ErrNoCleanup):
		return &steps.Refusal{Code: codeFirewalldNoCleanup, Why: "firewalld is set to leave its rules loaded when it stops, so stopping it wouldn't undo it",
			Fix: "set CleanupOnExit=yes in " + firewalld.ConfFile + ", then try again", Err: err}
	case errors.Is(err, firewalld.ErrBootBlocked):
		return &steps.Refusal{Code: codeFirewalldBootBlock, Why: "firewalld's service is masked or missing, so it can't start",
			Fix: "run `systemctl unmask firewalld.service`, then try again", Err: err}
	case errors.Is(err, firewalld.ErrBootUnknown):
		return &steps.Refusal{Code: codeFirewalldBootUnread, Why: "hardn can't tell how firewalld's service starts at boot", Fix: "check `systemctl is-enabled firewalld.service`, then try again", Err: err}
	case errors.As(err, &zone):
		return &steps.Refusal{Code: codeFirewalldZone, Why: fmt.Sprintf("firewalld's zone %s (%s) doesn't let SSH port %d through, and hardn changes only the default zone", zone.Zone, zone.By, zone.Port),
			Fix: fmt.Sprintf("let it through that zone (`firewall-offline-cmd --zone=%s --add-port=%d/tcp`), then try again", zone.Zone, zone.Port), Err: err}
	}
	return firewalldUnknown(err)
}

// disableFirewalld is Disable's plan there (firewalld.PlanDisable): a
// plain change, its undo an enable that never starts. Nothing where
// firewalld is not installed, or stopped and not starting at boot.
func disableFirewalld(ctx context.Context, sys host.System) ([]engine.Change, error) {
	st, err := firewalld.Read(ctx, sys)
	switch {
	case err != nil:
		return nil, firewalldUnknown(err)
	case st.Installed == detect.No:
		return nil, nil
	}
	p, err := firewalld.PlanDisable(ctx, sys, st)
	switch {
	case errors.Is(err, firewalld.ErrNoCleanup):
		return nil, &steps.Refusal{Code: codeFirewalldNoCleanup, Why: "firewalld is set to leave its rules loaded when it stops, so stopping it wouldn't turn it off",
			Fix: "set CleanupOnExit=yes in " + firewalld.ConfFile + ", then try again", Err: err}
	case err != nil:
		return nil, firewalldUnknown(err)
	case p.Empty():
		return nil, nil
	}
	var cmds []string
	for _, a := range p.Commands() {
		cmds = append(cmds, host.FormatArgv(a))
	}
	var undo []revert.Step
	for _, a := range p.Undo() {
		undo = append(undo, revert.Step{Op: revert.OpRun, Argv: a, Why: "firewalld, which hardn disabled"})
	}
	before := "running"
	if !p.Stop {
		before = "not running, starts at boot"
	}
	return []engine.Change{{ID: ID + "/disable", Summary: "Disable firewalld", Title: "Disable firewalld", Target: "firewalld",
		StateBefore: before, StateAfter: "not running", Commands: cmds,
		Risk: engine.RiskPlain, Stage: engine.StageFirewall, Resources: []string{steps.ResourceFirewalld},
		Rollback: engine.Undo{Steps: undo},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			return engine.Ran{}, firewalld.ExecuteDisable(ctx, sys, p)
		}}}, nil
}
