# Migrating legacy SSH files into 00-hardn.conf (`pkg/host/sshd/migrate`, `pkg/config`)

> **Unit:** refactor plan Phase 2, `next/p2-sshd-migrate` (decision §3.17, approved 2026-10-17: migrate the legacy artifacts automatically, with a backup and a printed note; `permitRootLogin` takes `"no" | "prohibit-password" | "yes"` and still reads the old bool, with a deprecation warning; §3.19 for writing the canonical value back). V-numbers: V24 (the v0.3.2 → origin/main upgrade path: keys in `~/.ssh_<user>`, `manage.conf` and `listen.conf` left behind, `sshConfigFile` ignored), the migration half of V13 (`hardn.conf` loses to `50-cloud-init.conf`), V5 with D7 (which `AuthorizedKeysFile` the keys are under), V15's restore step (Alpine's whole `sshd_config`), D8 (`sshConfigFile` dead) and V11's config half (the key can say `prohibit-password`) ([side-effects.md](../refactor/phase0/side-effects.md) §8, §9). Risk R8 (plan §4).
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only. `sshd -t` and `sshd -T` are answered by a small model of sshd's reading in the tests (`modelT`), not by sshd; legacy files are reconstructed from the v0.3.1, v0.3.2 and origin/main sources (§3), not captured from hosts. Nothing has run on Linux; §4 lists what is "written, not run (gate 5)".

`migrate.Find(ctx, sys, req)` reads the host and names every SSH file a legacy hardn wrote. `migrate.PlanFor(in)` turns those files, the settings hardn wants and the installed configuration into steps, with no I/O. `migrate.Apply(ctx, sys, plan, opts)` validates every state the steps lead to with `sshd.Validate`, refuses one with fewer working login paths than the installed configuration, backs up every file it changes with `backup.Run` (RollbackRequired), checks nothing changed since the plan, and writes atomically. It reloads nothing. In `pkg/config`, `permitRootLogin` reads the three strings and the old bool, and `config.Save` writes the canonical string.

## 1. Why

Brief §2 decides that hardn owns one drop-in, `/etc/ssh/sshd_config.d/00-hardn.conf`, read before every other drop-in. Every earlier hardn wrote somewhere else ([side-effects.md](../refactor/phase0/side-effects.md) §10): v0.3.x a `manage.conf` (or the `sshConfigFile` its `hardn.yml` named) and an `ssh.socket.d/listen.conf`; the unreleased builds after it a `hardn.conf` that `50-cloud-init.conf` beats; on Alpine every ref the whole `sshd_config`. A host upgraded from v0.3.2 to an origin/main build has `manage.conf` and `hardn.conf` both. Writing `00-hardn.conf` beside them leaves their lines in force where `00-hardn.conf` says nothing, `listen.conf` keeps overriding the socket's port (so `p2-reload` refuses a port change, `ErrSocketDropIn`), and deleting them blindly can drop the only `AuthorizedKeysFile` that names `~/.ssh_<user>`, where v0.3.x put the keys (R8). Never locking the user out (brief §3 principle 1) means the migration keeps every working login path and every key file, and can be undone.

## 2. Guarantees

### Finding the artifacts (`Find`)

1. **Read-only.** `Find` reads `sshd_config`, every `*.conf` in `sshd_config.d` that sshd's `Include` matches (a leading `.` never matches, following symlinks as sshd does, in C byte order), the fixed legacy paths and `FindRequest.ConfigFile`. It makes no other call (tested: every recorded call is a read).
2. **An artifact is recognised by what hardn wrote first**, never by its path alone:

   | Kind | Path | Recognised by |
   |---|---|---|
   | `KindManageConf` | `sshd_config.d/manage.conf` | its first two lines, `### Reference` and the cryptsus URL (v0.3.1 and v0.3.2), or `# SSH configuration managed by Hardn` |
   | `KindCustom` | `FindRequest.ConfigFile`, v0.3.x's `sshConfigFile` | the same, and it must be a `*.conf` in `sshd_config.d` |
   | `KindHardnConf` | `sshd_config.d/hardn.conf` | the same |
   | `KindListenConf` | `/etc/systemd/system/ssh.socket.d/listen.conf` | exactly `[Socket]`, `ListenStream=`, `ListenStream=<port>`, port 1 to 65535 |
   | `KindGeneratedMain` | `sshd_config` | `# SSH configuration managed by Hardn` first (origin/main, including its disable-root stub), or v0.3.x's Alpine template: `Protocol 2`, `StrictModes yes`, a blank line, `Port ` first, and its `HostbasedAcceptedKeyTypes …` line |

   `Generation` says which hardn wrote it (`GenV03`, `GenMain`). A legacy file is read with `ReadFileNoFollow` after an `Lstat`: a symlink or anything not a regular file is not an artifact.
