package screen

import "github.com/abbott/hardn/pkg/ui/component"

// ReleaseState is what the release check knows.
type ReleaseState int

// The states. Only ReleaseNewer and ReleaseFailed draw a line.
const (
	// ReleaseOff: the check is off, hardn.yml's default. Nothing is drawn.
	ReleaseOff ReleaseState = iota
	// ReleaseCurrent: this hardn is the latest release. Nothing is drawn.
	ReleaseCurrent
	// ReleasePending: the check is on and has not answered yet. Nothing
	// is drawn.
	ReleasePending
	// ReleaseNewer: a newer release exists, Release.Latest.
	ReleaseNewer
	// ReleaseFailed: the last check got no answer.
	ReleaseFailed
)

// The notices' words.
const (
	releaseAvailable = " available"
	// ReleaseCheckFailed is the notice of a check that got no answer.
	ReleaseCheckFailed = "Release check failed"
)

// Release is the release check's one line on the main menu (proposal
// P-15; docs/engine/screen-release-check.md), drawn from hardn's check
// (version.Checker, through output.ReleaseOf) where hardn.yml's
// checkReleases is on. It is a notice in the Header, the line under the
// repository link (docs/engine/gallery-release-notice.md, decision 9), so
// the menu is one line taller while it shows, and nothing waits on it:
//
//	hardn  v0.4.0              https://github.com/abbott/hardn
//	                           v0.4.1 available
//
//	hardn  v0.4.0              https://github.com/abbott/hardn
//	                           Release check failed
//
// A newer release is in the primary text colour and a failed check muted.
// Off, up to date or not yet answered, the Header draws its link as ever.
// The compact form has no Header, and so no notice.
type Release struct {
	// State is what the check knows.
	State ReleaseState
	// Latest is the newer release's tag, such as "v0.4.1", for
	// ReleaseNewer; with none, nothing is drawn.
	Latest string
}

// header returns h with r's notice, if it has one.
func (r Release) header(h component.Header) component.Header {
	switch {
	case r.State == ReleaseNewer && r.Latest != "":
		h.Notice, h.NoticeBelow = r.Latest+releaseAvailable, true
	case r.State == ReleaseFailed:
		h.Notice, h.NoticeMuted, h.NoticeBelow = ReleaseCheckFailed, true, true
	}
	return h
}
