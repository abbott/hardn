package characterization

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
// file_backup_repository.go) is given the real file system, as
// infrastructure.ServiceFactory gives it, each call recorded
// (recordingFS); the application layer's own check of the path
// (pkg/application backup_manager.go VerifyBackupPath) calls os directly.
// Both work under the case's temp dir, which the golden names $TMP.

// backupChild and backupDir name, in a child process, the Backup case it
// runs and the temp dir it runs in, as the parent wrote it.
const (
	backupChild = "HARDN_CHARACTERIZATION_BACKUP"
	backupDir   = "HARDN_CHARACTERIZATION_BACKUP_DIR"
)

// backupTmp stands for the case's temp dir in the goldens, and in a
// script, where the parent writes the temp dir in its place.
const backupTmp = "$TMP"

// backupPathUnder is the backupPath of every case, under the temp dir:
// hardn's default (/var/backups/hardn) moved there.
const backupPathUnder = "var/backups/hardn"

// sentinel is what a case writes to <backupPath>/.write_test beforehand,
// so that the golden shows whether the screen overwrote or removed it.
const sentinel = "a file of the user's own\n"

// start is what is at backupPath when a case starts.
type start int

const (
	atNothing  start = iota // nothing there
	atDir                   // an empty directory
	atSentinel              // a directory holding a .write_test of the user's
	atFile                  // a regular file
)

// backupCase is one run of the Backup screen: what it shows and why, the
// lines typed ($TMP for the temp dir), and the host and configuration it
// starts from.
type backupCase struct {
	name, screen, script string
	// enabled is enableBackups; dryRun, the session's mode as origin/main
	// reached the screen under -n.
	enabled, dryRun bool
	// at is what is at backupPath.
	at start
	// shared makes $TMP/shared a directory anyone can write to; afile
	// makes $TMP/afile a regular file.
	shared, afile  bool
	notes, defects []string
}

