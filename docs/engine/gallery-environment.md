# Environment in the gallery, before any build (slice 9, unit (a): `environment`, `play environment`)

> **Unit:** refactor plan slice 9, unit (a), `next/s9-gallery`: "Gallery mock and full sequence, reviewed first" (plan §2, "Phases 3–5 together", §3.22). Slice table row 9: "Environment | No frame; **origin/main only** | Shared sudoers file; `/tmp` temp; `su -` probe | Wording". Decisions cited, not reopened: §3.17 (approved 2026-10-17: sudoers file names, the grant and `env_keep` in separate files); §3.21 (approved 2026-10-17: doas alongside sudo on Alpine); P-03 (decided 2026-10-23: one "Plan only" mode set at start); unit (d)'s rule (2026-10-22: "The area's menu items and flags go to the new control"); `-g` on the new control (2026-10-23). Proposals shown: [P-07](../refactor/proposals.md#p-07) (c) (the item's words), [P-01](../refactor/proposals.md#p-01) (a status row), [P-43](../refactor/proposals.md#p-43) (1) (both directions, for comparison). V-numbers: V16, V18, V20; side-effects rows 5, 17, 18 ([side-effects.md](../refactor/phase0/side-effects.md)); the `su` probe in [v032-reference.md](../refactor/phase0/v032-reference.md)'s V26 row.
> **Status:** built and tested on macOS, in process. No hardn binary ran. The mocks read simulated hosts; the characterization runs pkg/menu's Environment screen and `config.LoadConfig` in child test processes, and `MenuManager.SetupSudoPreservation` in process, on the real file system under a temp dir that stands for `/`, with a stand-in for `su`. The seven decisions of §5 are adopted (the maintainer, 2026-10-29), with the Made here calls as stated and the words of §6 (decision 7); decision 1 (a)'s open item settled by words after. Nothing is wired into `cmd/hardn`, `pkg/menu`, `pkg/engine`, `pkg/host` or `pkg/config`. *Since `next/s9-control` ([control-environment.md](control-environment.md); [sudoers.md](../safety/sudoers.md) §2.13 to §2.16): the Environment control is built on the engine, with the reader, the user lookup, origin/main's line and the findings on the change it needed, and the mocks run on it (§8); the mocks of the options not adopted are retired (§3.1); the doas row and the doas refusal take the settlement's words, proposed (§6).*

## 1. Review description

