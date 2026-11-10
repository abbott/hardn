# Rocky Linux and AlmaLinux, read only (`next/el-readonly`)

> **Unit:** the first of the Rocky/AlmaLinux units (the maintainer, 2026-11-09: "let's explore rocky/alma", then "Yes, start unit 1"). Unit 1 of six: read only; then changes that move no port (2); SSH port changes through SELinux and firewalld (3a, which took unit 4); dnf and firewalld through the Firewall item (3b); dnf-automatic (5); CI and docs (6). All are built.
> **Status:** built and tested on macOS against the recording fake, and on `rocky10-test-110` (Rocky Linux 10.2, x86_64) on 2026-11-09: `go vet` and `go test ./...` on Linux (71 packages, all pass), then `p4status` (§4). The platform facts it relies on were read there by a read-only probe (§3). hardn itself didn't run there until unit 2 ([el-changes.md](el-changes.md) §4).

hardn now starts on Rocky Linux and AlmaLinux, reads them, and refuses every change. **Since unit 2** ([el-changes.md](el-changes.md)) 10 and 9 are Supported and the changes that move no port are made; §1.1's "no matrix row" and §1.3's refusal no longer hold. Before this unit it refused to start on them (`unsupported OS type detected: rocky`).

## 1. What it does

1. **Two distributions, no matrix row.** `detect.DistroRocky` (`rocky`) and `detect.DistroAlma` (`almalinux`) are told apart by os-release's `ID`. `Distro.EL` is true for both and nothing else. `Platform.Version` is `VERSION_ID`'s major (`10.2` gives `10`); a `VERSION_ID` that is not two numbers gives `""`. The matrix has no row for either, so both are `Unlisted`: the support gate refuses every plan with changes (`engine.ErrUnsupported`, [engine.md](engine.md) §2.6). RHEL itself (`rhel`), CentOS Stream and the other rebuilds stay `DistroOther`, so hardn still won't start on them.
2. **sshd.service.** Where systemd knows neither `ssh.socket` nor `ssh.service` (both `not-found` and inactive), `DetectSSH` asks about `sshd.socket` and `sshd.service` (`SSH.SSHDUnits`; `SSH.SocketUnit()`, `ServiceUnit()`). An enabled or active `sshd.service` is `SSHService`. An enabled or active `sshd.socket` is `ErrUnrecognised`: it starts one sshd per connection (`Accept=yes`, `sshd@.service`), unlike the Debian family's `ssh.socket`, which hardn reads. A host with either `ssh.*` unit found, or running though its file is gone (a Debian sshd removed but not restarted), is decided as before and never asked about `sshd.*`.
3. **No reload yet.** `reload.PlanFor` refuses `SSHDUnits` (`ErrModeUnknown`, "hardn does not reload sshd.service yet"). The support gate already refuses first. This refusal is there in case a later row lifts the gate before a reload has run on a VM.
4. **The status rows.**
   - AppArmor: with the kernel's AppArmor off, Not Applicable, "uses SELinux" (proposed words) while SELinux enforces (`/sys/fs/selinux/enforce` is `1`); "off in kernel" (Not Configured, in the risk level) when it is permissive (`0`) or disabled (no selinuxfs); Unknown otherwise.
   - Auto Updates: Unknown, "can't read" (`DETECT_UNRECOGNISED`, "dnf-automatic is not read yet"). No apt read runs; the screen says Can't read; on and off are refused. (Since unit 5 it reads dnf-automatic: [el-updates.md](el-updates.md).)
   - DNS: `HostOf` accepts both. NetworkManager's `resolv.conf` is then reported, not written ("NetworkManager", Not Applicable).
   - SSH, Users and Firewall read as on any systemd host, except that an `/etc/sysconfig/sshd` whose `OPTIONS` is not empty makes the SSH rows and the Firewall row's SSH ports Unknown (`RELOAD_SYSCONFIG_OPTIONS`, `reload.CheckSysconfig`). Those options reach sshd's command line, which `sshd -T` doesn't see, as Alpine's `/etc/conf.d/sshd` is refused (`RELOAD_OPENRC_CONFD`). Firewall reads ufw alone, so with ufw absent it says ufw isn't installed (since unit 3a it reads firewalld there: [el-ports.md](el-ports.md) §1.6). A host with no SSH server is Unknown, not "no SSH server": `sshd.NoServer` reads dpkg's and apk's databases only.
