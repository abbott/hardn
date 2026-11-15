# SSH port, ufw sources carried: design

Status: **adopted 2026-11-15**, `next/ssh-port-sources`: the maintainer, "Adopt all", every recommended (a) of §9's decisions 1 to 8. Built and tested in process (§10); run on a host 2026-11-15 (§12); what is still open is §11. The maintainer chose the fix the same day: "carry sources", hardn adds each source's rule for the new port itself. This replaces [P-33](../refactor/proposals.md#p-33) (2) (a), refuse. §9 (9), a throwaway host, is still open.

## 1. The bug

Where ufw lets the SSH port in from some sources alone, Change SSH port is refused (`SSH_PORT_SOURCE_RESTRICTED`): "ufw lets port 22 in from 10.2.8.0/24, 172.20.8.0/24 alone. A new port would be open to anyone. Move that rule to the new port by hand first."

It can't be satisfied. The check reads the port sshd is on now and nothing else, so the same rules added for 2230 by hand change nothing (seen on the maintainer's Debian 13 host: both sources on 22 and on 2230, still refused), and taking them off 22 would cut the session.

## 2. What is there today

| Step | Where |
|---|---|
| `Read` asks ufw for the sources of the first port sshd listens on: `Reading.OnlyFrom` | `pkg/engine/controls/sshchange/sshchange.go:153-163`, `pkg/host/ufw/source.go:16` |
| `Port.Plan` refuses on them before the new port is looked at | `plan.go:301-311` (`portRefusal`), `:485`; words `:44` |
| The screen plans the port sshd has first, so the refusal comes before the field | `pkg/screens/ssh/actions.go:190` |
| Without the refusal: `ssh/firewall` (`ufw allow 2230/tcp`, from anywhere), `ssh/files` (00-hardn.conf: `Port 22`, `Port 2230`), `ssh/reload`, `ssh/verify`, in that order, all lockout-risk | `pkg/engine/steps/ssh.go:132-164`, `firewall.go:49`, `:112` |
| ufw adds a rule only for a port that is Blocked, and never where the allow would change a saved rule | `pkg/host/ufw/plan.go:285`, `:309` |
| A new port left Restricted refuses the plan (`UFW_WOULD_BLOCK`) | `plan.go:219` |
| A rule is undone only where ufw answered "Rule added" | `pkg/host/ufw/exec.go:104` |
| The revert timer; `hardn confirm` from a new connection to 2230 | `steps/ssh.go:382` (`ConfirmVia`) |
| On confirm: 22 dropped from 00-hardn.conf, sshd reloaded. A ufw rule for 22 is deleted only where hardn added it in that change (`ufw delete allow 22/tcp`). A rule of the user's stays | `steps/cleanup.go:56-77`, `:218-236` |

So two things stop the host of §1: the refusal, and behind it `plan.go:219`. The new port's need is "from anywhere" (`pkg/host/ufw/cover.go:75`), and the hand-made rules leave that Restricted. §3 "What the plan is judged on" is how the second goes.

The reader drops what a copy needs: `ParseTuples` cuts a rule's comment and log type (`pkg/host/ufw/read.go:186`, `:193`), and joins a source port and a source application into `Rule.Src` (`:202-211`). ufw saves a comment as hex (` comment=6f6666696365` is "office"), so the reader must hex-decode it before it is passed to `comment`, as one argument.

**firewalld (EL).** No dead end. The new port is added to each zone that lets the current port in (`pkg/host/firewalld/firewalld.go:339`), and a zone's sources and interfaces are the zone's own, so they carry. SSH let in by a rich rule alone is refused (`FIREWALLD_NO_SSH_PATH`), and its fix can be done. Out of scope.
**Proxmox VE.** The port stays 22 (`PVE_PORT_CHANGE_REFUSED`), by design. Out of scope.

## 3. The new plan

**Each IP version on its own.** ufw keeps IPv4 and IPv6 rules apart (`Rule.Family`), and the plan is worked out for each version ufw writes (`writes`, `plan.go:274`): IPv4, and IPv6 where ufw's IPv6 is on.

