# Phase 0 appendix: side-effect inventory and lockout assessment

> **Part of:** `docs/refactor/phase0-findings.md` (§7). This appendix holds the full tables that the findings summarise.
> **Refs:** origin/main 9d46866 (the baseline), with v0.3.2 (bce7cc2) deltas.
> **Status:** draft for the Phase 0 review, 2026-10-16.
> **Labels:** **[V]** verified (code read, harness output or command output seen). **[I]** inferred from code plus documented OS or tool behaviour; needs a VM to confirm.

## 0. Scope, baseline and method

### 0.1 Baseline (maintainer decision 6)

- **origin/main is the baseline** for wording and behaviour.
- **The V-list (§8) is a list of defects to fix**, not behaviour to preserve. §8 re-labels each item's fix type accordingly.
- **v0.3.2 is the reference only where origin/main regressed.** Its full argv and file-content table is `v032-reference.md`.

### 0.2 Method

- **Static audit:** every adapter, manager, menu handler and legacy package that writes a file or runs a command was read. Grep sweeps covered `exec.Command|Execute(|WriteFile|MkdirAll|Remove|Rename|DryRun`.
- **Dynamic characterization, scratch only:**
  - A Go harness wired the real `infrastructure.ServiceFactory` and managers to the repo's own `interfaces.MockFileSystem` and `MockCommander`.
  - It set `cfg.DryRun = true` and recorded every command "executed" and every file "written". **Nothing ran for real.**
  - Fixtures are synthetic (user `alice`, example keys).
- **Adversarial verification:** a second pass re-tested 15 safety claims (A1–A15) in fresh harnesses. 14 were confirmed as stated; A10 (update checker) was confirmed with a narrower keyword match. Where the verification differs from the first pass, the verification wins, and the tables below already include its corrections.
- **One disclosure:** during the first pass, one lane ran a scratch-built origin/main binary as a non-root user with only `--help` and `--version`. Its root-only writes failed with EACCES and nothing was written. **The Phase 0 ground rules now forbid running any hardn binary**, including `--help`.
- **Not verifiable on the audit machine:** real OS behaviour such as `ufw reset` without `--force`, Ubuntu `ssh.socket`, Alpine locked accounts, and restart on a broken config. These stay **[I]** until they run on the disposable VM (gate 5). See §11.

---

## 1. Headline findings (most severe first)

1. **[V] `--dry-run` is ignored for every CLI operation flag** (`-r -c -d -w -g -e`).
   - `main.go:152` sets `cfg.DryRun`; nothing below the menu layer reads it. There are 0 functional `DryRun` hits in `pkg/{application,domain,adapter,port,infrastructure,interfaces}`.
   - Harness with `DryRun=true`: `HardenSystem` (`-r`) wrote `/etc/ssh/sshd_config.d/hardn.conf`, `/etc/sudoers.d/alice` and `/etc/systemd/resolved.conf`, and ran 17 commands, including `systemctl restart ssh` (twice), `ufw reset`, `sh -c "yes | ufw enable"` and `systemctl restart systemd-resolved`.
   - v0.3.2 honoured `-n` through per-function checks (10 packages).
2. **[V] "Disable root SSH" rebuilds the whole SSH config from a stub.**
   - `GetSSHConfig()` returns `&model.SSHConfig{Port: 22}` (`file_ssh_repository.go:127-131`).
   - Result: `Port 22`, no `ListenAddress`, no `AllowUsers`, `AuthenticationMethods publickey`, `PasswordAuthentication no`.
   - On Debian/Ubuntu/Proxmox this goes into the `hardn.conf` drop-in (the main file is untouched). **On Alpine it replaces the whole `/etc/ssh/sshd_config`**, which also drops `Subsystem sftp`, so sftp breaks [V harness].
   - A host on a non-default port whose firewall allows only that port gets sshd moved back to 22 [I outcome].
3. **[V] `hardn -c -u <user>` writes a global `AuthorizedKeysFile .ssh_%u`** (`main.go:373` passes `cfg.SshKeyPath`, default `.ssh_%u`, `config.go:117`).
   - The keys themselves go to `~/.ssh/authorized_keys`, and the same file sets `PasswordAuthentication no` and `AuthenticationMethods publickey`.
   - Per sshd_config(5), `%u` expands to the user name and relative paths are taken from home, so sshd reads the file `~/.ssh_<user>`. **Every key user, root included, is locked out** of new connections.
   - v0.3.2 wrote `AuthorizedKeysFile .ssh/authorized_keys %s/authorized_keys` (both paths).
4. **[V] No SSH change is validated or reload-safe on any ref.** There is no `sshd -t`, `sshd -T`, reload, `os.Rename`, `daemon-reload` or `00-hardn` in any of the 15 refs, the 37 stashes (and their parents) or the 215 reflog commits.
   - For this appendix the 24 dropped stashes (W and I) and the 5 reflog-only commits were searched the same way: no hits. Their only `ssh.socket` hits are v0.3.x's `listen.conf` writer in the 2025-03-05 snapshots.
   - Every SSH write ends in a restart. The disable-root menu restarts twice.
5. **[V] Key-only auth is forced with no check that any key works.** `HardenSystem` hard-codes `PermitRootLogin no` ("Never allow root login", `security_manager.go:49`) and ignores `permitRootLogin` / `disableRootSSH`.
6. **[V] The firewall can lock you out in three ways:**
   - (a) it allows only `cfg.SshPort`, never the port sshd actually listens on;
   - (b) it runs `ufw disable`, then `ufw reset`, before adding the allow rule. Any error after `disable` leaves UFW off with no rollback. `reset` without `--force` reads stdin; with stdin=/dev/null upstream ufw treats EOF as "Aborted" and still exits 0, so the reset is **probably a silent no-op** [I, from ufw source; untested on a host];
   - (c) Run All moves the SSH port before the firewall step, and that step runs only when `enableUfwSshPolicy` is true (default false).
7. **[V] No Proxmox gating.** The harness on a Proxmox fixture shows `ufw … enable` and `PermitRootLogin no`. `IsProxmox` gates only package code that can't be reached.
8. **[V] On Alpine, origin/main never installs `sudo`** (v0.3.2 did). The new user goes into `wheel` with a sudoers file, but there may be no sudo binary, while Run All sets `PermitRootLogin no`.
9. **[V] Nothing is backed up.** `BackupFile` has no callers; `RestoreBackup` has no caller and no UI. v0.3.2 backed up before 13–15 writes (and ignored errors).
10. **[V] Every write is non-atomic.** `OSFileSystem.WriteFile` = `MkdirAll(dir, 0755)` + `os.WriteFile`: truncates in place, follows symlinks, applies the mode only on create, no fsync, no rename.
11. **[V] Sudoers grants are written without `visudo`**, named after the raw user name (sudo silently skips names containing `.` [I, sudoers(5)]), and **overwrite** the `env_keep` line that `setup-sudo-env` wrote to the same file (A13: same file when the managed user is the invoking sudo user).
12. **[V] Run All prints "AppArmor configured" and "Lynis security audit completed"** (`run_all_menu.go:269-275`), but `SetupAppArmor`, `SetupLynis` and `SetupUnattendedUpgrades` have no callers.

---

## 2. How side effects flow on origin/main

- **Entry points:**
  - CLI operation flags (`cmd/hardn/main.go`) call managers directly: `-c -d -w -g -r -e`. v0.3.2's `-l -i -a -s` are commented out (`main.go:89-94`). *(2026-10-23, slice 2 unit (d), `next/s2-replace`: `-g` no longer calls a manager; it plans and applies the DNS control through the engine, and `-n -g` shows its plan: [cmd-engine.md](../../engine/cmd-engine.md).)*
  - `setup-sudo-env` is a subcommand (`main.go:420-450`).
  - The interactive menu (`pkg/menu`) → `application.MenuManager` → `*Manager` → domain service (pass-through) → adapter (`pkg/adapter/secondary`) → `interfaces.OSFileSystem` / `OSCommander`.
- **Direct `os/exec` outside the adapters:**
  - `pkg/menu/disable_root_menu.go:155-157` (the second restart)
  - `pkg/menu/dns_menu.go:351-357` (read-only)
  - `pkg/menu/input.go` (`stty`)
  - `pkg/security/status.go` and `pkg/system/*` (read-only probes, §4.5)
  - `pkg/config/config.go:313` (`su - $SUDO_USER -c 'echo $HARDN_CONFIG'`)
  - dead legacy `pkg/security/security.go` and `pkg/updates/updates.go`
