# The main menu on its layout (slice 2b, unit (c): `next/s2b-menu`)

> **Unit:** refactor plan slice 2b, unit (c), `next/s2b-menu` ([gallery-dryrun.md](gallery-dryrun.md) §8 (c)). The maintainer's request (2026-10-23): "Start slice 2b unit (c): the main menu on its layout in the full form at every height, with the decided indicator (Mode Live / Plan only and its source), the compact form gated." Decisions built: 2, 3 and 5 of [gallery-dryrun.md](gallery-dryrun.md) §4, adopted by the maintainer on 2026-10-23, decision 5 with the clarification "i'd like to avoid using a compact menu. if we need to reduce row counts, we can explore that later, but a two-column menu is not a desired method" (the full form at every height). Plan §3.15's compact main menu is withdrawn. Proposals: [P-03](../refactor/proposals.md#p-03) (decided), [P-25](../refactor/proposals.md#p-25) (c), [P-29](../refactor/proposals.md#p-29) (a) (still open). Deviations: DEV-052 (gated), DEV-062, DEV-064 (ended), DEV-065 (new).
> **Status:** built and tested on macOS, in process: `pkg/menu`'s main menu driven through scripted stdin with its screens recorded, its status Figma's fixture or `pkg/screens/mainmenu` over the gallery's hosts (`hostfake`). No hardn binary ran; nothing touched this machine's `/etc`; nothing has run on Linux. The VM steps are §9, for the orchestrator. Item 6 (Dry-Run) and Run All's option 2 are still there: unit (d) removes them.

## 1. What is built

1. **The form.** hardn's main menu is `pkg/ui/screen`'s `MainMenu` whole, in its **full form at every terminal height and width**: the header, the distribution bar, the host block, the risk level, the status rows, the divider, "Select an option" with the mode indicator, origin/main's items and "Enter selection". On a terminal shorter than its lines its top scrolls off, the prompt on the last line.
2. **The compact form is gated**, not deleted: `screen.CompactMainMenu` is `false`, "DISABLED 2026-10-23. Flip to true to restore." (§4).
3. **The items and the loop.** `pkg/menu`'s main menu draws through `screen.MainMenu.Screen` and runs on `nav.Run`: origin/main's ten items and Exit, unchanged in content and numbering, each key opening exactly the screen it opened; a line no item takes refused under the prompt; the main menu drawn again under what an item printed (§2, §5).
4. **The indicator.** `component.ModeIndicator` says "Mode Live" or "Mode Plan only", and what set plan only, "(-n)" or "(hardn.yml)", from `config.Config.DryRunSource` (unit (b)) (§3).
5. **The mocks it designs are retired** from the gallery (gallery-dryrun.md §8): its §10 says where each is now.

## 2. On screen: today's live menu against this one, line by line

"Today" is `next/s2b-mode` (7347cbe): the status blocks over origin/main's `pkg/style` menu (DEV-064). The words of the items, the heading, the Exit row and the prompt are unchanged; what changes is where and how they are drawn, and what a key does around them.

