# Run All mock (slice 8, unit (a))

Unit (a) of slice 8, `next/s8-gallery`: a proposal for review. Nothing reaches hardn; the mock is in `internal/tools/uigallery` (`runall.go`). It replaces the withdrawn removal survey (`next/s8-survey`, kept unmerged), whose §1 to §3 facts it reads; that survey's decisions are void.
Status: built and tested on macOS in process (`go test ./internal/tools/uigallery/`); no hardn binary ran, nothing ran on a VM. Decisions adopted 2026-11-04. Since unit (b) (`next/s8-control`) the mock reads each host with Run All's planner, `pkg/engine/runall` ([control-runall.md](control-runall.md)); its stand-ins are gone but `raTyped`, unit (c)'s (§7). The hosts' shapes are the VMs' reads (`vm-runs/s6/`, `vm-runs/s7/00-inspect-*.log`), their values synthetic.
Since `next/s8-screen` the listing, its mocks and `raTyped` are gone: `play runall` and `play runall/<host>` run hardn's own screen, `pkg/screens/runall` ([screen-runall.md](screen-runall.md)), on the same hosts with the gallery's engine and record, and main menu 7 still leads to `runall/debian13`. git keeps the listing (`next/s8-gallery`); §1's first line and the listing's mocks below are its record.

## 1. What to run

```
go run ./internal/tools/uigallery runall [-ascii] [-height 40] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery play runall                                 # the menu of the hosts
go run ./internal/tools/uigallery play runall/<host>                          # one host
go run ./internal/tools/uigallery play [-sequence dry-run] runall/debian13    # plan only
go run ./internal/tools/uigallery play                                        # main menu 7 leads to runall/debian13
```

The listing's mocks are `screen-<host>` for each host below, `states` (decision 10's alternative, drawn), `main-menu` (decision 7's, drawn) and `flag` (`hardn -r`'s refusals, decision 8). Main menu 7 in play now opens `runall/debian13`, where it said "The gallery has no screen for Run All yet": the one existing golden that changes is `play/user-management`, whose input now opens 7 and goes back (`7`, `0`) before item 1.

## 2. What origin/main's Run All does

- Main menu 7 lists hardn.yml's dry-run state, username and SSH port, then "Enabled Features", AppArmor and Lynis among them, and a security warning (`pkg/menu/run_all_menu.go`).
- Option 1 runs `MenuManager.HardenSystem` at once: no plan, no confirmation (P-02). Option 2 flips dry run, asks "Type 'yes'" only when turning it off, and saves hardn.yml. *On `next/v0.4` P0.8's gate asks "Type 'yes'" before option 1 too.*
- Dry run is a second code path (`dryRunHardening`) that prints what it supposes; live, the progress lines are printed after `HardenSystem` returns, "AppArmor configured" and "Lynis security audit completed" included, which nothing did (V23).
- `HardeningConfig` is built from `username`, `sudoNoPassword`, `sshKeys`, `sshPort`, `sshListenAddress`, `sshAllowedUsers`, `enableUfwSshPolicy`, `ufwAllowedPorts`, `configureDns`, `nameservers`, `enableAppArmor`, `enableLynis`; `enableUnattendedUpgrades` is commented out.
- `hardn -r` (`cmd/hardn/main.go`, `runAll`) runs the same `HardenSystem` with no question, on a terminal or off, `ufwAllowedPorts` replaced by `[]`; `-r -e` runs `-r` alone (slice 9's defect 10).

## 3. The mocks

Each row is Run All's outcome alone (decision 10 (a)); the states are the main menu's. The revert state is read first (`revert.ReadState`): with a change waiting (`State.Busy`) nothing is planned. Else every control hardn.yml asks for is planned alone, writing nothing; those left are planned together in one plan on the real engine, its backups hardn.yml's (`BackupSettings`, as `cmd/hardn`'s `engineOptions`), reviewed by control, confirmed by the hostname typed, and confirmed or reverted from a new connection where the plan is lockout-risk.

