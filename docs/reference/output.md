# hardn's output: the JSON document, codes, exit statuses and the audit log

> **What this is:** the public contract of what hardn reports, to a script and to a person (brief §2, "A fixed exit-code set and versioned JSON output", decided; brief Phase 3, the JSON contract, the exit codes and the audit log). Built by refactor plan Phase 3, `next/p3-output`, in `pkg/output`; [docs/engine/output.md](../engine/output.md) says what the package guarantees and how. **Once released, none of it changes but by the rules below.**
> **Status:** built and tested (goldens of every document below, the human report of each, the exit statuses, the audit log on the recording fake); the operation flags, the screens and `hardn confirm` run on the engine and have run on the Debian 13, Ubuntu 26.04 and Alpine 3.23 VMs. **JSON reaches the command line on `hardn system-details --output json` alone** (§1.10); no other command has `--output`. Every operation flag and every screen's change reports as §7 says and exits as §3 says; the audit log (§4) records each. `--yes` and `--confirm-timeout` are flags of `hardn` (refactor plan §3.11, adopted 2026-11-04); §6 says what `--yes` does off a terminal, and what it doesn't.

## 1. The JSON document

One run writes **exactly one JSON value on stdout**, then a newline; everything for a person (progress, warnings, notices) goes to stderr, so stdout parses whole (`TestStdoutIsOneDocument`). The same document, with a `time`, is one line of the audit log (§4). Strings are UTF-8; `<`, `>` and `&` are not escaped; DEL, C1 controls and format characters are, as `\uXXXX`, as stdout may be a terminal, the string's value unchanged (`TestJSONEscapesWhatATerminalActsOn`). **Arrays are never `null`**: an empty one is `[]`. Optional objects (`result.confirm`, `result.rollback`, `result.boot`) are left out when they do not apply.

### 1.1 `schemaVersion` and the rule for bumping it

`schemaVersion` is `1`. **Without** a new version: a field may be added anywhere; a new value may be given to a field this page calls *open* (codes, outcomes, change statuses, stages, steps, risk classes, `host` words), so a script must accept a value it does not know. **With** a new version: anything that would break a script reading version 1: a field removed or renamed, its type or meaning changed, a value of a closed field removed or its meaning changed, a code removed or renamed (§2), an exit status's meaning changed (§3). The codes and statuses are pinned by goldens (`pkg/output/testdata/codes.golden`, `sentinels.golden`, `json/*.json`), so such a change cannot land unseen.

### 1.2 The nine fields

