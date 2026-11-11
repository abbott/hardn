// Package steps is the engine's change kinds over the Phase 2 safety
// primitives (refactor plan P3, next/p3-steps): what a control returns
// from Plan for an SSH change (00-hardn.conf edited line by line, the
// firewall before it, the reload and the check of sshd's effective values
// after it), the migration of hardn's legacy SSH files, ufw's enable, the
// clean-up after a confirmed port change, and the changes of the units
// that keep a rollback of their own (sudoers, DNS, Alpine's updates and
// Proxmox VE's root keys), journalled as their registered plan steps.
//
// Every refusal is made by a planner, before anything changes, so that the
// engine shows it in the dry run and before the revert timer: sshd.Validate
// over a copy, reload.PlanFor and reload.CheckFree (a port another socket
// holds), reload.CheckInEffect with the applying session's port for every
// reload that makes sshd bind what its files configure (a listener change,
// and any reload of ssh.service or OpenRC's sshd) and for a port that is
// configured already, migrate.Plan.CheckRollback (the D9 host, P-39 (a),
// with a report), a port change that reaches no listener, a file that says
// what is asked while sshd -T does not show it, Proxmox VE's rules, and
// sshd -T failing without /run/sshd (a report). Each change names what it
// reads and changes (engine.Change.Resources), so that the engine refuses
// two controls that would plan the same thing from the same host. Each
// change is data (engine.Change): the engine backs up its files, journals
// its undo before it runs and what ran after, and rolls back in reverse
// from the journal.
//
// The package reaches the host only through host.System (depguard
// steps-through-host) and renders nothing. docs/engine/steps.md describes
// the guarantees, the facts they rely on and what is written but not yet
// run on a Linux host.
package steps

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
)

// The stable codes of the refusals that come with a report (Refusal):
// p3-output's registry holds them, and they do not change once released.
// Each has its sentinel (ErrListenConfNotInEffect, ErrNoRunDir,
// ErrNoHostKeys), which a Refusal matches with errors.Is, so that the
// registry codes every error by errors.Is alone.
const (
	// CodeListenConfNotInEffect: v0.3.x's listen.conf on an ssh.socket host
	// where it never took effect (D9), whose migration could not be rolled
	// back (P-39 (a), decided 2026-10-19: refused, with this report).
	CodeListenConfNotInEffect = "SSH_LISTEN_CONF_NOT_IN_EFFECT"
	// CodeNoRunDir: sshd -T cannot run because /run/sshd is missing (on
	// Ubuntu, before the first SSH connection since boot), so sshd's
	// listeners are not established (sshd-validate.md §6's code).
	CodeNoRunDir = "SSHD_ENV_NO_RUNDIR"
	// CodeNoHostKeys: sshd -T cannot run because sshd loads no host key
	// (none made yet, or the configuration's HostKey files missing), so
	// sshd's settings are not read (sshd-validate.md §6's code).
	CodeNoHostKeys = "SSHD_ENV_NO_HOSTKEYS"
	// CodeSELinuxLabels: where SELinux enforces, a file sshd rereads at the
	// reload is labelled otherwise than its policy (selinux.ErrMislabeled).
	CodeSELinuxLabels = "SELINUX_MISLABELED"
)

// The resources the planners' changes name (engine.Change.Resources): two
// controls whose changes name the same one cannot share a plan
// (engine.ErrConflict), so the SSH settings and ufw's enable of one plan go
// through one PlanSSH. The unit changes name their unit (plans.UnitSudoers
// and the others).
const (
	// ResourceSSHD is sshd's configuration (00-hardn.conf and every file
	// sshd reads) and its listeners.
	ResourceSSHD = "sshd"
	// ResourceUFW is ufw's rules, policies and state.
	ResourceUFW = "ufw"
	// ResourceFirewalld: firewalld's unit and configuration (the Firewall
	// item on Rocky Linux and AlmaLinux).
	ResourceFirewalld = "firewalld"
)

// claim names, on each of a planner's changes, the resources every SSH
// planner reads and changes: sshd's files and listeners, and ufw, whose
// state decides the firewall part (an inactive ufw gets no rule).
func claim(cs []engine.Change) []engine.Change {
	for i := range cs {
		cs[i].Resources = []string{ResourceSSHD, ResourceUFW}
	}
	return cs
}

