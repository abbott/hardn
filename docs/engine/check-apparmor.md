# AppArmor: the AppArmor status row (`pkg/engine/controls/apparmor`)

> **Unit:** refactor plan Phase 4, `next/p4-check-apparmor` (plan §2, row `p4-check-<area>`, AppArmor: "enabled? NotApplicable in containers, and on Alpine unless enabled", and the maintainer's decision of 2026-10-20 in the same row; brief Phase 4, "Where each status comes from"; brief 2.5, Alpine and containers; brief §3 principle 3). **Decided by the maintainer (2026-10-20): AppArmor is status only in v0.4**, with exactly the eight status lines of [P-43](../refactor/proposals.md#p-43) (3), "AppArmor in v0.4: status only", and a place in the risk level (plan §3.12; [P-16](../refactor/proposals.md#p-16)). It follows the pattern of [checks.md](checks.md) and takes up the "Status sources" item of [docs/safety/README.md](../safety/README.md) for AppArmor and, in part, [alpine.md](../safety/alpine.md) §6's `p4-check-apparmor` item (§2.11). No V-number covers origin/main's AppArmor row; [interaction.md](../refactor/phase0/interaction.md) §7 records it ("no container or Alpine NotApplicable state"), and V23 (Run All's claim that AppArmor ran) stays slice 7's and Run All's.
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only, with the kernel's files and `systemctl is-enabled apparmor.service` as each case sets them (§3). Nothing has run on Linux: everything host-specific is "written, not run (gate 5)" (§4). The eight lines are the maintainer's; this unit adds no wording (§5).

Figma's main menu has an AppArmor row ("Not Configured" in `00-TUI-MAIN.png`, with no detail). It is now a check-only control, `apparmor.enforce`, titled "AppArmor", whose `Check` reads what the kernel enforces: whether the AppArmor LSM is on, each loaded profile's mode, the global mode, and whether AppArmor's service loads the profiles at the next boot. `Plan` returns `engine.ErrNotMigrated`, and in v0.4 nothing replaces it: hardn neither turns AppArmor on nor off.

## 1. Why

origin/main's AppArmor row (`checkAppArmorStatus`, `pkg/security/status.go:355-412`) matches `aa-status`'s text: Configured when it says "profiles are in enforce mode" and not "0 profiles are in enforce mode", a substring that "10 profiles are in enforce mode" contains, so a host whose count of enforcing profiles ends in 0 (10, 20, 110) shows Not Configured (by code reading, not run); inside a container it reads the host's kernel, and on Alpine it asks for the package and `rc-status default` first. It has no `NotApplicable` and no `Unknown`: a failure of any command is Not Configured. A kernel with the LSM on and no profile loaded restricts nothing [V, kernel `Documentation/admin-guide/LSM/apparmor.rst`, via [P-43](../refactor/proposals.md#p-43) (3)], so "installed" and "loaded" are not "confined", and a status that says a host is safe when it is not is the worst error of this phase (brief Phase 4: prefer Unknown to guessing). The legacy row stays as it is until the main menu goes live (plan, "Phases 3–5 together").

## 2. Guarantees

