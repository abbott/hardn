package revert

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// The journal lock and the run lock (next/p3-engine-boot), each
// interleaving on the fake: a second hardn process on the same host is a
// process, whose /proc/self is its own pid, and whose Lock, Rename,
// WriteFileAtomic and Run the test can stop at.

// process is another hardn process on fake host f.
type process struct {
	*hostfake.System
	pid int
	// waiting is closed once a Lock of this process finds a lock held.
	waiting chan struct{}
	once    sync.Once
	// at, when set, is called before each ReadFileNoFollow, Rename,
	// WriteFileAtomic and Run of this process, with "read", "rename",
	// "write" or "run" and the path or the command line; it may block, as
	// the process would be preempted.
	at func(op, what string)
}

func newProcess(f *hostfake.System, pid int) *process {
	proc(f, pid, 1, "hardn", ptsTTY)
	return &process{System: f, pid: pid, waiting: make(chan struct{})}
}

func (p *process) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if name == ProcDir+"/self/stat" {
		return []byte(statLine(p.pid, 1, "hardn", ptsTTY, procStart(p.pid))), nil
	}
	return p.System.ReadFile(ctx, name)
}

func (p *process) Lock(ctx context.Context, name string, holder []byte) (func() error, error) {
	unlock, err := p.System.Lock(ctx, name, holder)
	if errors.Is(err, host.ErrLocked) {
		p.once.Do(func() { close(p.waiting) })
	}
	return unlock, err
}

func (p *process) Rename(ctx context.Context, oldname, newname string) error {
	if p.at != nil {
		p.at("rename", oldname)
	}
	return p.System.Rename(ctx, oldname, newname)
}

func (p *process) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	if p.at != nil {
		p.at("read", name)
	}
	return p.System.ReadFileNoFollow(ctx, name)
}

func (p *process) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if p.at != nil {
		p.at("write", name)
	}
	return p.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

func (p *process) Run(ctx context.Context, name string, args ...string) (host.Result, error) {
	if p.at != nil {
		p.at("run", host.FormatArgv(append([]string{name}, args...)))
	}
	return p.System.Run(ctx, name, args...)
}

// awaitLock waits until process p has found a lock held, as a process
// waiting for it does; it fails the test after a while instead (the lock
// was not held there).
func awaitLock(t *testing.T, p *process) {
	select {
	case <-p.waiting:
	case <-time.After(3 * time.Second):
		t.Errorf("process %d never found the lock held", p.pid)
	}
}

// syncLog is a log two processes write.
type syncLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncLog) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncLog) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// fastLock makes the journal lock's wait short for a test.
func fastLock(t *testing.T, wait time.Duration) {
	t.Helper()
	w, p := lockWait, lockPoll
	lockWait, lockPoll = wait, time.Millisecond
	t.Cleanup(func() { lockWait, lockPoll = w, p })
}

