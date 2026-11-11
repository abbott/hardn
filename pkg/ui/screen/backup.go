package screen

import (
	"fmt"
	"slices"
	"time"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// The Backup screen (refactor plan slice 3, unit (c), next/s3-screen):
// Figma's 05-BACKUP on the sub-menu layout, as the maintainer adopted its
// mock on 2026-10-27 (docs/engine/gallery-backup.md §5, decisions 1 to 4,
// 8 and 9, with the words they name), drawn from a reading of the backup
// settings and the runs under the path (Backup). pkg/ui cannot reach the
// host: pkg/screens/backup reads it, through Backup's control and
// pkg/host/backup, fills Backup and runs the items
// (docs/engine/screen-backup.md). Its frame keeps no status row (decision
// 1 (a)): the two Figma draws are other screens' (backupFixture).

// The Backup screen's words: decided (decisions 2, 4, 8 and 9: the title,
// the section, its rows and their values, the muted line, the item
// labels and descriptions but item 4's); item 4's descriptions, the
// flows' title bars and the runs' words decided 2026-10-27
// (screen-backup.md §5).
const (
	backupTitle       = "Backup Settings"
	backupSection     = "Backup Configuration"
	backupStateKey    = "Backup State"
	backupPathKey     = "Backup Path"
	backupEnabled     = "Enabled"
	backupDisabled    = "Disabled"
	backupRollback    = "Rollback backups are still taken"
	backupRunsTitle   = "Backup Runs"
	backupRunTitle    = "Backup Run"
	backupTakenAnyway = "taken anyway"
)

// The Backup screen's items, by key (decisions 2 and 9).
const (
	BackupStateItem  = "1" // Backup State: backups turned off, or on
	BackupPathItem   = "2" // Backup Path: another path
	BackupVerifyItem = "3" // Verify Directory: Check, writing nothing
	BackupRunsItem   = "4" // Backup Runs, read only
)

// BackupPathState is what Check made of the backup path, as the path's
// row says it.
type BackupPathState uint8

const (
	BackupPathTaken   BackupPathState = iota // Check takes it: status.ok
	BackupPathRefused                        // Check refuses it: status.bad
	BackupPathUnknown                        // Check could not tell: Unknown's marker
)

// Backup is the Backup screen's reading of the backup settings and of the
// runs under the path, which pkg/screens/backup fills.
type Backup struct {
	// Enabled is enableBackups.
	Enabled bool
	// Path is backupPath, an empty one the default, as the engine takes
	// it.
	Path string
	// Note is the path's note, what Check found (decision 3, decided):
	// "Root only", "Not created yet", "Unsafe", "Not absolute", "Not a
	// directory", "Can't tell".
	Note string
	// PathState is Check's verdict on the path.
	PathState BackupPathState
	// Runs are how many backup runs are under the path; RunsRead is false
	// when none could be read (the path refused, or not listed), and item
	// 4 then says nothing of them.
	Runs     int
	RunsRead bool
}

// SubMenu returns the Backup screen: the title bar "Backup Settings"; no
// status row (decision 1 (a)); the section "Backup Configuration", Backup
// State Enabled, or Disabled in status.warn with the muted line under it
// that rollback backups are still taken (decision 8), and Backup Path with
// its note (decision 3); then items 1 to 4 and Return, item 1 naming the
// way it turns (decision 4), item 4 the runs (decision 9); "Select an
// option"; the prompt, "Option" (P-29 (a)).
func (b Backup) SubMenu() SubMenu {
	rows := []component.KeyValue{{Key: backupStateKey, Value: backupEnabled, State: component.ValueOK}}
	turn := "Turn backups off"
	if !b.Enabled {
		rows = []component.KeyValue{{Key: backupStateKey, Value: backupDisabled, State: component.ValueWarn}, {Key: backupRollback, State: component.ValueNone}}
		turn = "Turn backups on"
	}
	rows = append(rows, b.pathRow(b.Path, b.Note, b.PathState))
	runs := ""
	switch {
	case !b.RunsRead:
	case b.Runs == 0:
		runs = "None yet"
	case b.Runs == 1:
		runs = "1 run"
	default:
		runs = fmt.Sprintf("%d runs", b.Runs)
	}
	return SubMenu{
		Title:    trail(backupTitle),
		Sections: []Section{{Header: component.SectionHeader{Title: backupSection}, Rows: component.BulletList{Items: rows}}},
		Heading:  fixtureHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: BackupStateItem, Label: backupStateKey, Description: turn},
			{Key: BackupPathItem, Label: backupPathKey, Description: "Change backup path"},
			{Key: BackupVerifyItem, Label: "Verify Directory", Description: "Check the path, writing nothing"},
			{Key: BackupRunsItem, Label: backupRunsTitle, Description: runs},
		}, Exit: returnItem()},
		Prompt: menuPrompt,
	}
}

// pathRow is the Backup Path row of path, its note and Check's verdict.
func (Backup) pathRow(path, note string, s BackupPathState) component.KeyValue {
	kv := component.KeyValue{Key: backupPathKey, Value: path, State: component.ValueOK, Note: note}
	switch s {
	case BackupPathRefused:
		kv.State = component.ValueBad
	case BackupPathUnknown:
		kv.State = component.ValueUnknown
	}
	return kv
}

