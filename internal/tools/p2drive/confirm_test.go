package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/confirm"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// `hardn confirm` with its clean-up (refactor plan P3,
// next/p3-confirm-cleanup; P-40, decided 2026-10-19), on p2drive's fake VM:
// the 2230 change made as vmtest.md §4.4 makes it (p2drive's port, or the
// engine's), then pkg/engine/confirm's Claim and Cleanup from a new
// connection to 2230, as cmd/hardn's runConfirm calls them. Nothing runs a
// real command: the VM is hostfake's.

// newConn is alice's new SSH connection from the runner to 2230, made
// after the apply (its sshd-session started then, so it was not running
// when MarkApplied listed them).
const newConn = "192.0.2.10 50123 192.0.2.20 2230"

func confirmingSession() revert.Session {
	return revert.Session{Remote: true, Conn: newConn, SSHD: revert.ProcStart{PID: 4100, Start: 410000}, Source: "SSH_CONNECTION"}
}

// noUFW is Alpine's OpenRC with sshd started and no ufw installed.
func noUFW(v *vm) {
	v.f.AddFile(detect.OpenRCStarted+"/sshd", nil, 0o644, 0, 0)
	v.hook = func(cmd string) (hostfake.Response, bool) {
		return hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}, strings.HasPrefix(cmd, "ufw ")
	}
}

// inactiveUFW is ufw installed, inactive, with no rules: `port --ufw
// enable` then adds 22's rule itself.
func inactiveUFW(v *vm) {
	v.allowed, v.inactive = nil, true
	v.saveRules()
}

// port2230 makes the 2230 change on v as vmtest.md §4.4 step 6 does, with
// p2drive's port command, and fails unless it is applied and waiting.
func (v *vm) port2230(facts detect.Facts, args ...string) revert.Journal {
	v.t.Helper()
	if code, out, errOut := v.do(context.Background(), facts, append([]string{"port", "--port", "2230", "--hardn", testHardn}, args...)...); code != 0 {
		v.t.Fatalf("p2drive port: exit %d; stderr %s\nstdout %s", code, errOut, out)
	}
	return v.journal(revert.PendingName)
}

// confirmOptions are the options cmd/hardn gives Claim and Cleanup, with
// the fake's clock, the facts given and a signal hold that counts.
func (v *vm) confirmOptions(facts detect.Facts, log *bytes.Buffer) (revert.Options, confirm.Options) {
	ro := revert.Options{Now: func() time.Time { return testNow }, Log: log}
	return ro, confirm.Options{Revert: ro, PID: 4000,
		Detect: func(context.Context, host.System) (detect.Facts, error) { return facts, nil },
		Hold: func() func() {
			v.held = len(v.f.Calls())
			return func() { v.released = len(v.f.Calls()) }
		}}
}

// confirmNow is hardn confirm from s on v: the claim, then the clean-up.
func (v *vm) confirmNow(sys host.System, facts detect.Facts, s revert.Session) (confirm.Held, error, confirm.Outcome) {
	v.t.Helper()
	var log bytes.Buffer
	ro, co := v.confirmOptions(facts, &log)
	h, err := confirm.Claim(context.Background(), sys, s, ro)
	if err != nil && !h.Run() {
		return h, err, confirm.Outcome{}
	}
	return h, err, confirm.Cleanup(context.Background(), sys, h, s, co)
}

// hostChangesSince are the commands and writes outside the run root that
// changed v from call number from (changesIn), as words.
func (v *vm) hostChangesSince(from int) []string { return changesIn(v.f.Calls()[from:]) }

