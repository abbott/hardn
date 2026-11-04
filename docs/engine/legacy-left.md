# The legacy code a live path still runs

> **Status:** a reading of `5079a305` (`next/v0.4`, slices 1 to 9 merged), in a scratch worktree; nothing re-homed. Start-up was shown by a scratch test through `start()` with stdout and stderr pipes; no hardn binary ran, nothing on a VM.
> **Scope:** refactor plan, "One unit after slice 9", part 1. Code no live path can run (`main.go`'s install and sources branches, the menus nothing opens, origin/main's status block behind `legacySecurityStatus = false`, the update alert behind `version.updateCheckEnabled = false`, `pkg/testing`, `pkg/characterization`) is the dead-code unit's, beside this one on `next/legacy-dead`.

## 1. What's live

`pkg/screens`, `pkg/engine`, `pkg/host`, `pkg/ui`, `pkg/output` and `internal/` (the gallery included) import no legacy package. Every live call comes from `cmd/hardn`, `pkg/menu` and `pkg/config`.

| Legacy code | What it does for the user | Live callers | Replacement | What a user would see change |
|---|---|---|---|---|
| `pkg/menu`: `MainMenu` (`main_menu.go`: `ShowMainMenu` 741, `pass` 789, `mainMenu` 852, `mode` 817, `showScreen` 307) and `input.go` (`endHardn` 70) | The main menu: draws the status, runs each item's screen, Exit or q quits; the end of input exits 130 | `main.go:210-236`; the `Set*` hooks of `hooks.go:43` (`users.go`, `ssh.go`, `dns.go`, `firewall.go`, `backup.go`, `updates.go`, `runall.go`, `environment.go`, `readers.go`, `status.go`) | `pkg/screens/mainmenu`: a `Run(ctx, Options)` beside `Status`, the hooks as `Options` fields; the items one list with `pkg/ui/screen/fixture.go:65`; `prompt.ExitCode`/`prompt.Reraise` as now | Nothing, apart from item 2's words (§3, decision 5) |
| `pkg/utils.ClearScreen` (`utils.go:24`) | Clears the screen as the main menu starts (`main_menu.go:751`), as each item's screen opens (311), at Exit or q (771), and at start-up (`PrintHeader`, `utils.go:31`) | as listed | None yet: a clear in `pkg/ui/term`, written only when the `Env` is not `Plain` | Off a terminal no `ESC[H ESC[2J` (decision 1); the clear counts of `pkg/menu`'s `TestMainMenuKeys` (`main_menu_layout_test.go:143`) and `main_menu_dns_test.go:233-244` |
| `pkg/utils.PrintHeader`, `PrintTilda`; `pkg/style` (`Colored`, `Green`, `UseColors`) | A green line of `~` at start-up; `--no-color` sets `style.UseColors` | `osdetect.go` (`DetectOS`); `main.go:111-119` (`initializeColor`) | None: the line goes (decision 1); `--no-color` already reaches `term` (`output.go`, `streamEnv`) | The line goes |
| `pkg/osdetect.DetectOS`, `OSInfo` | Reads `/etc/os-release`; says "debian trixie detected"; refuses no os-release or an OS other than Debian, Ubuntu, Alpine (exit 1); `OSInfo` goes to the factories | `main.go:190-194` (`detectOS`, `main.go:466`): every root-command run past the root check | `pkg/host/detect.Detect` and `Classify` (`DistroOther`, `DistroUnknown`) for the refusal; the OS is already on the main menu's context bar | The "detected" line goes (decision 1); the refusal's words |
| `pkg/logging` (`logging.go`) | Writes `/var/log/hardn.log`, which the Logs screen and `-p` read (`pkg/engine/logview`, its line format at `logview.go:205`); prints "[ERROR] …", "[WARNING] …", "[INFO] …", "[SUCCESS] …" on stdout, in colour | `InitLogging`, `main.go:61`; `Log*` in `main.go:154-192`, `environment.go:124-129`, `config.go:231-284`, `osdetect.go`, `confirm.go:64-95`, `revert.go` (confirm, rollback, `rollback --boot`), `dns.go:114`, `runall.go:115-122`, `typed.go:156`; `LogFileOnly` in `engine.go:146,207` and `confirm.go:160` (`revertFileLog`, the host's run log: every command the engine runs) | The file: a writer in `pkg/output`, beside `audit.go`, which already names `LegacyLogPath`, in the same line format. The console: `component.Messages` through `stderrEnv`, as `warnDeprecations` (`output.go:96`) | The console lines' form (decision 2); the log file unchanged |
| `pkg/cmd` (`system_details_cmd.go`, `dry_run.go`) | `hardn system-details`' flags and refusals; exit status 2 for a usage error | `main.go:72,88,482`; `readers.go:89`; `pkg/output/codes_test.go:20` | New-design code already (slice 1): to `cmd/hardn`; `ExitUsage` to `pkg/output` | Nothing |
| `pkg/infrastructure`, `pkg/application` (`MenuManager`), `pkg/domain`, `pkg/adapter`, `pkg/port`, `pkg/interfaces` (`Provider`) | Nothing: built at start-up, no I/O, and given to the main menu, whose one reader of `MenuManager` is origin/main's status block, off (`main_menu.go:119`). The status block comes from `withStatus` (`status.go`) and `pkg/screens/mainmenu.Status` | `main.go:54, 201-202, 210-211, 262`; `menu_factory.go:39-45` | None needed | Nothing |
| The update flags: `--test-update`, `--test-security-update`, `--debug-updates`, `HARDN_FORCE_UPDATE`, `HARDN_FORCE_SECURITY` | Nothing: accepted, "(no effect: update checks are disabled)" in their help; `version.Service.CheckForUpdates` returns at once (`service.go:51`) | `main.go:106-108, 213-233`; `MainMenu.SetTestUpdateAvailable`, `SetTestSecurityUpdate`, `CheckForUpdates` | None: the one renderer of an update is pkg/menu's alert, in `pkg/style` | Decision 3 |
| `pkg/config`'s legacy keys: `linuxCorePackages` and the other package lists, `debianRepos` and the other repositories, `useUvPackageManager`, `dmzSubnet`, `lang`, `language`, `lcAll`, `tz`, `pythonUnbuffered` | Nothing: read only by the dead install branches and `CreatePackageManager`, or by nothing. `/etc/hardn/hardn.yml.example`, written at every start, lists `dmzSubnet`, `lang`, `language`, `lcAll`, `tz` | none live | None | Decision 4 |

**Start-up off a terminal** (item 1 of the brief, shown by the scratch test). Before any flag acts, a root-command run writes, in order: (1) on stderr, as a user other than root, `InitLogging`'s two lines and "Warning: Unable to create example configuration file" (defect 28's); (2) on stdout, `FindConfigFile`'s "[INFO] Using configuration from: …" (or "from command-line flag", "from HARDN_CONFIG environment variable", "No configuration file found", then the first-run offer to create one), `ESC[34m` … `ESC[0m`; (3) `DetectOS`'s `ESC[H ESC[2J`; (4) the `~` line, `ESC[32m`; (5) "[SUCCESS] debian trixie detected", `ESC[32m`. The flags that get (2) to (5): `-c`, `-d`, `-w`, `-g`, `-r` and the main menu's start, which then clears again (`main_menu.go:751`) unless a warning was printed; `setup-sudo-env` gets (2) alone; `-p` alone, `-v`, `system-details`, `confirm` and `rollback` none. The colour is forced: `initializeColor` sets `color.NoColor = false` (`main.go:116`) and `style.UseColors = true`. `--no-color` drops the colour but not the clear; `NO_COLOR=1` drops fatih/color's but not the `~` line's. The design system draws nothing in colour when stdout is not a terminal (`docs/design-system/README.md`, the `term.Resolve` table: `Plain` when stdout is not a terminal, profile NoTTY when `Plain`) and has no clear.

