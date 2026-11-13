# Brute-force limiting mock (unit (a))

*Since the real unit (2026-11-13): the row and the item are built ([screen-bruteforce.md](screen-bruteforce.md)). The mocks but `firewalld` are hardn's own screens on simulated hosts, the review's plan is the engine's, and `play firewall/active22` runs item 5. Where this page says nothing reads a host, no engine plans the limit, or a golden did not change, this note is current.*

Unit (a) of the roadmap's "Brute-force limiting", `next/brute-force`. **Adopted (the maintainer, 2026-11-13): "Adopt all".** Every recommended answer of §3 is adopted, with §4's words and §5's calls as stated. It is the mock alone: nothing reads a host, no engine plans the limit, and nothing in hardn fills the new row or offers the new item.
Status: built and tested on macOS in process (`go test ./pkg/ui/screen/ ./internal/tools/uigallery/`). No hardn binary ran. Nothing ran on Linux. `cmd/hardn`, `pkg/screens`, `pkg/engine`, `pkg/host` and hardn.yml are untouched; no existing golden changed.

A host has two limits with no new package:

- **sshd's own**, `PerSourcePenalties`: on by default since OpenSSH 9.8, it refuses an address that keeps failing to log in, for a time that grows ([vmtest.md](../safety/vmtest.md) §3). hardn doesn't read it yet.
- **ufw's `limit` rule**: it refuses an address that opens 6 or more connections in 30 seconds ([ufw.md](../safety/ufw.md) §3, ufw(8)). hardn reads one as "Rate limited" and "Limit" today (`pkg/host/ufw`, `Limited`) and cannot add one.

## 1. What to run

```
go run ./internal/tools/uigallery bruteforce [-ascii] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery bruteforce -list                    # the mocks' names
```

The views are `pkg/ui/screen/ssh.go` (`SSHLimitRow`) and `pkg/ui/screen/firewall.go` (`Firewall.LimitSSH`, `FirewallLimitChange`), the mocks `internal/tools/uigallery/bruteforce.go`, the goldens `internal/tools/uigallery/testdata/golden/bruteforce/`. There is no `play bruteforce`: play runs hardn's own screens, which don't have the row or the item.

## 2. The mocks

Each is drawn by the screen's own view from rows written in the mock, as `pkg/screens/ssh` and `pkg/screens/firewall` word them on these hosts (their `screen/debian13`, `screen/active22` and `screen/rocky-running` goldens).

| Mock | What it shows |
| --- | --- |
| `ssh-on` | SSH Login on Debian 13 (OpenSSH 10.0p2), no file setting `PerSourcePenalties`: the row "Brute-Force Limit", "On", "sshd's default" |
| `ssh-off` | The same, `20-site.conf` setting `PerSourcePenalties no`: "Off", a warning, "by 20-site.conf" |
| `ssh-unavailable` | Debian 12 (OpenSSH 9.2p1), before 9.8: "Not available", "needs OpenSSH 9.8" |
| `firewall-offer` | Firewall, ufw active and allowing 22/tcp from anywhere: item 5, "Limit SSH" |
| `firewall-review` | Item 5 up to its question: what the limit does, SSH's rules as it leaves them, the plan, y/N |
| `firewall-limited` | After it: SSH "Rate limited", the rules "Limit", no item 5. This screen is hardn's today |
| `firewalld` | Firewall on Rocky Linux 10, firewalld running: as today, nothing added |

The row, in `ssh-on`, `ssh-off` and `ssh-unavailable`:

```
   -  Allowed Users        Anyone
   -  Brute-Force Limit    On sshd's default
  [ ] Brute-Force Limit    Off by 20-site.conf
   -  Brute-Force Limit    Not available needs OpenSSH 9.8
```

The item and its review:

```
   1) Disable firewall     Stop ufw (not recommended)
   2) Application Profiles Add, remove, apply
   3) Trusted Interfaces   Add, remove, apply
   5) Limit SSH            Block repeated connections

# Limit SSH ####################################################

# Note
# ufw refuses an address that opens 6 or more SSH connections in
# 30 seconds, yours included.

  Rules --------------------------------------------------------

  [+] 22/tcp               Limit
  [+] 22/tcp (v6)          Limit

  Limit SSH? [y/N, 0 or q]:
```

