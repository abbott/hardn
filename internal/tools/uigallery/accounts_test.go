package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
)

// The Account Review mocks (accounts.go): a listing alone, the view's own
// tests in pkg/ui/screen.

// TestAccountsGolden pins each mock on the core set and under LANG=C.
func TestAccountsGolden(t *testing.T) { goldenListing(t, accountsListing) }

// TestAccountsMocks: every note opens with the source label, and no mock
// offers a change: the review screens have Return alone, and the menus add
// item 3 to User Management's two.
func TestAccountsMocks(t *testing.T) {
	for _, m := range accountsListing.mocks() {
		if note := accountsListing.note(m); !strings.HasPrefix(note, accountsLabel) {
			t.Errorf("%s: %q", m.name, note)
		}
	}
	for _, m := range accountsMocks() {
		vm := m.vm()
		want := 0
		if strings.HasSuffix(m.name, "menu") {
			want = 3
		}
		if len(vm.Menu.Items) != want {
			t.Errorf("%s: %d items, want %d", m.name, len(vm.Menu.Items), want)
		}
		if out := ansi.Strip(vm.Render(component.NewCtx(plainOff()))); strings.Contains(out, "...") || strings.Contains(out, "…") {
			t.Errorf("%s: a row is cut:\n%s", m.name, out)
		}
	}
}
