# The boot unit before the first Arm, and the journal lock (`pkg/engine`, `pkg/host/revert`)

> **Unit:** refactor plan Phase 3, `next/p3-engine-boot` (plan §2, Phase 3 row `p3-engine-boot`, after `next/p3-join`; [P-34](../refactor/proposals.md#p-34) (c) and [P-35](../refactor/proposals.md#p-35) (a), decided by the maintainer 2026-10-19; brief 2.4 and §3 principle 1). It takes up the last two `p3-engine` items of [docs/safety/README.md](../safety/README.md), which came from stack B (the boot unit before the first `Arm`; a lock for `hardn rollback <id>`, `--abandon` and `--boot`), and [engine.md](engine.md) §7's move of the two signal holds onto `pkg/engine/hold`. No V-number: these are [revert.md](../safety/revert.md)'s and [revert-boot.md](../safety/revert-boot.md)'s own gaps.
> **Status:** built and tested on macOS against the recording fake (`hostfake`): the boot unit's change through the engine, side by side with `p2drive` (`TestEngineParity`, green again); the locks with two hardn processes on one fake host, as goroutines, each stopped where the race happens. Nothing has run on Linux: everything host-specific is "written, not run (gate 5)" (§4).

`next/p3-revert-boot` gave hardn a boot unit that re-arms the revert timer after a reboot, and taught `p2drive` to install it before it arms a lockout-risk change. The engine did not, so at `next/p3-join` `TestEngineParity` failed, and a lockout-risk change applied through the engine armed a timer that a reboot would lose with nothing to re-arm it, the gap P-34 (c) closes. Now the engine plans the boot unit as a change of its own, shows it first, and installs it under its run lock before `revert.Arm`. The same unit adds a short **journal lock** that `Arm`, `Rollback`, `Abandon` and `Boot` hold around their reads and claims of `/var/lib/hardn`, and has `hardn rollback --boot` try the engine's run lock, so that a hand-run boot command can no longer remove the unit between its install and `Arm`. Neither lock can hold up the timer's rollback.

## 1. Why

- **The engine and the boot unit.** revert-boot.md §6 asked the engine to refuse a plan while a change is journalled (before the unit is shown), to plan `PlanBootUnit` with `ReadBootUnit` and show it, and to run `EnsureBootUnit` before `Arm`, refusing the change when it fails, under the run lock. The README lists it under `p3-engine`.
- **The races** the lock closes, each named in a Phase 2 or Phase 3 page:
  - revert.md §8.1 item 8: `Abandon` reads, renames and checks, and a `hardn rollback <id>` that takes the same unfinished rollback up in between either runs beside the record or has its newer record replaced; until now "do not run the two at once";
  - revert.md §2.16: two rollbacks claiming one change in the instant between a claim and its first progress write could both run;
  - revert-boot.md §6: a `hardn rollback --boot` run by hand between another process's `EnsureBootUnit` and its `Arm` finds nothing journalled and removes the unit;
  - revert-boot.md §6's known limit: a rollback that claims and finishes between `Boot`'s read and its write leaves `pending.json` written again, re-armed.
- **Found while building this unit** (no page named them): two `hardn rollback --boot` run by hand at once both re-armed; the second's `systemd-run --unit=hardn-revert-<id>` fails on the unit the first made, and the change is rolled back as a timer that could not be re-armed. Worse, a confirmation, then a new change's `Arm`, both between `Boot`'s read and its write: `Boot` wrote the confirmed change's journal over the new one's, saw the confirmation and removed `pending.json`. The new change's journal was gone, its timer then found nothing to roll back, and the engine's next `Record` failed with nothing journalled to undo what it had done.

## 2. Guarantees

### The boot unit before the first Arm (`pkg/engine`)

1. **A change of its own** (`bootChange`, `BootChangeID` `hardn.boot-unit`). Planned by the engine, not by a control: no `Item` lists it, and `Change.Control` is empty. **Decisions of this unit**, as the plan row asks:
   - **The stage: `StageBoot`**, new, ahead of every other stage, and the only one that runs before `revert.Arm`. The boot unit is the timer's recovery after a reboot, so the timer must not start without it (revert-boot.md §2.16: a failure refuses the change). It is a stage, not a field of its own, so that it stays in `Plan.Changes`. There the plan's view shows it, `Options.Progress` reports it by its index, the plan made again under the run lock is compared with it (§2.4), and `order` puts it first. A control's change in `StageBoot` is `ErrChange` at plan time (`TestBootUnitRefused`), since such a change runs before anything is journalled.
   - **The risk class: `RiskPlain`.** It writes one unit file and one link, with `systemctl --no-reload enable` or `rc-update add`, so nothing is loaded or run until the next boot. At that boot the unit is ordered before sshd but never required by it (revert-boot.md §2.13, §2.14), and it acts only on a journal this binary wrote (revert-boot.md §2.2). Nothing that runs now changes. The plan is a lockout-risk plan anyway: only the risk of its other changes makes the engine plan the unit.
   - **The journal: none, and it is never rolled back.** There is no journal before `Arm` to hold it (`Record` before `Arm` is `ErrNothingPending`). It could be journalled after `Arm`, as an undo step that disables and removes the unit, but it is not, for three reasons:
     - Every rollback, the timer's included, would run one more command that can fail. A rollback that fails is `ErrIncomplete` and blocks every later change (revert.md §2.4), and here it would fail for something harmless.
     - With nothing journalled, the unit removes itself at the next boot (revert-boot.md §2.17), after one run of hardn there that reads a few files. That is what undoes a fresh install.
     - A removal by the rollback would race the next change's install, which is why revert-boot.md §2.17 keeps `hardn confirm` and `hardn rollback <id>` from removing it.

     **A stale or foreign file** the install replaces is backed up into the change's backup run (`backup.Optional` through `Run.Store`, as `EnsureBootUnit` does; with `enableBackups` false it is not, and the plan says so), unjournalled. A rollback does not restore it: it would re-enable a unit that runs another hardn binary, which leaves this hardn's journals alone (revert-boot.md §2.2), or a file that is not hardn's at all.
   - It is not counted in `Plan.Summary` (the journal's summary is the change's). `Result.Applied` lists it, and no `EventRolledBack` is ever sent for it (`TestBootUnitFails`).
2. **Refused while a change is journalled, before it is read or shown.** The plan reads the journal once (`revert.ReadState`). A lockout-risk plan plans the boot unit only when nothing is journalled; otherwise the plan is refused with the `*revert.PendingError` (`refuseApply`, as before), and the unit's files are not read (`TestBootUnitRefused`: no call on its paths).
3. **What the plan shows** (`TestBootUnitPlanned`, `TestBootUnitIdempotent`). `revert.PlanBootUnit` is planned over detect's `Init` and the hardn binary the timer runs (`Options.Revert.Hardn`), and `revert.ReadBootUnit` reads the host:
   - the summary: install, replace, or enable;
   - `Target`: the unit's path;
   - `Diff`: `FileDiff` of the file there against the unit, from `/dev/null` for none;
   - `StateBefore`: "not installed", "installed, not enabled", the mode and owner when only they differ, or a file that is not the unit this hardn installs;
   - `StateAfter`: "installed and enabled";
   - `Commands`: the check over the copy in `/run/hardn/<id>`, and the enable command.

   Refused at plan time: a hardn path the unit cannot name unquoted (`revert.ErrExecutable`, revert-boot.md §2.15), and a unit that cannot be read. An init system with no timer is not refused twice: the revert timer's refusal already says so.
4. **Idempotent, and installed under the run lock before `Arm`.** Nothing is planned once the unit is installed and enabled as planned. Otherwise only what is not as planned is done: the enable alone, or the file alone. `Apply` holds the run lock, plans again, and compares that plan with the one shown, the boot unit's change included: a unit installed, or disabled, in between is `ErrPlanChanged`, and nothing runs (`TestBootUnitPlanChanged`). Then `backup.Open`, `revert.EnsureBootUnit` with the plan's run directory and backup run, then `Arm`. `EnsureBootUnit` refuses too while a change is journalled, a second check. **A failure is the change not applied, with nothing armed**: the error wraps `EnsureBootUnit`'s (`revert.ErrBootUnit` for a refused check or a missing link), and `Options.Progress` reports `EventFailed` at `StepBoot`. What it did before it failed stays: a unit file written and not enabled does not run at boot, and the next apply only enables it (`TestBootUnitFails`).
5. **Parity with `p2drive`** (`TestEngineParity`, [steps.md](steps.md) §2.20). The commands compared now include the install, in p2drive's order:
   - on systemd, `systemd-analyze verify --recursive-errors=no /run/hardn/<id>/hardn-boot.service`, then `systemctl --no-reload enable hardn-boot.service`;
   - on OpenRC, `sh -n /run/hardn/<id>/hardn-boot`, then `rc-update add hardn-boot default`;
   - then the timer.

   This holds on Debian 13, Ubuntu 26.04 and Alpine 3.24, for 2230, `keyonly` and `ufw-enable`. The sudoers grant has no lockout risk, so neither side installs the unit there. `TestEngineCombined` expects the boot unit's change first, since p2drive's fake host has none installed.

### The journal lock (`pkg/host/revert`)

6. **What and where.** `/var/lib/hardn/journal.lock` (`JournalLockName`, in `Options.Dir`), taken with `host.System.Lock`. That is [engine.md](engine.md) §2.8's `flock(2)`: released by the kernel with the open file, so when its holder exits however it exits, and close-on-exec. It lives beside the journal it guards, under the same root-only checks (`stateDir`). With no journal directory nothing is journalled and there is nothing to lock against, so nothing is created (`Arm` creates the directory before it locks).
7. **Who holds it, for what.** Each holder holds it for **file operations alone**, never across a command, a reload, a timer's start or a wait for the user:

   | Holder | Under the lock | Released before |
   |---|---|---|
   | `Arm` | the check that nothing is journalled, and the write of `pending.json` | the timer's start |
   | `Rollback` | its read, the claim (or the take-up of a rollback that stopped), the first progress write naming its process | the restore, the inverse steps, the reloads |
   | `Abandon` | its read, the rename and the check after it (§8.1 item 4) | it returns |
   | `Boot` | its read of the journal; and for a re-arm the read again, the write of the re-armed journal only over the journal it read (`writeRearmed`), and the check of `confirmed.json` after it, with the removal of what it wrote when the change was confirmed | anything is run; the timer's start; any rollback, which takes the lock itself |

   **Not taken by:**
   - `Confirm`: its claim is one `rename(2)`, which settles it against every other claim (revert.md §2.5), and `Boot` reads `confirmed.json` again after its write, before it lets go of the lock;
   - `Record`, `MarkApplied` and `Done`: the engine's writes of its own change, under its run lock, whose check after the write stays (revert.md §2.5);
   - `ReadState` and the notices.
8. **Its wait is bounded, and never holds up a rollback.** A lock held by another process is tried again every 50 ms for up to 10 s, by the real clock (`lockWait`, `lockPoll`; `Options.Now` is the change's clock, which may be wrong). Every holder holds it for a few file operations, milliseconds, so a holder still there after 10 s is stopped (Ctrl-Z, `SIGSTOP`) or stuck in the kernel. `flock(2)` keeps its lock until it is unlocked or closed, so waiting longer would not help. Then:
   - **a rollback goes on without the lock, and logs it** (`TestJournalLockHeld`). The timer's `hardn rollback <id>` is unattended, and a lockout it would undo must not wait on another process (brief §3 principle 1). It then has the limits it had before this unit;
   - `Arm` refuses: nothing is armed (`host.ErrLocked`, naming the holder);
   - `Abandon` refuses: "nothing given up";
   - a re-arm at boot is a timer that cannot be re-armed, so the change is rolled back at once (`BootReasonRearmFailed`, revert-boot.md §2.6's rule), by a `Rollback` that itself goes on;
   - `Boot`'s first read is made without the lock, and logged.

   **A decision of this unit:** fail open for the rollback, closed for the rest.
9. **What it closes.** With the lock taken out (`go test -overlay` over a `lock.go` whose `lockJournal` does nothing), the tests of the first two items and the `Arm` at `Boot`'s write fail; the other `Boot` rows are caught by `writeRearmed`'s comparison alone, which the lock makes atomic with the write:
   - **revert.md §8.1 item 8, in both orders** (`TestJournalLockAbandonAndTakeUp`): with `Abandon` first, the take-up waits for it, then finds the rollback given up and does nothing; with the take-up first, `Abandon` waits for its first progress write, then finds process 7000 rolling it back and gives up nothing (`ErrRollingBack`);
   - **revert.md §2.16's window** (`TestJournalLockTwoRollbacks`): the second rollback waits for the first's claim and first progress write, then finds it running and does nothing;
   - **`Boot` against a confirmation and a new `Arm` at its write** (`TestBootRearmRaces`): the new change's `Arm` waits for `Boot`, which finds the confirmation and removes what it wrote before it lets go; `Arm` then writes its own journal, which nothing writes over. Two hand-run boot commands: the second finds the journal re-armed since its read and leaves it (`BootReasonClaimedMeanwhile`); one timer starts. revert-boot.md §6's known limit (a rollback that claims and finishes between `Boot`'s read and its write) is closed the same way, since that rollback's claim is under the lock.

### The run lock and `hardn rollback --boot`

10. **One run lock** (`revert.RunLockName`, `/run/hardn/engine.lock`; the engine's `LockName` is it). `revert.LockRun` takes it without waiting, naming the holder (`pid <n>, <what>, since <time>`). Its holders:
    - the engine, for a whole apply (its own code, as before);
    - `p2drive`, for a lockout-risk change, from before the boot unit's install to its last write;
    - `hardn rollback --boot` (`Boot`), for its whole run;
    - `RemoveBootUnit`, while it removes the unit.

    The engine's refusal now reads "another hardn is running (… holds the run lock …)", since the holder may be the boot command.
11. **`Boot` tries it once and never waits** (`TestBootRunLock`):
    - **Held by an apply:** the boot unit is kept, logged, with no warning (`RemoveErr` stays nil). This closes revert-boot.md §6's race: an apply between the install and `Arm` holds the run lock.
    - **A journalled change whose timer, started in this boot, is not there** is that apply's change: its timer is about to start, or `Arm` is removing a journal whose timer did not. That is `BootNothing`, with the new reason `BootReasonApplyRuns` (`apply-runs`); before this unit, `Boot` took it for one after a reboot and rolled back a change being applied.
    - **The run lock cannot be taken for another reason:** logged, and the unit is not removed (`RemoveErr`).
    - Otherwise `Boot` holds it to its end, so no apply arms a change, or installs the unit, while `Boot` acts.
12. **`RemoveBootUnit`** takes the run lock itself; while another hardn holds it, the unit is kept, nothing changes, and the error is `host.ErrLocked` (`TestRemoveBootUnitRunLock`; `TestBootUnitUnderRunLock` and p2drive's `TestRunLock` run it from inside the install, and the unit stays).

### No deadlock

13. **Why none can happen.** There are two locks:
    - the run lock is never waited for: `flock`'s `LOCK_NB`, tried once;
    - the journal lock is waited for, up to 10 s, and held only across file operations.

    The only nesting is run lock, then journal lock: an apply's `Arm` and in-process rollback, `Boot`'s reads, re-arm and rollback, p2drive's apply. Nothing that holds the journal lock takes the run lock. So no cycle of waits exists, and a holder that is stopped costs a waiter at most the bound.

    **Within one process:** two `flock`s of one file from separate opens conflict even in one process (flock(2)), so hardn never takes a lock it holds. `Boot` releases the journal lock before any `Rollback` takes it. `Boot`, which holds the run lock, removes the unit without taking it again (`removeUnit`). `TestBootRunLock` checks that a rollback at boot never takes either lock twice, and never finds one held.
14. **Each interleaving, on the fake:**

    | While | Runs | Result | Test |
    |---|---|---|---|
    | an apply holds the run lock, inside a change | the timer's `hardn rollback <id>` (or the user's) | takes the journal lock at once, never the run lock, waits for nothing; the apply then reports the change rolled back by the other (`revert.ErrRollingBack`) | `TestLocksDuringApply`, `TestTimerRollbackOverlaps` (unchanged) |
    | the same | `hardn rollback --abandon <id>` | refused (`not-started`), with no wait | `TestLocksDuringApply` |
    | the same | `hardn rollback --boot`, the timer there / not there yet | nothing (`timer-runs` / `apply-runs`), the unit kept, with no wait | `TestLocksDuringApply` |
    | an apply between the boot unit's install and `Arm` | `RemoveBootUnit`, `hardn rollback --boot` | the unit kept (`host.ErrLocked`); the change then armed with it | `TestBootUnitUnderRunLock`, p2drive's `TestRunLock` |
    | a rollback holds the journal lock for its claim | an apply's `Arm` | waits for it, then arms | `TestArmWaitsForJournalLock` |
    | the boot unit at boot | `Boot`, then its rollback | each lock taken once, released before the next | `TestBootRunLock`, `TestBoot` (unchanged) |
    | a holder stopped inside the lock | rollback / `Abandon` / `Arm` / a re-arm | goes on / refused / refused / rolled back | `TestJournalLockHeld` |
    | `Abandon` and a take-up; two rollbacks; `Boot` and a new `Arm`; two `Boot`s | | as §2.9 | `TestJournalLockAbandonAndTakeUp`, `TestJournalLockTwoRollbacks`, `TestBootRearmRaces` |

### The signal holds

15. **`cmd/hardn`'s `holdSignals` and `p2drive`'s are `pkg/engine/hold`'s** (engine.md §7). `hold.SignalsAs(name, log)` is `Signals` with the name ahead of its lines, so each keeps its words: `cmd/hardn` logs "hardn rollback: SIGTERM received; the rollback finishes first" as a warning, for `rollback <id>` and `--boot` alike, and p2drive logs "p2drive: …" to its output. Both now get the hold's release, which gives `SIGHUP` and `SIGINT` back the disposition they had before the hold. Their own Notify and Stop left both ignored at the system's level (engine.md §2.14), which was harmless only because each exits right after. p2drive's `signals.go` is gone. Tested: hold's `TestSignals` (both names), p2drive's `TestHoldSignals` (production's hold, its line), cmd/hardn's `TestHoldSignals` (unchanged).

### Through `host.System`

16. No new package and no new depguard rule: `lock.go` is in `pkg/host/revert`, under `revert-through-host`; `boot.go` is in `pkg/engine`, under `engine-through-host`; `hold` is unchanged in its rules. `cmd/hardn` and `p2drive` import `pkg/engine/hold`, which no rule denies them. `go.mod` and `go.sum` are unchanged.

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| `flock(2)` locks belong to the open file description, are released by `LOCK_UN` or when every descriptor of it is closed (so at exit), conflict between two separate opens even in one process, and `LOCK_NB` fails with `EWOULDBLOCK` | flock(2), as cited in [engine.md](engine.md) §3 | §2.6, §2.8 (a stopped holder keeps it), §2.13 |
| `host.Real.Lock`: guarded parent, no symlink, `O_NOFOLLOW`, close-on-exec, `LOCK_EX|LOCK_NB`, the holder line written once held, the file emptied on unlock | [engine.md](engine.md) §5 (`pkg/host/lock.go`) | §2.6, §2.10 |
| `flock` works on the file system of `/var/lib` (ext4, xfs) as on `/run`'s tmpfs | **[unverified]**: flock(2) does not limit it to local file systems, but whether a host mounts `/var/lib` over NFS is not hardn's to know; gate 5 runs it on the stock images | §2.6 |
| `rename(2)` is atomic: of two renames of one file one succeeds | [atomic.md](../safety/atomic.md) §3 | §2.7 (`Confirm` needs no lock) |
| The boot unit's install commands, its order before sshd, `--no-reload`, `systemd-analyze verify --recursive-errors=no` | [revert-boot.md](../safety/revert-boot.md) §2.13 to §2.16, §3 | §2.1, §2.5 |
| p2drive's lockout-risk command order (without the boot unit) passed on Debian 13 and Ubuntu 26.04 | [vmtest.md](../safety/vmtest.md) §4.9 (**run**, 2026-10-19) | §2.5: the order the engine keeps |
| The boot unit's install by p2drive on a host | **not run**: [vmtest.md](../safety/vmtest.md) §4.10 is written, not run | §2.5: parity is with a sequence no VM has run |
| A systemd transient unit name is taken until the unit is gone, so a second `systemd-run --unit=hardn-revert-<id>` fails | **[I]** (systemd-run(1) names the unit; revert-boot.md §2.5 relies on the converse) | §1 (two boot commands) |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root. No command drives the engine on a host yet (`p3-output` or slice 6), so the engine's install of the boot unit runs on a VM only once one does. Until then, [vmtest.md](../safety/vmtest.md) §4.10 runs p2drive's install, which §2.5 shows equal on the fake. To run on the Debian 13, Ubuntu 26.04 and Alpine 3.24 VMs:

- **through the engine**, once a command drives it: a lockout-risk change on a host without the unit. The plan shows the unit first; it is installed before `systemd-run` (`journalctl` order); a second change shows nothing for it; a `--plan-only` run while a change is journalled is refused and shows no unit;
- **the journal lock as root**, `/var/lib/hardn/journal.lock`:
  - a holder simulated with util-linux's `flock /var/lib/hardn/journal.lock sleep 30` (on Alpine, `apk add util-linux` **[unverified]**): `sudo hardn rollback --abandon <id>` refused after 10 s naming no hardn holder (the file is empty: the holder line is hardn's own); `sudo hardn rollback <id>` going on after 10 s and logging it; a change's timer firing during the hold, rolled back about 10 s late;
  - two `sudo hardn rollback <id>` started together from two SSH sessions: one rolls back, the other says it is being rolled back;
- **the run lock at boot:** `hardn rollback --boot` creating `/run/hardn` and taking `/run/hardn/engine.lock` from the early-boot unit (on OpenRC from `start()`), and releasing it before sshd starts; by hand during a p2drive change, the unit kept, logged;
- **the holds' release on Linux** for `hardn rollback` and p2drive: [engine.md](engine.md) §4's step, now theirs too.

## 5. API

```go
package engine // added by next/p3-engine-boot

const BootChangeID = "hardn.boot-unit"            // the boot unit's change
const StageBoot Stage = 1                           // new, first; StagePackages … StageVerify are 2 … 7 (words unchanged)
const StepBoot = "boot unit"                        // EventFailed's step when the install fails, before Arm
const LockName = revert.RunLockName                 // was the literal "engine.lock"
// unexported: (*Engine).bootChange, bootState, bootDetail, (*applier).boot;
// refuseApply takes the plan's one ReadState; Plan.Summary leaves StageBoot out.

package revert // added or changed by next/p3-engine-boot

const JournalLockName = "journal.lock" // in Options.Dir
const RunLockName = "engine.lock"      // in Options.RunDir
func LockRun(ctx context.Context, sys host.System, opts Options, what string) (unlock func() error, err error) // host.ErrLocked, naming the holder
const BootReasonApplyRuns BootReason = "apply-runs" // nothing: its timer, of this boot, is not there, and an apply holds the run lock
// Changed: Arm, Rollback, Abandon and Boot take the journal lock (§2.7); Boot and RemoveBootUnit the run lock (§2.11, §2.12).
// unexported: lockJournal, holderLine, lockWait (10 s), lockPoll (50 ms), writeRearmed, sameJournal, removeUnit.

package hold // added
func SignalsAs(name string, log io.Writer) (release func()) // Signals is SignalsAs("hardn", log)

// cmd/hardn: holdSignals is hold.SignalsAs("hardn rollback", revertWarnLog{}).
// internal/tools/p2drive: holdSignals is hold.SignalsAs("p2drive", log); apply holds revert.LockRun.
```

## 6. Changes to other units' code

- **`pkg/host/revert`** (`next/p2-revert`'s, `next/p3-rollback-abandon`'s, `next/p3-revert-boot`'s): `lock.go`; the journal lock in `Arm`, `Rollback`, `Abandon` and `Boot`; the run lock in `Boot` and `RemoveBootUnit`; `BootReasonApplyRuns`. [revert.md](../safety/revert.md) §2, §6 and §8.1 to §8.4, and [revert-boot.md](../safety/revert-boot.md) §2 and §5, predate it; their §7, §8.5 and §6 point here. Every earlier test passes unchanged.
- **`pkg/engine`** (`next/p3-engine`'s, `next/p3-steps`'s): `StageBoot`, `BootChangeID`, `StepBoot`, `bootChange`, `LockName`, the run lock's words. Test hosts install the boot unit (`newHost`), so the earlier tests are as they were; the boot unit's tests start from `newBareHost`. [engine.md](engine.md)'s §2.5, §2.14, §6 and §7 note it.
- **`internal/tools/p2drive`**: the run lock around a lockout-risk change; the hold; `signals.go` removed. The fake's change filter now counts the run root itself (`/run/hardn`, which the lock's directory check makes) as the run directory. `TestEngineCombined` expects the boot unit's change.
- **`cmd/hardn`**: `holdSignals` is the hold's; no CLI surface changes (§3.11 is not approved).

## 7. Inputs to later units, open items, size

- **`p3-output`:** registry codes for `BootReasonApplyRuns`; `host.ErrLocked` from `Arm` and `Abandon` (the journal lock) and from the run lock; `StepBoot`. The words of the boot unit's change (summary, states) and of the locks' refusals are provisional.
- **`p7-install`:** an uninstall calls `RemoveBootUnit`, which now takes the run lock itself, and refuses while an apply holds it.
- **Known limits:**
  - A rollback that went on without the journal lock, past its wait, has the limits it had before this unit (revert.md §2.16, §8.1 item 8); so does a hardn from before this unit, which takes no lock. A downgrade inside a confirm window is already unsupported (revert-boot.md §2.11).
  - `Record` and `MarkApplied` take no lock: revert.md §7's limit (a rollback that claims and finishes between their read and their write) stays, on the engine's own change under its run lock.
  - `Boot`'s run lock refuses an apply started while it runs: by hand, for as long as its rollback takes (up to `RollbackTimeout`).
  - The boot unit is not undone with a change rolled back; it removes itself at the next boot (§2.1). A host never rebooted keeps it, enabled and doing nothing until then.
- **Decisions of this unit**, none the maintainer's: the boot unit's stage, risk and journal treatment (§2.1); a rollback that goes on without the journal lock after 10 s, while `Arm`, `Abandon` and a re-arm do not (§2.8); `apply-runs` (§2.11).
- **Size:** about 2,060 changed lines of Go against `next/p3-join`, and about 205 of docs (this page and 11 lines in five others), over the plan's guideline of about 500. That is about 775 lines of non-test Go (130 in `pkg/engine/boot.go` and 130 in `pkg/host/revert/lock.go`, much of it doc comments) and about 1,290 of tests. The tests are most of it: every interleaving of §2.14 with two processes on one fake, each boot unit state, and each refusal. The commits split it as a review would: the engine's boot unit (which restores parity), the journal lock with the run lock in `revert`, p2drive's run lock, the holds, the docs.