**On firewalld (Rocky Linux, AlmaLinux).** ufw's limit does not exist there. firewalld's rich rules have a `limit`, but it is a rate for the rule: "A rule using this tag will match until this limit is reached" ([firewalld.richlanguage](https://firewalld.org/documentation/man-pages/firewalld.richlanguage.html), read 2026-11-13), so one address could use it up for everyone. The Firewall screen there stays as it is, and the host's limit is sshd's row on SSH Login.

## 3. Decisions for the maintainer

Adopted, 2026-11-13: every (a) below. Drawn: the same.

1. **The row's name.** It is a row of SSH Login's "SSH Configuration" section, after "Allowed Users". (a) *Recommended, drawn:* "Brute-Force Limit", the roadmap's word. (b) "Login Penalties", after sshd's keyword: closer to what you would edit, but it doesn't say what it is for.
2. **The row on an sshd before 9.8.** There sshd has `MaxAuthTries` and `MaxStartups` only: tries per connection and connections not yet logged in, neither per address. (a) *Recommended, drawn:* "Not available", "needs OpenSSH 9.8", a plain row: calling those two a brute-force limit would overstate them. (b) Show them ("6 tries a connection"): more to read, and it reads as a limit where an attacker only has to reconnect.
3. **What "Off" counts for.** (a) *Recommended, drawn:* a warning row and nothing more: the SSH Auth row and the score don't change, as ufw's limit or another tool may do the job. (b) A plain row. (c) It lowers SSH Auth to Partial: *gates* a change to the SSH Auth check.
4. **Whether SSH Login gets an item to turn the penalties back on.** (a) *Recommended, drawn:* no item: the row names the file to edit, and "Off" is someone's choice. (b) An item writing `PerSourcePenalties yes` to `00-hardn.conf`: *gates* an SSH change in the engine, and it is lockout-risk like every SSH change.
5. **The Firewall item's key.** Firewall's items keep their keys (1 by state, 2 and 3 with ufw installed, 4 Disable beside item 1). (a) *Recommended, drawn:* "Limit SSH" is 5, so nothing moves; where Disable is item 1 the menu reads 1, 2, 3, 5 and the prompt "[0-3, 5 or q]", as SSH Login's does on Alpine. (b) 4, with Disable moved to 5: no gap, but Disable's key changes.
6. **When the item is offered.** (a) *Recommended, drawn:* wherever ufw is active and allows SSH's port from anywhere with no limit, whatever sshd's row says: the two act apart, ufw before sshd answers. (b) Only where sshd's limit is off or not available: fewer items, but ufw's limit also covers what sshd's penalties don't count. *Gates* the real unit's condition.
7. **How the change is confirmed.** ufw's limit counts every new connection, the user's own too: a tool that connects often (Ansible, a loop of `scp`) can be refused. (a) *Recommended, drawn:* y/N, with the note saying "yours included": it stops as soon as the connections slow down, and `ufw allow 22/tcp` undoes it. (b) Lockout-risk: the hostname typed and `hardn confirm` after it, as for Enable. *Gates* the change's risk in the engine.
8. **What firewalld's screen shows.** (a) *Recommended, drawn:* nothing new; SSH Login's row is the host's limit. (b) A muted line in firewalld's section pointing to SSH Login: one more line on every Rocky host. (c) A rich rule with `limit`: not recommended, as it is one rate for every address.
9. **Run All.** (a) *Recommended:* Run All doesn't add the limit; the Firewall screen alone does. (b) Run All adds it where it enables ufw: *gates* a change to Run All's plan and its goldens.

## 4. Words

Decided with §3: "Brute-Force Limit"; "On"; "Off"; "Not available"; "needs OpenSSH 9.8"; the notes "sshd's default" and "by <file>", as the section's other rows; "Limit SSH"; "Block repeated connections"; "ufw refuses an address that opens 6 or more SSH connections in 30 seconds, yours included."; "Limit SSH?"; the review's "Limit", the Rules section's word.

## 5. Made here

- The row is built by `screen.SSHLimitRow`, with the section's other rows' shape; `pkg/screens/ssh` fills the section, so it will place the row.
- `Firewall.LimitSSH` is false in everything hardn fills, so ufw's screen is byte for byte what it was; `firewall-offer` and `firewall-limited` differ from `screen/active22` only in the item and the words "Rate limited" and "Limit".
- The review's notice is a note, not a warning: nothing stops or opens.
- The review lists each rule as the Rules section names it ("22/tcp", "22/tcp (v6)"), so the screen after it reads the same.
- `firewall-review` runs the real flow on a fixture plan and stops at the question. Its plan (`ufw limit 22/tcp`, "allow 22/tcp" to "limit 22/tcp") is illustrative.
- No mock for Rocky's SSH Login: the row there is `ssh-on`'s.

## 6. For the real unit

- `sshd.ReadEffective` does not keep `persourcepenalties`; it has to, with the file that decides it. Before 9.8 `sshd -T` has no such line: the version, or the line's absence, tells "Not available" from "Off".
- `ufw limit 22/tcp` has `allow 22/tcp`'s tuple with another action, so ufw replaces the allow instead of adding a rule (ufw.md §2.8, which tested the other direction, an allow over a saved limit). The plan must say so and undo with the allow; a rule of the administrator's (a comment, a source, `limit OpenSSH`) is left alone.
- `Evaluate` counts a limit as let through (ufw.md, "Not modelled"), so Enable and Change SSH port already pass on a limited port. Whether Change SSH port then adds a limit or an allow for the new port is to decide there.
- SSH on two ports (a pending `hardn confirm`): the item names and limits each.
- Run on a VM: the row on Debian 12 or Ubuntu 24.04 (before 9.8), and a limit applied over an open session.
