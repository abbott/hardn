# Proxmox VE: root key-only, SSH on 22, the cluster's keys and pve-firewall (`pkg/host/pve`)

> **Unit:** refactor plan Phase 2, `next/p2-pve` (decision §3.18, approved 2026-10-17: no SSH port change on Proxmox VE; brief §2 decision "Proxmox hosts: no UFW, and root SSH stays key-only"; brief 2.5). V-numbers: V8 on Proxmox VE (the port), V10 (UFW on Proxmox VE) and V11 (`PermitRootLogin no` on Proxmox VE) ([side-effects.md](../refactor/phase0/side-effects.md)); risk R15 (plan §4); the open item `next/p0-hazards` left to this unit (legacy `ConfigureSSH` can write an `AllowUsers` that excludes root). The choice to add root to such an `AllowUsers`, rather than refuse it, is [P-XX-pve](../refactor/proposals.md#p-xx-pve).
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only, with synthetic Proxmox VE 9 fixtures (`pkg/host/pve/testdata/pve9`) written from the sources in §3, not captured from a host. Nothing has run on Linux or on Proxmox VE; §4 is the manual checklist, "written, not run (gate 5)".

On a Proxmox VE host hardn keeps root's SSH login key-only and never `no`, keeps sshd on port 22, keeps root reachable by key, edits root's keys through Proxmox VE's link to the cluster-wide file and never over it, never locks root's password, and reports `pve-firewall` instead of using UFW. `pve.PlanSSH` and `pve.CheckPort` apply those rules to what hardn is about to write; `pve.CheckSSH` reads `sshd.ReadEffective`'s values against them; `pve.ReadKeys`, `PlanKeys`, `ApplyKeys` and `UndoKeys` edit `/root/.ssh/authorized_keys`; `pve.CheckAccount` guards root's password; `pve.ReadFirewall` reads `pve-firewall`'s state.

## 1. Why

origin/main treats Proxmox VE as Debian: it enables UFW (V10), writes `PermitRootLogin no` (V11), changes the SSH port before anything allows it (V8) and writes an `AllowUsers` that can leave root out (`docs/refactor/p0-hazards.md`, "Still open", on `next/p0-hazards`, which is not in this stack). On Proxmox VE each of those can cut a cluster apart: the nodes log in to each other as root, by key, on port 22, for the console proxy, migration and replication; UFW competes with `pve-firewall` and can cut off the web UI on 8006 and corosync; and root's `authorized_keys` is a link to a file every node shares, so renaming a new file over it breaks the link, and any key hardn adds or removes there applies to every node (plan R15; [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "Proxmox join / detection"). Brief §3 principle 1: never lock the user out.

## 2. Guarantees

### Which hosts (`Applies`)

1. **Only on Proxmox VE, and never on a guess.** Every entry point takes `detect.Detect`'s `Proxmox` fact ([detect.md](detect.md) §2.8). `Installed` Yes applies the rules; No leaves the input as it is (`PlanSSH` returns a copy, `CheckPort` and `CheckAccount` return nil) or is `ErrNotApplicable` (`ReadKeys`, `ReadFirewall`); anything else is `ErrProxmoxUnknown`, wrapping detect's reason, never "not Proxmox VE": V10 and V11 are critical. The rules that need the cluster file system (the keys, the firewall) also need `ClusterFS` Yes: pmxcfs leaves `/etc/pve` as an empty directory when it stops, so with it down the files are not there (§3).
2. **No UFW.** UFW is the `ufw` unit's, and its `ufw.Applies` already returns `ErrNotApplicable` on Proxmox VE and `ErrProxmoxUnknown` when that is not established ([ufw.md](ufw.md) §2.1); `TestNoUFW` pins it from this side. The firewall status on Proxmox VE is `ReadFirewall` (item 13).

### SSH settings hardn writes (`PlanSSH`, `CheckPort`)

3. **`PermitRootLogin prohibit-password`, always.** `PlanSSH(p, desired, rootGroups)` takes the settings hardn wants in `00-hardn.conf` (`[]migrate.Directive`, as [sshd-migrate.md](sshd-migrate.md) §6 hands them on) and sets every `PermitRootLogin` to `prohibit-password` (`no`, `yes` and `forced-commands-only` alike; `without-password` is the same value, sshd_config(5)), and adds one when `desired` has none: Proxmox VE's installer and `pvecm` write `PermitRootLogin yes` into `sshd_config`, which the drop-in must beat (§3). Each change is a `FindingRootKeyOnly` whose detail ends "root: key-only (required by Proxmox clustering)", the wording brief 2.5 asks for. It never writes `no` on Proxmox VE, whatever the configuration says (V11).
4. **SSH stays on port 22 (§3.18).** A `Port` other than exactly 22 (a second port, a service name) and a `ListenAddress` with an explicit port other than 22 (`0.0.0.0:2230`, `[::]:2230`) are refused with `ErrPortChange`, whose text gives the reason: "cluster SSH and pve-firewall's management rule use port 22". A `ListenAddress` with no port, or on 22, passes. `CheckPort(p, ports)` is the same rule for a port change itself: nil for no change or `{22}`, `ErrPortChange` for anything else on Proxmox VE, `ErrProxmoxUnknown` when that is not established. Nothing here moves a host that already listens elsewhere back to 22; `CheckSSH` reports it (item 8).
5. **root stays reachable by key.** sshd's checks, in sshd_config(5)'s terms (§3), all have to let root in with a key from any node:
   - an `AllowUsers` with no entry matching root from every host (`root`, `r*`; not `root@192.0.2.0/24`, since the other nodes' addresses are not read) gets `root` added to its first line, with a `FindingRootAdded` (`AllowUsers alice` becomes `AllowUsers alice root`). [P-XX-pve](../refactor/proposals.md#p-xx-pve) asks the maintainer whether to keep this or refuse instead;
   - a `DenyUsers` entry whose user part matches root (with or without a host), a `DenyGroups` pattern matching one of root's groups, a `DenyGroups` at all when root's groups are not established, an `AllowGroups` matching none of them, `PubkeyAuthentication no`, an `AuthenticationMethods` with no list that is `any` or `publickey` alone (a node's `BatchMode` login offers a key only), and an `AuthorizedKeysFile` that names neither `.ssh/authorized_keys`, `%h/.ssh/authorized_keys`, `/root/.ssh/authorized_keys` nor `/etc/pve/priv/authorized_keys` (origin/main's `.ssh_%u` alone, V5) are refused with `ErrRootExcluded`;
   - `rootGroups` comes from `RootGroups(ctx, sys)`: root's primary group (`LookupUser`) and every `/etc/group` line listing root. `nil` means not established, and then only the group `root` counts.
   
   Patterns are sshd's `*` and `?` (ssh_config(5), PATTERNS). A `Match` or `Include` line is refused with `fs.ErrInvalid`, as `migrate.PlanFor` refuses it, since these rules read every line as global. `PlanSSH` never changes its input slice.

### SSH as sshd reads it (`CheckSSH`)

6. **Read from `sshd -T`, never from the files.** `CheckSSH(e, rootGroups)` takes `sshd.ReadEffective`'s `Effective` ([sshd-effective.md](sshd-effective.md)) and returns three facts as `detect.Tri`, with findings saying why any is not Yes:
   - `RootKeyOnly`: `PermitRootLogin` is `prohibit-password` (OpenSSH 10.0p1, on Proxmox VE 9, prints `without-password`, which `Effective` already reads as `prohibit-password`), and no `Match` probe printed another value (one that does makes it No);
   - `RootByKey`: every check of item 5 passes on the effective values, and `AuthorizedKeysFile` reads root's file or, failing that, `AuthorizedKeysCommand` is `none` (a command may supply root's keys, which makes it Unknown rather than No). A value that is not established, a pattern that lets root in (or keeps it out) from some hosts only, or a `Match` probe that changed one of `PermitRootLogin`, `PubkeyAuthentication` or the user and group lists for some connections makes it Unknown; any check that keeps root out makes it No;
   - `Port22`: the Port set is `{22}` and every `listenaddress` sshd printed is on 22; otherwise No, with a `FindingPortNot22` naming each port and the reason. Not established Port or no listener line: Unknown.

   `sshd -T` prints `authenticationmethods any` when none is set, `authorizedkeysfile` with sshd's default files, and `authorizedkeyscommand none` when none is set (§3), so a missing line is Unknown, not a default.
7. **Proxmox VE's own `PermitRootLogin yes` is not drift.** A `shadowed` conflict ([sshd-effective.md](sshd-effective.md) §2.9) whose other line is `PermitRootLogin yes` in `/etc/ssh/sshd_config` is reported as a `FindingProxmoxRootLogin` ("expected, not drift"), with its file and line. The same keyword at `no`, in a drop-in, or as an `overridden` line (read before hardn's) is not Proxmox VE's line and is not classified so (tested). Before hardn writes its drop-in, `yes` is simply the value: `RootKeyOnly` is No. On a host without the stock Include, where hardn's block sits at the top of `sshd_config`, `pvecm create` and `add` rewrite hardn's own first `PermitRootLogin` line to `yes` ([sshd-validate.md](sshd-validate.md) §2.1, `clobberable`); `CheckSSH` then reports `RootKeyOnly` No, as it should.
8. **What `CheckSSH` does not see.** It reads only `Effective`: a `Match` block that sets `AuthenticationMethods` or `AuthorizedKeysFile` is not in `Effective.Overrides`, an untargeted `Match` block has no probe, and a probe sshd's package could not read is one of its own findings; the caller reports `ReadEffective`'s findings beside these. Like `Effective`, it is configuration, not what a daemon that has not reloaded runs with.

