# Phase 0 appendix: interaction audit

> **Part of:** `docs/refactor/phase0-findings.md` (§8). This appendix holds the full screen, prompt, anti-pattern and Figma tables that the findings summarise.
> **Refs:** origin/main 9d46866 (the baseline), compared with v0.3.2 (bce7cc2) and arch3 (ef0b26a).
> **Status:** draft for the Phase 0 review, 2026-10-16.
> **Labels:** **[V]** verified (code read, harness output or command output seen). **[I]** inferred.

## 0. Scope, baseline and method

- **Baseline (maintainer decision 6):** origin/main's screens, wording and behaviour are the baseline. Defects found here are defects to fix (the V-list is in `side-effects.md` §8). v0.3.2 is consulted only where origin/main regressed.
- **No hardn binary was executed.** `main()` writes `/var/log/hardn.log` and `/etc/hardn/hardn.yml.example` before flag parsing (`cmd/hardn/main.go:56-62`), so even `hardn --help` writes. Two scratch-only Go test harnesses were used instead:
  - one calls `rootCmd.Execute()` with `--help` (`main()` does not run under `go test`);
  - one renders the main menu from fixture data through the same `style.*` and formatter calls as `main_menu.go` and `security/status.go`, without calling the status probes, the user repository or the input reader.
- Builds used `-mod=mod` from the local module cache: the committed `vendor/` lacks the x/sys assembly files.
- **"Reachable"** means reachable from `ShowMainMenu` on origin/main: 17 menu files (16 screen files plus `input.go`). Four menu files are dead code: `help_menu.go`, `linux_packages_menu.go`, `python_packages_menu.go`, `sources_menu.go`.
- **Redaction:** fixture values that came from a real host or from the Figma mock are replaced: `<example-host>`, `192.0.2.10`, `<port>`, and "(withheld)" for user names.

---

## 1. Headline findings (most consequential first)

1. **Run All runs live with no confirmation** [V]. From the main menu, `7`, Enter, `1`, Enter calls `runAllHardening()` directly (`run_all_menu.go:142-144`). The typed-`yes` confirmation guards only option 2 (`:145-181`). v0.3.2 asked `(y/n)` first (`v0.3.2:pkg/menu/menu.go:575-578`).
2. **Run All reports work it never did** [V]. The live path calls `SecurityManager.HardenSystem` (user, SSH, firewall, DNS only), then prints "AppArmor configured" and "Lynis security audit completed" from config flags (`run_all_menu.go:270, 274`). The dry-run path is a separate hand-written simulation (`dryRunHardening`, `:334`), so plan and apply diverge.
3. **Input is raw single-key, not line-based** [V]. Every menu prompt goes through `ReadMenuInput` → `ReadRawKey`, which runs `stty -F /dev/tty raw -echo` and then `stty … sane` **per keystroke** (`input.go:59-144`). `q` as the first key quits with no Enter. Verified by harness: Ctrl-C (0x03) is discarded, and EOF loops forever (368 iterations in 2 s).
4. **Hidden menu options can still be selected** [V], because option lists are data but dispatch is a separate `switch`:
   - User Management with an existing user shows only 1 and 2, but `4` still runs `CreateUser(username, true, cfg.SudoNoPassword, cfg.SshKeys)` against the live user with no confirmation (`user_options.go:792-842`); `3` opens the config SSH-keys screen.
   - Firewall with UFW absent shows only option 1, but `2` still runs `ConfigureSecureFirewall` (`firewall_menu.go:243`).
5. **What the UI says doesn't match what happened** [V]:
   - Create New User asks "Allow sudo access? (y/n)" and always passes `true` (`user_options.go:680-682` vs `:744`).
   - "Remove SSH key" leaves the key in `authorized_keys` and prints "SSH key removed successfully" (`:538-545`).
   - Firewall "Install UFW", "Disable firewall" (after a y/n) and "Apply profiles" print "This operation isn't yet implemented in the new architecture".
6. **Status is computed in at least 6 independent places**, mostly from config files rather than effective state (§7). SSH Port status is `cfg.SshPort != 22`.
7. **"Minimal / hardened security" can never be shown** [V harness]: 8 equally weighted booleans give a maximum of 8, which maps to "Low".
8. **Screen clears wipe results** at 8 sites (§5, W1–W8).
9. **"Press any key" is everywhere:** 32 prompt strings and 55 `ReadKey()` call sites in reachable code (§4.4). Toggling dry-run costs 3 key presses, and in the Enabled state the Dry-Run menu is built but **never printed**.
10. **The security-update alert locks users out of the UI.** A matching release replaces the main menu with "Update Binary" and exits after any key. Verification narrowed the match (name: `security`, `[security]`, `cve-`; body also `vulnerability`, `exploit`; only when the latest tag is newer) and showed an unprivileged user can **plant the `/tmp` cache** and the URL it shows (`side-effects.md` §9 D2).
11. **The Figma main menu matches origin/main almost 1:1, not v0.3.2.** Figma uses origin/main's own glyph constants ⛛ ⭘ ⛬, all East Asian Ambiguous width.
12. **The CLI already broke compatibility with v0.3.2** (unreleased): `-l -i -a -s` removed; `-e`, `--no-color`, `--debug-updates`, `--test-update`, `--test-security-update`, `setup-sudo-env` and `system-details` added.
13. **No UI code has tests:** no `_test.go` in `pkg/menu`, `pkg/style` or `pkg/security`. The live menu cannot be driven by scripted stdin (§3).

---

## 2. CLI surface

### 2.1 origin/main `hardn --help` (captured via harness; `hardn help` is identical)

```text
A simple hardening tool for Debian, Ubuntu, Proxmox and Alpine Linux.

Usage:
  hardn [flags]
  hardn [command]

Available Commands:
  completion     Generate the autocompletion script for the specified shell
  help           Help about any command
  setup-sudo-env Configure sudo to preserve HARDN_CONFIG environment variable
  system-details Display detailed system summary

Flags:
  -f, --config string          Specify configuration file path
  -g, --configure-dns          Configure DNS resolvers
  -w, --configure-ufw          Configure UFW
  -c, --create-user            Create non-root user with sudo access
      --debug-updates          Enable debugging for update checks
  -d, --disable-root           Disable root SSH access
  -n, --dry-run                Dry run mode (preview changes without applying)
  -h, --help                   help for hardn
      --no-color               Disable colored output
  -p, --print-logs             Print logs
  -r, --run-all                Run all hardening steps
  -e, --setup-sudo-env         Configure sudoers to preserve HARDN_CONFIG environment variable
      --test-security-update   Test security update notification
      --test-update            Force update notification for testing
  -u, --username string        Specify username to create
  -v, --version                Show version information
```

- `system-details --help` adds `-E/--export`, `-O/--format` (default "text"), `--json`, `-S/--section` and `--yaml`. **All five are parsed and ignored**: the implementation is commented out (`pkg/cmd/system_details_cmd.go:38-42` vs `:95-121`).
- `setup-sudo-env` has no flags of its own; the root `-e` flag duplicates it.

### 2.2 v0.3.2 `hardn --help` (captured)

No subcommands. Flags: `-f/--config`, `-g/--configure-dns`, `-s/--configure-sources`, `-w/--configure-ufw`, `-c/--create-user`, `-d/--disable-root`, `-n/--dry-run`, `-h`, `-a/--install-all`, `-l/--install-linux`, `-i/--install-python`, `-p/--print-logs`, `-r/--run-all`, `-u/--username`, `-v/--version`. Long text: "A Linux hardening tool for Debian, Ubuntu, Proxmox and Alpine systems."

### 2.3 CLI behaviour notes [V unless marked]

