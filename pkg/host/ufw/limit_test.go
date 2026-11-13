package ufw_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// both is an action on port/tcp in IPv4 and IPv6.
func both(action, port string) []modelRule {
	return []modelRule{{fam: ufw.IPv4, action: action, port: port}, {fam: ufw.IPv6, action: action, port: port}}
}

// active is the model running, deny incoming, with rules.
func active(t *testing.T, rules ...modelRule) *model {
	m := newModel(t)
	m.active, m.rules = true, rules
	return m
}

// two is sshd on 22 and 2230, IPv4 and IPv6.
func two() sshd.Listeners {
	ls := []sshd.Listener{listener("0.0.0.0", 22), listener("::", 22), listener("0.0.0.0", 2230), listener("::", 2230)}
	return sshd.Listeners{SSHD: ls, All: ls, Known: true}
}

// TestPlanLimit: which ports a limit replaces the allow of, and what is left
// alone or refused.
func TestPlanLimit(t *testing.T) {
	commented := "### tuple ### allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in comment=616c696365"
	tests := []struct {
		name  string
		m     func(*testing.T) *model
		ls    sshd.Listeners
		pve   detect.Proxmox
		steps []string
		err   error
	}{
		{name: "a plain allow in both versions", m: func(t *testing.T) *model { return active(t, both("allow", "22")...) }, ls: stock22(),
			steps: []string{"ufw limit 22/tcp"}},
		{name: "two ports, each limited", m: func(t *testing.T) *model { return active(t, append(both("allow", "22"), both("allow", "2230")...)...) }, ls: two(),
			steps: []string{"ufw limit 22/tcp", "ufw limit 2230/tcp"}},
		{name: "one limited already, the other not", m: func(t *testing.T) *model { return active(t, append(both("limit", "22"), both("allow", "2230")...)...) }, ls: two(),
			steps: []string{"ufw limit 2230/tcp"}},
		{name: "limited already: nothing to do", m: func(t *testing.T) *model { return active(t, both("limit", "22")...) }, ls: stock22()},
		{name: "IPv6 off in ufw: IPv4's rule alone", m: func(t *testing.T) *model {
			m := active(t, modelRule{fam: ufw.IPv4, action: "allow", port: "22"})
			m.ipv6 = false
			return m
		}, ls: stock22(), steps: []string{"ufw limit 22/tcp"}},
		{name: "the allow in IPv4 alone: a limit would add IPv6's", m: func(t *testing.T) *model {
			return active(t, modelRule{fam: ufw.IPv4, action: "allow", port: "22"})
		}, ls: stock22(), err: ufw.ErrNoPlainAllow},
		{name: "a commented allow is the administrator's", m: func(t *testing.T) *model {
			return active(t, modelRule{fam: ufw.IPv4, tuple: commented, row: "22/tcp                     ALLOW IN    Anywhere                   # alice"},
				modelRule{fam: ufw.IPv6, action: "allow", port: "22"})
		}, ls: stock22(), err: ufw.ErrNoPlainAllow},
		{name: "allow OpenSSH is another tuple", m: func(t *testing.T) *model {
			return active(t, modelRule{fam: ufw.IPv4, tuple: "### tuple ### allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 OpenSSH - in", row: "22/tcp (OpenSSH)           ALLOW IN    Anywhere"},
				modelRule{fam: ufw.IPv6, tuple: "### tuple ### allow tcp 22 ::/0 any ::/0 OpenSSH - in", row: "22/tcp (OpenSSH (v6))      ALLOW IN    Anywhere (v6)"})
		}, ls: stock22(), err: ufw.ErrNoPlainAllow},
		{name: "a deny for some sources ahead of the allow", m: func(t *testing.T) *model {
			return active(t, append([]modelRule{{fam: ufw.IPv4, tuple: "### tuple ### deny tcp 22 0.0.0.0/0 any 198.51.100.0/24 in", row: "22/tcp                     DENY IN     198.51.100.0/24"}},
				both("allow", "22")...)...)
		}, ls: stock22(), err: ufw.ErrNoPlainAllow}, // IPv4 is Excepted, not Allowed, so the pair is left alone; IPv6 is open with no limit
		{name: "allowed by the default policy alone", m: func(t *testing.T) *model {
			m := active(t)
			m.incoming = "allow"
			return m
		}, ls: stock22(), err: ufw.ErrNoPlainAllow},
		{name: "from some sources only: nothing to limit", m: func(t *testing.T) *model {
			return active(t, modelRule{fam: ufw.IPv4, tuple: "### tuple ### allow tcp 22 0.0.0.0/0 any 192.0.2.0/24 in", row: "22/tcp                     ALLOW IN    192.0.2.0/24"})
		}, ls: stock22()},
		{name: "inactive", m: func(t *testing.T) *model {
			m := newModel(t)
			m.rules = both("allow", "22")
			return m
		}, ls: stock22(), err: ufw.ErrInactive},
		{name: "running, ENABLED=no", m: func(t *testing.T) *model {
			m := active(t, both("allow", "22")...)
			m.mismatch = true
			return m
		}, ls: stock22(), err: ufw.ErrEnabledMismatch},
		{name: "the status not read whole", m: func(t *testing.T) *model {
			m := active(t, both("allow", "22")...)
			m.garbage = true
			return m
		}, ls: stock22(), err: ufw.ErrStateUnknown},
		{name: "a rule file line not read", m: func(t *testing.T) *model {
			return active(t, append(both("allow", "22"), modelRule{fam: ufw.IPv4, tuple: "### tuple ### allow tcp", row: "23/tcp                     ALLOW IN    Anywhere"})...)
		}, ls: stock22(), err: ufw.ErrStateUnknown},
		{name: "the listeners not established", m: func(t *testing.T) *model { return active(t, both("allow", "22")...) },
			ls: sshd.Listeners{All: stock22().All}, err: ufw.ErrListenersUnknown},
		{name: "no listener", m: func(t *testing.T) *model { return active(t, both("allow", "22")...) },
			ls: sshd.Listeners{Known: true}, err: ufw.ErrListenersUnknown},
		{name: "Proxmox VE not established", m: func(t *testing.T) *model { return active(t, both("allow", "22")...) }, ls: stock22(),
			pve: detect.Proxmox{}, err: ufw.ErrProxmoxUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pve := notPVE
			if tt.err == ufw.ErrProxmoxUnknown {
				pve = tt.pve
			}
			p, err := ufw.PlanLimit(read(t, tt.m(t)), tt.ls, pve)
			if !errors.Is(err, tt.err) || tt.err == nil && err != nil {
				t.Fatalf("PlanLimit = %v, want %v", err, tt.err)
			}
			if got := argvs(p.Steps); !slices.Equal(got, tt.steps) {
				t.Errorf("steps %q, want %q", got, tt.steps)
			}
			for _, s := range p.Steps {
				if s.Kind != ufw.StepLimit || s.Undo[1] != "allow" || s.Undo[2] != s.Argv[2] {
					t.Errorf("step %v: kind %s, undo %q", s.Argv, s.Kind, s.Undo)
				}
			}
			for i, c := range p.After {
				if p.Before[i].Verdict.OK() && !c.Verdict.OK() {
					t.Errorf("%s: %s before, %s after", c.Need.Why, p.Before[i].Verdict, c.Verdict)
				}
			}
		})
	}
	if p, err := ufw.PlanLimit(ufw.State{}, stock22(), detect.Proxmox{Installed: detect.Yes}); err != nil || !p.NotApplicable {
		t.Errorf("Proxmox VE: %+v, %v; want NotApplicable", p, err)
	}
	if _, err := ufw.PlanLimit(ufw.State{}, stock22(), notPVE); !errors.Is(err, ufw.ErrNotInstalled) {
		t.Errorf("no ufw: %v, want ErrNotInstalled", err)
	}
}

