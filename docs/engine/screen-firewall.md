# The Firewall screen on the Firewall control (`pkg/screens/firewall`, `screen.Firewall`)

> **Unit:** refactor plan slice 4, unit (c), `next/s4-screen`, from 088c1026. **Status:** built and tested on macOS, in process: the screen through `Run` on `host_test.go`'s fake, ufw answering from its model. No real terminal, no hardn binary, no VM. Nothing is wired into `cmd/hardn` or `pkg/menu` (unit (d)). The gallery and `TestParity` run `Run`: done, below.

## What it does

Main menu 4 as adopted on 2026-11-01 ([gallery-firewall.md](gallery-firewall.md) §5, all ten decisions, §6's words), drawn from `firewall.Read` and the Firewall row (`Incoming.Check` through `output.StatusViewOf`, no engine), writing nothing, under plan only too. The row is Check's own reading, a second `ufw status`, kept on purpose so that it is the main menu's row to the letter.

- **ufw's screen:** sections ufw, SSH, Rules, Application Profiles. Item 1 by `Reading.First`: Install, Enable in the words of what its plan does (`Reading.Does`), Disable. A refused item 1 keeps its words and says the control's refusal when selected, before asking. Item 2 where ufw is installed; item 3, Disable, where `DisableBeside`.
- **The profiles' screen:** Add, and with a profile Remove and Apply. Add's fields are checked at the field by `ufw.ValidProfile`; a name held in any case is a warning. Remove's Select refuses a number no profile has. Apply's refusal is said by planning `ActionApplyProfiles` first. Add and Remove with no hardn.yml read say `screenkit.NotLoaded`'s words before asking. After a save, `Config` adopts it (`screenkit.Adopt`).
- **Proxmox VE:** "Firewall Configuration", read only, Return alone. No ufw call, and no item offered, so hardn.yml plays no part. Decision 7's row, from `PVEUFWEnabled`.
- **Each change** runs through `screenkit.Session.Change`: the review, y/N, or the hostname typed for a lockout-risk plan (`p.Typed()`), the apply, the screen read again. A lockout-risk item 1, live, whose hostname can't be read, says so before asking; a plan that turns lockout-risk by the key says so in place of the question. The question's words are the draw's; only the confirmation follows the plan made at the key. Plan only stops every item at its plan, Add and Remove too, and needs no hostname.
- **0** returns (nil), **q** quits (`nav.ErrQuit`), the end of the input and Ctrl-C abort (`prompt.ErrAborted`), from every screen, question and the form.

## API

```go
firewall.Run(ctx, firewall.Options{
	Prompter, System, Facts /* detect's */, Session /* revert.Session, as the caller reads it */,
	Config, NewEngine, ApplyContext, DryRun, LogPath, Now, Ran,
}) error // misuse (no In, Out, System, Config or NewEngine) wraps fs.ErrInvalid
```

- `pkg/ui/screen`: `Firewall` (`SubMenu`, `FirstChange`), `FirewallPVE`, `FirewallProfiles` (`SubMenu`, `ApplyChange`); `FirewallFirst` and its values; `FirewallDisableChange`, `FirewallAdd*`, `FirewallRemove*`; the item keys; `FirewallStatusKey`, `FirewallPolicyKey`, `FirewallBootKey`. No host type enters `pkg/ui`: `rows.go` maps them.
- `pkg/screens/screenkit` (its own commit): `Hostname`, `TypeTheHostname`, `Session.CantConfirm`, moved from Environment, whose goldens are unchanged.

## The gallery stage: done

- **`TestParity`** drives `Run` through `screenRun` (00aa7015; [parity-firewall.md](parity-firewall.md) §7).
- **The gallery** plays `Run` from `fwScreen` on the mocks' hosts, with `detect.Detect`'s facts, `fwSession(h)`, `statusConfig` and the gallery's engine (5a059f44; [gallery-firewall.md](gallery-firewall.md)). The mocks, their copies of the screen's code and the tests moved here are retired.

## What unit (d) must do

- Call `Run` from main menu 4, with `engineOptions`' session and engine, and Ctrl-C's `ApplyContext`. Label 4 "View pve-firewall" on Proxmox VE (decision 7).
- `hardn -w` (decision 4), decision 8's warnings, and `docs/configuration.md:140` and `:201` (control-firewall.md).
- Retire `pkg/menu/firewall_menu.go`, and freeze its characterization goldens.

## Decisions made here

- On Proxmox VE no item is offered, so hardn.yml plays no part; `firewall.Read` gets it as on any host.
- Proxmox VE not established: ufw's title, the row ("can't tell if Proxmox"), and item 1 "Enable firewall", refused in the control's words. No section.
- pve-firewall not read (a done context, `/etc/pve` not mounted): each row Unknown under the row's own words. ufw's state not read: Installed Unknown, item 1 refused.
- Decision 7's row goes last in pve-firewall, above the muted line. Nothing is drawn where ufw.conf says No.
- Only item 1 is checked for the hostname before asking, its plan read with the screen. Apply's plan is plain (`steps.PlanUFWApps`), so it is not. A plan that turns lockout-risk by the key says the same in place of the question, as Environment's does (`TestHostnameAtTheQuestion`).
- Add checks each field with the others filled with what ufw takes. So a name may hold spaces and `+`, which the gallery refused, and "all" or a number is refused. Ports split on commas, each one entry; ufw's file joins them with `|`.
- `NotLoaded` gets the control's save id, `firewall.ID+"/profiles"`, which the control does not export. Its words don't depend on it. Apply saves no hardn.yml, so it is not refused there.
- The profiles' screen reads the host again each round, the Firewall row too, which it does not draw.

## Words proposed

The decided words (§6) are used as decided. These are new to §6 and to control-firewall.md's proposed list:

- From the adopted mocks, not in §6: "Save a profile in hardn.yml", "Take one out of hardn.yml", "Let their ports through ufw"; the review's "Package"; "Profile name", "Such as WebServer.", "Title", "Description", "Ports", "Such as 80/tcp,443/tcp.", "Remove which profile?", "A profile with this name already exists."; the rows "Listeners", "Finding".
- New: "ufw can't use that name. Use letters, digits, dots, dashes and underscores." The mock's second sentence follows a new first one, as the name check is now ufw's.
- Decision 7's row: key "ufw", "Left on", note "Run ufw disable"; "Not read" where ufw.conf can't be read.

## Against the mocks

Run: each of the 19 hosts' plain-80 screen is line for line the gallery's `screen-<host>` golden. The one exception is `profiles`, whose profiles are the parity fixture's (WebServer, Metrics), not the gallery's. What the mocks never drew is new: decision 7's row, and a failed reading drawn as Unknown rows where the gallery drew an empty screen.

## Not run

Every apply ran on the fake: ufw's answers come from its model, the tools' from `host_test.go`. Nothing ran on a VM, a terminal or the binary. How the screen reads on a real Proxmox VE node with ufw left on: not run.