| What | Today | Now | Source |
|---|---|---|---|
| The status blocks under 40 rows | compact: no header, no divider, the risk line and the rows one group | the full form at every height: the header, the divider and their gaps; 36 lines at 80×24 (§6) | decision 5 as adopted; DEV-052 gated |
| "Select an option" | col 2, underlined, SGR 246 | col 2, underlined, `Menu.Heading` (`text.muted`) | decision 5 (theme colours) |
| The indicator | "Dry-run Mode" (SGR 2) "Disabled" (SGR 33) or "Enabled" (SGR 32), col 26, a trailing space | "Mode" (`text.muted`) "Live" (`status.warn`) or "Plan only" (`status.ok`), then "(-n)" or "(hardn.yml)" (`text.faint`), col 27 | decisions 2, 3, 5; DEV-065 |
| The blank line under the heading | one | one | – |
| Items 1 to 10 | keys bold, ending col 4; labels col 6; descriptions col 26, SGR 2 | keys bold (`Menu.Key`), ending col 4; labels col 6; descriptions col 27 (`text.muted`) | decision 5 (col 27); DEV-020, DEV-027 |
| Item 1's "(sudo, SSH keys)" | part of the description, dim | the item's aside, `text.faint` | decision 5 |
| "0) Exit" "q to quit" | after a blank line, description col 26 | after a blank line, description col 27, `Menu.ExitKey`/`Menu.Exit` (`text.muted`) | decision 5; DEV-030 |
| Blank lines before the prompt | two | one (`theme.PromptGap`) | decision 5; drift D11 |
| The prompt | "❯ Enter selection [0-10 or q]:" at col 2, ❯ dim (SGR 2), the words bold, the range from `style.Menu.GetValidRange` and " or q" appended | the same words at col 2 (`PromptLine`: `Prompt.Glyph`, `Prompt.Label`, `Prompt.Hint`), the hint generated from the items and `keys.Navigation`, "[0-10 or q]" | decision 5; DEV-029; brief 1C |
| Glyphs and colours | pkg/style's ❯ and SGR codes whatever the terminal | the theme's tokens at the terminal's profile, `>` and the other ASCII forms on a terminal that is not UTF-8, none of SGR 2 | decision 5 (ASCII forms); DEV-009, DEV-012 |
| A line no item takes (an empty line, `11`, `x`, `01`, `b`) | the screen cleared, the `~~~` rule (`utils.PrintHeader`), "✗ Invalid option. Please try again." (red), a blank line, "❯ Press Enter to continue...", Enter, then the screen cleared and drawn again, the status read again (`main_menu.go:538-544` at origin/main) | "⨳ Invalid option. Please try again." under the prompt (`(x)` in ASCII), `status.bad`, then the prompt asked again on the next line: no clear, no pause, the screen above kept, the status not read again | decision 5; P-25 (c); DEV-062 |
| After an item's screen returns | the screen cleared and the main menu drawn | a blank line, then the main menu drawn again under what the screen printed, its status read again; the legacy screens still clear as they open, and so does the DNS item | decision 5; brief 1C ("Don't clear the screen in a way that wipes the result") |
| hardn starts; 0 and q | the screen cleared; cleared and returned | the same | – |
| The end of the input; Ctrl-C | exit 130; SIGINT | the same (`endHardn`) | – |
| Typeahead | discarded after the prompt was written (`Answer`) | discarded before it is written (`prompt.Reader.Ask`), no window between | p1c-reader |

## 3. The indicator (decisions 2 and 3)

