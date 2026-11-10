# Rocky Linux and AlmaLinux: firewalld through the Firewall item (`next/el-readonly`, unit 3b)

> **Unit:** the third Rocky/AlmaLinux unit, second half ([el-ports.md](el-ports.md) §5). The maintainer, 2026-11-09 and 2026-11-10:
> - "Offer firewalld" where there is no firewall;
> - a transaction that brings a new kernel is refused ("Refuse");
> - Run All leaves the firewall to the Firewall screen there ("Leave to the screen").
>
> **Status:** built and tested against the recording fake. The design was revised after a three-lens critique and a read-only probe of rocky10-test-110 (§6). The build was reviewed from four lenses with a skeptic per lens, and the 27 confirmed findings fixed and checked again. Run on rocky10-test-110 with the maintainer's OK ("Yes, with reboot"), 2026-11-10 (§7).

## 1. What it does

### 1.1 dnf, a package tool

`packages.Dnf` is the tool for `Distro.EL`. Commands run with `LC_ALL=C` (`host.commandEnv`).

- **Read:** `rpm -q <name>`.
  - Exit 0, every line `<name>-…`: installed.
  - Exit 1, stdout `package <name> is not installed`: not installed.
  - Anything else: Unknown.
- Every dnf command carries `--setopt=exit_on_lock=True`: dnf otherwise waits for another dnf's lock (`exit_on_lock` defaults to False), and the install couldn't be stopped while it waited.
- **Simulate:** `dnf install --assumeno --setopt=install_weak_deps=False <name>`. It prints the table on stdout, then `Operation aborted.` on stderr, and exits 1.
  - Only `Installing:`, `Installing dependencies:` and `Installing weak dependencies:` are accepted. Any other section, or a `replacing` line, is `PACKAGE_OTHER_CHANGES`.
  - A kernel package of another release than the running one (`uname -r`; the row's version and arch) is `PACKAGE_KERNEL`, refused. Kernel packages are `kernel` and the `kernel-…` names but headers, tools and the files modules are built with (`packages.notKernels`).
  - `Error: Unable to find a match: <name>` is `PACKAGE_NOT_IN_REPOS`.
  - Exit 200 (another dnf holds the lock, with `exit_on_lock`) is `PACKAGE_LOCKED`.
  - A refused section's rows are named "<heading's first word> <package>" ("Upgrading sudo", "Replacing x").
- **Install:** `dnf install -y --setopt=install_weak_deps=False <name>`, checked with `rpm -q`.
  - It runs to its end (`context.WithoutCancel`, a 30-minute bound), for every tool: a killed rpm transaction leaves the database half done.
- **Undo:** `dnf remove -y --noautoremove <name>`. It leaves the dependencies, as apt's undo does.
- **Why no weak dependencies:** two of firewalld's (`ipset`, `iptables-nft`) require `kernel-modules-extra`. The repository has it only for recent kernels (211.68 on; the host ran 211.16), so dnf brings a new kernel: 14 packages, 96 MB. Without them it is 6 packages, 1.5 MB, and no kernel. firewalld's nftables backend doesn't use them; this is to establish on the VM (§4).

### 1.2 Install firewalld

Item 1 where firewalld isn't installed.

- **Refused** (`FIREWALLD_OTHER_FIREWALL`) where ufw is installed, or another firewall unit is active or enabled: `iptables`, `ip6tables`, `ebtables` and `ipset` (firewalld's `Conflicts=`), and `nftables`. An `is-enabled` that prints nothing, but for systemd 252's answer for a missing unit, refuses as unread (`FIREWALLD_UNKNOWN`).
- **Refused** (`FIREWALLD_BOOT_BLOCKED`) where `firewalld.service` is masked already: hardn leaves a mask it didn't make. A mask with no unit file (`firewalld.UnitFile`) reads as firewalld not installed, as does `not-found`, which systemd 252 (EL9) prints as nothing, exit 1, with `Failed to get unit file state` on stderr (`detect.MissingUnit`).
- **The preset is kept from enabling it.** Rocky's `90-default.preset` enables `firewalld.service`, so the package would start at the next boot. The change runs:
  1. `systemctl mask firewalld.service` (a link to /dev/null, which the package's preset can't enable);
  2. the install;
  3. `systemctl unmask firewalld.service`;
  4. a check that `systemctl is-enabled` says `disabled`.

  A crash in between leaves it masked, which an Enable later refuses, rather than enabled.
- **Undo:** `systemctl unmask firewalld.service`, then dnf's remove.
- Package install risk, `StagePackages`, as Install UFW. On Rocky Linux and AlmaLinux, Install iproute2 installs `iproute`, which provides `ss` there.

### 1.3 Enable firewalld

Item 1 where firewalld is installed and stopped: one lockout-risk change of `StageFirewall`, under confirm-or-revert.

- **The SSH ports** are sshd's configured listeners' ports and the applying session's server port.
  - Refused as ufw's Enable is: `ss` missing or failing; listeners unbound; a remote session's port unread; no SSH server.
- **Refused before anything changes:**
  - `FIREWALLD_OTHER_FIREWALL`, as in §1.2: firewalld's start would stop those units, and its undo wouldn't start them again.
  - `FIREWALLD_NO_CLEANUP`: `CleanupOnExit` is `no` or `false` in `/etc/firewalld/firewalld.conf`. A stop then leaves the rules loaded, and the undo would not undo.
  - `FIREWALLD_BOOT_BLOCKED`: `systemctl is-enabled` says `masked` or `masked-runtime`.
  - `FIREWALLD_BOOT_UNKNOWN`: any word other than `enabled` or `disabled`.
  - `FIREWALLD_ZONE_NO_SSH`: a zone that will hold an interface or a source doesn't let an SSH port through. Those zones are the ones the permanent configuration binds, and the zone of each active NetworkManager connection (`nmcli`). The default zone is hardn's to change, and these are the operator's.
- **The zones are read offline** (`firewall-offline-cmd --get-default-zone`, `--list-all-zones`, `--service=<s> --get-ports`). `firewall-cmd` waits 10 s for D-Bus while firewalld is down.
- **Apply:**
  1. In the default zone, each SSH port it doesn't let through by its target, a port, or a service's ports is added: `firewall-offline-cmd --zone=<d> --add-port=<p>/tcp`.
  2. `systemctl start firewalld.service`. The unit has no `Type=`, so the start returns before firewalld is ready.
  3. hardn waits, for 60 s at most, until `systemctl is-active` says `active` and `firewall-cmd --state` says `running`. Else `FIREWALLD_NOT_READY`.
  4. Every zone checked at plan time, and every zone active now, must let each SSH port through at runtime (`Allows`). Else `FIREWALLD_NOT_LET_THROUGH`.
  5. `systemctl enable firewalld.service` where it said `disabled`.

  An error rolls back. A new connection confirms: it checks rich rules and NetworkManager's later binds, which hardn doesn't read.
- **Undo** (`firewalld-enable`, a checked plan journalled before), in this order:
  1. `systemctl stop firewalld.service`, which the change started, then a check that `is-active` says `inactive` (`failed` may have left its rules loaded).
  2. `systemctl disable firewalld.service`, where the change enabled it.
  3. Only once both held, the ports it added. Where its add created `/etc/firewalld/zones/<d>.xml`, that file is removed (with the `.old` firewalld's writer leaves), and the ports go with it. Otherwise each port is queried and taken away through `firewall-offline-cmd`.
  - The ports go last, so an undo that stops part way leaves at worst a disabled firewalld with the SSH ports let through, never an enabled one that shuts sshd's port at the next boot. Where the Enable found firewalld enabled (stopped and starting at boot), the ports stay for the same reason: the undo ends there, as the journalled plan records, rather than shut SSH at the next boot.
  - A unit left `failed` (by the Enable's own start, or a kill at the stop) is cleared with `systemctl reset-failed` before the check: whatever it left loaded came after the Enable's adds, so it lets the SSH ports through.
  - It returns every error, never starts a unit, and never runs `firewall-cmd`. It works the same from the timer and from the boot unit, which isn't ordered against firewalld on Rocky Linux (its `Before=sshd.socket` names a disabled unit).
- **Running and not started at boot** is not enabled. A confirmation tests what runs now, not what the saved configuration starts at the next boot. Item 1 is Disable there. Disable, then Enable, proves the saved configuration with a start under confirm-or-revert.

### 1.4 Disable firewalld

Item 1 where it runs; beside Enable where it is stopped and starts at boot.

- Refused where it would stop a running firewalld with `CleanupOnExit` off (§1.3).
- `systemctl disable firewalld.service` where it says `enabled`, then `systemctl stop firewalld.service` where it runs. Then a check: `inactive` (not `failed`), and disabled where the change disabled it.
- Its words say what it does: item 1, firewalld running, "Stop firewalld"; beside Enable, stopped and starting at boot, "Don't start firewalld at boot".
- A plain change. Its undo enables again and never starts: a start loads the saved configuration, not the one that ran.

### 1.5 The Firewall row

- `firewalld.Read` decides "running" with `systemctl is-active firewalld.service`, and calls `firewall-cmd` only where it is `active`: no 10 s wait on a stopped firewalld. `is-enabled` is read every time, and its failure is Unknown, not an error.
- `failed` is `FIREWALLD_FAILED`: a daemon that died ran no clean-up, so its rules may be loaded. The fix is `journalctl -u firewalld`, then `systemctl reset-failed firewalld.service`.
- 3a's port Rollback uses `firewall-cmd` only where firewalld is active and `firewall-cmd --state` says running: active is from the fork (no `Type=`).
- Stopped and starting at boot (`is-enabled` says `enabled`): Not Configured, "starts at boot, not running".
- Running and not started at boot: Partial, "not started at boot".
- These are proposed words.

### 1.6 The screen and the main menu

On Rocky Linux and AlmaLinux:
- title "Firewall Configuration";
- a firewalld section: Installed, Running, At boot, Default zone, Active zones;
- item 1 (Install, Install iproute, Enable or Disable), and Disable beside Enable or Install iproute where `is-enabled` says `enabled`, then Return;
- item 1 is Disable wherever firewalld runs, `ss` or not: Disable reads no sockets;
- no profiles or interfaces items: those stay ufw's, refused (`FIREWALL_EL_READ_ONLY`, worded for them alone).
- The main menu says "Configure firewalld" there (proposed words).
- `Read` pre-plans Enable, whose refusal item 1 then says before anything is asked. Install and Install iproute aren't pre-planned, since dnf's simulation may refresh metadata (6 h expiry) over the network, and Disable isn't either. Each of those is planned when it is chosen, and refused, if so, before its question.

### 1.7 Run All and `-w`

Both leave the firewall to the Firewall screen on Rocky Linux and AlmaLinux. Run All's Firewall row is Status only, and `-w` is refused there, whatever `enableUfwSshPolicy` says.

## 2. Unit 3a, as changed

- Its port change reads firewalld with the new `Read`. Stopped and disabled still plans nothing for firewalld; stopped and starting at boot is still `FIREWALLD_STOPPED`; stopped with `is-enabled` unread is `FIREWALLD_UNKNOWN`.
- Its "a stopped firewalld filters nothing" rests on `CleanupOnExit=yes`, now written down beside `PlanFor`.

## 3. Codes

| Code | Sentinel |
|---|---|
| `PACKAGE_KERNEL` | `packages.ErrKernel` |
| `PACKAGE_LOCKED` | `packages.ErrLocked` |
| `FIREWALLD_OTHER_FIREWALL` | `firewalld.ErrOtherFirewall` |
| `FIREWALLD_NO_CLEANUP` | `firewalld.ErrNoCleanup` |
| `FIREWALLD_BOOT_BLOCKED` | `firewalld.ErrBootBlocked` |
| `FIREWALLD_BOOT_UNKNOWN` | `firewalld.ErrBootUnknown` |
| `FIREWALLD_ZONE_NO_SSH` | `firewalld.ErrZoneNoSSH` |
| `FIREWALLD_NOT_READY` | `firewalld.ErrNotReady` (registered before `FIREWALLD_UNKNOWN`, which its error wraps) |
| `FIREWALLD_FAILED` | `firewalld.ErrFailed` |
| `FIREWALLD_NOT_INSTALLED` | `firewall.ErrFirewalldMissing` (Enable on a host without it) |
| `FIREWALLD_NOT_AT_BOOT` | `firewall.ErrRunningNotAtBoot` (§1.3, running and not started at boot) |

`FIREWALLD_NOT_LET_THROUGH`'s words become "an SSH port" rather than "the new port".

## 4. API

```go
// pkg/host/packages
const Dnf Tool                      // ToolFor: Rocky Linux, AlmaLinux
var ErrKernel, ErrLocked error      // *KernelError{Packages}
const InstallTimeout = 30 * time.Minute
// pkg/host/firewalld
type State struct{ Installed, Running detect.Tri; Default string; Active []string; Enabled detect.Tri; Unit string }
func CheckOthers(ctx, sys) error                       // *OtherFirewallError
func CleanupOnExit(ctx, sys) (bool, error)
func ReadOffline(ctx, sys) (Offline, error)             // firewall-offline-cmd
func NMZones(ctx, sys) (map[string]string, error)
func PlanEnable(ctx, sys, st State, ports []uint16) (EnablePlan, error)
func ExecuteEnable(ctx, sys, p EnablePlan) error
func RollbackEnable(ctx, sys, u EnableUndo) error      // plans unit "firewalld-enable"
func PlanDisable(ctx, sys, st State) (DisablePlan, error)
func ExecuteDisable(ctx, sys, p DisablePlan) error
var ErrOtherFirewall, ErrNoCleanup, ErrBootBlocked, ErrBootUnknown, ErrZoneNoSSH, ErrNotReady, ErrFailed error
const UnitFile = "/usr/lib/systemd/system/firewalld.service"
// pkg/engine/controls/firewall (el.go)
Reading.EL, Reading.Firewalld, Reading.FirewalldErr
var ErrFirewalldMissing, ErrRunningNotAtBoot, ErrUFWBeside error
```

## 5. Tested (fake)

- dnf: the probe's transactions (with the kernel, and without weak dependencies), modules for the running kernel and for another, an upgrade, a replacement, a wrapped row, no such package, the lock; rpm's answers.
- firewalld: Read on systemd's words (no `firewall-cmd` while stopped); PlanEnable's refusals and the default zone's adds (an operator's ssh service on 2222 included); ExecuteEnable's wait, a failed start, a zone bound after the plan; RollbackEnable with every step, and with a failed stop; Disable; CleanupOnExit's forms.
- The plans registry: `firewalld-enable` round-trips through the journal, its wire format pinned, tampered entries refused.
- The undo's order and what it keeps: a failed stop, a stop left active or failed, a failed disable, firewalld enabled before; the zone file and its `.old`; a failed start rolled back through `reset-failed`.
- The install running to its end after its caller's context is cancelled.
- The control: Install's mask, install, unmask and check, and its undo (a failed dnf unmasks too); refused beside ufw, another firewall, or an operator's mask; Enable's change, its refusals (NO_CLEANUP, ZONE_NO_SSH, OTHER_FIREWALL, FAILED, BOOT_BLOCKED, NOT_AT_BOOT), planned with reads alone; Disable; the Reading's item 1, `ss` missing included. Run All's Firewall row on Rocky Linux.
- The screen: four Rocky goldens (missing, stopped, starting at boot, running); Install's and Disable's flows; the refusals said before asking; `-w` refused; the main menu's words.
- Each fix of the review was checked by undoing it: a test fails without it.

## 6. Facts

Read on rocky10-test-110, 2026-11-10, read only:
- dnf's streams, the unknown package's words, and rpm's stdout.
- `firewalld.service`: no `Type=`, `Conflicts=iptables.service ip6tables.service ebtables.service ipset.service`, `Alias=dbus-org.fedoraproject.FirewallD1.service`; only a D-Bus policy file, no activation file.
- `CleanupOnExit=yes`; `90-default.preset` line 69, `enable firewalld.service`.
- No NetworkManager zone set; conntrack `tcp_loose=1`.
- `iproute` without an `iproute2` provide; `metadata_expire=6h`.
- The transaction without weak dependencies: 6 packages, no kernel.

## 7. Run on rocky10-test-110 (Rocky Linux 10.2, x86_64), 2026-11-10

- **Linux tests:** 73 packages ok. p4status read the Firewall as "no firewall installed".
- **Install firewalld**, through the Firewall screen:
  - it ran `systemctl mask`, dnf (transaction 5: 6 packages, no weak dependencies, no kernel), then `systemctl unmask`;
  - firewalld was left installed, inactive and disabled, so the mask kept the preset from enabling it;
  - the main menu then said "Configure firewalld".
- **SSH 22 to 2230:** firewalld stopped and disabled planned nothing but the SELinux label. Confirmed on 2230.
- **Enable**, confirmed from a new connection on 2230:
  - `firewall-offline-cmd --zone=public --add-port=2230/tcp`, then the start, then the enable;
  - firewalld ran about 3 s after its start;
  - public held 2230/tcp at runtime and permanently;
  - the zone file was created.
- **Disable:** disable, then stop, plain.
- **Enable left to its 1-minute timer:** started and enabled, then rolled back (stopped, disabled).
- **Enable, then a reboot past its deadline** (the timer stopped first, as a power loss would). At 11.7 s the boot unit started, and firewalld had started at 11.8 s. At 12.1 s the boot unit:
  1. found the deadline passed;
  2. stopped firewalld, which deactivated cleanly;
  3. checked `inactive`;
  4. disabled it (both links removed);
  5. removed the zone file the Enable had created, and was done by 12.7 s;
  6. then removed itself.

  Nothing waited on anything. After the boot: sshd on 2230, firewalld inactive and disabled, the stock public zone.
- **Without its weak dependencies,** firewalld logged no warning on any of its starts.
- **Put back:** sshd on 22, the label gone, dnf transaction 5 undone, `/etc/firewalld` and `/var/log/firewalld` removed, hardn removed. The kernel, GRUB's default and `/boot` were the same throughout: no kernel came or went.
- **Not run:** a firewalld that fails at its start; CleanupOnExit=no; a zone NetworkManager binds; another firewall unit. The fake covers each.
