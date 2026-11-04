# Release notes: unreleased (after v0.3.2)

> **What this is:** the hand-written notes for the next release, collected as each change lands; the release copies them above the commit list the release workflow writes (`.github/workflows/release.yml`, "Generate changelog", from `git log`). Started by refactor plan slice 2b, unit (b), `next/s2b-mode`, for P-03's deprecation note (decision 10, [gallery-dryrun.md](../engine/gallery-dryrun.md) §4). The repository kept no notes before: the workflow's changelog is the commit subjects alone, and plan Phase 7 (`next/p7-docs`) covers the README and contributing only. **Every line is proposed** ([dryrun-mode.md](../engine/dryrun-mode.md) §5).
>
> **Ship slice 2b's units (b) to (d) together.** Unit (d) (`next/s2b-replace`, [dryrun-replace.md](../engine/dryrun-replace.md)) removed item 6 and Run All's option 2, and refuses under plan only the menu areas that still changed the host; without it, plan only from hardn.yml would reach the menus with `-n`'s gaps ([dryrun-mode.md](../engine/dryrun-mode.md) §2).

## Changed: dry run is "plan only", and `dryRun` is read

- **`dryRun` in hardn.yml is now read.** It was ignored: `hardn` ran live whatever the file said. `dryRun: true` now starts `hardn` in plan only, as `-n` does: changes are shown, not applied.
- **`-n` and `dryRun` both mean plan only.** A flag given wins: `-n` turns it on, `--dry-run=false` turns it off, whatever the file says.
- **Operation flags are refused under plan only**, from `-n` or from the file: `-r` exits with status 2 and changes nothing. `-c`, `-d`, `-g`, `-w`, `-e` and `setup-sudo-env` show their plan instead, and exit 3 when there are changes. Add `--dry-run=false` to apply. `hardn confirm` and `hardn rollback` still run under the file's `dryRun: true`, to keep or undo a change already made.
- **Check older files for `dryRun: true`.** An earlier `hardn` may have written one, and it now means plan only. The main menu's Dry-Run item (6) saved it to `./hardn.yml`, as did any menu save made under `-n`. A script or cron job that ran `hardn -c`, `-d` or `-g`, or `-e` or `setup-sudo-env` through sudo, now shows the plan and exits 3. Set `dryRun: false`, or run with `--dry-run=false`.
- **The menu no longer saves the mode.** Its saves write `dryRun` as the file had it.
- **The main menu shows the mode.** Beside "Select an option" it says "Mode Live", or "Mode Plan only" and what set it: "(-n)" or "(hardn.yml)".
- **The main menu's Dry-Run item (6) is removed.** 6 no longer selects anything. Start with `-n`, or set `dryRun: true`, for plan only.
- **Run All no longer turns dry run on or off.** Its option 2 is removed: Run All runs in the mode `hardn` started in.
- **Under plan only, User Management, SSH Login, Firewall, Backup and Environment open.** Each of their changes stops at its plan, adding and removing a Firewall profile too, and they write nothing. User Management used to apply its changes even under `-n`.

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

## Changed: Firewall

- **Main menu 4 shows what ufw does**: whether it is installed and running, its incoming policy, whether it starts at boot, whether SSH gets through, its rules and your application profiles. Opening it changes nothing.
- **Install works.** It installs ufw with apt or apk. It used to say it wasn't yet implemented.
- **Enabling asks you to type the hostname**, and rolls back unless you run `sudo hardn confirm` from a new session. It lets SSH through, denies incoming by default, and starts ufw now and at boot. It used to disable and reset ufw first.
- **Disable works**, after showing the change. It used to say it wasn't yet implemented.
- **Profiles are applied.** Application Profiles adds a profile to hardn.yml, removes one, and applies them to ufw. Ports are written to ufw's file joined by `|`, which ufw accepts; they used to be joined by commas. Removing a profile leaves its rule in ufw.
- **On Proxmox VE, 4 reads "View pve-firewall"** and only shows pve-firewall. It changes nothing there.
- **`hardn -w` enables the firewall the same way.** On a terminal, a change that could lock you out asks you to type the hostname. Off a terminal it is refused, exit status 1: run `hardn -w` from a terminal. A change that can't lock you out is applied without asking. With `-n` it shows the plan and exits 3; it used to be refused.
- **Run All and `-r` still configure ufw the old way** until their slice: they disable and reset it, then set both default policies.
- **`ufwDefaultIncomingPolicy` and `ufwDefaultOutgoingPolicy` are not used.** Where hardn.yml sets them, hardn warns, such as "hardn.yml's ufwDefaultIncomingPolicy isn't used". The file is still accepted.

