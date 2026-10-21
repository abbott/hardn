# Firewall: the firewall status row (`pkg/engine/controls/firewall`)

> **Unit:** refactor plan Phase 4, `next/p4-check-firewall` (plan §2, row `p4-check-<area>`, firewall: "`ufw status verbose` (policy + whether the SSH listener set is allowed); `pve-firewall` on Proxmox"; brief Phase 4, "Where each status comes from" and "Cross-checks that should produce findings"; brief 2.5, Proxmox VE's firewall; brief §3 principles 1 and 3; decision §3.17, approved: no UFW `disable`/`reset`; decision §3.18, approved: SSH stays on 22 on Proxmox VE; [P-05](../refactor/proposals.md#p-05), whose firewall cross-check this builds; [P-06](../refactor/proposals.md#p-06), the firewall counted once). Follows the pattern of [checks.md](checks.md), set by `next/p4-check-ssh`. It takes up the "Status sources" item of [docs/safety/README.md](../safety/README.md) for the firewall, [ufw.md](../safety/ufw.md) §6's and [pve.md](../safety/pve.md) §6's `p4-check-firewall` items, and [check-ssh.md](check-ssh.md) §6's hand-off: the cross-check against the sockets `ss` shows bound. V-numbers: V9 and V12's status half (the configured port taken for the listeners) and V10 (ufw on Proxmox VE): this row reads `pve-firewall` there and never ufw.
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only, with ufw answered by a model of ufw 0.36.2's status and saved files, `sshd -T`, `systemctl show -p Listen ssh.socket` and `ss -Htln` by the ports a case gives, and `pve-firewall`'s files written as its parser reads them (§3). Nothing has run on Linux: everything host-specific is "written, not run (gate 5)" (§4). Every detail below is **proposed wording**, for the maintainer's review (§5).

Figma's main menu has one firewall row: Firewall "deny policy". It is now a check-only control (`firewall.incoming`) whose `Check` reads ufw's state and whether ufw lets every SSH listener through, or on Proxmox VE `pve-firewall`'s state, ufw not consulted. `Plan` returns `engine.ErrNotMigrated` until slice 4.

## 1. Why

origin/main's firewall row counts the firewall twice (enabled, and configured) and reads neither whether ufw refuses new connections nor whether SSH gets through ([P-06](../refactor/proposals.md#p-06)); its firewall step allows the configured port, never the ones sshd listens on (V9), and enables ufw on Proxmox VE (V10). A row that shows "deny policy" while the firewall refuses the port sshd accepts on tells the user the host is hardened when the next SSH session cannot log in. The brief makes that a critical finding: "a default-deny firewall and an SSH port that isn't allowed" (brief Phase 4). A status that says a host is safe when it is not is the worst error of this phase: Unknown rather than a guess.

## 2. Guarantees

### Reading

1. **Which firewall:** whether the host runs Proxmox VE must be established first (`pve.Applies`): not established, the row is `Unknown`, "can't tell if Proxmox", with `pve.ErrProxmoxUnknown` (`PVE_UNKNOWN`), and nothing is read. On Proxmox VE, `pve.ReadFirewall` (§2.10); elsewhere `ufw.Read` (§2.5). ufw is never run or read on Proxmox VE (brief 2.5; tested: no `ufw` command and no read under `/etc/ufw` there).
2. **Read-only.** The check runs exactly `ufw status verbose`, `sshd -T -f /etc/ssh/sshd_config` (with `-C` per Match block, `sshd.ReadEffective`), `systemctl show -p Listen ssh.socket` on a socket-activated host, `ss -Htln`, and on Proxmox VE `systemctl is-active` of `pve-firewall.service` or `proxmox-firewall.service`; on Alpine, where `sshd.krb5` or `sshd.pam` is installed, its `-G` (`reload.OpenRCFlavour`). It reads ufw's settings and saved rules, sshd's files and `pve-firewall`'s two files. Tested: every call of every test (`readOnly`).
3. **The SSH listeners as SSH Port reads them, checked against what is bound** ([check-ssh.md](check-ssh.md) §2.12, §6): the configured set (`sshd -T`'s `listenaddress` lines ∪ `ssh.socket`'s `Listen` entries on a socket-activated host, `sshd.ReadEffective`), and the sockets `ss -Htln` shows bound (`reload.ReadBound`), compared by `reload.Unbound`, the rule `reload.CheckInEffect` uses. They are **established** when the configured set is (`Listeners.Known`, not empty) and every listener of it is bound. Then the firewall must let the configured set through, and a socket bound on 22 that no configured listener is on (sshd not reloaded since 22 was dropped, or another program: `ss` does not say whose) too (§2.7). **Not established**, the firewall is checked against every candidate: every listener the sources read, and every socket bound on their ports and on 22, any of which may be the one sshd accepts on (an edit to 2230 not reloaded leaves sshd on 22, with 2230 configured and not bound). Only those ports are looked at: another socket is not taken for sshd's (a web server's 80), so a stale port other than 22 (sshd still on 2230 after an edit back to 22) is not seen, and the row is then `Unknown`, "ssh config not in effect" (tested on Proxmox VE). **Without `ss`**, as SSH Port: off a socket-activated host the configured set stands, and the evidence says that the bound sockets were not read (a stock Alpine, **[unverified]**: those lines are from configuration alone, §5); on one, the set is not established ("ss not installed", `COMMAND_NOT_FOUND`), since after a `daemon-reload` `sshd -T` and `ssh.socket` agree whatever is bound.
4. **A read that fails is the check's error** ([checks.md](checks.md) §1.5), reported `Unknown` with it, **where the state depends on it**: ufw's status or settings, `pve-firewall`'s files, and, under a policy that refuses what no rule accepts, the SSH listeners. A state established without the listeners is reported: ufw inactive with `ENABLED=no`, and `pve-firewall` disabled or `ACCEPT`, read none; an allow policy, "off after restart" and "enabled, not running" read them for their findings, and where they are not established that is a finding beside the state, `FIREWALL_SSH_UNCHECKED` (with the error's text where a read failed), not the check's error, which would make the engine report the row `Unknown` (`Engine.Check`). The other way, a listener the firewall refuses is `Partial`, "ssh blocked", whatever else is not established (§2.6): a state established is reported ([checks.md](checks.md) §1.5).

