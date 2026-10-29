# The engine in `cmd/hardn`: `-g` and the DNS screen (slice 2, unit (d))

> **Unit:** refactor plan slice 2 (DNS, the pilot), unit (d), `next/s2-replace`: "Legacy path replaced" (plan §2, "Phases 3-5 together", §3.22). The maintainer, 2026-10-22: "Replace in unit (d): withdraw HARDN_UI=classic from §3.14, route menu item 3 and -g to the new control, and delete the legacy DNS screen after (b2), with the shared service going in slice 8"; and for `-g`, 2026-10-23: "apply without asking, -n -g shows the plan and writes nothing, and no udhcpc flag (the warning only)", a host every plan refuses exiting non-zero (V25's fix, decided: Phase 3's exit codes). Inputs: [screen-dns.md](screen-dns.md) §7, [parity-dns.md](parity-dns.md) §7, [engine.md](engine.md) §7's wiring item.
> **Status:** built and tested on macOS, in process: `cmd/hardn` through its seams on the recording fake (`hostfake`) with a model of systemd-resolved, the root command driven as the binary runs it; `pkg/menu`'s item with the screen stubbed. No hardn binary was run and nothing real was read, run or written. Nothing has run on Linux or as root: every host claim is **written, not run (gate 5)** (§4).
> **Amended by `next/s2-menu-live`** ([menu-live.md](menu-live.md)): the main menu has a second hook, `MainMenu.SetStatus`, set by `withStatus` on the same line 190 of `main.go` (its line count kept, decision 7), which draws the host block, risk level and status rows through `pkg/screens/mainmenu` on `statusHost`, a `host.Real` with no run log (the status only reads, on every pass), with `engineDetect`'s facts and `stdoutEnv()`. `engineHost`, its run log and `engineOptions` are unchanged; `pkg/menu` still builds no engine (§7: a second hook, not a map, since the status is no menu item).
> **Amended by `next/s3-replace`** (slice 3, Backup, unit (d); [screen-backup.md](screen-backup.md) §7): the main menu's 5 runs the Backup screen through a third hook, `MainMenu.SetBackup`, set by `withBackupScreen` on the same line of `main.go` (now 202), with the options of §2.1 assembled at each visit (§2.8). No flag routes to Backup. `engineOptions`, `newEngine`, the seams and `-g` are unchanged.

## 1. Why

`cmd/hardn` built no `engine.Options` before this unit: outside the tests only `hardn confirm`'s clean-up reached the engine, through `pkg/engine/confirm`. This is the first unit in which the hardn binary reaches the plan/apply engine from its own flags and menu, so the options are assembled once, here, for the DNS pilot and for every slice's unit (d) after it.

## 2. What is built

### 2.1 The options, assembled once (`engineOptions`, `cmd/hardn/engine.go`)

| Option | From | Note |
|---|---|---|
| `Revert.Log` | `revertFileLog{}`: hardn's log, `/var/log/hardn.log`, alone | as `hardn confirm`'s clean-up has it: the console shows the report or the screen. **Settled:** this `Log` (§8, decision 2) |
| `Revert.Now` | `revertNow` | as `hardn confirm` and `hardn rollback` |
| `Revert.Plans` | `revertPlans` (`plans.Registry()`) | **Settled: needed.** A DNS change journals its undo as the `dns` unit's kept plan (`revert.OpPlan`), which `revert.Record` refuses without its rollback; `engine.New` would fill in the same registry, and it is set so that the rollback here and a later `hardn rollback <id>` undo with one registry |
| `Revert.Hardn` | `revertExecutable()` (`os.Executable`, resolved), as `hardn rollback --boot` | **Settled: not needed for DNS**, which arms no timer (brief 2.4): the engine reads it only for a lockout-risk plan (`pkg/engine/plan.go:233-235`, `boot.go:46`). Set for the next slices' lockout-risk changes; left empty when this hardn's path cannot be read, so that such a plan is refused ("the revert timer") rather than armed to run nothing |
| `Session` | `revert.ReadSession(ctx, sys, revertGetenv, revertPID())`, as `runConfirm` reads it | journalled with the change, and required of a lockout-risk plan, which the engine refuses when it is not established. A read that fails is logged to the file and its partial session kept |
| `Backups` | `cfg.BackupSettings()`: `backupPath`, `enableBackups` | the engine's backups are rollback-required, taken whatever `enableBackups` says (§3.16) |
| `PID` | `revertPID()` | the run lock's holder line |
| `Detect`, `Hold` | the engine's own: `detect.Detect`; `nil`, which is `hold.Signals` over `Log` | through seams (`engineDetect`, `engineHold`) for the tests |
| `Progress` | the caller's | a screen's flow, per change (`newEngine`); none for `-g` |
| `Timeout`, `Margin`, `JournalTimeout`, `Revert.Dir`, `RunDir` | the engine's defaults | no flag or key changes them (§3.14's `--confirm-timeout` is a later slice's) |

`newEngine(sys, o)` is a screen's `NewEngine`: a copy of `o` with the flow's `Progress`, one engine per change.

### 2.2 The host and its logs

`engineHost` is `host.NewReal(host.Options{RunLog: revertFileLog{}})`: every command's argv, output and status goes to hardn's log alone, as the clean-up's does. Both paths build it once per run (per visit of the screen).

### 2.3 The main menu's 3 (`runDNSScreen`, `cmd/hardn/dns.go`)

