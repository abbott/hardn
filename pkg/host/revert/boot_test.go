package revert

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// nextBootID is the boot after the reboot.
const nextBootID = "1c6f2e9a-7d3b-4e5f-9a10-fedcba987654"

// rebooted is newChangeBare's port change to 2230 on Debian's ssh.service,
// armed at testNow and applied, then the host rebooted after: a new boot
// id, the transient timer gone, and ssh.service not started yet, since the
// boot unit runs before it. The boot unit is installed (bootHost).
func rebooted(t *testing.T, after time.Duration) *change {
	t.Helper()
	ch := newChangeBare(t)
	markApplied(t, ch.f, ch.c)
	return reboot(t, ch, after)
}

// reboot reboots ch's host, after after: what the boot unit finds.
func reboot(t *testing.T, ch *change, after time.Duration) *change {
	t.Helper()
	f := ch.f
	f.AddFile(bootIDFile, []byte(nextBootID+"\n"), 0o444, 0, 0)
	ch.c.t = testNow.Add(after)
	f.Respond(hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, "systemctl", "is-active", "ssh.service")
	f.AddDir(SystemdUnitDir, 0o755, 0, 0)
	u, _ := PlanBootUnit(systemd, testHardn)
	f.AddFile(u.Path, u.Data, 0o644, 0, 0)
	f.AddSymlink(u.Link, u.Path)
	f.Handler = func(argv []string, _ []byte) hostfake.Response {
		if strings.Join(argv, " ") == strings.Join(u.Disable, " ") {
			_ = f.Remove(context.Background(), u.Link) // as systemctl disable does
			return hostfake.Response{}
		}
		t.Errorf("unexpected command %q", argv)
		return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
	}
	return ch
}

// rearmArgv is the systemd timer re-armed for secs.
func rearmArgv(secs string) []string {
	return []string{"systemd-run", "--unit=hardn-revert-" + testID, "--description=hardn: roll back change " + testID + " unless confirmed",
		"--on-active=" + secs, "--timer-property=AccuracySec=1s", testHardn, "rollback", testID}
}

// noStarts fails when a unit was started or restarted, or the forward
// reload ran: at boot, before sshd's units, nothing is started from the
// boot unit.
func noStarts(t *testing.T, f *hostfake.System) {
	t.Helper()
	for _, c := range f.Calls() {
		if c.Op == "run" && c.Argv[0] == "systemctl" && (c.Argv[1] == "start" || c.Argv[1] == "restart" || c.Argv[1] == "reload") {
			t.Errorf("at boot: %s", c)
		}
	}
}

