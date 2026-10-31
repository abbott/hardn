# Release notes: unreleased (after v0.3.2)

> **What this is:** the hand-written notes for the next release, collected as each change lands; the release copies them above the commit list the release workflow writes (`.github/workflows/release.yml`, "Generate changelog", from `git log`). Started by refactor plan slice 2b, unit (b), `next/s2b-mode`, for P-03's deprecation note (decision 10, [gallery-dryrun.md](../engine/gallery-dryrun.md) §4). The repository kept no notes before: the workflow's changelog is the commit subjects alone, and plan Phase 7 (`next/p7-docs`) covers the README and contributing only. **Every line is proposed** ([dryrun-mode.md](../engine/dryrun-mode.md) §5).
>
> **Ship slice 2b's units (b) to (d) together.** Unit (d) (`next/s2b-replace`, [dryrun-replace.md](../engine/dryrun-replace.md)) removed item 6 and Run All's option 2, and refuses under plan only the menu areas that still changed the host; without it, plan only from hardn.yml would reach the menus with `-n`'s gaps ([dryrun-mode.md](../engine/dryrun-mode.md) §2).

## Changed: dry run is "plan only", and `dryRun` is read

- **`dryRun` in hardn.yml is now read.** It was ignored: `hardn` ran live whatever the file said. `dryRun: true` now starts `hardn` in plan only, as `-n` does: changes are shown, not applied.
- **`-n` and `dryRun` both mean plan only.** A flag given wins: `-n` turns it on, `--dry-run=false` turns it off, whatever the file says.
- **Operation flags are refused under plan only**, from `-n` or from the file: `-r`, `-c`, `-d` and `-w` exit with status 2 and change nothing. `-g`, `-e` and `setup-sudo-env` show their plan instead, and exit 3 when there are changes. Add `--dry-run=false` to apply. `hardn confirm` and `hardn rollback` still run under the file's `dryRun: true`, to keep or undo a change already made.
- **Check older files for `dryRun: true`.** An earlier `hardn` may have written one, and it now means plan only. The main menu's Dry-Run item (6) saved it to `./hardn.yml`, as did any menu save made under `-n`. A script or cron job that ran `hardn -g`, or `-e` or `setup-sudo-env` through sudo, now shows the plan and exits 3. Set `dryRun: false`, or run with `--dry-run=false`.
- **The menu no longer saves the mode.** Its saves write `dryRun` as the file had it.
- **The main menu shows the mode.** Beside "Select an option" it says "Mode Live", or "Mode Plan only" and what set it: "(-n)" or "(hardn.yml)".
- **The main menu's Dry-Run item (6) is removed.** 6 no longer selects anything. Start with `-n`, or set `dryRun: true`, for plan only.
- **Run All no longer turns dry run on or off.** Its option 2 is removed: Run All runs in the mode `hardn` started in.
- **Under plan only, some menu areas are refused.** User Management, and adding or removing a Firewall profile, say "Not available in plan only." They used to apply their changes even under `-n`.
- **Under plan only, Backup and Environment open.** Each of their changes stops at its plan, and they write nothing.

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
- **A dropped SSH session during a DNS or Backup change rolls it back**, and `hardn` ends.
- **`-g` interrupted stops the flags after it**, and exits 130 if its change applied.

See [docs/configuration.md](../configuration.md), "`enableBackups` and `backupPath`: the Backup screen".

## Fixed: `hardn confirm` over ssh, and doas

- **`ssh host "doas hardn confirm"` now confirms** from a new connection. It used to be refused, because doas clears `SSH_CONNECTION`. The same rules apply: a new connection, from another machine.
- **While `ssh -L`, `-R` or `-X` is forwarding, a one-command confirm is still refused.** The refusal says to log in and run it at the prompt.
- **On a doas host, hardn's messages say `doas hardn`**: the refusals and notices about a change waiting for confirmation or a rollback, and "must be run as root". They said `sudo hardn`.

## Changed: Environment

- **Main menu 8 says "Keep HARDN_CONFIG"**, where it said "Configure environment variable".
- **Environment (8) keeps `HARDN_CONFIG` in hardn's own file**, `/etc/sudoers.d/hardn-env-<user>`. It used to add the line to `/etc/sudoers.d/<user>`, which hardn's next sudo change for that user overwrote.
- **An earlier hardn's line is read, and left.** `/etc/sudoers.d/<user>` keeping `HARDN_CONFIG` shows as kept by that file.
- **Both ways.** Once hardn's file keeps it, Environment offers to stop keeping it.
- **doas on Alpine.** hardn changes only a doas rule of its own for the user, which this release does not write. Without one, it says to add `setenv { HARDN_CONFIG }` to the rule that lets the user use doas, and changes nothing.
- **A root login is refused**: there is no user to keep it for. It used to write `Defaults:root`.
- **No more `/tmp/hardn_sudoers_temp`.** A file or link there no longer stops the change, or is written through. The change is backed up first, and rolled back if it fails.
- **`-e` and `setup-sudo-env` do the same without asking you to confirm**, and exit 1 when refused. A change that could lock you out is refused: keep it from main menu 8, which asks you to confirm. With `-n` they show the plan and make no change; they used to make it anyway.
- **`setup-sudo-env` needs root**, as `hardn` does, and reads hardn.yml where there is one, asking nothing where there is none. With none found, `-e` first asks to create one, as `hardn` does at any start, with `-n` too.
- **No notice under sudo.** At each start hardn no longer runs your login shell through `su -` to look for `HARDN_CONFIG`. Environment (8) says whether it is kept.

See [docs/configuration.md](../configuration.md), "Using with sudo or doas".

## Changed: the main menu

- **An invalid choice is answered under the prompt**, which asks again. The screen is no longer cleared, and there is no "Press Enter" to go on.
- **After a screen, the main menu is drawn again below it**, so what the screen printed stays in view above it.

## Changed: System Details and Logs

- **Main menu 9 and 10 only read.** System Details has no Export item: `hardn system-details > file` saves the report. Both open under plan only.
- **Logs reads the file hardn writes,** `/var/log/hardn.log`, whatever `logFile` says.
- **`logFile` is retired.** Where hardn.yml sets it to another path, hardn warns "hardn.yml's logFile, <path>, isn't used". The file is still accepted. hardn no longer writes the key itself.
- **`sudo hardn` keeps hardn.yml's warnings on screen.** The main menu draws below them, where it used to clear them.
- **`hardn system-details --output json`** prints the report as hardn's JSON document, as `--json` and `-O json` now do with a warning. `-O text` warns too.
- **`hardn system-details --yaml`, `-O yaml`, `-S` and `-E` are refused,** exit status 2. They never did anything. For `-E`, use `> file`.
- **`hardn system-details` with `-p`, `-c`, `-d`, `-g`, `-w`, `-r` or `-e` is refused,** exit status 2. It used to print the report and not run them.
- **A bad command line exits 2 and says so on stderr alone,** for every command: an unknown flag or command, a missing value. It used to repeat it on stdout and exit 1.
- **`hardn -p` alone needs no root,** and off a terminal prints the log's lines and nothing else. To a terminal it prints the file's details and the last lines. With another operation flag it still prints after them.
- **`hardn -p` has exit statuses.** 1 where the log can't be read, with the reason on stderr; 0 with "hardn: no log yet: <path>" on stderr where there is none.
- **As a user other than root, `hardn system-details > file` holds the report alone.** hardn's two start-up lines go to stderr.
