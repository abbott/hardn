# Reload: making sshd use an installed configuration (`pkg/host/reload`)

> **Unit:** refactor plan Phase 2, `next/p2-reload` (brief 2.3 steps 3, 4 and 7; plan §2 `p2-reload` row: mechanics only, the ordering rule is `p3-engine`'s). V-numbers: V7 (restart instead of reload, no `sshd -t` before activation), V12 (`ssh.socket` not handled), and the reporting half of V24 (v0.3.x's `ssh.socket.d/listen.conf` left behind) ([side-effects.md](../refactor/phase0/side-effects.md)). Brief corrections it implements: [phase0-findings.md §12](../refactor/phase0-findings.md#12-proposed-adjustments-to-the-brief), rows 21 and 22.
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only, with `systemctl`, `rc-service` and `ss` answers written in the format of their sources (§3), not captured from a host. Nothing has run on Linux; §4 lists what is "written, not run (gate 5)", among it that SSH sessions survive a restart of `ssh.socket`.

Once `00-hardn.conf` (or hardn's block) is validated and installed, the running sshd still uses what it read before. `reload.PlanFor(in)` says, from detect's SSH facts and the keywords the change touched, which commands make it reread the files, in which order, which listeners must then be bound, and which listeners the caller must keep configured until the change is confirmed. `reload.Execute(ctx, sys, plan, opts)` runs the plan over `host.System` and checks the bound listeners with `ss`. `ReadSocketDropIns` reports `ssh.socket`'s pre-existing drop-ins, and `OpenRCFlavour` names the sshd binary Alpine's init script runs.

## 1. Why

origin/main writes the sshd files and runs `systemctl restart ssh` or `rc-service sshd restart`, with no `sshd -t` and no socket handling (V7, V12). On Ubuntu 24.04 and 26.04 sshd is started by `ssh.socket`: a reload there cannot move the port, and a port change reaches the socket only through `daemon-reload` and a restart of `ssh.socket`, while a firewall that already allows only the new port locks the user out (V12 with V9). v0.3.2's `listen.conf` overrides the socket's generator and was never followed by `daemon-reload` (V12, V24). The `sshd` unit alias that a guess would call is missing on a fresh Ubuntu 24.04 (findings §12 row 22). Brief §3 principle 1: never lock the user out.

## 2. Guarantees

### Planning (`PlanFor`, pure)

1. **The steps follow the SSH mode detect established**, and an Unknown mode is refused (`ErrModeUnknown`, wrapping `SSH.Err`, for example detect's `ErrConflict`): hardn does not guess which unit listens ([detect.md](detect.md) §2.7, §6).

   | Mode (platforms) | A listener keyword changed (`Port`, `ListenAddress`, `AddressFamily`) | Only other keywords changed |
   |---|---|---|
   | `SSHSocket` (Ubuntu 24.04, 26.04) | `systemctl daemon-reload`; check `systemctl show -p Listen ssh.socket`; if `ssh.socket` is active, `systemctl restart ssh.socket`; then verify | if `ssh.service` is active, `systemctl reload ssh.service` |
   | `SSHService` (Debian 13, 12; Proxmox VE 9) | if `ssh.service` is active, `systemctl reload ssh.service`; then verify | the same, without the verify |
   | `SSHOpenRC` (Alpine 3.24, 3.23) | if OpenRC has started sshd, `rc-service sshd reload`; then verify | the same, without the verify |

   Keywords are compared case-insensitively; no keyword means an empty plan (the engine's second, idempotent plan reloads nothing). Tested per platform, with detect's facts for each stock install.
2. **Never the `sshd` unit on systemd.** Only `ssh.service` and `ssh.socket` are named, as full unit names (`ssh` and `ssh.service` are the same unit; the full name leaves no doubt). Tested: no argument of any systemd plan starts with `sshd`. On OpenRC the service is `sshd`, its real name.
3. **Reload, not restart, for sshd itself.** `systemctl reload ssh.service` runs the unit's `ExecReload` (`sshd -t`, then `kill -HUP`), and `rc-service sshd reload` runs the init script's `checkconfig` (`sshd -t`) before `HUP`: a configuration sshd rejects fails the reload and leaves the old daemon running (§3). The one restart is `ssh.socket`'s, which the socket needs to bind a new port.
4. **A listener change on a socket host must reach the socket.** It is refused before anything runs when the socket's listeners would not come from `sshd_config`: `Generator` not Yes (masked, absent as on a Debian host opted into the socket, or Unknown: `ErrNoGenerator`), or a pre-existing drop-in with a `ListenStream` line, such as v0.3.x's `listen.conf` (`ErrSocketDropIn`; its listeners replace the generator's, §3). The caller must pass `ReadSocketDropIns`'s answer (`fs.ErrInvalid` otherwise). After `daemon-reload`, the socket's configured listeners are read back, and **the socket is not restarted unless they cover every listener the files name** (`*ListenerError`, `ErrSocketConfig`), so a generator or a drop-in hardn does not model cannot rebind the socket onto the wrong port.
5. **The old listeners stay until the change is confirmed (requirement, returned as data).** For a listener change, `Change.Before` is the established listener set before it (`sshd.ReadEffective`'s `Listeners.All`, with `Known`) and `Change.After` what the installed files configure (`Listeners.SSHD` of `sshd.Evaluate` over the validated candidate). Unless `Confirmed`, every listener of `Before` must be covered by `After`, or the plan is refused with a `*HoldError` (`ErrDropsListener`) naming the missing ones; `Plan.Hold` is `Before`, and `HoldPorts(Plan.Hold, want)` the Port lines to write: the old ones beside the new. Port and ListenAddress accumulate, so sshd, and the generator from it, configure both (§3). After confirmation from a new connection (`p2-revert`), the clean-up change is planned with `Confirmed`. This holds on every mode, not only the socket's that the plan row names: a reload on `ssh.service` or OpenRC rebinds too (§3, [I]), so dropping the old port there is the same lockout window. Coverage: the same port, and the same address or an unspecified one of the same family (sshd binds `::` IPv6-only, §3), so `AddressFamily inet` dropping `[::]:22` is refused, and widening `ListenAddress 192.0.2.10` to `0.0.0.0` is not.
6. **Alpine: no change that may switch the sshd binary.** `UsePAM`, `KerberosAuthentication` and `GSSAPIAuthentication` decide whether the init script runs `sshd`, `sshd.pam` or `sshd.krb5`, and its reload signals the binary the new files call for (`start-stop-daemon --exec`), which may not be the one running (§3, [I]). Such a change is refused (`ErrFlavour`); it needs a restart, which is a later unit's call.
7. **Pre-existing socket drop-ins are reported.** `ReadSocketDropIns` lists every `*.conf` in `ssh.socket.d` under each directory of systemd's unit load path that holds hand-made, runtime or packaged configuration: `/etc/systemd/system.control`, `/run/systemd/system.control`, `/run/systemd/transient`, `/etc/systemd/system`, `/run/systemd/system`, `/usr/local/lib/systemd/system`, `/usr/lib/systemd/system` (§3). The generator directories are not read: the generator's own `addresses.conf` is there. Each file's `ListenStream` values in its `[Socket]` section are given in order (`""` is the empty assignment that resets the list), read with systemd's comment and continuation rules; a symlink to `/dev/null` is `Masked`. Nothing is edited (migrating `listen.conf` is `p2-sshd-migrate`'s). A missing directory holds none; any other error fails the read (`Read` false), since an unread drop-in may move the listeners. `Plan.DropIns` carries them, refused plans included, for the caller to report.

### Executing (`Execute`)

8. **Steps run in order and stop at the first failure;** the `Report` holds every step up to the failed one with its `host.Result`. A command's error is returned wrapped with its argv (`*host.ExitError` for a non-zero exit, with the output tail: `sshd -t`'s complaint for a failed reload).
9. **"Is it up" is asked at the moment, not taken from detect.** A gated step first runs `systemctl is-active <unit>` (or, on OpenRC, looks for `/run/openrc/started/sshd`, as detect does). Not up: the step does not run, and `StepReport.NotUp` says why; sshd reads the files on disk when it next starts (on Ubuntu, `ssh.service` is inactive until the first connection, which then starts sshd from those files). The check comes after the files are installed, so a unit that starts in between reads the new files. If a reload fails and the unit is now not up (it stopped meanwhile), that is `NotUp`, not an error; if it is still up, the failure stands. A failed restart of `ssh.socket` (`Step.Restart`) always stands, since a restart that leaves the socket down is the failure itself (tested). A state that is neither (`activating`, …) is `ErrUnitState`: no guess. A not-up socket is never restarted, since `systemctl restart` starts a unit that is not running (§3).
10. **The real listener is verified with the bound sockets.** `sshd -T` and `systemctl show -p Listen` are configuration ([sshd-effective.md](sshd-effective.md) §2.5): after `daemon-reload` both name the new port before anything binds it. So once the steps are done, `ss -Htln` must show a LISTEN socket covering every listener of `Plan.Verify` (an `ss` `*` is dual-stack and covers both families). A reload signals sshd and returns before sshd has rebound, so the check polls: `Options.Attempts` (default 10) reads, `Options.Interval` (500 ms) apart; then `*ListenerError` with `ErrNotBound` and the missing listeners. `ss` missing (`host.ErrNotFound`: no iproute2) leaves the change `Unverified`, not failed; `ss` failing is an error. `Verified` says only that something is bound on those addresses and ports, not that it is sshd: proof is the confirmation from a new connection (brief 2.4, `p2-revert`).
11. **Context.** A done `ctx` is refused before anything runs; a rollback after Ctrl-C must pass `context.WithoutCancel(ctx)` (tested), as every `host.System` method requires ([system.md](system.md) §2.5). Every command reaches the run log through `host.System`.
12. **Through `host.System` only.** Depguard rule `reload-through-host` denies `os`, `io/ioutil`, `golang.org/x/sys`, `syscall`, `net` (except `net/netip`) and `plugin` to the package's non-test files; `host-stdlib` keeps it to the standard library and `pkg/host/...`.

### Alpine's binary (`OpenRCFlavour`)

13. It mirrors the init script's `update_command`: `/usr/sbin/sshd.krb5` when executable and its own `sshd.krb5 -f /etc/ssh/sshd_config -G` prints `KerberosAuthentication yes` or `GSSAPIAuthentication yes` (any case, whole line), else `/usr/sbin/sshd.pam` when executable with `UsePAM yes`, else `/usr/sbin/sshd`. For `sshd.Validate`'s and `ReadEffective`'s `Binary` ([sshd-validate.md](sshd-validate.md) §6). Where the script ignores a failing `-G`, this returns its error. An `/etc/conf.d/sshd` whose non-comment lines name `SSHD_BINARY`, `SSHD_CONFIG`, `SSHD_CONFDIR` or `cfgfile` is refused (`ErrConfd`): the script would run another binary or read another file than hardn models.

## 3. Platform facts relied on

| Fact | Source | Used for |
|---|---|---|
| Ubuntu 24.04 and 26.04 are socket-activated by default; `sshd-socket-generator` derives `ssh.socket`'s `ListenStream` from `sshd_config` and its drop-ins, at boot and on `daemon-reload`; the stock `sshd_config` says a Port, AddressFamily or ListenAddress change needs `systemctl daemon-reload` and `systemctl restart ssh.socket` | [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "`ssh.socket` per Ubuntu version" **[V]**; primary sources Ubuntu openssh `debian/rules`, `sshd_config` (`applied/ubuntu/resolute-devel`, lines 26-33), `debian/patches/sshd-socket-generator.patch` | §2.1, 2.4 |
| On a socket host a reload cannot change Port or ListenAddress (sshd uses the descriptors systemd passes); `systemctl reload` fails on an inactive unit; `ssh.service` is inactive until the first connection, which starts sshd from the files on disk | findings §12 row 21; audit research `research-ssh-distro-facts.md` §4b **[I]**: Ubuntu `debian/systemd/ssh.service`, `ssh.socket` (resolute), systemd v257 `src/core/unit.c` (`unit_reload`) | §2.1, 2.9 |
| `ssh.service`'s `ExecReload` is `sshd -t` then `kill -HUP $MAINPID`; a rejected configuration fails the reload and the old daemon keeps running; on `HUP` sshd re-executes and rereads its files, so existing sessions keep running | research §4b: Debian 13 and Ubuntu `debian/systemd/ssh.service`; OpenSSH `sshd.8` (V_10_3_P1, lines 80-84); the effect on sessions **[I]** | §2.3 |
| A reload on `ssh.service` (not socket-activated) or OpenRC rebinds a changed Port | research §4b, "Also inferred" **[I]** | §2.5: the hold on every mode |
| `ssh.socket` has `[Install] RequiredBy=ssh.service`, so restarting it restarts `ssh.service`; sessions survive because `ssh.service` has `KillMode=process` | research §4b **[I]**; systemd v257 `man/systemd.unit.xml` (`Requires=`, `RequiredBy=`) | §2.1; **written, not run** (§4) |
| `systemctl restart` starts a unit that is not running; `daemon-reload` reruns all generators; `is-active` exits 0 only when a unit is active | systemctl(1), [systemd v257 `man/systemctl.xml`](https://raw.githubusercontent.com/systemd/systemd/v257/man/systemctl.xml) | §2.9, 2.4 |
| After `daemon-reload`, a socket unit reattaches a kept descriptor only to a port still configured (`socket_deserialize_item`), warns that it is "not fully functional until restarted" while configured ports have no descriptor (`socket_coldplug`), and refuses to start while its service is active ("Socket service %s already active, refusing", `socket_start`) | [systemd v257 `src/core/socket.c`](https://raw.githubusercontent.com/systemd/systemd/v257/src/core/socket.c); that an unmatched descriptor is then closed **[I]** | §2.4, 2.5: new ports are not bound before the restart; the socket's configuration is checked before it |
| Debian and Proxmox VE run `ssh.service`; socket activation is opt-in there, with no generator and the port in `ssh.socket.d/listen.conf` | findings §10 **[V]**; research §4c (Debian `debian/rules`, `README.Debian` lines 184-218) | §2.4 `ErrNoGenerator` |
| The unit is `ssh` on Debian, Ubuntu and Proxmox VE; the `sshd.service` alias is missing on a fresh Ubuntu 24.04 (LP #2061055); Alpine's OpenRC service is `sshd` | findings §10, "Service names / init" **[V]**; research §5 | §2.2 |
| Alpine's `reload` is an `extra_started_commands` entry (started services only): `checkconfig` (`update_command`, host keys, `"$command" -t`), then `start-stop-daemon --signal HUP --exec "$command" --pidfile "$pidfile"`; `update_command` picks `sshd.krb5` or `sshd.pam` by that binary's own `-G` output; `command` is `${SSHD_BINARY:-/usr/sbin/sshd}` and `cfgfile` comes from `SSHD_CONFIG`/`SSHD_CONFDIR` | aports `3.24-stable/main/openssh/sshd.initd`, read 2026-10-17 from the GitHub mirror ([raw](https://raw.githubusercontent.com/alpinelinux/aports/3.24-stable/main/openssh/sshd.initd)); the gitlab.alpinelinux.org raw path disallows automated reads in its robots.txt | §2.6, 2.13; that `--exec` then misses a daemon started as another flavour **[I]** |
| v0.3.2 writes `/etc/systemd/system/ssh.socket.d/listen.conf` (`[Socket]`, `ListenStream=`, `ListenStream=<port>`) and never runs `daemon-reload`; drop-ins with different names apply in lexicographic order across directories, so `listen.conf` comes after the generator's `addresses.conf`, and its empty assignment resets the generator's listeners | research "Extra: legacy hardn SSH artifacts" (v0.3.2 `pkg/ssh/ssh.go:94-107`); [side-effects.md](../refactor/phase0/side-effects.md) V12, V24; Debian `README.Debian` (research §4c) for the reset | §2.4, 2.7 |
| The system unit load path, in order: `/etc/systemd/system.control`, `/run/systemd/system.control`, `/run/systemd/transient`, `/run/systemd/generator.early`, `/etc/systemd/system`, `/run/systemd/system`, `/run/systemd/generator`, `/usr/local/lib/systemd/system`, `/usr/lib/systemd/system`, `/run/systemd/generator.late`; drop-ins in `/etc` take precedence over `/run`, which take precedence over `/usr/lib`; "multiple drop-in files with different names are applied in lexicographic order" | systemd.unit(5), [systemd v257 `man/systemd.unit.xml`](https://raw.githubusercontent.com/systemd/systemd/v257/man/systemd.unit.xml) ("Unit File Load Path") | §2.7 |
| Unit-file syntax: lines starting with `#` or `;` are comments; a line ending in a backslash continues on the next, and comment lines in between are ignored; an empty assignment resets a list | systemd.syntax(7), [systemd v257 `man/systemd.syntax.xml`](https://raw.githubusercontent.com/systemd/systemd/v257/man/systemd.syntax.xml) | §2.7 |
| `ss -l` selects the LISTEN and CLOSE states, so the State column stays; one table (`-t`) drops the Netid column; no Process column without `-p`; `-H` drops the header. The local address is IPv4 as is, IPv6 in brackets, `*` for `::` when not IPv6-only, followed by `%<interface>` for a socket bound to one | [iproute2 v6.14.0 `misc/ss.c`](https://raw.githubusercontent.com/iproute2/iproute2/v6.14.0/misc/ss.c) (`main`, `inet_addr_print`, `sock_addr_print`); that every supported release's `ss` prints the same **[unverified]** | §2.10 `ParseBound` |
| `ss` (iproute2) is installed on a stock Debian, Ubuntu and Proxmox VE, and not on a stock Alpine | **[unverified]** | §2.10: `Unverified` when missing |
| sshd sets `IPV6_V6ONLY` on its IPv6 listening sockets, so `0.0.0.0` and `::` are separate listeners | [OpenSSH V_10_3_P1 `sshd.c`](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_3_P1/sshd.c) (`listen_on_addrs`: `sock_set_v6only`) | §2.5 coverage |
| systemd's dual-stack `[::]` socket and sshd's `0.0.0.0` plus `[::]` accept the same connections | [sshd-effective.md](sshd-effective.md) §2.5 | §2.4 the socket check compares every unspecified address as one |
| OpenRC marks a started service with `/run/openrc/started/<service>` | [detect.md](detect.md) §3 (OpenRC 0.63.2 `src/librc/librc.c`) | §2.9 |

## 4. Written, not run (gate 5)

Nothing here has run on Linux; no test starts `systemctl`, `rc-service`, `ss` or `sshd`. On the Ubuntu 26.04, Debian 13 and Alpine 3.24 VMs (`next/p2-vmtest`), on an Ubuntu 24.04 host, and by hand on Proxmox VE 9:

- **existing SSH sessions survive `systemctl restart ssh.socket`** on Ubuntu 24.04 and 26.04, both before the first connection (`ssh.service` inactive) and after it (the session running the change keeps working; §3, `KillMode=process` **[I]**), and survive `systemctl reload ssh.service` and `rc-service sshd reload`;
- the brief's 2230 scenario on Ubuntu 26.04: `Port 22` and `Port 2230` written, `daemon-reload`, `systemctl show -p Listen ssh.socket` naming both, `systemctl restart ssh.socket`, `ss -Htln` showing both, then the confirmed clean-up dropping 22; and what `ss` shows between `daemon-reload` and the restart (2230 not bound is expected, §3);
- a reload on Debian 13 and Alpine moving the port (§3 **[I]**), and how long sshd takes to rebind after `HUP` against the 10 × 500 ms poll;
- `systemctl reload ssh.service` with `ssh.service` inactive on Ubuntu (fails; the plan skips it), with a configuration `sshd -t` rejects (fails, old daemon running), and `rc-service sshd reload` with sshd stopped;
- the refusals on real hosts: a v0.3.2-hardened image with `listen.conf` (`ErrSocketDropIn`; R8), the generator masked in `/etc/systemd/system-generators` (`ErrNoGenerator`), and a Debian host opted into the socket;
- `ss -Htln`'s output on each platform (§3 **[unverified]**), whether `ss` is installed on each stock image, and a dual-stack `*` line for `ssh.socket` on Ubuntu;
- a `daemon-reload` whose generated listeners leave out a port the socket holds (the case the hold prevents): whether systemd closes that descriptor (§3 **[I]**), and whether a running sshd keeps accepting on its own copy;
- `OpenRCFlavour` on Alpine with `openssh-server-pam` installed and `UsePAM yes`, and whether a reload after a `UsePAM` change misses the running daemon (§2.6 **[I]**).

## 5. API

```go
package reload

const ServiceUnit = "ssh.service"; const SocketUnit = "ssh.socket"; const OpenRCService = "sshd"
const DefaultAttempts = 10; const DefaultInterval = 500 * time.Millisecond
const BinarySSHD, BinaryPAM, BinaryKrb5 = "/usr/sbin/sshd", "/usr/sbin/sshd.pam", "/usr/sbin/sshd.krb5"
const OpenRCConfd = "/etc/conf.d/sshd"

var ErrModeUnknown, ErrNoGenerator, ErrSocketDropIn, ErrListenersUnknown, ErrDropsListener,
	ErrFlavour, ErrUnitState, ErrSocketConfig, ErrNotBound, ErrConfd error

type Change struct { Keywords []string; Before, After []sshd.Listener; Confirmed bool }
type Input struct { SSH detect.SSH; Change Change; DropIns SocketDropIns }
type StepKind uint8 // StepRun, StepIfUp, StepCheckSocket; String() run, run-if-up, check-socket
type Step struct { Kind StepKind; Argv []string; Unit string; OpenRC, Restart bool; Why string }
type Plan struct { Mode detect.SSHMode; Listen bool; Steps []Step; Verify, Hold []sshd.Listener; DropIns []SocketDropIn }
type HoldError struct { Missing []sshd.Listener } // Is(ErrDropsListener)

func PlanFor(in Input) (Plan, error)                         // pure
func HoldPorts(hold []sshd.Listener, want []uint16) []uint16 // the confirm window's Port lines

type Options struct { Attempts int; Interval time.Duration; Wait func(ctx context.Context, d time.Duration) error }
type StepReport struct { Step Step; Ran bool; Result host.Result; NotUp string }
type Report struct { Steps []StepReport; Bound []Bound; Verified bool; Unverified string }
type ListenerError struct { Source string; Missing []sshd.Listener; Bad []string; Err error } // Unwrap: ErrSocketConfig or ErrNotBound
func Execute(ctx context.Context, sys host.System, p Plan, opts Options) (Report, error)

type Bound struct { Addr netip.Addr; Port uint16; Dual bool; Device string } // String()
func ReadBound(ctx context.Context, sys host.System) (bound []Bound, bad []string, err error) // ss -Htln
func ParseBound(out []byte) (bound []Bound, bad []string)

type SocketDropIns struct { Read bool; Files []SocketDropIn }
type SocketDropIn struct { Path string; Listen []string; Masked bool }
func ReadSocketDropIns(ctx context.Context, sys host.System) (SocketDropIns, error)

func OpenRCFlavour(ctx context.Context, sys host.System) (string, error)

package sshd

func ParseSocketListen(out []byte) (ls []Listener, bad []string) // added here; ReadEffective's parser of systemctl show -p Listen
```

## 6. Not in this unit; inputs to later units

- **`p3-engine`:** the order. Brief 2.3: the firewall allows the new port first (`p2-ufw`), then write and validate (`sshd.Validate`, with `OpenRCFlavour`'s binary on Alpine), install, `PlanFor` and `Execute`, then `sshd.ReadEffective` to compare the values it wanted. For a port change, write `HoldPorts(Change.Before, want)` for the confirm window; plan `Confirmed` only after `p2-revert`'s confirmation. Treat `Unverified` and `NotUp` as "not proven": the confirmation from a new connection is the proof. On a `*ListenerError` after `daemon-reload` the socket was not restarted, but the generator has already rewritten its configuration: roll the files back and plan the reverse change with `Confirmed` (its own `daemon-reload`), under `context.WithoutCancel`.
- **`p2-revert`:** the journal's "services to reload" can hold the `Change` and replay `PlanFor` on rollback; a port rollback is itself a listener change, planned with `Confirmed` (it drops the new port on purpose).
- **`p2-ufw`:** `ReadBound` is a bound-socket source ([sshd-effective.md](sshd-effective.md) §6); it does not say which program holds a socket.
- **`p2-sshd-migrate`:** `ReadSocketDropIns` finds v0.3.x's `listen.conf`; removing it (backup, printed note) lets a socket host's listener change go ahead.
- **`p2-pve`:** no port change on Proxmox VE (plan §3.18) is that unit's refusal; this one plans the `ssh.service` reload.
- **`p2-alpine`:** a `UsePAM`/Kerberos change (`ErrFlavour`) needs `rc-service sshd restart`, whose `stop_pre` runs `checkconfig`; whether sessions survive it is unverified.
- **Not modelled:** systemd's `socket.d/` drop-ins, which apply to every socket unit, and a `BindIPv6Only=` or `FreeBind=` setting (the socket check after `daemon-reload` and the `ss` check catch their effect on the listeners); sshd's `rdomain` (neither `ss` nor `systemctl show` prints it, so listeners are compared by address and port only).
- **`sshd.ParseSocketListen`** is new API in `pkg/host/sshd`; [sshd-effective.md](sshd-effective.md)'s API list predates it.
