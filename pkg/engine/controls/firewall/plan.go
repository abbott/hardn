package firewall

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// ProfileSetting is hardn.yml's key for the application profiles.
const ProfileSetting = "ufwAppProfiles"

// Errors of Enable's refusals, testable with errors.Is, each the Err of a
// *steps.Refusal in the decided words. The first three wrap
// ufw.ErrListenersUnknown, of which they are the more specific cases.
var (
	// ErrNoSS: ss is not installed, so what is bound is not read (decision 2).
	ErrNoSS = fmt.Errorf("ss not installed: %w", ufw.ErrListenersUnknown)
	// ErrSSFailed: ss ran and failed (decision 2).
	ErrSSFailed = fmt.Errorf("ss failed: %w", ufw.ErrListenersUnknown)
	// ErrNotInEffect: a configured listener is not bound, with a session or
	// from a console (decision 2).
	ErrNotInEffect = fmt.Errorf("sshd's configuration not in effect: %w", ufw.ErrListenersUnknown)
	// ErrRefused: ufw refuses an SSH listener, and there is nothing Enable
	// can plan: a rule hardn leaves alone, or IPV6=no.
	ErrRefused = errors.New("ufw refuses an SSH listener, and hardn changes no rule it finds")
	// ErrBootUnknown: there is nothing else to plan, and whether ufw starts
	// at boot is not read.
	ErrBootUnknown = errors.New("whether ufw starts at boot is not established")
)

// The refusals' codes, the registry's for the errors they wrap (pkg/output;
// pinned by the tests).
const (
	codeNoSS          = "FIREWALL_SS_NOT_INSTALLED"
	codeSSFailed      = "FIREWALL_SS_FAILED"
	codeNotInEffect   = "FIREWALL_SSH_NOT_IN_EFFECT"
	codeRefused       = "FIREWALL_SSH_REFUSED"
	codeBootUnknown   = "FIREWALL_BOOT_UNKNOWN"
	codeNotApplicable = "UFW_NOT_APPLICABLE"
	codeProxmox       = "UFW_PROXMOX_UNKNOWN"
	codeListeners     = "UFW_LISTENERS_UNKNOWN"
	codeState         = "UFW_STATE_UNKNOWN"
	codeMismatch      = "UFW_ENABLED_MISMATCH"
	codeBlocks        = "UFW_WOULD_BLOCK"
	codeInactive      = "UFW_INACTIVE"
	codeBadProfile    = "UFW_BAD_PROFILE"
)

// Plan is what Action does on the host in.Facts describe. ActionNone, the
// status list's, is engine.ErrNotMigrated with no change, as a check-only
// control's; and so, for now, is ActionInstall, on a host where it may run.
// Every other action reads the host, and refuses before anything changes.
func (c Incoming) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	if c.Action == ActionNone {
		return nil, engine.ErrNotMigrated
	}
	if err := applies(in.Facts.Proxmox); err != nil {
		return nil, err
	}
	switch c.Action {
	case ActionInstall:
		return nil, engine.ErrNotMigrated
	case ActionEnable:
		return enable(ctx, sys, in)
	case ActionDisable:
		changes, err := steps.PlanUFWDisable(ctx, sys, in, ID)
		return changes, refusal(ctx, sys, in, err)
	case ActionSaveProfiles:
		if _, err := profilesOf(in.Config); err != nil {
			return nil, err
		}
		return steps.PlanSave(ctx, sys, ID+"/profiles", in.Config, []string{ProfileSetting})
	case ActionApplyProfiles:
		profiles, err := profilesOf(in.Config)
		if err != nil || len(profiles) == 0 {
			return nil, err
		}
		changes, err := steps.PlanUFWApps(ctx, sys, in, ID, profiles)
		return changes, refusal(ctx, sys, in, err)
	}
	return nil, fmt.Errorf("firewall: action %d: %w", c.Action, fs.ErrInvalid)
}

