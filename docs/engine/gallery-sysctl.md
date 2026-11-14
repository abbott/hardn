# Kernel Settings

The roadmap's "sysctl". hardn shows the kernel's network and kernel settings against a baseline, writes the ones below it to one file in `/etc/sysctl.d`, and sets them at once. This page holds the design: the screen and its reviews (§2), what the hosts have (§3), the decisions (§4), and what is built (§9).

Status: **built 2026-11-14, on `next/sysctl`.** The maintainer adopted every recommended answer of §4's decisions 1 to 19, in these words: "Adopt all". §4 is kept as the record. Not run on a Linux host yet: the hosts' behaviour is §3.2's, read on 2026-11-14, and the tests run on a model of it.

## 1. What to run

```
go run ./internal/tools/uigallery sysctl [-ascii] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery sysctl -list                    # the mocks' names
go run ./internal/tools/uigallery play sysctl[/host]              # the screen, run
go run ./internal/tools/uigallery play                            # then 8, 4
```

Goldens: `internal/tools/uigallery/testdata/golden/sysctl/`. Each drawing is hardn's own screen on a simulated host (`pkg/host/sysctl/sysctlfake`).

## 2. The mocks

One host, `web-01`.

| Mock | What it shows |
| --- | --- |
| `menu` | Main menu 8, the Configuration group, with Kernel Settings as item 4 |
| `default` | Rocky Linux 10, its values rocky10-test-110's: four rows below the baseline, "Apply settings" |
| `configured` | The same host after Apply settings: every row at the baseline, hardn's file under "Set by hardn", "Remove settings" |
| `overridden` | After a boot, `99-sysctl.conf` sets a key back: the row names the file, Partial |
| `routes` | Debian 13 with forwarding on, its values deb13-vm's: Sending redirects and Reverse-path filter left alone, and why; eth0 alone puts IPv6 redirects below the baseline |
| `pve` | A Proxmox VE 9 node, forwarding off: the same two rows left alone. Debian 13's values: no node was read |
| `container` | A container: the host's kernel and file values and its own network values, with no verdict; Not Applicable, Apply settings unavailable |
| `alpine` | Alpine 3.23, its values alpine323-vm's: six rows below the baseline |
| `alpine-not-at-boot` | Alpine with the `sysctl` service in no runlevel, after Apply settings: a warning, the command that fixes it, Partial (decision 20) |
| `review` | `default`'s Apply settings up to its question: the file, one line a row with `*` for every interface, and a `sysctl -w` for each key below the baseline |
| `review-routes` | `routes`'s: four rows, and a notice of the three rows left alone |
| `review-alpine` | `alpine`'s: the file names each interface, as busybox reads no `*` |
| `review-alpine-service-off` | The same with the `sysctl` service in no runlevel: the notice says so, with the fix |
| `review-remove` | `configured`'s Remove settings: the file deleted, each earlier value set back, every interface's own |
| `refuse-file` | Apply settings with a file at hardn's path that hardn did not write: refused with words |

`default`, drawn:

```
# Kernel Settings ##############################################

  [ ] Kernel Settings      Not Configured 4 to set

  Network ------------------------------------------------------

  [ ] ICMP redirects       1 accepted, hardn sets 0
  [ ] IPv6 redirects       1 accepted, hardn sets 0
  [+] Source routing       0 refused
  [+] SYN cookies          1 on
  [+] Broadcast pings      1 ignored
  [+] Bogus ICMP errors    1 ignored
  [ ] Sending redirects    1 on, hardn sets 0
  [+] Reverse-path filter  1 strict
   -  Forwarding           0 off

  Kernel -------------------------------------------------------

  [+] Kernel addresses     1 hidden from users
  [+] Kernel log           1 root only
  [+] Address randomizing  2 full
  [ ] Process tracing      0 unrestricted, hardn sets 1
  [+] Unprivileged BPF     2 off
  [+] Performance events   2 limited

  Files --------------------------------------------------------

  [+] Hard links           1 protected
  [+] Symbolic links       1 protected
  [+] FIFOs in /tmp        1 protected
  [+] Files in /tmp        1 protected

  Select an option

   1) Apply settings       Set 4 settings

   0) Return

    > Option [0-1 or q]:
```

`review`, drawn:

