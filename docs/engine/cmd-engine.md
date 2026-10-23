# The engine in `cmd/hardn`: `-g` and the DNS screen (slice 2, unit (d))

> **Unit:** refactor plan slice 2 (DNS, the pilot), unit (d), `next/s2-replace`: "Legacy path replaced" (plan §2, "Phases 3-5 together", §3.22). The maintainer, 2026-10-22: "Replace in unit (d): withdraw HARDN_UI=classic from §3.14, route menu item 3 and -g to the new control, and delete the legacy DNS screen after (b2), with the shared service going in slice 8"; and for `-g`, 2026-10-23: "apply without asking, -n -g shows the plan and writes nothing, and no udhcpc flag (the warning only)", a host every plan refuses exiting non-zero (V25's fix, decided: Phase 3's exit codes). Inputs: [screen-dns.md](screen-dns.md) §7, [parity-dns.md](parity-dns.md) §7, [engine.md](engine.md) §7's wiring item.
> **Status:** built and tested on macOS, in process: `cmd/hardn` through its seams on the recording fake (`hostfake`) with a model of systemd-resolved, the root command driven as the binary runs it; `pkg/menu`'s item with the screen stubbed. No hardn binary was run and nothing real was read, run or written. Nothing has run on Linux or as root: every host claim is **written, not run (gate 5)** (§4).

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
- **What it runs:** `pkg/screens/dns.Run` with `prompt.Stdin()` (the one line reader every legacy prompt shares), stdout's `term.Env` (`stdoutEnv`), the host of §2.2, `detect.Detect` once per visit, the `*config.Config` `cmd/hardn` loaded (the one the main menu holds), `NewEngine` over §2.1's options, `DryRun` read from that configuration as the item is chosen (as the legacy menus read it, so the Dry-Run item and `-n` apply), `LogPath` the legacy log, `Now` revert's clock. The screen starts from a cleared screen, as the legacy DNS menu's `PrintHeader` cleared it.
- **What it returns:** nil, back to the main menu, which draws itself again with the list as saved (the screen adopts each save into that configuration); `nav.ErrQuit` ends hardn as the main menu's q does (it clears the screen and returns, exit status 0); the end of the input exits 130, Ctrl-C at a prompt ends hardn by SIGINT, and any other error is printed and exits 1, as at every legacy prompt (`endHardn`, factored out of `readLine` unchanged).
- **The audit log:** not written. `cmd/hardn` has no audit writer yet (its first is `next/p3-startup`'s, [output.md](output.md) §7), so `Options.Ran` is nil.

### 2.4 `-g` (`configureDNS`, `cmd/hardn/dns.go`)

In its place in the flag sequence, after the root check and the configuration's load, as before: `dnsctl.Controls(cfg)` (hardn.yml's own list, so nothing is saved) planned, then applied without a question, through the engine with §2.1's options; the report is `output.Human`'s on stdout. `StopUDHCPC` is false: on Alpine, `DNS_UDHCPC` is reported and `udhcpc.conf` left as it is. A refused plan is reported with its code and how to go on (`resolvconf manages DNS here (DNS_MANAGED)`, "To go on: …") and never applied; a failed apply is rolled back and reported so. The legacy lines ("Failed to configure DNS: …", "DNS configured successfully", and after `-g` alone "Script completed selected hardening operations.") are gone: the report is the whole of it.

### 2.5 Exit statuses, `-n -g`, and the other flags

