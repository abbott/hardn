# UFW: letting the SSH listeners through before the firewall can block them (`pkg/host/ufw`)

> **Unit:** refactor plan Phase 2, `next/p2-ufw` (decision §3.17, approved 2026-10-17: no UFW `disable`/`reset` sequence; brief §2 decision "Proxmox hosts: no UFW"; brief 2.2 "UFW: after making changes, check the rules with `ufw status verbose`"; brief 2.3 step 2, "firewall first"). V-numbers: V9 (origin/main allows only the configured port and runs `ufw disable` then `ufw reset` first), V10 (UFW enabled on Proxmox VE), the firewall half of V8 (the new port allowed before sshd moves; the order itself is `p3-engine`'s) and of V12 (a socket host's listeners) ([side-effects.md](../refactor/phase0/side-effects.md)).
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only, with ufw answered by a small model in the tests (`helpers_test.go`) written from ufw 0.36.2's sources (§3), not from output captured on a host. Nothing has run on Linux; §4 lists what is "written, not run (gate 5)".

`ufw.Read(ctx, sys, pve)` establishes UFW's state: `ufw status verbose`, `/etc/default/ufw` and the saved rules in `/etc/ufw/user.rules` and `user6.rules`. `ufw.PlanFor(in)` is pure: from the effective SSH listener set (`sshd.ReadEffective`'s `Listeners`), the ports a change adds and that state, it returns `ufw allow <port>/tcp` for each port nothing lets through, then the default policies, then `ufw --force enable`, and refuses a plan after which an SSH listener that is reachable now, or a new port, would not be. `ufw.Execute(ctx, sys, plan)` runs the steps and checks `ufw status verbose`; `ufw.Rollback` runs the undo of what took effect. `ufw.Check(state, needs)` is the cross-check for a status report.

## 1. Why

origin/main's firewall step sets the defaults, runs `ufw disable`, then `ufw reset`, then allows `cfg.SshPort` and pipes `yes` into `ufw enable` (side-effects row 19). It allows the *configured* port, never the ones sshd listens on (V9), so a stray `Port 22`, a `ListenAddress` with its own port or ssh.socket's listeners are blocked once ufw starts; a failure after `disable` leaves the firewall off with no rollback; and nothing stops it on Proxmox VE, where ufw competes with pve-firewall and blocks the web UI, corosync and migration (V10). A captured dry run allowed a different port from the configured SSH port ([phase0-findings.md](../refactor/phase0-findings.md) §3.3). Brief §3 principle 1: never lock the user out.

## 2. Guarantees

### Reading (`Read`, `ParseStatus`, `ParseTuples`)

