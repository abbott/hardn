# RHEL, read only (`next/rhel`, units (a) to (c))

> **Units:** the RHEL units (the maintainer, 2026-11-10: "RHEL 10 and 9", then "Yes, start (a)", "Yes, start (b)", "Report it, everywhere"). (a) detection, read only, CI; (b) a host with no repositories (§5); (c) RHEL 9's cloud-init `resolv.conf` (§6); then the host runs and the matrix rows.
> **Status:** (a), (b) and (c) built and tested on macOS (`go test ./...`, the recording fake). The facts they rely on were read on `rhel10-test-114` (RHEL 10.3) and `rhel9-test-115` (RHEL 9.9), x86_64, by read-only probes (§2, §5), and hardn's status reads ran on both through `p4status` (§4). (b)'s refusal has not run on a host: there, only dnf's output was read. (c)'s reading ran on `rhel9-test-115` through `p4status` (§6).

hardn now starts on RHEL, reads it as it reads Rocky Linux and AlmaLinux, and refuses every change: the matrix names no RHEL release yet, so 10 and 9 are Unlisted ([detect.md](../safety/detect.md)). Before this unit it refused to start there ("unsupported OS type detected: rhel").

## 1. What it does

1. **One distribution, EL, no matrix row.** `detect.DistroRHEL` (`rhel`, os-release's `ID`). `Distro.EL` is true for it, so every EL reading applies: sshd.service, SELinux, firewalld or none, dnf-automatic, `dnf needs-restarting -r`, NetworkManager ([el-readonly.md](el-readonly.md), [el-changes.md](el-changes.md)). `Platform.Version` is `VERSION_ID`'s major, as for Rocky Linux and AlmaLinux (`10.3` gives `10`). CentOS Stream (`centos`) stays `DistroOther`: hardn doesn't start there.
2. **The tools.** `packages.ToolFor` gives dnf; `dns.HostOf` accepts it.
3. **The words.** The header's bar reads "RHEL 10.3". The JSON's `platform.distro` adds `rhel` ([output.md](../reference/output.md)). The engine's name is "RHEL". The EL refusals name it: "hardn doesn't change DNS on Rocky Linux, AlmaLinux or RHEL yet" (`DNS_EL_READ_ONLY`), and the firewall's three, all `FIREWALL_EL_READ_ONLY`: `-w`'s (cmd/hardn's `firewallEL`), the profiles' and interfaces' (firewall's `el.go`), and steps' `ErrELFirewall`. `REBOOT_REQUIRED`'s description names it too. On RHEL, `-w`'s refusal carries no fix: the Firewall screen it points to refuses there as well (`firewallELOn`). The docs of the EL reads (header.md, check-*.md, screen-firewall.md, vmcheck.md, dns.md, packages.md, detect.md, output.md) name RHEL; those of the EL changes (configuration.md, el-*.md) name Rocky Linux and AlmaLinux until RHEL's rows land.
4. **CI.** `go vet` and `go test` on Red Hat's Universal Base Images, `redhat/ubi10:10.2` and `redhat/ubi9:9.8`, pinned by digest. They are freely redistributable, need no subscription, set PATH, and carry tar, gzip and the CA bundle (Red Hat's rpm manifest), so they need nothing installed.

## 2. What the VMs show (2026-11-10)

Read-only probes, run over ssh from the maintainer's Mac; the full table is in the session notes (`rhel-explore.md`).

| Area | RHEL 10.3, RHEL 9.9 | Against Rocky Linux, AlmaLinux |
|---|---|---|
| os-release | `ID="rhel"`; `ID_LIKE` "centos fedora" (10), "fedora" (9); `VERSION_ID` 10.3, 9.9 | `ID_LIKE` doesn't name rhel, so only `ID` places it |
| sshd, SELinux, sudo, users, firewall, updates | as on Rocky Linux and AlmaLinux 10 and 9: sshd 9.9p1 on sshd.service, Include first; RHEL 9's crypto-policy Include in `50-redhat.conf`, which `CryptoPolicyInclude` already allows; enforcing; `@includedir` (10), `#includedir` (9); no firewalld; dnf 4, no dnf-automatic | same |
| Registration | neither VM is registered: no repositories; every dnf run says "This system is not registered with an entitlement server"; `dnf install --assumeno` fails, "There are no enabled repositories" | new: unit (b), which refuses it before any change (§5; before, the generic `packages:` error). Exit codes and the registered host: §5 |
| DNS | RHEL 10: NetworkManager's file. RHEL 9: "; Created by cloud-init automatically, do not edit.", with cloud-init's `99-cloud-init.conf` setting NetworkManager's `dns = none` | RHEL 9: `classify` read `ManagerFile` (before unit (c)). No write followed (`DNS_EL_READ_ONLY` refuses every EL plan), but that refusal's reason and fix ("NetworkManager manages it there", "set the nameservers with nmcli") didn't hold there. Since unit (c), cloud-init's, reported (§6) |

