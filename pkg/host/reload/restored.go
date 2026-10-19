package reload

import (
	"cmp"
	"fmt"
	"net/netip"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host/sshd"
)

// restoredReady refuses (ErrRestoredListeners) a rollback whose restored
// ssh.socket drop-ins (Change.RestoredDropIns) would not give the socket
// exactly after once daemon-reload reads them, so that such a rollback
// stops before daemon-reload rather than at the check after it, when the
// socket's configuration has already moved (next/p2-revert-plans). d is
// ReadSocketDropIns's answer: at rollback time, after the restore; at plan
// time, before the change, which is the state the restore gives back.
//
// Each restored drop-in must be one of d's files, not masked, under a name
// no other file of d has (one of the same name in another directory masks
// it or is masked by it) that sorts after the generator's addresses.conf:
// drop-ins apply in the order of their names, whatever their directory
// (systemd.unit(5)), so one read after it sets the socket's list last. In
// that order, their ListenStream values must reset the list ("", as
// v0.3.x's listen.conf does), so that neither the unit's own lines nor the
// generator's count, and then name listeners that match after one for
// one: a port alone, which systemd binds on a dual-stack [::] (0.0.0.0
// where IPv6 is not supported), matches an unspecified address of either
// family; IPv4:port and [IPv6]:port match that address. Anything else
// (a path, an interface, vsock) is not read, so it is refused.
func restoredReady(d SocketDropIns, restored []string, after []sshd.Listener) error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("reload: restored %s: %s: %w", strings.Join(restored, ", "), fmt.Sprintf(format, a...), ErrRestoredListeners)
	}
	files := make([]SocketDropIn, 0, len(restored))
	for _, r := range restored {
		i := slices.IndexFunc(d.Files, func(f SocketDropIn) bool { return f.Path == r })
		switch {
		case i < 0:
			return bad("%s is not there", r)
		case d.Files[i].Masked:
			return bad("%s is masked", r)
		case path.Base(r) <= generatorDropIn:
			return bad("%s is read before the generator's %s, which sets the list after it", r, generatorDropIn)
		}
		for _, f := range d.Files {
			if f.Path != r && path.Base(f.Path) == path.Base(r) {
				return bad("%s has the name of %s", f.Path, r)
			}
		}
		files = append(files, d.Files[i])
	}
	slices.SortStableFunc(files, func(a, b SocketDropIn) int { return cmp.Compare(path.Base(a.Path), path.Base(b.Path)) })
	var values []string
	for _, f := range files {
		values = append(values, f.Listen...)
	}
	last := -1
	for i, v := range values {
		if v == "" {
			last = i
		}
	}
	if last < 0 {
		return bad("their ListenStream lines %q do not reset the list, so the unit's and the generator's listeners stay", values)
	}
	var exact, portOnly []restoredListener
	for _, v := range values[last+1:] {
		l, ok := parseListenStream(v)
		switch {
		case !ok:
			return bad("ListenStream=%s is not a port, IPv4:port or [IPv6]:port", v)
		case l.portOnly:
			portOnly = append(portOnly, l)
		default:
			exact = append(exact, l)
		}
	}
	// One for one: the exact values first, so that a port alone does not
	// take the listener an exact one needs.
	used := make([]bool, len(after))
	for _, l := range append(exact, portOnly...) {
		i := -1
		for k, a := range after {
			if !used[k] && l.matches(a) {
				i = k
				break
			}
		}
		if i < 0 {
			return bad("ListenStream=%s is not one of the listeners the socket had, %v", l.value, after)
		}
		used[i] = true
	}
	if i := slices.Index(used, false); i >= 0 {
		return bad("their ListenStream lines %q do not give %v, which the socket had", values[last+1:], after[i])
	}
	return nil
}

// restoredListener is one ListenStream value of a restored drop-in.
type restoredListener struct {
	value    string
	addr     netip.Addr // unset with portOnly
	port     uint16
	portOnly bool
}

// matches reports whether the socket systemd makes for l is a: the same
// port, and the same address, or for a port alone an unspecified one.
func (l restoredListener) matches(a sshd.Listener) bool {
	if l.port != a.Port {
		return false
	}
	if l.portOnly {
		return a.Addr.IsUnspecified()
	}
	return l.addr == a.Addr.Unmap()
}

// parseListenStream reads an IP ListenStream value (systemd.socket(5)): a
// port alone, IPv4:port or [IPv6]:port, without a zone.
func parseListenStream(v string) (restoredListener, bool) {
	l := restoredListener{value: v}
	if p, err := strconv.ParseUint(v, 10, 16); err == nil {
		l.port, l.portOnly = uint16(p), true
		return l, p != 0
	}
	ap, err := netip.ParseAddrPort(v)
	if err != nil || ap.Port() == 0 || ap.Addr().Zone() != "" || ap.Addr().Is4In6() {
		return l, false
	}
	l.addr, l.port = ap.Addr(), ap.Port()
	return l, true
}
