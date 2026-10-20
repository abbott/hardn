package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// The engine through pkg/engine/steps on p2drive's fake VM (refactor plan
// P3, next/p3-steps): the scenarios p2drive's own tests run, on twin hosts,
// give the same commands that change the host, in the same order, the same
// journal and the same sshd settings; and every refusal of the driver's
// comes from the engine's plan, before the revert timer. p2drive itself is
// unchanged: this file only drives the engine over its fake.

// stepsControl is a control whose Plan is one of the steps planners.
type stepsControl struct {
	id   string
	plan func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error)
}

func (c stepsControl) ID() string                          { return c.id }
func (c stepsControl) Title() string                       { return "Example " + c.id }
func (c stepsControl) Applies(detect.Facts) (bool, string) { return true, "" }
func (c stepsControl) Check(context.Context, host.System, detect.Facts) (engine.Status, error) {
	return engine.Status{}, nil
}
func (c stepsControl) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	return c.plan(ctx, sys, in)
}

func sshControl(s steps.SSH) stepsControl {
	s.Prefix = "ssh"
	return stepsControl{"ssh", func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		return steps.PlanSSH(ctx, sys, in, s)
	}}
}

func port2230() stepsControl { return sshControl(steps.SSH{Port: 2230}) }

func keyOnly() stepsControl {
	return sshControl(steps.SSH{Set: []migrate.Directive{{Keyword: "PasswordAuthentication", Args: []string{"no"}},
		{Keyword: "KbdInteractiveAuthentication", Args: []string{"no"}}}})
}

func ufwEnableControl() stepsControl {
	return stepsControl{"ufw", func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		return steps.PlanUFWEnable(ctx, sys, in, "ufw")
	}}
}

func migration() stepsControl {
	return stepsControl{"ssh.migrate", func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		return steps.PlanMigration(ctx, sys, in, "ssh.migrate", "")
	}}
}

// sudoersGrant grants alice sudo without a password, with p2drive's
// summary, so that the plan step reads the same.
func sudoersGrant() stepsControl {
	return stepsControl{"sudoers", func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		pol := sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword}
		c := sudoers.Choose(in.Facts, sudoers.ToolUnknown)
		if c.Err != nil {
			return nil, c.Err
		}
		st, err := sudoers.Read(ctx, sys, c, []string{pol.User})
		if err != nil {
			return nil, err
		}
		p, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
		if err != nil || len(p.Changes) == 0 {
			return nil, err
		}
		ch, err := steps.Sudoers("sudoers/alice", p, fmt.Sprintf("%v rules for %s: grant %v", c.Tool, pol.User, pol.Grant))
		return []engine.Change{ch}, err
	}}
}

// sshServiceDown is Debian 13 with ssh.service stopped, as detect shows
// it: sshd's listeners are then not established (sshd.Listeners.Known).
func sshServiceDown() detect.Facts {
	f := debian13
	f.SSH.Service = detect.UnitState{Enabled: "enabled", Active: "inactive"}
	return f
}

// viaEngine plans and applies the controls on v as a host with facts, as
// hardn's wiring would: the applying session, the hardn binary the timer
// runs, backups enabled under the default path, the fake's clock, and a
// signal hold that records when it held and released. systemctl
// list-timers fails, so the deadline is the journal's (engine.md §2.15).
func (v *vm) viaEngine(ctx context.Context, facts detect.Facts, cs ...engine.Control) (*engine.Plan, engine.Result, error) {
	prev := v.hook
	v.hook = func(cmd string) (hostfake.Response, bool) {
		if strings.HasPrefix(cmd, "systemctl list-timers ") {
			return hostfake.Response{ExitCode: 1, Stderr: "fixture: no timers\n"}, true
		}
		if prev != nil {
			return prev(cmd)
		}
		return hostfake.Response{}, false
	}
	var log bytes.Buffer
	e := engine.New(v.f, engine.Options{
		Revert:  revert.Options{Now: func() time.Time { return testNow }, Hardn: testHardn, Log: &log},
		Session: revert.Session{Remote: true, Conn: v.conn, Source: "SSH_CONNECTION"},
		Backups: backup.Settings{Path: backup.DefaultPath, Enabled: true},
		Detect:  func(context.Context, host.System) (detect.Facts, error) { return facts, nil },
		PID:     5000,
		Hold: func() func() {
			v.held = len(v.f.Calls())
			return func() { v.released = len(v.f.Calls()) }
		},
	})
	p, err := e.Plan(ctx, cs, nil)
	if err != nil {
		return nil, engine.Result{}, err
	}
	if err := p.Err(); err != nil {
		return p, engine.Result{}, err
	}
	res, err := e.Apply(ctx, p)
	return p, res, err
}

// lastJournal is the last pending.json written, decoded.
func (v *vm) lastJournal() (revert.Journal, bool) {
	var j revert.Journal
	calls := v.f.Calls()
	for i := len(calls) - 1; i >= 0; i-- {
		if c := calls[i]; c.Op == "write-atomic" && c.Path == path.Join(revert.DefaultDir, revert.PendingName) && c.Err == nil {
			if err := json.Unmarshal(c.Data, &j); err != nil {
				v.t.Fatal(err)
			}
			return j, true
		}
	}
	return j, false
}

