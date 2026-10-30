# Environment on the engine: the Environment control (`pkg/engine/controls/environment`)

> **Unit:** refactor plan slice 9 (Environment), unit (b), its second part, `next/s9-control`, from `next/s9-gallery` at af5904a (the first part: [sudoers.md](../safety/sudoers.md) §2.13 to §2.16, §7). It builds on unit (a) ([gallery-environment.md](gallery-environment.md)), whose seven decisions and Made here calls the maintainer adopted on 2026-10-29 ("Adopt all seven slice 9 Environment recommendations in gallery-environment.md §5, and the Made here calls as stated"), then settled decision 1 (a)'s open item by words ("Settle decision 1 (a)'s open item by words: the doas row reads as hardn's own state (e.g. "Not kept by hardn") and the refusal says the row will not change. Then start slice 9 unit (b)."). Decisions implemented: 1 (a) (on doas, no rule of the user's written), 2 (a) (a root login refused), 3 (a) (origin/main's line read, "Kept by /etc/sudoers.d/<user>"), 5 (a) (item 1 both ways), 6 (a) (no main-menu row), 7 (the words), and the settlement's words, adopted 2026-10-29 with this unit's (§7). V-numbers: V16 (the grant and the line in one file), V18 (no backup), V20 (`/tmp`), on the control's path. It fixes gallery-environment.md §7's items 20 to 22 with the first part (§8).
> **Status:** built and tested on macOS, in process, on `hostfake`, through the real engine and `pkg/output` (run). Nothing has run on Linux or as root: every host claim is "written, not run (gate 5)" (§4). Nothing is wired into `cmd/hardn`, the main menu or the legacy Environment screen (units (c) and (d); *since `next/s9-replace`, `-e`, `setup-sudo-env` and main menu 8's Environment screen call it: [cmd-engine.md](cmd-engine.md) §2.9, §2.10*); legacy and characterization goldens are unchanged (run: `git diff --stat af5904a -- pkg/menu/testdata pkg/characterization` is empty). Words are **decided**: gallery-environment.md §6's, used exactly, and this unit's, adopted 2026-10-29 (§7), the "by hardn" note on sudo too.

The Environment control is the third control that both checks and plans, after DNS and Backup. `Read` gives a screen what the Environment screen draws: the tool, who ran hardn, whether hardn's files keep `HARDN_CONFIG` for that user, whether origin/main's file does, the files read after hardn's rule, and why item 1 is refused before it asks. `Check` says the same as a status. `Plan` gives the engine the change that keeps the variable, or stops keeping it, with the user's grant and other variables carried as read, through `steps.Sudoers`.

## 1. Why

origin/main appends its `env_keep` line to `/etc/sudoers.d/<user>`, the file its grant overwrites, so the line is lost (V16); it reads its state as a substring of that one file, a commented line counting; it falls back to `USER`, root's own at a root login, and writes `Defaults:root`; it writes through `/tmp/hardn_sudoers_temp` and in place, nothing backed up (V20, V18); it has no doas path (gallery-environment.md §2, §7). Unit (a) drew the replacement on `sudoers.PlanFor` and `steps.Sudoers`, with stand-ins for what real code lacked: who ran hardn, the reader of hardn's files (without it, an env-only Policy removes hardn's grant, a lockout-risk change: §7 item 20), the plan's findings on the change, and origin/main's line. The first part of this unit built those in `pkg/host/sudoers` and `steps`; this part builds the control on them, and the gallery runs on it.

## 2. Guarantees

### The reading (`Read`, `Check`)

1. **One control, `environment.keep`, titled "Environment"** (`Keep`; `Controls(getenv)`), main menu 8's label. **It carries hardn's environment as a lookup** (`Keep.Getenv`: `os.Getenv` in hardn, a map in the tests and the gallery; nil an empty environment), since the engine's `Check` and `Plan` take none, as the DNS control carries `hardn.yml`; depguard's `controls-through-host` keeps `os` out of the package. `Applies` is true on every host: a host with neither tool is Not Applicable through `Check` and refused by `Plan` with a report (§10, decision 8).
2. **What is read** (`Read(ctx, sys, facts, getenv)`), in this order, each a read, nothing written or run (run: `TestRead`, 17 hosts and environments, every call a read, and the Policy after `Read` as hardn's files hold it):
   - the tool, `sudoers.Choose(facts, ToolUnknown)`: sudo (and sudo-rs), doas, none (`ToolNone`), or not established (`Choice.Err`);
   - `HARDN_CONFIG` in hardn's environment (`Value`: what the tool passed on; the change cannot reach it);
   - who ran hardn, `sudoers.Invoker` (`SUDO_USER` under sudo and sudo-rs, `DOAS_USER` under doas; unset or root is no one: decision 2 (a));
   - hardn's files for that user, `sudoers.Read`, and the user's whole Policy, `sudoers.PolicyOf` (`Policy`: the grant and every variable, which the change carries); `Kept` is `HARDN_CONFIG` among the variables, not any variable (`TestRead`, `TestCheck`: "EDITOR only", Not kept);
   - origin/main's `/etc/sudoers.d/<user>`, `sudoers.ReadLegacy` (`Legacy`: where sudo reads one, and whether it holds origin/main's line exactly, a comment not counting; decision 3 (a); gallery-environment.md §7 item 4);
   - sudoers' findings of hardn's rules as they are (`Findings`, the plan of `Policy`, coded and worded by `steps.SudoersFinding`): the files read after hardn's rule (`Later()`, "`<file>` may override hardn's rule"), lines after the main file's includedir, origin/main's file;
   - item 1's change, keeping, or stopping once `Kept` (decision 5 (a)), built as `Plan` builds it (`change`, through `steps.Sudoers`): `Refused` (on doas with no rule of hardn's for the user; a file of hardn's the change rewrites or removes that is not root's alone) and `Risk` (lockout-risk: on doas, a rule of hardn's that asks a password; never on sudo, §2.7).
3. **Why item 1 is refused** (`Refusal()`: `Err`, else `Refused`). `Err` says the state was not read: no tool, no user, the tool not established, a user name hardn does not write into a rule (`ErrUserName`), sudoers' read (`ErrNotIncluded`, a symlink at hardn's file), a file of hardn's edited by hand, origin/main's file not read (a symlink there). `Refused` says the state was read and item 1's change is refused, as `Plan` refuses it (§10, decision 17). So a screen draws the state row Unknown on `Err` and the state on `Refused`, and refuses item 1 before it asks on either (run: `TestRead`, `TestRefusals`).
4. **The state's words** (`Reading.Words()`, `Words(kept)`; §5): "Kept" or "Not kept", each with the note "by hardn", as the row reads hardn's rules alone (the settlement, on sudo too; adopted 2026-10-29); where hardn's files do not keep it and origin/main's line does, "Kept" with the note "by /etc/sudoers.d/<user>" (decision 3 (a)). Tested: `TestRead`, `TestControl`, `TestCheck`. **On sudo too** (adopted 2026-10-29): the row reads hardn's files and origin/main's line, not sudo's effect, so an `env_keep` for the user in another file reads "Not kept by hardn" beside a Value sudo kept.
5. **`Check`** (`Keep.Check`), on `Reading.State`, the one classification a screen draws its rows by too (§10, decision 19): Configured where kept ("Kept by hardn", "Kept by /etc/sudoers.d/alice"); Not Configured where not ("Not kept by hardn"); Not Applicable where no tool is installed ("sudo isn't installed here") or no user ran hardn through it ("Not run with sudo", "Not run with doas"); Unknown, with the error and no detail, where the tool or a file could not be read or a file of hardn's was edited. Its findings are `Read`'s; its evidence (§5) the variable the tool names its user in, hardn's file that keeps variables for the user and what it keeps, and origin/main's file where sudo reads one. It writes nothing (run: `TestCheck`, 13 cases, each `Reading.State` the same; `TestGoldens/check`). **No status model takes it** (decision 6 (a)): it is not in `controls.Status`, so neither the main menu's rows nor the risk level weigh it. It is the `engine.Control`'s, for `engine.Check`: the JSON document's status of the control, for a caller that asks for it; no caller in hardn asks yet, and the screen draws `Read`.

### The plan (`Plan`)

6. **Item 1's change, either way** (`Keep.Stop`, decision 5 (a)): the user's Policy, read from hardn's files, with `HARDN_CONFIG` added after its variables (keep) or taken out (stop), **the grant and every other variable carried as read**, planned by `sudoers.PlanFor` and made a change by `steps.Sudoers(id, plan, summary)`: `environment.keep/keep` "Keep HARDN_CONFIG" or `environment.keep/stop` "Stop keeping HARDN_CONFIG" (decided; the plan's heading and the step's line, within `engine.TitleMax`), in `StageAccounts`, journalled as `plans.Sudoers`' step, sudoers' findings on it (part 1). Tested: `TestPlan` (17 cases, each diff's added line, its target, risk and findings), `TestControl`.
7. **On sudo the change is to `hardn-env-<stem>` alone, never lockout-risk:** a grant of hardn's is carried, so its file is not planned for its content (V16's separation, with the reader: gallery-environment.md §7 item 20); and where its mode or owner is not hardn's, which `PlanFor` would rewrite with the change (sudoers.md §2 item 7), lockout-risk for a password grant (`plan.go:396-401`), that change is left out of the plan: it is the grant's to repair (slice 5), not this one's (§10, decision 16; run: `TestPlan`, a password grant at `0640`, a `NOPASSWD` one at `0644`; `TestRead`, "grant at 0640", no risk). A grant's file another user or a group can write is not left out but refused, as the journal refuses it (§2.10). Keeping writes `Defaults:alice env_keep += "HARDN_CONFIG"` or adds the variable to the list (`"EDITOR HARDN_CONFIG"`); stopping removes the file, or rewrites the list without it (`"EDITOR"`). After the apply `hardn-grant-alice` is byte for byte as it was, its mode and owner too, with a password grant and without, at `0440` and at another mode (run: `TestGrantCarried`, four cases; the gallery's `TestEnvironmentGrantCarried`, `play/environment-grant`).
8. **On doas it rewrites hardn's rule, and never writes the user's first** (decision 1 (a)): with a rule of hardn's, keeping adds `setenv { HARDN_CONFIG }` (`permit persist setenv { HARDN_CONFIG } alice`), stopping takes it out (`permit persist alice`), lockout-risk for a rule that asks a password and plain for `nopass` (`sudoers.RiskLockout`, `pkg/host/sudoers/plan.go:396-401`, through `steps.Sudoers`' risk, `pkg/engine/steps/plans.go:59-61`); the engine then plans hardn's boot unit first (run: `TestPlan`, `TestDoasLockout`, `plan-doas-stop`). With no rule of hardn's for the user, keeping is refused before anything changes (§6); stopping plans nothing.
9. **Nothing to change plans nothing:** keeping where hardn's files keep it, stopping where they do not, an empty plan (the flow's "Nothing needs changing.", as `play/environment-kept-only` showed at af5904a); so the plan after an apply is empty (idempotency; run: `TestApply`, sudo and sudo-rs, both ways). **A list that would not change plans nothing even where a file's mode or owner differs** (§10, decision 1; run: `TestPlan`, "keep, kept, the file's mode another").
10. **Every refusal comes before any change** (§6), the same in `Read` and `Plan`, each a `steps.Refusal` with its code where the control has words, the others as they are, among them a file of hardn's that is not root's alone, which the journal refuses: one the change rewrites or removes, or on sudo the grant's, though the change leaves it out (decision 16) (`sudoers.Plan.Check`, `File.RootsAlone`, through `plans.Sudoers`; `PLAN_INVALID`, in words since 2026-10-30: §6) (run: `TestRefusals`: each through `Plan` too, every call a read; `hardn-env-alice` and `hardn-grant-alice`, each owned by uid 1000, and group-writable). **Plan only writes nothing:** the engine's plan of either way, on every host, changes nothing outside hardn's run directory and runs nothing (run: `TestPlanOnlyWritesNothing`, 8 hosts).
11. **The apply** is `steps.Sudoers`': each file backed up into the plan's backup run (V18: "Backup of /etc/sudoers.d/hardn-env-alice kept in" once stopping removed it, `applied-stop`), then `sudoers.Apply`: `visudo -c`, the copy `/run/hardn/<id>/sudoers-validate/hardn-env-alice` checked with `visudo -cf`, the write, `visudo -c` again; nothing under `/tmp` (V20; run: `TestApply`, its commands and files in order).

