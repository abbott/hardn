# Firewall: the firewall status row (`pkg/engine/controls/firewall`)

> **Unit:** refactor plan Phase 4, `next/p4-check-firewall` (plan §2, row `p4-check-<area>`, firewall: "`ufw status verbose` (policy + whether the SSH listener set is allowed); `pve-firewall` on Proxmox"; brief Phase 4, "Where each status comes from" and "Cross-checks that should produce findings"; brief 2.5, Proxmox VE's firewall; brief §3 principles 1 and 3; decision §3.17, approved: no UFW `disable`/`reset`; decision §3.18, approved: SSH stays on 22 on Proxmox VE; [P-05](../refactor/proposals.md#p-05), whose firewall cross-check this builds; [P-06](../refactor/proposals.md#p-06), the firewall counted once). Follows the pattern of [checks.md](checks.md), set by `next/p4-check-ssh`. It takes up the "Status sources" item of [docs/safety/README.md](../safety/README.md) for the firewall, [ufw.md](../safety/ufw.md) §6's and [pve.md](../safety/pve.md) §6's `p4-check-firewall` items, and [check-ssh.md](check-ssh.md) §6's hand-off: the cross-check against the sockets `ss` shows bound. V-numbers: V9 and V12's status half (the configured port taken for the listeners) and V10 (ufw on Proxmox VE): this row reads `pve-firewall` there and never ufw.
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only, with ufw answered by a model of ufw 0.36.2's status and saved files, `sshd -T`, `systemctl show -p Listen ssh.socket` and `ss -Htln` by the ports a case gives, and `pve-firewall`'s files written as its parser reads them (§3). Nothing has run on Linux: everything host-specific is "written, not run (gate 5)" (§4). Every detail below is **proposed wording**, for the maintainer's review (§5).

Figma's main menu has one firewall row: Firewall "deny policy". It is the control `firewall.incoming`, whose `Check` reads ufw's state and whether ufw lets every SSH listener through, or on Proxmox VE `pve-firewall`'s state, ufw not consulted. It no longer only checks: since slice 4's unit (b) its `Plan` makes the changes the Firewall screen asks for, by `Action`; the status list's, `ActionNone`, is still `engine.ErrNotMigrated` (§2.14).

## 1. Why

origin/main's firewall row counts the firewall twice (enabled, and configured) and reads neither whether ufw refuses new connections nor whether SSH gets through ([P-06](../refactor/proposals.md#p-06)); its firewall step allows the configured port, never the ones sshd listens on (V9), and enables ufw on Proxmox VE (V10). A row that shows "deny policy" while the firewall refuses the port sshd accepts on tells the user the host is hardened when the next SSH session cannot log in. The brief makes that a critical finding: "a default-deny firewall and an SSH port that isn't allowed" (brief Phase 4). A status that says a host is safe when it is not is the worst error of this phase: Unknown rather than a guess.

## 2. Guarantees

### Reading

1. **Which firewall:** whether the host runs Proxmox VE must be established first (`pve.Applies`): not established, the row is `Unknown`, "can't tell if Proxmox", with `pve.ErrProxmoxUnknown` (`PVE_UNKNOWN`), and nothing is read. On Proxmox VE, `pve.ReadFirewall` (§2.10); elsewhere `ufw.Read` (§2.5). ufw is never run or read on Proxmox VE (brief 2.5; tested: no `ufw` command and no read under `/etc/ufw` there).
2. **Read-only.** The check runs exactly `ufw status verbose`, `sshd -T -f /etc/ssh/sshd_config` (with `-C` per Match block, `sshd.ReadEffective`), `systemctl show -p Listen ssh.socket` on a socket-activated host, `ss -Htln`, and on Proxmox VE `systemctl is-active` of `pve-firewall.service` or `proxmox-firewall.service`; on Alpine, where `sshd.krb5` or `sshd.pam` is installed, its `-G` (`reload.OpenRCFlavour`). It reads ufw's settings and saved rules, sshd's files and `pve-firewall`'s two files. Tested: every call of every test (`readOnly`).
3. **The SSH listeners as SSH Port reads them, checked against what is bound** ([check-ssh.md](check-ssh.md) §2.12, §6): the configured set (`sshd -T`'s `listenaddress` lines ∪ `ssh.socket`'s `Listen` entries on a socket-activated host, `sshd.ReadEffective`), and the sockets `ss -Htln` shows bound (`reload.ReadBound`), compared by `reload.Unbound`, the rule `reload.CheckInEffect` uses. They are **established** when the configured set is (`Listeners.Known`, not empty) and every listener of it is bound. Then the firewall must let the configured set through, and a socket bound on 22 that no configured listener is on (sshd not reloaded since 22 was dropped, or another program: `ss` does not say whose) too (§2.7). **Not established**, the firewall is checked against every candidate: every listener the sources read, and every socket bound on their ports and on 22, any of which may be the one sshd accepts on (an edit to 2230 not reloaded leaves sshd on 22, with 2230 configured and not bound). Only those ports are looked at: another socket is not taken for sshd's (a web server's 80), so a stale port other than 22 (sshd still on 2230 after an edit back to 22) is not seen, and the row is then `Unknown`, "ssh config not in effect" (tested on Proxmox VE). **Without `ss` the listeners are not established, on every host** ("ss not installed", `COMMAND_NOT_FOUND`; the evidence says that the bound sockets were not read). Here this row and SSH Port differ: SSH Port lets the configured set stand off a socket-activated host ([check-ssh.md](check-ssh.md) §2.12), but this row's `Configured` says that SSH gets through, and from configuration alone it would say so for the brief's own lockout: an edit to 2230 not reloaded, sshd still on 22, ufw allowing 2230 only (tested; decision of this unit, after review). On a socket-activated host, as before, `sshd -T` and `ssh.socket` agree after a `daemon-reload` whatever is bound. A stock Alpine is said not to ship `ss` (**[unverified]**, §3): there a deny policy is `Unknown` until `iproute2` is installed.
4. **A read that fails is the check's error** ([checks.md](checks.md) §1.5), reported `Unknown` with it, **where the state depends on it**: ufw's status or settings, `pve-firewall`'s files, and, under a policy that refuses what no rule accepts, the SSH listeners. A state established without the listeners is reported: ufw inactive with `ENABLED=no`, and `pve-firewall` disabled or `ACCEPT`, read none; an allow policy, "off after restart" and "enabled, not running" read them for their findings, and where they are not established that is a finding beside the state, `FIREWALL_SSH_UNCHECKED` (its detail the reason, as §5 words it; the error of a read that failed in the evidence), not the check's error, which would make the engine report the row `Unknown` (`Engine.Check`). The other way, a listener the firewall refuses is `Partial`, "ssh blocked", whatever else is not established (§2.6): a state established is reported ([checks.md](checks.md) §1.5).