// changingSince are the commands that changed v from call number from, as
// v.commands words them (every command run or started but the reads: sshd,
// ss, ufw status, systemctl's is-active, is-enabled, show and
// list-timers), without the revert timer's stop.
func (v *vm) changingSince(from int) []string {
	var out []string
	for _, c := range v.f.Calls()[from:] {
		if c.Op != "run" && c.Op != "start-detached" {
			continue
		}
		cmd := strings.Join(c.Argv, " ")
		if path.Base(c.Argv[0]) == "sshd" || c.Argv[0] == "ss" || cmd == "ufw status verbose" ||
			c.Argv[0] == "systemctl" && slices.Contains([]string{"is-active", "is-enabled", "show", "list-timers"}, c.Argv[1]) ||
			c.Argv[0] == "systemctl" && c.Argv[1] == "stop" && strings.HasPrefix(c.Argv[2], revert.UnitPrefix) {
			continue
		}
		out = append(out, cmd)
	}
	return out
}

// TestConfirmCleanup: once a new connection to 2230 confirms the change,
// hardn confirm drops port 22 from 00-hardn.conf, reloads sshd and deletes
// ufw's rule for 22 when the change added it, through the engine, with no
// revert timer of its own, on Debian's ssh.service, Ubuntu's ssh.socket
// and Alpine's OpenRC; port 22 is then gone from sshd -T, from what sshd
// binds, and from ufw where hardn put it there. A second hardn confirm
// finds nothing pending and nothing left to clean up.
func TestConfirmCleanup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		vm      func(*testing.T) *vm
		setup   func(*vm)
		facts   detect.Facts
		args    []string
		engine  bool     // the change made through the engine, not p2drive
		want    []string // the commands after the claim, in order
		allowed []uint16 // ufw's rules at the end
		bound   []string
	}{
		{"Debian 13 ssh.service, ufw allowed 22 before: its rule stays", newVM, nil, debian13, nil, false,
			[]string{"systemctl reload ssh.service"}, []uint16{22, 2230}, []string{"[::]:2230", "0.0.0.0:2230"}},
		{"Debian 13 ssh.service, --ufw enable from no rules: hardn's rule for 22 goes", newVM, inactiveUFW, debian13, []string{"--ufw", "enable"}, false,
			[]string{"ufw delete allow 22/tcp", "systemctl reload ssh.service"}, []uint16{2230}, []string{"[::]:2230", "0.0.0.0:2230"}},
		{"Debian 13 ssh.service, the change made through the engine", newVM, inactiveUFW, debian13, nil, true,
			[]string{"ufw delete allow 22/tcp", "systemctl reload ssh.service"}, []uint16{2230}, []string{"[::]:2230", "0.0.0.0:2230"}},
		{"Ubuntu 26.04 ssh.socket", socketVM, nil, ubuntu2604(), nil, false,
			[]string{"systemctl daemon-reload", "systemctl restart ssh.socket"}, []uint16{22, 2230}, []string{"[::]:2230", "0.0.0.0:2230"}},
		{"Ubuntu 26.04 ssh.socket, --ufw enable from no rules", socketVM, inactiveUFW, ubuntu2604(), []string{"--ufw", "enable"}, false,
			[]string{"ufw delete allow 22/tcp", "systemctl daemon-reload", "systemctl restart ssh.socket"}, []uint16{2230}, []string{"[::]:2230", "0.0.0.0:2230"}},
		{"Alpine 3.24 OpenRC, no ufw", newVM, noUFW, alpine324(), nil, false,
			[]string{"rc-service sshd reload"}, []uint16{22}, []string{"[::]:2230", "0.0.0.0:2230"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := tc.vm(t)
			if tc.setup != nil {
				tc.setup(v)
			}
			var j revert.Journal
			if tc.engine {
				if _, _, err := v.viaEngine(context.Background(), tc.facts, sshControl(steps.SSH{Port: 2230, UFW: steps.UFWEnable})); err != nil {
					t.Fatal(err)
				}
				j = v.journal(revert.PendingName)
			} else {
				j = v.port2230(tc.facts, tc.args...)
			}
			from := len(v.f.Calls())
			h, err, o := v.confirmNow(v.f, tc.facts, confirmingSession())
			if err != nil || h.Confirmed == nil || h.Finishing || !h.Run() || !slices.Equal(h.Cleanup.Drop, []uint16{22}) {
				t.Fatalf("Claim = %+v, %v; want the change confirmed, 22 held, the clean-up to run", h, err)
			}
			if !o.Done() || o.Result.Nothing || o.Plan.Risk == engine.RiskLockout {
				t.Fatalf("Cleanup = %+v (plan risk %v), err %v; want applied, with no lockout risk", o, o.Plan.Risk, o.Err)
			}
			// What changed after the claim: the timer stopped (systemd), the
			// clean-up's commands, its backup and its journal, written and
			// removed; never a revert timer of its own.
			if cmds := v.changingSince(from); !slices.Equal(cmds, tc.want) {
				t.Errorf("commands after the claim %q, want %q", cmds, tc.want)
			}
			for _, c := range v.f.Calls()[from:] {
				if c.Op == "start-detached" || c.Op == "run" && c.Argv[0] == "systemd-run" {
					t.Errorf("a revert timer for the clean-up: %s", c)
				}
			}
			if got := v.directives(); got != "Port 2230" {
				t.Errorf("00-hardn.conf's directives %q, want Port 2230 alone", got)
			}
			if out, _ := sshdT(v.files(sshd.MainConfig, false)); strings.Contains(out, "port 22\n") || !strings.Contains(out, "port 2230\n") {
				t.Errorf("sshd -T after the clean-up:\n%s", out)
			}
			if !sameStrings(v.bound, tc.bound) || !slices.Equal(v.allowed, tc.allowed) {
				t.Errorf("sshd binds %v, ufw allows %v; want %v, %v", v.bound, v.allowed, tc.bound, tc.allowed)
			}
			if _, ok := v.f.Entry(path.Join(revert.DefaultDir, revert.PendingName)); ok {
				t.Error("the clean-up's journal is left")
			}
			if c := v.journal(revert.ConfirmedName); c.ID != j.ID {
				t.Errorf("confirmed.json holds %s, want the change confirmed, %s", c.ID, j.ID)
			}
			if r := h.Report(o); len(r.Done) == 0 || r.Finish != "" || len(r.Left) > 0 || !strings.Contains(r.Done[0], "Port 22, which change "+j.ID) {
				t.Errorf("Report = %+v", r)
			}
			// hardn confirm again: nothing pending, nothing left to clean up.
			h, err, o = v.confirmNow(v.f, tc.facts, confirmingSession())
			if !errors.Is(err, revert.ErrNothingPending) || !h.Finishing || !o.Done() || !o.Result.Nothing {
				t.Errorf("confirm again: %+v, %v, %+v; want nothing pending and nothing to clean up", h, err, o)
			}
		})
	}
}

