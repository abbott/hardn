# Log Forwarding (`pkg/screens/logforward`, `logs.forward`)

> **Unit:** the roadmap's "Log forwarding", the real unit, on `next/log-forwarding`, from the mock the maintainer adopted on 2026-11-13 ("Adopt all": [gallery-logforward.md](gallery-logforward.md) §4). **Status:** built and tested on macOS, in process, on `hostfake`, through the real engine. No hardn binary ran, and nothing ran on Linux: §5 lists what only a host can show.

## 1. What it does

Main menu 6. hardn shows whether the host sends its logs to a syslog server, where, and by whose file; and sets the server hardn.yml's `logForwarding` names ([configuration.md](../configuration.md#log-forwarding)).

| Part | Where | What |
|---|---|---|
| The key | `pkg/config/logforward.go` | `LogForwarding`, and `Resolve`, which checks it and fills the defaults |
| The reader | `pkg/host/logfwd` | `Read`: the logger and each place it forwards to; `ReadConn`: the connection; `RsyslogFile`, `SetOpts`, `ClearOpts`: what hardn writes |
| The control | `pkg/engine/controls/logforward` | `Forward`: `Check` (the row), `Plan` by `Action` (Set, Stop); its refusals and their codes |
| The screen | `pkg/screens/logforward` | `Run`, and `View`, the mapping the gallery's hosts are drawn through too |
| The view | `pkg/ui/screen/logforward.go` | `screen.LogForward`, the mock's, with three states added (§3) |

- **Reading.** rsyslog is the logger where its package is installed (dpkg's or rpm's word); without it, on a host systemd booted, the logger is journald alone; on Alpine it is busybox syslogd where apk's database (`/lib/apk/db/installed`, a file) has no `rsyslog`; with rsyslog installed there, which of the two logs is not established, and the screen says "can't read" (`logfwd.ErrTwoLoggers`, `LOGFWD_NOT_READ`). Whether syslogd's service is in a runlevel is not looked at (unverified: how an installed Alpine adds `syslog` to one). rsyslog's forwarding is read from `/etc/rsyslog.conf` and `/etc/rsyslog.d/*.conf`: each `action(type="omfwd" ...)`, and each older `@host` (UDP) or `@@host` (TCP) line, with its options, port, bracketed address and template ([rsyslog's actions page](https://raw.githubusercontent.com/rsyslog/rsyslog/main/doc/source/configuration/actions.rst), "Remote Machine"); `$ActionSendStreamDriverMode 1` makes the `@@` lines after it TLS. busybox syslogd's is each `-R` of `SYSLOGD_OPTS` in `/etc/conf.d/syslog`. hardn never runs the logger to read it, and never edits a file it did not write.
- **Not read is not "not forwarding".** A file that cannot be read, an action that does not close, an `omfwd` action or `@` line with no host, `SYSLOGD_OPTS` set twice, not at all, or holding a character the shell expands: the row is Unknown, "can't read", and both items refuse.
- **Text from the host is made safe** (`logview.Safe`) before the row, the screen or a refusal shows it: a server's name, a file's name, rsyslogd's own error.
- **hardn's own setting** is `/etc/rsyslog.d/60-hardn-forward.conf` opening with `# Written by hardn (Log Forwarding).`; on Alpine, the words a comment line names: `# Added by hardn (Log Forwarding): -R 192.0.2.50:514 -L`. A file at hardn's path without its first line is not hardn's: Set server refuses (`LOGFWD_SET_OUTSIDE`, "move that file away"), whether or not the file forwards. hardn's file with its action edited out is still hardn's, and the review's diff starts from what is there. On Alpine, `-R` and its address are matched as a pair, two words side by side, so another `-R` on the line is never taken for hardn's.
- **Set server** (decisions 3 to 7, 11, 12): the plan is, in order, `rsyslog` installed where the host has journald alone and `rsyslog-gnutls` for TLS (`steps.PlanPackage`, as other controls install); hardn's file written, then `rsyslogd -N1`; `systemctl restart rsyslog`; `logger -t hardn "log forwarding set"`. On Alpine: `/etc/conf.d/syslog` rewritten with `-R <server>:<port>` and `-L` (kept out where `-L` is already there), its mode and owner kept; `rc-service syslog restart`; the test line. Plain y/N.
- **Rollback.** Every file is backed up and journalled by the engine. rsyslogd refusing its settings with hardn's file fails the change before the restart (`LOGFWD_CHECK_FAILED`): the file is put back and nothing restarts. A failed restart puts the file back and restarts again. An rsyslog hardn installed in the same change is removed by the rollback.
- **Stop forwarding** (decision 11): hardn's file deleted, or its words and comment taken out of `SYSLOGD_OPTS`, then the restart. rsyslog stays installed, and hardn.yml is not edited.
- **Run All** (decision 9): a Log Forwarding row only where hardn.yml names a server, planned as Set server, in the one plan. Never Stop.
- **The main menu** (decisions 8 and 10): item 6; the row is drawn after AppArmor's, only where hardn.yml names a server or the host forwards (`controls.Menu`, `controls.MenuReports`). It is no row of `risk.Rows`, so the risk level never counts it.

