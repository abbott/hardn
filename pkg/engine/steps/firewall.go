package steps

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// UFW is what an SSH change does with ufw.
type UFW uint8

const (
	// UFWKeep lets the ports the change adds through an active ufw before
	// sshd moves (brief 2.3 step 2), and saves no rule in an inactive one
	// (ufw.Desired.NoSaveWhileInactive; P-33 is open on that). Where ufw is
	// not installed, or on Proxmox VE, it has nothing to do.
	UFWKeep UFW = iota
	// UFWEnable also sets the default incoming policy to deny and enables
	// ufw, after its allow rules (ufw.md §2.7).
	UFWEnable
)

// firewallWhy is the why of every ufw undo step the journal holds.
const firewallWhy = "firewall"

// ErrSessionPort: a remote session whose connection was not read, as under tmux,
// so that starting ufw cannot be shown to keep its port. It wraps
// ufw.ErrListenersUnknown, of which it is the more specific case.
var ErrSessionPort = fmt.Errorf("the port this session is connected to is not established: %w", ufw.ErrListenersUnknown)

// planUFW plans ufw's part of a change: listeners, the ones that accept
// connections now (sshd.ReadEffective's, or a migration's Plan.Listening),
// and the ports the change adds let through before sshd moves, with the
// applying session's own port kept (ufw.md §6). nil when there is nothing
// for ufw to do: UFWKeep with no port added, ufw not installed, or
// Proxmox VE.
func (st sshState) planUFW(ctx context.Context, sys host.System, listeners sshd.Listeners, mode UFW, add []uint16) (*ufw.Plan, error) {
	if mode == UFWKeep && len(add) == 0 {
		return nil, nil
	}
	state, err := ufw.Read(ctx, sys, st.in.Facts.Proxmox)
	switch {
	case errors.Is(err, ufw.ErrNotApplicable) && mode == UFWKeep:
		return nil, nil
	case err != nil:
		return nil, err
	case !state.Installed && mode == UFWKeep:
		return nil, nil
	case !state.Installed:
		return nil, fmt.Errorf("ufw: %w", ufw.ErrNotInstalled)
	}
	in := ufw.Input{Proxmox: st.in.Facts.Proxmox, Listeners: listeners, Add: add, State: state, NoSSH: st.noSSH}
	c, cerr := revert.ParseConn(st.in.Session.Conn)
	switch {
	case cerr == nil:
		in.Keep = []uint16{c.Server.Port()}
	case mode == UFWEnable && st.in.Session.Remote: // fails closed, as inEffect does
		return nil, fmt.Errorf("%w: %v", ErrSessionPort, cerr)
	}
	if mode == UFWEnable {
		in.Desired = ufw.Desired{Enable: true, Incoming: ufw.PolicyDeny}
	} else {
		in.Desired = ufw.Desired{NoSaveWhileInactive: true}
	}
	p, err := ufw.PlanFor(in)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ufwTitle is ufw's plan's Title: the ports it allows, then whether it
// sets a default policy and enables ufw ("ufw: allow 22/tcp, 2230/tcp;
// enable"), or how many steps when that is too long (engine.TitleMax).
func ufwTitle(p ufw.Plan) string {
	var ports, rest []string
	for _, s := range p.Steps {
		switch s.Kind {
		case ufw.StepAllow:
			ports = append(ports, fmt.Sprintf("%d/tcp", s.Port))
		case ufw.StepDefault:
			rest = append(rest, "default policy")
		case ufw.StepEnable:
			rest = append(rest, "enable")
		}
	}
	if len(ports) > 0 {
		rest = append([]string{"allow " + strings.Join(ports, ", ")}, rest...)
	}
	return engine.FitTitle("ufw: "+strings.Join(rest, "; "), fmt.Sprintf("ufw: %d steps", len(p.Steps)))
}

// firewallChange is ufw's plan as a change, before sshd moves
// (StageFirewall): its default and enable undos journalled before it runs
// (Plan.PreUndo), and once it ran, error or not, the undos of what it did
// (Report.Undo, from ufw's answers: an allow ufw kept is not deleted), which
// the engine journals under a context without the apply's cancellation. It is
// lockout-risk; PlanUFWDisable's, which cannot lock anyone out (decision 6), says
// otherwise.
func firewallChange(id string, p ufw.Plan) engine.Change {
	var argvs [][]string
	for _, s := range p.Steps {
		argvs = append(argvs, s.Argv)
	}
	c := engine.Change{
		ID: id, Summary: "Firewall: " + strings.Join(formatArgvs(argvs), "; "), Title: ufwTitle(p), Target: "ufw", Commands: formatArgvs(argvs),
		StateBefore: activeWord(p, false), StateAfter: activeWord(p, true),
		Risk: engine.RiskLockout, Stage: engine.StageFirewall, Findings: ufwFindings(p.Findings),
		Rollback: engine.Undo{Steps: runSteps(firewallWhy, p.PreUndo())},
	}
	c.Apply = func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
		rep, err := ufw.Execute(ctx, sys, p)
		u := engine.Undo{Steps: runSteps(firewallWhy, rep.Undo)}
		return engine.Ran{Rollback: &u, Detail: fmt.Sprintf("firewall: verified %v%s", rep.Verified, because(rep.Unverified))}, err
	}
	return c
}

