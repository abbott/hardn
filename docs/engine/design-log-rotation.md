# Rotating hardn's own logs: design

Status: **adopted 2026-11-14**. The maintainer adopted every recommended answer of §3, in these words: "Adopt all". So each decision's (a) stands, and §3 is kept as the record.

## 1. What is there

hardn writes two files, and nothing rotates either.

| File | Holds | Mode | Written |
|---|---|---|---|
| `/var/log/hardn.log` | every line hardn says, and each command it runs with its output | 0644 | opened by a command that writes it (`cmd/hardn/console.go`, `openLog`), appended |
| `/var/log/hardn/audit.jsonl` | one JSON record per run: who, what, the plan | 0600, in a 0750 directory | `output.Append`, one line per run |

Read on the test VMs, 2026-11-14:

| Host | `hardn.log` | `audit.jsonl` | logrotate |
|---|---|---|---|
| deb13-vm | 45.8 MB, 449,696 lines | 38 kB, 23 records | not installed |
| ubuntu26-vm | 37.7 MB | 24 kB | installed, its timer enabled |
| alpine323-vm | 25.2 MB | 53 kB | not installed |
| centos9-test-119, oracle10-test-118 | none yet | none yet | installed, its timer enabled |

So `hardn.log` is the one that grows, about 100 bytes a line and 1.6 kB a record for the other. And the host's own rotation is not there to lean on: Debian 13 and Alpine come without logrotate. These VMs have had weeks of test runs; a host hardened once writes far less.

The Logs screen and `hardn -p` read `/var/log/hardn.log` alone (`pkg/engine/logview`).

## 2. Two ways

- **A: hardn rotates its own.** When a command opens the log and the file is over a size, it is renamed to `hardn.log.1` first (the older copies moved up, the oldest deleted). No package, the same on every release.
- **B: a logrotate rule.** hardn writes `/etc/logrotate.d/hardn`. It does nothing on Debian 13 or Alpine until logrotate is installed, and it is one more file under `/etc` for hardn to own and undo.

## 3. Decisions for the maintainer

1. **The way.** (a) *Recommended:* A, hardn rotates its own. (b) B, a logrotate rule: the host's usual tool, but absent on two of the supported families. (c) Both. Gates the rest.
2. **Which files.** (a) *Recommended:* both, each by its own size. (b) `hardn.log` alone: the audit log would take decades to matter, but "never" is the gap this item names.
3. **Size and copies.** (a) *Recommended:* `hardn.log` at 10 MiB, four older copies kept, so 50 MiB at most; `audit.jsonl` at 5 MiB (about 3,000 runs), nine kept. Fixed, no hardn.yml key. (b) The same, with keys in hardn.yml to change them. (c) Other numbers.
4. **Older copies compressed.** (a) *Recommended:* no. 50 MiB is the ceiling, and a plain file can be read with `less`. (b) gzip: about a tenth the size, more code, and a copy can't be read as it is.
5. **The Logs screen and `-p`.** (a) *Recommended:* the current file alone, as today, with one line naming the older copy where there is one ("Older: /var/log/hardn.log.1"). (b) Read the older copies too.
6. **When.** (a) *Recommended:* only when a command that writes the log opens it, before its first line. `hardn check`, which promises to write nothing, never rotates. A change waiting on its rollback timer is not affected: a process that has the file open keeps writing to it under its new name. (b) Also after each run.
7. **A copy that can't be made** (a read-only disk, a full one). (a) *Recommended:* say so once as a warning and go on appending to the file as it is: a log problem never stops a change. (b) Refuse to run.

## 4. Not verified

