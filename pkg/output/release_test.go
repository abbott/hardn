package output

import (
	"testing"

	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/version"
)

// TestReleaseOf: each state of the check as the main menu draws it; a tag
// that is not a version is never passed on.
func TestReleaseOf(t *testing.T) {
	for _, tc := range []struct {
		in   version.Release
		want screen.Release
	}{
		{version.Release{}, screen.Release{}},
		{version.Release{State: version.ReleaseCurrent}, screen.Release{State: screen.ReleaseCurrent}},
		{version.Release{State: version.ReleasePending}, screen.Release{State: screen.ReleasePending}},
		{version.Release{State: version.ReleaseFailed}, screen.Release{State: screen.ReleaseFailed}},
		{version.Release{State: version.ReleaseNewer, Latest: "v0.4.1"}, screen.Release{State: screen.ReleaseNewer, Latest: "v0.4.1"}},
		{version.Release{State: version.ReleaseNewer, Latest: "v99; rm"}, screen.Release{State: screen.ReleaseFailed}},
		{version.Release{State: version.ReleaseNewer, Latest: "v0.4.1\x1b[31m"}, screen.Release{State: screen.ReleaseFailed}},
		{version.Release{State: version.ReleaseNewer}, screen.Release{State: screen.ReleaseFailed}},
		{version.Release{State: version.ReleaseCurrent, Latest: "v0.4.1"}, screen.Release{State: screen.ReleaseCurrent}},
		{version.Release{State: 99}, screen.Release{}},
	} {
		if got := ReleaseOf(tc.in); got != tc.want {
			t.Errorf("%+v: %+v, want %+v", tc.in, got, tc.want)
		}
	}
}