// Boot, one row per branch: nothing pending, confirmed (also late, after a
// reboot), re-armed with the time left, past the deadline (rolled back at
// once), an apply a reboot interrupted, another binary's journal, one that
// does not say, an untrusted clock, the timer started in this boot, a
// change with no timer, a rollback unfinished or running, a timer that does
// not start.
func TestBoot(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T) *change
		opts  func(o *Options)
		in    *detect.Init // nil: systemd
		want  BootAction
		why   string // in BootResult.Why
		err   error
		check func(t *testing.T, ch *change, res BootResult)
	}{
		{"nothing pending: the boot unit is removed", func(t *testing.T) *change {
			ch := rebooted(t, 10*time.Second)
			if err := ch.f.Remove(context.Background(), stateFile(PendingName)); err != nil {
				t.Fatal(err)
			}
			return ch
		}, nil, nil, BootNothing, "no change is journalled", nil, func(t *testing.T, ch *change, res BootResult) {
			if !res.Removed || res.RemoveErr != nil || exists(ch.f, SystemdUnitDir+"/hardn-boot.service") || exists(ch.f, SystemdWants+"/hardn-boot.service") {
				t.Errorf("the boot unit is still there (removed %v, %v)", res.Removed, res.RemoveErr)
			}
		}},
		{"confirmed before the reboot", func(t *testing.T) *change {
			ch := newChangeBare(t)
			markApplied(t, ch.f, ch.c)
			proc(ch.f, newSSHD, listenerPID, "sshd-session", 0)
			if _, err := Confirm(context.Background(), ch.f, newSession, ch.opts); err != nil {
				t.Fatal(err)
			}
			return reboot(t, ch, 60*time.Second)
		}, nil, nil, BootNothing, "no change is journalled; the last change confirmed is " + testID, nil, func(t *testing.T, ch *change, res BootResult) {
			if got := ch.dropIn(t); got != applied {
				t.Errorf("00-hardn.conf = %q: a confirmed change was undone", got)
			}
		}},
		{"confirmed late, after a reboot", func(t *testing.T) *change {
			ch := rebooted(t, time.Hour)
			proc(ch.f, newSSHD, listenerPID, "sshd-session", 0)
			if _, err := Confirm(context.Background(), ch.f, newSession, ch.opts); err != nil {
				t.Fatal(err)
			}
			ch.c.t = ch.c.t.Add(time.Hour) // and rebooted again
			return ch
		}, nil, nil, BootNothing, "the last change confirmed is " + testID, nil, func(t *testing.T, ch *change, res BootResult) {
			if got := ch.dropIn(t); got != applied {
				t.Errorf("00-hardn.conf = %q: a confirmed change was undone", got)
			}
		}},
		{"inside the window: re-armed for what remains", func(t *testing.T) *change {
			ch := rebooted(t, 100*time.Second+500*time.Millisecond)
			respond(ch.f, hostfake.Response{}, rearmArgv("80s"))
			return ch
		}, nil, nil, BootRearmed, "re-armed for what remains of its window, 1m20s: it is rolled back at 2026-10-18 12:03:00 UTC", nil,
			func(t *testing.T, ch *change, res BootResult) {
				j := readJournalT(t, ch.f, stateFile(PendingName))
				if j.Timer.Boot != nextBootID || !slices.Equal(j.Timer.Argv, rearmArgv("80s")) || !j.Deadline().Equal(testNow.Add(DefaultTimeout)) ||
					j.Timer.HardnSHA256 != res.Change.Timer.HardnSHA256 || res.For != 80*time.Second {
					t.Errorf("re-armed timer %+v, for %s", j.Timer, res.For)
				}
				if got := ch.dropIn(t); got != applied {
					t.Errorf("00-hardn.conf = %q: changed by a re-arm", got)
				}
				// A confirmation from a new connection after the reboot
				// keeps it and stops the re-armed timer.
				proc(ch.f, newSSHD, listenerPID, "sshd-session", 0)
				ch.f.Respond(hostfake.Response{}, "systemctl", "stop", "hardn-revert-"+testID+".timer")
				if cr, err := Confirm(context.Background(), ch.f, newSession, ch.opts); err != nil || cr.CancelErr != nil {
					t.Errorf("Confirm after the reboot: %+v, %v", cr, err)
				}
				if !strings.Contains(runs(ch.f), "run systemctl stop hardn-revert-"+testID+".timer") {
					t.Errorf("the re-armed timer was not stopped:\n%s", runs(ch.f))
				}
			}},
		{"one second left", func(t *testing.T) *change {
			ch := rebooted(t, 179*time.Second+900*time.Millisecond)
			respond(ch.f, hostfake.Response{}, rearmArgv("1s"))
			return ch
		}, nil, nil, BootRearmed, "1s", nil, nil},
		{"past the deadline: rolled back at once, starting nothing", func(t *testing.T) *change {
			ch := rebooted(t, 10*time.Minute)
			ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
			return ch
		}, nil, nil, BootRolledBack, "its deadline, 2026-10-18 12:03:00 UTC, has passed: rolled back", nil, func(t *testing.T, ch *change, res BootResult) {
			if got := ch.dropIn(t); got != before {
				t.Errorf("00-hardn.conf = %q, want the backup", got)
			}
			noStarts(t, ch.f)
			r := res.Rollback
			if r == nil || r.Reason != ReasonTimeout || len(r.Reloads) != 1 || r.Reloads[0].Steps[0].Ran || r.Reloads[0].Steps[0].NotUp == "" {
				t.Errorf("rollback %+v", r)
			}
			st, err := ReadState(context.Background(), ch.f, ch.opts)
			if err != nil || st.Busy() || st.Reverted == nil || st.Reverted.ID != testID {
				t.Errorf("state %+v, %v: want the outcome for the next run's notice", st, err)
			}
			if !res.Removed || res.RemoveErr != nil {
				t.Errorf("the boot unit is kept: %v", res.RemoveErr)
			}
		}},
		{"its apply did not finish: rolled back at once", func(t *testing.T) *change {
			ch := reboot(t, newChangeBare(t), 30*time.Second)
			ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
			return ch
		}, nil, nil, BootRolledBack, "its apply did not finish before the reboot", nil, func(t *testing.T, ch *change, res BootResult) {
			if got := ch.dropIn(t); got != before {
				t.Errorf("00-hardn.conf = %q, want the backup", got)
			}
			noStarts(t, ch.f)
		}},
		{"another hardn binary's journal (its path)", func(t *testing.T) *change { return rebooted(t, 30*time.Second) },
			func(o *Options) { o.Hardn = "/usr/sbin/hardn" }, nil, BootLeft, "its timer runs /usr/local/bin/hardn, not this hardn (/usr/sbin/hardn)", nil, nil},
		{"another hardn binary's journal (its content)", func(t *testing.T) *change {
			ch := rebooted(t, 30*time.Second)
			ch.f.AddFile(testHardn, []byte("ELF, a later build"), 0o755, 0, 0)
			return ch
		}, nil, nil, BootLeft, "/usr/local/bin/hardn is not the binary that wrote the journal", nil, nil},
		{"a journal from before next/p3-revert-boot", func(t *testing.T) *change {
			ch := rebooted(t, 30*time.Second)
			old, err := os.ReadFile(filepath.Join("testdata", "pending-p2.json"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeJournal(old); err != nil {
				t.Fatalf("a Phase 2 journal no longer decodes: %v", err)
			}
			ch.f.AddFile(stateFile(PendingName), old, 0o600, 0, 0)
			return ch
		}, nil, nil, BootLeft, "its journal does not say which hardn binary its timer runs", nil, nil},
		{"no boot id recorded", func(t *testing.T) *change {
			ch := rebooted(t, 30*time.Second)
			editPending(t, ch, func(j *Journal) { j.Timer.Boot = "" })
			return ch
		}, nil, nil, BootLeft, "does not say in which boot its timer was started", nil, nil},
		{"the clock reads before the change: the whole window", func(t *testing.T) *change {
			ch := rebooted(t, -time.Hour)
			respond(ch.f, hostfake.Response{}, rearmArgv("180s"))
			return ch
		}, nil, nil, BootRearmed, "before the change was made (2026-10-18 12:00:00 UTC), so what remains of its window cannot be told: its timer is re-armed for the whole window, 3m0s",
			nil, func(t *testing.T, ch *change, res BootResult) {
				if !res.ClockUntrusted || res.For != DefaultTimeout || !res.Timer.Deadline.Equal(testNow.Add(-time.Hour+DefaultTimeout)) {
					t.Errorf("untrusted %v, for %s, deadline %v", res.ClockUntrusted, res.For, res.Timer.Deadline)
				}
				j := readJournalT(t, ch.f, stateFile(PendingName))
				if !j.Deadline().Equal(res.Timer.Deadline) {
					t.Errorf("the journal's deadline %v, want the re-armed timer's", j.Deadline())
				}
			}},
		{"its timer was started in this boot", func(t *testing.T) *change {
			ch := rebooted(t, 30*time.Second)
			ch.f.AddFile(bootIDFile, []byte(testBootID+"\n"), 0o444, 0, 0)
			return ch
		}, nil, nil, BootNothing, "its timer was started in this boot", nil, func(t *testing.T, ch *change, res BootResult) {
			if res.Removed || !exists(ch.f, SystemdUnitDir+"/hardn-boot.service") {
				t.Error("the boot unit was removed with a change journalled")
			}
		}},
		{"a change with no timer", func(t *testing.T) *change {
			ch := rebooted(t, 30*time.Second)
			if err := ch.f.Remove(context.Background(), stateFile(PendingName)); err != nil {
				t.Fatal(err)
			}
			j := Journal{ID: otherID, Risk: RiskOther, Summary: "DNS resolvers on web-01.example.net"}
			if _, err := Arm(context.Background(), ch.f, j, systemd, ch.opts); err != nil {
				t.Fatal(err)
			}
			return ch
		}, nil, nil, BootNothing, "it has no timer, and did not finish", nil, func(t *testing.T, ch *change, res BootResult) {
			if res.Removed {
				t.Error("the boot unit was removed with a change journalled")
			}
		}},
		{"a rollback that did not finish", func(t *testing.T) *change {
			ch := rebooted(t, 30*time.Second)
			editPending(t, ch, func(j *Journal) {
				j.Rollback = &Progress{Started: testNow, Reason: ReasonTimeout, Runner: ProcStart{PID: 7000, Start: 700000, Boot: testBootID}}
			})
			if err := ch.f.Rename(context.Background(), stateFile(PendingName), stateFile(RollbackName)); err != nil {
				t.Fatal(err)
			}
			return ch
		}, nil, nil, BootLeft, "its rollback did not finish, and is left to its own path: run `sudo hardn rollback " + testID + "`", nil,
			func(t *testing.T, ch *change, res BootResult) {
				if got := ch.dropIn(t); got != applied {
					t.Errorf("00-hardn.conf = %q: touched", got)
				}
			}},
		{"a rollback that runs", func(t *testing.T) *change {
			ch := rebooted(t, 30*time.Second)
			proc(ch.f, 7000, 1, "hardn", 0)
			editPending(t, ch, func(j *Journal) {
				j.Rollback = &Progress{Started: testNow, Reason: ReasonRequested, Runner: ProcStart{PID: 7000, Start: procStart(7000), Boot: nextBootID}}
			})
			if err := ch.f.Rename(context.Background(), stateFile(PendingName), stateFile(RollbackName)); err != nil {
				t.Fatal(err)
			}
			return ch
		}, nil, nil, BootNothing, "its rollback runs (process 7000)", nil, nil},
		{"the timer does not start: rolled back at once", func(t *testing.T) *change {
			ch := rebooted(t, 30*time.Second)
			respond(ch.f, hostfake.Response{Stderr: "Failed to start transient timer unit\n", ExitCode: 1}, rearmArgv("150s"))
			ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
			return ch
		}, nil, nil, BootRolledBack, "its timer did not start", nil, func(t *testing.T, ch *change, res BootResult) {
			if got := ch.dropIn(t); got != before {
				t.Errorf("00-hardn.conf = %q, want the backup", got)
			}
		}},
		{"no timer for the init system now: rolled back at once", func(t *testing.T) *change {
			ch := rebooted(t, 30*time.Second)
			ch.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
			return ch
		}, nil, &detect.Init{Kind: detect.Systemd, Running: detect.Unknown}, BootRolledBack, "its timer cannot be re-armed", nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ch := tt.setup(t)
			opts := ch.opts
			if tt.opts != nil {
				tt.opts(&opts)
			}
			in := systemd
			if tt.in != nil {
				in = *tt.in
			}
			ch.log.Reset()
			n := len(ch.f.Calls())
			res, err := Boot(ctx, ch.f, in, opts)
			if !errors.Is(err, tt.err) || (tt.err == nil) != (err == nil) {
				t.Fatalf("Boot: %v, want %v\nlog:\n%s", err, tt.err, ch.log)
			}
			if res.Action != tt.want || !strings.Contains(res.Why, tt.why) {
				t.Errorf("Boot = %s: %q\nwant %s: %q\nlog:\n%s", res.Action, res.Why, tt.want, tt.why, ch.log)
			}
			if !strings.Contains(ch.log.String(), "revert: boot: ") || !strings.Contains(ch.log.String(), string(res.Action)) {
				t.Errorf("log lacks the decision:\n%s", ch.log)
			}
			if res.Action == BootLeft || res.Action == BootNothing {
				for _, c := range ch.f.Calls()[n:] {
					if c.Op == "run" && (c.Argv[0] == "systemd-run" || c.Argv[0] == "ufw") {
						t.Errorf("%s: %s", res.Action, c)
					}
				}
			}
			if tt.check != nil {
				tt.check(t, ch, res)
			}
		})
	}
}

