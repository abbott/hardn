package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/netip"
	"os"
	"os/signal"
	"path"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/revert/plans"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
)

// The fake VM and its model of sshd are in fake_test.go.

func TestGates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		goos   string
		args   []string
		gate   string
		euid   int
		code   int
		built  bool
		stderr string
	}{
		{"macOS, whatever else", "darwin", []string{"--" + gateFlag, "facts"}, gateValue, 0, 2, false, "macOS"},
		{"macOS, no arguments", "darwin", nil, gateValue, 0, 2, false, "macOS"},
		{"not Linux", "freebsd", []string{"--" + gateFlag, "facts"}, gateValue, 0, 2, false, "Linux VM only"},
		{"no flag", "linux", []string{"facts"}, gateValue, 0, 2, false, "--" + gateFlag},
		{"no variable", "linux", []string{"--" + gateFlag, "facts"}, "", 0, 2, false, gateEnv},
		{"another value", "linux", []string{"--" + gateFlag, "facts"}, "1", 0, 2, false, gateEnv},
		{"not root", "linux", []string{"--" + gateFlag, "facts"}, gateValue, 1000, 2, false, "root"},
		{"unknown command", "linux", []string{"--" + gateFlag, "reboot"}, gateValue, 0, 2, false, "unknown command"},
		{"no command", "linux", []string{"--" + gateFlag}, gateValue, 0, 2, false, "usage"},
		{"stray argument", "linux", []string{"--" + gateFlag, "facts", "now"}, gateValue, 0, 2, false, "unexpected arguments"},
		{"all hold", "linux", []string{"--" + gateFlag, "facts"}, gateValue, 0, 1, true, "detect failed"},
		{"the flag after the command", "linux", []string{"facts", "--" + gateFlag}, gateValue, 0, 1, true, "detect failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			built := false
			var errOut bytes.Buffer
			e := env{
				goos: tc.goos, euid: func() int { return tc.euid }, now: func() time.Time { return testNow },
				getenv: func(k string) string {
					if k == gateEnv {
						return tc.gate
					}
					return ""
				},
				stdout: io.Discard, stderr: &errOut,
				system: func(io.Writer) host.System { built = true; return hostfake.New(host.Options{}) },
				detect: func(context.Context, host.System) (detect.Facts, error) {
					return detect.Facts{}, errors.New("detect failed")
				},
			}
			if code := run(context.Background(), tc.args, e); code != tc.code {
				t.Errorf("exit %d, want %d; stderr %q", code, tc.code, errOut.String())
			}
			if built != tc.built {
				t.Errorf("host built %v, want %v", built, tc.built)
			}
			if !strings.Contains(errOut.String(), tc.stderr) {
				t.Errorf("stderr %q lacks %q", errOut.String(), tc.stderr)
			}
		})
	}
}

