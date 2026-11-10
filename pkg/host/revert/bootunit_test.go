package revert

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// workDir is the run directory a boot unit's copy is checked in.
const workDir = DefaultRunDir + "/" + testID

// The boot unit and the boot script are pinned byte for byte
// (testdata/hardn-boot.service, testdata/hardn-boot.openrc; after an
// intended change: go test ./pkg/host/revert -run TestPlanBootUnit -update,
// and review the diff), and their order is checked line by line: before
// sshd's units, ordering only, so that a rollback at boot restores the files
// before sshd reads them and sshd starts whatever the unit does.
func TestPlanBootUnit(t *testing.T) {
	for _, tt := range []struct {
		name, golden string
		in           detect.Init
		want         BootUnit
		lines        []string // that must be there
		never        []string // that must not
	}{
		{"systemd", "hardn-boot.service", systemd, BootUnit{Init: detect.Systemd, Path: "/etc/systemd/system/hardn-boot.service", Mode: 0o644,
			Link: "/etc/systemd/system/multi-user.target.wants/hardn-boot.service", Check: []string{"systemd-analyze", "verify", "--recursive-errors=no"},
			Enable: []string{"systemctl", "--no-reload", "enable", "hardn-boot.service"}, Disable: []string{"systemctl", "--no-reload", "disable", "hardn-boot.service"},
			Reload: []string{"systemctl", "daemon-reload"}},
			[]string{"DefaultDependencies=no", "After=local-fs.target sysinit.target ufw.service", "Before=ssh.service ssh.socket sshd.service sshd.socket shutdown.target",
				"Type=oneshot", "ExecStart=" + testHardn + " rollback --boot", "TimeoutStartSec=7min", "WantedBy=multi-user.target"},
			[]string{"Requires=", "RequiredBy=", "BindsTo=", "Wants=ssh", "%"}},
		{"OpenRC", "hardn-boot.openrc", openrc, BootUnit{Init: detect.OpenRC, Path: "/etc/init.d/hardn-boot", Mode: 0o755,
			Link: "/etc/runlevels/default/hardn-boot", Check: []string{"sh", "-n"},
			Enable: []string{"rc-update", "add", "hardn-boot", "default"}, Disable: []string{"rc-update", "del", "hardn-boot", "default"}},
			[]string{"#!/sbin/openrc-run", "\tneed localmount", "\tafter bootmisc firewall", "\tbefore sshd", "\t" + testHardn + " rollback --boot"},
			[]string{"need sshd", "use sshd"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			u, err := PlanBootUnit(tt.in, testHardn)
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", tt.golden)
			if *update {
				if err := os.WriteFile(golden, u.Data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(u.Data, want) {
				t.Errorf("%s differs from %s:\n%s", u.Path, golden, u.Data)
			}
			data := u.Data
			u.Data, tt.want.Data = nil, nil
			if !reflect.DeepEqual(u, tt.want) {
				t.Errorf("unit %+v\nwant %+v", u, tt.want)
			}
			lines := strings.Split(string(data), "\n")
			for _, l := range tt.lines {
				if !contains(lines, l) {
					t.Errorf("no line %q", l)
				}
			}
			for _, l := range tt.never {
				for _, got := range lines {
					if !strings.HasPrefix(got, "#") && strings.Contains(got, l) {
						t.Errorf("line %q has %q", got, l)
					}
				}
			}
		})
	}
	for _, tt := range []struct {
		name  string
		in    detect.Init
		hardn string
		want  error
	}{
		{"a relative path", systemd, "hardn", ErrExecutable},
		{"a space", systemd, "/opt/my tools/hardn", ErrExecutable},
		{"a systemd specifier", systemd, "/opt/%n/hardn", ErrExecutable},
		{"a shell character", openrc, "/opt/$x/hardn", ErrExecutable},
		{"OpenRC not running", openrcNotRunning(), testHardn, ErrNoTimer},
		{"no init system", detect.Init{}, testHardn, ErrNoTimer},
	} {
		if _, err := PlanBootUnit(tt.in, tt.hardn); !errors.Is(err, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, err, tt.want)
		}
	}
}

// The unit's TimeoutStartSec is above all Boot can take: its own steps,
// a rollback, and the unit's removal after it, each under its own bound.
// Otherwise systemd's kill could land inside the rollback, before sshd
// starts.
func TestBootUnitTimeout(t *testing.T) {
	u, err := PlanBootUnit(systemd, testHardn)
	if err != nil {
		t.Fatal(err)
	}
	var got time.Duration
	for _, l := range strings.Split(string(u.Data), "\n") {
		if v, ok := strings.CutPrefix(l, "TimeoutStartSec="); ok {
			if got, err = time.ParseDuration(strings.TrimSuffix(v, "in")); err != nil { // systemd's "7min", Go's "7m"
				t.Fatal(err)
			}
		}
	}
	if bound := bootStepTimeout + RollbackTimeout + cleanupTimeout; got <= bound {
		t.Errorf("TimeoutStartSec %s, want above %s", got, bound)
	}
}

func contains(lines []string, l string) bool {
	for _, got := range lines {
		if got == l {
			return true
		}
	}
	return false
}

// bootHost is a host with a run directory and the boot unit's directories,
// whose enable commands make their links, as systemctl and rc-update do.
func bootHost(t *testing.T) *hostfake.System {
	t.Helper()
	f := newHost(t)
	f.AddDir(workDir, 0o700, 0, 0)
	f.AddDir(SystemdUnitDir, 0o755, 0, 0)
	f.AddDir(OpenRCInitDir, 0o755, 0, 0)
	f.AddDir(OpenRCDefault, 0o755, 0, 0)
	f.Handler = func(argv []string, _ []byte) hostfake.Response {
		switch strings.Join(argv, " ") {
		case "systemctl --no-reload enable hardn-boot.service":
			f.AddSymlink(SystemdWants+"/hardn-boot.service", SystemdUnitDir+"/hardn-boot.service")
		case "rc-update add hardn-boot default":
			f.AddSymlink(OpenRCDefault+"/hardn-boot", OpenRCInitDir+"/hardn-boot")
		case "systemctl --no-reload disable hardn-boot.service", "rc-update del hardn-boot default":
			for _, l := range []string{SystemdWants + "/hardn-boot.service", OpenRCDefault + "/hardn-boot"} {
				_ = f.Remove(context.Background(), l)
			}
		case "systemd-analyze verify --recursive-errors=no " + workDir + "/hardn-boot.service", "sh -n " + workDir + "/hardn-boot",
			"systemctl daemon-reload":
		default:
			t.Errorf("unexpected command %q", argv)
			return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
		}
		return hostfake.Response{}
	}
	return f
}

// calls renders the fake's changes and commands, one per line.
func calls(f *hostfake.System, from int) []string {
	var out []string
	for _, c := range f.Calls()[from:] {
		switch c.Op {
		case "run", "write-atomic", "remove", "rename":
			if c.Err == nil {
				s := c.Op + " "
				if c.Op == "run" {
					s += strings.Join(c.Argv, " ")
				} else {
					s += c.Path
				}
				out = append(out, s)
			}
		}
	}
	return out
}

// EnsureBootUnit: checked, written, enabled; then idempotent; a stale file
// backed up and replaced; a missing link alone enabled; refused, with
// nothing installed, when the check refuses the copy or the enable makes
// no link.
func TestEnsureBootUnit(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name string
		in   detect.Init
		want []string
	}{
		{"systemd", systemd, []string{
			"write-atomic " + workDir + "/hardn-boot.service", "run systemd-analyze verify --recursive-errors=no " + workDir + "/hardn-boot.service", "remove " + workDir + "/hardn-boot.service",
			"write-atomic /etc/systemd/system/hardn-boot.service", "run systemctl --no-reload enable hardn-boot.service"}},
		{"OpenRC", openrc, []string{
			"write-atomic " + workDir + "/hardn-boot", "run sh -n " + workDir + "/hardn-boot", "remove " + workDir + "/hardn-boot",
			"write-atomic /etc/init.d/hardn-boot", "run rc-update add hardn-boot default"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := bootHost(t)
			u, _ := PlanBootUnit(tt.in, testHardn)
			var log bytes.Buffer
			res, err := EnsureBootUnit(ctx, f, u, workDir, nil, Options{Log: &log})
			if err != nil || res.Already || !res.Wrote || !res.Enabled || res.Backup != nil {
				t.Fatalf("EnsureBootUnit = %+v, %v", res, err)
			}
			if got := calls(f, 0); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
			if e, ok := f.Entry(u.Path); !ok || !bytes.Equal(e.Data, u.Data) || e.Mode.Perm() != u.Mode || e.UID != 0 || e.GID != 0 {
				t.Errorf("%s: %+v", u.Path, e)
			}
			if st, err := ReadBootUnit(ctx, f, u); err != nil || !st.Installed() || !bytes.Equal(st.Data, u.Data) || st.Mode.Perm() != u.Mode || st.UID != 0 {
				t.Errorf("ReadBootUnit = %+v, %v", st, err)
			}
			if !strings.Contains(log.String(), "revert: boot unit "+u.Path+": written") || !strings.Contains(log.String(), ": enabled (") {
				t.Errorf("log %q", log.String())
			}
			// Idempotent: nothing written or run the second time.
			n := len(f.Calls())
			if res, err := EnsureBootUnit(ctx, f, u, workDir, nil, Options{}); err != nil || !res.Already || res.Wrote || res.Enabled {
				t.Errorf("again: %+v, %v", res, err)
			}
			if got := calls(f, n); len(got) > 0 {
				t.Errorf("again: %q", got)
			}
		})
	}
	u, _ := PlanBootUnit(systemd, testHardn)
	t.Run("a stale unit, backed up", func(t *testing.T) {
		f := bootHost(t)
		f.AddFile(u.Path, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o640, 1000, 1000)
		f.AddSymlink(u.Link, u.Path)
		// What a plan diffs against.
		if st, err := ReadBootUnit(ctx, f, u); err != nil || st.Current || !st.Linked || string(st.Data) != "[Service]\nExecStart=/usr/bin/true\n" || st.Mode.Perm() != 0o640 || st.UID != 1000 {
			t.Fatalf("ReadBootUnit = %+v, %v", st, err)
		}
		run, err := backup.Open(f, backup.Settings{Path: backup.DefaultPath, Enabled: true}, testID)
		if err != nil {
			t.Fatal(err)
		}
		res, err := EnsureBootUnit(ctx, f, u, workDir, run, Options{})
		if err != nil || !res.Wrote || res.Enabled || res.Backup == nil || res.Backup.Entry.Kind != backup.KindFile {
			t.Fatalf("EnsureBootUnit = %+v, %v", res, err)
		}
		if e, _ := f.Entry(filepath.Join(run.Dir(), res.Backup.Entry.File.Backup)); !strings.Contains(string(e.Data), "/usr/bin/true") {
			t.Errorf("backup %q", e.Data)
		}
	})
	t.Run("the link alone missing", func(t *testing.T) {
		f := bootHost(t)
		f.AddFile(u.Path, u.Data, 0o644, 0, 0)
		res, err := EnsureBootUnit(ctx, f, u, workDir, nil, Options{})
		if err != nil || res.Wrote || !res.Enabled {
			t.Fatalf("EnsureBootUnit = %+v, %v", res, err)
		}
		if got := calls(f, 0); !reflect.DeepEqual(got, []string{"run systemctl --no-reload enable hardn-boot.service"}) {
			t.Errorf("calls %q", got)
		}
	})
	t.Run("no checker installed: written unchecked", func(t *testing.T) {
		f := bootHost(t)
		f.Respond(hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}, "systemd-analyze", "verify", "--recursive-errors=no", workDir+"/hardn-boot.service")
		var log bytes.Buffer
		if res, err := EnsureBootUnit(ctx, f, u, workDir, nil, Options{Log: &log}); err != nil || !res.Wrote || !res.Enabled {
			t.Fatalf("EnsureBootUnit = %+v, %v", res, err)
		}
		if !strings.Contains(log.String(), "systemd-analyze is not installed, so it is written unchecked") {
			t.Errorf("log %q", log.String())
		}
	})
	t.Run("the wrong mode", func(t *testing.T) {
		f := bootHost(t)
		f.AddFile(u.Path, u.Data, 0o666, 0, 0)
		f.AddSymlink(u.Link, u.Path)
		if res, err := EnsureBootUnit(ctx, f, u, workDir, nil, Options{}); err != nil || !res.Wrote {
			t.Fatalf("EnsureBootUnit = %+v, %v", res, err)
		}
		if e, _ := f.Entry(u.Path); e.Mode.Perm() != 0o644 {
			t.Errorf("mode %v", e.Mode)
		}
	})
	for _, tt := range []struct {
		name  string
		setup func(f *hostfake.System)
		hook  func(op, name string) error // nil: none
		work  string                      // "": workDir
		want  error
	}{
		{"the check refuses the copy", func(f *hostfake.System) {
			f.Respond(hostfake.Response{Stderr: "hardn-boot.service: Command /usr/local/bin/hardn is not executable\n", ExitCode: 1},
				"systemd-analyze", "verify", "--recursive-errors=no", workDir+"/hardn-boot.service")
		}, nil, "", ErrBootUnit},
		{"the enable makes no link", func(f *hostfake.System) {
			f.Respond(hostfake.Response{}, "systemctl", "--no-reload", "enable", "hardn-boot.service")
		}, nil, "", ErrBootUnit},
		{"the enable fails", func(f *hostfake.System) {
			f.Respond(hostfake.Response{Stderr: "Failed to enable unit\n", ExitCode: 1}, "systemctl", "--no-reload", "enable", "hardn-boot.service")
		}, nil, "", errExit},
		{"the journal cannot be read", func(f *hostfake.System) {
			f.AddDir(DefaultDir, 0o700, 0, 0)
			f.AddFile(stateFile(PendingName), []byte(`{"schemaVersion": 2}`), 0o600, 0, 0)
		}, nil, "", ErrJournal},
		{"a file, not a link, where the link goes", func(f *hostfake.System) {
			f.AddFile(u.Link, []byte("not a link\n"), 0o644, 0, 0)
			f.Respond(hostfake.Response{Stderr: "Failed to enable unit: File exists\n"}, "systemctl", "--no-reload", "enable", "hardn-boot.service")
		}, nil, "", ErrBootUnit},
		{"the unit cannot be read", func(*hostfake.System) {}, failOn("read-nofollow", u.Path), "", fs.ErrPermission},
		{"the link cannot be read", func(*hostfake.System) {}, failOn("lstat", u.Link), "", fs.ErrPermission},
		{"the link cannot be read after the enable", func(*hostfake.System) {}, func() func(string, string) error {
			n := 0
			return func(op, name string) error {
				if op == "lstat" && name == u.Link {
					if n++; n > 1 {
						return fs.ErrPermission
					}
				}
				return nil
			}
		}(), "", fs.ErrPermission},
		{"the copy cannot be written", func(*hostfake.System) {}, failOn("write-atomic", workDir+"/hardn-boot.service"), "", fs.ErrPermission},
		{"the work directory is not clean", func(*hostfake.System) {}, nil, "run/hardn", fs.ErrInvalid},
		{"the unit cannot be written", func(*hostfake.System) {}, failOn("write-atomic", u.Path), "", fs.ErrPermission},
		{"a stale unit cannot be backed up", func(f *hostfake.System) {
			f.AddFile(u.Path, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644, 0, 0)
		}, func(op, name string) error {
			if op == "write-atomic" && strings.HasPrefix(name, backup.DefaultPath+"/") {
				return fs.ErrPermission
			}
			return nil
		}, "", fs.ErrPermission},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := bootHost(t)
			tt.setup(f)
			before, _ := f.Entry(u.Path)
			var sys host.System = f
			if tt.hook != nil {
				sys = &faulty{System: f, hook: tt.hook}
			}
			run, err := backup.Open(sys, backup.Settings{Path: backup.DefaultPath, Enabled: true}, testID)
			if err != nil {
				t.Fatal(err)
			}
			work := workDir
			if tt.work != "" {
				work = tt.work
			}
			_, err = EnsureBootUnit(ctx, sys, u, work, run, Options{})
			if !isErr(err, tt.want) {
				t.Fatalf("EnsureBootUnit: %v, want %v", err, tt.want)
			}
			if exists(f, workDir+"/hardn-boot.service") {
				t.Error("the copy is left behind")
			}
			// After the enable, the link is there; only its re-read failed.
			if st, _ := ReadBootUnit(ctx, f, u); st.Installed() != strings.HasSuffix(tt.name, "after the enable") {
				t.Errorf("installed %v", st.Installed())
			}
			if tt.want == fs.ErrPermission && strings.HasPrefix(tt.name, "a stale") {
				if e, _ := f.Entry(u.Path); !bytes.Equal(e.Data, before.Data) {
					t.Error("the stale unit was replaced with no backup")
				}
			}
		})
	}
}

