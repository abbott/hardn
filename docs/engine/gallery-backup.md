# Backup in the gallery, before any build (slice 3, unit (a): `backup`, `play backup`)

> **Unit:** refactor plan slice 3, unit (a), `next/s3-gallery`: "Gallery mock and full sequence, reviewed first" (plan §2, "Phases 3–5 together", §3.22). Slice table row 3: "Backup | `05-BACKUP.png` (copied-row drift) | No backups; dry-run writes a test file; no restore | §3.16". Decisions cited, not reopened: §3.16 (approved 2026-10-17: per-run `<backupPath>/<id>/` with a manifest; `enableBackups=false` still takes rollback-required backups, and says so); §3.19 as amended (2026-10-22: a save edits only the keys that changed); §3.15's compact main menu withdrawn (2026-10-23); P-03 (decided 2026-10-23: one "Plan only" mode set at start). Proposals shown: [P-19](../refactor/proposals.md#p-19) (a restore UI), [P-31](../refactor/proposals.md#p-31) (Figma's words or origin/main's, 05-BACKUP), [P-29](../refactor/proposals.md#p-29) (a), [P-07](../refactor/proposals.md#p-07) (b), [P-12](../refactor/proposals.md#p-12). V-numbers: V18; side-effects rows 29, 30 ([side-effects.md](../refactor/phase0/side-effects.md)).
> **Status:** built and tested on macOS, in process. No hardn binary ran. The mocks read simulated hosts; the characterization runs pkg/menu's Backup screen in a child test process on scripted stdin, in a temp dir, on the real file system there. **Every word new to origin/main is proposed** (§6), and every behaviour below is a proposal for the maintainer's review: nothing is wired into `cmd/hardn`, `pkg/menu`, `pkg/engine` or `pkg/config`.

## 1. Review description

1. **Purpose and scope.** What origin/main's Backup screen shows and does, option by option, live and under plan only (§2); the slice's three defects confirmed or corrected, and others found (§7); a proposed Backup screen on the design system, drawn from what real code reads of seven simulated hosts, with its flows on the real engine (§3); ten decisions (§5). In scope: characterization tests of the Backup screen and of whether hardn's changes take backups (additions only), the gallery's `backup` mode (`backup.go`, its tests, the dispatch in `main.go` and `play.go`, a read-only directory in the fake host), their goldens, this page, a bullet in the design-system README's review guide and a line in P-19's entry.
2. **Behaviour changes:** none to hardn. No component, layout, engine, config or menu code changed. `fakehost.go` gains `readOnly` (a directory whose subtree refuses `MkdirAll` and `WriteFileAtomic` with `EROFS`), used only by the `readonly` host.
3. **Tests added.** `pkg/characterization/menu_backup_test.go`: `TestLegacyMenuBackup`, 20 cases (§2). `pkg/characterization/backup_calls_test.go`: `TestLegacyBackupFile` (origin/main's two `BackupFile` functions run on real files), `TestLegacyOperationsTakeNoBackups` (every operation of the operations goldens, backups on: nothing written under `backupPath`), `TestEngineBackupRun` (`pkg/host/backup`'s run of one change, beside them). `internal/tools/uigallery/backup_test.go`: `TestBackupGolden` (13 listing mocks on the core set and under LANG=C), `TestPlayBackupGolden` (9 transcripts), `TestBackupReadings` (each host's screen held to `config.Read`, `backup.Check` and `backup.Load`), `TestPathCheckIsCheck` (the path field held to `backup.Check` on 13 entries), `TestBackupRefusesEveryChange` (today's engine refuses every change on the unsafe and relative hosts, the DNS control's among them, with no words of its own; the mock's path change, planned under itself, is not), `TestBackupTakenAnyway`, `TestRunFilesAreBackups` (the seeded runs byte for byte against what `pkg/host/backup` writes), `TestBackupRestore`, `TestBackupFigmaDrift`, `TestBackupHeights` (at 80×24 and 80×40), `TestBackupDrawAndVerifyWriteNothing`, `TestBackupUsage`. `TestPlayTouchesNothing` takes the new file as it comes.
4. **Golden diffs.** Added only: `pkg/characterization/testdata/golden/menu/backup_*.golden` (20) and `.../backups/*.golden` (3); `internal/tools/uigallery/testdata/golden/backup/*.golden` (13); `.../play/backup-*.golden` (9). No existing golden changed.
5. **UI.** No component changed, so no deviation; freeze screenshots deferred (design-system README §9.16).
6. **Inferred screens.** The Backup screen is Figma's 05-BACKUP, its two status rows left out (decision 1; `figma` draws the frame as it is); its flows, the runs and a run are "inferred from origin/main" (Figma has the menu only: [interaction.md](../refactor/phase0/interaction.md) §9.4, "Backup sub-flows"), the runs and the restore are P-19's. v0.3.2's Backup Settings had two settings and two items (`sub-menu/05-backup`'s source note).
7. **New dependencies:** none.
8. **Per-distro notes:** none run. The mocks use a simulated Debian 13 host; what `pkg/host/backup` has not run on Linux is [atomic.md](../safety/atomic.md) §4's.
9. **`git status --short --ignored`** before each commit: this unit's paths alone; once, an ignored `uigallery` binary a build had left at the root, removed before the docs commit.

