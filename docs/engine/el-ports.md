# Rocky Linux and AlmaLinux: SSH port changes (`next/el-readonly`, unit 3a)

> **Unit:** the third Rocky/AlmaLinux unit, first half. The maintainer, 2026-11-09:
> - "Start unit 3";
> - firewalld and the SELinux port label as "One unit";
> - "Offer firewalld" where there is no firewall (unit 3b).
>
> **Status:** built, tested against the recording fake, and run on rocky10-test-110 (§4), 2026-11-10.

hardn changes the SSH port on Rocky Linux and AlmaLinux, as it does elsewhere. The refusal unit 2 made (`SSH_PORT_EL_REFUSED`) is gone. The Firewall row reads firewalld there.

## 1. What it does

1. **Before sshd moves** (`steps.planELPorts`, called by `steps.PlanSSH` and the `listen.conf` migration on `Distro.EL`), each port the change adds is made reachable. Each part is a lockout-risk change of `StageFirewall`, before the files and the reload.
   - **The SELinux label.** `semanage port -a -t ssh_port_t -p tcp <port>`, then checked with `semanage port -l` (`selinux.PlanSSHPort`, `LabelsSSHPort`).
     - Skipped where SELinux is disabled (no selinuxfs), or where the policy names the port `ssh_port_t` already. Permissive counts as on: the label is made, so sshd still binds once SELinux enforces.
     - A port that only a range of another type holds is labelled. On rocky10-test-110, 2230 is in `unreserved_port_t`'s `1024-32767`, and an entry of its own wins over a range.
   - **firewalld.** The zones read are the active ones and the default zone.
     - A zone **carries SSH** where a current SSH port is let through by an added port or a service naming it (`--permanent --service=<s> --get-ports`), at runtime or in the permanent configuration.
     - Each zone that carries SSH gets each new port, at runtime and permanently, wherever it isn't let through already.
     - A zone whose target is ACCEPT needs nothing, and carries nothing either. SSH reaching the host through another zone by a rich rule must not be passed over because a zone of container interfaces accepts all.
     - Execute then queries each zone at runtime (`firewalld.Execute`).
     - firewalld not installed, or stopped and disabled, plans nothing.
2. **Refused before anything changes:**

   | Case | Code |
   |---|---|
   | SELinux on and no `semanage` (policycoreutils-python-utils, in AppStream; installed on rocky10-test-110) | `SELINUX_NO_SEMANAGE` |
   | The policy gives the port to another service by name (`semanage -a` can't; `-m` would take it away) | `SELINUX_PORT_TAKEN` |
   | firewalld stopped and starting at boot: its permanent configuration, which hardn reads only from a running firewalld, applies then | `FIREWALLD_STOPPED` |
   | firewalld running, not every zone accepting everything, and no zone carrying SSH (a rich rule, a policy, a direct rule) | `FIREWALLD_NO_SSH_PATH` |
   | firewalld not read | `FIREWALLD_UNKNOWN` |

   Tested at the SSH screen: all but `FIREWALLD_UNKNOWN`, which `firewalld`'s own tests cover.
3. **A failed apply:** a zone still not letting the port through after the steps is `FIREWALLD_NOT_LET_THROUGH`, and the change is rolled back.
4. **Undo.** Each change journals its undo before it runs, as a checked plan (`revert.OpPlan`; [revert-plans.md](../safety/revert-plans.md)): `selinux-ports` and `firewalld-ports`.
   - Each takes away only what is there. The planner planned only what wasn't, so a rollback of a step that never ran, or one taken up again, takes away nothing else.
   - firewalld running: `firewall-cmd [--permanent] --zone=<z> --remove-port=…`, each queried first.
   - firewalld not running, as when the boot unit runs before it starts: `firewall-offline-cmd --zone=<z> --remove-port=…`, permanently only, there being no runtime. Its queries answer as `firewall-cmd`'s (firewalld's `command.py`, which both use).
   - The boot unit is not ordered after firewalld: firewalld needs D-Bus, which starts after `sockets.target`, and the boot unit runs before `ssh.socket`.
   - If firewalld starts while the boot rollback runs, the port may stay open until firewalld next reloads. That locks no one out.
5. **After `hardn confirm`** the clean-up drops the old port from `00-hardn.conf` as everywhere. firewalld's `ssh` service and the policy's own `ssh_port_t 22` stay.
   - A port or label an earlier move added also stays after a later move. The journal holds only the confirmed change, which is how ufw's rules behave too.
6. **The Firewall row** (`checkFirewalld`, proposed words):

   | firewalld | Row |
   |---|---|
   | Running, no zone read with target ACCEPT | Configured, "firewalld running" |
   | Running, a zone with target ACCEPT | Not Configured, "zone accepts everything" |
   | Running, not started at boot (since 3b) | Partial, "not started at boot" |
   | Installed, stopped | Not Configured, "firewalld not running"; "starts at boot, not running" where it does (since 3b) |
   | Not installed | ufw's line, where ufw is installed; else Not Configured, "no firewall installed" |
   | Not read, or failed | Unknown, "can't read" |

   - The row doesn't check which listeners firewalld lets through, as ufw's row does. Whether SSH gets through is the port change's to check.
   - Since 3b, "running" is `systemctl is-active firewalld.service`, and `firewall-cmd` runs only where it is active ([el-firewalld.md](el-firewalld.md) §1.5).
   - p4status allows `firewall-cmd --state`, `--get-default-zone`, `--get-active-zones`, a zone's permanent `--get-target`, and `systemctl is-active` and `is-enabled firewalld.service`.
   - Since 3b the Firewall screen installs, enables and disables firewalld there; profiles and interfaces stay refused (`FIREWALL_EL_READ_ONLY`).