1. **One line per host state, the eight of P-43 (3), decided in a fixed order** (§5.2). Each fact is read from the kernel or the init system, never from a configuration file (brief §3 principle 3), and the first that decides the line ends the reading: what is not needed for the line is not read.
2. **Not Applicable in two cases only.** In a container (detect's `Container.Detected` Yes), through `Applies`: "set by the host" (brief 2.5); `Check` returns the same line when called without `Applies`, and reads nothing. On Alpine with AppArmor off, through `Check`: "Alpine default" (brief 2.5: not automated, left out of the risk score). **A departure from [checks.md](checks.md) §1.6** ("NotApplicable only through `Applies`"): `Applies` takes detect's facts and reads no file, and whether Alpine's kernel has AppArmor on is a reading of `/sys`. `Engine.Check` passes the state through (it overrides only a failed check's), and `pkg/output` exits 0 for it as for any `NotApplicable` ([output.md](output.md) §2.8). A fact that is not established is never grounds for `NotApplicable`: a container check not established is `Unknown`, and nothing under `/sys` is read then, since inside a container `/sys/module` shows the host's kernel ([alpine.md](../safety/alpine.md) §2.12).
3. **Whether the LSM is on** is the kernel's parameter `/sys/module/apparmor/parameters/enabled`: on Alpine through `alpine.CheckAppArmor` (its `Enabled`), elsewhere read by its rules, which `TestSameAsAlpine` checks give the same answer and the same code for the same file: `Y` on; `N` off; no `/sys/module/apparmor` in a `/sys/module` that is there, off (a kernel built without it); anything else not established. Off is "Alpine default" on Alpine and "off in kernel" elsewhere. Where whether the host runs Alpine is not established (no os-release), the two differ, so it is `Unknown`; Debian whose Proxmox VE is not established is not Alpine, and is "off in kernel". A distribution hardn does not list (`DistroOther`) is not Alpine either, whatever its `ID_LIKE`.
4. **The kernel command line is evidence, not a source of the state.** With the LSM off elsewhere than on Alpine, the evidence lists its `apparmor=`, `lsm=` and `security=` parameters from `/proc/cmdline`, or that it has none (the kernel's built-in order leaves AppArmor out); the state is the kernel's parameter, which is what the kernel made of that line (§3). A command line that cannot be read is said in the evidence, and the state stands.
5. **The loaded profiles** are `/sys/kernel/security/apparmor/profiles`, one line per profile, `name (mode)`, or `:ns:name (mode)` for a namespace below root's, as root sees them; hats such as `nvidia_modprobe//kmod` count as profiles. **Only the root namespace's profiles are the host's** (decision of this unit): a container's own namespace (an LXC or LXD container running AppArmor inside) confines that container's processes only, so its profiles are counted apart, in the evidence, and decide nothing; the profiles LXC generates for each container (`lxc-101_</var/lib/lxc>`) are in the root namespace and count. No profile of the host's is "no profiles loaded", whatever the global mode and the boot service say. The modes the kernel names are `enforce`, `complain`, `kill`, `unconfined` and `user` (§3): **`enforce` and `kill` count as enforcing** (decision of this unit: a violation of a kill-mode profile kills the task rather than being allowed), `complain`, `unconfined` and `user` do not. A mode the kernel's list does not name (a vendor's patch, say) is not counted; it is not ranked, so with nothing enforcing beside it the line is `Unknown`, and beside an enforcing profile it changes nothing but the evidence. A line in another form is `Unknown`.
6. **The global mode** `/sys/module/apparmor/parameters/mode` (`enforce` unless changed): `complain` makes every profile complain whatever its own mode (§3), so it is "logging only" even with profiles listed as enforcing; `enforce` leaves each profile's mode as listed; any other value is not ranked, so `Unknown`. The kernel prints it with no newline (v6.18, §3); one newline after it is accepted.
7. **Nothing enforcing** (no profile in `enforce` or `kill` mode, or the global mode `complain`) is "logging only", whatever the boot service says: the line describes the host now (decision of this unit; §5.2).
8. **The next boot.** With profiles enforcing, AppArmor's own service must load them again at boot, or the line is "off after restart": on systemd `systemctl is-enabled apparmor.service`, read as detect reads a unit (`detect.SystemctlState`, `UnitState.IsEnabled`): `enabled` loads them; `disabled`, `masked`, `linked` and `not-found` do not; `static`, `indirect`, `generated`, `transient`, `alias` and `bad` do not say, so `Unknown`. On OpenRC, `apparmor` in the `sysinit`, `boot` or `default` runlevel (`detect.ServiceRunlevels`) loads them; in no runlevel it does not; only in another runlevel is not established, so `Unknown`. `apparmor.service`'s stop unloads nothing, so a disabled service leaves the profiles loaded until the next boot (§3). `is-active` is not read: a stopped or failed service changes nothing that the profiles loaded do not show, and the next boot starts an enabled one again.
9. **Configured** is everything above at once: the LSM on, the global mode `enforce`, one profile or more enforcing, and the service loading them at boot. Its detail is the count of enforcing profiles: "34 profiles enforced", and "1 profile enforced" for one (the one grammatical adjustment of P-43's string, which the maintainer's decision allows).
10. **`Unknown` is always "can't read", with the error** ([checks.md](checks.md) §1.5's first kind): a read that fails (`PERMISSION_DENIED` for hardn run as another user than root, since the kernel refuses the profiles list and the mode to it, §3; `FILE_NOT_FOUND`; `COMMAND_NOT_FOUND`), detect's fact that is not established (its own code: `DETECT_NO_SIGNAL`, `DETECT_SIGNALS_DISAGREE`, ...), and a value hardn does not rank, wrapped as `detect.ErrUnrecognised` (`DETECT_UNRECOGNISED`), as `alpine.CheckAppArmor` does for the same file. **A departure from [checks.md](checks.md) §1.5's second kind** ("a value in a form hardn does not know" as `Unknown` with findings and no error): so that the same file gives the same code on Alpine and elsewhere, and so that the control needs no finding codes; such a check exits 1, as a failed one, rather than 3. Never a pass: no `Unknown` path returns another state.
11. **No findings.** The eight details and the evidence say everything this unit reads, so the control has no finding and registers no code. [alpine.md](../safety/alpine.md) §6 hands on `AppArmor.Note` (what enabling takes on Alpine, per bootloader) for rendering: **not taken up here** (decision of this unit). The note was written for an AppArmor screen, which is deferred past v0.4, and as a finding it would be wording the maintainer has not agreed (long, and about what hardn does not do). The bootloader files `alpine.CheckAppArmor` finds are left out of the evidence for the same reason: they say where enabling would start, not what the state is.
12. **Evidence** is each fact as it was read, one line per source: `/sys/module/apparmor/parameters/enabled: "Y\n"` (quoted, as `alpine.CheckAppArmor` writes it), or `/sys/module/apparmor: absent`; `/sys/kernel/security/apparmor/profiles: 15 enforce, 1 complain, 4 unconfined` (the host's, the kernel's modes in its order, then others), or `: none`, with `; 1 in other namespaces` after it where there are such; `/sys/module/apparmor/parameters/mode: "enforce"`; `systemctl is-enabled apparmor.service: enabled`, or `/etc/runlevels: apparmor in boot` (`in no runlevel`); `/proc/cmdline: apparmor=0`. Tested: `TestEvidence`.
13. **Read-only, through `host.System`.** The check reads files (on Alpine with `alpine.CheckAppArmor`'s `lstat` of the bootloader files) and runs exactly `systemctl is-enabled apparmor.service`, on systemd; every call of every test is checked (`readOnly`). depguard rule `controls-through-host` ([checks.md](checks.md) §1.11) covers the package, checked to report `os` and `pkg/output` imports in a throwaway file of it (in a temporary copy of the tree; nothing committed); no rule was added.
14. **`Plan` returns `engine.ErrNotMigrated`** (`engine.CheckOnly`), and `Engine.Plan` over `Controls()` is refused, its item's error coded `CONTROL_NOT_MIGRATED`, with no change (`TestCheckOnly`).

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| `enabled` is `module_param_named(enabled, apparmor_enabled, aaintbool, 0444)`, printed `Y\n` or `N\n`; `apparmor=` sets it at boot (`__setup("apparmor=", …)`); the LSM core sets it false for every LSM not in the order chosen at boot (`lsm=`) | linux v6.18 `security/apparmor/lsm.c`, `kernel/params.c` (`param_get_bool`), read 2026-10-20; `security/security.c` via [alpine.md](../safety/alpine.md) §3 | §2.3 |
| A kernel without AppArmor has no `/sys/module/apparmor` | [alpine.md](../safety/alpine.md) §3 (`alpine.CheckAppArmor`'s rule) | §2.3 |
| `mode` is `module_param_call(mode, …, S_IRUSR \| S_IWUSR)`: root only; `param_get_mode` prints `aa_profile_mode_names[aa_g_profile_mode]` with `"%s"` and refuses a caller that may not view the policy; v6.18's `param_attr_show` adds no newline; the initial value is `APPARMOR_ENFORCE`; it can be set to `enforce`, `complain`, `kill` or `unconfined` (`APPARMOR_MODE_NAMES_MAX_INDEX` 4) | linux v6.18 `security/apparmor/lsm.c`, `include/policy.h`, `kernel/params.c`, read 2026-10-20 | §2.6; whether older kernels print a newline is **[unverified]** |
| The global mode applies to every profile: `PROFILE_MODE(p, m)` is `aa_g_profile_mode == m \|\| p->mode == m`, and `COMPLAIN_MODE` is `PROFILE_MODE(p, APPARMOR_COMPLAIN)` | linux v6.18 `security/apparmor/include/policy.h`, read 2026-10-20 | §2.6, §2.7 |
| The mode names, in order: `enforce` ("enforce access rules"), `complain` ("allow and log access violations"), `kill` ("kill task on access violation"), `unconfined` ("profile set to unconfined"), `user` ("modified complain mode to userspace") | linux v6.18 `security/apparmor/policy.c` (`aa_profile_mode_names`), `include/policy.h` (`enum profile_mode`), read 2026-10-20 | §2.5; that kill mode denies what it does not allow is **[I]** from the comment |
| `profiles` is `0444` but its open is refused (`EACCES`) to a caller that may not view the policy; each line is a profile's label with its mode, `%s (%s)` or `:%s:%s (%s)` for another namespace's, subnamespaces included | linux v6.18 `security/apparmor/apparmorfs.c` (`profiles_open`, `seq_show_profile`), `label.c` (`aa_profile_snxprint`), read 2026-10-20 | §2.5, §2.10 |
| securityfs, with AppArmor's directory, is at `/sys/kernel/security`; the LSM restricts nothing until profiles are loaded | P-43 (3) (Debian wiki AppArmor/HowToUse; kernel `apparmor.rst`) | §2.5 |
| `apparmor.service`'s `ExecStop=/bin/true` unloads nothing, so profiles stay loaded until the next boot after it is disabled | P-43 (3) (apparmor `parser/apparmor.service`) | §2.8 |
| What `systemctl is-enabled` prints, and which states say a unit starts at boot | [detect.md](../safety/detect.md) (`UnitState.IsEnabled`; systemctl(1)) | §2.8 |
| Debian's kernel enables the LSM by default, package or not; Ubuntu installs and loads AppArmor by default; Proxmox VE's kernel has it on, `lxc-pve` depends on `apparmor`, and each container's generated profile is loaded at its start and unloaded at its stop | P-43 (3) (Debian linux `debian/config/config`; Ubuntu Server docs; `pve-kernel`, `lxc.git`, `pve-container`, lxc `src/lxc/lsm/apparmor.c`) | §5; the fixtures |
| LXC names a generated profile `lxc-<name>_<<lxcpath>>` (`lxc-101_</var/lib/lxc>`) | lxc `src/lxc/lsm/apparmor.c` (`apparmor_profile_full`), read 2026-10-20 | the Proxmox VE fixture |
| Alpine's kernels build AppArmor in but leave it out of `CONFIG_LSM`, so it is off unless the command line selects it; enabling it takes the package, `rc-update add apparmor boot`, the kernel parameter and a reboot | [alpine.md](../safety/alpine.md) §3 | §2.3 ("Alpine default"), §2.8 (OpenRC's `apparmor`) |
| Inside a container AppArmor is the host's, and `/sys/module` shows the host's kernel | brief 2.5 (decided); [detect.md](../safety/detect.md) §6; [alpine.md](../safety/alpine.md) §2.12 | §2.2 |
| OpenRC boots through `sysinit`, `boot` and `default` | **[I]** (a softlevel named on the kernel command line not read) | §2.8 |
| Other programs load profiles of their own (snapd's `snapd.apparmor.service`, LXC at a container's start, Docker's `docker-default`), which a disabled `apparmor.service` does not stop | **[I]** | §6, not modelled |
| `aa-status` counts its "profiles are in enforce mode" from the same list | **[unverified]** | §4 (compared on the VMs) |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root. On the VMs of [vmtest.md](../safety/vmtest.md) (Debian 13, Ubuntu 26.04, Alpine 3.24) and by hand on Proxmox VE 9, through a dev-only driver or the gallery's status scenario once one calls `Engine.Check` with this control:

- each stock host: the line as installed, its count against `aa-status`'s profiles in enforce and kill mode;
- Debian 13: `systemctl disable apparmor.service` ("off after restart"), then a reboot ("no profiles loaded"); a boot with `apparmor=0` and with an `lsm=` without it ("off in kernel", the parameter in the evidence); the global mode set to `complain` ("logging only"); every profile put in complain mode with `aa-complain` ("logging only");
- Ubuntu 26.04: the same run as a user other than root ("can't read", `PERMISSION_DENIED`); a host without the `apparmor` package, if it can be removed ("no profiles loaded");
- Proxmox VE 9 with containers running (their `lxc-<id>_</var/lib/lxc>` profiles counted), one of them nested and running AppArmor inside (its namespace's profiles counted apart, with the `:ns:` form of §3), and from inside an LXC container ("set by the host");
- Alpine 3.24 stock ("Alpine default"), then after `alpine.AppArmorNote`'s steps ("N profiles enforced", or "no profiles loaded" without a profile package), and with `apparmor` dropped from the boot runlevel ("off after restart");
- whether `/sys/module/apparmor/parameters/mode` ends in a newline on each platform's kernel (§3);
- the time `Check` takes (a few file reads and at most one command).

## 5. The status lines

### 5.1 The eight lines (decided, P-43 (3))

The maintainer's lines, quoted from [P-43](../refactor/proposals.md#p-43) (3), in Figma's grammar (marker, label, the state's word in its colour, a faint detail):

```text
   ▽  AppArmor             Configured 34 profiles enforced
   ⛬  AppArmor             Partial logging only
   ⛬  AppArmor             Partial off after restart
   ○  AppArmor             Not Configured no profiles loaded
   ○  AppArmor             Not Configured off in kernel
   ∅  AppArmor             Not Applicable set by the host
   ∅  AppArmor             Not Applicable Alpine default
   ?  AppArmor             Unknown can't read
```

Every line the control can show, with the host's state that gives it. The details are exactly these strings; the count is the number of enforcing profiles, and "1 profile enforced" is the only adjustment (§2.9). No wording here is new.

| The host | State | Detail |
|---|---|---|
| the LSM on, global mode `enforce`, one profile or more in `enforce` or `kill` mode, `apparmor.service` enabled (OpenRC: `apparmor` in `sysinit`, `boot` or `default`) | Configured | 34 profiles enforced (1 profile enforced) |
| the LSM on, profiles loaded, none in `enforce` or `kill` mode (all `complain`, `unconfined` or `user`) | Partial | logging only |
| the LSM on, profiles loaded, global mode `complain` | Partial | logging only |
| the LSM on, profiles enforcing, `apparmor.service` `disabled`, `masked`, `linked` or `not-found` (OpenRC: `apparmor` in no runlevel) | Partial | off after restart |
| the LSM on, no profile of the host's loaded (no package, say; a container's namespace's alone) | Not Configured | no profiles loaded |
| not Alpine: `enabled` `N` (`apparmor=0`, an `lsm=` without it, a built-in order without it), or a kernel built without AppArmor | Not Configured | off in kernel |
| inside a container (detect) | Not Applicable | set by the host |
| Alpine: `enabled` `N`, or a kernel built without AppArmor | Not Applicable | Alpine default |
| any fact not read or not ranked (the list in §5.3) | Unknown | can't read |

### 5.2 Which line when several apply

The facts are read in this order, and the first row that matches decides (§2.1). Decisions of this unit, for the maintainer, are marked.

| # | Fact | Decides |
|---|---|---|
| 1 | container | Yes: "set by the host"; not established: "can't read" |
| 2 | the LSM | not established: "can't read"; off: "Alpine default" on Alpine, "off in kernel" elsewhere, "can't read" where Alpine is not established. **Off in kernel beats everything about profiles**, which an LSM off has none of |
| 3 | the profiles list | not read: "can't read"; empty: "no profiles loaded". **No profiles loaded beats logging only and off after restart** (the decided "off after restart" is "with profiles still loaded"; a global `complain` with nothing loaded confines nothing either) |
| 4 | the global mode | not read: "can't read"; `complain`: "logging only"; not `enforce`: "can't read" |
| 5 | the profiles' modes | none enforcing: "logging only", or "can't read" with a mode hardn does not know beside them |
| 6 | the boot service | not established: "can't read"; not loading them: "off after restart". **Logging only beats off after restart** (decision of this unit): the line describes the host now, and nothing is enforced now; the service's state is in the evidence |
| 7 | — | "N profiles enforced" |

### 5.3 Unknown, "can't read", by cause

| The host | Error code |
|---|---|
| hardn not run as root: the profiles list or the global mode refused by the kernel | `PERMISSION_DENIED` |
| securityfs not mounted (no `/sys/kernel/security/apparmor/profiles` with the LSM on); no `/sys/module` | `FILE_NOT_FOUND` |
| `enabled` neither `Y` nor `N`; `/sys/module/apparmor` without its parameter; a global mode other than `enforce` or `complain`; a profiles line not in the kernel's form; profile modes the kernel does not name with none enforcing; `apparmor.service` `static`, `indirect`, `generated`, `transient`, `alias` or `bad`, or a state systemctl does not document; OpenRC's `apparmor` only in a runlevel other than `sysinit`, `boot` or `default` | `DETECT_UNRECOGNISED` |
| `systemctl` not installed | `COMMAND_NOT_FOUND` |
| detect's container check, init system, or (with the LSM off) distribution not established | detect's error's (`DETECT_NO_SIGNAL`, `DETECT_SIGNALS_DISAGREE`, `PERMISSION_DENIED`, ...) |
| an OpenRC runlevel entry that cannot be checked, with `apparmor` found in none | the error's (`PERMISSION_DENIED`, ...) |

Tested: `TestLines`, one case per row of §5.1 on Debian 13, Ubuntu 26.04, Proxmox VE 9 (LXC's profiles loaded) and Alpine 3.24 with and without AppArmor, a container, and each cause of §5.3; `TestStock` through `Engine.Check`.

## 6. API; changes to other units' code; not modelled; for the maintainer

```go
package apparmor // pkg/engine/controls/apparmor (new)
type Enforce struct{ engine.CheckOnly } // "apparmor.enforce", "AppArmor"
func Controls() []engine.Control        // Enforce
func Enforced(st engine.Status) (n int, ok bool) // the count of a Configured status, for next/p4-risk

package detect // added: the readers DetectSSH uses, for the status controls
func SystemctlState(ctx context.Context, sys host.System, verb, unit string) (string, error)
func ServiceRunlevels(ctx context.Context, sys host.System, service string) ([]string, Tri, error)
```

- **Other units' code:** `pkg/host/detect` exports its unexported `systemctl` and `runlevels` as `SystemctlState` and `ServiceRunlevels` (appended to `initsys.go`), so that this check reads `apparmor.service` and OpenRC's `apparmor` by the rules detect reads ssh's units with, instead of a copy ([detect.md](../safety/detect.md) is `next/p2-detect`'s page and predates them). `pkg/host/alpine` is called, not changed. `pkg/output`'s registry, `.golangci.yml`, `go.mod` and `go.sum` are unchanged: no finding, no sentinel, no rule.
- **Inputs to `next/p4-risk`** (AppArmor counts in the risk level, decided 2026-10-20, as the host's exposure though hardn does not change it in v0.4): the row's `State` (`NotApplicable`, "set by the host" and "Alpine default", left out of the score; `Unknown` counted as not configured, with a factor, plan row `p4-risk`), and for `Configured` the count of enforcing profiles, `Enforced(st)`, read back from the detail this package writes, so that no other package parses it. Whether the count weighs, and how "logging only" and "off after restart" weigh against "no profiles loaded", is `next/p4-risk`'s to decide; this package builds no risk.
- **Not modelled:** profiles that another program loads at boot or at a container's start (snapd, LXC, Docker, libvirt), so "off after restart" may overstate what a reboot leaves (**[I]**, §3), and the line describes `apparmor.service`'s profiles only; what a profile allows (a profile in enforce mode that allows everything counts); Ubuntu's `unconfined`-mode profiles, which serve its restriction of unprivileged user namespaces (**[I]**), count as not enforcing; a softlevel on the kernel command line (§2.8); whether profiles on disk differ from those loaded; AppArmor's other LSM parameters (`audit`, `debug`, `lock_policy`).
- **For the maintainer:** (a) the precedence of §5.2 where two lines apply: no profiles loaded over logging only and off after restart, and logging only over off after restart; (b) `kill` counted as enforcing (§2.5); (c) `not-found` (no `apparmor.service`) read as "off after restart" with profiles loaded by another program; (d) "Alpine default" returned by `Check` rather than `Applies`, against [checks.md](checks.md) §1.6 (§2.2); (e) every `Unknown` with an error, and a value hardn does not rank coded `DETECT_UNRECOGNISED`, against [checks.md](checks.md) §1.5's findings (§2.10); (f) `alpine.AppArmorNote` not shown anywhere while AppArmor has no screen (§2.11); (g) only the root namespace's profiles counted, a container's own namespace's apart (§2.5); (h) a distribution hardn does not list (`DistroOther`) read as not Alpine, so "off in kernel" there, whatever its `ID_LIKE` (§2.3); (i) the control's ID, `apparmor.enforce` (stable once released; `apparmor.status` or `apparmor.lsm` are the alternatives), and P-43 (4)'s record path that would follow from it, `/var/lib/hardn/controls/apparmor.enforce.json`.
- **Size:** about 375 lines of non-test Go (about 80 of them whole-line comments), 20 in `pkg/host/detect`, about 530 of tests, 70 of fixtures and this page, against the plan's guideline of about 500 changed lines: the eight lines on five platforms, each `Unknown` cause tested, and the precedence tables the maintainer asked for.
