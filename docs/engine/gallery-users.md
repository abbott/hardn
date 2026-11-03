# User Management mock (slice 5, unit (a))

Unit (a) of slice 5, `next/s5-gallery`: a proposal for review. Nothing reaches hardn; the mock is in `internal/tools/uigallery` (`users.go`, `usershost.go`).
Status: built and tested on macOS in process (`go test ./internal/tools/uigallery/`); no hardn binary ran, nothing ran on a VM.

## 1. What to run

```
go run ./internal/tools/uigallery users [-ascii] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery play users                      # the menu of the hosts
go run ./internal/tools/uigallery play users/<host>               # one host
go run ./internal/tools/uigallery play -sequence dry-run users/<host>   # plan only
go run ./internal/tools/uigallery play                            # main menu 1 leads to users/debian13
```

## 2. What origin/main does

<!-- origin -->

## 3. The mocks

Every row comes from real readers: the User row from `users.Admin.Check` through the main menu's own mapping (`output.StatusViewOf`), each user's admin rights from `users.ReadElevation`'s `GrantsOf`, hardn's own grant from `sudoers.Read` and `PolicyOf`, the session user from `sudoers.Invoker` (`SUDO_USER`, `DOAS_USER`), sshd's PAM from `ssh.ReadPAM`. Never from hardn.yml. Grants are planned by the real `sudoers.PlanFor` and `steps.Sudoers` and run on the real engine; a lockout-risk plan asks the typed hostname and is confirmed or reverted, as Firewall's Enable is. Values are synthetic: george is the login user, alice a second one, `web-01`.

| Host | What it shows |
|---|---|
| `debian13` | The Debian 13 VM's shape: george, uid 1000, `!`, in sudo and adm, two keys, NOPASSWD by cloud-init's `90-cloud-init-users`; alice, uid 1001, a password, in sudo, no key |
| `ubuntu2604` | The same on sudo-rs 0.2.13, `/etc/sudoers` with `%admin` too, `HOME_MODE 0750` |
| `alpine323` | The Alpine VM: doas, `wheel.conf` `permit nopass :wheel`; george (1001) and alpine (1000) in wheel, `*`, `/bin/ash`; cloud-init's sudoers file where no sudo reads it; sshd without PAM |
| `legacy` | origin/main's `/etc/sudoers.d/george` (NOPASSWD and its env_keep line); Remove refuses, naming it |
| `hardn` | hardn's own `hardn-grant-george` alone: Remove refuses, as hardn runs through george's sudo |
| `second-admin` | alice with a password, one key, `hardn-grant-alice`: Remove and Ask for password are typed, confirm-or-revert |
| `one-key` | george's one key: Remove SSH key refuses; alice with no `.ssh` |
| `sudo-password` | hardn's grant asking george's password: Don't ask password, y/N |
| `alpine-no-rule` | george in no doas rule: Grant writes `/etc/doas.d/hardn-george.conf` |
| `alpine-locked` | alice's field `!`, sshd without PAM: "Locked, key login refused" [I: not run] |
| `yml-missing` | hardn.yml names `deploy`, not on the host: nothing shows it (decision 7) |
| `not-read` | `visudo -c` rejects the files: every row "Can't read", every change refuses |
| `pve9` | Proxmox VE 9: root alone, no sudo; "No user besides root"; Create without admin rights |
| `no-sudo` | Debian without sudo: Grant refuses, "sudo isn't installed" |

Listing mocks besides `screen-<host>`: `select`, `manage`, `manage-alice`, `keys`, and `refusals` (eleven refusals, each from the real reading). 20 transcripts (`play/users-*`) play every flow. `debian13`, drawn:

```
# User Management ##############################################

  [+] User                 Configured non-root, sudo

# george -------------------------------------------------------

  [ ] Sudo                 Enabled, No password
   -  Granted by           /etc/sudoers.d/90-cloud-init-users
   -  SSH Keys             2 keys
   -  Password             None

# alice --------------------------------------------------------

  [+] Sudo                 Enabled
   -  Granted by           group sudo
  [ ] SSH Keys             None
   -  Password             Set

  Select an option

   1) Manage User          Configure sudo, SSH keys
   2) Create User          Configure a new user

   0) Return

  > Enter your choice [0-2 or q]:
```