// applies refuses Proxmox VE, and a host where that is not established, before
// any ufw call (brief 2.5). The words on Proxmox VE are its screen's.
func applies(p detect.Proxmox) error {
	err := ufw.Applies(p)
	switch {
	case errors.Is(err, ufw.ErrNotApplicable):
		return &steps.Refusal{Code: codeNotApplicable, Why: "hardn reads pve-firewall and doesn't change it: a wrong rule can cut off the web UI and the cluster", Err: err}
	case errors.Is(err, ufw.ErrProxmoxUnknown):
		return &steps.Refusal{Code: codeProxmox, Why: "hardn can't tell if this host runs Proxmox VE, where it leaves the firewall alone", Err: err}
	}
	return err
}

// enable is Enable's plan: refused where what accepts SSH is not read (decision
// 2: ss missing or failing, or a configured listener not bound, with a session
// or from a console); steps.PlanUFWEnable, lockout-risk; and where ufw's service
// does not start at boot, a plain change after it, so that a plan holding that
// change alone is plain. Nothing to plan under a row that says a listener is
// refused, or whether ufw starts at boot not read, is a refusal with its cause;
// on a host that is set, an empty plan.
func enable(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	sk := ReadSockets(ctx, sys, in.Facts)
	switch {
	case sk.NoSS:
		return nil, &steps.Refusal{Code: codeNoSS, Why: "hardn can't tell which ports SSH uses: ss isn't installed",
			Fix: "install iproute2, then try again", Err: ErrNoSS}
	case sk.SSFailed:
		return nil, &steps.Refusal{Code: codeSSFailed, Why: "hardn can't tell which ports SSH uses: ss failed",
			Fix: "check that it runs, then try again", Err: fmt.Errorf("%w: %v", ErrSSFailed, sk.err)}
	case len(sk.Unbound) > 0:
		return nil, &steps.Refusal{Code: codeNotInEffect, Why: "sshd isn't listening where its config says, so hardn can't tell which ports SSH uses",
			Fix: "reload sshd or restart ssh.socket, then try again", Err: ErrNotInEffect}
	}
	changes, err := steps.PlanUFWEnable(ctx, sys, in, ID)
	if err != nil {
		return nil, refusal(ctx, sys, in, err)
	}
	boot := ufw.ReadBoot(ctx, sys, in.Facts)
	if boot == detect.No {
		var after []string
		for _, c := range changes {
			after = append(after, c.ID)
		}
		changes = append(changes, steps.UFWBootChange(ID, in.Facts, after))
	}
	if len(changes) > 0 {
		return changes, nil
	}
	st, err := ufw.Read(ctx, sys, in.Facts.Proxmox)
	if err != nil {
		return nil, err
	}
	return nil, nothing(st, sk, boot)
}

// nothing is why an Enable with nothing to plan is refused: IPv6 off in ufw, a
// listener ufw refuses by a rule hardn leaves alone (ufw.PlanFor changes no rule
// it finds), or ufw's service not read. nil on a host that is set.
func nothing(st ufw.State, sk Sockets, boot detect.Tri) error {
	cs := verdicts(st, sk)
	i := slices.IndexFunc(cs, func(c ufw.Coverage) bool { return c.Verdict == ufw.Blocked || c.Verdict == ufw.Denied })
	switch {
	case i >= 0 && ipv6Off(st, cs[i]):
		return ipv6Refusal(cs[i], ErrRefused)
	case i >= 0:
		addr, _, _ := strings.Cut(strings.TrimPrefix(cs[i].Need.Why, "listener "), " (")
		return &steps.Refusal{Code: codeRefused, Why: "ufw refuses SSH on " + addr + ", and hardn doesn't change a rule it finds",
			Fix: "fix it with ufw allow or ufw delete, then try again", Err: ErrRefused}
	case boot == detect.Unknown:
		return &steps.Refusal{Code: codeBootUnknown, Why: "hardn can't tell if ufw starts at boot", Fix: "check its service, then try again", Err: ErrBootUnknown}
	}
	return nil
}

