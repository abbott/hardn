# The Backup parity table: origin/main's Backup screen against Backup's control (`TestParity`)

> **Unit:** refactor plan slice 3 (Backup), unit (b2), `next/s3-parity`, from `next/s3-control` at f8c7e52: "Parity table: end state (file contents, …) of legacy vs new path, per fixture distro, with each difference tied to a V-number or an approved proposal. The baseline is the legacy (origin/main) path; the P0.7b v0.3.2 reference shows where origin/main regressed" (plan §2, "Phases 3-5 together", §3.22); [gallery-backup.md](gallery-backup.md) §9: "origin/main's end state against the new path's (hardn.yml's two keys; what is written under `backupPath`), each difference tied to a V-number or a decision". Sources the table ties to: V18 ([side-effects.md](../refactor/phase0/side-effects.md) §8), where the cell's golden names it; side-effects rows 29 and 30 (§3); decisions 3, 6, 7 (a), 8 and 10 of [gallery-backup.md](gallery-backup.md) §5 (adopted 2026-10-27); [control-backup.md](control-backup.md) §2.3, §2.8, §6, §7 and §11 (§6-7's words and codes adopted 2026-10-27); plan §3.16, and §3.19 as amended 2026-10-22.
> **Status:** built and tested on macOS, in process: origin/main from `pkg/characterization`'s Backup goldens, read in place; the new path on the recording fake (`hostfake`). Nothing ran on Linux or as root: every host claim is **written, not run (gate 5)** (§9). No existing golden or testdata file changed (one characterization golden added, `backup_path`); DNS's `TestParity`, `parity.golden` and [parity-dns.md](parity-dns.md)'s region are byte for byte as at f8c7e52, `-update` included (run). **No difference is unexplained** (§6).

## 1. Why

Unit (d) replaces origin/main's Backup screen: main menu 5 goes to the new screen, and `pkg/menu/backup_menu.go` and the legacy backup layers are deleted after this unit (gallery-backup.md §9). This page and its test are the record of what that screen did, kept after its code is gone: every difference between its end state and the new path's, from the same start, with the source that explains it.

## 2. Method

- **origin/main:** `TestLegacyMenuBackup`'s goldens (`pkg/characterization/testdata/golden/menu/backup_*.golden`, 21), read in place: the screen run in a child process on scripted stdin, in a temp dir that is also its working directory and `HOME`, its repository on the real file system there with each call recorded (gallery-backup.md §2). Twenty are unit (a)'s; `backup_path`, item 2 taking a new path live, is added here: before it, only `backup_path_dry_run`, under `-n`, showed a path taken (run: the two goldens differ only in their header). The harness reads their layout (`parseRepository` in `internal/parity`; [screen-kit.md](screen-kit.md) §7):
  - the `Start:` line; the V-numbers of the `KNOWN-DEFECT` and `FIXED` lines;
  - **the screen's own write test:** each time it is drawn with backups on, the screen writes `<path>/.write_test` and removes it, with `os`, outside the repository's calls (read: `backup_manager.go:86-99`). Of the screen as printed, the reader takes only each draw's Backup Path row and its line "Backup directory exists and is writable" (the write succeeded) or "doesn't exist or isn't writable" (nothing written). Its entries are marked `(screen)`; the remove is taken as done, and the temp dir afterwards must hold no `.write_test` (checked);
  - the repository's calls, `mkdir`, `write` (its content skipped) and `remove`, between the first draw and the second; a `mkdir` or `write` that failed is the run's refusal, `failed: <error>`;
  - what is at `backupPath` after; `./hardn.yml` as written, whole, or not.

  The rest of the screen is skipped (what was shown, not an end state); the temp dir afterwards is recorded, not compared. Any other line or call fails the test, and so do a second failed call, a failed `remove`, a trailer section twice, and calls with other than two draws.