// TurnStep returns the screen above item 1's workflow: its title bar,
// "Turn Backups Off" or "Turn Backups On" (decided), the Backup screen's
// as its parent (shown only with TitleTrail).
func (b Backup) TurnStep() WorkflowStep {
	if b.Enabled {
		return WorkflowStep{Title: trail(backupTitle, "Turn Backups Off")}
	}
	return WorkflowStep{Title: trail(backupTitle, "Turn Backups On")}
}

// TurnReview is item 1's review, under "Backup Configuration": Backup
// State as the change leaves it; turning backups off, the muted line that
// rollback backups are still taken (decision 8). The flow ends it with the
// plan's findings.
func (b Backup) TurnReview() flow.Review {
	rows := []component.KeyValue{{Key: backupStateKey, Value: backupEnabled, State: component.ValueOK}}
	if b.Enabled {
		rows = []component.KeyValue{{Key: backupStateKey, Value: backupDisabled, State: component.ValueWarn}, {Key: backupRollback, State: component.ValueNone}}
	}
	return flow.Review{Title: backupSection, Items: rows}
}

// PathStep returns the screen above item 2's workflow: its title bar,
// "Change Backup Path" (decided).
func (Backup) PathStep() WorkflowStep {
	return WorkflowStep{Title: trail(backupTitle, "Change Backup Path")}
}

// PathReview is item 2's review, under "Backup Configuration": Backup Path
// as the change leaves it, path, with its note and Check's verdict on it.
func (b Backup) PathReview(path, note string, s BackupPathState) flow.Review {
	return flow.Review{Title: backupSection, Items: []component.KeyValue{b.pathRow(path, note, s)}}
}

// BackupRun is one backup run under the path, as Backup Runs lists it
// (decision 9, read only).
type BackupRun struct {
	// ID is the run's id, its directory's name under the path.
	ID string
	// Taken is when the run was taken, read from its id; the zero time
	// when the id gives none, and the list then names the run by its id.
	Taken time.Time
	// Files are the paths the run backed up, in the order taken.
	Files []BackupFile
}

// BackupFile is one path a run backed up: what it was before the change.
type BackupFile struct {
	Path string
	// Absent: the path was not there before the change.
	Absent bool
	// Link is a symlink's target, as stored; "" for a file.
	Link string
	// Size is a file's size, in bytes.
	Size int64
	// TakenAnyway: taken although enableBackups was false, as the
	// rollback needs it (§3.16).
	TakenAnyway bool
}

// label is the run's item in the list: when it was taken, UTC, to the
// minute, or its id.
func (r BackupRun) label() string {
	if r.Taken.IsZero() {
		return r.ID
	}
	return r.Taken.UTC().Format("2006-01-02 15:04")
}

// about is the run's description in the list: how many files, and
// whether a copy was taken with backups off.
func (r BackupRun) about() string {
	s := fmt.Sprintf("%d files", len(r.Files))
	if len(r.Files) == 1 {
		s = "1 file"
	}
	if slices.ContainsFunc(r.Files, func(f BackupFile) bool { return f.TakenAnyway }) {
		s += ", " + backupTakenAnyway
	}
	return s
}

// BackupRunsMenu returns Backup Runs (decision 9): the title bar "Backup
// Runs", "Select an option", one item per run of runs, in their order (newest
// first, as pkg/screens/backup lists them), each when it was taken and
// its files; Return; the prompt.
func BackupRunsMenu(runs []BackupRun) SubMenu {
	items := make([]component.MenuItem, len(runs))
	for i, r := range runs {
		items[i] = component.MenuItem{Key: fmt.Sprint(i + 1), Label: r.label(), Description: r.about()}
	}
	return SubMenu{
		Title:   trail(backupTitle, backupRunsTitle),
		Heading: fixtureHeading,
		Menu:    component.Menu{Items: items, Exit: returnItem()},
		Prompt:  menuPrompt,
	}
}

// SubMenu returns one run, read only: the title bar "Backup Run"; the
// section "Run <id>", each path it backed up and what it was, "<n>
// bytes", "Not there before" or "Link to <target>", one taken with
// backups off noted "taken anyway" in status.warn; Return alone, with no
// heading, and the prompt. The restore is a later unit's (decision 9).
func (r BackupRun) SubMenu() SubMenu {
	rows := make([]component.KeyValue, len(r.Files))
	for i, f := range r.Files {
		kv := component.KeyValue{Key: f.Path}
		switch {
		case f.Absent:
			kv.Value = "Not there before"
		case f.Link != "":
			kv.Value = "Link to " + f.Link
		default:
			kv.Value = fmt.Sprintf("%d bytes", f.Size)
		}
		if f.TakenAnyway {
			kv.State, kv.Note = component.ValueWarn, backupTakenAnyway
		}
		rows[i] = kv
	}
	return SubMenu{
		Title:    trail(backupTitle, backupRunsTitle, backupRunTitle),
		Sections: []Section{{Header: component.SectionHeader{Title: "Run " + r.ID}, Rows: component.BulletList{Items: rows}}},
		Menu:     component.Menu{Exit: returnItem()},
		Prompt:   menuPrompt,
	}
}
