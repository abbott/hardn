package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// TestRefusedBeforeArm: every refusal is the plan's, before anything
// changes. Each row is refused by Plan (ErrRefused and its own error) and
// by Apply, the dry run and the apply alike having run no command and
// changed nothing outside the run root (the plan's run directory and the
// run lock): no journal, no timer, no backup.
func TestRefusedBeforeArm(t *testing.T) {
	ctx := context.Background()
	lockoutChange := func() Change {
		return command("ssh.port/allow", StageFirewall, RiskLockout, []string{"ufw", "allow", "2230/tcp"}, []string{"ufw", "delete", "allow", "2230/tcp"})
	}
	with := func(edit func(*Change)) []Control {
		c := lockoutChange()
		edit(&c)
		return []Control{&stub{id: "ssh.port", title: "SSH Port", changes: []Change{c}}}
	}
	refused := errors.New("fixture: sshd -t refused the candidate")
	cases := []struct {
		name     string
		controls []Control
		facts    detect.Facts
		host     func(h *testHost)
		opts     func(o *Options)
		want     error
	}{
		{"a control refuses", []Control{&stub{id: "ssh.port", title: "SSH Port", err: refused}}, debian13, nil, nil, refused},
		{"an id that is not one", with(func(c *Change) { c.ID = "SSH Port" }), debian13, nil, nil, ErrChange},
		{"a summary of two lines", with(func(c *Change) { c.Summary = "one\ntwo" }), debian13, nil, nil, ErrChange},
		{"no Apply", with(func(c *Change) { c.Apply = nil }), debian13, nil, nil, ErrChange},
		{"no stage", with(func(c *Change) { c.Stage = 0 }), debian13, nil, nil, ErrChange},
		{"a relative backup", with(func(c *Change) { c.Backups = []string{"etc/ssh/sshd_config"} }), debian13, nil, nil, ErrChange},
		{"an undo revert would refuse", with(func(c *Change) { c.Rollback.Steps = []revert.Step{{Op: revert.OpRun}} }), debian13, nil, nil, revert.ErrJournal},
		{"a plan step of a unit with no rollback", with(func(c *Change) {
			c.Rollback.Steps = []revert.Step{{Op: revert.OpPlan, Unit: "example", Plan: json.RawMessage(`{}`)}}
		}), debian13, nil, nil, revert.ErrNoPlanRollback},
		{"confirmVia without lockout risk", with(func(c *Change) { c.Risk = RiskPlain; c.ConfirmVia = []revert.Listener{any2230} }), debian13, nil, nil, ErrChange},
		{"one control twice", []Control{&stub{id: "ssh.port", title: "SSH Port"}, &stub{id: "ssh.port", title: "SSH Port"}}, debian13, nil, nil, ErrChange},
		{"a platform outside the support matrix", with(func(*Change) {}),
			detect.Facts{Platform: detect.Platform{Distro: detect.DistroUbuntu, Version: "25.10", Support: detect.OutOfScope}, Init: debian13.Init}, nil, nil, ErrUnsupported},
		{"a platform not established", with(func(*Change) {}), detect.Facts{Init: debian13.Init}, nil, nil, ErrUnsupported},
		{"a change journalled already", with(func(*Change) {}), debian13, func(h *testHost) {
			h.AddDir(revert.DefaultDir, 0o700, 0, 0)
			h.AddFile(journal, pendingJSON(), 0o600, 0, 0)
		}, nil, revert.ErrPending},
		{"no timer: OpenRC installed, not running", with(func(*Change) {}),
			detect.Facts{Platform: debian13.Platform, Init: detect.Init{Kind: detect.OpenRC, Running: detect.No}}, nil, nil, revert.ErrNoTimer},
		{"a hardn the timer cannot run safely", with(func(*Change) {}), debian13, func(h *testHost) {
			h.AddFile(testHardn, []byte("ELF"), 0o777, 0, 0)
		}, nil, revert.ErrExecutable},
		{"a window under the floor", with(func(*Change) {}), debian13, nil, func(o *Options) { o.Timeout = 30 * time.Second }, revert.ErrTimeout},
		{"a session not established", with(func(*Change) {}), debian13, nil, func(o *Options) { o.Session = revert.Session{} }, revert.ErrSessionUnknown},
		{"a run root another user can write", with(func(*Change) {}), debian13, func(h *testHost) {
			h.AddDir(runRoot, 0o777, 0, 0)
		}, nil, revert.ErrUnsafe},
		{"a journal directory to be made under one another user can write", with(func(*Change) {}), debian13, func(h *testHost) {
			h.AddDir("/var/lib", 0o775, 0, 0)
		}, nil, revert.ErrUnsafe},
		{"a session whose SSH_CONNECTION does not parse", with(func(*Change) {}), debian13, nil, func(o *Options) {
			o.Session.Conn = "192.0.2.10 50022 192.0.2.20"
		}, revert.ErrJournal},
		{"a backupPath that is not absolute", with(func(c *Change) { c.Backups = []string{confPath} }), debian13, nil, func(o *Options) {
			o.Backups.Path = "relative/backups"
		}, backup.ErrSettings},
		{"a backup directory another user can change", with(func(*Change) {}), debian13, func(h *testHost) {
			h.AddDir("/srv/backups", 0o777, 1000, 1000)
		}, func(o *Options) { o.Backups.Path = "/srv/backups/hardn" }, backup.ErrUnsafeDir},
		{"a file to back up that is a directory", with(func(c *Change) { c.Backups = []string{"/etc/example"} }), debian13, nil, nil, backup.ErrNotRegular},
		{"a socket drop-in with no backup before it", with(func(c *Change) {
			c.Rollback.Reloads = []revert.Reload{{Service: revert.ReloadSSH, Keywords: []string{"Port"}, After: []revert.Listener{any2230},
				SocketDropIns: []string{"/etc/systemd/system/ssh.socket.d/listen.conf"}}}
		}), debian13, nil, nil, revert.ErrJournal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHost(t)
			if c.host != nil {
				c.host(h)
			}
			o := testOptions(h, nil, nil, c.facts)
			if c.opts != nil {
				c.opts(&o)
			}
			e := New(h, o)
			p, err := e.Plan(ctx, c.controls, nil)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if err := p.Err(); !errors.Is(err, ErrRefused) || !errors.Is(err, c.want) {
				t.Fatalf("Plan.Err: %v; want ErrRefused and %v", err, c.want)
			}
			if _, err := e.Apply(ctx, p); !errors.Is(err, c.want) {
				t.Errorf("Apply: %v, want %v", err, c.want)
			}
			if got := h.changedOutside(runRoot); len(got) > 0 {
				t.Errorf("changed outside %s: %q", runRoot, got)
			}
			if _, ok := h.Entry(runRoot + "/" + p.ID); ok {
				t.Errorf("the plan's run directory is left")
			}
		})
	}
}

