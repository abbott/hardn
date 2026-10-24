# The legacy Dry-Run path replaced (slice 2b, unit (d): `next/s2b-replace`)

> **Unit:** refactor plan slice 2b, unit (d), `next/s2b-replace` ([gallery-dryrun.md](gallery-dryrun.md) §8 (d)). The maintainer's request (2026-10-23): "Start slice 2b unit (d): remove item 6 (its key left unused) and the legacy Dry-Run code, remove Run All's option 2, refuse items 1, 5 and Firewall's profile add and remove under plan only, and freeze the legacy goldens as the parity record." Decisions built: 1, 6, 7 and 10 of [gallery-dryrun.md](gallery-dryrun.md) §4, adopted by the maintainer on 2026-10-23. Proposal: [P-03](../refactor/proposals.md#p-03), implemented by slice 2b, units (b) to (d).
> **Status:** built and tested on macOS, in process: `pkg/menu`'s screens driven through scripted stdin, the screens an item opens recorded in place of opened, saves observed in each test's temporary working directory, the configuration read from `hostfake`. No hardn binary ran; nothing touched this machine's `/etc`; nothing has run on Linux. The VM steps are §7, for the orchestrator. "Not available in plan only." is the maintainer's; **every other new line is proposed** (§6).

## 1. What is built

1. **Item 6 removed, its key left unused** (decision 1). The main menu lists 1 to 5, then 7 to 10, and 0; 6 is refused under the prompt as any key no item has, "⨳ Invalid option. Please try again." (`(x)` in ASCII); the hint, generated from the items, is "[0-5, 7-10 or q]". The menu is 35 lines at 80 columns, 57 at 60 (one and two fewer).
2. **Run All runs in the session's mode** (decision 7). Its option 2, "Enable dry-run mode and run" or "Disable dry-run mode and run", is removed. Option 1 and its typed yes are unchanged. Key 2 is left unused by item 6's rule; it was the last option, so no key would have been renumbered either way. 2 is refused as any other choice, in Run All's own words and pause; the hint is "[0-1 or q]".
3. **Plan only refuses what would still change the host** (decision 6), whatever set it (`-n` or hardn.yml's `dryRun: true`), in the maintainer's words, "Not available in plan only." (§3).
4. **The parity record**, frozen first and byte for byte (§4).
5. **The gallery's Dry-Run mode retired**, with its last mocks and play variants ([gallery-dryrun.md](gallery-dryrun.md) §11).
6. **The deprecation note made current** (decision 10): docs/configuration.md says item 6 is removed and which menu areas plan only refuses; docs/releases/unreleased.md's item-6 line no longer waits, with Run All's option 2 and the refusals beside it.

## 2. What was removed

Each was named by the request, or had its last caller in what was. After the unit, each grep below, from the repository root, finds nothing.

| Removed | Why | Grep (no match left) |
|---|---|---|
| Item 6 in `mainMenu()`, `legacyScreens.dryRun` | named | `grep -rn 'NewDryRunHandler' --include='*.go' .` |
| `pkg/menu/dry_run_handler.go`, `dry_run_menu.go` | named: item 6's screens | `grep -rn -e DryRunHandler -e DryRunMenu --include='*.go' .` |
| `infrastructure.MenuFactory.CreateDryRunMenu` | built the deleted `DryRunMenu`; it had no caller | `grep -rn 'CreateDryRunMenu' --include='*.go' .` |
| `MainMenu.createMainMenu` | origin/main's main menu on `pkg/style`, with item 6; drawn by nothing since unit (c), its last callers two tests | `grep -rn 'createMainMenu()' --include='*.go' .` (two comments name it as the record's source) |
| `style.Menu.SetDryRunStatus`, its fields `showDryRunInfo` and `dryRunEnabled`, and `Render`'s branch that drew "Dry-run Mode" | the legacy indicator; `createMainMenu` was its one caller | `grep -rn -e SetDryRunStatus -e showDryRunInfo -e dryRunEnabled --include='*.go' .` |
| Run All's option 2 (`run_all_menu.go`) | named | `grep -n -e 'dry-run mode and run' -e 'case "2"' pkg/menu/run_all_menu.go` |
| `config.Config.SetDryRun` | the in-session toggle; item 6 and option 2 were its callers | `grep -rn 'SetDryRun(' --include='*.go' .` |
| The gallery's `dryrun.go`, `dryrun_test.go`, six goldens, the `dryrun` and `play dryrun` dispatch | the remaining Dry-Run mocks; nothing else used them (`golangci-lint`'s `unused`: 0 issues) | `grep -rn -e dryRunPrefix -e dryRunList -e openDryRun internal/` |
| `pkg/characterization`'s four Dry-Run cases of `TestLegacyMenuDryRun`, and their goldens | they ran the deleted handler; the goldens are frozen (§4) | `grep -n 'dry_run_' pkg/characterization/*.go` |

