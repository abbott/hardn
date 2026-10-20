# VM acceptance: the Phase 2 driver and the steps to run (`internal/tools/p2drive`)

> **Unit:** refactor plan Phase 2, `next/p2-vmtest` (plan §2, row `p2-vmtest`; brief Phase 2 "Acceptance criteria"; plan §7, the verify items still open; risks R2, R8, R14, R15; decisions §3.14 and §3.18, approved 2026-10-17; brief §2 "Confirm-or-revert").
> **Status:** **everything on a VM is written, not run (gate 5).** The driver is built here and its tests pass on macOS against the recording fake (`hostfake`) only; no command in this page has run on any host. Where a step says what to see, that is the expectation the step checks, not a result. Results are recorded only once the maintainer has provided the VMs and the steps have run there (plan §1, R14).

`p2drive` composes the Phase 2 primitives under `pkg/host` without the Phase 3 engine, in the order brief 2.3 and [revert.md](revert.md) §7 give the engine, so that brief Phase 2's scenario can run on a disposable VM before the slices exist. §4 says exactly what to run, in order, and what each step proves.

## 1. Why

Brief Phase 2's acceptance is a VM test that changes the SSH port to 2230 and checks that key login on 2230 succeeds, that password login is rejected, that a broken configuration is rejected before it takes effect, that an unconfirmed change reverts automatically, and that `sudo hardn confirm` from a new session on the new port cancels the revert. Every primitive that test needs exists ([README.md](README.md)), but the engine that orders them is Phase 3's (plan `p3-engine`), and the slices that put them behind hardn's screens come after it. Without a driver, nothing that needs a real `sshd`, `systemctl`, `ufw`, `visudo` or a timer could be shown before Phase 3 (R14), and the units' "written, not run" lists would stay open.

## 2. Guarantees

### The gates

1. **It refuses to run anywhere but a disposable Linux VM, as root** (exit 2, before anything is read or run, and before the host seam is even built): on macOS it refuses by name, first; on any other system that is not Linux; without the flag `--i-am-on-a-disposable-vm` (before or after the command); without `HARDN_P2DRIVE_DISPOSABLE=yes` in its environment; and as any user but root. Both the flag and the variable are needed, so a stray alias or an exported variable alone runs nothing. Tested: `TestGates`, which also shows that the host is built only once every gate holds.
2. **Never in a release.** The release builds are `./cmd/hardn` alone (`.slsa-goreleaser/*.yml` `main: ./cmd/hardn`; `.github/workflows/ci.yml`; every build target of the makefile), a `main` package cannot be imported, and depguard's `dev-tools` rule denies `internal/tools` to every package outside it. `make check`'s `check-build` compiles it and discards the output; `check-cross` vets it for linux/amd64 and arm64.

### Planned before anything changes