3. **What hardn did not write, or sshd does not read, is left and said why** (`Found.Left`): a file at a legacy path without hardn's first lines (not hardn's, or edited at the top); a masked or linked `listen.conf`; an `sshConfigFile` that is relative or unclean (where v0.3.x wrote it is not known); one outside `sshd_config.d` or not `*.conf` (sshd never read it, so removing it gains nothing, and an Include of it would be refused by `sshd.Validate`); and an `sshConfigFile` of `sshd_config` itself, where v0.3.x wrote its drop-in over the main file: restoring a distribution's file there is not automated (§6, open for the maintainer). An `sshConfigFile` of `manage.conf`, `hardn.conf` or `00-hardn.conf` adds nothing (the first two are always looked at; the last is the target). Tested in a table: the stock host, v0.3.2, v0.3.2 upgraded to an unreleased build, the unreleased `-c` file, a custom drop-in, each `Left` case, and both Alpine generations.

### Planning (`PlanFor`, pure)

4. **The steps, in order:** write `00-hardn.conf` (`OpWriteDropIn`); remove each legacy drop-in (`OpRemove`); on Alpine, write `00-hardn.conf` and then `sshd_config` in the structure of Alpine's stock file (`OpRestoreMain`, one step, §2.12); remove `listen.conf` last (`OpRemoveSocketDropIn`). Each step has a `Note`, the printed note §3.17 asks for, saying what it does to which file and that a backup is kept; the caller adds where (`backup.Run.Dir`). Nothing to migrate is an empty plan, and `Desired` alone never makes one (the engine writes `00-hardn.conf` itself then). Tested per host shape, with goldens of every file written (`testdata/golden`).
5. **The effective configuration does not change, except for `Desired`.** `00-hardn.conf` is `Desired` first, then the lines of the files it replaces (the legacy drop-ins, an existing `00-hardn.conf`, the generated Alpine `sshd_config`), in the order sshd reads them (`sshd.ReadOrder`):
   - a first-value keyword's line moves when it is the first line sshd reads for that keyword and `Desired` does not set it; otherwise it is dropped, with why (`Plan.Dropped`: "no effect: sshd reads … first", or "hardn's settings set …"). `00-hardn.conf` is read before every other drop-in and, in `IncludeFirst` mode, before the rest of `sshd_config`, so a line that won before still wins, and one that lost (to `50-cloud-init.conf`, say) is not promoted over the file that beat it. Tested: v0.3.2's `PasswordAuthentication no` stays behind `50-cloud-init.conf`'s `yes`;
   - an accumulating keyword's line (Port, ListenAddress, HostKey, HostCertificate, Allow/Deny Users/Groups, AcceptEnv) moves unless `00-hardn.conf` already has that line;
   - lines under a `Match` never move: a legacy file with a `Match` block is left (`Left`), and an existing `00-hardn.conf` with one is refused (`fs.ErrInvalid`).

   `Plan.Carried` lists the lines that moved.