See [docs/configuration.md](../configuration.md), "Firewall Configuration with UFW Application Profiles".

## Changed: User Management

- **Main menu 1 is rebuilt.** It shows each user's admin rights, how they were granted, their SSH keys and their password state. Opening it changes nothing.
- **Each change is shown first.** Grant or remove admin rights, ask or don't ask a password for them, add or remove an SSH key, create a user. A change that could lock you out asks you to type the hostname, and rolls back unless you run `sudo hardn confirm` from a new session.
- **Some changes are refused before anything is asked**, such as removing the admin rights you are using, or your last key.
- **`hardn -c` creates the user without asking**, with admin rights, no password, and every key in hardn.yml's `sshKeys`. With `-n` it shows the plan and exits 3; it used to be refused. A name already taken, or a key that isn't one, is refused, exit status 1.
- **`-c` no longer changes sshd_config.**
- **`sudoNoPassword: false` warns.** User Management and `-c` don't use it: a new user has no password to ask. hardn warns "hardn.yml's sudoNoPassword: false isn't used by User Management or -c". Run All and `-r` still read it.
- **Run All no longer offers to set a username.** With none, it goes on without creating a user.

See [docs/configuration.md](../configuration.md), "`username`, `sudoNoPassword` and `sshKeys`: User Management".

## Changed: SSH Login

- **Main menu 2 is rebuilt.** It shows root login, password login, the SSH port, who is allowed to log in, and how you logged in, as sshd reads them. Opening it changes nothing.
- **Each change is shown first.** Disallow root login or allow it key-only, turn password login off or on, change the SSH port, move hardn's old settings. Each asks you to type the hostname, and rolls back unless you run `sudo hardn confirm` from a new session.
- **Some changes are refused before anything is asked**, such as turning root login off where no other admin could log in by key and become root.
- **`hardn -d` turns root login off the same way.** On a terminal it asks you to type the hostname. Off a terminal it is refused, exit status 1: run `hardn -d` from a terminal. With `-n` it shows the plan and exits 3; it used to be refused. Root login already off is nothing to change, exit status 0. It is refused where no other admin could log in afterwards, by key, or by password while password login is on, and become root.
- **`-d` changes root login alone.** It no longer rewrites hardn's sshd file from a stub, which reset the port and the allowed users (builds of main since v0.3.2).
- **With hardn's old sshd settings, `-d` and SSH Login's other changes are refused** until Move old settings (main menu 2) moves them.
- **`hardn -c -d` works again.** It creates the user first, then turns root login off only if an admin, the new user among them, can still log in.
- **hardn.yml's SSH settings warn where unused.** SSH Login and `-d` read sshd. `sshPort` is Change SSH port's default, and `sshConfigFile` is read to move hardn's old settings. Where `permitRootLogin`, `sshAllowedUsers`, `sshListenAddress`, `disableRootSSH` or `sshKeyPath` is set to other than its default, hardn warns, such as "hardn.yml's sshAllowedUsers isn't used by SSH Login or -d". Run All and `-r` still read `sshAllowedUsers` and `sshListenAddress`.

See [docs/configuration.md](../configuration.md), "SSH Configuration".

## Changed: Auto Updates

- **Main menu 6 is Auto Updates.** It shows whether the host updates itself, as apt or apk-cron and crond read it. Opening it changes nothing.
- **Turn automatic updates on**, after showing the change. On Debian and Ubuntu it installs unattended-upgrades where missing and writes hardn's own apt file; on Alpine it installs apk-cron and starts crond.
- **Turn them off where hardn turned them on.** hardn undoes only its own steps. Updates turned on outside hardn are left on.
- **Never on Proxmox VE.** There, 6 only shows the state.
- **hardn keeps a record** of what it turned on, under `/var/lib/hardn/controls/`.
- **Turning updates off saves `enableUnattendedUpgrades: false`** where hardn.yml says true, so Run All doesn't turn them back on.

See [docs/configuration.md](../configuration.md), "Feature Toggles".

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