### ufw (Debian, Ubuntu, Alpine)

5. **ufw's state** (`ufw.Read`, [ufw.md](../safety/ufw.md) §2.2 to §2.4): not installed is `NotConfigured`, "ufw not installed" ([ufw.md](../safety/ufw.md) §6: not installed is not configured, not unknown); a status not read whole (a line ufw does not print, `Default:` missing) is `Unknown`, "can't read", with `UFW_UNKNOWN`, and so is an incoming policy ufw does not set (`Default: disabled (incoming)`: ufw prints `disabled` for routed alone, [ufw.md](../safety/ufw.md) §3); `Status: inactive` with `ENABLED=no` is `NotConfigured`, "inactive", and nothing else is read. **`ENABLED=yes` and inactive** (a failed start; on Alpine a reboot with ufw in no runlevel, [ufw.md](../safety/ufw.md) §6) is `NotConfigured`, "enabled, not running": nothing filters now; each SSH listener its saved rules would refuse once ufw starts is `UFW_SSH_NOT_ALLOWED_AT_START` (`ufw.Check`), and saved rules or a default policy not read are `FIREWALL_SSH_UNCHECKED`, "saved rules unreadable" (the lines in the evidence) or "default policy unreadable".
6. **Active: the cross-check** (`ufw.Check(state, ufw.Needs(listeners, nil, nil))`, [ufw.md](../safety/ufw.md) §2.6, §2.16): each listener's verdict under `ufw status verbose` (the saved rules and policies, the chains loaded), in each IP version it accepts (a socket's `[::]` dual-stack). A listener ufw refuses (`Blocked` by the default policy, or `Denied` by a rule; IPv6 with `IPV6=no`) is `UFW_SSH_NOT_ALLOWED`. **One ufw lets through from loopback only** is refused too: its verdict is `Restricted` (`UFW_SSH_RESTRICTED`, ufw's own), and with the allow and limit rules that admit loopback alone left out (`on lo`, from or to an address in `127.0.0.0/8` or `::1`), `ufw.Evaluate` blocks or denies it, so no remote session reaches it (tested: `on lo`, to `127.0.0.1` and `::1`, from `127.0.0.1`). The row gets one **`FIREWALL_SSH_BLOCKED`** listing every refused listener: **the brief's critical finding, which `next/p4-risk` scores critical** (§6). The row is then `Partial`, "ssh blocked": ufw is active and refusing what no rule accepts, which the baseline wants, and refuses SSH, which it must not; the lockout is the finding's to weigh, not the state's. `FIREWALL_SSH_BLOCKED` is made **whatever the default policy** (decision of this unit): the brief names a default deny, and a deny rule for SSH under an allow policy refuses the same new sessions.
7. **Active, nothing refused:**
   - default **allow** incoming is `Partial`, "allow policy": ufw refuses only what its rules deny;
   - **a rule that opens every port** (`allow` or `limit`, in, TCP or any protocol, from anywhere, to any address, on any interface, its ports every one from 1 to 65535: `ufw allow from any`, `ufw allow 1:65535/tcp`) is `Partial`, "all ports allowed": the default policy then refuses no TCP connection. It counts wherever it is in the list, so one after a rule that denies everything counts too (the pessimistic error);
   - **`ENABLED=no`** while active (`ufw --force enable` failing after its start, which writes `ENABLED=no` back without stopping the chains, [ufw.md](../safety/ufw.md) §2.11) is `Partial`, "off after restart": ufw does not start at the next boot;
   - **ufw's service not starting at boot** (`ufw.ReadBoot` No, slice 4 unit (b), [gallery-firewall.md](gallery-firewall.md) §7's third finding) is the same line, in the same place: on OpenRC in no runlevel, on systemd `ufw.service` not enabled, with evidence naming the runlevel or the unit ("OpenRC: ufw in no runlevel", "systemctl is-enabled ufw.service: not enabled"). Not read (`Unknown`), the row is `Unknown`, "can't tell boot state", in place of `Configured`, with `FIREWALL_BOOT_UNCHECKED`, and no other line changes: `Configured` says it starts at the next boot. ufw's Python has no `systemctl` call and `ufw.service` runs `ufw-init` with `WantedBy=multi-user.target`, so nothing else starts it at boot: read, not run ([ufw.md](../safety/ufw.md) §3);
   - default **deny** or **reject** with the listeners not established (§2.3) is `Unknown`, with the reason (§5); with a verdict ufw's rules do not establish (a port list hardn cannot read) `Unknown`, "can't read", with `UFW_UNKNOWN`; with a socket bound on 22 that no configured listener is on, not let through from everywhere (refused, or let through from some sources only), `Unknown`, "can't tell if 22 is ssh", with ufw's finding and `SSH_PORT_22_BOUND` (ufw refusing a socket that may be another program's, or a stale sshd's, while sshd accepts on the configured ports, is not shown as a lockout, nor as safe);
   - **a listener let through from some sources only** (`UFW_SSH_RESTRICTED`: an allowlist, or a rule for some of the host's addresses or interfaces) is `Partial`, "ssh allowlist"; **from everywhere but some** (`UFW_SSH_ALLOWED_EXCEPT`, a blocklist) `Partial`, "ssh blocklist". Whether the user's own address is among those let through is not established ([ufw.md](../safety/ufw.md) §2.6; `ufw.Verdict.OK` is false for both), so `Configured`, which says that SSH gets through, would be a guess, and a restriction that admits no remote source would hide the lockout behind the best state (review of this unit). `Unknown` would rank a deliberate allowlist with a read that failed; `Partial` says the host differs from hardn's baseline, whose ufw step lets SSH through from anywhere (`ufw.PlanFor`'s `ufw allow 2230/tcp`; decision of this unit, for the maintainer, §6);
   - otherwise, with the default incoming policy **deny** or **reject** and every listener let through from everywhere (rate-limited by `limit` included), **`Configured`**, "deny policy" or "reject policy": those two policies alone.
