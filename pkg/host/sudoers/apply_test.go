package sudoers_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

var opts = sudoers.Options{RunDir: runDir}

const (
	grantFile = sudoers.DropInDir + "/hardn-grant-alice"
	envFile   = sudoers.DropInDir + "/hardn-env-alice"
	oldGrant  = "# Managed by hardn\nalice ALL=(ALL) ALL\n"
	newGrant  = "# Managed by hardn\nalice ALL=(ALL) NOPASSWD: ALL\n"
	envLine   = "# Managed by hardn\nDefaults:alice env_keep += \"HARDN_CONFIG\"\n"
)

var alicePolicy = sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword, Env: keep}

// TestApplySudoFlavours: sudo and sudo-rs take the same steps, in order:
// the whole configuration, each candidate as a copy, the writes (0440,
// root:root), the whole configuration again. Nothing under /etc is written
// before every check passed, and the copies are removed.
func TestApplySudoFlavours(t *testing.T) {
	want := []string{
		"visudo -c",
		"visudo -cf " + work + "/hardn-grant-alice",
		"visudo -cf " + work + "/hardn-env-alice",
		"visudo -c",
	}
	wantWrites := []string{
		"mkdir " + work,
		"write-atomic " + work + "/hardn-grant-alice",
		"write-atomic " + work + "/hardn-env-alice",
		"write-atomic " + grantFile,
		"write-atomic " + envFile,
		"removeall " + work,
	}
	for _, c := range []sudoers.Choice{sudoChoice, rsChoice} {
		t.Run(c.Flavor.String(), func(t *testing.T) {
			h := sudoHost(t)
			_, p := plan(t, h, c, alicePolicy)
			res, err := sudoers.Apply(context.Background(), h, p, opts)
			if err != nil || res.Cleanup != nil {
				t.Fatalf("Apply = %v, cleanup %v", err, res.Cleanup)
			}
			if got := runs(h); !slices.Equal(got, want) {
				t.Errorf("runs = %q\nwant   %q", got, want)
			}
			if got := writes(h); !slices.Equal(got, wantWrites) {
				t.Errorf("writes = %q\nwant     %q", got, wantWrites)
			}
			if !slices.Equal(res.Installed, []string{grantFile, envFile}) {
				t.Errorf("Installed = %q", res.Installed)
			}
			checkFile(t, h, grantFile, newGrant, 0o440)
			checkFile(t, h, envFile, envLine, 0o440)
			checkFile(t, h, sudoers.DropInDir+"/90-cloud-init-users", cloudInit, 0o440)
			checkAbsent(t, h, work)
			// The candidates were copies of exactly what was installed, with
			// its mode and owner.
			for _, call := range h.Calls() {
				if call.Op == "write-atomic" && strings.HasPrefix(call.Path, work) && (call.Mode != sudoers.SudoMode || call.UID != 0 || call.GID != 0) {
					t.Errorf("candidate %s", call)
				}
			}
		})
	}
}

