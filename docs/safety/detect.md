# Detect: platform facts (`pkg/host/detect`)

> **Unit:** refactor plan Phase 2, `next/p2-detect` (built before the other P2 units, which branch on it).
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only, with os-release fixtures copied from the vendors' sources. Nothing has run on Linux; §4 lists what is "written, not run (gate 5)". The legacy `pkg/osdetect` is unchanged (§6).

`detect.Detect(ctx, sys)` establishes the facts every later Phase 2 unit branches on: the distribution and its place in the support matrix, the init system, how sshd is started, Proxmox VE, containers, and the elevation tool. It is a set of functions over `host.System`: it reads files and runs four kinds of read-only query, and changes nothing.

## 1. Why

The legacy `pkg/osdetect` accepts only the `debian`, `ubuntu` and `alpine` IDs, never reads `ID_LIKE`, calls a host Proxmox when `/etc/pve` exists (or cannot be stat'ed), has no container, init or release gating, and falls back to a **fabricated "debian 11"** when anything fails ([phase0-findings.md §6](../refactor/phase0-findings.md#6-architecture-map-originmain-the-worktree-base), "osdetect"; [side-effects.md §5](../refactor/phase0/side-effects.md)). The lockout defects that need a platform fact to fix are V10 and V11 (UFW and `PermitRootLogin no` on Proxmox), V12 (`ssh.socket`), and V14 (Alpine elevation) ([side-effects.md](../refactor/phase0/side-effects.md), V-list). A wrong fact there locks a user out; an Unknown one stops the change.

## 2. Guarantees

1. **Unknown is a value, never a guess.** Every fact type's zero value is its Unknown (`Tri` `Unknown`, `InitUnknown`, `SSHUnknown`, `DistroUnknown`, `SupportUnknown`, `SudoFlavorUnknown`), and every fact carries an `Err` saying why it is Unknown, wrapping the cause for `errors.Is`/`errors.As`: `fs.ErrPermission`, `host.ErrNotFound`, a `*host.ExitError`, `ctx.Err()`, or one of `ErrConflict` (two signals disagree), `ErrUnrecognised` (output or file content not in a known form), `ErrNoSignal` (no signal present). A fact is No only when every check it needs was made; a check that could not be made (a permission error, an unexpected exit) leaves it Unknown. `Detect` returns an error only when `ctx` is done, since then any fact may be Unknown for that reason alone.
2. **Read-only.** Detection calls `ReadFile`, `Stat` and `Readlink`, and `Run`s exactly these: `dpkg-query --show --showformat=${Status}\t${Version}\n pve-manager`, `systemctl is-enabled|is-active ssh.socket|ssh.service` (on systemd only), and `sudo --version` (when sudo is present). It never runs `pveversion` (a Perl program that loads APT; it is only stat'ed). The SSH, Proxmox, elevation and whole-host tests assert that the fake recorded nothing but reads and these runs.
3. **Never the `sshd` unit name.** On systemd only `ssh.socket` and `ssh.service` are queried, on every distribution; the `sshd` alias is missing on a fresh Ubuntu 24.04 (§3). Tested: every recorded run in the SSH tests is checked.
4. **os-release** is read as os-release(5) says: `/etc/os-release`, and only when it is missing `/usr/lib/os-release`, never a mix; an `/etc/os-release` that exists but cannot be read is not passed over. Values are parsed as a shell would read assignments (unquoted, `'…'`, `"…"` with `\$ \" \\ \``, a backslash escaping the next character), with no expansion; a line that is not such an assignment is dropped; a repeated key keeps its last value. No `ID` means Unknown.
5. **Support matrix as data** (`Matrix()`, brief §2):

   | Support | Releases |
   |---|---|
   | Supported | Debian 13, 12; Ubuntu 26.04, 24.04; Alpine 3.24, 3.23; Proxmox VE 9 |
   | BestEffort | Ubuntu 22.04; Alpine 3.22, 3.21 (vendor support ends 2026-11-01) |
   | OutOfScope | Ubuntu interim releases (by rule: any `YY.MM` that is not April of an even year); Proxmox VE 8 (end of life 2026-08-31) |
   | Unlisted | any other version (Debian 11, Ubuntu 20.04, Debian testing/sid without `VERSION_ID`, Alpine edge), and any other `ID`; a derivative reports its first known `ID_LIKE` in `Platform.Like`, never a support claim |
   | SupportUnknown | os-release unreadable or without `ID`; a Debian `ID` whose Proxmox facts are not established (Proxmox VE keeps Debian's os-release); a Proxmox VE whose pve-manager version is not established |

   The key is `VERSION_ID` for Debian and Ubuntu, `VERSION_ID`'s major.minor for Alpine (`3.24.2` → `3.24`), and pve-manager's major version for Proxmox VE (`9.2.22` → `9`). Rows change by editing `matrix` in `platform.go`; `TestMatrix` pins the row counts.
6. **Init system.** `Systemd` when `/run/systemd/system` is a directory (`Running` Yes); `OpenRC` when `/run/openrc` is (`Running` Yes) or, failing both, when `/sbin/openrc-run` is installed (`Running` No: OpenRC present but not what booted, as in a container). Both run directories present is `ErrConflict`; neither a run directory nor `openrc-run` is `ErrNoSignal`.
7. **SSH mode.** On systemd: `SSHSocket` when `ssh.socket` is enabled or active; else `SSHService` when `ssh.service` is enabled or active; `ErrConflict` when both units are enabled; `ErrNoSignal` when neither is enabled or active (including both `not-found`); `ErrUnrecognised` for a state that decides neither (`static`, `activating`) or output systemctl(1) does not document. The non-zero exit both verbs give for an "off" state is read as an answer, not a failure. `Generator` reports `/usr/lib/systemd/system-generators/sshd-socket-generator` separately: a socket host without it (Debian opted in) changes its port through `ssh.socket.d`, not `sshd_config`. On OpenRC: `SSHOpenRC` when `/etc/init.d/sshd` exists (with `Init.Running` No, OpenRC did not boot the host and cannot start it). Without an established init system nothing is run.
8. **Proxmox VE.** `Installed` is Yes when dpkg reports pve-manager `installed` or `/usr/bin/pveversion` exists (either signal is enough: treating a host as Proxmox applies the more cautious rules), No only when dpkg says it is not installed (exit 1 "no packages found", `not-installed`, `config-files`, or no `dpkg-query` at all) **and** `pveversion` is absent; anything else (`half-configured`, exit 2) is Unknown. `ClusterFS` (pmxcfs mounted) is a separate fact, read from `/proc/self/mounts`: the last mount at `/etc/pve` decides, a FUSE type (`fuse`, `fuse.<subtype>`) is Yes, any other type is Unknown. `EtcPVE` (`/etc/pve` exists) is reported as a hint and decides nothing.
9. **Containers.** Checked in systemd's order, stopping at the first that fires, and `Signal` names it: `/run/host/container-manager` (non-empty), `/run/systemd/container` (non-empty), `container=` in `/proc/1/environ`, `/run/.containerenv` (podman), `/.dockerenv` (docker), then the PID namespace: `/proc/self/ns/pid` read as `pid:[inode]` and compared with the initial namespace's `0xEFFFFFFC`. `Manager` is what the signal names (`lxc`, `docker`, …; empty for the namespace check). Not detected is No only when every check was made: as non-root, `/proc/1/environ` is unreadable and the result is Unknown unless another check fires.
10. **Elevation.** `sudo` and `doas` are looked for as `host.Real` resolves a command: the first regular file with an execute bit in `host.SearchPath` (symlinks followed, so Ubuntu 26.04's alternatives chain resolves). When sudo is present, `sudo --version`'s first line tells `SudoRS` (`sudo-rs 0.2.13`) from `SudoOriginal` (`Sudo version 1.9.16p2`); anything else, or a failed run, leaves the flavour Unknown with `Elevation.Err`.

## 3. Platform facts relied on

### Fact × platform (what detection should report on a stock install)

| Fact | Debian 13 | Debian 12 | Ubuntu 26.04 | Ubuntu 24.04 | Alpine 3.24 / 3.23 | Proxmox VE 9 |
|---|---|---|---|---|---|---|
| os-release `ID` / `VERSION_ID` / codename | `debian` / `13` / `trixie` | `debian` / `12` / `bookworm` | `ubuntu` / `26.04` / `resolute` (`ID_LIKE=debian`) | `ubuntu` / `24.04` / `noble` | `alpine` / `3.24.2`, `3.23.6` / none | `debian` / `13` / `trixie` |
| `/etc/os-release` | link to `../usr/lib/os-release` | same | same | same | same | same (Debian's) |
| Init | systemd | systemd | systemd | systemd | OpenRC | systemd |
| SSH mode | `ssh.service` | `ssh.service` | `ssh.socket` | `ssh.socket` | OpenRC `sshd` | `ssh.service` |
| `sshd-socket-generator` | no | no | yes | yes | n/a | no |
| pve-manager / pmxcfs | not installed / no | not installed / no | not installed / no | not installed / no | no dpkg / no | installed / yes while pve-cluster runs |
| sudo | optional package | optional package | sudo-rs | sudo | not installed (community repo) | not installed **[I]** |
| doas | no | no | no | no | yes (`setup-alpine` default) | no |

Ubuntu 22.04 (best effort) is `ssh.service`; a 22.04 → 24.04 upgrade is migrated to `ssh.socket`. Fixtures: `pkg/host/detect/testdata/os-release/` holds the os-release files for every row above, Ubuntu 22.04 and 25.10, Alpine 3.22 and 3.21, and Debian sid, generated from the sources below.

### Sources

| Fact | Source | Used for |
|---|---|---|
| os-release format, `/etc` before `/usr/lib` used exclusively, later repeats win, `ID_LIKE` for unrecognised IDs | os-release(5), [systemd v257 `man/os-release.xml`](https://raw.githubusercontent.com/systemd/systemd/v257/man/os-release.xml) | §2.4, §2.5 `Like` |
| Debian 12/13 and sid os-release contents; `/etc/os-release` → `../usr/lib/os-release` | Debian base-files [13.8+deb13u7](https://sources.debian.org/data/main/b/base-files/13.8+deb13u7/etc/os-release), [12.4+deb12u15](https://sources.debian.org/data/main/b/base-files/12.4+deb12u15/etc/os-release), [14.2](https://sources.debian.org/data/main/b/base-files/14.2/etc/os-release), `debian/rules` (the link, `#OSNAME#`) | fixtures; §2.5 |
| Ubuntu 22.04/24.04/25.10/26.04 os-release contents and the same link | Ubuntu base-files, `git.launchpad.net/ubuntu/+source/base-files`, branches `applied/ubuntu/{jammy,noble,questing,resolute}-devel` (`etc/os-release`, `debian/rules`) | fixtures |
| Alpine os-release: `VERSION_ID=$pkgver` (three components), no codename, link into `/usr/lib` | aports `{3.21,3.22,3.23,3.24}-stable/main/alpine-base/APKBUILD`, `release()` ([3.24](https://gitlab.alpinelinux.org/alpine/aports/-/raw/3.24-stable/main/alpine-base/APKBUILD)) | fixtures; major.minor key |
| Proxmox VE keeps `ID=debian`; detection is the pve-manager dpkg status or `/usr/bin/pveversion`; `/etc/pve` stays as an empty directory when pmxcfs is down; pmxcfs is FUSE at `/etc/pve` | [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "Proxmox join / detection" **[V]**; [side-effects.md §4.3](../refactor/phase0/side-effects.md); primary sources pve-cluster `src/pmxcfs/pmxcfs.c`, `debian/pve-cluster.dirs`, pve-manager `bin/pveversion` (git.proxmox.com) | §2.8 |
| The exact type string pmxcfs shows in `/proc/mounts` | **[unverified]**; the kernel prints a FUSE mount's type as `fuse` or `fuse.<subtype>` (linux v6.18 [`fs/proc_namespace.c`](https://raw.githubusercontent.com/torvalds/linux/v6.18/fs/proc_namespace.c) `show_type`; `fs/fuse/inode.c` `.name = "fuse"`) | §2.8: any FUSE type is Yes, anything else Unknown |
| `/proc/<pid>/mounts` is in fstab(5) format; space, tab, newline, `\` and `#` are octal escapes | [proc_pid_mounts(5)](https://man7.org/linux/man-pages/man5/proc_pid_mounts.5.html), [fstab(5)](https://man7.org/linux/man-pages/man5/fstab.5.html); `fs/proc_namespace.c` (`mangle`, `seq_escape(m, s, " \t\n\\#")`) | `ParseMounts` |
| `dpkg-query --showformat` escapes `\t` `\n`; exit 1 = no package found, 2 = fatal | dpkg-query(1), [dpkg `man/dpkg-query.pod`](https://salsa.debian.org/dpkg-team/dpkg/-/raw/main/man/dpkg-query.pod) (`--showformat`, EXIT STATUS) | §2.8 |
| systemd booted ⇔ `/run/systemd/system/` exists | sd_booted(3), [systemd v257 `man/sd_booted.xml`](https://raw.githubusercontent.com/systemd/systemd/v257/man/sd_booted.xml), Notes | §2.6 |
| OpenRC's state directory is `/run/openrc` on Linux; `openrc-run` is installed in `/sbin` (Alpine 3.24 ships OpenRC 0.63.2 with `--sbindir=/sbin`) | OpenRC 0.63.2 [`src/librc/rc.h.in`](https://raw.githubusercontent.com/OpenRC/openrc/0.63.2/src/librc/rc.h.in) (`RC_SVCDIR`); OpenRC `meson.build`, `src/openrc-run/meson.build`; aports [`3.24-stable/main/openrc/APKBUILD`](https://gitlab.alpinelinux.org/alpine/aports/-/raw/3.24-stable/main/openrc/APKBUILD) | §2.6 |
| is-enabled states and their exit codes; is-active prints the state and exits 0 only when active; the unit active states | systemctl(1), [systemd v257 `man/systemctl.xml`](https://raw.githubusercontent.com/systemd/systemd/v257/man/systemctl.xml) (`is-enabled` table, `is-active`); `man/unit-states.xml` (ACTIVE states) | §2.7 `unitStates` |
| Ubuntu 24.04/26.04 socket-activated by default with `sshd-socket-generator` at `/usr/lib/systemd/system-generators/`; 22.04 `ssh.service` (upgrade migrates); Debian/PVE `ssh.service`, socket opt-in with no generator, its port then set in `ssh.socket.d` | [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "`ssh.socket` per Ubuntu version" **[V]**; primary sources Ubuntu openssh `debian/rules`, `sshd-socket-generator.patch`, Debian openssh `debian/rules`, `README.Debian` | §2.7 |
| Service names: `ssh` on Debian/Ubuntu/PVE, the `sshd` alias missing on fresh Ubuntu 24.04; Alpine OpenRC `/etc/init.d/sshd` | phase0-findings.md §10, "Service names / init" **[V]** (LP #2061055; aports `main/openssh/APKBUILD`) | §2.3, §2.7 |
| Container signals and their order; `/.dockerenv` last because it can end up in an image; the root PID namespace inode is fixed since kernel 3.8 | systemd v257 [`src/basic/virt.c`](https://raw.githubusercontent.com/systemd/systemd/v257/src/basic/virt.c) `detect_container`, `detect_container_files`, `running_in_pidns`; `src/basic/namespace-util.c`; OpenRC 0.63.2 `src/librc/librc.c` `detect_container` | §2.9 |
| `PROC_PID_INIT_INO` = `PID_NS_INIT_INO` = `0xEFFFFFFC` | linux v6.18 [`include/uapi/linux/nsfs.h`](https://raw.githubusercontent.com/torvalds/linux/v6.18/include/uapi/linux/nsfs.h), `include/linux/proc_ns.h` | §2.9 |
| `/proc/<pid>/ns/pid` is a link whose content is `pid:[inode]` | [namespaces(7)](https://man7.org/linux/man-pages/man7/namespaces.7.html) | §2.9 (`Readlink`) |
| LXC sets `container=lxc` for init; reading `/proc/1/environ` needs `CAP_SYS_PTRACE` (root) | LXC [`src/lxc/start.c`](https://raw.githubusercontent.com/lxc/lxc/main/src/lxc/start.c) (`putenv("container=lxc")`); systemd v257 `virt.c` (comments on `/run/systemd/container` and `getenv_for_pid`) | §2.9 |
| Ubuntu 26.04's sudo is sudo-rs; Alpine's default elevation is doas (`/etc/doas.d/20-wheel.conf`), sudo is in community; sudo is likely absent on PVE **[I]** | [phase0-findings.md §12](../refactor/phase0-findings.md#12-proposed-adjustments-to-the-brief), rows 25 and 26; [side-effects.md §4.2, §4.4](../refactor/phase0/side-effects.md); plan `p2-sudoers`; primary sources Ubuntu `rust-sudo-rs` `debian/sudo-rs.alternatives`, alpine-conf `setup-user.in` | §2.10, matrix |
| `sudo-rs --version` prints `sudo-rs <version>`; sudo prints `Sudo version <version>` | sudo-rs v0.2.13 [`src/sudo/mod.rs`](https://raw.githubusercontent.com/trifectatechfoundation/sudo-rs/v0.2.13/src/sudo/mod.rs) (`SudoAction::Version`); sudo 1.9.17p2 [`src/sudo.c`](https://raw.githubusercontent.com/sudo-project/sudo/v1.9.17p2/src/sudo.c) | §2.10 |
| Ubuntu LTS every two years, in April (22.04, 24.04); interim releases every six months with nine months of updates | [ubuntu.com/about/release-cycle](https://ubuntu.com/about/release-cycle); the "April of an even year" rule is read off its table | §2.5 `ubuntuInterim` |
| Support levels; PVE 8 end of life 2026-08-31; Alpine 3.21 support ends 2026-11-01 | brief §2 (decided; PVE 8's date); [phase0-findings.md §12](../refactor/phase0-findings.md#12-proposed-adjustments-to-the-brief), row 29 (Alpine 3.21); plan R16 | §2.5 |

## 4. Written, not run (gate 5)

Nothing here has run on Linux. On the Debian 13, Ubuntu 26.04 and Alpine 3.24 VMs (`next/p2-vmtest`), by hand on Proxmox VE 9, and in the gate-5 containers (`debian:13/12`, `ubuntu:26.04/24.04`, `alpine:3.24/3.23`):

- `Detect` on each host reports the row of the fact × platform table in §3, with no Unknown fact and no `Err`;
- the systemctl text: `is-enabled`/`is-active` of `ssh.socket` and `ssh.service` on Ubuntu 24.04 and 26.04 before and after the first connection (`ssh.service`'s enablement state there is assumed `disabled`), on Debian 13 and PVE 9, and on a Debian host opted into the socket; Ubuntu 22.04 → 24.04 upgraded host;
- `dpkg-query --show --showformat=${Status}\t${Version}\n pve-manager` passed as one argv, without a shell, on Debian 13 (exit 1) and PVE 9 (`install ok installed\t9.x`), and `ErrNotFound` on Alpine;
- **the type string pmxcfs shows at `/etc/pve` in `/proc/self/mounts`** (§3, unverified row), with pve-cluster running and stopped;
- `/proc/1/environ` readable as root, and the container checks in an LXC CT on PVE (`container=lxc`), in Docker (`/.dockerenv`, own PID namespace), in Podman (`/run/.containerenv`) and on the bare hosts (initial PID namespace `pid:[4026531836]`);
- OpenRC: `/run/openrc` on the Alpine VM, `Running` No in an `alpine` container with `openrc` installed;
- `sudo --version` on Ubuntu 26.04 (sudo-rs through the alternatives link, as root) and Debian; doas resolving from `SearchPath` on Alpine.

## 5. API

```go
package detect

func Detect(ctx context.Context, sys host.System) (Facts, error) // error only when ctx is done

type Facts struct {
	OS        OSRelease
	Platform  Platform
	Init      Init
	SSH       SSH
	Proxmox   Proxmox
	Container Container
	Elevation Elevation
}

type Tri uint8 // Unknown (zero), No, Yes
var ErrConflict, ErrUnrecognised, ErrNoSignal error

func ReadOSRelease(ctx context.Context, sys host.System) OSRelease
func ParseOSRelease(data []byte) map[string]string
type OSRelease struct { Path, ID string; IDLike []string; VersionID, VersionCodename, PrettyName string; Err error } // Known() bool

func Classify(osr OSRelease, pve Proxmox) Platform // pure
func Matrix() []Release                             // a copy
type Distro string   // DistroUnknown "", DistroDebian, DistroUbuntu, DistroAlpine, DistroProxmox, DistroOther
type Support uint8   // SupportUnknown (zero), Supported, BestEffort, OutOfScope, Unlisted
type Release struct { Distro Distro; Version string; Support Support; Note string }
type Platform struct { Distro, Like Distro; Version string; Support Support; Note string; Err error }

func DetectInit(ctx context.Context, sys host.System) Init
type InitKind uint8 // InitUnknown (zero), Systemd, OpenRC
type Init struct { Kind InitKind; Running Tri; Source string; Err error }

func DetectSSH(ctx context.Context, sys host.System, in Init) SSH
type SSHMode uint8 // SSHUnknown (zero), SSHSocket, SSHService, SSHOpenRC
type UnitState struct { Enabled, Active string } // systemctl's words
type SSH struct { Mode SSHMode; Socket, Service UnitState; Generator Tri; Source string; Err error }

func DetectProxmox(ctx context.Context, sys host.System) Proxmox
type Proxmox struct { Installed Tri; Version, Source string; ClusterFS Tri; ClusterFSType string; EtcPVE Tri; Err error }
func ParseMounts(data []byte) []Mount
type Mount struct { Source, Dir, Type, Options string }

func DetectContainer(ctx context.Context, sys host.System) Container
type Container struct { Detected Tri; Signal, Manager string; Err error }

func DetectElevation(ctx context.Context, sys host.System) Elevation
type SudoFlavor uint8 // SudoFlavorUnknown (zero), SudoOriginal, SudoRS
type Tool struct { Present Tri; Path string; Err error }
type Elevation struct { Sudo Tool; SudoFlavor SudoFlavor; SudoVersion string; Doas Tool; Err error }

// Paths and constants: EtcOSRelease, UsrOSRelease, SystemdRunDir, OpenRCRunDir, OpenRCRun,
// SocketGenerator, OpenRCSSHD, PVEVersionBin, PVEDir, SelfMounts, ContainerManagerFile,
// SystemdContainerFile, InitEnviron, PodmanEnvFile, DockerEnvFile, SelfPIDNamespace,
// InitPIDNamespaceInode.
```

**Import rule:** the package sits under `pkg/host`, so depguard's `host-stdlib` rule holds it to the standard library and `pkg/host` (`.golangci.yml`); its tests use `hostfake`.

## 6. Not in this unit, and inputs for later ones

- **The legacy `pkg/osdetect` is unchanged**, fabricated fallback included: its callers are legacy screens and operations, and replacing it changes their behaviour (brief §3 principle 2). Each slice moves its area onto `detect.Facts`; the brief's `Control.Applies(osdetect.OSInfo)` sketch becomes `Applies(detect.Facts)` in `p3-engine`.
- **No caching.** `Detect` costs six commands at most (one `dpkg-query`, four `systemctl`, one `sudo --version`), each in the run log. Whoever calls it decides how long the facts hold; `p4-header` can call `ReadOSRelease` alone.
- `p2-reload`: act only on a known `SSH.Mode`; `SSHSocket` without `Generator` is a Debian host opted into the socket, whose port lives in `ssh.socket.d`; never name the `sshd` unit (§2.3).
- `p2-sudoers`: `Elevation` (sudo vs sudo-rs, doas) per §3.21; sudo-rs's `env_keep` handling stays unverified until gate 5.
- `p2-pve`: Proxmox rules when `Proxmox.Installed` is Yes (Unknown on a Debian ID is Unknown platform, so no rule set is picked); firewall facts Unknown when `ClusterFS` is not Yes.
- Phase 4 (AppArmor): NotApplicable when `Container.Detected` is Yes; container detection runs before any `/sys/module/apparmor` read, which shows the host's kernel inside a container.
