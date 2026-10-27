package output_test

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The words after a change, and of the backups' refusals and failures
// (refactor plan slice 3, decisions 7 (a) and 8; next/s3-control).

const keptRun = backup.DefaultPath + "/" + fixtureID

// keptManifest is a backup run of hardn.yml, a file backed up twice, and a
// path not there before, with backups as enabled says.
func keptManifest(enabled bool) *backup.Manifest {
	content := &backup.Content{Backup: "x", Mode: 0o600, Size: 1, SHA256: strings.Repeat("ab", 32)}
	return &backup.Manifest{SchemaVersion: backup.SchemaVersion, RunID: fixtureID, BackupsEnabled: enabled, Dir: keptRun,
		Entries: []backup.Entry{
			{Seq: 1, Path: fixtureConfig, Kind: backup.KindFile, File: content, RollbackRequired: true, TakenAnyway: !enabled},
			{Seq: 2, Path: "/etc/resolv.conf", Kind: backup.KindFile, File: content, RollbackRequired: true, TakenAnyway: !enabled},
			{Seq: 3, Path: "/etc/resolv.conf", Kind: backup.KindFile, File: content, RollbackRequired: true, TakenAnyway: !enabled},
			{Seq: 4, Path: "/etc/systemd/resolved.conf.d/zz-hardn.conf", Kind: backup.KindAbsent, RollbackRequired: true, TakenAnyway: !enabled},
		}}
}

// wide is the core set's plain cell, wide enough that only a callout's
// own measure breaks a line.
func wide() term.Env {
	env := uitest.Core()[1].Env
	env.Width = 200
	return env
}

// escape is an SGR sequence.
var escape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// words is a report's words in one line: no escape sequence, no callout
// mark, a path broken at a hyphen joined, each run of spaces one.
func words(s string) string {
	s = escape.ReplaceAllString(s, "")
	s = strings.ReplaceAll("\n"+s, "\n# ", "\n")
	s = strings.ReplaceAll(s, "-\n", "-") // a path broken at its hyphen
	return strings.Join(strings.Fields(s), " ")
}

// saveRun is a run that applied one change, the save of hardn.yml, with m
// as its backup run.
func saveRun(m *backup.Manifest) output.Run {
	c := engine.Change{ID: "backup.settings/save", Control: "backup.settings", Summary: "Save the configuration to " + fixtureConfig,
		Title: "Save the configuration", Target: fixtureConfig, Stage: engine.StageFiles, Backups: []string{fixtureConfig}}
	f := debian13()
	return output.Run{Mode: output.ModeApply, HardnVersion: fixtureVersion, Hostname: fixtureHost, Facts: &f,
		Plan:   &engine.Plan{ID: fixtureID, Facts: f, Changes: []engine.Change{c}, Items: []engine.Item{{Control: c.Control, Title: "Backups", Changes: []string{c.ID}}}},
		Result: &engine.Result{ID: fixtureID, Applied: []engine.Applied{{Change: c.ID, Detail: "saved " + fixtureConfig}}, Backup: m}}
}