// TestLimitPorts: none where ufw's state is not what a limit needs.
func TestLimitPorts(t *testing.T) {
	m := active(t, both("allow", "22")...)
	if got := ufw.LimitPorts(read(t, m), stock22()); !slices.Equal(got, []uint16{22}) {
		t.Errorf("active = %v, want [22]", got)
	}
	m.active = false
	if got := ufw.LimitPorts(read(t, m), stock22()); got != nil {
		t.Errorf("inactive = %v, want none", got)
	}
	if got := ufw.LimitPorts(ufw.State{}, stock22()); got != nil {
		t.Errorf("not installed = %v, want none", got)
	}
}

// TestExecuteLimit: the limit replaces the allow in both IP versions, ufw
// then shows the port Limited, the second plan is empty, the journalled undo
// is the allow, and it puts the allow back.
func TestExecuteLimit(t *testing.T) {
	ctx := context.Background()
	m := active(t, both("allow", "22")...)
	h := m.host()
	p, err := ufw.PlanLimit(read(t, m), stock22(), notPVE)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := p.PreUndo(), [][]string{{"ufw", "allow", "22/tcp"}}; !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Errorf("PreUndo %q, want %q", got, want)
	}
	rep, err := ufw.Execute(ctx, h, p)
	if err != nil || !rep.Verified {
		t.Fatalf("Execute: %v, Verified %t (%s)", err, rep.Verified, rep.Unverified)
	}
	if got, want := runs(h), []string{"ufw limit 22/tcp", "ufw status verbose"}; !slices.Equal(got, want) {
		t.Errorf("runs %q, want %q", got, want)
	}
	for _, c := range rep.Coverage {
		if c.Verdict != ufw.Limited {
			t.Errorf("%s (%s %d): %s by %q, want limited", c.Need.Why, c.Need.Family, c.Need.Port, c.Verdict, c.By)
		}
	}
	if len(m.rules) != 2 {
		t.Errorf("rules %+v: the limit added one, or removed one", m.rules)
	}
	if again, err := ufw.PlanLimit(read(t, m), stock22(), notPVE); err != nil || !again.Empty() {
		t.Errorf("second plan %q, %v; want empty", argvs(again.Steps), err)
	}
	if got, want := undos(rep), []string{"ufw allow 22/tcp"}; !slices.Equal(got, want) {
		t.Errorf("undo %q, want %q", got, want)
	}
	if err := ufw.Rollback(ctx, h, rep); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.rules, both("allow", "22")) {
		t.Errorf("after the undo: %+v, want the two allows", m.rules)
	}
}

