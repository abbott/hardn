# SSH Login, SSH Auth and SSH Port: the SSH status rows (`pkg/engine/controls/ssh`)

> **Unit:** refactor plan Phase 4, `next/p4-check-ssh` (plan §2, row `p4-check-<area>`, ssh: "`sshd -T` set ∪ socket Listen set"; brief Phase 4, "Where each status comes from"; brief 2.5, Proxmox VE's root SSH; brief §3 principle 3; decision §3.18, approved 2026-10-17: SSH stays on 22 on Proxmox VE; [P-05](../refactor/proposals.md#p-05), the SSH statuses from `sshd -T`, proposed, which this builds without its firewall cross-check, `next/p4-risk`'s). The first Phase 4 unit: it sets the check-only pattern of [checks.md](checks.md). It takes up the "Status sources" item of [docs/safety/README.md](../safety/README.md) for SSH, [sshd-effective.md](../safety/sshd-effective.md) §6's and [pve.md](../safety/pve.md) §6's `p4-check-ssh` items, and [steps.md](steps.md) §2.15's `/run/sshd` for a check. V-numbers: V13's status half (the SSH rows read from files, so a drop-in read first was not seen) and origin/main's SSH Port row from `hardn.yml` instead of the effective port ([P-05](../refactor/proposals.md#p-05)).
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only, with `sshd -T`, `systemctl show -p Listen ssh.socket` and `ss -Htln` answered by a model of the fixtures' files (§3). Nothing has run on Linux: everything host-specific is "written, not run (gate 5)" (§4). Every detail below is **proposed wording**, for the maintainer's review (§6).

Figma's main menu has three SSH rows: SSH Login "root disallowed", SSH Auth "key-only auth", SSH Port "non-default (2230)". Each is now a check-only control (`ssh.login`, `ssh.auth`, `ssh.port`) whose `Check` reads what sshd runs with from its files, as `sshd -T` prints it, with `ssh.socket`'s listeners on a socket-activated host and the sockets that are bound, and on Proxmox VE `pve.CheckSSH`. `Plan` returns `engine.ErrNotMigrated` until slice 6.

## 1. Why

