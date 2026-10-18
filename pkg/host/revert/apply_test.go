package revert

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

var update = flag.Bool("update", false, "rewrite testdata/pending.json")

// TestArmSystemd arms a port change on systemd and pins the journal's
// format byte for byte (testdata/pending.json; after an intended format
// change: go test ./pkg/host/revert -run TestArmSystemd -update, and review
// the diff).
func TestArmSystemd(t *testing.T) {
	ctx := context.Background()
	f := newHost(t)
	var log bytes.Buffer
	c := &clock{testNow}
	respond(f, hostfake.Response{}, systemdRunArgv(testID))
	j, err := Arm(ctx, f, lockout(), systemd, testOptions(c, &log))
	if err != nil {
		t.Fatal(err)
	}
	if j.Created != testNow || j.Timer == nil || j.Timer.Deadline != testNow.Add(DefaultTimeout) {
		t.Errorf("armed journal: created %v, timer %+v", j.Created, j.Timer)
	}
	if got := runs(f); got != "run "+strings.Join(quoteAll(systemdRunArgv(testID)), " ")+"\n" {
		t.Errorf("runs:\n%s", got)
	}
	// The journal is written before the timer starts, so a timer that
	// fires at once finds it.
	ws := writes(f)
	if want := []string{"mkdir " + DefaultDir, "write-atomic " + stateFile(PendingName)}; !reflect.DeepEqual(ws, want) {
		t.Errorf("writes = %q, want %q", ws, want)
	}
	if e, _ := f.Entry(DefaultDir); e.Mode.Perm() != 0o700 {
		t.Errorf("%s mode %v, want 0700", DefaultDir, e.Mode)
	}
	if got := readJournalT(t, f, stateFile(PendingName)); !reflect.DeepEqual(got, j) {
		t.Errorf("on disk %+v\nreturned %+v", got, j)
	}
	if !strings.Contains(log.String(), "revert: armed change "+testID) {
		t.Errorf("log = %q", log.String())
	}

	e, _ := f.Entry(stateFile(PendingName))
	golden := filepath.Join("testdata", "pending.json")
	if *update {
		if err := os.WriteFile(golden, e.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(e.Data, want) {
		t.Errorf("pending.json differs from %s:\n%s", golden, e.Data)
	}
	if _, err := decodeJournal(want); err != nil {
		t.Errorf("the golden journal does not decode: %v", err)
	}
}

func quoteAll(argv []string) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if strings.ContainsAny(a, " ") {
			a = `"` + a + `"`
		}
		out[i] = a
	}
	return out
}

func TestArmOpenRC(t *testing.T) {
	ctx := context.Background()
	f := newHost(t)
	c := &clock{testNow}
	j := lockout()
	argv := []string{"/bin/sh", "-c", SleeperScript, testHardn, "180", testID}
	respond(f, hostfake.Response{PID: 4321}, argv)
	got, err := Arm(ctx, f, j, openrc, testOptions(c, nil))
	if err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	var started bool
	for _, call := range calls {
		if call.Op == "start-detached" {
			started = reflect.DeepEqual(call.Argv, argv)
		}
	}
	if !started {
		t.Errorf("no detached start of %q:\n%s", argv, runs(f))
	}
	pidfile := "/run/hardn/revert-" + testID + ".pid"
	if e, ok := f.Entry(pidfile); !ok || string(e.Data) != "4321\n" || e.Mode.Perm() != 0o600 {
		t.Errorf("pidfile %+v, %v", e, ok)
	}
	if got.Timer.PIDFile != pidfile || got.Timer.Kind != TimerSetsid {
		t.Errorf("timer %+v", got.Timer)
	}
}

