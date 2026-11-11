# The Auto Updates screen on the Auto Updates control (`pkg/screens/updates`, `screen.Updates`)

> **Unit:** refactor plan slice 7, unit (c), `next/s7-screen`, from `9a4bb455`. **Status:** built and tested on macOS, in process: the screen through `Run` on `fake_test.go`'s model (the control's `model_test.go` and the gallery's hosts, copied), the control's plans on the real engine. No real terminal, no hardn binary, no VM. Not yet on the main menu: unit (d) wires it.

> *Since `next/s7-replace` (unit (d)): main menu 6 opens it ([cmd-engine.md](cmd-engine.md) §2.18).*

## What it does

Main menu 6 as the maintainer adopted its mock on 2026-11-03 ([gallery-updates.md](gallery-updates.md) §4, "Adopt all, start (b)", §5's words), drawn from `updates.Auto`'s `Read` alone ([control-updates.md](control-updates.md) §2), writing nothing, under plan only too. Never from hardn.yml.

- **The row** is `Reading.Status` through `output.StatusViewOf` and `risk.Assess`, over the header the main menu reads (`header.Read`), so its lines are the main menu's (an Unknown row's "Counted as not configured", the uptime's). Under it, decision 9 (a)'s "changed since hardn", from `CodeChangedSince` in `Reading.Status.Findings`.
- **The section**, Update Configuration: on apt the package, Periodic Jobs where `APT::Periodic::Enable` is 0, Upgrades and Package Lists (the value and the file that sets it), the two timers, Proxmox VE; on Alpine apk-cron, Daily Script, crond, Old Script, Other Jobs; on Rocky Linux, AlmaLinux and RHEL dnf-automatic's rows ([el-updates.md](el-updates.md) §1.4); then Turned on by.
- **The item**, key 1, named by its result (P-12): Enable automatic updates while off or partial, Disable automatic updates while on by hardn (`ByHardn`). It is Unavailable (P-22) on Proxmox VE ("Not on Proxmox VE") and while on but not by hardn ("Turned on outside hardn"); choosing it then says it is unavailable, and no engine is made. Where it is offered, its description is `Reading.Blocked` when something is in the way (R1).
- **Refusals** come from the control or the engine, said before asking: the item is planned by the engine first (`screenkit.Session.Preplan`, this screen's `preplan` until 2026-11-10, when the Firewall screen took it up), and its first refusal said in `screenkit.RefusalWords` as a warning (a change waiting for hardn confirm in SSH's words, `screenkit.WordsPending`, since 2026-11-11). *Since 2026-11-11,* "Checking the host…", muted, is said before that plan (the maintainer: "A line before the check"). The Firewall screen's Install firewalld says `PACKAGE_NO_REPOS` alike (`TestRefusalGolden`, `refusal/no-repos.golden` in each).
- **The change** is `updates.Auto{Action: AutoOn | AutoOff, Version, Records}` through `screenkit.Session.Change`, plain y/N. The review is "Auto Updates On/Off", then the control's `Notes` as rows. The engine plans again from a fresh reading, and after the apply the screen is drawn again from one. A change that fails is rolled back by the engine, said, and the screen goes on. Plan only stops at the plan.
- **A reading cut short** (the context) draws Unknown and Can't read, never an empty screen. Every other failed read is the control's, kept in `Reading` and drawn as a row.
- **0** returns (nil), **q** quits (`nav.ErrQuit`), the end of the input and Ctrl-C abort (`prompt.ErrAborted`), from the screen and the question.

## API

```go
updates.Run(ctx, updates.Options{
	Prompter, System, Facts /* detect's */, Config /* hardn.yml; nil for none */,
	NewEngine, ApplyContext, DryRun, LogPath, Now, Ran,
	Version /* hardn's, in the record */, Records /* revert.Options; zero in hardn */,
})
```

Required: the Prompter's In and Out, System, NewEngine. Without them `Run` returns an error wrapping `fs.ErrInvalid` and writes nothing. `NewEngine` is called once when the item is chosen (the plan before asking) and once for the change. Drawing never calls it, and the screen never calls `engine.New`. `pkg/ui/screen/updates.go` only draws: `Updates`, `UpdatesItem`, `UpdatesChange`, the section's keys and the words. `rows.go` maps the control's reading to them.

## For unit (d)

- **Main menu 6** runs `Run`, labelled "Auto Updates", "Configure automatic updates" (decision 1). It passes the loaded hardn.yml, hardn's version and zero `Records`, under plan only too.
- **Off** saves `enableUnattendedUpgrades: false` through the control's plan (`steps.PlanSave`) when the loaded hardn.yml says true. The screen sets it false in the copy the change plans with (`Controls(edited)`), and the control saves that copy as given, so `screenkit.Adopt` finds the bytes written and a later save in the same session is not refused as `CONFIG_CHANGED`. With a nil `Config` there is nothing to save.
- **Delete or keep, and say which:** the dead `pkg/updates` (nothing imports it) and the interactive "Enable automatic security updates?" prompt (`autoUpdates`, `pkg/config/config.go:432-436`). *Done in `next/s7-replace`: `pkg/updates` deleted; the prompt kept, since it sets `enableUnattendedUpgrades`, Run All's key, which slice 8 owns.*
- **Run All** belongs to slice 8. It plans `AutoOn` only, never `AutoOff` (decision 2).
- **The gallery's second stage** moves `internal/tools/uigallery/updates.go` onto `Run`. It retires `updReading`'s `rows`, `item`, `screen`, the runner's `preplan`, `refusalOf`, `notes` and `change`, and `TestUpdatesHosts`, `TestUpdatesRefusalWords`, `TestUpdatesHeights` and `TestUpdatesPlanOnlyWritesNothing`, which are held here now.

## Decisions

- The row is assessed over `header.Read`, not the zero header the SSH Login screen uses. With a zero header, a host that does not upgrade itself gains "Uptime unknown", which the main menu would not show.
- The package's version stays dropped: the mock's "2.12" and "1.0-r3" were the gallery fake's constants, not read; §5's words are "Installed", "Not installed".
- Each round still reads `header.Read` in full: `header` exports `ReadUptime`, but no reader of the reboot alone, which `risk.Assess` also uses.
- The direction is the draw's (`Updates.Off`). The plan is the engine's, from a fresh reading, so a host that changed since the draw is refused or plans nothing.
- `Notes` reads the host again when the item is chosen, after the plan before asking.

## Words

§5's, unchanged: the title, the section, the rows' keys and values, the item's labels and descriptions, the two questions. The refusals and the review's notes are the control's.

## Not run

Nothing ran on a VM or a real terminal. The screen has not been called from the main menu.
