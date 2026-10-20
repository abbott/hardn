package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
)

// Synthetic values only: alice, web-01.example.net, 192.0.2.0/24, port
// 2230.
const (
	testHardn = "/usr/local/bin/hardn"
	confPath  = "/etc/example/web.conf"
	selfPID   = 5000
	applySSHD = 3970
	bootID    = "8e2a3c4d-1b2c-4d3e-8f40-0123456789ab"
	runRoot   = revert.DefaultRunDir
	journal   = revert.DefaultDir + "/" + revert.PendingName
	lockFile  = runRoot + "/" + LockName
)

var (
	testNow  = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	debian13 = detect.Facts{
		Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init:     detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
	}
	applySession = revert.Session{Remote: true, Conn: "192.0.2.10 50022 192.0.2.20 22",
		SSHD: revert.ProcStart{PID: applySSHD, Start: applySSHD * 100}, Source: "SSH_CONNECTION"}
	any2230 = revert.Listener{Addr: netip.IPv4Unspecified(), Port: 2230}
)

// testHost is a fake Debian 13 host with hardn installed, the parents of
// the journal, backup and run directories, /proc as MarkApplied and
// Rollback read it, and a Handler that answers systemd's commands and
// every synthetic one, failing those in exit.
type testHost struct {
	*hostfake.System
	mu      sync.Mutex
	exit    map[string]int // a command line, joined by spaces, and its exit code
	trigger string         // list-timers' JSON; "" fails it
	// journalWrites counts the writes of pending.json; the one numbered
	// failJournalAt fails (0: none). failDone makes its removal fail
	// (revert.Done).
	journalWrites, failJournalAt int
	failDone                     bool
	// onRun, when set, is called with each command before it is answered,
	// as another process acting at that moment (next/p3-engine-boot).
	onRun func(argv []string)
}

// failNextJournal makes the n-th write of pending.json from now fail.
func (h *testHost) failNextJournal(n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failJournalAt = h.journalWrites + n
}

// WriteFileAtomic fails the write of pending.json failJournalAt names.
func (h *testHost) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	h.mu.Lock()
	fail := false
	if name == journal {
		h.journalWrites++
		fail = h.journalWrites == h.failJournalAt
	}
	h.mu.Unlock()
	if fail {
		return fmt.Errorf("fixture: write %s: no space left on device", name)
	}
	return h.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

// Remove fails the removal of pending.json when failDone is set.
func (h *testHost) Remove(ctx context.Context, name string) error {
	h.mu.Lock()
	fail := name == journal && h.failDone
	h.mu.Unlock()
	if fail {
		return fmt.Errorf("fixture: remove %s: read-only file system", name)
	}
	return h.System.Remove(ctx, name)
}

// clock is a time the test moves.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// newHost is newBareHost with hardn's boot unit installed and enabled for
// testHardn, for systemd and OpenRC alike, so that a lockout-risk plan has
// no boot unit to install (bootChange plans nothing): the tests of the
// boot unit's change start from newBareHost.
func newHost(t *testing.T) *testHost {
	t.Helper()
	h := newBareHost(t)
	for _, in := range []detect.Init{debian13.Init, {Kind: detect.OpenRC, Running: detect.Yes, Source: detect.OpenRCRunDir}} {
		installBootUnit(t, h, in)
	}
	return h
}

// installBootUnit installs and enables the boot unit for testHardn on init
// system in, as EnsureBootUnit leaves it.
func installBootUnit(t *testing.T, h *testHost, in detect.Init) revert.BootUnit {
	t.Helper()
	u, err := revert.PlanBootUnit(in, testHardn)
	if err != nil {
		t.Fatal(err)
	}
	h.AddFile(u.Path, u.Data, u.Mode, 0, 0)
	h.AddSymlink(u.Link, u.Path)
	return u
}

