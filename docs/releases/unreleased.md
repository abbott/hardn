# hardn (unreleased)

## New

- **The audit log records confirmations and rollbacks.** Each `hardn confirm` and `hardn rollback` (`<id>`, `--abandon`, `--boot`) adds a line to `/var/log/hardn/audit.jsonl` with who ran it, the change's id and how it ended, refused and failed runs included. The rollback of a change nobody confirmed is recorded too. See [output.md](../reference/output.md) §4.1.
