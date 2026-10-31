# The status model on a host: the read-only driver (`internal/tools/p4status`)

> **Unit:** refactor plan Phase 4, `next/p4-vmcheck` (plan header, "Phase 4 built (2026-10-21)": "Nothing has run on a Linux host (gate 5): a VM run needs a read-only driver that calls `Engine.Check` there"; [gallery-status.md](gallery-status.md) §4; brief §4 "System safety"; risk R14). No V-number and no proposal is decided here, and no behaviour of hardn changes: the one change outside the driver is `output.MainMenuOf`, which the gallery now calls and hardn does not yet (§2.11).
> **Status:** built and tested on macOS, in process, over hostfake fixtures of five of the gallery's status hosts (Debian 13, Ubuntu 26.04, Alpine 3.24, Proxmox VE 9, and Debian 13 hardened). **Nothing has run on Linux**: the VM run is [vmtest.md](../safety/vmtest.md) §8, every step written, not run (gate 5, §4). No line of the main menu is new: each is a check's, the probe's or the score's. The driver's own words (§5.2) are proposed.
> **Amended by `next/s2-menu-live`** (refactor plan slice 2; the maintainer, 2026-10-23: "Main menu goes live"): the driver reads the main menu's one list of status controls (`controls.Status`), so it prints **the DNS row** after Auto Updates, checked against `hardn.yml` as hardn finds it (`configPaths`: `config.ConfigFileSearchPath` with no `-f`, each path made absolute, read through the guard by `config.Read`; hardn's defaults, listing no nameserver, where none exists; one that does not load stops the reading, as it stops hardn). The DNS check's three commands are allowed (§2.5), the evidence opens with `hardn.yml` (§5.2), and the fixtures were exported again (§2.10). hardn's live main menu now draws what this driver prints ([menu-live.md](menu-live.md)). The passages it changes are marked below; the rest stands.

`p4status` is a dev-only command. Run as root on a host, it reads the host as hardn's main menu will, through the real code, and prints (1) the main menu as hardn will draw it, then (2) the evidence each line was read from, with every command it ran, so that a person can check each line against the host by hand. It changes nothing, by construction: it reaches the host only through a guard that refuses every change and runs only the commands the checks run.

## 1. Why

The five checks, the header probe and the risk level have run only on the recording fake, and the gallery's main menu only on simulated hosts ([gallery-status.md](gallery-status.md) §4). A status that says a host is safe when it is not is the worst error Phase 4 can make, and a model can agree with a check that is wrong about a real host. The maintainer's VMs (Debian 13 and Ubuntu 26.04, amd64) can show it, but hardn's main menu is not live until slice 2 (plan, "Phases 3–5 together"), and `p2drive` changes the host. A driver that only reads lets every line be compared with the host now, on a VM in any state, without wiring anything into `cmd/hardn`.

## 2. Guarantees

### What it prints

