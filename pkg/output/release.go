package output

import (
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/version"
)

// ReleaseOf is the main menu's release line from what the release check
// knows (version.Checker.Release): a newer release with its tag, a failed
// check, or nothing drawn (off, up to date, not answered yet). The tag is
// drawn only where it passes version.ParseTag again, so nothing but a
// version ever reaches the header. It is pure.
func ReleaseOf(r version.Release) screen.Release {
	switch r.State {
	case version.ReleaseNewer:
		if _, ok := version.ParseTag(r.Latest); !ok {
			return screen.Release{State: screen.ReleaseFailed}
		}
		return screen.Release{State: screen.ReleaseNewer, Latest: r.Latest}
	case version.ReleaseCurrent:
		return screen.Release{State: screen.ReleaseCurrent}
	case version.ReleasePending:
		return screen.Release{State: screen.ReleasePending}
	case version.ReleaseFailed:
		return screen.Release{State: screen.ReleaseFailed}
	}
	return screen.Release{}
}