| Host | What it shows |
|---|---|
| debian13 | The VM: george there, ufw on, updates on. Plans root login off and the nameservers: lockout-risk, typed, then `sudo hardn confirm`; the redraw then reads as pending |
| ubuntu2604 | The VM (ssh.socket): as debian13 |
| alpine323 | The VM: root login off, Auto Updates on, the nameservers. Firewall refused: `ss` isn't installed (the VM's log: "ss absent") |
| fresh | Root alone, logged in by key. Plans george with passwordless sudo (george logs in by key and has no password; decision 4), Auto Updates, the nameservers (plain, still typed). Firewall refused (ufw not installed); SSH Login refused: root's own login (decision 3) |
| hardened | Everything as asked: "Apply all" unavailable, "Nothing to change" |
| partial | debian13 with the nameservers set: root login off alone |
| port | `sshPort: 2230`: the SSH Port row says "SSH Login's: 2230"; the rest as debian13 |
| pve9 | Root key-only and the nameservers. User refused (no sudo on Proxmox VE), Firewall refused (pve-firewall is read only), Auto Updates Not Applicable |
| container | LXC: root login off alone. DNS Not Applicable; the firewall applies and is on |
| nothing-asked | A default hardn.yml: every row "Nothing asked", the item unavailable |
| refused | Auto Updates refuses (timer off): its row says so, the warning repeats it in full before the plan, the rest is planned |
| pending | A port change waits for `hardn confirm`: every row asked of and the item say "Change pending", choosing it says SSH Login's words, nothing planned |
| ufw-inactive | ufw installed and inactive, `ss` there, george's session, the nameservers set: Enable planned, root login off "After the firewall: run Run All again" (decision 5 (a)); the two in one plan the engine refuses whole (§7) |
| new-admin | george's session, hardn.yml asking carol with a key: Create carol and root login off in one lockout-risk plan; its undo removes carol and puts root login back |
| george-no-sudo | george with keys and no sudo, alice the admin, hardn by su: User "User Management's: not an admin", not changed; SSH Login refused (alice has no key) |

`screen-fresh`, plain:

```
# Run All ######################################################

# Controls -----------------------------------------------------

   -  User                 Create george, 1 key
   -  Firewall             Refused: ufw isn't installed. Inst...
   -  SSH Login            Refused: You're logged in as root...
   -  SSH Auth             Nothing asked
   -  SSH Port             Already as asked
   -  AppArmor             Status only
   -  Auto Updates         Turn on
   -  DNS                  Set 2 nameservers
   -  Backups              Used, not changed
   -  Environment          Nothing asked

  Select an option

   1) Apply all            Plan, review, confirm

   0) Return

  > Enter your choice [0-1 or q]:
```

Where it departs from the other screens: the rows are a section of the main menu's labels and Run All's outcomes, Backups and Environment after them, no state; a refusal is cut to the row and given whole in a warning before the review; the review is one line per control, the plan below it in the engine's order; the hostname is asked for a plain plan too (decision 6).

## 4. Heights

A sub-menu has no compact form: each screen is 22 lines, 23 with the item's lockout line, at 80x24 and 80x40, Unicode and ASCII, no line over 80 cells. Full sections at every height, as gallery-firewall.md decision 9; no new decision.

## 5. Decisions for the maintainer

In dependency order. The brief's (5) "refuses or doesn't apply" comes second here: the design problem's answer rests on it.

**Adopted (the maintainer, 2026-11-04):** "Adopt all, start (b)." Every recommended answer below is adopted, and every Made here call as stated. What follows from them:

- Run All plans User (Create), SSH Login (root login off; root key-only on Proxmox VE), Firewall (Enable), DNS and Auto Updates (on only), each planned alone first and skipped, said, where it refuses, doesn't apply or has nothing to change; the rest in one plan, with the firewall before root login off where both are asked (1 (a), 2 (a), 3 (a), 5 (a)).
- hardn.yml's keys as 4 (a), `sudoNoPassword` unused (Create's grant as `-c`'s); the hostname typed whatever the risk (6 (a)); main menu item 7 kept (7 (a), P-02 decided); `hardn -r` as 8 (a); §6's words decided (9); each row Run All's outcome alone (10 (a)).
- Unit (b) builds the planner as real code for the screen and `-r`, with the two real fixes of §7.

