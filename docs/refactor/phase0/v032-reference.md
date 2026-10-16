# v0.3.2 reference behaviour

> **Phase 0 appendix (P0.7b).** This is a read-only description of tag `v0.3.2` (commit `bce7cc2`). It changes no behaviour.
>
> **Role (maintainer decision 6).** origin/main (`9d46866`) is the baseline for wording *and* behaviour. Use this file as the reference **only where origin/main regressed**; §5 says where that is, V-number by V-number (V-numbers from the side-effect inventory, §7). Behaviour on the V-list is a **defect to fix in both versions, never behaviour to preserve**. §6 lists v0.3.2's own defects, which must not be carried over.
>
> **How a vertical slice uses this.** Its parity table compares the new path with origin/main. For every row where §5 says "regressed", it also compares against the v0.3.2 end state given here, and ties each difference to a V-number or an approved proposal.

**Citations and labels.**
- A bare `path:line` cites tag `v0.3.2`. origin/main citations are prefixed `origin/main:`.
- **[H]**: also observed in the scratch harness (§1). This is real v0.3.2 code run against fake commands.
- **[P]**: parsed with a local OpenSSH 9.9p2 `sshd -G -f <file>`, run as a normal user. It only parses; nothing listens.
- **[I]**: inferred, and needs a disposable VM (gate 5). It has been written, not run.
- Fixture values are synthetic: user `alice`, port `2230`, key `<key>`, and nameservers `192.0.2.53`, `198.51.100.53` and `203.0.113.53`.

---

## 1. Method

- **Source.** The tag was exported with `git archive v0.3.2` from a read-only checkout. No hardn binary was run.
- **Harness (scratch only, never committed).** It used a copy of the v0.3.2 tree with two kinds of change:
  - Every absolute `"/etc/…"` and `"/home/…"` string literal in `pkg/{ssh,user,firewall,dns,packages,security,updates,osdetect}` was prefixed with a scratch root. That's 41 changed lines and no logic change.
  - The `uid != 0` check (`cmd/hardn/main.go:115`) was skipped, but only while that scratch root was set.
- **No real commands ran.** `PATH` pointed only at a directory of recording shims. Each shim logs its argv and exits 0 unless a scenario sets otherwise. An `apt-get install X` or `apk add X` creates a shim named `X` when the scenario says that package provides it.
- **How scenarios were driven.**
  - Each CLI scenario ran in its own process through the real cobra root command, `hardn -f <fixture> <flags>`. That covers 71 scenarios: four distro families × `-r -n-r -c -n-c -d -n-d -w -n-w -g -n-g -s -l -i`, plus edge cases.
  - Menu functions were called directly, with stdin redirected from a file holding the answers: 10 scenarios.
- **Fixture config.** It's the shipped example config (`pkg/config/embed.go:13-186`) loaded over `DefaultConfig()`, exactly as `LoadConfig` does, with the synthetic overrides above. `dmzSubnet` was set to `192.0.2`, so no local address matched and the "not DMZ" branch ran.
- **Harness limits.**
  - Shims succeed unless told otherwise, so error branches are shown only where noted.
  - `adduser` creates no home directory, so `/home/alice` showing mode 0700 on Alpine is a harness artefact; a real `adduser` creates it.
  - The umask was 022.
  - Fast shims make "same second" timing certain; on a host it's only likely [I].
  - Directory modes come from `os.MkdirAll`.

---

## 2. Global behaviour (every operation)

1. **Writes before argument parsing**, even for `--help`, `--version` and `-n`:
   - `InitLogging("/var/log/hardn.log")` (`main.go:55`; `logging.go:18-41`): `MkdirAll` 0755, then `OpenFile(O_CREATE|O_APPEND, 0644)`.
   - `EnsureExampleConfigExists` (`main.go:58`; `embed.go:190-210`) writes `/etc/hardn/hardn.yml.example` 0644 if it's missing.
2. **Root check, then config load.**
   - Non-root exits 1 (`main.go:109-120`).
   - Search order (`config.go:238-293`):
     1. `-f`
     2. `$HARDN_CONFIG`
     3. `/etc/hardn/hardn.yml`
     4. `~/.config/hardn/hardn.yml`
     5. `~/.hardn.yml`
     6. `./hardn.yml`

     An explicit path or env path that is missing does not fall back.
   - If no file is found, it prompts `[Y/n]` on stdin (empty means yes) and writes `/etc/hardn/hardn.yml` as root (`config.go:303-311, 358-431`), whatever `-n` says.
   - **That saved file is `DefaultConfig()` plus four answers.** It has no `sshAllowedUsers`, no `nameservers`, no repo lists and no package lists (`config.go:93-153`). See §6 items 8 and 10.
3. **`cfg.DryRun = dryRun`** (`main.go:142`). A `dryRun: true` in the YAML is overridden by the flag's default `false` [H `debian-g-yaml-dryrun`: it applied].
4. **Username.** `-u` overrides `username`. `-c` and `-r` with no username exit 1 (`main.go:145-153`).
5. **OS detection** (`osdetect.go:22-78`).
   - Only `ID=alpine` takes the Alpine branches. **Every other ID, including unsupported ones, takes the Debian/Ubuntu branches**; there's no rejection.
   - Proxmox means `/etc/pve` exists (`:72-75`), and `OsType` stays `debian`.
   - No operation tells Debian from Ubuntu.
6. **Flag dispatch** (`main.go:162-250`).
   - No operation flag opens the menu. `-r` runs `runAllHardening` and returns, so other flags are ignored.
   - Otherwise, in this order:
     1. Sources, if any of `-s -l -i -a -c` is set (`:177-182`).
     2. `-d` (`:184-191`).
     3. Python packages for `-i`/`-a` (`:193-195`).
     4. Linux packages for `-l`/`-a` (`:197-199`).
     5. `-c`: sudo, user, SSH (`:201-218`).
     6. `-w` (`:220-222`).
     7. `-g` (`:224-226`).
     8. `-p`.
   - A success line is always printed (`:245-250`).
7. **Exec model.**
   - Commands run as `exec.Command(name, argv…)`. There are two shell strings:
     - `sh -c "yes | ufw enable"` (`firewall.go:113`);
     - `su - <u> -c "<string>"` (`user.go:167,174,181,187`).
   - Go's `os/exec` connects a nil Stdin or Stdout to the null device. So every `.Run()` gets stdin from `/dev/null` and its output is discarded.
   - Output is captured only where it's parsed or reported: `dpkg -l` and `dpkg-query` (`packages.go:22, 211, 246`), the UFW default policies (`firewall.go:78, 86`) and `InstallPackages` (`packages.go:286-325`).