// TestApplyRejected: whatever check fails, nothing under /etc is left
// changed.
func TestApplyRejected(t *testing.T) {
	visudoC := []string{"visudo", "-c"}
	for _, tc := range []struct {
		name  string
		setup func(*hostfake.System, *sudoers.Plan)
		stage sudoers.Stage
		path  string
		runs  []string
	}{
		{"before: the host's configuration is already invalid", func(h *hostfake.System, _ *sudoers.Plan) {
			h.AddFile(sudoers.DropInDir+"/90-cloud-init-users", []byte(bad), 0o440, 0, 0)
		}, sudoers.StageBefore, sudoers.MainConfig, []string{"visudo -c"}},
		{"candidate rejected", func(_ *hostfake.System, p *sudoers.Plan) {
			p.Changes[1].Data = []byte(bad)
		}, sudoers.StageCandidate, envFile, []string{"visudo -c", "visudo -cf " + work + "/hardn-grant-alice", "visudo -cf " + work + "/hardn-env-alice"}},
		{"after: rejected once installed; restored", func(h *hostfake.System, _ *sudoers.Plan) {
			h.Respond(hostfake.Response{}, visudoC[0], visudoC[1:]...)
			h.Respond(hostfake.Response{Stderr: "visudo: /etc/sudoers.d/hardn-grant-alice: syntax error\n", ExitCode: 1}, visudoC[0], visudoC[1:]...)
			h.Respond(hostfake.Response{}, visudoC[0], visudoC[1:]...)
		}, sudoers.StageAfter, sudoers.MainConfig, []string{"visudo -c", "visudo -cf " + work + "/hardn-grant-alice", "visudo -cf " + work + "/hardn-env-alice", "visudo -c", "visudo -c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := sudoHost(t)
			h.AddFile(grantFile, []byte(oldGrant), 0o440, 0, 0)
			_, p := plan(t, h, sudoChoice, alicePolicy)
			tc.setup(h, &p)
			res, err := sudoers.Apply(context.Background(), h, p, opts)
			var ce *sudoers.CheckError
			if !errors.As(err, &ce) || !errors.Is(err, sudoers.ErrRejected) || ce.Stage != tc.stage || ce.Path != tc.path || len(ce.Tail) == 0 {
				t.Fatalf("Apply = %v (%+v), want a %s rejection of %s", err, ce, tc.stage, tc.path)
			}
			if res.Restored != (tc.stage == sudoers.StageAfter) || errors.Is(err, sudoers.ErrNotRestored) {
				t.Errorf("Restored = %t, err %v", res.Restored, err)
			}
			if got := runs(h); !slices.Equal(got, tc.runs) {
				t.Errorf("runs = %q\nwant   %q", got, tc.runs)
			}
			checkFile(t, h, grantFile, oldGrant, 0o440)
			checkAbsent(t, h, envFile, work)
			if tc.stage != sudoers.StageAfter {
				for _, w := range writes(h) {
					if strings.Contains(w, sudoers.DropInDir) {
						t.Errorf("wrote %s", w)
					}
				}
			}
		})
	}
}

// TestApplyRestoreFails: when the configuration put back does not check
// either, both failures are returned, and the restore is not complete.
func TestApplyRestoreFails(t *testing.T) {
	h := sudoHost(t)
	_, p := plan(t, h, sudoChoice, alicePolicy)
	h.Respond(hostfake.Response{}, "visudo", "-c")
	h.Respond(hostfake.Response{Stderr: "syntax error\n", ExitCode: 1}, "visudo", "-c")
	res, err := sudoers.Apply(context.Background(), h, p, opts)
	got := stages(err)
	if !errors.Is(err, sudoers.ErrNotRestored) || res.Restored || !slices.Equal(got, []sudoers.Stage{sudoers.StageAfter, sudoers.StageRestored}) {
		t.Fatalf("Apply = %v, Restored %t, stages %v", err, res.Restored, got)
	}
	checkAbsent(t, h, grantFile, envFile)
}

// TestApplyRestoreWriteFails: a file that cannot be put back is an error
// wrapping ErrNotRestored, and the restored configuration is not checked
// as though it were back.
func TestApplyRestoreWriteFails(t *testing.T) {
	h := sudoHost(t)
	h.AddFile(grantFile, []byte(oldGrant), 0o440, 0, 0)
	_, p := plan(t, h, sudoChoice, alicePolicy)
	h.Respond(hostfake.Response{}, "visudo", "-c")
	h.Respond(hostfake.Response{Stderr: "syntax error\n", ExitCode: 1}, "visudo", "-c")
	res, err := sudoers.Apply(context.Background(), &failNth{System: h, path: grantFile, n: 2}, p, opts)
	if !errors.Is(err, sudoers.ErrNotRestored) || !errors.Is(err, errDisk) || res.Restored || !slices.Equal(stages(err), []sudoers.Stage{sudoers.StageAfter}) {
		t.Fatalf("Apply = %v, Restored %t", err, res.Restored)
	}
	checkFile(t, h, grantFile, newGrant, 0o440) // its restore write failed
	checkAbsent(t, h, envFile)                  // restored: every file is tried
	if got := runs(h); len(got) != 4 {
		t.Errorf("runs = %q, want no restored check", got)
	}
}