func TestSetDirectives(t *testing.T) {
	port := func(ps ...string) []migrate.Directive {
		var out []migrate.Directive
		for _, p := range ps {
			out = append(out, migrate.Directive{Keyword: "Port", Args: []string{p}})
		}
		return out
	}
	const keys = "AuthorizedKeysFile .ssh/authorized_keys .ssh_%u/authorized_keys\n"
	for _, tc := range []struct {
		name string
		old  string
		set  []migrate.Directive
		want string
		err  bool
	}{
		{"a new file", "", port("22", "2230"), dropInHeader + setMarker + "\nPort 22\nPort 2230\n", false},
		{"p2drive's file again", dropInHeader + setMarker + "\nPort 22\nPort 2230\n",
			[]migrate.Directive{{Keyword: "PasswordAuthentication", Args: []string{"no"}}},
			dropInHeader + setMarker + "\nPasswordAuthentication no\nPort 22\nPort 2230\n", false},
		{"a migrated file: the labels stay with their lines",
			"# Managed by hardn.\n# From /etc/ssh/sshd_config.d/manage.conf:\nProtocol 2\nListenAddress 0.0.0.0:2230\nport=2230\n# Every key file the files hardn replaced named, so that no key stops working:\n" + keys,
			port("2230", "2231"),
			"# Managed by hardn.\n" + setMarker + "\nPort 2230\nPort 2231\n# From /etc/ssh/sshd_config.d/manage.conf:\nProtocol 2\nListenAddress 0.0.0.0:2230\n# Every key file the files hardn replaced named, so that no key stops working:\n" + keys, false},
		{"a header up to a blank line", "# site header\n\n  Port 2222\nPasswordAuthentication no",
			port("22", "2230"),
			"# site header\n\n" + setMarker + "\nPort 22\nPort 2230\nPasswordAuthentication no\n", false},
		{"no leading comment", "PermitRootLogin yes\nAllowUsers alice\n",
			[]migrate.Directive{{Keyword: "PermitRootLogin", Args: []string{"prohibit-password"}}},
			setMarker + "\nPermitRootLogin prohibit-password\nAllowUsers alice\n", false},
		{"a Match block", "Port 22\nMatch User alice\n  PasswordAuthentication yes\n", port("2230"), "", true},
		{"an Include", "Include /etc/ssh/extra/*.conf\n", port("2230"), "", true},
		{"an argument that needs quotes", "", []migrate.Directive{{Keyword: "Banner", Args: []string{"a b"}}}, "", true},
		{"no argument", "", []migrate.Directive{{Keyword: "Port"}}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var old []byte
			if tc.old != "" {
				old = []byte(tc.old)
			}
			got, err := setDirectives(old, tc.set)
			switch {
			case tc.err && err == nil:
				t.Fatalf("got %q, want an error", got)
			case !tc.err && err != nil:
				t.Fatalf("error %v", err)
			case !tc.err && string(got) != tc.want:
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestWanted(t *testing.T) {
	ls := func(ss ...string) []sshd.Listener {
		var out []sshd.Listener
		for _, s := range ss {
			ap := netip.MustParseAddrPort(s)
			out = append(out, sshd.Listener{Addr: ap.Addr(), Port: ap.Port()})
		}
		return out
	}
	after := sshd.Effective{
		Port:                   sshd.Set{Values: []string{"22", "2230"}, Known: true},
		Listeners:              sshd.Listeners{SSHD: ls("[::]:22", "0.0.0.0:22", "[::]:2230", "0.0.0.0:2230"), Known: true},
		PermitRootLogin:        sshd.Setting{Value: "prohibit-password", Known: true},
		PasswordAuthentication: sshd.Setting{Value: "yes", Known: true},
	}
	pinned := after
	pinned.Port.Values = []string{"2230", "2231"}
	pinned.Listeners.SSHD = ls("0.0.0.0:2230")
	d := func(k, v string) migrate.Directive { return migrate.Directive{Keyword: k, Args: []string{v}} }
	for _, tc := range []struct {
		name  string
		after sshd.Effective
		set   []migrate.Directive
		ok    bool
	}{
		{"the ports", after, []migrate.Directive{d("Port", "22"), d("Port", "2230")}, true},
		{"a port sshd does not show", after, []migrate.Directive{d("Port", "2231")}, false},
		{"a port in the Port set no listener has: a ListenAddress pins its own", pinned, []migrate.Directive{d("Port", "2230"), d("Port", "2231")}, false},
		{"without-password is prohibit-password", after, []migrate.Directive{d("PermitRootLogin", "without-password")}, true},
		{"another value", after, []migrate.Directive{d("PasswordAuthentication", "no")}, false},
		{"Unknown is not a pass", after, []migrate.Directive{d("KbdInteractiveAuthentication", "no")}, false},
		{"a keyword not checked", after, []migrate.Directive{d("MaxAuthTries", "3")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := wanted(tc.after, tc.set); (err == nil) != tc.ok {
				t.Errorf("wanted: %v, want ok %v", err, tc.ok)
			}
		})
	}
}

// TestPort2230 runs the brief's scenario on the fake, up to the
// confirmation, on Debian's ssh.service and on Ubuntu's ssh.socket: armed
// first, the firewall before sshd, the backup and the journal before the
// write, then the reload (on the socket: daemon-reload, the socket's
// configuration checked, its restart), the listener and MarkApplied.
func TestPort2230(t *testing.T) {
	for _, tc := range []struct {
		name   string
		vm     func(*testing.T) *vm
		facts  detect.Facts
		reload []string
	}{
		{"Debian 13, ssh.service", newVM, debian13, []string{"systemctl reload ssh.service"}},
		{"Ubuntu 26.04, ssh.socket", socketVM, ubuntu2604(),
			[]string{"systemctl daemon-reload", "systemctl show -p Listen ssh.socket", "systemctl restart ssh.socket"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := tc.vm(t)
			code, out, errOut := v.do(context.Background(), tc.facts, "port", "--port", "2230", "--hardn", testHardn)
			if code != 0 {
				t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
			}
			e, _ := v.f.Entry(sshd.HardnDropIn)
			if want := dropInHeader + setMarker + "\nPort 22\nPort 2230\n"; v.dropIn() != want || e.Mode.Perm() != 0o644 || e.UID != 0 {
				t.Errorf("00-hardn.conf %q (mode %v), want %q 0644 root's", e.Data, e.Mode, want)
			}
			if want := []string{"[::]:22", "0.0.0.0:22", "[::]:2230", "0.0.0.0:2230"}; !slices.Equal(v.allowed, []uint16{22, 2230}) || !sameStrings(v.bound, want) {
				t.Errorf("ufw allows %v, sshd listens on %v; want 22 and 2230 both", v.allowed, v.bound)
			}
			j := v.journal(revert.PendingName)
			switch {
			case j.Applied == nil:
				t.Error("the journal is not marked applied")
			case j.Timer == nil || !slices.Contains(j.Timer.Argv, testHardn) || !slices.Contains(j.Timer.Argv, "rollback"):
				t.Errorf("timer %+v, want %s rollback <id>", j.Timer, testHardn)
			case len(j.Undo) != 1 || strings.Join(j.Undo[0].Argv, " ") != "ufw delete allow 2230/tcp":
				t.Errorf("undo %+v, want ufw delete allow 2230/tcp", j.Undo)
			case j.Backup == nil || !slices.Equal(j.Backup.Seqs, []int{1}) || j.Backup.Dir != path.Join("/var/backups/hardn", j.ID):
				t.Errorf("backup %+v", j.Backup)
			case len(j.Reload) != 1 || j.Reload[0].IfUp || len(j.Reload[0].After) != 2:
				t.Errorf("reload %+v, want ssh after the forward reload ran, with the restored listeners", j.Reload)
			case len(j.ConfirmVia) != 2 || j.ConfirmVia[0].Port != 2230 || j.ConfirmVia[1].Port != 2230:
				t.Errorf("confirmVia %v, want port 2230", j.ConfirmVia)
			}
			// The boot unit first (revert-boot.md): checked, written,
			// enabled, then the timer.
			bootUnit := revert.SystemdUnitDir + "/" + revert.BootUnitName + ".service"
			boot := []int{v.index(runIs("systemd-analyze verify --recursive-errors=no " + path.Join(runRoot, j.ID, revert.BootUnitName+".service"))),
				v.index(writeOf(bootUnit)), v.index(runIs("systemctl --no-reload enable " + revert.BootUnitName + ".service")), v.index(armed)}
			if !slices.IsSorted(boot) || slices.Contains(boot, -1) {
				t.Errorf("order: boot unit checked, written, enabled, then armed at %v; want ascending", boot)
			}
			if e, ok := v.f.Entry(bootUnit); !ok || e.Mode.Perm() != 0o644 || e.UID != 0 || !strings.Contains(string(e.Data), "ExecStart="+testHardn+" rollback --boot\n") {
				t.Errorf("%s: %+v", bootUnit, e)
			}
			if !strings.Contains(out, "boot unit, before the timer: write "+bootUnit) {
				t.Errorf("the plan does not show the boot unit:\n%s", out)
			}
			arm := v.index(armed)
			allow := v.index(runIs("ufw allow 2230/tcp"))
			backedUp := v.index(func(c hostfake.Call) bool {
				return c.Op == "write-atomic" && strings.HasPrefix(c.Path, "/var/backups/hardn/") && strings.HasSuffix(c.Path, "manifest.json")
			})
			written := v.index(writeOf(sshd.HardnDropIn))
			journalled := slices.IndexFunc(v.f.Calls()[backedUp+1:], writeOf(path.Join(revert.DefaultDir, revert.PendingName))) + backedUp + 1
			order := []int{arm, allow, backedUp, journalled, written}
			for _, r := range tc.reload {
				order = append(order, slices.IndexFunc(v.f.Calls()[written+1:], runIs(r))+written+1)
			}
			if !slices.IsSorted(order) || slices.Contains(order, -1) || order[len(order)-1] == written {
				t.Errorf("order: armed, ufw allow, backup, journal, write, %q at %v; want ascending", tc.reload, order)
			}
			if _, ok := v.f.Entry(path.Join(runRoot, j.ID)); ok {
				t.Error("the run directory is left behind")
			}
			if !strings.Contains(out, "to port 2230 and run") || !strings.Contains(out, "sudo "+testHardn+" confirm") || !strings.Contains(out, "sha256 ") {
				t.Errorf("stdout does not say how to confirm, or what the timer runs:\n%s", out)
			}
		})
	}
}

// TestPortOpenRC: Alpine's port change: the sleeper as the timer, with its
// pidfile; no ufw installed, so no firewall step; rc-service sshd reload.
func TestPortOpenRC(t *testing.T) {
	v := newVM(t)
	v.f.AddFile(detect.OpenRCStarted+"/sshd", nil, 0o644, 0, 0)
	v.hook = func(cmd string) (hostfake.Response, bool) {
		return hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}, strings.HasPrefix(cmd, "ufw ")
	}
	code, out, errOut := v.do(context.Background(), alpine324(), "port", "--port", "2230")
	if code != 0 {
		t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
	}
	j := v.journal(revert.PendingName)
	if j.Applied == nil || j.Timer == nil || j.Timer.Kind != revert.TimerSetsid || j.Undo != nil || len(j.ConfirmVia) != 2 {
		t.Fatalf("journal applied %v, timer %+v, undo %v, confirmVia %v", j.Applied != nil, j.Timer, j.Undo, j.ConfirmVia)
	}
	if _, ok := v.f.Entry(j.Timer.PIDFile); !ok {
		t.Errorf("no pidfile %s", j.Timer.PIDFile)
	}
	started := v.index(func(c hostfake.Call) bool { return c.Op == "start-detached" })
	if reloaded := v.index(runIs("rc-service sshd reload")); !(started >= 0 && started < v.index(writeOf(sshd.HardnDropIn)) && v.index(writeOf(sshd.HardnDropIn)) < reloaded) {
		t.Errorf("order: sleeper %d, write, reload %d; want ascending", started, reloaded)
	}
	script := revert.OpenRCInitDir + "/" + revert.BootUnitName
	boot := []int{v.index(runIs("sh -n " + path.Join(runRoot, j.ID, revert.BootUnitName))), v.index(writeOf(script)),
		v.index(runIs("rc-update add " + revert.BootUnitName + " default")), started}
	if !slices.IsSorted(boot) || slices.Contains(boot, -1) {
		t.Errorf("order: boot script checked, written, added to the default runlevel, then the sleeper at %v; want ascending", boot)
	}
	if e, ok := v.f.Entry(script); !ok || e.Mode.Perm() != 0o755 || !strings.Contains(string(e.Data), "\t"+testHardn+" rollback --boot\n") {
		t.Errorf("%s: %+v", script, e)
	}
	if !strings.Contains(out, "ufw: not installed") || !sameStrings(v.bound, []string{"[::]:22", "0.0.0.0:22", "[::]:2230", "0.0.0.0:2230"}) {
		t.Errorf("sshd listens on %v; stdout:\n%s", v.bound, out)
	}
}

// TestBootUnitInstalled: a boot unit installed and enabled as planned is
// left as it is (no check, write or enable), and the plan says so; a stale
// one is backed up and replaced; a check that refuses the copy, or a
// --plan-only, arms nothing and leaves the host as it was.
// TestRunLock: a lockout-risk change holds the engine's run lock from
// before the boot unit's install to its last write, as the engine does
// (engine-boot.md §2.2), so `hardn rollback --boot` run by hand meanwhile
// keeps the unit; while another hardn holds the lock, the change is not
// applied and nothing changes.
func TestRunLock(t *testing.T) {
	lockFile := path.Join(runRoot, revert.RunLockName)
	t.Run("held by another hardn", func(t *testing.T) {
		v := newVM(t)
		v.f.AddDir(runRoot, 0o700, 0, 0)
		if _, err := v.f.Lock(context.Background(), lockFile, []byte("pid 6000, change 20261019T110000Z-5e6f7a8b, since 2026-10-19T11:59:59Z\n")); err != nil {
			t.Fatal(err)
		}
		code, out, errOut := v.do(context.Background(), debian13, "port", "--port", "2230")
		if code != 1 || !strings.Contains(errOut, "not applied") || !strings.Contains(errOut, "pid 6000") {
			t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
		}
		if ch := v.changes(); len(ch) > 0 || v.index(armed) >= 0 {
			t.Errorf("changed the host: %q", ch)
		}
	})
	t.Run("held across the boot unit's install and Arm", func(t *testing.T) {
		v := newVM(t)
		u, _ := revert.PlanBootUnit(debian13.Init, testHardn)
		var removeErr error
		v.hook = func(cmd string) (hostfake.Response, bool) {
			if cmd == strings.Join(u.Enable, " ") {
				_, removeErr = revert.RemoveBootUnit(context.Background(), v.f, u, revert.Options{})
			}
			return hostfake.Response{}, false
		}
		if code, out, errOut := v.do(context.Background(), debian13, "port", "--port", "2230", "--hardn", testHardn); code != 0 {
			t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
		}
		if !errors.Is(removeErr, host.ErrLocked) {
			t.Errorf("RemoveBootUnit during the install: %v, want host.ErrLocked", removeErr)
		}
		if e, ok := v.f.Entry(lockFile); !ok || len(e.Data) != 0 {
			t.Errorf("the run lock after the change: %q (exists %v), want it released", e.Data, ok)
		}
	})
}

func TestBootUnitInstalled(t *testing.T) {
	u, err := revert.PlanBootUnit(debian13.Init, testHardn)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("installed already", func(t *testing.T) {
		v := newVM(t)
		v.f.AddFile(u.Path, u.Data, 0o644, 0, 0)
		v.f.AddSymlink(u.Link, u.Path)
		code, out, errOut := v.do(context.Background(), debian13, "keyonly")
		if code != 0 || !strings.Contains(out, "boot unit: "+u.Path+", installed and enabled already") {
			t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
		}
		for _, c := range v.f.Calls() {
			if c.Path == u.Path && c.Op == "write-atomic" || c.Op == "run" && (c.Argv[0] == "systemd-analyze" || slices.Contains(c.Argv, "enable")) {
				t.Errorf("%s", c)
			}
		}
	})
	t.Run("a stale unit", func(t *testing.T) {
		v := newVM(t)
		v.f.AddFile(u.Path, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644, 0, 0)
		v.f.AddSymlink(u.Link, u.Path)
		if code, out, errOut := v.do(context.Background(), debian13, "keyonly"); code != 0 {
			t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
		}
		if e, _ := v.f.Entry(u.Path); string(e.Data) != string(u.Data) || len(v.checked) != 1 {
			t.Errorf("%s = %q after %d checks", u.Path, e.Data, len(v.checked))
		}
		if v.index(runIs("systemctl --no-reload enable "+revert.BootUnitName+".service")) >= 0 {
			t.Error("enabled again")
		}
		j := v.journal(revert.PendingName)
		e, ok := v.f.Entry(path.Join(j.Backup.Dir, "0001-"+revert.BootUnitName+".service"))
		if !ok || !strings.Contains(string(e.Data), "/usr/bin/true") || !slices.Equal(j.Backup.Seqs, []int{2}) {
			t.Errorf("the old unit's backup %q (%v), journalled seqs %v: want it kept, not journalled", e.Data, ok, j.Backup.Seqs)
		}
	})
	t.Run("the check refuses the copy", func(t *testing.T) {
		v := newVM(t)
		v.hook = func(cmd string) (hostfake.Response, bool) {
			return hostfake.Response{Stderr: "hardn-boot.service: Command /usr/local/bin/hardn is not executable\n", ExitCode: 1}, strings.HasPrefix(cmd, "systemd-analyze ")
		}
		code, out, errOut := v.do(context.Background(), debian13, "port", "--port", "2230")
		if code != 1 || !strings.Contains(errOut, "not applied") || !strings.Contains(errOut, revert.ErrBootUnit.Error()) {
			t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
		}
		if ch := v.changes(); len(ch) > 0 || v.index(armed) >= 0 {
			t.Errorf("changed the host: %q", ch)
		}
	})
	t.Run("--plan-only", func(t *testing.T) {
		v := newVM(t)
		code, out, errOut := v.do(context.Background(), debian13, "port", "--port", "2230", "--plan-only")
		if code != 0 || !strings.Contains(out, "boot unit, before the timer: write "+u.Path) {
			t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
		}
		if ch := v.changes(); len(ch) > 0 {
			t.Errorf("changed the host: %q", ch)
		}
	})
	// A second lockout-risk command while the first change is journalled,
	// with another hardn binary: refused at its plan, before the boot unit
	// the first change relies on is touched.
	t.Run("a change journalled already", func(t *testing.T) {
		v := newVM(t)
		ctx := context.Background()
		if code, out, errOut := v.do(ctx, debian13, "port", "--port", "2230"); code != 0 {
			t.Fatalf("port: exit %d; stderr %s\nstdout %s", code, errOut, out)
		}
		first := v.journal(revert.PendingName)
		unit, _ := v.f.Entry(u.Path)
		v.f.AddFile("/usr/sbin/hardn", []byte("ELF, another build"), 0o755, 0, 0)
		n := len(v.f.Calls())
		code, out, errOut := v.do(ctx, debian13, "keyonly", "--hardn", "/usr/sbin/hardn")
		if code != 1 || !strings.Contains(errOut, "refused before anything changed") || !strings.Contains(errOut, revert.ErrPending.Error()) || strings.Contains(out, "boot unit") {
			t.Fatalf("keyonly: exit %d; stderr %s\nstdout %s", code, errOut, out)
		}
		for _, c := range v.f.Calls()[n:] {
			if c.Op == "write-atomic" && !strings.HasPrefix(c.Path, runRoot+"/") || c.Op == "run" && (c.Argv[0] == "systemd-analyze" || slices.Contains(c.Argv, "enable")) {
				t.Errorf("the second command changed the host: %s", c)
			}
		}
		if e, _ := v.f.Entry(u.Path); !bytes.Equal(e.Data, unit.Data) || !strings.Contains(string(e.Data), "ExecStart="+testHardn+" rollback --boot") {
			t.Errorf("the boot unit is now %q", e.Data)
		}
		if j := v.journal(revert.PendingName); j.ID != first.ID || j.Timer.Hardn != testHardn {
			t.Errorf("pending %s, timer runs %s", j.ID, j.Timer.Hardn)
		}
	})
}

// TestPortUFWEnable: --ufw enable from an inactive ufw with no rules
// allows the held port and the new one, then enables ufw, all before sshd
// moves.
func TestPortUFWEnable(t *testing.T) {
	v := newVM(t)
	v.allowed, v.inactive = nil, true
	v.saveRules()
	if code, out, errOut := v.do(context.Background(), debian13, "port", "--port", "2230", "--ufw", "enable"); code != 0 {
		t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
	}
	order := []int{v.index(armed), v.index(runIs("ufw allow 22/tcp")), v.index(runIs("ufw allow 2230/tcp")),
		v.index(runIs("ufw --force enable")), v.index(writeOf(sshd.HardnDropIn)), v.index(runIs("systemctl reload ssh.service"))}
	if !slices.IsSorted(order) || slices.Contains(order, -1) {
		t.Errorf("order: armed, allow 22, allow 2230, enable, write, reload at %v; want ascending", order)
	}
	if v.inactive || !slices.Equal(v.allowed, []uint16{22, 2230}) {
		t.Errorf("ufw inactive %v, allows %v; want active with 22 and 2230", v.inactive, v.allowed)
	}
}

// TestKeyOnly: password and keyboard-interactive logins off, checked in
// sshd -T after the reload; no listener moves, so nothing names a port to
// confirm on and the firewall is not touched.
func TestKeyOnly(t *testing.T) {
	v := newVM(t)
	code, out, errOut := v.do(context.Background(), debian13, "keyonly")
	if code != 0 {
		t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
	}
	if want := dropInHeader + setMarker + "\nPasswordAuthentication no\nKbdInteractiveAuthentication no\n"; v.dropIn() != want {
		t.Errorf("00-hardn.conf %q, want %q", v.dropIn(), want)
	}
	if !strings.Contains(out, `sshd -T after: port [22] (known true); permitrootlogin "prohibit-password", passwordauthentication "no", kbdinteractiveauthentication "no"`) {
		t.Errorf("stdout lacks the values after the reload:\n%s", out)
	}
	if j := v.journal(revert.PendingName); j.Applied == nil || len(j.ConfirmVia) != 0 || j.Undo != nil || len(j.Reload) != 1 {
		t.Errorf("journal applied %v, confirmVia %v, undo %v, reload %v", j.Applied != nil, j.ConfirmVia, j.Undo, j.Reload)
	}
	if v.index(func(c hostfake.Call) bool { return c.Op == "run" && c.Argv[0] == "ufw" }) >= 0 {
		t.Error("ufw was read or changed for a change that moves no port")
	}
}

// TestRefusedBeforeArm: each refusal comes at plan time, before the timer,
// the firewall or any write outside the run directory.
func TestRefusedBeforeArm(t *testing.T) {
	migrated := "# Managed by hardn.\n# From /etc/ssh/sshd_config.d/manage.conf:\nListenAddress 0.0.0.0:2230\nPort 2230\n"
	for _, tc := range []struct {
		name  string
		facts detect.Facts
		setup func(*vm)
		args  []string
		want  string
	}{
		{"a port change on Proxmox VE (§3.18)", proxmox9(), nil, []string{"port", "--port", "2230"}, "port 22"},
		{"a ListenAddress in sshd_config pins port 22", debian13, func(v *vm) {
			v.f.AddFile(sshd.MainConfig, []byte(stockMain+"ListenAddress 0.0.0.0:22\n"), 0o644, 0, 0)
			v.boot()
		}, []string{"port", "--port", "2230"}, `pinned by /etc/ssh/sshd_config:4 "ListenAddress 0.0.0.0:22"`},
		{"a migrated 00-hardn.conf pins 2230", debian13, func(v *vm) {
			v.f.AddFile(sshd.HardnDropIn, []byte(migrated), 0o644, 0, 0)
			v.boot()
		}, []string{"port", "--port", "2231"}, `pinned by /etc/ssh/sshd_config.d/00-hardn.conf:3 "ListenAddress 0.0.0.0:2230"`},
		{"another service holds the new port", debian13, func(v *vm) { v.other = []string{"0.0.0.0:2230"} },
			[]string{"port", "--port", "2230"}, "already bound"},
		{"legacy hardn files", debian13, func(v *vm) {
			v.f.AddFile(migrate.ManageConf, fixture(v.t, "v032-manage.conf"), 0o644, 0, 0)
			v.boot()
		}, []string{"port", "--port", "2231"}, "run `p2drive migrate` first"},
		{"an Include that is not the stock first line (P-38)", debian13, func(v *vm) {
			v.f.AddFile(sshd.MainConfig, []byte("UsePAM yes\n"+sshd.IncludeLine+"\n"), 0o644, 0, 0)
			v.boot()
		}, []string{"keyonly"}, "P-38"},
		{"pve-root off Proxmox VE", debian13, nil, []string{"pve-root"}, "for a Proxmox VE host"},
		// v0.3.x wrote listen.conf and ran no daemon-reload (D9): the socket
		// still listens on 22, so the rollback's daemon-reload, with
		// listen.conf restored, would move it to 2230
		// (migrate.Plan.CheckRollback, reload.ErrRestoredListeners).
		{"migrate --apply of listen.conf on a socket host that never read it (D9)", ubuntu2604(), func(v *vm) {
			v032Files(v)
			v.configured = []string{"[::]:22", "0.0.0.0:22"}
			v.bound = slices.Clone(v.configured)
		}, []string{"migrate", "--apply"}, "ListenStream=2230 is not one of the listeners the socket had"},
		// The socket configured for 22 beside listen.conf's 2230 (an
		// administrator who added the port first): the rollback's
		// daemon-reload would leave 2230 alone, and the rollback unfinished.
		{"migrate --apply where ssh.socket is configured for more than listen.conf", ubuntu2604(), func(v *vm) {
			v032Files(v)
			v.configured = []string{"[::]:22", "0.0.0.0:22", "[::]:2230", "0.0.0.0:2230"}
			v.bound = slices.Clone(v.configured)
		}, []string{"migrate", "--apply"}, "do not give [::]:22, which the socket had"},
		// A daemon-reload since ssh.socket was last restarted: configured
		// on listen.conf's 2230, still bound on 22, where this session is.
		// The migration's hold and its rollback both read configuration, so
		// the restart would close 22 for good (reload.CheckInEffect).
		{"migrate --apply where ssh.socket is configured for 2230 and bound on 22", ubuntu2604(), func(v *vm) {
			v032Files(v)
			v.configured = []string{"[::]:2230"}
			v.bound = []string{"[::]:22", "0.0.0.0:22"}
		}, []string{"migrate", "--apply"}, "this session is connected to 192.0.2.20:22"},
		{"migrate --apply, configured for 2230, bound on 22, a session on 2230", ubuntu2604(), func(v *vm) {
			v032Files(v)
			v.configured = []string{"[::]:2230"}
			v.bound = []string{"[::]:22", "0.0.0.0:22"}
			v.conn = conn2230
		}, []string{"migrate", "--apply"}, "[[::]:2230] configured, not bound"},
		// sshd_config edited and not reloaded: sshd -T shows 2231, sshd
		// still listens on 22, where this session is.
		{"a port change where sshd -T is not what sshd listens on", debian13, func(v *vm) {
			v.f.AddFile(sshd.MainConfig, []byte(stockMain+"Port 2231\n"), 0o644, 0, 0)
		}, []string{"port", "--port", "2230"}, "this session is connected to 192.0.2.20:22"},
		// A remote session whose connection is not known: the port it is
		// on cannot be shown kept, so a listener change fails closed.
		{"a port change from a session whose connection is not known", debian13, func(v *vm) { v.conn = "" },
			[]string{"port", "--port", "2230"}, "the port this session is connected to is not established"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVM(t)
			if tc.setup != nil {
				tc.setup(v)
			}
			code, out, errOut := v.do(context.Background(), tc.facts, tc.args...)
			if code != 1 || !strings.Contains(errOut, "refused before anything changed") || !strings.Contains(errOut, tc.want) {
				t.Fatalf("exit %d, want 1 refused (%q); stderr %s\nstdout %s", code, tc.want, errOut, out)
			}
			if ch := v.changes(); len(ch) > 0 {
				t.Errorf("changes before the refusal: %q", ch)
			}
		})
	}
}

// TestPortRollsBack: a reload sshd refuses rolls the change back at once,
// while the session is open, with signals held off around the rollback:
// the file is gone, the firewall rule too, sshd reloaded on 22 alone, the
// timer stopped, the journal cleared.
func TestPortRollsBack(t *testing.T) {
	v := newVM(t)
	v.failReloads = 1
	code, out, errOut := v.do(context.Background(), debian13, "port", "--port", "2230", "--hardn", testHardn)
	if code != 1 || !strings.Contains(errOut, "rolled back") {
		t.Fatalf("exit %d, want 1 rolled back; stderr %s\nstdout %s", code, errOut, out)
	}
	if v.dropIn() != "" {
		t.Error("00-hardn.conf is still there")
	}
	if !slices.Equal(v.allowed, []uint16{22}) || !sameStrings(v.bound, []string{"[::]:22", "0.0.0.0:22"}) {
		t.Errorf("ufw allows %v, sshd listens on %v; want 22 alone", v.allowed, v.bound)
	}
	v.cleared()
	stopped := v.index(func(c hostfake.Call) bool { return c.Op == "run" && c.Argv[0] == "systemctl" && c.Argv[1] == "stop" })
	undone := v.index(runIs("ufw delete allow 2230/tcp"))
	if stopped < 0 || !(v.held >= 0 && v.held <= undone && stopped < v.released) {
		t.Errorf("signals held from call %d to %d; the undo at %d, the timer stopped at %d: want the whole rollback held", v.held, v.released, undone, stopped)
	}
}

// TestFailedAfterArm: each step after Arm that fails rolls the change back
// while the session is open, and leaves the host as it was.
func TestFailedAfterArm(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		setup func(*vm)
		want  string
		file  string // 00-hardn.conf at the end
	}{
		{"ufw refuses the rule", []string{"port", "--port", "2230"}, func(v *vm) {
			v.hook = func(cmd string) (hostfake.Response, bool) {
				return hostfake.Response{Stderr: "ERROR: Could not update running firewall\n", ExitCode: 1}, cmd == "ufw allow 2230/tcp"
			}
		}, "firewall: ", ""},
		{"00-hardn.conf changed since the plan", []string{"port", "--port", "2230"}, func(v *vm) {
			v.hook = func(cmd string) (hostfake.Response, bool) {
				if strings.HasPrefix(cmd, "systemd-run ") {
					v.f.AddFile(sshd.HardnDropIn, []byte("PermitRootLogin no\n"), 0o644, 0, 0)
				}
				return hostfake.Response{}, false
			}
		}, "changed since the plan", "PermitRootLogin no\n"},
		{"sshd -T does not show the values", []string{"keyonly"}, func(v *vm) {
			v.hook = func(cmd string) (hostfake.Response, bool) {
				v.stale = v.stale || cmd == "systemctl reload ssh.service"
				return hostfake.Response{}, false
			}
		}, "check of the values", ""},
		{"MarkApplied cannot list the sshd processes", []string{"port", "--port", "2230"}, func(v *vm) {
			v.f.AddFile("/proc/77/stat", []byte("unreadable\n"), 0o444, 0, 0)
		}, "mark applied", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVM(t)
			tc.setup(v)
			code, out, errOut := v.do(context.Background(), debian13, tc.args...)
			if code != 1 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "(rolled back)") {
				t.Fatalf("exit %d, want 1, %q rolled back; stderr %s\nstdout %s", code, tc.want, errOut, out)
			}
			if v.index(armed) < 0 {
				t.Error("not armed")
			}
			if v.dropIn() != tc.file || !slices.Equal(v.allowed, []uint16{22}) {
				t.Errorf("00-hardn.conf %q, want %q; ufw allows %v, want 22", v.dropIn(), tc.file, v.allowed)
			}
			v.cleared()
		})
	}
}