1. **The main menu, first, as hardn will draw it.** `output.MainMenuOf(screen.MainMenuFixture(), header, reports, level)`, rendered by `pkg/ui/screen` with `component.NewCtx`: the same mapping and screen as the gallery's status hosts, so the header, distribution bar, host block, risk summary and status rows are the reading's, and the items, heading, mode, prompt and version (`v0.4.0`) the mock's, as there (slice 2 owns them). By default it is plain at 80×24, the gallery's `plain-80` cell, so it pastes into a log; `-profile` (`plain`, `none`, `16`, `256`, `truecolor`), `-width` and `-height` pick another terminal, each built by `term.Resolve` from the terminal it names (no colour library of its own). Plain draws the full form at any height. When the guard refused a call (§2.6), a line above the main menu, `== incomplete: <n> call(s) refused, in <parts>`, names the parts of the reading that made one: a row's control ID, `detect`, `header`. Tested: `TestMainMenuAsGallery` (over each fixture, line for line the main menu in the gallery's committed golden `status/<host>.golden`, plain-80 cell), `TestProfiles`.
2. **Then the evidence** (`evidence.go`), after the line `== p4status evidence: …`, plain in every profile, each string read from the host with its control characters written as `\xNN`, and its format and line-separating characters (a bidirectional override such as U+202E, U+2028) as `\uNNNN`, so that none reorders a line checked by hand:
   - `facts (detect)`: os-release, platform (with `like`, which the Auto Updates check and the header probe branch on for a derivative), init, ssh, proxmox (with `etc-pve`), container, elevation, as `key=value`, each with `error <CODE> <message>` when its `Err` is set;
   - under each part of the reading that made a refused call (`facts (detect)`, a row, `header`), first, `incomplete <n> call(s) refused` (§2.6), whatever the row's state;
   - per row, in the order checked, `row <control id> (<title>)`, then `state` (the JSON word: `configured`, `not-configured`, `partial`, `not-applicable`, `unknown`), `detail` (`(none)` when empty), each `evidence` line, each `finding <CODE> <detail> [<source>]`, and `error <CODE> <message>` with the error's code in `pkg/output`'s registry;
   - `header`: `os`, `hostname`, `ipv4` and `ipv6` (default route, interface, address), `primary`, `uptime` (Figma's form and the seconds), `reboot` (pending, running kernel, then `own kernel`, or inside a container `container host's kernel`, whose sources are not read), with its evidence and findings, each fact's `error` line when it has one;
   - `risk`: `band`, `score`, `forced`, each `factor <detail> (<kind>, row <control>, cost <points>)`, and the cross-checks' findings;
   - `commands (<n> run, <m> refused, in order)`: every command run, `exit <status> <argv>`, or `failed <argv>: <error>` for one that did not start (not installed), and every refused call, `refused <method> <what>: <why>`. A command's words are quoted for a POSIX shell (`shellWords`): a word of letters, digits and `_@%+=:,./-` as it is (but a first word with `=`), any other in single quotes, so that a line pasted into a shell runs what ran (vmtest.md §8.3). `host.FormatArgv`'s Go quoting would not: in `"--showformat=${Status}\\t${Version}\\n"` the shell expands `${Status}`.

   When the reading fails (detection, or Ctrl-C at any point, the header probe's included), only the evidence is printed: the facts read so far, `not read: <CODE> <message>`, and the commands. Tested: `TestEvidence` (Proxmox VE 9: the facts, four rows, the header, the band, the uptime factor's cost and its finding), `TestCheckError` (a check's error, its code, its `unknown` row and the risk's factor), `TestRunOverHosts`, `TestRefused`, `TestRefusedTolerated`, `TestReadFails`, `TestReadCancelledInHeader`, `TestClean` (a C1 control, U+202E, U+2066, U+2028, a tag character), `TestShellWords` (dpkg-query's format, an empty word, a quote, a first word with `=`).
3. **The reading is the gallery's** (uigallery's `readStatus`): detect's facts once (`engine.Options.Detect`, which keeps them), `Engine.Check` over the five areas' `Controls()` in Figma's order (`statusControls`: User, Firewall, SSH Login, SSH Auth, SSH Port, AppArmor, Auto Updates; *since `next/s2-menu-live` `controls.Status(hardn.yml)`, the main menu's one list, then DNS*), `header.Read` on the same facts, `risk.Assess` over both; each control is wrapped (`row`) only so that the guard knows which row's check made a call (§2.6), its `ID`, `Title`, `Applies` and `Check` its own. `Engine.Check` detects and checks, and nothing else (`pkg/engine/control.go`); the driver never plans or applies (a plan makes and removes its run directory, `/run/hardn/<id>`, [engine.md](engine.md) §2.6: each check package's own `Plan` test does, which the guard would refuse). Tested: `TestStatusControls` (the controls are `risk.Rows`, in order).

### Read-only by construction

4. **The guard** (`guard.go`) is the only `host.System` the reading gets: it wraps `host.Real` and decides every method of `host.System` in one table (`decisions`):
   - **passed**: `ReadFile`, `ReadFileNoFollow`, `ScanLines` (*since `next/s1-readers`*), `Stat`, `Lstat`, `ReadDir`, `Readlink`, `ResolveSymlink`, `LookupUser`, `LookupGroup`. `ReadFile` and `ReadDir` open what they are given (`os.ReadFile`, `os.ReadDir`), so the guard first asks what the path is, following symlinks as they would, and **refuses a special file** (a device, a FIFO, a socket) before it is opened: opening one can change the host (a watchdog device starts its timer when opened) or never return (a FIFO). The other reads open nothing, or look before they open (`ReadFileNoFollow`, `ScanLines`, `ResolveSymlink`, [system.md](../safety/system.md)). A path swapped for a special file between the look and the open is not caught; only root can swap what the checks read. A read still changes what any read changes: access times, as the mount's `relatime` keeps them, and a mount where the path is an automount point (a systemd automount of `/boot`, which the header probe lists) **[unverified]** on the VMs. Built with `CGO_ENABLED=0` (vmtest.md §8.1), `LookupUser` and `LookupGroup` are Go's own `os/user`, which reads `/etc/passwd` and `/etc/group` and asks no name service;
   - **refused**, with no call on the host: `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `Chmod`, `Chown`, `WriteFileAtomic`, `AppendFile`, `Lock` (it creates its file and writes a holder line), `StartDetached` (a process that outlives the run), `RunInput` (no check gives a command input), `RunInteractive` (*since `next/s1-readers`*: no check hands the terminal to a command);
   - **allowed**: `Run`, of the commands of §2.5 alone.

   The guard embeds no `System`, so a method added to `host.System` does not build until the guard has it, and `TestGuardDecidesEveryMethod` fails until `decisions` names it: the test calls every method of the interface through reflection over a fresh fake, and checks that a passed method makes the calls it names (for `ReadFile` and `ReadDir`, the look, then the read), a refused one none, and `Run` refuses `systemctl restart ssh.service` with no call and runs `systemctl is-active ssh.service`. The guard is not a `host.LegacySystem` (no in-place write to assert to). The engine's signal hold (`engine.Options.Hold`), which only an apply takes, is a refusal too. Tested: also `TestGuardIsNoLegacySystem`, `TestGuardReadsNoDevice`, `TestGuardReadsPassErrors`, `TestHold` (the hold refused, and listed with §5.2's line).
