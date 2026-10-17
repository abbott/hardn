# Enhancement proposals

> **What this is:** the enhancement log from brief §2 (Design fidelity). An enhancement improves on the design: a new state, clearer wording, an extra component or a better interaction. **The maintainer decides each one.** Nothing here is implemented until its status is `approved`.
>
> **Not here:** deviations, where the code differs from Figma because of terminal limits or export drift. The agent decides those and records them in [../design-system/deviations.md](../design-system/deviations.md). If a change alters meaning, flow or wording, it belongs here; small, obvious fixes are deviations.
>
> **Seeded:** 2026-10-16, from brief §7 (P-01 to P-07, the Figma main-menu review items) and from the refactor plan §5 (P-08 to P-20). Evidence: [phase0-findings.md](phase0-findings.md), [phase0/interaction.md](phase0/interaction.md), [phase0/side-effects.md](phase0/side-effects.md), [phase0/inventory.md](phase0/inventory.md) and [phase0/v032-reference.md](phase0/v032-reference.md).

## How to read an entry

- **Status:** `proposed` → `approved` / `rejected` / `deferred` → `implemented` (with the branch that implements it).
- **Effort** [I], an estimate:
  - **S** fits in one small change unit (≲200 lines);
  - **M** is one full unit (≲500 lines, excluding goldens);
  - **L** spans several units.
- **Risk** [I]: what could go wrong for users if it ships, and why.
- **Screenshots:**
  - **Before** is a `freeze` capture of the current origin/main screen, or the name of a Figma frame. Figma PNGs are a reference that is never committed.
  - **After** is a capture of the gallery mock.
  - Images go under `docs/refactor/img/`, which the allow-list covers via `docs/**/*.png`. Until captured, entries show a placeholder path.
- **V-numbers** refer to the defect list in [phase0/side-effects.md](phase0/side-effects.md) (V1–V27). Under decision 6, a V-number is a defect to fix, and fixing it doesn't need an enhancement entry. Entries cite V-numbers where the proposal goes beyond the fix.

## Index