// TestRefusedUnderTheLock: a refusal that appears only when the plan is
// made again under the run lock (the host changed since the dry run) stops
// Apply before Arm, as does a plan that is no longer the one shown; nothing
// changes outside the run root.
func TestRefusedUnderTheLock(t *testing.T) {
	ctx := context.Background()
	detectFailed := errors.New("fixture: /etc/os-release cannot be read")
	for _, c := range []struct {
		name   string
		change func(h *testHost, s *stub, detect *error, id string)
		want   error
	}{
		{"a control refuses now", func(_ *testHost, s *stub, _ *error, _ string) { s.err = errors.New("fixture: refused now") }, ErrRefused},
		{"a change journalled since", func(h *testHost, _ *stub, _ *error, _ string) {
			h.AddDir(revert.DefaultDir, 0o700, 0, 0)
			h.AddFile(journal, pendingJSON(), 0o600, 0, 0)
		}, revert.ErrPending},
		{"the backup run directory made since", func(h *testHost, _ *stub, _ *error, id string) {
			h.AddDir(backup.DefaultPath+"/"+id, 0o700, 0, 0)
		}, backup.ErrRunExists},
		{"detection failing now", func(_ *testHost, _ *stub, detect *error, _ string) { *detect = detectFailed }, detectFailed},
		{"a different change", func(_ *testHost, s *stub, _ *error, _ string) { s.changes[0].Summary = "Run ufw allow 2231/tcp" }, ErrPlanChanged},
		{"another change too", func(_ *testHost, s *stub, _ *error, _ string) {
			s.changes = append(s.changes, command("ssh.port/more", StageFiles, RiskLockout, []string{"example-tool"}, nil))
		}, ErrPlanChanged},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHost(t)
			s := &stub{id: "ssh.port", title: "SSH Port", changes: []Change{
				command("ssh.port/allow", StageFirewall, RiskLockout, []string{"ufw", "allow", "2230/tcp"}, []string{"ufw", "delete", "allow", "2230/tcp"}),
			}}
			var detectErr error
			o := testOptions(h, nil, nil, debian13)
			o.Detect = func(context.Context, host.System) (detect.Facts, error) { return debian13, detectErr }
			e := New(h, o)
			p, err := e.Plan(ctx, []Control{s}, nil)
			if err != nil || p.Err() != nil {
				t.Fatalf("Plan: %v, %v", err, p.Err())
			}
			c.change(h, s, &detectErr, p.ID)
			if _, err := e.Apply(ctx, p); !errors.Is(err, c.want) {
				t.Fatalf("Apply: %v, want %v", err, c.want)
			}
			if got := h.changedOutside(runRoot); len(got) > 0 {
				t.Errorf("changed outside %s: %q", runRoot, got)
			}
			if want := 2; c.want == detectFailed && s.plans != 1 || c.want != detectFailed && s.plans != want {
				t.Errorf("planned %d times, want the dry run, then under the lock (unless detection failed)", s.plans)
			}
		})
	}
}

