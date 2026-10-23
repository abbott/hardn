# Release notes: unreleased (after v0.3.2)

> **What this is:** the hand-written notes for the next release, collected as each change lands; the release copies them above the commit list the release workflow writes (`.github/workflows/release.yml`, "Generate changelog", from `git log`). Started by refactor plan slice 2b, unit (b), `next/s2b-mode`, for P-03's deprecation note (decision 10, [gallery-dryrun.md](../engine/gallery-dryrun.md) §4). The repository kept no notes before: the workflow's changelog is the commit subjects alone, and plan Phase 7 (`next/p7-docs`) covers the README and contributing only. **Every line is proposed** ([dryrun-mode.md](../engine/dryrun-mode.md) §5).
>
> **Ship with slice 2b's unit (d), not before.** Until it, plan only from hardn.yml reaches the menus with `-n`'s gaps (User Management, Backup and Firewall's profile add and remove still apply changes), and item 6 is still there ([dryrun-mode.md](../engine/dryrun-mode.md) §2, §8).

## Changed: dry run is "plan only", and `dryRun` is read

- **`dryRun` in hardn.yml is now read.** It was ignored: `hardn` ran live whatever the file said. `dryRun: true` now starts `hardn` in plan only, as `-n` does: changes are shown, not applied.
- **`-n` and `dryRun` both mean plan only.** A flag given wins: `-n` turns it on, `--dry-run=false` turns it off, whatever the file says.
- **Operation flags are refused under plan only**, from `-n` or from the file: `-r`, `-c`, `-d`, `-w`, `-e` and `setup-sudo-env` exit with status 2 and change nothing. `-g` shows its plan instead, and exits 3 when there are changes. Add `--dry-run=false` to apply. `hardn confirm` and `hardn rollback` still run under the file's `dryRun: true`, to keep or undo a change already made.
- **Check older files for `dryRun: true`.** An earlier `hardn` may have written one, and it now means plan only. The main menu's Dry-Run item (6) saved it to `./hardn.yml`, as did any menu save made under `-n`. A script or cron job that ran `hardn -g` now shows the plan and exits 3. Set `dryRun: false`, or run with `--dry-run=false`.
- **The menu no longer saves the mode.** Its saves write `dryRun` as the file had it.
- **The main menu shows the mode.** Beside "Select an option" it says "Mode Live", or "Mode Plan only" and what set it: "(-n)" or "(hardn.yml)".
- **The main menu's Dry-Run item (6) is removed.** 6 no longer selects anything. Start with `-n`, or set `dryRun: true`, for plan only. *(Refactor plan slice 2b, unit (d): not built yet; this line waits for it.)*

See [docs/configuration.md](../configuration.md), "`dryRun`: plan only".

## Changed: the main menu

- **An invalid choice is answered under the prompt**, which asks again. The screen is no longer cleared, and there is no "Press Enter" to go on.
- **After a screen, the main menu is drawn again below it**, so what the screen printed stays in view above it.