A **source rule** is a saved ufw rule, inbound, allow or limit, tcp or any protocol, whose ports include a port sshd listens on now, and that has a source, an interface or a "to" address. A rule **from anywhere** has none of the three.

| In one IP version, 22 has | 2230 in that version |
|---|---|
| a rule from anywhere, source rules beside it or not | from anywhere |
| source rules alone | each carried, as the table below |
| no rule that lets it in | nothing added, where the other version carries (a rule there for 2230 already warns, §6); else as today |

Where no version has source rules alone, nothing changes: `ufw allow <new>/tcp`, as today. That command writes every version, so it is planned only there. Where one version is from anywhere and the other carries, the first gets a rule for that version alone: `ufw allow from 0.0.0.0/0 to any port 2230 proto tcp` (or `from ::/0`). So 22 open to anyone on IPv4 and to 2001:db8:8::/64 alone on IPv6 never opens 2230 to anyone on IPv6. ufw inactive: no rule is saved, as today (P-33 (1) (b)).

For each source rule, one command for the new port, in the rules' order:

| Rule on 22 | Command for 2230 |
|---|---|
| a source | `ufw allow from 10.2.8.0/24 to any port 2230 proto tcp` |
| a source on an interface | `ufw allow in on eth0 from 10.2.8.0/24 to any port 2230 proto tcp` |
| anywhere on an interface, both versions | `ufw allow in on eth0 to any port 2230 proto tcp` |
| anywhere on an interface, one version | `ufw allow in on eth0 from 0.0.0.0/0 to any port 2230 proto tcp` (or `from ::/0`) |
| an IPv6 source | `ufw allow from 2001:db8:8::/64 to any port 2230 proto tcp` |
| a limit | `ufw limit from 10.2.8.0/24 to any port 2230 proto tcp`: the action is kept |
| a "to" address | `ufw allow from 10.2.8.0/24 to 10.2.8.5 port 2230 proto tcp` |
| a range or list (`20:30/tcp`, `22,80/tcp`) | the same source, port 2230 alone; the rule itself is left |
| any protocol (`port 22`) | `proto tcp`: SSH needs no UDP |
| no port (`ufw allow from 10.2.8.0/24`) | nothing: it lets 2230 in already |
| a comment, a log type | copied, the log type after the action and interface: `ufw allow in on eth0 log from 10.2.8.0/24 to any port 2230 proto tcp comment 'office'` |
| an application profile (`to any app OpenSSH`) | under §9 (4) (a): the same source as a plain port rule |

**One command, one rule.** A command with no address writes both versions. It is used only where 22 has the rule in both versions and 2230 has it in neither. Else the address is pinned (`0.0.0.0/0` or `::/0`), so ufw's answer is for one rule and its undo is exact. Otherwise ufw could answer "Skipping adding existing rule" and "Rule added (v6)" to one command, nothing would be journalled, and the IPv6 rule would stay after a revert.

Not carried:

- **Deny and reject rules.** One with no port covers the new port already. One that names port 22 is not copied: it must sit before the allow that would undo it, and hardn only appends. §9 (5).
- **A source port or a source application.** §9 (2).

**Already there.** A rule for the new port counts as there only where it matches as ufw itself matches rules (`UFWRule.match`): the same IP version, port 2230, protocol tcp (or any, which covers tcp), source and "to" address, source port, applications, interface and direction. A udp rule, or one in the other IP version, is not it: the rule is added. One that is there is never repeated or changed: an allow or limit counts; a deny refuses (`UFW_WOULD_BLOCK`).

**What the plan is judged on.** Today the new port's need is "from anywhere", which source rules leave Restricted, so `plan.go:219` refuses (`UFW_WOULD_BLOCK`). In a version that carries, that need is replaced: one need per carried source rule (a `Need` gains a source), each to be let in on 2230 by the rules as they will be, and for a remote session its client address (§5 (2)). `allowPorts` and `changes` are not asked about that port in that version: they know `ufw allow <port>/tcp` alone. So `plan.go:219` judges the carried rules, not open-to-all. This is what lets the hand-prepared host through: both sources are let in on 2230 already, every need holds, and there is no step.