## 2. What origin/main does

Each claim is **run** (a golden of `TestLegacyMenuBackup`, `menu/backup_<case>.golden`; of `backup_calls_test.go`, `backups/<name>.golden`; or a named test) or **read** (the code cited). Line numbers are origin/main's (9d46866); its Backup files are the trunk's but for the pauses' "Press Enter" (DEV-050), on the same lines. In the goldens the case's temp dir is `$TMP`, also the child's working directory, and `HOME` is `$TMP/home`; the repository's file system is the real one, each call recorded, as `infrastructure.ServiceFactory.CreateBackupManager` gives it (service_factory.go:221-235, read).

### 2.1 Reaching it

- **Main menu 5**, "Backup" "Configure Hardn backup settings" (`main_menu.go:454`; trunk `:716`, read).
- **Under plan only** (`-n`, or hardn.yml's `dryRun: true` since slice 2b), the trunk refuses 5 under the prompt, "Not available in plan only." (run: pkg/menu `TestPlanOnlyRefusals`). origin/main opened it under `-n`, and nothing in `backup_menu.go`, `backup_manager.go`, `backup_service.go` or `file_backup_repository.go` reads the mode (read). Each option does under `-n` what it does live (run: `backup_open_dry_run`, `backup_toggle_on_dry_run`, `backup_path_dry_run`, `backup_verify_dry_run`): the directory made, `.write_test` written and removed, `./hardn.yml` saved. The trunk's save writes `dryRun` as loaded (`config.SaveConfig`, slice 2b decision 8; run: those goldens, `dryRun: false`); origin/main's wrote the session's, `true` under `-n` (read: 9d46866 `config.go:445-447`).

### 2.2 The screen (run: `backup_open`, `backup_open_missing`, `backup_open_file`, `backup_disabled_3`)

- Cleared, the green `~~~` line, "Backup Settings", "Current Backup Configuration:".
- "⛛ Backups Enabled" (or "✗ Backups Disabled"), "ℹ Backup Path <path>".
- **With backups on, a write test on every draw** (`backup_manager.go:80-102`): `os.Stat`, then `os.WriteFile(<path>/.write_test, "test", 0644)` and `os.Remove`, outside the repository. Then "✓ Backup directory exists and is writable", or "⭘ Backup directory doesn't exist or isn't writable" and "- Directory will be created when needed", for a regular file at the path too (`backup_open_file`). A user's own `.write_test` there is overwritten, then deleted (run: `backup_open`, "the user's .write_test (25 bytes) is gone").
- Items: "1) Toggle backups (currently: Enabled)" "Enable or disable automatic backups of modified files"; "2) Change backup path" "Current: <path>"; "3) Verify backup directory" "Test if backup directory exists and is writable", only with backups on; "0) Return to main menu"; "❯ Enter selection [0-3 or q]:" (`[0-2 or q]` with backups off).
- 17 lines down to the prompt with the directory there, 18 without it.

### 2.3 Option 1, the toggle (run: `backup_toggle_off`, `backup_toggle_on`, `backup_toggle_on_fails`, `backup_toggle_on_dry_run`)

- **Off:** "ℹ Backups have been disabled", "- No automatic backups will be created". No file system call.
- **On:** the repository's `SetBackupConfig` (`file_backup_repository.go:248-259`) runs `VerifyBackupDirectory` (`:220-238`): `MkdirAll(<path>, 0755)`, `WriteFile(<path>/.write_test, 0644)`, `Remove`; the directory is left on disk. Then "✓ Backups have been enabled", "- Modified files will be backed up to: <path>".
- **Either way** it saves the whole configuration to `./hardn.yml` (`backup_menu.go:153-161`; row 29), pauses, and draws the screen again by calling itself (`:165-167`).
- **A failed enable** (a regular file at the path: "✗ Error toggling backups: failed to create backup directory …: not a directory") saves nothing, yet the screen drawn again says Enabled and shows item 3: the repository set `Enabled` before its check (`:250-256`), and nothing undoes it.

### 2.4 Option 2, the path (run: `backup_path_relative`, `backup_path_empty`, `backup_path_unsafe`, `backup_path_tilde`, `backup_path_fails`, `backup_path_dry_run`)

- "- Current backup path: <path>", "- Enter new backup path: " (`ReadInput`).
- Nothing typed: "- Backup path unchanged".
- Otherwise `SetBackupDirectory` (`backup_manager.go:46-56`): a leading `~` becomes `os.UserHomeDir()`, so `~alice/backups` is `$HOME/alice/backups` (run), root's home under sudo (read); **no other check**: with backups on, the repository's write test of the new path (`MkdirAll` 0755, `.write_test`). `backups` is taken, made under the working directory and saved, `backupPath: backups`; so is a path under a directory anyone can write to (`$TMP/shared/hardn`, `$TMP/shared` 0777).
- **A failed change** (a path under a regular file) saves nothing, yet the screen drawn again shows the new path as the one in use: the repository set it before its check (`file_backup_repository.go:248-256`).

### 2.5 Option 3, Verify (run: `backup_verify`, `backup_verify_dry_run`, `backup_verify_fails`, `backup_disabled_3`)

- "- Verifying backup directory: <path>", then the repository's `VerifyBackupDirectory`: `MkdirAll` 0755, write and remove `.write_test`; "✓ Backup directory is valid and writable", or "✗ Backup directory verification failed: …" and "- Please choose a different backup path".
- **With backups off** item 3 is not shown, yet 3 is taken: a pause, then the screen (`backup_menu.go:210-233`).
- Opening the screen and Verify answer one question by two code paths, `os` and the repository (`backup_manager.go:80-102`, `file_backup_repository.go:220-238`).

### 2.6 An invalid key, 0, q (run: `backup_invalid`, `backup_return`, every case's q)

- An invalid key: "✗ Invalid option. Please try again.", a pause, the screen again.
- 0 and q return to the main menu, saving nothing.

### 2.7 What the settings do

- **On origin/main, nothing.** `utils.BackupFile` (`utils.go:77`) and the repository's `BackupFile` (`file_backup_repository.go:40`, through `BackupManager.BackupFile`, `backup_manager.go:26`, and `BackupServiceImpl`, `backup_service.go:62`) have no caller; nor have `ListBackups`, `RestoreBackup` and `CleanupOldBackups` (`:78`, `:117`, `:153`) (read: `git grep -n 'BackupFile('` at 9d46866; [side-effects.md](../refactor/phase0/side-effects.md) §1 item 9, V18). No operation of the operations goldens, on any cell, with backups on at `/var/backups/hardn`, writes under it or makes a `.bak` copy (run: `backups/legacy_operations.golden`; the test fails if one does).
- **v0.3.2 had fifteen calls** (read: `git grep -n 'BackupFile(' v0.3.2`: dns 3, firewall 1, packages 4, ssh 5, user 2, of `utils.BackupFile`): origin/main's move to layers dropped them, a regression.
- **What the legacy functions would do** (run: `backups/legacy_backupfile.golden`): copy to `<backupPath>/<date>/<name>.<HHMMSS>.bak`, making the day's directory before they look for the file, so a missing file leaves it empty; `utils.BackupFile` skips under `cfg.DryRun`, the repository's does not read it.
- **On the trunk, the engine's backups** (read; run: `backups/engine_run.golden`, `TestBackupTakenAnyway`). Every engine change (the DNS screen, `-g`) backs up its `Backups` into `<backupPath>/<run-id>/`, 0700, a numbered copy and `manifest.json` at 0600, as `backup.RollbackRequired` (`pkg/engine/apply.go:296`, `:411-412`), checked first at plan time (`plan.go:224-228`). The settings come from hardn.yml as loaded (`cmd/hardn/engine.go:76`), which is the configuration the legacy Backup screen edits in the same session (`cmd/hardn/main.go:201-202`, `dns.go:109-110`).
- **`enableBackups: false` skips almost nothing today**: `pkg/host/backup` skips an `Optional` backup and takes a `RollbackRequired` one, "taken although enableBackups is false: the rollback needs it" (run: `backups/engine_run.golden`); every engine backup is rollback-required, so taken anyway; only the boot unit's backup of a file it replaces is `Optional` (read: `pkg/engine/boot.go:42`).

## 3. The mocks

### 3.1 What to run

From the repository root, with `GOTOOLCHAIN=local GOWORK=off` (add `-profile plain`, `-ascii`, `-width 60` or `-theme light`):

```text
go run ./internal/tools/uigallery backup                         # every mock under its note
go run ./internal/tools/uigallery backup screen-default verify   # some of them; -list names them
go run ./internal/tools/uigallery play backup                    # the hosts' menu
go run ./internal/tools/uigallery play backup/default            # the screen on one host
go run ./internal/tools/uigallery play -sequence dry-run backup/default   # plan only
```

The hosts are Debian 13 (`web-01.example.net`), hardn.yml as `hardn.yml.example` writes the two keys:

| Host | hardn.yml / host | Path row |
|---|---|---|
| `default` | defaults; `/var/backups/hardn` not made yet | `/var/backups/hardn` Not created yet |
| `off` | `enableBackups: false` | Not created yet; Disabled, "Rollback backups are still taken" under it |
| `elsewhere` | `backupPath: /srv/backups/hardn`, root's, 0700 | Root only |
| `unsafe` | `backupPath: /tmp/hardn-backups`, `/tmp` world-writable | Unsafe |
| `relative` | `backupPath: backups`, as origin/main's screen saves it | Not absolute |
| `readonly` | `/var/backups` on a read-only file system | Not created yet (Check cannot see it) |
| `runs` | three earlier runs in `/var/backups/hardn`: an SSH port change, a DNS change, a save with backups off | Root only; 3 runs |

| Listing mock | What it shows |
|---|---|
| `screen-<host>` | The screen on each host: the title bar, the section with Backup State (with backups off, the muted line under it) and Backup Path (its note from `backup.Check`), items 1 to 4, Return, the prompt; no status rows (decision 1 (a)) |
| `figma` | Figma's frame as drawn (`sub-menu/05-backup`), its two copied rows among them (decision 1 (c)) |
| `status-row` | Decision 1 (b): one status row of its own, "Backups Enabled", the section keeping Backup Path alone |
| `verify` | Verify's report on `default`, `elsewhere`, `unsafe`, `relative` |
| `runs` | Backup Runs (4): the runs, newest first, each "1 file, taken anyway" or "2 files" (P-19) |
| `run` | One run: its id, each file and its size, "taken anyway" in `status.warn`, item 1 Restore (P-19) |
| `run-dir` | That run's directory: `0001-hardn.yml` and `manifest.json`, each 0600, then the manifest as written |

| Play golden | Keys | What it shows (task item) |
|---|---|---|
| `play/backup-toggle` | `backup/default`: 1, y, 1, y, 0 | Off then on: the review, the plan (the save of `enableBackups` alone, in place), y/N, the step, the result, "Backup of /etc/hardn/hardn.yml kept in" and the run's directory, the screen read again; turning off adds "Backed up anyway: the rollback needs it." (§3.16), turning on never does |
| `play/backup-path` | `backup/default`: 2, `backups`, `/var/backups//hardn/`, `/tmp/hardn`, `/etc/os-release`, `/var/backups/hardn`, `/srv/backups/hardn`, y, 0 | Inline errors: not absolute, not clean, unsafe, not a directory, the path in use (a warning); then the save of `backupPath`, its own backup kept under the new path; also under LANG=C, `(x)` |
| `play/backup-off` | `backup/off`: 3, 1, n, 0 | Verify writing nothing; turning backups on declined |
| `play/backup-path-from-unsafe` | `backup/unsafe`: 3, 1, 2, `/var/backups/hardn`, y, 1, y, 0 | Verify's refusal; item 1 refused before it asks; a new path saved, planned under itself (decision 7 (a)); item 1 again, now planned |
| `play/backup-relative` | `backup/relative`: 3, 1, 0 | The same refusals for a relative path |
| `play/backup-readonly` | `backup/readonly`: 3, 1, y, 0 | Verify passes; the change fails at its backup (`EROFS`), nothing changed |
| `play/backup-runs` | `backup/runs`: 4, 3, 1, 0, 2, 1, y, 0, 0, 0 | The runs; the SSH run's restore refused where it is chosen; the DNS run's restore: the review, a plan of two files put back with their diffs, y, the engine's apply, where it kept what it replaced (P-19) |
| `play/backup-dry-run` | `-sequence dry-run backup/default`: 1, 2, `/srv/backups/hardn`, 3, 0 | Plan only: each change stops at its plan, "No actual changes were made to your system."; Verify as live |
| `play/backup-hosts` | `backup`: 7, 4, 0, 0, 0 | The hosts' menu, the runs host's list, back |

The playable main menu's 5 still opens the Figma frame's mock (`sub-menu/05-backup`), which `TestPlayReaches` keeps reachable; the Backup mocks start at `play backup`.

### 3.2 The frame's two status rows (the copied-row drift)

05-BACKUP draws two status rows, "Backups Enabled UFW installed, deny policy" and "SSH Port 2230/tcp non-default" (read: `05-backup-tree.v2.json`, `05-backup-spec.v2.md`). Both are other screens' (read: the export's trees):

- **"SSH Port 2230/tcp non-default"** is 02-SSH_LOGIN's second status row, whole: label, value and detail (`02-ssh-login-tree.v2.json`, beside "SSH Login Configured root disallowed"). In both frames its value and detail sit in a layer named FIREWALL.
- **"Backups Enabled"** is the row's own, but its detail, "UFW installed, deny policy", is a firewall's, in no other frame; the main menu's Firewall row says "Configured deny policy" (`00-tui-main-tree.v2.json`). Its value and detail sit in a layer named USERS.

A Backup screen can say neither the SSH port nor the firewall's policy. **The mocks treat them so:** `screen-*` draws no status row, its section saying the state and the path once each (decision 1 (a)); `status-row` draws one of its own, the state alone, the section keeping the path (b); `figma` draws the frame as Figma has it (c). `TestBackupFigmaDrift` holds the three.

## 4. Heights (run: `TestBackupHeights`, Unicode and ASCII, at 80×24 and at 80×40)

| Screen | Lines to the prompt | 80×24 | 80×40 |
|---|---|---|---|
| origin/main's (run: `backup_open`, `backup_open_missing`) | 17, 18 | fits | fits |
| Figma's frame as drawn (`figma`) | 19 | fits | fits |
| The mock, backups on (`default`, `elsewhere`, `unsafe`, `relative`, `readonly`, `runs`) | 17 | fits | fits |
| The mock, backups off (`off`) | 18 | fits | fits |
| With a status row of its own (`status-row`) | 18 | fits | fits |
| Backup Runs, three runs (`runs`) | 11 | fits | fits |
| One run (`run`) | 13 | fits | fits |

Each is the same number of lines at both heights: one form at every height. A sub-menu has no compact form, and none is proposed (the maintainer, 2026-10-23, of the main menu: no compact or two-column form). At 60 columns rows and items stack, a line each more.

## 5. Decisions for the maintainer

Each decision lists its options; the recommendation is the mock's unless said otherwise. Already decided and not reopened: §3.16, §3.19 as amended, P-03, the compact menu's withdrawal. Where one decision's answer changes another's, the one it depends on comes first, and the later one says so.

1. **The frame's two status rows (the copied-row drift, §3.2).** *Recommended:* (a) none: the section's two rows say what a status row would, and Backup has no status row on the main menu (mocked: `screen-*`). *Alternatives:* (b) one row of its own, the state: "Backups Enabled", Partial "rollback backups only" when off, Not Configured with Check's word when the path is refused; the section then keeps Backup Path alone, so nothing is said twice (`status-row`); (c) Figma's two as drawn (`figma`).
2. **Words, 05-BACKUP (P-31).** *Recommended:* origin/main's title, "Backup Settings": two words, and v0.3.2's title too (read: v0.3.2 `pkg/menu/menu.go:712`), where "Manage" adds nothing a screen does not do; Figma's "Manage Hardn Backups" also has the capital P-07 (b) drops. Figma's section, rows and item labels ("Backup Configuration", "Backup State", "Backup Path", "Verify Directory", "Return"); origin/main's prompt, "Enter selection", as hardn's main menu and DNS screen (P-29 (a)). *Alternatives:* (a) Figma's throughout, "Enter your choice" included; (b) origin/main's throughout ("Toggle backups (currently: Enabled)", "Return to main menu"); (c) "Manage hardn Backups", Figma's with P-07 (b).
3. **Verify, and the path's note.** *Recommended:* Verify is `backup.Check` with no path: absolute, the deepest existing directory a directory, it and every one above it root's (or hardn's user's) and writable by neither group nor others; **nothing is written**, under plan only too (run: `TestBackupDrawAndVerifyWriteNothing`, both modes, every host). The row's note says what Check found: "Root only", "Not created yet", "Unsafe", "Not absolute", "Not a directory". A read-only file system then shows only when a change fails at its backup (`play/backup-readonly`). *Alternatives:* (a) origin/main's write test of `.write_test` (§2.2; overwrites a user's file, writes under plan only); (b) Check plus the mount's `ro` option read from `/proc/self/mounts` (a helper for unit (b)); (c) Figma's note "Writable", which nothing establishes without a write.
4. **Item descriptions. Depends on decision 3.** *Recommended:* item 1 names the result, "Turn backups off" or "Turn backups on" (P-12's reasoning for "Toggle"); item 3 "Check the path, writing nothing", which holds only if decision 3 takes the recommendation (under 3 (a), origin/main's "Test if backup directory exists and is writable"); item 2 Figma's "Change backup path". *Alternatives:* Figma's "Toggle backups of modified files" and "Test if path is writable"; origin/main's.
5. **A path typed not clean** (`/var/backups//hardn/`, `/var/backups/../tmp`). `backup.Open` cleans it without a word. *Recommended (mocked, the mock's own rule):* refused inline, "Write it as /var/backups/hardn", so that the path saved is the path used. *Alternatives:* cleaned without a word; cleaned, the review and question naming the clean path, as the DNS screen's Add names an address as written.
6. **The path field's other refusals** are Check's, inline, the field asked again: "Not an absolute path", "Others can change /tmp", "Not a directory"; the path in use a warning, "<path> is the backup path already", as the DNS screen warns of a nameserver already configured. A path not there yet is taken: the first backup makes it, 0700. *Alternative:* the plan refuses them after the review, as the engine does today for the path in use.
7. **A host whose backup path is refused** (`unsafe`, `relative`). Today's engine refuses every change there at its plan, Check's error as it is (run: `TestBackupRefusesEveryChange`), **the path change too**, as its own backup of hardn.yml goes to the path refused, so no screen can correct it. *Recommended (mocked: `play/backup-path-from-unsafe`):* (a) a change of Backup's settings is planned under the settings it saves: a new path, which the field has checked, takes the change's own backup, and turning backups off takes it with them off (so decision 8's line follows the save); item 1 is refused before it asks on such a host, in Verify's words, as the DNS screen refuses before asking where every plan refuses. An engine input per plan, unit (b); the engine's other changes still take hardn.yml as loaded. *Alternatives:* (b) as today: refused, backupPath set in `/etc/hardn/hardn.yml` by hand; (c) the path change backs up under the default path.
8. **§3.16's "and says so" (approved; where it is said).** Today it reaches the manifest (`takenAnyway`, `backupsEnabled: false`) and `backup.Result`, never the user: `pkg/output` names neither (read). *Recommended (mocked):* the row "Disabled" in `status.warn` with the muted line under it, "Rollback backups are still taken"; the same line in the review of turning backups off; after any change applied, "Backup of <files> kept in" and the run's directory, read from its manifest, and under it, where the manifest says a copy was taken with backups off, "Backed up anyway: the rollback needs it." (never after turning them on, which decision 7 (a) plans with them on). For every change of hardn, not this screen's alone: `pkg/output`'s, unit (b) or (c). *Alternatives:* (a) the row alone; (b) a note per backed-up file in the plan; (c) as today.
9. **Backup Runs and a restore (P-19).** *Recommended:* item 4, "Backup Runs", listing the runs under the path in use, newest first, and each run's files, read only, in slice 3; the restore as its own later unit, a change kind on the engine with its journal, and confirm-or-revert for SSH and firewall files (P-19's risk). The mock plays a restore as an engine change (the gallery's `restoreControl`: a change per file that differs from its copy, its apply `backup.Restore`, the engine backing up and journalling each file first, so the restore can be rolled back), and refuses a run of SSH or firewall files where it is chosen (`play/backup-runs`, `TestBackupRestore`), to show how it would be reached. *Alternatives:* (a) the list and the restore both in slice 3; (b) no item 4: `hardn rollback <id>` for a change still journalled, nothing after; (c) a command, `hardn restore <run-id>`.
10. **Plan only and item 5 (P-03 decided; slice 2b refuses 5).** *Recommended:* once unit (d) replaces the legacy screen, lift 2b's refusal: the screen opens under plan only, Verify and the runs read only, and each change stops at its plan with "No actual changes were made to your system." (`play/backup-dry-run`), as the DNS screen under plan only. *Alternative:* keep 5 refused under plan only.

Also open, not mocked, for the maintainer: **a path change leaves the earlier runs where they were** (under decision 7 (a), the change's own backup goes under the new path), and Backup Runs then lists the new path's only; and **nothing prunes runs** (origin/main's `CleanupOldBackups` has no caller; nothing in `pkg/engine` or `pkg/host/backup` removes a run), so `<backupPath>` grows with every change: retention is a proposal of its own, if wanted.

## 6. Words

origin/main's where it has them; **every other word is proposed**. The flow's own words are P-27's, origin/main's and Figma's ("Operation Successful", "Operation Cancelled", "Operation Failed", "No actual changes were made to your system.", "Save the configuration" from `steps.PlanSave`).

| Where | Words | Source |
|---|---|---|
| Title bar | "Backup Settings" | origin/main (decision 2) |
| Section, rows | "Backup Configuration"; "Backup State" "Enabled", "Disabled"; "Backup Path" | Figma |
| Path notes | "Root only", "Not created yet", "Unsafe", "Not absolute", "Not a directory", "Can't tell" | **proposed** (decision 3) |
| Backups off | "Rollback backups are still taken" (muted, under Backup State) | **proposed** (decision 8) |
| Status row, decision 1 (b) | "Backups" "Enabled"; "Partial" "rollback backups only"; "Not Configured" "unsafe", "not absolute", "not a directory", "can't tell" | **proposed** |
| Items | "Backup State" "Turn backups off" / "Turn backups on"; "Backup Path" "Change backup path"; "Verify Directory" "Check the path, writing nothing"; "Backup Runs" "None yet" / "1 run" / "2 runs"; "Return"; "Select an option"; "Enter selection" | Figma's labels; descriptions **proposed** but Figma's "Change backup path"; item 4 **proposed** (P-19); origin/main's prompt |
| Title bars of the flows | "Turn Backups Off", "Turn Backups On", "Change Backup Path", "Backup Runs", "Backup Run", "Restore Backup Run" | **proposed** |
| Path field | "Enter new backup path"; "Backup path cannot be empty", "Not an absolute path", "Write it as <path>", "Others can change <dir>", "Not a directory", "<path> is the backup path already" (a warning), "Can't check it: <error>" | origin/main's question without its colon; the rest **proposed** |
| Questions | "Turn backups off?", "Turn backups on?", "Use <path>?", "Restore this run?" | **proposed** |
| A refused path: Verify's warning, and item 1's before it asks | "Others can change <dir>, so backups can't go in <path>. Choose a directory only root can change."; "\"<path>\" is not an absolute path. Start it with /."; "<path> is not a directory. Choose another path."; "Can't check <path>: <error>." | **proposed** |
| Verify | "Only root can change <path>. Nothing was written."; "Only root can change <dir>, where the first backup makes <path>. Nothing was written."; after a refusal, "Nothing was written." | **proposed** |
| After a change applied | "Backup of <files> kept in" and `<backupPath>/<run-id>`; "Backed up anyway: the rollback needs it."; a warning, "Can't read the backup in <dir>: <error>" | **proposed** (decision 8) |
| Runs | "Select a run"; "2026-10-19 14:30" (UTC, to the minute) "1 file, taken anyway" / "2 files"; "No change has been backed up yet." | **proposed** |
| A run | "Run <run-id>"; each path, "<n> bytes" / "Not there before" / "Link to <target>", "taken anyway"; "Restore" "Put these files back" | **proposed** |
| Restore | "Backup Run" "Run" "Taken" "<time> UTC"; "Restore <path>", "Remove <path>"; for a run of SSH or firewall files, "Restoring SSH or firewall files needs the typed confirmation and the revert timer. This mock does not plan it (P-19)." (the mock's note, not hardn's) | **proposed** |
| The engine's refusal of any change of hardn while backupPath is refused (the DNS screen's, `-g`), as `pkg/output` reports a refusal: Why, then "To go on:" | Why: the refused path's sentence above ("Others can change /tmp, so backups can't go in /tmp/hardn-backups"); To go on: "choose a directory only root can change, with Backup's item 2"; the codes as today, UNSAFE_DIRECTORY, BACKUP_SETTINGS_INVALID. Today: "refused at plan time: engine: the backups: backup: /tmp: directory another user can change" (run: `TestBackupRefusesEveryChange`; §7 item 12) | **proposed**, not mocked: the engine's, unit (b) |
| A backup that cannot be written | "Can't back up <file> in <backupPath>: read-only file system. Nothing was changed.", a code of its own (unit (b) names it). Today: "Save the configuration to /etc/hardn/hardn.yml: backup: mkdir /var/backups/hardn: read-only file system (UNCLASSIFIED_ERROR)" (run: `play/backup-readonly`; §7 item 13) | **proposed**, not mocked: the mock shows today's |
| The gallery's, dev-only | "Backup hosts", "Select a host", each host's label, about and note | the gallery's |

## 7. Defects

The slice's three:

| Listed | Verdict | Evidence |
|---|---|---|
| No backups | **Confirmed for origin/main**, a regression from v0.3.2, which called `utils.BackupFile` from fifteen places: no legacy change backs up; the toggle's "Modified files will be backed up to" is false. **Corrected for the trunk**: every engine change backs up into `<backupPath>/<run-id>/`, whatever `enableBackups` says; the legacy menus' writes (User Management, SSH Login, Firewall, Environment, Run All) still back up nothing | read: `git grep` at 9d46866 and at v0.3.2 (§2.7); run: `backups/legacy_operations.golden`, `backups/legacy_backupfile.golden`, `backup_toggle_on`; read: `apply.go:296`, `:411-412`; run: `backups/engine_run.golden`, `TestBackupTakenAnyway` |
| Dry run writes a test file | **Confirmed, three places**, each `<backupPath>/.write_test`: opening the screen with backups on (`os.WriteFile`, `backup_manager.go:91-99`), Verify, and turning backups on or setting a path while on (the repository, `file_backup_repository.go:220-238`, each with `MkdirAll` 0755, and `./hardn.yml` saved). **Corrected:** since `next/s2b-replace` hardn refuses 5 under plan only, so it no longer reaches them in that mode; live they still write | run: `backup_open_dry_run`, `backup_verify_dry_run`, `backup_toggle_on_dry_run`, `backup_path_dry_run`; run: pkg/menu `TestPlanOnlyRefusals` |
| No restore | **Confirmed for origin/main** (`RestoreBackup` uncalled, no UI). **Corrected for the trunk**: the engine restores a run during a failed apply, and `hardn rollback <id>` while a change is journalled; once a change is done its run is kept and nothing restores it (P-19) | read: `file_backup_repository.go:117`; [revert.md](../safety/revert.md) §2, "Rolling back"; [engine.md](engine.md) §2 items 4, 12 |

Others found:

| # | Defect | Evidence |
|---|---|---|
| 4 | The write test overwrites and deletes a user's `<backupPath>/.write_test` | run: `backup_open` |
| 5 | A failed enable leaves the screen saying Enabled for the session, unsaved | `file_backup_repository.go:248-256`; run: `backup_toggle_on_fails` |
| 6 | A relative path, or one under a directory anyone can write to, is taken, made and saved; the engine refuses every change with it, the DNS screen's next change in the same session included, since both use one configuration | `backup_manager.go:46-56`; `cmd/hardn/main.go:201-202`, `dns.go:109-110` (read); run: `backup_path_relative`, `backup_path_unsafe`, `TestBackupRefusesEveryChange` |
| 7 | `~` expands to the home of the user hardn runs as, root's under sudo; `~alice/backups` is `$HOME/alice/backups` | `backup_manager.go:47-53` (read); run: `backup_path_tilde` |
| 8 | Item 3 hidden with backups off, yet taken | `backup_menu.go:101-108`, `:210-233`; run: `backup_disabled_3` |
| 9 | Every action saves the whole configuration to `./hardn.yml` (row 29) | `backup_menu.go:157`, `:195`; run: `backup_toggle_off`, `backup_path_relative` |
| 10 | The legacy directory is made 0755, where `pkg/host/backup` makes runs 0700 and refuses a directory group or others can write | `file_backup_repository.go:222`; `pkg/host/backup` `prepare`, `trustedDir` (read); run: `backups/engine_run.golden` |
| 11 | Each action pauses, then re-enters the screen by recursion | `backup_menu.go:165-167`, `:206-208`, `:231-233`, `:243-245` (read; run: the pauses counted in each golden) |
| 12 | The engine's refusal of an unsafe or relative `backupPath` is Check's error as it is, with no Why or Fix | run: `TestBackupRefusesEveryChange` (no `steps.Refusal`; UNSAFE_DIRECTORY, BACKUP_SETTINGS_INVALID) |
| 13 | A backup that cannot be written (a read-only file system) is reported `UNCLASSIFIED_ERROR` | run: `play/backup-readonly` |
| 14 | §3.16's "says so" reaches the manifest and `backup.Result`, not the user | `pkg/output` names no `TakenAnyway` (read) |
| 15 | Nothing prunes runs | read: no caller of `CleanupOldBackups`; nothing in `pkg/engine` or `pkg/host/backup` removes a run |
| 16 | `config.Edit` keeps a trailing comment, but not its column, when a value's length changes (`enableBackups: true` to `false` moves the comment one cell) | run: `play/backup-toggle` |
| 17 | A failed path change leaves the screen showing the new path as the one in use, unsaved | `file_backup_repository.go:248-256`; run: `backup_path_fails` |
| 18 | A regular file at the path reads "Directory will be created when needed" | `backup_menu.go:80-83`, `backup_manager.go:87-96`; run: `backup_open_file` |
| 19 | The legacy `BackupFile` functions make the day's directory before they look for the file, so a missing file leaves an empty one | `file_backup_repository.go:46-58`, `utils.go:89-98`; run: `backups/legacy_backupfile.golden` |

12, 13 and 14 are the engine's and `pkg/output`'s, for unit (b) or (c); 16 is `config.Edit`'s, cosmetic; 19 is moot while nothing calls them.

## 8. What the mocks take from real code, and what not

**Real:** hardn.yml read by `config.Read`, its settings `Config.BackupSettings`; the path's state, Verify and the path field's refusals, `backup.Check` (`TestBackupReadings`, `TestPathCheckIsCheck`); each change's plan, review, y/N and apply, `output.FlowEngine` on `engine.New`, and the plan the save of one setting, `steps.PlanSave` (`config.Edit` in place); the engine's refusals and failures, as reported; the configuration adopting each save, as the DNS screen does (`adopt`); what a change kept and "taken anyway", read from the run's manifest with `backup.Load` (`TestBackupTakenAnyway`); the runs, `backup.Load` over each directory; the restore's apply, `backup.Restore` inside an engine change; the layout, `screen.SubMenu`, and every component.

**Not, the mock's own:**
- the words of §6;
- the path field's "not clean" rule (decision 5);
- the engine's backups taken as the settings a change saves (decision 7 (a)), where `cmd/hardn`'s `engineOptions` takes hardn.yml as loaded;
- the directory named in "Others can change <dir>", read from Check's error text: `pkg/host/backup` has no typed error carrying it;
- the runs host's three runs, seeded by the gallery with fixed times, held byte for byte to what `pkg/host/backup` writes for the same files but the creation time (`TestRunFilesAreBackups`): a real run's manifest takes the wall clock;
- the runs list (`ReadDir`, then `backup.Load` on each), which no package offers;
- the "backup.settings" control around `steps.PlanSave`: no control of hardn has it;
- the restore's control, `restoreControl` (a change per file that differs from its copy), and its refusal of a run of SSH or firewall files, a note where P-19 asks for the typed confirmation and the revert timer.

**Live helpers the build would need** (not added, as this unit changes no live code): a backup control (unit (b)); a refusal of Check that names its directory, with words (Why, Fix) for each of Check's refusals and a code for a backup write that fails; a run listing in `pkg/host/backup`; the engine's backups taken per plan for decision 7 (a); the "kept" and "taken anyway" lines in `pkg/output` for decision 8; a restore change kind for decision 9.

## 9. Not in this unit

- **(b) The control**, on the engine, with its own goldens: Backup's settings as a control (`Check`; `Plan` the save of `enableBackups` and `backupPath`), the refusals' words, and decisions 7 and 8's engine side. **It begins by extracting the DNS screen's shared glue out of `pkg/screens/dns`** (the planning adapter around `output.FlowEngine`, `adopt`, the refusal words) **and the parity harness out of `pkg/screens/dns`'s tests into shared packages**, slice 3 being their second user ([parity-dns.md](parity-dns.md)).
- **(b2) Parity**: origin/main's end state against the new path's (hardn.yml's two keys; what is written under `backupPath`), each difference tied to a V-number or a decision; the characterization goldens of §2 are its legacy side.
- **(c) The screen** on the design system and prompt kit, `pkg/screens/backup`, retiring these mocks.
- **(d) The legacy path replaced**: main menu 5 to the new screen, 2b's plan-only refusal lifted if decision 10's recommendation is taken, `backup_menu.go` and the legacy backup layers deleted after (b2), their goldens frozen as the parity record.

## 10. Size

Non-test Go: about 1,255 lines added (`backup.go` 1,190, about 215 of them whole-line comments; `fakehost.go` 27, `main.go` 21 and `play.go` 17 added, 7 removed in all). Tests: about 1,185 lines (`backup_test.go` 577, `menu_backup_test.go` 360, `backup_calls_test.go` 246). This page. Over the guideline of about 500 changed lines, as slices 2 and 2b's units (a) were: seven hosts, thirteen listing mocks and nine sequences, a restore on the engine, and the tests that hold each to real code; the split would have been the characterization, then the screen and its flows, then the runs and the restore. Testdata: 45 new golden files (23 characterization, 13 listing, 9 play), about 7,100 lines; no existing one changed.
