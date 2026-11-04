# Run All mock (slice 8, unit (a))

Unit (a) of slice 8, `next/s8-gallery`: a proposal for review. Nothing reaches hardn; the mock is in `internal/tools/uigallery` (`runall.go`). It replaces the withdrawn removal survey (`next/s8-survey`, kept unmerged), whose §1 to §3 facts it reads; that survey's decisions are void.
Status: built and tested on macOS in process (`go test ./internal/tools/uigallery/`); no hardn binary ran, nothing ran on a VM. The hosts' shapes are the VMs' reads (`vm-runs/s6/`, `vm-runs/s7/00-inspect-*.log`), their values synthetic.

## 1. What to run

```
go run ./internal/tools/uigallery runall [-ascii] [-height 40] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery play runall                                 # the menu of the hosts
go run ./internal/tools/uigallery play runall/<host>                          # one host
go run ./internal/tools/uigallery play [-sequence dry-run] runall/debian13    # plan only
go run ./internal/tools/uigallery play                                        # main menu 7 leads to runall/debian13
```

The listing's mocks are `screen-<host>` for each host below, `main-menu` (decision 7's alternative, drawn) and `flag` (`hardn -r`'s refusals, decision 8). Main menu 7 in play now opens `runall/debian13`, where it said "The gallery has no screen for Run All yet": the one existing golden that changes is `play/user-management`, whose input now opens 7 and goes back (`7`, `0`) before item 1.

## 2. What origin/main's Run All does

- Main menu 7 lists hardn.yml's dry-run state, username and SSH port, then "Enabled Features", AppArmor and Lynis among them, and a security warning (`pkg/menu/run_all_menu.go`).
- Option 1 runs `MenuManager.HardenSystem` at once: no plan, no confirmation (P-02). Option 2 flips dry run, asks "Type 'yes'" only when turning it off, and saves hardn.yml. *On `next/v0.4` P0.8's gate asks "Type 'yes'" before option 1 too.*
- Dry run is a second code path (`dryRunHardening`) that prints what it supposes; live, the progress lines are printed after `HardenSystem` returns, "AppArmor configured" and "Lynis security audit completed" included, which nothing did (V23).
- `HardeningConfig` is built from `username`, `sudoNoPassword`, `sshKeys`, `sshPort`, `sshListenAddress`, `sshAllowedUsers`, `enableUfwSshPolicy`, `ufwAllowedPorts`, `configureDns`, `nameservers`, `enableAppArmor`, `enableLynis`; `enableUnattendedUpgrades` is commented out.
- `hardn -r` (`cmd/hardn/main.go`, `runAll`) runs the same `HardenSystem` with no question, on a terminal or off, `ufwAllowedPorts` replaced by `[]`; `-r -e` runs `-r` alone (slice 9's defect 10).

## 3. The mocks

Each row's state is its control's real `Check`, mapped as the main menu maps it (`output.StatusViewOf`); its words are what Run All does. Every control hardn.yml asks for is planned alone first, writing nothing; those left are planned together in one plan on the real engine, reviewed by control, confirmed by the hostname typed, and confirmed or reverted from a new connection where the plan is lockout-risk.

| Host | What it shows |
|---|---|
| debian13 | The VM: george there, ufw on, updates on. Plans root login off and the nameservers: lockout-risk, typed, then `sudo hardn confirm` |
| ubuntu2604 | The VM (ssh.socket): as debian13 |
| alpine323 | The VM: root login off, Auto Updates on, the nameservers. Firewall refused: `ss` isn't installed (the VM's log: "ss absent") |
| fresh | Root alone, logged in by key. Plans george, Auto Updates, the nameservers (plain, still typed). Firewall refused (ufw not installed); SSH Login refused: root's own login (decision 3) |
| hardened | Everything as asked: "Apply all" unavailable, "Nothing to change" |
| partial | debian13 with the nameservers set: root login off alone |
| port | `sshPort: 2230`: the SSH Port row says "SSH Login's: 2230"; the rest as debian13 |
| pve9 | Root key-only and the nameservers. User refused (no sudo on Proxmox VE), Firewall refused (pve-firewall is read only), Auto Updates Not Applicable |
| container | LXC: root login off alone. DNS Not Applicable; the firewall applies and is on |
| nothing-asked | A default hardn.yml: every row "Nothing asked", the item unavailable |
| refused | Auto Updates refuses (timer off): its row says so, the warning repeats it in full before the plan, the rest is planned |
| pending | A port change waits for `hardn confirm`: the item says "Change pending", choosing it says SSH Login's words, nothing planned |

`screen-fresh`, plain:

```
# Run All ######################################################

  [ ] User                 Not Configured Create george, 1 key
  [ ] Firewall             Not Configured Refused
                           - ufw isn't installed. Install it,...
  [ ] SSH Login            Partial Refused
                           - You're logged in as root over SS...
  [ ] SSH Auth             Not Configured Nothing asked
  [ ] SSH Port             Not Configured Already as asked
  [+] AppArmor             Configured Status only
  [ ] Auto Updates         Not Configured Turn on
  [ ] DNS                  Not Configured Set 2 nameservers
  [+] Backups              Configured Used, not changed
  [-] Environment          Not Applicable Nothing asked

  Select an option

   1) Apply all            Plan, review, confirm

   0) Return

  > Enter your choice [0-1 or q]:
```

Where it departs from the other screens: the rows are the main menu's, Backups and Environment after them, their detail Run All's outcome rather than the check's; a refusal's line is cut to the width and given whole in a warning before the review; no section; the review is one line per control, the plan below it in the engine's order; the hostname is asked for a plain plan too (decision 6).

## 4. Heights

A sub-menu has no compact form: each screen is 20 to 23 lines at 80x24 and 80x40, Unicode and ASCII, no line over 80 cells (pve9, with two refusal lines, is 23). Full sections at every height, as gallery-firewall.md decision 9; no new decision.

## 5. Decisions for the maintainer

In dependency order. The brief's (5) "refuses or doesn't apply" comes second here: the design problem's answer rests on it.

1. **Scope: which controls Run All plans.** GATES all. Run All is main menu 7 and `hardn -r`; the brief decides one combined plan of the real controls. *(a), recommended:* it plans User (Create), SSH Login (root login off; root key-only on Proxmox VE), Firewall (Enable), DNS and Auto Updates (on only, never off: control-updates.md §2). It leaves SSH Auth (no key asks for it), the SSH port (decision 3), Backup's settings (a saver is planned alone, engine.md §2.18: Run All uses them as its plan's backups), Environment (no key asks for it: screen 8), AppArmor (status only in v0.4) and Lynis (§3.12, open: no row). *(b)* Environment always, as a plain change that `sudo hardn` needs. *(c)* the firewall's install too: not in one plan, since Enable reads the host as it is and refuses with no ufw (fresh), so it would take a second plan. Drawn and played: (a).
2. **A control that refuses or doesn't apply.** GATES 3. The engine refuses a whole plan for any control's refusal (engine.md §2.6), and DNS refuses on common hosts (control-dns.md, "Run All (slice 8)"). *(a), recommended:* Run All plans each control alone first, writing nothing, and skips one that refuses, doesn't apply, or has nothing to change, saying so on its row and in full before the review; the rest is one plan, whose own refusals (two controls in conflict) still refuse it whole. No engine change. *(b)* refuse everything when one refuses: on alpine323, fresh and pve9 nothing applies. *(c)* skip semantics in the engine: an engine change that weakens §2.6. Played: (a), on refused, fresh, pve9, alpine323.
3. **The admin and root login in one plan.** The controls plan from the host as it is: creating george and turning root login off together has SSH Login's proof refuse, george not existing yet. The reading showed more: on a fresh host the session is root's own, and `sshchange.Root` refuses that before the proof ("You're logged in as root over SSH..."), so no plan can turn root login off until the user logs in as the admin. *(a), recommended:* skip it, as 2 (a): the first Run All creates george, the row says why; logged in as george, the next Run All turns root login off on the real proof. *(b)* two phases in one run, accounts then a plan read afresh: refused all the same from root's session, and a second confirmation. *(c)* SSH changes left to SSH Login: `disableRootSSH` unused. *(d)* the proof given the planned users (an engine change): nothing it reads exists at plan time, and it still refuses root's own session. **The port:** always SSH Login's, never planned: Enable in the same plan allows the listeners as they are, so after the change was confirmed and 22 closed, the new port would not be allowed. Played: (a), on fresh.
4. **hardn.yml's keys, key by key.** Proposed (a), against slices 4 to 7's warnings: `username` and `sshKeys`, Create with admin rights and the keys, as `-c` (an existing user is as asked; its keys are User Management's); `disableRootSSH: true`, root login off (its slice 6 warning drops when true); `sshPort`, compared with sshd's listeners only; `enableUfwSshPolicy: true`, Enable; `configureDns: true` with `nameservers`, the nameservers; `enableUnattendedUpgrades: true`, on (false asks nothing); `enableBackups`, `backupPath`, the plan's backups, unchanged. Unused, warned once at load as slice 6's: `sudoNoPassword` (no more `/etc/sudoers.d/<user>`, V17), `sshAllowedUsers`, `sshListenAddress`, `sshKeyPath`, `ufwAllowedPorts`, `enableAppArmor`, `enableLynis`; `permitRootLogin` keeps its §3.17 deprecation; the examples lose `sshAllowedUsers: [george]` (slice 6's note). *(b)* no gates: `nameservers` alone asks DNS and the firewall is always enabled, which changes what origin/main's keys meant.
5. **One plan or several.** *(a), recommended (the brief's):* one plan. The engine runs accounts before the firewall before files (§2.5); a lockout-risk change arms the timer for the whole plan, so not confirming also undoes george and the nameservers (§2.10); one failure rolls everything back (§2.12); no planned control saves hardn.yml, so the one save per plan (control-dns.md) is never reached, and Backup's saver is never in it. *(b)* one plan per control, each confirmed. *(c)* the plain changes apart from the lockout-risk ones, so george stays when root login off is not confirmed: two confirmations.
6. **The confirmation.** *(a), recommended:* the hostname typed whatever the risk, as brief 1C and Phase 5 item 8 have it (`flow.RiskLockout`'s doc names Run All), confirm-or-revert only where the plan is lockout-risk (the engine's rule). Played: fresh's plain plan asks the hostname. *(b)* typed only at lockout risk, y/N otherwise.
7. **The main menu item (P-02).** *(a), recommended:* keep 7, its label and description (P-07's): with 6 (a), 7 chosen by accident opens a screen and applies nothing without the hostname. *(b)* the last item, 10, the rest moved up (`runall main-menu`, drawn, not played): muscle memory, docs and every main-menu golden change. *(c)* its own group below a divider: no such component (not drawn). *(d)* `MenuItem.Risk`'s "Lockout risk" line: a line taller, and the compact main menu at 24 rows has none to spare.
8. **`hardn -r`.** *(a), recommended:* on a terminal (stdin and stdout), the screen's plan, skips said on stderr, "Apply this plan?" above "Type the hostname", then the timer and `sudo hardn confirm` where lockout-risk, as `-w` and `-d`; off a terminal, any plan with changes refused, `PLAN_REFUSED`, nothing run (`runall flag`); `-n -r` the plan alone, 3 with changes, 0 without; `-r` with `-c`, `-d`, `-w`, `-g` or `-e`, a usage error, 2, nothing run (defect 10); `-u` still names the user. *(b)* off a terminal as `-w`: a plain plan applied unasked. *(c)* `-r` with other flags run in order after it. Exit statuses as `-w`'s otherwise. Words drawn, flag not built.
9. **The words.** §6, all proposed; the main menu's own "Run All", "Execute hardening operations" stay P-07's.

**Made here:**
- The rows are `StatusViewOf`'s with Run All's outcome as their detail, so the main menu's mapping is the one source of the state.
- A refusal's line is cut to one row; its words are whole in the warning before the review, and when nothing else is planned choosing the item says them and goes back.
- The pending refusal says SSH Login's words: revert's own name the change id, wrapped mid-id.
- The port row compares `sshd -T`'s listeners with `sshPort`; the control is never planned.
- The applying session matters: hardened's is on 2230, else the firewall plans 22.
- `play/user-management` opens 7 and goes back, rather than dropping the step.

## 6. Words

| Where | Words |
|---|---|
| Title, item | Run All; 1) Apply all, "Plan, review, confirm"; "Nothing to change" (unavailable); "Refused"; "Change pending" |
| A row's outcome | the change ("Create george, 1 key", "Root login off", "Root key-only", "Enable, SSH allowed", "Set 2 nameservers", "Turn on"); Already as asked; Nothing asked; Status only; Used, not changed; Refused; SSH Login's: 2230; Change pending; a Not Applicable control's own reason |
| Before the review | "<Row> skipped: <the control's refusal>" |
| Confirmation | "Apply this plan?" above "Type the hostname" |
| `hardn -r` | "hardn -r asks you to type the hostname, and this isn't a terminal (PLAN_REFUSED)", "To go on: run hardn -r from a terminal."; "hardn -r runs alone: it can't be combined with -c, -d, -w, -g or -e." |

## 7. For unit (b)

Stand-ins, labelled in `runall.go` and play's note:
- `raAsk`: hardn.yml's keys to controls (decision 4), to become the Run All screen's and `-r`'s one mapping.
- `raReading.pick`, `alone`, `port`: each control planned alone and skipped (decisions 2 and 3), the same function for the screen and `-r`.
- `raTyped`: the hostname asked whatever the risk, as `cmd/hardn`'s `typedEngine` does; the screen runs on `screenkit.Planning` without `screenkit.Change`, which has no typed-always option: a `Change` option, or the screen's own.
- The words (§6), and `raPendingWords` (SSH Login's).

Findings: on the Alpine VM the firewall control refuses (no `ss`), so Run All never enables it there; on Proxmox VE as installed, Create refuses (no sudo); in an LXC container the firewall control applies (only DNS and AppArmor are Not Applicable), which the brief assumed otherwise; each control's `Plan` runs alone and then together, and again under the lock (§2.9), which costs reads only.

## 8. Not run

No hardn binary, no VM: `-r` and the main menu item are not built, only drawn and played in the gallery. Decision 7 (b) is drawn, not played. The hosts' facts are the fake's, in the VMs' shapes.