// stages lists the stages of the CheckErrors in err's tree, depth first.
func stages(err error) []sudoers.Stage {
	var out []sudoers.Stage
	var walk func(error)
	walk = func(e error) {
		if ce, ok := e.(*sudoers.CheckError); ok {
			out = append(out, ce.Stage)
			return
		}
		switch u := e.(type) {
		case interface{ Unwrap() []error }:
			for _, x := range u.Unwrap() {
				walk(x)
			}
		case interface{ Unwrap() error }:
			if x := u.Unwrap(); x != nil {
				walk(x)
			}
		}
	}
	if err != nil {
		walk(err)
	}
	return out
}

// failNth fails the nth WriteFileAtomic of path (from 1).
type failNth struct {
	*hostfake.System
	path string
	n    int
	seen int
}

func (f *failNth) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if name == f.path {
		if f.seen++; f.seen == f.n {
			return errDisk
		}
	}
	return f.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

// failWrite fails WriteFileAtomic of one path.
type failWrite struct {
	*hostfake.System
	path string
}

var errDisk = errors.New("no space left on device")

func (f failWrite) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if name == f.path {
		return errDisk
	}
	return f.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

// TestApplyWriteFails: a failed write puts back the files already written.
func TestApplyWriteFails(t *testing.T) {
	h := sudoHost(t)
	h.AddFile(grantFile, []byte(oldGrant), 0o440, 0, 0)
	_, p := plan(t, h, sudoChoice, alicePolicy)
	res, err := sudoers.Apply(context.Background(), failWrite{h, envFile}, p, opts)
	if !errors.Is(err, errDisk) || !res.Restored || errors.Is(err, sudoers.ErrNotRestored) {
		t.Fatalf("Apply = %v, Restored %t", err, res.Restored)
	}
	if !slices.Equal(res.Installed, []string{grantFile}) {
		t.Errorf("Installed = %q", res.Installed)
	}
	checkFile(t, h, grantFile, oldGrant, 0o440)
	checkAbsent(t, h, envFile)
	if got := runs(h); got[len(got)-1] != "visudo -c" || len(got) != 4 {
		t.Errorf("runs = %q", got)
	}
}

// TestApplyChanged: a file changed between Read and Apply is not
// overwritten, and nothing is installed.
func TestApplyChanged(t *testing.T) {
	h := sudoHost(t)
	_, p := plan(t, h, sudoChoice, alicePolicy)
	h.AddFile(envFile, []byte("Defaults:alice env_keep += \"EDITOR\"\n"), 0o440, 0, 0)
	if _, err := sudoers.Apply(context.Background(), h, p, opts); !errors.Is(err, sudoers.ErrChanged) {
		t.Fatalf("Apply = %v, want ErrChanged", err)
	}
	checkAbsent(t, h, grantFile)
	checkFile(t, h, envFile, "Defaults:alice env_keep += \"EDITOR\"\n", 0o440)
}

func TestApplyRefuses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*hostfake.System)
		dir   string
		want  error
	}{
		{"relative run directory", func(*hostfake.System) {}, "run/hardn", fs.ErrInvalid},
		{"group-writable run directory", func(h *hostfake.System) { h.AddDir(runDir, 0o770, 0, 0) }, runDir, sudoers.ErrUnsafeRunDir},
		{"run directory of another user", func(h *hostfake.System) { h.AddDir("/run/hardn", 0o755, 1000, 1000) }, runDir, sudoers.ErrUnsafeRunDir},
		{"run directory through a symlink", func(h *hostfake.System) {
			h.AddDir("/var/hardn", 0o700, 0, 0)
			h.AddSymlink("/var/run", "/run")
		}, "/var/run/hardn", sudoers.ErrUnsafeRunDir},
		{"work directory left over", func(h *hostfake.System) { h.AddDir(work, 0o700, 0, 0) }, runDir, fs.ErrExist},
		{"no run directory", func(*hostfake.System) {}, "/run/other", fs.ErrNotExist},
		{"run directory sshd refuses", func(h *hostfake.System) { h.AddDir("/run/hardn x", 0o700, 0, 0) }, "/run/hardn x", fs.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := sudoHost(t)
			_, p := plan(t, h, sudoChoice, alicePolicy)
			tc.setup(h)
			if _, err := sudoers.Apply(context.Background(), h, p, sudoers.Options{RunDir: tc.dir}); !errors.Is(err, tc.want) {
				t.Errorf("Apply = %v, want %v", err, tc.want)
			}
			if len(h.Runs()) != 0 {
				t.Errorf("runs = %q", runs(h))
			}
			checkAbsent(t, h, grantFile, envFile)
		})
	}
	h := sudoHost(t)
	_, p := plan(t, h, sudoChoice, alicePolicy)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sudoers.Apply(ctx, h, p, opts); !errors.Is(err, context.Canceled) || len(h.Calls()) != len(callsOfRead(h)) {
		t.Errorf("Apply(cancelled) = %v, calls %q", err, h.Calls())
	}
	if _, err := sudoers.Apply(context.Background(), h, sudoers.Plan{Tool: sudoers.ToolSudo}, opts); err != nil || len(h.Runs()) != 0 {
		t.Errorf("Apply(empty) = %v, runs %q", err, runs(h))
	}
}