// commands are the commands that changed the host, in order, with id as
// <id>: every command run or started but sshd's, ss's, ufw status and
// systemctl's reads (is-active, is-enabled, show, list-timers).
func (v *vm) commands(id string) []string {
	var out []string
	for _, c := range v.f.Calls() {
		if c.Op != "run" && c.Op != "start-detached" {
			continue
		}
		cmd := strings.Join(c.Argv, " ")
		if path.Base(c.Argv[0]) == "sshd" || c.Argv[0] == "ss" || cmd == "ufw status verbose" ||
			c.Argv[0] == "systemctl" && slices.Contains([]string{"is-active", "is-enabled", "show", "list-timers"}, c.Argv[1]) {
			continue
		}
		out = append(out, c.Op+" "+strings.ReplaceAll(cmd, id, "<id>"))
	}
	return out
}

// normalJournal is j with its id as <id> everywhere, and no summary: the
// engine's is its plan's (Plan.Summary), the driver's its own words. A
// change without lockout risk has no session either: the engine journals
// the applying session for every change (engine.Options.Session), the
// driver only where a confirmation needs it.
func normalJournal(j revert.Journal) string {
	id := j.ID
	j.Summary = ""
	if j.Risk != revert.RiskLockout {
		j.Session = revert.Session{}
	}
	data, _ := json.MarshalIndent(j, "", "  ")
	return strings.ReplaceAll(string(data), id, "<id>")
}

