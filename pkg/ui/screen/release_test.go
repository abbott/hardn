package screen

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// TestRelease: a newer release and a failed check take the link's place in
// the Header, one line for one line; off, up to date, pending, or newer
// with no tag, the main menu is drawn as with no check at all.
func TestRelease(t *testing.T) {
	c := component.NewCtx(term.Fixed(80, colorprofile.NoTTY, false, false))
	plain := MainMenuFixture().Render(c)
	first := func(r Release) (string, string) {
		vm := MainMenuFixture()
		vm.Release = r
		head, rest, _ := strings.Cut(vm.Render(c), "\n")
		return head, rest
	}
	_, plainRest, _ := strings.Cut(plain, "\n")
	for _, tt := range []struct {
		name string
		r    Release
		want string
	}{
		{"off", Release{}, "hardn  v0.4.0              https://github.com/abbott/hardn"},
		{"current", Release{State: ReleaseCurrent}, "hardn  v0.4.0              https://github.com/abbott/hardn"},
		{"current ignores a tag", Release{State: ReleaseCurrent, Latest: "v0.4.0"}, "hardn  v0.4.0              https://github.com/abbott/hardn"},
		{"pending", Release{State: ReleasePending}, "hardn  v0.4.0              https://github.com/abbott/hardn"},
		{"newer with no tag", Release{State: ReleaseNewer}, "hardn  v0.4.0              https://github.com/abbott/hardn"},
		{"newer", Release{State: ReleaseNewer, Latest: "v0.4.1"}, "hardn  v0.4.0              v0.4.1 available"},
		{"failed", Release{State: ReleaseFailed}, "hardn  v0.4.0              Release check failed"},
	} {
		head, rest := first(tt.r)
		if ansi.Strip(head) != tt.want || rest != plainRest {
			t.Errorf("%s: header %q, want %q; rest unchanged %t", tt.name, head, tt.want, rest == plainRest)
		}
	}
}
