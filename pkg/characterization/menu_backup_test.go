package characterization

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/menu"
)

// The Backup screen (main menu 5), refactor plan slice 3's evidence
// (docs/engine/gallery-backup.md §2): each of its options on a script, in
// a child process of this test binary, as TestLegacyMenuDryRun runs the
// Firewall screen. Its repository (pkg/adapter/secondary
// file_backup_repository.go) is given the legacy mock file system, each
// call recorded (recordingFS): on a host it is the real one. The
// application layer's own check of the path (pkg/application
// backup_manager.go VerifyBackupPath) calls os directly, so it runs on a
// real directory under the case's temp dir, which the golden names $TMP.

// backupChild and backupDir name, in a child process, the Backup case it
// runs and the temp dir it runs in, as the parent wrote it.
const (
	backupChild = "HARDN_CHARACTERIZATION_BACKUP"
	backupDir   = "HARDN_CHARACTERIZATION_BACKUP_DIR"
)

// backupTmp stands for the case's temp dir in the goldens.
const backupTmp = "$TMP"

// backupPathUnder is the backupPath of every case, under the temp dir:
// hardn's default (/var/backups/hardn) moved there.
const backupPathUnder = "var/backups/hardn"

// sentinel is what a case writes to <backupPath>/.write_test beforehand,
// so that the golden shows whether the screen overwrote or removed it.
const sentinel = "a file of the user's own\n"

// backupCase is one run of the Backup screen: what it shows and why, the
// lines typed, and the host and configuration it starts from.
type backupCase struct {
	name, screen, script string
	// enabled is enableBackups; dryRun, the session's mode as origin/main
	// reached the screen under -n.
	enabled, dryRun bool
	// exists: backupPath is a directory; withSentinel: holding a
	// .write_test file of the user's.
	exists, withSentinel bool
	// failMkdir and failWrite make the repository's file system refuse
	// MkdirAll of backupPath and WriteFile of its .write_test.
	failMkdir, failWrite bool
	notes, defects       []string
}

// backupCases are the Backup screen's options: open and leave, live and
// in dry run, with the directory there or not; 1 each way, and failing; 2
// with a relative path, and with none; 3 (Verify), live, in dry run, and
// failing; 3 while hidden; an invalid key; 0.
func backupCases() []backupCase {
	const (
		opens   = "Opening the screen with backups on writes <backupPath>/.write_test and removes it (backup_manager.go:91-99), with os.WriteFile, outside the repository: a file of that name is overwritten, then deleted."
		readsNo = "The screen and its layers read no mode: nothing in backup_menu.go, backup_manager.go, backup_service.go or file_backup_repository.go reads cfg.DryRun."
		nothing = "Nothing takes a backup: utils.BackupFile and the repository's BackupFile have no caller (side-effects.md item 9, V18)."
		mockFS  = "The repository's calls are on the legacy mock file system; on a host they are real (infrastructure.ServiceFactory gives it the OS file system)."
	)
	return []backupCase{
		{name: "backup_open", screen: "Backup (main menu 5), backups on, q", script: "q\n", enabled: true, exists: true, withSentinel: true,
			defects: []string{opens}},
		{name: "backup_open_dry_run", screen: "Backup (main menu 5) in dry-run mode, backups on, q", script: "q\n", enabled: true, dryRun: true, exists: true, withSentinel: true,
			defects: []string{opens + " In dry run too."},
			notes:   []string{readsNo, "Since next/s2b-replace the main menu refuses 5 under plan only (pkg/menu TestPlanOnlyRefusals), so hardn no longer opens this screen in that mode."}},
		{name: "backup_open_missing", screen: "Backup (main menu 5), backups on, backupPath not there, q", script: "q\n", enabled: true},
		{name: "backup_disabled_3", screen: "Backup (main menu 5), backups off, 3 (not shown), then q", script: "3\n\nq\n",
			defects: []string{"Item 3 is not shown with backups off, yet 3 is taken: a pause, nothing else, then the screen again (backup_menu.go:102-108, :210-233)."}},
		{name: "backup_toggle_off", screen: "Backup (main menu 5), backups on, 1 (toggle), then q", script: "1\n\nq\n", enabled: true, exists: true,
			defects: []string{"The whole configuration is saved to ./hardn.yml, not to the file loaded (side-effects.md row 29)."},
			notes:   []string{nothing}},
		{name: "backup_toggle_on", screen: "Backup (main menu 5), backups off, backupPath not there, 1 (toggle), then q", script: "1\n\nq\n",
			defects: []string{"\"Modified files will be backed up\": " + nothing, "The whole configuration is saved to ./hardn.yml, not to the file loaded (side-effects.md row 29)."},
			notes:   []string{mockFS + " So the redrawn screen finds no directory on disk here; on a host the repository made it (0755) and wrote and removed its .write_test."}},
		{name: "backup_toggle_on_fails", screen: "Backup (main menu 5), backups off, 1 (toggle) with the directory refused, then q", script: "1\n\nq\n", failMkdir: true,
			defects: []string{"The enable fails, yet the screen drawn again says Enabled: the repository set it before its check (file_backup_repository.go:248-256), and nothing undoes it. ./hardn.yml is not saved, so the next start says Disabled."},
			notes:   []string{mockFS}},
		{name: "backup_path_relative", screen: "Backup (main menu 5), backups on, 2 (path) \"backups\", then q", script: "2\nbackups\n\nq\n", enabled: true, exists: true,
			defects: []string{"A relative path is taken and saved: no check but the directory's (backup_manager.go:46-56). pkg/host/backup's Open refuses a relative backupPath (ErrSettings), which every engine plan checks (backup.Check) and every apply opens, so the session's next engine change, and every one after a start that reads this file, is refused (docs/engine/gallery-backup.md §6).",
				"The whole configuration is saved to ./hardn.yml, not to the file loaded (side-effects.md row 29)."},
			notes: []string{mockFS + " The relative path is under the child's working directory, the temp dir."}},
		{name: "backup_path_empty", screen: "Backup (main menu 5), backups on, 2 (path) with nothing typed, then q", script: "2\n\n\nq\n", enabled: true, exists: true},
		{name: "backup_verify", screen: "Backup (main menu 5), backups on, 3 (Verify), then q", script: "3\n\nq\n", enabled: true, exists: true,
			defects: []string{"Verify makes the directory 0755 and writes and removes <backupPath>/.write_test (file_backup_repository.go:220-238)."},
			notes:   []string{mockFS}},
		{name: "backup_verify_dry_run", screen: "Backup (main menu 5) in dry-run mode, backups on, 3 (Verify), then q", script: "3\n\nq\n", enabled: true, dryRun: true, exists: true,
			defects: []string{"Verify makes the directory and writes and removes <backupPath>/.write_test in dry run too."},
			notes:   []string{readsNo, mockFS}},
		{name: "backup_verify_fails", screen: "Backup (main menu 5), backups on, 3 (Verify) with the test file refused, then q", script: "3\n\nq\n", enabled: true, exists: true, failWrite: true,
			notes: []string{mockFS + " So here the screen's own check (on disk) says writable while Verify (the mock) fails; on a host both write to one file system."}},
		{name: "backup_invalid", screen: "Backup (main menu 5), backups on, 7 (no such option), then q", script: "7\n\nq\n", enabled: true, exists: true},
		{name: "backup_return", screen: "Backup (main menu 5), backups on, 0", script: "0\n", enabled: true, exists: true},
	}
}