// errExit stands for a command's non-zero exit (*host.ExitError) in a
// test's want; isErr matches it with errors.As.
var errExit = errors.New("a non-zero exit")

func isErr(err, want error) bool {
	if want == errExit {
		var xe *host.ExitError
		return errors.As(err, &xe)
	}
	return errors.Is(err, want)
}

// EnsureBootUnit refuses, before it writes or runs anything, while a change
// is journalled: rewritten for another hardn binary, the unit would leave
// that change alone at boot (it is not this binary's journal).
func TestEnsureBootUnitPending(t *testing.T) {
	ctx := context.Background()
	for _, installed := range []bool{false, true} {
		f := bootHost(t)
		c := &clock{testNow}
		mine, _ := PlanBootUnit(systemd, testHardn)
		if installed {
			if _, err := EnsureBootUnit(ctx, f, mine, workDir, nil, Options{}); err != nil {
				t.Fatal(err)
			}
		}
		armed(t, f, c)
		other, _ := PlanBootUnit(systemd, "/usr/sbin/hardn")
		n := len(f.Calls())
		_, err := EnsureBootUnit(ctx, f, other, workDir, nil, testOptions(c, nil))
		var pe *PendingError
		if !errors.Is(err, ErrPending) || !errors.As(err, &pe) || pe.State.Pending == nil || pe.State.Pending.ID != testID {
			t.Fatalf("installed %v: EnsureBootUnit: %v, want the PendingError of %s", installed, err, testID)
		}
		if got := calls(f, n); len(got) > 0 {
			t.Errorf("installed %v: changed the host: %q", installed, got)
		}
		if e, ok := f.Entry(mine.Path); ok != installed || ok && !bytes.Equal(e.Data, mine.Data) {
			t.Errorf("installed %v: the unit is now %q", installed, e.Data)
		}
	}
}