7. **EPEL's ufw:** where ufw is installed and active on such a host, a port change also lets its new port through ufw, as elsewhere (`steps.ErrELFirewall` refuses only the Firewall item's ufw changes).

## 2. API

```go
// pkg/host/firewalld
type State struct{ Installed, Running detect.Tri; Default string; Active []string; Enabled detect.Tri }
func Read(ctx, sys) (State, error)
func Allows(ctx, sys, zone string, port uint16, permanent bool) (bool, error)
type Step struct{ Zone string; Port uint16; Permanent bool }
func PlanFor(ctx, sys, st State, hold, add []uint16) (Plan, error)
func (Plan) Undo() Undo
func Execute(ctx, sys, p Plan, add []uint16) error
func Rollback(ctx, sys, u Undo) error
var ErrUnknown, ErrNoSSHPath, ErrStartsAtBoot, ErrNotLetThrough error
// pkg/host/selinux
func PlanSSHPort(ctx, sys, port uint16) (*PortStep, error)
func LabelsSSHPort(ctx, sys, port uint16) (bool, error)
func RollbackPorts(ctx, sys, u PortsUndo) error
var ErrNoSemanage, ErrPortTaken error
```

## 3. Tested (fake)

- A port change on a Rocky model (SELinux enforcing, `semanage`, firewalld running with public's stock services) is applied and rolled back by the timer. The label and both ports are added, then removed; sshd's files come back byte for byte, and `sshd.service` is reloaded twice.
  - Again with firewalld down at the rollback: `firewall-offline-cmd` removes the permanent port.
- The planners, on each answer form:
  - `--state`'s three answers, and `is-enabled`'s;
  - the zones with and without " (default)";
  - ACCEPT alone, and beside a zone that carries SSH or one that doesn't;
  - a port already let through;
  - a zone carrying SSH only permanently;
  - no SSH path;
  - a port another type takes.
- Both rollbacks round-trip through the journal, their wire format is pinned, and tampered entries are refused (`plans` tests).

## 4. Run on rocky10-test-110 (Rocky Linux 10.2, x86_64), 2026-11-10

firewalld was installed for the run, with the maintainer's OK ("Yes, install it"), and the install undone after it.
- **Linux tests:** `go vet` clean, `go test` 73 packages ok. p4status: Firewall "Configured, firewalld running".
- **firewalld 2.4.3**, as `dnf install` leaves it: enabled (the preset), public the default zone with `cockpit dhcpv6-client ssh`, `--get-active-zones` with " (default)".
- **Port 22 to 2230, confirmed** from a new connection on 2230. The plan and its steps: the label, `--add-port` at runtime and permanently, the drop-in, the reload. After the confirmation's clean-up: sshd on 2230 alone, `ssh_port_t 2230, 22`, firewalld `2230/tcp` at runtime and permanently.
- **2230 to 2240, left to a 1-minute timer:** both ports during the window. The rollback queried and removed 2240 permanently and at runtime, then took its label away. sshd was back on 2230 alone, and `reverted.json` written.
- **The same change, firewalld stopped before the timer fired:** `firewall-cmd --state` answered exit 252 after 10 s. The rollback ran `firewall-offline-cmd --zone=public --query-port=2240/tcp`, then `--remove-port`, skipped the runtime entry, and took the label away. Started again, firewalld let 2230 alone through.
- **Not run:** the boot unit's rollback at a real boot.

Found on the way:
- With firewalld down, `firewall-cmd` retries D-Bus for 10 s before it answers. "Waiting on dbus connection..." goes to stderr, which hardn doesn't parse.
- Installing firewalld's kernel made a rescue boot entry for the host's machine-id. `dnf history undo` left that entry in `/boot` and GRUB's default on it: the next boot would have been the rescue kernel. Both were put back by hand (`grubby --set-default` to the running kernel; the entry and its 107 MB removed).

## 5. What the next unit must do (3b)

Done in unit 3b, as [el-firewalld.md](el-firewalld.md) describes; the brief as it was:

- **dnf** as `packages.Tool` (`dnf install --assumeno` read: "Installing", "Installing dependencies", "Upgrading" sections). It must say before installing when the transaction brings a kernel (as firewalld's does on a cloud image). Its undo must not leave GRUB's default on a rescue entry (§4).
- **The Firewall item on Rocky Linux and AlmaLinux:**
  - install firewalld;
  - enable it with the SSH listeners let through its default zone first (lockout risk, confirm-or-revert);
  - disable it.

  Its screen reads firewalld. ufw's profiles and interfaces stay refused there.
- **CI images** (unit 6): done, [el-changes.md](el-changes.md) §5.

## 6. Decisions of this unit

- **Zones:** the new port goes where the current one gets through, so the exposure matches. A zone with no SSH path gets nothing.
- **Stopped and enabled is refused**, not planned offline: an offline plan would have to read zone bindings from NetworkManager's profiles.
- **SELinux disabled:** no label. Turning SELinux on later needs `semanage port -a -t ssh_port_t -p tcp <port>` first, or sshd won't bind.
- **Clean-up removes only what the confirmed change added,** as for ufw.
