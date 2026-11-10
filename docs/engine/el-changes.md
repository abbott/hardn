# Rocky Linux and AlmaLinux: changes that move no port (`next/el-readonly`, unit 2)

> **Unit:** the second Rocky/AlmaLinux unit (the maintainer, 2026-11-09: "Start unit 2"), after [el-readonly.md](el-readonly.md).
> **Status:** built and tested on macOS against the recording fake, and run on rocky10-test-110 (§3c, §4).

Rocky Linux and AlmaLinux 10 and 9 are Supported. hardn changes what moves no SSH port, and refuses the rest with words that say why. **Since unit 3a** ([el-ports.md](el-ports.md)) the port changes too; §1.5's port row and `SSH_PORT_EL_REFUSED` are gone, and the Firewall row reads firewalld. **Since unit 3b** ([el-firewalld.md](el-firewalld.md)) the Firewall item installs, enables and disables firewalld; §1.5's Firewall row now holds for ufw's profiles and interfaces alone.

## 1. What it does

1. **Matrix rows.** Rocky Linux 10 and 9, and AlmaLinux 10 and 9, are `Supported` (`detect/platform.go`), by the matrix's rule: the releases the vendor supports, the newest two. A VM run covered Rocky Linux 10 alone. The engine's support gate now lets their plans through.
2. **What changes:**
   - user creation (`useradd`), sudo rules (`/etc/sudoers.d`, `visudo`) and SSH keys;
   - root login off or key-only, and password login off or on (`00-hardn.conf`, validated over a copy, then reloaded);
   - the Environment and Backup settings.

   The confirm-or-revert timer (`systemd-run`) and the boot unit are systemd's, as on Debian.
3. **The reload is `sshd.service`'s** (`reload.PlanFor` from `detect.SSH.ServiceUnit`): `systemctl reload sshd.service`, and on a rollback `systemctl start sshd.service` when it is down. It is checked up with `systemctl is-active sshd.service`. Rocky Linux's `ExecReload` is `/bin/kill -HUP $MAINPID` with no `sshd -t` first, unlike Debian's, so `sshd.Validate`'s `sshd -t` over a copy of the whole configuration is the only check of the files before the reload. The reload step says so.
3a. **SELinux labels, where SELinux enforces** (`pkg/host/selinux`):
   - **sshd's files, on Rocky Linux and AlmaLinux** (`matchpathcon -V`; code `SELINUX_MISLABELED`). `steps.PlanSSH` refuses a change when a file sshd rereads at the reload is labelled otherwise than its policy:
     - every file `sshd.Validate` checked, with what each links to, followed to the end. A link that can't be followed is refused too.
     - each `HostKey` that exists; sshd only warns about a missing one.

     Why: hardn's own `sshd -t` runs as unconfined root and would pass a file sshd's context is refused, and their reload has no `sshd -t` of its own, so sshd would exit there. Debian's `ssh.service` runs `sshd -t` first, so this isn't checked there. `00-hardn.conf` is left out: it is written new and takes its directory's label.
   - **hardn's binary, on every SELinux host** (`stat -c %C`; code `SELINUX_HARDN_NOT_RUNNABLE`). `revert.CheckExecutable`, which the engine runs before arming, refuses a binary whose type, as it is, isn't `bin_t`. On rocky10-test-110 systemd refused to run a binary of type `admin_home_t` (copied into `/root`) or `user_tmp_t` (moved from `/tmp`), exit 203, so the revert timer and the boot unit would never roll back. The type is read, not compared with the policy: the policy's own label for `/root` is `admin_home_t`, which `matchpathcon` would call verified. `install.sh` installs in place, so its binary is `bin_t`.
   - Where SELinux is permissive or disabled, nothing is asked.