// revert.md §8.1 item 8: Abandon and a `hardn rollback <id>` that takes the
// unfinished rollback up are made one after the other by the journal lock,
// in either order: given up, the take-up then finds it given up and does
// nothing; taken up, Abandon then finds it running and gives up nothing.
func TestJournalLockAbandonAndTakeUp(t *testing.T) {
	ctx := context.Background()
	fastLock(t, 10*time.Second)
	t.Run("Abandon first", func(t *testing.T) {
		ch := unfinishedRollback(t)
		ch.opts.Log = &syncLog{}
		ab, rb := newProcess(ch.f, 6000), newProcess(ch.f, 7000)
		var rres RollbackResult
		var rerr error
		done := make(chan struct{})
		ab.at = func(op, what string) {
			if op == "rename" && what == stateFile(RollbackName) {
				ab.at = nil
				go func() { rres, rerr = Rollback(ctx, rb, testID, ch.opts); close(done) }()
				awaitLock(t, rb) // the take-up waits for the lock Abandon holds
			}
		}
		if _, err := Abandon(ctx, ab, testID, ch.opts); err != nil {
			t.Fatalf("Abandon = %v", err)
		}
		<-done
		if rerr != nil || !rres.Nothing || !strings.Contains(rres.Why, "given up") || rres.Resumed {
			t.Errorf("the take-up: %+v, %v; want nothing done, its rollback given up", rres, rerr)
		}
		if exists(ch.f, stateFile(RollbackName)) || !exists(ch.f, stateFile(AbandonedName(testID))) {
			t.Error("rollback.json written again, or the record not kept")
		}
	})
	t.Run("the take-up first", func(t *testing.T) {
		ch := unfinishedRollback(t)
		ch.opts.Log = &syncLog{}
		ab, rb := newProcess(ch.f, 6000), newProcess(ch.f, 7000)
		var aerr error
		adone := make(chan struct{})
		saved := false
		rb.at = func(op, what string) {
			switch {
			case op == "write" && what == stateFile(RollbackName) && !saved:
				// The take-up's first progress write, under the lock.
				saved = true
				go func() { _, aerr = Abandon(ctx, ab, testID, ch.opts); close(adone) }()
				awaitLock(t, ab)
			case op == "run":
				<-adone // the rollback runs on while Abandon looks
			}
		}
		if _, err := Rollback(ctx, rb, testID, ch.opts); !errors.Is(err, ErrIncomplete) {
			t.Fatalf("the take-up = %v, want it incomplete again", err)
		}
		<-adone
		if !errors.Is(aerr, ErrRollingBack) || !strings.Contains(aerr.Error(), "process 7000 is rolling it back now") {
			t.Errorf("Abandon = %v; want refused, process 7000 rolling it back", aerr)
		}
		if exists(ch.f, stateFile(AbandonedName(testID))) {
			t.Error("given up while it was taken up")
		}
	})
}

// revert.md §2.16: two rollbacks claiming one change at once. The second
// waits for the first's claim and first progress write, then finds it
// running, and does nothing.
func TestJournalLockTwoRollbacks(t *testing.T) {
	ctx := context.Background()
	fastLock(t, 10*time.Second)
	ch := newChange(t)
	ufwDeleted(ch.f)
	ch.opts.Log = &syncLog{}
	p1, p2 := newProcess(ch.f, 7000), newProcess(ch.f, 7100)
	var res2 RollbackResult
	var err2 error
	done := make(chan struct{})
	p1.at = func(op, what string) {
		switch {
		case op == "rename" && what == stateFile(PendingName):
			go func() { res2, err2 = Rollback(ctx, p2, testID, ch.opts); close(done) }()
			awaitLock(t, p2)
		case op == "run":
			<-done
		}
	}
	if _, err := Rollback(ctx, p1, testID, ch.opts); err != nil {
		t.Fatal(err)
	}
	<-done
	if err2 != nil || !res2.Nothing || !strings.Contains(res2.Why, "being rolled back by process 7000") {
		t.Errorf("the second rollback: %+v, %v; want nothing, process 7000 has it", res2, err2)
	}
	restored(t, ch)
}

