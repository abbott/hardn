package ufw

import "slices"

// OnlyFrom is the sources ufw lets TCP port in from, where it lets it in
// from some sources alone: each saved rule (State.Rules, Read's, both IP
// versions) that allows or limits port/tcp inbound, its Src as read, in
// rule order, each once. It is nil where ufw is not active, where no such
// rule allows port, or where one allows it from anywhere in either IP
// version (Src ""; its destination address or interface does not narrow
// who may connect). Deny rules are not weighed, nor the default policy: a
// source listed may still be denied before its rule.
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
		if r.Src == "" {
			return nil
		}
		if !slices.Contains(srcs, r.Src) {
			srcs = append(srcs, r.Src)
		}
	}
	return srcs
}