```
# Apply settings ###############################################

# Note
# hardn sets these now. Remove settings puts the earlier values
# back. With Process tracing set, gdb -p and strace -p need
# root.

  Settings -----------------------------------------------------

   -  ICMP redirects       0 ignored
   -  IPv6 redirects       0 ignored
   -  Sending redirects    0 off
   -  Process tracing      1 children only

   -  Write hardn's kernel settings
      /etc/sysctl.d/90-hardn.conf
      @@ -0,0 +1,18 @@
      +# Written by hardn (Kernel Settings).
      +# before: net.ipv4.conf.all.accept_redirects = 1
      +# before: net.ipv4.conf.default.accept_redirects = 1
      +# before: net.ipv4.conf.eth0.accept_redirects = 1
      +# before: net.ipv4.conf.lo.accept_redirects = 1
      +net.ipv4.conf.*.accept_redirects = 0
      +# before: net.ipv6.conf.all.accept_redirects = 1
      +# before: net.ipv6.conf.default.accept_redirects = 1
      +# before: net.ipv6.conf.eth0.accept_redirects = 1
      +# before: net.ipv6.conf.lo.accept_redirects = 1
      +net.ipv6.conf.*.accept_redirects = 0
      +# before: net.ipv4.conf.all.send_redirects = 1
      +# before: net.ipv4.conf.default.send_redirects = 1
      +# before: net.ipv4.conf.eth0.send_redirects = 1
      +# before: net.ipv4.conf.lo.send_redirects = 1
      +net.ipv4.conf.*.send_redirects = 0
      +# before: kernel.yama.ptrace_scope = 0
      +kernel.yama.ptrace_scope = 1

   -  Set the values now
      $ sysctl -w net.ipv4.conf.all.accept_redirects=0
      $ sysctl -w net.ipv4.conf.default.accept_redirects=0
      $ sysctl -w net.ipv4.conf.eth0.accept_redirects=0
      $ sysctl -w net.ipv4.conf.lo.accept_redirects=0
      $ sysctl -w net.ipv6.conf.all.accept_redirects=0
      $ sysctl -w net.ipv6.conf.default.accept_redirects=0
      $ sysctl -w net.ipv6.conf.eth0.accept_redirects=0
      $ sysctl -w net.ipv6.conf.lo.accept_redirects=0
      $ sysctl -w net.ipv4.conf.all.send_redirects=0
      $ sysctl -w net.ipv4.conf.default.send_redirects=0
      $ sysctl -w net.ipv4.conf.eth0.send_redirects=0
      $ sysctl -w net.ipv4.conf.lo.send_redirects=0
      $ sysctl -w kernel.yama.ptrace_scope=1

  Apply kernel settings? [y/N, 0 or q]:
```

`alpine-not-at-boot`'s last section:

```
  Set by hardn -------------------------------------------------

   -  File                 /etc/sysctl.d/90-hardn.conf
  [ ] At boot              Not set: sysctl service not enabled
   -  To fix               rc-update add sysctl boot
```

## 3. Survey

### 3.1 The keys, and what six hosts have

Read on 2026-11-14 with `sysctl -n` or `/proc/sys`, as a normal user: `/Users/bruce/moon/_code/hardn-framework/_support/hardn-audit-2026-10-16/vm-runs/sysctl/HOST.txt`. D = deb13-vm (Debian 13), U = ubuntu26-vm (Ubuntu 26.04), A = alpine323-vm (Alpine 3.23), R = rocky10-test-110, C = centos9-test-119, O = oracle10-test-118. "–": not readable as a normal user. These tables are `all` and `default`; each interface's own value is §3.2's F1, which puts D, U and A below the baseline on redirects too.

**Set** (decision 3 (a)). "Passes" is what the row accepts; hardn writes the first value.

| Row | Key | Passes | D | U | A | R | C | O |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| ICMP redirects | `net.ipv4.conf.{all,default}.accept_redirects` | 0 | 0 | 0 | 0 | **1** | **1** | **1** |
| IPv6 redirects | `net.ipv6.conf.{all,default}.accept_redirects` | 0 | 0 | 0 | 0 | **1** | **1** | **1** |
| Source routing | `net.ipv4.conf.all.accept_source_route`, `net.ipv6.conf.all.accept_source_route` | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| SYN cookies | `net.ipv4.tcp_syncookies` | 1 or more | 1 | 1 | 1 | 1 | 1 | 1 |
| Broadcast pings | `net.ipv4.icmp_echo_ignore_broadcasts` | 1 | 1 | 1 | 1 | 1 | 1 | 1 |
| Bogus ICMP errors | `net.ipv4.icmp_ignore_bogus_error_responses` | 1 | 1 | 1 | 1 | 1 | 1 | 1 |
| Kernel addresses | `kernel.kptr_restrict` | 1 or more | **0** | 1 | **0** | 1 | 1 | 1 |
| Kernel log | `kernel.dmesg_restrict` | 1 | 1 | 1 | 1 | 1 | **0** | 1 |
| Address randomizing | `kernel.randomize_va_space` | 2 | 2 | 2 | 2 | 2 | 2 | 2 |
| Process tracing | `kernel.yama.ptrace_scope` | 1 or more | **0** | 1 | 1 | **0** | **0** | **0** |
| Unprivileged BPF | `kernel.unprivileged_bpf_disabled` | 2, or 1 | 2 | 2 | 1 | 2 | 2 | 1 |
| Performance events | `kernel.perf_event_paranoid` | 2 or more | 3 | 4 | 2 | 2 | 2 | 2 |
| Hard links | `fs.protected_hardlinks` | 1 | 1 | 1 | 1 | 1 | – | 1 |
| Symbolic links | `fs.protected_symlinks` | 1 | 1 | 1 | 1 | 1 | – | 1 |
| FIFOs in /tmp | `fs.protected_fifos` | 1 or more | 1 | 1 | **0** | 1 | – | 1 |
| Files in /tmp | `fs.protected_regular` | 1 or more | 2 | 2 | **0** | 1 | – | 1 |

