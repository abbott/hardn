# Log forwarding mock

> **Renamed 2026-11-14.** The screen and its Configuration item are now "Syslog Settings"; its status row, the main menu's row and the hardn.yml screen's section are "Syslog". The main menu's last rows are Syslog, NTP, AppArmor. The control id is unchanged; the codes are now `SYSLOG_*`. The record below keeps the old name, Log Forwarding.

> **Several servers, 2026-11-14.** hardn.yml's `logForwarding` is now `syslogServers`, a list of one to four; the screen has a Server row per server, with its protocol and connection, and no Protocol or Connection row; Set server is Set servers; `hardn check` compares the servers. [design-syslog-servers.md](design-syslog-servers.md) is the design and what was built. The record below is the one-server unit as it was.

> **Adopted 2026-11-13.** The maintainer adopted every recommended answer of §4, in these words: "Adopt all". The decisions below stay as the record. The real unit is built on them: [screen-logforward.md](screen-logforward.md) says what it is and what it settled beyond them. Two decisions are not built there: 13 (hardn's own audit log) is its own later unit, and 14 (`hardn check`) is a follow-up.

The mock unit of the roadmap's "Log forwarding". hardn shows whether the host sends its logs to a syslog server and where, and sets a server named in hardn.yml. This unit drew that screen and its reviews, surveyed how each supported release forwards, and listed what the maintainer had to decide.
Status of the mock unit, as it was written: built and tested on macOS in process. No hardn binary ran. Nothing ran on Linux: every fact about a host in §3 is read from an upstream source, linked, or marked unverified. Since the real unit, the gallery's mocks are hardn's own screen on simulated hosts (§1, §2).

## 1. What to run

```
go run ./internal/tools/uigallery logforward [-ascii] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery logforward -list                    # the mocks' names
go run ./internal/tools/uigallery play logforward                     # the hosts' menu, played on the engine
go run ./internal/tools/uigallery play logforward/<host>              # one host; the main menu's 8, then 2, leads to "off"
```

The view is `pkg/ui/screen/logforward.go` (`screen.LogForward`), the hosts `internal/tools/uigallery/logforward.go`, the goldens `internal/tools/uigallery/testdata/golden/logforward/`. Since the real unit each mock is the real screen (`pkg/screens/logforward`) on a simulated host, read by the real control, each review planned by the real engine.

## 2. The mocks

One host, `web-01`, and a syslog server at `192.0.2.50` (`logs.example.net`). As adopted, each reading was written by hand and each review's plan too. Since the real unit they are the control's reading and the engine's plan on a simulated host; what that changed in a drawing is noted in its row.

| Mock | What it shows |
| --- | --- |
| `off` | Ubuntu 24.04, rsyslog, nothing forwarded; hardn.yml names the server: "Set server" |
| `no-server` | The same host and no server in hardn.yml: the item says so, and refuses with words |
| `tcp` | Forwarding over TCP by hardn's file, a connection open: "Stop forwarding" |
| `tcp-down` | The same, no connection to the server: the row is Partial |
| `udp` | Forwarding over UDP: the Protocol row is a warning, "not encrypted, losses unseen", and there is no Connection row |
| `tls` | Forwarding over TLS to the server's name, with the CA file |
| `foreign` | Forwarding set by `50-remote.conf`, not hardn's: "Set by Not hardn", and Set server unavailable |
| `mismatch` | hardn's file forwards to `192.0.2.50`; hardn.yml names `192.0.2.60`: the hardn.yml row warns, Set server is 1 and Stop forwarding 2 |
| `journald` | Debian 13, journald alone: "no syslog forwarder", and Set server "Installs rsyslog" |
| `alpine` | Alpine 3.23, busybox syslogd forwarding over UDP by `/etc/conf.d/syslog` |
| `alpine-tcp` | Alpine with hardn.yml asking for TCP: "busybox syslogd is UDP only" |
| `not-read` | rsyslog's settings cannot be read (an action in `50-remote.conf` that does not close): the row and the server are Unknown |
| `review` | `off`'s Set server up to its question: the drop-in's diff, rsyslogd's check, the restart, a test line |
| `review-tls` | The same for TLS: `rsyslog-gnutls` installed first |
| `review-journald` | `journald`'s Set server: rsyslog installed first, and a notice saying what else that does |
| `review-alpine` | Set server on Alpine: one line of `/etc/conf.d/syslog`, and the restart. Since the real unit, a comment line above it names the words hardn added, by which Stop forwarding finds them |
| `review-stop` | `tcp`'s Stop forwarding: hardn's file deleted, the restart |
| `review-move` | Added with the real unit. `mismatch`'s Set server: hardn's file rewritten |
| `refuse-no-server`, `refuse-alpine-tcp`, `refuse-not-read`, `refuse-rsyslogd` | Added with the real unit. Set server refused with words, before anything is asked |

The reviews open with "Checking the host...", the plan made before asking, as every screen's do.

`tcp`, drawn:

```
# Log Forwarding ###############################################

  [+] Log Forwarding       Configured 192.0.2.50:514 TCP

  Forwarding ---------------------------------------------------

   -  Logger               rsyslog
   -  Server               192.0.2.50:514
   -  Protocol             TCP not encrypted
  [+] Connection           Open
   -  File                 /etc/rsyslog.d/60-hardn-forward.conf
   -  Set by               hardn
   -  hardn.yml            192.0.2.50:514 TCP

  Select an option

   1) Stop forwarding      Undo hardn's change

   0) Return

    > Option [0-1 or q]:
```

`review`'s plan, drawn:

```
   -  Write hardn's forwarding file
      /etc/rsyslog.d/60-hardn-forward.conf
      @@ -0,0 +1,4 @@
      +# Written by hardn (Syslog Settings).
      +*.* action(type="omfwd" protocol="tcp"
      +    target="192.0.2.50" port="514"
      +    queue.type="linkedList")
      $ rsyslogd -N1

   -  Restart rsyslog
      rsyslog
      $ systemctl restart rsyslog

   -  Send a test line
      $ logger -t hardn "log forwarding set"

  Forward logs to 192.0.2.50? [y/N, 0 or q]:
```

## 3. Survey

Read on 2026-11-13. "Unverified" means no upstream source was fetched for it; none of it was run.

### 3.1 The logger each release has

| Release | Logger by default | Source |
| --- | --- | --- |
| Debian 13, 12 | journald alone | Debian 12's release notes, 5.1.7: "From bookworm, rsyslog is no longer installed by default" ([release notes](https://www.debian.org/releases/bookworm/amd64/release-notes/ch-information.en.html#changes-to-system-logging)). Neither cloud image lists rsyslog ([13](https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.json), [12](https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.json)) |
| Ubuntu 26.04, 24.04, 22.04 | rsyslog (8.2512.0, 8.2312.0, 8.2112.0) | The server cloud images' package lists ([26.04](https://cloud-images.ubuntu.com/resolute/current/resolute-server-cloudimg-amd64.manifest), [24.04](https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.manifest), [22.04](https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.manifest)). Other install media: unverified |
| Alpine 3.24, 3.23, 3.22 | busybox syslogd, the `syslog` service | busybox is built with `CONFIG_SYSLOGD=y` and `CONFIG_FEATURE_REMOTE_LOG=y` on each branch ([3.24](https://raw.githubusercontent.com/alpinelinux/aports/3.24-stable/main/busybox/busyboxconfig), [3.23](https://raw.githubusercontent.com/alpinelinux/aports/3.23-stable/main/busybox/busyboxconfig), [3.22](https://raw.githubusercontent.com/alpinelinux/aports/3.22-stable/main/busybox/busyboxconfig)). Alpine's boot adds `syslog` to the boot runlevel ([initramfs-init](https://raw.githubusercontent.com/alpinelinux/mkinitfs/master/initramfs-init.in), `rc_add syslog boot`; read on master, not per release). Cloud images: unverified |
| Proxmox VE 9 | Unverified; journald alone expected, as Debian 13 | pve-manager's [debian/control](https://git.proxmox.com/?p=pve-manager.git;a=blob_plain;f=debian/control;hb=HEAD) names no syslog package. What the installer adds: unverified |
| Rocky, Alma, RHEL 10, 9 | rsyslog | CentOS Stream's package groups list rsyslog as mandatory in `core` ([9](https://gitlab.com/redhat/centos-stream/release-engineering/comps/-/raw/main/comps-centos-stream-9.xml.in), [10](https://gitlab.com/redhat/centos-stream/release-engineering/comps/-/raw/c10s/comps-centos-stream-10.xml.in)). Rocky's, Alma's and RHEL's own lists, and their cloud images: unverified |

### 3.2 rsyslog (Ubuntu, Rocky, Alma, RHEL; Debian and Proxmox VE once installed)

- **The file.** A drop-in under `/etc/rsyslog.d/`. Debian's and Ubuntu's `rsyslog.conf` end with `$IncludeConfig /etc/rsyslog.d/*.conf` ([Debian](https://salsa.debian.org/debian/rsyslog/-/raw/debian/latest/debian/rsyslog.conf), [Debian 12](https://salsa.debian.org/debian/rsyslog/-/raw/debian/bookworm/debian/rsyslog.conf), [Ubuntu 24.04](https://git.launchpad.net/ubuntu/+source/rsyslog/plain/debian/rsyslog.conf?h=ubuntu/noble)); CentOS Stream's has `include(file="/etc/rsyslog.d/*.conf" mode="optional")` ([9](https://gitlab.com/redhat/centos-stream/rpms/rsyslog/-/raw/c9s/rsyslog.conf), [10](https://gitlab.com/redhat/centos-stream/rpms/rsyslog/-/raw/c10s/rsyslog.conf)). Ubuntu 26.04's and 22.04's files: unverified.
- **The directive.** One `omfwd` action, which "forwards syslog messages to remote systems via UDP, TCP, or TLS" and is built in ([omfwd](https://www.rsyslog.com/doc/configuration/modules/omfwd.html)). Its quick start for TCP, on one line here:

  ```
  action(type="omfwd" target="logs.example.com" port="514" protocol="tcp" queue.type="linkedList")
  ```

  The page says to define a queue for TCP "to avoid blocking if the remote server is unavailable". A selector before it (`*.*`) chooses the messages, as CentOS Stream's own file writes `*.emerg action(...)`. The older forms a file hardn did not write may use (`*.* @host`, `@@host`): unverified here.
- **Reload.** A restart. rsyslogd's HUP only closes its open files ([rsyslogd(8)](https://raw.githubusercontent.com/rsyslog/rsyslog/main/tools/rsyslogd.8)). The same page has `-N1`, which checks the configuration without running, and `-o file`, which writes the whole configuration as rsyslogd reads it. The unit is `rsyslog.service` on Debian ([rsyslog.service](https://salsa.debian.org/debian/rsyslog/-/raw/debian/latest/debian/rsyslog.service)); its name on the others: unverified.
- **TLS.** On the same action: `StreamDriver="gtls"` (or `"ossl"`), `StreamDriverMode="1"`, `StreamDriverAuthMode="x509/name"`, `StreamDriverPermittedPeers="<the server's name>"`, and `StreamDriver.CAFile`, which the page dates to rsyslog 8.2108.0. It needs:
  - the driver's package: `rsyslog-gnutls` or `rsyslog-openssl` on Debian and Ubuntu ([Debian](https://salsa.debian.org/debian/rsyslog/-/raw/debian/latest/debian/control), [Ubuntu](https://git.launchpad.net/ubuntu/+source/rsyslog/plain/debian/control?h=ubuntu/noble)) and on CentOS Stream ([9](https://gitlab.com/redhat/centos-stream/rpms/rsyslog/-/raw/c9s/rsyslog.spec), [10](https://gitlab.com/redhat/centos-stream/rpms/rsyslog/-/raw/c10s/rsyslog.spec));
  - a CA file rsyslogd can read, and the name on the server's certificate. A client certificate only where the server asks for one.
  - rsyslog 8.2108.0 or later for the per-action CA file: Debian 13 has 8.2504.0 and Debian 12 8.2302.0 ([sources.debian.org](https://sources.debian.org/api/src/rsyslog/)), Ubuntu as §3.1, CentOS Stream 9 and 10 8.2604.0 (their spec files). Rocky's, Alma's and RHEL's minor releases: unverified.
- **Ubuntu confines rsyslogd.** It drops to the user `syslog` (`$PrivDropToUser`, the rsyslog.conf above), and its AppArmor profile lets it read `/etc/rsyslog.conf` and `/etc/rsyslog.d/**` ([profile](https://git.launchpad.net/ubuntu/+source/rsyslog/plain/debian/usr.sbin.rsyslogd?h=ubuntu/noble)). Whether the profile's abstractions let it read a CA file elsewhere, and whether the profile is enforced as installed: unverified.
- **SELinux on Rocky, Alma and RHEL.** The policy lets rsyslogd connect over TCP to the ports labelled `rsh` (514), `syslogd` (601, 20514) and `syslog_tls` (6514, 10514), and send UDP to any port ([ports, c9s](https://raw.githubusercontent.com/fedora-selinux/selinux-policy/c9s/policy/modules/kernel/corenetwork.te.in), [rules, c9s](https://raw.githubusercontent.com/fedora-selinux/selinux-policy/c9s/policy/modules/system/logging.te); c10s the same). What happens on another TCP port, and the policy as Rocky and Alma ship it: unverified.

### 3.3 journald alone (Debian 13, 12; Proxmox VE 9 expected)

journald has no syslog client. Its own means, from [journald.conf(5)](https://www.freedesktop.org/software/systemd/man/latest/journald.conf.html) and [systemd-journal-upload(8)](https://www.freedesktop.org/software/systemd/man/latest/systemd-journal-upload.service.html):

- `ForwardToSyslog=` hands messages to a syslog daemon on the same host. Upstream, it is off.
- `ForwardToSocket=` sends to an address, in "Journal Export Format", which is not syslog. The systemd version it needs: unverified.
- `systemd-journal-upload` uploads to a URL over HTTPS or HTTP, for `systemd-journal-remote` to receive. A syslog server does not.

So a syslog server gets these hosts' logs only through a syslog daemon. rsyslog is packaged on both. Debian's rsyslog reads the local socket (`imuxsock`, the rsyslog.conf above), and Debian 12's systemd carries a patch named "Re-enable-journal-forwarding-to-syslog" ([series](https://salsa.debian.org/systemd-team/systemd/-/raw/debian/bookworm/debian/patches/series)). Whether Debian 13's journald hands messages to a newly installed rsyslog with no other change: unverified. rsyslog as packaged also writes its own files under `/var/log` (Debian's release notes, above).

### 3.4 busybox syslogd (Alpine)

From busybox's [syslogd.c](https://raw.githubusercontent.com/mirror/busybox/master/sysklogd/syslogd.c) (master; the versions Alpine ships were not matched) and Alpine's service ([init script](https://raw.githubusercontent.com/alpinelinux/aports/3.23-stable/main/busybox/syslog.initd), [conf.d](https://raw.githubusercontent.com/alpinelinux/aports/3.23-stable/main/busybox/syslog.confd); 3.22 and 3.24 the same):

- **The file.** No drop-in directory. The service runs `syslogd ${SYSLOGD_OPTS} -n`, and `/etc/conf.d/syslog` is the one line `SYSLOGD_OPTS="-t"`. Forwarding is an edit of that line.
- **The option.** `-R HOST[:PORT]`, "Log to HOST:PORT (default PORT:514)". With `-R` alone the host stops logging locally: `-L` is "Log locally and via network (default is network only if -R)".
- **UDP only.** The remote socket is `SOCK_DGRAM`. No TCP, no TLS.
- **Everything or nothing.** The remote options choose no facility; `-l N` limits by priority, for local and remote alike.
- **Reload.** Options are read at start, so `rc-service syslog restart`.
- **Anything more** needs rsyslog, which Alpine packages in main with `rsyslog-tls` beside it ([APKBUILD, 3.22](https://raw.githubusercontent.com/alpinelinux/aports/3.22-stable/main/rsyslog/APKBUILD), 8.2410.0; 3.23 and 3.24: unverified).

### 3.5 Telling that it works

The host cannot know what the server received; only the server shows that. What the host can show, none of it run here:

- **The setting is in effect:** the file is there, `rsyslogd -N1` accepts it, the service is running. On Alpine, `-R` is on syslogd's command line.
- **TCP and TLS:** a connection to the server among the host's sockets (`ss`, which hardn reads already). When rsyslog opens it, and how soon it notices the server gone: unverified.
- **UDP:** nothing. A message to a server that is not there is lost without a word.
- **A test line:** `logger` writes one message through the logger, for the administrator to find on the server. busybox has `logger` (`CONFIG_LOGGER=y`, §3.1's files); the other releases': unverified.

## 4. Decisions for the maintainer

Fourteen. Drawn: every recommendation below but 8, 9, 10, 13 and 14, which change nothing on this screen. In order: an earlier one gates the later ones it names.

1. **The hardn.yml key.** hardn has no key for a log server. The roadmap has hardn "set a server from hardn.yml (address, port, and UDP, TCP or TLS)". The key's name and shape are what the user writes, and what the screen's "hardn.yml" row and the Set server item read.
   - (a) *Recommended, drawn:* one mapping, one server, nothing set by default:

     ```yaml
     logForwarding:
       server: "192.0.2.50"   # address or name
       port: 514              # default 514; 6514 for tls
       protocol: tcp          # udp, tcp or tls; default tcp
       caFile: ""             # tls only
     ```

     With no `server`, hardn changes nothing and Set server says "No server in hardn.yml". The screen has no form: the server is typed in the file, as the roadmap says.
   - (b) Flat keys (`logServer`, `logServerPort`, `logServerProtocol`), as `sshPort` is: nothing new to parse, three keys to keep together.
   - (c) One string, `logServer: "tcp://192.0.2.50:514"`: shortest, and a new format to explain and check.
   - (d) (a), and a form on the screen that saves the key, as DNS saves `nameservers`: more to build, and not what the roadmap describes.

   *Gates 2, 3, 9 and 14.*

2. **Which protocols are offered, and UDP.** `protocol` in decision 1's key. rsyslog does UDP, TCP and TLS; busybox syslogd does UDP alone (§3.4).
   - (a) *Recommended, drawn:* all three, TCP the default. UDP is offered, because it is all Alpine's logger has and what many syslog servers listen on. The screen says what each lacks on the Protocol row: TCP "not encrypted"; UDP a warning, "not encrypted, losses unseen"; TLS green.
   - (b) TCP and TLS only: no silent loss, and Alpine then gets nothing without rsyslog (decision 5).
   - (c) All three, with no word on the row: shorter, and UDP reads as good as TLS.

   *Gates 5.*

3. **TLS in the first version, or later.** Shown on the `tls` and `review-tls` mocks.
   - (a) *Recommended, drawn:* in the first version, on rsyslog hosts, checking the server's certificate only: hardn.yml names a CA file already on the host, hardn installs the driver's package (`rsyslog-gnutls`) and writes the action of §3.2 with the server's name as the permitted peer. No client certificate. The directive is one for every rsyslog host. What is not settled is in §3.2: where Ubuntu's rsyslogd may read a CA file from, and the port SELinux allows.
   - (b) Later: the first version sets UDP and TCP, and only shows TLS where another file set it. Smaller, and hardn's own forwarding is then always in the clear.
   - (c) (a) with a client certificate too (`certFile`, `keyFile`): for servers that require one; two more keys and a private key to guard.

   *Gates the real unit's size.*

4. **A host with journald alone.** Debian 13 and 12 as installed, and Proxmox VE 9 as expected (§3.1): nothing on the host can send syslog (§3.3). Shown on `journald` and `review-journald`.
   - (a) *Recommended, drawn:* Set server installs rsyslog, then does as on any rsyslog host. The Logger row says "journald alone, no syslog forwarder", the item says "Installs rsyslog", and the review says what else that does: rsyslog also keeps its own log files under `/var/log`. Stop forwarding leaves rsyslog installed (decision 11).
   - (b) journald's own means: `systemd-journal-upload`. It does not speak syslog, so the maintainer's syslog server would not receive it.
   - (c) Show only: the screen says there is no forwarder and names the package; hardn installs nothing.

   Proxmox VE follows this answer as Debian does, unless the maintainer wants it read-only there, as the Firewall is.
   *Gates 9.*

5. **Alpine, where hardn.yml asks for TCP or TLS.** busybox syslogd is UDP only (§3.4). Shown on `alpine`, `alpine-tcp` and `review-alpine`.
   - (a) *Recommended, drawn:* hardn sets what busybox has. With `protocol: udp` it adds `-R <server>:<port> -L` to `SYSLOGD_OPTS` in `/etc/conf.d/syslog` and restarts `syslog`. With `tcp` or `tls` the item says "busybox syslogd is UDP only" and refuses with words; nothing is installed. This is not the roadmap's "one drop-in": Alpine has no drop-in directory, so it is one line of an existing file.
   - (b) Install rsyslog on Alpine for TCP and TLS, replacing busybox syslogd as the logger: every protocol everywhere, and hardn swaps a base service.
   - (c) Fall back to UDP with a warning: forwards something, and not what hardn.yml says.

6. **Which logs are forwarded.** The selector in hardn's file (`*.*` in the mocks).
   - (a) *Recommended, drawn:* everything, as it is logged. The server decides what to keep. busybox syslogd can only send everything (§3.4), so this is the one answer that is the same on every host.
   - (b) Authentication only (`auth,authpriv.*`): less traffic; misses the kernel, cron and every service; not possible on Alpine.
   - (c) A hardn.yml key that chooses: one more key, and Alpine ignores it.

7. **A server set by a file hardn did not write.** Shown on `foreign`: `50-remote.conf` forwards already.
   - (a) *Recommended, drawn:* hardn shows it (server, protocol, the file, "Set by Not hardn") and Set server is unavailable, "Set outside hardn". hardn never edits or removes that file, and never adds a second destination beside it.
   - (b) Set server stays offered where hardn.yml names another server, adding hardn's file: the host then sends everything twice, to two servers.
   - (c) hardn takes the file over: one source of truth, and hardn rewrites what the administrator wrote.

8. **Where it lives in the menu.** *Revised by the maintainer on 2026-11-14: Log Forwarding is no main-menu item. It lives under `8) Configuration`, as its item 2, with hardn.yml (1) and Time Sync (3); the main menu keeps v0.9.0's ten items and keys ([screen-config.md](screen-config.md) §1.1). The adopted answer, (a), is kept below as the record.* The main menu today: 1 User Management, 2 SSH Login, 3 Firewall, 4 DNS, 5 Auto Updates, 6 Run All, 7 Backup, 8 Configuration, 9 System Details, 10 Logs.
   - (a) *Recommended:* its own item, 6, "Log Forwarding", "Send logs to a syslog server", after Auto Updates with the other things hardn sets. Run All, Backup, Configuration, System Details and Logs each move down one (7 to 11), the hint reads `[0-11 or q]`, and every page and golden that names those keys changes. Someone typing 6 from habit gets this screen, which asks before it changes anything, not Run All.
   - (b) Item 11, after Logs: nothing moves, and a control sits after the two screens that only read.
   - (c) An item of the Logs screen (10): no new main-menu item, but Logs shows hardn's own log, and this is about the host's.

   *Gates the real screen unit only; the mocks draw the screen itself.*

9. **Whether Run All sets it.** Run All applies every control hardn.yml asks for.
   - (a) *Recommended:* yes, where hardn.yml names a server, with the same changes as Set server, the rsyslog install on a journald host among them, in Run All's one review. With no server, it is skipped and not listed. Run All never stops forwarding.
   - (b) No: Log Forwarding is a screen action only, and a host built by Run All alone does not forward.

   *Gated by 1 and 4.*

10. **The main menu's status rows and the risk level.** The main menu draws a row per control and a risk level from them.
    - (a) *Recommended:* a "Log Forwarding" row only where hardn.yml names a server or the host forwards, with the screen's own words (Configured and the server; Not Configured, "not forwarding"; Partial, "server not reached" or "not hardn.yml's server"). The risk level never counts it: a host with no syslog server to send to has nothing to fix.
    - (b) Always a row, and Not Configured raises the risk level: every host without a server reads worse.
    - (c) No row: the screen alone says it.

11. **How the change is confirmed, and its undo.** Shown on `review` and `review-stop`.
    - (a) *Recommended, drawn:* a plain y/N. Forwarding cannot lock anyone out, so there is no typed confirmation and no confirm-or-revert timer. The file is checked (`rsyslogd -N1`) before the restart, and a failed check or restart rolls the change back, as any change does. The undo is the item "Stop forwarding", offered only for hardn's own setting: it deletes hardn's file (on Alpine, takes its options back out) and restarts the logger. It leaves rsyslog installed, and says so.
    - (b) Stop forwarding also removes an rsyslog hardn installed: the host is as before, and hardn needs a record of the install, as Auto Updates keeps.
    - (c) No undo item: delete the file by hand.

12. **How the screen says forwarding works.** The host cannot know what the server received (§3.5). Shown on `tcp`, `tcp-down` and `udp`, and as the reviews' last step.
    - (a) *Recommended, drawn:* two things. A Connection row for TCP and TLS, "Open" or a warning "None", from the host's sockets; with none the status row is Partial, "server not reached". UDP has no such row. And Set server ends by logging one line (`logger -t hardn 'log forwarding set'`) for the administrator to find on the server.
    - (b) The test line alone: nothing on the screen claims the server is reached.
    - (c) Neither: the screen shows the setting and nothing about delivery.

13. **hardn's own audit log.** `/var/log/hardn/audit.jsonl` is a file root can edit; the roadmap's point is that a forwarded copy is not. Forwarding the host's logs does not carry that file.
    - (a) *Recommended:* yes, as its own unit after this one: hardn also writes each audit record to the local logger, so whichever forwarder the host has carries it, busybox included. Nothing on this screen changes.
    - (b) Have rsyslog read the file (its `imfile` input): no change to hardn's writer, and nothing for Alpine.
    - (c) No: the audit log stays on the host alone.

14. **Whether `hardn check` compares it.** `hardn check`, built on another branch and not in this one, compares the host with a baseline it saved earlier, not with hardn.yml.
    - (a) *Recommended:* yes: the forwarding state (whether the host forwards, to which server, by which protocol) would be one more fact in that baseline, and drift is that fact changed since the baseline was saved.
    - (b) No: the screen alone shows it.

    *Adopted as (a), and not built here: a follow-up once both branches are on main.* (Corrected 2026-11-13: this decision first said `hardn check` compares the host with hardn.yml.)

## 5. Words

Proposed, decided with §4.

- Title "Log Forwarding"; section "Forwarding"; the status row "Log Forwarding": "not forwarding", "server not reached", "not hardn.yml's server".
- Rows: "Logger" (rsyslog, busybox syslogd, "journald alone" with "no syslog forwarder"); "Server" (None); "Protocol" (TCP "not encrypted", UDP "not encrypted, losses unseen", TLS); "CA File"; "Connection" (Open, None); "File"; "Set by" (hardn, Not hardn); "hardn.yml" ("No server", "differs").
- Items: "Set server" (the server, "Installs rsyslog", "No server in hardn.yml", "busybox syslogd is UDP only", "Can't read rsyslog's settings", "Set outside hardn"); "Stop forwarding" ("Undo hardn's change").
- Questions: "Forward logs to <server>?", "Stop forwarding logs?".
- Steps: "Install rsyslog", "Install rsyslog-gnutls", "Write hardn's forwarding file", "Restart rsyslog", "Set syslogd's server", "Restart syslogd", "Send a test line", "Delete hardn's forwarding file".
- Notices: "Every message the host logs goes to the server, as it is logged."; "rsyslog is installed to forward the journal. It also keeps its own log files under /var/log."; "busybox syslogd forwards over UDP alone: not encrypted, and a lost message is not noticed."; "Logs stay on the host alone. rsyslog stays installed."
- Refusals, not drawn: "hardn.yml names no log server. Add logForwarding's server, then try again."; "busybox syslogd forwards over UDP only. Set protocol: udp in hardn.yml, then try again."; "rsyslogd can't read its settings: <its error>. Fix that, then try again."

## 6. Made here

- A state the user can fix (no server in hardn.yml, a protocol busybox has not, settings unread) keeps Set server offered, its description saying what is in the way; choosing it refuses with words. A state of the host itself (set outside hardn) is unavailable. As Auto Updates' R1.
- Set server is 1 wherever it shows; Stop forwarding is 1 alone and 2 beside it.
- "File" and "Set by" are two rows: the path and "hardn" do not fit one row at 80 columns.
- The hardn.yml row is always drawn, so the screen says what Set server would do before it is chosen.
- hardn's file is `/etc/rsyslog.d/60-hardn-forward.conf`, opening with "# Written by hardn (Syslog Settings).", as Auto Updates' file does. The name and the number are the mock's.
- The TLS mock names the server by name, not address: the certificate is checked against it.
- No mock for Rocky, Alma, RHEL or Proxmox VE: their screens would be `off` and `journald` with another name in the note.

## 7. For the real unit

*Since the real unit: [screen-logforward.md](screen-logforward.md) §4 says which of these it settled, and how; what only a host can show is still open.*

- **Reading rsyslog.** Which files forward and where. `rsyslogd -o` writes the configuration as read (§3.2); whether every supported release's rsyslogd has it, and the older `@` forms, were not checked. Every string made safe to show.
- **The queue.** The mock writes `queue.type="linkedList"` alone. How much is kept while the server is away, and whether on disk, is the unit's to set.
- **The CA file on Ubuntu** (AppArmor, the `syslog` user) and **the port on Rocky, Alma and RHEL** (SELinux): §3.2's unverified points, to settle on VMs.
- **Debian 13 after the install:** whether journald's messages reach rsyslog unaided (§3.3).
- **The install on a journald host** brings rsyslog's local files; whether hardn's review should name them.
- **Alpine:** an `/etc/conf.d/syslog` the administrator already edited, and one that already has `-R`.
- **Proxmox VE 9:** its default logger, and whether the screen changes anything there.
- **A name for the server** is resolved when rsyslog connects: a host whose DNS is down forwards nothing.
- **The firewall:** hardn's ufw rules filter incoming traffic; whether an outgoing rule ever blocks the server was not looked at.
- Nothing ran on Linux.