// TestLegacyMenuBackup records origin/main's Backup screen, each option in
// a child process of this test binary whose stdin is the script, not a
// terminal, and whose working directory is a temp dir, as
// TestLegacyMenuDryRun does. Each golden holds the screen as printed (ESC
// as \x1b, the temp dir as $TMP), then what the repository asked of its
// file system, the directory as the screen left it, and ./hardn.yml as
// the screen left it. The pauses say "Press Enter", where origin/main
// says "Press any key" (DEV-050); nothing else differs from origin/main
// in these files (backup_menu.go, backup_manager.go, backup_service.go
// and file_backup_repository.go are origin/main's on the trunk, the
// pauses' words aside).
func TestLegacyMenuBackup(t *testing.T) {
	if name := os.Getenv(backupChild); name != "" {
		runBackupChild(name, os.Getenv(backupDir))
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range backupCases() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestLegacyMenuBackup$")
			cmd.Env = append(os.Environ(), backupChild+"="+tc.name, backupDir+"="+dir, "NO_COLOR=", "RUNEWIDTH_EASTASIAN=0", "HARDN_CONFIG=")
			cmd.Dir = dir
			cmd.Stdin = strings.NewReader(tc.script)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("child: %v\n%s", err, out)
			}
			screen, trailer, _ := strings.Cut(string(out), childTrailer)
			saved, err := os.ReadFile(filepath.Join(dir, "hardn.yml"))
			switch {
			case errors.Is(err, fs.ErrNotExist):
				saved = nil
			case err != nil:
				t.Fatal(err)
			}

			var b strings.Builder
			b.WriteString(baselineHeader + "\n")
			b.WriteString("# Generated by TestLegacyMenuBackup (pkg/characterization); regenerate with -update. Do not edit.\n")
			fmt.Fprintf(&b, "# Screen: %s (pkg/menu), run in a child process\n", tc.screen)
			fmt.Fprintf(&b, "# Script: stdin %q, not a terminal, so nothing typed is echoed\n", tc.script)
			fmt.Fprintf(&b, "# Start: enableBackups %t, backupPath %s/%s (%s), dryRun %t\n", tc.enabled, backupTmp, backupPathUnder, startWords(tc), tc.dryRun)
			fmt.Fprintf(&b, "# Pauses: %d (\"Press Enter to ...\"; origin/main says \"Press any key to ...\", DEV-050)\n", strings.Count(screen, "Press Enter to"))
			for _, d := range tc.defects {
				fmt.Fprintf(&b, "# KNOWN-DEFECT: %s\n", d)
			}
			for _, n := range tc.notes {
				fmt.Fprintf(&b, "# NOTE: %s\n", n)
			}
			b.WriteString("# The screen as printed, ESC as the four characters \\x1b, each line ending in $; <no-newline> marks text with no final newline.\n")
			b.WriteString("# ----\n")
			b.WriteString(visible(screen))
			b.WriteString("# ----\n")
			b.WriteString(trailer)
			if saved == nil {
				b.WriteString("# ./hardn.yml: not written\n")
			} else {
				b.WriteString("# ./hardn.yml as written:\n")
				b.Write(saved)
			}
			got := strings.ReplaceAll(b.String(), dir, backupTmp)
			if resolved, err := filepath.EvalSymlinks(dir); err == nil && strings.Contains(got, resolved) {
				t.Fatalf("golden would contain the temp dir %s", resolved)
			}
			assertGolden(t, "menu/"+tc.name+".golden", got)
		})
	}
}