- **Writes before flags are parsed** (`main.go:56-62`). As root, `hardn --help` creates `/var/log/hardn.log` and `/etc/hardn/hardn.yml.example`; as non-root it prints two warnings ahead of the help text.
- **`--version`** (verification C4): on a **release build** (version injected by ldflags), cobra intercepts `-v/--version` and prints only `hardn version X`; build date and commit are lost (v0.3.2 printed name, version, build date and commit). The custom `PrintVersionInfo` at `main.go:125-128` runs only on unversioned dev builds. Cobra also auto-adds `completion` and `help` because subcommands now exist.
- **Root required** for everything except `--version` (`main.go:137-142`).
- **Exit codes:** failed operations are logged and the process still exits 0 (`main.go:247-395`). Only pre-checks and `setup-sudo-env` call `os.Exit(1)`.
- **A first-run prompt runs in flag mode too:** `LoadConfig` → "No configuration file found. Would you like to create one? [Y/n]" reads stdin **without a TTY check** (`config.go:368-375`), so a scripted `sudo hardn -r` on a host with no config consumes a line of stdin or blocks. The follow-up questions are TTY-gated.
- The unreachable in-app Help screen is stale (`--config-file`, removed flags).
- Test hooks (`--test-update`, `--test-security-update`, `--debug-updates`) appear in production help.

### 2.4 arch3

- `cmd/legacy/main.go` registers exactly origin/main's flags and subcommands.
- `cmd/hardn/menu/main.go` is a new binary with no cobra and no flags. `./cmd/hardn` (the release entrypoint) does not exist on arch3.

---

## 3. Input model by ref

| | origin/main | v0.3.2 | arch3 legacy (`cmd/legacy`) | arch3 new (`cmd/hardn/menu`) |
|---|---|---|---|---|
| Menu selection | Raw per key: `ReadMenuInput` → `ReadRawKey` runs `stty -F /dev/tty raw -echo` before and `stty … sane` after **each byte** (`input.go:59-144`). Echo, backspace and Enter handled by hand. Only digits and `q` accepted; everything else silently dropped (`:97-101`) | Hybrid: first key in cbreak (`readKey`, `stty cbreak min 1`), the rest of the line via `bufio` | Same as origin/main (`LegacyInputAdapter.go:24, 108`) | `golang.org/x/term` `MakeRaw`; falls back to `ReadLine` when stdin isn't a TTY |
| Quit key | `q` as the first key quits immediately, no Enter (`:76-79`) | `q` as the first key quits; **an arrow key pressed twice also exits** | as origin/main | `q` **anywhere** in the line quits |
| Text fields and y/n | Shared `bufio.NewReader(os.Stdin)`, line-based (`:13-19`) | Shared bufio reader | same | `ReadLine` |
| "Press any key" | `ReadKey`: `stty cbreak min 1`, reads 1 byte (`:22-55`) | `readKey` (cbreak) | same | "Press Enter to return..." |
| Empty Enter | Invalid option | Invalid option | same | **Runs the first option's action** |
| Ctrl-C | At menu prompts raw mode clears ISIG, so Ctrl-C arrives as 0x03 and is **dropped** [V harness]: users cannot Ctrl-C out of any menu. In cooked `ReadInput`, SIGINT kills the process with no cleanup. During cbreak `ReadKey`, Ctrl-C kills it before the deferred `stty -cbreak`, leaving the tty in `-icanon` [I]. No `signal.Notify` anywhere | A kill during cbreak leaves the tty in cbreak [I] | as origin/main | `signal.Notify` → clear screen → `os.Exit(0)` (not 130) |
| EOF / piped input | `ReadRawKey` returns "" on error and `ReadMenuInput` **loops forever**, spawning 2 `stty` per iteration [V harness: 368 iterations in 2 s] | `readKey` returns "\x00"; loops on "Invalid option" [I] | as origin/main | `log.Fatalf` (exit 1) |
| Readers on stdin | 3 independent readers (menu bufio, two bufio readers in `config.go`) plus raw `os.Stdin.Read`; bytes buffered in one are invisible to the others [I] | 3 | same | 1 bufio reader plus raw reads |
| Typeahead discard | **None** (no `tcflush`). A key typed during a long step is read as the next input: it can dismiss the result's "Press any key", and the main loop's clear then wipes the result [I] | None | None | None |

- **No ref is Enter-based.** The brief's Enter-everywhere decision (§2) is a behaviour change from shipped v0.3.2 too, not only from Figma.
- stash@{0} on hex-migration-new ("hex-v2 raw mode") added a persistent hand-rolled termios raw mode: the opposite direction (`inventory.md` A6).
- **Implication:** all of `pkg/menu/input.go` is what the Phase 1C prompt kit replaces. Reachable call sites: 17 `ReadMenuInput`, 28 `ReadInput`, 55 `ReadKey` (§4.4).

---

## 4. Screen inventory (origin/main)

### 4.1 Navigation map (reachable)

```text
[pre-menu] non-root refusal | HARDN_CONFIG sudo NOTICE | first-run config prompts | (security-update alert -> exit)
Main menu (main_menu.go)  1..10, 0/q
 1 User Management (user_menu.go + user_options.go)
     variant A (configured user exists): 1 Manage a user -> Select user -> Management -> {1 Manage Sudo | 2 SSH keys (per user)}
                                         2 Create a user -> Create New User form
                                         [hidden but selectable: 3 SSH Key Mgmt (config), 4 Update user from config]
     variant B (no user / not created):  1 Enter|Change username  2 toggle sudo-password (config)  3 SSH Key Management (config)  4 Create user
 2 SSH Login -> "Disable Root SSH Access" (disable_root_menu.go): 1 disable (y/n) | 2 view config ; returns to MAIN after any action
 3 DNS (dns_menu.go): 1 apply | 2 add | 3 remove | 4/5/6 presets ; recursive re-entry
 4 Firewall (firewall_menu.go): 1 install|enable|disable | 2 configure | 3 app profiles -> Manage UFW Application Profiles (add/remove/apply)
 5 Backup (backup_menu.go): 1 toggle | 2 path | 3 verify
 6 Dry-Run (dry_run_handler.go -> dry_run_menu.go): intro -> settings -> result
 7 Run All (run_all_handler.go -> run_all_menu.go): [username pre-check] -> summary -> 1 run | 2 toggle dry-run and run
 8 Environment (environment_settings_menu.go): 1 sudo env preservation | 2 guide
 9 System Details (system_details_menu.go + pkg/system/display.go): 1 refresh | 2 export
10 Logs (logs_menu.go): dump
 0 Exit / q
Unreachable: Help, Linux Packages, Python Packages, Package Sources (+ Edit Repositories, Edit Proxmox Repositories)
```

### 4.2 Two visual generations coexist [V]

- **New box/shade style** (`style.NewBox` with a `░` title bar; from 607eafa): Main menu, User Management, Manage User (select and management), Manage Sudo, Create New User (`style.ScreenHeader`), SSH Key Management (boxed), System Details.
- **Legacy style:** `utils.PrintHeader()` (clears the screen, then a green 71-character `~~~` line) followed by a blue bold title. Used by SSH Login, DNS, Firewall (and app profiles), Backup, Dry-Run (3 screens), Run All, Environment (and its guide), Logs, the main menu's invalid-option page, and the per-user SSH keys screen (`ClearScreen` plus an "ℹ" line only).
- Run All prints the dimmed ASCII "Hardn Linux" logo mid-screen, without clearing.

### 4.3 Every screen, menu and prompt

Every menu is a `style.Menu` titled "Select an option". Unless noted, the prompt is `❯ Enter selection [<range> or q]:`, with the range generated from the option numbers (`style.go:708-744, 866`). "Clears" = the screen is wiped on entry or after an action; "Pauses" = "Press any key" prompts.