// RemoveBootUnit: never while a change is journalled; otherwise disabled,
// then removed; a unit that is not there is no error.
func TestRemoveBootUnit(t *testing.T) {
	ctx := context.Background()
	for _, in := range []detect.Init{systemd, openrc} {
		t.Run(in.Kind.String(), func(t *testing.T) {
			f := bootHost(t)
			c := &clock{testNow}
			u, _ := PlanBootUnit(in, testHardn)
			if _, err := EnsureBootUnit(ctx, f, u, workDir, nil, Options{}); err != nil {
				t.Fatal(err)
			}
			armed(t, f, c)
			n := len(f.Calls())
			if removed, err := RemoveBootUnit(ctx, f, u, testOptions(c, nil)); removed || !errors.Is(err, ErrPending) {
				t.Errorf("while pending: %v, %v; want ErrPending", removed, err)
			}
			if got := calls(f, n); len(got) > 0 {
				t.Errorf("while pending: %q", got)
			}
			if err := f.Remove(ctx, stateFile(PendingName)); err != nil {
				t.Fatal(err)
			}
			n = len(f.Calls())
			var log bytes.Buffer
			if removed, err := RemoveBootUnit(ctx, f, u, Options{Log: &log}); !removed || err != nil {
				t.Fatalf("RemoveBootUnit = %v, %v", removed, err)
			}
			// The disable command's own removal of the link is recorded
			// before the command (bootHost's handler).
			// Then, on systemd, the daemon-reload that clears every unit's
			// NeedDaemonReload, which the file's removal set.
			want := []string{"remove " + u.Link, "run " + strings.Join(u.Disable, " "), "remove " + u.Path}
			if u.Reload != nil {
				want = append(want, "run "+strings.Join(u.Reload, " "))
			}
			if got := calls(f, n); !reflect.DeepEqual(got, want) {
				t.Errorf("calls %q, want %q", got, want)
			}
			if !strings.Contains(log.String(), "disabled and removed") {
				t.Errorf("log %q", log.String())
			}
			if removed, err := RemoveBootUnit(ctx, f, u, Options{}); removed || err != nil {
				t.Errorf("again: %v, %v", removed, err)
			}
		})
	}
}

