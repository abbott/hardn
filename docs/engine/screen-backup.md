# The Backup screen on Backup's control (`pkg/screens/backup`, `screen.Backup`)

> **Unit:** refactor plan slice 3 (Backup), unit (c), `next/s3-screen`, from `next/s3-parity` at 3904fef: "Screen on the design system and prompt kit" (plan §2, "Phases 3–5 together", §3.22); [gallery-backup.md](gallery-backup.md) §9: "the screen on the design system and prompt kit, `pkg/screens/backup`, retiring these mocks". It builds the screen the maintainer adopted as the gallery's mock on 2026-10-27 (gallery-backup.md §5, decisions 1 (a), 2, 3, 4, 8, 9 and 10, with the words they name), drawn through Backup's control ([control-backup.md](control-backup.md)) and acting through the engine and the flow, and makes `TestParity` drive it ([parity-backup.md](parity-backup.md) §7). Decisions implemented: 1 (a) (no status row), 2 (the words of 05-BACKUP, P-31), 3 (Verify is Check, writing nothing; the path's notes), 4 (the item descriptions), 5 and 6 (the path field, through the control's `Field`), 7 (a) (item 1 refused before it asks where the path is refused; a new path taken there), 8 ("Rollback backups are still taken"; the lines after a change, `pkg/output`'s), 9 (Backup Runs, read only, P-19), 10's screen side (a dry run stops each change at its plan). V-numbers: V18; side-effects rows 29 and 30.
> **Status:** built and tested on macOS, in process: the screen through its entry point on the recording fake (`hostfake`), every host state, item, refusal, failure, redraw, dry run, decline and navigation; `TestParity` through the screen; the gallery's playable mode on its simulated hosts, its transcripts pinned on the core set. No real terminal, no hardn binary and no host were used; nothing ran on Linux or as root: every host claim is **written, not run (gate 5)** (§4). Nothing is wired into `cmd/hardn` or the live main menu (unit (d)); the legacy Backup screen (`pkg/menu/backup_menu.go`) and its characterization goldens are untouched. Words: the decided ones used exactly; every other word is **proposed** (§5).

## 1. Why

origin/main's Backup screen writes `<backupPath>/.write_test` on every draw and on Verify, takes a relative path or one under `/tmp`, saves the whole configuration to `./hardn.yml`, and drives nothing (gallery-backup.md §2, §7). Unit (a) drew the replacement on the real reading, which the maintainer adopted; unit (b) built the control, the engine's side of decisions 7 (a) and 8 and the run listing; unit (b2) recorded the parity table with a driver of its own. This unit draws the adopted screen from the host and runs each item through the control and the engine, so that one Backup screen exists, on the real code, for unit (d) to call from hardn's main menu.

## 2. Guarantees

### Where the screen lives

1. **The drawing in `pkg/ui`, the reading and the running outside it.** `pkg/ui/screen` holds the view-model, `screen.Backup` (the two settings, the path's note and Check's verdict, how many runs), which draws the screen on the sub-menu layout (`SubMenu`), its changes' title bars and reviews (`TurnStep`, `TurnReview`, `PathStep`, `PathReview`), and the runs (`BackupRunsMenu`, `BackupRun.SubMenu`). `pkg/screens/backup` reads the settings and the runs, fills the view, binds the items and runs each change with `screenkit.Session.Change`. **One entry point**, `Run` (§6).
2. **Through `host.System` only.** `pkg/screens/backup` reaches the host through the `host.System` it is given, Backup's control, `pkg/host/backup` and the engine, and draws through `pkg/ui` and `pkg/output`: depguard's `screens-through-host` covers it (`os`, `syscall`, `net` and the rest denied to its non-test files; run: lint 0 issues). `pkg/ui/screen/backup.go` imports nothing of the host (`ui-renders-only`; run). It never calls `engine.New`: the caller builds the engine (`Options.NewEngine`), so the gallery's `TestPlayTouchesNothing` walks it unchanged but for naming it (run).

### The screen (decisions 1 (a), 2, 3, 4, 8, 9)

3. **Each round reads the settings and draws the screen** (`nav.Prompter.Run`'s draw): `backupctl.Read` against the configuration the screen holds (Check of the path, an empty path the default, as the engine takes it, and whether the path is there), then, where Check takes the path and it is there, `backup.Runs` under it. **Drawing writes nothing, live and under plan only** (decision 3; run: `TestScreen`, every host, both modes: no write, no engine, nothing planned). hardn.yml is not read to draw: its settings are the ones the configuration holds, which takes the file's once a change saved it (§2.12).
4. **The title bar and section as adopted**: "Backup Settings"; no status row (decision 1 (a): the frame's two are other screens'); "Backup Configuration" with "Backup State" Enabled (`status.ok`), or Disabled in `status.warn` with the muted "Rollback backups are still taken" under it (decision 8), and "Backup Path" with the control's note (decision 3): "Root only", "Not created yet" (`status.ok`), "Unsafe", "Not absolute", "Not a directory" (`status.bad`), "Can't tell" (Unknown's marker). 17 lines to the prompt with backups on, 18 off, at 80×24 and 80×40 alike, the mock's counts (run: `TestBackupHeights`). A long path is cut at 64 columns with the ellipsis, as every row is (README §9.3; run: `/etc/os-release/backups Not a dire...`).
5. **Items 1 to 4 and Return**, the decided words: 1 "Backup State" "Turn backups off" or "Turn backups on" (decision 4), 2 "Backup Path" "Change backup path", 3 "Verify Directory" "Check the path, writing nothing", 4 "Backup Runs" with "None yet", "1 run" or "<n> runs" (proposed), **and no description where the runs were not read** (the path refused, or not listed: §8, decision 3); "Return"; "Select an option"; "Enter selection" (P-29 (a)).

### The items (decisions 3, 5, 6, 7 (a), 8, 9, 10)

6. **Item 1 turns backups the other way through the plan**, under "Turn Backups Off" or "Turn Backups On": the review, "Backup Configuration" with Backup State as the change leaves it and, turning them off, "Rollback backups are still taken" (decision 8); the plan, Backup's control's save of `enableBackups` from a copy of hardn.yml with it turned over (`backupctl.Controls` over the configuration as loaded, the engine planning the change under the settings it saves: decision 7 (a)), the `enableBackups` line alone edited in place; "Turn backups off?" or "Turn backups on?", y/N; the apply; then `pkg/output`'s "Backup of /etc/hardn/hardn.yml kept in" and the run's directory, and after turning them off "Backed up anyway: the rollback needs it." (decision 8; run: `TestTurn`: said once, after turning off alone).
7. **Item 1 is refused before it asks** where Check refuses the path (decision 7 (a); every change's plan would refuse it there but a new path's): a warning, the control's Why (control-backup.md §6, decided) and the screen's own second sentence (gallery-backup.md §6): "Others can change /tmp, so backups can't go in /tmp/hardn-backups. Choose a directory only root can change."; "\"backups\" is not an absolute path. Start it with /."; "/etc/os-release is not a directory. Choose another path." (the file on the path, as the control names it); Check failing otherwise, "Can't check /var/backups/hardn: backup: lstat /var/backups: permission denied." No engine is made, nothing planned or written (run: `TestRefusedBeforeAsking`).
8. **Item 2 asks "Enter new backup path"** and checks each entry with the control's `Field` (decisions 5, 6), each refusal an inline result under the field, which is asked again: "Backup path cannot be empty", "Not an absolute path", "Write it as /var/backups/hardn", "Others can change /tmp", "Not a directory", "Can't check it: <error>"; the path in use a warning, "/var/backups/hardn is the backup path already". A path not there yet is taken. Then the review, "Backup Path" with the entry and what Check makes of it ("Not created yet", "Root only"), the plan, the save of `backupPath` planned and applied under the path it saves, so that its own backup goes under the new path and nothing is written under the old (decision 7 (a)); "Use <path>?", y/N; the apply (run: `TestChangePath`). **On a host whose path Check refuses, item 2 is the way out**: its change is planned there, and item 1 then plans (run: `TestRefusedPathMoved`, the unsafe and the relative hosts).
9. **Item 3, Verify, is the control's reading, writing nothing, under plan only too** (decision 3): "Only root can change /var/backups/hardn. Nothing was written."; where the path is not there yet, "Only root can change /var/backups, where the first backup makes /var/backups/hardn. Nothing was written.", naming the deepest directory there, which Check checked; where Check refuses the path, the refusal of §2.7 as a warning, then "Nothing was written." (run: `TestVerify`, nine hosts, both modes).
10. **Item 4, Backup Runs, read only** (decision 9): the runs under the path in use, newest first (`backup.Runs`), each "2026-10-19 14:30" (its id's time, UTC, to the minute) and "1 file", "2 files", ", taken anyway" where a copy was taken with backups off; a run's screen, "Backup Run", the section "Run <id>", each path and "<n> bytes", "Not there before" or "Link to <target>", "taken anyway" in `status.warn`, Return alone. **No restore** (a later unit). With none, "No change has been backed up yet."; where the path is refused, §2.7's refusal; where none can be listed, "Can't read the runs in /var/backups/hardn: <error>."; a run that cannot be read beside others, "Some runs can't be read: <error>." above the list (run: `TestRuns`).
11. **With no hardn.yml read** (`Config.Path()` empty), items 1 and 2 are refused before asking, before the path's refusal, in the save's words, "No hardn.yml was read, so there is none to save to. Create /etc/hardn/hardn.yml, then try again." (`screenkit.NotLoaded`; control-backup.md §10, decision 10); Verify and Backup Runs read as ever (run: `TestNotLoaded`).
12. **A failure is the engine's.** A backup that cannot be written (a read-only file system) fails the change at its backup, `BACKUP_WRITE_FAILED`: "Can't back up /etc/hardn/hardn.yml in /var/backups/hardn: read-only file system", then "Nothing was changed. To go on: choose a backup path hardn can write, with Backup's item 2." (control-backup.md §2.11); hardn.yml as it was, nothing adopted (run: `TestFailedBackup`). The plan's own refusals are the flow's report, with the code and "To go on:", nothing asked or written: hardn.yml edited elsewhere since it was read, `CONFIG_CHANGED` (run: `TestRefusedByThePlan`), and the save's others.

### After a change

13. **The result stays on the screen, and the screen is drawn again under it from a fresh reading**: after turning backups off, Disabled and the muted line, item 1 "Turn backups on", item 4 "1 run"; after a path change, the new path "Root only"; after a failure, the host as it was.
14. **The configuration adopts hardn.yml as saved** (`screenkit.Session.Change`, `Adopt`): two changes in a row both save, no `CONFIG_CHANGED` (run: `TestTurn`). A decline, a dry run and a failure adopt nothing (run: `TestNothingWritten`, `TestFailedBackup`). Mutation-checked with `go test -overlay` (run, not committed): with the adoption removed from `Session.Change`, `TestTurn`, `TestChangePath` and `TestRefusedPathMoved` fail.
15. **Navigation**: 0 returns (`Run` returns nil); q quits from the screen, a change's question and the runs (`nav.ErrQuit`); the end of the input and Ctrl-C abort (`prompt.ErrAborted`), at the screen, the path field and the runs; a dry run stops each change at its plan, "No actual changes were made to your system."; a change's apply alone runs under `Options.ApplyContext` (run: `TestNavigation`, `TestNothingWritten`, `TestApplyContext`). `Options.Ran` is given what each planned change did. `Run`'s misuse (no Prompter's In or Out, System, Config or NewEngine) wraps `fs.ErrInvalid`, writing nothing (run: `TestRunNeeds`).

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| `backup.Check` refuses a relative path, and the deepest existing directory of the run's path unless it and every directory above it are root's (or hardn's user's) and writable by neither group nor others; it writes nothing | atomic.md §2.11, §2.14; `pkg/host/backup` `Check`, `checkTrusted` (read); control-backup.md §2.2 | §2.3, §2.7, §2.9 |
| `backupctl.Read` refuses the settings where the engine's plans refuse them | control-backup.md §2.2, `TestReadAsTheEngine` (run there) | §2.7: refused before asking only what the engine refuses |
| The engine plans and applies Backup's change under the settings it saves (`engine.BackupsSaver`) | control-backup.md §2.8 | §2.6, §2.8: `NewEngine`'s backups may be hardn.yml's as loaded |
| `backup.Runs` lists the runs newest first by their id, changing nothing; a run id begins with its UTC time to the second (`NewRunID`) | `pkg/host/backup` `runs.go`, `backup.go` `NewRunID` (read); control-backup.md §2.14 | §2.10 |
| `config.Edit` edits only the settings that changed | config-edit.md §2 | §2.6, §2.8 |
| The layouts' content stops at 64 columns, a row's value cut with the ellipsis | design-system README §9.3 | §2.4 |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root, and nothing calls `Run` but the tests and the gallery. On the VMs of [vmtest.md](../safety/vmtest.md) (Debian 13, Ubuntu 26.04, Alpine 3.24), once unit (d) wires it: the screen on each stock host, `/var/backups/hardn` made and not; Verify there, nothing written (`find /var/backups -newer`); turning backups off and on, `/etc/hardn/hardn.yml`'s one line changed, its mode and owner kept, the run under `/var/backups/hardn/<id>/` 0700, its manifest `takenAnyway` with backups off, the screen drawn again; a path change on a host with `backupPath: /tmp/hardn-backups`, its backup under the new path, nothing under `/tmp`, then item 1 planned; Backup Runs after DNS changes, each run's files; `/var/backups` on a read-only mount, `BACKUP_WRITE_FAILED`, nothing changed; under plan only once decision 10's refusal is lifted. control-backup.md §4 covers the control's own claims.

## 5. Words

The decided words used exactly; **every other word is proposed**.

| Where | Words | Source |
|---|---|---|
| Title bar | "Backup Settings" | origin/main; **decided** (decision 2) |
| Section, rows | "Backup Configuration"; "Backup State" "Enabled", "Disabled"; "Backup Path" | Figma; **decided** (decision 2) |
| Path notes | "Root only", "Not created yet", "Unsafe", "Not absolute", "Not a directory", "Can't tell" | **decided** (decision 3) |
| Backups off | "Rollback backups are still taken" (muted; also in item 1's review when turning off) | **decided** (decision 8) |
| Items | "Backup State" "Turn backups off" / "Turn backups on"; "Backup Path" "Change backup path"; "Verify Directory" "Check the path, writing nothing"; "Backup Runs"; "Return"; "Select an option"; "Enter selection" | **decided** (decisions 2, 4, 9) |
| Item 4's descriptions | "None yet", "1 run", "<n> runs" | gallery-backup.md §6, **proposed**; none where the runs were not read, this unit's (§8, decision 3) |
| Flows' title bars | "Turn Backups Off", "Turn Backups On", "Change Backup Path", "Backup Runs", "Backup Run" | gallery-backup.md §6, **proposed** |
| Path field | "Enter new backup path" | origin/main's question without its colon |
| Path field's results | "Backup path cannot be empty", "Not an absolute path", "Write it as <path>", "Others can change <dir>", "Not a directory", "Can't check it: <error>"; "<path> is the backup path already" (a warning) | **decided** (decisions 5, 6; control-backup.md §7) |
| Questions | "Turn backups off?", "Turn backups on?", "Use <path>?" | gallery-backup.md §6, **proposed** |
| A refused path (Verify's warning; items 1 and 4 before asking) | the control's Why, **decided** (control-backup.md §6): "Others can change <dir>, so backups can't go in <path>", "\"<path>\" is not an absolute path", "<file> is not a directory"; then "Choose a directory only root can change.", "Start it with /.", "Choose another path."; Check failing otherwise, "Can't check <path>: <error>." | the second sentences and "Can't check", gallery-backup.md §6, **proposed** |
| Verify | "Only root can change <path>. Nothing was written."; "Only root can change <dir>, where the first backup makes <path>. Nothing was written."; after a refusal, "Nothing was written." | gallery-backup.md §6, **proposed** |
| Runs | "Select a run"; "2026-10-19 14:30" "1 file" / "2 files" / ", taken anyway"; "No change has been backed up yet."; "Run <id>"; "<n> bytes", "Not there before", "Link to <target>", "taken anyway" | gallery-backup.md §6, **proposed** |
| Runs not read, new | "Can't read the runs in <path>: <error>."; "Some runs can't be read: <error>." | this unit, **proposed** |
| After a change applied | "Backup of <files> kept in" and the run's directory; "Backed up anyway: the rollback needs it." | **decided** (decision 8); `pkg/output`'s |
| A backup write that fails | "Can't back up <file> in <backupPath>: <cause>"; "Nothing was changed."; "To go on: choose a backup path hardn can write, with Backup's item 2." | **decided** (control-backup.md §6, §7) |
| No hardn.yml read | "No hardn.yml was read, so there is none to save to. Create /etc/hardn/hardn.yml, then try again." | `steps.PlanSave`'s `CONFIG_NOT_LOADED`, control-dns.md §6, **proposed** there |
| The gallery's, dev-only | "Backup hosts", "Select a host", each host's label, about and note; the restore's mock's note, "Restore" "Put these files back", "Restore Backup Run", "Restore this run?", the lockout note | the gallery's; the restore's mock's words gallery-backup.md §6's, **proposed** |

## 6. API

```go
package backup // pkg/screens/backup (new; parity_test.go was its only file)

type Options struct {
	Prompter     nav.Prompter                                   // the one line reader, the writer, the render context
	System       host.System                                    // the host
	Config       *config.Config                                 // hardn.yml as loaded; adopts each change as saved. Required
	NewEngine    func(progress func(engine.Event)) *engine.Engine // one per change. Required
	ApplyContext func(ctx context.Context) (context.Context, context.CancelFunc) // each apply's context; nil: Run's
	DryRun       bool
	LogPath      string           // a failure's report names it
	Now          func() time.Time // AfterFlow's clock; nil is time.Now
	Ran          func(output.Run) // what each planned change did, for the audit log
}

// Run shows the Backup screen until 0 (nil) or q (nav.ErrQuit);
// prompt.ErrAborted on the end of the input or Ctrl-C; fs.ErrInvalid,
// writing nothing, without a Prompter's In and Out, System, Config or NewEngine.
func Run(ctx context.Context, o Options) error

package screen // pkg/ui/screen, added
const BackupStateItem, BackupPathItem, BackupVerifyItem, BackupRunsItem = "1", "2", "3", "4"
type BackupPathState uint8 // BackupPathTaken, BackupPathRefused, BackupPathUnknown
type Backup struct { Enabled bool; Path, Note string; PathState BackupPathState; Runs int; RunsRead bool }
func (Backup) SubMenu() SubMenu
func (Backup) TurnStep() WorkflowStep; func (Backup) TurnReview() flow.Review
func (Backup) PathStep() WorkflowStep; func (Backup) PathReview(path, note string, s BackupPathState) flow.Review
type BackupRun struct { ID string; Taken time.Time; Files []BackupFile }
type BackupFile struct { Path string; Absent bool; Link string; Size int64; TakenAnyway bool }
func BackupRunsMenu(runs []BackupRun) SubMenu; func (BackupRun) SubMenu() SubMenu
```

`Options` are the DNS screen's but `Facts`: Backup's control reads no host fact (its `Check` ignores them, and it applies on every host), and the engine establishes its own when it plans. No new error, code or sentinel; no change to `pkg/engine`, `pkg/host`, `pkg/output`, `pkg/config`, `pkg/screens/screenkit`, `pkg/ui/flow`, `pkg/ui/nav` or the components.

## 7. What unit (d) must do

Unit (d) replaces the legacy Backup path (gallery-backup.md §9; parity-backup.md §8):

- **Main menu 5 calls `Run`** in place of `NewBackupMenu(...)` (`pkg/menu/main_menu.go:666`, `:716`), through a hook `cmd/hardn` sets, as `SetDNS` and `withDNSScreen` do (`main_menu.go:111-120`, `cmd/hardn/dns.go`), so that `pkg/menu` builds no engine. `cmd/hardn`'s hook is `runDNSScreen`'s shape: `Prompter` on `engineInput()` and `stdoutEnv()`; `System`, `engineHost()`; `Config`, the configuration the main menu holds (adopted in place, so the main menu, and the DNS screen's next visit, see the settings as saved); `NewEngine`, `newEngine(sys, engineOptions(ctx, sys, c))`, assembled once per visit (its backups `c.BackupSettings()` then; Backup's own changes are planned under the settings they save, so a path change within a visit needs nothing more); `ApplyContext`, `engineSignals`; `DryRun`, `c.DryRun`; `LogPath`, `output.LegacyLogPath`; `Now`, `revertNow`. No `Facts`. `Ran` nil until the audit log has a writer.
- **Map what `Run` returns** as for DNS: nil back to the main menu; `nav.ErrQuit` ends hardn as the main menu's q; `prompt.ErrAborted` and any other error as at every prompt (`endHardn`).
- **Lift 2b's plan-only refusal of item 5** (decision 10, adopted): `main_menu.go:716`'s `Refusal: planOnly`, "Not available in plan only." (`:62-67`, `:701-706`), goes for 5 and stays for User Management; the screen then opens under plan only, Verify and the runs read only, each change its plan alone.
- **Freeze the record first, then delete** (parity-backup.md §8): `git mv` the 21 `pkg/characterization/testdata/golden/menu/backup_*.golden` unchanged to `pkg/screens/backup/testdata/legacy/menu/`, retire `TestLegacyMenuBackup` in the same commit, point `backupParity.Legacy` there (`parity.golden` and parity-backup.md §4 byte for byte); then delete `pkg/menu/backup_menu.go`, `MenuManager`'s backup methods and the legacy backup layers parity-backup.md §8 lists, each with its grep.
- **No flag** routes to Backup (read: `grep -i backup cmd/hardn/*.go` finds only the engine's `Backups` option and rollback messages), so nothing like DNS's `-g` moves.

## 8. Decisions of this unit

None is the maintainer's; each is for review.

1. **`Options` without `Facts`** (§6): nothing reads them. *Alternative:* DNS's `Options` whole, for one shape across screens, `Facts` unused.
2. **With no hardn.yml read, items 1 and 2 are refused before the path's refusal** (§2.11): with no file nothing can be saved, the path change included, so the path's own Fix ("with Backup's item 2") would point at an item refused too. *Alternative:* the path's refusal first, as the DNS screen puts the host's first.
3. **Item 4 on a refused path is refused before it asks, in the refused path's words, and has no description** (§2.5, §2.10), where the mock drew "None yet" and said "No change has been backed up yet.": `backup.Runs` refuses a directory Check refuses, so nothing establishes that there are none. Where the runs cannot be listed, no description either, and the item says why; a run that cannot be read is said above the others. *Alternative:* the mock's "None yet".
4. **A run is a screen of its own, read only, with Return alone and no heading** (§2.10), as the mock's run screen less its Restore, so that the restore's unit adds an item. *Alternative:* the run's files written as the item's result above the list drawn again.
5. **A run's time is its id's** (`NewRunID`: UTC, to the second; shown to the minute), as the mock's, not its manifest's `Created`: the id names the directory, and orders the list.
6. **Verify names the deepest directory there** where the path is not there yet (§2.9), Check's subject, where the mock named the path's parent; on every gallery host they are the same, `/var/backups` (run: the goldens are the mock's there).
7. **Item 2's review reads the new path with the control** (`backupctl.Read` on the copy: "Not created yet", "Root only"), where the mock used `Lstat`.
8. **The flows' title bars carry the Backup screen as their parent** (`trail`, shown only with `TitleTrail`), as the DNS screen's do; the mock's had none. The same drawing while `TitleTrail` is off.
9. **The gallery's engines take hardn.yml's backups as the screen opened**, as `cmd/hardn` assembles `engineOptions` once per visit; the mock took them at each change. The same for Backup's changes, which take the settings they save.
10. **The main menu's 5 in play leads to the real screen; Figma's 05-BACKUP frame stays as the record** (`pkg/ui/screen`'s `sub-menu/05-backup`, its golden unchanged), as every Figma frame stays among the mocks: no item leads to it now, play starts at it by name, and `TestPlayReaches` takes it as a record (`figmaRecords`), its item checked to lead to hardn's screen. *Alternatives:* keep 5 on the frame, the real screen at `play backup` alone; or retire the frame (the record of 05-BACKUP's drift, decision 1, would then be git's alone).
11. **The restore's mock is kept as `play backup-restore`** (control-backup.md §11: the gallery keeps it for its later unit), on the runs host, on hardn's own runs list and run screen (`screen.BackupRunsMenu`, `BackupRun.SubMenu`) with the mock's item 1, Restore, and its `restoreControl`, under a note saying it is a mock. Its list's mapping from a manifest is a copy of the screen's (`runView`, 15 lines), rather than an export of `pkg/screens/backup` for a mock. *Alternative:* export it.
12. **The other listing mocks are retired, not kept as a design record** (as screen-dns.md decision 9): the screen per host, Figma's frame as listed (`-screens` draws it), decision 1 (b)'s status row (not adopted), Verify's reports, the runs, a run and its directory. git keeps them (`next/s3-parity`); gallery-backup.md records them.
13. **`TestParity`'s via lines name the screen and what was typed** ("the Backup screen, typed 2, /other, y"; "under plan only" for a dry run), as DNS's do; its golden's header, which says "as unit (c)'s screen is to run it", is kept, as parity-dns.md §7 kept DNS's.

## 9. Tests

- `pkg/screens/backup` (through `Run`, on `hostfake`): `TestScreen` (13 host states, both modes: rows, item 1 and item 4's descriptions, drawing writes nothing), `TestTurn` (off then on, the review's line, "Backed up anyway" once, hardn.yml's one line, the runs, the adoption), `TestChangePath` (each inline refusal, the review, the question, the save, the backup under the new path, nothing under the old), `TestRefusedPathMoved`, `TestVerify` (nine hosts and two directories not there yet, both modes, nothing written), `TestRefusedBeforeAsking` (items 1 and 4 on five refused hosts), `TestNotLoaded`, `TestRefusedByThePlan` (`CONFIG_CHANGED`; 0f7c02d), `TestRuns` (the list, two runs' screens, a run unreadable, none listable, none), `TestNothingWritten` (n, a dry run of each change, 0 at the field), `TestFailedBackup`, `TestApplyContext`, `TestNavigation`, `TestRunNeeds`. Mutation-checked with `go test -overlay` (run, not committed): the draw writing `.write_test` fails `TestScreen`, `TestVerify`, `TestRuns` and three more; Verify making the directory fails `TestVerify`; item 1 asking on a refused path fails `TestRefusedBeforeAsking` and `TestRefusedPathMoved`; the adoption removed fails three.
- `pkg/screens/backup` `TestParity`: drives `Run` (parity-backup.md §7); 72 cells, no end state changed. Mutation-checked: the draw writing `.write_test` is UNEXPLAINED; item 1 asking on a refused path changes `toggle-on-file`'s refusal, failing the golden and the page.
- `pkg/ui/screen`: `TestBackupScreen`, `TestBackupHeights`, `TestBackupWorkflows`, `TestBackupRuns`, `TestBackupFigmaDrift` (the latter two moved from the gallery).
- The gallery: `TestPlayBackupGolden` (11 transcripts), `TestBackupScreenHosts`, `TestPlayBackup` (each change: the transcript in order, the host's changes, the hold taken once by the failed backup), `TestBackupRefusesEveryChange` and `TestRunFilesAreBackups` (kept), `TestBackupRestore` (the mock), `TestPlayBackupUsage`, `TestPlayReaches` (`backupLinks`, `figmaRecords`), `TestPlayTouchesNothing` (walks `pkg/screens/backup`, `pkg/engine/controls/backup`, `pkg/host/backup`). Retired with the mocks: `TestBackupGolden`, `TestBackupReadings`, `TestPathCheckIsCheck` (the control's `TestField` holds `Field` to Check), `TestBackupTakenAnyway` (`TestTurn` and the control's `TestTakenAnyway`), `TestBackupDrawAndVerifyWriteNothing` (`TestBackupScreenHosts`), `TestBackupUsage` (`TestPlayBackupUsage`).

## 10. Goldens

- **Changed, the "via" line alone:** `pkg/screens/backup/testdata/golden/parity.golden`, the "new path:" line of all 72 cells (605d282's body lists them). Every end state, the table, and parity-backup.md §4's region are as they were (run: `git diff` shows "new path:" lines only; the page unchanged).
- **Removed:** `internal/tools/uigallery/testdata/golden/backup/*.golden` (13), the listing's mocks.
- **Changed:** `internal/tools/uigallery/testdata/golden/play/backup-toggle`, `-path`, `-off`, `-path-from-unsafe`, `-relative`, `-readonly`, `-dry-run`, `-hosts` (8): the real screen where the mock was; the opening note and the host's note shorter; otherwise byte for byte the mock's (run: a diff of the plain cells), but item 4 with no description on the unsafe and relative hosts (decision 3). `play/backup-runs`: Backup Runs read only, a run's files, no restore.
- **Added:** `play/backup` (the main menu's 5, Verify), `play/backup-restore` (the restore's mock, from `backup-runs`' old sequence).
- **Unchanged:** every other golden: the legacy and characterization goldens, DNS's (`pkg/screens/dns`, its `parity.golden`, parity-dns.md's region), the controls', `pkg/output`'s, `pkg/ui/screen`'s (`sub-menu/05-backup` included), and the gallery's others (run: `git diff --stat 3904fef` lists no other testdata).

## 11. Not in this unit; inputs to later units; open items

- **Unit (d)**: §7. **The restore**: its own later unit (decision 9), on the engine with confirm-or-revert for SSH and firewall files; the gallery's mock stays until then.
- **For the maintainer:**
  - **Item 4 on a refused path** has no description and is refused before it asks (§8, decision 3), where the adopted mock drew "None yet".
  - **The proposed words** (§5): the flows' title bars, the questions, Verify's, the runs', the refused path's second sentences, and this unit's two for runs not read.
  - **A long runs list** is one item per run, with no paging: nothing prunes runs (gallery-backup.md §5, still open), so the list grows with every change and is taller than 24 rows past 16 runs (counted from the layout, eight lines and one per run: `TestBackupHeights` has three in 11; not run).
  - **Earlier runs under an old path** are not listed after a path change (gallery-backup.md §5, still open).
  - **hardn.yml edited elsewhere while the screen is open** is not shown, as on the DNS screen (screen-dns.md §11); a change is then refused `CONFIG_CHANGED`.

## 12. Size

`git diff --numstat 3904fef f467b0b` (run; 0f7c02d adds one test, 21 lines): non-test Go 1,301 lines added and 1,090 removed: `pkg/screens/backup` 441 (136 of them whole-line comments), `pkg/ui/screen/backup.go` 257 (72), the gallery's `backupscreen.go` 558 (114; `restoreControl` moved whole from `backup.go`, 1,059 removed), `main.go`, `play.go`, `doc.go` and `mock.go` 45 added, 31 removed. Tests 1,519 added and 738 removed: `pkg/screens/backup`'s 778 new and `parity_test.go` 96 and 143, `pkg/ui/screen`'s 172, the gallery's 445 new, 590 removed, `play_test.go` 28 and 5. Testdata 1,271 lines added and 2,289 removed (2 goldens added, 10 changed, 13 removed). Over the guideline of about 500 changed lines, as screen-dns.md's: a screen, its view-model, its parity driver and its gallery play together, and retiring the mocks removes their code in the same unit; net of the removals, about 210 lines of non-test Go. `go.mod` and `go.sum` unchanged.
