# Confirm-or-revert (`pkg/host/revert`, `hardn confirm`, `hardn rollback <id>`)

> **Unit:** refactor plan Phase 2, `next/p2-revert` (brief 2.4; brief §2 decision "Confirm-or-revert: a 180-second default, confirmed from a new session"; decision §3.14, approved 2026-10-17: `--confirm-timeout` / `confirmTimeout`). V-numbers: the confirm-or-revert half of V6 ("no confirm-or-revert"), assigned to this unit, and the "no rollback anywhere" part of V18 ([side-effects.md](../refactor/phase0/side-effects.md)).
> **Status:** built and tested on macOS against the recording fake (`hostfake`), and on real files inside `t.TempDir()` for the journal. Nothing has run on Linux: the timers, a rollback surviving the SSH session and hardn's exit, the `/proc` walk as root under sudo and doas, and the brief's 2230 scenario are "written, not run (gate 5)" (§4).

A change that could lock the user out (SSH or firewall) is journalled in `/var/lib/hardn/pending.json` before its first step, and a timer is armed that runs `hardn rollback <id>` when the confirm window ends: `systemd-run` on systemd, a `setsid`-detached sleeper on OpenRC. `sudo hardn confirm` from a new SSH connection keeps the change and stops the timer; otherwise the rollback restores the change's backups, runs its inverse steps, reloads the services it changed and leaves a notice that the next run of hardn shows. The Phase 3 engine writes the journal (`Arm`, `Record`); this unit defines it, arms and stops the timers, decides which session may confirm, and rolls back. One change is journalled at a time, so `confirm` needs no token.

## 1. Why

origin/main disables password authentication in the same step that adds keys, with no proof that a key works and nothing that undoes the change (V6), and has no rollback of any kind (V18). A confirmation typed into the session that applied the change proves nothing: existing sessions survive an sshd reload, and UFW normally keeps an established connection working while new ones are blocked (brief 2.4). Only a new connection, made after the change, shows that the user can still get in; when nobody makes one, the change must undo itself. Brief §3 principle 1: never lock the user out.

## 2. Guarantees

### The journal (`/var/lib/hardn`)

