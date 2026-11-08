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

	daemonReload    = "run systemctl daemon-reload"
	checkSocket     = "check-socket systemctl show -p Listen ssh.socket"
	restartSocket   = "run-if-up(ssh.socket) systemctl restart ssh.socket"
	reloadService   = "run-if-up(ssh.service) systemctl reload ssh.service"
	reloadActivated = "run-if-up(ssh.service activated) systemctl reload ssh.service"
	reloadOpenRC    = "run-if-up(sshd) rc-service sshd reload"
)

// known is a listener set sshd.ReadEffective established.
func known(ls []sshd.Listener) sshd.Listeners { return sshd.Listeners{All: ls, Known: true} }

func port(before, after []sshd.Listener) reload.Change {
	return reload.Change{Keywords: []string{"Port"}, Before: known(before), After: after}
}

// rollback restores after, as p2-revert plans it.
func rollback(after []sshd.Listener) reload.Change {
	return reload.Change{Keywords: []string{"Port"}, After: after, Rollback: true}
}

// restoring is rollback(after) for a rollback that put back the drop-ins
// named, as one of a listen.conf migration does.
func restoring(after []sshd.Listener, dropIns ...string) reload.Change {
	c := rollback(after)
	c.RestoredDropIns = dropIns
	return c
}

// listenConfPath is v0.3.x's drop-in; listenConf2230 is it, as
// ReadSocketDropIns reads the file v0.3.2 writes for port 2230.
const listenConfPath = "/etc/systemd/system/ssh.socket.d/listen.conf"

var listenConf2230 = dropInOf("", "2230")

// dropInOf is listen.conf with these ListenStream values.
func dropInOf(listen ...string) reload.SocketDropIns {
	return reload.SocketDropIns{Read: true, Files: []reload.SocketDropIn{{Path: listenConfPath, Listen: listen}}}
}

func steps(p reload.Plan) []string {
	var out []string
	for _, s := range p.Steps {
		k := s.Kind.String()
		switch {
		case s.Activated:
			k += "(" + s.Unit + " activated)"
		case s.Unit != "":
			k += "(" + s.Unit + ")"
		}
		k += " " + strings.Join(s.Argv, " ")
		if len(s.Start) > 0 {
			k += " else " + strings.Join(s.Start, " ")
		}
		out = append(out, k)
	}
	return out
}

