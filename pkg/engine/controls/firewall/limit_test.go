package firewall_test

import (
	"context"
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// TestLimit: Limit SSH replaces the plain allow of SSH's port with a limit,
// a plain change whose undo, the allow, is journalled before it runs; ufw
// then shows the port limited, the row stays Configured, and a second plan
// is empty.
func TestLimit(t *testing.T) {
	ctx := context.Background()
	l := liveOn(t, site{plat: debian13(), ufw: allow22})
	if r := firewall.Read(ctx, l, l.facts, nil, remote22); !slices.Equal(r.LimitPorts(), []uint16{22}) {
		t.Fatalf("offered for %v, want [22]", r.LimitPorts())
	}
	e, p := planOn(t, l, firewall.ActionLimitSSH, remote22, nil)
	if p.Err() != nil || len(p.Changes) != 1 {
		t.Fatalf("plan: %d changes, %v", len(p.Changes), p.Err())
	}
	c := p.Changes[0]
	if c.ID != firewall.ID+"/limit" || c.Risk != engine.RiskPlain || p.Risk != engine.RiskPlain || c.Stage != engine.StageFirewall ||
		c.Title != "Limit 22/tcp" || c.Target != "ufw" || c.StateBefore != "allow 22/tcp" || c.StateAfter != "limit 22/tcp" ||
		len(c.Findings) != 0 || undoWords(c) != "[ufw allow 22/tcp]" || !slices.Equal(c.Resources, []string{steps.ResourceUFW}) {
		t.Errorf("change %+v", c)
	}
	if got := commands(p); !slices.Equal(got, []string{"ufw limit 22/tcp"}) {
		t.Errorf("commands %q", got)
	}
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got := ranUFW(l); !slices.Equal(got, []string{"ufw limit 22/tcp"}) {
		t.Errorf("ran %q", got)
	}
	if len(l.u.rules) != 1 || l.u.rules[0].action != "limit" || !l.u.active {
		t.Errorf("after: active %t, rules %+v", l.u.active, l.u.rules)
	}
	if st, err := (firewall.Incoming{}).Check(ctx, l, l.facts); err != nil || st.State != engine.Configured {
		t.Errorf("the row after: %s %q, %v", st.State, st.Detail, err)
	}
	r := firewall.Read(ctx, l, l.facts, nil, remote22)
	if r.LimitPorts() != nil || r.First != firewall.ItemDisable {
		t.Errorf("after: offered for %v, item %d", r.LimitPorts(), r.First)
	}
	if _, p = planOn(t, l, firewall.ActionLimitSSH, remote22, nil); p.Err() != nil || !p.Empty() {
		t.Errorf("again: %d changes, %v", len(p.Changes), p.Err())
	}
}

// TestLimitTwoPorts: sshd on two ports, each with its plain allow: one
// change names and limits both.
func TestLimitTwoPorts(t *testing.T) {
	l := liveOn(t, site{plat: debian13(), ports: []uint16{22, 2230}, ufw: fw{active: true, rules: allow("22", "2230")}})
	_, p := planOn(t, l, firewall.ActionLimitSSH, remote22, nil)
	if p.Err() != nil || len(p.Changes) != 1 {
		t.Fatalf("plan: %d changes, %v", len(p.Changes), p.Err())
	}
	c := p.Changes[0]
	if got := commands(p); !slices.Equal(got, []string{"ufw limit 22/tcp", "ufw limit 2230/tcp"}) || c.Title != "Limit 22/tcp, 2230/tcp" ||
		c.StateBefore != "allow 22/tcp, 2230/tcp" || undoWords(c) != "[ufw allow 2230/tcp] [ufw allow 22/tcp]" {
		t.Errorf("commands %q, change %+v", got, c)
	}
}

// TestLimitRollback: a limit that fails is rolled back with the allow, so
// SSH's port is never left without a rule that lets it through.
func TestLimitRollback(t *testing.T) {
	l := liveOn(t, site{plat: debian13(), ufw: allow22})
	l.fail = "ufw limit 22/tcp"
	e, p := planOn(t, l, firewall.ActionLimitSSH, remote22, nil)
	if p.Err() != nil {
		t.Fatal(p.Err())
	}
	if _, err := e.Apply(context.Background(), p); err == nil {
		t.Fatalf("no failure; ran %q", ranUFW(l))
	}
	if got, want := ranUFW(l), []string{"ufw limit 22/tcp", "ufw allow 22/tcp"}; !slices.Equal(got, want) {
		t.Errorf("ran %q, want %q", got, want)
	}
	if len(l.u.rules) != 1 || l.u.rules[0].action != "allow" {
		t.Errorf("rules %+v: the allow must still be there", l.u.rules)
	}
}

// TestLimitNothing: an empty plan, and no item, where every SSH port is
// limited already, where SSH is let through from some sources only, and on
// a host with no SSH server.
func TestLimitNothing(t *testing.T) {
	ctx := context.Background()
	for name, s := range map[string]site{
		"limited already":  {plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "limit", port: "22"}}}},
		"from some source": {plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "allow", port: "22", src: "192.0.2.0/24"}}}},
		"no SSH server":    {plat: debian13(), failT: noSSHD, noMain: true, dpkg: noServer, bound: []string{}, ufw: allow22},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, s)
			if r := firewall.Read(ctx, l, l.facts, nil, console); r.LimitPorts() != nil {
				t.Errorf("offered for %v", r.LimitPorts())
			}
			if _, p := planOn(t, l, firewall.ActionLimitSSH, console, nil); p.Err() != nil || !p.Empty() {
				t.Errorf("plan: %d changes, %v", len(p.Changes), p.Err())
			}
			changedNothing(t, l)
		})
	}
}

