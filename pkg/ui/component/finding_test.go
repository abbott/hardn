package component_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	. "github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestFindingMarker: a plan's finding (KeyValue.Finding) that is a warning
// leads with the warning's marker, ⭘ [ ], where there is no colour to tell
// it from a change's bullet, and with the bullet where there is (DEV-068,
// DEV-060); a finding of any other state, and a warning bullet item that is
// no finding, keep the bullet at every profile.
func TestFindingMarker(t *testing.T) {
	const text = "dhcpcd may rewrite /etc/resolv.conf on a lease"
	head := func(c Ctx, kv KeyValue) string {
		l := strings.SplitN(ansi.Strip(kv.Render(c)), "\n", 2)[0]
		return strings.TrimSpace(strings.TrimSuffix(l, text))
	}
	for _, cell := range uitest.Full() {
		c := NewCtx(cell.Env)
		g := c.Theme.Glyphs
		want := g.Bullet
		if !cell.Env.HasColor() {
			want = g.Warn
		}
		if got := head(c, KeyValue{Key: text, State: ValueWarn, Finding: true}); got != want {
			t.Errorf("%s: a warning finding leads with %q, want %q", cell.Name, got, want)
		}
		for _, kv := range []KeyValue{{Key: text, State: ValueWarn}, {Key: text, State: ValueNone, Finding: true}, {Key: text, Finding: true}} {
			if got := head(c, kv); got != g.Bullet {
				t.Errorf("%s: %+v leads with %q, want the bullet", cell.Name, kv, got)
			}
		}
		if lines := strings.Split(ansi.Strip(KeyValue{Key: text + " " + text, State: ValueWarn, Finding: true}.Render(c)), "\n"); len(lines) > 1 {
			if !strings.HasPrefix(lines[1], "      ") || strings.TrimSpace(lines[1]) == "" {
				t.Errorf("%s: a wrapped finding's next line %q, want it under the text at col 6", cell.Name, lines[1])
			}
		}
	}
}