// callsOfRead: the calls Read made, all reads.
func callsOfRead(h *hostfake.System) []hostfake.Call {
	var out []hostfake.Call
	for _, c := range h.Calls() {
		if c.Op != "run" && !strings.HasPrefix(c.Op, "write") {
			out = append(out, c)
		}
	}
	return out
}

// TestApplyCancelledMidway: a context cancelled during the writes (after
// the first one) still gets the files put back: restore runs under
// context.WithoutCancel.
func TestApplyCancelledMidway(t *testing.T) {
	h := sudoHost(t)
	_, p := plan(t, h, sudoChoice, alicePolicy)
	ctx, cancel := context.WithCancel(context.Background())
	sys := cancelAfter{h, grantFile, cancel}
	if _, err := sudoers.Apply(ctx, sys, p, opts); !errors.Is(err, context.Canceled) {
		t.Fatalf("Apply = %v", err)
	}
	checkAbsent(t, h, grantFile, envFile, work)
	if got := runs(h); got[len(got)-1] != "visudo -c" {
		t.Errorf("runs = %q: no check of the restored files", got)
	}
}

// cancelAfter cancels once path is written.
type cancelAfter struct {
	*hostfake.System
	path   string
	cancel context.CancelFunc
}

func (c cancelAfter) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	err := c.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
	if name == c.path {
		c.cancel()
	}
	return err
}

// TestRollback: Rollback puts the files back as Read found them and checks
// the result; a second Rollback changes nothing.
func TestRollback(t *testing.T) {
	h := sudoHost(t)
	h.AddFile(grantFile, []byte(oldGrant), 0o440, 0, 0)
	_, p := plan(t, h, sudoChoice, alicePolicy)
	if _, err := sudoers.Apply(context.Background(), h, p, opts); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		before := len(writes(h))
		if err := sudoers.Rollback(context.Background(), h, p); err != nil {
			t.Fatalf("Rollback %d: %v", i, err)
		}
		checkFile(t, h, grantFile, oldGrant, 0o440)
		checkAbsent(t, h, envFile)
		if n := len(writes(h)) - before; i == 1 && n != 0 {
			t.Errorf("second Rollback wrote %q", writes(h)[before:])
		}
		if got := runs(h); got[len(got)-1] != "visudo -c" {
			t.Errorf("runs = %q", got)
		}
	}
}

// TestApplyDoas: on Alpine, doas checks each file it reads (doas.conf,
// then doas.d's *.conf), since it has no whole-configuration check; the
// candidate as a copy; and the distribution's 20-wheel.conf is never
// written or removed.
func TestApplyDoas(t *testing.T) {
	h := doasHost(t)
	_, p := plan(t, h, doasChoice, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword, Env: keep})
	if _, err := sudoers.Apply(context.Background(), h, p, opts); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"doas -C /etc/doas.conf", "doas -C /etc/doas.d/20-wheel.conf",
		"doas -C " + work + "/hardn-alice.conf",
		"doas -C /etc/doas.conf", "doas -C /etc/doas.d/20-wheel.conf", "doas -C /etc/doas.d/hardn-alice.conf",
	}
	if got := runs(h); !slices.Equal(got, want) {
		t.Errorf("runs = %q\nwant   %q", got, want)
	}
	checkFile(t, h, "/etc/doas.d/hardn-alice.conf", "# Managed by hardn\npermit persist setenv { HARDN_CONFIG } alice\n", 0o640)
	checkFile(t, h, sudoers.DoasDir+"/20-wheel.conf", wheel, 0o644)
	for _, w := range writes(h) {
		if strings.Contains(w, "20-wheel") || strings.Contains(w, sudoers.DoasConfig) {
			t.Errorf("wrote %s", w)
		}
	}
}

