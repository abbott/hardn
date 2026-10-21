# Auto Updates: the Auto Updates status row (`pkg/engine/controls/updates`)

> **Unit:** refactor plan Phase 4, `next/p4-check-updates` (plan §2, row `p4-check-<area>`, Updates: "unattended-upgrades, or apk-cron + crond"; brief Phase 4, "Where each status comes from"; brief 2.5, Alpine's `apk-cron`; brief §3 principle 3). Sources: [P-43](../refactor/proposals.md#p-43)'s research of 2026-10-20, (3) "Debian, Ubuntu, Proxmox VE: Auto Updates" and "Alpine: Auto Updates", and (6) decision 5 (Proxmox VE, **open**). It follows the pattern of [checks.md](checks.md) and takes up the "Status sources" item of [docs/safety/README.md](../safety/README.md) for updates and [alpine.md](../safety/alpine.md) §6's `p4-check-updates` item (`alpine.ReadUpdates`). V-numbers: none covers origin/main's Auto Updates row (§1); V23 (Run All's claims) stays slice 7's and Run All's.
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only, with `apt-config dump`'s output as synthetic fixtures and `dpkg-query`, `systemctl`, `apk` and `rc-service` answered as each case sets them (§3). Nothing has run on Linux: everything host-specific is "written, not run (gate 5)" (§4). **Every detail and finding detail below is proposed wording**, for the maintainer's review (§5, §6).

Figma's main menu has an Auto Updates row ("Not Configured" in `00-TUI-MAIN.png`, with no detail). It is now a check-only control, `updates.auto`, titled "Auto Updates", whose `Check` reads whether the host upgrades its packages by itself: on Debian, Ubuntu and Proxmox VE whether apt's daily job runs `unattended-upgrade`, read as `apt.systemd.daily` decides it from the effective `APT::Periodic` values, with the package and the systemd timers that start the job; on Alpine apk-cron's daily script and crond, through `alpine.ReadUpdates`. `Plan` returns `engine.ErrNotMigrated` until slice 7, which builds both directions (P-43 (1)).

## 1. Why

origin/main's row (`checkUnattendedUpgrades`, `pkg/security/status.go:414-437`) is Configured on Debian and Ubuntu when `dpkg -l unattended-upgrades` and `systemctl is-enabled unattended-upgrades` both exit 0. The first holds for a package removed with its files kept, which dpkg still lists (**[I]**, dpkg-query(1)'s exit status); the second asks about `unattended-upgrades.service`, which only lets an upgrade in progress finish at shutdown (`unattended-upgrade-shutdown --wait-for-signal`, §3), not the job that upgrades. Neither reads `APT::Periodic` or apt's timers, so a host with `Unattended-Upgrade "0"`, or with `apt-daily-upgrade.timer` disabled, shows Configured (by code reading, not run). On Alpine the row is whether origin/main's own `apk-upgrade` exists ([alpine.md](../safety/alpine.md) §1). No state is Unknown. A status that says a host is updating itself when it is not is the worst error of this phase (brief Phase 4: prefer Unknown to guessing). The legacy row stays as it is until the main menu goes live (plan, "Phases 3–5 together").

## 2. Guarantees

1. **The line describes the host now** (decision of this unit, as [check-apparmor.md](check-apparmor.md) §5.2 decided "logging only" over "off after restart"): **Configured** is the upgrade job running now and after the next boot, with apt's package lists refreshed; **Partial** is upgrades running with a piece missing: the lists not refreshed, or a timer that runs now and does not start at the next boot ("off after restart"); **NotConfigured** is no upgrade running now, whichever piece stops it; **Unknown** is "can't read". [checks.md](checks.md) §1.3 reads `Partial` as "some of it": here a piece missing that stops every upgrade (the package there, its timer off) is `NotConfigured`, since the host gets no update (for the maintainer, §6 (a)).
2. **Which tool** is detect's distribution: Alpine reads apk-cron (§2.8); Debian, Ubuntu, Proxmox VE, a distribution hardn does not list whose `ID_LIKE` names Debian or Ubuntu (`DistroOther` with `Like`), and Debian whose Proxmox VE is not established (apt either way) read apt; another distribution hardn does not list is `Unknown` with `DETECT_UNRECOGNISED`, and one not established with detect's error. `Applies` is true on every host: a container upgrades its own packages, and leaving a host whose tool hardn does not read out of the risk level would understate it.
3. **The effective values, never the files** (brief §3 principle 3): one command, `apt-config dump APT::Periodic Unattended-Upgrade::Origins-Pattern Unattended-Upgrade::Allowed-Origins`, which prints the configuration apt itself built from every file, in the order and with the overrides apt applies (§3). Its environment is `host.Real`'s (`PATH` and `LC_ALL=C`, nothing inherited), so `APT_CONFIG` is unset and the files read are the host's own. Names are compared without case, as apt compares them; the dump prints each name as the file that first set it spelt it. Each line must be `name "value";`, apt-config's default format; a line in another form, or an item printed twice, is a form hardn does not know (`DETECT_UNRECOGNISED`). A dump that prints nothing (none of the subtrees set) is every value unset.
4. **Each value ranked as `apt.systemd.daily` reads it** (§3), and no other way:
   - `APT::Periodic::Enable`: unset is on (the script's default 1); a decimal is ranked, `0` (or `00`, since the script tests `-eq 0`) off, any other on; another form ("yes", "") is not ranked.
   - `APT::Periodic::Unattended-Upgrade` and `APT::Periodic::Update-Package-Lists`, intervals: unset is `0` (the script's default) and `0` never runs; `always`, a decimal (days) and a decimal with one of `s`, `m`, `h`, `d` run (`0d` and `00` at every start of the job, as `check_stamp` reads them: P-43 (3)'s "`"0d"` counts as on"); another form ("1w", "weekly", "") is not ranked.
   - A value not ranked is `Unknown` with the finding `UPDATES_APT_VALUE_UNKNOWN` naming it (`Unattended-Upgrade "1w"`), no error ([checks.md](checks.md) §1.5's second kind), where the line depends on it.
5. **The package** `unattended-upgrades` is `detect.DpkgInstalled`, the rule detect reads `pve-manager` with: installed, or not (`not-installed`, `config-files`, no such package, no `dpkg-query`); any other status (`half-installed`, `unpacked`, ...) is `DETECT_UNRECOGNISED`, and a `dpkg-query` that fails otherwise its error. The package installed is taken as `unattended-upgrade` on the job's `PATH`, which the script checks with `command -v` (§6, not modelled).
6. **The timers and their services**, on a host systemd booted (detect's `Init`): for `apt-daily-upgrade` (upgrades: `apt.systemd.daily install`) and `apt-daily` (lists: `apt.systemd.daily update`), `systemctl is-active <name>.timer` (running now: `active`; not: `inactive`, `failed`), `systemctl is-enabled <name>.timer` (at the next boot: `enabled`; not: `disabled`, `masked`, `linked`, `not-found`) and `systemctl is-enabled <name>.service` (can be started: any documented state but `masked`, `masked-runtime`, `not-found` and `bad`), through detect's `SystemctlIsActive` and `SystemctlIsEnabled`, which run that verb only. A timer's other states (`activating`, `maintenance`, ...; `enabled-runtime` and the other `-runtime` states, `static`, `indirect`, `generated`, `transient`, `alias`, `bad`, which say nothing of the next boot, as [check-apparmor.md](check-apparmor.md) §2.8 reads them) and a service's `bad` are `Unknown` with `UPDATES_SCHEDULE_UNKNOWN`; a state systemctl(1) does not list is detect's `DETECT_UNRECOGNISED`. **Masking the services** is a common way to stop the upgrades while the timers keep running, so a masked `apt-daily-upgrade.service` is `NotConfigured`, "upgrade job masked", and a masked `apt-daily.service` "lists not refreshed".
   Off systemd, apt's cron job `apt-compat` runs the job instead; it is not read (cron's own state is not a fact detect establishes), so every timer fact is `Unknown` with `UPDATES_SCHEDULE_UNKNOWN`, "no systemd timers"; an init system not established is detect's error.
7. **The order** (§5.2): the facts the upgrade depends on decide `NotConfigured` first (the package, `Enable`, `Unattended-Upgrade`, the upgrade timer running, its service); then the lists and the next boot decide `Partial`. Every fact is read before the line is decided, and **a state the facts read establish is reported whatever else could not be read** ([checks.md](checks.md) §1.5): the package not installed is `NotConfigured` whether or not `apt-config` or `systemctl` answered. A fact not established decides only when nothing in its tier is established `No`: then the line is `Unknown`, with the errors and the findings of the facts left open, each once. Within the second tier, a `No` decides `Partial` with its own detail even when a fact before it in the order is open: the state holds either way.
8. **Alpine** reads `alpine.ReadUpdates`, `next/p2-alpine`'s reader, whose facts are its own (apk-cron installed by `apk info -e`, its daily script an executable regular file, crond's `rc-service` status, its link in the default runlevel). **Configured is `next/p2-alpine`'s rule** (`Updates.Status`: package AND script AND runlevel AND running). The other lines follow §2.1, which **departs from `Updates.Status`** (decision of this unit, §6 (b)): apk-cron not installed is `NotConfigured`, "no apk-cron", as there; its script not run, and crond stopped, crashed or not installed, are `NotConfigured` here (Partial there), since nothing upgrades; crond started and out of the default runlevel is `Partial`, "off after restart", as there. A fact `ReadUpdates` could not establish decides only as in §2.7. The departure is never kinder than `Updates.Status`: `Configured` and `Partial` only where it says so, its `NotConfigured` kept (`TestNoKinderThanAlpine`, over every combination of package, script, crond and runlevel). `ReadUpdates`' refusals are `Unknown` with their codes: OpenRC not booting the host (the Alpine container image) `ALPINE_NO_OPENRC`, as [alpine.md](../safety/alpine.md) §2.1 decided; an init system not established `ALPINE_UNKNOWN`. apk-cron's `apk -U upgrade` refreshes the index itself, so Alpine has no "lists not refreshed".
9. **Proxmox VE is reported as it is** (P-43 (6) decision 5 is the maintainer's and **open**: NotApplicable with a note, on with a warning, or on with Proxmox's origin added). It reads as Debian. Where upgrades run (`Configured` or `Partial`) and no entry of `Unattended-Upgrade::Origins-Pattern` or `Allowed-Origins` names `Proxmox`, the line carries `UPDATES_PROXMOX_NOT_UPGRADED`, "Proxmox packages not upgraded": Debian's default origins never match Proxmox's packages, whose Release files say `Origin: Proxmox` (P-43 (6) decision 5, [V] there), so the kernel and Proxmox VE itself are left to `apt full-upgrade` by hand. An entry naming `Proxmox` is taken to cover them; it is not matched as unattended-upgrades matches a pattern (§6). Where Proxmox VE is not established, the origins are not compared, and the evidence says so. The state is not changed by the finding: deciding what Auto Updates means on Proxmox VE is decision 5's; if the maintainer picks (a), `Applies` becomes false on Proxmox VE, with the note as its reason, and the finding goes.
10. **`Unknown` is always "can't read", in two kinds** ([checks.md](checks.md) §1.5). A read that fails, or a form neither the command nor apt documents, is the error, which `Engine.Check` reports (exit 1): `COMMAND_NOT_FOUND`, `COMMAND_FAILED`, `DETECT_UNRECOGNISED`, `PERMISSION_DENIED`, detect's codes, `ALPINE_*`. A documented value the control does not rank is a finding with no error (exit 3): `UPDATES_APT_VALUE_UNKNOWN`, `UPDATES_SCHEDULE_UNKNOWN`. No `Unknown` path returns another state.
11. **Findings**: this package's three codes, exported constants registered in `pkg/output` (appended, `codes.golden` regenerated, a row each in [docs/reference/output.md](../reference/output.md) §2.2), and on Alpine `ReadUpdates`' two, `ALPINE_LEGACY_SCRIPT` and `ALPINE_FOREIGN_SCRIPT`, coded by `steps.FindingCode` and `steps.SourceAt` as a plan codes them, with this unit's short details ("old apk-upgrade script", "other apk-upgrade file") rather than the primitive's, which describe what a plan does (decision of this unit, §6 (h)). They explain a state; none sets one.
12. **Evidence** is each fact as it was read, one line per source: `apt-config dump: APT::Periodic::Unattended-Upgrade "1"`, or `... unset`; `dpkg-query --show unattended-upgrades: installed` (`not installed`); `systemctl is-active apt-daily-upgrade.timer: active`, `systemctl is-enabled apt-daily-upgrade.timer: enabled`, `systemctl is-enabled apt-daily-upgrade.service: static`, the same for `apt-daily`; `init: openrc, running yes: no systemd timers`; on Proxmox VE `apt-config dump: 3 origins, none naming Proxmox`; on Alpine `ReadUpdates`' own lines. A read that failed has no line; its error says what. Tested: `TestEvidence`.
13. **Read-only, through `host.System`.** On a host apt manages the check runs exactly `apt-config dump` with its three roots, `dpkg-query --show --showformat=... unattended-upgrades`, and on systemd six `systemctl` queries (`is-active` once, `is-enabled` twice, per timer); on Alpine `ReadUpdates`' reads (`apk info -e apk-cron`, `rc-service crond status`, and `lstat`, `stat` and `ReadFileNoFollow` of its files). Nothing else, and none of them changes the host (`readOnly` in every test). depguard rule `controls-through-host` ([checks.md](checks.md) §1.11) covers the package; no rule was added.
14. **`Plan` returns `engine.ErrNotMigrated`** (`engine.CheckOnly`), and `Engine.Plan` over `Controls()` is refused, its item's error coded `CONTROL_NOT_MIGRATED`, with no change (`TestCheckOnly`).

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| `apt.systemd.daily` reads `APT::Periodic::Enable` into `AutoAptEnable` (default 1) and exits when `[ $AutoAptEnable -eq 0 ]`; each interval defaults to 0 before its `apt-config shell` line; `check_stamp` succeeds for `always` and a missing stamp, fails for `0`, and otherwise takes `s`, `m` and `h` suffixes and days (a bare number, or `d`); it has an `update` and an `install` branch by `$1` (both with no argument); `apt-get update` runs when `check_stamp` passes for `UpdateInterval`; `unattended-upgrade` runs only when `command -v unattended-upgrade` succeeds and `check_stamp` passes for `UnattendedUpgradeInterval`, whatever the update did | apt 3.0.3 and 2.6.1 `debian/apt.systemd.daily` (salsa.debian.org apt-team/apt), read 2026-10-20; P-43 (3) | §2.4, §2.6 |
| `apt-daily.service` runs `/usr/lib/apt/apt.systemd.daily update`, `apt-daily-upgrade.service` runs it with `install`, both `ConditionACPower=true`; `apt-daily-upgrade.timer`: `OnCalendar=*-*-* 6:00`, `RandomizedDelaySec=60m`, `Persistent=true`, `WantedBy=timers.target` | apt 3.0.3 `debian/apt-daily.service`, `apt-daily-upgrade.service`, `apt-daily-upgrade.timer`, read 2026-10-20; the units of apt 2.6.1 (Debian 12) **[unverified]** beyond P-43's citation | §2.6 |
| apt's cron job `apt-compat` exits when `/run/systemd/system` is a directory, and otherwise execs `apt.systemd.daily` | apt 3.0.3 `debian/apt.apt-compat.cron.daily`, read 2026-10-20 | §2.6 (off systemd) |
| `apt-config dump` prints with `%F "%v";\n` (`APT::Config::Dump::Format`); each argument after `dump` is a root whose subtree is printed, root first, in pre-order; a list item's name is its parent's with `::`; a root not set prints nothing; `%v` is the value as it is | apt 3.0.3 `cmdline/apt-config.cc` (`DoDump`), `apt-pkg/contrib/configuration.cc` (`Configuration::Dump`), read 2026-10-20 | §2.3 |
| Names are case-insensitive; a value is on one line, with no backslash or extra quotation mark; later reads replace earlier scalar values | apt.conf(5), manpages.debian.org trixie, read 2026-10-20 | §2.3 |
| `apt-config` exits 100 with `E: Syntax error ...` on a file it cannot parse | **[I]**; any exit other than 0 is an error here | §5.3 |
| `host.Real` runs every command with `PATH` and `LC_ALL=C` only | this repo, `pkg/host/real.go:45-48` | §2.3 (`APT_CONFIG` unset) |
| unattended-upgrades' postinst turns the job on from a debconf answer that defaults to true (`20auto-upgrades`: `Update-Package-Lists "1"`, `Unattended-Upgrade "1"`); Ubuntu has it on as installed; a Debian host from debian-installer has no package; Proxmox VE 9 probably ships without it | P-43 (3) ([V] the postinst and Ubuntu's docs; **[I]** Debian's preseed and Proxmox VE) | the fixtures, `TestStock` |
| `unattended-upgrades.service` is "Unattended Upgrades Shutdown", `ExecStart=/usr/share/unattended-upgrades/unattended-upgrade-shutdown --wait-for-signal` | upstream unattended-upgrades master `debian/unattended-upgrades.service` (GitHub mvo5/unattended-upgrades), read 2026-10-20; Debian 13's copy **[unverified]** | §1 |
| `unattended-upgrade` does not refresh the package lists itself, so without `Update-Package-Lists` (or another `apt update`) it finds no new updates | P-43 (3) **[I]** | §2.1 ("lists not refreshed") |
| The states `systemctl is-enabled` and `is-active` print, as detect accepts them; `-runtime` states are links in `/run`, for this boot only | [detect.md](../safety/detect.md) (systemctl(1)); [check-apparmor.md](check-apparmor.md) §3 | §2.6 |
| A started timer reads `active` until it is stopped; a timer whose service is masked still elapses and starts nothing | **[I]** | §2.6 |
| Proxmox VE's repositories' Release files say `Origin: Proxmox`, which Debian's default origins never match; Proxmox's guide upgrades with `apt full-upgrade` | P-43 (6) decision 5 ([V] there: download.proxmox.com `dists/trixie/Release`; pve-admin-guide "System Software Updates") | §2.9 |
| Debian's default origins take stable point releases as well as security updates | P-43 (7) ([V] there: unattended-upgrades `debian/NEWS` 0.99) | §6 (not modelled) |
| `dpkg-query --show` exit 1 is "no packages found"; the status's third word is the package's state | [detect.md](../safety/detect.md) (`pve-manager`'s rule, dpkg-query(1)) | §2.5 |
| apk-cron's `/etc/periodic/daily/apk` runs `apk -U upgrade` (the index refreshed); crond's status exit codes; the default runlevel link | [alpine.md](../safety/alpine.md) §3 | §2.8 |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root. On the VMs of [vmtest.md](../safety/vmtest.md) (Debian 13, Ubuntu 26.04, Alpine 3.24) and by hand on Proxmox VE 9, through a dev-only driver or the gallery's status scenario once one calls `Engine.Check` with this control:

- each stock host: the line as installed, and `apt-config dump`'s exact output for the three roots (the order, the names' case, and the fixtures' items) against `testdata/`;
- Debian 13: `apt install unattended-upgrades` ("unattended-upgrades"); `dpkg-reconfigure -plow unattended-upgrades` answering No ("upgrades off"); `systemctl disable --now apt-daily-upgrade.timer` ("upgrade timer off"); `systemctl disable apt-daily-upgrade.timer` alone ("off after restart"); `systemctl mask apt-daily-upgrade.service` with the timer running ("upgrade job masked") and what `is-active` prints for that timer then; `systemctl stop apt-daily.timer` ("lists not refreshed"); a file setting `APT::Periodic::Unattended-Upgrade "1w"` (the finding); a file with a syntax error (`apt-config`'s exit status and message); `apt remove unattended-upgrades` (`config-files`, "no unattended-upgrades");
- Ubuntu 26.04: stock ("unattended-upgrades"), the same recipes, and the check as a user other than root (the same line: nothing read needs root **[I]**);
- Proxmox VE 9: stock ("no unattended-upgrades"); with unattended-upgrades installed ("unattended-upgrades" and "Proxmox packages not upgraded"); with `origin=Proxmox` added to `Origins-Pattern` (no finding); whether apt's timers are enabled on its stock install;
- Alpine 3.24: stock ("no apk-cron"), after `alpine.Apply` ("apk-cron"), `rc-service crond stop` ("crond not running"), `rc-update del crond default` with crond running ("off after restart"), and an `alpine:3.24` container (`ALPINE_NO_OPENRC`);
- that an upgrade really follows on a host shown "unattended-upgrades" (`/var/log/unattended-upgrades/` after the timer's run), and none on one shown "upgrade timer off" or "upgrade job masked";
- the time `Check` takes (three commands and six queries, or `ReadUpdates`' two commands and its file reads).

## 5. The status lines

### 5.1 Every line (proposed)

In Figma's grammar (marker, label, the state's word in its colour, a faint detail). **Every detail is proposed wording**; Figma draws the row with no detail. "off after restart" is AppArmor's agreed detail (P-43 (3)), reused for the same meaning.

```text
   ▽  Auto Updates         Configured unattended-upgrades
   ▽  Auto Updates         Configured apk-cron
   ⛬  Auto Updates         Partial lists not refreshed
   ⛬  Auto Updates         Partial off after restart
   ○  Auto Updates         Not Configured no unattended-upgrades
   ○  Auto Updates         Not Configured upgrades off
   ○  Auto Updates         Not Configured upgrade timer off
   ○  Auto Updates         Not Configured upgrade job masked
   ○  Auto Updates         Not Configured no apk-cron
   ○  Auto Updates         Not Configured daily script not run
   ○  Auto Updates         Not Configured crond not running
   ?  Auto Updates         Unknown can't read
```

Each detail fits the room beside its state at 64 columns ([checks.md](checks.md) §1.9; "no unattended-upgrades" is exactly the 22 cells; `TestDetails`).

| The host | State | Detail |
|---|---|---|
| apt: unattended-upgrades installed; `Enable` unset or not 0; `Unattended-Upgrade` and `Update-Package-Lists` on; both timers `active` and `enabled`; neither service masked | Configured | unattended-upgrades |
| Alpine: apk-cron installed with its executable daily script; crond started and in the default runlevel | Configured | apk-cron |
| apt: upgrades run (as Configured), and `Update-Package-Lists` unset or 0, `apt-daily.timer` not active, or `apt-daily.service` masked | Partial | lists not refreshed |
| apt: upgrades run, the lists refreshed, and a timer `active` but not `enabled` (`disabled`, `masked`, `linked`, `not-found`) | Partial | off after restart |
| Alpine: as Configured, but crond not in the default runlevel | Partial | off after restart |
| apt: unattended-upgrades not installed (`not-installed`, `config-files`, no such package, no `dpkg-query`) | Not Configured | no unattended-upgrades |
| apt: `Enable` 0 (or `00`), or `Unattended-Upgrade` unset or 0 | Not Configured | upgrades off |
| apt: `apt-daily-upgrade.timer` not active (`inactive`, `failed`), enabled or not | Not Configured | upgrade timer off |
| apt: `apt-daily-upgrade.service` `masked` (in `/etc` or `/run`) or `not-found`, its timer running | Not Configured | upgrade job masked |
| Alpine: apk-cron not installed (origin/main's `apk-upgrade` or not: §6 (g)) | Not Configured | no apk-cron |
| Alpine: apk-cron installed, its daily script absent, not executable, or not a regular file | Not Configured | daily script not run |
| Alpine: crond stopped, crashed, or not installed | Not Configured | crond not running |
| any fact that decides not established (§5.3) | Unknown | can't read |

### 5.2 Which line when several apply

The first `No`, in this order, decides; a fact not established decides `Unknown` only when nothing in its tier is `No` (§2.7). Decisions of this unit, for the maintainer.

| Tier | # | apt | Alpine | Gives |
|---|---|---|---|---|
| upgrades run now | 1 | unattended-upgrades installed | apk-cron installed | "no unattended-upgrades" / "no apk-cron" |
| | 2 | `Enable` not 0 | its daily script run | "upgrades off" / "daily script not run" |
| | 3 | `Unattended-Upgrade` on | crond running | "upgrades off" / "crond not running" |
| | 4 | `apt-daily-upgrade.timer` active | | "upgrade timer off" |
| | 5 | `apt-daily-upgrade.service` startable | | "upgrade job masked" |
| | | one of these not established, none `No` | | "can't read" |
| complete | 6 | `Update-Package-Lists` on | crond in the default runlevel | "lists not refreshed" / "off after restart" |
| | 7 | `apt-daily.timer` active | | "lists not refreshed" |
| | 8 | `apt-daily.service` startable | | "lists not refreshed" |
| | 9 | `apt-daily-upgrade.timer` enabled | | "off after restart" |
| | 10 | `apt-daily.timer` enabled | | "off after restart" |
| | | one of these not established, none `No` | | "can't read" |
| | 11 | — | — | "unattended-upgrades" / "apk-cron" |

The package first: it is what the user installs; then the switches, then the timer and its service. "Lists not refreshed" before "off after restart": the first is the host now, the second the host after a reboot.

### 5.3 Unknown, "can't read", by cause

An error is in `errors[]` with its code, and the run exits 1; a finding, with no error, exits 3. The findings' details are proposed.

| The host | Error code | Finding: code, detail (*proposed*), source |
|---|---|---|
| no `apt-config`; no `systemctl` | `COMMAND_NOT_FOUND` | |
| `apt-config` fails (a file it cannot parse); `dpkg-query` fails other than "no packages found" | `COMMAND_FAILED` | |
| `apt-config dump`'s output not in its format, or an item twice; a `dpkg-query` status other than installed, not-installed or config-files; a state systemctl(1) does not list; another distribution hardn does not list (not Debian's or Ubuntu's derivative) | `DETECT_UNRECOGNISED` | |
| detect's init system or distribution not established | detect's error's (`DETECT_SIGNALS_DISAGREE`, `DETECT_NO_SIGNAL`, ...) | |
| `Enable` not a decimal; an interval not `0`, `always`, or a decimal with at most one of `s`, `m`, `h`, `d` | | `UPDATES_APT_VALUE_UNKNOWN`, `Unattended-Upgrade "1w"`, `apt-config dump` |
| a timer `activating`, `deactivating`, `maintenance`, `reloading` or `refreshing`; a timer's `is-enabled` a `-runtime` state, `static`, `indirect`, `generated`, `transient`, `alias` or `bad`; a service `bad` | | `UPDATES_SCHEDULE_UNKNOWN`, "apt-daily-upgrade.timer enabled-runtime", `systemctl is-enabled apt-daily-upgrade.timer` |
| a host systemd did not boot (OpenRC on Debian, say): cron's `apt-compat` not read | | `UPDATES_SCHEDULE_UNKNOWN`, "no systemd timers", detect's source (`/run/openrc`) |
| Alpine: OpenRC did not boot the host (the container image) | `ALPINE_NO_OPENRC` | |
| Alpine: the init system not established | `ALPINE_UNKNOWN` | |
| Alpine: `apk info -e` failing or in another form, `rc-service crond status` exiting 4, 8, 16 or another code, a file not readable | the error's (`COMMAND_FAILED`, `DETECT_UNRECOGNISED`, `PERMISSION_DENIED`) | |

### 5.4 Findings on other lines

| The host | Line | Finding: code, detail (*proposed*), source |
|---|---|---|
| Proxmox VE, upgrades running, no origin naming `Proxmox` | Configured or Partial | `UPDATES_PROXMOX_NOT_UPGRADED`, "Proxmox packages not upgraded", `apt-config dump` |
| Alpine: origin/main's `apk-upgrade`, as it wrote it | any | `ALPINE_LEGACY_SCRIPT`, "old apk-upgrade script", `/etc/periodic/daily/apk-upgrade` |
| Alpine: anything else at that path | any | `ALPINE_FOREIGN_SCRIPT`, "other apk-upgrade file", `/etc/periodic/daily/apk-upgrade` |

Tested: `TestLines`, one case per row of §5.1 to §5.4 on Debian 13, Ubuntu 26.04, Proxmox VE 9 and Alpine 3.24, with each precedence of §5.2 that differs; `TestStock` through `Engine.Check`; `TestEvidence`; `TestFindings`, each finding's detail and source; `TestNoKinderThanAlpine`; `TestDetails`, the exported details and their room.

## 6. API; changes to other units' code; not modelled; for the maintainer

```go
package updates // pkg/engine/controls/updates (new)
const ID = "updates.auto"
type Auto struct{ engine.CheckOnly } // ID, "Auto Updates"; Applies always
func Controls() []engine.Control    // Auto
const DetailUnattended = "unattended-upgrades" // and DetailApkCron, DetailListsStale, DetailOffAfterRestart,
// DetailNoUnattended, DetailUpgradesOff, DetailTimerOff, DetailJobMasked, DetailNoApkCron,
// DetailScriptNotRun, DetailCrondOff, DetailCantRead
const CodeAptValueUnknown = "UPDATES_APT_VALUE_UNKNOWN" // and CodeScheduleUnknown "UPDATES_SCHEDULE_UNKNOWN",
// CodeProxmoxNotUpgraded "UPDATES_PROXMOX_NOT_UPGRADED"

package detect // added: readers for the status controls
func DpkgInstalled(ctx context.Context, sys host.System, pkg string) (Tri, error)       // pve-manager's rule, any package
func SystemctlIsActive(ctx context.Context, sys host.System, unit string) (string, error) // systemctl is-active unit; no other verb
```

- **Other units' code:** `pkg/host/detect`'s `pveManager` now calls `dpkgStatus`, its own body with the package as an argument (the error names the package; for `pve-manager` it reads as before; detect's tests unchanged and passing), and `DpkgInstalled` exports it, appended to `proxmox.go`; `SystemctlIsActive` is appended to `initsys.go` beside `next/p4-check-apparmor`'s `SystemctlIsEnabled`. [detect.md](../safety/detect.md) is `next/p2-detect`'s page and is not edited. `pkg/host/alpine` is called, not changed. `pkg/output`'s registry gets the three finding codes, appended, with `codes.golden`, `TestFindingCodes`' list and [docs/reference/output.md](../reference/output.md) §2.2; `.golangci.yml`, `go.mod` and `go.sum` are unchanged.
- **Inputs to `next/p4-risk`**, which owns "a long uptime with no automatic updates" (brief Phase 4; [P-04](../refactor/proposals.md#p-04)) with the header's uptime (`next/p4-header`): whether the host upgrades itself is this row's `State` with its `Detail`, compared with the exported constants rather than a copy of the wording: `Configured` yes; `Partial` "off after restart" yes until the next boot; `Partial` "lists not refreshed" runs the upgrade on lists apt's timer does not refresh, so it finds no new updates unless something else refreshes them (**[I]**, §3): count it as no; `NotConfigured` no; `Unknown` counts as not configured and adds a factor (plan row `p4-risk`). On Proxmox VE, `UPDATES_PROXMOX_NOT_UPGRADED` means the kernel and Proxmox VE's packages are not upgraded even where the row is `Configured`, which matters for the pending-reboot and uptime factors; and apk-cron upgrades Alpine's kernel with no reboot, which is the pending-reboot finding's ([alpine.md](../safety/alpine.md) §4). This package builds no risk.
- **Not modelled:** unattended-upgrades' own configuration: which origins and packages it takes (an empty `Origins-Pattern`, `Package-Blacklist`, `OnlyOnACPower`), and a conffile prompt that holds a package back; Debian's default origins taking stable point releases as well as security updates (§3), so "unattended-upgrades" says the job runs, not what it installs; `ConditionACPower` on the services (a host on battery) and `apt.systemd.daily`'s own early exits other than `Enable`; the `unattended-upgrade` binary on the job's `PATH` (P-43 (3)'s test; the package installed stands for it); the timers' schedules (an `OnCalendar` drop-in, a weekly interval) and a service drop-in replacing `ExecStart`; a runtime mask of a service, read as masked now, not as gone after the next boot; upgrades run by other means (an administrator's cron job, cron-apt), shown by what apt's job does; cron's `apt-compat` off systemd (§2.6); unattended-upgrades' origin patterns matched as it matches them, or a wildcard origin (§2.9); a distribution's own tools on a Debian or Ubuntu derivative. On Alpine, `next/p2-alpine`'s reading: origin/main's `apk-upgrade` does not count (§6 (g)); a symlink at `/etc/periodic/daily/apk` reads as not run, though run-parts runs a link to an executable ([alpine.md](../safety/alpine.md) §3); crond in a runlevel other than `default` reads as "off after restart"; cronie or dcron instead of busybox crond.
- **For the maintainer:** (a) the meaning of the states (§2.1): `NotConfigured` whenever no upgrade runs now, `Partial` only for upgrades running with a piece missing, against [checks.md](checks.md) §1.3's "some of it"; the alternative reads every piece missing as `Partial` (the package installed and its timer off, say), which says more of the host than it gets; (b) on Alpine, the departure from `Updates.Status` (§2.8): its `Partial` for a script not run or crond not running is `NotConfigured` here, so that Alpine and apt read alike; (c) beyond the plan's "`apt-daily-upgrade.timer` enabled": whether it runs now (`is-active`), and whether its service is masked (§2.6); (d) `apt-daily.timer` and its service read with `Update-Package-Lists` for "lists not refreshed", and that line `Partial` rather than `NotConfigured`, though it may install nothing new (§6, inputs to `p4-risk`); (e) cron's `apt-compat` not read: `Unknown` off systemd rather than a reading of cron (§2.6); (f) Proxmox VE: decision 5 left open, the row read as Debian with `UPDATES_PROXMOX_NOT_UPGRADED`, and an origin taken to cover Proxmox when its text names it (§2.9); (g) origin/main's `apk-upgrade`, which upgrades daily where crond runs it, is "no apk-cron" with `ALPINE_LEGACY_SCRIPT`, `next/p2-alpine`'s rule, not counted as automatic updates; (h) the Alpine findings' details reworded to describe the host, not `next/p2-alpine`'s, which describe a plan; (i) the control's ID, `updates.auto` (stable once released), and P-43 (4)'s record path that follows from it, `/var/lib/hardn/controls/updates.auto.json`; (j) a distribution hardn does not list read through apt when its `ID_LIKE` names Debian or Ubuntu, and otherwise `Unknown` (`DETECT_UNRECOGNISED`) rather than `NotApplicable`, so it stays in the risk level; (k) the `Configured` details name the tool ("unattended-upgrades", "apk-cron") rather than a frequency ("daily"), which an interval or a timer's schedule can change; (l) every wording of §5, and "off after restart" reused from AppArmor's agreed line; (m) "upgrade job masked" also for a service `not-found`.
- **Size:** about 530 lines of non-test Go in the package (about 125 of them whole-line comments), 30 in `pkg/host/detect`, 11 in `pkg/output` and its golden list, about 750 of tests, 50 of fixtures with their README, and this page, against the plan's guideline of about 500 changed lines: two tools read on four platforms, the two tiers of §5.2 with an `Unknown` for every fact, and a test for each line of §5.
