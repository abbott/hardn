// Package drift is hardn check (docs/engine/design-check.md, adopted
// 2026-11-13): the facts of a host that a baseline keeps (Read), the
// baseline's file (Save, Load) and the comparison of two readings
// (Compare). It reads the host through the readers the screens and the
// checks already have, writes one file, the baseline, and only when asked
// (Save), and never plans or applies a change.
package drift

import (
	"slices"
	"strings"
)

// The facts, by id. An id is part of the baseline's format and of hardn
// check's JSON document: it never changes or goes within a Format.
const (
	Admins         = "users.admins"
	UID0           = "users.uid0"
	Keys           = "users.keys"
	EmptyPasswords = "users.emptyPasswords"
	RootLogin      = "ssh.rootLogin"
	PasswordLogin  = "ssh.passwordLogin"
	SSHPorts       = "ssh.ports"
	SSHListeners   = "ssh.listeners"
	AllowUsers     = "ssh.allowUsers"
	FirewallState  = "firewall.state"
	Incoming       = "firewall.incoming"
	FirewallBoot   = "firewall.boot"
	Rules          = "firewall.rules"
	Ports          = "firewall.ports"
	Nameservers    = "dns.nameservers"
	AutoUpdates    = "updates.auto"
)

// Spec is one fact hardn check reads: its id, the words a difference's line
// names it by, and for a set, compared item by item whatever their order,
// the words for one item; a fact with no Item is one value. A set with
// Order is compared for its items' order too, and Order names that
// difference: ufw applies the first rule that matches.
type Spec struct {
	ID    string
	Title string
	Item  string
	Order string
}

// Set reports whether the fact is a set.
func (s Spec) Set() bool { return s.Item != "" }

// Specs are the facts, in the order they are read, kept and reported.
var Specs = []Spec{
	{Admins, "Admins", "Admin", ""},
	{UID0, "Accounts with uid 0", "Account with uid 0", ""},
	{Keys, "SSH keys", "SSH key", ""},
	{EmptyPasswords, "Accounts with an empty password", "Account with an empty password", ""},
	{RootLogin, "SSH root login", "", ""},
	{PasswordLogin, "SSH password login", "", ""},
	{SSHPorts, "SSH port", "", ""},
	{SSHListeners, "SSH listeners", "", ""},
	{AllowUsers, "SSH allowed users", "", ""},
	{FirewallState, "Firewall", "", ""},
	{Incoming, "Firewall incoming policy", "", ""},
	{FirewallBoot, "Firewall at boot", "", ""},
	{Rules, "Firewall rules", "Firewall rule", "Firewall rule order"},
	{Ports, "Listening ports", "Reachable port", ""},
	{Nameservers, "Nameservers", "", ""},
	{AutoUpdates, "Auto updates", "", ""},
}

// Fact is one fact as read. A fact that could not be established has Known
// false and says why; nothing is assumed for it.
type Fact struct {
	ID    string `json:"id"`
	Known bool   `json:"known"`
	// Why is why the fact is not established: an error's text, for a
	// person. It is not compared.
	Why string `json:"why,omitempty"`
	// NotCompared: the fact is left out of the comparison, on either side:
	// the listening ports on a host with no ss (decision 5).
	NotCompared bool `json:"notCompared,omitempty"`
	// Value is a value's text ("" for none); Items a set's members, sorted,
	// each once; the ufw rules of firewall.rules in ufw's order instead.
	Value string   `json:"value,omitempty"`
	Items []string `json:"items,omitempty"`
}

// Facts are one reading of the host: every fact of Specs, in its order.
type Facts []Fact

// value, set, ordered and unknown make a fact.
func value(id, v string) Fact { return Fact{ID: id, Known: true, Value: v} }

func set(id string, items []string) Fact {
	items = slices.Clone(items)
	slices.Sort(items)
	return Fact{ID: id, Known: true, Items: slices.Compact(items)}
}

// ordered is a set whose items keep their order, each once, the first of
// two the same kept.
func ordered(id string, items []string) Fact {
	return Fact{ID: id, Known: true, Items: once(items)}
}

// once is items in their order, each once.
func once(items []string) []string {
	var out []string
	for _, it := range items {
		if !slices.Contains(out, it) {
			out = append(out, it)
		}
	}
	return out
}

