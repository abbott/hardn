# Time sync mock

> **Renamed 2026-11-14.** The screen and its Configuration item are now "NTP Settings"; its status row, the main menu's row and the hardn.yml screen's section are "NTP". The main menu's last rows are Syslog, NTP, AppArmor. The `ntpServers` key, the `NTP_*` codes and the control id are unchanged. The record below keeps the old name, Time Sync.

The mock unit of the roadmap's "Time sync". hardn would show which service keeps the host's time, whether it runs, whether the clock is in sync and which servers it uses; and set the servers hardn.yml names on the service the host already runs. This unit draws that screen and its reviews, surveys how each supported release keeps time, lists what the maintainer has to decide (§4), and plans what the real unit shares with Log Forwarding (§5).

**Adopted 2026-11-14.** The maintainer adopted the recommended answer of each of §4's eighteen decisions: "Adopt all". §5's extraction is built (§5.5), and so is the real unit: [screen-timesync.md](screen-timesync.md). From here on this page is the mock's record; where it says nothing is wired, read that page.

Status: built and tested on macOS in process. No hardn binary ran. Nothing ran on Linux: every fact about a host in §3 is read from an upstream source, linked, or marked unverified. Nothing is wired into hardn: no menu item, no control, no reader, no hardn.yml key.

## 1. What to run

```
go run ./internal/tools/uigallery timesync [-ascii] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery timesync -list                    # the mocks' names
go run ./internal/tools/uigallery play timesync[/host]              # the real screen on a simulated host
```

Since the real unit, each mock is hardn's own drawing: the control's reading of a simulated host (`pkg/host/timesync/timefake`) through the screen's mapping, and each review the real screen up to its question (`internal/tools/uigallery/timesync.go`). These drawings replaced the hand-written goldens. Three mocks were added (`review-pve`, `refuse-no-servers`, `refuse-not-read`); `no-service` is Debian 13 with no time service; `not-read` is a line hardn cannot make out, its clock still read.

## 2. The mocks

One host, `web-01`, on a network with its own time servers, `192.0.2.10` and `192.0.2.11`.

| Mock | What it shows |
| --- | --- |
| `default` | Ubuntu 26.04, chrony, in sync on Ubuntu's own pools; hardn.yml names no servers: the item says so |
| `off` | The same host, hardn.yml naming the two servers: "Set servers" |
| `hardn` | In sync on hardn's servers, set by hardn's file; Ubuntu's pools switched off: "Remove servers" |
| `not-synced` | hardn's servers set, neither answers: Clock "Not in sync", Synced to "None", the row Partial |
| `stopped` | chrony installed and not running: a warning, Set servers unavailable |
| `no-service` | No time service: a warning, Set servers unavailable |
| `foreign` | `site.sources`, not hardn's, names a server: "in site.sources", Set servers unavailable, "Set outside hardn" |
| `dhcp` | A server handed out by DHCP beside the pools: "from DHCP"; Set servers stays offered |
| `mismatch` | hardn's file names two servers, hardn.yml now names another second: the hardn.yml row warns, Set servers is 1 and Remove servers 2 |
| `timesyncd` | Ubuntu 24.04, systemd-timesyncd, in sync on the server built into it: "built in" |
| `alpine` | Alpine 3.23, busybox ntpd: no Synced to row, as ntpd does not say |
| `container` | A container: no service, a muted line, the row Not Applicable, Set servers unavailable |
| `not-read` | chrony's settings cannot be read: the row, the clock and the servers Unknown |
| `review` | `off`'s Set servers up to its question: hardn's file, Ubuntu's five pools commented out, `chronyc reload sources` |
| `review-rhel` | Set servers on Rocky Linux 10: `/etc/chrony.conf` edited in place, then a restart |
| `review-timesyncd` | `timesyncd`'s Set servers: a drop-in with `NTP=`, then a restart |
| `review-alpine` | `alpine`'s Set servers: one line of `/etc/conf.d/ntpd`, then a restart |
| `review-remove` | `hardn`'s Remove servers, the undo: hardn's file deleted, the pools back on |

`hardn`, drawn:

```
# Time Sync ####################################################

  [+] Time Sync            Configured 192.0.2.10

  Time ---------------------------------------------------------

   -  Service              chrony
  [+] Clock                In sync
   -  Synced to            192.0.2.10
   -  Server               192.0.2.10 by hardn
   -  Server               192.0.2.11 by hardn
   -  File                 /etc/chrony/sources.d/hardn.sources
   -  hardn.yml            192.0.2.10, 192.0.2.11

  Select an option

   1) Remove servers       Undo hardn's change

   0) Return

    > Option [0-1 or q]:
```

`review-rhel`'s plan, drawn:

```
   -  Set chrony's servers
      /etc/chrony.conf
      @@ -1,7 +1,10 @@
       # Use four public NTP servers from the pool.ntp.org
      | project. If this
       # host has a static public IP address, please consider
      | joining the pool:
       # https://www.ntppool.org/join.html
      -pool 2.rocky.pool.ntp.org iburst
      +#hardn: pool 2.rocky.pool.ntp.org iburst
      +# Added by hardn (NTP Settings): the 2 lines below.
      +server 192.0.2.10 iburst
      +server 192.0.2.11 iburst

       # Use NTP servers from DHCP.
       sourcedir /run/chrony-dhcp

   -  Restart chrony
      chronyd
      $ systemctl restart chronyd

  Set time servers? [y/N, 0 or q]:
```

## 3. Survey

Read on 2026-11-13. "Unverified" means no upstream source was fetched for it. None of it was run. One thing was observed, not fetched: an Ubuntu 26.04 test VM running chronyd, listening on 127.0.0.1:323/udp.

### 3.1 The time service each release has

