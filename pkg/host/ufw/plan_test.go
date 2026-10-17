package ufw_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// hardening is the legacy sequence's end state, without its disable and
// reset: default deny incoming, allow outgoing, enabled.
var hardening = ufw.Desired{Enable: true, Incoming: ufw.PolicyDeny, Outgoing: ufw.PolicyAllow}

func read(t *testing.T, m *model) ufw.State {
	t.Helper()
	st, err := ufw.Read(context.Background(), m.host(), notPVE)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func argvs(steps []ufw.Step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, strings.Join(s.Argv, " "))
	}
	return out
}

// noDisableOrReset: decision §3.17; "ufw disable" is only ever an undo.
func noDisableOrReset(t *testing.T, p ufw.Plan) {
	t.Helper()
	for _, s := range p.Steps {
		if slices.Contains(s.Argv, "disable") || slices.Contains(s.Argv, "reset") {
			t.Errorf("step %q", s.Argv)
		}
	}
}

func TestPlanFor(t *testing.T) {
	allow := func(fam ufw.Family, port string) modelRule { return modelRule{fam: fam, action: "allow", port: port} }
	both := func(port string) []modelRule { return []modelRule{allow(ufw.IPv4, port), allow(ufw.IPv6, port)} }
	for _, tc := range []struct {
		name    string
		setup   func(m *model)
		add     []uint16
		desired ufw.Desired
		want    []string
		finding []string
	}{
		{"fresh and inactive: SSH and the new port, then enable", nil, []uint16{2230}, hardening,
			[]string{"ufw allow 22/tcp", "ufw allow 2230/tcp", "ufw --force enable"}, nil},
		{"active, both let through: nothing to do", func(m *model) {
			m.active = true
			m.rules = append(both("22"), both("2230")...)
		}, []uint16{2230}, hardening, nil, nil},
		{"active, default allow: rules before the default deny", func(m *model) { m.active, m.incoming = true, "allow" },
			[]uint16{2230}, hardening, []string{"ufw allow 22/tcp", "ufw allow 2230/tcp", "ufw default deny incoming"}, nil},
		{"inactive and staying so: the rules are saved for a later enable", nil, nil, ufw.Desired{},
			[]string{"ufw allow 22/tcp"}, nil},
		{"an existing limit rule counts", func(m *model) {
			m.rules = []modelRule{{fam: ufw.IPv4, action: "limit", port: "22"}, {fam: ufw.IPv6, action: "limit", port: "22"}}
		}, nil, hardening, []string{"ufw --force enable"}, nil},
		{"IPv6 missing only: the port's rule is added for both", func(m *model) { m.rules = []modelRule{allow(ufw.IPv4, "22")} },
			nil, hardening, []string{"ufw allow 22/tcp", "ufw --force enable"}, nil},
		{"active with a deny already in force: reported, not overridden", func(m *model) {
			m.active = true
			m.rules = []modelRule{{fam: ufw.IPv4, action: "deny", port: "22"}, allow(ufw.IPv6, "22")}
		}, nil, hardening, nil, []string{"ssh-not-allowed IPv4 22"}},
		{"active, a deny in IPv4: the port is left alone in both", func(m *model) {
			m.active = true
			m.rules = []modelRule{{fam: ufw.IPv4, action: "deny", port: "22"}}
		}, nil, hardening, nil, []string{"ssh-not-allowed IPv4 22", "ssh-not-allowed IPv6 22"}},
		{"outgoing changed after the rules", nil, nil, ufw.Desired{Enable: true, Outgoing: ufw.PolicyDeny},
			[]string{"ufw allow 22/tcp", "ufw default deny outgoing", "ufw --force enable"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(t)
			if tc.setup != nil {
				tc.setup(m)
			}
			p, err := ufw.PlanFor(ufw.Input{Proxmox: notPVE, Listeners: stock22(), Add: tc.add, State: read(t, m), Desired: tc.desired})
			if err != nil {
				t.Fatal(err)
			}
			if got := argvs(p.Steps); !slices.Equal(got, tc.want) {
				t.Errorf("steps\n got %q\nwant %q", got, tc.want)
			}
			var fs []string
			for _, f := range p.Findings {
				fs = append(fs, strings.Join([]string{f.Kind.String(), f.Coverage.Need.Family.String(), "22"}, " "))
			}
			if !slices.Equal(fs, tc.finding) {
				t.Errorf("findings %q, want %q", fs, tc.finding)
			}
			noDisableOrReset(t, p)
		})
	}
}