Kept: `handled`, which still opens Run All's handler; `style.Menu.SetIndentation`, which User Management's screens call; the Firewall case of `TestLegacyMenuDryRun` and its golden, Configure being unchanged; `TestMainMenuRenderGolden`, which records the gated legacy status block (`legacySecurityStatus`) and lost only the menu under it; the indicator's no-source state (§3).

## 3. The refusals (decision 6)

| Where | Live | Under plan only (`-n` or hardn.yml) | Style |
|---|---|---|---|
| Main menu 1, User Management | opens, as before | refused before its screen is built | under the prompt as an invalid key: "⨳ Not available in plan only." (`(x)` in ASCII), `status.bad`, the prompt asked again, no clear, the main menu not read or drawn again |
| Main menu 5, Backup | opens, as before (with backups on, it writes and removes `<path>/.write_test`) | the same | the same |
| Firewall, Manage application profiles: 1 Add, 2 Remove | asks and saves `./hardn.yml`, as before | refused before any question or save; 2 with no profile still says "No profiles to remove" | the screen's own refusal of a choice it does not take: "✗ Not available in plan only." in red, then its "❯ Press Enter to continue..." pause, then the profiles screen again |

The main menu's refusal is `nav.Option.Refusal` (new): asked as the item is chosen, before its action, a reason refuses the line under the prompt as a line no item takes is refused. An item disabled (`MenuItem.Disabled`) would have said "User Management is unavailable." and left its key out of the hint; an action that printed the words would have had the main menu drawn again under them. The keys stay in the hint, "[0-5, 7-10 or q]", as the retired mock `play dryrun/proposed` drew it.

Not refused, each keeping its own check as before: 2 (SSH Login), 3 (DNS, which stops at its plan), Firewall's install, enable, disable, Configure and apply-profiles lines, 7 (Run All's option 1 simulates), 8, 9; 10 reads only. With 1, 5 and the profiles refused and option 2 and item 6 gone, no legacy save runs under plan only (decision 8's premise).

**No in-session toggle remains**, so "Mode Plan only" with no source, which item 6 and option 2 set, no longer occurs in hardn: plan only is always "(-n)" or "(hardn.yml)". The no-source state stays in code, unreachable: it is `component.ModeIndicator`'s zero `Source`, and `pkg/menu` maps `DryRunDefault` to it should anything write `DryRun` after the start (`TestMainMenuMode`'s last case). Removing it would change the component, which this unit leaves as it is.

## 4. The parity record

Frozen in the unit's first commit (dcdcbdf), before any source changed, under **`pkg/menu/testdata/legacy`**, each file at the path it had under its package's `testdata/golden`. Slice 2's pattern: the record lives with the package that replaces the legacy code, as `pkg/screens/dns/testdata/legacy` did; here `pkg/menu` holds the main menu, Run All and the refusals, and its tests read the record (`TestLegacyRecord`, `TestMainMenuItems`). Nothing writes there: `pkg/menu`'s `-update` writes under `testdata/golden`, `pkg/characterization`'s under its own testdata.

origin/main (9d46866) has no golden files: every file here is the trunk's characterization of origin/main's behaviour, recorded at the revision named.