### ufw (Debian, Ubuntu, Alpine)

5. **ufw's state** (`ufw.Read`, [ufw.md](../safety/ufw.md) §2.2 to §2.4): not installed is `NotConfigured`, "ufw not installed" ([ufw.md](../safety/ufw.md) §6: not installed is not configured, not unknown); a status not read whole (a line ufw does not print, `Default:` missing) is `Unknown`, "can't read", with `UFW_UNKNOWN`; `Status: inactive` with `ENABLED=no` is `NotConfigured`, "inactive", and nothing else is read. **`ENABLED=yes` and inactive** (a failed start; on Alpine a reboot with ufw in no runlevel, [ufw.md](../safety/ufw.md) §6) is `NotConfigured`, "enabled, not running": nothing filters now; each SSH listener its saved rules would refuse once ufw starts is `UFW_SSH_NOT_ALLOWED_AT_START` (`ufw.Check`), and saved rules not read whole are `FIREWALL_SSH_UNCHECKED`.
6. **Active: the cross-check** (`ufw.Check(state, ufw.Needs(listeners, nil, nil))`, [ufw.md](../safety/ufw.md) §2.6, §2.16): each listener's verdict under `ufw status verbose` (the saved rules and policies, the chains loaded), in each IP version it accepts (a socket's `[::]` dual-stack). A listener ufw refuses (`Blocked` by the default policy, or `Denied` by a rule; IPv6 with `IPV6=no`) is `UFW_SSH_NOT_ALLOWED`, and the row gets one **`FIREWALL_SSH_BLOCKED`** listing every refused listener: **the brief's critical finding, which `next/p4-risk` scores critical** (§6). The row is then `Partial`, "ssh blocked": ufw is active and refusing what no rule accepts, which the baseline wants, and refuses SSH, which it must not; the lockout is the finding's to weigh, not the state's. `FIREWALL_SSH_BLOCKED` is made **whatever the default policy** (decision of this unit): the brief names a default deny, and a deny rule for SSH under an allow policy refuses the same new sessions.
7. **Active, nothing refused:**
   - default **allow** incoming is `Partial`, "allow policy": ufw refuses only what its rules deny;
   - **`ENABLED=no`** while active (`ufw --force enable` failing after its start, which writes `ENABLED=no` back without stopping the chains, [ufw.md](../safety/ufw.md) §2.11) is `Partial`, "off after restart": ufw does not start at the next boot;
   - default **deny** or **reject** with the listeners not established (§2.3) is `Unknown`, with the reason (§5); with a verdict ufw's rules do not establish (a port list hardn cannot read) `Unknown`, "can't read", with `UFW_UNKNOWN`; with a socket bound on 22 that no configured listener is on, refused by ufw, `Unknown`, "can't tell if 22 is ssh", with `UFW_SSH_NOT_ALLOWED` and `SSH_PORT_22_BOUND` (ufw refusing a socket that may be another program's, or a stale sshd's, while sshd accepts on the configured ports, is not shown as a lockout, nor as safe);
   - otherwise **`Configured`**, "deny policy" or "reject policy". **A listener let through from some sources only** (`UFW_SSH_RESTRICTED`, an allowlist) or **from everywhere but some** (`UFW_SSH_ALLOWED_EXCEPT`, a blocklist), or rate-limited (`limit`), is let through: `Configured`, with the finding beside it (decision of this unit). `Partial` there would rank an SSH allowlist, which narrows who can reach sshd, below SSH open to all; that the session's own address is among those let through is not established ([ufw.md](../safety/ufw.md) §2.6), which the finding says.
