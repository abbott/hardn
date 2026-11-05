package screen

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// TestBackupScreen: the Backup screen's section and items for each kind
// of reading (decisions 1 (a), 2, 3, 4, 8 and 9): no status row; Backup
// State Enabled, or Disabled in status.warn with the muted line under it;
// Backup Path with its note, in the colour of Check's verdict; item 1
// naming the way it turns; item 4 the runs, or nothing when none were
// read; the decided words, and origin/main's prompt.
func TestBackupScreen(t *testing.T) {
	path := func(note string, s component.ValueState) component.KeyValue {
		return component.KeyValue{Key: "Backup Path", Value: "/var/backups/hardn", State: s, Note: note}
	}
	on := component.KeyValue{Key: "Backup State", Value: "Enabled", State: component.ValueOK}
	off := []component.KeyValue{{Key: "Backup State", Value: "Disabled", State: component.ValueWarn}, {Key: "Rollback backups are still taken", State: component.ValueNone}}
	tests := []struct {
		name       string
		b          Backup
		rows       []component.KeyValue
		turn, runs string
	}{
		{"on, not made", Backup{Enabled: true, Path: "/var/backups/hardn", Note: "Not created yet", RunsRead: true},
			[]component.KeyValue{on, path("Not created yet", component.ValueOK)}, "Turn backups off", "None yet"},
		{"off, one run", Backup{Path: "/var/backups/hardn", Note: "Root only", RunsRead: true, Runs: 1},
			append(slices.Clone(off), path("Root only", component.ValueOK)), "Turn backups on", "1 run"},
		{"on, three runs", Backup{Enabled: true, Path: "/var/backups/hardn", Note: "Root only", RunsRead: true, Runs: 3},
			[]component.KeyValue{on, path("Root only", component.ValueOK)}, "Turn backups off", "3 runs"},
		{"refused", Backup{Enabled: true, Path: "/var/backups/hardn", Note: "Unsafe", PathState: BackupPathRefused},
			[]component.KeyValue{on, path("Unsafe", component.ValueBad)}, "Turn backups off", ""},
		{"not told", Backup{Path: "/var/backups/hardn", Note: "Can't tell", PathState: BackupPathUnknown},
			append(slices.Clone(off), path("Can't tell", component.ValueUnknown)), "Turn backups on", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := tt.b.SubMenu()
			if vm.Title.Title != "Backup Settings" || len(vm.Status.Rows) != 0 || len(vm.Sections) != 1 || vm.Sections[0].Header.Title != "Backup Configuration" ||
				vm.Heading != "Select an option" || vm.Prompt != "Enter selection" || vm.Menu.Exit.Label != "Return" {
				t.Fatalf("the screen's frame: %+v", vm)
			}
			if got := vm.Sections[0].Rows.Items; !slices.Equal(got, tt.rows) {
				t.Errorf("rows %+v\nwant %+v", got, tt.rows)
			}
			want := []component.MenuItem{{Key: "1", Label: "Backup State", Description: tt.turn}, {Key: "2", Label: "Backup Path", Description: "Change backup path"},
				{Key: "3", Label: "Verify Directory", Description: "Check the path, writing nothing"}, {Key: "4", Label: "Backup Runs", Description: tt.runs}}
			if !slices.Equal(vm.Menu.Items, want) {
				t.Errorf("items %+v\nwant %+v", vm.Menu.Items, want)
			}
		})
	}
}

// TestBackupHeights: the screen, with backups on and off, the runs and a
// run, in their one form (a sub-menu has no compact form), at 80x24 and at
// 80x40, in Unicode and ASCII: as many lines at either height, the prompt
// on the last, within 24 rows; the screen's counts are the adopted mock's
// (gallery-backup.md §4).
func TestBackupHeights(t *testing.T) {
	runs := []BackupRun{
		{ID: "20261019T143000Z-5e6f7a8b", Taken: time.Date(2026, 10, 19, 14, 30, 0, 0, time.UTC), Files: []BackupFile{{Path: "/etc/hardn/hardn.yml", Size: 91, TakenAnyway: true}}},
		{ID: "20261018T091500Z-1a2b3c4d", Taken: time.Date(2026, 10, 18, 9, 15, 0, 0, time.UTC), Files: []BackupFile{{Path: "/etc/resolv.conf", Size: 77}, {Path: "/etc/hardn/hardn.yml", Size: 91}}},
		{ID: "20261017T083000Z-9c0d1e2f", Taken: time.Date(2026, 10, 17, 8, 30, 0, 0, time.UTC), Files: []BackupFile{{Path: "/etc/ssh/sshd_config.d/00-hardn.conf", Absent: true}}},
	}
	draws := map[string]struct {
		vm    SubMenu
		lines int
	}{
		"on":   {Backup{Enabled: true, Path: "/var/backups/hardn", Note: "Root only", RunsRead: true, Runs: 3}.SubMenu(), 17},
		"off":  {Backup{Path: "/var/backups/hardn", Note: "Not created yet", RunsRead: true}.SubMenu(), 18},
		"runs": {BackupRunsMenu(runs), 11},
		"run":  {runs[1].SubMenu(), 10},
	}
	for name, d := range draws {
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
				env.Height = rows
				lines := strings.Split(ansi.Strip(d.vm.Render(component.NewCtx(env))), "\n")
				if len(lines) != d.lines || len(lines) > 24 || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s at 80x%d, unicode %v: %d lines, the last %q; want %d, the prompt", name, rows, unicode, len(lines), lines[len(lines)-1], d.lines)
				}
			}
		}
	}
}

