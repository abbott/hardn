# SSH Login mock (slice 6, unit (a))

Unit (a) of slice 6, `next/s6-gallery`: a proposal for review. Nothing reaches hardn; the mock is in `internal/tools/uigallery` (`ssh.go`, `sshhost.go`).
Status: built and tested on macOS in process (`go test ./internal/tools/uigallery/`); no hardn binary ran, nothing ran on a VM. The hosts' shapes are the VMs' read of 2026-11-03 (`vm-runs/s6/00-inspect-*.log`), their values synthetic. *Since `next/s6-control` the mock runs on the real control, no stand-ins left ([control-ssh.md](control-ssh.md)).*

## 1. What to run

```
go run ./internal/tools/uigallery ssh [-ascii] [mock | all]              # the listing, no input
go run ./internal/tools/uigallery play ssh                                 # the menu of the hosts
go run ./internal/tools/uigallery play ssh/<host>                          # one host
go run ./internal/tools/uigallery play -sequence dry-run ssh/<host>        # plan only
go run ./internal/tools/uigallery play                                     # main menu 2 leads to ssh/debian13
```

Main menu 2 in play now opens the mock, as 1 opens User Management. One existing golden changes: `play/ssh-toggle`, which typed Figma's hostname into the fixture's toggle and now records the mock's Disallow root login refusing those two wrong names (the typed phrase is `web-01`). The main menu's own goldens, `sub-menu/02-ssh-login`, `workflow-step/ssh-login-toggle-confirmation` and the fixtures in `pkg/ui/screen/frames.go` are unchanged and kept.

## 2. What origin/main leaves on hosts