// editPending rewrites ch's pending.json through edit, as a test seam.
func editPending(t *testing.T, ch *change, edit func(j *Journal)) {
	t.Helper()
	j := readJournalT(t, ch.f, stateFile(PendingName))
	edit(&j)
	data, err := encode(j)
	if err != nil {
		t.Fatal(err)
	}
	ch.f.AddFile(stateFile(PendingName), data, 0o600, 0, 0)
}

// A boot on a socket host past the deadline: the files restored, then
// daemon-reload and the socket's configuration checked (the generator reads
// the restored files), and ssh.socket, not started yet, left for its own
// start, which binds what the restored files say; with ssh.socket up after
// all (the unit's order not applied), it is restarted, not started.
func TestBootSocketHost(t *testing.T) {
	ctx := context.Background()
	for _, up := range []bool{false, true} {
		ch := rebooted(t, 10*time.Minute)
		f := ch.f
		ch.opts.DetectSSH = func(context.Context, host.System) detect.SSH {
			return detect.SSH{Mode: detect.SSHSocket, Generator: detect.Yes, GeneratorPath: detect.SocketGenerator,
				Socket: detect.UnitState{Enabled: "enabled", Active: "inactive"}, Service: detect.UnitState{Enabled: "disabled", Active: "inactive"}}
		}
		f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
		f.Respond(hostfake.Response{}, "systemctl", "daemon-reload")
		f.Respond(hostfake.Response{Stdout: "Listen=0.0.0.0:22 (Stream)\n"}, "systemctl", "show", "-p", "Listen", "ssh.socket")
		if up {
			f.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", "ssh.socket")
			f.Respond(hostfake.Response{}, "systemctl", "restart", "ssh.socket")
			f.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", "ssh.socket")
		} else {
			f.Respond(hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, "systemctl", "is-active", "ssh.socket")
		}
		res, err := Boot(ctx, f, systemd, ch.opts)
		if err != nil || res.Action != BootRolledBack {
			t.Fatalf("up %v: Boot = %+v, %v\nlog:\n%s", up, res, err, ch.log)
		}
		got := runs(f)
		restore := slices.IndexFunc(f.Calls(), func(c hostfake.Call) bool { return c.Op == "write-atomic" && c.Path == dropIn })
		reloaded := slices.IndexFunc(f.Calls(), func(c hostfake.Call) bool {
			return c.Op == "run" && strings.Join(c.Argv, " ") == "systemctl daemon-reload"
		})
		if restore < 0 || reloaded < restore {
			t.Errorf("up %v: the files restored at %d, daemon-reload at %d: want the restore first", up, restore, reloaded)
		}
		at := 0
		for _, want := range []string{"run systemctl daemon-reload", "run systemctl show -p Listen ssh.socket", "run systemctl is-active ssh.socket"} {
			i := strings.Index(got[at:], want)
			if i < 0 {
				t.Fatalf("up %v: %q not in order in:\n%s", up, want, got)
			}
			at += i
		}
		if restarted := strings.Contains(got, "run systemctl restart ssh.socket"); restarted != up {
			t.Errorf("up %v: restarted %v:\n%s", up, restarted, got)
		}
		if strings.Contains(got, "systemctl start ") {
			t.Errorf("up %v: a unit started from the boot unit:\n%s", up, got)
		}
	}
}