origin/main's SSH Port row is `cfg.SshPort != 22`, read from `hardn.yml`, and the other SSH rows are read from the files, so a drop-in read first (cloud-init's `PasswordAuthentication yes`) decides the value and nothing says so (V13; [P-05](../refactor/proposals.md#p-05)). On Ubuntu 24.04 and 26.04 `ssh.socket` decides the port, and on Proxmox VE root's key login and port 22 are what the cluster needs, so "root disallowed" and a moved port there are not a hardened host but a broken cluster (brief 2.5). A status row that says a host is hardened when it is not is worse than none (brief Phase 4: prefer Unknown to guessing).

## 2. Guarantees

### Reading

1. **One reading per control, from sshd's own answer** (`read`): on OpenRC the binary Alpine's init script runs (`reload.OpenRCFlavour`); `sshd.ReadEffective` (sshd -T over the installed files, one `sshd -T -C` per Match block, and `systemctl show -p Listen ssh.socket` where the socket may hold the listeners); the files themselves (`migrate.Find`), for their Match blocks (`sshd.ReadOrder`); the bound sockets (`reload.ReadBound`), for the port and, on a host where sshd holds its own sockets, for SSH Login and SSH Auth too (§2.8a); on Proxmox VE, `pve.CheckSSH` over that reading, with root's groups (`pve.RootGroups`) where SSH Login's state depends on them. This is the reading `pkg/engine/steps` makes for a plan ([steps.md](steps.md) §2.1). Each control reads on its own, so `Engine.Check` over the three runs `sshd -T` three times; a reading shared between them would need state across `Check` calls, which the engine does not give, and nothing measured asks for it.
2. **Read-only.** The checks make reads and run exactly `sshd -T -f /etc/ssh/sshd_config` (with `-C` per Match block), `systemctl show -p Listen ssh.socket`, `ss -Htln`, and on Alpine, where `sshd.krb5` or `sshd.pam` is installed, its `-f /etc/ssh/sshd_config -G` (`reload.OpenRCFlavour`, [reload.md](../safety/reload.md) §2.13). Tested: every call of every test is checked (`readOnly`; the Alpine fixture has neither flavour).
3. **A read that fails is the check's error**, reported `Unknown` with the error, whose code `pkg/output` gives ([checks.md](checks.md) §1.5): the detail says what the error shows of the host (§5's table). Before Ubuntu's first SSH connection since boot `sshd -T` fails without `/run/sshd` ([steps.md](steps.md) §2.15): all three rows are `Unknown`, "sshd not started yet", code `SSHD_ENV_NO_RUNDIR`, and no other source is used for them. That detail, and "sshd not running", are shown only where sshd said the directory is missing and detect's unit states establish `ssh.service` stopped (on a socket-activated host, "sshd not started yet" with `ssh.socket` active, "sshd not running" without it); any other `SSHD_ENV_NO_RUNDIR` (the directory not root's alone, `ssh.service` active or its state not established, OpenRC) is "can't read". Tested: `TestUnread`, each failure for each control.
4. **What was read but establishes nothing is `Unknown`, with the findings that say why** (`sources-disagree`, `effective-unknown`: [sshd-effective.md](../safety/sshd-effective.md) §2.2 to §2.6). A finding has no keyword field, so an `Unknown` row carries those whose detail names one of its keywords, as a whole word in any case ("Port", "ListenAddress"), the listeners' words ("listens", "listeners", ssh.socket's "Listen") counting as SSH Port's, and those that name none; when `sshd -T`'s output was not read at all, its one finding is every row's. The conflict report's note that `00-hardn.conf` sets nothing is not about a value and is never shown. The match is on the details' words, which [sshd-effective.md](../safety/sshd-effective.md) does not pin: a `Keyword` field on `sshd.Finding` would make it structural (§6, for the maintainer). Tested: `TestLogin`, `TestAuth`, `TestPort` (a value hardn does not know, a value printed twice, the files and `sshd -T` disagreeing, another keyword's finding left out, a disagreement on another keyword not taken for the listeners').
5. **Match blocks are read from the files, every one of them**, not from the probes, which cover only the connections they could build ([pve.md](../safety/pve.md) §2.6). A block that sets `PermitRootLogin`, `PasswordAuthentication` or `KbdInteractiveAuthentication` (an alias read as its keyword) to a value that lets in more than `sshd -T`'s global one (`no` < `forced-commands-only` < `prohibit-password` < `yes`, [sshd-migrate.md](../safety/sshd-migrate.md) §2.14) keeps the row from `Configured`, with a finding `SSH_MATCH_ALLOWS_MORE` at its line and the line in the evidence. A block's criteria are not evaluated: which connections it matches is not established, so any such block counts. A block whose value hardn does not rank is `Unknown`, "can't read", with `SSH_MATCH_VALUE_UNKNOWN` at its line and the line in the evidence (sshd refuses such a value when it loads the files, **[I]**, so the files changed after `sshd -T` read them, or the ranking lacks a value).
6. **Findings shown beside the state:** for each row's keywords, another file's line read before `00-hardn.conf`'s (`SSHD_OVERRIDDEN`: hardn's value is not the one in effect), after it with another value (`SSHD_SHADOWED`: cloud-init's `PasswordAuthentication yes`), or adding a port (`SSHD_ACCUMULATES`: a `Port 22` elsewhere keeps 22 open) ([sshd-effective.md](../safety/sshd-effective.md) §2.9); `SSH_MATCH_ALLOWS_MORE` and `SSH_MATCH_VALUE_UNKNOWN` (§2.5); `SSH_PORT_NOT_IN_EFFECT` (§2.8a, §2.12) and `SSH_PORT_22_BOUND` (§2.12); on Proxmox VE, `pve.CheckSSH`'s `PVE_ROOT_NOT_KEY_ONLY`, `PVE_ROOT_UNREACHABLE` and `PVE_PROXMOX_ROOT_LOGIN` for SSH Login (the last in place of `SSHD_SHADOWED` at the same line: Proxmox VE's own `PermitRootLogin yes` is not drift, [pve.md](../safety/pve.md) §2.7) and `PVE_PORT_NOT_22` for SSH Port. A primitive's finding is coded by `pkg/engine/steps`' own conversion (`steps.SSHDFinding`, `steps.PVEFinding`), as a plan codes it. Every code is registered; tested: each test checks that every finding's code is a registered finding code.
7. **Evidence** is each value as the command printed it (`sshd -T: permitrootlogin without-password`; `systemctl show -p Listen ssh.socket: [::]:2230`; `ss -Htln: *:2230`, the bound sockets on the configured ports and on 22, or that `ss` is not installed), each Match line that keeps the row from `Configured` (`file:line: text (Match …)`, without its indentation), and on Proxmox VE, where SSH Login's state rests on root's key login, every keyword `pve.CheckSSH` reads for it. Findings are one line each: the listeners as the evidence lists them, `ssh.socket` named only on a socket-activated host. Tested: `TestEvidence`, `TestFindingText`.
8. **Every control applies everywhere** (`Applies` true): a host without sshd is `Unknown` ("sshd not installed", "no sshd_config"), not `NotApplicable`, since detect's units alone do not establish that no SSH server runs, and `NotApplicable` would leave the rows out of the risk level.

8a. **A configuration this run shows not in effect is not reported as the host's** (decision of this unit, from the review): on a host where sshd holds its own sockets (`ssh.service`, OpenRC), a configured listener that `ss` does not show bound means that sshd has not reread its files since they were edited, or could not bind, so `sshd -T`'s other values are not established either: SSH Login and SSH Auth are then `Unknown`, "config not in effect", with `SSH_PORT_NOT_IN_EFFECT` and the bound sockets in the evidence, whatever `sshd -T` says (a password login in the files included: what runs may differ either way). Not on a socket-activated host, where a `daemon-reload` without a restart of `ssh.socket` leaves the sockets behind and says nothing of sshd's other settings; not with the listeners not established, nor without `ss`'s reading (missing or failing): there the rows are `sshd -T`'s, the case §6 leaves not modelled. A socket on 22 the configuration lacks (§2.12) does not count: after the confirm clean-up's crash the running sshd's other settings are the confirm window's ([confirm-cleanup.md](confirm-cleanup.md) §2.4), and the socket may be another program's. Tested: `TestLogin`, `TestAuth` (an `ssh.service` host; a socket host; `ss` failing).

### SSH Login (`ssh.login`)

9. **Off Proxmox VE** the baseline is `PermitRootLogin no`. The state is `sshd -T`'s global value, unless a Match block lets root in more (§2.5): `no` is `Configured`; `prohibit-password` (`without-password` before OpenSSH 10.1) and `forced-commands-only` are `Partial`; `yes` is `NotConfigured`; a Match block that lets in more than a global `no`, `forced-commands-only` or `prohibit-password` is `Partial`.
10. **On Proxmox VE** the baseline is root key-only (`prohibit-password`, never `no`) with root reachable by key from the other nodes (brief 2.5, [pve.md](../safety/pve.md) §6): `Configured` only when `pve.CheckSSH`'s `RootKeyOnly` and `RootByKey` are both `Yes`; `RootByKey` `No` (a `DenyUsers root`, `PubkeyAuthentication no`, …) is `Partial`, the cluster's root login broken; `Unknown` when it is not established (a narrowed `ListenAddress`; a Match block for root that may keep its key out), never `Configured`. `yes` is `NotConfigured`; `no` and `forced-commands-only` are `Partial`: root has no password login, which is the point of hardening, but the cluster cannot log in, with `PVE_ROOT_UNREACHABLE`. `PVE_PROXMOX_ROOT_LOGIN` is shown and is not a risk factor (pve.md §6). Root's groups are read only where the state depends on them (`RootKeyOnly` `Yes`, so `RootByKey` decides): a failure to read them is then `Unknown` with its error, and elsewhere the state stands, with `pve.CheckSSH`'s findings made without them. **The port is SSH Port's row, not this one's** (decision of this unit, from the review): [pve.md](../safety/pve.md) §6 hands on one status, `Configured` only when key-only, root by key and port 22 are all `Yes`; the three rows split it, so SSH Login is `Configured` on a host moved off 22 while SSH Port is `NotConfigured` with `PVE_PORT_NOT_22`. The host is not shown safe, and the port is not counted twice in the risk level; whether SSH Login should require `Port22` too is for the maintainer (§6). **When whether the host runs Proxmox VE is not established**, a state both baselines give is reported (`yes` "root allowed", a Match block that lets root in more, `forced-commands-only`); otherwise the row is `Unknown`, "can't tell if Proxmox", with `pve.ErrProxmoxUnknown` (`PVE_UNKNOWN`) and the evidence, since the two baselines differ. SSH Port alike, after its own reading (§2.12).

### SSH Auth (`ssh.auth`)

11. **Key-only, the same everywhere, Proxmox VE included:** `PasswordAuthentication no`, `KbdInteractiveAuthentication no` and `PubkeyAuthentication yes`, with no Match block turning password or keyboard-interactive authentication on, is `Configured`. Password authentication on is `NotConfigured`, whatever else is unknown; keyboard-interactive on is `NotConfigured` too, **whether or not sshd uses PAM** (decision of this unit): with PAM it asks for the password (**[I]**); without PAM (Alpine's stock sshd) it is likely to offer no password prompt, but what it offers depends on the build, which hardn does not read, and making the state depend on `sshd -T`'s `usepam` would rest on facts not read for this unit (whether a build without PAM prints it). The error is pessimistic, never a host shown safe; for the maintainer (§6); both off with public keys off too is `Partial` (no password login, but no key login either); a Match block that turns either on is `Partial`. `AuthenticationMethods` is not read (§6).

### SSH Port (`ssh.port`)

12. **The listeners, not the Port lines, and bound, both ways.** The listener set is `sshd -T`'s `listenaddress` lines ∪ `ssh.socket`'s `Listen` entries on a socket-activated host (`sshd.Listeners.All`), which must be established (`Listeners.Known`: every source read, the socket and sshd agreeing, the files and `sshd -T` agreeing on Port, the unit that listens up; [sshd-effective.md](../safety/sshd-effective.md) §2.5) or the row is `Unknown`, with the reason where detect's facts and the findings show it. Both sources are configuration, so the listeners are then checked against the bound sockets (`reload.ReadBound`, `reload.Unbound`, the rule `reload.CheckInEffect` uses): a configured listener that is not bound (an edit not reloaded; on Ubuntu a `daemon-reload` without a restart of `ssh.socket`) is `Unknown`, "not listening on 2230" ("2230 on another address" where each such port is bound, on other addresses only), with `SSH_PORT_NOT_IN_EFFECT`: what accepts connections is then not what is configured. The other way, off Proxmox VE, **a socket bound on 22 while no configured listener is on 22** is `Unknown`, "22 still open", with `SSH_PORT_22_BOUND` and that socket in the evidence: `Port 22` dropped and sshd not reloaded, as the confirm clean-up leaves the host when killed between its write and its reload ([confirm-cleanup.md](confirm-cleanup.md) §2.4), or another program on 22; `ss` without `-p` does not say whose a socket is, hence `Unknown`, not `Partial` ([reload.md](../safety/reload.md) §2.10a: `CheckInEffect` does not see such a socket either). Only 22 is looked at: another port bound is not taken for sshd's (a web server's 80), so a stale non-default port (2230 kept bound after a move to 2231) is not seen. **Without `ss`**: on a socket-activated host `Unknown`, "ss not installed", with the error (`COMMAND_NOT_FOUND`), as `reload.CheckInEffect` fails there: after a `daemon-reload` `sshd -T` and `ssh.socket` agree on the new port whatever is bound. Elsewhere (a stock Alpine is said not to ship `ss`, **[unverified]**, [reload.md](../safety/reload.md) §3) the configured set stands, and the evidence says that the bound sockets were not read: those `Configured` lines come from configuration alone (§5 marks them), for the maintainer (§6). Whether Proxmox VE runs is read after these, so an `Unknown` they establish is shown rather than "can't tell if Proxmox".
13. **Off Proxmox VE** every listener on a port other than 22 is `Configured`; 22 alone is `NotConfigured`. **22 beside another port is `Partial`** (decision of this unit): the move is half done, as during a confirm window, where `00-hardn.conf` holds `Port 22` beside `Port 2230` until `hardn confirm` drops it (P-40), or while another file adds `Port 22` (`SSHD_ACCUMULATES`). Not `NotConfigured`, since the new port is there and the plan's own clean-up is what finishes it; not `Configured`, since 22 still accepts connections, which a non-default port is meant to avoid.
14. **On Proxmox VE** the baseline is port 22 alone (§3.18: cluster SSH and pve-firewall's management rule use it): `pve.CheckSSH`'s `Port22` `Yes` is `Configured`, `No` is `NotConfigured` with `PVE_PORT_NOT_22`, and `Unknown` stays `Unknown`. A port other than 22 there is not hardening but a lockout for the cluster. The detail names the host's ports as off Proxmox VE ("default (22)", "non-default (2230)", "22 and 2230"; "other ports configured" where every listener is on 22 and a `Port` line names another), and the state says what they mean there. A socket on 22 the configuration lacks is not looked for: `Configured` there needs 22.

### Check-only

15. **`Plan` returns `engine.ErrNotMigrated`** (`engine.CheckOnly`), and `Engine.Plan` over the three controls is refused, each item's error coded `CONTROL_NOT_MIGRATED`, with no change and no call but the run directory's (`TestCheckOnly`).
16. **Through `host.System` only:** depguard rule `controls-through-host` ([checks.md](checks.md) §1.11), checked to report `os` and `pkg/ui` imports in a throwaway file of this package (in a temporary copy of the tree; nothing committed).

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| `sshd -T` reads the files on disk; a daemon not reloaded since an edit runs with what it read before | [sshd-effective.md](../safety/sshd-effective.md) §2.1 | §2.8a, §2.12 (checked against the bound sockets); §6 (otherwise not) |
| sshd's re-execution on `HUP` closes its listening sockets and binds what its files name; so on `ssh.service` and OpenRC a configured port not bound means the running sshd did not reread them (or could not bind) | [reload.md](../safety/reload.md) §2.10a, §3 | §2.8a |
| OpenSSH 10.0p1 (Debian 13, Proxmox VE 9) prints `without-password`, 10.2p1 (Ubuntu 26.04) and 10.3_p1 (Alpine 3.24) `prohibit-password`; one value | [sshd-effective.md](../safety/sshd-effective.md) §3 | §2.9; the fixtures |
| `PermitRootLogin`'s values from fewest login paths to most: `no` < `forced-commands-only` < `prohibit-password` < `yes` | [sshd-migrate.md](../safety/sshd-migrate.md) §2.14; [pve.md](../safety/pve.md) §3 (sshd_config(5), read 2026-10-18) | §2.5, §2.9 |
| A satisfied Match block's values replace the global ones for that connection; aliases read as their keyword | [sshd-effective.md](../safety/sshd-effective.md) §3 (servconf.c) | §2.5 |
| With `UsePAM yes`, keyboard-interactive authentication runs PAM's conversation, which asks for the password on a stock PAM stack; without PAM what it offers depends on the build | **[I]** (not read for this unit) | §2.11: the decision does not depend on it |
| sshd checks a Match block's values when it loads the files, whether or not the block matches, and refuses one it does not know | **[I]** (servconf.c, not read for this unit) | §2.5 (`SSH_MATCH_VALUE_UNKNOWN`) |
| `ss` without `-p` does not say which process holds a socket | [reload.md](../safety/reload.md) §2.10a | §2.12 ("22 still open" is `Unknown`) |
| `ssh.socket` holds the listening sockets on Ubuntu 24.04 and 26.04; `systemctl show -p Listen` and `sshd -T` are configuration, not bound sockets | [sshd-effective.md](../safety/sshd-effective.md) §2.5, §3 | §2.12 |
| `ss -Htln`'s `*` is a dual-stack socket and covers both families; `ss` does not say whose a socket is without `-p` | [reload.md](../safety/reload.md) §2.10, §2.10a, §3 | §2.12 |
| `ss` is installed on a stock Debian, Ubuntu and Proxmox VE, and not on a stock Alpine | [reload.md](../safety/reload.md) §3 **[unverified]** | §2.12 |
| `sshd -T` fails without `/run/sshd`, which only `ssh.service` creates and which is gone while it is stopped; a socket-activated Ubuntu before its first connection is in that state, `ssh.service` inactive until then | [steps.md](steps.md) §3 (**[V]** for Debian; **[I]** for Ubuntu) | §2.3; the details "sshd not running" and "sshd not started yet" |
| Proxmox VE: root key-only and port 22, which the cluster's root SSH and pve-firewall's management rule use | brief 2.5; decision §3.18; [pve.md](../safety/pve.md) §1, §6 | §2.10, §2.14 |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root. On the VMs of [vmtest.md](../safety/vmtest.md) (Debian 13, Ubuntu 26.04, Alpine 3.24) and by hand on Proxmox VE 9, through a dev-only driver or the gallery's status scenario once one calls `Engine.Check` with these controls:

- each stock host: the three rows of §5 as installed, compared with `sshd -T`, `systemctl show -p Listen ssh.socket` and `ss -tlnp`;
- after the brief's 2230 change: in the confirm window "22 and 2230", after `hardn confirm` "non-default (2230)", and on Ubuntu between `daemon-reload` and the restart of `ssh.socket` "not listening on 2230";
- an edit of `Port` in a drop-in on Debian 13 without a reload: SSH Port "not listening on 2230", SSH Login and SSH Auth "config not in effect", and after `systemctl reload ssh.service` the new rows;
- `Port 22` dropped from `00-hardn.conf` in the confirm window and sshd not reloaded (the clean-up killed before its reload): "22 still open"; then the reload: "non-default (2230)";
- Ubuntu 26.04 without `iproute2`, if it can be removed: SSH Port "ss not installed";
- Ubuntu 26.04 from the console after a boot, before any SSH connection: three rows "sshd not started yet"; Debian 13 with `ssh.service` stopped: "sshd not running";
- Alpine 3.24 with and without `iproute2` (whether `ss` is there, §3);
- cloud-init's `50-cloud-init.conf` (`PasswordAuthentication yes`) under hardn's `00-hardn.conf`: `SSHD_SHADOWED` beside "key-only auth"; a Match block with `PasswordAuthentication yes`: "match block allows passwords";
- Alpine 3.24's sshd with `PasswordAuthentication no` alone: "kbd-interactive on", and whether that sshd offers any keyboard-interactive prompt (§2.11);
- Proxmox VE 9 stock ("root allowed", "default (22)"), then with hardn's `00-hardn.conf` from `pve.PlanSSH` ("root key-only (Proxmox)" with `PVE_PROXMOX_ROOT_LOGIN`);
- the time `Engine.Check` takes over the three controls (three readings, §2.1).

## 5. The status lines

Every line the three rows can show, in Figma's grammar (marker, label, the state's word, the detail). **Every detail is proposed wording**; Figma's "root disallowed", "key-only auth" and "non-default (2230)" are the anchors. A port list is the ports in ascending order, joined by ", ". The lines the review asked to be read again are listed after the tables, with alternatives.

**SSH Login, off Proxmox VE**

| The host (`sshd -T`; Match blocks from the files) | State | Detail |
|---|---|---|
| `PermitRootLogin no`, no Match block lets root in more | Configured | root disallowed |
| `prohibit-password` (or `without-password`), nothing more in a Match block | Partial | root key-only |
| `forced-commands-only`, nothing more in a Match block | Partial | root commands only |
| `yes` | Not Configured | root allowed |
| `no`, `forced-commands-only` or `prohibit-password`, and a Match block that lets root in more | Partial | match block allows root |

**SSH Login, Proxmox VE**

| The host (`pve.CheckSSH`) | State | Detail |
|---|---|---|
| `prohibit-password` globally and in every Match block, root reachable by key (whatever the port: §2.10) | Configured | root key-only (Proxmox) |
| the same, and something keeps root's key login out (`DenyUsers root`, `PubkeyAuthentication no`, …) | Partial | root key login blocked |
| the same, and whether root's key login works is not established (a narrowed `ListenAddress`; a Match block for root that may keep its key out, as `AuthenticationMethods publickey,password`) | Unknown | can't tell if root key works |
| `yes` | Not Configured | root allowed |
| a Match block that lets root in more than the global value | Partial | match block allows root |
| `no` | Partial | root disallowed |
| `forced-commands-only` | Partial | root commands only |
| `prohibit-password`, and a Match block stricter (`no`) | Partial | match block denies root |

**SSH Auth** (Proxmox VE alike)

| The host (`sshd -T`; Match blocks from the files) | State | Detail |
|---|---|---|
| `PasswordAuthentication no`, `KbdInteractiveAuthentication no`, `PubkeyAuthentication yes`, no Match block turning either on | Configured | key-only auth |
| `PasswordAuthentication yes` | Not Configured | password auth on |
| `PasswordAuthentication no`, `KbdInteractiveAuthentication yes`, with PAM or without (§2.11) | Not Configured | kbd-interactive on |
| both off, and a Match block that turns one on | Partial | match block allows passwords |
| both off, and `PubkeyAuthentication no` | Partial | key auth off |

**SSH Port, off Proxmox VE**

| The host (listeners: `sshd -T` ∪ `ssh.socket`; bound: `ss`) | State | Detail |
|---|---|---|
| every listener on a port other than 22 (2230), each bound, nothing bound on 22 | Configured | non-default (2230) |
| the same, `ss` not installed on a host where sshd holds its sockets (Alpine): **from configuration alone** (§2.12, for the maintainer) | Configured | non-default (2230) |
| 22 and other ports | Partial | 22 and 2230 |
| 22 alone | Not Configured | default (22) |

**SSH Port, Proxmox VE**

| The host (`pve.CheckSSH`) | State | Detail |
|---|---|---|
| the Port set is 22 and every listener is on 22 | Configured | default (22) |
| every listener on other ports (2230) | Not Configured | non-default (2230) |
| 22 and other ports | Not Configured | 22 and 2230 |
| every listener on 22, and a `Port` line names another | Not Configured | other ports configured |

**Unknown, every row where it applies**

| The host | Rows | Detail | Error code |
|---|---|---|---|
| a configured listener not bound (`ss`), and nothing bound on its port | Port | not listening on 2230 | none (finding `SSH_PORT_NOT_IN_EFFECT`) |
| a configured listener not bound, its port bound on other addresses only | Port | 2230 on another address | none (finding `SSH_PORT_NOT_IN_EFFECT`) |
| off Proxmox VE, a socket bound on 22, no configured listener on 22 | Port | 22 still open | none (finding `SSH_PORT_22_BOUND`) |
| on `ssh.service` or OpenRC, a configured listener not bound (§2.8a) | Login, Auth | config not in effect | none (finding `SSH_PORT_NOT_IN_EFFECT`) |
| `ss` not installed on a socket-activated host | Port | ss not installed | `COMMAND_NOT_FOUND` |
| how sshd is started not established (detect's SSH mode) | Port | can't tell what listens | none |
| `ssh.socket` not active | Port | ssh.socket not active | none |
| `ssh.service` not active, or OpenRC has not started sshd | Port | sshd not running | none |
| the files and `sshd -T` give different ports | Port | files and sshd differ | none |
| `ssh.socket` and `sshd -T` give different listeners (`daemon-reload` not run) | Port | socket and sshd differ | none |
| `/run/sshd` missing on a socket-activated host, `ssh.service` inactive and `ssh.socket` active (before the first SSH connection, **[I]**) | all | sshd not started yet | `SSHD_ENV_NO_RUNDIR` |
| `/run/sshd` missing, `ssh.service` inactive, on an `ssh.service` host, or on a socket host with `ssh.socket` not active | all | sshd not running | `SSHD_ENV_NO_RUNDIR` |
| any other `SSHD_ENV_NO_RUNDIR`: `/run/sshd` not root's alone, `ssh.service` active or its state not established, OpenRC | all | can't read | `SSHD_ENV_NO_RUNDIR` |
| `sshd -T` refuses the configuration | all | sshd config invalid | `SSHD_CONFIG_INVALID` |
| no `sshd` | all | sshd not installed | `COMMAND_NOT_FOUND` |
| no `/etc/ssh/sshd_config` | all | no sshd_config | `FILE_NOT_FOUND` |
| whether Proxmox VE runs not established, and the baselines give different states (§2.10) | Login, Port | can't tell if Proxmox | `PVE_UNKNOWN` |
| a value `sshd -T` printed in a form hardn does not know, or twice; the files and `sshd -T` disagreeing on a setting; on Proxmox VE a `pve.CheckSSH` fact not established (kept as a guard: the reading above it returns first, so no test reaches it) | all | can't read | none |
| a Match block's value hardn does not rank | Login, Auth | can't read | none (finding `SSH_MATCH_VALUE_UNKNOWN`) |
| any other read that fails (an `Include` other than the stock one, Alpine's `/etc/conf.d/sshd` naming another binary, root's groups where `RootByKey` decides, `ss` failing) | all | can't read | the error's (`SSHD_INCLUDE_UNSUPPORTED`, `RELOAD_OPENRC_CONFD`, `FILE_NOT_FOUND`, `COMMAND_FAILED`) |

**Wording to read again** (the review's list; every line is the maintainer's to decide). Changed here to lower case and to describe the host: "Match allows root" → "match block allows root"; "Match allows passwords" → "match block allows passwords"; "Match blocks root" → "match block denies root"; "root blocked for keys" → "root key login blocked"; "root key login unclear" (repeated the label's "login" and restated `Unknown`) → "can't tell if root key works"; on Proxmox VE "22, as Proxmox needs" and "Proxmox needs 22 only" (what Proxmox needs, not the host) → the port lines off Proxmox VE, the state carrying the judgement. Still open, with alternatives: "match block allows root" or "some clients allow root"; "match block allows passwords" or "some clients allow passwords"; "root key-only (Proxmox)", the one detail with a capital, or "root key-only" as off Proxmox VE (the state then tells them apart); "kbd-interactive on" (sshd's own abbreviation) or "keyboard auth on"; "22 and 2230" or "22 (2230 open)"; the new lines "22 still open", "config not in effect", "ss not installed", "2230 on another address", "other ports configured".

## 6. API; changes to other units' code; not modelled; for the maintainer

```go
package engine // added (checks.md)
var ErrNotMigrated error // CONTROL_NOT_MIGRATED
type CheckOnly struct{}  // embedded: Plan returns ErrNotMigrated
func (CheckOnly) Plan(context.Context, host.System, Input) ([]Change, error)

package ssh // pkg/engine/controls/ssh (new)
const (
	CodeMatchAllowsMore   = "SSH_MATCH_ALLOWS_MORE"
	CodeNotInEffect       = "SSH_PORT_NOT_IN_EFFECT"
	CodeMatchValueUnknown = "SSH_MATCH_VALUE_UNKNOWN"
	Code22Bound           = "SSH_PORT_22_BOUND"
)
type Login struct{ engine.CheckOnly } // "ssh.login", "SSH Login"
type Auth struct{ engine.CheckOnly }  // "ssh.auth", "SSH Auth"
type Port struct{ engine.CheckOnly }  // "ssh.port", "SSH Port"
func Controls() []engine.Control      // Login, Auth, Port

package reload // added
func Unbound(want []sshd.Listener, bound []Bound) []sshd.Listener // the listeners of want no bound socket covers

package steps // added: the planners' finding conversions, for the checks
func FindingCode(prefix, kind string) string
func SourceAt(file string, line int) string
func SSHDFinding(f sshd.Finding) engine.Finding
func PVEFinding(f pve.Finding) engine.Finding
```

- **Other units' code:** `pkg/host/reload` gains `Unbound`, its unexported `unbound` exported for the port's check, so the check uses `CheckInEffect`'s own rule ([reload.md](../safety/reload.md)'s API list predates it; that page is `next/p2-reload`'s); `pkg/engine/steps` exports its finding conversions (`FindingCode`, `SourceAt`, `SSHDFinding`, `PVEFinding`, appended to `findings.go`), so a check codes a primitive's finding as a plan does instead of copying them ([steps.md](steps.md) §2.23 is that unit's page; slice 6 makes this package call `steps.PlanSSH` anyway); `pkg/output` registers `CONTROL_NOT_MIGRATED` (after `PLAN_USED`) and the four finding codes (at the end), with the reference page's rows. `go.mod` and `go.sum` are unchanged.
- **Not modelled:** a daemon not reloaded since an edit, for SSH Login and SSH Auth, except where this run shows it (§2.8a): `sshd -T` reads the files, and nothing shows what a running sshd read (§3), so an edit that moves no port and is not reloaded, or a host without `ss`, shows the files' values; a stale non-default port, and whose a socket on 22 is (§2.12); `AuthenticationMethods`, so a host that asks for a key and then a password (`publickey,password`) shows "password auth on"; `DenyUsers`, `AllowUsers` and `AllowGroups` for root off Proxmox VE, so `PermitRootLogin yes` with `DenyUsers root` shows "root allowed"; host-based and GSSAPI authentication; a Match block's criteria (§2.5); options on sshd's command line ([sshd-effective.md](../safety/sshd-effective.md) §3).
- **For the maintainer:** the wording of §5, and its list of lines to read again; the states chosen where the brief does not say: 22 beside another port `Partial` (§2.13); keyboard-interactive on `NotConfigured` with PAM or without (§2.11); public keys off with passwords off `Partial`; on Proxmox VE `PermitRootLogin no` `Partial`, and SSH Login `Configured` whatever the port, which SSH Port's row carries (§2.10, against [pve.md](../safety/pve.md) §6's one status); SSH Login and SSH Auth `Unknown` when this run shows the configuration not in effect, on `ssh.service` and OpenRC only (§2.8a); "22 still open" `Unknown` rather than `Partial` (§2.12); without `ss` off a socket host, SSH Port `Configured` from configuration alone, or `Unknown` there too, which on a stock Alpine would make it `Unknown` always (§2.12); whether a host with no SSH server should become `NotApplicable` once detect can establish that (§2.8); a `Keyword` field on `sshd.Finding`, a Phase 2 change, so that §2.4's attribution need not read the details' words.
- **Inputs to later units:** `next/p4-risk` weighs these rows (the port less than the auth settings, brief Phase 4) and owns P-05's cross-check (a default-deny firewall that does not allow an SSH listener), as the firewall check's "whether the SSH listener set is allowed" does. **That cross-check must be made against what `ss` shows bound** (`reload.ReadBound`, with `reload.Unbound` for coverage), **not against `Listeners.All`**, which is configuration: in the state `SSH_PORT_NOT_IN_EFFECT` reports (configured 2230, bound 22), a check over `Listeners.All` passes with ufw allowing 2230 and denying 22, which is a lockout. `SSH_PORT_NOT_IN_EFFECT`, and `SSH_PORT_22_BOUND` where the firewall denies 22, are themselves lockout signals when the firewall denies a bound port. Without `ss`, the configured set is all there is, and the check should say so as SSH Port's evidence does. This package exports no listener reading of its own: the Phase 2 readers (`sshd.ReadEffective`, `reload.ReadBound`, `reload.Unbound`) are the shared reading, and the firewall check, which may be built in the parallel stack, says what it needs from them. Slice 6 replaces `Plan` (`steps.PlanSSH`) and may give `Check` the configuration to compare with.
- **Size:** about 820 lines of non-test Go (about 165 of them whole-line comments; the package 765, `engine.CheckOnly` 20, `reload.Unbound` 7, `steps`' exports 19, the registry 7), about 815 of tests, 75 of fixtures and 250 of docs, against the plan's guideline of about 500: three controls over five readers on four platforms, a test for each status line of §5, and the pattern's pieces (`engine.CheckOnly`, the lint rule, the codes) that the other four areas reuse. The commits split it as a review would: the pattern, then the SSH controls, then the review's fixes (§7).

## 7. Review fixes

What the review of 2026-10-20 found, and what changed (each in its section above):

- **A socket still on 22 read as `Configured`** (§2.12): only the configured listeners were checked against `ss`, so `Port 22` dropped and sshd not reloaded showed "non-default (2230)", with 22 left out of the evidence. Now a socket on 22 that no configured listener is on is `Unknown`, "22 still open", `SSH_PORT_22_BOUND`, and the evidence lists the sockets on 22.
- **SSH Login and SSH Auth `Configured` while SSH Port showed the configuration not in effect** (§2.8a): now `Unknown`, "config not in effect", on `ssh.service` and OpenRC.
- **No `ss` on a socket-activated host** (§2.12): now `Unknown` with the error, as `reload.CheckInEffect` fails there; elsewhere the configured set stands, marked in §5 for the maintainer. The Alpine fact is marked unverified in the code too.
- **`/run/sshd` details from the SSH mode alone** (§2.3): now from sshd's "missing" and detect's unit states, else "can't read".
- **Proxmox VE's SSH Login against pve.md §6** (§2.10): the split is recorded as a decision, for the maintainer.
- **An established state hidden behind Proxmox VE or root's groups not established** (§2.10): the reading comes first; a state both baselines give stands; root's groups are read where they decide.
- **Findings matched by lower-case substrings** (§2.4): whole words in any case, the listeners' words SSH Port's, and a disagreement's reason matched to its keyword (§2.12's "files and sshd differ", "socket and sshd differ").
- **A Match value hardn does not rank** (§2.5): now a finding and evidence, `SSH_MATCH_VALUE_UNKNOWN`, not a bare "can't read".
- **The hand-off to `next/p4-risk`** (§6): the cross-check is against bound sockets, not `Listeners.All`.
- **Wording** (§5): lower case, describing the host; the open lines listed with alternatives.
- **Keyboard-interactive without PAM** (§2.11, §3): the page now says what the code does, a decision for the maintainer.
- **Copied helpers** (§2.6, §6): the finding conversions are `steps`'; the listener reading is not exported (§6 says why).
- **Text** (§2.7): Match lines trimmed, listeners joined as in the evidence, `ssh.socket` named only where it listens; SSH Login's evidence on Proxmox VE lists what `pve.CheckSSH` read.
- **Tests** for every line of §5 (Proxmox VE `forced-commands-only`, an inactive `ssh.service`, root's groups, an `Include`, `/etc/conf.d/sshd`, a Match value, `ss`'s unread lines), a failed read checked to be `Unknown` rather than made so, and `engine.CheckOnly` tested in `pkg/engine` (`TestCheckOnly`). The first commit's links to this page, which arrived in the next commit, are history and are not rewritten.