// Errors, testable with errors.Is.
var (
	// ErrListenConfNotInEffect is the D9 host's refusal, with its report
	// (Refusal, CodeListenConfNotInEffect). It wraps
	// reload.ErrRestoredListeners, migrate.Plan.CheckRollback's own, of
	// which it is the more specific case.
	ErrListenConfNotInEffect = errors.New("v0.3.x's listen.conf never took effect")
	// ErrNoRunDir is the refusal of sshd -T failing for a missing
	// privilege separation directory, with its report (Refusal,
	// CodeNoRunDir). It wraps sshd's *EnvError (EnvPrivsepDir), to which
	// sshd-validate.md §6 gives the same code.
	ErrNoRunDir = errors.New("sshd -T cannot run without its privilege separation directory")
	// ErrNoHostKeys is the refusal of sshd -T failing for want of a host
	// key, with its report (Refusal, CodeNoHostKeys). It wraps sshd's
	// *EnvError (EnvHostKeys), to which sshd-validate.md §6 gives the same
	// code.
	ErrNoHostKeys = errors.New("sshd -T cannot run without a host key")
	// ErrNoWriteStrategy: sshd_config's first directive is not exactly the
	// stock Include (P-38), or there is none: hardn's block at the top of
	// sshd_config is not written yet.
	ErrNoWriteStrategy = errors.New("no write strategy for this sshd_config")
	// ErrMigrateFirst: hardn's legacy SSH files are on the host; their
	// migration (PlanMigration) is a change of its own, made first.
	ErrMigrateFirst = errors.New("legacy hardn SSH files: migrate them first")
	// ErrELFirewall: the Firewall item's ufw changes are refused on Rocky
	// Linux, AlmaLinux and RHEL, which use firewalld (firewall.ErrEL;
	// el-changes.md). A port change still lets its new port through an
	// installed, active ufw there (EPEL's), as elsewhere.
	ErrELFirewall = errors.New("hardn doesn't drive ufw on Rocky Linux, AlmaLinux or RHEL, which use firewalld")
	// ErrNoNewListener: a port change whose Port line reaches no listener
	// (a ListenAddress with its own port ignores every Port line).
	ErrNoNewListener = errors.New("the port change adds no listener")
	// ErrNotInEffect: sshd -T, read again after the reload, does not show
	// what the change set.
	ErrNotInEffect = errors.New("sshd does not show the change")
	// ErrChanged: a file the change writes is not as the plan read it: its
	// backup, taken just before the write, shows another content.
	ErrChanged = errors.New("changed since the plan")
	// ErrProxmoxRoot: on Proxmox VE, the candidate configuration does not
	// show root logging in key-only, by key from the other nodes, on port
	// 22 (pve.CheckSSH, pve.md §6): a No locks the cluster out, and an
	// Unknown is not a pass. Refused at plan time.
	ErrProxmoxRoot = errors.New("the cluster's root SSH login by key would not be kept")
)

// Refusal is a refusal at plan time with a report: a stable code, why the
// host is refused, and how it becomes one hardn can change. Err is the
// primitive's own refusal.
type Refusal struct {
	Code string // CodeListenConfNotInEffect, CodeNoRunDir, CodeNoHostKeys
	Why  string
	Fix  string
	Err  error
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("%s. To go on: %s (%s): %v", r.Why, r.Fix, r.Code, r.Err)
}

// Unwrap returns the primitive's refusal.
func (r *Refusal) Unwrap() error { return r.Err }

// refusalErrs are each code's sentinel.
var refusalErrs = map[string]error{CodeListenConfNotInEffect: ErrListenConfNotInEffect, CodeNoRunDir: ErrNoRunDir, CodeNoHostKeys: ErrNoHostKeys}

// Is makes errors.Is(r, target) true for the sentinel of r's Code, so that
// the more specific code is the one a registry matches first.
func (r *Refusal) Is(target error) bool {
	e, ok := refusalErrs[r.Code]
	return ok && target == e
}

// Failure is a change's failure in the user's words, for the report:
// what happened on the host, in a few words, and the next step. Err is the
// unit's own error, whose code the report gives on the step's line, and
// which the log and the JSON document's message keep (next/s2-control).
type Failure struct {
	Text string // "Reloading systemd-resolved had no effect"
	Next string // a sentence: "Run resolvectl dns to see the servers in use."
	Err  error
}

func (f *Failure) Error() string {
	if f.Err == nil {
		return f.Text
	}
	return f.Text + ": " + f.Err.Error()
}

// Unwrap returns the unit's error.
func (f *Failure) Unwrap() error { return f.Err }

