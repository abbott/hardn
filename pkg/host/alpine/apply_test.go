package alpine_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
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
	r, err := backup.Open(w.h, backup.Settings{Path: backup.DefaultPath, Enabled: enabled}, runID)
	if err != nil {
		t.Fatal(err)
	}
	return r
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

// TestApplyFails: whichever step fails, by an error or by succeeding
// without its effect, the steps that ran are undone, last first, and the
// host reads as it did before.
func TestApplyFails(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(w *world)
		want  error
		undo  []string // the commands after the failure
	}{
		{"apk add fails", func(w *world) {
			w.h.Respond(hostfake.Response{Stderr: "ERROR: unable to select packages\n", ExitCode: 1}, "apk", "add", "apk-cron")
		}, nil, nil},
		{"apk add has no effect", func(w *world) { w.inert["apk add apk-cron"] = true }, alpine.ErrNotEffective, nil},
		{"no default runlevel", func(w *world) {
			_ = w.h.RemoveAll(context.Background(), detect.OpenRCRunlevels+"/default")
		}, alpine.ErrNotEffective, []string{"apk del apk-cron"}},
		{"crond start fails", func(w *world) {
			w.h.Respond(hostfake.Response{Stderr: " * ERROR: crond failed to start\n", ExitCode: 1}, "rc-service", "crond", "start")
		}, nil, []string{"rc-update del crond default", "apk del apk-cron"}},
		{"crond starts and dies", func(w *world) { w.inert["rc-service crond start"] = true }, alpine.ErrNotEffective,
			[]string{"rc-update del crond default", "apk del apk-cron"}},
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
			res, err := alpine.Apply(context.Background(), w.h, p, alpine.Options{Backups: backups(t, w, true)})
			var exit *host.ExitError
			if err == nil || tc.want != nil && !errors.Is(err, tc.want) || tc.want == nil && !errors.As(err, &exit) {
				t.Fatalf("Apply = %v, want %v", err, tc.want)
			}
			if !res.Restored || errors.Is(err, alpine.ErrNotRestored) {
				t.Errorf("Restored = %v, err %v", res.Restored, err)
			}
			if after := w.read(t, f); after.Package != before.Package || after.Script != before.Script || after.Runlevel != before.Runlevel ||
				after.Crond != before.Crond || after.Legacy != alpine.LegacyHardn {
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

// TestApplyRefuses: before anything changes, Apply refuses a plan that
// removes the legacy script without a backup run, a host that changed
// since the plan, and a plan its state does not give.
func TestApplyRefuses(t *testing.T) {
	f := facts(t, "3.24")
	for _, tc := range []struct {
		name   string
		mutate func(w *world, p *alpine.Plan)
		opts   bool
		want   error
	}{
		{"no backup run", func(*world, *alpine.Plan) {}, false, alpine.ErrNoBackup},
		{"legacy edited", func(w *world, _ *alpine.Plan) {
			w.h.AddFile(alpine.LegacyScript, []byte(alpine.LegacyContent+"# local\n"), 0o755, 0, 0)
		}, true, alpine.ErrChanged},
		{"installed meanwhile", func(w *world, _ *alpine.Plan) {
			w.installed = true
			w.h.AddFile(alpine.Script, []byte(apkCron), 0o755, 0, 0)
		}, true, alpine.ErrChanged},
		{"argv edited", func(_ *world, p *alpine.Plan) {
			p.Changes[0].Argv = []string{"apk", "add", "--allow-untrusted", "evil"}
		}, true, fs.ErrInvalid},
		{"steps reordered", func(_ *world, p *alpine.Plan) { slices.Reverse(p.Changes) }, true, fs.ErrInvalid},
		{"step added", func(_ *world, p *alpine.Plan) {
			p.Changes = append(p.Changes, alpine.Change{Step: alpine.StepStart, Argv: []string{"rc-service", "crond", "start"}, Undo: []string{"rc-service", "crond", "stop"}})
		}, true, fs.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := stock(t, true)
			p := w.plan(t, f)
			tc.mutate(w, &p)
			var opts alpine.Options
			if tc.opts {
				opts.Backups = backups(t, w, true)
			}
			m := w.mark()
			_, err := alpine.Apply(context.Background(), w.h, p, opts)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Apply = %v, want %v", err, tc.want)
			}
			if got := w.changes(m); len(got) != 0 {
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