func TestPlanFor(t *testing.T) {
	tests := []struct {
		name               string
		in                 reload.Input
		steps              []string
		verify, hold, adds []sshd.Listener
		dropIns            int
	}{
		{"ubuntu 26.04 port", reload.Input{SSH: ubuntu2604, Change: port(at22, both), DropIns: none},
			[]string{daemonReload, checkSocket, restartSocket}, both, at22, at2230, 0},
		{"ubuntu 24.04 port", reload.Input{SSH: ubuntu2404, Change: port(at22, both), DropIns: none},
			[]string{daemonReload, checkSocket, restartSocket}, both, at22, at2230, 0},
		{"ubuntu listen address, any case", reload.Input{SSH: ubuntu2604, DropIns: none, Change: reload.Change{
			Keywords: []string{"LISTENADDRESS"}, Before: known(at22), After: at22}},
			[]string{daemonReload, checkSocket, restartSocket}, at22, at22, nil, 0},
		{"ubuntu other keys", reload.Input{SSH: ubuntu2604, Change: reload.Change{Keywords: []string{"PasswordAuthentication", "PermitRootLogin"}}},
			[]string{reloadActivated}, nil, nil, nil, 0},
		{"ubuntu other keys, listen.conf reported", reload.Input{SSH: ubuntu2404, Change: reload.Change{Keywords: []string{"PubkeyAuthentication"}},
			DropIns: reload.SocketDropIns{Read: true, Files: []reload.SocketDropIn{{Path: "/etc/systemd/system/ssh.socket.d/listen.conf", Listen: []string{"", "2222"}}}}},
			[]string{reloadActivated}, nil, nil, nil, 1},
		{"ubuntu port, drop-in without ListenStream", reload.Input{SSH: ubuntu2604, Change: port(at22, both),
			DropIns: reload.SocketDropIns{Read: true, Files: []reload.SocketDropIn{{Path: "/etc/systemd/system/ssh.socket.d/10-bind.conf"}}}},
			[]string{daemonReload, checkSocket, restartSocket}, both, at22, at2230, 1},
		{"debian 13, 12 and proxmox ve 9 port", reload.Input{SSH: debian, Change: port(at22, both)},
			[]string{reloadService}, both, at22, at2230, 0},
		{"debian other keys", reload.Input{SSH: debian, Change: reload.Change{Keywords: []string{"PermitRootLogin"}}},
			[]string{reloadService}, nil, nil, nil, 0},
		{"alpine port", reload.Input{SSH: alpine, Change: port(at22, both)},
			[]string{reloadOpenRC}, both, at22, at2230, 0},
		{"alpine other keys", reload.Input{SSH: alpine, Change: reload.Change{Keywords: []string{"PasswordAuthentication"}}},
			[]string{reloadOpenRC}, nil, nil, nil, 0},
		{"confirmed clean-up drops the old port", reload.Input{SSH: ubuntu2604, DropIns: none, Change: reload.Change{
			Keywords: []string{"port"}, Before: known(both), After: at2230, Confirmed: true}},
			[]string{daemonReload, checkSocket, restartSocket}, at2230, nil, nil, 0},
		{"widening a ListenAddress keeps its connections", reload.Input{SSH: debian, Change: reload.Change{
			Keywords: []string{"ListenAddress"}, Before: known([]sshd.Listener{l("192.0.2.10", 22)}), After: []sshd.Listener{l("0.0.0.0", 22)}}},
			[]string{reloadService}, []sshd.Listener{l("0.0.0.0", 22)}, []sshd.Listener{l("192.0.2.10", 22)}, []sshd.Listener{l("0.0.0.0", 22)}, 0},
		{"ubuntu rollback restarts the socket whatever its state", reload.Input{SSH: ubuntu2604, Change: rollback(at22), DropIns: none},
			[]string{daemonReload, checkSocket, restartSocket + " else systemctl restart ssh.socket"}, at22, nil, nil, 0},
		{"ubuntu rollback of other keys: the socket starts sshd", reload.Input{SSH: ubuntu2604, Change: reload.Change{Keywords: []string{"PermitRootLogin"}, Rollback: true}},
			[]string{reloadActivated}, nil, nil, nil, 0},
		{"debian rollback starts ssh.service", reload.Input{SSH: debian, Change: rollback(at22)},
			[]string{reloadService + " else systemctl start ssh.service"}, at22, nil, nil, 0},
		{"alpine rollback starts sshd", reload.Input{SSH: alpine, Change: reload.Change{Keywords: []string{"PermitRootLogin"}, Rollback: true}},
			[]string{reloadOpenRC + " else rc-service sshd start"}, nil, nil, nil, 0},
		{"nothing changed", reload.Input{SSH: detect.SSH{}}, nil, nil, nil, nil, 0},
		// An openssh upgrade: no keyword, the binaries replaced; the reload
		// makes the listener run the new sshd.
		{"alpine binary", reload.Input{SSH: alpine, Change: reload.Change{Binary: true}},
			[]string{reloadOpenRC}, nil, nil, nil, 0},
		{"alpine binary rollback starts sshd", reload.Input{SSH: alpine, Change: reload.Change{Binary: true, Rollback: true}},
			[]string{reloadOpenRC + " else rc-service sshd start"}, nil, nil, nil, 0},
		{"debian binary", reload.Input{SSH: debian, Change: reload.Change{Binary: true}},
			[]string{reloadService}, nil, nil, nil, 0},
		{"ubuntu binary", reload.Input{SSH: ubuntu2604, Change: reload.Change{Binary: true}},
			[]string{reloadActivated}, nil, nil, nil, 0},
		// The rollback of a listen.conf migration restored listen.conf, whose
		// ListenStream lines give the socket its listeners again; After is the
		// socket's listeners before the migration.
		{"ubuntu rollback restoring listen.conf", reload.Input{SSH: ubuntu2404, Change: restoring([]sshd.Listener{l("::", 2230)}, listenConfPath),
			DropIns: listenConf2230}, []string{daemonReload, checkSocket, restartSocket + " else systemctl restart ssh.socket"},
			[]sshd.Listener{l("::", 2230)}, nil, nil, 1},
		// A port alone is 0.0.0.0 where IPv6 is not supported; addresses
		// given are matched as they are, one for one.
		{"rollback restoring listen.conf, IPv4 only", reload.Input{SSH: ubuntu2404, Change: restoring([]sshd.Listener{l("0.0.0.0", 2230)}, listenConfPath),
			DropIns: listenConf2230}, []string{daemonReload, checkSocket, restartSocket + " else systemctl restart ssh.socket"},
			[]sshd.Listener{l("0.0.0.0", 2230)}, nil, nil, 1},
		{"rollback restoring a drop-in of addresses", reload.Input{SSH: ubuntu2404, Change: restoring(at2230, listenConfPath),
			DropIns: dropInOf("", "[::]:2230", "", "0.0.0.0:2230", "[::]:2230")}, []string{daemonReload, checkSocket, restartSocket + " else systemctl restart ssh.socket"},
			at2230, nil, nil, 1},
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
			if !slices.Equal(p.Verify, tc.verify) || !slices.Equal(p.Hold, tc.hold) || !slices.Equal(p.Adds, tc.adds) {
				t.Errorf("verify %v hold %v adds %v, want %v, %v and %v", p.Verify, p.Hold, p.Adds, tc.verify, tc.hold, tc.adds)
			}
			if len(p.DropIns) != tc.dropIns || p.Mode != tc.in.SSH.Mode {
				t.Errorf("drop-ins %v mode %v", p.DropIns, p.Mode)
			}
		})
	}
}