### Root's keys (`ReadKeys`, `PlanKeys`, `ApplyKeys`, `UndoKeys`)

9. **Through the link, never over it.** `ReadKeys` refuses anything but the state Proxmox VE keeps: `/root/.ssh/authorized_keys` a symlink, resolved with `ResolveSymlink` (so one in a directory another user can change is `host.ErrSymlinkParent`, [atomic.md](atomic.md) §2.6), to exactly `/etc/pve/priv/authorized_keys`, which exists. A regular file, a missing link, a link elsewhere and a missing cluster file are `ErrKeysLink` (`pvecm updatecerts` restores that state, and `pveproxy` runs it at every start, §3; hardn does not); pmxcfs not mounted is `ErrClusterFSDown`, so nothing is ever written into the empty `/etc/pve` directory under the mount point. It reads the file with `ReadFileNoFollow`, the cluster's node names from `/etc/pve/nodes`, and this node's own root key from `/root/.ssh/id_rsa.pub` when there is one. `ApplyKeys` writes with `host.WriteFileThroughSymlink` (atomic.md §2.3), so the new file is renamed over the cluster file inside `/etc/pve/priv`, and the link itself is never written, renamed or replaced (tested: no call names it, its target is unchanged, and `backup.Restore` restores through it).
10. **Edits keep every other line.** `PlanKeys(k, KeyEdit{Add, Remove})` is pure. A line to add must be one authorized_keys key line (sshd(8), AUTHORIZED_KEYS FILE FORMAT: optional options, with spaces only inside double quotes; a key type; a base64 key whose blob starts with that type's name, RFC 4253 §6.6; a comment), else `ErrBadKey`; it is appended unless a line with the same type and key is there (options and comment are not compared, so a key is never there twice). A key to remove is named by any line with its type and key, and every line with them goes. Comments, blank lines, options and order are kept byte for byte. **Removing a cluster node's root key is refused** with `ErrClusterKey`: this node's (`id_rsa.pub`), or one whose comment is `root@<node>` for a node in `/etc/pve/nodes` (the other nodes log in with them; the comment convention is **[unverified]**, §3), and so is an edit that leaves no key. Every plan that changes the file carries a `FindingClusterWide`: "the change applies to root on every node of the cluster". `Narrows()` (a key removed) marks a lockout-risk change for `p2-revert`; adding one is not.
11. **Backed up first, and never over a change it did not plan.** `ApplyKeys(ctx, sys, p, run, plan)` needs a backup run (`ErrNoBackup` without one), reads the keys again and refuses with `ErrKeysChanged` when their SHA-256 or the link's target is not what the plan was made from (another node, or `pvecm updatecerts`, wrote the file meanwhile: pmxcfs replicates it), takes a `RollbackRequired` backup of `/root/.ssh/authorized_keys` (which records the link, its target and the cluster file's content, atomic.md §2.12), then writes with mode `0600` and the owner left alone: pmxcfs accepts a chmod to `0600` under `priv/` and no other, and a chown only to root and `www-data` (§3). A plan with no change writes nothing.
12. **The undo keeps other nodes' keys.** `UndoKeys` reverses a plan on the file as it is now: it removes the lines the plan added and puts back the lines it removed, and leaves every other line. Restoring the whole file from the backup would drop a key another node wrote in the meantime (tested: bob's key, added "on another node" after the apply, survives the undo). It does not guard node keys (it only removes what the plan added) and does not compare-and-swap: a write by another node between its read and its write can still be lost. Run it under `context.WithoutCancel` after Ctrl-C ([system.md](system.md) §2.5).

