# The revert timer across a reboot (`hardn rollback --boot`, the boot unit)

> **Unit:** refactor plan Phase 3, `next/p3-revert-boot` (plan §2, Phase 3 row `p3-revert-boot`; [P-34](../refactor/proposals.md#p-34) (c), decided by the maintainer 2026-10-19: re-arm at boot). No V-number: the timer lost at a reboot is [revert.md](revert.md)'s own gap (§2.20, §7). Brief 2.4 and §3 principle 1.
> **Status:** built and tested on macOS against the recording fake (`hostfake`), and through `cmd/hardn`'s seams with no binary run. Nothing has run on Linux: no boot unit has been installed, enabled or started at a boot, and no timer re-armed. Everything host-specific is "written, not run (gate 5)" (§4); the VM steps are [vmtest.md](vmtest.md) §4.10.

The revert timer is runtime state: a transient systemd timer, or an OpenRC sleeper with its pidfile in `/run/hardn`. A reboot inside the confirm window loses it, and the change stays applied and unconfirmed until someone runs hardn. Now, before it arms the first timer on a host, hardn installs a boot unit (`hardn-boot.service` on systemd, the `hardn-boot` init script on OpenRC), shown as a step of the plan. At boot, before sshd reads its files, the unit runs `hardn rollback --boot`. That command re-arms the timer for what remains of the window, or rolls the change back at once when the deadline has passed. It acts only on a journal written by the same hardn binary, in an earlier boot.

## 1. Why

[P-34](../refactor/proposals.md#p-34): a reboot can come from anyone. The user may reboot on purpose inside the window, or a locked-out user may power-cycle the host from the provider's console, or something else may reboot it. Before this unit, the next run of hardn said the change was overdue ("its timer did not run (a reboot stops it)"). A user the change had locked out could not reach that run over SSH, and nothing undid the change at boot. The maintainer chose (c): a deliberate reboot keeps the change open for confirmation, and a power cycle after the deadline recovers the host. The risk P-34 names is a boot-time action that nobody watches. It must never act on a change the user meant to keep (confirmed, also late), nor on a journal from a hardn that has since been replaced.

## 2. Guarantees

### The command (`hardn rollback --boot`, `revert.Boot`)

1. **A flag on the existing subcommand, and no other CLI change.** `--boot` is a bool flag local to `rollback`, with no short form. It takes no `<id>`, because it acts on the change the journal holds, and it does not combine with `--abandon` (both are usage errors from cobra, with nothing run: `TestRevertBootCommand`). Without the flag, `rollback` and `rollback --abandon` are unchanged. As `hardn rollback`, it needs root (otherwise exit 1, with nothing read) and refuses `-n` (exit 2), and it holds off `SIGHUP`, `SIGINT` and `SIGTERM` while it runs ([revert.md](revert.md) §2.17), since it may roll back. It passes `revertPlans`, the registry `hardn rollback` passes ([revert-plans.md](revert-plans.md)). No other flag, key, default or subcommand changes (§3.11 is not approved).
2. **Only this binary's journal, from an earlier boot** (`TestBoot`). Since this unit, `Arm` records three things in the timer: the hardn binary the timer runs (`timer.hardn`, its checked path), the SHA-256 of that file's content (`timer.hardnSHA256`), and the boot's id (`timer.boot`, from `/proc/sys/kernel/random/boot_id`) (§2.11). At boot, `cmd/hardn` passes its own path (`os.Executable`, resolved with `filepath.EvalSymlinks`). `Boot` then acts on a lockout-risk change in `pending.json` only when all of these hold:
   - the timer was not started in this boot. If it was, the timer runs, and there is nothing to do (`BootNothing`). This makes the command idempotent: run again in the same boot, by hand or by systemd, it re-arms nothing twice.
   - `timer.hardn` is this hardn's path, and `timer.hardnSHA256` is the SHA-256 of that file now, read without following a symlink at the path.
   - `timer.boot` is recorded, and this boot's id can be read.

   Anything else is `BootLeft`: reported, not acted on, with the two ways on (`sudo hardn confirm` from a new SSH connection, or `sudo hardn rollback <id>`). That covers a journal written before this unit (it has none of the three fields; [`testdata/pending-p2.json`](../../pkg/host/revert/testdata/pending-p2.json) is Phase 2's golden), another binary's path, the same path with other content (hardn upgraded or replaced since), and a missing boot id. **This is a decision of this unit, as P-34's Risk asks.** A binary that did not write the journal may read it differently, and a boot-time action that nobody watches is the case where that matters most. The cost: a change whose hardn was replaced inside its window gets no boot-time recovery. The next run's overdue notice still says how to undo it ([revert.md](revert.md) §2.20).
3. **Then, by the journal and the clock**:
   - **no `applied`** (a reboot interrupted the apply): rolled back at once. It cannot be confirmed (`CheckConfirm`, [revert.md](revert.md) §2.11), and no check after the apply has passed its files, which sshd is about to read. Keeping it until the deadline would only delay the rollback the timer makes anyway. **A decision of this unit**: P-34 names only "the remaining window" and "the deadline has passed".
   - **a wall clock that reads before the change was created** (no RTC, a clock reset): not trusted. What remains of the window cannot be told, so the timer is re-armed for the whole window (`ClockUntrusted`, said in the log and the command's output). Its deadline is then the re-arm's time plus the window, by that clock.
   - **past the deadline** (`Journal.Deadline`, §2.12): rolled back at once (§2.4).
   - **otherwise**: re-armed for what remains, rounded up to the second, at least 1 s, and **never longer than the window** (`TimeoutSeconds`). The deadline stays the one the change was armed with. A window of 180 s armed at 12:00:00 and rebooted at 12:01:40.5 is re-armed for 80 s (`TestBoot`, "inside the window"). There is no floor of `MinTimeout` here: a re-arm keeps the window the user already had, and does not open a new one.
4. **A rollback at boot starts nothing** (`TestBoot`, `TestBootSocketHost`). It is `revert.Rollback` with `ReasonTimeout`, as the timer's would be, so it leaves `reverted.json` and the next run's notice. It also sets the new `Options.NoStart`: every reload runs as one journalled with `ifUp`, so `Step.Start` is dropped ([revert.md](revert.md) §2.14 step 3). The boot unit runs before `ssh.service` and `ssh.socket` (§2.13), so they are not up yet:
   - **Debian, Proxmox VE (`ssh.service`):** the files are restored, the ufw undo runs, and the gated reload finds the unit inactive and skips it (`reload.StepReport.NotUp`). sshd then starts after the unit and reads the restored files.
   - **Ubuntu (`ssh.socket`):** the files are restored. `systemctl daemon-reload` reruns `sshd-socket-generator` over them (it ran at boot over the change's files), and `systemctl show -p Listen ssh.socket` must show the restored listeners. The restart is gated and skipped. `ssh.socket` then starts after the unit and binds what the restored files say.
   - **Alpine (OpenRC):** `rc-service sshd reload` is gated on `/run/openrc/started/sshd`, which does not exist yet, so it is skipped.

   **Why no start:** a `systemctl start ssh.service` or `systemctl restart ssh.socket` run from a unit ordered before them would queue a job that waits for that same unit to finish, while `systemctl` waits for the job (§3). It would also start sshd early, ahead of its own ordering. If sshd's unit is up after all (the order did not apply, or the command was run by hand later), it is reloaded or restarted as usual, never started (`TestBootSocketHost`, `up`).
5. **The re-arm cannot resurrect a change that was confirmed or rolled back.** The re-armed journal (the new timer: its argv with the remaining seconds, its deadline, this boot's id, the same `hardn` and `hardnSHA256`) is written over `pending.json` before the timer starts, as `Arm` writes it, through `rewritePending`. Then the journal is read again:
   - `rollback.json` holding the change means a rollback claimed it in between: the `pending.json` just written is removed, and the result is `BootNothing`.
   - `confirmed.json` holding it means a `hardn confirm` from the console claimed it: the same, and no timer starts (`TestBootConfirmedMeanwhile`).

   On systemd the timer is `systemd-run` with `--unit=hardn-revert-<id>`, which is free because transient units do not survive a reboot. On OpenRC it is the sleeper, with its pidfile, and its new argv in the journal, so a confirmation's cancel recognises its `/proc/<pid>/cmdline` ([revert.md](revert.md) §2.9; `TestBootOpenRC`). After the re-arm, `hardn confirm` from a new SSH connection confirms the change and stops the new timer (`TestBoot`, `TestBootOpenRC`): the connections the journal saw at `MarkApplied` are gone, so no `ErrOldConnection` applies.
6. **When the timer cannot be re-armed, the change is rolled back at once**, as in item 4. That covers no timer for the init system detected now, hardn failing `CheckExecutable`, a journal that cannot be written, and a timer that does not start (`TestBoot`). A change that no timer would ever roll back is exactly what P-34 is about. The cost is a deliberate reboot whose change is undone. That is safe, and the change can be applied again.
7. **Left to its own path**:
   - a rollback in `rollback.json` that did not finish is `BootLeft`, with the command to finish it (`sudo hardn rollback <id>`) or give it up (`sudo hardn rollback --abandon <id>`, [revert.md](revert.md) §8). At boot its runner is never alive, since `alive` compares the boot id.
   - a rollback whose runner still runs (the command run by hand) is `BootNothing`.
   - a change with no timer (`risk: other`, an apply that did not finish) is `BootNothing`: the next run's notice says how to undo it, as before.

   In none of these cases is anything run or written.
8. **Nothing journalled: nothing to do**, and the boot unit is removed (§2.17). A change confirmed in the window, or late (after a reboot, past its deadline: `Confirm` does not read the clock, [vmtest.md](vmtest.md) §4.9), is in `confirmed.json`, not `pending.json`. The command says which change was confirmed last (`TestBoot`, "confirmed before the reboot", "confirmed late, after a reboot").
9. **Logged.** Every decision goes to `Options.Log` as one `revert: boot: change <id> (<summary>): <action>: <why>` line. The rollback's own lines follow ([revert.md](revert.md) §2.19), and so do the boot unit's install and removal lines. `cmd/hardn` sends both these lines and the run log of every command to `/var/log/hardn.log` and to the console, which under the systemd unit is the unit's journal (`journalctl -u hardn-boot.service`). Under OpenRC the console is the boot console, and the log file is the record.
10. **Exit codes:**
    - 0: nothing to do, re-armed, or rolled back.
    - 1: `BootLeft`, a rollback that did not finish, a journal that cannot be read, or not root. At boot the systemd unit then shows as failed (`systemctl --failed`), and sshd still starts (§2.13).
    - 2: `-n`.

### The journal

11. **Three optional timer fields; the wire format stays compatible.** `timer.hardn`, `timer.hardnSHA256` and `timer.boot` are `omitempty` and keep `schemaVersion` 1. A journal Phase 2 wrote still decodes and passes `Check` (`TestDecodePhase2Journal`). Unknown fields are still refused ([revert.md](revert.md) §2.2), and `TestWireFormat`'s plan files are unchanged. `testdata/pending.json` gained the three fields (`TestArmSystemd -update`, reviewed); `pending-p2.json` keeps the Phase 2 form. **The other direction does not hold:** a hardn from before this unit refuses a journal with these fields as unknown (`ErrJournal`), as it refuses any field it does not know. So a hardn downgraded inside a confirm window can neither confirm nor roll back the change: the timer runs the binary at its path, now the older one. Do not downgrade hardn while a change is journalled; if it happened, put back the binary that armed the change, then confirm it or roll it back. `Check` refuses:
    - `hardn` without `hardnSHA256`, or `hardnSHA256` without `hardn`;
    - a `hardn` that is not clean, or is not in the timer's argv;
    - a `hardnSHA256` that is not 64 lower-case hex digits;
    - a `boot` with whitespace or longer than 64.

    `Arm` hashes the binary after `CheckExecutable` and fails when it cannot read it. A boot id it cannot read is left out and logged, and the boot command then leaves that change alone (§2.2).
12. **`Journal.Deadline` is the timer's deadline** when the journal has one. `Arm` sets it to `created` plus the window, as before, so nothing changes until a re-arm with an untrusted clock moves it (§2.3). Notices, `Confirm`'s messages and the rollback's reason then follow the timer that actually runs. The re-armed timer's rollback is `timeout` once its own deadline has passed, where `created` plus the window would read it as `requested` (no `reverted.json`, [revert.md](revert.md) §4's last item).

### The boot unit (`PlanBootUnit`, `ReadBootUnit`, `EnsureBootUnit`, `RemoveBootUnit`)

13. **systemd: `/etc/systemd/system/hardn-boot.service`**, `0644` root's, pinned byte for byte ([`testdata/hardn-boot.service`](../../pkg/host/revert/testdata/hardn-boot.service), `TestPlanBootUnit`): `Type=oneshot`, `RemainAfterExit=yes`, `ExecStart=<hardn> rollback --boot`, `WantedBy=multi-user.target`. **Its order**, checked line by line in the test:
    - `Before=ssh.service ssh.socket`, so that an immediate rollback restores the files before sshd reads them (item 4). This covers Debian's and Proxmox VE's `ssh.service`, Ubuntu's `ssh.socket`, and the `ssh.service` that socket starts.
    - `DefaultDependencies=no`. The defaults would order the unit `After=basic.target` (systemd.service(5)), and basic.target comes after sockets.target and so after `ssh.socket`. With `Before=ssh.socket` that would be an ordering cycle, which systemd breaks by dropping a job.
    - So the unit names its own order: `After=local-fs.target` (the journal in `/var/lib`, hardn in `/usr/local/bin`), `After=sysinit.target` (the transient timer a re-arm creates requires and follows sysinit.target, systemd.timer(5), so `systemd-run` does not wait on a target that is not yet reached), `After=ufw.service` (the rollback's `ufw delete` finds ufw's rules loaded; an `After=` on a unit that is not installed orders nothing), plus `Conflicts=` and `Before=shutdown.target`, the shutdown ordering the defaults would have given.
    - **Ordering only.** There is no `Requires=`, `Wants=`, `BindsTo=` or `RequiredBy=` on sshd's units (the test refuses them), so sshd starts whatever this unit does, including when it fails.
    - `TimeoutStartSec=6min`. A oneshot's start timeout "is disabled by default" (systemd.service(5)), and sshd waits for this unit, so a bound is needed. It is above `RollbackTimeout` (5 min, hardn's own bound on a rollback), and it is the backstop for a hardn that hangs outside a command.

    `RemainAfterExit=yes` keeps the unit active once it has run, so systemd does not run it again in the same boot. `timer.boot` makes a second run harmless anyway (§2.2).
14. **OpenRC: `/etc/init.d/hardn-boot`**, `0755` root's, in the default runlevel ([`testdata/hardn-boot.openrc`](../../pkg/host/revert/testdata/hardn-boot.openrc)): `need localmount`, `after bootmisc firewall`, `before sshd`, and a `start()` that runs `<hardn> rollback --boot`. Per OpenRC's service-script guide, `after`/`before` set "start order only" and need sets a hard dependency, so sshd starts whatever this service does. sshd is never `need`ed or `use`d (the test refuses both).
15. **Planned and shown, before the first `Arm` on a host** (`PlanBootUnit`, pure; `ReadBootUnit`, read-only). The plan line names what will be written and run, or says that the unit is installed and enabled already. `PlanBootUnit` refuses a hardn path the unit or the script could not name unquoted (only letters, digits and `/._+-`; no space, no systemd `%` specifier, nothing the shell reads: `ErrExecutable`) and an init system that is not running (`ErrNoTimer`, as for the timer). The path and the binary are hardn's (`Options.Hardn`), the same one the timer runs.
16. **Installed idempotently, atomically, checked first** (`EnsureBootUnit`, `TestEnsureBootUnit`):
    - A unit installed as planned (same content, mode `0644`/`0755`, owner root) and enabled (its link is a symlink to it: `multi-user.target.wants/hardn-boot.service`, `/etc/runlevels/default/hardn-boot`) is left as it is. Nothing is written or run (`Already`).
    - A missing or different file is replaced. First a copy is written in the change's run directory under the unit's own name and checked: `systemd-analyze verify --recursive-errors=no <copy>`, which fails on a warning about this unit alone; without the option, verify exits 0 "regardless whether warnings arise" (systemd-analyze(1)). Verify catches, among other things, an `ExecStart=` binary that is not there or not executable. On OpenRC the check is `sh -n <copy>`, the script's syntax. A checker that is not installed (`host.ErrNotFound`) checks nothing: the log says the unit is written unchecked. Any other failure refuses the unit (`ErrBootUnit`), and the copy is removed either way.
    - A file already at the path is backed up into the change's backup run (`backup.Optional`; nothing rolls it back, since the unit outlives the change), then the unit is written with `WriteFileAtomic`, root's, with its mode.
    - Then `systemctl --no-reload enable hardn-boot.service` or `rc-update add hardn-boot default` runs, unless the link is there, and the link must be there afterwards (`ErrBootUnit`). `--no-reload` skips the implicit `daemon-reload` that `enable` would do (systemctl(1)): systemd reads the unit at the next boot, and a `daemon-reload` now would rerun `sshd-socket-generator` on a socket host for nothing.
    - **A failure refuses the change**: nothing is armed (p2drive: "not applied"). A lockout-risk change whose timer a reboot would lose, with nothing to re-arm it, is what P-34 (c) closes.
17. **Removed at a boot that finds nothing journalled** (`RemoveBootUnit`; `TestRemoveBootUnit`, `TestBoot`).
    - **When:** at the end of `hardn rollback --boot`, when nothing is journalled: nothing was, it was confirmed meanwhile, or the boot's own rollback finished. That boot's run is the unit's last. The unit therefore lives from the first `Arm` on a host until the first boot after the change ends, and the next lockout-risk change installs it again (§2.15). Between changes, no hardn runs at boot.
    - **How:** it runs `systemctl --no-reload disable hardn-boot.service` or `rc-update del hardn-boot default` while the link is there, then removes the file, and checks that neither is left.
    - **Never while a change is journalled:** `pending.json` or `rollback.json` there is a `*PendingError` (`Is(ErrPending)`), and nothing changes.
    - **Not removed by `hardn confirm` or `hardn rollback <id>`**, a decision of this unit. Those run while an engine may be about to `Arm` the next change (the run lock is `p3-steps`'), and the boot unit is harmless when nothing is journalled.
    - An uninstall (`p7-install`) calls the same primitive.
18. **`p2drive` installs it with every lockout-risk command** (`port`, `keyonly`, `pve-root`, `ufw-enable`, `migrate --apply`, and `sudoers` when its plan is lockout-risk), shown in the plan and run after the backup run is opened and before `Arm` (`TestPort2230`, `TestPortOpenRC`, `TestBootUnitInstalled`):
    - `--plan-only` and every plan-time refusal install nothing;
    - an installed unit is left as it is;
    - a stale one is backed up into the change's run, unjournalled, and replaced;
    - a check that refuses the copy arms nothing.

    The unit runs the `--hardn` binary, the one the timer runs. `backup.Open` now runs before `Arm`, so an unusable `--backup-path` is refused before anything changes rather than rolled back after the timer.

### Through `host.System`

19. Everything is in `pkg/host/revert`, which depguard's `revert-through-host` and `host-stdlib` cover (no `os`, `syscall`, `net` but `net/netip`, …). There is no new package and no new rule. The hash is `crypto/sha256` over `ReadFileNoFollow`. hardn's own path comes from `cmd/hardn` (`os.Executable`), as for the timer ([revert.md](revert.md) §2.23).

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| Transient units are kept in `/run/systemd/transient`, and the OpenRC sleeper and its pidfile in `/run/hardn`, so a reboot loses the timer | [revert.md](revert.md) §3 (systemd.unit(5)); that `/run` does not survive a reboot is **[unverified]** there | §1, §2.5 (the unit name is free again) |
| `/proc/sys/kernel/random/boot_id` "was generated once" per boot | random(4), as [revert.md](revert.md) §3 | §2.2 |
| Service defaults: "`Requires=` and `After=` on `sysinit.target`", an "`After=` dependency on `basic.target`", and "`Conflicts=` and `Before=` on `shutdown.target`", unless `DefaultDependencies=no` | [systemd.service(5)](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html), Default Dependencies, read 2026-10-19 | §2.13 |
| `TimeoutStartSec=` defaults to the manager's "except when `Type=oneshot` is used, in which case the timeout is disabled by default"; `RemainAfterExit=` keeps a service "active even when all its processes exited" | systemd.service(5), read 2026-10-19 | §2.13 |
| basic.target follows sockets.target, which follows the socket units (`ssh.socket`) | bootup(7) **[not re-read here]**; the cycle it implies is the reason for `DefaultDependencies=no` | §2.13 |
| Timer units get "`Requires=` and `After=` on `sysinit.target`" and "`Before=` on `timers.target`"; only `OnCalendar=` timers also wait for `time-set.target`/`time-sync.target`; `OnActiveSec=` counts "relative to the moment the timer unit itself is activated" | [systemd.timer(5)](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html), read 2026-10-19 | §2.13 (`After=sysinit.target`), §2.5 |
| `enable` reloads the manager configuration "in a way equivalent to `daemon-reload`"; `--no-reload`: "do not implicitly reload daemon configuration after executing the changes" | [systemctl(1)](https://www.freedesktop.org/software/systemd/man/latest/systemctl.html), read 2026-10-19 | §2.16 |
| `systemd-analyze verify` reports `ExecStart=` commands "not found in the system or not executable"; without `--recursive-errors`, "zero is returned as the exit status regardless whether warnings arise"; with `no`, "a non-zero process exit status when warnings arise during verification of only the specified unit"; "Added in version 250" | [systemd-analyze(1)](https://www.freedesktop.org/software/systemd/man/latest/systemd-analyze.html), read 2026-10-19 | §2.16 |
| systemd on Debian 13, Ubuntu 26.04 and Proxmox VE 9 is 250 or later, and ships `systemd-analyze` | **[unverified]**; without it the unit is written unchecked, and the log says so (§2.16) | §2.16 |
| A `systemctl start` (or `restart`) run from a unit ordered `Before=` the unit it starts waits for a job that waits for the first unit: a deadlock until a timeout | **[I]**, from the job ordering systemd.unit(5) describes; avoided rather than relied on (§2.4) | §2.4 |
| `systemctl is-active` prints `inactive` for a unit whose start job is queued behind the boot unit | **[I]**; an `activating` there fails the reload as not established (`reload.ErrUnitState`), leaving the rollback incomplete with the files already restored | §2.4 |
| `daemon-reload` reruns `sshd-socket-generator`, and `ssh.socket` then binds what `systemctl show -p Listen` says | [vmtest.md](vmtest.md) §4.9 (Ubuntu 26.04 run, settled) | §2.4 |
| A `daemon-reload` and a `systemd-run` from a unit early in the boot work like later ones | **[unverified]** | §2.4, §2.5 |
| `ufw.service` is `DefaultDependencies=no`, `After=local-fs.target`, `Before=network-pre.target` on Debian and Ubuntu, so `After=ufw.service` adds no cycle | **[unverified]** (recalled, not read here) | §2.13 |
| OpenRC: `need` is "a hard dependency"; `after` declares that "we need to be started after another service, without creating a dependency", and `before` is its reverse; `after` "has no effect on services in different stacked runlevels" (sshd and this service are both in `default`) | [OpenRC service-script guide](https://github.com/OpenRC/openrc/blob/master/service-script-guide.md), read 2026-10-19 | §2.14 |
| OpenRC marks a service started once `start()` returns, and handles a started service whose script has since been removed at shutdown without harm | **[unverified]** | §2.17 (the self-removal at boot) |
| The timer fires within a second or two of its deadline | [vmtest.md](vmtest.md) §4.9 (Debian 13, Ubuntu 26.04 runs) | §2.3 (rounding up adds at most a second) |
| `Confirm` claims the journal without reading the clock, so a late confirmation is accepted | [vmtest.md](vmtest.md) §4.9 (Ubuntu 26.04 run, "Found by the run") | §2.8 |

## 4. Written, not run (gate 5)

Nothing here has run on Linux. On the Debian 13, Ubuntu 26.04 and Alpine 3.24 VMs, [vmtest.md](vmtest.md) §4.10 has the steps. They cover the boot unit installed by `p2drive port`; a reboot inside the window (re-armed, with `systemctl list-timers` showing `hardn-revert-<id>` with the time left, or the sleeper's argv on Alpine, and a confirmation from a new connection after the reboot); a power-off across the deadline (rolled back at boot, before sshd, with nothing started from the unit); and a confirmed change rebooted (nothing happens, and the unit removed). Also:

- the unit's order on each init system: `hardn-boot.service` finishing before `ssh.service`/`ssh.socket` start (`journalctl -b -o short-monotonic`), no ordering cycle reported (`journalctl -b | grep -i cycle`), and `systemctl is-active` reading `inactive` for sshd's units while the boot unit runs (§3);
- `systemd-analyze verify --recursive-errors=no` passing on the stock Debian 13 and Ubuntu 26.04 hosts over the copy in `/run/hardn/<id>` (a pass at gate 5 settles §3's row; a refusal there blocks every lockout-risk change, so it is the first thing to check);
- `systemctl --no-reload enable` making the link without a `daemon-reload`, and the unit running at the next boot;
- `systemd-run` and `daemon-reload` from the early-boot unit (§3);
- OpenRC: the service in the default runlevel before sshd, the sleeper surviving the end of `start()`, and the self-removal at a boot with nothing journalled (no error at the next shutdown, §3);
- the untrusted clock (a VM booted with its clock set before the change) and the replaced binary (another build installed inside the window, then a reboot: left, exit 1, `systemctl --failed` lists the unit): written as steps, by hand;
- `/var/log/hardn.log` and the unit's journal as the record, as root; `main()`'s writes before flag parsing at boot (V26, `p3-startup`).

## 5. API

```go
package revert // added or changed by next/p3-revert-boot

// Timer gains three optional fields, which Arm sets (§2.11):
type Timer struct { /* Kind, Unit, PIDFile, Argv, Deadline as before */
	Hardn       string `json:"hardn,omitempty"`       // the binary the timer runs
	HardnSHA256 string `json:"hardnSHA256,omitempty"` // its content's SHA-256, hex
	Boot        string `json:"boot,omitempty"`        // the boot's id when the timer started
}
func (Journal) Deadline() time.Time // changed: the timer's deadline when there is one (§2.12)

// Options gains NoStart: Rollback starts no unit (every reload as with ifUp).
type Options struct { /* … */ NoStart bool }

type BootAction string // BootNothing "nothing", BootRearmed "rearmed", BootRolledBack "rolled-back", BootLeft "left"
type BootResult struct {
	Action BootAction; Why string; Change Journal
	Timer *Timer; For time.Duration; ClockUntrusted bool // a re-arm
	Rollback *RollbackResult                             // a rollback at once
	Removed bool; RemoveErr error                        // the boot unit, once nothing is journalled
}
func Boot(ctx context.Context, sys host.System, in detect.Init, opts Options) (BootResult, error) // opts.Hardn: this hardn

const BootUnitName = "hardn-boot"
const SystemdUnitDir, SystemdWants, OpenRCInitDir, OpenRCDefault = "/etc/systemd/system", SystemdUnitDir + "/multi-user.target.wants", "/etc/init.d", "/etc/runlevels/default"
var ErrBootUnit error
type BootUnit struct { Init detect.InitKind; Path string; Data []byte; Mode fs.FileMode; Link string; Check, Enable, Disable []string }
func PlanBootUnit(in detect.Init, hardn string) (BootUnit, error) // pure; ErrExecutable, ErrNoTimer
type BootUnitState struct { File, Current, Linked bool }       // Installed() bool
func ReadBootUnit(ctx context.Context, sys host.System, u BootUnit) (BootUnitState, error)
type BootUnitResult struct { Unit BootUnit; Already, Wrote, Enabled bool; Backup *backup.Result }
func EnsureBootUnit(ctx context.Context, sys host.System, u BootUnit, work string, run *backup.Run, opts Options) (BootUnitResult, error)
func RemoveBootUnit(ctx context.Context, sys host.System, u BootUnit, opts Options) (removed bool, err error) // *PendingError while journalled

// cmd/hardn (package main)
// hardn rollback --boot: rollbackBoot (the flag); rollbackArgs (no <id> with --boot, not with --abandon);
// runBoot(): revertPreflight("rollback --boot"), revertHoldSignals, revert.Boot with
// Options{Log: revertLog{}, Now: revertNow, Hardn: revertExecutable(), Plans: revertPlans} and revertInit;
// exit 0 nothing/re-armed/rolled back, 1 left, incomplete, unreadable or not root, 2 -n.
// Seams: revertExecutable (os.Executable, resolved), revertInit (detect.DetectInit).

// internal/tools/p2drive: bootStep, planBoot, sayBoot, ensureBoot; lockout.boot.
```

## 6. Inputs to later units, open items, size

- **`p3-engine` and `p3-steps`.** Before the first `Arm` of a lockout-risk change, plan `PlanBootUnit` over detect's `Init` and `Options.Hardn`, and show it with `ReadBootUnit` in the plan. In the apply, `EnsureBootUnit` runs before `Arm`, with the change's run directory and backup run, and a failure refuses the change, as `p2drive` does (§2.18). The run lock should cover `EnsureBootUnit` with `Arm`, and `hardn rollback --boot`'s removal: a boot command run by hand between another process's `EnsureBootUnit` and its `Arm` would find nothing journalled and remove the unit (at boot, nothing else runs). [README.md](README.md) lists it under `p3-engine`.
- **`p3-output`:** registry codes for `ErrBootUnit` and for each `BootAction`, and the words of the command's lines, which are provisional. The boot-time rollback leaves the timer's notice, "rolled back at …, because it was not confirmed within 180s", also for an apply a reboot interrupted, or a timer that could not be re-armed. The unit's journal has the true reason. A notice of its own is `p3-output`'s to shape.
- **`p7-install`:** an uninstall calls `RemoveBootUnit` (never while a change is journalled).
- **Known limits.**
  - A rollback that both claims and finishes between `Boot`'s read and its write leaves `pending.json` written again, re-armed. The timer then rolls the change back a second time, which is idempotent. This is the same limit as `Record`'s ([revert.md](revert.md) §7).
  - Rebooted repeatedly with an untrusted clock inside each window, the change is re-armed for the whole window each time.
  - After a reboot, an `sshd-session` that happens to get the pid and start time of one the journal saw at `MarkApplied` is refused as an old connection (`ErrOldConnection`). Another new connection confirms.
- **Size:** about 2,250 changed lines against `next/p3-rollback-abandon`, excluding testdata, well over the plan's guideline of about 500.
  - About 930 lines of non-test Go, much of it doc comments:
    - `boot.go`, about 275: the decision, the re-arm and the rollback at boot;
    - `bootunit.go`, about 335, the two unit texts included: the plan, install, check and removal;
    - the timer's fields, its hash and boot id, `Arm` and `Deadline`, about 120;
    - `cmd/hardn`, about 100;
    - `p2drive`, about 95.
  - About 1,080 lines of tests: a row per branch of the decision on the fake, the socket host's order and OpenRC's sleeper, the install, idempotency, check and removal on both init systems, the new `Check` rules and the Phase 2 journal, the command's exit codes, and `p2drive`'s order.
  - About 235 lines of docs.

  The unit could have been split in two, the boot unit's primitives and `p2drive` first and the boot command second. It was kept whole because the install's order and the removal's timing are only meaningful with the command that runs at boot.