// directives are 00-hardn.conf's lines but its comments: the driver's file
// and the engine's differ in their header and label only.
func (v *vm) directives() string {
	var out []string
	for _, l := range strings.Split(v.dropIn(), "\n") {
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// TestEngineParity: brief Phase 2's scenarios through the engine give
// p2drive's commands, in its order, its journal and its sshd settings, on
// Debian's ssh.service, Ubuntu's ssh.socket and Alpine's OpenRC.
func TestEngineParity(t *testing.T) {
	noUFW := func(v *vm) {
		v.f.AddFile(detect.OpenRCStarted+"/sshd", nil, 0o644, 0, 0)
		v.hook = func(cmd string) (hostfake.Response, bool) {
			return hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}, strings.HasPrefix(cmd, "ufw ")
		}
	}
	inactive := func(v *vm) {
		v.allowed, v.inactive = nil, true
		v.saveRules()
	}
	for _, tc := range []struct {
		name  string
		vm    func(*testing.T) *vm
		setup func(*vm)
		facts detect.Facts
		args  []string
		c     engine.Control
		want  []string // the commands, as a check that the scenario ran
	}{
		{"2230, Debian 13 ssh.service", newVM, nil, debian13, []string{"port", "--port", "2230", "--hardn", testHardn}, port2230(),
			[]string{"ufw allow 2230/tcp", "systemctl reload ssh.service"}},
		{"2230, Ubuntu 26.04 ssh.socket", socketVM, nil, ubuntu2604(), []string{"port", "--port", "2230", "--hardn", testHardn}, port2230(),
			[]string{"ufw allow 2230/tcp", "systemctl daemon-reload", "systemctl restart ssh.socket"}},
		{"2230, Alpine 3.24 OpenRC", newVM, noUFW, alpine324(), []string{"port", "--port", "2230", "--hardn", testHardn}, port2230(),
			[]string{"rc-service sshd reload"}},
		{"keyonly, Debian 13", newVM, nil, debian13, []string{"keyonly", "--hardn", testHardn}, keyOnly(),
			[]string{"systemctl reload ssh.service"}},
		{"keyonly, Ubuntu 26.04", socketVM, nil, ubuntu2604(), []string{"keyonly", "--hardn", testHardn}, keyOnly(),
			[]string{"systemctl reload ssh.service"}},
		{"keyonly, Alpine 3.24", newVM, noUFW, alpine324(), []string{"keyonly", "--hardn", testHardn}, keyOnly(),
			[]string{"rc-service sshd reload"}},
		{"ufw-enable, Debian 13", newVM, inactive, debian13, []string{"ufw-enable", "--hardn", testHardn}, ufwEnableControl(),
			[]string{"ufw allow 22/tcp", "ufw --force enable"}},
		{"ufw-enable, Ubuntu 26.04", socketVM, inactive, ubuntu2604(), []string{"ufw-enable", "--hardn", testHardn}, ufwEnableControl(),
			[]string{"ufw allow 22/tcp", "ufw --force enable"}},
		{"sudoers grant, Debian 13", newVM, nil, debian13, []string{"sudoers", "--user", "alice", "--grant", "nopasswd"}, sudoersGrant(),
			[]string{"visudo -c", "visudo -c"}},
		{"sudoers grant, Ubuntu 26.04", socketVM, nil, ubuntu2604(), []string{"sudoers", "--user", "alice", "--grant", "nopasswd"}, sudoersGrant(),
			[]string{"visudo -c", "visudo -c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			driver, eng := tc.vm(t), tc.vm(t)
			if tc.setup != nil {
				tc.setup(driver)
				tc.setup(eng)
			}
			if code, out, errOut := driver.do(context.Background(), tc.facts, tc.args...); code != 0 {
				t.Fatalf("p2drive: exit %d; stderr %s\nstdout %s", code, errOut, out)
			}
			p, res, err := eng.viaEngine(context.Background(), tc.facts, tc.c)
			if err != nil {
				t.Fatalf("engine: %v", err)
			}
			jd, okd := driver.lastJournal()
			je, oke := eng.lastJournal()
			if !okd || !oke {
				t.Fatalf("journal written: p2drive %v, engine %v", okd, oke)
			}
			dc, ec := driver.commands(jd.ID), eng.commands(je.ID)
			if !slices.Equal(dc, ec) {
				t.Errorf("commands:\np2drive %q\nengine  %q", dc, ec)
			}
			for _, w := range tc.want {
				if !slices.ContainsFunc(ec, func(c string) bool { return strings.HasSuffix(c, " "+w) }) {
					t.Errorf("the engine did not run %q: %q", w, ec)
				}
			}
			if a, b := normalJournal(jd), normalJournal(je); a != b {
				t.Errorf("journal:\np2drive %s\nengine  %s", a, b)
			}
			if a, b := driver.directives(), eng.directives(); a != b {
				t.Errorf("00-hardn.conf's directives: p2drive %q, engine %q", a, b)
			}
			if !slices.Equal(driver.allowed, eng.allowed) || driver.inactive != eng.inactive || !sameStrings(driver.bound, eng.bound) {
				t.Errorf("ufw %v (inactive %v), bound %v; p2drive's %v (inactive %v), bound %v", eng.allowed, eng.inactive, eng.bound,
					driver.allowed, driver.inactive, driver.bound)
			}
			if p.Risk == engine.RiskLockout && (res.Confirm == nil || res.Confirm.ID != je.ID) {
				t.Errorf("Result.Confirm %+v, want the change's", res.Confirm)
			}
			if _, ok := eng.f.Entry(path.Join(runRoot, je.ID)); ok {
				t.Error("the engine left its run directory")
			}
		})
	}
}

// TestEngineOrder: on Debian's ssh.service, the 2230 change through the
// engine keeps brief 2.3's order, as TestPort2230 pins it for p2drive:
// armed, ufw allow, the backup, the journal (with the reload, IfUp, before
// the write), the write, the reload; and the journal ends with the reload
// without IfUp, confirmed by 2230.
func TestEngineOrder(t *testing.T) {
	v := newVM(t)
	if _, _, err := v.viaEngine(context.Background(), debian13, port2230()); err != nil {
		t.Fatal(err)
	}
	pending := path.Join(revert.DefaultDir, revert.PendingName)
	backedUp := v.index(func(c hostfake.Call) bool {
		return c.Op == "write-atomic" && strings.HasPrefix(c.Path, "/var/backups/hardn/") && strings.HasSuffix(c.Path, "manifest.json")
	})
	written := v.index(writeOf(sshd.HardnDropIn))
	journalled := -1
	for i, c := range v.f.Calls()[backedUp+1 : written] {
		var j revert.Journal
		if c.Op == "write-atomic" && c.Path == pending && json.Unmarshal(c.Data, &j) == nil && len(j.Reload) == 1 && j.Reload[0].IfUp && j.Backup != nil {
			journalled = backedUp + 1 + i
		}
	}
	order := []int{v.index(armed), v.index(runIs("ufw allow 2230/tcp")), backedUp, journalled, written,
		v.index(runIs("systemctl reload ssh.service"))}
	if !slices.IsSorted(order) || slices.Contains(order, -1) {
		t.Errorf("order: armed, ufw allow, backup, journal with the reload IfUp, write, reload at %v; want ascending", order)
	}
	j := v.journal(revert.PendingName)
	if j.Applied == nil || len(j.Reload) != 1 || j.Reload[0].IfUp || len(j.ConfirmVia) != 2 || j.ConfirmVia[0].Port != 2230 {
		t.Errorf("journal applied %v, reload %+v, confirmVia %v", j.Applied != nil, j.Reload, j.ConfirmVia)
	}
}

// TestEngineRefusedBeforeArm: every refusal of p2drive's TestRefusedBeforeArm
// is the engine's plan's, before the timer, the firewall or any write
// outside the run directory; the D9 host and sshd -T without /run/sshd come
// with a report (steps.Refusal and its code).
func TestEngineRefusedBeforeArm(t *testing.T) {
	migrated := "# Managed by hardn.\n# From /etc/ssh/sshd_config.d/manage.conf:\nListenAddress 0.0.0.0:2230\nPort 2230\n"
	for _, tc := range []struct {
		name  string
		facts detect.Facts
		setup func(*vm)
		c     engine.Control
		want  string
		code  string // a steps.Refusal's
		is    error
	}{
		{"a port change on Proxmox VE (§3.18)", proxmox9(), nil, port2230(), "port 22", "", nil},
		{"a ListenAddress in sshd_config pins port 22", debian13, func(v *vm) {
			v.f.AddFile(sshd.MainConfig, []byte(stockMain+"ListenAddress 0.0.0.0:22\n"), 0o644, 0, 0)
			v.boot()
		}, port2230(), `pinned by /etc/ssh/sshd_config:4 "ListenAddress 0.0.0.0:22"`, "", steps.ErrNoNewListener},
		{"a migrated 00-hardn.conf pins 2230", debian13, func(v *vm) {
			v.f.AddFile(sshd.HardnDropIn, []byte(migrated), 0o644, 0, 0)
			v.boot()
		}, sshControl(steps.SSH{Port: 2231}), `pinned by /etc/ssh/sshd_config.d/00-hardn.conf:3 "ListenAddress 0.0.0.0:2230"`, "", steps.ErrNoNewListener},
		{"another service holds the new port", debian13, func(v *vm) { v.other = []string{"0.0.0.0:2230"} }, port2230(), "already", "", nil},
		{"legacy hardn files", debian13, func(v *vm) {
			v.f.AddFile(migrate.ManageConf, fixture(v.t, "v032-manage.conf"), 0o644, 0, 0)
			v.boot()
		}, sshControl(steps.SSH{Port: 2231}), migrate.ManageConf, "", steps.ErrMigrateFirst},
		{"an Include that is not the stock first line (P-38)", debian13, func(v *vm) {
			v.f.AddFile(sshd.MainConfig, []byte("UsePAM yes\n"+sshd.IncludeLine+"\n"), 0o644, 0, 0)
			v.boot()
		}, keyOnly(), "P-38", "", steps.ErrNoWriteStrategy},
		{"the migration of listen.conf on a socket host that never read it (D9, P-39 (a))", ubuntu2604(), func(v *vm) {
			v032Files(v)
			v.configured = []string{"[::]:22", "0.0.0.0:22"}
			v.bound = slices.Clone(v.configured)
		}, migration(), "systemctl daemon-reload` and `systemctl restart ssh.socket`", steps.CodeListenConfNotInEffect, nil},
		{"the migration where ssh.socket is configured for more than listen.conf", ubuntu2604(), func(v *vm) {
			v032Files(v)
			v.configured = []string{"[::]:22", "0.0.0.0:22", "[::]:2230", "0.0.0.0:2230"}
			v.bound = slices.Clone(v.configured)
		}, migration(), "do not give [::]:22, which the socket had", "", nil},
		{"the migration where ssh.socket is configured for 2230 and bound on 22", ubuntu2604(), func(v *vm) {
			v032Files(v)
			v.configured = []string{"[::]:2230"}
			v.bound = []string{"[::]:22", "0.0.0.0:22"}
		}, migration(), "this session is connected to 192.0.2.20:22", "", nil},
		{"the migration, configured for 2230, bound on 22, a session on 2230", ubuntu2604(), func(v *vm) {
			v032Files(v)
			v.configured = []string{"[::]:2230"}
			v.bound = []string{"[::]:22", "0.0.0.0:22"}
			v.conn = conn2230
		}, migration(), "[[::]:2230] configured, not bound", "", nil},
		{"a port change where sshd -T is not what sshd listens on", debian13, func(v *vm) {
			v.f.AddFile(sshd.MainConfig, []byte(stockMain+"Port 2231\n"), 0o644, 0, 0)
		}, port2230(), "this session is connected to 192.0.2.20:22", "", nil},
		{"a port change from a session whose connection is not known", debian13, func(v *vm) { v.conn = "" },
			port2230(), "the port this session is connected to is not established", "", nil},
		// A reload of ssh.service or OpenRC's sshd rereads every file, so a
		// change with no listener keyword still moves sshd to a port its
		// files name and it has not bound (next/p3-steps review).
		{"keyonly where sshd_config names a port sshd has not read, Debian 13", debian13, func(v *vm) {
			v.f.AddFile(sshd.MainConfig, []byte(stockMain+"Port 2230\n"), 0o644, 0, 0)
		}, keyOnly(), "this session is connected to 192.0.2.20:22", "", reload.ErrNotInEffect},
		{"keyonly where sshd_config names a port sshd has not read, Alpine 3.24", alpine324(), func(v *vm) {
			v.f.AddFile(detect.OpenRCStarted+"/sshd", nil, 0o644, 0, 0)
			v.f.AddFile(sshd.MainConfig, []byte(stockMain+"Port 2230\n"), 0o644, 0, 0)
		}, keyOnly(), "this session is connected to 192.0.2.20:22", "", reload.ErrNotInEffect},
		{"keyonly where the listeners are not established (ssh.service not up)", sshServiceDown(), nil,
			keyOnly(), "which the reload (ssh.service) makes it bind", "", reload.ErrListenersUnknown},
		// The file saying so is not the host being so (brief §3 principle
		// 3): sshd reads another drop-in's line first.
		{"keyonly where 00-hardn.conf says so and another drop-in sets it first", debian13, func(v *vm) {
			v.f.AddFile(sshd.DropInDir+"/00-aaa.conf", []byte("PasswordAuthentication yes\n"), 0o644, 0, 0)
			v.f.AddFile(sshd.HardnDropIn, []byte("# Managed by hardn.\n# Set by hardn:\nPasswordAuthentication no\nKbdInteractiveAuthentication no\n"), 0o644, 0, 0)
			v.boot()
		}, keyOnly(), `set first by /etc/ssh/sshd_config.d/00-aaa.conf:1 "PasswordAuthentication yes"`, "", steps.ErrNotInEffect},
		// A port in 00-hardn.conf that sshd has not bound is not "already
		// there": p2drive refused it, and the engine does too.
		{"a port 00-hardn.conf names and sshd has not bound, with another directive", debian13, func(v *vm) {
			v.f.AddFile(sshd.HardnDropIn, []byte("# Managed by hardn.\nPort 2230\n"), 0o644, 0, 0)
		}, sshControl(steps.SSH{Port: 2230, Set: []migrate.Directive{{Keyword: "PasswordAuthentication", Args: []string{"no"}}}}),
			"port 2230 is configured, not in effect", "", reload.ErrNotInEffect},
		{"a port 00-hardn.conf names beside 22 and sshd has not bound", debian13, func(v *vm) {
			v.f.AddFile(sshd.HardnDropIn, []byte("# Managed by hardn.\nPort 22\nPort 2230\n"), 0o644, 0, 0)
		}, port2230(), "[[::]:2230 0.0.0.0:2230] configured, not bound", "", reload.ErrNotInEffect},
		{"a port on disk ssh.socket has not read (no daemon-reload)", ubuntu2604(), func(v *vm) {
			v.socket = true
			v.f.AddFile(detect.SocketGenerator, []byte("ELF"), 0o755, 0, 0)
			v.f.AddFile(sshd.HardnDropIn, []byte("# Managed by hardn.\nPort 22\nPort 2230\n"), 0o644, 0, 0)
		}, port2230(), "the listeners sshd configures now", "", reload.ErrListenersUnknown},
		{"sshd -T without /run/sshd (Ubuntu before its first connection)", ubuntu2604(), func(v *vm) {
			v.hook = func(cmd string) (hostfake.Response, bool) {
				return hostfake.Response{Stderr: "Missing privilege separation directory: /run/sshd\r\n", ExitCode: 255},
					strings.HasPrefix(cmd, "sshd -T")
			}
		}, keyOnly(), "open an SSH connection to this host", steps.CodeNoRunDir, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVM(t)
			if tc.setup != nil {
				tc.setup(v)
			}
			p, _, err := v.viaEngine(context.Background(), tc.facts, tc.c)
			if p == nil || !errors.Is(err, engine.ErrRefused) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want refused at plan time with %q", err, tc.want)
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Errorf("err = %v, want %v", err, tc.is)
			}
			var r *steps.Refusal
			if got := errors.As(err, &r); got != (tc.code != "") || got && r.Code != tc.code {
				t.Errorf("refusal %+v, want code %q", r, tc.code)
			}
			// v.changes counts a read by sshd's full path (Alpine's) as a change.
			read := func(c string) bool { return strings.HasPrefix(c, "run /usr/sbin/sshd -") }
			if ch := slices.DeleteFunc(v.changes(), read); len(ch) > 0 {
				t.Errorf("changes before the refusal: %q", ch)
			}
			if v.index(func(c hostfake.Call) bool { return c.Op == "lock" }) >= 0 {
				t.Error("the run lock was taken for a refused plan")
			}
		})
	}
}