1. **Scope: which controls Run All plans.** GATES all. Run All is main menu 7 and `hardn -r`; the brief decides one combined plan of the real controls. *(a), recommended:* it plans User (Create), SSH Login (root login off; root key-only on Proxmox VE), Firewall (Enable), DNS and Auto Updates (on only, never off: control-updates.md §2). It leaves SSH Auth (no key asks for it), the SSH port (decision 3), Backup's settings (a saver is planned alone, engine.md §2.18: Run All uses them as its plan's backups), Environment (no key asks for it: screen 8), AppArmor (status only in v0.4) and Lynis (§3.12, open: no row). *(b)* Environment always, as a plain change that `sudo hardn` needs: not mocked. *(c)* the firewall's install too: not in one plan, since Enable reads the host as it is and refuses with no ufw (fresh), so it would take a second plan: not mocked. Drawn and played: (a).
2. **A control that refuses or doesn't apply.** GATES 3. The engine refuses a whole plan for any control's refusal (engine.md §2.6), and DNS refuses on common hosts (control-dns.md, "Run All (slice 8)"). *(a), recommended:* Run All plans each control alone first, writing nothing, and skips one that refuses, doesn't apply, or has nothing to change, saying so on its row and in full before the review; the rest is one plan, whose own refusals (two controls in conflict) still refuse it whole. No engine change. *(b)* refuse everything when one refuses: on alpine323, fresh and pve9 nothing applies; not mocked. *(c)* skip semantics in the engine: an engine change that weakens §2.6; not mocked. Played: (a), on refused, fresh, pve9, alpine323, george-no-sudo.
3. **The admin and root login in one plan.** The controls plan from the host as it is: creating george and turning root login off together has SSH Login's proof refuse, george not existing yet. The reading showed more: on a fresh host the session is root's own, and `sshchange.Root` refuses that before the proof ("You're logged in as root over SSH..."), so no plan can turn root login off until the user logs in as the admin. *(a), recommended:* skip it, as 2 (a): the first Run All creates george, the row says why; logged in as george, the next Run All turns root login off on the real proof. *(b)* two phases in one run, accounts then a plan read afresh: refused all the same from root's session, and a second confirmation; not mocked. *(c)* SSH changes left to SSH Login: `disableRootSSH` unused; not mocked. *(d)* the proof given the planned users (an engine change): nothing it reads exists at plan time, and it still refuses root's own session; not mocked. **The port:** always SSH Login's, never planned: Enable in the same plan allows the listeners as they are, so after the change was confirmed and 22 closed, the new port would not be allowed. Played: (a), on fresh.
4. **hardn.yml's keys, key by key.** *(a), recommended, played,* against slices 4 to 7's warnings: `username` and `sshKeys`, Create with admin rights and the keys, as `-c`: the grant asks no password ([control-users.md](control-users.md) §1, Create: "hardn's grant without a password"), george logging in by key with no password; a user there is as asked only as an admin with the keys, else left to User Management; `disableRootSSH: true`, root login off (its slice 6 warning drops when true); `sshPort`, compared with sshd's listeners only; `enableUfwSshPolicy: true`, Enable; `configureDns: true` with `nameservers`, the nameservers; `enableUnattendedUpgrades: true`, on (false asks nothing): new, as origin/main's Run All never acted on it (§2), so the first-run prompt "Enable automatic security updates? [y/N]" (`pkg/config/config.go`) now means Run All turns updates on; `enableBackups`, `backupPath`, the plan's backups, unchanged. Unused, warned once at load as slice 6's: `sshAllowedUsers`, `sshListenAddress`, `sshKeyPath`, `ufwAllowedPorts`, `enableAppArmor`, `enableLynis`; `permitRootLogin` keeps its §3.17 deprecation; the examples lose `sshAllowedUsers: [george]` (slice 6's note). *(b)* no gates: `nameservers` alone asks DNS and the firewall is always enabled, which changes what origin/main's keys meant; not mocked.
   **`sudoNoPassword`.** *(a), recommended, played:* unused, Create's grant as `-c`'s (no more `/etc/sudoers.d/<user>`, V17): a grant that asks a password george doesn't have would leave george unable to become root. *(b)* `sudoNoPassword: false` makes Run All skip Create and say why (Run All sets no password); not mocked.
5. **One plan or several.** *(a), recommended (the brief's):* one plan. The engine runs accounts before the firewall before files (§2.5); a lockout-risk change arms the timer for the whole plan, so not confirming also undoes george and the nameservers (§2.10); one failure rolls everything back (§2.12); no planned control saves hardn.yml, so the one save per plan (control-dns.md) is never reached, and Backup's saver is never in it (tested). Played on new-admin: carol (accounts) before the drop-in (files) before the reload, one timer, the journal's undo `userdel --remove carol` beside the drop-in's backup and sshd's reload. *(b)* one plan per control, each confirmed; not mocked. *(c)* the plain changes apart from the lockout-risk ones, so george stays when root login off is not confirmed: two confirmations; not mocked.
   **The firewall and root login together.** Where ufw is installed and off and hardn.yml asks for both, the engine refuses Enable and root login off as one plan (`engine.ErrConflict`: each plans sshd and ufw from the host as it is now, engine.md §2.9), so (a)'s one plan does not hold there. *(a), recommended:* plan the firewall and skip root login off, saying why on its row ("After the firewall: run Run All again"), as decision 3 (a) skips it on a fresh host: one confirmation, no engine change, and the next Run All plans it on the host as the firewall left it; the brief's order (accounts, then the firewall, then sshd's files) puts the firewall first. *(b)* the reverse: root login off now, the firewall next time. *(c)* two plans in one run, the firewall's then the rest read afresh: two typed confirmations. *(d)* an engine change letting the two share a plan: weakens §2.9's "each from the host as it is now". All not mocked: `ufw-inactive` drew the engine's refusal; since unit (b) it draws (a).
6. **The confirmation.** *(a), recommended:* the hostname typed whatever the risk, as brief 1C and Phase 5 item 8 have it (`flow.RiskLockout`'s doc names Run All), confirm-or-revert only where the plan is lockout-risk (the engine's rule). Played: fresh's plain plan asks the hostname. *(b)* typed only at lockout risk, y/N otherwise; not mocked.
7. **The main menu item (P-02).** *(a), recommended:* keep 7, its label and description (P-07's): with 6 (a), 7 chosen by accident opens a screen and applies nothing without the hostname. *(b)* the last item, 10, the rest moved up (`runall main-menu`, drawn, not played): muscle memory, docs and every main-menu golden change. *(c)* its own group below a divider: no such component; not mocked. *(d)* `MenuItem.Risk`'s "Lockout risk" line: a line taller, and the compact main menu at 24 rows has none to spare; not mocked.
8. **`hardn -r`.** *(a), recommended:* on a terminal (stdin and stdout), the screen's plan, skips said on stderr, "Apply this plan?" above "Type the hostname", then the timer and `sudo hardn confirm` where lockout-risk, as `-w` and `-d`; off a terminal, any plan with changes refused, `PLAN_REFUSED`, nothing run (`runall flag`); `-n -r` the plan alone, 3 with changes, 0 without; `-r` with `-c`, `-d`, `-w`, `-g` or `-e`, a usage error, 2, nothing run (defect 10); `-u` still names the user. *(b)* off a terminal as `-w`: a plain plan applied unasked; not mocked. *(c)* `-r` with other flags run in order after it; not mocked. Exit statuses as `-w`'s otherwise. Words drawn, flag not built.
   *Amended (the maintainer, 2026-11-04: "Adopt, sign off"), for plain plans only:* off a terminal, `-r`'s plan with no lockout risk is applied with `--yes`, and without it shown, nothing applied, `CONFIRMATION_REQUIRED`, exit 2; a lockout-risk plan is still refused there, `PLAN_REFUSED`, 1, `--yes` or not; on a terminal the hostname typed whatever the risk (6 (a)), `--yes` or not.
9. **The words.** §6, all proposed; the main menu's own "Run All", "Execute hardening operations" stay P-07's.
10. **The rows.** *(a), recommended, drawn and played:* each row Run All's outcome alone, under a "Controls" section; the main menu shows the states, and on 8 of 12 hosts the two contradicted ("SSH Port Not Configured Already as asked"). *(b)* the state and the outcome, as drawn before (`runall states`, drawn, not played).

**Made here:**
- The rows' labels are `StatusViewOf`'s, so the main menu's mapping stays the one source of the state, drawn there alone.
- A refusal is cut to its row after "Refused:"; its words are whole in the warning before the review, and when nothing else is planned choosing the item says them and goes back.
- The revert state is read once, before any plan: a change waiting marks every row asked of and the item "Change pending", with SSH Login's words, whatever each control would plan.
- The User row with the user there: "Already as asked" only where `users.Read` has it an admin with every asked key; else "User Management's:" and what is missing ("not an admin", "1 key missing").
- The port row compares `sshd -T`'s listeners with `sshPort`; the control is never planned.
- The applying session matters: hardened's is on 2230, else the firewall plans 22.
- `play/user-management` opens 7 and goes back, rather than dropping the step.

## 6. Words

Decided (2026-11-04, decision 9):

| Where | Words |
|---|---|
| Title, item | Run All; Controls (the rows' section); 1) Apply all, "Plan, review, confirm"; "Nothing to change" (unavailable); "Refused"; "Change pending" |
| A row's outcome | the change ("Create george, 1 key", "Root login off", "Root key-only", "Enable, SSH allowed", "Set 2 nameservers", "Turn on"); Already as asked; Nothing asked; Status only; Used, not changed; "Refused: <words>"; SSH Login's: 2230; User Management's: not an admin, 1 key missing; Change pending; a Not Applicable control's own reason |
| Before the review | "<Row> skipped: <the control's refusal>" |
| Confirmation | "Apply this plan?" above "Type the hostname" |
| `hardn -r` | "hardn -r asks you to type the hostname, and this isn't a terminal (PLAN_REFUSED)", "To go on: run hardn -r from a terminal."; "hardn -r runs alone: it can't be combined with -c, -d, -w, -g or -e." |

## 7. For unit (b)

Done in unit (b): the stand-ins are gone from `runall.go`; the mock calls `runall.Read` with the gallery's engine (`raEngine`) and draws its `Reading`.
- `raAsk`: now `runall.Asked`, hardn.yml's keys to controls (decision 4), the screen's and `-r`'s one mapping.
- `raReading.pick`, `alone`, `port`, `userThere`, `raLeft`: now `runall.Read`, each row's `Kind` and words; the revert state read before them is `Read`'s too.
- Decision 5 (a)'s rule, new: `runall.Read` plans the firewall and makes root login off `Later` ("After the firewall: run Run All again"); `ufw-inactive` now draws that, not the engine's refusal.
- The words (§6) and `raPendingWords`: now `runall.Title`, `Question`, the `Words*` constants and `PendingWords`. The item's own ("Apply all", "Plan, review, confirm", "Nothing to change", "Controls") stay the screen's, unit (c)'s.

Still a stand-in, unit (c)'s:
- `raTyped`: the hostname asked whatever the risk, as `cmd/hardn`'s `typedEngine` does; the screen runs on `screenkit.Planning` without `screenkit.Change`, which has no typed-always option: a `Change` option, or the screen's own.

Carried:
- Plain output: a plan's finding gets its own marker, not the change's bullet (debian13's "- dhcpcd may rewrite /etc/resolv.conf" reads as a change), answering slice 2's open note ("findings without a review look like plan bullets in plain output (before Run All)"). Fixed in unit (b): the warning marker `[ ]` (DEV-068).
- A real fix: `revert.State.hardn()` says "sudo hardn" where no elevation tool is recorded (`pkg/host/revert/store.go:36-41`), so pve9's root with no sudo is told `sudo hardn confirm`; a root session with no sudo or doas should say `hardn confirm`. The mock keeps the real words. Fixed in unit (b) (`revert.Hardn`, revert.md §9.5): pve9 now says `hardn confirm`.
- Main menu item 2's description, "Toggle SSH root access", is P-07's, not Run All's: to the leftover-legacy unit. *Done in `next/legacy-rehome`:* it reads "Root login, passwords, port" (P-07 (a)).

Findings: on the Alpine VM the firewall control refuses (no `ss`), so Run All never enables it there; on Proxmox VE as installed, Create refuses (no sudo); in an LXC container the firewall control applies (only DNS and AppArmor are Not Applicable), which the brief assumed otherwise; each control's `Plan` runs alone and then together, and again under the lock (§2.9), which costs reads only. With ufw inactive, Enable and root login off in one plan are refused whole by the engine (`engine.ErrConflict`: "both plan changes of sshd and ufw, each from the host as it is now"); decision 5 (a), adopted, plans the firewall alone, and ufw-inactive draws and plays that.

## 8. Not run

No hardn binary, no VM: `-r` and the main menu item are not built, only drawn and played in the gallery. Decisions 7 (b) and 10 (b) are drawn, not played. The hosts' facts are the fake's, in the VMs' shapes.
