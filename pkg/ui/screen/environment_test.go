package screen

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The Environment screen's readings the tests draw, one per state, as the
// gallery's hosts read (gallery-environment.md §3.1): alice ran sudo
// hardn, or doas hardn; the words are the control's.
var (
	envDebian = Environment{Tool: "sudo", State: EnvironmentNotKept, User: "alice", Word: "Not kept", Note: "by hardn"}
	envKept   = Environment{Tool: "sudo", State: EnvironmentKept, User: "alice", Word: "Kept", Note: "by hardn", Value: "/home/alice/hardn.yml"}
	envLegacy = Environment{Tool: "sudo", State: EnvironmentKeptLegacy, User: "alice", Word: "Kept", Note: "by /etc/sudoers.d/alice", Value: "/home/alice/hardn.yml"}
	envDoas   = Environment{Tool: "doas", State: EnvironmentNotKept, User: "alice", Word: "Not kept", Note: "by hardn", Refused: true}
	envOver   = Environment{Tool: "doas", State: EnvironmentKept, User: "alice", Word: "Kept", Note: "by hardn",
		Later: []string{"/etc/doas.d/wheel.conf may override hardn's rule"}, Risk: true}
	envRoot   = Environment{Tool: "sudo", State: EnvironmentNotRun, User: "Not run with sudo", Refused: true}
	envNoTool = Environment{Tool: "sudo", State: EnvironmentNoTool, Refused: true}
	envEdited = Environment{Tool: "sudo", State: EnvironmentUnknown, User: "alice", Refused: true}
)

// TestEnvironmentScreen: the Environment screen's section and items for
// each kind of reading (decisions 1 (a), 2 (a), 3 (a), 5 (a), 6 (a), 7):
// no status row; the User row, or "Not run with sudo" in status.warn;
// the state row in the control's words, Not kept in status.warn, Kept in
// status.ok or, with a file that may override hardn's rule, status.warn
// and the file under it, Kept by origin/main's file in status.ok, Not
// Applicable, Unknown; Value or "Not set"; item 1 labelled by the tool,
// keeping or stopping, Lockout risk where it asks and its plan is; the
// guide; the decided words, and origin/main's prompt.
func TestEnvironmentScreen(t *testing.T) {
	user := component.KeyValue{Key: "User", Value: "alice"}
	notSet := component.KeyValue{Key: "Value", Value: "Not set"}
	kept := component.KeyValue{Key: "Value", Value: "/home/alice/hardn.yml"}
	tests := []struct {
		name string
		e    Environment
		rows []component.KeyValue
		item component.MenuItem
	}{
		{"not kept", envDebian, []component.KeyValue{user, {Key: "Under sudo", Value: "Not kept", Note: "by hardn", State: component.ValueWarn}, notSet},
			component.MenuItem{Key: "1", Label: "Under sudo", Description: "Keep HARDN_CONFIG"}},
		{"kept", envKept, []component.KeyValue{user, {Key: "Under sudo", Value: "Kept", Note: "by hardn", State: component.ValueOK}, kept},
			component.MenuItem{Key: "1", Label: "Under sudo", Description: "Stop keeping HARDN_CONFIG"}},
		{"legacy", envLegacy, []component.KeyValue{user, {Key: "Under sudo", Value: "Kept", Note: "by /etc/sudoers.d/alice", State: component.ValueOK}, kept},
			component.MenuItem{Key: "1", Label: "Under sudo", Description: "Keep HARDN_CONFIG"}},
		{"doas, refused", envDoas, []component.KeyValue{user, {Key: "Under doas", Value: "Not kept", Note: "by hardn", State: component.ValueWarn}, notSet},
			component.MenuItem{Key: "1", Label: "Under doas", Description: "Keep HARDN_CONFIG"}},
		{"overridden", envOver, []component.KeyValue{user, {Key: "Under doas", Value: "Kept", Note: "by hardn", State: component.ValueWarn},
			{Key: "/etc/doas.d/wheel.conf may override hardn's rule", State: component.ValueWarn}, notSet},
			component.MenuItem{Key: "1", Label: "Under doas", Description: "Stop keeping HARDN_CONFIG", Risk: true}},
		{"root login", envRoot, []component.KeyValue{{Key: "User", Value: "Not run with sudo", State: component.ValueWarn}, notSet},
			component.MenuItem{Key: "1", Label: "Under sudo", Description: "Keep HARDN_CONFIG"}},
		{"no tool", envNoTool, []component.KeyValue{{Key: "Under sudo", State: component.ValueNotApplicable}, notSet},
			component.MenuItem{Key: "1", Label: "Under sudo", Description: "Keep HARDN_CONFIG"}},
		{"unknown", envEdited, []component.KeyValue{user, {Key: "Under sudo", State: component.ValueUnknown}, notSet},
			component.MenuItem{Key: "1", Label: "Under sudo", Description: "Keep HARDN_CONFIG"}},
		{"unknown, no user", Environment{Tool: "sudo", State: EnvironmentUnknown, Refused: true}, []component.KeyValue{{Key: "Under sudo", State: component.ValueUnknown}, notSet},
			component.MenuItem{Key: "1", Label: "Under sudo", Description: "Keep HARDN_CONFIG"}},
		{"later, not kept", Environment{Tool: "doas", State: EnvironmentNotKept, User: "alice", Word: "Not kept", Note: "by hardn", Later: envOver.Later, Risk: true},
			[]component.KeyValue{user, {Key: "Under doas", Value: "Not kept", Note: "by hardn", State: component.ValueWarn}, notSet},
			component.MenuItem{Key: "1", Label: "Under doas", Description: "Keep HARDN_CONFIG", Risk: true}},
		{"risk, refused", Environment{Tool: "doas", State: EnvironmentKept, User: "alice", Word: "Kept", Note: "by hardn", Refused: true, Risk: true},
			[]component.KeyValue{user, {Key: "Under doas", Value: "Kept", Note: "by hardn", State: component.ValueOK}, notSet},
			component.MenuItem{Key: "1", Label: "Under doas", Description: "Stop keeping HARDN_CONFIG"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := tt.e.SubMenu()
			if vm.Title.Title != "Environment Variable Settings" || len(vm.Title.Parents) != 0 || len(vm.Status.Rows) != 0 || len(vm.Sections) != 1 ||
				vm.Sections[0].Header.Title != "HARDN_CONFIG" || vm.Heading != "Select an option" || vm.Prompt != "Option" ||
				vm.Menu.Exit != (component.MenuItem{Key: "0", Label: "Return"}) {
				t.Fatalf("the screen's frame: %+v", vm)
			}
			if got := vm.Sections[0].Rows.Items; !slices.Equal(got, tt.rows) {
				t.Errorf("rows %+v\nwant %+v", got, tt.rows)
			}
			want := []component.MenuItem{tt.item, {Key: "2", Label: "Guide", Description: "How to set HARDN_CONFIG"}}
			if !slices.Equal(vm.Menu.Items, want) {
				t.Errorf("items %+v\nwant %+v", vm.Menu.Items, want)
			}
		})
	}
}