| Field | Type | Meaning |
|---|---|---|
| `schemaVersion` | number | `1` (§1.1) |
| `hardnVersion` | string | the build's version (`main.Version`, set by the release build) |
| `host` | object | the host the run read (§1.3) |
| `mode` | string | `check` (read each control's state, change nothing), `plan` (the dry run: plan, change nothing), `apply` (plan, then apply) |
| `result` | object | how the run ended (§1.4) |
| `changes` | array | the plan's changes, in the order an apply runs them (§1.5); `[]` for a check |
| `status` | array | each control's state, for a check (§1.6); `[]` otherwise |
| `findings` | array | what the user should know that stops nothing (§1.7) |
| `errors` | array | each error of the run, with its code (§1.8) |

### 1.3 `host`

detect's facts in this schema's own words (they are mapped, never written as detect holds them: [detect.md](../safety/detect.md) §6). A fact not established is `"unknown"`.

| Field | Values |
|---|---|
| `hostname` | the host's name; `""` when not read |
| `os.id`, `os.versionId`, `os.codename`, `os.prettyName` | `/etc/os-release`'s `ID`, `VERSION_ID`, `VERSION_CODENAME`, `PRETTY_NAME`, as written there |
| `platform.distro` | `debian`, `ubuntu`, `alpine`, `proxmox`, `other`, `unknown` (open) |
| `platform.version` | the support matrix's key (`13`, `24.04`, `3.24`); `""` when not established |
| `platform.support` | `supported`, `best-effort`, `out-of-scope`, `unlisted`, `unknown` |
| `init` | `systemd`, `openrc`, `unknown` (open) |
| `ssh` | how sshd is started: `ssh.socket`, `ssh.service`, `openrc`, `unknown` (open) |
| `proxmox.installed`, `container.detected` | `yes`, `no`, `unknown`; with `proxmox.version` (pve-manager's) and `container.manager` |
| `elevation.sudo`, `elevation.doas` | `yes`, `no`, `unknown`; `elevation.sudoFlavor`: `sudo`, `sudo-rs`, `unknown` (open) |

### 1.4 `result`

| Field | Meaning |
|---|---|
| `outcome` | the run in one word (open); each has its exit status (§3): `nothing-to-do` 0, `applied` 0, `awaiting-confirmation` 0, `changes-needed` 3 (a check or a plan found changes to make), `declined` 3 (the user declined them at the confirmation, or quit with `q` or went back with `0` before they were applied: nothing changed), `refused` 1, `failed` 1, `rolled-back` 1 (every change started was undone), `rollback-incomplete` 1 (the rollback did not finish here, another process had the change, or a change was not covered: `result.rollback` says which), `usage-error` 2, `cancelled` 130. **`cancelled` says nothing of a rollback**: an interrupted apply is rolled back, and `result.rollback.finished` and `result.rollback.command` say whether that finished and how to finish it |
| `exitCode` | the run's exit status, so that the audit log has it too |
| `id` | the change's id (its journal's, its backups' and its revert timer's, `<backupPath>/<id>/`); `""` without a plan |
| `risk` | the plan's risk class: `plain`, `package-install`, `service-restart`, `lockout` (open); `""` without a plan |
| `confirm` | only while a lockout-risk change waits for confirmation: `deadline` (RFC 3339, UTC: when the revert timer fires), `approximate` (true when the timer's own trigger time could not be read and the deadline is the journal's: the timer fires a few seconds after it, 1 to 2 s on Ubuntu 26.04, [vmtest.md](../safety/vmtest.md) §4.9), `via` (the listeners a confirming connection must reach, `address:port`), `command` (`sudo hardn confirm`, `doas hardn confirm` on a host with doas and no sudo), `undo` (`sudo hardn rollback <id>`, doas likewise) |
| `rollback` | only when an apply stopped once its change was journalled, whatever the plan's risk: `finished` (the rollback in this run finished and undid every change started), `byAnotherProcess` (another process had taken the change: the timer's rollback, a `hardn rollback <id>`, a rollback given up, a confirmation; this run undid nothing, and its `ROLLBACK_RUNNING` error says which), `step` (`start`, `window`, `backup`, `journal`, `apply`, `journal-after`, `mark-applied`, `done`), `change` (the change that failed, or at `start` and `window` the one that was next and never started; `""` for the apply's last write), `changeUndone` (true when nothing of the change that failed is left either: it undid its own steps before the rollback, as DNS's does, or left the rest to its journalled undo and this rollback finished; added by `next/s2-control`), `restored` (files put back), `undone` (the inverse steps run, in words), `unjournalled` (§1.4.1; left out when none), `command` (`sudo hardn rollback <id>`, `doas` on a host with doas and no sudo, which finishes a rollback that did not finish; `""` otherwise) |
| `boot` | only when the apply ran the install of hardn's boot unit, before the change was journalled ([engine-boot.md](../engine/engine-boot.md) §2.1): `installed` (false when the install failed, and nothing was armed), `detail`. It is never rolled back; it removes itself at a boot that finds nothing journalled |
| `backup` | only when an apply applied every change (`applied`, `awaiting-confirmation`) and kept a copy of a file first: `dir` (the backup run's directory, `<backupPath>/<id>`), `files` (each file whose content was kept, once, in the order backed up; a path that was not there before is not one), `takenAnyway` (true when a copy was taken with `enableBackups: false`, as the rollback needs it, §3.16). Added by `next/s3-control` ([control-backup.md](../engine/control-backup.md)); the human report says the same after "Operation Successful": "Backup of <files> kept in", the directory, and "Backed up anyway: the rollback needs it." when `takenAnyway` |

#### 1.4.1 `result.rollback.unjournalled`

A change that ran whose undo the journal did not hold once it had run (its journal after the step could not be written, or another process's rollback took the journal while it ran), so the rollback did not run it: **what it did may be left on the host**, and this is what undoes it, for a person to check and run.

| Field | Meaning |
|---|---|
| `change` | the change's id |
| `undo` | what undoes it, in the order to run: each command as its argv (`ufw delete allow 2230/tcp`), a unit's kept plan as `the <unit> plan's rollback`, another step as its operation and path, then each reload (`reload ssh`) |
| `backups` | the files it wrote, each `path` with its `seq`, the backup in `dir` that restores it |
| `dir` | the change's backup run directory (`<backupPath>/<id>`) |
| `why` | why the journal does not hold it |

### 1.5 `changes[]`

| Field | Meaning |
|---|---|
| `id` | unique in the plan (`ssh.port/files`); `hardn.boot-unit` for the boot unit |
| `control` | the control that planned it; `""` for hardn's own, the boot unit's |
| `summary`, `target` | one line; the file or service it acts on |
| `risk`, `stage` | the risk class (§1.4); the stage it runs in: `boot`, `packages`, `accounts`, `firewall`, `files`, `reload`, `verify` (open), in that order (brief 2.3) |
| `after` | the changes it runs after |
| `diff` | a unified diff for a file; `""` for none |
| `stateBefore`, `stateAfter` | a setting's or service's state, for a change with no file to diff |
| `commands`, `backups` | the commands it runs; the files backed up before it runs |
| `status` | `planned` (a check's or a plan's), `applied`, `failed` (the change that failed; `result.rollback` says what was undone), `rolled-back` (applied, then undone by the rollback in this run, which finished), `unknown` (applied, but the rollback did not finish here or another process had the change, so whether it still stands is not established), `not-run` (the apply stopped before it started: Ctrl-C, or the confirm window closing, between two changes), `unjournalled` (§1.4.1) (open) |
| `detail` | what it did, or why it failed or did not run |

### 1.6 `status[]`

`control`, `title`, `state` (`configured`, `not-configured`, `partial`, `not-applicable`, `unknown`; open), `detail` and `evidence` (what the state was read from, the effective value rather than the file: brief §3 principle 3). A check that failed is `unknown`, with its error in `errors[]`.

### 1.7 `findings[]`

`code` (§2.2), `detail`, `source` (a file and line, a command; `""` when not known), `control` (whose check or plan found it), `change` (the change it rides on; `""` for a check's). A finding stops nothing.

A check with a risk level (`Run.RiskLevel`, added by `next/p4-risk`) also carries its cross-checks' own findings, after the controls': why a reboot is pending (`REBOOT_*`, the header probe's) and `UPTIME_NO_AUTO_UPDATES`, each with no `control` and no `change`. The level itself, its band and its factors, is the main menu's, not this document's ([risk.md](../engine/risk.md) §8 (j)).

### 1.8 `errors[]`

`code` (§2.1), `message` (the error's text, which may change: match on the code), `control` and `change` (what it is about, when known), `step` (open: the apply's step, as `result.rollback.step`; `boot-unit` for the boot unit's install; `rollback` for the rollback's own error; `""` otherwise), and for a refusal with a report, `why` (why the host is refused) and `fix` (what makes it one hardn can change). An apply that failed gives one entry for its step's error, one for its rollback's error when the rollback did not finish, and one (`CHANGE_UNJOURNALLED`, its message naming what undoes the change, as §1.4.1) for a change the journal did not cover; a refused plan, one per refusal. Each error is there once, however the run's own error wraps it. Quitting (`q`) or going back (`0`) is not an error (`declined`), nor is the non-interactive policy's refusal for a plan with nothing to change (§6).

### 1.9 The documents

The goldens are the reference, each a whole document: `pkg/output/testdata/json/check.json` (a check: drift, a finding, a deprecated value; exit 3), `plan.json` (the 2230 scenario's plan on Debian 13, the boot unit first; exit 3), `refused.json` (the D9 host refused with its report, [P-39](../refactor/proposals.md#p-39) (a); exit 1), `awaiting-confirmation.json` (applied, the deadline approximate; exit 0), `rolled-back.json` (the reload failed, the rollback finished; exit 1) and `unjournalled.json` (from the engine's own run: the journal after a step failed, the change before it rolled back, the change itself with what undoes it; exit 1). The `result` of the one waiting for confirmation:

```json
{
  "outcome": "awaiting-confirmation",
  "exitCode": 0,
  "id": "20261019T120000Z-1a2b3c4d",
  "risk": "lockout",
  "confirm": {
    "deadline": "2026-10-19T12:03:00Z",
    "approximate": true,
    "via": ["0.0.0.0:2230", "[::]:2230"],
    "command": "sudo hardn confirm",
    "undo": "sudo hardn rollback 20261019T120000Z-1a2b3c4d"
  },
  "boot": { "installed": true, "detail": "wrote /etc/systemd/system/hardn-boot.service; enabled it" },
  "backup": {
    "dir": "/var/backups/hardn/20261019T120000Z-1a2b3c4d",
    "files": ["/etc/ssh/sshd_config.d/00-hardn.conf"],
    "takenAnyway": false
  }
}
```

### 1.10 `details`: `hardn system-details`'s form alone

**`hardn system-details --output json` reaches it** (`cmd/hardn/readers.go`; `TestDetailsDocuments` holds the form; §3.11's change for this one command, approved on 2026-10-30, narrowed: [gallery-system-details.md](../engine/gallery-system-details.md) §5, decision 1 (a)). The form is the document of §1, mode `check`, with the nine fields and one more; **every other document, and so the audit log's lines (§4), has the nine alone** (`TestDetailsOnThisFormAlone`, `pkg/output/output_test.go`'s `TestStdoutIsOneDocument`). Added as §1.1 allows, so `schemaVersion` stays `1`. Goldens: `pkg/output/testdata/json/details.json` (every section read), `details-unreadable.json` (none).

**Here a `null` is a fact**, where the rest of the document's arrays are never `null`: a section, or one fact of it, that was not read or is not known is `null`. An array read and holding none is `[]`. Each section with anything not read adds one finding, `DETAILS_UNREADABLE`, `detail` `<section> not read`, `source` each thing not read, joined by `, `. It stops nothing: the rest was read, so the exit status stays as it was.

| Field | Type | Meaning |
|---|---|---|
| `kernel` | string or null | `/proc/sys/kernel/osrelease` |
| `uptime` | string or null | as the main menu's header words it: `3 days, 5 hours, 12 minutes` |
| `addresses` | array or null | each global address: `interface`, `family` (`inet`, `inet6`), `address` (with its prefix length, as `ip` prints it) |
| `users` | array or null | uid 1000 and up, but `nobody` and `nfsnobody`: `name`, `tool`: `sudo` or `doas` (open) for the tool whose rules let them run every command as root, `""` for none, `unknown` where that is not established, as where hardn runs as another user and cannot read the rules |
| `cpu` | object or null | `model` (null where `/proc/cpuinfo` has none, as on arm64 [I, not run]), `cpus` (logical CPUs), `sockets` (null likewise; none is made up), `load` (the 1, 5 and 15 minute figures as `/proc/loadavg` writes them) |
| `disk` | object or null | `source` and `type` from `/proc/self/mounts`' line for `/` (the device, or on ZFS the dataset), `usedBytes` and `totalBytes`; on ZFS alone, `pool` and `health`: as `zpool` says it (open), or `unknown` where no `zpool` answers, as in an LXC guest on ZFS. Every ZFS form is [unverified]: no ZFS VM |
| `memory` | object or null | `totalBytes`, and `usedBytes`, what `MemAvailable` leaves; null on a kernel with no `MemAvailable` |
| `logins` | array or null | root, then each listed user, from one `lslogins` call: `user`, `when` (as `lslogins` prints it), `tty`, `from`, each `""` for none recorded; `unknown: true`, there only then, where nothing is known of the account: `lslogins` had no line for it, or a blank where a source refuses or none holds records. `null` where `lslogins` failed or is not installed, never "no login" |

`host.hostname` and `host.os` hold the rest of what the screen shows. A blank `when` with no `unknown` says "none recorded", which sees only logins with a terminal: `ssh host cmd` moves nothing ([gallery-system-details.md](../engine/gallery-system-details.md) §5, decision 3).

## 2. Codes

Every code is SCREAMING_SNAKE, stable once released, and never reused for another meaning. A script matches codes, never messages. Codes are open: a new release may add codes, and an error it did not have a name for before may get one; an error hardn does not know by name is `UNCLASSIFIED_ERROR`, with its message.

**How an error gets its code.** The registry is one ordered list (`pkg/output/codes.go`); an error takes the first entry that matches it by `errors.Is` or `errors.As`, so the more specific comes first ([steps.md](../engine/steps.md) §7): the engine's change kinds' refusals before the primitive refusals they wrap (the D9 refusal is `SSH_LISTEN_CONF_NOT_IN_EFFECT`, not `RELOAD_RESTORED_LISTENERS`; `/run/sshd` is `SSHD_ENV_NO_RUNDIR` from either); a typed error told apart by a field before the sentinel it matches (each of `hardn rollback --abandon`'s refusals by its kind, not as `NOTHING_PENDING`, whose words are about confirming; the run lock and the journal lock before `LOCKED`); a sentinel that wraps another before it (`SYMLINK_IN_UNSAFE_DIRECTORY` before `SYMLINK_REFUSED`, `DNS_NO_NAMESERVERS` before `DNS_INVALID`); the engine's before the primitives' errors it wraps; the standard library's, and the engine's wrappers of a whole plan or apply (`CHANGE_UNJOURNALLED`, `ROLLED_BACK`, `PLAN_REFUSED`), last. An interruption anywhere in an error is `CANCELLED`. An apply's error takes its step's code.

The *Exit* column is the status an error of the code gives a run (§3).

### 2.1 Error codes, in precedence order

| Code | Exit | Meaning |
|---|---|---|
| `CANCELLED` | 130 | interrupted: Ctrl-C, SIGTERM or a hangup, or a prompt aborted (Ctrl-C, the end of the input) |
| `SSH_LISTEN_CONF_NOT_IN_EFFECT` | 1 | v0.3.x's listen.conf never took effect (D9): refused, with a report (P-39 (a)) |
| `SSHD_ENV_NO_RUNDIR` | 1 | sshd -T cannot run: its privilege separation directory (/run/sshd) is missing or not root's alone |
| `SSH_SETTINGS_NOT_READ` | 1 | sshd's settings are not read, so no SSH change is planned (the SSH Login screen's refusals, `sshchange.Err*`; new in `next/s6-control`) |
| `SSH_SESSION_NOT_LOGGED` | 1 | the session's login is not in sshd's log, so nothing that closes a way in is planned |
| `SSH_ROOT_SESSION` | 1 | the session is root's own SSH login |
| `SSH_NO_ADMIN_LOGIN` | 1 | no admin could log in over SSH and become root afterwards |
| `SSH_PASSWORD_SESSION` | 1 | the session logged in with a password |
| `SSH_ADMIN_PASSWORD_ONLY` | 1 | an admin with a password and no key would lose SSH login |
| `SSH_MATCH_LETS_MORE` | 1 | a Match block lets more in than hardn's setting |
| `SSH_PVE_ROOT_KEY_ONLY` | 1 | root stays key-only on Proxmox VE |
| `SSH_PVE_ROOT_PASSWORD_SESSION` | 1 | the session is root's own SSH login, by password |
| `SSH_PVE_NO_ROOT_KEY` | 1 | root could not log in by key afterwards |
| `SSH_PORT_SOURCE_RESTRICTED` | 1 | ufw lets the SSH port in from some sources alone |
| `SSHD_NO_WRITE_STRATEGY` | 1 | no write strategy for this sshd_config |
| `SSH_MIGRATE_FIRST` | 1 | legacy hardn SSH files: migrate them first |
| `SSH_PORT_NO_NEW_LISTENER` | 1 | the port change adds no listener |
| `SSHD_CHANGE_NOT_IN_EFFECT` | 1 | sshd does not show the change |
| `SSHD_FILE_CHANGED` | 1 | a file the SSH change writes is not as the plan read it |
| `PVE_ROOT_LOGIN_NOT_KEPT` | 1 | the cluster's root SSH login by key would not be kept |
| `SSH_CLEANUP_SESSION_NOT_KEPT` | 1 | the clean-up's session is not connected to a port it keeps (`hardn confirm`'s clean-up, P-40) |
| `UPDATES_NOT_READ` | 1 | how updates run is not read, so no change is planned (the Auto Updates screen's refusals, `updates.Err*`; new in `next/s7-control`, [control-updates.md](../engine/control-updates.md)) |
| `UPDATES_PERIODIC_OFF` | 1 | APT::Periodic::Enable turns apt's daily jobs off |
| `UPDATES_TIMER_OFF` | 1 | an apt timer is off |
| `UPDATES_JOB_MASKED` | 1 | an apt daily service is masked |
| `UPDATES_JOB_MISSING` | 1 | an apt daily service is missing |
| `UPDATES_NO_ORIGINS` | 1 | unattended-upgrades allows no origin |
| `UPDATES_PROXMOX` | 1 | hardn does not change updates on Proxmox VE |
| `UPDATES_OTHER_PACKAGES` | 1 | the package change would change other packages |
| `UPDATES_NOT_IN_REPOS` | 1 | unattended-upgrades is not in apt's package lists |
| `UPDATES_NOT_IN_EFFECT` | 1 | apt does not read the update setting as changed |
| `UPDATES_RECORD_UNREADABLE` | 1 | hardn's record of the updates it turned on cannot be read |
| `UPDATES_CHANGED_SINCE` | 1 | a file hardn left changed since |
| `UPDATES_NOTHING_TO_UNDO` | 1 | hardn did not turn updates on |
| `UPDATES_STAY_ON` | 1 | another file keeps updates on without hardn's |
| `CONFIG_CHANGED` | 1 | hardn.yml changed since it was read: saving would undo what changed it (the save of hardn.yml, [control-dns.md](../engine/control-dns.md) §2.9; `config.Save`'s too since `next/s2-config-edit`, [config-edit.md](../engine/config-edit.md)) |
| `CONFIRM_WINDOW_CLOSING` | 1 | the confirm window is closing |
| `BOOT_UNIT_NOT_INSTALLED` | 1 | the boot unit was not installed, so nothing is armed |
| `PLATFORM_UNSUPPORTED` | 1 | no changes on a platform outside the support matrix |
| `ENGINE_CHANGE_INVALID` | 1 | change not valid |
| `ENGINE_ORDER` | 1 | changes cannot be ordered |
| `ENGINE_CONFLICT` | 1 | two changes of the plan act on the same thing |
| `PLAN_CHANGED` | 1 | the host changed since the plan was made; plan again |
| `PLAN_USED` | 1 | the plan was applied already; plan again |
| `CONTROL_NOT_MIGRATED` | 1 | the control only checks the host; its changes are not planned yet (a check-only control's plan, refused: [checks.md](../engine/checks.md)) |
| `CONFIRMATION_REQUIRED` | 2 | applying changes without a terminal needs an explicit yes |
| `USAGE` | 2 | a usage error: bad flags or configuration |
| `AUDIT_DIR_UNSAFE` | 1 | the audit log's directory is not root's alone |
| `CONFIG_NOT_LOADED` | 1 | the configuration was not loaded from a file |
| `CONFIG_FILE_INVALID` | 1 | hardn.yml does not decode as the configuration (`config.Read`, [control-dns.md](../engine/control-dns.md) §2.9; added by `next/s2-control`) |
| `CONFIG_NOT_EDITABLE` | 1 | hardn.yml cannot be edited in place: among others, several documents, an anchor or alias on a setting to change, a merge (`<<`), a comment inside brackets, and the other forms of [config-edit.md](../engine/config-edit.md) §5 (`config.Edit`; added by `next/s2-config-edit`) |
| `CONFIG_EDIT_UNSAFE` | 1 | the edited hardn.yml would not read back as the configuration to save, so nothing is written (`config.Edit`, [config-edit.md](../engine/config-edit.md); added by `next/s2-config-edit`) |
| `RUN_LOCK_HELD` | 1 | another hardn holds the run lock: an apply, or hardn rollback --boot |
| `JOURNAL_LOCK_HELD` | 1 | another hardn has held the journal lock past its wait |
| `ABANDON_OTHER_ROLLBACK_RUNNING` | 1 | nothing given up: another change's rollback runs now |
| `ABANDON_OTHER_ROLLBACK_UNFINISHED` | 1 | nothing given up: another change's rollback has not finished |
| `ABANDON_NOT_STARTED` | 1 | nothing given up: the change is journalled and its rollback has not started |
| `ABANDON_OTHER_PENDING` | 1 | nothing given up: another change is journalled |
| `ABANDON_GIVEN_UP` | 1 | nothing given up: the change's rollback was given up already |
| `ABANDON_ROLLED_BACK` | 1 | nothing given up: the timer rolled the change back |
| `ABANDON_NOT_JOURNALLED` | 1 | nothing given up: the change is not journalled |
| `CHANGE_ROLLBACK_GIVEN_UP` | 1 | nothing to confirm: the change was not confirmed in time, and its rollback did not finish and was given up |
| `CHANGE_ROLLED_BACK` | 1 | nothing to confirm: the change was not confirmed in time and was rolled back |
| `CONFIRM_TIMEOUT_INVALID` | 2 | the confirm timeout is not accepted (confirmTimeout: a duration of at least 60 s) |
| `CHANGE_ID_INVALID` | 2 | not a change id |
| `JOURNAL_INVALID` | 1 | the change journal is not valid |
| `JOURNAL_UNSAFE` | 1 | the change journal, the run directory, or a directory above them, can be changed by a user other than root |
| `RECORD_INVALID` | 1 | hardn's record of what it changed for a control is not valid |
| `CHANGE_PENDING` | 1 | a change is already journalled |
| `NOTHING_PENDING` | 1 | no change is waiting for confirmation |
| `NOT_CONFIRMABLE` | 1 | the journalled change is not waiting for confirmation |
| `ROLLBACK_RUNNING` | 1 | a rollback of the change has started |
| `CONFIRM_SAME_SESSION` | 1 | this is the SSH connection that applied the change |
| `CONFIRM_OLD_CONNECTION` | 1 | this SSH connection was open before the change was applied |
| `CONFIRM_LOCAL_CONNECTION` | 1 | this SSH connection comes from the host itself |
| `SESSION_UNKNOWN` | 1 | this session's SSH connection is not established |
| `CONFIRM_WRONG_LISTENER` | 1 | this connection did not reach a listener the change added |
| `CHANGE_APPLIED_ALREADY` | 1 | the change is applied already |
| `REVERT_NO_TIMER` | 1 | no revert timer for this init system |
| `HARDN_EXECUTABLE_UNSAFE` | 1 | not safe to run as root from the timer |
| `JOURNAL_CLAIM_RACE` | 1 | the journal changed while it was being claimed |
| `ROLLBACK_INCOMPLETE` | 1 | the rollback did not finish |
| `PLAN_ROLLBACK_UNREGISTERED` | 1 | no rollback is registered for the unit of this plan |
| `BOOT_UNIT_NOT_AS_PLANNED` | 1 | the boot unit is not as planned |
| `LOCK_UNAVAILABLE` | 1 | a lock cannot be taken: its directory is not root's alone, the file is not a regular file, or the disk is full |
| `PLAN_INVALID` | 1 | not a plan its unit's rollback accepts |
| `SSHD_CONFIG_INVALID` | 1 | sshd rejects the configuration |
| `SSHD_ENV_NO_HOSTKEYS` | 1 | sshd -t loaded no host key (as without root, or from the configuration's HostKey files) |
| `SSHD_ENV_NO_PRIVSEP_USER` | 1 | sshd's privilege separation user does not exist |
| `SSHD_ENVIRONMENT` | 1 | sshd cannot check the configuration on this host |
| `SSHD_UNEXPECTED` | 1 | unexpected sshd result |
| `SSHD_INCLUDE_UNSUPPORTED` | 1 | an Include the validation copy cannot reproduce |
| `SSHD_DROPIN_NOT_INCLUDED` | 1 | sshd_config does not Include sshd_config.d |
| `SSHD_CONFIG_NOT_REGULAR` | 1 | an sshd configuration file is not a regular file |
| `SSHD_LOGIN_NOT_LOGGED` | 1 | sshd's log has no login for this session (`sshd.ReadAccepted`; new in `next/s6-control`) |
| `SSH_MIGRATE_INCLUDE_OTHER` | 1 | sshd_config's first directive is not the stock Include, so hardn has no place for its settings |
| `SSH_MIGRATE_NOT_ESTABLISHED` | 1 | the configuration's login paths are not established |
| `SSH_MIGRATE_FEWER_LOGIN_PATHS` | 1 | fewer working login paths than the installed configuration |
| `SSH_MIGRATE_LISTENER_OVERLAP` | 1 | listeners that cannot be bound together |
| `SSH_MIGRATE_CHANGED` | 1 | a legacy SSH file changed since the migration was planned |
| `RELOAD_MODE_UNKNOWN` | 1 | how sshd is started is not established |
| `RELOAD_NO_GENERATOR` | 1 | ssh.socket's listeners are not derived from sshd_config |
| `RELOAD_SOCKET_DROPIN` | 1 | a drop-in overrides the ssh.socket listeners sshd_config sets |
| `RELOAD_LISTENERS_UNKNOWN` | 1 | the listeners before or after the change are not established |
| `RELOAD_DROPS_LISTENER` | 1 | the change drops a listener before it is confirmed |
| `RELOAD_ADDS_LISTENER` | 1 | a confirmed change adds a listener |
| `RELOAD_FLAVOUR_SWITCH` | 1 | the change may switch the sshd binary OpenRC runs |
| `RELOAD_UNIT_STATE_UNKNOWN` | 1 | the unit's state is not established |
| `RELOAD_UNIT_DOWN` | 1 | the unit is not up after the step |
| `RELOAD_LISTENER_IN_USE` | 1 | a listener the change adds is already bound |
| `RELOAD_SOCKET_CONFIG` | 1 | ssh.socket is not configured for every listener |
| `RELOAD_NOT_BOUND` | 1 | not every listener is bound |
| `RELOAD_RESTORED_LISTENERS` | 1 | the restored ssh.socket drop-ins do not give the socket the listeners it had |
| `RELOAD_NOT_IN_EFFECT` | 1 | the configured listeners are not the ones in effect |
| `RELOAD_OPENRC_CONFD` | 1 | conf.d sets the sshd binary, configuration file or arguments |
| `FIREWALL_SS_NOT_INSTALLED` | 1 | ss is not installed, so which ports SSH uses is not established and the firewall is not enabled |
| `FIREWALL_SS_FAILED` | 1 | ss failed, so which ports SSH uses is not established and the firewall is not enabled |
| `FIREWALL_SSH_NOT_IN_EFFECT` | 1 | a listener sshd is configured for is not bound, so the firewall is not enabled |
| `FIREWALL_SSH_REFUSED` | 1 | ufw refuses an SSH listener, and hardn changes no rule it finds |
| `FIREWALL_BOOT_UNKNOWN` | 1 | whether ufw starts at boot is not established |
| `FIREWALL_BOOT_BLOCKED` | 1 | ufw's service is masked or missing |
| `UFW_ROLLBACK_FAILED` | 1 | an undo of ufw's rules failed; those after it did not run |
| `UFW_NOT_APPLICABLE` | 1 | ufw is not used on Proxmox VE: pve-firewall manages its packet filter |
| `UFW_PROXMOX_UNKNOWN` | 1 | ufw: whether the host runs Proxmox VE (no ufw there) is not established |
| `UFW_NOT_INSTALLED` | 1 | ufw is not installed |
| `UFW_LISTENERS_UNKNOWN` | 1 | the SSH listener set is not established |
| `UFW_STATE_UNKNOWN` | 1 | ufw's state is not established |
| `UFW_WOULD_BLOCK` | 1 | the change would block an SSH listener |
| `UFW_VERIFY_FAILED` | 1 | ufw status does not show the planned state |
| `UFW_ENABLED_MISMATCH` | 1 | ufw's ENABLED setting and its loaded chains disagree |
| `UFW_INACTIVE` | 1 | ufw is not running: its rules would only be saved |
| `UFW_BAD_PROFILE` | 1 | the application profile is not one ufw reads |
| `UFW_BAD_INTERFACE` | 1 | the interface is not one ufw takes |
| `PACKAGE_NO_TOOL` | 1 | hardn can't install packages on this system |
| `PACKAGE_NOT_IN_REPOS` | 1 | package not found in the repositories |
| `PACKAGE_OTHER_CHANGES` | 1 | installing it would change other packages |
| `PACKAGE_VERIFY_FAILED` | 1 | package not installed after the install |
| `PVE_NOT_APPLICABLE` | 1 | not a Proxmox VE host |
| `PVE_UNKNOWN` | 1 | whether the host runs Proxmox VE is not established, so its rules cannot be applied |
| `PVE_PORT_CHANGE_REFUSED` | 1 | the SSH port stays 22 on Proxmox VE: cluster SSH and pve-firewall's management rule use port 22 |
| `PVE_ROOT_EXCLUDED` | 1 | root must stay reachable by key on Proxmox VE: cluster nodes log in to each other as root |
| `PVE_ROOT_PASSWORD_KEPT` | 1 | root's password stays usable on Proxmox VE: the web UI's root@pam login and the cluster join use it |
| `PVE_CLUSTER_FS_DOWN` | 1 | pmxcfs is not mounted at /etc/pve |
| `PVE_KEYS_LINK` | 1 | root's authorized_keys is not Proxmox VE's link to /etc/pve/priv/authorized_keys |
| `PVE_KEYS_CHANGED` | 1 | the cluster-wide authorized_keys changed since the plan |
| `PVE_BAD_KEY` | 1 | not an authorized_keys key line |
| `PVE_CLUSTER_KEY` | 1 | the edit would remove a cluster node's root key, or every key |
| `PVE_NO_BACKUP` | 1 | Proxmox VE's keys edit needs a backup run |
| `PVE_PLAN_MISMATCH` | 1 | the keys plan's new content does not follow from its edit |
| `PVE_FIREWALL_UNKNOWN` | 1 | pve-firewall's state is not established |
| `USERS_ELEVATION_NOT_READ` | 1 | the elevation tool's rules are not read, so no grant is planned (`users.Err*`, each a `steps.Refusal` in the words of [gallery-users.md](../engine/gallery-users.md) §6); new in `next/s5-control` |
| `USERS_NO_TOOL` | 1 | no elevation tool is installed, so no grant is planned |
| `USERS_NOT_LISTED` | 1 | not a user the User Management screen lists |
| `USERS_GRANT_EDITED` | 1 | hardn's file for the user holds a rule hardn did not write |
| `USERS_NOT_ADMIN` | 1 | a password choice for a user who is not an admin |
| `USERS_GROUP_GRANT` | 1 | the user is an admin through a group's rule, which hardn leaves alone |
| `USERS_GRANT_NOT_HARDNS` | 1 | the user is an admin through a rule hardn did not write |
| `USERS_STILL_UNKNOWN` | 1 | whether the user stays an admin without hardn's grant is not established |
| `USERS_STILL_ADMIN` | 1 | the user stays an admin without hardn's grant |
| `USERS_SESSION_USER` | 1 | the grant is the session user's own way to become root |
| `USERS_LAST_ADMIN` | 1 | the session user is not known, and the user is the last admin |
| `USERS_NO_PASSWORD_TO_ASK` | 1 | the user has no password the elevation tool could ask |
| `USERS_KEY_FILES_NOT_READ` | 1 | the files sshd takes keys from are not read, so no key is planned |
| `USERS_LAST_KEY` | 1 | the key is the session user's last |
| `USERS_KEY_ELSEWHERE` | 1 | the key is also in another file sshd reads |
| `USERS_KEY_THERE` | 1 | the user has the key already |
| `USERS_KEY_GONE` | 1 | the key is no longer in the file hardn writes |
| `USERS_NAME_FORM` | 1 | not a user name hardn creates |
| `USERS_NAME_TAKEN` | 1 | a user or group of that name is there |
| `USERS_HOME_THERE` | 1 | the new user's home is there already |
| `USERS_NEW_PASSWORD` | 1 | a new user has no password to ask |
| `USERS_PASSWORD_BYPASS` | 1 | Alpine: sshd lacks the fix for CVE-2026-123158, so anyone could log in as a new user, whose password field is `*`, with a long password: Create is refused ([control-users.md](../engine/control-users.md)) |
| `USERS_NO_USERADD` | 1 | Alpine: useradd, which Create runs, isn't installed (busybox has adduser alone): Create is refused until `apk add shadow` ([control-users.md](../engine/control-users.md)) |
| `AUTHKEYS_NOT_PLANNED` | 1 | authkeys: sshd does not read .ssh/authorized_keys (`authkeys.Err*`, [authkeys.md](../safety/authkeys.md)); new in `next/s5-control` |
| `AUTHKEYS_STRICT_MODES` | 1 | sshd's StrictModes would ignore the keys |
| `AUTHKEYS_NOT_KEY` | 1 | authkeys: not a key |
| `SUDOERS_INVALID_BEFORE` | 1 | the installed elevation configuration does not check, before anything is written |
| `SUDOERS_INVALID_AFTER` | 1 | the elevation configuration does not check with the plan installed (put back) |
| `SUDOERS_INVALID_RESTORED` | 1 | the elevation configuration does not check after the old files were put back |
| `SUDOERS_INVALID` | 1 | the elevation tool rejects the configuration |
| `SUDOERS_CHECK_FAILED` | 1 | the elevation tool's check could not run |
| `SUDOERS_NO_TOOL` | 1 | no elevation tool is installed |
| `SUDOERS_TOOL_AMBIGUOUS` | 1 | both sudo and doas are installed; name the one to configure |
| `SUDOERS_DOAS_UNSUPPORTED` | 1 | doas drop-ins are established on Alpine only |
| `SUDOERS_USER_NAME` | 1 | a user name hardn does not write into an elevation rule |
| `SUDOERS_VAR_NAME` | 1 | not an environment variable name |
| `SUDOERS_NOT_INCLUDED` | 1 | the elevation tool does not read the drop-in directory |
| `SUDOERS_ENV_NEEDS_GRANT` | 1 | doas keeps variables only on the rule that permits the user |
| `SUDOERS_PERMS` | 1 | a doas file is not root's, or group or others can write it |
| `SUDOERS_CHANGED` | 1 | an elevation file changed since it was read |
| `SUDOERS_NOT_RESTORED` | 1 | the files were not all put back, or do not check |
| `SUDOERS_FILE_EDITED` | 1 | hardn's file holds a rule hardn did not write, so the user's rules are not read back from it (`sudoers.EditedError` names it); new in `next/s9-control` ([sudoers.md](../safety/sudoers.md) §2 item 13) |
| `ENVIRONMENT_NO_USER` | 1 | no user ran hardn through sudo or doas (a root login), so there is no user to keep HARDN_CONFIG for (`environment.ErrNoUser`); new in `next/s9-control` ([control-environment.md](../engine/control-environment.md) §6) |
| `ALPINE_NOT_ALPINE` | 1 | not an Alpine host |
| `ALPINE_UNKNOWN` | 1 | Alpine's updates: a fact the answer depends on is not established |
| `ALPINE_NO_OPENRC` | 1 | OpenRC did not boot the host, so it runs no crond |
| `ALPINE_NO_CROND` | 1 | crond's OpenRC service is not installed |
| `ALPINE_SCRIPT_MISMATCH` | 1 | apk-cron's daily script does not match the package's state |
| `ALPINE_NO_BACKUP` | 1 | removing the legacy script needs a backup run |
| `ALPINE_CHANGED` | 1 | Alpine's update files changed since they were read |
| `ALPINE_OTHER_PACKAGES` | 1 | apk would change packages other than apk-cron |
| `ALPINE_NOT_EFFECTIVE` | 1 | the step ran, but the host does not show its effect |
| `ALPINE_NOT_RESTORED` | 1 | the steps were not all undone |
| `DNS_UNKNOWN` | 1 | DNS: a fact the answer depends on is not established |
| `DNS_UNSUPPORTED` | 1 | not a distribution family hardn knows |
| `DNS_CONTAINER` | 1 | in a container, the container manager provides /etc/resolv.conf |
| `DNS_MANAGED` | 1 | /etc/resolv.conf is managed by something hardn does not configure |
| `DNS_RESOLVED_NOT_RUNNING` | 1 | systemd-resolved is not running |
| `DNS_OVERRIDDEN` | 1 | a later resolved.conf.d drop-in sets DNS= or Domains= |
| `DNS_DROPIN_BLOCKED` | 1 | something other than a regular file is at hardn's drop-in path |
| `DNS_NO_NAMESERVERS` | 1 | invalid DNS configuration: no nameserver |
| `DNS_OPTION_NOT_APPLICABLE` | 1 | invalid DNS configuration: option does not apply to this host |
| `DNS_INVALID` | 1 | invalid DNS configuration |
| `DNS_NO_BACKUP` | 1 | a file the plan writes needs a backup run |
| `DNS_CHANGED` | 1 | the resolver files changed since they were read |
| `DNS_NOT_EFFECTIVE` | 1 | the step ran, but the host does not show its effect |
| `DNS_NOT_RESTORED` | 1 | the steps were not all undone |
| `UNSAFE_DIRECTORY` | 1 | a directory hardn writes in (a run, backup or log directory) that another user can change, or reached through a symlink |
| `BACKUP_SETTINGS_INVALID` | 1 | invalid backup settings |
| `BACKUP_RUN_EXISTS` | 1 | backup run directory already exists |
| `BACKUP_NOT_REGULAR` | 1 | a file to back up is neither a regular file nor a symlink |
| `BACKUP_MANIFEST_INVALID` | 1 | invalid backup manifest |
| `BACKUP_CORRUPT` | 1 | backup copy does not match its manifest |
| `BACKUP_CHANGED` | 1 | a file changed since its backup in a way restore does not undo |
| `BACKUP_PATH_NOT_DIRECTORY` | 1 | a backup path, or a path above it, is a file, not a directory; it was `UNCLASSIFIED_ERROR` until `next/s3-control` |
| `BACKUP_WRITE_FAILED` | 1 | a backup could not be written (its directory, its copy or its manifest), so the change did not go ahead; it was `UNCLASSIFIED_ERROR` for a read-only file system, and the seam's or the standard library's code otherwise (`PERMISSION_DENIED`, `WRITE_NOT_DURABLE`), until `next/s3-control` ([control-backup.md](../engine/control-backup.md)) |
| `DETECT_SIGNALS_DISAGREE` | 1 | detection: two signals for one fact disagree, so it is Unknown |
| `DETECT_UNRECOGNISED` | 1 | detection: a file or a command's output is not in a form hardn knows |
| `DETECT_NO_SIGNAL` | 1 | detection: no signal established the fact |
| `ACCOUNTS_MALFORMED` | 1 | a line of /etc/passwd or /etc/group is not in a form hardn knows ([check-users.md](../engine/check-users.md)) |
| `ACCOUNTS_TCB` | 1 | /etc/tcb exists, whose files getspnam may read before /etc/shadow: an account's lock is not established ([check-users.md](../engine/check-users.md) §7) |
| `SYMLINK_IN_UNSAFE_DIRECTORY` | 1 | refusing a symlink in a directory another user can change |
| `SYMLINK_REFUSED` | 1 | refusing a symlink |
| `HARD_LINK_REFUSED` | 1 | refusing a file with other hard links, in a directory another user can change |
| `WRITE_NOT_DURABLE` | 1 | replaced, but the directory could not be synced |
| `LOCKED` | 1 | locked by another process |
| `COMMAND_NOT_FOUND` | 1 | command not found |
| `UNKNOWN_USER` | 1 | unknown user |
| `UNKNOWN_GROUP` | 1 | unknown group |
| `COMMAND_FAILED` | 1 | a command hardn ran exited with a status other than 0 |
| `PERMISSION_DENIED` | 1 | permission denied |
| `FILE_NOT_FOUND` | 1 | a file or directory does not exist |
| `TIMED_OUT` | 1 | a step ran out of time |
| `CHANGE_UNJOURNALLED` | 1 | a change ran that the journal does not cover |
| `ROLLED_BACK` | 1 | the apply failed and was rolled back (an apply error with no error of its step) |
| `PLAN_REFUSED` | 1 | the plan was refused (a refusal with no code of its own) |
| `UNCLASSIFIED_ERROR` | 1 | an error hardn does not know by name; its message says what it is |

### 2.2 Finding codes

Each primitive's stable kind code, upper case under its prefix (`SSHD_`, `PVE_`, `UFW_`, `SUDOERS_`, `ALPINE_`, `DNS_`), as `pkg/engine/steps` derives it ([steps.md](../engine/steps.md) §2.23), and the configuration's.

| Code | Meaning |
|---|---|
| `SSHD_CLOBBERABLE` | no Include: hardn's block at the top of sshd_config, which cloud-init and Proxmox VE rewrite |
| `SSHD_INCLUDE_NOT_FIRST` | sshd_config's first directive is not the stock Include |
| `SSHD_MATCH_UNTARGETED` | a Match block that could not be evaluated for the connections it may match |
| `SSHD_WARNING` | sshd -t passed, and complained about a line |
| `SSHD_OVERRIDDEN` | a line of another file is read before hardn's and sets the same keyword |
| `SSHD_SHADOWED` | a line of another file sets a keyword hardn sets, read after hardn's: no effect while hardn's is there |
| `SSHD_ACCUMULATES` | another file adds values to a keyword whose values accumulate |
| `SSHD_MATCH_OVERRIDE` | a Match block sets a keyword hardn sets, for the connections it matches |
| `SSHD_SOURCES_DISAGREE` | two sources of an effective value disagree, so it is Unknown |
| `SSHD_EFFECTIVE_UNKNOWN` | an effective value is Unknown |
| `SSH_MIGRATE_LINE_DROPPED` | a legacy line the migration leaves out, with why |
| `PVE_ROOT_KEY_ONLY` | PermitRootLogin set to prohibit-password: root key-only, as Proxmox VE clustering needs |
| `PVE_ROOT_ALLOWED` | root added to AllowUsers, so that it stays reachable by key |
| `PVE_PROXMOX_ROOT_LOGIN` | Proxmox VE's own PermitRootLogin yes in sshd_config, which hardn's drop-in shadows: not drift |
| `PVE_PORT_NOT_22` | sshd is configured for a port other than 22, or not for 22 |
| `PVE_ROOT_UNREACHABLE` | a setting keeps, or may keep, root from logging in by key |
| `PVE_CLUSTER_WIDE_KEYS` | the authorized_keys edit applies to every node of the cluster |
| `PVE_RULE_MAY_BLOCK` | a pve-firewall rule may drop the port before its management rule accepts it |
| `PVE_BLACKLIST` | pve-firewall's blacklist IP set has entries |
| `PVE_FIREWALL_NOT_MODELLED` | proxmox-firewall (nftables) filters the host; its rules are not read |
| `PVE_ROOT_NOT_KEY_ONLY` | PermitRootLogin is not prohibit-password, globally or in a Match block |
| `UFW_SSH_NOT_ALLOWED` | ufw is active and blocks or denies an SSH listener |
| `UFW_SSH_RESTRICTED` | ufw accepts an SSH listener for some sources, addresses or interfaces only |
| `UFW_UNKNOWN` | ufw's verdict, or its status, could not be read |
| `UFW_SSH_ALLOWED_EXCEPT` | ufw accepts an SSH listener from everywhere but some sources, addresses or interfaces |
| `UFW_SSH_NOT_ALLOWED_AT_START` | ufw is inactive, and its saved rules would not let an SSH listener through once it starts |
| `UFW_SSH_NEW_PORT_WIDER` | the plan lets a new port through from anywhere while a listener is restricted now (P-33) |
| `SUDOERS_LATER_FILE` | a file the tool reads after hardn's, whose matching rule wins |
| `SUDOERS_AFTER_INCLUDE` | the main sudoers file has lines after its includedir |
| `SUDOERS_LEGACY_FILE` | origin/main's /etc/sudoers.d/<user>, reported, not changed |
| `ALPINE_LEGACY_SCRIPT` | origin/main's own apk-upgrade script, which a plan removes once apk-cron is in |
| `ALPINE_FOREIGN_SCRIPT` | something else at the legacy script's path, which hardn leaves alone |
| `DNS_PER_LINK` | systemd-resolved queries per-link servers beside the global ones hardn sets |
| `DNS_DHCP_CLIENT` | a DHCP client that may rewrite /etc/resolv.conf is installed |
| `DNS_UDHCPC` | Alpine's udhcpc, the DHCP client in use, rewrites /etc/resolv.conf on each lease |
| `DNS_PROXMOX_NODE` | Proxmox VE's node DNS settings rewrite /etc/resolv.conf |
| `DNS_UDHCPC_UNKNOWN` | whether Alpine's udhcpc is the DHCP client is not established ([dns.md](../safety/dns.md) §3) |
| `CONFIG_DEPRECATED_VALUE` | a configuration value in a form that is going away; it still works (§3.17) |
| `PLATFORM_BEST_EFFORT` | a best-effort release: its vendor still supports it, and hardn changes it but makes no promises for it (P-41); in every plan made on one, as the plan's own finding (no `control` or `change`) |
| `SSH_MATCH_ALLOWS_MORE` | a Match block lets in more than the global value does (root's login, a password), for the connections it matches ([check-ssh.md](../engine/check-ssh.md)) |
| `SSH_PORT_NOT_IN_EFFECT` | a listener sshd's configuration names is not bound: the configuration is not in effect ([check-ssh.md](../engine/check-ssh.md)) |
| `SSH_MATCH_VALUE_UNKNOWN` | a Match block sets a value hardn does not rank, so what it lets in is not established ([check-ssh.md](../engine/check-ssh.md)) |
| `SSH_PORT_22_BOUND` | a socket on port 22 is bound while no configured SSH listener is on 22: sshd not reloaded, or another program ([check-ssh.md](../engine/check-ssh.md)) |
| `SSH_PASSWORD_BYPASS` | Alpine: sshd lacks the fix for CVE-2026-123158 and checks passwords without PAM, so anyone can log in with a long password as an account whose password field is `*` ([check-ssh.md](../engine/check-ssh.md) §11, [alpine.md](../safety/alpine.md)) |
| `SSH_SSHD_UNFIXED` | Alpine: sshd lacks the fix for CVE-2026-123158 and checks passwords without PAM, with no account exposed yet ([check-ssh.md](../engine/check-ssh.md) §11) |
| `SSH_FIX_UNKNOWN` | Alpine: whether the openssh package has the fix for CVE-2026-123158, or which accounts it would expose, is not established ([check-ssh.md](../engine/check-ssh.md) §11) |
| `FIREWALL_SSH_BLOCKED` | an active firewall refuses new connections to an SSH listener, other than one refused on IPv6 alone while IPv4 gets in: lockout risk, scored critical ([check-firewall.md](../engine/check-firewall.md)) |
| `FIREWALL_SSH_UNCHECKED` | the SSH listeners are not established, so whether the firewall lets them through is not either ([check-firewall.md](../engine/check-firewall.md)) |
| `FIREWALL_BOOT_UNCHECKED` | whether an active ufw starts at the next boot is not read, so the row is not Configured ([check-firewall.md](../engine/check-firewall.md)) |
| `FIREWALL_PVE_LOCAL_ONLY` | on Proxmox VE no management IP set is written: pve-firewall's management rule accepts SSH and the web UI from the local network only ([check-firewall.md](../engine/check-firewall.md)) |
| `USERS_NOPASSWD` | a user account (uid 1000 or more) with a login shell becomes root without a password (sudo's NOPASSWD, doas's nopass) ([check-users.md](../engine/check-users.md)) |
| `USERS_RULE_NOT_READ` | an elevation rule or sudo Defaults hardn does not read may decide whether a user account becomes root ([check-users.md](../engine/check-users.md)) |
| `USERS_UID0` | an account other than root has uid 0, so it is root ([check-users.md](../engine/check-users.md)) |
| `APPARMOR_GLOBAL_MODE_UNKNOWN` | AppArmor's global mode is kill or unconfined, which hardn does not rank, so what the profiles enforce is not established ([check-apparmor.md](../engine/check-apparmor.md)) |
| `APPARMOR_BOOT_UNKNOWN` | AppArmor's service is in a state that does not say whether it loads the profiles at the next boot ([check-apparmor.md](../engine/check-apparmor.md)) |
| `UPDATES_APT_VALUE_UNKNOWN` | an APT::Periodic value in a form hardn does not rank, so whether apt's daily job runs is not established ([check-updates.md](../engine/check-updates.md)) |
| `UPDATES_SCHEDULE_UNKNOWN` | whether anything starts the daily update job is not established: an apt timer in a state that does not say or that could not be read, a host systemd did not boot, or an Alpine OpenRC did not boot ([check-updates.md](../engine/check-updates.md)) |
| `UPDATES_PROXMOX_NOT_UPGRADED` | Proxmox VE: no unattended-upgrades origin matches Proxmox VE's repository, so Proxmox VE's own packages are not upgraded ([check-updates.md](../engine/check-updates.md)) |
| `UPDATES_ORIGINS_UNKNOWN` | a distribution hardn does not list, built from Debian or Ubuntu: whether unattended-upgrades' origins match its own archives is not established ([check-updates.md](../engine/check-updates.md)) |
| `UPDATES_CHANGED_SINCE_HARDN` | hardn's record of turning updates on names a file the host no longer holds as hardn left it, or a package hardn installed that is gone: "changed since hardn" ([control-updates.md](../engine/control-updates.md)) |
| `REBOOT_REQUIRED` | `/var/run/reboot-required` exists: a package asks for a reboot ([header.md](../engine/header.md)) |
| `REBOOT_NEWER_KERNEL` | a kernel newer than the one running is installed in `/boot` ([header.md](../engine/header.md)) |
| `REBOOT_MODULES_MISSING` | Alpine: the running kernel's own modules are not installed, while another kernel's are ([header.md](../engine/header.md)) |
| `REBOOT_KERNEL_REPLACED` | a kernel Debian built runs, and dpkg has its release's package installed as another version than the build running ([header.md](../engine/header.md)) |
| `UPTIME_NO_AUTO_UPDATES` | the host has been up 30 days or more and does not upgrade its packages by itself (on Proxmox VE, not Proxmox VE's own); the risk level's cross-check, with no `control` ([risk.md](../engine/risk.md)) |
| `USERS_LOCKED` | a user account's password is locked (!) and sshd does not use PAM, so sshd refuses it, keys included: the User row does not count it ([check-users.md](../engine/check-users.md)) |
| `USERS_NO_KEY` | a new user is planned with no key and no password, so it cannot log in ([control-users.md](../engine/control-users.md)) |
| `DETAILS_UNREADABLE` | a section of the System Details report, or a fact of it, was not read: `source` says what; the rest was read (§1.10) |

The save's finding that hardn.yml's comments would be lost (CONFIG_COMMENTS_LOST, added by `next/s2-control`) is gone: the save now edits the settings that changed and keeps every comment but those of a list item it replaces or removes, which its diff shows, and the maintainer dropped the warning (2026-10-22). It was never merged or released, so no released code went ([config-edit.md](../engine/config-edit.md)).

### 2.3 `hardn rollback --boot`'s codes

What the boot unit's command did (`revert.BootAction`) and why (`revert.BootReason`), the stable values of [revert-boot.md](../safety/revert-boot.md) §5, so that no consumer reads its words. Not in a document yet: `--boot` writes none until its wiring (`next/p3-startup`).

| Code | Meaning |
|---|---|
| `BOOT_NOTHING` | nothing to do |
| `BOOT_REARMED` | the revert timer runs again, for what remains of the window |
| `BOOT_ROLLED_BACK` | the change was rolled back at once |
| `BOOT_LEFT` | a change or a rollback left to the person: exit 1 |
| `BOOT_NO_CHANGE` | nothing is journalled |
| `BOOT_NO_TIMER` | a change with no timer, left to the next run's notice |
| `BOOT_TIMER_RUNS` | its timer was started in this boot, and is there |
| `BOOT_ROLLBACK_RUNS` | a rollback of it runs |
| `BOOT_CONFIRMED_MEANWHILE` | confirmed after the journal was read |
| `BOOT_CLAIMED_MEANWHILE` | a confirmation or a rollback claimed it after the journal was read |
| `BOOT_APPLY_RUNS` | another hardn's apply holds the run lock, the change not applied yet |
| `BOOT_IN_WINDOW` | re-armed for what remains of the window |
| `BOOT_CLOCK_UNTRUSTED` | re-armed for the whole window: the clock reads before the change |
| `BOOT_DEADLINE_PASSED` | rolled back: past its deadline |
| `BOOT_APPLY_INTERRUPTED` | rolled back: its apply did not finish |
| `BOOT_REARM_FAILED` | rolled back: its timer could not be re-armed |
| `BOOT_JOURNAL_UNREADABLE` | left: the journal cannot be read |
| `BOOT_ROLLBACK_UNFINISHED` | left: a rollback of it did not finish |
| `BOOT_NO_BINARY_RECORDED` | left: the journal does not name the binary its timer runs |
| `BOOT_OTHER_BINARY` | left: its timer runs another path |
| `BOOT_OTHER_CONTENT` | left: the binary at the path is not the one that wrote the journal |
| `BOOT_BINARY_UNREADABLE` | left: this hardn's binary cannot be read |
| `BOOT_NO_BOOT_ID` | left: no boot id recorded, or this boot's cannot be read |
| `BOOT_TIMER_UNKNOWN` | left: whether its timer of this boot is still there cannot be told |
| `BOOT_RUN_LOCK_HELD` | left: another hardn holds the run lock, the change applied |

## 3. Exit statuses

A public interface (brief §2, decided): `0`, `1`, `2`, `3`, `130`, and no other. One function over a run's results and errors gives it (`output.ExitCode`), the same that writes `result.exitCode`; the first rule that holds wins:

| Status | When |
|---|---|
| `130` | anything in the run was interrupted (`CANCELLED`): Ctrl-C, or `SIGTERM` or a hangup, which hardn treats as Ctrl-C ([engine.md](../engine/engine.md) §2.14), or a prompt aborted (Ctrl-C, the end of the input). An apply interrupted is rolled back first; whether that finished is `result.rollback.finished`, never the outcome: an unfinished one is still `cancelled`, with `result.rollback.command` to finish it |
| `2` | a usage error: bad flags or configuration (an error whose code's *Exit* is 2: `USAGE`, `CONFIRM_TIMEOUT_INVALID`, `CHANGE_ID_INVALID`), and changes that would be applied without a terminal and without an explicit yes (`CONFIRMATION_REQUIRED`, §6; not for a plan with nothing to change, which is 0). Before anything runs, `cmd/hardn` exits 2 for: a command line cobra rejects; `-r` with `-c`, `-d`, `-w`, `-g` or `-e` (`-r` runs alone); a bad `--confirm-timeout` or hardn.yml `confirmTimeout`; `hardn confirm` or `hardn rollback` under `-n`; a change id `hardn rollback` cannot take; `system-details`' refused flags |
| `1` | an error. What this run applied, it rolled back (`result.rollback.finished` says whether that finished; `rollback-incomplete` when it did not, and `result.rollback.command` finishes it). **A refusal is 1 too**: the run did not do what was asked and changed nothing, as `hardn confirm`'s and `hardn rollback`'s refusals already exit 1 ([revert.md](../safety/revert.md) §2.21), and as `hardn rollback --boot` does when it leaves a change to the person (`BOOT_LEFT`: [revert-boot.md](../safety/revert-boot.md) §6 asked for this meaning to be registered or given a code; it is registered here) |
| `3` | a check found a control not as asked (`not-configured`, `partial`, or `unknown`: a state not established is not taken as hardened, brief §3), a plan has changes to make, or the user declined them at the confirmation or quit (`q`) or went back (`0`) before they were applied (`declined`): so a script can tell when a host has drifted from its hardened state |
| `0` | success: nothing to change, or every change applied, a change waiting for confirmation included (it is rolled back at its deadline unless confirmed: `result.confirm`) |

`pkg/ui/prompt`'s `ExitInterrupted` is the same 130 (`TestExitStatuses`).

**Every operation flag** (`-g`, `-c`, `-d`, `-w`, `-r`, `-e`, `setup-sudo-env`) runs on the engine and exits with this function's status, its human report (§7) on stdout. **A dry run** (`-n`, or hardn.yml's `dryRun: true` with no `-n`/`--dry-run` given; `--dry-run=false` turns it off) makes each flag a plan (`mode` `plan`): 3 with changes to make, 0 with none, 1 refused ([dryrun-mode.md](../engine/dryrun-mode.md)). **Several flags in one invocation** run in order, `-c`, `-d`, `-w`, `-g`, then `-p` and `-e`, and hardn's status is the worst of theirs: a 1 or a 2 over a 3, a 3 over a 0 (`runStatus`, `cmd/hardn/environment.go`). Other statuses outside this function: not root, an OS hardn refuses, a hardn.yml that does not load, or `-c` with no username, is 1 before anything runs; `hardn confirm` exits 1 when it does not confirm, and `hardn rollback` 1 when it refuses.

**Signals** (`cmd/hardn/dns.go`, `runControls`; [cmd-engine.md](../engine/cmd-engine.md) §2.5 and §2.6). Ctrl-C, `SIGTERM` or a hangup during a flag's apply cancels it, rolls it back, and ends hardn with 130 once reported. One that reaches the apply or its report without interrupting the apply ends hardn once the flag is reported, the flags after it not run: with the failure's status during the rollback of a failed apply, and with 130 for a change applied, not this function's 0, so that a script never reads 0 for an invocation whose later flags did not run. Before the apply, while it plans, a signal ends hardn by the signal. After a hangup during the apply or its report, the report is not written to a terminal or a pipe, so that a terminal nothing reads any more cannot hang hardn; to a regular file (`hardn -g > report.txt`) it is written. Ctrl-C at a typed-hostname question (`-w`, `-d`, `-r` on a terminal) ends hardn by the signal, its audit record written first; the end of the input there is 130.

## 4. The audit log

- **Path:** `/var/log/hardn/audit.jsonl` (`output.AuditPath`, the path the refactor plan proposes, row `p3-output`). The directory is made `0750` when missing and must be a directory owned by root that neither its group nor others can write (`AUDIT_DIR_UNSAFE` otherwise), checked by its path and again on the directory the append opens, so that one put in its place in between is refused too; the file is created `0600`. A record holds the plan, its diffs included, so both are root's alone. A run ended by Ctrl-C, `SIGTERM` or a hangup still writes its record.
- **Format:** one line per run, appended: `{"time": "<RFC 3339, UTC>", ...the run's document (§1)...}`, the same nine fields as stdout's, so the plan, the results and any rollback (`result.rollback`) are there. Written with one `write(2)` on a descriptor opened with `O_APPEND`, so two runs never overwrite each other's lines. A write cut short (a crash, a full disk) leaves a line that is not valid JSON, and the next record is appended on that same line, so a reader skips any line that does not parse: at most that record and the next are lost. Never rewritten, truncated or rotated by hardn; rotation is the packaging's (`next/p7-install`).
- **The legacy log**, as the brief's [Verify] asked recorded (`output.LegacyLogPath`; [phase0-findings.md](../refactor/phase0-findings.md) §6, **[V]**): `/var/log/hardn.log`, opened before flags were parsed until `next/p3-startup` (V26); since then by the main menu, the operation flags and `setup-sudo-env` once they run, never in a dry run, and by `confirm` and `rollback` (`<id>`, `--abandon`, `--boot`) once their refusals of `-n` and of a user other than root are past (`openStartLog`, `cmd/hardn/main.go`; `revertPreflight`, `cmd/hardn/revert.go`); `O_CREATE|O_APPEND`, `0644`, its directory made `0755`; each line Go's `log.LstdFlags` date and time (`2006/01/02 15:04:05`), then `ERROR: `, `WARNING: `, `INFO: `, `SUCCESS: ` or `INSTALLED: ` and the message; not rotated. The `logFile` key is only read (the Logs screen and `-p`), so the two differ when it is changed. It is unchanged: the audit log is new, beside it. **The run log** (`host.Real`'s record of each command's argv, output and exit status, [system.md](../safety/system.md) §2.6) goes where its caller says: `cmd/hardn`'s confirm and rollback commands send it to the legacy log (`revertLog`), and that stays the run log's path, a decision of `next/p3-output`, unchanged by `next/p3-startup`, which made the log's opening lazy: where the log is not opened (a dry run), the run log is written nowhere.
- **Written by** `cmd/hardn` since `next/p3-startup` (`audit`, `cmd/hardn/output.go`): one record for each run of the engine, after its report, whatever its outcome: each operation flag's (`-g`, `-c`, `-e`, `setup-sudo-env`, `-w`, `-d`, `-r`), a plan alone (`-n`, `mode` `plan`: the one thing a dry run writes, which opens neither the legacy log nor `/etc/hardn/hardn.yml.example` and asks no first-run question), a refusal (`-r`'s by its planner before any plan, a change pending, among them: `outcome` `refused`, its code the refusal's), a confirmation declined, an apply, its rollback, one interrupted (written before hardn ends on the signal); each change a screen plans (DNS, Backup, Firewall, SSH Login, User Management, Environment, Auto Updates, Run All's Apply all), once its flow ends; and `hardn confirm`'s clean-up when it runs. Nothing is written before flags are parsed and an operation runs, so never for `--help`, `--version`, a usage error or the non-root refusal. A record that cannot be written (`AUDIT_DIR_UNSAFE`, a full disk) is a warning on stderr with its code: the run goes on, its exit status its own. **Not recorded yet:** the confirmation itself, `hardn rollback <id>` (the timer's included), `--abandon` and `--boot`, which need a `mode` of their own ([docs/engine/output.md](../engine/output.md) §7); a screen's change after which a hangup ends hardn before its flow is drawn.

## 5. What `cmd/hardn` does with this today

- **hardn.yml's warnings**, on **stderr**, once, as it loads, as one Warning callout: a value in a deprecated form, with the file and line and what to write instead (refactor plan §3.17's bool `permitRootLogin`: `true`, `false`, or YAML 1.1's `on`, `Off`, `Yes`; v0.3.2's `disableRoot`, read as `disableRootSSH`), and each key set to a value nothing reads (such as `logFile` naming another path, `sudoNoPassword: false`, the ufw policies). For a bool `permitRootLogin` it says, after the file's path:

  ```text
  permitRootLogin: false (line 12) is deprecated; write "no", which means the same
  ```

  In the JSON document a deprecated value is a finding, `CONFIG_DEPRECATED_VALUE`; nothing else repeats the warning. The value still works. The main menu does not clear the screen as it opens when hardn.yml warned, so the warning stays above it.
