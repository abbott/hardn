# The main menu goes live, with the DNS row (`next/s2-menu-live`)

> **Unit:** refactor plan slice 2, its main-menu unit, `next/s2-menu-live` (plan §2, "Phases 3–5 together": "Main menu goes live when all three are in place: slice 2, `p1c-legacy-input` and the `p4-check-*` units"; all three are merged in `next/v0.4`). Decisions implemented: the maintainer's of 2026-10-23, "Main menu goes live" (`cmd/hardn`'s main menu draws its host block, status rows and risk level from the controls, as the gallery and p4status do, the DNS row included; the legacy security status gated, not deleted) and "Shown, not weighed" (the DNS row weighs 0, so no host's risk level changes); slice 2 decision 8 (the DNS row's design: [gallery-dns.md](gallery-dns.md) §4; [P-01](../refactor/proposals.md#p-01)'s status line, its DNS part agreed).
> **Status:** built and tested on macOS against `hostfake` and the gallery's model only. Nothing has run on Linux: every host claim is "written, not run (gate 5)", and the VM steps are §10, for the orchestrator. No new flag, environment variable, configuration key or module.

## 1. What goes live

hardn's main menu has drawn its host lines, status rows and risk level from the legacy `pkg/security` (`CheckSecurityStatus`, `GetSecurityRiskLevel`, `DisplaySecurityStatusWithCustomPrinter`), with rows and a score the status model of Phase 4 replaced. The gallery's status hosts ([gallery-status.md](gallery-status.md)) and `internal/tools/p4status` ([vmcheck.md](vmcheck.md)) already drew the main menu from the controls' checks, the header probe and the risk level; this unit gives them one list of the status controls with the DNS row in it, and makes hardn's main menu draw the same blocks.

## 2. One list of status controls

`controls.Status(cfg)` (`pkg/engine/controls`, a package of its own above the areas' packages): the five areas' `Controls()` in Figma's order (User, Firewall, SSH Login, SSH Auth, SSH Port, AppArmor, Auto Updates), then `dns.Controls(cfg)`, checked against `hardn.yml` as loaded. `cmd/hardn` (through `pkg/screens/mainmenu`, §5), the gallery (`readStatus`) and p4status (`read`) all read it; it replaces the gallery's and p4status's `statusControls` copies ("until slice 2 exports one list"). Every caller can import it without a depguard breach: it is under `controls-through-host`, and imports only the engine, the config and the areas' packages. `TestStatus` (`pkg/engine/controls`) holds it to `risk.Rows`, by ID and title, in order; p4status's `TestStatusControls` holds its production list to the rows.

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

## 5. Live: the main menu draws the status model

**`legacySecurityStatus`** (`pkg/menu/main_menu.go`): `DISABLED 2026-10-23. Flip to true to restore.` With it false, each pass of `ShowMainMenu` draws the status model's blocks above its items; flipped, origin/main's block comes back, its code untouched (`showLegacyStatus`, the old loop's lines moved into a function, `displaySecurityStatus`, `displayNormalSecurityStatus`, `formatHardnVersionLine`, `formatHostInfoLines`, `formatOSTitle`, and `pkg/security` unchanged). The legacy side is still tested where it was: `TestMainMenuRenderGolden` and its four goldens, unchanged, render that block from the same functions; the gate only chooses which block a pass draws. The security-update alert, which replaces the block when an update check finds a security release, is drawn on either side, as origin/main drew it (unreachable while `pkg/version`'s `updateCheckEnabled` is false, V26).

**How `pkg/menu` reaches the model: a hook `cmd/hardn` sets**, `MainMenu.SetStatus(func(ctx context.Context, below int) (string, error))`, as slice 2's `SetDNS` reaches the DNS screen, so that `pkg/menu` runs no check and builds no engine (it imports neither). `cmd/hardn` (`status.go`, `withStatus`, from `main.go:190`) gives it `drawStatus`, which calls **`pkg/screens/mainmenu.Status`**, the main menu's glue, as `pkg/screens/dns` is the DNS screen's: `engine.Check` over `controls.Status(cfg)` with the facts of `engineDetect` (detect.Detect), `header.Read` on the same facts, `risk.Assess`, then `output.MainMenuOf` and `screen.MainMenu.StatusBlocks`, rendered for `stdoutEnv()`'s terminal and written through its writer, the gap before the menu included: the same mapping and screen as the gallery and p4status. `cfg` is the configuration the main menu holds, so the DNS row follows each list the DNS screen saves into it. The alternatives: `pkg/menu` assembling the engine (the task's rule against it), or `cmd/hardn` passing a finished view-model each pass (a second hook for the terminal's size); a package of its own lets `pkg/menu`'s tests draw the main menu with the real status over the gallery's hosts, which a hook body in `cmd/hardn`'s `main` package would not.

**Every pass reads the host again**, read only. `Engine.Check` checks, never plans or applies; the engine's signal hold, which only an apply takes, is never taken; the host is **`statusHost`, a `host.Real` with no run log**: on every pass the run log would put each command and its whole output (sshd -T's configuration among them) in `/var/log/hardn.log`, which origin/main's checks never did; the run log stays with `engineHost`, for what changes the host (`-g`, the DNS screen). **Proved on `hostfake`** (`TestLiveStatusOnHosts`, `pkg/menu`): over the gallery's Debian 13, Ubuntu 26.04, Alpine 3.24, Proxmox VE 9 and hardened Debian 13 (p4status's fixtures), every call a pass makes is a read or a command the host's fixture answers that is one of the queries of [vmcheck.md](vmcheck.md) §2.5, and nothing is written; `TestDrawStatus` (`cmd/hardn`) and `TestNothingRead` (`pkg/screens/mainmenu`) likewise.

**The cost, per pass, measured** (`TestLiveStatusOnHosts` pins the commands; the file operations from a probe of the same reading): Debian 13 28 commands, Ubuntu 26.04 31, Alpine 3.24 13, Proxmox VE 9 23, hardened Debian 13 28; about 85 to 125 file operations besides (`stat`, `lstat`, `read`, `readdir`). origin/main's pass ran "about 6–10 subprocesses" ([side-effects.md](../refactor/phase0/side-effects.md) §4.5, **[I]**, not counted by a test). The time a pass takes on a host is not measured here (§10). The binary grows by 747,217 bytes on linux/amd64 (15,257,403 against 14,510,186, `go build -trimpath -tags=netgo`): the five checks, the header probe and the risk level are now linked into hardn.

**A check that fails or a fact not established** is its row's Unknown (`Engine.Check`), never a guess, and the status is drawn: on a host where nothing can be read every row is Unknown, each weighed one with "Counted as not configured" under it, DNS with "can't read" and nothing under it, the risk level Unknown, the host block's facts "Unknown" (`TestNothingRead`). The reading's only error is its context's (detection fails with nothing else), returned before anything is drawn.

**Ctrl-C during the checks** ends hardn as the menu's Ctrl-C does today, by SIGINT. No handler is registered while they run (the prompt reader registers one only while it reads), so Go's default ends hardn at once, by the signal with which `prompt.Reraise` ends it at the menu's prompt, and the command a check is running, in hardn's process group, gets the terminal's SIGINT too; nothing is half done, since a check writes nothing. As during origin/main's checks; and no handler means nothing can keep a hung read from being interrupted. A hook that returns a cancelled context's error ends hardn the same way (`endHardn`, `prompt.ErrInterrupted`), any other error as at a legacy prompt, printed, exit 1 (`TestLiveStatusEnds`).

**The form**: the blocks' full or compact form for the screen they make with the legacy menu, 17 lines from its heading to its prompt (`StatusBlocks`, §6). The menu's items, numbering, prompt, dispatch and dry-run mode indicator are origin/main's, drawn as before (slice 2b's).

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

## 7. What going live changes

Each line of origin/main's block (`pkg/menu` `displayNormalSecurityStatus`, `pkg/security` `CheckSecurityStatus`, `GetSecurityRiskLevel`, `DisplaySecurityStatusWithCustomPrinter`; `pkg/menu`'s legacy goldens draw it) against the live block, each difference tied to its source. **Open** marks a legacy line or behaviour the live block drops that no decision covers: listed for the maintainer (§11), with where the model carries the fact, if anywhere.

| origin/main | Live | Source |
|---|---|---|
| `hardn v0.4.0` and the repository URL | the same, the `Header` component; the URL at column 27 | DEV-017; the version is `cmd/hardn`'s, as before |
| With an update found: `v0.4.1 available` in place of the URL, and the URL on a second line | **not drawn**: the `Header` has no notice | **Open**; unreachable while update checks are off (`updateCheckEnabled`, V26); P-15 |
| The security-update alert in place of the block, then Enter ends hardn | the same, origin/main's code, on either side of the gate | kept (§5); unreachable while V26's gate holds |
| The OS bar, from `osdetect`: "Debian 13 (Trixie)", "Alpine Linux 3.23.0" | the `ContextBar` from the header probe (os-release): "Debian 13 (Trixie)", "Alpine 3.24.2", "Proxmox VE 9.0.10 (Trixie)" (origin/main showed Debian there) | `next/p4-header`, [header.md](header.md) §5 |
| The host line: hostname badge and the first address of `GetHostInfo`, "Unknown" without one; the whole host block left out when it failed | hostname badge and the primary address (the default route's), "no default route", "no address", or "Unknown"; always drawn | `next/p4-header`, header.md §2, §5.2 |
| The uptime line, `MenuManager.FormatUptime` | `output.FormatUptime`, Figma's form ("182 days, 2 hours, 26 minutes", a unit of 0 left out); then "- Reboot pending", or "- Can't tell if reboot pending" | `next/p4-header`; P-04 (approved, implemented); P-21 (decided 2026-10-21) |
| "⛬ Risk Level Moderate ≈ medium security": eight equal booleans counted, Minimal never reached | the same five words and glosses, from the weighted score; "Unknown" when no row is established; "Critical ≈ lockout risk" when the firewall refuses SSH; Minimal reachable | `next/p4-risk`, [risk.md](risk.md) §5, §6; P-06, P-14 (proposed, built as the plan row asks); DEV-016 |
| — | each factor a line under the row it is about ("Up 182 days, no updates", "No password to become root", "Counted as not configured") | P-06's place decided (2026-10-21), `next/p4-risk-lines` |
| "Users": "Configured non-root, sudo" (any entry in `/etc/sudoers.d`, or a member of `sudo` or `wheel`), "Not Configured root user only" | "User": "non-root, sudo", "non-root, doas", "non-root, no sudo" (Partial), "root only", "can't read", and the others of [check-users.md](check-users.md) §5 | `next/p4-check-users`; the label Figma's (P-13 (a), proposed) |
| "Sudo Not Installed", only when `which sudo` or `/etc/sudoers` fails | **no row**. The User row says it where there is a non-root account: "non-root, no sudo" (or "no doas") when no tool is installed setuid root; with root alone, "root only" says nothing of the tool | **Open** (P-13, proposed) |
| "Sudo Method": "Configured no password" (warning), "Configured password required", "Not Configured unknown", for the first non-root sudo user, or root | **no row**. A grant without a password is a line under User, "No password to become root" (`USERS_NOPASSWD`, established) or "Maybe no password to become root" (`USERS_RULE_NOT_READ`), and costs 5 (risk.md §5.3); "password required" is not said: a Configured User row with no such line | **Open** (P-13 (a): fold it into the User row's detail, proposed) |
| "Firewall": "Not Configured vulnerable", "Enabled configure policies", "Configured deny policy" (`ufw status verbose`, then `iptables -L`) | the firewall check: whether ufw, or pve-firewall on Proxmox VE, lets SSH through ("deny policy", "inactive", "ufw not installed", "enabled, not running", "ssh blocked", ...); no `iptables` fallback | `next/p4-check-firewall`; P-05 (approved, implemented); the plan's Phase 4 row |
| "SSH Login": root allowed or disallowed, from the `PermitRootLogin` line of `sshd_config`, or of `hardn.conf` | from `sshd -T`, the running configuration: "root disallowed", "root key-only" (Partial), "root allowed", "root locked" | `next/p4-check-ssh`, `next/p4-locked`; P-05 |
| "SSH Auth": "password auth enabled" or "key-only auth", from the same file | from `sshd -T`: "key-only auth", "password auth on", keyboard-interactive counted with PAM | `next/p4-check-ssh`, `next/p4-kbd-pam`; P-05 |
| "SSH Port": "default (22)" or "non-default (N)", N **hardn.yml's** `sshPort`, not the host's | the ports sshd listens on (`sshd -T`, ssh.socket, `ss`): "non-default (2230)", "default (22)", and the lines of [check-ssh.md](check-ssh.md) §5 | `next/p4-check-ssh`; P-05 |
| "AppArmor": "Configured" or "Not Configured", no detail (`aa-status`) | the eight agreed lines: "N profiles enforced", "logging only", "off after restart", "no profiles loaded", "off in kernel", "set by the host", "Alpine default", "can't read" | P-43 (3), decided 2026-10-20; `next/p4-check-apparmor` |
| "Auto Updates": "Configured" or "Not Configured", no detail (`dpkg -l` and `systemctl is-enabled unattended-upgrades`; Alpine's `/etc/periodic/daily/apk-upgrade`) | the Auto Updates check's lines: "unattended-upgrades", "no unattended-upgrades", "no apk-cron", "lists not refreshed", ... | `next/p4-check-updates`, [check-updates.md](check-updates.md) §5 |
| — | **"DNS"**, last: "Configured 192.0.2.54, 2001:db8::54", "Not Configured none in hardn.yml", "Not Applicable NetworkManager", "Unknown can't read", ...; weight 0 | this unit: the maintainer, 2026-10-23 ("Main menu goes live", "Shown, not weighed"); slice 2 decision 8; [control-dns.md](control-dns.md) §5 |
| The glyphs ⛛ ⭘ ⛬ in origin/main's colours | the adopted set, ⛛ ⭘ ? – and ⛬ on the risk level alone, with ASCII forms, in the theme's tokens | the maintainer, 2026-10-21 (`next/p4-status-glyphs`); the bands' colours DEV-016 |
| The box: the bar in its top shade, an empty row, the box's bottom rule | the main menu layout's blocks and `Divider`, each a `SectionGap` apart; under 40 rows, or where they do not fit with the menu, the compact form's | DEV-051, DEV-052; §6 |
| Each pass: `pkg/security`'s commands; `GetExtendedUserInfo` for Sudo Method, which reloads hardn.yml from the default path (ignoring `-f`; asking to create one when none exists), runs `su - $SUDO_USER -c 'echo $HARDN_CONFIG'` under sudo, and sets console logging silent for the rest of the session; on the first pass `osdetect.GetOS` detecting the OS again (`DetectOS` clears the screen and logs it), as `cmd/hardn`'s detection does not fill its cache | the status reading alone, through `host.System`, 13 to 31 commands (§5); no reload, no `su`, no prompt, no change to logging | [side-effects.md](../refactor/phase0/side-effects.md) rows 3, 4 and 5, §4.5; the `GetOS` pass read from the code, not observed. The console's output is unchanged: no console log call is reachable from the menu's items but through that same reload, which silences logging first (`pkg/logging`'s users, by `grep`) |

`pkg/menu`'s legacy goldens (`main_menu/*`, `run_all/*`) are unchanged: they render origin/main's block from its functions (§5). The characterization goldens are untouched; `main.go` keeps its line count, so none of their citations of it goes stale ([cmd-engine.md](cmd-engine.md) §8, decision 7).

## 8. Words and codes

**No new word on the main menu.** Every status line, level, gloss and factor is a check's, the header probe's or the score's, proposed on their pages; the DNS row's are [control-dns.md](control-dns.md) §5's. p4status's evidence gains two lines of its own, proposed ([vmcheck.md](vmcheck.md) §5.2):

| Where | Words (*proposed*) |
|---|---|
| p4status's evidence, first | `hardn.yml`, then `file <path>` |
| the same, none found | `file none found: hardn's defaults, no nameservers` |

No new code in `pkg/output`'s registry: the reading's one error is a cancelled context, which ends hardn as Ctrl-C does and is never shown; a main menu with no status set panics, "menu: the main menu has no status (SetStatus)", a programming error, as with `SetDNS`.

## 9. Tests

- `pkg/engine/controls`: `TestStatus` (the list is `risk.Rows`, by ID and title, DNS last, the configuration given).
- `pkg/engine/risk`: `TestShownNotWeighed` (a DNS report in every state, failed or absent, before or after the others, changes no level on six kinds of host; DNS alone established leaves the band Unknown, with no factor under it; probed to fail without the rule), `TestRows` (eight rows, DNS weight 0, the seven 100).
- `pkg/output`: `TestStatusViewOfFigma`, `TestStatusRows` (the DNS row in its place, a report of no row after it), golden `human/status`.
- `pkg/ui/screen`: `TestStatusBlocks` (at 1 to 130 columns, 0, 1 and 24 to 60 rows, menus of five heights: the form by DEV-052's rule for the caller's screen, each block what `Render` draws above its heading, Render's own form for its own menu, the full form with no terminal), `TestHeights` (§6).
- `pkg/screens/mainmenu`: `TestNothingRead`, `TestForm`, `TestCancelled`.
- `pkg/menu`: `TestLiveStatus` (the hook's text above the items on every pass, told 17 lines, a live context; no legacy line), `TestLiveStatusEnds` (Ctrl-C by SIGINT, another error exit 1, no hook a panic), `TestSecurityAlertLive`, `TestLiveStatusOnHosts` (five of the gallery's hosts: the status line for line the gallery's, its cost, read only), `TestLiveMainMenuGolden` (new goldens `main_menu_live/debian13-80x24`, `-80x40`, `hardened-80x24`, `-80x40`, written with `pkg/menu`'s own `-update`: the live screen on a 256-colour UTF-8 terminal, 32 and 37 lines).
- `cmd/hardn`: `TestDrawStatus` (the hook's reading through `statusHost`, the version, the DNS row following the configuration, read only), `TestStatusSeams`.
- The gallery and p4status: as before, over the one list (`TestStatusHosts`, the status goldens, `TestRunOverHosts`, `TestMainMenuAsGallery`, `TestStatusControls`, `TestRulesMatchTheCode`).

## 10. Written, not run (gate 5): the VM steps, for the orchestrator

Nothing here has run on Linux. On the VMs of [vmtest.md](../safety/vmtest.md), Debian 13, Ubuntu 26.04 and Alpine 3.23 (no 3.24 VM), with hardn and p4status built from this branch (`CGO_ENABLED=0 GOOS=linux go build -trimpath -tags=netgo ./cmd/hardn` and `./internal/tools/p4status`, the VM's `GOARCH`), as root (Alpine through doas), in a terminal whose size is set (tmux: `tmux new -x 80 -y 40`):

1. **Before:** `sha256sum /etc/resolv.conf /etc/hardn/hardn.yml /etc/ssh/sshd_config.d/* 2>/dev/null > /root/menu-live-before`; `grep -c ': \$ ' /var/log/hardn.log` (the run log's command lines); `touch /root/menu-live-stamp`.
2. **p4status:** `./p4status -profile none -width 80 -height 40 > /root/p4status.txt` (Unicode glyphs, no colour, as `hardn` draws them on a UTF-8 terminal without colour); exit 0.
3. **The live main menu at 80×40:** `./hardn` in the 80×40 tmux window; at the main menu, `tmux capture-pane -p > /root/menu-40.txt` (the text, without escapes), then `q`. Compare `menu-40.txt` from the header to the rule with p4status's main menu, line for line: the header's version differs (p4status shows the mock's `v0.4.0`, hardn its build's), every other line the same, **the DNS row included**. The full form is expected (37 lines with no line under a row). Where the VM's locale is not UTF-8, hardn draws ASCII glyphs: compare with `./p4status -profile plain` then.
4. **At 80×24:** `tmux resize-window -x 80 -y 24`, run again, `tmux capture-pane -p -S -40 > /root/menu-24.txt`: the compact blocks (no header, no rule), the risk line and the first rows scrolled off the 24 visible lines (32 lines in all with no line under a row), the items and the prompt at the bottom.
5. **Against the host's own tools** (each row as vmtest.md §8.5 checks p4status's), the DNS row with them: Debian 13 and Alpine `grep nameserver /etc/resolv.conf`, Ubuntu `resolvectl dns`; `grep -A4 '^nameservers' /etc/hardn/hardn.yml`. The row is Configured exactly when hardn.yml's list is the one in use, in order; "none in hardn.yml" with `nameservers: []`.
6. **The row follows the configuration:** from the main menu, 3 (DNS), add a nameserver or take a preset, y, then 0: the main menu, drawn again, shows the DNS row from the new list (Configured on Debian 13 and Ubuntu after a change that applied). Then restore the VM's DNS and hardn.yml as cmd-engine.md §4.1 did.
7. **Read only, and no run log:** `sha256sum -c /root/menu-live-before` (only what step 6 changed, before its restore); `find /etc /var/lib -newer /root/menu-live-stamp` lists nothing the main menu wrote; the run log's count from step 1 grew by step 6's change alone, not by any pass of the main menu.
8. **The cost:** where `strace` is installed, `strace -f -e trace=execve -o /root/hardn.trace ./hardn`, `q` at once: the `execve` lines after hardn's own are about 28 (Debian 13), 31 (Ubuntu 26.04), 13 (Alpine) per pass, as §5's fixtures (the VMs' states may add or remove a few); and the time a pass takes, by eye or `script -T`.
9. **Ctrl-C during the checks:** `./hardn`, Ctrl-C before the menu appears (or while it redraws after an item): hardn ends at once, `echo $?` 130 in bash, the terminal sane, nothing written.

## 11. Open for the maintainer

None blocks this unit; each is a decision this unit did not take.

1. **The compact form's 25th line** (§6): accept the bar scrolling off at 80×24, or which blank line gives way, or a cap on the lines under the rows.
2. **"Sudo Method" and "Sudo Not Installed"** (§7; P-13, proposed): dropped with origin/main's block; the no-password fact is a line under User, "password required" is not said, and "no sudo" only where a non-root account exists.
3. **The update notice in the version line** (§7; P-15): the `Header` draws none; unreachable while update checks are off (V26).
4. **"⭘ Not Configured none in hardn.yml"** on every host whose hardn.yml lists no nameserver, hardn's default: a Not Configured row, in its colour, on a host DNS was never asked of, though it weighs nothing (control-dns.md §5's words, proposed; the alternative is Not Applicable or a muted state there).
5. **The cost** (§5): 13 to 31 commands a pass where origin/main ran about 6 to 10; the time on a host is step 8's. Reading again only after an item that may have changed the host would be a cache, not built.
6. **The live main menu at 80×24** (§6): 32 lines, its risk level scrolled off, as origin/main's was, until slice 2b moves the menu onto the layout and its compact two-column form.

## 12. API

```go
package controls // pkg/engine/controls (new)
func Status(cfg *config.Config) []engine.Control // Figma's seven rows' controls, then dns.Controls(cfg)

package risk // pkg/engine/risk
// Rows(): DNS added last, Weight 0; Assess leaves a row of weight 0 out of everything.

package screen // pkg/ui/screen
func (vm MainMenu) StatusBlocks(c component.Ctx, below int) string // the status model's blocks over a caller's menu of below lines

package mainmenu // pkg/screens/mainmenu (new)
type Options struct { System host.System; Detect func(context.Context, host.System) (detect.Facts, error); Config *config.Config; Version string; Env term.Env }
func Status(ctx context.Context, o Options, below int) (string, error) // the blocks and the gap before the menu, ready to print; error: ctx's alone

package menu // pkg/menu
func (m *MainMenu) SetStatus(draw func(ctx context.Context, below int) (string, error)) // a pass panics without it
const legacySecurityStatus = false // DISABLED 2026-10-23

package main // cmd/hardn
var statusHost func() host.System // host.Real, no run log
func withStatus(m *menu.MainMenu, c *config.Config, version string) *menu.MainMenu
func drawStatus(ctx context.Context, c *config.Config, version string, below int) (string, error)
```

The gallery: `statusControls` removed (`controls.Status`), `statusConfig` added, `harden` configures DNS (`dnsConfigured`). p4status: `statusControls` removed, `env.controls func(*config.Config) []engine.Control`, `env.configs`, `configPaths`, `loadConfig`, `partConfig`, the DNS check's commands in `rules`. No depguard change: `pkg/screens/mainmenu` is under `screens-through-host`, `pkg/engine/controls` under `controls-through-host`. `go.mod` and `go.sum` unchanged.

## 13. Size

About 645 changed lines of non-test Go (510 added, 135 removed; `git diff --numstat` against `next/v0.4`, a large share of them comments): p4status about 140 and the gallery 60 (one list, `hardn.yml`, the DNS check's commands), `pkg/menu` 140 (the gate, the hook, `showLegacyStatus` moved out of the loop), `pkg/screens/mainmenu` 90, `pkg/ui/screen` 90, `cmd/hardn` 55, `pkg/engine/controls` 30, `pkg/engine/risk` 30, `pkg/output` 10; and about 830 of tests. Over the guideline of about 500: going live touches the three readers of the one list, and the DNS row changes the gallery's model and p4status's reading and fixtures with it; the split is the three commits (the list and the row, the blocks over a caller's menu, the live menu). Testdata: the gallery's nineteen status goldens (each the DNS row alone), `pkg/output`'s `human/status`, p4status's five fixtures (exported again), and the four new `main_menu_live` goldens.