Where it departs from the frames (`pkg/ui/screen/frames.go`, kept): a section per user, its header the name, in place of 01-USER's one "User Configuration" section with a badge; UID:GID and Directory dropped for "Granted by" and "Password"; Manage User's items named by their result (P-12) in place of "Sudo Method, Toggle password requirement"; SSH Keys lists the keys by fingerprint and offers Remove. Item keys stay put: 3 is always SSH Keys, so a user with no admin rights has 1 and 3.

## 4. Heights

Every screen is as many lines at 80x24 and 80x40, Unicode and ASCII. Over 24 rows: `alpine-locked` 33 (three users), the two-user hosts 26, `not-read` 24. One user: 18 or 19; Manage User 20; SSH Keys 19; Select 10. A pasted key's echo (about 100 cells) overruns 80 columns; the goldens therefore answer no to Add SSH key, and the tests paste one.

## 5. Decisions for the maintainer

Drawn: every recommendation below. Not mocked: deleting a user, setting a password, group membership changes, `authorized_keys2`, root's keys on Proxmox VE.

1. **Scope: which changes the screen offers.** *Gates every decision below.* The mock offers create a user, grant and remove admin rights, ask or don't ask a password for them, add and remove keys. origin/main has no Delete User. (a) *Recommended:* those six, no delete: each is a file or a command hardn can journal and roll back; `userdel --remove` destroys a home directory, which no rollback restores. (b) Add Delete User, a refusal for the session user and a typed confirmation, with the home kept (`userdel` without `--remove`).
2. **How a grant is written, and what Remove does to one hardn didn't write.** *Gates 4, 5.* (a) *Recommended, drawn:* hardn's own drop-in per [sudoers.md](../safety/sudoers.md) §2 (`hardn-grant-<stem>`, `/etc/doas.d/hardn-<stem>.conf`), which sorts after cloud-init's and the distributions' and wins; Remove removes hardn's file alone, and where cloud-init's file, origin/main's, or the sudo or wheel group grants it, refuses before asking, naming it (for a group, `gpasswd -d <user> <group>`); where another rule would still grant once hardn's file is gone, it refuses too, read by the real reader with the file hidden, so elevation is never reported removed while a rule still grants it. (b) Group membership (`usermod -aG sudo`, `gpasswd -d`): no file, but the group's rule is the distribution's, `%sudo` asks a password the VMs' login users don't have, and Alpine's `wheel.conf` is `nopass`, so the password choice cannot be made per user.
3. **A user's password (V17).** *Gates 4.* On every VM the login user has none: `!` on Debian and Ubuntu, `*` on Alpine. (a) *Recommended, drawn:* create key-only, no password; refuse "Ask for password" for a user with none, and refuse yes to Create's "Require sudo password?", in Figma's polarity, with the reason. On Alpine, whose sshd has no PAM and refuses a key login to a `!` account, `useradd --password '*'` (tested: the new user reads back unlocked). (b) Set one through `chpasswd` from a typed entry, twice; it puts a secret through hardn's input and its journal must never hold it. (c) Ask each time which.
4. **The lockout rules.** Refused, before asking: removing the session user's own elevation; asking a password the user lacks; removing the session user's last key; a grant hardn didn't write (2). Typed hostname, confirm or revert: removing another user's grant, asking a password the user has (`sudoers.RiskLockout`), removing any key of the session user's (sshd's log, which says which key it used, is not read), and anyone's last key. y/N: granting, not asking a password, adding a key, creating a user. *Recommended:* as drawn.
5. **origin/main's `/etc/sudoers.d/<user>` (V16).** Slice 9 left it here. (a) *Recommended:* leave it and say so: the row says "Granted by /etc/sudoers.d/george", Remove refuses naming it, and `PlanFor`'s legacy finding says whether hardn's rule wins. (b) Migrate: write hardn's grant, then remove the old file in the same plan, its env_keep line carried to `hardn-env-<stem>`; one plan, one rollback. (c) Remove it: loses the env_keep line slice 9 reads.
6. **`-c` and `-u`.** (a) *Recommended:* on the new control, applied as `-g` is: `-u <name>` plans Create with no key and admin rights, under the same refusals. (b) Retired, saying where the screen is. *Gated by* 1.
7. **hardn.yml's `username`, `sudoNoPassword`, `sshKeys`.** (a) *Recommended:* the form's defaults alone (Username, the admin answer, a key offered), never the user the rows are about: `yml-missing` shows that nothing names `deploy`; a value set and unused warns once, as Firewall's decision 8. (b) Unused, with that warning.
8. **The words.** P-11: Figma's polarity, "Require sudo password?" (drawn), default no. P-13 (a): "User", the detail the check's ("non-root, sudo"). P-29 (b): Figma's words, as drawn. §6 lists the new ones.

