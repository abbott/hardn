# The Firewall screen on the Firewall control (`pkg/screens/firewall`, `screen.Firewall`)

> **Unit:** refactor plan slice 4, unit (c), `next/s4-screen`, from 088c1026. **Status:** built and tested on macOS, in process: the screen through `Run` on `host_test.go`'s fake, ufw answering from its model. No real terminal, no hardn binary, no VM. Nothing is wired into `cmd/hardn` or `pkg/menu` (unit (d)). The gallery and `TestParity` run `Run`: done, below.
> *Since `next/s4-replace` (unit (d), below): main menu 4 calls `Run` through `MainMenu.SetFirewall`, "View pve-firewall" on Proxmox VE; `-w` runs the Firewall control's Enable, the hostname typed on a terminal, refused off one; `-n -w` shows the plan. The legacy screen is retired, its goldens frozen under `testdata/legacy`. Still nothing run on Linux: the VM checks below are written, not run (gate 5).*

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

## Built in `next/s4-replace`

Unit (d), 2026-11-02; [cmd-engine.md](cmd-engine.md) §2.12 and §2.13 have the whole of it.

- **The record frozen first** (1f10c1f0; [parity-firewall.md](parity-firewall.md) §8): `menu/firewall_*` (48), `menu/firewall_configure_dry_run` and `ops/configure_secure_firewall_profiles/*` (4) moved unchanged to `testdata/legacy/`, their tests retired with them (`TestLegacyMenuFirewall`, `TestLegacyMenuDryRun`, the profiles case of `TestOperationGoldens`); `ops/configure_secure_firewall/*` (4) copied, its case kept for `-r` and Run All (dd863653, its dry run now `-n -r`). `TestLegacyRecord` holds the 57 files' SHA-256. `parity.golden` byte for byte (run).
- **`Run` from item 4** (5d981ac0): `SetFirewall`, set by `withFirewallScreen` in `withHooks`; `Session` the one `engineOptions` holds; the rest as Environment's. 4 reads "View pve-firewall" on Proxmox VE (decision 7), "Configure UFW rules" elsewhere. Under plan only it opens, Add and Remove no longer refused.
- **`hardn -w`** (d4a5ac35; decision 4): Enable through `runControls` with a `typedConfirm`: on a terminal the plan, "Enable the firewall?", "Type the hostname", the timer armed, "sudo hardn confirm"; off one a lockout-risk plan refused, `PLAN_REFUSED`, nothing armed. `-n -w` and hardn.yml's `dryRun: true` show the plan, 3.
- **Deleted, each with its grep** ([parity-firewall.md](parity-firewall.md) §8): `pkg/menu/firewall_menu.go` and the legacy screens' firewall entry, `MenuManager.ConfigureSecureFirewall` and `GetFirewallStatus`. **Stays:** `FirewallManager.ConfigureSecureFirewall` and the UFW repository, which `security_manager.go:58` (Run All, `-r`) calls, until slice 8.
- **Decision 8:** `ufwDefaultIncomingPolicy` and `ufwDefaultOutgoingPolicy` warn on load where hardn.yml sets them ("hardn.yml's ufwDefaultIncomingPolicy isn't used"), the file still accepted.

## VM checks (written, not run; gate 5)

As root through each VM's login user, from the Terminal panel, on Debian 13, Ubuntu 26.04 and Alpine 3.23, each host snapshotted and put back after; `touch` a stamp and `sleep 2` first; after each, `find /etc /var/backups /var/lib/hardn -newer <stamp>` and `ufw status verbose`. No Proxmox VE VM: "View pve-firewall", the read-only screen and `-w`'s `UFW_NOT_APPLICABLE` are not checked on a host.

1. **4 opens** (`sudo hardn`, 4): the sections ufw, SSH, Rules, Application Profiles; 0 back to the main menu; nothing written by opening.
2. **Install** (no ufw; on Alpine, whose network now works): "Install ufw?", y; apt or apk adds ufw. On Alpine, apk adds ufw with iptables, iptables-openrc, libnftnl, libxtables, ufw-pyc and ufw-openrc (read on the VM 2026-11-03): the state after names them ("installed, inactive, with …"). Enable on Alpine then needs `ss` (iproute2): without it, §6's refusal.
3. **Enable**: "Enable the firewall?", "Type the hostname" with the name `hostname` prints; a wrong name asked again; applied, the countdown, "sudo hardn confirm". `sudo hardn confirm` from a new SSH session keeps it: `ufw status verbose` active, deny incoming, SSH's port allowed; at boot Yes (`systemctl is-enabled ufw`, or `rc-update show` on Alpine).
4. **Not confirmed**: Enable again from a snapshot, no confirm; after 180 s ufw is inactive as before, the boot service as it was, SSH still open.
5. **Disable** (3, or 1 where it is item 1): "Disable the firewall?", y; `ufw status` inactive, every port open.
6. **Add** (2, 1): a profile WebServer, ports 80/tcp,443/tcp: hardn.yml holds it, ports one entry each; a name ufw refuses, said at the field.
7. **Apply** (2, 3): `/etc/ufw/applications.d/hardn` holds the ports joined by `|`; `ufw app list` lists WebServer; `ufw status` allows it.
8. **Remove** (2, 2): hardn.yml without it; the rule ufw holds stays ("A rule already in ufw stays.").
9. **Plan only** (`sudo hardn -n`, 4): every item, Add and Remove too, stops at its plan, no hostname asked; nothing written but the run directory.
10. **`-w` on a terminal**: as check 3, exit 0 after confirm; 0 or q at the question exits 3, nothing changed; Ctrl-D exits 130.
11. **`-w` off a terminal** (`sudo hardn -w < /dev/null`, or over `ssh host 'sudo hardn -w' | cat`): with ufw inactive, the plan, "Enabling the firewall here risks a lockout (PLAN_REFUSED)", "To go on: run hardn -w from a terminal, which asks you to confirm.", exit 1; no revert timer (`ps`), no boot unit, ufw as it was.
12. **`-n -w`**, and hardn.yml's `dryRun: true` with `-w` alone: the plan, no question, exit 3, on a terminal and off; nothing written but the run directory.
13. **The two keys**: hardn.yml with `ufwDefaultIncomingPolicy: allow` and `ufwDefaultOutgoingPolicy: deny`: at start "hardn.yml's ufwDefaultIncomingPolicy isn't used" and the same for ufwDefaultOutgoingPolicy; hardn goes on, the file accepted; Enable still sets deny incoming.

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
- New: "ufw can't use that name. Start it with a letter or digit, then use letters, digits, spaces, dots, dashes, underscores or +." and "ufw can't use that name. It keeps all and numbers for itself.", where the mock said "Use letters, digits, dots, dashes and underscores.": the name check is now ufw's, which takes spaces and `+`.
- Decision 7's row: key "ufw", "Left on", note "Run ufw disable"; "Not read" where ufw.conf can't be read.

## Against the mocks

Run: each of the 19 hosts' plain-80 screen is line for line the gallery's `screen-<host>` golden. The one exception is `profiles`, whose profiles are the parity fixture's (WebServer, Metrics), not the gallery's. What the mocks never drew is new: decision 7's row, and a failed reading drawn as Unknown rows where the gallery drew an empty screen.

## Not run

Every apply ran on the fake: ufw's answers come from its model, the tools' from `host_test.go`; unit (d)'s on hostfake with `cmd/hardn/firewall_test.go`'s ufw model. Nothing ran on a VM, a terminal or the binary. How the screen reads on a real Proxmox VE node with ufw left on: not run.