// TestApplyDoasPerms: doas refuses to run when a file it reads is not
// root's alone; so does Apply, before anything is written.
func TestApplyDoasPerms(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed func(*hostfake.System)
		path string
	}{
		{"group-writable", func(h *hostfake.System) { h.AddFile(sudoers.DoasDir+"/20-wheel.conf", []byte(wheel), 0o664, 0, 0) }, sudoers.DoasDir + "/20-wheel.conf"},
		{"another owner", func(h *hostfake.System) { h.AddFile(sudoers.DoasDir+"/zz-local.conf", []byte(wheel), 0o640, 1000, 0) }, sudoers.DoasDir + "/zz-local.conf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := doasHost(t)
			tc.seed(h)
			_, p := plan(t, h, doasChoice, sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword})
			_, err := sudoers.Apply(context.Background(), h, p, opts)
			var ce *sudoers.CheckError
			if !errors.As(err, &ce) || !errors.Is(err, sudoers.ErrPerms) || !errors.Is(err, sudoers.ErrRejected) || ce.Stage != sudoers.StageBefore || ce.Path != tc.path {
				t.Fatalf("Apply = %v", err)
			}
			checkAbsent(t, h, sudoers.DoasDir+"/hardn-alice.conf")
		})
	}
}

// TestCheckErrorNotRejected: a check that could not run (no visudo) is a
// CheckError, but not a rejection.
func TestCheckErrorNotRejected(t *testing.T) {
	h := sudoHost(t)
	_, p := plan(t, h, sudoChoice, alicePolicy)
	h.Respond(hostfake.Response{Err: host.ErrNotFound}, "visudo", "-c")
	_, err := sudoers.Apply(context.Background(), h, p, opts)
	var ce *sudoers.CheckError
	if !errors.As(err, &ce) || errors.Is(err, sudoers.ErrRejected) || !errors.Is(err, host.ErrNotFound) {
		t.Fatalf("Apply = %v", err)
	}
	checkAbsent(t, h, grantFile, envFile)
}

// TestApplyRemove: a plan that removes a grant has no candidate to check,
// and a failed StageAfter puts the grant back with its mode and owner.
func TestApplyRemove(t *testing.T) {
	for _, fail := range []bool{false, true} {
		h := sudoHost(t)
		h.AddFile(grantFile, []byte(oldGrant), 0o400, 0, 42)
		_, p := plan(t, h, sudoChoice, sudoers.Policy{User: "alice"})
		if len(p.Changes) != 1 || !p.Changes[0].Remove || p.Changes[0].Risk != sudoers.RiskLockout {
			t.Fatalf("changes = %+v", p.Changes)
		}
		wantRuns := []string{"visudo -c", "visudo -c"}
		wantWrites := []string{"mkdir " + work, "remove " + grantFile, "removeall " + work}
		if fail {
			h.Respond(hostfake.Response{}, "visudo", "-c")
			h.Respond(hostfake.Response{Stderr: "syntax error\n", ExitCode: 1}, "visudo", "-c")
			h.Respond(hostfake.Response{}, "visudo", "-c") // the restored check
			wantRuns = append(wantRuns, "visudo -c")
			wantWrites = slices.Insert(wantWrites, 2, "write-atomic "+grantFile)
		}
		res, err := sudoers.Apply(context.Background(), h, p, opts)
		switch {
		case !fail && err != nil, fail && (!errors.Is(err, sudoers.ErrRejected) || !res.Restored):
			t.Fatalf("Apply (StageAfter fails: %t) = %v, Restored %t", fail, err, res.Restored)
		case !slices.Equal(res.Installed, []string{grantFile}):
			t.Errorf("Installed = %q", res.Installed)
		}
		if got := runs(h); !slices.Equal(got, wantRuns) {
			t.Errorf("runs = %q\nwant   %q", got, wantRuns)
		}
		if got := writes(h); !slices.Equal(got, wantWrites) {
			t.Errorf("writes = %q\nwant     %q", got, wantWrites)
		}
		e, ok := h.Entry(grantFile)
		switch {
		case !fail && ok:
			t.Error("grant not removed")
		case fail && (!ok || string(e.Data) != oldGrant || e.Mode.Perm() != 0o400 || e.UID != 0 || e.GID != 42):
			t.Errorf("grant put back as %+v", e)
		}
	}
}

