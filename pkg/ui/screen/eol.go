package screen

// EndOfSupport is the main menu's notice for a release whose vendor
// support ends soon or has ended (docs/engine/gallery-eol.md, adopted
// 2026-11-13; no Figma frame), filled by output.NoticeOf from the host's
// release and clock. It is an Alert with no route, MainMenu.Notice. Here
// with no colour and ASCII glyphs, ending soon, ended, and ended on a
// release hardn refuses to change:
//
//	# Warning: Alpine 3.22 support ends 2027-05-01
//	# Upgrade to Alpine 3.24.
//
//	# Warning: Alpine 3.22 support ended 2027-05-01
//	# Upgrade to Alpine 3.24.
//
//	# Warning: Alpine 3.21 support ended 2026-11-01
//	# Out of scope: hardn refuses every change.
//	# Upgrade to Alpine 3.24.
//
// Whether a release is near its date, or past it, is the caller's to say:
// this holds the words alone.
type EndOfSupport struct {
	// Release is the distribution and its release: "Alpine 3.22".
	Release string
	// Date is the day the vendor's support ends, as it is drawn:
	// "2027-05-01", or "May 2027" where the vendor names a month alone.
	Date string
	// Ended is whether Date has passed.
	Ended bool
	// Refused is whether hardn refuses every change on the release, as on
	// one out of scope.
	Refused bool
	// Upgrade is the release to upgrade to, "Alpine 3.24"; empty for none.
	Upgrade string
}

// The notice's words.
const (
	supportEnds  = " support ends "
	supportEnded = " support ended "
	// OutOfScopeRefused says why a change is refused, in the README's words.
	OutOfScopeRefused = "Out of scope: hardn refuses every change."
	upgradeTo         = "Upgrade to "
)

// Alert is the notice as the main menu draws it; nil for no release or no
// date.
func (e EndOfSupport) Alert() *Alert {
	if e.Release == "" || e.Date == "" {
		return nil
	}
	a := &Alert{Title: e.Release + supportEnds + e.Date}
	if e.Ended {
		a.Title = e.Release + supportEnded + e.Date
	}
	if e.Refused {
		a.Why = OutOfScopeRefused
	}
	if e.Upgrade != "" {
		a.Fix = upgradeTo + e.Upgrade + "."
	}
	return a
}

// The main menu's notice on TrueNAS, which hardn refuses to change
// (docs/engine/design-custom-distros.md, decision 1 (a)), drawn as the
// end-of-support notice is. Here with no colour and ASCII glyphs:
//
//	# Warning: TrueNAS manages its own system
//	# hardn refuses every change.
//	# Make them in TrueNAS's web UI.
const (
	TrueNASTitle = "TrueNAS manages its own system"
	TrueNASWhy   = "hardn refuses every change."
	TrueNASFix   = "Make them in TrueNAS's web UI."
)

// TrueNAS is the main menu's notice on TrueNAS.
func TrueNAS() *Alert { return &Alert{Title: TrueNASTitle, Why: TrueNASWhy, Fix: TrueNASFix} }