// TestConfirmNothingHeld: a change that held no port (keyonly) is confirmed
// as before the clean-up existed: no clean-up, nothing read or run after
// the claim but the timer's stop.
func TestConfirmNothingHeld(t *testing.T) {
	v := newVM(t)
	if code, out, errOut := v.do(context.Background(), debian13, "keyonly", "--hardn", testHardn); code != 0 {
		t.Fatalf("p2drive keyonly: exit %d; %s %s", code, errOut, out)
	}
	from := len(v.f.Calls())
	h, err, o := v.confirmNow(v.f, debian13, confirmingSession())
	if err != nil || h.Confirmed == nil || h.Holds() || h.Run() || o.Plan != nil || o.Result != nil {
		t.Fatalf("Claim = %+v, %v; Cleanup %+v: want confirmed with nothing held", h, err, o)
	}
	for _, c := range v.f.Calls()[from:] {
		if c.Op == "run" && !(c.Argv[0] == "systemctl" && c.Argv[1] == "stop") {
			t.Errorf("after the claim: %s", c)
		}
	}
	if r := h.Report(o); len(r.Done)+len(r.Left) > 0 || r.Finish != "" {
		t.Errorf("Report = %+v, want nothing", r)
	}
}

// failWrite fails the write of one file, as a full disk would.
type failWrite struct {
	*hostfake.System
	name string
}