1. **Purpose and scope.** What origin/main's Environment screen, `-e` and `setup-sudo-env` do, and what `config.LoadConfig`'s `su -` probe does (§2); the slice's three defects confirmed, and others found (§7); a proposed Environment screen on the design system, drawn from what real code reads of fifteen simulated hosts, each drawing every decision's recommended option unless it names another, its one change on the real engine (§3); seven decisions (§5), adopted 2026-10-29. In scope: characterization tests (additions only), the gallery's `environment` mode (`environment.go`, its tests, the dispatch in `main.go` and `play.go`, `doas -C` in the fake host), their goldens, this page, a bullet in the design-system README's review guide and a line in P-07's entry.
2. **Behaviour changes:** none to hardn. No component, layout, engine, host, config or menu code changed. `fakehost.go` answers `doas -C <file>` (:572-575), every file passing, as `statushost.go:87` does, and `engine.go`'s `reads` counts it a read (:661-662), as it does visudo's checks. Alpine's doas files are added by the Environment hosts alone (`withDoasFiles`), so no other host's reading changes.
3. **Tests added.** `pkg/characterization/menu_environment_test.go`: `TestLegacyMenuEnvironment`, 12 cases. `environment_calls_test.go`: `TestLegacyEnvironmentKeep`, the 10 starts the operations goldens do not hold. `environment_notice_test.go`: `TestLegacyEnvLossNotice`, 9 cases. `internal/tools/uigallery/environment_test.go`: `TestEnvironmentGolden` (20 listing mocks on the core set and under LANG=C), `TestPlayEnvironmentGolden` (16 transcripts), `TestEnvHosts` (each host's screen held to `sudoers.Choose`, `Read` and `PlanFor`), `TestEnvironmentReader` (the gallery's reader held to what `PlanFor` writes, for any variables; item 1's Policy carrying the grant and the other variables; a hand-edited file named), `TestEnvironmentGrantCarried`, `TestEnvironmentDoas`, `TestEnvironmentNoTmpNoProbe`, `TestEnvironmentPlanOnlyWritesNothing`, `TestEnvironmentHeights`, `TestEnvironmentUsage`, `TestPlayEnvironmentStepsNotCut`. `TestPlayTouchesNothing` takes the new file as it comes. *Since `next/s9-control`: `TestEnvHosts` holds each host to the Environment control's reading and words; `TestEnvironmentReader` is gone with the gallery's reader; `TestEnvironmentGrantCarried` and `TestEnvironmentDoas` plan the control; the goldens of the retired mocks are deleted (§3.1).*
4. **Golden diffs.** Added only: `pkg/characterization/testdata/golden/menu/environment_*.golden` (12), `.../environment/keep_*.golden` (10) and `notice_*.golden` (9); `internal/tools/uigallery/testdata/golden/environment/*.golden` (20); `.../play/environment-*.golden` (16). No existing golden changed (`git diff --diff-filter=MDR 44f8766` on the testdata is empty).
5. **UI.** No component changed, so no deviation; freeze screenshots deferred (design-system README §9.16).
6. **Inferred screens.** Every Environment mock is "inferred from origin/main": there is no Figma frame, and v0.3.2 had no Environment screen ([interaction.md](../refactor/phase0/interaction.md):458, "added 834dc4b").
7. **New dependencies:** none.
8. **Per-distro notes:** none run here. Phase 2's VM runs showed `env_keep` in its own drop-in keeping `HARDN_CONFIG` under sudo 1.9.16p2 (Debian 13) and sudo-rs 0.2.13 (Ubuntu 26.04) ([vmtest.md](../safety/vmtest.md):248, :254, :302, :315). doas's `setenv` has not run on any host ([sudoers.md](../safety/sudoers.md) §4).
9. **`git status --short --ignored`** before each docs commit: the docs paths alone (three, then two after review), and an ignored `uigallery` binary a build had left at the root, not committed.

## 2. What origin/main does

Each claim is **run** (a golden of `TestLegacyMenuEnvironment`, `menu/environment_<case>.golden`; of `TestLegacyEnvironmentKeep`, `environment/keep_<case>.golden`; of `TestLegacyEnvLossNotice`, `environment/notice_<case>.golden`; an existing operations golden; or a named test) or **read** (the code cited). Line numbers are the trunk's (44f8766). The Environment files are origin/main's (9d46866) on the same lines, but for the pauses' "Press Enter" (DEV-050); `config.go`'s probe and notice are origin/main's 23 lines down (origin/main :289-345); on origin/main `-e` is at `cmd/hardn/main.go:403-410` and the subcommand at :420-449, without the trunk's dry-run refusal. In the goldens `/` is a temp dir on the real file system, each call recorded with hardn's path; the tests run as a user other than root.

### 2.1 Reaching it (run: `ops/setup_sudo_preservation/*.golden`, `notice_setup_sudo_env`; cmd/hardn `TestDryRunRefusesOperations`, `TestPlanOnlyRefusesOperations`; pkg/menu `TestPlanOnlyRefusals`)

- **Main menu 8**, "Environment" "Configure environment variable" (`main_menu.go:747`; origin/main `:459`), built as `NewEnvironmentSettingsMenu(m.menuManager, m.config)` (`:694`) over `service_factory.go:218-228`. **Under plan only** 8 opens as live: its item has no refusal, where 1 has (run).
- **`-e`/`--setup-sudo-env`** (`cmd/hardn/main.go:100`): refused under `-n` and under hardn.yml's `dryRun: true`, exit 2 (run). Otherwise, past the root check (`:141-152`) and `LoadConfig` with its probe (`:156`), `SetupSudoPreservation` (`:422-428`): "[SUCCESS] Sudo environment configured to preserve HARDN_CONFIG", or "[ERROR] Failed to configure sudoers: <err>" and exit 1 (read; each `keep_*` golden says which). `-r` returns before it (`:243-273`), so `-r -e` never runs `-e` (read).
- **`setup-sudo-env`** (`:439-476`): its dry-run refusals read hardn.yml through `config.FileDryRun` alone, which runs no probe (run: `notice_setup_sudo_env`); then `detectOS`, the factory, `SetupSudoPreservation`. **No root check and no `LoadConfig`**, though its help says "This command must be run with sudo privileges." (`:447`; read).
- **All three take the user from the environment:** `SUDO_USER`, else `USER` (`file_environment_repository.go:135-138`); `-u` and hardn.yml's `username` play no part (read).

### 2.2 The screen (run: `environment_open`, `_open_unset`, `_open_user`, `_open_root`, `_open_no_user`, `_open_commented`, `_open_elsewhere`)

- Cleared, the green `~~~` line, "Environment Variable Settings"; "- Current HARDN_CONFIG: <value>" in green, or "- HARDN_CONFIG environment variable is not set"; "- Sudo preservation: Enabled" (green) or "Disabled" (red); "Select an option"; "1) Setup sudo environment preservation" "Configure sudo to preserve HARDN_CONFIG", "2) Show environment variables guide" "Learn how to set up environment variables", "0) Return"; "❯ Enter selection [0-2 or q]:".
- **"Current HARDN_CONFIG" is hardn's own environment** (`environment_settings_menu.go:37`): under sudo, what `env_keep` let through. The screen never says whose file it read, or which hardn.yml hardn loaded.
- **The status is a substring of one file:** `/etc/sudoers.d/<SUDO_USER, else USER>` holding `env_keep += "HARDN_CONFIG"` (`file_environment_repository.go:104-124`), read twice per draw, 4 calls: the repository's `GetEnvironmentConfig` checks it for a field nothing uses (`:141-147`), then the service checks it again (`environment_service.go:53-65`). So:
  - `USER` alice, no `SUDO_USER`: alice's file (`_open_user`);
  - `USER` root: `/etc/sudoers.d/root`, Disabled, while alice's file keeps it (`_open_root`);
  - no user: no read, Disabled (`_open_no_user`; `environment_service.go:60-62`);
  - the line commented out: **Enabled** (`_open_commented`);
  - the line in the trunk's `/etc/sudoers.d/hardn-env-alice`: **Disabled** (`_open_elsewhere`), so once unit (b) writes its file the legacy screen says Disabled;
  - a failed read: Disabled (`environment_settings_menu.go:144-148`, read).
- No draw writes anything (every menu golden: `/` afterwards unchanged, `./hardn.yml` not written).

### 2.3 Item 1, keep the variable (run: `environment_keep_not_root`, `environment_keep_dry_run`; `keep_*`; `ops/setup_sudo_preservation/*`, `ops/configure_sudo/*`)

- "- Setting up sudo environment preservation..." first. For a user other than root: "⭘ This operation requires sudo privileges." and "- Please run: sudo hardn setup-sudo-env", a pause, the screen again, live and under dry run alike. **hardn never reaches this branch:** `cmd/hardn` refuses a user other than root first (`main.go:148-152`).
- **As root** (read, not run: `:82`): under dry run "- [DRY-RUN] Would configure sudo to preserve HARDN_CONFIG environment variable", no call (`:86-87`); otherwise `MenuManager.SetupSudoPreservation` (`:90`), then "✓ Successfully configured sudo to preserve HARDN_CONFIG" or "✗ Failed to configure sudo: <err>" (`:91-95`). It is the call `-e` and `setup-sudo-env` make (`cmd/hardn/main.go:423`, `:470`), recorded by `TestLegacyEnvironmentKeep`.
- **The call** stats and reads the user's file, stats `/etc/sudoers.d`, then stats and reads the user's file again. With the substring there, nothing is written and the call succeeds (`keep_already`), for a commented-out line too (`keep_commented`). Otherwise it writes the whole new file to `/tmp/hardn_sudoers_temp` (0440), runs `visudo -c -f` on it, removes it, and writes `/etc/sudoers.d/<user>` (0440) in place with `os.WriteFile`, nothing backed up (`keep_new`; `file_environment_repository.go:72-97`; V18): the only sudoers write origin/main checks (side-effects row 18).
- **With the grant there,** the line is appended to the grant's file (`ops/setup_sudo_preservation/debian.golden`, step 9), and the next `ConfigureSudo` overwrites that file, the line lost (`ops/configure_sudo/debian.golden`; V16, A13).
- **The failures and odd starts:**
  - visudo refusing: "invalid sudoers configuration: exit status 1", visudo's output not passed on, the temp file removed, nothing written (`keep_visudo_fails`);
  - a directory at the temp name: "failed to create temporary sudoers file at /tmp/hardn_sudoers_temp: open /tmp/hardn_sudoers_temp: is a directory", so any local user can stop `-e`, `setup-sudo-env` and item 1 (`keep_tmp_dir`; V20);
  - a symlink at the temp name: its target (`/etc/hosts` in the test) is overwritten with the sudoers lines, visudo runs on the link, and the remove takes the link alone (`keep_tmp_symlink`; V20). `fs.protected_symlinks` would refuse root's open on Debian and Ubuntu [I, not run];
  - no `/etc/sudoers.d` (a doas-only Alpine [I]): "sudoers.d directory does not exist; your system may not support sudo drop-in configurations"; nothing looks for doas (`keep_no_sudoers_d`);
  - `USER` root and no `SUDO_USER` (a root login; under doas too, which names its caller in `DOAS_USER` [I]): `Defaults:root env_keep += "HARDN_CONFIG"` in `/etc/sudoers.d/root` (`keep_root`);
  - no user at all: success with no call, so each caller says it is done (`keep_no_user`; `environment_service.go:45-47`);
  - `alice.smith`: `/etc/sudoers.d/alice.smith`, a name sudo skips for its dot [I, sudoers(5)] (`keep_dotted`; V16);
  - a `Stat` error other than not-exist reads as no file, so the env line alone would replace the user's file, grant included (`file_environment_repository.go:50-66`; read, not run).

### 2.4 Item 2, the guide (run: `environment_guide`)

- "Environment Variables Guide"; three headings, each printed after an empty bold span with the heading unstyled, as the heading is in the format string and `style.Bolded("", style.Blue)` its argument (`environment_settings_menu.go:124`, `:129`, `:134`): "HARDN_CONFIG Environment Variable", "Using with sudo", "For persistent configuration:"; under them "export HARDN_CONFIG=/path/to/your/config.yml", "sudo hardn setup-sudo-env", "echo 'export HARDN_CONFIG=/path/to/config.yml' >> ~/.bashrc"; a pause; the screen again.
- It names neither `-f` nor doas. Its `~/.bashrc` may not reach the probe's login shell: Debian's and Ubuntu's stock `~/.bashrc` returns early when not interactive [I, not run].

### 2.5 An invalid key, 0, q (run: `environment_invalid`, `environment_return`, every open case's q)

- An invalid key: "✗ Invalid option. Please try again.", a pause, the screen again. 0 and q return; nothing is saved.
- Each option but 0 and q draws the screen again by calling itself (`:101`, `:106`, `:115`), a call deeper each time.

### 2.6 The `su -` probe and its notice (run: `notice_not_sudo`, `_kept`, `_lost`, `_lost_unset`, `_profile_prints`, `_su_fails`, `_no_sudo_user`, `_doas`, `_setup_sudo_env`)

- **When:** `SUDO_UID` set, `HARDN_CONFIG` empty, `SUDO_USER` set (`config.go:312-331`). Then, from root's process, `su - alice -c "echo $HARDN_CONFIG"` (`:336`; `notice_lost`): alice's login shell and profile [I] (side-effects row 5).
- **Any output reads as set** (`:344`): a profile's greeting gives the notice (`notice_profile_prints`); empty output gives none (`notice_lost_unset`), nor does `su` failing (`notice_su_fails`).
- **No probe** off sudo (`notice_not_sudo`), with the variable kept (`notice_kept`), with `SUDO_USER` unset (`notice_no_sudo_user`), under doas, which nothing on origin/main reads (`notice_doas`), or for `setup-sudo-env`'s read (`notice_setup_sudo_env`).
- **The notice:** a blank line, "NOTICE: The HARDN_CONFIG environment variable is set in your user environment" / "but is not preserved when using sudo. To fix this, run:" / "  sudo hardn setup-sudo-env" / "Then run your command again.", a blank line (`:350-356`). It prints **before the file is looked for**, so under `-f` too, the file loading after it (`notice_lost`).
- **On every `LoadConfig`:** at start (`cmd/hardn/main.go:156`), and each time the User Management screens read a user (`getConfigForSSHKeyPath`, `os_user_repository.go:20-35`, through `GetExtendedUserInfo` from `user_menu.go:68`, `:155`, `:208` and `user_options.go:329`, `:417`), by `fmt.Println`, which the `logging.SetSilentMode` there (`:22`) does not silence (read). At start the main menu's first clear wipes it ([interaction.md](../refactor/phase0/interaction.md):160, W8).
- v0.3.2 had no probe ([v032-reference.md](../refactor/phase0/v032-reference.md):629, the V26 row).

## 3. The mocks

### 3.1 What to run

From the repository root, with `GOTOOLCHAIN=local GOWORK=off` (add `-profile plain`, `-ascii`, `-width 60` or `-theme light`):

```text
go run ./internal/tools/uigallery environment                          # every mock under its note
go run ./internal/tools/uigallery environment screen-debian13 origin   # some of them; -list names them
go run ./internal/tools/uigallery play environment                     # the hosts' menu
go run ./internal/tools/uigallery play environment/debian13            # the screen on one host
go run ./internal/tools/uigallery play -sequence dry-run environment/debian13   # plan only
```

`play` refuses `-fail` there, and any `-sequence` but `normal` and `dry-run` (`TestEnvironmentUsage`). The screen on `debian13`, plain, at 80 columns (`screen-debian13`):

```text
# Environment Variable Settings ################################

# HARDN_CONFIG -------------------------------------------------

   -  User                 alice
  [ ] Under sudo           Not kept
   -  Value                Not set

  Select an option

   1) Under sudo           Keep HARDN_CONFIG
   2) Guide                How to set HARDN_CONFIG

   0) Return

  > Enter selection [0-2 or q]:
```

origin/main's title; one section, "HARDN_CONFIG": **User**, who ran hardn through the tool; **Under sudo** or **Under doas**, whether hardn's files keep the variable for that user; **Value**, `HARDN_CONFIG` as this hardn sees it. Item 1 is labelled with the state row's key, as Backup's item 1 is: "Keep HARDN_CONFIG", or once hardn's files keep it "Stop keeping HARDN_CONFIG" (decision 5 (a)), marked Lockout risk where its plan is; item 2 is the guide; no status row (`envReading.subMenu`, `environment.go:614-634`). Each draw reads the host again.

The hosts are `web-01.example.net`; alice ran hardn through the host's tool unless the row says otherwise (each host's environment: `environment.go:181-190`). **Each host draws every decision's recommended option** (§5) unless its row names another, not recommended (`envTreatment`, whose zero value is the recommended set).