// TestEnvironmentHeights: the screen in its one form (a sub-menu has
// none other), at 80x24 and at 80x40, in Unicode and ASCII: as many lines
// at either height, the prompt on the last, within 24 rows; the counts
// are the adopted mock's (gallery-environment.md §4): 15 with no User row
// or no state row, 16 with the three rows, 18 with a may-override line
// and a Lockout risk line.
func TestEnvironmentHeights(t *testing.T) {
	draws := map[string]struct {
		e     Environment
		lines int
	}{
		"not kept": {envDebian, 16}, "kept": {envKept, 16}, "legacy": {envLegacy, 16}, "doas": {envDoas, 16},
		"overridden": {envOver, 18}, "root login": {envRoot, 15}, "no tool": {envNoTool, 15}, "unknown": {envEdited, 16},
	}
	for name, d := range draws {
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
				env.Height = rows
				lines := strings.Split(ansi.Strip(d.e.SubMenu().Render(component.NewCtx(env))), "\n")
				if len(lines) != d.lines || len(lines) > 24 || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s at 80x%d, unicode %v: %d lines, the last %q; want %d, the prompt", name, rows, unicode, len(lines), lines[len(lines)-1], d.lines)
				}
			}
		}
	}
}

// TestEnvironmentWorkflows: item 1's title bar, keeping or stopping, the
// Environment screen's as its parent, and its review: the User row and
// the state row as the change leaves it, in status.ok where kept and no
// file may override hardn's rule, else in status.warn.
func TestEnvironmentWorkflows(t *testing.T) {
	for _, tt := range []struct {
		e     Environment
		title string
	}{{envDebian, "Keep HARDN_CONFIG"}, {envLegacy, "Keep HARDN_CONFIG"}, {envKept, "Stop Keeping HARDN_CONFIG"}, {envOver, "Stop Keeping HARDN_CONFIG"}} {
		if s := tt.e.Step(); s.Title.Title != tt.title || !slices.Equal(s.Title.Parents, []string{"Environment Variable Settings"}) {
			t.Errorf("step %+v, want %q under Environment Variable Settings", s, tt.title)
		}
	}
	user := component.KeyValue{Key: "User", Value: "alice"}
	for _, tt := range []struct {
		name       string
		e          Environment
		word, note string
		state      component.ValueState
	}{
		{"keep", envDebian, "Kept", "by hardn", component.ValueOK},
		{"keep, legacy", envLegacy, "Kept", "by hardn", component.ValueOK},
		{"stop", envKept, "Not kept", "by hardn", component.ValueWarn},
		{"stop, overridden", envOver, "Not kept", "by hardn", component.ValueWarn},
		{"keep, a later file", Environment{Tool: "doas", State: EnvironmentNotKept, User: "alice", Later: envOver.Later}, "Kept", "by hardn", component.ValueWarn},
	} {
		r := tt.e.Review(tt.word, tt.note)
		want := []component.KeyValue{user, {Key: "Under " + tt.e.Tool, Value: tt.word, Note: tt.note, State: tt.state}}
		if r.Title != "HARDN_CONFIG" || !slices.Equal(r.Items, want) || r.Notice != nil {
			t.Errorf("%s: review %+v, want %+v", tt.name, r, want)
		}
	}
}

// TestEnvironmentGuide: item 2's words, decided with decision 7, each a
// line of its own.
func TestEnvironmentGuide(t *testing.T) {
	want := []string{"HARDN_CONFIG names the hardn.yml hardn reads. Set it in your shell profile:", "export HARDN_CONFIG=/path/to/hardn.yml", "Then keep it with item 1."}
	g := EnvironmentGuide()
	if len(g) != len(want) {
		t.Fatalf("%d lines, want %d", len(g), len(want))
	}
	for i, m := range g {
		if m.Text != want[i] || m.Kind != component.MessageInfo {
			t.Errorf("line %d: %+v, want %q", i+1, m, want[i])
		}
	}
	env := term.Fixed(200, colorprofile.NoTTY, true, true)
	if got := ansi.Strip(g.Render(component.NewCtx(env))); strings.Join(strings.Fields(got), " ") != strings.Join(strings.Fields(strings.Join(want, " ")), " ") {
		t.Errorf("rendered %q", got)
	}
}