1. **Proxmox VE is never touched.** With `detect.Proxmox.Installed` Yes, `Read` runs nothing and returns `ErrNotApplicable`; `PlanFor` returns a `NotApplicable` plan, and `Execute` runs nothing for it. A Proxmox fact that is not established is `ErrProxmoxUnknown` (wrapping detect's reason), never "not Proxmox": V10 is critical. Tested: no call is recorded on the fake.
2. **Read-only.** `Read` runs exactly `ufw status verbose`, reads `/etc/default/ufw`, stats `/proc/sys/net/ipv6`, and reads `user.rules` and, when ufw's IPv6 is on, `user6.rules` (tested: no other call). ufw missing (`host.ErrNotFound`) is `State{}` with `Installed` false; a failing command or read is an error; content it cannot read goes to `Bad`, and `State.Known()` is false.
3. **What is in force and what is saved.** `ufw status verbose` says whether ufw's chains are loaded (`Status: inactive` when `iptables -L ufw-user-input` fails) and, while active, prints the logging level, `Default: <in> (incoming), <out> (outgoing), <routed> (routed)` and the rules of its saved lists, IPv4's then IPv6's, each in order. Inactive, it prints nothing else, so the plan reads the saved rules themselves, the `### tuple ###` lines ufw loads when it starts, and the defaults from `/etc/default/ufw` as ufw reads them (§3). A status row is split at its action (`ALLOW|DENY|REJECT|LIMIT` + `IN|OUT|FWD`): before it the To column (`[address] [ports[/proto]] [(application)] [(v6)] [on <interface>]` or `Anywhere[/proto]`), after it From, then a log type and `# comment`, which may hold anything (tested with `# x11 ALLOW IN y`).
4. **IPv6 as ufw decides it.** `State.IPv6` is `IPV6=yes` in `/etc/default/ufw` and the kernel's `/proc/sys/net/ipv6`, ufw's own `use_ipv6()`. Without it ufw adds IPv4 rules only and, once started, drops all IPv6 (§3).

### Planning (`Needs`, `Evaluate`, `PlanFor`, pure)

5. **The effective listener set, not the configured port.** `Needs` lists every listener of sshd's `listenaddress` lines and of `ssh.socket`'s `Listen` set (a socket's `[::]` is dual-stack, systemd's default, so it also needs IPv4; sshd's own `[::]` is IPv6 only, §3), then `Input.Add` (the port the change adds) and `Input.Keep` (ports the configuration does not show, such as the current session's) in each IP version the listeners use. `Listeners.Known` false is `ErrListenersUnknown`, so nothing is enabled or narrowed on an incomplete set ([sshd-effective.md](sshd-effective.md) §6).
6. **A verdict per need, as ufw's chains decide.** In each family's list, in order, the first rule that matches TCP to the port wins ("Rule ordering is important and the first match wins", ufw(8)): an unrestricted `allow` is `Allowed`, `limit` `Limited` (accepted, rate-limited per source), `deny`/`reject` `Denied`; a rule restricted by source, interface or one destination address only restricts (`Restricted`), and an earlier restricted rule of the other kind makes an unrestricted one `Restricted`. No match: the default incoming policy decides (`allow` is `Allowed`, `deny`/`reject` `Blocked`). Inactive ufw accepts everything (its stop sets every policy to ACCEPT, §3); IPv6 with ufw's IPv6 off is `Blocked`. A port list, range or address it cannot read is `VerdictUnknown`, never a pass. Tested case by case (`TestVerdict`).
7. **Rules first, then the policy, then enable; never `disable` or `reset`.** A port gets `ufw allow <port>/tcp` when, with ufw active and the planned default policy, nothing lets it through (`Blocked`), even if ufw stays inactive, so a later `ufw enable` by hand keeps it open. Then `ufw default <policy> incoming|outgoing` for each `Desired` policy that differs, then `ufw --force enable` when `Desired.Enable` and ufw is inactive: ufw(8) says enabling flushes its chains and that SSH should be allowed before it. No step runs `ufw disable` or `ufw reset` (tested over every plan in `TestPlanFor`); `ufw disable` exists only as the undo of the plan's own enable.
8. **Rules already there are never repeated, changed or removed.** A port with any `Denied`, `Restricted` or unreadable verdict, in either family, gets no rule: `ufw allow <port>/tcp` applies to both families, so it would widen an administrator's restricted IPv4 rule for the sake of IPv6, and ufw turns a saved `deny <port>/tcp` into the allow ("Rule updated") instead of adding one (§3). Tested: the active host with a deny in IPv4 only gets no step.
9. **Refused when the plan would block a listener (`*BlockError`, `ErrBlocks`).** After the steps, every need is evaluated again: one that is not `Allowed` or `Limited` then, while it is now (inactive ufw, or let through by the rules in force), or a new port, refuses the whole plan with the needs, their verdicts and the deciding rule. So a saved deny, a saved restricted allow, or `IPV6=no` with sshd on `[::]:22`, all of which take effect only when ufw starts, are refused before anything runs (tested). A listener that is already not let through and stays so is a pre-existing state, not caused by the plan: it is a `Finding`, and the plan goes ahead (tested).
10. **Idempotent.** With the rules, policies and state the plan leads to, the second plan is empty (`TestExecute2230`). Other `Desired` errors: a policy ufw does not have, a port 0 (`fs.ErrInvalid`); `ErrNotInstalled`; `ErrStateUnknown` for a line not read.

### Executing (`Execute`, `Rollback`)

11. **Steps in order, stop at the first failure.** A command's error is returned with its argv (`*host.ExitError` for ufw's exit 1 and `ERROR:` on stderr). The `Report` holds every step up to the failed one with its `host.Result`, and `Report.Undo`, the inverse commands of the steps that took effect, last first (`ufw disable`, `ufw default <old> <dir>`, `ufw delete allow <port>/tcp`): the input to `Rollback` and to `p2-revert`'s journal.
12. **An undo never deletes a rule hardn did not add.** An allow is undone only when ufw's answer is "Rule added"/"Rules updated" for every family and nothing else; "Skipping adding existing rule" (the rule was saved already) is not undone, and a mixed or unknown answer (ufw added IPv4's and skipped IPv6's; `ufw delete allow <port>/tcp` would delete both) is left, with `StepReport.Note` saying why: an allow rule left behind never locks anyone out (tested).
13. **Verified with `ufw status verbose`.** After the steps, the status must be read whole and show the planned state: active or not, the default incoming policy, and every need the plan lets through let through by the rules in force (`ErrVerify` naming the need otherwise). This catches what an exit status does not: ufw answers a declined enable with "Aborted" and exit 0 (tested with the model). Inactive by design, `Verified` stays false with `Unverified` "rules are saved, not in force". `Report.Coverage` is each need's verdict under the status.
14. **Rollback** runs `Report.Undo` in order, every command even after one fails, and returns the errors joined. Every `host.System` method refuses a done context: after Ctrl-C pass `context.WithoutCancel(ctx)` (tested); a done context makes `Execute` run nothing (tested).
15. **Through `host.System` only.** Depguard rule `ufw-through-host` denies `os`, `io/ioutil`, `golang.org/x/sys`, `syscall`, `net` (except `net/netip`) and `plugin` to the package's non-test files (probed: an `os` import fails lint); `host-stdlib` keeps it to the standard library and `pkg/host/...`.