## 2. Re-home order

Each step builds and passes `make check` alone.

1. **`pkg/cmd`.** `SystemDetailsCmd` and its form to `cmd/hardn`; `ExitUsage` to `pkg/output`, beside the codes its test checks. Delete `pkg/cmd`.
2. **The factories.** After the dead-code unit removes origin/main's status block: `menu.NewMainMenu(cfg, versionService)`, with no `MenuManager` or `OSInfo`; `main.go` drops `provider`, `serviceFactory`, `menuFactory` and `CreatePackageManager`. Delete `pkg/infrastructure`, `pkg/application`, `pkg/domain`, `pkg/adapter`, `pkg/port`, `pkg/interfaces`.
3. **The update flags** (decision 3). `MainMenu`'s update fields go with them.
4. **The clear.** A clear in `pkg/ui/term`, a no-op when `Plain`; `showScreen` and `ShowMainMenu` call it.
5. **Start-up** (decision 1). The refusal of an unsupported OS from `pkg/host/detect`, exit 1 as now; `FindConfigFile` returns what it found and `cmd/hardn` says it; `initializeColor` keeps `color.NoColor` alone. Delete `pkg/osdetect`, `pkg/utils`, `pkg/style` (`pkg/menu`'s tests take `ansi.Strip` from `charmbracelet/x/ansi`, a direct dependency, for `style.StripAnsi`).
6. **`pkg/logging`** (decision 2). The file writer in `pkg/output`, the line format byte for byte, so `logview` and its tests hold; `revertFileLog` and `engine.go`'s file-only lines onto it; the console lines onto `component.Messages`; `setRevertColour` and `FallbackOutput` go. Delete `pkg/logging`; `fatih/color` leaves `go.mod`.
7. **`pkg/menu`** into `pkg/screens/mainmenu` (`Run`, the items as one list with the gallery's fixture, item 2's words per decision 5). `hooks.go` builds `Options`. Delete `pkg/menu`.
8. **`pkg/config`'s legacy keys** (decision 4), the embedded example with them.

## 3. Decisions for the maintainer

**1. Start-up's lines and clears. GATES step 5.** Every `hardn -c`, `-d`, `-w`, `-g`, `-r` and the main menu's start begin with origin/main's start-up: which `hardn.yml` was read, a screen clear, a green `~` line and "debian trixie detected", in colour codes and with the clear even when stdout is a pipe or a file (§1, written by `pkg/config/config.go:231-284`, `pkg/osdetect/osdetect.go`, `pkg/utils/utils.go:20-37`). On a terminal the clear erases the config line and the scrollback above the command; off one the codes are noise in a log, against the design system's plain output off a terminal.
- **(a), recommended:** drop the clear, the `~` line and the "detected" line; say which `hardn.yml` was read in one plain line on stderr for the flags and `setup-sudo-env`, and nothing before the main menu, which clears over it; the main menu clears only on a terminal; an unsupported OS still refused, exit 1. The config path is the one fact here shown nowhere else (`-f`, `HARDN_CONFIG` and the search path pick it); the OS is on the main menu's context bar.
- **(b):** drop all of it, the config line too. Quietest; which file was read is then unsaid.
- **(c):** keep every line on a terminal; off one write them without colour and without the clear.
- **Changes:** a flag's output starts with its plan; slice 8's VM transcript of `hardn -r`; the clear counts in `pkg/menu`'s tests. The first-run offer to create `hardn.yml` is unchanged.

**2. The form of hardn's console lines. GATES step 6.** Every console line hardn prints outside the screens (the root check's "[ERROR] This script needs to be run as root.", `hardn confirm`'s and `hardn rollback`'s reports, `-r`'s errors, the config line) print as "[ERROR] …", "[WARNING] …", "[INFO] …", "[SUCCESS] …" on stdout through `pkg/logging`, coloured unless `--no-color` (confirm and rollback only on a colour terminal, `output.go:219`). The new design's failures already render otherwise: off a terminal `-g`'s reads "# Operation Failed", the message, the code.
- **(a), recommended:** every console line a `component.Message` on stderr, its kind's look, plain off a terminal. One look across hardn, and stdout holds a command's output alone, defect 28's rule.
- **(b):** keep the "[ERROR]" words; move them to stderr; colour only on a colour terminal.
- **(c):** keep them as they are, `pkg/logging`'s console half kept in the new design.
- **Changes:** the strings asserted in `cmd/hardn/confirm_test.go`, `revert_test.go`, `revert_boot_test.go`; the journal lines of confirm and rollback under the systemd timer. `/var/log/hardn.log` is unchanged either way.

**3. The update-check flags. GATES step 3.** `--test-update`, `--test-security-update`, `--debug-updates` and `HARDN_FORCE_UPDATE`, `HARDN_FORCE_SECURITY` are accepted and do nothing since update checks were disabled (2026-10-17, `pkg/version/checker.go:44`); their help says so. The only code that would draw an update, the version line's "available" and the security alert that replaced the menu, is pkg/menu's, in `pkg/style`, off.
- **(a), recommended:** remove the three flags and the two variables; P-15, the update checker made opt-in, draws a notice on the new header when checks return. Nothing reachable is lost.
- **(b):** keep them, inert, until P-15; their fields move to `pkg/screens/mainmenu` with no renderer.
- **Changes:** (a) takes three lines out of `hardn --help`, and `hardn --test-update` becomes "unknown flag", exit 2.

**4. `hardn.yml`'s package, repository and locale keys. GATES step 8.** Since the install and sources items and flags went, nothing reads them (§1, last row); the example file hardn writes lists five.
- **(a), recommended:** remove them; a `hardn.yml` that sets one gets "not used" on load, as `permitRootLogin` and the others do (`output.go:169`); the example drops them. A user who set package lists learns hardn ignores them.
- **(b):** remove them without a warning.
- **(c):** keep them as fields nothing reads.
- **Changes:** (a) new warnings above the main menu and on stderr for those hosts; `hardn.yml.example`'s lines. Screens edit only the keys they change (`pkg/config/edit.go`), so a user's keys stay in the file.

**5. Main menu item 2's description** (P-07 (a); `pkg/menu/main_menu.go:859`, `pkg/ui/screen/fixture.go:65`). It says "Toggle SSH root access"; since slice 6 the SSH Login screen disallows or allows root login, turns password login on or off and changes the port, each item named by its result (P-12).
- **(a), recommended:** "Root login, passwords, port": the three things the screen changes, as item 1's "Create, Configure" names its two.
- **(b):** "Configure SSH login", as items 3, 4 and 6 say "Configure"; it says less.
- **(c):** keep it; it names one of three and a toggle the screen no longer has.
- **Changes:** `pkg/menu/testdata/golden/main_menu_live`, `pkg/ui/screen/testdata/golden/main-menu`, `internal/tools/uigallery/testdata/golden/status` and `play`. Gates nothing; lands with step 7 at the latest.

## 4. Not run

- The main menu's start through `start()`: `pkg/menu`'s `exit` is `os.Exit`, so its clears are from `pkg/menu`'s tests, not the scratch test.
- `DetectOS` on a real `os-release`: this Mac has none. The scratch test ran it to its refusal, then its own calls (`PrintHeader`, `LogSuccess`) for "debian trixie detected".
- Start-up on a terminal; any step of §2; `make check` (this unit commits this page alone).
