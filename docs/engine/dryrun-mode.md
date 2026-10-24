# The mode: plan only, its precedence and its source (slice 2b, unit (b))

> **Unit:** refactor plan slice 2b, unit (b), `next/s2b-mode` ([gallery-dryrun.md](gallery-dryrun.md) §8 (b)). The maintainer's request (2026-10-23): "Start slice 2b unit (b): the mode, with §3.19's precedence and its source for the indicator, decisions 8 to 10 and the deprecation note." Plan §3.19 (approved 2026-10-17): "`dryRun` precedence: explicit flag wins, otherwise config". Decisions 1, 2, 8, 9 and 10 of [gallery-dryrun.md](gallery-dryrun.md) §4, adopted by the maintainer on 2026-10-23. V3 ([side-effects.md](../refactor/phase0/side-effects.md)); P-03 ([proposals.md](../refactor/proposals.md#p-03)).
> **Status:** built and tested on macOS, in process: the root command driven as the binary runs it, through `cmd/hardn`'s seams, on hardn.yml files in each test's temporary directory and the recording mocks; `-g` on `hostfake`. No hardn binary ran; no test read or wrote a hardn.yml outside its temporary directory, or anything under this machine's `/etc`. Nothing has run on Linux or as root. **Every word new to origin/main is proposed** (§5).
> **Unit (d) built** (`next/s2b-replace`, [dryrun-replace.md](dryrun-replace.md)): item 6 and Run All's option 2 removed, `SetDryRun` with them; User Management, Backup and Firewall's profile add and remove refused under plan only; docs/configuration.md and the release notes made current. §2 and §4.1 item 2 are resolved.

## 1. What is built

### 1.1 The precedence (§3.19; decisions 1 and 2)

The session's mode is set once, as hardn starts, in the root command, right after hardn.yml loads (`cmd/hardn/main.go`, `config.Config.ResolveDryRun`), where origin/main set it from the flag alone (`cfg.DryRun = dryRun`, V3):

| Start | Mode | Source (`DryRunSource`) |
|---|---|---|
| No flag, no file found (the default file created at the question, or declined) | live | none (`DryRunDefault`) |
| No flag, file `dryRun: false` (or no key) | live | none (`DryRunDefault`) |
| No flag, file `dryRun: true` | plan only | `DryRunFile` |
| `-n`, `--dry-run` or `--dry-run=true`, any file or none | plan only | `DryRunFlag` |
| `--dry-run=false`, any file or none | live | none (`DryRunDefault`) |

Live names no source, whatever made it live (decision 2: "live names nothing"), so unit (c) reads the source alone.

"Given" is pflag's `Changed("dry-run")` (`dryRunGiven`): `--dry-run=false` is given and wins over a file's `true`. The file is whichever the loader found (`-f`, `HARDN_CONFIG`, `/etc/hardn/hardn.yml`, the two home files, `./hardn.yml`), its `dryRun` as loaded, the default `false` where it has none. `DryRun` stays the field every legacy menu reads; the DNS screen reads it as its item is chosen, as before.

### 1.2 The source, for unit (c)

`Config.DryRunSource()` names what set plan only: the flag (`DryRunFlag`) or the file (`DryRunFile`). It names nothing (`DryRunDefault`) for a live session and for plan only set inside the session. It is `ResolveDryRun`'s source only while `DryRun` still holds the value it set: `SetDryRun`, which item 6 and Run All's option 2 now call, sets no source, and any other write to `DryRun` drops it, so a mode the session set never shows "(-n)" or "(hardn.yml)" (decision 2). *Since unit (d): `SetDryRun` is deleted with its two callers, and no in-session toggle remains, so "Mode Plan only" with no source no longer occurs in hardn. The no-source state stays in code, unreachable: the indicator's zero source, and `pkg/menu`'s mapping of `DryRunDefault`, should anything write `DryRun` after the start ([dryrun-replace.md](dryrun-replace.md) §3).* The main menu holds the configuration (`m.config`), so unit (c)'s indicator reads the source there. Its words are not changed here: `SetDryRunStatus(true, m.config.DryRun)` still draws "Dry-run Mode Disabled" or "Enabled". *Since unit (c), `next/s2b-menu`: "Mode Live", "Mode Plan only (-n)", "Mode Plan only (hardn.yml)", or "Mode Plan only" for a mode set inside the session, read from `DryRun` and `DryRunSource()` on every pass ([menu-layout.md](menu-layout.md) §3).*