func TestArmRefuses(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		setup func(t *testing.T, f *hostfake.System, c *clock)
		init  func() Journal
		want  error
	}{
		{"a change is pending", func(t *testing.T, f *hostfake.System, c *clock) {
			armed(t, f, c)
		}, func() Journal { j := lockout(); j.ID = otherID; return j }, ErrPending},
		{"a rollback has not finished", func(t *testing.T, f *hostfake.System, c *clock) {
			armed(t, f, c)
			if err := f.Rename(ctx, stateFile(PendingName), stateFile(RollbackName)); err != nil {
				t.Fatal(err)
			}
		}, func() Journal { j := lockout(); j.ID = otherID; return j }, ErrPending},
		{"below the floor", nil, func() Journal { j := lockout(); j.TimeoutSeconds = 30; return j }, ErrTimeout},
		{"no summary", nil, func() Journal { j := lockout(); j.Summary = ""; return j }, ErrJournal},
		{"a bad undo step", nil, func() Journal {
			j := lockout()
			j.Undo = []Step{{Op: OpRun, Argv: []string{"./ufw", "disable"}}}
			return j
		}, ErrJournal},
		{"an unsafe hardn", func(t *testing.T, f *hostfake.System, c *clock) {
			f.AddFile(testHardn, nil, 0o777, 0, 0)
		}, lockout, ErrExecutable},
		{"a state directory alice owns", func(t *testing.T, f *hostfake.System, c *clock) {
			f.AddDir(DefaultDir, 0o700, 1000, 1000)
		}, lockout, ErrUnsafe},
		{"a symlinked state directory", func(t *testing.T, f *hostfake.System, c *clock) {
			f.AddDir("/srv/state", 0o700, 0, 0)
			f.AddSymlink(DefaultDir, "/srv/state")
		}, lockout, ErrUnsafe},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newHost(t)
			c := &clock{testNow}
			if tt.setup != nil {
				tt.setup(t, f, c)
			}
			before := len(writes(f))
			_, err := Arm(ctx, f, tt.init(), systemd, testOptions(c, nil))
			if !errors.Is(err, tt.want) {
				t.Fatalf("Arm = %v, want %v", err, tt.want)
			}
			if w := writes(f)[before:]; len(w) > 0 && w[len(w)-1] != "mkdir "+DefaultDir {
				t.Errorf("wrote %q after the refusal", w)
			}
		})
	}
	// The refusal says how to go on.
	f := newHost(t)
	c := &clock{testNow}
	armed(t, f, c)
	j := lockout()
	j.ID = otherID
	_, err := Arm(ctx, f, j, systemd, testOptions(c, nil))
	var pe *PendingError
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "sudo hardn confirm") || !strings.Contains(err.Error(), "sudo hardn rollback "+testID) {
		t.Errorf("PendingError = %v", err)
	}
}

func TestArmNoTimer(t *testing.T) {
	f := newHost(t)
	_, err := Arm(context.Background(), f, lockout(), openrcNotRunning(), testOptions(&clock{testNow}, nil))
	if !errors.Is(err, ErrNoTimer) || exists(f, stateFile(PendingName)) || len(writes(f)) != 0 {
		t.Fatalf("Arm = %v, writes %q; want ErrNoTimer and nothing written", err, writes(f))
	}
}

func TestArmTimerFails(t *testing.T) {
	ctx := context.Background()
	// systemd-run fails: the journal is removed again.
	f := newHost(t)
	respond(f, hostfake.Response{ExitCode: 1, Stderr: "Failed to start transient timer unit"}, systemdRunArgv(testID))
	_, err := Arm(ctx, f, lockout(), systemd, testOptions(&clock{testNow}, nil))
	if err == nil || !strings.Contains(err.Error(), "the timer did not start") || exists(f, stateFile(PendingName)) {
		t.Fatalf("Arm = %v, pending left %v; want the error and no journal", err, exists(f, stateFile(PendingName)))
	}
	// OpenRC: the sleeper started, its pidfile could not be written: it is
	// stopped and the journal removed.
	f = newHost(t)
	argv := []string{"/bin/sh", "-c", SleeperScript, testHardn, "180", testID}
	respond(f, hostfake.Response{PID: 4321}, argv)
	f.Respond(hostfake.Response{}, "kill", "-s", "TERM", "4321")
	f.AddDir("/run/hardn/revert-"+testID+".pid", 0o700, 0, 0) // a directory where the pidfile goes
	_, err = Arm(ctx, f, lockout(), openrc, testOptions(&clock{testNow}, nil))
	if err == nil || exists(f, stateFile(PendingName)) || !strings.Contains(runs(f), "run kill -s TERM 4321") {
		t.Fatalf("Arm = %v; runs:\n%s", err, runs(f))
	}
}

func openrcNotRunning() detect.Init {
	return detect.Init{Kind: detect.OpenRC, Running: detect.No, Source: detect.OpenRCRun}
}

func newRealT(t *testing.T) host.System {
	t.Helper()
	return host.NewReal(host.Options{})
}