// On OpenRC the sleeper is started again for what remains, with its new
// argv in the journal and its pidfile, so that a confirmation after the
// reboot ends it (its command line is the journal's).
func TestBootOpenRC(t *testing.T) {
	ctx := context.Background()
	f := newHost(t)
	c := &clock{testNow}
	sleeper := func(secs string) []string {
		return []string{"/bin/sh", "-c", SleeperScript, testHardn, secs, testID}
	}
	respond(f, hostfake.Response{PID: 4321}, sleeper("180"))
	if _, err := Arm(ctx, f, lockout(), openrc, testOptions(c, nil)); err != nil {
		t.Fatal(err)
	}
	markApplied(t, f, c)
	// The reboot: /run is gone with the pidfile, the boot id is new.
	if err := f.RemoveAll(ctx, DefaultRunDir); err != nil {
		t.Fatal(err)
	}
	f.AddDir("/run", 0o755, 0, 0)
	f.AddFile(bootIDFile, []byte(nextBootID+"\n"), 0o444, 0, 0)
	c.t = testNow.Add(60 * time.Second)
	respond(f, hostfake.Response{PID: 812}, sleeper("120"))
	res, err := Boot(ctx, f, openrc, testOptions(c, nil))
	if err != nil || res.Action != BootRearmed || res.For != 120*time.Second {
		t.Fatalf("Boot = %+v, %v", res, err)
	}
	j := readJournalT(t, f, stateFile(PendingName))
	pidfile := DefaultRunDir + "/revert-" + testID + ".pid"
	if e, ok := f.Entry(pidfile); !ok || string(e.Data) != "812\n" || !slices.Equal(j.Timer.Argv, sleeper("120")) || j.Timer.PIDFile != pidfile {
		t.Errorf("pidfile %q, timer %+v", e.Data, j.Timer)
	}
	f.AddFile("/proc/812/cmdline", []byte(strings.Join(sleeper("120"), "\x00")+"\x00"), 0o444, 0, 0)
	f.Respond(hostfake.Response{}, "kill", "-s", "TERM", "812")
	proc(f, newSSHD, listenerPID, "sshd-session", 0)
	if cr, err := Confirm(ctx, f, newSession, testOptions(c, nil)); err != nil || cr.CancelErr != nil {
		t.Fatalf("Confirm = %+v, %v", cr, err)
	}
	if !strings.Contains(runs(f), "run kill -s TERM 812") {
		t.Errorf("the re-armed sleeper was not stopped:\n%s", runs(f))
	}
}