// A holder that keeps the journal lock (stopped, or stuck in the kernel):
// a rollback goes on without it after lockWait, and says so; Abandon and
// Arm refuse, naming the holder, and change nothing; a re-arm at boot that
// cannot write its journal rolls the change back.
func TestJournalLockHeld(t *testing.T) {
	ctx := context.Background()
	fastLock(t, 20*time.Millisecond)
	stuck := "pid 6000, hardn rollback --abandon " + otherID + ", since 2026-10-18T12:00:00Z\n"
	hold := func(t *testing.T, f *hostfake.System) {
		t.Helper()
		if _, err := f.Lock(ctx, stateFile(JournalLockName), []byte(stuck)); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("a rollback goes on", func(t *testing.T) {
		ch := newChange(t)
		ufwDeleted(ch.f)
		hold(t, ch.f)
		if _, err := Rollback(ctx, ch.f, testID, ch.opts); err != nil {
			t.Fatal(err)
		}
		restored(t, ch)
		if !strings.Contains(ch.log.String(), "goes on without the journal lock") || !strings.Contains(ch.log.String(), "pid 6000") {
			t.Errorf("log:\n%s", ch.log)
		}
	})
	t.Run("Abandon refuses", func(t *testing.T) {
		ch := unfinishedRollback(t)
		hold(t, ch.f)
		calls := len(ch.f.Calls())
		_, err := Abandon(ctx, ch.f, testID, ch.opts)
		if !journalLocked(err, stuck) || !strings.Contains(err.Error(), "nothing given up") || !strings.Contains(err.Error(), "pid 6000") {
			t.Fatalf("Abandon = %v", err)
		}
		if did := changes(ch.f.Calls()[calls:]); len(did) > 0 {
			t.Errorf("a refused Abandon did %q", did)
		}
	})
	t.Run("Arm refuses", func(t *testing.T) {
		f := newHost(t)
		if err := f.MkdirAll(ctx, DefaultDir, 0o700); err != nil {
			t.Fatal(err)
		}
		hold(t, f)
		_, err := Arm(ctx, f, lockout(), systemd, testOptions(&clock{testNow}, nil))
		if !journalLocked(err, stuck) || exists(f, stateFile(PendingName)) || runs(f) != "" {
			t.Errorf("Arm = %v; journal %v, ran %q", err, exists(f, stateFile(PendingName)), runs(f))
		}
	})
	t.Run("a re-arm at boot rolls back", func(t *testing.T) {
		ch := rebooted(t, 100*time.Second)
		ufwDeleted(ch.f)
		hold(t, ch.f)
		res, err := Boot(ctx, ch.f, systemd, ch.opts)
		if err != nil || res.Action != BootRolledBack || res.Reason != BootReasonRearmFailed || !strings.Contains(res.Why, "pid 6000") {
			t.Fatalf("Boot = %+v, %v", res, err)
		}
		restored(t, ch)
		if strings.Contains(runs(ch.f), "--on-active=80s") {
			t.Errorf("re-armed:\n%s", runs(ch.f))
		}
	})
}

// journalLocked reports whether err is the journal lock held past its wait
// by holder, as a *LockedError that is host.ErrLocked.
func journalLocked(err error, holder string) bool {
	var le *LockedError
	return errors.Is(err, host.ErrLocked) && errors.As(err, &le) && !le.Run && le.Name == stateFile(JournalLockName) &&
		le.Holder == strings.TrimSpace(holder) && le.Waited == lockWait
}

// The journal lock that cannot be taken for another reason than another
// hardn holding it (here a journal.lock that is a directory): a rollback
// goes on without it and logs it; Abandon and Arm refuse (ErrNoLock, with
// the lock's own error, not host.ErrLocked), and change nothing. A done ctx
// ends Arm's wait, and Arm writes nothing; a journal directory another user
// can write is refused before any lock is taken; a journal that cannot be
// read under the lock is Arm's refusal, and the lock is released.
func TestJournalLockErrors(t *testing.T) {
	ctx := context.Background()
	fastLock(t, 10*time.Second)
	lockDir := func(f *hostfake.System) { f.AddDir(stateFile(JournalLockName), 0o700, 0, 0) }
	notLocked := func(err error) bool {
		return errors.Is(err, ErrNoLock) && !errors.Is(err, host.ErrLocked) && errors.Is(err, fs.ErrInvalid)
	}
	t.Run("a rollback goes on", func(t *testing.T) {
		ch := newChange(t)
		ufwDeleted(ch.f)
		lockDir(ch.f)
		if _, err := Rollback(ctx, ch.f, testID, ch.opts); err != nil {
			t.Fatal(err)
		}
		restored(t, ch)
		if !strings.Contains(ch.log.String(), "goes on without the journal lock") {
			t.Errorf("log:\n%s", ch.log)
		}
	})
	t.Run("Abandon refuses", func(t *testing.T) {
		ch := unfinishedRollback(t)
		lockDir(ch.f)
		calls := len(ch.f.Calls())
		_, err := Abandon(ctx, ch.f, testID, ch.opts)
		if !notLocked(err) || !strings.Contains(err.Error(), "nothing given up") {
			t.Fatalf("Abandon = %v", err)
		}
		if did := changes(ch.f.Calls()[calls:]); len(did) > 0 {
			t.Errorf("a refused Abandon did %q", did)
		}
	})
	t.Run("Arm refuses", func(t *testing.T) {
		f := newHost(t)
		if err := f.MkdirAll(ctx, DefaultDir, 0o700); err != nil {
			t.Fatal(err)
		}
		lockDir(f)
		_, err := Arm(ctx, f, lockout(), systemd, testOptions(&clock{testNow}, nil))
		if !notLocked(err) || exists(f, stateFile(PendingName)) || runs(f) != "" {
			t.Errorf("Arm = %v; journal %v, ran %q", err, exists(f, stateFile(PendingName)), runs(f))
		}
	})
	t.Run("Arm, its ctx done while it waits", func(t *testing.T) {
		f := newHost(t)
		if err := f.MkdirAll(ctx, DefaultDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Lock(ctx, stateFile(JournalLockName), []byte("pid 6000, hardn rollback "+otherID+", since 2026-10-18T12:00:00Z\n")); err != nil {
			t.Fatal(err)
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := Arm(cctx, f, lockout(), systemd, testOptions(&clock{testNow}, nil))
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "waiting for the journal lock") || time.Since(start) > 5*time.Second {
			t.Errorf("Arm = %v after %s", err, time.Since(start))
		}
		if exists(f, stateFile(PendingName)) || runs(f) != "" {
			t.Errorf("armed: journal %v, ran %q", exists(f, stateFile(PendingName)), runs(f))
		}
	})
	t.Run("a journal directory another user can write", func(t *testing.T) {
		f := newHost(t)
		f.AddDir(DefaultDir, 0o755, 1000, 1000)
		if _, err := lockJournal(ctx, f, Options{}, "hardn rollback --abandon "+testID); !errors.Is(err, ErrUnsafe) || !errors.Is(err, ErrNoLock) ||
			!strings.Contains(err.Error(), "the journal lock") {
			t.Errorf("lockJournal = %v", err)
		}
		for _, c := range f.Calls() {
			if c.Op == "lock" {
				t.Errorf("locked: %v", c)
			}
		}
	})
	t.Run("Arm, the journal unreadable under the lock", func(t *testing.T) {
		f := newHost(t)
		if err := f.MkdirAll(ctx, DefaultDir, 0o700); err != nil {
			t.Fatal(err)
		}
		f.AddFile(stateFile(PendingName), []byte("{not json"), 0o600, 0, 0)
		if _, err := Arm(ctx, f, lockout(), systemd, testOptions(&clock{testNow}, nil)); !errors.Is(err, ErrJournal) || runs(f) != "" {
			t.Errorf("Arm = %v; ran %q", err, runs(f))
		}
		if e, _ := f.Entry(stateFile(JournalLockName)); len(e.Data) != 0 || lockDepth(f.Calls(), stateFile(JournalLockName)) != 0 {
			t.Errorf("the journal lock left held: %q", e.Data)
		}
	})
}

// lockDepth is how many times name is held at the end of calls.
func lockDepth(calls []hostfake.Call, name string) int {
	n := 0
	for _, c := range calls {
		switch {
		case c.Path != name:
		case c.Op == "lock" && c.Err == nil:
			n++
		case c.Op == "unlock":
			n--
		}
	}
	return n
}

// LockRun: tried once, never waited for; another hardn holding the run lock
// is a *LockedError naming it (Run); a run directory another user can
// write, or an engine.lock that is not a regular file, is ErrNoLock, with
// its own error, not host.ErrLocked; a done ctx is its cause.
func TestLockRun(t *testing.T) {
	ctx := context.Background()
	runLock := DefaultRunDir + "/" + RunLockName
	holder := "pid 6000, change " + otherID + ", since 2026-10-18T12:00:00Z"
	done, cancel := context.WithCancel(ctx)
	cancel()
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, f *hostfake.System)
		ctx   context.Context
		check func(err error) bool
	}{
		{"held by another hardn", func(t *testing.T, f *hostfake.System) {
			f.AddDir(DefaultRunDir, 0o700, 0, 0)
			if _, err := f.Lock(ctx, runLock, []byte(holder+"\n")); err != nil {
				t.Fatal(err)
			}
		}, ctx, func(err error) bool {
			var le *LockedError
			return errors.Is(err, host.ErrLocked) && errors.As(err, &le) && le.Run && le.Name == runLock && le.Holder == holder && le.Waited == 0 &&
				strings.Contains(err.Error(), "another hardn holds the run lock "+runLock+" ("+holder+")")
		}},
		{"a run directory another user can write", func(t *testing.T, f *hostfake.System) { f.AddDir(DefaultRunDir, 0o755, 1000, 1000) }, ctx,
			func(err error) bool {
				return errors.Is(err, ErrUnsafe) && errors.Is(err, ErrNoLock) && !errors.Is(err, host.ErrLocked)
			}},
		{"engine.lock is a directory", func(t *testing.T, f *hostfake.System) {
			f.AddDir(DefaultRunDir, 0o700, 0, 0)
			f.AddDir(runLock, 0o700, 0, 0)
		}, ctx, func(err error) bool {
			return errors.Is(err, fs.ErrInvalid) && errors.Is(err, ErrNoLock) && !errors.Is(err, host.ErrLocked)
		}},
		{"a done ctx", func(t *testing.T, f *hostfake.System) {}, done, func(err error) bool {
			return errors.Is(err, context.Canceled) && !errors.Is(err, ErrNoLock) && !errors.Is(err, host.ErrLocked)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHost(t)
			tc.setup(t, f)
			start := time.Now()
			unlock, err := LockRun(tc.ctx, f, Options{}, "hardn rollback --boot")
			if unlock != nil || !tc.check(err) || !strings.HasPrefix(err.Error(), "revert: ") {
				t.Errorf("LockRun = %v", err)
			}
			if took := time.Since(start); took > time.Second {
				t.Errorf("LockRun took %s: it waited", took)
			}
		})
	}
}