- **`-g`'s status** is `output.ExitCode`'s (Phase 3's, [reference](../reference/output.md) §3): 0 applied or nothing to change; 1 refused, failed or rolled back; 130 interrupted.
- **`-n -g`** is no longer refused: `-g` left `operationFlags`, the list `RefuseDryRunOperations` refuses, so its dry run is the plan alone (`mode` `plan`), shown with output's dry-run footer, nothing written but the engine's run directory under `/run/hardn`, made and removed by the plan (engine.md). Its status: **3 with changes to make**, 0 with none, 1 refused (§8, decision 4). `-n` with any other operation flag is refused as before (exit status 2, before the root check), the message naming those flags only.
- **With other flags in one invocation** (origin/main logged each failure and went on, V25): the flags run in their order as before, each reporting as before; `-g`'s status, when not 0, is hardn's once the flags after it (`-p`, `-e`) have run (`defer exit(code)` in its block, `main.go:398-402`). An interrupted `-g` ends hardn at once with 130, the flags after it not run. `-e`'s own failure still exits 1 at once. `-r` with `-g` runs `-r` alone, as before (its block returns first).

### 2.6 Signals

Both paths run under `signal.NotifyContext` for SIGINT, SIGTERM and SIGHUP (`engineSignals`), as engine.md §2.14 asks of the engine's caller: a signal during an apply cancels it, and the engine rolls the change back here with SIGHUP and SIGINT held, rather than Go's default ending hardn with the change half applied and journalled. `-g` registers around its plan and apply; the DNS item around its visit of the screen. On the screen, Ctrl-C at a prompt still ends hardn (the prompt reports it, `prompt.ErrInterrupted`); a signal outside a prompt and an apply cancels the visit's context, after which the screen reads nothing of the host (Unknown) and plans nothing until the user goes back (§11).

### 2.7 Deleted, kept, moved

- **Deleted**, each with its last caller: `pkg/menu/dns_menu.go` (`git grep -nE 'NewDNSMenu|DNSMenu\b|getCurrentDnsSettings' -- '*.go'`: no match); `MenuManager.ConfigureDNS` (its one caller was `dns_menu.go:150`; `git grep -n '\.ConfigureDNS(' -- '*.go'`: every caller left is `DNSManager`'s or `DNSService`'s); `cmd/hardn`'s `dnsManager` (`git grep -n dnsManager -- cmd/`: no match; `-g` was its caller); `pkg/characterization`'s `TestMenuDNSGoldens`, which mirrored the deleted handlers. `MenuManager`'s `dnsManager` field stays (its constructor's callers set it; slice 8 removes it with the service).
- **Kept** until slice 8, for `hardn -r` (`main.go:254`) and Run All's live run (`pkg/menu/run_all_menu.go:268`), both through `SecurityManager.HardenSystem`: the legacy DNS service of [screen-dns.md](screen-dns.md) §7's list, byte for byte, with `pkg/characterization`'s `ops/dns_*`.
- **Moved:** the record `TestParity` reads, frozen under `pkg/screens/dns/testdata/legacy` before its sources changed ([parity-dns.md](parity-dns.md) §7).

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| The engine reads `Revert.Hardn` only for a lockout-risk plan; `New` fills a nil `Revert.Plans` with `plans.Registry()`; `Record` refuses an `OpPlan` step whose unit has no rollback | `pkg/engine/plan.go:143, 232-243`, `boot.go:46`, `engine.go:153-155`; `pkg/host/revert` `checkPlans` (code) | §2.1 |
| The DNS change's undo is the `dns` unit's `OpPlan` | `pkg/engine/steps/plans.go`, `pkg/host/revert/plans` (code) | §2.1 |
| `detect.Detect` fails only when its context is done | `pkg/host/detect` `Detect`'s doc | §2.3 |
| A change without a timer that did not finish is named by the next run's notice, with `sudo hardn rollback <id>` | `pkg/host/revert` `Notices`, `NoticeInterrupted` (code) | §11 |
| `prompt.Reraise` ends hardn by SIGINT despite a `signal.Notify` it holds | `pkg/ui/prompt` `TestInterruptChild` (observed on macOS) | §2.6 |
| `hold.Signals`' `signal.Ignore` ends an earlier `NotifyContext`'s registration of SIGINT and SIGHUP | engine.md §2.14; `pkg/engine/hold` | §2.6, §11 |
| The characterization goldens cite `cmd/hardn/main.go`'s lines to 462 (`:123`, `:151`, `:158`, `:230-260`, `:234-250`, `:283-289`, `:365-383`, `:386-392`, `:409-416`, `:426-461`, `:440`) | `pkg/characterization/operations_test.go` (strings) | §8, decision 7 |

## 4. Written, not run (gate 5)

Nothing here has run. For the maintainer's VM runs, as root, on Debian 13, Ubuntu 26.04 (and 24.04), Alpine 3.24 (and 3.23) and Proxmox VE 9 by hand, after each `echo $?` and name resolution checked (`getent hosts` of a public name, and `resolvectl query` on systemd-resolved):