// TestBackupWorkflows: the flows' title bars, the Backup screen's as
// their parent, and their reviews: turning backups off, Disabled and the
// muted line that rollback backups are still taken (decision 8); turning
// them on, Enabled alone; a new path, its row as the screen draws one.
func TestBackupWorkflows(t *testing.T) {
	on, off := Backup{Enabled: true}, Backup{}
	for _, tt := range []struct {
		step  WorkflowStep
		title string
	}{{on.TurnStep(), "Turn Backups Off"}, {off.TurnStep(), "Turn Backups On"}, {on.PathStep(), "Change Backup Path"}} {
		if tt.step.Title.Title != tt.title || !slices.Equal(tt.step.Title.Parents, []string{"Backup Settings"}) {
			t.Errorf("step %+v, want %q under Backup Settings", tt.step, tt.title)
		}
	}
	if r := on.TurnReview(); r.Title != "Backup Configuration" || !slices.Equal(r.Items, []component.KeyValue{
		{Key: "Backup State", Value: "Disabled", State: component.ValueWarn}, {Key: "Rollback backups are still taken", State: component.ValueNone}}) {
		t.Errorf("turning backups off: %+v", r)
	}
	if r := off.TurnReview(); !slices.Equal(r.Items, []component.KeyValue{{Key: "Backup State", Value: "Enabled", State: component.ValueOK}}) {
		t.Errorf("turning backups on: %+v", r)
	}
	r := on.PathReview("/srv/backups/hardn", "Not created yet", BackupPathTaken)
	if want := (component.KeyValue{Key: "Backup Path", Value: "/srv/backups/hardn", State: component.ValueOK, Note: "Not created yet"}); r.Title != "Backup Configuration" || !slices.Equal(r.Items, []component.KeyValue{want}) {
		t.Errorf("a new path: %+v", r)
	}
}

// TestBackupRuns: Backup Runs, newest first as given, each when it was
// taken (UTC, to the minute; the id where the time is not known) and its
// files, "taken anyway" where a copy was taken with backups off; one run,
// read only, each path and what it was, Return alone.
func TestBackupRuns(t *testing.T) {
	runs := []BackupRun{
		{ID: "20261019T143000Z-5e6f7a8b", Taken: time.Date(2026, 10, 19, 14, 30, 59, 0, time.UTC),
			Files: []BackupFile{{Path: "/etc/hardn/hardn.yml", Size: 91, TakenAnyway: true}, {Path: "/etc/motd", Link: "/run/motd"}, {Path: "/etc/new", Absent: true}}},
		{ID: "elsewhere", Files: []BackupFile{{Path: "/etc/resolv.conf", Size: 77}}},
	}
	list := BackupRunsMenu(runs)
	want := []component.MenuItem{{Key: "1", Label: "2026-10-19 14:30", Description: "3 files, taken anyway"}, {Key: "2", Label: "elsewhere", Description: "1 file"}}
	if list.Title.Title != "Backup Runs" || list.Heading != "Select a run" || !slices.Equal(list.Menu.Items, want) || list.Menu.Exit.Label != "Return" {
		t.Errorf("the list: %+v", list)
	}
	run := runs[0].SubMenu()
	rows := []component.KeyValue{{Key: "/etc/hardn/hardn.yml", Value: "91 bytes", State: component.ValueWarn, Note: "taken anyway"},
		{Key: "/etc/motd", Value: "Link to /run/motd"}, {Key: "/etc/new", Value: "Not there before"}}
	if run.Title.Title != "Backup Run" || run.Heading != "" || len(run.Menu.Items) != 0 || len(run.Sections) != 1 ||
		run.Sections[0].Header.Title != "Run 20261019T143000Z-5e6f7a8b" || !slices.Equal(run.Sections[0].Rows.Items, rows) {
		t.Errorf("the run: %+v", run)
	}
	c := component.NewCtx(term.Fixed(80, colorprofile.NoTTY, false, true))
	if got := ansi.Strip(run.Render(c)); !strings.HasSuffix(got, "0) Return\n\n    > Enter selection [0 or q]:") {
		t.Errorf("the run's menu:\n%s", got)
	}
}

// TestBackupFigmaDrift: the frame's two copied status rows (decision 1,
// gallery-backup.md §3.2). Figma's frame, kept as the record of 05-BACKUP
// (backupFixture), draws them as Figma has them; the screen draws none,
// and says the state and the path once each in its section (1 (a)).
func TestBackupFigmaDrift(t *testing.T) {
	c := component.NewCtx(term.Fixed(80, colorprofile.TrueColor, false, true))
	figma := ansi.Strip(backupFixture().Render(c))
	for _, want := range []string{"Backups              Enabled UFW installed, deny policy", "SSH Port             2230/tcp non-default"} {
		if !strings.Contains(figma, want) {
			t.Errorf("Figma's frame lacks %q:\n%s", want, figma)
		}
	}
	got := ansi.Strip(Backup{Enabled: true, Path: "/var/backups/hardn", Note: "Root only", RunsRead: true}.SubMenu().Render(c))
	if strings.Contains(got, "UFW") || strings.Contains(got, "SSH Port") || strings.Contains(got, "Backups ") {
		t.Errorf("the screen keeps a status row:\n%s", got)
	}
	if strings.Count(got, "Enabled") != 1 || strings.Count(got, "/var/backups/hardn") != 1 {
		t.Errorf("the screen says the state or the path more than once:\n%s", got)
	}
}
