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

3. **Every refusal comes before the revert timer is armed.** A lockout-risk command reads detect's facts (`detect.Detect`), the session it runs in (`revert.ReadSession`), Alpine's sshd binary (`reload.OpenRCFlavour`) and the installed configuration (`sshd.ReadEffective`); then it plans: the Include mode, the legacy files (`migrate.Find`), Proxmox VE's rules (`pve.Applies`, `pve.CheckPort`, `pve.PlanSSH`), the new `00-hardn.conf`, its validation in context (`sshd.Validate` over a copy in `/run/hardn/<id>`), the reload (`reload.PlanFor`) and the firewall (`ufw.Read`, `ufw.PlanFor`). It prints the plan; `--plan-only` stops there. Refused before the timer, with nothing written outside the run directory (tested: `TestPortRefusedBeforeArm`, a port change on Proxmox VE):
   - a port change on Proxmox VE (`pve.ErrPortChange`, §3.18);
   - an `sshd_config` whose first directive is not exactly the stock Include (no write strategy: [P-38](../refactor/proposals.md#p-38)), or that has no Include (hardn's block at the top of `sshd_config` is not something this driver writes);
   - legacy hardn SSH files on the host (`manage.conf`, `hardn.conf`, `listen.conf`, a generated Alpine `sshd_config`): `migrate` comes first;
   - listeners that are not established (`reload.ErrListenersUnknown`), a port sshd already has, and whatever the primitives refuse ([sshd-validate.md](sshd-validate.md), [reload.md](reload.md), [ufw.md](ufw.md)).
4. **`00-hardn.conf` is edited line by line, never regenerated** ([sshd-migrate.md](sshd-migrate.md) §6): the lines of the keywords the change sets are dropped and the new ones come first, after the file's leading comments, so they are global; every other line stays as it was. A file with a `Match` or `Include` line, and an argument that would need quoting, are refused (`fs.ErrInvalid`). Tested: `TestSetDirectives`.
5. **The port change holds the old port.** The Port lines are `reload.HoldPorts` of the listeners sshd configures now and the new port, so during the confirm window `00-hardn.conf` has both `Port 22` and `Port 2230` ([reload.md](reload.md) §2.5). On Ubuntu 24.04 and 26.04 the reload is `reload.PlanFor`'s socket path: `systemctl daemon-reload`, `systemctl show -p Listen ssh.socket` checked for every listener, then `systemctl restart ssh.socket`. Dropping port 22 after the confirmation (brief 2.3 step 5) is a change of its own, which this driver does not make ([revert.md](revert.md) §7): after a confirmation both ports stay open and allowed.

### Applied under confirm-or-revert

6. **In the engine's order, journalled write-ahead** ([revert.md](revert.md) §7): `revert.Arm` (the journal, then the timer: `systemd-run` on systemd, a `setsid` sleeper on OpenRC); the firewall first (brief 2.3 step 2): `ufw.Plan.PreUndo()` journalled, `ufw.Execute`, then its `Report.Undo` journalled; each file backed up (`backup.Run.Backup`, `RollbackRequired`) and the backups and the `ssh` reload (with `IfUp`) journalled before the write; the write, only once the backup shows the file as the plan read it; `reload.Execute`, and the reload journalled again without `IfUp` once its gated step ran; `sshd -T` read again and checked for the values the change set (an Unknown is not a pass; on Proxmox VE, `pve.CheckSSH` must show root key-only); then `revert.MarkApplied`. Tested: `TestPort2230` pins that order on the fake (armed, then `ufw allow 2230/tcp`, then the backup, then the journal, then the write, then the reload) and the journal it leaves; `TestUFWEnable` pins ufw's allow before its enable, from an inactive ufw, and the undo journalled for both (`ufw disable`, then `ufw delete allow 22/tcp`).
7. **A step that fails rolls the change back at once**, while the session is open: `revert.Rollback` with `ReasonFailed`, which runs without the command's cancellation. Ctrl-C and `SIGTERM` cancel the command and so roll back too. A hangup ends the process: the timer, armed before the first step, rolls the change back at the deadline. Tested: `TestPortRollsBack` (a reload sshd refuses: the file is removed, the ufw rule deleted, sshd reloaded, the timer stopped, the journal cleared).
8. **The timer runs hardn, not the driver** ([revert.md](revert.md) §7): `--hardn` (default `/usr/local/bin/hardn`) is the binary `systemd-run` or the sleeper runs as `hardn rollback <id>`, checked by `revert.CheckExecutable` (root's, not a symlink, no group- or world-writable directory above it). The confirmation is `hardn confirm`; the undo before the deadline is `hardn rollback <id>`. Both go through `cmd/hardn`'s `main()`, which writes `/var/log/hardn.log` and `/etc/hardn/hardn.yml.example` before it parses flags (V26, `p3-startup`): accepted on a disposable VM.

### The other commands

9. **`broken`** validates `00-hardn.conf` with `PermitRootLogin maybe` added and requires `sshd.Validate` to refuse it as a configuration error (`*sshd.ConfigError`, its problems naming the real file), with `00-hardn.conf` as it was and nothing reloaded. Tested: `TestBroken`.
10. **`sudoers`** chooses sudo, sudo-rs or doas from detect's facts (`sudoers.Choose`), plans the grant (`sudoers.Read`, `sudoers.PlanFor`), backs up each file it changes and installs it with `sudoers.Apply`: `visudo -c`, `visudo -cf` on a root-only copy (or `doas -C`), the write, `visudo -c` again, the old files put back on failure. Tested: `TestSudoers`. **It is not journalled**: `cmd/hardn`'s `revertPlans` registers no plan rollback yet ([revert.md](revert.md) §7), so a `sudoers` `OpPlan` step would be refused (`revert.ErrNoPlanRollback`) and `hardn rollback` could not undo it. Its undo is `--grant none`, which removes hardn's files for the user.
11. **`migrate`** is a dry run by default: `migrate.Find`, `migrate.PlanFor`, `migrate.Check` (validation and the login-path comparison over a copy) and the reload's plan. `--apply` carries it out under confirm-or-revert as items 6 to 8 say, every file of `Plan.Sources` backed up first. **It refuses `--apply` on a socket-activated host when the plan removes `listen.conf`:** the rollback would restore `listen.conf`, then plan the reload with it in place, which `reload.PlanFor` refuses (`reload.ErrSocketDropIn`, probed against `reload.PlanFor` for this unit), so the rollback could not finish. That is an input to Phase 3 ([README.md](README.md)).
12. **`facts`** is read-only: detect's facts, the Include mode, the legacy files, `sshd -T`'s values, listeners, findings and conflicts, `ssh.socket`'s drop-ins, the bound sockets (`ss -Htln`), ufw's state and its coverage of the listeners, on Proxmox VE `pve.CheckSSH`, `pve.ReadFirewall` and root's keys link, the sudoers choice and the change journal's notices. A read that fails is printed and the report goes on.
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
| OpenSSH per release: Debian 13 and Proxmox VE 9 10.0p1, Ubuntu 26.04 10.2p1, Alpine 3.24 10.3_p1 (all ≥ 9.8, so penalties are on); Debian 12 9.2p1 and Ubuntu 24.04 9.6p1 (before 9.8: no such keyword) | [sshd-effective.md](sshd-effective.md) §3 (the audit's research §1 **[V]**) | §4.2: where the exemption line is valid |
| Ubuntu 24.04/26.04 are socket-activated; a port change needs `daemon-reload` and `restart ssh.socket`; Debian 13 and Proxmox VE 9 run `ssh.service`; Alpine runs OpenRC's `sshd` | [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9) **[V]**; [reload.md](reload.md) §3 | §2.5, §4.4 |
| That sessions survive `systemctl restart ssh.socket`, and that a reload of `ssh.service` or OpenRC's sshd rebinds a changed Port | **[I]** ([reload.md](reload.md) §3, §4) | §4.4, §4.6: checked there |
| Proxmox VE: no SSH port change (§3.18); `PermitRootLogin yes` written by Proxmox VE into `sshd_config`, which the drop-in's `prohibit-password` beats; root's `authorized_keys` a link to `/etc/pve/priv/authorized_keys` | [pve.md](pve.md) §3 | §4.7 |
| An Ubuntu ISO install without imported keys gets `50-cloud-init.conf` with `PasswordAuthentication yes` | [sshd-effective.md](sshd-effective.md) §3 (research §3b, §6 **[V]**) | §4.5 |
| v0.3.2's `manage.conf` and `listen.conf`, verbatim, are `pkg/host/sshd/migrate/testdata/legacy/v032-manage.conf` and `v032-listen.conf`; v0.3.x's keys are in `~/.ssh_<user>/authorized_keys` | [sshd-migrate.md](sshd-migrate.md) §3 (v032-reference.md §4.2 **[V]**; side-effects.md D7) | §4.8: the R8 image |
| That OpenSSH 10.x still accepts `manage.conf`'s `Protocol 2` and `HostbasedAcceptedKeyTypes` lines | **[unverified]**; `migrate.Check` runs `sshd -t` over them first, so a refusal shows in the dry run | §4.8 |
| The release builds use `CGO_ENABLED=0`, `-trimpath` and `-tags=netgo` | `.slsa-goreleaser/linux-amd64.yml`, `linux-arm64.yml` (this repo) | §4.1: the cross-build |
| `ss` (iproute2) is on a stock Debian, Ubuntu and Proxmox VE and not on a stock Alpine; without it the reload is `Unverified` (`no-ss`), not failed | **[unverified]** ([reload.md](reload.md) §3, §2.10) | §4.2: iproute2 on Alpine |
| ufw is in Debian and Ubuntu, and in Alpine 3.24's community repository | [ufw.md](ufw.md) §3 (sources.debian.org; aports); whether a stock Ubuntu 26.04 has it installed and inactive is **[unverified]** | §4.2 |
| sudo's `env_reset` drops the caller's variables, and `sudo env NAME=value cmd` runs `env` as the command, which sets it for `cmd`; doas and sudo-rs the same | sudoers(5) for `env_reset` ([revert.md](revert.md) §3); for `env` as the command, and for doas and sudo-rs, **[unverified]** | §4.3: how the gate variable reaches the driver |

## 4. What to run, in order (written, not run)

The plan names server-side scripts under `test/vm/`. The `.gitignore` allow-list re-includes no shell script there (it would need a line such as `!/test/vm/*.sh`, which is the maintainer's to add), so none is committed, and the commands are given inline below instead; each block can be pasted as it is.

Synthetic names below: the VM is `web-01.example.net` at `192.0.2.20`, the runner (another machine, the maintainer's workstation or a second VM) is `192.0.2.10`, the user is `alice`. Substitute the real values when running; do not commit them.

### 4.1 Build and copy (on the development machine)

Never run hardn or p2drive here (plan §1); p2drive refuses macOS anyway.

```sh
cd hardn-p2   # the worktree on next/p2-vmtest or later
out=$(mktemp -d)
for arch in amd64; do   # arm64 for an aarch64 VM
  GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=$arch \
    mise exec -- go build -trimpath -tags=netgo -o "$out/hardn-linux-$arch" ./cmd/hardn
  GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=$arch \
    mise exec -- go build -trimpath -tags=netgo -o "$out/p2drive-linux-$arch" ./internal/tools/p2drive
done
scp "$out/hardn-linux-amd64" "$out/p2drive-linux-amd64" alice@web-01.example.net:/tmp/
```

On the VM, as root (installed root's, so that `revert.CheckExecutable` accepts hardn for the timer):

```sh
install -o root -g root -m 0755 /tmp/hardn-linux-amd64 /usr/local/bin/hardn
install -o root -g root -m 0755 /tmp/p2drive-linux-amd64 /usr/local/bin/p2drive
```

On a Debian host with `/etc/staff-group-for-usr-local`, `/usr/local` is `2775 root:staff` and the timer refuses hardn there ([revert.md](revert.md) §2.8): install both in `/usr/sbin` instead and pass `--hardn /usr/sbin/hardn`.

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
P2="sudo env HARDN_P2DRIVE_DISPOSABLE=yes /usr/local/bin/p2drive --i-am-on-a-disposable-vm"   # doas on Alpine
```

Keep the transcript of each command (`$P2 … 2>&1 | tee /root/p2-<step>.log`).

### 4.3 The runner's checks

From the runner, another machine (a connection from the VM itself never confirms, [revert.md](revert.md) §2.11):

```sh
key()   { ssh -o BatchMode=yes -o ConnectTimeout=5 -p "$1" alice@web-01.example.net true; echo "key login on $1: exit $?"; }
offers(){ ssh -v -o BatchMode=yes -o ConnectTimeout=5 -o PubkeyAuthentication=no \
            -o PreferredAuthentications=password,keyboard-interactive -p "$1" alice@web-01.example.net true 2>&1 |
          grep 'Authentications that can continue'; }
confirm(){ ssh -t -p "$1" alice@web-01.example.net sudo /usr/local/bin/hardn confirm; }   # doas on Alpine
```

`key 2230` exit 0 is a key login on 2230. `offers 2230` printing `publickey` alone is sshd refusing every password method on 2230: password login is rejected. "Connection refused" or a timeout from `key 2230` after a rollback is the port closed again.

### 4.4 Debian 13 (`ssh.service`), then Ubuntu 26.04 (`ssh.socket`)

The same steps on each; the socket-specific checks are marked.

| # | Run | Expect | Proves |
|---|---|---|---|
| 1 | `$P2 facts` | the Debian 13 (Ubuntu 26.04) row of [detect.md](detect.md) §3's table with no `err`; `include: first`; no legacy files; `sshd -T now`: port [22], listeners known; ufw installed, inactive | detect.md §4, sshd-validate.md §4 (stock), sshd-effective.md §4 (stock), ufw.md §4 (reading) |
| 2 | `$P2 broken` | `refused: /etc/ssh/sshd_config.d/00-hardn.conf line 3: …`; exit 0; `ls /etc/ssh/sshd_config.d` unchanged | **a broken configuration is rejected before it takes effect** (brief); sshd-validate.md §4 (the real messages, mapped to the real file) |
| 3 | `$P2 ufw-enable`; then from the runner within 180 s: `confirm 22` | the plan lists `ufw allow 22/tcp` before `ufw --force enable`; the applying session survives the enable; `confirm 22` says confirmed; `ufw status verbose`: active, deny incoming, 22 allowed | UFW's allow before its enable, under confirm-or-revert (ufw.md §4, the session surviving; revert.md §4, confirming) |
| 4 | snapshot `p2-ufw` | | |
| 5 | `$P2 port --port 2230 --plan-only` | the plan: `ufw allow 2230/tcp`; `00-hardn.conf` with `Port 22` and `Port 2230`; the reload (Ubuntu: `daemon-reload`, `check-socket`, `restart ssh.socket`); held 22, added 2230; nothing changed | the plan and every refusal come before the timer |
| 6 | `$P2 port --port 2230` | armed; `firewall: verified true`; `reload: verified true`; `sshd -T after: port [22 2230]`; "applied"; the applying session still works. Ubuntu: `systemctl show -p Listen ssh.socket` and `ss -Htln` show 22 and 2230 | the port change in brief 2.3's order with the old port held (reload.md §4, the 2230 scenario; sshd-effective.md §4, the confirm window); Ubuntu: **sessions survive `restart ssh.socket`** (plan §7) |
| 7 | from the runner: `key 2230`, `key 22` | both exit 0 | **key login on 2230 succeeds**; 22 is held until the change is confirmed |
| 8 | refusals, each from the runner or the VM as said: `sudo /usr/local/bin/hardn confirm` in the applying session; in a second session from the runner, opened before step 6; `confirm 22` (the held port); on the VM `ssh -t -p 2230 alice@localhost sudo /usr/local/bin/hardn confirm` | each refused, saying why (`ErrSameSession`, `ErrOldConnection`, `ErrWrongListener`, `ErrLocalConn`) | only a new connection from another machine to the new port confirms (revert.md §4, confirming) |
| 9 | from the runner, before the deadline: `confirm 2230` | "confirmed"; `systemctl list-timers` no longer lists `hardn-revert-<id>`; the deadline passes with nothing rolled back | **`sudo hardn confirm` from a new session on the new port cancels the revert** (brief) |
| 10 | `$P2 keyonly`, then from the runner `confirm 2230`, then `offers 2230`, `offers 22`, `key 2230` | confirmed; `offers` prints `publickey` alone on both ports; `key 2230` exit 0 | **password login is rejected** (brief), after key login was confirmed (brief 2.3 step 6) |
| 11 | revert to `p2-ufw`; `$P2 port --port 2230`; do not confirm; after the deadline (180 s; `journalctl -u hardn-revert-<id>` to follow) | the timer's `hardn rollback <id>` ran: `00-hardn.conf` gone, `ufw status` without 2230, `sshd -T` port 22 alone; Ubuntu: `ssh.socket` on 22 alone; from the runner `key 2230` refused or timed out, `key 22` exit 0; `sudo /usr/local/bin/hardn confirm` then says "was rolled back at …, because it was not confirmed within 180s" | **an unconfirmed change reverts automatically** (brief); revert.md §4 (systemd: the timer within a second of the deadline, after the session that armed it) |
| 12 | revert to `p2-ufw`; `$P2 port --port 2230`; close the applying session; after the deadline, reconnect on 22 | rolled back as in 11 | the rollback survives the end of the SSH session and of p2drive (revert.md §4) |
| 13 | revert to `p2-clean`; `$P2 sudoers --user alice --grant nopasswd`; `sudo -l -U alice`; as alice `sudo -n true`; `$P2 sudoers --user alice --grant none` | `visudo -c`, `visudo -cf` on the copy, the write, `visudo -c`; `hardn-grant-alice` `0440` root's; `sudo -n true` exit 0; then the file gone | the sudoers grant checked with visudo before it takes effect (sudoers.md §4; Ubuntu 26.04: sudo-rs's visudo) |
| 14 | `$P2 sudoers --user alice --grant password --env HARDN_CONFIG`; as alice, `HARDN_CONFIG=/tmp/x sudo sh -c 'echo "$HARDN_CONFIG"'` (alice's password asked) | `/tmp/x` printed; `hardn-env-alice` holds `Defaults:alice env_keep += "HARDN_CONFIG"` | **sudo-rs keeps `env_keep`** on Ubuntu 26.04 (plan §7; sudoers.md §4) |

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
- step 13 with doas: `/etc/doas.d/hardn-alice.conf` `0640`, `doas -C` on the copy, `doas -n true` as alice (sudoers.md §4).

### 4.7 Proxmox VE 9 (by hand; plan `p2-vmtest`, adjustment §3.18)

On one node of a two-node test cluster, as root, with the node's web console shell open before any change ([pve.md](pve.md) §4 is the full checklist; these are the plan's six items):

| # | Run | Expect | Proves |
|---|---|---|---|
| 1 | `readlink /root/.ssh/authorized_keys`; `$P2 facts` | `/etc/pve/priv/authorized_keys`; `proxmox: installed yes`, `pmxcfs yes`; `root's keys: … -> /etc/pve/priv/authorized_keys`; ufw: not applicable | detection; the link as Proxmox VE keeps it |
| 2 | `$P2 port --port 2230` | refused before anything changed: "cluster SSH and pve-firewall's management rule use port 22"; no `systemd-run`, nothing written | **the port change is refused** |
| 3 | `$P2 pve-root`; from the other node `ssh root@<node> /usr/local/bin/hardn confirm` (a new connection, from another machine) | `sshd -T after: permitrootlogin "prohibit-password"` while `sshd_config` still says `PermitRootLogin yes`; `Proxmox VE: root key-only yes, root by key yes, port 22 yes` and a `proxmox-root-login` finding; confirmed | **prohibit-password is effective over Proxmox VE's yes** |
| 4 | from the other node: `ssh -o BatchMode=yes root@<node> true`; `ssh -o BatchMode=yes -o PubkeyAuthentication=no -o PreferredAuthentications=password root@<node> true` | exit 0; then refused | **root key login on 22; password rejected** |
| 5 | `readlink /root/.ssh/authorized_keys`; `ls -la /etc/pve/priv/` | the same link; no `.authorized_keys.hardn-*` left | **the `authorized_keys` link is intact** (R15) |
| 6 | restore `PermitRootLogin` by removing `00-hardn.conf` and reloading, then `$P2 pve-root` and do not confirm | after 180 s the timer's `hardn rollback <id>` restores the files and reloads `ssh.service`: `sshd -T` shows `permitrootlogin yes` again | **the revert timer for a change that moves no port** |

### 4.8 The v0.3.2-hardened image (R8)

Built from `p2-clean` of the Ubuntu 26.04 VM and, separately, of the Debian 13 VM, with v0.3.2's files as the migrate fixtures reproduce them (§3): copy `pkg/host/sshd/migrate/testdata/legacy/v032-manage.conf` to `/etc/ssh/sshd_config.d/manage.conf` and `v032-listen.conf` to `/etc/systemd/system/ssh.socket.d/listen.conf`, both `0644` root's; put alice's key in `~alice/.ssh_alice/authorized_keys` (directory `0700`, file `0600`, alice's) and remove `~alice/.ssh/authorized_keys`, so that only the legacy key file lets her in; reboot (v0.3.x never ran `daemon-reload`, D9: the reboot is what makes `listen.conf` take effect). After it sshd listens where v0.3.2 put it: on Debian `manage.conf`'s `ListenAddress 0.0.0.0:2230` alone, on Ubuntu `ssh.socket` on `listen.conf`'s 2230; open the applying session and the runner's checks on 2230. Snapshot `p2-v032`. (Installing the v0.3.2 release binary and running its SSH hardening there gives the same files; that is the maintainer's choice.)

| # | Run | Expect | Proves |
|---|---|---|---|
| 1 | `$P2 facts` | `legacy: manage.conf (v0.3)`, `legacy: listen.conf (v0.3)`; Ubuntu: `ssh.socket` on 2230 and a `sources-disagree` finding | `migrate.Find` and sshd-effective on a real upgrade path |
| 2 | `$P2 port --port 2231` | refused: "legacy hardn SSH files …: run `p2drive migrate` first" | no change on top of an unmigrated host |
| 3 | `$P2 migrate` | the steps (write `00-hardn.conf` with the moved lines and `AuthorizedKeysFile .ssh/authorized_keys .ssh_%u/authorized_keys`, remove `manage.conf`, remove `listen.conf`), `migrated (a copy)` values, the reload plan; nothing changed | sshd-migrate.md §4 (the plan with the real `systemctl show -p Listen`; `Check` in a dry run) |
| 4 | Ubuntu: `$P2 migrate --apply` | refused: its rollback's reload would be refused (`ErrSocketDropIn`) | the rollback gap stays a refusal, not a half-undone change (§2.11) |
| 5 | Debian: `$P2 migrate --apply`; from the runner `key 2230` (the key in `~/.ssh_alice`), then `confirm 2230` | applied; `ssh.service` reloaded; key login on 2230 with the legacy key file; confirmed | **R8: a v0.3.2 host keeps its key login through the migration**; the inert `listen.conf` removed (sshd-migrate.md §4, Debian) |

### 4.9 The results

For each VM: the transcripts, `journalctl -u 'hardn-revert-*'` (systemd) or `/var/log/hardn.log`, and for each step whether it showed what the table expects. Every "Expect" that does not hold is a finding for the unit whose page the "Proves" column names. Until then every row stays "written, not run".

## 5. The images

| Image | Path it covers | Prepared as |
|---|---|---|
| Debian 13 | systemd, `ssh.service`, sudo, ufw | §4.2 |
| Ubuntu 26.04 | systemd, `ssh.socket` with the generator, sudo-rs, ufw | §4.2 |
| Ubuntu 26.04 with `50-cloud-init.conf` `PasswordAuthentication yes` | a cloud drop-in that loses to `00-hardn.conf` | §4.5 |
| Alpine 3.24 | OpenRC, doas, no ufw | §4.2 |
| v0.3.2-hardened Ubuntu 26.04 and Debian 13 | the upgrade path (R8) | §4.8 |
| Proxmox VE 9, two nodes | Proxmox VE's rules, by hand | [pve.md](pve.md) §4 |

Debian 12 and Ubuntu 24.04 (OpenSSH 9.2p1 and 9.6p1) are where sshd-validate.md and sshd-effective.md want their 9.x checks; the same §4.4 steps apply there, without the exemption line.

## 6. Written, not run (gate 5)

All of §4. The units' own lists that this sequence covers, in part: [system.md](system.md) §4 (commands from `SearchPath`; `WriteFileAtomic` and `Chown` as root), [detect.md](detect.md) §4 (stock rows), [atomic.md](atomic.md) §4 (writes and backups as root), [sshd-validate.md](sshd-validate.md) §4, [sshd-effective.md](sshd-effective.md) §4, [reload.md](reload.md) §4 (the 2230 scenario, sessions over a socket restart), [ufw.md](ufw.md) §4 (the 2230 scenario, the enable), [revert.md](revert.md) §4 (the brief's scenario, both timers, the refusals), [pve.md](pve.md) §4 items 1-4 and 6, [sudoers.md](sudoers.md) §4 (sudo, sudo-rs, doas), [sshd-migrate.md](sshd-migrate.md) §4 (R8). Not covered by this driver, and so open after it: [alpine.md](alpine.md) §4, [dns.md](dns.md) §4, [pve.md](pve.md) §4 items 5 and 7, the legacy operations of system.md §4 (P-32), the container checks of detect.md §4, and the items of each list beyond these steps.

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
sudoers    --user NAME [--grant nopasswd|password|none] [--env A,B] [--tool sudo|doas] [--backup-path P] [--plan-only]

lockout flags: --timeout D (180s; at least 60s)  --hardn PATH (/usr/local/bin/hardn)
               --backup-path P (/var/backups/hardn)  --plan-only
--ufw keep:    allow the ports the change adds in an active ufw; nothing in an inactive one (the default)
--ufw enable:  also default deny incoming and enable, after the allows
               (no mode leaves an active ufw unread: a port moved behind it would lock the user out)

exit 0 done (broken: refused as expected; a lockout change: applied, waiting for `hardn confirm`)
exit 1 failed (a lockout change: rolled back, or the rollback's own error)
exit 2 a gate does not hold, or a usage error
```

**Size:** about 1,440 lines of non-test Go (much of it doc comments and the transcript's lines) and 570 of tests, against the plan's guideline of about 500: eight commands over nine primitives, the engine's write-ahead order with its failure path, and a test per gate and per scenario on the fake. The commands share one planning and one apply path; splitting them (the read-only report, the lockout changes, sudoers and the migration) would have kept each unit nearer the guideline.