func (f failWrite) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if name == f.name {
		return errors.New("fixture: no space left on device")
	}
	return f.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

// TestConfirmCleanupFails: a clean-up step that fails (ufw's delete, the
// write of 00-hardn.conf, the reload, the check after it) is rolled back in
// this process, signals held: port 22 is held again, as before the
// clean-up, its ufw rule back, its journal gone; the confirmation stands
// (confirmed.json). The report says what is still open and how to finish
// it; the next hardn confirm, once the cause is gone, finishes it.
func TestConfirmCleanupFails(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fail  func(v *vm) (sys host.System, fix func())
		step  string
		cause error // the step's error matches it, when set
	}{
		{"ufw's delete", func(v *vm) (host.System, func()) {
			v.hook = func(cmd string) (hostfake.Response, bool) {
				return hostfake.Response{Stderr: "ERROR: fixture\n", ExitCode: 1}, cmd == "ufw delete allow 22/tcp"
			}
			return v.f, func() { v.hook = nil }
		}, "ssh.cleanup/firewall", nil},
		{"the write of 00-hardn.conf", func(v *vm) (host.System, func()) {
			return failWrite{v.f, sshd.HardnDropIn}, func() {}
		}, "ssh.cleanup/files", nil},
		{"the reload", func(v *vm) (host.System, func()) {
			v.failReloads = 1
			return v.f, func() {}
		}, "ssh.cleanup/reload", nil},
		{"the check after the reload: sshd -T still shows 22", func(v *vm) (host.System, func()) {
			v.hook = func(cmd string) (hostfake.Response, bool) {
				if cmd == "systemctl reload ssh.service" {
					v.stale = true // sshd did not reread 00-hardn.conf
				}
				return hostfake.Response{}, false
			}
			return v.f, func() { v.hook, v.stale = nil, false }
		}, "ssh.cleanup/verify", steps.ErrNotInEffect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVM(t)
			inactiveUFW(v)
			j := v.port2230(debian13, "--ufw", "enable")
			before, allowed := v.dropIn(), slices.Clone(v.allowed)
			sys, fix := tc.fail(v)
			h, err, o := v.confirmNow(sys, debian13, confirmingSession())
			if err != nil || h.Confirmed == nil {
				t.Fatalf("Claim = %+v, %v; want confirmed", h, err)
			}
			var ae *engine.ApplyError
			if !errors.As(o.Err, &ae) || !errors.Is(o.Err, engine.ErrRolledBack) || ae.Change != tc.step || (tc.cause != nil && !errors.Is(o.Err, tc.cause)) {
				t.Fatalf("Cleanup err = %v; want %s failed and rolled back", o.Err, tc.step)
			}
			if v.held < 0 || v.released < v.held {
				t.Errorf("signals held %d, released %d: want the rollback under the hold", v.held, v.released)
			}
			v.stale = false
			// The rollback's `ufw allow 22/tcp` meets a rule ufw kept when its
			// delete failed: ufw skips an existing rule, which the fake does
			// not model, so it is counted once.
			if v.dropIn() != before || !slices.Equal(slices.Compact(slices.Clone(v.allowed)), allowed) || !sameStrings(v.bound, []string{"[::]:22", "0.0.0.0:22", "[::]:2230", "0.0.0.0:2230"}) {
				t.Errorf("after the rollback: 00-hardn.conf %q, ufw %v, sshd on %v; want as before the clean-up", v.dropIn(), v.allowed, v.bound)
			}
			if _, ok := v.f.Entry(path.Join(revert.DefaultDir, revert.PendingName)); ok {
				t.Error("the clean-up's journal is left")
			}
			if c := v.journal(revert.ConfirmedName); c.ID != j.ID {
				t.Errorf("confirmed.json %s, want %s: the confirmation stands", c.ID, j.ID)
			}
			r := h.Report(o)
			if len(r.Left) != 1 || !strings.Contains(r.Left[0], "Port 22, which change "+j.ID+" held during its confirm window, is still open") ||
				!strings.Contains(r.Finish, "`sudo hardn confirm` again from a new SSH connection to port 2230") ||
				!strings.Contains(r.Finish, "delete the `Port 22` line from "+sshd.HardnDropIn) ||
				!strings.Contains(r.Finish, "`sudo systemctl reload ssh.service`, and `sudo ufw delete allow 22/tcp`") {
				t.Errorf("Report = %+v", r)
			}
			// The cause gone, hardn confirm again finishes it.
			fix()
			h, err, o = v.confirmNow(v.f, debian13, confirmingSession())
			if !errors.Is(err, revert.ErrNothingPending) || !h.Finishing || !h.Run() || !o.Done() || o.Result.Nothing {
				t.Fatalf("confirm again: %+v, %v, %+v; want the clean-up finished", h, err, o)
			}
			if v.directives() != "Port 2230" || !slices.Equal(v.allowed, []uint16{2230}) {
				t.Errorf("finished: 00-hardn.conf %q, ufw %v", v.directives(), v.allowed)
			}
		})
	}
}