// refusal is err, a ufw plan's, as a *steps.Refusal in the decided words where
// it has them; any other error, nil included, as it is. A saved rule that would
// block a listener (a deny, or a blocklist: P-33 (3) (a)) names its port, the
// one IPV6=no refuses first; ENABLED and the chains disagreeing is said by which
// way, which ufw's state read again says.
func refusal(ctx context.Context, sys host.System, in engine.Input, err error) error {
	var be *ufw.BlockError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &be):
		st, rerr := ufw.Read(ctx, sys, in.Facts.Proxmox)
		if i := slices.IndexFunc(be.Coverage, func(c ufw.Coverage) bool { return ipv6Off(st, c) }); i >= 0 && rerr == nil {
			return ipv6Refusal(be.Coverage[i], err)
		}
		return &steps.Refusal{Code: codeBlocks, Why: fmt.Sprintf("A saved ufw rule would block SSH on port %d, for some addresses or all", be.Coverage[0].Need.Port),
			Fix: "remove it with ufw delete, then try again", Err: err}
	case errors.Is(err, ufw.ErrListenersUnknown):
		return &steps.Refusal{Code: codeListeners, Why: "hardn can't tell which ports SSH uses", Err: err}
	case errors.Is(err, ufw.ErrStateUnknown):
		return &steps.Refusal{Code: codeState, Why: "hardn can't read ufw's state", Fix: "check ufw status verbose, then try again", Err: err}
	case errors.Is(err, ufw.ErrInactive):
		return &steps.Refusal{Code: codeInactive, Why: "ufw isn't running, so hardn saves no rule for it", Fix: "enable the firewall, then apply", Err: err}
	case errors.Is(err, ufw.ErrEnabledMismatch):
		if st, rerr := ufw.Read(ctx, sys, in.Facts.Proxmox); rerr == nil && !st.Status.Active {
			return &steps.Refusal{Code: codeMismatch, Why: "ufw is set to start at boot but isn't running",
				Fix: "check that its rules let SSH through, then run ufw enable", Err: err}
		}
		return &steps.Refusal{Code: codeMismatch, Why: "ufw is running but isn't set to start at boot", Fix: "run ufw enable", Err: err}
	}
	return err
}

// ipv6Off: ufw refuses c because IPv6 is off in its file, not by any rule.
func ipv6Off(st ufw.State, c ufw.Coverage) bool {
	return c.Need.Family == ufw.IPv6 && !st.IPv6 && !st.NoKernelIPv6
}

func ipv6Refusal(c ufw.Coverage, err error) error {
	code := codeBlocks
	if errors.Is(err, ErrRefused) {
		code = codeRefused
	}
	return &steps.Refusal{Code: code, Why: fmt.Sprintf("ufw's IPv6 is off in %s, so it would refuse SSH on port %d", ufw.DefaultsFile, c.Need.Port),
		Fix: "set IPV6=yes, then try again", Err: err}
}

// verdicts is ufw's verdict on each SSH listener: while active under what ufw
// shows, else under what its saved rules do once it starts. nil when sshd's
// listeners were not read.
func verdicts(st ufw.State, sk Sockets) []ufw.Coverage {
	ls, ok := sk.Listeners()
	if !ok {
		return nil
	}
	needs := ufw.Needs(ls, nil, nil)
	fw := ufw.Firewall{Active: true, IPv6: st.IPv6, NoKernelIPv6: st.NoKernelIPv6}
	if st.Status.Active {
		fw.Incoming = st.Status.Incoming
		return ufw.Evaluate(st.Status.Rules, fw, needs)
	}
	fw.Incoming = st.Incoming
	return ufw.Evaluate(st.Rules, fw, needs)
}

// profilesOf is hardn.yml's application profiles as ufw's, every one valid, none
// named twice; else a refusal naming the first that is not. Nil cfg is none.
func profilesOf(cfg *config.Config) ([]ufw.Profile, error) {
	if cfg == nil {
		return nil, nil
	}
	out := make([]ufw.Profile, 0, len(cfg.UfwAppProfiles))
	for _, p := range cfg.UfwAppProfiles {
		pr := ufw.Profile{Name: p.Name, Title: p.Title, Description: p.Description, Ports: p.Ports}
		err := ufw.ValidProfile(pr)
		if err == nil && slices.ContainsFunc(out, func(o ufw.Profile) bool { return o.Name == p.Name }) {
			err = fmt.Errorf("ufw: profile %q: named twice: %w", p.Name, ufw.ErrBadProfile)
		}
		if err != nil {
			return nil, &steps.Refusal{Code: codeBadProfile, Why: "ufw can't read the profile " + p.Name, Fix: "correct it in hardn.yml, then try again", Err: err}
		}
		out = append(out, pr)
	}
	return out, nil
}
