# hardn: Phase 0 findings

> **What this is:** the brief's Phase 0 deliverable (repo recovery and discovery), dated 2026-10-16, written against brief revision 12. It comes from a read-only audit of the 2025 checkout. Adversarial verification (safety claims re-tested in Go test harnesses; gc behaviour tested on a replica of the object store) and primary-source research (OpenSSH, Ubuntu/Debian/Alpine packaging, Proxmox, Lip Gloss v2, Go and GitHub Actions) cross-checked the audit. Four plan reviews corrected it, and it records the maintainer's decisions of 2026-10-16 (§13).
>
> **Labels:** **[V]** verified (seen in code, command output or a primary source). **[I]** inferred from evidence, not executed on a Linux host.
>
> **Appendices** (written alongside this file):
> - [phase0/inventory.md](phase0/inventory.md): the full branch, stash and commit inventory, with files touched and conflicts for every item
> - [phase0/side-effects.md](phase0/side-effects.md): the per-distro side-effect table, harness evidence and the V1–V27 defect list
> - [phase0/interaction.md](phase0/interaction.md): every screen, menu and prompt with its inputs, outputs and navigation
> - [phase0/v032-reference.md](phase0/v032-reference.md): v0.3.2 behaviour, the reference for regressions (decision 6)
>
> **Logs started in Phase 0:** [proposals.md](proposals.md) (enhancements, maintainer decides) and [../design-system/deviations.md](../design-system/deviations.md) (deviations from Figma).
>
> **Redaction:** host names, addresses, local paths, editor settings and the contents of the maintainer's real config are replaced by neutral wording. Example values are synthetic.
>
> **Status:** Phase 0 is complete apart from the VM-dependent timing (§9). **Stop: waiting for the Phase 0 review.**

## Contents