// TestCancelledDuringReload: Ctrl-C while the forward reload runs, which
// leaves ssh.service down. The reload is journalled again without IfUp
// under its own context, so the rollback starts the unit.
func TestCancelledDuringReload(t *testing.T) {
	v := newVM(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	v.hook = func(cmd string) (hostfake.Response, bool) {
		if cmd != "systemctl reload ssh.service" || v.down {
			return hostfake.Response{}, false
		}
		cancel() // SIGINT reaches p2drive and the systemctl it runs
		v.down = true
		return hostfake.Response{Stderr: "signal: interrupt\n", ExitCode: -1}, true
	}
	code, out, errOut := v.do(ctx, debian13, "port", "--port", "2230")
	if code != 1 || !strings.Contains(errOut, "(rolled back)") {
		t.Fatalf("exit %d, want 1 rolled back; stderr %s\nstdout %s", code, errOut, out)
	}
	if v.down || v.index(runIs("systemctl start ssh.service")) < 0 {
		t.Errorf("ssh.service down %v: the rollback did not start it", v.down)
	}
	if v.dropIn() != "" || !slices.Equal(v.allowed, []uint16{22}) {
		t.Errorf("00-hardn.conf %q, ufw allows %v; want gone and 22", v.dropIn(), v.allowed)
	}
	v.cleared()
}

// TestPVERoot: PermitRootLogin prohibit-password on Proxmox VE, through
// pve.PlanSSH over the whole of 00-hardn.conf, and pve.CheckSSH after the
// reload, whose three answers must each be yes.
func TestPVERoot(t *testing.T) {
	pveMain := sshd.IncludeLine + "\n\nPermitRootLogin yes\nUsePAM yes\n"
	for _, tc := range []struct {
		name  string
		files map[string]string
		code  int
		want  string // in stdout (0) or stderr (1)
		file  string // 00-hardn.conf at the end
	}{
		{"stock", nil, 0, "Proxmox VE: root key-only yes, root by key yes, port 22 yes",
			dropInHeader + setMarker + "\nPermitRootLogin prohibit-password\n"},
		{"a kept AllowUsers gets root", map[string]string{sshd.HardnDropIn: "# Managed by hardn.\n# From /etc/ssh/sshd_config.d/manage.conf:\nAllowUsers alice\nPubkeyAuthentication yes\n"}, 0,
			"Proxmox VE: root key-only yes, root by key yes, port 22 yes",
			"# Managed by hardn.\n" + setMarker + "\nPermitRootLogin prohibit-password\nAllowUsers alice root\n# From /etc/ssh/sshd_config.d/manage.conf:\nPubkeyAuthentication yes\n"},
		{"another drop-in keeps root out", map[string]string{sshd.DropInDir + "/50-site.conf": "AllowUsers alice\n"}, 1,
			"root by key no", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVM(t)
			v.f.AddFile(sshd.MainConfig, []byte(pveMain), 0o644, 0, 0)
			for p, d := range tc.files {
				v.f.AddFile(p, []byte(d), 0o644, 0, 0)
			}
			v.boot()
			old := v.dropIn()
			code, out, errOut := v.do(context.Background(), proxmox9(), "pve-root")
			if got := map[int]string{0: out, 1: errOut}[code]; code != tc.code || !strings.Contains(got, tc.want) {
				t.Fatalf("exit %d, want %d with %q; stderr %s\nstdout %s", code, tc.code, tc.want, errOut, out)
			}
			if tc.code == 1 {
				tc.file = old
				v.cleared()
			}
			if v.dropIn() != tc.file {
				t.Errorf("00-hardn.conf\n%s\nwant\n%s", v.dropIn(), tc.file)
			}
			if v.index(func(c hostfake.Call) bool { return c.Op == "run" && c.Argv[0] == "ufw" }) >= 0 {
				t.Error("ufw was run on Proxmox VE")
			}
		})
	}
}

