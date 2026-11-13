package engine

import (
	"time"

	"github.com/abbott/hardn/pkg/host/detect"
)

// CodeBestEffort is the finding of a plan made on a best-effort release
// (detect.BestEffort: Ubuntu 22.04 and Alpine 3.22): one its vendor
// still supports and hardn changes, with no promises (platform.go's
// BestEffort). It is stable
// once released; p3-output's registry holds it (P-41, decided by the
// maintainer on 2026-10-19: such releases are changed, with this warning
// in every plan; next/p3-confirm-cleanup).
const CodeBestEffort = "PLATFORM_BEST_EFFORT"

// CodeSupportEnds and CodeSupportEnded are the findings of a plan made on
// a release whose vendor support ends within detect.EndNear days, or has
// ended (docs/engine/gallery-eol.md, adopted 2026-11-13;
// docs/engine/screen-eol.md): the main menu's notice, on the plan. They
// are stable once released.
const (
	CodeSupportEnds  = "PLATFORM_SUPPORT_ENDS"
	CodeSupportEnded = "PLATFORM_SUPPORT_ENDED"
)

// platformFindings are the plan's findings of the platform p on the day
// today: on a best-effort release, CodeBestEffort, whatever the plan holds
// (P-41: every plan made on one); then, on a release whose support ends
// soon or has ended, at every support level, CodeSupportEnds or
// CodeSupportEnded (endFinding). The support gate is unchanged, and never
// reads the clock: out-of-scope, unlisted and unestablished releases are
// refused for a plan with changes (refuseApply, ErrUnsupported), and a
// release past its date is changed as before.
func platformFindings(p detect.Platform, today time.Time) []Finding {
	var out []Finding
	if p.Support == detect.BestEffort {
		detail := releaseName(p) + " is a best-effort release: its vendor still supports it, and hardn changes it but makes no promises for it"
		if p.Note != "" {
			detail += " (" + p.Note + ")"
		}
		out = append(out, Finding{Code: CodeBestEffort, Detail: detail})
	}
	return append(out, endFinding(p, today)...)
}

// endFinding is the end-of-support finding of p on the day today, none
// where the date is further off or there is none: "Alpine 3.22 support
// ends 2027-05-01: upgrade to Alpine 3.24", the notice's words
// (screen.EndOfSupport) on one line. Every word is hardn's own: the name
// is detect's and the version the matrix row's, never the host's text.
func endFinding(p detect.Platform, today time.Time) []Finding {
	end := p.End(today)
	if !end.Due() {
		return nil
	}
	code, verb := CodeSupportEnds, " support ends "
	if end.State == detect.Ended {
		code, verb = CodeSupportEnded, " support ended "
	}
	detail := releaseName(p) + verb + end.Words
	if r, ok := p.Upgrade(); ok {
		detail += ": upgrade to " + r.Distro.Name() + " " + r.Version
	}
	return []Finding{{Code: code, Detail: detail}}
}

// releaseName is p's distribution and release in a finding's words,
// "Ubuntu 22.04".
func releaseName(p detect.Platform) string {
	name := p.Distro.Name()
	if name == "" {
		name = string(p.Distro)
	}
	return name + " " + p.Version
}