// sshState is what every SSH change reads before anything changes.
type sshState struct {
	in     engine.Input
	binary string         // OpenRC's sshd flavour; "" elsewhere
	before sshd.Effective // sshd -T over the installed files
	found  migrate.Found
	pve    bool // Proxmox VE's rules apply
	// noSSH: no SSH server, established after sshd's read failed for want
	// of sshd (sshd.NoSSHD, sshd.NoServer): Enable lets no SSH through
	// (ufw.Input.NoSSH), and nothing else is read.
	noSSH bool
}

// readSSH reads sshd's installed configuration and hardn's legacy files.
// sshd -T failing for a missing /run/sshd is a Refusal (CodeNoRunDir), for
// want of a host key another (CodeNoHostKeys).
func readSSH(ctx context.Context, sys host.System, in engine.Input, configFile string) (sshState, error) {
	st := sshState{in: in}
	var err error
	if in.Facts.Init.Kind == detect.OpenRC {
		if st.binary, err = reload.OpenRCFlavour(ctx, sys); err != nil {
			return st, err
		}
	}
	if in.Facts.SSH.SSHDUnits {
		if err := reload.CheckSysconfig(ctx, sys); err != nil {
			return st, err
		}
	}
	if st.before, _, err = sshd.ReadEffective(ctx, sys, sshd.EffectiveRequest{SSH: in.Facts.SSH, Binary: st.binary}); err != nil {
		err = fmt.Errorf("sshd's effective configuration: %w", err)
		if hostKeys(err) != nil {
			return st, NoHostKeys(in.Facts, ServerWorld(ctx, sys, in.Facts, err), err)
		}
		return st, noRunDir(in.Facts.SSH, err)
	}
	if st.found, err = migrate.Find(ctx, sys, migrate.FindRequest{ConfigFile: configFile}); err != nil {
		return st, err
	}
	if st.pve, err = pve.Applies(in.Facts.Proxmox); err != nil {
		return st, err
	}
	return st, nil
}

// noRunDir reports sshd failing for a missing privilege separation
// directory as a Refusal: sshd -T needs it. On Debian and Ubuntu it is
// /run/sshd, which only ssh.service creates (RuntimeDirectory=sshd), and
// it is gone while the service is stopped (sshd-validate.md §3): so the
// fix is to start ssh.service from the console, or, where ssh.socket
// listens (a socket-activated Ubuntu before its first SSH connection since
// boot [I]), to open an SSH connection, which starts it. Elsewhere (OpenRC)
// sshd names the directory it lacks, which must be root's and writable by
// root alone (sshd-validate.md §3). Without sshd -T, the listeners a
// change must hold are not established, so the change is refused rather
// than planned from a guess.
func noRunDir(ssh detect.SSH, err error) error {
	var ee *sshd.EnvError
	if !errors.As(err, &ee) || ee.Kind != sshd.EnvPrivsepDir {
		return err
	}
	dir := ee.Path
	if dir == "" {
		dir = sshd.PrivsepDir
	}
	r := &Refusal{Code: CodeNoRunDir, Err: err}
	switch {
	case sshd.SocketMayListen(ssh):
		r.Why = "sshd -T cannot run: " + dir + " is missing, which only ssh.service creates when it starts, and on this host ssh.socket starts ssh.service at the first SSH connection since boot; so the listeners sshd configures are not established"
		r.Fix = "open an SSH connection to this host (ssh.socket then starts ssh.service, which creates " + dir + "), then plan again from it"
	case ssh.Mode == detect.SSHOpenRC:
		r.Why = "sshd -T cannot run: its privilege separation directory " + dir + " is missing, or another user can write to it; so the listeners sshd configures are not established"
		r.Fix = "from the console, make " + dir + " a directory owned by root that no other user can write to, then plan again"
	default:
		unit := ssh.ServiceUnit()
		r.Why = "sshd -T cannot run: " + dir + " is missing, which " + unit + " creates when it starts and is gone while it is stopped: sshd is not running, so the listeners it configures are not established"
		r.Fix = "start " + unit + " from the console (`systemctl start " + unit + "`), check that it is active, then plan again"
	}
	return r
}

// The words of the host-key refusal (NoHostKeys), each a Refusal's Why
// and Fix as screenkit.RefusalWords joins them: proposed, for approval
// (check-ssh.md §5).
const (
	whyNoHostKeys   = "sshd has no host keys, so hardn can't read its settings"
	whyHostKeyFiles = "sshd's HostKey files aren't there, so hardn can't read its settings"
	fixHostKeyFiles = "fix the HostKey lines, then try again"
	fixKeygen       = "run ssh-keygen -A, then try again"
	fixOffOpenRC    = "run rc-service sshd start"
	fixOffSystemd   = "run ssh-keygen -A and systemctl start %s"
	fixRemoveApt    = "apt remove openssh-server"
	fixRemoveDnf    = "dnf remove openssh-server"
	fixRemoveApk    = "apk del %s"
	fixThenTryAgain = ", then try again"
	fixOrRemove     = ", or "
)