// v032Debian is a Debian 13 host v0.3.2 hardened: manage.conf's
// ListenAddress 0.0.0.0:2230 in force, listen.conf inert.
func v032Debian(t *testing.T) *vm {
	v := newVM(t)
	v.f.AddFile(migrate.ManageConf, fixture(t, "v032-manage.conf"), 0o644, 0, 0)
	v.f.AddFile(migrate.ListenConf, fixture(t, "v032-listen.conf"), 0o644, 0, 0)
	v.allowed = []uint16{2230}
	v.conn = conn2230
	v.saveRules()
	v.boot()
	return v
}

// TestMigrate: the R8 migration on Debian, as a dry run, applied, and
// rolled back when the reload fails.
func TestMigrate(t *testing.T) {
	manage := fixture(t, "v032-manage.conf")
	t.Run("a dry run changes nothing", func(t *testing.T) {
		v := v032Debian(t)
		code, out, errOut := v.do(context.Background(), debian13, "migrate")
		if code != 0 || !strings.Contains(out, "legacy: manage.conf (hardn v0.3.x) "+migrate.ManageConf) || !strings.Contains(out, "--plan-only: nothing changed") {
			t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
		}
		if ch := v.changes(); len(ch) > 0 {
			t.Errorf("a dry run changed the host: %q", ch)
		}
	})
	t.Run("--apply", func(t *testing.T) {
		v := v032Debian(t)
		code, out, errOut := v.do(context.Background(), debian13, "migrate", "--apply")
		if code != 0 {
			t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
		}
		for _, line := range []string{"ListenAddress 0.0.0.0:2230\n", "AuthorizedKeysFile .ssh/authorized_keys .ssh_%u/authorized_keys\n"} {
			if !strings.Contains(v.dropIn(), line) {
				t.Errorf("00-hardn.conf lacks %q:\n%s", line, v.dropIn())
			}
		}
		for _, p := range []string{migrate.ManageConf, migrate.ListenConf} {
			if _, ok := v.f.Entry(p); ok {
				t.Errorf("%s is still there", p)
			}
		}
		j := v.journal(revert.PendingName)
		// p2drive's backups of Plan.Sources come first; migrate.Apply's own
		// of the same files follow in the same run, after the journal.
		if j.Applied == nil || j.Backup == nil || !slices.Equal(j.Backup.Seqs, []int{1, 2, 3}) || len(j.Reload) != 1 || j.Reload[0].IfUp {
			t.Errorf("journal applied %v, backup %+v, reload %+v", j.Applied != nil, j.Backup, j.Reload)
		}
		if !sameStrings(v.bound, []string{"0.0.0.0:2230"}) || v.index(runIs("systemctl reload ssh.service")) < 0 {
			t.Errorf("sshd listens on %v, want 0.0.0.0:2230, reloaded", v.bound)
		}
	})
	for _, tc := range []struct {
		name  string
		setup func(*vm)
		want  string
	}{
		{"the reload refused", func(v *vm) { v.failReloads = 1 }, "reload: "},
		// sshd -T after the reload leaves 00-hardn.conf out: what Check
		// showed of the migrated copy is not what sshd shows.
		{"sshd -T not showing the migration", func(v *vm) {
			v.hook = func(cmd string) (hostfake.Response, bool) {
				v.stale = v.stale || cmd == "systemctl reload ssh.service"
				return hostfake.Response{}, false
			}
		}, "does not show the migrated configuration"},
	} {
		t.Run("--apply, "+tc.name+": rolled back", func(t *testing.T) {
			v := v032Debian(t)
			tc.setup(v)
			code, out, errOut := v.do(context.Background(), debian13, "migrate", "--apply")
			if code != 1 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "(rolled back)") {
				t.Fatalf("exit %d, want 1, %q rolled back; stderr %s\nstdout %s", code, tc.want, errOut, out)
			}
			if e, ok := v.f.Entry(migrate.ManageConf); !ok || !bytes.Equal(e.Data, manage) || v.dropIn() != "" {
				t.Errorf("manage.conf restored %v, 00-hardn.conf %q; want manage.conf back and no 00-hardn.conf", ok, v.dropIn())
			}
			if _, ok := v.f.Entry(migrate.ListenConf); !ok {
				t.Error("listen.conf not restored")
			}
			v.cleared()
		})
	}
}