// TestEngineRollsBack: a failed step after the timer rolls the change back
// at once, under the signal hold, and leaves the host as it was: a reload
// sshd refuses; sshd -T not showing the values; Ctrl-C during the reload,
// which leaves ssh.service down (the reload journalled without IfUp under
// a context of its own, so the rollback starts it).
func TestEngineRollsBack(t *testing.T) {
	for _, tc := range []struct {
		name  string
		c     engine.Control
		setup func(v *vm, cancel context.CancelFunc)
		want  error
	}{
		{"the reload refused", port2230(), func(v *vm, _ context.CancelFunc) { v.failReloads = 1 }, engine.ErrRolledBack},
		{"sshd -T does not show the values", keyOnly(), func(v *vm, _ context.CancelFunc) {
			v.hook = func(cmd string) (hostfake.Response, bool) {
				v.stale = v.stale || cmd == "systemctl reload ssh.service"
				return hostfake.Response{}, false
			}
		}, steps.ErrNotInEffect},
		{"Ctrl-C during the reload", port2230(), func(v *vm, cancel context.CancelFunc) {
			v.hook = func(cmd string) (hostfake.Response, bool) {
				if cmd != "systemctl reload ssh.service" || v.down {
					return hostfake.Response{}, false
				}
				cancel()
				v.down = true
				return hostfake.Response{Stderr: "signal: interrupt\n", ExitCode: -1}, true
			}
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVM(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tc.setup(v, cancel)
			_, res, err := v.viaEngine(ctx, debian13, tc.c)
			if !errors.Is(err, engine.ErrRolledBack) || tc.want != nil && !errors.Is(err, tc.want) || res.Failed == nil {
				t.Fatalf("err = %v, want rolled back with %v", err, tc.want)
			}
			if tc.want == nil && v.index(runIs("systemctl start ssh.service")) < 0 {
				t.Error("the rollback did not start the ssh.service the interrupted reload left down")
			}
			if v.dropIn() != "" || !slices.Equal(v.allowed, []uint16{22}) || v.down {
				t.Errorf("00-hardn.conf %q, ufw %v, ssh.service down %v; want as before", v.dropIn(), v.allowed, v.down)
			}
			if !sameStrings(v.bound, []string{"[::]:22", "0.0.0.0:22"}) {
				t.Errorf("sshd listens on %v", v.bound)
			}
			v.cleared()
			stopped := v.index(func(c hostfake.Call) bool { return c.Op == "run" && c.Argv[0] == "systemctl" && c.Argv[1] == "stop" })
			if stopped < 0 || !(v.held >= 0 && v.held < stopped && stopped < v.released) {
				t.Errorf("signals held from call %d to %d, the timer stopped at %d", v.held, v.released, stopped)
			}
		})
	}
}

// TestEngineMigrate: the R8 migration through the engine, on Debian and on
// Ubuntu's ssh.socket. The engine backs up Plan.Sources and journals them
// before migrate.Apply, which takes those backups (Options.Taken): the run
// holds one copy of each file, not p2drive's two. On the socket host the
// journal's reload names listen.conf as restored and holds the socket's
// listener before the change.
func TestEngineMigrate(t *testing.T) {
	t.Run("Debian 13", func(t *testing.T) {
		v := v032Debian(t)
		if _, _, err := v.viaEngine(context.Background(), debian13, migration()); err != nil {
			t.Fatal(err)
		}
		j := v.journal(revert.PendingName)
		if j.Applied == nil || j.Backup == nil || !slices.Equal(j.Backup.Seqs, []int{1, 2, 3}) || len(j.Reload) != 1 || j.Reload[0].IfUp {
			t.Errorf("journal applied %v, backup %+v, reload %+v", j.Applied != nil, j.Backup, j.Reload)
		}
		m, err := backup.Load(context.Background(), v.f, j.Backup.Dir)
		if err != nil || len(m.Entries) != 3 {
			t.Errorf("the run holds %d backups (%v), want 3: migrate.Apply took the engine's", len(m.Entries), err)
		}
		if _, ok := v.f.Entry(migrate.ManageConf); ok || !strings.Contains(v.dropIn(), "ListenAddress 0.0.0.0:2230\n") {
			t.Errorf("not migrated:\n%s", v.dropIn())
		}
	})
	t.Run("Ubuntu 26.04, ssh.socket", func(t *testing.T) {
		v := v032Ubuntu(t)
		if _, _, err := v.viaEngine(context.Background(), ubuntu2604(), migration()); err != nil {
			t.Fatal(err)
		}
		j := v.journal(revert.PendingName)
		if j.Applied == nil || len(j.Reload) != 1 || j.Reload[0].IfUp || !slices.Equal(j.Reload[0].SocketDropIns, []string{migrate.ListenConf}) ||
			len(j.Reload[0].After) != 1 || j.Reload[0].After[0].String() != "[::]:2230" {
			t.Errorf("journal applied %v, reload %+v", j.Applied != nil, j.Reload)
		}
		if _, ok := v.f.Entry(migrate.ListenConf); ok || !sameStrings(v.bound, []string{"0.0.0.0:2230", "[::]:2230"}) {
			t.Errorf("listen.conf left, or ssh.socket on %v", v.bound)
		}
	})
	t.Run("Ubuntu 26.04, sshd -T not showing the migration: rolled back", func(t *testing.T) {
		v := v032Ubuntu(t)
		v.hook = func(cmd string) (hostfake.Response, bool) {
			v.stale = v.stale || cmd == "systemctl restart ssh.socket"
			return hostfake.Response{}, false
		}
		if _, _, err := v.viaEngine(context.Background(), ubuntu2604(), migration()); !errors.Is(err, engine.ErrRolledBack) {
			t.Fatalf("err = %v, want rolled back", err)
		}
		if _, ok := v.f.Entry(migrate.ListenConf); !ok || v.dropIn() != "" || !sameStrings(v.bound, []string{"[::]:2230"}) {
			t.Errorf("listen.conf restored %v, 00-hardn.conf %q, ssh.socket on %v", ok, v.dropIn(), v.bound)
		}
		v.cleared()
	})
}

// TestEnginePVERoot: PermitRootLogin prohibit-password on Proxmox VE
// through pve.PlanSSH over the whole of 00-hardn.conf, and pve.CheckSSH,
// whose three answers must each be yes, over the candidate at plan time
// and over sshd -T after the reload.
func TestEnginePVERoot(t *testing.T) {
	pveMain := sshd.IncludeLine + "\n\nPermitRootLogin yes\nUsePAM yes\n"
	const site = sshd.DropInDir + "/50-site.conf"
	for _, tc := range []struct {
		name  string
		login string // the PermitRootLogin asked for
		files map[string]string
		// between, when set, changes the host once the reload ran.
		between func(v *vm)
		want    error  // nil: applied
		dirs    string // the directives at the end, when applied
	}{
		{"stock", "prohibit-password", nil, nil, nil, "PermitRootLogin prohibit-password"},
		// pve.PlanSSH writes prohibit-password for no (brief 2.5), and the
		// check after the reload reads what was written, not what was asked.
		{"no asked: key-only written and checked", "no", nil, nil, nil, "PermitRootLogin prohibit-password"},
		{"a kept AllowUsers gets root", "prohibit-password", map[string]string{sshd.HardnDropIn: "# Managed by hardn.\n# From /etc/ssh/sshd_config.d/manage.conf:\nAllowUsers alice\nPubkeyAuthentication yes\n"},
			nil, nil, "PermitRootLogin prohibit-password\nAllowUsers alice root\nPubkeyAuthentication yes"},
		// Foreseeable from the candidate: refused before the timer.
		{"another drop-in keeps root out", "prohibit-password", map[string]string{site: "AllowUsers alice\n"}, nil, steps.ErrProxmoxRoot, ""},
		// Not foreseeable: the check after the reload rolls it back.
		{"a drop-in that keeps root out, written after the reload", "prohibit-password", nil, func(v *vm) {
			v.f.AddFile(site, []byte("AllowUsers alice\n"), 0o644, 0, 0)
		}, engine.ErrRolledBack, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVM(t)
			v.f.AddFile(sshd.MainConfig, []byte(pveMain), 0o644, 0, 0)
			for p, d := range tc.files {
				v.f.AddFile(p, []byte(d), 0o644, 0, 0)
			}
			v.boot()
			if tc.between != nil {
				v.hook = func(cmd string) (hostfake.Response, bool) {
					if cmd == "systemctl reload ssh.service" {
						tc.between(v)
					}
					return hostfake.Response{}, false
				}
			}
			old := v.directives()
			root := sshControl(steps.SSH{Set: []migrate.Directive{{Keyword: "PermitRootLogin", Args: []string{tc.login}}}})
			_, _, err := v.viaEngine(context.Background(), proxmox9(), root)
			switch {
			case tc.want == nil && err != nil:
				t.Fatal(err)
			case tc.want != nil && (!errors.Is(err, tc.want) || !strings.Contains(err.Error(), "root by key no")):
				t.Fatalf("err = %v, want %v: root by key no", err, tc.want)
			case errors.Is(tc.want, steps.ErrProxmoxRoot):
				if !errors.Is(err, engine.ErrRefused) || v.index(armed) >= 0 {
					t.Errorf("err = %v, armed at %d; want refused at plan time", err, v.index(armed))
				}
				tc.dirs = old
			case tc.want != nil:
				tc.dirs = old
				v.cleared()
			}
			if got := v.directives(); got != tc.dirs {
				t.Errorf("00-hardn.conf's directives %q, want %q", got, tc.dirs)
			}
			if v.index(func(c hostfake.Call) bool { return c.Op == "run" && c.Argv[0] == "ufw" }) >= 0 {
				t.Error("ufw was run on Proxmox VE")
			}
		})
	}
}

