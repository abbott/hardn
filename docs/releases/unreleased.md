# Release notes: unreleased (after v0.3.2)

> **What this is:** the hand-written notes for the next release, collected as each change lands; the release copies them above the commit list the release workflow writes (`.github/workflows/release.yml`, "Generate changelog", from `git log`). Started by refactor plan slice 2b, unit (b), `next/s2b-mode`, for P-03's deprecation note (decision 10, [gallery-dryrun.md](../engine/gallery-dryrun.md) §4). The repository kept no notes before: the workflow's changelog is the commit subjects alone, and plan Phase 7 (`next/p7-docs`) covers the README and contributing only. **Every line is proposed** ([dryrun-mode.md](../engine/dryrun-mode.md) §5).
>
> **Ship slice 2b's units (b) to (d) together.** Unit (d) (`next/s2b-replace`, [dryrun-replace.md](../engine/dryrun-replace.md)) removed item 6 and Run All's option 2, and refuses under plan only the menu areas that still changed the host; without it, plan only from hardn.yml would reach the menus with `-n`'s gaps ([dryrun-mode.md](../engine/dryrun-mode.md) §2).

## Changed: dry run is "plan only", and `dryRun` is read

- **`dryRun` in hardn.yml is now read.** It was ignored: `hardn` ran live whatever the file said. `dryRun: true` now starts `hardn` in plan only, as `-n` does: changes are shown, not applied.
- **`-n` and `dryRun` both mean plan only.** A flag given wins: `-n` turns it on, `--dry-run=false` turns it off, whatever the file says.
- **Operation flags are refused under plan only**, from `-n` or from the file: `-r`, `-c`, `-d`, `-w`, `-e` and `setup-sudo-env` exit with status 2 and change nothing. `-g` shows its plan instead, and exits 3 when there are changes. Add `--dry-run=false` to apply. `hardn confirm` and `hardn rollback` still run under the file's `dryRun: true`, to keep or undo a change already made.
- **Check older files for `dryRun: true`.** An earlier `hardn` may have written one, and it now means plan only. The main menu's Dry-Run item (6) saved it to `./hardn.yml`, as did any menu save made under `-n`. A script or cron job that ran `hardn -g` now shows the plan and exits 3. Set `dryRun: false`, or run with `--dry-run=false`.
- **The menu no longer saves the mode.** Its saves write `dryRun` as the file had it.
- **The main menu shows the mode.** Beside "Select an option" it says "Mode Live", or "Mode Plan only" and what set it: "(-n)" or "(hardn.yml)".
- **The main menu's Dry-Run item (6) is removed.** 6 no longer selects anything. Start with `-n`, or set `dryRun: true`, for plan only.
- **Run All no longer turns dry run on or off.** Its option 2 is removed: Run All runs in the mode `hardn` started in.
- **Under plan only, some menu areas are refused.** User Management, and adding or removing a Firewall profile, say "Not available in plan only." They used to apply their changes even under `-n`.
- **Under plan only, Backup opens.** Each of its changes stops at its plan, and it writes nothing.

See [docs/configuration.md](../configuration.md), "`dryRun`: plan only".

## Changed: the Backup screen

- **Opening Backup writes nothing.** With backups on, it used to write and remove `.write_test` in the backup path each time it was shown, deleting a file of that name.
- **Verify Directory writes nothing.** With backups on, it used to create the directory and write a test file.
- **Relative paths, and paths under a directory others can change, are refused**, such as `backups` or `/tmp/hardn-backups`. They used to be saved.
- **Check `backupPath` in older files.** While it holds such a path, changes from the DNS and Backup screens and `-g` are refused. Change it with Backup's item 2.
- **Turning backups on or off, and changing the path, show the change first.** Only that setting is saved, in the file `hardn` loaded. They used to save the whole configuration to `./hardn.yml`.
- **A path change backs up under the new path**, as does every change after it.
- **Backups off still keeps what a rollback needs.** Changes from the DNS and Backup screens and `-g` still copy the files they change to the backup path, and say so.
- **Backup Runs lists the backups in the path in use**, newest first, and only reads. There is no restore yet.

See [docs/configuration.md](../configuration.md), "`enableBackups` and `backupPath`: the Backup screen".

## Changed: the main menu

- **An invalid choice is answered under the prompt**, which asks again. The screen is no longer cleared, and there is no "Press Enter" to go on.
- **After a screen, the main menu is drawn again below it**, so what the screen printed stays in view above it.