// backupCases are the Backup screen's options: open and leave, live and
// in dry run, with the directory there, not there or a file; 1 each way,
// failing, and in dry run; 2 relative, empty, under a directory anyone
// can change, with ~, under a file, and in dry run; 3 (Verify), live, in
// dry run, and failing; 3 while hidden; an invalid key; 0.
func backupCases() []backupCase {
	const (
		opens   = "Opening the screen with backups on writes <backupPath>/.write_test and removes it (backup_manager.go:91-99), with os.WriteFile, outside the repository: a file of that name is overwritten, then deleted."
		readsNo = "The screen and its layers read no mode: nothing in backup_menu.go, backup_manager.go, backup_service.go or file_backup_repository.go reads cfg.DryRun."
		nothing = "Nothing takes a backup: utils.BackupFile and the repository's BackupFile have no caller (side-effects.md item 9, V18; run: backups/legacy_operations.golden)."
		whole   = "The whole configuration is saved to ./hardn.yml, not to the file loaded (side-effects.md row 29)."
		refused = "Since next/s2b-replace the main menu refuses 5 under plan only (pkg/menu TestPlanOnlyRefusals), so hardn no longer opens this screen in that mode."
		asLoad  = "The save writes dryRun as loaded, false here (config.SaveConfig, slice 2b decision 8); origin/main's wrote the session's, true under -n (read: 9d46866 config.go:445-447)."
	)
	return []backupCase{
		{name: "backup_open", screen: "Backup (main menu 5), backups on, q", script: "q\n", enabled: true, at: atSentinel,
			defects: []string{opens}},
		{name: "backup_open_dry_run", screen: "Backup (main menu 5) in dry-run mode, backups on, q", script: "q\n", enabled: true, dryRun: true, at: atSentinel,
			defects: []string{opens + " In dry run too."},
			notes:   []string{readsNo, refused}},
		{name: "backup_open_missing", screen: "Backup (main menu 5), backups on, backupPath not there, q", script: "q\n", enabled: true},
		{name: "backup_open_file", screen: "Backup (main menu 5), backups on, backupPath a regular file, q", script: "q\n", enabled: true, at: atFile,
			defects: []string{"A path that is a file reads \"doesn't exist or isn't writable\" and \"Directory will be created when needed\", which it cannot be (backup_menu.go:80-83; backup_manager.go:87-96 returns false for both)."}},
		{name: "backup_disabled_3", screen: "Backup (main menu 5), backups off, 3 (not shown), then q", script: "3\n\nq\n",
			defects: []string{"Item 3 is not shown with backups off, yet 3 is taken: a pause, nothing else, then the screen again (backup_menu.go:102-108, :210-233)."}},
		{name: "backup_toggle_off", screen: "Backup (main menu 5), backups on, 1 (toggle), then q", script: "1\n\nq\n", enabled: true, at: atDir,
			defects: []string{whole},
			notes:   []string{nothing}},
		{name: "backup_toggle_on", screen: "Backup (main menu 5), backups off, backupPath not there, 1 (toggle), then q", script: "1\n\nq\n",
			defects: []string{"\"Modified files will be backed up\": " + nothing, "The repository makes backupPath 0755 and writes and removes its .write_test (file_backup_repository.go:248-256, :220-238).", whole}},
		{name: "backup_toggle_on_fails", screen: "Backup (main menu 5), backups off, backupPath a regular file, 1 (toggle), then q", script: "1\n\nq\n", at: atFile,
			defects: []string{"The enable fails, yet the screen drawn again says Enabled: the repository set it before its check (file_backup_repository.go:248-256), and nothing undoes it. ./hardn.yml is not saved, so the next start says Disabled."}},
		{name: "backup_toggle_on_dry_run", screen: "Backup (main menu 5) in dry-run mode, backups off, backupPath not there, 1 (toggle), then q", script: "1\n\nq\n", dryRun: true,
			defects: []string{"In dry run too, turning backups on makes backupPath, writes and removes its .write_test, and saves ./hardn.yml."},
			notes:   []string{readsNo, refused, asLoad}},
		{name: "backup_path_relative", screen: "Backup (main menu 5), backups on, 2 (path) \"backups\", then q", script: "2\nbackups\n\nq\n", enabled: true, at: atDir,
			defects: []string{"A relative path is taken, made under the working directory and saved: no check but the directory's (backup_manager.go:46-56). pkg/host/backup's Open refuses a relative backupPath (ErrSettings), which every engine plan checks (backup.Check) and every apply opens, so the session's next engine change, and every one after a start that reads this file, is refused (docs/engine/gallery-backup.md §7).",
				whole},
			notes: []string{"The working directory is the temp dir."}},
		{name: "backup_path_empty", screen: "Backup (main menu 5), backups on, 2 (path) with nothing typed, then q", script: "2\n\n\nq\n", enabled: true, at: atDir},
		{name: "backup_path_unsafe", screen: "Backup (main menu 5), backups on, 2 (path) under a directory anyone can write to, then q", script: "2\n$TMP/shared/hardn\n\nq\n", enabled: true, at: atDir, shared: true,
			defects: []string{"A path under a directory anyone can change is taken, made and saved; pkg/host/backup refuses it (ErrUnsafeDir), and so every engine change after.", whole}},
		{name: "backup_path_tilde", screen: "Backup (main menu 5), backups on, 2 (path) \"~alice/backups\", then q", script: "2\n~alice/backups\n\nq\n", enabled: true, at: atDir,
			defects: []string{"~alice is read as ~ then alice: the path is $HOME/alice/backups, the home of the user hardn runs as, root's under sudo (backup_manager.go:47-53).", whole},
			notes: []string{"HOME is $TMP/home here."}},
		{name: "backup_path_fails", screen: "Backup (main menu 5), backups on, 2 (path) under a regular file, then q", script: "2\n$TMP/afile/backups\n\nq\n", enabled: true, at: atDir, afile: true,
			defects: []string{"The change fails, yet the screen drawn again shows the new path: the repository set it before its check (file_backup_repository.go:248-256), and nothing undoes it. ./hardn.yml is not saved."}},
		{name: "backup_path_dry_run", screen: "Backup (main menu 5) in dry-run mode, backups on, 2 (path) to a new directory, then q", script: "2\n$TMP/other\n\nq\n", enabled: true, dryRun: true, at: atDir,
			defects: []string{"In dry run too, a new path is made, its .write_test written and removed, and ./hardn.yml saved."},
			notes:   []string{readsNo, refused, asLoad}},
		{name: "backup_verify", screen: "Backup (main menu 5), backups on, 3 (Verify), then q", script: "3\n\nq\n", enabled: true, at: atDir,
			defects: []string{"Verify makes the directory 0755 and writes and removes <backupPath>/.write_test (file_backup_repository.go:220-238)."}},
		{name: "backup_verify_dry_run", screen: "Backup (main menu 5) in dry-run mode, backups on, 3 (Verify), then q", script: "3\n\nq\n", enabled: true, dryRun: true, at: atDir,
			defects: []string{"Verify makes the directory and writes and removes <backupPath>/.write_test in dry run too."},
			notes:   []string{readsNo, refused}},
		{name: "backup_verify_fails", screen: "Backup (main menu 5), backups on, backupPath a regular file, 3 (Verify), then q", script: "3\n\nq\n", enabled: true, at: atFile},
		{name: "backup_invalid", screen: "Backup (main menu 5), backups on, 7 (no such option), then q", script: "7\n\nq\n", enabled: true, at: atDir},
		{name: "backup_return", screen: "Backup (main menu 5), backups on, 0", script: "0\n", enabled: true, at: atDir},
	}
}