### 1.3 Plan only from hardn.yml with an operation flag (decision 9)

Refused as `-n` with one is: exit status 2, nothing run, the message naming the file and `--dry-run=false` (§5). Where:

- **Before the root check**, as `-n`'s refusal: when an operation flag is given and `-n`/`--dry-run` is not, hardn reads the `dryRun` of the file the loader will find, quietly (`config.FileDryRun`: no log line, no question, no file created). The root command and `setup-sudo-env` both.
- **Again once the file is loaded** (root command): the configuration the session runs on decides, should the file say `true` now where it did not before the root check, or not have been readable then. A file that does not load is the loader's to report, as before (exit 1).

| Operation | No file | `dryRun: false` | `dryRun: true` | `-n` | `--dry-run=false` over `true` |
|---|---|---|---|---|---|
| `-r`, `-c`, `-d`, `-w`, `-e`, `setup-sudo-env` | runs | runs | refused, 2, naming the file | refused, 2 (V1's) | runs |
| `-g` | applies | applies | the plan, 3 with changes, 0 without, 1 refused | the same | applies |
| `-g` with another operation flag | as each | as each | refused, 2, naming the other flag | the same | as each |

`hardn confirm` and `hardn rollback` (`--boot` included) are not refused by the file: they read no hardn.yml, and they keep or undo a change already made; a key in the file must never stop the boot unit's rollback. `-n` still refuses them, as before. This goes beyond decision 9, which is about the operation flags, so it is the maintainer's to decide (§4.1); docs/configuration.md and the release notes say they still run.

### 1.4 The legacy saves write `dryRun` as loaded (decision 8)

`config.SaveConfig`, which every legacy save calls (pkg/menu's 12 sites, [gallery-dryrun.md](gallery-dryrun.md) decision 8: Firewall's profiles, Backup, User Management, SSH keys, Run All, Dry-Run, Python packages, sources; and `CreateDefaultConfig`, whose defaults say `false` either way), writes the whole configuration with `dryRun` as the file gave it, never the session's mode: `-n`, `--dry-run=false` and a toggle are never saved; only an edit of the file sets it. With no file loaded that is the default, `false`. The new saves already write only their settings: `config.Edit` with settings named, which `steps.PlanSave` requires and `config.Save` passes on, compares and writes those alone (`TestSaveOfASettingKeepsDryRun`, confirmed with the session's mode differing from the file's either way). `config.Save` and `Edit` with no setting named still compare every setting, `dryRun` with it; nothing calls them so (§8).

### 1.5 The deprecation note (decision 10)

- **docs/configuration.md**: a section beside `dryRun`, "`dryRun`: plan only", with the precedence, the refusal, what plan only does not yet cover (User Management, Backup and Firewall's profile add and remove, until unit (d); `confirm` and `rollback`) and a "Changed in the next release" note, which leaves out decision 10's "item 6 removed" while item 6 is still there (a deviation, §4.1); the search path's sixth entry, `./hardn.yml`, which it lacked and which the note needs. The development example keeps `dryRun: true`, now commented and followed by what it does: every start plan only, scripts and cron included, operation flags refused, `-g` only planning. Kept rather than changed: plan only is a fair setting for a test host, and the line now says what it means.
- **The release's notes**: the repository kept none (the release workflow writes the changelog from the commit subjects, `.github/workflows/release.yml`; plan Phase 7's `next/p7-docs` covers the README and contributing only), so this unit starts [docs/releases/unreleased.md](../releases/unreleased.md): `dryRun` now read (it was ignored, V3), `-n` and `dryRun` meaning plan only, the flag winning, `--dry-run=false` to override a file's `true`, the operation flags refused and `-g` planning, what a file an earlier hardn left with `dryRun: true` now does, and item 6's removal, marked as waiting for unit (d); its header says the notes ship with unit (d), not before.
- **Files origin/main left**: item 6's Enable saved `dryRun: true` to `./hardn.yml` (`menu/dry_run_enable.golden` before this unit), as did every legacy save made under `-n`. Such a file, where it is the one found, now starts plan only: the menus preview, an operation flag is refused (2) and `hardn -g` from a script plans and exits 3 where it applied before.

## 2. Until unit (d): item 6 and Run All's option 2

> *Resolved by unit (d), `next/s2b-replace` ([dryrun-replace.md](dryrun-replace.md)): item 6 and option 2 are removed, and plan only's three gaps below are refused, "Not available in plan only.". The bullets are `next/s2b-mode`'s.*

Both still work, on the session alone:

- **Item 6** sets the session's mode with no source (`SetDryRun`) and still saves `./hardn.yml`, now with `dryRun` as loaded: turning plan only off in a session that hardn.yml or `-n` started applies changes for that session only, and the next start's mode is set again as it starts: plan only where the file says so or `-n` is given; turning it on in a live session lasts until hardn ends. Its three screens and pauses are as before.
- **Run All's option 2** flips the session's mode the same way, asks its typed "yes" when it goes live, and saves `./hardn.yml` with `dryRun` as loaded; declined, the mode stays flipped (decision 7's defect, removed with it).
- **Plan only reaches the legacy menus as `-n` always did**, with its gaps: User Management changes the host (V2), Backup writes its test file and saves, Firewall's profile add and remove save `./hardn.yml` (decision 6, unit (d)). Before this unit a file's `dryRun: true` gave a live session; it now gives a plan-only one with those gaps, as `-n` gives.

## 3. API

| Package | Added or changed |
|---|---|
| `pkg/config` | `type DryRunSource` (`DryRunDefault`, none, for live; `DryRunFlag`, `DryRunFile`); `(*Config).ResolveDryRun(given, flag bool)`, `SetDryRun(on bool)` (deleted by unit (d)), `DryRunSource() DryRunSource`; `FileDryRun(explicitPath string) (path string, dryRun bool, err error)`; `SaveConfig` writes `dryRun` as loaded. Unexported: the loaded `dryRun`, the source and the value it set (in `loaded`), recorded by `UnmarshalYAML`; `readConfigFile`, the loader's read, shared with `FileDryRun`; `defaultSearchPath`, the one default search order `FindConfigFile` and `ConfigFileSearchPath` (so `FileDryRun`) walk |
| `pkg/cmd` | `RefuseConfigDryRunOperations(dryRun bool, path string, operations []string) error`, gated by `dryRunFlagApplies` as `RefuseDryRunOperations` is; both now return a usage error that is `output.ErrUsage` to `errors.Is` (code `USAGE`), `-n`'s words unchanged, the file's new (§5) |
| `cmd/hardn` | the seam `fileDryRun`; `refuseConfigDryRunOperations`, `dryRunGiven`, `refuse`; `cfg.DryRun = dryRun` replaced by `ResolveDryRun` and the refusal as loaded. The `-n` refusal stays on line 123 |
| `pkg/menu` | item 6 and Run All's option 2 call `SetDryRun` (both removed by unit (d)) |

No new flag, key or module.

## 4. Decisions of this unit

1. **Both refusal points** (§1.3): before the root check, as asked, so that a refusal runs nothing, as `-n`'s; and on the configuration as loaded, so that what the session runs on decides. The cost: a user not root reads the file found from their own environment before the root check, so a `dryRun: true` in their home file is refused (2) before "needs root" (1), and under `sudo` the home searched may differ.
2. **`setup-sudo-env` is refused** under the file's plan only, though it loads no configuration: it is in V1's list, and it would otherwise apply what the file asks to preview. A file that does not load does not stop it, as it never read one.
3. **The code is `USAGE`**, reused: "a usage error: bad flags or configuration", exit 2, the same meaning. `-n`'s refusal carries it too, with no change to its words; neither prints a code (stderr, human words).
4. **Live names no source**, whatever made it live (a file's `false`, no key, `--dry-run=false`, no file): decision 2's "live names nothing", so the source is plan only's alone.
5. **The example files** (`hardn.yml.example`, `pkg/config/embed.go`) are unchanged: "Preview changes without applying them" still holds.

### 4.1 For the maintainer

1. **`confirm` and `rollback` under the file's plan only** (proposed). Not refused (§1.3): they read no hardn.yml, they keep or undo a change already made, and a key in the file must never stop the boot unit's `rollback --boot`. This sets what plan only covers beyond decision 9. *Alternative:* refused as `-n` refuses them, `rollback --boot` excepted.
2. **Decision 10's "item 6 removed" is not yet in docs/configuration.md** (a deviation). The page describes the code at the tip, where item 6 is still in the menu, so the line would be false; unit (d) adds it. The release notes carry it, marked as waiting for unit (d), and ship with (d). *Resolved by unit (d): both say item 6 is removed, the release notes no longer marked as waiting.*

## 5. Words

**Proposed** where new; the table is the review's.

| Where | Words | Source |
|---|---|---|
| Refusal, plan only from hardn.yml with an operation flag (stderr, exit 2) | "hardn: /etc/hardn/hardn.yml sets plan only (dryRun: true), so -d/--disable-root can't run. Nothing was changed. To apply, add --dry-run=false." | **proposed** (decision 9): the file, the mode's name, and the way on |
| Refusal, `-n` with an operation flag | unchanged | P0.8 |
| docs/configuration.md, "`dryRun`: plan only" | "`dryRun: true` starts `hardn` in plan only: changes are shown, not applied, except in the three menu areas below. `-n` does the same for one run."; "Not yet in every menu."; "The flag wins."; "Operation flags are refused under plan only, as with `-n`"; "`hardn confirm` and `hardn rollback` still run under the file's `dryRun: true`"; "Set it here, not in the menu." | **proposed** (decision 10) |
| docs/configuration.md, the note | "Changed in the next release. `dryRun` used to be ignored … Set it to `false`, or run with `--dry-run=false`." | **proposed** (decision 10) |
| docs/configuration.md, the development example | "plan only at every start"; "With `dryRun: true`, every start is plan only, scripts and cron jobs included …" | **proposed** |
| Release notes | docs/releases/unreleased.md, six bullets | **proposed** (decision 10) |
| Indicator, `-n/--dry-run`'s help | unchanged ("Dry-run Mode" "Disabled"/"Enabled"; "Dry run mode (preview changes without applying)") | origin/main; the indicator's are unit (c)'s |

## 6. Tests

- `cmd/hardn` (`mode_test.go`): `TestDryRunPrecedence` (nine starts: no flag and no file; file false; file true; `-n` over each; `--dry-run=false` over true and over none; `--dry-run=true`; each mode and source, none for live, and no file read before the root check without an operation flag); `TestPlanOnlyRefusesOperations` (each of the six operations under six sources: no file, file false, file true, `-n` over false and true, `--dry-run=false` over true; refused before the root check with nothing run, or changes made through the mocks); `TestPlanOnlyRefusedAsLoaded` (the file saying `true` only when loaded: refused after the load, before OS detection); `TestConfigureDNSFlagPlanOnlyFromFile` (`-g` plans and exits 3, `--dry-run=false -g` applies, `-g -d` refused naming `-d` alone; hardn.yml never saved). The harness clears `Changed("dry-run")` between runs and stubs `fileDryRun`.
- `pkg/config` (`dryrun_test.go`): `TestResolveDryRun` (eight cases, YAML 1.1's `yes` among them; live with no source), `TestDryRunSourceOnlyAsSet`, `TestSaveConfigDryRunAsLoaded` (decision 8: under `-n`, `--dry-run=false` and a toggle each way, the save writes the loaded value and leaves the session's), `TestSaveOfASettingKeepsDryRun`, `TestFileDryRun` (`-f` and `HARDN_CONFIG` only: the search path's other files are this machine's).
- `pkg/cmd`: `TestRefuseConfigDryRunOperations`, `TestRefusalCodes`.
- Mutation-checked (`go test -overlay`): the old overwrite fails eight of the nine starts, the refusal as loaded and `-g`'s plan; the root command's refusal before the root check removed fails each of its operations under the file's `true` (refused only after the root check) and `-g -d`; the refusal as loaded removed fails `TestPlanOnlyRefusedAsLoaded`; "given" read as the flag's value fails every `--dry-run=false`; `SaveConfig` writing the session's mode fails every case of `TestSaveConfigDryRunAsLoaded`; the loaded value not recorded fails the file's `true`; a live start given a source fails the live rows of `TestResolveDryRun` and `TestDryRunPrecedence`.

## 7. Goldens

`pkg/characterization`, each changed because this unit changes what it records:

- `config/full-flag.golden`: V3's note flipped to a `FIXED` line (the loader keeps `dryRun`, and the session now runs in it unless the flag is given).
- `menu/dry_run_enable.golden`: `./hardn.yml` as written now holds `dryRun: false`, the defaults' as loaded, not the session's `true` (decision 8); the "session's dryRun saved" and V3 defects become two `FIXED` lines (decision 8, V3), the whole-file save to `./hardn.yml` staying a known defect.
- `menu/dry_run_disable.golden`: the same lines; the file as written is unchanged (`false` either way).
- `menu/dry_run_return.golden`: the save's defect line without "the session's dryRun with it"; the file unchanged.

No other golden changed. The ops goldens' note "as cmd/hardn/main.go:158 does for -n" (44 files under pkg/characterization/testdata, and the 16 frozen copies under pkg/screens/dns/testdata/legacy) and `setup-sudo-env`'s "called at cmd/hardn/main.go:440" (4) cited, at the base 1467f0d, the comment above `cfg.DryRun = dryRun` (`:158-159`) and the one above the `setup-sudo-env` refusal (`:440-441`). This unit moved them, to `:163-167` (`ResolveDryRun`) and `:452-455`; `:158` now points at the load failure's error line. Left as they are: only the line numbers changed, not what they record, and this unit regenerates only the goldens whose behaviour it changes.

## 8. Open items

1. **Do not release this unit without unit (d)**: plan only from hardn.yml now reaches the legacy menus with `-n`'s gaps (§2), and the notes promise item 6's removal. The release notes' header says so; docs/configuration.md names the gaps. *Unit (d) built: the gaps are refused; (b) to (d) ship together.*
2. Unit (c): the indicator's words and the source after them, from `DryRunSource`. *Built: `next/s2b-menu`.*
3. Unit (d): the line "item 6 removed" in docs/configuration.md (§4.1), its "Not yet in every menu" line removed, and the release notes' last bullet and header made current. *Done by `next/s2b-replace`.*
4. `config.Save` and `config.Edit` with no setting named would write the session's mode; nothing calls them so (`steps.PlanSave` requires a setting). A later caller should name its settings, or this should write `dryRun` as loaded too.
5. The release workflow does not read docs/releases/unreleased.md: its notes are copied by hand at the release until plan Phase 7's `next/p7-release` decides.
6. README's "Dry run (mode)" row and its example still say what `-n` does alone: `next/p7-docs`'s.
7. The maintainer: §4.1's two, `confirm` and `rollback` under the file's plan only, and the item-6 line's deferral.

## 9. Size

About 195 lines of non-test Go added and 20 removed (`pkg/config` 105, `cmd/hardn` 48, `pkg/cmd` 36, `pkg/menu` 3 changed in place); about 520 lines of tests added and 9 removed (`mode_test.go` 260, `dryrun_test.go` 180, `pkg/cmd` 50, the harness and the characterization tests' FIXED lines 30); about 160 lines of docs (this page 120, the note 15, the release notes 14, dated notes on five pages). Over the guideline of about 500 changed lines because of the tests: every start, and each operation flag under each source, driven through the root command; the code alone is within it. Testdata: 4 goldens changed, 9 lines added and 7 removed. The review's fixes: about 40 lines of non-test Go added and 35 removed (live with no source, one default search path, the refusal's words), 13 and 12 of tests, and about 40 lines of docs.
