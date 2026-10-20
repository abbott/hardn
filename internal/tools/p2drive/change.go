package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/revert/plans"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// runRoot holds each change's run directory, root's from / down
// (docs/safety/sshd-validate.md §6): /run/hardn/<id>, where sshd.Validate
// and sudoers.Apply build their copies.
const runRoot = revert.DefaultRunDir

// journalTimeout bounds a journal write made after Ctrl-C (recordRan), as
// revert.Arm bounds its own clean-up.
const journalTimeout = 30 * time.Second

// lockoutOpts are the flags of a lockout-risk change.
type lockoutOpts struct {
	timeout, hardn, backupPath string
	planOnly                   bool
}

func lockoutFlags(fs *flag.FlagSet) *lockoutOpts {
	o := &lockoutOpts{}
	fs.StringVar(&o.timeout, "timeout", "", "the confirm window, seconds or a duration such as 3m (default 180s, at least 60s)")
	fs.StringVar(&o.hardn, "hardn", "/usr/local/bin/hardn", "the hardn binary the revert timer runs: absolute, root's, not a symlink")
	fs.StringVar(&o.backupPath, "backup-path", backup.DefaultPath, "where each change's backups go, <path>/<id>/")
	fs.BoolVar(&o.planOnly, "plan-only", false, "stop after the plan; change nothing")
	return o
}

// window is the confirm window --timeout asks for (revert.ResolveTimeout:
// 180s by default, at least 60s); a bad value is a usage error.
func (o *lockoutOpts) window() (time.Duration, error) {
	var flagTimeout *time.Duration
	if o.timeout != "" {
		t, err := revert.ParseTimeout(o.timeout)
		if err != nil {
			return 0, fmt.Errorf("%w: %w", err, errUsage)
		}
		flagTimeout = &t
	}
	timeout, err := revert.ResolveTimeout(flagTimeout, nil)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", err, errUsage)
	}
	return timeout, nil
}

// ufwMode is what a change does with ufw (--ufw).
type ufwMode string

const (
	// ufwKeep lets the ports the change adds through an active ufw before
	// sshd moves (brief 2.3 step 2), and saves no rule in an inactive one
	// (ufw.Desired.NoSaveWhileInactive; P-33 is open on that).
	ufwKeep ufwMode = "keep"
	// ufwEnable also sets the default incoming policy to deny and enables
	// ufw, after its allow rules (ufw.md §2.7).
	ufwEnable ufwMode = "enable"
)

// There is no mode that leaves an active ufw unread: a port moved behind a
// firewall that does not let it through is a lockout the timer would have
// to undo. Where ufw is not installed, or on Proxmox VE, keep has nothing
// to do.

func ufwFlag(fs *flag.FlagSet) *string {
	return fs.String("ufw", string(ufwKeep), "keep (allow the new port in an active ufw) or enable (also default deny incoming and enable ufw)")
}

func parseUFW(s string) (ufwMode, error) {
	switch m := ufwMode(s); m {
	case ufwKeep, ufwEnable:
		return m, nil
	}
	return "", fmt.Errorf("--ufw %q: keep or enable: %w", s, errUsage)
}

// base is what every lockout-risk change reads first, before anything
// changes: the change's id (also its backup run's and its timer's), its run
// directory, detect's facts, the session applying it, the sshd binary, and
// sshd's effective configuration as installed.
type base struct {
	id, runDir, binary string
	facts              detect.Facts
	session            revert.Session
	before             sshd.Effective
}