// TestEngineSudoersRefused: visudo -c refusing the installed configuration
// before any write: sudoers.Apply wrote nothing, so the plan step leaves the
// journal before the rollback, which only ends the change (TestSudoersRollsBack).
func TestEngineSudoersRefused(t *testing.T) {
	v := newVM(t)
	v.hook = func(cmd string) (hostfake.Response, bool) {
		return hostfake.Response{Stderr: ">>> /etc/sudoers: syntax error near line 2 <<<\n", ExitCode: 1}, cmd == "visudo -c"
	}
	var rolling *revert.Journal
	_, res, err := v.viaEngine(context.Background(), debian13, sudoersGrant())
	if !errors.Is(err, engine.ErrRolledBack) || !errors.Is(err, sudoers.ErrRejected) {
		t.Fatalf("err = %v, want rolled back after visudo's refusal", err)
	}
	if res.Failed.Rollback.Journal.ID != "" {
		j := res.Failed.Rollback.Journal
		rolling = &j
	}
	if rolling == nil || len(rolling.Undo) != 0 {
		t.Errorf("the journal the rollback started from: %+v, want no plan step", rolling)
	}
	if _, ok := v.f.Entry("/etc/sudoers.d/hardn-grant-alice"); ok {
		t.Error("the grant is left")
	}
	v.cleared()
}