- **Colour:** stdout and stderr are each styled by their own detection (`term.Detect`): a stream that is not a terminal is plain, as with `--plain` or `HARDN_PLAIN`: no animation, ASCII symbols, and no colour. `NO_COLOR` and `TERM=dumb` turn colour off too. `--color=always` colours a stream whatever else holds; `--color=never` (`--no-color`, deprecated) never does. Under the systemd timer `hardn rollback`'s streams are the journal, under the OpenRC sleeper the null device, so its lines there carry no escape codes ([vmtest.md](../safety/vmtest.md) §4.9).
- **The operation flags:** the human report of §7 on stdout and the exit statuses of §3; on a terminal a plan with no lockout risk applies without a question; off one, only with `--yes` (§6).
- **`hardn system-details --output json`** (§1.10) and `-p`'s exit statuses.
- Nothing else: no other `--output`.

## 6. Non-interactive use

The policy (brief Phase 3), as a library function, `output.Confirmation`: **never prompt without a terminal on stdin; apply without asking only on an explicit yes.** Without a terminal, or with JSON output (stdout is a script's), a run without yes shows the plan, applies nothing, and exits 2 (`CONFIRMATION_REQUIRED`, naming `--yes`). It is about confirming changes: a plan with nothing to change asks nothing and exits 0, so a scheduled run on a hardened host is 0. With yes it applies without asking.

How the operation flags use it (`gatePlan`, `cmd/hardn/dns.go`; refactor plan §3.11, adopted 2026-11-04):

- **`-g`, `-c`, `-e`, `setup-sudo-env`, and `-w`'s and `-d`'s plans with no lockout risk:** on a terminal they apply without a question, `--yes` or not. With stdin not a terminal, a plan with changes is applied with `--yes`; without it, shown and not applied (`CONFIRMATION_REQUIRED`, exit 2).
- **`-w`'s and `-d`'s lockout-risk plans:** on a terminal (stdin and stdout), the hostname typed confirms them; off one they are refused (`PLAN_REFUSED`, exit 1), `--yes` or not. **`--yes` never applies a lockout-risk plan off a terminal.** `-e` and `setup-sudo-env` refuse a lockout-risk plan anywhere (`PLAN_REFUSED`, exit 1).
- **`-r`:** on a terminal it asks for the hostname typed for any plan with changes, `--yes` or not. Off one, a plain plan is applied with `--yes`, and without it `CONFIRMATION_REQUIRED`, exit 2; a lockout-risk plan is refused, `PLAN_REFUSED`, exit 1, `--yes` or not (the maintainer, 2026-11-04: "Adopt, sign off", amending [gallery-runall.md](../engine/gallery-runall.md) §5 decision 8 (a)).
- **In a dry run** none of this applies: the plan is shown, nothing is asked, `--yes` plays no part.

One question is open, the maintainer's: *should `--yes` let a lockout-risk plan apply off a terminal, held by the confirm-or-revert timer?* The code that would change is `gatePlan`, whose branch for a plan the flag confirms by the hostname typed returns `confirm.offTerminal` off a terminal; it would return nil for a lockout-risk plan with `--yes`, and the countdown would have to be said after the report, since `output.AfterFlow` is drawn on `runTyped`'s path alone today.

## 7. The human report

The same run, for a person, through `pkg/ui`'s components only (design-system README §9.15): a check's status rows; a plan's changes with "No actual changes were made to your system."; an apply's steps, then "Operation Successful", the confirm countdown with when the change is rolled back ("at about …" when the deadline is approximate) and how to undo it now, or the error report naming what was rolled back (the change that failed too, when it had undone its own steps), how to finish a rollback that did not finish, and what undoes a change the journal did not cover; each finding as a warning with its code; each error with its code, a refusal by its why, and its fix as "To go on: …"; a step's failure that its change kind words (`steps.Failure`, DNS's reload without effect) by those words alone, its code on the step's line, and their next step. After the interactive flow, which draws the plan, the steps and "Operation Successful" itself, the countdown follows it. The flow lists the plan's findings before its confirmation, one line each, without their codes (since `next/s2-flow-review`); after it come only those it did not list, a refused plan's, as warnings with their codes. A finding two changes carry is said once; the document keeps one per change. Its words are provisional, as the Phase 2 pages' are; they are not part of this contract. As JSON, none of it is written to stdout.
