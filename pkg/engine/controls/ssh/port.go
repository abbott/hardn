package ssh

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// Port is SSH Port: every SSH listener on a port other than 22. The
// listeners are sshd -T's, with ssh.socket's on a socket-activated host
// (sshd.Listeners.All), and must be bound. On Proxmox VE the baseline is
// port 22 alone, which cluster SSH and pve-firewall's management rule use
// (decision §3.18).
type Port struct{ engine.CheckOnly }

// ID is "ssh.port".
func (Port) ID() string { return "ssh.port" }

// Title is Figma's row label.
func (Port) Title() string { return "SSH Port" }

// Applies is true everywhere, as Login's.
func (Port) Applies(detect.Facts) (bool, string) { return true, "" }

// Check reads the configured listener set (sshd.ReadEffective), which must
// be established (Listeners.Known), and the bound sockets (ss -Htln): a
// configured listener that is not bound, or off Proxmox VE a socket bound
// on 22 that no configured listener is on, is Unknown, since the
// configuration is then not what accepts connections. Without ss: Unknown
// on a socket-activated host, where sshd -T and ssh.socket agree after a
// daemon-reload whatever is bound (reload.CheckInEffect refuses it too);
// elsewhere (a stock Alpine is said not to ship ss, [unverified]) the
// configured set stands, and the evidence says so. On Proxmox VE,
// pve.CheckSSH's Port22. docs/engine/check-ssh.md lists each status line.
func (Port) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	r, err := read(ctx, sys, f)
	if err != nil {
		return unread(ctx, sys, f, err)
	}
	l := r.eff.Listeners
	st := engine.Status{Evidence: listenEvidence(r), Findings: r.conflicts("port", "listenaddress")}
	if !l.Known {
		return unknown(listenersUnknown(f.SSH, r), st.Evidence, append(st.Findings, r.unknowns("port", "listenaddress")...)), nil
	}
	ports := l.Ports()
	b, err := readBound(ctx, sys, l)
	switch {
	case errors.Is(err, host.ErrNotFound) && f.SSH.Mode == detect.SSHSocket:
		st.State, st.Detail = engine.Unknown, "ss not installed"
		return st, err
	case errors.Is(err, host.ErrNotFound):
		st.Evidence = append(st.Evidence, "ss: not installed, so the bound sockets are not read")
	case err != nil:
		st.State, st.Detail = engine.Unknown, "can't read"
		return st, err
	default:
		st.Evidence = append(st.Evidence, "ss -Htln: "+b.on(with22(ports)))
		if len(b.missing) > 0 {
			st.Findings = append(st.Findings, b.notInEffect(f.SSH.Mode))
			st.State, st.Detail = engine.Unknown, notListening(b)
			return st, nil
		}
	}
	onPVE, err := pve.Applies(f.Proxmox)
	if err != nil {
		st.State, st.Detail = engine.Unknown, "can't tell if Proxmox"
		return st, err
	}
	if onPVE {
		chk, err := r.proxmox(ctx, sys, f.Proxmox, false)
		if err != nil {
			st.State, st.Detail = engine.Unknown, "can't read"
			return st, err
		}
		st.Findings = pveFindings(st.Findings, chk.Findings, pve.FindingPortNot22)
		switch _, d := portState(ports); {
		case chk.Port22 == detect.Yes:
			st.State, st.Detail = engine.Configured, d
		case chk.Port22 == detect.No && slices.Equal(ports, []uint16{22}): // all on 22, another Port line
			st.State, st.Detail = engine.NotConfigured, "other ports configured"
		case chk.Port22 == detect.No:
			st.State, st.Detail = engine.NotConfigured, d
		default:
			st.State, st.Detail = engine.Unknown, "can't read"
		}
		return st, nil
	}
	if !slices.Contains(ports, 22) && slices.ContainsFunc(b.all, func(x reload.Bound) bool { return x.Port == 22 }) {
		st.Findings = append(st.Findings, engine.Finding{Code: Code22Bound, Source: "ss -Htln",
			Detail: b.on([]uint16{22}) + " bound, not configured: sshd not reloaded since 22 was dropped, or another program on 22"})
		st.State, st.Detail = engine.Unknown, "22 still open"
		return st, nil
	}
	st.State, st.Detail = portState(ports)
	return st, nil
}

// portState is the state of listeners on ports off Proxmox VE, and the
// detail that names them.
func portState(ports []uint16) (engine.State, string) {
	others := slices.DeleteFunc(slices.Clone(ports), func(p uint16) bool { return p == 22 })
	switch {
	case len(others) == len(ports):
		return engine.Configured, "non-default (" + portList(others) + ")"
	case len(others) == 0:
		return engine.NotConfigured, "default (22)"
	}
	return engine.Partial, "22 and " + portList(others) // 22 still open beside the new port: a confirm window, say
}

// notListening is the detail of b.missing: the ports no socket is bound
// on, or, where each is bound on another address only, that.
func notListening(b bound) string {
	ports := portsOf(b.missing)
	none := slices.DeleteFunc(slices.Clone(ports), func(p uint16) bool {
		return slices.ContainsFunc(b.all, func(x reload.Bound) bool { return x.Port == p })
	})
	if len(none) > 0 {
		return "not listening on " + portList(none)
	}
	return portList(ports) + " on another address"
}

// listenersUnknown says why the configured listeners are not established,
// as far as detect's facts and ReadEffective's findings show it
// (sshd-effective.md §2.5, §2.6a): a disagreement counts only for the
// keyword it names.
func listenersUnknown(s detect.SSH, r reading) string {
	differ := func(k string) bool {
		return slices.ContainsFunc(r.found, func(f sshd.Finding) bool {
			return f.Kind == sshd.FindingDisagree && slices.Contains(names(f.Detail), k)
		})
	}
	switch {
	case s.Mode == detect.SSHUnknown:
		return "can't tell what listens"
	case s.Mode == detect.SSHSocket && s.Socket.IsActive() != detect.Yes:
		return "ssh.socket not active"
	case s.Mode == detect.SSHService && s.Service.IsActive() != detect.Yes,
		s.Mode == detect.SSHOpenRC && s.OpenRC.Started != detect.Yes:
		return "sshd not running"
	case !r.eff.Port.Known && differ("port"):
		return "files and sshd differ"
	case s.Mode == detect.SSHSocket && differ("listenaddress"):
		return "socket and sshd differ"
	}
	return "can't read"
}

// listenEvidence is what the listeners were read from: sshd -T's
// listenaddress lines and, where read, ssh.socket's Listen entries.
func listenEvidence(r reading) []string {
	ev := r.said("listenaddress")
	if l := r.eff.Listeners; l.SocketRead {
		ev = append(ev, "systemctl show -p Listen "+sshd.SocketUnit+": "+listeners(l.Socket))
	}
	return ev
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

// with22 is ports with 22, ascending: the ports whose bound sockets the
// evidence lists.
func with22(ports []uint16) []uint16 {
	if slices.Contains(ports, 22) {
		return ports
	}
	out := append(slices.Clone(ports), 22)
	slices.Sort(out)
	return out
}

// portsOf lists ls's ports, ascending, each once.
func portsOf(ls []sshd.Listener) []uint16 {
	return sshd.Listeners{All: ls}.Ports()
}

func portList(ps []uint16) string {
	s := make([]string, len(ps))
	for i, p := range ps {
		s[i] = strconv.Itoa(int(p))
	}
	return strings.Join(s, ", ")
}
