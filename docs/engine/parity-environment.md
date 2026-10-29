# The Environment parity table: origin/main's SetupSudoPreservation against the Environment control (`TestParity`)

> **Unit:** refactor plan slice 9 (Environment), unit (b2), `next/s9-parity`, from `next/s9-control` at 9a8e3d8: "Parity table: end state (file contents, …) of legacy vs new path, per fixture distro, with each difference tied to a V-number or an approved proposal. The baseline is the legacy (origin/main) path; the P0.7b v0.3.2 reference shows where origin/main regressed" (plan §2, "Phases 3-5 together", §3.22); [gallery-environment.md](gallery-environment.md) §9: "origin/main's end state (`keep_*.golden`, `ops/setup_sudo_preservation/*.golden`) against the control's: `/etc/sudoers.d/<user>` against `hardn-env-<stem>`, each difference tied to V16, V18, V20 or a decision". Sources the table ties to: V1, V16, V18, V20 ([side-effects.md](../refactor/phase0/side-effects.md) §8), where the cell's golden names it; side-effects rows 17 and 18 (§3); decisions 1 (a), 2 (a), 3 (a) and 7 of [gallery-environment.md](gallery-environment.md) §5, and its Made here call for `-e` (adopted 2026-10-29); its §7 defects 4 to 7, 11 and 12; [control-environment.md](control-environment.md) §2.2, §2.3, §2.6, §2.11, §6 and §7; plan §3.17 and §3.21 (approved 2026-10-17).
> **Status:** built and tested on macOS, in process: origin/main from `pkg/characterization`'s goldens, read in place; the new path on the recording fake (`hostfake`). Nothing ran on Linux or as root: every host claim is **written, not run (gate 5)** (§9). No existing golden or testdata file changed; DNS's and Backup's `TestParity`, their `parity.golden` and the regions of [parity-dns.md](parity-dns.md) and [parity-backup.md](parity-backup.md) are byte for byte as at 9a8e3d8, `-update` included (run). **One difference is UNEXPLAINED, carried pending** (§6): hardn's header line in `hardn-env-<stem>`, 14 cells.

## 1. Why

Unit (d) replaces origin/main's Environment path: main menu 8 goes to the new screen, `-e` and `setup-sudo-env` to the control, and `environment_settings_menu.go` and the legacy environment layers are deleted after this unit (gallery-environment.md §9). This page and its test are the record of what that path did, kept after its code is gone: every difference between its end state and the new path's, from the same start, on each distribution, with the source that explains it.

## 2. Method