// TestEngineCleanup: once the 2230 change is confirmed, the clean-up is a
// change of its own: port 22 dropped from 00-hardn.conf, the reload with
// reload.Change.Confirmed, and the ufw rule for 22 deleted only where hardn
// added it; planned again, it is empty.
func TestEngineCleanup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(*vm)
		c       engine.Control
		allowed []uint16 // ufw's rules at the end
	}{
		{"ufw allowed 22 already: its rule stays", nil, port2230(), []uint16{22, 2230}},
		{"hardn allowed 22: its rule goes", func(v *vm) {
			v.allowed, v.inactive = nil, true
			v.saveRules()
		}, sshControl(steps.SSH{Port: 2230, UFW: steps.UFWEnable}), []uint16{2230}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVM(t)
			if tc.setup != nil {
				tc.setup(v)
			}
			ctx := context.Background()
			if _, _, err := v.viaEngine(ctx, debian13, tc.c); err != nil {
				t.Fatal(err)
			}
			// hardn confirm's claim, from a new connection (revert.md §2.5).
			j := v.journal(revert.PendingName)
			if err := v.f.Rename(ctx, path.Join(revert.DefaultDir, revert.PendingName), path.Join(revert.DefaultDir, revert.ConfirmedName)); err != nil {
				t.Fatal(err)
			}
			c := steps.CleanupOf(j)
			if !slices.Equal(c.Drop, []uint16{22}) {
				t.Fatalf("CleanupOf = %+v, want port 22 dropped", c)
			}
			cleanup := stepsControl{"ssh", func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
				return steps.PlanCleanup(ctx, sys, in, "ssh", c)
			}}
			v.conn = "192.0.2.10 50023 192.0.2.20 2230"
			if _, _, err := v.viaEngine(ctx, debian13, cleanup); err != nil {
				t.Fatal(err)
			}
			if got := v.directives(); got != "Port 2230" {
				t.Errorf("00-hardn.conf's directives %q, want Port 2230 alone", got)
			}
			if !sameStrings(v.bound, []string{"[::]:2230", "0.0.0.0:2230"}) || !slices.Equal(v.allowed, tc.allowed) {
				t.Errorf("sshd on %v, ufw %v; want 2230 alone, ufw %v", v.bound, v.allowed, tc.allowed)
			}
			if j := v.journal(revert.PendingName); j.Applied == nil || len(j.ConfirmVia) != 0 {
				t.Errorf("the clean-up's journal %+v: want applied, under confirm-or-revert", j)
			}
			if err := v.f.Remove(ctx, path.Join(revert.DefaultDir, revert.PendingName)); err != nil {
				t.Fatal(err)
			}
			p, _, err := v.viaEngine(ctx, debian13, cleanup)
			if err != nil || !p.Empty() {
				t.Errorf("planned again: %v, %d changes; want none", err, len(p.Changes))
			}
		})
	}
}