8. **Evidence**: `ufw status verbose`'s status line and, active, its `Default:` line as ufw prints it; `ENABLED`; the listeners as SSH Port's evidence lists them (`sshd -T: listenaddress …`, `systemctl show -p Listen ssh.socket: …`, `ss -Htln: …` on the SSH ports and 22, or that `ss` is not installed); active, each listener's verdict and the rule or policy that decided it (`ufw: listener 0.0.0.0:2230 (IPv4): allowed by 2230/tcp ALLOW IN Anywhere`). Tested: `TestEvidence`.
9. **Findings**: ufw's own, coded by `steps.UFWFinding` as a plan codes them (`UFW_SSH_NOT_ALLOWED`, `UFW_SSH_RESTRICTED`, `UFW_SSH_ALLOWED_EXCEPT`, `UFW_UNKNOWN`, `UFW_SSH_NOT_ALLOWED_AT_START`); SSH Port's `SSH_PORT_NOT_IN_EFFECT` for each configured listener not bound, and `SSH_PORT_22_BOUND` (§2.7); this package's `FIREWALL_SSH_BLOCKED` and `FIREWALL_SSH_UNCHECKED`. Every code is registered; tested: each test checks that every finding's code is a registered finding code.

### Proxmox VE (`pve-firewall`)