func TestArmOther(t *testing.T) {
	ctx := context.Background()
	f := newHost(t)
	opts := testOptions(&clock{testNow}, nil)
	j := Journal{ID: testID, Risk: RiskOther, Summary: "DNS resolvers 192.0.2.53"}
	got, err := Arm(ctx, f, j, systemd, opts)
	if err != nil || got.Timer != nil || runs(f) != "" {
		t.Fatalf("Arm = %+v, %v; runs %q; want no timer", got, err, runs(f))
	}
	if err := Done(ctx, f, otherID, opts); !errors.Is(err, ErrNothingPending) {
		t.Errorf("Done(other id) = %v", err)
	}
	if err := Done(ctx, f, testID, opts); err != nil || exists(f, stateFile(PendingName)) {
		t.Errorf("Done = %v, pending left %v", err, exists(f, stateFile(PendingName)))
	}
	// A lockout change ends with confirm or rollback.
	f = newHost(t)
	armed(t, f, &clock{testNow})
	if err := Done(ctx, f, testID, opts); !errors.Is(err, ErrNotConfirmable) {
		t.Errorf("Done(lockout) = %v", err)
	}
}

func TestRecord(t *testing.T) {
	ctx := context.Background()
	f := newHost(t)
	c := &clock{testNow}
	opts := testOptions(c, nil)
	j := armed(t, f, c)

	j.Backup = &Backups{Dir: "/var/backups/hardn/" + testID, Seqs: []int{1, 2}}
	j.Undo = []Step{{Op: OpRun, Argv: []string{"ufw", "delete", "allow", "2230/tcp"}, Why: "the rule p2-ufw added"}}
	j.Reload = []Reload{{Service: ReloadSSH, Keywords: []string{"Port"}, After: []Listener{{Addr: any2230.Addr, Port: 22}}}}
	// What Arm set cannot be changed by a Record.
	j.TimeoutSeconds, j.Timer = 600, nil
	if err := Record(ctx, f, j, opts); err != nil {
		t.Fatal(err)
	}
	got := readJournalT(t, f, stateFile(PendingName))
	if got.TimeoutSeconds != 180 || got.Timer == nil || got.Backup == nil || len(got.Undo) != 1 || len(got.Reload) != 1 {
		t.Errorf("recorded %+v", got)
	}
	j.ID = otherID
	if err := Record(ctx, f, j, opts); !errors.Is(err, ErrNothingPending) {
		t.Errorf("Record(other id) = %v", err)
	}
	j.ID = testID
	j.Reload = []Reload{{Service: ReloadSSH, Keywords: []string{"ListenAddress"}}}
	if err := Record(ctx, f, j, opts); !errors.Is(err, ErrJournal) {
		t.Errorf("Record(listener reload without After) = %v", err)
	}
	if err := f.Rename(ctx, stateFile(PendingName), stateFile(RollbackName)); err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, f, got, opts); !errors.Is(err, ErrRollingBack) {
		t.Errorf("Record after a rollback claimed it = %v", err)
	}
}

