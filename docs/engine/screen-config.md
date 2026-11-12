# The Configuration screen (`pkg/screens/config`, `screen.Config`)

Unit (b) of the roadmap's Configuration item ("View and manage hardn.yml" until this unit), `next/config-screen`: the screen [gallery-config.md](gallery-config.md) mocked, as adopted there (2026-11-12, all (a)), now main menu 8. It is the read-only view alone; editing is a later unit.
Status: built and tested in process (`make check`, all ten steps). No hardn binary ran. Nothing ran on Linux, and nothing on a terminal.

## 1. What it does

Main menu `8) Configuration`, "View hardn.yml", shows which hardn.yml hardn read ("Path", "From": `-f`, `HARDN_CONFIG` or "Search path") and each key's value, grouped by the screen that uses it. A key the file does not set says "(default)". Keys the file sets that nothing reads are listed under "Not used", where set to other than their old default: a retired key at its old default, such as `sudoNoPassword: true`, is not listed. `0` returns, `q` quits. It changes nothing, and opens under plan only as live. System Details is 9 and Logs 10 again, the prompt "[0-10 or q]".

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

## 5. Not run

Nothing on Linux, nothing on a terminal, no hardn binary. The first run's file is tested through the bytes it writes, not through the offer at a terminal. Ctrl-C at the prompt is `nav`'s and `prompt`'s own tests'.

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