8. **Error handling.**
   - Inside a function, a failed step either returns (aborting the rest of that function) or is logged and skipped; §4 says which for each step.
   - **Callers drop almost every error.** `main.go` checks only `CreateUser` (`:213-216, :273-276`) and `-d`'s `DisableRootSSHAccess` (`:185-190`). It ignores the return of `WriteSources`, `WriteProxmoxRepos`, `InstallPackages`, `InstallPythonPackages`, `WriteSSHConfig`, `ConfigureUFW`, `ConfigureDNS`, `SetupAppArmor`, `SetupLynis`, `SetupUnattendedUpgrades` and `SetupHushlogin`.
   - Two silent failures seen in the harness: a failed `uv pip install` (`debian-i-uv`), and a `resolved.conf` write into a missing `/etc/systemd`.
   - The process exits 0 after any operation failure.
9. **Writes.** Every write is a direct `os.WriteFile`:
   - it truncates in place and follows symlinks;
   - the mode applies only when the file is created;
   - there's no temp file plus rename and no fsync;
   - there's no validation: no `sshd -t`, no `visudo -c`.
10. **Menu render probes (read-only).** Every main-menu render runs `ufw status verbose`, `iptables -L`, `apk info -e apparmor` / `rc-status default` or `aa-status`, `dpkg -l unattended-upgrades`, `systemctl is-enabled unattended-upgrades` and `which sudo`, and reads `manage.conf` or `sshd_config` (`pkg/status/status.go:182-403`). They run in dry-run too.

---

## 3. Backups (`utils.BackupFile`, `pkg/utils/utils.go:50-92`)

- **When it runs.** It skips when `enableBackups: false` (`:51-54`; the default is `true`, `config.go:99`) or under dry-run (`:56-59`).
- **Order.**
  1. `MkdirAll(<backupPath>/<YYYY-MM-DD>, 0755)` (`:62-65`). This runs *before* the existence check, so the dated directory appears even when there's nothing to back up.
  2. If the source is missing, log and return (`:68-71`).
  3. Copy to `<backupPath>/<YYYY-MM-DD>/<basename>.<HHMMSS>.bak` with mode **0644** (`:74-88`).
- `backupPath` defaults to `/var/backups/hardn` (`config.go:100`).
- **Defects (not reference):**
  - Only the basename and a seconds timestamp are kept. **A second backup of the same file in the same second overwrites the first.** In Run All the original is lost [H `alpine-r`, `menu-runall-alpine`, `debian-r-existing-manage`]. The only file holding the stock `sshd_config` (or the previous `manage.conf`) ended up containing hardn's own output.
  - Sudoers backups are 0644 (world-readable).
  - All 15 callers ignore the returned error.
  - Nothing restores. `pkg/backup/backup.go` (`ListBackups`, `RestoreBackup`, `CleanupOldBackups`) is imported nowhere.

| # | File backed up | Call site | Operation | Families |
|---|---|---|---|---|
| 1–2 | `/etc/sudoers.d/<u>` | `user.go:80`, `:150` | create user | Alpine, Deb/Ubu/PVE |
| 3 | `/etc/ufw/applications.d/hardn` | `firewall.go:180` | UFW app profiles | all |
| 4 | `/etc/ssh/sshd_config` | `ssh.go:51` | SSH write | Alpine |
| 5 | `/etc/systemd/system/ssh.socket.d/listen.conf` | `ssh.go:94` | SSH write | Deb/Ubu/PVE |
| 6 | `<sshConfigFile>` | `ssh.go:124` | SSH write | Deb/Ubu/PVE |
| 7 | `/etc/ssh/sshd_config` | `ssh.go:196` | disable root | Alpine |
| 8 | `<sshConfigFile>` | `ssh.go:250` | disable root | Deb/Ubu/PVE |
| 9–10 | `/etc/apt/sources.list` | `packages.go:114`, `:133` | sources | PVE, Deb/Ubu |
| 11 | `/etc/apt/sources.list.d/ceph.list` | `packages.go:168` | PVE repos | PVE |
| 12 | `/etc/apt/sources.list.d/pve-enterprise.list` | `packages.go:189` | PVE repos | PVE |
| 13 | `/etc/systemd/resolved.conf` | `dns.go:48` | DNS | resolved hosts |
| 14 | `/etc/resolvconf/resolv.conf.d/head` | `dns.go:68` | DNS | resolvconf hosts |
| 15 | `/etc/resolv.conf` | `dns.go:93` | DNS | direct |

**Not backed up:** `/etc/apk/repositories` (`packages.go:92`), `/etc/periodic/daily/apk-upgrade`, `~/.hushlogin`, and anything written under a user's home.

---

## 4. Operations

### 4.1 Create user (+ sudo install, hushlogin)

**Triggers:**
- CLI `-c` (`main.go:201-218`);
- CLI `-r` (`main.go:272-277`);
- menu 1, `userCreationMenu` (`menu.go:317-349`);
- menu Run All (`menu.go:651-653`, skipped if `username` is empty).

**CLI `-c`, in order** [H `debian-c`, `alpine-c`]:
1. **Package sources are rewritten** (§4.6). `-c` always does this (`main.go:177-182`).
2. **sudo package** (`main.go:202-211`; both branches are identical).
   - `IsPackageInstalled("sudo")` (`packages.go:16-37`) runs `dpkg -l sudo` if dpkg is on PATH. The package counts as installed only if dpkg exits 0 *and* the output contains `sudo`. Otherwise it tries `apk info -e sudo`.
   - If sudo isn't installed: `InstallPackages(["sudo"])` (§4.7, with the locale steps and the Proxmox hold/unhold).
3. **`user.CreateUser`** (`user.go:18-195`).
   - **If `user.Lookup` succeeds**, it returns nil: no group, no sudoers file, no keys, no hushlogin (`:19-24`) [H `debian-c-existing`, `alpine-c-existing`].
   - Dry-run logs only (`:28-34`).
   - If `exec.LookPath("sudo")` fails:
     - Alpine runs `apk add sudo`.
     - Deb/Ubu/PVE run `apt-get update` then `apt-get install -y sudo` (`:36-56`).

     In CLI `-c` this fires only if step 2 didn't already provide sudo [H `alpine-c-sudo-present`, `menu-user-*`].
4. **`ssh.WriteSSHConfig`** (§4.2) runs unconditionally, even for an existing user (`main.go:217`).