- **origin/main:** one call, `SetupSudoPreservation`, which item 1 of the Environment screen as root, `hardn -e` and `hardn setup-sudo-env` all make (read: `environment_settings_menu.go:90`, `cmd/hardn/main.go:423`, `:470`). Its goldens, read in place:
  - `environment/keep_*.golden` (10, `TestLegacyEnvironmentKeep`): the call in process, as item 1 makes it, on the real file system under a temp dir that stands for `/`, commands to the mock commander. The harness reads their layout (`parseRoot`, [screen-kit.md](screen-kit.md) §7): the `Environment:` header line and `== / before` (the start); the V-numbers of the `KNOWN-DEFECT` and `FIXED` lines; `== result`'s outcome (each caller's words after it are read, not run, and skipped); `== calls` (`stat` and `read` skipped; `write` with its content; `exec`, visudo's a check; `remove` of a file the run made, a temp file: one `/ before` does not hold, and through a planted symlink only with its target shown changed and the link gone); `== / afterwards`, the end state, which the calls must explain. A failed call is the run's refusal, `failed: <error>`, one at most. Any other line, section or operation fails the test.
  - `ops/setup_sudo_preservation/{d}.golden` (4, `TestOperationGoldens`): `-e`'s and `setup-sudo-env`'s operation per distribution, read as DNS's ops goldens, its `remove` of the temp file taken as a temp file (of a seeded file, the reader fails) and visudo as a check (`Slice.Temps`, `Slice.Check`), its `seed` lines the start, its `cfg.DryRun=true` section and `hardn -n` refusals the dry run.
- **One run for every distribution.** origin/main's path reads no host fact (read: `file_environment_repository.go` reads `/etc/sudoers.d`, the user's file and `os.TempDir()`, and runs visudo; `environment_service.go` and `environment_manager.go` call it; `CreateEnvironmentManager`, `service_factory.go:219-228`, gives it the provider's file system and commander, no OS facts). The four ops goldens' traces are identical; they differ in the fixture's host line and its failing commands, which this path never runs (run: `diff`). So each `keep_*` golden, run once, stands for every cell, as parity-backup.md §2's host-independent rows.
- **Paths: the temp dir is the root, and the goldens already say so.** `rootedFS` records each call with hardn's own path, the temp dir cut, and the test fails if the temp dir is left in a golden (read: `environment_calls_test.go:93-120`, `:475-478`); `TMPDIR` unset, `os.TempDir()` is `/tmp`, the root's. No `keep_*` or `setup_sudo_preservation` golden holds `$TMP` (run: `grep`), so origin/main's paths are the new path's as they are; `Slice.LegacyPaths` reads a `$TMP` as the root, as Backup's does. The planted symlink's target, `../etc/hosts`, resolves inside the root, to `/etc/hosts` (its golden's `NOTE`).
- **The same start.** Each cell's host is held to its golden's start (`Cell.LegacyStart`): the environment and every entry of `/ before`, mode and content, or the ops golden's seed (its `/etc/sudoers.d` mode unrecorded, taken as `keep_*`'s 0750; alice's file 0440, as `ConfigureSudo` writes it, `ops/configure_sudo`). The host then gets what only the new path reads, which origin/main's root lacks and its path never reads: on sudo `/etc/sudoers` (Debian 13's in shape, with its includedir); on doas Alpine 3.23's cloud image's `doas.conf` and `wheel.conf`; the engine's directories. hostfake keeps no sticky bit on `/tmp`; the new path makes no call under `/tmp` (checked).
- **The environment.** The golden's `SUDO_USER` is the user who ran hardn through the host's tool; under doas the new path reads it as `DOAS_USER`, the variable doas sets [I, doas(1); sudoers.md §2.14]. origin/main reads `SUDO_USER`, else `USER` (read: `file_environment_repository.go:135-138`), so under doas its run would be `keep_root`'s, `USER` root, and so at a root login on a host with neither tool, where nothing sets `SUDO_USER` (defect 5; written, not run); the table compares its golden as recorded, and each such cell's second note and the Alpine and Proxmox VE sources say so.
- **visudo refusing** (`visudo-fails`): origin/main's fixture fails `visudo -c -f` on its temp file, which holds alice's unparsable grant; the new path's fails `visudo -c`, the configuration that includes the same file, and passes a copy of hardn's own file, as visudo answers a file it cannot parse [I].
- **The new path** (`TestParity`, `pkg/screens/environment/parity_test.go`, through `internal/parity`), on hostfake, the distribution's facts from the control's tests:
  - **Item 1, as unit (c)'s screen is to run it** (control-environment.md §11): `environment.Read`; where its `Refusal()` is set, refused before it asks (its words `screenkit.RefusalWords`, then `Stays()` on doas); else the control's change through `screenkit.Session.Change`, `environment.Keep{Getenv, Stop: Kept}`, under the decided title and question (gallery-environment.md §6), answered y. **`Session.Change`, not the engine directly:** it is what the gallery runs (`internal/tools/uigallery/environment.go`, `change`) and what §11 names for the screen; it fits with no change to `Session`.
  - **`-e`, as unit (d) is to run it** (§11): `environment.Controls` planned and applied through the engine as `cmd/hardn` builds it, asking nothing. In every cell its end state, checks, temp files, backups, journal, findings and refusal's code equal item 1's (checked). So each row stands for item 1, `-e` and `setup-sudo-env` alike, as on origin/main one call does.
  - **Plan only:** item 1 under `Session.DryRun`, and `-e`'s plan alone: neither writes, journals, backs up or runs anything, in every cell (checked). The `grant` row, `-e`'s golden, has no lines, so its dry run is an aspect, as DNS's (a) rows.
  - **hardn.yml:** none loaded; the engine takes `config.DefaultConfig`'s backups (on, `/var/backups/hardn`), as `cmd/hardn` takes the loaded one's. Neither path saves it (`hardn.yml` agrees: not saved).
  - **The end state** (`NewStateChecked`): the host's files, the commands, the backups (the run's manifest) and the journal; the checks (`visudo`, `doas -C`), each run id as `<run id>`; the files written under the engine's run directory, the temp files.
  - **Untouched:** every file and symlink of the start, the user's own grant included, is as it was after either run (checked).
- **Rows:** the 10 `keep_*` goldens and `ops/setup_sudo_preservation` (`grant`), 11 rows on 4 distributions, 44 cells. **Not rows** (`TestParityGoldens` holds each golden a row or a not-row, and each named there):
  - `menu/environment_open`, `_open_unset`, `_open_user`, `_open_root`, `_open_no_user`, `_open_commented`, `_open_elsewhere`: the screen drawn, its reading alone; each `/ afterwards: unchanged` and `./hardn.yml: not written` (run). The reading's defects are §7's 4, 5 and 17; unit (c)'s screen reads with the control.
  - `menu/environment_guide`, `_invalid`, `_return`: words alone, nothing written (run).
  - `menu/environment_keep_not_root`, `_keep_dry_run`: item 1's branch for a user other than root, which hardn never reaches (defect 8); as root under dry run it prints and calls nothing (read, not run).
  - `environment/notice_*` (9): the `su -` probe at `LoadConfig` and its notice (side-effects row 5), not the Environment's item; unit (d) removes it (decision 4).
  - `ops/configure_sudo/*`: `ConfigureSudo`'s grant (slice 5), V16's evidence that the next grant overwrites the line; not Environment's operation.
- **Item 1's stop way** (decision 5 (a)) has no origin/main counterpart: origin/main only keeps, and no start holds a file of hardn's that keeps the variable (`keep_already`'s is origin/main's line; item 1 still keeps there). It is compared against nothing here; the control's `TestApply` and `applied-stop` hold it.
- **Distributions:** the ops goldens' four, each on the control's tests' facts: Debian 13 (sudo), Ubuntu 26.04 (sudo-rs; the ops golden's host line says 24.04, which nothing on origin/main's path reads), Alpine 3.23's cloud image (doas), Proxmox VE 9 (neither tool [I]). **Unlike Backup, the new path reads the host:** `sudoers.Choose` takes the tool from the facts. So on Alpine item 1 is refused, hardn having no doas rule for alice (decision 1 (a)); on Proxmox VE "sudo isn't installed here" (decision 7); where origin/main writes `/etc/sudoers.d/<user>` on every host.
- **The aspects compared:** the refusal (none, its code and "before asking", or `failed:`); the files written, path and mode; of the files under `/etc/sudoers.d` and `/etc/doas.d` either wrote, the keep lines (`env_keep`, doas's `setenv`), the other rules and the comments, line by line, against none where one alone wrote (`Slice.Either`); the directories and the commands that change something; the checks; the temp files; where both wrote, the write (in place or atomic), a write's against a write; wherever either wrote, backed up or journalled, the backup and the journal; the findings; `hardn.yml`. **Not compared:** a file's owner (origin/main's goldens record none: `os.WriteFile` as root, read; the new path's is root's, checked); a temp file's content; what each caller prints (read, not run, in origin/main's goldens). In `visudo-fails` the new path backs up and journals before `visudo -c` refuses, then rolls back from the journal: compared, though neither wrote.
- **Commands that only read:** origin/main's only command is visudo, a check; the new path's are visudo and `doas -C` (reached by no row). Any other command is unscripted and fails the run.
- **The table is a test:** `envTable` (`parity_test.go`) holds every difference, keyed by scenario, distribution and aspect, with its source and one line. `TestParity` fails on a difference not in it (UNEXPLAINED), on an entry whose difference no longer occurs, and on an entry tied to a V-number the cell's golden does not name in a `KNOWN-DEFECT` or `FIXED` line: `keep_new` names V16, V18 and V20; `keep_dotted` V16 and V20; `keep_root`, `keep_tmp_dir` and `keep_tmp_symlink` V20; `setup_sudo_preservation` V1, V18 and V20; the others none. So V16 is not cited for `grant`, and `dotted`'s and `tmp-symlink`'s write cites defect 11, not V18. One tie rests on unit (b)'s engineering, not on a decision the maintainer adopted, and says so: `no-sudoers-d`'s refusal before asking on sudo (control-environment.md §2.3, from its §10 decisions 3 and 17; its words, "the error as it is", adopted with unit (b)'s on 2026-10-29). An entry with no source this table may cite is **pending** (`Tie.Pending`): carried, its tie proposed, and marked UNEXPLAINED in the golden and §4 (§6).
- **Mutation-checked** (run at this unit's tip, each on a copy outside the repository through `go test -overlay`; the goldens from a changed copy):
  1. the `tmp-dir` refusal entry removed: UNEXPLAINED;
  2. an entry for `no-user`'s files added: stale;
  3. `dotted`'s write tied to V18: "does not name";
  4. the pending `comments` entry removed: UNEXPLAINED;
  5. `keep_new`'s line written with `EDITOR` too: UNEXPLAINED, `keep lines`;
  6. a `chmod` call in `keep_new`: the reader fails;
  7. `keep_tmp_symlink`'s `/etc/hosts` change dropped: the reader fails (a write through the link not shown);
  8. a `remove` of alice's file after its write in `setup_sudo_preservation/debian`: the reader fails (a file the fixture seeded); so in `keep_already`, its `/ before` holding the file;
  9. `keep_new`'s `/etc/sudoers.d` made 0755: the test fails on the start;
  10. `environment.Controls` stopping: "item 1 left … -e left";
  11. sudoers' copy written to `/tmp/hardn_sudoers_temp`: "a call under /tmp";
  12. the control keeping `EDITOR` too: UNEXPLAINED, `keep lines`;
  13. a header line in `keep_new` the reader does not know: the reader fails;
  14. the grant line dropped from `setup_sudo_preservation/debian`'s write: stale, `rules`;
  15. the pending entry made explained: only `parity.golden` and the page fail (limit 1);
  16. a not-row's golden misnamed: `TestParityGoldens` fails;
  17. `visudo-fails`' backup entry removed: UNEXPLAINED, `backup`;
  18. the pending entry back to its first 8 cells: UNEXPLAINED, `comments`, in `already`, `commented` and `tmp-dir`.
- **Its limits:** (1) a difference already tied whose values change, or a pending entry turned explained, fails only on `parity.golden` and the page, which `make golden-update` accepts: the review of that diff is the guard; (2) a `-run` filter checks only the cells it runs, and the table, the golden and the page only when every cell ran; (3) origin/main's end states are its run's in a temp dir, as the test's user, not root on a host: the symlink was followed by its owner, where on Debian and Ubuntu `fs.protected_symlinks` refuses root's open of another user's link [I, `keep_tmp_symlink`'s `KNOWN-DEFECT`], and its runs are sudo's, `SUDO_USER` alice, which doas and a host with neither tool do not set [I] (each such cell's note); (4) the new path's doas, `doas -C` and Proxmox VE facts are the control's tests' model [I]; (5) the item 1 driver is this test's, not a screen, until unit (c) (§7).
- **Its renderings:** [`parity.golden`](../../pkg/screens/environment/testdata/golden/parity.golden), every cell with both end states whole, what agrees and what differs; and the region of §4 below. `make golden-update` rewrites both; without `-update` the test fails when either is not what it makes.

