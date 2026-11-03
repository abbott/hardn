# User Management mock (slice 5, unit (a))

Unit (a) of slice 5, `next/s5-gallery`: a proposal for review. Nothing reaches hardn; the mock is in `internal/tools/uigallery` (`users.go`, `usershost.go`).
Status: built and tested on macOS in process (`go test ./internal/tools/uigallery/`); no hardn binary ran, nothing ran on a VM: not `useradd --password '*'` (on the fake only), `install -d -o`, sudo-rs's reading of `hardn-grant-*`, nor `gpasswd -d`'s effect. That sshd without PAM refuses a key to a `!` account is [I], from the brief. The eight decisions of §5 are adopted (the maintainer, 2026-11-03), with the Made here calls as stated and the words of §6. *Since `next/s5-control` (unit (b)): the mock runs on the real control, no stand-in left ([control-users.md](control-users.md)).*
*Since `next/s5-screen` ([screen-users.md](screen-users.md)): the listing and its mocks are gone; git keeps them at `next/s5-control`, and §3 records what they showed. `play users` and `play users/<host>` run hardn's own screen on the same eighteen hosts, as does main menu 1; each `play/users-*` golden changed in its opening note, and the five that play Create also in "Grant admin rights?", now [Y/n]: hardn.yml's defaults set `sudoNoPassword`, which the screen takes as that question's default.*

## 1. What to run

```
go run ./internal/tools/uigallery users [-ascii] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery play users                      # the menu of the hosts
go run ./internal/tools/uigallery play users/<host>               # one host
go run ./internal/tools/uigallery play -sequence dry-run users/<host>   # plan only
go run ./internal/tools/uigallery play                            # main menu 1 leads to users/debian13
```

## 2. What origin/main does

Goldens: pkg/characterization/testdata/golden/menu/users_<case>.golden (TestLegacyMenuUsers). "run" = its golden; "read" = file:line.