// activeWord is ufw's state before or after p, for the plan's view; ""
// when p does not change it.
func activeWord(p ufw.Plan, after bool) string {
	for _, s := range p.Steps {
		switch {
		case s.Kind == ufw.StepEnable && after, s.Kind == ufw.StepDisable && !after:
			return "active"
		case s.Kind == ufw.StepEnable, s.Kind == ufw.StepDisable:
			return "inactive"
		}
	}
	return ""
}

// PlanUFWEnable plans ufw enabled with default deny incoming, after allow
// rules for every listener sshd is configured for and for the applying
// session's port (ufw.md §2.7; p2drive's ufw-enable): one lockout-risk
// change, id prefix+"/firewall", or none when ufw is already so. The
// listeners must be established (ufw.ErrListenersUnknown), and so must a remote
// session's port (ErrSessionPort). Where sshd's read fails for want of sshd
// (sshd.NoSSHD), the host is established to have no SSH server at all
// (sshd.NoServer) and ss shows nothing on 22 (NothingOn22), there is no
// listener to let through: the plan lets the session's port alone through,
// a remote session's still required, and is lockout-risk as ever (the
// maintainer, 2026-11-07).
func PlanUFWEnable(ctx context.Context, sys host.System, in engine.Input, prefix string) ([]engine.Change, error) {
	st, err := readSSH(ctx, sys, in, "")
	if err != nil {
		if !sshd.NoSSHD(err) {
			return nil, err
		}
		if _, ok := sshd.NoServer(ctx, sys, in.Facts); !ok || !NothingOn22(ctx, sys) {
			return nil, err
		}
		st = sshState{in: in, noSSH: true}
	}
	return st.enableUFW(ctx, sys, prefix, UFWEnable)
}

// NothingOn22 reports whether ss shows no socket bound on 22, every line
// read: what a host with no SSH server (sshd.NoServer) must show besides,
// before Enable lets no SSH through, so that a server no package installed
// and no searched path holds, listening on SSH's port, is not cut off. ss
// missing or failing is false.
func NothingOn22(ctx context.Context, sys host.System) bool {
	bound, bad, err := reload.ReadBound(ctx, sys)
	return err == nil && len(bad) == 0 && !slices.ContainsFunc(bound, func(b reload.Bound) bool { return b.Port == 22 })
}

// enableUFW is ufw's enable alone, for mode UFWEnable (PlanUFWEnable, and
// an SSH change with nothing else left to do); none for UFWKeep.
func (st sshState) enableUFW(ctx context.Context, sys host.System, prefix string, mode UFW) ([]engine.Change, error) {
	if mode != UFWEnable {
		return nil, nil
	}
	if !st.before.Listeners.Known && !st.noSSH {
		return nil, fmt.Errorf("the listeners sshd configures now: %w", ufw.ErrListenersUnknown)
	}
	p, err := st.planUFW(ctx, sys, st.before.Listeners, UFWEnable, nil)
	if err != nil || p.Empty() {
		return nil, err
	}
	return claim([]engine.Change{firewallChange(prefix+"/firewall", *p)}), nil
}