5. **The header.** The distribution's bar reads "Rocky Linux 10.2" and "AlmaLinux 10.1". The pending reboot is `needs-restarting -r` (dnf-plugins-core): exit 1 with "Reboot is required" is `REBOOT_REQUIRED` with that source, and exit 0 is none. The plugin sets exit 1 by raising dnf's own error, as any dnf failure does, so exit 1 without that line is Unknown (`DETECT_UNRECOGNISED`). Any other exit is Unknown, and the command missing is Unknown (`DETECT_NO_SIGNAL`). `/var/run/reboot-required` and `/boot` are not read there.
6. **The words.** The JSON's `platform.distro` adds `rocky` and `almalinux`. `ssh` stays `ssh.service` for `sshd.service` ([output.md](../reference/output.md)). The engine's names for them are "Rocky Linux" and "AlmaLinux". `p4status` allows `sshd.socket`'s and `sshd.service`'s `is-enabled` and `is-active`, and `needs-restarting -r`.
7. **The crypto policy's Include.** Rocky Linux's `40-redhat-crypto-policies.conf` holds `Include /etc/crypto-policies/back-ends/opensshserver.config`, which `sshd.Validate` (and so `ReadEffective`, and every SSH row) refused as any Include in a drop-in until now. That one line is now followed, to a regular file that sets only crypto settings ([sshd-validate.md](../safety/sshd-validate.md) §2.4), on every distribution; it exists only on the RHEL family. The scope is the maintainer's choice of 2026-11-09 ("Fix Include first", that path alone).

## 2. API

```go
const DistroRocky, DistroAlma detect.Distro = "rocky", "almalinux"
func (d Distro) EL() bool
const SSHSocketUnit, SSHServiceUnit, SSHDSocketUnit, SSHDServiceUnit = "ssh.socket", "ssh.service", "sshd.socket", "sshd.service"
type SSH struct{ /* … */ SSHDUnits bool }
func (s SSH) SocketUnit() string
func (s SSH) ServiceUnit() string
var header.NeedsRestarting = []string{"needs-restarting", "-r"}
const apparmor.DetailSELinux, apparmor.SELinuxEnforce = "uses SELinux", "/sys/fs/selinux/enforce"
const reload.SysconfigSSHD = "/etc/sysconfig/sshd"
var reload.ErrSysconfig error // RELOAD_SYSCONFIG_OPTIONS
func reload.CheckSysconfig(ctx context.Context, sys host.System) error
const sshd.CryptoPolicyFile = "/etc/crypto-policies/back-ends/opensshserver.config"
const sshd.CryptoPolicyInclude = "Include " + sshd.CryptoPolicyFile
```

## 3. Platform facts, as rocky10-test-110 showed them

The probe and its output are in the side-session notes, under `probe/` (outside the repository).

| Fact | Used by |
|---|---|
| `ID="rocky"`, `ID_LIKE="rhel centos fedora"`, `VERSION_ID="10.2"` (fixture `rocky-10.2`) | §1.1 |
| `ssh.socket` and `ssh.service`: `not-found`. `sshd.service` enabled and active; `sshd.socket` disabled, `Accept=yes`, `Conflicts=sshd.service` | §1.2 |
| `Include /etc/ssh/sshd_config.d/*.conf` is the first directive; the vendor's drop-ins are `40-redhat-crypto-policies.conf` and `50-redhat.conf` (and cloud-init's `50-cloud-init.conf`) | the SSH rows, unchanged |
| No AppArmor module; SELinux enforcing (`/sys/fs/selinux/enforce`: `1`); `ssh_port_t` tcp 22 only | §1.4 |
| `/etc/sysconfig/sshd`: `OPTIONS=""` | §1.4 |
| `resolv.conf` a plain file, "# Generated by NetworkManager"; no systemd-resolved; `dhcpcd` installed | §1.4 |
| dnf 4.20.0; dnf-automatic not installed | §1.4 |
| `needs-restarting -r` installed, exit 0, "Reboot should not be necessary"; its source (`needs_restarting.py`) prints "Reboot is required to fully utilize these updates." and raises dnf's error for exit 1 | §1.5 |
| firewalld and ufw both not installed | the Firewall row |