**Set unless the host routes** (decisions 5 and 6).

| Row | Key | Passes | D | U | A | R | C | O |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Sending redirects | `net.ipv4.conf.{all,default}.send_redirects` | 0 | **1** | **1** | **1** | **1** | **1** | **1** |
| Reverse-path filter | `net.ipv4.conf.default.rp_filter` (written 2) | 1 or 2 | 2 | 2 | 1 | 1 | 1 | 2 |
| (not set) | `net.ipv4.conf.all.rp_filter` | | 0 | 2 | 1 | 0 | 0 | 0 |

**Shown, never set.**

| Row | Key | D | U | A | R | C | O |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Forwarding | `net.ipv4.ip_forward`, `net.ipv6.conf.all.forwarding` | 0 | 0 | 0 | 0 | 0 | 0 |

**Left out.**

| Key | Today | Why |
| --- | --- | --- |
| `fs.suid_dumpable` | D A 0; U R C O 2 | 0 stops core dumps of setuid programs reaching apport or systemd-coredump; those four ship `50-coredump.conf` (contents not read) |
| `net.ipv4.conf.all.log_martians` | 0 on all | Kernel-log noise on bridge and container hosts |
| `net.ipv6.conf.all.accept_ra` | 1 on all | 0 drops the SLAAC address and the route learned from it |
| `kernel.kexec_load_disabled` | A 1, others 0 | Cannot be undone before a reboot; breaks kdump |
| `kernel.sysrq` | D 438, U 176, A 1, R C O 16 | Each vendor's own choice |
| `kernel.core_pattern` | apport (U), systemd-coredump (R C O), `core` (D A) | Owned by those services |
| `net.core.bpf_jit_harden` | – on all | Root-only: not read |
| `net.ipv4.conf.all.secure_redirects` | 1 on all | No effect once redirects are ignored |
| `net.ipv4.tcp_rfc1337` | A 1, others 0 | Small gain |

