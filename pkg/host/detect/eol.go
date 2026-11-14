package detect

import "time"

// The end of a release's vendor support (docs/engine/gallery-eol.md,
// adopted 2026-11-13; docs/engine/screen-eol.md): the date is the matrix
// row's (Release.Ends), and whether it is near or past is counted from a
// day the caller gives, the host's clock. It is a notice alone: a release's
// Support, and so what hardn changes, never moves with the clock.

// EndNear is how early a release's end of support is near: this many days
// before its last day, or fewer (decision 3).
const EndNear = 90

// EndState is where a day stands against a release's end of support.
type EndState uint8

const (
	EndNone  EndState = iota // no date: no matrix row, or the vendor names none
	EndLater                 // more than EndNear days away
	EndSoon                  // EndNear days or fewer to the last day, that day included
	Ended                    // after the last day
)

// End is a release's end of support on one day.
type End struct {
	State EndState
	// Date is the matrix's date as written, "2027-05-01", or "2027-05"
	// where the vendor names a month alone; Words as a notice says it,
	// "2027-05-01" or "May 2027". Both "" for EndNone.
	Date, Words string
}

// Due reports an end of support to tell the user about: near, or past.
func (e End) Due() bool { return e.State == EndSoon || e.State == Ended }

// End is p's end of support on the day today, by today's own calendar
// date (its location's). A date the matrix writes in neither form is
// EndNone: nothing is claimed from it. It is pure.
func (p Platform) End(today time.Time) End {
	last, words, ok := lastDay(p.Ends)
	if !ok {
		return End{}
	}
	y, m, d := today.Date()
	left := int(last.Sub(time.Date(y, m, d, 0, 0, 0, 0, time.UTC)).Hours() / 24)
	e := End{State: EndLater, Date: p.Ends, Words: words}
	switch {
	case left < 0:
		e.State = Ended
	case left <= EndNear:
		e.State = EndSoon
	}
	return e
}

// lastDay is the last day of support a matrix date names, and the date in
// a notice's words: the day itself, or the last day of a month.
func lastDay(ends string) (last time.Time, words string, ok bool) {
	if t, err := time.Parse(time.DateOnly, ends); err == nil {
		return t, ends, true
	}
	if t, err := time.Parse("2006-01", ends); err == nil {
		return t.AddDate(0, 1, -1), t.Format("January 2006"), true
	}
	return time.Time{}, "", false
}

// Name is the distribution's name as hardn's words say it, "Rocky Linux";
// "" for one hardn does not name.
func (d Distro) Name() string {
	switch d {
	case DistroDebian:
		return "Debian"
	case DistroUbuntu:
		return "Ubuntu"
	case DistroAlpine:
		return "Alpine"
	case DistroProxmox:
		return "Proxmox VE"
	case DistroRocky:
		return "Rocky Linux"
	case DistroAlma:
		return "AlmaLinux"
	case DistroRHEL:
		return "RHEL"
	case DistroCentOS:
		return "CentOS Stream"
	case DistroOracle:
		return "Oracle Linux"
	}
	return ""
}

// Newest is the newest release of d that hardn supports, the first of its
// Supported rows; false where the matrix has none.
func Newest(d Distro) (Release, bool) {
	for _, r := range matrix {
		if r.Distro == d && r.Support == Supported {
			return r, true
		}
	}
	return Release{}, false
}

// Upgrade is the release to upgrade to from p (decision 5): the newest of
// its distribution that hardn supports; false where p is that release.
func (p Platform) Upgrade() (Release, bool) {
	r, ok := Newest(p.Distro)
	if !ok || r.Version == p.Version {
		return Release{}, false
	}
	return r, true
}