## 3. Host states

Every cell's host is the characterization's root, as origin/main's run was, with the distribution's tool files added (§2); none is read from a stock install, so §4 marks them "forced". Whether a stock Alpine 3.23 or Proxmox VE 9 has `/etc/sudoers.d` is not established (not read); the new path does not read it there.

| Scenario | origin/main's golden | Start | The new path |
|---|---|---|---|
| `new`, `dotted` | `keep_new`, `keep_dotted` | alice, or alice.smith, with no file | sudo: item 1, y; doas, neither: refused before asking |
| `grant` | `ops/setup_sudo_preservation/{d}` | alice's grant in `/etc/sudoers.d/alice` | `-e`: plan and apply; item 1 the same; its dry run |
| `already`, `commented` | `keep_already`, `keep_commented` | the grant and origin/main's line, or the line commented | sudo: item 1, y, Kept by `/etc/sudoers.d/alice` or Not kept |
| `no-sudoers-d` | `keep_no_sudoers_d` | no `/etc/sudoers.d` | refused before asking, on every host |
| `root`, `no-user` | `keep_root`, `keep_no_user` | `SUDO_USER` unset, `USER` root or unset | refused before asking, on every host |
| `visudo-fails` | `keep_visudo_fails` | an unparsable grant; visudo refusing | sudo: item 1, y, the apply failing at `visudo -c` |
| `tmp-symlink`, `tmp-dir` | `keep_tmp_symlink`, `keep_tmp_dir` | another user's symlink to `/etc/hosts`, or directory, at `/tmp/hardn_sudoers_temp` | sudo: item 1, y; doas, neither: refused before asking |

## 4. The table

<!-- TestParity: begin. Generated by TestParity (pkg/screens/environment); make golden-update rewrites it. Do not edit. -->

**Host states** (§3): what the new path ran on, and whether the distribution's stock install has it (real), the harness forced it, or it cannot exist.

| Scenarios | Debian 13 (sudo) | Ubuntu 26.04 (sudo-rs) | Alpine 3.23 (doas) | Proxmox VE 9 (neither tool) |
|---|---|---|---|---|
| new | no file (forced) | no file (forced) | no file (forced) | no file (forced) |
| grant | alice's grant (forced) | alice's grant (forced) | alice's grant (forced) | alice's grant (forced) |
| already | the grant and the line (forced) | the grant and the line (forced) | the grant and the line (forced) | the grant and the line (forced) |
| commented | the line commented (forced) | the line commented (forced) | the line commented (forced) | the line commented (forced) |
| dotted | alice.smith, no file (forced) | alice.smith, no file (forced) | alice.smith, no file (forced) | alice.smith, no file (forced) |
| no-sudoers-d | no /etc/sudoers.d (forced) | no /etc/sudoers.d (forced) | no /etc/sudoers.d (forced) | no /etc/sudoers.d (forced) |
| root | a root login (forced) | a root login (forced) | a root login (forced) | a root login (forced) |
| no-user | no user (forced) | no user (forced) | no user (forced) | no user (forced) |
| visudo-fails | visudo refuses (forced) | visudo refuses (forced) | visudo refuses (forced) | visudo refuses (forced) |
| tmp-symlink | a /tmp symlink (forced) | a /tmp symlink (forced) | a /tmp symlink (forced) | a /tmp symlink (forced) |
| tmp-dir | a /tmp directory (forced) | a /tmp directory (forced) | a /tmp directory (forced) | a /tmp directory (forced) |

**Agreeing and differing aspects** (§4): in how many compared cells each aspect agrees and differs.

| Aspect | Agrees in | Differs in |
|---|---|---|
| refusal | 12 | 32 |
| keep lines | 8 | 18 |
| rules | 22 | 4 |
| directories | 44 | 0 |
| commands | 44 | 0 |
| findings | 36 | 8 |
| hardn.yml | 44 | 0 |
| files | 18 | 26 |
| comments | 12 | 14 |
| checks | 14 | 30 |
| temp files | 14 | 30 |
| write | 0 | 8 |
| backup | 12 | 16 |
| journal | 12 | 16 |
| dry run | 0 | 4 |

**Differences** (§4): each with its source; every cell, both end states whole, is in [parity.golden](../../pkg/screens/environment/testdata/golden/parity.golden).