// TestKept: after any change applied, the report and AfterFlow say what it
// backed up, each file once and none that was not there before, and
// where; with backups off, that it was taken anyway; the JSON document has
// it as result.backup. Nothing for a run that kept no copy, a dry run, or
// an apply that failed.
func TestKept(t *testing.T) {
	plain := wide()
	files := "Backup of " + fixtureConfig + ", /etc/resolv.conf kept in"
	for _, tc := range []struct {
		name    string
		run     output.Run
		lines   []string
		anyway  bool
		noKept  bool
		jsonOut *output.Kept
	}{
		{name: "backups on", run: saveRun(keptManifest(true)), lines: []string{files, keptRun},
			jsonOut: &output.Kept{Dir: keptRun, Files: []string{fixtureConfig, "/etc/resolv.conf"}}},
		{name: "backups off", run: saveRun(keptManifest(false)), lines: []string{files, keptRun, "Backed up anyway: the rollback needs it."}, anyway: true,
			jsonOut: &output.Kept{Dir: keptRun, Files: []string{fixtureConfig, "/etc/resolv.conf"}, TakenAnyway: true}},
		{name: "no copy kept", run: saveRun(&backup.Manifest{Dir: keptRun, Entries: keptManifest(false).Entries[3:]}), noKept: true},
		{name: "no backup run", run: saveRun(nil), noKept: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			human := words(output.Human(plain, tc.run, fixtureNow))
			after := words(output.AfterFlow(plain, tc.run, fixtureNow))
			want := strings.Join(strings.Fields(strings.Join(tc.lines, "\n")), " ")
			for name, got := range map[string]string{"Human": human, "AfterFlow": after} {
				switch {
				case tc.noKept && strings.Contains(got, "kept in"):
					t.Errorf("%s says a backup was kept: %s", name, got)
				case !tc.noKept && !strings.Contains(got, want):
					t.Errorf("%s: %s\nwant it to say %s", name, got, want)
				case !tc.anyway && strings.Contains(got, "anyway"):
					t.Errorf("%s says taken anyway: %s", name, got)
				}
			}
			if !tc.noKept && !strings.Contains(human, "Operation Successful "+want) {
				t.Errorf("Human: not after Operation Successful: %s", human)
			}
			d := output.Build(tc.run)
			if got := d.Result.Backup; fmt.Sprint(got) != fmt.Sprint(tc.jsonOut) {
				t.Errorf("result.backup %+v, want %+v", got, tc.jsonOut)
			}
		})
	}
	// A dry run, and an apply that failed, keep nothing to say.
	r := saveRun(keptManifest(true))
	r.Mode, r.Result = output.ModePlan, nil
	if s := output.Human(plain, r, fixtureNow) + output.AfterFlow(plain, r, fixtureNow); strings.Contains(s, "kept in") {
		t.Errorf("a dry run says a backup was kept:\n%s", s)
	}
	r = saveRun(keptManifest(true))
	r.Result.Failed = &engine.ApplyError{ID: fixtureID, Change: "backup.settings/save", Step: engine.StepApply, Err: errors.New("fixture: failed")}
	r.Err = r.Result.Failed
	if s := output.Human(plain, r, fixtureNow); strings.Contains(s, "kept in") || output.Build(r).Result.Backup != nil {
		t.Errorf("a failed apply says a backup was kept:\n%s", s)
	}
}

