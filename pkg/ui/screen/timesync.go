package screen

import (
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// The NTP Settings screen (docs/roadmap.md, "Time sync";
// docs/engine/gallery-timesync.md, adopted 2026-11-14;
// docs/engine/screen-timesync.md): main menu 8, then 3. Which service keeps the
// host's time, whether it runs, whether the clock is in sync, the servers
// it uses and where each is set; the servers hardn.yml names; and one or
// two items named by their result. hardn's servers take the place of the
// service's own (decision 1), so a host hardn set shows hardn's alone, and
// whatever DHCP hands out. On the sub-menu layout: there is no Figma frame.
// pkg/screens/timesync fills TimeSync from what the control reads.

// The screen's words: proposed (gallery-timesync.md, "Words").
const (
	timeTitle   = "NTP Settings"
	timeSection = "Time"

	timeServiceKey = "Service"
	timeClockKey   = "Clock"
	timeSyncedKey  = "Synced to"
	timeServerKey  = "Server"
	timeFileKey    = "File"

	timeChrony    = "chrony"
	timeTimesyncd = "systemd-timesyncd"
	timeBusybox   = "busybox ntpd"

	timeNone       = "None"
	timeNotRunning = "not running"
	timeInSync     = "In sync"
	timeNotInSync  = "Not in sync"
	timeByHardn    = "by hardn"
	timeFromDHCP   = "from DHCP"
	timeBuiltIn    = "built in"
	timeInFile     = "in "
	timeNoWant     = "No servers"

	timeSet          = "Set servers"
	timeRemove       = "Remove servers"
	timeDescRemove   = "Undo hardn's change"
	timeDescNoWant   = "No servers in hardn.yml"
	timeDescBad      = "hardn.yml is not valid"
	timeDescOther    = "Set outside hardn"
	timeDescNone     = "No time service"
	timeDescStopped  = " is not running"
	timeDescHostTime = "The host keeps the time"

	timeContainerNote = "In a container: the host keeps the time."

	timeAskSet    = "Set time servers?"
	timeAskRemove = "Remove hardn's time servers?"
)

// The items' keys: Set servers is 1 wherever it shows, and Remove servers
// is 1 alone and 2 beside it.
const (
	TimeSyncFirstKey  = setUndoFirstKey
	TimeSyncSecondKey = setUndoSecondKey
)

// TimeService is the service that keeps the host's time.
type TimeService uint8

const (
	// TimeChrony: chronyd.
	TimeChrony TimeService = iota
	// TimeTimesyncd: systemd-timesyncd.
	TimeTimesyncd
	// TimeBusybox: busybox ntpd, Alpine's, which says nothing of the
	// server it follows.
	TimeBusybox
	// TimeNoService: the host has no time service.
	TimeNoService
)

// timeServiceWords are the services' names.
var timeServiceWords = map[TimeService]string{TimeChrony: timeChrony, TimeTimesyncd: timeTimesyncd, TimeBusybox: timeBusybox}

// TimeClock is whether the clock is in sync.
type TimeClock uint8

const (
	// TimeClockUnknown: not read.
	TimeClockUnknown TimeClock = iota
	// TimeClockInSync: the clock follows a time server.
	TimeClockInSync
	// TimeClockNotInSync: it follows none.
	TimeClockNotInSync
)

// TimeFrom is where a server the service uses is set.
type TimeFrom uint8

const (
	// TimeFromFile: a file of the service's own, as the distribution
	// installs it: a Server row says nothing more of it.
	TimeFromFile TimeFrom = iota
	// TimeFromHardn: hardn's own setting.
	TimeFromHardn
	// TimeFromOther: a file that is neither the service's own nor
	// hardn's, named by TimeServer.File.
	TimeFromOther
	// TimeFromDHCP: handed out with the host's address.
	TimeFromDHCP
	// TimeFromBuiltIn: compiled into the service, as systemd-timesyncd's
	// fallback servers are.
	TimeFromBuiltIn
)

// TimeServer is one server, or pool of servers, the service uses.
type TimeServer struct {
	// Name is its name or address.
	Name string
	// From is where it is set.
	From TimeFrom
	// File is the file that names it, by its base name, such as
	// "site.sources": for TimeFromOther.
	File string
}

// TimeSync is the NTP Settings screen's reading of the host, which its caller
// fills.
type TimeSync struct {
	// Status is the NTP row.
	Status component.StatusList
	// Service keeps the host's time.
	Service TimeService
	// Stopped: Service is installed and not running.
	Stopped bool
	// Container: the host is a container, whose clock is its host's.
	Container bool
	// Unread is why the service's settings cannot be read, in a few words,
	// such as "Can't read chrony's settings"; "" where they are read.
	Unread string
	// Clock is whether the clock is in sync.
	Clock TimeClock
	// Synced is the server the clock follows, as the service names it; ""
	// for none, and for a service that does not say.
	Synced string
	// Servers are the servers the service uses, in its order.
	Servers []TimeServer
	// File is the file hardn's own servers are set in; "" with none of
	// hardn's.
	File string
	// Restores are the servers hardn's own setting took the place of,
	// which Remove servers puts back: the service's own, as the
	// distribution installs them, and systemd-timesyncd's built-in ones.
	Restores []TimeServer
	// Cleared: a file that is not hardn's clears the servers hardn sets
	// (systemd-timesyncd's empty NTP=, read after hardn's file): set
	// outside hardn, with no server to show for it.
	Cleared bool
	// Want are the servers hardn.yml names; nil for none.
	Want []string
	// WantBad: hardn.yml's servers are not valid, so Want is nil and
	// nothing is set from it.
	WantBad bool
}

// own are the servers that are hardn's own setting, in order.
func (t TimeSync) own() []string {
	var out []string
	for _, s := range t.Servers {
		if s.From == TimeFromHardn {
			out = append(out, s.Name)
		}
	}
	return out
}

// outside reports whether a file that is neither the service's own nor
// hardn's names a server, or clears hardn's.
func (t TimeSync) outside() bool {
	return t.Cleared || slices.ContainsFunc(t.Servers, func(s TimeServer) bool { return s.From == TimeFromOther })
}

// differs reports whether hardn.yml names servers and hardn's own setting
// names others, or in another order.
func (t TimeSync) differs() bool {
	own := t.own()
	return len(t.Want) > 0 && len(own) > 0 && !slices.Equal(t.Want, own)
}

// reports reports whether the service says which server the clock follows:
// busybox ntpd does not, and a service that is not there or not running
// says nothing.
func (t TimeSync) reports() bool {
	return !t.Container && !t.Stopped && (t.Service == TimeChrony || t.Service == TimeTimesyncd)
}

// timeNames is names as one value: "192.0.2.10, 192.0.2.11".
func timeNames(names []string) string { return strings.Join(names, ", ") }

// serverRow is s as a Server row, its note where it is set.
func (s TimeServer) row() component.KeyValue {
	row := component.KeyValue{Key: timeServerKey, Value: s.Name}
	switch s.From {
	case TimeFromHardn:
		row.Note = timeByHardn
	case TimeFromDHCP:
		row.Note = timeFromDHCP
	case TimeFromBuiltIn:
		row.Note = timeBuiltIn
	case TimeFromOther:
		row.Note = timeInFile + s.File
	}
	return row
}

// serverRows are the Server rows: one a server, "None" with none.
func serverRows(servers []TimeServer) []component.KeyValue {
	if len(servers) == 0 {
		return []component.KeyValue{{Key: timeServerKey, Value: timeNone}}
	}
	rows := make([]component.KeyValue, len(servers))
	for i, s := range servers {
		rows[i] = s.row()
	}
	return rows
}

// Rows are the Time section's rows: the service, a warning where it is not
// running or not there; the clock; the server it follows, where the
// service says; each server the service uses and where it is set; hardn's
// file; and the servers hardn.yml names, a warning where hardn's own
// setting names others. In a container: no service is expected, and a
// muted line says whose clock it is.
func (t TimeSync) Rows() []component.KeyValue {
	service := component.KeyValue{Key: timeServiceKey, Value: timeServiceWords[t.Service]}
	switch {
	case t.Service == TimeNoService && t.Container:
		service.Value = timeNone
	case t.Service == TimeNoService && t.Unread != "":
		service.State = component.ValueUnknown // not told, which is not "None"
	case t.Service == TimeNoService:
		service.Value, service.State = timeNone, component.ValueWarn
	case t.Stopped:
		service.Note, service.State, service.KeepNote = timeNotRunning, component.ValueWarn, true
	}
	clock := component.KeyValue{Key: timeClockKey, State: component.ValueUnknown}
	switch t.Clock {
	case TimeClockInSync:
		clock = component.KeyValue{Key: timeClockKey, Value: timeInSync, State: component.ValueOK}
	case TimeClockNotInSync:
		clock = component.KeyValue{Key: timeClockKey, Value: timeNotInSync, State: component.ValueWarn}
	}
	rows := []component.KeyValue{service, clock}
	if t.Container {
		return append(rows, component.KeyValue{Key: timeContainerNote, State: component.ValueNone})
	}
	switch {
	case !t.reports():
	case t.Synced != "":
		rows = append(rows, component.KeyValue{Key: timeSyncedKey, Value: t.Synced})
	case t.Clock == TimeClockNotInSync:
		rows = append(rows, component.KeyValue{Key: timeSyncedKey, Value: timeNone, State: component.ValueWarn})
	}
	switch {
	case t.Unread != "":
		rows = append(rows, component.KeyValue{Key: timeServerKey, State: component.ValueUnknown})
	case t.Service != TimeNoService:
		rows = append(rows, serverRows(t.Servers)...)
		if t.File != "" {
			rows = append(rows, component.KeyValue{Key: timeFileKey, Value: t.File})
		}
	}
	return append(rows, wantRow(timeNames(t.Want), timeNoWant, t.WantBad, t.differs()))
}

// setItem is Set servers, its description what it would set or what is in
// the way: the settings unread, a hardn.yml that is not valid, or no
// servers in it; else the servers.
func (t TimeSync) setItem() component.MenuItem {
	it := component.MenuItem{Key: TimeSyncFirstKey, Label: timeSet}
	switch {
	case t.Unread != "":
		it.Description = t.Unread
	case t.WantBad:
		it.Description = timeDescBad
	case len(t.Want) == 0:
		it.Description = timeDescNoWant
	default:
		it.Description = timeNames(t.Want)
	}
	return it
}

// MenuItems are the screen's items, named by their result: Set servers
// while hardn's own setting is not hardn.yml's servers; Remove servers
// while hardn's own setting is there; and Set servers shown as unavailable
// where hardn sets nothing: in a container, with no time service or one
// that is not running, and where a file that is neither the service's own
// nor hardn's names a server. Beside hardn's own setting such a file leaves
// Remove servers: alone where hardn.yml's servers are hardn's, and as 2
// under the unavailable Set servers where they differ.
func (t TimeSync) MenuItems() []component.MenuItem {
	remove := component.MenuItem{Label: timeRemove, Description: timeDescRemove}
	own, off := len(t.own()) > 0, ""
	switch {
	case t.Container:
		off = timeDescHostTime
	case t.Unread != "":
		own = false // Set servers alone, saying what is not read
	case t.Service == TimeNoService:
		off = timeDescNone
	case t.Stopped:
		off = timeServiceWords[t.Service] + timeDescStopped
	case !own && t.outside():
		off = timeDescOther
	case t.outside() && t.differs():
		remove.Key = TimeSyncSecondKey
		return []component.MenuItem{{Key: TimeSyncFirstKey, Label: timeSet, Description: timeDescOther, Disabled: true}, remove}
	}
	return setUndoItems(t.setItem(), remove, off, own, t.differs())
}

// Removes reports whether it, one of MenuItems, is Remove servers; Set
// servers otherwise.
func (TimeSync) Removes(it component.MenuItem) bool { return it.Label == timeRemove }

// SubMenu returns the NTP Settings screen: the title bar; the row; the
// section; the items; Refresh; Return; "Select an option"; "Option".
func (t TimeSync) SubMenu() SubMenu {
	return SubMenu{
		Title:    trail(timeTitle),
		Status:   t.Status,
		Sections: []Section{{Header: component.SectionHeader{Title: timeSection}, Rows: component.BulletList{Items: t.Rows()}}},
		Heading:  fixtureHeading,
		Menu:     component.Menu{Items: withRefresh(t.MenuItems()), Exit: returnItem()},
		Prompt:   menuPrompt,
	}
}

// from are the servers set in one of froms, in order.
func (t TimeSync) from(froms ...TimeFrom) []TimeServer {
	var out []TimeServer
	for _, s := range t.Servers {
		if slices.Contains(froms, s.From) {
			out = append(out, s)
		}
	}
	return out
}

// SetChange is Set servers' change: its review the servers as the change
// leaves them, hardn.yml's in place of the service's own, and the ones DHCP
// hands out, which stay. With no servers in hardn.yml there is nothing to
// set, and ok is false.
func (t TimeSync) SetChange() (ch Change, ok bool) {
	if len(t.Want) == 0 {
		return Change{}, false
	}
	var servers []TimeServer
	for _, name := range t.Want {
		servers = append(servers, TimeServer{Name: name, From: TimeFromHardn})
	}
	return Change{Step: WorkflowStep{Title: trail(timeTitle, timeSet)}, Question: timeAskSet,
		Review: flow.Review{Title: timeSection, Items: serverRows(append(servers, t.from(TimeFromDHCP)...))}}, true
}

// RemoveChange is Remove servers' change: its review the servers as the
// change leaves them: the ones hardn's took the place of, back (Restores),
// and the others that were beside hardn's; "None" with neither.
func (t TimeSync) RemoveChange() Change {
	left := append(slices.Clone(t.Restores), t.from(TimeFromFile, TimeFromOther, TimeFromDHCP, TimeFromBuiltIn)...)
	return Change{Step: WorkflowStep{Title: trail(timeTitle, timeRemove)}, Question: timeAskRemove,
		Review: flow.Review{Title: timeSection, Items: serverRows(left)}}
}