// v032Files puts v0.3.2's manage.conf and listen.conf on an Ubuntu host
// whose ssh.socket holds the listeners, without a reboot: the test sets
// what the socket is configured for and bound on.
func v032Files(v *vm) {
	v.socket = true
	v.f.AddFile(detect.SocketGenerator, []byte("ELF"), 0o755, 0, 0)
	v.f.AddFile(migrate.ManageConf, fixture(v.t, "v032-manage.conf"), 0o644, 0, 0)
	v.f.AddFile(migrate.ListenConf, fixture(v.t, "v032-listen.conf"), 0o644, 0, 0)
}

// v032Ubuntu is an Ubuntu 26.04 host v0.3.2 hardened and rebooted since:
// ssh.socket on listen.conf's [::]:2230, sshd-socket-generator installed
// (a rollback detects the SSH facts again), ufw allowing 22 and 2230.
func v032Ubuntu(t *testing.T) *vm {
	v := socketVM(t)
	v.f.AddFile(detect.SocketGenerator, []byte("ELF"), 0o755, 0, 0)
	v.f.AddFile(migrate.ManageConf, fixture(t, "v032-manage.conf"), 0o644, 0, 0)
	v.f.AddFile(migrate.ListenConf, fixture(t, "v032-listen.conf"), 0o644, 0, 0)
	v.allowed = []uint16{22, 2230}
	v.conn = conn2230
	v.saveRules()
	v.boot()
	return v
}