// newBareHost is a fake Debian 13 host with hardn installed and no boot
// unit; see newHost.
func newBareHost(t *testing.T) *testHost {
	t.Helper()
	h := &testHost{System: hostfake.New(host.Options{}), exit: map[string]int{}}
	h.AddFile(testHardn, []byte("ELF"), 0o755, 0, 0)
	for _, d := range []string{"/var/lib", "/var/backups", "/run", "/etc/example", revert.SystemdUnitDir, revert.OpenRCInitDir} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddFile("/proc/self/stat", []byte(statLine(selfPID, 1, "hardn")), 0o444, 0, 0)
	h.AddFile("/proc/sys/kernel/random/boot_id", []byte(bootID+"\n"), 0o444, 0, 0)
	h.AddFile("/proc/900/stat", []byte(statLine(900, 1, "sshd")), 0o444, 0, 0)
	h.AddFile(fmt.Sprintf("/proc/%d/stat", applySSHD), []byte(statLine(applySSHD, 900, "sshd-session")), 0o444, 0, 0)
	h.Handler = h.answer
	return h
}

func statLine(pid, ppid int, comm string) string {
	return fmt.Sprintf("%d (%s) S %d %d %d 34819 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 %d 0 0\n", pid, comm, ppid, pid, pid, pid*100)
}

