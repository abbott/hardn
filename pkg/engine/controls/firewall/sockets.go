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

// Sockets are the SSH listeners read as SSH Port reads them, with the same
// primitives and rule: the configured set (sshd -T's listenaddress lines,
// with ssh.socket's Listen entries on a socket-activated host), checked
// against the sockets ss shows bound. Both configured sources are
// configuration: between an edit and the reload that applies it, what accepts
// connections is what is bound (docs/engine/check-ssh.md §2.12, §6). Enable's
// refusals and the Firewall screen's SSH rows read them too, this one reading
// (decision 2).
type Sockets struct {
	mode     detect.SSHMode
	Conf     sshd.Listeners
	Read     bool            // Conf was read
	Bound    []reload.Bound  // what ss shows; nil when not read
	bad      []string        // LISTEN lines ss printed that were not read
	Unbound  []sshd.Listener // configured, not bound
	NoSS     bool            // ss is not installed
	SSFailed bool            // ss ran and failed
	// NoServer: sshd's read failed for want of sshd (sshd.NoSSHD), the
	// host is established to have no SSH server at all (sshd.NoServer), and
	// ss shows nothing on 22 (noServer): there is no SSH listener to let
	// through. Why still says what the read showed; the check and Enable
	// read NoServer beside Established.
	NoServer bool
	ev       []string
	// Why is the detail of a row whose state depends on the listeners
	// when they are not established; "" when they are. err is the read
	// that failed, if one did.
	Why string
	err error
}

// ReadSockets reads the SSH listeners: on OpenRC the binary Alpine's init
// script runs (reload.OpenRCFlavour), sshd.ReadEffective, then ss -Htln
// (reload.ReadBound). They are established when the configured set is
// (Listeners.Known) and every listener of it is bound (reload.Unbound, the
// rule reload.CheckInEffect uses). Without ss they are not established on
// any host: the configured set is configuration, and whether SSH gets
// through is this row's claim (docs/engine/check-firewall.md §2.3), where
// SSH Port lets the configured set stand off a socket-activated host.
func ReadSockets(ctx context.Context, sys host.System, f detect.Facts) Sockets {
	s := Sockets{mode: f.SSH.Mode}
	var bin string
	if f.Init.Kind == detect.OpenRC {
		b, err := reload.OpenRCFlavour(ctx, sys)
		if err != nil {
			s.Why, s.err = "can't tell ssh ports", err
			return s
		}
		bin = b
	}
	if f.SSH.SSHDUnits {
		if err := reload.CheckSysconfig(ctx, sys); err != nil {
			s.Why, s.err = "can't tell ssh ports", err
			return s
		}
	}
	eff, _, err := sshd.ReadEffective(ctx, sys, sshd.EffectiveRequest{SSH: f.SSH, Binary: bin})
	if err != nil {
		s.Why, s.err = sshdWhy(err), fmt.Errorf("sshd's listeners: %w", err)
		if sshd.NoSSHD(err) {
			if ev, ok := sshd.NoServer(ctx, sys, f); ok {
				s.noServer(ctx, sys, ev)
			}
		}
		return s
	}
	s.Conf, s.Read = eff.Listeners, true
	for _, v := range eff.Keywords["listenaddress"] {
		s.ev = append(s.ev, "sshd -T: listenaddress "+v)
	}
	if s.Conf.SocketRead {
		s.ev = append(s.ev, "systemctl show -p Listen "+sshd.SocketUnit+": "+listeners(s.Conf.Socket))
	}
	bound, bad, err := reload.ReadBound(ctx, sys)
	switch {
	case errors.Is(err, host.ErrNotFound):
		s.ev = append(s.ev, "ss: not installed, so the bound sockets are not read")
		s.NoSS, s.Why, s.err = true, "ss not installed", err
	case err != nil:
		s.SSFailed, s.Why, s.err = true, "can't tell ssh ports", err
	default:
		s.Bound, s.bad, s.Unbound = bound, bad, reload.Unbound(s.Conf.All, bound)
		s.ev = append(s.ev, "ss -Htln: "+s.on(s.Ports()))
		if len(bad) > 0 {
			s.ev = append(s.ev, fmt.Sprintf("ss -Htln: lines not read: %q", bad))
		}
	}
	switch {
	case s.Why != "":
	case !s.Conf.Known || len(s.Conf.All) == 0:
		s.Why = "can't tell ssh ports"
	case len(s.Unbound) > 0:
		s.Why = "ssh config not in effect"
	}
	return s
}