// begin reads base and creates the run directory; the caller removes it
// with end.
func (d *driver) begin(ctx context.Context) (base, error) {
	var b base
	f, err := d.detect(ctx, d.sys)
	if err != nil {
		return b, err
	}
	b.facts = f
	d.say("host: %s %s (%v), init %v, sshd by %v, Proxmox VE %v", f.Platform.Distro, f.Platform.Version, f.Platform.Support, f.Init.Kind, f.SSH.Mode, f.Proxmox.Installed)
	b.id = revert.NewID(d.now())
	b.runDir = path.Join(runRoot, b.id)
	if err := d.sys.MkdirAll(ctx, b.runDir, 0o700); err != nil {
		return b, err
	}
	if b.session, err = d.session(ctx, d.sys); err != nil {
		d.say("session: %v (only a new SSH connection can confirm)", err)
	}
	if f.Init.Kind == detect.OpenRC {
		if b.binary, err = reload.OpenRCFlavour(ctx, d.sys); err != nil {
			return b, err
		}
	}
	before, findings, err := sshd.ReadEffective(ctx, d.sys, sshd.EffectiveRequest{SSH: f.SSH, Binary: b.binary})
	if err != nil {
		return b, fmt.Errorf("sshd's effective configuration: %w", err)
	}
	b.before = before
	d.sayEffective("before", before, findings)
	return b, nil
}

// end removes the run directory, which the primitives leave empty.
func (d *driver) end(ctx context.Context, b base) {
	if b.runDir == "" {
		return
	}
	if err := d.sys.Remove(context.WithoutCancel(ctx), b.runDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		d.say("run directory %s not removed: %v", b.runDir, err)
	}
}

// lockout is one lockout-risk change (SSH or the firewall, brief 2.4), as
// planned: everything Arm and the steps need, known before anything
// changes.
type lockout struct {
	base
	summary string
	ufw     *ufw.Plan // nil: ufw is not touched
	files   *files    // nil: no sshd file changes
	// adds are the listeners the change adds (reload.Plan.Adds): a
	// confirmation must come over one of them (revert.Journal.ConfirmVia).
	adds []sshd.Listener
	// boot is the boot unit installed before the timer is armed.
	boot bootStep
}

// bootStep is the boot unit a lockout-risk change installs before its timer
// is armed (P-34 (c), docs/safety/revert-boot.md): after a reboot inside the
// window it runs `hardn rollback --boot`, which re-arms the timer or rolls
// the change back. Planned, and what the host has of it read, before
// anything changes, and shown in the plan.
type bootStep struct {
	unit  revert.BootUnit
	state revert.BootUnitState
	hardn string
}

// planBoot plans the boot unit that runs hardn on init system in, and reads
// what the host has of it. A change journalled already refuses the plan
// (*revert.PendingError, as Arm and EnsureBootUnit would), before the unit
// is shown or touched: it is that change's boot-time recovery.
func (d *driver) planBoot(ctx context.Context, in detect.Init, hardn string) (bootStep, error) {
	js, err := revert.ReadState(ctx, d.sys, d.revertOptions(hardn))
	switch {
	case err != nil:
		return bootStep{}, err
	case js.Busy():
		return bootStep{}, &revert.PendingError{State: js}
	}
	u, err := revert.PlanBootUnit(in, hardn)
	if err != nil {
		return bootStep{}, err
	}
	st, err := revert.ReadBootUnit(ctx, d.sys, u)
	return bootStep{unit: u, state: st, hardn: hardn}, err
}

// sayBoot prints the boot unit's step of the plan.
func (d *driver) sayBoot(bs bootStep) {
	u := bs.unit
	if bs.state.Installed() {
		d.say("  boot unit: %s, installed and enabled already: after a reboot inside the window it runs `%s rollback --boot`", u.Path, bs.hardn)
		return
	}
	var do []string
	if !bs.state.Current {
		do = append(do, fmt.Sprintf("write %s (%04o, root's; a copy checked first with %s)", u.Path, uint32(u.Mode.Perm()), host.FormatArgv(u.Check)))
	}
	if !bs.state.Linked {
		do = append(do, host.FormatArgv(u.Enable))
	}
	d.say("  boot unit, before the timer: %s: after a reboot inside the window it runs `%s rollback --boot`, which re-arms the timer or rolls the change back", strings.Join(do, ", then "), bs.hardn)
}