| ID | Title | Source | Area / slice | Effort | Risk | Status |
|---|---|---|---|---|---|---|
| [P-01](#p-01) | Line up status rows and menu items | brief §7.1 | Main menu; slice 7 | L | Medium | proposed |
| [P-02](#p-02) | Make Run All hard to trigger by accident | brief §7.2 | Main menu; slice 8 | M | Low | proposed |
| [P-03](#p-03) | Resolve the dry-run model | brief §7.3 | Dry-Run; slice 2b | M | Medium | proposed |
| [P-04](#p-04) | Treat uptime as a finding (pending reboot) | brief §7.4 | HostBlock; Phase 4 | M | Low | proposed |
| [P-05](#p-05) | SSH statuses from `sshd -T`, with a firewall cross-check | brief §7.5 | Status; Phase 4 | M | Low | proposed |
| [P-06](#p-06) | Make the risk score transparent | brief §7.6 | RiskSummary; Phase 4 | M | Low | proposed |
| [P-07](#p-07) | Main-menu wording | brief §7.7 | Main menu | S | Low | proposed |
| [P-08](#p-08) | Sub-screen header: Header + Breadcrumb, or Figma's TitleBar | plan §5.1 | Layouts; Phase 1 | S | Low | proposed |
| [P-09](#p-09) | Replace "Press any key to continue" | plan §5.2 | All screens; Phase 1C | M | Low | proposed |
| [P-10](#p-10) | Typed confirmation + confirm-or-revert where Figma shows `(y/n)` | plan §5.3 | SSH, sudo; slices 5–6 | M | Low | proposed |
| [P-11](#p-11) | Polarity of the sudo-password question | plan §5.4 | User Management; slice 5 | S | Medium | proposed |
| [P-12](#p-12) | Name the result instead of "Toggle root SSH access" | plan §5.5 | SSH Login; slice 6 | S | Low | proposed |
| [P-13](#p-13) | "Sudo Method" row; "Users" vs "User" | plan §5.6 | Main menu | S | Low | proposed |
| [P-14](#p-14) | Reachable risk bands, with factors shown | plan §5.7 | RiskSummary; Phase 4 | S | Low | proposed |
| [P-15](#p-15) | Update checker: opt-in and non-blocking | plan §5.8 | Startup; Phase 3 | M | Low | proposed |
| [P-16](#p-16) | Restore UFW install; add AppArmor and Auto Updates screens | plan §5.9 (brief §7.1) | Firewall, AppArmor, Updates; slices 4, 7 | L | Medium | proposed |
| [P-17](#p-17) | System Details content | plan §5.10 | System Details; slice 1 | M | Low | proposed |
| [P-18](#p-18) | Logs via `$PAGER` or a tail, from the file hardn writes | plan §5.11 | Logs; slice 1 | S | Low | proposed |
| [P-19](#p-19) | Backups: a restore UI | plan §5.12 | Backup; slice 3 | L | Medium | proposed |
| [P-20](#p-20) | Proxmox: manage pve-firewall rules for SSH and 8006 | plan §5.13 (brief §2.5) | Firewall on Proxmox; after slice 4 | L | High | proposed |
| [P-21](#p-21) | Pending-reboot wording and place in the HostBlock | `next/p1b-header` | HostBlock; Phase 4 | S | Low | proposed |
| [P-22](#p-22) | Menu item markers: lockout risk and unavailable | `next/p1b-menu` | Menu; slices 4–8 | S | Low | proposed |
| [P-23](#p-23) | Words for the message kinds | `next/p1b-blocks` | Message, Callout; Phase 1C | S | Low | proposed |
| [P-24](#p-24) | Words for the apply steps and the confirm countdown | `next/p1b-plan` | StepProgress, ConfirmCountdown; Phases 1C, 2 | S | Low | proposed |
| [P-25](#p-25) | Navigation: free text, `0` off menus, the refusals' words, the typed phrase and explaining the inputs | `next/p1c-nav` | Every prompt; Phase 1C | S | Low | proposed |
| [P-26](#p-26) | Forms: back through the fields, the earlier answer and a free-text default | `next/p1c-form` | Every form; Phase 1C | S | Low | proposed |

---

## Brief §7: Figma main-menu review items

<a id="p-01"></a>
### P-01: Line up status rows and menu items

- **Status:** proposed
- **Source:** brief §7.1. Related: [P-16](#p-16), [P-13](#p-13).
- **Problem:**
  - AppArmor and Auto Updates show a status ("Not Configured") but have no menu item. DNS has a menu item but no status row [V, `00-TUI-MAIN` and origin/main `main_menu.go`].
  - SSH Auth and SSH Port also have rows with no screen that changes them.
  - On origin/main, AppArmor, Lynis and Auto Updates can't be applied at all: the code is dead. Run All still prints them as configured (V23).
- **Proposal:**
  - Drive status rows and menu items from one list of controls (the Phase 3/4 `Control` registry). Every row then either has a menu item or is marked read-only, and DNS gains a status row.
  - The AppArmor and Auto Updates screens themselves are [P-16](#p-16).
- **Effort:** L (new screens, plus status sources for each control).
- **Risk:** Medium. It adds apply paths that are unreachable today, and the menu numbering changes (hints and help are generated, so no hard-coded ranges break).
- **Screenshots:**
  - Before: Figma `00-TUI-MAIN.png` (reference only); `docs/refactor/img/p-01-before.png` (placeholder).
  - After: `docs/refactor/img/p-01-after.png` (placeholder).

<a id="p-02"></a>
### P-02: Make Run All hard to trigger by accident

- **Status:** proposed
- **Source:** brief §7.2.
- **Problem:**
  - Run All is item 7, one keystroke and Enter away in live mode.
  - On origin/main, option 1 inside Run All runs `HardenSystem` with no confirmation at all [V, harness A4]: `7`, Enter, `1`, Enter.
- **Proposal:**
  - Set Run All apart visually, e.g. in its own group below a divider, as the last numbered item.
  - Always show the combined `PlanView`, and require a typed confirmation (e.g. the hostname).
  - The brief already decides the plan preview and typed confirmation (Phase 5 item 8), so the open question here is placement and numbering.
- **Effort:** M.
- **Risk:** Low. Safer by construction; renumbering affects muscle memory only.
- **Screenshots:** Before: `docs/refactor/img/p-02-before.png` (placeholder). After: `docs/refactor/img/p-02-after.png` (placeholder).

<a id="p-03"></a>
### P-03: Resolve the dry-run model

- **Status:** proposed
- **Source:** brief §7.3. Related: decision on flag/config precedence (plan §3.19).
- **Problem:**
  - The banner says "Dry-run Mode Disabled", while item 6 says "Simulate changes".
  - On origin/main:
    - the CLI ignores `-n` (V1);
    - the YAML `dryRun` key is always overwritten by the flag default (V3);
    - the Dry-Run menu's Enabled state is never printed [V].
- **Proposal:**
  - Under the canonical flow, every action previews its plan before confirming, so a global toggle is mostly redundant.
  - Option (a): keep item 6 as a mode switch, shown by `ModeIndicator` as "Plan only" or "Live".
  - Option (b): drop item 6, renumber, and keep `-n/--dry-run` as a global "plan only" mode.
  - Either way: an explicitly set flag wins; otherwise the config value applies.
- **Effort:** M.
- **Risk:** Medium. Removing a menu item and changing a config key's meaning are surface changes that need a deprecation note.
- **Screenshots:** Before: `docs/refactor/img/p-03-before.png` (placeholder). After: `docs/refactor/img/p-03-after.png` (placeholder).

<a id="p-04"></a>
### P-04: Treat uptime as a finding (pending reboot)

- **Status:** proposed
- **Source:** brief §7.4.
- **Problem:** the HostBlock shows uptime as decoration. A long uptime with automatic updates not configured probably means an old running kernel.
- **Proposal:**
  - Show a pending-reboot flag next to the uptime. Sources:
    - the running kernel vs the newest installed one;
    - `/var/run/reboot-required` where it exists;
    - on Alpine, a missing `/lib/modules/$(uname -r)`.
  - Long uptime without automatic updates adds a risk factor ([P-06](#p-06)).
- **Effort:** M.
- **Risk:** Low (read-only).
- **Screenshots:** Before: `docs/refactor/img/p-04-before.png` (placeholder). After: `docs/refactor/img/p-04-after.png` (placeholder).

<a id="p-05"></a>
### P-05: SSH statuses from `sshd -T`, with a firewall cross-check

- **Status:** proposed
- **Source:** brief §7.5 (brief Phase 4 already requires this source; this entry covers what the user sees).
- **Problem:**
  - On origin/main, SSH Port status is `cfg.SshPort != 22`, from the config, not the effective port [V].
  - The other SSH rows are read from files, so a drop-in that sorts earlier silently overrides them (V13).
  - On Ubuntu 24.04/26.04, `ssh.socket` decides the listening port.
- **Proposal:**
  - Report the effective set: the `sshd -T` values plus the `ssh.socket` Listen set on socket hosts. When they disagree, report `Unknown` with a finding.
  - A default-deny firewall combined with an SSH listener the firewall doesn't allow is a **critical** finding.
- **Effort:** M.
- **Risk:** Low (read-only). Without sshd, or when `sshd -T` fails for an environment reason, the status is `Unknown`.
- **Screenshots:** Before: `docs/refactor/img/p-05-before.png` (placeholder). After: `docs/refactor/img/p-05-after.png` (placeholder).

<a id="p-06"></a>
### P-06: Make the risk score transparent

- **Status:** proposed
- **Source:** brief §7.6. Related: [P-14](#p-14).
- **Problem:**
  - "Moderate ≈ medium security" says the same thing twice.
  - The score is 8 equally weighted booleans, and the firewall counts twice (enabled + configured) [V].
  - The user can't see why the level is what it is.
- **Proposal:**
  - Define the weighted scoring in one place and document it:
    - authentication settings weigh more than a non-default port;
    - the firewall counts once;
    - `NotApplicable` is excluded;
    - `Unknown` counts as not configured and adds a factor saying so.
  - `RiskSummary` lists the contributing factors instead of a second adjective.
- **Effort:** M.
- **Risk:** Low. Displayed levels change for the same host, so the release notes should say so.
- **Screenshots:** Before: `docs/refactor/img/p-06-before.png` (placeholder). After: `docs/refactor/img/p-06-after.png` (placeholder).

<a id="p-07"></a>
### P-07: Main-menu wording

- **Status:** proposed (each part is decided separately)
- **Source:** brief §7.7.
- **Problem and proposal:**
  - **(a) Item 2:** "Toggle root SSH access" (Figma; origin/main says "Toggle SSH root access") doesn't say which way it switches, and on origin/main it can only disable [V]. Name the result instead; see [P-12](#p-12).
  - **(b) Capitalisation:** "Hardn" becomes "hardn", e.g. item 5's "Configure Hardn backup settings" [V, origin/main `main_menu.go:454`].
  - **(c) Item 8:** "Configure environment variable" should name the variable. The screen manages only sudo preservation of `HARDN_CONFIG` [V]. Proposed wording: "Keep HARDN_CONFIG under sudo".
- **Effort:** S.
- **Risk:** Low (wording only; goldens are updated deliberately).
- **Screenshots:** Before: `docs/refactor/img/p-07-before.png` (placeholder). After: `docs/refactor/img/p-07-after.png` (placeholder).

---

## Refactor plan §5: further proposals

<a id="p-08"></a>
### P-08: Sub-screen header: Header + Breadcrumb, or Figma's TitleBar

- **Status:** proposed
- **Source:** plan §5.1.
- **Problem:** the brief's sub-menu layout uses `Header` + `Breadcrumb`. Every Figma sub-screen (`01-USER`, `02-SSH_LOGIN`, `05-BACKUP`, the `USER-*` and `SSH_LOGIN-*` frames) uses a single TitleBar, with no header and no breadcrumb.
- **Proposal:**
  - (a) Keep the brief's Header + Breadcrumb. This costs about 2 lines on an 80×24 console.
  - (b) Follow Figma's TitleBar and put the breadcrumb text inside it, e.g. "hardn › User Management".
- **Effort:** S.
- **Risk:** Low. The main cost is the height budget on small consoles.
- **Screenshots:**
  - Before: Figma `01-USER.png` (reference only).
  - After: `docs/refactor/img/p-08-after-a.png`, `p-08-after-b.png` (placeholders).

<a id="p-09"></a>
### P-09: Replace "Press any key to continue"

- **Status:** proposed
- **Source:** plan §5.2.
- **Problem:**
  - origin/main has 32 such strings, 55 `ReadKey` call sites and 23 `PressAnyKey` calls [V]. Figma shows the prompt in 4 frames.
  - `ReadKey` relies on GNU-only `stty -F` in cbreak mode, against the brief's Enter-based input decision.
- **Proposal:**
  - "Enter to continue" only where output must be read before the screen changes.
  - Otherwise, return to the menu with the result left on screen (canonical flow, step 6).
- **Effort:** M (legacy input is routed through the single line reader first).
- **Risk:** Low.
- **Screenshots:**
  - Before: Figma `SSH_LOGIN-SSH_SETTINGS_DISPLAY.png` (reference only).
  - After: `docs/refactor/img/p-09-after.png` (placeholder).

<a id="p-10"></a>
### P-10: Typed confirmation + confirm-or-revert where Figma shows `(y/n)`

- **Status:** proposed
- **Source:** plan §5.3. It records a departure from the Figma frames; the brief already decides the behaviour (1C confirmations; 2.4 confirm-or-revert).
- **Problem:**
  - `SSH_LOGIN-TOGGLE_CONFIRMATION` and `USER-MANAGE_SUDO-CONFIRMATION` confirm lockout-risk changes with a plain `(y/n)`, with no default.
  - Each puts collect, confirm and result on one screen.
- **Proposal:**
  - Split each into the canonical flow (collect → review → plan → confirm → apply → return).
  - Use a typed confirmation (e.g. the hostname) for SSH, firewall and sudo-method changes.
  - Add confirm-or-revert for SSH and firewall changes: 180 s by default, confirmed by `sudo hardn confirm` from a new session.
- **Effort:** M.
- **Risk:** Low. It adds steps, but each one is a safety check.
- **Screenshots:**
  - Before: Figma `SSH_LOGIN-TOGGLE_CONFIRMATION.png`, `USER-MANAGE_SUDO-CONFIRMATION.png` (reference only).
  - After: `docs/refactor/img/p-10-after-*.png` (placeholders, one per step).

<a id="p-11"></a>
### P-11: Polarity of the sudo-password question

- **Status:** proposed
- **Source:** plan §5.4.
- **Problem:** Figma `USER-CREATE_USER` asks "Require sudo password?". The code asks "Allow sudo without password?" [V]. The polarity is opposite, so the same `y` means opposite things.
- **Proposal:** use one wording everywhere. Recommended [I]: the Figma direction, "Require a password for sudo? [Y/n]", with the safer answer as the default.
- **Effort:** S.
- **Risk:** Medium. Inverting a question can catch users who answer from habit; the goldens and the review summary must show the result plainly.
- **Screenshots:**
  - Before: Figma `USER-CREATE_USER.png` (reference only); `docs/refactor/img/p-11-before.png` (placeholder).
  - After: `docs/refactor/img/p-11-after.png` (placeholder).

<a id="p-12"></a>
### P-12: Name the result instead of "Toggle root SSH access"

- **Status:** proposed
- **Source:** plan §5.5. Overlaps [P-07](#p-07) (a).
- **Problem:** "Toggle" hides the direction, and on origin/main the screen can only disable root login [V].
- **Proposal:**
  - Label the item by its result, computed from the effective state: "Disallow root login" or "Allow root login (key-only)".
  - On Proxmox, root stays key-only (brief §2), so the item reads "Root login: key-only (required by Proxmox clustering)" and is read-only.
- **Effort:** S.
- **Risk:** Low.
- **Screenshots:**
  - Before: Figma `02-SSH_LOGIN.png` (reference only).
  - After: `docs/refactor/img/p-12-after.png` (placeholder).

<a id="p-13"></a>
### P-13: "Sudo Method" row; "Users" vs "User"

- **Status:** proposed
- **Source:** plan §5.6.
- **Problem:** origin/main's main menu labels the row "Users" and adds a "Sudo Method" row. Figma shows "User" and no Sudo Method row [V].
- **Proposal:**
  - (a) Follow Figma: use "User", and fold the sudo method into that row's detail, e.g. "non-root, sudo (password)".
  - (b) Keep the extra row, at the cost of one line.
- **Effort:** S.
- **Risk:** Low.
- **Screenshots:** Before: `docs/refactor/img/p-13-before.png` (placeholder). After: `docs/refactor/img/p-13-after.png` (placeholder).

<a id="p-14"></a>
### P-14: Reachable risk bands, with factors shown

- **Status:** proposed
- **Source:** plan §5.7. Depends on [P-06](#p-06).
- **Problem:** the top band, "Minimal / hardened security", is unreachable: the maximum score is 8, and the band needs more than 8 [V].
- **Proposal:** set band thresholds that a fully hardened host actually reaches, and show the factors that kept a host out of the top band.
- **Effort:** S (once P-06 exists).
- **Risk:** Low.
- **Screenshots:** Before: `docs/refactor/img/p-14-before.png` (placeholder). After: `docs/refactor/img/p-14-after.png` (placeholder).

<a id="p-15"></a>
### P-15: Update checker: opt-in and non-blocking

- **Status:** proposed
- **Source:** plan §5.8.
- **Problem** (origin/main):
  - Every interactive start makes an unauthenticated GitHub API call, from two racing goroutines.
  - Root writes the cache to the predictable `/tmp/hardn-version-cache.json`. An unprivileged user can plant that file, so root's hardn shows a fake "Update Binary <URL>" screen and exits [V, harness].
  - A release mentioning "security" replaces the menu and exits.
  - There is no opt-out.
- **Proposal:**
  - Off by default; opt-in by flag or config.
  - Cache in a root-owned directory and validate it.
  - Never hide the menu or exit; show at most one notice line.
  - Until then, the post-review hazard gate turns the checker off behind a named constant.
- **Effort:** M.
- **Risk:** Low. It removes a network call and a `/tmp` write; the default changes, so it needs approval.
- **Screenshots:** Before: `docs/refactor/img/p-15-before.png` (placeholder). After: `docs/refactor/img/p-15-after.png` (placeholder).

<a id="p-16"></a>
### P-16: Restore UFW install; add AppArmor and Auto Updates screens

- **Status:** proposed
- **Source:** plan §5.9; brief §7.1. Related: [P-01](#p-01).
- **Problem:**
  - v0.3.2 could install UFW and set up AppArmor, Lynis and unattended-upgrades.
  - On origin/main that code is unreachable, while Run All reports AppArmor and Lynis as done (V23).
  - No ref has an AppArmor or Auto Updates screen.
- **Proposal:**
  - **UFW install:** restore it, but never on Proxmox (brief §2).
  - **AppArmor screen:** Debian and Ubuntu. `NotApplicable` on Alpine unless it is already enabled, and inside containers.
  - **Auto Updates screen:** unattended-upgrades on Debian/Ubuntu; `apk-cron` plus an enabled `crond` on Alpine.
  - **Lynis:** Debian and Ubuntu only.
- **Effort:** L.
- **Risk:** Medium. Package installs and AppArmor enforcement can affect running services [I]. Every step goes through plan/apply with rollback.
- **Screenshots:**
  - Before: n/a (no screen exists).
  - After: `docs/refactor/img/p-16-after-*.png` (placeholders; inferred screens, labelled as such in the gallery).

<a id="p-17"></a>
### P-17: System Details content

- **Status:** proposed
- **Source:** plan §5.10.
- **Problem:**
  - There is no Figma frame, and the screen is not in v0.3.2.
  - origin/main's gopsutil-based report has problems:
    - it uses a fixed 50-column box (`pkg/system/display.go:32`);
    - its renderer does I/O;
    - it clears the screen;
    - it defaults to a hard-coded ZFS dataset (`pkg/system/details.go:85`);
    - its export writes a path relative to the current directory, as root [V].
- **Proposal:**
  - Build it from design-system components, using the 2025 prototype sections as reference: OS and kernel, host and IPs, users, CPU and load, disk, memory, last login.
  - Label it "inferred from origin/main (unreleased)".
  - Exports go through `--output json`.
- **Effort:** M.
- **Risk:** Low (read-only).
- **Screenshots:** Before: `docs/refactor/img/p-17-before.png` (placeholder). After: `docs/refactor/img/p-17-after.png` (placeholder).

<a id="p-18"></a>
### P-18: Logs via `$PAGER` or a tail, from the file hardn writes

- **Status:** proposed
- **Source:** plan §5.11.
- **Problem:**
  - The Logs screen prints the entire file.
  - It reads `cfg.LogFile`, while hardn always writes the hard-coded `/var/log/hardn.log`, so the two diverge when `logFile` is changed [V].
- **Proposal:**
  - Open the log in `$PAGER`/`less` when available; otherwise show the last N lines.
  - Read the file hardn actually writes.
- **Effort:** S.
- **Risk:** Low.
- **Screenshots:** Before: `docs/refactor/img/p-18-before.png` (placeholder). After: `docs/refactor/img/p-18-after.png` (placeholder).

<a id="p-19"></a>
### P-19: Backups: a restore UI

- **Status:** proposed
- **Source:** plan §5.12.
- **Problem:**
  - origin/main backs up nothing (`enableBackups` is a no-op).
  - v0.3.2 kept only basenames and ignored errors.
  - Neither has a restore path [V].
- **Proposal:**
  - Keep per-run backups under `<backupPath>/<run-id>/`, with a manifest of original paths.
  - The Backup screen gains "Restore a previous run": list the runs, show the plan, confirm, apply.
- **Effort:** L.
- **Risk:** Medium. A restore writes `/etc` files, so it goes through plan/apply, and through confirm-or-revert for SSH and firewall files.
- **Screenshots:**
  - Before: Figma `05-BACKUP.png` (reference only).
  - After: `docs/refactor/img/p-19-after.png` (placeholder).

<a id="p-20"></a>
### P-20: Proxmox: manage pve-firewall rules for SSH and 8006

- **Status:** proposed
- **Source:** plan §5.13; brief §2.5 asks for this to be logged.
- **Problem:** on Proxmox hosts, hardn will only *report* pve-firewall's state (whether it is enabled, the default incoming policy, whether SSH and the web UI on port 8006 are allowed). It never manages the rules.
- **Proposal:** a later phase plans and applies `host.fw` / `cluster.fw` changes that allow SSH (22) and 8006 from the management network, under confirm-or-revert.
- **Effort:** L.
- **Risk:** High. Cluster-wide configuration lives on pmxcfs, and a wrong rule can cut off the web UI and cluster traffic on every node.
- **Screenshots:** Before: n/a. After: `docs/refactor/img/p-20-after.png` (placeholder).

---

## Design-system units: further proposals

<a id="p-21"></a>
### P-21: Pending-reboot wording and place in the HostBlock

- **Status:** proposed
- **Source:** `next/p1b-header`, which builds the HostBlock's pending-reboot slot (brief 1B puts the finding in HostBlock). Depends on [P-04](#p-04).
- **Problem:**
  - P-04 proposes the pending-reboot finding and places it "next to the uptime", as the plan's `next/p4-header` row does, but gives it no wording. Figma draws none.
  - Next to the uptime does not fit beside the label. Figma's uptime, "182 days, 2 hours, 26 minutes", is 29 cells from col 27; a space and "⛬ Reboot pending" (16 cells; "[*] Reboot pending", 18, in ASCII) make the line 73 cells (75), against the 64-cell content width. The flag fits only beside an uptime of at most 20 cells (18 in ASCII), so it would be cut on the long-uptime hosts P-04 is about. Stacked, below `StackBelow` (64 columns since `next/p1b-menu`), the line is 6 + 29 + 1 + 16 = 52 cells (54 in ASCII), which fits at 60 columns.
- **Proposal:**
  - The wording: the warning marker (`⛬`, `[*]` in ASCII) and "Reboot pending", both in `status.warn`, so the finding never rests on colour alone.
  - The place, for the maintainer to choose:
    - (a) its own line under the uptime, at the value column. Built in `next/p1b-header`: always fits, one line more when set.
    - (b) next to the uptime, as P-04 says: no extra line, but cut with the ellipsis whenever the uptime is over 20 cells (18 in ASCII) beside the label.
    - (c) next to the uptime when it fits, else under it: never cut, but its place changes with the uptime's length.
  - Shown only when the status model reports a pending reboot; the reason (a newer kernel installed, `/var/run/reboot-required`) is not shown.
  - "Reboot pending" follows the brief's own term, "pending reboot"; the component holds the wording, so changing it is one string and the goldens.
- **Effort:** S. (a) is built, so this approves its wording and place; (b) or (c) is a change to `HostBlock` and its goldens.
- **Risk:** Low: display only, and nothing sets it before P-04 is approved.
- **Screenshots:** Before: none (Figma `00-TUI-MAIN.png` has no such line). After: `docs/refactor/img/p-21-after.png` (placeholder); the `hostblock/reboot-pending` golden shows (a) meanwhile.

<a id="p-22"></a>
### P-22: Menu item markers: lockout risk and unavailable

- **Status:** proposed
- **Source:** `next/p1b-menu`, which builds brief 1B's Menu fields "enabled" and "risk". Related: [P-02](#p-02), [P-10](#p-10), [P-12](#p-12), [P-01](#p-01).
- **Problem:**
  - Figma's menus mark no item. Nothing on the main menu says that SSH Login, Firewall or Run All can lock the user out, though the confirm step treats those changes differently (a typed confirmation and confirm-or-revert: brief 1C and Phase 2.4, P-10), and no item is ever shown as unavailable.
  - Some items will be unavailable: brief §2 and Phase 2.5 keep root SSH key-only on Proxmox, which P-12 makes a read-only item, and P-01 marks controls read-only.
  - A state shown by colour alone is lost without colour, and at 16 colours the faint of a disabled item is the bright black of every description.
- **Proposal:**
  - The wording, built in `next/p1b-menu` (DEV-028): a note line under the item's description. A risky item: `⛬ Lockout risk` (`[*] Lockout risk` in ASCII) in `status.warn`, bold, as a plan's lockout-risk change. An unavailable item: `∅ Unavailable` (`[-] Unavailable`), the whole item in `text.faint`, and its key left out of the prompt's hint ("0-2, 4-10 or q"). "Unavailable", not "Disabled", which the same screens use for a setting's state ("Dry-run Mode Disabled").
  - The place, for the maintainer to choose:
    - (a) a line of its own under the description (built): never cuts the description, the same in both glyph sets; one line more per marked item.
    - (b) the glyph alone after the label, with one legend line under the menu (`⛬ Lockout risk`): one line in all; the glyph fits labels of up to 18 cells (16 in ASCII), but reads only with the legend.
    - (c) after the description when it fits, else under it: no extra line for descriptions of up to 22 cells (20 in ASCII), so "Configure UFW rules" keeps its line and "Execute hardening operations" does not; the place changes with the description's length.
  - Which items, on the main menu [I]: SSH Login (2) and Firewall (4), whose changes brief Phase 2.4 puts under confirm-or-revert, and Run All (7), which brief 1C and P-02 put under a typed confirmation; User Management (1) when it changes the sudo method (P-10). Nothing is marked until this is approved: the main-menu golden (`menu/main`) marks no item, as Figma.
  - Each word is one constant in `pkg/ui/component/menu.go`, so changing the wording is one string and the goldens.
- **Effort:** S. (a) is built, so this approves its words and place; (b) or (c) is a change to `Menu` and its goldens. Marking items is data in the screens (`next/p1d-slice`, Phase 4).
- **Risk:** Low: display only. Under (a), three marked items make the main menu three lines longer, against the compact menu's 24 lines (refactor plan §3.15).
- **Screenshots:** Before: Figma `00-TUI-MAIN.png`, `02-SSH_LOGIN.png` (reference only). After: `docs/refactor/img/p-22-after.png` (placeholder); the `menu/risk` and `menu/disabled` goldens show (a) meanwhile.

<a id="p-23"></a>
### P-23: Words for the message kinds

- **Status:** proposed
- **Source:** `next/p1b-blocks`, which builds brief 1B's `Message` (success, info, warn and error) and Figma's `Callout`. Related: [P-09](#p-09), [P-10](#p-10).
- **Problem:**
  - Brief 1B defines a message as "glyph, color and a word, never color alone". origin/main's ~244 one-line messages are a glyph and a sentence ("✓ DNS configured successfully") with no word for the kind, and Figma draws no one-line message, so neither gives the words.
  - Figma draws three callouts: "Warning", "Operation Successful" and "Operation Cancelled". Brief 1B's messages also report errors and information, which the canonical flow's result step (brief 1C step 5, P-10) and dry-run's stop at the plan will need. Figma draws no failure and no information callout.
  - A callout's kind shows in its bar's colour, which is lost without colour and to a colour-blind reader, so a callout needs its kind's glyph and word whatever its title, and a word of its own when it has no title.
- **Proposal:** that a message carries its kind's word is brief-decided (brief 1B), and a callout carries its kind's glyph and word by the same never-colour-alone rule; both are built (DEV-031, DEV-032). The maintainer decides the words.
  - Message words, before the text, bold in the kind's colour: "Success", "Info", "Warning", "Error" and "Cancelled", brief 1B's kind names and Figma's "Warning" and "Cancelled": `[!] Error: Failed to configure sudo: exit status 1`. Alternatives: "Done" or "OK"; "Note"; "Failed".
  - Callout titles for a callout given none: Figma's three for ok, warn and cancelled, and "Operation Failed" for bad and "Note" for info. Alternatives: "Error" or "Failed"; "Info" or "Information".
  - A callout given a title of the screen's own puts its kind's word before it, `[*] Warning: Changing the SSH port`, unless the title is that word or the kind's own title. The alternative is the title as given, its kind then told by the glyph alone.
  - Not built, and a departure from brief 1B, for the maintainer to choose knowingly: no word on one-line messages, as origin/main has them. It keeps the ~244 lines as they read today, many of whose sentences already say the kind ("Error: Failed to ..." repeats it), but the kind then rests on the glyph, and in ASCII `[-]` is also Not Applicable's marker and `[*]` Partial's.
  - Each word is one string in `MessageKind.String` (message words) or `MessageKind.title` (callout titles), in `pkg/ui/component/message.go`, so changing one is a string and the goldens.
- **Effort:** S. The words are built, so this approves them; dropping the word from messages, or from before a callout's own title, is a small change to `Message` or `Callout` and their goldens.
- **Risk:** Low: display only.
- **Screenshots:** Before: Figma `SSH_LOGIN-TOGGLE_CONFIRMATION.png`, `USER-CREATE_USER.png` (reference only). After: `docs/refactor/img/p-23-after.png` (placeholder); the `callout/bad`, `callout/info`, `callout/long` and `message/*` goldens show the built form meanwhile.

<a id="p-24"></a>
### P-24: Words for the apply steps and the confirm countdown

- **Status:** proposed
- **Source:** `next/p1b-plan`, which builds brief 1B's `StepProgress` and the confirm-or-revert countdown (brief §2 and Phase 2.4). Related: [P-03](#p-03), [P-10](#p-10), [P-22](#p-22), [P-23](#p-23).
- **Problem:**
  - Brief 1B names the step states (pending, running, done, failed, rolled back) but gives them no words, and the unit adds a sixth, skipped, for a step left out because it is already done or because an earlier one failed. Without colour a step's state shows only by its glyph and its word.
  - Brief §2 and Phase 2.4 decide the countdown: 180 seconds by default, confirmed by running `sudo hardn confirm` from a new SSH session, on the new port if it changed; otherwise the change reverts automatically. Neither the brief, Figma nor origin/main words the notice.
  - A countdown in its last seconds has to read as urgent without colour too.
- **Proposal:** built as below (DEV-039, DEV-041); the maintainer decides the words.
  - Step words, before the step's title in the state's colour: "Pending", "Running", "Done", "Failed", "Rolled back" and "Skipped", as in `[<] Rolled back: Set Port 2230`. Alternatives: "Waiting" for pending, "OK" for done, "Undone" or "Reverted" for rolled back, "Not run" for skipped.
  - The countdown, under brief §2's words:
    - `[*] Warning: This change reverts automatically in 2:45`, the word being a warning `Message`'s (P-23);
    - `To keep it, open a new SSH session and run:`, or `... a new SSH session on port 2230 and run:` when the change moved the port;
    - `sudo hardn confirm`, in bold.

    Under 30 seconds the first line turns to `[!] Urgent: ...` in red. Alternatives: "Unless confirmed, this change is undone in 2:45"; "Confirm within 2:45"; "Reverting soon" for "Urgent"; another threshold.
  - Not new, listed so the plan group's words are in one place: `PlanView`'s "Lockout risk" is the menu's (P-22); `ErrorReport`'s "Operation Failed" and "Info" are P-23's; the dry-run footer "No actual changes were made to your system." and "Check the log file at <path> for details" are origin/main's (pkg/menu run_all_menu.go:288, 295). P-03 may reword the footer if dry-run becomes a "Plan only" mode.
  - Each word is one string: `StepState.String` in `pkg/ui/component/steps.go`, and the `confirm*` constants and `UrgentBelow` in `pkg/ui/component/confirm.go`, so changing one is a string and the goldens.
- **Effort:** S. The words are built, so this approves them; another wording is strings and goldens.
- **Risk:** Low: display only. Nothing shows them before the canonical flow (`next/p1c-flow`) and confirm-or-revert (`next/p2-revert`) use the components.
- **Screenshots:** Before: none (no Figma frame or origin/main screen). After: `docs/refactor/img/p-24-after.png` (placeholder); the `stepprogress/*` and `confirmcountdown/*` goldens show the built form meanwhile.

<a id="p-25"></a>
### P-25: Navigation: free text, `0` off menus, the refusals' words, the typed phrase and explaining the inputs

- **Status:** proposed
- **Source:** `next/p1c-nav`, which builds brief 1C's navigation grammar in `pkg/ui/nav`. Related: [P-10](#p-10), [P-22](#p-22), [P-23](#p-23), [P-09](#p-09).
- **Problem:** brief 1C decides the grammar, "the same at every level": numbers select, `0` or `b` goes back, `q` quits, y/N questions, typed confirmations; and the plan's p1c-nav row asks for identical back and quit on the main menu, a sub-menu and a workflow step. `next/p1c-nav` builds that (DEV-044, DEV-045). The brief leaves open, or does not word:
  - a free-text question, such as a user name, where `0`, `b` and `q` are legal values: Linux allows the user names `b` and `q`, and origin/main takes any line as one (pkg/menu input.go `ReadInput`);
  - the words for a line a prompt refuses;
  - how a typed confirmation shows the phrase to type, which a narrow terminal must not hide;
  - how the navigation inputs are explained. Brief 1C has prompts say what `q` does "in those terms, e.g. 'q to quit'", but only the main menu's Exit row does (DEV-030); nothing says what `0` and `b` do (sub-menus show "0) Return" with no description, as Figma draws them), and the hint puts them in the brackets where `[y/N]` lists a question's answers, so `1. Username [0, b or q]:` can read as if the name must be `0`, `b` or `q`, and `Type the hostname [bos-san-l01, 0, b or q]:` lists the phrase among them.

  And one narrowing of the brief, which the first build made and this proposal offers instead as a choice: `0` going back on menus only.
- **Proposal:** built as below (DEV-044, DEV-045, README §9.8); the maintainer decides (a), (c), (d) and (e), and whether to depart from the brief as (b) describes.
  - **(a) Free text:** `0`, `b` and `q`, in any case, navigate at every prompt, free text included, so a value can never be `0`, `b` or `q`, and every free-text question's hint says so: `1. Username [0, b or q]:` where Figma draws `1. Username:`. Cost: hardn's forms cannot create a user named `b` or `q` (one can still be chosen from a numbered list); origin/main's free-text fields take no `0` as a value (their numbered prompts start at 1, and one already takes "0 to cancel", [interaction.md](phase0/interaction.md) §5, anti-pattern 7). Alternatives:
    - (a2) free text takes every line as its value, with no navigation there and no hint; leaving a field then takes Ctrl-C, which ends hardn. The grammar is no longer the same at every level, a departure from brief 1C.
    - (a3) an escape, such as a leading `\` that takes the rest as typed (`\q`): one more rule to learn, shown nowhere unless the hint grows.
  - **(b) `0` at every prompt:** built as brief 1C and the plan have it. `0` goes back from a y/N question, a typed confirmation and free text as from a menu, where it is also the Exit or Return item's number, and every hint shows it: `[y/N, 0, b or q]`, `[0, b or q]`. A `0` typed at a y/N question meant as "no" goes back instead, which applies nothing. Alternative, **a departure from brief 1C and the plan's p1c-nav row**, for the maintainer to choose knowingly: `0` goes back only on a menu, where it is shown as "0)"; elsewhere it is not an input (a y/N question refuses it, a typed confirmation compares it with the phrase, free text takes it as a value). Hints are shorter (`[y/N, b or q]`, `[b or q]`), but back is then not the same at every level, and the plan's "identical back/quit" holds for `b` only.
  - **(c) The refusals' words,** each written under the prompt as an error message (`✗ Error: ...`, P-23's word), then the prompt again:
    - a menu: origin/main's "Invalid option. Please try again." (pkg/menu main_menu.go:540);
    - a disabled item's key: "<Label> is unavailable.", such as "Root SSH Login is unavailable.";
    - a y/N question: "Invalid answer. Please try again.";
    - free text, which refuses one line only, one longer than the reader's limit (64 KiB): "Invalid answer. Please try again." too;
    - a typed confirmation: "That does not match exactly. Please try again." It does not repeat the phrase: the prompt under it shows the phrase whole ((d)), while the message's prose would wrap at a hyphen inside it (x/ansi `wrap.go:270-276`: a hyphen is always a breakpoint), so "bos-san-l01" would read "bos-" / "san-l01". The first build named it ("That does not match 'bos-san-l01'.").

    Alternatives: one sentence for every prompt ("Invalid input. Please try again."); naming the inputs ("Please answer y or n."); no "Please try again." where the prompt is asked again anyway.
  - **(d) The typed confirmation's layout:** the phrase is shown as the answer the hint offers, `Type the hostname [bos-san-l01, 0, b or q]:`, and always whole: the prompt cuts the label first, and hardn asks only when the whole hint shows after some of the label. A phrase too long for that is refused before anything is written, and the flow asks with a shorter phrase or label: with the label "Type the hostname", up to 45 cells from 64 columns up and 41 at 60 (43 and 39 in ASCII). Linux allows a hostname of 64 bytes, so a long one needs another phrase, such as its first label. The label is the flow's (P-10). Alternatives:
    - (d2) the phrase in the label, as origin/main's Run All asks "Type 'yes' to confirm you want to apply real changes:" (pkg/menu run_all_menu.go:162): a narrow terminal cuts the label first, phrase included.
    - (d3) when the hint does not fit, the phrase on a line of its own above the prompt, at column 6: room for 58 cells at 64 columns, but a second layout for one prompt.
  - **(e) Explaining the navigation inputs:** built, nothing explains `0` and `b`, and the hint lists them with a prompt's answers. Alternatives:
    - (e2) a help line generated from the bindings (`keys.Set.Help`, which writes the Exit row's "q to quit"; nav uses it nowhere else): "0 or b to go back, q to quit", under the first question of a form and under each menu's prompt, or as the description of a sub-menu's "0) Return" row, as the main menu's Exit row says "q to quit". One line more per screen, or none on the Return row.
    - (e3) a hint that sets the navigation apart from the answers: `Create user 'george'? [y/N] (0 or b back, q quit):`, `1. Username (0 or b back, q quit):`. Longer prompts, cut sooner on a narrow terminal.
  - Each word is one constant in `pkg/ui/nav/nav.go`, so changing one is a string and the goldens.
- **Effort:** S. (a) to (e) are built, so this approves them; (a2), (b)'s alternative, (d2), (d3), (e2) or (e3) are each a small change to `pkg/ui/nav` (and `keys` for (b)) and its goldens, and (a3) one more to `nav.Text`.
- **Risk:** Low. (a) changes what a free-text field accepts compared with origin/main, for three one-character values; (b)'s alternative departs from the brief; nothing else is applied differently.
- **Screenshots:** Before: Figma `00-TUI-MAIN.png`, `01-USER.png`, `USER-CREATE_USER.png` (reference only). After: `docs/refactor/img/p-25-after.png` (placeholder); the `nav/*` goldens show the built form meanwhile.

<a id="p-26"></a>
### P-26: Forms: back through the fields, the earlier answer and a free-text default

- **Status:** proposed
- **Source:** `next/p1c-form`, which builds brief 1C's forms in `pkg/ui/form`. Related: [P-25](#p-25), [P-10](#p-10), [P-11](#p-11).
- **Problem:**
  - Brief 1C's grammar has `0` or `b` go back, the same at every level, but does not say what back means inside a form, where each field is a prompt and the whole form is one step of the flow. origin/main's forms have no back at all: an invalid user name returns to the User Management menu, and an invalid port discards all four fields of a firewall profile ([interaction.md](phase0/interaction.md) §5, anti-pattern 4).
  - When back returns to a field, its earlier answer is on screen above it; nothing decides what Enter does there.
  - A free-text field's default (brief 1C's fields have a `Default`) cannot show in the hint: the brackets list the inputs a prompt takes, and a y/N question marks its default with a capital, which a value cannot take without changing it. origin/main words its few text defaults in the label: "Enter new username (leave empty to keep current)" (pkg/menu user_options.go:580), "Enter filename to export system status (default: system_status.txt)" (system_details_menu.go:134).
- **Proposal:** built as below (DEV-046, DEV-047, [README](../design-system/README.md) §9.9); the maintainer decides (a), (b) and (c).
  - **(a) Back:** `0` or `b` goes back to the field asked before, one field at a time, passing over a skipped one, and from the first field leaves the form for the menu. Alternative (a2): `0` or `b` leaves the form from any field, in one line however far the user got; correcting an earlier answer then means leaving and starting again.
  - **(b) The earlier answer:** a field asked again starts afresh. Enter takes its `Default`, as the first time, not the earlier answer, which stays on screen above. Alternative (b2): Enter keeps the earlier answer; the prompt would then have to show it, as (c) asks for a default, or the user cannot tell what Enter does.
  - **(c) A free-text default:** nothing shows it before the answer unless the screen's own words say so, as origin/main's labels do; after an empty line the form echoes the value applied, as in `1. Backup path: /var/backups/hardn`. A field's title is one line: beside the hint it is cut first, so a question that would be cut there is asked under its head, its title on a line of its own, cut only when even that is too narrow (DEV-046). So origin/main's "Enter new username (leave empty to keep current)" shows whole, under its head, from 54 columns in a numbered form; "Enter filename to export system status (default: system_status.txt)", 67 cells, is cut at every width under the 64-cell content cap, at the words that state the default. Words that must be read whole go in the description, which wraps; how the screens split such a label is theirs, in their words. Alternatives: (c2) the form puts it in the hint in fixed words, such as `[Enter for /var/backups/hardn, 0, b or q]`; (c3) a fixed description line, such as "Enter keeps /var/backups/hardn.".
  - The form adds no words of its own: its refusals are `pkg/ui/nav`'s ([P-25](#p-25) (c)), and its validation errors are the screens', such as origin/main's username rule.
- **Effort:** S. (a), (b) and (c) are built, so this approves them; (a2), (b2), (c2) or (c3) is a small change to `pkg/ui/form` and its goldens.
- **Risk:** Low. Under (a) a user who types `b` to leave a long form steps back through it, field by field; `q` still quits. Nothing is applied before a form ends, whatever is chosen.
- **Screenshots:** Before: Figma `USER-CREATE_USER.png` (reference only). After: `docs/refactor/img/p-26-after.png` (placeholder); the `form/back` and `form/yes-no` goldens show the built form meanwhile.
