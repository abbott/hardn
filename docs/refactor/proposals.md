# Enhancement proposals

> **What this is:** the enhancement log from brief §2 (Design fidelity). An enhancement improves on the design: a new state, clearer wording, an extra component or a better interaction. **The maintainer decides each one.** Nothing here is implemented until its status is `approved`.
>
> **Not here:** deviations, where the code differs from Figma because of terminal limits or export drift. The agent decides those and records them in [../design-system/deviations.md](../design-system/deviations.md). If a change alters meaning, flow or wording, it belongs here; small, obvious fixes are deviations.
>
> **Seeded:** 2026-10-16, from brief §7 (P-01 to P-07, the Figma main-menu review items) and from the refactor plan §5 (P-08 to P-20). Evidence: [phase0-findings.md](phase0-findings.md), [phase0/interaction.md](phase0/interaction.md), [phase0/side-effects.md](phase0/side-effects.md), [phase0/inventory.md](phase0/inventory.md) and [phase0/v032-reference.md](phase0/v032-reference.md).

## How to read an entry

- **Status:** `proposed` → `approved` / `rejected` / `deferred` / `decided` (the maintainer chose among the alternatives, or set their own terms) → `implemented` (with the branch that implements it).
- **Effort** [I], an estimate:
  - **S** fits in one small change unit (≲200 lines);
  - **M** is one full unit (≲500 lines, excluding goldens);
  - **L** spans several units.
