# Roadmap

Features hardn doesn't have yet. None has a release or a date.

## Backup

- **Restore a backup run.** Backup (7) lists past runs and their files, and only reads. A rollback already puts back the files of the change it undoes. Restoring a run you choose would go through the same steps as any change: shown first, asked, and confirm-or-revert where it restores SSH or firewall files.

## Audit log

- **Record confirmations and rollbacks.** `/var/log/hardn/audit.jsonl` doesn't record `hardn confirm`'s confirmation itself, `hardn rollback <id>` (the revert timer's included), `--abandon` or `--boot`.

## Releases

- **Attestations as release assets.** Each binary's attestation is only in GitHub's attestation API, so checking it on a host with no network needs a signed-in `gh` elsewhere to download it first. Each release would carry its binaries' attestation bundles, and the README would document the offline check, with GitHub's repository ID comparison for a bundle not downloaded from the API.