The probe's `subscription-manager status` re-enabled the dnf plugins `subscription-manager.conf` and `product-id.conf` on both VMs (rhsm's `auto_enable_yum_plugins`). hardn never runs subscription-manager.

## 3. Tests

`TestClassify` (RHEL 10.3 and 9.9 from the VMs' os-release, testdata `rhel-10.3`, `rhel-9.9`; no minor; CentOS Stream not RHEL), `TestEL`, `TestHostOS` (rhel starts; centos doesn't), `TestToolFor`, `TestReadRocky` (RHEL 10's resolv.conf reported), `TestPlanRocky` (DNS refused on RHEL), `TestOS` (the header's "RHEL 10.3"), `TestConfigureFirewallFlag` (`-w`'s words; on RHEL, no fix), and `TestDistroWords` (the JSON's `platform.distro` for every distribution).

## 4. What the VM run showed (2026-11-10)

`p4status` built from `951ca4b1` (read only by construction, [vmcheck.md](vmcheck.md)), copied to `/tmp` on each VM, run with sudo, then deleted. Both runs exited 0: 29 commands on RHEL 10 and on RHEL 9, none refused. The menu's bar reads "RHEL 10.3" and "RHEL 9.9", the platform `distro=rhel`, `support=unlisted`, and item 3 "Configure firewalld".

