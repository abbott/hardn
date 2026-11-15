# CentOS Stream and Oracle Linux: design

Status: **adopted 2026-11-14**. The maintainer adopted every recommended answer of §4, in these words: "Adopt all". So each decision's (a) stands, and §4 is kept as the record. Units 1 to 3 are built: detection (§6), the host runs (§7), and CentOS Stream 10 and 9 and Oracle Linux 10 and 9 Supported (§8).

## 1. What it is

Before unit 1 hardn refused to start on both: "unsupported OS type detected: centos" (or `ol`). Both are of the Enterprise Linux (EL) family hardn already changes on Rocky Linux, AlmaLinux and RHEL: dnf, SELinux, firewalld or none, NetworkManager, sshd.service. The work is to tell them apart, give them `Distro.EL`, run the EL host kit on each, and only then add matrix rows. It follows RHEL's units ([el-rhel.md](el-rhel.md)).

## 2. What is known

| | CentOS Stream | Oracle Linux |
|---|---|---|
| What it is | what RHEL's next minor release is built from; packages arrive before RHEL has them | a RHEL rebuild, as Rocky Linux and AlmaLinux |
| os-release `ID` | `centos`, `ID_LIKE="rhel fedora"` | `ol`, `ID_LIKE="fedora"` |
| `VERSION_ID` | the major alone: "10", "9" | major.minor: "10.1", "9.8" |
| Ends, 10 | "maintained until 2030. The exact date will be contingent on the end of the Full Support phase of RHEL 10" ([CentOS](https://blog.centos.org/2024/12/introducing-centos-stream-10/), read 2026-11-14) | June 2035, the end of Premier Support ([Oracle](https://www.oracle.com/a/ocom/docs/elsp-lifetime-069338.pdf), read 2026-11-14) |
| Ends, 9 | 2027-05-31 ([CentOS](https://www.centos.org/cl-vs-cs/), read 2026-11-14) | June 2032 (same) |

os-release is as read on the four hosts of §7.

Where the code changes, all of it small:

| Place | Change |
|---|---|
| `pkg/host/detect/platform.go` | `DistroCentOS` (`centos`), `DistroOracle` (`ol`); `EL()` true for both; `Classify`; the matrix rows, last |
| `elMajor` | CentOS Stream's `VERSION_ID` has no minor. Today "10" gives no version, so the host would be Unlisted for ever |
| `pkg/host/detect/eol.go` | `Name()`: "CentOS Stream", "Oracle Linux" |
| `pkg/host/packages`, `pkg/host/dns` | dnf; DNS reported, as on EL |
| `pkg/output/build.go` | the JSON's `platform.distro` words |
| Eight messages that list "Rocky Linux, AlmaLinux and RHEL" | decision 4 |
| CI, README, docs/platforms.md, the other docs that list EL | the images and the names |

Everything else goes through `Distro.EL()` (31 call sites) and needs no change.

## 3. What the hosts answered

Asked before unit 2, answered by §7's hosts:

- **os-release on each**: §2.
- **CentOS Linux 8 and 7** also say `ID="centos"`, with "8" and "7". Only 10 and 9 have rows, so they start, are read, and have every change refused, as RHEL 8 (`TestClassify`; not run on a host).
- **Oracle's kernel** is Oracle's own (UEK) on both: `6.12.0-…el9uek`, `6.12.0-…el10uek`. `dnf needs-restarting -r` exits 0 there ("Reboot should not be necessary."), and Oracle's plugin lists `kernel-uek` and `kernel-uek-core` among the packages that ask for a reboot. Its answer after a kernel update is not read.
- **Oracle's defaults**: firewalld is installed, active and enabled, its public zone letting cockpit, dhcpv6-client and ssh through. semanage is not installed. The stock chrony pool and what rsyslog ships as are not read.
- **Oracle's repositories**: the installs ran from `ol9_appstream`, `ol9_baseos_latest`, `ol10_appstream` and `ol10_baseos_latest`.
- **CentOS Stream ahead of RHEL**: every step passed (firewalld 1.3.4-21.el9 and 2.4.3-4.el10).

## 4. Decisions for the maintainer

1. **The units.** (a) *Recommended:* RHEL's order. Unit 1: detection only; both start and are read, every change refused (Unlisted), CI on their images. Unit 2: the EL host kit on four VMs. Unit 3: the matrix rows, README and docs. (b) Rows at once, on the strength of the RHEL runs: one unit, but "Supported" with nothing run on either. Gates every decision below.
2. **CentOS Stream's level.** (a) *Recommended:* Supported, 10 and 9, once the kit passes, as the others. (b) Best effort ("hardn changes it but makes no promises"), since its packages move before RHEL's: honest, but every plan there then carries the `PLATFORM_BEST_EFFORT` finding. Oracle Linux 10 and 9 would be Supported either way.
3. **CentOS Stream 10's last day.** The vendor names a year, and ties the day to RHEL 10's full support, which Red Hat ends on 2030-05-31. (a) *Recommended:* 2030-05-31, with that source in the comment. (b) No date, as Proxmox VE 9: no end-of-support notice ever shows there. Stream 9's is 2027-05-31, so its notice starts on 2027-03-02 (90 days before).
4. **The messages that list the family.** Eight say "Rocky Linux, AlmaLinux and RHEL" (or "or"), such as "hardn doesn't change DNS on Rocky Linux, AlmaLinux or RHEL yet". (a) *Recommended:* name the host's own: "hardn doesn't change DNS on Oracle Linux yet". Shorter, and no list to keep. It changes the words on today's three as well. (b) List all five. (c) "on Enterprise Linux", a term the screens don't use yet.
5. **The JSON's `platform.distro`.** (a) *Recommended:* os-release's `ID`, as the others (`almalinux`, `rhel`): `centos` and `ol`. (b) `centos-stream` and `oracle`: clearer, but no longer the host's own ID.
6. **The short names**, in the README's line and table and in the docs: (a) *Recommended:* "CentOS Stream" and "Oracle". "CentOS" alone would read as CentOS Linux, which stays out. (b) "CentOS" and "Oracle".

## 5. Needed from the maintainer

Four throwaway VMs, x86_64, each with a NAME and NAME-root ssh alias: CentOS Stream 10 and 9, Oracle Linux 10 and 9 (default kernel). They gate unit 2 and so unit 3; unit 1 needs none.

## 6. Unit 1, built

Detection, read only. hardn starts on CentOS Stream and Oracle Linux and reads them as EL. Neither had a matrix row until unit 3, so both were Unlisted and every change refused. Built and tested on macOS (`make check`).

- **Detection** (`pkg/host/detect`): `DistroCentOS` (`centos`), `DistroOracle` (`ol`), `EL()` true for both, named "CentOS Stream" and "Oracle Linux". CentOS Stream's version is its `VERSION_ID`, the major alone (`centosMajor`); Oracle Linux needs major.minor, as the others.
- **The tools**: dnf (`packages.ToolFor`); DNS reported (`dns.HostOf`); the header's bar reads "CentOS Stream 10", "Oracle Linux 10.1"; the JSON's `platform.distro` is `centos`, `ol`.
- **The refusals name the host's own distribution** (decision 4), on today's three too: "hardn doesn't change DNS on Oracle Linux yet" (`DNS_EL_READ_ONLY`); "hardn doesn't configure the firewall from -w on …", "hardn doesn't apply ufw's profiles or trusted interfaces on …, which uses firewalld", "hardn adds no connection limit on …, which uses firewalld", "hardn doesn't drive ufw on …, which uses firewalld" (`FIREWALL_EL_READ_ONLY`). The sentinels `dns.ErrEL` and `steps.ErrELFirewall` stay, and `errors.Is` still matches them (`steps.Worded`). Their own text has no host, so it lists all five; so does `REBOOT_REQUIRED`'s description.
- **Tests**: `TestClassify`, `TestEL`, `TestHostOS`, `TestToolFor`, `TestHostOfEL`, `TestDistroWords`, `TestOS`, `TestPlanRocky`, `TestRockyNeverUFW`, `TestStepsRefuseUFWOnRocky`, `TestConfigureFirewallFlag`.

Since:

- **The four os-release fixtures** are each host's own (unit 2): `testdata/os-release/centos-stream-10`, `centos-stream-9`, `oracle-10.1` and `oracle-9.8`, which replaces `oracle-9.6`.
- **CentOS Linux 8 and 7** say `centos` too, so they are named "CentOS Stream".
- **CI.** `go vet` and `go test` on `quay.io/centos/centos:stream10` and `stream9` and on `oraclelinux:10` and `9`, pinned by digest (read from the registries, 2026-11-14). tar, gzip and the CA bundle are installed first, since what the images carry was not read. Not run yet: CI runs on GitHub alone.

## 7. Unit 2: the host runs

The RHEL host kit ([el-rhel.md](el-rhel.md) §7), copied to `co-hostkit` and adapted, ran on 2026-11-14 on `centos9-test-119`, `centos10-test-120`, `oracle9-test-117` (Oracle Linux 9.8) and `oracle10-test-118` (Oracle Linux 10.1), x86_64, with hardn v0.9.0 built from `851c0abd` plus unit 3's four matrix rows. Every step passed on all four, and the clean-up left each `SAME AS BEFORE`, a new login on 22 working.

| Step | CentOS Stream 10 and 9 | Oracle Linux 10.1 and 9.8 |
|---|---|---|
| Linux tests | `go vet` clean; `go test ./...`: 84 packages ok, none failed (Go 1.27.4); `p4status` exit 0 | the same |
| Users, root login (`-u -c`, `-d`) | the test user logs in by key; root login off rolled back by its 1-minute timer, then confirmed from the Mac | the same |
| SSH port | semanage installed already, so the refusal and its install were skipped; 22 to 2230 labelled `ssh_port_t`, confirmed on 2230; back to 22, confirmed | refused at the port field: "SELinux is on, and semanage isn't installed to let sshd use port 2230."; policycoreutils-python-utils installed; then both moves with firewalld running: `firewall-cmd --zone=public --add-port=2230/tcp`, runtime and permanent, confirmed. 2230/tcp stays in the zone after the move back ([el-ports.md](el-ports.md) §5) |
| Firewall | Install: transaction 1, no kernel (9: firewalld and 8 packages; 10: and 3), not active, not enabled, not masked; Enable confirmed, ssh let through; a port move with firewalld running rolled back by its timer; Disable; Enable rolled back by its timer | running from the start, so Install is never offered; a move to 2240 rolled back by its timer; Disable; Enable rolled back by its timer; Enable confirmed |
| Auto Updates | a dry run changes nothing; On; Off; dnf-automatic installed by hand, then On and Off ("Stay on") | 9.8: the same. 10.1: On refused before asking (below); installed by hand, then On and Off |
| The screens | the header shows hardn v0.9.0 and "CentOS Stream 10" or "CentOS Stream 9"; no Reboot pending line; the Install Timer row reads Stopped dnf-automatic-install.timer; no unit name cut short | the same, with "Oracle Linux 10.1" or "Oracle Linux 9.8" |

**What differs from RHEL:**

- **Oracle Linux comes with firewalld running** and without semanage. CentOS Stream is as RHEL: semanage installed, no firewalld.
- **Oracle Linux 10.1 was behind its repositories**, so an install would upgrade installed packages. hardn refused Auto Updates' On before asking: "Installing dnf-automatic would also change other packages: Upgrading dnf, Upgrading dnf-data, Upgrading libdnf, Upgrading python3-dnf, Upgrading python3-hawkey, Upgrading python3-libdnf, Upgrading yum. Install it by hand, then try again." Nothing changed. The kit installed the version matching the installed dnf (`dnf-automatic-4.20.0-18.0.1.el10`), and On and Off passed. The dry run was refused the same way; On and Off from "not installed" did not run there.
- **The semanage refusal's fix**, `dnf install policycoreutils-python-utils`, would upgrade 8 SELinux packages on that host. The kit installed the matching version (`3.9-1.el10`) instead.
- *Both wordings changed, 2026-11-15.* Where every other change is an upgrade, Auto Updates' On now refuses with "Installing dnf-automatic would upgrade other packages: dnf, dnf-data, … Update the host (`dnf upgrade`), then try again."; any other mix reads as before. The semanage fix now reads "Install it (`dnf install policycoreutils-python-utils`, which may upgrade SELinux packages), then plan again."
- **The kit stopped three times**, none a hardn defect: the test user's sudo asks a password in v0.9.0, which the kit's login check didn't expect (all four); the kit's pattern for the port field's prompt (all four); the refusal above (Oracle Linux 10.1). Each run was taken up again from the step.

**Not run:** Run All, `-w` and `-r`; a reboot; DNS; Time Sync; Log Forwarding; backup; `PACKAGE_NO_REPOS`; arm64; CentOS Linux 8 and 7.

## 8. Unit 3: supported

CentOS Stream 10 and 9 and Oracle Linux 10 and 9 join the matrix as Supported, for the same changes as Rocky Linux, AlmaLinux and RHEL (`platform.go`; `TestMatrix`: 17 Supported).

| Row | Ends | Source (read 2026-11-14) |
|---|---|---|
| CentOS Stream 10 | 2030-05-31 | CentOS names 2030 and ties the day to the end of RHEL 10's full support (decision 3) |
| CentOS Stream 9 | 2027-05-31 | [CentOS](https://www.centos.org/cl-vs-cs/) |
| Oracle Linux 10 | June 2035 | the end of Premier Support, [Oracle](https://www.oracle.com/a/ocom/docs/elsp-lifetime-069338.pdf) |
| Oracle Linux 9 | June 2032 | the same |

Oracle's PDF was downloaded and its text extracted on 2026-11-14. Its rows, under "Release, GA Date, Premier Support", then the end of Extended Support: "Oracle Linux 9 June 2022 June 2032 June 2035" and "Oracle Linux 10 June 2025 June 2035 June 2038".

Docs changed: the README (its line, support table, "Tested" and links), [platforms.md](../platforms.md) (end of support, the two tables, and the family's section: a host behind its repositories, CentOS Linux 8 and 7), [detect.md](../safety/detect.md), usage.md, configuration.md, contributing.md, packages.md, dns.md, `hardn --help`'s first line and the release notes.
