# The Configuration screen (`pkg/screens/config`, `screen.Config`)

*Since `next/config-edit` (2026-11-15; [design-config-edit.md](design-config-edit.md), its §7 for what was built): the screen edits. Where a file was read it has one item, `1) Edit a setting`, under "Select an option"; with none, "File" ends with "Start hardn again to create one." and there is no item. The group's item 1 reads "View or edit the file hardn reads". `configscreen.Run` now needs a `System` and `NewEngine`, and takes `Facts`, `ApplyContext`, `DryRun`, `LogPath`, `Now` and `Ran`, as the other saving screens do: each save is `steps.PlanSave` of one key through `screenkit.Session.Change`. The host is read only for the logger, and only where `syslogServers` has a TCP or TLS server (`configscreen.LoggerNote`). Where this page says the screen only reads, changes nothing, or has Return alone, this note is current. `hardn config` is unchanged.*

*Since `next/menu-layout` (2026-11-14; [gallery-menu-layout.md](gallery-menu-layout.md)): the menu is `1) hardn.yml`, `2) DNS Settings` ("Configure nameservers"), `3) NTP Settings`, `4) Syslog Settings`; Kernel Settings is main menu 4.*

*Since the several-servers unit (2026-11-14): the Syslog group's key is `syslogServers`, its row the servers' addresses alone, under the key when they are over the row's width ([design-syslog-servers.md](design-syslog-servers.md) §2.1, §6).*

*Since `next/sysctl` (2026-11-14): the menu has a fourth item, `4) Kernel Settings`, "Harden network and kernel" ([gallery-sysctl.md](gallery-sysctl.md), decision 1), the prompt "[0-4 or q]", 10 lines; this screen has a Kernel Settings group (`kernelSettings`). §1.1's drawing is the three items before it.*

*Since `next/time-sync` (2026-11-14): main menu 8, Configuration, is a menu of three screens: `1) hardn.yml`, this screen, unchanged; `2) Log Forwarding` ([screen-logforward.md](screen-logforward.md)); `3) Time Sync` ([screen-timesync.md](screen-timesync.md)). Each returns to it (§1.1). The main menu's keys are unchanged from v0.9.0, and its item 8 reads "hardn.yml, log forwarding, time sync" (the maintainer, 2026-11-14, revising [gallery-logforward.md](gallery-logforward.md)'s decision 8 and [gallery-timesync.md](gallery-timesync.md)'s decision 9). This screen has a Log Forwarding group (`logForwarding`) and a Time Sync group (`ntpServers`).*

Unit (b) of the roadmap's Configuration item ("View and manage hardn.yml" until this unit), `next/config-screen`: the screen [gallery-config.md](gallery-config.md) mocked, as adopted there (2026-11-12, all (a)), now main menu 8. It was the read-only view alone; editing came with `next/config-edit` (the note above).
Status: built and tested in process (`make check`, all ten steps). *Run on `deb13-vm` (Debian 13) and `alpine323-vm` (Alpine 3.23.6), 2026-11-12, the linux-amd64 build of `bf7f111c` (`vm-runs/v090/`): `go test ./...` passed on both; `hardn config` as the login user and through sudo or doas, a named file missing by `-f` and by `HARDN_CONFIG`, `config -c` refused, and main menu 8 opened, left with 0 and quit on a terminal. Debian read `/etc/hardn/hardn.yml`; Alpine had no hardn.yml. Not run: the other distributions, arm64.*

## 1. What it does

Main menu `8) Configuration`, then `1) hardn.yml`, "View or edit the file hardn reads", shows which hardn.yml hardn read ("Path", "From": `-f`, `HARDN_CONFIG` or "Search path") and each key's value, grouped by the screen that uses it. A key the file does not set says "(default)". Keys the file sets that nothing reads are listed under "Not used", where set to other than their old default: a retired key at its old default, such as `sudoNoPassword: true`, is not listed. `1` edits a setting (since `next/config-edit`), `0` returns, `q` quits. Viewing changes nothing, and opens under plan only as live. System Details is 9 and Logs 10 again, the prompt "[0-10 or q]". `hardn config` prints it, as before.