| Row | Both VMs | From |
|---|---|---|
| User | Configured, "non-root, sudo", with `USERS_NOPASSWD` (cloud-init's `90-cloud-init-users`) | sudoers, `sshd -T` |
| SSH Login | Partial, "root key-only" | `permitrootlogin without-password` |
| SSH Auth | Configured, "key-only auth" | |
| SSH Port | Not Configured, "default (22)" | `sshd -T`, `ss -Htln` |
| Firewall | Not Configured, "no firewall installed" | firewalld.service not found, no ufw |
| Auto Updates | Not Configured, "no dnf-automatic" | `rpm -q dnf-automatic` |
| AppArmor | Not Applicable, "uses SELinux" | `/sys/fs/selinux/enforce` `"1"` |
| DNS | RHEL 10: Not Applicable, "NetworkManager". **RHEL 9: Not Applicable, "no hardn.yml"**, with `DNS_DHCP_CLIENT` ("dhclient may rewrite /etc/resolv.conf on a lease") | RHEL 9: cloud-init's file read as one hardn writes, dhclient installed. Which DHCP client NetworkManager runs there is not read. Since unit (c) the row reads "cloud-init" (§6) |
| Reboot | pending No | `dnf needs-restarting -r`: exit 0 |

The risk level is High on both (score 36.7), from the passwordless sudo alone. systemd 252 on RHEL 9 answers a missing unit's `is-enabled` with exit 1 and `is-active` with exit 3, RHEL 10's systemd with exit 4: both read as not found.

## 5. Unit (b): no enabled repository

The maintainer, 2026-11-10: "Yes, start (b)". An unregistered RHEL host has no repositories: they come with its subscription. hardn's dnf installs (firewalld from the Firewall item, dnf-automatic from Auto Updates) failed there before any change with the generic `packages:` error; they are now refused, before any change, in their own words.

- **The reading.** On both VMs (probe 3, run as hardn runs dnf: `PATH=/usr/sbin:/usr/bin:/sbin:/bin`, `LC_ALL=C`), `dnf install --assumeno …` exits 1 with one line on stderr, `Error: There are no enabled repositories in "/etc/yum.repos.d", "/etc/yum/repos.d", "/etc/distro.repos.d".`, and the subscription plugin's notices on stdout ("Updating Subscription Management repositories.", "This system is not registered with an entitlement server. …"). `packages.PlanInstall` reads a dnf exit 1 with a stderr line starting `Error: There are no enabled repositories in ` as `ErrNoRepos` (the directories are dnf.conf's `reposdir`, so only the start is matched). The same line on stdout, or with exit 0, is not.
- **The refusal** (proposed words), `PACKAGE_NO_REPOS`: "hardn can't install firewalld: dnf has no enabled repositories" (or dnf-automatic), "To go on: register the host (RHEL: subscription-manager or rhc) or enable a repository, then try again." hardn never registers a host and never runs subscription-manager.
- **redhat.repo.** `p4status` and probe 3 left `/etc/yum.repos.d/redhat.repo` as the first probe wrote it (22:01 UTC on RHEL 10, 17:01 EST on RHEL 9), when `subscription-manager` re-enabled the plugin and the file was written (by subscription-manager or the first dnf run: the probe didn't tell them apart). Later dnf runs on these unregistered hosts left it unchanged.
- **A registered host** (the maintainer registered both VMs, 2026-11-10; probes 4 and 5, read only). Each has BaseOS and AppStream enabled (`rhel-10-for-x86_64-…`, `rhel-9-for-x86_64-…`). `dnf install --assumeno` exits 1 with "Operation aborted." on stderr, as on Rocky Linux; on stdout, above the table, "Updating Subscription Management repositories." and, while the metadata is fetched, a download line per repository. `dnfChanges` skips every line before the first section heading, so it reads the table unchanged: firewalld adds 4 packages on RHEL 10 and 9 on RHEL 9 (ipset, iptables-nft and the rest are firewalld 1.3's dependencies there), no kernel; dnf-automatic adds itself (`TestPlanInstallDnf`, the four outputs as dnf printed them). `dnf needs-restarting -r` exits 0. None of these runs rewrote `redhat.repo`: it kept its registration time on both. On RHEL 10, about a minute after registration, another dnf held the metadata lock (most likely `dnf-makecache.service`, which finished at 00:38:05 UTC; the probe didn't read the PID's unit): a simulation then exits 200 ("metadata already locked by 8511"), which hardn refuses as `PACKAGE_LOCKED`, "another dnf is running".
- **Not read yet:** `dnf remove --assumeno` with no repository (`packages.PlanRemove`, Auto Updates' Off), which only a host that had one when hardn installed dnf-automatic can reach: there a failure would be the generic `packages:` error.

Tests: `TestPlanInstallDnf` (the VMs' output is `ErrNoRepos`; the line with exit 0, or on stdout alone, is not), `TestELInstall` (firewalld: `PACKAGE_NO_REPOS`, nothing masked or installed), `TestDnfOnRefusals` (dnf-automatic: `PACKAGE_NO_REPOS`), and the registry's goldens (`codes.golden`, `sentinels.golden`).

## 6. Unit (c): cloud-init's resolv.conf

The maintainer, 2026-11-10: "Report it, everywhere". On `rhel9-test-115` cloud-init wrote `/etc/resolv.conf` as the VM booted (`/var/log/cloud-init.log`: "Writing to /etc/resolv.conf", at 16:56:38 local, eight seconds after `uptime -s`'s 16:56:30) and `/etc/NetworkManager/conf.d/99-cloud-init.conf` with `dns = none`, so NetworkManager leaves the file alone (its journal: `dns=none … rc-manager=unmanaged`).

- **The reading, on every distribution.** A regular `resolv.conf` whose first line is `; Created by cloud-init automatically, do not edit.` is `dns.ManagerCloudInit` (`cloud-init`), a manager hardn reports and never writes, as NetworkManager's and resolvconf's: the DNS row is Not Applicable, "cloud-init"; the screen's "DNS Implementation" is "cloud-init, reported only"; no nameservers are read from the file and no DHCP client finding is made; a plan is `DNS_MANAGED`: "cloud-init manages DNS here", "set the nameservers in cloud-init's network configuration, or in /etc/resolv.conf by hand" (proposed words: when cloud-init renders the network again isn't read, and nothing else rewrites the file there). A plan v0.6.0 journalled for such a file, read then as `file`, is still rolled back (`dns.valid`, `TestCheckJournalledCloudInit`). The header is cloud-init 24.4's sysconfig renderer's (`cloudinit/net/sysconfig.py:43` on both VMs, behind resolv.conf's `;`), the only form read; older cloud-init releases' headers are not.
- **The EL refusal** (`DNS_EL_READ_ONLY`) is made after `dns.Read` alone, before anything is planned (only reads run: `TestPlanRocky`), and names what the read finds (proposed words; `elRefusal`). "hardn doesn't change DNS on Rocky Linux, AlmaLinux or RHEL yet", then:
  - NetworkManager's, resolvconf's or cloud-init's file, or another program's link: `DNS_MANAGED`'s words after a colon, and its fix ("…: NetworkManager manages DNS here", "set the nameservers with nmcli");
  - systemd-resolved's link: "…: systemd-resolved manages DNS here", "set DNS= in /etc/systemd/resolved.conf, then restart systemd-resolved";
  - a file with no header, none, or one a running systemd-resolved reads as another program's (it then reads its servers from the file): the first clause alone, "set the nameservers in /etc/resolv.conf by hand";
  - a read that failed: the first clause alone, "set the nameservers by hand".

  Before, it was made before any read and always named NetworkManager.
- **On the VM** (`p4status` built from `f8dbaa03`, 2026-11-10): 29 commands, none refused; the DNS row reads Not Applicable, "cloud-init", from `/etc/resolv.conf`'s first line, with no nameservers read and no `DNS_DHCP_CLIENT` (before: "no hardn.yml", and the dhclient finding); every other row as in §4.
- **Not done:** the UI gallery has no cloud-init host (nor any EL host).

Tests: `TestReadCloudInit` (the RHEL 9 file: `ManagerCloudInit`, no nameservers, no findings, `ErrManaged`), `TestCodes` (`cloud-init`, 7), `TestCheckJournalledCloudInit` (v0.6.0's journal still rolled back; NetworkManager's file claimed as `file` still refused), the DNS control's check and refusal tables (a cloud-init file on Debian 13: "cloud-init", `DNS_MANAGED`), `TestPlanRocky` (the EL refusal's seven forms, only reads before it), and the screen's "cloud-init, reported only".