| Screen (code) | Entry | Inputs (prompts, validation) | Outputs | Navigation | Clears | Pauses | Figma |
|---|---|---|---|---|---|---|---|
| **Pre-menu** (`main.go:137-142`; `config.go:289-442`) | Start | First run: "No configuration file found. Would you like to create one? [Y/n]" (no TTY check; empty/EOF = yes), then TTY-gated username (`[george]` default), SSH port, auto updates `[y/N]`, SSH key | Non-root refusal ("[ERROR] This script needs to be run as root." + 2 hints); under sudo a 4-line HARDN_CONFIG NOTICE (after a `su - $SUDO_USER` probe on every start); "Created configuration file at …"; "Using configuration from …" | → main menu | All wiped by the first main-menu clear (W8) | none | none |
| **Security-update alert** (`main_menu.go:155-210, 418-423`) | Menu start, when the update check matches | none | "hardn vX" header, "Update Binary" badge, details, URL, Version/Build Date/Git Commit bullets, "Installer Script:" with a curl-to-sudo-sh command | **Exits hardn** after any key | — | "Press any key to exit..." | none |
| **Main menu** (`main_menu.go:377-547`) | Start; return from every screen | `ReadMenuInput`: digits, `q` (no Enter) | Header, distro bar, host block, risk, status rows (re-probed every loop), menu (§8) | 1–10 → screens; `0`+Enter or `q` exits (exit 0, no message; v0.3.2 printed "Hardn has exited.") | `ClearScreen` every loop (`:391`) | none | 00-TUI-MAIN |
| **Main: invalid option** (`main_menu.go:538-544`) | Invalid main-menu input | none | "✗ Invalid option. Please try again." | → main menu | `PrintHeader` (full clear) | 1 | none |
| **User Management** (`user_menu.go:36-142`; `user_options.go:62-861`) | Main 1 | Variant B: "Enter username to create:" / "Enter new username (leave empty to keep current):" (`^[a-z][a-z0-9_-]*$`, 1–32 chars, reserved list) | Box "User Management"; hardcoded "⛛ Non-root Configured UID ≥ 1000" line (always); per user: badge + last-login meta, Sudo, SSH Keys N, UID:GID, Directory. Variant A: `1 Manage a user`, `2 Create a user`, `0 Return`. Variant B: `1 Enter/Change username`, `2 Require/Allow sudo password` (config toggle), `3 Manage SSH keys`, `4 Create user`, `0 Return` | Recursive `m.Show()`; `q`/`0` → main. **Hidden options 3/4 selectable in variant A** | `ClearScreen` on entry; variant B create result wiped by the main loop (W7) | after each action | 01-USER (variant A only) |
| **Manage User: select** (`user_options.go:187-242`) | User 1 | Number of a non-system user | Box "Manage User", menu "Select a user" | Invalid → "Invalid selection" + key → **back two levels** | — | on invalid | USER-MANAGE_USER-SELECT_USER |
| **Manage User: management** (`user_options.go:247-293`) | Select | `1 Sudo Method`, `2 Manage SSH keys`, `0 Return` | "Managing: <badge>" | switch dispatch | — | — | USER-MANAGE_USER-MANAGEMENT |
| **Manage Sudo** (`user_options.go:296-411`) | Management 1 | "Require password for sudo? (y/n):" **only** when moving to password-required | "✓ Sudo will (NOT) require a password …", "User sudo settings updated successfully" (suppressed in dry-run) | → management | — | 1 | USER-MANAGE_SUDO-CONFIRMATION |
| **Per-user SSH keys** (`user_options.go:413-560`) | Management 2 | Add: "Paste SSH public key:" (**no format check**). Remove: "Select a key to remove:", "Enter number to remove (0 to cancel):" | "ℹ Managing SSH keys for user: X", keys truncated 15+…+15; menu "Select SSH key operation"; remove reports success **without removing** | → management | `ClearScreen`, no header | after each action | USER-SSH_KEYS (add-only state) |
| **Create New User** (`user_options.go:628-757`) | User A2 | "Enter username to create:"; section "User Settings": "Allow sudo access? (y/n):" (**answer ignored**), "Allow sudo without password? (y/n):"; section "SSH Access": "Add SSH public key? (y/n):", "Paste SSH public key:"; "Summary"; confirm "Create user 'x'? (y/n):" | "ℹ Configure a new user"; "Creating user 'x'..."; result | Invalid or existing name → message, key, **back to the menu** (the flow restarts) | `ScreenHeader` | after result and on invalid input | USER-CREATE_USER (cancelled path only) |
| **SSH Key Management (config keys)** (`ssh_keys_menu.go`; `ssh_keys_options.go:51-146`) | User B3 (or hidden A3) | Add: "Paste SSH public key (e.g., ssh-ed25519 AAAAC3NzaC1lZDI1...):" (prefix `ssh-`/`ecdsa-` checked). Remove: "Enter key number to remove (1-N):" | Box with **placeholder content**: "⛛ SSH Keys Configured ssh-ed25519" (`:114`), "Last login: 2023-10-01" (`:62`), "This is a bottom line example." (`:155`). Saves `./hardn.yml`; add also writes to a live user | `1 Add`, `2 Remove`, `0 Return to user menu`; the box is drawn once and the menu repeats under it | `ClearScreen` on entry | after each action | none |
| **SSH Login** (`disable_root_menu.go:38-205`) | Main 2 | Confirm "⭘ Are you sure you want to disable root SSH access? (y/n):" | "⭘ WARNING: Root SSH access is currently ENABLED" or "✓ … already DISABLED"; a 3-bullet checklist; `1 Disable root SSH access` (struck through when disabled), `2 View current SSH configuration`, `0 Return`. View: root login (from the file), SSH port and allowed users (**from config**) | **Always returns to main** after an action. **No way to re-enable** root login, so "Toggle SSH root access" is false | `PrintHeader` | 1 ("…to return to the main menu") | 02-SSH_LOGIN, SSH_LOGIN-TOGGLE_CONFIRMATION, SSH_LOGIN-SSH_SETTINGS_DISPLAY |
| **DNS** (`dns_menu.go:38-342`) | Main 3 | "Enter nameserver IP address:" (only "4 dot-separated parts"); "Enter nameserver number to remove (1-N):" | Current DNS (effective: resolved / `resolvectl dns` / `resolv.conf`) and configured nameservers; `1 Configure DNS` (**no confirmation**), `2 Add`, `3 Remove` (only when nameservers exist; the range still says 0–6), `4/5/6` Cloudflare/Google/Quad9 presets (**config only**), `0 Return` | Recursive `m.Show()` | `PrintHeader`; add/remove results **wiped immediately** (W1, W2) | after actions | none (legacy screenshot only) |
| **Firewall** (`firewall_menu.go:38-306`) | Main 4 | none beyond the menu; Disable asks y/n then hits a stub | UFW Installed / Status / SSH Port (config, with "consider changing" / "good security"); rules list (effectively unreachable: shows "No firewall rules configured"); configured profiles. `1` = Install (stub) / Enable (**no confirmation**) / Disable (y/n, stub); `2 Configure firewall` (**no confirmation**); `3 Manage application profiles`; `0 Return to main menu` | Recursion without `return` adds an **extra pause per level** on unwind | `PrintHeader` | after actions, plus the unwind pauses | none (legacy screenshot only) |
| **Manage UFW Application Profiles** (`firewall_menu.go:309-557`) | Firewall 3 | Add: 4 prompts (name, title, description, "ports (e.g., '80/tcp,443/tcp')"), port validated **after** all 4 (a failure discards all). Remove: number, then "Are you sure … (y/n):" | `1 Add`, `2 Remove`, `3 Apply` (2/3 only when profiles exist), `0 Return to firewall menu`; Apply = a dry-run listing or a stub | Recursive | `PrintHeader` each recursion; **all results wiped**, including dry-run output (W3–W5) | after actions | none |
| **Backup** (`backup_menu.go:31-247`) | Main 5 | "Enter new backup path:" (no validation) | Backups Enabled/Disabled, path, path check; `1 Toggle backups (currently: X)`, `2 Change backup path`, `3 Verify backup directory` (when enabled), `0 Return to main menu`. Saves `./hardn.yml` | Recursive after each action | `PrintHeader` | after each action | 05-BACKUP |
| **Dry-Run** (`dry_run_handler.go:31-75`; `dry_run_menu.go`) | Main 6 | Disabled: `1 Enable dry-run mode`, `0`. Enabled: `1 Disable`, `0 Keep enabled`, but **the menu is never printed** | Intro text; "Dry-run mode is now ENABLED/DISABLED …". Saves `./hardn.yml`; the next start overrides it from the flag (`main.go:152`) | Invalid option leaves the whole flow | `PrintHeader` per screen | **3** for one toggle | none |
| **Run All** (`run_all_handler.go:35-67`; `run_all_menu.go`) | Main 7 | Live with no username: "Would you like to set a username now? (y/n):" (yes opens the **whole User Management screen** mid-flow). Option 2 going live: "Type 'yes'" | "Run All Hardening Steps": Current Configuration (dry-run, user, SSH port from config); Enabled Features (6 config flags); "SECURITY WARNING:" / "This cannot be undone."; `1 Run all hardening steps` (**no confirmation**), `2 Enable/Disable dry-run mode and run`, `0 Cancel`. Execution: ASCII logo, `[i/N]` step lines printed **after** `HardenSystem` returns (N never reached), false AppArmor/Lynis lines, "Check the log file at <cfg.LogFile>" | → main | `PrintHeader`; "Continuing without user creation" wiped (W6) | after the run | none |
| **Environment** (`environment_settings_menu.go:32-150`) | Main 8 | none beyond the menu | "Current HARDN_CONFIG: X" or "not set"; "Sudo preservation: Enabled/Disabled"; `1 Setup sudo environment preservation`, `2 Show environment variables guide`, `0 Return`; the guide is static text | → main | `PrintHeader` | after each action | none |
| **System Details** (`system_details_menu.go:37-245`; `pkg/system/display.go`) | Main 9 | "Enter filename to export system status (default: system_status.txt):" | Box: OS, kernel, host, IPs, DNS, users, CPU with load graphs, disk/ZFS, memory, last login, uptime; `1 Refresh`, `2 Export` (when loaded), `0 Return`. Export writes a current-directory path as root | → main | `ClearScreen` | after actions | none |
| **Logs** (`logs_menu.go:32-63`) | Main 10 | none | "View Logs", "Log file: <cfg.LogFile>", "Log Contents:", a rule, then **the entire file**. Writes always go to `/var/log/hardn.log`; this screen reads `cfg.LogFile` | → main | `PrintHeader` | 1 | none |
| *Help* (dead) | — | — | Static usage text with stale flags | unreachable | — | — | none |
| *Linux Packages / Python Packages / Package Sources* (dead) | — | — | Install and repository editors; commented out of the main menu, flags removed (ea8680f) | unreachable | — | — | none |

