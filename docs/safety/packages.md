# Packages: installing one named package (`pkg/host/packages`)

> **Unit:** refactor plan slice 4, `next/s4-control`, unit (b); the primitive of decision 1 ([gallery-firewall.md](../engine/gallery-firewall.md)), which slice 7 can share.
> **Status:** built and tested on macOS against `hostfake` only. Nothing has run on a VM; the forms come from read-only runs on 2026-11-02 (Debian 13, Ubuntu 26.04, Alpine 3.23), kept outside the repository in the audit's `vm-runs/s4/unit-b`.

## 1. API

`ToolFor(facts)` gives `Apt` on Debian and Ubuntu, `Apk` on Alpine, `Dnf` on Rocky Linux, AlmaLinux and RHEL, `ErrNoTool` otherwise, Proxmox VE included. `Installed(ctx, sys, tool, name)` reads a `detect.Tri`. `PlanInstall(ctx, sys, tool, name)` returns a `Plan{Tool, Name, Argv, Undo, Adds}`; `Empty()` says there is nothing to run. `Execute(ctx, sys, plan)` returns a `Report{Undo, Installed}`.

| Tool | Read | Simulation | Install | Undo |
|---|---|---|---|---|
| Apt | `detect.DpkgInstalled` | `apt-get install -s -y <name>` | `apt-get install -y <name>` | `apt-get remove -y <name>` |
| Apk | `apk info -e <name>` | `apk add --simulate <name>` | `apk add <name>` | `apk del <name>` |
| Dnf | `rpm -q <name>` | `dnf install --assumeno --setopt=install_weak_deps=False --setopt=exit_on_lock=True <name>` | `dnf install -y --setopt=install_weak_deps=False --setopt=exit_on_lock=True <name>` | `dnf remove -y --noautoremove --setopt=exit_on_lock=True <name>` |

## 2. Guarantees

1. **Everything goes through `host.System`** (depguard rule `packages-through-host`): no file, system or network calls of its own.
2. **`PlanInstall` only reads:** the read, then the simulation. Already installed plans nothing.
3. **Names** other than `^[a-z0-9][a-z0-9+.-]*$` are refused (`fs.ErrInvalid`), nothing run; so is a plan whose commands are not its tool's and name's.
4. **`Execute` checks:** no Yes from `Installed` after the command is `ErrVerify`. `Report.Undo` is set once the command ran, even failed; not for an empty plan or a command not found.
5. **A refusal names the changes:** `*OtherPackagesError{Changes}`, with `errors.Is(err, ErrOtherPackages)`.

## 3. Simulation rules

| Form | Result | From |
|---|---|---|
| `Inst x (ver ...)` | new package, in `Adds` | deb13-vm-2.log, ubuntu26-vm-2.log |
| `Conf x (ver ...)` | ignored | the same |
| `Remv x [ver]` | refused | deb13-vm-2.log, ubuntu26-vm-2.log (`apt-get remove -s`) |
| `Inst x [old] (new ...)`, `Purg x [ver]` | refused | [I] apt's simulate form, not in the logs |
| `E: Unable to locate package x`, exit 100 | `ErrNotInRepos` | deb13-vm.log, ubuntu26-vm.log |
| `(i/n) Installing x (ver)` | new package, in `Adds` | [I] `pkg/host/alpine`'s tests; no VM has shown it for ufw |
| `(i/n)` with any other verb | refused: apk commits the whole world | [alpine.md](alpine.md), `alpine.simulate` |
| `unable to select packages` with `(no such package)`, exit 1 | `ErrNotInRepos` | alpine323-vm.log (empty index), alpine323-vm-2.log (no DNS) |
| dnf: a row under `Installing:`, `Installing dependencies:` or `Installing weak dependencies:` | new package, in `Adds` | rocky10-test-110, probes 4 and 3b-2 (2026-11-09, -10) |
| dnf: any other section, or a `replacing` line | refused | [I] dnf's `output.py` headings; probe 4 showed `Upgrading:` for an installed sudo |
| dnf: a `kernel` or `kernel-…` row of another release than `uname -r` | `ErrKernel` (the maintainer, 2026-11-10: "Refuse") | probe 4: firewalld's weak dependencies brought kernel 211.74 to a host on 211.16 |
| dnf: `Error: Unable to find a match: x`, exit 1 | `ErrNotInRepos` | probe 3b |
| dnf: exit 200 | `ErrLocked` | [I] dnf's `main.py` (a lock) |
| `rpm -q x`: exit 0, `x-…` lines; exit 1, `package x is not installed` on stdout | installed; not installed | probes 4 and 3b |

So on Alpine `ErrNotInRepos` also covers a missing index, a missing community repository and no network. Any other failure is the error, wrapped. Exit 0 without `name` among the new packages is refused too: another package instead is `*OtherPackagesError`, no change listed is `detect.ErrUnrecognised` (Ubuntu's "already the newest version" form, with dpkg saying not installed).

## 4. Written, not run

Every install and undo (dnf's runs in the 3b VM run, el-firewalld.md §4). Each install runs to its end under its own 30-minute bound, whatever its caller's context does: a killed rpm or dpkg transaction leaves the package database half done. The successful apk form above. `apt-get install -y` with no `DEBIAN_FRONTEND` (`host.System` passes `PATH` and `LC_ALL=C` alone): a package that asks debconf a question without a terminal [I]. `apt-get remove` leaves the dependencies in `Adds`, `apk del` removes those nothing else needs [I]. This unit does not refresh the index: `apt-get update` and `apk update` are the caller's. Proposed words: "hardn can't install packages on this system", "package not found in the repositories", "installing it would change other packages", "package not installed after the install".
