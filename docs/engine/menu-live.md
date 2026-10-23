# The main menu goes live, with the DNS row (`next/s2-menu-live`)

> **Unit:** refactor plan slice 2, its main-menu unit, `next/s2-menu-live` (plan §2, "Phases 3–5 together": "Main menu goes live when all three are in place: slice 2, `p1c-legacy-input` and the `p4-check-*` units"; all three are merged in `next/v0.4`). Decisions implemented: the maintainer's of 2026-10-23, "Main menu goes live" (`cmd/hardn`'s main menu draws its host block, status rows and risk level from the controls, as the gallery and p4status do, the DNS row included; the legacy security status gated, not deleted) and "Shown, not weighed" (the DNS row weighs 0, so no host's risk level changes); slice 2 decision 8 (the DNS row's design: [gallery-dns.md](gallery-dns.md) §4; [P-01](../refactor/proposals.md#p-01)'s status line, its DNS part agreed).
> **Status:** built and tested on macOS against `hostfake` and the gallery's model only. Nothing has run on Linux: every host claim is "written, not run (gate 5)". No new flag, environment variable, configuration key or module.

## 1. What goes live

hardn's main menu has drawn its host lines, status rows and risk level from the legacy `pkg/security` (`CheckSecurityStatus`, `GetSecurityRiskLevel`, `DisplaySecurityStatusWithCustomPrinter`), with rows and a score the status model of Phase 4 replaced. The gallery's status hosts ([gallery-status.md](gallery-status.md)) and `internal/tools/p4status` ([vmcheck.md](vmcheck.md)) already drew the main menu from the controls' checks, the header probe and the risk level; this unit gives them one list of the status controls with the DNS row in it, and makes hardn's main menu draw the same blocks.

## 2. One list of status controls

`controls.Status(cfg)` (`pkg/engine/controls`, a package of its own above the areas' packages): the five areas' `Controls()` in Figma's order (User, Firewall, SSH Login, SSH Auth, SSH Port, AppArmor, Auto Updates), then `dns.Controls(cfg)`, checked against `hardn.yml` as loaded. `cmd/hardn`, the gallery (`readStatus`) and p4status (`read`) all import it; it replaces the gallery's and p4status's `statusControls` copies ("until slice 2 exports one list"). Every caller can import it without a depguard breach: it is under `controls-through-host`, and imports only the engine, the config and the areas' packages. `TestStatus` (`pkg/engine/controls`) holds it to `risk.Rows`, by ID and title, in order; p4status's `TestStatusControls` holds its production list to the rows.

**Where the DNS row goes: after Auto Updates, last.** Figma's 00-TUI-MAIN draws seven rows, in an order that is not the menu items' (Firewall is the second row and item 4), and P-01's "drive status rows and menu items from one list" is still proposed: putting DNS at item 3's place would reorder Figma's rows for a decision not made. Last, Figma's seven keep their places and lines, the risk lines included, and the order changes in one place, `controls.Status`, when P-01 is decided.

## 3. The risk level: shown, not weighed

`risk.Rows` gains DNS (`dns.nameservers`) after Figma's seven, **weight 0**; the seven keep their weights, still 100 together. `risk.Assess` leaves a row of weight 0 out of everything (`TestShownNotWeighed`): the share and the whole, whether any row is established (a DNS row alone established leaves the band Unknown), and the factors. So **a weight-0 row adds no factor and no "Counted as not configured" line**, which would be false of a row that is not counted. The score, the band, the factors and the lines under the rows are byte for byte the same on every gallery status host: the risk test runs every kind of host with a DNS report in each state, failed and absent, against the same host without it, and the gallery's status goldens gained the DNS row and nothing else (§4). [risk.md](risk.md) §5.1 and §5.2 say it.

**An Unknown DNS row** (a check that failed, or a fact `dns.Read` did not establish) shows its state, "?" in `text.muted`, "Unknown", and the check's own detail, "can't read" ([control-dns.md](control-dns.md) §5), with nothing under it; a DNS row with no report at all shows "Unknown" with no detail. Never a guess: the control returns Unknown with the read's error for anything not established.

## 4. The DNS row

From the DNS control's `Check`, as every row is from its control's (`output.StatusViewOf`: title, state's glyph and word, detail): Configured when the nameservers in use are `hardn.yml`'s, in order (the list, "192.0.2.54, 2001:db8::54"); Not Applicable (–) for the managers hardn only reports (NetworkManager, resolvconf, another program), a container and a family hardn does not configure; Not Configured otherwise ("none in hardn.yml", "not hardn.yml's", "not valid in hardn.yml", "resolved not running"); Unknown, "can't read", when not read. **Every word is [control-dns.md](control-dns.md) §5's**, proposed there; this unit adds none.

**The gallery's status hosts gain the row.** Each is checked against its `hardn.yml` as hardn loads it (`statusConfig`: `/etc/hardn/hardn.yml` read through the host, or hardn's defaults, listing no nameserver, with none); the hardened host, and so `figma`, `lockout` and `lockout-at-start`, now also has DNS as Configure DNS leaves Debian 13 (`dnsConfigured`: `hardn.yml` lists 192.0.2.54 and 2001:db8::54, and dhcpcd's `/etc/resolv.conf` lists them). Their notes are unchanged. The goldens changed by the DNS row alone, in every cell (no line removed, none changed; [gallery-status.md](gallery-status.md) §5 has the column):

| Golden (`internal/tools/uigallery/testdata/golden/`) | The DNS row added |
|---|---|
| `status/debian13`, `ubuntu2604`, `alpine324`, `pve9`, `fresh`, `ubuntu2604-cloud`, `ubuntu2604-iso`, `reboot`, `apparmor-logging`, `apparmor-restart`, `apparmor-kernel`, `apparmor-unread` | ⭘ DNS Not Configured *none in hardn.yml* (`[ ]` in ASCII) |
| `status/hardened`, `figma`, `lockout`, `lockout-at-start`; `play/status/lockout`; `play/status/menu` (Figma's host played) | ⛛ DNS Configured *192.0.2.54, 2001:db8::54* (`[+]`) |
| `status/container` | – DNS Not Applicable *container* (`[-]`) |

`pkg/output`'s `human/status` golden gained the row too: its fixtures now carry a DNS report (Configured on Figma's, the hardened, reboot-pending and lockout hosts, Not Applicable in the container, Unknown "can't read" with nothing under it on the host with nothing read). **p4status prints it**: the DNS check's commands are on its allowlist, it reads `hardn.yml` where hardn looks for it, and its fixtures were exported again from the gallery (each gains the DNS reading's commands and files: [vmcheck.md](vmcheck.md), amended).

## 6. The heights, with one row more

Measured by `TestHeights` (`pkg/ui/screen`), on a terminal, Figma's fixture with no line under a row (each line under a row, and the host block's reboot line, adds one to every figure), Unicode and ASCII alike:

| Rows | Terminal | Form drawn | Lines | hardn's live main menu (§5) |
|---|---|---|---|---|
| Figma's 7 | 80×24 | compact | 24, `CompactBudget` | 31 |
| Figma's 7 | 80×40 | full | 35 | 36 |
| 7 and DNS | 80×24 | compact | **25**, one over the budget | 32 |
| 7 and DNS | 80×40 | full | 36, fits | 37, fits |
| 7 and DNS | 60×24, 60×40 | compact (the rows stack, DEV-021) | 36 (59 in full) | 42 |

**The compact form no longer fits 24 lines** with the DNS row: at 80×24 its first line, the distribution bar, scrolls off; the host, the risk level, every row, the mode and every item stay on screen (what DEV-052 keeps). With a line under a row it already took 25 (Figma's own host, [gallery-status.md](gallery-status.md) §5 (a)); now 26. **Nothing is changed here**: which line gives way is a layout decision the maintainer made before (DEV-052, 2026-10-17: the bar keeps its blank line, the heading gives up the one under it), and the cap on the lines under the rows was left to slice 2 (risk.md §8 (i)). Open for the maintainer (§11): (a) accept, the bar scrolling off at 24 rows; (b) the compact form drops the blank line between the host block and the risk group (24 lines, the uptime over the risk line); (c) it drops the blank line before the prompt; (d) a cap on the lines under the rows in the compact form, for the hosts that have them. The full form at 40 rows is unaffected while a host has at most four lines under its rows.

**hardn's live main menu** draws the status blocks above its legacy menu, 17 lines from the heading to the prompt (§5), so it fits 24 rows in neither form, as origin/main's did not (37 lines, 38 with its "Sudo Not Installed" row: `pkg/menu`'s goldens; §7): at 80×24 the compact blocks (32 lines) leave its top 8 lines scrolled off, the risk level among them, where origin/main's left 13 or 14 off, the risk level among them too. From 40 rows its full blocks fit (37 lines) with up to three lines under the rows. `MainMenu.StatusBlocks` chooses the form by DEV-052's rule for that screen (`TestStatusBlocks`).