// Boot writes its re-armed journal only over the journal it read, under
// the journal lock: another `hardn rollback --boot` that re-armed it, or a
// confirmation followed by a new change armed, between Boot's read and its
// write, wins; nothing is written over it, and no second timer starts.
// Both act while Boot hashes hardn's binary, after its read.
func TestBootRearmRaces(t *testing.T) {
	ctx := context.Background()
	fastLock(t, 10*time.Second)
	t.Run("another boot command re-armed it", func(t *testing.T) {
		ch := rebooted(t, 100*time.Second)
		respond(ch.f, hostfake.Response{}, rearmArgv("80s"))
		p, other := newProcess(ch.f, selfPID), newProcess(ch.f, 7000)
		p.at = func(op, what string) {
			if op == "read" && what == testHardn {
				p.at = nil
				if res, err := Boot(ctx, other, systemd, ch.opts); err != nil || res.Action != BootRearmed {
					t.Errorf("the other Boot = %+v, %v", res, err)
				}
			}
		}
		res, err := Boot(ctx, p, systemd, ch.opts)
		if err != nil || res.Action != BootNothing || res.Reason != BootReasonClaimedMeanwhile || !strings.Contains(res.Why, "re-armed it") {
			t.Fatalf("Boot = %+v, %v", res, err)
		}
		if got := readJournalT(t, ch.f, stateFile(PendingName)); got.Timer == nil || got.Timer.Argv[3] != "--on-active=80s" {
			t.Errorf("the journal: %+v", got.Timer)
		}
		if n := strings.Count(runs(ch.f), "--on-active=80s"); n != 1 {
			t.Errorf("%d timers re-armed:\n%s", n, runs(ch.f))
		}
	})
	t.Run("confirmed, then a new change armed", func(t *testing.T) {
		ch := rebooted(t, 100*time.Second)
		respond(ch.f, hostfake.Response{}, systemdRunArgv(otherID))
		p := newProcess(ch.f, selfPID)
		var b Journal
		p.at = func(op, what string) {
			if op == "read" && what == testHardn {
				p.at = nil
				// The confirmation's claim is one rename; then change
				// otherID is armed, as nothing is journalled.
				if err := ch.f.Rename(ctx, stateFile(PendingName), stateFile(ConfirmedName)); err != nil {
					t.Fatal(err)
				}
				j := lockout()
				j.ID = otherID
				var err error
				if b, err = Arm(ctx, ch.f, j, systemd, ch.opts); err != nil {
					t.Errorf("Arm: %v", err)
				}
			}
		}
		res, err := Boot(ctx, p, systemd, ch.opts)
		if err != nil || res.Action != BootNothing || res.Reason != BootReasonConfirmedMeanwhile {
			t.Fatalf("Boot = %+v, %v", res, err)
		}
		if got := readJournalT(t, ch.f, stateFile(PendingName)); !sameJournal(got, b) {
			t.Errorf("the new change's journal was written over: %+v", got)
		}
		if strings.Contains(runs(ch.f), "--on-active=80s") {
			t.Errorf("the confirmed change was re-armed:\n%s", runs(ch.f))
		}
	})
	// A journal that cannot be read again at the write is a timer that
	// cannot be re-armed: the change is rolled back, and that rollback,
	// which cannot read it either, does not finish; nothing is re-armed.
	t.Run("unreadable at the write", func(t *testing.T) {
		ch := rebooted(t, 100*time.Second)
		p := newProcess(ch.f, selfPID)
		p.at = func(op, what string) {
			if op == "read" && what == testHardn {
				p.at = nil
				ch.f.AddFile(stateFile(PendingName), []byte("{not json"), 0o600, 0, 0)
			}
		}
		res, err := Boot(ctx, p, systemd, ch.opts)
		if !errors.Is(err, ErrJournal) || res.Action != BootRolledBack || res.Reason != BootReasonRearmFailed || !strings.Contains(res.Why, "could not be written") {
			t.Fatalf("Boot = %+v, %v", res, err)
		}
		kept(t, ch, res)
		if strings.Contains(runs(ch.f), "--on-active=80s") {
			t.Errorf("re-armed:\n%s", runs(ch.f))
		}
	})
	// At the write itself: the change confirmed just before it, and a new
	// change's Arm, which waits for the journal lock Boot holds; Boot
	// finds the confirmation and removes what it wrote before it lets go,
	// so Arm then writes its own journal, which nothing writes over.
	t.Run("confirmed, and a new change armed, at the write", func(t *testing.T) {
		ch := rebooted(t, 100*time.Second)
		ch.opts.Log = &syncLog{}
		respond(ch.f, hostfake.Response{}, systemdRunArgv(otherID))
		p, q := newProcess(ch.f, selfPID), newProcess(ch.f, 7000)
		var b Journal
		var berr error
		done := make(chan struct{})
		p.at = func(op, what string) {
			if op == "write" && what == stateFile(PendingName) {
				p.at = nil
				if err := ch.f.Rename(ctx, stateFile(PendingName), stateFile(ConfirmedName)); err != nil {
					t.Error(err)
				}
				j := lockout()
				j.ID = otherID
				go func() { b, berr = Arm(ctx, q, j, systemd, ch.opts); close(done) }()
				awaitLock(t, q)
			}
		}
		res, err := Boot(ctx, p, systemd, ch.opts)
		<-done
		if err != nil || res.Action != BootNothing || res.Reason != BootReasonConfirmedMeanwhile {
			t.Fatalf("Boot = %+v, %v", res, err)
		}
		if berr != nil {
			t.Fatalf("Arm = %v", berr)
		}
		if got := readJournalT(t, ch.f, stateFile(PendingName)); !sameJournal(got, b) {
			t.Errorf("the new change's journal: %+v; want Arm's", got)
		}
	})
}