func TestConfirm(t *testing.T) {
	ctx := context.Background()
	stop := []string{"systemctl", "stop", "hardn-revert-" + testID + ".timer"}
	newSession := Session{Remote: true, Conn: newConn, Source: "/proc/3990/environ"}

	t.Run("a new connection to the new port", func(t *testing.T) {
		f := newHost(t)
		c := &clock{testNow}
		var log bytes.Buffer
		opts := testOptions(c, &log)
		j := armed(t, f, c)
		respond(f, hostfake.Response{}, stop)
		res, err := Confirm(ctx, f, newSession, opts)
		if err != nil || res.Local || res.CancelErr != nil || res.Journal.ID != testID {
			t.Fatalf("Confirm = %+v, %v", res, err)
		}
		if exists(f, stateFile(PendingName)) || !reflect.DeepEqual(readJournalT(t, f, stateFile(ConfirmedName)), j) {
			t.Error("the journal was not moved to confirmed.json")
		}
		if !strings.Contains(runs(f), "run systemctl stop hardn-revert-"+testID+".timer") {
			t.Errorf("timer not stopped:\n%s", runs(f))
		}
		if !strings.Contains(log.String(), "confirmed change "+testID+" (") {
			t.Errorf("log = %q", log.String())
		}
		// The timer's rollback, had it fired anyway, finds nothing.
		r, err := Rollback(ctx, f, testID, opts)
		if err != nil || !r.Nothing {
			t.Errorf("Rollback after Confirm = %+v, %v", r, err)
		}
		// A second confirm has nothing to confirm.
		if _, err := Confirm(ctx, f, newSession, opts); !errors.Is(err, ErrNothingPending) {
			t.Errorf("second Confirm = %v", err)
		}
	})
	t.Run("the local console, with a timer that will not stop", func(t *testing.T) {
		f := newHost(t)
		c := &clock{testNow}
		armed(t, f, c)
		respond(f, hostfake.Response{ExitCode: 5, Stderr: "Failed to stop hardn-revert.timer: Unit not loaded."}, stop)
		res, err := Confirm(ctx, f, Session{Source: "no SSH variable or sshd among the ancestors"}, testOptions(c, nil))
		if err != nil || !res.Local || res.CancelErr == nil || exists(f, stateFile(PendingName)) {
			t.Fatalf("Confirm = %+v, %v", res, err)
		}
	})
	for _, tt := range []struct {
		name string
		s    Session
		want error
		hint string
	}{
		{"the session that applied it", Session{Remote: true, Conn: applyConn}, ErrWrongListener, "a new SSH connection to port 2230"},
		{"an unknown session", Session{Remote: true}, ErrSessionUnknown, "sudo hardn confirm"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newHost(t)
			c := &clock{testNow}
			armed(t, f, c)
			_, err := Confirm(ctx, f, tt.s, testOptions(c, nil))
			if !errors.Is(err, tt.want) || !strings.Contains(err.Error(), tt.hint) || !exists(f, stateFile(PendingName)) {
				t.Fatalf("Confirm = %v; want %v with %q, and the change still pending", err, tt.want, tt.hint)
			}
		})
	}
	t.Run("too late: the timer rolled it back", func(t *testing.T) {
		f := newHost(t)
		c := &clock{testNow}
		j := armed(t, f, c)
		c.t = j.Deadline()
		if _, err := Rollback(ctx, f, testID, testOptions(c, nil)); err != nil {
			t.Fatal(err)
		}
		_, err := Confirm(ctx, f, newSession, testOptions(c, nil))
		var np *NotPendingError
		if !errors.As(err, &np) || np.Reverted == nil || !strings.Contains(err.Error(), "was rolled back at 2026-10-18 12:03:00 UTC, because it was not confirmed within 180s") {
			t.Fatalf("Confirm = %v", err)
		}
	})
	t.Run("a rollback has started", func(t *testing.T) {
		f := newHost(t)
		c := &clock{testNow}
		armed(t, f, c)
		if err := f.Rename(ctx, stateFile(PendingName), stateFile(RollbackName)); err != nil {
			t.Fatal(err)
		}
		if _, err := Confirm(ctx, f, newSession, testOptions(c, nil)); !errors.Is(err, ErrRollingBack) {
			t.Fatalf("Confirm = %v", err)
		}
	})
	t.Run("an interrupted change is not confirmed", func(t *testing.T) {
		f := newHost(t)
		opts := testOptions(&clock{testNow}, nil)
		if _, err := Arm(ctx, f, Journal{ID: testID, Risk: RiskOther, Summary: "DNS"}, systemd, opts); err != nil {
			t.Fatal(err)
		}
		if _, err := Confirm(ctx, f, newSession, opts); !errors.Is(err, ErrNotConfirmable) {
			t.Fatalf("Confirm = %v", err)
		}
	})
	t.Run("the sleeper on OpenRC", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			cmdline string
			killed  bool
		}{
			{"still the sleeper: signalled", "/bin/sh\x00-c\x00" + SleeperScript + "\x00" + testHardn + "\x00180\x00" + testID + "\x00", true},
			{"the pid was reused: left alone", "/usr/sbin/crond\x00-f\x00", false},
			{"it has ended", "", false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				f := newHost(t)
				c := &clock{testNow}
				respond(f, hostfake.Response{PID: 4321}, []string{"/bin/sh", "-c", SleeperScript, testHardn, "180", testID})
				if _, err := Arm(ctx, f, lockout(), openrc, testOptions(c, nil)); err != nil {
					t.Fatal(err)
				}
				if tt.cmdline != "" {
					f.AddFile("/proc/4321/cmdline", []byte(tt.cmdline), 0o444, 0, 0)
				}
				f.Respond(hostfake.Response{}, "kill", "-s", "TERM", "4321")
				if _, err := Confirm(ctx, f, newSession, testOptions(c, nil)); err != nil {
					t.Fatal(err)
				}
				if got := strings.Contains(runs(f), "run kill -s TERM 4321"); got != tt.killed {
					t.Errorf("killed %v, want %v:\n%s", got, tt.killed, runs(f))
				}
				if exists(f, "/run/hardn/revert-"+testID+".pid") {
					t.Error("pidfile left")
				}
			})
		}
	})
}

