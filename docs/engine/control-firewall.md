# The Firewall control (`pkg/engine/controls/firewall`)

> **Unit:** refactor plan slice 4, unit (b), `next/s4-control`. **Status:** built and tested on macOS against `hostfake` and the gallery's model; nothing ran on a VM or through the binary, and nothing is wired into `pkg/menu` or `cmd/hardn`.

`Incoming` is the main menu's Firewall row ([check-firewall.md](check-firewall.md)) and every change the Firewall screen makes. It installs ufw, enables or disables it, saves and applies application profiles, and refuses before anything changes. On Proxmox VE, or where that is not established, every action refuses before any ufw call. The gallery (`go run ./internal/tools/uigallery firewall`, [gallery-firewall.md](gallery-firewall.md)) now runs on it.

## API

```go
firewall.Incoming{Action: firewall.ActionEnable} // ActionNone, ActionInstall, ActionEnable, ActionDisable, ActionSaveProfiles, ActionApplyProfiles
firewall.Read(ctx, sys, facts, cfg, session) firewall.Reading // reads only; what the screen draws, as facts
firewall.ReadSockets(ctx, sys, facts) firewall.Sockets        // the one reading of the SSH listeners
```

- **`Controls()`** stays `Incoming{}`, `ActionNone`, whose `Plan` is `engine.ErrNotMigrated`: nothing that plans over the status list can plan a lockout.
- **`Reading`:** `OnPVE`, `Proxmox`, `Profiles`, `PVE`, `PVEErr`, `PVEUFWEnabled`, `PVEUFWErr`, `State`, `StateErr`, `Boot`, `BootUnit`, `Sockets`, `First` (`ItemNone`, `ItemInstall`, `ItemEnable`, `ItemDisable`), `DisableBeside`, `Plan`, `PlanErr`, `Does` (`Allows`, `SetsDeny`, `StartsUFW`, `Boot`); methods `Coverage()`, `Applied(name)`. `Plan` and `PlanErr` are item 1's, where it is Install or Enable.
- **`Sockets`:** `Conf`, `Read`, `Bound`, `Unbound`, `NoSS`, `SSFailed`, `Why`; `Established()`, `Ports()`, `NoneBound()`, `Listeners()`, `Stray(port)`.
- **Errors:** `ErrNoSS`, `ErrSSFailed`, `ErrNotInEffect`, `ErrRefused`, `ErrBootUnknown`, `ErrBootBlocked`; `steps.ErrSessionPort`. Each refusal is a `*steps.Refusal` whose `Why` and `Fix` `screenkit.RefusalWords` says.
- **`pkg/host/ufw`:** `StepDisable`, `StepAllowApp`, `PlanDisable`, `PlanApps`, `Profile`, `ProfileFile`, `ProfileBytes`, `ValidProfile`, `ReadEnabled`, `ReadBoot`, `ReadBootUnit`, `BootMasked`, `HasApp`, `ErrInactive`, `ErrBadProfile` ([ufw.md](../safety/ufw.md)).
- **`pkg/engine/steps`:** `PlanUFWDisable`, `UFWBootChange`, `PlanUFWApps`.
- **`pkg/host/packages`:** [packages.md](../safety/packages.md).

## What units (c) and (d) must do

- **Unit (c), the screen:** draw from `Reading`, the host's facts (the At boot row's init kind), the config (Apply, Add, Remove) and `Check`'s row, as `fwReading` does; move the words, rows and layout out of `internal/tools/uigallery/firewall.go`. Apply's refusal is said by planning `ActionApplyProfiles` first, as the gallery's `applyRefusal` does: `Reading` does not hold it.
- **Main menu 4** reads "View pve-firewall" on Proxmox VE, and a node with ufw left on gets its warning row from `PVEUFWEnabled` (decision 7). The gallery draws neither.
- **`hardn -w`, unit (d):** in `runControls` (`cmd/hardn/dns.go:71`), on a terminal ask the typed hostname and arm the timer; off a terminal refuse (decision 4).
- **Decision 8:** `ufwDefaultIncomingPolicy` and `ufwDefaultOutgoingPolicy` warn on load in `pkg/config`, as `logFile` does.
- **Docs:** `docs/configuration.md:140` says adding or removing a profile is refused in plan only; each now stops at its plan. The profile section at `docs/configuration.md:201` needs the sub-screen and the ports' `|` form.

## Engineering calls

