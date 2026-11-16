# Warnings on custom distributions: design

Status: **adopted 2026-11-16** (§4), **built 2026-11-16** (below). The maintainer's answers: decision 1 (a), refuse on TrueNAS; decision 2 (b), the review and the docs only, no main-menu warning on Proxmox VE or Raspberry Pi OS; decision 3 (a), the general warning and four lines of their own. Decisions 4 to 6 as recommended: 4 (a), 5 (a), 6's words without the two main-menu lines decision 2 drops. TrueNAS keeps its main-menu line under 1 (a), as an out-of-scope release has one, so the refusal is said before a change is chosen. A run on a TrueNAS host and on a Raspberry Pi OS host before release (the maintainer has both).

## Built 2026-11-16

Built as adopted on `next/custom-distros`; not run on a TrueNAS host or a Pi. Where this page was silent:

- **Detection** (`pkg/host/detect/custom.go`, `Custom`, `DetectCustom`): `Platform.Variant` (`truenas`, `raspios`), `Distro` staying `debian`; read only on a Debian `ID` whose Proxmox VE is No. Each signal in order, the first Yes deciding: TrueNAS by `freenas-debug`, `disable-rootfs-protection`, a kernel release ending `+truenas` outside a container only (a container shares the host's kernel), no package tool asked; then Raspberry Pi OS by `/etc/rpi-issue`, an uncommented line naming `archive.raspberrypi.com` in `raspi.list` or `raspi.sources`, dpkg's `raspberrypi-sys-mods`. `Classify` takes the facts as a third argument.
- **Unknown**: TrueNAS not established on Debian is a platform not established, refused as Proxmox VE's is. Raspberry Pi OS not established is plain Debian, its reason in `Custom.Err`: it only adds warnings, so refusing a Debian host for it would be worse.
- **TrueNAS**: `engine.ErrTrueNAS`, `PLATFORM_TRUENAS`, refused in `refuseApply` before the support level; an empty plan is not refused, as on any platform. Its main-menu notice takes the place of the end-of-support one on every day: Debian's upgrade is TrueNAS's to make. Run All refuses each row it would plan, in the same words.
- **`PLATFORM_CUSTOM`**: also on Proxmox VE 8, whose plans stay refused as out of scope.
- **Auto Updates on Raspberry Pi OS**: `UPDATES_RPI_NOT_UPGRADED` where upgrades run, its origins not compared; the row's line is "Pi packages not upgraded", as Proxmox VE's is short, and the review's, on turning updates on, is the full sentence.
- **SSH's line**: an Info notice above the SSH screen's changes, as the services' notices are; not on the SSH Keys screen or Run All.
- **NTP's lines**: on Set servers alone, not on Remove.
- **JSON**: `platform.variant`, `""` for none.
- **Gallery**: `eol truenas` (the main menu) and `timesync review-raspios`.

## 1. What is asked for

The maintainer, 2026-11-16: in the docs and in the menu's actions, warn on TrueNAS, Proxmox VE and Raspberry Pi OS that a change that is trivial on plain Debian or Alpine can break a custom distribution; for advanced users who know what they're doing.

## 2. What hardn does on each today

| | Detected as | Changes | Warning today |
|---|---|---|---|
| Proxmox VE 9 | Proxmox VE (pve-manager or `pveversion`) | Allowed, with many guards: root SSH stays key-only, port 22 kept, ufw refused (pve-firewall read only), Auto Updates not offered, two sysctl keys left alone | Per row and per review only (DNS: "Saving Proxmox VE's DNS settings rewrites /etc/resolv.conf"; NTP: "On a cluster, set every node the same way.") |
| TrueNAS (SCALE / Community Edition) | **Debian 12, Supported**: os-release is Debian's | Allowed | None |
| Raspberry Pi OS, 64-bit | **Debian 12 or 13, Supported**: os-release is Debian's | Allowed | None |
| Raspberry Pi OS, 32-bit | `ID=raspbian`, a distribution hardn doesn't list | **hardn doesn't start**; install.sh has no armv7 build | — |

Sources: `pkg/host/detect/platform.go` (`Classify`), `cmd/hardn/hostos.go`, `pkg/host/pve`, `docs/platforms.md` §Proxmox.

## 3. What the research found

**TrueNAS.** Its middleware writes the system's files from its own database, and writes them again on boot, on a service restart and on update ([`etc.py`](https://raw.githubusercontent.com/truenas/middleware/master/src/middlewared/middlewared/plugins/etc.py)): `/etc/passwd`, `/etc/shadow`, `/etc/sudoers`, `/etc/ssh/sshd_config`, `/etc/chrony/chrony.conf`, syslog-ng's files, the sysctl tunables. `apt` and `dpkg` have their execute bits taken away, and `/usr` is read only. TrueNAS: "Users that enable this functionality will not receive support", "These changes do not persist across updates" ([Developer Mode](https://www.truenas.com/docs/scale/25.10/scaletutorials/systemsettings/advanced/developermode/)); updating "with `apt` or methods other than the SCALE web interface can result in a nonfunctional system" (24.04 notes). So on TrueNAS each of hardn's changes is overwritten later (users, SSH, NTP, sysctl), can't run (ufw, unattended-upgrades, rsyslog need apt), or is against the vendor's support terms. Signals: `/usr/local/bin/freenas-debug`, `/usr/local/libexec/disable-rootfs-protection`, `/etc/version`, a kernel release ending `+truenas`. **Not run on a TrueNAS host.**

**Raspberry Pi OS.** The 64-bit image keeps Debian's os-release. Signals: `/etc/rpi-issue` (written by pi-gen and rpi-image-gen), the package `raspberrypi-sys-mods`, the APT source `archive.raspberrypi.com`. Hazards:
- **Auto Updates misses the Pi's own packages.** Debian's unattended-upgrades origins don't match the Pi archive (`Origin: Raspberry Pi Foundation`), so the kernel, firmware and `rpi-eeprom` are never upgraded, while hardn's row says Configured. Raspberry Pi recommends `full-upgrade`.
- **A headless lockout means pulling the SD card**: often password SSH alone, no console.
- **No clock without NTP**: only the Pi 5 has an RTC, and it needs a battery.
- DNS (NetworkManager, which hardn already refuses to write over), sysctl and ufw: low risk.
**Not run on a Pi.**

**Proxmox VE.** hardn already guards what Proxmox's docs name (root SSH for the cluster, port 22, pve-firewall, `apt full-upgrade`). Not guarded: an up-front disclaimer; NTP servers the nodes can't reach (the cluster needs synced clocks); a Linux user hardn creates is not a web-UI user until added in Proxmox VE.

## 4. Decisions for the maintainer

1. **TrueNAS: warn or refuse.** (a) *Recommended:* refuse every change, as hardn refuses an out-of-scope release, and show the screens: TrueNAS writes these files back, and its vendor doesn't support changes made outside its web UI. The main menu says "TrueNAS manages its own system: make changes in its web UI." (b) Allow, with the warning of decision 2 and a typed hostname on every change: most changes are lost on the next boot or update, and the ones needing apt fail.
2. **Where the warning shows** (Proxmox VE and Raspberry Pi OS; TrueNAS too under 1 (b)). (a) *Recommended:* all three: a Warning on the main menu at every open, under the header as the end-of-support notice is; a finding on every change's review, above the changes, as `PLATFORM_BEST_EFFORT` is, and in the JSON (`PLATFORM_CUSTOM`); and the docs. (b) The review and the docs only. (c) (a), and a typed hostname on every change there.
3. **Which actions.** (a) *Recommended:* every change carries the general warning (decision 2), and three get a line of their own where the research found a concrete risk: Raspberry Pi OS's Auto Updates (the Pi's own packages not upgraded), SSH (a lockout means pulling the SD card) and NTP (no clock without it); Proxmox VE's NTP (cluster nodes need synced clocks). (b) The general warning alone.
4. **Raspberry Pi OS's Auto Updates gap.** (a) *Recommended:* a finding on the row and in the review, "the Pi's own packages (kernel, firmware) aren't upgraded automatically: use apt full-upgrade", as Proxmox VE's `UPDATES_PROXMOX_NOT_UPGRADED`; nothing else changes. (b) Add the Pi archive's origin to hardn's apt file, so they are: Raspberry Pi recommends `full-upgrade` for them, which unattended-upgrades doesn't do.
5. **Raspberry Pi OS 32-bit.** (a) *Recommended:* stays out (hardn doesn't start, no armv7 build); the docs say so. (b) Support it: an armv7 build and a support row.
6. **The words** (proposed):
   - Main menu, Proxmox VE and Raspberry Pi OS: title "Custom distribution: Proxmox VE" (or "Raspberry Pi OS"); "A change that is safe on Debian can break it. For advanced users."
   - Review finding: "Proxmox VE is a custom distribution: a change that is safe on Debian can break it."
   - TrueNAS under 1 (a): title "TrueNAS manages its own system"; "hardn refuses every change. Make them in TrueNAS's web UI."

## 5. Checks when built

- Detection from fixtures: each signal alone, none, a Debian host with none.
- The warning on the main menu, every review and the JSON on each platform; none on plain Debian.
- TrueNAS: every change refused (or warned, by decision 1), the screens shown.
- A run on a real host of each where one is available; until then, detection is from the vendor's sources alone.