// TestMigrateSocket: the R8 migration on Ubuntu's ssh.socket, listen.conf
// included. Applied, the journal's reload names listen.conf as restored and
// carries the socket's listener before the change. Rolled back when sshd -T
// does not show the migration: listen.conf and manage.conf back,
// 00-hardn.conf gone, then daemon-reload, the socket's configuration
// checked and ssh.socket restarted on listen.conf's [::]:2230.
func TestMigrateSocket(t *testing.T) {
	listenConf := fixture(t, "v032-listen.conf")
	t.Run("--apply", func(t *testing.T) {
		v := v032Ubuntu(t)
		code, out, errOut := v.do(context.Background(), ubuntu2604(), "migrate", "--apply")
		if code != 0 {
			t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
		}
		if _, ok := v.f.Entry(migrate.ListenConf); ok {
			t.Error("listen.conf is still there")
		}
		j := v.journal(revert.PendingName)
		if j.Applied == nil || len(j.Reload) != 1 || j.Reload[0].IfUp || !slices.Equal(j.Reload[0].SocketDropIns, []string{migrate.ListenConf}) ||
			len(j.Reload[0].After) != 1 || j.Reload[0].After[0].String() != "[::]:2230" {
			t.Errorf("journal applied %v, reload %+v; want listen.conf restored and [::]:2230 after", j.Applied != nil, j.Reload)
		}
		if !sameStrings(v.bound, []string{"0.0.0.0:2230", "[::]:2230"}) || v.index(runIs("systemctl restart ssh.socket")) < 0 {
			t.Errorf("ssh.socket listens on %v, want 0.0.0.0:2230 and [::]:2230, restarted", v.bound)
		}
	})
	t.Run("--apply, sshd -T not showing the migration: rolled back", func(t *testing.T) {
		v := v032Ubuntu(t)
		v.hook = func(cmd string) (hostfake.Response, bool) {
			v.stale = v.stale || cmd == "systemctl restart ssh.socket"
			return hostfake.Response{}, false
		}
		code, out, errOut := v.do(context.Background(), ubuntu2604(), "migrate", "--apply")
		if code != 1 || !strings.Contains(errOut, "does not show the migrated configuration") || !strings.Contains(errOut, "(rolled back)") {
			t.Fatalf("exit %d, want 1 rolled back; stderr %s\nstdout %s", code, errOut, out)
		}
		if e, ok := v.f.Entry(migrate.ListenConf); !ok || !bytes.Equal(e.Data, listenConf) {
			t.Errorf("listen.conf restored %v", ok)
		}
		if _, ok := v.f.Entry(migrate.ManageConf); !ok || v.dropIn() != "" {
			t.Errorf("manage.conf restored %v, 00-hardn.conf %q", ok, v.dropIn())
		}
		last := func(match func(hostfake.Call) bool) int {
			calls := v.f.Calls()
			for i := len(calls) - 1; i >= 0; i-- {
				if match(calls[i]) {
					return i
				}
			}
			return -1
		}
		order := []int{last(writeOf(migrate.ListenConf)), last(runIs("systemctl daemon-reload")),
			last(runIs("systemctl show -p Listen ssh.socket")), last(runIs("systemctl restart ssh.socket"))}
		if !slices.IsSorted(order) || order[0] < 0 {
			t.Errorf("the rollback's restore of listen.conf, daemon-reload, check and restart at %v; want ascending", order)
		}
		if !sameStrings(v.bound, []string{"[::]:2230"}) || !sameStrings(v.configured, []string{"[::]:2230"}) {
			t.Errorf("ssh.socket configured %v, listens on %v; want listen.conf's [::]:2230", v.configured, v.bound)
		}
		v.cleared()
	})
}

