package screen

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// runAllTestRows are the rows as pkg/screens/runall fills them on alpine323,
// the longest refusal the gallery's hosts have; the hosts behind each are
// that package's goldens. Synthetic values only.
var runAllTestRows = []RunAllRow{{Label: "User", Outcome: "Already as asked"},
	{Label: "Firewall", Outcome: "Refused", Refusal: "hardn can't tell which ports sshd listens on: ss is not installed. Install iproute2."},
	{Label: "SSH Login", Outcome: "Root login off"}, {Label: "SSH Auth", Outcome: "Nothing asked"}, {Label: "SSH Port", Outcome: "Already as asked"},
	{Label: "AppArmor", Outcome: "Status only"}, {Label: "Auto Updates", Outcome: "Turn on"}, {Label: "DNS", Outcome: "Set 2 nameservers"},
	{Label: "Backups", Outcome: "Used, not changed"}, {Label: "Environment", Outcome: "Nothing asked"}}

// TestRunAllHeights (from the gallery's, gallery-runall.md §4): the screen
// at 80x24 and 80x40, Unicode and ASCII, fits 24 rows, the prompt on its
// last line, no line over 80 cells, in each of the item's states, the
// lockout line too. pkg/screens/runall's goldens hold every host's.
func TestRunAllHeights(t *testing.T) {
	for _, st := range []RunAllState{RunAllPlanned, RunAllNothing, RunAllSkipped, RunAllRefused, RunAllPending} {
		vm := RunAll{Rows: runAllTestRows, State: st, Lockout: true}.SubMenu()
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
				env.Height = rows
				lines := strings.Split(ansi.Strip(vm.Render(component.NewCtx(env))), "\n")
				if len(lines) > term.DefaultHeight || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("state %d at 80x%d, unicode %v: %d lines, the last %q; want at most %d, the prompt", st, rows, unicode, len(lines), lines[len(lines)-1], term.DefaultHeight)
				}
				for _, l := range lines {
					if w := ansi.StringWidth(l); w > 80 {
						t.Errorf("state %d at 80x%d: %d cells: %q", st, rows, w, l)
					}
				}
			}
		}
	}
}

// TestRunAllItem: the one item's description in each state (§6's words),
// unavailable only with nothing to change and nothing refused, marked only
// where a lockout-risk plan is planned.
func TestRunAllItem(t *testing.T) {
	for _, tc := range []struct {
		st             RunAllState
		about          string
		disabled, risk bool
	}{
		{RunAllPlanned, "Plan, review, confirm", false, true},
		{RunAllNothing, "Nothing to change", true, false},
		{RunAllSkipped, "Nothing to change", false, false},
		{RunAllRefused, "Refused", false, false},
		{RunAllPending, "Change pending", false, false},
	} {
		it := RunAll{Rows: runAllTestRows, State: tc.st, Lockout: true}.SubMenu().Menu.Items
		if len(it) != 1 || it[0].Key != RunAllItem || it[0].Label != "Apply all" || it[0].Description != tc.about || it[0].Disabled != tc.disabled || it[0].Risk != tc.risk {
			t.Errorf("state %d: %+v; want 1) Apply all, %q, unavailable %v, marked %v", tc.st, it, tc.about, tc.disabled, tc.risk)
		}
	}
}

// TestRunAllRows: each row its label and outcome alone; a refused row's
// words after "Refused:", in the row's note, which the list cuts.
func TestRunAllRows(t *testing.T) {
	vm := RunAll{Rows: runAllTestRows}.SubMenu()
	if len(vm.Sections) != 1 || vm.Sections[0].Header.Title != "Controls" || vm.Title.Title != "Run All" || len(vm.Status.Rows) != 0 {
		t.Fatalf("sections %+v, title %q, status %d rows; want Run All, one Controls section, no status", vm.Sections, vm.Title.Title, len(vm.Status.Rows))
	}
	items := vm.Sections[0].Rows.Items
	if items[1].Key != "Firewall" || items[1].Value != "Refused:" || items[1].Note != runAllTestRows[1].Refusal || items[0].Note != "" || items[0].Value != "Already as asked" {
		t.Errorf("rows %+v", items[:2])
	}
	out := ansi.Strip(vm.Render(component.NewCtx(term.Fixed(80, colorprofile.Ascii, false, true))))
	if !strings.Contains(out, "Refused: hardn can't tell") || strings.Contains(out, "Install iproute2.") {
		t.Errorf("the refusal is not cut to its row:\n%s", out)
	}
}

// TestRunAllApplyAll: the review grouped by control, the planned rows in
// order, under Run All; the question; the title bar Run All > Apply all.
func TestRunAllApplyAll(t *testing.T) {
	c := RunAllApplyAll([]RunAllRow{{Label: "User", Outcome: "Create carol, 1 key"}, {Label: "SSH Login", Outcome: "Root login off"}})
	if c.Question != "Apply this plan?" || c.Step.Title.Title != "Apply all" || strings.Join(c.Step.Title.Parents, ">") != "Run All" {
		t.Errorf("question %q, title %+v", c.Question, c.Step.Title)
	}
	if c.Review.Title != "Run All" || len(c.Review.Items) != 2 || c.Review.Items[0].Key != "User" || c.Review.Items[1].Value != "Root login off" {
		t.Errorf("review %+v", c.Review)
	}
}