**Debian / Ubuntu / Proxmox argv** (new user) [H `debian-c`; Proxmox adds the hold/unhold of §4.7 around the install, H `proxmox-c`]:
```
dpkg -l sudo
<InstallPackages(sudo): apt-get update … apt-get install --yes sudo … (§4.7)>
adduser --disabled-password --gecos "" alice
usermod -aG sudo alice
su - alice -c "mkdir -p ~/.ssh_alice && chmod 700 ~/.ssh_alice"
su - alice -c "echo '<key>' >> ~/.ssh_alice/authorized_keys"      # one per sshKeys entry
su - alice -c "chmod 600 ~/.ssh_alice/authorized_keys"
su - alice -c "touch ~/.hushlogin"
systemctl restart ssh                                              # WriteSSHConfig
```

**Alpine argv** [H `alpine-c`]:
```
apk update                         # sources rewrite (§4.6)
apk info -e sudo
apk add --no-cache sudo            # only if not installed
adduser -D -g "" alice
addgroup alice wheel
chown -R alice:alice /home/alice/.ssh_alice
chown alice:alice /home/alice/.hushlogin
rc-service sshd restart            # WriteSSHConfig
```

**Files.**
- **Both families:**
  - `MkdirAll /etc/sudoers.d 0755`.
  - `/etc/sudoers.d/<u>` with mode **0440** and content `alice ALL=(ALL) NOPASSWD: ALL\n`. That's for `sudoNoPassword: true`, the default (`config.go:115`); otherwise it's `alice ALL=(ALL) ALL\n`.
  - The file is backed up first if it exists (`user.go:74-91`, `:144-161`).
- **Deb/Ubu/PVE:** the key directory is `~/<sshKeyPath with %u→user>`, so `~/.ssh_alice` by default (`user.go:164`). The user creates it via `su`.
- **Alpine (written as root):**
  - The home is hard-coded to `/home/<u>` (`:95`).
  - `MkdirAll /home/<u>/.ssh_<u> 0700` (`:99`).
  - `authorized_keys` is mode **0600**: keys joined by `\n`, plus a trailing `\n`, **overwriting** any existing file (`:104-108`).
  - `.hushlogin` is created by `os.Create`, giving 0644 under umask 022 [H] (`:117-127`).

**Error handling.**
- `adduser` failure returns (`:61-63`, `:131-133`).
- Group-add failure is logged and skipped.
- Sudoers mkdir or write failure returns.
- On Deb/Ubu, failed `su` steps are logged and skipped.
- On Alpine, failed key mkdir or write returns, and failed `chown` is logged.
- The caller logs the `CreateUser` error and **still writes SSH config**.

**Dry-run:**
- CLI `-n -c` runs only `dpkg -l sudo` (or `apk info -e sudo`) and `user.Lookup`, and writes nothing [H `debian-n-c`].
- The menu path honours dry-run through the same functions.

**Menu 1 vs CLI.**
- Menu 1 prompts for a username if it's empty.
- It does **not** rewrite sources and doesn't pre-install sudo through `InstallPackages`, so `CreateUser`'s own `apt-get update` / `apt-get install -y sudo` (or `apk add sudo`) runs instead.
- It then writes SSH config and logs errors (`menu.go:337-345`) [H].

### 4.2 SSH config write (`ssh.WriteSSHConfig`, `ssh.go:17-171`)

**Triggers:**
- `-c` (`main.go:217`);
- `-r` (`main.go:280`);
- menu 1 (`menu.go:343`);
- menu Run All (`menu.go:656`).

**Debian / Ubuntu / Proxmox** [H `debian-c`, `ubuntu-c`, `proxmox-c`; identical output]:
1. `BackupFile(/etc/systemd/system/ssh.socket.d/listen.conf)` (`:94`).
2. `MkdirAll /etc/systemd/system/ssh.socket.d 0755` (`:97`).
3. Write `listen.conf` with mode 0644 (`:102-110`). On failure it logs "Failed to set ssh port listener." and returns.
   ```
   [Socket]
   ListenStream=
   ListenStream=2230
   ```
4. `MkdirAll(dir(sshConfigFile), 0755)`, then `BackupFile(sshConfigFile)` (`:113-124`).
5. Write **`sshConfigFile`**, mode 0644 (`:126-159`). The default is `/etc/ssh/sshd_config.d/manage.conf` (`config.go:112`), and **a configured path is honoured**. The content is below.
6. `systemctl restart ssh` (`:162-165`).
   - There's no `daemon-reload`, no `ssh.socket` restart and no `sshd -t`.
   - [I] So `listen.conf` does nothing on hosts that aren't socket-activated (Debian and Proxmox by default). On socket-activated Ubuntu it takes effect only at the next daemon-reload or reboot.

**Debian-family drop-in (file 5).** This is the verbatim output for alice and port 2230. One comment line is elided; it's at `pkg/ssh/ssh.go:138`, and sshd ignores it. The `AuthorizedKeysFile` separators are 4 spaces.
```
### Reference
### https://cryptsus.com/blog/how-to-secure-your-ssh-server-with-public-key-elliptic-curve-ed25519-crypto.html

Protocol 2
StrictModes yes

ListenAddress 0.0.0.0:2230

AuthenticationMethods publickey
PubkeyAuthentication yes

HostbasedAcceptedKeyTypes ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519
#PubkeyAcceptedKeyTypes <elided comment; see pkg/ssh/ssh.go:138>

PermitRootLogin no
AllowUsers alice

# To disable tunneled clear text passwords, change to no here!
PasswordAuthentication no
PermitEmptyPasswords no

#AuthorizedKeysFile /etc/ssh/authorized_keys
# mkdir custom SSH path (e.g., /home/$USERNAME/$SSH_KEY_PATH)
AuthorizedKeysFile    .ssh/authorized_keys    .ssh_%u/authorized_keys

### PVE ONLY: DO NOT DISABLE
#X11Forwarding yes
#AuthorizedKeysFile /etc/pve/priv/authorized_keys
```

**Alpine** [H `alpine-c`]:
1. `BackupFile(/etc/ssh/sshd_config)` (`:51`).
2. **Replace the whole** `/etc/ssh/sshd_config`, mode 0644 (`:60-83`). The content is below.
3. `rc-service sshd restart` (`:86-89`).

The stock `Include /etc/ssh/sshd_config.d/*.conf`, `Subsystem sftp internal-sftp`, `AllowTcpForwarding no` and the rest are lost [P: the parsed output has no `subsystem` line; I: SFTP stops working].
```
Protocol 2
StrictModes yes

Port 2230
ListenAddress 0.0.0.0:2230

AuthenticationMethods publickey
PubkeyAuthentication yes

HostbasedAcceptedKeyTypes ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519

PermitRootLogin no
AllowUsers alice

PasswordAuthentication no
PermitEmptyPasswords no

AuthorizedKeysFile    .ssh/authorized_keys    .ssh_%u/authorized_keys
```