func TestBroken(t *testing.T) {
	v := newVM(t)
	code, out, errOut := v.do(context.Background(), debian13, "broken")
	if code != 0 {
		t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
	}
	if !strings.Contains(out, "refused: "+sshd.HardnDropIn+" line 4") {
		t.Errorf("stdout does not name the real file and line:\n%s", out)
	}
	if ch := v.changes(); len(ch) > 0 {
		t.Errorf("a change outside the run directory: %q", ch)
	}
}

// TestSudoers: the grant, journalled as a plan step before anything is
// written, checked with visudo before it is written, and the journal cleared
// once applied (a change of no lockout risk); then --grant none, a
// lockout-risk change under confirm-or-revert, which the timer's rollback
// undoes from the journal alone, through plans.Registry().
func TestSudoers(t *testing.T) {
	v := newVM(t)
	grant := "/etc/sudoers.d/hardn-grant-alice"
	pending := path.Join(revert.DefaultDir, revert.PendingName)
	if code, out, errOut := v.do(context.Background(), debian13, "sudoers", "--user", "alice", "--grant", "nopasswd"); code != 0 {
		t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
	}
	e, ok := v.f.Entry(grant)
	if !ok || !strings.Contains(string(e.Data), "alice ALL=(ALL) NOPASSWD: ALL") || e.Mode.Perm() != 0o440 {
		t.Fatalf("%s: %q %v", grant, e.Data, e.Mode)
	}
	journalled := v.index(writeOf(pending))
	backedUp := v.index(func(c hostfake.Call) bool {
		return c.Op == "write-atomic" && strings.HasSuffix(c.Path, "manifest.json")
	})
	checked := v.index(func(c hostfake.Call) bool { return c.Op == "run" && c.Argv[0] == "visudo" && c.Argv[1] == "-cf" })
	if written := v.index(writeOf(grant)); !(journalled >= 0 && journalled < backedUp && backedUp < checked && checked < written) {
		t.Errorf("order: journal %d, backup %d, visudo -cf %d, write %d; want ascending", journalled, backedUp, checked, written)
	}
	if v.index(armed) >= 0 {
		t.Error("a timer for a grant of no lockout risk")
	}
	v.cleared()

	if code, out, errOut := v.do(context.Background(), debian13, "sudoers", "--user", "alice", "--grant", "none", "--hardn", testHardn); code != 0 {
		t.Fatalf("--grant none: exit %d; stderr %s\nstdout %s", code, errOut, out)
	}
	if _, ok := v.f.Entry(grant); ok {
		t.Errorf("--grant none left %s", grant)
	}
	j := v.journal(revert.PendingName)
	if j.Risk != revert.RiskLockout || j.Timer == nil || j.Applied == nil || len(j.Undo) != 1 || j.Undo[0].Unit != plans.UnitSudoers {
		t.Fatalf("journal %+v; want a lockout-risk change, armed and applied, with the sudoers plan", j)
	}
	// Not confirmed: the timer's hardn rollback <id>, at the deadline.
	res, err := revert.Rollback(context.Background(), v.f, j.ID, revert.Options{Now: func() time.Time { return j.Deadline() }, Plans: plans.Registry()})
	if err != nil || res.Reason != revert.ReasonTimeout || len(res.Undone) != 1 {
		t.Fatalf("Rollback = %+v, %v", res, err)
	}
	if e, ok := v.f.Entry(grant); !ok || !strings.Contains(string(e.Data), "NOPASSWD: ALL") {
		t.Errorf("%s not restored: %q", grant, e.Data)
	}

	// A grant that asks the password: the check to confirm by is one
	// that asks it, not sudo -n.
	code, out, errOut := v.do(context.Background(), debian13, "sudoers", "--user", "alice", "--grant", "password", "--hardn", testHardn)
	if code != 0 || !strings.Contains(out, "check `sudo -v`, which asks the password") || strings.Contains(out, "sudo -n true") {
		t.Errorf("--grant password: exit %d; stderr %s\nstdout %s", code, errOut, out)
	}
}