// TestLegacyMenuBackup records origin/main's Backup screen, each option in
// a child process of this test binary whose stdin is the script, not a
// terminal, and whose working directory and HOME are a temp dir, as
// TestLegacyMenuDryRun does. Each golden holds the screen as printed (ESC
// as \x1b, the temp dir as $TMP), then what the repository asked of the
// file system, backupPath and the temp dir as the screen left them, and
// ./hardn.yml as the screen left it. The pauses say "Press Enter", where origin/main
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
			prepareBackup(t, tc, dir)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestLegacyMenuBackup$")
			cmd.Env = append(os.Environ(), backupChild+"="+tc.name, backupDir+"="+dir, "NO_COLOR=", "RUNEWIDTH_EASTASIAN=0", "HARDN_CONFIG=",
				"HOME="+filepath.Join(dir, "home"))
			cmd.Dir = dir
			cmd.Stdin = strings.NewReader(strings.ReplaceAll(tc.script, backupTmp, dir))
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
			fmt.Fprintf(&b, "# The temp dir afterwards: %s\n", tree(t, dir))
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

// startWords says what is at backupPath when the case starts, and what
// else the temp dir holds.
func startWords(tc backupCase) string {
	var s string
	switch tc.at {
	case atSentinel:
		s = fmt.Sprintf("a directory holding .write_test, %d bytes of the user's", len(sentinel))
	case atDir:
		s = "an empty directory"
	case atFile:
		s = "a regular file"
	default:
		s = "nothing there"
	}
	if tc.shared {
		s += "; " + backupTmp + "/shared a directory anyone can write to"
	}
	if tc.afile {
		s += "; " + backupTmp + "/afile a regular file"
	}
	return s
}

// prepareBackup builds what case tc starts from in dir: what is at
// backupPath, and $TMP/shared or $TMP/afile.
func prepareBackup(t *testing.T, tc backupCase, dir string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(backupPathUnder))
	var err error
	switch tc.at {
	case atDir, atSentinel:
		err = os.MkdirAll(path, 0o755)
		if err == nil && tc.at == atSentinel {
			err = os.WriteFile(filepath.Join(path, ".write_test"), []byte(sentinel), 0o644)
		}
	case atFile:
		err = os.MkdirAll(filepath.Dir(path), 0o755)
		if err == nil {
			err = os.WriteFile(path, []byte("not a directory\n"), 0o644)
		}
	}
	if err == nil && tc.shared {
		shared := filepath.Join(dir, "shared")
		if err = os.Mkdir(shared, 0o755); err == nil {
			err = os.Chmod(shared, 0o777) // past the umask
		}
	}
	if err == nil && tc.afile {
		err = os.WriteFile(filepath.Join(dir, "afile"), []byte("not a directory\n"), 0o644)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// tree lists what is under dir, each path relative to it, a directory's
// ending in /, in order.
func tree(t *testing.T, dir string) string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if d.IsDir() {
			rel += "/"
		}
		paths = append(paths, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	return strings.Join(paths, " ")
}

// runBackupChild runs the Backup screen of case name, in the child
// process, under dir: hardn's defaults for alice, backupPath moved under
// dir, the BackupManager built as infrastructure.ServiceFactory builds it,
// over the real file system, recorded. Then it reports, after
// childTrailer, what the repository asked of the file system and what is
// at backupPath.
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
	cfg := config.DefaultConfig()
	cfg.Username, cfg.SshPort = "alice", 2230
	cfg.EnableBackups, cfg.BackupPath, cfg.DryRun = tc.enabled, path, tc.dryRun

	log := &trace{}
	repo := secondary.NewFileBackupRepository(recordingFS{inner: interfaces.OSFileSystem{}, log: log}, interfaces.NewMockCommander(), cfg.BackupPath, cfg.EnableBackups)
	backups := application.NewBackupManager(service.NewBackupServiceImpl(repo))
	mm := application.NewMenuManager(nil, nil, nil, nil, nil, backups, nil, nil, nil, nil)
	menu.NewBackupMenu(mm, cfg).Show()

	fmt.Print(childTrailer)
	fmt.Printf("# The repository's file system calls:\n%s", log)
	fmt.Printf("# %s after: %s\n", path, onDisk(path, tc.at == atSentinel))
}

// onDisk says what is at path once the screen has returned: nothing, a
// regular file, or a directory and its entries, and with withSentinel
// what became of the user's .write_test.
func onDisk(path string, withSentinel bool) string {
	if fi, err := os.Lstat(path); err == nil && !fi.IsDir() {
		return "a regular file"
	}
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