### 4.4 Clear and pause call sites per reachable file [V `git grep -c` on origin/main]

| File | `ClearScreen()` | `PrintHeader()` | `ReadKey()` |
|---|---|---|---|
| `main_menu.go` | 5 | 1 | 2 |
| `user_options.go` | 5 | 0 | 20 |
| `user_menu.go` | 1 | 0 | 0 |
| `ssh_keys_menu.go` | 1 | 0 | 0 |
| `ssh_keys_options.go` | 0 | 0 | 4 |
| `disable_root_menu.go` | 0 | 1 | 1 |
| `dns_menu.go` | 0 | 1 | 6 |
| `firewall_menu.go` | 0 | 2 | 4 |
| `backup_menu.go` | 0 | 1 | 4 |
| `dry_run_handler.go` | 0 | 2 | 2 |
| `dry_run_menu.go` | 0 | 1 | 1 |
| `run_all_handler.go` | 0 | 0 | 1 |
| `run_all_menu.go` | 0 | 1 | 3 |
| `environment_settings_menu.go` | 0 | 2 | 3 |
| `system_details_menu.go` | 1 | 0 | 3 |
| `logs_menu.go` | 0 | 1 | 1 |
| **Total (16 files)** | **13** | **13** | **55** |

The audit script's figure of 56 `ReadKey()` covers all 17 reachable files; the extra match is the definition `func ReadKey() string` at `input.go:22`, so there are 55 call sites.

---

## 5. The brief's 11 anti-patterns (Phase 0.5)

Counts come from the audit's reproducible script over the 17 reachable origin/main menu files. The v0.3.2 column covers `pkg/menu/menu.go`. arch3-legacy is a renamed copy of origin/main with almost identical counts; arch3-new is `pkg/adapter/driver`.

