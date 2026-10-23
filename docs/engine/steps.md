# The engine's change kinds (`pkg/engine/steps`)

> **Unit:** refactor plan Phase 3, `next/p3-steps` (plan §2, row `p3-steps`; brief 2.2, 2.3 and 2.4; brief §2 decisions "Confirm-or-revert" and "hardn's SSH settings live in `00-hardn.conf`"). Decisions it implements: §3.14 (approved 2026-10-17: `--confirm-timeout` / `confirmTimeout`), §3.17 (the legacy SSH files migrated), §3.18 (no SSH port change on Proxmox VE) and [P-39](../refactor/proposals.md#p-39) (a), decided by the maintainer on 2026-10-19: the D9 host stays refused, now with a report. It takes up the `p3-engine` items of [docs/safety/README.md](../safety/README.md) that `next/p3-engine` left to it ([engine.md](engine.md) §7). V-numbers: V7 and V12 (no validation in context, no `ssh.socket` handling) and V8's order, now on the engine's path; the engine half of V24 and R8 (the v0.3.x files migrated with a rollback) ([side-effects.md](../refactor/phase0/side-effects.md)).
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only: the planners on `internal/tools/p2drive`'s fake VM through the engine, side by side with `p2drive` on a twin VM, and the unit changes on `hostfake`. **Nothing has run on Linux** through the engine; everything host-specific is "written, not run (gate 5)" (§4).

`next/p3-engine` built the engine with synthetic changes. This unit gives it the real ones. A control's `Plan` calls a planner here (`PlanSSH`, `PlanMigration`, `PlanUFWEnable`, `PlanCleanup`) or wraps a unit's plan (`Sudoers`, `DNS`, `Alpine`, `PVEKeys`). The planner makes every check a Phase 2 primitive offers before anything changes, and returns `engine.Change`s. A change is data: what it backs up, what the journal holds before it runs, the function that runs it, and what it says the journal needs once it ran. The engine orders the changes (firewall, files, reload, verify), backs them up, journals them write-ahead and rolls them back in reverse. Every refusal is a planner's error, so it shows in the plan, which is the dry run, before the revert timer is armed; so does the engine's refusal of two controls whose planners would act on the same thing (§2.22). No control is built here: which settings a control asks for, and from which configuration keys, is the slices' (slice 6 for SSH).

## 1. Why

`internal/tools/p2drive` composes the Phase 2 primitives by hand for four changes. Its order ran on Debian 13 and Ubuntu 26.04 ([vmtest.md](../safety/vmtest.md) §4.9). The engine applies any plan in that order (engine.md §2.5), but it holds no primitive itself. Without this unit, every slice would have to rebuild the driver's checks and its journal. Each one that got a check wrong would lock a user out, or would leave a change the timer cannot undo (brief §3 principle 1).

## 2. Guarantees

### The SSH change (`PlanSSH`)

1. **One planner for every change of hardn's sshd settings** (`SSH{Prefix, Set, Port, UFW}`). Reads, all before anything changes:
   - detect's facts, from `engine.Input`;
   - Alpine's sshd binary (`reload.OpenRCFlavour`);
   - `sshd -T` over the installed files (`sshd.ReadEffective`);
   - hardn's legacy files (`migrate.Find`);
   - whether Proxmox VE's rules apply (`pve.Applies`).

   Then it refuses:
   - an `sshd_config` whose first directive is not exactly the stock Include, or that has no Include (`ErrNoWriteStrategy`; [P-38](../refactor/proposals.md#p-38); hardn's block at the top of `sshd_config` is not written yet);
   - legacy hardn files on the host (`ErrMigrateFirst`: their migration, §2.6, is a change of its own and comes first, as [sshd-migrate.md](../safety/sshd-migrate.md) §6 requires).

   **Changes, all of lockout risk:** `<prefix>/firewall` (when ufw has something to do), `/files`, `/reload` and `/verify`.

   **None once the host is as asked** (idempotency: `TestEngineIdempotent`, `TestEngineAlreadyAsked`). "As asked" is decided from what is in effect, not from the file's bytes (brief §3 principle 3):
   - **a directive:** `00-hardn.conf` already has its lines, as `setDirectives` writes them or in another layout (a migrated file keeps them under its own label: `saysAlready`; every line of the file is global, so sshd reads the same values), **and** `sshd -T` over the installed files shows them (`inForce`, the same `mismatch` as the check after the reload). A file that says so while `sshd -T` does not, because another drop-in is read first, is refused with `ErrNotInEffect`, naming the line sshd reads first; an empty plan would have reported the host as asked.
   - **a port:** configured (in `sshd -T`'s Port set and on one of its listeners) **and** in effect: the listeners established (`reload.ErrListenersUnknown` otherwise) and `reload.CheckInEffect` passing over them with the applying session's port. A port that is configured and not in effect (an edit not reloaded, a `daemon-reload` without a restart of the socket) is refused, as p2drive refused it, rather than dropped from the change: dropping it took the hold of 22, ufw's allow and the listener checks with it, and the reload then moved sshd to the port on disk.
   - **ufw's enable:** with `UFW: UFWEnable`, ufw's enable is planned also when nothing else is left (the change `PlanUFWEnable` makes), so that one SSH change serves a plan that asks for SSH settings and ufw's enable together (§2.22).

   Tested: `TestEngineAlreadyAsked` (a migrated file's layout, a port configured and in effect), `TestSaysAlready`, `TestEngineRefusedBeforeArm` (another drop-in read first; a port on disk sshd or `ssh.socket` has not read), `TestEngineCombined`.
2. **`00-hardn.conf` is edited line by line, never regenerated** ([sshd-migrate.md](../safety/sshd-migrate.md) §6; `setDirectives`, p2drive's edit, [vmtest.md](../safety/vmtest.md) §2.4):
   - the lines of the keywords the change sets are dropped;
   - the new lines come first, under migrate's own label `# Set by hardn:`, so that they are global;
   - every other line stays, the migrated `AuthorizedKeysFile` union (R8) included;
   - a new file starts with migrate's header, `# Managed by hardn.`;
   - a `Match` or `Include` line, and an argument that would need quoting, are refused (`fs.ErrInvalid`).

   **A port change** writes `reload.HoldPorts` of the listeners sshd configures now and the new port: `Port 22` and `Port 2230` during the confirm window.

   **On Proxmox VE** every write goes through `pve.PlanSSH` over the whole file it leaves, the kept lines too (`pveSet`). A migrated `AllowUsers alice` becomes `AllowUsers alice root`.

   Tested: `TestSetDirectives`, `TestEnginePVERoot`.
3. **Validated in context before the rename.** The candidate is checked with `sshd.Validate` over a copy of the whole configuration in the plan's run directory (`sshd -t`, `sshd -T`, the `Match` probes). The engine plans again under the run lock, so the check is made again moments before the apply.

   The write (`<prefix>/files`, `StageFiles`) runs only when two things hold:
   - the engine has backed up `00-hardn.conf` (`backup.RollbackRequired`, `Change.Backups`; engine.md §2.4);
   - that backup shows the file as the plan read it (`ErrChanged` otherwise).

   The write is `host.WriteFileAtomicKeep`. A write that fails after its rename (`host.ErrNotDurable`) is a failed step, which the engine rolls back ([atomic.md](../safety/atomic.md) §7). **Its undo, journalled with the backup before the write, is the `ssh` reload with `IfUp`.** An sshd that starts between the write and the reload (on Ubuntu, `ssh.service` is started by a connection) reads the new file, and the rollback then reloads it. p2drive journals the reload the same way ([vmtest.md](../safety/vmtest.md) §2.6).
4. **The reload** (`<prefix>/reload`, `StageReload`) is `reload.Execute` of the plan `reload.PlanFor` made at plan time. It reloads `ssh.service` or OpenRC's `sshd`, or on `ssh.socket` runs `daemon-reload`, the socket check and `restart ssh.socket`, and never touches the `sshd` unit ([reload.md](../safety/reload.md) §2).

   **Its journal:** before it runs, the same `ssh` reload with `IfUp`. Once its gated step ran (`reload.Report.Steps`), the same reload without `IfUp`, also when `Execute` then failed ([revert.md](../safety/revert.md) §7 item 3). The engine writes that second entry under a context without the apply's cancellation (engine.md §2.4). So after a Ctrl-C that left `ssh.service` down, the rollback starts it (`TestEngineRollsBack`, "Ctrl-C during the reload").

   **One entry, not two.** The write and the reload journal the same reload, so the engine journals it once (`journalUndo`, §5). It carries `IfUp` only while both say `IfUp`. The journal therefore matches p2drive's at every moment.

   **`ConfirmVia`** is the listeners the reload adds (`reload.Plan.Adds`): a confirming connection must reach one of them ([reload.md](../safety/reload.md) §6).
5. **The check after the reload** (`<prefix>/verify`, `StageVerify`) reads `sshd -T` again. It fails unless every value the file sets shows, as written: on Proxmox VE as `pve.PlanSSH` left it, so a `PermitRootLogin no` asked for is written and checked as `prohibit-password` (brief 2.5). The values:
   - each `Port` in the Port set **and on one of sshd's listeners**;
   - the lockout-relevant settings (`wanted`), with `without-password` read as `prohibit-password`. An Unknown is not a pass.

   On Proxmox VE, `pve.CheckSSH` must also say `yes` to all three of root key-only, root by key and port 22 (`checkPVE`, [pve.md](../safety/pve.md) §6). It runs over the candidate at plan time too (§2.14), so this check catches only what changed after the plan.

   A failure is `ErrNotInEffect`, and the engine rolls the change back. Tested: `TestWanted`, `TestEngineRollsBack`, `TestEnginePVERoot` ("no asked: key-only written and checked", which fails with the directives as asked; "a drop-in that keeps root out, written after the reload": rolled back, the file as it was).

### The migration (`PlanMigration`)

6. **The R8 migration** ([sshd-migrate.md](../safety/sshd-migrate.md) §6, decision §3.17). It plans, in p2drive's order:
   - `migrate.Find` with the loaded `sshConfigFile`;
   - `migrate.PlanFor`;
   - `migrate.Check` (validation and the login-path comparison over a copy);
   - `reload.PlanFor` over `Plan.Reload` and `Plan.SocketDropIns`;
   - `Plan.CheckRollback` (§2.12);
   - for every reload that makes sshd bind what its files configure (§2.12), `reload.CheckInEffect` with the applying session's port; `reload.CheckFree`;
   - ufw over `Plan.Listening`.

   **Changes:** the firewall when a port is added through an active ufw; `<prefix>/files`, with no `Commands` (it runs none; its diff shows every file it writes or removes) and `migrate.Plan.Dropped` as findings (§2.23); the reload when the plan has keywords; and the check, which requires `sshd -T` to show what `Check` showed of the copy (`sameEffective`).

   The reload's undo is `migrate.Plan.Rollback()`: the listeners that accept connections now (`Plan.Listening.All`), with `listen.conf` named as a drop-in the restore puts back (`socketDropIns`). Its apply plans again from the drop-ins read once the files are gone, as p2drive's does.

   Tested: `TestEngineMigrate` covers:
   - Debian;
   - Ubuntu's `ssh.socket`: the journal names `listen.conf` and `[::]:2230`;
   - `sshd -T` not showing the migration after the socket restart: `listen.conf` and `manage.conf` restored, `00-hardn.conf` gone, `ssh.socket` back on `[::]:2230`.
7. **`migrate.Apply` takes the engine's backups (decision of this unit; [README](../safety/README.md): "`migrate.Apply` takes its own backups").** `migrate.Options` gains `Taken`, the backups the caller has taken by path. With it, `Apply` takes no backup of its own. It refuses a file a step changes that `Taken` lacks (`fs.ErrInvalid`, before anything is written). It checks each entry against the plan as it checks its own (`ErrChanged`). It reports those entries as `Report.Backups`.

   The engine backs up `Plan.Sources` (`Change.Backups`) and journals them before `Apply` runs. The journal therefore names every backup of every file the migration changes before any of them changes ([revert.md](../safety/revert.md) §7 item 1). The run holds one copy of each file, where p2drive's holds two, the second unjournalled. The journal is p2drive's (`Backup.Seqs` `[1, 2, 3]` on the v0.3.2 Debian host).

   **Why not a hook before `Apply`'s first write:** the engine journals a change's undo before `Apply` runs, and journals what ran once it returns (engine.md §2.4). A hook in the middle would need a journal call inside a change, an engine API that nothing else needs, and two places where a change's undo is journalled. Tested: `TestApplyTaken` (`pkg/host/sshd/migrate`), `TestEngineMigrate` (three backups in the run).

### The firewall, ufw's enable and the clean-up

8. **ufw** (`firewallChange`, `StageFirewall`, lockout risk). Planned with `ufw.Read` and `ufw.PlanFor` over the listeners sshd is configured for, the ports the change adds, and the applying session's own port kept (`Input.Keep`, [ufw.md](../safety/ufw.md) §6). Under `UFWKeep`, an inactive ufw gets no rule (`NoSaveWhileInactive`; [P-33](../refactor/proposals.md#p-33) is open), and on Proxmox VE or without ufw installed there is nothing to do. Under `UFWEnable`, ufw is enabled with default deny incoming, after its allow rules.

   **Its journal:** `Plan.PreUndo()` before it runs; once it ran, error or not, `Report.Undo`, which comes from ufw's answers, so an allow ufw kept is not deleted. Both are `run` steps, `why: firewall`. `ufw.Plan.Findings` ride on the change, or on the write when ufw has nothing to do (§2.23).

   **`PlanUFWEnable`** is p2drive's `ufw-enable` (it needs sshd's listeners established, `ufw.ErrListenersUnknown`).
9. **The clean-up after the confirmation is a change of its own** (`PlanCleanup`; brief 2.3 step 5: "Confirm the change from a new connection, then remove the old port rule"). Its input is `Cleanup{Drop, Firewall}`. `CleanupOf(journal)` reads it from a confirmed change's journal (`confirmed.json` keeps the last one, [revert.md](../safety/revert.md) §2.5):
   - `Drop`: the ports of the listeners its reload restores, which are what sshd listened on before the change, less any `ConfirmVia` has;
   - `Firewall`: its `ufw delete allow` undo commands.

   It plans:
   - `00-hardn.conf`'s Port lines without the dropped ports, edited line by line and validated in context;
   - on Proxmox VE, `pve.CheckPort` over the ports left;
   - the reload with `reload.Change.Confirmed`, which may only drop listeners ([reload.md](../safety/reload.md) §2.5);
   - `CheckInEffect` with the applying session's port;
   - before them, `ufw delete allow <port>/tcp`, only for a rule hardn added (one of `Firewall`, [ufw.md](../safety/ufw.md) §6) that ufw still has. The rule it journals first, `ufw allow <port>/tcp`, keeps an existing rule when the delete never ran.

   It refuses a port another file keeps (a `Port` line in `sshd_config`), since hardn edits only `00-hardn.conf`, a clean-up that would leave sshd no port (`reload.ErrDropsListener`), and on Proxmox VE a port other than 22 left. The check after the reload requires the dropped ports gone from the Port set and from every listener. Its `ufw delete allow` is read back (`ufw.Read`), as `ufw.Execute` does for every other ufw change: a rule still there fails the step (`ufw.ErrVerify`), and the clean-up is rolled back. Its changes are of lockout risk, so the clean-up is confirmed like any other change; planned again, it is empty.

   **Who plans it, and when**, was the maintainer's question that engine.md §7 set out: the next run, `hardn confirm`, or the session that applied the change. **Decided ([P-40](../refactor/proposals.md#p-40), 2026-10-19): `hardn confirm`**, built by `next/p3-confirm-cleanup` ([confirm-cleanup.md](confirm-cleanup.md)), which reads the journal at once; for a clean-up it does not run then (a console, a crash, a refusal), it reads `confirmed.json` before the next claim replaces it and carries the clean-up to that confirmation when that change held no port (confirm-cleanup.md §2.8), so the limit below bites only a carried clean-up the next confirming session may not run, which the report gives to finish by hand. The change kind serves all three, with one limit on the first. **`CleanupOf` reads one journal**, and `confirmed.json` keeps only the last confirmed lockout-risk change ([revert.md](../safety/revert.md) §2.5). Once another one (`keyonly`, a sudoers change, another SSH change) is confirmed before the clean-up is planned, `CleanupOf(confirmed.json)` is empty: port 22, and hardn's ufw rule for it, stay open until something else drops them. That locks no one out, but it is a silent loss of hardening. So "the next run" can take the clean-up from `confirmed.json` only while it still holds the port change; `hardn confirm` and the applying session read it at once. `Drop` could be derived without the journal (`00-hardn.conf`'s Port lines against the configured `sshPort`), the ufw half could not: "only where hardn added it" needs the change's `Report.Undo`. So the pending clean-up would have to be kept somewhere a later confirmation does not replace. An input to the maintainer's decision (§7).

   Tested: `TestCleanupOf`, `TestEngineCleanup` (22's rule kept where ufw allowed 22 before the change, deleted where hardn added it; empty when planned again), `TestEngineCleanupVerified` (ufw keeping the rule: rolled back), `TestEngineRefusedBeforeArm` (the clean-up's refusals).

   **Since `next/p3-confirm-cleanup`** ([confirm-cleanup.md](confirm-cleanup.md) §2.4): `Cleanup.Proven`, which `hardn confirm` sets once `revert.CheckConfirm` has accepted the session (a new SSH connection, from another machine, to a listener the change added), plans the clean-up without a revert timer of its own: its changes take the class they have without the lockout risk (the reload `RiskServiceRestart`, the rest `RiskPlain`), so the engine journals it `other`, and `PlanCleanup` refuses unless the applying session is an SSH connection to a port the clean-up keeps (`ErrSessionNotKept`, `SSH_CLEANUP_SESSION_NOT_KEPT`). Without `Proven` it is as above. The write's summary says what it does: `Drop port 22 from /etc/ssh/sshd_config.d/00-hardn.conf`. Tested over the fake VM in `internal/tools/p2drive/confirm_test.go`.

### The units with a rollback of their own

10. **Sudoers, DNS, Alpine's updates and Proxmox VE's root keys** (`Sudoers`, `DNS`, `Alpine`, `PVEKeys`). Each is journalled as its unit's registered plan step, made with the step function (`plans.Sudoers`, `DNS`, `Alpine`, `PVEKeys`). The step function refuses a plan its rollback would refuse, before the plan becomes a change (`plans.ErrPlan`, [revert-plans.md](../safety/revert-plans.md) §2.4).

    The engine journals the step before the unit's apply. The rollback, here or from the timer, is the unit's own. Each unit's own backups go into the plan's backup run (`engine.Run.Store`) and stay out of the journal. For root's keys this is on purpose: a restore would drop the keys other nodes wrote meanwhile ([pve.md](../safety/pve.md) §2.12, §6).

    **An apply refused before it wrote anything drops the step again** (an empty `engine.Ran.Rollback`), so the rollback only ends the change ([revert-plans.md](../safety/revert-plans.md) §6). Each unit signals "wrote nothing" its own way:
    - **sudoers:** an error with nothing installed, nothing restored and not `sudoers.ErrNotRestored`, or a backup that failed before `sudoers.Apply`;
    - **Proxmox VE:** an error from `ApplyKeys` with a zero `backup.Result`;
    - **DNS and Alpine:** the rollback runs only after an `Apply` that returned nil ([dns.md](../safety/dns.md) §6, [alpine.md](../safety/alpine.md) §6), so any error drops the step, except `ErrNotRestored`. There the unit's own undo did not finish, and the step stays, so that the journal's rollback tries again what is left of the plan's own writes ([revert-plans.md](../safety/revert-plans.md) §6). **Decision of this unit:** the README's "only after an `Apply` that returned nil" is read with revert-plans.md's exception, because the alternative leaves a half-changed host with nothing journalled to undo it.

    **Classes:**
    - sudoers: `StageAccounts`; lockout risk when a change of the plan is `sudoers.RiskLockout` (a grant removed, or one that asks for a password), so that a new SSH connection, where elevation is checked, confirms it; plain otherwise. Its files are backed up into the run before `sudoers.Apply` ([sudoers.md](../safety/sudoers.md) §6).
    - Proxmox VE's keys: `StageAccounts`; lockout risk when `Narrows()`.
    - DNS: `StageFiles`, plain (brief 2.4).
    - Alpine: `StagePackages`; the highest class of its steps, whatever their order: a service restart when crond is started or restarted, else a package install for apk-cron's install alone, else plain (`TestAlpineRisk`).

    Each names its unit as a resource (`plans.UnitSudoers` and the others, §2.22).

    **DNS, since `next/s2-control`** ([control-dns.md](control-dns.md) §8, the defects [gallery-dns.md](gallery-dns.md) §8 recorded):
    - the plan's findings ride on the change (`DNSFinding`, §2.23), where they were dropped (D1);
    - a file the plan creates (the drop-in, a new `/etc/resolv.conf`) is diffed against `/dev/null`, from the plan's own reading of the path (`From`), so the diff and the JSON document's change show it created, not an empty file edited (D2);
    - an `Apply` that failed after acting and undid its own steps (`dns.Result.Restored`) drops the step and says so (`engine.Ran.Undone`), so the engine reports the change rolled back (engine.md §2.15); one whose own undo did not finish (`dns.ErrNotRestored`) keeps the step and says so too, so the engine reports it rolled back once the journal's rollback, which runs `dns.Rollback`, finished; one refused before it acted drops the step and says nothing undone. A reload that ran without effect, undone by `dns.Apply` or left to the journal, is a `Failure` in a few words, "Reloading systemd-resolved had no effect", with the next step, "Run resolvectl dns to see the servers in use." (proposed), which `pkg/output` shows in place of `dns`'s error, its code on the step's line (D3). Tested: `TestDNSChange`.

    Tested: `TestPlanStepDropped` (Proxmox VE off the platform, DNS on a host changed since the plan, the DNS and Alpine rule, a plan each step function refuses); `TestPlanStepKept` (each of the four keeps its step once it wrote; sudoers drops it when its backup fails before `sudoers.Apply`); `TestEngineSudoersRefused` (`visudo -c` refusing before any write: the rollback starts from a journal with no plan step, and the journal is cleared); the sudoers grant in `TestEngineParity`.

### Plan-time checks: every refusal before the timer

11. **`reload.Execute`'s in-use check is a check of its own.** `reload.CheckFree` (new, §5) runs it at plan time: `ss -Htln` must show no socket but the held ones on a port the change adds (`reload.ErrInUse`; on a socket host also `ss` missing or failing). It runs for every reload that adds listeners. `Execute` still runs the check before its first step. Tested: `TestCheckFree`, `TestExecuteInUse` (the same refusals on their own) and `TestEngineRefusedBeforeArm` ("another service holds the new port").
12. **`migrate.Plan.CheckRollback`, and `reload.CheckInEffect` for every reload that makes sshd bind its files' listeners.** `CheckRollback` runs for every migration. `CheckInEffect` runs in `PlanSSH`, `PlanMigration` and `PlanCleanup`, with the applying session's own end (`revert.ParseConn`'s `Server`) from `engine.Input.Session` (§2.18), for every reload that makes sshd, or `ssh.socket`, bind what the files configure (`rebinds`): a listener change, **and any reload of `ssh.service` or OpenRC's `sshd`**, whatever keywords the change sets. On `SIGHUP` sshd closes its listening sockets and re-executes, reading every file again ([reload.md](../safety/reload.md) §3), so a change with no listener keyword (`keyonly`) on a host whose files name a port sshd has not bound (an edit of `sshd_config` not reloaded) would move sshd there. Every later check passes (the reload's, which checks no listener when none changed, and the check after it, which reads the auth keywords); the apply asks for a confirmation; and the timer's rollback restores `00-hardn.conf` alone, so sshd stays on the new port, which ufw or a network firewall may not let through. The configured set must also be established (`reload.ErrListenersUnknown`), so a stopped `ssh.service` refuses `keyonly` too. On `ssh.socket` a reload with no listener keyword reloads `ssh.service`, whose sshd keeps the descriptors systemd passed it ([reload.md](../safety/reload.md) §3, **[I]**), so it is not checked. p2drive's `keyonly` has the same gap, and p2drive stays as it is. A remote session whose connection is not established is refused, closed ([revert-plans.md](../safety/revert-plans.md) §2.10). Tested: `TestEngineRefusedBeforeArm` (`keyonly` with a `Port` line sshd has not read, Debian 13 and Alpine 3.24; with `ssh.service` not up).

    **P-39 (a), decided:** on the D9 host, `CheckRollback`'s refusal (`reload.ErrRestoredListeners`) comes with a report (`Refusal`, code `SSH_LISTEN_CONF_NOT_IN_EFFECT`). The D9 host is one where v0.3.x's `listen.conf` names ports none of which `ssh.socket` is configured for. The report says three things:
    - what was found: `listen.conf`'s port, and what the socket is configured for;
    - why it is refused: the rollback's `daemon-reload` would move the socket to `listen.conf`'s port alone, off the port the host is reached on;
    - how the host becomes migratable, in the order to do it: first make sure the firewall lets `listen.conf`'s port through, since the socket then listens there alone; then, from this session, `systemctl daemon-reload` and `systemctl restart ssh.socket`, which established sessions survive ([vmtest.md](../safety/vmtest.md) §4.9, run), and a new connection to that port checked before hardn runs again. A reboot also works, but ends the session, so it is offered last, and only with console access in case the port is not reachable. Without the firewall step first, the fix could itself lock the user out.

    The report's sentinel is `ErrListenConfNotInEffect` (§7).

    A socket configured for more than `listen.conf` gives is refused by `CheckRollback` with its own words; it is not D9, and gets no report.

    Tested: `TestNotInEffect`, and `TestEngineRefusedBeforeArm`, where every row of p2drive's `TestRefusedBeforeArm` is refused by the engine's plan with no change outside the run directory and no run lock taken. Among them: D9, the socket configured for more, configured 2230 and bound 22 with the session on 22 and on 2230, `sshd -T` showing 2231 while sshd listens on 22, and a session whose connection is not known.
13. **A port change must reach a listener**, before and after:
    - before: the new port among `reload.Plan.Adds` (`ErrNoNewListener`, naming each `ListenAddress` with its own port by file and line: the R8 path, a migrated `ListenAddress 0.0.0.0:2230`);
    - after the reload: on one of `sshd -T`'s listeners, not only in its Port set (§2.5).
14. **Proxmox VE.**
    - `pve.CheckPort` before a port change (`pve.ErrPortChange`, §3.18; `TestEngineRefusedBeforeArm`), and over the ports a clean-up leaves;
    - `pve.PlanSSH` over the whole file (§2.2);
    - `pve.CheckSSH` over the candidate at plan time (`checkPVECandidate`): `sshd -T` of the validated copy, evaluated with detect's SSH facts (on a socket host with `ssh.socket`'s listeners) so that its listeners are established as the check after the reload reads them, and the installed files with the candidate `00-hardn.conf`, for their `Match` blocks. Root key-only, root by key and port 22 must each be yes, else `ErrProxmoxRoot`, with the findings that say why ("AllowUsers alice does not match root"). What the candidate already shows keeping root out is refused before the timer, where it used to arm the timer, write the file and reload sshd with the cluster's root login by key gone before the rollback. `TestEnginePVERoot` ("another drop-in keeps root out");
    - `pve.CheckSSH` again after the reload (§2.5).

    **`pve.CheckAccount` is not wired:** no change kind of this unit, nor the engine, locks, deletes or expires a password. The first one is the user-management slice's, which pve.md §6 already names; that README item stays open.
15. **`sshd -T` without `/run/sshd`** (Ubuntu before its first SSH connection since boot) **is refused with the reason** (`Refusal`, code `SSHD_ENV_NO_RUNDIR`, sshd-validate.md §6's code; sentinel `ErrNoRunDir`). `sshd.ReadEffective` and `sshd.Validate` fail with `*sshd.EnvError{EnvPrivsepDir}`. The report says that sshd's listeners are not established without it, and its fix fits how sshd is started: where `ssh.socket` listens, an SSH connection to the host, which starts `ssh.service`, which creates the directory; on `ssh.service`, where the directory is gone while the service is stopped and a connection starts nothing, `systemctl start ssh.service` from the console; on OpenRC, the directory sshd names, made root's and writable by root alone, as sshd requires (sshd-validate.md §3). No other source of the listeners is used: `ss` does not say whose a socket is ([reload.md](../safety/reload.md) §6). Tested: `TestNoRunDir` (each mode), and `TestEngineRefusedBeforeArm`'s row on Ubuntu.
16. **The support level** (`detect.Support`) is the engine's gate for every plan, the DNS plans included: closed by `next/p3-engine` (engine.md §2.6).

### Inputs

17. **`--confirm-timeout` / `confirmTimeout`** (§3.14). `engine.ConfirmTimeout(flag, cfg)` reads `cfg.ConfirmTimeout`, a new `pkg/config` key, with `revert.ParseTimeout`, and resolves it with the flag through `revert.ResolveTimeout`. The flag wins; the default is 180 s, the floor `revert.MinTimeout`; a bad value is `revert.ErrTimeout`. The result is `engine.Options.Timeout`. The key is `omitempty`, so a legacy save writes nothing new. The six config-load characterization goldens gain one line each, `"ConfirmTimeout": ""`, which is the new field, empty (`TestConfigLoadGoldens`).

    It is in `pkg/engine` (`timeout.go`), not in this package: it resolves an engine option, `pkg/engine` already imports `pkg/config` and `revert`, and the wiring that builds the engine then need not import the change kinds (§5).

    **No flag is wired in `cmd/hardn` here.** The wiring that builds the engine's `Options` (`p3-output`, or slice 6 with the first lockout-risk command) parses `--confirm-timeout` with `revert.ParseTimeout` and passes it with the loaded configuration to `engine.ConfirmTimeout`. Tested: `TestConfirmTimeout` (`pkg/engine`), `TestConfirmTimeoutKey`.
18. **`engine.Input.Session`** (new, §5) is the session applying the change, the one the journal names. Controls read the applying session's port from it for `CheckInEffect` and ufw's `Keep`, so the plan and the journal cannot name two sessions (`TestInputSession`).
19. **`config.prohibitPasswordApplied` is not flipped.** No change kind of this unit applies `Config.RootLogin()`: `PlanSSH` takes the directives its caller asks for, and the control that maps `permitRootLogin` to them is slice 6's. The flag flips there ([sshd-migrate.md](../safety/sshd-migrate.md) §2.21).

### Parity with p2drive

20. **The brief's 2230 scenario, `keyonly`, `ufw-enable` and the sudoers grant** run through the engine on p2drive's own fake VM, beside p2drive on a twin VM (`TestEngineParity`). The platforms:
    - Debian's `ssh.service`;
    - Ubuntu's `ssh.socket`;
    - Alpine's OpenRC (no ufw installed).

    2230 and `keyonly` run on all three; `ufw-enable` and the sudoers grant on Debian and Ubuntu (Alpine has neither ufw nor sudo in p2drive's fake, whose `doas` is not modelled).

    **Compared:**
    - the commands that change the host, in order: every command run or started (the `systemd-run` timer, the OpenRC sleeper, `ufw allow`, `systemctl reload ssh.service`, `daemon-reload`, `restart ssh.socket`, `rc-service sshd reload`, `visudo`), the change's id read as `<id>`;
    - the last journal written;
    - `00-hardn.conf`'s directives;
    - ufw's rules and the bound sockets.

    **Equal, except for:**
    - the reads: `sshd -t`/`-T`, `ss`, `ufw status`, `systemctl is-active`/`show` and the engine's `list-timers` for the deadline. The engine reads more: it plans twice, once more under the run lock;
    - the journal's `summary`: the engine's is `Plan.Summary()`, p2drive's its own words;
    - for a change without lockout risk, the journal's `session`: the engine journals the applying session for every change, p2drive only where a confirmation needs it;
    - `00-hardn.conf`'s header and label (hardn's, migrate's; p2drive's name itself).

    `TestEngineOrder` pins the order on Debian's `ssh.service` as `TestPort2230` does: armed, `ufw allow 2230/tcp`, the backup, the journal with the reload `IfUp` before the write, the write, the reload. `TestEngineRollsBack` covers p2drive's `TestPortRollsBack`, `TestFailedAfterArm` and `TestCancelledDuringReload` under the engine's signal hold.

    **p2drive itself is unchanged**: the comparison is a test file in its package (`internal/tools/p2drive/engine_test.go`) that drives the engine over its fake. Since `next/p3-revert-boot` p2drive installs the boot unit before it arms a lockout-risk change, and since `next/p3-engine-boot` the engine does too, so the commands compared include that install ([engine-boot.md](engine-boot.md) §2.5). `setDirectives`, `pveSet`, `wanted` and `sameEffective` are copied from it: a `main` package cannot be imported. Moving p2drive onto this package is left for a later revision of the driver.

### Through `host.System`

21. Depguard rule `steps-through-host`, the last of the rules, denies these to `pkg/engine/steps`' non-test files: `os`, `io/ioutil`, `golang.org/x/sys`, `syscall`, `net` (but `net/netip`), `plugin` and `pkg/ui`. `engine-through-host`'s glob covers the directory too; the rule of its own keeps the unit's rule if that glob changes. Probed in a scratch copy outside the repository: an `os` import fails lint, reported under `engine-through-host` (depguard names the first rule that denies it), and under `steps-through-host` once the engine rule's glob is narrowed to leave this directory out.

### Several controls in one plan

22. **Two controls that would plan the same thing are refused at plan time** (`engine.ErrConflict`, a change of the engine's, §5). Each planner reads the host as it is, not as another control's change in the same plan leaves it. Two SSH planners each wrote `00-hardn.conf` from the same old content, and the second write failed (`ErrChanged`) after the timer was armed and ufw changed. `PlanUFWEnable` beside `PlanSSH{Port: 2230}` on an inactive ufw applied as a success: the SSH planner, seeing ufw inactive, allowed nothing, and ufw's enable allowed 22 alone, so a confirmation on 2230 could never arrive and the timer reverted the change. The engine now refuses, before anything changes:
    - **two changes that back up the same file** (`Change.Backups`), from one control or two;
    - **two controls whose changes name the same resource** (`Change.Resources`, new): every change of `PlanSSH`, `PlanUFWEnable`, `PlanMigration` and `PlanCleanup` names `sshd` and `ufw` (`ResourceSSHD`, `ResourceUFW`), since each reads both (an inactive ufw decides that no rule is saved); the unit changes name their unit (`sudoers`, `dns`, `alpine-updates`, `pve-keys`). One control's changes may share them.

    **The rule for slice 6 and Run All**, which the engine now enforces: every SSH setting of one plan, and ufw's enable, go through **one** `PlanSSH` (`UFW: UFWEnable`; it plans the enable when nothing else is left, §2.1); the migration and the clean-up each go in a plan of their own, never beside another SSH change; the changes of one unit go through one plan of that unit (`sudoers.PlanFor` takes several policies). A sudoers grant shares a plan with an SSH change (other resources; `StageAccounts` runs before `StageFiles`, brief 2.3 step 1). Tested: `TestConflicts` (`pkg/engine`), `TestEngineCombined` (two SSH controls, ufw's enable beside a port change, the clean-up beside an SSH change: refused; `keyonly` with ufw's enable through one `PlanSSH`, the enable alone when the settings are in place, a sudoers grant beside `keyonly`: applied).

### Findings

23. **The planners' findings ride on their changes** (`engine.Change.Findings`, new, additive; a change of the engine's, §5), instead of being dropped: `pve.PlanSSH`'s (a `PermitRootLogin no` written as `prohibit-password`), `sshd.Validate`'s and `migrate.Check`'s (a line another file sets first), `ufw.Plan.Findings` (an inactive ufw whose saved rules would not let SSH through once it starts, [P-33](../refactor/proposals.md#p-33); carried on the write when ufw has nothing to do), and `migrate.Plan.Dropped`. They do not refuse anything; p3-output renders them as it renders a check's. Each code is the primitive's own stable kind code (pinned by its `TestFindingCodes`), upper case, under its prefix, said once: `PVE_ROOT_KEY_ONLY`, `SSHD_OVERRIDDEN`, `UFW_SSH_NOT_ALLOWED_AT_START`; a dropped migration line is `SSH_MIGRATE_LINE_DROPPED`. Tested: `TestEngineFindings`, `TestFindingCode`.

    **DNS's** (`DNSFinding(state, finding)`, since `next/s2-control`, for a plan and a check alike) are coded the same way (`DNS_PER_LINK`, `DNS_DHCP_CLIENT`, `DNS_UDHCPC`, `DNS_PROXMOX_NODE`) and worded in a few words where `dns`'s detail is a long sentence (gallery-dns.md §5, decision 5; proposed): "eth0 still uses its own nameserver 192.0.2.1" (its source `resolvectl dns`), "dhcpcd may rewrite /etc/resolv.conf on a lease", "udhcpc rewrites /etc/resolv.conf on each lease" (and "may", when whether it does is not established), "/etc/udhcpc/post-bound/10-dns may rewrite /etc/resolv.conf" (a udhcpc hook named by its path, since `next/s2-flow-review`; it was "a udhcpc hook may rewrite /etc/resolv.conf"), "Saving Proxmox VE's DNS settings rewrites /etc/resolv.conf". The state says which of udhcpc's it is; the per-link finding's link is `dns.Finding.Link`.

### The save of hardn.yml (`PlanSave`)

24. **One change kind for every save of the configuration** (refactor plan §3.19, approved 2026-10-17: to the loaded path, by one atomic save function, warning that comments are lost; slice 2 decision 1, adopted 2026-10-22; `next/s2-control`). *Amended by `next/s2-config-edit`* ([config-edit.md](config-edit.md)), the maintainer's decision of 2026-10-22 for every slice: "edit only the keys that changed, keeping the rest of the file and its comments; drop the comments-lost warning". `PlanSave` now returns `[]engine.Change`, none when the file already gives `cfg`'s settings; its diff is `config.Edit`'s, the settings changed edited in place; it refuses an edit `config.Edit` refuses (`CONFIG_NOT_EDITABLE`, `CONFIG_EDIT_UNSAFE`, each with a report: Why what about the file stops it, Fix "set <settings> in <path> by hand, then try again"; proposed); and it has no finding (`CodeCommentsLost` removed). *Since `next/s2-config-edit`'s review fixes*, `PlanSave(ctx, sys, id, cfg, settings, after...)` saves the `settings` named alone (their `hardn.yml` keys: the DNS control's `dns.Setting`, `nameservers`), every other setting as the file has it, whatever `cfg` holds in memory (`-u`'s and `-n`'s values, which `cmd/hardn` sets after loading, are not saved); naming none is `fs.ErrInvalid`; and a `CONFIG_EDIT_UNSAFE` report names a setting only when the save changes it ([config-edit.md](config-edit.md) §5, decision 12). The text below is `next/s2-control`'s, these points aside. `PlanSave(ctx, sys, id, cfg, after...)` reads the file at `cfg.Path()` and plans `config.Save` of `cfg` there, after the changes `after` names: `StageFiles`, plain, `Backups` the file (so the engine backs it up and journals it before the save, and the rollback restores the bytes the plan read), `Resources` `hardn.yml` (`ResourceConfig`: one plan saves it once; a second save is `ErrConflict`). Before anything changes it refuses, each with a report (`Refusal`), a configuration not loaded from a file (`config.ErrNotLoaded`, `CONFIG_NOT_LOADED`), a symlink, which `config.Save` would refuse (`host.ErrSymlink`, `SYMLINK_REFUSED`: "<path> is a link", "edit the file it links to by hand"), and a file gone or whose bytes are not the ones the configuration was read from (`config.Config.LoadedFrom`; `ErrConfigChanged`, `CONFIG_CHANGED`, the report `Changed(path, cause)` gives: "<path> changed since it was read", "start again, so that the file is read as it is now"): a save over an edit made since would undo it. A control that reads the file to decide whether to save gives the same report for a file gone or no longer decoding (`config.ErrInvalidFile`, `CONFIG_FILE_INVALID`, new). Its `Apply` refuses, before it writes, a file whose backup, taken just before, is not what the plan read (`ErrConfigChanged`), and a configuration that no longer gives the bytes the diff shows (`engine.ErrPlanChanged`). A save that fails is a `Failure`, "Saving /etc/hardn/hardn.yml failed" (proposed), and the engine rolls back the changes before it, so that the host and `hardn.yml` agree (decision 1 (iii)). With comments in the file (`config.HasComments`), its finding was `CONFIG_COMMENTS_LOST`, "Saving hardn.yml loses its comments" (removed by `next/s2-config-edit`). Its title is "Save the configuration", its target the path; **its diff is the file against what `config.Save` writes**: since `next/s2-config-edit` `config.Edit`'s bytes, the settings changed alone (it was `config.Marshal`'s, every key of the configuration, canonical, so a short `hardn.yml` was shown rewritten whole, defaults included; the mock showed none, a placeholder: gallery-dns.md §8). Which controls plan it, and when, is theirs: the DNS control plans it when the list it applies is not the one `hardn.yml` holds ([control-dns.md](control-dns.md) §2.9). Tested: `TestPlanSave` (since `next/s2-config-edit` with the exact diff, the file saved byte for byte, the user name and dry run set in memory not saved, no change when nothing changed, no setting named refused, and `CONFIG_NOT_EDITABLE`'s and `CONFIG_EDIT_UNSAFE`'s reports); with the engine, `pkg/engine/controls/dns`' `TestApply` (the list saved alone, the user name and dry run set in memory kept out), `TestFailedSave`, `TestRefusals` (`CONFIG_NOT_EDITABLE` and `CONFIG_EDIT_UNSAFE` among them).

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| A `ListenAddress` with its own port ignores every `Port` line ("If port is not specified, sshd will listen on the address and all Port options specified") | sshd_config(5), V_10_0_P1, as cited in [vmtest.md](../safety/vmtest.md) §2.3 | §2.13 |
| `sshd -T` needs `/run/sshd` on Debian and Ubuntu; only `ssh.service`'s `RuntimeDirectory=sshd` creates it, and it is gone while the service is stopped | [sshd-validate.md](../safety/sshd-validate.md) §3 **[V]** (Debian `debian/rules`; LP #2107356) | §2.15 |
| That a socket-activated Ubuntu before its first connection is in that state | **[I]** ([sshd-validate.md](../safety/sshd-validate.md) §2.8) | §2.15 |
| v0.3.2 wrote `ssh.socket.d/listen.conf` without `daemon-reload` (D9); its `ListenStream=` reset, then `ListenStream=<port>`, replaces the generator's listeners once read | [side-effects.md](../refactor/phase0/side-effects.md) D9 (**[V]** grep, **[I]** its effect); [reload.md](../safety/reload.md) §3 | §2.12 |
| `daemon-reload` reruns `sshd-socket-generator`; `restart ssh.socket` then binds what `systemctl show -p Listen` says; SSH sessions survive `restart ssh.socket` | [vmtest.md](../safety/vmtest.md) §4.9, the Ubuntu 26.04 run (**run**, 2026-10-19) | §2.4, §2.9, §2.12 (the report's fix) |
| `systemctl reload ssh.service` makes sshd listen on an added `Port` before `ss` is read, and stop listening on a removed one | [vmtest.md](../safety/vmtest.md) §4.9, the Debian 13 run (**run**) | §2.4, §2.9 |
| An established SSH connection survives `ufw --force enable`; a port whose rule is deleted then times out; `ufw delete allow N/tcp` runs without a prompt with no terminal | [vmtest.md](../safety/vmtest.md) §4.9, the Debian ufw pass (**run**, IPv4) | §2.8, §2.9 |
| That an established session on a port whose ufw rule the clean-up deletes survives (ufw's `before.rules` accept established traffic) | **[I]** | §2.9 |
| `revert.Rollback` restores every backup, then runs the undo steps, then the reloads, in order | [revert.md](../safety/revert.md) §2.14 | §2.4: two equal reloads journalled once undo the same |
| p2drive's command sequence, which `TestEngineParity` shows the engine's equals on the fake, passed on Debian 13 and Ubuntu 26.04 | [vmtest.md](../safety/vmtest.md) §4.9 (**run**, p2drive built from `next/p2-revert-plans`) | §2.20: the engine's sequence is one that ran, though the engine has not |
| yaml.v3 decodes an integer scalar (`confirmTimeout: 240`) into a string field as written | **observed**, `TestConfirmTimeoutKey` | §2.17 |
| On `SIGHUP` sshd closes its listening sockets and re-executes itself, reading its files again; `ssh.service`'s and OpenRC's reloads send it | [reload.md](../safety/reload.md) §3 (OpenSSH V_10_3_P1 `sshd.c`, `sighup_restart`; Debian's `ssh.service`, Alpine's init script) | §2.12: any reload of `ssh.service` or OpenRC's `sshd` rebinds |
| That rebinding moves sshd to a `Port` its files name: on Debian 13's `ssh.service` | [vmtest.md](../safety/vmtest.md) §4.9 (**run**, an added and a removed `Port`); on OpenRC **[I]** ([reload.md](../safety/reload.md) §3) | §2.12 |
| On a socket host a reload cannot change `Port` or `ListenAddress`: sshd uses the descriptors systemd passes | [reload.md](../safety/reload.md) §3 **[I]** | §2.12: no check for a non-listener reload on `ssh.socket` |
| For most keywords the first value sshd obtains wins; `Port`, `AllowUsers` and the others named there accumulate | [sshd-effective.md](../safety/sshd-effective.md) §3 | §2.1: the line sshd reads first, named in the refusal |
| sshd needs its privilege separation directory owned by root and writable by neither group nor others | [sshd-validate.md](../safety/sshd-validate.md) §3 (OpenSSH `sshd.c`) | §2.15: the fix on OpenRC |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root, and no change has gone through the engine on a host. On the VMs of [vmtest.md](../safety/vmtest.md), once a command puts the engine and these planners behind it (`p3-output` or slice 6), these are still to run:

- **2230 through the engine** on Debian 13 (`ssh.service`), Ubuntu 26.04 (`ssh.socket`) and Alpine 3.24 (OpenRC), with and without an active ufw. The new port confirmed from a new connection; the timer's rollback at the deadline; the four refusals of `hardn confirm` ([vmtest.md](../safety/vmtest.md) §4.4).
- **`keyonly`, `ufw-enable` and the sudoers grant** through the engine, as vmtest.md §4.4 steps 3, 10 and 13.
- **The clean-up** after a confirmed 2230 on Debian 13 and Ubuntu 26.04:
  - 22 dropped, `ss` showing 2230 alone;
  - the session that applied it, on 22, surviving `systemctl reload ssh.service` and `restart ssh.socket` **[I]**;
  - hardn's ufw rule for 22 deleted and a pre-existing one kept;
  - its own confirmation from a new connection to 2230.
- **The migration** on the v0.3.2-hardened images ([vmtest.md](../safety/vmtest.md) §4.8), Debian and Ubuntu, through the engine. Also **the D9 refusal and its report** on a v0.3.2 Ubuntu not rebooted since, then the report's fix (`systemctl daemon-reload`, `systemctl restart ssh.socket`, a new connection to 2230), and the migration then applied.
- **`/run/sshd`** on Ubuntu 26.04 from the console before any SSH connection since boot: refused with the report; planned once a connection has started `ssh.service`.
- **Proxmox VE 9 by hand** ([vmtest.md](../safety/vmtest.md) §4.7, [pve.md](../safety/pve.md) §4): `PermitRootLogin prohibit-password` through `pve.PlanSSH`, `pve.CheckSSH` after the reload; a port change refused at plan time.
- **DNS, Alpine's updates and Proxmox VE's keys** through the engine, with their registered rollbacks from the timer and from `hardn rollback <id>` ([revert-plans.md](../safety/revert-plans.md) §4): no driver command makes these changes yet.
- **The review's refusals** (§2.1, §2.12, §2.14): `keyonly` on Debian 13 and Alpine 3.24 after `Port 2230` is added to `sshd_config` and not reloaded, refused with sshd still on 22; a port 2230 in `00-hardn.conf` that sshd has not read, refused; on Proxmox VE 9, `PermitRootLogin prohibit-password` beside a drop-in with `AllowUsers alice`, refused at plan time.
- **ufw's enable through `PlanSSH`** with `keyonly` on an inactive ufw, and **the clean-up's read-back** of ufw after `ufw delete allow` on Debian 13.

## 5. Changes to other units' code

- **`pkg/engine`** (`next/p3-engine`'s; [engine.md](engine.md)'s API list predates these):
  - `Input` gains `Session` (§2.18);
  - `Change` gains `Resources` and `Findings`; `ErrConflict`, refused at plan time by `conflicts` (`conflict.go`) for two changes that back up one file and two controls that name one resource, the refusal in the second control's `Item` (§2.22, §2.23). Why in the engine: only the plan sees every control's changes. `ConfirmTimeout` moved here from this package (`timeout.go`, §2.17). Tested: `TestConflicts`, `TestConfirmTimeout`;
  - the journal's undo is built by one function, `journalUndo`, for the apply's `record` and the plan's `checkJournals` alike. It journals an `ssh` reload two changes name once, `IfUp` only while both do (§2.4).

  Why in the engine: the write and the reload are separate changes in separate stages (engine.md §2.5), and each must journal the reload for its own reason. Only the journal sees both. The rollback's effect is unchanged, since revert reloads after every restore (§3). Tested: `TestJournalUndo`, `TestInputSession`; every engine test passes unchanged.
- **`pkg/host/reload`** ([reload.md](../safety/reload.md), `next/p2-reload`'s page, whose API list predates this): `CheckFree(ctx, sys, Plan) error`, `Execute`'s check before its first step, exported. `Execute` is unchanged.
- **`pkg/host/sshd/migrate`** ([sshd-migrate.md](../safety/sshd-migrate.md) §6 records it; its §5 API list predates it): `Options.Taken`, and `Apply` taking those backups (§2.7). Without `Taken`, `Apply` is as it was.
- **`pkg/config`**: `Config.ConfirmTimeout` (`confirmTimeout`, `omitempty`), §2.17; its comment names `engine.ConfirmTimeout`. `hardn.yml.example` does not document it yet, as for every key not yet wired.
- **`.golangci.yml`**: `steps-through-host`, appended last (§2.21).
- No dependency changes: `go.mod` and `go.sum` are unchanged.

## 6. API

```go
package steps // pkg/engine/steps (new)

const CodeListenConfNotInEffect = "SSH_LISTEN_CONF_NOT_IN_EFFECT"; const CodeNoRunDir = "SSHD_ENV_NO_RUNDIR"
const CodeMigrateDropped = "SSH_MIGRATE_LINE_DROPPED"                // a finding's
const ResourceSSHD = "sshd"; const ResourceUFW = "ufw"                // engine.Change.Resources
var ErrListenConfNotInEffect, ErrNoRunDir error                       // Refusal's, by Code
var ErrNoWriteStrategy, ErrMigrateFirst, ErrNoNewListener, ErrNotInEffect, ErrChanged, ErrProxmoxRoot error
type Refusal struct { Code, Why, Fix string; Err error } // Error, Unwrap, Is

type UFW uint8 // UFWKeep, UFWEnable
type SSH struct { Prefix string; Set []migrate.Directive; Port uint16; UFW UFW }
func PlanSSH(ctx context.Context, sys host.System, in engine.Input, s SSH) ([]engine.Change, error)
func PlanMigration(ctx context.Context, sys host.System, in engine.Input, prefix, configFile string) ([]engine.Change, error)
func PlanUFWEnable(ctx context.Context, sys host.System, in engine.Input, prefix string) ([]engine.Change, error)
type Cleanup struct { Drop []uint16; Firewall [][]string }
func CleanupOf(j revert.Journal) Cleanup
func PlanCleanup(ctx context.Context, sys host.System, in engine.Input, prefix string, c Cleanup) ([]engine.Change, error)

func Sudoers(id string, p sudoers.Plan, summary string) (engine.Change, error)
func DNS(id string, p dns.Plan, summary string) (engine.Change, error)
                                                                      // next/s2-control: findings, a created file's diff, Ran.Undone and Failure
type Failure struct { Text, Next string; Err error }                  // a step's failure in the user's words (next/s2-control)
func DNSFinding(s dns.State, f dns.Finding) engine.Finding            // DNS_..., in a few words (next/s2-control)
const CodeConfigChanged = "CONFIG_CHANGED"; const ResourceConfig = "hardn.yml" // CodeCommentsLost removed by next/s2-config-edit
func Changed(path string, cause error) *Refusal // CONFIG_CHANGED's report
var ErrConfigChanged error
func PlanSave(ctx context.Context, sys host.System, id string, cfg *config.Config, settings []string, after ...string) ([]engine.Change, error) // next/s2-control; a slice, and settings, since next/s2-config-edit
func Alpine(id string, p alpine.Plan, summary string) (engine.Change, error)
func PVEKeys(id string, px detect.Proxmox, p pve.KeysPlan, summary string) (engine.Change, error)

package engine  // added: Input.Session revert.Session; Change.Resources []string; Change.Findings []Finding
                //   var ErrConflict error
                //   func ConfirmTimeout(flag *time.Duration, cfg *config.Config) (time.Duration, error) // Options.Timeout
package reload  // added: func CheckFree(ctx context.Context, sys host.System, p Plan) error
package migrate // added: Options.Taken map[string]backup.Entry
package config  // added: Config.ConfirmTimeout string `yaml:"confirmTimeout,omitempty"`
```

The ids of a planner's changes are `<prefix>/firewall`, `/files`, `/reload` and `/verify`. A unit change's id is the caller's.

## 7. Not in this unit; inputs to later units

- **For the maintainer (open):**
  - who plans the clean-up after a confirmation, and when (engine.md §7: (a) the next run, (b) `hardn confirm`, (c) the session that applied the change). The change kind serves all three (§2.9). An input to that decision: `CleanupOf` reads `confirmed.json`, which keeps only the last confirmed change, so (a) loses the held port once another lockout-risk change is confirmed first, unless the pending clean-up is kept somewhere of its own (§2.9). *Decided: (b), [P-40](../refactor/proposals.md#p-40) (2026-10-19); built by `next/p3-confirm-cleanup` ([confirm-cleanup.md](confirm-cleanup.md)).*
  - the support gate's rule for best-effort releases (engine.md §7). *Decided: changed, with a warning in the plan, [P-41](../refactor/proposals.md#p-41) (2026-10-19); built by `next/p3-confirm-cleanup` (`PLATFORM_BEST_EFFORT`, [confirm-cleanup.md](confirm-cleanup.md) §2.11).*
- **Slice 6 (SSH) and Run All:**
  - the controls: which settings are asked for, from which keys (`sshPort`, `permitRootLogin` with `Config.RootLogin()`, the password settings), and `config.prohibitPasswordApplied` flipped with them (§2.19);
  - the combination rule the engine now enforces (§2.22): every SSH setting and ufw's enable through one `PlanSSH`; the migration and the clean-up each in a plan of its own; one plan per unit's changes;
  - brief 2.3 step 6 (key login confirmed before password or root login goes), which the engine leaves to the controls (engine.md §7): one plan is one confirm-or-revert cycle, so the key's change and the password's must be separate plans. No change kind here can tell them apart, and no control yet needs a prerequisite across plans, so the engine does not gain one;
  - a package install (ufw itself) before a lockout-risk change goes in a plan of its own (engine.md §7);
  - hardn's block at the top of an `sshd_config` without an Include, and P-38.
- **`p3-output`:**
  - the codes: `ErrListenConfNotInEffect` (`SSH_LISTEN_CONF_NOT_IN_EFFECT`) and `ErrNoRunDir` (`SSHD_ENV_NO_RUNDIR`), which a `Refusal` matches by `errors.Is`, so the registry keeps one mechanism; `ErrNoWriteStrategy`, `ErrMigrateFirst`, `ErrNoNewListener`, `ErrNotInEffect`, `ErrChanged`, `ErrProxmoxRoot`, `engine.ErrConflict`, and `reload.ErrInUse` from `CheckFree`. **Precedence:** a `Refusal` wraps the primitive's refusal (`reload.ErrRestoredListeners`, sshd's `*EnvError`), so one error matches two entries; the registry matches the steps sentinels first, as the more specific (for `/run/sshd` both give `SSHD_ENV_NO_RUNDIR`);
  - `Refusal.Why` and `Fix` shown as the report;
  - the planners' findings, now on `engine.Change.Findings` (§2.23): the renderer, and the codes in the registry;
  - the `--confirm-timeout` flag, through `engine.ConfirmTimeout` (§2.17).
- **The user-management slice:** `pve.CheckAccount` before any password lock (§2.14).
- **Open in the README, not this unit's:** `MkdirAll`, `RemoveAll` and `Rename` unguarded ([atomic.md](../safety/atomic.md) §7).
- **Size:** about 1,970 lines of non-test Go in `pkg/engine/steps` (about 460 of them whole-line comments, and about 200 copied from p2drive, §2.20), 250 in the engine, `reload`, `migrate` and `pkg/config`, and about 1,960 lines of tests (`git diff --numstat next/p3-engine`), against the plan's guideline of about 500 changed lines. The unit is eight change kinds over eleven Phase 2 primitives, each refusal with its test, and a parity harness that runs every p2drive scenario beside the engine. The review's fixes added about 400 lines of code (the plan-time checks of §2.1, §2.12 and §2.14, the conflict check, the findings) and 700 of tests.