6. **No key file stops being read (R8).** `AuthorizedKeysFile` becomes one line naming every file `Desired`, the replaced files and the line sshd reads first name, or sshd's default from `Input.Before` (`.ssh/authorized_keys .ssh/authorized_keys2`) when no line names one. An unreleased build's bare `.ssh_%u` (V5: a file name where v0.3.x had a directory, and keys in `~/.ssh`, D7) adds `.ssh_%u/authorized_keys` and `.ssh/authorized_keys`. So a v0.3.2 host keeps `.ssh/authorized_keys .ssh_%u/authorized_keys`, and `Desired` can never narrow the list; dropping a path is a later, confirmed change.
7. **`listen.conf` (v0.3.x).** On a socket host whose generator derives `ssh.socket`'s listeners from the sshd files (`SSHSocket`, `Generator` Yes) and with the stock first-line Include, its port moves into `00-hardn.conf` and the file is removed. On `ssh.service` and OpenRC hosts `ssh.socket` does not listen, so it had no effect and is removed. Otherwise it is left, with why: a socket host whose port lives in `ssh.socket.d` (Debian opted into the socket, or the generator masked: its `listen.conf` may be the administrator's own way of setting the port), an Unknown SSH mode, or no stock Include.
8. **The socket's listeners are kept (`Plan.Hold`).** `listen.conf`'s `ListenStream=<port>` is a dual-stack `[::]:<port>` (§3), so its port is held for both families. On a socket host with the generator, every listener `ssh.socket` has now (`Input.Before.Listeners.Socket`) is held too, whenever the plan writes the files: the generator gives the socket the files' listeners at the next `daemon-reload`. That covers v0.3.x's missing `daemon-reload` (D9): until a reboot such a socket still listens on port 22, not on `listen.conf`'s port, and the plan keeps both (tested: `v032-ubuntu-unreloaded`). A held listener the files do not configure is added: as `ListenAddress <address>:<port>` lines when the files have a ListenAddress, otherwise as `Port <port>`, with `Port 22` beside it when the files had no Port line, because they configured sshd's default 22 until then. Without the socket's listeners on such a host, `PlanFor` refuses (`ErrNotEstablished`), as it does when a held listener has to be added and a Port, ListenAddress or AddressFamily line is one hardn's model does not read (a host name, a service name).
9. **No two listeners that cannot both be bound.** A ListenAddress line the plan moved or added is dropped (`Plan.Dropped`, with why) when the other ListenAddress lines already configure every listener it does: on an upgraded host, `manage.conf`'s `ListenAddress 0.0.0.0:2230` beside `hardn.conf`'s `Port 2230` and `ListenAddress 0.0.0.0`. Two sockets on one port of one family, at one address or one of them unspecified, cannot both be bound (§3), and the generator would hand both to `ssh.socket`. Lines of `Desired` and of the files the plan leaves are never dropped; an overlap that remains is refused by `Apply` (§2.15).
10. **`Plan.Keywords`** lists the keywords whose lines the steps write, move or remove (lower case, first word), with `port` when `listen.conf`'s port moves: `reload.Change.Keywords` for the reload that follows. On a socket host a migration of `listen.conf` is therefore a listener change (`daemon-reload` and a restart of `ssh.socket`).
11. **Refusals before anything is planned:** a `Desired` keyword that is not a plain word, a `Match` or `Include`, a directive with no argument, an argument with a line break or a NUL (`fs.ErrInvalid`); an unknown Include mode (`fs.ErrInvalid`); legacy drop-ins on a host whose first directive is not the stock Include (`ErrIncludeOther`: `00-hardn.conf` would not be read first, and [sshd-validate.md](sshd-validate.md) §6 leaves that mode to the maintainer). With no Include at all (an Alpine host upgraded from 3.18 or earlier), legacy drop-ins are removed with a note that sshd never read them. Arguments are written back quoted as sshd's `argv_split` reads them (tested: each form reads back as the same arguments).
12. **Alpine's generated `sshd_config` is restored only as an explicit step** (§3.17: "restore the distribution default structure only with an explicit plan step and backup"), on Alpine, in `IncludeNone` mode, without a `Match` block. The new file has the Include first, then the directives Alpine's stock file does not comment out (§3): `AuthorizedKeysFile .ssh/authorized_keys`, `AllowTcpForwarding no`, `GatewayPorts no`, `X11Forwarding no`, `Subsystem sftp internal-sftp` (left out when `00-hardn.conf` defines that subsystem: OpenSSH refuses a second definition). Every line of the generated file goes to `00-hardn.conf`, which the Include reads first, so its values stay; the stock lines add what the generated file lacked (sftp, V15) and Alpine's forwarding defaults. Once there is an Include, sshd also reads the other drop-ins in `sshd_config.d`; the note names them, and `Apply`'s comparison catches any that would narrow a login path. Comments of the generated file are not kept. Off Alpine, or with an Include already, the file is left.