- Reached from main menu 1 (main_menu.go:781, read); under plan only the trunk now refuses it before the screen (main_menu.go:819-824, read). From the CLI, -c -u alice runs CreateUser(username, true, sudoNoPassword, sshKeys) then ConfigureSSH (main.go:396-411, read; run: ops/create_user_c_path/*.golden); -n with -c is refused, exit 2 (main.go:526, read; cmd/hardn TestDryRunRefusesOperations).
- Which menu shows depends on os/user on the machine, not the host's `id` (user_options.go:69, read). Configured user present: 1 Manage a user, 2 Create a user (run: open_configured_user_on_host). Absent: 1 Change username, 2 sudo-password toggle, 3 Manage SSH keys, 4 Create user (run: open_configured_user_absent). No username: 1-3, item 4 hidden (run: open_no_configured_user).
- The box lists every passwd entry with UID >= 1000 and a login shell, each with Sudo, SSH Keys, UID:GID, Directory, last login (run: open_*); each redraw runs id, id -u/-g, getent, last, groups, sudo cat per user, twice (run: any golden's call log).
- Create a user: username, "Allow sudo access?", "Allow sudo without password?" (only after y), "Add SSH public key?", key, summary, confirm (run: create_sudo_no_password_with_key, create_sudo_password_required_no_key, create_key_yes_empty, create_declined). Runs adduser --disabled-password, usermod -aG sudo, su - u -c mkdir/cat >>/chmod, writes /etc/sudoers.d/<u> 0440 and /home/<u>/.ssh/authorized_keys 0600, sh -c chown -R (run: same).
- Refusals: empty name, upper case, reserved name, a name os/user finds (run: create_empty_name, create_bad_name, create_reserved_name, create_existing_name). Keys: 7 says "Invalid option" (run: invalid_key); 0 and q return (run: return_0, quit_q).
- Manage a user: pick a user, then 1 Sudo Method, 2 Manage SSH keys, 0 (run: manage_select_user_menu, manage_select_0, manage_select_bad_number, manage_invalid_option).
- Sudo Method flips the user's NOPASSWD state: dropping the password asks nothing; requiring it asks "Require password for sudo?" (run: manage_sudo_drop_password, manage_sudo_require_password, manage_sudo_require_password_answer_n). Either way CreateUser rewrites sudoers and re-appends every key (run: same).
- Manage SSH keys: Add writes the key unchecked (run: manage_keys_add); Remove calls CreateUser with the other keys (run: manage_keys_remove_is_a_noop); Remove is hidden with no key, still taken (run: manage_keys_remove_hidden_no_keys).
- Absent-user menu: 1 and 2 save ./hardn.yml only (run: absent_1_change_username, absent_2_toggle_sudo_password); 3 adds to sshKeys, saves, and reaches the host only if os/user finds the user (run: absent_3_add_key_saves_config; ssh_keys_options.go:75-87, read); a key not starting ssh-/ecdsa- is refused (run: absent_3_invalid_key); 4 creates the configured user and leaves the screen (run: absent_4_create_user).
- Alpine: adduser -D -g "", addgroup wheel, sudoers.d written; no sudo install, no word about it (run: alpine_create_user_with_sudo, alpine_manage_sudo_require_password).
- With no config file, the repository's LoadConfig asks "No configuration file found. Would you like to create one?" mid-screen (os_user_repository.go:20-33, config.go:383-396, read; the goldens seed $HOME/.hardn.yml to avoid it).

hardn.yml keys read: username, sudoNoPassword, sshKeys, dryRun (the screen; user_options.go:65, :110, :805, :401); sshKeyPath (the repository, by its own LoadConfig, os_user_repository.go:543-548); -c also reads sshPort, sshListenAddress, permitRootLogin, sshAllowedUsers (main.go:396-411). All read; the whole configuration is written back to ./hardn.yml (run: absent_*).

**Defects**

- Mutates in dry run (V2): create, sudo toggle, add and remove key, item 4 run as live; only the success line is dropped (run: create_dry_run_mutates, manage_sudo_*_dry_run, manage_keys_add_dry_run, absent_4_create_user_dry_run). Absent-menu items save hardn.yml under dry run (run: absent_2_toggle_sudo_password_dry_run).
- Sudo answer ignored (V22): "Allow sudo access? n" still gets usermod -aG sudo and sudoers ALL=(ALL) ALL; the summary says Disabled (run: create_sudo_no_still_grants_sudo).
- Key removal a no-op (V22): "SSH key removed successfully", authorized_keys unchanged (run: manage_keys_remove_is_a_noop).
- Keys re-appended with no dedupe on every sudo toggle and key change, through su ... cat >> (V22; run: manage_sudo_require_password).
- Hidden options: Create-a-user menu takes 3 (SSH Key Management) and 4 (rewrites the configured user's sudoers NOPASSWD, leaves the screen); 4 with no username; Remove with no key (run: hidden_3_ssh_keys_menu, hidden_4_updates_configured_user, hidden_4_declined, hidden_4_no_username, manage_keys_remove_hidden_no_keys).
- Alpine elevation (V14): wheel and sudoers.d, sudo never installed (run: alpine_create_user_with_sudo).
- "Require sudo password?" polarity: the form asks "Allow sudo without password?", Figma USER-CREATE_USER the opposite (run: create_sudo_no_password_with_key; proposals.md:257).
- V16: sudoers written in place, no visudo -c (run: create_*, manage_sudo_*).
- V17: users created with no password; "Required" sudo is then unusable (run: create_sudo_password_required_no_key).
- V21: root writes /home/<u>/.ssh, home assumed, sh -c chown with the name interpolated (run: manage_keys_add).
- Dropping the sudo password takes one key, no confirmation (run: manage_sudo_drop_password).
- Existence decided by os/user, the repository by id (read: user_options.go:69, os_user_repository.go:60).
- Cosmetic: "creatingd"/"updatingd" in the success line (run: absent_4_create_user, hidden_4_updates_configured_user); cancelling the sudo question reads two lines for one pause (run: manage_sudo_require_password_answer_n); SSH Key Management prints "Last login: 2023-10-01" and "This is a bottom line example." (run: hidden_3_ssh_keys_menu).
- hardn.yml saved to ./hardn.yml, not the loaded file (run: absent_1_change_username).

## 3. The mocks

Every row comes from real readers: the User row from `users.Admin.Check` through the main menu's own mapping (`output.StatusViewOf`), each user's admin rights from `users.ReadElevation`'s `GrantsOf`, hardn's own grant from `sudoers.Read` and `PolicyOf`, the session user from `sudoers.Invoker` (`SUDO_USER`, `DOAS_USER`), sshd's PAM from `ssh.ReadPAM`. Never from hardn.yml. *Since `next/s5-control`: every row is `users.Read`'s, each refusal the control's, each change `users.Admin`'s plan.* Grants are planned by the real `sudoers.PlanFor` and `steps.Sudoers` and run on the real engine; a lockout-risk plan asks the typed hostname and is confirmed or reverted, as Firewall's Enable is. Values are synthetic: george is the login user, alice a second one, `web-01`.

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
| `alpine-no-rule` | george in no doas rule: Grant writes `/etc/doas.d/zz-hardn-george.conf` (`hardn-george.conf` before `next/s5-control`) |
| `alpine-locked` | alice's field `!`, sshd without PAM: "Locked, key login refused" [I: not run] |
| `yml-missing` | hardn.yml names `deploy`, not on the host: nothing shows it (decision 7) |
| `not-read` | `visudo -c` rejects the files: every row "Can't read", every change refuses |
| `pve9` | Proxmox VE 9: root alone, no sudo; "No user besides root", no Manage User; Create without admin rights |
| `no-sudo` | Debian without sudo: Grant refuses, "sudo isn't installed" |
| `root-login` | `hardn`'s host, no `SUDO_USER` (root's SSH login, `su -`): removing george's grant, the last admin's, refuses; every key removal typed |
| `debian-nopam` | `UsePAM no` in a drop-in: george's `!` refuses key login; Create plans `--password '*'` |
| `alpine-stray` | `authorizedkeysfile .ssh/authorized_keys` alone, as the VM; a stray key in `authorized_keys2` not counted, so removing george's one real key refuses |
| `group-home` | alice's home 0775: Add SSH key refuses, as StrictModes would ignore the key |

Listing mocks besides `screen-<host>`: `select`, `manage`, `manage-alice`, `keys`, and `refusals` (fourteen, each from the real reading). 21 transcripts (`play/users-*`) play every flow, `users-create-no-admin` among them. `debian13`, drawn:

```
# User Management ##############################################

  [+] User                 Configured non-root, sudo

# george -------------------------------------------------------

  [ ] Sudo                 Enabled, No password to ask for
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

Where it departs from the frames (`pkg/ui/screen/frames.go`, kept): a section per user, its header the name, in place of 01-USER's one "User Configuration" section with a badge; UID:GID and Directory dropped for "Granted by" and "Password"; Manage User's items named by their result (P-12) in place of "Sudo Method, Toggle password requirement"; SSH Keys lists the keys by fingerprint and offers Remove. Item keys stay put: 3 is always SSH Keys, so a user with no admin rights has 1 and 3; with no user besides root, Manage User is not offered and Create keeps 2.

## 4. Heights

Every screen is as many lines at 80x24 and 80x40, Unicode and ASCII: full sections at every height, the rule the maintainer adopted for Firewall (gallery-firewall.md decision 9, 2026-11-01, "Adopt all ten"), applied here; no new decision. Over 24 rows: `alpine-locked` 33 (three users); 26, the two-user hosts (`debian13`, `ubuntu2604`, `alpine323`, `second-admin`, `one-key`, `yml-missing`, `debian-nopam`, `alpine-stray`, `group-home`). `not-read` 24. One user: 18 or 19; `pve9` 15; Manage User 20; SSH Keys 19; Select 10. A pasted key's echo (about 100 cells) overruns 80 columns; the goldens therefore answer no to Add SSH key, and the tests paste one. `pve9`, drawn:

```
# User Management ##############################################

  [ ] User                 Not Configured root only

# Users --------------------------------------------------------

   -  No user besides root

  Select an option

   2) Create User          Configure a new user

   0) Return

  > Enter your choice [0, 2 or q]:
```

## 5. Decisions for the maintainer

Drawn: every recommendation below. Not mocked: deleting a user, setting a password, group membership changes, `authorized_keys2`, root's keys on Proxmox VE.

**Adopted (the maintainer, 2026-11-03):** "Adopt all, start (b)." Every recommended answer below is adopted, and every Made here call as stated. What follows from them:

- No Delete User (1).
- Grants are hardn's own files; Remove takes away hardn's file alone and refuses, before asking, wherever another rule grants or would still grant (2 (a)). Unit (b) renames hardn's doas file so it sorts after `wheel.conf` (2 (a) (i)), in `pkg/host/sudoers` and [sudoers.md](../safety/sudoers.md) §2; nothing released has written the old name.
- Users are created key-only, `useradd --password '*'` where sshd's PAM is not established as on; "Ask for password" and Create's "Require password for admin rights?" refuse for a user with none; a no-password grant on such an account is a cost, not a failure (3 (a)).
- The lockout rules as drawn (4); the typed hostname with confirm-or-revert where §5 4 says, so [P-10](../refactor/proposals.md#p-10) is decided for User Management.
- origin/main's `/etc/sudoers.d/<user>` is left in place and named (5 (a)).
- `-c` and `-u` move to the new control, applied as `-g` is (6 (a)); built in unit (d).
- hardn.yml's `username`, `sudoNoPassword` and `sshKeys` are Create's defaults alone; a value set and unused warns once (7 (a)).
- [P-11](../refactor/proposals.md#p-11) Figma's polarity, default no, in §6's words; [P-13](../refactor/proposals.md#p-13) (a); [P-29](../refactor/proposals.md#p-29) (b); §6's words are decided (8).

1. **Scope: which changes the screen offers.** *Gates every decision below.* The mock offers create a user, grant and remove admin rights, ask or don't ask a password for them, add and remove keys. origin/main has no Delete User. (a) *Recommended:* those six, no delete: each is a file or a command hardn can journal and roll back; `userdel --remove` destroys a home directory, which no rollback restores. (b) Add Delete User, a refusal for the session user and a typed confirmation, with the home kept (`userdel` without `--remove`).
2. **How a grant is written, and what Remove does to one hardn didn't write.** *Gates 4, 5.* (a) *Recommended, drawn:* hardn's own drop-in per [sudoers.md](../safety/sudoers.md) §2 (`hardn-grant-<stem>`, `/etc/doas.d/hardn-<stem>.conf`); Remove removes hardn's file alone, and where cloud-init's file, origin/main's, or the sudo or wheel group grants it, refuses before asking, naming it (for a group, `gpasswd -d <user> <group>`); where other rules would still grant once hardn's file is gone, it refuses too, naming every one, read by the real reader with hardn's file and each rule found hidden in turn (a rule asking a password the user lacks is not counted; one the reader can't read refuses, naming it), so elevation is never reported removed while a rule still grants it.
   The order: sudo reads hardn's drop-in after cloud-init's, so hardn's wins. doas reads `/etc/doas.d` in name order and the last match wins, so `hardn-<stem>.conf` (sudoers.md §2 item 2) sorts before `wheel.conf` and loses for a wheel member. (i) *Recommended:* a doas file name that sorts after `wheel.conf`, so hardn's rule decides for its user as the sudo drop-in does; a unit (b) change to `pkg/host/sudoers`, not drawn. (ii) Refuse "Ask for password" for a wheel member, naming `wheel.conf`. The mock draws today's name, and its plan says `wheel.conf` overrides hardn's rule for a wheel member alone. (b) Group membership (`usermod -aG sudo`, `gpasswd -d`): no file, but the group's rule is the distribution's, `%sudo` asks a password the VMs' login users don't have, and Alpine's `wheel.conf` is `nopass`, so the password choice cannot be made per user.
3. **A user's password (V17).** *Gates 4.* On every VM the login user has none: `!` on Debian and Ubuntu, `*` on Alpine. (a) *Recommended, drawn:* create key-only, no password; refuse "Ask for password" for a user with none, and refuse yes to Create's "Require sudo password?", in Figma's polarity, with the reason. Wherever sshd's PAM is not established as on (Alpine's sshd has none, or `UsePAM no`), sshd refuses a key login to a `!` account, so `useradd --password '*'` (on the fake only; not run on a VM: the new user reads back unlocked). A `NOPASSWD` grant for an account with no password is drawn as a cost (warn, "No password to ask for", as the main menu's "No password to become root" is), not a failure; the alternative keeps it a failure. (b) Set one through `chpasswd` from a typed entry, twice; it puts a secret through hardn's input and its journal must never hold it. (c) Ask each time which.
4. **The lockout rules.** Refused, before asking: removing the session user's own elevation; asking a password the user lacks; removing the session user's last key; a grant hardn didn't write (2); with the session user not known (no `SUDO_USER` or `DOAS_USER`: root's SSH login, `su -`), removing the last admin's grant, and every key removal is then typed. Typed hostname, confirm or revert: removing another user's grant, asking a password the user has (`sudoers.RiskLockout`), removing any key of the session user's (sshd's log, which says which key it used, is not read), and anyone's last key. y/N: granting, not asking a password, adding a key, creating a user. *Recommended:* as drawn.
5. **origin/main's `/etc/sudoers.d/<user>` (V16).** Slice 9 left it here. (a) *Recommended:* leave it and say so: the row says "Granted by /etc/sudoers.d/george", Remove refuses naming it, and `PlanFor`'s legacy finding says whether hardn's rule wins. (b) Migrate: write hardn's grant, then remove the old file in the same plan, its env_keep line carried to `hardn-env-<stem>`; one plan, one rollback. (c) Remove it: loses the env_keep line slice 9 reads.
6. **`-c` and `-u`.** (a) *Recommended:* on the new control, applied as `-g` is: `-u <name>` plans Create with no key and admin rights, under the same refusals. (b) Retired, saying where the screen is. *Gated by* 1.
7. **hardn.yml's `username`, `sudoNoPassword`, `sshKeys`.** (a) *Recommended:* the form's defaults alone (Username, the admin answer, a key offered), never the user the rows are about: `yml-missing` shows that nothing names `deploy`; a value set and unused warns once, as Firewall's decision 8. (b) Unused, with that warning.
8. **The words.** P-11: Figma's polarity, "Require sudo password?" (drawn), default no. P-13 (a): "User", the detail the check's ("non-root, sudo"). P-29 (b): Figma's words, as drawn. §6 lists the new ones.

**Made here:** full sections at every height, Firewall's adopted rule (§4); one section per user (Figma's badge does not scale past one); key stays put per item; the uid range 1000 to 60000, not read from login.defs; the password state from `/etc/shadow`'s first character; the session key not known; Create's key optional, a notice when absent; a grant to a user with a password asks it, without one doesn't; a key is one OpenSSH public key, its type one sshd 10 takes, its blob naming the same type; keys counted only in the files sshd reads, and changed only where they include `~/.ssh/authorized_keys`; a key added only to a home and `~/.ssh` StrictModes takes (the user's own, or root's for the home, not writable by group or others); Create states its home, `--home-dir /home/<name> --create-home`.

## 6. Words

Proposed: one term, "admin rights", in Create's questions ("Grant admin rights?", "Require password for admin rights?") and Manage's items; the rows name the tool (Sudo, doas), and so does Manage User's detail ("Configure doas, SSH keys" on Alpine). "Granted by"; "Enabled, No password to ask for"; "Password": Set, None, "Locked, key login refused"; "No user besides root"; items "Grant admin rights" ("Let george become root"), "Remove admin rights" ("Take them away"), "Ask for password", "Don't ask password" ("For admin rights"), "Add SSH key", "Remove SSH key", "Remove which key?". Refusals: "You're running hardn through george's sudo. Removing it would leave no way to become root. Grant another user first."; "george is an admin through <file>, which hardn didn't write, so hardn can't remove it. Change it there."; "alice is an admin through the sudo group, which hardn leaves alone. Take alice out of it with gpasswd -d alice sudo."; "<user> would stay an admin through <rule> and the sudo group, …"; "hardn can't tell who you're logged in as, and george is the last admin. Grant another user first."; "sshd would ignore a key for alice: /home/alice is writable by its group (0775). …"; "george has no password, so sudo couldn't ask for one. Set one with passwd george, then try again."; "This is george's last key, and you're logged in as george. Add another key first."; "sudo isn't installed, so hardn can't grant admin rights. Install sudo, then try again."; "hardn can't read sudo's rules: its files don't check. Check them with visudo -c, then try again."; "A new user has no password, so sudo couldn't ask for one."; "Without a key carol can't log in: there's no password."; "sshd refuses alice's keys: the account is locked (!) and sshd doesn't use PAM."; the name and key checks' lines.

## 7. For unit (b)

*Since `next/s5-control`: every stand-in below is gone, built in the control ([control-users.md](control-users.md)) and [authkeys](../safety/authkeys.md); the fake host still answers `useradd`, `userdel` and `install`, and wheel.conf no longer overrides hardn's rule, which is `zz-hardn-<stem>.conf`.*

Stand-ins, each labelled in `users.go`:

1. `usrControl`: `users.Admin` with a `Plan`; unit (b) migrates `Admin` off `engine.CheckOnly`.
2. `usrDecidedBy`: the deciding rule and its NOPASSWD from the check's evidence strings. `users.Grant` says neither the rule nor the password; unit (b) needs them exported.
3. `readPasswords`: `/etc/shadow`'s first character. `accounts.Lock` folds `*` and a hash into `NotLocked`, so "no password" and "set" are not told apart; unit (b) needs a third state.
4. `readKeyFiles`, `readKeys`: `sshd.ReadEffective` does not expose `AuthorizedKeysFile` (the brief assumed it does); the mock reads sshd -T's `authorizedkeysfile` itself (Match blocks not read), counts keys in those files alone, and plans `authorized_keys`.
5. `parseKey`, `fingerprint`: an OpenSSH key check with no `x/crypto`; unit (b) decides whether to add the dependency.
6. `usrCreate` (`useradd --home-dir --create-home --shell`, `--password '*'` where PAM is not on; undo `userdel --remove`); check on a VM: a `*` account created so logs in by key without PAM (on the fake only so far), `usrKeysChange` (`install -d -m 700 -o -g`, then the file 0600, the user's), `stillAdmin`'s `hiddenSys`.
7. The fake host (`userSim`): `useradd` (its home only with `--create-home`; Alpine's 0755 [I: login.defs has no HOME_MODE; the VM's login home is 0755], `USERGROUPS_ENAB yes` as the VM), `userdel`, `install -d` and a rejecting `visudo -c`; Alpine's `adduser` (busybox) is not modelled.

Findings: for a user in wheel, `wheel.conf` overrides hardn's rule on the Alpine VM (decision 2): it sorts after `hardn-*.conf`, and doas's last match is wheel's `nopass`. `PlanFor` says "may override" for every user; the mock keeps it for a wheel member alone, as "overrides". On Proxmox VE the row is "Not Configured, root only" and only Create (no admin rights) works.