// TestExecuteLimitFails: a limit that fails is undone all the same, and one
// ufw answers without saving is caught by the status.
func TestExecuteLimitFails(t *testing.T) {
	ctx := context.Background()
	m := active(t, both("allow", "22")...)
	m.fail = "ufw limit 22/tcp"
	h := m.host()
	p, err := ufw.PlanLimit(read(t, m), stock22(), notPVE)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ufw.Execute(ctx, h, p)
	if err == nil || rep.Verified {
		t.Fatalf("Execute: %v, Verified %t; want the step's error", err, rep.Verified)
	}
	if got, want := undos(rep), []string{"ufw allow 22/tcp"}; !slices.Equal(got, want) {
		t.Errorf("undo %q, want %q", got, want)
	}
	if !slices.Equal(m.rules, both("allow", "22")) {
		t.Errorf("rules %+v: SSH's allow must still be there", m.rules)
	}

	m = active(t, both("allow", "22")...)
	m.noop = "ufw limit 22/tcp"
	h = m.host()
	if p, err = ufw.PlanLimit(read(t, m), stock22(), notPVE); err != nil {
		t.Fatal(err)
	}
	if rep, err = ufw.Execute(ctx, h, p); !errors.Is(err, ufw.ErrVerify) || rep.Verified {
		t.Errorf("a limit not saved: %v, Verified %t; want ErrVerify", err, rep.Verified)
	}
}
