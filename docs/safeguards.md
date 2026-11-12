# Safeguards

What hardn refuses, and how it undoes a change. The four steps are in the [README](../README.md#how-it-works).

- **Refused before anything changes.** hardn refuses these changes when it can't show them to be safe:
  - **Root login.** Turning root's SSH login off is refused unless another admin can still log in and become root (by key, or by password while password login is on). It is also refused while you are logged in as root over SSH.
  - **Password login.** Turning password login off is refused if you logged in with a password, if an admin has a password and no key, or if no admin could log in by key.
  - **Firewall.** Enabling `ufw` first lets through every port sshd listens on. It is refused if hardn can't tell which ports those are, or if a rule already there would block SSH.
  - **One change at a time.** While a change waits for confirmation, or a rollback hasn't finished, new changes are refused.
  - The refusal says why, and in most cases what to do.
- **The revert timer.** A lockout-risk change runs under a systemd timer, or a process under OpenRC. It rolls the change back unless `hardn confirm` comes from a new connection in time. Where neither systemd nor OpenRC runs, such a change is refused. So is any lockout-risk change when a user other than root can change the hardn binary.
- **Across a reboot.** Before the first such change, hardn installs a boot unit, `hardn-boot`. At boot, before sshd starts, it restarts the timer for the time left, or rolls the change back if the time ran out. It is removed at the first boot with nothing waiting.
- **Backups.** Before a change, the files it touches are backed up under `backupPath` (`/var/backups/hardn` by default); with backups off (`enableBackups: false`), only the files a rollback needs are. A rollback restores them. `7) Backup` lists the runs.

Learn more: [revert.md](safety/revert.md) (confirm-or-revert), [revert-boot.md](safety/revert-boot.md) (the boot unit), [atomic.md](safety/atomic.md) (writes and backups), [pve.md](safety/pve.md) (Proxmox), and the index, [safety](safety/README.md).