// ensureBoot installs the boot unit, before the timer is armed: a change
// whose timer a reboot would lose, with nothing to re-arm it, is not
// applied. revert.EnsureBootUnit logs what it does.
func (d *driver) ensureBoot(ctx context.Context, bs bootStep, runDir string, run *backup.Run, opts revert.Options) error {
	if _, err := revert.EnsureBootUnit(ctx, d.sys, bs.unit, runDir, run, opts); err != nil {
		return fmt.Errorf("not applied: %w", err)
	}
	return nil
}

// files is a lockout change's sshd part.
type files struct {
	paths    []string // backed up, RollbackRequired, before any write
	keywords []string // the reload's (reload.Change.Keywords)
	// restored is what the restored files configure, the rollback's
	// reload.Change.After: on a socket host, the socket's listeners.
	restored []sshd.Listener
	// socketDropIns are the ssh.socket drop-ins the change removes, which
	// the rollback's restore puts back (v0.3.x's listen.conf): its reload
	// expects them (revert.Reload.SocketDropIns).
	socketDropIns []string
	reload        reload.Plan // planned before anything changes, to show
	// write makes the change and returns the reload to run.
	write func(ctx context.Context, run *backup.Run) (reload.Plan, error)
	// want checks, once reloaded, that sshd -T shows what the change set.
	want func(ctx context.Context, after sshd.Effective) error
}

// planUFW plans ufw's part: listeners, the ones that accept connections
// now (sshd.ReadEffective's, or a migration's Plan.Listening), and the
// ports the change adds let through before sshd moves (brief 2.3 step 2),
// with the applying session's own port kept. nil when there is nothing for
// ufw to do: keep with no port added, ufw not installed, or Proxmox VE.
func (d *driver) planUFW(ctx context.Context, b base, listeners sshd.Listeners, mode ufwMode, add []uint16) (*ufw.Plan, error) {
	if mode == ufwKeep && len(add) == 0 {
		return nil, nil
	}
	st, err := ufw.Read(ctx, d.sys, b.facts.Proxmox)
	switch {
	case errors.Is(err, ufw.ErrNotApplicable) && mode == ufwKeep:
		d.say("ufw: not applicable on Proxmox VE (pve-firewall; `facts` reports it)")
		return nil, nil
	case err != nil:
		return nil, err
	case !st.Installed && mode == ufwKeep:
		d.say("ufw: not installed; no rule to add")
		return nil, nil
	case !st.Installed:
		return nil, fmt.Errorf("ufw: %w", ufw.ErrNotInstalled)
	}
	in := ufw.Input{Proxmox: b.facts.Proxmox, Listeners: listeners, Add: add, State: st}
	if c, err := revert.ParseConn(b.session.Conn); err == nil {
		in.Keep = []uint16{c.Server.Port()}
	}
	if mode == ufwEnable {
		in.Desired = ufw.Desired{Enable: true, Incoming: ufw.PolicyDeny}
	} else {
		in.Desired = ufw.Desired{NoSaveWhileInactive: true}
	}
	p, err := ufw.PlanFor(in)
	if err != nil {
		return nil, err
	}
	for _, f := range p.Findings {
		d.say("ufw finding: %v", f)
	}
	return &p, nil
}

// showPlan prints what the change will do.
func (d *driver) showPlan(lc lockout) {
	d.say("plan %s: %s", lc.id, lc.summary)
	if lc.ufw != nil {
		for _, s := range lc.ufw.Steps {
			d.say("  firewall: %s (%s)", host.FormatArgv(s.Argv), s.Why)
		}
		if lc.ufw.Empty() {
			d.say("  firewall: nothing to change")
		}
	}
	if fc := lc.files; fc != nil {
		d.say("  files, backed up first: %s", strings.Join(fc.paths, ", "))
		for _, s := range fc.reload.Steps {
			d.say("  reload: %s %s (%s)", s.Kind, host.FormatArgv(s.Argv), s.Why)
		}
		if len(fc.reload.Hold) > 0 {
			d.say("  held until confirmed: %v; added: %v", fc.reload.Hold, fc.reload.Adds)
		}
		for _, f := range fc.reload.DropIns {
			d.say("  ssh.socket drop-in, reported: %s %v", f.Path, f.Listen)
		}
	}
	if lc.boot.unit.Path != "" {
		d.sayBoot(lc.boot)
	}
}