## 4. The review

The host of §1, with 2230 not yet in ufw:

```
   -  SSH Port             22 held until confirmed

   ⭘  Lockout risk
      ufw: allow 2230/tcp from 22's sources

      ufw
      $ ufw allow from 10.2.8.0/24 to any port 2230 proto tcp
      $ ufw allow from 172.20.8.0/24 to any port 2230 proto tcp

   ⭘  Lockout risk
      Add SSH port 2230

      /etc/ssh/sshd_config.d/00-hardn.conf
      @@ -0,0 +1,4 @@
      +# Managed by hardn.
      +# Set by hardn:
      +Port 22
      +Port 2230

   ⭘  Lockout risk
      Reload sshd (ssh.service)

      ssh.service
      $ systemctl reload ssh.service

   ⭘  Lockout risk
      sshd -T shows ports 22, 2230

      sshd -T
      $ sshd -T

  Change the SSH port?
```

Where hardn-boot.service is not installed yet, "Install hardn's boot unit" comes first, as in the review today (`internal/tools/uigallery/testdata/golden/play/ssh-port.golden`).

The host as it is (both sources on 2230 by hand): the same review with no ufw step (§3, "What the plan is judged on"). With one of the two there: the ufw step holds the one missing command.

## 5. Order and safety

1. **Before sshd moves.** The ufw step is `ssh/firewall`, `StageFirewall`, where `ufw allow 2230/tcp` is today.
2. **The session's own source.** Under §9 (3) (a). hardn has it: `engine.Input.Session.Conn` is `SSH_CONNECTION`, or the same four values from the kernel's socket table (client address and port, server address and port; `pkg/host/revert/session.go:24-47`), which the ufw planner already reads for the session's port (`steps/firewall.go:65-68`). The rules as they will be must let that client address in on the new port, to that server address: first match, as ufw's chains. Else refused (§7).
   - **Loopback.** ufw accepts everything on `lo` before any user rule (before.rules, `-i lo -j ACCEPT`). A client at 127.0.0.1 or ::1 (a local tunnel, a port forward) is let in already: no check, no refusal.
   - **A rule on an interface.** `in on eth0` is the interface the packet arrives on, not the one that holds the server address. hardn reads the route back to the client (`ip route get <client address>`, its `dev`; a new read) and the rule counts where that is its interface. A `dev lo` there is loopback, as above. Under asymmetric routing the way back is not the way in, and the check can be wrong either way: then the window (3) is what holds, and the change reverts.
   - **A console session** has no check.
3. **The window.** Unchanged: 22 and its rules stay, the revert timer runs, `hardn confirm` must come from a new connection to 2230, which also proves a carried source reaches it.
4. **Rollback.** 00-hardn.conf is restored, sshd reloaded, then each rule hardn added is deleted. 22's rules were never touched.
5. **The delete.** `ufw delete` and the add's own words, the comment alone dropped: `ufw delete allow from 10.2.8.0/24 to any port 2230 proto tcp`. The action (`limit`) and the log type (`log`, `log-all`) are repeated as added: ufw's delete compares them too, and a delete without them leaves the rule behind. A delete with no comment matches a rule that has one. Never by number: numbers shift. It is journalled only where ufw answered "Rule added" and nothing else, so a rule that was there before is never deleted (`exec.go:104`, unchanged). That holds because ufw is active: inactive, ufw answers "Rules updated" for any saved change, a replaced rule too, and `exec.go:51` counts that as added. §3 saves nothing while ufw is inactive.
6. **On confirm.** 22 leaves 00-hardn.conf and sshd as today. What happens to 22's ufw rules is §9 (1).

## 6. The new port has rules already

"Has" is §3's "Already there": ufw's own match, per IP version.

| 2230 has | Plan |
|---|---|
| every source rule of 22 (the host of §1) | nothing to add; go on |
| some of them | add the missing ones |
| a rule from anywhere, or a source 22 doesn't have | under §9 (6) (a): left as it is, with a warning |
| a deny for a carried source | refused, `UFW_WOULD_BLOCK`, as today |