8. **Evidence**: `ufw status verbose`'s status line and, active, its `Default:` line as ufw prints it; `ENABLED`; the listeners as SSH Port's evidence lists them (`sshd -T: listenaddress …`, `systemctl show -p Listen ssh.socket: …`, `ss -Htln: …` on the SSH ports and 22, or that `ss` is not installed); active, each listener's verdict and the rule or policy that decided it (`ufw: listener 0.0.0.0:2230 (IPv4): allowed by 2230/tcp ALLOW IN Anywhere`), and a rule that opens every port (`ufw: every port allowed by Anywhere ALLOW IN Anywhere`); beside a state that does not depend on the listeners, the error of their read that failed (`sshd -T: sshd's listeners: …`). Tested: `TestEvidence`.
9. **Findings**: ufw's own, coded by `steps.UFWFinding` as a plan codes them (`UFW_SSH_NOT_ALLOWED`, `UFW_SSH_RESTRICTED`, `UFW_SSH_ALLOWED_EXCEPT`, `UFW_UNKNOWN`, `UFW_SSH_NOT_ALLOWED_AT_START`); SSH Port's `SSH_PORT_NOT_IN_EFFECT` for each configured listener not bound, and `SSH_PORT_22_BOUND` (§2.7), each in SSH Port's words, so that a code has one detail in both rows (tested); this package's `FIREWALL_SSH_BLOCKED`, `FIREWALL_SSH_UNCHECKED`, `FIREWALL_BOOT_UNCHECKED` and, on Proxmox VE, `FIREWALL_PVE_LOCAL_ONLY`. Every code is registered; tested: each test checks that every finding's code is a registered finding code.

### Proxmox VE (`pve-firewall`)

10. **pve-firewall's state** (`pve.ReadFirewall`, [pve.md](../safety/pve.md) §2.14 to §2.16), in this order:
    - pmxcfs not mounted: `Unknown`, "/etc/pve not mounted", with `pve.ErrClusterFSDown` (`PVE_CLUSTER_FS_DOWN`); whether the firewall is on, the service's state, `nftables` or `policy_in` not established (a file not read, an option in a form `pve-firewall` skips, a time-stamped `enable`, `systemctl` failing): `Unknown`, "can't read", with `ReadFirewall`'s `Err` (`PVE_FIREWALL_UNKNOWN`; tested each);
    - the service not active: `Unknown`, "pve-firewall stopped" ("proxmox-firewall stopped" with `nftables: 1`): what it loaded before is not read, whether the firewall is enabled or not, as `ReadFirewall` decides;
    - disabled (`cluster.fw`'s `enable` 0, its default, or `host.fw`'s): `NotConfigured`, "disabled";
    - `nftables: 1`: `Unknown`, "uses nftables", with `PVE_FIREWALL_NOT_MODELLED` (proxmox-firewall's rules are not read);
    - `policy_in` `ACCEPT`: `Partial`, "accept policy";
    - `DROP` or `REJECT`: 22 and 8006 must be accepted from the management IP set (`ReachManagement`, `pve-firewall`'s own rule): a rule that may drop either first, or a non-empty blacklist, is `Unknown`, "ssh may be blocked" or "web ui may be blocked", with `PVE_RULE_MAY_BLOCK` or `PVE_BLACKLIST`.
11. **Then the SSH listeners** (§2.3) **and the management IP set**: every listener on 22, the one SSH port `pve-firewall`'s management rule accepts (§3.18), with the IP set `management` written, is `Configured`, "drop policy" or "reject policy". A listener on another port is `Unknown`, "2230 may be blocked" ("ssh ports may be blocked" where the list leaves no room, 29 cells beside `Unknown`), with `PVE_PORT_NOT_22`, whose source is `sshd -T` for a configured port and `ss -Htln` for a bound one: `pve-firewall`'s rules for that port, which may accept it, are not read, so it is neither shown blocked nor allowed. The listeners not established: `Unknown` with the reason, as off Proxmox VE. **No management IP set written** (`[IPSET management]` missing or empty) is `Unknown`, "no management ipset", with `FIREWALL_PVE_LOCAL_ONLY`: `pve-firewall`'s management rule then accepts 22 and 8006 from the cluster's local network alone, which it adds to the set itself ([pve.md](../safety/pve.md) §3) and hardn does not read (`pve-firewall localnet`, not run), so an administrator outside that network may be locked out (an accept rule, not read, may let more sources in), and whether one is, is not established (review of this unit). With the set written, `ReachManagement` ("allowed from the management network", not "allowed", [pve.md](../safety/pve.md) §6) is `Configured`, where a ufw allowlist is `Partial` (§2.7): on Proxmox VE the management rule is `pve-firewall`'s own way of letting SSH in under DROP or REJECT (accept rules, which may let more sources in, are not read, [pve.md](../safety/pve.md) §2.16), and the rule decision §3.18 keeps SSH on 22 for, while hardn's ufw step lets SSH through from anywhere (decision of this unit, for the maintainer). **`FIREWALL_SSH_BLOCKED` is not made on Proxmox VE**: `ReadFirewall` never establishes a port refused (its `Reach` has no such value), only one that may be.
12. **Evidence**: `pve-firewall`'s options (`enabled`, `policy_in`, `nftables`), `systemctl is-active`'s answer, how far 22 and 8006 are let through (`Reach`'s code), the management IP set as written ("not written" for none), and the listeners as in §2.8.

### The control

13. **`Applies` is true everywhere**: detect's facts do not establish a host without a packet filter, and `NotApplicable` would leave the row out of the risk level ([checks.md](checks.md) §1.6).
14. **The status list plans nothing.** `Controls()` is `Incoming{}`, `ActionNone`, whose `Plan` returns `engine.ErrNotMigrated`, and `Engine.Plan` over it is refused, the item's error coded `CONTROL_NOT_MIGRATED`, with no change and no call but the run directory's (`TestActionNone`). The package no longer only checks: a screen plans `Incoming` with the `Action` it was asked for (slice 4, unit (b); [gallery-firewall.md](gallery-firewall.md) §5 to §7). `Check` is the same for every `Action`.
15. **Through `host.System` only:** depguard rule `controls-through-host` ([checks.md](checks.md) §1.11) covers this package; checked to report `os` and `pkg/ui` imports in a throwaway file of the package (in a temporary copy of the tree; nothing committed).

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| `ufw status verbose` says active when ufw's user chains exist and then prints `Default: <in> (incoming), …` and the saved rules; inactive, the status line alone | [ufw.md](../safety/ufw.md) §3 (`backend_iptables.py` `get_status`) | §2.5, §2.6, §2.8 |
| Inactive ufw accepts everything (its stop sets every policy to ACCEPT); `ENABLED=yes` starts it at boot, `ENABLED=no` does not; a failed enable writes `ENABLED=no` back without stopping what the start loaded | [ufw.md](../safety/ufw.md) §3 (`ufw-init-functions`, `frontend.py` `set_enabled`) | §2.5, §2.7 |
| The first matching rule wins, then the default policy; loopback is accepted before any user rule; with `IPV6` not `yes`, a started ufw drops all IPv6 | [ufw.md](../safety/ufw.md) §2.6, §3 (`doc/ufw.8`, `before.rules`, `ufw_start`) | §2.6 (`ufw.Check`'s verdicts) |
| A systemd `[::]` socket is dual-stack; sshd's own `[::]` is IPv6 only | [reload.md](../safety/reload.md) §3; [ufw.md](../safety/ufw.md) §3 | §2.6 (the IP versions a listener needs) |
| `sshd -T` and `systemctl show -p Listen` are configuration, not bound sockets; between an edit and its reload they differ from what accepts connections | [sshd-effective.md](../safety/sshd-effective.md) §2.5 | §2.3 |
| `ss` without `-p` does not say which process holds a socket | [reload.md](../safety/reload.md) §2.10a | §2.3, §2.7 ("can't tell if 22 is ssh") |
| `ss` is installed on a stock Debian, Ubuntu and Proxmox VE, and not on a stock Alpine | [reload.md](../safety/reload.md) §3 **[unverified]** | §2.3 |
| ufw is packaged in Alpine 3.24's community repository; Debian and Alpine install it only on request; Ubuntu ships it installed and inactive | [ufw.md](../safety/ufw.md) §3 (Alpine's APKBUILD); the rest **[unverified]** | §4; the stock lines of §5 |
| pve-firewall's input chain: the blacklist's drop, the host's rules, the cluster's, then the management IP set's accepts for 22 and 8006 (among others), then `policy_in` (DROP when unset); nothing when the cluster's `enable` is 0 (its default); the service rereads the files every 10 s | [pve.md](../safety/pve.md) §3 (`Firewall.pm`, read 2026-10-18) | §2.10, §2.11 |
| `pve-firewall.service` is active on a stock node, firewall disabled | [pve.md](../safety/pve.md) §4 **[unverified]** | §2.10: if it is not, the stock row is "service stopped" |
| On Proxmox VE SSH stays on 22 (cluster SSH and pve-firewall's management rule) | decision §3.18; [pve.md](../safety/pve.md) §2.4 | §2.11 |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root. On the VMs of [vmtest.md](../safety/vmtest.md) (Debian 13, Ubuntu 26.04, Alpine 3.24) and by hand on Proxmox VE 9, through a dev-only driver or the gallery's status scenario once one calls `Engine.Check` with this control:

- each stock host: Debian 13 and Alpine 3.24 "ufw not installed" (whether a stock Debian 13 has ufw, §3); Ubuntu 26.04 "inactive"; Proxmox VE 9 "disabled" (whether `pve-firewall.service` is active there, §3);
- `ufw allow 22/tcp` then `ufw --force enable` on each: "deny policy"; `ufw default reject incoming`: "reject policy"; `ufw default allow incoming`: "allow policy";
- the brief's 2230 change through the engine: in the confirm window "deny policy" (22 and 2230 allowed); a hand-made `ufw delete allow 2230/tcp` while sshd is on 2230: "ssh blocked" with `FIREWALL_SSH_BLOCKED`, and a new SSH session refused (from another host, the console open);
- an edit of `Port 2230` in a drop-in without a reload, with `ufw allow 2230/tcp` and no rule for 22: "ssh blocked" (sshd still on 22); on Ubuntu, the same between `daemon-reload` and the restart of `ssh.socket`;
- `ufw allow from 192.0.2.0/24 to any port 22 proto tcp` alone: "ssh allowlist" with `UFW_SSH_RESTRICTED` (and, with IPv6 on, "ssh blocked" for `[::]:22`); `IPV6=no` with sshd on `[::]:22`: "ssh blocked"; `ufw allow in on lo to any port 22 proto tcp` alone: "ssh blocked", and a new session from another host refused; `ufw allow 1:65535/tcp`: "all ports allowed";
- Alpine 3.24 with ufw from the community repository, enabled, then rebooted without `rc-update add ufw default`: "enabled, not running" (whether ufw comes back is [ufw.md](../safety/ufw.md) §4's question); without `iproute2` "ss not installed", with it "deny policy" (whether a stock Alpine has `ss`, §3);
- ufw enabled and its start made to fail (as [ufw.md](../safety/ufw.md) §4): "off after restart";
- Proxmox VE 9: the datacenter firewall on with an IP set `management`: "drop policy"; on without one: "no management ipset", and a connection from outside the local network refused; `policy_in: ACCEPT`: "accept policy"; a host rule `IN DROP -p tcp -dport 22`: "ssh may be blocked"; `systemctl stop pve-firewall`: "service stopped"; `systemctl stop pve-cluster`: "/etc/pve not mounted"; `nftables: 1`: "uses nftables"; each compared with `iptables-save` and a connection from inside and outside the management network.

## 5. The status lines

Every line the row can show, in Figma's grammar (marker, label, the state's word, the detail). **Every detail is proposed wording**; Figma's "deny policy" is the anchor. A port list is the ports in ascending order, joined by ", ".

**ufw (Debian, Ubuntu, Alpine)**

| The host (`ufw status verbose`, its settings; the SSH listeners, §2.3) | State | Detail |
|---|---|---|
| active, default deny incoming, every SSH listener let through from everywhere (rate-limited included), the bound sockets read (§2.3) | Configured | deny policy |
| the same, default reject incoming | Configured | reject policy |
| active, an SSH listener refused, by the default policy or a rule, whatever the policy, or let through from loopback only (`FIREWALL_SSH_BLOCKED`) | Partial | ssh blocked |
| active, default allow incoming, no SSH listener refused | Partial | allow policy |
| active, default deny or reject, a rule that opens every port, no SSH listener refused | Partial | all ports allowed |
| active, default deny or reject, nothing refused, `ENABLED=no`, or ufw's service in no runlevel or not enabled (`ufw.ReadBoot` No) | Partial | off after restart |
| active, default deny or reject, an SSH listener let through from some sources only (`UFW_SSH_RESTRICTED`) | Partial | ssh allowlist |
| the same, from everywhere but some (`UFW_SSH_ALLOWED_EXCEPT`) | Partial | ssh blocklist |
| inactive, `ENABLED=no` (Ubuntu's stock state) | Not Configured | inactive |
| inactive, `ENABLED=yes` | Not Configured | enabled, not running |
| ufw not installed (a stock Alpine; other packet filters are not read) | Not Configured | ufw not installed |

**Proxmox VE (`pve-firewall`; ufw not read)**

| The host (`cluster.fw`, `host.fw`, the service; the SSH listeners) | State | Detail |
|---|---|---|
| enabled, service active, `policy_in` DROP, 22 and 8006 accepted from the management IP set, written, every SSH listener on 22 | Configured | drop policy |
| the same, REJECT | Configured | reject policy |
| enabled, service active, `policy_in` ACCEPT | Partial | accept policy |
| disabled (the stock datacenter `enable: 0`), service active | Not Configured | disabled |

**Unknown**

| The host | Detail | Error code |
|---|---|---|
| whether the host runs Proxmox VE not established | can't tell if Proxmox | `PVE_UNKNOWN` |
| `ufw status verbose` failing (not root), a settings or rules file not read | can't read | the error's (`COMMAND_FAILED`, `FILE_NOT_FOUND`) |
| ufw's status not read whole, an incoming policy ufw does not set (`disabled`), or a rule whose port list hardn cannot read | can't read | none (finding `UFW_UNKNOWN`) |
| default deny or reject, and: no `sshd` | sshd not installed | `COMMAND_NOT_FOUND` |
| … no `/etc/ssh/sshd_config` | no sshd_config | `FILE_NOT_FOUND` |
| … `sshd -T` refusing the configuration | sshd config invalid | `SSHD_CONFIG_INVALID` |
| … `sshd -T` failing otherwise (`/run/sshd` missing, an `Include` other than the stock one), or on Alpine `/etc/conf.d/sshd` naming the binary | can't tell ssh ports | the error's (`SSHD_ENV_NO_RUNDIR`, `RELOAD_OPENRC_CONFD`, …) |
| … the configured listeners not established (the socket and sshd differ, the unit that listens not up), nothing refused | can't tell ssh ports | none |
| … `ss` failing | can't tell ssh ports | `COMMAND_FAILED` |
| … `ss` not installed, on any host (a stock Alpine, §2.3) | ss not installed | `COMMAND_NOT_FOUND` |
| … a configured listener not bound, every candidate let through (§2.3) | ssh config not in effect | none (finding `SSH_PORT_NOT_IN_EFFECT`) |
| … a socket on 22 no configured listener is on, not let through from everywhere (§2.7) | can't tell if 22 is ssh | none (ufw's finding, `SSH_PORT_22_BOUND`) |
| … nothing else open, and whether ufw starts at boot not read (`ufw.ReadBoot` `Unknown`) | can't tell boot state | none (finding `FIREWALL_BOOT_UNCHECKED`) |
| Proxmox VE: pmxcfs not mounted | /etc/pve not mounted | `PVE_CLUSTER_FS_DOWN` |
| Proxmox VE: an option (`policy_in` included) in a form `pve-firewall` skips, a file not read, `systemctl` failing | can't read | `PVE_FIREWALL_UNKNOWN` |
| Proxmox VE: the service not active | pve-firewall stopped (proxmox-firewall stopped with `nftables: 1`) | none |
| Proxmox VE: `nftables: 1` | uses nftables | none (finding `PVE_FIREWALL_NOT_MODELLED`) |
| Proxmox VE: DROP or REJECT, a rule that may drop 22, or a blacklist | ssh may be blocked | none (findings `PVE_RULE_MAY_BLOCK`, `PVE_BLACKLIST`) |
| Proxmox VE: DROP or REJECT, a rule that may drop 8006 | web ui may be blocked | none (finding `PVE_RULE_MAY_BLOCK`) |
| Proxmox VE: DROP or REJECT, an SSH listener on another port | 2230 may be blocked; ssh ports may be blocked when the list leaves no room | none (finding `PVE_PORT_NOT_22`) |
| Proxmox VE: DROP or REJECT, the listeners not established | as ufw's above | as above |
| Proxmox VE: DROP or REJECT, every SSH listener on 22, no management IP set written | no management ipset | none (finding `FIREWALL_PVE_LOCAL_ONLY`) |

**Wording to read again** (every line is the maintainer's to decide). The policy in each tool's own word ("deny policy", "reject policy", "allow policy" for ufw; "drop policy", "reject policy", "accept policy" for `pve-firewall`, its `policy_in` values), or Figma's "deny policy" and "allow policy" for both; "ssh blocked" or "blocks ssh"; "inactive" (ufw's own word) and "disabled" (`pve-firewall`'s), or "off" for both; "enabled, not running" or "stopped (enabled)", and "ufw not installed" or "no ufw" (both repeat "not" beside Not Configured); "off after restart" (P-43's AppArmor line); "can't tell ssh ports" or "ssh ports unclear"; "ssh allowlist" or "ssh from some sources", "ssh blocklist" or "ssh blocked for some"; "all ports allowed" or "every port open"; "no management ipset" or "ssh from local network"; "pve-firewall stopped" (was "service stopped", which did not say which service); "ssh ports may be blocked" (where "2230, 2231, 2232, 2233 may be blocked" passes 29 cells); "ssh config not in effect", "can't tell if 22 is ssh", "uses nftables", "web ui may be blocked", "/etc/pve not mounted" (new); "sshd config invalid", "no sshd_config" and "sshd not installed" are SSH Login's, where the error shows them (checks.md §1.5). Lower case throughout, "ssh" included, as Figma's details are.

## 6. API; changes to other units' code; not modelled; for the maintainer

```go
package firewall // pkg/engine/controls/firewall (new)
const (
	CodeSSHBlocked   = "FIREWALL_SSH_BLOCKED"   // the critical cross-check
	CodeSSHUnchecked = "FIREWALL_SSH_UNCHECKED" // the listeners not established, beside a state that does not depend on them
	CodeLocalOnly    = "FIREWALL_PVE_LOCAL_ONLY" // Proxmox VE: no management IP set written
)
const ID, ProfileSetting = "firewall.incoming", "ufwAppProfiles"
var ErrNoSS, ErrSSFailed, ErrNotInEffect, ErrRefused, ErrBootUnknown error // Enable's refusals