// noServer is ReadSockets past sshd.NoServer: no SSH server stands only
// where ss shows nothing bound on 22, every line read, so that a server no
// package installed and no searched path holds, listening on SSH's port,
// is not taken for none. ss missing or failing is NoSS or SSFailed, as on
// any host, so Enable is refused in their words; a socket on 22 leaves
// Why as sshd's read had it.
func (s *Sockets) noServer(ctx context.Context, sys host.System, ev []string) {
	bound, bad, err := reload.ReadBound(ctx, sys)
	switch {
	case errors.Is(err, host.ErrNotFound):
		s.ev = append(s.ev, "ss: not installed, so the bound sockets are not read")
		s.NoSS, s.Why, s.err = true, "ss not installed", err
		return
	case err != nil:
		s.SSFailed, s.Why, s.err = true, "can't tell ssh ports", err
		return
	case len(bad) > 0:
		s.ev = append(s.ev, fmt.Sprintf("ss -Htln: lines not read: %q", bad))
		return
	case slices.ContainsFunc(bound, func(b reload.Bound) bool { return b.Port == 22 }):
		s.ev = append(s.ev, "ss -Htln: a socket on 22, though no SSH server is installed")
		return
	}
	s.Bound = bound
	s.NoServer, s.ev = true, append(append(s.ev, ev...), "ss -Htln: nothing on 22")
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

// Established: the SSH listeners are what accepts SSH connections.
func (s Sockets) Established() bool { return s.Why == "" }

// Ports are the ports sshd's sockets may be on: the configured ones and
// 22, ascending.
func (s Sockets) Ports() []uint16 {
	ps := s.Conf.Ports()
	if !slices.Contains(ps, 22) {
		ps = append(ps, 22)
		slices.Sort(ps)
	}
	return ps
}

// NoneBound: sshd's configuration is not in effect and nothing is bound on any
// port it may be on, so what accepts SSH is not read: ss -Htln says no socket's
// program, so another port's cannot be taken for sshd's.
func (s Sockets) NoneBound() bool {
	return len(s.Unbound) > 0 && !slices.ContainsFunc(s.Bound, func(b reload.Bound) bool { return slices.Contains(s.Ports(), b.Port) })
}

// Listeners are what the firewall must let through: the configured set,
// and the bound sockets on Ports() that may be the ones sshd accepts on.
// Established, those are the sockets on 22 while no configured listener
// is (Stray). Otherwise every one: the configuration not in effect is the
// running sshd's old ports (22, as a rule) bound and the new ones not yet.
// A dual-stack socket (ss's "*") counts as ssh.socket's, which ufw.Needs
// reads as both families. ok is false when sshd's listeners were not read.
func (s Sockets) Listeners() (ls sshd.Listeners, ok bool) {
	if !s.Read {
		return ls, false
	}
	ls = sshd.Listeners{SSHD: slices.Clone(s.Conf.SSHD), Socket: slices.Clone(s.Conf.Socket), All: slices.Clone(s.Conf.All)}
	for _, b := range s.Bound {
		if !slices.Contains(s.Ports(), b.Port) || s.Established() && !s.Stray(b.Port) {
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

// Stray reports whether a socket bound on port is a stray: on 22 while the
// configured set is established and has no listener on 22. It is sshd's,
// not reloaded since 22 was dropped (it then accepts on the configured
// ports too), or another program's; ss does not say whose (SSH Port's "22
// still open", check-ssh.md §2.12).
func (s Sockets) Stray(port uint16) bool {
	return s.Established() && port == 22 && !slices.Contains(s.Conf.Ports(), 22)
}

// on lists the bound sockets on ports.
func (s Sockets) on(ports []uint16) string {
	var out []string
	for _, b := range s.Bound {
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
func (s Sockets) notInEffect() []engine.Finding {
	if len(s.Unbound) == 0 {
		return nil
	}
	why := "an edit not reloaded"
	if s.mode == detect.SSHSocket {
		why = "ssh.socket not restarted after daemon-reload"
	}
	d := listeners(s.Unbound) + " configured, not bound: not in effect (" + why + ")"
	if len(s.bad) > 0 {
		d += fmt.Sprintf("; ss lines not read: %q", s.bad)
	}
	return []engine.Finding{{Code: ssh.CodeNotInEffect, Detail: d, Source: "ss -Htln"}}
}

// bound22 is SSH Port's finding of a stray socket on 22 (ssh.Code22Bound),
// in SSH Port's words.
func (s Sockets) bound22() engine.Finding {
	return engine.Finding{Code: ssh.Code22Bound, Source: "ss -Htln",
		Detail: s.on([]uint16{22}) + " bound, not configured: sshd not reloaded since 22 was dropped, or another program on 22"}
}

// unchecked adds CodeSSHUnchecked's finding to out, beside a state that
// does not depend on the listeners: why they are not established, and the
// error of a read that failed in the evidence.
func (s Sockets) unchecked(out engine.Status) engine.Status {
	out.Findings = append(out.Findings, engine.Finding{Code: CodeSSHUnchecked, Detail: s.Why, Source: s.source()})
	if s.err != nil {
		out.Evidence = append(out.Evidence, s.source()+": "+s.err.Error())
	}
	return out
}

// source is the command the listeners' reading stopped at.
func (s Sockets) source() string {
	if !s.Read || !s.Conf.Known {
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