// PlanUFWDisable plans "ufw disable": one plain change, id prefix+"/disable",
// which cannot lock anyone out (decision 6), or none on an inactive ufw. Its
// rules stay saved, and it never resets. A status read as active is all it
// needs, so a settings or rule file that cannot be read leaves that way off
// (ufw.PlanDisable); any other failure to read is ufw.ErrStateUnknown.
func PlanUFWDisable(ctx context.Context, sys host.System, in engine.Input, prefix string) ([]engine.Change, error) {
	st, err := ufw.Read(ctx, sys, in.Facts.Proxmox)
	if err != nil && !(st.Status.Known && st.Status.Active) {
		return nil, ufwStateErr(err)
	}
	p, err := ufw.PlanDisable(st, in.Facts.Proxmox)
	if err != nil || p.Empty() {
		return nil, err
	}
	c := firewallChange(prefix+"/disable", p)
	const title = "Disable ufw"
	c.Summary, c.Title, c.Risk, c.Resources = title, title, engine.RiskPlain, []string{ResourceUFW}
	return []engine.Change{c}, nil
}

// ufwStateErr is err, ufw.Read's, as ufw.ErrStateUnknown; Proxmox VE's as it is.
func ufwStateErr(err error) error {
	if errors.Is(err, ufw.ErrNotApplicable) || errors.Is(err, ufw.ErrProxmoxUnknown) {
		return err
	}
	return fmt.Errorf("%w: %v", ufw.ErrStateUnknown, err)
}

// UFWBootChange is the change that adds ufw's service to the boot services,
// a step of Enable (gallery-firewall.md §5, Made here): "rc-update add ufw
// default" on OpenRC, "systemctl enable ufw.service" elsewhere, plain, id
// prefix+"/boot". It goes before ufw's change: a rollback undoes in reverse and
// stops at the first undo that fails, and this undo, a boot link, must not stand
// in the way of "ufw disable", which is what reverses a lockout. It is checked by
// reading again once it ran (ErrVerify). "ufw enable" adds no runlevel, and
// ENABLED is not what starts ufw.service (docs/safety/ufw.md §3).
func UFWBootChange(prefix string, f detect.Facts) engine.Change {
	argv, undo := []string{"systemctl", "enable", "ufw.service"}, []string{"systemctl", "disable", "ufw.service"}
	if f.Init.Kind == detect.OpenRC {
		argv, undo = []string{"rc-update", "add", "ufw", "default"}, []string{"rc-update", "del", "ufw", "default"}
	}
	const title = "Start ufw at boot"
	c := engine.Change{ID: prefix + "/boot", Summary: title, Title: title, Target: "ufw", Commands: []string{host.FormatArgv(argv)},
		Risk: engine.RiskPlain, Stage: engine.StageFirewall, Resources: []string{ResourceUFW},
		Rollback: engine.Undo{Steps: runSteps(firewallWhy, [][]string{undo})}}
	c.Apply = func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
		if _, err := sys.Run(ctx, argv[0], argv[1:]...); err != nil {
			return engine.Ran{}, fmt.Errorf("ufw: %s: %w", host.FormatArgv(argv), err)
		}
		if got := ufw.ReadBoot(ctx, sys, f); got != detect.Yes {
			return engine.Ran{}, fmt.Errorf("ufw: %w: after %s, starts at boot is %s", ufw.ErrVerify, host.FormatArgv(argv), got)
		}
		return engine.Ran{Detail: "boot: verified true"}, nil
	}
	return c
}

// profileMode is what ufw.ProfileFile is written with: root's, as ufw's own
// files in /etc/ufw/applications.d are.
const profileMode fs.FileMode = 0o644