// TestLimitRefusals: every host on which Limit SSH can't show that it
// replaces hardn's kind of rule alone refuses before any ufw command but
// the read, and offers no item.
func TestLimitRefusals(t *testing.T) {
	ctx := context.Background()
	const (
		wordsPorts    = "hardn can't tell which ports SSH uses."
		wordsState    = "hardn can't read ufw's state. Check ufw status verbose, then try again."
		wordsInactive = "ufw isn't running, so it limits nothing. Enable the firewall (Firewall, option 1), then try again."
		wordsPlain    = "ufw's rule for SSH isn't a plain allow of its port, and hardn doesn't change a rule it finds. Set the limit with ufw limit."
	)
	failing := &hostfake.Response{Stderr: "ERROR: You need to be root to run this script\n", ExitCode: 1}
	for name, tc := range map[string]struct {
		s     site
		words string
		want  error
	}{
		"ss missing":                {site{plat: noSS(debian13()), ufw: allow22}, wordsNoSS, firewall.ErrNoSS},
		"ss fails":                  {site{plat: debian13(), ufw: allow22, ssErr: &hostfake.Response{Stderr: "ss: failed\n", ExitCode: 1}}, wordsSSBad, firewall.ErrSSFailed},
		"a listener not bound":      {site{plat: debian13(), ports: []uint16{2230}, bound: []string{"0.0.0.0:22", "[::]:22"}, ufw: fw{active: true, rules: allow("2230")}}, wordsStale, firewall.ErrNotInEffect},
		"sshd -T fails":             {site{plat: debian13(), ufw: allow22, failT: &hostfake.Response{Stderr: "sshd: no hostkeys available -- exiting.\n", ExitCode: 1}}, wordsPorts, ufw.ErrListenersUnknown},
		"no ufw":                    {site{plat: debian13(), ufw: fw{missing: true}}, "ufw isn't installed. Install it, then try again.", ufw.ErrNotInstalled},
		"ufw's status fails":        {site{plat: debian13(), ufw: fw{fail: failing}}, wordsState, ufw.ErrStateUnknown},
		"ufw's status not read":     {site{plat: debian13(), ufw: with(allow22, func(u *fw) { u.garbage = true })}, wordsState, ufw.ErrStateUnknown},
		"inactive":                  {site{plat: debian13(), ufw: fw{rules: allow("22")}}, wordsInactive, ufw.ErrInactive},
		"running, not set to start": {site{plat: debian13(), ufw: with(allow22, func(u *fw) { u.enabled = "no" })}, "ufw is running but isn't set to start at boot. Run ufw enable.", ufw.ErrEnabledMismatch},
		"allow OpenSSH":             {site{plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "allow", port: "22", app: "OpenSSH"}}}}, wordsPlain, ufw.ErrNoPlainAllow},
		"the allow in IPv4 alone":   {site{plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "allow", port: "22", v4: true}}}}, wordsPlain, ufw.ErrNoPlainAllow},
		"the policy allow, no rule": {site{plat: debian13(), ufw: fw{active: true, incoming: "allow"}}, wordsPlain, ufw.ErrNoPlainAllow},
		"Proxmox VE":                {site{plat: pve9(), ufw: allow22, pve: pf{cluster: "[OPTIONS]\nenable: 1\n"}}, "hardn reads pve-firewall and doesn't change it: a wrong rule can cut off the web UI and the cluster.", ufw.ErrNotApplicable},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, tc.s)
			if r := firewall.Read(ctx, l, l.facts, nil, remote22); r.LimitPorts() != nil {
				t.Errorf("offered for %v", r.LimitPorts())
			}
			_, p := planOn(t, l, firewall.ActionLimitSSH, remote22, nil)
			refused(t, p, tc.words, tc.want)
			changedNothing(t, l)
		})
	}
}

// TestLimitEL: on Rocky Linux firewalld has no such rule: refused, with no
// ufw command, and no item.
func TestLimitEL(t *testing.T) {
	ctx := context.Background()
	e := &elHost{installed: true, active: "active", enabled: "enabled"}
	h, facts := e.site().build(t)
	h.Respond(hostfake.Response{Stdout: "running\n"}, "firewall-cmd", "--state")
	h.Respond(hostfake.Response{Stdout: "public\n"}, "firewall-cmd", "--get-default-zone")
	h.Respond(hostfake.Response{Stdout: "public (default)\n  interfaces: eth0\n"}, "firewall-cmd", "--get-active-zones")
	cs, err := firewall.Incoming{Action: firewall.ActionLimitSSH}.Plan(ctx, h, engine.Input{Facts: facts, Config: &config.Config{}})
	if cs != nil || err == nil || codeOf(err) != "FIREWALL_EL_READ_ONLY" {
		t.Errorf("plan %+v, %v (%s)", cs, err, codeOf(err))
	}
	if r := firewall.Read(ctx, h, facts, nil, remote22); r.LimitPorts() != nil {
		t.Errorf("offered for %v", r.LimitPorts())
	}
	noUFWRun(t, e.ran)
}