// TestConfirmCleanupRefused: every refusal PlanCleanup has is kept, before
// anything changes, and the confirmation stands: a Port line in
// sshd_config, which hardn does not edit; a clean-up that would leave sshd
// no port; Proxmox VE's port 22; a session not on a port the clean-up
// keeps.
func TestConfirmCleanupRefused(t *testing.T) {
	for _, tc := range []struct {
		name    string
		before  func(v *vm)                                     // before the change
		after   func(v *vm, h *confirm.Held, s *revert.Session) // after the claim, before the clean-up
		facts   detect.Facts                                    // the clean-up's
		refused error
	}{
		{"Port 22 in sshd_config", func(v *vm) {
			v.f.AddFile(sshd.MainConfig, []byte(stockMain+"Port 22\n"), 0o644, 0, 0)
			v.boot()
		}, nil, debian13, steps.ErrNoWriteStrategy},
		{"no port left: 2230 taken out of 00-hardn.conf by hand", nil, func(v *vm, _ *confirm.Held, _ *revert.Session) {
			v.f.AddFile(sshd.HardnDropIn, []byte("# Managed by hardn.\n# Set by p2drive:\nPort 22\n"), 0o644, 0, 0)
		}, debian13, reload.ErrDropsListener},
		{"Proxmox VE's port 22", nil, nil, proxmox9(), pve.ErrPortChange},
		{"a session on the port the clean-up drops", nil, func(_ *vm, h *confirm.Held, s *revert.Session) {
			s.Conn = "192.0.2.10 50123 192.0.2.20 22"
		}, debian13, steps.ErrSessionNotKept},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVM(t)
			if tc.before != nil {
				tc.before(v)
			}
			j := v.port2230(debian13)
			var log bytes.Buffer
			ro, co := v.confirmOptions(tc.facts, &log)
			s := confirmingSession()
			h, err := confirm.Claim(context.Background(), v.f, s, ro)
			if err != nil || !h.Run() {
				t.Fatalf("Claim = %+v, %v", h, err)
			}
			if tc.after != nil {
				tc.after(v, &h, &s)
			}
			from := len(v.f.Calls())
			o := confirm.Cleanup(context.Background(), v.f, h, s, co)
			if !errors.Is(o.Err, engine.ErrRefused) || !errors.Is(o.Err, tc.refused) || o.Result != nil || o.Plan == nil {
				t.Fatalf("Cleanup = %+v; want refused at plan time with %v", o, tc.refused)
			}
			if ch := v.hostChangesSince(from); len(ch) > 0 {
				t.Errorf("changed before the refusal: %q", ch)
			}
			if c := v.journal(revert.ConfirmedName); c.ID != j.ID {
				t.Errorf("confirmed.json %s: the confirmation stands", c.ID)
			}
			if r := h.Report(o); len(r.Left) != 1 || !strings.Contains(r.Finish, "or by hand: delete the `Port 22` line") {
				t.Errorf("Report = %+v", r)
			}
		})
	}
}

