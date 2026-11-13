# Brute-force limiting (`pkg/screens/ssh`, `pkg/screens/firewall`)

The real unit of the roadmap's "Brute-force limiting", `next/brute-force`: what [gallery-bruteforce.md](gallery-bruteforce.md) mocked, as adopted there (2026-11-13, all (a)).
Status: built and tested in process on macOS (`make check`). No hardn binary ran. **Nothing ran on Linux**: §5.

## 1. What it does

- **SSH Login, the row "Brute-Force Limit"**, after Allowed Users: sshd's `PerSourcePenalties`, read only. "On" with "sshd's default" or "by <file>"; "Off", a warning, with "by <file>"; "Not available", "needs OpenSSH 9.8", on an sshd that has none; "Can't read" where `sshd -T`'s line does not establish it. It changes no status row and no score, and the screen has no item for it (decisions 3 and 4).
- **Firewall, item 5 "Limit SSH"**, on ufw: offered where ufw is active and allows an SSH port from anywhere by the plain rule `ufw allow <port>/tcp` writes, whatever sshd's row says (decision 6). Its review is the note, each rule as a "Limit", and the plan `ufw limit <port>/tcp`; y/N (decision 7). After it SSH reads "Rate limited", the rules "Limit", and the item is gone. The other items keep their keys (decision 5).
- **firewalld** (Rocky, Alma, RHEL): nothing new on the screen; the action is refused (`FIREWALL_EL_READ_ONLY`) (decision 8). **Run All** and `-w` don't add the limit (decision 9): no code of theirs changed.

## 2. The API

- `sshd.Effective.Penalties` (`PenaltiesOn`, `PenaltiesOff`, `PenaltiesNone`, `PenaltiesUnknown`), read by `sshd.ReadEffective` from the `sshd -T` it already runs; `sshchange.Reading.PenaltiesBy()`, the file whose line decides it; `screen.SSHLimitRow`.
- `ufw.LimitPorts(state, listeners)`, `ufw.PlanLimit(state, listeners, pve)` (pure), `ufw.StepLimit`, `ufw.ErrNoPlainAllow` ([ufw.md](../safety/ufw.md) §2.22).
- `steps.PlanUFWLimit`: one plain change, id `firewall.incoming/limit`, resource ufw. `firewall.ActionLimitSSH`; `firewall.Reading.LimitPorts()`.
- `screen.Firewall.LimitSSH`, `screen.FirewallLimitItem` ("5"), `screen.FirewallLimitChange(rules)`.
- Code `UFW_NO_PLAIN_ALLOW` ([output.md](../reference/output.md)).

## 3. Engineering calls

- **On, off or none comes from `sshd -T`'s line, not the version.** OpenSSH 9.8 and 10.0 always print `persourcepenalties`: `no`, or the penalties, `crash:` first; 9.7 has no such keyword (`servconf.c` at V_9_8_P1, V_10_0_P2 and V_9_7_P1, read 2026-11-13). No line beside the settings sshd always prints is "Not available"; no line and none of those, a value that is neither, or two lines, is "Can't read". A distribution's patched sshd is read the same way, so a backport shows as what it prints.
- **The deciding file is `servconf.c`'s rule:** off, the first `no`; on, the first line that sets a penalty (one turns them on whatever a `yes` or `no` says), else the first `yes`. Match blocks can't set it. hardn's own file gets no note, as the other rows; the name goes through `logview.Safe`.
- **The limit replaces hardn's kind of allow and nothing else.** A limit accepts, so the port never lacks a rule that lets SSH in: the plan is refused unless every listener let through before is let through after, the undo (`ufw allow <port>/tcp`) is journalled before the step runs, a failed step is rolled back with it, and the apply fails unless ufw then shows the port limited. A rule with a source, an interface, an application (`OpenSSH`), a comment or a log type, and a port allowed in one IP version alone, are the administrator's and are left, and with nothing else to limit the item is not offered. The action then refuses (`UFW_NO_PLAIN_ALLOW`) only while a port is open from anywhere with no limit; with none open so (a source rule alone), its plan is empty.
- **It refuses where it can't prove that:** `ss` missing or failing, sshd's listeners not read or not bound, ufw not installed, not read whole, not running, or running with `ENABLED=no`, Proxmox VE. No SSH server, or every port limited already: an empty plan.
- **The plan is made again when 5 is typed**, so a rule changed since the screen was drawn is refused before the question.
- **SSH on two ports** (a pending `hardn confirm`): each with the plain allow is named and limited.
- **Change SSH port on a limited port** still adds a plain allow for the new port, as before; the item then offers the limit for it. Not changed here (the page left it to that unit).
- **SSH Login is a line longer:** 24 lines at 80x24 on most hosts, so it still fits; with a Match block row 25, the old settings' hosts 26 and 27, whose top scrolls off as legacy-main's did.

## 4. Tested

- `pkg/host/sshd`: on (9.8's and 10.0's lines, mixed case), off, no line, an unknown value, an empty value, two lines, too little output; sshd missing, `sshd -T` failing, no output, garbage.
- `pkg/screens/ssh`: every state of the row, the deciding file (yes, a penalty, no, no then a penalty, hardn's file, none), a file name with control characters, sshd not read; the status rows and the items unchanged.
- `pkg/host/ufw`, `pkg/engine/controls/firewall`, `pkg/screens/firewall`: which ports are limited; one and two ports; IPv6 off; the undo and the rollback of a failed limit; the verify; every refusal above with its words and code, nothing run; not offered on each host that can't take it, firewalld and Proxmox VE among them; n, 0 and plan only change nothing; the rule changed between the draw and the key.
- The gallery: the `bruteforce` listing through the screens on simulated hosts; `play firewall/active22` with 5, y; `play ssh/limit-off`, `play ssh/limit-unavailable`, `play firewall/limited`. Its `firewalld` mock is still rows written in the gallery, which simulates no firewalld.

## 5. Not run

Nothing ran on a Linux host. To run, as root over SSH:

- `sshd -T | grep -i persourcepenalties` on Debian 13 or Ubuntu 26.04 (expected: the penalties), with `PerSourcePenalties no` in a drop-in (expected: `no`), and on Debian 12 or Ubuntu 24.04 (expected: no line), each beside SSH Login's row.
- On Debian 13 with ufw active and `ufw allow 22/tcp`: Firewall, 5, y. Expected: "Rule updated" for both IP versions, the session stays up, `ufw status verbose` shows `22/tcp LIMIT IN Anywhere` and the v6 row, the screen reads "Rate limited". Then `ufw allow 22/tcp`: the allow is back in place.
- The same with a second SSH session opening 6 connections in 30 seconds: the sixth is refused, an open session is not.