type Action uint8 // ActionNone (zero), ActionInstall, ActionEnable, ActionDisable, ActionSaveProfiles, ActionApplyProfiles
type Incoming struct{ Action Action } // "Firewall"; ActionNone's Plan is engine.ErrNotMigrated
func Controls() []engine.Control      // Incoming{}: the status list's, which plans nothing

// One reading of the SSH listeners, the row's, Enable's and the screen's.
type Sockets struct { Conf sshd.Listeners; Read bool; Bound []reload.Bound; Unbound []sshd.Listener; NoSS, SSFailed bool; Why string /* and what the row's evidence needs */ }
func ReadSockets(ctx context.Context, sys host.System, f detect.Facts) Sockets
func (s Sockets) Established() bool
func (s Sockets) Ports() []uint16
func (s Sockets) Listeners() (sshd.Listeners, bool)
func (s Sockets) Stray(port uint16) bool
func (s Sockets) NoneBound() bool

// What the screen draws, as facts. It writes nothing and runs only reads.
type Item uint8 // ItemNone (zero), ItemInstall, ItemEnable, ItemDisable
type Does struct{ Allows, SetsDeny, StartsUFW, Boot bool }
type Reading struct {
	OnPVE bool; Proxmox error; Profiles []config.UfwAppProfile
	PVE pve.Firewall; PVEErr error; PVEUFWEnabled detect.Tri; PVEUFWErr error
	State ufw.State; StateErr error; Boot detect.Tri; Sockets Sockets
	First Item; DisableBeside bool
	Plan []engine.Change; PlanErr error; Does Does
}
func Read(ctx context.Context, sys host.System, f detect.Facts, cfg *config.Config, session revert.Session) Reading
func (r Reading) Coverage() []ufw.Coverage
func (r Reading) Applied(name string) bool

