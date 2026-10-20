package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

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
	for _, c := range []struct {
		name   string
		change func(h *testHost, s *stub)
		want   error
	}{
		{"a control refuses now", func(_ *testHost, s *stub) { s.err = errors.New("fixture: refused now") }, ErrRefused},
		{"a change journalled since", func(h *testHost, _ *stub) {
			h.AddDir(revert.DefaultDir, 0o700, 0, 0)
			h.AddFile(journal, pendingJSON(), 0o600, 0, 0)
		}, revert.ErrPending},
		{"a different change", func(_ *testHost, s *stub) { s.changes[0].Summary = "Run ufw allow 2231/tcp" }, ErrPlanChanged},
		{"another change too", func(_ *testHost, s *stub) {
			s.changes = append(s.changes, command("ssh.port/more", StageFiles, RiskLockout, []string{"example-tool"}, nil))
		}, ErrPlanChanged},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHost(t)
			s := &stub{id: "ssh.port", title: "SSH Port", changes: []Change{
				command("ssh.port/allow", StageFirewall, RiskLockout, []string{"ufw", "allow", "2230/tcp"}, []string{"ufw", "delete", "allow", "2230/tcp"}),
			}}
			e := New(h, testOptions(h, nil, nil, debian13))
			p, err := e.Plan(ctx, []Control{s}, nil)
			if err != nil || p.Err() != nil {
				t.Fatalf("Plan: %v, %v", err, p.Err())
			}
			c.change(h, s)
			if _, err := e.Apply(ctx, p); !errors.Is(err, c.want) {
				t.Fatalf("Apply: %v, want %v", err, c.want)
			}
			if got := h.changedOutside(runRoot); len(got) > 0 {
				t.Errorf("changed outside %s: %q", runRoot, got)
			}
			if s.plans != 2 {
				t.Errorf("planned %d times, want 2 (the dry run, then under the lock)", s.plans)
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
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		state  State
		detail string
	}{{NotConfigured, "SSH Port: not as asked"}, {NotApplicable, "controlled by the container's host"}, {NotConfigured, ""}}
	for i, w := range want {
		if r := reps[i]; r.Status.State != w.state || r.Status.Detail != w.detail || r.Err != nil {
			t.Errorf("report %d: %+v, want %v %q", i, r, w.state, w.detail)
		}
	}
	if text, _ := NotApplicable.MarshalText(); string(text) != "not-applicable" {
		t.Errorf("NotApplicable is %q", text)
	}
}