func TestPlanForRefuses(t *testing.T) {
	stateOf := func(setup func(m *model)) func(t *testing.T) ufw.State {
		return func(t *testing.T) ufw.State {
			m := newModel(t)
			if setup != nil {
				setup(m)
			}
			return read(t, m)
		}
	}
	unknown := stock22()
	unknown.Known = false
	for _, tc := range []struct {
		name    string
		pve     detect.Proxmox
		ls      sshd.Listeners
		state   func(t *testing.T) ufw.State
		add     []uint16
		desired ufw.Desired
		want    error
	}{
		{"Proxmox VE not established", detect.Proxmox{}, stock22(), stateOf(nil), nil, hardening, ufw.ErrProxmoxUnknown},
		{"listeners not Known", notPVE, unknown, stateOf(nil), nil, hardening, ufw.ErrListenersUnknown},
		{"no listener", notPVE, sshd.Listeners{Known: true}, stateOf(nil), nil, hardening, ufw.ErrListenersUnknown},
		{"ufw not installed", notPVE, stock22(), func(*testing.T) ufw.State { return ufw.State{} }, nil, hardening, ufw.ErrNotInstalled},
		{"an unread rule line", notPVE, stock22(), stateOf(func(m *model) {
			m.rules = []modelRule{{fam: ufw.IPv4, tuple: "### tuple ### allow tcp 22 in"}}
		}), nil, hardening, ufw.ErrStateUnknown},
		{"a saved deny would take effect", notPVE, stock22(), stateOf(func(m *model) {
			m.rules = []modelRule{{fam: ufw.IPv4, action: "deny", port: "22"}}
		}), nil, hardening, ufw.ErrBlocks},
		{"a saved deny in IPv4 only: no allow turns it round", notPVE, stock22(), stateOf(func(m *model) {
			m.rules = []modelRule{{fam: ufw.IPv4, action: "deny", port: "22"}, {fam: ufw.IPv6, action: "deny", port: "2222"}}
		}), nil, hardening, ufw.ErrBlocks},
		{"a saved restricted allow would take effect", notPVE, stock22(), stateOf(func(m *model) {
			m.rules = []modelRule{{fam: ufw.IPv4, tuple: "### tuple ### allow tcp 22 0.0.0.0/0 any 192.0.2.0/24 in"}}
		}), nil, hardening, ufw.ErrBlocks},
		{"IPV6=no: enabling drops sshd's [::]:22", notPVE, stock22(), stateOf(func(m *model) { m.ipv6 = false }), nil, hardening, ufw.ErrBlocks},
		{"the new port is denied", notPVE, stock22(), stateOf(func(m *model) {
			m.active = true
			m.rules = []modelRule{{fam: ufw.IPv4, action: "deny", port: "2230"}}
		}), []uint16{2230}, hardening, ufw.ErrBlocks},
		{"a policy ufw does not have", notPVE, stock22(), stateOf(nil), nil, ufw.Desired{Incoming: "drop"}, fs.ErrInvalid},
		{"port 0", notPVE, stock22(), stateOf(nil), []uint16{0}, hardening, fs.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ufw.PlanFor(ufw.Input{Proxmox: tc.pve, Listeners: tc.ls, Add: tc.add, State: tc.state(t), Desired: tc.desired})
			if !errors.Is(err, tc.want) {
				t.Errorf("PlanFor: %v, want %v", err, tc.want)
			}
			var be *ufw.BlockError
			if errors.As(err, &be) && (len(be.Coverage) == 0 || !strings.Contains(err.Error(), "would be")) {
				t.Errorf("BlockError %v", err)
			}
		})
	}
}

// TestPlanForProxmox: never touched (brief §2; V10), whatever is asked.
func TestPlanForProxmox(t *testing.T) {
	p, err := ufw.PlanFor(ufw.Input{Proxmox: detect.Proxmox{Installed: detect.Yes}, Desired: hardening, Add: []uint16{2230}})
	if err != nil || !p.NotApplicable || !p.Empty() {
		t.Fatalf("PlanFor: %+v, %v", p, err)
	}
	m := newModel(t)
	h := m.host()
	if _, err := ufw.Execute(context.Background(), h, p); err != nil || len(h.Calls()) != 0 {
		t.Errorf("Execute: %v, calls %v", err, h.Calls())
	}
}