// TestSudoersRollsBack: Apply fails after Arm, and the change is rolled
// back at once and its journal cleared, never left as an unfinished
// rollback: after the write (visudo -c refusing the installed grant, Apply
// puts the old files back, and the plan's rollback finds nothing of its
// own left), and before any write (the installed configuration already
// refused, which the plan's rollback would refuse again, so its step
// leaves the journal first).
func TestSudoersRollsBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail int // the visudo -c call that fails; 0: every one
		want string
	}{
		{"after the write", 2, "the old files are back"},
		{"before any write", 0, "before check of /etc/sudoers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVM(t)
			n := 0
			v.hook = func(cmd string) (hostfake.Response, bool) {
				if cmd != "visudo -c" {
					return hostfake.Response{}, false
				}
				if n++; n == tc.fail || tc.fail == 0 {
					return hostfake.Response{Stderr: ">>> /etc/sudoers.d/hardn-grant-alice: syntax error near line 2 <<<\n", ExitCode: 1}, true
				}
				return hostfake.Response{Stdout: "/etc/sudoers: parsed OK\n"}, true
			}
			code, out, errOut := v.do(context.Background(), debian13, "sudoers", "--user", "alice", "--grant", "nopasswd")
			if code != 1 || !strings.Contains(errOut, "(rolled back)") || !strings.Contains(errOut, tc.want) {
				t.Fatalf("exit %d, want 1 rolled back (%q); stderr %s\nstdout %s", code, tc.want, errOut, out)
			}
			if _, ok := v.f.Entry("/etc/sudoers.d/hardn-grant-alice"); ok {
				t.Error("the grant is left")
			}
			v.cleared()
		})
	}
}

// TestUFWEnable: allow before enable (brief 2.3 step 2; ufw.md §2.7), under
// confirm-or-revert, from an inactive ufw with no rules.
func TestUFWEnable(t *testing.T) {
	v := newVM(t)
	v.allowed, v.inactive = nil, true
	v.saveRules()
	if code, out, errOut := v.do(context.Background(), debian13, "ufw-enable"); code != 0 {
		t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
	}
	allow, enable := v.index(runIs("ufw allow 22/tcp")), v.index(runIs("ufw --force enable"))
	if arm := v.index(armed); !(arm >= 0 && arm < allow && allow < enable) {
		t.Errorf("order: armed %d, allow %d, enable %d; want ascending", arm, allow, enable)
	}
	if v.inactive || !slices.Equal(v.allowed, []uint16{22}) {
		t.Errorf("ufw inactive %v, allows %v; want active with 22", v.inactive, v.allowed)
	}
	j := v.journal(revert.PendingName)
	var undo []string
	for _, s := range j.Undo {
		undo = append(undo, strings.Join(s.Argv, " "))
	}
	if want := []string{"ufw disable", "ufw delete allow 22/tcp"}; j.Applied == nil || !slices.Equal(undo, want) || j.Backup != nil || j.Reload != nil {
		t.Errorf("journal applied %v, undo %q (want %q), backup %v, reload %v", j.Applied != nil, undo, want, j.Backup, j.Reload)
	}
}

// TestHoldSignals: SIGINT and SIGHUP ignored and SIGTERM caught until the
// release: this test process survives each, sent to itself.
func TestHoldSignals(t *testing.T) {
	var log bytes.Buffer
	release := holdSignals(&log)
	if !signal.Ignored(syscall.SIGINT) || !signal.Ignored(syscall.SIGHUP) {
		t.Fatal("SIGINT and SIGHUP are not ignored")
	}
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM} {
		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(50 * time.Millisecond) // the process is still here
	release()
	if signal.Ignored(syscall.SIGINT) || signal.Ignored(syscall.SIGHUP) {
		t.Error("release left SIGINT or SIGHUP ignored")
	}
}

// TestFacts: the read-only report changes nothing and prints what
// docs/safety/vmtest.md §4 says to look for.
func TestFacts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		vm    func(*testing.T) *vm
		facts detect.Facts
		want  []string
	}{
		{"Debian 13", newVM, debian13, []string{
			`include: include first, line 1 "Include /etc/ssh/sshd_config.d/*.conf"`,
			"sshd -T now: port [22] (known true)", "ufw: installed true, active true"}},
		{"Ubuntu 26.04", socketVM, ubuntu2604(), []string{"ssh.socket [[::]:22 0.0.0.0:22]", "ssh.socket drop-ins (read true)"}},
		{"v0.3.2 on Debian 13", v032Debian, debian13, []string{
			"legacy: manage.conf (hardn v0.3.x) " + migrate.ManageConf, "legacy: listen.conf (hardn v0.3.x) " + migrate.ListenConf}},
		{"Proxmox VE 9", func(t *testing.T) *vm {
			v := newVM(t)
			v.f.AddFile(sshd.MainConfig, []byte(sshd.IncludeLine+"\n\nPermitRootLogin yes\nUsePAM yes\n"), 0o644, 0, 0)
			v.hook = func(cmd string) (hostfake.Response, bool) {
				// pve-firewall's units, not installed on this fake node
				return hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, strings.Contains(cmd, "pve-firewall")
			}
			return v
		}, proxmox9(), []string{"proxmox ssh: root key-only no, root by key yes, port 22 yes", "ufw: ufw is not used on Proxmox VE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := tc.vm(t)
			code, out, errOut := v.do(context.Background(), tc.facts, "facts")
			if code != 0 {
				t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut, out)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("stdout lacks %q:\n%s", w, out)
				}
			}
			if ch := v.changes(); len(ch) > 0 {
				t.Errorf("facts changed the host: %q", ch)
			}
		})
	}
}
