# Backup on the engine: Backup's control (`pkg/engine/controls/backup`)

> **Unit:** refactor plan slice 3 (Backup), unit (b), its second part, `next/s3-control`, from `next/s3-shared` at 555ffef (the first part: [screen-kit.md](screen-kit.md)). It builds on unit (a) ([gallery-backup.md](gallery-backup.md)), whose ten decisions the maintainer adopted on 2026-10-27 ("Adopt all ten slice 3 Backup recommendations in gallery-backup.md §5"). Decisions implemented: §3.16 (approved 2026-10-17: per-run `<backupPath>/<id>/` with a manifest; `enableBackups=false` still takes rollback-required backups, "and says so"); §3.19 as amended (2026-10-22: a save edits only the keys that changed); slice 3's decisions 3 (Check, writing nothing; the path's notes), 5 (a path not written clean refused, "Write it as <path>"), 6 (Check's refusals inline, the path in use a warning), 7 (a) (a change of Backup's settings planned under the settings it saves: the engine side) and 8 (what a change kept, and "taken anyway", in `pkg/output`: the engine side), and the run listing decision 9's read-only Backup Runs needs. V-numbers: V18 (no backup), on the engine's path. It fixes gallery-backup.md §7's defects 12, 13 and 14 (§8).
> **Status:** built and tested on macOS, in process, on `hostfake`, and `pkg/host/backup`'s helpers also on real files in `t.TempDir()` (run). Nothing has run on Linux or as root: every host claim is "written, not run (gate 5)" (§4). Nothing is wired into `cmd/hardn`, the main menu or the legacy Backup screen (units (c) and (d)); legacy and characterization goldens are unchanged (run: `git diff --stat 555ffef -- pkg/menu/testdata pkg/characterization` is empty). Every new word is **decided** and used exactly (§7): those decisions 2 to 9 name, and the rest of §6 and §7's, adopted on 2026-10-27 with `BACKUP_WRITE_FAILED`.

Backup's control is the second control that both checks and plans, and the first whose change moves where the engine keeps its backups. `Check` gives a screen what the section of the Backup screen says: the state and the path, the path's note from `backup.Check`, writing nothing. `Plan` gives the engine the save of `enableBackups` and `backupPath`. The engine plans and applies that save under the settings it saves, and every change of hardn now says what it kept, and where.

## 1. Why

origin/main's Backup screen writes a test file on every draw, takes a relative path or one under `/tmp`, and saves the whole configuration to `./hardn.yml`; its settings drive nothing (gallery-backup.md §2, §7). On the trunk, the engine backs up every change into `<backupPath>/<id>/`, but from `hardn.yml` as loaded: on a host whose `backupPath` is refused it refuses every change, the path change that would correct it among them, in Check's error as it is (defect 12); a backup that cannot be written is `UNCLASSIFIED_ERROR` (defect 13); and §3.16's "and says so" reached the manifest, never the user (defect 14). Unit (a) drew the replacement and the maintainer adopted it. This unit builds the control and the engine side of decisions 7 (a) and 8; unit (c) builds the screen on them.

## 2. Guarantees

### The reading (`Check`, `Read`)