- **How `pkg/menu` reaches it:** a hook `cmd/hardn` sets, `MainMenu.SetDNS(func() error)`, from `withDNSScreen` at `main.go:190`. `pkg/menu` builds no engine and imports neither the engine nor the screens; it imports `pkg/ui/nav` for `nav.ErrQuit` alone.
- **What it runs:** `pkg/screens/dns.Run` with `prompt.Stdin()` (the one line reader every legacy prompt shares), stdout's `term.Env` (`stdoutEnv`), the host of §2.2, `detect.Detect` once per visit, the `*config.Config` `cmd/hardn` loaded (the one the main menu holds), `NewEngine` over §2.1's options, `ApplyContext` `engineSignals` (§2.6), `DryRun` read from that configuration as the item is chosen (as the legacy menus read it, so the Dry-Run item and `-n` apply), `LogPath` the legacy log, `Now` revert's clock. The screen starts from a cleared screen, as the legacy DNS menu's `PrintHeader` cleared it.
- **What it returns:** nil, back to the main menu, which draws itself again with the list as saved (the screen adopts each save into that configuration); `nav.ErrQuit` ends hardn as the main menu's q does (it clears the screen and returns, exit status 0); the end of the input exits 130, Ctrl-C at a prompt ends hardn by SIGINT, and any other error is printed and exits 1, as at every legacy prompt (`endHardn`, factored out of `readLine` unchanged).
- **The audit log:** not written. `cmd/hardn` has no audit writer yet (its first is `next/p3-startup`'s, [output.md](output.md) §7), so `Options.Ran` is nil.

### 2.4 `-g` (`configureDNS`, `cmd/hardn/dns.go`)

In its place in the flag sequence, after the root check and the configuration's load, as before: `dnsctl.Controls(cfg)` (hardn.yml's own list, so nothing is saved) planned, then applied without a question, through the engine with §2.1's options (`runControls`, a flag's run of its controls, which the next slices' flags reuse, §7); the report is `output.Human`'s on stdout. `StopUDHCPC` is false: on Alpine, `DNS_UDHCPC` is reported and `udhcpc.conf` left as it is. A refused plan is reported with its code and how to go on (`resolvconf manages DNS here (DNS_MANAGED)`, "To go on: …") and never applied; a failed apply is rolled back and reported so. The legacy lines ("Failed to configure DNS: …", "DNS configured successfully", and after `-g` alone "Script completed selected hardening operations.") are gone: the report is the whole of it.

### 2.5 Exit statuses, `-n -g`, and the other flags

- **`-g`'s status** is `output.ExitCode`'s (Phase 3's, [reference](../reference/output.md) §3): 0 applied or nothing to change; 1 refused, failed or rolled back; 130 interrupted.
- **`-n -g`** is no longer refused: `-g` left `operationFlags`, the list `RefuseDryRunOperations` refuses, so its dry run is the plan alone (`mode` `plan`), shown with output's dry-run footer. Nothing is written but the engine's run directory, `/run/hardn/<id>`, which the plan makes and removes; on a host with no `/run/hardn` the plan makes that too and leaves it, empty (mode 0700, on tmpfs, so gone at the next boot), as every `Engine.Plan` does (`MkdirAll`, then `removeRunDir` of `<id>` alone). Its status: **3 with changes to make**, 0 with none, 1 refused (§8, decision 4). `-n` with any other operation flag is refused as before (exit status 2, before the root check), the message naming those flags only.
- **With other flags in one invocation** (origin/main logged each failure and went on, V25): the flags run in their order as before, each reporting as before; `-g`'s status, when not 0, is hardn's once the flags after it (`-p`, `-e`) have run (`defer exit(code)` in its block, `main.go:398-402`). An interrupted `-g` ends hardn at once with 130, the flags after it not run. `-e`'s own failure still exits 1 at once. `-r` with `-g` runs `-r` alone, as before (its block returns first). **With another legacy flag** (`-w -g`, `-u -g`, ...), the legacy completion line, "Script completed selected hardening operations.", still prints at the end whatever `-g`'s status, under its refusal's report too: accepted, as the legacy flags report as before (origin/main printed it after any flag's logged failure, V25), and the exit status is the run's verdict (§8, decision 11).

### 2.6 Signals

Both paths register `signal.NotifyContext` for SIGINT, SIGTERM and SIGHUP (`engineSignals`, over `engineSignalSet`) **around each apply alone**, as engine.md §2.14 asks of the engine's caller: a signal during an apply cancels it, and the engine rolls the change back here with SIGHUP and SIGINT held, rather than Go's default ending hardn with the change half applied and journalled.

- **`-g`** (`runControls`) registers once its plan is made, around the apply and its report, and ends hardn with 130 when interrupted.
- **The DNS screen** registers for each change's apply through `dnsscreen.Options.ApplyContext` (the screen's flow engine applies under it, `planning.Apply`), stopped once the apply returns; the screen then goes on, reading the host again.
- **Registered again for each apply.** The hold around an in-process rollback (`hold.Signals`) ignores SIGHUP and SIGINT, which ends a registration made before it (`signal.Ignore`, engine.md §2.14), and its release gives them Go's default. The first build registered once for the screen's whole visit, so after one change failed and was rolled back, a Ctrl-C or a hangup during a later change's apply in the same visit ended hardn with that change half applied (a review found it).
- **Outside an apply** (at a prompt, while the screen reads the host, during a plan) nothing of `cmd/hardn`'s is registered, so the signals act as they always have: Ctrl-C at a prompt ends hardn by SIGINT (the prompt's own interception, `prompt.ErrInterrupted`), SIGTERM and a hangup by Go's default. The first build caught them for the whole visit, and the prompt watches no context, so a SIGTERM at a prompt was swallowed until the user typed, and the visit then ended with "context canceled", exit status 1 (a review found it). A plan interrupted so has written nothing but its run directory (§2.5).

*Chosen over* registering for the visit and registering again in a hold of `cmd/hardn`'s own (`Options.Hold` wrapping `hold.Signals`): that fixes the first defect from `cmd/hardn` alone, but not the second, as a signal the visit catches at a prompt cannot end hardn there. The cost is one field in `pkg/screens/dns.Options` (§6).

