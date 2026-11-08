package ufw_test

import (
	"context"
	"errors"
	"fmt"
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
			checkPlan(t, p, tc.want, tc.finding)
		})
	}
}

func checkPlan(t *testing.T, p ufw.Plan, want, finding []string) {
	t.Helper()
	if got := argvs(p.Steps); !slices.Equal(got, want) {
		t.Errorf("steps\n got %q\nwant %q", got, want)
	}
	var fs []string
	for _, f := range p.Findings {
		fs = append(fs, fmt.Sprintf("%s %s %d", f.Kind, f.Coverage.Need.Family, f.Coverage.Need.Port))
	}
	if !slices.Equal(fs, finding) {
		t.Errorf("findings %q, want %q", fs, finding)
	}
	noDisableOrReset(t, p)
}

// TestPlanForRules: what the allow would change, the states ufw can be in,
// and the findings beside the steps.
func TestPlanForRules(t *testing.T) {
	v4 := []sshd.Listener{listener("0.0.0.0", 22)}
	v4only := sshd.Listeners{SSHD: v4, All: v4, Known: true}
	lo := []sshd.Listener{listener("127.0.0.1", 22), listener("0.0.0.0", 2230), listener("::", 2230)}
	deny := func(fam ufw.Family, port string) modelRule { return modelRule{fam: fam, action: "deny", port: port} }
	allow := func(fam ufw.Family, port string) modelRule { return modelRule{fam: fam, action: "allow", port: port} }
	for _, tc := range []struct {
		name    string
		setup   func(m *model)
		ls      sshd.Listeners // stock22 when not Known
		add     []uint16
		desired ufw.Desired
		want    []string
		finding []string
	}{
		{name: "IPv4 listeners, a deny in IPv6: the allow would turn it round, so no rule",
			setup: func(m *model) { m.rules = []modelRule{deny(ufw.IPv6, "22")} }, ls: v4only,
			finding: []string{"ssh-not-allowed-at-start IPv4 22"}},
		{name: "a limit of OpenSSH in IPv4 only: another tuple, not replaced", setup: func(m *model) {
			m.rules = []modelRule{{fam: ufw.IPv4, tuple: "### tuple ### limit tcp 22 0.0.0.0/0 any 0.0.0.0/0 OpenSSH - in"}}
		}, desired: hardening, want: []string{"ufw allow 22/tcp", "ufw --force enable"}},
		{name: "a new port under a saved deny, ufw staying inactive: reported", setup: func(m *model) {
			m.rules = []modelRule{deny(ufw.IPv4, "2230"), deny(ufw.IPv6, "2230")}
		}, add: []uint16{2230}, want: []string{"ufw allow 22/tcp"},
			finding: []string{"ssh-not-allowed-at-start IPv4 2230", "ssh-not-allowed-at-start IPv6 2230"}},
		{name: "NoSaveWhileInactive: nothing saved, and what a start would block reported", add: []uint16{2230},
			desired: ufw.Desired{NoSaveWhileInactive: true}, finding: []string{"ssh-not-allowed-at-start IPv4 22",
				"ssh-not-allowed-at-start IPv6 22", "ssh-not-allowed-at-start IPv4 2230", "ssh-not-allowed-at-start IPv6 2230"}},
		{name: "NoSaveWhileInactive does not stop an enable's rules", desired: ufw.Desired{Enable: true, NoSaveWhileInactive: true},
			want: []string{"ufw allow 22/tcp", "ufw --force enable"}},
		{name: "a loopback listener: before.rules lets it through", setup: func(m *model) {
			m.active = true
			m.rules = []modelRule{allow(ufw.IPv4, "2230"), allow(ufw.IPv6, "2230")}
		}, ls: sshd.Listeners{SSHD: lo, All: lo, Known: true}, desired: hardening},
		{name: "no IPv6 in the kernel: IPv6 needs nothing", setup: func(m *model) { m.noKernel6 = true }, add: []uint16{2230},
			desired: hardening, want: []string{"ufw allow 22/tcp", "ufw allow 2230/tcp", "ufw --force enable"}},
		{name: "the new port opened wider than the restricted current one", setup: func(m *model) {
			m.active, m.ipv6 = true, false
			m.rules = []modelRule{{fam: ufw.IPv4, tuple: "### tuple ### allow tcp 22 0.0.0.0/0 any 192.0.2.0/24 in",
				row: "22/tcp                     ALLOW IN    192.0.2.0/24"}}
		}, ls: v4only, add: []uint16{2230}, desired: hardening, want: []string{"ufw allow 2230/tcp"},
			finding: []string{"ssh-restricted IPv4 22", "ssh-new-port-wider IPv4 2230"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(t)
			if tc.setup != nil {
				tc.setup(m)
			}
			if !tc.ls.Known {
				tc.ls = stock22()
			}
			p, err := ufw.PlanFor(ufw.Input{Proxmox: notPVE, Listeners: tc.ls, Add: tc.add, State: read(t, m), Desired: tc.desired})
			if err != nil {
				t.Fatal(err)
			}
			checkPlan(t, p, tc.want, tc.finding)
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
		{"a saved limit in IPv4 only: the allow would replace it", notPVE, stock22(), stateOf(func(m *model) {
			m.rules = []modelRule{{fam: ufw.IPv4, action: "limit", port: "22"}}
		}), nil, hardening, ufw.ErrBlocks},
		{"a saved allow with a comment in IPv4 only: the allow would replace it", notPVE, stock22(), stateOf(func(m *model) {
			m.rules = []modelRule{{fam: ufw.IPv4, tuple: "### tuple ### allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in comment=6b657074"}}
		}), nil, hardening, ufw.ErrBlocks},
		{"a saved blocklist would take effect", notPVE, stock22(), stateOf(func(m *model) {
			m.rules = []modelRule{{fam: ufw.IPv4, tuple: "### tuple ### deny tcp 22 0.0.0.0/0 any 198.51.100.0/24 in"},
				{fam: ufw.IPv4, action: "allow", port: "22"}, {fam: ufw.IPv6, action: "allow", port: "22"}}
		}), nil, hardening, ufw.ErrBlocks},
		{"enabled at boot, not running", notPVE, stock22(), stateOf(func(m *model) { m.mismatch = true }), nil, ufw.Desired{}, ufw.ErrEnabledMismatch},
		{"running, not enabled at boot", notPVE, stock22(), stateOf(func(m *model) { m.active, m.mismatch = true, true }), nil, ufw.Desired{}, ufw.ErrEnabledMismatch},
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

// TestPlanForNoSSH: with no SSH server (Input.NoSSH) there is no listener
// to let through: Enable sets the policy and starts ufw with no allow but
// Keep's, its saved rules standing; listeners given with NoSSH are refused
// as a contradiction, and without NoSSH no listener is still refused.
func TestPlanForNoSSH(t *testing.T) {
	stateOf := func(setup func(m *model)) func(t *testing.T) ufw.State {
		return func(t *testing.T) ufw.State {
			m := newModel(t)
			if setup != nil {
				setup(m)
			}
			return read(t, m)
		}
	}
	for _, tc := range []struct {
		name string
		keep []uint16
		m    func(*model)
		want []string
	}{
		{"inactive", nil, nil, []string{"ufw --force enable"}},
		{"inactive, default allow", nil, func(m *model) { m.incoming = "allow" }, []string{"ufw default deny incoming", "ufw --force enable"}},
		{"a session's port kept", []uint16{22}, nil, []string{"ufw allow 22/tcp", "ufw --force enable"}},
		{"active, set", nil, func(m *model) { m.active = true }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ufw.PlanFor(ufw.Input{Proxmox: notPVE, NoSSH: true, Keep: tc.keep, State: stateOf(tc.m)(t),
				Desired: ufw.Desired{Enable: true, Incoming: ufw.PolicyDeny}})
			if err != nil {
				t.Fatal(err)
			}
			if got := argvs(p.Steps); !slices.Equal(got, tc.want) {
				t.Errorf("steps %q, want %q", got, tc.want)
			}
			noDisableOrReset(t, p)
		})
	}
	if _, err := ufw.PlanFor(ufw.Input{Proxmox: notPVE, NoSSH: true, Listeners: stock22(), State: stateOf(nil)(t), Desired: hardening}); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("listeners with no SSH server: %v, want fs.ErrInvalid", err)
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

// TestPlanForSaysWhy: a need left blocked on a port the allow would change
// names the saved rule.
func TestPlanForSaysWhy(t *testing.T) {
	m := newModel(t)
	m.rules = []modelRule{{fam: ufw.IPv4, action: "limit", port: "22"}}
	_, err := ufw.PlanFor(ufw.Input{Proxmox: notPVE, Listeners: stock22(), State: read(t, m), Desired: hardening})
	want := `listener [::]:22 (IPv6 22) would be blocked by "default deny (incoming); no rule added: \"ufw allow 22/tcp\" would change \"### tuple ### limit tcp 22 0.0.0.0/0 any 0.0.0.0/0 in\""`
	if !errors.Is(err, ufw.ErrBlocks) || !strings.Contains(err.Error(), want) {
		t.Errorf("PlanFor: %v\nwant it to contain %s", err, want)
	}
}