**Value semantics (both families).**
- **`ListenAddress`** is `sshListenAddress:sshPort`, unless the address already contains `:` (`ssh.go:43-46`).
  - The Debian drop-in has **no `Port` line**, so an IPv6 value such as `::` is written bare and sshd listens on its default port, not `sshPort` [H `debian-c-listen-v6`; P `listenaddress [::]:22`].
  - The default `0.0.0.0` (`config.go:110`) means IPv4 only (V27).
- **`PermitRootLogin`** is `yes` exactly when `permitRootLogin: true`, otherwise `no` (`:54-57`, `:118-121`). Proxmox is not special-cased.
- **`AllowUsers`** is `sshAllowedUsers` joined by spaces, and is **written even when the list is empty**, as `AllowUsers ` (`:72`, `:141`).
  - [P] OpenSSH 9.9p2 rejects it: `no argument after keyword "AllowUsers"`, exit 255. OpenSSH 10.2p1's `servconf.c` makes it fatal: "no AllowUsers specified".
  - `DefaultConfig` leaves the list empty (`config.go:109`) [H `debian-c-defaultconfig`], and the interactive default-config prompts never ask for it (`config.go:380-415`).
  - The list comes from config, **not from `-u`** [H `debian-c-existing`: `-u nobody` still wrote `AllowUsers alice`].
- **`AuthorizedKeysFile`** lists **both** `.ssh/authorized_keys` and `<sshKeyPath>/authorized_keys` (default `.ssh_%u`) [P `authorizedkeysfile .ssh/authorized_keys .ssh_%u/authorized_keys`].
  - This is global, so existing `~/.ssh/authorized_keys` keys keep working.
  - hardn-created users have their keys at the second path (§4.1).
