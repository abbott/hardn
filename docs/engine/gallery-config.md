# Configuration mock (unit (a))

*Since `next/config-screen` (2026-11-12): the screen is built and is main menu 8 ([screen-config.md](screen-config.md)). The mocks draw through its mapping, `play config` runs it, and a long value keeps its "(default)" (decision 2's second cost is fixed). Where this page says nothing is wired, or names `configView`, this note is current.*

Unit (a) of the roadmap's "View and manage hardn.yml", `next/config-gallery`. **Adopted (the maintainer, 2026-11-12): "Adopt all".** Every recommended answer of §3 is adopted, with the Made here calls as stated and §4's words. It is the read-only view alone: no editing, no engine.
Status: built and tested on macOS in process (`go test ./pkg/ui/screen/ ./internal/tools/uigallery/`). No hardn binary ran. Nothing ran on Linux. Nothing is wired into hardn: the main menu, `cmd/hardn` and `pkg/screens` are untouched.

## 1. What to run

```
go run ./internal/tools/uigallery config [-ascii] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery config -list                    # the mocks' names
```

The view is `pkg/ui/screen/config.go` (`screen.Config`), the mocks `internal/tools/uigallery/config.go`, the goldens `internal/tools/uigallery/testdata/golden/config/`.

## 2. The mocks

Each mock is a hardn.yml loaded by the real loader (`config.Read`) from a fake host. How it was found is the mock's own word.

| Mock | What it shows |
| --- | --- |
| `etc` | `/etc/hardn/hardn.yml`, found on the search path; a typical file, the rest default |
| `flag` | The same file, found by `-f /root/site.yml` |
| `env` | The same file, found by `HARDN_CONFIG` |
| `none` | No file anywhere: every row default, "hardn is using its defaults." |
| `minimal` | A two-line file (`username`, `nameservers`) |
| `unused` | The typical file plus five keys nothing reads: the "Not used" section |
| `missing` | `HARDN_CONFIG` names a file that is not there |

The screen: the title bar; "File" (Path, From); one section per group of keys, each row the key and its value, "(default)" after a value the file does not set; "Not used"; `0) Return`.

## 3. Decisions for the maintainer

**Adopted, all seven (a).** What follows: the item is "Configuration", "View hardn.yml", at main menu 8, System Details 9 and Logs 10; rows are the file's keys, grouped by screen; "Not used" is drawn; the editing unit covers the keys no screen saves, `adminNoPassword` aside; and `next/remove-environment` waits for this screen.

Drawn: every recommendation below, but 1, 6 and 7, which are about the main menu.

1. **The item's label and description.** (a) *Recommended:* "Configuration" / "View hardn.yml". The title bar draws "Configuration". (b) "hardn.yml".
2. **What a row is called.** (a) *Recommended, drawn:* the file's key as written (`sshPort`), so what you read is what you edit. Costs, both in the renders: `enableUnattendedUpgrades` is wider than the label column, so its value goes under it; and `sshConfigFile`'s default path is cut at 80 columns and loses its "(default)", so it reads as set. Fixing the second needs a row that stacks a long value, a component change for the screen unit. (b) Plain labels ("SSH port"): nothing stacks, but the key has to be looked up.
3. **Grouping.** (a) *Recommended, drawn:* by the screen that uses the key (Users, SSH, Firewall, DNS, Auto Updates, Backup, General). (b) The file's own order.
4. **Keys nothing reads.** (a) *Recommended, drawn:* listed under "Not used", each a warning row. (b) Left to the start-up warning alone.
5. **What the editing unit covers.** *Gates that unit's scope.* (a) *Recommended:* only keys no other screen saves: `username`, `sshKeys`, `sshPort`, `disableRootSSH`, `sshConfigFile`, `enableUfwSshPolicy`, `configureDns`, `confirmTimeout`, `dryRun`. Keys another screen saves already (`nameservers` by DNS, `ufwAppProfiles` and `ufwTrustedInterfaces` by Firewall, `enableUnattendedUpgrades` by Auto Updates, `enableBackups` and `backupPath` by Backup; each a `steps.PlanSave` in its control) are shown with that screen named. `adminNoPassword` stays a hand edit, a deliberate opt-in. (b) Edit every key here.
6. **Position.** (a) *Recommended:* main menu 8, so System Details is 9 and Logs 10 again. (b) Next to Run All.
7. **Whether `next/remove-environment` waits for this screen.** That branch renumbers the menu to nine items. (a) *Recommended:* it waits, so users never see the menu renumber twice. (b) It ships first.

## 4. Words

Decided with §3: "Configuration"; "File"; "Path"; "From": "-f", "HARDN_CONFIG", "Search path"; "None found"; "not found"; "hardn is using its defaults."; "(default)"; "Not set"; "None"; "1 key", "2 keys"; "Not used". The group names: "Users", "SSH", "Firewall", "DNS", "Auto Updates", "Backup", "General" (the main menu says "User Management" and "SSH Login"; shortened here).

## 5. Made here

- "hardn is using its defaults." is a muted line in "File", as the Logs screen's "No log yet", not a callout: the sub-menu layout has no notice block.
- A default is told by "(default)" alone. The theme draws every plain value muted already, so there is no dimmer style without a new value state.
- `permitRootLogin` is listed under "Not used" only where it is not the default, as hardn's start-up warning has it (`cmd/hardn/output.go`, `notUsed`). There is no "deprecated form" row: `Config.Deprecations()` does not report the bool (`pkg/config/rootlogin.go`). So the `unused` mock writes `permitRootLogin: "yes"`.
- A "Not used" row shows the key's value where it is one word or number.
- An empty list says "None"; `ufwAppProfiles` shows the names alone; `sshKeys` a count, never a key.
- The screen is full at every height: 45 lines for `etc`, taller than 24 rows.
- The values are worded in the mapping, the sections and row order in `pkg/ui/screen`.

## 6. For unit (b)

- `configView` in `internal/tools/uigallery/config.go` is the stand-in: the one mapping from a loaded `*config.Config`, the file's top-level keys and how it was found to `screen.Config`. It moves into `pkg/screens`.
- `config.FindConfigFile` reads the real OS (`os.Stat`, `os.Getenv`) and cannot be faked. The mocks never call it. The screen needs what `cmd/hardn` found passed in.
- `Config` does not say which keys the file set. The mock decodes the bytes a second time into a map; `pkg/config` should say it.
- `disableRoot`, the one `Deprecation` today, is read as `disableRootSSH`; the mock does not draw it.
- With no hardn.yml, hardn on a terminal first offers to create one (`config.LoadConfig`): the `none` and `missing` states show only after answering no, and a file created then has no "From" word yet.
- A null value (`sshPort:`), an empty `confirmTimeout` and the old key `disableRoot` are read as the loader reads them: default, default, and `disableRootSSH` set. "Not used" lists the keys in the start-up warning's order, and only the keys hardn knows it doesn't read: a mistyped key is not shown.
- Every string from the file is made safe to show (`logview.Safe`).
- Nothing ran on Linux.
