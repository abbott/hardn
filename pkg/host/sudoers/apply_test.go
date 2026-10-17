package sudoers_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

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
			_, err := sudoers.Apply(context.Background(), h, p, opts)
			var ce *sudoers.CheckError
			if !errors.As(err, &ce) || !errors.Is(err, sudoers.ErrRejected) || ce.Stage != tc.stage || ce.Path != tc.path || len(ce.Tail) == 0 {
				t.Fatalf("Apply = %v (%+v), want a %s rejection of %s", err, ce, tc.stage, tc.path)
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
// either, both failures are returned.
func TestApplyRestoreFails(t *testing.T) {
	h := sudoHost(t)
	_, p := plan(t, h, sudoChoice, alicePolicy)
	h.Respond(hostfake.Response{}, "visudo", "-c")
	h.Respond(hostfake.Response{Stderr: "syntax error\n", ExitCode: 1}, "visudo", "-c")
	_, err := sudoers.Apply(context.Background(), h, p, opts)
	if err == nil || !strings.Contains(err.Error(), "after check") || !strings.Contains(err.Error(), "restored check") {
		t.Fatalf("Apply = %v", err)
	}
	checkAbsent(t, h, grantFile, envFile)
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
	if !errors.Is(err, errDisk) {
		t.Fatalf("Apply = %v", err)
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