- `ValidProfile` also takes `n:m/proto`, which the gallery's form accepts; a profile name used twice is refused.
- A right profile file with the wrong mode or owner counts as needing a write.
- `PlanApps` also matches `%20` in a saved app name [I].
- `firewallChange` stays lockout-risk; `PlanUFWDisable` alone sets its change plain.
- The boot change goes before ufw's: its undo, a boot link, runs last and cannot hold up `ufw disable`.
- A loopback-only rule counts as refused in `verdicts`, as in `Check`. Enable refuses on what ufw would still refuse once its plan ran.
- `linked`, `linked-runtime` and `not-found` read No, as `detect`'s table; `masked` and `not-found` refuse, with what to do. Boot not read refuses Enable always, and holds the row from Configured.
- Where Install, or Enable on an inactive ufw, refuses, item 1 keeps its own words, as the adopted mocks draw it; the refusal is said on selecting it.
- A refused listener's words drop `(dual-stack)` from the address.
- On Proxmox VE the refusal is §6's "hardn reads pve-firewall…" sentence, code `UFW_NOT_APPLICABLE`.
- `Read` also plans Install, so its refusal is said before asking. Each draw of a host with no ufw runs the simulation, which changes nothing.
- Install's state after names the other new packages a simulation adds.
- Install's undo is journalled before it runs, and replaced by `Report.Undo` after.
- Other packages are named by the second word of each change the simulation lists, or the change itself when it is one word.
- Mock and play notes that named stand-ins changed, so every `play/firewall-*` golden did. No other golden outside the Firewall listing changed.

## Findings

- Apply with no profile left plans nothing: the last one removed stays in the file, and an inactive ufw does not refuse. The sub-screen hides Apply there.
- `revert.Rollback` restores backups before undos, so rolling back a first Apply runs `ufw delete … app` with the profile file gone. If ufw minds [I], the rollback stops incomplete with the allow left: no lockout.
- A changed profile's existing rule keeps its old ports without `ufw app update` [I].
- `TestLiveStatusOnHosts` (`pkg/menu`, protected here) pins the status pass's command count: the row's read of `ufw.service` adds one, so it fails until its `statusRuns` says 29, 32, 29.

## Words proposed

- Install: "hardn can't find ufw in apt's package lists. Run apt-get update, then try again."; "hardn can't find ufw in apk's package lists. Check the network and the community repository, run apk update, then try again."; "Installing ufw would also change other packages: \<names\>. Install it by hand, then try again."; "hardn can't install packages on this system."
- Install's change: "Install ufw", "not installed", "installed, inactive" (the gallery's, kept).
- Disable's change: "Disable ufw" (the gallery's, kept).
- Profiles: "ufw can't read the profile \<name\>. Correct it in hardn.yml, then try again."; the title "Apply the application profiles".
- Enable: "hardn can't tell which ports SSH uses." (the gallery's, not in §6); the title "Start ufw at boot"; "hardn can't tell which port this session uses. Run hardn outside tmux or screen, or from a console, then try again."; "Port 22 is bound, sshd's config doesn't list it, and ufw would refuse it. Reload sshd, stop what's on it, or run ufw allow 22/tcp, then try again."; "ufw's service is masked, so it can't start at boot. Run systemctl unmask ufw.service, then try again."; "ufw's service isn't on this system, so it can't start at boot. Reinstall ufw, then try again."
- No ufw: "ufw isn't installed. Install it, then try again." At boot: "Service masked", "No service". The row's detail: "can't tell boot state". Install's state after: "installed, inactive, with \<names\>".
- Evidence: "OpenRC: ufw in no runlevel", "systemctl is-enabled ufw.service: not enabled".
- The `what` text of the new codes ([output.md](../reference/output.md)).

## Facts, from the VMs (read only, 2026-11-02)

Under `_support/hardn-audit-2026-10-16/vm-runs/s4/unit-b/`, outside this repo.

| Fact | Log |
|---|---|
| `ufw enable` does not touch the unit: ufw's Python has `set_enabled` and no `systemctl`; `ufw.service` is enabled, `WantedBy=multi-user.target`, its start `ufw-init` | `deb13-vm.log`, `ubuntu26-vm.log` |
| `apt-get install -s` prints `Inst x (ver …)`, `Conf x (ver …)`, `Remv x [ver]` | `deb13-vm-2.log` |
| A name apt does not know is `E: Unable to locate package x`, exit 100 | `deb13-vm.log`, `ubuntu26-vm.log` |
| Stock Alpine 3.23 has no ufw, no `ss`, no `iproute2`; main and community are on; the index is empty | `alpine323-vm.log` |
| An empty index and no DNS both say `ufw (no such package)`, exit 1 | `alpine323-vm.log`, `alpine323-vm-2.log` |
| The Alpine VM has no DNS, so no successful apk simulation has been seen | `alpine323-vm-2.log` |

So `alpine-stock` refuses Install, and with ufw there Enable refuses with §6's `ss` words until `iproute2` is installed. Install does not bring it (decision 1).

## Not run

Every install and undo; `apk add --simulate`'s success, whose form is `pkg/host/alpine`'s tests', and a new ufw on either tool, whose form is another package's [I]; `apt-get install -y` with no `DEBIAN_FRONTEND` [I]; what undo leaves behind [I]; `PlanApps`, `PlanDisable` and `ReadBoot`'s No; `systemctl enable` on a masked unit, which the model fails from memory [I], and `rc-update del` on a service in no runlevel, a boot undo that may stop a rollback after ufw's undos ran [I]; that a disabled `ufw.service` stops ufw at boot, which the unit file implies and no VM has rebooted to show; `ufw status`' form for a multi-port profile, from memory. ufw and the package tools answer from `firewallhost.go`'s model, sources there.