| File | Recorded by | Bytes of | What it records |
|---|---|---|---|
| `menu/dry_run_enable.golden` | `pkg/characterization` `TestLegacyMenuDryRun` | 1467f0d (written by 2d2d617) | item 6, live, 1 to enable: three cleared screens and three pauses; `./hardn.yml` saved whole with `dryRun: true` |
| `menu/dry_run_disable.golden` | the same | 1467f0d (2d2d617) | item 6 under dry run, 1 to disable: the Enabled menu never printed; `./hardn.yml` saved whole, `dryRun: false` |
| `menu/dry_run_return.golden` | the same | 1467f0d (2d2d617) | item 6, live, 0 to keep it: nothing changed, `./hardn.yml` saved all the same |
| `menu/dry_run_quit.golden` | the same | 1467f0d (2d2d617) | item 6, live, q at its menu: nothing saved, "is now DISABLED" and a second pause |
| `main_menu/alpine-update-available.golden` | `pkg/menu` `TestMainMenuRenderGolden` | cc04c17 (f6fedaa) | origin/main's main menu on `pkg/style`, Alpine, live: the status block, "Dry-run Mode Disabled", items 1 to 10 with 6 Dry-Run, "[0-10 or q]"; the update notice off (V26's gate) |
| `main_menu/debian-figma.golden` | the same | cc04c17 (56f0d93) | the same on Debian 13 in dry run, "Dry-run Mode Enabled" |
| `main_menu/debian-figma-no-color.golden` | the same | cc04c17 (56f0d93) | the same with `style.UseColors` off |
| `main_menu/ubuntu-fresh.golden` | the same | cc04c17 (56f0d93) | the same on a fresh Ubuntu 24.04 host, live |
| `run_all/option1-live-cancel.golden` | `pkg/menu` `TestRunAllConfirmationGolden` | cc04c17 (5cb0fd6) | Run All, live, option 1 declined at its typed yes (A4's gate), its menu with option 2, "[0-2 or q]" |

**Why 1467f0d for the Dry-Run four:** unit (b) rewrote three of them (4d5732d, 4925c6a): `dry_run_enable`'s saved file now says `dryRun: false`, and the three headers mark decision 8 and V3 fixed. Those edits describe the trunk's save, which writes `dryRun` as loaded, not origin/main's, which wrote the session's mode; the record is origin/main's. 1467f0d is unit (a)'s last commit, after the maintainer's adoption, and holds unit (a)'s bytes (2d2d617's, `cmp` equal).

**Moved or copied**, as slice 2 did: the Dry-Run four are **moved** (their test cases retired with the code they ran; git records the renames, the bytes 1467f0d's); `main_menu/*` and `run_all/*` are **copied** (their tests stay, on code that stays: the gated legacy status block, Run All's option 1), and the goldens in place were regenerated where this unit changed what they record.

**Proved** for each file, from the repository root, each `cmp` silent:

```text
for n in enable disable return quit; do git show 1467f0d:pkg/characterization/testdata/golden/menu/dry_run_$n.golden | cmp - pkg/menu/testdata/legacy/menu/dry_run_$n.golden; done
for f in alpine-update-available debian-figma debian-figma-no-color ubuntu-fresh; do git show cc04c17:pkg/menu/testdata/golden/main_menu/$f.golden | cmp - pkg/menu/testdata/legacy/main_menu/$f.golden; done
git show cc04c17:pkg/menu/testdata/golden/run_all/option1-live-cancel.golden | cmp - pkg/menu/testdata/legacy/run_all/option1-live-cancel.golden
```

`TestLegacyRecord` lists the nine with their SHA-256 and fails on a changed byte, a missing file or one not listed.

## 5. Tests and goldens

- `pkg/menu`: `TestLegacyRecord` (new); `TestPlanOnlyRefusals` (new: the main menu's 1 and 5 and Firewall's profile add and remove, live and under `-n` and hardn.yml; no screen opened, nothing asked, the profile list unchanged and no `./hardn.yml` written for each refused action; live, the screens open and a profile added or removed is saved; ⨳ in Unicode); `TestRunAllInTheSessionsMode` (new: no option 2 live and under each source, 2 refused, the mode and its source as hardn started, nothing applied or saved); `TestMainMenuKeys` (6 refused, every key in turn); `TestMainMenuItems` (origin/main's items read from the record, but 6; the hint); `TestMainMenuMode` (the four starts, and a write to `DryRun` drawn with no source); `TestMainMenuHeights` (35 and 57 lines); the hint in `TestMainMenuInvalid`, `TestMainMenuInterrupt`, `TestDNSItem`; `TestRunAllConfirmation` without its option-2 case. Mutation-checked (`go test -overlay`): each refusal dropped, the reason given live, and nav ignoring `Refusal` each fail a test.
- `pkg/ui/nav`: `TestRefusal` (new). `pkg/config`: `TestDryRunSourceOnlyAsSet`, `TestSaveConfigDryRunAsLoaded` on writes to `DryRun` in place of `SetDryRun`. `pkg/ui/screen`: `TestCompact`, `TestHeights`, `TestCompactForm` on the main-menu mock without item 6. `pkg/characterization`: `TestLegacyMenuDryRun`, the Firewall case alone.
- **The main-menu mock's items follow hardn's** (`screen.MainMenuFixture`, which the gallery's status hosts, play and p4status draw): its item 6 goes too, so that the gallery's main menu stays line for line hardn's (`TestLiveStatusOnHosts`), and p4status's with it on the VMs; its Figma note says so. Figma's own menu fixtures (`component/fixture.FigmaMenu`, nav's test menu) keep Figma's ten items.
- **Goldens changed**, each because this unit changes what it records: `pkg/menu` `main_menu_live/*` (item 6 and the hint) and `main_menu/*` (the status block alone, `createMainMenu` gone), `run_all/option1-live-cancel` (option 2, the column width, the hint, a FIXED line); `pkg/ui/screen` `main-menu/00-tui-main` and `00-tui-main-compact` (the compact record follows the mock, so that flipping the gate still draws what it holds); the gallery's `status/*` (17) and the play transcripts that draw the main menu (6). Deleted: the gallery's `dryrun/*` (4) and `play/dryrun*` (2). Moved: `menu/dry_run_*` (4, §4). Added: the record's copies (5).

## 6. Words

**Proposed** where new, but the maintainer's "Not available in plan only.".

| Where | Words | Source |
|---|---|---|
| Main menu, 1 and 5 under plan only | "⨳ Not available in plan only." (`(x)` in ASCII) | **decided** (decision 6); the glyph DEV-062 |
| Firewall profiles, 1 and 2 under plan only | "✗ Not available in plan only.", then "❯ Press Enter to continue..." | the decided words in the screen's own refusal style (`firewall_menu.go`'s invalid option) |
| Main menu, 6 | "⨳ Invalid option. Please try again." | origin/main (P-25 (c)) |
| Run All, 2 | "✗ Invalid option. Please try again.", "Press Enter to continue..." | Run All's own |
| Hints | "[0-5, 7-10 or q]", Run All's "[0-1 or q]" | generated |
| docs/configuration.md | "**Some menu areas are refused.** User Management, Backup, and adding or removing a Firewall profile say "Not available in plan only." Start without plan only to use them."; the note's "The main menu's Dry-Run item (6) is removed, and Run All no longer turns dry run on or off: for plan only, start with `-n` or set `dryRun: true`." | **proposed** (decision 10) |
| docs/releases/unreleased.md | the header; "**Run All no longer turns dry run on or off.** …"; "**Under plan only, some menu areas are refused.** …" | **proposed** |

## 7. The VM steps (for the orchestrator)

On Debian 13, Ubuntu 26.04 and Alpine 3.23 ([vmtest.md](../safety/vmtest.md)), as root (Alpine through doas), hardn built from this branch (`CGO_ENABLED=0 GOOS=linux go build -trimpath -tags=netgo ./cmd/hardn`, the VM's `GOARCH`), from `/root`, in tmux (`tmux new -x 80 -y 40`). Never choose Run All's 1 here: live it runs every step.

1. **Before:** `cp -p /etc/hardn/hardn.yml /root/hardn.yml.before 2>/dev/null`; `ls -l /root/hardn.yml 2>/dev/null` (note it; no step may write it); `sha256sum /etc/hardn/hardn.yml /etc/resolv.conf /etc/ssh/sshd_config.d/* /etc/ufw/applications.d/* /root/hardn.yml 2>/dev/null > /root/dryrun-replace-before`; `touch /root/dryrun-replace-stamp`; `sleep 2` (busybox find compares whole seconds, menu-live.md §10.1).
2. **Live, the main menu:** `./hardn`: "Mode Live"; items 1 to 5, then 7 to 10, no "6) Dry-Run"; "❯ Enter selection [0-5, 7-10 or q]:"; 35 lines with no line under a row (36 on Ubuntu 26.04, whose User row has one). `6`: "⨳ Invalid option. Please try again." under the prompt, the prompt again, nothing cleared, no pause.
3. **Live, 1 and 5 open as before:** `1`: User Management opens on a cleared screen; leave it with `0`: the main menu under it. `5`: Backup opens; `0`. With backups on and its directory present, opening Backup writes and removes `<backupPath>/.write_test`, as origin/main does (gallery-dryrun.md §2.4).
4. **Live, Run All:** `7` (asked "Would you like to set a username now? (y/n)" when hardn.yml has none: `n`): "1) Run all hardening steps" and "0) Return to main menu", no option 2, "[0-1 or q]". `2`: "✗ Invalid option. Please try again.", "Press Enter to continue...", Enter: the screen again. `0`: "Operation cancelled. No changes were made.", the main menu, still "Mode Live". `q`.
5. **`-n`:** `./hardn -n`: "Mode Plan only (-n)". `1`: "⨳ Not available in plan only." under the prompt, no screen, the prompt again; `5`: the same; `6`: "Invalid option". `4` (Firewall), `3` (Manage application profiles), `1`: "✗ Not available in plan only.", "❯ Press Enter to continue...", Enter: the profiles screen again, nothing asked; `2`: "No profiles to remove" where there are none, else the refusal again; `0`, `0`. `7`: "Dry-Run Mode Enabled", no option 2; `2`, Enter, `0`: still "Mode Plan only (-n)". `q`.
6. **hardn.yml:** set `dryRun: true` in `/etc/hardn/hardn.yml` (where none exists, create one with `nameservers: []` and `dryRun: true`, noting it). `./hardn`: "Mode Plan only (hardn.yml)"; `1` and `5` refused as in step 5; `4`, `3`, `1` refused; Enter, `0`, `0`, `q`. `./hardn --dry-run=false`: "Mode Live"; `5` opens Backup; `0`, `q`. Restore the file (`cp -p /root/hardn.yml.before /etc/hardn/hardn.yml`), or remove the one created.
7. **ASCII:** `LANG=C ./hardn -n`, `1`: "(x) Not available in plan only."; `q`.
8. **After:** `sha256sum -c /root/dryrun-replace-before`: every file OK, step 6 having restored hardn.yml; `ls -l /root/hardn.yml`: as in step 1, or still absent; `find /etc /var/lib /var/backups /root -newer /root/dryrun-replace-stamp` lists only `/root` itself, step 6's `/etc/hardn` where its edit or the file's creation changed the directory, and the backup directory of steps 3 and 6's live Backup (`.write_test`, written and removed); nothing from steps 5 to 7.

Record per VM: the main menu's line count and hint, each refusal's line as drawn, Run All's menu live and under `-n`, and the `find` output.

## 8. Decisions of this unit

1. **The main-menu mock drops item 6 with hardn** (§5): one list of items for hardn, the gallery and p4status, as `TestMainMenuItems` holds them, rather than a gallery that draws an item hardn no longer has.
2. **`nav.Option.Refusal`** for 1 and 5 (§3): "under the prompt, as an invalid key" needs the refusal at the line's resolution. An optional field, no change for any other menu.
3. **The Firewall refusal pauses**, as that screen's invalid-option refusal does: the profiles screen clears as it is drawn again, so a line printed without the pause would never be seen.
4. **Run All's key 2 left unused**, by item 6's rule; nothing would be renumbered, option 2 being the last.
5. **`config.Config.SetDryRun` deleted** with its last callers; the indicator's no-source state kept, unreachable (§3).
6. **`TestMainMenuRenderGolden` kept** for the gated legacy status block, and its goldens copied to the record rather than moved: the block's code stays behind `legacySecurityStatus`, and this test is its only one.
7. **The record in `pkg/menu/testdata/legacy`** (§4), the package that replaces the code, as slice 2 placed its own.
8. **proposals.md:** P-03's status line and its row in the table both say implemented; they are the same status.

## 9. Open

1. **Not verified on a host:** everything of §7; nothing has run on Linux.
2. **The maintainer:** the proposed lines of §6; the main menu's hint still lists 1 and 5 under plan only (as the adopted mock did); `confirm` and `rollback` under the file's plan only ([dryrun-mode.md](dryrun-mode.md) §4.1 item 1), still open.
3. Slice 2b's sign-off: units (b) to (d) released together (the release notes' header).

## 10. Size

Against `next/s2b-menu` (`git diff --numstat cc04c17`), the unit's eight commits: about 125 lines of non-test Go added and 950 removed (removed: the gallery's `dryrun.go` 509, item 6's two screens 201, `createMainMenu`, Run All's option 2 and their comments about 110, `pkg/style`'s indicator 45; added: `pkg/menu` 65, the refusals, `notInPlanOnly` and comments, `pkg/ui/nav` 20, comments elsewhere); about 465 lines of tests added and 360 removed (`TestPlanOnlyRefusals`, `TestRunAllInTheSessionsMode`, `TestLegacyRecord`, `TestRefusal`, the rewritten `TestMainMenuItems` and `TestMainMenuMode`; the gallery's `dryrun_test.go`, 185, removed); and about 200 lines of docs, this page 135 of them. **Over the guideline** of about 500 changed lines, deletions and testdata aside (about 790 added), because of the tests: each refusal under each source and live, Run All under each source, the record's own test; the code alone, 125 lines, is well within it. Testdata: 49 golden files added, moved, changed or deleted (about 360 lines added, 2,550 removed), §5.