// TestEngineIdempotent: once a change is applied and confirmed, the same
// control plans nothing (brief Phase 3: the second plan is empty).
func TestEngineIdempotent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*vm)
		c     engine.Control
	}{
		{"2230", nil, port2230()},
		{"keyonly", nil, keyOnly()},
		{"ufw-enable", func(v *vm) {
			v.allowed, v.inactive = nil, true
			v.saveRules()
		}, ufwEnableControl()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVM(t)
			if tc.setup != nil {
				tc.setup(v)
			}
			ctx := context.Background()
			if _, _, err := v.viaEngine(ctx, debian13, tc.c); err != nil {
				t.Fatal(err)
			}
			if err := v.f.Rename(ctx, path.Join(revert.DefaultDir, revert.PendingName), path.Join(revert.DefaultDir, revert.ConfirmedName)); err != nil {
				t.Fatal(err)
			}
			from := len(v.f.Calls())
			p, res, err := v.viaEngine(ctx, debian13, tc.c)
			if err != nil || !p.Empty() || !res.Nothing {
				t.Fatalf("planned again: %v, %d changes", err, len(p.Changes))
			}
			for _, c := range v.f.Calls()[from:] {
				if c.Op == "lock" || c.Op == "write-atomic" && !strings.HasPrefix(c.Path, runRoot+"/") {
					t.Errorf("the second plan changed the host: %s", c)
				}
			}
		})
	}
}