AlmaLinux is not read from a host: its `ID` and `VERSION_ID` form are the vendor's (`almalinux`, `10.1`), and nothing else is assumed to differ from Rocky Linux's.

## 4. What the VM run showed

`p4status` on `rocky10-test-110` (read only by construction, [vmcheck.md](vmcheck.md)): 15 commands run, none refused. The header, Firewall ("ufw not installed"), DNS ("NetworkManager"), Auto Updates (Unknown) and AppArmor ("uses SELinux", `/sys/fs/selinux/enforce` `"1"`) read as §1 says. The pending reboot was No (`needs-restarting -r`: exit 0).

**The first run read SSH Login, SSH Auth, SSH Port and User as Unknown**, `SSHD_INCLUDE_UNSUPPORTED`: the vendor's `40-redhat-crypto-policies.conf` includes the crypto policy, and `sshd.Validate` refused any Include in a drop-in. §1.7 follows that one Include now. The policy file there, under DEFAULT, FIPS, FUTURE and LEGACY alike, sets only `Ciphers`, `MACs`, `GSSAPIKexAlgorithms` (or `GSSAPIKeyExchange`), `KexAlgorithms`, `HostKeyAlgorithms`, `PubkeyAcceptedAlgorithms`, `HostbasedAcceptedAlgorithms`, `CASignatureAlgorithms` and `RequiredRSASize`.

**The run after §1.7** (22 commands, none refused; `go test ./...` on Linux passing again):

| Row | Reads as | From `sshd -T` |
|---|---|---|
| SSH Login | Partial, "root key-only" | `permitrootlogin without-password` |
| SSH Auth | Configured, "key-only auth" | |
| SSH Port | Not Configured, "default (22)" | |
| User | Configured, "non-root, sudo" | |

The User row's `USERS_NOPASSWD` names cloud-init's `90-cloud-init-users`. The probe's facts agree with each row.

Not run: hardn itself (`hardn -n`, the main menu), and AlmaLinux.

## 5. What the next units must do

- **Changes that move no port** (unit 2): matrix rows for Rocky Linux and AlmaLinux 10 and 9 (`Supported`, by the rule of `platform.go`'s matrix); the reload of `sshd.service` (`systemctl reload sshd.service`, the same `kill -HUP` as Debian's) with the refusal of §1.3 lifted, run on the VM; `reload.CheckSysconfig` in the plan's reads too (`pkg/engine/steps`). Debian's `/etc/default/ssh` `SSHD_OPTS` is the same gap, unread today: a fix for both families is the maintainer's call. Refuse port changes until units 3 and 4. Check the labels on the files hardn writes, and on restored backups, on the VM. Built: [el-changes.md](el-changes.md).
- **firewalld** (unit 3) and **the SELinux port label** (unit 4): see the exploration brief. On a host with neither firewalld nor ufw, the Firewall item needs the maintainer's choice: offer firewalld, or report only. Built as one unit: [el-ports.md](el-ports.md) (3a) and [el-firewalld.md](el-firewalld.md) (3b), which offers firewalld.
- **dnf-automatic** (unit 5): its reader replaces §1.4's Unknown. Built: [el-updates.md](el-updates.md).
- **CI images and docs** (unit 6): done ([el-changes.md](el-changes.md) §5).

## 6. Decisions of this unit

- Detect `sshd.*` by the absence of both `ssh.*` units, not by the distribution: `DetectSSH`'s callers have no platform (the rollback's among them), and the Debian family is unchanged.
- No matrix row: Unlisted until unit 2. The README said "read only" until then.
- AlmaLinux with Rocky Linux, from os-release's documented form; no VM.
- The crypto policy's Include: that one path, followed to a file of crypto settings only (the maintainer, 2026-11-09), not any absolute Include. A Debian drop-in holding any other Include is refused as before.