1. **What is recorded** (format: [`pkg/host/revert/testdata/pending.json`](../../pkg/host/revert/testdata/pending.json), compared byte for byte by `TestArmSystemd`; after an intended change, `go test ./pkg/host/revert -run TestArmSystemd -update` and review the diff): `schemaVersion` (1), `id`, `created`, `timeoutSeconds`, `risk` (`lockout` or `other`), a one-line `summary`, the `session` that applied the change (§2.10), `confirmVia` (the listeners a confirming connection must reach), the `timer` (kind, unit or pidfile, its argv, `deadline`), and, as the engine's steps complete (`Record`), `backup` (the backup run's directory, `Manifest.Dir`, and the `seq` of each entry the change took, [atomic.md](atomic.md) §7), `undo` (inverse steps, in the order to run them: `run` an argv, `chmod`/`chown` a path back, `rmdir` a directory the change created) and `reload` (services to reload once the files are back: `ssh`, with `reload.Change`'s keywords and the listeners the restored files configure). Arm sets `schemaVersion`, `created` and the timer; `Record` cannot change them, the timeout or the risk.
2. **Checked before it is written and after it is read** (`Journal.Check`, `ErrJournal`): every field, since a rollback runs it as root. A command is a name (resolved from `host.SearchPath`) or a clean absolute path; paths are clean and absolute; `seq` values ascend from 1; a reload of a listener keyword (`Port`, `ListenAddress`, `AddressFamily`) carries `after`; a lockout-risk change has a timer, a window of at least `MinTimeout` and a session established by `ReadSession`; an `other` change has neither. **Unknown JSON fields are refused** (unlike the backup manifest): a field this version does not know may be a step a rollback must run.
3. **Root's alone** (`ErrUnsafe`). `/var/lib/hardn` and every directory above it must be directories, not symlinks, owned by root and writable by neither group nor others; each file in it must be root's and not group- or world-writable, and is read with `ReadFileNoFollow` (a symlink there is refused). Files are written with `WriteFileAtomic`, `0600`; the directory is created `0700` (`TestStateUnsafe`, `TestStateRealFiles`). Otherwise a user who could write the journal would choose what root runs when the timer fires.
4. **One change at a time** (`*PendingError`, `ErrPending`). `Arm` refuses, before writing anything but the directory, while `pending.json` or `rollback.json` exists, and its message says how to go on: `sudo hardn confirm` from a new SSH session, or `sudo hardn rollback <id>`, or, for an unfinished rollback, to finish it. **This holds for every change, not only lockout-risk ones** (a decision of this unit): there is one journal file, and a change that did not finish, of any class, is resolved before the next one starts. `other` changes (DNS, AppArmor, updates) get no timer (brief 2.4); they are journalled so that an apply hardn did not finish can be undone, and `Done` removes the journal once their apply succeeds.
5. **Claimed by rename.** `pending.json` becomes `rollback.json` when a rollback starts and `confirmed.json` when it is confirmed (that file keeps the last confirmed change). `rename(2)` succeeds for one of two processes renaming one file, so a confirmation and the timer's rollback at the same moment never both act: the loser finds nothing pending. After the rename the claimed file's id is checked; another change's journal put there since it was read is put back (`ErrClaim`, `TestClaim`).

### The confirm window (§3.14)

6. **180 s by default, 60 s at the least** (`ResolveTimeout(flag, key)`): the flag when set, else the key, else 180 s, rounded up to a whole second. **The floor is 60 s, and a value below it is refused with `ErrTimeout`, naming the flag or the key, rather than raised without a word.** Why 60: in the window the user opens a terminal, connects (to the new port), unlocks a key and types a sudo password (brief 2.4, which calls `iptables-apply`'s 10 s too short), and the window starts before the change is applied (§2.7), so the apply itself (validation, reload, the listener check, up to 5 s by `reload.DefaultAttempts`) comes out of it; under a minute the revert races a practised user, and a typo such as `18` for `180` would arm a revert nobody can beat. `ParseTimeout` reads whole seconds (`180`) or a Go duration (`3m`). **The flag and the key are not wired yet** (§7): there is no lockout-risk apply to time until `p3-engine`, and this unit changes no CLI surface besides the two subcommands.

### The timer

7. **Armed before the first step** (brief 2.4). `Arm` writes `pending.json`, then starts the timer, so a timer that fires at once finds the journal. When the timer does not start, `pending.json` is removed again and `Arm` returns the error: the change must not be applied (`TestArmTimerFails`). Which timer is `TimerFor`'s, a pure function of detect's `Init`:
   - **systemd** (`Init` Systemd, Running Yes): `systemd-run --unit=hardn-revert-<id> --description=… --on-active=<N>s --timer-property=AccuracySec=1s <hardn> rollback <id>`. The transient service runs "in a clean and detached execution environment, with the service manager as its parent process" (systemd-run(1)), not in the user's session, so it outlives hardn and the SSH session (§4). `AccuracySec=` "Defaults to 1min" (systemd.timer(5)), which would let the timer fire up to a minute late; it is set to 1 s.
   - **OpenRC** (`Init` OpenRC, Running Yes): `/bin/sh -c 'sleep "$1" && "$0" rollback "$2"' <hardn> <N> <id>`, started with the new `host.System.StartDetached` (§5): in a new session (`setsid(2)`: "Initially, the new session has no controlling terminal"), so the `SIGHUP` that the end of the SSH session sends to its terminal's foreground process group (POSIX `_exit`) does not reach it, and with stdin, stdout and stderr on the null device, so it holds none of hardn's pipes and cannot die of `SIGPIPE` once hardn exits ([system.md](system.md) §2.5). Its pid goes to `/run/hardn/revert-<id>.pid` (`0600`). The values are the shell's positional parameters, never part of the program text. The shell does not `exec`, so the pid stays the sleeper's.
   - **Anything else** (OpenRC installed but not running, an unknown init): `ErrNoTimer`, and nothing is written: a lockout-risk change is not applied with no way to undo it (`TestArmNoTimer`).
8. **hardn is named by its checked path** (`CheckExecutable`, `ErrExecutable`). hardn is outside `host.SearchPath` ([system.md](system.md) §2.2), so the timer runs `Options.Hardn`, which the caller takes from `os.Executable` and resolves with `filepath.EvalSymlinks`. It must be absolute and clean, a regular file with an execute bit, and it and every directory above it owned by root, not symlinks, and writable by neither group nor others: whoever can replace the file, or rename a directory on the way, chooses what root runs unattended when the timer fires. Debian's `/usr/local` is `0755 root:root` unless `/etc/staff-group-for-usr-local` exists, in which case it is `2775 root:staff` (Debian Policy §9.1.2); **on such a host hardn in `/usr/local/bin` is refused for the timer**, and the refusal names the directory.
9. **Stopped on confirmation and on a rollback before the deadline.** systemd: `systemctl stop hardn-revert-<id>.timer`. OpenRC: the pid in the pidfile is signalled with `kill -s TERM` only when `/proc/<pid>/cmdline` is exactly the sleeper's argv, so a pid reused since is left alone; then the pidfile is removed (`TestConfirm`, "the sleeper on OpenRC"). **A timer that cannot be stopped is harmless**: the rollback it runs finds nothing pending under its id and ends with nothing done (§2.13); the result says so (`CancelErr`).

### Confirming (`Confirm`, `CheckConfirm`, `ReadSession`)

10. **Which session hardn runs in** (`ReadSession(ctx, sys, env, pid)`). Remote when `SSH_CONNECTION`, `SSH_CLIENT` or `SSH_TTY` is set. sudo's `env_reset` keeps only "TERM, PATH, HOME, MAIL, SHELL, LOGNAME, USER and SUDO_*" (sudoers(5)), so under `sudo hardn` the variable is gone; ReadSession then walks hardn's ancestors in `/proc`: Remote, with the connection, when one's `/proc/<pid>/environ` ("the initial environment that was set when the currently executing program was started", proc_pid_environ(5); root passes its ptrace check) holds `SSH_CONNECTION`; Remote when one's `comm` is `sshd` or `sshd-session` (OpenSSH 9.8 split the server into "a listener binary, sshd(8), and a per-session binary sshd-session"). Local once the walk reaches init with neither. When the walk cannot finish (no `/proc`, an unreadable ancestor, more than 64 of them), the session is Remote with no connection and the error wraps `ErrSessionUnknown`: the local-console exception never applies to a session hardn could not place (`TestReadSession`). **This is a check against mistakes, not a security boundary**: whoever runs hardn as root can go around it.
11. **Who may confirm** (`CheckConfirm`, pure, `TestCheckConfirm`):
    - **The local console exception:** a session that is not over SSH (no `SSH_CONNECTION`, a Proxmox host's console) confirms, with a warning that this shows nothing about SSH (brief 2.4: losing SSH is not a lockout at the console). In this unit that is `hardn confirm` from that session; confirming in the applying session's own countdown is `p3-engine`'s flow (§7).
    - **Over SSH**, the connection must be known (`ErrSessionUnknown` otherwise), and with `confirmVia` it must reach one of those listeners, the ones the change added (`reload.Plan.Adds`; `ErrWrongListener` otherwise): a connection over a held old port proves nothing about the new one, and the clean-up after the confirmation drops the port that worked ([reload.md](reload.md) §6). Such a connection is necessarily new. Without `confirmVia`, it must not be the connection that applied the change (`ErrSameSession`; the same four `SSH_CONNECTION` fields), which must then be known. **A multiplexed session (ssh `ControlMaster`) shares its connection, so it is refused as the same one**: it proves nothing about a new connection.
12. **Confirm** reads the journal, applies `CheckConfirm`, claims the journal (§2.5), stops the timer (§2.9) and returns the change. A rollback that has started is `ErrRollingBack` (too late); nothing pending is a `*NotPendingError`, which, when the timer undid the last change, says so: "was rolled back at …, because it was not confirmed within 180s"; a change that did not finish (`other`) is `ErrNotConfirmable`, naming `hardn rollback <id>`. A refused session's error says how to confirm: from a new SSH connection to port 2230, before the deadline.

### Rolling back (`Rollback`)

13. **Idempotent.** `Rollback(ctx, sys, id, opts)` with nothing journalled under `id` returns `Nothing` and why (confirmed, rolled back already, another change pending), with no error: the timer of a confirmed change ends there. An id that is not one (`../etc`) is `ErrID` (`TestRollbackNothing`).
14. **Claimed, then in order** (`TestRollbackByTheTimer`): the journal is claimed (§2.5), then
    1. the backups are restored: `backup.Load` of the run directory (which checks it is root's), the journal's entries only, `backup.Restore` (last taken first, idempotent, past a failed entry, [atomic.md](atomic.md) §2.16);
    2. the inverse steps run in the journal's order, **stopping at the first that fails**: ufw's rule, since an undo left out never locks anyone out and a later one run out of order may ([ufw.md](ufw.md) §2.14); a path that is gone is "already gone", not an error; `rmdir` removes only a directory;
    3. the services are reloaded: an `ssh` reload is `reload.PlanFor` with `Change.Rollback` set, over detect's SSH facts read now and, for a listener change on a socket host, `ReadSocketDropIns`, then `reload.Execute`;
    4. a timer that has not fired is stopped (a rollback by the timer itself leaves it, and removes the sleeper's pidfile).
15. **A backup that was not restored stops the reloads** (a decision of this unit; `TestRollbackRestoreFails`): the files on disk are then a mix of old and new that no check has passed, and a reload makes sshd act on them, which brief 2.2's "validate before activating" rules out. The inverse steps still run: the firewall's undo does not depend on sshd's files. The rollback is then incomplete (below), and the next `hardn rollback <id>` tries again. The trade-off is plain: while the restore fails, the applied configuration stays active in sshd.
16. **Resumable.** Progress (`restored`, `undoDone`, `reloadDone`, reason, start) is written to `rollback.json` after each part, so a rollback that stops (an error, a crash, the deadline) is taken up by the next `hardn rollback <id>` after what it had done: a failed undo is retried, one that succeeded is not run twice (`TestRollbackResumes`). Until it finishes, `ErrIncomplete` names the command to run, a new change is refused and `confirm` is too late.
17. **Not stopped by Ctrl-C.** `Rollback` runs under `context.WithoutCancel` with its own deadline, `RollbackTimeout` (5 minutes): every `host.System` method refuses a done context ([system.md](system.md) §2.5), and a rollback half done is worse than one finished (`TestRollbackAfterCancel`).
18. **Why it ran** is `Options.Reason` when set (the engine's `failed`, after a step failed with the session open), otherwise `timeout` once the deadline has passed and `requested` before it or for a change without a timer. A rollback by the timer leaves `reverted.json` (id, summary, when, the window, any failure) for the next run.
19. **Logged.** Every decision and step goes to `Options.Log` (`revert: rollback <id>: restore …: rewritten`, `undo 1 of 1: ufw delete allow 2230/tcp`, `reload ssh ([Port PasswordAuthentication])`, `done`), every command to the `host.System` run log. `cmd/hardn` sends both to the legacy log, `/var/log/hardn.log`, and the console: under the systemd timer the console is the journal, under the OpenRC sleeper the null device, so there the file is the record.

### The next-run notice (`Notices`, `Acknowledge`)

20. `Notices(state, now)` is pure and returns one plain-text paragraph per thing to say: a change waiting for confirmation (until when, how to confirm, how to undo it now); one past its deadline that was not rolled back ("its timer did not run (a reboot stops it)", §6, [P-XX-revert](../refactor/proposals.md#p-xx-revert)); a change that did not finish; a rollback that did not finish; and, from `reverted.json`, **"Change … was rolled back at …, because it was not confirmed within 180s."**, with any failure (brief 2.4). `Acknowledge` removes `reverted.json` once a run has shown it.

### The command line (`cmd/hardn/revert.go`)

21. **`hardn confirm`** (no arguments) and **`hardn rollback <id>`** (exactly one) are new subcommands (brief §2 decision); `main.go` is unchanged: `revert.go`'s `init` adds them and the root command's `PersistentPreRun`, the notice. Both need root (exit 1 otherwise, with `sudo hardn …`), and both refuse `-n`, which only previews (exit 2, as `next/p0-hazards` does for V1). `confirm` exits 1 when it does not confirm; `rollback` exits 0 when it rolled back or had nothing to do, 1 when it did not finish, 2 for an id that is not one. Their output goes through the legacy `logging` package, as the rest of `cmd/hardn`'s does.
22. **The notice** runs for every command run as root but `confirm` and `rollback`, which report the journal themselves; not as another user (the journal is root's, and nothing is read). After a subcommand, whose output stays on screen, a "rolled back" notice is acknowledged; after the root command it is not, because the legacy main menu clears the screen and the notice would be lost: it shows again until a subcommand, `confirm` or `rollback` has shown it. `confirm` acknowledges it when it reports the change as rolled back already, `rollback <id>` when it has nothing to do for the change the timer undid (`TestRevertNotices`).

### Through `host.System`

23. Depguard rule `revert-through-host` denies `os`, `io/ioutil`, `golang.org/x/sys`, `syscall`, `net` (except `net/netip`) and `plugin` to the package's non-test files (probed in a scratch copy: an `os`, a `net` and a `syscall` import each fail lint; `net/netip` passes); `host-stdlib` keeps it to the standard library and `pkg/host/...`. hardn's own path, the environment and the pid come from `cmd/hardn`.

## 3. Platform facts relied on

| Fact | Source | Used for |
|---|---|---|
| A transient service from systemd-run runs "in a clean and detached execution environment, with the service manager as its parent process"; a timer option creates a timer unit alongside the service; `--timer-property=` sets a property on that timer | [systemd-run(1)](https://www.freedesktop.org/software/systemd/man/latest/systemd-run.html) | §2.7 |
| The timer and the service both take `--unit`'s name (`hardn-revert-<id>.timer` and `.service`) | **[unverified]**: systemd-run(1)'s example shows a `.timer` and a `.service` of one name, generated; the cancel relies on it, and is harmless if wrong (§2.9) | §2.9 |
| `AccuracySec=` "Defaults to 1min" | [systemd.timer(5)](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html) | §2.7 |
| Transient units are kept in `/run/systemd/transient` (system mode), so a reboot loses them | [systemd.unit(5)](https://www.freedesktop.org/software/systemd/man/latest/systemd.unit.html), load path table ("Dynamic configuration for transient units"); that `/run` does not survive a reboot is **[unverified]** here | §2.20, §6 |
| Stopping a timer unit does not stop the service it already started | **[unverified]** (systemd.timer(5) does not say); a rollback by the timer does not stop it (§2.14 step 4) | §2.14 |
| `setsid()` makes the caller a session and process group leader; "Initially, the new session has no controlling terminal"; `EPERM` for a process group leader | [setsid(2)](https://manpages.debian.org/trixie/manpages-dev/setsid.2.en.html) | §2.7, §5 |
| When a controlling process exits, "the SIGHUP signal shall be sent to each process in the foreground process group" of its controlling terminal | POSIX [`_exit`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/_exit.html) | §2.7 |
| os/exec connects a nil `Stdin`, `Stdout` or `Stderr` to the null device; `SysProcAttr.Setsid` makes the child call setsid | [pkg.go.dev/os/exec](https://pkg.go.dev/os/exec) ("If Stdin is nil, the process reads from the null device"); [pkg.go.dev/syscall#SysProcAttr](https://pkg.go.dev/syscall#SysProcAttr) | §5 |
| A Go program writing to a broken pipe on fd 1 or 2 without `signal.Notify` exits with `SIGPIPE` | [system.md](system.md) §2.5 | §2.7 |
| `SSH_CONNECTION` "Identifies the client and server ends of the connection": client address, client port, server address, server port; `SSH_TTY` names the session's tty | [ssh(1)](https://man.openbsd.org/ssh.1), ENVIRONMENT | §2.10, §2.11 |
| With `env_reset` sudo keeps "TERM, PATH, HOME, MAIL, SHELL, LOGNAME, USER and SUDO_*" and what `env_keep`/`env_check` add: not `SSH_CONNECTION` by default | [sudoers(5)](https://www.sudo.ws/docs/man/sudoers.man/) | §2.10 |
| sudo-rs (Ubuntu 26.04) and doas (Alpine) reset the environment too | **[unverified]**: the `/proc` walk does not depend on it | §2.10 |
| `/proc/<pid>/environ` is the initial environment, NUL-separated, behind a `PTRACE_MODE_READ_FSCREDS` check; `/proc/<pid>/status` has `PPid:`, `/proc/<pid>/comm` the command name, `/proc/<pid>/cmdline` the argv NUL-separated | [proc_pid_environ(5)](https://manpages.debian.org/trixie/manpages/proc_pid_environ.5.en.html); `status`, `comm` and `cmdline` from proc(5) **[not re-read here]** | §2.9, §2.10 |
| OpenSSH 9.8 split the server into "a listener binary, sshd(8), and a per-session binary sshd-session"; so the per-session process is `sshd-session` on Debian 13, Ubuntu 26.04 and Alpine 3.23/3.24 and `sshd` on Debian 12 and Ubuntu 24.04 | [OpenSSH 9.8 release notes](https://www.openssh.org/txt/release-9.8); the version per release is **[unverified]** here | §2.10 |
| Debian's `/usr/local` is `0755 root:root`, or `2775 root:staff` when `/etc/staff-group-for-usr-local` exists | [Debian Policy §9.1.2](https://www.debian.org/doc/debian-policy/ch-opersys.html) | §2.8 |
| `install.sh` installs hardn as `/usr/local/bin/hardn`; hardn is outside `host.SearchPath` | `install.sh:53`; [system.md](system.md) §2.2 | §2.8 |
| `rename(2)` is atomic: of two renames of one file, one succeeds | [atomic.md](atomic.md) §3, rename(2) | §2.5 |
| `kill -s TERM <pid>` is POSIX `kill(1)`; busybox's `kill` takes it | POSIX [kill](https://pubs.opengroup.org/onlinepubs/9799919799/utilities/kill.html); busybox **[unverified]** | §2.9 |
| `/bin/sh` and `sleep` exist on every supported release (busybox ash and sleep on Alpine) | **[unverified]**, as [system.md](system.md) §3's row for the commands hardn runs | §2.7 |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root; no timer has fired. On the Debian 13, Ubuntu 26.04 and Alpine 3.24 VMs (`next/p2-vmtest`) and by hand on Proxmox VE 9:

- **the brief's scenario** (brief Phase 2 acceptance): change the SSH port to 2230; with no confirmation the change is rolled back at the deadline (key login on 22 again, 2230 closed, the ufw rule gone); `sudo hardn confirm` from a new session on 2230 cancels the rollback; from the applying session, or over a held port 22, it is refused; on Proxmox VE (no port change, §3.18) the timer for a non-port change;
- **systemd:** `systemd-run --on-active=180s` creating `hardn-revert-<id>.timer` and `.service` (§3, unverified row), firing within a second of the deadline, and the rollback running after the SSH session that armed it has ended, with logind's `KillUserProcesses` on and off, and after hardn has exited; `systemctl stop hardn-revert-<id>.timer` stopping it; the rollback's own run not stopped by it (§3);
- **OpenRC:** the sleeper surviving the end of the SSH session, hardn's exit, sudo's exit with `use_pty` and doas's; busybox `sh -c`, `sleep` and `kill -s TERM`; `/proc/<pid>/cmdline` of the sleeper equal to its argv, NUL-terminated; whether it stays in sshd's cgroup and is killed by `rc-service sshd stop` with `rc_cgroup_cleanup="YES"` in `/etc/rc.conf` **[unverified]**;
- **sessions:** `ReadSession` under `sudo` (sudo 1.9 on Debian 12 and 13, Ubuntu 24.04), sudo-rs (Ubuntu 26.04), doas (Alpine) and `su -`, over SSH and on a console; the `comm` of the per-session sshd on each release; tmux or screen started on the console and attached over SSH is seen as local (its server's ancestors are not the SSH session) **[I]**;
- the journal under `/var/lib/hardn` and the pidfile under `/run/hardn` as root, with real owners (the real-file test runs unprivileged in `t.TempDir()` with a seam that trusts its own user, §5);
- a restore from a `systemd-run` transient service and from the detached sleeper ([atomic.md](atomic.md) §4), and the ssh reload's plan replayed with `Rollback` on each init mode ([reload.md](reload.md) §4);
- the timer's `hardn rollback <id>` going through `main()`, which writes `/var/log/hardn.log` and `/etc/hardn/hardn.yml.example` before parsing flags (V26, `p3-startup`), and whose legacy console colours reach the systemd journal as escape codes (`initializeColor` turns colour on unless `--no-color`);
- a clock stepped back during the window: a rollback by the timer then reads as `requested` and leaves no `reverted.json`.

## 5. Changes to the seam (`pkg/host`, `hostfake`)

- **`host.System` gains `StartDetached(ctx, name, args...) (pid, error)`**, the start-detached method [system.md](system.md) §7 asked `p2-revert` to design. `Real` resolves the name as `Run` does, with `Run`'s environment and working directory, sets `SysProcAttr.Setsid`, leaves the three streams nil (the null device), uses `exec.Command`, not `CommandContext`, so no context kills it, and reaps it from a goroutine while hardn runs. It is tested through a new `spawn` seam, which starts nothing (`TestRealStartDetached`, `TestRealStartDetachedErrors`). `RunLog.Detached` writes `run <n>: started detached, pid <p>, not waited for` or why it did not start.
- **The fake** answers `StartDetached` from its script and `Handler` as it answers `Run`, starting nothing: `Response.PID` (new) is the pid, or a counter from 1000; `Response.Err` fails it; unscripted is `ErrUnscripted`. Recorded as `start-detached` with `Call.PID` (new); `Runs` leaves it out.
- [system.md](system.md)'s API list predates the method.
- No dependency changes: `go.mod` and `go.sum` are unchanged; the package uses the standard library and `pkg/host/...` only.

## 6. API

```go
package revert

const DefaultDir = "/var/lib/hardn"; const DefaultRunDir = "/run/hardn"
const PendingName, RollbackName, ConfirmedName, RevertedName = "pending.json", "rollback.json", "confirmed.json", "reverted.json"
const SchemaVersion = 1
const DefaultTimeout = 180 * time.Second; const MinTimeout = 60 * time.Second; const RollbackTimeout = 5 * time.Minute
const UnitPrefix = "hardn-revert-"; const SleeperScript = `sleep "$1" && "$0" rollback "$2"`; const ProcDir = "/proc"

var ErrTimeout, ErrID, ErrJournal, ErrUnsafe, ErrPending, ErrNothingPending, ErrNotConfirmable, ErrRollingBack,
	ErrSameSession, ErrSessionUnknown, ErrWrongListener, ErrNoTimer, ErrExecutable, ErrClaim, ErrIncomplete error

type Options struct { Dir, RunDir string; Now func() time.Time; Log io.Writer; Hardn string; Reason Reason
	DetectSSH func(context.Context, host.System) detect.SSH; Reload reload.Options }

func NewID(time.Time) string // backup.NewRunID's form
func ValidID(string) bool
func ResolveTimeout(flag, key time.Duration) (time.Duration, error) // pure
func ParseTimeout(string) (time.Duration, error)                     // pure

type Risk string   // RiskLockout "lockout", RiskOther "other"
type Reason string // ReasonTimeout, ReasonRequested, ReasonFailed
type Listener struct { Addr netip.Addr; Port uint16; RDomain string } // String()
func ListenersOf([]sshd.Listener) []Listener
type Backups struct { Dir string; Seqs []int }
type StepOp string // OpRun "run", OpChmod "chmod", OpChown "chown", OpRemoveDir "rmdir"
type Owner struct { UID, GID int }
type Step struct { Op StepOp; Argv []string; Path string; Mode *backup.Perm; Owner *Owner; Why string }
type ReloadService string // ReloadSSH "ssh"
type Reload struct { Service ReloadService; Keywords []string; After []Listener }
type Progress struct { Started time.Time; Reason Reason; Restored bool; UndoDone, ReloadDone int }
type Journal struct { SchemaVersion int; ID string; Created time.Time; TimeoutSeconds int; Risk Risk; Summary string
	Session Session; ConfirmVia []Listener; Timer *Timer; Backup *Backups; Undo []Step; Reload []Reload; Rollback *Progress }
func (Journal) Timeout() time.Duration; func (Journal) Deadline() time.Time; func (Journal) Check() error
type Outcome struct { SchemaVersion int; ID, Summary string; Created time.Time; TimeoutSeconds int; Reason Reason; Finished time.Time; Errors []string }

type Session struct { Remote bool; Conn, Source string }
type Conn struct { Client, Server netip.AddrPort }
func ParseConn(string) (Conn, error)
func ReadSession(ctx context.Context, sys host.System, env func(string) string, pid int) (Session, error)
func CheckConfirm(j Journal, s Session) (local bool, err error) // pure

type TimerKind string // TimerSystemd "systemd-run", TimerSetsid "setsid"
type Timer struct { Kind TimerKind; Unit, PIDFile string; Argv []string; Deadline time.Time }
func TimerFor(in detect.Init, id, hardn string, timeout time.Duration, runDir string) (Timer, error) // pure
func CheckExecutable(ctx context.Context, sys host.System, path string) error

type State struct { Pending, RollingBack *Journal; Reverted *Outcome } // Busy() bool
type PendingError struct { State State } // Is(ErrPending)
func ReadState(ctx context.Context, sys host.System, opts Options) (State, error)
func Arm(ctx context.Context, sys host.System, j Journal, in detect.Init, opts Options) (Journal, error)
func Record(ctx context.Context, sys host.System, j Journal, opts Options) error
func Done(ctx context.Context, sys host.System, id string, opts Options) error
type ConfirmResult struct { Journal Journal; Local bool; CancelErr error }
type NotPendingError struct { Reverted *Outcome } // Is(ErrNothingPending)
func Confirm(ctx context.Context, sys host.System, s Session, opts Options) (ConfirmResult, error)
type StepResult struct { Step Step; Result host.Result; Note string; Err error }
type RollbackResult struct { Journal Journal; Reason Reason; Nothing bool; Why string; Reverted *Outcome; Resumed bool
	Restored []backup.Restored; Undone []StepResult; Reloads []reload.Report; Skipped string; CancelErr error }
func Rollback(ctx context.Context, sys host.System, id string, opts Options) (RollbackResult, error)
type NoticeKind string // NoticePending, NoticeOverdue, NoticeInterrupted, NoticeUnfinished "rollback-unfinished", NoticeReverted
type Notice struct { Kind NoticeKind; ID, Text string }
func Notices(st State, now time.Time) []Notice // pure
func Acknowledge(ctx context.Context, sys host.System, opts Options) error

package host // added (§5)

// in System:
StartDetached(ctx context.Context, name string, args ...string) (pid int, err error)
func (*RunLog) Detached(id, pid int, err error)

package hostfake // added (§5)

// Response.PID int; Call.PID int; Op "start-detached"
```

## 7. Not in this unit; inputs to later units

- **`p3-engine`, applying a lockout-risk change:** `ResolveTimeout` over the flag and the key; `ReadSession(ctx, sys, os.Getenv, os.Getpid())`; `Arm` with `ID` the backup run's id (`backup.NewRunID`), the summary, the session, `ConfirmVia: revert.ListenersOf(reloadPlan.Adds)` (for a port change; [reload.md](reload.md) §6; for the `listen.conf` migration [sshd-migrate.md](sshd-migrate.md) §6), `Options.Hardn` from `os.Executable` and `filepath.EvalSymlinks`, and detect's `Init`; then, as steps complete, `Record`: the backup run's directory and each entry's `Seq`; ufw's `Plan.PreUndo()` before `Execute` and `Report.Undo` once it returns, as `OpRun` steps, last first ([ufw.md](ufw.md) §6); a `Reload` only once the forward reload's gated step ran (`reload.Report.Steps`); directory modes, owners and created directories as `OpChmod`, `OpChown`, `OpRemoveDir` ([atomic.md](atomic.md) §7). A failed step: `Rollback` with `Options.Reason: ReasonFailed`. A `Record` that returns `ErrRollingBack`: stop, a rollback has the change. While it waits, the original session shows the countdown and the command (brief 2.4; the countdown frame is a Phase 1 gap); on a local console it may confirm there (`Confirm` with its own session). Changes without lockout risk: `Arm` with `RiskOther`, `Done` once applied.
- **`p3-engine`, a run lock:** `Arm`'s "one at a time" reads, then writes; two engines at once would both pass. The engine holds one lock per run. `Record` and a rollback's claim race only when the timer fires during the apply, which the 60 s floor makes implausible.
- **`p3-engine`, after the confirmation:** brief 2.3 step 5 (remove the old port's rule, drop the held `Port` lines; `reload.Change.Confirmed`) is a change of its own. `Confirm` runs no clean-up in this unit, so after a confirmation both ports stay open and allowed: safe, not yet hardened. The engine decides whether `hardn confirm` triggers the clean-up or the next run plans it.
- **The flag and the key (§3.14):** `--confirm-timeout` and `confirmTimeout` are wired with the first lockout-risk apply (`p3-engine` or the SSH slice), through `ParseTimeout` and `ResolveTimeout`; no `pkg/config` key is added here, so a legacy save writes no new field.
- **`p3-output`:** registry codes for the sentinels above; the exit codes (`confirm` 1 when refused, `rollback` 1 when incomplete, 2 for a bad id or `-n`); the words of the notices and messages, which are provisional; the log's path and the audit record of a rollback (today the legacy `/var/log/hardn.log` and the run log).
- **The main-menu slice (Phase 4):** show `Notices` inside the menu (a callout) and `Acknowledge` there; the legacy menu clears the screen after the notice today (§2.22).
- **The slices that move legacy SSH and firewall operations onto the engine:** until then the legacy paths ignore the journal: a legacy `-d` or `-w` during a confirm window is not refused, and a rollback would restore over what it wrote.
- **`next/p2-vmtest`:** the driver passes a hardn binary's path to `Options.Hardn` (not its own); the items in §4.
- **For the maintainer:** a reboot inside the window loses the timer (transient units and the sleeper are runtime state), so the change stays unconfirmed and the next run says it is overdue ([P-XX-revert](../refactor/proposals.md#p-xx-revert)).
- **Size:** about 1,750 lines of non-test Go in `pkg/host/revert` (about 300 of them whole-line comments), 190 in `cmd/hardn/revert.go` and 145 in the seam, and about 1,870 of tests, against the plan's guideline of about 500: the journal and its checks (it is run as root), two timers, the session walk, the confirmation rules, a resumable rollback over three earlier units, the notices, the two subcommands, and a test per failure path.
