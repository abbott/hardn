package ssh

import (
	"context"
	"errors"
	"fmt"
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
// configured listener that is not bound is Unknown, since the configuration
// is then not what accepts connections. Without ss (Alpine does not ship
// it) the configured set stands, and the evidence says so. On Proxmox VE,
// pve.CheckSSH's Port22. docs/engine/check-ssh.md lists each status line.
func (Port) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	onPVE, err := pve.Applies(f.Proxmox)
	if err != nil {
		return engine.Status{State: engine.Unknown, Detail: "can't tell if Proxmox"}, err
	}
	r, err := read(ctx, sys, f)
	if err != nil {
		return failed(f.SSH, err)
	}
	l := r.eff.Listeners
	st := engine.Status{Evidence: listenEvidence(r), Findings: r.conflicts("port", "listenaddress")}
	if !l.Known {
		return unknown(listenersUnknown(f.SSH, r), st.Evidence, append(st.Findings, r.unknowns("port", "listenaddress")...)), nil
	}
	bound, bad, err := reload.ReadBound(ctx, sys)
	switch {
	case errors.Is(err, host.ErrNotFound):
		st.Evidence = append(st.Evidence, "ss: not installed, so the bound sockets are not read")
	case err != nil:
		st.State, st.Detail = engine.Unknown, "can't read"
		return st, err
	default:
		st.Evidence = append(st.Evidence, "ss -Htln: "+boundOn(bound, l.Ports()))
		if missing := reload.Unbound(l.All, bound); len(missing) > 0 {
			d := fmt.Sprintf("%v configured, not bound: not in effect (an edit not reloaded, or ssh.socket not restarted)", missing)
			if len(bad) > 0 {
				d += fmt.Sprintf("; ss lines not read: %q", bad)
			}
			st.Findings = append(st.Findings, engine.Finding{Code: CodeNotInEffect, Detail: d, Source: "ss -Htln"})
			st.State, st.Detail = engine.Unknown, "not listening on "+portList(portsOf(missing))
			return st, nil
		}
	}
	if onPVE {
		chk, err := r.proxmox(ctx, sys, f.Proxmox, false)
		if err != nil {
			st.State, st.Detail = engine.Unknown, "can't read"
			return st, err
		}
		st.Findings = pveFindings(st.Findings, chk.Findings, pve.FindingPortNot22)
		switch chk.Port22 {
		case detect.Yes:
			st.State, st.Detail = engine.Configured, "22, as Proxmox needs"
		case detect.No:
			st.State, st.Detail = engine.NotConfigured, "Proxmox needs 22 only"
		default:
			st.State, st.Detail = engine.Unknown, "can't read"
		}
		return st, nil
	}
	ports := l.Ports()
	others := slices.DeleteFunc(slices.Clone(ports), func(p uint16) bool { return p == 22 })
	switch {
	case len(others) == len(ports):
		st.State, st.Detail = engine.Configured, "non-default ("+portList(others)+")"
	case len(others) == 0:
		st.State, st.Detail = engine.NotConfigured, "default (22)"
	default: // 22 still open beside the new port: during a confirm window, say
		st.State, st.Detail = engine.Partial, "22 and "+portList(others)
	}
	return st, nil
}

// listenersUnknown says why the configured listeners are not established,
// as far as detect's facts and ReadEffective's findings show it
// (sshd-effective.md §2.5, §2.6a).
func listenersUnknown(s detect.SSH, r reading) string {
	disagree := slices.ContainsFunc(r.found, func(f sshd.Finding) bool { return f.Kind == sshd.FindingDisagree })
	switch {
	case s.Mode == detect.SSHUnknown:
		return "can't tell what listens"
	case s.Mode == detect.SSHSocket && s.Socket.IsActive() != detect.Yes:
		return "ssh.socket not active"
	case s.Mode == detect.SSHService && s.Service.IsActive() != detect.Yes,
		s.Mode == detect.SSHOpenRC && s.OpenRC.Started != detect.Yes:
		return "sshd not running"
	case disagree && !r.eff.Port.Known:
		return "files and sshd differ"
	case disagree:
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

// boundOn lists the bound sockets on one of ports.
func boundOn(bound []reload.Bound, ports []uint16) string {
	var s []string
	for _, b := range bound {
		if slices.Contains(ports, b.Port) {
			s = append(s, b.String())
		}
	}
	if len(s) == 0 {
		return "none on " + portList(ports)
	}
	return strings.Join(s, " ")
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
