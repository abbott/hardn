package screen

import (
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// The Environment screen (refactor plan slice 9, unit (c), next/s9-screen):
// origin/main's main menu 8 on the sub-menu layout, as the maintainer
// adopted its mock on 2026-10-29 (docs/engine/gallery-environment.md §5,
// decisions 1 to 7, with the words of §6), drawn from a reading of whether
// the elevation tool keeps HARDN_CONFIG for the user who ran hardn
// (Environment). pkg/ui cannot reach the host: pkg/screens/environment
// reads it, through the Environment control (pkg/engine/controls/
// environment), fills Environment and runs the items
// (docs/engine/screen-environment.md). There is no Figma frame, and no
// status row (decision 6 (a)).

// The Environment screen's words: origin/main's title
// (environment_settings_menu.go:34); Backup's heading and prompt (decided,
// P-29 (a)); the rest decided with decision 7 (gallery-environment.md §6;
// P-07 (c) for "Keep HARDN_CONFIG"). The state's words (Kept, Not kept,
// their notes, "Not run with sudo", "<file> may override hardn's rule")
// are the control's, which pkg/screens/environment fills in.
const (
	envTitle      = "Environment Variable Settings"
	envSection    = "HARDN_CONFIG"
	envUserKey    = "User"
	envValueKey   = "Value"
	envNotSet     = "Not set"
	envKeep       = "Keep HARDN_CONFIG"
	envStop       = "Stop keeping HARDN_CONFIG"
	envKeepTitle  = "Keep HARDN_CONFIG"
	envStopTitle  = "Stop Keeping HARDN_CONFIG"
	envGuideLabel = "Guide"
	envGuideAbout = "How to set HARDN_CONFIG"
)

// envGuide are item 2's words: origin/main's guide
// (environment_settings_menu.go:120-139) shortened, its "sudo hardn
// setup-sudo-env" now item 1, whose label names the tool (decided with
// decision 7).
var envGuide = []string{
	"HARDN_CONFIG names the hardn.yml hardn reads. Set it in your shell profile:",
	"export HARDN_CONFIG=/path/to/hardn.yml",
	"Then keep it with item 1.",
}

// The Environment screen's items, by key.
const (
	EnvironmentKeepItem  = "1" // Under sudo: keep HARDN_CONFIG, or stop keeping it
	EnvironmentGuideItem = "2" // Guide: how to set HARDN_CONFIG
)

// EnvironmentState is what the reading established of whether hardn's
// rules keep HARDN_CONFIG, by which the screen draws its rows.
type EnvironmentState uint8

const (
	// EnvironmentNotKept: read; neither hardn's files nor origin/main's
	// line keep it. The state row is in status.warn; item 1 keeps it.
	EnvironmentNotKept EnvironmentState = iota
	// EnvironmentKept: read; hardn's files keep it. The state row is in
	// status.ok, or status.warn where a file read after hardn's rule may
	// override it, each such file a line under it; item 1 stops keeping
	// it (decision 5 (a)).
	EnvironmentKept
	// EnvironmentKeptLegacy: read; origin/main's /etc/sudoers.d/<user>
	// keeps it, hardn's files do not (decision 3 (a)). The state row is in
	// status.ok; item 1 keeps it in hardn's own file.
	EnvironmentKeptLegacy
	// EnvironmentNotRun: no user ran hardn through the tool (a root login;
	// decision 2 (a)). The User row says so, in status.warn, and there is
	// no state row.
	EnvironmentNotRun
	// EnvironmentNoTool: neither sudo nor doas is installed. The state row
	// is Not Applicable, and there is no User row.
	EnvironmentNoTool
	// EnvironmentUnknown: the state was not read (the tool not
	// established, a file of hardn's edited by hand, a read that failed).
	// The state row is Unknown, under the User row where the user is
	// known.
	EnvironmentUnknown
)

// Environment is the Environment screen's reading of the host, which
// pkg/screens/environment fills from the Environment control's.
type Environment struct {
	// Tool is the elevation tool as the screen names it, "sudo" or "doas"
	// (the control's ToolName: "sudo" where neither is installed, decided
	// with decision 7, as drawn): the state row's key and item 1's label,
	// "Under sudo".
	Tool string
	// State is what the reading established.
	State EnvironmentState
	// User is who ran hardn through the tool; with State EnvironmentNotRun,
	// the control's words for no one ("Not run with sudo").
	User string
	// Word and Note are the state row's words where the state was read
	// (the control's Reading.Words): "Not kept" and "by hardn", "Kept" and
	// "by /etc/sudoers.d/alice".
	Word, Note string
	// Later are the files the tool reads after hardn's rule, each finding's
	// words ("/etc/doas.d/wheel.conf may override hardn's rule"): a line
	// each under a Kept row, and the review's state row in status.warn.
	Later []string
	// Value is HARDN_CONFIG as this hardn sees it; "" is not set.
	Value string
	// Refused: item 1 is refused before it asks. Risk: item 1's change is
	// lockout-risk; its item is marked so where it asks.
	Refused, Risk bool
}

// underKey is the state row's key and item 1's label: "Under sudo",
// "Under doas".
func (e Environment) underKey() string { return "Under " + e.Tool }

// SubMenu returns the Environment screen: the title bar "Environment
// Variable Settings"; no status row (decision 6 (a)); the section
// "HARDN_CONFIG", its rows by State: User, who ran hardn through the tool
// (in status.warn, "Not run with sudo", where no one did); "Under sudo" or
// "Under doas", hardn's rules keeping HARDN_CONFIG or not in the control's
// words, each file that may override hardn's rule under a Kept row, Not
// Applicable with neither tool, Unknown where it was not read; Value,
// HARDN_CONFIG as hardn sees it, or "Not set". Then item 1, labelled by
// the tool, keeping HARDN_CONFIG or, once hardn's files keep it, stopping,
// marked Lockout risk where it asks and its change is; item 2, the guide;
// Return; "Select an option"; origin/main's prompt, "Enter selection".
func (e Environment) SubMenu() SubMenu {
	about := envKeep
	if e.State == EnvironmentKept {
		about = envStop
	}
	return SubMenu{
		Title:    trail(envTitle),
		Sections: []Section{{Header: component.SectionHeader{Title: envSection}, Rows: component.BulletList{Items: e.rows()}}},
		Heading:  fixtureHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: EnvironmentKeepItem, Label: e.underKey(), Description: about, Risk: !e.Refused && e.Risk},
			{Key: EnvironmentGuideItem, Label: envGuideLabel, Description: envGuideAbout},
		}, Exit: returnItem()},
		Prompt: mainPrompt,
	}
}