| Scenarios | On | Aspect | origin/main | New path | Source |
|---|---|---|---|---|---|
| new | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | files | `/etc/sudoers.d/alice 0440` | `/etc/sudoers.d/hardn-env-alice 0440` | V16; plan §3.17: /etc/sudoers.d/<user>, the file a grant overwrites; now hardn-env-<stem>, hardn's own |
| new, grant, already, commented, dotted, tmp-symlink, tmp-dir | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | comments | `none` | `# Managed by hardn` | UNEXPLAINED (pending; proposed, not approved: sudoers.md §2 item 6 (next/p2-sudoers)): no comment; now hardn's header line, # Managed by hardn |
| new, grant, tmp-symlink | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | checks | `visudo -c -f /tmp/hardn_sudoers_temp` | `visudo -c, visudo -cf /run/hardn/<run id>/sudoers-validate/hardn-env-alice, visudo -c` | control-environment.md §2.11; side-effects row 18: the temp file alone checked; now the configuration before and after, and a copy of the file |
| new, grant | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | temp files | `/tmp/hardn_sudoers_temp 0440, removed` | `/run/hardn/<run id>/sudoers-validate/hardn-env-alice 0440, removed` | V20; control-environment.md §2.11: a fixed name in /tmp, anyone's to take; now a copy in hardn's run directory, removed with it |
| new, grant | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | write | `in place` | `atomic` | V18: written in place; now replaced whole by a rename |
| new, grant | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | backup | `none` | `/etc/sudoers.d/hardn-env-alice (absent before: the rollback removes it)` | V18; control-environment.md §2.11: nothing backed up; now recorded absent first, so a rollback removes it |
| new, grant | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | journal | `none` | `journalled first` | V18; control-environment.md §2.6: no rollback; now journalled first |
| new, already, commented, dotted, tmp-symlink | Alpine 3.23 (doas) | refusal | `none` | `SUDOERS_ENV_NEEDS_GRANT, before asking` | plan §3.21; decision 1 (a); control-environment.md §6; defect 5: success on a doas host (under doas, root's file written [I]); now refused before asking: hardn has no doas rule for alice |
| new, grant | Alpine 3.23 (doas) | files | `/etc/sudoers.d/alice 0440` | `none` | plan §3.21; decision 1 (a); defects 5, 7: /etc/sudoers.d/<user> written on a doas host (root's under doas [I]); now nothing written |
| new, grant, tmp-symlink | Alpine 3.23 (doas) | keep lines | `Defaults:alice env_keep += "HARDN_CONFIG"` | `none` | plan §3.21; decision 1 (a); defects 5, 7: the line written to a sudoers file on a doas host (Defaults:root under doas [I]); now nothing written |
| new, grant, dotted, tmp-symlink | Alpine 3.23 (doas) | checks | `visudo -c -f /tmp/hardn_sudoers_temp` | `none` | plan §3.21; decision 1 (a): visudo run on a doas host; now refused, nothing checked |
| new, grant, dotted | Alpine 3.23 (doas) | temp files | `/tmp/hardn_sudoers_temp 0440, removed` | `none` | V20; decision 1 (a): the temp file in /tmp; now refused, nothing written |
| new, already, commented, dotted, root, no-user, tmp-symlink | Proxmox VE 9 (neither tool) | refusal | `none` | `SUDOERS_NO_TOOL, before asking` | decision 7; control-environment.md §6: whatever the host, a sudoers file written, a failure, or nothing; now refused before asking: sudo isn't installed here |
| new, grant | Proxmox VE 9 (neither tool) | files | `/etc/sudoers.d/alice 0440` | `none` | decision 7; control-environment.md §6; defect 5: /etc/sudoers.d/<user> written on a host without sudo (root's at a root login [I]); now nothing written |
| new, grant, tmp-symlink | Proxmox VE 9 (neither tool) | keep lines | `Defaults:alice env_keep += "HARDN_CONFIG"` | `none` | decision 7; control-environment.md §6; defect 5: the line written to a sudoers file on a host without sudo (Defaults:root at a root login [I]); now nothing written |
| new, grant, dotted, root, tmp-symlink | Proxmox VE 9 (neither tool) | checks | `visudo -c -f /tmp/hardn_sudoers_temp` | `none` | decision 7; control-environment.md §6: visudo run on a host without sudo; now refused, nothing checked |
| new, grant, dotted, root | Proxmox VE 9 (neither tool) | temp files | `/tmp/hardn_sudoers_temp 0440, removed` | `none` | V20; decision 7: the temp file in /tmp; now refused, nothing written |
| grant | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | files | `/etc/sudoers.d/alice 0440` | `/etc/sudoers.d/hardn-env-alice 0440` | plan §3.17; decision 3 (a); side-effects rows 17, 18: the line appended to the grant's file, which the next grant overwrites; now hardn-env-alice, the grant's file left as it was |
| grant | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | rules | `alice ALL=(ALL) NOPASSWD: ALL` | `none` | plan §3.17; decision 3 (a); side-effects row 17: the grant carried in the file written; now the line alone, the grant left in alice's file |
| grant, already, commented, visudo-fails | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | findings | `none` | `SUDOERS_LEGACY_FILE` | decision 3 (a); control-environment.md §2.2, §7: alice's file not reported; now in the review: it may also let alice use sudo |
| grant | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | dry run | `the service writes and runs as live (cfg.DryRun unread below the menus); no path reaches it: hardn -n refused (exit status 2)` | `the plan alone: nothing written` | V1; Made here (-e): no path reaches the service's dry run: -n -e refused; now the plan alone |
| grant | Alpine 3.23 (doas) | refusal | `none` | `SUDOERS_ENV_NEEDS_GRANT` | plan §3.21; decision 1 (a); control-environment.md §6; defect 5: a sudoers file written on a doas host (root's under doas [I]); now -e's plan refused: hardn has no doas rule for alice |
| grant | Alpine 3.23 (doas) | rules | `alice ALL=(ALL) NOPASSWD: ALL` | `none` | plan §3.21; decision 1 (a); defects 5, 7: the grant carried into a sudoers file on a doas host (under doas, root's, the line alone [I]); now nothing written |
| grant | Alpine 3.23 (doas) | dry run | `the service writes and runs as live (cfg.DryRun unread below the menus); no path reaches it: hardn -n refused (exit status 2)` | `refused (SUDOERS_ENV_NEEDS_GRANT): nothing written` | V1; Made here (-e); decision 1 (a): no path reaches the service's dry run: -n -e refused; now the plan's refusal |
| grant | Proxmox VE 9 (neither tool) | refusal | `none` | `SUDOERS_NO_TOOL` | decision 7; control-environment.md §6: a sudoers file written on a host without sudo; now -e's plan refused: sudo isn't installed here |
| grant | Proxmox VE 9 (neither tool) | rules | `alice ALL=(ALL) NOPASSWD: ALL` | `none` | decision 7; control-environment.md §6; defect 5: the grant carried into a sudoers file on a host without sudo (at a root login, root's, the line alone [I]); now nothing written |
| grant | Proxmox VE 9 (neither tool) | dry run | `the service writes and runs as live (cfg.DryRun unread below the menus); no path reaches it: hardn -n refused (exit status 2)` | `refused (SUDOERS_NO_TOOL): nothing written` | V1; Made here (-e); decision 7: no path reaches the service's dry run: -n -e refused; now the plan's refusal |
| already | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | files | `none` | `/etc/sudoers.d/hardn-env-alice 0440` | decision 3 (a); control-environment.md §2.2: origin/main's line found, nothing written; now Kept by /etc/sudoers.d/alice, and item 1 still writes hardn-env-alice |
| already | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | keep lines | `none` | `Defaults:alice env_keep += "HARDN_CONFIG"` | decision 3 (a); control-environment.md §2.2: origin/main's line found in alice's file, nothing written; now the line in hardn-env-alice too |
| already | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | checks | `none` | `visudo -c, visudo -cf /run/hardn/<run id>/sudoers-validate/hardn-env-alice, visudo -c` | decision 3 (a); control-environment.md §2.11: nothing written or checked; now hardn-env-alice's checks |
| already | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | temp files | `none` | `/run/hardn/<run id>/sudoers-validate/hardn-env-alice 0440, removed` | decision 3 (a); control-environment.md §2.11: nothing written; now hardn-env-alice's copy in hardn's run directory |
| already | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | backup | `none` | `/etc/sudoers.d/hardn-env-alice (absent before: the rollback removes it)` | decision 3 (a); control-environment.md §2.11: nothing written or backed up; now hardn-env-alice recorded absent first, so a rollback removes it |
| already | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | journal | `none` | `journalled first` | decision 3 (a); control-environment.md §2.6: nothing written; now journalled first |
| commented | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | files | `none` | `/etc/sudoers.d/hardn-env-alice 0440` | defect 4; decision 3 (a): a commented line read as kept, nothing written; now Not kept, and item 1 writes hardn-env-alice |
| commented | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | keep lines | `none` | `Defaults:alice env_keep += "HARDN_CONFIG"` | defect 4; decision 3 (a): a commented line read as kept, nothing written; now the line in hardn-env-alice |
| commented | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | checks | `none` | `visudo -c, visudo -cf /run/hardn/<run id>/sudoers-validate/hardn-env-alice, visudo -c` | defect 4; control-environment.md §2.11: nothing written or checked; now hardn-env-alice's checks |
| commented | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | temp files | `none` | `/run/hardn/<run id>/sudoers-validate/hardn-env-alice 0440, removed` | defect 4; control-environment.md §2.11: nothing written; now hardn-env-alice's copy in hardn's run directory |
| commented | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | backup | `none` | `/etc/sudoers.d/hardn-env-alice (absent before: the rollback removes it)` | defect 4; control-environment.md §2.11: nothing written or backed up; now hardn-env-alice recorded absent first, so a rollback removes it |
| commented | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | journal | `none` | `journalled first` | defect 4; control-environment.md §2.6: nothing written; now journalled first |
| dotted | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | files | `/etc/sudoers.d/alice.smith 0440` | `/etc/sudoers.d/hardn-env-alice_2esmith 0440` | V16; plan §3.17: /etc/sudoers.d/alice.smith, a name sudo skips; now hardn-env-alice_2esmith, which it reads |
| dotted | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | checks | `visudo -c -f /tmp/hardn_sudoers_temp` | `visudo -c, visudo -cf /run/hardn/<run id>/sudoers-validate/hardn-env-alice_2esmith, visudo -c` | control-environment.md §2.11; side-effects row 18: the temp file alone checked; now the configuration before and after, and a copy of the file |
| dotted | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | temp files | `/tmp/hardn_sudoers_temp 0440, removed` | `/run/hardn/<run id>/sudoers-validate/hardn-env-alice_2esmith 0440, removed` | V20; control-environment.md §2.11: a fixed name in /tmp, anyone's to take; now a copy in hardn's run directory, removed with it |
| dotted, tmp-symlink | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | write | `in place` | `atomic` | defect 11: written in place; now replaced whole by a rename |
| dotted | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | backup | `none` | `/etc/sudoers.d/hardn-env-alice_2esmith (absent before: the rollback removes it)` | defect 11; control-environment.md §2.11: nothing backed up; now recorded absent first, so a rollback removes it |
| dotted, tmp-symlink | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | journal | `none` | `journalled first` | defect 11; control-environment.md §2.6: no rollback; now journalled first |
| dotted | Alpine 3.23 (doas) | files | `/etc/sudoers.d/alice.smith 0440` | `none` | plan §3.21; decision 1 (a); defects 5, 7: /etc/sudoers.d/<user> written on a doas host (root's under doas [I]); now nothing written |
| dotted | Alpine 3.23 (doas) | keep lines | `Defaults:alice.smith env_keep += "HARDN_CONFIG"` | `none` | plan §3.21; decision 1 (a); defects 5, 7: the line written to a sudoers file on a doas host (Defaults:root under doas [I]); now nothing written |
| dotted | Proxmox VE 9 (neither tool) | files | `/etc/sudoers.d/alice.smith 0440` | `none` | decision 7; control-environment.md §6; defect 5: /etc/sudoers.d/<user> written on a host without sudo (root's at a root login [I]); now nothing written |
| dotted | Proxmox VE 9 (neither tool) | keep lines | `Defaults:alice.smith env_keep += "HARDN_CONFIG"` | `none` | decision 7; control-environment.md §6; defect 5: the line written to a sudoers file on a host without sudo (Defaults:root at a root login [I]); now nothing written |
| no-sudoers-d | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | refusal | `failed: sudoers.d directory does not exist; your system may not support sudo drop-in configurations` | `SUDOERS_NOT_INCLUDED, before asking` | control-environment.md §2.3, §6; its §10 decisions 3, 17 (unit (b)'s, not adopted): failed in its own words, before any write; now SUDOERS_NOT_INCLUDED, refused before asking |
| no-sudoers-d | Alpine 3.23 (doas) | refusal | `failed: sudoers.d directory does not exist; your system may not support sudo drop-in configurations` | `SUDOERS_ENV_NEEDS_GRANT, before asking` | plan §3.21; decision 1 (a); defect 7: failed, doas never looked for; now refused before asking: hardn has no doas rule for alice |
| no-sudoers-d | Proxmox VE 9 (neither tool) | refusal | `failed: sudoers.d directory does not exist; your system may not support sudo drop-in configurations` | `SUDOERS_NO_TOOL, before asking` | decision 7; control-environment.md §6: whatever the host, a sudoers file written, a failure, or nothing; now refused before asking: sudo isn't installed here |
| root | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs), Alpine 3.23 (doas) | refusal | `none` | `ENVIRONMENT_NO_USER, before asking` | decision 2 (a); defect 5: the line written for root, USER standing in for SUDO_USER; now refused before asking: no user to keep it for |
| root | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs), Alpine 3.23 (doas) | files | `/etc/sudoers.d/root 0440` | `none` | decision 2 (a); defect 5: /etc/sudoers.d/root, Defaults:root; now nothing written |
| root | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs), Alpine 3.23 (doas) | keep lines | `Defaults:root env_keep += "HARDN_CONFIG"` | `none` | decision 2 (a); defect 5: Defaults:root, USER standing in for SUDO_USER; now nothing written |
| root | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs), Alpine 3.23 (doas) | checks | `visudo -c -f /tmp/hardn_sudoers_temp` | `none` | decision 2 (a): the temp file checked; now refused, nothing checked |
| root | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs), Alpine 3.23 (doas) | temp files | `/tmp/hardn_sudoers_temp 0440, removed` | `none` | V20; decision 2 (a): the temp file in /tmp; now refused, nothing written |
| root | Proxmox VE 9 (neither tool) | files | `/etc/sudoers.d/root 0440` | `none` | decision 7; control-environment.md §6; defect 5: /etc/sudoers.d/<user> written on a host without sudo (root's at a root login [I]); now nothing written |
| root | Proxmox VE 9 (neither tool) | keep lines | `Defaults:root env_keep += "HARDN_CONFIG"` | `none` | decision 7; control-environment.md §6; defect 5: the line written to a sudoers file on a host without sudo (Defaults:root at a root login [I]); now nothing written |
| no-user | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs), Alpine 3.23 (doas) | refusal | `none` | `ENVIRONMENT_NO_USER, before asking` | decision 2 (a); defect 6: success, with nothing read or written; now refused before asking: no user to keep it for |
| visudo-fails | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | refusal | `failed: invalid sudoers configuration: exit status 1` | `failed: SUDOERS_INVALID_BEFORE` | control-environment.md §2.11; defect 12: visudo refused the temp file, its output dropped; now the configuration, checked first, refused before hardn-env-alice is written |
| visudo-fails | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | checks | `visudo -c -f /tmp/hardn_sudoers_temp -> exit status 1` | `visudo -c -> exit status 1` | control-environment.md §2.11: the temp file checked, and refused; now the configuration, checked first, refused |
| visudo-fails | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | temp files | `/tmp/hardn_sudoers_temp 0440, removed` | `none` | control-environment.md §2.11: the temp file written, then refused; now refused before any copy is written |
| visudo-fails | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | backup | `none` | `/etc/sudoers.d/hardn-env-alice (absent before: the rollback removes it)` | control-environment.md §2.11: nothing backed up; now hardn-env-alice recorded absent before visudo -c refused, the apply rolled back |
| visudo-fails | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | journal | `none` | `journalled first` | control-environment.md §2.6, §2.11: no rollback; now journalled before visudo -c refused, and rolled back from the journal |
| visudo-fails | Alpine 3.23 (doas) | refusal | `failed: invalid sudoers configuration: exit status 1` | `SUDOERS_ENV_NEEDS_GRANT, before asking` | plan §3.21; decision 1 (a); defect 7: failed, doas never looked for; now refused before asking: hardn has no doas rule for alice |
| visudo-fails | Alpine 3.23 (doas) | checks | `visudo -c -f /tmp/hardn_sudoers_temp -> exit status 1` | `none` | plan §3.21; decision 1 (a): visudo run on a doas host; now refused, nothing checked |
| visudo-fails | Alpine 3.23 (doas) | temp files | `/tmp/hardn_sudoers_temp 0440, removed` | `none` | decision 1 (a): the temp file in /tmp; now refused, nothing written |
| visudo-fails | Proxmox VE 9 (neither tool) | refusal | `failed: invalid sudoers configuration: exit status 1` | `SUDOERS_NO_TOOL, before asking` | decision 7; control-environment.md §6: whatever the host, a sudoers file written, a failure, or nothing; now refused before asking: sudo isn't installed here |
| visudo-fails | Proxmox VE 9 (neither tool) | checks | `visudo -c -f /tmp/hardn_sudoers_temp -> exit status 1` | `none` | decision 7; control-environment.md §6: visudo run on a host without sudo; now refused, nothing checked |
| visudo-fails | Proxmox VE 9 (neither tool) | temp files | `/tmp/hardn_sudoers_temp 0440, removed` | `none` | decision 7: the temp file in /tmp; now refused, nothing written |
| tmp-symlink | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | files | `/etc/hosts 0644, /etc/sudoers.d/alice 0440` | `/etc/sudoers.d/hardn-env-alice 0440` | V20; plan §3.17: /etc/hosts overwritten through the planted link (as its owner; fs.protected_symlinks refuses root here [I]), then alice's file; now hardn-env-alice alone |
| tmp-symlink | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | temp files | `/tmp/hardn_sudoers_temp 0440, written through the symlink there to /etc/hosts, the symlink removed` | `/run/hardn/<run id>/sudoers-validate/hardn-env-alice 0440, removed` | V20; control-environment.md §2.11: a fixed name in /tmp, anyone's to take; now a copy in hardn's run directory, removed with it |
| tmp-symlink | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | backup | `none` | `/etc/sudoers.d/hardn-env-alice (absent before: the rollback removes it)` | defect 11; control-environment.md §2.11: nothing backed up; now recorded absent first, so a rollback removes it |
| tmp-symlink | Alpine 3.23 (doas) | files | `/etc/hosts 0644, /etc/sudoers.d/alice 0440` | `none` | V20; plan §3.21; decision 1 (a); defects 5, 7: /etc/hosts overwritten through the planted link, and a sudoers file on a doas host (root's under doas [I]); now nothing written |
| tmp-symlink | Alpine 3.23 (doas) | temp files | `/tmp/hardn_sudoers_temp 0440, written through the symlink there to /etc/hosts, the symlink removed` | `none` | V20; decision 1 (a): the temp file in /tmp; now refused, nothing written |
| tmp-symlink | Proxmox VE 9 (neither tool) | files | `/etc/hosts 0644, /etc/sudoers.d/alice 0440` | `none` | V20; decision 7; control-environment.md §6; defect 5: /etc/hosts overwritten through the planted link, and a sudoers file on a host without sudo (root's at a root login [I]); now nothing written |
| tmp-symlink | Proxmox VE 9 (neither tool) | temp files | `/tmp/hardn_sudoers_temp 0440, written through the symlink there to /etc/hosts, the symlink removed` | `none` | V20; decision 7: the temp file in /tmp; now refused, nothing written |
| tmp-dir | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | refusal | `failed: failed to create temporary sudoers file at /tmp/hardn_sudoers_temp: open /tmp/hardn_sudoers_temp: is a directory` | `none` | V20; control-environment.md §2.11: a planted directory at the temp name stopped it; now nothing under /tmp |
| tmp-dir | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | files | `none` | `/etc/sudoers.d/hardn-env-alice 0440` | V20; control-environment.md §2.11: the planted directory stopped it before any write; now hardn-env-alice, nothing under /tmp |
| tmp-dir | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | keep lines | `none` | `Defaults:alice env_keep += "HARDN_CONFIG"` | V20; control-environment.md §2.11: the planted directory stopped it before any write; now the line in hardn-env-alice |
| tmp-dir | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | checks | `none` | `visudo -c, visudo -cf /run/hardn/<run id>/sudoers-validate/hardn-env-alice, visudo -c` | V20; control-environment.md §2.11: stopped before its check; now the configuration before and after, and a copy of the file |
| tmp-dir | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | temp files | `none` | `/run/hardn/<run id>/sudoers-validate/hardn-env-alice 0440, removed` | V20; control-environment.md §2.11: stopped at the temp file; now a copy in hardn's run directory, removed with it |
| tmp-dir | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | backup | `none` | `/etc/sudoers.d/hardn-env-alice (absent before: the rollback removes it)` | V20; control-environment.md §2.11: nothing written or backed up; now hardn-env-alice recorded absent first, so a rollback removes it |
| tmp-dir | Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) | journal | `none` | `journalled first` | V20; control-environment.md §2.6: nothing written; now journalled first |
| tmp-dir | Alpine 3.23 (doas) | refusal | `failed: failed to create temporary sudoers file at /tmp/hardn_sudoers_temp: open /tmp/hardn_sudoers_temp: is a directory` | `SUDOERS_ENV_NEEDS_GRANT, before asking` | plan §3.21; decision 1 (a); defect 7: failed, doas never looked for; now refused before asking: hardn has no doas rule for alice |
| tmp-dir | Proxmox VE 9 (neither tool) | refusal | `failed: failed to create temporary sudoers file at /tmp/hardn_sudoers_temp: open /tmp/hardn_sudoers_temp: is a directory` | `SUDOERS_NO_TOOL, before asking` | decision 7; control-environment.md §6: whatever the host, a sudoers file written, a failure, or nothing; now refused before asking: sudo isn't installed here |

**v0.3.2** (§4): where it differs from origin/main, from [v032-reference.md](../refactor/phase0/v032-reference.md) §5.

| Scenarios | v0.3.2 |
|---|---|
| new, grant, already, commented, dotted, no-sudoers-d, root, no-user, visudo-fails, tmp-symlink, tmp-dir | no such path: v0.3.2 had no Environment screen, -e or setup-sudo-env, and wrote no env_keep line (§5, V20: origin/main only; run: git grep v0.3.2) |

<!-- TestParity: end -->

## 5. What origin/main's path did that the new one does not

From the table, and, where marked, from the code:

1. **It wrote the line into `/etc/sudoers.d/<user>`**, the file `ConfigureSudo` writes the grant to: appended to alice's grant (`grant`), which the next grant overwrites, the line lost (V16, `ops/configure_sudo`; side-effects row 17); named after the raw user name, so `alice.smith`'s is a file sudo skips (`dotted`, V16). The new path writes `hardn-env-<stem>` of its own (plan §3.17), the grant's file left byte for byte, and reports origin/main's file (`SUDOERS_LEGACY_FILE`, decision 3 (a)).
2. **It went through `/tmp/hardn_sudoers_temp`**, a fixed name anyone can take (V20): a planted symlink's target, `/etc/hosts`, overwritten and the link removed (`tmp-symlink`, a run as the link's owner; on Debian and Ubuntu `fs.protected_symlinks` refuses root's open, and the write fails instead [I]); a planted directory stopped every caller (`tmp-dir`). The new path writes nothing under `/tmp`: it checks a copy in hardn's run directory, removed with it (control-environment.md §2.11).
3. **It checked the temp file alone** (`visudo -c -f`; side-effects row 18) and dropped visudo's output (defect 12). The new path checks the configuration before and after, and the copy (`visudo -c`, `visudo -cf`), and stops before anything is written when the configuration is already broken (`visudo-fails`).
4. **It wrote in place, backed nothing up and could not roll back** (V18, defect 11). The new path replaces the file atomically, recorded absent first, journalled, where origin/main wrote nothing too (`already`, `commented`, `tmp-dir`); where `visudo -c` refuses, it has backed up and journalled first, and rolls back from the journal (`visudo-fails`).
5. **It read a commented line as kept and wrote nothing** (`commented`, defect 4), and found its own line and wrote nothing (`already`). The new path counts origin/main's exact line alone and still writes `hardn-env-alice` (decision 3 (a)).
6. **It wrote `Defaults:root` at a root login** (`root`, defect 5) and **reported success with no user** (`no-user`, defect 6). The new path refuses before asking: no user to keep it for (decision 2 (a)).
7. **It wrote a sudoers file on any host**, on Alpine and on Proxmox VE alike, and failed in a sudo message where `/etc/sudoers.d` is missing (defect 7). Its goldens are sudo runs, compared as recorded: under doas from alice, and at a root login with neither tool, it read `USER` root and wrote `Defaults:root` to `/etc/sudoers.d/root`, as `keep_root` (defect 5; written, not run), where the new path refuses (`SUDOERS_ENV_NEEDS_GRANT`, `SUDOERS_NO_TOOL`). The new path takes the tool from the host: on doas it refuses where hardn has no rule for alice (decision 1 (a); plan §3.21), with neither tool "sudo isn't installed here" (decision 7).
8. **Under `-n`, `-e` and `setup-sudo-env` were refused** (exit status 2; V1's gate), and the service under `cfg.DryRun` writes as live, reached by no path (`grant`'s dry run). Now `-n` shows the plan and writes nothing (Made here, adopted).
9. **The `su -` probe at every `LoadConfig`** and its notice (side-effects row 5; the `notice_*` goldens, not rows) has no counterpart: unit (d) removes it (decision 4).

The new path does what origin/main did not: it stops keeping the variable (decision 5 (a)), compared against nothing here (§2); and it names the file that may override hardn's rule.

## 6. UNEXPLAINED differences

**One, carried pending** (`Tie.Pending`), never as explained: in §4 and `parity.golden` its source reads "UNEXPLAINED (pending; proposed, not approved: …)".

- **`comments`, in `new`, `grant`, `already`, `commented`, `dotted`, `tmp-symlink` and `tmp-dir`, on Debian 13 and Ubuntu 26.04 (14 cells):** origin/main's file holds no comment, or, in `already`, `commented` and `tmp-dir`, origin/main wrote none; the new path's `hardn-env-<stem>` starts with `# Managed by hardn` (run: each cell's file in `parity.golden`; `sudoers.Header`, `pkg/host/sudoers`). No source this table may cite says it: no V-number, no decision of gallery-environment.md §5, no section of control-environment.md, and neither plan §3.17 nor §3.21 names a header. **Proposed tie, not approved:** [sudoers.md](../safety/sudoers.md) §2 item 6, `next/p2-sudoers`'s guarantee: "each in its own file under the header `# Managed by hardn`". **Blocking:** the table holds every other difference to a source; this one waits on the maintainer to approve that source, or name another, before the entry's `Pending` goes.

## 7. For unit (c)

- **Keep the test, and drive the screen.** `TestParity` lives in `pkg/screens/environment`, the package unit (c) fills (control-environment.md §11), as Backup's went to `pkg/screens/backup`: depguard's `screens-through-host` covers the package's non-test files alone, so the test reads goldens with `os`. The control's package would take it too (`controls-through-host` exempts tests as well), but unit (c) would then move it beside the screen it drives. Unit (c) replaces `item1` with the screen's `Run`, typed 1 and y. The table, `parity.golden` and §4 must not change but for each cell's "new path:" line: a screen that leaves a cell otherwise shows as UNEXPLAINED or a failed golden.
- **What the screen must leave as the control's apply leaves it:**
  - refused before it asks wherever `Read` refuses: no user, no tool, sudoers not included, doas with no rule of hardn's (then `Stays()`); nothing written or run;
  - else `Session.Change` with `environment.Keep{Getenv: os.Getenv, Stop: Kept}`: `hardn-env-<stem>` alone, 0440, atomic, recorded absent first, journalled; `visudo -c`, the copy checked with `visudo -cf` in the run directory and removed with it, `visudo -c`; nothing under `/tmp`; the user's own files untouched; `SUDOERS_LEGACY_FILE` in the review where origin/main's file is;
  - a configuration visudo refuses: the apply fails before `hardn-env-alice` is written, backed up and journalled first, rolled back from the journal;
  - under plan only, the plan alone.
- **The words:** the driver's title and question are the decided ones; its review has the section alone. The screen's rows and review are unit (c)'s; no aspect here compares them.

## 8. For unit (d)

**What origin/main's path did:** main menu 8 (`NewEnvironmentSettingsMenu`), `-e` and `setup-sudo-env`: §4's rows and §5.

**The record to freeze first,** before the legacy environment code goes, as parity-backup.md §8 did:

- `TestParity` reads `pkg/characterization/testdata/golden/environment/keep_*.golden` (10) and `ops/setup_sudo_preservation/*.golden` (4) in place, and `TestParityGoldens` lists those and the 21 not-rows. Move the record unchanged (`git mv`) under `pkg/screens/environment/testdata/legacy/`, in the characterization's layout, and point `legacyDir` there:
  - `environment/keep_*` with `TestLegacyEnvironmentKeep`, which runs the legacy layers, retired in the same commit;
  - `menu/environment_*` (12) with `TestLegacyMenuEnvironment`, which runs `environment_settings_menu.go`: not rows, but the screen's record;
  - `environment/notice_*` (9) with `TestLegacyEnvLossNotice`, retired when the probe goes (decision 4);
  - `ops/setup_sudo_preservation/*`: copied byte for byte if `TestOperationGoldens` keeps a case that still reaches the legacy service, moved with the case if not, as parity-dns.md §7 decided for DNS's.
  
  Then prove it: each file `cmp` equal to its old path at the freeze's parent; `go test -run 'TestParity' -update` leaves `git status` clean. `parity.golden`'s header says "read in place": kept as it is.
- **What may then go** (gallery-environment.md §9; each with its `git grep`, written, not run): `pkg/menu/environment_settings_menu.go`; `MenuManager`'s `SetupSudoPreservation`, `IsSudoPreservationEnabled` and `GetEnvironmentConfig`; `application.EnvironmentManager`; `service.EnvironmentServiceImpl`, its test and its `EnvironmentRepository`; the port `secondary.EnvironmentRepository`; `secondary.FileEnvironmentRepository`; `model.EnvironmentConfig`; `ServiceFactory.CreateEnvironmentManager`; the probe and its notice in `config.LoadConfig`. Then main menu 8 opens the screen, and `-e` and `setup-sudo-env` run the control, refusing a lockout-risk plan (Made here).

## 9. Written, not run (gate 5)

Nothing here ran on Linux or as root. origin/main's end states are its run's on macOS, as the test's user, in a temp dir, with a mock commander; the new path's are hostfake's, its visudo and doas the tests' model. What a host would show is control-environment.md §4's list: `hardn-env-alice` written under `sudo hardn` on Debian 13 and sudo-rs on Ubuntu 26.04, 0440 `root:root`, `sudo -l -U alice` showing the line; origin/main's `/etc/sudoers.d/alice` left as it was; Alpine refused without a rule of hardn's; Proxmox VE refused. Whether `fs.protected_symlinks` stops origin/main's write through the planted link on Debian and Ubuntu is not run [I].

## 10. Files and size

- **Added:** `pkg/screens/environment/parity_test.go`; `pkg/screens/environment/testdata/golden/parity.golden`; this page.
- **Changed:** `internal/parity` (the root layout's reader, `parseRoot`; the ops layout's temp files, checks and seed; `Slice.Check`, `Slice.Temps`, `Tie.Pending`, `EndState.Checks`, `EndState.Temps`, `NewStateChecked`; and their tests); [screen-kit.md](screen-kit.md) §2 and §7, [parity-dns.md](parity-dns.md) §8, [gallery-environment.md](gallery-environment.md) §9, [control-environment.md](control-environment.md) §11, the design-system README's "Running and regenerating" and the makefile's comment on `golden-update`. No non-test Go outside `internal/parity`; no legacy code, no existing golden or testdata file.
- **Size** (`git diff --numstat 9a8e3d8` and `wc -l`, run): `internal/parity` 507 lines added and 38 removed, its tests 287 and 5; Environment's part, `parity_test.go`, 800 lines of test Go, its table 53 entries; `parity.golden` 1,239 lines; this page about 250 lines; the other pages, the README and the makefile 15 lines added and 8 removed. Backup's unit (b2) for comparison (parity-backup.md §10): the harness 431 and 31, its part 632 lines, 39 entries. Over the guideline of about 500 changed lines because the table is a test: a reader of a third golden layout, checks and temp files as aspects, the driver of item 1 and `-e`, the table and its golden. No non-test Go outside `internal/parity`; `go.mod` and `go.sum` unchanged.