## 2. Injection surface

`logForwarding`'s values are written between double quotes in rsyslog's file and as words of a line a shell reads on Alpine. Two checks, either of which alone refuses:

- `config.LogForwarding.Resolve`: `server` an IPv4 address or a host name (labels of letters, digits and hyphens); `port` 1 to 65535; `protocol` exactly `udp`, `tcp` or `tls`; `caFile` an absolute, clean path of letters, digits and `._+-/`, for `tls` alone. Nothing is trimmed or passed through.
- `logfwd.Server.Check`, run by `RsyslogFile` and `SetOpts` whoever calls them: the same character sets.

So no newline, quote, space, backslash, `$`, backquote, `;` or leading `-` reaches either file. On Alpine the existing line is rewritten only where every character of it is one hardn reads exactly; otherwise it is "can't read".

## 3. Refusals and codes

Said before anything is asked, each a `steps.Refusal` ([reference/output.md](../reference/output.md) §2.1):

| Code | When |
|---|---|
| `LOGFWD_NO_SERVER` | hardn.yml names no server |
| `LOGFWD_BAD_SERVER` | `logForwarding` does not pass the check; the refusal says which value |
| `LOGFWD_NOT_READ` | the logger or its settings are not read; or `rsyslogd -N1` already fails before hardn writes, with rsyslogd's first line |
| `LOGFWD_SET_OUTSIDE` | a setting that is not hardn's forwards already (decision 7) |
| `LOGFWD_UDP_ONLY` | Alpine, and hardn.yml asks for TCP or TLS (decision 5) |
| `LOGFWD_NO_CA_FILE` | `caFile` is not a file on the host |
| `LOGFWD_NO_INIT` | rsyslog on a host systemd did not boot, or syslogd where OpenRC did not |
| `LOGFWD_NOTHING_TO_STOP` | Stop forwarding with nothing of hardn's set |
| `LOGFWD_CHECK_FAILED` | at apply: rsyslogd refuses its settings with hardn's file; rolled back |
| `LOGFWD_CHANGED_SINCE` | at apply: `/etc/conf.d/syslog` is not as the plan read it |

A package install's refusals are `PACKAGE_*`, as everywhere: on Proxmox VE with journald alone, `PACKAGE_NO_TOOL`.

## 4. Settled here, beyond the adopted decisions

Each is the safer reading; those that are product choices are listed for the maintainer in the unit's report.

1. **IPv6 servers are refused** (`LOGFWD_BAD_SERVER`): how rsyslog's `target` and busybox's `-R` take one was not verified, and an address in brackets is a pattern to the shell that reads `/etc/conf.d/syslog`.
2. **TLS needs the server's name, not its address**, and `caFile`: the certificate is checked against the name (`StreamDriverPermittedPeers`).
3. **`caFile` with `udp` or `tcp` is refused**, not ignored: the file says TLS is meant.
4. **Proxmox VE with journald alone is refused, which is not decision 4 (a)** ("Proxmox VE follows this answer as Debian does, unless the maintainer wants it read-only there"): hardn installs no package on Proxmox VE (`packages.ToolFor`, a rule of every control), so the plan refuses with `PACKAGE_NO_TOOL`, and Set server reads "hardn can't install rsyslog here". With rsyslog already there it is as Debian. **Open, the maintainer's call:** keep this, or have hardn install on Proxmox VE, which changes `packages.ToolFor` for every control.
5. **Alpine's mark.** There is no drop-in, so a comment line names the words hardn added. Stop forwarding takes out exactly those, and refuses where they are gone. `-L` that was there before is the administrator's.
6. **Three view states the mock had not:** hardn.yml "Not valid" (Set server: "hardn.yml is not valid"); Connection Unknown, the row then Unknown, "connection not read", where `ss` cannot be run (never "None"); a logger hardn cannot tell, Unknown.
7. **The connection** is `ss -Htnp`: an established socket to the server's address and port; for a server named by name, rsyslogd's established socket to that port.
8. **The queue** stays `queue.type="linkedList"` alone: in memory, rsyslog's default size. What is kept while the server is away is not tuned.
9. **A restart starts a stopped logger.** Set server on a host whose rsyslog or syslogd is stopped starts it.
10. **Another syslog daemon** (syslog-ng) is not looked for. On Debian and Ubuntu installing rsyslog beside one is refused by the install's simulation, which would remove a package.

## 5. Not run

Nothing ran on Linux. To show on a host, per family: the row and the screen as read; Set server, the test line found on the server, Stop forwarding; a syntax error planted in another file (refused, nothing written); a bad `caFile` content (rsyslogd's check fails, rolled back). And the survey's unverified points ([gallery-logforward.md](gallery-logforward.md) §3): rsyslog's unit name outside Debian; Debian 13's journal reaching a new rsyslog; Ubuntu's AppArmor profile and the CA file; SELinux's ports; busybox's `-R` and `-L` on the versions Alpine ships; `ss -Htnp`'s lines for an established socket; an `omfwd` action with no `protocol` or `port`, which hardn shows as UDP and 514.