// PlanUFWApps plans the application profiles' Apply: ufw.ProfileFile written
// with a diff and the engine's backup where it is not as it should be, then
// ufw.Execute over ufw.PlanApps, its undo from Report.Undo: one plain change,
// id prefix+"/apply", or none when the file and the rules are as they should
// be. ufw must be active (ufw.ErrInactive).
func PlanUFWApps(ctx context.Context, sys host.System, in engine.Input, prefix string, profiles []ufw.Profile) ([]engine.Change, error) {
	st, err := ufw.Read(ctx, sys, in.Facts.Proxmox)
	if err != nil {
		return nil, ufwStateErr(err)
	}
	p, err := ufw.PlanApps(st, profiles, in.Facts.Proxmox)
	if err != nil {
		return nil, err
	}
	want := ufw.ProfileBytes(profiles)
	before, same, err := readProfileFile(ctx, sys, want)
	if err != nil {
		return nil, err
	}
	if same && p.Empty() {
		return nil, nil
	}
	const title = "Apply the application profiles"
	c := engine.Change{ID: prefix + "/apply", Summary: title, Title: title, Target: ufw.ProfileFile,
		Risk: engine.RiskPlain, Stage: engine.StageFirewall, Resources: []string{ResourceUFW}}
	for _, s := range p.Steps {
		c.Commands = append(c.Commands, host.FormatArgv(s.Argv))
	}
	if !same {
		after := string(want)
		c.Diff, c.Backups = engine.FileDiff(ufw.ProfileFile, before, &after), []string{ufw.ProfileFile}
	}
	c.Apply = func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
		if !same {
			if err := sys.MkdirAll(ctx, path.Dir(ufw.ProfileFile), 0o755); err != nil {
				return engine.Ran{}, err
			}
			if err := sys.WriteFileAtomic(ctx, ufw.ProfileFile, want, profileMode, 0, 0); err != nil {
				return engine.Ran{}, err
			}
		}
		rep, err := ufw.Execute(ctx, sys, p)
		u := engine.Undo{Steps: runSteps(firewallWhy, rep.Undo)}
		return engine.Ran{Rollback: &u, Detail: fmt.Sprintf("firewall: verified %v%s", rep.Verified, because(rep.Unverified))}, err
	}
	return []engine.Change{c}, nil
}

// PlanUFWInterfaces plans the trusted interfaces' Apply: ufw.Execute over
// ufw.PlanInterfaces, "ufw allow in on <interface>" for each with no rule
// yet, its undo from Report.Undo: one plain change, id prefix+"/interfaces",
// since letting more through can lock no one out; or none when every rule is
// there. ufw must be active (ufw.ErrInactive).
func PlanUFWInterfaces(ctx context.Context, sys host.System, in engine.Input, prefix string, ifaces []string) ([]engine.Change, error) {
	st, err := ufw.Read(ctx, sys, in.Facts.Proxmox)
	if err != nil {
		return nil, ufwStateErr(err)
	}
	p, err := ufw.PlanInterfaces(st, ifaces, in.Facts.Proxmox)
	if err != nil || p.Empty() {
		return nil, err
	}
	const title = "Apply the trusted interfaces"
	c := engine.Change{ID: prefix + "/interfaces", Summary: title, Title: title, Target: "ufw",
		Risk: engine.RiskPlain, Stage: engine.StageFirewall, Resources: []string{ResourceUFW}}
	for _, s := range p.Steps {
		c.Commands = append(c.Commands, host.FormatArgv(s.Argv))
	}
	c.Apply = func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
		rep, err := ufw.Execute(ctx, sys, p)
		u := engine.Undo{Steps: runSteps(firewallWhy, rep.Undo)}
		return engine.Ran{Rollback: &u, Detail: fmt.Sprintf("firewall: verified %v%s", rep.Verified, because(rep.Unverified))}, err
	}
	return []engine.Change{c}, nil
}

// readProfileFile reads ufw.ProfileFile: its content, nil where it is not
// there, and whether it holds want with its mode and root as its owner.
func readProfileFile(ctx context.Context, sys host.System, want []byte) (before *string, same bool, err error) {
	data, err := sys.ReadFile(ctx, ufw.ProfileFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	fi, err := sys.Stat(ctx, ufw.ProfileFile)
	if err != nil {
		return nil, false, err
	}
	was := string(data)
	uid, gid, known := host.Owner(fi)
	return &was, was == string(want) && fi.Mode().Perm() == profileMode && known && uid == 0 && gid == 0, nil
}