## 7. The refusal

"A new port would be open to anyone" goes. What still refuses, under §9 (2) (a), (3) (a) and (7) (a), each before the port is asked, since none depends on the new port:

| Case | Code | Words (proposed) |
|---|---|---|
| a rule hardn can't copy; gone under §9 (2) (b) | `SSH_PORT_SOURCE_RESTRICTED`, kept | "hardn can't copy this ufw rule to a new port: 22/tcp from 10.2.8.0/24 port 1024. Replace it with a rule by address, then try again." |
| the session's source in no carried rule; gone under §9 (3) (b) | `SSH_PORT_SESSION_SOURCE`, new | "No ufw rule for port 22 covers 10.9.0.4, where you're logged in from. You couldn't reach the new port. Add a rule for it first." |
| a remote session's address not read; gone under §9 (3) (b) | the same | "hardn can't tell where you're logged in from, so it can't tell ufw would let you in on a new port. Run hardn from a console." |

Each can be met by a ufw rule on the current port, with the session open. The third is rare: it fires only where both `SSH_CONNECTION` and the socket table give nothing. A loopback client is never refused (§5 (2)).

## 8. After the change

- **Firewall check and screen.** The SSH finding stays `UFW_SSH_RESTRICTED`, Partial, "ssh allowlist", as it is on such a host now. Ports lists 2230 as let in from some sources. Limit SSH offers nothing before or after: it replaces a plain allow only (`pkg/host/ufw/limit.go:42`).
- **`hardn check`.** `ssh.ports`, `ssh.listeners`, `firewall.rules` and `firewall.ports` differ until `hardn check --accept`, as after any port change.
- **Run All and hardn.yml's `sshPort`.** Nothing to do: Run All compares `sshPort` with sshd's listeners and never plans a port (`pkg/engine/runall/runall.go:535`), and no flag changes one. The screen's item is the only caller of `sshchange.Port`. A later caller gets the same plan and refusals, with `Input.Session` as its session.

**Tests.** `pkg/host/ufw`:

