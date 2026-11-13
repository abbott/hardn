package screen

import "github.com/abbott/hardn/pkg/ui/component"

// The listening ports (docs/roadmap.md, "Listening ports"; the mock unit,
// next/listening-ports, docs/engine/gallery-ports.md): what something on the
// host listens on, as ss lists it (ss -Htlnp, ss -Hulnp), set against the firewall's rules
// on the Firewall screen (FirewallPorts) and listed plainly on System Details
// (Listeners). Adopted 2026-11-13 ("Adopt all"); there is no Figma frame.
// pkg/ui cannot reach the host: pkg/screens/firewall and
// pkg/screens/systemdetails read ss and the rules and fill them
// (docs/engine/screen-ports.md).

// The ports' words (gallery-ports.md, "Words"). "Not filtered", "ss not
// installed" and "ss failed" are the Firewall screen's SSH section's; "Some
// not read" is the real unit's, proposed.
const (
	portsSection     = "Ports"
	listenersSection = "Listening Ports"

	portOpen       = "Open"
	portSome       = "Some sources"
	portBlocked    = "Blocked"
	portLocal      = "Local only"
	portUnfiltered = "Not filtered"
	portUnusedNote = "nothing listening"

	portsKey         = "Listening ports"
	portsNoSS        = "ss not installed"
	portsFailed      = "ss failed"
	portsPartial     = "Some not read"
	portsNoneKey     = "Nothing is listening"
	listenersNotRoot = "Run hardn as root to see every process."
)

// PortReach is how a port stands against the firewall's rules.
type PortReach uint8

const (
	// PortOpen: something listens, and the firewall lets it in from anywhere.
	PortOpen PortReach = iota
	// PortSome: something listens, and the firewall lets it in from some
	// sources only.
	PortSome
	// PortBlocked: something listens, and the firewall does not let it in.
	PortBlocked
	// PortLocal: something listens on a loopback address alone, which nothing
	// outside the host reaches, whatever the rules.
	PortLocal
	// PortUnused: the firewall lets it in and nothing listens on it.
	PortUnused
	// PortUnfiltered: something listens and the firewall is not running.
	PortUnfiltered
	// PortUnknown: something listens, and how the firewall stands to it is
	// not established. Its row's value is the component's "Unknown".
	PortUnknown
)

// FirewallPort is one port of the Firewall screen's Ports section: a port
// something listens on, or one a rule lets in.
type FirewallPort struct {
	// Port and Proto name it: "443" and "tcp"; a rule's range with nothing
	// listening as the firewall writes it, and Proto "" for a rule of both
	// protocols, whose row is the port alone.
	Port, Proto string
	// Process is what listens on it, "" where ss does not say or nothing does.
	Process string
	// Reach is how it stands against the rules.
	Reach PortReach
}

// FirewallPorts is the Firewall screen's reading of the listening ports and the
// rules.
type FirewallPorts struct {
	// NoSS: ss is not installed, so the listening ports are not read.
	// Failed: ss ran and failed.
	NoSS, Failed bool
	// Partial: ss printed lines that were not read, which a last row says.
	Partial bool
	// Ports are the section's ports, in its order.
	Ports []FirewallPort
}

// portKey is a row's key: the port and its protocol.
func portKey(port, proto string) string {
	if proto == "" {
		return port
	}
	return port + "/" + proto
}

// unreadRows are the rows of a reading that listed nothing, and whether it
// is one: no ss, ss failed, or nothing listening and every line read.
func unreadRows(noSS, failed, none bool) ([]component.KeyValue, bool) {
	switch {
	case noSS:
		return []component.KeyValue{{Key: portsKey, Value: portsNoSS, State: component.ValueUnknown}}, true
	case failed:
		return []component.KeyValue{{Key: portsKey, Value: portsFailed, State: component.ValueUnknown}}, true
	case none:
		return []component.KeyValue{{Key: portsNoneKey, State: component.ValueNone}}, true
	}
	return nil, false
}

// partialRow says that some of ss's lines were not read.
func partialRow() component.KeyValue {
	return component.KeyValue{Key: portsKey, Value: portsPartial, State: component.ValueUnknown}
}

// Rows are the Ports section's rows, for Firewall.Ports and Firewalld.Ports:
// each port, how it stands and what listens on it; a rule's port with nothing
// listening, a warning; one not established, "Unknown"; without ss, or with
// an ss that failed, the one row that says so; with ss and no port, a muted
// line; and a last row where some of ss's lines were not read.
func (p FirewallPorts) Rows() []component.KeyValue {
	if rows, ok := unreadRows(p.NoSS, p.Failed, len(p.Ports) == 0 && !p.Partial); ok {
		return rows
	}
	words := map[PortReach]string{PortOpen: portOpen, PortSome: portSome, PortBlocked: portBlocked, PortLocal: portLocal, PortUnfiltered: portUnfiltered}
	var rows []component.KeyValue
	for _, port := range p.Ports {
		row := component.KeyValue{Key: portKey(port.Port, port.Proto), Value: words[port.Reach], Extra: port.Process}
		switch port.Reach {
		case PortUnused:
			row = component.KeyValue{Key: row.Key, Value: portOpen, Note: portUnusedNote, State: component.ValueWarn}
		case PortUnknown:
			row.State = component.ValueUnknown
		}
		rows = append(rows, row)
	}
	if p.Partial {
		rows = append(rows, partialRow())
	}
	return rows
}

// Listener is one listening socket, as ss lists it.
type Listener struct {
	// Port and Proto name it: "443" and "tcp".
	Port, Proto string
	// Address is the address it is bound to, in ss's form: "0.0.0.0", "[::]",
	// "127.0.0.1".
	Address string
	// Process is what listens on it, "" where ss does not say.
	Process string
}

// Listeners is System Details' reading of the listening sockets.
type Listeners struct {
	// NoSS: ss is not installed, so they are not read. Failed: ss ran and
	// failed.
	NoSS, Failed bool
	// Partial: ss printed lines that were not read, which a row says.
	Partial bool
	// NotRoot: hardn does not run as root, so ss names its user's processes
	// alone, which the section's last line says.
	NotRoot bool
	// List are the sockets, in the section's order.
	List []Listener
}

// Rows are the Listening Ports section's rows, for SystemDetails.Ports: each
// socket's port and protocol, its address, and its process where ss names it;
// without ss, or with an ss that failed, the one row that says so; with ss
// and no socket, a muted line; a row where some of ss's lines were not read.
func (l Listeners) Rows() []component.KeyValue {
	if rows, ok := unreadRows(l.NoSS, l.Failed, len(l.List) == 0 && !l.Partial); ok {
		return rows
	}
	var rows []component.KeyValue
	for _, s := range l.List {
		rows = append(rows, component.KeyValue{Key: portKey(s.Port, s.Proto), Value: s.Address, Extra: s.Process})
	}
	if l.Partial {
		rows = append(rows, partialRow())
	}
	if l.NotRoot && len(l.List) > 0 {
		rows = append(rows, component.KeyValue{Key: listenersNotRoot, State: component.ValueNone})
	}
	return rows
}