// TestApplyDoasRejected: a doas -C rejection at any stage leaves nothing
// under /etc changed: of the distribution's file before; of the candidate;
// of the installed file after, which is then removed again.
func TestApplyDoasRejected(t *testing.T) {
	conf := sudoers.DoasDir + "/hardn-alice.conf"
	wheelConf := sudoers.DoasDir + "/20-wheel.conf"
	for _, tc := range []struct {
		name  string
		setup func(*hostfake.System, *sudoers.Plan)
		stage sudoers.Stage
		path  string
	}{
		{"before", func(h *hostfake.System, _ *sudoers.Plan) {
			h.AddFile(wheelConf, []byte(bad), 0o644, 0, 0)
		}, sudoers.StageBefore, wheelConf},
		{"candidate", func(_ *hostfake.System, p *sudoers.Plan) { p.Changes[0].Data = []byte(bad) }, sudoers.StageCandidate, conf},
		{"after", func(h *hostfake.System, _ *sudoers.Plan) {
			h.Respond(hostfake.Response{Stderr: "doas: " + conf + ":2: syntax error\n", ExitCode: 1}, "doas", "-C", conf)
		}, sudoers.StageAfter, conf},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := doasHost(t)
			_, p := plan(t, h, doasChoice, sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword})
			tc.setup(h, &p)
			res, err := sudoers.Apply(context.Background(), h, p, opts)
			var ce *sudoers.CheckError
			if !errors.As(err, &ce) || !errors.Is(err, sudoers.ErrRejected) || ce.Stage != tc.stage || ce.Path != tc.path {
				t.Fatalf("Apply = %v, want a %s rejection of %s", err, tc.stage, tc.path)
			}
			if res.Restored != (tc.stage == sudoers.StageAfter) || errors.Is(err, sudoers.ErrNotRestored) {
				t.Errorf("Restored = %t, err %v", res.Restored, err)
			}
			checkAbsent(t, h, conf, work)
			if tc.stage == sudoers.StageAfter {
				if got := runs(h); got[len(got)-1] != "doas -C "+wheelConf {
					t.Errorf("runs = %q: no check of the restored files", got)
				}
			}
		})
	}
}

// TestApplyCopyFails: a copy that cannot be written stops Apply before
// anything under /etc changes.
func TestApplyCopyFails(t *testing.T) {
	h := sudoHost(t)
	_, p := plan(t, h, sudoChoice, alicePolicy)
	res, err := sudoers.Apply(context.Background(), failWrite{h, work + "/hardn-grant-alice"}, p, opts)
	if !errors.Is(err, errDisk) || res.Restored || len(res.Installed) != 0 {
		t.Fatalf("Apply = %v, %+v", err, res)
	}
	checkAbsent(t, h, grantFile, envFile, work)
	if got := runs(h); !slices.Equal(got, []string{"visudo -c"}) {
		t.Errorf("runs = %q", got)
	}
}

// TestApplySudoersRS: on sudo-rs with /etc/sudoers-rs, visudo -c checks
// that file, and a rejection of it names it.
func TestApplySudoersRS(t *testing.T) {
	for _, main := range []string{"@includedir /etc/sudoers.d\n", "@includedir /etc/sudoers.d\n" + bad + "\n"} {
		h := sudoHost(t)
		h.Handler = model(h, true)
		h.AddFile(sudoers.SudoersRS, []byte(main), 0o440, 0, 0)
		_, p := plan(t, h, rsChoice, alicePolicy)
		_, err := sudoers.Apply(context.Background(), h, p, opts)
		if !strings.Contains(main, bad) {
			if err != nil {
				t.Fatalf("Apply = %v", err)
			}
			checkFile(t, h, grantFile, newGrant, 0o440)
			continue
		}
		var ce *sudoers.CheckError
		if !errors.As(err, &ce) || ce.Stage != sudoers.StageBefore || ce.Path != sudoers.SudoersRS {
			t.Fatalf("Apply = %v, want a before rejection of %s", err, sudoers.SudoersRS)
		}
		checkAbsent(t, h, grantFile, envFile)
	}
}