- one table case per row of §3's two tables, and each "not carried";
- each IP version: 22 from anywhere on IPv4 and sources alone on IPv6, and the other way round (the pinned rule, 2230 never open to anyone in the carrying version); sources on IPv4 and no rule on IPv6; an interface rule saved for one version;
- the rules already there, whole and in part; a udp-only rule and a rule in the other IP version for the same source (not "there": the tcp rule is added);
- a deny on the new port; a deny on 22 not carried, with 2230 let in from its source (the finding of §9 (5) (a));
- the session let in, not let in, by interface (the route's `dev`), from loopback;
- the reader: a comment hex-decoded, one non-ASCII and one with a quote; a log type;
- the undo from each of ufw's answers, a limit and a log type kept in the delete; the second plan empty.

`sshchange`: the three refusals, and no refusal on the host of §1. `steps`: the journal's undo; the clean-up under §9 (1). Gallery: `ufw-restricted` plans; a host with the rules there by hand; goldens.

**The host run.** A throwaway Debian 13 or Ubuntu VM with ufw, reached from an address inside a rule: lockout is the risk under test. Never a standing VM without his word. Cases: the two-source host, confirmed from a new connection; the same, left to revert (2230's rules gone, 22's as before); the rules there by hand; one of two; a limit; an interface rule; the session's address in no rule.

**Not verified** when this was written (the host run, §12, settled these but `from ::/0` and the `dev` on a second interface: §11):

- ufw's answer to a `from` rule, one line per rule;
- that `from 0.0.0.0/0` (and `from ::/0`) writes one IP version alone;
- where ufw takes the log word (after the action and interface, by its man page);
- that `ufw delete` with the add's words removes a rule that has a log type;
- that the `dev` of `ip route get` is the interface an `in on` rule matched for the session.

## 9. Decisions for the maintainer

1. **22's own ufw rules once the change is confirmed.** *Gates the clean-up (§5 (6)), its tests and the host run.* Today hardn deletes a rule for the old port only where it added it; a rule of yours for 22 stays after the port moves. (a) *Recommended:* the same here: 22's source rules stay, with nothing listening on 22, and you delete them when you like. hardn never deletes a rule it did not add. (b) `hardn confirm` deletes them, each by its exact words, a range, list or profile rule left: the firewall ends clean, and hardn deletes rules you wrote.
2. **A source rule hardn can't copy** (a source port, a source application). *Gates §7's first refusal, and (7) and (8).* (a) *Recommended:* refuse the change, naming the rule: the new port gets every source or none. (b) Leave that rule out and warn, where the session's source is in another: the change goes on, and someone that rule let in can't reach the new port.
3. **The session's source in no carried rule, or not read.** *Gates the session check (§5 (2)), §7's other two refusals, and (7) and (8).* (a) *Recommended:* refuse (§7): the change could only revert. (b) Warn and rely on the revert timer: no session check and two fewer refusals, and a window that locks you out until it ends.
4. **A source rule on an application profile** (`from 10.2.8.0/24 to any app OpenSSH`). *Gates one row of §3's table and its test.* (a) *Recommended:* carry its source as a plain port rule for the new port: same source, and the usual Debian form doesn't refuse. (b) Refuse, naming the rule: only like-for-like copies, and that form must be replaced by hand first.
5. **A deny that names port 22** (`deny from 10.2.8.9 to any port 22` before the allow). *Gates a new trigger for a finding, and its test.* (a) *Recommended:* not carried; the review warns that 2230 lets in more than 22 (`UFW_SSH_NEW_PORT_WIDER`). That finding fires today only where the plan lets the new port in from anywhere (`FindingWidened`, `pkg/host/ufw/plan.go:343-356`), so it needs a second trigger: a deny on the current port that was not carried, with the new port let in from the deny's source. (b) Refuse. (c) Carry it too, added before the allows: exact, and more rule forms to get right.
6. **A new port already open wider** (a rule of yours from anywhere, or from a source 22 doesn't have). *Gates §6's third row.* (a) *Recommended:* leave it, go on, and warn in the review: the rule is yours. (b) Refuse until it is narrowed: the move can never widen SSH, and one more refusal.
7. **The code.** *Depends on (2) and (3); gates nothing else.* Under (2) (b) nothing refuses as "a rule hardn can't copy", so `SSH_PORT_SOURCE_RESTRICTED` has no case left and is retired. Under (3) (b) there is no `SSH_PORT_SESSION_SOURCE`. Under (2) (a) and (3) (a): (a) *Recommended:* `SSH_PORT_SOURCE_RESTRICTED` stays for "a rule hardn can't copy", and `SSH_PORT_SESSION_SOURCE` is new. (b) Retire the old code for a new one: a script matching it stops matching.
8. **Words.** *Depends on (2) and (3); gates nothing else, a build-time detail.* The review's title "ufw: allow 2230/tcp from 22's sources" and §7's refusals: the first only under (2) (a), the other two only under (3) (a). (a) *Recommended:* as written. (b) Yours.
9. **A throwaway host for the run.** A Debian 13 or Ubuntu VM with ufw, by NAME and NAME-root. *Blocks the host run, not the build.*

## 10. Built

**What.** Where an active ufw lets a port sshd listens on in from source rules alone, in an IP version ufw writes, Change SSH port copies each rule to the new port before sshd moves, and is no longer refused for it. Decisions 1 to 8 (a) as adopted.

**Where.**

| Part | Code |
|---|---|
| The plan for each IP version, "already there", the session check, the two warnings, `ip route get` | `pkg/host/ufw/carry.go` |
| `PlanFor` uses it where a port is added and ufw is active; `Input.Session`; `StepAllowFrom` | `pkg/host/ufw/plan.go` |
| A need of some connections alone (`Need.From`, `To`, `Iface`, `Like`, `Session`) | `pkg/host/ufw/cover.go` |
| The reader: comment hex-decoded, log type | `pkg/host/ufw/read.go` (`Rule.Comment`, `Rule.Log`) |
| The session for the planner, the step's title | `pkg/engine/steps/firewall.go` (`UFWSession`, `ufwTitle`) |
| The refusals before the port is asked, their words and codes | `pkg/engine/controls/sshchange` (`Reading.CarryErr`, `portRefusal`, `carryRefusal`) |
| `SSH_PORT_SESSION_SOURCE`; `ufw.ErrCantCopy` under `SSH_PORT_SOURCE_RESTRICTED` | `pkg/output/codes.go` |
| Mocks: `ssh/ufw-restricted`, `ufw-two-sources`, `ufw-sources-part`, `ufw-sources-there`, `ufw-port-wider`, `ufw-deny-22`, `ufw-source-port`, `ufw-session-out` | `internal/tools/uigallery`, goldens `play/ssh-port-sources*`, `ssh-port-wider`, `ssh-port-deny`, `ssh-port-source-port`, `ssh-port-session-out` |

The order of steps is unchanged: `ssh/firewall` (now the copies), `ssh/files`, `ssh/reload`, `ssh/verify`, all lockout-risk, then the window. Execute, its undo by ufw's answer, the journal and the rollback are the code that was there. The clean-up is untouched: it deletes only `ufw delete allow <port>/tcp` for a dropped port, which no carried rule is.

**Tests.** `pkg/host/ufw/carry_test.go`: `TestCarryRows` (each row of §3's and §6's tables, each IP version, the host of §1 whole and in part, udp and other-version rules, the warnings), `TestCarryRefuses`, `TestCarrySession`, `TestRouteDev`, `TestParseTuplesCarried`, `TestCarryExecute` (second plan empty, rollback), `TestCarryUndo` (each answer, limit and log kept, a rule of the user's left), `TestCarryFailsBetweenSteps`, `TestCarryWiderWords`. `sshchange`: `TestRefusals` (the three, in their words and codes), `TestPortCarries`, `TestApplyRollBack` (ufw's files byte for byte after the rollback). `steps`: `TestCleanupOf`, the title. `pkg/screens/ssh`: `TestRefusals`, `TestPortSources`.

**Choices made here**, where the page was silent:

1. **A rule with no port** counts as a source rule, so its IP version carries, and is never copied. Else such a host would be refused `UFW_WOULD_BLOCK`.
   *2026-11-15, after a host refused:* where the current port is open to anyone, see "A rule with no port beside an open SSH port" below.
2. **A rule that names the new port too** (`22:3000/tcp`) gets no copy.
3. **Before the port is asked, the session is judged on a port no rule names.** A deny of the new port itself for the session's address refuses at the field, `UFW_WOULD_BLOCK`.
4. **The session check runs wherever one IP version carries**, also for a session in a version that is open or has no rule.
5. **Denies.** A deny that matches a carried source on the new port refuses, unless it matches it on the current port too, before that port's rule. So a deny with no port placed after 22's allow refuses: appended rules would fall behind it.
6. **A new port open wider already** still gets the copies, with the warning. Wider is judged by what the rule lets in: a rule 22's rules cover (`10.1.0.0/16` under 22's `10.0.0.0/8`) warns nothing. An IP version with no rule for 22 warns for any allow of the new port.
7. **An uncarried deny warns** only where a rule for 22 after it, or a default policy of allow, lets its source in on the new port. `deny 22` after the allows warns nothing. So too in a version where 22 is from anywhere and 2230 gets the pinned rule.
8. **The review leaves out 22's own findings** (`UFW_SSH_RESTRICTED`, and "not allowed" in an IP version with no rule): they are what is carried. The Firewall check reports them as before.
9. **A "to" address with no source:** `ufw allow to 10.2.8.5 port 2230 proto tcp`.
10. **The comment** is one argument; the review shows it as hardn shows any argument (`comment office`, `comment "my office"`), not in single quotes.
11. **A comment that is not hex** is a line not read: `UFW_STATE_UNKNOWN`.
12. **An interface rule where the route is not read** covers nothing of the session: refused, "No ufw rule for port 22 covers ...".
13. **sshd on several ports:** the rules of each are carried, each source once. The session refusal names the first port.
14. **The warnings' words:** `ufw has "allow 2230/tcp from anywhere" already, which port 22 has not: left as it is`; `"deny 22/tcp from 192.0.2.99" is not copied from port 22: port 2230 lets that in`.
15. **A default policy of allow** changes nothing: the rules are still carried.
16. **`SSH_PORT_SOURCE_RESTRICTED`'s sentinel text is kept** ("ufw lets the SSH port in from some sources alone"); the registry's one line says "by a rule hardn can't copy".

**A rule with no port beside an open SSH port** (2026-11-15). Seen on an Alpine 3.24 host: ufw active with `22/tcp` from anywhere on IPv4 and IPv6, and `ufw allow from 10.0.0.0/8`, which names no port. Change SSH port to 2230 was refused `UFW_WOULD_BLOCK`: on IPv4 the new port read as let in from 10.0.0.0/8 alone, and hardn adds no `ufw allow <port>/tcp` where that would widen a restricted rule (ufw.md §2.8). But 22 is open to anyone there, so opening 2230 to anyone widens nothing about SSH.

Now, in the SSH port change on an active ufw, a new port gets `ufw allow <port>/tcp` in spite of that, where in the IP version:

- each port sshd is on now is let in from anywhere, by an allow or a limit (a limit counts as open, as it does in `sourceRules`; the new port's rule is a plain allow, as on any host with `limit 22/tcp`); and
- every rule that matches the new port is an allow or limit that names no port and no application.

Hands stay off, as before, for: a rule that names the new port, alone, in a range or list, or by an application; any deny or reject that matches it, with a port or without; a rule not read; a version where the current port is not open to anyone (the carry, or the refusal); and every plan that adds no port (enabling ufw, the Firewall screen), where the allow would open SSH itself. With ufw inactive nothing changes either. `allow 2230/tcp` is its own tuple and replaces no rule; its undo is the usual `ufw delete allow 2230/tcp`, only where ufw answered that it added it.

Code: `everyPort`, `changes`, `allowPorts` in `pkg/host/ufw/plan.go`; `PlanFor` passes the current ports only where ufw is active and a port is added. Tests: `pkg/host/ufw/everyport_test.go`. Mock `ssh/ufw-any-port`, golden `play/ssh-port-any-port`.

Not changed: the carried plan. Where one IP version carries and another has 22 open beside a rule with no port, the open version's new port still gets no rule and the change is refused. Not run on a host.

The refusal's words (`*BlockError`) now name a saved rule as the warnings do (`ruleText`): `restricted by "allow any port from 10.0.0.0/8"`, not ufw's saved line `### tuple ### allow any any 0.0.0.0/0 any 10.0.0.0/8 in`. Where another rule keeps hands off beside the rule with no port (a deny, a rule that names the port), the refusal names it: `restricted by "allow any port from 10.0.0.0/8; no rule added: "ufw allow 2230/tcp" would change "allow 2230/tcp from 10.0.0.0/8""`.

Run All and hardn.yml's `sshPort`: nothing built, as §8 says. Run All's port row reads `sshchange.Read` and plans no port (`pkg/engine/runall/runall.go`, `Reading.port`).

**Not done.** No gallery mock of "hardn can't tell where you're logged in from": the gallery's engine gives a remote session with no connection the stock one (`sshEngineSession`). `sshchange`'s `TestRefusals` covers it.

## 11. Not verified

Settled by the host run (§12): ufw's answer to a `from` rule; `from 0.0.0.0/0` writes IPv4 alone; the log word goes after the interface; `ufw delete` with the add's words removes a rule with a log type, a limit, an interface, a comment; `to <address> port <port> proto tcp` with no `from`; the check after the step reads rows with an interface, a log type and a comment.

Still open:

- that `from ::/0` writes IPv6 alone (only `from 0.0.0.0/0` ran);
- the `dev` of `ip route get` on a host with more than one interface: the host has eth0 alone, so the route and the rule could only agree;
- a session over IPv6: the host has no IPv6 route;
- §3's rows that did not run on a host: an application profile, a range or list, any protocol, a rule with no port;
- the two warnings (a new port open wider, a deny not copied) and a deny on the new port;
- "hardn can't tell where you're logged in from";
- Ubuntu, and any ufw other than 0.36.2-9.

## 12. On a host

2026-11-15, `deb13-test-105` (alias `deb13-vm`, 10.2.8.135): Debian 13, x86_64, ufw 0.36.2-9 active, IPv6 rules on, client 10.2.8.2. The binary of this branch, run as `sudo` from an SSH session, port 22 to 2230. C is 10.2.8.0/24, X is 172.20.8.0/24. The window was 180 s in cases 1 and 2, 60 s (`--confirm-timeout 60`) after. "Exact" is `ufw show added` and the cksum of user.rules and user6.rules, before and after.

| Case | Result |
|---|---|
| 1. 22 from C and X; confirmed from a new connection to 2230 | Pass. Two commands in the review, "Rule added" each. In the window sshd on 22 and 2230; after `hardn confirm`, 2230 alone. 2230 has both rules, 22's are unchanged |
| 1b. Back from 2230 to 22, both ports' rules there; confirmed from 22 | Pass. No ufw step; sshd on 22 alone; no rule touched |
| 2. The same start, not confirmed | Pass. Rolled back at 180 s: both 2230 rules deleted ("Rule deleted"), 22's exact, sshd on 22, the session open |
| 3. Both sources on 2230 by hand | Pass. No ufw step. After the revert the rules are exact |
| 4. C on 2230 by hand | Pass. One command (X). After the revert X's rule is gone, C's stays, exact |
| 5. `limit` from C; X with comment 'my office' | Pass. `ufw limit from 10.2.8.0/24 to any port 2230 proto tcp` and `ufw allow from 172.20.8.0/24 to any port 2230 proto tcp comment "my office"`; ufw saved the comment whole. The revert deleted both (`delete limit ...`; `delete allow ...` with no comment). Exact |
| 6. The session's source in no rule (C's rule taken off 22 inside the session, put back after) | Pass. Refused at the menu item: "No ufw rule for port 22 covers 10.2.8.2, where you're logged in from. You couldn't reach the new port. Add a rule for it first." Nothing changed |
| 7. C, and C with source port 5000 | Pass. Refused at the menu item: "hardn can't copy this ufw rule to a new port: 22/tcp from 10.2.8.0/24 port 5000. Replace it with a rule by address, then try again." Nothing changed |
| 8a. `in on eth0` from C; `in on eth0 log` from X, comment 'office' | Pass. `ufw allow in on eth0 from ...` and `ufw allow in on eth0 log from ... comment office`, "Rule added" each; status shows `2230/tcp on eth0`, `(log) # office`. The revert's deletes, with `in on eth0` and `log`, removed both. Exact |
| 8b. 22 from `0.0.0.0/0`; 2001:db8:8::/64 on IPv6 | Pass. `ufw allow from 0.0.0.0/0 to any port 2230 proto tcp` ("Rule added": IPv4 alone, no v6 row) and the IPv6 source ("Rule added (v6)"). The revert deleted both ("Rule deleted", "Rule deleted (v6)"). Exact |
| 8c. `to 10.2.8.135 port 22 proto tcp`, no source | Pass. `ufw allow to 10.2.8.135 port 2230 proto tcp`, added and deleted by the revert. Exact |

Seen on the way:

- ufw answered one line to each command, as the fakes do. Nothing differed from what §11 assumed.
- `ufw show added` lists the pinned rule as `ufw allow 2230/tcp`, as it lists `from 0.0.0.0/0` on 22 as `ufw allow 22/tcp`. The delete by `from 0.0.0.0/0` still matched it.
- The refusals show their words, not their codes.
- A binary a user can change is refused once the port is typed: "engine: the revert timer: revert: /home/tmp-38c802/hardn-ps: a user other than root can change it: not safe to run as root from the timer." The run used a root-owned copy.
- 1b's `hardn confirm` printed systemd's "Warning: The unit file, source configuration file or drop-ins of hardn-revert-....timer changed on disk. Run 'systemctl daemon-reload' to reload units." once. The cause was not looked at.

The host was put back as it started: /etc byte for byte, ufw's status and iptables rules, hardn's state, backups and logs.