10. **pve-firewall's state** (`pve.ReadFirewall`, [pve.md](../safety/pve.md) §2.14 to §2.16), in this order:
    - pmxcfs not mounted: `Unknown`, "/etc/pve not mounted", with `pve.ErrClusterFSDown` (`PVE_CLUSTER_FS_DOWN`); whether the firewall is on, the service's state or `nftables` not established (a file not read, an option in a form `pve-firewall` skips, a time-stamped `enable`, `systemctl` failing): `Unknown`, "can't read", with `ReadFirewall`'s `Err` (`PVE_FIREWALL_UNKNOWN`);
    - the service not active: `Unknown`, "service stopped": what it loaded before is not read, whether the firewall is enabled or not, as `ReadFirewall` decides;
    - disabled (`cluster.fw`'s `enable` 0, its default, or `host.fw`'s): `NotConfigured`, "disabled";
    - `nftables: 1`: `Unknown`, "uses nftables", with `PVE_FIREWALL_NOT_MODELLED` (proxmox-firewall's rules are not read);
    - `policy_in` `ACCEPT`: `Partial`, "accept policy";
    - `DROP` or `REJECT`: 22 and 8006 must be accepted from the management IP set (`ReachManagement`, `pve-firewall`'s own rule): a rule that may drop either first, or a non-empty blacklist, is `Unknown`, "ssh may be blocked" or "web ui may be blocked", with `PVE_RULE_MAY_BLOCK` or `PVE_BLACKLIST`.
11. **Then the SSH listeners** (§2.3): every one on 22, the one SSH port `pve-firewall`'s management rule accepts (§3.18), is `Configured`, "drop policy" or "reject policy". A listener on another port is `Unknown`, "2230 may be blocked", with `PVE_PORT_NOT_22`: `pve-firewall`'s rules for that port, which may accept it, are not read, so it is neither shown blocked nor allowed. The listeners not established: `Unknown` with the reason, as off Proxmox VE. `ReachManagement` is "allowed from the management network", not "allowed" ([pve.md](../safety/pve.md) §6): it is `Configured` all the same, as an allowlist is under ufw (§2.7): `pve-firewall`'s own baseline, which adds the cluster's local network to the set itself. **`FIREWALL_SSH_BLOCKED` is not made on Proxmox VE**: `ReadFirewall` never establishes a port refused (its `Reach` has no such value), only one that may be.
12. **Evidence**: `pve-firewall`'s options (`enabled`, `policy_in`, `nftables`), `systemctl is-active`'s answer, how far 22 and 8006 are let through (`Reach`'s code), the management IP set as written, and the listeners as in §2.8.

### Check-only

13. **`Applies` is true everywhere**: detect's facts do not establish a host without a packet filter, and `NotApplicable` would leave the row out of the risk level ([checks.md](checks.md) §1.6).
14. **`Plan` returns `engine.ErrNotMigrated`** (`engine.CheckOnly`), and `Engine.Plan` over `Controls()` is refused, the item's error coded `CONTROL_NOT_MIGRATED`, with no change and no call but the run directory's (`TestCheckOnly`).
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
- `ufw allow from 192.0.2.0/24 to any port 22 proto tcp` alone: "deny policy" with `UFW_SSH_RESTRICTED` (and, with IPv6 on, "ssh blocked" for `[::]:22`); `IPV6=no` with sshd on `[::]:22`: "ssh blocked";
- Alpine 3.24 with ufw from the community repository, enabled, then rebooted without `rc-update add ufw default`: "enabled, not running" (whether ufw comes back is [ufw.md](../safety/ufw.md) §4's question); with and without `iproute2` (§3);
- ufw enabled and its start made to fail (as [ufw.md](../safety/ufw.md) §4): "off after restart";
- Proxmox VE 9: the datacenter firewall on with an IP set `management`: "drop policy"; `policy_in: ACCEPT`: "accept policy"; a host rule `IN DROP -p tcp -dport 22`: "ssh may be blocked"; `systemctl stop pve-firewall`: "service stopped"; `systemctl stop pve-cluster`: "/etc/pve not mounted"; `nftables: 1`: "uses nftables"; each compared with `iptables-save` and a connection from inside and outside the management network.

## 5. The status lines

Every line the row can show, in Figma's grammar (marker, label, the state's word, the detail). **Every detail is proposed wording**; Figma's "deny policy" is the anchor. A port list is the ports in ascending order, joined by ", ".

**ufw (Debian, Ubuntu, Alpine)**

| The host (`ufw status verbose`, its settings; the SSH listeners, §2.3) | State | Detail |
|---|---|---|
| active, default deny incoming, every SSH listener let through (from anywhere, rate-limited, or from some sources only: §2.7) | Configured | deny policy |
| the same, `ss` not installed off a socket-activated host (a stock Alpine): **the listeners from configuration alone** (§2.3) | Configured | deny policy |
| the same, default reject incoming | Configured | reject policy |
| active, an SSH listener refused, by the default policy or a rule, whatever the policy (`FIREWALL_SSH_BLOCKED`) | Partial | ssh blocked |
| active, default allow incoming, no SSH listener refused | Partial | allow policy |
| active, default deny or reject, nothing refused, `ENABLED=no` | Partial | off after restart |
| inactive, `ENABLED=no` (Ubuntu's stock state) | Not Configured | inactive |
| inactive, `ENABLED=yes` | Not Configured | enabled, not running |
| ufw not installed (a stock Alpine; other packet filters are not read) | Not Configured | ufw not installed |

**Proxmox VE (`pve-firewall`; ufw not read)**

| The host (`cluster.fw`, `host.fw`, the service; the SSH listeners) | State | Detail |
|---|---|---|
| enabled, service active, `policy_in` DROP, 22 and 8006 accepted from the management IP set, every SSH listener on 22 | Configured | drop policy |
| the same, REJECT | Configured | reject policy |
| enabled, service active, `policy_in` ACCEPT | Partial | accept policy |
| disabled (the stock datacenter `enable: 0`), service active | Not Configured | disabled |

**Unknown**

| The host | Detail | Error code |
|---|---|---|
| whether the host runs Proxmox VE not established | can't tell if Proxmox | `PVE_UNKNOWN` |
| `ufw status verbose` failing (not root), a settings or rules file not read | can't read | the error's (`COMMAND_FAILED`, `FILE_NOT_FOUND`) |
| ufw's status not read whole, or a rule whose port list hardn cannot read | can't read | none (finding `UFW_UNKNOWN`) |
| default deny or reject, and: no `sshd` | sshd not installed | `COMMAND_NOT_FOUND` |
| … `sshd -T` failing (an invalid configuration, `/run/sshd` missing, an `Include` other than the stock one) | can't tell ssh ports | the error's (`SSHD_CONFIG_INVALID`, `SSHD_ENV_NO_RUNDIR`, …) |
| … the configured listeners not established (the socket and sshd differ, the unit that listens not up), nothing refused | can't tell ssh ports | none |
| … `ss` failing | can't tell ssh ports | `COMMAND_FAILED` |
| … `ss` not installed on a socket-activated host | ss not installed | `COMMAND_NOT_FOUND` |
| … a configured listener not bound, every candidate let through (§2.3) | ssh config not in effect | none (finding `SSH_PORT_NOT_IN_EFFECT`) |
| … a socket on 22 no configured listener is on, refused by ufw (§2.7) | can't tell if 22 is ssh | none (findings `UFW_SSH_NOT_ALLOWED`, `SSH_PORT_22_BOUND`) |
| Proxmox VE: pmxcfs not mounted | /etc/pve not mounted | `PVE_CLUSTER_FS_DOWN` |
| Proxmox VE: an option in a form `pve-firewall` skips, a file not read, `systemctl` failing | can't read | `PVE_FIREWALL_UNKNOWN` |
| Proxmox VE: the service not active | service stopped | none |
| Proxmox VE: `nftables: 1` | uses nftables | none (finding `PVE_FIREWALL_NOT_MODELLED`) |
| Proxmox VE: DROP or REJECT, a rule that may drop 22, or a blacklist | ssh may be blocked | none (findings `PVE_RULE_MAY_BLOCK`, `PVE_BLACKLIST`) |
| Proxmox VE: DROP or REJECT, a rule that may drop 8006 | web ui may be blocked | none (finding `PVE_RULE_MAY_BLOCK`) |
| Proxmox VE: DROP or REJECT, an SSH listener on another port | 2230 may be blocked | none (finding `PVE_PORT_NOT_22`) |
| Proxmox VE: DROP or REJECT, the listeners not established | as ufw's above | as above |

**Wording to read again** (every line is the maintainer's to decide). The policy in each tool's own word ("deny policy", "reject policy", "allow policy" for ufw; "drop policy", "reject policy", "accept policy" for `pve-firewall`, its `policy_in` values), or Figma's "deny policy" and "allow policy" for both; "ssh blocked" or "blocks ssh"; "inactive" (ufw's own word) and "disabled" (`pve-firewall`'s), or "off" for both; "enabled, not running" or "stopped (enabled)"; "off after restart" (P-43's AppArmor line); "ufw not installed" or "no ufw"; "can't tell ssh ports" or "ssh ports unclear"; "ssh config not in effect", "can't tell if 22 is ssh", "uses nftables", "web ui may be blocked", "/etc/pve not mounted" (new). Lower case throughout, "ssh" included, as Figma's details are.

## 6. API; changes to other units' code; not modelled; for the maintainer

```go
package firewall // pkg/engine/controls/firewall (new)
const (
	CodeSSHBlocked   = "FIREWALL_SSH_BLOCKED"   // the critical cross-check
	CodeSSHUnchecked = "FIREWALL_SSH_UNCHECKED" // the listeners not established, beside a state that does not depend on them
)
type Incoming struct{ engine.CheckOnly } // "firewall.incoming", "Firewall"
func Controls() []engine.Control         // Incoming

package steps // added, at the end of findings.go
func UFWFinding(f ufw.Finding) engine.Finding // UFW_...
```

- **Other units' code:** `pkg/engine/steps` exports `UFWFinding` beside `SSHDFinding` and `PVEFinding` ([checks.md](checks.md) §1.7: an area adds its primitive's conversion there, rather than a copy; [steps.md](steps.md) §2.23 is that unit's page); `pkg/output` registers the two finding codes (at the end), with the reference page's rows. The package uses `ssh.CodeNotInEffect` and `ssh.Code22Bound`, SSH Port's codes for the same facts, rather than codes of its own. `go.mod` and `go.sum` are unchanged.
- **Not modelled:** packet filters other than ufw off Proxmox VE (nftables, iptables, firewalld, Alpine's awall, Docker's chains), so "ufw not installed" and "inactive" describe ufw alone, and a host filtered by another tool shows `NotConfigured`, the pessimistic error; ufw on Proxmox VE, which origin/main enables (V10): not read there; ufw's chains rule by rule (what `ufw status verbose` shows is its saved rules, [ufw.md](../safety/ufw.md) §2.3); whether ufw is in an OpenRC runlevel on Alpine, so an active ufw that will not come back after a reboot shows "deny policy"; a stale SSH port other than 22 (§2.3); whose a socket on 22 is (§2.7); `pve-firewall`'s accept rules, security groups, aliases and the management set's automatic members, and IPv4 against IPv6 ([pve.md](../safety/pve.md) §6); the session's own address against an allowlist (§2.7).
- **For the maintainer:** the wording of §5; the control's `ID`, `firewall.incoming` (stable once released, in `--output json`'s `status[]`), and its type `Incoming`, for a one-row area; the states chosen where the brief does not say: an SSH listener refused `Partial` rather than `NotConfigured` (§2.6); `FIREWALL_SSH_BLOCKED` under an allow policy too (§2.6); an SSH allowlist, a blocklist and `pve-firewall`'s management-only rule `Configured` with a finding (§2.7, §2.11); `ENABLED=no` while active `Partial`, `ENABLED=yes` while inactive `NotConfigured` (§2.5, §2.7); without `ss` off a socket host, `Configured` from configuration alone, as SSH Port (§2.3); on Proxmox VE a stopped service `Unknown` even when the firewall is disabled, as `ReadFirewall` decides (§2.10), and an SSH listener on another port `Unknown` rather than blocked (§2.11).
- **Inputs to `next/p4-risk`:** **`FIREWALL_SSH_BLOCKED` is the brief's critical cross-check**: score it critical, once per row (the firewall counts once, P-06). `UFW_SSH_NOT_ALLOWED_AT_START` is the same lockout at ufw's next start (a reboot with `ENABLED=yes`), not critical here; whether it is, is `p4-risk`'s. `FIREWALL_SSH_UNCHECKED`, like `Unknown`, says the cross-check was not made: a factor. `SSH_PORT_NOT_IN_EFFECT` and `SSH_PORT_22_BOUND` here are the SSH rows' facts seen from the firewall; count them once.
- **Inputs to slice 4:** `Plan` replaces `engine.CheckOnly`'s with `steps.PlanUFWEnable` (`ufw.PlanFor`) and may give `Check` the configuration to compare with; the SSH listener reading of `sockets.go` is the one `CheckInEffect` and SSH Port use, and a plan's own refusal (`ufw.ErrBlocks`) stays the plan's.
- **Size:** about 550 lines of non-test Go (about 105 of them whole-line comments), 640 of tests and 200 of docs, against the plan's guideline of about 500: one control over three readers (ufw, `pve-firewall`, the SSH listeners against the bound sockets) on four platforms, and a test for each line of §5.