1. **One control, `backup.settings`, titled "Backups"** (`Settings`; `Controls(cfg)`). It carries `hardn.yml` as loaded (`Settings.Config`), as the DNS control does, since the engine's `Check` takes no configuration; nil is hardn's defaults. `Applies` is true on every host.
2. **What is read** (`Read`): the two keys (`Config.BackupSettings()`), an empty `backupPath` taken as the default, `/var/backups/hardn`, as the engine takes it (`engine.New`); `backup.Check` of them with no file to back up; and whether `backupPath` is a directory already, a symlink to one followed (`Stat`), as Check and the backups follow it. So `Read` refuses the settings where the engine's plans refuse them, and nowhere else (run: `TestReadAsTheEngine`, six hosts, against the engine's plan of the DNS change). **Nothing is written** (decision 3): no `.write_test`, no directory made, under plan only too (run: `TestCheck`, `TestField`, every case's calls).
3. **The note** (`Reading.Note`, `Status.Detail`; decided, decision 3), in this order:
   - Check passes and the path is a directory, or a symlink to one: Configured, "Root only";
   - Check passes and the path is not there: Configured, "Not created yet" (the first backup makes it, 0700, atomic.md §2.10);
   - `backup.ErrSettings` (relative; an empty path is the default, item 2): Not Configured, "Not absolute";
   - a `backup.DirError` of `ErrUnsafeDir` (the path, or a directory above it, another user can change; `/tmp` included, sticky or not): Not Configured, "Unsafe";
   - a `backup.DirError` of `ENOTDIR` (the path, or a path above it, is a file): Not Configured, "Not a directory";
   - Check failing otherwise (a permission denied reading a directory): Unknown, "Can't tell", with Check's error.

   The state is the same with backups on or off: the engine's backups are rollback-required, and taken either way (§3.16). Tested: `TestCheck` (each note, an empty path made and not, a symlink to a directory, the DirError's directory, an error exactly for Unknown, nothing written), `TestReadAsTheEngine`, `TestGoldens/check`.
4. **Evidence:** `hardn.yml`'s two keys as the file gives them, with its path (`/etc/hardn/hardn.yml: enableBackups true, backupPath /var/backups/hardn`; `backupPath ""` for an empty one; none where no `hardn.yml` was read), then, of the path in use, Check's error, or `<path>: a directory`, or `<path>: absent`.

### The plan (`Plan`)

