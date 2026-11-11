# SSH Login on the engine: the SSH Login control (`pkg/engine/controls/sshchange`)

> **Unit:** refactor plan slice 6 (SSH Login), unit (b), `next/s6-control`, from `3eb43194`. It builds on unit (a) ([gallery-ssh.md](gallery-ssh.md)), whose ten decisions and Made here calls the maintainer adopted on 2026-11-03 ("Adopt all, start (b)").
> **Status:** built and tested on macOS, in process, on `hostfake`, through the real engine; the gallery's SSH Login mock runs on it. Nothing has run on Linux or as root (§7). Nothing is wired into `cmd/hardn` or the main menu; `pkg/menu` and `pkg/characterization` goldens are unchanged. Words: gallery-ssh.md §6's, used exactly; the rest proposed (§6). *Since `next/s6-replace` (unit (d)), main menu 2's screen and `-d` call it ([cmd-engine.md](cmd-engine.md) §2.16, §2.17), and `pkg/characterization`'s `disable_root_ssh` case and its goldens are deleted (cmd-engine.md's goldens).*

The SSH Login control is every change the SSH Login screen makes: root login, password login, the port, and moving hardn's old files. `Read` gives the screen what it draws; `Plan` refuses before anything is written, in the decided words, or plans the change on the real engine, every change lockout-risk, so typed and confirmed or reverted.

## 1. What it does

- **Root** (`plan.go`): Disallow root login (`PermitRootLogin no`) and Allow root key-only back (`prohibit-password`, never `yes`); on Proxmox VE Root key-only, `prohibit-password` whatever is asked, refused once there (decision 3 (a)).
- **Auth**: Key-only login (`PasswordAuthentication no`, and `KbdInteractiveAuthentication no` where it is yes and sshd uses PAM) and Password login on (decision 4 (a)). *Since 2026-11-08*, Password login on is refused on Alpine where sshd, as the SSH rows read it (`ssh.ReadBypass`), lacks CVE-2026-123158's fix and checks passwords without PAM (its PAM not established as on, as Create takes it) (`SSH_PASSWORDS_UNFIXED`, after the common refusals; its words and code approved by the maintainer, 2026-11-08: "openssh-server 10.2_p1-r0 lacks the fix for CVE-2026-123158: with passwords on, anyone could log in as an account with no password. Upgrade openssh first: item 5."), as Create is refused on the same host ([control-users.md](control-users.md) §7): with passwords on, any account whose field is `*` could be logged in to with a long password once an account has a `$5$` or `$6$` hash ([alpine.md](../safety/alpine.md) §7). A fix not established, or no sshd that can run, refuses nothing; tested by `TestPasswordsOnUnfixed`. On Proxmox VE Key-only weighs root's own key (`RootPath`), as Root does, in place of the admins: refused with `SSH_PVE_NO_ROOT_KEY` where root could not log in by key, after the password-session refusal. Whether passwords log in is read through `AuthenticationMethods` (`Reading.PasswordsOn`, `sshd.MethodsMet`), as the SSH Auth check reads it ([check-ssh.md](check-ssh.md) §10).
- **Port**: adds the port, 22 held until confirmed (`steps.PlanSSH`); refused on Proxmox VE and where an active ufw lets the port sshd listens on now (22 where its listeners are not read) in from some sources alone, the port named in the refusal, or on one interface alone, named "<src> on <iface>" or "anywhere on <iface>" (decision 5 (a), `ufw.OnlyFrom`).
- **Migrate**: hardn's old files into `00-hardn.conf` (`steps.PlanMigration`); the other changes refuse until it is done, and refuse an Include not first (decision 6 (a), P-38 (a)).
- **The proof** (decision 2 (a), `paths.go`): before a change that closes a way in, for each admin, sshd's own answer from the session's address (`sshd.ReadUser`), `DenyUsers` and `AllowUsers` matched by hardn (`sshd.Listed`), the keys sshd reads, their type and RSA size (`authkeys.TypeAccepted`, `Key.RSABits`), StrictModes, `PubkeyAuthentication`, one `AuthenticationMethods` list met, the lock without PAM, and becoming root after. Refused: root's own SSH login, a password session for Key-only, a session sshd's log has no line for (`sshd.ReadAccepted`: the last line from the session's address and port, logged by its sshd or that process's `[priv]` parent where `/proc` shows them, `revert.Session.LogPIDs`; on systemd the current boot's journal); from a console, the admins alone.

## 2. The API

- `Read(ctx, sys, facts, cfg, sess, getenv) (Reading, error)`: `Reading{Facts, Binary, Effective, EffErr, Found, FoundErr, Users, UsersErr, Session, Login, LoginErr, PVE, Root, RootPassword, RootLocked, ListenPort, OnlyFromPort, OnlyFrom}`. It writes nothing; a read that fails is kept beside what it would have given. Methods: `Console`, `RootOwn`, `ByKey`, `RootValue`, `Methods`, `PasswordsOn`, `MatchMore`, `DecidedBy`.
- `Reading.LoginPaths(ctx, sys, keyOnly) []Admin` and `RootPath(ctx, sys) Admin`: `Admin{Name; Key, Password bool; Why}`, `Why` one of §6's whys.
- The controls, each embedding the check it changes ([pkg/engine/controls/ssh](check-ssh.md)) and planning nothing at its zero value (`engine.ErrNotMigrated`): `Root{ssh.Login; Want; Getenv}`, `Auth{ssh.Auth; Action; Getenv}` (`AuthNone`, `AuthKeyOnly`, `AuthPasswordsOn`), `Port{ssh.Port; To}`, `Migrate{ssh.Login; ConfigFile}` (`MigrateID`, `ssh.migrate`). Each refusal is a `*steps.Refusal` with a code; `screenkit.RefusalWords` gives its words.
- The readers: [sshd-effective.md](../safety/sshd-effective.md) (`ReadUser`, `Listed`, `MethodsMet`, `ReadAccepted`), [authkeys.md](../safety/authkeys.md) (`RSABits`, `TypeAccepted`), [ufw.md](../safety/ufw.md) (`OnlyFrom`).

## 3. For units (c) and (d)

- **(c), the screen:** it draws only from `Reading` and plans these controls through `screenkit`; the gallery's rows, layout and words (`internal/tools/uigallery/ssh.go`) move into it. Main menu 2 opens it. *Done in `next/s6-screen` ([screen-ssh.md](screen-ssh.md)).*
- **(d), the command line:** `-d` plans `Root{Want: "no"}`, refused under the same rules, typed on a terminal and refused off one, as `-w`; `-c -d` comes back, its guard `-d`'s (decision 8 (a)). hardn.yml's SSH keys: none drives the screen; `sshPort` is the form's default, `sshConfigFile` is `Migrate`'s alone, the rest unused, a value set warning once (decision 9 (a)). *Done in `next/s6-replace`: `-d` plans Root, `-n` its plan; the five unused keys warn where not the default.*

## 4. Engineering calls

- A separate package from `pkg/engine/controls/ssh`, which `controls/users` imports: the proof needs `users.Read`.
- `.golangci.yml` unchanged: the controls rule's glob covers the package.
- On Proxmox VE the root key-only check runs before the in-effect one, as in the mock.
- A change already in effect is an empty plan; `Migrate`'s zero value plans, as it has no field to make it check-only.
- From a console sshd is asked about `0.0.0.0` [I], and any `DenyUsers` entry naming the admin counts whatever its host.
- With `StrictModes no` the StrictModes check is skipped.
- On Rocky Linux, AlmaLinux and RHEL a port change also refuses with `SELINUX_NO_SEMANAGE`, `SELINUX_PORT_TAKEN`, `FIREWALLD_STOPPED`, `FIREWALLD_NO_SSH_PATH` and `FIREWALLD_UNKNOWN` ([el-ports.md](el-ports.md) §1.2).
- Refusals reuse `SSHD_ENV_NO_RUNDIR`, `SSH_MIGRATE_FIRST`, `SSHD_NO_WRITE_STRATEGY`, `PVE_PORT_CHANGE_REFUSED`; the listen.conf refusal keeps `SSH_LISTEN_CONF_NOT_IN_EFFECT`.
- Each refusal's words are split at the last ". " into `Why` and `Fix`, which `screenkit.RefusalWords` joins back exactly.
- The gallery refuses Change SSH port before its field by planning the port sshd has, which plans nothing but the port's refusals.
- The gallery's engine sees the tools' setuid bit as `Read` does (`engineSys.Stat`), and its record counts `journalctl -u` as a read.

## 5. Codes proposed

`SSH_SETTINGS_NOT_READ`, `SSH_SESSION_NOT_LOGGED`, `SSH_ROOT_SESSION`, `SSH_NO_ADMIN_LOGIN`, `SSH_PASSWORD_SESSION`, `SSH_ADMIN_PASSWORD_ONLY`, `SSH_MATCH_LETS_MORE`, `SSH_PVE_ROOT_KEY_ONLY`, `SSH_PVE_ROOT_PASSWORD_SESSION`, `SSH_PVE_NO_ROOT_KEY`, `SSH_PORT_SOURCE_RESTRICTED`, `SSHD_LOGIN_NOT_LOGGED` ([output.md](../reference/output.md)).

## 6. Words proposed

- `Migrate`'s title "Move old settings" (the decided item name).
- Several ufw sources in the restricted-port refusal, joined by ", ".
- An interface rule's source in the restricted-port refusal: "<src> on <iface>", "anywhere on <iface>".
- The registry's one-line meanings, the sentinels' texts.
- SSH Auth's detail "key and password" and evidence `sshd -T: authenticationmethods <lists>` (check-ssh.md §10).

## 7. Facts, and what is not run

Read from the VMs on 2026-11-03 (`vm-runs/s6/00-inspect-*.log`, `01-inspect-b-*.log`): `sshd -T -C` prints `allowusers`, `denyusers` and `allowgroups` one pattern per line as written, `USER@HOST` kept; `requiredrsasize 1024` and `pubkeyacceptedalgorithms` printed under `-C`; `authenticationmethods any`, its lists on one line; `permitrootlogin without-password` on Debian 13, `prohibit-password` on Ubuntu 26.04 and Alpine 3.23; no `SSH_` variable under sudo or doas; each session's "Accepted publickey for <user> from <addr> port <port> ssh2: ED25519 SHA256:<fp>" in `journalctl -q -o short-iso -u ssh` on Debian 13 and Ubuntu 26.04, in `/var/log/messages` on Alpine (busybox syslogd, rotated to `messages.0`), from the session's `sshd-session [priv]` pid.

Not run: no change applied on a VM; sshd's `-T -C` with a Match block; a console session's `/proc` as `revert.ReadSession` reads it; `hardn confirm` from a new connection after the root and password changes.