3. **Every refusal comes before the revert timer is armed.** A lockout-risk command reads detect's facts (`detect.Detect`), the session it runs in (`revert.ReadSession`), Alpine's sshd binary (`reload.OpenRCFlavour`) and the installed configuration (`sshd.ReadEffective`); then it plans: the Include mode, the legacy files (`migrate.Find`), Proxmox VE's rules (`pve.Applies`, `pve.CheckPort`, and `pve.PlanSSH` over the whole of the `00-hardn.conf` the change leaves, the lines it keeps as well as the ones it sets), the new `00-hardn.conf`, its validation in context (`sshd.Validate` over a copy in `/run/hardn/<id>`), the reload (`reload.PlanFor`), the bound sockets (`ss -Htln`, the check `reload.Execute` makes before its first step, run here at plan time too) and the firewall (`ufw.Read`, `ufw.PlanFor`). It prints the plan; `--plan-only` stops there. Refused before the timer, with nothing written outside the run directory (tested: `TestRefusedBeforeArm`, one row each):
   - a port change on Proxmox VE (`pve.ErrPortChange`, §3.18);
   - an `sshd_config` whose first directive is not exactly the stock Include (no write strategy: [P-38](../refactor/proposals.md#p-38)), or that has no Include (hardn's block at the top of `sshd_config` is not something this driver writes);
   - legacy hardn SSH files on the host (`manage.conf`, `hardn.conf`, `listen.conf`, a generated Alpine `sshd_config`): `migrate` comes first;
   - **a port change that adds no listener on the new port**: a `ListenAddress` with its own port ignores every `Port` line (sshd_config(5), V_10_0_P1, `ListenAddress`: "If port is not specified, sshd will listen on the address and all Port options specified"), so the new port must be among the listeners the reload adds (`reload.Plan.Adds`); the refusal names each such `ListenAddress` by file and line. This is the R8 path: a migrated v0.3.2 `00-hardn.conf` keeps `manage.conf`'s `ListenAddress 0.0.0.0:2230` (§4.8 step 8);
   - **a new port another socket already holds** (`reload.ErrInUse`);
   - `pve-root` off Proxmox VE; a migration whose rollback's reload would be refused (`migrate.Plan.CheckRollback`: on a socket host, `listen.conf` not giving exactly what `ssh.socket` is configured for now, D9 among them, item 11);
   - **a listener change where what is configured is not what is bound** (`reload.CheckInEffect`, `reload.ErrNotInEffect`; `port` and `migrate`): a configured listener `ss` does not show bound (a `daemon-reload` since `ssh.socket` was restarted, an `sshd_config` edited and not reloaded), or none covering the port this session is connected to; a remote session whose connection is not established is refused too;
   - listeners that are not established (`reload.ErrListenersUnknown`), a port sshd already has (a usage error, exit 2), and whatever the primitives refuse ([sshd-validate.md](sshd-validate.md), [reload.md](reload.md), [ufw.md](ufw.md)).
4. **`00-hardn.conf` is edited line by line, never regenerated** ([sshd-migrate.md](sshd-migrate.md) §6): the lines of the keywords the change sets are dropped and the new ones come first, under `# Set by p2drive:`, so they are global; every other line stays as it was. The new lines go after the file's header (its leading comments up to a blank line, or up to the first comment ending in `:`, which labels the lines after it as migrate's `# From <file>:` does and so stays with them). A file with a `Match` or `Include` line, and an argument that would need quoting, are refused (`fs.ErrInvalid`). On Proxmox VE, a kept line `pve.PlanSSH` changes (root added to a kept `AllowUsers`, say) is rewritten among the new lines. Tested: `TestSetDirectives`, `TestPVERoot`.
5. **The port change holds the old port.** The Port lines are `reload.HoldPorts` of the listeners sshd configures now and the new port, so during the confirm window `00-hardn.conf` has both `Port 22` and `Port 2230` ([reload.md](reload.md) §2.5). On Ubuntu 24.04 and 26.04 the reload is `reload.PlanFor`'s socket path: `systemctl daemon-reload`, `systemctl show -p Listen ssh.socket` checked for every listener, then `systemctl restart ssh.socket`. Dropping port 22 after the confirmation (brief 2.3 step 5) is a change of its own, which this driver does not make ([revert.md](revert.md) §7): after a confirmation both ports stay open and allowed.

### Applied under confirm-or-revert

6. **In the engine's order, journalled write-ahead** ([revert.md](revert.md) §7): `revert.Arm` (the journal, then the timer: `systemd-run` on systemd, a `setsid` sleeper on OpenRC); the firewall first (brief 2.3 step 2): `ufw.Plan.PreUndo()` journalled, `ufw.Execute`, then its `Report.Undo` journalled; each file backed up (`backup.Run.Backup`, `RollbackRequired`) and the backups and the `ssh` reload (with `IfUp`) journalled before the write; the write, only once the backup shows the file as the plan read it; `reload.Execute`, and the reload journalled again without `IfUp` once its gated step ran, also when `Execute` then failed; `sshd -T` read again and checked for the values the change set (each new `Port` in sshd's Port set **and on one of its listeners**; an Unknown is not a pass; on Proxmox VE, `pve.CheckSSH` must show root key-only, root by key and port 22 each `yes`, since a `no` in the last two is a lockout of the cluster, [pve.md](pve.md) §6); then `revert.MarkApplied`. **The two journal writes that record a step that already ran** (ufw's `Report.Undo`, the reload without `IfUp`) run under a context of their own (`context.WithoutCancel`, 30 s), so that after Ctrl-C the journal still says what the rollback must undo: without it, a reload that Ctrl-C interrupted after it stopped `ssh.service` would be rolled back without starting it again. Tested: `TestPort2230` pins that order on the fake, on Debian's `ssh.service` and on Ubuntu's `ssh.socket` (armed, `ufw allow 2230/tcp`, the backup, the journal, the write, then the reload: `systemctl reload ssh.service`, or `daemon-reload`, `systemctl show -p Listen ssh.socket` and `restart ssh.socket`), and the journal it leaves; `TestPortOpenRC` the sleeper and `rc-service sshd reload` on Alpine, with no ufw installed; `TestPortUFWEnable` and `TestUFWEnable` ufw's allows before its enable, from an inactive ufw, and the undo journalled (`ufw disable`, then `ufw delete allow 22/tcp`); `TestKeyOnly` and `TestPVERoot` the values checked after the reload; `TestCancelledDuringReload` the journal write after Ctrl-C.
7. **A step that fails rolls the change back at once**, while the session is open: `revert.Rollback` with `ReasonFailed`, which runs without the command's cancellation, and with `SIGHUP` and `SIGINT` ignored and `SIGTERM` caught until it returns (`holdSignals`, as `cmd/hardn` holds them for `hardn rollback`, [revert.md](revert.md) §2.17). The commands the rollback runs inherit the ignored signals, so a second Ctrl-C reaches neither p2drive nor the `ufw` or `systemctl` it is running (they are in its process group). Ctrl-C and `SIGTERM` during the apply cancel the command and so roll back too. A hangup before the rollback ends the process: the timer, armed before the first step, rolls the change back at the deadline. **A rollback that does not finish all the same** (a command that fails) stops the timer anyway (revert.md §2.14 step 4), so p2drive's error says to run `hardn rollback <id>`, which takes it up; [README.md](README.md) hands that on to `p3-engine`. Tested: `TestPortRollsBack` (a reload sshd refuses: the file is removed, the ufw rule deleted, sshd reloaded, the timer stopped, the journal cleared, all under the hold); `TestFailedAfterArm` (ufw refusing the rule, `00-hardn.conf` changed since the plan, `sshd -T` not showing the values, `MarkApplied` unable to list the sshd processes: each rolled back, the host as it was); `TestCancelledDuringReload` (Ctrl-C during the reload, `ssh.service` left down: the rollback starts it); `TestHoldSignals` (this test process survives `SIGINT`, `SIGHUP` and `SIGTERM` sent to itself while held).
8. **The timer runs hardn, not the driver** ([revert.md](revert.md) §7): `--hardn` (default `/usr/local/bin/hardn`) is the binary `systemd-run` or the sleeper runs as `hardn rollback <id>`, checked by `revert.CheckExecutable` (root's, not a symlink, no group- or world-writable directory above it). That check is of owner and mode, not of what the file is: a v0.3.2 hardn installed over it has no `confirm` or `rollback` (its root command takes any arguments and opens the menu), so the timer would roll nothing back. So before arming, the driver prints the SHA-256 of the binary the timer will run, to compare with the build's (§4.1). The confirmation is `hardn confirm`; the undo before the deadline is `hardn rollback <id>`. Both go through `cmd/hardn`'s `main()`, which writes `/var/log/hardn.log` and `/etc/hardn/hardn.yml.example` before it parses flags (V26, `p3-startup`): accepted on a disposable VM.

### The other commands

9. **`broken`** validates `00-hardn.conf` with `PermitRootLogin maybe` added and requires `sshd.Validate` to refuse it as a configuration error (`*sshd.ConfigError`, its problems naming the real file), with `00-hardn.conf` as it was and nothing reloaded. Tested: `TestBroken`.
10. **`sudoers`** chooses sudo, sudo-rs or doas from detect's facts (`sudoers.Choose`), plans the grant (`sudoers.Read`, `sudoers.PlanFor`), backs up each file it changes and installs it with `sudoers.Apply`: `visudo -c`, `visudo -cf` on a root-only copy (or `doas -C`), the write, `visudo -c` again, the old files put back on failure. **It is journalled** (since `next/p2-revert-plans`, [revert-plans.md](revert-plans.md) §2.11): the plan is one `sudoers` plan step (`plans.Sudoers`), recorded before the backups and the write, which `hardn rollback <id>` and the timer undo with `sudoers.Rollback` through `cmd/hardn`'s registry. A plan with a lockout-risk change (`--grant none` removing a grant, `--grant password`: `sudoers.RiskLockout`) is a lockout-risk change under confirm-or-revert, so it takes the lockout flags, prints the timer's hardn and how to confirm (with `sudo -n true` to check, or for a password grant `sudo -v`, which asks the password: `sudo -n` cannot **[unverified]**), and is rolled back at the deadline unless a new SSH connection from another machine confirms it, which is also where to check that elevation still works; any other (a `nopasswd` grant where there was none) is journalled without a timer and cleared once applied. A failure after the timer is armed rolls it back at once. Tested: `TestSudoers` (the grant, journalled before its backup and write and cleared; then `--grant none`, armed and applied, and the timer's rollback through the registry putting the grant back), `TestSudoersRollsBack` (`visudo -c` refusing the installed grant: restored, rolled back, the journal cleared; refusing the configuration before any write: the plan step dropped from the journal, then rolled back and cleared).
11. **`migrate`** is a dry run by default: `migrate.Find`, `migrate.PlanFor`, `migrate.Check` (validation and the login-path comparison over a copy) and the reload's plan. `--apply` carries it out under confirm-or-revert as items 6 to 8 say, every file of `Plan.Sources` backed up and journalled first; its values check compares `sshd -T` after the reload with what `migrate.Check` showed of the migrated copy (the lockout-relevant settings, the Port set and sshd's listeners; an Unknown on either side is not a pass). `migrate.Apply` takes its own backups of the same files and writes with no point between the two where a journal write could go, so its backups land in the same run after p2drive's and are not the ones journalled ([README.md](README.md), `p3-engine`). **On a socket-activated host** (since `next/p2-revert-plans`, [revert-plans.md](revert-plans.md) §2.8 to §2.12) the rollback's reload is journalled with the listeners that accept connections now (`Plan.Listening.All`, the socket's) and with `listen.conf` named as a drop-in the restore puts back (`socketDropIns`), so the rollback restores `listen.conf`, runs `daemon-reload`, checks that the socket is configured for those listeners and restarts it; before that unit, `reload.PlanFor` refused the restored `listen.conf` (`reload.ErrSocketDropIn`) and the rollback could not finish, so `--apply` was refused on every socket host. ufw is planned over `Plan.Listening`. The journalled reload is `migrate.Plan.Rollback()`. **It refuses, in the dry run as with `--apply`, a migration whose rollback could not finish**: `Plan.CheckRollback` plans the rollback's reload over the drop-ins as they are, and refuses where `listen.conf`'s lines do not give exactly what `ssh.socket` is configured for (`reload.ErrRestoredListeners`): `listen.conf` never in effect (D9, the socket still on 22), or the socket configured for 22 beside 2230. **And where what is configured is not what is bound** (`reload.CheckInEffect`, item 3): a `daemon-reload` since `ssh.socket` was restarted leaves it configured for 2230 and bound on 22, which neither the hold nor the rollback, both read from configuration, would keep. Before `next/p2-revert-plans`'s review fixes the driver checked only that the migration's hold was among the socket's configured listeners, which passed both of those hosts. Tested: `TestMigrate` (Debian v0.3.2: the dry run changes nothing; `--apply`; `--apply` rolled back when the reload fails and when `sshd -T` does not show the migrated configuration); `TestMigrateSocket` (Ubuntu v0.3.2, rebooted since: `--apply`, and its rollback restarting `ssh.socket` on `listen.conf`'s `[::]:2230`); `TestRefusedBeforeArm` (D9; the socket configured for more than `listen.conf`; configured 2230 and bound 22, with the session on 22 and on 2230; a port change where `sshd -T` shows 2231 and sshd is still on 22).
12. **`facts`** is read-only: detect's facts, the Include mode, the legacy files, `sshd -T`'s values, listeners, findings and conflicts, `ssh.socket`'s drop-ins, the bound sockets (`ss -Htln`), ufw's state and its coverage of the listeners, on Proxmox VE `pve.CheckSSH`, `pve.ReadFirewall` and root's keys link, the sudoers choice and the change journal's notices. A read that fails is printed and the report goes on. Tested: `TestFacts` (Debian 13, Ubuntu 26.04, v0.3.2 on Debian 13, Proxmox VE 9: nothing changed, and the lines §4 quotes).
13. **Only through `host.System`.** Every command any of these runs goes to the host's run log, printed with the transcript; the driver itself runs nothing directly.

### What the driver does not cover

- hardn's block at the top of an `sshd_config` without an Include, and the `IncludeOther` case ([P-38](../refactor/proposals.md#p-38));
- the confirmed clean-up (dropping the held port and its rule);
- the Alpine updates and AppArmor rules ([alpine.md](alpine.md) §4), DNS ([dns.md](dns.md) §4), root's keys on Proxmox VE beyond reading the link ([pve.md](pve.md) §4 item 5), and the legacy operations through `host.Real` ([system.md](system.md) §4, P-32): their gate-5 lists stay open for a later driver revision or for the slices (§6).

## 3. Platform facts relied on

| Fact | Source | Used for |
|---|---|---|
| OpenSSH 9.8 added `PerSourcePenalties`, "on by default": sshd refuses connections from a client address that repeatedly fails authentication, connects without authenticating, or crashes it, for a penalty that grows with repeat offences; `PerSourcePenaltyExemptList` exempts addresses | [OpenSSH 9.8 release notes](https://www.openssh.com/txt/release-9.8) ("Potentially-incompatible changes", "New features"), read 2026-10-18 | §4.2: the runner is exempted |
| The defaults: `authfail` 5 s, `noauth` 1 s, `grace-exceeded` 10 s, a penalty enforced only once it reaches `min` 15 s, at most `max` 10 min; `PerSourcePenaltyExemptList` is "a comma-separated list of addresses", wildcards and CIDR ranges allowed | [sshd_config(5)](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_0_P1/sshd_config.5), V_10_0_P1, `PerSourcePenalties` and `PerSourcePenaltyExemptList`, read 2026-10-18 | §4.2: the alternative of spacing the attempts |
| A `ListenAddress` with a port listens on that port alone: "If port is not specified, sshd will listen on the address and all Port options specified" | [sshd_config(5)](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_0_P1/sshd_config.5), V_10_0_P1, `ListenAddress`, read 2026-10-18 | §2.3, §2.6: a port change must add a listener on the new port |
| OpenSSH per release: Debian 13 and Proxmox VE 9 10.0p1, Ubuntu 26.04 10.2p1, Alpine 3.24 10.3_p1 (all ≥ 9.8, so penalties are on); Debian 12 9.2p1 and Ubuntu 24.04 9.6p1 (before 9.8: no such keyword) | [sshd-effective.md](sshd-effective.md) §3 (the audit's research §1 **[V]**) | §4.2: where the exemption line is valid |
| Ubuntu 24.04/26.04 are socket-activated; a port change needs `daemon-reload` and `restart ssh.socket`; Debian 13 and Proxmox VE 9 run `ssh.service`; Alpine runs OpenRC's `sshd` | [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9) **[V]**; [reload.md](reload.md) §3 | §2.5, §4.4 |
| That sessions survive `systemctl restart ssh.socket`, and that a reload of `ssh.service` or OpenRC's sshd rebinds a changed Port | **[I]** ([reload.md](reload.md) §3, §4) | §4.4, §4.6: checked there |
| Proxmox VE: no SSH port change (§3.18); `PermitRootLogin yes` written by Proxmox VE into `sshd_config`, which the drop-in's `prohibit-password` beats; root's `authorized_keys` a link to `/etc/pve/priv/authorized_keys` | [pve.md](pve.md) §3 | §4.7 |
| An Ubuntu ISO install without imported keys gets `50-cloud-init.conf` with `PasswordAuthentication yes` | [sshd-effective.md](sshd-effective.md) §3 (research §3b, §6 **[V]**) | §4.5 |
| v0.3.2's `manage.conf` and `listen.conf`, verbatim, are `pkg/host/sshd/migrate/testdata/legacy/v032-manage.conf` and `v032-listen.conf`; v0.3.x's keys are in `~/.ssh_<user>/authorized_keys` | [sshd-migrate.md](sshd-migrate.md) §3 (v032-reference.md §4.2 **[V]**; side-effects.md D7) | §4.8: the R8 image |
| That OpenSSH 10.x still accepts `manage.conf`'s `Protocol 2` and `HostbasedAcceptedKeyTypes` lines | **[unverified]**; `migrate.Check` runs `sshd -t` over them first, so a refusal shows in the dry run | §4.8 |
| The release builds use `CGO_ENABLED=0`, `-trimpath` and `-tags=netgo` | `.slsa-goreleaser/linux-amd64.yml`, `linux-arm64.yml` (this repo) | §4.1: the cross-build |
| `ss` (iproute2) is on a stock Debian, Ubuntu and Proxmox VE and not on a stock Alpine; without it the reload is `Unverified` (`no-ss`), not failed | **[unverified]** ([reload.md](reload.md) §3, §2.10) | §4.2: iproute2 on Alpine |
| ufw is in Debian and Ubuntu, and in Alpine 3.24's community repository | [ufw.md](ufw.md) §3 (sources.debian.org; aports); the Ubuntu 26.04.1 cloud image has it installed and inactive (§4.9) | §4.2 |
| Which sshd drop-ins Alpine 3.24's cloud images ship, and whether cloud-init or another agent there writes `sshd_config.d` | **[unverified]**: plan §7 lists it as still unverified; nothing in the findings or the units' pages answers it | §4.6: read on such an image with `facts` |
| sudo's `env_reset` drops the caller's variables, and `sudo env NAME=value cmd` runs `env` as the command, which sets it for `cmd`; doas and sudo-rs the same | sudoers(5) for `env_reset` ([revert.md](revert.md) §3); for `env` as the command, and for doas and sudo-rs, **[unverified]** | §4.3: how the gate variable reaches the driver |

## 4. What to run, in order (written, not run)

The plan names server-side scripts under `test/vm/`. The `.gitignore` allow-list re-includes no shell script there (it would need a line such as `!/test/vm/*.sh`, which is the maintainer's to add), so none is committed, and the commands are given inline below instead; each block can be pasted as it is.

Synthetic names below: the VM is `web-01.example.net` at `192.0.2.20`, the runner (another machine, the maintainer's workstation or a second VM) is `192.0.2.10`, the user is `alice`. Substitute the real values when running; do not commit them.

### 4.1 Build and copy (on the development machine)

Never run hardn or p2drive here (plan §1); p2drive refuses macOS anyway.

```sh
cd hardn-p2   # the worktree on next/p2-vmtest or later
out=$(mktemp -d)
arch=amd64    # arm64 for an aarch64 VM
GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=$arch \
  mise exec -- go build -trimpath -tags=netgo -o "$out/hardn-linux-$arch" ./cmd/hardn
GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=$arch \
  mise exec -- go build -trimpath -tags=netgo -o "$out/p2drive-linux-$arch" ./internal/tools/p2drive
shasum -a 256 "$out/hardn-linux-$arch"   # record it: the lockout commands print the one the timer runs
scp "$out/hardn-linux-$arch" "$out/p2drive-linux-$arch" alice@web-01.example.net:/tmp/
```

On the VM, as root (installed root's, so that `revert.CheckExecutable` accepts hardn for the timer), with `arch` as above and `BIN` the directory both go in:

```sh
arch=amd64 BIN=/usr/local/bin
install -o root -g root -m 0755 /tmp/hardn-linux-$arch $BIN/hardn
install -o root -g root -m 0755 /tmp/p2drive-linux-$arch $BIN/p2drive
sha256sum $BIN/hardn    # the build's, recorded above
```

On a Debian host with `/etc/staff-group-for-usr-local`, `/usr/local` is `2775 root:staff` and the timer refuses hardn there ([revert.md](revert.md) §2.8): use `BIN=/usr/sbin` instead, add `--hardn /usr/sbin/hardn` to every lockout command below (`port`, `keyonly`, `pve-root`, `ufw-enable`, `migrate --apply`; its default is `/usr/local/bin/hardn`), and set `BIN=/usr/sbin` on the runner too (§4.3). Each lockout command prints `the revert timer runs <path>, sha256 <sum>` before it arms the timer: it must be the build's.

### 4.2 Prepare each VM once, then snapshot it as `p2-clean`

1. `alice`, with a password, the runner's public key in `~alice/.ssh/authorized_keys` (`~/.ssh` `0700`, the file `0600`, alice's), and elevation: Debian and Ubuntu `adduser alice` and `usermod -aG sudo alice`; Alpine `adduser alice` and `adduser alice wheel` (doas's stock `20-wheel.conf`). On Proxmox VE, root's key as the cluster has it.
2. **The runner's exemption from `PerSourcePenalties`** (OpenSSH ≥ 9.8, §3): the password-rejection checks and any failed attempt accrue penalties against the runner's address, and once one reaches 15 s sshd refuses every new connection from it, the confirming one included, which would read as a lockout. On Debian 13, Ubuntu 26.04, Alpine 3.24 and Proxmox VE 9:

   ```sh
   printf 'PerSourcePenaltyExemptList 192.0.2.10\n' > /etc/ssh/sshd_config.d/90-runner.conf
   sshd -t && systemctl reload ssh.service     # Alpine: rc-service sshd reload
   ```

   The drop-in sorts after `00-hardn.conf` and sets nothing hardn writes, so it shows in no conflict; leave it out on OpenSSH before 9.8 (Debian 12, Ubuntu 24.04), which refuses the keyword. Without it, space the attempts out: a failed authentication adds 5 s and a connection that never authenticates 1 s, enforcement starts at 15 s accrued (§3), and how a penalty decays between attempts is **[unverified]**; wait out a refusal (at most `max`, 10 min) rather than retrying into it.
3. Tools: on Debian 13 `apt install ufw` and leave it inactive; on Alpine `apk add iproute2` (the listener check reads `ss`) and leave ufw out (with ufw not installed, `--ufw keep` has nothing to do); on Ubuntu 26.04 check `ufw status` reads `inactive` (§3 **[unverified]**).
4. `sshd -T | grep -E '^(port|passwordauthentication|permitrootlogin) '` and `systemctl status ssh.socket ssh.service` (or `rc-service sshd status`) recorded for the run's notes.
5. Snapshot `p2-clean`. Every scenario below starts from it, or from the snapshot it names.

Commands on the VM run as root from an SSH session from the runner, `ssh -t alice@web-01.example.net`, then:

```sh
BIN=/usr/local/bin   # or /usr/sbin, §4.1
P2="sudo env HARDN_P2DRIVE_DISPOSABLE=yes $BIN/p2drive --i-am-on-a-disposable-vm"   # doas on Alpine
```

In a root shell (Proxmox VE, §4.7, where sudo is likely absent **[I]**, [detect.md](detect.md) §3) the same without `sudo`:

```sh
P2="env HARDN_P2DRIVE_DISPOSABLE=yes $BIN/p2drive --i-am-on-a-disposable-vm"
```

Keep the transcript of each command (`$P2 … 2>&1 | tee /root/p2-<step>.log`).

### 4.3 The runner's checks

From the runner, another machine (a connection from the VM itself never confirms, [revert.md](revert.md) §2.11):

```sh
BIN=/usr/local/bin   # where hardn is on the VM, §4.1
key()   { ssh -o BatchMode=yes -o ConnectTimeout=5 -p "$1" alice@web-01.example.net true; echo "key login on $1: exit $?"; }
offers(){ ssh -v -o BatchMode=yes -o ConnectTimeout=5 -o PubkeyAuthentication=no \
            -o PreferredAuthentications=password,keyboard-interactive -p "$1" alice@web-01.example.net true 2>&1 |
          grep 'Authentications that can continue'; }
confirm(){ ssh -t -p "$1" alice@web-01.example.net sudo "$BIN/hardn" confirm; }   # doas on Alpine
```

`key 2230` exit 0 is a key login on 2230. `offers 2230` printing `publickey` alone is sshd refusing every password method on 2230: password login is rejected. "Connection refused" or a timeout from `key 2230` after a rollback is the port closed again.

### 4.4 Debian 13 (`ssh.service`), then Ubuntu 26.04 (`ssh.socket`)

The same steps on each; the socket-specific checks are marked.

| # | Run | Expect | Proves |
|---|---|---|---|
| 1 | `$P2 facts` | the Debian 13 (Ubuntu 26.04) row of [detect.md](detect.md) §3's table with no `err`; `include: include first, line <n> "Include /etc/ssh/sshd_config.d/*.conf"` (the first directive, below the stock comments); no `legacy:` line; `sshd -T now: port [22] (known true)`, `listeners now: … (known true; …)`; `ufw: installed true, active false` | detect.md §4, sshd-validate.md §4 (stock), sshd-effective.md §4 (stock), ufw.md §4 (reading) |
| 2 | `$P2 broken` | `refused: /etc/ssh/sshd_config.d/00-hardn.conf line 3: …`; exit 0; `ls /etc/ssh/sshd_config.d` unchanged | **a broken configuration is rejected before it takes effect** (brief); sshd-validate.md §4 (the real messages, mapped to the real file) |
| 3 | `$P2 ufw-enable`; then from the runner within 180 s: `confirm 22` | the plan lists `ufw allow 22/tcp` before `ufw --force enable`; the applying session survives the enable; `confirm 22` says confirmed; `ufw status verbose`: active, deny incoming, 22 allowed | UFW's allow before its enable, under confirm-or-revert (ufw.md §4, the session surviving; revert.md §4, confirming) |
| 4 | snapshot `p2-ufw` | | |
| 5 | `$P2 port --port 2230 --plan-only` | the plan: `ufw allow 2230/tcp`; `00-hardn.conf` with `Port 22` and `Port 2230`; the reload (Ubuntu: `daemon-reload`, `check-socket`, `restart ssh.socket`); held 22, added 2230; nothing changed | the plan and every refusal come before the timer |
| 6 | `$P2 port --port 2230` | `the revert timer runs …, sha256 <the build's>`; armed; `firewall: verified true`; `reload: verified true`; `sshd -T after: port [22 2230]`, `listeners after:` on 22 and 2230; "applied"; the applying session still works. Ubuntu: `systemctl show -p Listen ssh.socket` and `ss -Htln` show 22 and 2230 | the port change in brief 2.3's order with the old port held (reload.md §4, the 2230 scenario; sshd-effective.md §4, the confirm window); Ubuntu: **sessions survive `restart ssh.socket`** (plan §7) |
| 7 | from the runner: `key 2230`, `key 22` | both exit 0 | **key login on 2230 succeeds**; 22 is held until the change is confirmed |
| 8 | refusals, each from the runner or the VM as said: `sudo $BIN/hardn confirm` in the applying session; in a second session from the runner, opened before step 6; `confirm 22` (the held port); on the VM `ssh -t -p 2230 alice@localhost sudo $BIN/hardn confirm` | each refused, saying why (`ErrSameSession`, `ErrOldConnection`, `ErrWrongListener`, `ErrLocalConn`) | only a new connection from another machine to the new port confirms (revert.md §4, confirming) |
| 9 | from the runner, before the deadline: `confirm 2230` | "confirmed"; `systemctl list-timers` no longer lists `hardn-revert-<id>`; the deadline passes with nothing rolled back | **`sudo hardn confirm` from a new session on the new port cancels the revert** (brief) |
| 10 | `$P2 keyonly`, then from the runner `confirm 2230`, then `offers 2230`, `offers 22`, `key 2230` | confirmed; `offers` prints `publickey` alone on both ports; `key 2230` exit 0 | **password login is rejected** (brief), after key login was confirmed (brief 2.3 step 6) |
| 11 | revert to `p2-ufw`; `$P2 port --port 2230`; do not confirm; after the deadline (180 s; `journalctl -u hardn-revert-<id>` to follow) | the timer's `hardn rollback <id>` ran: `00-hardn.conf` gone, `ufw status` without 2230, `sshd -T` port 22 alone; Ubuntu: `ssh.socket` on 22 alone; from the runner `key 2230` refused or timed out, `key 22` exit 0; `sudo $BIN/hardn confirm` then says "was rolled back at …, because it was not confirmed within 180s" | **an unconfirmed change reverts automatically** (brief); revert.md §4 (systemd: the timer within a second of the deadline, after the session that armed it) |
| 12 | revert to `p2-ufw`; `$P2 port --port 2230`; close the applying session; after the deadline, reconnect on 22 | rolled back as in 11 | the rollback survives the end of the SSH session and of p2drive (revert.md §4) |
| 13 | revert to `p2-clean`; `$P2 sudoers --user alice --grant nopasswd`; `ls /var/lib/hardn`; `sudo -l -U alice`; as alice `sudo -n true`; then `$P2 sudoers --user alice --grant none --hardn $BIN/hardn` and do not confirm; after the deadline, as alice `sudo -n true`; then `$P2 sudoers --user alice --grant none --hardn $BIN/hardn` again and, from the runner, `confirm 22` | the grant: no timer, `visudo -c`, `visudo -cf` on the copy, the write, `visudo -c`; `hardn-grant-alice` `0440` root's; no `pending.json` left; `sudo -n true` exit 0. The first `--grant none`: armed, the file gone, "applied"; at the deadline the timer's `hardn rollback <id>` puts `hardn-grant-alice` back and `visudo -c` passes (`journalctl -u hardn-revert-<id>`: `undo 1 of 1: … sudoers`), and `sudo -n true` exits 0 again. The second: confirmed, the file gone | the sudoers grant checked with visudo before it takes effect (sudoers.md §4; Ubuntu 26.04: sudo-rs's visudo); **the registered sudoers rollback, run by the timer from the journal** ([revert-plans.md](revert-plans.md) §4) |
| 14 | `$P2 sudoers --user alice --grant password --env HARDN_CONFIG --hardn $BIN/hardn`; from the runner `confirm 22` (alice's password asked by sudo there); as alice, `HARDN_CONFIG=/tmp/x sudo sh -c 'echo "$HARDN_CONFIG"'` (alice's password asked) | armed (a grant that asks a password is a lockout-risk change); confirmed; `/tmp/x` printed; `hardn-env-alice` holds `Defaults:alice env_keep += "HARDN_CONFIG"` | **sudo-rs keeps `env_keep`** on Ubuntu 26.04 (plan §7; sudoers.md §4); confirming from a new session that elevation works (brief 2.3 step 6) |
| 15 | **Written, not run (gate 5):** added by `next/p3-rollback-abandon` ([revert.md](revert.md) §8) after the 2026-10-19 runs of §4.9. Revert to `p2-ufw`; `$P2 port --port 2230 --timeout 10m` (a window long enough that the timer does not fire before the steps below; the whole step, up to the abandon, inside it); make its rollback unfinishable on purpose: `sudo rm -r /var/backups/hardn/<id>` (the run directory p2drive printed, `backup 1: … in /var/backups/hardn/<id>`); `sudo $BIN/hardn rollback <id>` twice, `echo $?` after each; `sudo ls /var/lib/hardn`; `systemctl list-timers 'hardn-revert-*'`; `sudo $BIN/hardn -n rollback --abandon <id>`; as alice, without sudo, `$BIN/hardn rollback --abandon <id>`; then `sudo $BIN/hardn rollback --abandon <id>`; `sudo sshd -t; echo $?`; `sudo ls /var/lib/hardn`; `sudo grep abandoned /var/log/hardn.log`; `grep Port /etc/ssh/sshd_config.d/00-hardn.conf`; `sudo ufw status`; from the runner `key 22`; `sudo $BIN/hardn rollback <id>`; `sudo $BIN/hardn rollback --abandon <id>` again; `$P2 sudoers --user alice --grant nopasswd`; revert to `p2-ufw` | each `rollback <id>` exits 1: the first runs ufw's undo (`undo 1 of …: ufw delete allow 2230/tcp`), cannot load the backup run, so restores nothing and does not reload `ssh`, stops the timer (`systemctl stop hardn-revert-<id>.timer`), and its error names `sudo hardn rollback --abandon <id>`; the second retries the restore and runs that `systemctl stop` again (should it fail, `the timer was not stopped` is logged, which changes nothing: [revert.md](revert.md) §2.14 step 4); `rollback.json` in `/var/lib/hardn`, no `reverted.json`; `list-timers` without `hardn-revert-<id>`. `-n`: exit 2, "-n only previews"; alice: exit 1, "must be run as root"; `rollback.json` still there. The abandon: exit 0, "Gave up the rollback of change <id>", "The host stays as the rollback left it", ufw's undo under "Undone by the rollback", the restore of `/var/backups/hardn/<id>` and the `ssh` reload under "Not undone, or not finished", the warning to check sshd's files with `sudo sshd -t`, and the record's path; `sshd -t` exit 0 (nothing was restored, so the files are the change's, which passed it); `abandoned-<id>.json` and no `rollback.json`; the log's `revert: rollback <id> (…) abandoned: backups restored false, …` line; `00-hardn.conf` still with `Port 22` and `Port 2230` and ufw without a 2230 rule (as the rollback left them); `key 22` exit 0. Then `rollback <id>`: exit 0, "Nothing to roll back for <id>: its rollback did not finish and was given up (`hardn rollback --abandon`), …; its record is kept in /var/lib/hardn/abandoned-<id>.json"; the second abandon: exit 1, "abandoned already", with the record's path; the sudoers grant journalled and applied, not refused as another change in the journal | **a rollback that cannot finish is given up with `hardn rollback --abandon <id>`** ([P-35](../refactor/proposals.md#p-35) (a)): root only, refused under `-n`, the host left as the rollback left it, the record kept, the journal freed for the next change (revert.md §8) |
| 16 | **Written, not run (gate 5):** added by `next/p3-rollback-abandon` ([revert.md](revert.md) §8.1 item 7), step 15 with the timer's own rollback as the one that cannot finish. Revert to `p2-ufw`; `$P2 port --port 2230`; `sudo rm -r /var/backups/hardn/<id>`; do not confirm; after the deadline (180 s; `journalctl -u hardn-revert-<id>` to follow), `sudo ls /var/lib/hardn`; `sudo $BIN/hardn rollback --abandon <id>`; `sudo $BIN/hardn rollback <id>`, `echo $?`; `sudo $BIN/hardn confirm`, `echo $?`; `sudo ls /var/lib/hardn`; `$P2 facts`; revert to `p2-ufw` | the timer's `hardn rollback <id>` ran ufw's undo, could not load the backup run, did not reload `ssh`, and exited 1 (the journal: `did not finish`); `rollback.json` and `reverted.json` in `/var/lib/hardn`. The abandon: exit 0, the report of step 15. `rollback <id>`: exit 0, "Nothing to roll back for <id>: its rollback did not finish and was given up …", not "it was rolled back at"; `confirm`: exit 1, "change <id> (…) was not confirmed within 180s, and its rollback did not finish and was given up …"; `reverted.json` still there after both, beside `abandoned-<id>.json`; `facts`: `journal reverted: … The rollback did not finish: backup: …` | after the timer's rollback is given up, **neither `rollback <id>` nor `confirm` says the change was rolled back**, and the next run's notice that it did not finish is kept (revert.md §8.1 item 7) |

### 4.5 Ubuntu 26.04 with cloud-init's `PasswordAuthentication yes`

From `p2-clean` of the Ubuntu 26.04 VM (or an Ubuntu ISO install without imported keys, §3): `printf 'PasswordAuthentication yes\n' > /etc/ssh/sshd_config.d/50-cloud-init.conf` and `systemctl reload ssh.service`.

| # | Run | Expect | Proves |
|---|---|---|---|
| 1 | `offers 22` | `publickey,password` | the cloud file is in force before hardn |
| 2 | `$P2 keyonly`; `confirm 22`; `$P2 facts`; `offers 22` | `facts` lists a `shadowed` conflict at `50-cloud-init.conf` line 1, `passwordauthentication "no"`; `offers` prints `publickey` | `00-hardn.conf` beats `50-cloud-init.conf` (V13), reported and not edited (brief 2.2; sshd-effective.md §4) |

### 4.6 Alpine 3.24 (OpenRC)

As 4.4 steps 1, 2, 5 to 13 (without ufw: steps 3 and 4 do not apply, the plans have no firewall step, and `p2-clean` stands for `p2-ufw`), with `doas` for `sudo`, and `rc-service sshd status` for systemctl. Also:

- the timer is the sleeper: `cat /run/hardn/revert-<id>.pid` names a process whose `/proc/<pid>/cmdline` is `/bin/sh -c sleep "$1" && "$0" rollback "$2" /usr/local/bin/hardn 180 <id>`; it survives the end of the applying session and of `doas`; `confirm` removes the pidfile and ends the sleeper (revert.md §4, OpenRC);
- `p2drive facts` shows `openrc sshd binary: /usr/sbin/sshd` (or `sshd.pam` with `UsePAM yes`), the Alpine row of detect.md §3, and `sudoers: doas`;
- `rc-service sshd reload` moving the port, and how long sshd takes to rebind against the 10 × 500 ms poll (reload.md §4);
- step 13 with doas: `/etc/doas.d/hardn-alice.conf` `0640`, `doas -C` on the copy, `doas -n true` as alice (sudoers.md §4);
- **an Alpine 3.24 cloud image** (plan §7: "Alpine cloud images' sshd drop-ins", §3 **[unverified]**), booted as the image ships it, before any change: `ls -la /etc/ssh/sshd_config.d`, then `$P2 facts`. Record every drop-in, the `include:` line and each conflict `facts` lists; then steps 1, 2, 5 and 6 above on it. A drop-in that sets a lockout-relevant keyword (`PasswordAuthentication`, `PermitRootLogin`, `Port`, `ListenAddress`) after `00-hardn.conf` shows as a `shadowed` conflict, as `50-cloud-init.conf` does on Ubuntu (§4.5); one that sorts before `00-hardn.conf` is a finding for [sshd-effective.md](sshd-effective.md).

### 4.7 Proxmox VE 9 (by hand; plan `p2-vmtest`, adjustment §3.18)

On one node of a two-node test cluster, as root, with the node's web console shell open before any change ([pve.md](pve.md) §4 is the full checklist; these are the plan's six items). `$P2` is the root form of §4.2, without `sudo`.

| # | Run | Expect | Proves |
|---|---|---|---|
| 1 | `readlink /root/.ssh/authorized_keys`; `$P2 facts` | `/etc/pve/priv/authorized_keys`; `proxmox: installed yes`, `pmxcfs yes`; `proxmox ssh: root key-only no, root by key yes, port 22 yes` (Proxmox VE's own `PermitRootLogin yes`); `root's keys: … -> /etc/pve/priv/authorized_keys`; `ufw: ufw is not used on Proxmox VE: …` | detection; the link as Proxmox VE keeps it |
| 2 | `$P2 port --port 2230` | refused before anything changed: "cluster SSH and pve-firewall's management rule use port 22"; no `systemd-run`, nothing written | **the port change is refused** |
| 3 | `$P2 pve-root`; from the other node `ssh root@<node> $BIN/hardn confirm` (a new connection, from another machine) | `sshd -T after: permitrootlogin "prohibit-password"` while `sshd_config` still says `PermitRootLogin yes`; `Proxmox VE: root key-only yes, root by key yes, port 22 yes` and a `proxmox-root-login` finding; confirmed. Any of the three not `yes` rolls the change back at once | **prohibit-password is effective over Proxmox VE's yes** |
| 4 | from the other node: `ssh -o BatchMode=yes root@<node> true`; `ssh -o BatchMode=yes -o PubkeyAuthentication=no -o PreferredAuthentications=password root@<node> true` | exit 0; then refused | **root key login on 22; password rejected** |
| 5 | `readlink /root/.ssh/authorized_keys`; `ls -la /etc/pve/priv/` | the same link; no `.authorized_keys.hardn-*` left | **the `authorized_keys` link is intact** (R15) |
| 6 | restore `PermitRootLogin` by removing `00-hardn.conf` and reloading, then `$P2 pve-root` and do not confirm | after 180 s the timer's `hardn rollback <id>` restores the files and reloads `ssh.service`: `sshd -T` shows `permitrootlogin yes` again | **the revert timer for a change that moves no port** |

### 4.8 The v0.3.2-hardened image (R8)

Built from `p2-clean` of the Ubuntu 26.04 VM and, separately, of the Debian 13 VM, with v0.3.2's files as the migrate fixtures reproduce them (§3): copy `pkg/host/sshd/migrate/testdata/legacy/v032-manage.conf` to `/etc/ssh/sshd_config.d/manage.conf` and `v032-listen.conf` to `/etc/systemd/system/ssh.socket.d/listen.conf`, both `0644` root's; put alice's key in `~alice/.ssh_alice/authorized_keys` (directory `0700`, file `0600`, alice's) and remove `~alice/.ssh/authorized_keys`, so that only the legacy key file lets her in; on Ubuntu, snapshot `p2-v032-d9` now, before the reboot (D9: `ssh.socket` still on 22, `listen.conf` not in effect); reboot (v0.3.x never ran `daemon-reload`, D9: the reboot is what makes `listen.conf` take effect). After it sshd listens where v0.3.2 put it: on Debian `manage.conf`'s `ListenAddress 0.0.0.0:2230` alone, on Ubuntu `ssh.socket` on `listen.conf`'s 2230; open the applying session and the runner's checks on 2230. Snapshot `p2-v032`.

Installing the v0.3.2 release binary and running its SSH hardening there gives the same files, which is the maintainer's choice, with one condition: **v0.3.2's `install.sh` writes `/usr/local/bin/hardn`**, the path §4.1 installs the new hardn in and `--hardn`'s default, and v0.3.2 has no `confirm` or `rollback` (its root command takes any arguments and opens its menu), so a timer armed over it rolls nothing back and nothing can confirm. So install v0.3.2 elsewhere (`/root/hardn-v032`, by hand rather than with its `install.sh`), or install the new hardn again after it (§4.1) and check `sha256sum $BIN/hardn` against the build's before step 2. Every lockout command below names the binary: `--hardn $BIN/hardn`, and its `the revert timer runs …, sha256 …` line must show the build's sum.

| # | Run | Expect | Proves |
|---|---|---|---|
| 1 | `$P2 facts` | `legacy: manage.conf (hardn v0.3.x) /etc/ssh/sshd_config.d/manage.conf`, `legacy: listen.conf (hardn v0.3.x) /etc/systemd/system/ssh.socket.d/listen.conf`; Ubuntu: `ssh.socket` on 2230 and a `sources-disagree` finding | `migrate.Find` and sshd-effective on a real upgrade path |
| 2 | `$P2 port --port 2231 --hardn $BIN/hardn` | refused: "legacy hardn SSH files …: run `p2drive migrate` first" | no change on top of an unmigrated host |
| 3 | `$P2 migrate` | the steps (write `00-hardn.conf` with the moved lines and `AuthorizedKeysFile .ssh/authorized_keys .ssh_%u/authorized_keys`, remove `manage.conf`, remove `listen.conf`), `migrated (a copy)` values, the reload plan; nothing changed | sshd-migrate.md §4 (the plan with the real `systemctl show -p Listen`; `Check` in a dry run) |
| 4 | Ubuntu: `$P2 migrate --apply --hardn $BIN/hardn`; from the runner `key 2230` (the key in `~/.ssh_alice`), then `confirm 2230` | applied: `listen.conf` and `manage.conf` gone, `daemon-reload`, `check-socket`, `restart ssh.socket`; `ss -Htln` shows 2230; `sshd -T after` as `migrated (a copy)` showed it; key login on 2230 with the legacy key file; confirmed | **R8 on `ssh.socket`**: a v0.3.2 Ubuntu host keeps its key login through the migration, `listen.conf`'s port moved into `00-hardn.conf` (sshd-migrate.md §4) |
| 5 | Ubuntu: revert to `p2-v032`; `$P2 migrate --apply --hardn $BIN/hardn`; do not confirm; after the deadline (`journalctl -u hardn-revert-<id>`), from the runner `key 2230` | the timer's `hardn rollback <id>`: `listen.conf` and `manage.conf` restored, `00-hardn.conf` gone, then `daemon-reload`, `systemctl show -p Listen ssh.socket` naming `[::]:2230`, `restart ssh.socket`, `ss` showing 2230 bound; the journal cleared; `key 2230` exit 0 | **the `listen.conf` migration rolls back on `ssh.socket`** (revert-plans.md §4; before `next/p2-revert-plans` it stopped at `ErrSocketDropIn`) |
| 6 | Ubuntu: revert to `p2-v032-d9`; `$P2 migrate --apply --hardn $BIN/hardn` (the session on 22) | refused before anything changed: "the rollback's reload would be refused …: ListenStream=2230 is not one of the listeners the socket had" (D9; `reload.ErrRestoredListeners`) | a migration whose rollback could not give the socket back what it is configured for is refused, not applied (§2.11; revert-plans.md §2.10) |
| 6a | Ubuntu: revert to `p2-v032-d9`; `systemctl daemon-reload` (not followed by a restart); `systemctl show -p Listen ssh.socket` and `ss -Htln`; then `$P2 migrate --apply --hardn $BIN/hardn` (the session on 22) | `Listen=[::]:2230`; whether `ss` still shows 22 bound, and 2230 not, is **[I]** (systemd reattaches a kept descriptor only to a port still configured, reload.md §3, while sshd may hold the one it was passed): record it; either way refused before anything changed: "this session is connected to 192.0.2.20:22, which the configured listeners [[::]:2230] do not cover" (`reload.ErrNotInEffect`) | configured is not bound: the restart would close 22 for good, and the rollback would not reopen it (revert-plans.md §2.10, §4) |
| 7 | Debian: `$P2 migrate --apply --hardn $BIN/hardn`; from the runner `key 2230` (the key in `~/.ssh_alice`), then `confirm 2230` | applied; `ssh.service` reloaded; `sshd -T after` as `migrated (a copy)` showed it; key login on 2230 with the legacy key file; confirmed | **R8: a v0.3.2 host keeps its key login through the migration**; the inert `listen.conf` removed (sshd-migrate.md §4, Debian) |
| 8 | Debian: `$P2 port --port 2231 --hardn $BIN/hardn` | refused before anything changed: no new listener on 2231, "pinned by /etc/ssh/sshd_config.d/00-hardn.conf:<line> "ListenAddress 0.0.0.0:2230"" | a port change the migrated `ListenAddress` would ignore is refused, not reported applied (§2.3) |

### 4.9 The results

For each VM: the transcripts, `journalctl -u 'hardn-revert-*'` (systemd) or `/var/log/hardn.log`, and for each step whether it showed what the table expects. Every "Expect" that does not hold is a finding for the unit whose page the "Proves" column names. Until then every row stays "written, not run".

#### Debian 13, run 2026-10-19

Built from `next/p2-revert-plans` (a7f873c), linux/amd64. The VM: Debian 13 (trixie), OpenSSH 10.0p2 (Debian-7+deb13u4), sudo 1.9.16p2, systemd with `ssh.service` (no socket), the stock first-line `Include`, `PasswordAuthentication no` and `KbdInteractiveAuthentication no` already in the main file, `/usr/local` `root:root 0755`. The runner was the development machine (`192.0.2.10` below), the VM `192.0.2.20`; the transcripts, with the real addresses, are kept outside the repository. The VM had no working DNS at first, so the steps below ran without ufw (not installed: `--ufw keep` had nothing to do); the ufw steps ran later the same day, once the VM could install packages (the ufw pass, below).

Snapshots could not be reverted from the runner, so the steps ran in an order where each rollback returns the VM to the state the next step needs: 1, 2, 5, 11 (also 12), 6–9, 10, 13, 14.

| # | Result | Notes |
|---|---|---|
| 1 | **pass** | `facts`: Debian 13 supported, systemd, `ssh.service`, not Proxmox VE; `sshd -T` port 22 known; listeners `[::]:22` and `0.0.0.0:22` known, socket none; ufw not installed; sudo (flavour sudo). It also reports `effective-unknown: the conflict report is not established` because `00-hardn.conf` does not exist yet: by design ([sshd-effective.md](sshd-effective.md), `ConflictsKnown`) |
| 2 | **pass** | the real sshd refused the candidate (`unsupported option "maybe"`), mapped to `/etc/ssh/sshd_config.d/00-hardn.conf line 4`; nothing written, nothing reloaded; exit 0 |
| 3, 4 | see the ufw pass | ufw not installed in this pass (no DNS on the VM) |
| 5 | **pass** | plan only: `Port 22` and `Port 2230`, backup first, `systemctl reload ssh.service`, 22 held and 2230 added; nothing changed |
| 6 | **pass** | the timer runs `/usr/local/bin/hardn` with the build's sha256; backup of the absent `00-hardn.conf`; `reload: verified true`; `sshd -T` after: port 22 and 2230; listeners on both families for 22 and 2230 |
| 7 | **pass** | key login on 2230 and on 22, both exit 0, during the window |
| 8 | **pass** | all four refused, each with its reason and what to do: the applying connection ("this is the SSH connection that applied the change"); a connection opened before the change ("was open when the change was applied"); a new connection to the held port 22 ("did not reach a listener the change added"); from the VM to its own port 2230 over `[::1]` ("comes from the host itself") |
| 9 | **pass** | a new connection from the runner to 2230: confirmed; the timer stopped; nothing rolled back after the deadline |
| 10 | **pass** | `keyonly`: confirmed from a new connection; sshd offers `publickey` alone on 2230 and on 22; key login on both. `reload: verified false (no listener changed)` is expected for a change that adds no listener |
| 11 | **pass** | not confirmed: the systemd timer fired at the deadline to the second, restored the absent `00-hardn.conf`, reloaded `ssh.service`, and checked with `ss` that 22 alone listens; 2230 then refused connections, key login on 22 exit 0; `hardn confirm` afterwards: "was rolled back at …, because it was not confirmed within 180s" |
| 12 | **pass** (with 11) | the SSH session that applied the change had ended, and p2drive with it, before the deadline |
| 13 | **pass** | grant: `visudo -c`, `visudo -cf` on the copy in `/run/hardn/<id>/sudoers-validate`, the write (`hardn-grant-alice`, `0440 root:root`), `visudo -c`; no timer; alice's `sudo -n true` exit 0. `--grant none` not confirmed: armed, the file gone, alice's `sudo -n true` refused; at the deadline the timer ran the registered sudoers rollback ("undo 1 of 1: roll back the sudoers plan"), the file came back, `visudo -c` passed, alice's `sudo -n true` exit 0 again. `--grant none` confirmed from a new connection: the file stays gone |
| 14 | **pass** | `--grant password --env HARDN_CONFIG`: armed (lockout risk) and confirmed; `hardn-grant-alice` (`alice ALL=(ALL) ALL`) and `hardn-env-alice` (`Defaults:alice env_keep += "HARDN_CONFIG"`) both `0440`, `visudo -c` passes; alice's `sudo -n true` refused (password grant); with alice's password, `HARDN_CONFIG` reaches the root shell |

Facts this run settled (were **[unverified]**):
- OpenSSH 10.0p2 on Debian 13 prints `permitrootlogin without-password` in `sshd -T`, not `prohibit-password` ([sshd-effective.md](sshd-effective.md) §4 expected `prohibit-password` from 10.x).
- On Debian 13, `systemctl reload ssh.service` makes sshd listen on an added `Port` before `ss` is read, and stop listening on a removed one ([reload.md](reload.md)).
- `systemd-run --on-active=180s --timer-property=AccuracySec=1s` fires within the second, after the session that armed it has gone ([revert.md](revert.md) §4).
- sudo 1.9.16p2's `visudo -cf` accepts the candidate copies, and `env_keep` from a separate drop-in applies ([sudoers.md](sudoers.md) §4).

Found by the run:
- `p2drive sudoers` said `checked with` (empty) and `installed [...]` for a removal: fixed with this record (it now names the whole-configuration check and says written or removed).
- The legacy logger (`[INFO]`, `[ERROR]`) writes colour codes when its output is not a terminal: for p3-output, which replaces it.

##### The ufw pass

Same build and VM, after `apt install ufw` (0.36.2-9; iptables 1.8.11, nf_tables), left inactive with Debian's defaults (`IPV6=yes`, `DEFAULT_INPUT_POLICY="DROP"`). The first pass's state (its `00-hardn.conf`, the two sudoers drop-ins, `/var/lib/hardn`, `/var/backups/hardn`) was moved aside first, which left the VM as `p2-clean`. The runner's connections were IPv4 only: the VM has no global IPv6 address. Order: 1, 3, 5, 11 (with 7 in its window), 6, 9, then `port --ufw enable` twice from an inactive ufw with no rules (reset by hand: `ufw disable`, the two `ufw delete allow`, `00-hardn.conf` moved aside).

| # | Result | Notes |
|---|---|---|
| 1 | **pass** | `ufw: installed true, active false, enabled false, ipv6 true, incoming deny`; both listeners on 22 `allowed (ufw is inactive)` |
| 3 | **pass** | plan: `ufw allow 22/tcp`, then `ufw --force enable` (the stock policy already denies incoming, so no `ufw default` step); armed; `firewall: verified true`; the applying session ran its next command after the enable; `confirm 22` from a new connection: confirmed, the timer stopped; `ufw status verbose`: active, deny (incoming), 22/tcp allowed over IPv4 and IPv6 |
| 4 | not run | no snapshot from the runner: the state after step 3 stood in for `p2-ufw` |
| 5 | **pass** | with ufw active the plan has `ufw allow 2230/tcp` before the backup and the reload; nothing changed |
| 6 | **pass** | `ufw allow 2230/tcp` after the timer and before the backup; `firewall: verified true`; `reload: verified true`; listeners on 22 and 2230 |
| 7 | **pass** | in step 11's window, key login on 2230 and on 22 through the active ufw, both exit 0 |
| 9 | **pass** | `confirm 2230` from a new connection: confirmed, the timer stopped; ufw keeps 22 and 2230 |
| 11 | **pass** | not confirmed: at the deadline, to the second, the timer removed `00-hardn.conf`, ran `ufw delete allow 2230/tcp` (`undo 1 of 1`), reloaded `ssh.service` and read 22 alone in `ss`; ufw active with 22 alone; from the runner `key 2230` timed out (ufw drops it) and `key 22` exit 0; `hardn confirm`: "was rolled back at …, because it was not confirmed within 180s" |
| `port --ufw enable` | **pass** | plan `ufw allow 22/tcp`, `ufw allow 2230/tcp`, then `ufw --force enable`, run in that order after the timer; the journal's undo: `ufw disable`, `ufw delete allow 2230/tcp`, `ufw delete allow 22/tcp`. Not confirmed: at the deadline the timer ran those three (`undo 1 of 3` to `3 of 3`), removed `00-hardn.conf` and reloaded: ufw inactive, `ufw show added` (None), `ENABLED=no`, 22 alone. Run again and confirmed from a new connection to 2230: ufw active, deny (incoming), 22 and 2230 allowed, `ENABLED=yes`; the VM was left so |

Facts this pass settled ([ufw.md](ufw.md) §4, on Debian 13, over IPv4):
- An established SSH connection survives `ufw --force enable`; new connections to the allowed ports are accepted, and a port whose rule is deleted then times out (ufw drops, it does not refuse).
- ufw's answers: `allow` "Rules updated" / "Rules updated (v6)" while inactive, "Rule added" / "Rule added (v6)" while active; `delete allow` "Rules updated" (and v6) while inactive, "Rule deleted" / "Rule deleted (v6)" while active; `--force enable` "Firewall is active and enabled on system startup"; `disable` "Firewall stopped and disabled on system startup". `ufw delete allow N/tcp` ran without a prompt, from the timer's service with no terminal.
- ufw is `/usr/sbin/ufw` on Debian 13, inside `host.SearchPath` ([system.md](system.md) §3).

Not covered by the ufw pass: IPv6 connections, `ufw default deny incoming` on an active ufw, and the rest of [ufw.md](ufw.md) §4 (the rule forms, the partial failures, Ubuntu and Alpine).

#### Ubuntu 26.04, run 2026-10-19

Built from `next/p2-revert-plans` (0761766, the p2drive fix of the Debian run included), linux/amd64. The VM: the Ubuntu 26.04.1 LTS cloud image, OpenSSH 10.2p1 (Ubuntu-2ubuntu3.6), sudo-rs 0.2.13 (with its own `visudo`), `ssh.socket` enabled and active on `0.0.0.0:22` and `[::]:22` with `ssh.service` disabled and socket-activated, `sshd-socket-generator` present and no `ssh.socket` drop-in, the stock `Include` as the first directive (line 24), `60-cloudimg-settings.conf` (`PasswordAuthentication no`), `KbdInteractiveAuthentication no` in the main file, ufw 0.36.2-9build1 installed and inactive, `/usr/local` `root:root 0755`. Runner and VM as in the Debian run; IPv4 only (no global IPv6 address on the VM). No snapshots from the runner, so as on Debian 13 the order lets each rollback leave the state the next step needs: 1, 2, `port --ufw enable` not confirmed (from the inactive ufw), 3, 5, 11 (with 7 in its window, and 12), 6, 8, 9, 10, 13, 14, then §4.5 after moving `00-hardn.conf` aside.

| # | Result | Notes |
|---|---|---|
| 1 | **pass** | Ubuntu 26.04 supported, systemd, `ssh.socket` with the generator, sudo-rs (flavour sudo-rs); `include first, line 24`; `sshd -T` port 22; listeners `[::]:22`, `0.0.0.0:22`, the socket's the same; no drop-ins; `ufw: installed true, active false` |
| 2 | **pass** | `unsupported option "maybe"` at `00-hardn.conf line 4`; nothing written, nothing reloaded |
| `port --ufw enable` | **pass** | plan: `ufw allow 22/tcp`, `ufw allow 2230/tcp`, `ufw --force enable`, the backup, then `daemon-reload`, `check-socket`, `restart ssh.socket`; after it `systemctl show -p Listen ssh.socket` and `ss` show 22 and 2230 on both families, and the generator wrote `addresses.conf`; the applying session kept working across the socket restart; key login on 2230 and 22 through ufw. Not confirmed: the timer removed `00-hardn.conf`, ran `ufw disable` and the two deletes, `daemon-reload`, checked the socket was configured for 22 alone, restarted it and read 22 alone in `ss`; ufw inactive with no rules; the applying session survived that restart too |
| 3 | **pass** | `ufw allow 22/tcp`, then `ufw --force enable`; `confirm 22`: confirmed |
| 4 | not run | the state after step 3 stood in for `p2-ufw` |
| 5 | **pass** | `ufw allow 2230/tcp`, the backup, `daemon-reload`, `check-socket`, `restart ssh.socket`; held 22, added 2230; nothing changed |
| 11, 12 | **pass** | not confirmed, the applying session ended: the timer deleted the 2230 rule, removed `00-hardn.conf`, reloaded the socket onto 22 alone; from the runner `key 2230` timed out, `key 22` exit 0; `hardn confirm`: "was rolled back at …, because it was not confirmed within 180s" |
| 6 | **pass** | the build's sha256; `firewall: verified true`; `reload: verified true`; `ssh.socket` configured and bound on 22 and 2230; **sessions survive `restart ssh.socket`**: the applying connection and one opened before step 6 both kept working |
| 7 | **pass** | key login on 2230 and 22 (in the `port --ufw enable` window, through ufw) |
| 8 | **pass** | all four refused with their reasons: the applying connection; the connection opened before the change; a new connection to the held 22; from the VM to its own 2230 over `[::1]` |
| 9 | **pass** | `confirm 2230` from a new connection: confirmed, no timer left |
| 10 | **pass** | `keyonly`: `systemctl reload ssh.service` (no listener changes, so no socket restart); confirmed from a new connection to 2230; `publickey` alone offered on 2230 and 22; key login on 2230 |
| 13 | **pass** | grant: `visudo -c`, `visudo -cf` on the copy, the write (`0440` root's), `visudo -c`; no timer; `sudo -l -U alice` lists the `NOPASSWD` rule; alice's `sudo -n true` exit 0. `--grant none` not confirmed: armed, the file gone, alice refused ("interactive authentication is required"); the timer's registered sudoers rollback put it back, `visudo -c` passed, alice's `sudo -n true` exit 0. `--grant none` confirmed from a new connection: the file stays gone |
| 14 | **pass** | `--grant password --env HARDN_CONFIG`: armed, confirmed; `hardn-grant-alice` and `hardn-env-alice` `0440`; alice's `sudo -n true` refused; with alice's password `HARDN_CONFIG` reaches the root shell, and a variable not kept does not |

§4.5 (cloud-init's `PasswordAuthentication yes`), after moving `00-hardn.conf` aside and putting the socket back on 22:

| # | Result | Notes |
|---|---|---|
| 1 | **pass** | `50-cloud-init.conf` written: `sshd -T` `passwordauthentication yes`; `offers 22`: `publickey,password` |
| 2 | **pass** | `keyonly`, `confirm 22`: confirmed; `facts`: `shadowed passwordauthentication: /etc/ssh/sshd_config.d/50-cloud-init.conf:1 "PasswordAuthentication yes" (hardn …/00-hardn.conf:4)`, `passwordauthentication "no"`; `offers 22`: `publickey`; `50-cloud-init.conf` unchanged |

Facts this run settled (were **[unverified]**):
- The Ubuntu 26.04.1 cloud image has ufw installed and inactive (§3).
- SSH sessions survive `systemctl restart ssh.socket` (plan §7), which stops and starts `ssh.service` with it (its journal shows both at each restart): the applying connection stayed open across all five (three by p2drive, two by the timer's rollbacks), and a connection opened before step 6 across that one.
- `daemon-reload` reruns `sshd-socket-generator`, which writes `/run/systemd/generator/ssh.socket.d/addresses.conf` for the added `Port` and removes it when `00-hardn.conf` goes; `restart ssh.socket` then binds what `systemctl show -p Listen` says ([reload.md](reload.md)).
- sudo-rs 0.2.13: `visudo -c` and `visudo -cf` on the candidate copies, `sudo -l -U`, and `env_keep` from a separate drop-in all work as they do with sudo (plan §7; [sudoers.md](sudoers.md) §4).
- OpenSSH 10.2p1 on Ubuntu prints `permitrootlogin prohibit-password` (Debian 13's 10.0p2 prints `without-password`).
- ufw on Ubuntu answers as on Debian 13 (the ufw pass above).

Found by the run:
- The timer fired 1 to 2 s after the deadline hardn prints (23:40:41 for 23:40:39): systemd counts the 180 s from when it starts the timer, up to a second after hardn's clock (`list-timers` showed hardn's deadline or a second after it), and `AccuracySec=1s` adds up to another. [revert.md](revert.md) §4 expected "within a second of the deadline". A `hardn confirm` in that gap is accepted (`Confirm` claims the journal without reading the clock; the claim decides between it and the rollback), so nothing is unsafe; the printed deadline is a second or two early. For `p3-engine`'s countdown: count down to the timer's own trigger time, or say "about".
- Step 1's Expect said `line 1` for the stock Include: it is the first directive, below the stock comments (line 12 on Debian 13, 24 on Ubuntu 26.04). Corrected in §4.4.

### 4.10 Reboots inside and after the confirm window (`next/p3-revert-boot`)

**Written, not run (gate 5).** Added by `next/p3-revert-boot` ([revert-boot.md](revert-boot.md); [P-34](../refactor/proposals.md#p-34) (c)) after the runs of §4.9. Build both binaries from that branch or later (§4.1) and install them as §4.1 says. Record the hardn build's `sha256sum`: the journal's `timer.hardnSHA256` must equal it. Every step starts from the snapshot it names. `boot()` below is the boot id, `cat /proc/sys/kernel/random/boot_id`, and `<id>` is the change id the lockout command printed.

On Debian 13 (`ssh.service`), from `p2-ufw`:

| # | Run | Expect | Proves |
|---|---|---|---|
| 1 | `$P2 port --port 2230 --plan-only`; `ls /etc/systemd/system/hardn-boot.service` | the plan ends with `boot unit, before the timer: write /etc/systemd/system/hardn-boot.service (0644, root's; a copy checked first with systemd-analyze verify --recursive-errors=no), then systemctl --no-reload enable hardn-boot.service: after a reboot inside the window it runs `/usr/local/bin/hardn rollback --boot`, …`; nothing changed; no such file | the boot unit is planned and shown, and nothing is installed before the apply |
| 2 | `$P2 port --port 2230 --timeout 10m`; `systemctl cat hardn-boot.service`; `systemctl is-enabled hardn-boot.service`; `stat -c '%a %U:%G' /etc/systemd/system/hardn-boot.service`; `sudo grep -A3 '"deadline"' /var/lib/hardn/pending.json`; `boot()` | the transcript has `revert: boot unit /etc/systemd/system/hardn-boot.service: written (0644, root's)` and `…: enabled (systemctl --no-reload enable hardn-boot.service)` before `revert: armed change <id>`, and no `systemd-analyze` refusal; the unit as [revert-boot.md](revert-boot.md) §2.13 shows it; `enabled`; `644 root:root`; the timer has `"hardn": "/usr/local/bin/hardn"`, `"hardnSHA256"` equal to the build's sum, and `"boot"` equal to `boot()` | installed (checked with the real `systemd-analyze verify`, written, enabled without a `daemon-reload`) before the first arm; the journal names the binary and the boot (revert-boot.md §2.16, §2.11) |
| 3 | about two minutes into the window: `sudo systemctl reboot`. After it: `boot()`; `journalctl -b -u hardn-boot.service`; `systemctl list-timers 'hardn-revert-*'`; `journalctl -b -o short-monotonic -u hardn-boot.service -u ssh.service \| head -20`; `journalctl -b \| grep -i 'ordering cycle'`; from the runner `key 22`, `key 2230` | a new boot id; `revert: boot: change <id> (…): rearmed: its timer is re-armed for what remains of its window, <about 8m>: it is rolled back at <step 2's deadline> unless confirmed …`, and `[SUCCESS] At boot, change <id> …`; `list-timers` lists `hardn-revert-<id>.timer`, LEFT about the same and NEXT at step 2's deadline, a second later at most; `hardn-boot.service` finished before `ssh.service` started; no ordering cycle; both key logins exit 0 | **re-armed at boot for what remains of the window**, never longer, and ordered before sshd (revert-boot.md §2.3, §2.13) |
| 4 | from the runner, before the deadline: `confirm 2230` (a new connection, made after the reboot); `systemctl list-timers 'hardn-revert-*'`; wait past the deadline | confirmed; no `hardn-revert-<id>` timer; nothing rolled back (`grep Port /etc/ssh/sshd_config.d/00-hardn.conf`: 22 and 2230) | **a confirmation from a new connection after the reboot works**, and stops the re-armed timer (revert-boot.md §2.5) |
| 5 | `sudo systemctl reboot`; then `journalctl -b -u hardn-boot.service`; `ls /etc/systemd/system/hardn-boot.service /etc/systemd/system/multi-user.target.wants/hardn-boot.service`; `grep Port /etc/ssh/sshd_config.d/00-hardn.conf`; `systemctl list-timers 'hardn-revert-*'` | `nothing: no change is journalled; the last change confirmed is <id>`, then `boot unit …: disabled and removed`; both files gone; `Port 22` and `Port 2230` still there; no timer | **a confirmed change, then a reboot: nothing happens**, and the boot unit removes itself (revert-boot.md §2.8, §2.17) |
| 6 | revert to `p2-ufw`; `$P2 port --port 2230 --timeout 60s`; once it says "applied", at once `sudo systemctl poweroff`, before the timer fires; keep the VM off for more than 60 s from the arm, then start it. `journalctl -b -o short-monotonic -u hardn-boot.service -u ssh.service`; `sshd -T \| grep ^port`; `sudo ufw status`; `sudo ls /var/lib/hardn`; from the runner `key 2230`, `key 22`; `sudo $BIN/hardn confirm` | `revert: boot: change <id> …: rolled back now: its deadline, …, has passed`, the restore of `00-hardn.conf`, `undo 1 of 1: ufw delete allow 2230/tcp`, `systemctl is-active ssh.service` printing `inactive`, the reload skipped, and no `systemctl start` or `reload`; `rollback <id>: done`; the unit removed; all of it before `ssh.service` started; port 22 alone; no 2230 rule; `reverted.json` and no `pending.json`; `key 2230` refused or timed out, `key 22` exit 0; `confirm`: "was rolled back at …, because it was not confirmed within 60s" | **rolled back at boot once the deadline has passed**, the files back before sshd read them, nothing started from the unit (revert-boot.md §2.4) |
| 7 | revert to `p2-ufw`; `$P2 port --port 2230 --timeout 10m`; install another build of hardn over `$BIN/hardn` (say, built as §4.1 with `-ldflags '-X main.Version=other'` added), `sha256sum $BIN/hardn`; `sudo systemctl reboot`; then `journalctl -b -u hardn-boot.service`; `systemctl --failed`; `systemctl list-timers 'hardn-revert-*'`; from the runner `key 2230`; then install step 2's build again and `confirm 2230` | `left: /usr/local/bin/hardn is not the binary that wrote the journal (sha256 …, then …)`; `hardn-boot.service` failed (exit 1); sshd up; no timer; the confirm with the right build works | **another hardn binary's journal is not acted on at boot** (revert-boot.md §2.2) |
| 8 | by hand, where the hypervisor can set the guest's clock at boot (on Proxmox VE, `qm set <vmid> --startdate <a time before step 2>`, **[unverified]**): step 3 with the clock set back | `rearmed: the clock reads …, before the change was made (…), so what remains of its window cannot be told: its timer is re-armed for the whole window, 10m0s` | an untrusted clock re-arms for the whole window, never longer (revert-boot.md §2.3) |

On Ubuntu 26.04 (`ssh.socket`), steps 1 to 7 the same, from its `p2-ufw`, and in each `-u ssh.socket` beside `-u ssh.service`:

- step 3: `hardn-boot.service` finished before `ssh.socket` started; after the reboot, `systemctl show -p Listen ssh.socket` and `ss -Htln` show 22 and 2230: the generator read the change's files at boot.
- step 6: in the unit's journal, after the restore, `systemctl daemon-reload`, then `systemctl show -p Listen ssh.socket` (22 alone), then `systemctl is-active ssh.socket` printing `inactive`, and no `restart`. Then `ssh.socket` starts and binds 22 alone (`systemctl show -p Listen ssh.socket`, `ss -Htln`). This is the socket path of revert-boot.md §2.4.

On Alpine 3.24 (OpenRC), steps 1 to 6 from its `p2-clean` (no ufw: no firewall step or undo), with `doas` for `sudo`, `/var/log/hardn.log` for the unit's journal, and `rc_logger="YES"` set in `/etc/rc.conf` before the snapshot, for `/var/log/rc.log`:

- step 1: the plan writes `/etc/init.d/hardn-boot (0755, root's; a copy checked first with sh -n)`, then `rc-update add hardn-boot default`.
- step 2: `rc-update show default` lists `hardn-boot`; `ls -l /etc/runlevels/default/hardn-boot` links to `/etc/init.d/hardn-boot`.
- step 3: in `/var/log/rc.log`, `hardn-boot` started before `sshd`. `cat /run/hardn/revert-<id>.pid` names a process whose `tr '\0' ' ' < /proc/<pid>/cmdline` is `/bin/sh -c sleep "$1" && "$0" rollback "$2" /usr/local/bin/hardn <about 480> <id>`: the sleeper, re-armed.
- step 4: `confirm 2230` ends that process and removes the pidfile.
- step 5: the init script and its link are gone. Also record whether the next shutdown prints an error for the removed `hardn-boot` (revert-boot.md §3, **[unverified]**).
- step 6: `rc-service sshd reload` is not run (the unit is not started yet), and `sshd` starts after `hardn-boot` on port 22 alone.

Results go under §4.9's scheme, as one table per VM.

## 5. The images

| Image | Path it covers | Prepared as |
|---|---|---|
| Debian 13 | systemd, `ssh.service`, sudo, ufw | §4.2 |
| Ubuntu 26.04 | systemd, `ssh.socket` with the generator, sudo-rs, ufw | §4.2 |
| Ubuntu 26.04 with `50-cloud-init.conf` `PasswordAuthentication yes` | a cloud drop-in that loses to `00-hardn.conf` | §4.5 |
| Alpine 3.24 | OpenRC, doas, no ufw | §4.2 |
| Alpine 3.24 cloud image | the sshd drop-ins it ships (plan §7) | as the image ships it, §4.6 |
| v0.3.2-hardened Ubuntu 26.04 and Debian 13 | the upgrade path (R8) | §4.8 |
| Proxmox VE 9, two nodes | Proxmox VE's rules, by hand | [pve.md](pve.md) §4 |

Debian 12 and Ubuntu 24.04 (OpenSSH 9.2p1 and 9.6p1) are where sshd-validate.md and sshd-effective.md want their 9.x checks; the same §4.4 steps apply there, without the exemption line.

## 6. Written, not run (gate 5)

All of §4. The units' own lists that this sequence covers, in part: [system.md](system.md) §4 (commands from `SearchPath`; `WriteFileAtomic` and `Chown` as root), [detect.md](detect.md) §4 (stock rows), [atomic.md](atomic.md) §4 (writes and backups as root), [sshd-validate.md](sshd-validate.md) §4, [sshd-effective.md](sshd-effective.md) §4, [reload.md](reload.md) §4 (the 2230 scenario, sessions over a socket restart), [ufw.md](ufw.md) §4 (the 2230 scenario, the enable), [revert.md](revert.md) §4 (the brief's scenario, both timers, the refusals), [pve.md](pve.md) §4 items 1-4 and 6, [sudoers.md](sudoers.md) §4 (sudo, sudo-rs, doas), [sshd-migrate.md](sshd-migrate.md) §4 (R8), [revert-plans.md](revert-plans.md) §4 (the sudoers rollback by the timer, §4.4 step 13; the `listen.conf` rollback and the D9 refusal, §4.8 steps 5 and 6). Not covered by this driver, and so open after it: [alpine.md](alpine.md) §4, [dns.md](dns.md) §4, [pve.md](pve.md) §4 items 5 and 7, the legacy operations of system.md §4 (P-32), the container checks of detect.md §4, and the items of each list beyond these steps.

Plan §7's four items, which the plan puts in this unit: sessions surviving an `ssh.socket` restart (§4.4 step 6) and sudo-rs with `env_keep` on Ubuntu 26.04 (§4.4 step 14) are settled by the Ubuntu 26.04 run (§4.9); Alpine cloud images' sshd drop-ins (§4.6, the cloud image) stay open until such an image is made. The fourth, ufw `reset` with stdin `/dev/null`, is **closed by decision §3.17**, not by a run: `pkg/host/ufw` runs no `ufw reset` (and `ufw disable` only as the undo of its own enable; [ufw.md](ufw.md) §2.7, tested over every plan there), so how `reset` reads its stdin no longer matters to the new code. The legacy firewall step still runs `disable` then `reset` until the firewall slice replaces it, gated meanwhile by `next/p0-hazards` (ufw.md §6).

## 7. API (the command line)

```
p2drive --i-am-on-a-disposable-vm <command> [flags]     (HARDN_P2DRIVE_DISPOSABLE=yes, as root, on Linux)

facts                     read-only report
broken                    a broken 00-hardn.conf must be refused; changes nothing
port       --port N       (default 2230)  [--ufw keep|enable]  + the lockout flags
keyonly                   PasswordAuthentication no, KbdInteractiveAuthentication no  [--ufw …] + the lockout flags
pve-root                  PermitRootLogin prohibit-password, Proxmox VE only  [--ufw …] + the lockout flags
ufw-enable                allow the SSH listeners, default deny incoming, enable  + the lockout flags
migrate    [--apply] [--config-file PATH]  + the lockout flags (without --apply: a dry run)
sudoers    --user NAME [--grant nopasswd|password|none] [--env A,B] [--tool sudo|doas]  + the lockout flags
           (a timer only for a lockout-risk plan: --grant none removing a grant, --grant password)

lockout flags: --timeout D (180s; at least 60s)  --hardn PATH (/usr/local/bin/hardn)
               --backup-path P (/var/backups/hardn)  --plan-only
--ufw keep:    allow the ports the change adds in an active ufw; nothing in an inactive one (the default)
--ufw enable:  also default deny incoming and enable, after the allows
               (no mode leaves an active ufw unread: a port moved behind it would lock the user out)

exit 0 done (broken: refused as expected; a lockout change: applied, waiting for `hardn confirm`)
exit 1 failed (a lockout change: rolled back, or the rollback's own error)
exit 2 a gate does not hold, or a usage error
```

**Size:** about 1,710 lines of non-test Go (much of it doc comments and the transcript's lines) and 1,280 of tests, against the plan's guideline of about 500: eight commands over nine primitives, the engine's write-ahead order with its failure path, and on the fake a test per gate, per scenario (Debian, Ubuntu's socket, Alpine's OpenRC, Proxmox VE, the v0.3.2 migration) and per failure after the timer is armed, with a model of sshd's reading for `sshd -T` (the review round after the first commit added about 270 lines of code and 710 of tests). The commands share one planning and one apply path; splitting them (the read-only report, the lockout changes, sudoers and the migration) would have kept each unit nearer the guideline.
