package ufw

import "slices"

// OnlyFrom is the sources ufw lets TCP port in from, where it lets it in
// from some sources alone: each saved rule (State.Rules, Read's, both IP
// versions) that allows or limits port/tcp inbound, its Src as read, in
// rule order, each once. A rule limited to an interface (Iface) narrows
// who may connect too: it is listed as "<src> on <iface>", "anywhere on
// <iface>" where it has no Src. It is nil where ufw is not active, where
// no such rule allows port, or where one allows it from anywhere on any
// interface in either IP version (Src and Iface ""; its destination
// address does not narrow who may connect). Deny rules are not weighed,
// nor the default policy: a source listed may still be denied before its
// rule.
func OnlyFrom(st State, port uint16) []string {
	if !st.Status.Active {
		return nil
	}
	var srcs []string
	for _, r := range st.Rules {
		if r.Dir != "in" || r.Action != "allow" && r.Action != "limit" || r.Proto != "tcp" && r.Proto != "any" {
			continue
		}
		if in, ok := portIn(r.DPort, port); !ok || !in {
			continue
		}
		src := r.Src
		switch {
		case src == "" && r.Iface == "":
			return nil
		case r.Iface == "":
		case src == "":
			src = "anywhere on " + r.Iface
		default:
			src += " on " + r.Iface
		}
		if !slices.Contains(srcs, src) {
			srcs = append(srcs, src)
		}
	}
	return srcs
}
