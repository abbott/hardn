package firewall

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// sockets are the SSH listeners read as SSH Port reads them, with the same
// primitives and rule in this package's own copy: the configured set
// (sshd -T's listenaddress lines, with ssh.socket's Listen entries on a
// socket-activated host), checked against the sockets ss shows bound. Both
// configured sources are configuration: between an edit and the reload
// that applies it, what accepts connections is what is bound
// (docs/engine/check-ssh.md §2.12, §6).
type sockets struct {
	mode    detect.SSHMode
	conf    sshd.Listeners
	read    bool            // conf was read
	bound   []reload.Bound  // what ss shows; nil when not read
	bad     []string        // LISTEN lines ss printed that were not read
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
// rule reload.CheckInEffect uses). Without ss they are not established on
// any host: the configured set is configuration, and whether SSH gets
// through is this row's claim (docs/engine/check-firewall.md §2.3), where
// SSH Port lets the configured set stand off a socket-activated host.
func readSockets(ctx context.Context, sys host.System, f detect.Facts) sockets {
	s := sockets{mode: f.SSH.Mode}
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
	if err != nil {
		s.why, s.err = sshdWhy(err), fmt.Errorf("sshd's listeners: %w", err)
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
	case errors.Is(err, host.ErrNotFound):
		s.ev = append(s.ev, "ss: not installed, so the bound sockets are not read")
		s.why, s.err = "ss not installed", err
	case err != nil:
		s.why, s.err = "can't tell ssh ports", err
	default:
		s.bound, s.bad, s.unbound = bound, bad, reload.Unbound(s.conf.All, bound)
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

// sshdWhy is what a failed sshd -T shows of the host, in the SSH rows'
// words where they have one (ssh's failed): no sshd, no sshd_config, a
// configuration sshd refuses; "can't tell ssh ports" otherwise.
func sshdWhy(err error) string {
	var pe *fs.PathError
	switch {
	case errors.Is(err, host.ErrNotFound):
		return "sshd not installed"
	case errors.As(err, &pe) && pe.Path == sshd.MainConfig && errors.Is(err, fs.ErrNotExist):
		return "no sshd_config"
	case errors.Is(err, sshd.ErrConfig):
		return "sshd config invalid"
	}
	return "can't tell ssh ports"
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
// show bound (ssh.CodeNotInEffect), in SSH Port's words, nil for none.
func (s sockets) notInEffect() []engine.Finding {
	if len(s.unbound) == 0 {
		return nil
	}
	why := "an edit not reloaded"
	if s.mode == detect.SSHSocket {
		why = "ssh.socket not restarted after daemon-reload"
	}
	d := listeners(s.unbound) + " configured, not bound: not in effect (" + why + ")"
	if len(s.bad) > 0 {
		d += fmt.Sprintf("; ss lines not read: %q", s.bad)
	}
	return []engine.Finding{{Code: ssh.CodeNotInEffect, Detail: d, Source: "ss -Htln"}}
}

// bound22 is SSH Port's finding of a stray socket on 22 (ssh.Code22Bound),
// in SSH Port's words.
func (s sockets) bound22() engine.Finding {
	return engine.Finding{Code: ssh.Code22Bound, Source: "ss -Htln",
		Detail: s.on([]uint16{22}) + " bound, not configured: sshd not reloaded since 22 was dropped, or another program on 22"}
}

// unchecked adds CodeSSHUnchecked's finding to out, beside a state that
// does not depend on the listeners: why they are not established, and the
// error of a read that failed in the evidence.
func (s sockets) unchecked(out engine.Status) engine.Status {
	out.Findings = append(out.Findings, engine.Finding{Code: CodeSSHUnchecked, Detail: s.why, Source: s.source()})
	if s.err != nil {
		out.Evidence = append(out.Evidence, s.source()+": "+s.err.Error())
	}
	return out
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
