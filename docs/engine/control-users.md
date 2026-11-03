# User Management on the engine: the User control (`pkg/engine/controls/users`)

> **Unit:** refactor plan slice 5 (User Management), unit (b), `next/s5-control`, from `ebf87b11`. It builds on unit (a) ([gallery-users.md](gallery-users.md)), whose eight decisions and Made here calls the maintainer adopted on 2026-11-03 ("Adopt all, start (b)").
> **Status:** built and tested on macOS, in process, on `hostfake`, through the real engine. Nothing has run on Linux or as root (§6). Nothing is wired into `cmd/hardn` or the main menu; `pkg/menu` and `pkg/characterization` goldens are unchanged. Words: gallery-users.md §6's, used exactly; the rest proposed (§5). *Since `next/s5-replace` (unit (d)), main menu 1's screen and `-c` call it ([cmd-engine.md](cmd-engine.md) §2.14, §2.15).*

The User control is Figma's User row and every change the User Management screen makes: admin rights, a user's SSH keys, a new user. `Read` gives the screen what it draws; `Plan` refuses before anything is written, in the decided words, or plans the change on the real engine.

## 1. What it does

- **Admin rights** (`plan.go`): Grant, Remove, Ask for password, Don't ask password, as hardn's own file (`sudoers.PlanFor`, `steps.Sudoers`; decision 2 (a)). Remove takes hardn's file alone and refuses where another rule grants or would still grant (`StillAdmin`), for the session user's own, and for the last admin with the session user not known, another admin counted only where it can become root (a rule asking no password, or one it has set; decision 4). Grant is plain for a user not an admin; for one already an admin, the planner's risk (a password asked where none was is a lockout risk).
- **SSH keys** (`keys.go`): Add and Remove in `~/.ssh/authorized_keys` alone, and only where sshd reads it; keys counted in every file sshd reads ([authkeys.md](../safety/authkeys.md)). Add refuses a home or `~/.ssh` StrictModes would ignore a key in; Remove refuses the session user's last key and a key also in another file sshd reads. Removing the session user's key, anyone's with the session not known, or anyone's last key is a lockout risk (typed hostname, confirm or revert); adding is plain.
- **Create** (`create.go`): one plain plan: `useradd --home-dir /home/<name> --create-home --shell /bin/bash` (`/bin/ash` on Alpine), with `--password '*'` where sshd's PAM is not established as on (decision 3 (a)), undone by `userdel --remove` only where useradd made the user (the name and home checked again before it; useradd failing undoes nothing); then hardn's grant without a password, where asked; then the keys, where given, every one in one change, the same key once. A name useradd refuses, one a user or group has, and one whose `/home/<name>` is there are refused; so is a password asked for admin rights, and a key that is not one, before anything is written.

## 2. The API

