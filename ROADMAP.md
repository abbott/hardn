# Roadmap

Features hardn doesn't have yet. None has a release or a date.

## Backup

- **Restore a backup run.** Backup (7) lists past runs and their files, and only reads. A rollback already puts back the files of the change it undoes. Restoring a run you choose would go through the same steps as any change: shown first, asked, and confirm-or-revert where it restores SSH or firewall files.

## Audit log

- **Record confirmations and rollbacks.** `/var/log/hardn/audit.jsonl` doesn't record `hardn confirm`'s confirmation itself, `hardn rollback <id>` (the revert timer's included), `--abandon` or `--boot`.

## SSH on Alpine (CVE-2026-123158)

- **Run All turns password login off on an exposed host.** Today SSH Auth reports the bypass and Create refuses; Run All doesn't change sshd for it.
- **hardn upgrades openssh and restarts sshd.** Today you run `apk -U upgrade` and `rc-service sshd restart` yourself: `apk upgrade openssh` upgrades only the `openssh` package, not `openssh-server`, which has the fix.
