package reload_test

import (
	"errors"
	"io/fs"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// What detect reports on each platform's stock install (docs/safety/detect.md
// §3, fact × platform table).
var (
	// Ubuntu 26.04 after the first connection: ssh.service runs from the
	// socket.
	ubuntu2604 = detect.SSH{Mode: detect.SSHSocket, Socket: unit("enabled", "active"), Service: unit("disabled", "active"),
		Generator: detect.Yes, GeneratorPath: detect.SocketGenerator}
	// Ubuntu 24.04 before the first connection.
	ubuntu2404 = detect.SSH{Mode: detect.SSHSocket, Socket: unit("enabled", "active"), Service: unit("disabled", "inactive"),
		Generator: detect.Yes, GeneratorPath: detect.SocketGenerator}
	debian = detect.SSH{Mode: detect.SSHService, Socket: unit("disabled", "inactive"), Service: unit("enabled", "active"), Generator: detect.No}
	alpine = detect.SSH{Mode: detect.SSHOpenRC, OpenRC: detect.OpenRCService{Started: detect.Yes, InRunlevel: detect.Yes, Runlevels: []string{"default"}}}
)

func unit(enabled, active string) detect.UnitState {
	return detect.UnitState{Enabled: enabled, Active: active}
}

func l(addr string, port uint16) sshd.Listener {
	return sshd.Listener{Addr: netip.MustParseAddr(addr), Port: port}
}

var (
	at22   = []sshd.Listener{l("0.0.0.0", 22), l("::", 22)}
	at2230 = []sshd.Listener{l("0.0.0.0", 2230), l("::", 2230)}
	both   = slices.Concat(at22, at2230)
	none   = reload.SocketDropIns{Read: true}

	daemonReload  = "run systemctl daemon-reload"
	checkSocket   = "check-socket systemctl show -p Listen ssh.socket"
	restartSocket = "run-if-up(ssh.socket) systemctl restart ssh.socket"
	reloadService = "run-if-up(ssh.service) systemctl reload ssh.service"
	reloadOpenRC  = "run-if-up(sshd) rc-service sshd reload"
)

func port(before, after []sshd.Listener) reload.Change {
	return reload.Change{Keywords: []string{"Port"}, Before: before, After: after}
}

func steps(p reload.Plan) []string {
	var out []string
	for _, s := range p.Steps {
		k := s.Kind.String()
		if s.Unit != "" {
			k += "(" + s.Unit + ")"
		}
		out = append(out, k+" "+strings.Join(s.Argv, " "))
	}
	return out
}

func TestPlanFor(t *testing.T) {
	tests := []struct {
		name    string
		in      reload.Input
		steps   []string
		verify  []sshd.Listener
		hold    []sshd.Listener
		dropIns int
	}{
		{"ubuntu 26.04 port", reload.Input{SSH: ubuntu2604, Change: port(at22, both), DropIns: none},
			[]string{daemonReload, checkSocket, restartSocket}, both, at22, 0},
		{"ubuntu 24.04 port", reload.Input{SSH: ubuntu2404, Change: port(at22, both), DropIns: none},
			[]string{daemonReload, checkSocket, restartSocket}, both, at22, 0},
		{"ubuntu listen address, any case", reload.Input{SSH: ubuntu2604, DropIns: none, Change: reload.Change{
			Keywords: []string{"LISTENADDRESS"}, Before: at22, After: at22}},
			[]string{daemonReload, checkSocket, restartSocket}, at22, at22, 0},
		{"ubuntu other keys", reload.Input{SSH: ubuntu2604, Change: reload.Change{Keywords: []string{"PasswordAuthentication", "PermitRootLogin"}}},
			[]string{reloadService}, nil, nil, 0},
		{"ubuntu other keys, listen.conf reported", reload.Input{SSH: ubuntu2404, Change: reload.Change{Keywords: []string{"PubkeyAuthentication"}},
			DropIns: reload.SocketDropIns{Read: true, Files: []reload.SocketDropIn{{Path: "/etc/systemd/system/ssh.socket.d/listen.conf", Listen: []string{"", "2222"}}}}},
			[]string{reloadService}, nil, nil, 1},
		{"ubuntu port, drop-in without ListenStream", reload.Input{SSH: ubuntu2604, Change: port(at22, both),
			DropIns: reload.SocketDropIns{Read: true, Files: []reload.SocketDropIn{{Path: "/etc/systemd/system/ssh.socket.d/10-bind.conf"}}}},
			[]string{daemonReload, checkSocket, restartSocket}, both, at22, 1},
		{"debian 13, 12 and proxmox ve 9 port", reload.Input{SSH: debian, Change: port(at22, both)},
			[]string{reloadService}, both, at22, 0},
		{"debian other keys", reload.Input{SSH: debian, Change: reload.Change{Keywords: []string{"PermitRootLogin"}}},
			[]string{reloadService}, nil, nil, 0},
		{"alpine port", reload.Input{SSH: alpine, Change: port(at22, both)},
			[]string{reloadOpenRC}, both, at22, 0},
		{"alpine other keys", reload.Input{SSH: alpine, Change: reload.Change{Keywords: []string{"PasswordAuthentication"}}},
			[]string{reloadOpenRC}, nil, nil, 0},
		{"confirmed clean-up drops the old port", reload.Input{SSH: ubuntu2604, DropIns: none, Change: reload.Change{
			Keywords: []string{"port"}, Before: both, After: at2230, Confirmed: true}},
			[]string{daemonReload, checkSocket, restartSocket}, at2230, nil, 0},
		{"widening a ListenAddress keeps its connections", reload.Input{SSH: debian, Change: reload.Change{
			Keywords: []string{"ListenAddress"}, Before: []sshd.Listener{l("192.0.2.10", 22)}, After: []sshd.Listener{l("0.0.0.0", 22)}}},
			[]string{reloadService}, []sshd.Listener{l("0.0.0.0", 22)}, []sshd.Listener{l("192.0.2.10", 22)}, 0},
		{"nothing changed", reload.Input{SSH: detect.SSH{}}, nil, nil, nil, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := reload.PlanFor(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got := steps(p); !slices.Equal(got, tc.steps) {
				t.Errorf("steps = %q, want %q", got, tc.steps)
			}
			if !slices.Equal(p.Verify, tc.verify) || !slices.Equal(p.Hold, tc.hold) {
				t.Errorf("verify %v hold %v, want %v and %v", p.Verify, p.Hold, tc.verify, tc.hold)
			}
			if len(p.DropIns) != tc.dropIns || p.Mode != tc.in.SSH.Mode {
				t.Errorf("drop-ins %v mode %v", p.DropIns, p.Mode)
			}
		})
	}
}

func TestPlanForRefuses(t *testing.T) {
	listenConf := reload.SocketDropIns{Read: true, Files: []reload.SocketDropIn{{Path: "/etc/systemd/system/ssh.socket.d/listen.conf", Listen: []string{"", "2222"}}}}
	masked := ubuntu2604
	masked.Generator, masked.GeneratorPath = detect.No, "/etc/systemd/system-generators/sshd-socket-generator"
	unknownGen := ubuntu2604
	unknownGen.Generator = detect.Unknown
	conflict := detect.SSH{Socket: unit("enabled", "inactive"), Service: unit("enabled", "active"), Err: detect.ErrConflict}
	tests := []struct {
		name string
		in   reload.Input
		want []error
	}{
		{"mode unknown", reload.Input{SSH: conflict, Change: reload.Change{Keywords: []string{"PermitRootLogin"}}},
			[]error{reload.ErrModeUnknown, detect.ErrConflict}},
		{"mode unknown, no reason", reload.Input{Change: reload.Change{Keywords: []string{"PermitRootLogin"}}},
			[]error{reload.ErrModeUnknown, detect.ErrNoSignal}},
		{"mode unknown comes first", reload.Input{Change: port(nil, nil)}, []error{reload.ErrModeUnknown}},
		{"generator masked", reload.Input{SSH: masked, Change: port(at22, both), DropIns: none}, []error{reload.ErrNoGenerator}},
		{"generator unknown", reload.Input{SSH: unknownGen, Change: port(at22, both), DropIns: none}, []error{reload.ErrNoGenerator}},
		{"drop-ins not read", reload.Input{SSH: ubuntu2604, Change: port(at22, both)}, []error{fs.ErrInvalid}},
		{"v0.3.x listen.conf", reload.Input{SSH: ubuntu2404, Change: port(at22, both), DropIns: listenConf}, []error{reload.ErrSocketDropIn}},
		{"drops the old port", reload.Input{SSH: ubuntu2604, Change: port(at22, at2230), DropIns: none}, []error{reload.ErrDropsListener}},
		{"AddressFamily inet drops IPv6", reload.Input{SSH: debian, Change: reload.Change{
			Keywords: []string{"AddressFamily"}, Before: at22, After: at22[:1]}}, []error{reload.ErrDropsListener}},
		{"listeners not established", reload.Input{SSH: debian, Change: port(nil, both)}, []error{reload.ErrListenersUnknown}},
		{"alpine UsePAM", reload.Input{SSH: alpine, Change: reload.Change{Keywords: []string{"UsePAM"}}}, []error{reload.ErrFlavour}},
		{"alpine GSSAPIAuthentication", reload.Input{SSH: alpine, Change: reload.Change{Keywords: []string{"gssapiauthentication"}}}, []error{reload.ErrFlavour}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := reload.PlanFor(tc.in)
			for _, w := range tc.want {
				if !errors.Is(err, w) {
					t.Errorf("err = %v, want %v", err, w)
				}
			}
			if p.Steps != nil || p.Verify != nil || p.Hold != nil || len(p.DropIns) != len(tc.in.DropIns.Files) {
				t.Errorf("refused plan = %+v, want Mode and DropIns only", p)
			}
		})
	}
}