### The gallery on the control

12. **The Environment mocks read and plan with the control** (`environment.Read`, `environment.Keep`), where the gallery's stand-ins stood (gallery-environment.md §8: `invoker`, `userVar`, `currentPolicy`, `errEdited`, `legacyKeeps`, `sudoersFindings`, `envControl`, `envTreatment.target`); the state (`Reading.State`, by which the rows are drawn), the state's words, the summaries, the refusals and the sentence after the doas refusal (`Reading.Stays`) are the control's, the screen's words the gallery's (unit (c)'s). Each host is held to the control's reading and its words (`TestEnvHosts`). The mocks of the options not adopted are retired with their goldens (§10, decision 13).

### Through `host.System`

13. The control reads and plans only through `host.System`, `pkg/host/sudoers` and `pkg/engine/steps`, and renders nothing: depguard's `controls-through-host` covers the directory (`os`, `syscall`, `pkg/ui`, `pkg/output` denied; run: lint 0 issues). It reads hardn's environment only through `Keep.Getenv`.

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| sudo and sudo-rs set `SUDO_USER`, OpenDoas `DOAS_USER`, to the invoking user's name | **[I]** sudo(8), sudo-rs v0.2.13 `environment.rs`, OpenDoas v6.8.2 `env.c`, as [sudoers.md](../safety/sudoers.md) §3 cites them; not read again here, not run | §2.2: who ran hardn |
| `Defaults:<user> env_keep += "HARDN_CONFIG"` in its own drop-in keeps the variable under sudo 1.9.16p2 (Debian 13) and sudo-rs 0.2.13 (Ubuntu 26.04) | run on the Phase 2 VMs: [vmtest.md](../safety/vmtest.md):248, :254 (Debian 13), :302, :315 (Ubuntu 26.04) | §2.7: the change on sudo |
| doas takes its options, `setenv` among them, from the last matching rule alone | **[I]** doas.conf(5), [sudoers.md](../safety/sudoers.md) §2 item 6, §3; doas's `setenv` has run on no host | §2.8: the change on doas is to the user's own rule, and a group's rule may win |
| `PlanFor` marks a change to the file that carries a user's grant lockout-risk when it removes it or the grant asks a password | read: `pkg/host/sudoers/plan.go:308-331`, `:396-401`; run: `TestPlan` | §2.7, §2.8 |
| `PolicyOf` reads a file back only when its bytes are `PlanFor`'s for some grant and list | [sudoers.md](../safety/sudoers.md) §2.13; run: `TestPolicyOf` | §2.6: what the change carries |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root. The control is called only by tests and the gallery yet (units (c), (d)); `ENVIRONMENT_NO_USER` is registered in `pkg/output` and reaches no live path (*since `next/s9-replace`, `-e`, `setup-sudo-env` and main menu 8's Environment screen call it, and `hardn -e` at a root login reports `ENVIRONMENT_NO_USER`: [cmd-engine.md](cmd-engine.md) §2.9, §2.10; screen-environment.md §4, checks 1 to 16*). On the VMs of [vmtest.md](../safety/vmtest.md) (Debian 13, Ubuntu 26.04, Alpine 3.24) and on Proxmox VE 9, through unit (c)'s screen or a dev-only driver:

- `Read` and `Check` under `sudo hardn` (Debian 13), sudo-rs (Ubuntu 26.04), `doas hardn` (Alpine) and at a root login: the user, the tool, `Not kept`, then `Kept` after keeping, with `sudo -l -U alice` showing the `env_keep` line; `HARDN_CONFIG` reaching the next `sudo hardn`;
- on Debian 13 with `hardn-grant-alice` (a password grant): keeping and stopping leave it byte for byte, `0440` `root:root` (and at `0640`, left so), and alice's `sudo` asks as before;
- origin/main's `/etc/sudoers.d/alice` as its `-e` wrote it: "Kept by /etc/sudoers.d/alice", then keeping writes `hardn-env-alice`, the legacy finding in the review; with the line commented out, "Not kept";
- on Alpine 3.24 without a rule of hardn's: refused before asking; with one (written by hand as `PlanFor` writes it): keeping and stopping, **doas's `setenv` keeping `HARDN_CONFIG`** (never run on any host), the lockout-risk plan with the boot unit, the typed hostname and the revert timer;
- on Proxmox VE 9: Not Applicable, refused before asking.

## 5. The state's words

As the screen's state row, the review and `Check`'s detail say them (`Words`, `Reading.Words`, `Reading.NotRun`):

| Host | `Check` | Detail | Row |
|---|---|---|---|
| hardn's files keep it | Configured | `Kept by hardn` | Kept, note "by hardn" |
| origin/main's line keeps it, hardn's files do not | Configured | `Kept by /etc/sudoers.d/alice` | Kept, note "by /etc/sudoers.d/alice" |
| neither | Not Configured | `Not kept by hardn` | Not kept, note "by hardn" |
| neither sudo nor doas | Not Applicable | `sudo isn't installed here` | Not Applicable |
| no user ran hardn through the tool | Not Applicable | `Not run with sudo`, `Not run with doas` | the User row |
| the tool or a file not read, a file of hardn's edited | Unknown, with the error | none | Unknown |