// hostKeys is sshd's *EnvError in err where sshd loaded no host key; nil
// otherwise.
func hostKeys(err error) *sshd.EnvError {
	var ee *sshd.EnvError
	if errors.As(err, &ee) && ee.Kind == sshd.EnvHostKeys {
		return ee
	}
	return nil
}

// NoHostKeys reports sshd -T failing because sshd loaded no host key as a
// Refusal (CodeNoHostKeys): without sshd -T, sshd's settings are not read,
// so nothing is planned. err is returned as it is otherwise, nil too. hardn
// runs as root, so the keys are missing, not unreadable. The fix fits the
// host:
//   - the configuration's own HostKey lines (HostKeysSet): fix them;
//   - sshd off (detect.SSH.Off): keys alone leave sshd's mode unknown, so
//     no change plans (reload.ErrModeUnknown), and on Debian and Ubuntu
//     /run/sshd missing; so sshd is started too. On OpenRC, rc-service
//     sshd start, whose init script makes the missing keys (ssh-keygen -A
//     in checkconfig, before sshd -t: the Alpine 3.23 VM's
//     /etc/init.d/sshd, 2026-11-11); on systemd, ssh-keygen -A and
//     systemctl start, as neither Debian's ssh.service (ExecStartPre sshd
//     -t; sshd-keygen.service only on the first boot) nor, where cloud-init
//     is active, EL's sshd-keygen@.service (its drop-in's condition) makes
//     them (the Debian 13, Ubuntu 26.04 and Rocky 10.2 VMs' units,
//     2026-11-11). Or
//     removing the server: apt remove or dnf remove openssh-server; on
//     Alpine apk del with remove, World's entries that keep it installed
//     (alpine.ParseServerWorld), none offered without them; on Proxmox VE,
//     whose cluster logs in over SSH, none;
//   - else (sshd running, or set to start, without its keys): ssh-keygen -A.
func NoHostKeys(f detect.Facts, remove []string, err error) error {
	ee := hostKeys(err)
	if ee == nil {
		return err
	}
	r := &Refusal{Code: CodeNoHostKeys, Why: whyNoHostKeys, Fix: fixKeygen, Err: err}
	switch {
	case ee.HostKeysSet:
		r.Why, r.Fix = whyHostKeyFiles, fixHostKeyFiles
	case !f.SSH.Off():
	case f.Init.Kind == detect.OpenRC:
		r.Fix = fixOffOpenRC
		if f.Platform.Distro == detect.DistroAlpine && len(remove) > 0 {
			r.Fix += fixOrRemove + fmt.Sprintf(fixRemoveApk, strings.Join(remove, " "))
		}
		r.Fix += fixThenTryAgain
	default:
		r.Fix = fmt.Sprintf(fixOffSystemd, strings.TrimSuffix(f.SSH.ServiceUnit(), ".service"))
		switch d := f.Platform.Distro; {
		case d == detect.DistroDebian, d == detect.DistroUbuntu:
			r.Fix += fixOrRemove + fixRemoveApt
		case d.EL():
			r.Fix += fixOrRemove + fixRemoveDnf
		}
		r.Fix += fixThenTryAgain
	}
	return r
}

// ServerWorld is what NoHostKeys names for apk del: on Alpine, where err
// is sshd off without its host keys, World's entries that keep an OpenSSH
// server installed (alpine.ServerWorld); nil elsewhere, and where World
// is not read. It changes nothing.
func ServerWorld(ctx context.Context, sys host.System, f detect.Facts, err error) []string {
	if ee := hostKeys(err); ee == nil || ee.HostKeysSet || !f.SSH.Off() || f.Platform.Distro != detect.DistroAlpine {
		return nil
	}
	remove, _ := alpine.ServerWorld(ctx, sys)
	return remove
}