// rows are the section's rows, by State.
func (e Environment) rows() []component.KeyValue {
	var rows []component.KeyValue
	switch e.State {
	case EnvironmentNotRun:
		rows = append(rows, component.KeyValue{Key: envUserKey, Value: e.User, State: component.ValueWarn})
	case EnvironmentNoTool:
		rows = append(rows, component.KeyValue{Key: e.underKey(), State: component.ValueNotApplicable})
	case EnvironmentUnknown:
		if e.User != "" {
			rows = append(rows, component.KeyValue{Key: envUserKey, Value: e.User})
		}
		rows = append(rows, component.KeyValue{Key: e.underKey(), State: component.ValueUnknown})
	default:
		under := e.stateRow(e.State == EnvironmentKept, e.Word, e.Note)
		if e.State == EnvironmentKeptLegacy {
			under.State = component.ValueOK
		}
		rows = append(rows, component.KeyValue{Key: envUserKey, Value: e.User}, under)
		if e.State == EnvironmentKept {
			for _, l := range e.Later {
				rows = append(rows, component.KeyValue{Key: l, State: component.ValueWarn})
			}
		}
	}
	value := component.KeyValue{Key: envValueKey, Value: e.Value}
	if e.Value == "" {
		value.Value = envNotSet
	}
	return append(rows, value)
}

// stateRow is the state row for hardn's files keeping HARDN_CONFIG or
// not, in word and note: Not kept in status.warn; Kept in status.ok, or
// status.warn where a file the tool reads after hardn's may override it,
// as what wins is not read (sudoers.md §6).
func (e Environment) stateRow(kept bool, word, note string) component.KeyValue {
	row := component.KeyValue{Key: e.underKey(), Value: word, Note: note, State: component.ValueOK}
	if !kept || len(e.Later) > 0 {
		row.State = component.ValueWarn
	}
	return row
}

// Step returns the screen above item 1's workflow: its title bar, "Keep
// HARDN_CONFIG", or "Stop Keeping HARDN_CONFIG" once hardn's files keep
// it (decided), the Environment screen's as its parent (shown only with
// TitleTrail).
func (e Environment) Step() WorkflowStep {
	if e.State == EnvironmentKept {
		return WorkflowStep{Title: trail(envTitle, envStopTitle)}
	}
	return WorkflowStep{Title: trail(envTitle, envKeepTitle)}
}

// Review is item 1's review, under "HARDN_CONFIG": the User row, and the
// state row as the change leaves it, in word and note (the control's
// Words of the way item 1 turns), in status.ok where it keeps it and no
// file read after hardn's may override it, else in status.warn. The flow
// ends it with the plan's findings.
func (e Environment) Review(word, note string) flow.Review {
	return flow.Review{Title: envSection, Items: []component.KeyValue{
		{Key: envUserKey, Value: e.User}, e.stateRow(e.State != EnvironmentKept, word, note)}}
}

// EnvironmentGuide is item 2, the guide: how to set HARDN_CONFIG, then
// keep it with item 1 (decided with decision 7).
func EnvironmentGuide() component.Messages {
	msgs := make(component.Messages, len(envGuide))
	for i, w := range envGuide {
		msgs[i] = component.Message{Text: w}
	}
	return msgs
}