func TestPlanForRefuses(t *testing.T) {
	listenConf := reload.SocketDropIns{Read: true, Files: []reload.SocketDropIn{{Path: listenConfPath, Listen: []string{"", "2222"}}}}
	extra := reload.SocketDropIns{Read: true, Files: append(slices.Clone(listenConf2230.Files),
		reload.SocketDropIn{Path: "/etc/systemd/system/ssh.socket.d/port.conf", Listen: []string{"2231"}})}
	generators := reload.SocketDropIns{Read: true, Files: []reload.SocketDropIn{{Path: "/etc/systemd/system/ssh.socket.d/addresses.conf", Listen: []string{"", "2230"}}}}
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
		{"binary with keywords", reload.Input{SSH: alpine, Change: reload.Change{Binary: true, Keywords: []string{"PermitRootLogin"}}}, []error{fs.ErrInvalid}},
		{"binary confirmed", reload.Input{SSH: alpine, Change: reload.Change{Binary: true, Confirmed: true}}, []error{fs.ErrInvalid}},
		{"binary, mode unknown", reload.Input{Change: reload.Change{Binary: true}}, []error{reload.ErrModeUnknown}},
		{"generator masked", reload.Input{SSH: masked, Change: port(at22, both), DropIns: none}, []error{reload.ErrNoGenerator}},
		{"generator unknown", reload.Input{SSH: unknownGen, Change: port(at22, both), DropIns: none}, []error{reload.ErrNoGenerator}},
		{"drop-ins not read", reload.Input{SSH: ubuntu2604, Change: port(at22, both)}, []error{fs.ErrInvalid}},
		{"v0.3.x listen.conf", reload.Input{SSH: ubuntu2404, Change: port(at22, both), DropIns: listenConf}, []error{reload.ErrSocketDropIn}},
		{"drops the old port", reload.Input{SSH: ubuntu2604, Change: port(at22, at2230), DropIns: none}, []error{reload.ErrDropsListener}},
		{"AddressFamily inet drops IPv6", reload.Input{SSH: debian, Change: reload.Change{
			Keywords: []string{"AddressFamily"}, Before: known(at22), After: at22[:1]}}, []error{reload.ErrDropsListener}},
		{"listeners not established", reload.Input{SSH: debian, Change: port(nil, both)}, []error{reload.ErrListenersUnknown}},
		{"before read but not known", reload.Input{SSH: debian, Change: reload.Change{Keywords: []string{"Port"},
			Before: sshd.Listeners{All: at22}, After: both}}, []error{reload.ErrListenersUnknown}},
		{"rollback with no listeners to restore", reload.Input{SSH: debian, Change: rollback(nil)}, []error{reload.ErrListenersUnknown}},
		{"confirmed adds a listener", reload.Input{SSH: debian, Change: reload.Change{Keywords: []string{"Port"},
			Before: known(at22), After: at2230, Confirmed: true}}, []error{reload.ErrAddsListener}},
		{"confirmed and rollback", reload.Input{SSH: debian, Change: reload.Change{Keywords: []string{"PermitRootLogin"},
			Confirmed: true, Rollback: true}}, []error{fs.ErrInvalid}},
		{"the generator's addresses.conf masked", reload.Input{SSH: ubuntu2604, Change: port(at22, both), DropIns: reload.SocketDropIns{Read: true,
			Files: []reload.SocketDropIn{{Path: "/etc/systemd/system/ssh.socket.d/addresses.conf", Masked: true}}}}, []error{reload.ErrSocketDropIn}},
		// A rollback that does not say it restored listen.conf is refused
		// as before (next/p2-vmtest's probe), and so is a forward change
		// naming restored drop-ins, a drop-in the rollback did not restore,
		// and one named as the generator's.
		{"rollback with listen.conf it did not restore", reload.Input{SSH: ubuntu2404, Change: rollback([]sshd.Listener{l("::", 2230)}), DropIns: listenConf2230},
			[]error{reload.ErrSocketDropIn}},
		{"forward change naming restored drop-ins", reload.Input{SSH: ubuntu2404, Change: reload.Change{Keywords: []string{"Port"}, Before: known(at22), After: both,
			RestoredDropIns: []string{listenConfPath}}, DropIns: listenConf2230}, []error{fs.ErrInvalid}},
		{"rollback, another drop-in with ListenStream", reload.Input{SSH: ubuntu2404, Change: restoring(at2230, listenConfPath), DropIns: extra},
			[]error{reload.ErrSocketDropIn}},
		{"rollback restoring one named as the generator's", reload.Input{SSH: ubuntu2404,
			Change: restoring(at2230, "/etc/systemd/system/ssh.socket.d/addresses.conf"), DropIns: generators}, []error{reload.ErrSocketDropIn}},
		{"rollback restoring listen.conf, generator masked", reload.Input{SSH: masked, Change: restoring(at2230, listenConfPath), DropIns: listenConf2230},
			[]error{reload.ErrNoGenerator}},
		// A rollback whose restored drop-ins would not give the socket
		// exactly After is refused before daemon-reload
		// (next/p2-revert-plans): listen.conf never in effect (D9), and a
		// socket configured for more than listen.conf gives.
		{"rollback restoring listen.conf not in effect (D9)", reload.Input{SSH: ubuntu2404, Change: restoring(at22, listenConfPath), DropIns: listenConf2230},
			[]error{reload.ErrRestoredListeners}},
		{"rollback restoring listen.conf, the socket on more", reload.Input{SSH: ubuntu2404, Change: restoring(both, listenConfPath), DropIns: listenConf2230},
			[]error{reload.ErrRestoredListeners}},
		{"rollback restoring listen.conf, a listener of it not the socket's", reload.Input{SSH: ubuntu2404, Change: restoring(at2230, listenConfPath),
			DropIns: dropInOf("", "2230", "2231")}, []error{reload.ErrRestoredListeners}},
		{"rollback, the restored drop-in not there", reload.Input{SSH: ubuntu2404, Change: restoring(at2230, listenConfPath), DropIns: none},
			[]error{reload.ErrRestoredListeners}},
		{"rollback, the restored drop-in masked", reload.Input{SSH: ubuntu2404, Change: restoring(at2230, listenConfPath),
			DropIns: reload.SocketDropIns{Read: true, Files: []reload.SocketDropIn{{Path: listenConfPath, Masked: true}}}}, []error{reload.ErrRestoredListeners}},
		{"rollback, the restored drop-in without a reset", reload.Input{SSH: ubuntu2404, Change: restoring([]sshd.Listener{l("::", 2230)}, listenConfPath),
			DropIns: dropInOf("2230")}, []error{reload.ErrRestoredListeners}},
		{"rollback, a ListenStream path", reload.Input{SSH: ubuntu2404, Change: restoring([]sshd.Listener{l("::", 2230)}, listenConfPath),
			DropIns: dropInOf("", "/run/sshd.sock")}, []error{reload.ErrRestoredListeners}},
		{"rollback, the restored drop-in read before the generator's", reload.Input{SSH: ubuntu2404,
			Change:  restoring([]sshd.Listener{l("::", 2230)}, "/etc/systemd/system/ssh.socket.d/00-listen.conf"),
			DropIns: reload.SocketDropIns{Read: true, Files: []reload.SocketDropIn{{Path: "/etc/systemd/system/ssh.socket.d/00-listen.conf", Listen: []string{"", "2230"}}}}},
			[]error{reload.ErrRestoredListeners}},
		{"rollback, a drop-in of the restored one's name elsewhere", reload.Input{SSH: ubuntu2404, Change: restoring([]sshd.Listener{l("::", 2230)}, listenConfPath),
			DropIns: reload.SocketDropIns{Read: true, Files: append(slices.Clone(listenConf2230.Files),
				reload.SocketDropIn{Path: "/etc/systemd/system.control/ssh.socket.d/listen.conf"})}}, []error{reload.ErrRestoredListeners}},
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
			if p.Steps != nil || p.Verify != nil || p.Hold != nil || p.Adds != nil || len(p.DropIns) != len(tc.in.DropIns.Files) {
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
	if want := "reload: [0.0.0.0:22 [::]:22]: write the old Port lines beside the new ones until the change is confirmed: " + reload.ErrDropsListener.Error(); err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
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
