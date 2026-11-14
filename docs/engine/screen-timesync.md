# Time Sync (`pkg/screens/timesync`, `time.sync`)

> **Unit:** the roadmap's "Time sync", the real unit, on `next/time-sync`, from the mock the maintainer adopted on 2026-11-14 ("Adopt all": [gallery-timesync.md](gallery-timesync.md) §4). **Status:** built and tested on macOS, in process, on `hostfake`, through the real engine. No hardn binary ran, and nothing ran on Linux: §5 lists what only a host can show. Decision 16 (`hardn check`) is not built: it waits for that branch.

## 1. What it does

Main menu `8) Configuration`, then `3) Time Sync` (decision 9 as revised on 2026-11-14; [screen-config.md](screen-config.md) §1.1). hardn shows which service keeps the host's time, whether it runs, whether the clock is in sync and to which server, and each server with where it is set; and makes that service use the servers hardn.yml's `ntpServers` names in place of its own ([configuration.md](../configuration.md#time-sync), [platforms.md](../platforms.md#time-sync)).

| Part | Where | What |
|---|---|---|
| The key | `pkg/config/timesync.go` | `NTPServers`, and `Resolve`, which checks it through `config.ResolveHost` |
| The reader | `pkg/host/timesync` | `Read`: the service, the clock, the servers. `SourcesFile`, `ChronyOff`, `ChronyOn`, `TimesyncdFile`, `SetPeers`, `ClearPeers`: what hardn writes |
| The control | `pkg/engine/controls/timesync` | `Sync`: `Check` (the row), `Plan` by `Action` (Set, Remove); its refusals and their codes |
| The screen | `pkg/screens/timesync` | `Run`, and `View`, the mapping the gallery's hosts are drawn through too |
| The view | `pkg/ui/screen/timesync.go` | `screen.TimeSync`, the mock's |
| A model for tests | `pkg/host/timesync/timefake` | how the commands answer; the gallery's hosts and every test use it |

Shared with Log Forwarding, not copied ([screen-logforward.md](screen-logforward.md) §1): `config.ResolveHost`, `hostaddr.Valid`, `confd.Parse`, `host.ReadFileIfThere`, `steps.Refuse`, `HostWords`, `ServiceArgv`, `RestartService`, `RewriteFile`, `WriteOwnFile`, `screenkit.Session.Reviewed`, `StatusRow`, `screen.Change`, `wantRow`, `setUndoItems`. Two small additions serve both: `steps.OwnFile.MkDir` (systemd's drop-in directory is not there on a stock host) and `steps.DeleteOwnFile`, which Stop forwarding now uses too. Log Forwarding's goldens are byte for byte as they were.

- **The service** (decision 5). chrony where its package is installed (dpkg's, rpm's or apk's word); else systemd-timesyncd on Debian, Ubuntu and Proxmox VE where that package is installed; else, on Alpine, busybox ntpd where its service is started or in a runlevel; else none. Whether it runs: `systemctl is-active`, `rc-service <name> status`. hardn installs, starts and swaps none.
- **The clock.** chrony: `chronyc -N tracking`, its "Leap status" and the name in its "Reference ID". Otherwise the kernel's flag: `timedatectl show`'s `NTPSynchronized` under systemd, `busybox adjtimex`'s return value under OpenRC. systemd-timesyncd's server is `timedatectl show-timesync`'s `ServerName`; busybox ntpd names none.
- **The servers.** chrony: every `server`, `pool` and `peer` line of the main file and of each file it names (`sourcedir`, `confdir`, `include`). systemd-timesyncd: each `NTP=` of `timesyncd.conf` and its drop-ins, in systemd's order, an empty one taking away those before it; then the links' servers and, with no other, the built-in ones, from `show-timesync`. busybox ntpd: the `-p` words of `NTPD_OPTS`.
- **Whose a server is** (decisions 7 and 8). hardn's: its own file, or the lines under its comment. DHCP's: a file of `/run/chrony-dhcp`, or `LinkNTPServers`. The service's own: chrony's main file and `ubuntu-ntp-pools.sources` (`timesync.DistroSources`). Anything else is set outside hardn: Set servers refuses.
- **Not read is not "None".** A package state or a service state that does not say, a file that cannot be read, a line hardn cannot make out: the row is Unknown, "can't read", and both items refuse. Text from the host is made safe (`logview.Safe`) before it is drawn or said.
- **Set servers** (decisions 1, 3, 6). chrony whose main file reads `/etc/chrony/sources.d`: hardn's `hardn.sources` there, each own line of the main file and of the distribution's files switched off, then `chronyc reload sources`, or a restart where the main file changed. chrony that reads no such directory: the main file edited in place, then a restart. systemd-timesyncd: `60-hardn.conf`, then a restart. busybox ntpd: `NTPD_OPTS` through `confd`, then a restart. Lines are `server <name> iburst`; no option is offered.
- **Remove servers** (decision 13): hardn's file deleted, its lines taken out, every line it switched off put back, then the same reload or restart. hardn.yml is not edited.
- **Rollback.** Every file is backed up and journalled by the engine; a failed reload or restart puts the files back and runs the command again.
- **Run All** (decision 15): a Time Sync row only where hardn.yml names servers, planned as Set servers. Never Remove.
- **The main menu** (decision 11; decision 9 as revised): no item of its own, under Configuration; the row is drawn last, on every host but a container. It is no row of `risk.Rows`.
- **System Details and the report** (decision 10): "Time service", "Clock" and "Synced to" with OS and Kernel, from the same reading; the JSON document's `details.time`.
- **A container** (decision 12): no service is looked for; the Clock row is the kernel's word; the screen's row is Not Applicable.
- **Notices** (decisions 4 and 18): where the host's own servers are authenticated, "The host's own servers are authenticated (NTS). hardn's are not."; on Proxmox VE, "On a cluster, set every node the same way."

## 2. Editing a file hardn did not write

`/etc/chrony/chrony.conf`, `/etc/chrony.conf`, `ubuntu-ntp-pools.sources` and `/etc/conf.d/ntpd` are the host's.

- **Minimal.** A chrony line is switched off by `#hardn: ` put before it, the line itself untouched. hardn's own lines in a main file sit under one comment that says how many they are: `# Added by hardn (Time Sync): the 2 lines below.` On Alpine, hardn's `-p` words go where the host's were, under `# Set by hardn (Time Sync), in place of: -p pool.ntp.org`.
- **Exactly reversed.** `ChronyOn` takes the mark off each switched-off server line and deletes hardn's comment with exactly the lines it names; `ClearPeers` puts the comment's words back where hardn's are. Tests hold a file to its bytes after Set then Remove, for files with no final line break, CRLF, indented lines and an administrator's own `#hardn:` comment.
- **Refused in a form hardn does not write back** (`TIMESYNC_NOT_READ`): a comment of hardn's that does not name exactly the lines under it, or twice; on Alpine, `NTPD_OPTS` not one double-quoted line with single spaces, its `-p` words not side by side, none at all, or a mark that names anything but peers.
- **Read again at apply.** The engine plans again under its lock and refuses a plan that differs (`PLAN_CHANGED`); the change itself reads the file again and refuses one that is not as planned (`TIMESYNC_CHANGED_SINCE`). The file's mode and owner are kept.

## 3. Injection surface

`ntpServers` goes into chrony's files, systemd's unit settings and a line a shell reads. Two checks, either of which alone refuses:

- `config.NTPServers.Resolve`: one to four, none twice, each an IPv4 address or a host name (`config.ResolveHost`). Nothing is trimmed or passed through.
- `timesync`'s writers, whoever calls them: one to four, each `hostaddr.Valid`.

So no space, newline, quote, `#`, `$`, `;` or leading `-` reaches a file. Words hardn puts back on Alpine come from its own comment and are checked too: a comment edited to name anything but `-p` and a name is refused.

## 4. Refusals and codes

| Code | When |
|---|---|
| `TIMESYNC_NO_SERVERS` | hardn.yml names no servers |
| `TIMESYNC_BAD_SERVERS` | `ntpServers` does not pass the check; the refusal says which server |
| `TIMESYNC_NOT_READ` | the service or its settings are not read, or a file is in a form hardn does not write back; also where the init that booted the host is not the one the service belongs to, as whether the service runs is then not read |
| `TIMESYNC_SET_OUTSIDE` | a file that is neither the service's own nor hardn's sets servers; a systemd-timesyncd drop-in read after hardn's has an empty `NTP=`, which clears hardn's servers; or a file at hardn's path is not hardn's |
| `TIMESYNC_NO_SERVICE` | no time service, or one that is not running |
| `TIMESYNC_CONTAINER` | a container |
| `TIMESYNC_NOTHING_TO_REMOVE` | Remove servers with nothing of hardn's set |
| `TIMESYNC_CHANGED_SINCE` | at apply: a file is not as the plan read it |

## 5. Settled here, beyond the adopted decisions

Each is the safer reading; the product choices among them are in the unit's report.

1. **systemd-timesyncd's main file.** Decision 7 counts a line of a service's main file as the distribution's, to be replaced. For `NTP=` in `timesyncd.conf` hardn refuses instead (`TIMESYNC_SET_OUTSIDE`): systemd adds a drop-in's servers to it, so replacing would mean editing that file or writing an empty `NTP=` first, which the adopted review does not show. A stock file has none.
2. **`peer` lines are switched off with `server` and `pool`**: all three are sources chronyd would still follow.
3. **Alpine with no `-p`** is "can't read": ntpd then takes its servers from `/etc/ntp.conf`, which hardn does not read.
4. **Remove servers is refused while the service is stopped**, as Set servers is: its restart would start it.
5. **Set servers is refused wherever a file outside hardn names a server**, also where hardn's own setting is there beside it: the screen then shows Set servers as unavailable, beside Remove servers where hardn.yml's servers differ. An empty `NTP=` in a systemd-timesyncd drop-in whose name sorts after `60-hardn.conf` counts the same, whether hardn's file is there yet or not: it would clear hardn's servers. With hardn's file there, the screen shows no server of hardn's then, so it offers no Remove servers.
6. **A clock not read is Unknown**, on the row too: chronyc's answer is not replaced by the kernel's.
7. **Other time daemons** (openntpd, ntpsec) are not looked for: the Service row reads "None" there, and the Clock row still tells.
8. **p4status** keeps to `controls.Status`: it does not draw the Time Sync row.
9. **The main menu keeps v0.9.0's ten items** (decision 9 as revised). With the Time Sync row it is 39 lines at 80 columns (38 before), and on 24 rows its top scrolls off, as before.

## 6. Not run

Nothing ran on Linux. Read from upstream sources on 2026-11-13 and 14, not run: `chronyc -N tracking`'s lines (chrony's `client.c`), `busybox adjtimex`'s (`adjtimex.c`), systemd's reading of `NTP=` (`timesyncd-conf.c`), Alpine's chrony service name and file (`aports`). Not verified at all: `timedatectl show` and `show-timesync`'s lines; chronyc on Alpine with `cmdport 0`; whether `busybox adjtimex` and `timedatectl show` answer in a container; `chrony`'s unit name on Proxmox VE; ucf's and rpm's handling of an edited main file on upgrade; how long the clock reads "Not in sync" after a restart; outgoing 123/udp through the firewall.
