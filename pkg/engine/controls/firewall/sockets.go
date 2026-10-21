package firewall

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// sockets are the SSH listeners as SSH Port reads them: the configured set
// (sshd -T's listenaddress lines, with ssh.socket's Listen entries on a
// socket-activated host), checked against the sockets ss shows bound. Both
// configured sources are configuration: between an edit and the reload
// that applies it, what accepts connections is what is bound
// (docs/engine/check-ssh.md §2.12, §6).
type sockets struct {
	conf    sshd.Listeners
	read    bool            // conf was read
	bound   []reload.Bound  // what ss shows; nil when not read
	unbound []sshd.Listener // configured, not bound
	ev      []string
	// why is the detail of a row whose state depends on the listeners
	// when they are not established; "" when they are. err is the read
	// that failed, if one did.
	why string
	err error
}

// readSockets reads the SSH listeners: on OpenRC the binary Alpine's init
// script runs (reload.OpenRCFlavour), sshd.ReadEffective, then ss -Htln
// (reload.ReadBound). They are established when the configured set is
// (Listeners.Known) and every listener of it is bound (reload.Unbound, the
// rule reload.CheckInEffect uses). Without ss, off a socket-activated host
// the configured set stands, and the evidence says so; on one, sshd -T and
// ssh.socket agree after a daemon-reload whatever is bound, so the set is
// not established (as SSH Port, check-ssh.md §2.12).
func readSockets(ctx context.Context, sys host.System, f detect.Facts) sockets {
	var s sockets
	var bin string
	if f.Init.Kind == detect.OpenRC {
		b, err := reload.OpenRCFlavour(ctx, sys)
		if err != nil {
			s.why, s.err = "can't tell ssh ports", err
			return s
		}
		bin = b
	}
	eff, _, err := sshd.ReadEffective(ctx, sys, sshd.EffectiveRequest{SSH: f.SSH, Binary: bin})
	switch {
	case errors.Is(err, host.ErrNotFound):
		s.why, s.err = "sshd not installed", fmt.Errorf("sshd's listeners: %w", err)
		return s
	case err != nil:
		s.why, s.err = "can't tell ssh ports", fmt.Errorf("sshd's listeners: %w", err)
		return s
	}
	s.conf, s.read = eff.Listeners, true
	for _, v := range eff.Keywords["listenaddress"] {
		s.ev = append(s.ev, "sshd -T: listenaddress "+v)
	}
	if s.conf.SocketRead {
		s.ev = append(s.ev, "systemctl show -p Listen "+sshd.SocketUnit+": "+listeners(s.conf.Socket))
	}
	bound, bad, err := reload.ReadBound(ctx, sys)
	switch {
	case errors.Is(err, host.ErrNotFound) && f.SSH.Mode != detect.SSHSocket:
		s.ev = append(s.ev, "ss: not installed, so the bound sockets are not read")
	case errors.Is(err, host.ErrNotFound):
		s.why, s.err = "ss not installed", err
	case err != nil:
		s.why, s.err = "can't tell ssh ports", err
	default:
		s.bound, s.unbound = bound, reload.Unbound(s.conf.All, bound)
		s.ev = append(s.ev, "ss -Htln: "+s.on(s.ports()))
		if len(bad) > 0 {
			s.ev = append(s.ev, fmt.Sprintf("ss -Htln: lines not read: %q", bad))
		}
	}
	switch {
	case s.why != "":
	case !s.conf.Known || len(s.conf.All) == 0:
		s.why = "can't tell ssh ports"
	case len(s.unbound) > 0:
		s.why = "ssh config not in effect"
	}
	return s
}

// established: the SSH listeners are what accepts SSH connections.
func (s sockets) established() bool { return s.why == "" }

// ports are the ports sshd's sockets may be on: the configured ones and
// 22, ascending.
func (s sockets) ports() []uint16 {
	ps := s.conf.Ports()
	if !slices.Contains(ps, 22) {
		ps = append(ps, 22)
		slices.Sort(ps)
	}
	return ps
}

// listeners are what the firewall must let through: the configured set,
// and the bound sockets on ports() that may be the ones sshd accepts on.
// Established, those are the sockets on 22 while no configured listener
// is (stray). Otherwise every one: the configuration not in effect is the
// running sshd's old ports (22, as a rule) bound and the new ones not yet.
// A dual-stack socket (ss's "*") counts as ssh.socket's, which ufw.Needs
// reads as both families. ok is false when sshd's listeners were not read.
func (s sockets) listeners() (ls sshd.Listeners, ok bool) {
	if !s.read {
		return ls, false
	}
	ls = sshd.Listeners{SSHD: slices.Clone(s.conf.SSHD), Socket: slices.Clone(s.conf.Socket), All: slices.Clone(s.conf.All)}
	for _, b := range s.bound {
		if !slices.Contains(s.ports(), b.Port) || s.established() && !s.stray(b.Port) {
			continue
		}
		l := sshd.Listener{Addr: b.Addr, Port: b.Port}
		if b.Dual {
			ls.Socket = append(ls.Socket, l)
		} else {
			ls.SSHD = append(ls.SSHD, l)
		}
		ls.All = append(ls.All, l)
	}
	return ls, true
}

// stray reports whether a socket bound on port is a stray: on 22 while the
// configured set is established and has no listener on 22. It is sshd's,
// not reloaded since 22 was dropped (it then accepts on the configured
// ports too), or another program's; ss does not say whose (SSH Port's "22
// still open", check-ssh.md §2.12).
func (s sockets) stray(port uint16) bool {
	return s.established() && port == 22 && !slices.Contains(s.conf.Ports(), 22)
}

// on lists the bound sockets on ports.
func (s sockets) on(ports []uint16) string {
	var out []string
	for _, b := range s.bound {
		if slices.Contains(ports, b.Port) {
			out = append(out, b.String())
		}
	}
	if len(out) == 0 {
		return "none on " + portList(ports)
	}
	return strings.Join(out, " ")
}

// notInEffect is SSH Port's finding of the configured listeners ss does not
// show bound (ssh.CodeNotInEffect), nil for none.
func (s sockets) notInEffect() []engine.Finding {
	if len(s.unbound) == 0 {
		return nil
	}
	return []engine.Finding{{Code: ssh.CodeNotInEffect, Detail: listeners(s.unbound) + " configured, not bound", Source: "ss -Htln"}}
}

// source is the command the listeners' reading stopped at.
func (s sockets) source() string {
	if !s.read || !s.conf.Known {
		return "sshd -T"
	}
	return "ss -Htln"
}

func listeners(ls []sshd.Listener) string {
	if len(ls) == 0 {
		return "none"
	}
	s := make([]string, len(ls))
	for i, l := range ls {
		s[i] = l.String()
	}
	return strings.Join(s, " ")
}