- `/etc/ssh/sshd_config.d/hardn.conf` (`sshd_config` itself on Alpine), from `SaveSSHConfig`: `# SSH configuration managed by Hardn`, `Protocol 2`, `StrictModes yes`, `Port`, `ListenAddress`, `AuthenticationMethods publickey`, `PubkeyAuthentication yes`, `PermitRootLogin no|yes`, `AllowUsers`, `PasswordAuthentication no`, `PermitEmptyPasswords no`, one `AuthorizedKeysFile` line per key path. Main menu 2 and `-d` write it through `DisableRootSSH` with the stub's `Port 22` and no key path, so `AuthorizedKeysFile .ssh/authorized_keys`, where that build put the keys (side-effects.md D7), then `systemctl restart ssh` (`rc-service sshd restart`). Its `AuthenticationMethods publickey` keeps passwords off even where `50-cloud-init.conf`'s `PasswordAuthentication yes` is read first.
- v0.3.x: `sshd_config.d/manage.conf` (or hardn.yml's `sshConfigFile`), with `AuthorizedKeysFile    .ssh/authorized_keys    .ssh_%u/authorized_keys` and the keys in `~/.ssh_<user>/` (v032-reference.md §4.2, V5); `/etc/systemd/system/ssh.socket.d/listen.conf`; and Alpine's generated `sshd_config` ([sshd-migrate.md](../safety/sshd-migrate.md) §2, item 2's table).

Each is recognised by its first lines and moved by `PlanMigration` (§3.17, decided).

## 3. The mocks

The three rows are the main menu's: `Engine.Check` over `controls.Status`, `risk.Assess`, `output.StatusViewOf`, the SSH rows kept. The section is sshd's: `sshd.ReadEffective` and `migrate.Find`, never hardn.yml; a note names the file that decides a value where it is not `00-hardn.conf`. Admins come from `users.Read`, the session from `revert.ReadSession` and sshd's log. Each change is `steps.PlanSSH` or `PlanMigration` on the real engine, every change lockout-risk, so typed and confirmed or reverted. Synthetic values: george, alice, `web-01`, 192.0.2.10, 198.51.100.7, 2230. Each host is self-consistent: the session's `Accepted` line is one sshd could have written with its files; where none could, hardn runs as root on the console.

The key-path defect (V5, D7, R8): the migration keeps every key file sshd read (`AuthorizedKeysFile` merged, sshd-migrate.md §2.6; `play/ssh-migrate-main` shows it), and the admin proof counts only keys in files sshd reads, so a key in a file it does not read leaves its admin not able.

| Host | What it shows |
|---|---|
| `debian13` | The VM: Include first, passwords off in `sshd_config`, root `*` with george's key; root key-only by default; Disallow root login plans `PermitRootLogin no` |
| `ubuntu2604` | ssh.socket, `60-cloudimg-settings.conf`; the port change through `daemon-reload` and a socket restart |
| `alpine323` | sshd without PAM, root `!`: "root locked", Root SSH Login "Locked" (check-ssh.md §9); keyboard-interactive yes but no password path; no ufw, no `ss` |
| `hardened` | Figma's state on 2230, the session on 2230: Allow root key-only, Password login on |
| `password-login` | `50-cloud-init.conf` yes beats `sshd_config`'s no; Key-only login refuses for alice (a password, no key) |
| `password-session` | The same, alice logged in by password: Key-only login refuses |
| `root-session` | hardn from root's own SSH login: Disallow root login refuses |
| `legacy-v032` | manage.conf (v0.3.2's two key paths, george's keys in `~/.ssh_george`) and a listen.conf never in effect: every change asks Move old settings, which the planner refuses as P-39 (a) decided (2026-10-19), naming the hand steps; once listen.conf is in effect item 4 plans the migration |
| `legacy-main` | origin/main's `hardn.conf`: `50-cloud-init.conf`'s `PasswordAuthentication yes` read first, but `AuthenticationMethods publickey` keeps passwords off (SSH Auth, which does not read it, says on); items 1-3 refuse; Move old settings plans the migration |
| `include-other` | `Port 22` above the Include (P-38): rows read, every change refuses |
| `match-allows` | A Match block lets root and passwords in from 192.0.2.0/24: rows Partial; Disallow root login refuses |
| `allow-users` | `AllowUsers george`: alice, with a key, shown unable; george enough |
| `pve9` | A stock node, root allowed: item 1 "Root key-only" plans prohibit-password; the port refuses |
| `ufw-restricted` | ufw allows 22 from 192.0.2.0/24: Change SSH port refuses |
| `confirm-window` | 22 and 2230 held, `hardn confirm` pending: the engine refuses another change |
| `not-read` | No `/run/sshd`: rows and items Can't read, every change refuses |
| `nopam-locked` | george `!`, the only admin, no PAM, so root on the console: Disallow root login refuses |
| `rsa-small` | george's only key RSA 1024, `RequiredRSASize 2048`; root on the console: Disallow root login refuses, naming it |
| `admin-no-sudo` | george by key, but `%sudo` asks a password his `!` hasn't got; root on the console: Disallow root login refuses |

The listing adds `refusals`, eighteen, each from the real reading, five on variants the menu does not list (`allow-users-host`, `no-accepted`, `pve9-password`, with `rsa-small` and `admin-no-sudo`). 16 transcripts play the flows (`play/ssh-*`). `debian13`, drawn:

```
# SSH Login ####################################################

  [ ] SSH Login            Partial root key-only
  [+] SSH Auth             Configured key-only auth
  [ ] SSH Port             Not Configured default (22)

# SSH Configuration --------------------------------------------

   -  Root SSH Login       Key only sshd's default
   -  Password Login       Off by sshd_config
   -  SSH Port             22
   -  Allowed Users        Anyone
   -  Logged in as         george, by key

  Select an option

   1) Disallow root login  Root can't log in over SSH
   2) Password login on    Let passwords log in again
   3) Change SSH port      Now 22

   0) Return

  > Enter your choice [0-3 or q]:
```

Departures from the frames: title "SSH Login", not "Manage Root SSH Login"; the SSH Port row is the main menu's, and SSH Auth is added; the section adds Password Login, a Match Block row, Old Settings and Logged in as, each value's deciding file as its note; items named by their result (P-12), View SSH Settings dropped (decision 1), item 4 only with old files.

## 4. Heights

Every screen is as many lines at 80x24 and 80x40, Unicode and ASCII, no line over 80 cells: full sections at every height, the rule the maintainer adopted for Firewall (gallery-firewall.md decision 9, 2026-11-01), applied here as in gallery-users.md §4; no new decision.

| Lines | Hosts |
|---|---|
| 23 | debian13, ubuntu2604, alpine323, hardened, password-*, root-session, include-other, allow-users, pve9, ufw-restricted, confirm-window, nopam-locked, rsa-small, admin-no-sudo |
| 24 | match-allows, not-read |
| 25, 26 | legacy-main, legacy-v032: over 24, their top scrolls off |

## 5. Decisions for the maintainer

Drawn: every recommendation below but 7 (built in `pve.PlanSSH`, no host shows it) and 8 (unit (d)'s). Not mocked: AllowUsers changes, ListenAddress, a confirm from a new connection after the root and password changes (the engine scenario `confirm-cleanup-debian13` plays the port's), P-36 on a node with AllowUsers.

**Adopted (the maintainer, 2026-11-03):** "Adopt all, start (b)." Every recommended answer below is adopted, and every Made here call as stated. What follows from them:

- The scope of (1): root login both ways, never "yes"; password login off and back on; the port; Move old settings; allowed users shown, not changed; no View SSH Settings ([P-28](../refactor/proposals.md#p-28) (a) for SSH Login).
- The proof of (2) (a), before asking, then the typed hostname and confirm-or-revert from a new connection, for every SSH change ([P-10](../refactor/proposals.md#p-10) decided for SSH). Unit (b) builds it into `pkg/engine/controls/ssh`, with the SSH Auth check reading `AuthenticationMethods`.
- Root login (3 (a)), password login (4 (a)), the port (5: [P-33](../refactor/proposals.md#p-33) (2) (a), refuse), old files and the Include (6 (a); [P-38](../refactor/proposals.md#p-38) (a)), [P-36](../refactor/proposals.md#p-36) (a) (7).
- `-d` on the new control and `-c -d` back (8 (a)), in unit (d); hardn.yml's SSH keys as 9 (a).
- [P-12](../refactor/proposals.md#p-12) and the 02-SSH frames of [P-31](../refactor/proposals.md#p-31): §6's words are decided (10).

1. **Scope.** *Gates all below.* What the screen changes. (a) *Recommended, drawn:* root login (disallow; allow key-only back, never "yes"), password login (off; back on), the port, and moving hardn's old files; allowed users shown, not changed; View SSH Settings dropped, as the section already shows what it would (P-28 (a)). (b) Keep View SSH Settings as a screen of its own (P-28 (b), a new layout). (c) Also an Allowed Users form: one more lockout path (an admin left out), its checks 2's.
2. **What closing a login path needs as proof.** *Gates 3, 4, 8.* The rule slice 5's `-c -d` review asked for. (a) *Recommended, drawn:* before asking, for each admin: sshd's own answer from the session's address (`sshd -T -C user=,host=,addr=`: sshd applies the Match blocks); `AllowUsers` and `DenyUsers` matched by hardn against that address (`USER@HOST`: an address, CIDR or pattern; a matching deny wins); a key in the files sshd names, of a type `PubkeyAcceptedAlgorithms` accepts, an RSA key at least `RequiredRSASize` bits; StrictModes; PubkeyAuthentication; one `AuthenticationMethods` list met by what the admin has (key; password; keyboard-interactive with PAM); the account's lock without PAM; and becoming root after (a grant asking no password, or one the admin has). Not matched, so the admin is not shown able: `AllowGroups`, `DenyGroups`, a host-name pattern (the client's name is not read). On Proxmox VE, root's own key, as root stays key-only. Then the typed hostname and confirm-or-revert, confirmed from a new connection. Refused: root's own SSH login (on Proxmox VE, by password), a password session for key-only, a session whose line sshd's log does not have; from a console, the admins alone. (b) The reading alone, no timer: one less step, no net if the reading is wrong. (c) The timer alone: no refusal before asking, so a lockout is only undone after it happens.
3. **Root login.** *Gated by 2.* (a) *Recommended, drawn:* Disallow writes `PermitRootLogin no`; on Proxmox VE `prohibit-password`, the item read-only once there (P-12); Allow root key-only offered back. A Match block that lets root in more refuses, naming it. (b) No way back: re-allowing is then a hand edit.
4. **Password login.** *Gated by 2.* (a) *Recommended, drawn:* `PasswordAuthentication no`, and `KbdInteractiveAuthentication no` where sshd uses PAM; an admin with a password and no key refuses, naming them; Password login on offered back. (b) That admin as a cost in the plan, not a refusal: they lose SSH login, warned.
5. **The port.** The form takes 1 to 65535, not one sshd has; in use, ssh.socket and Proxmox VE (refused, §3.18) are the planner's; `hardn confirm` drops 22 (P-40). A 22 that ufw restricts by source (P-33 (2)): (a) *recommended, drawn:* refuse, naming the source, until the rule is moved by hand; no new rule form. (b) Give the new port the same source: right in the end, new ufw rule forms and their undo (M). (c) As built: open the new port to anyone, reported.
6. **hardn's old files and an Include not first.** (a) *Recommended, drawn:* migration as its own item 4, the other items refusing until it is done, so each plan has one purpose and one rollback; P-38 (a), refuse, naming the line. (b) Migration inside each change's plan: one less step, a bigger rollback. P-38's (b) to (d) as listed there. A listen.conf never in effect (`legacy-v032`) is not a new decision: P-39 (a), decided 2026-10-19, keeps refusing with a report; the refusal gives the hand steps (`systemctl daemon-reload`, `systemctl restart ssh.socket`), and once listen.conf is in effect item 4 plans the migration.
7. **Proxmox VE's AllowUsers without root (P-36).** (a) *Recommended:* as built, `pve.PlanSSH` adds `root` and says so; root stays key-only. (b) Refuse. (c) `root@<node>` from corosync (M).
8. **`-d`, and `hardn -c -d`.** *Gated by 2.* (a) *Recommended:* `-d` plans the new control's Disallow, refused under the same rules, typed on a terminal and refused off one, as `-w` (`cmd/hardn/typed.go`); `-c -d` returns once 2 is adopted, as its guard is then -d's own. (b) `-c -d` stays refused (`pkg/cmd/create_disable_root.go`).
9. **hardn.yml's SSH keys.** `sshPort`, `permitRootLogin` (the bool's warning §3.17 owes), `sshAllowedUsers`, `sshListenAddress`, `disableRootSSH`, `sshKeyPath`, `sshConfigFile`. (a) *Recommended:* none drives the screen, which reads sshd; `sshPort` is the form's default; `sshConfigFile` is read for migration alone; the rest unused, a value set warning once, as Firewall's decision 8. (b) Run All's, until slice 8.
10. **The words.** P-12 as drawn; P-31 for 02-SSH_LOGIN and SSH_LOGIN-TOGGLE_CONFIRMATION: Figma's section and rows, the title "SSH Login" as it covers more than root, §6's words.

**Made here:** every change typed, as PlanSSH makes each lockout-risk, Password login on included; refusals before asking from the engine's own plan where it has one (`preplan`); the deciding file from the first line sshd reads; Logged in as from sshd's log; AllowGroups and DenyGroups not read, an admin under either not shown able; from a console (no `SSH_CONNECTION`), no session to keep, Logged in as Not known; Password Login read through `AuthenticationMethods`, its note "by AuthenticationMethods" ("in <file>" does not fit at 80 columns; the file is Old Settings' or `00-hardn.conf`); hardened's session on 2230, as the reload refuses a session the files no longer cover.

## 6. Words

Decided (2026-11-03, decision 10): title "SSH Login"; rows "Root SSH Login" (Disallowed, Key only, Allowed, Commands only, Locked "root's account, no PAM"), "Password Login" (Off, On, "On, keyboard-interactive"), "SSH Port", "Allowed Users" (Anyone), "Match Block" (Lets more in), "Old Settings", "Logged in as" ("george, by key", "by password", Not known); notes "sshd's default", "by <file>", "by AuthenticationMethods", "ssh.socket"; every item's description "Can't read" where sshd -T is not read. Items: "Disallow root login" (Root can't log in over SSH), "Allow root key-only" (Root logs in by key alone), "Root key-only" (Kept for the Proxmox VE cluster), "Key-only login" (Turn password login off), "Password login on" (Let passwords log in again), "Change SSH port" (Now 22), "Move old settings" (Into 00-hardn.conf); "New SSH port". Refusals: "You're logged in as root over SSH. Turning root login off would close the way you came in. …"; "No admin could log in over SSH afterwards: <why>. Fix that first."; "You logged in with a password. …"; "alice would lose SSH login: alice has a password and no key. …"; "<Match line>, in <file>, lets more in …"; "ufw lets port 22 in from <source> alone. …"; "hardn's old SSH files are still here (<files>). Move them into 00-hardn.conf first: item 4."; "sshd_config doesn't start with …"; "A change is waiting for hardn confirm. …"; "/run/sshd isn't there yet, …"; "The SSH port stays 22 on Proxmox VE: …"; "You logged in as root with a password. Making root key-only would close the way you came in. …"; "Root would lose SSH login: <why>. Add a key for root first."; "hardn v0.3.x's listen.conf (port 2230 for ssh.socket) never took effect: ssh.socket listens on 22. … run systemctl daemon-reload and systemctl restart ssh.socket, …" (five lines); the form's two. The whys: "<user> is in DenyUsers", "AllowUsers lets <user> in from <host> alone", "AllowUsers names <user>@<host>, a host name hardn doesn't match", "sshd doesn't accept <user>'s key type (<type>)", "<user>'s RSA key is <n> bits, under RequiredRSASize <m>", "AuthenticationMethods <lists> asks more than <user> has", "<user> can't become root: sudo asks a password <user> hasn't got".

## 7. For unit (b)

*Since `next/s6-control`* ([control-ssh.md](control-ssh.md)): items 1 to 4 are gone, the gallery reading `sshchange.Read` and planning `sshchange.Root`, `Auth`, `Port` and `Migrate`; item 5, the fake, stays. The SSH Auth check reads `AuthenticationMethods` (check-ssh.md §10), so `legacy-main`'s SSH Auth row is Configured "key-only auth". The rest below is as unit (a) left it.

Stand-ins, each labelled in `ssh.go`:

1. `sshControl2`: the SSH controls with a `Plan`; unit (b) migrates Login, Auth and Port off `engine.CheckOnly`.
2. `sshLoginPaths`, `sshLoginPath`, `sshdSays`: per admin, sshd's answer, `AllowUsers`/`DenyUsers` against the address (`sshListed`, `sshPattern`), `AuthenticationMethods` (`sshMethodsMet`), key types and RSA size (`sshTypeAccepted`, `sshRSABits`, as `authkeys` exposes no modulus length), becoming root (users.Read's grant and password); root's own key on Proxmox VE; gone into `pkg/engine/controls/sshchange` (`LoginPaths`, `RootPath`, `loginPath`), AllowGroups and DenyGroups not read, an admin under either not shown able, as decision 2 and Made here say.
3. `sshSessionLogin`: sshd's log for the session's connection; journalctl and `/var/log/messages` as the VMs log.
4. `restricted22`: ufw's source for 22, from `ufw.Read` in place of the status text.
5. The fake (`sshSim`): Match blocks for `-C` (User, Address, Host; Group not matched), `requiredrsasize` for `-C`, sshd's log, `/run/sshd` missing, no `ss` on Alpine; a console session (`onConsole`: hardn under login(1)).

Findings: the real SSH Auth check (`pkg/engine/controls/ssh`) does not read `AuthenticationMethods` (check-ssh.md §2.11), so on `legacy-main` it says passwords on where the section says off: unit (b) must read it. On `legacy-v032` (D9) the migration is refused (SSH_LISTEN_CONF_NOT_IN_EFFECT), so every SSH change is too until listen.conf takes effect; a session on a port the files no longer configure blocks every reload (`ErrNotInEffect`); on Alpine no `ss`, so a new port in use is not caught (`CheckInEffect` skips the bound check); the checks read an Include not first, the planner alone refuses it; a port refused after the form is the engine's message, not §6's; the listen.conf refusal is shown in §6's words, the engine's report not logged.

## 8. Not run

No hardn binary, no VM: not sshd's real `-T -C` (its `requiredrsasize` and `AllowUsers USER@HOST` included), its log lines as hardn reads them, a console session's `/proc` as `revert.ReadSession` reads it, `hardn confirm` from a new connection after the root and password changes, a reload on Alpine with a port in use, the Debian VM's `90-runner.conf`.