What each key does, and the rule that the kernel uses the higher of `all` and an interface's `rp_filter`: the kernel's [ip-sysctl](https://docs.kernel.org/networking/ip-sysctl.html), [sysctl/kernel](https://docs.kernel.org/admin-guide/sysctl/kernel.html), [sysctl/fs](https://docs.kernel.org/admin-guide/sysctl/fs.html) and [Yama](https://docs.kernel.org/admin-guide/LSM/Yama.html).

### 3.2 Read on hosts, 2026-11-14

As root, on deb13-vm, ubuntu26-vm, alpine323-vm, centos10-test-120, oracle9-test-117 and alpine324-test-116, with a boot of the last three: `/Users/bruce/moon/_code/hardn-framework/_support/hardn-audit-2026-10-16/vm-runs/sysctl/probe.log`, `boot-*.log`, `probe2.log`. Where a note elsewhere on this page says "unverified", these override it.

- **F1. An interface's own value differs from `all` and `default`.** eth0 reads `net.ipv6.conf.eth0.accept_redirects=1` on Debian 13, Ubuntu 26.04 and Alpine while `all` and `default` read 0; eth0's IPv4 `accept_redirects` is 1 on Ubuntu and Alpine. So a row reads every interface, and Debian, Ubuntu and Alpine are below the baseline on redirects.
- **F2. procps `sysctl -p FILE`** (Debian, Ubuntu, Rocky, CentOS Stream, Oracle) reads `net.ipv6.conf.*.accept_redirects = 0` as `all`, `default` and each interface that exists, and prints each. An unknown key: exit 1, and "sysctl: cannot stat /proc/sys/no/such/key". Writing `all` and `default` alone did not change eth0.
- **F3. After a boot on a systemd host**, with the `*` line in `/etc/sysctl.d`: eth0 kept `send_redirects=0` and IPv6 `accept_redirects=0`. With `all` and `default` lines alone, eth0's IPv4 values followed `default`, and its IPv6 `accept_redirects` stayed 1. The `*` line is the one that holds.
- **F4. Alpine's busybox `sysctl`** (`/sbin/sysctl`, a link to busybox) reads no `*`: "sysctl: error: 'net.ipv6.conf.*/accept_redirects' is an unknown key". It exits 0 even when a key fails. So on Alpine the file names each interface, and success is told by stderr and by reading each value back. Lines naming eth0 held after a boot with the `sysctl` service in the boot runlevel.
- **F5. Alpine's `sysctl` service** is in the boot runlevel on alpine323-vm and in no runlevel on alpine324-test-116, where no file of `/etc/sysctl.d` is applied at boot (decision 20).
- **F6.** `/sbin/sysctl` and `/usr/sbin/sysctl` are both there on Debian 13 and Ubuntu; `systemd-sysctl` is "static" on the systemd hosts.
- **F7.** On a procps host a line naming an interface that later goes away is harmless at boot. `sysctl -p` on the file fails on it (F2), so Apply settings does not load the file, and leaves the line out when it writes the file again (§9). On Alpine an interface added later is not covered until Apply settings runs again.

How the files are read:

| | At boot | Order |
| --- | --- | --- |
| Debian, Ubuntu, Rocky, CentOS Stream, Oracle (systemd) | `systemd-sysctl.service` | Files of `/etc`, `/run`, `/usr/local/lib` and `/usr/lib` together, by file name; the later name wins ([sysctl.d(5)](https://www.freedesktop.org/software/systemd/man/latest/sysctl.d.html)) |
| Alpine (OpenRC) | the `sysctl` service, where it is in a runlevel | `/lib`, `/usr/lib`, `/etc/sysctl.d/*.conf`, then `/etc/sysctl.conf`, then `/run/sysctl.d` (read in its init script, `probe2.log`) |

Files found: `/etc/sysctl.d/99-sysctl.conf` on R, C and O; `99-cloudimg-ipv6.conf` on U; only a README on D and A. Alpine's `00-alpine.conf` was read (`probe2.log`); the other vendors' files were not.

### 3.3 Unverified

- hardn itself on a Linux host: Apply settings, Remove settings and a boot after each.
- `sysctl -w KEY=VALUE`, which Apply settings, Remove settings and a rollback run: not run on a host in the probes, procps's or busybox's. Only `-p` was run. Each value is read back, so one that does not take fails the change.
- A `*` line whose family the kernel no longer has (IPv6 turned off since), in the file at boot.
- Unprivileged BPF at 1 refusing a write of 2: from the kernel's source, from memory. Apply settings does not write a key that passes.
- A line naming an interface, in the file at boot on a systemd host before the interface exists (the reverse-path filter's only: F3's boot covers `default`, which a new interface follows).
- A line in a file read before hardn's that names one interface or `all` outright, against hardn's `*` line at boot on a systemd host. hardn looks only at the files read after its own (decision 11).
- An interface whose name has a "." (a VLAN, `eth0.100`) on Alpine: hardn writes it `eth0/100`, as procps reads it. A wrong name fails the read-back, and the change is rolled back.
- procps older than the hosts read: one that reads no `*` fails the change, which is rolled back.
- Proxmox VE's values, its vendor files, and whether it turns forwarding on itself. Docker doing so is from memory.
- What a container can write: hardn sets nothing there.
- That `99-sysctl.conf` on R, C and O is a link to `/etc/sysctl.conf`.
- Every root-only key on the hosts of §3.1, and CentOS Stream 9's four `fs.protected_*`.
- That these keys are a common hardening set (CIS): from memory.

## 4. Decisions for the maintainer

Twenty. All are adopted, each at (a), and built. An earlier one gates the later ones it names.

1. **The screen's name and place.**
   - (a) *Recommended, drawn:* "Kernel Settings", item 4 of the Configuration group (main menu 8): "Harden network and kernel". The main menu keeps its ten keys; item 8's description gains "kernel settings".
   - (b) The same place, named "sysctl": the tool's name, not what it is for.
   - (c) A main-menu item: Log Forwarding and Time Sync were both moved off it.

   *Gates every word below.*

2. **What a row is.**
   - (a) *Recommended, drawn:* one row a setting, in plain words: its value, what the value means and, below the baseline, what hardn sets. A row stands for its keys (IPv4 and IPv6 for Source routing; `all`, `default` and each interface by decision 9) and shows the weakest. The key names are in the review's file. 19 rows.
   - (b) One row a key, by its full name: 24 rows of two lines each at 80 columns, as a key is wider than the label column.

   *Gates the screen.*

3. **The key set.** §3.1.
   - (a) *Recommended, drawn:* the "Set" table, 19 keys; Sending redirects and Reverse-path filter by decisions 5 and 6; Forwarding shown; the "Left out" table left out.
   - (b) Add `fs.suid_dumpable=0` and `log_martians=1`: changes Ubuntu, Rocky, CentOS and Oracle, which then keep no core dump of a setuid program, and fills the kernel log on a bridge host.
   - (c) Network keys only: drops Process tracing, the one row a user will notice (`gdb -p` and `strace -p` then need root), and with it Kernel addresses and the `/tmp` rows.

   *Gates 4 to 8.*

4. **A value stricter than the baseline.**
   - (a) *Recommended, drawn:* the baseline is a minimum. A stricter value passes and is never lowered; hardn writes only the keys below it. `unprivileged_bpf_disabled` passes at 1 or 2 and is written 2: 1 cannot be undone before a reboot.
   - (b) An exact match: Debian and Ubuntu ship stricter values (`perf_event_paranoid` 3 and 4, `protected_regular` 2), which would read as wrong or be lowered.

5. **Reverse-path filter: strict or loose.**
   - (a) *Recommended, drawn:* loose. `default` passes at 1 or 2; hardn writes 2 only where it is 0, and never writes `all`. Changes none of the six hosts.
   - (b) Strict, `all` and `default` 1: drops packets on a host with two uplinks, a policy-routed VPN or any asymmetric path, SSH's among them. Decision 15 then needs confirm-or-revert.
   - (c) Shown, never set.

   *Gates 15.*

6. **A host that routes.**
   - (a) *Recommended, drawn:* hardn never writes the forwarding keys, on any host. A host routes when any reads 1: `net.ipv4.ip_forward`, or `forwarding` under `net.ipv4.conf` or `net.ipv6.conf` for `all`, `default` or one interface (a router set up per link). Then Sending redirects and Reverse-path filter are left alone and say why; every other row is set. Nothing is asked.
   - (b) Also count a bridge, WireGuard or tun interface as routing: more hosts left alone, and a list of interface kinds to keep.
   - (c) Ask "Does this host route?" at Apply: one more question, which the kernel already answers.
   - (d) Write forwarding 0 where the host does not route: changes none of the six, and at the next boot undoes a file read before hardn's that turns it on.

   *Gates 7.*

7. **Proxmox VE.**
   - (a) *Recommended, drawn:* always a host that routes, whatever forwarding reads: a routed or NAT bridge for guests may be added at any time. The other rows are set as on Debian. Unverified: no node was read.
   - (b) As any host, by decision 6: Sending redirects is set while forwarding is off.

8. **IPv6 keys.**
   - (a) *Recommended:* set as IPv4's, where the kernel has them. A key the kernel does not have (IPv6 off at boot) is not written, and its row reads Not Applicable. `accept_ra` is never written, so SLAAC and its route keep working. Not drawn: no mock has IPv6 off.
   - (b) No IPv6 keys: Rocky, CentOS and Oracle go on accepting IPv6 redirects.

9. **Keys each interface has.** ICMP redirects, IPv6 redirects, Sending redirects and Reverse-path filter. The kernel decides for each interface. IPv4 redirects are accepted, on a host that does not route, and sent if `all` or the interface's own value is 1; Reverse-path filter uses the higher of the two. IPv6 redirects follow the interface's own value alone, and writing `all` or `default` changes no interface that exists. Writing IPv4's `default` also sets every interface whose own value was never written.
   - (a) *Recommended, drawn since the build:* the row reads every interface and shows the weakest: `all` or any interface for the IPv4 redirect rows, each interface for IPv6 redirects, the higher of `all` and each interface for Reverse-path filter. The file uses systemd's glob (`net.ipv6.conf.*.accept_redirects = 0`, as its `50-default.conf` does for `rp_filter`), which systemd-sysctl also applies to an interface as it appears, so the setting holds after a boot. Each interface's earlier value is recorded for Remove. Alpine: busybox `sysctl` reads no globs, so the file names `all`, `default` and each interface that exists.
   - (b) *The mock's first drawing:* `all` and `default` in the file, and each interface set now with `sysctl -w`. After a boot an interface can be back at its own value (IPv6 redirects on Rocky, CentOS and Oracle) while the screen reads Configured, and Remove cannot put an interface's own earlier value back.

   *Gates 10, 12 and 14.* The boot was read on 2026-11-14 (F2 to F4).

10. **hardn's file: its name and what is in it.**
    - (a) *Recommended, drawn:* `/etc/sysctl.d/90-hardn.conf`, opening "# Written by hardn (Kernel Settings).", holding only the keys hardn changed, each under `# before: N`, plain `key = value`. It is read after the vendors' `50-*` and `55-*` and before `99-sysctl.conf`, so the administrator's own file wins. A file there without hardn's mark is refused.
    - (b) A name read last (`99-zz-hardn.conf`): hardn wins over the administrator's files, but not over `/etc/sysctl.conf` on Alpine, which is read after the directory.
    - (c) Every baseline key in the file: the vendor's defaults pinned too, a 22-key review on a host that needs one line, and stricter values to skip.

    *Gates 11 to 14.*

11. **A key set back by a file read after hardn's.**
    - (a) *Recommended, drawn (`overridden`):* report. The row warns and names the file, the screen is Partial, and hardn edits no file but its own. To leave a key alone on purpose, this is the way: decision 17 needs no per-key setting.
    - (b) Refuse Apply settings while any key is held: one line of the administrator's blocks every other row.
    - (c) Edit the other file: hardn changing a file it did not write.

12. **Setting the values now.**
    - (a) *Recommended, drawn:* `sysctl -p /etc/sysctl.d/90-hardn.conf` on every family, then `sysctl -w` for each interface that exists, for the redirect rows: on a host that does not route, an interface's own value keeps redirects on (decision 9). No `sysctl`: refused with words. As built, `sysctl -w` alone, one for each key below the baseline, every interface's among them (review of 2026-11-14): `sysctl -p` sets every line of the file again, so a later Apply would set a key a later file holds since, a row left alone since the host began to route, or a strict reverse-path filter back to loose, none of which the review shows; and it fails on a line naming an interface that is gone (F2). The file's lines are for the boot (F3, F4).
    - (b) Write `/proc/sys` directly: no tool needed and the same everywhere, but the review shows no command.
    - (c) `sysctl --system`, or `rc-service sysctl restart`: reads every file of the host again, which changes more than the review shows.

13. **A key that fails to set.**
    - (a) *Recommended:* all or nothing. The change fails, the engine puts the file back as it was and sets back the values already written.
    - (b) Keep what was set and report the one that failed: the file and the kernel then disagree.

14. **Remove settings, the undo.** Deleting the file changes nothing until a boot.
    - (a) *Recommended, drawn (`review-remove`):* delete the file and set each key back to the value recorded under `# before:`, now. An interface gets the value its row had: its own earlier value is recorded only with 9 (a).
    - (b) Delete the file only: the values stay until the next boot, and the screen goes on reading Configured.
    - (c) Delete, then `sysctl --system`: sets back only the keys another file names.

15. **Confirm-or-revert.**
    - (a) *Recommended, drawn:* none, plain y/N. No key of 3 (a) with 5 (a) can cut off SSH: they ignore redirects, restrict tracing, and protect `/tmp`. With 5 (b), strict filtering, it can, and (b) is needed.
    - (b) Confirm from a new connection or revert, as an SSH port change.

16. **Containers.**
    - (a) *Recommended, drawn:* reported only. The values are shown with no verdict (the host's for the Kernel and Files rows, the container's own for Network), the row is Not Applicable, Apply settings is unavailable, and there is no main-menu or Run All row.
    - (b) Set the network keys a container can write: half a baseline, on a kind of container not checked (§3.3).

    *Gates 18.*

17. **hardn.yml.**
    - (a) *Recommended:* one switch, `kernelSettings: true`, off by default. It only puts Kernel Settings in Run All and on the main menu (decision 18); the screen's Apply settings works without it.
    - (b) The switch and a list of keys to leave alone: one more key and two more checks, for what a later file already does (decision 11).
    - (c) None: the screen only, never in Run All.

    *Gates 18.*

18. **Run All, `-r`, and the main menu.**
    - (a) *Recommended:* as Log Forwarding and Time Sync. A Run All row, and a main-menu status row, only where `kernelSettings` is true; Apply only, never Remove; plain y/N; not counted in the risk level; no row in a container (decision 16).
    - (b) In Run All on every host: changes a host whose hardn.yml never asked.

19. **`hardn check`.**
    - (a) *Recommended:* one fact, "Kernel settings": the keys below the baseline, by name, or "none". Added with Log Forwarding's and Time Sync's facts in one change of the baseline's format, as a new fact makes every saved baseline invalid.
    - (b) A fact a key: 22 facts for one screen.
    - (c) None.

20. **Alpine with the `sysctl` service in no runlevel (F5).** *Adopted 2026-11-14, in the maintainer's words: "Adopt (a) for decision 20".* There no file of `/etc/sysctl.d` is applied at boot, hardn's among them.
    - (a) *Adopted, built, drawn (`alpine-not-at-boot`):* Apply settings still sets the values now. The review's notice says they are not set at boot. The screen then warns "Not set: sysctl service not enabled", with the fix, `rc-update add sysctl boot`, and the row is Partial "not set at boot". hardn does not enable the service.
    - (b) hardn enables the service: one step less for the user, and every other file of `/etc/sysctl.d` and `/etc/sysctl.conf` is then applied at boot too, which hardn did not show.
    - (c) Refuse Apply settings until the service is enabled: nothing is set on a host where the values could be set now.

## 5. Shared code

- **Reused as it is:** `steps.WriteOwnFile` and `DeleteOwnFile` (the mock's plan calls both), `screenkit.Session.Reviewed`, `screenkit.StatusRow`, the "# Written by hardn" mark and the refusal of a file without it, `detect`'s container and Proxmox VE facts.
- **New, kept in the control:** a change that runs commands after a file, reads the values back, and sets them back on a rollback (`values` in `pkg/engine/controls/kernelsettings/plan.go`). `steps.RestartService` was the model. No second control needs it yet.
- **Stays specific:** the key table, the reading of `/proc/sys` and of the files read after hardn's, and the words.
- **Not shared:** Log Forwarding's and Time Sync's hardn.yml row and two-item rule. This screen sets hardn's own baseline, not a value hardn.yml names.

## 6. Words

- Screen: "Kernel Settings". Sections: "Network", "Kernel", "Files", and "Set by hardn" for its file. Rows: §3.1.
- Below the baseline: "1 accepted, hardn sets 0". Held by a later file: "1 accepted, set in 99-sysctl.conf". Left alone: "1 on, left alone: this host routes", "left alone: Proxmox VE".
- Status: "Not Configured 4 to set", "Partial 1 set outside hardn", "Partial not set at boot", "Configured by hardn", "Not Applicable in a container", "Unknown can't read".
- Items: "Apply settings" ("Set 4 settings", "Set 1 setting", or "Can't read the settings"), "Remove settings" ("Undo hardn's change"). Unavailable: "Not in a container", "Nothing to set". In a container: "In a container: hardn sets none of these."
- Not set at boot (decision 20): "At boot", "Not set: sysctl service not enabled"; "To fix", "rc-update add sysctl boot". A setting the kernel does not have: "Not Applicable".
- Questions: "Apply kernel settings?", "Remove hardn's kernel settings?".
- Notices: "hardn sets these now. Remove settings puts the earlier values back." "With Process tracing set, gdb -p and strace -p need root." "Forwarding, Sending redirects and Reverse-path filter are left as they are." "The earlier values are set again." "The sysctl service isn't enabled, so these are not set at boot. To fix: rc-update add sysctl boot."

## 7. Made here

- The hosts are simulated: `internal/tools/uigallery/sysctl.go` gives each the values of §3 and a model of its `sysctl` (`pkg/host/sysctl/sysctlfake`). The screens, the reviews and the plans are hardn's own.
- A row left alone has no verdict mark, like Forwarding.
- A host with every row at the baseline and no file of hardn's is Configured, with Apply settings unavailable: "Nothing to set". Not drawn.
- No mock for a key that cannot be read (the row is Unknown, and Apply settings refused), for IPv6 off (the row reads Not Applicable), or for a host with no `sysctl` (refused).

## 8. Still to do

- Run it on the VMs, with a boot: §3.3's first five lines.
- A Proxmox VE node and a container (LXC and Docker) to read.
- `hardn check` (decision 19): no fact is added in this unit. It waits for the one change of the baseline's format that adds Log Forwarding's, Time Sync's and this one.
- Decision 20 for the maintainer.
- Decision 1 (a)'s last words, "item 8's description gains kernel settings", are not built: the main menu's description column holds 37 cells, and "hardn.yml, log forwarding, time sync" fills 36. The item reads as before. Words that fit are the maintainer's to choose.

## 9. Built

| What | Where |
| --- | --- |
| The reader: each key from `/proc/sys`, every interface under `net/ipv4/conf` and `net/ipv6/conf`; the files read after hardn's; forwarding; the tool; under OpenRC, whether the `sysctl` service is in a runlevel; hardn's file, written and read back | `pkg/host/sysctl` |
| The control `kernel.settings`: the row, Apply settings, Remove settings, the refusals | `pkg/engine/controls/kernelsettings` |
| The screen, and its view | `pkg/screens/kernelsettings`, `pkg/ui/screen/kernelsettings.go` |
| Item 4 of the Configuration group | `pkg/ui/screen/configgroup.go`, `pkg/screens/config/group.go`, `cmd/hardn/kernelsettings.go` |
| `kernelSettings` in hardn.yml; the Run All row and the main menu's row where it is true, none in a container | `pkg/config`, `pkg/engine/runall`, `pkg/engine/controls/status.go` |
| The codes | `pkg/output/codes.go`, [output.md](../reference/output.md) |

**A row.** Its keys are read, every interface's among them, `lo` too, and the row shows the weakest. ICMP redirects, IPv6 redirects and Sending redirects are below the baseline when `all`, `default` or any interface is. The reverse-path filter is below it when `default` is 0, or an interface and `all` are both 0. Source routing's IPv6 key passes below 0 too: the kernel then accepts no routing header (its ip-sysctl document), which is stricter, and hardn leaves it. A key the kernel does not have is left out; a row with none of its keys reads Not Applicable. A key that cannot be read makes the row, and the screen's row, Unknown.

**A host that routes.** Any of `net.ipv4.ip_forward` and `forwarding` under `net.ipv4.conf` or `net.ipv6.conf`, for `all`, `default` or an interface, at 1; and every Proxmox VE node. Sending redirects and Reverse-path filter are then left alone.

**A key held by a later file.** A key below the baseline is held when the last line that names it, in the files the host reads after `90-hardn.conf`, sets a value below the baseline. Under systemd a line with `*` counts; under OpenRC `/etc/sysctl.conf` and `/run/sysctl.d` are read after `/etc/sysctl.d`. hardn's file does not name a held key: no line is added for a held row, and a line hardn wrote before the row was held is left out when the file is next written.

**hardn's file**, `/etc/sysctl.d/90-hardn.conf`:

```
# Written by hardn (Kernel Settings).
# before: net.ipv6.conf.eth0.accept_redirects = 1
# before: net.ipv6.conf.lo.accept_redirects = 1
net.ipv6.conf.*.accept_redirects = 0
# before: kernel.yama.ptrace_scope = 0
kernel.yama.ptrace_scope = 1
```

- Only the keys below the baseline. Above each line, each key it changes and the value it had: `# before: KEY = N`. Decision 10 (a) drew `# before: N`; a `*` line needs the key named, so every record names it.
- procps hosts: one `*` line for a row every interface has (F2, F3). Alpine: a line for each key below the baseline, `all`, `default` and each interface by name (F4).
- The reverse-path filter: `default` and each interface by name, never `*`, which would write `all`.
- Written again, the file keeps its lines and their first records, and adds what is new. Left out: the lines of a row a later file now holds, and a line naming an interface that is gone. A line whose key now reads above its value, where a higher value passes, takes that value. No other value is taken: hardn writes 1 for no reverse-path filter and for no unprivileged BPF (decision 5).
- The lines of Sending redirects and Reverse-path filter stay when the host begins to route. Apply settings does not set them again, and Remove settings still puts their earlier values back.
- A file there without hardn's first line is refused. One with it and a line hardn does not write is not read, and refused.

**Apply settings.** The file is written; `sysctl -w` sets each key below the baseline of the rows the review lists, and no other key; each is read back from `/proc/sys`. The file is not loaded (decision 12). A failure is the tool's exit status, on Alpine the word "error" on its stderr, or a value that does not read as set. Then the change fails with `KERNEL_SETTINGS_NOT_SET`, the engine puts the file back as it was, and `sysctl -w` sets back each value (decision 13).

**Remove settings.** The file is deleted, `sysctl -w` sets each recorded key back where the kernel still has it, and each is read back. A failure puts the file back and sets each value back to what it read.

**Never written:** a forwarding key, `accept_ra`, `net.ipv4.conf.all.rp_filter`, and any key outside §3.1's "Set" tables. hardn's file is read with the same rule, so a line added by hand is not acted on.

**Refusals**, before anything is written: `KERNEL_SETTINGS_FILE_NOT_HARDNS`, `KERNEL_SETTINGS_NO_SYSCTL`, `KERNEL_SETTINGS_CONTAINER`, `KERNEL_SETTINGS_NOT_READ`, `KERNEL_SETTINGS_NOTHING_TO_REMOVE`; at the apply, `KERNEL_SETTINGS_CHANGED_SINCE`.

**Run All and the main menu** list Kernel Settings only where hardn.yml has `kernelSettings: true`, and never in a container. Run All plans Apply settings, never Remove settings. The risk level does not count the row.

**System Details and `hardn check`** show nothing of it.

### Limits

- An interface added after Apply settings: on a procps host the `*` line sets it as it appears (F3); on Alpine it is not covered until Apply settings runs again (F7).
- A line left out of the file takes its records with it: Remove settings does not put back the values of a row a later file holds. They stay as they are until the next boot.
- hardn's lines for Sending redirects and Reverse-path filter, written before the host began to route, are still set at boot.
- Remove settings puts back the values the file recorded. An interface that appeared since has no record, and keeps hardn's value until the next boot.
- A row passes on the values the kernel has now. A later file that will set a key back at the next boot shows only once the key reads so.
- On Alpine with the `sysctl` service in no runlevel nothing is set at boot (decision 20).
- The row "Reverse-path filter", left alone at 1, is wider than its line: "1 strict, left alone: this host routes" goes to a second line.

## 10. On hosts (2026-11-14)

hardn built from this branch, driven through the screen (main menu 8, then 4).

| Step | centos10-test-120 (CentOS Stream 10), oracle9-test-117 (Oracle Linux 9.8) |
|---|---|
| The screen | Not Configured, "4 to set": ICMP redirects, IPv6 redirects, Sending redirects, Process tracing |
| Apply settings | the file written with a `*` line a redirect row; 13 `sysctl -w`; Operation Successful; Configured "by hardn". Only those keys changed, on `all`, `default`, `eth0` and `lo` |
| A boot with the file | every value as right after Apply |
| Remove settings | the file deleted, every value as before Apply |
| A boot without it | every value as before Apply |
| `-n -r` with `kernelSettings: true` | the same plan, nothing changed |

alpine324-test-116 turned out to be a container: the screen read Not Applicable "in a container", each row with no verdict, Apply settings Unavailable, and nothing changed (decision 16). So F4's and F5's reads are a container's.

**deb13-vm (Debian 13):** as the two above, with "5 to set" (ICMP redirects from `lo`, IPv6 redirects from `eth0` and `lo`, Sending redirects, Kernel addresses, Process tracing). The values held over a boot; Remove settings and a boot left every value as before.

**alpine323-vm (Alpine 3.23, busybox `sysctl`, its service in the boot runlevel):**

| Step | Result |
|---|---|
| The screen | Not Configured, "6 to set": ICMP redirects, IPv6 redirects, Sending redirects, Kernel addresses, FIFOs in /tmp, Files in /tmp |
| Apply settings | the file with each interface named; Operation Successful; Configured |
| A boot with the file | Partial, "2 to set": `net.ipv4.conf.default.accept_redirects` and IPv6's read 1, where both read 0 before Apply and so were not in the file. The host boots with 1; what had set them to 0 before is not known |
| Apply settings again | both added to the file; Configured; held over the next boot |
| Remove settings | the file deleted, each recorded value set back |
| A boot without it | every value as right after Remove |

So a key that passes when Apply runs but is not what the host boots with is found only after a boot, as Partial; Apply then adds it. The end state differs from the start in those two keys alone, which are now what the host boots with.

Not run on a host: Ubuntu, Proxmox VE, a host that routes, a key held by a later file, and Alpine with its service in no runlevel (decision 20: seen in a container alone).