| Session | The indicator |
|---|---|
| live (no flag, no file, `dryRun: false`, `--dry-run=false`) | Mode Live |
| `-n` or `--dry-run` | Mode Plan only (-n) |
| hardn.yml's `dryRun: true` | Mode Plan only (hardn.yml) |
| plan only set inside the session (item 6, Run All's option 2, until unit (d)) | Mode Plan only |
| turned off inside the session, whatever started it | Mode Live |

`pkg/menu` maps `DryRun` and `DryRunSource()` of the configuration every legacy screen reads (`m.config`) to the view-model on every pass (`MainMenu.mode`), so the indicator is never a pass behind a toggle. The component draws no source for live, whatever it is given, so "Live (-n)" cannot be drawn. On a narrow terminal the label gives way first, then the source, each whole or left out, and the word last: from col 6 the source stays down to 27 columns. The label was cut with the ellipsis before; it is now whole or gone, as the adopted mock drew it (DEV-065).

`style.Menu.SetDryRunStatus` has one caller, `createMainMenu`, which hardn no longer draws: it stays, with its caller, as the record `pkg/menu`'s `main_menu/*` goldens render, until unit (d).

## 4. The gate

`screen.CompactMainMenu`, in `pkg/ui/screen/main.go`: "DISABLED 2026-10-23. Flip to true to restore." With it `false`, `MainMenu.Render`, `Screen` and `StatusBlocks` never choose the compact form. The code stays as it was: `render`, `screen` and `statusBlocks` take the gate as a parameter, which those three pass the constant and the tests pass `true`, as `TitleTrail`'s tests do; `component.Menu.Compact`, `nav.Screen.Compact`, `theme.CompactBelow` and `CompactBudget` are unchanged.

How each existing compact test keeps running: `TestCompact`, `TestStatusBlocks`, `TestScreenMenu` and `TestCompactForm` call `render`, `statusBlocks`, `screen` and `compact` with the form allowed and hold what they held; `TestHeights` records both forms side by side, what is drawn following the constant; `TestScreen` holds `nav.Run` to `Render` in every cell, now the full form, and, with the form allowed, `screen`'s drawing to `render`'s, the compact form in the 24-row cells and at 60×40; `nav`'s `TestRunCompact` and the component's `menu/compact` golden are untouched. `TestFullAtEveryHeight` holds the gate (1 to 130 columns, six heights, on and off a terminal). The goldens: `main-menu/00-tui-main` draws the full form in every cell, and its cells before the gate are kept byte for byte as `main-menu/00-tui-main-compact`, drawn with the form allowed. **Flipped**, `Render` draws what that file holds and only golden comparisons fail (checked in a copy of the tree): `pkg/ui/screen`'s `TestGolden`, `pkg/menu`'s `TestLiveMainMenuGolden` and five of the gallery's (`TestPlayGolden`, `TestPlayDryRunGolden`, `TestPlayStatusGolden`, `TestStatusGolden`, `TestScreensRenderAsGoldens`); regenerate them with `make golden-update`, then `go test ./pkg/menu -update`, whose goldens are its own, and `make check` passes. `TestFullAtEveryHeight` and `pkg/menu`'s `TestMainMenuHeights`, which pin the full form, skip. The gallery's 24-row terminal cells now draw the full form; `pkg/screens/mainmenu`'s `TestForm`, which tested the form `Status` drew, went with `Status`'s drawing (§7).

`StatusBlocks` has no caller in hardn since this unit; it is kept with the compact form it can choose.

## 5. The loop

`ShowMainMenu` clears the screen once, as hardn starts, then runs one **pass** per drawing: it asks for the terminal (`SetTerminal`, `stdoutEnv` in `cmd/hardn`, so each pass draws for the terminal as it is then), reads the status (`SetStatus`), fills the heading, the mode, the items and the prompt, and runs `nav.Run` on `MainMenu.Screen`. `nav.Run` draws, asks through a `nav.Prompter` whose reader is `input()`, the one `prompt.Stdin` every legacy read shares, refuses a line no item takes under the prompt and asks again, and runs the item's action. Each item's action ends the pass when its screen returns (`errAgain`), so the next pass reads the host and the terminal again and draws under what the screen printed, `nav.Run`'s blank line before it. 0 ends `nav.Run` with nil and q with `nav.ErrQuit`: the screen is cleared and the main menu returns, as before. Any other error, the end of the input or Ctrl-C at the prompt, a status not read, a write that failed, ends hardn through `endHardn`, as at a legacy prompt. The security-update alert still replaces the main menu at the top of a pass (unreachable while V26's gate holds).

**Why `nav.Run`**, not a loop of `pkg/menu`'s own: it is the grammar's loop (brief 1C), already tested, that the gallery and the DNS screen run; `TestScreen` holds what it draws to `Render`; it brings the refusal, the generated hint and the gap before a redraw with it. A pass per drawing, rather than one `nav.Run` for the session, keeps the status and the terminal read on every pass, as the live menu read them, and the alert's check per pass.

| Key | Opens (unchanged) |
|---|---|
| 1 | User Management (`NewUserMenu`) |
| 2 | SSH Login (`NewDisableRootMenu`) |
| 3 | DNS, the DNS screen (`SetDNS`), from a cleared screen |
| 4 | Firewall (`NewFirewallMenu`) |
| 5 | Backup (`NewBackupMenu`) |
| 6 | Dry-Run (`NewDryRunHandler`), until unit (d) |
| 7 | Run All (`NewRunAllHandler`), its option 2 until unit (d) |
| 8 | Environment (`NewEnvironmentSettingsMenu`) |
| 9 | System Details (`NewSystemDetailsMenu`) |
| 10 | Logs (`NewLogsMenu`) |
| 0, q, Q | nothing: the screen cleared, hardn returns |
| empty, `11`, `01`, `x`, `b`, `-1`, `1 0`, `100` | refused under the prompt |
| the end of the input; Ctrl-C | exit 130; SIGINT |

Spaces around a key are ignored and letters in any case, as origin/main's read had it.

## 6. Heights

Measured on hardn's main menu (`TestMainMenuHeights`, `pkg/menu`, Unicode and ASCII alike) over the gallery's hardened Debian 13 host: eight rows, no line under any; each line under a row, and the host block's reboot line, adds one.

| Terminal | Lines | Scrolled off (the prompt on the last line) |
|---|---|---|
| 80×24 | 36 | the first 12: the header to the SSH Login row, **the risk level among them** |
| 80×40 | 36 | none |
| 60×24 (rows stacked, DEV-021) | 59 | the first 35: every status block, the heading, the mode and item 1's label |
| 60×40 | 59 | the first 19: the header to SSH Auth's label |

