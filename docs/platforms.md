# Platform notes

What hardn does differently on each distribution. The support table is in the [README](../README.md#supported-distributions).

## End of support

hardn knows when each release's free security updates end. From 90 days before, the main menu shows a warning and the release to upgrade to, and every plan carries one (`PLATFORM_SUPPORT_ENDS`, then `PLATFORM_SUPPORT_ENDED`). The warning is by the host's clock. It changes nothing hardn does: only a new hardn moves a release to Out of scope.

| Distribution | Release: end of support |
|---|---|
| Debian | 13: 2030-06-30, 12: 2028-06-30 (the end of LTS) |
| Ubuntu | 26.04: May 2031, 24.04: May 2029, 22.04: May 2027 (without Ubuntu Pro) |
| Alpine | 3.24: 2028-06-01, 3.23: 2027-11-01, 3.22: 2027-05-01, 3.21: 2026-11-01, 3.20: 2026-04-01 |
| Proxmox | 9: not announced, so no warning; 8: August 2026 |
| Rocky, Alma, RHEL | 10: 2035-05-31, 9: 2032-05-31 (RHEL without the paid add-on) |

## Log forwarding

What Log Forwarding (main menu 8, then 2) does differs by the host's logger:

| Distribution | Logger hardn finds | Set server |
|---|---|---|
| Ubuntu; Rocky, Alma, RHEL | rsyslog | writes `/etc/rsyslog.d/60-hardn-forward.conf`; UDP, TCP or TLS |
| Debian 13 and 12 | journald alone, unless rsyslog is installed | installs rsyslog first, which also keeps its own log files under `/var/log` |
| Proxmox | as Debian | Set server installs rsyslog where journald runs alone: the one package hardn installs on Proxmox |
| Alpine | busybox syslogd; with rsyslog also installed, hardn can't tell which one logs and changes nothing | adds `-R <server>:<port> -L` to `SYSLOGD_OPTS` in `/etc/conf.d/syslog`, under a comment naming what it added; UDP only |

- hardn reads rsyslog's forwarding from `/etc/rsyslog.conf` and `/etc/rsyslog.d/*.conf`: `omfwd` actions and the older `@host` and `@@host` lines. A file another `include` names, RELP, and another syslog daemon (syslog-ng) are not read.
- TLS uses rsyslog's `gtls` driver (`rsyslog-gnutls`). Alpine gets no TLS: nothing is installed there.
- Not verified on a host yet: rsyslog's service name outside Debian (`rsyslog` is used); whether Debian 13's journald hands messages to a newly installed rsyslog unaided; where Ubuntu's AppArmor profile lets rsyslogd read a CA file; the TCP ports SELinux lets rsyslogd reach on Rocky, Alma and RHEL (514, 601, 6514, 10514 and 20514 are labelled for it).

## Time sync

What Time Sync (main menu 8, then 3) does differs by the host's time service. hardn sets the service the host already runs:

| Distribution | Service hardn finds | Set servers |
|---|---|---|
| Ubuntu 26.04 | chrony | writes `/etc/chrony/sources.d/hardn.sources`, comments out the pools of `ubuntu-ntp-pools.sources`, then `chronyc reload sources`: no restart |
| Debian, Proxmox with chrony installed | chrony | writes `hardn.sources`, comments out the `pool` line of `/etc/chrony/chrony.conf`, restarts chrony. On Proxmox the review says to set every node the same way |
| Rocky, Alma, RHEL | chrony | edits `/etc/chrony.conf` in place, as it reads no directory of yours: its `pool` line commented out, hardn's lines under a comment naming them; restarts chronyd |
| Debian 13 and 12; Ubuntu 24.04 and 22.04 | systemd-timesyncd | writes `/etc/systemd/timesyncd.conf.d/60-hardn.conf` with `NTP=`, restarts systemd-timesyncd |
| Alpine | busybox ntpd where its service is started or in a runlevel; chrony where that package is installed | replaces the `-p` words of `NTPD_OPTS` in `/etc/conf.d/ntpd`, under a comment naming the ones replaced; restarts ntpd |

- A line hardn switches off is kept as it was behind `#hardn: `. Remove servers takes the mark off again and deletes what hardn added, so each file is as it was.
- A file edited between the review and your answer is not written: hardn says so, and you run the item again.
- Not set, and said so: a host with no time service or a stopped one; a container; servers in a file that is neither the service's own nor hardn's (another `.sources` file, a `confdir` or `include` file, systemd-timesyncd's `NTP=` in any file but hardn's); `NTPD_OPTS` with no `-p`, or not written as one double-quoted line. Another time daemon (openntpd, ntpsec) is not looked for: the screen reads "None".
- The clock is read with `chronyc -N tracking` for chrony, `timedatectl show` under systemd, and `busybox adjtimex` on Alpine.
- Not verified on a host yet: everything on this list. The files are as each distribution's package source has them; the commands' output is as their source code prints it. What a package upgrade does with a main file hardn edited (ucf on Debian and Ubuntu, rpm elsewhere) is not known.

## Alpine

- On Alpine, `ufw` and `sudo` come from the community repository. On Alpine 3.22 and 3.23, the vendor supports only the main repository.
- Stock Alpine has no `ss`, so listening ports show "ss not installed" until `iproute2` is installed. Firewall offers to install it.
- On Alpine, Auto Updates restarts no service. An upgraded service keeps running its old version until it restarts, unless its package restarts it.

## Rocky, Alma and RHEL

On Rocky, Alma and RHEL:

- A new SSH port is labelled for sshd in SELinux and let through firewalld before sshd moves.
- The Firewall screen installs, enables and disables firewalld. It refuses a dnf install that brings a new kernel.
- Run All and `-w` leave the firewall to that screen. ufw is never installed or enabled.
- The Firewall screen has no Limit SSH: firewalld has no limit per address. SSH Login's Brute-Force Limit row shows sshd's own.
- Auto Updates turns on dnf-automatic's daily security updates; restarts stay as automatic.conf sets them (never, as shipped). Off undoes only what hardn did.
- DNS is only read.
- RHEL's repositories come with its subscription: on a host with none enabled, such as one that isn't registered, hardn refuses to install firewalld or dnf-automatic.

## Proxmox

On a Proxmox node:

- hardn never turns root's SSH login off, because the nodes log in to each other as root by key: `-d` and Run All make it key-only instead. Where it is already off, they leave it off; SSH Login's Root key-only item turns it back to key-only.
- SSH stays on port 22.
- Root's keys are edited through the cluster's shared file, and a node's own key is never removed.
- Root's password is never locked.
- The firewall is only read (pve-firewall). `ufw` is never touched, and `-w` is refused.
- Auto Updates only shows the state.

The default cluster join (`pvecm add`, or the web UI) logs in to the other node's API with root's password, and needs no SSH password login. Two steps log in over SSH as root instead. hardn does neither, so do them by hand:

- **`pvecm add <node> --use_ssh`** joins over SSH to the existing node. Where hardn made root key-only on that node (`-d`, Run All or SSH Login), root logs in there by key only. Add the joining node's root key (`/root/.ssh/id_rsa.pub`) to root's keys there first.
- **`pvecm qdevice setup <address>`** logs in to the QDevice host as root over SSH, using root's password where the node's key isn't there yet. If hardn turned root login off on that host (`-d`, or Run All), allow it again there by hand for the setup.