// checkInclude refuses an sshd_config hardn has no write strategy for.
func checkInclude(ctx context.Context, sys host.System) error {
	inc, err := sshd.DetectInclude(ctx, sys)
	switch {
	case err != nil:
		return err
	case inc.Mode == sshd.IncludeOther:
		return fmt.Errorf("%s line %d %q is not exactly %q: %w (brief 2.2 covers the stock Include and none; proposal P-38)", sshd.MainConfig, inc.Line, inc.Text, sshd.IncludeLine, ErrNoWriteStrategy)
	case inc.Mode != sshd.IncludeFirst:
		return fmt.Errorf("%s has no Include (%v): hardn's block at its top is not written yet: %w", sshd.MainConfig, inc.Mode, ErrNoWriteStrategy)
	}
	return nil
}

// noArtifacts refuses an edit of 00-hardn.conf while legacy files are on
// the host: their migration comes first, as a change of its own.
func (st sshState) noArtifacts() error {
	if len(st.found.Artifacts) == 0 {
		return nil
	}
	var ps []string
	for _, a := range st.found.Artifacts {
		ps = append(ps, a.Path)
	}
	return fmt.Errorf("%s: %w (docs/safety/sshd-migrate.md)", strings.Join(slices.Compact(ps), ", "), ErrMigrateFirst)
}

// socketDropIns reads ssh.socket's drop-ins where the socket may listen.
func (st sshState) socketDropIns(ctx context.Context, sys host.System) (reload.SocketDropIns, error) {
	if !sshd.SocketMayListen(st.in.Facts.SSH) {
		return reload.SocketDropIns{}, nil
	}
	return reload.ReadSocketDropIns(ctx, sys)
}

// inEffect refuses a listener change where what sshd (or ssh.socket) is
// configured for is not what is bound, or does not cover the port the
// applying session is connected to (reload.CheckInEffect): the reload would
// drop what is bound and not configured, and the rollback, which restores
// configuration, would not bring it back. A remote session whose connection
// is not established fails it, closed; a console session has no port.
func (st sshState) inEffect(ctx context.Context, sys host.System, configured []sshd.Listener) error {
	var server netip.AddrPort
	if s := st.in.Session; s.Remote {
		c, err := revert.ParseConn(s.Conn)
		if err != nil {
			return fmt.Errorf("the port this session is connected to is not established (%v), so the change cannot be shown to keep it: %w", err, reload.ErrNotInEffect)
		}
		server = c.Server
	}
	return reload.CheckInEffect(ctx, sys, st.in.Facts.SSH.Mode, configured, server)
}

// sshReload is the ssh reload a rollback runs once the files are back.
func sshReload(kws []string, after []sshd.Listener, dropIns []string) revert.Reload {
	return revert.Reload{Service: revert.ReloadSSH, Keywords: slices.Clone(kws), After: revert.ListenersOf(after), IfUp: true,
		SocketDropIns: slices.Clone(dropIns)}
}

// readOptional reads a root-controlled file without following a link;
// absent is not an error.
func readOptional(ctx context.Context, sys host.System, name string) (data []byte, absent bool, err error) {
	data, _, err = sys.ReadFileNoFollow(ctx, name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, true, nil
	}
	return data, false, err
}

// sameAsRead checks that the engine's backup of a file, taken just before
// the write, shows it as the plan read it: absent, or the content whose
// SHA-256 is sum. A file changed since the plan is not overwritten.
func sameAsRead(e backup.Entry, ok bool, name string, absent bool, sum string) error {
	switch {
	case !ok:
		return fmt.Errorf("%s: no backup taken", name)
	case absent && e.Kind == backup.KindAbsent, !absent && e.Kind == backup.KindFile && e.File != nil && e.File.SHA256 == sum:
		return nil
	}
	return fmt.Errorf("%s (%s now): %w", name, e.Kind, ErrChanged)
}

// runSteps journals commands as revert's run steps, in the order given.
func runSteps(why string, argvs [][]string) []revert.Step {
	var out []revert.Step
	for _, a := range argvs {
		out = append(out, revert.Step{Op: revert.OpRun, Argv: slices.Clone(a), Why: why})
	}
	return out
}

// portsOf are ls's ports, each once, ascending.
func portsOf(ls []sshd.Listener) []uint16 {
	var ps []uint16
	for _, l := range ls {
		ps = append(ps, l.Port)
	}
	slices.Sort(ps)
	return slices.Compact(ps)
}

// because is " (<why>)" when why is set.
func because(why string) string {
	if why == "" {
		return ""
	}
	return " (" + why + ")"
}

// formatArgvs is each command as the plan shows it.
func formatArgvs(argvs [][]string) []string {
	var out []string
	for _, a := range argvs {
		out = append(out, host.FormatArgv(a))
	}
	return out
}