func TestHoldError(t *testing.T) {
	_, err := reload.PlanFor(reload.Input{SSH: debian, Change: port(at22, at2230)})
	var he *reload.HoldError
	if !errors.As(err, &he) || !slices.Equal(he.Missing, at22) {
		t.Fatalf("err = %v, want a *HoldError missing %v", err, at22)
	}
	if got := reload.HoldPorts(at22, []uint16{2230}); !slices.Equal(got, []uint16{22, 2230}) {
		t.Errorf("HoldPorts = %v, want [22 2230]", got)
	}
}

// TestNeverSSHDUnit: on systemd only ssh.service and ssh.socket are named;
// the sshd alias is missing on a fresh Ubuntu 24.04.
func TestNeverSSHDUnit(t *testing.T) {
	for _, s := range []detect.SSH{ubuntu2604, ubuntu2404, debian} {
		for _, c := range []reload.Change{port(at22, both), {Keywords: []string{"PermitRootLogin"}}} {
			p, err := reload.PlanFor(reload.Input{SSH: s, Change: c, DropIns: none})
			if err != nil {
				t.Fatal(err)
			}
			for _, st := range p.Steps {
				for _, a := range slices.Concat(st.Argv[1:], []string{st.Unit}) {
					if strings.HasPrefix(a, "sshd") {
						t.Errorf("%v names %q", st.Argv, a)
					}
				}
			}
		}
	}
}