3b. **A unit waiting to restart is down.** `reload.isUp` reads systemd's "activating" with `SubState=auto-restart` as down ("waiting to restart"), not as a state it doesn't know. That is sshd exited, with systemd restarting it after `RestartSec` (42 s on Rocky Linux). A change's check after the reload then fails with `RELOAD_UNIT_DOWN`, and its rollback's `systemctl start` runs. `start` runs such a unit at once (rocky10-test-110, a unit run three times by its first run, a start and a restart). This holds on every systemd host.
4. **`/etc/sysconfig/sshd` at plan time.** `steps.PlanSSH` reads `OPTIONS` (`reload.CheckSysconfig`) before `sshd -T`, as the status rows do. A non-empty `OPTIONS` refuses the change (`RELOAD_SYSCONFIG_OPTIONS`).
5. **Refused:**

   | What | Refusal | Code |
   |---|---|---|
   | An SSH port change (SSH Port, Run All's `sshPort`) | `steps.PlanSSH` and the SSH screen: "hardn doesn't change the SSH port on Rocky Linux or AlmaLinux yet: SELinux, and firewalld where it runs, would block a new one." Run All's SSH Port row is Refused with it, rather than pointing at SSH Login (proposed words) | `SSH_PORT_EL_REFUSED`, as `pve.CheckPort` refuses one on Proxmox VE |
   | Every Firewall action that touches the host (install, enable, disable, profiles, interfaces; since unit 3b, profiles and interfaces alone) | "hardn doesn't change the firewall on Rocky Linux or AlmaLinux yet: it drives ufw alone, not firewalld." Checked before anything else, Proxmox VE's facts included. Saving `hardn.yml`'s profiles and interfaces is not refused | `FIREWALL_EL_READ_ONLY` |
   | Auto Updates on or off (until unit 5: [el-updates.md](el-updates.md)) | "hardn doesn't read dnf-automatic yet"; no apt command runs | `UPDATES_NOT_READ` |
   | DNS | "hardn doesn't change DNS on Rocky Linux or AlmaLinux yet: NetworkManager manages it there. Set the nameservers with nmcli." Refused whatever `resolv.conf` looks like | `DNS_EL_READ_ONLY` |

   Run All plans each row alone and skips the ones that refuse. So on Rocky Linux it does the user and root login, and says why it skips the rest.

## 2. API

```go
var steps.ErrELPort error     // SSH_PORT_EL_REFUSED
var firewall.ErrEL error      // FIREWALL_EL_READ_ONLY
var dns.ErrEL error           // DNS_EL_READ_ONLY (pkg/engine/controls/dns)
var steps.ErrELFirewall error // the ufw planners' own refusal (firewall.ErrEL is it)
// pkg/host/selinux
const EnforceFile = "/sys/fs/selinux/enforce"
var Matchpathcon = []string{"matchpathcon", "-V"}
var ErrMislabeled error       // SELINUX_MISLABELED
func Enforcing(ctx, sys) (detect.Tri, error)
func Mislabeled(ctx, sys, paths ...string) ([]string, error)
func Check(ctx, sys, paths ...string) error
const RunnableType = "bin_t"
var Stat = []string{"stat", "-c", "%C"}
var ErrNotRunnable error      // SELINUX_HARDN_NOT_RUNNABLE
func Type(ctx, sys, path string) (string, error)
func CheckRunnable(ctx, sys, path string) error
```

`reload.PlanFor` takes the unit from `Input.SSH` (`SSH.ServiceUnit()`), so it has no new API. The read-only refusal of unit 1 (§1.3 there) is gone.

## 3. Tested (fake)

- Disallow root, key-only and password login on are planned on a Rocky model: Debian 13's files, `sshd.*` units, the stock sysconfig, no ufw.
- Disallow is applied and rolled back by the timer's rollback, which re-detects the `sshd.*` units. The files come back byte for byte, and `sshd.service` is reloaded twice. A step naming `ssh.service` fails the test.
- The port and sysconfig refusals, at the SSH screen and in `steps.PlanSSH` itself.
- Every firewall action refused with no host call.

## 3c. Settled on the VM (experiments, 2026-11-09)

The lockout review's points that could be tried were tried on rocky10-test-110, and each test was removed after.

| Point | Result |
|---|---|
| `~/.ssh` and `authorized_keys` as hardn makes them (`install -d`, a temporary file renamed), from an unconfined shell and from a systemd unit | `ssh_home_t` both ways: no fix needed |
| systemd running a binary by its label | `admin_home_t` and `user_tmp_t`: exit 203; `bin_t`: 0. Fixed by §1.3a |
| `systemctl reload sshd.service` with the files unchanged | returned in 51 ms, after sshd logged "Received SIGHUP; restarting" and was listening again; MainPID unchanged |
| a unit waiting to restart | `is-active` prints "activating" (exit 3), `SubState=auto-restart`; `start` and `restart` each run it at once. Fixed by §1.3b |

## 4. The VM change run (rocky10-test-110, 2026-11-09)

hardn itself, built from this tree, was run on the VM.

1. Installed with `install` (`bin_t`).
2. `-n -d` planned the boot unit, `00-hardn.conf`, `systemctl reload sshd.service` and the `sshd -T` check.
3. `-u hardntest -c --yes` made the user, `/etc/sudoers.d/hardn-grant-hardntest` (`etc_t`), and `~/.ssh` and `authorized_keys` (`ssh_home_t`, matchpathcon "verified"). A key login as hardntest from the Mac worked, with `sudo -n`.
4. `-d --confirm-timeout 1m`, not confirmed: the timer rolled it back at its deadline. `00-hardn.conf` was gone, `sshd.service` reloaded (re-detected as the `sshd.*` units), and root login back to key-only.
5. `-d`, confirmed with `sudo hardn confirm` from a new connection: `permitrootlogin no`, the timer stopped, sshd on the same PID, and hardntest still able to log in.
6. The journal is `var_lib_t`, the backups `var_t`, and the boot unit and its link `systemd_unit_file_t`.
7. Everything was removed after. The snapshot before and after differs only in two directories' times.

It found two defects, both fixed since:
- **The boot unit ordered itself `Before=ssh.service ssh.socket`,** units Rocky Linux doesn't have, so nothing put its boot-time rollback before sshd. It now names `sshd.service` and `sshd.socket` too, on every host: a unit that doesn't exist orders nothing ([revert-boot.md](../safety/revert-boot.md) §2).
- **The reload step was titled "Reload sshd (ssh.service)".** It now says `sshd.service`.

Not run: a reboot inside the confirm window, and AlmaLinux.

## 5. What the next units must do

- **firewalld** (unit 3): read the active zones; let the SSH listeners through before a port change. Built: [el-ports.md](el-ports.md) (3a), [el-firewalld.md](el-firewalld.md) (3b).
- **The SELinux port label** (unit 4): `semanage port -a` before the reload, rolled back with `-d`. With unit 3, it lifts the port refusal of §1.5. Built in 3a: [el-ports.md](el-ports.md).
- **dnf-automatic** (unit 5): built, [el-updates.md](el-updates.md).
- **CI images** (unit 6): done. CI's per-distribution `go vet` and `go test` job runs on `rockylinux/rockylinux:10` and `:9`, and `almalinux:10` and `:9`. It hasn't run them yet.

## 6. Decisions of this unit

- `Supported` for 10 and 9 of both, by the matrix rule, though only Rocky Linux 10 was on a VM (the maintainer started the unit with this listed as its decision).
- The port refused rather than planned without firewalld and SELinux: on `sshd.service` an unlabelled port isn't bound. hardn's check after the reload would roll that back, but the change would always fail.
- The firewall refused, not ufw driven: the maintainer, 2026-11-09, "we shouldn't be forcing ufw installs anyway".