// TestBackupsRefusalWords: the engine's refusal of a plan's backup
// settings (the DNS screen's, -g's, while backupPath is refused) is
// reported as a refusal, Why then "To go on:", its code as before; a
// control's own refusal keeps its words; an error of the backups that is
// not the path's keeps its message.
func TestBackupsRefusalWords(t *testing.T) {
	plain := wide()
	unsafe := fmt.Errorf("engine: the backups: %w", &backup.DirError{Dir: "/tmp", Err: backup.ErrUnsafeDir})
	relative := fmt.Errorf("engine: the backups: backup: backupPath %q is not an absolute directory: %w", "backups", backup.ErrSettings)
	for _, tc := range []struct {
		name, path   string
		err          error
		owner        string
		why, fix     string
		code, phrase string
	}{
		{name: "under /tmp", path: "/tmp/hardn-backups", err: unsafe, why: "Others can change /tmp, so backups can't go in /tmp/hardn-backups",
			fix: "choose a directory only root can change, with Backup's item 2", code: "UNSAFE_DIRECTORY"},
		{name: "relative", path: "backups", err: relative, why: `"backups" is not an absolute path`, fix: "set an absolute path with Backup's item 2",
			code: "BACKUP_SETTINGS_INVALID"},
		{name: "not the path's", path: backup.DefaultPath, err: fmt.Errorf("engine: the backups: backup: %w", backup.ErrNotRegular), code: "BACKUP_NOT_REGULAR"},
		{name: "a control's own", path: "/tmp/hardn-backups", owner: "example", err: &steps.Refusal{Code: "DNS_MANAGED", Why: "NetworkManager manages DNS here",
			Fix: "set the nameservers with nmcli", Err: unsafe}, why: "NetworkManager manages DNS here", fix: "set the nameservers with nmcli", code: "UNSAFE_DIRECTORY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &engine.Plan{ID: fixtureID, Facts: debian13(), Backups: backup.Settings{Path: tc.path, Enabled: true}, Refused: []error{tc.err}}
			if tc.owner != "" {
				p.Items = []engine.Item{{Control: tc.owner, Title: "Example", Err: tc.err}}
			}
			r := output.Run{Mode: output.ModePlan, Plan: p, Err: p.Err()}
			d := output.Build(r)
			if len(d.Errors) != 1 || d.Errors[0].Why != tc.why || d.Errors[0].Fix != tc.fix || d.Errors[0].Code != tc.code {
				t.Fatalf("errors %+v, want %q, %q, %s", d.Errors, tc.why, tc.fix, tc.code)
			}
			if tc.why == "" {
				return
			}
			human := words(output.Human(plain, r, fixtureNow))
			want := tc.why + " (" + tc.code + ") To go on: " + tc.fix + "."
			if !strings.Contains(human, want) {
				t.Errorf("Human: %s\nwant it to say %s", human, want)
			}
		})
	}
}

// TestBackupWriteWords: an apply that failed at a backup it could not
// write (a read-only file system) is reported in a few words, its code on
// the step's line, the report naming the file and the backupPath and the
// cause, then that nothing was changed once the rollback finished and no
// boot unit was installed first (it is never rolled back), then how to
// go on; the document has the code, and the words as why. (The
// flow's failure, the same words through FlowEngine, is the gallery's
// play/backup-readonly.)
func TestBackupWriteWords(t *testing.T) {
	plain := wide()
	we := &backup.WriteError{Path: fixtureConfig, BackupPath: backup.DefaultPath, Err: &fs.PathError{Op: "mkdir", Path: backup.DefaultPath, Err: syscall.EROFS}}
	why := "Can't back up " + fixtureConfig + " in " + backup.DefaultPath + ": read-only file system"
	fix := "To go on: choose a backup path hardn can write, with Backup's item 2."
	for _, tc := range []struct {
		finished bool
		boot     bool // the boot unit's install ran first, never rolled back
	}{{true, false}, {false, false}, {true, true}} {
		r := saveRun(nil)
		r.Result.Applied = nil
		ae := &engine.ApplyError{ID: fixtureID, Change: "backup.settings/save", Step: engine.StepBackup, Err: we}
		if !tc.finished {
			ae.RollbackErr = fmt.Errorf("fixture: %w", errors.New("not finished"))
		}
		if tc.boot {
			r.Result.Boot = &engine.BootInstall{Detail: "written, enabled"}
		}
		r.Result.Failed, r.Err = ae, ae
		d := output.Build(r)
		if d.Errors[0].Code != "BACKUP_WRITE_FAILED" || d.Errors[0].Why != why {
			t.Errorf("%+v: errors %+v", tc, d.Errors)
		}
		human := words(output.Human(plain, r, fixtureNow))
		next := fix
		if tc.finished && !tc.boot {
			next = "Nothing was changed. " + fix
		}
		if !strings.Contains(human, "Failed BACKUP_WRITE_FAILED") || !strings.Contains(human, "Operation Failed "+why+" "+next) {
			t.Errorf("%+v: %s", tc, human)
		}
		if (!tc.finished || tc.boot) && strings.Contains(human, "Nothing was changed") {
			t.Errorf("%+v: said to have changed nothing: %s", tc, human)
		}
	}
}