| # | Anti-pattern | origin/main | v0.3.2 | arch3 new TUI |
|---|---|---|---|---|
| 1 | Prompting and executing interleaved; partial state on failure | **Present** (detail below) | Present | n/a (System Details only) |
| 2 | Screen clears wiping the output just produced | **Present**: 13 `ClearScreen` + 13 `PrintHeader`; **8 wipe sites** | Present: 15 `PrintHeader` + 3 `PrintLogo` (both clear) | Clears on every loop |
| 3 | "Press Enter/any key to continue" after every action | **Present**: 32 prompt strings, 55 `ReadKey()` call sites (the script's 56 includes the definition), 23 `PressAnyKey()`. Variants: 20 "to continue", 10 "to return to the main menu", 1 "to continue to dry-run configuration", 1 "to exit" | Present: 16 strings, 19 `readKey()` | "Press Enter to return..." |
| 4 | Invalid input restarts the whole flow | **Present**: 5 flows (detail below) | Partly (Backup re-shows the menu) | n/a |
| 5 | Prompt hints and ranges hardcoded apart from menu definitions | **Mostly absent for menus**: `GetValidRange()` derives `[0-N or q]` from the options. Residual: `or q` always appended; gaps hidden (DNS shows 0–6 with no 3); dispatch is separate, so hint and behaviour drift (§1 item 4). Free-text ranges come from `len` | **Present**: `[0-12 or q]`, `[0-2]` | **Present**: `"Enter selection [0-1 or q]:"` hardcoded |
| 6 | Menus as imperative `switch` statements | **Present**: 15 `switch choice` blocks, 61 `case "N"` arms. Option lists are data, handlers are not bound to options (hence selectable hidden options) | Present: 2 switches, 13 arms | **Absent**: `Option{…, Action}` dispatch |
| 7 | Back and quit keys differ between levels | **Present** (detail below) | Present | Present: `q` anywhere quits; empty Enter runs option 1 |
| 8 | Inconsistent confirmation wording | **Present** (detail below) | Present: 7 `(y/n)` accepting only `y`/`Y`, so typing `yes` cancels | n/a |
| 9 | Raw subprocess output dumped into the UI | **Mostly absent, with an inverse defect** (detail below) | **Present**: failed apt/apk output embedded in red console errors | `clear` streams to stdout (harmless) |
| 10 | Styling calls scattered through feature code | **Present**: 1,029 `style.*` refs in 17 reachable files (1,462 incl. dead files) plus 445 `fmt.Print*` lines (§6) | Present: 80 `style.*` + 158 raw ANSI literals | Present: 23 raw ANSI + 30 style refs in the terminal adapter; 9 raw ANSI in the tui model |
| 11 | Status computed separately in several places | **Present**: at least 6 sources (§7) | Present | Present: a 4th implementation (`OsSecurityInfoAdapter`, firewall/MAC, plus macOS) |

**#1, origin/main:**
- (a) Run All's username pre-check opens the full User Management screen, where users can be created, before the summary.
- (b) Passwordless sudo is applied with no confirmation (`user_options.go:383-397`).
- (c) Firewall Enable and Configure execute on selection (v0.3.2 asked y/n).
- (d) DNS apply executes on selection.
- (e) Variant B option 4 creates the user from config immediately.
- (f) Config SSH-key add writes config **and** a live user in one step.
- (g) First-run config creation happens inside `LoadConfig`.
- Partial state: `HardenSystem` stops at the first error with no rollback. UFW sets defaults, then `ufw disable`, then `ufw reset` without `--force`, and only then adds rules and re-enables; a failure after `disable` leaves the firewall off. (`ufw reset` without `--force` probably aborts silently with stdin=/dev/null [I]; `side-effects.md` row 19.)

**#2 wipe sites, origin/main:**

| Site | What is wiped |
|---|---|
| W1, W2 | DNS add/remove nameserver messages (`dns_menu.go:162-178`) |
| W3–W5 | Firewall profile add, remove and apply, including apply's `[DRY-RUN] Would write profiles...` (`firewall_menu.go:368-396`) |
| W6 | Run All "Continuing without user creation" (`run_all_handler.go:59-66`) |
| W7 | The variant B create/update-user result, including errors (→ `main_menu.go:391`) |
| W8 | Startup notices: config NOTICE, "Created configuration file at …", "Using configuration from …" (→ `main_menu.go:391`) |

**#4, origin/main:** Create New User (invalid or existing name → back to the User Management menu); Add nameserver (bad IP → redrawn screen); Add app profile (invalid port discards all 4 fields); Select user (invalid number → back two levels); Dry-run menu (invalid option leaves the whole flow).

**#7, origin/main:**
- Main menu: `0` exits and `q` quits. In submenus `0` is Return and `q` **returns to the parent**.
- `b` is never accepted (`input.go:97-101` drops it).
- Free-text prompts have no consistent cancel: empty input cancels some, "0 to cancel" works on one, the profile fields have none.
- After an action, SSH Login, DNS and Firewall return to the main menu; Backup, Environment and User loop in place.
- Exit labels: `Return` ×8, `Return to main menu` ×5, `Return to user menu` ×1, `Return to firewall menu` ×1, `Exit` ×1.

**#8, origin/main:**
- Forms: `(y/n)` ×10 with no default shown (empty = no); `[Y/n]` (empty = **yes**) in first-run; `[y/N]`; `Type 'yes'` (Run All option 2).
- Accepted answers differ: `EqualFold` y/yes, `ToLower` y/yes, exactly `yes`.
- Phrasing differs: "Are you sure you want to …?", "Would you like to …?", "Create user 'x'?", "Require password for sudo?".
- Several high-impact actions have **no confirmation**: #1 (b)–(e) and Run All option 1.

**#9, origin/main:** `OSCommander` uses `CombinedOutput`, so output is captured, not streamed; adapters then **discard it** and errors carry only "exit status N". No output is logged and no tail is shown (Lynis, dead code, is the exception). The Firewall screen prints raw `ufw status` rule lines; Logs dumps the whole file. `logging.Log*` prints coloured `[INFO]`/`[ERROR]` lines to stdout from 6 packages (98 calls), interleaved with menu output.

---

## 6. Styling call-site census (sizes the depguard rule and Phase 5)

| Package (origin/main) | `style.X` refs | fatih/color calls | raw ANSI literals | files importing `pkg/style` |
|---|---|---|---|---|
| `pkg/menu` (all 21 files) | 1,462 | 0 | 0 | 20 |
| `pkg/menu` (17 reachable) | 1,029 | 0 | 0 | 16 |
| `pkg/security` | 8 | 0 | 0 | 1 |
| `pkg/system` | 3 | 0 | 0 | 1 |
| `pkg/utils` | 8 | 0 | 4 (`ClearScreen`, `PrintHeader`) | 1 |
| `cmd/hardn` | 2 | 2 | 0 | 1 |
| `pkg/logging` | 0 | 5 | 0 | 0 |
| `pkg/style` (the library) | n/a | 0 | 83 (definitions) | n/a |

- **Most-used symbols outside `pkg/style`:** `Colored` 271, `BulletItem` 144, `Red` 111, `SymCrossMark` 96, `Green` 88, `MenuOption` 87, `Cyan` 84, `Bolded` 80, `Yellow` 76, `Dimmed` 69, `SymWarning` 50, `SymCheckMark` 50, `SymInfo` 48, `Blue` 45, `SymRightCarrot` 31, `PressAnyKey` 23, `NewMenu` 22, `NewStatusFormatter` 15.
- About 244 inline message lines combine a glyph and a colour (✗ red, ⭘ yellow, ✓ green, ℹ blue/cyan). They map one-to-one to the brief's `Message` component.
- Raw SGR strings are built in feature code from exported constants, e.g. `style.Bold + "hardn" + style.Reset`.
- **Per reachable file (style refs / `fmt.Print` lines):** user_options 226/83, firewall_menu 140/82, dns_menu 102/47, run_all_menu 97/50, backup_menu 88/31, main_menu 63/25, disable_root_menu 62/29, environment 52/26, ssh_keys_options 44/14, dry_run_menu 39/16, system_details_menu 37/14, ssh_keys_menu 24/17, dry_run_handler 16/10, logs 15/7, user_menu 13/18, run_all_handler 11/5. Dead files: sources_menu 232/108, help 72/33, linux_packages 72/31, python_packages 57/20.
- **v0.3.2:** `pkg/menu` 80 style refs + **158 raw ANSI** literals; status 7; utils 4 + 4 raw; cmd 1 colour; logging 4 colour.
- **arch3:** `legacy_pkg/adapter/inbound/menu` 1,425 (+86 in menu/system); terminal adapter 30 style + 23 raw ANSI; tui model 9 raw ANSI; driver 18; legacy terminal/box 21 fatih/color.

**Degradation defects in `pkg/style`** [V harness]:
- `UseColors=false` (from `--no-color`, or `NO_COLOR`) is honoured **only by `Colored`**. `Bolded`, `Dimmed`, `Underlined`, `Italicized` and `Striked` always emit SGR (harness output with `UseColors=false`: `"\x1b[1mx\x1b[0m\x1b[2my\x1b[0m\x1b[4mz\x1b[0mw"`). Box top borders are always dimmed (origin of this: `inventory.md` Part C).
- `main.go`'s `initializeColor()` sets `UseColors=true` unless `--no-color` is passed, overriding `NO_COLOR` [I] and forcing colour when output isn't a TTY.
- Unicode-vs-ASCII box drawing is keyed on `UseColors`, not on Unicode support.
- `PadRight` and `StatusFormatter` measure with `len()` bytes; `boxes.go` correctly uses `runewidth`.
- Muted text uses SGR faint (`Dimmed`); the brief wants explicit gray tokens.
- No TTY, `TERM` or locale detection exists anywhere in `cmd`/`pkg` on origin/main or arch3.

---

## 7. Status computation map (origin/main)

| Control | Main-menu source (`pkg/security/status.go`) | Other independent computations | Config or effective? |
|---|---|---|---|
| SSH root login | `checkRootLoginEnabled` (`:233-264`): reads `/etc/ssh/sshd_config.d/hardn.conf` if it exists, **else** `sshd_config`; the first `PermitRootLogin` line decides; only `no` counts as disabled, so `prohibit-password` shows "root allowed / Not Configured"; unreadable or unset = enabled | (1) `disable_root_menu.go:208-254`: checks hardn.conf **then falls through** to sshd_config, trims, accepts `No`. (2) An exported duplicate `CheckRootLoginEnabled` (`:489-520`) with no callers | Config-file grep; ignores Include order, other drop-ins (`50-cloud-init.conf` sorts first and wins [I]) and Match blocks. No `sshd -T` |
| SSH auth | `checkPasswordAuth` (`:456-487`): the same single-file grep for `PasswordAuthentication no` | none | Config-file grep; ignores KbdInteractive / ChallengeResponse |
| SSH port | `cfg.SshPort != 22` (`:52`) | `firewall_menu.go:75-82`, `disable_root_menu.go:191`, `run_all_menu.go:63` (all from config) | **`hardn.yml` only.** The firewall step allows `cfg.SshPort`, so a config/sshd mismatch is a lockout path [I] |
| Firewall | `checkFirewallStatus` (`:267-314`): `ufw status verbose` contains "Status: active"; configured = deny incoming + allow outgoing + any `ALLOW IN` with `/tcp` (not specifically SSH). Fallback: `iptables -L` with more than 3 "Chain" and the substring "ssh" | `UFWFirewallRepository.GetFirewallStatus`: `which ufw`, then **non-verbose** `ufw status`; "configured" requires "deny (incoming)", which only verbose output contains, so it is never true [I] | Effective, but two disagreeing definitions; no SSH-port-allowed cross-check |
| Users | `checkUserSecurity` (`:317-351`): any `/etc/sudoers.d` entry other than README/root, **or** a non-root member of `sudo`/`wheel` | `user_menu.go:123` hardcoded "Non-root Configured"; user details via `OSUserRepository` | Heuristic: cloud-init's sudoers drop-in, or hardn's own env_keep file, could count [I] |
| Sudo installed | `checkSudoConfiguration` (`:440-453`): `which sudo` + `/etc/sudoers` | — | Effective. The row appears only when sudo is missing |
| Sudo Method | **Computed during rendering**: the print function builds an `OSUserRepository` and calls `osdetect.GetOS()` (`:98-129`); this also reloads config and silences logging (`side-effects.md` row 4) | user detail block | Effective |
| AppArmor | `checkAppArmorStatus` (`:355-412`): `aa-status`; Alpine `apk info -e`, `rc-status`, `aa-status` | Run All "Enabled Features" shows the config flag | Effective; no container or Alpine NotApplicable state |
| Auto updates | `checkUnattendedUpgrades` (`:415-437`): `dpkg -l unattended-upgrades` + `systemctl is-enabled`; Alpine checks `/etc/periodic/daily/apk-upgrade` | Run All shows the config flag. Nothing on origin/main can configure it | Effective |
| Risk level | `GetSecurityRiskLevel` (`:177-230`): 8 equally weighted booleans. Firewall enabled and configured both count (double weight); port weight equals auth weight; sudo method not counted | none | Derived. Bands: 0–2 Critical "no security", 3–4 High "weak", 5–6 Moderate "medium", 7–8 Low "strong". **"Minimal / hardened" is unreachable** [V harness] |
| DNS | none (no main-menu row) | DNS screen: effective (`dns_menu.go:346-397`) plus configured nameservers | Both, labelled separately (good) |
| Uptime / pending reboot | uptime only (`FormatUptime`) | System Details | No reboot-required or kernel check anywhere |

- Every main-menu redraw re-runs all these checks (ufw, aa-status, dpkg, systemctl, which, user enumeration, host info): about 6–10 subprocesses per return to the menu [I]. The argv list is in `side-effects.md` §4.5.
- v0.3.2 used the same logic in `pkg/status/status.go`, reading `manage.conf` instead of `hardn.conf`, with different wording ("SSH Root Login Enabled/Disabled", "Root user only / Non-root user found", "Some protections active").

---

## 8. Main menu comparison

### 8.1 origin/main rendered (harness; fixture = the Figma scenario; ANSI stripped)

Left column = display width with ambiguous width counted as 1. Host name, address and port are substituted (§0); box lines are padded to 64, so their widths are unchanged.

```text
 58|hardn  v0.4.0             https://github.com/abbott/hardn
 65|░ Debian 12 (Bookworm) ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
 64|  <example-host>          192.0.2.10
 64|                          182 days, 2 hours, 26 minutes
 64|  ⛬  Risk Level           Moderate ≈ medium security
 64|  ⛛  Users                Configured non-root, sudo
 64|  ⛛  Sudo Method          Configured password required
 64|  ⛛  Firewall             Configured deny policy
 64|  ⛛  SSH Login            Configured root disallowed
 64|  ⛛  SSH Auth             Configured key-only auth
 64|  ⛛  SSH Port             Configured non-default (<port>)
 64|  ⭘  AppArmor             Not Configured
 64|  ⭘  Auto Updates         Not Configured
 65|  ───────────────────────────────────────────────────────────────
 48|  Select an option        Dry-run Mode Disabled
 60|   1) User Management     Create, Configure (sudo, SSH keys)
 48|   2) SSH Login           Toggle SSH root access
 47|   3) DNS                 Configure Nameservers
 45|   4) Firewall            Configure UFW rules
 57|   5) Backup              Configure Hardn backup settings
 42|   6) Dry-Run             Simulate changes
 54|   7) Run All             Execute hardening operations
 56|   8) Environment         Configure environment variable
 49|   9) System Details      View system information
 39|  10) Logs                View log file
 55|   0) Exit                Press 'q' to exit immediately
 33|  ❯ Enter selection [0-10 or q]:
```

Blank lines are omitted. Every line fits in 80 columns. Figma's main menu is about 36 lines tall (§9.3 item 11), so it scrolls on 80×24 consoles.

### 8.2 Element by element

| Element | Brief §1 transcription | Figma 00-TUI-MAIN | origin/main | v0.3.2 |
|---|---|---|---|---|
| Header | `hardn  v0.4.0 … URL` | same; version in a subtle box | same (`main_menu.go:314-350`). When an update is found, "vX available" replaces the URL | Dimmed ASCII "Hardn Linux" logo, no version |
| Distro bar | `▌Debian 12 (Bookworm) ███…` | Left stub plus a solid dark rectangle (fills, not glyphs) | `░ Debian 12 (Bookworm) ░░░…` (light-shade glyphs). **Proxmox renders as Debian** | Green `~~~ Debian Bookworm ~~~`; Proxmox shows "Proxmox" |
| Host block | badge, IP, uptime | badge on blue, IP, uptime | same (dark-blue badge) | none |
| Risk | `∴ Risk Level  Moderate ≈ medium security` | glyph **`⛬`**, same text | `⛬`, same text | `⛬ Risk Level Moderate ≈ Some protections active` |
| Status glyphs | `▽` / `○` | **`⛛` / `⭘`** | `⛛` (SymEnabled, dimmed) / `⭘` (SymWarning) | `◎` / `▲` |
| Status rows | User, Firewall, SSH Login, SSH Auth, SSH Port, AppArmor, Auto Updates | same 7 | **Users** (plural), *Sudo* (only when missing), **Sudo Method** (extra), then the same 6 | Different words |
| Menu title + mode | `Select an option   Dry-run Mode Disabled` | same (heading underlined) | same | A separate `✱ Dry-run Mode Disabled` line |
| Items | 1–10 | 1–10 | identical except item 2 "Toggle **SSH root** access" (Figma and brief: "Toggle root SSH access"); Figma dims "(sudo, SSH keys)" separately | 12 different items |
| Exit | `0) Exit  Press 'q' to exit immediately` | same, curly quotes | same, ASCII quotes | `0) Exit  Tip: Press 'q' to exit immediately` |
| Prompt | `❯ Enter your choice [0-10 or q]:` | same | `❯ Enter **selection** [0-10 or q]:` (changed in 607eafa, 2025-03-22) | `- Enter your choice [0-12 or q]:` |

Figma also contains stale **hidden** text in the main frame ("UID ≥ 1000" on the AppArmor and Auto Updates rows, and 7 stray "(sudo, SSH keys)" nodes). Use the tree JSON or PNG, not `texts.txt`, as the visible content.

### 8.3 The brief's §7 review items, checked against origin/main

1. **Rows vs items:** confirmed. AppArmor and Auto Updates have rows but no item; DNS has an item but no row. SSH Auth and SSH Port have rows but no screen that changes them. Sudo Method is a row Figma doesn't have.
2. **Run All:** worse than assumed: option 1 runs live with **no confirmation**, and the result claims steps that never ran.
3. **Dry-run ambiguity:** confirmed. The persisted `dryRun` is ignored at startup, and the Enabled-state menu is never printed.
4. **Uptime:** decoration only; no pending-reboot check anywhere.
5. **SSH status from `sshd -T`:** not done (single-file grep; port from `hardn.yml`); no firewall-allows-SSH-port cross-check.
6. **Risk transparency:** unweighted score; firewall counts twice; port weight = auth weight; top band unreachable.
7. **Wording:** "Toggle SSH root access" can only disable. "Configure Hardn backup settings" is capitalised. "Configure environment variable" manages only HARDN_CONFIG sudo preservation.

---

## 9. Figma mapping

The export has 11 frames in `screens/` plus 2 legacy-TUI images; 13 frames in total. The three filenames the brief names exist exactly as written. Gallery mock names = lowercase with `_` → `-` (e.g. `USER-MANAGE_SUDO-CONFIRMATION.png` ↔ `user-manage-sudo-confirmation`). The export is read by path from outside the repository (decision 3).

### 9.1 Frames → origin/main screens

| Figma frame (node) | origin/main screen | Layout and Figma content | Differences (Figma vs origin/main) |
|---|---|---|---|
| 00-TUI-MAIN (1:2601) | Main menu | Header badges + URL; distro bar; host badge, IP, uptime; risk; 7 status rows; full-width rule; heading + mode; 10 items + `0) Exit`; prompt | §8.2 |
| 01-USER (1:2253) | User Management, variant A | **TitleBar** (no Header, no breadcrumb); status row "Non-root"; **SectionHeader** "User Configuration"; user badge; dash KV list (Sudo "Enabled," + red "No password"; SSH Keys; UID:GID; Directory); 2 items + Return | "User Configuration" vs "Configuration"; "Manage User"/"Create User" vs "Manage a user"/"Create a user"; "Enabled, No password" vs "Enabled no password"; prompt wording; no last-login meta in Figma |
| USER-MANAGE_USER-SELECT_USER (1:122) | Manage User: select | TitleBar; underlined "Select a user"; two users (names withheld), no descriptions; Return | Prompt wording only. Empty-list state not shown |
| USER-MANAGE_USER-MANAGEMENT (1:198) | Manage User: management | TitleBar; **SubjectLine** "Managing: [user]"; heading; 2 items + Return (indented +2 cells) | Prompt wording only. Says "Add or remove SSH keys" while USER-SSH_KEYS offers only Add |
| USER-MANAGE_SUDO-CONFIRMATION (1:509) | Manage Sudo | TitleBar "Manage User"; SubjectLine; Warning **Callout**; SectionHeader + KV "Sudo Method — Password required" (green); "Require password for sudo? (y/n): y"; success callout; press-any-key | Title "Manage User" vs "Manage Sudo"; Figma shows the **target** state, origin/main the current one; ✓-sentence results; origin/main has **no confirmation** for the opposite direction |
| USER-SSH_KEYS (1:265) | Per-user SSH keys | TitleBar; SubjectLine "Managing SSH keys for: [user]"; `1) Add SSH key`; Return; `[0-1 or q]` | origin/main: no box; "ℹ Managing SSH keys for user:"; a key list; "Select SSH key operation"; optional Remove |
| USER-CREATE_USER (1:5) | Create New User | Numbered fields 1–5 with echoed answers; a review section (Sudo "Enabled, No password" red; SSH Keys "None" amber); "Create user 'george'? (y/n): n"; **cancelled** callout; press-any-key | "Grant sudo privilege?", "Require sudo password?" (**opposite polarity** to origin/main's "Allow sudo without password?"), "Add SSH key?". Only the cancelled path is shown. Hidden ⮕ field markers |
| 02-SSH_LOGIN (1:779) | SSH Login | TitleBar; 2 status rows (SSH Login, SSH Port `<port>/tcp`); SectionHeader; KV (Root SSH Login "Enabled" red, Port, Allowed Users); items "Root SSH Login" / "View SSH Settings" | Title "Manage Root SSH Login" vs "Disable Root SSH Access"; items "Toggle root SSH access" / "Show SSH configuration" vs "Disable root SSH access" / "View current SSH configuration". **Figma drift:** status "root disallowed" next to section "Root SSH Login Enabled" |
| SSH_LOGIN-TOGGLE_CONFIRMATION (1:597) | SSH Login confirm + apply | Warning callout with a 3-item BulletList; SectionHeader + KV; "Disable SSH login for 'root'? (y/n): y"; success callout; press-any-key | vs "Are you sure you want to disable root SSH access? (y/n)"; "Operation Successful" vs a 3–4 line restart narrative. **A lockout-risk change with a plain y/n** |
| SSH_LOGIN-SSH_SETTINGS_DISPLAY (1:707) | SSH Login: view | TitleBar, SectionHeader, KV (Root SSH Login, SSH Port, Allowed Users), press-any-key | Same 3 rows; origin/main has a dashed rule, then "Press any key to return to the main menu" |
| 05-BACKUP (1:1096) | Backup | TitleBar; 2 status rows; SectionHeader; KV (Backup State "Enabled" green; Backup Path + inline "Writable"); 3 items + Return; `[0-3 or q]` | Title "Manage Hardn Backups" vs "Backup Settings"; items "Backup State / Backup Path / Verify Directory" vs "Toggle backups (currently: X) / Change backup path / Verify backup directory". **Figma drift:** status rows copied from other screens ("UFW installed, deny policy", "SSH Port `<port>`/tcp") |
| images/03-dns-legacy-tui (1:2938) | DNS | A 2025-03-20 terminal **screenshot**, not a design: green `~~~` rule, "Enter your choice", "0) Return to main menu" | Pre-607eafa. origin/main differs only in "Enter selection", exit label "Return" and the `❯` on "Press any key" (verification C9) |
| images/04-firewall-legacy-tui (1:2939) | Firewall | Same era: "UFW Firewall Configuration", status rows, "No firewall rules configured", 3 items | Same as above. "No firewall rules configured" next to "UFW Status Active" fits the non-verbose `ufw status` defect [I] |

**Conventions Figma bakes in that conflict with brief §2/§1C:** "Press any key to continue..." in 4 frames; `(y/n)` with no capitalised default in 4 frames; a lockout-risk SSH toggle confirmed with a plain y/n; collect, confirm and result on one screen. Mocks must split these into the canonical flow and log the departures in `proposals.md`.

### 9.2 Components Figma uses that brief §1B lacks

| Component | Export candidate id(s) | Seen in |
|---|---|---|
| TitleBar (a ContextBar variant; sub-screens have no Header or Breadcrumb) | b8cbe5da | 10 frames |
| SectionHeader (stub + rule) | a4701d0e | 7 frames |
| MenuHeading (underlined; underline only in the PNGs) | — | 00, 01, 02, 05, user menus |
| Callout (accent bar + bold title + muted body + optional bullets; ok / warn / cancelled) | bf9038e5, 6abe89f5 | confirmations, create user |
| SubjectLine (label + badge) | ad50041d | user management screens |
| BulletList | — | SSH toggle warning |
| AnsweredField / confirm echo | 85c87931, 824bb873 | create user, confirmations |
| Exit/Return item style (muted) | ad108bad | all menus |
| KV value carrying semantic state ("Enabled" green in one screen, red in another: colour follows meaning) | 4f55bc9f, 72d8f8a4, 0b24ef06, abd1f7cf | 01, 02, 05 |
| Menu (items, list, descriptions) | 6f82f470, 1726e2d0, 411eadb8 | all menus |
| PromptLine | 213f2a96 | all |
| StatusRow / StatusList | d741958a, c1390b17, e8e27283 | 00, 01, 02, 05 |
| "Press any key" (to be replaced) | 5e67fe79 | 4 frames |

### 9.3 00-TUI-MAIN vs the brief's §1 transcription [V]

1. **Glyphs:** Figma and code use ⛛ (U+26DB), ⭘ (U+2B58), ⛬ (U+26EC), not ▽ ○ ∴. All six are East Asian **Ambiguous** width, as are ≈, ▌, █ and ─; only ❯, ░, ✓, ✗ and ℹ are narrow (Unicode 15.0). The code constants date from 5189b6f (2025-03-18); v0.3.2 used ◎ and ▲.
2. **Distro bar:** not glyphs. The stub and fill are #101114 rectangles; the distro text sits on a #000000 knockout, not on the bar. "Debian" bold muted, "12" regular muted, "(Bookworm)" faint.
3. **Host badge:** no brackets; badge background #0C2AC0; text #C0C4D8 bold, **not white**.
4. **Columns:** the transcription puts values and the URL at col 33. Figma puts the value column at **col ≈ 26.7** from the bar edge and the URL at col ≈ 22.5 (not aligned). Status labels at col ≈ 5.9; glyphs at col ≈ 2.5–2.8. Proposed grid: content indent col 2, marker cols 2–4, label col 6 (width 21), **value col 27**, 64-cell content width (= origin/main's `NewBox(Width: 64)`).
5. **Divider:** a 1-px #16181D rule across the full 64-cell panel with about one blank line above and below (the transcription has `──` from col 2 with none).
6. **Two grays:** muted #81869C (descriptions, IP, mode, prompt) and faint #2B2F3B (status details, uptime, URL, codename, "≈ medium security", parentheticals). Faint is 1.57:1 on black and the divider 1.18:1: near-invisible.
7. **Exit row:** `0)` and "Exit" are muted, unlike items 1–10 (bold primary keys); typographic quotes in the description.
8. **Prompt:** `❯` muted and smaller; "Enter your choice" bold muted; `[0-10 or q]:` regular muted; a #C0C4D8 block cursor.
9. **Glyph colour follows state** (green ⛛, red ⭘, amber ⛬); the brief does not say so.
10. **Matches:** the order and wording of all rows match the transcription. "Risk Level" bold; "Moderate" and "Disabled" amber; "Configured" green; "Not Configured" red; "Select an option" underlined (PNG only); version in a #101114 box; "hardn" bold.
11. **Height:** about 36 lines; sub-screens 18–23 lines.

### 9.4 Screens with no Figma frame: source labels

**Labels** (plan review template item 6, re-applied per decision 6 and plan §3.25):
- **Figma file**: a frame exists (§9.1).
- **inferred from origin/main**: the screen exists on origin/main with no frame. origin/main is the baseline; the v0.3.2 column is consulted only where origin/main regressed.
- **inferred from v0.3.2**: the screen exists only on v0.3.2 (dropped on origin/main); restoring it is plan §3.12's call.
- **none (new)**: no ref has it; it is a new design under the brief or a proposal.

The findings draft labelled DNS, Firewall, Dry-Run, Run All and Logs "infer from v0.3.2". **Decision 6 relabels them "inferred from origin/main"**, with v0.3.2 as the regression reference.

| Screen | Source label | v0.3.2 equivalent | origin/main change since v0.3.2 (delta) | Regression reference from v0.3.2 |
|---|---|---|---|---|
| DNS | inferred from origin/main (the Figma file holds only a pre-607eafa screenshot) | "Configure DNS Settings": warning, one `(y/n)`, then apply configured nameservers | Full screen: effective vs configured nameservers; 6 options (add, remove, presets); recursive; **apply has no confirmation** | A confirmation before apply |
| Firewall + Manage UFW Application Profiles | inferred from origin/main (Figma: screenshot only) | "UFW Firewall Configuration": "SSH access will be allowed on port N", one `(y/n)`, then configure | Status block, 3 options, a profiles sub-screen with a 4-field form; Enable/Configure have no confirmation; 3 actions are stubs | Confirmation; UFW install |
| Dry-Run | inferred from origin/main | One screen: status, then "Would you like to enable/disable dry-run mode? (y/n)" | 3-screen handler + menu, number-based; the Enabled-state menu is not printed | One screen, one answer |
| Run All (summary, confirm, progress, result) | inferred from origin/main | Warning + dry-run note, `(y/n)`, then all steps incl. sources, packages, hushlogin, AppArmor, Lynis, unattended upgrades | Summary screen; option 1 has **no confirmation**; the live path covers only user/SSH/firewall/DNS; fake progress; a separate dry-run simulation | Confirmation before running; the dropped steps (§3.12) |
| Environment (+ guide) | inferred from origin/main | **does not exist** (added 834dc4b) | origin/main only | — |
| System Details (+ export) | inferred from origin/main (plus the 2025 hex prototypes' section layout as ideas, plan §6) | **does not exist** | origin/main only | — |
| Logs | inferred from origin/main | "View Logs": dump | Adds a "Log file:" line and a "Log Contents:" rule; still a full dump | — |
| User Management variant B (Enter/Change username, sudo-password toggle in config, config SSH Key Management, Create user from config) | inferred from origin/main | "User Creation Menu": prompt for a username if unset, then create immediately, **no confirmation** | Variant B menu; the config SSH Key Management screen ships placeholder text | — |
| Per-user SSH keys: Remove flow, paste prompt, results | inferred from origin/main | n/a | Exists; reports false success on remove (V22) | — |
| Manage Sudo, "allow passwordless" direction | inferred from origin/main | n/a | Exists; no confirmation | — |
| Create New User: success, failure and validation states | inferred from origin/main | n/a | Exist; validation failure exits the flow | — |
| SSH Login: "already disabled", failures, restart failure | inferred from origin/main (Figma covers the main states) | "Disable Root SSH Access": warning, `(y/n)`, restart | Status + checklist + 2 options; returns to main | In-place `PermitRootLogin` edit (V4) |
| Backup sub-flows (state toggle, path input, verify result) | inferred from origin/main (Figma covers the menu only) | Numeric `[0-2]` sub-menu | Toggle, path, verify | — |
| Main-menu variants: update available, security-update alert, dry-run Enabled, each risk band, Sudo-not-installed row | inferred from origin/main | n/a | origin/main only | — |
| Main invalid-option page; Exit | inferred from origin/main | Printed "Hardn has exited." | Clears silently | The exit message |
| Pre-menu: first-run prompts, HARDN_CONFIG NOTICE, non-root refusal | inferred from origin/main | Same prompts | Unchanged in substance ("george" default added) | — |
| Help | inferred from v0.3.2 (pending §3.12) | Menu item 12 | Unreachable and stale | — |
| Linux Packages, Python Packages, Package Sources (+ repository editors) | inferred from v0.3.2 (pending §3.12) | Items 7, 8, 9; execute immediately | Removed from the menu and the flags removed (ea8680f); dead code remains | — |
| AppArmor, Auto Updates, SSH Auth, SSH Port | none (new; brief Phase 5) | **No screen** (config + Run All only) | **No screen and no reachable operation** | v0.3.2's operations, not screens |
| Plan preview, step progress, error report, confirm-or-revert countdown, `hardn confirm` | none (new; brief requirements) | none | none | — |

### 9.5 States with no frame anywhere

Unknown, NotApplicable and Partial status; error callout; validation error under a prompt; dry-run / PlanView; StepProgress; ErrorReport; the confirm-or-revert countdown; empty lists; narrow width; ASCII glyph set; light theme. Glyphs for Unknown / NotApplicable / Partial have no Figma source; keep them narrow or ASCII (`?`, `-`, `~`).

---

## 10. Other interaction defects [V code]

- `config.SaveConfig(m.config, "hardn.yml")` at 15 reachable sites writes the **whole config** to `./hardn.yml` in the current directory, not to the loaded path. Run from `/root`, edits land in `/root/hardn.yml`, which is last in the search order, so the next start may ignore them [I].
- Every recursive `m.Show()` grows the stack (27 sites); code after a `switch` runs again on unwind (the Firewall double pause).
- The two "Add SSH key" flows validate differently (prefix-checked vs unchecked). DNS IP validation is only "4 dot-separated parts".
- Section widths are mixed on one screen: box 64 vs `SectionDivider(…, 72)` on Create New User.

---

## 11. arch3 interaction notes

- **Legacy path** (`cmd/legacy`, `legacy_pkg/adapter/inbound/menu/*`): origin/main's menu code renamed. The main-menu diff shows only import paths and item 9 (System Details) commented out, so 9 does nothing.
- **New path** (`cmd/hardn/menu`): a data-driven `Menu`/`Option` model, a `Screen`/`Section`/`Content`/`Header` view model rendered by `TerminalAdapter.DisplayScreen`, one item (System Details) plus Exit. It is the nearest precursor of the brief's view-model + `Render(vm, env)` idea, but: renderers print directly; ANSI is hand-written (23 literals); the header hardcodes `"hardn  v0.4.0"`; the prompt range is hardcoded; input is raw with `q`-anywhere and empty-Enter-runs-option-1; Ctrl-C exits 0.
- It adds a macOS firewall/MAC status implementation: scope drift for a Linux-only tool.
- Reference only (the brief forbids building on old branches): the `Option{Action}` binding, which removes the selectable-hidden-options class of bug; the `Screen`/`Section` view-model shape.