- `Admin{Action, User, Key, Fingerprint, New, Getenv}`; `Action`: `ActionNone` (the status list's: `engine.ErrNotMigrated`), `ActionGrant`, `ActionRemove`, `ActionAskPassword`, `ActionNoPassword`, `ActionCreate`, `ActionAddKey`, `ActionRemoveKey`. `New{Name; Admin, Password bool; Keys []string}`: the screen passes one key, `-c` hardn.yml's `sshKeys`. Change ids `ChangeGrant`, `ChangeCreate`, `ChangeKeys` (`users.admin/grant`, `/create`, `/keys`).
- `Read(ctx, sys, facts, getenv) (Reading, error)`: `Reading{Choice, NotRead, Invoker, Status, StatusErr, PAM, KeyEntries, KeyErr, Users}`; `User{accounts.User, Admin, Tool, Decided, NoPassword, Password, Policy, PolicyErr, Files, Keys, KeyDir, Locked}`. It writes nothing; it runs `visudo -c` or `doas -C` and `sshd -T`.
- Before asking: `Reading.Refusal(ctx, sys, Admin)`, `ToolRefusal()`, `KeysRefusal()`, `CheckName(ctx, sys, name)`, `CheckNew(ctx, sys, New)`, `ParseKey(s)` and `CheckKey(user, s)` (Add's key: `ParseKey`'s, or one a file sshd reads holds already): each a `*steps.Refusal` whose words `screenkit.RefusalWords` says. `StillAdmin(ctx, sys, Reading, user) Still{By, Unknown}`. `User.NewGrant()`: the grant Grant plans (`GrantPassword` where `Password` is `Set`, else `GrantNoPassword`), which the screen's review says.
- Refusal codes: `USERS_*` (twelve for admin rights; `USERS_KEY_FILES_NOT_READ`, `_LAST_KEY`, `_KEY_ELSEWHERE`, `_KEY_THERE` (Add's key there already), `_KEY_GONE` (Remove's key no longer in `~/.ssh/authorized_keys`, `PlanRemove`'s `fs.ErrNotExist`), `_NAME_FORM`, `_NAME_TAKEN`, `_HOME_THERE`, `_NEW_PASSWORD`) and `AUTHKEYS_NOT_PLANNED`, `_STRICT_MODES`, `_NOT_KEY`; findings `USERS_LOCKED` on Add for a locked user, `USERS_NO_KEY` on Create with no key ([output.md](../reference/output.md)).
- Beside it: `accounts.Password` (`PasswordNotRead`, `Set`, `None`, `Empty`; `Shadow.PasswordOf`); [authkeys](../safety/authkeys.md) (`Read`, `Keys`, `ParsePublic`, `PlanAdd`, `PlanRemove`, `CheckStrict`); `sshd.Effective`'s `StrictModes` and `AuthorizedKeysFile`; `ssh.ReadKeyFiles`, read as `ssh.ReadPAM` reads sshd.

## 3. For units (c) and (d)

- **(c), the screen:** it draws only from `Reading`, refuses through `Refusal`, `CheckName`, `CheckNew`, `KeysRefusal` and `ParseKey` before it asks, and plans `Admin` through `screenkit`. Main menu 1 opens it. The gallery's rows, forms and words (`internal/tools/uigallery/users.go`) move into it. *Done in `next/s5-screen` ([screen-users.md](screen-users.md)).*
- **(d), the command line:** `-c` and `-u` on this control, applied as `-g` is (decision 6 (a)): `-u <name>` plans Create with admin rights and no key, under the same refusals. hardn.yml's `username`, `sudoNoPassword` and `sshKeys` are Create's defaults alone, never the user the rows are about; a value set and unused warns once (decision 7 (a)). *Done in `next/s5-replace`: `-c` plans Create, `-n` its plan; `sudoNoPassword: false` alone warns.*

## 4. Engineering calls

- `ssh.ReadKeyFiles` beside `ReadPAM`: one more `sshd -T` per `Read`; `Plan` reads sshd and the keys only for the key actions and Create.
- `Locked` is a `!` password where sshd's PAM is not established as on, not only where it is off [I].
- The key actions skip the tool's and hardn's-file-edited refusals: a key needs neither.
- A key in another file sshd reads is refused from `Read`'s files, naming that file, before `PlanRemove`.
- The last key is the session user's last key not `Restricted` (options that may stop a login: `command=`, `from=`, `restrict`, …), counted once across every file sshd reads; a restricted key is listed and removable.
- The keys change is in `StageAccounts` (the engine's "the user and the key in place"); the mock's was `StageFiles`.
- The keys change always backs up `authorized_keys`: a new one is removed on rollback, then a `~/.ssh` it made.
- The file's uid and gid are read from `/etc/passwd` at apply, so a user Create makes in the same plan has them.
- Create makes `~/.ssh` with `install -d -m 700 -o <name> -g <name>`: useradd gives the user its own group (USERGROUPS_ENAB yes), and `CheckName` refuses a group of the name already there.
- Create's grant is plain and after the account; its key after the account; `CheckStrict` is not run for a home that does not exist yet [I].
- Create with no key has a finding, `USERS_NO_KEY`, in place of the mock's review notice.
- Add for a locked user reuses `USERS_LOCKED` as the plan's finding.
- The name check is doas's (`sudoers.CheckUser(ToolDoas, …)`, the stricter) on every host, as the mock's.
- The gallery's engine sees the tools' setuid bit as `Read` does (`usrSys`), so its plans read the host the screen read.

## 5. Words proposed

- "george's key is also in /home/george/.ssh/authorized_keys2, which sshd reads too. Remove it there, then try again."
- "/home/carol is already there, and undoing Create would delete it. Choose another name."
- The Password row for an empty field: "Empty" (bad).
- Create with several keys: "Add carol's keys". A bad key in `New.Keys` is numbered, one key or several: "Key 2 isn't an OpenSSH public key, such as ssh-ed25519 AAAA… name." (`CheckNew`); the screen's pasted key keeps §6's "Paste one OpenSSH public key, …", refused at its field first.
- The mock's, kept: "Create user carol", "Add carol's key", "Add a key for alice", "Remove a key from alice"; Create's states "absent", "no password, key login". The two findings are §6's words without the full stop.

## 6. Facts, and what is not run

Read from the VMs on 2026-11-03 (`vm-runs/s5/00-inspect-*.log`, `01-inspect-b-*.log`): `authorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2` on Debian 13 and Ubuntu 26.04, `.ssh/authorized_keys` on Alpine 3.23; `strictmodes yes` on all three; `usepam yes` on Debian and Ubuntu; `useradd` at `/usr/sbin/useradd` on all three, `USERGROUPS_ENAB yes`, `HOME_MODE` 0700 (Debian), 0750 (Ubuntu), none (Alpine); `install` is GNU coreutils' on Debian, uutils' on Ubuntu 26.04, BusyBox's on Alpine, each taking `-d -m -o -g`; `doas -C` on piped rules, the last match winning.

Written, not run on a Linux host: `useradd --password '*'` and a key login to that account without PAM; `userdel --remove` as Create's undo; a key written into a user's home (`WriteFileAtomic` there) and `install -d` with numeric ids on BusyBox; `gpasswd -d`; the order of `/etc/doas.d` that puts `zz-hardn-<stem>.conf` after `wheel.conf` [I]; sudo-rs reading `hardn-grant-*`.