### The cross-check (`Check`)

16. `Check(state, needs)` evaluates the needs under `ufw status verbose` (what is in force) and returns a `Finding` for each that is not `Allowed` or `Limited` while ufw is active: `ssh-not-allowed` (`Blocked` or `Denied`), `ssh-restricted`, `ufw-unknown` (also for a status not read whole). `Finding.Incoming` carries the default policy, so `p4-risk` can score "default deny and an SSH listener not allowed" critical (plan, `p4-risk` row). `FindingKind.String()` is the stable code.

## 3. Platform facts relied on

ufw sources are Debian's ufw 0.36.2-9 (trixie), read 2026-10-17 through `sources.debian.org/data/main/u/ufw/0.36.2-9/` (Launchpad's git disallows automated reads in its robots.txt).

| Fact | Source | Used for |
|---|---|---|
| ufw 0.36.2 is packaged in Debian 13 (0.36.2-9) and 12 (0.36.2-1), and in Alpine 3.24's community repository (0.36.2-r4, with its own OpenRC script) | sources.debian.org `api/src/ufw/`; aports [`3.24-stable/community/ufw/APKBUILD`](https://raw.githubusercontent.com/alpinelinux/aports/3.24-stable/community/ufw/APKBUILD); Ubuntu 24.04/26.04's version **[unverified]** | the sources below hold for the supported releases |
| `get_status` returns "Status: inactive" when `iptables -L ufw-user-<chain> -n` exits 1; verbose output is `Status: active\n<Logging>\n<Default>\n<New profiles>` then, with rules, a blank line, `%-26s %-12s%s` header, dashes, and rows `"%-26s %-12s%-26s%s%s"` of To, action and direction, From, `" (<logtype>)"` and `" # <comment>"`; rules are `self.rules + self.rules6`, grouped in, out, routed with each group's order kept | `src/backend_iptables.py` `get_status` | §2.3, `ParseStatus` |
| The To and From columns: the address unless `0.0.0.0/0` or `::/0`; the port (or, verbose, the port and `(<app>[ (v6)])`); `/<proto>` unless `any`; `Anywhere` (plus `/<proto>` when both sides are any) for any address and port; `(v6)` for IPv6 wildcards; `" on <iface>"` last, the incoming interface on To for non-routed rules | `get_status`, the `for loc in ['dst', 'src']` loop and the interface handling | `statusTo`, `statusRule` |
| Saved rules are `### tuple ### <action> <proto> <dport> <dst> <sport> <src> [<dapp> <sapp>] <dir>[_<iface>]` with an optional ` comment=`; an action carries `_<logtype>` and `route:` for routed rules; empty applications are `-` | `backend_iptables.py` `_write_rules`, `_read_rules` (`pat_tuple`, `re.split(r'\s+', …)`); `src/common.py` `set_action` | `ParseTuples` |
| The saved rules are `/etc/ufw/user.rules` and `user6.rules`, the defaults `/etc/default/ufw` (`config_dir` is `/etc`) | `backend_iptables.py` `__init__` (`files['rules']`, `files['rules6']`); `setup.py` (`real_confdir`, `user_rules`, `defaults`) | §2.2 |
| `/etc/default/ufw` is read with `^\w+="?\w+"?`, split at `=`, key and value lower-cased, quotes stripped; `use_ipv6()` is `ipv6 == 'yes'` and `/proc/sys/net/ipv6` exists; `accept` is allow, `reject` reject, anything else deny; routed is `disabled` when forwarding is off | `src/backend.py` `_get_defaults`, `use_ipv6`, `_get_default_policy` | §2.4, `readDefaults` |
| The stock defaults are `IPV6=yes`, `DEFAULT_INPUT_POLICY="DROP"`, `DEFAULT_OUTPUT_POLICY="ACCEPT"`, `DEFAULT_FORWARD_POLICY="DROP"` | `conf/ufw.defaults` | test fixtures |
| With IPv6 not `yes`, starting ufw sets ip6tables' INPUT, FORWARD and OUTPUT to DROP (loopback accepted); stopping it sets every policy to ACCEPT and flushes its chains, ip6tables included | `src/ufw-init-functions` `ufw_start`, `ufw_stop` | §2.6 (inactive accepts, IPv6 off blocks), §2.9 |
| ufw accepts `RELATED,ESTABLISHED` first in `ufw-before-input`, so a session open before a change keeps working | `conf/before.rules`; brief 2.4 | §4: why the session survives enable |
| "Rule ordering is important and the first match wins"; enabling flushes the chains and "may drop existing connections (eg ssh)", so allow SSH before `ufw enable`; `ufw --force enable` skips the prompt; a limit rule denies an address that tries to "initiate 6 or more connections within 30 seconds"; rules matching both IPv4 and IPv6 apply to both | `doc/ufw.8` (RULE SYNTAX, REMOTE MANAGEMENT, `limit`, `--force`) | §2.6, §2.7 |
| `enable` prompts under SSH unless `--force`; a declined prompt (EOF reads as no) prints "Aborted" and exits 0; errors print `ERROR: …` on stderr and exit 1 | `src/ufw` (`continue_under_ssh`, `bailout`, `sys.exit(0)`), `src/frontend.py` `continue_under_ssh`, `src/util.py` `error` | §2.7 `--force`, §2.11, §2.13 |
| `allow` of a port with no address is applied to IPv4 and, with IPv6 on, IPv6 (`set_rule` twice); the answers are "Rule added", while inactive "Rules updated", for a saved rule "Skipping adding existing rule", each with " (v6)"; a rule equal but for its action or comment is replaced ("Rule updated") | `src/frontend.py` `set_rule`; `backend_iptables.py` `set_rule` | §2.8, §2.12 |
| `default <policy> <direction>` writes `DEFAULT_<CHAIN>_POLICY` and, while enabled, ufw stops and starts the firewall to apply it | `backend_iptables.py` `set_default_policy`; `frontend.py` `set_default_policy` | §2.7 order |
| `delete <rule>` (rule syntax, not a number) does not prompt | `frontend.py` (`delete_rule`'s prompt is for numbered deletes); the routing through `ufw.parser` **[I]** | undo argv |
| sshd binds `::` IPv6-only; a systemd `[::]` socket is dual-stack by default | [reload.md](reload.md) §3 (OpenSSH `sshd.c` `sock_set_v6only`; systemd.socket(5)) | §2.5 |
| Proxmox VE: no UFW; pve-firewall manages the packet filter, and a default deny would cut the web UI and cluster traffic | brief §2, 2.5; V10; [detect.md](detect.md) §2.8 | §2.1 |
| `ufw` is installed in `/usr/sbin` (inside `host.SearchPath`) | **[unverified]** ([system.md](system.md) §3) | every run |

## 4. Written, not run (gate 5)

No `ufw` has run. On the Debian 13, Ubuntu 26.04 and Alpine 3.24 VMs (`next/p2-vmtest`) and an Ubuntu 24.04 host, as root:

- `ufw status verbose` and `user.rules`/`user6.rules` on each, stock and after `ufw allow OpenSSH` (Ubuntu's profile), `ufw limit 22/tcp`, `ufw allow from 192.0.2.0/24 to any port 22 proto tcp`, `ufw deny 2230/tcp` and an `in on eth0` rule: every row read (`Bad` empty) and the same verdicts from the status and the saved rules;
- the brief's 2230 scenario, ufw step first: `ufw allow 22/tcp`, `ufw allow 2230/tcp`, `ufw --force enable` with an SSH session open; **the session survives**, new connections to 22 and 2230 are accepted over IPv4 and IPv6, `Execute` reports `Verified`; then the sshd change and reload (`p2-reload`), a new connection on 2230, `Rollback` restoring the inactive firewall and no rules;
- on Ubuntu 26.04, `ssh.socket`'s dual-stack `[::]:22`: an IPv4 connection accepted once the IPv4 rule is there;
- `ufw default deny incoming` on an active ufw with an SSH session open (ufw stops and starts the firewall): the session survives;
- each answer `undo` reads ("Rule added", "Rules updated", "Skipping adding existing rule", with " (v6)"), and `ufw delete allow 22/tcp` running without a prompt;
- `IPV6=no` with sshd on `[::]:22`: the refusal, and that ufw does drop IPv6 once started;
- a declined enable: `ufw enable` without `--force` under SSH with stdin on `/dev/null` (expected: "Aborted", exit 0), and whether OpenSSH ≥ 9.8's `sshd-session` process name defeats ufw's `under_ssh` check (irrelevant to `--force`);
- Alpine: ufw from the community repository; whether ufw comes back after a reboot without `rc-update add ufw default` (expected: not, §6);
- Proxmox VE 9 by hand: no `ufw` command in the run log.

## 5. API

```go
package ufw

const DefaultsFile, UserRules, User6Rules, KernelIPv6 = "/etc/default/ufw", "/etc/ufw/user.rules", "/etc/ufw/user6.rules", "/proc/sys/net/ipv6"

var ErrNotApplicable, ErrProxmoxUnknown, ErrNotInstalled, ErrListenersUnknown, ErrStateUnknown, ErrBlocks, ErrVerify error

func Applies(pve detect.Proxmox) error // nil, ErrNotApplicable, or ErrProxmoxUnknown

type Policy string // PolicyAllow, PolicyDeny, PolicyReject, PolicyDisabled; "" not established
type Family uint8  // IPv4 = 4, IPv6 = 6
type Rule struct { Action, Dir, Proto, Dst, DPort, Src, Iface string; Family Family; Line string }

type Status struct { Known, Active bool; Logging string; Incoming, Outgoing, Routed Policy; NewProfiles string; Rules []Rule; Bad []string }
type State struct { Installed bool; Status Status; IPv6 bool; Incoming, Outgoing Policy; Rules []Rule; Bad []string } // Known() bool
func Read(ctx context.Context, sys host.System, pve detect.Proxmox) (State, error)
func ParseStatus(out []byte) Status
func ParseTuples(data []byte, fam Family) (rules []Rule, bad []string)

type Need struct { Port uint16; Family Family; Addr netip.Addr; New bool; Why string }
func Needs(ls sshd.Listeners, add, keep []uint16) []Need
type Verdict uint8 // VerdictUnknown (zero), Allowed, Limited, Restricted, Blocked, Denied; OK() bool
type Coverage struct { Need Need; Verdict Verdict; By string }
func Evaluate(rules []Rule, active, ipv6 bool, incoming Policy, needs []Need) []Coverage // pure
type FindingKind uint8 // FindingNotAllowed "ssh-not-allowed", FindingRestricted "ssh-restricted", FindingUnknown "ufw-unknown"
type Finding struct { Kind FindingKind; Coverage Coverage; Incoming Policy }
func Check(st State, needs []Need) ([]Coverage, []Finding)

type Desired struct { Enable bool; Incoming, Outgoing Policy }
type Input struct { Proxmox detect.Proxmox; Listeners sshd.Listeners; Add, Keep []uint16; State State; Desired Desired }
type StepKind uint8 // StepAllow, StepDefault, StepEnable
type Step struct { Kind StepKind; Argv, Undo []string; Port uint16; Why string }
type Plan struct { NotApplicable bool; Steps []Step; Needs []Need; Before, After []Coverage; Active bool; Incoming Policy; IPv6 bool; Findings []Finding } // Empty() bool
type BlockError struct { Coverage []Coverage } // Unwrap: ErrBlocks
func PlanFor(in Input) (Plan, error) // pure

type StepReport struct { Step Step; Result host.Result; Undo []string; Note string }
type Report struct { Steps []StepReport; Undo [][]string; Status Status; Coverage []Coverage; Verified bool; Unverified string }
func Execute(ctx context.Context, sys host.System, p Plan) (Report, error)
func Rollback(ctx context.Context, sys host.System, rep Report) error
```

## 6. Not in this unit; inputs to later units

- **`p3-engine`:** the order (brief 2.3): `Read` and `PlanFor` at plan time, so a refusal (`*BlockError`, `ErrListenersUnknown`) shows in a dry run; `Execute` before the sshd change is written; any error from it means `Rollback` under `context.WithoutCancel` while the session is open. Fill `Input.Keep` with the current session's server port (`SSH_CONNECTION`'s fourth field) when hardn runs over SSH: the listener set is configuration, so a port the running daemon still holds from before an unapplied change is not in it ([sshd-effective.md](sshd-effective.md) §2.5, §6). `reload.ReadBound` is not used to add rules: `ss -Htln` does not say which program holds a socket, and allowing every bound port would open other services. Whether hardn adds an allow rule the user did not ask for when the UFW SSH policy is off (`enableUfwSshPolicy`, V8's open approach) is the engine's and the maintainer's call; this unit only plans what it is asked to.
- **After the confirmation (brief 2.3 step 5):** dropping the old port's rule is not planned here. It is the clean-up change's: run the `ufw delete allow <port>/tcp` of the step that added it (`Report.Undo`), which exists only when hardn added that rule.
- **`p2-revert`:** the journal's inverse commands are `Report.Undo`; the change's risk class is lockout (firewall).
- **`p3-output`:** `ErrBlocks`, `ErrVerify` (`UFW_VERIFY_FAILED` in the plan's example), `ErrNotInstalled`, `ErrNotApplicable` and the finding codes (§2.16) to registry codes.
- **`p4-check-firewall` / `p4-risk`:** `Check(state, Needs(listeners, nil, nil))` for the status; "default deny and a listener not allowed" is a `FindingNotAllowed` with `Incoming` deny. On Proxmox VE the firewall status is `p2-pve`'s (pve-firewall, read-only).
- **`p2-alpine` / the firewall slice:** on OpenRC `ufw enable` does not add ufw to a runlevel (v0.3.2 ran `rc-update add ufw default`; origin/main does not; [side-effects.md](../refactor/phase0/side-effects.md) §4.4, §6), so it is not started at boot **[I]**; installing ufw is [P-16](../refactor/proposals.md#p-16).
- **The legacy firewall code** (`pkg/adapter/secondary/ufw_firewall_repository.go`, `pkg/application/firewall_manager.go`, `pkg/domain/service/firewall_service.go`) is untouched: its sequence (V9) stays until the firewall slice moves onto this package, and `next/p0-hazards` gates it meanwhile.
- **Not modelled:** rules outside ufw's user lists (`before.rules`, `after.rules`, other iptables or nftables users such as Docker); routed rules; a `limit` rule's rate limit (counted as let through); sshd's `rdomain`; a socket's `BindIPv6Only=ipv6-only` (a socket `[::]` is taken as dual-stack).
- **Size:** about 1,090 lines of Go outside tests (about 160 of them whole-line comments) and 950 of tests, against the plan's guideline of about 500: the unit reads two forms of the same rules (the status for what is in force, the saved tuples for an inactive ufw), evaluates them as ufw's chains do, and its executor reads ufw's answers so that an undo never deletes a rule it did not add.
