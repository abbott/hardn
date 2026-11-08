# Roadmap

Features hardn doesn't have yet. None has a release or a date.

## Backup

- **Restore a backup run.** Backup (7) lists past runs and their files, and only reads. A rollback already puts back the files of the change it undoes. Restoring a run you choose would go through the same steps as any change: shown first, asked, and confirm-or-revert where it restores SSH or firewall files.

## Audit log

- **Record confirmations and rollbacks.** `/var/log/hardn/audit.jsonl` doesn't record `hardn confirm`'s confirmation itself, `hardn rollback <id>` (the revert timer's included), `--abandon` or `--boot`.