// apply carries lc out under confirm-or-revert, in docs/safety/revert.md
// §7's order, under the run lock (revert.LockRun): the boot unit
// (revert-boot.md), Arm (the timer starts before the first step), then
// each step
// journalled before it takes effect (Record), the firewall first (brief 2.3
// step 2), the sshd files after their backups, the reload and the check of
// the listener and of the values; MarkApplied last. A step that fails rolls
// the change back at once, while this session is open
// (revert.ReasonFailed).
func (d *driver) apply(ctx context.Context, lc lockout, o *lockoutOpts) error {
	timeout, err := o.window()
	if err != nil {
		return err
	}
	opts := d.revertOptions(o.hardn)
	d.sayHardn(ctx, o.hardn)
	// The run lock, as the engine holds it for an apply (engine-boot.md
	// §2.4, §2.10): `hardn rollback --boot`, run by hand between the boot
	// unit's install and Arm, would find nothing journalled and remove the
	// unit.
	unlock, err := revert.LockRun(ctx, d.sys, opts, "p2drive, change "+lc.id)
	if err != nil {
		return fmt.Errorf("not applied: %w", err)
	}
	defer func() { _ = unlock() }()
	run, err := backup.Open(d.sys, backup.Settings{Path: o.backupPath, Enabled: true}, lc.id)
	if err != nil {
		return fmt.Errorf("not applied: %w", err)
	}
	if err := d.ensureBoot(ctx, lc.boot, lc.runDir, run, opts); err != nil {
		return err
	}
	j := revert.Journal{ID: lc.id, TimeoutSeconds: int(timeout / time.Second), Risk: revert.RiskLockout,
		Summary: lc.summary, Session: lc.session, ConfirmVia: revert.ListenersOf(lc.adds)}
	if j, err = revert.Arm(ctx, d.sys, j, lc.facts.Init, opts); err != nil {
		return fmt.Errorf("not applied: %w", err)
	}
	// record journals what the next step will do, before it runs: with
	// ctx done, the step does not run either. recordRan journals what a
	// step that ran did (ufw's Report.Undo, the reload's IfUp cleared),
	// also after Ctrl-C, so it runs without the cancellation, as the
	// rollback does (revert.md §7 item 3: "also when Execute failed after
	// it").
	record := func() error { return revert.Record(ctx, d.sys, j, opts) }
	recordRan := func() error {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), journalTimeout)
		defer cancel()
		return revert.Record(rctx, d.sys, j, opts)
	}
	fail := func(step string, err error) error { return d.rollBack(ctx, lc.id, opts, step, err) }
	if p := lc.ufw; p != nil && !p.Empty() {
		j.Undo = undoSteps(p.PreUndo())
		if err := record(); err != nil {
			return fail("journal the firewall's undo", err)
		}
		rep, err := ufw.Execute(ctx, d.sys, *p)
		j.Undo = undoSteps(rep.Undo)
		if rerr := recordRan(); rerr != nil {
			return fail("journal the firewall's undo", errors.Join(err, rerr))
		}
		if err != nil {
			return fail("firewall", err)
		}
		d.say("firewall: verified %v%s", rep.Verified, because(rep.Unverified))
	}
	if fc := lc.files; fc != nil {
		var seqs []int
		for _, p := range fc.paths {
			r, err := run.Backup(ctx, p, backup.RollbackRequired)
			if err != nil {
				return fail("backup of "+p, err)
			}
			seqs = append(seqs, r.Entry.Seq)
			d.say("backup %d: %s (%s) in %s", r.Entry.Seq, p, r.Entry.Kind, run.Dir())
		}
		j.Backup = &revert.Backups{Dir: run.Dir(), Seqs: seqs}
		if len(fc.keywords) > 0 {
			j.Reload = []revert.Reload{{Service: revert.ReloadSSH, Keywords: fc.keywords, After: revert.ListenersOf(fc.restored), IfUp: true,
				SocketDropIns: fc.socketDropIns}}
		}
		if err := record(); err != nil {
			return fail("journal the backups and the reload", err)
		}
		plan, err := fc.write(ctx, run)
		if err != nil {
			return fail("write", err)
		}
		rep, err := reload.Execute(ctx, d.sys, plan, reload.Options{})
		if len(j.Reload) > 0 && slices.ContainsFunc(rep.Steps, func(s reload.StepReport) bool { return s.Step.Kind == reload.StepIfUp && s.Ran }) {
			j.Reload[0].IfUp = false // the unit was up: a rollback starts it if the change left it down
			if rerr := recordRan(); rerr != nil {
				return fail("journal the reload", errors.Join(err, rerr))
			}
		}
		if err != nil {
			return fail("reload", err)
		}
		d.say("reload: verified %v%s", rep.Verified, because(rep.Unverified))
		after, findings, err := sshd.ReadEffective(ctx, d.sys, sshd.EffectiveRequest{SSH: lc.facts.SSH, Binary: lc.binary})
		if err != nil {
			return fail("sshd -T after the reload", err)
		}
		d.sayEffective("after", after, findings)
		if fc.want != nil {
			if err := fc.want(ctx, after); err != nil {
				return fail("check of the values", err)
			}
		}
	}
	if j, err = revert.MarkApplied(ctx, d.sys, lc.id, opts); err != nil {
		return fail("mark applied", err)
	}
	d.say("change %s applied; it is rolled back at %s unless confirmed.", lc.id, j.Deadline().Format(time.RFC3339))
	where := "a new SSH connection"
	if ports := portsOf(lc.adds); len(ports) > 0 {
		where = "a new SSH connection to port " + joinPorts(ports)
	}
	d.say("to keep it: from another machine, open %s and run `sudo %s confirm` (doas on Alpine).", where, o.hardn)
	d.say("to undo it now: sudo %s rollback %s", o.hardn, lc.id)
	return nil
}