- **Key-only.** `AuthenticationMethods publickey`, `PasswordAuthentication no` and `PermitEmptyPasswords no` are always written (V6).
- **Obsolete keywords.** `HostbasedAcceptedKeyTypes` is accepted as an alias [P], and `Protocol` is ignored (10.2p1 `servconf.c`: `sIgnore`).
- **Precedence** [I; sshd_config(5) and the audit's ssh research].
  - `manage.conf` sorts **after** `50-cloud-init.conf` and `60-cloudimg-settings.conf`, so for first-value keys those files win (V13).
  - `ListenAddress` and `AllowUsers` accumulate across the main file and every drop-in.

**Dry-run** (`:18-38`). It logs only: no exec, no write [H]. The preview text doesn't match the file (for example "Listen Address: 0.0.0.0" and "PermitRootLogin: false").

**Errors.** Any mkdir, write or restart failure returns; backup errors are ignored. Callers ignore the return except menu 1.

### 4.3 Disable root SSH (`ssh.DisableRootSSHAccess`, `ssh.go:174-300`): in-place edit

**Triggers:**
- `-d` (`main.go:184-191`);
- menu 2 after `y` (`menu.go:352-381`);
- Run All when `disableRoot: true` (`main.go:283-285`; `menu.go:659-661`). This runs after §4.2 has already written `PermitRootLogin` from config.

**Target file.**
- Alpine: `/etc/ssh/sshd_config`.
- Deb/Ubu/PVE: **only `cfg.SshConfigFile`** (`manage.conf` by default). The main `sshd_config` and other drop-ins are not touched.
- A missing target returns `SSH config file <path> not found` and changes nothing (`:246-248`) [H `debian-d-missing`].

**Sequence:**
1. `Stat`
2. `BackupFile`
3. `ReadFile`
4. Line edit
5. `WriteFile` 0644
6. `systemctl restart ssh` (Deb/Ubu/PVE) or `rc-service sshd restart` (Alpine)

Steps 4–6 run even when nothing changes.

**Edit rules** (`:205-229`, `:259-283`). The file is split on `\n` and rejoined, so the trailing newline is kept. For each line:
- A line that **begins with exactly** `PermitRootLogin yes` is replaced by `PermitRootLogin no`. Anything after it on the line is dropped.
  - These are **not** matched: leading whitespace, two spaces or a tab, `prohibit-password`, other casing, comment lines.
- A line that begins with `AllowUsers` and names at least one user becomes `AllowUsers ` plus every user except `root`, single-spaced. **`AllowUsers root` becomes `AllowUsers `**, which sshd rejects [H+P `debian-d`].
- Every other line stays byte-identical.

[H `debian-d`] Before → after:
```
ListenAddress 0.0.0.0:2230          ListenAddress 0.0.0.0:2230
PermitRootLogin yes                 PermitRootLogin no
  PermitRootLogin yes                 PermitRootLogin yes        (unchanged)
#PermitRootLogin yes                #PermitRootLogin yes         (unchanged)
PermitRootLogin  yes                PermitRootLogin  yes         (unchanged)
AllowUsers root alice               AllowUsers alice
AllowUsers root                     AllowUsers                   (sshd: fatal)
PasswordAuthentication no           PasswordAuthentication no
```

**Dry-run** (`:175-187`). It logs only. But:
- the CLI then prints `[SUCCESS] Disabled root SSH access` (`main.go:189`) [H `debian-n-d`];
- **menu 2 still restarts sshd** (`menu.go:362-371`) [H `menu-disable-root-dryrun-{debian,alpine}`].

Outside dry-run, menu 2 restarts **twice** [H].

**Errors.** They're returned. `-d` logs them; Run All ignores them.

### 4.4 UFW (`firewall.ConfigureUFW`, `firewall.go:17-133`)

**Triggers:**
- `-w` (`main.go:220-222`);
- menu 4 after `y` (`menu.go:495-516`);
- Run All when `enableUfwSshPolicy: true`. That's `false` by default (`config.go:129`).

**argv** [H `debian-w`, `proxmox-w` (identical), `alpine-w`]:
```
apt-get install -y ufw                 # Deb/Ubu/PVE, only if `ufw` is not on PATH; no apt-get update first
apk add ufw                            # Alpine equivalent
ufw default deny incoming              # failure → return (:77-82)
ufw default allow outgoing             # failure → return (:85-90)
ufw allow 2230/tcp comment SSH         # failure is ONLY LOGGED (:94-100)
ufw allow "from any to any app 'LabHTTPS'"   # one per profile; ONE argv element (:199); failure logged
sh -c "yes | ufw enable"               # failure → return (:113-116)
rc-update add ufw default              # Alpine only; failure logged
rc-service ufw start                   # Alpine only; failure logged
```

**What it doesn't do:**
- no `ufw disable`;
- no `ufw reset`, so **existing rules are kept**;
- no allow for the port sshd is listening on now, or for 22;
- no `ufw status` check;
- no Proxmox gating (V10).

**Ordering hazards** [H `debian-w-allow-fails`; effect I]:
- On an already-active UFW, `default deny incoming` applies before the SSH allow.
- `ufw enable` runs **even after the SSH allow failed**.

**App profiles** (`WriteUfwAppProfiles`, `:136-209`).
- Nothing happens if `ufwAppProfiles` is empty. That's the default (`config.go:119`); the example config has one profile.
- Otherwise: `MkdirAll /etc/ufw/applications.d 0755`, then `BackupFile`, then write `/etc/ufw/applications.d/hardn` with mode 0644:
  ```
  [LabHTTPS]
  title=Lab Web Server (HTTPS)
  description=Lab Web server secure port
  ports=30443/tcp

  ```
- The allow argv is a single string, which [I] ufw will reject as one token.

**Dry-run** (`:18-39`). It logs only, with no exec and no install [H `debian-n-w`]. It prints a recommendation when `sshPort == 22`.

**Errors.** The CLI and Run All ignore the return; menu 4 logs it.

### 4.5 DNS (`dns.ConfigureDNS`, `dns.go:16-110`)

**Triggers:**
- `-g` (`main.go:224-226`);
- menu 3 after `y`;
- Run All when `configureDns: true` (default `false`).

**Strategy**, chosen the same way in dry-run (`:27-38`, `:45-106`):
1. `systemctl is-active systemd-resolved` exits 0 → **resolved**.
2. Otherwise, `resolvconf` is on PATH → **resolvconf**.
3. Otherwise → **direct**.

On Alpine `systemctl` is absent, so the exec fails and selection falls through [H `alpine-g`].

| Strategy | Writes (mode 0644, overwrite) | Then | Backup | Nameservers written |
|---|---|---|---|---|
| resolved [H `ubuntu-g`] | `/etc/systemd/resolved.conf` = `[Resolve]\nDNS=192.0.2.53 198.51.100.53 203.0.113.53\nDomains=lan\n` (no `MkdirAll`) | `systemctl restart systemd-resolved` | `dns.go:48` | **all** |
| resolvconf [H `debian-g-resolvconf`] | `MkdirAll /etc/resolvconf/resolv.conf.d 0755`; `head` = `domain lan\nsearch lan\nnameserver 192.0.2.53\nnameserver 198.51.100.53\n` | `resolvconf -u` | `dns.go:68` | **first 2** |
| direct [H `debian-g`, `proxmox-g`, `alpine-g`] | `/etc/resolv.conf` with the same 4 lines as `head` (written through a symlink [I]) | — | `dns.go:93` | **first 2** |

**Empty `nameservers`.** That's the `DefaultConfig` case (`config.go:104`).
- Dry-run panics at `dns.go:21` [H `debian-n-g-empty-ns`].
- resolved writes `DNS=` and restarts [H `ubuntu-g-empty-ns`].
- resolvconf and direct take the backup, then **panic** at `:72` or `:97` before writing [H `debian-g-empty-ns`].

**Errors.** Write and command failures return. The CLI and Run All ignore them silently [H].

**Expected strategy per family** [I; audit research §8]:
- Ubuntu: resolved.
- Debian standard install: direct.
- Proxmox VE: direct (PVE also manages the file).
- Alpine: direct (udhcpc may rewrite it).

### 4.6 Package sources (`WriteSources`, `packages.go:40-144`; `WriteProxmoxRepos`, `:147-198`)

**Triggers:**
- `-s -l -i -a -c` (`main.go:177-182`);
- `-r` (`main.go:263-266`);
- menu 9 (`menu.go:544-562`);
- menu Run All (`menu.go:586-589`).

`CODENAME` is replaced by `VERSION_CODENAME`, one entry per line.

| Family | File (0644) | Content | Backup | Then |
|---|---|---|---|---|
| Debian, **Ubuntu** | `/etc/apt/sources.list` | `debianRepos`. The example gives `deb http://deb.debian.org/debian <codename> main contrib non-free`, plus `-security` and `-updates` lines. **Ubuntu gets deb.debian.org lines with its own codename** [H `ubuntu-s`] | `:133` | — |
| Proxmox | `/etc/apt/sources.list` | `proxmoxSrcRepos` (example: `embed.go:134-138`) | `:114` | `WriteProxmoxRepos` (`main.go:179-181`): `MkdirAll /etc/apt/sources.list.d 0755`; `ceph.list` = `proxmoxCephRepo`; `pve-enterprise.list` = `proxmoxEnterpriseRepo`. The example's enterprise entry is a commented line. Backups at `:168`, `:189` [H `proxmox-s`] |
| Alpine | `/etc/apk/repositories` | `…/alpine/v<major.minor>/main` and `…/community`, **repeated** under `# Security updates`, plus `…/edge/testing` when `alpineTestingRepo` is set | **none** | `apk update` (failure → return) |

**Defects.**
- **When the repo lists are absent (`DefaultConfig`), `sources.list`, `ceph.list` and `pve-enterprise.list` are truncated to empty files** [H `debian-c-defaultconfig`, `proxmox-c-defaultconfig`].
- deb822 `*.sources` files are not handled [I].
- Alpine dry-run slices `VERSION_ID` at its last `.` without checking for one (`:44-45`).

**Dry-run.** It logs only.

### 4.7 Package installs, including Proxmox holds and unholds

**`InstallPackages`** (`packages.go:271-355`).
- An empty list is a no-op. Dry-run logs only.
- Alpine: `apk add --no-cache <pkgs…>`. Failure returns.
- Debian / Ubuntu / Proxmox [H `debian-l`, `proxmox-l`]:
```
[PVE] for prefix in proxmox, pve:  dpkg-query -W -f=${binary:Package}\n   # \n is a real newline
                                   apt-mark hold <every installed package starting with prefix>
apt-get update                                   # failure → return (holds stay)
apt-get install --yes locales                    # failure logged
sed -i /en_US.UTF-8/s/^# //g /etc/locale.gen     # failure logged
locale-gen                                       # failure logged
apt-get install --yes <pkgs…>                    # failure → return (holds stay)
apt-get autoremove --yes                         # failure logged
apt-get clean                                    # failure logged
rm -rf /var/lib/apt/lists/*                      # literal "*", no shell: [I] removes nothing
[PVE] same query; apt-mark unhold <each>         # releases holds the admin had set too
```

**About the Proxmox holds:**
- The prefixes `proxmox` and `pve` are hard-coded (`:293`, `:348`).
- `proxmoxPackagePatterns` (`config.go:65`) is **never read**.
- Prefix matching also holds `pve-manager`, `pve-firewall` and similar [H].
- The whole hold, install, unhold cycle repeats on every `InstallPackages` call: three times in Run All.

**Callers:**
- **`-l` / Run All** (`main.go:317-371`): core packages first. Then DMZ packages only if a local IPv4 address starts with `<dmzSubnet>.` (`utils.CheckSubnet`); otherwise DMZ **and** Lab packages.
- **`-c`**: `sudo` only (§4.1).
- **`-i`** (`InstallPythonPackages`, `:358-477`):
  - Alpine: `InstallPackages(alpinePythonPackages)`.
  - Deb/Ubu/PVE:
    1. `apt-get update`
    2. `apt-get install --yes <pythonPackages> [<nonWslPythonPackages> when $WSL is empty]`
    3. Then one of:
       - **pip:** `[apt-get install -y python3-pip]`, then `pip3 install <pythonPipPackages>` as root;
       - **uv:** `[pip3 install uv]`, then `uv pip install <pythonPipPackages>` [H `debian-i`, `debian-i-uv`].
  - This path has no locale steps and no holds.
  - [I] PEP 668 makes root `pip3 install` fail on Debian 12+ and Ubuntu 23.04+.
- **`-a`**: both.
- **Menu Run All on Alpine** also installs `alpinePythonPackages` (`menu.go:618-622`). CLI `-r` doesn't [H].

**Errors.** Callers ignore the return [H `debian-i-uv`: the failure was dropped silently].

### 4.8 AppArmor (`security.SetupAppArmor`, `security.go:15-102`): Run All when `enableAppArmor`

| Family | argv [H `debian-r`, `alpine-r`] | On failure |
|---|---|---|
| Deb/Ubu/PVE | `apt-get install -y apparmor`; `aa-enforce /etc/apparmor.d/*` (one literal argv); **if that fails**, `aa-enforce /etc/apparmor.d/<f>` for each non-directory entry | install → return; enforce → logged |
| Alpine | `apk add apparmor`; `rc-update add apparmor default`; `rc-service apparmor start`; `aa_enforce /etc/apparmor.d/<f>` for each non-directory entry (underscore in the name) | install → return; others logged |

- hardn writes no files here.
- [I]: `aa-enforce` comes from apparmor-utils, which hardn doesn't install. The Alpine binary is also `aa-enforce`. Alpine also needs kernel boot parameters.
- **Dry-run:** logs only.

### 4.9 Lynis (`security.SetupLynis`, `security.go:105-141`): Run All when `enableLynis`

- `apt-get install -y lynis` or `apk add lynis`; failure returns.
- Then `lynis audit system`. Output goes to the null device; a non-zero exit returns an error [H].
- **Dry-run:** logs only.

### 4.10 Unattended upgrades (`updates.SetupUnattendedUpgrades`, `updates.go:15-97`): Run All when `enableUnattendedUpgrades`

**Deb/Ubu/PVE** [H `debian-r`]. This is identical on Ubuntu and Proxmox, so Ubuntu gets a `Debian-Security` origin [I: wrong there].
```
apt-get install -y unattended-upgrades                 # failure → return
(os.Setenv DEBIAN_FRONTEND=noninteractive — process-wide; affects every later command)
debconf-set-selections                                 # failure logged; stdin:
  unattended-upgrades unattended-upgrades/enable_auto_updates boolean true
  unattended-upgrades unattended-upgrades/origins_pattern string origin=Debian,codename=${distro_codename},label=Debian-Security
dpkg-reconfigure -f noninteractive unattended-upgrades  # failure → return
systemctl enable unattended-upgrades                    # failure logged
```

**Alpine** [H `alpine-r`]:
- `MkdirAll /etc/periodic/daily 0755`.
- Write `/etc/periodic/daily/apk-upgrade` with mode **0755** and no backup:
  ```
  #!/bin/sh
  apk update && apk upgrade --available
  ```
- `rc-update add crond default`, then `rc-service crond start` (failures logged).

**Dry-run:** logs only.

### 4.11 Hushlogin

- **Root's** (`utils.SetupHushlogin`, `utils.go:161-184`) is Run All step 1. It creates `$HOME/.hushlogin` if missing, mode 0644 [H]. `$HOME` is the invoking environment's [I: root under `sudo -i` or `su -`]. Dry-run logs only.
- **The new user's** is covered in §4.1.

### 4.12 Run All order

| Step | CLI `-r` (`main.go:255-314`) | Menu 5 (`menu.go:565-696`) | Gate |
|---|---|---|---|
| 1 | root hushlogin `:260` | `:583` | — |
| 2 | sources (+ PVE repos) `:263-266` | `:586-589` | — |
| 3 | packages: core, then DMZ or DMZ+Lab `:269` | `:592-648` | non-empty lists |
| 3b | — | Alpine Python packages `:619-622` | Alpine |
| 4 | `CreateUser` `:272-277` (username required, `:150`) | `:651-653` (skipped if empty) | username |
| 5 | `WriteSSHConfig` `:280` | `:656` | — |
| 6 | `DisableRootSSHAccess` `:283-285` | `:659-661` | `disableRoot` |
| 7 | `ConfigureUFW` `:288-290` | `:664-666` | `enableUfwSshPolicy` |
| 8 | `ConfigureDNS` `:293-295` | `:669-671` | `configureDns` |
| 9 | AppArmor `:298-300` | `:674-676` | `enableAppArmor` |
| 10 | Lynis `:303-305` | `:679-681` | `enableLynis` |
| 11 | unattended upgrades `:308-310` | `:684-686` | `enableUnattendedUpgrades` |

- All six toggles default to `false` (`config.go:125-131`; the example config agrees, `embed.go:170-176`).
- A failed step never stops later ones. Only a `CreateUser` error is logged (CLI).
- The success message prints unconditionally (`main.go:312`; `menu.go:688`).
- The menu asks `y/n` first and shows the dry-run state.
- With every toggle on [H]:
  - 47 commands on Debian, 48 on Ubuntu (it adds `systemctl restart systemd-resolved`) and 26 on Alpine;
  - **sshd is restarted twice** (steps 5 and 6);
  - menu and CLI argv are identical except for step 3b.

### 4.13 Dry-run summary (`-n`, or menu 6)

Every operation function checks `cfg.DryRun` at the top and returns after logging `[DRY-RUN] …` lines (10 packages). What **still** happens:

| Path | Still executes or writes | Evidence |
|---|---|---|
| any invocation | `/var/log/hardn.log` open/append; `/etc/hardn/hardn.yml.example`; the no-config prompt and its write of `/etc/hardn/hardn.yml` | §2.1–2.2 |
| `-n -r` | `systemctl is-active systemd-resolved` (read-only; only when `configureDns`); nothing else | [H] 1 command on Debian, 0 on Alpine, 0 files |
| `-n -c` | `dpkg -l sudo` / `apk info -e sudo`; `user.Lookup` | [H] |
| `-n -d` | nothing, but it prints `[SUCCESS] Disabled root SSH access` | [H] |
| `-n -w`, `-n -s/-l/-i/-a` | nothing | [H] |
| `-n -g` | `systemctl is-active systemd-resolved`, `LookPath(resolvconf)`; **panics** when `nameservers` is empty | [H] |
| menu 2 (disable root) | **`systemctl restart ssh` / `rc-service sshd restart`** | `menu.go:362-371` [H] |
| menus 6, 8, 10 | `config.SaveConfig(cfg, "hardn.yml")` writes the whole config, including `dryRun`, to **`./hardn.yml` in the CWD** with mode 0644 (`menu.go:307-310, 477-480, 737-740, 752-755`) | [H `menu-toggle-dryrun`] |
| config `dryRun: true` | ignored; the flag default wins | `main.go:142` [H] |

---

## 5. Where origin/main differs

### 5.1 Keyed to V1–V27 (side-effect inventory §7)

"Regressed" means origin/main lost behaviour that v0.3.2 had. Only those rows make v0.3.2 a parity reference, and only for the part named in the last column.

| V | v0.3.2 | origin/main | Regressed? → what to compare against v0.3.2 |
|---|---|---|---|
| V1 | `-n` honoured by every operation (§4.13) | ignored below the menu layer (`origin/main:cmd/hardn/main.go:152`) | **Yes.** Under `-n`: zero mutating argv and zero writes per operation. Preview *wording* is not a reference |
| V2 | menu create-user honours dry-run (`user.go:28-34`; `ssh.go:18-38`) | user menus ignore it (`origin/main:pkg/menu/user_options.go:744,831`) | **Yes, for create-user only.** The sudo-toggle and key screens don't exist in v0.3.2 |
| V3 | same override (`main.go:142`) [H] | same (`origin/main:main.go:152`) | No |
| V4 | in-place edit: `PermitRootLogin yes`→`no`, root removed from `AllowUsers`, everything else byte-identical, backed up (§4.3) | rewrite from the `{Port:22}` stub (`origin/main:pkg/adapter/secondary/file_ssh_repository.go:127-155`) | **Yes.** Only those two keys may change. Not the prefix-only matcher, and not the empty `AllowUsers` |
| V5 | `AuthorizedKeysFile .ssh/authorized_keys <sshKeyPath>/authorized_keys`; keys in `~/.ssh_<u>/` (§4.1–4.2) | `.ssh_%u` only (`origin/main:main.go:368-374`; `file_ssh_repository.go:88-94`) | **Yes.** Both paths (= plan P0.8 item 4) |
| V6 | key-only written in the same step, with no key check | same | No |
| V7 | restart, no `sshd -t`; menu 2 restarts twice (and once **in dry-run**) | restart, no validation; menu restarts twice (`origin/main:pkg/menu/disable_root_menu.go:151-158`) | No (v0.3.2 is worse) |
| V8 | SSH step before UFW; `enableUfwSshPolicy` default false (`config.go:129`) | same | No |
| V9 | allows only `sshPort`; **no `ufw disable`/`reset`, so existing rules are kept**; enables even if the SSH allow failed | `ufw disable` + `ufw reset` added (`origin/main:…/ufw_firewall_repository.go:113,118`); aborts before enable if a rule fails (`:128-132`) | **Partly.** Rule preservation is the reference. Enabling after a failed allow is a v0.3.2 defect |
| V10 | UFW enabled on Proxmox [H `proxmox-w`] | same | No |
| V11 | `PermitRootLogin` follows `permitRootLogin`; `disableRoot` gate honoured | `no` forced (`origin/main:pkg/application/security_manager.go:49`); `disableRootSSH` unused | **Yes, for honouring the config key.** On Proxmox v0.3.2 also writes `no` by default, so there's no PVE reference (Decided: `prohibit-password`) |
| V12 | `ssh.socket.d/listen.conf` on every non-Alpine host, with no `daemon-reload` (§4.2) | no socket handling | **Intent only.** A port change must cover `ssh.socket`. v0.3.2's mechanism is defective; `listen.conf` is a migration artefact (5.2) |
| V13 | `manage.conf` also sorts after `50-cloud-init.conf`; status reads `manage.conf` or `sshd_config` (`pkg/status/status.go:182-213, 373-403`) | `hardn.conf`, same problem | No |
| V14 | installs sudo on Alpine: CLI `apk add --no-cache sudo` (`main.go:203-206`) or `apk add sudo` (`user.go:37-44`) [H] | never (`origin/main:…/os_user_repository.go:96-109`) | **Yes.** sudo is present after create-user on Alpine. Locked `adduser -D` accounts are the same in both [I] |
| V15 | whole `sshd_config` replaced, **with backup** (`ssh.go:51`) | replaced, no backup | **Backup only.** Whole-file replacement is not a reference (Decided) |
| V16 | same unvalidated sudoers write; backed up if it existed | no backup | No (backup → V18) |
| V17 | no password set (`--disabled-password` / `-D`) | same | No |
| V18 | 15 `BackupFile` call sites (§3); non-atomic writes | no backups (`BackupFile` has no callers) | **Yes, for "back up before every managed write".** The §3 file set is the minimum. Naming, collisions, mode 0644 and ignored errors are not reference |
| V19 | 3 strategies; backups; resolved gets all servers, the others the first 2; empty list panics or writes `DNS=` (§4.5) | no backups; all servers everywhere; empty accepted (`origin/main:…/file_dns_repository.go:103,139-140,180-185`) | **Backups only** |
| V20 | — | `setup-sudo-env` is origin/main only | n/a |
| V21 | Debian keys via `su - u -c "echo '<key>' >> …"` (**key inside a shell string**, run as the user); Alpine root-writes into a just-created dir, argv `chown` | root writes through `~/.ssh`; `sh -c "chown …"` with the username | No (each has its own defect) |
| V22 | no remove-key or sudo-toggle screens; an existing user is skipped entirely | no-op remove, ignored answer | n/a |
| V23 | AppArmor, Lynis and unattended-upgrades / apk periodic are live in Run All when enabled (§4.8–4.10) | dead; Run All claims success (`origin/main:pkg/menu/run_all_menu.go:269-275`) | **Yes.** The argv in §4.8–4.10 is the reference, minus the §6 defects |
| V24 | — (see 5.2) | — | 5.2 is the upgrade-path input |
| V25 | exit 0; more errors dropped than origin/main (§2.8) | exit 0 | No |
| V26 | same pre-parse writes; **no network, no `/tmp` cache, no `su - $SUDO_USER` probe** | adds an update check, a `/tmp` cache and the `su` probe at config load | **Yes, for "no network / `/tmp` / `su` at load".** Pre-parse writes are the same, so not a reference |
| V27 | `ListenAddress 0.0.0.0:<port>` default | `0.0.0.0` default | No |

**Other operation differences (not on the V-list):**
- **`-r` scope.** v0.3.2 runs 11 steps (§4.12). origin/main's `HardenSystem` does user, SSH, UFW if enabled, and DNS if enabled (`origin/main:pkg/application/security_manager.go:32-78`). It has no sources, packages, hushlogin or disable-root step (V23 covers the rest).
- **`-w` app profiles.** v0.3.2 applies `ufwAppProfiles`. origin/main's CLI passes an empty list (`origin/main:cmd/hardn/main.go:381`).
- **`sshConfigFile`.** v0.3.2 honours it. origin/main never sets `ConfigFilePath`, so it always writes `hardn.conf` (`origin/main:…/file_ssh_repository.go:37-43`).
- **Package flags.** `-l -i -a -s` are live in v0.3.2 and commented out on origin/main (`origin/main:cmd/hardn/main.go:89-94`).
- **Sources on `-c`.** v0.3.2's `-c` rewrites sources; origin/main's doesn't. That's an origin/main improvement (§6).
- **Proxmox `sources.list`.** v0.3.2 writes `proxmoxSrcRepos`. origin/main writes `debianRepos`, but that code is unreachable.
- **Holds.** v0.3.2 uses a prefix query. origin/main uses a fixed four-name list (`origin/main:…/os_package_respository.go:306,321`).
- **Locales.** The locale steps exist only in v0.3.2.

### 5.2 What v0.3.2 leaves on a host (V24 migration input)

- `<sshConfigFile>`: default `/etc/ssh/sshd_config.d/manage.conf`, or any custom path from an old `hardn.yml`. On Alpine, the whole `/etc/ssh/sshd_config` is hardn's.
- `/etc/systemd/system/ssh.socket.d/listen.conf`.
- `~<u>/.ssh_<u>/authorized_keys`, or the configured `sshKeyPath`.
- `/etc/sudoers.d/<u>`.
- `/etc/ufw/applications.d/hardn`, plus UFW rules, with UFW enabled (and on Alpine `rc-update`).
- Rewritten `/etc/apt/sources.list` or `/etc/apk/repositories`, and the PVE `ceph.list` and `pve-enterprise.list`.
- `/etc/systemd/resolved.conf`, `/etc/resolvconf/resolv.conf.d/head` or `/etc/resolv.conf`.
- `/etc/periodic/daily/apk-upgrade`.
- `~/.hushlogin` for root and for each user.
- `/var/backups/hardn/<date>/*.bak`.
- Config key `disableRoot`, which origin/main renamed to `disableRootSSH`.

---

## 6. v0.3.2 defects: do not carry over

Items 1–7 are the side-effect inventory's "v0.3.2-only" list, confirmed here. Items 8 onward are new in this pass.

1. Menu 2 restarts sshd in dry-run [H].
2. UFW is enabled after the SSH allow failed [H].
3. DNS panics on an empty `nameservers` list (dry-run, resolvconf, direct) [H].
4. The SSH key is interpolated into an `su -c` shell string (`user.go:174`).
5. `-c` (and `-l -i -a`) rewrites apt or apk sources [H].
6. Debian repos are written onto Ubuntu [H].
7. Unhold clears package holds the admin had set [H].
8. **An empty `AllowUsers ` line** is written whenever `sshAllowedUsers` is empty, including `DefaultConfig` and the interactive default config, and by `-d` on `AllowUsers root`. OpenSSH refuses the whole file [P; 10.2p1 source]. [I: the restart fails and new connections fail.]
9. **A same-second backup collision** destroys the original backup [H].
10. **An empty `sources.list`, `ceph.list` or `pve-enterprise.list`** when the repo lists are absent [H].
11. `AllowUsers` comes from config, not `-u`. An existing user gets no keys or sudoers, but the SSH config is still rewritten [H].
12. An IPv6 `sshListenAddress` drops `sshPort` [H+P].
13. Argv that only a shell would handle correctly: the profile allow is one string; `rm -rf /var/lib/apt/lists/*` and `aa-enforce /etc/apparmor.d/*` pass a literal `*` [H].
14. `proxmoxPackagePatterns` is unused, and prefix holds also catch `pve-manager` and `pve-firewall` [H].
15. `DEBIAN_FRONTEND` is set process-wide (`updates.go:70`).
16. Callers drop errors from almost every operation [H].
17. `listen.conf` is written without a `daemon-reload` [I effect].
18. The Alpine whole-file replacement drops `Include` and `Subsystem sftp` [H+P].
19. `-n -d` prints success, and the dry-run preview disagrees with what's written [H].
20. `apt-get install -y ufw` runs with no `apt-get update` first (`firewall.go:64`).

---

## 7. Still needs a VM (gate 5; written, not run)

- Outcome of restarting with `AllowUsers ` on Debian 13, Ubuntu 26.04 and Alpine 3.24: unit state, listener, existing sessions.
- Whether `listen.conf` takes effect on socket-activated Ubuntu after a reboot.
- `ufw allow "from any to any app 'X'"` passed as one token.
- `ufw default deny incoming` on an already-active UFW before the allow.
- `aa-enforce` with a literal glob without apparmor-utils; `aa_enforce` on Alpine.
- Alpine `adduser -D` lock state, and pubkey login with non-PAM sshd.
- debconf `origins_pattern` on Ubuntu.
- PEP 668 `pip3 install` as root.
- deb822 `.sources` duplicates after the `sources.list` rewrite.
- The v0.3.2 `-n` captures for menu dry-run screens (plan P0.7(d)), and the P0.6 timing runs.
