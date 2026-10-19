# sshd validation (`pkg/host/sshd`)

> **Unit:** refactor plan Phase 2, `next/p2-sshd-validate` (brief 2.2; brief §2 decision "hardn's SSH settings live in `00-hardn.conf`"). V-numbers: V7, its pre-install half (no `sshd -t` before activation; here `sshd -t` and `sshd -T` run on a copy, before anything is installed; `sshd -T` against the installed configuration is `p2-sshd-effective`'s, and the reload `p2-reload`'s), and the in-context part of V13 (another drop-in decides the value) ([side-effects.md §8](../refactor/phase0/side-effects.md)). Brief corrections it implements: [phase0-findings.md §12](../refactor/phase0-findings.md#12-proposed-adjustments-to-the-brief), rows 20 (`00-hardn` < `000-x`) and 22 (`sshd -t` on a fragment checks only the fragment; `/run/sshd`).
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only, with `sshd`'s answers scripted from the messages in OpenSSH's source (§3). No `sshd` has run; §4 lists what is "written, not run (gate 5)".

`sshd.Validate(ctx, sys, req)` answers one question before anything is renamed into place: will sshd accept the configuration it reads once these files are installed? It builds a copy of that configuration in a run directory, runs `sshd -t`, `sshd -T` and one `sshd -T -C` per `Match` block against the copy, and writes nothing under `/etc`. `sshd.ParseInclude` and `sshd.DetectInclude` tell the caller where hardn's settings go.

## 1. Why

No ref of hardn runs `sshd -t` or `sshd -T` (V7). Running `sshd -t -f <candidate>` on a drop-in, as the brief first said, checks that fragment alone; the configuration sshd will run with is the main file plus every drop-in, in order ([phase0-findings.md §12](../refactor/phase0-findings.md#12-proposed-adjustments-to-the-brief), row 22). A broken configuration that reaches disk is refused at the next reload (the old daemon keeps running) but stops sshd at the next restart or boot: a lockout (brief §3 principle 1).

## 2. Guarantees

### Include detection

1. **`ParseInclude(main)`** classifies `sshd_config` by its directives, read as sshd reads them: the loader drops each line's leading spaces, tabs and carriage returns, then trailing whitespace and form feeds go; the keyword is `strdelim`'s first token (up to whitespace or `=`, double quotes joining), or its second when the first is empty, so `=Include …` and `""Include …` are Includes; a line whose keyword starts with `#`, or with an unmatched quote before the keyword, is skipped; keywords compare case-insensitively (§3). `Validate` refuses a file with a NUL byte (`fs.ErrInvalid`): sshd's loader keeps a line only up to the NUL and joins the next line to it, so `Inc` NUL newline `lude …` is an Include to sshd and two unknown words to anything else.
   - `IncludeFirst`: the first directive is **exactly** `Include /etc/ssh/sshd_config.d/*.conf`, byte for byte after the trailing whitespace. The drop-ins are read before any setting of `sshd_config`, so hardn writes `/etc/ssh/sshd_config.d/00-hardn.conf` (`HardnDropIn`).
   - `IncludeNone`: no `Include` at all (an Alpine host upgraded from 3.18 or earlier). hardn's block goes at the top of `sshd_config`, and `Validate` reports it **clobberable** (`FindingClobberable`): cloud-init, when `ssh_pwauth` is set, rewrites `sshd_config` itself if it finds no Include line, and Proxmox VE's `pvecm create` and `add` rewrite its `PermitRootLogin` (§3).
   - `IncludeOther`: an `Include` exists, but the first directive is not exactly that line: another spelling (`include`, a tab, `Include=`, `=Include`, quotes, a trailing comment, indentation), another path, or a later position (after `=Port 22`, say). sshd may read some of these the same; hardn does not assume it, and has no write strategy for this mode (§6). `Validate` reports it (`FindingIncludeNotFirst`) and still validates when every Include is the exact line.

   `DetectInclude(ctx, sys)` is `ParseInclude` of `/etc/ssh/sshd_config`.

### Validation in context (`Validate`)

2. **The candidate is an overlay.** `Request.Changes` replaces `sshd_config`, or writes or removes drop-ins in `sshd_config.d` (a removal is how `p2-sshd-migrate` checks the configuration without a legacy `hardn.conf` or `manage.conf`). A drop-in must be a name sshd's `*.conf` matches: it ends in `.conf` and does not start with `.`, because glob's `*` never matches a leading dot (§3). Any other path, a removal of `sshd_config`, a removal that also carries `Data`, or one path twice is `fs.ErrInvalid`. The configuration checked is the host's, read now, with the changes applied.
3. **The copy is what sshd will read.** `Validate` reads `sshd_config` (unless replaced) and every entry of `sshd_config.d` that `*.conf` matches, following symlinks as sshd does, and refuses anything that is not a regular file (`ErrNotRegular`; a FIFO would block the read). It writes, in `RunDir/sshd-validate/` (`0700`), `sshd_config` (`0600`) with its `Include` line rewritten to `Include RunDir/sshd-validate/sshd_config.d/*.conf`, and `sshd_config.d/` holding the candidate drop-ins and byte-for-byte copies of the others, under their own names, so sshd sorts them in the same C byte order ([phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "sshd `Include` per distro"). The atomic write's leftover temporary files (`.<name>.hardn-<random>`, [atomic.md](atomic.md) §2.2) are not copied, because sshd never reads them. In `IncludeNone` mode no drop-in is copied, and writing one is refused (`ErrNotIncluded`): sshd would never read it, so checking it would prove nothing; removing one is allowed. Tested: the copy's files, as the fake holds them when `sshd -t` runs, are exactly these.
4. **What the copy cannot reproduce is refused, not approximated.** Only the exact Include line is rewritten. Any other `Include`, in `sshd_config` or in a drop-in, is `ErrUnsupportedInclude`, and nothing is run: it may read the real `sshd_config.d`, directly or through another file (a relative path is resolved against `/etc/ssh`, §3), and the copy would then check the old drop-ins and pass. A NUL byte in any file of the configuration is `fs.ErrInvalid` for the same reason (§2.1). This fails closed on any host whose configuration has Includes of its own. Tested: `=Include` after a `Match` and an Include joined at a NUL are both refused before anything is written.
5. **The checks, in order, each stopping the validation when it fails:**
   1. `sshd -t -f <copy>`: parse and load the host keys;
   2. `sshd -T -f <copy>`: the same, then print the effective configuration (`Report.Effective`);
   3. for each `Match` line of `sshd_config`, then of each drop-in in sshd's order: `sshd -T -f <copy> -C user=…,host=…,addr=…[,laddr=…][,lport=…][,rdomain=…]` (`Report.Probes`).

   `Validate` returns nil only when all of them exit 0. It never runs `sshd -G`: missing on Debian 12 (9.2p1) and Ubuntu 22.04 (8.9p1), and it rejects `-C` (§3). `sshd` is resolved from `host.SearchPath` (`Request.Binary` names another, such as Alpine's `sshd.pam`). Tested: the exact argv of every run.
6. **Match probes.** Each block's connection starts from `Request.Conn` (default `DefaultConn()`: user `hardn-probe`, host `probe.invalid`, address `192.0.2.1`, names and an address nothing real uses) and is changed to satisfy the block's criteria, checked by a mirror of sshd's own matching (`match_pattern`, `match_pattern_list`, `addr_match_list`, §3):
   - every occurrence of an attribute is a pattern list that must accept the value (sshd ANDs `User alice User bob`);
   - the value is the first candidate every list accepts: the starting value, then, from each pattern that is not negated, `*` and `?` filled in (`x` or `y` in names; `1`, `2`, `1.1`, `1.1.1`, `:1` or `198.51.100.1` in addresses, so `10.*` gives `10.1.1.1`), then a CIDR block's first and last address (`192.0.2.128/25,!192.0.2.128/26` gives `192.0.2.255`); a candidate a negated pattern matches is rejected;
   - a list of negations alone is reported, not given the default: sshd never matches it (`Match User !root` is dead configuration);
   - with `UseDNS no` (sshd's default; read from `Report.Effective`), sshd matches `Host` against the client's address, so `Host` takes addresses only, and a name pattern such as `*.example.net` is reported;
   - a line OpenSSH 9.2 and 9.6 pair differently from 10.x is reported: they read `Match` with `strdelim`, which knows double quotes only and splits at `=`, so `Match User 'bob'` means the literal `'bob'` there and `bob` on 10.x;
   - criteria the spec cannot set (`Group`, decided by the host's group database; `Version`, by the installed sshd; `Invalid-User`, which OpenSSH 9.2's `-C` does not accept; a `LocalPort` given as a service name) are reported too.

   Each reason is in `Probe.Reasons`, and a block with any is reported (`FindingMatchUntargeted`); the block is still probed. No reason means the block applies to `Probe.Conn`, by that mirror. So every block is evaluated against a connection, which `sshd -t` alone never does. A `Match` in a drop-in applies to that file only (§3), and is probed as written. Tested: the candidate rules, negations alone, a negated first address, repeated attributes, 9.x quoting and `UseDNS no`, in a table and through `Validate`.
7. **Classification** of a failed run, with the copy's paths replaced by the real ones in every error message, `Problems` and `Tail`, so errors name `/etc/ssh/sshd_config.d/00-hardn.conf`, not the copy. `Argv`, in every error type, is the argv that ran, so it names the copy.

   | sshd's result | Error |
   |---|---|
   | a line of the output is exactly `Missing privilege separation directory: <dir>` or `<dir> must be owned by root and not group or world-writable.` (both `fatal`, so exit 255) | `*EnvError{Kind: EnvPrivsepDir, Path}`, `ErrEnvironment` |
   | a line is exactly `Privilege separation user <user> does not exist` (`fatal`) | `*EnvError{Kind: EnvPrivsepUser, User}`, `ErrEnvironment` |
   | exit 1 and `no hostkeys available` | `*EnvError{Kind: EnvHostKeys, HostKeysSet}`, `ErrEnvironment` |
   | any other exit 255 | `*ConfigError`, `ErrConfig`: `Problems` lists each `<file> line <n>: <message>` (and `<file>: line <n>: Bad configuration option: …`) with the real file; `Tail` the last lines |
   | any other exit, or a signal (`ExitCode` -1) | `*UnexpectedError{Argv, ExitCode, Tail}`, `ErrUnexpected`, wrapping the `*host.ExitError` |
   | not run | the host's error, its text mapped the same way (`host.ErrNotFound` when sshd is not installed, `ctx.Err()`) |

   The environment is checked before exit 255, because sshd's `fatal()` exits 255 for these too (§3). Its messages are matched as whole lines, ignoring the `\r` log.c ends them with, and never on a line that names a file and a line, so a configuration message quoting a value such as `/x must be owned by root …` stays a `ConfigError`. `HostKeysSet` says the configuration has `HostKey` lines: sshd adds its default key files only when there are none, so then the cause of "no hostkeys" may be the configuration's own lines (a missing file) and not the host; the plan's row counts it an environment error, and `p3-output` can tell the two apart. An environment error says nothing certain about the configuration; it is not a pass, and the caller must not install. On success, any `<file> line <n>:` line in `sshd -t`'s output (a deprecated or unsupported option) is reported as `FindingWarning`.
8. **`/run/sshd` is checked, never created.** On Debian, Ubuntu and Proxmox VE (and a derivative whose `ID_LIKE` is either), `Validate` first checks `/run/sshd` as sshd does: a directory, owned by root, writable by neither group nor others. Otherwise it returns `*EnvError{Kind: EnvPrivsepDir, Path: "/run/sshd"}` with `Argv` nil, before writing anything. Only `ssh.service` creates the directory (`RuntimeDirectory=sshd`), and it is gone while the service is stopped (§3); a socket-activated Ubuntu before its first connection is probably in that state too (**[I]**). Creating it silently would hide that state from the caller. Elsewhere (Alpine, an unknown platform) only sshd's own message, classified above, reports it.
9. **The run directory is root's.** `Request.RunDir` must be absolute, clean, not `/`, free of characters an `Include` argument would need quoted or would glob (whitespace, quotes, `\`, `#`, `*`, `?`, `[`, `]`, `{`, `}`: `fs.ErrInvalid`), and every directory from `/` to it, checked with `Lstat`, must be a real directory (no symlink) owned by root and writable by neither group nor others (`ErrUnsafeRunDir`). hardn runs as root, so only root can change the copy between its writing and sshd's reading; a copy another user could edit would let them make a broken configuration pass. `RunDir/sshd-validate` must not exist (`fs.ErrExist`). The copy is written with `WriteFileAtomic` (guarded, [atomic.md](atomic.md) §2.5). `Validate` is not safe for concurrent use with the same `RunDir`: `host.System` has no exclusive mkdir, so two calls could both find `sshd-validate` absent and share, and remove, one copy. One pending change at a time (the engine's) keeps this from happening; give each run its own `RunDir`.
10. **Nothing else is written, and the copy is removed.** `Validate` creates `RunDir/sshd-validate`, writes the copy into it, and removes it with `RemoveAll` under `context.WithoutCancel`, on success and on failure, so cleanup runs after Ctrl-C too ([system.md](system.md) §2.5). A failed removal is `Report.Cleanup` and does not change the verdict. Tested: every write, mkdir and removal in every test is under the copy, and it is gone afterwards, also when reading the configuration, creating the directory, or writing `sshd_config` or a drop-in fails; a refused request makes reads only.
11. **Only through `host.System`.** The depguard rule `sshd-through-host` denies `os` (and so `os/exec`), `io/ioutil`, `golang.org/x/sys`, `syscall`, `net` and `plugin` to the package's non-test files; `net/netip` (address parsing for probes) is carved out. Checked on a scratch copy: a file importing `os`, `syscall` and `net/http` gets three depguard issues; `net/netip` passes.

## 3. Platform facts relied on

| Fact | Source | Used for |
|---|---|---|
| The stock `sshd_config`'s first directive is `Include /etc/ssh/sshd_config.d/*.conf` on Debian 12/13, Ubuntu 22.04/24.04/26.04, Proxmox VE 9 and Alpine ≥ 3.19 | [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "sshd `Include` per distro" **[V]** (Debian and Ubuntu `sshd_config`, aports `include-config-dir.patch`) | §2.1 |
| An Alpine host upgraded from ≤ 3.18 with a modified `sshd_config` may lack the Include (apk installs the new file as `.apk-new`) | same row; **[I]** in the audit's research, not verified | §2.1 `IncludeNone` |
| That the stock files hold no other `Include` | **[unverified]** | §2.4: a stock host is not refused |
| First value wins, except Port, ListenAddress, HostKey, Allow/Deny Users/Groups and AcceptEnv, which accumulate; Include order is C byte order (`00-hardn` < `000-x`) | [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9) **[V]**; row 20 of §12 | §2.3 copies keep their names; `p2-sshd-effective` |
| Without a line starting `Include /etc/ssh/sshd_config.d/*.conf`, cloud-init rewrites every matching line of `sshd_config` or appends one; with it, it writes `50-cloud-init.conf`. It does so when `ssh_pwauth` is set | cloud-init [`cloudinit/ssh_util.py`](https://raw.githubusercontent.com/canonical/cloud-init/main/cloudinit/ssh_util.py) (`_includes_dconf`, `update_ssh_config`), read 2026-10-17; `cloudinit/config/cc_set_passwords.py` (the `ssh_pwauth` condition, from the audit's research, not re-read) | §2.1 clobberable |
| Proxmox VE's `setup_sshd_config` (from `pvecm create` and `add`) replaces the first `PermitRootLogin` line of `sshd_config` with `PermitRootLogin yes`, or appends one, then reloads | pve-cluster [`src/PVE/Cluster/Setup.pm`](https://git.proxmox.com/?p=pve-cluster.git;a=blob_plain;f=src/PVE/Cluster/Setup.pm;hb=HEAD) (`setup_sshd_config`), read 2026-10-17; plan R15 | §2.1 clobberable |
| sshd's line syntax: the loader drops leading spaces, tabs and CRs, and appends each line with `strlen`, so a NUL ends the line and joins the next one to it; then trailing whitespace and `\f` are stripped; the keyword is `strdelim`'s first token (whitespace, one `=`, double quotes), or its second when the first is empty ("Ignore leading whitespace"); a NULL token (an unmatched quote) skips the line; a keyword starting `#` is a comment; arguments split by `argv_split` (quotes, backslash escapes, a `#` word ends the line) | OpenSSH [V_9_2_P1 `servconf.c`](https://raw.githubusercontent.com/openssh/openssh-portable/V_9_2_P1/servconf.c) lines 1350-1380 (`process_server_config_line_depth`) and 2623-2624 (`load_server_config`), and [V_10_3_P1](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_3_P1/servconf.c) lines 1341-1371 and 2836-2837; `misc.c` [V_9_2_P1](https://raw.githubusercontent.com/openssh/openssh-portable/V_9_2_P1/misc.c) line 398 and [V_10_3_P1](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_3_P1/misc.c) line 469 (`strdelim_internal`), and `argv_split` | §2.1, §2.4, §2.6 |
| A relative Include is resolved against `/etc/ssh` (`SSHDIR`) | `servconf.c` V_9_2_P1 line 2156, V_10_3_P1 line 2333 | §2.4 |
| An included file does not change the including file's Match state | `servconf.c` V_9_2_P1 line 2161, V_10_3_P1 line 2338 ("Don't let included files clobber the containing file's Match state") | §2.6 |
| Include globs skip names with a leading `.` unless the pattern has one | OpenSSH [`openbsd-compat/glob.c`](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_3_P1/openbsd-compat/glob.c) lines 752-754 ("Initial DOT must be matched literally"); [glob(7)](https://man7.org/linux/man-pages/man7/glob.7.html) | §2.2, §2.3 |
| `sshd -t`/`-T` load the host keys (exit 1, `sshd: no hostkeys available -- exiting.`); as root they need the privsep directory, a root-owned directory not group- or world-writable (`fatal`: `Missing privilege separation directory: %s`, `%s must be owned by root and not group or world-writable.`); test mode is exempt from the absolute-path check | OpenSSH [V_9_2_P1 `sshd.c`](https://raw.githubusercontent.com/openssh/openssh-portable/V_9_2_P1/sshd.c) lines 1696, 1917-1919, 1965-1981; [V_10_3_P1](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_3_P1/sshd.c) lines 1454, 1670-1672, 1727-1739; [phase0-findings.md §12](../refactor/phase0-findings.md#12-proposed-adjustments-to-the-brief), row 22 | §2.7, §2.8 |
| `fatal()` exits 255; messages go to stderr without a program-name prefix, each ended by `\r\n` | OpenSSH [`fatal.c`](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_3_P1/fatal.c) line 45 (`cleanup_exit(255)`); `log.c` lines 408-410 | §2.7 |
| As root, sshd needs the privilege separation user (`fatal`: `Privilege separation user %s does not exist`) | `sshd.c` V_9_2_P1 line 1806, V_10_3_P1 line 1722 | §2.7 |
| sshd adds its default host key files only when the configuration names no `HostKey` | `servconf.c` V_9_2_P1 line 287, V_10_3_P1 line 303 (`num_host_key_files == 0`) | §2.7 `HostKeysSet` |
| Configuration messages: `%s line %d: …` and `%s: line %d: Bad configuration option: %s`; `Directive '%s' is not allowed within a Match block`; `Bad Match condition` | `servconf.c` V_9_2_P1 lines 742, 1391, 2244; V_10_3_P1 lines 797, 1382, 2421 | §2.7 `Problems`; the fake's scripts |
| `-C` takes `addr=`, `host=`, `user=`, `laddr=`, `rdomain=`, `lport=` (and `invalid-user` from OpenSSH 10), comma-separated; a bad spec exits 1; `-C` without `-T` is fatal; a criterion the spec does not supply does not match | `servconf.c` `parse_server_match_testspec`, `match_cfg_line` (V_9_2_P1 and V_10_3_P1); `sshd.c` V_9_2_P1 lines 1665-1669, 1732; [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9) | §2.6 |
| Match criteria: All, User, Group, Host, Address, LocalAddress, LocalPort, RDomain; OpenSSH 10 adds Invalid-User and Version. Every criterion on a line must match (ANDed), a repeated one too | `servconf.c` `match_cfg_line`, V_9_2_P1 and V_10_3_P1 | §2.6 |
| `Match` is tokenised by `strdelim` on 9.2 and 9.6 (double quotes only; `=` splits, so `User=bob` already parses there) and by `argv_split`/`argv_next` on 10.0 and 10.3 (single and double quotes, backslash escapes; `attr=value` only for the listed attributes). Which of 9.6 to 10.0 changed it is not checked; Ubuntu 26.04's 10.2 lies between 10.0 and 10.3 (**[I]**) | `servconf.c` V_9_2_P1 line 1116, [V_9_6_P1](https://raw.githubusercontent.com/openssh/openssh-portable/V_9_6_P1/servconf.c) line 1082, [V_10_0_P1](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_0_P1/servconf.c) (`match_cfg_line` reads `argv_next`, read 2026-10-17), V_10_3_P1 line 1055; versions per distro: the audit's `research-ssh-distro-facts.md` §1 **[V]** | §2.6 |
| A pattern list matches when a pattern that is not negated matches and no negated one does (`match_pattern_list` returns `got_positive`, -1 on a negated match); a subpattern of 1023 bytes or more matches nothing; `User` is case-sensitive, `Host` lower-cased. An address list matches CIDR entries by containment and others as patterns over the address text, starts from no match, and an empty entry is an error; `Match` takes only a result of 1 as a match | `match.c` V_9_2_P1 and V_10_3_P1 lines 57 (`match_pattern`), 122-171 (`match_pattern_list`); `addrmatch.c` V_9_2_P1 line 49, V_10_3_P1 line 46 (`addr_match_list`); `servconf.c` `match_cfg_line`; [ssh_config(5)](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_3_P1/ssh_config.5) line 2295 ("a negated match will never produce a positive result by itself") | §2.6 |
| With `UseDNS no` (the default), `Match Host` can use addresses only, not names | [sshd_config(5)](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_3_P1/sshd_config.5) `UseDNS`, line 2041 onward | §2.6 |
| `sshd -G` is absent on Debian 12 (9.2p1) and Ubuntu 22.04 (8.9p1) and rejects `-C` | plan `p2-sshd-validate` row; [phase0-findings.md §12](../refactor/phase0-findings.md#12-proposed-adjustments-to-the-brief), row 22 | §2.5 |
| Debian and Ubuntu build sshd with `/run/sshd` as the privsep directory (`--with-privsep-path=/run/sshd`); only `ssh.service`'s `RuntimeDirectory=sshd` creates it, and it is gone while the service is stopped (LP #2107356, closed as expected behaviour) | the audit's `research-ssh-distro-facts.md` §3d **[V]**, citing [Debian `debian/rules`](https://sources.debian.org/data/main/o/openssh/1:10.0p1-7+deb13u4/debian/rules) line 60 and [LP #2107356](https://bugs.launchpad.net/ubuntu/+source/openssh/+bug/2107356); that it also hits socket-activated Ubuntu before the first connection is **[I]** there | §2.8 |
| Proxmox VE 9 runs Debian 13's openssh package unchanged, so the same holds there | the audit's `research-ssh-distro-facts.md` §1 **[V]** (the pve-no-subscription index has no openssh of its own) | §2.8 |
| A derivative whose `ID_LIKE` is debian or ubuntu uses that build | **[I]** | §2.8 |
| Alpine's privsep directory is `/var/empty` | **[unverified]** (stated in the audit's research without a source); not pre-checked | §2.8 |

## 4. Written, not run (gate 5)

No `sshd` has run. The tests script `sshd`'s answers in the fake from the messages above; whether real sshd prints them is the first thing to check. On the Debian 13, Ubuntu 26.04 and Alpine 3.24 VMs (`next/p2-vmtest`), by hand on Proxmox VE 9, and where possible on Debian 12 (9.2p1) and Ubuntu 24.04 (9.6p1):

- `Validate` as root with `RunDir` under `/run/hardn/<id>` on the stock configuration (no error, no finding) and with the brief's 2230 candidate; `sshd -t`/`-T` reading the copy with the rewritten Include, and no permission check on the copy's files (**[unverified]** that sshd makes none);
- each classified message from the real binaries, with the copy's paths: a `Port` in a `Match` block, a bad option, a bad value, `Bad Match condition`; distribution patches could change the text;
- `/run/sshd`: removed by `systemctl stop ssh` on Debian 13 (`EnvPrivsepDir` from the pre-check), and on Ubuntu 26.04 before the first connection after boot; and `sshd -t` as an unprivileged user (`EnvHostKeys`);
- the probes' output with `Match User`, `Match Address` and `Match Group` blocks; `-C` accepting the default connection's user, which has no account (**[unverified]** that sshd -T needs none), and matching no stock block; the matching mirror against real sshd for negations, a repeated attribute and `Match User 'bob'` on Debian 12 and Ubuntu 24.04 (9.x) against Debian 13 (10.0); `Match Host` under `UseDNS no`;
- that fatal messages arrive as whole lines with no prefix (the privsep directory and user messages), and the privsep-user message when the `sshd` account is removed;
- Alpine: `IncludeNone` on a host upgraded from 3.18 with a modified `sshd_config`; `sshd` against `sshd.pam` with `UsePAM yes` (a warning from one, a pass or a failure from the other, **[I]**); the privsep message from `/var/empty`;
- Proxmox VE 9: the stock configuration after `pvecm create` (`PermitRootLogin yes` in `sshd_config`, `IncludeFirst`).

## 5. API

```go
package sshd

const MainConfig = "/etc/ssh/sshd_config"
const DropInDir = "/etc/ssh/sshd_config.d"
const HardnDropIn = DropInDir + "/00-hardn.conf"
const IncludeLine = "Include " + DropInDir + "/*.conf"
const DefaultBinary = "sshd"
const PrivsepDir = "/run/sshd"
const WorkDir = "sshd-validate"

type IncludeMode uint8 // IncludeUnknown (zero), IncludeFirst, IncludeNone, IncludeOther; String()
type Include struct { Mode IncludeMode; Line int; Text string }
func ParseInclude(main []byte) Include
func DetectInclude(ctx context.Context, sys host.System) (Include, error)

type Conn struct { User, Host, Addr, LAddr, LPort, RDomain string }
func DefaultConn() Conn
func (c Conn) Spec() string // "user=…,host=…,addr=…[,laddr=…][,lport=…][,rdomain=…]"

type Change struct { Path string; Data []byte; Remove bool } // Remove with Data: fs.ErrInvalid
type Request struct { Changes []Change; RunDir string; Platform detect.Platform; Binary string; Conn Conn }
type Block struct { File string; Line int; Text string }
type Probe struct { Block Block; Conn Conn; Reasons []string; Output []byte } // no Reasons: the block applies to Conn
type FindingKind uint8 // FindingClobberable, FindingIncludeNotFirst, FindingMatchUntargeted, FindingWarning; String()
type Finding struct { Kind FindingKind; File string; Line int; Detail string }
type Report struct { Include Include; Files []string; Effective []byte; Probes []Probe; Findings []Finding; Cleanup error }
func Validate(ctx context.Context, sys host.System, req Request) (Report, error)

var ErrConfig, ErrEnvironment, ErrUnexpected, ErrUnsupportedInclude, ErrNotIncluded, ErrUnsafeRunDir, ErrNotRegular error
type Problem struct { File string; Line int; Message string }
type ConfigError struct { Argv []string; Problems []Problem; Tail []string; Err error } // Is(ErrConfig); Unwrap: *host.ExitError
type EnvKind uint8 // EnvPrivsepDir, EnvHostKeys, EnvPrivsepUser; String()
type EnvError struct { Kind EnvKind; Path, User string; HostKeysSet bool; Argv, Tail []string; Err error } // Is(ErrEnvironment)
type UnexpectedError struct { Argv []string; ExitCode int; Tail []string; Err error } // Is(ErrUnexpected); Unwrap: *host.ExitError
```

**Import rules** (depguard, `.golangci.yml`): the package is under `pkg/host`, so `host-stdlib` holds it to the standard library and `pkg/host/...`; `sshd-through-host` (§2.11) keeps it to `host.System`. Its tests use `hostfake`.

## 6. Not in this unit; inputs to later units

- **`p3-engine`:** call `Validate` with the candidate before the backup and `WriteFileAtomic` of the real file, and install only on a nil error: an `EnvError` is not a pass. Give it a run directory that is root's from `/` down (`/run/hardn/<run-id>`, `0700`, not `/var/run/…`, which is a symlink). Report `Report.Findings`. After installing, the reload (`p2-reload`) and the effective-value check (`p2-sshd-effective`) follow; on socket-activated Ubuntu `sshd -T` is not the listener (brief 2.3).
- **`p2-sshd-effective`:** parses `Report.Effective` and each `Probe.Output` (keys case-insensitive, `without-password` = `prohibit-password`, accumulating keys as sets); `Report.Files` is the drop-in order for its conflict report (not precedence: in `IncludeFirst` mode the drop-ins are read at `Include.Line`, before the rest of `sshd_config`). A probe with no `Reasons` shows a block that applies to `Probe.Conn`, but an earlier block that matches `Probe.Conn` too still wins for the keywords it sets; a probe with `Reasons` may show the block not applied, so its values are Unknown for that block, not evidence.
- **`p2-sshd-migrate`:** removals are `Change{Remove: true}`. A custom v0.3.x `sshConfigFile` path is outside `MainConfig` and `DropInDir`, so `Validate` refuses it; that unit decides what to check there.
- **`p2-alpine`, `p2-reload`:** which sshd flavour the OpenRC script runs (`sshd.pam` with `UsePAM yes`); pass it as `Request.Binary`.
- **`p3-output`:** map `ConfigError` to `SSHD_CONFIG_INVALID` and `EnvError{EnvPrivsepDir}` to `SSHD_ENV_NO_RUNDIR` (the plan's example codes); `EnvHostKeys` (with or without `HostKeysSet`), `EnvPrivsepUser`, `UnexpectedError`, `ErrUnsupportedInclude` and `ErrNotIncluded` need codes of their own. Show `Tail` and the error text, which name the real files; `Argv` names the copy.
- **Open for the maintainer:** `IncludeOther` (an Include that is not exactly the stock first line) has no write strategy in brief §2.2, which covers the Include at the top and no Include; hardn reports it and should not guess.
- **Not here:** writing `00-hardn.conf` or the hardn block (the engine and the slices), the reload, `sshd -T` against the installed configuration, the `ssh.socket` listener.
