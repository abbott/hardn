# sshd effective values (`pkg/host/sshd`)

> **Unit:** refactor plan Phase 2, `next/p2-sshd-effective` (brief 2.2, "after installing it, run `sshd -T` and confirm the effective values"; brief §3 principle 3; brief §2 decision "hardn's SSH settings live in `00-hardn.conf`"). V-numbers: V13 (another drop-in decides the value; status read from files, not `sshd -T`), the post-install half of V7 (`sshd -T` after activation), and the listener set that V9's and V12's fixes need (`p2-ufw`, `p2-reload`) ([side-effects.md §8](../refactor/phase0/side-effects.md)). Brief corrections it implements: [phase0-findings.md §12](../refactor/phase0-findings.md#12-proposed-adjustments-to-the-brief), rows 18 and 19 (C byte order; accumulating keywords compared as sets).
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only, with `sshd -T` and `systemctl show` answers written in the format of their sources (§3), not captured from a host. Nothing has run on Linux; §4 lists what is "written, not run (gate 5)".

`sshd.ReadEffective(ctx, sys, req)` answers, once a configuration is installed: what does sshd run with, where does it listen, and which other files' lines beat, add to or lose to hardn's? It runs `sshd -T` (and one `sshd -T -C` per `Match` block) against the installed files, reads `ssh.socket`'s listeners on a socket-activated host, reads the files, and returns an `Effective` and a `[]Finding`. `sshd.Evaluate(in)` is the same computation over given inputs, with no I/O (for example `Validate`'s `Report.Effective` and `Report.Probes` with the candidate's files).

## 1. Why

hardn's status was read from files (V13), and the drop-in it wrote, `hardn.conf`, sorts after `50-cloud-init.conf`, so cloud-init's `PasswordAuthentication yes` won and nothing said so ([side-effects.md](../refactor/phase0/side-effects.md), V13). sshd uses the first value it reads for most keywords, but keeps every value of Port, ListenAddress, AllowUsers and a few more, so a stray `Port 22` in any file keeps that port open after hardn moves to 2230, and a stray `AllowUsers bob` widens access ([phase0-findings.md §12](../refactor/phase0-findings.md#12-proposed-adjustments-to-the-brief), row 19). On Ubuntu 24.04 and 26.04 systemd holds the listening socket, so sshd's own Port says nothing about what accepts connections until `daemon-reload` and a restart of `ssh.socket` (V12). A firewall that allows the wrong set locks the user out (brief §3 principle 1).

## 2. Guarantees

### Effective values (`Effective`)

1. **sshd's own answer.** The values come from `sshd -T`'s output, one `keyword value` per line, never from grepping a file (brief §3 principle 3). Keywords are lower-cased (OpenSSH 10.4 and 10.5 print mixed case) and an alias reads as its keyword (sshd prints `kbdinteractiveauthentication`, but `challengeresponseauthentication` would read the same); every line is kept in `Effective.Keywords`.
2. **The lockout-relevant settings are typed and normalised.** `PermitRootLogin` (`prohibit-password`, `forced-commands-only`, `yes`, `no`), `PasswordAuthentication`, `KbdInteractiveAuthentication` and `PubkeyAuthentication` (`yes`, `no`) are `Setting{Value, Known}`, compared case-insensitively, with `without-password`, which OpenSSH before 10.1 prints, read as `prohibit-password` (one value in sshd's table). A value outside that list (sshd prints `UNKNOWN` or `unset` for some), a keyword printed twice with two values, or a missing keyword is Unknown (`Known` false, `Value` ""), with a `FindingUnknown`.
3. **Accumulating keywords are sets.** Port, AllowUsers, AllowGroups, DenyUsers, DenyGroups and AcceptEnv are `Set{Values, Known}`: every value sshd printed, in its order, without repeats; no line is the empty set. Port must be one or more numbers from 1 to 65535, or it is Unknown.
4. **Listeners are where sshd listens, not Port.** `Listeners.SSHD` comes from sshd's `listenaddress` lines (`0.0.0.0:22`, `[::]:22`, optionally ` rdomain <name>`), since a `ListenAddress` with a port of its own listens on a port Port does not list. On a host whose SSH mode is `SSHService` or `SSHOpenRC` (`detect.SSH.Mode`) they are the listeners, `Known`.
5. **On a socket-activated host the socket's listeners are added.** With `SSHSocket`, `ReadEffective` runs `systemctl show -p Listen ssh.socket` and reads each `Listen=<address> (Stream)` line into `Listeners.Socket`. `Listeners.All` is the union of both sources, each listener once (0.0.0.0 and :: kept apart): what a firewall must allow, kept even when Unknown, because allowing more is the safe direction there. `Known` needs the two sources to agree, comparing every unspecified address as one (systemd's dual-stack `[::]:22` and sshd's `0.0.0.0:22` plus `[::]:22` accept the same connections). Disagreement is `FindingDisagree`, the case of a Port change written but not yet applied by `daemon-reload` and a restart of `ssh.socket`; an entry that is not an IP address, a port and `(Stream)`, or no entry at all, is `FindingUnknown`. With the SSH mode Unknown the listeners are Unknown too (`FindingUnknown`): it is not known whether sshd or the socket listens.
6. **The files are checked against sshd.** For the typed settings and the sets, `Evaluate` predicts from the files what `sshd -T` should print: the first value outside any `Match` block, or every such value for a set. Where the files say something and sshd prints something else (a file changed between the two reads, or a rule this package does not model), the value is Unknown with a `FindingDisagree` naming both, never one of the two. A keyword with any line under a `Match` block, or a Port given as a service name, is not predicted, so no disagreement is claimed for it.
7. **Match probes.** `ReadEffective` probes each `Match` block as `Validate` does (`sshd -T -C`, the connection built by the same mirror of sshd's matching, [sshd-validate.md](sshd-validate.md) §2.6; an untargeted block is a `FindingMatchUntargeted`). Where a probe prints a typed setting or a user, group or environment set differently from `sshd -T`, it is an `Override{Block, Conn, Keyword, Value}`: a connection like `Conn` gets `Value`. A probe whose block may not apply to its connection shows nothing certain about the block ([sshd-validate.md](sshd-validate.md) §6).

### Conflict report (`Effective.Conflicts`)

8. **The read order is sshd's.** The configuration is flattened as sshd reads it: `sshd_config`'s lines, with every drop-in's at the `Include` line, in C byte order. A drop-in starts under the `Match` block its Include is under (none for the stock first-line Include) and its own `Match` blocks end with it; `sshd_config`'s run to its end. Only `IncludeLine` is followed (`ReadEffective`, like `Validate`, refuses any other Include). Aliases read as their keyword (`ChallengeResponseAuthentication` and `SKeyAuthentication` as `kbdinteractiveauthentication`, `DSAAuthentication` as `pubkeyauthentication`, `HostDSAKey` as `hostkey`, and three more), and each `Subsystem` name is a keyword of its own (sshd keeps the first definition of each name).
9. **hardn's lines** are `Owner`: by default all of `00-hardn.conf`; in `IncludeNone` mode the caller names its block's lines in `sshd_config` (`Owner{File: MainConfig, First, Last}`). For each keyword hardn's lines set outside a `Match` block, every other line that sets it is reported, in read order, as a `Conflict{Kind, Keyword, Hardn, Other, Match}` and a `Finding` at the other line:

   | The other line | Kind | Meaning |
   |---|---|---|
   | outside a Match block, accumulating keyword | `accumulates` | it adds values: `Port 22` elsewhere keeps 22 open |
   | outside a Match block, read before hardn's | `overridden` | sshd uses it, not hardn's (only the first such line, which wins; reported even when its value is hardn's, since hardn does not control it) |
   | outside a Match block, read after hardn's, another value | `shadowed` | no effect while hardn's line is there (cloud-init's `50-cloud-init.conf`, Proxmox VE's `PermitRootLogin yes` in `sshd_config`): reported, never edited (brief 2.2) |
   | under a Match block | `match-override` | for the connections the block matches, sshd uses it in place of hardn's |

   A line of hardn's own under a `Match` block is a `match-override` finding too: it applies only to the connections that block matches. A later line with hardn's value is not reported.
10. **The byte-order rule, exactly.** Among the names the Include reads (ending in `.conf`, not starting with `.`), a drop-in beats `00-hardn.conf` for a first-value keyword exactly when its name is less than `00-hardn.conf` byte by byte (strcmp). So `0-*`, `00-a…` to `00-g…`, and also `00-h.conf`, `00-hardn-x.conf` (`.` and `-` sort below letters) and `+plus.conf` (`+` is below `0`) beat it; `000-*`, `00.conf`, `00_x.conf`, `00-hardn0.conf`, `A-upper.conf` and the legacy `hardn.conf` do not. Tested on a fixture directory with each of these names, plus `.hidden.conf` and `00-hardn.conf.bak`, which are never read (`testdata/effective/byteorder`); the read order matches the order the audit measured with a real sshd (§3).
11. **Read-only, through `host.System`.** `ReadEffective` makes reads (`Stat`, `ReadFile`, `ReadDir`) and runs exactly `sshd -T -f /etc/ssh/sshd_config`, one `sshd -T -f /etc/ssh/sshd_config -C <spec>` per `Match` block (`EffectiveRequest.Binary` names another sshd, such as Alpine's `sshd.pam`) and, with `SSHSocket` only, `systemctl show -p Listen ssh.socket`. Tested: the exact runs of every fixture, and no call that changes the fake. The package's depguard rule `sshd-through-host` ([sshd-validate.md](sshd-validate.md) §2.11) covers the new files.
12. **Errors are for I/O; Unknown is for content.** `ReadEffective` returns an error, and no values, when a command cannot give an answer: `sshd -T`'s errors are classified as `Validate`'s are (`*EnvError` for a missing `/run/sshd`, `*ConfigError`, `*UnexpectedError`, the host's own), `systemctl show` failing is its `*host.ExitError`, and the files are read as `Validate` reads them (`ErrUnsupportedInclude`, `ErrNotRegular`, `fs.ErrInvalid` for a NUL byte), so a host `Validate` refused is refused here too. Output it cannot read makes values Unknown with findings; `Evaluate` never returns an error. A done context is refused before anything runs.
13. **Finding codes are stable.** `FindingKind.String()` is the code: `clobberable`, `include-not-first`, `match-untargeted`, `sshd-warning` (from `Validate`), and `overridden`, `shadowed`, `accumulates`, `match-override`, `sources-disagree`, `effective-unknown`. `TestFindingCodes` pins all ten. `ReadEffective`'s findings are `Validate`'s about the installed files (`clobberable`, `include-not-first`, `match-untargeted`), then `Evaluate`'s.

## 3. Platform facts relied on

| Fact | Source | Used for |
|---|---|---|
| For most keywords the first value sshd obtains wins; Port, ListenAddress, HostKey, AllowUsers, DenyUsers, AllowGroups, DenyGroups and AcceptEnv accumulate across the main file and every drop-in | [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "sshd `Include` per distro" **[V]** (sshd_config(5); servconf.c "XXX appends to list; doesn't respect first-match-wins"; local sshd experiments); §12 row 19 | §2.3, §2.9 |
| HostCertificate accumulates too; Subsystem keeps the first definition of each name | OpenSSH [V_10_3_P1 `servconf.c`](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_3_P1/servconf.c) `process_server_config_line_depth` (`sHostCertificate` → `servconf_add_hostcert`; `sSubsystem`: "Subsystem '%s' already defined"), read 2026-10-17 | §2.8, §2.9 |
| Include globs are processed in C byte order (`strcmp`; sshd never calls `setlocale`); measured order `0-x 00-a 00-hardn 000-early 00_x 01-x 10-x 50-cloud-init 60-cloudimg-settings A-upper Z hardn`; a leading `.` is never matched | the audit's `research-ssh-distro-facts.md` §3b **[V]** (sshd_config(5) lines 932-943, `openbsd-compat/glob.c`, local sshd); [phase0-findings.md §12](../refactor/phase0-findings.md#12-proposed-adjustments-to-the-brief) row 18; [sshd-validate.md](sshd-validate.md) §3 (glob.c lines 752-754) | §2.8, §2.10 |
| `sshd -T` prints `port %d` per port, `listenaddress %s:%s` or `[%s]:%s` with an optional ` rdomain %s` per listening address, one line per value of AllowUsers, DenyUsers, AllowGroups, DenyGroups and AcceptEnv, the multistate values by name (`unset` for -1, `UNKNOWN` otherwise), and each keyword by the first name the keywords table gives its opcode | OpenSSH V_10_3_P1 `servconf.c` `dump_config`, `format_listen_addrs`, `dump_cfg_strarray`, `fmt_intarg`, `lookup_opcode_name`, read 2026-10-17 | §2.1-2.4 |
| `multistate_permitrootlogin` lists `without-password` first in 9.2 and `prohibit-password` first in 10.3, so `sshd -T` printed `without-password` before 10.1; both mean `PERMIT_NO_PASSWD`; multistate values are read with `strcasecmp` | `servconf.c` V_9_2_P1 and V_10_3_P1 (`multistate_permitrootlogin`, `parse_multistate`), read 2026-10-17; `research-ssh-distro-facts.md` §3d **[V]** (10.1, bz#3788) | §2.2 |
| OpenSSH 10.4 and 10.5 print directive names in mixed case | `research-ssh-distro-facts.md` §3d **[V]** (release notes) | §2.1 |
| The aliases `hostdsakey`, `dsaauthentication`, `challengeresponseauthentication`, `skeyauthentication`, `keepalive`, `pubkeyacceptedkeytypes`, `hostbasedacceptedkeytypes`, the same in 9.2 and 10.3; `kbdinteractiveauthentication` comes before its aliases in both, so it is the name `sshd -T` prints | `servconf.c` V_9_2_P1 and V_10_3_P1, `keywords[]`, read 2026-10-17 | §2.1, §2.8 |
| A satisfied Match block's values replace the global ones for that connection (`copy_set_server_options` copies every option the block set), the first instance among satisfied blocks winning; a Match in an included file ends with that file | `servconf.c` V_10_3_P1 `parse_server_match_config`, `copy_set_server_options`, read 2026-10-17; `research-ssh-distro-facts.md` §3c **[V]**; [sshd-validate.md](sshd-validate.md) §3 ("Don't let included files clobber the containing file's Match state") | §2.7, §2.8, §2.9 |
| `Match All` matches whatever the connection; whether plain `sshd -T` (no `-C`) applies such a block | `servconf.c` V_10_3_P1 `match_cfg_line` (read); that `-T` without `-C` evaluates Match blocks against a blank connection is **[unverified]** (sshd.c not re-read) | §2.6: a keyword under any Match block is not predicted, which holds either way |
| On Ubuntu 24.04/26.04 `ssh.socket` holds the listening sockets and sshd uses the descriptors systemd passes; a reload cannot change Port or ListenAddress; `daemon-reload` reruns `sshd-socket-generator`, which writes one `ListenStream` per address, and `restart ssh.socket` applies it | [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "`ssh.socket` per Ubuntu version" **[V]**; `research-ssh-distro-facts.md` §4a **[V]**, §4b **[I]** | §2.5 |
| Debian and Proxmox VE run `ssh.service` (socket activation opt-in, no generator, its port then in `ssh.socket.d`); Alpine runs OpenRC's `sshd` | [detect.md](detect.md) §3; `research-ssh-distro-facts.md` §4c, §5 **[V]** | §2.4, §2.5 |
| `systemctl show -p Listen` prints one `Listen=<address> (<type>)` line per entry of the socket's `Listen` property | systemd v257 [`src/systemctl/systemctl-show.c`](https://raw.githubusercontent.com/systemd/systemd/v257/src/systemctl/systemctl-show.c) (`print_property`, `"Listen"`: `"%s (%s)"`), [`src/core/dbus-socket.c`](https://raw.githubusercontent.com/systemd/systemd/v257/src/core/dbus-socket.c) (`property_get_listen`: type, address), read 2026-10-17 | §2.5 |
| The address text is `[::]:22` / `0.0.0.0:22` and a stream socket's type is `Stream`; `ListenStream=22` binds a dual-stack `[::]` | **[unverified]**: `socket_address_print`, `socket_port_type_to_string` and systemd.socket(5) were not read; an entry in another form makes the listeners Unknown, never Known | §2.5 |
| cloud-init writes `50-cloud-init.conf` (`PasswordAuthentication yes` on an Ubuntu ISO install without imported keys); Ubuntu cloud images ship `60-cloudimg-settings.conf`; Proxmox VE writes `PermitRootLogin yes` into `sshd_config`, which the drop-in overrides (not drift); the legacy `hardn.conf` sorts after both cloud files | `research-ssh-distro-facts.md` §3b, §6 and "Extra: Proxmox cluster tooling" **[V]**; [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "Proxmox join / detection"; plan `p2-pve` row | §2.9 examples, fixtures |
| `sshd -T` loads the host keys and, as root on Debian, Ubuntu and Proxmox VE, needs `/run/sshd` | [sshd-validate.md](sshd-validate.md) §3 | §2.12 |
| OpenSSH 8.9 (Ubuntu 22.04, best effort) prints the same names and values as 9.2 | **[unverified]**; an alias would still read (§2.1), another value would be Unknown | §2.1, §2.2 |

## 4. Written, not run (gate 5)

No `sshd` and no `systemctl` has run. The fixtures' outputs follow the sources above; whether the real binaries print exactly that is the first thing to check. On the Debian 13, Ubuntu 26.04 and Alpine 3.24 VMs (`next/p2-vmtest`), by hand on Proxmox VE 9, and where possible on Debian 12 (9.2p1) and Ubuntu 24.04 (9.6p1), as root:

- `ReadEffective` on the stock configuration: no finding except the expected `shadowed` ones (none on Debian; `50-cloud-init.conf` on an Ubuntu ISO install), every value Known; `without-password` from 9.2 and 9.6, `prohibit-password` from 10.x;
- the brief's 2230 change: during the confirm window (`Port 22` and `Port 2230` in `00-hardn.conf`) on Ubuntu 26.04, `sources-disagree` before `daemon-reload` and `restart ssh.socket` and Known after, with `Listeners.All` holding both ports throughout; on Debian 13 and Alpine, Known from sshd alone after the reload;
- `systemctl show -p Listen ssh.socket`'s text on Ubuntu 24.04 and 26.04 stock, after the generator wrote `addresses.conf`, with the generator masked, and on a Debian host opted into the socket with `ssh.socket.d/listen.conf` (a disagreement with sshd's Port is expected there);
- the conflict report and the cross-check against real files: the byte-order fixture's names in a real `sshd_config.d` (no `sources-disagree` expected), Proxmox VE after `pvecm create` (`shadowed` `PermitRootLogin yes`), the v0.3.2-hardened image (`manage.conf`, R8) and origin/main's `hardn.conf`, a `Port 22` left in `sshd_config`, a `Match User` block in a drop-in, and an Alpine host with no Include and hardn's block (`Owner` with its lines);
- the probes' `Override`s against the installed files, and `sshd -T` failing on Ubuntu 26.04 before the first connection (`/run/sshd` missing, `*EnvError`).

## 5. API

```go
package sshd

const SocketUnit = "ssh.socket"

type Setting struct { Value string; Known bool }      // Value lower case; "" when Unknown
type Set struct { Values []string; Known bool }       // Has(v) bool
type Listener struct { Addr netip.Addr; Port uint16; RDomain string } // String()
type Listeners struct { SSHD, Socket, All []Listener; Known bool }    // Ports() []uint16
type Origin struct { File string; Line int; Text string }
type Conflict struct { Kind FindingKind; Keyword string; Hardn, Other Origin; Match Block }
type Override struct { Block Block; Conn Conn; Keyword, Value string }
type Owner struct { File string; First, Last int }     // zero: all of HardnDropIn
type File struct { Path string; Data []byte }

type Effective struct {
	Keywords map[string][]string
	PermitRootLogin, PasswordAuthentication, KbdInteractiveAuthentication, PubkeyAuthentication Setting
	Port, AllowUsers, AllowGroups, DenyUsers, DenyGroups, AcceptEnv Set
	Listeners Listeners
	Conflicts []Conflict
	Overrides []Override
}

type EffectiveInputs struct {
	Effective []byte; Probes []Probe
	Mode detect.SSHMode; Socket []byte
	Main []byte; DropIns []File; Owner Owner
}
func Evaluate(in EffectiveInputs) (Effective, []Finding) // pure

type EffectiveRequest struct { SSH detect.SSH; Binary string; Conn Conn; Owner Owner }
func ReadEffective(ctx context.Context, sys host.System, req EffectiveRequest) (Effective, []Finding, error)

// FindingKind gains FindingOverridden, FindingShadowed, FindingAccumulates,
// FindingMatchOverride, FindingDisagree, FindingUnknown (after FindingWarning).
```

`Validate`'s probe loop is now a helper both use, unchanged in behaviour (its tests pass as they were).

## 6. Not in this unit; inputs to later units

- **`p2-ufw`:** allow `Listeners.All` (`Ports()`) plus the new port. `Known` false means the listener set is not established; `All` is still what both sources name, and the finding says why.
- **`p2-reload`:** after `daemon-reload` and `restart ssh.socket`, call `ReadEffective` again: `Listeners.Known` with the new port in both sources is the plan's "verify the real listener"; `sources-disagree` before then is expected. Pre-existing `ssh.socket.d/*.conf` files are that unit's to report.
- **`p3-engine`:** after installing and reloading, compare the values it wanted with `Effective`; an Unknown is not a pass. Whether an `overridden` conflict whose value equals hardn's should stop a change is policy for the maintainer to decide; this unit reports it.
- **`p2-pve`:** a `shadowed` `PermitRootLogin yes` in `sshd_config` is Proxmox VE's own line, not drift.
- **`p2-sshd-migrate`:** the legacy `hardn.conf` and `manage.conf` show as `shadowed` or `accumulates`; `Evaluate` over the candidate's files and `Validate`'s outputs shows the report before anything is removed.
- **`p3-output`:** map the finding codes (§2.13) into `findings[]`, with `Conflict`'s files and lines.
- **Phase 4 (`p4-check-ssh`):** status from `Effective`, the listener set from `Listeners`, per-connection values from `Overrides`.
- **Not modelled:** a `[::]` socket with `BindIPv6Only=ipv6-only` is compared as if dual-stack (§2.5); `Match` blocks are reported, not evaluated beyond the probes.
- **Size:** about 1,200 changed lines of Go (about 780 of them non-test, much of it doc comments) against the plan's guideline of about 500: the unit reads three sources (sshd -T, the socket, the files) and checks them against each other, and its tests cover each source's failure forms.
