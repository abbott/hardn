package backup_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/output"
	backupscreen "github.com/abbott/hardn/pkg/screens/backup"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The words the tests look for: decided ones (gallery-backup.md §5, §6;
// control-backup.md §6, §7) and the screen's proposed ones.
const (
	unsafeWords   = "Others can change /tmp, so backups can't go in /tmp/hardn-backups. Choose a directory only root can change."
	relativeWords = `"backups" is not an absolute path. Start it with /.`
	fileWords     = "/var/backups/hardn is not a directory. Choose another path."
	underWords    = "/etc/os-release is not a directory. Choose another path."
	unknownWords  = "Can't check /var/backups/hardn: backup: lstat /var/backups: permission denied."
	notLoaded     = "No hardn.yml was read, so there is none to save to. Create /etc/hardn/hardn.yml, then try again."
	anyway        = "Backed up anyway: the rollback needs it."
	written       = "Nothing was written."
)

// TestScreen: the screen on each host, as read (decisions 1 (a), 3, 4, 8,
// 9): Backup State, with backups off the muted line under it; Backup Path
// and Check's note; item 1 naming the way it turns; item 4 the runs read,
// or nothing where none could be. Drawing reads only, live and under plan
// only (decision 3): nothing written, no engine made, nothing planned.
func TestScreen(t *testing.T) {
	tests := []struct {
		host, state, path, turn, runs string
		rollback                      bool
	}{
		{hostDefault, "Enabled", "/var/backups/hardn Not created yet", "Turn backups off", "None yet", false},
		{hostOff, "Disabled", "/var/backups/hardn Not created yet", "Turn backups on", "None yet", true},
		{hostMade, "Enabled", "/var/backups/hardn Root only", "Turn backups off", "None yet", false},
		{hostElsewhere, "Enabled", "/srv/backups/hardn Root only", "Turn backups off", "None yet", false},
		{hostUnsafe, "Enabled", "/tmp/hardn-backups Unsafe", "Turn backups off", "", false},
		{hostRelative, "Enabled", "backups Not absolute", "Turn backups off", "", false},
		{hostFile, "Enabled", "/var/backups/hardn Not a directory", "Turn backups off", "", false},
		{hostUnderFile, "Enabled", "/etc/os-release/backups Not a dire...", "Turn backups off", "", false}, // cut at 64 columns
		{hostUnknown, "Enabled", "/var/backups/hardn Can't tell", "Turn backups off", "", false},
		{hostReadOnly, "Enabled", "/var/backups/hardn Not created yet", "Turn backups off", "None yet", false},
		{hostRuns, "Enabled", "/var/backups/hardn Root only", "Turn backups off", "3 runs", false},
		{hostSomeRuns, "Enabled", "/var/backups/hardn Root only", "Turn backups off", "3 runs", false},
	}
	for _, tt := range tests {
		for _, dryRun := range []bool{false, true} {
			h, cfg := newHost(t, tt.host)
			n := len(h.Calls())
			p := play(h, cfg, dryRun, "0")
			s := screens(p.out)
			if p.err != nil || p.unread != 0 || len(s) != 1 {
				t.Fatalf("%s: Run = %v, %d unread, %d screens:\n%s", tt.host, p.err, p.unread, len(s), p.out)
			}
			if got := [3]string{row(s[0], "Backup State"), row(s[0], "Backup Path"), item(s[0], "1")}; got != [3]string{tt.state, tt.path, tt.turn} {
				t.Errorf("%s: rows and item 1 %q, want %q, %q, %q", tt.host, got, tt.state, tt.path, tt.turn)
			}
			if got := item(s[0], "4"); got != tt.runs {
				t.Errorf("%s: item 4 %q, want %q", tt.host, got, tt.runs)
			}
			if strings.Contains(s[0], "Rollback backups are still taken") != tt.rollback {
				t.Errorf("%s: the rollback line shown %t:\n%s", tt.host, !tt.rollback, s[0])
			}
			if strings.Contains(s[0], "UFW") || strings.Contains(s[0], "SSH Port") || !strings.Contains(p.out, "[0-4 or q]") {
				t.Errorf("%s: a status row, or not items 1 to 4:\n%s", tt.host, p.out)
			}
			if w := writes(h.System, n); len(w) > 0 || p.engines != 0 || len(p.runs) != 0 {
				t.Errorf("%s, plan only %t: drawing wrote %v, made %d engines and %d runs", tt.host, dryRun, w, p.engines, len(p.runs))
			}
		}
	}
	h, cfg := newHost(t, hostNoRuns)
	p := play(noReadDir{h, backupPath}, cfg, false, "0")
	if s := screens(p.out); p.err != nil || len(s) != 1 || row(s[0], "Backup Path") != "/var/backups/hardn Root only" || item(s[0], "4") != "" {
		t.Errorf("runs not listable: Run = %v, item 4 %q:\n%s", p.err, item(s[0], "4"), p.out)
	}
}

// TestTurn: item 1 turns backups off, then on, each through the plan, the
// review (turning off, that rollback backups are still taken: decision 8)
// and y/N: hardn.yml's enableBackups line alone changed, its backup taken
// under the path first, anyway with backups off, and said so; the
// configuration adopting the file as saved, so that the second change's
// save is not refused (CONFIG_CHANGED); the screen drawn again under each
// result.
func TestTurn(t *testing.T) {
	ctx := context.Background()
	h, cfg := newHost(t, hostDefault)
	p := play(h, cfg, false, "1", "y", "1", "y", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 2 || p.runs[0].Err != nil || p.runs[1].Err != nil {
		t.Fatalf("Run = %v, %d unread, runs %+v:\n%s", p.err, p.unread, p.runs, p.out)
	}
	if w := inOrder(p.out, "Turn Backups Off #", "Backup State Disabled", "Rollback backups are still taken", "Save the configuration",
		"Turn backups off? [y/N, 0 or q]: y", "Operation Successful", "Backup of /etc/hardn/hardn.yml kept in", anyway,
		"Backup Settings #", "Backup State Disabled", "Rollback backups are still taken", "1) Backup State Turn backups on", "4) Backup Runs 1 run",
		"Turn Backups On #", "Backup State Enabled", "Turn backups on? [y/N, 0 or q]: y", "Operation Successful", "Backup of /etc/hardn/hardn.yml kept in",
		"Backup Settings #", "Backup State Enabled", "4) Backup Runs 2 runs"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if strings.Count(p.out, anyway) != 1 || strings.Count(p.out, "Rollback backups are still taken") != 2 {
		t.Errorf("taken anyway said %d times, the rollback line %d; want 1, 2:\n%s", strings.Count(p.out, anyway), strings.Count(p.out, "Rollback backups are still taken"), p.out)
	}
	if strings.Contains(p.out, "CONFIG_CHANGED") || content(h.System, hardnYML) != string(ymlOf(true, backupPath)) || !cfg.EnableBackups {
		t.Errorf("hardn.yml after both:\n%s", content(h.System, hardnYML))
	}
	runs, err := hostbackup.Runs(ctx, h, backupPath)
	// Both runs' ids have the engine's one time, so their order is their
	// random part's.
	if err != nil || len(runs) != 2 || runs[0].Entries[0].TakenAnyway == runs[1].Entries[0].TakenAnyway ||
		runs[0].BackupsEnabled == runs[0].Entries[0].TakenAnyway || runs[1].BackupsEnabled == runs[1].Entries[0].TakenAnyway {
		t.Errorf("the runs %+v, %v; want two, one taken anyway with backups off", runs, err)
	}
	h, cfg = newHost(t, hostDefault)
	p = play(h, cfg, false, "1", "y", "0")
	want := strings.Replace(string(ymlOf(true, backupPath)), "enableBackups: true", "enableBackups: false", 1)
	if got := content(h.System, hardnYML); p.err != nil || got != want || cfg.EnableBackups {
		t.Errorf("turned off: hardn.yml\n%s\nthe configuration %t", got, cfg.EnableBackups)
	}
	if data, _, err := h.ReadFileNoFollow(ctx, hardnYML); err != nil || !cfg.LoadedFrom(data) {
		t.Errorf("the configuration does not record hardn.yml as saved (%v)", err)
	}
}

// TestChangePath: item 2's field refuses, each inline under it and asked
// again, what Backup's control's Field refuses, in its words (decisions
// 5, 6), the path in use a warning; then the plan for the path taken, its
// review and question, y: hardn.yml's backupPath line alone changed, the
// change's backup under the new path, nothing under the old (decision 7
// (a)); the configuration adopted, the screen drawn again on the new path.
func TestChangePath(t *testing.T) {
	h, cfg := newHost(t, hostDefault)
	p := play(h, cfg, false, "2", "", "backups", "/var/backups//hardn/", "/tmp/hardn", osRelease, backupPath, elsewhere+"/new", "y", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil {
		t.Fatalf("Run = %v, %d unread, runs %+v:\n%s", p.err, p.unread, p.runs, p.out)
	}
	if w := inOrder(p.out, "Change Backup Path #", "(x) Backup path cannot be empty", "(x) Not an absolute path", "(x) Write it as /var/backups/hardn",
		"(x) Others can change /tmp", "(x) Not a directory", "/var/backups/hardn is the backup path already", "Backup Path /srv/backups/hardn/new Not created...", // cut at 64 columns

		"Use /srv/backups/hardn/new? [y/N, 0 or q]: y", "Operation Successful", "Backup of /etc/hardn/hardn.yml kept in /srv/backups/hardn/new/",
		"Backup Settings #", "Backup Path /srv/backups/hardn/new Root only", "4) Backup Runs 1 run"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	want := strings.Replace(string(ymlOf(true, backupPath)), `"`+backupPath+`"`, `"`+elsewhere+`/new"`, 1)
	if got := content(h.System, hardnYML); got != want || cfg.BackupPath != elsewhere+"/new" {
		t.Errorf("hardn.yml\n%s\nthe configuration %q", got, cfg.BackupPath)
	}
	for _, w := range writes(h.System, 0) {
		if strings.Contains(w, backupPath) {
			t.Errorf("written under the old path: %s", w)
		}
	}
	if runs, err := hostbackup.Runs(context.Background(), h, elsewhere+"/new"); err != nil || len(runs) != 1 {
		t.Errorf("the runs under the new path: %d, %v", len(runs), err)
	}
}

// TestRefusedPathMoved: on a host whose backup path Check refuses (decision
// 7 (a)), item 1 is refused before it asks, in the refused path's words;
// item 2 takes another path, its change planned under the path it saves,
// nothing written under the refused one; item 1 is then planned.
func TestRefusedPathMoved(t *testing.T) {
	for _, tt := range []struct{ host, words, old string }{{hostUnsafe, unsafeWords, unsafePath}, {hostRelative, relativeWords, "backups"}} {
		h, cfg := newHost(t, tt.host)
		p := play(h, cfg, false, "1", "2", backupPath, "y", "1", "y", "0")
		if p.err != nil || p.unread != 0 || len(p.runs) != 2 || p.runs[0].Err != nil || p.runs[1].Err != nil {
			t.Fatalf("%s: Run = %v, %d unread, runs %+v:\n%s", tt.host, p.err, p.unread, p.runs, p.out)
		}
		if w := inOrder(p.out, "Warning "+tt.words, "Backup Settings #", "Change Backup Path #", "Use /var/backups/hardn?", "Operation Successful",
			"Backup Path /var/backups/hardn Root only", "Turn Backups Off #", "Operation Successful", anyway); w != "" {
			t.Errorf("%s: %q not shown after what came before it:\n%s", tt.host, w, p.out)
		}
		for _, w := range writes(h.System, 0) {
			if p := strings.Fields(w)[1]; p == tt.old || strings.HasPrefix(p, tt.old+"/") {
				t.Errorf("%s: written under the refused path: %s", tt.host, w)
			}
		}
		if cfg.BackupPath != backupPath || cfg.EnableBackups {
			t.Errorf("%s: the configuration %q, %t", tt.host, cfg.BackupPath, cfg.EnableBackups)
		}
	}
}

// TestVerify: item 3 is Check's reading, writing nothing, live and under
// plan only (decision 3): who can change the path, or the directory the
// first backup makes it in; where Check refuses the path, its words as a
// warning; each with "Nothing was written.".
func TestVerify(t *testing.T) {
	tests := []struct{ host, say string }{
		{hostDefault, "Only root can change /var/backups, where the first backup makes /var/backups/hardn. " + written},
		{hostMade, "Only root can change /var/backups/hardn. " + written},
		{hostElsewhere, "Only root can change /srv/backups/hardn. " + written},
		{hostReadOnly, "Only root can change /var/backups, where the first backup makes /var/backups/hardn. " + written},
		{hostUnsafe, "Warning " + unsafeWords + " " + written},
		{hostRelative, "Warning " + relativeWords + " " + written},
		{hostFile, "Warning " + fileWords + " " + written},
		{hostUnderFile, "Warning " + underWords + " " + written},
		{hostUnknown, "Warning " + unknownWords + " " + written},
	}
	for _, tt := range tests {
		for _, dryRun := range []bool{false, true} {
			h, cfg := newHost(t, tt.host)
			n := len(h.Calls())
			p := play(h, cfg, dryRun, "3", "0")
			if p.err != nil || p.unread != 0 || p.engines != 0 || len(screens(p.out)) != 2 {
				t.Fatalf("%s: Run = %v, %d unread, %d engines:\n%s", tt.host, p.err, p.unread, p.engines, p.out)
			}
			if !strings.Contains(flat(p.out), tt.say) {
				t.Errorf("%s: Verify does not say %q:\n%s", tt.host, tt.say, p.out)
			}
			if w := writes(h.System, n); len(w) > 0 {
				t.Errorf("%s, plan only %t: Verify wrote %v", tt.host, dryRun, w)
			}
		}
	}
	h, cfg := newHost(t, hostDefault)
	cfg.BackupPath = "/srv/new/hardn"
	if p := play(h, cfg, false, "3", "0"); !strings.Contains(flat(p.out), "Only root can change /srv, where the first backup makes /srv/new/hardn. "+written) {
		t.Errorf("two directories not there yet:\n%s", p.out)
	}
}

// TestRefusedBeforeAsking: where Check refuses the backup path, item 1 and
// Backup Runs say why before anything is asked (decision 7 (a)), in the
// refused path's words: the control's Why, then the screen's own second
// sentence; and Check failing otherwise. Nothing is planned or written.
func TestRefusedBeforeAsking(t *testing.T) {
	for _, tt := range []struct{ host, say string }{
		{hostUnsafe, unsafeWords}, {hostRelative, relativeWords}, {hostFile, fileWords}, {hostUnderFile, underWords}, {hostUnknown, unknownWords},
	} {
		h, cfg := newHost(t, tt.host)
		p := play(h, cfg, false, "1", "4", "0")
		if p.err != nil || p.unread != 0 || p.engines != 0 || len(p.runs) != 0 {
			t.Fatalf("%s: Run = %v, %d unread, %d engines, %d runs:\n%s", tt.host, p.err, p.unread, p.engines, len(p.runs), p.out)
		}
		if n := strings.Count(flat(p.out), " Warning "+tt.say); n != 2 {
			t.Errorf("%s: %q said %d times, want 2:\n%s", tt.host, tt.say, n, p.out)
		}
		if strings.Contains(p.out, "[y/N") || strings.Contains(p.out, "Select a run") {
			t.Errorf("%s: an item asked something:\n%s", tt.host, p.out)
		}
		if w := writes(h.System, 0); len(w) > 0 {
			t.Errorf("%s: wrote %v", tt.host, w)
		}
	}
}

// TestNotLoaded: with no hardn.yml read, items 1 and 2 say so before
// asking, in the save's words (CONFIG_NOT_LOADED; control-backup.md §10
// decision 10), and on a path Check refuses, before the path's refusal
// (screen-backup.md §8, decision 2); Verify and Backup Runs read as ever.
// Nothing is planned, written or adopted.
func TestNotLoaded(t *testing.T) {
	for _, tt := range []struct {
		host, path string
		read       []string // Verify's and Backup Runs' words
		refused    int      // how many times the path's refusal is said
	}{
		{hostDefault, "", []string{"Only root can change /var/backups", "No change has been backed up yet."}, 0},
		{hostUnsafe, unsafePath, []string{"Warning " + unsafeWords + " " + written, "Warning " + unsafeWords}, 2},
	} {
		h, _ := newHost(t, tt.host)
		cfg := config.DefaultConfig()
		cfg.BackupPath = tt.path
		p := play(h, cfg, false, "1", "2", "3", "4", "0")
		if p.err != nil || p.unread != 0 || p.engines != 0 || len(p.runs) != 0 {
			t.Fatalf("%s: Run = %v, %d unread, %d engines, runs %+v:\n%s", tt.host, p.err, p.unread, p.engines, p.runs, p.out)
		}
		if w := inOrder(p.out, append([]string{"Warning " + notLoaded, "Warning " + notLoaded}, tt.read...)...); w != "" {
			t.Errorf("%s: %q not shown after what came before it:\n%s", tt.host, w, p.out)
		}
		if n := strings.Count(flat(p.out), " Warning "+notLoaded); n != 2 {
			t.Errorf("%s: said %d times, want 2:\n%s", tt.host, n, p.out)
		}
		if n := strings.Count(flat(p.out), " Warning "+unsafeWords); n != tt.refused || strings.Contains(p.out, "Enter new backup path") {
			t.Errorf("%s: the path's refusal said %d times, want %d, or a field asked:\n%s", tt.host, n, tt.refused, p.out)
		}
		if w := writes(h.System, 0); len(w) > 0 || cfg.Path() != "" {
			t.Errorf("%s: wrote %v; the configuration at %q", tt.host, w, cfg.Path())
		}
	}
}

// TestRuns: Backup Runs (decision 9), read only: the runs under the path,
// newest first, each when it was taken and its files, "taken anyway" where
// a copy was taken with backups off; one run's files and what each was;
// back to the list, then the screen. With none, a line saying so; a run
// that cannot be read is said above the others; a path whose runs cannot
// be listed says why. Nothing is written, nothing planned.
func TestRuns(t *testing.T) {
	h, cfg := newHost(t, hostRuns)
	n := len(h.Calls())
	p := play(h, cfg, false, "4", "2", "0", "1", "0", "0", "0")
	if p.err != nil || p.unread != 0 || p.engines != 0 {
		t.Fatalf("Run = %v, %d unread, %d engines:\n%s", p.err, p.unread, p.engines, p.out)
	}
	if w := inOrder(p.out, "Backup Runs #", "Select a run", "1) 2026-10-19 14:30 1 file, taken anyway", "2) 2026-10-18 09:15 2 files",
		"3) 2026-10-17 08:30 1 file", "[0-3 or q]: 2", "Backup Run #", "Run 20261018T091500Z-1a2b3c4d", "/etc/resolv.conf 58 bytes", "/etc/hardn/hardn.yml",
		"[0 or q]: 0", "Backup Runs #", "[0-3 or q]: 1", "Run 20261019T143000Z-5e6f7a8b", "taken anyway", "Backup Runs #", "Backup Settings #"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if strings.Contains(p.out, "Restore") {
		t.Errorf("a restore offered (decision 9: a later unit):\n%s", p.out)
	}
	if w := writes(h.System, n); len(w) > 0 {
		t.Errorf("Backup Runs wrote %v", w)
	}
	h, cfg = newHost(t, hostSomeRuns)
	p = play(h, cfg, false, "4", "0", "0")
	if w := inOrder(p.out, "Warning Some runs can't be read: backup: ", "Backup Runs #", "3) 2026-10-17 08:30"); w != "" || p.err != nil {
		t.Errorf("a run not read: %q not shown (%v):\n%s", w, p.err, p.out)
	}
	h, cfg = newHost(t, hostNoRuns)
	p = play(noReadDir{h, backupPath}, cfg, false, "4", "0")
	if !strings.Contains(flat(p.out), "Warning Can't read the runs in /var/backups/hardn: backup: open /var/backups/hardn: permission denied.") || strings.Contains(p.out, "Select a run") {
		t.Errorf("no runs listable:\n%s", p.out)
	}
	h, cfg = newHost(t, hostMade)
	if p = play(h, cfg, false, "4", "0"); !strings.Contains(p.out, "No change has been backed up yet.") || strings.Contains(p.out, "Select a run") {
		t.Errorf("no runs:\n%s", p.out)
	}
}

// TestNothingWritten: n at the question, a dry run of each change, and 0
// at the path field change and save nothing, and the configuration stays
// as loaded; a dry run says no change was made.
func TestNothingWritten(t *testing.T) {
	tests := []struct {
		name   string
		dryRun bool
		lines  []string
		runs   int
		mode   output.Mode
	}{
		{"n", false, []string{"1", "n", "0"}, 1, output.ModeApply},
		{"dry run, item 1", true, []string{"1", "0"}, 1, output.ModePlan},
		{"dry run, item 2", true, []string{"2", elsewhere, "0"}, 1, output.ModePlan},
		{"0 at the field", false, []string{"2", "0", "0"}, 0, ""},
	}
	for _, tt := range tests {
		h, cfg := newHost(t, hostDefault)
		before, was := content(h.System, hardnYML), *cfg
		p := play(h, cfg, tt.dryRun, tt.lines...)
		if p.err != nil || p.unread != 0 || len(p.runs) != tt.runs || tt.runs > 0 && p.runs[0].Mode != tt.mode {
			t.Fatalf("%s: Run = %v, %d unread, runs %+v:\n%s", tt.name, p.err, p.unread, p.runs, p.out)
		}
		if w := writes(h.System, 0); len(w) > 0 || content(h.System, hardnYML) != before || cfg.EnableBackups != was.EnableBackups || cfg.BackupPath != was.BackupPath {
			t.Errorf("%s: wrote %v; hardn.yml:\n%s", tt.name, w, content(h.System, hardnYML))
		}
		if tt.dryRun != strings.Contains(p.out, "No actual changes were made to your system.") {
			t.Errorf("%s: the dry-run footer:\n%s", tt.name, p.out)
		}
	}
}

// TestFailedBackup: a backup that cannot be written (a read-only file
// system) fails the change at its backup, BACKUP_WRITE_FAILED, in the
// control's words; nothing changed, nothing adopted, and the screen drawn
// again as it was.
func TestFailedBackup(t *testing.T) {
	h, cfg := newHost(t, hostReadOnly)
	before := content(h.System, hardnYML)
	p := play(h, cfg, false, "1", "y", "0")
	if p.err != nil || len(p.runs) != 1 || output.CodeOf(p.runs[0].Err) != "BACKUP_WRITE_FAILED" {
		t.Fatalf("Run = %v, runs %+v:\n%s", p.err, p.runs, p.out)
	}
	if w := inOrder(p.out, "Failed BACKUP_WRITE_FAILED", "Operation Failed", "Can't back up /etc/hardn/hardn.yml in /var/backups/hardn: read-only file system",
		"Nothing was changed. To go on: choose a backup path hardn can write, with Backup's item 2.", "Backup Settings #", "Backup State Enabled"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if content(h.System, hardnYML) != before || !cfg.EnableBackups {
		t.Errorf("hardn.yml changed, or the configuration adopted:\n%s", content(h.System, hardnYML))
	}
}

// TestRefusedByThePlan: a refusal of the plan's own is the flow's report,
// with its code and how to go on, and nothing is written: hardn.yml
// edited elsewhere after it was read (CONFIG_CHANGED), which the screen
// does not show, its settings being the configuration's.
func TestRefusedByThePlan(t *testing.T) {
	h, cfg := newHost(t, hostDefault)
	h.AddFile(hardnYML, append(ymlOf(true, backupPath), "# edited elsewhere\n"...), 0o600, 0, 0)
	n := len(h.Calls())
	p := play(h, cfg, false, "1", "0")
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Plan == nil || output.CodeOf(p.runs[0].Plan.Err()) != "CONFIG_CHANGED" {
		t.Fatalf("Run = %v, runs %+v:\n%s", p.err, p.runs, p.out)
	}
	if w := inOrder(p.out, "/etc/hardn/hardn.yml changed since it was read (CONFIG_CHANGED)", "To go on: start again, so that the file is read as it is now.",
		"Backup Settings #", "Backup State Enabled"); w != "" || strings.Contains(p.out, "[y/N") {
		t.Errorf("%q not shown, or asked:\n%s", w, p.out)
	}
	if w := writes(h.System, n); len(w) > 0 || !cfg.EnableBackups {
		t.Errorf("wrote %v; the configuration %t", w, cfg.EnableBackups)
	}
}

// TestApplyContext: a change's apply, and nothing else, runs under the
// context Options.ApplyContext makes, stopped once the apply returned: a
// declined change, a dry run, Verify and the runs make none.
func TestApplyContext(t *testing.T) {
	for _, tc := range []struct {
		dryRun bool
		lines  []string
		made   int
	}{{false, []string{"1", "n", "3", "4", "1", "y", "0"}, 1}, {true, []string{"1", "0"}, 0}} {
		h, cfg := newHost(t, hostDefault)
		made, stopped := 0, 0
		p := playWith(h, cfg, tc.dryRun, func(o *backupscreen.Options) {
			o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
				made++
				c, cancel := context.WithCancel(ctx)
				return c, func() { stopped++; cancel() }
			}
		}, tc.lines...)
		if p.err != nil || made != tc.made || stopped != made {
			t.Errorf("Run = %v, %d contexts made, %d stopped, want %d:\n%s", p.err, made, stopped, tc.made, p.out)
		}
	}
}

// TestNavigation: 0 goes back, q quits from the screen, a change's
// question and the runs, and the end of the input aborts, at the screen,
// the path field and the runs; nothing written.
func TestNavigation(t *testing.T) {
	for _, tt := range []struct {
		host  string
		lines []string
		want  error
	}{
		{hostDefault, []string{"0"}, nil}, {hostDefault, []string{"q"}, nav.ErrQuit}, {hostDefault, []string{"1", "q"}, nav.ErrQuit},
		{hostRuns, []string{"4", "q"}, nav.ErrQuit}, {hostRuns, []string{"4", "1", "q"}, nav.ErrQuit}, {hostDefault, nil, prompt.ErrAborted},
		{hostDefault, []string{"2"}, prompt.ErrAborted}, {hostRuns, []string{"4"}, prompt.ErrAborted},
	} {
		h, cfg := newHost(t, tt.host)
		n := len(h.Calls())
		p := play(h, cfg, false, tt.lines...)
		if !errors.Is(p.err, tt.want) || (tt.want == nil) != (p.err == nil) {
			t.Errorf("%s %q: Run = %v, want %v", tt.host, tt.lines, p.err, tt.want)
		}
		if w := writes(h.System, n); len(w) > 0 {
			t.Errorf("%s %q: wrote %v", tt.host, tt.lines, w)
		}
	}
}

// TestRunNeeds: Run refuses options it cannot run with, writing nothing:
// no Prompter's In or Out, no System, Config or NewEngine.
func TestRunNeeds(t *testing.T) {
	h, cfg := newHost(t, hostDefault)
	n := len(h.Calls())
	var out bytes.Buffer
	in := prompt.New(strings.NewReader("0\n"), false)
	newEngine := func(func(engine.Event)) *engine.Engine { return nil }
	for _, o := range []backupscreen.Options{
		{Config: cfg}, {System: h}, {System: h, Config: cfg},
		{System: h, Config: cfg, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in}, System: h, Config: cfg, NewEngine: newEngine},
		{Prompter: nav.Prompter{Out: &out}, System: h, Config: cfg, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, Config: cfg, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, System: h, NewEngine: newEngine},
	} {
		if err := backupscreen.Run(context.Background(), o); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Run(%+v) = %v, want fs.ErrInvalid", o, err)
		}
	}
	if out.Len() > 0 || len(h.Calls()) != n {
		t.Errorf("wrote %q, %v", out.String(), h.Calls()[n:])
	}
}