func unknown(id string, why any) Fact {
	f := Fact{ID: id}
	switch w := why.(type) {
	case error:
		f.Why = w.Error()
	case string:
		f.Why = w
	}
	return f
}

// Change is what a difference is.
type Change string

// The changes.
const (
	Changed Change = "changed" // a value, or whether the fact is established
	Added   Change = "added"   // a set has an item the baseline has not
	Removed Change = "removed" // a set lacks an item the baseline has
)

// Difference is one difference between a baseline and the host: what it
// was and what it is. A nil Was or Is is "not established"; for an item
// added Was is "", for one removed Is is "". Title names the fact, or for
// an item added or removed, one item of it.
type Difference struct {
	Fact   string
	Title  string
	Change Change
	Was    *string
	Is     *string
}

// Note is a fact left out of the comparison, and why.
type Note struct {
	Fact  string
	Title string
	Why   string
}

// Compare lists how now differs from base, in Specs' order, a set's items
// removed then added, each in sorted order: the same two readings give the
// same list, whatever order their facts or items are in. A set with an
// Order is the exception: where the items both sides have are in another
// order, that is one more difference, after the set's others, Was and Is
// those items in each order. A fact established on one side alone is a
// difference; one established on neither is none, whatever each says of
// why. A fact either side marks NotCompared is a Note instead, with the
// host's reason, or NoSSBaseline where the baseline alone has one. A fact
// a reading lacks is taken as not established.
func Compare(base, now Facts) (diffs []Difference, notes []Note) {
	for _, sp := range Specs {
		a, b := base.fact(sp.ID), now.fact(sp.ID)
		if a.NotCompared || b.NotCompared {
			why := b.Why
			if !b.NotCompared {
				why = NoSSBaseline
			}
			notes = append(notes, Note{Fact: sp.ID, Title: sp.Title, Why: why})
			continue
		}
		d := Difference{Fact: sp.ID, Title: sp.Title, Change: Changed}
		switch {
		case !a.Known && !b.Known:
		case !a.Known || !b.Known:
			d.Was, d.Is = a.text(sp), b.text(sp)
			diffs = append(diffs, d)
		case sp.Set():
			was, is := sorted(a.Items), sorted(b.Items)
			for _, it := range was {
				if _, found := slices.BinarySearch(is, it); !found {
					diffs = append(diffs, Difference{Fact: sp.ID, Title: sp.Item, Change: Removed, Was: ptr(it), Is: ptr("")})
				}
			}
			for _, it := range is {
				if _, found := slices.BinarySearch(was, it); !found {
					diffs = append(diffs, Difference{Fact: sp.ID, Title: sp.Item, Change: Added, Was: ptr(""), Is: ptr(it)})
				}
			}
			if sp.Order == "" {
				break
			}
			both := func(items, other []string) []string {
				return slices.DeleteFunc(once(items), func(it string) bool {
					_, found := slices.BinarySearch(other, it)
					return !found
				})
			}
			if x, y := both(a.Items, is), both(b.Items, was); !slices.Equal(x, y) {
				diffs = append(diffs, Difference{Fact: sp.ID, Title: sp.Order, Change: Changed,
					Was: ptr(strings.Join(x, ", ")), Is: ptr(strings.Join(y, ", "))})
			}
		case a.Value != b.Value:
			d.Was, d.Is = ptr(a.Value), ptr(b.Value)
			diffs = append(diffs, d)
		}
	}
	return diffs, notes
}

// fact is the fact of id; one not established where fs has none.
func (fs Facts) fact(id string) Fact {
	for _, f := range fs {
		if f.ID == id {
			return f
		}
	}
	return Fact{ID: id}
}

// text is the whole fact as one string: nil where not established, a
// set's items joined, in their order for a set with one.
func (f Fact) text(sp Spec) *string {
	switch {
	case !f.Known:
		return nil
	case sp.Order != "":
		return ptr(strings.Join(once(f.Items), ", "))
	case sp.Set():
		return ptr(strings.Join(sorted(f.Items), ", "))
	}
	return ptr(f.Value)
}

func sorted(items []string) []string {
	out := slices.Clone(items)
	slices.Sort(out)
	return slices.Compact(out)
}

func ptr(s string) *string { return &s }