// TestStateUnsafe refuses a journal another user could have written: it
// holds commands a rollback runs as root.
func TestStateUnsafe(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name  string
		setup func(f *hostfake.System, data []byte)
		want  error
	}{
		{"pending.json alice's", func(f *hostfake.System, data []byte) {
			f.AddFile(stateFile(PendingName), data, 0o600, 1000, 1000)
		}, ErrUnsafe},
		{"pending.json world-writable", func(f *hostfake.System, data []byte) {
			f.AddFile(stateFile(PendingName), data, 0o666, 0, 0)
		}, ErrUnsafe},
		{"pending.json a symlink", func(f *hostfake.System, data []byte) {
			f.AddFile("/var/lib/elsewhere.json", data, 0o600, 0, 0)
			f.AddSymlink(stateFile(PendingName), "/var/lib/elsewhere.json")
		}, nil},
		{"/var/lib writable by others", func(f *hostfake.System, data []byte) {
			f.AddDir("/var/lib", 0o777, 0, 0)
			f.AddFile(stateFile(PendingName), data, 0o600, 0, 0)
		}, ErrUnsafe},
		{"an unknown field", func(f *hostfake.System, data []byte) {
			f.AddFile(stateFile(PendingName), bytes.Replace(data, []byte(`"risk"`), []byte(`"runFirst": ["sh"], "risk"`), 1), 0o600, 0, 0)
		}, ErrJournal},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := newHost(t)
			armed(t, src, &clock{testNow})
			e, _ := src.Entry(stateFile(PendingName))
			f := newHost(t)
			f.AddDir(DefaultDir, 0o700, 0, 0)
			tt.setup(f, e.Data)
			_, err := ReadState(ctx, f, Options{})
			if tt.want == nil {
				// A symlink is refused by the guarded read itself.
				if err == nil {
					t.Fatal("ReadState accepted a symlinked journal")
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("ReadState = %v, want %v", err, tt.want)
			}
		})
	}
	// No directory at all: nothing journalled.
	st, err := ReadState(ctx, newHost(t), Options{})
	if err != nil || st.Busy() || st.Reverted != nil {
		t.Errorf("ReadState with no directory = %+v, %v", st, err)
	}
}

// TestStateRealFiles writes, reads and claims the journal on real files in
// t.TempDir(), through host.Real.
func TestStateRealFiles(t *testing.T) {
	ctx := context.Background()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func(top string, uid int) { trustedTop, trustedUID = top, uid }(trustedTop, trustedUID)
	trustedTop, trustedUID = dir, os.Geteuid()
	sys := newRealT(t)
	opts := Options{Dir: filepath.Join(dir, "lib", "hardn"), Now: (&clock{testNow}).now}
	j := Journal{ID: testID, Risk: RiskOther, Summary: "DNS resolvers 192.0.2.53",
		Undo: []Step{{Op: OpChmod, Path: filepath.Join(dir, "made"), Mode: permT(0o755)}}}
	armedJ, err := Arm(ctx, sys, j, systemd, opts)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(opts.Dir, PendingName))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("pending.json: %v, %v", fi, err)
	}
	if fi, err := os.Stat(opts.Dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("state directory: %v, %v", fi, err)
	}
	st, err := ReadState(ctx, sys, opts)
	if err != nil || st.Pending == nil || !reflect.DeepEqual(*st.Pending, armedJ) {
		t.Fatalf("ReadState = %+v, %v", st, err)
	}
	if err := os.Mkdir(filepath.Join(dir, "made"), 0o700); err != nil {
		t.Fatal(err)
	}
	res, err := Rollback(ctx, sys, testID, opts)
	if err != nil || len(res.Undone) != 1 {
		t.Fatalf("Rollback = %+v, %v", res, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "made")); err != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("chmod undone: %v, %v", fi, err)
	}
	if _, err := os.Stat(filepath.Join(opts.Dir, RollbackName)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("rollback.json left: %v", err)
	}
	if st, err := ReadState(ctx, sys, opts); err != nil || st.Busy() {
		t.Errorf("after the rollback: %+v, %v", st, err)
	}
}

func permT(p uint32) *backup.Perm {
	v := backup.Perm(p)
	return &v
}