- `sudo hardn -g` on each stock host: `/etc/resolv.conf` (Debian, Alpine, Proxmox VE) or `/etc/systemd/resolved.conf.d/zz-hardn.conf` (Ubuntu) as the report says, the findings (dhcpcd, udhcpc with `udhcpc.conf` untouched, Proxmox VE's settings, eth0's own server), exit status 0; again, "Nothing needs changing.", 0; `/var/log/hardn.log` holding the commands and the engine's lines, the console the report alone.
- `sudo hardn -n -g`: the plan and its footer, nothing written (`/etc` unchanged; no plan directory left under `/run/hardn`), exit status 3; on a host as asked, 0.
- Refused, nothing written, exit status 1: a NetworkManager desktop, resolvconf's link, an LXC container, `nameservers: []`.
- A reload made to fail (resolved's unit masked, or `systemctl` made to exit non-zero), rolled back, exit status 1; Ctrl-C during the reload, rolled back, 130; an SSH session dropped during it (SIGHUP), rolled back.
- `sudo hardn -g -e`: both run; the exit status `-g`'s. `sudo hardn -n -g -d`: refused, 2, naming `-d`.
- The main menu's 3 as [screen-dns.md](screen-dns.md) §4 lists, then Return: the main menu again, and 3 again showing the list as saved; q in the screen ends hardn (0); Ctrl-D at its prompt ends it with 130; `sudo hardn -n` then 3, a dry run.
- `sudo hardn -r` and Run All's live run on Debian 13: DNS as origin/main configures it (the legacy service, unchanged).

## 5. Words

No new words in the report or the screens: `-g`'s report is `output.Human`'s, the screen's are [screen-dns.md](screen-dns.md) §5's, both **proposed** where they say so. What changes:

| Where | Words | Source |
|---|---|---|
| `-n` with other operation flags | the refusal no longer names `-g/--configure-dns` | `pkg/cmd`'s, generated |
| `-g`, before | "Failed to configure DNS: …", "DNS configured successfully", "Script completed selected hardening operations." | origin/main's; gone after `-g`, the report in their place |
| The DNS item with no screen set (a programming error, never in a release) | "menu: no DNS screen is set" | **proposed**, internal |
| hardn's log, a session not read | revert's own error ("revert: session: …") | revert's |

## 6. API

```go
package main // cmd/hardn, added
func engineOptions(ctx context.Context, sys host.System, c *config.Config) engine.Options
func newEngine(sys host.System, o engine.Options) func(progress func(engine.Event)) *engine.Engine
func configureDNS(c *config.Config) int                       // -g; the exit status
func withDNSScreen(m *menu.MainMenu, c *config.Config) *menu.MainMenu
func runDNSScreen(ctx context.Context, c *config.Config) error // the DNS item
var engineHost, engineDetect, engineHold, engineSignals, engineInput, engineStdout // seams

package menu // pkg/menu, added
func (m *MainMenu) SetDNS(run func() error)
```

No new flag, environment variable, configuration key, error code or module. `pkg/menu`'s `endHardn` is `readLine`'s abort handling, unchanged, made a function.

## 7. For the next slices' (d) units

- **The options:** call `engineOptions(ctx, sys, cfg)` with `engineHost()`; a screen takes `newEngine(sys, o)`; a flag plans with `engine.New(sys, o)` and renders with `output.Emit(…, output.FormatHuman, run, revertNow())`, its status `output.ExitCode(run)`, as `configureDNS` does. A lockout-risk change needs `Hardn` and `Session`, which are there; `--confirm-timeout` (§3.14) would set `Timeout`.
- **Signals:** register `engineSignals` around the plan and apply (a flag) or the screen's visit (a menu item).
- **The menu item:** a hook as `SetDNS`. When a second item needs one, generalise it (a map of item to screen, or one `SetScreens`), rather than adding a setter per item.
- **`-n` with the slice's flag:** decided per flag by the maintainer; to show the plan, take the flag out of `operationFlags`.
- **`main.go`'s lines:** the characterization goldens cite them to 462 (§3). Keep them (as here), or regenerate those goldens deliberately in a commit of their own, each changed line explained.
- **The legacy goldens `TestParity` reads:** freeze them before their sources change, as [parity-dns.md](parity-dns.md) §7 and §8 say.

## 8. Decisions of this unit

None is the maintainer's beyond those quoted above; each is for review.

1. **A hook on the main menu** (`SetDNS`), set by `cmd/hardn`, rather than `pkg/menu` assembling the engine (the task's rule) or `cmd/hardn` mapping the screen's return to a quit flag (which would copy `pkg/menu`'s abort handling into `cmd/hardn`). `pkg/menu` imports `pkg/ui/nav` for `nav.ErrQuit` alone.
2. **The log file alone** for the engine's lines and the run log, as `hardn confirm`'s clean-up: the console is the report's or the screen's. *Alternative:* `revertLog`, both, as `hardn rollback`, whose console is the journal's or the null device.
3. **`Plans` and `Hardn` set** (§2.1), the one assembly serving the lockout-risk slices too.
4. **`-n -g` exits 3 when the plan has changes to make**, output.ExitCode's reading of a plan (reference §3: "a check or a plan found changes to make"), the one function for every run. The task's text said "exit 0 (or the refusal's status when refused)"; 3 is Phase 3's decided code for a plan with changes, and 0 is kept for a plan with none. Say if 0 is wanted: one line in `configureDNS`.
5. **`-g`'s status after the flags that follow it**, and at once when interrupted (§2.5).
6. **The legacy "Script completed" line no longer follows `-g` alone**, which would print a success line under a refusal.
7. **`main.go`'s lines to 462 kept**: line 224 is a comment where `dnsManager` was, the `-g` block keeps its 8 lines, `withDNSScreen` wraps line 190, so that no characterization golden's citation goes stale and none changes but the DNS ones.
8. **The DNS screen starts from a cleared screen**, as the legacy DNS menu did, and as every other item's screen starts.
9. **`NotifyContext` around the DNS screen's visit**, not only around `-g` (§2.6; its own commit, after a first build that ran the screen on `context.Background()`, where a signal mid-apply ended hardn half applied).

## 9. Tests

- `cmd/hardn`: `TestEngineOptions` (each field to its source; no path, no session), `TestEngineSeamsAreReal`, `TestDNSScreen` (Add on Debian 13 applied and saved, the configuration adopted and recording the saved bytes, no audit record; a dry run and a decline writing nothing; q; the end of the input), `TestDNSScreenInterrupted` (a signal mid-apply rolled back, the screen going on), `TestConfigureDNSFlag` (through the root command: Debian 13, Ubuntu 26.04, Alpine 3.24, a host as asked; resolvconf, NetworkManager, a container, no nameservers and a fact not established refused with nothing written, 1; a reload with no effect rolled back, 1; `-n -g` 3, 0 and 1 with nothing written; hardn.yml never saved; no question; nothing through the legacy provider), `TestConfigureDNSFlagWithOthers` (`-g` then `-e`, refused and applied; Ctrl-C mid-apply, 130, `-e` not run; `-n -g -d`), and `TestDryRunRefusesOperations`, `TestDryRunAloneIsNotRefused` (`-n -g` reaches the root check), `TestDeprecationWarning` (now with `-d`). Every test that does not give the engine a host fails if a run reaches it (`runHardn`).
- `pkg/menu`: `TestDNSItem` (each return mapped; the screen cleared first; the configuration the screen changed is the menu's).
- `pkg/cmd`: `TestRefuseDryRunOperations`' three-flag case names `-d` in place of `-g`.
- Mutation-checked (`go test -overlay`): without the signals' context, `TestDNSScreenInterrupted` and the `-g` Ctrl-C case fail; `-n` applying fails `-n -g`; no deferred exit fails every refusal and failure.

## 10. Goldens

`pkg/characterization`'s `ops/dns_{systemd_resolved,resolvconf,direct,empty_nameservers}/*` (16) change in two places, where `-g` leaving the service changes what they say: "Reached by" (`-r` and Run All's live run through `HardenSystem`'s DNS step; `-g` and the DNS screen on the control), and the modelled section, now `hardn -n -r`, still refused (exit status 2), so V1's FIXED line and the header stay. Every other characterization golden, `pkg/menu`'s `main_menu` and `run_all`, `parity.golden` and parity-dns.md §4 are byte for byte as at e666243 (`git diff e666243 --stat`).

## 11. Open items

- **A signal outside a prompt and an apply on the screen** cancels the visit (§2.6): the rows read Unknown and a change is refused as cancelled until the user goes back. And after an in-process rollback, the hold's release ends the registration of SIGINT and SIGHUP for the rest of the visit, so a later one takes Go's default (a change it interrupts is then journalled, and the next run says to run `sudo hardn rollback <id>`). Re-registering per change needs a context the flow can be given per change, the screen's or `pkg/ui/flow`'s to offer.
- **The audit log** stays unwritten until `next/p3-startup` (§2.3): then `-g`'s run and `Options.Ran` take `output.Append`.
- **`-n -g`'s 3** (§8, decision 4).
- **The `parity.golden` header** still says origin/main's goldens are "read in place"; kept byte for byte (parity-dns.md §7).
- **The main menu's DNS row** (P-01) is a later unit's.

## 12. Size

About 280 lines of non-test Go added and 425 removed (`git diff --numstat e666243`): `engine.go` 95, `dns.go` 107 (about half of both doc comments), `main.go` 14 changed in place, `pkg/menu` 50, the deleted `dns_menu.go` 397 and `MenuManager.ConfigureDNS` 5; about 765 lines of tests added and 305 removed (`menu_dns_test.go`, 277), `dns_test.go` 500 of them (its host model about 150, `-g`'s table about 200). Over the guideline of about 500 changed lines because of the tests: the root command driven end to end on each host kind, refusal and failure, and the menu's returns; the code alone is within it. Testdata: 16 goldens copied (about 735 lines), 16 changed (3 lines each), 13 goldens and 3 fixtures moved. `go.mod` and `go.sum` unchanged. `git status --short --ignored` before each commit listed only the unit's paths, none ignored.