package steps // added, at the end of findings.go
func UFWFinding(f ufw.Finding) engine.Finding // UFW_...
```

- **Other units' code:** `pkg/engine/steps` exports `UFWFinding` beside `SSHDFinding` and `PVEFinding` ([checks.md](checks.md) §1.7: an area adds its primitive's conversion there, rather than a copy; [steps.md](steps.md) §2.23 is that unit's page); `pkg/output` registers the three finding codes (at the end), with the reference page's rows. The package uses `ssh.CodeNotInEffect` and `ssh.Code22Bound`, SSH Port's codes for the same facts, with SSH Port's details, rather than codes of its own. **The SSH listener reading is a copy, not shared:** `sockets.go` makes the same `reload.OpenRCFlavour`, `sshd.ReadEffective` and `reload.ReadBound` calls and applies the same rule (`reload.Unbound`) as `pkg/engine/controls/ssh`, whose reading is unexported; it differs where this row needs it (no `ss` is not established here, §2.3), and the two must be kept in step until one exports the reading. `go.mod` and `go.sum` are unchanged.
- **Not modelled:** packet filters other than ufw off Proxmox VE (nftables, iptables, firewalld, Alpine's awall, Docker's chains), so "ufw not installed" and "inactive" describe ufw alone, and a host filtered by another tool shows `NotConfigured`, the pessimistic error; ufw on Proxmox VE, which origin/main enables (V10): not read there; ufw's chains rule by rule (what `ufw status verbose` shows is its saved rules, [ufw.md](../safety/ufw.md) §2.3); a stale SSH port other than 22 (§2.3); whose a socket on 22 is (§2.7); `pve-firewall`'s accept rules, security groups, aliases and the management set's automatic members, and IPv4 against IPv6 ([pve.md](../safety/pve.md) §6); the session's own address against an allowlist (§2.7).
- **For the maintainer:** the wording of §5; the control's `ID`, `firewall.incoming` (stable once released, in `--output json`'s `status[]`), and its type `Incoming`, for a one-row area; the states chosen where the brief does not say: an SSH listener refused, or let through from loopback only, `Partial` rather than `NotConfigured` (§2.6); `FIREWALL_SSH_BLOCKED` under an allow policy too (§2.6); an SSH allowlist and a blocklist under ufw `Partial` with the finding, changed after review from `Configured` (§2.7: `Configured` would claim that SSH gets through; `Unknown` is the other honest choice), where `Partial` ranks an allowlist below SSH open to all with a deny policy; `pve-firewall`'s management-only rule `Configured` with the set written, `Unknown` without it (§2.11); a rule that opens every port `Partial` (§2.7); `ENABLED=no` while active `Partial`, `ENABLED=yes` while inactive `NotConfigured` (§2.5, §2.7); without `ss`, `Unknown` on every host, changed after review from SSH Port's `Configured` from configuration alone (§2.3), which on a stock Alpine leaves a deny policy `Unknown` until `iproute2` is installed; on Proxmox VE a stopped service `Unknown` even when the firewall is disabled, as `ReadFirewall` decides (§2.10), and an SSH listener on another port `Unknown` rather than blocked (§2.11).
- **Inputs to `next/p4-risk`:** **`FIREWALL_SSH_BLOCKED` is the brief's critical cross-check, and the only critical code of this row**: score it critical, once per row (the firewall counts once, P-06). For the status row it replaces [ufw.md](../safety/ufw.md) §6's hand-off ("`FindingNotAllowed` under a default deny, scored critical"), which `status[]` cannot apply (`engine.Finding` has no policy): **`UFW_SSH_NOT_ALLOWED` and `UFW_SSH_RESTRICTED` in `status[]` are per-listener detail, not scored**. Where ufw refuses a socket on 22 that may not be sshd's ("can't tell if 22 is ssh"), the row carries `UFW_SSH_NOT_ALLOWED` and no `FIREWALL_SSH_BLOCKED` on purpose: it is not shown as a lockout. `UFW_SSH_NOT_ALLOWED_AT_START` is the same lockout at ufw's next start (a reboot with `ENABLED=yes`), not critical here; whether it is, is `p4-risk`'s. `FIREWALL_SSH_UNCHECKED` and `FIREWALL_PVE_LOCAL_ONLY`, like `Unknown`, say the cross-check was not made or not established: factors. `SSH_PORT_NOT_IN_EFFECT` and `SSH_PORT_22_BOUND` here are the SSH rows' facts seen from the firewall, with the same details: count them once, by `Code`.
- **Slice 4, unit (b):** `Plan` by `Action` replaces `engine.CheckOnly`'s; `sockets.go`'s reading is exported, the row's, Enable's and the screen's, and a plan's own refusal (`ufw.ErrBlocks`) stays the plan's, in [gallery-firewall.md](gallery-firewall.md) §6's words. `Check` takes no configuration.
- **Size** (against `next/p4-check-ssh`): 734 lines of non-test Go (142 of them whole-line comments; the package 725, `steps.UFWFinding` 4, the registry 5), 755 of tests (`firewall_test.go` 753, `codes_test.go` 2) and 164 of docs (this page 160, the reference rows 3, the safety index 1), against the plan's guideline of about 500: one control over three readers (ufw, `pve-firewall`, the SSH listeners against the bound sockets) on four platforms, a test for each line of §5, and the review's fixes (loopback only, allowlists, a rule that opens every port, no `ss`, the management IP set), about 170 of the package's lines.