// revertOptions are a change's journal options, with hardn the binary its
// timer runs: plans.Registry() is the registry cmd/hardn's `hardn
// rollback` passes, so Record accepts the plan steps the timer can undo,
// and the in-process rollback undoes them as the timer would.
func (d *driver) revertOptions(hardn string) revert.Options {
	return revert.Options{Log: d.out, Hardn: hardn, Now: d.now, Plans: plans.Registry()}
}

// rollBack rolls change id back at once, after step failed with err, while
// this session is open (revert.ReasonFailed): without the command's
// cancellation, with SIGHUP and SIGINT ignored and SIGTERM caught until it
// returns (d.hold).
func (d *driver) rollBack(ctx context.Context, id string, opts revert.Options, step string, err error) error {
	d.say("%s failed: %v", step, err)
	d.say("rolling change %s back while this session is open", id)
	opts.Reason = revert.ReasonFailed
	release := d.hold(d.out)
	_, rerr := revert.Rollback(context.WithoutCancel(ctx), d.sys, id, opts)
	release()
	if rerr == nil {
		return fmt.Errorf("%s: %w (rolled back)", step, err)
	}
	return fmt.Errorf("%s: %w; the rollback did not finish: %w", step, err, rerr)
}

// sayHardn prints the SHA-256 of the hardn binary the timer will run, to
// compare with the build's (docs/safety/vmtest.md §4.1): a v0.3.2 hardn
// installed over it has no confirm or rollback subcommand.
// revert.CheckExecutable, in Arm, checks its owner and mode, not what it
// is.
func (d *driver) sayHardn(ctx context.Context, name string) {
	data, err := d.sys.ReadFile(ctx, name)
	if err != nil {
		d.say("the revert timer runs %s: %v", name, err)
		return
	}
	sum := sha256.Sum256(data)
	d.say("the revert timer runs %s, sha256 %s: it must be the hardn built with this p2drive", name, hex.EncodeToString(sum[:]))
}

