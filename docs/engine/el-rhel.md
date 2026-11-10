# RHEL, read only (`next/rhel`, units (a) and (b))

> **Units:** the RHEL units (the maintainer, 2026-11-10: "RHEL 10 and 9", then "Yes, start (a)", "Yes, start (b)"). (a) detection, read only, CI; (b) a host with no repositories (§5); (c) RHEL 9's cloud-init `resolv.conf`; then the host runs and the matrix rows.
> **Status:** (a) and (b) built and tested on macOS (`go test ./...`, the recording fake). The facts they rely on were read on `rhel10-test-114` (RHEL 10.3) and `rhel9-test-115` (RHEL 9.9), x86_64, by read-only probes (§2, §5), and hardn's status reads ran on both through `p4status` (§4). (b)'s refusal has not run on a host: there, only dnf's output was read.

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
| Registration | neither VM is registered: no repositories; every dnf run says "This system is not registered with an entitlement server"; `dnf install --assumeno` fails, "There are no enabled repositories" | new: unit (b). Today such a simulation fails before any change with the generic `packages:` error. Not yet read: dnf's exit codes there, and whether hardn's dnf reads on a registered host make the plugin refresh `redhat.repo` |
| DNS | RHEL 10: NetworkManager's file. RHEL 9: "; Created by cloud-init automatically, do not edit.", with cloud-init's `99-cloud-init.conf` setting NetworkManager's `dns = none` | RHEL 9: `classify` reads `ManagerFile`. No write follows (`DNS_EL_READ_ONLY` refuses every EL plan), but that refusal's reason and fix ("NetworkManager manages it there", "set the nameservers with nmcli") don't hold there: unit (c) |

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
| DNS | RHEL 10: Not Applicable, "NetworkManager". **RHEL 9: Not Applicable, "no hardn.yml"**, with `DNS_DHCP_CLIENT` ("dhclient may rewrite /etc/resolv.conf on a lease") | RHEL 9: cloud-init's file read as one hardn writes, dhclient installed. Which DHCP client NetworkManager runs there is not read. Unit (c) |
| Reboot | pending No | `dnf needs-restarting -r`: exit 0 |

The risk level is High on both (score 36.7), from the passwordless sudo alone. systemd 252 on RHEL 9 answers a missing unit's `is-enabled` with exit 1 and `is-active` with exit 3, RHEL 10's systemd with exit 4: both read as not found.

## 5. Unit (b): no enabled repository

The maintainer, 2026-11-10: "Yes, start (b)". An unregistered RHEL host has no repositories: they come with its subscription. hardn's dnf installs (firewalld from the Firewall item, dnf-automatic from Auto Updates) failed there before any change with the generic `packages:` error; they are now refused, before any change, in their own words.

- **The reading.** On both VMs (probe 3, run as hardn runs dnf: `PATH=/usr/sbin:/usr/bin:/sbin:/bin`, `LC_ALL=C`), `dnf install --assumeno …` exits 1 with one line on stderr, `Error: There are no enabled repositories in "/etc/yum.repos.d", "/etc/yum/repos.d", "/etc/distro.repos.d".`, and the subscription plugin's notices on stdout ("Updating Subscription Management repositories.", "This system is not registered with an entitlement server. …"). `packages.PlanInstall` reads a dnf exit 1 with a stderr line starting `Error: There are no enabled repositories in ` as `ErrNoRepos` (the directories are dnf.conf's `reposdir`, so only the start is matched). The same line on stdout, or with exit 0, is not.
- **The refusal** (proposed words), `PACKAGE_NO_REPOS`: "hardn can't install firewalld: dnf has no enabled repositories" (or dnf-automatic), "To go on: register the host (RHEL: subscription-manager or rhc) or enable a repository, then try again." hardn never registers a host and never runs subscription-manager.
- **redhat.repo.** `p4status` and probe 3 left `/etc/yum.repos.d/redhat.repo` as the first probe wrote it (22:01 UTC on RHEL 10, 17:01 EST on RHEL 9), when `subscription-manager` re-enabled the plugin and the file was written (by subscription-manager or the first dnf run: the probe didn't tell them apart). Later dnf runs on these unregistered hosts left it unchanged.
- **Not read yet: a registered host.** There the plugin prints "Updating Subscription Management repositories." on stdout before dnf's table, which `dnfChanges` must skip; whether it rewrites `redhat.repo` on each dnf run is not known. That needs the maintainer to register a VM. Nor is `dnf remove --assumeno` with no repository (`packages.PlanRemove`, Auto Updates' Off), which only a host that had one when hardn installed dnf-automatic can reach: there a failure would be the generic `packages:` error.

Tests: `TestPlanInstallDnf` (the VMs' output is `ErrNoRepos`; the line with exit 0, or on stdout alone, is not), `TestELInstall` (firewalld: `PACKAGE_NO_REPOS`, nothing masked or installed), `TestDnfOnRefusals` (dnf-automatic: `PACKAGE_NO_REPOS`), and the registry's goldens (`codes.golden`, `sentinels.golden`).