// pendingJSON is a change another run journalled and did not finish.
func pendingJSON() []byte {
	j := revert.Journal{SchemaVersion: revert.SchemaVersion, ID: "20261019T110000Z-5e6f7a8b", Created: testNow.Add(-30 * time.Second),
		Risk: revert.RiskOther, Summary: "Write /etc/example/web.conf"}
	data, err := json.Marshal(j)
	if err != nil {
		panic(err)
	}
	return data
}

// TestCheck: a control that applies is checked; one that does not is
// NotApplicable with its reason, unchecked.
func TestCheck(t *testing.T) {
	h := newHost(t)
	e := New(h, testOptions(h, nil, nil, debian13))
	reps, err := e.Check(context.Background(), []Control{
		&stub{id: "ssh.port", title: "SSH Port"},
		&stub{id: "example.apparmor", title: "AppArmor", why: "controlled by the container's host"},
		fileControl{id: "example.file", path: confPath, want: "listen 2230\n"},
		failingCheck{}, // a check that fails, saying Configured all the same: Unknown
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		state  State
		detail string
		err    bool
	}{{NotConfigured, "SSH Port: not as asked", false}, {NotApplicable, "controlled by the container's host", false}, {NotConfigured, "", false},
		{Unknown, "sshd -T failed", true}}
	for i, w := range want {
		if r := reps[i]; r.Status.State != w.state || r.Status.Detail != w.detail || (r.Err != nil) != w.err {
			t.Errorf("report %d: %+v, want %v %q", i, r, w.state, w.detail)
		}
	}
	if text, _ := NotApplicable.MarshalText(); string(text) != "not-applicable" {
		t.Errorf("NotApplicable is %q", text)
	}
}

// failingCheck is a control whose Check fails but returns Configured.
type failingCheck struct{ fileControl }

func (failingCheck) ID() string    { return "example.failing" }
func (failingCheck) Title() string { return "Failing" }
func (failingCheck) Check(context.Context, host.System, detect.Facts) (Status, error) {
	return Status{State: Configured, Detail: "sshd -T failed"}, errors.New("fixture: sshd -T failed")
}

// TestNotMadeByPlan: Apply refuses a plan Engine.Plan did not make.
func TestNotMadeByPlan(t *testing.T) {
	h := newHost(t)
	e := New(h, testOptions(h, nil, nil, debian13))
	p := &Plan{ID: "20261019T120000Z-0a1b2c3d", Changes: []Change{command("example.x/run", StageFiles, RiskPlain, argv("example-tool"), nil)}}
	if _, err := e.Apply(context.Background(), p); !errors.Is(err, ErrRefused) {
		t.Errorf("Apply: %v, want ErrRefused", err)
	}
	if n := len(h.Calls()); n != 0 {
		t.Errorf("%d calls", n)
	}
}

// TestJournalWithEarlierChanges: a change's undo is checked as Record will
// check it, with the changes before it in the journal: a reload whose
// socketDropIns an earlier change backed up is planned, and the same
// reload alone is refused (revert.ErrJournal).
func TestJournalWithEarlierChanges(t *testing.T) {
	const dropIn = "/etc/systemd/system/ssh.socket.d/listen.conf"
	reload := Change{ID: "ssh.listen/reload", Summary: "Reload ssh", Risk: RiskLockout, Stage: StageReload,
		Rollback: Undo{Reloads: []revert.Reload{{Service: revert.ReloadSSH, Keywords: []string{"Port"}, After: []revert.Listener{any2230}, SocketDropIns: []string{dropIn}}}},
		Apply:    func(context.Context, host.System, Run) (Ran, error) { return Ran{}, nil }}
	migrate := Change{ID: "ssh.listen/migrate", Summary: "Remove " + dropIn, Risk: RiskLockout, Stage: StageFiles, Backups: []string{dropIn},
		Apply: func(context.Context, host.System, Run) (Ran, error) { return Ran{}, nil }}
	for _, c := range []struct {
		name    string
		changes []Change
		want    error
	}{
		{"after the change that backs the drop-in up", []Change{reload, migrate}, nil},
		{"alone", []Change{reload}, revert.ErrJournal},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHost(t)
			h.AddFile(dropIn, []byte("[Socket]\nListenStream=2230\n"), 0o644, 0, 0)
			e := New(h, testOptions(h, nil, nil, debian13))
			p, err := e.Plan(context.Background(), []Control{&stub{id: "ssh.listen", title: "SSH listeners", changes: c.changes}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := p.Err(); c.want == nil && got != nil || c.want != nil && (!errors.Is(got, c.want) || !errors.Is(got, ErrChange) || p.Items[0].Err == nil) {
				t.Errorf("Plan.Err: %v, want %v", got, c.want)
			}
		})
	}
}