5. **The save of the two keys that changed**: `steps.PlanSave(ctx, sys, "backup.settings/save", in.Config, ["enableBackups", "backupPath"])`, `config.Edit` editing a key only where `in.Config`'s value differs from the file's (config-edit.md §2), the rest of `hardn.yml` and its comments as they were; nothing when neither changed (run: `TestPlan`, each diff's changed lines exactly). `in.Config` is the copy the caller edited (backups turned off, a new path); the engine's other changes take `hardn.yml` as loaded.
6. **A new path is refused first** (`backupPath` other than the file's, read now), with a report (`steps.Refusal`, §6): not absolute or empty (`BACKUP_SETTINGS_INVALID`), not written clean (decision 5: `/srv/backups//hardn/`, the same code), a directory another user can change (`UNSAFE_DIRECTORY`), a file on the path (`BACKUP_PATH_NOT_DIRECTORY`). A path not there yet is taken. The field checks the same before (`Field`); the plan checks again for a caller that did not. A path unchanged is not checked by the control: the engine refuses it, under the plan's settings (item 8).
7. **The save's own refusals** follow, each with its report (control-dns.md §6): no `hardn.yml` read, `CONFIG_NOT_LOADED`, even when nothing changes, as there is no file to compare with (§10, decision 10); a file gone, no longer decoding or changed since it was read, `CONFIG_CHANGED`; an edit `config.Edit` refuses. Tested: `TestPlan`, `TestPlanSaveRefusals` (nothing written by any).

### Decision 7 (a) on the engine

8. **A change of Backup's settings is planned and applied under the settings it saves.** The control is an `engine.BackupsSaver`: `SavedBackups(in)` is `in.Config.BackupSettings()`. When it plans a change, the engine takes the plan's backups under those settings (`Plan.Backups`), not `Options.Backups`: `backup.Check` refuses them at plan time, `Apply` backs up into `<their path>/<id>`, and the journal names that run, so that a rollback reads the backups there (`revert.Rollback` loads the journal's `Backup.Dir`, read: rollback.go:176, :316). A new path takes the change's own backup; turning backups off takes it with them off, so its manifest says `takenAnyway` and item 9 says so; turning them on takes it with them on. **Every other plan keeps `Options.Backups`**, `hardn.yml` as loaded (`cmd/hardn`'s `engineOptions`, unchanged), whatever `backupPath` the configuration it plans with gives. **A change of the backup settings is planned alone**: a plan with it and another control's change is refused (`ErrConflict`, in the saver's item, whichever comes first), since a plan's changes all back up into its one run, so no other change is ever backed up under the settings saved (§10, decision 2). An empty path is the default, as for `Options`; two savers that disagree conflict (`ErrConflict`); the plan made again under the run lock must have the same settings (`ErrPlanChanged`); the boot unit's change in a saver's lockout-risk plan says whether a file it replaces is backed up under the plan's settings, which the run is opened with. Tested on the engine: `TestPlanBackups`, `TestPlanBackupsAlone`, `TestPlanBackupsUnderTheLock`, `TestRollbackFromSavedPath`, `TestBootUnitUnderSavedBackups` (`pkg/engine`, synthetic controls); with this control and the DNS control: `TestRefusedPathMoved` (on a host whose `backupPath` is under `/tmp` or relative, the path change applies, `hardn.yml`'s `backupPath` line alone changed, its backup under the new path and nothing written under the old; the DNS change and turning backups off still refused, `UNSAFE_DIRECTORY` and `BACKUP_SETTINGS_INVALID`; the DNS change on a good host planned under the loaded settings, not its configuration's), `TestRollbackFromNewPath` (the path change and, in the control's own plan, a change after it that fails: `hardn.yml` put back from the new path's run, its manifest read there, the refused path untouched; the path change planned with the DNS change refused, `ErrConflict`), `TestTakenAnyway`.

### Decision 8: what a change kept (`pkg/output`)

9. **After any change applied** (`applied`, `awaiting-confirmation`), for every change of hardn (the DNS screen, `-g`, `hardn confirm`'s clean-up through `output.Emit`, read: `cmd/hardn/confirm.go:92`; the Backup screen to come): `engine.Result.Backup` is the backup run's manifest as written; `pkg/output` says, after "Operation Successful" and any countdown, before any finding, "Backup of <files> kept in" and the run's directory, and, where a copy was taken with backups off, "Backed up anyway: the rollback needs it." (decided, decision 8), in `Human` and in `AfterFlow` alike. Each file is named once, in the order backed up; **a path that was not there before is not named** (the run records it absent, and no copy of it was kept: §10, decision 3), so a change that only creates a file says nothing. Nothing after a dry run, a declined plan or a failed apply. The JSON document gains `result.backup` (`dir`, `files`, `takenAnyway`), **additively** (schema version 1 unchanged: a field added, [reference](../reference/output.md) §1.4), left out when nothing was kept. Tested: `TestKept`, `TestResultBackup`, the goldens of §7.
10. **The engine's refusal of any change while `backupPath` is refused** (the plan's backups refused by `backup.Check`) is reported as a refusal: its Why, the code, then "To go on: " and its Fix, the refused path's words (§6), in the document's `why` and `fix` too; the codes as before (`UNSAFE_DIRECTORY`, `BACKUP_SETTINGS_INVALID`), and `BACKUP_PATH_NOT_DIRECTORY`, new, where a file is on the path (it was `UNCLASSIFIED_ERROR`). A control's own refusal keeps its words. Tested: `TestBackupsRefusalWords`, `refused-dns-unsafe`, the gallery's `TestBackupRefusesEveryChange`.
11. **A backup that cannot be written** (a read-only file system) is `BACKUP_WRITE_FAILED` (`backup.ErrWrite`, new) and reads "Can't back up <file> in <backupPath>: <cause>", then, once the rollback finished, "Nothing was changed.", left out when the boot unit's install ran first (it is never rolled back), then how to go on (§6). It was `UNCLASSIFIED_ERROR` with `backup`'s raw error. Tested: `TestBackupWriteWords`, `failed-readonly`, the gallery's `play/backup-readonly`.

### `pkg/host/backup`'s helpers

12. **`DirError`**: Check's, Backup's, Load's and Restore's refusal of a directory names it (`Dir`) with its error, `ErrUnsafeDir` or `ENOTDIR`; its text is as before, "backup: <dir>: <error>". **A file on the way to the run directory is walked past and refused by name** (`/etc/os-release/backups`: "backup: /etc/os-release: not a directory"), where Check said "lstat <run dir>: not a directory" on macOS (run: `TestDirError` on real files); still `ENOTDIR`. Tested: `TestDirError`, `TestRuns`.
13. **`ErrWrite`, `WriteError`**: a backup that could not be written (making the run directory, the copy, the manifest) names the file (`Path`) and the backupPath, its text as before ("backup: mkdir /var/backups/hardn: read-only file system"). Tested: `TestWriteError` (each of the three writes, on real files with an injected `EROFS`).
14. **`Runs(ctx, sys, dir)`**: the runs under a backupPath, newest first, each with its manifest (`Load`), changing nothing; a directory with no manifest (no backup recorded) and an entry not named as a run id are not runs; a manifest `Load` refuses is left out and its error returned with the runs read; a path not there has none; a path another user can change is a `DirError`. Tested: `TestRuns` (real files and the fake).
15. Existing behaviour otherwise unchanged; every existing test of the package passes (run).

### Through `host.System`

16. The control reads and plans only through `host.System`, `pkg/host/backup`, `pkg/config` and `pkg/engine/steps`, and renders nothing: depguard's `controls-through-host` covers `pkg/engine/controls/**` (`os`, `syscall`, `pkg/ui`, `pkg/output` denied; run: lint 0 issues). It names `ENOTDIR` as "a `DirError` that is not `ErrUnsafeDir`", having no `syscall`.

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| `backup.Check` refuses a relative path (`ErrSettings`), a run directory that exists, and the deepest existing directory of the run's path unless it and every directory above it are root's (or hardn's user's) and writable by neither group nor others, sticky or not | atomic.md §2.11, §2.14; `pkg/host/backup` (read; `TestCheck`) | §2.3, §2.6, §2.8 |
| `config.Edit` edits a named setting only where its value differs from the file's | config-edit.md §2 (read: `edit.go`, `Edit`'s comment) | §2.5: the changed key alone |
| `revert.Rollback` restores from the backup run the journal names (`Backups.Dir`), loading its manifest there | read: `pkg/host/revert/rollback.go:176`, `:316` | §2.8: the rollback finds the backups where the change put them |
| `lstat(2)` of a path under a regular file fails with `ENOTDIR` | run on macOS (`TestDirError`, real files); Linux: lstat(2) **[unverified]**, not re-read for this unit | §2.12: the walk past a file |
| `EROFS`'s text is "read-only file system" | run on macOS (`TestWriteError`); Linux's errno text **[unverified]** | §2.11's cause |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root. The control's `Check` and `Plan` are called only by tests and the gallery yet (units (c), (d)); its `Refusal` and `NotWritten` are called by `pkg/output` (`errorsOf`, `errorOf`, `failureWords`) on `cmd/hardn`'s live paths now (`-g`, the DNS screen, `hardn confirm`'s clean-up), whose output changes with §2.9 to §2.11. On the VMs of [vmtest.md](../safety/vmtest.md) (Debian 13, Ubuntu 26.04, Alpine 3.24), through a dev-only driver or unit (c)'s screen:

- each note against the host: `/var/backups/hardn` made and not; `backupPath: /tmp/hardn-backups`; `backupPath: backups`; a path under `/etc/os-release`;
- turning backups off and on, `/etc/hardn/hardn.yml`'s one line changed, mode and owner kept; the run under `/var/backups/hardn/<id>/`, 0700, its manifest `takenAnyway` with backups off;
- on a host with `backupPath: /tmp/hardn-backups`: `-g` refused with the refused path's words; the path change to `/var/backups/hardn` applied, its backup there, nothing under `/tmp`; `-g` then applied;
- `/var/backups` on a read-only mount (`mount -o remount,ro`, or a bind mount): any change failing at its backup, `BACKUP_WRITE_FAILED`, "Can't back up … read-only file system", nothing changed;
- after `-g` on Debian 13, "Backup of /etc/resolv.conf kept in" and the run's directory; with `enableBackups: false`, "Backed up anyway: the rollback needs it.";
- a path change rolled back (a change after it made to fail): `hardn.yml` put back from the new path's run.

## 5. The notes

The path's notes, decided (decision 3), as `Status.Detail`:

| Host | State | Detail |
|---|---|---|
| Check passes, the path a directory, or a symlink to one | Configured | `Root only` |
| Check passes, the path not there | Configured | `Not created yet` |
| `backupPath` relative (an empty one is the default) | Not Configured | `Not absolute` |
| it, or a directory above it, another user can change (`/tmp` among them) | Not Configured | `Unsafe` |
| it, or a path above it, is a file | Not Configured | `Not a directory` |
| Check failed otherwise | Unknown | `Can't tell` |

## 6. The refusals

Each is a `steps.Refusal`; `pkg/output` shows the Why with the code, then "To go on: " and the Fix. All **decided**, with their codes (the maintainer, 2026-10-27: "Adopt the proposed words and BACKUP_WRITE_FAILED in control-backup.md §6–7"). They are gallery-backup.md §6's, cut into Why and Fix; "with Backup's item 2" names the item that changes the path, in both the legacy screen and the adopted one.

| Code | When | Why | Fix |
|---|---|---|---|
| `UNSAFE_DIRECTORY` | the path, or a directory above it, another user can change | Others can change `<dir>`, so backups can't go in `<path>` | choose a directory only root can change, with Backup's item 2 |
| `BACKUP_SETTINGS_INVALID` | a relative path, or a new path empty (the control's) | `"<path>"` is not an absolute path | set an absolute path with Backup's item 2 |
| `BACKUP_SETTINGS_INVALID` | a new path not written clean (the control's alone) | `"<path>"` is not written as `<clean>` | write it as `<clean>` |
| `BACKUP_PATH_NOT_DIRECTORY` (new) | a file on the path | `<file>` is not a directory | choose another path with Backup's item 2 |
| `BACKUP_WRITE_FAILED` (new) | a backup that could not be written (`NotWritten`) | Can't back up `<file>` in `<backupPath>`: `<cause>` | choose a backup path hardn can write, with Backup's item 2; after "Nothing was changed." once the rollback finished, unless the boot unit was installed first |
| `CONFIG_NOT_LOADED`, `CONFIG_CHANGED`, `CONFIG_NOT_EDITABLE`, `CONFIG_EDIT_UNSAFE`, `SYMLINK_REFUSED` | the save's | as control-dns.md §6 | as there |

The first four are the control's (`Refusal`, `checkPath`) and the engine's refusal of any change while `backupPath` is refused (§2.10); a screen says the Why with its own second sentence (gallery-backup.md §6's "Choose a directory only root can change.").

## 7. Words

| Where | Words | Source |
|---|---|---|
| After a change applied | "Backup of <files> kept in", the run's directory; "Backed up anyway: the rollback needs it." | **decided** (decision 8) |
| The path's notes | "Root only", "Not created yet", "Unsafe", "Not absolute", "Not a directory", "Can't tell" | **decided** (decision 3) |
| The path field (`Field`) | "Not an absolute path", "Write it as <path>", "Others can change <dir>", "Not a directory", "<path> is the backup path already" (a warning) | **decided** (decisions 5, 6) |
| The path field | "Backup path cannot be empty", "Can't check it: <error>" | **decided** (2026-10-27; gallery-backup.md §6's) |
| The refusals and the write's failure | §6's Why and Fix; "Nothing was changed." | **decided** (2026-10-27) |
| The control's title | "Backups" | **decided** (2026-10-27; the gallery's) |
| Evidence | `<hardn.yml>: enableBackups <bool>, backupPath <path>`; `<path>: a directory`, `<path>: absent`, or Check's error | **decided** (2026-10-27) |
| The save | "Save the configuration", "Save the configuration to <path>" | `steps.PlanSave`'s, as before |

The goldens (`pkg/engine/controls/backup/testdata/golden/human/`, the core cells, and `testdata/json/`), from the real engine on `hostfake`, show them as `pkg/output` renders them: `check` (six hosts' rows), `plan-off`, `applied-off` (kept, taken anyway), `applied-on` (kept alone), `applied-path-from-unsafe` (kept under the new path), `refused-dns-unsafe` (the DNS change refused in the refused path's words), `refused-path` (a new path refused by the control), `failed-readonly` (the write's failure). The plan's id, random in its last part, is fixed in them (`stable`), the run's directory with it.

**Existing goldens changed**, each named in its commit's body too (legacy and characterization goldens unchanged):

| Golden | Commit | Why |
|---|---|---|
| `pkg/output/testdata/codes.golden` | 5530da4, 61c3a9b | lists `BACKUP_WRITE_FAILED`, then `BACKUP_PATH_NOT_DIRECTORY` |
| `pkg/output/testdata/sentinels.golden` | 5530da4 | lists `backup.ErrWrite` |
| `pkg/output/testdata/golden/human/awaiting-confirmation.golden`, `after-flow.golden`, `after-flow-not-listed.golden`; `testdata/json/awaiting-confirmation.json` | f894961 | §2.9's lines (`result.backup` in the JSON): the fixture's apply now has the backup run the engine returns, the drop-in's |
| `pkg/engine/controls/dns/testdata/golden/human/applied-debian13-cloudflare.golden`; `testdata/json/applied-debian13-cloudflare.json` | f894961 | §2.9's lines: resolv.conf and hardn.yml kept (`applied-ubuntu2604` unchanged: its drop-in was not there before, so nothing was kept) |
| `internal/tools/uigallery/testdata/golden/play/dns.golden`, `dns-presets.golden`, `dns-remove.golden`, `dns-pve9.golden`, `dns-alpine324.golden` | f894961 | §2.9's lines after each change applied |
| `internal/tools/uigallery/testdata/golden/play/engine/dns-cloudflare-debian13.golden`, `dns-configure-pve9.golden`, `dns-udhcpc-alpine324.golden`, `confirm-cleanup-debian13.golden` | f894961 | the same |
| `internal/tools/uigallery/testdata/golden/play/backup-readonly.golden` | 5530da4, f894961 | `BACKUP_WRITE_FAILED` where it showed `UNCLASSIFIED_ERROR`, the report rewrapped; then the failure in §2.11's words |

## 8. Defects fixed (gallery-backup.md §7)

| # | Defect | Fix | Evidence now |
|---|---|---|---|
| 12 | The engine's refusal of an unsafe or relative `backupPath` was Check's error as it is, with no Why or Fix | the refused path's report, in `pkg/output`, for the engine's refusal of the plan's backups (§2.10) | `TestBackupsRefusalWords`; `refused-dns-unsafe`; the gallery's `TestBackupRefusesEveryChange` |
| 13 | A backup that cannot be written was `UNCLASSIFIED_ERROR` | `BACKUP_WRITE_FAILED`, with words (§2.11) | `TestWriteError`, `TestBackupWriteWords`, `failed-readonly`, `play/backup-readonly` |
| 14 | §3.16's "says so" reached the manifest, not the user | `Result.Backup`, and `pkg/output`'s lines (§2.9) | `TestKept`, `TestTakenAnyway`, the goldens |

And defect 6's second half: a path change on a host whose `backupPath` is refused is no longer refused (decision 7 (a), §2.8), so the screen can correct it; every other change is still refused there.

## 9. API

```go
package backup // pkg/engine/controls/backup (new)

const ID = "backup.settings"
const ChangeSave = ID + "/save"
const SettingEnabled, SettingPath = "enableBackups", "backupPath"
const DetailRootOnly, DetailNotCreated, DetailUnsafe, DetailNotAbsolute, DetailNotDirectory, DetailCantTell = ... // §5
func Controls(cfg *config.Config) []engine.Control
type Settings struct{ Config *config.Config } // an engine.Control and engine.BackupsSaver
func (Settings) SavedBackups(in engine.Input) hostbackup.Settings
type Reading struct { Settings hostbackup.Settings; Err error; Made bool }
func Read(ctx, sys host.System, cfg *config.Config) Reading
func (Reading) Note() string
func Refusal(path string, err error) *steps.Refusal // Check's refusal of path with its report; nil for another error
func NotWritten(err error) *steps.Refusal         // a WriteError with its report; nil for another error
type FieldError struct { Words string; Warn bool; Err error }
func Field(ctx, sys host.System, current, entry string) error // nil, or a *FieldError

package engine // added
type BackupsSaver interface { Control; SavedBackups(in Input) backup.Settings }
// Plan.Backups backup.Settings: the settings the plan's backups are taken under
// Result.Backup *backup.Manifest: the backup run, once every change applied and a backup was taken

package backup // pkg/host/backup, added
var ErrWrite error // BACKUP_WRITE_FAILED
type DirError struct { Dir string; Err error }                   // ErrUnsafeDir or ENOTDIR
type WriteError struct { Path, BackupPath string; Err error }     // ErrWrite and Err
func Runs(ctx context.Context, sys host.System, dir string) ([]Manifest, error)

package output // added
// Result.Backup *Kept (result.backup); type Kept struct { Dir string; Files []string; TakenAnyway bool }
// codes BACKUP_WRITE_FAILED, BACKUP_PATH_NOT_DIRECTORY
```

## 10. Decisions of this unit

1. **Decision 7 (a)'s route: the control says which settings its plan saves** (`engine.BackupsSaver`), and the engine takes the plan's backups under them. *Alternatives* (screen-kit.md §7): the backup settings from `Engine.Plan`'s configuration for every plan (every change of the DNS screen would then follow its copy's settings, where decision 7 (a) keeps them as loaded); `NewEngine` given the edited copy, called after `Controls` (a change to `screenkit` and to every caller's options, and the engine's backups chosen outside the plan, so the plan made again under the lock could not check them). The interface changes no caller's API: `Engine.Plan`, `output.FlowEngine`, `screenkit` and `cmd/hardn` are called as before. `FlowEngine`'s code did change (`flowengine.go`: its failure passes the document to `failureWords`, so a backup write's failure reads `NotWritten`'s words), as did `pkg/output`'s refusals (§2.10).
2. **A change of the backup settings is planned alone:** a plan has one backup run, so a plan with Backup's change and another control's is refused (`ErrConflict`), where the first build took every change's backups under the settings Backup's change saves, against decision 7 (a)'s "every other change still under `hardn.yml` as loaded" (on a host whose `backupPath` is refused, another control's change applied when planned with the saver's: run, `TestPlanBackupsAlone` without the refusal). Run All (slice 8) plans Backup's change in a plan of its own, unless the maintainer decides otherwise. A saver that plans nothing leaves `Options.Backups`; two that disagree conflict.
3. **"Backup of <files>" names the files whose content was kept**, once each: a path the run records absent (a file the change creates) is not named, as nothing of it was kept; a change that only creates files says nothing. The JSON's `files` are the same.
4. **The lines go after "Operation Successful" and any countdown, before the findings**, in `Human` and `AfterFlow`; the gallery's mock put them after everything, which is the same where a change has no countdown or finding left to say.
5. **`Result.Backup` only once every change applied**: after a failed apply the rollback restored the files, and the report says what was rolled back.
6. **The refusal words are applied in `pkg/output`**, for the plan's refusals no control owns (the engine's), from `Plan.Backups`' path: the engine cannot build a `steps.Refusal` (`steps` imports `engine`), and the words live in the control, which a screen uses too.
7. **`BACKUP_WRITE_FAILED` comes before the seam's and the standard library's codes**, which its errors also match, as the registry's rule puts the more specific first: a backup write that failed with a permission denied, or not synced, now reads `BACKUP_WRITE_FAILED` where it read `PERMISSION_DENIED` or `WRITE_NOT_DURABLE` (unreleased: v0.4 is not out). Adopted by the maintainer, 2026-10-27.
8. **`BACKUP_PATH_NOT_DIRECTORY`, new**, so that each of the control's refusals has a code; a `DirError` of `ENOTDIR` was `UNCLASSIFIED_ERROR`. Adopted with §6, 2026-10-27.
9. **Check walks past a file on the path to name it** (§2.12), so that the note and the refusal say which file.
10. **With no `hardn.yml` read, `Plan` is refused** (`CONFIG_NOT_LOADED`) even when the settings are the defaults: there is no file to compare with, and the screen says so before asking (`screenkit.NotLoaded`). The DNS control plans nothing for a host already as asked; Backup's has no host state to be as asked.
11. **`Check`'s state does not depend on `enableBackups`**: the engine's backups are taken either way; the screen's Backup State row says on or off, with the muted line (decision 8).
12. **No depguard rule of its own** (control-dns.md added `dns-control-through-host`): `controls-through-host` covers the directory.
13. **`Read` takes the path as the engine does**: an empty `backupPath` as the default, a symlink to a directory followed (§2.2), so that unit (c)'s refusal of item 1 from `Read`'s error refuses only what the engine refuses. The alternative, the engine refusing an empty path, would refuse every change on a host whose `hardn.yml` says `backupPath: ""`, which the engine plans today (run: `TestReadAsTheEngine`).

## 11. Not in this unit

- **Unit (c), the screen** (`pkg/screens/backup`), retiring the mocks: the section from `Read`, Verify's words, item 1 refused before it asks from `Read().Err` (decision 7 (a)), item 2's field `Field`, the change through `screenkit.Session.Change` with `backup.Controls` (its engine `cmd/hardn`'s, as loaded: the control's change takes the settings it saves, so `Session` needs no change), Backup Runs from `backup.Runs` (decision 9, read only); the decided item words.
- **Run All (slice 8)**: a plan of Backup's change with another control's is refused (§10, decision 2); slice 8 plans it alone, or the maintainer decides otherwise.
- **Unit (b2), parity**, and **unit (d)**: main menu 5 to the new screen, 2b's plan-only refusal lifted (decision 10), the legacy screen deleted after (b2).
- **The restore** (decision 9): a later unit, a change kind with confirm-or-revert for SSH and firewall files; the gallery keeps its mock.
- **The "Can't read the backup in <dir>" warning** (gallery-backup.md §6, proposed) is gone: the lines come from the manifest the engine wrote, with nothing read back.
- **Open, as before:** a path change leaves earlier runs under the old path, and Backup Runs lists the new path's; nothing prunes runs.

## 12. Size

`git diff --numstat 555ffef` (run, after the review's fixes): non-test Go 705 lines added and 52 removed outside the gallery (the control 358, of them 118 whole-line comments; `pkg/host/backup` 148; `pkg/engine` 86; `pkg/output` 113), the gallery 70 added and 196 removed; tests about 1,600 lines added. Testdata: 14 new golden files (8 human, 6 JSON) for the control; 18 existing ones changed, each named in §7. Over the guideline of about 500 changed lines: a control that checks and plans, two engine decisions, three helpers in `pkg/host/backup`, and their words in `pkg/output`, each with tests; the commits split it so.