### 2.7 Deleted, kept, moved

- **Deleted**, each with its last caller: `pkg/menu/dns_menu.go` (`git grep -nE 'NewDNSMenu|DNSMenu\b|getCurrentDnsSettings' -- '*.go'`: no match); `MenuManager.ConfigureDNS` (its one caller was `dns_menu.go:150`; `git grep -n '\.ConfigureDNS(' -- '*.go'`: every caller left is `DNSManager`'s or `DNSService`'s); `cmd/hardn`'s `dnsManager` (`git grep -n dnsManager -- cmd/`: no match; `-g` was its caller); `pkg/characterization`'s `TestMenuDNSGoldens`, which mirrored the deleted handlers. `MenuManager`'s `dnsManager` field stays (its constructor's callers set it; slice 8 removes it with the service).
- **Kept** until slice 8, for `hardn -r` (`main.go:254`) and Run All's live run (`pkg/menu/run_all_menu.go:268`), both through `SecurityManager.HardenSystem`: the legacy DNS service of [screen-dns.md](screen-dns.md) §7's list, byte for byte, with `pkg/characterization`'s `ops/dns_*`.
- **Moved:** the record `TestParity` reads, frozen under `pkg/screens/dns/testdata/legacy` before its sources changed ([parity-dns.md](parity-dns.md) §7).

### 2.8 The main menu's 5 (`runBackupScreen`, `cmd/hardn/backup.go`; slice 3, unit (d), `next/s3-replace`)