// startWords says what is at backupPath when the case starts.
func startWords(tc backupCase) string {
	switch {
	case tc.withSentinel:
		return fmt.Sprintf("a directory holding .write_test, %d bytes of the user's", len(sentinel))
	case tc.exists:
		return "an empty directory"
	}
	return "nothing there"
}

// runBackupChild runs the Backup screen of case name, in the child
// process, under dir: hardn's defaults for alice, backupPath moved under
// dir, the BackupManager built as infrastructure.ServiceFactory builds it,
// over the recording mock file system. Then it reports, after
// childTrailer, what the repository asked of its file system and what is
// at backupPath on disk.
func runBackupChild(name, dir string) {
	var tc backupCase
	for _, c := range backupCases() {
		if c.name == name {
			tc = c
		}
	}
	if tc.name == "" || dir == "" {
		fmt.Fprintf(os.Stderr, "no backup case %q in %q\n", name, dir)
		os.Exit(2)
	}
	path := filepath.Join(dir, filepath.FromSlash(backupPathUnder))
	if tc.exists {
		must(os.MkdirAll(path, 0o755))
	}
	if tc.withSentinel {
		must(os.WriteFile(filepath.Join(path, ".write_test"), []byte(sentinel), 0o644))
	}
	cfg := config.DefaultConfig()
	cfg.Username, cfg.SshPort = "alice", 2230
	cfg.EnableBackups, cfg.BackupPath, cfg.DryRun = tc.enabled, path, tc.dryRun

	mock := interfaces.NewMockFileSystem()
	if tc.failMkdir {
		mock.MkdirAllError[path] = errors.New("mkdir " + path + ": read-only file system")
	}
	if tc.failWrite {
		mock.WriteFileError[filepath.Join(path, ".write_test")] = errors.New("open " + filepath.Join(path, ".write_test") + ": read-only file system")
	}
	log := &trace{}
	repo := secondary.NewFileBackupRepository(recordingFS{inner: mock, log: log}, interfaces.NewMockCommander(), cfg.BackupPath, cfg.EnableBackups)
	backups := application.NewBackupManager(service.NewBackupServiceImpl(repo))
	mm := application.NewMenuManager(nil, nil, nil, nil, nil, backups, nil, nil, nil, nil)
	menu.NewBackupMenu(mm, cfg).Show()

	fmt.Print(childTrailer)
	fmt.Printf("# The repository's file system calls (the legacy mock):\n%s", log)
	fmt.Printf("# %s on disk after: %s\n", path, onDisk(path, tc.withSentinel))
}

// onDisk says what is at path once the screen has returned: nothing, or
// a directory and its entries, and with withSentinel what became of the
// user's .write_test.
func onDisk(path string, withSentinel bool) string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "not there"
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	s := "a directory, empty"
	if len(names) > 0 {
		s = "a directory holding " + strings.Join(names, ", ")
	}
	if withSentinel {
		data, err := os.ReadFile(filepath.Join(path, ".write_test"))
		switch {
		case err != nil:
			s += fmt.Sprintf("; the user's .write_test (%d bytes) is gone", len(sentinel))
		case string(data) != sentinel:
			s += fmt.Sprintf("; .write_test now holds %q", data)
		default:
			s += "; .write_test as it was"
		}
	}
	return s
}

// must ends the child on err.
func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