### 1.1 The Configuration menu

Main menu 8 opens a menu, not this screen (the maintainer, 2026-11-14):

```
# Configuration ################################################

   1) hardn.yml            View the file hardn reads
   2) Log Forwarding       Send logs to a syslog server
   3) Time Sync            Keep the clock in sync

   0) Return

    > Option [0-3 or q]:
```

- Each item opens its screen, unchanged, under the menu; `0` there returns to the menu, which is drawn again, and `q` quits hardn.
- The menu reads nothing: no state is drawn beside an item.
- The view is `screen.ConfigGroup` (`pkg/ui/screen/configgroup.go`), the screen `configscreen.RunGroup` (`pkg/screens/config/group.go`), whose three screens `cmd/hardn` gives it (`runConfigGroup`, `cmd/hardn/readers.go`).
- Tests: `pkg/screens/config/group_test.go` (each key opens its screen and returns, 0, q, a screen's q and errors), `pkg/ui/screen/configgroup_test.go` (the drawing, 9 lines), `cmd/hardn`'s `TestConfigScreenHook` and `TestMenuOptions`.
- The gallery: the `config` listing's first mock, `menu`; `play config/menu`; the main menu's 8 in `play`.

## 2. The API

- `configscreen.Run(ctx, Options{Prompter, Config, Found})` (`pkg/screens/config`, package `configscreen`): draws the screen until `0` (nil) or `q` (`nav.ErrQuit`). No `host.System`: it reads no host.
- `configscreen.View(cfg, found) screen.Config`: the one mapping from a loaded `*config.Config` and the load's `config.Found` to the view. The gallery draws its mocks through it.
- `pkg/config`: `(*Config).FileSets(key)` (the file gives the key a value, through a top-level `<<` merge too), `FileWord(key)` (that value: a list or a mapping on one line, in YAML's flow form; otherwise where it is one word or number), `FileUsername()` and `FileDryRun()`, recorded as the file is decoded; `FileSets` and `FileWord` again after `Save`. `OnSearchPath(path)`.
- `mainmenu.Options.Configuration`, set by `cmd/hardn`'s `withReaderScreens` (`runConfigScreen`) with the `config.Found` of the load (`loadedFound`); hardn.yml is not looked for again.
- `component.KeyValue.KeepNote`: opt-in, see §3.

## 3. Engineering calls

- **A long value keeps its note.** A row with `KeepNote` whose value and note have no room beside the key puts them under the key, that row alone; a value too wide even there is cut ahead of the note. Only this screen's rows set it, so no other screen's output changed: the goldens that changed are the menu's renumbering and the seven `config/` mocks' `sshConfigFile` row.
- **"Path" and "From" are the file hardn read**, not what is there now: `Config.Path()` and how the file was found at load. A file the first run's offer created says "Search path" where it is on it, and has no "From" otherwise.
- **`dryRun` and `username` are the file's, not the session's.** `-n` sets `Config.DryRun` and `-u` sets `Config.Username` for the session; the screen shows `FileDryRun()` and `FileUsername()`, the string the loader read (`0755`, not 493).
- **A configuration loaded from no file reads as none read**: "None found", or "not found" for a path `-f` or `HARDN_CONFIG` named at load.
- **As the loader reads it:** a null keeps the default, an empty `confirmTimeout` is the default, `disableRoot` sets `disableRootSSH` unless the file has `disableRootSSH` too. "Not used" is in the start-up warning's order; a mistyped key is not listed.
- **Every string from the file, and the path, goes through `logview.Safe`.** `sshKeys` is a count, never a key.
- `Options.Found` is `config.Found` itself, not a second struct. The main menu's field is `Configuration`: `Options.Config` is the configuration.

## 4. Tested

- `pkg/screens/config`: each "From" value, no file, a named file missing, the file read whatever the load's `Found` says, a `username` that reads as a number, merged keys, every yaml key of `config.Config` a row or "Not used", defaults marked, the loader's rules, the long value's note, "Not used" and its order, control characters, `-n` and `-u`, `0`, `q`, a refused key, the end of the input, invalid options.
- `pkg/config`: `FileSets`, `FileWord`, `FileUsername`, `FileDryRun`, merge keys, `OnSearchPath`, after `Save`, the first run's file.
- `pkg/ui/component`: `KeepNote`, and that a row without it is cut as before.
- `cmd/hardn`: the hook shows the held configuration and the load's `Found`, never asks `findConfig`, and reaches no host; `loadedFound` for a created file. `pkg/screens/mainmenu`: item 8, every key, plan only.
- The gallery: `play config`, `play config/<mock>`, the main menu's 8, and the `config` listing through the same mapping.

### 4.1 Editing (`next/config-edit`)

- `pkg/screens/config/edit_test.go`: each kind of key saved, only its lines changed and comments kept; a key the file does not hold added, one set back to its default; lists, one entry a save, the last removed; each refused answer asked again with nothing planned; `-u` and plan only; the save's refusals (a link, changed since read, a `<<` merge); no file, no item; the logger's note on busybox syslogd, and nothing said on another logger or one not read.
- `pkg/config/editkeys_test.go`: `config.Edit` on `syslogServers` (added, removed, the last removed) and the other cases above. `Save` naming `username` makes it `FileUsername()`.
- `pkg/ui/screen/configedit_test.go`: "Edit a setting", a list's screen, a key's note.
- `cmd/hardn`: `TestMenuOptions` holds the screen's options.
- The gallery: the `config` listing's `alpine-tcp` and `edit*` mocks; `play config-edit`, `config-edit-plan-only`, `config-edit-alpine`.

## 5. Not run

Since the run of 2026-11-12 (above): the other distributions and arm64. The first run's file is tested through the bytes it writes, not through the offer at a terminal. Ctrl-C at the prompt is `nav`'s and `prompt`'s own tests'.

## 6. `hardn config`

`hardn config` prints what the screen shows, without the menu or the prompt, on stdout: `screen.Config.Report` of the same `configscreen.View`, through `configscreen.Print`. On a terminal it is drawn as the screen is; off one it is plain, each value whole. `--color`, `--plain`, `--ascii` and `--theme` apply.

- It doesn't need root, and writes nothing: no log, no example hardn.yml, no offer to create hardn.yml (`loadExisting`).
- It reads the file every command reads: `-f`, then `HARDN_CONFIG`, then the search path.
- It reads as the user who runs it: `HARDN_CONFIG`, the home directory and `./hardn.yml` are that user's, so `sudo hardn config` is what shows the file `sudo hardn` reads.
- Like `system-details`, it doesn't say on stderr which file it read. The load's warnings are not printed: "Not used" lists the keys. `-n` and `-u` change nothing in it.

| Exit status | When |
|---|---|
| `0` | The report printed. That includes no file found, and a file `-f` or `HARDN_CONFIG` names that isn't there ("not found", "hardn is using its defaults.") |
| `1` | hardn.yml can't be read or doesn't parse, or a named file is in a directory the user can't enter ("Failed to load configuration: ..." on stderr), or the write failed |
| `2` | `-c`, `-d`, `-g`, `-w`, `-r` or `-p` given ("hardn: -p is refused: run it without config"), or an argument |

Tested in process (`cmd/hardn/config_test.go`): a file on the search path, by `-f`, by `HARDN_CONFIG`; no file; a named file missing; a file that doesn't parse; a file the user can't read; a failed write; as a user other than root; nothing written and no log opened; each operation flag refused; an argument; the whole plain report, equal to the screen's view; `--help` lists it. `pkg/ui/screen`: the screen opens with the report. `pkg/screens/config`: `Print`. Not run: the hardn binary, anything on Linux.