The evidence (**decided**, 2026-10-29): `SUDO_USER: alice` (`unset`, `root`), or `DOAS_USER`; `/etc/sudoers.d/hardn-env-alice: keeps HARDN_CONFIG EDITOR` or `: absent`, on doas `/etc/doas.d/hardn-alice.conf: keeps HARDN_CONFIG`, `: keeps nothing` or `: absent`; `/etc/sudoers.d/alice: keeps HARDN_CONFIG` or `: does not keep HARDN_CONFIG`; with no tool, `Choose`'s error.

## 6. The refusals

Each is a `steps.Refusal`; a screen says its Why, then its Fix (`screenkit.RefusalWords`), and `pkg/output` the Why with the code, then "To go on: " and the Fix. The code is the registry's for the error it wraps (pinned: `TestRefusals`).

| Code | When | Why | Fix | Words |
|---|---|---|---|---|
| `SUDOERS_NO_TOOL` | neither sudo nor doas installed (`Choose`'s `ToolNone`) | sudo isn't installed here | none: the decided words have none (§10, decision 7) | **decided** |
| `ENVIRONMENT_NO_USER` (new) | no user ran hardn through the tool (`ErrNoUser`) | hardn wasn't run with sudo, so there's no user to keep it for | run sudo hardn as that user | **decided** ("doas" on doas); the code **decided** (2026-10-29) |
| `SUDOERS_FILE_EDITED` | a file of hardn's edited by hand (`sudoers.EditedError`) | `<file>` holds a rule hardn didn't write | put hardn's back, or remove it | **decided** |
| `SUDOERS_FILE_EDITED` | both of sudo's files edited | `<grant file>` and `<env file>` hold rules hardn didn't write | put hardn's back, or remove them | **decided** (2026-10-29) |
| `SUDOERS_ENV_NEEDS_GRANT` | doas, keeping, no rule of hardn's for the user (decision 1 (a)) | hardn has no doas rule for alice to keep HARDN_CONFIG on | add setenv { HARDN_CONFIG } to the rule that lets alice use doas | **decided**; a screen says after it "Under doas stays Not kept by hardn." (`Reading.Stays`), **decided** (the settlement; adopted 2026-10-29), on the screen alone (§10, decision 18) |
| `PLAN_INVALID` | a file of hardn's that is not root's alone (owned by another user or group, or writable by group or others; `sudoers.File.RootsAlone`): one item 1's change rewrites or removes, or on sudo the grant's (decision 16); the journal's check (§2.10) | `<file>` isn't root's alone | make it root:root, writable by root only | **decided** (the maintainer, 2026-10-30: "give the not-root's-alone refusal words like "<file> isn't root's alone. Make it root:root, writable by root only."") |

The tool not established, both tools on Alpine (`ErrAmbiguous`), a user name hardn does not write (`ErrUserName`), `ErrNotIncluded` and a symlink at a file read: the error as it is, a screen saying it with `screenkit.RefusalWords` (its text with a full stop), as the gallery did (run: `TestRefusals`, four cases; the file not root's alone, worded above, four more).

## 7. Words

**Adopted (the maintainer, 2026-10-29):** "Adopt unit (b)'s proposed words, with "by hardn" on sudo's row too", the button's message naming the doas row as drawn with "Under doas stays Not kept by hardn." after the refusal, the same "by hardn" note on sudo's row, part 1's legacy-finding words with "also" where no grant is removed, and the code `SUDOERS_FILE_EDITED`. Every word below is decided.

| Where | Words | Source |
|---|---|---|
| The change (plan heading, step line, `Summary`) | "Keep HARDN_CONFIG", "Stop keeping HARDN_CONFIG" | **decided** (decision 7) |
| The state (row, review, `Check`) | "Kept", "Not kept", each with the note "by hardn" ("Kept by hardn", "Not kept by hardn"); the note "by /etc/sudoers.d/alice" | **decided**: the words decision 7, the "by hardn" note the settlement, on sudo too (2026-10-29), the legacy note 3 (a) |
| The User row, `Check`'s detail | "Not run with sudo", "Not run with doas" | **decided** |
| A file read after hardn's rule | "`<file>` may override hardn's rule" | **decided** (`steps.SudoersFinding`, part 1) |
| The refusals | §6 | §6 |
| After the doas refusal, on the screen alone (`Reading.Stays`) | "Under doas stays Not kept by hardn." | **decided** (the settlement of 2026-10-29: "the refusal says the row will not change"; adopted 2026-10-29); not in the refusal's Fix, so `-e`, `setup-sudo-env` and the JSON document do not print it (§10, decision 18) |
| `Check`'s detail with no tool | "sudo isn't installed here" | the refusal's, **decided** |
| `Check`'s evidence | §5 | **decided** (2026-10-29) |
| The code | `SUDOERS_FILE_EDITED` (part 1; `pkg/output/codes.go:297`), its text the sentinel's (`sudoers.ErrEdited`, `pkg/host/sudoers/policy.go:14`), "hardn's file holds a rule hardn did not write"; [reference/output.md](../reference/output.md)'s row for it | **decided** (2026-10-29; developer-facing); the refusal's words for it, §6, too |
| The code | `ENVIRONMENT_NO_USER`, "no user ran hardn through sudo or doas (a root login), so there is no user to keep HARDN_CONFIG for"; the sentinel's text "no user ran hardn through the elevation tool" | **decided** (2026-10-29; developer-facing) |
| The control's title | "Environment" | origin/main's main menu 8 label |
| Part 1's findings | the legacy file's four details and the after-include line's ([sudoers.md](../safety/sudoers.md) §2 item 5, §2.16) | **decided** (2026-10-29), with "also" where the plan removes no grant and "still" where it removes hardn's: the Environment's Policy grants nothing and its plan removes none, so its legacy finding is "may also let alice use sudo" (`pkg/host/sudoers/plan.go`, `legacy`; `plan-legacy`) |

The goldens (`pkg/engine/controls/environment/testdata/golden/human/`, the core cells, and `testdata/json/`), from the real engine on `hostfake`, show them as `pkg/output` renders them: `check` (seven hosts' statuses), `plan-keep`, `applied-keep` (nothing kept: the file was not there), `applied-stop` (hardn-env-alice's backup kept), `plan-legacy` (the legacy finding), `plan-doas-stop` (the boot unit, then the rule under "Lockout risk", the later file's finding), `refused-no-tool`, `refused-no-user`, `refused-edited`, `refused-doas`. The plan's id, random in its last part, is fixed in them (`stable`), in the boot unit's copy command too. `pkg/output`'s status view cuts a long detail at its column ("Kept by /etc/sudoers.d/...", "sudo isn't installe..." in `check`'s 80-column cells), as for every control; the JSON document has it whole. No main-menu row draws it (decision 6 (a)).

**Existing goldens changed**, each named in its commit's body too (legacy and characterization goldens unchanged):

| Golden | Commit | Why |
|---|---|---|
| `pkg/output/testdata/codes.golden`, `sentinels.golden` | ea8b909 | list `SUDOERS_FILE_EDITED` and `sudoers.ErrEdited` (part 1) |
| `pkg/output/testdata/codes.golden`, `sentinels.golden` | 95f8c86 | list `ENVIRONMENT_NO_USER` and `environment.ErrNoUser` |
| `internal/tools/uigallery/testdata/golden/play/environment-legacy` | 300bf6b | the review's legacy-file finding, four lines for developers, now "/etc/sudoers.d/alice may still let alice use sudo", "also" since 2026-10-29 (part 1; §7 item 22, proposed) |
| `internal/tools/uigallery/testdata/golden/environment/screen-alpine324`, `screen-alpine324-setenv`, `screen-doas-overridden`, `item1-doas-refuse`; `play/environment-doas-refuse`, `environment-doas-setenv`, `environment-doas-stop`, `environment-hosts` | 49860e8 | the doas state row, "Not kept by hardn", "Kept by hardn" (the settlement); **and its colour:** `status.warn` moves from the value ("Not kept", and on `doas-overridden` "Kept") to the note "by hardn", the value now muted, the marker still warn (decision 14), against the Made here call's "Kept in `status.warn`" and §6's "Not kept" (warn) |
| `environment/item1-doas-refuse`, `refusals`; `play/environment-doas-refuse`, `environment-doas-setenv` | 49860e8 | the doas refusal's added sentence, "Under doas stays Not kept by hardn."; since 39960f0 the screen's after the refusal (`Reading.Stays`), the text unchanged |
| `pkg/engine/controls/environment/testdata/golden/human/refused-doas`, `testdata/json/refused-doas.json` (this unit's own, from 2694316) | 39960f0 | the sentence gone from the refusal's Fix, "To go on:" and the JSON's `fix` and `message` (§10, decision 18) |
| `pkg/engine/controls/environment/testdata/json/applied-stop.json`, `check.json`, `plan-doas-stop.json`, `plan-keep.json`, `refused-doas.json`, `refused-no-user.json` (this unit's own) | 42dae0c | the control's ID `environment.keep`, its change ids `environment.keep/keep`, `environment.keep/stop` (§10, decision 20); the human goldens do not show them |
| every remaining `play/environment-*` golden | 49860e8 | play's note, which said the reader, the user lookup and the findings were the gallery's, unit (b)'s to build; and `item1-doas-refuse`'s and `alpine324-setenv`'s notes, which described the retired option and the old row |
| `play/environment-hosts` | 49860e8 | the hosts' menu without the five retired hosts: `doas-overridden` is 5, not 7 |
| deleted: `environment/screen-alpine324-rule`, `screen-alpine323-cloud`, `screen-kept-only`, `screen-grant-no-reader`, `screen-legacy-hardn-only`, `item1-doas-rule`; `play/environment-doas-rule`, `environment-wheel`, `environment-kept-only`, `environment-grant-no-reader` | 49860e8 | the mocks of the options not adopted, retired (§10, decision 13) |

Nothing else in the gallery's goldens changed: the real code gives the mocks' text word for word (each plain cell diffed), and the colour of the doas state row's value is the one drawing change (above).

## 8. Defects fixed (gallery-environment.md §7)

| # | Defect | Fix | Evidence now |
|---|---|---|---|
| V16 | The line in the file the grant overwrites | `hardn-env-<stem>` beside `hardn-grant-<stem>` (§3.17), **and the grant carried by the reader** | `TestGrantCarried`, `play/environment-grant` |
| 20 | An env-only Policy removes hardn's grant | the change plans the user's whole Policy (`PolicyOf`, part 1), its variables alone changed (§2.6, §2.7) | `TestGrantCarried`, `TestPlan`; `TestPolicyOfCarries` |
| 21 | `steps.Sudoers` drops the findings; the later file's detail names no file | `steps.Sudoers` carries them, worded (part 1); the gallery's overwrite gone (§2.12) | `TestPlan`, `plan-legacy`, `plan-doas-stop`; `TestSudoersFindings` |
| 22 | The legacy finding written for developers | reworded in sudoers (part 1; decided 2026-10-29) | `plan-legacy`, `play/environment-legacy` |
| 4 | A commented line reads kept | `ReadLegacy`: origin/main's exact line (part 1) | `TestRead`, `TestCheck` ("legacy, commented") |
| 5 | `USER` stands in for `SUDO_USER`; `Defaults:root` | `Invoker` (part 1); no user refused (decision 2 (a)) | `TestRead`, `TestRefusals`, `refused-no-user` |
| 6 | No user counts as success | refused, `ENVIRONMENT_NO_USER` | `TestRefusals`, `refused-no-user` |
| 7 | No doas path | `DOAS_USER`, hardn's doas rule read and changed; refused without one (decision 1 (a)) | `TestRead`, `TestPlan`, `refused-doas`, `plan-doas-stop` |
| 11, V18 | Written in place, nothing backed up | the engine's backup before the apply | `applied-stop` |
| V20 | `/tmp/hardn_sudoers_temp` | the copy checked in `/run/hardn/<id>/sudoers-validate/` | `TestApply` |

On the control's path. origin/main's code keeps them until unit (d) replaces it, as gallery-environment.md §7 says of 4 to 19.

## 9. API

```go
package environment // pkg/engine/controls/environment (new)

const ID = "environment.keep"
const Variable = "HARDN_CONFIG"
const ChangeKeep, ChangeStop = ID + "/keep", ID + "/stop"
const SummaryKeep, SummaryStop = "Keep HARDN_CONFIG", "Stop keeping HARDN_CONFIG"
const WordKept, WordNotKept, NoteHardn = "Kept", "Not kept", "by hardn"
var ErrNoUser error // ENVIRONMENT_NO_USER
func Controls(getenv func(string) string) []engine.Control
type Keep struct { Getenv func(string) string; Stop bool } // an engine.Control
type Reading struct {
	Choice   sudoers.Choice
	User     string
	Value    string
	Err      error // the state not read
	Policy   sudoers.Policy
	Kept     bool
	Legacy   sudoers.Legacy
	Findings []engine.Finding
	Refused  error // item 1 refused, the state read
	Risk     bool
}
func Read(ctx context.Context, sys host.System, f detect.Facts, getenv func(string) string) Reading
func (Reading) Refusal() error
func (Reading) Later() []engine.Finding
func (Reading) ToolName() string // "sudo" or "doas"
func (Reading) NotRun() string   // "Not run with sudo"
func (Reading) Words() (value, note string)
func (Reading) State() engine.State // NotApplicable, Unknown, Configured, NotConfigured
func (Reading) Stays() string    // "Under doas stays Not kept by hardn", or ""
func Words(kept bool) (value, note string)

package output // added
// code ENVIRONMENT_NO_USER
```

## 10. Decisions of this unit

Engineering; say if you'd rather otherwise.

1. **The way item 1 turns is the caller's** (`Keep.Stop`), not inferred in `Plan`: `-e` and `setup-sudo-env` keep, the screen passes `Read`'s `Kept`; and a variable list that would not change plans nothing, even where the file that holds it has another mode or owner, which a plan that changes the list rewrites with it (sudoers.md §2 item 7); the grant's file on sudo is never in the plan, and is refused where another user or a group can write it (decision 16).
2. **The environment is carried by the control** (`Keep.Getenv`), as the DNS control carries `hardn.yml`: no engine API changes, and the package reads no process state of its own.
3. **`Read` tells the state not read (`Err`) from item 1 refused (`Refused`)**, `Refusal()` joining them, so a screen draws Unknown for the first and the state for the second (doas with no rule of hardn's: "Not kept by hardn", refused).
4. **The files read after hardn's rule are those of the rule as it is** (the plan of the user's Policy), not of item 1's change: the screen's warning is about the state it shows; the plan lists its own findings in the review.
5. **origin/main's file not read is an error** (a symlink there: Unknown, item 1 refused), not "Not kept": the state cannot be told.
6. **`ENVIRONMENT_NO_USER`, a sentinel of the control's**: `Invoker` says no one with `""` and no error (part 1), so the refusal is the control's; it comes after the `SUDOERS_` codes in the registry.
7. **No tool's refusal has no Fix**: its decided words have none; `pkg/output` says the Why and the code alone (`refused-no-tool`).
8. **`Applies` true on every host**, as the DNS control: Not Applicable through `Check`, refused through `Plan`, so `-e` on Proxmox VE says why rather than "Nothing needs changing."
9. **`Check`'s Unknown has no detail**: the state's word says it, and the error is the report's.
10. **The change ids are `environment.keep/keep` and `environment.keep/stop`**, not the user's name (the gallery's `environment/alice`): a name may end in `$`, which an id does not take.
11. **`Check`'s findings are all of sudoers' for the rule as it is** (later files, after-include lines, origin/main's file), as a plan carries them.
12. **Both edited files are named in one refusal** (plural words, decided 2026-10-29), the grant's first, rather than one per attempt.
13. **The gallery's mocks of the options not adopted are retired with their goldens** (1 (b), 3 (b), 5 (b), and the plan without the reader), rather than kept on gallery-local code: the control plans none of them, and keeping them would keep a planner of the gallery's own; git keeps them at af5904a, and gallery-environment.md §3 records what each showed. The later file's finding is still shown, on `doas-overridden`.
14. **The "by hardn" note is drawn as the legacy row's** (a `KeyValue` note: the value plain, the note in the state's colour), so `status.warn` moves from the value to "by hardn" (§7, the goldens changed), on sudo too since 2026-10-29; adopted as drawn with the words (§7).
15. **Nothing for `-e`'s lockout refusal is added here**: `engine.Plan.Risk` is lockout for exactly the plans unit (d) refuses (`TestDoasLockout`), never on sudo (decision 16), and its words are unit (d)'s to propose with the wiring.
16. **On sudo the change leaves the grant's file out where its mode alone is not hardn's** (`planFor`), rather than rewriting it with the change: its bytes are the grant's as read (`PolicyOf`), so `PlanFor`'s change to it is a mode or owner alone, the grant's to repair (slice 5); in this change it would make item 1 lockout-risk for a password grant, and `-e` refuse it (unit (d)). No `pkg/host/sudoers` API changes: the control keeps the plan's change to `FileNames(...).Env` alone. **A grant's file another user or a group can write is refused** (`PLAN_INVALID`), the whole plan put through `steps.Sudoers` first, as before the grant's file was left out: it is what sudo reads to grant, and leaving it as it is unsaid would hide it (run: `TestRefusals`, `hardn-grant-alice`, two cases).
17. **`Read` builds item 1's change as `Plan` does** (`change`: `planFor`, then `steps.Sudoers`), so a plan the journal refuses, a file of hardn's not root's alone, is refused before item 1 asks, not after (`Refused`), in words since 2026-10-30 (`notRoots`, the file found with `sudoers.File.RootsAlone`; §6).
18. **The settlement's sentence is the screen's, not the refusal's** (`Reading.Stays`, said after `screenkit.RefusalWords`), rather than the doas refusal's `Fix`: `pkg/output` prints a `Fix` after "To go on:" and in the JSON document's `fix`, where the sentence is no step and there is no row to stay anything (`-e`, `setup-sudo-env`: unit (d)). The refusal keeps the decided words; the gallery says the sentence after them, its goldens unchanged.
19. **`Reading.State` is the one classification** of a reading (Not Applicable, Unknown, Configured, Not Configured), `Check` built on it and the gallery's rows drawn by it, as Backup's `Reading.Note` serves its `Check` and its screen: unit (c) draws from the control rather than repeating `Check`'s conditions, and `Words` is said only where `State` is Configured or Not Configured.
20. **The ID is `environment.keep`**, `<area>.<row>` as every control's ([checks.md](checks.md) §1.2), the row the type's name as for the others (`backup.settings`, `dns.nameservers`, `updates.auto`), where it was `environment`: changed before units (c) and (d) wire it and before any release, so the change ids are `environment.keep/keep` and `environment.keep/stop`, as `backup.settings/save`.

## 11. Not in this unit

- **Unit (c), the screen** (`pkg/screens/environment`), retiring the mocks: the section from `Read`, by `State()` as the gallery's `rows` (the User row, `NotRun` with no user; the state row from `Words`, Unknown on `Err`, Not Applicable with no tool; each `Later()` finding's detail under a Kept row; Value), item 1 labelled by the tool (`ToolName`), "Keep HARDN_CONFIG" or "Stop keeping HARDN_CONFIG" from `Kept`, refused before it asks from `Refusal()` in `screenkit.RefusalWords`, then `Stays()` where it is not empty, marked Lockout risk from `Risk`; the change through `screenkit.Session.Change` with `environment.Keep{Getenv: os.Getenv, Stop: Kept}`; the review's state row from `Words(!Kept)`; the typed hostname for a lockout-risk plan; "Takes effect at the next sudo hardn."; item 2, the guide; plan only, stopping at the plan. *Built in `next/s9-screen` ([screen-environment.md](screen-environment.md)): `pkg/screens/environment`'s `Run`, each item as listed here, `Getenv` the caller's (`os.Getenv` from unit (d): `os` is denied to `pkg/screens`), the typed phrase the kernel's hostname; `TestParity` drives it; the gallery's mocks retired. `Words` has taken no tool since 9a8e3d8: this item said `Words(tool, !Kept)`, and §9 `Words(t sudoers.Tool, kept bool)`, both corrected in `next/s9-screen`.*
- **Unit (b2), parity**: origin/main's end state against the control's (gallery-environment.md §9). *Built in `next/s9-parity`: [parity-environment.md](parity-environment.md), `TestParity` in `pkg/screens/environment`, the control run as item 1 through `screenkit.Session.Change` and as `-e` through the engine, held equal.*
- **Unit (d)**: main menu 8 to the screen; `-e` and `setup-sudo-env` on `environment.Controls(os.Getenv)`, applied without asking; **refusing, non-zero, naming main menu 8, a plan with `Risk == engine.RiskLockout` before `Apply`**, so the revert timer is never armed unasked (Made here, adopted), with words proposed then; `-n` showing the plan; `setup-sudo-env`'s root check and `hardn.yml` load; the `su -` probe and its notice removed (decision 4); the legacy code deleted after (b2). *Built in `next/s9-replace` ([cmd-engine.md](cmd-engine.md) §2.10): `keepEnvironment` runs `environment.Controls(os.Getenv)` through `runControls`, whose new `refuse` parameter takes `-e`'s refusal of a plan whose `Risk` is `engine.RiskLockout`, before `Apply` and under `-n` too: "Keeping HARDN_CONFIG here risks a lockout (PLAN_REFUSED)" / "To go on: keep it from main menu 8, Environment, which asks you to confirm.", proposed, the code the registry's `PLAN_REFUSED` (`engine.ErrRefused`), exit status 1. No change to this package: decision 15 held.*
- **Slice 5**: origin/main's `/etc/sudoers.d/<user>`, read here for its line, migrated there (decision 3 (a)).
- **Run All (slice 8)**: Environment is not hardening; whether Run All plans it is slice 8's.

## 12. Size

`git diff --numstat af5904a` (run, after the review's fixes): non-test Go 886 lines added and 19 removed outside the gallery, both parts (the control 562, of them 205 whole-line comments; `pkg/host/sudoers` 279; `steps` 37; `pkg/output` 8); the gallery 143 added and 467 removed; tests 1,653 lines added outside the gallery, the gallery's tests 91 added and 177 removed. Testdata: 16 new golden files (10 human, 6 JSON) for the control; 10 of the gallery's deleted; the changed ones named in §7. **Against Backup's unit (b)** (lines added, a proxy): its control unit added 705 non-test lines outside the gallery after its review's fixes ([control-backup.md](control-backup.md) §12), 776 in gallery-environment.md §10's count from the merges; so this unit is about **1.26** of the first and **1.14** of the second, and about **1.03** of its tests (about 1,600). Over the guideline of about 500 changed lines: a reader, a user lookup, a legacy read and the findings in `pkg/host/sudoers` and `steps` (part 1), and a control that reads, checks and plans both ways on three tools, each with tests; the commits split it so.