// A daemon-reload that fails after the removal is logged, and the unit
// is removed all the same: systemd reads the removal at its next
// daemon-reload or boot.
func TestRemoveBootUnitReloadFails(t *testing.T) {
	ctx := context.Background()
	f := bootHost(t)
	u, _ := PlanBootUnit(systemd, testHardn)
	if _, err := EnsureBootUnit(ctx, f, u, workDir, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	f.Respond(hostfake.Response{Stderr: "Failed to reload daemon\n", ExitCode: 1}, u.Reload[0], u.Reload[1:]...)
	var log bytes.Buffer
	if removed, err := RemoveBootUnit(ctx, f, u, Options{Log: &log}); !removed || err != nil {
		t.Fatalf("RemoveBootUnit = %v, %v; want removed, no error", removed, err)
	}
	if st, err := ReadBootUnit(ctx, f, u); err != nil || st.File || st.Linked {
		t.Errorf("after: %+v, %v; want neither file nor link", st, err)
	}
	if want := "removed, but systemctl daemon-reload failed"; !strings.Contains(log.String(), want) {
		t.Errorf("log %q, want %q", log.String(), want)
	}
}

// RemoveBootUnit's failures: the journal or the unit unreadable, the
// disable failing or leaving the link, the file not removed; and a file
// that is not hardn's unit, disabled and kept, with ErrBootUnit.
func TestRemoveBootUnitFailures(t *testing.T) {
	ctx := context.Background()
	u, _ := PlanBootUnit(systemd, testHardn)
	disable := u.Disable
	for _, tt := range []struct {
		name    string
		setup   func(f *hostfake.System)
		hook    func(op, name string) error
		want    error
		removed bool
		file    bool // the file is still there
	}{
		{"the journal cannot be read", func(f *hostfake.System) {
			f.AddDir(DefaultDir, 0o700, 0, 0)
			f.AddFile(stateFile(PendingName), []byte(`{"schemaVersion": 2}`), 0o600, 0, 0)
		}, nil, ErrJournal, false, true},
		{"the unit cannot be read", func(*hostfake.System) {}, failOn("read-nofollow", u.Path), fs.ErrPermission, false, true},
		{"the disable fails", func(f *hostfake.System) {
			respond(f, hostfake.Response{Stderr: "Failed to disable unit\n", ExitCode: 1}, disable)
		}, nil, errExit, false, true},
		{"the disable leaves the link", func(f *hostfake.System) { respond(f, hostfake.Response{}, disable) }, nil, ErrBootUnit, true, false},
		{"the file is not removed", func(*hostfake.System) {}, failOn("remove", u.Path), fs.ErrPermission, true, true},
		{"not hardn's unit: disabled and kept", func(f *hostfake.System) {
			f.AddFile(u.Path, []byte("[Service]\nExecStart=/usr/local/bin/other\n"), 0o644, 0, 0)
		}, nil, ErrBootUnit, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := bootHost(t)
			if _, err := EnsureBootUnit(ctx, f, u, workDir, nil, Options{}); err != nil {
				t.Fatal(err)
			}
			tt.setup(f)
			var sys host.System = f
			if tt.hook != nil {
				sys = &faulty{System: f, hook: tt.hook}
			}
			removed, err := RemoveBootUnit(ctx, sys, u, Options{})
			if !isErr(err, tt.want) || removed != tt.removed {
				t.Fatalf("RemoveBootUnit = %v, %v; want %v, %v", removed, err, tt.removed, tt.want)
			}
			if exists(f, u.Path) != tt.file {
				t.Errorf("the file there: %v, want %v", exists(f, u.Path), tt.file)
			}
		})
	}
	t.Run("not hardn's unit: the disable ran", func(t *testing.T) {
		f := bootHost(t)
		f.AddFile(u.Path, []byte("edited\n"), 0o644, 0, 0)
		f.AddSymlink(u.Link, u.Path)
		var log bytes.Buffer
		if _, err := RemoveBootUnit(ctx, f, u, Options{Log: &log}); !errors.Is(err, ErrBootUnit) || !strings.Contains(err.Error(), "kept, disabled") {
			t.Fatalf("RemoveBootUnit: %v", err)
		}
		if e, _ := f.Entry(u.Path); string(e.Data) != "edited\n" || exists(f, u.Link) || !strings.Contains(log.String(), "disabled, and kept") {
			t.Errorf("file %q, link there %v, log %q", e.Data, exists(f, u.Link), log.String())
		}
	})
}