| Release | Service by default | Source |
| --- | --- | --- |
| Debian 13, 12 | systemd-timesyncd | Both cloud images list `systemd-timesyncd` and no chrony ([13](https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.json), [12](https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.json)). The package is `Priority: standard` in Debian 13 ([control](https://salsa.debian.org/systemd-team/systemd/-/raw/debian/trixie/debian/control)). Other install media, and Debian 12's priority: unverified |
| Ubuntu 26.04 | chrony 4.8 | The server cloud image's package list has `chrony` and no `systemd-timesyncd` ([manifest](https://cloud-images.ubuntu.com/resolute/current/resolute-server-cloudimg-amd64.manifest)); and the VM observed. Other install media: unverified |
| Ubuntu 24.04, 22.04 | systemd-timesyncd | The server cloud images' package lists ([24.04](https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.manifest), [22.04](https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.manifest)). Other install media: unverified |
| Alpine 3.24, 3.23, 3.22 | busybox ntpd where one was chosen; possibly none | busybox is built with `CONFIG_NTPD=y` on each branch ([3.24](https://raw.githubusercontent.com/alpinelinux/aports/3.24-stable/main/busybox/busyboxconfig), [3.23](https://raw.githubusercontent.com/alpinelinux/aports/3.23-stable/main/busybox/busyboxconfig), [3.22](https://raw.githubusercontent.com/alpinelinux/aports/3.22-stable/main/busybox/busyboxconfig)). Nothing starts it unasked: `setup-ntp` asks "Which NTP client to run? ('busybox', 'openntpd', 'chrony' or 'none')", with busybox its default answer, and adds the chosen service to the default runlevel ([setup-ntp](https://raw.githubusercontent.com/alpinelinux/alpine-conf/master/setup-ntp.in), master; the version each release ships was not matched). So an Alpine host may run busybox ntpd, chrony, openntpd or nothing. Cloud images: unverified |
| Proxmox VE 9 | chrony | The admin guide: "As of Proxmox VE 7, chrony is used as the default NTP daemon", its file `/etc/chrony/chrony.conf` ([system-timesync.adoc](https://git.proxmox.com/?p=pve-docs.git;a=blob_plain;f=system-timesync.adoc;hb=HEAD)). `proxmox-ve` and `pve-manager` do not depend on it ([proxmox-ve](https://git.proxmox.com/?p=proxmox-ve.git;a=blob_plain;f=debian/control;hb=HEAD), [pve-manager](https://git.proxmox.com/?p=pve-manager.git;a=blob_plain;f=debian/control;hb=HEAD)), so what puts it on a node, and whether its package is Debian's unchanged: unverified |
| Rocky, Alma, RHEL 10, 9 | chrony | CentOS Stream's package groups list chrony as mandatory ([9](https://gitlab.com/redhat/centos-stream/release-engineering/comps/-/raw/main/comps-centos-stream-9.xml.in), [10](https://gitlab.com/redhat/centos-stream/release-engineering/comps/-/raw/c10s/comps-centos-stream-10.xml.in)). Rocky's, Alma's and RHEL's own lists and packages, and their cloud images: unverified |

On Debian and Ubuntu the two packages cannot be installed together: each `Conflicts: time-daemon` and `Provides: time-daemon` ([chrony](https://salsa.debian.org/debian/chrony/-/raw/debian/latest/debian/control), systemd's control above). A host has one or the other.

### 3.2 chrony (Ubuntu 26.04, Proxmox VE 9, Rocky, Alma, RHEL; anywhere it was installed)

- **The directives.** `server <name or address> [options]` and `pool <name> [options]`; a pool's name resolves to several servers, four by default (`maxsources`) ([chrony.conf](https://gitlab.com/chrony/chrony/-/raw/master/doc/chrony.conf.adoc)). `sourcedir <directory>` reads files ending `.sources`, which hold only `server`, `pool` and `peer` lines and "can be reloaded by the reload sources command in chronyc"; `confdir <directory>` reads files ending `.conf`. No directive takes a source away: a `pool` line in one file cannot be undone from another.
- **Debian's files** (Debian 13 and 12, so Proxmox VE if its package is Debian's). `/etc/chrony/chrony.conf` has `pool 2.debian.pool.ntp.org iburst`, `sourcedir /run/chrony-dhcp`, `sourcedir /etc/chrony/sources.d` and `confdir /etc/chrony/conf.d` ([13](https://salsa.debian.org/debian/chrony/-/raw/debian/trixie/debian/chrony.conf), [12](https://salsa.debian.org/debian/chrony/-/raw/debian/bookworm/debian/chrony.conf)). The file is managed by ucf, which merges it on upgrade ([postinst](https://salsa.debian.org/debian/chrony/-/raw/debian/trixie/debian/postinst)). chrony is 4.6.1 on Debian 13 and 4.3 on Debian 12 ([sources.debian.org](https://sources.debian.org/api/src/chrony/)); `sourcedir` and `reload sources` came with 4.1 ([NEWS](https://gitlab.com/chrony/chrony/-/raw/master/NEWS)).
- **Ubuntu 26.04's files.** `/etc/chrony/chrony.conf` has the same two `sourcedir` lines and the `confdir`, and no pool ([chrony.conf](https://git.launchpad.net/ubuntu/+source/chrony/plain/debian/chrony.conf?h=ubuntu/resolute)). The pools are in `/etc/chrony/sources.d/ubuntu-ntp-pools.sources`, which the package's postinst installs with ucf unless a debconf answer says not to ([postinst](https://git.launchpad.net/ubuntu/+source/chrony/plain/debian/postinst?h=ubuntu/resolute), [templates](https://git.launchpad.net/ubuntu/+source/chrony/plain/debian/templates?h=ubuntu/resolute)): four lines `pool N.ntp.ubuntu.com iburst maxsources 1 nts prefer` and `pool ntp-bootstrap.ubuntu.com iburst maxsources 1 nts certset 1` ([the file](https://git.launchpad.net/ubuntu/+source/chrony/plain/debian/ubuntu-ntp-pools.sources?h=ubuntu/resolute)). So Ubuntu 26.04's defaults are authenticated (NTS, over 4460/tcp as well as 123/udp).
- **CentOS Stream's file** (so Rocky, Alma and RHEL, unverified). `/etc/chrony.conf` is chrony's example with the pool renamed after the distribution's os-release `ID` and `sourcedir /run/chrony-dhcp` added ([spec, c10s](https://gitlab.com/redhat/centos-stream/rpms/chrony/-/raw/c10s/chrony.spec), [c9s](https://gitlab.com/redhat/centos-stream/rpms/chrony/-/raw/c9s/chrony.spec), [the example](https://gitlab.com/chrony/chrony/-/raw/master/examples/chrony.conf.example2)). It reads no directory under `/etc`: there is no drop-in for an administrator. The pool's name on Rocky (`2.rocky.pool.ntp.org` in the mock) is what that build rule would give, not read from Rocky's package.
- **Alpine's file**, where chrony was chosen: `pool pool.ntp.org iburst`, and `cmdport 0` ([chrony.conf, 3.23](https://raw.githubusercontent.com/alpinelinux/aports/3.23-stable/main/chrony/chrony.conf)), which closes chronyc's UDP port but "does not disable the Unix domain command socket" (chrony.conf, `cmdport`). Whether chronyc as root reads it there: unverified.
- **Authenticated and plain servers together.** By default (`authselectmode mix`), where both are named, every authenticated source gets `require` and `trust`: chronyd updates the clock only while one of them is selectable, and follows a plain server only if it agrees with them (chrony.conf, `authselectmode`, `require`, `trust`). So on Ubuntu 26.04 a plain server added beside the NTS pools sets the clock only while those pools are reached.
- **Servers from DHCP.** Added, not swapped in. Debian's dhclient hook writes `server <address> iburst` lines to `/run/chrony-dhcp/<interface>.sources` and runs `chronyc reload sources` ([chrony.dhcp](https://salsa.debian.org/debian/chrony/-/raw/debian/trixie/debian/chrony.dhcp)); CentOS Stream installs chrony's NetworkManager dispatcher script, which does the same ([spec](https://gitlab.com/redhat/centos-stream/rpms/chrony/-/raw/c10s/chrony.spec), [script](https://gitlab.com/chrony/chrony/-/raw/master/examples/chrony.nm-dispatcher.dhcp)). Which DHCP client each release runs, and so whether the hook fires: unverified.
- **cloud-init.** Only where the user data has an `ntp` key ([cc_ntp.py](https://raw.githubusercontent.com/canonical/cloud-init/main/cloudinit/config/cc_ntp.py), `if "ntp" not in cfg`). It then writes the whole main file from a template: `/etc/chrony/chrony.conf` on Debian and Ubuntu, `/etc/chrony.conf` elsewhere. Debian's template has no `sourcedir` and no `confdir` ([template](https://raw.githubusercontent.com/canonical/cloud-init/main/templates/chrony.conf.debian.tmpl)), so on such a host chrony reads no drop-in. Which images set `ntp`: unverified.
- **Reload.** `chronyc reload sources` for a `.sources` file; anything else needs a restart. The unit is `chrony.service` on Debian ([unit](https://salsa.debian.org/debian/chrony/-/raw/debian/trixie/debian/chrony.service)) and `chronyd.service` on CentOS Stream (the spec). Proxmox's guide restarts `chronyd`; whether that is an alias on Debian: unverified.
- **Reading it.** `chronyc -n tracking` prints `Reference ID    : CB00710F (192.0.2.10)` and `Leap status     : Normal` or `Not synchronised`; `chronyc -n sources` prints a line a source, `^* 192.0.2.10 ...`, where `*` is the one followed and `?` one not reached; `-c` prints both as CSV; `-N` prints names as the files have them ([chronyc](https://gitlab.com/chrony/chrony/-/raw/master/doc/chronyc.adoc)). The CSV's columns: unverified.

### 3.3 systemd-timesyncd (Debian 13, 12; Ubuntu 24.04, 22.04)

- **The files.** `/etc/systemd/timesyncd.conf` and drop-ins `/etc/systemd/timesyncd.conf.d/*.conf`, section `[Time]` ([timesyncd.conf(5)](https://www.freedesktop.org/software/systemd/man/latest/timesyncd.conf.html)).
- **The directives.** `NTP=` is "a space-separated list of NTP server host names or IP addresses"; timesyncd "will contact all configured system or per-interface servers in turn, until one responds". `FallbackNTP=` is "only relevant if no other NTP server information is known", and without it "a compiled-in list". That list is `ntp.ubuntu.com` on Ubuntu and `0`–`3.debian.pool.ntp.org` on Debian ([Debian 13](https://salsa.debian.org/systemd-team/systemd/-/raw/debian/trixie/debian/rules), [12](https://salsa.debian.org/systemd-team/systemd/-/raw/debian/bookworm/debian/rules), [Ubuntu 24.04](https://git.launchpad.net/ubuntu/+source/systemd/plain/debian/rules?h=ubuntu/noble); 22.04: unverified). So a host as installed has no server in any file. The man page names no option for NTS.
- **Servers from DHCP.** Added: `NTP=` is "combined with any per-interface NTP servers acquired from systemd-networkd", and networkd's `UseNTP=` is on by default ([systemd.network(5)](https://www.freedesktop.org/software/systemd/man/latest/systemd.network.html)). Only where networkd manages the interface; with another network manager: unverified.
- **cloud-init**, with an `ntp` key: `/etc/systemd/timesyncd.conf.d/cloud-init.conf`, an `NTP=` line (cc_ntp.py and its [template](https://raw.githubusercontent.com/canonical/cloud-init/main/templates/timesyncd.conf.tmpl)).
- **Reload.** `systemctl restart systemd-timesyncd`, as Proxmox's guide does. Whether a reload is enough: unverified.
- **Reading it.** `timedatectl show` has `NTPSynchronized`, which "shows whether the kernel reports the time as synchronized" ([org.freedesktop.timedate1](https://www.freedesktop.org/software/systemd/man/latest/org.freedesktop.timedate1.html)); `timedatectl show-timesync` is "machine readable" and has `ServerName`, `ServerAddress`, `SystemNTPServers`, `LinkNTPServers`, `FallbackNTPServers` ([timedatectl(1)](https://www.freedesktop.org/software/systemd/man/latest/timedatectl.html), [org.freedesktop.timesync1](https://www.freedesktop.org/software/systemd/man/latest/org.freedesktop.timesync1.html)). The exact lines printed: unverified.

### 3.4 busybox ntpd (Alpine)

From busybox's [ntpd.c](https://raw.githubusercontent.com/mirror/busybox/master/networking/ntpd.c) (master; the versions Alpine ships were not matched) and Alpine's service ([init script](https://raw.githubusercontent.com/alpinelinux/aports/3.23-stable/main/busybox/ntpd.initd), [conf.d](https://raw.githubusercontent.com/alpinelinux/aports/3.23-stable/main/busybox/ntpd.confd); 3.22 and 3.24 the same):

- **The file.** No drop-in directory. The service runs `ntpd ${NTPD_OPTS:--N -p pool.ntp.org} -n`, and `/etc/conf.d/ntpd` is the one line `NTPD_OPTS="-N -p pool.ntp.org"`.
- **The option.** `-p PEER`, "Obtain time from PEER (may be repeated)". With no `-p`, `server HOST` lines of `/etc/ntp.conf` are used (`CONFIG_FEATURE_NTPD_CONF=y`, §3.1's files). No option prefers one peer; how ntpd chooses among several: unverified.
- **DHCP and cloud-init.** Nothing in the service reads DHCP's servers: unverified beyond the two files. cloud-init on Alpine writes `/etc/ntp.conf` (cc_ntp.py), which ntpd reads only with no `-p`.
- **Reload.** Options are read at start, so `rc-service ntpd restart`.
- **Reading it.** ntpd has no query command. What is left is the kernel's own flag, which the other two report too: `adjtimex(2)` returns `TIME_ERROR` while `STA_UNSYNC` is set, "The system clock is not synchronized to a reliable server" ([adjtimex(2)](https://git.kernel.org/pub/scm/docs/man-pages/man-pages.git/plain/man/man2/adjtimex.2)). busybox has an `adjtimex` applet (`CONFIG_ADJTIMEX=y`) that prints the status and the return value ([adjtimex.c](https://raw.githubusercontent.com/mirror/busybox/master/miscutils/adjtimex.c)). Which server ntpd follows cannot be read.

### 3.5 Containers

A container has no clock of its own: the kernel has no time namespace for the wall clock, as Ubuntu's chrony notes say ([README.container](https://git.launchpad.net/ubuntu/+source/chrony/plain/debian/README.container?h=ubuntu/resolute)).

- systemd-timesyncd does not start: `ConditionVirtualization=!container` ([unit](https://raw.githubusercontent.com/systemd/systemd/main/units/systemd-timesyncd.service.in)).
- Debian's chrony does not start in a container (`ConditionVirtualization=|!container`, the unit of §3.2). Ubuntu's starts with `-x`, which "disables the control of the system clock" ([starter](https://git.launchpad.net/ubuntu/+source/chrony/plain/debian/chronyd-starter.sh?h=ubuntu/resolute), [chronyd](https://gitlab.com/chrony/chrony/-/raw/master/doc/chronyd.adoc)): a chronyd that runs and sets nothing. chrony's own unit, CentOS Stream's, needs `CAP_SYS_TIME` ([unit](https://gitlab.com/chrony/chrony/-/raw/master/examples/chronyd.service)).
- Alpine's ntpd in a container, and whether a container may read the kernel's flag: unverified.

## 4. Decisions for the maintainer

Eighteen. Drawn: the recommended answer of each but 9, 10, 11, 16 and 17, which change nothing on this screen. An earlier one gates the later ones it names. Six fit Log Forwarding's adopted answer and say so in one line (7, 13, 14, 15, 16, 17), and 2 takes its form from there; the rest are new for time.

1. **Whether hardn's servers replace the host's own or go beside them.** New. The host already has servers: a `pool` line (chrony), a built-in list (systemd-timesyncd), `-p pool.ntp.org` (busybox ntpd). Shown on `hardn`, every review, and the Server rows.
   - (a) *Recommended, drawn:* replace. After Set servers the service uses hardn's servers alone, and whatever DHCP hands out (decision 8). systemd-timesyncd does this by itself: `NTP=` in hardn's drop-in, and the built-in list is then unused (§3.3). busybox ntpd: hardn's `-p` in place of the others, on the one line. chrony cannot have a `pool` line undone from another file (§3.2), so hardn comments it out where it stands, `#hardn: pool …`, and Remove servers takes the mark off again. That edits a file hardn did not write, which Log Forwarding never does outside Alpine: `/etc/chrony/chrony.conf` or `/etc/chrony.conf`, and on Ubuntu 26.04 `ubuntu-ntp-pools.sources`. Both Debian's and Ubuntu's are merged by ucf on a package upgrade; what ucf then asks: unverified.
   - (b) Beside: hardn adds its servers, marked `prefer` on chrony, and edits no other file. Works on Debian's chrony. On Ubuntu 26.04 it does not do what was asked: the defaults are authenticated, so chrony sets the clock only while Ubuntu's pools are reached, and follows hardn's servers only where they agree (§3.2). On a network closed to the internet the clock would not be set at all.
   - (c) A hardn.yml key that chooses: one more key, and (b)'s problem stays for whoever picks it.

   *Gates 3, 5, 8 and the reviews.*

2. **The hardn.yml key: its name, how many servers, names or addresses.** New, but its form is as Log Forwarding (decision 1): typed in the file, no form on the screen, nothing set by default.
   - (a) *Recommended, drawn:* a list, one to four, each an IPv4 address or a host name, checked as `logForwarding`'s server is (IPv6 refused for now, as there):

     ```yaml
     ntpServers:
       - "192.0.2.10"
       - "192.0.2.11"
     ```

     Order is kept: systemd-timesyncd tries them in turn (§3.3). Four is `pool`'s own default count; chrony's example file recommends "at least three NTP servers to be able to detect one server providing incorrect time", and hardn says nothing about a shorter list: one own server is the roadmap's case.
   - (b) Addresses only: no DNS needed to keep time, and a server that moves needs every hardn.yml edited.
   - (c) A mapping with options per server (`{server, prefer, nts}`): room for decisions 3 and 4, and more to check.

   *Gates 3, 15, 16 and 17.*

3. **`iburst`, `prefer` and the like: offered or fixed.** New. chrony's per-server options; systemd-timesyncd and busybox ntpd have none.
   - (a) *Recommended, drawn:* fixed, not offered. hardn writes `server <name> iburst` on chrony, as every distribution's own file and DHCP hook do (§3.2), and no `prefer`: with decision 1 (a) there is nothing to prefer them over.
   - (b) A key for options: for a site that wants `prefer`, `minpoll`, `xleave`; chrony only, and each is a string hardn must check before writing.

4. **NTS, authenticated NTP: now or later.** New. Only chrony has it among the three (§3.2, §3.3, §3.4), and Ubuntu 26.04 uses it by default.
   - (a) *Recommended:* later. hardn's servers are plain NTP on every service. On Ubuntu 26.04 that moves a host from authenticated public servers to unauthenticated own ones, and the review's notice should say so there (not drawn: the words are decision 1's to settle first).
   - (b) Now, chrony only: `nts: true` in hardn.yml writes the `nts` option; the own server must run NTS-KE, and a private CA needs `ntstrustedcerts`. A second size of unit.
   - (c) Refuse Set servers where the host's own servers use NTS: never lowers a host, and Ubuntu 26.04 then cannot be set at all.

5. **A host whose time service is stopped, or that has none.** New, and not Log Forwarding's decision 4, which installs rsyslog. Shown on `stopped` and `no-service`.
   - (a) *Recommended, drawn:* show only. The Service row warns ("chrony not running", "None"), the status row is Not Configured, and Set servers is unavailable. hardn installs no time service, starts none and swaps none, as the roadmap says. An Alpine set up with `none` (§3.1) reads this way.
   - (b) Set servers also starts and enables a stopped service: one step more, and it overrules whoever stopped it.
   - (c) Install one where there is none (chrony): every host can then be set, and hardn chooses a base service for the host.

6. **Rocky, Alma and RHEL, where chrony reads no drop-in.** New; it is to chrony what Alpine is to Log Forwarding's decision 5. Shown on `review-rhel`.
   - (a) *Recommended, drawn:* edit `/etc/chrony.conf` in place: the pool commented out (decision 1), hardn's lines under a comment naming them, `systemctl restart chronyd`. The same is done on any chrony host whose main file reads no `sources.d`, such as one cloud-init wrote (§3.2).
   - (b) Add one `sourcedir` line to `/etc/chrony.conf` and keep hardn's servers in a file of their own: still an edit of that file, and a reload in place of a restart afterwards.
   - (c) Read only there: nothing of the distribution's is edited, and the EL hosts cannot be set.

7. **Servers set by a file hardn did not write.** As Log Forwarding (decision 7): hardn shows it and Set servers is unavailable, "Set outside hardn"; hardn never edits or removes that file. Here "not hardn's" means a server in a drop-in that is neither hardn's nor one the distribution's package installs (`foreign`: `site.sources`). A line in the service's main file counts as the distribution's, whoever wrote it, and decision 1 applies to it.

8. **Servers handed out by DHCP.** New. chrony and systemd-timesyncd both add them to the others (§3.2, §3.3). Shown on `dhcp`.
   - (a) *Recommended, drawn:* shown, "from DHCP", and left alone. Set servers stays offered and its review lists them beside hardn's.
   - (b) Set servers also turns them off (chrony's `sourcedir /run/chrony-dhcp` commented out; networkd's `UseNTP=no`): hardn's servers truly alone, and two more files of the host's edited, one of them the network's.
   - (c) Set servers unavailable while DHCP hands out servers: nothing surprising, and most DHCP networks could never be set.

9. **Where it lives in the menu.** *Revised by the maintainer on 2026-11-14: Time Sync is no main-menu item. It lives under `8) Configuration`, as its item 3, with hardn.yml (1) and Log Forwarding (2); the main menu keeps v0.9.0's ten items and keys ([screen-config.md](screen-config.md) §1.1). The adopted answer, (a), is kept below as the record.* New. The main menu today: 1 User Management, 2 SSH Login, 3 Firewall, 4 DNS, 5 Auto Updates, 6 Log Forwarding, 7 Run All, 8 Backup, 9 Configuration, 10 System Details, 11 Logs.
   - (a) *Recommended:* its own item, 7, "Time Sync", "Keep the clock in sync", after Log Forwarding with the other things hardn sets. Run All, Backup, Configuration, System Details and Logs each move down one (8 to 12), the hint reads `[0-12 or q]`, and every page and golden that names those keys changes, as when Log Forwarding went in. Whether twelve items fit an 80×24 terminal with the status rows: unverified, the real unit's heights test says.
   - (b) Item 12, after Logs: nothing moves, and a control sits after the screens that only read.
   - (c) An item of the DNS screen (4), renamed for both: no new main-menu item, and two unrelated services behind one name.

   *Gates the real screen unit only; the mocks draw the screen itself.*

10. **Whether "Show it" also appears on System Details and in the Markdown report.** New. The roadmap's first step is read only; System Details (main menu 9) and `hardn system-details --output markdown` are where hardn reports a host.
    - (a) *Recommended:* yes, three rows there, "Time service", "Clock" and "Synced to", from the same reading as this screen, with no servers list and no item. A report handed over then says whether the host's clock can be trusted, which its log times depend on.
    - (b) This screen only: one place, and the report says nothing of the clock.
    - (c) System Details only, and no Time Sync item until Set servers is built: the roadmap's two steps as two releases.

11. **Whether a clock that is not in sync is a status row, a warning, or counts in the risk level.** New. Log Forwarding's row shows only where it is configured (its decision 10), because a host with no log server has nothing to fix. Every host has a clock.
    - (a) *Recommended:* a "Time Sync" row on the main menu on every host but a container: Configured with the server followed ("in sync" on Alpine), Partial "not in sync" or "not hardn.yml's servers", Not Configured "no time service" or "chrony not running", Unknown "can't read". No callout above the menu. The risk level does not count it: hardn cannot fix a host with no time service (decision 5), and a clock out of sync for a minute after boot would raise the level and drop it again.
    - (b) It counts in the risk level: a host that cannot keep time reads worse, as it is, and so does every host seen at the wrong moment.
    - (c) A warning callout on the main menu while the clock is not in sync, as the end-of-support notice is drawn: hard to miss, and one more thing above the menu.
    - (d) No row: the screen alone says it.

12. **What a container shows and is offered.** New. Its clock is its host's (§3.5). Shown on `container`.
    - (a) *Recommended, drawn:* the screen opens and says so: Service "None", the Clock row from the kernel's flag, a muted line "In a container: the host keeps the time.", the row Not Applicable, Set servers unavailable, "The host keeps the time". No main-menu row (decision 11). A chronyd that runs there, as Ubuntu's does with `-x`, sets no clock and is shown no differently. Whether a container may read the flag is unverified: if not, the Clock row is Unknown.
    - (b) No Time Sync item in a container: shorter menu, and the keys differ between hosts.
    - (c) As any host: hardn would write servers for a service that cannot set the clock.

13. **How the change is confirmed, and its undo.** As Log Forwarding (decision 11): a plain y/N, since it cannot lock anyone out; a failed write, reload or restart rolls back; the undo is an item, here "Remove servers", offered only for hardn's own setting, which puts the host's own servers back (decision 1) and installs or removes nothing. Shown on `review` and `review-remove`.

14. **How the screen says time sync works.** As Log Forwarding (decision 12), without the test line: the Clock row and the Synced to row are the evidence, read from the service after the change. Unlike a log server, the host can know: there is nothing to look up elsewhere. On Alpine only the Clock row (§3.4).

15. **Whether Run All sets it.** As Log Forwarding (decision 9): yes where hardn.yml names servers, with the same changes as Set servers in Run All's one review; skipped and not listed with none; Run All never removes them. Two options, as there: (a) *recommended*, that; (b) a screen action only, and a host built by Run All alone keeps the distribution's servers. *Gated by 1 and 2.*

16. **Whether `hardn check` includes it.** As Log Forwarding (decision 14): yes, as a follow-up once both branches are on main. `hardn check`, on another branch, compares the host with a baseline it saved. The facts would be the service and the servers it is set to use. One point is new: (a) *recommended,* the clock's state is not in the baseline, since it changes by itself; (b) it is, and a server away for an hour reads as drift; (c) time sync is not checked at all.

17. **A hardn.yml that is not valid, and a state hardn cannot read.** As Log Forwarding (its real unit's §4, item 6): the hardn.yml row says "Not valid" and Set servers "hardn.yml is not valid"; unread settings are Unknown, never "None", and the item refuses with words. Shown on `not-read`.

18. **Proxmox VE 9.** New only in that it differs from Log Forwarding, where Proxmox VE is refused because hardn installs no package there. Time sync installs nothing.
    - (a) *Recommended:* as Debian's chrony: Set servers offered. A cluster depends on its nodes' clocks agreeing (Proxmox's guide, §3.1), so the review would add one line saying to set every node the same way (not drawn).
    - (b) Read only on Proxmox VE, as the Firewall is: nothing hardn does can split a cluster's time, and the administrator sets it by hand.

## 5. Shared with Log Forwarding

The two features are the same shape: read a service's setting, show whose it is, write hardn.yml's value to the service the host already runs, offer the undo. This is the plan for the real unit, from the code as it was on this branch. Nothing here was moved in the mock's unit; §5.5 is what the extraction then moved.

### 5.1 What would move

| Today | Becomes | Why it is the same thing |
| --- | --- | --- |
| `pkg/host/logfwd/busybox.go`: `opts`, `parseOpts`, `opts.bytes`, `optsLine`, `optsValue`, and the mark's reading | `pkg/host/confd`: `Parse(data, key, mark) (Line, error)`, `Line.Words`, `Line.Marked`, `Line.Bytes(words, marked)`, `ErrMalformed` | Alpine's `/etc/conf.d/syslog` and `/etc/conf.d/ntpd` are the same file form: one `KEY="words"` assignment, read exactly or refused, and a comment line naming hardn's words |
| `pkg/config/logforward.go`: `validLogName`, `logName`, and `Resolve`'s address test (IPv4, a host name, IPv6 refused) | `pkg/config/hostname.go`: `ResolveHost(s) (host string, isAddr bool, err error)`, which `LogForwarding.Resolve` and the new `NTPServers.Resolve` call | The same rule for a server typed in hardn.yml |
| `pkg/host/logfwd/logfwd.go`: `serverAddress` and that part of `Server.Check` | `pkg/host/hostaddr`: `Check(name) error`, called by `logfwd.Server.Check` and by time sync's writers | The second check, by whoever writes the file (screen-logforward.md §2), stays a second check |
| `pkg/host/logfwd/logfwd.go`: `readFile` | a helper of `pkg/host`, if it has none already | "Not there" is not an error, for both readers |
| `pkg/engine/controls/logforward/plan.go`: `refuse`, `hostWords`, `checkWords`, `firstLine`, `ids` | `pkg/engine/steps`, beside `Refusal` and `PlanPackage`: `Refuse`, `HostWords(err, prefix, sentinel)`, `FirstLine`, `IDs` | Words and codes are each control's; the making of a refusal is not |
| the same file: `restartArgv`, `restartChange` | `steps.ServiceArgv(facts, systemd unit, OpenRC service, verb)`, `steps.RestartService(id, title, service, argv, after)` | Restart by the init that booted the host, refused otherwise; rolled back by restarting again |
| the same file: `optionsChange` | `steps.RewriteFile(id, title, path, edit)`: read, edit, diff, refuse at apply if the file changed since the plan, keep mode and owner | Time sync needs it three times: `/etc/conf.d/ntpd`, `/etc/chrony.conf`, `ubuntu-ntp-pools.sources` |
| the same file: `fileChange`, without its `rsyslogd -N1` | `steps.WriteOwnFile(id, title, path, data, check)`, `check` optional | hardn's own drop-in, backed up, written atomically |
| `pkg/screens/logforward/logforward.go`: `runner.change`'s order (Preplan, refusal said, fresh reading, notice, `screenkit.Change`) and `StatusRow` | `screenkit.Session.Reviewed(ctx, cfg, control, build)` and `screenkit.StatusRow(id, title, status)` | The same glue word for word; only the control and the view differ |
| `pkg/ui/screen`: `LogForwardChange`, `TimeSyncChange`, and `FirewallChange` already | one `screen.Change{Step, Question, Review}` | Three copies of one struct |
| `pkg/ui/screen/logforward.go` and `timesync.go`: the hardn.yml row ("No …", "Not valid", "differs") and the two-item menu (Set is 1; the undo 1 alone, 2 beside it; unavailable with a reason) | `wantRow(value, none, bad, differs)` and `setUndoItems(...)`, unexported, in `pkg/ui/screen` | Same rule, same words but the noun |
| `internal/tools/uigallery/logforward.go`: `lfReview` (type a key, cut the screen off, keep the review) | `reviewOf(run, key)` in the gallery, for both listings | The real time sync mocks will be drawn the same way |

### 5.2 What stays specific

- **Log Forwarding:** rsyslog's parser (`parseRsyslog`, `legacyServer`, `stripComments`, `parenBody`), `RsyslogFile`, `ReadConn` and `ss`, `Proto`, the CA file, `rsyslog-gnutls`, the journald install, the test line, `-R`/`-L` and `legacyHostPort`.
- **Time sync:** chrony's and timesyncd's readers, the three ways of asking whether the clock is in sync, commenting the host's own `pool` lines out and back, `chronyc reload sources`, the container state.

### 5.3 What must not be shared, though it looks alike

- **`logfwd.Reading` with `Now`, `Own`, `Foreign`.** Log Forwarding has one destination and asks "whose is it". Time sync has a list, each server with its own origin, two of which (DHCP, built in) have no file. One shared "setting" type would bend one to the other.
- **"Set outside hardn".** For logs, anything forwarding that is not hardn's blocks Set server. For time, the distribution's own servers do not block: they are what Set servers replaces (decision 1). The same word on the screen, two rules.
- **The Alpine mark.** Log Forwarding's names the words hardn *added*, and `opts.own` pairs `-R` with its argument to find them. Time sync's names the words hardn *replaced*, to put back. `pkg/host/confd` shares the line's parser and the mark's reading, not what the mark means.
- **The server.** `LogServer` is an address, a port, a protocol and a CA file. A time server is a name. Only the name's check is common.
- **The status.** Log Forwarding's row is hidden unless wanted (`Shown`), and its Partial is "server not reached". Time sync's row is always there, and its Partial is the clock.
- **The undo.** Stop forwarding deletes hardn's setting. Remove servers also restores what hardn switched off.
- **Reload.** rsyslog and syslogd restart. chrony reloads `.sources` files without one; `RestartService`'s rollback is not a reload's.

### 5.4 Risks, and the order

- **Log Forwarding has not run on a Linux host.** Its first bugs will be found on a VM. If the code has moved by then, each one has two possible causes: the original or the move.
- **Its tests pin the words.** `hostWords` trims the prefix `logfwd: ` from an error; a moved function changes that prefix, and so a refusal's text. Log Forwarding's 22 gallery goldens, its play transcripts and its packages' tests are the guard: they must pass unchanged, with no golden rewritten.
- **A helper made for two callers fits two callers.** `RewriteFile` and `confd` are shaped here from one user and one mock. Time sync's real needs (three files, a mark that restores) may reshape them once.
- **Recommended order:** (1) Log Forwarding runs on one host of each family and its fixes land; (2) the extraction of §5.1, alone, no behaviour changed, Log Forwarding's goldens byte-identical; (3) time sync's real unit on the shared code, once §4 is decided. Step 2 does not wait for §4, so it can go while the decisions are read. If step 1 cannot come first, extract only the pure parts first (`confd`, `ResolveHost`, `hostaddr`, `screen.Change`, `Refuse`), whose tests need no host, and leave the engine changes for after.

### 5.5 Built: the extraction

Step (2) of §5.4, on 2026-11-14, after Log Forwarding ran on a host of each family that day (Ubuntu 26.04 rsyslog TCP, Debian 13 journald then rsyslog installed, Alpine 3.23 busybox UDP: set, verify, stop). Nothing of Time sync's real unit was built. Log Forwarding's behaviour is unchanged: no golden was rewritten, and its packages' tests pass as they were. Built and tested on macOS, in process; nothing ran on Linux since the move.

| Was | Is now | Differs from §5.1 |
| --- | --- | --- |
| `logfwd`'s `opts` fields, `parseOpts`'s parser, `opts.bytes`, `optsLine`, `optsValue` | `pkg/host/confd`: `Parse(data, key, mark) (Line, error)`, `Line.Words`, `Line.Marked`, `Line.N`, `Line.HasMark()`, `Line.Bytes(words, marked)`, `*Fault{Line, What}` | `Parse` returns a `*Fault`, not an error, and there is no `confd.ErrMalformed`: the file's name and the sentinel are the caller's, so `logfwd`'s errors read as before (`logfwd: /etc/conf.d/syslog:3: … : forwarding hardn can't make out`, `logfwd.ErrMalformed`, `SYSLOG_NOT_READ`); and an exported sentinel or error type under `pkg/host` or `pkg/engine` must have a code of its own in `pkg/output`'s registry (`TestSentinelsRegistered`, `TestTypedErrors`), which is permanent once released and was not asked for. `N`, the assignment's line, and `HasMark` added: `logfwd` needs both. `Bytes` puts the mark on the line above the assignment, where `SetOpts` put it |
| `config`'s `validLogName`, `logName`, `Resolve`'s address test | `pkg/config/hostname.go`: `ResolveHost(s) (host, isAddr, err)`, `ErrHostIPv6`, `ErrHostName` | The two errors added: `Resolve` words each as it did |
| `logfwd`'s `serverAddress` | `pkg/host/hostaddr`: `Valid(name) bool` | A bool, not `Check(name) error`: no new sentinel to register, as `confd`. `logfwd.Server.Check` keeps its own error (`logfwd: address "…"`, `ErrServer`) |
| `logfwd`'s `readFile` | `host.ReadFileIfThere(ctx, sys, name) (data, ok, err)` | `pkg/host` had none (`steps.readOptional` reads without following a link, another read). `logfwd.readFile` stays, one line adding its `logfwd: ` prefix |
| the control's `refuse`, `hostWords`, `firstLine`, `ids` | `steps.Refuse`, `steps.HostWords(err, prefix, sentinel)`, `steps.FirstLine`, `steps.IDs` (`pkg/engine/steps/refuse.go`) | The control keeps `hostWords(err)`, one line naming its prefix and sentinel |
| `restartArgv`, `restartChange` | `steps.ServiceArgv(facts, unit, service, verb) (argv, ok)`, `steps.RestartService(control, id, title, service, argv, after)` (`service.go`) | `ServiceArgv` returns ok, not a refusal: the words and the code (`SYSLOG_NO_INIT`) are the control's. `RestartService` takes the control's id too: it is the change's resource and its undo's why |
| `optionsChange` | `steps.RewriteFile(ctx, sys, Rewrite{Control, ID, Title, Path, Detail, Unread, Changed}, edit)` | A struct, not four arguments: the apply's words (`Detail`), the refusal of a file not read (`Unread`, `SYSLOG_NOT_READ`) and of one changed since the plan (`Changed`, `SYSLOG_CHANGED_SINCE`) are the control's. `edit`'s error is returned as it is |
| `fileChange` | `steps.WriteOwnFile(OwnFile{Control, ID, Title, Path, Before, Data, After, Check})`, `FileCheck{Argv, Refused, Detail}` | A struct. `Check` is optional; Log Forwarding's is `rsyslogd -N1` |
| `runner.change`'s order, `StatusRow` | `screenkit.Session.Reviewed(ctx, cfg, control, build)`, `screenkit.StatusRow(id, title, status)` | `build` returns the change, its notice and ok. `logforward.StatusRow(rd)` stays, one line |
| `LogForwardChange`, `TimeSyncChange`, `FirewallChange` | `screen.Change` (`pkg/ui/screen/change.go`) | None |
| the hardn.yml row and the two-item menu, twice | `wantRow(value, none, bad, differs)`, `setUndoItems(set, undo, off, own, differs)`, same file | The mock's view (`timesync.go`) calls them too: its goldens are unchanged |
| `lfReview`'s tail | `reviewOf(c, key, first, run)` in `internal/tools/uigallery/logforward.go` | Takes the screen as first drawn, which it cuts off. One caller until the real Time Sync screen |

Left where it was:

- **`checkWords`** (§5.1's fifth row lists it): it reads `rsyslogd -N1`'s output, and only Log Forwarding has such a check. It calls `steps.FirstLine`.
- **`opts.remotes`, `opts.own`, `opts.pair`, `SetOpts`, `ClearOpts`:** what Log Forwarding's mark means (§5.3).
- **Stop forwarding's delete of hardn's file:** not in §5.1. Time sync's Remove servers also restores, so the shape is not known yet.
- **The reviews' notices' words, every refusal's words and codes, the test line.**
- **§5.3, all of it:** `logfwd.Reading` with `Now`, `Own`, `Foreign`; "Set outside hardn", which each view decides before it calls `setUndoItems` (its `off`); the mark's meaning; `LogServer`; the status; the undo; the reload.

Tests: `pkg/host/confd`, `pkg/host/hostaddr`, `pkg/config` (`TestResolveHost`), `pkg/host` (`TestReadFileIfThere`), `pkg/engine/steps` (`service_test.go`), `pkg/ui/screen` (`change_test.go`), each new; Log Forwarding's own, unchanged, cover the rest through the control and the screen.

## 6. Words

Proposed, decided with §4.

- Title "Time Sync"; section "Time"; the status row "Time Sync": the server followed, "in sync", "not in sync", "not hardn.yml's servers", "no time service", "chrony not running", "in a container", "can't read".
- Rows: "Service" (chrony, systemd-timesyncd, busybox ntpd, None; "not running"); "Clock" (In sync, Not in sync); "Synced to" (None); "Server" ("by hardn", "from DHCP", "built in", "in <file>"; None); "File"; "hardn.yml" ("No servers", "Not valid", "differs").
- Items: "Set servers" (the servers, "No servers in hardn.yml", "hardn.yml is not valid", "Can't read chrony's settings", "Set outside hardn", "No time service", "chrony is not running", "The host keeps the time"); "Remove servers" ("Undo hardn's change").
- Questions: "Set time servers?", "Remove hardn's time servers?".
- Steps: "Write hardn's time servers", "Switch off Ubuntu's pools", "Set chrony's servers", "Set ntpd's servers", "Restart chrony", "Restart systemd-timesyncd", "Restart ntpd", "Delete hardn's time servers", "Switch Ubuntu's pools back on".
- Notices: "hardn's servers take the place of the host's own. Remove servers puts those back."; "The host's own servers are used again."; "In a container: the host keeps the time."

## 7. Made here

- A Server row a server, not one row for all: four names do not fit a row at 80 columns. A server of the distribution's own file has no note; the others say where they are set.
- No "Set by" row: each Server row says it, since one host can have servers from three places.
- "Synced to" is drawn only for a service that says which server it follows; busybox ntpd does not.
- A state the user can fix (no servers in hardn.yml, a hardn.yml that is not valid, settings unread) keeps Set servers offered, its description saying what is in the way. A state of the host (set outside hardn, no service, not running, a container) is unavailable. As Log Forwarding.
- hardn's files and marks are the mock's: `/etc/chrony/sources.d/hardn.sources`, `/etc/systemd/timesyncd.conf.d/60-hardn.conf`, each opening "# Written by hardn (NTP Settings)."; `#hardn: ` before a line switched off.
- The chrony mocks are Ubuntu 26.04, the one default-chrony release whose files were all read; `review-rhel` is Rocky Linux 10. No mock for Debian with timesyncd (as `timesyncd`, four built-in servers), Proxmox VE or a chrony chosen on Alpine.
- The view (`screen.TimeSync`) has a field, `Restores`, for the servers Remove servers puts back: its review lists them.

## 8. For the real unit

- **Reading chrony's settings:** every `server`, `pool` and `peer` line of the main file, each `confdir` and `sourcedir` it names, and `include`; which file each is in; which are hardn's. Every string made safe to show.
- **Which drop-ins are the distribution's.** Decision 7 needs the list (`ubuntu-ntp-pools.sources` so far).
- **ucf and rpm on an edited main file:** what a package upgrade asks or overwrites after hardn commented a line out (decision 1).
- **A chrony host with no `sources.d`** (cloud-init's file, EL): detected from the main file, not from the distribution.
- **`chronyc` where the command port is closed** (Alpine's `cmdport 0`), and its CSV form.
- **The kernel's flag** as the Clock row on every service, and how soon after a restart it is true: a Partial row for the first minute would be wrong.
- **Alpine:** which service runs (ntpd, chrony, openntpd, none); an `/etc/conf.d/ntpd` already edited; openntpd, which hardn would show and not set.
- **A name for a server** needs DNS before the clock is set.
- **The firewall:** outgoing 123/udp to the servers was not looked at.
- **Twelve main-menu items** at 80×24.
- Nothing ran on Linux.