Today's live menu took 32 lines at 80×24, its top 8 off, the risk level among them, and 37 at 80×40 ([menu-live.md](menu-live.md) §6). Unit (d)'s removal of item 6 takes one line off, 35 at 80 columns, 57 at 60 (the gallery measured it, gallery-dryrun.md §3.2). Fewer rows are for later, by other means than a two-column menu (the maintainer, 2026-10-23).

## 7. API

| Package | Added or changed |
|---|---|
| `pkg/ui/component` | `type ModeSource` (`ModeSourceNone`, `ModeSourceFlag`, `ModeSourceFile`); `ModeIndicator.Source`; the words |
| `pkg/ui/theme` | `ModeStyles.Source` (`text.faint`): 105 slots |
| `pkg/ui/screen` | `const CompactMainMenu = false`; `render`, `screen`, `statusBlocks` and `compact` take the gate |
| `pkg/screens/mainmenu` | `Status(ctx, Options) (screen.MainMenu, error)`: the view-model, the status blocks filled, where it returned their rendering for a menu of `below` lines; `Options.Env` removed |
| `pkg/menu` | `SetStatus(func(ctx) (screen.MainMenu, error))`, its type changed with `Status`'s; `SetTerminal(func() term.Env)`, new; `handleMenuChoice` replaced by `mainMenu` (a `nav.Menu`), `legacyScreens` (each item's constructor) and the seam `openScreen`; `showDNS` returns the item's error; `createMainMenu` kept, not drawn |
| `cmd/hardn` | `readStatus` in place of `drawStatus`; `withStatus` sets the terminal too (`stdoutEnv`), on any `statusMenu` (the two setters, `*menu.MainMenu`'s), so that a test records both |

The status pass is menu-live.md's: the same reading on every pass, through `statusHost`, read only, its errors and Ctrl-C as before; only what it returns changed, the view-model rather than its text, so that the layout is drawn whole by one `Render`, through one writer. No new flag, key or module; no depguard change (`pkg/menu` already reached `pkg/ui`).

## 8. Words

The indicator's are the maintainer's, adopted (decisions 2, 3); the main menu's are origin/main's. **Proposed** where new.

| Where | Words | Source |
|---|---|---|
| Indicator | "Mode" "Live"; "Mode" "Plan only", "(-n)", "(hardn.yml)" | adopted (decisions 2, 3) |
| Main menu | "Select an option", the ten items and their descriptions, "0) Exit" "q to quit", "Enter selection", "[0-10 or q]" | origin/main (P-29 (a) open); DEV-030 |
| Refusal under the prompt | "⨳ Invalid option. Please try again.", `(x)` in ASCII | origin/main's words (P-25 (c)); the glyph DEV-062 |
| docs/configuration.md, "`dryRun`: plan only" | "**The main menu says so.** Beside "Select an option" it shows "Mode Plan only (hardn.yml)", or "(-n)" when the flag set it." | **proposed** |
| docs/releases/unreleased.md | "**The main menu shows the mode** …"; "## Changed: the main menu", two bullets (§2's refusal and redraw) | **proposed** |
| The gallery's `-height` help | "The main menu takes its full form at every height, its top scrolling off a shorter terminal (its compact form is gated: screen.CompactMainMenu)" | the gallery's, not hardn's |

## 9. The VM steps (for the orchestrator; not run)

On Debian 13, Ubuntu 26.04 and Alpine 3.23 ([vmtest.md](../safety/vmtest.md)), as root (Alpine through doas), hardn and p4status built from this branch (`CGO_ENABLED=0 GOOS=linux go build -trimpath -tags=netgo ./cmd/hardn` and `./internal/tools/p4status`, the VM's `GOARCH`), in tmux (`tmux new -x 80 -y 40`):

1. **Before:** `cp -p /etc/hardn/hardn.yml /root/hardn.yml.before 2>/dev/null`; `sha256sum /etc/hardn/hardn.yml /etc/resolv.conf /etc/ssh/sshd_config.d/* 2>/dev/null > /root/menu-layout-before`; `touch /root/menu-layout-stamp`.
2. **Live, 80×40:** `./hardn`; `tmux capture-pane -p > /root/layout-40.txt`; then `q`, `echo $?` 0, the screen cleared. Expect the header, the bar, the host block, the risk level, the rows, the divider, "Select an option         Mode Live" (the mode at col 27), the ten items and "0) Exit", one blank line, "❯ Enter selection [0-10 or q]:": 36 lines with no line under a row. `./p4status -profile none -width 80 -height 40` draws the same lines from the header to the prompt, blank lines and all (p4status draws the gallery's main menu, whose items, heading, mode and prompt are hardn's; `TestLiveStatusOnHosts` holds them equal): only the version and the hostname's form differ, as in menu-live.md §10.
3. **Live, 80×24:** `tmux resize-window -x 80 -y 24`, `./hardn`, `tmux capture-pane -p -S -40 > /root/layout-24.txt`: the same 36 lines, the full form (header and divider), the first 12 above the visible 24, the risk level among them, the prompt on the last line.
4. **An invalid key:** at the prompt `x`, Enter: "⨳ Invalid option. Please try again." in red under the prompt, the prompt again under it; the screen not cleared, no "Press Enter". Then Enter alone, then `11`: the same each time.
5. **An item and back:** `9` (System Details): its screen clears and draws; leave it as it asks (its 0, or Enter); the main menu is drawn under it, a blank line between, the screen's output above it in the scrollback. Then `3` (DNS), `0`: the same. `q`.
6. **`-n`:** from `/root`, `./hardn -n`: "Mode Plan only (-n)". `6`, through the Dry-Run screens, turn it off (1): back at the main menu, "Mode Live"; `6` again, turn it on: "Mode Plan only", no source. `q`. Item 6 saves `./hardn.yml` in the working directory, `dryRun` as loaded (unit (b)): remove `/root/hardn.yml` after.
7. **hardn.yml:** set `dryRun: true` in `/etc/hardn/hardn.yml` (create one with `nameservers: []` and `dryRun: true` where none exists, noting it); `./hardn`: "Mode Plan only (hardn.yml)"; `q`; `./hardn --dry-run=false`: "Mode Live"; `q`. Restore the file from step 1 (or remove the one created).
8. **Ends:** `./hardn`, Ctrl-D at the prompt: exit 130; `./hardn`, Ctrl-C at the prompt: ended by SIGINT, 130 in bash, the terminal sane; `./hardn`, `0`: cleared, 0.
9. **ASCII:** `LANG=C ./hardn` (or `TERM=linux`): `>` for the prompt glyph, `[+]` markers, and `x` answered "(x) Invalid option. Please try again.".
10. **After:** `sha256sum -c /root/menu-layout-before` (only what step 7 changed, before its restore); `find /etc /var/lib /root -newer /root/menu-layout-stamp` lists nothing the main menu wrote, only step 6's `/root/hardn.yml` and the captures (on Alpine, mind busybox find's whole seconds, menu-live.md §10.1).

Record per VM: the line counts at 80×40 and 80×24, the heading line, each indicator state, and the time to the menu as menu-live.md §10.2 did.

## 10. Tests

- `pkg/menu`: `TestMainMenuKeys` (the dispatch table: every key, 0, q, Q, spaces, an empty line, seven keys no item has, the end of the input, Ctrl-C as the reader reports it; which screen each key built, held to the constructor and arguments origin/main's `handleMenuChoice` used, transcribed in the test (`originScreens`: the same type, its fields the same pointers); the refusals, how often the main menu was read and drawn, the clears, no pause), `TestOpenScreen` (the seam's default opens what was built), `TestMainMenuInterrupt` (Ctrl-C by SIGINT at the prompt, unix), `TestMainMenuInvalid` (⨳ and `(x)`, the prompt again, no clear, no pause, not drawn again), `TestMainMenuAgain` (the main menu drawn again under an item's output, uncleared, the same drawing), `TestMainMenuMode` (the five sessions of §3, and item 6's toggles), `TestMainMenuItems` (the items are `createMainMenu`'s and the main-menu mock's, the heading, prompt and hint origin/main's), `TestMainMenuHeights` (§6, Unicode and ASCII), `TestMainMenuDraw` (at 80×24 in colour, exactly `Render` of the mock), `TestLiveStatus`, `TestLiveStatusEnds` (no terminal set: a panic too), `TestSecurityAlertLive`, `TestLiveStatusOnHosts` (the whole main menu line for line the gallery's, header to prompt, on five hosts), `TestDNSItem` (driven through the loop now). `TestScreens` lost its "invalid main-menu choice" case, the pause it held being gone. Mutation-checked (`go test -overlay` on `main_menu.go`): two items' constructors swapped in `legacyScreens`, an item built with another item's constructor (Logs with Environment's, Dry-Run's handler with Run All's), with an argument nil or another value (the OS, the host-info manager, a fresh configuration), two items' bindings swapped in `mainMenu`, the source dropped, the mode inverted, the first or a per-pass clear, the DNS item's return, the exit's clear and the aside folded into the description each fail at least one of them.
- `pkg/ui/component`: `TestLayout`'s mode and heading cases, `TestModeWord` (every state at 1 to 130 columns).
- `pkg/ui/screen`: `TestFullAtEveryHeight`, `TestHeights`; the compact tests of §4.
- `pkg/screens/mainmenu`: `TestNothingRead`, `TestCancelled` on the view-model; `cmd/hardn`: `TestReadStatus`, `TestWithStatus` (both hooks set, the status `readStatus`'s and the terminal `stdoutEnv`'s, asked on each pass; mutation-checked, the terminal not set or the version changed fail it).

## 11. Goldens

- `pkg/ui/component`: `modeindicator/live`, `plan-only` (was `dry-run`), `plan-only-flag`, `plan-only-file` (new); `menuheading/normal`, `long` (plan only from hardn.yml now).
- `pkg/ui/nav`: `nav/main-menu`, `nav/rounds`: the indicator's line.
- `pkg/ui/screen`: `main-menu/00-tui-main`: the indicator's line, then the full form in the 24-row cells and at 60×40; `main-menu/00-tui-main-compact` (new): the earlier cells, byte for byte.
- `pkg/menu`: `main_menu_live/debian13-80x24`, `-80x40`, `hardened-80x24`, `-80x40`: the whole main menu on the layout, 36 lines, the same at 24 and 40 rows (each was the blocks over pkg/style's menu, 32 and 37 lines); `main_menu/*` (origin/main's screen, the legacy record) untouched.
- The gallery: the status hosts' and play's main menus (`status/*`, `play/*`), the indicator's line and the full form in their 24-row cells; the retired mocks' goldens deleted (gallery-dryrun.md §10).
- No characterization golden changed.

## 12. Not in this unit: unit (d)

Item 6 removed, its key refused as any key no item has; `dry_run_handler.go` and `dry_run_menu.go` deleted, their characterization goldens frozen; Run All's option 2 removed; User Management, Backup and Firewall's profile add and remove refused under plan only, "Not available in plan only." (decision 6, proposed); `createMainMenu` and `style.Menu.SetDryRunStatus` with their last use; the gallery's remaining Dry-Run mocks (`screen`, `screen-enabled`, `toggle`, `legacy-firewall`, `play dryrun`, `play dryrun/saved`) retired; the release notes' item-6 line made current.

## 13. Open

1. **The maintainer:** fewer rows on the main menu, by other means than two columns (the maintainer's "later"); at 80×24 the risk level scrolls off, as it did.
2. `StatusBlocks` has no caller in hardn; it goes if the compact form is removed rather than gated.
3. After a first-run question (hardn.yml's creation), the first drawing starts one blank line under the cleared screen's top (`nav.Run`'s gap once a prompt was asked). Cosmetic; not changed.
4. Not verified on a host: everything of §9; nothing has run on Linux.

## 14. Size

Against `next/s2b-mode` (`git diff --numstat 7347cbe`): about 535 lines of non-test Go added and 550 removed (`pkg/menu` 205 and 150: the loop, the items, the hook; `pkg/ui/screen` 105 and 55: the gate, much of it comments; `pkg/ui/component` 80 and 35: the indicator; the gallery 85 and 250, most of the removal the retired mocks; `pkg/screens/mainmenu` 25 and 35; `cmd/hardn` 20 and 15; the theme 10); about 800 lines of tests added and 415 removed (the dispatch table, the heights, the mode in each source, the gate's tests, the live menu's tests on the new hook, the retired mocks' tests); and about 210 lines of docs, this page most of them. **Over the guideline** of about 500 changed lines: the unit is four commits, each building and passing on its own (the indicator, the gate, the main menu on its layout, the mocks' retirement), and a third of the change is code removed. Testdata: 44 golden files changed, added or deleted (2,350 lines added, 2,895 removed): §11.