- **`$TMP` is the root, `$TMP/home` is `$HOME`.** The goldens write the temp dir as `$TMP`. The reader maps paths in the `Start:` line, the calls, the screen's rows, the after line and the saved `hardn.yml` (`Slice.LegacyPaths`, pairs taken in order):
  - `$TMP/home`, the run's `HOME`, reads as `$HOME`. So `path-tilde`'s `~alice/backups` reads `$HOME/alice/backups`: the home of the user hardn runs as, then `alice`, root's under sudo, as its golden's `KNOWN-DEFECT` says (read: `backup_manager.go:47-53`). It does not read `/home/alice/backups`, alice's home, which origin/main never used;
  - `$TMP` is then the root: `$TMP/var/backups/hardn` (hardn's default, which the characterization moved under its temp dir) is `/var/backups/hardn`; `$TMP/shared` (0777) is `/shared`; `$TMP/afile` is `/afile`; `$TMP/other` is `/other`;
  - `backups`, relative, stays relative: the working directory.

  The new path's host has the same entries at those paths, as `prepareBackup` made them: `/var/backups/hardn` absent, an empty directory 0755, one holding the same 25-byte `.write_test`, or a regular file; `/shared` 0777, not sticky; `/afile` a file. Each cell holds its host, `hardn.yml` as loaded (`enableBackups`, `backupPath`) and plan only to the golden's `Start:` line, read as above (`Cell.LegacyStart`); a row's fixture and mode are taken from its start.
- **The same configuration.** origin/main's run made its configuration in memory (`runBackupChild`: `config.DefaultConfig()`, `alice`, `sshPort` 2230, `enableBackups` and `backupPath` as its start says, `dryRun` the session's), no file loaded. The new path loads `testdata/fixtures/backups-on.yml` or `backups-off.yml` from `/etc/hardn/hardn.yml` (0644): those four settings as `hardn.yml.example` writes them, under one comment line. Where both saved, the `hardn.yml settings` aspect agrees, "as loaded", in all 12 cells: origin/main's saved file holds the fixture's settings, by value.
- **The new path** (`TestParity`, `pkg/screens/backup/parity_test.go`, through `internal/parity`), each scenario on hostfake, as unit (c)'s screen is to run it (control-backup.md §11):
  - **The screen opened, and Verify (3):** the control's `Check` through the engine (`Engine.Check`), which reads the path as the screen's section and Verify do (`Read`; decision 3); where `Check` refuses the path, its refusal is `Read`'s error's code and the note. Under plan only it is the same `Check`, which writes nothing in either mode (decision 3: "under plan only too"); so `open-dry-run` and `verify-dry-run` run as `open` and `verify` do, and `Session.DryRun` reaches a change alone.
  - **Item 1:** refused before it asks where `Read` refuses the path (decision 7 (a)); else `screenkit.Session.Change`, the copy's `enableBackups` turned over, `backup.Controls`, the engine as `cmd/hardn` builds it (`engineOptions`: backups as `hardn.yml` was loaded; the control's change planned under the settings it saves, decision 7 (a)), answered y.
  - **Item 2:** the same, its form one text field checked by the control's `Field` (decisions 5, 6), answered as typed, then y where it is taken (`path`). An entry `Field` refuses is refused inline, and the form left with 0: nothing planned; the refusal is its words, with the code of `Check`'s error where it has one.
  - **Plan only:** `Session.DryRun`: each change stops at its plan (decision 10).
  - **`Session.Change`, not the engine directly:** it is what unit (c)'s screen calls for each change (control-backup.md §11), and it fits: the control plans from the edited copy, the engine takes hardn.yml as loaded, and `Session` needs no change. It also runs `output.AfterFlow`: the test checks that "Backed up anyway: the rollback needs it." is said exactly where a backup was taken anyway (§3.16's "and says so", decision 8). The flows' title bars and questions are the gallery mock's, **proposed** (gallery-backup.md §6), for unit (c) to decide; no golden here shows them.
  - **The end state** (`NewStateUnder`): what was written under the backup path the change saves, where decision 7 (a) puts its backup (else `hardn.yml`'s as loaded), each run's directory as `<run id>`; a backup the engine took elsewhere is then a host file and directory written, UNEXPLAINED; `hardn.yml`; the backups, from the run's manifest, with "taken anyway" where it says so; the journal; and what is at `/var/backups/hardn` after, in the characterization's words (`onDisk`'s).
- **Rows:** 18 of the 21 goldens. **Not rows:** `backup_return` (0) and `backup_invalid` (7, then the screen again): the screen opened with backups on over an empty directory, then a key that changes nothing: each draw's write test, as `open`'s, and nothing else; each ends as it started (after: "a directory, empty"). `backup_disabled_3`: backups off, so no write test; 3, hidden, then taken: a pause, no call (after: "not there", as it started). **Backup Runs** (decision 9, item 4, read only) has no origin/main counterpart: nothing lists runs there (`ListBackups` has no caller: gallery-backup.md §2.7). **A backup write that fails** (a read-only file system, `BACKUP_WRITE_FAILED`) has no origin/main golden, which ran on a writable temp dir; it is control-backup.md §2.11's (`failed-readonly`). The writes that fail here are origin/main's `MkdirAll` under a file: `toggle-on-file`, `path-under-file`, `verify-file`.
- **Distributions:** DNS's four cells, Debian 13, Ubuntu 24.04, Alpine 3.23 and Proxmox VE 9. Neither path reads the distribution: origin/main's screen and its layers read no host fact (read: `backup_menu.go`, `backup_manager.go`, `backup_service.go` and `file_backup_repository.go` read only the backup path, through `os` and the repository's file system), so one golden per scenario stands for every host; the new path ran on each distribution's facts over the same host, and every difference of §4 is on all four (run).
- **The aspects compared:**
  - the refusal (refused or not, and its code);
  - the host's files, directories and commands: none on either path, in every cell, as both write only under the backup path and to `hardn.yml`;
  - **under the backup path:** its directories made, each an `MkdirAll` call as recorded, with its mode (one of a directory there already changes nothing); its files written and removed, with their modes: the repository's as its calls record them, the screen's write test marked `(screen)`, its mode the code's (`os.WriteFile(…, 0644)`, read: `backup_manager.go:92`); and what is at `/var/backups/hardn` after. The content is not compared: the write test's `test`, the run's copy and manifest, which the control's and `pkg/host/backup`'s tests hold;
  - where both wrote: the write (in place or atomic), the backup and the journal; the findings;
  - **`hardn.yml`:** where it is saved; the scenario's key apart (`Scenario.Setting`): `enableBackups` for item 1, compared in 8 cells, `backupPath` for item 2, in `path`'s 4. A cell where both saved and no key is named fails. Then every other setting by value, as hardn loads the file, and the rest of its lines;
  - **`hardn.yml`'s mode is not compared:** origin/main's is not in the layout, so the reader marks it "mode not recorded". From the code it is `config.SaveConfig`'s `os.WriteFile(…, 0644)`: 0644 less the umask for a new file, an existing file's own mode kept (read: `pkg/config/config.go:494`).

  **Commands that only read:** none on either path (no `exec` in origin/main's calls; the new path ran none: the fake refuses any, and none was asked), so there are none to record. The dry run is its own rows (`*-dry-run`), as DNS's `add-dry-run`.
- **The table is a test:** `backupTable` (`parity_test.go`) holds every difference, keyed by scenario, distribution and aspect, with its source and one line. `TestParity` fails on a difference not in it (reported UNEXPLAINED), on an entry whose difference no longer occurs, and on an entry tied to a V-number that origin/main's golden for the cell does not name in a `KNOWN-DEFECT` or `FIXED` line: of the 18, only `backup_toggle_on` names one, V18 (`backup_toggle_off` and `backup_path` name it in a `NOTE`, which does not count).
- **Mutation-checked** (run at this unit's tip, each on a copy outside the repository: the goldens read from a changed copy, or a source file replaced with `go test -overlay`):
  1. the `path-empty` refusal entry removed: UNEXPLAINED;
  2. an entry for `open-missing`'s refusal added: stale;
  3. `toggle-off`'s write tied to V18: "does not name";
  4. origin/main's saved `sshPort: 2230` made 22 in `backup_toggle_off`: UNEXPLAINED, `hardn.yml settings`;
  5. a write added to `backup_verify_fails`' calls, where `backupPath files` agrees: UNEXPLAINED. Added to `backup_path_empty`'s, where that aspect is tied already, only `parity.golden` and the page fail (limit 1);
  6. `backup_verify_fails`' failed `mkdir` made a failed `rename`, and 7. an unknown trailer line in `backup_open`: the reader fails;
  8. the control's `Read` writing `.write_test`: UNEXPLAINED, `files` and `backupPath after`;
  9. the two dry-run changes applied live: UNEXPLAINED, write, backup, journal, rest;
  10. `open`'s host built from another start: the test fails on the `Start:` line;
  11. `path-empty`'s draws printing "doesn't exist or isn't writable": stale;
  12. item 2 saving `/elsewhere` in place of the path typed: UNEXPLAINED, `hardn.yml backupPath`;
  13. the engine's plan taking a path change's backups under the loaded path (`pkg/engine/plan.go`): UNEXPLAINED, `files`, `directories`, `backupPath after`;
  14. every row loading `backups-off.yml`, and every row live: the test fails on hardn.yml as loaded or plan only against `Start:`.
- **Its limits:** (1) a difference already tied whose values change fails only on `parity.golden` and the page, which `make golden-update` accepts: the review of that diff is the guard; (2) a `-run` filter checks only the cells it runs, and the table, the golden and the page only when every cell ran; (3) origin/main's whole saved `hardn.yml` is in its own golden, not in `parity.golden`; (4) origin/main's end states are its run's in a temp dir, as the test's user, not root on a host. Its `MkdirAll` also made parents that the trace does not show (the temp dir line): `$TMP/var` and `$TMP/var/backups` in `toggle-on`, which a host has; `$TMP/home` and `$TMP/home/alice` in `path-tilde`, which under sudo would be under root's home (written, not run); (5) the goldens record the trunk's `config.SaveConfig`, which writes `dryRun` as loaded (slice 2b decision 8), where origin/main's wrote the session's (their `NOTE`); (6) the content of what is written under the backup path is not compared; (7) until unit (c), the new path's items are this test's driver (`backupItem`), not a screen; (8) the screen's write test is read from its printed lines: its mode is the code's, and its remove is inferred from the temp dir afterwards, not traced.
- **Its renderings:** [`parity.golden`](../../pkg/screens/backup/testdata/golden/parity.golden), every cell with both end states whole, what agrees and what differs; and the region of §4 below. `make golden-update` rewrites both; without `-update` the test fails when either is not what it makes.

## 3. Host states

Every cell's host is the harness's, as origin/main's run was the characterization's: `/etc/hardn/hardn.yml` the fixture, `/var/backups` root's 0755, and at `backupPath` what the golden's `Start:` line says. None is read from a stock install, so §4 marks them "forced". Whether a stock Alpine 3.23 has `/var/backups` is not established (not read); where it has none, the first backup makes it 0700, as it makes `/var/backups/hardn` in `toggle-on` (atomic.md §2.10, which control-backup.md §2.3 cites).

| Scenario | origin/main's golden | Start | The new path's run |
|---|---|---|---|
| `open`, `open-missing`, `open-file` | `backup_open`, `_open_missing`, `_open_file` | backups on; the user's `.write_test`, nothing, a file | the screen opened: `Check` |
| `toggle-off`, `toggle-on` | `backup_toggle_off`, `_toggle_on` | on, a directory; off, nothing | item 1, y |
| `toggle-on-file` | `backup_toggle_on_fails` | off, a file | item 1, refused before it asks |
| `path` | `backup_path` | on, a directory | item 2: `/other`, y |
| `path-relative`, `path-tilde`, `path-unsafe`, `path-under-file`, `path-empty` | `backup_path_relative`, `_tilde`, `_unsafe`, `_fails`, `_empty` | on, a directory (`/shared` 0777; `/afile` a file) | item 2: `backups`, `~alice/backups`, `/shared/hardn`, `/afile/backups`, nothing; each refused inline, then 0 |
| `verify`, `verify-file` | `backup_verify`, `_verify_fails` | on, a directory; on, a file | item 3: `Check` |
| `open-dry-run`, `toggle-on-dry-run`, `path-dry-run`, `verify-dry-run` | the four `*_dry_run` | as `open`, `toggle-on`, `path` and `verify`, under `-n` | under plan only: `Check`, as live; item 1; item 2, `/other`; item 3, `Check`, as live |

## 4. The table

<!-- TestParity: begin. Generated by TestParity (pkg/screens/backup); make golden-update rewrites it. Do not edit. -->

**Host states** (§3): what the new path ran on, and whether the distribution's stock install has it (real), the harness forced it, or it cannot exist.

| Scenarios | Debian 13 | Ubuntu 24.04 | Alpine 3.23 | Proxmox VE 9 |
|---|---|---|---|---|
| open, open-dry-run | the user's .write_test (forced) | the user's .write_test (forced) | the user's .write_test (forced) | the user's .write_test (forced) |
| open-missing, toggle-on, toggle-on-dry-run | not there (forced) | not there (forced) | not there (forced) | not there (forced) |
| open-file, toggle-on-file, verify-file | a file (forced) | a file (forced) | a file (forced) | a file (forced) |
| toggle-off, path, path-relative, path-empty, path-tilde, verify, path-dry-run, verify-dry-run | a directory (forced) | a directory (forced) | a directory (forced) | a directory (forced) |
| path-unsafe | a directory, /shared 0777 (forced) | a directory, /shared 0777 (forced) | a directory, /shared 0777 (forced) | a directory, /shared 0777 (forced) |
| path-under-file | a directory, /afile a file (forced) | a directory, /afile a file (forced) | a directory, /afile a file (forced) | a directory, /afile a file (forced) |

**Agreeing and differing aspects** (§4): in how many compared cells each aspect agrees and differs.

| Aspect | Agrees in | Differs in |
|---|---|---|
| refusal | 40 | 32 |
| files | 72 | 0 |
| directories | 72 | 0 |
| commands | 72 | 0 |
| backupPath dirs | 32 | 40 |
| findings | 72 | 0 |
| hardn.yml | 40 | 32 |
| backupPath files | 16 | 56 |
| backupPath after | 52 | 20 |
| hardn.yml enableBackups | 8 | 0 |
| hardn.yml settings | 12 | 0 |
| write | 0 | 12 |
| backup | 0 | 12 |
| journal | 0 | 12 |
| hardn.yml rest | 0 | 12 |
| hardn.yml backupPath | 4 | 0 |

**Differences** (§4): each with its source; every cell, both end states whole, is in [parity.golden](../../pkg/screens/backup/testdata/golden/parity.golden).

| Scenarios | On | Aspect | origin/main | New path | Source |
|---|---|---|---|---|---|
| open | all | backupPath files | `write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen)` | `none` | decision 3; side-effects row 30: the screen's write test on opening; now Check, nothing written |
| open | all | backupPath after | `/var/backups/hardn: a directory, empty; the user's .write_test (25 bytes) is gone` | `/var/backups/hardn: a directory holding .write_test; .write_test as it was` | decision 3; side-effects row 30: the screen's write test overwrote, then deleted, the user's .write_test; now Check, nothing written |
| open-file | all | refusal | `none` | `BACKUP_PATH_NOT_DIRECTORY (Not a directory)` | decision 3; control-backup.md §6: a file at the path read as one to be made when needed; now Check's note, Not a directory |
| toggle-off | all | backupPath dirs | `none` | `/var/backups/hardn/<run id> 0700` | plan §3.16; control-backup.md §2.3: nothing made; now the change's run directory, 0700 |
| toggle-off | all | backupPath files | `write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen)` | `write /var/backups/hardn/<run id>/0001-hardn.yml 0600, write /var/backups/hardn/<run id>/manifest.json 0600` | decision 3; plan §3.16; control-backup.md §2.3; side-effects row 30: the screen's write test on its first draw; now the copy of hardn.yml and the run's manifest, 0600 |
| toggle-off, toggle-on | all | backupPath after | `/var/backups/hardn: a directory, empty` | `/var/backups/hardn: a directory holding <run id>` | plan §3.16: left empty; now holding the change's backup run |
| toggle-off, path | all | write | `in place` | `atomic` | plan §3.19: written in place; now replaced whole by a rename |
| toggle-off | all | backup | `none` | `/etc/hardn/hardn.yml (taken anyway: backups off)` | plan §3.16; decisions 7 (a), 8: nothing backed up; now hardn.yml first, taken anyway with backups off, and said |
| toggle-off, path | all | journal | `none` | `journalled first` | control-backup.md §2.8: no rollback; now journalled first, naming the backup run |
| toggle-off, toggle-on, path | all | hardn.yml | `./hardn.yml, in the working directory` | `/etc/hardn/hardn.yml, the file loaded` | side-effects row 29; plan §3.19: ./hardn.yml, last in hardn's search order; now the file loaded |
| toggle-off, toggle-on | all | hardn.yml rest | `48 settings, written whole; no file was loaded` | `the enableBackups lines alone changed; the file's other 4 lines kept (4 settings, 1 comment lines)` | side-effects row 29; plan §3.19: every setting written whole; now the item's line alone |
| toggle-on | all | backupPath dirs | `/var/backups/hardn 0755` | `/var/backups/hardn 0700, /var/backups/hardn/<run id> 0700` | plan §3.16; control-backup.md §2.3; side-effects row 30: made 0755 for the write test; now 0700 by the first backup, the change's run in it |
| toggle-on | all | backupPath files | `write /var/backups/hardn/.write_test 0644, remove /var/backups/hardn/.write_test, write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen)` | `write /var/backups/hardn/<run id>/0001-hardn.yml 0600, write /var/backups/hardn/<run id>/manifest.json 0600` | decision 3; plan §3.16; side-effects row 30: the repository's write test, then the screen's on its next draw; now the copy of hardn.yml and the run's manifest, 0600 |
| toggle-on | all | write | `in place` | `atomic` | V18; plan §3.19: written in place; now replaced whole by a rename |
| toggle-on | all | backup | `none` | `/etc/hardn/hardn.yml` | V18; plan §3.16; decision 7 (a): nothing backed up; now hardn.yml first, with backups on |
| toggle-on | all | journal | `none` | `journalled first` | V18; control-backup.md §2.8: no rollback; now journalled first, naming the backup run |
| toggle-on-file | all | refusal | `failed: mkdir /var/backups/hardn: not a directory` | `BACKUP_PATH_NOT_DIRECTORY, before asking` | decision 7 (a); control-backup.md §6, §11: the enable failed at its MkdirAll; now refused before asking, in Check's words |
| path | all | backupPath dirs | `/other 0755` | `/other 0700, /other/<run id> 0700` | decisions 6, 7 (a); plan §3.16; side-effects row 30: made 0755 for the write test; now 0700 by the change's backup, its run in it |
| path | all | backupPath files | `write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen), write /other/.write_test 0644, remove /other/.write_test, write /other/.write_test 0644 (screen), remove /other/.write_test (screen)` | `write /other/<run id>/0001-hardn.yml 0600, write /other/<run id>/manifest.json 0600` | decisions 3, 7 (a); plan §3.16; side-effects row 30: the write tests, the screen's on each draw and the repository's; now the copy of hardn.yml and the run's manifest under the new path, 0600 |
| path | all | backup | `none` | `/etc/hardn/hardn.yml` | plan §3.16; decision 7 (a); control-backup.md §2.8: nothing backed up; now hardn.yml first, under the new path |
| path | all | hardn.yml rest | `48 settings, written whole; no file was loaded` | `the backupPath lines alone changed; the file's other 4 lines kept (4 settings, 1 comment lines)` | side-effects row 29; plan §3.19: every setting written whole; now the item's line alone |
| path-relative | all | refusal | `none` | `BACKUP_SETTINGS_INVALID, inline: Not an absolute path` | decision 6: taken, made under the working directory; now refused inline, not absolute |
| path-relative | all | backupPath dirs | `backups 0755` | `none` | decision 6; side-effects row 30: made 0755; now nothing made |
| path-relative | all | backupPath files | `write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen), write backups/.write_test 0644, remove backups/.write_test, write backups/.write_test 0644 (screen), remove backups/.write_test (screen)` | `none` | decisions 3, 6; side-effects row 30: the write tests, the screen's on each draw and the repository's; now nothing written |
| path-relative, path-unsafe, path-tilde | all | hardn.yml | `./hardn.yml, in the working directory` | `not saved` | decision 6; side-effects row 29: saved to ./hardn.yml; now nothing saved |
| path-empty | all | refusal | `none` | `inline: Backup path cannot be empty` | control-backup.md §7: nothing typed left the path as it was; now the field refuses it, in words decided 2026-10-27, and asks again |
| path-empty | all | backupPath files | `write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen), write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen)` | `none` | decision 3; side-effects row 30: the screen's write test on each draw; now nothing written |
| path-unsafe | all | refusal | `none` | `UNSAFE_DIRECTORY, inline: Others can change /shared` | decision 6: taken under a directory anyone can write to; now refused inline, naming it |
| path-unsafe | all | backupPath dirs | `/shared/hardn 0755` | `none` | decision 6; side-effects row 30: made 0755; now nothing made |
| path-unsafe | all | backupPath files | `write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen), write /shared/hardn/.write_test 0644, remove /shared/hardn/.write_test, write /shared/hardn/.write_test 0644 (screen), remove /shared/hardn/.write_test (screen)` | `none` | decisions 3, 6; side-effects row 30: the write tests, the screen's on each draw and the repository's; now nothing written |
| path-tilde | all | refusal | `none` | `BACKUP_SETTINGS_INVALID, inline: Not an absolute path` | decision 6: ~ read as the home of the user hardn runs as, then taken; now refused inline, not absolute |
| path-tilde | all | backupPath dirs | `$HOME/alice/backups 0755` | `none` | decision 6; side-effects row 30: made 0755; now nothing made |
| path-tilde | all | backupPath files | `write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen), write $HOME/alice/backups/.write_test 0644, remove $HOME/alice/backups/.write_test, write $HOME/alice/backups/.write_test 0644 (screen), remove $HOME/alice/backups/.write_test (screen)` | `none` | decisions 3, 6; side-effects row 30: the write tests, the screen's on each draw and the repository's; now nothing written |
| path-under-file | all | refusal | `failed: mkdir /afile: not a directory` | `BACKUP_PATH_NOT_DIRECTORY, inline: Not a directory` | decision 6; control-backup.md §6: failed at its MkdirAll; now refused inline, Not a directory |
| path-under-file | all | backupPath files | `write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen)` | `none` | decision 3; side-effects row 30: the screen's write test on its first draw; now nothing written |
| verify, verify-dry-run | all | backupPath dirs | `/var/backups/hardn 0755` | `none` | decisions 3, 10; side-effects row 30: Verify's MkdirAll 0755, the directory there already; now Check, nothing made |
| verify, verify-dry-run | all | backupPath files | `write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen), write /var/backups/hardn/.write_test 0644, remove /var/backups/hardn/.write_test, write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen)` | `none` | decisions 3, 10; side-effects row 30: Verify's write test, and the screen's on each draw; now Check, nothing written |
| verify-file | all | refusal | `failed: mkdir /var/backups/hardn: not a directory` | `BACKUP_PATH_NOT_DIRECTORY (Not a directory)` | decision 3; control-backup.md §6: Verify failed at its MkdirAll; now Check's refusal, Not a directory, nothing tried |
| open-dry-run | all | backupPath files | `write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen)` | `none` | decisions 3, 10; side-effects row 30: the same under -n; now Check under plan only, nothing written |
| open-dry-run | all | backupPath after | `/var/backups/hardn: a directory, empty; the user's .write_test (25 bytes) is gone` | `/var/backups/hardn: a directory holding .write_test; .write_test as it was` | decisions 3, 10; side-effects row 30: the same under -n; now the screen opens under plan only, writing nothing |
| toggle-on-dry-run | all | backupPath dirs | `/var/backups/hardn 0755` | `none` | decision 10; side-effects row 30: made under -n for the write test; now the plan alone |
| toggle-on-dry-run | all | backupPath files | `write /var/backups/hardn/.write_test 0644, remove /var/backups/hardn/.write_test, write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen)` | `none` | decisions 3, 10; side-effects row 30: the write tests under -n, the repository's and the screen's; now the plan alone |
| toggle-on-dry-run | all | backupPath after | `/var/backups/hardn: a directory, empty` | `/var/backups/hardn: not there` | decision 10; side-effects row 30: made under -n; now not made |
| toggle-on-dry-run, path-dry-run | all | hardn.yml | `./hardn.yml, in the working directory` | `not saved` | decision 10; side-effects row 29: saved under -n; now a dry run saves nothing |
| path-dry-run | all | backupPath dirs | `/other 0755` | `none` | decision 10; side-effects row 30: made under -n for the write test; now the plan alone |
| path-dry-run | all | backupPath files | `write /var/backups/hardn/.write_test 0644 (screen), remove /var/backups/hardn/.write_test (screen), write /other/.write_test 0644, remove /other/.write_test, write /other/.write_test 0644 (screen), remove /other/.write_test (screen)` | `none` | decisions 3, 10; side-effects row 30: the write tests under -n, the repository's and the screen's; now the plan alone |

**v0.3.2** (§4): where it differs from origin/main, from [v032-reference.md](../refactor/phase0/v032-reference.md) §3, §4.13, §5.1.

| Scenarios | v0.3.2 |
|---|---|
| open, open-missing, open-file, open-dry-run | the settings printed, nothing written: no write test (read: v0.3.2 menu.go:710-725) |
| toggle-off, toggle-on, toggle-on-file | ./hardn.yml saved whole, dryRun included (§4.13, menu 10), nothing checked or made (read: menu.go:728-743); BackupFile then backed up before its 15 writes, unless enableBackups was false (§3; §5.1, V18: regressed) |
| path, path-relative, path-unsafe, path-tilde, path-under-file | the path saved as typed to ./hardn.yml (§4.13, menu 10), nothing checked or made, ~ not expanded (read: menu.go:744-761) |
| path-empty | "Backup path unchanged.", nothing saved (read: menu.go:756-757) |
| verify, verify-file, verify-dry-run | no item 3: 3 is an invalid option (read: menu.go:719-727, 764-768) |
| toggle-on-dry-run, path-dry-run | saved under -n too, nothing checked or made (§4.13: menus 6, 8, 10; read: menu.go:728-761) |

<!-- TestParity: end -->

## 5. What origin/main's path did that the new one does not

From the table, and, where marked, from the code (no golden shows it):

1. **Each draw of the screen with backups on wrote `<backupPath>/.write_test` and removed it**, with `os`, outside the repository (`backup_manager.go:91-99`; side-effects row 30): §4's `(screen)` entries, in 14 of the 18 rows, read from the line each draw printed. A user's file of that name was overwritten, then deleted (`open`, `open-dry-run`). Each item draws the screen again (`backup_menu.go:165-167`, `:206-208`, `:231-233`, `:243-245`), so the test ran again on the path then in use (`path`: `/other`); `backup_return` and `backup_invalid` print it too. The new screen reads `Check`, writing nothing (decision 3).
2. **Verify made the directory (`MkdirAll` 0755) and wrote and removed `.write_test`** (`verify`); on a file it failed at the `MkdirAll` (`verify-file`). Verify is now `Check`, and a file reads "Not a directory" (decision 3).
3. **Turning backups on made the directory 0755 and ran the write test; nothing took a backup** (V18: "Modified files will be backed up" was false). Now turning them on or off is a change on the engine: its own backup of `hardn.yml` into `<backupPath>/<run id>/` (0700, the copy and the manifest 0600), journalled, under the settings it saves; turning them off takes it anyway, and says so (§3.16; decisions 7 (a), 8). The first backup makes a missing `backupPath` 0700.
4. **Each change saved the whole configuration to `./hardn.yml`** in the working directory, in place: 48 settings, the file loaded left as it was (row 29). *From the code* (`backup_menu.go:157`, `:195`; `config.SaveConfig`): run in the loaded file's own directory, it overwrote that file whole. Now the file loaded, the key's line alone, atomically (§3.19 as amended).
5. **A path taken was made 0755 and write-tested, then saved whole to `./hardn.yml`, with no backup** (`path`). Now it is a change on the engine: `hardn.yml`'s `backupPath` line alone, atomically, its own backup under the new path, which it makes 0700, journalled (decision 7 (a)). `backupPath` saved agrees: `/other` (`hardn.yml backupPath`).
6. **It took any path:** relative, made under the working directory (`path-relative`); `~` read as the home of the user hardn runs as (`path-tilde`); under a directory anyone can write to (`path-unsafe`); each made, write-tested and saved, after which the engine refuses every change (gallery-backup.md §7, defect 6). The field now refuses each inline, with `Check`'s words, and nothing is saved (decision 6).
7. **A file at or above the path made the change fail at its `MkdirAll`** (`toggle-on-file`, `path-under-file`), the screen drawn again then showing Enabled or the new path, unsaved (gallery-backup.md §7, defects 5 and 17; the golden's screen, not compared). Item 1 is now refused before it asks (decision 7 (a)), and the field refuses the path (decision 6).
8. **Under `-n`, as origin/main reached the screen, every option did as live:** the directory made, the write test, `./hardn.yml` saved (`*-dry-run`). Since `next/s2b-replace` hardn refuses item 5 under plan only; unit (d) lifts that refusal (decision 10), and the screen then writes nothing: `Check`, and each change its plan alone.
9. **Nothing typed was "Backup path unchanged"**; the field now refuses it, "Backup path cannot be empty", and asks again (control-backup.md §7). The end state is the same.

The new path does what origin/main did not: Backup Runs, read only (decision 9); a refused `backupPath` corrected by the path change, planned under the path it saves (decision 7 (a); control-backup.md §2.8, `TestRefusedPathMoved`), which no origin/main golden starts from.

## 6. UNEXPLAINED differences

**None.** Every difference `TestParity` finds has an entry in `backupTable` with a source that says it; a difference with no entry fails the test and is marked UNEXPLAINED in the golden and in §4.

## 7. For unit (c)

- **Keep the test, and drive the screen.** `TestParity` lives in `pkg/screens/backup`, the package unit (c) fills, so that unit (c) replaces its driver, `backupItem`, with the screen's own items (its `Run`, typed as `Scenario.Lines` say: no key for the screen opened, then 1, 2 or 3 and the answers), as `pkg/screens/dns`'s `TestParity` drives the DNS screen. The table, `parity.golden` and §4 must not change: a screen that leaves a cell otherwise shows as UNEXPLAINED or a failed golden.
- **What the screen must leave as the control's apply leaves it:**
  - opening the screen and Verify: nothing written, under plan only too; `Check`'s note on a refused path;
  - item 1: refused before it asks where `Read` refuses the path; else `Session.Change` with `backup.Controls`, the engine as loaded: `hardn.yml`'s `enableBackups` line alone, atomic, its backup under the plan's backup path, journalled, and "Backed up anyway: the rollback needs it." after turning backups off;
  - item 2: the field `Field`, refusing inline what it refuses, an empty entry included; a path taken (`path`) saved as item 1 saves, its `backupPath` line alone, and its backup under the new path;
  - under plan only: the plan alone.
- **The words:** the flows' title bars and questions in `backupItem` are the gallery mock's, proposed (gallery-backup.md §6); unit (c) decides its own, which no aspect here compares.

## 8. For unit (d)

**What origin/main's screen did:** main menu 5, `NewBackupMenu` (`pkg/menu/main_menu.go:666`): §4's rows, and §5.

**The record to freeze first,** before `backup_menu.go` and the legacy backup layers go, as parity-dns.md §7 did for DNS:

- `TestParity` reads `pkg/characterization/testdata/golden/menu/backup_*.golden` in place (18 of them; all 21 are origin/main's record). `TestLegacyMenuBackup` runs `backup_menu.go` and its layers, so it goes with them. First move the 21 goldens unchanged (`git mv`) to `pkg/screens/backup/testdata/legacy/menu/`, in the characterization's layout, retire `TestLegacyMenuBackup` in the same commit, and point `backupParity.Legacy` there. `parity.golden` and §4 then stay byte for byte; the golden's header still says "read in place", kept as it is, as parity-dns.md §7 kept DNS's.
- `backups/legacy_backupfile.golden` (`TestLegacyBackupFile`) runs `utils.BackupFile` and the repository's `BackupFile`, neither called by hardn (V18): it goes, or is moved, with whichever of them unit (d) deletes. `backups/legacy_operations.golden` runs the legacy operations, which the other areas' legacy screens and Run All still call, and `backups/engine_run.golden` the engine: neither is this screen's. `TestParity` reads none of the three.

**What it may then delete** (read, `git grep` at this unit's tip): `pkg/menu/backup_menu.go`; `MenuManager`'s backup methods (`pkg/application/menu_manager.go:133-156`) and its `backupManager`; `application.BackupManager`, `service.BackupServiceImpl` and `secondary.FileBackupRepository`, built only by `ServiceFactory.CreateBackupManager` (`pkg/infrastructure/service_factory.go:198`, `:220-235`) and `MenuFactory.CreateMainMenu` (`menu_factory.go:59`); `utils.BackupFile`, with no caller. Each with its grep, as slice 2b's unit (d) did. Then main menu 5 opens the new screen, and 2b's plan-only refusal of 5 is lifted (decision 10).

## 9. Written, not run (gate 5)

Nothing here ran on Linux or as root. origin/main's end states are its run's on macOS, as the test's user, in a temp dir; the new path's are hostfake's. What a host would show is control-backup.md §4's list: turning backups off and on, the run under `/var/backups/hardn/<run id>/`, 0700, its manifest `takenAnyway` with backups off, `hardn.yml`'s one line changed, its mode and owner kept. The modes origin/main's run made are `os.MkdirAll`'s 0755 and `os.WriteFile`'s 0644 as it asked for them, less the umask, which the goldens do not record.

## 10. Files and size

- **Added:** `pkg/screens/backup/parity_test.go`; `pkg/screens/backup/testdata/fixtures/backups-on.yml`, `backups-off.yml`; `pkg/screens/backup/testdata/golden/parity.golden`; `pkg/characterization/testdata/golden/menu/backup_path.golden`; this page.
- **Changed:** `internal/parity` (its reader of the repository's layout, the screen's write test included, `Scenario.Setting`, `Slice.LegacyPaths` and `UnderBackupPath`, `NewStateUnder`, `FileWrite.NoMode`, and their tests); `pkg/characterization/menu_backup_test.go` (the case `backup_path`); [screen-kit.md](screen-kit.md) §7, [parity-dns.md](parity-dns.md) §8, control-backup.md §11, gallery-backup.md §2.4 and §9, the design-system README's "Running and regenerating" and the makefile's comment on `golden-update`. No non-test Go outside `internal/parity`, no legacy code, no existing golden or testdata file.
- **Size** (`git diff --numstat f8c7e52` and `wc -l`, run): `internal/parity` 431 lines added and 31 removed, its tests 219 and 9; Backup's part, `parity_test.go`, 632 lines of test Go, its table 39 entries; `pkg/characterization`'s test 5 lines added and 1 removed; testdata 4 new files, 2,322 lines (`parity.golden` 2,201, `backup_path.golden` 111, the fixtures 5 each); this page 225 lines; the other pages, the README and the makefile 18 lines added and 11 removed. Over the guideline of about 500 changed lines because the table is a test, as DNS's: a reader of a second golden layout, the backup path as an aspect, the driver of the new path, the table and its golden. No non-test Go outside `internal/parity`; `go.mod` and `go.sum` unchanged.