1. [Backup](#1-backup)
2. [Repository state](#2-repository-state)
3. [Ranked inventory](#3-ranked-inventory-recency-first-then-relevance)
4. [Branch and tag naming scheme](#4-branch-and-tag-naming-scheme-chosen-and-in-use)
5. [Baseline build, test, lint and CI status](#5-baseline-build-test-lint-and-ci-status)
6. [Architecture map](#6-architecture-map-originmain-the-worktree-base)
7. [Side-effect inventory (summary)](#7-side-effect-inventory-summary)
8. [Interaction audit (summary)](#8-interaction-audit-summary)
9. [Timing table](#9-timing-table-deferred)
10. [Verify list](#10-verify-list-brief-9)
11. [Risks](#11-risks)
12. [Proposed adjustments to the brief](#12-proposed-adjustments-to-the-brief)
13. [Decisions recorded](#13-decisions-recorded-2026-10-16)
14. [Evidence](#14-evidence)

---

## 1. Backup

- The maintainer keeps a separate backup elsewhere. Its location and date are intentionally not recorded here (decision 2).
- As brief §4 says, the backup is a last resort. It doesn't relax any git-safety rule.

---

## 2. Repository state

### 2.1 Snapshot after P0.1 (2026-10-16)

| Item | State | Evidence |
|---|---|---|
| Old checkout HEAD | `arch3` @ ef0b26a (2025-04-03 22:36). Left as it is. | [V] |
| Working tree | Two unstaged cosmetic edits (index clean): `legacy_pkg/infrastructure/style/style.go` (mtime 2025-04-10; two whitespace-only lines) and `pkg/adapter/driven/system/OsNetworkInfoAdapter.go` (mtime 2025-11-14). No untracked files: the design export, brief, plan and findings draft were moved out of the repo (decision 3). | [V] `GIT_OPTIONAL_LOCKS=0 git status --porcelain=v1 --untracked-files=all` |
| Stashes | 37, dated 2025-03-21 to 2025-03-30. None has an untracked-files parent. Untouched. | [V] `git stash list --date=iso` |
| Local branches | 2025: `arch3`, `box-content`, `hex-migration`, `hex-migration-legacy`, `hex-migration-new`, `linux-packages`, `main`, `refactor/ioa`. New: `next/v0.4`, `next/p0-*` (§4). | [V] |
| Worktrees | The old checkout, plus `hardn-next` (a sibling directory), which holds `next/v0.4` @ 9d46866 and the stacked Phase 0 units. | [V] `git worktree list` |
| Remote | `origin` = github.com/abbott/hardn. Remote `main` = 9d46866 and `linux-packages` = 6092ee3. Unchanged since 2025-03-28; a no-prune fetch confirmed this before the worktree was created. | [V] |
| Local `main` vs `origin/main` | `git log origin/main..main` is empty. Local `main` (57cccb8) is 1 commit behind (9d46866 changes only the README). | [V] |
| Release tags | v0.3.1 (d4160b3) and v0.3.2 (bce7cc2), both on the remote and both GitHub releases. v0.3.2 is "Latest". | [V] |
| Archive tags | 75 local `archive/…` tags (§4). | [V] `git tag -l 'archive/*' \| wc -l` |
| gc guards | `gc.auto=0`, `maintenance.auto=false` (local config). | [V] `git config --get` |
| Unreachable commits | `git fsck --unreachable --no-reflogs` reports nothing; before P0.1 it reported 125 commits. | [V] |

**Rule:** every git command against the old checkout is read-only and runs with `GIT_OPTIONAL_LOCKS=0`. A plain `git status` takes the index lock and refreshes it (verified on a replica).

### 2.2 History preservation (P0.1, done)

Before P0.1, this history was exposed to the next automatic gc:

| Population | Count | At risk from | Anchored by |
|---|---|---|---|
| Commits reachable from refs | 138 | nothing | (branch-tip tags, as insurance) |
| Commits reachable only through the `refs/stash` reflog: the 36 older stashes, W+I | 72 | Only an explicit `git reflog expire --expire=…`, a `gc.refs/stash.reflogExpire*` config, or `git stash drop`/`clear`. **Default gc never expires `refs/stash`** [V: git 2.50.1 `reflog.c:102-111` plus experiments] | `archive/stash/*` (all 37, annotated) |
| Commits reachable only through HEAD or branch reflogs: 404f386, ff74d07, b5c60d6, 7a25bb9, 13b3209 | 5 | the next gc (every non-stash reflog is older than 90 days) | `archive/reflog/*` |
| Unreachable commits: 24 dropped stashes (W+I pairs), 2025-03-05 to 03-23 | 48 | the next gc (objects date from 2025-03; the prune window is 2 weeks) | `archive/dropped-stash/*` (the W commit; I is its parent) |
| Dangling blobs and trees under no commit | 50 blobs + 28 trees | the next gc | `archive/dangling-objects/2026-10-16` (one anchor commit; not a real commit) |

- **The trigger** [V, git 2.50.1 source and a replica]:
  - `git fetch` and `git commit` run `git maintenance run --auto --detach`.
  - That runs `gc --auto` once `.git/objects/17` holds more than 27 loose objects (`builtin/gc.c:427-462`). It held 19.
  - An editor's autofetch ran a fetch every 180 s while its window was focused, in this checkout and in a second local copy.
  - On the replica, a triggered gc removed the 48 dangling and 5 reflog-only commits and emptied the HEAD reflog (259 → 0 entries). It kept the 37 stashes.
- **Guards now in place:**
  - `gc.auto=0` makes `need_to_gc` return 0 (`builtin/gc.c:629-636`).
  - `maintenance.auto=false` stops fetch and commit from spawning maintenance (`run-command.c:1820-1822`).
  - So autofetch can no longer trigger a gc [V source].
- **Residual risk:**
  - The anchors protect every commit and object.
  - A *manual* `git gc` would still expire the non-stash reflog **entries**, which are the only record of the 2025 resets, amends and deleted branch names.
  - So never run `git gc`, `git prune` or `git maintenance run` in this repo. Never run `git stash drop`/`clear` or `git reflog expire --expire…`, and never set any `gc.*reflogExpire*` config.
- **Worktree hazard** [V, throwaway repo]: `git stash` in a linked worktree writes the shared `refs/stash` and renumbers the 37 historical stashes. **Never stash in `hardn-next`.**
- **Second local copy** [V]:
  - It has identical refs, stash list, reflogs and objects. It is not the backup, and no work happens in it.
  - Decision 1's guards and anchors apply only to this checkout.

### 2.3 Activity timeline (the brief's "untouched since March 2025" is not quite right)

| Date | Activity |
|---|---|
| 2025-02-28 → 03-02 | 63 pre-public commits, squashed into e4355f8. The ignored `log.txt` is the only record of them. |
| 2025-03-02 / 03-04 | v0.3.1 / v0.3.2 released |
| 2025-03-05 → 03-31 | IOA refactor (PR #1, merged 03-08), with its dropped stashes; 43 unreleased commits on `main`; the box-content and system-info stash chains (03-18 → 03-20); then the hex-migration lineage (four local-only branches) and the 37 live stashes |
| 2025-04-03 | Last commit (ef0b26a on `arch3`); `build/hardn_new` built |
| 2025-04-10 | `style.go` working-tree edit |
| 2025-06-07 | Remote PR #2 opened and closed within 14 minutes (Commander injection; its objects are **not** local) |
| 2025-10-20 → 11-07 | External PR #3 (one-line docs change), closed |
| 2025-11-14 | A `git fetch` (origin/HEAD reflog), plus the `OsNetworkInfoAdapter.go` edit |
| 2026-01-06 | File-manager metadata activity only |
| 2026-10-15/16 | Figma file and export, the brief, a whitespace edit to the local config file; then this Phase 0 |

### 2.4 Changes made to the old checkout during Phase 0

| When (2026-10-16) | What | Source |
|---|---|---|
| 01:37:54 | 11 `# claude-code-runtime` lines appended to `.git/info/exclude`; an ignored lock file created | Agent tooling. The maintainer chose to keep them. |
| repeatedly | `.git/FETCH_HEAD` rewritten and auto-maintenance run (no gc: below the threshold). No ref moved and no objects were added. | An editor's autofetch, not the audit |
| 02:11:28 | `.git` mtime bumped by a lock-taking git command; index content unchanged | Unattributed. Most likely an agent's plain `git status` [I] |
| about 01:52 | Index stat-cache refresh in an older local copy (no content change) | Most likely an audit lane [I] |
| P0.1 | `gc.auto=0`, `maintenance.auto=false`; 75 archive tags; a no-prune fetch (no ref moved); worktree `hardn-next` added | Decision 1 |
| P0.1 | The Figma export moved out to a sibling `_design/` folder; the brief, plan, findings draft and audit evidence moved to a sibling `_support/` folder. Both sit in the parent directory, outside every git tree. | Decision 3 |

Apart from these, the working tree, the stash list, HEAD, the existing branch refs and the index content are unchanged.

---

## 3. Ranked inventory (recency first, then relevance)

- **How it is ranked:**
  - By most recent activity: the commit date for commits and branches, the stash time for live and dropped stashes, and file mtimes for working-tree and ignored files (a rough signal, per brief 0.2).
  - "Behind/ahead" is relative to `origin/main` (9d46866).
  - **P0.1** marks the 26 commits assessed after anchoring: 24 dropped stashes and 2 reflog-only commits (§3.2).
- **Recommendations:**
  - **salvage** = cherry-pick specific commits;
  - **reference** = keep for ideas and re-implement;
  - **archive** = tag and leave.
  - **The maintainer decides.** No item is recommended for salvage as code.
- **Baseline** (decision 6): origin/main's content is the baseline for wording and behaviour, and arch3's menus and operations are that same code relocated (see row 4). Rows that "landed" are therefore already in the baseline.

### 3.1 Ranked table

| # | Item | Last activity | Position | What it was trying to do | Builds? | Rec. |
|---|---|---|---|---|---|---|
| 1 | Working-tree edits on `arch3` (2 files) | 2025-11-14 / 04-10 (mtime) | unstaged; never committed | Cosmetic: whitespace, import order, comments | yes (with the ignored vendor files on disk) | archive (leave in place) |
| 2 | Remote PR #3 (external fork) | 2025-11-07 | fork; closed | One-line contributing-doc tweak | n/a | fold into the Phase 7 docs refresh |
| 3 | Remote PR #2 "Inject commander into system and security routines" | 2025-06-07 | remote only (objects not local); base 9d46866; 13 files, +162/−112 | Replace about 41 `exec.Command` sites in `pkg/{security,system,updates}` with `interfaces.Commander`; adds 3 mock tests | **no** as submitted (2 compile errors) [V]; yes with a 3-line fix | **reference**: call-site checklist for the `System.Run` seam. A local copy needs an approved fetch (`archive/2025-06/pr-2`, not created) or the saved diff |
| 4 | **`arch3`** (efc18db..ef0b26a, 6 commits) | 2025-04-03 | local only; 83 ahead / 2 behind [V] | "Pragmatic hexagonal" (ADR 004). Its `legacy_pkg/` holds origin/main's menus and operations, relocated (menu item 9 commented out). Its new UI is a two-screen prototype: a Figma-style header with a two-item menu, plus System Details | committed tree **no** (vendor); `./cmd/hardn` absent; `go vet` fails | reference (lessons only) |
| 5 | `hex-migration-new` (26 commits) | 2025-03-31 | local only; 77/2 | Parallel "hex-v2" app in `pkg/` (entrypoint `cmd/hardn/menu`): header + System Details | as arch3 | reference: menu-as-data (1eef14c), hint generator, host-fact collectors, `FormatUptime` in Figma format |
| 6 | stash@{0} "hex-v2 raw mode" (4e64967) | 2025-03-30 | base 061daae | Hand-rolled termios raw mode, ISIG cleared, `/tmp/hardn_debug.log` mode 0666 | not built | archive (conflicts with §2 Enter-based input) |
| 7 | stash@{1..3} "hex-v2 1–3" | 2025-03-29 | base 428e823 | WIP of f26e207 | = f26e207 | archive (fully landed) |
| 8 | **`origin/main`** (9d46866) | 2025-03-28 | remote default branch; 43 commits past v0.3.2 [V] | IOA refactor (PR #1), setup-sudo-env, update checker, system-details, `--no-color`, structured layout, Figma-aligned main menu | yes with `-mod=mod`/`readonly`; **no** with the committed vendor | **worktree base and behaviour baseline** (decision 6). Not releasable as it is (§7) |
| 9 | `hex-migration-legacy` (15 commits) | 2025-03-28 | local only; 51/2 | Strangler fig: move all code to `legacy_pkg/`, entrypoint to `cmd/legacy` | `./cmd/hardn` gone | archive |
| 10 | stash@{4..11} "hex-menu 1–7" | 2025-03-25 → 03-28 | `hex-migration-legacy` | New-menu and ScreenBuilder drafts | — | archive (@{4} ≡ 483afe3; the rest are superseded drafts) |
| 11 | stash@{12..31} "legacy rename", "system info snap", "refactor snapshot" | 2025-03-22 → 03-25 | `hex-migration` | Progress snapshots | — | archive. **Never apply @{13} on its own: it deletes 70 files.** |
| 12 | `hex-migration` (36 commits) | 2025-03-25 | local only; 36/2 | Hexagonal system-info consolidation; inbound/outbound renames; UI abstraction; resolv.conf manager | vendor fails; vet fails (`string(int)` UID bug, f23515b) | reference: resolv.conf manager detection (948b040) for brief §2.5 DNS |
| 13 | **P0.1** 13b3209 (reflog-only) "feat: systemsysInfo screen demo" | 2025-03-25 00:05 | `hex-migration` lineage; amended 11 s later into ba7f1af; never pushed | Stand-alone demo binary for the system-info screen through the hex TUI stack | **no** (zero-argument `NewSystemInfoManager()`); the amend builds | archive |
| 14 | **P0.1** fc5e551 (dropped) "refactor system info snap 1" | 2025-03-23 17:38 | base a7cc7e0 (`hex-migration`, 2 behind / 15 ahead); re-stashed 2 min later as live stash bf0202a | Delete legacy `pkg/system`; route system-details through `NewSystemInfoManager` + a factory | **no** (wrong interface name; the factory package was untracked) | archive (strict subset of bf0202a) |
| 15 | Local `main` (57cccb8) + stash@{32..36} | 2025-03-23 (stashes 03-21 → 03-22) | 0 ahead / 1 behind | Structured-layout styling | yes (`-mod=mod`) | archive (landed in 57cccb8 / 607eafa / 1c9f4ea) |
| 16 | 7a25bb9 (reflog-only, `main`) "remove deprecated flags" | 2025-03-21 02:21 | amended away; local only | Pre-amend commit | — | archive (content = ea8680f + 6f6d9a0) |
| 17 | `box-content` = `linux-packages` = `origin/linux-packages` (6092ee3) | 2025-03-21 01:17 | in origin/main's history; `linux-packages` is still on the remote | "user service and menu" | yes | archive (deleting the remote branch at RC needs approval) |
| 18 | **P0.1** b371694 (dropped) "menu refactor, extended user, snapshot 4" | 2025-03-20 12:22 | base f9dfe3b (in origin/main's history) | Boxed User Management view plus option handlers; extended user info; create-user asks "Allow sudo access?" | **no** as stashed (a package was untracked); yes with 6092ee3's 4 new files | **reference** (evidence for the V22 fix, §3.2) |
| 19 | **P0.1** 1bb6c29 (dropped) "… snapshot 3" | 2025-03-20 00:21 | base f9dfe3b | Earlier stage of snapshot 4; `last -1` parser | yes (vet: 1 finding) | archive |
| 20 | **P0.1** 777fa1a (dropped) "… snapshot 2" | 2025-03-19 21:57 | base f9dfe3b | User box, extended user, create-user dialog, example-config reorder | yes | archive |
| 21 | **P0.1** 114d9ef (dropped) "… snapshot 1" | 2025-03-19 18:57 | base f9dfe3b | `GetExtendedUserInfo` end to end, with a service test | yes | archive |
| 22 | **P0.1** db16a33 (dropped) "menu refactor, extended user" | 2025-03-19 18:00 | base f9dfe3b | First extended-user snapshot | yes | archive |
| 23 | **P0.1** 206a9f2 (dropped) "menu refactor" | 2025-03-19 16:43 | base f9dfe3b | SSH keys as their own boxed screen, with a loop instead of recursion | yes | archive |
| 24 | **P0.1** 0534a08 (dropped) "box content v4" | 2025-03-19 15:34 | base f9dfe3b | Reverts v3's rename; SSH-keys loop; fixes v3's compile error | yes | archive |
| 25 | **P0.1** 1d2e1a5 (dropped) "box content v3" | 2025-03-19 15:09 | base f9dfe3b | File split: `user_options.go`, `ssh_keys_*.go`; `Show` → `ShowUserMenu` | **no** (`HandleSSHKeysOptions() (no value) used as value`) | archive |
| 26 | **P0.1** 90396be (dropped) "box content v3" | 2025-03-19 15:07 | base f9dfe3b; nothing staged | Same as 1d2e1a5, without its 3 new files | **no** (partial capture) | archive |
| 27 | **P0.1** 8efc3ef (dropped) "box content v2" | 2025-03-19 14:33 | base f9dfe3b | Shared box styling; boxed user screen | yes | archive |
| 28 | **P0.1** 964620d (dropped) "box content v1" | 2025-03-19 10:48 | base f9dfe3b | First box-content pass; `details` → `system-details` | yes | archive |
| 29 | **P0.1** bca4b30 (dropped) "user menu v2" | 2025-03-19 00:58 | base f9dfe3b | Second boxed User Management draft | yes | archive |
| 30 | **P0.1** 12607d8 (dropped) "user menu" | 2025-03-18 23:31 | base f9dfe3b | First boxed User Management draft | yes | archive |
| 31 | **P0.1** 146459b (dropped) "v3.3 WIP on system-info" | 2025-03-18 17:37 | base 5189b6f (in origin/main's history); branch `system-info` since deleted | gopsutil-backed system report ("system details") | yes | archive (landed in f9dfe3b) |
| 32 | **P0.1** 994f03c (dropped) "v3.3 WIP on main" | 2025-03-18 16:45 | base 5189b6f | Earliest form ("machine summary"); first gopsutil/v3 in the history | yes | archive |
| 33 | **P0.1** 984d938 (dropped) "version display" | 2025-03-08 05:54 | base f86e1a7 (`refactor/ioa`; 21 behind / 0 ahead) | Main-menu header rework; version line; dry-run status beside the title | yes | archive (= e308c99) |
| 34 | b5c60d6 (reflog-only, `refactor/ioa`) | 2025-03-08 05:52 | reset away | Same content as 984d938 | — | archive (= e308c99) |
| 35 | **P0.1** c57fba2 (dropped) "colors" | 2025-03-08 04:27 | base f86e1a7 | Identical-tree re-stash of c3ed213 | yes | archive |
| 36 | **P0.1** 91013d9 (dropped) "colors library integration" | 2025-03-08 04:25 | base f86e1a7 | Despite the name: hand-rolled ANSI header and version-line drafts | yes | archive |
| 37 | **P0.1** c3ed213 (dropped) "colors library integration" | 2025-03-08 03:10 | base f86e1a7 | Move `pkg/style` and every menu onto fatih/color objects | yes | archive (abandoned within about 75 min) |
| 38 | `refactor/ioa` (f86e1a7) = PR #1 | 2025-03-07 20:11 | merged; deleted on the remote; local tracking ref stale | IOA layers | yes | archive (fully contained in origin/main) |
| 39 | ff74d07 (reflog-only) | 2025-03-07 20:06 | amended into f86e1a7 | Service-layer unit tests | — | archive |
| 40 | **P0.1** 7e8de67 (dropped) "IOA main menu" | 2025-03-05 22:11 | base 404f386 (36 behind / 1 ahead) | First IOA-backed menus (MenuManager, MenuFactory) | yes | archive (= 0cbc152) |
| 41 | **P0.1** 404f386 (reflog-only) "configuration models and services" | 2025-03-05 20:59 | parent 8420206; amended into 08a805a | IOA foundation for SSH, firewall, DNS; old-vs-new ComparisonTester | yes | archive (message-only amend of 08a805a) |
| 42 | **P0.1** cac2e0e (dropped) "IOA branch" | 2025-03-05 18:04 | base bfb66a1 (37 behind) | `NetworkOperations` seam | **no** (mocks file untracked) | archive (= 8420206 minus one fixed line) |
| 43 | **P0.1** 2aef5d3 (dropped) "initial IOA attempt" | 2025-03-05 17:07 | base 99d8d1f (38 behind) | First test-seam draft: Commander, FileSystem, Network, in-memory mocks | yes | **reference** (ideas for `p2-system`, §3.2) |
| 44 | **P0.1** b8a0fed (dropped) "initial IOA attempt" | 2025-03-05 17:03 | base 99d8d1f | Partial capture: `.gitignore` +`*.scratch` only | yes | archive |
| 45 | Tag `v0.3.2` (bce7cc2) | 2025-03-04 | release | Latest release | yes with modules | **regression reference** (decision 6; [phase0/v032-reference.md](phase0/v032-reference.md)) |
| 46 | 50 dangling blobs + 28 trees | — (no commit) | anchored in `archive/dangling-objects/2026-10-16` | Not assessed: no commit context | — | archive (anchored only) |
| 47 | Ignored local-only files (§3.3) | mtimes 2025-02-28 → 2026-10-15 | never committed | Plans, experiments, captured outputs, the Bash predecessor | — | reference (§3.3). Ranked last: the newest mtime is a whitespace edit to the local config, not work |

### 3.2 P0.1 assessment of the anchored commits

**Method:**
- Every commit was assessed read-only (`GIT_OPTIONAL_LOCKS=0`) from `git archive` extracts in scratch.
- **Builds:** go1.27.2, `GOTOOLCHAIN=local`, `vendor/` removed, `-mod=mod`. `go build ./...` ran for linux/amd64 and darwin/arm64, and `go vet` for linux/amd64.
- **"Landed":** tree and patch-id equality with later commits, plus a closest-commit diff. A unique-line count gives the non-trivial added lines that appear in no later commit on any branch or remote.

**Results:**
- 20 of the 26 build. The 6 that don't are partial captures (files were untracked when stashed) or mid-edit compile errors.
- **24 archive, 2 reference, 0 salvage.** Nothing unique is worth cherry-picking. Everything useful is already on origin/main, the baseline.

| Item | Files touched | Landed? (unique lines that never landed) | Salvageable | Conflicts with the brief |
|---|---|---|---|---|
| 13b3209 | `cmd/hardn/systeminfo_demo/{main,systeminfo_screen}.go` (+148) | Superseded by its amend ba7f1af; the demo directory was deleted in 6025558 (3/104 unique) | nothing | In-house ScreenBuilder/ScreenManager instead of design-system components; `fmt.Sprintf` column padding |
| fc5e551 | 13 files, +51/−1,626: deletes `pkg/system/*` and 4 analysis docs; edits 4 files | Deletions landed in 6e3d08a. The 4 edits (35/39 unique) never landed and are byte-identical in live stash bf0202a | nothing | `ClearScreen` in `SystemDetailsMenu.Show()`; raw ANSI via `pkg/style`; hex-migration lineage (brief §2 rejects it as a base) |
| b371694 | 26 files, +2,034/−743 (`user_options.go` 764, `user_menu.go`, `ssh_keys_*.go`, `os_user_repository.go` +418, service test, `hardn.yml.example` reorder) | Almost entirely in 6092ee3 (9/1,463 unique). **It is the only archived commit that passes the "Allow sudo access?" answer to `CreateUser`** (`user_options.go:647`); 6092ee3 reverted it to literal `true`, which is still at origin/main `user_options.go:744` | **Evidence only:** the intended behaviour honoured the answer, which supports the V22 fix (a one-token change in the user-management slice, not a cherry-pick) | 7 `ClearScreen`; 24 "Press any key" `ReadKey` (stty cbreak, single key); about 82 raw-colour style calls; fixed widths 64/72; `y/n` with no default; mutations interleaved with prompts and not gated by dry-run; imperative switches |
| 1bb6c29 | 25 files, +1,946/−707 | Mostly, via b371694 → 6092ee3 (34/1,401 unique: a `last -1` parser, a "Never logged in" fallback, unchecked `Sscanf`) | nothing | As b371694; the sudo choice is ignored (literal `true`); `last` output parsed by field position |
| 777fa1a | 24 files, +1,630/−711 | Mostly, in 6092ee3 (50/1,176 unique). Unique: a broken `id -u -g -n` call (GNU `id` rejects `-u` with `-g`), diagnostics, draft strings, width 72; the example-config reorder never landed | nothing as code. Idea: the User box's field order matches Figma `01-USER` (origin/main already renders it) | Introduces the forms that ship on origin/main: the `"1000:1000" // Fallback` UID:GID (origin/main `user_menu.go:158,163`) and the create dialog's literal sudo `true` (`user_options.go:375`); screen clears; 16 `ReadKey`; recursive `m.Show()`; sudo toggle without confirmation; invalid input exits the flow; saves to `./hardn.yml`; 217 `style.*` calls |
| 114d9ef | 23 files, +1,337/−683 | Mostly, in 6092ee3 (91/969 unique); its service, test and port code is unchanged on origin/main | none | Fabricated values (the UID fallback; a Directory row built from the username); `userExists` never assigned, so "Create user" is offered for existing accounts; clears; recursion; 9 `ReadKey` |
| db16a33 | 18 files, +1,086/−490 | Partly (92/787 unique: the broken `id` flags, placeholder UID, hard-coded `/home/<user>`, dead branches) | nothing | **First appearance of the fabricated `"1000:1000"` UID:GID fallback** (`user_menu.go:208`); placeholders shown as real state (principle 3); `CreateUser(…, true, …)` with no dry-run gate; 3 clears; 10 `ReadKey` |
| 206a9f2 | 11 files, +807/−489 | Partly (57/593 unique). The SSH-keys loop is on origin/main | none | **First appearance of the hard-coded `ssh-ed25519` / `dark` notice** (origin/main `ssh_keys_menu.go:114`); placeholders; `userExists` unassigned; clears; recursion |
| 0534a08 | 11 files, +819/−487 | Intermediate (95/613 unique) | none | Recursion in the user menu and `ssh_keys_options.go`; 13 `ReadKey`; placeholders; 193 `style.*` calls |
| 1d2e1a5 | 12 files, +854/−493 | Intermediate (133/630 unique). The "Last login: 2023-10-01" and "This is a bottom line example." placeholders move into `ssh_keys_menu.go`; both ship on origin/main (`:62`, `:155`) | none | Recursive navigation at several sites; clears; 9 `ReadKey`; 165 `style.*` calls |
| 90396be | 9 files, +204/−493 | Superseded by 1d2e1a5, which is the complete capture (56/178 unique) | nothing | Clears; raw-ANSI styling; fixed widths; UID `"1000"` placeholder |
| 8efc3ef | 8 files, +284/−131 | Partly (85/206 unique, all in user-menu drafts) | none | **Origin of the unconditional `Dimmed(…)` box border** (origin/main `boxes.go:229,258`; `Dimmed` ignores `UseColors`, `style.go:197-202`), part of the `--no-color` leak; first "This is a bottom line example." |
| 964620d | 7 files, +251/−125 | Everything except `user_menu.go` landed in 6092ee3 (70/177 unique) | nothing | A full `ClearScreen` replaces the System Details header; the `details` → `system-details` rename shipped with no alias (already on origin/main; a note only) |
| bca4b30 | 5 files, +204/−110 | Everything except `user_menu.go` landed (58/139 unique) | nothing | `ClearScreen`; raw ANSI in feature code; fixed width 64; placeholders |
| 12607d8 | 4 files, +198/−109 | Partly (56/135 unique) | Idea only: the earliest code form of Figma `01-USER` | **First appearance of `Last login: 2023-10-01`** (origin/main `ssh_keys_menu.go:62`); `userExists` unassigned; clears |
| 146459b | 354 files (16 outside vendor, +1,265/−485); go.mod +gopsutil/v3 v3.24.5 and 7 indirect modules | Yes, almost entirely, in f9dfe3b after renames (34/1,254 unique, all names) | none | Renderer does I/O (principle 5); fixed 50-column box; clears; a new dependency with no approval trail (principle 8); hard-coded ZFS dataset default |
| 994f03c | Same 354 files; outside vendor it differs from 146459b in 5 files | Yes, via 146459b into f9dfe3b (57/1,257 unique: "machine summary" names) | none | As 146459b. Its index commit holds the **first gopsutil/v3 in the history** |
| 984d938 | `pkg/menu/main_menu.go`, `pkg/style/style.go`, `pkg/utils/utils.go` (+201/−122) | Fully: tree = b5c60d6; patch-id = e308c99 (on origin/main); 0 unique | nothing | Already on origin/main: `ClearScreen` at the top of every main-menu loop; hand-rolled ANSI; a `curl … \| sudo sh` update hint. **Python Packages and Help were dropped from the menu here** (relevant to the dropped-features decision) |
| c57fba2 | Same tree as c3ed213 | No (shares c3ed213's unique lines) | nothing | As c3ed213 |
| 91013d9 | `main_menu.go`, `style.go`, `utils.go` (+262/−119) | Mostly: `style.go`/`utils.go` identical to 984d938; 67 unique lines in `main_menu.go`, mostly commented-out layout experiments | nothing | Screen clear on every loop; raw ANSI in feature code; centring by byte length (`len()`), not display width |
| c3ed213 | 17 files, +561/−475: `pkg/style/style.go`, 14 `pkg/menu/*_menu.go`, `pkg/status`, `pkg/utils` | No (403/505 unique); abandoned within about 75 minutes | Nothing. Latent defects: style helpers call fatih/color's `Add` on shared package-level colours, so one bold header would make every later use bold [V source; effect I]; `PrintPounds` builds a string it never prints; "Risk Level" mistyped as "Risk Label" | A second styling library in feature code (against §2 "Lip Gloss for rendering only" and principles 6 and 7); every new import outside `pkg/ui` would be blocked by the planned depguard rule. **Not a new dependency:** fatih/color v1.18.0 has been in go.mod since the first public commit e4355f8 (`cmd/hardn`, `pkg/logging`) |
| 7e8de67 | 6 files, +926: `cmd/hardn/main.go`, `application/menu_manager.go`, `infrastructure/{menu,service}_factory.go`, `menu/{main,user}_menu.go` | Exactly: tree = 0cbc152 (on origin/main) | nothing | Already on origin/main: single-keypress first-key input; repeated "Press any key" pauses |
| 404f386 | 21 files, +1,564/−75: IOA models, services, ports, adapters, managers; `pkg/testing/compare.go` | Exactly: 08a805a has the same tree, parent and author date (message-only amend) | nothing unique | none of its own |
| cac2e0e | 6 files, +133/−67: `cmd/hardn/main.go`, `pkg/interfaces/{os_network,provider}.go`, `pkg/menu/{linux,run_all}.go`, `pkg/utils/utils.go` | Yes: 8420206 matches except one line, which 8420206 fixes | nothing. Never apply it on its own | Package-level provider singletons (replaced by an injected `System` in `p2-system`) |
| 2aef5d3 | 6 files, +559: `.gitignore`; new `pkg/interfaces/{commander,filesystem,network,provider,mocks}.go` | No, superseded (284/364 unique). bfb66a1/8420206 chose the interface origin/main has | **Ideas for `p2-system`:** record argv as `{Name, Args}` (origin/main's `MockCommander` joins it into one string, `mock_interfaces.go:173-182`, which is ambiguous for arguments with spaces); put `ReadDir` and `LookPath` in the seam; an in-memory directory model for fakes | Anti-patterns to avoid: `Command` returns a real `*exec.Cmd` and `Open`/`Create` return `*os.File`, which defeat fakes; no context, no stderr split, no atomic write |
| b8a0fed | `.gitignore` +1 line (`*.scratch`) | A subset of 2aef5d3; never landed, and moot under the allow-list `.gitignore` | nothing | none |

**Defects on origin/main traced to their origin in this assessment:**
- The fabricated `"1000:1000"` UID:GID fallback: first in db16a33; the commented form on origin/main comes from 777fa1a.
- The ignored sudo answer: the create dialog passed a literal `true` from its first version (777fa1a), b371694 briefly passed the answer, and 6092ee3 went back to `true`.
- The placeholders "Last login: 2023-10-01" (12607d8), "This is a bottom line example." (8efc3ef) and the hard-coded `ssh-ed25519` notice (206a9f2).
- The `Dimmed` box border that ignores `--no-color` (8efc3ef).
- gopsutil/v3 and its 7 indirect modules, added with no approval trail (994f03c).
- The `system-details` rename with no alias (964620d).
- Python Packages and Help dropped from the main menu (984d938/c3ed213 era).

### 3.3 Local-only (ignored) files worth knowing about

- **`.scratch/hardn.sh`** (62K, 2025-03-02): the original Bash implementation (`write_ssh_config`, `configure_ufw`, `setup_apparmor`, `write_proxmox_repos`, …). No identifier from it is in any commit. **Reference for distro knowledge.**
- **`.scratch/hardn_ssh.conf`:** a reference sshd config with Proxmox notes ("PVE ONLY: DO NOT DISABLE", `/etc/pve/priv/authorized_keys`).
- **`.scratch/.unsaved/dry_run_output.md`:** a captured **lockout-class bug**. The UFW dry run allows a different port from the configured SSH port, and prints "configured and enabled successfully!" during a dry run.
- **`.scratch/utils_test.go.scratch`:** five never-committed tests for backup and dry-run behaviour (ideas for 0.7).
- **`.scratch/.unsaved/os_host_info_repository.go`:** a Proxmox-detection fallback (`/etc/pve` stat, then `ls`). Not in any commit.
- **`legacy_pkg/**/*.md`** (30 AI-generated analysis docs, 2025-03-23 → 28):
  - `USER_INTERFACE_ANALYSIS.md` proposes view-models, themes and terminal-capability detection, which fit the brief.
  - It also proposes I/O-performing UI ports, screen clears and raw input, which conflict with it.
  - None of the docs addresses lockout safety, validation, rollback or plan/apply.
- **`adr/004-factory-consolidation.md`** and `MIGRATION.md` (tracked on arch3) cite a "Pragmatic Hexagonal Architecture Guide v1.7" that exists nowhere on disk.
- **`hardn.yml`** at the repo root:
  - A real host configuration with real public keys. It holds no secrets, but it is personal data.
  - It is on the `./hardn.yml` search path, and legacy menus rewrite `./hardn.yml` without its comments. **Never run hardn from the repo root.**
- **`build/`** (75 MB): 0.3.1, 0.3.2 and "1.0.0" binaries, with cascading `.sha256.sha256…` files from the makefile's `checksums` target. `make clean` / `make all` would delete them.
- **`log.txt`:** the only record of the 63 pre-public commits.
- **Files that don't belong in the repo:**
  - `scripts/install.wip.sh` is a third-party installer, not hardn.
  - `.scratch/` also holds an unrelated personal script.
- **`build_demo.sh` and `build_fixed_demo.sh`:**
  - Both `cd` into the second local copy by absolute path and run `go build` there, which writes into that copy.
  - `build_fixed_demo.sh` also *prints* (it does not run) `scp` and `sudo mv` instructions for a remote test host.
  - Don't run them.
- **Font file:** an older local copy holds a commercially licensed font file. Never commit it.

---

## 4. Branch and tag naming scheme (chosen and in use)

**Worktree and branches:**
- **Worktree:** `hardn-next`, a sibling of the old checkout. Its integration branch **`next/v0.4`** was created at 9d46866, which equals `origin/main` after a no-prune fetch.
- **Topic branches:** **`next/p<phase>-<topic>`**, for example `next/p0-allowlist`, `next/p0-findings`, `next/p0-check`, `next/p0-golden`, `next/p0-refresh`, `next/p1a-term`, `next/p2-atomic`.
  - Phase 0's units are stacked and stay unmerged until the Phase 0 review.
  - Later units are reviewed, then merged `--no-ff` into `next/v0.4`.

**Archive tags** (local only; created 2026-10-16 in the old checkout; 75 in total):

| Pattern | Count | Points at | Kind |
|---|---|---|---|
| `archive/2025-03/<branch>` | 7 | branch tips: `box-content`, `hex-migration`, `hex-migration-legacy`, `hex-migration-new`, `linux-packages`, `main`, `refactor-ioa` (for `refactor/ioa`) | lightweight |
| `archive/2025-04/arch3` | 1 | `arch3` (ef0b26a) | lightweight |
| `archive/stash/<yyyy-mm-dd>-<sha7>` | 37 | every live stash's W commit | annotated; the message holds the original `stash@{n}` text and base |
| `archive/dropped-stash/<yyyy-mm-dd>-<sha7>` | 24 | each dropped stash's W commit (its I commit is a parent) | lightweight |
| `archive/reflog/<yyyy-mm-dd>-<sha7>` | 5 | 404f386, ff74d07, b5c60d6, 7a25bb9, 13b3209 | lightweight |
| `archive/dangling-objects/2026-10-16` | 1 | one anchor commit whose tree holds the 50 dangling blobs and 28 trees (entries named `<type>-<sha>`) | lightweight; not a real commit |
| `archive/2025-06/pr-2` | 0 | remote PR #2 head | optional (plan §3.24); not created |

**Pushing** (decision 8): nothing is pushed or published during development. At RC, with approval, only `next/v0.4`'s commits go to `main`, by explicit refspec. **Never `git push --tags`**: that would publish the archive tags.

---

## 5. Baseline build, test, lint and CI status

**Toolchain note:**
- The matrix below was recorded **before** decision 4, with go1.24.0 and `GOTOOLCHAIN=local`, in scratch extracts.
- The local toolchain is now **go1.27.2**, with golangci-lint 2.14.0 and govulncheck 1.9.0. A `mise.toml` in the parent folder pins them, outside every git tree (decision 4).
- `make check` (`next/p0-check`) re-runs these steps under the new toolchain.

| Check | origin/main 9d46866 | v0.3.2 bce7cc2 | arch3 ef0b26a |
|---|---|---|---|
| `go build ./...` from a **clean checkout** (committed vendor) | **FAIL**: `vendor/golang.org/x/sys/unix/syscall_unix_gc.go:11:6: missing function body` | FAIL (same) | FAIL (same) |
| Build with modules (`-mod=mod`/`readonly`); cross-build of the 3 release targets | pass | pass | packages build; **`./cmd/hardn` absent** |
| `go vet` | clean | clean | FAIL (stringintconv, `HostInfoServiceAdapter.go:95-96`) |
| `go test` | 60 tests pass. **`TestHostInfoService` fails off-CI** (it reads the real hostname); passes with `CI=true`. Coverage 5.4% overall | no tests | 2 packages fail |
| golangci-lint 1.64.6 (repo `.golangci.yml`, a **v1 schema**) | 1 gofmt issue (`pkg/security/status.go:94`) | 82 errcheck | 80 issues |
| staticcheck 2025.1 | clean | clean | 13 |
| `go mod verify` / `tidy -diff` | ok / tidy | ok | ok |
| govulncheck v1.1.4 (the newest that runs on go1.24) | **40 reachable**, 11 package-level, 18 module-level | 4 / 10 / 55 | as origin/main |

**First run under go1.27.2** (`next/p0-check`, 2026-10-16) [V]:
- module verify, tidy, build, vet and the 3-target cross-build pass;
- `go test` fails only on `TestHostInfoService`;
- golangci-lint 2.14.0 rejects the v1 config;
- govulncheck 1.9.0 reports GO-2026-5970 (x/text, reachable through `cases.Title`) and **no stdlib findings**.

The anchored-commit builds in §3.2 also used go1.27.2.

**After `next/p0-refresh`** (2026-10-16) [V]: `make check` passes **every step**:
- module verify, tidy, build and vet;
- all tests, with the characterization goldens unchanged (no `-update`);
- golangci-lint 2.14.0 (v2 config): **0 issues**;
- govulncheck 1.9.0: **no vulnerabilities**;
- cross-builds of linux/amd64, linux/arm64 and darwin/amd64.

Linux test runs and Go 1.26.x builds are still pending the VM host.

**Why a clean checkout fails to build** [V]:
- The allow-list `.gitignore` (`*`, then `!*.go`, `!vendor/*` …) leaves 71 files out of the committed vendor tree:
  - 32 `.s`, 3 `.c` and 2 `.h` files;
  - 6 legal files, so **fatih/color, rivo/uniseg and spf13/cobra ship with no license text**;
  - 28 other build files.
- The old checkout builds only because those files sit ignored on disk.
- CI hides the problem: `ci.yml` does `rm -rf vendor`, and the SLSA builder runs `go mod vendor` itself (`builder_go_slsa3.yml@v2.1.0` lines 262-271).
- **Decision 7:** stop committing `vendor/`. The refresh change removes it, and builds use modules.

**Staleness** (as of 2026-10-16):
- **Go:**
  - The supported releases are 1.27 (1.27.2) and 1.26 (1.26.9).
  - go.mod has 1.23.0 / toolchain 1.24.0, the workflows pin 1.21, and v0.3.2 was built with 1.21.13. All are EOL.
- **Dependencies:**
  - The latest x/text, x/sys, x/term, gopsutil/v4 and govulncheck need **Go ≥ 1.26**.
  - fatih/color 1.19 and Lip Gloss v2 need ≥ 1.25.
  - `gopkg.in/yaml.v3` is archived; the maintained fork is `go.yaml.in/yaml/v3`. gopsutil/v3 is frozen.
- **Vulnerabilities:**
  - Building with ≥ 1.26.9 fixes every reachable stdlib finding.
  - The one reachable third-party finding is x/text GO-2026-5970, fixed in x/text ≥ v0.39.

**CI** [V]:
- Two workflows fire on `v*` tags.
- Node16/20-era actions are still pinned (`setup-go@v4`, `golangci-lint-action@v3`, `softprops@v1`). They still run, because hosted runners force them onto Node 24 (seen in other repositories' runs on 2026-10-15).
- The lint step installs golangci-lint v1.64.8 and is non-blocking (`continue-on-error: true`).
- **`download-artifact@v3` has been blocked on github.com since 2025-01-30**, and it can't read the builder's v4 artifacts. So release.yml's signing job cannot run, and it never ran for a release.
- `.slsa-goreleaser.yml`'s `archives`/`checksum`/`signs` keys are silently ignored by the builder. The `main.TreeState` ldflag targets an undeclared variable.
- No CI run history is retrievable (`total_count: 0`).

**Supply chain** [V]:
- **slsa-github-generator:** v2.1.0 is its final release. Since 2026-08-07 its README says it is no longer actively maintained. slsa-verifier is also unmaintained.
- **`install.sh`:**
  - It verifies nothing and fails open (`curl -L` without `-f`).
  - armv7l installs the arm64 binary.
  - It runs the unverified binary as root, relying on `hardn --version` to create the config (`install.sh:63-65`).
- **Verification paths are all broken:**
  - `verify.sh` and the makefile use slsa-verifier v1 flags.
  - The README points to a nonexistent `scripts/verify.sh`.
  - The cosign identity regexp never matches.

**Release-tooling hazard** (new; found by `next/p0-check`, then confirmed) [V]:
- The makefile's first target, and so its default goal, is `tag` (origin/main `makefile:18-20`): `git tag -a v$(VERSION)` followed by `git push origin v$(VERSION)`.
- Today `VERSION=0.3.2` and that tag already exists, so the first line fails and make stops before the push [I: make stops at the first failing recipe line].
- Once `VERSION` changes, a bare `make` would tag and **push**.
- **Gated** in `next/p0-check` (commit 16fd553) under decision 8: `.DEFAULT_GOAL := check`. `make tag` and `make release` are unchanged and still work when named explicitly. Presented for review with the other Phase 0 units. See R3.

**Proposed refresh:** `next/p0-refresh`, built on `next/p0-golden` and not merged without approval (it passes the full `make check`; see above):
- a go.mod floor of 1.26;
- dependency bumps, plus the `go.yaml.in/yaml/v3` and gopsutil/v4 path changes, which need new-dependency approval;
- a v2 lint config;
- `vendor/` removed.

---

## 6. Architecture map (origin/main, the worktree base)

```
cmd/hardn/main.go  ── main(): InitLogging(/var/log/hardn.log) + EnsureExampleConfigExists(/etc/hardn/...)  ← BEFORE flag parsing
   └─ cobra rootCmd
       ├─ setup-sudo-env, system-details (pkg/cmd), help, completion
       └─ Run: --version | root check | LoadConfig(-f) | cfg.DryRun = flag | DetectOS (clears screen)
            ├─ op flags -r -d -c -w -g -e -p  ──► managers DIRECTLY (no dry-run guard)
            └─ no op flags ──► MenuFactory.CreateMainMenu ──► CheckForUpdates (2 goroutines) ──► ShowMainMenu

menu.<Screen>.Show() ─► application.MenuManager (facade over 10 managers)
                     ─► application.<Area>Manager
                     ─► domain/service.<Area>ServiceImpl      (1-line pass-throughs)
                     ─► adapter/secondary.<Impl>              (implements port/secondary.<Area>Repository; ports declared TWICE)
                     ─► interfaces.FileSystem / interfaces.Commander (+ MockFileSystem / MockCommander)
                     ─► os / os/exec
Bypasses (direct exec/os): main-menu status (pkg/security/status.go), disable-root restart, DNS screen reader,
pkg/system collectors, user menu os/user lookups, config su-probe, osdetect, logging, version checker.
```

- **Layout vs brief §1:**
  - The brief's package list matches **v0.3.2** exactly.
  - On origin/main, `pkg/{packages,backup,user,status,dns,firewall,ssh}` no longer exist; the IOA adapters replaced them.
  - The old checkout's HEAD (arch3) matches neither. Its `legacy_pkg/` is origin/main's code relocated.
- **Seams for Phases 2–3:**
  - `interfaces.Commander` (`Execute` / `ExecuteWithInput`; combined output, no context).
  - `interfaces.FileSystem` (no `Lstat`, `Rename`, `Chown`, `ReadDir` or atomic write).
  - `MockCommander` (records argv as one space-joined string) and `MockFileSystem`.
  - All hardening **mutations** go through these seams except one direct sshd restart. Main-menu **status** goes through none of them. Overall about 55–60% of system access is seamed [I].
- **Dead or unreachable on origin/main:**
  - `SetupAppArmor`, `SetupLynis`, `SetupUnattendedUpgrades`, `UpdateSystem`, `utils.BackupFile`, `SetupHushlogin`, and all of `BackupManager`'s backup, restore and cleanup functions;
  - the Linux/Python packages, Sources and Help menus, and the package adapter;
  - `--debug-updates`.
  - So **AppArmor, Lynis and Auto Updates cannot be applied at all on origin/main.**
- **Dry-run** [V]:
  - v0.3.2 used per-function `cfg.DryRun` checks in leaf packages, as the brief guessed.
  - origin/main checks only in menu handlers (about 31 sites), and **zero** in the IOA layers. So the CLI ignores `-n` entirely, and the User Management menu mutates in dry-run (only the success message is gated).
  - The YAML `dryRun` key is always overwritten by the flag default (`main.go:152`; v0.3.2 `main.go:142`).
- **Config:**
  - The 6-step search order in brief §1 is **confirmed**.
  - There's no fallback after a missing `-f`/`HARDN_CONFIG` path.
  - With no config found, an untyped `[Y/n]` prompt (EOF counts as yes) writes `/etc/hardn/hardn.yml`, and an empty username becomes **"george"**.
  - Unknown keys are silently ignored, so the renamed `disableRoot` key is dropped without a warning.
  - **17 `SaveConfig(…, "hardn.yml")` call sites** (15 reachable, across 7 screens; v0.3.2 had 4) write **`./hardn.yml` in the current directory**, not the loaded file.
  - `permitRootLogin` is a bool, so it can't express `prohibit-password`.
  - Under sudo, `DetectEnvVarLoss` runs `su - $SUDO_USER -c 'echo $HARDN_CONFIG'` on every config load.
- **Logging:**
  - The path is hard-coded as `/var/log/hardn.log`, in `log.LstdFlags` + `LEVEL:` format, with no rotation.
  - The `logFile` key is only used for *reading* logs.
  - A global silent mode is switched on during the user-info lookup and never restored.
- **Backups:**
  - v0.3.2 backed up before 15 writes to `<backupPath>/<date>/<basename>.<HHMMSS>.bak`. It kept only the basename, ignored errors and had no restore path.
  - **origin/main backs up nothing.** `enableBackups` is a no-op, and neither ref has a restore path.
- **osdetect:**
  - It accepts only the `debian`, `ubuntu` and `alpine` IDs; `ID_LIKE` is not read.
  - Proxmox = `/etc/pve` exists.
  - There is no container detection, no init-system detection and no release gating.
  - The fallback is a fabricated "debian 11".
- **Update checker (origin/main only):**
  - It makes an unauthenticated GitHub API call on every interactive start, from two racing goroutines.
  - It caches as root in the predictable `/tmp/hardn-version-cache.json`.
  - A newer release whose name matches `security`/`[security]`/`cve-`, or whose body matches `security`/`cve-`/`vulnerability`/`exploit`, **replaces the menu and exits hardn**. There is no opt-out.
- **Tests:** 60 pass-through service tests plus one host-info test. No adapter, menu, config or status tests, and no golden files.

---

## 7. Side-effect inventory (summary)

Full detail is in [phase0/side-effects.md](phase0/side-effects.md): the side-effect table per distro (exact argv, paths, modes, dry-run, validation, backup, lockout), the harness evidence and V1–V27.

**Decision 6:** the V-list behaviours are **defects to fix**, not behaviour to preserve. Characterization goldens record them, labelled `KNOWN-DEFECT: Vn`.

| Area | What origin/main does | Lockout-relevant defects |
|---|---|---|
| **Startup** | Writes `/var/log/hardn.log` and `/etc/hardn/hardn.yml.example` before flag parsing (even `--help`); runs the `su - $SUDO_USER` probe; calls the GitHub API; writes a `/tmp` cache; runs `stty raw` per keystroke | `install.sh` relies on `hardn --version` writing the config |
| **SSH** | `os.WriteFile` of `/etc/ssh/sshd_config.d/hardn.conf` (Debian/Ubuntu/PVE) or of the **whole `/etc/ssh/sshd_config`** (Alpine); then `systemctl restart ssh` / `rc-service sshd restart` | **No `sshd -t`/`-T`, no reload and no backup** on any ref. **Disable-root rebuilds the config from a stub** (`Port 22`, key-only). **`-c` sets a global `AuthorizedKeysFile .ssh_%u`** while keys go to `~/.ssh`. Key-only auth is forced with no check that a key works. `hardn.conf` sorts after `50-cloud-init.conf`. **No `ssh.socket` handling** (v0.3.2 wrote `ssh.socket.d/listen.conf`, but without `daemon-reload`) |
| **Users/sudo** | `adduser`/`usermod -aG sudo` (Debian) or `adduser -D` + `wheel` (Alpine); writes `/etc/sudoers.d/<user>` 0440 **without `visudo`**; appends keys via `su - u -c 'cat >>'` | Alpine never gets `sudo` installed; sudo is passwordless or nothing; the sudoers overwrite drops setup-sudo-env's `env_keep`; "remove key" doesn't remove the key; `sh -c "chown -R <user>…"` with an unvalidated username |
| **UFW** | `default deny` → `disable` → `reset` (no `--force`) → app profiles → `allow <cfg.SshPort>/tcp` → `yes \| ufw enable` | Allows the *configured* port, not the effective one. Without `--force`, upstream ufw reads stdin and treats EOF as "Aborted" with exit 0, so with stdin=/dev/null `reset` is **probably a silent no-op** [I, from ufw source]. A failure after `disable` leaves the firewall off with no rollback. **Enabled on Proxmox** |
| **DNS** | systemd-resolved: overwrite `resolved.conf` + restart; resolvconf: `head` + `-u`; otherwise write through `/etc/resolv.conf` (follows a symlink) | An empty nameserver list is accepted (`DNS=`); the `lan` domain is forced; NetworkManager is not detected |
| **Run All** | user → SSH (`PermitRootLogin no`, forced) → UFW only if `enableUfwSshPolicy` (default false) → DNS | The port changes before the firewall allows it; no Proxmox gating; **prints "AppArmor configured" / "Lynis … completed" for steps that never run** |
| **AppArmor, Lynis, auto updates, packages, sources, hushlogin** | Dead code on origin/main | v0.3.2 had them live; v0.3.2's `-c` also rewrote apt sources and wrote Debian repos onto Ubuntu |
| **Config** | `./hardn.yml` rewritten (comments lost) from 7 screens | — |

**V-list (lockout-principle violations):**
- **Critical:**
  - V1 CLI `-n` ignored;
  - V4 disable-root stub;
  - V5 global `AuthorizedKeysFile`;
  - V6 key-only with no proof that a key works;
  - V8 port changed before the firewall allows it;
  - V9 UFW disable/reset ordering, allowing only the configured port;
  - V10 UFW on Proxmox;
  - V11 `PermitRootLogin no` on Proxmox;
  - V14 no sudo on Alpine;
  - V24 the v0.3.2 → origin/main upgrade path (keys in `~/.ssh_<u>` stop working; `manage.conf` and `listen.conf` left behind).
- **High:** V2, V3, V7, V12, V13, V15–V18, V23.
- **Medium/low:** V19–V22, V25–V27.
- **Every Phase 2 primitive is new work.** No ref contains `sshd -t`, a sshd reload, `os.Rename`, `00-hardn`, `pve-firewall`, `prohibit-password`, `apk-cron` or `systemd-run`. This was re-verified across all 15 refs, every stash commit and parent, and the reflog commits.

**Verified by a second, adversarial harness pass** (all 15 claims held):
- Under `DryRun=true`, `-r` still wrote `hardn.conf`, the sudoers file and `resolved.conf`, and ran 17 commands.
- Disable-root on Alpine replaced a seeded `Port 2222 / ListenAddress / AllowUsers` with the 15-line stub, **including no `Subsystem sftp`**, so sftp breaks.
- "Remove key" left both keys in place.
- `ConfigureSudo` erased the `env_keep` line.
- Menu input spun 368 iterations on EOF in 2 s.
- **An unprivileged user can plant `/tmp/hardn-version-cache.json`**, which makes root's hardn show "Update Binary <attacker URL>" and exit.
- Root also writes the fixed `/tmp` names `hardn-version-cache.json` and `hardn_sudoers_temp` with symlink-following `os.WriteFile`.
- The CLI exits 0 after operation failures (V25).
- `--version` on release builds prints only cobra's `hardn version X`; v0.3.2 also printed the build date and commit.

**After the review (P0.8, 2026-10-17):** V1, V4, V5 and V24 for `-c`, V10, V11, V26 and A4 are gated off, each behind a named constant with the old code kept; see [p0-hazards.md](p0-hazards.md).

---

## 8. Interaction audit (summary)

Every screen, menu and prompt, with its inputs, outputs and navigation, is in [phase0/interaction.md](phase0/interaction.md).

**Input model:** no ref is Enter-based, so **the brief's Enter-everywhere decision changes behaviour relative to v0.3.2 too**, not only relative to Figma.

| Ref | Selection input | Effect |
|---|---|---|
| origin/main | `stty -F /dev/tty raw -echo` and `sane` **per keystroke** | `q` quits with no Enter; Ctrl-C (0x03) is dropped; EOF loops forever; "press any key" uses cbreak. `stty -F` is GNU-only, so it fails on macOS |
| v0.3.2 | cbreak first key, then a line | — |
| arch3 | `term.MakeRaw` | `q` anywhere quits; an empty Enter runs option 1 |

There is no typeahead discard and no `signal.Notify` on origin/main. `pkg/menu/input.go` mixes a shared `bufio.Reader` with direct `os.Stdin` reads, so buffered bytes can be lost [I, static reading].

**The 11 anti-patterns (brief 0.5) on origin/main:**

| # | Anti-pattern | Present? | Scale / example |
|---|---|---|---|
| 1 | Prompting and executing interleaved | yes | Firewall/DNS apply on selection; passwordless sudo with no confirmation; HardenSystem has no rollback |
| 2 | Clears wipe output | yes | 8 wipe sites |
| 3 | "Press any key" | yes | 32 strings, 55 `ReadKey` call sites, 23 `PressAnyKey` (Figma itself uses it in 4 frames) |
| 4 | Invalid input restarts the flow | yes | 5 flows |
| 5 | Hard-coded hints | mostly no | `GetValidRange` derives hints, but dispatch is a separate `switch`, so **hidden options stay selectable** |
| 6 | Imperative switches | yes | 15 switches, 61 arms |
| 7 | Inconsistent back/quit | yes | `q` quits at the top level and goes back below it; `b` can't be typed; 4 different exit labels |
| 8 | Inconsistent confirmations | yes | `(y/n)` ×10 with no default, `[Y/n]`, `[y/N]`, a typed `yes`; several high-impact actions have **none** |
| 9 | Raw subprocess output | mostly no, but inverted | Output is discarded; errors carry only "exit status N" |
| 10 | Scattered styling | yes | 1,029 `style.*` references in reachable `pkg/menu` code (1,462 in total) plus 445 `fmt.Print*`; `Bolded`/`Dimmed` ignore `--no-color`; `NO_COLOR` is overridden in `main.go`; widths measured with `len()` |
| 11 | Status computed in several places | yes | At least 6 sources, and they disagree |

**Worse than brief §7 assumes:**
- **Run All option 1 runs live with no confirmation** (`7`, Enter, `1`, Enter).
- The "Minimal/hardened" risk band is unreachable: there are 8 equal booleans, and the firewall counts twice.
- The Dry-Run menu in its Enabled state is never printed.
- "Toggle SSH root access" can only disable.
- The security-update alert can lock users out of the UI.

**Main menu:**
- origin/main matches Figma `00-TUI-MAIN` almost 1:1. **v0.3.2 does not**: it has 12 different items.
- Differences from Figma:
  - "Users" vs "User";
  - an extra "Sudo Method" row;
  - "Toggle SSH root access" vs "Toggle root SSH access";
  - the prompt "Enter selection" vs Figma's "Enter your choice";
  - a shade-glyph distro bar;
  - Proxmox shown as Debian.
- **Figma's glyphs are ⛛ (U+26DB), ⭘ (U+2B58) and ⛬ (U+26EC)**, not the brief's ▽ ○ ∴.
  - They are origin/main's own `pkg/style` constants (since 5189b6f).
  - All three are East Asian **Ambiguous** width, as are ≈, ≥, ‘ ’, ▌ █ ─ and the brief's ▽ ○ ∴. ❯ and ░ are narrow.

**Screens and Figma frames:**
- There are 11 frames, with filenames exactly as the brief expects, plus 2 legacy-TUI screenshots (DNS, Firewall) of the pre-607eafa UI.
- **Screens with no frame:**
  - DNS, Firewall and app profiles, Dry-Run, Run All, Logs: infer them from v0.3.2 and note origin/main's changes.
  - **Environment** and **System Details**: they don't exist in v0.3.2, so infer them from origin/main.
  - Also: User Management variant B, key removal, the passwordless direction, result and failure states, main-menu variants and the pre-menu prompts.
  - **AppArmor, Auto Updates, SSH Auth and SSH Port have no screen on any ref.**
  - Brief-required screens with no frame: plan preview, step progress, error report, confirm countdown.
- **Figma drift:**
  - SSH Login shows status "root disallowed" next to "Root SSH Login Enabled".
  - Backup has copied-in status rows.
  - Create User asks "Require sudo password?", the opposite polarity to the code.
  - There are stale hidden "UID ≥ 1000" nodes.

### 8.1 Figma export

**Location and provenance:**
- It sat untracked inside the old checkout and was never committed on any ref, stash or reflog [V].
- It has since moved out of the repo to the sibling `_design/` folder (decision 3).
- `next/p0-allowlist` narrows `!README.md` to source and doc trees, so a future copy stays ignored.
- It was extracted from `hardn-v040.fig` by fig-extract. The tokens came from a version with an uncommitted change, so the export cannot be regenerated exactly.

**Format and blind spots:**
- **Format:** custom fig-extract inferred-token JSON. It is not W3C DTCG (no `$value`/`$type`), not Tokens Studio and not CSS variables. The `.fig` has no components and no variables, so every token is inferred from raw values.
- **Blind spots:**
  - underline is not captured (it is visible only in the PNGs, e.g. "Select an option");
  - inline bold runs are not captured;
  - hidden nodes appear in `texts.txt` but not in the trees;
  - the main-menu divider is collapsed into an SVG.

**Tokens:**

| Proposed Go token | Figma value | Notes |
|---|---|---|
| `text.primary` | #C0C4D8 | |
| `text.muted` | #81869C | |
| `text.faint` | #2B2F3B | **1.57:1 on black: nearly invisible.** Deviation candidate |
| `status.ok` / `status.bad` / `status.warn` | #70D47F / #E74669 / #DBB853 | |
| `badge.bg` / `badge.fg` | #0C2AC0 / #DFDFDF | `badge.fg` merges the host badge's #C0C4D8 |
| `bar.bg` | #101114 | |
| `accent.ok` / `accent.warn` | #008A00 / #D2BC13 | callout accent bars |
| `divider` | #16181D | **1.18:1** on black. Deviation candidate |

- Proposed additions: `status.unknown`, `status.na`, `status.partial`, `accent.bad`, `accent.info`.
- The dark palette fails on white (primary 1.73:1), so the light theme needs its own values.
- The 14 drift items and the 14 candidate deviations are logged in [../design-system/deviations.md](../design-system/deviations.md#candidates).

**Geometry:**
- 1 cell = 19.2 px; content width = **64 cells**, which equals origin/main's `style.NewBox(Width: 64)`.
- Indent col 2; marker cols 2–4; label col 6 (width 21); **value col 27**; one blank line between sections.
- The brief's transcription puts the value column at col 33. It is not a reliable geometry source.
- **Height:** the main menu is about **36 lines**, so it scrolls on 80×24 consoles (serial consoles, the Proxmox web console).

**Components Figma uses that brief §1B lacks:**
- TitleBar (sub-screens have no Header or Breadcrumb);
- SectionHeader;
- MenuHeading (underlined);
- Callout (ok, warn, cancelled);
- SubjectLine;
- BulletList;
- AnsweredField;
- a muted Exit/Return item;
- values with semantic state ("Enabled" is green in one place and red in another).

**Figma bakes in patterns the brief rejects:**
- "Press any key to continue...";
- `(y/n)` with no capitalised default;
- **a lockout-risk SSH toggle confirmed with a plain y/n**;
- collect, confirm and result on one screen.

Mocks split these into the canonical flow, and the departures go to [proposals.md](proposals.md).

---

## 9. Timing table (deferred)

**Status: written, not run** (decision 5).
- Brief 0.6 can't run on this machine: there's no Docker or VM, and running hardn here is forbidden.
- The maintainer will provide a Debian VM on Proxmox when asked, and the harness runs there.
- **origin/main's `-n` applies changes**, so the timing runs use apply mode in disposable VMs, one case per snapshot, never `-n`.
- Each case runs detached inside the VM and writes its timings to a file. `-c`, `-d` and `-r` can cut SSH, so results are collected through the console or the disk.

| Distro (VM) | Ref | `-c` | `-w` | `-g` | `-d` | `-r` |
|---|---|---|---|---|---|---|
| Debian 13 | v0.3.2 / origin/main | not run | not run | not run | not run | not run |
| Ubuntu 26.04 | v0.3.2 / origin/main | not run | not run | not run | not run | not run |
| Alpine 3.24 | v0.3.2 / origin/main | not run | not run | not run | not run | not run |
| Proxmox VE 9 (nested, by hand) | origin/main | not run: origin/main enables UFW and sets `PermitRootLogin no` there (V10, V11), so only after the P0.8 Proxmox gate | | | | |

Decision 5 covers one Debian VM so far. The Ubuntu and Alpine rows need further VMs, which the maintainer would be asked for when that work is due.

---

## 10. Verify list (brief §9)

| Item | Answer | Status |
|---|---|---|
| Local repo: branches, stashes, uncommitted, local-only commits on `main` | §2, §3. No local-only commits on `main`; 4 local-only branches; 37 stashes; 24 dropped stashes and 5 reflog-only commits (all now anchored) | **V** |
| `design/hardn-v040/`: location, format, screens, tracked? | It was at the old checkout's root, untracked, and never committed on any ref, stash or reflog; `README.md` was re-included by `!README.md`. Now moved out of the repo (decision 3). Format: custom fig-extract inferred-token JSON (§8.1). Screens: 11 frames plus 2 legacy screenshots (§8) | **V** |
| `go.mod`: Go version, direct deps, TUI lib, x/crypto | `go 1.23.0` / `toolchain go1.24.0`. Direct deps: fatih/color, go-runewidth, gopsutil/v3, cobra, testify, x/text, yaml.v3. No Viper (the README credit is stale), no TUI or prompt library, no x/crypto. arch3 adds x/term | **V** |
| `vendor/` committed? | Yes, but incomplete: 71 files missing, and fresh builds fail (§5). Decision 7: stop committing it | **V** |
| Baseline build/test/lint; CI | §5. ci.yml build and test would run (node16/20 actions are forced onto Node 24); lint is non-blocking. release.yml's signing job cannot run (download-artifact@v3 is blocked). hardn has no retrievable CI runs | **V** |
| `install.sh` checks | None; it fails open (§5) | **V** |
| Workflows / SLSA: builder version, targets, trigger | `builder_go_slsa3` v2.1.0 (final, unmaintained); linux/amd64, linux/arm64, darwin/amd64; a `v*` tag push fires **two** workflows | **V** |
| How dry-run works | §6 | **V** |
| Backup system | §6: v0.3.2 backed up by basename only, with no restore; origin/main backs up nothing | **V** |
| Log location and format | §6: `/var/log/hardn.log`, `log.LstdFlags` + `LEVEL:`, no rotation; `logFile` is only read | **V** |
| Unreleased changes on origin/main since v0.3.2 | 43 commits:<br>• flags `-l -i -a -s` removed (now hard errors);<br>• `-e`, `--no-color`, `--debug-updates`, `--test-update`, `--test-security-update`, `setup-sudo-env` and `system-details` added;<br>• cobra auto-adds `completion`/`help`;<br>• `disableRoot` → `disableRootSSH` with no alias (silently ignored);<br>• `sshConfigFile` ignored;<br>• "george" default;<br>• `--version` lost the build date and commit;<br>• AppArmor, Lynis, auto updates and packages unreachable;<br>• update checker;<br>• the test hooks `HARDN_FORCE_UPDATE`/`HARDN_FORCE_SECURITY` ship in production;<br>• Sigstore job (never ran) | **V** |
| sshd `Include` per distro | It is the first directive on Debian 12/13, Ubuntu 22.04/24.04/26.04, PVE 9 and Alpine ≥ 3.19 (Alpine hosts upgraded from ≤ 3.18 may lack it). First value wins, **except Port, ListenAddress, HostKey, Allow/Deny Users/Groups and AcceptEnv, which accumulate**. Include order is C byte order, so `00-hardn` sorts before `000-x` | **V** (primary sources and local sshd experiments) |
| `ssh.socket` per Ubuntu version | 22.04: `ssh.service` (a 24.04 upgrade migrates it). 24.04/26.04: socket by default, with a generator. Port changes need `daemon-reload` + `restart ssh.socket`; a reload can't change the port. Debian/PVE: `ssh.service` (socket is opt-in, with no generator) | **V** |
| Service names / init | Debian/Ubuntu/PVE: `ssh` (the `sshd` alias is missing on fresh 24.04). Alpine: OpenRC `sshd` (`rc-service sshd reload` runs `sshd -t`) | **V** |
| `apk-cron` | In the main repo. `/etc/periodic/daily/apk` runs `apk -U upgrade` (no `--available`), daily at 02:00 plus up to 2 h of random sleep. Needs `crond` enabled. Logs to `/var/log/apk.log`. A kernel upgrade needs a reboot, which it doesn't handle | **V** |
| Proxmox join / detection | The default `pvecm add` and GUI join use the API with the root password and fingerprint; **no SSH password login is needed**. The exceptions are `--use_ssh` and qdevice setup. Detect PVE via the `pve-manager` dpkg status or `/usr/bin/pveversion`; `/etc/pve` stays as an empty directory when pmxcfs is down. PVE writes `PermitRootLogin yes` to the main file, `authorized_keys` is a symlink into `/etc/pve/priv`, and inter-node SSH uses port 22 | **V** |
| Lip Gloss APIs (chosen major) | v2, `charm.land/lipgloss/v2` v2.0.6 (Go ≥ 1.25):<br>• `colorprofile.Detect`/`Writer` (downsampling happens at write time);<br>• `lipgloss.Complete`;<br>• `HasDarkBackground` (raw mode with ISIG off, up to 4 s; opt-in only);<br>• `LightDark`;<br>• `Width` (ambiguous = 1 cell; the only override is an env var);<br>• `table`/`list`/`tree`.<br>No Bubble Tea. `/compat` queries the terminal at import | **V** (source of v2.0.6 and colorprofile v0.4.3) |

**Still unverified until they run on a host** (decision 5): ufw `reset` with stdin=/dev/null; whether sessions survive an `ssh.socket` restart; sshd drop-ins on Alpine cloud images; sudo-rs with `env_keep` on Ubuntu 26.04.

---

## 11. Risks

| # | Risk | Severity | Mitigation / status |
|---|---|---|---|
| R1 | Loss of local-only history | critical → **mitigated** | Decision 1: anchors plus gc guards; fsck reports nothing unreachable. Residual: a manual gc still expires the non-stash reflog entries (§2.2). Never stash in the worktree |
| R2 | The worktree base has active lockout hazards, and **its `-n` is not a dry run** (§7). A VM test that trusts `-n` can lock the VM | critical | Never run hardn on this machine. Disposable VMs only. P0.8 hazard gates (named constants, old code kept) after the review |
| R3 | A bare `make` ran the `tag` target, which pushes a tag once `VERSION` changes (§5) | high → **mitigated** | `next/p0-check` sets `.DEFAULT_GOAL := check` (tag/release targets kept, still callable by name). Never run `make tag`, `make release` or `make all` during the refactor (decision 8) |
| R4 | The allow-list `.gitignore` silently drops new work (goldens, testdata, JSON fixtures, schemas, new scripts, root `AGENTS.md`) | high → reduced | `next/p0-allowlist` re-includes testdata, goldens and docs PNGs, and narrows `!README.md`. Stage by path; run `git status --ignored` in every review |
| R5 | A fresh checkout doesn't build with the committed vendor | high | `-mod=mod`/`readonly` until the refresh removes `vendor/` (decision 7) |
| R6 | Unreleased surface changes (removed flags, the renamed key, the update kill switch, `--version`) hit v0.3.2 users at the next release | medium | Ratify each one; deprecation **warnings** for renamed or removed forms (brief §4) |
| R7 | EOL toolchain and reachable vulnerabilities | medium | go1.27.2 locally (decision 4); go.mod floor 1.26 in the refresh; x/text bump fixes GO-2026-5970 |
| R8 | The v0.3.2 → v0.4 upgrade breaks SSH (keys in `~/.ssh_<u>`, `manage.conf`, `listen.conf`) | critical | P0.8 gate for `-c`; sshd migration unit; a VM test on a v0.3.2-hardened image |
| R9 | Proxmox breakage (UFW enabled, `PermitRootLogin no`, a port change, renaming over the symlinked `authorized_keys`) | critical | P0.8 Proxmox gate; Proxmox rules in Phase 2; a manual PVE checklist |
| R10 | `install.sh` goes live from `main` at the RC push and fails closed against v0.3.2, which has no checksums | high | A two-step RC push: release first, then the `install.sh` commit |
| R11 | The SLSA generator is frozen and may break on newer runners | medium | Decide between the frozen generator and GitHub attestations at Phase 7 |
| R12 | Personal data and real-host values leak into goldens, screenshots or committed docs | medium | Synthetic fixtures only; these docs are redacted. The root `hardn.yml` and the brief's main-menu transcription contain real host values |
| R13 | A second local copy with identical history exists. An editor was auto-fetching it during the audit, and decision 1's anchors and gc guards were applied to this checkout only | medium | It is not the backup, and no work happens in it; all work happens in `hardn-next` |
| R14 | Server-only claims reported as tested | medium | "Written, not run" until the VM exists (decision 5) |
| R15 | Figma fidelity traps: near-invisible grays (1.57:1, 1.18:1), ambiguous-width glyphs, contradictory mock data, a ~36-line menu | medium | Deviation log; ASCII markers in CJK locales; a height rule; synthetic fixtures |
| R16 | `makefile` is the same file as `Makefile` on this case-insensitive volume | low | Extend the tracked lowercase `makefile` (as `next/p0-check` does) |
| R17 | `GOTOOLCHAIN=auto` silently downloads a toolchain once go.mod says 1.26 | low | `GOTOOLCHAIN=local` for every go command; `make check` sets it |

---

## 12. Proposed adjustments to the brief

Each needs the maintainer's OK before the brief is edited. "Resolved" means a decision of 2026-10-16 already settles the substance, so only the brief's wording is left to update.

| # | Brief section | Adjustment | Status |
|---|---|---|---|
| 1 | §1 Layout | Describe origin/main's IOA layout; the old checkout's HEAD (arch3) matches neither | proposed |
| 2 | §1 Dependencies | Drop Viper; add go-runewidth, gopsutil/v3 and testify. fatih/color is real (direct since e4355f8) | proposed |
| 3 | §1 Releases | No release has carried Sigstore signatures; only the provenance DSSE envelopes are Sigstore-signed | proposed |
| 4 | §1 Versions | origin/main, not v0.3.2, is the layout that matches Figma | proposed |
| 5 | §1 Design assets | Screens absent from v0.3.2 (Environment, System Details, User Management variants) are labelled "inferred from origin/main (unreleased)" | proposed |
| 6 | §1 transcription | Glyphs are ⛛ ⭘ ⛬. Take geometry from the Figma trees (value col 27, 64-cell content width), not the transcription (col 33). The host badge text is #C0C4D8 bold; the distro text sits on a black knockout; there are two grays. Replace the real host values in the transcription with placeholders | proposed |
| 7 | §1 Repository state | Activity continued until 2025-11; remote PR #2 exists; a second local copy exists | proposed |
| 8 | §4 / 0.1 Preserve | Anchor dropped stashes, reflog-only commits and dangling objects; set gc guards before any fetch or worktree commit; forbid `git stash` in the worktree; run read-only git with `GIT_OPTIONAL_LOCKS=0` | **resolved** (decision 1, done) |
| 9 | §4 Backup | "Ask where it is and when it was taken, and record it" becomes "location intentionally not recorded" | **resolved** (decision 2) |
| 10 | §1 Design assets | The `.git/info/exclude` advice doesn't work in this repo: the allow-list's `!README.md` (and arch3's `!/*.md`) rules take precedence. Never copy the export into a tree; `next/p0-allowlist` narrows `!README.md` | **resolved** (decision 3: export moved out, no hook) |
| 11 | 0.3 | A clean-checkout build failure is a repo-hygiene defect, not staleness; the fix is part of the refresh | proposed |
| 12 | Phase 7 "Vendor … the same way existing ones are handled" | Stop committing `vendor/`; builds use modules; the SLSA builder vendors for itself | **resolved** (decision 7) |
| 13 | 0.6 / 0.7 | Never use origin/main `-n` as a dry run. Goldens come from Go test harnesses over `MockCommander`/`MockFileSystem`, never from running the binary. Menu dry-run screens need an input seam or VM captures | proposed |
| 14 | §3 principle 2 | "Preserve behaviour" names a baseline: origin/main for wording and behaviour; V-list = defects; v0.3.2 = the reference where origin/main regressed | **resolved** (decision 6) |
| 15 | Phase 1 | The Lip Gloss major and the Go floor are coupled, so the refresh comes first | proposed |
| 16 | Phase 6 | `make check` goes in the existing lowercase `makefile`; the first worktree commit amends the allow-list | proposed (allow-list done) |
| 17 | §1B components | Add TitleBar, SectionHeader, MenuHeading, Callout, SubjectLine, BulletList and AnsweredField. Figma sub-screens have no Header or Breadcrumb, so keeping them is a proposal ([P-08](proposals.md#p-08)) | proposed |
| 18 | §1A width rules | Add a **height** rule: the main menu is about 36 lines and scrolls on 80×24 consoles | proposed |
| 19 | §1A glyphs | Every status and risk glyph, plus ▌ █ ─ and ≈, is ambiguous-width, not only ▽ and ○. Use ASCII markers in CJK locales | proposed |
| 20 | §2.2 example | `000-…` is the wrong example. sshd sorts in C byte order (`00-hardn` < `000-x`), so use `0-*` or `00-a…` | proposed |
| 21 | §2.2 first-value-wins | Add the accumulating keywords (Port, ListenAddress, HostKey, Allow/Deny Users/Groups, AcceptEnv); the port check compares the whole set | proposed |
| 22 | §2.2 `sshd -t -f <candidate>` | On a fragment it checks only the fragment, so validate the assembled config. As root on Debian/Ubuntu it also needs `/run/sshd` | proposed |
| 23 | §2.3 steps 3–4 | On Ubuntu 24.04/26.04, `reload` can't change Port and fails on an inactive unit; port changes need `daemon-reload` + `restart ssh.socket`. 22.04, Debian and PVE are not socket-activated | proposed |
| 24 | §2.3 step 7 | Never call an `sshd` unit on Ubuntu: the alias is missing on fresh 24.04 | proposed |
| 25 | §2.2 sudoers | Ubuntu 26.04 uses **sudo-rs**, with the same filename rule | proposed |
| 26 | §2.2 / §2.5 Alpine | sudo isn't installed by default; the default is **doas** | proposed |
| 27 | §2.5 DNS | A regular `/etc/resolv.conf` can still be managed (Alpine's udhcpc, Debian's DHCP clients, PVE). In `resolved.conf.d`, the **last**-sorting file wins | proposed |
| 28 | §2.5 Proxmox | The join `[Verify]` is resolved: no SSH password login is needed (`--use_ssh` and qdevice are the exceptions). PVE's own `PermitRootLogin yes` isn't drift. `authorized_keys` is a symlink: never rename over it. Inter-node SSH needs port 22, so propose no port change on PVE (also changes the Phase 6 PVE check). Detect PVE via the dpkg `pve-manager` status | proposed |
| 29 | §2 supported versions | Alpine 3.21 support ends 2026-11-01. Only `main` is supported on 3.21–3.23; `sudo` and `ufw` are in community. Lynis isn't packaged in any stable Alpine branch | proposed |
| 30 | §2 Lip Gloss | v2 doesn't downsample in `Render`; that happens only through `colorprofile.Writer`. Its NO_COLOR handling isn't no-color.org compliant. Background detection uses raw mode with ISIG off, so make it opt-in (changes 1A "detect the background where possible") | proposed |
| 31 | §1 / Phase 7 | slsa-github-generator v2.1.0 is final and unmaintained, so "update the generator version" can't be done; slsa-verifier is unmaintained too. The alternative is `actions/attest@v4` with `gh attestation verify` (reopens §2's "SLSA Go builder" rationale) | proposed; decide at Phase 7 |
| 32 | §5 phase order | Vertical slices per area after the P3/P4 cores, instead of separate P3, P4 and P5 passes; System v0 built in Phase 2 | proposed (plan) |
| 33 | Phase 3 (new) | Lazy startup writes: no `/etc` or `/var/log` writes before flag parsing, landing after `install.sh` stops relying on `hardn --version` | proposed (plan) |

---

## 13. Decisions recorded (2026-10-16)

The maintainer answered these on 2026-10-16. They are final.

| # | Decision | Where it applies |
|---|---|---|
| 1 | **History preserved.** `gc.auto=0` and `maintenance.auto=false` set in the old checkout. 75 local archive tags created: `archive/2025-03/<branch>` ×7, `archive/2025-04/arch3`, `archive/stash/*` ×37 (annotated), `archive/dropped-stash/*` ×24, `archive/reflog/*` ×5, and `archive/dangling-objects/2026-10-16` holding 50 dangling blobs and 28 trees. `git fsck --unreachable --no-reflogs` now reports nothing | §2.2, §4 |
| 2 | **Backup:** the maintainer keeps a separate backup elsewhere; its location is intentionally not recorded | §1 |
| 3 | **Export and support files moved out of the repo**, to sibling `_design/` and `_support/` folders in the parent directory. No pre-commit hook is needed | §2.4, §8.1, §12 #10 |
| 4 | **Toolchain:** a `mise.toml` in the parent folder (outside every git tree) pins go 1.27.2, golangci-lint 2.14.0 and govulncheck 1.9.0 | §5 |
| 5 | **VM:** the maintainer provides a Debian VM on Proxmox when asked. P0.6 timing and VM captures are deferred ("written, not run") | §9, §10 |
| 6 | **Behaviour baseline (recency first, then relevance):** the most recent work is arch3, whose menus and operations are origin/main's code relocated into `legacy_pkg/`, plus a two-screen prototype. So origin/main's content is the baseline for wording **and** behaviour. Behaviours on the V-list (§7) are **defects to fix**, not behaviour to preserve. v0.3.2 is the reference only where origin/main regressed | §3, §7, [phase0/v032-reference.md](phase0/v032-reference.md) |
| 7 | **Vendor:** stop committing `vendor/` (an agent decision: CI already deletes it, and the SLSA builder runs `go mod vendor` itself) | §5, §12 #12 |
| 8 | **Never push or publish anything.** Everything stays local | §4, R3 |

**Phase 0 units presented at this review**, as stacked local branches in this order (each built on the previous one; none merged into `next/v0.4`):
1. `next/p0-allowlist`: the `.gitignore` allow-list fix;
2. `next/p0-check`: the `make check` target, plus the default-goal safety gate;
3. `next/p0-golden`: characterization goldens;
4. `next/p0-refresh`: stop committing `vendor/`, plus the toolchain and dependency refresh;
5. `next/p0-findings`: this document, its appendices and the two logs (last, so it can describe the others).

---

## 14. Evidence

- The raw audit evidence is kept **outside the repository** next to the brief, and is not committed. It contains unredacted local details.
  - Lane reports carry full `ref:path:line` evidence, harness outputs and scripts.
  - Verification reports supersede the audit where they differ.
  - Plan reviews are kept with them.
- The P0.1 assessment used scratch extracts and scripts, which are not committed.

| Lane | Covers |
|---|---|
| `lineage-arch3` | arch3 / hex-migration-new, stash@{0..3}, working tree, salvage map |
| `lineage-hex` | hex-migration / -legacy, stash@{4..31}, analysis docs |
| `mainline` | v0.3.1 → v0.3.2 → origin/main, PR #1–#3, stash@{32..36}, CLI/config surface diff, reflog-only 7a25bb9 / b5c60d6 / ff74d07 |
| `local-only` | ignored and untracked files, other copies, object-store risk, allow-list hazards |
| `architecture` | package map, call graph, dry-run, config, logging, backup, osdetect, seams, tests |
| `side-effects` | the side-effect table, harness evidence, V1–V27 ([appendix](phase0/side-effects.md)) |
| `interaction` | CLI surface, input model, screens, anti-patterns, styling census, status map, Figma mapping ([appendix](phase0/interaction.md)) |
| `baseline` | build/test/lint matrix, staleness, govulncheck, vendor, CI/SLSA, install.sh, refresh proposal |
| `design-export` | token inventory, drift, Go mapping with 256/16-color picks, contrast, cell geometry, glyph widths, screens, missing components |
| verification | git preservation (gc, stash, allow-list), mainline and design claims, the safety-defect harnesses (A1–A15), primary-source research (OpenSSH, distros, Proxmox, Lip Gloss, toolchain) |
| P0.1 assessment | the 26 anchored commits in §3.2 |
