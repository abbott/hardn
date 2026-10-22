# hardn's output: the JSON document, codes, exit statuses and the audit log

> **What this is:** the public contract of what hardn reports, to a script and to a person (brief §2, "A fixed exit-code set and versioned JSON output", decided; brief Phase 3, the JSON contract, the exit codes and the audit log). Built by refactor plan Phase 3, `next/p3-output`, in `pkg/output`; [docs/engine/output.md](../engine/output.md) says what the package guarantees and how. **Once released, none of it changes but by the rules below.**
> **Status:** built and tested on macOS (goldens of every document below, the human report of each, the exit statuses, the audit log on the recording fake). **Not reachable from the command line yet:** refactor plan §3.11 (`--output json`, `--yes`) is not approved, so no flag selects JSON, and the audit log's first writer comes with `next/p3-startup`. What runs today is the deprecation warning and the colour rule of §5. Nothing has run on Linux (gate 5).

## 1. The JSON document

One run writes **exactly one JSON value on stdout**, then a newline; everything for a person (progress, warnings, notices) goes to stderr, so stdout parses whole (`TestStdoutIsOneDocument`). The same document, with a `time`, is one line of the audit log (§4). Strings are UTF-8; `<`, `>` and `&` are not escaped. **Arrays are never `null`**: an empty one is `[]`. Optional objects (`result.confirm`, `result.rollback`, `result.boot`) are left out when they do not apply.

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
| `rollback` | only when an apply stopped once its change was journalled, whatever the plan's risk: `finished` (the rollback in this run finished and undid every change started), `byAnotherProcess` (another process had taken the change: the timer's rollback, a `hardn rollback <id>`, a rollback given up, a confirmation; this run undid nothing, and its `ROLLBACK_RUNNING` error says which), `step` (`start`, `window`, `backup`, `journal`, `apply`, `journal-after`, `mark-applied`, `done`), `change` (the change that failed, or at `start` and `window` the one that was next and never started; `""` for the apply's last write), `restored` (files put back), `undone` (the inverse steps run, in words), `unjournalled` (§1.4.1; left out when none), `command` (`sudo hardn rollback <id>`, `doas` on a host with doas and no sudo, which finishes a rollback that did not finish; `""` otherwise) |
| `boot` | only when the apply ran the install of hardn's boot unit, before the change was journalled ([engine-boot.md](../engine/engine-boot.md) §2.1): `installed` (false when the install failed, and nothing was armed), `detail`. It is never rolled back; it removes itself at a boot that finds nothing journalled |

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
  "boot": { "installed": true, "detail": "wrote /etc/systemd/system/hardn-boot.service; enabled it" }
}
```

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
| `SSHD_NO_WRITE_STRATEGY` | 1 | no write strategy for this sshd_config |
| `SSH_MIGRATE_FIRST` | 1 | legacy hardn SSH files: migrate them first |
| `SSH_PORT_NO_NEW_LISTENER` | 1 | the port change adds no listener |
| `SSHD_CHANGE_NOT_IN_EFFECT` | 1 | sshd does not show the change |
| `SSHD_FILE_CHANGED` | 1 | a file the SSH change writes is not as the plan read it |
| `PVE_ROOT_LOGIN_NOT_KEPT` | 1 | the cluster's root SSH login by key would not be kept |
| `SSH_CLEANUP_SESSION_NOT_KEPT` | 1 | the clean-up's session is not connected to a port it keeps (`hardn confirm`'s clean-up, P-40) |
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
| `CONFIG_ROOT_LOGIN_NOT_APPLIED` | 2 | permitRootLogin "prohibit-password" is not applied by this build's SSH code, which would write PermitRootLogin no; write "no" or "yes" |
| `CONFIG_NOT_LOADED` | 1 | the configuration was not loaded from a file |
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
| `UFW_ROLLBACK_FAILED` | 1 | an undo of ufw's rules failed; those after it did not run |
| `UFW_NOT_APPLICABLE` | 1 | ufw is not used on Proxmox VE: pve-firewall manages its packet filter |
| `UFW_PROXMOX_UNKNOWN` | 1 | ufw: whether the host runs Proxmox VE (no ufw there) is not established |
| `UFW_NOT_INSTALLED` | 1 | ufw is not installed |
| `UFW_LISTENERS_UNKNOWN` | 1 | the SSH listener set is not established |
| `UFW_STATE_UNKNOWN` | 1 | ufw's state is not established |
| `UFW_WOULD_BLOCK` | 1 | the change would block an SSH listener |
| `UFW_VERIFY_FAILED` | 1 | ufw status does not show the planned state |
| `UFW_ENABLED_MISMATCH` | 1 | ufw's ENABLED setting and its loaded chains disagree |
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
| `DETECT_SIGNALS_DISAGREE` | 1 | detection: two signals for one fact disagree, so it is Unknown |
| `DETECT_UNRECOGNISED` | 1 | detection: a file or a command's output is not in a form hardn knows |
| `DETECT_NO_SIGNAL` | 1 | detection: no signal established the fact |
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
| `DNS_UDHCPC` | Alpine's udhcpc rewrites /etc/resolv.conf on each lease |
| `DNS_PROXMOX_NODE` | Proxmox VE's node DNS settings rewrite /etc/resolv.conf |
| `CONFIG_DEPRECATED_VALUE` | a configuration value in a form that is going away; it still works (§3.17) |
| `PLATFORM_BEST_EFFORT` | a best-effort release: its vendor still supports it and hardn changes it, but hardn is not tested on it (P-41); in every plan made on one, as the plan's own finding (no `control` or `change`) |
| `SSH_MATCH_ALLOWS_MORE` | a Match block lets in more than the global value does (root's login, a password), for the connections it matches ([check-ssh.md](../engine/check-ssh.md)) |
| `SSH_PORT_NOT_IN_EFFECT` | a listener sshd's configuration names is not bound: the configuration is not in effect ([check-ssh.md](../engine/check-ssh.md)) |
| `SSH_MATCH_VALUE_UNKNOWN` | a Match block sets a value hardn does not rank, so what it lets in is not established ([check-ssh.md](../engine/check-ssh.md)) |
| `SSH_PORT_22_BOUND` | a socket on port 22 is bound while no configured SSH listener is on 22: sshd not reloaded, or another program ([check-ssh.md](../engine/check-ssh.md)) |

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
| `2` | a usage error: bad flags or configuration (an error whose code's *Exit* is 2: `USAGE`, `CONFIRM_TIMEOUT_INVALID`, `CHANGE_ID_INVALID`, `CONFIG_ROOT_LOGIN_NOT_APPLIED`), and changes that would be applied without a terminal and without an explicit yes (`CONFIRMATION_REQUIRED`, §6; not for a plan with nothing to change, which is 0) |
| `1` | an error. What this run applied, it rolled back (`result.rollback.finished` says whether that finished; `rollback-incomplete` when it did not, and `result.rollback.command` finishes it). **A refusal is 1 too**: the run did not do what was asked and changed nothing, as `hardn confirm`'s and `hardn rollback`'s refusals already exit 1 ([revert.md](../safety/revert.md) §2.21), and as `hardn rollback --boot` does when it leaves a change to the person (`BOOT_LEFT`: [revert-boot.md](../safety/revert-boot.md) §6 asked for this meaning to be registered or given a code; it is registered here) |
| `3` | a check found a control not as asked (`not-configured`, `partial`, or `unknown`: a state not established is not taken as hardened, brief §3), a plan has changes to make, or the user declined them at the confirmation or quit (`q`) or went back (`0`) before they were applied (`declined`): so a script can tell when a host has drifted from its hardened state |
| `0` | success: nothing to change, or every change applied, a change waiting for confirmation included (it is rolled back at its deadline unless confirmed: `result.confirm`) |

`pkg/cmd`'s `ExitUsage` and `pkg/ui/prompt`'s `ExitInterrupted` are the same 2 and 130 (`TestExitStatuses`). The legacy operation flags keep their own exits until their slices move them onto the engine (§3.11 is not approved).

## 4. The audit log

- **Path:** `/var/log/hardn/audit.jsonl` (`output.AuditPath`, the path the refactor plan proposes, row `p3-output`). The directory is made `0750` when missing and must be a directory owned by root that neither its group nor others can write (`AUDIT_DIR_UNSAFE` otherwise), checked by its path and again on the directory the append opens, so that one put in its place in between is refused too; the file is created `0600`. A record holds the plan, its diffs included, so both are root's alone. A run ended by Ctrl-C, `SIGTERM` or a hangup still writes its record.
- **Format:** one line per run, appended: `{"time": "<RFC 3339, UTC>", ...the run's document (§1)...}`, the same nine fields as stdout's, so the plan, the results and any rollback (`result.rollback`) are there. Written with one `write(2)` on a descriptor opened with `O_APPEND`, so two runs never overwrite each other's lines. A write cut short (a crash, a full disk) leaves a line that is not valid JSON, and the next record is appended on that same line, so a reader skips any line that does not parse: at most that record and the next are lost. Never rewritten, truncated or rotated by hardn; rotation is the packaging's (`next/p7-install`).
- **The legacy log**, as the brief's [Verify] asked recorded (`output.LegacyLogPath`; [phase0-findings.md](../refactor/phase0-findings.md) §6, **[V]**): `/var/log/hardn.log`, opened by `main()` before flags are parsed (V26), `O_CREATE|O_APPEND`, `0644`, its directory made `0755`; each line Go's `log.LstdFlags` date and time (`2006/01/02 15:04:05`), then `ERROR: `, `WARNING: `, `INFO: `, `SUCCESS: ` or `INSTALLED: ` and the message; not rotated. The `logFile` key is only read (the Logs screen and `-p`), so the two differ when it is changed. It is unchanged: the audit log is new, beside it. **The run log** (`host.Real`'s record of each command's argv, output and exit status, [system.md](../safety/system.md) §2.6) goes where its caller says: `cmd/hardn`'s confirm and rollback commands send it to the legacy log (`revertLog`), and that stays the run log's path, a decision of `next/p3-output`, until `next/p3-startup` makes the startup writes lazy.
- **Not written yet:** nothing in `cmd/hardn` appends to it; its first writer is `next/p3-startup`'s wiring.

## 5. What `cmd/hardn` does with this today

- **The deprecation warning** refactor plan §3.17 approved: a configuration whose `permitRootLogin` is a bool (`true`, `false`, or YAML 1.1's `on`, `Off`, `Yes`) is warned about on **stderr**, once, as it loads, with the file and line and what to write instead:

  ```text
    [*] Warning: /etc/hardn/hardn.yml: permitRootLogin: false (line 12) is
        deprecated; write "no", which means the same
  ```

  In the JSON document it is a finding, `CONFIG_DEPRECATED_VALUE`; nothing else repeats the warning. The value still works. **A limit:** `sudo hardn` with no operation flag goes on to the legacy main menu, which clears the screen as it opens, so there the warning is gone at once (left in the terminal's scrollback on some terminals); every other command keeps it on screen. The main menu slice (Phase 4) is to show it.
- **No colour codes** from `hardn confirm`, `hardn rollback <id>`, `--abandon` and `--boot` unless both stdout (where their lines go) and stderr (where the notices go) are colour terminals, by `pkg/ui/term`'s detection (`NO_COLOR`, `TERM=dumb`, `HARDN_PLAIN` and `--no-color` turn it off): under the systemd timer both are the journal, under the OpenRC sleeper the null device. The Debian 13 and Ubuntu 26.04 VM runs found escape codes in the journal ([vmtest.md](../safety/vmtest.md) §4.9).
- Nothing else: no `--output`, `--yes` or `--confirm-timeout` flag (§3.11 is not approved), and the existing flags behave as before.

## 6. Non-interactive use

The policy (brief Phase 3), as a library function, `output.Confirmation`: **never prompt without a terminal on stdin; apply without asking only on an explicit yes.** Without a terminal, or with JSON output (stdout is a script's), a run without yes shows the plan, applies nothing, and exits 2 (`CONFIRMATION_REQUIRED`, naming `--yes`). It is about confirming changes: a plan with nothing to change asks nothing and exits 0, so a scheduled run on a hardened host is 0. With yes it applies without asking; a lockout-risk change is still confirmed afterwards from a new connection, or rolled back at its deadline. **Not wired:** `--yes` is refactor plan §3.11's, not approved, so no operation flag consults it yet ([docs/engine/output.md](../engine/output.md) §7 says where it will be).

## 7. The human report

The same run, for a person, through `pkg/ui`'s components only (design-system README §9.15): a check's status rows; a plan's changes with "No actual changes were made to your system."; an apply's steps, then "Operation Successful", the confirm countdown with when the change is rolled back ("at about …" when the deadline is approximate) and how to undo it now, or the error report naming what was rolled back, how to finish a rollback that did not finish, and what undoes a change the journal did not cover; each finding as a warning with its code; each error with its code, a refusal by its why, and its fix as "To go on: …". After the interactive flow, which draws the plan, the steps and "Operation Successful" itself, the countdown and the findings follow it. Its words are provisional, as the Phase 2 pages' are; they are not part of this contract. As JSON, none of it is written to stdout.