// TestRollbackChanged: Rollback undoes only what the plan wrote. Once
// Apply refused a file another writer changed (ErrChanged), Rollback of
// the same plan leaves that file alone and says so.
func TestRollbackChanged(t *testing.T) {
	h := sudoHost(t)
	_, p := plan(t, h, sudoChoice, alicePolicy)
	other := "# written by another tool\nalice ALL=(ALL) ALL\n"
	h.AddFile(grantFile, []byte(other), 0o440, 0, 0)
	if _, err := sudoers.Apply(context.Background(), h, p, opts); !errors.Is(err, sudoers.ErrChanged) {
		t.Fatalf("Apply = %v, want ErrChanged", err)
	}
	before := len(writes(h))
	if err := sudoers.Rollback(context.Background(), h, p); !errors.Is(err, sudoers.ErrChanged) || !errors.Is(err, sudoers.ErrNotRestored) {
		t.Fatalf("Rollback = %v, want ErrChanged and ErrNotRestored", err)
	}
	checkFile(t, h, grantFile, other, 0o440)
	if w := writes(h)[before:]; len(w) != 0 {
		t.Errorf("Rollback wrote %q", w)
	}
}

// TestRollbackTool: a plan whose tool is lost is refused before anything
// is written, not checked as doas's.
func TestRollbackTool(t *testing.T) {
	h := sudoHost(t)
	_, p := plan(t, h, sudoChoice, alicePolicy)
	if _, err := sudoers.Apply(context.Background(), h, p, opts); err != nil {
		t.Fatal(err)
	}
	before := len(h.Calls())
	p.Tool = sudoers.ToolUnknown
	if err := sudoers.Rollback(context.Background(), h, p); !errors.Is(err, fs.ErrInvalid) || len(h.Calls()) != before {
		t.Errorf("Rollback = %v, calls %q", err, h.Calls()[before:])
	}
	checkFile(t, h, grantFile, newGrant, 0o440)
}

