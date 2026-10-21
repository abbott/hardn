# The main menu's header: the probe (`pkg/engine/header`) and its blocks (`pkg/output`)

> **Unit:** refactor plan Phase 4, `next/p4-header` (plan §2, row `p4-header`: "Cheap header probe (OS, version, codename, hostname, primary IP, uptime) plus the pending-reboot flag shown next to uptime"; row `p4-risk`'s pending-reboot sources, "kernel vs newest installed; `/var/run/reboot-required`; Alpine `/lib/modules/$(uname -r)` missing"; brief Phase 4, "Cross-checks", and brief §7.4, "Uptime is a finding, not decoration"; [P-04](../refactor/proposals.md#p-04) and [P-21](../refactor/proposals.md#p-21), both **open**; plan §6's salvage list: `FormatUptime` in Figma's format and the cheap header probe, ideas only). It comes before `next/p4-risk`, which reads its uptime and pending reboot (§7). No V-number covers origin/main's header (§1).
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only, with the kernel's files and `ip`'s output as synthetic fixtures (§3). Nothing has run on Linux: everything host-specific is "written, not run (gate 5)" (§4). **Every word below that Figma does not show is proposed**, for the maintainer's review (§5). Nothing is wired into `cmd/hardn` or the live main menu (plan, "Phases 3–5 together": the main menu goes live with slice 2).

Figma's main menu opens with three blocks: the wordmark, the version and the repository link (`Header`); the distribution's bar, "Debian 12 (Bookworm)" (`ContextBar`); and the host, its name in a badge, its address and its uptime, "182 days, 2 hours, 26 minutes" (`HostBlock`, whose pending-reboot line is P-21's). `header.Read` reads what they show, through `host.System`, as data: the distribution from detect's facts, the hostname, the primary address in each family, the uptime, and whether a reboot is pending, with the findings that say why. `output.HeaderViewOf` maps that to the three components; the engine renders nothing.

## 1. Why

origin/main's header (`formatHostInfoLines`, `pkg/menu/main_menu.go:350-374`) shows the hostname, `IPAddresses[0]` and the uptime. The address is the first IPv4 address, not loopback or link-local, of the first interface that is up, in `net.Interfaces`' order (`GetIPAddresses`, `pkg/adapter/secondary/os_host_info_repository.go:119-160`): an address of the host, not necessarily the one it is reached by (a second interface, a bridge, a VPN listed first), and "Unknown" on a host with IPv6 alone. The uptime is `/proc/uptime`, or else `uptime`'s output parsed by hand, formatted as "%d days, %d hours, %d minutes" (`FormatUptime`, `pkg/application/host_info_manager.go:60-72`): "1 days, 0 hours, 5 minutes". Nothing reads whether a reboot is pending, which brief §7.4 asks for: a long uptime with no automatic updates probably means an old kernel. (By code reading, not run.) The legacy header stays as it is until the main menu goes live.

## 2. Guarantees

1. **Read-only, through `host.System`.** `Read` reads the files of §3 and runs exactly one command, `ip -4 addr show dev <interface>`, and only when an IPv4 default route exists (`TestNoIPWithoutARoute`). Every test checks that each call is a read, a `stat`, a `readdir` or that command (`readOnly`). depguard rule `header-through-host` (the last of the rules) denies `os`, `io/ioutil`, `golang.org/x/sys`, `syscall`, `net` (but `net/netip`), `plugin`, `pkg/ui` and `pkg/output` to the package's non-test files; checked in a temporary copy of the tree with a throwaway file importing `os`, `net` and `pkg/ui/term` (each reported; `pkg/output` cannot be imported at all, as it imports this package). Nothing committed.
2. **Each fact is established, or not with its reason; nothing is guessed** (brief Phase 4). Each has an `Err`, nil when it is established (the pending reboot: its `Pending`, §2.7), that wraps the cause as it came, so its code in `pkg/output`'s registry is the cause's: `PERMISSION_DENIED`, `FILE_NOT_FOUND`, `COMMAND_NOT_FOUND`, `COMMAND_FAILED`, detect's `DETECT_UNRECOGNISED` (a form the probe does not know), `DETECT_NO_SIGNAL` (no source shows the fact either way), `DETECT_SIGNALS_DISAGREE` (two default routes the kernel chooses between), `CANCELLED` (§5.4). A done context leaves every fact not established and runs nothing (`TestCancelled`).
3. **The distribution** comes from detect's facts, which the caller has (as `Engine.Check` has them), so nothing is read again: on Proxmox VE (`Proxmox.Installed` Yes, whose os-release is Debian's) "Proxmox VE" with pve-manager's version; otherwise by os-release's `ID`, "Debian", "Ubuntu" or "Alpine" with `VERSION_ID`; the codename is `VERSION_CODENAME` as written. A distribution hardn does not name shows its `PRETTY_NAME`, else its `ID`, alone. Debian whose Proxmox VE is not established is "Debian": its os-release says so, and Proxmox VE runs on it. os-release not read is not established (`TestOS`).
4. **The hostname** is the kernel's, `/proc/sys/kernel/hostname` (what `uname -n` prints, [I]), the name the host runs with, not `/etc/hostname`, which is configuration (brief §3 principle 3). An empty one is not established.
5. **The primary address, decided:** in each family, the address of the interface the host's default route leaves by. That is the address a connection out through the default route comes from, so the one a host elsewhere sees and reaches it by; an address of another interface (a bridge, a VPN, a second network) is not. Read without running `ip` where `/proc` offers it:
   - **The IPv4 default route** is `/proc/net/route`'s (the main table, §3): destination and mask 0, up, not rejecting (unreachable, prohibit) and with an interface (a blackhole has none); the lowest metric. Two at that metric on different interfaces are not established (`DETECT_SIGNALS_DISAGREE`): the file does not show which the kernel uses (**[I]**: it chooses among default gateways of equal metric by their reachability). Two on one interface are that interface.
   - **The IPv4 address:** `/proc` has no IPv4 address by interface (`/proc/net/fib_trie` lists the local addresses with no interface), so this is the one fact read with a command, `ip -4 addr show dev <interface>`, in iproute2's or BusyBox's form. The address is the one the kernel takes as the source of a route through that gateway (`inet_select_addr`, §3): the first address of global scope that is not secondary and is in the gateway's network, else the first such address (an address `/32` with a gateway on the link); `tentative` and `dadfailed` ones are not usable. A route's preferred source (`ip route … src`), which `/proc/net/route` does not show, is not modelled (§7).
   - **The IPv6 default route** is `/proc/net/ipv6_route`'s, every table (§3): `::/0` with no source prefix, up, not rejecting, not on `lo` or no device; the lowest metric, two interfaces at it not established. The file absent, while `/proc/net/route` was read, is a kernel with IPv6 off: no IPv6 route.
   - **The IPv6 address** is `/proc/net/if_inet6`'s: of the route's interface, global scope, not tentative, not failed duplicate address detection, not deprecated and not temporary (privacy addresses change); a permanent (configured) address before an autoconfigured one; among several, the lowest. **A rule of this unit**: the kernel picks an IPv6 source per destination (RFC 6724), which no file shows, so this is "an address of the default route's interface, the configured one first", not the kernel's choice for a given peer.
   - **`Address.Primary`** is IPv4's address, else IPv6's (the host block shows one, as Figma does). Not established when neither is an address and one family is not established; with no address and both established, the host has no default route in either family, or none with an address (§5.2). Tested: `TestAddress`, one case per rule.
6. **The uptime** is `/proc/uptime`'s first number, the seconds since boot, suspend included (§3). Anything but two decimal numbers in the kernel's form (`350735.47 234388.90`) is not established, as is a value a `time.Duration` cannot hold. Inside a container it is the container's where LXCFS virtualises the file (LXC and Proxmox VE's containers, §3), the host's otherwise (**[I]**, Docker).
7. **Whether a reboot is pending** (`Reboot.Pending`, a `detect.Tri`), from the sources the platform has: on Debian, Ubuntu, Proxmox VE and their derivatives (by detect's platform, its `ID_LIKE`, or os-release's `ID` and `ID_LIKE`), `/var/run/reboot-required` and the running kernel against the newest in `/boot`; on Alpine, the running kernel's modules. **Yes** when one source shows it, with one finding per source that does (§5.3); **No** when every source that applies shows none; **Unknown** otherwise, with the sources' errors. A source that shows a reboot pending decides it even when another could not be read (`Err` is then set beside Yes). A platform the check knows no source for is Unknown (`DETECT_NO_SIGNAL`), as is one whose os-release was not read. Tested: `TestReboot`.
   - **`/var/run/reboot-required`** present is Yes. Absent says only that no package asked: Debian writes it less often than Ubuntu (**[unverified]**, §3), so its absence alone is never taken as No on a host with a kernel to compare (inside a container it is the one source, below).
   - **The kernel, decided: `/boot`, not the package database.** The running kernel's release is `/proc/sys/kernel/osrelease` (what `uname -r` prints); the installed ones are the `vmlinuz-<release>` files in `/boot`. One source for the three platforms with no command, whereas the packages are named differently on each (`linux-image-*` on Debian and Ubuntu, `proxmox-kernel-*` on Proxmox VE), and `/boot` holds what the bootloader boots. A release compares by its version, the part before its flavour (`6.12.43+deb13` of `6.12.43+deb13-amd64`, `6.17.0-14` of `6.17.0-14-generic`, `6.14.8-2` of `6.14.8-2-pve`), as dpkg compares versions (`verrevcmp`, §3), so two flavours of one version are equal; the newest of any flavour counts (the brief's "the newest installed one"). Newer than the running one is Yes; not newer is No. A `/boot` with no `vmlinuz-<release>` (not mounted, a kernel the hypervisor boots) is Unknown, never No (`TestReboot`, `TestCompareKernels`).
   - **Alpine:** `/lib/modules/<release>` present is No. Absent, with another kernel's modules there, is Yes: Alpine's kernel package replaces the modules when it upgrades the kernel (plan row `p4-risk`; **[unverified]**). No kernel's modules at all (a kernel with everything built in, a container) is Unknown. `/var/run/reboot-required` is not Alpine's and is not read.
   - **Inside a container** (detect's `Container.Detected` Yes) the kernel is the host's: neither kernel source is read, and the evidence says so; only a package's request (`reboot-required`) counts. A container check not established reads the kernel as a host's would.
   - **Evidence** lists what each source showed, one line each: `/var/run/reboot-required: present` (`absent`), `/proc/sys/kernel/osrelease: 6.12.43+deb13-amd64`, `/boot: newest vmlinuz-6.12.48+deb13-amd64`, `/lib/modules/6.12.40-0-lts: present` (`absent; /lib/modules: 6.12.45-0-lts`), `kernel: the host's (container)`.
8. **Three findings, no others:** `REBOOT_REQUIRED`, `REBOOT_NEWER_KERNEL`, `REBOOT_MODULES_MISSING`, exported constants registered in `pkg/output` (appended, `codes.golden` regenerated, a row each in [docs/reference/output.md](../reference/output.md) §2.2), each with its detail and source (§5.3; `TestFindings`). They explain a pending reboot; they set nothing. The probe is not a control and has no status row: the findings reach a document when `p4-risk` or the wiring puts them there.
9. **The blocks** (`output.HeaderViewOf`, pure): `Header` with hardn's version as given and `RepoURL`; `ContextBar` with the distribution's name, version and codename, capitalised as Figma shows it ("Trixie"); `HostBlock` with the hostname, the address words (§5.2), the uptime (`FormatUptime`) and `RebootPending` set only when the probe established a pending reboot. A fact not established is empty, which each component shows as "Unknown" (design-system README §9.11). `pkg/output` imports the probe; the probe imports neither `pkg/ui` nor `pkg/output` (§2.1). Tested: `TestHeaderViewOf`, `TestHeaderAddress`, and the golden `human/header` (`TestHeaderBlocks`, the core cells, the blocks a section gap apart as the main menu lays them out), since `pkg/output`'s human tests use goldens.
10. **The uptime's words** (`FormatUptime`): Figma's form, "182 days, 2 hours, 26 minutes", shortened only where it says the same (design-system README §9.15, "Informational text as short as it can be"): whole minutes; a unit that is 0 left out ("3 days, 5 minutes", not "3 days, 0 hours, 5 minutes"); "1 day", "1 hour", "1 minute" for one (origin/main printed "1 days"); "0 minutes" under a minute. Days stay days (no weeks or years: Figma counts days). The longest under 10,000 days, "9999 days, 23 hours, 59 minutes", is 31 cells, within the 37 beside the label at 64 columns, so it is never cut (`TestFormatUptime`).

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| `/proc/net/route` lists the main table only (`fib_get_table(…, RT_TABLE_MAIN)`): a header line, then per route `"%s\t%08X\t%08X\t%04X\t%d\t%u\t%u\t%08X\t%d\t%u\t%u"` (interface or `*`, destination, gateway, flags, 0, 0, metric, mask, …), broadcast and multicast routes left out; the flags are `RTF_REJECT` for unreachable and prohibit routes, `RTF_GATEWAY` with a gateway, `RTF_UP` always | linux `net/ipv4/fib_trie.c` (`fib_route_seq_show`, `fib_flag_trans`, `fib_route_seq_start`), master, read 2026-10-20 | §2.5 |
| The destination, gateway and mask are a `__be32` printed with `%08X`, so a little-endian host prints the address's bytes reversed (192.0.2.1 is `010200C0`); the probe takes the value's bytes in the host's own order (`binary.NativeEndian`) | follows from the row above; the test fixtures are a little-endian host's (amd64, arm64, the release targets) | §2.5 |
| `RTF_UP` 0x0001, `RTF_GATEWAY` 0x0002, `RTF_REJECT` 0x0200 | linux `include/uapi/linux/route.h`, read 2026-10-20 | §2.5 |
| `/proc/net/ipv6_route` walks every fib6 table; per route the destination and its length, the source and its length (zeros without `CONFIG_IPV6_SUBTREES`), the next hop, then `" %08x %08x %08x %08x %8s\n"` (metric, reference count, 0, flags, device or "") | linux `net/ipv6/ip6_fib.c` (`ipv6_route_native_seq_show`, `ipv6_route_seq_next_table`), read 2026-10-20 | §2.5 |
| `/proc/net/if_inet6` is `"%pi6 %02x %02x %02x %02x %8s\n"`: the address, the interface's index, the prefix length, the scope, the low byte of the flags, the interface | linux `net/ipv6/addrconf.c` (`if6_seq_show`), read 2026-10-20 | §2.5 |
| The scope is `ipv6_addr_scope`, the address type's scope bits (`IPV6_ADDR_SCOPE_MASK` 0x00f0: loopback 0x10, link-local 0x20, site-local 0x40, v4-compatible 0x80), so a global address is 00 | linux `include/net/ipv6.h`, read 2026-10-20; "global is 00" **[I]** from the mask | §2.5 |
| `IFA_F_TEMPORARY` 0x01, `IFA_F_DADFAILED` 0x08, `IFA_F_DEPRECATED` 0x20, `IFA_F_TENTATIVE` 0x40, `IFA_F_PERMANENT` 0x80 | linux `include/uapi/linux/if_addr.h`, read 2026-10-20 | §2.5 |
| A route's source, with no preferred source set, is `inet_select_addr(dev, gateway, scope)`: the first address that is not secondary, of the scope, in the gateway's network, else the first such address; a new primary address is inserted after the existing primaries, a secondary at the end | linux `net/ipv4/fib_semantics.c` (`fib_result_prefsrc`, `fib_info_update_nhc_saddr`), `net/ipv4/devinet.c` (`inet_select_addr`, `__inet_insert_ifa`), read 2026-10-20 | §2.5 |
| BusyBox's `ip` takes `-4` (`ip_parse_common_args`: `oneline`, `family`, `4`, `6`, `0`) and `dev <name>` for `addr show`, and prints `    inet <address>/<length> ` (or ` peer <address>/<length> `), `brd …`, `scope <scope> `, then `secondary`, `tentative`, `dadfailed`, `deprecated`, `dynamic` and the label | BusyBox `networking/libiproute/ip_parse_common_args.c`, `ipaddress.c` (`print_addrinfo`), GitHub mirror, read 2026-10-20 | §2.5 |
| iproute2's `ip -4 addr show dev <name>` prints the same line form, primaries before secondaries, and exits non-zero for a device that does not exist | **[unverified]** (BusyBox mimics it) | §2.5 |
| `/proc/uptime` holds two numbers of seconds, "the uptime of the system (including time spent in suspend)" and the idle time | proc_uptime(5), man7.org, read 2026-10-20 | §2.6 |
| `/proc/sys/kernel/hostname` sets and shows the hostname "in exactly the same way as" hostname(1); `osrelease` is a substring of `/proc/version` | proc_sys_kernel(5), man7.org, read 2026-10-20; that `osrelease` is what `uname -r` prints **[I]** | §2.4, §2.7 |
| LXCFS virtualises `/proc/uptime` (with `cpuinfo`, `meminfo`, `stat`, …) to the container's running time | LXCFS `README.md`, read 2026-10-20; that Proxmox VE's containers use it **[unverified]** | §2.6 |
| unattended-upgrades reboots "if the file /var/run/reboot-required is found after the upgrade" (`Automatic-Reboot`) | unattended-upgrades `README.md`, read 2026-10-20 | §2.7 |
| Ubuntu writes `/var/run/reboot-required` from its kernel's and some libraries' package scripts (update-notifier-common); Debian writes it less often | **[unverified]** | §2.7 |
| Debian, Ubuntu and Proxmox VE install each kernel as `/boot/vmlinuz-<release>`, `<release>` what `uname -r` prints once it boots; GRUB's default entry is the newest by version | **[unverified]** | §2.7 |
| dpkg's version comparison: non-digit runs by character, letters before other characters, `~` before everything, digits 0; digit runs as numbers, leading zeros skipped | dpkg `lib/dpkg/version.c` (`order`, `verrevcmp`), GitHub mirror, read 2026-10-20 | §2.7 |
| Alpine's kernel package replaces `/lib/modules/<release>` when it upgrades the kernel, so the running kernel's modules are gone until the reboot | refactor plan row `p4-risk`; **[unverified]** | §2.7 |
| Inside a container the kernel is the host's | brief 2.5 (decided, for AppArmor); [detect.md](../safety/detect.md) §6 | §2.7 |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root. On the VMs of [vmtest.md](../safety/vmtest.md) (Debian 13, Ubuntu 26.04, Alpine 3.24) and by hand on Proxmox VE 9, through a dev-only driver or the gallery's status scenario once one calls `header.Read`:

- each fact against the host's own tools: the hostname against `hostname`; the address against `ip -4 route get 192.0.2.1` and `ip -6 route show default` (with a second interface at a lower metric, a secondary address, an address in another network listed first, a host with IPv6 alone, and IPv6 off with `ipv6.disable=1`); the uptime against `uptime -p`; the running kernel against `uname -r`;
- `ip -4 addr show dev <interface>` as BusyBox prints it on Alpine 3.24, and as iproute2 prints it on the others (§3);
- the pending reboot: a kernel upgraded and not booted on Debian 13 and Ubuntu 26.04 (Yes, `REBOOT_NEWER_KERNEL`; on Ubuntu `REBOOT_REQUIRED` too), Proxmox VE 9 after a `proxmox-kernel` upgrade, Alpine 3.24 after `apk upgrade linux-lts` (Yes, `REBOOT_MODULES_MISSING`), then each after its reboot (No); a `/boot` on a partition left unmounted (Unknown);
- inside an LXC container on Proxmox VE (the container's uptime, through LXCFS) and a Docker container without iproute2 (the host's uptime; IPv4 `COMMAND_NOT_FOUND`);
- a big-endian host is not among the targets, and is not run.

## 5. The lines

Every line the three blocks can show, with the host's state that gives it, in Figma's grammar. Figma's own are marked; **everything else is proposed**.

### 5.1 The context bar

| The host | Bar (plain, ASCII) |
|---|---|
| Debian 12 | `# Debian 12 (Bookworm) ####…` (Figma's) |
| Debian 13 | `# Debian 13 (Trixie) ####…` |
| Debian testing or sid (no `VERSION_ID`) | `# Debian (Forky) ####…` |
| Ubuntu 26.04 | `# Ubuntu 26.04 (Resolute) ####…` |
| Proxmox VE 9 | `# Proxmox VE 9.0.10 (Trixie) ####…`: pve-manager's version, Debian's codename |
| Proxmox VE, pve-manager's version not read | `# Proxmox VE (Trixie) ####…` |
| Alpine 3.24 | `# Alpine 3.24.0 ####…` (os-release's `VERSION_ID`; no codename) |
| a distribution hardn does not name | its `PRETTY_NAME` (`# Linux Mint 22.1 ####…`), else its `ID` |
| os-release not read | `# Unknown ####…` |

### 5.2 The host block

| Line | The host | Exact string |
|---|---|---|
| badge | the kernel's hostname | as set (`web-01.example.net`, `web-01`) |
| badge | not read, or empty | `Unknown` |
| address | an IPv4 address of the default route's interface (§2.5) | `192.0.2.10` (Figma's form) |
| address | no IPv4 address there, or IPv4 not read, and an IPv6 address | `2001:db8:1::10` |
| address | neither family has a default route | `no default route` (*proposed*) |
| address | a default route, and no global address on its interface | `no address` (*proposed*) |
| address | no address, and a family not established | `Unknown` |
| uptime | read | Figma's form (§2.10): `182 days, 2 hours, 26 minutes` (Figma's), `41 days, 1 hour`, `3 days, 5 minutes`, `1 day`, `0 minutes` |
| uptime | not read, or in no form known | `Unknown` |
| reboot | a reboot pending (Yes) | `⛬ Reboot pending`, `[*] Reboot pending` in ASCII, under the uptime: P-21 (a) as `next/p1b-header` built it; its wording and place are P-21's, **open** |
| reboot | No, or Unknown | no line |

### 5.3 The findings (*proposed*)

| The host | Code | Detail | Source |
|---|---|---|---|
| `/var/run/reboot-required` exists | `REBOOT_REQUIRED` | updates need a reboot | `/var/run/reboot-required` |
| `/boot` holds a kernel newer than the running one | `REBOOT_NEWER_KERNEL` | newer kernel installed | `/boot/vmlinuz-6.12.48+deb13-amd64` |
| Alpine: the running kernel's modules gone, another's there | `REBOOT_MODULES_MISSING` | running kernel's modules gone | `/lib/modules/6.12.40-0-lts` |

### 5.4 Not established, by cause

| The host | Fact | Code |
|---|---|---|
| hardn not run as root, or a file refused | any | `PERMISSION_DENIED` |
| no `/proc` (not mounted), no `/proc/sys/kernel/hostname` or `/proc/uptime` | hostname, uptime, address | `FILE_NOT_FOUND` |
| `ip` not installed (a container without iproute2) | IPv4 address | `COMMAND_NOT_FOUND` |
| `ip` exits non-zero (the interface gone since the route was read) | IPv4 address | `COMMAND_FAILED` |
| a line of `/proc/net/route`, `ipv6_route`, `if_inet6` or `ip`'s output in no form known; an empty hostname; an uptime not two decimal numbers; an empty or odd kernel release; `/lib/modules/<release>` not a directory | that fact | `DETECT_UNRECOGNISED` |
| two default routes of one family at the lowest metric on different interfaces | that family | `DETECT_SIGNALS_DISAGREE` |
| no `vmlinuz-<release>` in `/boot` (or no `/boot`); no kernel's modules in `/lib/modules` (or none); a platform the check knows no source for; no detect facts | reboot; distribution | `DETECT_NO_SIGNAL` |
| os-release not read | distribution; reboot | detect's error's |
| the context done | every fact | `CANCELLED` |

## 6. API; changes to other units' code

```go
package header // pkg/engine/header (new)

const HostnameFile, UptimeFile = "/proc/sys/kernel/hostname", "/proc/uptime"
const RouteFile, Route6File, Inet6File = "/proc/net/route", "/proc/net/ipv6_route", "/proc/net/if_inet6"
const RebootRequiredFile, KernelReleaseFile, BootDir, ModulesDir = "/var/run/reboot-required", "/proc/sys/kernel/osrelease", "/boot", "/lib/modules"
const CodeRebootRequired, CodeNewerKernel, CodeModulesMissing = "REBOOT_REQUIRED", "REBOOT_NEWER_KERNEL", "REBOOT_MODULES_MISSING"
const DetailRebootRequired, DetailNewerKernel, DetailModulesMissing = "updates need a reboot", "newer kernel installed", "running kernel's modules gone"

type Header struct { OS OS; Hostname Hostname; Address Address; Uptime Uptime; Reboot Reboot }
type OS struct { Name, Version, Codename string; Err error }
type Hostname struct { Name string; Err error }
type Uptime struct { Duration time.Duration; Err error }
type Address struct { IPv4, IPv6 Family }
type Family struct { Iface string; Addr netip.Addr; Err error }
func (Address) Primary() (addr netip.Addr, known bool)
type Reboot struct { Pending detect.Tri; Running string; Findings []engine.Finding; Evidence []string; Err error }
func Read(ctx context.Context, sys host.System, f detect.Facts) Header

package output // added
const RepoURL = "https://github.com/abbott/hardn"
const NoDefaultRoute, NoAddress = "no default route", "no address"
type HeaderView struct { Header component.Header; Context component.ContextBar; Host component.HostBlock }
func HeaderViewOf(version string, h header.Header) HeaderView
func FormatUptime(d time.Duration) string
```

- **`pkg/output`**: `header.go` (the mapping), the three codes appended to `findingCodes` and to `TestFindingCodes`' list, `codes.golden` regenerated (three lines added), the golden `human/header` added. [output.md](output.md)'s API list predates `HeaderViewOf`; that page is `next/p3-output`'s.
- **`.golangci.yml`**: the rule `header-through-host`, at the end of the depguard rules.
- **[docs/reference/output.md](../reference/output.md)** §2.2: a row per code, at the end of the table.
- Nothing in `pkg/host`, `detect` or the controls changed; `go.mod` and `go.sum` are unchanged.

## 7. Inputs to `next/p4-risk`; not modelled; for the maintainer; size

- **Inputs to `next/p4-risk`**, which owns "pending reboot" and "long uptime without auto updates" (plan row `p4-risk`; brief Phase 4): `Reboot.Pending` (Yes is a factor, with `Reboot.Findings` saying why; Unknown counts as not established, by the plan's rule for `Unknown`, and `Reboot.Err` says why), and `Uptime.Duration` when `Uptime.Err` is nil, beside the Auto Updates row's state ([check-updates.md](check-updates.md) §6 says which of its lines count as updating). Inside a container (detect's `Container.Detected` Yes) the kernel is the host's: no kernel source is read, so Pending is the packages' request alone, and the uptime may be the host's (§2.6); whether a long uptime there is a factor is `p4-risk`'s to decide. How long is "long" is `p4-risk`'s too.
- **Not modelled:** a default route outside the main table (policy routing) for IPv4, which `/proc/net/route` does not list (a host routed that way shows "no default route" in IPv4, the pessimistic error); a route's preferred source (`ip route … src`), so the address shown is the interface's, not the route's; the kernel's per-destination IPv6 source (§2.5); a kernel pinned for the next boot (`proxmox-boot-tool kernel pin`, `GRUB_DEFAULT`), so "newer kernel installed" means installed, not what boots next; **a kernel updated in place under the same release** (Debian 12's `6.1.0-<abi>` releases may be kept across uploads, **[unverified]**): the file in `/boot` is replaced and its name does not change, so the comparison shows No while the running kernel is older, the false-safe direction, which `/var/run/reboot-required` covers only where a package writes it (see below); libraries and services that need a restart (needrestart's domain); `/boot` on a bootloader or architecture that names its kernel otherwise than `vmlinuz-<release>` (Raspberry Pi's `kernel8.img`; `vmlinux` on some architectures), which is Unknown.
- **For the maintainer** (all wording here is proposed):
  - (a) the bar's words: "Proxmox VE" with pve-manager's version and Debian's codename ("Proxmox VE 9.0.10 (Trixie)"), against "Proxmox VE 9" or Debian's own line; Alpine's full `VERSION_ID` ("3.24.0") against the branch ("3.24"); "Alpine" against os-release's `NAME`, "Alpine Linux";
  - (b) the address when there is none: "no default route" and "no address" in the address's place, against "Unknown", which says hardn could not read it, which is not so;
  - (c) an IPv6 address over 37 cells (four-digit groups throughout, up to 39) is cut with the ellipsis beside the label at 64 columns (`HostBlock` cuts each value to the content width); a cut address is not the address. Whether the block stacks a long address, or shows IPv4 alone, is a component change;
  - (d) the pending-reboot line's wording and place (P-21, open; P-04, open). P-21 says nothing sets the flag before P-04 is approved: `HeaderViewOf` sets it, but only the tests and the golden call it, and the wiring into the live main menu should wait for P-04. A pending reboot not established shows no line, as nothing pending does; whether it should say so is P-21's to settle;
  - (e) the findings' details (§5.3);
  - (f) the kernel comparison by release name (§2.7) leaves Debian 12's same-release update unseen (Not modelled, above): closing it takes the package database (the running kernel's build string in `/proc/version`, "Debian 6.1.140-1", against `dpkg-query` of `linux-image-$(uname -r)`), a command and a Debian-specific parser this unit did not add;
  - (g) the uptime's shortening (§2.10): units at 0 dropped and the singular, against Figma's three units always.
- **Size:** about 930 lines of non-test Go (the probe about 835, of them about 165 whole-line comments; the mapping about 100) and about 900 of tests, against the plan's guideline of about 500 changed lines: three readers (the default route and the primary address in two families, one of them through `ip`; the uptime; the pending reboot with dpkg's version comparison), each fact's every "not established" path, and a test per rule of §2 and per cause of §5.4.