// TestConfirmCrashAfterClaim: hardn killed between the claim and the
// clean-up leaves port 22 held, recorded in confirmed.json alone. The next
// hardn confirm, from a session that could have confirmed the change,
// finishes it; from the session that applied it, or a console, it says what
// is left and how to finish it, and changes nothing. The SSH slice can
// read the same: steps.CleanupOf(revert.ReadConfirmed).
func TestConfirmCrashAfterClaim(t *testing.T) {
	ctx := context.Background()
	v := newVM(t)
	inactiveUFW(v)
	j := v.port2230(debian13, "--ufw", "enable")
	// The claim alone, as hardn confirm makes it, then nothing: killed.
	if _, err := revert.Confirm(ctx, v.f, confirmingSession(), revert.Options{Now: func() time.Time { return testNow }}); err != nil {
		t.Fatal(err)
	}
	if v.directives() != "Port 22\nPort 2230" || !slices.Equal(v.allowed, []uint16{22, 2230}) {
		t.Fatalf("after the claim: %q, ufw %v; want both held", v.directives(), v.allowed)
	}
	c, err := revert.ReadConfirmed(ctx, v.f, revert.Options{})
	if err != nil || c == nil || !slices.Equal(steps.CleanupOf(*c).Drop, []uint16{22}) {
		t.Fatalf("ReadConfirmed = %+v, %v; want the change, holding 22", c, err)
	}
	for _, tc := range []struct {
		name string
		s    revert.Session
		want error // CheckConfirm's refusal; nil: a console
	}{
		{"the session that applied the change", revert.Session{Remote: true, Conn: applyConn, SSHD: revert.ProcStart{PID: 3970, Start: 397000}, Source: "SSH_CONNECTION"},
			revert.ErrSameSession},
		{"a connection from the host itself", revert.Session{Remote: true, Conn: "127.0.0.1 50124 127.0.0.1 2230", SSHD: revert.ProcStart{PID: 4200, Start: 420000}, Source: "SSH_CONNECTION"},
			revert.ErrLocalConn},
		{"a console", revert.Session{Source: "login"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from := len(v.f.Calls())
			h, err, o := v.confirmNow(v.f, debian13, tc.s)
			if !errors.Is(err, revert.ErrNothingPending) || !h.Finishing || h.Run() || o.Plan != nil {
				t.Fatalf("Claim = %+v, %v; Cleanup %+v: want nothing pending, the clean-up left", h, err, o)
			}
			if (tc.want == nil) != h.Console || tc.want != nil && !errors.Is(h.Refused, tc.want) {
				t.Errorf("console %v, refused %v; want %v", h.Console, h.Refused, tc.want)
			}
			if ch := v.hostChangesSince(from); len(ch) > 0 {
				t.Errorf("changed: %q", ch)
			}
			if r := h.Report(o); len(r.Left) != 1 || !strings.Contains(r.Left[0]+" "+r.Finish, "a new SSH connection to port 2230, made from another machine") {
				t.Errorf("Report = %+v", r)
			}
		})
	}
	h, err, o := v.confirmNow(v.f, debian13, confirmingSession())
	if !errors.Is(err, revert.ErrNothingPending) || !h.Finishing || !h.Run() || h.Change.ID != j.ID || !o.Done() || o.Result.Nothing {
		t.Fatalf("Claim = %+v, %v; Cleanup %+v: want the clean-up finished", h, err, o)
	}
	if v.directives() != "Port 2230" || !slices.Equal(v.allowed, []uint16{2230}) || !sameStrings(v.bound, []string{"[::]:2230", "0.0.0.0:2230"}) {
		t.Errorf("finished: 00-hardn.conf %q, ufw %v, sshd on %v", v.directives(), v.allowed, v.bound)
	}
	if _, err := ufw.Read(ctx, v.f, debian13.Proxmox); err != nil {
		t.Fatal(err)
	}
}