// TestPlanCheck: a plan PlanFor made passes Check, for sudo and doas, a
// grant and a removal; each edit of one field a journal could carry is
// refused with fs.ErrInvalid, and Rollback then reads and writes nothing.
func TestPlanCheck(t *testing.T) {
	h := sudoHost(t)
	h.AddFile(grantFile, []byte(oldGrant), 0o440, 0, 0)
	_, sudo := plan(t, h, sudoChoice, alicePolicy)
	_, remove := plan(t, h, sudoChoice, sudoers.Policy{User: "alice"})
	_, doas := plan(t, doasHost(t), doasChoice, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword})
	for _, p := range []sudoers.Plan{sudo, remove, doas} {
		if err := p.Check(); err != nil {
			t.Fatalf("Check of %+v = %v", p.Changes, err)
		}
	}
	for _, tc := range []struct {
		name string
		base sudoers.Plan
		edit func(*sudoers.Plan)
	}{
		{"the main sudoers file as a change", sudo, func(p *sudoers.Plan) {
			p.Changes[0].Path, p.Changes[0].Old.Path = sudoers.MainConfig, sudoers.MainConfig
		}},
		{"another file in the drop-in directory", sudo, func(p *sudoers.Plan) {
			p.Changes[0].Path, p.Changes[0].Old.Path = sudoers.DropInDir+"/90-cloud-init-users", sudoers.DropInDir+"/90-cloud-init-users"
		}},
		{"an unclean path", sudo, func(p *sudoers.Plan) { p.Changes[0].Path = sudoers.DropInDir + "//hardn-grant-alice" }},
		{"a sudo file in doas's directory", doas, func(p *sudoers.Plan) { p.Changes[0].Path, p.Changes[0].Old.Path = grantFile, grantFile }},
		{"one path twice", sudo, func(p *sudoers.Plan) { p.Changes[1] = p.Changes[0] }},
		{"its old state from another path", sudo, func(p *sudoers.Plan) { p.Changes[0].Old.Path = envFile }},
		{"old content for a file that did not exist", sudo, func(p *sudoers.Plan) { p.Changes[1].Old.Data = []byte(oldGrant) }},
		// Old is what root's rollback writes back, with its mode and owner.
		{"an old state others can write", sudo, func(p *sudoers.Plan) { p.Changes[0].Old.Mode = 0o666 }},
		{"an old state group-writable", sudo, func(p *sudoers.Plan) { p.Changes[0].Old.Mode = 0o460 }},
		{"an old state setuid", sudo, func(p *sudoers.Plan) { p.Changes[0].Old.Mode = 0o440 | fs.ModeSetuid }},
		{"an old state owned by alice", sudo, func(p *sudoers.Plan) { p.Changes[0].Old.UID = 1000 }},
		{"an old state of another group", sudo, func(p *sudoers.Plan) { p.Changes[0].Old.GID = 1000 }},
		{"a mode that is not sudo's", sudo, func(p *sudoers.Plan) { p.Changes[0].Mode = 0o644 }},
		{"a removal with content", remove, func(p *sudoers.Plan) { p.Changes[0].Data = []byte(newGrant) }},
		{"a removal of a file that did not exist", remove, func(p *sudoers.Plan) { p.Changes[0].Old = sudoers.File{Path: grantFile} }},
		{"a write that is not hardn's", sudo, func(p *sudoers.Plan) { p.Changes[0].Data = []byte("alice ALL=(ALL) NOPASSWD: ALL\n") }},
		{"another main file", sudo, func(p *sudoers.Plan) { p.Main = "/etc/sudoers.d/hardn-grant-alice" }},
		{"a main file for doas", doas, func(p *sudoers.Plan) { p.Main = sudoers.MainConfig }},
		{"no tool", sudo, func(p *sudoers.Plan) { p.Tool = sudoers.ToolUnknown }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.base
			p.Changes = slices.Clone(p.Changes)
			tc.edit(&p)
			if err := p.Check(); !errors.Is(err, fs.ErrInvalid) {
				t.Errorf("Check = %v, want fs.ErrInvalid", err)
			}
			before := len(h.Calls())
			if err := sudoers.Rollback(context.Background(), h, p); !errors.Is(err, fs.ErrInvalid) || !errors.Is(err, sudoers.ErrNotRestored) || len(h.Calls()) != before {
				t.Errorf("Rollback = %v, calls %q; want refused with nothing read", err, h.Calls()[before:])
			}
		})
	}
}

// TestApplyRestoreTimeout: the restore and the cleanup run under their own
// deadline, Options.RestoreTimeout or DefaultRestoreTimeout, not ctx's.
func TestApplyRestoreTimeout(t *testing.T) {
	for _, d := range []time.Duration{0, 5 * time.Second} {
		h := sudoHost(t)
		_, p := plan(t, h, sudoChoice, alicePolicy)
		h.Respond(hostfake.Response{}, "visudo", "-c")
		h.Respond(hostfake.Response{Stderr: "syntax error\n", ExitCode: 1}, "visudo", "-c")
		h.Respond(hostfake.Response{}, "visudo", "-c")
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		rec := &deadlines{System: h}
		start := time.Now()
		_, err := sudoers.Apply(ctx, rec, p, sudoers.Options{RunDir: runDir, RestoreTimeout: d})
		end := time.Now()
		cancel()
		want := d
		if want == 0 {
			want = sudoers.DefaultRestoreTimeout
		}
		if !errors.Is(err, sudoers.ErrRejected) || len(rec.seen) != 3 { // two removes, the cleanup
			t.Fatalf("Apply = %v, removals %d", err, len(rec.seen))
		}
		for _, dl := range rec.seen {
			if dl.Before(start.Add(want)) || dl.After(end.Add(want)) {
				t.Errorf("RestoreTimeout %s: deadline %s after the start, want %s", d, dl.Sub(start), want)
			}
		}
	}
}

// deadlines records the deadline of each removal's context.
type deadlines struct {
	*hostfake.System
	seen []time.Time
}

func (d *deadlines) Remove(ctx context.Context, name string) error {
	dl, _ := ctx.Deadline()
	d.seen = append(d.seen, dl)
	return d.System.Remove(ctx, name)
}

func (d *deadlines) RemoveAll(ctx context.Context, name string) error {
	dl, _ := ctx.Deadline()
	d.seen = append(d.seen, dl)
	return d.System.RemoveAll(ctx, name)
}
