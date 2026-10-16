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
| [P-23](#p-23) | Words for the message kinds Figma does not draw | `next/p1b-blocks` | Message, Callout; Phase 1C | S | Low | proposed |

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
  - Next to the uptime does not fit beside the label. Figma's uptime, "182 days, 2 hours, 26 minutes", is 29 cells from col 27; a space and "⛬ Reboot pending" (16 cells; "[*] Reboot pending", 18, in ASCII) make the line 73 cells (75), against the 64-cell content width. The flag fits only beside an uptime of at most 20 cells (18 in ASCII), so it would be cut on the long-uptime hosts P-04 is about. Stacked, below 70 columns, the line is 6 + 29 + 1 + 16 = 52 cells (54 in ASCII), which fits at 60 columns.
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
### P-23: Words for the message kinds Figma does not draw

- **Status:** proposed
- **Source:** `next/p1b-blocks`, which builds brief 1B's `Message` (success, info, warn and error) and Figma's `Callout`. Related: [P-09](#p-09), [P-10](#p-10).
- **Problem:**
  - Figma draws three callouts: "Warning", "Operation Successful" and "Operation Cancelled". Brief 1B's messages also report errors and information, which the canonical flow's result step (brief 1C step 5, P-10) and dry-run's stop at the plan will need. Figma draws no failure and no information callout.
  - A callout's kind shows in its bar's colour, which is lost without colour, so its title must say the kind; a callout given no title needs a word of its own.
  - origin/main's one-line messages are a glyph and a sentence ("✓ DNS configured successfully"), with no word for the kind.
- **Proposal:**
  - The callout words, built in `next/p1b-blocks` (DEV-032): a callout with no title shows its kind's word, Figma's three for ok, warn and cancelled, and "Operation Failed" for bad and "Note" for info. Alternatives: "Error" or "Failed"; "Info" or "Information".
  - One-line messages keep origin/main's form, the kind's glyph and the sentence, with no kind word (built, DEV-031). The alternative, a bold kind word before the text ("Failed: ..."), as the theme's first plan had it, repeats what the glyph says and changes the wording of origin/main's ~244 message lines.
  - Each word is one string in `MessageKind.String` (`pkg/ui/component/message.go`), so changing one is a string and the goldens.
- **Effort:** S. The words are built, so this approves them; a kind word on messages is a change to `Message` and its goldens.
- **Risk:** Low: display only.
- **Screenshots:** Before: Figma `SSH_LOGIN-TOGGLE_CONFIRMATION.png`, `USER-CREATE_USER.png` (reference only). After: `docs/refactor/img/p-23-after.png` (placeholder); the `callout/bad`, `callout/info` and `message/*` goldens show the built form meanwhile.