- How much one real hardening run writes to `hardn.log`: the VMs' figures are test runs.
- Whether anything outside hardn reads `hardn.log` by name on a host (log forwarding reads the system's logger, not this file).

## 5. Built

On `next/log-rotation`, 2026-11-14. Every decision's (a).

**What**

- `hardn.log`: over 10 MiB it is renamed to `hardn.log.1` before the command's first line; `.1` to `.4` kept.
- `audit.jsonl`: over 5 MiB it is renamed to `audit.jsonl.1` before the record is appended; `.1` to `.9` kept.
- A file at its size exactly stays. Renames only: a copy keeps its mode and owner; the oldest is replaced by the one moved onto it.
- A log path or a copy that is a symlink, a directory, a FIFO or a device: nothing is renamed, one warning ("<path> was not rotated: <why>"), and the line or record is appended to the file as it is. So is any rename that fails. Where the log can't be opened or the record can't be written either, that warning is the only one.
- The audit log's directory is checked first, by path and on the directory the rotation opens: nothing is renamed in one that is not root's alone.
- The Logs screen and `hardn -p` to a terminal read the current file and add a row, "Older  /var/log/hardn.log.1", where that file is there. `hardn -p` off a terminal prints the current file's lines and nothing else.

**Where**

| | |
|---|---|
| `pkg/host/rotate.go` | `Real.RotateFile(ctx, name, limit, keep, trusted)`: the one function, for both files. New in `host.System`; the fake's is `pkg/host/hostfake/rotate.go` |
| `cmd/hardn/console.go` | `openLog` calls it (`rotateLog`) before it opens `hardn.log` |
| `pkg/output/audit.go` | `Append` calls it after the directory check, before `AppendFile`; the sizes and counts (`LogMaxSize`, `LogKeep`, `AuditMaxSize`, `AuditKeep`); `RotateError` |
| `pkg/engine/logview`, `pkg/ui/screen/logs.go`, `pkg/screens/logs` | `Reading.Older`, the row |
| `internal/tools/uigallery/logs.go` | the host `logs/older` and the form `cli-terminal-older` |

**Choices**

- **Not under hardn's run lock.** That lock is held for a whole apply and is never waited for, and `hardn confirm` and the timer's rollback open the log while another hardn may hold it. The rotation takes `flock(2)` on the log itself, on its own descriptor, and leaves no lock file. Whoever holds it and finds the file already replaced or small rotates nothing and says nothing. A lock still held after 300 ms is the warning ("locked by another process"): any user can lock `hardn.log`, which all can read.
- **A process with the file open** goes on writing to it as `.1`.
- **`AppendFile` tries its open again** when the file is renamed away under it. Seen in the race test; not run on Linux.
- **`hardn.log`'s directory need not be root's alone**: `/var/log` is its group's to write on Ubuntu. No name is followed either way.
- **`hardn check`** reaches neither `openLog` nor `Append`, so it can't rotate; `--accept` writes its record, so it rotates the audit log.

**Tests**

| Test | Holds |
|---|---|
| `pkg/host` `TestRotateFile` | at the limit, one byte over, copies up, the oldest deleted, modes kept, a gap, no file |
| `TestRotateFileRefuses` | a symlink, a directory, a FIFO, at the log and at a copy; a directory others can write; a done context |
| `TestRotateFileLocked` | a lock held on a read-only descriptor: the error; let go within the wait, rotated |
| `TestRotateFileFails` | a rename that fails: the error, the log still there |
| `TestRotateFileOnce`, `TestRotateFileRace` | eight rotators at once rotate once; with writers, no line lost |
| `hostfake` `TestContract` | three cases, on real files and on the fake |
| `pkg/output` `TestAppendRotates`, `TestAppendNotRotated`, `TestAppendRotateSwappedDir` | the audit log's sizes and nine copies; a symlinked copy warns and the record is written; a directory not root's, checked or swapped in, has nothing renamed |
| `cmd/hardn` `TestOpenLogRotates`, `TestOpenLogNotRotated`, `TestAuditNotRotated` | the same through hardn: one warning, the line appended |
| `cmd/hardn` `TestCheckNeverRotates` | `check` and `check --output json` leave both full logs alone; `--accept` rotates the audit log only |
| `logview` `TestReadOlder`, `logs` `TestOlder`, `screen` `TestLogsStates` | the row, and the current file alone read |

**On two hosts** (2026-11-14: centos10-test-120, CentOS Stream 10, and oracle9-test-117, Oracle Linux 9.8; the same on both)

A live command that changes nothing there (`hardn -w`, refused on the family) wrote the logs; each case was set up by hand, and both hosts were left with no hardn file.

| Case | Result |
|---|---|
| `pkg/host`'s rotation and append tests, as root and as a user | pass |
| `hardn.log` at 10 MiB exactly | stays |
| `hardn.log` over it, `audit.jsonl` one byte over 5 MiB | each renamed to `.1`, a new file begun, modes kept |
| four more rotations | `.1` to `.4` hold the newest four; the first is gone |
| a symlink at `hardn.log.2` | "/var/log/hardn.log was not rotated: /var/log/hardn.log.2: refusing a symlink"; nothing renamed, the lines appended |
| two commands at once on a full log | one rotation; both commands' lines in the new file |
| the log locked by another user | "/var/log/hardn.log was not rotated: locked by another process"; the lines appended |
| `hardn check` on full logs | no file changed |

**On three more hosts** (2026-11-14, hardn built from main at 1345ea88: deb13-vm, Debian 13; ubuntu26-vm, Ubuntu 26.04; alpine324-test-116, Alpine 3.24, a container with busybox; the same on all three)

`hardn -w` is not refused there, so the live command was `hardn rollback --abandon 20000101T000000Z-00000000`, with no change pending: it opens the log (`revertPreflight`), refuses ("it is not journalled", exit 1), and writes one line and one audit record. The hosts' own logs were copied aside first and put back after.

| Case | Result |
|---|---|
| the host's own `hardn.log`: 45.9 MB (Debian), 37.7 MB (Ubuntu, by the main menu), 0.8 MB (Alpine) | renamed to `.1` on Debian and Ubuntu, a new file begun; appended to on Alpine |
| no logs | `hardn.log` 0644 and `audit.jsonl` 0600 made |
| `hardn.log` at 10 MiB exactly | stays |
| `hardn.log` over it, `audit.jsonl` one byte over 5 MiB | each renamed to `.1`, a new file begun, modes kept |
| four more rotations | `.1` to `.4` hold the newest four; the first is gone |
| a symlink to `/etc/passwd` at `hardn.log.2` | "/var/log/hardn.log was not rotated: /var/log/hardn.log.2: refusing a symlink"; nothing renamed, the line appended, `/etc/passwd` unchanged |
| two commands at once on a full log | one rotation; both commands' lines in the new file |
| the log locked by `nobody` (`flock`) | "/var/log/hardn.log was not rotated: locked by another process"; the line appended |
| `hardn check` on full logs | no file changed |
| `hardn -p` off a terminal | the current file's lines alone |

`pkg/host`'s tests were not run on these three.

**Not verified**

- A rename failing on a real read-only or full disk; the test injects the failure.