// Boot and the run lock: another hardn that holds it keeps the boot unit
// (an apply may have installed it and not armed its change yet), and owns
// a journalled change not applied yet whose timer, of this boot, is not
// there yet; an applied one is left, not re-armed; Boot never waits for the
// lock, and one it cannot take for another reason keeps the unit too. A
// rollback at boot takes the journal lock after Boot has released it: one
// process never holds it twice.
func TestBootRunLock(t *testing.T) {
	ctx := context.Background()
	fastLock(t, 10*time.Second)
	runLock := DefaultRunDir + "/" + RunLockName
	apply := []byte("pid 6000, change " + otherID + ", since 2026-10-18T12:00:00Z\n")
	holdRun := func(t *testing.T, f *hostfake.System) {
		t.Helper()
		f.AddDir(DefaultRunDir, 0o700, 0, 0)
		if _, err := f.Lock(ctx, runLock, apply); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("nothing journalled, an apply runs", func(t *testing.T) {
		ch := rebooted(t, 100*time.Second)
		if err := ch.f.Remove(ctx, stateFile(PendingName)); err != nil {
			t.Fatal(err)
		}
		holdRun(t, ch.f)
		res, err := Boot(ctx, ch.f, systemd, ch.opts)
		if err != nil || res.Action != BootNothing || res.Removed || res.RemoveErr != nil {
			t.Fatalf("Boot = %+v, %v", res, err)
		}
		kept(t, ch, res)
		if !strings.Contains(ch.log.String(), "the boot unit is kept") || !strings.Contains(ch.log.String(), "pid 6000") {
			t.Errorf("log:\n%s", ch.log)
		}
	})
	t.Run("the apply's change, its timer not started yet", func(t *testing.T) {
		ch := newChangeBare(t) // armed in this boot, being applied
		timerState(ch.f, "inactive")
		holdRun(t, ch.f)
		res, err := Boot(ctx, ch.f, systemd, ch.opts)
		if err != nil || res.Action != BootNothing || res.Reason != BootReasonApplyRuns || !strings.Contains(res.Why, "another hardn holds the run lock") ||
			!strings.Contains(res.Why, "pid 6000") {
			t.Fatalf("Boot = %+v, %v", res, err)
		}
		if !strings.Contains(ch.log.String(), "boot: another hardn holds the run lock") {
			t.Errorf("log:\n%s", ch.log)
		}
		if !exists(ch.f, stateFile(PendingName)) || strings.Contains(runs(ch.f), "ufw delete") {
			t.Error("the apply's change was rolled back")
		}
	})
	// Applied, its apply is over: the holder is another hardn, here a
	// second `hardn rollback --boot` about to start the re-armed timer.
	// Left, and never re-armed beside it.
	t.Run("an applied change, its timer not there", func(t *testing.T) {
		ch := newChangeBare(t)
		markApplied(t, ch.f, ch.c)
		timerState(ch.f, "inactive")
		f := ch.f
		f.AddDir(DefaultRunDir, 0o700, 0, 0)
		if _, err := f.Lock(ctx, runLock, []byte("pid 6100, hardn rollback --boot, since 2026-10-18T12:01:00Z\n")); err != nil {
			t.Fatal(err)
		}
		res, err := Boot(ctx, f, systemd, ch.opts)
		if err != nil || res.Action != BootLeft || res.Reason != BootReasonRunLockHeld || !strings.Contains(res.Why, "pid 6100, hardn rollback --boot") ||
			!strings.Contains(res.Why, "run `sudo hardn rollback --boot` again") {
			t.Fatalf("Boot = %+v, %v", res, err)
		}
		if j := readJournalT(t, f, stateFile(PendingName)); j.Applied == nil || j.Timer == nil || j.Timer.Argv[3] != "--on-active=180s" {
			t.Errorf("the journal: %+v", j)
		}
		if n := strings.Count(runs(f), "systemd-run"); n != 1 || strings.Contains(runs(f), "ufw delete") {
			t.Errorf("re-armed or rolled back (%d timers):\n%s", n, runs(f))
		}
		if res.Removed || res.RemoveErr != nil {
			t.Errorf("the boot unit's removal tried: %+v", res)
		}
	})
	t.Run("the run lock cannot be taken", func(t *testing.T) {
		ch := rebooted(t, 100*time.Second)
		if err := ch.f.Remove(ctx, stateFile(PendingName)); err != nil {
			t.Fatal(err)
		}
		ch.f.AddDir(DefaultRunDir, 0o755, 1000, 1000)
		res, err := Boot(ctx, ch.f, systemd, ch.opts)
		if err != nil || res.Action != BootNothing || res.Reason != BootReasonNoChange || res.Removed || !errors.Is(res.RemoveErr, ErrUnsafe) ||
			!strings.Contains(res.RemoveErr.Error(), "kept") {
			t.Fatalf("Boot = %+v, %v", res, err)
		}
		kept(t, ch, res)
		if !strings.Contains(ch.log.String(), "the run lock cannot be taken, so the boot unit is not removed") {
			t.Errorf("log:\n%s", ch.log)
		}
	})
	t.Run("a rollback at boot", func(t *testing.T) {
		ch := rebooted(t, time.Hour)
		ufwDeleted(ch.f)
		res, err := Boot(ctx, ch.f, systemd, ch.opts)
		if err != nil || res.Action != BootRolledBack {
			t.Fatalf("Boot = %+v, %v", res, err)
		}
		depth := map[string]int{}
		for _, c := range ch.f.Calls() {
			switch c.Op {
			case "lock":
				if c.Err != nil {
					t.Errorf("a lock found held: %v", c)
				}
				if depth[c.Path]++; depth[c.Path] > 1 {
					t.Errorf("%s taken twice", c.Path)
				}
			case "unlock":
				depth[c.Path]--
			}
		}
		if depth[stateFile(JournalLockName)] != 0 || depth[runLock] != 0 {
			t.Errorf("locks left held: %v", depth)
		}
	})
}

// RemoveBootUnit and LockRun: the run lock is tried, never waited for, and
// its holder named; the unit is kept while an apply holds it.
func TestRemoveBootUnitRunLock(t *testing.T) {
	ctx := context.Background()
	ch := rebooted(t, time.Hour)
	if err := ch.f.Remove(ctx, stateFile(PendingName)); err != nil {
		t.Fatal(err)
	}
	u, _ := PlanBootUnit(systemd, testHardn)
	unlock, err := LockRun(ctx, ch.f, ch.opts, "change "+otherID)
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := ch.f.Entry(DefaultRunDir + "/" + RunLockName); string(e.Data) != fmt.Sprintf("pid %d, change %s, since 2026-10-18T13:00:00Z\n", selfPID, otherID) {
		t.Errorf("holder %q", e.Data)
	}
	removed, err := RemoveBootUnit(ctx, ch.f, u, ch.opts)
	if removed || !errors.Is(err, host.ErrLocked) || !strings.Contains(err.Error(), "change "+otherID) {
		t.Fatalf("RemoveBootUnit = %v, %v", removed, err)
	}
	if st, err := ReadBootUnit(ctx, ch.f, u); err != nil || !st.Installed() || strings.Contains(runs(ch.f), "disable") {
		t.Errorf("the unit changed while an apply held the run lock: %+v, %v\n%s", st, err, runs(ch.f))
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	if removed, err := RemoveBootUnit(ctx, ch.f, u, ch.opts); !removed || err != nil {
		t.Errorf("RemoveBootUnit once released = %v, %v", removed, err)
	}
}