- **How `pkg/menu` reaches it:** a hook `cmd/hardn` sets, `MainMenu.SetBackup(func() error)`, from `withBackupScreen` on `main.go:202`, beside `withDNSScreen` and `withStatus` (the line count kept, §8 decision 7). The DNS and Backup items share one action in `pkg/menu`, `showScreen`: the screen from a cleared screen, its return mapped, a panic with no screen set ("menu: the Backup item has no screen (SetBackup)", internal, as DNS's).
- **What it runs:** `pkg/screens/backup.Run` with what §2.3 gives the DNS screen but `Facts` (the screen reads none): `engineInput()` and `stdoutEnv()`; `engineHost()`; the `*config.Config` the main menu holds, which the screen adopts each save into; `NewEngine` over `engineOptions(ctx, sys, c)`; `ApplyContext` `engineSignals`; `DryRun` read from that configuration as the item is chosen; `LogPath` the legacy log; `Now` revert's clock; `Ran` nil.
- **The options per visit:** both hooks call `engineOptions` on each visit, from the configuration as it then is, so `Backups` is `backupPath` and `enableBackups` as the last save left them: a path change through Backup reaches the DNS screen's next change (`TestBackupThenDNS`: DNS with a change, Backup's path change, DNS with a change, through both hooks as `main` sets them). DNS's hook already did (read: `runDNSScreen`); nothing there changed. *Since the review's fixes (b68e6ea), `withDNSScreen` is generic over a menu with `SetDNS`, as `withBackupScreen` is, so that the test takes its hook (decision 13).* Within one visit of the Backup screen the engine's `Backups` are as the visit began, and Backup's own changes are planned under the settings they save (decision 7 (a)), so a path change then a turn backs up under the new path.
- **What it returns:** as §2.3: nil back to the main menu; `nav.ErrQuit` ends hardn as the main menu's q; the end of the input 130, Ctrl-C at a prompt SIGINT, any other error printed and 1 (`endHardn`).
- **Under plan only** (decision 10, gallery-backup.md §5): 5 is not refused; the screen opens with `DryRun` true, Verify and Backup Runs read, each change stops at its plan. `nav.Option.Refusal` stays on User Management (1), and Firewall's profile add and remove keep theirs.
- **Signals:** as §2.6, each change's apply under `engineSignals` through `backupscreen.Options.ApplyContext` (`TestBackupScreenSignals`, 29f7e51).
- **Deleted, kept, moved:** [parity-backup.md](parity-backup.md) §8: the legacy Backup screen and its layers, each with its grep; `TestLegacyMenuBackup` and `TestLegacyBackupFile` retired; their 22 goldens frozen under `pkg/screens/backup/testdata/legacy`.

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| The engine reads `Revert.Hardn` only for a lockout-risk plan; `New` fills a nil `Revert.Plans` with `plans.Registry()`; `Record` refuses an `OpPlan` step whose unit has no rollback | `pkg/engine/plan.go:143, 232-243`, `boot.go:46`, `engine.go:153-155`; `pkg/host/revert` `checkPlans` (code) | §2.1 |
| The DNS change's undo is the `dns` unit's `OpPlan` | `pkg/engine/steps/plans.go`, `pkg/host/revert/plans` (code) | §2.1 |
| `detect.Detect` fails only when its context is done | `pkg/host/detect` `Detect`'s doc | §2.3 |
| A change without a timer that did not finish is named by the next run's notice, with `sudo hardn rollback <id>` | `pkg/host/revert` `Notices`, `NoticeInterrupted` (code) | §2.6 (what a signal outside a registration would have left) |
| `prompt.Reraise` ends hardn by SIGINT despite a `signal.Notify` it holds | `pkg/ui/prompt` `TestInterruptChild` (observed on macOS) | §2.6 |
| `hold.Signals`' `signal.Ignore` ends an earlier `NotifyContext`'s registration of SIGINT and SIGHUP | engine.md §2.14; `pkg/engine/hold`; **observed** by `TestDNSScreenSignals` with the registration made for the visit (a mutation): the test process killed by the signal during the second apply; *since `next/s3-replace`*, by `TestBackupScreenSignals` too, by SIGINT and SIGHUP (SIGTERM's registration survives the hold; the test fails on its count) | §2.6 |
| A prompt intercepts `os.Interrupt` alone, during a read, and watches no context | `pkg/ui/prompt` `intercept` (code) | §2.6 |
| `Engine.Plan` makes `/run/hardn/<id>` with `MkdirAll` and removes `<id>` alone | `pkg/engine/plan.go:118`, `removeRunDir` (code) | §2.5 |
| The characterization goldens cite `cmd/hardn/main.go`'s lines to 462 (`:123`, `:151`, `:158`, `:230-260`, `:234-250`, `:283-289`, `:365-383`, `:386-392`, `:409-416`, `:426-461`, `:440`) | `pkg/characterization/operations_test.go` (strings) | §8, decision 7 |

## 4. Written, not run (gate 5)

Run in part on 2026-10-23 (§4.1); the rest has not run. For the maintainer's VM runs, as root, on Debian 13, Ubuntu 26.04 (and 24.04), Alpine 3.24 (and 3.23) and Proxmox VE 9 by hand, after each `echo $?` and name resolution checked (`getent hosts` of a public name, and `resolvectl query` on systemd-resolved):

- `sudo hardn -g` on each stock host: `/etc/resolv.conf` (Debian, Alpine, Proxmox VE) or `/etc/systemd/resolved.conf.d/zz-hardn.conf` (Ubuntu) as the report says, the findings (dhcpcd, udhcpc with `udhcpc.conf` untouched, Proxmox VE's settings, eth0's own server), exit status 0; again, "Nothing needs changing.", 0; `/var/log/hardn.log` holding the commands and the engine's lines, the console the report alone.
- `sudo hardn -n -g`: the plan and its footer, nothing written (`/etc` unchanged; no plan directory left under `/run/hardn`, which may itself be left, empty, where it was not before), exit status 3; on a host as asked, 0.
- Refused, nothing written, exit status 1: a NetworkManager desktop, resolvconf's link, an LXC container, `nameservers: []`.
- A reload made to fail (resolved's unit masked, or `systemctl` made to exit non-zero), rolled back, exit status 1; Ctrl-C during the reload, rolled back, 130; an SSH session dropped during it (SIGHUP), rolled back. Ctrl-C during the plan (before the first write): hardn ends by SIGINT, nothing written.
- `sudo hardn -g -e`: both run; the exit status `-g`'s. `sudo hardn -n -g -d`: refused, 2, naming `-d`.
- The main menu's 3 as [screen-dns.md](screen-dns.md) §4 lists, then Return: the main menu again, and 3 again showing the list as saved; q in the screen ends hardn (0); Ctrl-D at its prompt ends it with 130; `sudo hardn -n` then 3, a dry run.
- Signals on the screen (§2.6), on Ubuntu 26.04: a change whose reload is made to fail, rolled back, then in the same visit Ctrl-C during the next change's reload, and again with the SSH session dropped: rolled back each time, the drop-in as before; after Ctrl-C the screen goes on, after the hangup hardn ends at its next prompt (its input gone; *not under sudo-rs, Ubuntu 26.04's sudo, which keeps hardn's input open: the Backup screen's run, [screen-backup.md](screen-backup.md) §4.2, found hardn at its prompt after the rollback until killed*); `kill -TERM` and `kill -HUP` of hardn while the screen waits at a prompt: hardn ends at once, nothing changed, as at the legacy menu.
- `sudo hardn -r` and Run All's live run on Debian 13: DNS as origin/main configures it (the legacy service, unchanged).
- *Since `next/s3-replace`:* the main menu's 5 as [screen-backup.md](screen-backup.md) §4.1 lists, plan only included; *run on 2026-10-28 on Debian 13, Ubuntu 26.04 and Alpine 3.23, screen-backup.md §4.2: passed where it could run; after a hangup under Ubuntu's sudo-rs, hardn did not end (below).*

### 4.1 Run 2026-10-23: Debian 13, Ubuntu 26.04, Alpine 3.23

As root on the maintainer's three VMs, hardn built from `a343c19` (sha256 `787d6933…`), hardn.yml listing the DHCP server's own address and 1.1.1.1. Each host was put back afterwards and checked byte for byte against copies taken first.

Passed:
- **`-n -g`** on Ubuntu 26.04 (the stub link, systemd-resolved) and Alpine 3.23 (a file dhcpcd writes): the plan, the dry-run footer and the findings (`DNS_PER_LINK`; `DNS_DHCP_CLIENT`), exit status 3; every file's sha256 the same after. On Alpine an empty `/run/hardn` was left, as §2.5 says. On a host as asked, "Nothing needs changing.", 0.
- **`-g`** on Ubuntu, Debian and Alpine: applied, exit status 0. On Ubuntu and Debian the drop-in `zz-hardn.conf`, `resolvectl dns` showing the list as global servers, names resolving. On Alpine `/etc/resolv.conf` with the list, dhcpcd's comment and `domain` lines kept, a backup under `/var/backups/hardn`. Again: "Nothing needs changing.", 0.
- **Refused, nothing written, exit status 1:** `nameservers: []` (`DNS_NO_NAMESERVERS`) on Ubuntu and Alpine. Debian as found: systemd-resolved running with `/etc/resolv.conf` a plain file (`DNS_MANAGED`, "To go on: link /etc/resolv.conf to /run/systemd/resolve/stub-resolv.conf"); with that done, `-n -g` and `-g` passed there as on Ubuntu.
- **The main menu's 3** on all three, over a terminal (scripted with expect):
  - Add: "Invalid IP address format" and "Nameserver … is already configured" as inline results (⨳), then 9.9.9.9 applied and saved.
  - Remove 9.9.9.9: applied and saved. The second save was not refused `CONFIG_CHANGED`.
  - Cloudflare declined: "Operation Cancelled", nothing written.
  - Return to the main menu, 3 again showing the list as saved, then q: exit status 0.
  - hardn.yml's comment kept through both saves; on Debian the maintainer's file kept its quoting and every other line.
  - On Alpine every change asked udhcpc's question (answered n; `udhcpc.conf` unchanged).
- **`-r` on Debian** (`HARDN_CONFIG` with only the SSH and DNS steps' settings): the legacy service as origin/main, exit status 0. `/etc/systemd/resolved.conf` overwritten with the list and `Domains=lan`, systemd-resolved restarted (V19); `/etc/ssh/sshd_config.d/hardn.conf` and `/etc/sudoers.d/alice` written; `sshd -t` passed and a new login worked. OpenSSH 10.0p2 accepted the drop-in's `Protocol 2` ([vmtest.md](../safety/vmtest.md) §3's row, for that line).

Found:
- **udhcpc reported on a host where dhcpcd is the client.** On Alpine 3.23 with dhcpcd installed, ifupdown-ng runs dhcpcd for `iface eth0 inet dhcp` and no udhcpc runs, yet every plan says "udhcpc rewrites /etc/resolv.conf on each lease" (`DNS_UDHCPC`) and every change asks "Stop udhcpc rewriting /etc/resolv.conf?". The check reads `udhcpc.conf` alone ([dns.md](../safety/dns.md) §3's Alpine row). A claim about a client not in use: to fix. *Fixed in `next/s2-udhcpc`* (the maintainer's decision, 2026-10-23: "report DNS_UDHCPC and ask udhcpc's question only when udhcpc is the DHCP client in use"): `dns.Read` reads which client ifupdown-ng runs ([dns.md](../safety/dns.md) §2.4, §3), `DNS_UDHCPC` and the question only where it is udhcpc, `DNS_UDHCPC_UNKNOWN` where that is not established; **rerun 2026-10-23 on the Alpine 3.23 VM, passed** (§4.1, "udhcpc rerun" below).
- **With no hardn.yml, hardn stops at the legacy "No configuration file found. Would you like to create one? [Y/n]"** before `-g` runs; with no terminal it waited. When that session ended, a default `/etc/hardn/hardn.yml` was left (`permitRootLogin: false`, which the next run warns is deprecated); whether the session's end or the signal created it is not established. Legacy start-up, not this unit's, now in front of `-g`.
- **`-r` needs a username** ("Please specify a username with -u flag or in the configuration file."), as on origin/main.

udhcpc rerun (2026-10-23, `next/s2-udhcpc` at `eb704e5`, sha256 `4eea4cc8…`), on the Alpine 3.23 VM, `hardn -n -g` in each state of `/etc/network/interfaces`, put back after:
- **As found** (dhcpcd installed): `DNS_DHCP_CLIENT` alone; the screen's Add asked no udhcpc question.
- **`dhcp-program udhcpc` on `eth0`:** "udhcpc rewrites /etc/resolv.conf on each lease" (`DNS_UDHCPC`) beside dhcpcd's; the screen asked the question.
- **A non-auto `eth1` with `dhcp-program udhcpc`:** "udhcpc rewrites /etc/resolv.conf if it is the DHCP client" (`DNS_UDHCPC_UNKNOWN`, from `/etc/network/interfaces`).
- **An `/etc/conf.d/networking` file:** the same, from that file.
- **`udhcpc-opts -s /bin/true`:** "udhcpc may rewrite /etc/resolv.conf on each lease".
- **Checked first:** the executor's rule matched with its tabs, no conf.d or pid file, `ifquery --list -a` gave `lo` and `eth0`.

Not run: Proxmox VE 9, Ubuntu 24.04 and Alpine 3.24 (no VMs); a NetworkManager desktop, resolvconf's link, an LXC container; a failed reload and its rollback; Ctrl-C and a hangup during an apply, and the hold's release; `-g -e`, `-n -g -d`; `hardn -n` then 3; Run All's live run; Ctrl-D at the screen. Name resolution on the Alpine VM could not be checked: it reaches no DNS server, its router not answering, before the change as after.

## 5. Words

No new words in the report or the screens: `-g`'s report is `output.Human`'s, the screen's are [screen-dns.md](screen-dns.md) §5's, both **proposed** where they say so. What changes:

| Where | Words | Source |
|---|---|---|
| `-n` with other operation flags | the refusal no longer names `-g/--configure-dns` | `pkg/cmd`'s, generated |
| `-g`, before | "Failed to configure DNS: …", "DNS configured successfully", "Script completed selected hardening operations." | origin/main's; gone after `-g`, the report in their place |
| The DNS item with no screen set (a programming error: `cmd/hardn` sets it on the one main menu it builds) | a panic, "menu: the DNS item has no screen (SetDNS)"; no code, as it is no outcome of a run | internal |
| `-g` or a screen's change interrupted during its apply | `output.Human`'s and the flow's report of `CANCELLED`, the error's own text ("… resolvectl --no-pager dns: context canceled (CANCELLED)"), then the change rolled back | Phase 3's rendering, unchanged; **proposed** in its place, for every report: "Interrupted: the change was rolled back." (§11). A plan is no longer reported interrupted: before the apply, the signals end hardn (§2.6) |
| hardn's log, a session not read | revert's own error ("revert: session: …") | revert's |
| The Backup item with no screen set (`next/s3-replace`) | a panic, "menu: the Backup item has no screen (SetBackup)" | internal, as DNS's |
| docs/configuration.md, docs/releases/unreleased.md (`next/s3-replace`) | the Backup screen's section and notes; plan only's "**Backup opens.** Each of its changes stops at its plan."; from the review's fixes, "With backups on," before what Backup and Verify used to write, "**Backups off still keeps what a rollback needs.** Each change still copies the files it changes to the backup path" (", and says so." in the notes), and the notes' "**Check `backupPath` in older files.** While it holds such a path, changes from the DNS and Backup screens and `-g` are refused. Change it with Backup's item 2." | **proposed**; no word of the screen changed (screen-backup.md §5, §11) |

## 6. API

```go
package main // cmd/hardn, added
func engineOptions(ctx context.Context, sys host.System, c *config.Config) engine.Options
func newEngine(sys host.System, o engine.Options) func(progress func(engine.Event)) *engine.Engine
func runControls(c *config.Config, controls []engine.Control) int // a flag's controls planned, applied, reported; the exit status
func configureDNS(c *config.Config) int                            // -g: runControls(c, dnsctl.Controls(c))
func withDNSScreen[M dnsMenu](m M, c *config.Config) M          // dnsMenu: SetDNS; generic since next/s3-replace
func runDNSScreen(ctx context.Context, c *config.Config) error // the DNS item
func withBackupScreen[M backupMenu](m M, c *config.Config) M    // next/s3-replace; backupMenu: SetBackup
func runBackupScreen(ctx context.Context, c *config.Config) error // the Backup item, next/s3-replace
var engineSignalSet = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
var engineHost, engineDetect, engineHold, engineSignals, engineInput, engineStdout // seams

package menu // pkg/menu, added
func (m *MainMenu) SetDNS(run func() error) // the DNS item panics without it
func (m *MainMenu) SetBackup(run func() error) // the Backup item, next/s3-replace; panics without it

package dns // pkg/screens/dns, added to Options
ApplyContext func(ctx context.Context) (context.Context, context.CancelFunc) // each change's apply's context and its stop; nil: Run's
```

No new flag, environment variable, configuration key, error code or module. `pkg/menu`'s `endHardn` is `readLine`'s abort handling, unchanged, made a function.

## 7. For the next slices' (d) units

- **A flag:** `runControls(cfg, controls)`, as `configureDNS` calls it: the options, the plan, the apply without a question unless `-n` or a refusal, the report, the exit status, the signals around the apply. A flag that should ask first is not this function (§3.11's `--yes` is not approved).
- **A screen:** call `engineOptions(ctx, sys, cfg)` with `engineHost()` and take `newEngine(sys, o)` as its `NewEngine`. A lockout-risk change needs `Hardn` and `Session`, which are there; `--confirm-timeout` (§3.14) would set `Timeout`.
- **Signals:** `engineSignals` around each apply alone, never a visit (§2.6): a flag has it from `runControls`; a screen takes it as its apply's context, as the DNS screen's `ApplyContext` (a screen on `output.FlowEngine` applies under it as `pkg/screens/dns`'s `planning.Apply` does).
- **The menu item:** a hook as `SetDNS`. When a second item needs one, generalise it (a map of item to screen, or one `SetScreens`), rather than adding a setter per item. *Since `next/s3-replace`, Backup's is a second setter, `SetBackup`, the two items sharing `showScreen` (§8, decision 12); the next slices' items (Firewall, User Management) may generalise it then, three setters being where a map pays.*
- **`-n` with the slice's flag:** decided per flag by the maintainer; to show the plan, take the flag out of `operationFlags`.
- **`main.go`'s lines:** the characterization goldens cite them to 462 (§3). Keep them (as here), or regenerate those goldens deliberately in a commit of their own, each changed line explained.
- **The legacy goldens `TestParity` reads:** freeze them before their sources change, as [parity-dns.md](parity-dns.md) §7 and §8 say. *Backup's (`next/s3-replace`) add `TestLegacyRecord` in the screen's package, each frozen file's SHA-256, as slice 2b's `pkg/menu` record has it ([parity-backup.md](parity-backup.md) §8).*
- *Since `next/s3-replace`, what they have:* `withBackupScreen`/`runBackupScreen` as the model of a screen's hook (`backup.go`, 62 lines); `engineOptions` assembled per visit, which a screen that changes the backup settings needs; `showScreen` in `pkg/menu`; a lifted plan-only refusal (`nav.Option.Refusal` removed from the item) for when User Management (slice 5) and Firewall's profiles (slice 4) go on the flow.

## 8. Decisions of this unit

None is the maintainer's beyond those quoted above; each is for review.

1. **A hook on the main menu** (`SetDNS`), set by `cmd/hardn`, rather than `pkg/menu` assembling the engine (the task's rule) or `cmd/hardn` mapping the screen's return to a quit flag (which would copy `pkg/menu`'s abort handling into `cmd/hardn`). `pkg/menu` imports `pkg/ui/nav` for `nav.ErrQuit` alone.
2. **The log file alone** for the engine's lines and the run log, as `hardn confirm`'s clean-up: the console is the report's or the screen's. *Alternative:* `revertLog`, both, as `hardn rollback`, whose console is the journal's or the null device.
3. **`Plans` and `Hardn` set** (§2.1), the one assembly serving the lockout-risk slices too.
4. **`-n -g` exits 3 when the plan has changes to make**, output.ExitCode's reading of a plan (reference §3: "a check or a plan found changes to make"), the one function for every run. 3 is Phase 3's decided code for a plan with changes (V25's fix), and 0 is kept for a plan with none. The task's "exit 0" was the orchestrator's wording, not the maintainer's decision, so the decided code stands (the orchestrator, 2026-10-23).
5. **`-g`'s status after the flags that follow it**, and at once when interrupted (§2.5).
6. **The legacy "Script completed" line no longer follows `-g` alone**, which would print a success line under a refusal.
7. **`main.go`'s lines to 462 kept**: line 224 is a comment where `dnsManager` was, the `-g` block keeps its 8 lines, `withDNSScreen` wraps line 190, so that no characterization golden's citation goes stale and none changes but the DNS ones.
8. **The DNS screen starts from a cleared screen**, as the legacy DNS menu did, and as every other item's screen starts.
9. **`NotifyContext` around each apply alone**, `-g`'s and each of the DNS screen's changes (§2.6). The first build ran the screen on `context.Background()`, where a signal mid-apply ended hardn half applied; its fix registered for the screen's whole visit, a registration the reviews found lost after one in-process rollback, and catching SIGTERM at a prompt, where hardn then waited for input. Per apply needs the screen to give its apply a context, `dnsscreen.Options.ApplyContext`, the one change outside `cmd/hardn` and `pkg/menu`.
10. **A DNS item chosen with no screen set panics** (a programming error), rather than an error that would need a code no run reports.
11. **The legacy completion line kept after `-g` with another legacy flag** (§2.5).

From `next/s3-replace` (slice 3, unit (d)):

12. **`SetBackup`, a second setter, not a generalised hook** (§7 advised generalising at the second item): the unit's brief named `SetBackup`, as `SetDNS`; the two items share their action (`showScreen`), so a third item, or a map, changes one place. *Alternative:* `SetScreen(key, run)` now, changing `SetDNS`'s callers and `TestDNSItem`.
13. **`withBackupScreen` generic over a menu with `SetBackup`**, as `withStatus` is over its two setters, so that `cmd/hardn`'s tests take the hook as `main` sets it and run it (`TestBackupThenDNS`). *Since the review's fixes (b68e6ea), `withDNSScreen` too, over `SetDNS`: the cross-screen test called `runDNSScreen` directly, so a hook that copied the configuration when set passed every test; `main.go` unchanged.*
14. **No `Facts`** for the Backup screen: its `Options` have none (screen-backup.md §8, decision 1), so `runBackupScreen` detects nothing.

## 9. Tests

- `cmd/hardn`: `TestEngineOptions` (each field to its source; no path, no session), `TestEngineSeamsAreReal`, `TestEngineSignals` (the set; SIGINT, SIGTERM and SIGHUP each sent to the test process, as the kernel delivers it, cancelling `engineSignals`' context: one left out ends the process), `TestDNSScreen` (Add on Debian 13 applied and saved, the configuration adopted and recording the saved bytes, no audit record; a dry run and a decline writing nothing; q; the end of the input; the signals registered once per apply, never while a prompt reads), `TestDNSScreenSignals` (per signal, sent to the test process: a first change rolled back under the engine's own hold, then the signal during the next change's apply in the same visit, rolled back, the screen going on and reading the host), `TestConfigureDNSFlag` (through the root command: Debian 13, Ubuntu 26.04, Alpine 3.24, a host as asked; resolvconf, NetworkManager, a container, no nameservers and a fact not established refused with nothing written, 1; a reload with no effect rolled back, 1; `-n -g` 3, 0 and 1 with nothing written; hardn.yml never saved; no question; nothing through the legacy provider; the signals registered once with an apply, never for a plan alone or refused), `TestConfigureDNSFlagWithOthers` (`-g` then `-e`, refused and applied; Ctrl-C mid-apply, 130, `-e` not run, the plan's commands run with no signal registered; `-n -g -d`), and `TestDryRunRefusesOperations`, `TestDryRunAloneIsNotRefused` (`-n -g` reaches the root check), `TestDeprecationWarning` (now with `-d`). Every test that does not give the engine a host fails if a run reaches it (`runHardn`).
- `pkg/menu`: `TestDNSItem` (each return mapped; the screen cleared first; the configuration the screen changed is the menu's; no screen set, a panic).
- *Since `next/s3-replace`:* `cmd/hardn` `TestBackupScreen` (turning backups off applied, hardn.yml's one line saved, its backup under `/var/backups/hardn`, the configuration adopted; a path change backing up under the new path, nothing under the old; dry runs, a decline and Verify writing nothing; 0, q and the end of the input; the signals registered once per apply, never at a prompt; no audit record) and `TestBackupThenDNS` (through both hooks as `main` sets them: a DNS change, a path change through Backup, then a DNS change backing up under the new path, nothing under the old; the configuration holding both saves). *Corrected by the review's fixes (b68e6ea): as first built, it visited DNS only after the path change, so options assembled once at the first DNS visit passed it, and "DNS's options assembled once fail it" held only for options assembled before the first visit (run).* Mutation-checked since, in the working tree, not committed: DNS's options assembled once when the hook is set, at the first visit or at the first change, and `withDNSScreen` copying the configuration, each fail it. `TestBackupScreenSignals` (29f7e51; per signal, sent to the test process: a first save failing, rolled back under the engine's own hold, then the signal as the next save writes hardn.yml, rolled back, the screen going on; each report naming `/var/log/hardn.log`; mutation-checked: `LogPath` "" fails it, no `ApplyContext` ends the process by each signal, a registration for the visit ends it by SIGINT and SIGHUP and fails SIGTERM's count); `TestDNSScreenSignals` names the log too (161ff6b; `LogPath` "" fails it). **Held by reading alone:** `main.go:202`'s call of the three hooks, as since `next/s2-replace` for DNS's and the status's: no test drives hardn's interactive start (its update check, `ReadKey`, the live status); without it, choosing 5 panics, "menu: the Backup item has no screen (SetBackup)", which §4.1's first check of [screen-backup.md](screen-backup.md) would show. `pkg/menu` `TestBackupItem` (`TestDNSItem`'s cases for 5, one helper), `TestMainMenuKeys` (3 and 5 through their hooks, each from a cleared screen), `TestPlanOnlyRefusals` (5 opening under plan only with `DryRun` true; 1 still refused).
- `pkg/screens/dns`: `TestApplyContext` (a change's apply, and nothing else, under `ApplyContext`'s context, stopped after it; none for a decline or a dry run; cancelled, rolled back, the screen drawn again from a fresh reading, the next change applied).
- `pkg/cmd`: `TestRefuseDryRunOperations`' three-flag case names `-d` in place of `-g`.
- Mutation-checked (`go test -overlay`): without the signals' context, `TestDNSScreenSignals` and the `-g` Ctrl-C case fail; the registration made for the screen's visit, as the first build's fix had it, fails `TestDNSScreen` and ends `TestDNSScreenSignals`' process ("signal: interrupt"); SIGHUP left out of `engineSignals` ends `TestEngineSignals`' ("signal: hangup"); `-g` registering before its plan fails the plan's check; the screen's apply not under `ApplyContext` fails `TestApplyContext`; `-n` applying fails `-n -g`; no deferred exit fails every refusal and failure.

## 10. Goldens

`pkg/characterization`'s `ops/dns_{systemd_resolved,resolvconf,direct,empty_nameservers}/*` (16) change in two places, where `-g` leaving the service changes what they say: "Reached by" (`-r` and Run All's live run through `HardenSystem`'s DNS step; `-g` and the DNS screen on the control), and the modelled section, now `hardn -n -r`, still refused (exit status 2), so V1's FIXED line and the header stay. Every other characterization golden, `pkg/menu`'s `main_menu` and `run_all`, `parity.golden` and parity-dns.md §4 are byte for byte as at e666243 (`git diff e666243 --stat`).

*Since `next/s3-replace`:* no golden of this package's; `pkg/menu`'s testdata unchanged; one gallery golden added, `play/backup-plan-only` ([screen-backup.md](screen-backup.md) §10); the Backup record moved ([parity-backup.md](parity-backup.md) §8).

## 11. Open items

- **An interrupted apply's words** (§5): `CANCELLED` is reported in the error's own text, as Phase 3's renderer reports every cancelled run; "Interrupted: the change was rolled back." is **proposed** for `output.Human` and the flow, a change to every report, so the maintainer's.
- **The audit log** stays unwritten until `next/p3-startup` (§2.3): then `-g`'s run and `Options.Ran` take `output.Append`.
- **The `parity.golden` header** still says origin/main's goldens are "read in place"; kept byte for byte (parity-dns.md §7).
- **The main menu's DNS row** (P-01) is a later unit's.
- *From `next/s3-replace`:* **a setter per item** (`SetBackup` beside `SetDNS`, decision 12) or one generalised hook, for the maintainer; **the gallery's main menu** is Figma's mock, its indicator "Mode Live" under `-sequence dry-run` too, so the gallery shows the Backup screen under plan only but not hardn's plan-only main menu above it; **Backup's `parity.golden` header** also still says "read in place", kept byte for byte; **the Backup screen through hardn's main menu** has not run on a host ([screen-backup.md](screen-backup.md) §4.1).

## 12. Size

About 280 lines of non-test Go added and 425 removed (`git diff --numstat e666243`): `engine.go` 95, `dns.go` 107 (about half of both doc comments), `main.go` 14 changed in place, `pkg/menu` 50, the deleted `dns_menu.go` 397 and `MenuManager.ConfigureDNS` 5; about 765 lines of tests added and 305 removed (`menu_dns_test.go`, 277), `dns_test.go` 500 of them (its host model about 150, `-g`'s table about 200). Over the guideline of about 500 changed lines because of the tests: the root command driven end to end on each host kind, refusal and failure, and the menu's returns; the code alone is within it. Testdata: 16 goldens copied (about 735 lines), 16 changed (3 lines each), 13 goldens and 3 fixtures moved. `go.mod` and `go.sum` unchanged. `git status --short --ignored` before each commit listed only the unit's paths, none ignored.

**The review fixes** (four commits after `5037bf5`): about 85 lines of non-test Go added and 50 removed (`git diff --numstat 5037bf5`): `cmd/hardn/dns.go` (`runControls`, the signals per apply) 43 and 34, `engine.go` 9 and 4, `pkg/screens/dns` (`ApplyContext`, `planning.Apply`) 25 and 4, `pkg/menu` 6 and 8, `pkg/output/doc.go` 3 and 1; about 255 lines of tests added and 65 removed (`dns_test.go` 129 and 44). No golden changed.

**`next/s3-replace`** (slice 3, unit (d); `git diff --numstat a2cf7bd`, run, the docs aside): non-test Go about 125 lines added and 890 removed (`cmd/hardn/backup.go` 62 new; `pkg/menu` about 50 changed; the legacy Backup screen and its layers about 860 removed: `file_backup_repository.go` 259, `backup_menu.go` 247, `backup_service.go` 106, `backup_manager.go` 102, `utils.BackupFile` 46, the port 31, `MenuManager`'s methods 28, the factories 21, the model 18); tests about 380 added and 1,200 removed (`backup_test.go` 159 new, `legacy_record_test.go` 95, `TestBackupItem`'s helper; `menu_backup_test.go`, `backup_service_test.go` and `TestLegacyBackupFile` removed). Testdata: 22 goldens moved unchanged, 1 added. **Its review's fixes** (`git diff --numstat 85b4ad6`, run, the docs aside): non-test Go 8 lines added and 1 removed (`dns.go`, `withDNSScreen` generic), `internal/parity`'s comments 20 and 19; tests 135 added and 29 removed (`backup_test.go` 123 and 22: the cross-screen test redone, `TestBackupScreenSignals` new; `dns_test.go` 12 and 7). No golden changed.