### Root's password (`CheckAccount`)

13. **Never locked, deleted or expired.** `CheckAccount(p, user, op)` returns `ErrRootPassword` for root on Proxmox VE for `OpLockPassword` (`passwd -l`, `usermod -L`), `OpDeletePassword` (`passwd -d`) and `OpExpireAccount` (`usermod -e 1`, `chage -E 0`), whether pmxcfs is up or not: the cluster join logs in to the existing node's API as `root@pam` with the password (§3), and the web UI's `root@pam` login uses it too **[I]**, while SSH stays key-only. `ErrProxmoxUnknown` for root when that is not established; nil for any other user, or another host. Changing root's password is not one of these ops. No code path in hardn locks a password today (legacy included); this is the guard the user-management slice calls.

### The firewall, read-only (`ReadFirewall`)

14. **Read, never changed or run.** `ReadFirewall(ctx, sys, p)` reads `/etc/pve/firewall/cluster.fw` and `/etc/pve/local/host.fw` (pmxcfs's link `local` → `nodes/<node>`, the directory pve-firewall reads `host.fw` from) and runs exactly one command, `systemctl is-active` of the unit that applies them: `pve-firewall.service`, or `proxmox-firewall.service` when `host.fw` sets `nftables: 1`. It never runs `pve-firewall status`, which takes the firewall's lock and compiles the rules, never touches UFW, iptables or nftables, and writes nothing (tested on every case: no call that changes the fake, and the one run). With pmxcfs not mounted it makes no call at all and every field is Unknown with `Err` wrapping `ErrClusterFSDown`. Off Proxmox VE it is `ErrNotApplicable` or `ErrProxmoxUnknown`; the only other error is a done context.
15. **The facts.** A missing file is its defaults, a file that cannot be read makes what it decides Unknown (`Err` names it, wrapping `ErrStateUnknown`), and so does a value outside the form pve-firewall itself reads:
   - `Enabled`: `cluster.fw`'s `enable` is an integer, 0 when unset, and on when not 0; `host.fw`'s `enable` is 0 or 1, 1 when unset. Yes needs both on, No either off;
   - `PolicyIn`: `cluster.fw`'s `policy_in`, `ACCEPT`, `DROP` or `REJECT` in any case, `DROP` when unset;
   - `NFTables`: `host.fw`'s `nftables`, 0 or 1, 0 when unset;
   - `Unit` and `Service.Active`: the word systemctl printed (`IsActive()` reads it as detect does);
   - `Management`: the entries of `cluster.fw`'s IP set `management`, as written; pve-firewall adds the cluster's local network to that set itself (`pve-firewall localnet` shows it; not run);
   - a non-empty IP set `blacklist` is a `FindingBlacklist`: every host's firewall drops traffic from it.
16. **How far 22 and 8006 are let through (`Reach`).** "Allowed" is never a bare yes, because pve-firewall's own rule allows both ports only from the management IP set (§3):
   - not established whether the firewall is on, or the service not active (a stopped service leaves whatever rules it loaded, which are not read; a running one rereads the files every 10 seconds): `ReachUnknown`;
   - firewall off, service running: `ReachNotFiltered` (pve-firewall filters nothing; other packet filters are not read);
   - `nftables: 1`: `ReachUnknown`, with a `FindingNotModelled` (proxmox-firewall is a tech preview and its rules are not read);
   - otherwise `ReachAnywhere` for policy `ACCEPT`, `ReachManagement` for `DROP` or `REJECT` (an accept rule may let more sources in), **unless a rule may drop the port first**. pve-firewall puts the host's rules, then the cluster's, before its built-in management rules (§3), so every enabled rule in `host.fw`'s or `cluster.fw`'s `[RULES]` that may drop or reject a new TCP connection to the port makes it `ReachUnknown`, with a `FindingRuleMayBlock` naming the file and line. "May" is decided conservatively: `ACCEPT`, a macro with `(ACCEPT)`, an `OUT` or `FORWARD` rule, a disabled (`|`) rule, a rule for another protocol (`-p udp`) and one whose numeric `-dport`/`--dport` list leaves the port out cannot; a `GROUP` (security groups are not read), any other macro (its ports are not read), a port by name and anything else may.

### Through `host.System`

17. Every read and write goes through `host.System` and is in the run log; the package's non-test files may not import `os`, `io/ioutil`, `golang.org/x/sys`, `syscall`, `net` or `plugin` (depguard rule `pve-through-host`, checked with a probe file importing `os`). Finding codes are stable (`TestFindingCodes`): `root-key-only`, `root-allowed`, `proxmox-root-login`, `port-not-22`, `root-unreachable`, `cluster-wide-keys`, `rule-may-block`, `blacklist`, `firewall-not-modelled`.

## 3. Platform facts relied on

The audit's research note `research-alpine-proxmox.md` (in the audit evidence outside the repository, plan header) is cited below as "research"; every row it supports was verified there against the primary source named, and the rows marked "read 2026-10-18" were read again for this unit.

| Fact | Source | Used for |
|---|---|---|
| Proxmox VE's ISO installer and `pvecm create`/`add` (`setup_sshd_config`) write `PermitRootLogin yes` into `/etc/ssh/sshd_config`; hardn's drop-in has to win by sshd's order, and the line is not drift | research §6(d) **[V]** (pve-installer `Proxmox/Install.pm` 1436-1443; pve-cluster `src/PVE/Cluster/Setup.pm` 141-156); [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "Proxmox join / detection"; [sshd-validate.md](sshd-validate.md) §3 | §2.3, §2.7 |
| Nodes reach each other with `ssh -o BatchMode=yes … root@<ip>` and no port option (key-only, port 22); "An SSH tunnel on TCP port 22 between nodes is required"; `PermitRootLogin no` would break it, `prohibit-password` does not | research §6(c), (d) **[V]** (pve-cluster `src/PVE/SSHInfo.pm`; pve-docs Cluster Manager, Requirements and "Role of SSH") | §2.3, §2.4, §2.5 |
| The default cluster join (`pvecm add`, the GUI) logs in to the existing node's API as `root@pam` with root's password and the certificate fingerprint; SSH password login is not needed ("administrators may keep PasswordAuthentication no") | research §6(a) **[V]** (pve-cluster `src/PVE/CLI/pvecm.pm` 403-427, `src/PVE/Cluster/Setup.pm` 700-721; pve-docs commit 7a641568, 2026-05-03) | §2.13 |
| The web UI's `root@pam` login uses root's Linux password | **[I]** (research §6, "inferred") | §2.13 |
| `setup_ssh_keys` moves a regular `/root/.ssh/authorized_keys` to `.org` and links it to `/etc/pve/priv/authorized_keys`, shared by the cluster; `ssh_merge_keys` merges `.org` and always re-adds the node's key; `pveproxy.service` runs `pvecm updatecerts --silent` at every start, which re-asserts the link; `setup_rootsshconfig` generates `/root/.ssh/id_rsa` | research §6(b) **[V]** (pve-cluster `Setup.pm`, `ClusterConfig.pm`; pve-manager `services/pveproxy.service`) | §2.9, §2.10 |
| A node's root key carries the comment `root@<node>` (ssh-keygen's default `user@host` for the key `setup_rootsshconfig` generates) | **[unverified]**: neither the `ssh-keygen` call in `Setup.pm` nor ssh-keygen(1) was read | §2.10: an extra guard; the node's own `id_rsa.pub` is compared by key |
| pve-firewall builds the host's input chain with the host's rules, then the cluster's ("add host rules first, so that cluster wide rules can be overwritten"), then the management IP set's accepts for `--dport 8006`, `5900:5999`, `3128`, `22` and `60000:60050`; the policy is `$cluster_options->{policy_in} \|\| 'DROP'`; nothing is generated when `cluster.fw`'s `enable` is false; the host firewall is off only when `host.fw` sets `enable` to 0; host options such as `nftables` are read as `(0\|1)`; a rule line starting with `\|` is disabled; `host.fw` is `/etc/pve/nodes/$nodename/host.fw` | pve-firewall [`src/PVE/Firewall.pm`](https://git.proxmox.com/?p=pve-firewall.git;a=blob_plain;f=src/PVE/Firewall.pm;hb=refs/heads/master) (`enable_host_firewall`, the option parsers, `parse_fw_rule`), read 2026-10-18; research §7 **[V]** | §2.15, §2.16 |
| `cluster.fw` `[OPTIONS]`: `enable` integer, default 0; `policy_in` `ACCEPT \| DROP \| REJECT`; `[RULES]`, `[IPSET <name>]`, `[GROUP <name>]`, `[ALIASES]`; `host.fw` `[OPTIONS]`: `enable` boolean default 1, `nftables` boolean default 0 (tech preview); `#` starts a comment; `DIRECTION ACTION [OPTIONS]`, `DIRECTION MACRO(ACTION)`, `\|` disables a rule, `GROUP <name>`, `-dport` port lists and `a:b` ranges; the `management` IP set gets the local cluster network automatically; the `blacklist` set is dropped by every host's firewall; with the firewall on and policy DROP/REJECT, TCP 8006 and 22 are still allowed from management hosts; `systemctl status proxmox-firewall` for the nftables service | [pve-docs, Firewall](https://pve.proxmox.com/mediawiki/index.php?title=Firewall&action=raw) ("Configuration Files", "Firewall Rules", "IP Sets", "Default firewall rules", "nftables"), read 2026-10-18 | §2.14-§2.16 |
| pve-firewall's service unit is `pve-firewall.service` | pve-firewall [`debian/`](https://git.proxmox.com/?p=pve-firewall.git;a=tree;f=debian;hb=refs/heads/master) (`pve-firewall.service`, `pvefw-logger.service`), read 2026-10-18 | §2.14 |
| The daemon updates the rules every 10 s and re-hooks its chains into INPUT; `pve-firewall status` compiles the rules, takes the firewall lock and needs root | research §7 **[V]** (`src/PVE/Service/pve_firewall.pm`, `$updatetime=10`, `run_locked`) | §2.14, §2.16 |
| pmxcfs creates the link `local` → `nodes/<nodename>` (and `qemu-server`, `lxc`, `openvz`) | pve-cluster [`src/pmxcfs/pmxcfs.c`](https://git.proxmox.com/?p=pve-cluster.git;a=blob_plain;f=src/pmxcfs/pmxcfs.c;hb=refs/heads/master) `create_symlinks`, read 2026-10-18; [pve-docs, pmxcfs](https://pve.proxmox.com/mediawiki/index.php?title=Proxmox_Cluster_File_System_(pmxcfs)&action=raw), "Symbolic links" | §2.14: `/etc/pve/local/host.fw` |
| pmxcfs's node name and Firewall.pm's `$nodename` are the same name | **[unverified]** (both are the host's short name **[I]**) | §2.14 |
| pmxcfs: `chmod` succeeds only for `0600` under a private path and `0640` elsewhere (else `EPERM`); `chown` only to uid 0 or -1 and the `www-data` gid or -1; `getattr` reports group `www-data` and masks modes to `0700`/`0600` under `priv/`, `0755` for directories and links and `0750` for files elsewhere; `rename`, `create` and `unlink` are implemented, `fsync` and `fsyncdir` are not; "O_EXCL creates were not atomic"; read-only when the node loses quorum; `/etc/pve/priv/` is root's alone | pmxcfs.c (`cfs_fuse_chmod`, `cfs_fuse_chown`, `cfs_fuse_getattr`, `fuse_ops`), read 2026-10-18; pve-docs pmxcfs ("POSIX Compatibility", "File Access Rights") | §2.9, §2.11 |
| The kernel's FUSE client treats `ENOSYS` from `FSYNC` and `FSYNCDIR` as success and stops sending them | linux v6.18 [`fs/fuse/file.c`](https://raw.githubusercontent.com/torvalds/linux/v6.18/fs/fuse/file.c) `fuse_fsync`, [`fs/fuse/dir.c`](https://raw.githubusercontent.com/torvalds/linux/v6.18/fs/fuse/dir.c) `fuse_dir_fsync`, read 2026-10-18 | §2.11: `WriteFileAtomic`'s fsyncs do not fail on pmxcfs |
| libfuse answers `ENOSYS` for an operation the file system does not implement | **[unverified]** | §2.11 |
| pmxcfs leaves `/etc/pve` as an empty directory when it stops; Proxmox VE detection is the pve-manager dpkg status or `/usr/bin/pveversion`; pmxcfs mounted is a separate fact | [detect.md](detect.md) §2.8 and §3 | §2.1 |
| `AllowUsers`/`DenyUsers`: user name patterns; `USER@HOST` checks both; `AllowUsers` is not consulted for users `DenyUsers` matches; `AllowGroups` matches the primary or a supplementary group; `PermitRootLogin` `yes`, `prohibit-password` (alias `without-password`), `forced-commands-only`, `no`; `AuthenticationMethods any` is "the default behaviour of accepting any single authentication method", and a list needs every method in it; `AuthorizedKeysFile` is absolute or relative to the home directory, takes `%h`, `%u`, `%U` and `%%`, defaults to `.ssh/authorized_keys .ssh/authorized_keys2`, and `none` skips files | [sshd_config(5)](https://man.openbsd.org/sshd_config), read 2026-10-18 (OpenBSD's current page; the portable release on Proxmox VE 9, 10.0p1, is assumed the same here **[I]**) | §2.5, §2.6 |
| `*` matches any run of characters, `?` one | [ssh_config(5)](https://man.openbsd.org/ssh_config), PATTERNS, read 2026-10-18 | §2.5 |
| `sshd -T` prints `authenticationmethods any` when none is set (`dump_cfg_strarray_oneline`), `authorizedkeysfile` with the default files filled in (`fill_default_server_options`), `authorizedkeyscommand none` for none, and one line per `AllowUsers`/`DenyUsers`/`AllowGroups`/`DenyGroups` value | OpenSSH [V_10_0_P1 `servconf.c`](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_0_P1/servconf.c), read 2026-10-18 | §2.6 |
| Proxmox VE 9 runs Debian 13's OpenSSH 10.0p1, which prints `without-password`, and `ssh.service` | [sshd-effective.md](sshd-effective.md) §3; [detect.md](detect.md) §3 | fixtures |
| authorized_keys: one key per line, `#` and blank lines are comments; options, key type, base64 key, comment; no spaces in options except within double quotes | [sshd(8)](https://man.openbsd.org/sshd), AUTHORIZED_KEYS FILE FORMAT, read 2026-10-18 | §2.10 |
| A public key blob starts with the key format name as an SSH string | RFC 4253 §6.6 | §2.10 |

## 4. Written, not run (gate 5): the Proxmox VE 9 checklist

Nothing here has run on Proxmox VE or Linux. By hand on a Proxmox VE 9 node of a two-node test cluster (plan `p2-vmtest`, "Proxmox VE 9 (manual)"), as root, with a second session open on the node's console (web UI shell) before any change:

1. **Detection.** `detect.Detect`: `Proxmox.Installed` Yes, `ClusterFS` Yes; with `systemctl stop pve-cluster`, `ClusterFS` No: `ReadKeys` refuses with `ErrClusterFSDown` and `ReadFirewall` is Unknown with no call; start it again.
2. **Root key-only over Proxmox VE's yes.** Write `00-hardn.conf` from `PlanSSH` (for example `PasswordAuthentication no`, `AllowUsers alice`), through `sshd.Validate` and the reload: `sshd -T` prints `permitrootlogin without-password` while `sshd_config` still says `PermitRootLogin yes`; `CheckSSH` gives key-only, root by key and port 22, with one `proxmox-root-login` finding at `sshd_config`'s line. Run `pvecm updatecerts` and `systemctl restart pveproxy`: the same. On a node created afterwards, check what `pvecm create`/`add` wrote.
3. **Root key login on 22, password rejected.** From the other node, `ssh -o BatchMode=yes root@<node> true` succeeds; `ssh -o PubkeyAuthentication=no root@<node>` is refused; a VM migration and the web UI's node shell between the nodes work. The web UI's `root@pam` login still works (the password is never locked, §2.13).
4. **Port change refused.** `PlanSSH` with `Port 2230` and `CheckPort(…, {2230})` refuse with the reason; nothing is written.
5. **The keys link.** `ApplyKeys` adding alice's key: `/root/.ssh/authorized_keys` is still the same link (`readlink`), `/etc/pve/priv/authorized_keys` has the key on both nodes, no `.authorized_keys.hardn-*` file is left in `/etc/pve/priv`, and the write's steps on pmxcfs succeed: the `O_CREAT|O_EXCL` temporary file, the `fchmod` to `0600`, the skipped chown, both fsyncs (§3, the libfuse row), the `renameat` inside `priv/`. With the node out of quorum (the other node off), the write fails closed and the file is unchanged. `backup.Restore` of that run puts the old content back through the link (its `fchown` to root and `www-data` accepted). `UndoKeys` after adding bob's key on the other node keeps bob's.
6. **Revert timer for a non-port change** (plan `p2-vmtest`). A lockout-risk change on the node, such as `PermitRootLogin yes` to `prohibit-password`, left unconfirmed, is rolled back at the deadline; confirmed with `sudo hardn confirm` (or as root) from a new session, it stays ([revert.md](revert.md) §4).
7. **pve-firewall.** `ReadFirewall` on the stock node (firewall off; `systemctl is-active pve-firewall.service` printing `active` there is **[unverified]**, and `inactive` would make every reach Unknown); after enabling it at the datacenter with an IP set `management` holding the admins' network; with `policy_in: ACCEPT`; with a host rule `IN DROP -p tcp -dport 22`; with `nftables: 1` and `proxmox-firewall` installed. Compare each `Reach` with `iptables-save` and with a connection from inside and from outside the management network. Check that `/etc/pve/local/host.fw` is the file pve-firewall reads (the node-name row of §3).
8. **No UFW.** No `ufw` command appears in the run log of any of the above.

## 5. API

```go
package pve

const SSHPort, WebPort = 22, 8006
const RootLogin = "prohibit-password"
const GroupFile = "/etc/group"
const RootKeys = "/root/.ssh/authorized_keys"
const ClusterKeys = "/etc/pve/priv/authorized_keys"
const NodeKey = "/root/.ssh/id_rsa.pub"
const NodesDir = "/etc/pve/nodes"
const KeysPerm fs.FileMode = 0o600
const ClusterFW = "/etc/pve/firewall/cluster.fw"
const HostFW = "/etc/pve/local/host.fw"
const FirewallUnit = "pve-firewall.service"
const NFTablesUnit = "proxmox-firewall.service"

var ErrNotApplicable, ErrProxmoxUnknown, ErrPortChange, ErrRootExcluded, ErrRootPassword,
	ErrClusterFSDown, ErrKeysLink, ErrKeysChanged, ErrBadKey, ErrClusterKey, ErrNoBackup, ErrStateUnknown error

func Applies(p detect.Proxmox) (bool, error)

type FindingKind uint8 // FindingRootKeyOnly, FindingRootAdded, FindingProxmoxRootLogin, FindingPortNot22,
	// FindingRootUnreachable, FindingClusterWide, FindingRuleMayBlock, FindingBlacklist, FindingNotModelled; String() is the code
type Finding struct { Kind FindingKind; File string; Line int; Detail string }

// SSH
func PlanSSH(p detect.Proxmox, desired []migrate.Directive, rootGroups []string) ([]migrate.Directive, []Finding, error)
func CheckPort(p detect.Proxmox, ports []uint16) error
func RootGroups(ctx context.Context, sys host.System) ([]string, error)
type SSHCheck struct { RootKeyOnly, RootByKey, Port22 detect.Tri; Findings []Finding }
func CheckSSH(e sshd.Effective, rootGroups []string) SSHCheck

// Root's keys
type Keys struct { Target string; Data []byte; Nodes []string; Own string }
type KeyEdit struct { Add, Remove []string }
type KeysPlan struct { Target string; Before [32]byte; Old, New []byte; Added, Removed []string; Findings []Finding }
func (p KeysPlan) Changed() bool
func (p KeysPlan) Narrows() bool
func ReadKeys(ctx context.Context, sys host.System, p detect.Proxmox) (Keys, error)
func PlanKeys(k Keys, e KeyEdit) (KeysPlan, error)
func ApplyKeys(ctx context.Context, sys host.System, p detect.Proxmox, run *backup.Run, plan KeysPlan) (backup.Result, error)
func UndoKeys(ctx context.Context, sys host.System, p detect.Proxmox, plan KeysPlan) error

// Root's password
type AccountOp uint8 // OpLockPassword, OpDeletePassword, OpExpireAccount
func CheckAccount(p detect.Proxmox, user string, op AccountOp) error

// pve-firewall
type Reach uint8 // ReachUnknown (zero), ReachNotFiltered, ReachAnywhere, ReachManagement; String()
type Firewall struct {
	Enabled    detect.Tri
	PolicyIn   string
	NFTables   detect.Tri
	Unit       string
	Service    detect.UnitState // Active only
	Management []string
	SSH, Web   Reach
	Findings   []Finding
	Err        error // wraps ErrStateUnknown
}
func ReadFirewall(ctx context.Context, sys host.System, p detect.Proxmox) (Firewall, error)
```

**Import rules** (depguard, `.golangci.yml`): `host-stdlib` holds the package to the standard library and `pkg/host`; `pve-through-host` (§2.17) denies its non-test files the packages that reach the host behind the seam. Its tests use `hostfake` and `os` (to read `testdata`).

## 6. Not in this unit; inputs to later units

- **The legacy paths are not changed.** On this stack legacy `cmd/hardn` still changes the SSH port on Proxmox VE (V8) and `ConfigureSSH` still writes the configured `AllowUsers`, which can leave root out; UFW and `PermitRootLogin no` there are gated on `next/p0-hazards` (its `proxmoxTreatedAsDebian`), which is not in this stack. Phase 2 does not touch `pkg/cmd`, `pkg/adapter` or `pkg/menu`; wiring `CheckPort` and `PlanSSH`'s `AllowUsers` rule into the legacy SSH path is for slice 6, or for the merge with `next/p0-hazards`.
- **`p3-engine`:** on Proxmox VE, pass the engine's `00-hardn.conf` settings through `PlanSSH` before `migrate.PlanFor` and `sshd.Validate` (with `RootGroups`' answer), and show its findings in the plan; call `CheckPort` before any port change and `CheckAccount` before any account change. `PermitRootLogin prohibit-password` over Proxmox VE's `yes` narrows root's login paths (no password): it is a lockout-risk change under confirm-or-revert of its own, made after any migration, since `migrate.Apply` refuses a `Desired` that narrows (`migrate.Input.Desired`); over a v0.3.2 `manage.conf`'s `PermitRootLogin no` it widens, which a migration accepts ([sshd-migrate.md](sshd-migrate.md) §6). Plan root's keys with `ReadKeys`/`PlanKeys`, show `FindingClusterWide`, and use `Narrows()` for the risk class. **Undo a keys change with `UndoKeys`, not with `backup.Restore`**: a whole-file restore drops keys other nodes wrote during the confirm window (§2.12). `p2-revert`'s journal has no step for that today (its undo steps are `run`, `chmod`, `chown` and `rmdir`, and its backups are restored whole), so the engine needs a journal step for an inverse keys edit, or to accept the whole-file restore for this file: that is `p2-revert`'s and `p3-engine`'s design. With the engine applying `Config.RootLogin()`, flip `config.prohibitPasswordApplied` ([sshd-migrate.md](sshd-migrate.md) §2.21).
- **`p4-check-ssh` on Proxmox VE:** status from `CheckSSH`: Configured when key-only, root by key and port 22 are Yes, with the detail "root: key-only (required by Proxmox clustering)"; `proxmox-root-login` is not drift and not a risk factor; a No in `RootByKey` or `Port22` is a lockout finding for the cluster.
- **`p4-check-firewall` / `p4-risk` on Proxmox VE:** status from `ReadFirewall`; `ReachManagement` is "allowed from the management network", not "allowed"; `ReachNotFiltered` is the stock state (the cluster firewall is off by default), not a blocked port. How these score is `p4-risk`'s.
- **The user-management slice:** `CheckAccount` before any password lock; root's keys only through this package on Proxmox VE.
- **[P-20](../refactor/proposals.md#p-20):** managing pve-firewall's rules (today read-only).
- **Not modelled:** proxmox-firewall's rules; security groups' contents, macros' ports, aliases and the `management` set's automatic members; IPv4 and IPv6 told apart; VM and VNet firewalls; any packet filter other than pve-firewall; the other nodes' addresses (so a `USER@HOST` pattern for root is Unknown). A `Match` block setting `AuthenticationMethods` or `AuthorizedKeysFile` (§2.8).
- **Size:** about 1,250 lines of non-test Go (much of it doc comments) and 930 of tests, against the plan's guideline of about 500: five rule sets (SSH settings, effective SSH, the keys link with its backup and undo, the password guard, the firewall reader) over sshd's, pmxcfs's and pve-firewall's own formats, and a test per refusal.