**Made here:** one section per user (Figma's badge does not scale past one); key stays put per item; the uid range 1000 to 60000, not read from login.defs; the password state from `/etc/shadow`'s first character; the session key not known; Create's key optional, a notice when absent; a grant to a user with a password asks it, without one doesn't; a key is one OpenSSH public key, its type one sshd 10 takes, its blob naming the same type.

## 6. Words

Proposed: "Granted by"; "Password": Set, None, "Locked, key login refused"; "No user besides root"; items "Grant admin rights" ("Let george use sudo"), "Remove admin rights" ("Take sudo away"), "Ask for password", "Don't ask password" ("For sudo"), "Add SSH key", "Remove SSH key", "Remove which key?". Refusals: "You're running hardn through george's sudo. Removing it would leave no way to become root. Grant another user first."; "george is an admin through <file>, which hardn didn't write, so hardn can't remove it. Change it there."; "alice is an admin through the sudo group, which hardn leaves alone. Take alice out of it with gpasswd -d alice sudo."; "<user> would stay an admin through <rule>, …"; "george has no password, so sudo couldn't ask for one. Set one with passwd george, then try again."; "This is george's last key, and you're logged in as george. Add another key first."; "sudo isn't installed, so hardn can't grant admin rights. Install sudo, then try again."; "hardn can't read sudo's rules: its files don't check. Check them with visudo -c, then try again."; "A new user has no password, so sudo couldn't ask for one."; "Without a key carol can't log in: there's no password."; "sshd refuses alice's keys: the account is locked (!) and sshd doesn't use PAM."; the name and key checks' lines.

## 7. For unit (b)

Stand-ins, each labelled in `users.go`:

1. `usrControl`: `users.Admin` with a `Plan`; unit (b) migrates `Admin` off `engine.CheckOnly`.
2. `usrDecidedBy`: the deciding rule and its NOPASSWD from the check's evidence strings. `users.Grant` says neither the rule nor the password; unit (b) needs them exported.
3. `readPasswords`: `/etc/shadow`'s first character. `accounts.Lock` folds `*` and a hash into `NotLocked`, so "no password" and "set" are not told apart; unit (b) needs a third state.
4. `readKeys`, `usrKeyFiles`: `sshd.ReadEffective` does not expose `AuthorizedKeysFile` (the brief assumed it does); the mock reads both default files, and plans `authorized_keys` alone.
5. `parseKey`, `fingerprint`: an OpenSSH key check with no `x/crypto`; unit (b) decides whether to add the dependency.
6. `usrCreate` (`useradd --create-home --shell`, `--password '*'` on Alpine; undo `userdel --remove`), `usrKeysChange` (`install -d -m 700 -o -g`, then the file 0600, the user's), `stillAdmin`'s `hiddenSys`.
7. The fake host (`userSim`): `useradd`, `userdel`, `install -d` and a rejecting `visudo -c`; Alpine's `adduser` (busybox) is not modelled.

Findings: on the Alpine VM `wheel.conf` sorts after `hardn-*.conf`, so for a user in wheel doas's last match is wheel's `nopass` and hardn's rule cannot ask a password; `PlanFor`'s finding says "may override", but for a wheel member it does. On Proxmox VE the row is "Not Configured, root only" and only Create (no admin rights) works.

## 8. Not run

No hardn binary, no VM. The Alpine rule that sshd without PAM refuses a key to a `!` account is [I], from the brief. `useradd --password '*'`, `install -d -o`, sudo-rs's reading of `hardn-grant-*`, and `gpasswd -d`'s effect were not run anywhere.