- **Command runner:** `OSCommander.Execute` = `exec.Command(...).CombinedOutput()` with stdin = /dev/null (`os_commander.go:12-24`). Most callers discard the output; errors carry only "exit status N".
- **Unreachable on origin/main:** the Linux/Python package and sources menus (commented out at `main_menu.go:457-458, 514-520`), the CLI install and sources flags, AppArmor, Lynis, unattended upgrades, `utils.BackupFile`, `utils.SetupHushlogin`, and UFW install/disable (the menu prints "isn't yet implemented").
- **Exit status:** operation failures in the flag path are logged and the process **exits 0**, then prints "Script completed selected hardening operations." Only the pre-flight checks and `setup-sudo-env` call `os.Exit(1)` (A15).

---

## 3. Side-effect table (origin/main, 31 rows)

**Dry-run column:** CLI = the flag path (`main.go`); Menu = the interactive path. **"Verification"** gives the second-pass result (A-numbers) or a correction.

| # | Operation | Distro | Exact argv / path / mode | Code (origin/main) | Dry-run honoured? | Validated before activation? | Backup / rollback? | Lockout risk | Verification |
|---|---|---|---|---|---|---|---|---|---|
| 1 | Log init | all | `MkdirAll(/var/log, 0755)`; `OpenFile(/var/log/hardn.log, O_CREATE\|O_APPEND, 0644)` on **every** invocation, before flag parsing (even `--help`) | `cmd/hardn/main.go:56`; `pkg/logging/logging.go:20-43` | n/a (always) | n/a | n/a | none | A9 confirmed. `install.sh` relies on it (`hardn --version` to initialise) |
| 2 | Example config | all | `MkdirAll(/etc/hardn, 0755)`; `os.WriteFile(/etc/hardn/hardn.yml.example, 0644)` if missing; before flag parsing | `main.go:59`; `pkg/config/embed.go:84-104` | always | n/a | n/a | none | A9 confirmed |
| 3 | Default-config creation | all | No config found: prompt `[Y/n]` on stdin with **no TTY check** (empty or EOF = yes); writes `/etc/hardn/hardn.yml` (0644) as root. The username prompt defaults to `"george"`; `sudoNoPassword: true` is the default | `config.go:258-266, 368-442`; `config.go:121` | always | no | no | none directly; "george" + passwordless sudo is what Run All then creates | Also runs in flag mode, so a scripted `sudo hardn -r` with no config consumes or blocks on stdin |
| 4 | Config reload during status render | all | Each main-menu render calls `GetExtendedUserInfo` → `config.LoadConfig("")`: ignores `-f`, can trigger row 3, and calls `logging.SetSilentMode(true)`, never restored | `security/status.go:100,120`; `os_user_repository.go:19-35, 433` | always | no | no | none (visibility) | New finding: after the first render, **all console log output is suppressed** for the rest of the process |
| 5 | sudo env-loss probe | all (under sudo) | `su - $SUDO_USER -c "echo $HARDN_CONFIG"` on every `LoadConfig`: runs the user's login shell and profile from a root process | `config.go:289-322` | always | n/a | n/a | none | New since v0.3.2 [V, verification new findings]. *Removed in `next/s9-replace` (slice 9, decision 4; the record left as it was)* |
| 6 | Update check | all (menu start) | HTTPS GET `api.github.com/repos/abbott/hardn/releases/latest` (3 s timeout; only when `Version` is set, i.e. release builds); writes `/tmp/hardn-version-cache.json` 0644 as root (or `$HARDN_CACHE_PATH`), symlink-following `os.WriteFile` | `pkg/version/checker.go:18, 97-159, 318-358`; `main_menu.go:69-114`; `main.go:204` | always | n/a | n/a | none to SSH; **UI lockout** (see verification) | A10, corrected: the release **name** matches only `security`, `[security]` or `cve-`; `vulnerability`/`exploit` only in the body; nothing is checked unless the latest tag is newer. A match replaces the menu with "Update Binary <URL>" and exits after a key. **An unprivileged user can plant the cache** (future `last_check`, tag v99, a "security" body, any URL) and root's hardn shows that URL and exits [V harness]. Runs twice concurrently (data race) |
| 7 | Terminal mode | all (menu) | `stty -F /dev/tty cbreak min 1` / `-cbreak` (any-key); `stty -F /dev/tty raw -echo` / `sane` **per keystroke** (menu input) | `pkg/menu/input.go:22-55, 105-144` | n/a | n/a | restored by `defer` only | none (terminal left raw or cbreak on a crash) | A11 confirmed: Ctrl-C (0x03) is discarded; immediate EOF looped 368 times in 2 s, spawning 2 `stty` per iteration; `q` returns with no Enter. GNU `-F` flag: Linux only |
| 8 | Write sshd config | Debian / Ubuntu / Proxmox | `os.WriteFile(/etc/ssh/sshd_config.d/hardn.conf, 0644)`: `Protocol 2`, `StrictModes yes`, `Port <cfg>`, `ListenAddress <cfg, default 0.0.0.0>`, `AuthenticationMethods publickey`, `PubkeyAuthentication yes`, `PermitRootLogin no` (forced by `HardenSystem`), `AllowUsers <cfg>` (if set), `PasswordAuthentication no`, `PermitEmptyPasswords no`, `AuthorizedKeysFile .ssh/authorized_keys` (`-r`) or `.ssh_%u` (`-c`) | `file_ssh_repository.go:35-105`; `security_manager.go:46-56`; `ssh_manager.go:22-41`; `main.go:368-376` | CLI **no** [V]; Menu Run All yes (`run_all_menu.go:238-244`) | **no** `sshd -t`, no `sshd -T`, no Include check | **no** backup, no rollback | **CRITICAL** | A1, A3 confirmed. `sshConfigFile` is dead: the path is always hardcoded |
| 9 | Write sshd config | Alpine | **Replaces the whole** `/etc/ssh/sshd_config` with the same ~15 lines | `file_ssh_repository.go:38-41, 103` | CLI no [V]; Menu Run All yes | no | no | **CRITICAL** | A2 + new finding: the 15-line file has **no `Subsystem sftp`** (and no `Include`), so sftp breaks on every Alpine SSH write [V harness] |
| 10 | Restart sshd | Deb/Ubu/PVE `systemctl restart ssh`; Alpine `rc-service sshd restart` | restart, not reload; `ssh.socket` not handled; no `daemon-reload` | `file_ssh_repository.go:107-121` | as row 8 | no | no | HIGH (an invalid config stops the service and new connections fail [I]) | A8 confirmed (no reload or socket handling on any ref) |
| 11 | Disable root SSH | all | Rows 8/9 with the stub `{Port: 22}`: `Port 22`, no `ListenAddress`/`AllowUsers`, key-only. The menu then **restarts a second time** | `file_ssh_repository.go:127-155`; `disable_root_menu.go:139-168`; `main.go:277-283` | CLI **no** [V]; Menu yes (`disable_root_menu.go:139`) | no | no | **CRITICAL** (port reset; key-only) | A2 confirmed: a seeded `Port 2222` / `ListenAddress` / `AllowUsers root alice` became the stub; on Debian only the drop-in changed |
| 12 | Create user | Debian / Ubuntu / Proxmox | `adduser --disabled-password --gecos "" <u>`; `usermod -aG sudo <u>`. No password is ever set (no `chpasswd`/`passwd` on any ref) | `os_user_repository.go:110-124` | CLI no; Menu **no** (`user_options.go:744, 831`); Menu Run All yes | adduser's own check only on the CLI/config path; the menu validates `^[a-z][a-z0-9_-]*$` | no rollback (partial state on later failure) | HIGH (with `sudoNoPassword: false` the user can't sudo) | A5 confirmed: "Allow sudo access? (y/n)" is computed, then literal `true` is passed (`:744`) |
| 13 | Create user | Alpine | `adduser -D -g "" <u>`; `addgroup <u> wheel`; **sudo not installed** | `os_user_repository.go:96-109` | as row 12 | no | no | **CRITICAL** (Alpine) | A14 confirmed. `adduser -D` probably leaves the account locked, which non-PAM sshd refuses even for keys [I; Alpine VM needed] |
| 14 | Add SSH key | Debian / Ubuntu / Proxmox | `su - <u> -c "mkdir -p ~/.ssh && chmod 700 ~/.ssh"`; key on stdin to `su - <u> -c "cat >> ~/.ssh/authorized_keys"` (**no dedupe**); `su - <u> -c "chmod 600 ~/.ssh/authorized_keys"` | `os_user_repository.go:210-227` | CLI no; Menu **no** (`user_options.go:397, 487, 538`; `ssh_keys_options.go:78`) | no key-format check on the CLI/config path | no | LOW (duplicates; runs as the user) | A5: **"Remove SSH key" removes nothing** (append-only path) and reports success |
| 15 | Add SSH key | Alpine | As root: `MkdirAll(/home/<u>/.ssh, 0700)`; read, append, `os.WriteFile(…/authorized_keys, 0600)` (follows symlinks); `chown -R <u>:<u> /home/<u>/.ssh`. Hard-codes `/home/<u>` | `os_user_repository.go:162-209` | as row 14 | no | no | MEDIUM (root writes through a user-controlled path) | — |
| 16 | Add SSH key (MenuManager path) | all | As root: `/home/<u>` or `/root` assumed; `os.WriteFile(authorized_keys, 0600)`; `sh -c "chown -R <u>:<u> <dir>"` (**user name interpolated into a shell string**) | `file_ssh_repository.go:158-216`; `menu_manager.go:53-73` | Menu no | no | no | MEDIUM | — |
| 17 | Sudo grant | all | `MkdirAll(/etc/sudoers.d, 0755)`; `os.WriteFile(/etc/sudoers.d/<username>, 0440)` = `<u> ALL=(ALL) NOPASSWD: ALL` (default) or `<u> ALL=(ALL) ALL` | `os_user_repository.go:233-264` | CLI no [V]; Menu **no** | **no `visudo -c`**; file name not sanitised | no | HIGH | A13 confirmed: overwrites the `env_keep` line from row 18 when the managed user is the invoking sudo user (the typical admin case) |
| 18 | Sudo env preservation | all (CLI `-e`, `setup-sudo-env`, menu 8) | Writes `$TMPDIR/hardn_sudoers_temp` (0440; predictable `/tmp` name; follows symlinks); `visudo -c -f <temp>`; writes `Defaults:<SUDO_USER\|USER> env_keep += "HARDN_CONFIG"` to `/etc/sudoers.d/<user>` via `os.WriteFile` | `file_environment_repository.go:33-101` | CLI no [V]; Menu yes (`environment_settings_menu.go:86`) | **yes** (`visudo -c -f`): the only validated sudoers write | no | LOW–MEDIUM | — |
| 19 | UFW configure | all (UFW must already be installed) | `ufw default deny incoming`; `ufw default allow outgoing`; `ufw disable`; `ufw reset`; [`/etc/ufw/applications.d/hardn` 0644 + `ufw allow from any to any app <Name>`]; `ufw allow <cfg.SshPort>/tcp comment "SSH access"`; `sh -c "yes \| ufw enable"` (fixed string) | `ufw_firewall_repository.go:97-142, 214-261`; `firewall_manager.go:40-75`; `firewall_menu.go:209-281`; `main.go:380-386` | CLI no [V]; Menu yes | Allow before enable: **yes, but only `cfg.SshPort`**; no `ufw status verbose` afterwards; no Proxmox check | none by hardn. A failure after `disable` leaves the firewall **off** | **CRITICAL** | A6 confirmed (order verified). **`ufw reset` without `--force` probably aborts silently** (EOF → "Aborted", exit 0) [I, ufw source]. So the effect today is likely "defaults changed, firewall disabled, then re-enabled with one more allow rule" [I]; the wipe-all-rules reading holds only if reset proceeds |
| 20 | UFW install / disable | all | Not implemented: the menu prints "isn't yet implemented"; `-w` returns "UFW firewall is not installed" | `firewall_menu.go:177-207`; `ufw_firewall_repository.go:99-101` | — | — | — | — | v0.3.2 installed ufw (apt/apk) and ran `rc-update add ufw default` on Alpine |
| 21 | DNS via systemd-resolved | when `systemctl is-active systemd-resolved` succeeds | **Overwrites** `/etc/systemd/resolved.conf` (0644) with `[Resolve]`, `DNS=<ns…>`, `Domains=lan`; `systemctl restart systemd-resolved` | `file_dns_repository.go:35-55, 98-120`; `dns_manager.go:22-31` | CLI no [V]; Menu yes (`dns_menu.go:146`) | no; an empty list is accepted from CLI/Run All (harness wrote `DNS=`) | no | none (DNS outage possible) | — |
| 22 | DNS via resolvconf | when `which resolvconf` succeeds | `/etc/resolvconf/resolv.conf.d/head` = `domain lan`, `search lan`, `nameserver …`; `resolvconf -u` | `file_dns_repository.go:123-161` | as row 21 | no | no | none | — |
| 23 | DNS direct | otherwise (typical Alpine) | `os.WriteFile(/etc/resolv.conf, 0644)` = `domain lan` / `search lan` / `nameserver …`. **Follows a symlink** (writes into the target, doesn't replace the link); NetworkManager not detected | `file_dns_repository.go:164-190` | as row 21 [V harness] | no; an empty list gives a file with no `nameserver` | no | none (resolution can break) | — |
| 24 | Package install | all | `apt-get update`; `apt-get install --yes <pkgs>`; `apt-get autoremove --yes`; `apt-get clean`; `rm -rf "/var/lib/apt/lists/*"` (literal, so a no-op); Proxmox `apt-mark hold`/`unhold` of {proxmox-archive-keyring, proxmox-backup-client, proxmox-ve, pve-kernel}; Alpine `apk add --no-cache`; `pip3 install uv` / `uv pip install` / `pip3 install` as root | `os_package_respository.go:46-165, 304-331` | **unreachable** | no | no | none | — |
| 25 | Package sources | Deb/Ubu, Alpine, PVE | `/etc/apt/sources.list` (copied to `.bak` first, overwritten every run); `/etc/apk/repositories` (no backup), then `apk update`; `/etc/apt/sources.list.d/{ceph,pve-enterprise}.list` | `os_package_respository.go:167-277` | **unreachable** | no | partial | none | A7: the `.bak` is the only backup in the tree, and it is unreachable |
| 26 | Auto updates | Deb/Ubu; Alpine | Deb: `apt-get install -y unattended-upgrades`; `debconf-set-selections` (origins pattern `Debian-Security`, wrong on Ubuntu); `dpkg-reconfigure -f noninteractive unattended-upgrades`; `systemctl enable unattended-upgrades`; sets `DEBIAN_FRONTEND` for the process. Alpine: `/etc/periodic/daily/apk-upgrade` 0755 = `apk update && apk upgrade --available`; `rc-update add crond default`; `rc-service crond start` | `pkg/updates/updates.go:15-97` | **dead** (no callers) | no | no | none | — |
| 27 | AppArmor | Deb/Ubu; Alpine | Deb: `apt-get install -y apparmor`; `aa-enforce "/etc/apparmor.d/*"` (literal glob), then each top-level file (needs apparmor-utils, not installed). Alpine: `apk add apparmor`; `rc-update add apparmor default`; `rc-service apparmor start`; `aa_enforce <each>` (wrong binary name [I]) | `pkg/security/security.go:15-102` | **dead** | no | no | none (services may break) | — |
| 28 | Lynis | Deb/Ubu; Alpine | Distro repos only: `apt-get install -y lynis` / `apk add lynis`; `lynis audit system` (output discarded unless it fails) | `pkg/security/security.go:105-142` | **dead** | n/a | n/a | none | — |
| 29 | Config save | all (menu) | `config.SaveConfig(cfg, "hardn.yml")`: the whole config (including runtime `dryRun`) to **`./hardn.yml` in the current directory**, 0644, not to the file that was loaded | `dns_menu.go:186,205,224,293,334`; `firewall_menu.go:478,528`; `user_options.go:610,775`; `ssh_keys_options.go:68,137`; `backup_menu.go:157,195`; `dry_run_menu.go:118`; `run_all_menu.go:174` | not gated (config only) | n/a | no | none | C6: 17 call sites, 15 reachable (v0.3.2 had 4). Comments in the file are lost |
| 30 | Backup dir check | all (menu 5) | `MkdirAll(backupPath, 0755)`; write and remove `<backupPath>/.write_test` | `file_backup_repository.go:220-238`; `backup_manager.go:93-99` | menu | n/a | n/a | none | The Backup screen configures a feature that never runs *(2026-10-27, slice 3 unit (d), `next/s3-replace`: the legacy screen is deleted; hardn's Backup screen writes nothing to draw or Verify, [screen-backup.md](../../engine/screen-backup.md))* |
| 31 | System-details export | all (menu 9) | `utils.WriteToFile(<user-typed name or ./system_status.txt>, 0644)` as root, relative to the current directory | `system_details_menu.go:133-241` | yes (`:225`) | n/a | n/a | none | — |

---

## 4. Per-distro inventory (origin/main, reachable code)

The code distinguishes only **Alpine** from everything else. **Debian and Ubuntu are treated identically.** **Proxmox is Debian plus an `/etc/pve` check** that gates nothing reachable. Platform facts below come from the verified research in the findings (F§10); they describe the target systems, not hardn's code.

### 4.1 Debian

| Kind | What origin/main does |
|---|---|
| Files written | `/var/log/hardn.log` and `/etc/hardn/hardn.yml.example` (every run); `/etc/hardn/hardn.yml` (first run, after a prompt); `/tmp/hardn-version-cache.json` (menu start, release builds); `$TMPDIR/hardn_sudoers_temp` (`setup-sudo-env`); `/etc/ssh/sshd_config.d/hardn.conf` 0644; `/etc/sudoers.d/<user>` 0440; `~<user>/.ssh/authorized_keys` (via `su`, as the user) or `/home/<u>`/`/root` `.ssh/authorized_keys` 0600 (menu path, as root); `/etc/ufw/applications.d/hardn` 0644 (when app profiles are configured); one of `/etc/systemd/resolved.conf`, `/etc/resolvconf/resolv.conf.d/head` or `/etc/resolv.conf`; `./hardn.yml`; `<backupPath>/.write_test`; `./system_status.txt` (export) |
| Commands run | `adduser --disabled-password --gecos "" <u>`; `usermod -aG sudo <u>`; `id <u>`; `su - <u> -c …` (key install); `sh -c "chown -R <u>:<u> <dir>"` (menu path); `systemctl restart ssh` (twice from the disable-root menu); `which ufw`; `ufw default deny incoming`; `ufw default allow outgoing`; `ufw disable`; `ufw reset`; `ufw allow from any to any app <Name>`; `ufw allow <port>/tcp comment "SSH access"`; `sh -c "yes \| ufw enable"`; `systemctl is-active systemd-resolved`; `systemctl restart systemd-resolved`; `which resolvconf`; `resolvconf -u`; `visudo -c -f <temp>`; `su - $SUDO_USER -c 'echo $HARDN_CONFIG'`; `stty -F /dev/tty …`; plus the read-only probes in §4.5 |
| Services touched | `ssh` (restart); `systemd-resolved` (restart); `ufw` (disable, reset, enable) |
| Packages installed | **None reachable.** UFW must already be installed. (v0.3.2 installed `sudo`, `ufw` and the configured package lists) |
| Dead code that would run if wired | `apt-get update/install/autoremove/clean`; `unattended-upgrades` install + `debconf-set-selections` + `dpkg-reconfigure` + `systemctl enable`; `apparmor` install + `aa-enforce`; `lynis` install + `lynis audit system`; `/etc/apt/sources.list` rewrite |
| Platform facts (F§10) | `ssh.service`, not socket-activated (socket opt-in, no generator). The stock config `Include`s `sshd_config.d/*.conf` as its first directive; first value wins, except `Port`, `ListenAddress`, `HostKey`, `Allow/Deny Users/Groups` and `AcceptEnv`, which accumulate |

### 4.2 Ubuntu

Same code path as Debian (no Debian/Ubuntu distinction anywhere). Differences that matter on the target:

| Kind | Note |
|---|---|
| SSH activation | 24.04 and 26.04 are **socket-activated by default**. A `Port` change needs `daemon-reload` plus `restart ssh.socket`; a reload can't change the port. origin/main has **no** `ssh.socket` handling (V12), while UFW allows only the configured port. 22.04 uses `ssh.service` (an upgrade to 24.04 migrates it) |
| Service name | `ssh`. The `sshd` alias is missing on fresh 24.04, so never call `sshd` as a unit name (origin/main correctly uses `ssh`) |
| Drop-in order | `hardn.conf` sorts after cloud-init's `50-cloud-init.conf`, which then wins for single-value keys (V13) |
| sudo | 26.04 ships **sudo-rs** (same drop-in file-name rule) |
| Dead code | The unattended-upgrades preseed uses the origin pattern `Debian-Security`, which is wrong on Ubuntu. v0.3.2 wrote Debian repositories into Ubuntu's `sources.list` |

### 4.3 Proxmox VE

Detected only by `os.Stat("/etc/pve")`. The os-release `ID` is `debian`, and the main menu renders it as "Debian". **Everything in §4.1 applies unchanged.**

| Kind | Note |
|---|---|
| Gating present | **None** for SSH, root login, `AllowUsers` or UFW. `IsProxmox` gates only the unreachable package holds and sources |
| What runs that shouldn't | `ufw … enable` with default-deny (V10: blocks 8006, corosync and migration ports; competes with pve-firewall); `PermitRootLogin no` (V11) |
| Platform facts (F§10) | PVE writes `PermitRootLogin yes` into the main `sshd_config` itself (not drift; a drop-in overrides it). Root's `authorized_keys` is a **symlink** to the cluster-wide file under `/etc/pve/priv`, so never rename over it. Inter-node SSH uses port 22. Joining a cluster uses the API, not SSH password login. Robust detection is the `pve-manager` package status or `/usr/bin/pveversion`; `/etc/pve` stays as an empty directory when the cluster filesystem is down |

### 4.4 Alpine

| Kind | What origin/main does |
|---|---|
| Files written | **The whole `/etc/ssh/sshd_config`** (15 lines; no `Subsystem sftp`, no `Include`); `/etc/sudoers.d/<user>` 0440; `/home/<u>/.ssh/authorized_keys` 0600 (as root, dir 0700); usually `/etc/resolv.conf` (direct DNS path); plus the all-distro files in §4.1 (log, example config, cache, `./hardn.yml`) |
| Commands run | `adduser -D -g "" <u>`; `addgroup <u> wheel`; `chown -R <u>:<u> /home/<u>/.ssh`; `rc-service sshd restart` (twice from the disable-root menu); the UFW sequence (if UFW is installed); `systemctl is-active systemd-resolved` (fails harmlessly); plus the probes in §4.5 (`apk info -e apparmor`, `rc-status default` on Alpine) |
| Services touched | `sshd` (OpenRC restart); `ufw` (if installed; no `rc-update add ufw`, so not persistent across reboot [I]) |
| Packages installed | **None**: `sudo` is never installed (V14; v0.3.2 ran `apk add sudo`) |
| Dead code that would run if wired | `/etc/periodic/daily/apk-upgrade` (`apk update && apk upgrade --available`) + `rc-update add crond default` + `rc-service crond start`; `apk add apparmor` + `aa_enforce`; `apk add lynis`; `/etc/apk/repositories` rewrite |
| Platform facts (F§10) | The default elevation tool is **doas**; `sudo` and `ufw` are in the community repository (only `main` is supported on 3.21–3.23; 3.21 support ends 2026-11-01). Lynis isn't packaged in any stable branch. Alpine ≥ 3.19 has the `Include`; hosts upgraded from ≤ 3.18 may lack it. `rc-service sshd reload` runs `sshd -t` itself. `apk-cron` runs `apk -U upgrade` daily and needs `crond` |

### 4.5 Read-only probes (all distros) [V code]

Run on every main-menu render, on the user and DNS screens and in System Details. They write nothing but cost about 6–10 subprocesses per return to the main menu [I].

| Where | argv |
|---|---|
| Main-menu status (`pkg/security/status.go`) | `ufw status verbose`; fallback `iptables -L`; `aa-status`; Alpine `apk info -e apparmor`, `rc-status default`; `dpkg -l unattended-upgrades`; `systemctl is-enabled unattended-upgrades`; `which sudo` |
| User details (`os_user_repository.go`) | `id <u>`; `id -u <u>`; `id -g <u>`; `getent passwd <u>`; `groups <u>`; `grep <u> /etc/sudoers`; `cat /etc/passwd`; `cat /etc/group`; **`sudo cat <sshKeyPath>`** (`:565`) |
| Last login | `last -1 <u>` (`last_command_adapter.go`); `lastlog -u <u>` (direct `exec`) |
| Host info (`os_host_info_repository.go`) | `cat /etc/resolv.conf`; `hostname -f`; `domainname`; `uptime`; `cat /etc/os-release`; `uname -r`; `cat /proc/cpuinfo`; `cat /proc/meminfo`; `df -k` |
| DNS screen | `systemctl is-active systemd-resolved`; `resolvectl dns` |
| System Details (`pkg/system`) | `uname -r`; `uptime`; `cat /proc/cpuinfo`; `nproc`; `lscpu`; `free -b`; `zpool status -x`; `zfs get -Hp available\|used <fs>`; `df -k <root>`; `groups <u>`; `sudo -l -U <u>`; `lastlog -u` / `last -1` |

---

## 5. Distro gating present on origin/main [V]

| Gate | Where | Effect |
|---|---|---|
| `osType == "alpine"` | `file_ssh_repository.go:39,111`; `os_user_repository.go:96,162,518`; `os_package_respository.go:58,109,169,281`; `security/status.go:235,357,416` | Whole `sshd_config` vs drop-in; `rc-service sshd` vs `systemctl … ssh`; busybox `adduser` + `wheel` vs `adduser` + `sudo`; root file write vs `su`; apk vs apt |
| `IsProxmox` (`/etc/pve` exists) | `osdetect.go:100-104`; `os_package_respository.go:66,97,245`; `main.go:267` | **Only** package holds and sources, which are unreachable. **No gating** of UFW, `PermitRootLogin`, `AllowUsers` or autoremove |
| Debian vs Ubuntu | none | Treated identically |
| Unsupported `ID` | `osdetect.go:94-98` | Exits with an error (only `debian`, `ubuntu`, `alpine`; `ID_LIKE` not read) |
| Container detection | none | — |
| `ssh.socket` / init detection | none | `systemctl` is always used off Alpine; on Alpine `systemctl is-active systemd-resolved` still runs and fails harmlessly |

---

## 6. Where v0.3.2 differs [V unless marked]

Decision 6: these rows matter **where origin/main regressed**. The full v0.3.2 argv and file-content table is `v032-reference.md`.

| Area | v0.3.2 (bce7cc2) | origin/main |
|---|---|---|
| Dry-run | Per-function `cfg.DryRun` checks (10 packages); CLI `-n` honoured. **Bug:** the disable-root menu restarts sshd even in dry-run (`v0.3.2:pkg/menu/menu.go:362-371`) | Menu-only checks; the CLI ignores `-n`; the user menu ignores dry-run |
| SSH file | `cfg.SshConfigFile` (default `/etc/ssh/sshd_config.d/manage.conf`) plus `/etc/systemd/system/ssh.socket.d/listen.conf` (`ListenStream=` / `ListenStream=<port>`, **no `daemon-reload`**, so probably never effective until a reboot [I]); Alpine: whole `sshd_config` replaced (with backup) | `hardn.conf` hard-coded; `sshConfigFile` ignored; no socket handling |
| SSH content | `ListenAddress <addr>:<port>` (no `Port` line in the Debian drop-in); `AllowUsers %s` even when empty; `AuthorizedKeysFile .ssh/authorized_keys %s/authorized_keys`; `PermitRootLogin` from `cfg.PermitRootLogin` | `Port` plus `ListenAddress`; `AllowUsers` only if set; a single `AuthorizedKeysFile`; `PermitRootLogin no` forced in Run All |
| Disable root | In-place edit of `PermitRootLogin yes` → `no` and root removed from `AllowUsers`, everything else kept, with backup (`ssh.go:174-300`) | Full rewrite from a stub |
| Users | Existing user (`user.Lookup`) → skip entirely; installs `sudo` if missing (apt/apk); keys to `~/<.ssh_%u>/authorized_keys` via `su - u -c "echo '<key>' >> …"` (**key interpolated into a shell string**); `.hushlogin` | Existing users are updated (sudo file overwritten, keys appended); no sudo install; keys in `~/.ssh` |
| UFW | Installs ufw (apt/apk); default deny/allow; `ufw allow <port>/tcp` (**failure only logged**); app-profile allow passed as one argv string (a bug); `yes \| ufw enable` **even if the SSH allow failed**; Alpine `rc-update add ufw default` + `rc-service ufw start` | No install; `disable` + `reset` added; aborts before enable if the allow fails; no Alpine `rc-update` |
| DNS | Same three strategies, with backups. **Panics** on an empty nameserver list (`cfg.Nameservers[0]`); at most 2 nameservers | No backups; empty list accepted; all nameservers |
| Packages / sources | Live: `-c` **also rewrites `/etc/apt/sources.list`**; every install runs `apt-get install locales`, `sed -i /etc/locale.gen`, `locale-gen`; pattern-based Proxmox hold then **unhold** (clears the admin's holds); `pip3 install` as root | Unreachable |
| AppArmor / Lynis / unattended upgrades | Live in Run All when enabled in config | Dead code; Run All claims success |
| Backups | `utils.BackupFile` before 13 writes, to `/var/backups/hardn/<YYYY-MM-DD>/<base>.<HHMMSS>.bak` 0644 (sudoers backups world-readable); errors ignored; no restore UI | None |
| Config keys | `disableRoot`, `sshConfigFile` (used) | `disableRootSSH` (renamed, no alias; not used by `HardenSystem`); `sshConfigFile` present but unused |

**Violations that exist only in v0.3.2** (relevant only if v0.3.2 behaviour is borrowed as a regression reference):
- the disable-root menu restarts sshd in dry-run;
- UFW is enabled even when the SSH allow failed (a lockout);
- DNS panics on an empty list;
- the SSH key is interpolated into an `su -c` shell string;
- `-c` rewrites apt sources;
- Debian repositories are written onto Ubuntu;
- admin package holds are cleared by `unhold`.

---

## 7. Harness evidence (excerpt; every run had `cfg.DryRun=true`)

Values are synthetic; the SSH port is shown as `<port>`.

```
===== hardn -n -r (os=debian proxmox=true, cfg.DryRun=true) err=<nil>
 commands: id alice | su - alice -c mkdir -p ~/.ssh && chmod 700 ~/.ssh | INPUT:<key>|su - alice -c cat >> ~/.ssh/authorized_keys |
           su - alice -c chmod 600 ~/.ssh/authorized_keys | systemctl restart ssh | which ufw | ufw default deny incoming |
           ufw default allow outgoing | ufw disable | ufw reset | ufw allow <port>/tcp comment SSH access | sh -c yes | ufw enable |
           systemctl is-active systemd-resolved | which resolvconf | systemctl restart systemd-resolved
 files: /etc/ssh/sshd_config.d/hardn.conf (Port <port> / ListenAddress 0.0.0.0 / AuthenticationMethods publickey / PermitRootLogin no /
        PasswordAuthentication no / AuthorizedKeysFile .ssh/authorized_keys), /etc/sudoers.d/alice (alice ALL=(ALL) NOPASSWD: ALL),
        /etc/systemd/resolved.conf
===== hardn -n -d (os=alpine) -> /etc/ssh/sshd_config = "...Port 22 ... AuthenticationMethods publickey ... PasswordAuthentication no ..."; rc-service sshd restart
===== hardn -n -c -u alice (debian) -> hardn.conf contains "AuthorizedKeysFile .ssh_%u"; keys appended to ~/.ssh/authorized_keys; systemctl restart ssh
===== hardn -n -c NEW user (debian) -> adduser --disabled-password --gecos  alice ; usermod -aG sudo alice ; su ... (keys)
===== hardn -n -c NEW user (alpine) -> adduser -D -g  alice ; addgroup alice wheel ; chown -R alice:alice /home/alice/.ssh (no sudo install)
===== hardn -n -g with EMPTY nameservers -> /etc/systemd/resolved.conf "[Resolve]\nDNS=\nDomains=lan" ; systemctl restart systemd-resolved
===== hardn -n -g direct path (alpine) -> /etc/resolv.conf "domain lan\nsearch lan\nnameserver 1.1.1.1"
===== hardn -n -e -> visudo -c -f $TMPDIR/hardn_sudoers_temp ; /etc/sudoers.d/alice "Defaults:alice env_keep += \"HARDN_CONFIG\""
```

One mock artefact: on the "NEW user" runs the mock's `id alice` keeps failing after `adduser`, so `ConfigureSudo` returns "does not exist". On a real host it would go on to write the sudoers file.

---

## 8. The V-list (V1–V27), re-labelled per decision 6

**Decision 6 changes how these are classified.** None of them is behaviour to preserve; every one is a defect that will be fixed. What differs is how much of the *fix* is already settled:

| Fix-type label | Meaning |
|---|---|
| **Fix: safety** | A pure safety or bug fix with no visible design choice. Review only |
| **Fix: regression** | origin/main regressed from v0.3.2. **v0.3.2's behaviour is the reference** for the fix (decision 6) |
| **Fix: decided** | The corrected behaviour is already decided in brief §2 or §4; it still gets a characterization test of the old behaviour first |
| **Fix + approach** | The defect is fixed regardless, but *how* changes visible behaviour beyond both v0.3.2 and the brief, so the approach needs the named plan §3 row (or a new one) |

The "Former label" column keeps the first pass's classification (Safety / Restores / Approval / Decided) for traceability. "Plan hook" names the P0.8 hazard gate, the plan §3 row and/or the plan unit (`next/p…`) that carries the fix.

| ID | Defect | Severity | Evidence | Fix type (decision 6) | Former label | Plan hook |
|---|---|---|---|---|---|---|
| V1 | CLI `-n/--dry-run` ignored for `-r -c -d -w -g -e` | critical | [V] harness; `main.go:152`; 0 DryRun refs in core layers (A1) | **Fix: regression** (v0.3.2 honoured `-n`) + approach (§3.10: refuse operation flags under `-n`) *(2026-10-23, slice 2 unit (d): for `-g`, the DNS control's plan in place of the refusal)* | Restores | P0.8 item 1 |
| V2 | User-management menu ignores dry-run (create user, toggle sudo, add/remove key); only the success message is gated | high | [V] `user_options.go:397,487,538,744,831`; `ssh_keys_options.go:78` (A5) | **Fix: safety** (gate the mutation, not the message) | Restores / Safety | slice: user management |
| V3 | `dryRun: true` in `hardn.yml` is overridden by the flag default `false` | high | [V] `main.go:152` (v0.3.2 `main.go:142` too) (C5) | **Fix + approach** (§3.19: an explicit flag wins, otherwise config). Not a regression: v0.3.2 behaves the same *(2026-10-23, slice 2b unit (b), `next/s2b-mode`: fixed. `-n`/`--dry-run` given wins, true or false, otherwise the loaded file's `dryRun`; an operation flag under the file's plan only is refused as under `-n`; the legacy saves write `dryRun` as loaded: [dryrun-mode.md](../../engine/dryrun-mode.md))* | Approval | §3.19 |
| V4 | Disable-root rewrites the whole SSH config from a stub (`Port 22`, drops `ListenAddress`/`AllowUsers`, forces key-only); Alpine whole-file replace | critical | [V] harness; `file_ssh_repository.go:127-155` (A2) | **Fix: regression** (v0.3.2 edited `PermitRootLogin` in place, from a real parse) | Safety / Restores | P0.8 item 2 |
| V5 | `-c` sets a global `AuthorizedKeysFile .ssh_%u` while keys live in `~/.ssh/authorized_keys`, with password auth off | critical | [V] harness; `main.go:373`; `config.go:117`; sshd_config(5) (A3) | **Fix: regression** (v0.3.2's dual `AuthorizedKeysFile`) + approach (§3.17: `sshKeyPath` semantics) | Safety + Approval | P0.8 item 4 |
| V6 | Password auth disabled and `AuthenticationMethods publickey` set in the same step as keys are added; no proof any key works; no confirm-or-revert | critical | [V] `file_ssh_repository.go:62-85`; `security_manager.go:46-56` | **Fix: decided** (brief §2.3 step 6, §2.4) | Decided | Phase 2 (`p2-revert`) |
| V7 | No `sshd -t` before activation, no `sshd -T` after; restart instead of reload; double restart in the menu | high | [V] grep across all refs (A8); `file_ssh_repository.go:107-121`; `disable_root_menu.go:151-158` | **Fix: safety** (+ decided, brief §2.2/§2.3) | Safety | `p2-sshd-validate`, `p2-reload` |
| V8 | SSH port changed before the firewall allows it; firewall step optional (`enableUfwSshPolicy` default false) and never checks whether UFW is already active | critical | [V] `security_manager.go` order 46 → 57; `config.go:135`; [I] outcome | **Fix: decided** (brief §2.3 step 2 ordering) + approach for adding an allow rule the user didn't ask for (no §3 row yet) | Safety + Approval | `p3-engine` (ordering rule), `p2-ufw` |
| V9 | UFW allows only `cfg.SshPort` (not the effective sshd port) and runs `ufw disable` + `ufw reset` before re-adding rules | critical | [V] `firewall_manager.go:40-75`; `ufw_firewall_repository.go:104-139` (A6). **Correction:** `reset` without `--force` probably aborts silently with stdin=/dev/null [I, ufw source], so the reset may be a no-op today; a failure after `disable` still leaves UFW off | **Fix: safety** (allow the effective and the new port; keep the old one) + approach (§3.17: no UFW `disable`/`reset`) | Safety + Approval | §3.17; `p2-ufw` |
| V10 | UFW enabled on Proxmox (blocks 8006, corosync and migration ports; competes with pve-firewall) | critical | [V] harness (proxmox=true) (A12) | **Fix: decided** (brief §2, §2.5) | Decided | P0.8 item 6; `p2-pve` |
| V11 | `PermitRootLogin no` forced, including on Proxmox; config `permitRootLogin`/`disableRootSSH` ignored in Run All | critical (PVE clusters) | [V] `security_manager.go:49`; harness (A12) | **Fix: decided** for PVE (`prohibit-password`) + **regression** for honouring the config key (v0.3.2 read `cfg.PermitRootLogin`) + approach (§3.17: string values) | Decided / Restores / Approval | P0.8 item 6; §3.17; `p2-pve` |
| V12 | `ssh.socket` not handled: a port change has no effect until `daemon-reload` + socket restart, while UFW allows only the new port | high (critical with V9) | [V] no socket code on origin/main; [V research] Ubuntu 24.04/26.04 socket-activated | **Fix: decided** (brief §2.3 step 4). v0.3.2 is **not** a working reference: its `listen.conf` was never followed by `daemon-reload` [I] | Safety | Phase 2 (`p2-reload`) |
| V13 | Drop-in named `hardn.conf` sorts after `50-cloud-init.conf` and other low-numbered files, so first-value-wins lets them override hardn's keys; status is read from files, not `sshd -T` | high | [V] `file_ssh_repository.go:42`; `security/status.go:232-264,455-487`; `disable_root_menu.go:208-254`; [I] precedence | **Fix: decided** (`00-hardn.conf`, brief §2.2; status from `sshd -T`) + approach (§3.17: migrate the legacy artifacts, §10) | Decided + Approval | §3.17; `p2-sshd-effective`, `p2-sshd-migrate`; Phase 4 |
| V14 | Alpine: `sudo` never installed; `adduser -D` likely locked for pubkey under non-PAM sshd | critical (Alpine) | [V] no sudo install (A14); v0.3.2 `user.go:36-56` had it; [I] lock | **Fix: regression** (v0.3.2 installed sudo) + approach (§3.21: doas vs sudo; account unlock) | Restores + Approval | §3.21; `p2-sudoers`, `p2-alpine` |
| V15 | Alpine: the whole `/etc/ssh/sshd_config` is replaced (sftp subsystem, `Include` and comments lost), with no backup | high | [V] harness (A2 + new finding: no `Subsystem sftp`) | **Fix: decided** (brief §2.2: hardn block or drop-in). v0.3.2 also replaced the file (with a backup) | Decided | `p2-alpine` |
| V16 | Sudoers grant: no `visudo -cf`; file name = raw user name (a `.` makes sudo skip it); non-atomic; mode only on create; `MkdirAll /etc/sudoers.d 0755`; overwrite drops `env_keep` | high | [V] `os_user_repository.go:233-264` (A13); [I] sudo skip rule | **Fix: safety** (validate, atomic, separate drop-ins) + approach (§3.17: sudoers file names) | Safety + Approval | §3.17; `p2-sudoers` |
| V17 | Users created without a password; `sudoNoPassword: false` then leaves no usable sudo, while root login is disabled in the same run | high | [V] `os_user_repository.go:112`; no `chpasswd` on any ref; [I] chain | **Fix + approach** (refuse, warn, or set a password; no §3 row yet) | Approval | new §3 row needed |
| V18 | Every write non-atomic; no backups on origin/main; no rollback anywhere; backup errors ignored in v0.3.2 | high | [V] `os_filesystem.go:17-26`; 0 `os.Rename`; `BackupFile` uncalled (A7) | **Fix: safety** (atomic write, brief §2.1) + **regression** (v0.3.2 took backups) + approach (§3.16: backup layout) | Safety + Restores | §3.16; `p2-atomic` |
| V19 | DNS: `resolved.conf` overwritten wholesale (not `resolved.conf.d`), `systemd-resolved` restarted, `domain`/`search lan` forced, empty nameservers accepted on CLI/Run All, NetworkManager not detected, the direct path writes through the `/etc/resolv.conf` symlink | medium | [V] code + harness; [I] runtime effects | **Fix: safety** (validate the list; atomic write) + **decided** (brief §2.5: manager-aware) + approach (drop the forced `lan`; use a drop-in; no §3 row yet) | Safety + Approval / Decided | `p2-dns` |
| V20 | `setup-sudo-env` uses a predictable temp file in `/tmp`, written as root (follows symlinks; `fs.protected_symlinks` usually mitigates [I]) | medium | [V] `file_environment_repository.go:72-75` | **Fix: safety** | Safety | `p2-sudoers` |
| V21 | Root writes `authorized_keys` through a user-controlled `~/.ssh` (symlink-following) and runs `sh -c "chown -R u:u <dir>"` with the user name interpolated; `/home/<u>` assumed | medium | [V] `file_ssh_repository.go:158-216`; `os_user_repository.go:162-209` | **Fix: safety** | Safety | `p2-atomic` |
| V22 | "Remove SSH key" is a no-op that reports success; the "Allow sudo access? n" answer is ignored (always sudo); keys re-appended with no dedupe on every sudo toggle | medium (a key the user thinks is revoked stays valid) | [V] `user_options.go:530-545, 680-690, 744`; `os_user_repository.go:86-90, 210-227` (A5) | **Fix: safety** (honour the stated choice). Intent evidenced by `archive/dropped-stash/2025-03-20-b371694`, which passed `hasSudo`; 6092ee3 reverted it (`inventory.md` A25) | Restores | slice: user management |
| V23 | Run All prints "AppArmor configured" and "Lynis security audit completed", but the code is dead | high (misreports security posture) | [V] `run_all_menu.go:269-275`; no callers (A4) | **Fix: safety** (stop the false claims now) + approach (§3.12: wire the features or drop them) | Approval | §3.12; slice 7 |
| V24 | Upgrade from v0.3.2: keys in `~/.ssh_<u>/authorized_keys` stop working after any origin/main SSH write; `manage.conf` and `ssh.socket.d/listen.conf` left behind; `disableRoot` renamed; `sshConfigFile` ignored | critical (upgraded hosts) | [V] code diff; [I] runtime | **Fix + approach** (§3.17 migration policy; §3.11 `disableRoot` alias) | Approval | P0.8 item 4; §3.11; §3.17; `p2-sshd-migrate` |
| V25 | CLI exits 0 after operation failures | medium | [V] `main.go:247-416` (A15) | **Fix: decided** (Phase 3 exit codes) | Decided | Phase 3 |
| V26 | Writes before flag parsing (`/var/log/hardn.log`, `/etc/hardn/hardn.yml.example`); an outbound HTTPS check and a root `/tmp` cache; rendering status can create `/etc/hardn/hardn.yml` and silences console logging | low–medium | [V] dynamic `--help` (first pass); code. **Verification added:** the `/tmp` cache can be planted by an unprivileged user (§9, D2) | **Fix: safety** (`/tmp` writes; the silent-mode leak) + approach (§3.23 lazy startup writes; §3.12 update check opt-in) | Safety + Approval | P0.8 item 5; §3.12; §3.23 |
| V27 | `ListenAddress 0.0.0.0` default makes sshd IPv4-only (IPv6 SSH stops) | medium | [V] `config.go:116`; [I] effect | **Fix + approach** (no §3 row yet) | Approval | new §3 row needed |

**By severity:** critical V1, V4, V5, V6, V8, V9, V10, V11, V14, V24 (and V12 when combined with V9); high V2, V3, V7, V12, V13, V15, V16, V17, V18, V23; medium V19, V20, V21, V22, V25, V27; low–medium V26.

---

## 9. Additional verified defects (verification pass; not numbered in the V-list)

These came out of the second pass. They are listed here so the Phase 0 review can decide whether to number them (V28+).

| # | Defect | Evidence | Related | Plan hook |
|---|---|---|---|---|
| D1 | **Run All option 1 runs live with no confirmation** (`7`, Enter, `1`, Enter); only option 2 asks for a typed `yes` | [V] `run_all_menu.go:142-144, 161-171` (A4) | interaction anti-patterns 1 and 8 | P0.8 item 3 |
| D2 | **An unprivileged local user can plant `/tmp/hardn-version-cache.json`.** Root never validates it: a future `last_check`, tag v99, a "security" body and any URL make root's hardn show "Update Binary <URL>" and exit. Root also writes fixed `/tmp` names (`hardn-version-cache.json`, `hardn_sudoers_temp`) with symlink-following `os.WriteFile` | [V] harness (TestZZ_A10 and the new-findings run) | V20, V26 | P0.8 item 5 |
| D3 | The update check runs **twice concurrently** (`main.go:204` and `main_menu.go:384`); both goroutines write `MainMenu` fields with no synchronisation while the menu loop reads them (a data race) | [V] code | V26 | §3.12 |
| D4 | `logging.SetSilentMode(true)` in `getConfigForSSHKeyPath` is never restored (`:32` commented out): after the first User Management render, **all `LogError`/`LogInfo`/`LogSuccess` console output is suppressed** | [V] `os_user_repository.go:22, 32` | table row 4 | slice: user management |
| D5 | Menu input mixes a shared `bufio.Reader` with direct `os.Stdin.Read` calls; bytes buffered by one are invisible to the other (pasted input can be lost or reordered) | [V] code `input.go:13-19, 36, 119`; effect not exercised [I] | plan risk R17 | `p1c-legacy-input` |
| D6 | Menu input **spins forever on EOF** (368 iterations in 2 s, 2 `stty` per iteration) and **drops Ctrl-C** (0x03) at every menu prompt; `q` quits with no Enter | [V] harness (A11) | interaction §3 | Phase 1C |
| D7 | Three components disagree on what `sshKeyPath` means: sshd reads the file `~/.ssh_<u>`; the user display reads `~/.ssh_<u>/authorized_keys` then falls back to `~/.ssh/authorized_keys`, using a fresh `LoadConfig("")` that ignores `-f`/`-u`; the writer uses `~/.ssh/authorized_keys` | [V] `file_ssh_repository.go:88-91`; `os_user_repository.go:20-35, 537-589` | V5 | §3.17 |
| D8 | `sshConfigFile` is dead: `model.SSHConfig.ConfigFilePath` is read once and never set, so the drop-in path is always hardcoded | [V] `file_ssh_repository.go:37` | V24 | §3.17 |
| D9 | v0.3.2's `ssh.socket.d/listen.conf` was written without `daemon-reload`, and no ref contains `daemon-reload`, so the override probably never took effect until a reboot | [V] grep; [I] systemd behaviour | V12 | `p2-reload` |

---

## 10. Legacy SSH artifacts a migration must handle (from verification C3)

Settled by `git grep`: v0.3.x never wrote `hardn.conf`; only `manage.conf` and `hardn.conf` ever appear as `sshd_config.d` names on origin/main's history.

| Artifact | Written by | Note |
|---|---|---|
| `/etc/ssh/sshd_config.d/manage.conf` | v0.3.1, v0.3.2 and main builds before 3cc6a2f | The v0.3.x `sshConfigFile` default |
| A custom `sshConfigFile` path | v0.3.x (honoured the key) | The old `hardn.yml` may still name it |
| `/etc/ssh/sshd_config.d/hardn.conf` | origin/main and main builds from 08a805a / 3cc6a2f on | Hardcoded |
| `/etc/systemd/system/ssh.socket.d/listen.conf` | v0.3.1, v0.3.2, pre-3cc6a2f builds | A systemd override, not an sshd drop-in |
| A fully generated `/etc/ssh/sshd_config` | Alpine, all three refs | v0.x backups under `backupPath` |

A host upgraded from v0.3.2 to an origin/main build can have **both** `manage.conf` and `hardn.conf`, because origin/main ignores the old key and never removes `manage.conf`. How the stale files interact with `00-hardn.conf` under sshd's include order has not been verified on a host.

---

## 11. Not verifiable here: written, not run (gate 5)

These need the disposable VM the maintainer will provide. Until they run there, they are reported as "written, not run".

- `ufw reset` without `--force` with stdin=/dev/null (V9).
- Ubuntu 24.04/26.04 `ssh.socket` port behaviour, and whether sessions survive an `ssh.socket` restart (V12).
- Alpine `adduser -D` lock vs pubkey under non-PAM sshd (V14).
- The effect of `systemctl`/`rc-service` restart on existing sessions with a broken sshd config (V7).
- Stock `Include` placement per distro version, and Alpine cloud images' sshd drop-ins (V13, V15).
- sudo-rs + `env_keep` on Ubuntu 26.04 (V16).
- Whether `fs.protected_symlinks` blocks the `/tmp` symlink attacks on each target (V20, D2).

---

## 12. Characterization order for P0.7

Feasibility is proven: `MockCommander.ExecutedCommands` and `MockFileSystem.Files` already let a Go test record exact argv and file contents per operation. There are **no adapter tests today**. Capture in this order, labelled "origin/main behaviour (baseline, decision 6)" and `KNOWN-DEFECT: Vn` where the V-list applies, then flip each golden deliberately in the fixing commit:

1. `HardenSystem` argv and files per {debian, ubuntu, proxmox, alpine} × {DryRun on, off} (V1, V6, V8–V11).
2. `DisableRootSSH` per distro (V4, V15).
3. The `-c` path `ConfigureSSH` with `cfg.SshKeyPath` (V5).
4. `ConfigureSecureFirewall` argv sequence (V9).
5. `ConfigureSudo` and `SetupSudoPreservation` file content and file names (V16, V20).
6. DNS: the three strategies, including empty nameservers (V19).
7. Menu flows with dry-run on: they must show **no** adapter calls once fixed (V2). This needs the menu reader injected (`next/p0-input-seam`).
8. Status functions against fixture `sshd_config` sets (V13).

---

## 13. Added in Phase 1

Side effects that new dependencies bring with them, recorded when the dependency lands. Line numbers are in the module source at the pinned versions.

### 13.1 Lip Gloss v2 and colorprofile at package initialisation (`next/p1a-term`)

**Scope.** The effects below run in every process that links `charm.land/lipgloss/v2` (the root package), before `main`. In `next/p1a-term` only `pkg/ui/term` imports it, and nothing in `cmd/hardn` imports `pkg/ui` yet (`go list -deps ./cmd/hardn` lists no `charm.land` or `charmbracelet` package) [V]. So the init effects do not reach the hardn binary yet; they arrive with the first unit that wires `pkg/ui` into `cmd/hardn`. The `pkg/ui` test binaries run them today.

**The one change linked into `cmd/hardn` now** [V `go list -deps ./cmd/hardn`, with this branch's `go.mod` and with the base one via `-modfile`]: MVS raises `github.com/clipperhouse/uax29/v2` from v2.2.0 to v2.7.0, and `cmd/hardn` links its `graphemes` package through `go-runewidth` v0.0.31 (`graphemes.FromString` at `runewidth.go:633`, `665`, `719`, `782`, `891`). Its grapheme-break tables move from Unicode 15.0.0 to 17.0.0 (`graphemes/trie.go:4` in each version). The ANSI-sequence handling new in v2.7.0 is opt-in and off by default (`graphemes/iterator.go:30-41`); `go-runewidth` does not set it. The package has no `init` function and imports only the standard library, so it adds no init effect. The existing goldens are unchanged.

**What runs at init** [V code: lipgloss v2.0.6, colorprofile v0.4.3, x/term v0.2.2, xo/terminfo v0.0.0-20220910002029-abceb7e1c41e]:

1. `var Writer = colorprofile.NewWriter(os.Stdout, os.Environ())` (lipgloss `writer.go:14`). `NewWriter` calls `Detect` (colorprofile `writer.go:23-28`).
2. `Detect` (colorprofile `env.go:33-54`):
   - copies the environment and checks whether stdout is a terminal with one `ioctl` (`TIOCGETA` on darwin and the BSDs, `TCGETS` on Linux; x/term `term_unix.go:14-16`, `term_unix_bsd.go`, `term_unix_other.go`). When `TTY_FORCE` parses as true with `strconv.ParseBool` (`1`, `t`, `T`, `true`, `TRUE`, `True`), stdout counts as a terminal and the `ioctl` is skipped (`env.go:36`, `130-133`);
   - stops there when the environment alone gives TrueColor or `NO_COLOR` parses as true (`env.go:40-43`);
   - otherwise, **only when stdout is a terminal or `TTY_FORCE` is true, and `TERM` is set and not `dumb`** (`env.go:36`, `45`). With `TTY_FORCE` both steps below run even when stdout is redirected:
     - **reads terminfo:** `terminfo.Load(TERM)` (`env.go:208-229`) calls `user.Current()` for the home directory (xo/terminfo `load.go:38`) and then tries `<dir>/<first letter>/<TERM>` and `<dir>/<first byte in hex>/<TERM>` (`terminfo.go:222-226`) in `$TERMINFO`, `~/.terminfo`, `$TERMINFO_DIRS`, `/etc/terminfo`, `/lib/terminfo` and `/usr/share/terminfo` until one exists (`load.go:22-58`). Without cgo, as in hardn's release builds (`CGO_ENABLED=0` in `.slsa-goreleaser.yml`) and `make check-cross`, `user.Current()` opens and reads `/etc/passwd` (Go 1.27.2 `os/user`: `lookup_stubs.go:23-27`, `lookup_unix.go:227-234`, `lookup.go:10`); with cgo it calls libc `getpwuid_r` (`cgo_lookup_cgo.go:27`). The result is cached for the process (`lookup.go:21-22`);
     - **runs `tmux info` when `$TMUX` is non-empty:** `exec.CommandContext(context.Background(), "tmux", "info").Output()` (`env.go:239-262`), so `tmux` is found through `$PATH` and there is no timeout. Whether `TMUX` survives `sudo` depends on the host's sudoers environment policy; not checked here.
3. In-memory only: x/ansi reads `RUNEWIDTH_EASTASIAN` (`method.go:20-25`) and builds width tables (`wcwidth.go:24`); ultraviolet fills a key map (`decoder.go:1320`); x/termios fills option tables (`syscalls_linux.go:8`, `syscalls_darwin.go:8`).

Nothing at init writes a file, changes a terminal mode or sends anything to the terminal.

**How hardn handles it.** The init-time `Detect` cannot be switched off while Lip Gloss is linked; hardn ignores its result. `pkg/ui/term` never uses `lipgloss.Writer` or `lipgloss.Print*`/`Fprint*`/`Sprint*` (the `Fprint*` functions run `Detect` again on every call, `writer.go:67-97`). Its profile comes from `term.Resolve`, which calls only `colorprofile.Env` (environment variables; no isatty, terminfo or tmux), and `Env.Writer` is a `colorprofile.Writer` with that profile fixed.

**Background query: not at init.** The terminal is queried only by `term.Detect`, and only when the user opts in (`--theme=auto`, `HARDN_THEME=auto`) with stdin and stdout both terminals and plain mode off. `lipgloss.HasDarkBackground` then switches the terminal to raw mode, with ISIG, ICANON and ECHO off (lipgloss `query.go:13`; x/term `term_unix.go:31`), for up to 2 s per descriptor (`defaultQueryTimeout`, lipgloss `terminal.go:49`), stdin and then stdout (`query.go:60-64`), and reports dark on any error (`query.go:86-91`). Type-ahead is read and discarded, Ctrl-C does not interrupt, and a late reply stays in stdin. `charm.land/lipgloss/v2/compat` runs this query at import (`compat/color.go:13`) as well as a second `Detect` (`compat/color.go:16`), so hardn must never import it (depguard rule planned in `next/p1d-lint`).
