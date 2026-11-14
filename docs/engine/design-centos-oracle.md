# CentOS Stream and Oracle Linux: design

Status: **adopted 2026-11-14**. The maintainer adopted every recommended answer of §4, in these words: "Adopt all". So each decision's (a) stands, and §4 is kept as the record. Unit 1 is built (§6); units 2 and 3 are not.

## 1. What it is

hardn refuses to start on both today: "unsupported OS type detected: centos" (or `ol`). Both are of the Enterprise Linux (EL) family hardn already changes on Rocky Linux, AlmaLinux and RHEL: dnf, SELinux, firewalld or none, NetworkManager, sshd.service. The work is to tell them apart, give them `Distro.EL`, run the EL host kit on each, and only then add matrix rows. It follows RHEL's units ([el-rhel.md](el-rhel.md)).

## 2. What is known

| | CentOS Stream | Oracle Linux |
|---|---|---|
| What it is | what RHEL's next minor release is built from; packages arrive before RHEL has them | a RHEL rebuild, as Rocky Linux and AlmaLinux |
| os-release `ID` | `centos` (el-rhel.md §1: "CentOS Stream (`centos`) stays `DistroOther`") | `ol` (a third party's sample; not read on a host) |
| `VERSION_ID` | the major alone, "10" (not read on a host) | major.minor, "10.1" (not read on a host) |
| Ends, 10 | "maintained until 2030. The exact date will be contingent on the end of the Full Support phase of RHEL 10" ([CentOS](https://blog.centos.org/2024/12/introducing-centos-stream-10/), read 2026-11-14) | June 2035, ten years from release (a third party; Oracle's own page is a PDF this session couldn't read) |
| Ends, 9 | 2027-05-31 ([CentOS](https://www.centos.org/cl-vs-cs/), read 2026-11-14) | June 2032 (same) |

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

## 3. Not known until a host is read

- **os-release on each**, as above, and `ID_LIKE`.
- **CentOS Linux 8 and 7** also say `ID="centos"`, with "8" and "7". Only 10 and 9 would get rows, so they would start, be read, and have every change refused, as RHEL 8 is today.
- **Oracle's kernel.** The default is Oracle's own (UEK). hardn's reboot reading is `dnf needs-restarting -r`; whether it answers the same there is not read.
- **Oracle's defaults**: whether firewalld is installed, the sudoers include line, the stock chrony pool, what rsyslog ships as.
- **Oracle's repositories**: free and enabled without registration, as far as I know; if not, RHEL's `PACKAGE_NO_REPOS` refusal already covers it.
- **CentOS Stream ahead of RHEL**: a newer sshd, dnf or firewalld than hardn has read anywhere.

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

Detection, read only. hardn starts on CentOS Stream and Oracle Linux and reads them as EL. Neither has a matrix row, so both are Unlisted and every change is refused. Built and tested on macOS (`make check`); nothing has run on either distribution.

- **Detection** (`pkg/host/detect`): `DistroCentOS` (`centos`), `DistroOracle` (`ol`), `EL()` true for both, named "CentOS Stream" and "Oracle Linux". CentOS Stream's version is its `VERSION_ID`, the major alone (`centosMajor`); Oracle Linux needs major.minor, as the others.
- **The tools**: dnf (`packages.ToolFor`); DNS reported (`dns.HostOf`); the header's bar reads "CentOS Stream 10", "Oracle Linux 10.1"; the JSON's `platform.distro` is `centos`, `ol`.
- **The refusals name the host's own distribution** (decision 4), on today's three too: "hardn doesn't change DNS on Oracle Linux yet" (`DNS_EL_READ_ONLY`); "hardn doesn't configure the firewall from -w on …", "hardn doesn't apply ufw's profiles or trusted interfaces on …, which uses firewalld", "hardn adds no connection limit on …, which uses firewalld", "hardn doesn't drive ufw on …, which uses firewalld" (`FIREWALL_EL_READ_ONLY`). The sentinels `dns.ErrEL` and `steps.ErrELFirewall` stay, and `errors.Is` still matches them (`steps.Worded`). Their own text has no host, so it lists all five; so does `REBOOT_REQUIRED`'s description.
- **Tests**: `TestClassify`, `TestEL`, `TestHostOS`, `TestToolFor`, `TestHostOfEL`, `TestDistroWords`, `TestOS`, `TestPlanRocky`, `TestRockyNeverUFW`, `TestStepsRefuseUFWOnRocky`, `TestConfigureFirewallFlag`.

Not verified:

- **The four os-release fixtures** (`testdata/os-release/centos-stream-10`, `centos-stream-9`, `oracle-10.1`, `oracle-9.6`) are written as best known, not read from a host. Unit 2 replaces them.
- **CentOS Linux 8 and 7** say `centos` too, so they are named "CentOS Stream".
- Everything in §3.
- No README, docs/platforms.md or roadmap change: unit 3.
- **CI.** `go vet` and `go test` on `quay.io/centos/centos:stream10` and `stream9` and on `oraclelinux:10` and `9`, pinned by digest (read from the registries, 2026-11-14). tar, gzip and the CA bundle are installed first, since what the images carry was not read. Not run yet: CI runs on GitHub alone.