| Host | What it is | Rows | Item 1 |
|---|---|---|---|
| `debian13` | Debian 13, sudo 1.9.16p2; sudo dropped alice's `HARDN_CONFIG` | alice; Not kept; Not set | writes `hardn-env-alice`, y/N |
| `ubuntu2604` | Ubuntu 26.04, sudo-rs 0.2.13 (`Choose`'s flavour `SudoRS`) | as `debian13` | as `debian13` |
| `alpine324` | Alpine 3.24 after setup-alpine: `20-wheel.conf`, `permit persist :wheel`, and the stock `doas.conf`, its rule commented out (sudoers.md §3, from alpine-conf's `setup-user` and aports 3.24-stable; not run, sudoers.md §4); `DOAS_USER` alice; hardn has no rule for alice | alice; Under doas Not kept; Not set | refused before asking (decision 1 (a)) |
| `alpine324-rule` | the same, decision 1 (b), not recommended | the same; Lockout risk | writes `permit persist setenv { HARDN_CONFIG } alice` after the Warning. *Retired in `next/s9-control`* |
| `alpine324-setenv` | `alpine324` after alice did what its refusal says: `20-wheel.conf` says `permit persist setenv { HARDN_CONFIG } :wheel`, and doas kept `/home/alice/hardn.yml` [I: doas.conf(5), sudoers.md:94; `setenv` has run on no host] | alice; Under doas **Not kept**; `/home/alice/hardn.yml` | refused again, in the same words (decision 1 (a)'s cost) |
| `alpine323-cloud` | *Retired in `next/s9-control`.* Alpine 3.23's cloud image, as the maintainer's VM: `wheel.conf`, `permit nopass :wheel` (vmtest.md:562, :570; it ships in the image, :580), which sorts after `hardn-alice.conf`; its `doas.conf` the package's, not read on the VM [I]; decision 1 (b), not recommended, as under (a) item 1 refuses here as on `alpine324` | as `alpine324-rule` | as `alpine324-rule`, the plan carrying the later-file finding |
| `doas-overridden` | the cloud image with hardn's rule for alice, `permit persist setenv { HARDN_CONFIG } alice` | Kept in `status.warn`, "/etc/doas.d/wheel.conf may override hardn's rule"; Not set; Lockout risk | "Stop keeping HARDN_CONFIG": rewrites the rule without `setenv`, lockout-risk, as the rule asks a password |
| `pve9` | Proxmox VE 9: no sudo [I] (sudoers.md:95), no doas [I]; root logged in | Under sudo Not Applicable; Not set | refused: "sudo isn't installed here." |
| `kept` | `hardn-env-alice` there; sudo kept `/home/alice/hardn.yml` | alice; Kept; `/home/alice/hardn.yml` | "Stop keeping HARDN_CONFIG": removes `hardn-env-alice`, y/N |
| `kept-only` | the same, decision 5 (b), not recommended | the same; item 1 "Keep HARDN_CONFIG" | an empty plan. *Retired in `next/s9-control`* |
| `root-login` | Debian 13; root logged in, no `SUDO_USER` | User "Not run with sudo" in `status.warn`; Not set | refused before asking |
| `grant` | `hardn-grant-alice` (`NOPASSWD`) there, no env file | alice; Not kept; Not set | the gallery's reader carries the grant: writes `hardn-env-alice` alone |
| `grant-no-reader` | the same, planned as real code can today (an env-only Policy) | the same; Lockout risk | removes `hardn-grant-alice` too (§3.2). *Retired in `next/s9-control`* |
| `legacy` | origin/main's `/etc/sudoers.d/alice`, its grant and the line (`ops/setup_sudo_preservation/debian.golden`); sudo kept the variable | Kept, "by /etc/sudoers.d/alice"; `/home/alice/hardn.yml` | writes `hardn-env-alice`, the review listing the legacy-file finding (decision 3 (a)) |
| `legacy-hardn-only` | the same, decision 3 (b), not recommended | Not kept; `/home/alice/hardn.yml` | writes `hardn-env-alice`. *Retired in `next/s9-control`* |

*Since `next/s9-control`* ([control-environment.md](control-environment.md) §2.12, §10 decision 13): ten hosts, each read and planned by the Environment control. The five that drew an option not adopted (`alpine324-rule`, `alpine323-cloud`, `kept-only`, `grant-no-reader`, `legacy-hardn-only`) are retired with their goldens; git keeps them at af5904a, and this page records what they showed. On the doas hosts the state row reads as hardn's own state, "Not kept by hardn", "Kept by hardn" (`alpine324`, `alpine324-setenv`, `doas-overridden`), and the refusal adds "Under doas stays Not kept by hardn." (the settlement, proposed).

| Listing mock | What it shows |
|---|---|
| `screen-<host>` | The screen on each host, as above |
| `origin` | origin/main's screen in its own words on the design system, on `debian13`, under the proposed section header (origin/main has none); its long labels put every description under its label |
| `item1-doas-refuse` | `alpine324`'s screen, then item 1's refusal (decision 1 (a), recommended) |
| `item1-doas-rule` | `alpine324-rule`'s screen, then what item 1 shows above its plan: the Warning "The rule also lets alice use doas, in wheel or not.", the review's rows (decision 1 (b), not recommended). *Retired in `next/s9-control`* |
| `guide` | Item 2's words |
| `refusals` | Item 1's refusals on `root-login`, `pve9` and `alpine324`, and on a host whose `hardn-env-alice` was edited by hand to grant alice sudo as well (`editedHost`) |

| Play golden | Keys | What it shows |
|---|---|---|
| `play/environment-keep` | `environment/debian13`: 2, 1, y, 0 | The guide; the review (User alice, Under sudo Kept); the plan, "Keep HARDN_CONFIG", `/etc/sudoers.d/hardn-env-alice` and its diff; y/N; the step; "Operation Successful", "Takes effect at the next sudo hardn."; the screen read again, Kept, Value still Not set, item 1 now "Stop keeping HARDN_CONFIG"; also under LANG=C |
| `play/environment-sudo-rs` | `environment/ubuntu2604`: 1, y, 0 | The same plan and words under sudo-rs |
| `play/environment-kept` | `environment/kept`: 1, y, 0 | "Stop Keeping HARDN_CONFIG": the file removed, no risk, y/N, "Backup of /etc/sudoers.d/hardn-env-alice kept in" its run; Not kept after, Value still `/home/alice/hardn.yml` |
| `play/environment-kept-only` | `environment/kept-only`: 1, 0 | The review, then "Nothing needs changing." *Retired in `next/s9-control`* |
| `play/environment-root-login` | `environment/root-login`: 1, 0 | Refused before asking |
| `play/environment-pve9` | `environment/pve9`: 1, 0 | Refused before asking, "sudo isn't installed here." |
| `play/environment-plan-only` | `-sequence dry-run environment/debian13`: 1, 0 | The plan, then "No actual changes were made to your system." |
| `play/environment-grant` | `environment/grant`: 1, y, 0 | `hardn-env-alice` alone written, no risk; the grant carried |
| `play/environment-grant-no-reader` | `environment/grant-no-reader`: 1, 0, 0 | The env-only Policy: hardn's boot unit first, then the change removing `hardn-grant-alice` and writing `hardn-env-alice`, Lockout risk, the hostname to type; 0, "Operation Cancelled". *Retired in `next/s9-control`* |
| `play/environment-doas-refuse` | `environment/alpine324`: 1, 0 | Refused before asking: "hardn has no doas rule for alice to keep HARDN_CONFIG on. …" |
| `play/environment-doas-setenv` | `environment/alpine324-setenv`: 1, 0 | Not kept beside Value `/home/alice/hardn.yml`; refused again, in the same words |
| `play/environment-doas-rule` | *Retired in `next/s9-control`.* `environment/alpine324-rule`: 1, `web-01`, 0 | The Warning; hardn's boot unit; `/etc/doas.d/hardn-alice.conf`, `permit persist setenv { HARDN_CONFIG } alice`; the hostname typed; the revert timer, "doas hardn confirm"; "Takes effect at the next doas hardn."; Kept after, item 1 stopping, Lockout risk |
| `play/environment-wheel` | *Retired in `next/s9-control`.* `environment/alpine323-cloud`: 1, `web-01`, 0 | As `environment-doas-rule`, the review's state row Kept in `status.warn` and, under it, the later-file finding "/etc/doas.d/wheel.conf may override hardn's rule"; the screen after the same |
| `play/environment-doas-stop` | `environment/doas-overridden`: 1, `web-01`, 0 | "Stop Keeping HARDN_CONFIG": hardn's boot unit; the rule rewritten `permit persist alice`, Lockout risk; the hostname typed; the revert timer; its backup kept; Not kept after, item 1 keeping, Lockout risk |
| `play/environment-legacy` | `environment/legacy`: 1, y, 0 | Kept by `/etc/sudoers.d/alice`, yet item 1 writes `hardn-env-alice`, the review listing the legacy-file finding |
| `play/environment-hosts` | `environment`: 7, 0, 0 | The fifteen hosts' menu, `doas-overridden`'s screen, back. *Since `next/s9-control`: 5, 0, 0, the ten hosts' menu* |

Each play ends with the fake's record of what changed. Checks are reads and left out of it (`engine.go:647-668`). Listed once by a test not kept, the apply on `debian13` ran `visudo -c`; wrote the copy `/run/hardn/<id>/sudoers-validate/hardn-env-alice`, 0440 root:root, and ran `visudo -cf` on it; wrote `/etc/sudoers.d/hardn-env-alice`, 0440; ran `visudo -c` again. On `alpine324-rule` it ran `doas -C` on `/etc/doas.conf` and `20-wheel.conf`, then on the copy (0640), wrote the rule, then `doas -C` on all three, as `sudoers.Apply` documents (`apply.go:104-114`). The playable main menu's 8 still says the gallery has no screen for it (`play.go`'s `links`, unchanged); the Environment mocks start at `play environment`.

### 3.2 hardn's grant, with the reader and without

`sudoers.PlanFor` takes the user's whole state, grant and variables, and plans each of hardn's files from it (`plan.go:289-330`): a Policy without a grant removes hardn's grant; one without variables removes `hardn-env-<stem>` (the `Policy` doc, `sudoers.go`). Nothing in `pkg/host/sudoers` reads hardn's files back into a Policy. So item 1 planned with real code alone, an env-only Policy, **removes hardn's grant**. On `grant-no-reader` that is a lockout-risk change (`plan.go:316-319`, `:393-398`): hardn's boot unit installed first, the change's diff `--- /etc/sudoers.d/hardn-grant-alice` `+++ /dev/null` beside the new env file, the hostname to type (`play/environment-grant-no-reader`, declined there). With the gallery's reader (`currentPolicy`, §8) the same host plans `hardn-env-alice` alone, no risk, and after the apply `hardn-grant-alice` is byte for byte as it was (`TestEnvironmentGrantCarried`; `play/environment-grant`). V16's separation holds only with a reader. **The variables are the user's whole list too:** the reader takes whatever list hardn's `env_keep` line or doas rule's `setenv` holds, as `PlanFor` writes any (`sudoers.go:343-359`), and item 1 adds `HARDN_CONFIG` to it or takes it out, the others carried: with `HARDN_CONFIG EDITOR` kept, stopping leaves `env_keep += "EDITOR"` (`TestEnvironmentReader`; `envTreatment.target`). Slice 5 needs the same reader the other way round: a grant planned with no variables removes `hardn-env-<stem>`. *Since `next/s9-control`: the reader is `sudoers.PolicyOf`, and the Environment control plans the user's whole Policy with it, the grant and the other variables carried (control-environment.md §2.6, §2.7; `TestGrantCarried`); `grant-no-reader` and its play are retired, as no code plans an env-only Policy for item 1 now.*

## 4. Heights (run: `TestEnvironmentHeights`, Unicode and ASCII, at 80×24 and at 80×40)

| Screen | Lines to the prompt | 80×24 | 80×40 |
|---|---|---|---|
| origin/main's (counted in `environment_open`, `environment_open_unset`) | 14 as printed; items 1 and 2 are 83 and 85 columns wide, so 16 rows on an 80-column terminal (counted, not run in a terminal) | fits | fits |
| origin/main's words on the design system (`origin`) | 17 | fits | fits |
| No User row or no state row (`pve9`, `root-login`) | 15 | fits | fits |
| The three rows (`debian13`, `ubuntu2604`, `alpine324`, `alpine324-setenv`, `kept`, `kept-only`, `grant`, `legacy`, `legacy-hardn-only`) | 16 | fits | fits |
| A Lockout risk line under item 1 (`alpine324-rule`, `alpine323-cloud`, `grant-no-reader`) | 17 | fits | fits |
| A may-override line and a Lockout risk line (`doas-overridden`) | 18 | fits | fits |

*Since `next/s9-control`* the retired hosts' rows (`alpine324-rule`, `alpine323-cloud`, `grant-no-reader`, `kept-only`, `legacy-hardn-only`) are gone from `TestEnvironmentHeights`; the others' heights are unchanged ("Not kept by hardn" fits the row).

Each mock is the same number of lines at both heights: one form at every height. A sub-menu has no compact form, and none is proposed (the maintainer, 2026-10-23, of the main menu). At 60 columns rows and items stack, a line each more (the goldens' `truecolor-dark-unicode-60` cells).

## 5. Decisions for the maintainer

In dependency order: a decision whose answer changes another decision, or another unit, comes first and says what it gates. Each is one line, the question, the recommended option and why, what it gates and blocks; its options under it, lettered, the recommended one first. The mocks draw each recommended option unless a host names another (§3.1). Already decided and not reopened: §3.17, §3.21, P-03, unit (d)'s rule for flags, `-g`'s behaviour, and slice 9's row, whose defects fixed include the `su -` probe (plan, slice table row 9).

**Adopted (the maintainer, 2026-10-29):** "Adopt all seven slice 9 Environment recommendations in gallery-environment.md §5, and the Made here calls as stated." Every recommended answer below is adopted, and every Made here call as stated. What follows from them:

- On doas, item 1 refuses where hardn has no rule for the user, and slice 9 writes no doas rule (1).
- A root login with no `SUDO_USER` or `DOAS_USER` refuses (2).
- origin/main's `/etc/sudoers.d/<user>` is read, its line counted; the file is slice 5's (3).
- Unit (d) removes the `su -` probe and its notice, and `next/p3-startup`'s row no longer rebuilds the notice (4).
- Item 1 turns both ways (5).
- Environment has no main-menu row (6, [P-01](../refactor/proposals.md#p-01)).
- §6's words are decided (7), main menu 8's description "Keep HARDN_CONFIG" ([P-07](../refactor/proposals.md#p-07) (c)); as drawn, `pve9` says "Under sudo" where no tool is installed.
- `-e` and `setup-sudo-env` refuse a lockout-risk plan; the revert timer is not armed unasked, and `-n` shows the plan.

**Settled after (the maintainer, 2026-10-29):** "Settle decision 1 (a)'s open item by words: the doas row reads as hardn's own state (e.g. "Not kept by hardn") and the refusal says the row will not change. Then start slice 9 unit (b)." The reader stays on hardn's files (sudoers.md §6); unit (b) proposes the words. *Proposed in `next/s9-control`* ([control-environment.md](control-environment.md) §5 to §7): on doas the row "Kept by hardn" and "Not kept by hardn", the note "by hardn" after the decided value; the refusal "hardn has no doas rule for alice to keep HARDN_CONFIG on. Add setenv { HARDN_CONFIG } to the rule that lets alice use doas. Under doas stays Not kept by hardn."

1. **doas, where hardn has no rule for the user: refuse, or write one?** *Recommended:* (a) refuse, as keeping a variable then grants nothing. *Gates* 5, 7 and the reach of unit (b)'s reader on doas; *blocks* unit (b) on doas only.
   - (a) Refuse before asking, naming the rule to change (`alpine324`, `play/environment-doas-refuse`). **Its cost:** once the user does what the refusal says, `setenv` on the rule that lets them use doas (on a stock host `20-wheel.conf`'s group rule), the screen still reads hardn's files alone: Under doas Not kept beside a Value doas kept, and item 1 refuses again in the same words (`alpine324-setenv`, `play/environment-doas-setenv`; run: `TestEnvironmentDoas`). That is the contradiction 3 (b) is not recommended for. Unit (b) can avoid it by reading the rule doas applies for the user (another file's rules, which `pkg/host/sudoers` does not read: sudoers.md §6), or by words: the row as hardn's own state (for example "Not kept by hardn") and the refusal saying the row will not change. **Settled by words** (2026-10-29, above); unit (b) proposes them.
   - (b) Write `permit persist setenv { HARDN_CONFIG } <user>` after the Warning (`alpine324-rule`, `play/environment-doas-rule`): item 1 then works on a stock setup-alpine host, but the rule grants doas beyond `wheel`, and as a password grant it is lockout-risk (`plan.go:393-398`), so the hostname is typed, hardn's boot unit installed and the revert timer armed; on the cloud image `wheel.conf`'s later rule wins anyway [I] (`alpine323-cloud`, `play/environment-wheel`), and its accounts have no password (vmtest.md:580).
   - (c) The same rule with `nopass` (not mocked): not lockout-risk, but passwordless doas for the user.
   - Under each, once hardn has a rule for the user (nothing in hardn writes one today), item 1 adds `setenv` to it: lockout-risk for a password rule, plain for `nopass` (run: `TestEnvironmentDoas`; `plan.go:393-398`).
2. **The user at a root login, with no `SUDO_USER` or `DOAS_USER`: refuse, or ask whose?** *Recommended:* (a) refuse, as the variable is lost by the user who runs sudo hardn, and only then is it known whose. *Gates* 7 and `-e` at a root login; *blocks* unit (b)'s user lookup.
   - (a) Refuse before asking, "hardn wasn't run with sudo, so there's no user to keep it for. Run sudo hardn as that user." (`root-login`, `play/environment-root-login`).
   - (b) Ask for a user name, checked by `sudoers.CheckUser` and looked up (not mocked): root can then set it for another user.
   - origin/main's `USER` fallback is a defect, not an option: it writes `Defaults:root` (`keep_root`).
3. **origin/main's `/etc/sudoers.d/<user>`, the user's grant and the `env_keep` line in one file (V16): read its line here, count hardn's files alone, or migrate it?** *Recommended:* (a) read it and leave the file to slice 5, so the row agrees with Value. *Gates* slice 5's scope and 7; *blocks* unit (b)'s reading of that file.
   - (a) Read its line, comments excluded, as "Kept by /etc/sudoers.d/alice" (`legacy`, `play/environment-legacy`), item 1 still writing `hardn-env-alice`: the file carries the grant, which is slice 5's, and the trunk's legacy grant write still drops the line (`ops/configure_sudo/*.golden`).
   - (b) Count hardn's files alone (`legacy-hardn-only`): simpler, but the row says Not kept while Value shows that sudo kept it.
   - (c) Migrate it here, taking the line out of the grant's file: `pkg/host/sudoers` never edits a file that is not hardn's (sudoers.md §2 item 5), so a new kind of change, to a file that grants.
4. **Slice 9 removes the `su -` probe and its notice (plan, slice row 9): drop "the `HARDN_CONFIG` notice" from `next/p3-startup`'s row, the screen's state row saying what the notice said?** *Recommended:* yes. *Gates* `next/p3-startup`'s row and `setup-sudo-env`'s load of hardn.yml (Made here); *blocks* unit (d)'s change to `config.LoadConfig`.
   - Why the probe goes: it runs the user's login shell from root on every `LoadConfig` (side-effects row 5), reads a profile's greeting as the variable set (`notice_profile_prints`), never runs under doas (`notice_doas`), and in the menu the first clear wipes the notice (W8).
   - No: `next/p3-startup` keeps a notice, rebuilt on the prompt kit without the probe; what it would rest on is not mocked.
   - Leaving the probe itself to `next/p3-startup` is not an option here: it changes slice 9's agreed scope (plan :707). The probe would run until `next/p3-startup` lands, which waits for `next/p7-install` (plan :86), and once `setup-sudo-env` loads hardn.yml through `LoadConfig` (`cmd/hardn/main.go:156` for the root command), it would print "run: sudo hardn setup-sudo-env" during `setup-sudo-env` itself, which today runs no probe (`notice_setup_sudo_env`).
5. **Item 1's direction: both ways, or keep only?** *Recommended:* (a) both, so hardn can undo what it did, as Backup's item 1 turns both ways (decided 2026-10-27). *Gates* 7; *depends on* 1; *blocks* unit (b)'s plan for stopping.
   - (a) Both ways, "Stop keeping HARDN_CONFIG" once hardn's files keep it. On sudo it removes `hardn-env-<stem>`, or takes `HARDN_CONFIG` out of its list, no risk, y/N, its backup kept (`kept`, `play/environment-kept`). **On doas there is no env file:** stopping rewrites hardn's rule without `setenv`, and for a password rule that is lockout-risk, with the typed hostname, hardn's boot unit and the revert timer (`doas-overridden`, `play/environment-doas-stop`; run: `TestEnvironmentDoas`; `plan.go:320-331`, `:393-398`), under 1 (a) as under (b), for any password rule hardn has.
   - (b) Keep only, origin/main's one direction (`kept-only`, `play/environment-kept-only`: "Nothing needs changing."): one action, the file removed by hand to stop.
6. **A main-menu status row (P-01)?** *Recommended:* (a) no: whether sudo keeps `HARDN_CONFIG` is how hardn is run, so a row would tell the user nothing of how the host is hardened, and fewer main-menu rows are wanted (§3.15 as amended, open). *Gates* 7 and unit (b)'s `Check`.
   - Precedents: DNS's row was decided "Shown, not weighed" (plan :182, 2026-10-23); Backup has none (gallery-backup.md decision 1 (a), adopted 2026-10-27: "Backup has no status row on the main menu").
   - (a) No row.
   - (b) A row from the control's `Check`, "Environment" Configured "HARDN_CONFIG kept", shown and not weighed, as DNS's (not mocked): P-01's one list of controls, each with its row.
7. **Words: adopt §6?** *Recommended:* (a) §6 as drawn, as "under sudo" is wrong on doas hosts and the screen names the tool where it matters. *Last:* depends on 1, 2, 3, 5 and 6; *blocks* unit (c)'s words and unit (d)'s main menu item.
   - (a) §6 as drawn, main menu 8's description "Keep HARDN_CONFIG": P-07 (c) without "under sudo".
   - (b) P-07 (c) as proposed, "Keep HARDN_CONFIG under sudo": the tool most hosts run.
   - (c) The description by the host's tool, "under sudo" or "under doas": exact, but the main menu's words then change with the host.

**Made here** (engineering, stated; say if you'd rather otherwise):

- **The reader** (§3.2) is unit (b)'s, in `pkg/host/sudoers`, for slices 9 and 5 alike: without it item 1 removes hardn's grant. It reads any variable list hardn's files hold, and item 1's change carries the user's grant and other variables, adding or taking out `HARDN_CONFIG` alone (`TestEnvironmentReader`). A file of hardn's edited by hand refuses item 1 before asking, naming it (`refusals`; `TestEnvironmentReader`).
- **Who ran hardn:** `SUDO_USER` under sudo and sudo-rs, `DOAS_USER` under doas; root or unset is no one (`invoker`). Read from sudo(8), sudo-rs 0.2.13's `environment.rs` and OpenDoas 6.8.2's `env.c` [I, not run].
- **The plan's findings reach the review:** `steps.Sudoers` carries them (the gallery's `sudoersFindings` until then; *since `next/s9-control` `steps.Sudoers`' own, `sudoersFindings` gone*), their codes registered (`SUDOERS_LATER_FILE`, `SUDOERS_LEGACY_FILE`: `pkg/output/codes.go:409`, `:411`), their details reworded for the user (§7 items 21, 22): the later file's as the screen says it, "<file> may override hardn's rule".
- **A file read after hardn's** (the cloud image's `wheel.conf`) is reported, not refused: Kept in `status.warn` with "<file> may override hardn's rule" under it, on the screen and in the review, where the line is the plan's finding (`play/environment-wheel`, `doas-overridden`), since whether its rule matches the user is not read (sudoers.md §6). Under 1 (a) it arises only once hardn has a rule for the user.
- **Plan only:** the screen opens, and item 1 stops at its plan, "No actual changes were made to your system." (P-03; as Backup's decision 10; `play/environment-plan-only`, `TestEnvironmentPlanOnlyWritesNothing`). Main menu 8 already opens under plan only.
- **After a change:** "Takes effect at the next sudo hardn." The Value row is this hardn's environment, which the change cannot reach, so it stays as it was (`play/environment-keep`, `play/environment-kept`).
- **Words (§6), decided with decision 7:** the change "Keep HARDN_CONFIG" or "Stop keeping HARDN_CONFIG", the plan's heading and the step's line the same at any length of the user's name, which the review's User row says; item 1 "Stop keeping HARDN_CONFIG"; the User row "Not run with sudo" in `status.warn`; the guide "Then keep it with item 1.", item 1's label naming the tool.
- **`-e` and `setup-sudo-env`** go to the control (unit (d)'s rule) as `-g` did: applied without asking; `-n` shows the plan and writes nothing, where today it exits 2; a refused host exits non-zero. **They refuse, non-zero, naming main menu 8, a plan that is lockout-risk or writes the user's first doas rule**, as the typed hostname and the Warning are the screen's flow alone (`review`, `change`: `environment.go:830-881`): `-g` never meets one, as a DNS change never arms the revert timer (`cmd/hardn/engine.go:179-181`), and arming it unasked is the maintainer's to accept. `setup-sudo-env` gains the root check and loads hardn.yml, whose backup settings the engine takes (`cmd/hardn/engine.go:197`). Both spellings stay. Say if you'd rather keep `-n`'s refusal, or have `-e` arm the timer.

**Also, not mocked:** the screen reads hardn's files, not their effect: nothing runs `sudo -l` or evaluates doas's rule order (sudoers.md §6), so Kept means hardn's files keep it (decision 1 (a)'s cost, settled by words: the row says so). On doas a password rule of hardn's makes item 1 lockout-risk both ways (`plan.go:393-398`): stopping is played (`play/environment-doas-stop`), adding `setenv` run (`TestEnvironmentDoas`); unit (b)'s, with decisions 1 and 5.

## 6. Words

origin/main's where it has them; every other word is **decided** with decision 7 (§5, adopted 2026-10-29), unless its row says otherwise. The flow's own words are P-27's, origin/main's and Figma's ("Operation Successful", "Operation Cancelled", "No actual changes were made to your system.", P-27 (b)'s "Nothing needs changing."), with P-23's, P-24's and P-25's ("Type the hostname", the countdown), all still proposed; "Backup of <files> kept in" is decided (Backup's decision 8).

| Where | Words | Source |
|---|---|---|
| Main menu 8 | "Environment" "Keep HARDN_CONFIG" (today "Configure environment variable") | the label origin/main's; the description **decided** (decision 7; P-07 (c)'s "Keep HARDN_CONFIG under sudo" without "under sudo"); not drawn: the gallery's main menu is the screen fixtures' (`pkg/ui/screen/fixture.go:68`) |
| Title bar | "Environment Variable Settings" | origin/main (`environment_settings_menu.go:34`) |
| Section | "HARDN_CONFIG" | **decided** (decision 7) |
| Rows | "User" "alice", or "Not run with sudo" / "Not run with doas" (warn); "Under sudo" / "Under doas" "Kept", "Not kept" (warn), Not Applicable, Unknown; the note "by /etc/sudoers.d/alice"; "<file> may override hardn's rule" (warn); "Value" "<path>" / "Not set" | **decided** (decision 7), but "Under doas" "Not kept", reworded as hardn's own state in unit (b) (decision 1 (a), settled by words); Not Applicable and Unknown the components' state words. *Since `next/s9-control`, proposed:* on doas "Kept by hardn", "Not kept by hardn", both values (the control's `Words`, [control-environment.md](control-environment.md) §5) |
| Items | "Under sudo" "Keep HARDN_CONFIG" / "Stop keeping HARDN_CONFIG"; "Guide" "How to set HARDN_CONFIG"; "Return"; "Select an option"; "Enter selection" | item 1's label the state row's key, as Backup's (decided); heading and prompt Backup's (decided); the rest **decided** (decision 7) |
| Flows' title bars | "Keep HARDN_CONFIG", "Stop Keeping HARDN_CONFIG" | **decided** (decision 7; "Keep HARDN_CONFIG" from P-07 (c)) |
| Review | the section "HARDN_CONFIG", "User" and the state the change makes, as the screen's rows say it | **decided** (decision 7) |
| The doas rule's notice (decision 1 (b)) | "The rule also lets alice use doas, in wheel or not." | not adopted (decision 1 (a): no rule written); sudoers.md §6: "the screen must say so". *Retired with its mock in `next/s9-control`* |
| The plan's change | "Keep HARDN_CONFIG", "Stop keeping HARDN_CONFIG": the plan's heading and the step's line alike, at any length of the user's name (within `engine.TitleMax`) | **decided** (decision 7) |
| Questions | "Keep HARDN_CONFIG under sudo?", "Stop keeping HARDN_CONFIG under sudo?" ("under doas" on doas) | **decided** (decision 7) |
| After a change applied | "Takes effect at the next sudo hardn." ("doas hardn" on doas) | **decided** (decision 7) |
| Refusals, before asking | "sudo isn't installed here."; "hardn wasn't run with sudo, so there's no user to keep it for. Run sudo hardn as that user."; "<file> holds a rule hardn didn't write. Put hardn's back, or remove it."; "hardn has no doas rule for alice to keep HARDN_CONFIG on. Add setenv { HARDN_CONFIG } to the rule that lets alice use doas." | **decided** (decision 7), but the last, reworded in unit (b) to say the row will not change (decision 1 (a), settled by words); it is from `sudoers.ErrEnvNeedsGrant`'s rule (`sudoers.go:76`): doas keeps a variable through any rule that permits the user, a group's too (sudoers.md:54, :94), and hardn writes only the user's own. *Since `next/s9-control`* each is the control's `steps.Refusal` with its code ([control-environment.md](control-environment.md) §6); the last, proposed, adds "Under doas stays Not kept by hardn."; two edited files, proposed: "<file> and <file> hold rules hardn didn't write. Put hardn's back, or remove them." |
| Guide | "HARDN_CONFIG names the hardn.yml hardn reads. Set it in your shell profile:" / "export HARDN_CONFIG=/path/to/hardn.yml" / "Then keep it with item 1." | origin/main's guide shortened, its "sudo hardn setup-sudo-env" now item 1, whose label names the tool; **decided** (decision 7) |
| The plan's findings | the later file's "<file> may override hardn's rule", the screen's words (`sudoersFindings`); the legacy file's sudoers' detail as it is (`plan.go:377-390`) | the first **decided** (decision 7); the second not written for this screen, unit (b)'s (§7 item 22). *Since `next/s9-control`, proposed:* the legacy file's "<file> may override hardn's rule" (read after hardn's rule), "<file> may still let <user> use sudo" (hardn grants the user nothing), the two joined, "<file> may override hardn's rule and still let <user> use sudo", or "<file> is from an earlier hardn; hardn's rule wins"; a line after `/etc/sudoers`' includedir "Line <n> of /etc/sudoers may override hardn's rule" ([sudoers.md](../safety/sudoers.md) §2.16, §7) |
| The gallery's, dev-only | "Environment hosts", "Select a host", "Enter your choice", each host's label, about and note | the gallery's |

Settled with decision 7, as drawn: `pve9`'s state row and item 1's label say "Under sudo" where no tool is installed (`underKey`).

## 7. Defects

The slice's three:

| Listed | Verdict | Evidence |
|---|---|---|
| Shared sudoers file | **Confirmed (V16).** origin/main appends the `env_keep` line to `/etc/sudoers.d/<user>`, the file its grant overwrites, so the line is lost; a dotted name is a file sudo skips [I]. **Corrected in the mocks:** `hardn-env-<stem>` beside `hardn-grant-<stem>` (§3.17), **but only with a reader** (§3.2); real code alone removes the grant | run: `ops/setup_sudo_preservation/*.golden`, `ops/configure_sudo/*.golden`, `keep_dotted`; run: `TestEnvironmentGrantCarried`, `play/environment-grant`, `play/environment-grant-no-reader` |
| `/tmp` temp file | **Confirmed (V20).** `os.TempDir()/hardn_sudoers_temp`, written as root by `os.WriteFile`: a planted symlink's target is overwritten, a planted directory stops every caller. **Corrected in the mocks** by `pkg/host/sudoers`: each candidate copied into `/run/hardn/<id>/sudoers-validate/` and checked there | run: `keep_tmp_symlink`, `keep_tmp_dir`; side-effects §9 D2; run: `TestEnvironmentNoTmpNoProbe` (no call under `/tmp`); read: `apply.go:104-114`, sudoers.md §2 item 8 |
| `su -` probe | **Confirmed** (side-effects row 5; no V-number of its own, v032-reference's V26 row): run from root on every `LoadConfig` under sudo; any output reads as set; never under doas. **Not corrected yet:** nothing the mocks do runs `su`; unit (d) takes it out of `LoadConfig` (plan, slice row 9; decision 4, adopted, drops the notice from `next/p3-startup`'s row) | run: `notice_lost`, `notice_profile_prints`, `notice_doas`; run: `TestEnvironmentNoTmpNoProbe`, `TestEnvironmentPlanOnlyWritesNothing` |

Others found:

| # | Defect | Evidence |
|---|---|---|
| 4 | The status is a substring of one file: a commented line reads Enabled, and item 1 then succeeds writing nothing; the line in any other file, the trunk's `hardn-env-alice` too, reads Disabled | run: `environment_open_commented`, `keep_commented`, `environment_open_elsewhere` |
| 5 | `USER` stands in for `SUDO_USER`: at a root login (and under doas [I]) root's file is read and written | run: `environment_open_root`, `keep_root` |
| 6 | No user at all counts as success, nothing written | run: `keep_no_user`; `environment_service.go:45-47` |
| 7 | No doas path: without `/etc/sudoers.d` the call fails with a sudo message; the probe never runs under doas; nothing reads `DOAS_USER` | run: `keep_no_sudoers_d`, `notice_doas` |
| 8 | Item 1's branch for a user other than root is unreachable in hardn, and its words name `setup-sudo-env` | run: `environment_keep_not_root`; read: `cmd/hardn/main.go:148-152` |
| 9 | `setup-sudo-env` has no root check, though its help says it must be run with sudo | read: `cmd/hardn/main.go:447`, `:451-475` |
| 10 | `-e` and `setup-sudo-env` ignore `-u` and hardn.yml's `username`; `-r -e` never runs `-e` | read: `file_environment_repository.go:134-139`; `cmd/hardn/main.go:243-273` |
| 11 | The sudoers file is written in place, nothing backed up (V18) | run: `keep_new`; `file_environment_repository.go:95-97` |
| 12 | visudo's refusal reaches the user as "exit status 1", its output dropped | run: `keep_visudo_fails` |
| 13 | A `Stat` error other than not-exist replaces the user's file, grant included, with the env line alone | read: `file_environment_repository.go:50-66` (not run: a test user cannot make root's stat fail) |
| 14 | The notice prints under `-f` with the file loaded, prints mid-session from User Management, unsilenced, and at start the main menu's clear wipes it | run: `notice_lost`; read: `os_user_repository.go:20-35`; interaction.md:160 (W8) |
| 15 | The guide's headings print an empty bold span; its `~/.bashrc` may not reach a login shell [I] | run: `environment_guide`; `environment_settings_menu.go:124`, `:129`, `:134` |
| 16 | Items 1 and 2 are 83 and 85 columns wide, past an 80-column terminal | counted: `environment_open` |
| 17 | Each draw reads the file twice | run: every menu golden's calls; `file_environment_repository.go:141-147`, `environment_service.go:53-65` |
| 18 | A failure reading the status shows Disabled; the temp file's cleanup warnings print mid-screen | read: `environment_settings_menu.go:144-148`; `file_environment_repository.go:84`, `:92` |
| 19 | Each option redraws the screen by recursion | read: `environment_settings_menu.go:101`, `:106`, `:115` |
| 20 | An env-only Policy removes hardn's grant (§3.2). *The reader is built in `next/s9-control`:* `sudoers.PolicyOf`, the Policy planned from it carrying the grant (sudoers.md §2.13); the control that plans so is unit (b)'s second part. *Fixed there: the Environment control plans the user's whole Policy, its variables alone changed ([control-environment.md](control-environment.md) §8)* | run: `play/environment-grant-no-reader` (af5904a), `TestEnvironmentGrantCarried`; `TestPolicyOfCarries`; `TestGrantCarried` |
| 21 | `steps.Sudoers` drops the plan's findings, so neither the later-file nor the legacy-file finding reaches a plan view; the later-file detail names no file in the review ("a rule there that matches wins"), the file being in `Source` alone and the review showing the detail. The mocks carry them and word the later file's (`sudoersFindings`) | read: `pkg/engine/steps/plans.go:51-86`; `plan.go:365-369`, `pkg/output/flowengine.go:111-126`; run: `play/environment-wheel` (the mocks' words), `TestEnvironmentDoas`. *Fixed in `next/s9-control`:* `steps.Sudoers` carries them, each naming its file (`steps.SudoersFinding`; run: `TestSudoersFindings`); the mocks' own over them gone with `sudoersFindings` in its second part |
| 22 | The legacy-file finding's detail is written for developers ("origin/main's file for alice (V16) … hardn grants alice nothing, yet a grant in this file still applies"); here "grants alice nothing" only says hardn's files hold no grant. *Reworded in `next/s9-control`* (proposed, §6): `play/environment-legacy` now says "/etc/sudoers.d/alice may still let alice use sudo" | run: `play/environment-legacy`; read: `plan.go:377-390` (af5904a) |

4 to 19 go with the legacy code in unit (d) (*since `next/s9-control` 4, 5, 6, 7 and 11 are fixed on the control's path, [control-environment.md](control-environment.md) §8; origin/main's code keeps them until unit (d)*); 20 to 22 are unit (b)'s (the reader, `steps.Sudoers`, the finding words): its first part, `next/s9-control`, built the host side of all three (sudoers.md §2.13 to 2.16), and the reading of origin/main's file that replaces 4's substring check (`sudoers.ReadLegacy`: a commented line does not count). The existing `ops/setup_sudo_preservation/*.golden` cite `cmd/hardn/main.go:409-416` and `:426-461`, now `:422-428` and `:439-476`; goldens are additions only here, so they are left.

## 8. What the mocks take from real code, and what not

**Real:** the host's facts, `detect.Detect`; the tool and its flavour, `sudoers.Choose`; hardn's files and the drop-in entries, `sudoers.Read`; item 1's plan, its findings, its risk and its refusal on doas, `sudoers.PlanFor` (`ErrEnvNeedsGrant`); the change, `steps.Sudoers` (its diff, risk, backups and apply, `sudoers.Apply` with its checks on copies in the run directory); the engine, `engine.New` (hardn's boot unit and the revert timer for a lockout-risk change, the journal); the flow, `screenkit.Session.Change` on `output.FlowEngine` (review, plan, y/N or the typed hostname, apply, result, the kept backup, plan only); the refusals' layout, `screenkit.Warnings`; the screen, `screen.SubMenu`, and every component (`TestEnvHosts`, `TestEnvironmentGrantCarried`, `TestEnvironmentDoas`).

**Not, the gallery's own, each a stand-in for unit (b)'s work** (*since `next/s9-control`, 1 to 6 and 8 are gone: the mocks read with `environment.Read` and plan with `environment.Keep`, [control-environment.md](control-environment.md) §2.12; 9's state, change and refusal words are the control's, the screen's the gallery's; 7 stands*):

1. `invoker` (`environment.go:335-346`): who ran hardn, `SUDO_USER` or `DOAS_USER`, root or unset none. Unit (b): the lookup in the Environment control, replacing origin/main's `USER` fallback (decision 2).
2. `currentPolicy` (`:364-413`): the reader. It takes the variables of hardn's `env_keep` line or doas rule's `setenv`, whatever they are, tries them with each grant against the real `PlanFor`, and takes the Policy whose files hold exactly what it would write, `PlanFor` checking each name as it checks any Policy's; a hand-edited file is `errEdited`, named. With `envTreatment.target` (`:415-437`), item 1's Policy is the user's as read, `HARDN_CONFIG` added or taken out, the grant and the other variables carried. Unit (b): a reader in `pkg/host/sudoers`, where `Read` already reads the files, for any `Env` list, shared with slice 5 (§3.2), and the change built from the user's whole Policy so; on doas it reads hardn's files alone (decision 1 (a), settled by words).
3. `sudoersFindings` (`:663-678`): the plan's findings on the change, the later file's in the screen's words. Unit (b): `steps.Sudoers` sets `Change.Findings`, their details reworded (§7 items 21, 22).
4. `legacyKeeps` (`:506-513`): origin/main's own substring check of `/etc/sudoers.d/<user>`, for decision 3 (a). Unit (b), under decision 3 (a): the line itself, a comment not counting (§7 item 4).
5. `envControl` (`:680-726`): an `engine.Control` with `Plan` alone; the screen reads the host itself (`readEnv`). Unit (b): the Environment control, `Check` included (decision 6).
6. `doasGrant` (`:158-162`): `GrantPassword` for a new per-user doas rule, under decision 1 (b) only.
7. The fake host: `doas -C` passes every file (no rejection modelled); doas's files for Alpine (`doas.conf` 0640 per sudoers.md §3, its content the package's, on the 3.23 cloud image too [I]; the drop-ins' 0640 the model's [I]); and no rule is evaluated, so what wins and whether a variable is kept are not modelled: the hosts' notes say what sudo or doas did.
8. The treatments (`envTreatment`, `:131-156`): the switches for decisions 1, 3 and 5, each zero value the recommended option, and `noReader` for §3.2.
9. The words of §6.

**Live helpers the build would need** (not added, as this unit changes no live code; *since built in `next/s9-control`, unit (b)'s first part:* the reader, `sudoers.PolicyOf`; the user lookup, `sudoers.Invoker`; origin/main's line, `sudoers.ReadLegacy`; the findings on the change, `steps.SudoersFinding`; sudoers.md §2.13 to 2.16; *its second part:* the Environment control, `Check` included, and the refusal on doas as its typed refusal, [control-environment.md](control-environment.md)): the reader in `pkg/host/sudoers`, for any variable list; the user lookup; `steps.Sudoers` carrying findings, with their words; the Environment control and its `Check`; under decision 1 (a), the refusal before asking on doas as a typed refusal of the control (`ErrEnvNeedsGrant` with words); `cmd/hardn`'s `-e` and `setup-sudo-env` on the control, refusing a lockout-risk plan; `config.LoadConfig` without the probe (plan, slice row 9; decision 4).

## 9. Not in this unit

- **(b) The control**, on the engine, with its own goldens: `pkg/engine/controls/environment` (`Check`, `Plan` both ways, decision 5 (a)), the reader in `pkg/host/sudoers` (hardn's files alone, on doas too: decision 1 (a), settled by words), the user lookup, `steps.Sudoers`' findings and their words, and decisions 1 to 3's engine side. The screen kit (`pkg/screens/screenkit`) and the parity harness (`internal/parity`) are there already, from slice 3. *Built in `next/s9-control`* ([sudoers.md](../safety/sudoers.md) §2.13 to §2.16; [control-environment.md](control-environment.md)).
- **(b2) Parity:** origin/main's end state (`keep_*.golden`, `ops/setup_sudo_preservation/*.golden`) against the control's: `/etc/sudoers.d/<user>` against `hardn-env-<stem>`, each difference tied to V16, V18, V20 or a decision. `internal/parity` needs a reader for the `environment/keep_*` layout, as it gained one for Backup's.
- **(c) The screen** on the design system and prompt kit, `pkg/screens/environment`, retiring these mocks.
- **(d) The legacy path replaced:** main menu 8 to the new screen, its description "Keep HARDN_CONFIG" (decision 7 (a)); `-e` and `setup-sudo-env` to the control, refusing a lockout-risk plan (§5, made here, adopted); the probe and its notice removed (plan, slice row 9; decision 4, adopted); `environment_settings_menu.go`, `EnvironmentManager`, the environment service and its test, its port, `FileEnvironmentRepository`, `model.EnvironmentConfig`, `CreateEnvironmentManager` and `MenuManager`'s three methods deleted after (b2), the menu goldens frozen as the parity record. The plan's unit after slice 9 then removes what legacy code is left.

## 10. Size

**This unit.** Non-test Go: 1,128 lines added, 7 removed (`environment.go` 1,081, 253 of them whole-line comments; `main.go` 23, `play.go` 17, `fakehost.go` 4, `engine.go` 3). Tests: 1,518 lines (`environment_test.go` 504, `environment_calls_test.go` 501, `environment_notice_test.go` 270, `menu_environment_test.go` 243). Testdata: 67 new golden files (31 characterization, 20 listing, 16 play), 8,442 lines; none changed. This page. Over the guideline of about 500 changed lines, as slices 2, 2b and 3's units (a) were.

**Against Backup** (lines added, a proxy, not time or cost; Backup's from the merges on `next/v0.4`, 3a09801 to 1cd3984, the hangup fix left out). Backup's unit (a): 1,268 non-test, 1,186 test, 7,105 golden lines in 45 files. So this unit is about **0.89** of Backup's non-test lines, **1.28** of its tests, **1.19** of its golden lines. Backup's slice came to 4,253 non-test lines: (a) 1,268, the shared extraction 322, the control 776, parity 431, the screen 1,304, the replacement 152.

For units (b) to (d), measured only in what exists:

- No shared extraction: `screenkit` and `internal/parity` exist (Backup's 322 lines).
- The host side exists: `pkg/host/sudoers` (1,215 non-test lines) and `steps.Sudoers`; what is new is the reader, the user lookup and the findings on the change. Backup's control unit added to `pkg/host/backup`, the engine and `pkg/output`.
- The legacy to delete: 462 lines in six files, plus the probe in `config.go` and `cmd/hardn`'s two entry points, against Backup's 763 in six files at 3a09801.
- The screen has two items, Backup's four.

The earlier estimate of about 0.45 for the slice would leave units (b) to (d) about 790 non-test lines, 0.26 of Backup's 2,985: not measured, and dependent on decisions 1, 3, 4 and 6. Its "about 0.3 after slice 5" assumed slice 5 first. Slice 5 has not started, and it needs the same reader (§3.2), so here slice 9 builds it.