// confirmOnWrite confirms the change the moment Boot writes the re-armed
// journal, before the write: a hardn confirm between Boot's read and its
// write.
type confirmOnWrite struct {
	*hostfake.System
	done bool
}

func (c *confirmOnWrite) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if !c.done && name == stateFile(PendingName) {
		c.done = true
		if err := c.System.Rename(ctx, name, stateFile(ConfirmedName)); err != nil {
			return err
		}
	}
	return c.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

// A confirmation that claims the change while Boot re-arms it wins: the
// pending.json Boot wrote again is removed, and no timer starts.
func TestBootConfirmedMeanwhile(t *testing.T) {
	ch := rebooted(t, 30*time.Second)
	sys := &confirmOnWrite{System: ch.f}
	n := len(ch.f.Calls())
	res, err := Boot(context.Background(), sys, systemd, ch.opts)
	if err != nil || res.Action != BootNothing || !strings.Contains(res.Why, "confirmed while hardn re-armed") {
		t.Fatalf("Boot = %+v, %v", res, err)
	}
	started := slices.ContainsFunc(ch.f.Calls()[n:], func(c hostfake.Call) bool { return c.Op == "run" && c.Argv[0] == "systemd-run" })
	if exists(ch.f, stateFile(PendingName)) || !exists(ch.f, stateFile(ConfirmedName)) || started {
		t.Errorf("pending %v, confirmed %v, timer started %v", exists(ch.f, stateFile(PendingName)), exists(ch.f, stateFile(ConfirmedName)), started)
	}
}

// A journal Boot cannot read is left, with the error.
func TestBootUnreadable(t *testing.T) {
	ch := rebooted(t, 30*time.Second)
	ch.f.AddFile(stateFile(PendingName), []byte(`{"schemaVersion": 2}`), 0o600, 0, 0)
	res, err := Boot(context.Background(), ch.f, systemd, ch.opts)
	if !errors.Is(err, ErrJournal) || res.Action != BootLeft || res.Removed {
		t.Fatalf("Boot = %+v, %v", res, err)
	}
}