// TestEngineCombined: controls whose planners read and change the same
// thing cannot share a plan (engine.ErrConflict, at plan time, before the
// timer): two SSH changes would write 00-hardn.conf from the same old
// content, and ufw's enable planned beside a port change would leave the
// new port out of ufw. The SSH settings and ufw's enable of one plan go
// through one PlanSSH, which plans the enable also when nothing else is
// left to do; a sudoers grant shares a plan with an SSH change.
func TestEngineCombined(t *testing.T) {
	keyonlyEnable := sshControl(steps.SSH{UFW: steps.UFWEnable, Set: []migrate.Directive{{Keyword: "PasswordAuthentication", Args: []string{"no"}},
		{Keyword: "KbdInteractiveAuthentication", Args: []string{"no"}}}})
	inactive := func(v *vm) {
		v.allowed, v.inactive = nil, true
		v.saveRules()
	}
	cleanup := stepsControl{"ssh.cleanup", func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		return steps.PlanCleanup(ctx, sys, in, "ssh.cleanup", steps.Cleanup{Drop: []uint16{22}})
	}}
	held := func(v *vm) {
		v.f.AddFile(sshd.HardnDropIn, []byte("# Managed by hardn.\n# Set by hardn:\nPort 22\nPort 2230\n"), 0o644, 0, 0)
		v.boot()
	}
	for _, tc := range []struct {
		name    string
		setup   func(*vm)
		cs      []engine.Control
		refused string   // "": applied
		changes []string // the plan's, when applied
		active  bool     // ufw active at the end
	}{
		{"two SSH controls", nil, []engine.Control{keyOnly(), stepsControl{"ssh.port", func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
			return steps.PlanSSH(ctx, sys, in, steps.SSH{Prefix: "ssh.port", Port: 2230})
		}}}, `both back up /etc/ssh/sshd_config.d/00-hardn.conf`, nil, true},
		{"ufw's enable beside a port change", inactive, []engine.Control{ufwEnableControl(), port2230()},
			`controls "ufw" and "ssh" both plan changes of sshd and ufw`, nil, false},
		{"the clean-up beside an SSH change", held, []engine.Control{cleanup, keyOnly()},
			`controls "ssh.cleanup" and "ssh" both plan changes of sshd and ufw`, nil, true},
		{"keyonly and ufw's enable through one PlanSSH", inactive, []engine.Control{keyonlyEnable}, "",
			[]string{"ssh/firewall", "ssh/files", "ssh/reload", "ssh/verify"}, true},
		{"ufw's enable through PlanSSH, the SSH settings in place already", func(v *vm) {
			inactive(v)
			v.f.AddFile(sshd.HardnDropIn, []byte("# Managed by hardn.\n# Set by hardn:\nPasswordAuthentication no\nKbdInteractiveAuthentication no\n"), 0o644, 0, 0)
			v.boot()
		}, []engine.Control{keyonlyEnable}, "", []string{"ssh/firewall"}, true},
		{"a sudoers grant beside keyonly", nil, []engine.Control{sudoersGrant(), keyOnly()}, "",
			[]string{"sudoers/alice", "ssh/files", "ssh/reload", "ssh/verify"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVM(t)
			if tc.setup != nil {
				tc.setup(v)
			}
			p, _, err := v.viaEngine(context.Background(), debian13, tc.cs...)
			if tc.refused != "" {
				if p == nil || !errors.Is(err, engine.ErrRefused) || !errors.Is(err, engine.ErrConflict) || !strings.Contains(err.Error(), tc.refused) {
					t.Fatalf("err = %v, want refused at plan time with %q", err, tc.refused)
				}
				if ch := v.changes(); len(ch) > 0 {
					t.Errorf("changes before the refusal: %q", ch)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, c := range p.Changes {
				ids = append(ids, c.ID)
			}
			if !slices.Equal(ids, tc.changes) {
				t.Errorf("changes %q, want %q", ids, tc.changes)
			}
			if v.inactive == tc.active || tc.active && !slices.Contains(v.allowed, 22) {
				t.Errorf("ufw inactive %v, allowed %v; want active %v with 22", v.inactive, v.allowed, tc.active)
			}
		})
	}
}

// TestEngineAlreadyAsked: a host already as asked plans nothing, decided
// from sshd -T and what is bound, not from 00-hardn.conf's bytes: a
// migrated file with the lines under its own label, and a port configured
// and in effect.
func TestEngineAlreadyAsked(t *testing.T) {
	for _, tc := range []struct {
		name string
		file string
		c    engine.Control
	}{
		{"a migrated 00-hardn.conf with keyonly's lines under its own label",
			"# Managed by hardn.\n# From /etc/ssh/sshd_config.d/manage.conf:\nPasswordAuthentication no\nKbdInteractiveAuthentication no\n", keyOnly()},
		{"port 2230 configured and in effect", "# Managed by hardn.\n# Set by hardn:\nPort 22\nPort 2230\n", port2230()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVM(t)
			v.f.AddFile(sshd.HardnDropIn, []byte(tc.file), 0o644, 0, 0)
			v.boot()
			p, res, err := v.viaEngine(context.Background(), debian13, tc.c)
			if err != nil || !p.Empty() || !res.Nothing {
				t.Fatalf("plan: %v, %d changes; want none", err, len(p.Changes))
			}
			if v.dropIn() != tc.file {
				t.Errorf("00-hardn.conf rewritten: %q", v.dropIn())
			}
		})
	}
}