### Applying (`Apply`)

13. **Validated before anything exists.** `Apply` first runs `sshd.Validate` (`sshd -t`, `sshd -T`, one `-T -C` per `Match`) over the host with every step applied, and compares the result with `Plan.Before`; then, before each step, over the host as it is with that step applied. A file is written or removed only after the state it leaves passed (tested: no write or removal of an sshd file without an `sshd -t` since the previous one; the Alpine step's two writes are validated together, since `00-hardn.conf` is read only once `sshd_config` has the Include). `listen.conf` is not sshd's and goes last, after every sshd state passed. The write comes before the removals, so every intermediate state configures at least what the final one does.
14. **Never fewer working login paths.** Every state is refused with a `*NarrowError` (`ErrFewerLoginPaths`), naming each keyword, when its `sshd -T` shows, against `Plan.Before`'s: a `yes` turned off among `PubkeyAuthentication`, `PasswordAuthentication`, `KbdInteractiveAuthentication`, `PermitEmptyPasswords`, `UsePAM`, `HostbasedAuthentication`, `GSSAPIAuthentication`, `KerberosAuthentication`; a lower `PermitRootLogin` (no < forced-commands-only < prohibit-password < yes); an `AllowUsers`/`AllowGroups` that newly restricts or no longer lists someone; a new `DenyUsers`/`DenyGroups` entry; a key file no longer in `AuthorizedKeysFile`; an `AuthenticationMethods` other than `any` that differs; any change to the algorithm lists, host keys, authorized-keys and principals commands and files, CA keys, revoked keys, `PubkeyAuthOptions`, `MaxAuthTries`, `ForceCommand` or `ChrootDirectory`; and a listener of `Before`, or of `Hold` (both families for `[::]`), no longer configured. Widening is not a narrowing (tested per rule). A `Before` without the values compared (`sshd -T` not read, a setting Unknown, a listener unread) is `ErrNotEstablished`. A narrowing that `Desired` asks for (`PasswordAuthentication no`) is refused here: it is the engine's change, after the migration, under confirm-or-revert (brief 2.3 step 6).
15. **No new overlapping listeners.** The final state is refused (`ErrListenerOverlap`) when its `sshd -T` listeners have a pair §2.9 describes that `Before`'s do not (tested: a site drop-in's `ListenAddress 192.0.2.10:2230` beside the `0.0.0.0:2230` that `listen.conf`'s port needs).
16. **Backed up first, and as the plan read it.** After the whole-plan check and before the first change, every file a step writes or removes is backed up (`backup.Run.Backup`, `RollbackRequired`, so even with `enableBackups: false`, §3.16), `00-hardn.conf` too when absent (its rollback removes it). Each backup must then show the file as `PlanFor` read it (`Plan.Sources`: absent, or the same SHA-256); otherwise `ErrChanged`, naming the file, and nothing is written (tested for `manage.conf`, `listen.conf` and `00-hardn.conf` edited between plan and apply). The backup run directory then exists with its manifest, and restoring it changes nothing.
17. **Writes and removals** go through `host.WriteFileAtomicKeep` (an existing file keeps its mode and owner; a new `00-hardn.conf` is root's, `0644`) and the guarded `Remove` ([atomic.md](atomic.md) §2). `Apply` stops at the first error. `Report.Done` lists the steps carried out; an Alpine step that failed between its two writes is not in it, but its first write is in `Report.Backups` like every other, so `backup.Restore` of the run (under `context.WithoutCancel` after Ctrl-C) gives the host back as it was (tested: after every scenario, and after a validation failure midway).
18. **Nothing is reloaded.** The running sshd, and `ssh.socket`, keep what they read until the caller's reload (`p2-reload`), and `Apply` changes no unit. A done context is refused before anything runs.

### `permitRootLogin` and saving (`pkg/config`)

19. **The key takes `"no"`, `"prohibit-password"` or `"yes"`** (`config.RootLogin`), so a Proxmox VE host can say key-only (brief 2.5). The old bool still works: `true`/`false` and YAML 1.1's spellings the bool field took (`on`, `Off`, `Yes`) read as `yes`/`no` and record a `config.Deprecation` (key, line, what was written, what to write instead); a bare `no`/`yes` is the string form, not deprecated. `Config.Deprecations()` returns them for the caller to show as warnings: printing is the caller's (`p3-output` or the slices'), not `LoadConfig`'s, because the legacy `SaveConfig` still writes the bool and would bring the warning back after the user fixed it. Any other value is the error the bool field gave, with the same text; every other key decodes and fails as before (tested).
20. **Legacy code is unchanged.** `PermitRootLogin`, the bool legacy code reads, is `true` exactly for `yes`, so legacy code writes `PermitRootLogin no` for `prohibit-password`, which is stricter, not looser. The legacy `SaveConfig` encodes as before, the bool included, with one exception: a `prohibit-password` value is written as that string, since the bool would turn it into `no` (tested: the encoding is byte for byte yaml.v3's over the fields otherwise).
21. **The canonical value is written only by `config.Save`** (§3.19): to the path given, which is to be the loaded one, with `host.WriteFileAtomicKeep` (mode and owner kept; a new file `0644`). It writes `permitRootLogin` as its string (`"no"` and `"yes"` quoted, so YAML 1.1 readers do not take them for bools), so a deprecated bool becomes the value it meant. Comments in the file are lost; the caller says so before saving. Nothing calls `Save` yet: replacing the 17 legacy `SaveConfig` call sites is the slices' work ([atomic.md](atomic.md) §7). The example configuration (`hardn.yml.example`, the embedded copy, `docs/configuration.md`) now shows `permitRootLogin: "no"` and the three values.

### Import rules

22. The package is under `pkg/host/sshd`, so depguard's `host-stdlib` (standard library and `pkg/host/...` only) and `sshd-through-host` (no `os`, `io/ioutil`, `golang.org/x/sys`, `syscall`, `net` except `net/netip`, `plugin`) already cover its non-test files; no new rule was needed. Everything reaches the host through `host.System`. `pkg/host/sshd` gains a small exported read-order API for it (§5), in a new file.

## 3. Platform facts and sources relied on

| Fact | Source | Used for |
|---|---|---|
| The legacy artifacts: `manage.conf` (v0.3.1, v0.3.2, main before 3cc6a2f), a custom `sshConfigFile`, `hardn.conf` (main from 08a805a/3cc6a2f), `listen.conf` (v0.3.x), a whole `sshd_config` on Alpine (all three refs); a host can have `manage.conf` and `hardn.conf` | [side-effects.md](../refactor/phase0/side-effects.md) §10 **[V]** (`git grep` over every ref) | §2.2 |
| v0.3.x's drop-in, Alpine `sshd_config` and `listen.conf` templates, verbatim; `ListenAddress <addr>:<port>`, the dual `AuthorizedKeysFile`, `AllowUsers` even when empty | [v032-reference.md](../refactor/phase0/v032-reference.md) §4.2 **[V]**; `git show v0.3.1:pkg/ssh/ssh.go` and `v0.3.2:…` (same templates), read 2026-10-17 | §2.2, fixtures |
| origin/main's `SaveSSHConfig`: `# SSH configuration managed by Hardn` first, `Port`, `ListenAddress` without a port, one `AuthorizedKeysFile` line per key path; `hardn.conf`, or `sshd_config` on Alpine; disable-root rebuilds from `Port 22` | `git show origin/main:pkg/adapter/secondary/file_ssh_repository.go`, read 2026-10-17; side-effects.md V4, V5 | §2.2, §2.6, fixtures |
| `sshConfigFile` was honoured by v0.3.x and is ignored on origin/main; keys in `~/.ssh_<user>/authorized_keys` on v0.3.x | side-effects.md §6, D7, D8 **[V]** | §2.3, §2.6 |
| sshd's Include is the first directive on every supported stock config (Alpine from 3.19); first value wins except the accumulating keywords; drop-ins in C byte order, so `00-hardn.conf` is read before `50-cloud-init.conf` | [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9) **[V]**; [sshd-validate.md](sshd-validate.md) §3 | §2.5 |
| `ListenAddress` forms: `address`, `address:port`, `[address]:port`, each with an optional `rdomain <domain>`; without a port, on every `Port`; without any, every local address; `Port` defaults to 22; `AddressFamily` `any`, `inet`, `inet6`; `AuthorizedKeysFile` defaults to `.ssh/authorized_keys .ssh/authorized_keys2` | [sshd_config(5)](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_3_P1/sshd_config.5), V_10_3_P1, read 2026-10-17 | §2.6, §2.8, §2.9 (`listenersOf`) |
| sshd binds `::` IPv6 only, so `0.0.0.0` and `[::]` on one port coexist; a systemd `[::]` socket is dual-stack unless `BindIPv6Only=ipv6-only`, and `ListenStream=<port>` alone listens via IPv6 | [reload.md](reload.md) §3 (systemd.socket(5), v257) and §2.5 | §2.8, `Hold` |
| `sshd-socket-generator` (Ubuntu 24.04, 26.04) derives `ssh.socket`'s listeners from `sshd_config` and its drop-ins with sshd's parser, at boot and at `daemon-reload`, into `addresses.conf`; `listen.conf` applies after it (drop-ins apply in lexicographic order across directories) and its empty `ListenStream=` resets the generator's list | research `research-ssh-distro-facts.md` §4a and "Extra" **[V]**; [reload.md](reload.md) §3 | §2.7, §2.8 |
| v0.3.x never ran `daemon-reload` after writing `listen.conf`, so it took effect only at a later reload or reboot | side-effects.md D9 **[I]** | §2.8 |
| `systemctl show -p Listen ssh.socket` prints the listeners systemd loaded, which are the ones bound unless a bind failed | **[I]** ([sshd-effective.md](sshd-effective.md) §2.5 treats it as configuration) | §2.8 |
| The generator gives a socket that binds `0.0.0.0:<port>` and `[::]:<port>` together, the set `Port <port>` alone configures | **[I]**: that is the stock instruction for a port change (reload.md §3); the generator's handling of the pair (for example `BindIPv6Only=ipv6-only`) was not read: launchpad's robots.txt refuses automated fetching | §2.8 |
| The generator passes duplicate addresses through; Linux refuses a second listening socket on one port and family at the same address or beside the unspecified one; on a socket host that fails `ssh.socket`'s start (reload.md §2.8a) | **[unverified]**; not relied on: dropping a covered line accepts the same connections, and refusing an overlap is fail-closed | §2.9, §2.15 |
| Alpine's stock `sshd_config`: the Include first (include-config-dir.patch, from 3.19); `AllowTcpForwarding no`, `GatewayPorts no`, `X11Forwarding no` (disable-forwarding-by-default.patch); `Subsystem sftp internal-sftp` | research `research-ssh-distro-facts.md` §2b **[V]** (aports 3.24-stable); not re-read here (gitlab's robots.txt refuses automated fetching of raw files) | §2.12 |
| Upstream's `sshd_config` leaves `AuthorizedKeysFile .ssh/authorized_keys` and a `Subsystem sftp` line uncommented, and comments out every other directive | [OpenSSH `sshd_config`](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_3_P1/sshd_config), V_10_3_P1, read 2026-10-17; that Alpine keeps the first line uncommented is **[I]** | §2.12 |
| A second `Subsystem` of one name is refused (servconf.c's "already defined") | **[I]**, not re-read here; leaving the stock line out is safe either way, since `00-hardn.conf`'s is read first | §2.12 |
| yaml.v3 decodes YAML 1.1's `yes`/`no`/`on`/`off` into a bool field and tags bare `yes`/`no` as strings | go.yaml.in/yaml/v3 behaviour, pinned by `TestPermitRootLogin` | §2.19 |

## 4. Written, not run (gate 5)

Nothing here has run against a real sshd or on Linux. On the v0.3.2-hardened image (`next/p2-vmtest`, R8), the Debian 13, Ubuntu 26.04 and Alpine 3.24 VMs, and by hand on Proxmox VE 9:

- the v0.3.2-hardened Ubuntu image: `Find` naming `manage.conf` and `listen.conf`; the plan with the real `systemctl show -p Listen ssh.socket`, both after a reboot (port 2230) and right after v0.3.2's write without one (port 22, D9); `Apply`, then `p2-reload`'s `daemon-reload` and restart of `ssh.socket`; key login on 2230 with the key in `~alice/.ssh_alice/authorized_keys`, and on 22 when it was held; the generator's `addresses.conf` for `ListenAddress 0.0.0.0:2230` plus `[::]:2230` (§3, [I]) and whether the socket binds both;
- the same image upgraded to an origin/main build (`manage.conf` and `hardn.conf`): the dropped duplicate `ListenAddress`, the merged `AuthorizedKeysFile`, and that `50-cloud-init.conf`'s `PasswordAuthentication` still wins as before;
- Debian 13 with v0.3.2's files (`ssh.service`): the inert `listen.conf` removed, the reload of `ssh.service`, logins as before;
- Alpine 3.24 with v0.3.2's and origin/main's generated `sshd_config`: the restore step, `sshd -t` with `sshd.pam` where `UsePAM yes`, sftp working afterwards, and the stock directives against Alpine's real file;
- every `sshd -T` value `Apply` compares (§2.14), printed by the real binaries: the model in the tests prints a subset, so a keyword real sshd prints in another form is compared as written there; that `sshd -T` prints a duplicate `listenaddress` line for a duplicate listener;
- that sshd and the generator accept the `ListenAddress [::]:<port>` lines the plan adds on a host with IPv6 disabled (not modelled; the reload's `ss` check would fail it);
- Proxmox VE 9: a v0.3.2 `manage.conf` with `PermitRootLogin no` migrated as it is, then `p2-pve`'s `Desired` `PermitRootLogin prohibit-password` accepted (a widening);
- `config.Save` writing `/etc/hardn/hardn.yml` as root, mode and owner kept.

## 5. API

```go
package migrate

const ManageConf = sshd.DropInDir + "/manage.conf"
const HardnConf = sshd.DropInDir + "/hardn.conf"
const ListenConf = "/etc/systemd/system/ssh.socket.d/listen.conf"

type Kind uint8       // KindManageConf, KindCustom, KindHardnConf, KindListenConf, KindGeneratedMain; String()
type Generation uint8 // GenV03, GenMain; String()
type Artifact struct { Kind Kind; Path string; Gen Generation; Data []byte; Ports []uint16 }
type Left struct { Path, Why string }
type Found struct { Main []byte; Include sshd.Include; DropIns []sshd.File; Artifacts []Artifact; Left []Left }
type FindRequest struct { ConfigFile string } // the loaded hardn.yml's sshConfigFile
func Find(ctx context.Context, sys host.System, req FindRequest) (Found, error)

var ErrIncludeOther, ErrNotEstablished error
type Directive struct { Keyword string; Args []string }
type Input struct { Found Found; Desired []Directive; SSH detect.SSH; Platform detect.Platform; Before sshd.Effective }
type OpKind uint8 // OpWriteDropIn, OpRemove, OpRestoreMain, OpRemoveSocketDropIn; String()
type Op struct { Kind OpKind; Path string; Data, DropIn []byte; Artifact Kind; Gen Generation; Note string }
type Carried struct { sshd.Origin; Why string }
type Source struct { Path string; Data []byte; Absent bool }
type Plan struct {
	Ops []Op; DropIn []byte; Carried, Dropped []Carried; Left []Left
	Keywords []string; Hold []sshd.Listener; Before sshd.Effective; Sources []Source
}
func (p Plan) Empty() bool
func PlanFor(in Input) (Plan, error) // pure

var ErrFewerLoginPaths, ErrListenerOverlap, ErrChanged error
type Narrowing struct { Keyword, Before, After string }
type NarrowError struct { State string; Narrowings []Narrowing } // Is(ErrFewerLoginPaths)
type Options struct { RunDir string; Platform detect.Platform; Binary string; Backup *backup.Run }
type Report struct { Done []Op; Backups []backup.Result; After sshd.Effective; Findings []sshd.Finding; Kept []sshd.Listener }
func Apply(ctx context.Context, sys host.System, p Plan, opts Options) (Report, error)

package sshd // added for this unit, in order.go

type Line struct { Origin; Keyword string; Args []string; Match *Block }
func ReadOrder(main []byte, drops []File) []Line // the directives in sshd's read order, by Evaluate's rules
func Accumulates(keyword string) bool
func CanonicalKeyword(keyword string, args []string) string

package config

type RootLogin string // RootLoginNo "no", RootLoginProhibitPassword "prohibit-password", RootLoginYes "yes"; Valid()
type Deprecation struct { Key string; Line int; Found, Use string } // String()
func (c *Config) UnmarshalYAML(n *yaml.Node) error
func (c Config) MarshalYAML() (any, error)
func (c *Config) RootLogin() RootLogin
func (c *Config) SetRootLogin(r RootLogin) error
func (c *Config) Deprecations() []Deprecation
func Save(ctx context.Context, sys host.System, c *Config, path string) error
```

## 6. Not in this unit; inputs to later units

- **`p3-engine`:** the order, once per run before any other SSH change: `detect.Detect`; `sshd.ReadEffective` (with detect's SSH) as `Input.Before`; `Find` with the loaded `sshConfigFile`; `PlanFor`; show the plan, its notes and `Dropped`; then `Apply` with a run directory that is root's (`/run/hardn/<run-id>`, [sshd-validate.md](sshd-validate.md) §6), `reload.OpenRCFlavour`'s binary on Alpine, and the run's backup store; then `p2-reload` with `Plan.Keywords`. For the reload's `Change.Before`, `Report.Kept` is every listener the migration kept configured (sshd's, `listen.conf`'s and the socket's), all covered by `Report.After`; `sshd.ReadEffective` on a `listen.conf` host reports its listeners not `Known` (the socket and sshd disagree), so whether `Kept` may stand in as a `Known` set is the engine's call. `Change.After` is `Report.After.Listeners.SSHD`. Any error after a step ran means restoring the run's backups and the reload with `Rollback`, under `context.WithoutCancel`. `Desired` is the engine's own settings for `00-hardn.conf`; a narrowing among them comes after the migration, as its own confirmed change.
- **`p2-revert`:** on a socket host the `listen.conf` migration is a listener change, so it is a lockout-risk change under confirm-or-revert: journal the backup run's entries (`Report.Backups`' `Seq`) and the reload; confirm from a connection to a port in `Report.Kept`.
- **`p2-pve`:** pass `PermitRootLogin prohibit-password` in `Desired`; a migrated `PermitRootLogin no` from a v0.3.2 `manage.conf` is then replaced, which `Apply` accepts as a widening.
- **`p3-output` and the slices:** show `Config.Deprecations()` as warnings, with `Deprecation.String()`'s text or a registry code; switch the legacy `SaveConfig` callers to `config.Save`; map `ErrFewerLoginPaths`, `ErrListenerOverlap`, `ErrChanged`, `ErrIncludeOther` and `ErrNotEstablished` to registry codes; print each `Op.Note` with the backup directory.
- **Open for the maintainer:** (a) a v0.3.x `sshConfigFile` of `sshd_config` itself (its drop-in written over the main file) is reported, not migrated: restoring the distribution's file there is a design choice per distribution; (b) a `manage.conf` or `hardn.conf` edited by hand above hardn's first lines is not recognised and stays (a backup-and-remove rule for "hardn's file, edited" would need its own approval); (c) on a socket host the migration keeps port 22 configured where the files configured it before (`Port 22` beside `listen.conf`'s port), and `[::]` beside `0.0.0.0` on `listen.conf`'s port; closing either is a later confirmed change, which the user may not expect from a migration.
- **Not here:** writing `00-hardn.conf` for anything but the migration; the reload; the confirm window; key files themselves (moving keys out of `~/.ssh_<user>` is not needed while `AuthorizedKeysFile` names it); legacy `disableRoot` (§3.11, an alias of `disableRootSSH`).
- **Size:** about 1,800 lines of non-test Go (much of it doc comments) and 1,400 of tests, against the plan's guideline of about 500: five artifact kinds over three init modes, the listener model the socket hold and the duplicate check need, the login-path comparison, the config key and its save path, and a test per host shape.