- **Risk** [I]: what could go wrong for users if it ships, and why.
- **Screenshots:**
  - **Before** is a `freeze` capture of the current origin/main screen, or the name of a Figma frame. Figma PNGs are a reference that is never committed.
  - **After** is a capture of the gallery mock.
  - **Deferred until the maintainer approves freeze.** `charmbracelet/freeze` is not in hardn's module graph: v0.2.2 would be downloaded and run (pinned, with `go run`), and refactor plan Phase 1 asks the maintainer once before its first use. Until then every path marked "placeholder" is only a name, and the review uses the goldens and the gallery's output: an entry names its goldens where they show the built form "meanwhile"; the [design-system README](../design-system/README.md#phase-1-review-guide)'s review guide covers the rest.
  - **Once approved,** the captures take the paths their entries name, under `docs/refactor/img/`, which the allow-list `.gitignore` re-includes with `!docs/**/*.png`; the gallery's own PNGs are named after its mocks (design-system README §9.16).
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
| [P-08](#p-08) | Sub-screen header: Header + Breadcrumb, or Figma's TitleBar | plan §5.1 | Layouts; Phase 1 | S | Low | decided (2026-10-17); implemented, `next/p1d-divider` |
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
| [P-25](#p-25) | Navigation: free text, `0` off menus, the refusals' words, the typed phrase and explaining the inputs | `next/p1c-nav` | Every prompt; Phase 1C | S | Low | proposed; back `0` only decided (2026-10-17), implemented, `next/p1d-divider` |
| [P-26](#p-26) | Forms: back through the fields, the earlier answer and a free-text default | `next/p1c-form` | Every form; Phase 1C | S | Low | proposed |
| [P-27](#p-27) | The canonical flow: back at the confirmation, an empty plan, the plan's heading, the steps' lines, a long hostname and a narrow terminal | `next/p1c-flow` | Every action; Phase 1C | S | Low | proposed |
| [P-28](#p-28) | A layout for a screen that only shows: SSH_LOGIN-SSH_SETTINGS_DISPLAY | `next/p1d-slice` | Layouts; SSH Login; slice 6 | M | Low | proposed |
| [P-29](#p-29) | Figma's words or origin/main's: the menu prompt and the User Management sub-menu | `next/p1d-slice` | Every menu; Phase 5 | S | Low | proposed |
| [P-30](#p-30) | A notice line in the sub-menu layout: origin/main's Firewall screen and an empty DNS list | `next/p1d-gallery-screens` | Layouts; Firewall, DNS; slices 2, 4 | S | Low | proposed |
| [P-31](#p-31) | Figma's words or origin/main's on the other Figma frames | `next/p1d-gallery-screens` | SSH Login, Backup, User Management; slices 3, 5, 6 | S | Low | proposed |
| [P-32](#p-32) | Run the legacy commands and file calls through `host.Real` | `next/p2-system` | Every legacy command; Phase 2 | S | Medium | proposed |
| [P-33](#p-33) | UFW rules hardn adds on its own: while ufw is inactive, for a moved port, and around a blocklist | `next/p2-ufw` | Firewall; slice 4 | S | Low | proposed |
| [P-34](#p-34) | A reboot inside the confirm window | `next/p2-revert` | Confirm-or-revert; Phase 3 | S | Low | proposed |
| [P-35](#p-35) | Giving up a rollback that cannot finish | `next/p2-revert` | Confirm-or-revert; Phase 3 | S | Low | proposed |
| [P-36](#p-36) | An `AllowUsers` without root on Proxmox VE: add root, or refuse | `next/p2-pve` | Proxmox VE; slice 6 | S | Low | proposed |
| [P-37](#p-37) | DNS on hosts whose resolver hardn reports but does not configure | `next/p2-dns` | DNS; slice 2 | none to M | Low to Medium | proposed |
| [P-38](#p-38) | An `sshd_config` whose Include is not the stock first line | `next/p2-vmtest` (from `next/p2-sshd-validate`) | SSH; slice 6 | none to M | Low to Medium | proposed |

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)):
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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: `docs/refactor/img/p-02-before.png` (placeholder). After: `docs/refactor/img/p-02-after.png` (placeholder).

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: `docs/refactor/img/p-03-before.png` (placeholder). After: `docs/refactor/img/p-03-after.png` (placeholder).

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: `docs/refactor/img/p-04-before.png` (placeholder). After: `docs/refactor/img/p-04-after.png` (placeholder).

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: `docs/refactor/img/p-05-before.png` (placeholder). After: `docs/refactor/img/p-05-after.png` (placeholder).

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: `docs/refactor/img/p-06-before.png` (placeholder). After: `docs/refactor/img/p-06-after.png` (placeholder).

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: `docs/refactor/img/p-07-before.png` (placeholder). After: `docs/refactor/img/p-07-after.png` (placeholder).

---

## Refactor plan §5: further proposals

<a id="p-08"></a>
### P-08: Sub-screen header: Header + Breadcrumb, or Figma's TitleBar

- **Status:** decided (maintainer, 2026-10-17); implemented in `next/p1d-divider`.
- **Source:** plan §5.1.
- **Problem:** the brief's sub-menu layout uses `Header` + `Breadcrumb`. Every Figma sub-screen (`01-USER`, `02-SSH_LOGIN`, `05-BACKUP`, the `USER-*` and `SSH_LOGIN-*` frames) uses a single TitleBar, with no header and no breadcrumb.
- **Proposal:**
  - (a) Keep the brief's Header + Breadcrumb. This costs about 2 lines on an 80×24 console.
  - (b) Follow Figma's TitleBar and put the breadcrumb text inside it, e.g. "hardn › User Management" (as proposed; the decision drops the "hardn" segment).
- **Decision** (the maintainer, 2026-10-17): merge the two patterns, (b) without its product segment. Every sub-screen, sub-menu and workflow step alike, opens with Figma's single title bar wrapped by `bar.bg`, with no `Header` and no `Breadcrumb` line, as the screen exports draw it. The trail lives inside the bar and names only the section and any sub-section, as "User Management › Manage User": no "hardn" segment, which is redundant. The trail is off by default (`screen.TitleTrail = false`), so a sub-screen's bar shows its own title, as "Manage User" on USER-MANAGE_USER-MANAGEMENT; the layouts keep each screen's parents, so flipping the constant is the only change that shows them. The main menu is unchanged: `Header` and the `ContextBar` "Debian 12 (Bookworm)", as 00-TUI-MAIN draws it. This supersedes the brief's sub-screen breadcrumb (§1B's sub-menu and workflow-step layouts, §1C's breadcrumb above sub-screens). The `Breadcrumb` component stays, its states without "hardn", and no screen draws it on a line of its own; its collapse is the title bar's (DEV-018, DEV-037, DEV-053, DEV-054).
- **Effort:** S.
- **Risk:** Low. The main cost is the height budget on small consoles; the decision saves the two lines: 01-USER takes 21 lines from 64 columns, as Figma's frame does.
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)):
  - Before: Figma `01-USER.png` (reference only).
  - After: `docs/refactor/img/p-08-after.png` (placeholder); the `sub-menu/*` and `workflow-step/*` goldens show the built form meanwhile, and the `titlebar/trail*` component goldens the trail.

<a id="p-09"></a>
### P-09: Replace "Press any key to continue"

- **Status:** proposed
- **Source:** plan §5.2.
- **Problem:**
  - origin/main has 32 such strings, 55 `ReadKey` call sites and 23 `PressAnyKey` calls [V]. Figma shows the prompt in 4 frames.
  - `ReadKey` relies on GNU-only `stty -F` in cbreak mode, against the brief's Enter-based input decision. *(Resolved by `next/p1c-legacy-input`, DEV-050: no `stty` call remains and every legacy pause now reads "Press Enter ...". What is left for this proposal is which pauses stay.)*
- **Proposal:**
  - "Enter to continue" only where output must be read before the screen changes.
  - Otherwise, return to the menu with the result left on screen (canonical flow, step 6).
- **Effort:** M (legacy input is routed through the single line reader first).
- **Risk:** Low.
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)):
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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)):
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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)):
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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)):
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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: `docs/refactor/img/p-13-before.png` (placeholder). After: `docs/refactor/img/p-13-after.png` (placeholder).

<a id="p-14"></a>
### P-14: Reachable risk bands, with factors shown

- **Status:** proposed
- **Source:** plan §5.7. Depends on [P-06](#p-06).
- **Problem:** the top band, "Minimal / hardened security", is unreachable: the maximum score is 8, and the band needs more than 8 [V].
- **Proposal:** set band thresholds that a fully hardened host actually reaches, and show the factors that kept a host out of the top band.
- **Effort:** S (once P-06 exists).
- **Risk:** Low.
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: `docs/refactor/img/p-14-before.png` (placeholder). After: `docs/refactor/img/p-14-after.png` (placeholder).

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: `docs/refactor/img/p-15-before.png` (placeholder). After: `docs/refactor/img/p-15-after.png` (placeholder).

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)):
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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: `docs/refactor/img/p-17-before.png` (placeholder). After: `docs/refactor/img/p-17-after.png` (placeholder).

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: `docs/refactor/img/p-18-before.png` (placeholder). After: `docs/refactor/img/p-18-after.png` (placeholder).

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)):
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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: n/a. After: `docs/refactor/img/p-20-after.png` (placeholder).

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: none (Figma `00-TUI-MAIN.png` has no such line). After: `docs/refactor/img/p-21-after.png` (placeholder); the `hostblock/reboot-pending` golden shows (a) meanwhile.

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: Figma `00-TUI-MAIN.png`, `02-SSH_LOGIN.png` (reference only). After: `docs/refactor/img/p-22-after.png` (placeholder); the `menu/risk` and `menu/disabled` goldens show (a) meanwhile.

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: Figma `SSH_LOGIN-TOGGLE_CONFIRMATION.png`, `USER-CREATE_USER.png` (reference only). After: `docs/refactor/img/p-23-after.png` (placeholder); the `callout/bad`, `callout/info`, `callout/long` and `message/*` goldens show the built form meanwhile.

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
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: none (no Figma frame or origin/main screen). After: `docs/refactor/img/p-24-after.png` (placeholder); the `stepprogress/*` and `confirmcountdown/*` goldens show the built form meanwhile.

<a id="p-25"></a>
### P-25: Navigation: free text, `0` off menus, the refusals' words, the typed phrase and explaining the inputs

- **Status:** proposed (each part is decided separately). Back is `0` only, `b` dropped: decided (maintainer, 2026-10-17, over brief §1C), implemented in `next/p1d-divider` (**Decision** below).
- **Source:** `next/p1c-nav`, which builds brief 1C's navigation grammar in `pkg/ui/nav`. Related: [P-10](#p-10), [P-22](#p-22), [P-23](#p-23), [P-09](#p-09).
- **Problem:** brief 1C decides the grammar, "the same at every level": numbers select, `0` goes back (the brief adds `b`, which the maintainer dropped, **Decision** below), `q` quits, y/N questions, typed confirmations; and the plan's p1c-nav row asks for identical back and quit on the main menu, a sub-menu and a workflow step. `next/p1c-nav` builds that (DEV-044, DEV-045). The brief leaves open, or does not word:
  - a free-text question, such as a user name, where `0` and `q` are legal values: Linux allows the user name `q`, and origin/main takes any line as one (pkg/menu input.go `ReadInput`);
  - the words for a line a prompt refuses;
  - how a typed confirmation shows the phrase to type, which a narrow terminal must not hide;
  - how the navigation inputs are explained. Brief 1C has prompts say what `q` does "in those terms, e.g. 'q to quit'", but only the main menu's Exit row does (DEV-030); nothing says what `0` does (sub-menus show "0) Return" with no description, as Figma draws them), and the hint puts the navigation in the brackets where `[y/N]` lists a question's answers, so `1. Username [0 or q]:` can read as if the name must be `0` or `q`, and `Type the hostname [bos-san-l01, 0 or q]:` lists the phrase among them.

  And one narrowing of the brief, which the first build made and this proposal offers instead as a choice: `0` going back on menus only.
- **Proposal:** built as below (DEV-044, DEV-045, README §9.8); the maintainer decides (a), (c), (d) and (e), and whether to depart from the brief as (b) describes.
  - **(a) Free text:** `0` and `q`, in any case, navigate at every prompt, free text included, so a value can never be `0` or `q`, and every free-text question's hint says so: `1. Username [0 or q]:` where Figma draws `1. Username:`. Since the decision below, free text reserves only `0` and `q`: `b` is a value as any other line, so a user named `b` is possible. Cost: hardn's forms cannot create a user named `q` (one can still be chosen from a numbered list); origin/main's free-text fields take no `0` as a value (their numbered prompts start at 1, and one already takes "0 to cancel", [interaction.md](phase0/interaction.md) §5, anti-pattern 7). Alternatives:
    - (a2) free text takes every line as its value, with no navigation there and no hint; leaving a field then takes Ctrl-C, which ends hardn. The grammar is no longer the same at every level, a departure from brief 1C.
    - (a3) an escape, such as a leading `\` that takes the rest as typed (`\q`): one more rule to learn, shown nowhere unless the hint grows.
  - **(b) `0` at every prompt:** built as brief 1C and the plan have it. `0` goes back from a y/N question, a typed confirmation and free text as from a menu, where it is also the Exit or Return item's number, and every hint shows it: `[y/N, 0 or q]`, `[0 or q]`. A `0` typed at a y/N question meant as "no" goes back instead, which applies nothing. Alternative, **a departure from brief 1C and the plan's p1c-nav row**, for the maintainer to choose knowingly: `0` goes back only on a menu, where it is shown as "0)"; elsewhere it is not an input (a y/N question refuses it, a typed confirmation compares it with the phrase, free text takes it as a value). Hints are shorter (`[y/N or q]`, `[q]`), but back is then not the same at every level: with `b` dropped (**Decision** below), a y/N question, a typed confirmation and free text would have no back at all, only `q`, and the plan's "identical back/quit" would hold for `q` alone.
  - **(c) The refusals' words,** each written under the prompt as an error message (`✗ Error: ...`, P-23's word), then the prompt again:
    - a menu: origin/main's "Invalid option. Please try again." (pkg/menu main_menu.go:540);
    - a disabled item's key: "<Label> is unavailable.", such as "Root SSH Login is unavailable.";
    - a y/N question: "Invalid answer. Please try again.";
    - free text, which refuses one line only, one longer than the reader's limit (64 KiB): "Invalid answer. Please try again." too;
    - a typed confirmation: "That does not match exactly. Please try again." It does not repeat the phrase: the prompt under it shows the phrase whole ((d)), while the message's prose would wrap at a hyphen inside it (x/ansi `wrap.go:270-276`: a hyphen is always a breakpoint), so "bos-san-l01" would read "bos-" / "san-l01". The first build named it ("That does not match 'bos-san-l01'.").

    Alternatives: one sentence for every prompt ("Invalid input. Please try again."); naming the inputs ("Please answer y or n."); no "Please try again." where the prompt is asked again anyway.
  - **(d) The typed confirmation's layout:** the phrase is shown as the answer the hint offers, `Type the hostname [bos-san-l01, 0 or q]:`, and always whole: the prompt cuts the label first, and hardn asks only when the whole hint shows after some of the label. A phrase too long for that is refused before anything is written, and the flow asks with a shorter phrase or label: with the label "Type the hostname", up to 48 cells from 64 columns up and 44 at 60 (46 and 42 in ASCII), 3 more than before the decision below shortened the hint. Linux allows a hostname of 64 bytes, so a long one needs another phrase, such as its first label. The label is the flow's (P-10). Alternatives:
    - (d2) the phrase in the label, as origin/main's Run All asks "Type 'yes' to confirm you want to apply real changes:" (pkg/menu run_all_menu.go:162): a narrow terminal cuts the label first, phrase included.
    - (d3) when the hint does not fit, the phrase on a line of its own above the prompt, at column 6: room for 58 cells at 64 columns, but a second layout for one prompt.
  - **(e) Explaining the navigation inputs:** built, nothing explains `0`, and the hint lists `0` and `q` with a prompt's answers. Alternatives:
    - (e2) a help line generated from the bindings (`keys.Set.Help`, which writes the Exit row's "q to quit"; nav uses it nowhere else): "0 to go back, q to quit", under the first question of a form and under each menu's prompt, or as the description of a sub-menu's "0) Return" row, as the main menu's Exit row says "q to quit". One line more per screen, or none on the Return row.
    - (e3) a hint that sets the navigation apart from the answers: `Create user 'george'? [y/N] (0 back, q quit):`, `1. Username (0 back, q quit):`. Longer prompts, cut sooner on a narrow terminal.
  - Each word is one constant in `pkg/ui/nav/nav.go`, so changing one is a string and the goldens.
- **Decision** (the maintainer, 2026-10-17, over brief §1C): drop `b`. Back is `0` only, everywhere, as origin/main, v0.3.2 and Figma have it: neither origin/main nor v0.3.2 ever read `b`, and every Figma hint is "[0-N or q]"; only brief §1C ("`0` or `b` goes back") brought it in. `keys.Back` binds `0` alone, its help "0 to go back". `b` is now an ordinary line everywhere: a menu and a y/N question refuse it as any input they do not take, a typed confirmation compares it with the phrase, and free text takes it as a value; `nav.CheckPhrase` reserves `0` and `q`, in any case, and takes `b` as a phrase. Every hint loses it: "[0-10 or q]" on the main menu and "[0-2 or q]" on a sub-menu, Figma's own; "[y/N, 0 or q]", "[bos-san-l01, 0 or q]" and "[0 or q]" (DEV-044, DEV-045). For the parts above: (a) free text now reserves only `0` and `q`, so a user named `b` is possible; (b)'s alternative would now leave no back off menus; (d)'s phrase can be 3 cells longer; (e)'s help line and hint lose `b`. (a) to (e) are otherwise still the maintainer's.
- **Effort:** S. (a) to (e) are built, so this approves them; (a2), (b)'s alternative, (d2), (d3), (e2) or (e3) are each a small change to `pkg/ui/nav` (and `keys` for (b)) and its goldens, and (a3) one more to `nav.Text`.
- **Risk:** Low. (a) changes what a free-text field accepts compared with origin/main, for two one-character values, `0` and `q`; (b)'s alternative departs from the brief; nothing else is applied differently.
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: Figma `00-TUI-MAIN.png`, `01-USER.png`, `USER-CREATE_USER.png` (reference only). After: `docs/refactor/img/p-25-after.png` (placeholder); the `nav/*` goldens show the built form meanwhile.

<a id="p-26"></a>
### P-26: Forms: back through the fields, the earlier answer and a free-text default

- **Status:** proposed
- **Source:** `next/p1c-form`, which builds brief 1C's forms in `pkg/ui/form`. Related: [P-25](#p-25), [P-10](#p-10), [P-11](#p-11).
- **Problem:**
  - Brief 1C's grammar has `0` go back, the same at every level (the brief adds `b`, which the maintainer dropped on 2026-10-17, [P-25](#p-25)), but does not say what back means inside a form, where each field is a prompt and the whole form is one step of the flow. origin/main's forms have no back at all: an invalid user name returns to the User Management menu, and an invalid port discards all four fields of a firewall profile ([interaction.md](phase0/interaction.md) §5, anti-pattern 4).
  - When back returns to a field, its earlier answer is on screen above it; nothing decides what Enter does there.
  - A free-text field's default (brief 1C's fields have a `Default`) cannot show in the hint: the brackets list the inputs a prompt takes, and a y/N question marks its default with a capital, which a value cannot take without changing it. origin/main words its few text defaults in the label: "Enter new username (leave empty to keep current)" (pkg/menu user_options.go:580), "Enter filename to export system status (default: system_status.txt)" (system_details_menu.go:134).
- **Proposal:** built as below (DEV-046, DEV-047, [README](../design-system/README.md) §9.9); the maintainer decides (a), (b) and (c).
  - **(a) Back:** `0` goes back to the field asked before, one field at a time, passing over a skipped one, and from the first field leaves the form for the menu. Alternative (a2): `0` leaves the form from any field, in one line however far the user got; correcting an earlier answer then means leaving and starting again.
  - **(b) The earlier answer:** a field asked again starts afresh. Enter takes its `Default`, as the first time, not the earlier answer, which stays on screen above. Alternative (b2): Enter keeps the earlier answer; the prompt would then have to show it, as (c) asks for a default, or the user cannot tell what Enter does.
  - **(c) A free-text default:** nothing shows it before the answer unless the screen's own words say so, as origin/main's labels do; after an empty line the form echoes the value applied, as in `1. Backup path: /var/backups/hardn`. A field's title is one line: beside the hint it is cut first, so a question that would be cut there is asked under its head, its title on a line of its own, cut only when even that is too narrow (DEV-046). So origin/main's "Enter new username (leave empty to keep current)" shows whole from 54 columns in a numbered form: under its head below 64 columns, and from 64 on its one line, which it and `[0 or q]:` fill exactly; "Enter filename to export system status (default: system_status.txt)", 67 cells, is cut at every width under the 64-cell content cap, at the words that state the default. Words that must be read whole go in the description, which wraps; how the screens split such a label is theirs, in their words. Alternatives: (c2) the form puts it in the hint in fixed words, such as `[Enter for /var/backups/hardn, 0 or q]`; (c3) a fixed description line, such as "Enter keeps /var/backups/hardn.".
  - The form adds no words of its own: its refusals are `pkg/ui/nav`'s ([P-25](#p-25) (c)), and its validation errors are the screens', such as origin/main's username rule.
- **Effort:** S. (a), (b) and (c) are built, so this approves them; (a2), (b2), (c2) or (c3) is a small change to `pkg/ui/form` and its goldens.
- **Risk:** Low. Under (a) a user who types `0` to leave a long form steps back through it, field by field; `q` still quits. Nothing is applied before a form ends, whatever is chosen.
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: Figma `USER-CREATE_USER.png` (reference only). After: `docs/refactor/img/p-26-after.png` (placeholder); the `form/back` and `form/yes-no` goldens show the built form meanwhile.

<a id="p-27"></a>
### P-27: The canonical flow: back at the confirmation, an empty plan, the plan's heading, the steps' lines, a long hostname and a narrow terminal

- **Status:** proposed
- **Source:** `next/p1c-flow`, which builds brief 1C's canonical action flow in `pkg/ui/flow`. Related: [P-09](#p-09), [P-10](#p-10), [P-23](#p-23), [P-24](#p-24), [P-25](#p-25), [P-26](#p-26).
- **Problem:** brief 1C decides the steps: collect with a form, review the answers, show the plan (dry-run stops there), confirm with y/N or, for lockout risk and Run All, a typed confirmation, apply with rollback, and return to the menu without wiping the result. It leaves open:
  - what `0` does at the confirmation, where the grammar has it go back one level ([P-25](#p-25) (b)), and where a typed confirmation has no "no";
  - what a plan with no changes says, and whether the flow then asks anything;
  - how the plan reads apart from the review above it: both are rows from column 2 under the review's section header, and Figma draws no plan;
  - what the steps show where nothing animates, since "rolled back" is a second state of a step already shown done; and, where they animate, what a line the user types while a step runs does to the steps drawn in place;
  - what a typed confirmation asks when the hostname is too long to show whole beside its label, which `pkg/ui/nav` refuses to ask ([P-25](#p-25) (d)) and left to the flow, and what the flow does when the terminal is too narrow for any phrase.
- **Proposal:** built as below (DEV-048, DEV-049, [README](../design-system/README.md) §9.10); the maintainer decides (a) to (e).
  - **(a) Back at the confirmation** declines, as `n` does: Figma's "Operation Cancelled" callout, nothing applied, then the menu. At a typed confirmation it is the only way to decline short of `q`. Alternatives: (a2) back returns to the form, asked again from its first field, the review and plan following it again (an action with no form would only draw them again); (a3) back returns to the menu with no callout, as back from the form's first field does.
  - **(b) An empty plan** says "Nothing needs changing.", brief Phase 3's words for a plan that finds nothing to change, as an info message, and the flow asks nothing more. Alternatives: Figma's "Operation Successful" callout with that sentence as its body; words that name the state, such as "Already configured", as origin/main's "already DISABLED" for root SSH login.
  - **(c) The plan's heading:** none; the plan follows the review after a blank line, and no words are added. Alternative: a section header of its own, such as "Planned Changes", so that the plan never reads as more rows of the review.
  - **(d) The steps without animation** (a pipe, plain mode, `TERM=dumb`): each step's line is written once, when it is done, failed or skipped, and a rollback shows only in the error report after them, which names each change undone. Alternative: a line for each rollback too, such as "Rolled back: Create user george", which the report then repeats.
    - **With animation**, the terminal stays in cooked mode with its echo on ([README](../design-system/README.md) §9.7), so an Enter typed while a step runs moves the cursor down a line, and each redraw after it lands a line lower: the block's first line keeps a stale frame, such as "Running: Create user george", above the steps, which are redrawn whole, and the result follows (DEV-049). Built: accepted as a terminal limit; what was typed is discarded before the next prompt, so it answers nothing. Alternatives: (d2) turn the terminal's echo off (termios `ECHO` only, not raw mode) for the length of Apply and restore it before the result, at the cost that a crash or a Ctrl-C during Apply leaves the echo off, which the line reader's cooked-mode design avoids; (d3) stop drawing in place once a typed line is waiting to be read, and write each later step's line under the block, which leaves the misdrawn block above them; (d4) read and count the lines typed during Apply to move up by as many more, a read during Apply that the line reader's design rejects.
  - **(e) A long hostname** (over 48 cells beside "Type the hostname" from 64 columns, 44 at 60, in Unicode glyphs; Linux allows 64) is asked by a short phrase the action gives, `Confirm.Short`, such as its first label, which the hint then shows: `bos-san-l01` for `bos-san-l01.<...>.example.net`. The flow never cuts a phrase itself, so an address such as 192.0.2.123 is never confirmed by typing 192. **A narrow terminal:** when neither phrase shows (for "Type the hostname" and `bos-san-l01`, below 27 columns in Unicode glyphs and 29 in ASCII and plain mode), nothing is asked and nothing written for the prompt: Figma's "Operation Cancelled" callout with the new sentence "The terminal is too narrow to confirm. Widen it and try again.", nothing applied, and `Cancelled` with no error, so the menu carries on. Alternatives: (e2) never shorten the phrase: the flow ends so too, and the action must word a shorter one; (e3) the whole name on a line of its own above the prompt ([P-25](#p-25) (d3)); (e4) the narrow ending as a failure, an `ErrorReport` and `Failed`, for an exit code apart from a decline's; (e5) other words under the callout, or none.
  - Not new here, listed so the flow's words are in one place: the result callouts are Figma's ("Operation Successful", "Operation Cancelled") and the failure's are [P-23](#p-23)'s; the step words are [P-24](#p-24)'s; the dry-run footer is origin/main's. Each confirmation's words are the action's: the fixtures ask Figma's "Create user 'george'?" and, at lockout risk, "Type the hostname", nav's example label, under Figma's "Disable SSH login for 'root'?", which [P-10](#p-10) and [P-25](#p-25) (d) decide. No continue prompt is asked before the menu returns, pending [P-09](#p-09): the result stays on screen above the menu.
  - Each choice is a few lines in `pkg/ui/flow/flow.go` and `progress.go` (the sentences are the `nothingToChange` and `tooNarrow` constants), so changing one is code of that size and the `flow/*` goldens.
- **Effort:** S. (a) to (e) are built, so this approves them; each alternative is a small change to `pkg/ui/flow` and its goldens, (e3) one to `pkg/ui/nav`, and (d2) to (d4) one to `pkg/ui/prompt` too.
- **Risk:** Low. Under every alternative nothing is applied without the confirmation, and nothing after a decline or an abort; (a2) makes the flow a loop. Under (e) the user types less than the whole name: the short phrase is the action's, which must still name the host, and the hint shows what to type; a terminal too narrow applies nothing. Under (d) the misdrawn block is cosmetic: the steps under it and the result are whole.
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: Figma `USER-CREATE_USER.png`, `SSH_LOGIN-TOGGLE_CONFIRMATION.png` (reference only). After: `docs/refactor/img/p-27-after.png` (placeholder); the `flow/*` goldens show the built form meanwhile.

<a id="p-28"></a>
### P-28: A layout for a screen that only shows: SSH_LOGIN-SSH_SETTINGS_DISPLAY

- **Status:** proposed
- **Source:** `next/p1d-slice`, which builds brief 1B's three layouts; brief 1B: a screen that fits no layout becomes a proposal, not a one-off. Depends on [P-09](#p-09). Related: [P-08](#p-08), [P-12](#p-12).
- **Problem:**
  - Figma's SSH_LOGIN-SSH_SETTINGS_DISPLAY is a title bar, the "SSH Configuration" section with three rows, then "❯ Press any key to continue...": no menu and no action. origin/main's "View current SSH configuration" is the same screen, ending "Press any key to return to the main menu" (pkg/menu disable_root_menu.go:203), as do its System Details and Logs views (system_details_menu.go:126, logs_menu.go:62; [P-17](#p-17), [P-18](#p-18)).
  - It fits none of the three layouts as built. The sub-menu layout always ends with a menu's `PromptLine`, its hint generated from the menu's keys and the navigation inputs, so with no menu it ends "> [0 or q]:", a prompt the frame does not have; the workflow step runs an action's flow; and nothing in `pkg/ui` asks a pause.
- **Proposal:** for the maintainer to choose with P-09:
  - (a) if P-09 drops the pause here, no new layout: the screen is a sub-menu, its section above a menu of "0) Return" alone, drawn by the sub-menu layout as built; or "View SSH Settings" writes the section as an action's result, and the SSH Login menu draws itself again below it (brief 1C: return without wiping the result). 02-SSH_LOGIN's own section already shows the same three rows.
  - (b) if P-09 keeps "Enter to continue" where output must be read, as here: a fourth layout, a display: the title bar ([P-08](#p-08), decided), its sections, then a pause `PromptLine` in P-09's words, which `pkg/ui/nav` would ask as a pause of its own.
- **Effort:** S for (a); M for (b) (a layout, a pause in `pkg/ui/nav`, goldens).
- **Risk:** Low: display only.
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: Figma `SSH_LOGIN-SSH_SETTINGS_DISPLAY.png` (reference only). After: `docs/refactor/img/p-28-after.png` (placeholder).

<a id="p-29"></a>
### P-29: Figma's words or origin/main's: the menu prompt and the User Management sub-menu

- **Status:** proposed (each part is decided separately)
- **Source:** `next/p1d-slice`, whose fixtures fill the layouts after Figma's frames while origin/main is the baseline for wording (refactor plan, decision 6). Related: [P-07](#p-07), [P-13](#p-13), [P-25](#p-25).
- **Problem:**
  - **(a) The menu prompt.** Every Figma menu frame, the brief's main-menu transcription and v0.3.2 ask "Enter your choice"; origin/main asks "Enter selection" on every menu since 607eafa (2025-03-22), `style.Menu`'s default prompt (pkg/style style.go:668), which the main menu never changes ([interaction.md](phase0/interaction.md) §8.2). The design system took Figma's words in `next/p1b-menu` and `next/p1c-form` (the `PromptLine` and `nav` examples, and `form.Select`'s prompt, the `choice` constant in `pkg/ui/form/field.go`) with no proposal behind them; on the main menu, the first screen users see, they would replace origin/main's words.
  - **(b) The User Management sub-menu (01-USER).** Figma's words differ from origin/main's ([interaction.md](phase0/interaction.md) §9.1): the items "Manage User" and "Create User" against "Manage a user" and "Create a user", and "Configure sudo, SSH keys" against "Configure sudo, SSH Keys" (pkg/menu user_options.go:83-90); the section "User Configuration" against "Configuration" (user_menu.go:130); the sudo row "Enabled, No password" against "Enabled no password" (user_menu.go:220-230); and the prompt, as (a).
- **Proposal:** the maintainer decides each, for every screen Phase 5 moves onto the layouts:
  - (a) one prompt label for every menu and `form.Select`: Figma's "Enter your choice", as the components, `nav`, `form` and the `sub-menu/01-user` golden have it; or origin/main's "Enter selection", the baseline, as the main-menu fixture has it meanwhile (`main-menu/00-tui-main`).
  - (b) Figma's words for the User Management sub-menu, as the 01-USER fixture and the `sub-menu/01-user` golden show the frame; or origin/main's, the baseline, which the real screen keeps until this is decided.
  - Each is a string: the fixtures' prompt constants in `pkg/ui/screen/fixture.go`, `form`'s `choice`, and the screen's own item and section words, so changing one is a string and the goldens.
- **Effort:** S.
- **Risk:** Low (wording only; goldens are updated deliberately).
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: Figma `00-TUI-MAIN.png`, `01-USER.png` (reference only). After: `docs/refactor/img/p-29-after.png` (placeholder); the `main-menu/00-tui-main` and `sub-menu/01-user` goldens show origin/main's and Figma's prompt meanwhile.

<a id="p-30"></a>
### P-30: A notice line in the sub-menu layout: origin/main's Firewall screen and an empty DNS list

- **Status:** proposed
- **Source:** `next/p1d-gallery-screens`, which mocks the screens that fit a layout; brief 1B: a screen that fits no layout becomes a proposal, not a one-off. Related: [P-16](#p-16), [P-23](#p-23), [P-28](#p-28).
- **Problem:**
  - origin/main's Firewall screen (pkg/menu firewall_menu.go), of which the export holds a legacy screenshot (images/04-firewall-legacy-tui.png), shows three status rows, then, with UFW installed and no rule listed, a warning line, "No firewall rules configured", before its menu. That is what it shows on every host with UFW, since the rules list is effectively unreachable ([interaction.md](phase0/interaction.md) §4.3). With rules it shows a "Current Firewall Rules:" heading over a line per rule, marked ✓ for allow, ✗ for deny and - otherwise.
  - The sub-menu layout has status rows, sections of key-value rows under a header, and a menu, but no line of its own for a notice: as a section, the line would need a header origin/main does not show; as a key-value row, a key it does not have. So no Firewall mock is built ([README](../design-system/README.md) §9.12).
  - The DNS screen's empty state is the same case: with no nameserver configured, origin/main shows "No nameservers configured" under its "Configured Nameservers:" heading. The DNS mock shows two nameservers configured instead (DEV-058).
- **Proposal:** for the maintainer to choose:
  - (a) a notice block in the sub-menu layout, after the status rows and the sections: one or more `Message`s, such as "⛬ Warning: No firewall rules configured", the kind's word added as every glyph sentence has it (DEV-031, [P-23](#p-23)); the DNS screen's empty state the same, under its section's place.
  - (b) no new block: the empty state as a bullet item of its section, such as "No nameservers configured" under "Configured Nameservers", and for the Firewall a "Firewall Rules" section with the item "No firewall rules configured" (a header origin/main shows only over rules).
  - (c) no line: the state said by a status row, such as "Firewall Rules" "Not Configured" (new words).
  - With rules, under any of them: a section of rows, a rule's state shown by a value state's marker (`ValueOK` for allow, `ValueBad` for deny) beside the rule, so that the mark is never colour alone (DEV-034).
- **Effort:** S (a block of `SubMenu` and its goldens for (a); fixture data for (b) and (c)).
- **Risk:** Low: display only.
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: the export's `images/04-firewall-legacy-tui.png` (reference only). After: `docs/refactor/img/p-30-after.png` (placeholder).

<a id="p-31"></a>
### P-31: Figma's words or origin/main's on the other Figma frames

- **Status:** proposed (each frame is decided separately)
- **Source:** `next/p1d-gallery-screens`, whose mocks draw each Figma frame as Figma fills it, while origin/main is the baseline for wording (refactor plan, decision 6). It asks of the other frames what [P-29](#p-29) (b) asks of 01-USER. Related: [P-07](#p-07), [P-11](#p-11), [P-12](#p-12), [P-28](#p-28), [P-29](#p-29).
- **Problem:** the frames' words differ from origin/main's (origin/main pkg/menu; [interaction.md](phase0/interaction.md) §4.3, §9.1):
  - **02-SSH_LOGIN, SSH_LOGIN-TOGGLE_CONFIRMATION:** the title "Manage Root SSH Login" against "Disable Root SSH Access"; the items "Root SSH Login" (Toggle root SSH access) and "View SSH Settings" (Show SSH configuration) against "Disable root SSH access" (Modify SSH config to prevent root login) and "View current SSH configuration" (Show details of SSH security settings); "0) Return" with no description against "Keep current settings"; the question "Disable SSH login for 'root'?" against "Are you sure you want to disable root SSH access?"; the warning "Before disabling root SSH login, ensure:" against "Before disabling root SSH access, ensure that:", and its checklist, "Non-root user has sudo privileges", "Non-root user SSH access has been tested" and "If SSH fails, a fallback access method is available", against "You have created at least one non-root user with sudo privileges", "You have tested SSH access with this non-root user" and "You have a backup method to access this system if SSH fails" (origin/main shows them on the SSH Login screen, above its menu); the section "SSH Configuration" and its rows "Root SSH Login", "SSH Port" and "Allowed Users" against "Current SSH Configuration:" and "Root SSH login", "SSH port" and "Allowed users", which origin/main shows only under item 2, its view of the configuration ([P-28](#p-28)) (disable_root_menu.go). Item 1's words are also [P-12](#p-12)'s.
  - **05-BACKUP:** the title "Manage Hardn Backups" against "Backup Settings"; the section "Backup Configuration" against the heading "Current Backup Configuration:"; its rows "Backup State" and "Backup Path", the path's note "Writable", against "Backups" and "Backup Path", and a line of its own, "Backup directory exists and is writable"; the items "Backup State" (Toggle backups of modified files), "Backup Path" (Change backup path) and "Verify Directory" (Test if path is writable) against "Toggle backups (currently: <state>)" (Enable or disable automatic backups of modified files), "Change backup path" (Current: <path>) and "Verify backup directory" (Test if backup directory exists and is writable); "Return" against "Return to main menu" (backup_menu.go).
  - **USER-SSH_KEYS:** "Managing SSH keys for: [user]" over "Select an option" and "1) Add SSH key", against "ℹ Managing SSH keys for user: <user>", the user's keys, "Select SSH key operation" and "1) Add SSH key" (Add a new SSH public key), with "2) Remove SSH key" when there are keys (user_options.go).
  - **USER-CREATE_USER:** "Username", "Grant sudo privilege?", "Require sudo password?", "Add SSH key?" and the section "User Configuration", against "Enter username to create", "Allow sudo access?", "Allow sudo without password?" (the opposite polarity, [P-11](#p-11)), "Add SSH public key?" and "Summary".
  - **USER-MANAGE_SUDO-CONFIRMATION:** its title bar, "Manage User", against "Manage Sudo", which the title bar would take.
  - USER-MANAGE_USER-SELECT_USER and USER-MANAGE_USER-MANAGEMENT differ from origin/main only in the prompt ([P-29](#p-29) (a)).
- **Proposal:** the maintainer decides each frame, for the screens Phase 5 moves onto the layouts: Figma's words, as the gallery's mocks and their goldens show them (`sub-menu/02-ssh-login`, `sub-menu/05-backup`, `sub-menu/user-ssh-keys`, `workflow-step/*`); or origin/main's, the baseline, which the real screens keep until this is decided. Each is a string of the screen's fixture or definition and the goldens.
- **Effort:** S.
- **Risk:** Low (wording only; goldens are updated deliberately).
- **Screenshots** (captures wait on freeze, [above](#how-to-read-an-entry)): Before: Figma `02-SSH_LOGIN.png`, `05-BACKUP.png`, `USER-SSH_KEYS.png`, `USER-CREATE_USER.png`, `USER-MANAGE_SUDO-CONFIRMATION.png`, `SSH_LOGIN-TOGGLE_CONFIRMATION.png` (reference only). After: `docs/refactor/img/p-31-after.png` (placeholder); the mocks' goldens show Figma's words meanwhile.

## Safety-primitive units: further proposals

<a id="p-32"></a>
### P-32: Run the legacy commands and file calls through `host.Real`

- **Status:** proposed
- **Source:** `next/p2-system`, which builds `host.System`, adapters from the legacy `interfaces.Commander`/`FileSystem` to it, and the gate `legacyThroughHost` in `pkg/interfaces/system_adapter.go`, shipped `false`. Review of that unit (2026-10-17): the change is a behaviour change with no §3 row or V-number, so it needs the maintainer (brief §3 principle 2; plan §1, review template item 2).
- **Problem:** with the gate off, legacy production code (`OSCommander`, `OSFileSystem`) keeps calling os/exec and os directly, and only new code goes through `host.Real`: two execution paths until Phase 3 migrates each area. With it on, every legacy command changes, all **written, not run** (gate 5): resolved from `/usr/sbin:/usr/bin:/sbin:/bin` only; environment exactly `PATH` and `LC_ALL=C` (children lose `HOME`, `TERM`, `LANG`, `USER`, `SUDO_*`); working directory `/`; a different "command not found" text; stdout and stderr interleaved only approximately in the combined output; at most 10 s of waiting for pipes a daemon holds, then the pipes are closed under it; every command in the run log. The full list, with sources, is [docs/safety/system.md](../safety/system.md) §5.
- **Proposal:**
  - (a) flip `legacyThroughHost` to `true` once `next/p2-vmtest` has run the legacy operations on the Debian 13, Ubuntu 26.04 and Alpine 3.24 VMs (system.md §4) with no regression;
  - (b) flip it now, accepting the list above unrun;
  - (c) keep it off, and let legacy code reach `host.Real` only as each area migrates in Phase 3.
  - Either way the adapters and the through-System characterization goldens stay, so the flip is one constant.
- **Effort:** S (one constant, plus any regression the VM run finds).
- **Risk:** Medium [I]: a legacy command that depended on the inherited environment, the working directory or a tool outside the four directories (only `uv` is likely, on a path unreachable on origin/main) would fail on hosts; the goldens cannot show it, because the change sits below the seam they record.
- **Screenshots:** none (no screen changes).

<a id="p-33"></a>
### P-33: UFW rules hardn adds on its own: while ufw is inactive, for a moved port, and around a blocklist

- **Status:** proposed
- **Source:** `next/p2-ufw` and its review (2026-10-17): V8's open approach, "adding an allow rule the user didn't ask for", which has no §3 row ([side-effects.md](phase0/side-effects.md), V8 row); [docs/safety/ufw.md](../safety/ufw.md) §2.7, §2.9. Related: [P-16](#p-16), [P-20](#p-20).
- **Problem:** the UFW primitive makes three choices on the user's behalf:
  - (1) With ufw inactive and staying so, `PlanFor` saves `ufw allow <port>/tcp` for each SSH listener and the new port (ufw writes them to its rule files without loading them), so that a later `ufw enable` keeps SSH open. That adds rules to a firewall the user may not use.
  - (2) A port change on a host whose current SSH port is source-restricted (`allow from 192.0.2.0/24 to any port 22`) opens the new port with `ufw allow 2230/tcp`, from anywhere: moving the port widens who can reach SSH. The plan reports it (`ssh-new-port-wider`) and goes ahead.
  - (3) A saved blocklist on the SSH port (`deny from 198.51.100.0/24 to any port 22` before an allow) is `Excepted`: enabling ufw is refused, since the primitive cannot tell whether the session's own address is among the exceptions; on an active ufw it is reported (`ssh-allowed-except`).
- **Proposal:** the maintainer decides each:
  - (1) (a) save the rules while ufw is inactive, as now; (b) save none (`Desired.NoSaveWhileInactive`, already there), and report each need a later start would block (`ssh-not-allowed-at-start`).
  - (2) (a) open the new port from anywhere and report it, as now; (b) give the new port the current port's source restriction (`ufw allow from <src> to any port <new> proto tcp` for each restricted rule); (c) refuse the port change until the restriction is moved by hand.
  - (3) (a) refuse the enable, as now; (b) allow it when the session's client address (`SSH_CONNECTION`'s first field) is outside every excepted source, and refuse otherwise.
- **Effort:** S for (1), (2)(c) and (3)(a); M for (2)(b) and (3)(b) (a new input, new rule forms and their undo).
- **Risk:** Low for (1) and (2)(a); Medium for (2)(b) (new rule forms to read back and undo) and (3)(b) (the session's address is one client among those the blocklist may concern).
- **Screenshots:** none (no screen changes).

<a id="p-34"></a>
### P-34: A reboot inside the confirm window

- **Status:** proposed
- **Source:** `next/p2-revert` (brief 2.4; [docs/safety/revert.md](../safety/revert.md) §2.7, §2.20, §7).
- **Problem:** the revert timer is runtime state: on systemd a transient timer and service (systemd keeps transient units in `/run/systemd/transient`, systemd.unit(5)), on OpenRC a detached sleeper with its pidfile in `/run/hardn`. A reboot between the apply and the deadline loses it, whoever reboots: the user, locked out, through the provider's console or a power cycle, or anything else. The change then stays applied and unconfirmed. The journal survives in `/var/lib/hardn`, so the next run of hardn says the change is overdue and how to undo it ("its timer did not run (a reboot stops it)"), but a user locked out by the change cannot reach that run over SSH, and nothing undoes it at boot.
- **Proposal:** the maintainer decides:
  - (a) keep it as it is: the overdue notice, and `hardn rollback <id>` from a console;
  - (b) roll back at boot: an unconfirmed lockout-risk change found at boot is rolled back, by a unit hardn installs once (systemd: a oneshot service in `/etc/systemd/system`, enabled, running `hardn rollback --pending` or an equivalent before `ssh.service`; OpenRC: a `local.d` script or a service in the default runlevel). This is a new persistent file and a new boot-time behaviour, and on systemd possibly a new CLI form;
  - (c) re-arm at boot: the same unit re-arms the timer for what remains of the window (or the whole window), instead of rolling back at once.
- **Effort:** S for (a); M for (b) and (c) (a unit or script, its install and removal, and a VM test that reboots inside the window).
- **Risk:** Low for (a), which leaves a lockout through a reboot unrepaired; Medium for (b) and (c): a boot-time rollback acts while nobody watches, and must not run against a journal from a hardn that was since replaced or a change the user meant to keep (confirmed late, after the reboot).
- **Screenshots:** none (no screen changes).

<a id="p-35"></a>
### P-35: Giving up a rollback that cannot finish

- **Status:** proposed
- **Source:** `next/p2-revert` (review fixes; [docs/safety/revert.md](../safety/revert.md) §2.16, §7).
- **Problem:** a rollback that stops is kept in `/var/lib/hardn/rollback.json` and taken up again by `hardn rollback <id>`; until it finishes, every new change is refused. One that can never finish (its backup run directory was deleted, an undo command such as `ufw` was removed) fails the same way on every attempt, and the only way out today is by hand: moving `rollback.json` aside, which the notice and the error now say. `revert.Abandon` does it properly (renames it to `abandoned-<id>.json`, logs what was and was not done, refuses while the rollback is running), but no command line reaches it: this unit adds no CLI form beyond `hardn confirm` and `hardn rollback <id>`.
- **Proposal:** the maintainer decides whether to add a deliberate form, for example `hardn rollback --abandon <id>` (root, refuses `-n`, says that the host stays as the rollback left it and lists what it did not undo), wired to `revert.Abandon`; or to keep the manual step, documented.
- **Effort:** S (a flag, its wording, a test).
- **Risk:** Low: it only frees the journal; a user could abandon a rollback that a retry would have finished, so the wording must show what is left undone.
- **Screenshots:** none (no screen changes).

<a id="p-36"></a>
### P-36: An `AllowUsers` without root on Proxmox VE: add root, or refuse

- **Status:** proposed
- **Source:** `next/p2-pve` ([docs/safety/pve.md](../safety/pve.md) §2.5); the open item `next/p0-hazards` left to that unit: legacy `ConfigureSSH` writes the configured `AllowUsers`, which can leave root out and so block the cluster's root logins whatever `PermitRootLogin` says. Related: V11, plan R15, [P-33](#p-33) (rules hardn adds on its own).
- **Problem:** Proxmox VE's nodes log in to each other as root, by key, on port 22 (pve-docs, Cluster Manager). An `AllowUsers alice` in hardn's settings would stop that. `pve.PlanSSH` adds `root` to such an `AllowUsers` (`AllowUsers alice root`) and reports it (`root-allowed`): hardn then writes a user the configuration did not name. A `root@<host>` entry does not count, since the other nodes' addresses are not read, so `root` is added beside it. Root stays key-only either way (`PermitRootLogin prohibit-password`).
- **Proposal:** the maintainer decides:
  - (a) add `root` and say so in the plan, as built;
  - (b) refuse the change with `ErrRootExcluded` until the configuration lists root (or the user removes `AllowUsers`), as `PlanSSH` already does for a `DenyUsers` matching root;
  - (c) add `root@<each node's address>` from the cluster's configuration instead of a bare `root` (new reads: corosync's node list).
- **Effort:** S for (a) and (b) (one branch and its test); M for (c).
- **Risk:** Low for (a) (root by key from anywhere, as on a stock node); Low for (b), which blocks a hardening the user asked for until they edit the configuration; Medium for (c) (a node added later is locked out until hardn runs again).
- **Screenshots:** none (no screen changes).

<a id="p-37"></a>
### P-37: DNS on hosts whose resolver hardn reports but does not configure

- **Status:** proposed
- **Source:** `next/p2-dns`, the DNS primitive ([docs/safety/dns.md](../safety/dns.md) §2.3, §2.9; V19). Brief 2.5 asks hardn to "configure that manager instead of overwriting the file"; the unit configures systemd-resolved (a drop-in) and plain files (Debian, Alpine, Proxmox VE), and reports the rest.
- **Problem:**
  - **resolvconf and NetworkManager hosts.** origin/main configured a resolvconf host by writing `/etc/resolvconf/resolv.conf.d/head` and running `resolvconf -u` (side-effects.md row 22), and never detected NetworkManager. The new primitive refuses both with `ErrManaged`, so on such a host the DNS screen would stop where origin/main wrote a file. The legacy `head` is documented for comment lines and is prepended to what the interfaces provide **[unverified: resolvconf(8) not read for this unit]**; NetworkManager rewrites a regular `/etc/resolv.conf` and keeps its servers per connection (NetworkManager.conf(5), `rc-manager`).
  - **Per-link servers on systemd-resolved hosts.** Global `DNS=` does not displace the servers DHCP, netplan or networkd give each link: resolved queries them in parallel (resolved.conf(5)). A user who sets 192.0.2.53 on Ubuntu keeps querying the DHCP server too. The unit reports it (`dns-per-link`) and changes nothing per link.
  - **Containers.** The unit refuses (`ErrContainer`): the container manager provides the file (Docker, LXC) **[unverified per runtime]**.
- **Proposal:**
  - (a) keep the unit's behaviour: report resolvconf, NetworkManager, per-link servers and containers, and let the slice-2 screen say what to change by hand;
  - (b) add writers: resolvconf through its `base` (or origin/main's `head`) plus `resolvconf -u`, and NetworkManager through `nmcli connection modify <c> ipv4.dns … ipv4.ignore-auto-dns yes` on the active connections **[unverified: syntax not read for this unit]**, each with its own read-back check and undo;
  - (c) on systemd-resolved, offer an explicit plan step that also stops the per-link servers (netplan `dhcp4-overrides: use-dns: false`, or a networkd drop-in `UseDNS=false` **[unverified: syntax not read for this unit]**), which can break name resolution where the DHCP server is the only resolver for a local zone.
  - The recommendation is (a) for v0.4, and (b) and (c) only if hosts in use need them; either way the decision belongs to slice 2.
- **Effort:** (a) none; (b) M per manager; (c) M.
- **Risk:** (a) Low: no write on a host whose manager hardn does not understand; (b) and (c) Medium **[I]**: each is a new writer that can only be proved on a VM (gate 5).
- **Screenshots:** none (no screen changes in this unit).

<a id="p-38"></a>
### P-38: An `sshd_config` whose Include is not the stock first line

- **Status:** proposed
- **Source:** `next/p2-sshd-validate` ([docs/safety/sshd-validate.md](../safety/sshd-validate.md) §2.1 `IncludeOther`, finding `include-not-first`, and §6 "Open for the maintainer"), also refused by `next/p2-sshd-migrate` (`ErrIncludeOther`, [sshd-migrate.md](../safety/sshd-migrate.md) §2.11) and by `next/p2-vmtest`'s driver ([vmtest.md](../safety/vmtest.md)). Brief 2.2's strategy, decided in brief §2 ("hardn's SSH settings live in `00-hardn.conf`").
- **Problem:** brief 2.2 has two write strategies: `/etc/ssh/sshd_config.d/00-hardn.conf` when `sshd_config`'s first directive is the stock `Include /etc/ssh/sshd_config.d/*.conf`, and a `# BEGIN hardn` … `# END hardn` block at the top of `sshd_config` when there is no Include at all. A third shape has none: an Include that is not exactly that first line, such as another spelling (`include`, a tab, `Include=`, quotes, a trailing comment, indentation), another path or glob, or the stock line below other directives (an administrator's `Port 22` above it, say). sshd may read some of these as the stock line; in others the drop-ins are read after the lines above the Include, which then win for every first-value keyword, or are not read at all. hardn reports the shape (`include-not-first`), validates only exact Includes (any other is `ErrUnsupportedInclude`), and writes nothing: no SSH setting can be applied on such a host today.
- **Proposal:** the maintainer decides:
  - (a) keep reporting and refusing: the SSH controls stay Unknown there, and the report tells the user to restore the stock first line by hand;
  - (b) treat it as the no-Include case: write hardn's block at the top of `sshd_config`, above the administrator's Include, so that it wins by first value. The main file changes (the Debian conffile prompt on an OpenSSH upgrade, which `00-hardn.conf` avoids), and the block is clobberable as in that case (cloud-init, Proxmox VE's `pvecm`);
  - (c) an explicit plan step, backed up and validated in context with the migration's login-path comparison, that inserts the exact stock Include as the first directive and leaves the administrator's line where it is, then writes `00-hardn.conf`. The drop-ins are then read before lines that used to come first, which can change which value wins, so the step is a lockout-risk change under confirm-or-revert;
  - (d) accept the stock line further down only when nothing above it sets a keyword hardn writes, and refuse the rest as in (a) **[I]**.
- **Effort:** (a) none; (b) M (the block writer, its reading back and undo); (c) M; (d) S.
- **Risk:** (a) Low: nothing is written on a host hardn does not model, at the cost of no SSH hardening there; (b) Medium: main-file edits that other tools rewrite; (c) Medium: it reorders what sshd reads, validated before it takes effect; (d) Low to Medium: one more reading of sshd's rules to get right.
- **Screenshots:** none (no screen changes).

<a id="p-xx-revert-plans"></a>
### P-XX-revert-plans: Migrating `listen.conf` on a socket host where it never took effect (D9)

- **Status:** proposed
- **Source:** `next/p2-revert-plans` ([docs/safety/revert-plans.md](../safety/revert-plans.md) §2.10, §6), on decision §3.17 (migrate the legacy artifacts automatically, approved 2026-10-17) and D9 ([side-effects.md](phase0/side-effects.md): v0.3.2 wrote `ssh.socket.d/listen.conf` without `daemon-reload`, [I] for its effect).
- **Problem:** on an Ubuntu host hardened by v0.3.2 and not rebooted since, `ssh.socket` still listens where it did before (port 22) while `listen.conf` names another port (2230). The migration itself is safe there: it holds both ports ([sshd-migrate.md](../safety/sshd-migrate.md) §2.8). Its rollback is not: it restores the files, `listen.conf` included, and the reload that follows (`daemon-reload`) gives the socket `listen.conf`'s port alone, so it cannot give back the listeners the socket had before, and would move the socket off the port the user connected on. The check after `daemon-reload` stops the restart, but systemd may already have dropped the old port's descriptor then **[I]**. So `p2drive` refuses that migration, and the engine would have to: on such hosts §3.17's "automatically" does not hold.
- **Proposal:** the maintainer decides:
  - (a) keep refusing, with a report saying why; the host is migrated once the socket listens where `listen.conf` says (after a reboot, or `systemctl daemon-reload` and `systemctl restart ssh.socket`), which is v0.3.2's own change taking effect late, outside hardn's confirm-or-revert;
  - (b) on such a host, roll the files back but not the socket: the rollback restores `listen.conf` and the rest and reloads nothing for the socket, which keeps the migration's listeners (both ports) until the next `daemon-reload` or reboot, when `listen.conf` applies as it would have before the migration; the rollback reports the socket as left on a superset of its listeners;
  - (c) on such a host, the rollback writes a hardn drop-in holding the socket's listeners from before the change beside the restored `listen.conf`, so that its reload gives them back; a new hardn artifact under `ssh.socket.d` to remove later.
  - The recommendation is (a) for v0.4: nothing is applied whose rollback cannot be faithful. (b) closes no port and needs only a journal flag the rollback honours; it is the candidate if such hosts are common.
- **Effort:** (a) none; (b) S (a reload entry the rollback skips, its notice); (c) M (a writer, its own removal).
- **Risk:** (a) Low, at the cost of no migration on those hosts until they are rebooted; (b) Low to Medium: the socket's runtime and files disagree after the rollback, as they did before the migration; (c) Medium: one more file overriding the generator.
- **Screenshots:** none (no screen changes).