// checkFree refuses at plan time, before the timer is armed, a change
// whose added listeners another socket already holds (reload.ErrInUse). It
// is reload.Execute's own check, which Execute runs before any step: here
// over a plan with no steps, so that only that check runs. Execute runs it
// again before the reload.
func (d *driver) checkFree(ctx context.Context, rp reload.Plan) error {
	if len(rp.Adds) == 0 {
		return nil
	}
	_, err := reload.Execute(ctx, d.sys, reload.Plan{Mode: rp.Mode, Hold: rp.Hold, Adds: rp.Adds}, reload.Options{})
	return err
}

// undoSteps journals ufw's undo commands, in the order to run them.
func undoSteps(argvs [][]string) []revert.Step {
	var out []revert.Step
	for _, a := range argvs {
		out = append(out, revert.Step{Op: revert.OpRun, Argv: slices.Clone(a), Why: "firewall"})
	}
	return out
}

// joinPorts is "2230" or "2230 or 2231".
func joinPorts(ps []uint16) string {
	var s []string
	for _, p := range ps {
		s = append(s, fmt.Sprint(p))
	}
	return strings.Join(s, " or ")
}

// because is " (<why>)" when why is set.
func because(why string) string {
	if why == "" {
		return ""
	}
	return " (" + why + ")"
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

// lockoutCommand runs a planned lockout-risk change: the plan, then, unless
// --plan-only, apply. plan is the command's own planning; the run directory
// goes when it returns.
func (d *driver) lockoutCommand(ctx context.Context, o *lockoutOpts, plan func(b base) (lockout, error)) error {
	b, err := d.begin(ctx)
	defer d.end(ctx, b)
	if err != nil {
		return err
	}
	lc, err := plan(b)
	if err != nil {
		return fmt.Errorf("refused before anything changed: %w", err)
	}
	empty := lc.files == nil && (lc.ufw == nil || lc.ufw.Empty())
	if !empty {
		if lc.boot, err = d.planBoot(ctx, b.facts.Init, o.hardn); err != nil {
			return fmt.Errorf("refused before anything changed: %w", err)
		}
	}
	d.showPlan(lc)
	if empty {
		d.say("nothing to change")
		return nil
	}
	if o.planOnly {
		d.say("--plan-only: nothing changed")
		return nil
	}
	return d.apply(ctx, lc, o)
}

// sayEffective prints the lockout-relevant values sshd -T shows, the
// listeners and the findings.
func (d *driver) sayEffective(when string, e sshd.Effective, findings []sshd.Finding) {
	d.say("sshd -T %s: port %v (known %v); permitrootlogin %q, passwordauthentication %q, kbdinteractiveauthentication %q, pubkeyauthentication %q",
		when, e.Port.Values, e.Port.Known, e.PermitRootLogin.Value, e.PasswordAuthentication.Value, e.KbdInteractiveAuthentication.Value, e.PubkeyAuthentication.Value)
	d.say("listeners %s: %v (known %v; sshd %v, ssh.socket %v)", when, e.Listeners.All, e.Listeners.Known, e.Listeners.SSHD, e.Listeners.Socket)
	for _, f := range findings {
		at := ""
		if f.File != "" {
			at = fmt.Sprintf(" %s:%d", f.File, f.Line)
		}
		d.say("  finding %v%s: %s", f.Kind, at, f.Detail)
	}
}