func (h *testHost) answer(argv []string, _ []byte) hostfake.Response {
	h.mu.Lock()
	onRun := h.onRun
	h.mu.Unlock()
	if onRun != nil {
		onRun(argv)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	cmd := strings.Join(argv, " ")
	if code, ok := h.exit[cmd]; ok {
		return hostfake.Response{ExitCode: code, Stderr: "fixture: refused"}
	}
	// The boot unit's enable commands make their links, as systemctl and
	// rc-update do (revert.BootUnit.Link).
	switch cmd {
	case "systemctl --no-reload enable " + revert.BootUnitName + ".service":
		h.AddSymlink(revert.SystemdWants+"/"+revert.BootUnitName+".service", revert.SystemdUnitDir+"/"+revert.BootUnitName+".service")
	case "rc-update add " + revert.BootUnitName + " default":
		h.AddSymlink(revert.OpenRCDefault+"/"+revert.BootUnitName, revert.OpenRCInitDir+"/"+revert.BootUnitName)
	}
	if len(argv) > 1 && argv[0] == "systemctl" && argv[1] == "list-timers" {
		if h.trigger == "" {
			return hostfake.Response{ExitCode: 1}
		}
		return hostfake.Response{Stdout: h.trigger}
	}
	return hostfake.Response{}
}

func (h *testHost) failOn(cmd string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.exit[cmd] = 1
}

// runs renders the commands the fake ran, one per line, with the id of
// the change replaced by <id>.
func (h *testHost) runs(id string) string {
	var b strings.Builder
	for _, r := range h.Runs() {
		b.WriteString(strings.ReplaceAll(host.FormatArgv(r), id, "<id>") + "\n")
	}
	return b.String()
}

// changedOutside lists the calls that changed something outside dir, and
// every command run.
func (h *testHost) changedOutside(dir string) []string { return h.changedOutsideSince(dir, 0) }

// changedOutsideSince is changedOutside for the calls from call number from.
func (h *testHost) changedOutsideSince(dir string, from int) []string {
	var out []string
	for _, c := range h.Calls()[from:] {
		switch c.Op {
		case "run", "start-detached":
			out = append(out, c.String())
		case "write-atomic", "remove", "removeall", "rename", "mkdir", "chmod", "chown", "lock":
			if c.Err == nil && c.Path != dir && !strings.HasPrefix(c.Path, dir+"/") {
				out = append(out, c.String())
			}
		}
	}
	return out
}

func (h *testHost) content(t *testing.T, name string) (string, bool) {
	t.Helper()
	e, ok := h.Entry(name)
	return string(e.Data), ok
}

// holds records Options.Hold's calls, and the journal as it stood when
// the in-process rollback was about to run.
type holds struct {
	h                *testHost
	held, released   int
	pending, rolling *revert.Journal
}

func (o *holds) hold() func() {
	o.held++
	st, err := revert.ReadState(context.Background(), o.h, revert.Options{})
	if err == nil {
		o.pending, o.rolling = st.Pending, st.RollingBack
	}
	return func() { o.released++ }
}

func testOptions(h *testHost, hs *holds, log *bytes.Buffer, facts detect.Facts) Options {
	o := Options{
		Revert:  revert.Options{Now: func() time.Time { return testNow }, Hardn: testHardn},
		Session: applySession,
		Detect:  func(context.Context, host.System) (detect.Facts, error) { return facts, nil },
		PID:     selfPID,
	}
	if hs != nil {
		hs.h = h
		o.Hold = hs.hold
	}
	if log != nil {
		o.Revert.Log = log
	}
	return o
}

// stub is a control that plans the changes it is given.
type stub struct {
	id, title string
	changes   []Change
	err       error
	why       string // set: not applicable
	plans     int
}

func (s *stub) ID() string    { return s.id }
func (s *stub) Title() string { return s.title }
func (s *stub) Applies(detect.Facts) (bool, string) {
	return s.why == "", s.why
}
func (s *stub) Check(context.Context, host.System, detect.Facts) (Status, error) {
	return Status{State: NotConfigured, Detail: s.title + ": not as asked", Findings: []Finding{{Code: "EXAMPLE_FINDING", Detail: "synthetic"}}}, nil
}
func (s *stub) Plan(context.Context, host.System, Input) ([]Change, error) {
	s.plans++
	return s.changes, s.err
}

// command is a change that runs argv, journals undo before it and, when
// after is set, after it.
func command(id string, stage Stage, risk RiskClass, argv, undo []string, after ...[]string) Change {
	c := Change{ID: id, Summary: "Run " + strings.Join(argv, " "), Diff: CommandDiff(argv[0], "before\n", "after\n"),
		Risk: risk, Stage: stage, Rollback: Undo{Steps: runSteps(undo)}}
	c.Apply = func(ctx context.Context, sys host.System, _ Run) (Ran, error) {
		_, err := sys.Run(ctx, argv[0], argv[1:]...)
		var ran Ran
		if len(after) > 0 {
			u := Undo{Steps: runSteps(after...)}
			ran.Rollback = &u
		}
		return ran, err
	}
	return c
}

func runSteps(argvs ...[]string) []revert.Step {
	var out []revert.Step
	for _, a := range argvs {
		if len(a) > 0 {
			out = append(out, revert.Step{Op: revert.OpRun, Argv: a, Why: "synthetic"})
		}
	}
	return out
}

// fileControl wants path to hold want, and observes the fake: once it
// does, it plans nothing (idempotency).
type fileControl struct {
	id, path, want string
	risk           RiskClass
}

func (c fileControl) ID() string                          { return c.id }
func (c fileControl) Title() string                       { return "Example file" }
func (c fileControl) Applies(detect.Facts) (bool, string) { return true, "" }
func (c fileControl) Check(ctx context.Context, sys host.System, _ detect.Facts) (Status, error) {
	data, err := sys.ReadFile(ctx, c.path)
	if err == nil && string(data) == c.want {
		return Status{State: Configured, Evidence: []string{c.path}}, nil
	}
	return Status{State: NotConfigured, Evidence: []string{c.path}}, nil
}

func (c fileControl) Plan(ctx context.Context, sys host.System, _ Input) ([]Change, error) {
	data, err := sys.ReadFile(ctx, c.path)
	var before *string
	switch {
	case err == nil && string(data) == c.want:
		return nil, nil
	case err == nil:
		s := string(data)
		before = &s
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	return []Change{writeChange(c.id+"/write", c.path, before, c.want, c.risk)}, nil
}

// writeChange writes want to path, after the engine has backed it up.
func writeChange(id, path string, before *string, want string, risk RiskClass) Change {
	return Change{
		ID: id, Summary: "Write " + path, Diff: FileDiff(path, before, &want),
		Risk: risk, Stage: StageFiles, Backups: []string{path},
		Apply: func(ctx context.Context, sys host.System, run Run) (Ran, error) {
			if _, ok := run.Backups[path]; !ok {
				return Ran{}, errors.New("fixture: not backed up first")
			}
			return Ran{Detail: "written"}, sys.WriteFileAtomic(ctx, path, []byte(want), 0o644, 0, 0)
		},
	}
}