5. **The commands** (`allow.go`, `rules`): `argv[0]` exactly as the code passes it, and the arguments in exactly these shapes; anything else is refused. Every one is a query. Found by reading every `Run` call reachable from detect, the five checks and the probe (and their Phase 2 readers), and confirmed by a probe (not committed): every distinct command the seven packages' tests and the gallery's status tests ran, 40, is allowed.

   | Command | Arguments | Run by |
   |---|---|---|
   | `sshd`, `/usr/sbin/sshd` | `-T -f /etc/ssh/sshd_config`, or with `-C <connection>` (`user=…,host=…,addr=…` then `laddr`, `lport`, `rdomain` where set; no comma or white space in a value) | `sshd.ReadEffective`: sshd's test mode over the installed files, once per `Match` block with `-C` (SSH rows, Firewall) |
   | `/usr/sbin/sshd.pam`, `/usr/sbin/sshd.krb5` | as `sshd`, or `-f /etc/ssh/sshd_config -G` | `reload.OpenRCFlavour` (Alpine: which build the init script runs), then as `sshd` |
   | `systemctl` | `is-enabled` `ssh.socket`, `ssh.service`, `apparmor.service`, `apt-daily-upgrade.timer`, `apt-daily-upgrade.service`, `apt-daily.timer`, `apt-daily.service`; `is-active` `ssh.socket`, `ssh.service`, `apt-daily-upgrade.timer`, `apt-daily.timer`, `pve-firewall.service`, `proxmox-firewall.service`, *since `next/s2-menu-live`* `systemd-resolved.service`; `show -p Listen ssh.socket` | `detect.DetectSSH`; the AppArmor check (`atBoot`); the Auto Updates check (`timer`); `pve.ReadFirewall`; `sshd.ReadEffective` where the socket may listen; the DNS check (`dns.Read`, on a host systemd booted) |
   | `ss` | `-Htln` | `reload.ReadBound` (SSH Port, the SSH rows' staleness, Firewall) |
   | `ufw` | `status verbose` | `ufw.Read` (Firewall) |
   | `apt-config` | `dump APT::Periodic Unattended-Upgrade::Origins-Pattern Unattended-Upgrade::Allowed-Origins` | the Auto Updates check (`dump`) |
   | `dpkg-query` | `--show '--showformat=${Status}\t${Version}\n'` and one of `pve-manager`, `unattended-upgrades`, `linux-image-<release>` (a release's characters only, no pattern) | `detect.DetectProxmox`; the Auto Updates check; the header probe (`kernelReplaced`) |
   | `sudo` | `--version` | `detect.DetectElevation` |
   | `visudo` | `-c` | `sudoers.CheckInstalled` (User) |
   | `doas` | `-C /etc/doas.conf`, or `-C /etc/doas.d/<name>.conf` (one clean path) | `sudoers.CheckInstalled` (User) |
   | `ip` | `-4 addr show dev <interface>` (a name the kernel accepts: 1 to 15 bytes, no slash, colon or white space) | the header probe's address (`readIPv4`) |
   | `apk` | `info -e apk-cron` | `alpine.ReadUpdates` (Auto Updates) |
   | `rc-service` | `crond status` | `alpine.ReadUpdates` (Auto Updates) |
| `resolvectl` (*since `next/s2-menu-live`*) | `--no-pager dns`, `--no-pager domain` | `dns.Read` (DNS), where `/etc/resolv.conf` is systemd-resolved's and it runs: the servers and search domains in use |

   **Not run, so not allowed**: `uname` (the probe reads `/proc/sys/kernel/osrelease` and `version`), `hostname` (`/proc/sys/kernel/hostname`), `pveversion` (detect looks for it, and asks dpkg for `pve-manager`'s version), `aa-status` (the AppArmor check reads `/sys`), `rc-status` (OpenRC's runlevels are read from `/etc/runlevels`). The words are written out rather than taken from the packages' constants, so that the table is what runs; `TestRulesMatchTheCode` holds them to the constants that are exported (`sshd.MainConfig`, `reload.BinaryPAM`, `pve.FirewallUnit`, `alpine.Package`, `sudoers.DoasDir`, `header.KernelPackagePrefix`, …) and `sshd.DefaultConn`'s spec to the connection's shape. A check that comes to run another command, or another shape, is refused on the host (the run exits 1 and names it) and fails `TestRunOverHosts`, whose fixtures answer only what the checks run today (§2.10). Tested: `TestAllowed` (each shape, and near misses of each: `sshd -t`, an `-o` after `-C`, `systemctl restart`, another unit, `ss -K`, `ufw enable`, a `dpkg-query` pattern, `visudo -cf`, a `doas -C` path with `..` or another directory, `ip link set`, `apk add`, `rc-service crond start`, `uname -r`, …).
6. **A refusal is reported, and fails the run.** The refused call returns an error that wraps p4status's `errRefused` (a command as one that did not start, exit -1). **A row is `unknown` for it only where its check returns the error.** A check that carries on without a failing command keeps the state it reached without it: SSH Login and SSH Auth on `ssh.service` without `ss` ([check-ssh.md](check-ssh.md) §2.8a) stay `sshd -T`'s, which may show the host safer than a working `ss` would ("config not in effect"). So the guard records each call with the part of the reading that made it (`guard.within`: `detect`, the row's control ID, which `row.Check` sets, `header`; `Engine.Check` checks one control at a time, so the attribution is exact), and a refusal is shown wherever it may have changed a line: above the main menu, `== incomplete: <n> call(s) refused, in <parts>`, each part in the order of its first refusal; in the evidence, `incomplete <n> call(s) refused` first under each such part, whatever its state; among the commands, in order, with its reason; on stderr, counted; and the exit status is 1. Tested: `TestRefused` (a control added to the run that tries `systemctl restart ssh.service`, writes `00-hardn.conf` and takes the engine's lock: all three refused and listed, none reached the fake, the file not written, its row `unknown` with the refusal, named above the main menu and marked `incomplete 3 call(s) refused`), `TestRefusedTolerated` (`ss`'s rule taken out, over `hardened`: four refusals, Firewall and SSH Port `unknown`, SSH Login and SSH Auth still `configured`, all four named above the main menu and marked in the evidence).
7. **Over the fixtures, a full run only reads** (`TestRunOverHosts`, each of the five): exit 0 with nothing on stderr; every call the fake saw is a read or a `Run` the rules allow and the fixture answers; the evidence lists exactly the fake's commands, in its order, with their exit statuses (`failed` for one not installed); no refusal and no check error.

### Gates and exit status

8. **Linux, as root.** Another platform than Linux, and a user other than root, are refused (exit 2) before the host is built (`env.system`): the checks need root (sshd -T reads the host keys, the sudoers files are root's). A flag that is wrong or an argument exits 2 the same way; `-h` prints the usage and exits 0. There is no "disposable VM" gate as `p2drive` has: nothing is changed, so any host may be read. Exit status 0: printed, nothing refused; 1: a call refused, the reading failed (detection, Ctrl-C), or stdout could not be written; 2: as above. A check's error is part of the host's reading, printed on its row, and does not change the exit status. Ctrl-C and `SIGTERM` end the reading (`signal.NotifyContext`); `host.Real` kills the command it is running. `read` returns ctx's error after the header probe as well as after the checks, so a reading cut short in the probe is not scored and shown as a whole one. Tested: `TestGates`, `TestReadFails`, `TestReadCancelledInHeader` (Ctrl-C during the probe, after every check: no main menu, `not read: CANCELLED header: context canceled`, exit 1), `TestWriteFails` (a stdout that fails: `p4status: <error>` on stderr, exit 1).

### Never released

9. **As `p2drive`**: no build target of the makefile, the goreleaser configs or the workflows builds it (they build `./cmd/hardn` alone), a main package cannot be imported, and depguard's `dev-tools` rule keeps every package outside `internal/tools` from importing one. Two depguard rules of its own, the last of the list: `p4status-through-host` denies `os`, `io/ioutil`, `golang.org/x/sys`, `syscall`, `net` and `plugin` to every non-test file but `main.go`, which alone takes the process (stdout, stderr, the arguments, the user, the signals) and builds `host.Real`; `p4status-main` denies `os/exec`, `io/ioutil`, `golang.org/x/sys`, `net` and `plugin` to `main.go`. No file runs a command or makes a system call of its own. (Probed: a file of the package importing `os` and `os/exec`, and `main.go` importing `os/exec`, are three depguard issues.)

### The fixtures

10. **`testdata/hosts/*.json`** are the gallery's status hosts `debian13`, `ubuntu2604`, `alpine324`, `pve9` and `hardened`, as the gallery's model makes them, written by `testdata/export/export_test.go` (a test of the gallery's package that only `go test -overlay` builds, so that neither tool imports the other): every command one reading ran with the model's answer, in order; every file system entry it touched, the entries of the directories it listed, and the directories and link targets on their way; the setuid files (hostfake keeps no setuid bit; the users check counts only a tool that is setuid root); the accounts. [testdata/README.md](../../internal/tools/p4status/testdata/README.md) says how to write them again: when the gallery's model or a check changes, `TestMainMenuAsGallery` or `TestRunOverHosts` fails until they are. Synthetic data only (`web-01`, `192.0.2.20`, `alice`).

### Decisions of this unit

11. Each is this unit's; none changes what hardn does:
   - **The builder in `pkg/output`** (`MainMenuOf`, in a file of its own): `pkg/output` holds the main menu's mappings (`HeaderViewOf`, `StatusViewOf`), so the gallery, the driver and, with slice 2, hardn's live main menu share one function that fills the blocks the status model owns, and the driver's main menu cannot differ from the gallery's. It makes `pkg/output` import `pkg/ui/screen`, which adds that one package to the release binary's link (every package it imports is linked already): 1,753 bytes on linux/amd64 (`go build -trimpath -tags=netgo ./cmd/hardn`, with and without the function). The gallery's `mainMenu` now calls it; its goldens are unchanged. The alternative, a copy of the gallery's five lines in the driver, would let the two drift.
   - **The reading is copied**, not shared (*since `next/s2-menu-live` the list is shared, `controls.Status`; the reading stays this package's, over its guard*): `statusControls` and `read` repeat the gallery's (`statusControls`, `readStatus`), a third copy after the gallery and `pkg/engine/risk`'s tests. Sharing it needs an exported list of the status rows and a reading function, which [gallery-status.md](gallery-status.md) §7 leaves to slice 2; `TestStatusControls` and `TestMainMenuAsGallery` keep the copy honest until then.
   - **Its refusal has no registry code**: it is the driver's, not hardn's output contract, and `pkg/output` cannot import a main package; a row's error that wraps it shows as `UNCLASSIFIED_ERROR`, with its message, and the evidence names every refusal.
   - **The commands are listed by shape, not by name**: a command name alone would let `systemctl restart` through.
   - **Special files are refused** to `ReadFile` and `ReadDir` (§2.4), though no check reads one on a stock host: a symlink in a configuration directory can lead to one.
   - **A refusal marks the rows it was made for; it does not hide the main menu or change a row** (§2.6): the main menu stays hardn's drawing of what was read, so it can be compared with the gallery's and with the host, and the line above it and the evidence's `incomplete` lines say which rows may be wrong. Making those rows `Unknown` would draw a main menu hardn never draws; leaving the main menu out would hide the rows a refusal did not touch.
   - **The commands are quoted for a shell, not by `host.FormatArgv`** (§2.2): vmtest.md §8.3 has a person run each one by hand.

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| The commands detect, the five checks and the header probe run, and their shapes | the code, by reading every reachable `sys.Run` (`pkg/host/detect`, `sshd`, `reload`, `ufw`, `pve`, `alpine`, `sudoers`, `pkg/engine/controls/*`, `pkg/engine/header`); a probe over their tests and the gallery's status tests (40 distinct commands, all allowed) | §2.5 |
| `Engine.Check` only detects and runs each control's `Check`; a check-only control's `Plan` returns `ErrNotMigrated` | `pkg/engine/control.go`; [checks.md](checks.md) §1 | §2.3 |
| No package of hardn's that p4status links has an `init` function | `go list -deps ./internal/tools/p4status`, and a search of their files | §2.4: none of hardn's code reaches the host before the guard |
| `os.ReadFile` and `os.ReadDir` open the path they are given; `host.Real.ReadFileNoFollow` checks what it is before it opens it | `pkg/host/real.go` (`ReadFile`, `ReadDir`, `ReadFileNoFollow`, `openNoFollow`) | §2.4 |
| Opening a watchdog device starts its timer, which reboots the host unless it is fed | Linux `Documentation/watchdog/watchdog-api.rst` **[unverified]**: from memory, not read here | §2.4: special files refused |
| `sshd -T` and `-G` parse the configuration, print it and exit; `-T` needs root (host keys) and does not create `/run/sshd` | sshd(8) **[unverified]** here; [sshd-validate.md](../safety/sshd-validate.md) (`-T` and `/run/sshd`), [reload.md](../safety/reload.md) (Alpine's `-G`) | §2.5, §2.8 |
| `systemctl is-enabled`, `is-active` and `show` only query; `ss -Htln` lists listening sockets; `ip addr show` lists addresses; `apt-config dump` prints the configuration; `dpkg-query --show` reads dpkg's database; `sudo --version` prints the version; `visudo -c` checks the sudoers files ("check-only mode"); `doas -C` parses a file and exits; `apk info -e` and `rc-service <service> status` query | their manual pages **[unverified]** here, read by the checks' units ([detect.md](../safety/detect.md), [reload.md](../safety/reload.md), [check-updates.md](check-updates.md), [sudoers.md](../safety/sudoers.md), [alpine.md](../safety/alpine.md), [header.md](header.md)) | §2.5 |
| `ufw status verbose` changes nothing: no lock file, no rule, no module loaded | **[unverified]**: an active ufw lists its chains through iptables or nft; whether `ufw` takes a lock or loads a module for a status is not checked | §2.5; vmtest.md §8.4 records it |
| Without cgo, Go's `os/user` parses `/etc/passwd` and `/etc/group` itself | the `os/user` package documentation **[unverified]** here | §2.4 |
| Reading a path under an automount point mounts it | autofs and `systemd.automount(5)` **[unverified]** here | §2.4 |
| `term.Resolve` makes a terminal's profile of its `TERM`, `COLORTERM` and `--color`; plain for output that is not a terminal | `pkg/ui/term/resolve.go` | §2.1 |
| The gallery's model of its status hosts, and the formats it answers in | [gallery-status.md](gallery-status.md) §2.3, §3 | §2.10 |

## 4. Written, not run (gate 5)

Nothing here has run on Linux, as root, or over `host.Real`. On the maintainer's VMs, [vmtest.md](../safety/vmtest.md) §8: build, copy, run with sudo, and compare each line of the main menu and the evidence with the host by hand. What that run shows first:

- **no refusal** on Debian 13 and Ubuntu 26.04 as they are (a refusal is a command or shape the checks run that §2.5 forgot: the run names it);
- each command's exit status as the checks expect it on a real host (`systemctl is-*` exiting non-zero for a state that is not "on"; `dpkg-query` exit 1 for a package not installed; sudo-rs's `visudo -c` on Ubuntu 26.04, **[unverified]**);
- that the commands change nothing: `ufw status verbose` (§3), and the host's files and units before and after the run;
- every row, header fact and factor against its independent command (vmtest.md §8.3), which is gate 5 for the checks' pages too (each page's §4).

Not on the VMs: Alpine 3.24 and Proxmox VE 9 (no VM), so their lines stay the fixtures' (`TestRunOverHosts`); the `-G` shapes of `sshd.pam` and `sshd.krb5` run only on Alpine with those builds.

## 5. The lines

### 5.1 The main menu's

**None is new.** Every line is a check's, the probe's or the score's, with its host state and exact strings in its page's table, all proposed there: [check-users.md](check-users.md), [check-firewall.md](check-firewall.md), [check-ssh.md](check-ssh.md) §5, [check-apparmor.md](check-apparmor.md) (the eight agreed lines, P-43 (3)), [check-updates.md](check-updates.md), [header.md](header.md) §5, [risk.md](risk.md) §6. Over the fixtures the driver shows the gallery's lines for those hosts ([gallery-status.md](gallery-status.md) §5: `debian13`, `ubuntu2604`, `alpine324`, `pve9`, `hardened`).

### 5.2 The driver's own (*proposed*)

| When | Stream | Exact words |
|---|---|---|
| Not Linux | stderr, exit 2 | `p4status: refused: it reads a Linux host, and this is <goos>` |
| Not root | stderr, exit 2 | `p4status: run as root` |
| An argument | stderr, exit 2 | `p4status: unexpected arguments [<args>]`, then the usage |
| A wrong `-profile` | stderr, exit 2 | `p4status: -profile "<value>" is not plain, none, 16, 256 or truecolor` |
| `-width` or `-height` below 1 | stderr, exit 2 | `p4status: -width <w> and -height <h> must be positive` |
| A call refused | stderr, exit 1 | `p4status: <n> call(s) refused; the evidence lists them` |
| A call refused | stdout, above the main menu | `== incomplete: <n> call(s) refused, in <parts>` (`<parts>`: a row's control ID, `detect`, `header`, joined by `, `) |
| A part of the reading that made one | evidence, first under its heading | `incomplete <n> call(s) refused` |
| A refused call's error, in its row's `error` line | evidence | `p4status: <argv>: <why>: refused`, or `p4status: <method> <path>: <why>: refused` |
| The reading failed | stderr, exit 1 | `p4status: the host was not read: <error>`; in the evidence `not read: <CODE> <message>` |
| The evidence starts | stdout | `== p4status evidence: read only; check each line against the host (docs/safety/vmtest.md §8)` |
| Where `hardn.yml` was read (*since `next/s2-menu-live`*) | evidence, first, under `hardn.yml` | `file       <path>`, or `file       none found: hardn's defaults, no nameservers` |
| `hardn.yml` does not load (*since `next/s2-menu-live`*) | stderr, exit 1 | `p4status: the host was not read: hardn.yml: config: <path>: …`, as hardn refuses to start with it |
| A command not allowed | evidence | `refused    Run <argv>: not a command the checks run` |
| A change | evidence | `refused    <method> <path>: it changes the host` |
| `RunInput`, `StartDetached` | evidence | `refused    RunInput <argv>: a command with input`, `refused    StartDetached <argv>: a detached process` |
| A special file | evidence | `refused    ReadFile <path>: a device, FIFO or socket` (or `ReadDir`) |
| The engine's hold | evidence | `refused    Hold the engine's signal hold: only an apply takes it` |
| A command not installed | evidence | `failed     <argv>: <error>` |
| The running kernel's owner | evidence, `reboot` | `pending <yes, no or unknown>; running <release>; own kernel`, or `…; container host's kernel` |

The headings and labels are the evidence's (§2.2): `facts (detect)`, `row <id> (<title>)`, `header`, `risk`, `commands (<n> run, <m> refused, in order)`; `incomplete`, `state`, `detail`, `evidence`, `finding`, `error`, `exit <n>`, `refused`, `failed`. Shortened in review (§8), still proposed: the root gate (was `p4status: refused: run it as root; sshd -T and the sudoers files need root`), the refusal's last words (was `refused by p4status, which changes nothing`), the special file's reason (was `not a regular file or a directory, which opening may change`), the kernel's owner (was `host's kernel false`, which read as "not this host's kernel" on a VM).

## 6. API

```text
sudo ./p4status [-profile plain|none|16|256|truecolor] [-width 80] [-height 24]
  stdout: the main menu, a blank line, the evidence
  exit 0 printed, nothing refused; 1 a call refused, the host not read, or stdout not written;
  2 a flag or an argument, not Linux, not root
```

```go
package output // pkg/output, added
func MainMenuOf(base screen.MainMenu, h header.Header, reports []engine.Report, l risk.Level) screen.MainMenu

package main // internal/tools/p4status (new)
type env struct { goos string; euid func() int; stdout, stderr io.Writer; system func() host.System; controls func() []engine.Control }
// next/s2-menu-live: controls func(*config.Config) []engine.Control (controls.Status); configs []string (configPaths)
func run(ctx context.Context, args []string, e env) int
type guard struct{ /* … */ } // host.System over another; newGuard(sys) *guard
var decisions map[string]decision // pass, refuse, allow: every method of host.System
func allowed(argv []string) bool  // rules: §2.5
var errRefused error
func statusControls() []engine.Control // removed by next/s2-menu-live: controls.Status
type reading struct { facts detect.Facts; reports []engine.Report; header header.Header; level risk.Level }
func read(ctx context.Context, g *guard, controls []engine.Control) (reading, error) // the hold is g.hold
func (g *guard) within(part string) // the part of the reading the calls from now on are recorded as
func mainMenu(env term.Env, r reading) string
func incomplete(calls []call) string // the line above the main menu; "" with nothing refused
func shellWords(argv []string) string
func renderEnv(profile string, width, height int) (term.Env, error)
func evidence(r reading, readErr error, calls []call) string
```

`internal/tools/uigallery`: `hostStatus.mainMenu` calls `output.MainMenuOf`; nothing else in the gallery changed.

## 7. Not in this unit; inputs to later units; size

- **The VM run itself** ([vmtest.md](../safety/vmtest.md) §8) and its results, which go in vmtest.md once run, with synthetic names.
- **Slice 2** (the main menu live): one exported list of the status rows' controls and one reading (detect once, check, probe, score), which the gallery, `pkg/engine/risk`'s tests and this driver each copy now (§2.11); `cmd/hardn` should call `output.MainMenuOf` with its own items over the same reading. When it does, the reading here should follow it.
- **A check that runs a new command** must add its shape to `rules` in the same change, or the driver refuses it on a host (§2.5).
- **For the maintainer:** whether `ufw status verbose` may run on a host where ufw takes a lock or loads a module for it (§3), which the VM run records.
- **Size:** about 990 lines of non-test Go in the driver (`guard.go` 275: a method per method of `host.System`, about 60 lines of comments; `allow.go` 140, the rules and their shapes; `evidence.go` 285; `main.go` 160, about 45 of them the package doc; `read.go` 135), 26 in `pkg/output`, 39 of `.golangci.yml` and 4 changed in the gallery; tests about 890 lines, the exporter 195, and about 80 KB of fixtures. Of these, the review round (§8) added about 135 lines of code and 125 of tests. Over the guideline of about 500 changed lines because the guard decides 21 methods, the allowlist is 15 commands with their shapes, and the evidence prints every field a person checks; it could have been split as the guard and the allowlist first, then the printing.

## 8. Review fixes

What the review of 2026-10-21 found, and what changed (each in its section above):

- **A refused call could leave a row `Configured` under a main menu that looked complete** (§2.6): with `ss` refused, SSH Login and SSH Auth on `ssh.service` stayed `sshd -T`'s, as their check does without `ss`, and §2.6 said the opposite. Now every call is recorded with the part of the reading that made it; a line above the main menu names those parts, and the evidence marks each one `incomplete`, whatever its state. §2.6 says a row is `unknown` only where its check returns the error. The main menu is still printed (§2.11).
- **Ctrl-C during the header probe exited 0 with a main menu** (§2.8): `read` now returns ctx's error after the probe, so no main menu and exit 1.
- **vmtest.md §8.3 checked SSH Login and SSH Auth against the files the check reads**: the running daemon's start and last `SIGHUP` against the files' mtimes, and from another host the methods the running sshd offers; a file newer than both is recorded as not established in effect.
- **vmtest.md §8.3's criteria**: the AppArmor count is the root namespace's `enforce` and `kill` profiles; SSH Login, SSH Auth and the address name the checks' exceptions (§2.8a's "config not in effect", a Match block that lets in less, a route's preferred source and policy routing); the risk score is risk.md §5.2's formula; the commands paste as they ran.
- **vmtest.md §8.4's "before and after" had no "before"**: the snapshot commands are now in §8.2, ahead of the run, and §8.4 diffs them.
- **The evidence's commands were Go-quoted** (§2.2): pasted, dpkg-query's `${Status}` expanded. They are now quoted for a POSIX shell.
- **Evidence**: `like` on the platform line and `etc-pve` on the Proxmox VE line; the reboot line's `host's kernel false`, which read backwards on a VM, is now `own kernel` or `container host's kernel`; format and line-separating characters (U+202E, U+2066 to U+2069, U+2028) are escaped as control characters are.
- **Tests** for the engine's hold refused and for a stdout that fails (`TestHold`, `TestWriteFails`), and for each fix above (`TestRefusedTolerated`, `TestReadCancelledInHeader`, `TestShellWords`, `TestClean`, `TestEvidence`).
- **The driver's own words** (§5.2) shortened, still proposed: `p4status: run as root`; a refusal ends `: refused`; a special file is `a device, FIFO or socket`.
