# The main menu from the real checks, in the gallery (`internal/tools/uigallery`: `status`, `play status`)

> **Unit:** refactor plan Phase 4, `next/p4-gallery-status` (plan header, "Phase 4 started", item 5: "the review surface (the gallery's main menu from the real checks over the simulated hosts)"; brief Phase 4, "Connect the Phase 1 main-menu slice to the live status model", here in the gallery alone; brief Phase 5, "gallery first"). It shows what the five checks ([check-ssh.md](check-ssh.md), [check-firewall.md](check-firewall.md), [check-users.md](check-users.md), [check-apparmor.md](check-apparmor.md), [check-updates.md](check-updates.md)), the header probe ([header.md](header.md)) and the risk level ([risk.md](risk.md)) make of a host, as `pkg/output` maps them. No V-number, no proposal decided, and no behaviour of hardn changes: the unit is dev-only, in a binary no build target makes ([design-system README](../design-system/README.md) §9.14, "Never released").
> **Status:** built and tested on macOS, in process, over hostfake: each host read by the real code, its main menu pinned as a golden on the core set, every call the checks made checked to be a read, the race detector run on the package. No real terminal and no host were used; nothing here runs on Linux, and nothing needs to: the gallery runs no command (§2.7). **No wording is new here**: every status line, level, gloss and factor is the checks', the probe's or the score's, proposed on their pages (§5).

The checks, the probe and the score each have their tests, and `pkg/output`'s golden `human/status` draws the risk summary and the status rows over synthetic reports. None draws the main menu as hardn will draw it from a host. Now `uigallery status` lists, and `uigallery play status` plays, hardn's main menu whose header, distribution bar, host block, risk summary and status rows are read from a simulated host by the real code, so the maintainer can review every line in its place.

## 1. Why

A status line is reviewed in its place: beside the six others, under the level they score, below the header the same host fills. The pages' tables list each line alone; the risk page computes levels over synthetic rows. Which lines a real platform gives as installed, and what level they add up to, was not shown anywhere, nor how the main menu takes a factor or a pending reboot at 80×24 (risk.md §8 (i)).

## 2. Guarantees

### The hosts (`status.go`, `statushost.go`)

1. **Fourteen simulated hosts**, `statusHosts()`, listed by `status` and in play's menu in this order:

   | Host | What it is | Shows |
   |---|---|---|
   | `debian13` | Debian 13 as the engine's scenarios start from it (`newHost`'s platform): `ssh.service` on 22, ufw active allowing 22, alice in sudo, AppArmor enforcing, no unattended-upgrades; up 3 days | a platform's stock lines |
   | `ubuntu2604` | Ubuntu 26.04 as the scenarios start (`ssh.socket`, sudo-rs), unattended-upgrades on | the same, Auto Updates Configured |
   | `alpine324` | Alpine 3.24 as the scenarios start: OpenRC, doas with alice in wheel, no ufw, no AppArmor in its kernel, crond, no apk-cron | "non-root, doas", "ufw not installed", "Alpine default", "no apk-cron" |
   | `pve9` | a Proxmox VE 9 node as installed: root alone, `PermitRootLogin yes`, pve-firewall running and disabled, LXC's AppArmor profiles; up 41 days | Proxmox VE's lines, the uptime factor |
   | `fresh` | Debian 13 before anything is set up: root alone, no ufw | "root only", Critical |
   | `hardened` | Debian 13 after hardn: 2230, root and passwords refused, ufw allowing 2230 alone, unattended-upgrades on | every row Configured, Minimal |
   | `figma` | the hardened host with no AppArmor profile and no unattended-upgrades, up 182 days, 2 hours, 26 minutes | 00-TUI-MAIN's rows and level, with its factor |
   | `lockout` | the hardened host, ufw allowing 443 alone | "ssh blocked", "Critical ≈ lockout risk" |
   | `reboot` | `ubuntu2604` with a newer kernel and `/var/run/reboot-required` | the host block's "Reboot pending" and its factor |
   | `apparmor-logging`, `apparmor-restart`, `apparmor-kernel`, `apparmor-unread` | `debian13` with its profiles in complain mode; `apparmor.service` disabled; `apparmor=0`; securityfs not mounted | AppArmor's "logging only", "off after restart", "off in kernel", "can't read" |
   | `container` | `debian13` in an LXC container | "set by the host" |

   With `debian13`'s "7 profiles enforced", `figma`'s "no profiles loaded" and `alpine324`'s "Alpine default", **AppArmor's eight lines** ([P-43](../refactor/proposals.md#p-43) (3)) are each one host's. Each host is fresh each time it is read, and nothing carries from one to the next. Tested: `TestStatusHosts` (every host's main menu as §5 lists it).
2. **Read by the real code, as the main menu will read it** (`session.readStatus`): detect's facts once (`engine.Options.Detect`, which keeps them), `Engine.Check` over the status rows' controls (`statusControls`: the five areas' `Controls()`, in `risk.Rows`' order, Figma's), `header.Read` on the same facts, `risk.Assess` over both; then `output.HeaderViewOf` and `output.StatusViewOf` fill the main menu's mock (`screen.MainMenuFixture`) in place of its header, bar, host block, risk summary and status rows, its version the mock's, `v0.4.0` (`hostStatus.mainMenu`). The gallery computes no line: each is a check's, the probe's or the score's. Tested: `TestStatusHosts` (the rows, the level, its gloss and factors, the bar, the host block, the main menu's rows the reports' in order; the one check error where a host says, `FILE_NOT_FOUND` for `apparmor-unread`; detect's platform, Proxmox VE, container and init system as the host's note names them).
3. **The model** (`statushost.go`). `statusOn` adds to `simulate`'s host of a platform what the checks and the probe read of it, as the platform installs it: `/etc/passwd` and `/etc/group`, the login shells, doas's rule on Alpine (setup-alpine's `20-wheel.conf`); AppArmor's module parameters, its profiles and `/proc/cmdline` (`statusWith`'s five states of the module); the kernel's hostname, uptime, routes and addresses, the running kernel and those installed (`/boot`, dpkg's `linux-image-*`, Alpine's `/lib/modules`); and the answers (`readings`) of `systemctl is-active` and `is-enabled` for apt's timers and services, `apparmor.service` and `pve-firewall.service`, `dpkg-query`, `apt-config dump`, `ip -4 addr show dev`, `apk info -e`, `rc-service crond status` and `doas -C`. Its formats are the ones the checks' and the probe's own tests pin (their site builders and testdata), written again: a main package cannot import them. `fakehost.go` gains a Proxmox VE 9 platform (`pve9Platform`: Debian 13 underneath, `pve-manager` 9.0.10 in dpkg, `pveversion`, pmxcfs mounted on `/etc/pve`, no sudo, no ufw), doas as a platform's flag (Alpine's, as before), `apt-config` not shipped on Alpine and `apk` only there, and on a status host `dpkg-query`'s answer for every package. **The scenarios' hosts carry no readings** (`fakeHost.status` nil): every command their engine runs is answered as before, one the model does not answer is still kept as unanswered (`dpkg-query` but for `pve-manager` among them), and every existing golden is unchanged. Tested: `TestScenarioHostsKeepTheirs`; `TestFakeHost`, `TestScenarioHosts`, `TestPlayEngine` as they were.
4. **The setuid bit.** hostfake keeps none (its package doc), and the users check counts only a tool that is setuid root (check-users.md §5), so the checks read a status host through `statusSys`, whose `Stat` shows the bit on the elevation tools the platform installs (`/usr/bin/sudo`, `/usr/bin/doas`), as the users check's own tests do. Tested: `TestStatusSys`.

### Play and the listing

5. **`play status`** opens a menu of the hosts (the sub-menu layout, "Status hosts", `0` to return), and **`play status/<host>`** one host: its note, an info `Callout` titled `status/<host>`, then its main menu, set apart as a scenario's is. Its items are the mock's and lead where the mock's do (`links`, through `session.open` with the mock's name), to the screen mocks on their fixture plans, so `-sequence` and `-fail` are taken as for the mocks and said in play's note; `0` goes back from it (to the hosts' menu, or out of play), `q` quits, and the end of the input aborts (130), as everywhere in play. Play ends with the simulated system's record, "Nothing was applied." unless a mock's workflow applied. Tested: `TestPlayStatus` (an item leading to User Management and back, the host's main menu drawn again from the same reading; `-sequence typed -fail 2` in the note; the end of the input at the hosts' menu), `TestPlayStatusGolden`.
6. **`status [flags] [host | all]...`** lists the hosts with no input: for each, a header `status <host>`, its note, then its main menu as `screen.MainMenu.Render` draws it, down to its prompt, a write each. `-list` prints the hosts' names; `-theme`, `-profile`, `-ascii`, `-override`, and `-width` and `-height` sized as play's (`look.sizeTo`): on a terminal of 40 rows or more, from 64 columns, the full form, as 00-TUI-MAIN.png; below, the compact form; off a terminal 80×24. A name that is not a host, or a flag after a name, exits 2 before anything is read. Tested: `TestStatusGolden`, `TestStatusUsage`, `TestStatusSmoke` (every profile, both themes and glyph sets, at 60 and 80 columns: every host's header, no line wider than the terminal but a header).

### Nothing on this machine

7. **The checks only read, and only the fake.** Every host's engine is built on its fake (`engine.New(h.sys(), engine.Options{…, Hold: s.hold})`), which `TestPlayTouchesNothing` holds to the gallery's own hold as before; `TestStatusHosts` checks every call the checks and the probe made on every host: reads, and commands that only read (`statusReads`: the scenarios' reads and `apt-config dump`, `ip -4 addr show`, `doas -C`, `apk info -e`, `rc-service crond status`), each answered by the model, and the hold never taken. The new code links nothing play did not link already (`pkg/output` imports the checks, the probe and the score).

### Goldens

8. **One golden per host, and two of play**, on `uitest.Core()` (truecolor dark Unicode at 80 columns, plain at 80, truecolor dark Unicode at 60): `testdata/golden/status/<host>.golden` (the listing), `play/status/menu.golden` (the menu, Figma's host chosen, back, out) and `play/status/lockout.golden` (the lockout played directly, then `q`), written with `go test -run 'TestStatusGolden|TestPlayStatusGolden' ./internal/tools/uigallery -update` (`make golden-update` writes the same). At 24 rows a terminal's cells take the compact form; plain mode, which is not a terminal, the full form. **The existing goldens are unchanged.**

### Decisions of this unit

9. None is the maintainer's; each is the gallery's, and none changes hardn:
   - **The rest of the main menu is the mock's**: its items, heading, mode and prompt are `MainMenuFixture`'s, and its items lead to the mocks. The status model fills the blocks it owns (plan row `p4-risk`, `p4-header`); the items, the mode and the sub-screens are slice 2's and each area's slice.
   - **The hosts are the gallery's catalogue**, beside the engine's scenarios, not `pkg/ui/screen` mocks: `pkg/ui` renders only, and a host is read through the engine.
   - **Detect once.** `Engine.Check` detects the facts itself; the gallery keeps them (`Options.Detect`) for the header probe, rather than detecting twice, so the header and the rows are of one reading.
   - **AppArmor's eight lines are hosts**, one line each, on `debian13` but for AppArmor where the line is not a platform's own (Alpine's, a container's).
   - **No `pkg/ui` change**: every line shows with the components as they are. The compact form's overflow at 80×24 (§5, (a)) is shown, not fixed.

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| What each check and the probe read, and in which form: `/etc/passwd`, `/etc/group`, the sudoers and doas files, `visudo -c`, `doas -C`; AppArmor's `enabled`, `mode` and `profiles`, `apparmor.service`; `apt-config dump`, `dpkg-query`, apt's timers and services, `apk info -e`, crond's status and runlevel link; `pve-firewall`'s files and service; the kernel's hostname, uptime, routes, addresses, release and build, `/boot`, `/lib/modules`, `/var/run/reboot-required` | the checks' and the probe's tests (`site.build` in each `_test.go`, their testdata and its README), by reading; [check-users.md](check-users.md), [check-apparmor.md](check-apparmor.md), [check-updates.md](check-updates.md), [check-firewall.md](check-firewall.md), [header.md](header.md) §3 | §2.3 |
| Debian 13, Ubuntu 26.04 and Proxmox VE 9 boot with AppArmor on and profiles loaded; Ubuntu has unattended-upgrades on as installed, a Debian host from debian-installer has no package, Proxmox VE 9 probably none | [check-apparmor.md](check-apparmor.md) §3 and [check-updates.md](check-updates.md) §3 (P-43 (3); Debian's preseed and Proxmox VE **[I]** there) | §2.1: the stock hosts |
| apt's two timers are active and enabled without unattended-upgrades | **[unverified]** | §2.3: the stock Debian and Proxmox VE hosts' Auto Updates, "no unattended-upgrades" whatever the timers |
| A Proxmox VE 9 node as installed: `PermitRootLogin yes`, pve-firewall disabled (datacenter `enable: 0`), no sudo, root alone, OpenSSH 10.0p1's `without-password` | brief 2.5; [check-ssh.md](check-ssh.md) §3; [check-firewall.md](check-firewall.md) §5; [check-users.md](check-users.md) testdata README | §2.1, §2.3 |
| setup-alpine leaves doas with `/etc/doas.d/20-wheel.conf` (`permit persist :wheel`) and the package's `doas.conf` commented out | [check-users.md](check-users.md) testdata README (aports, by reading) | §2.3 |
| Alpine's kernel has no AppArmor unless built and booted with it; a stock Alpine is said not to ship `ss` | [check-apparmor.md](check-apparmor.md); [check-ssh.md](check-ssh.md) §3 **[unverified]** | §2.3: the model answers `ss` on Alpine, as the scenarios' model does; without it, SSH Port's line there is the same, from configuration alone (check-ssh.md §2.12) |
| The compact main menu takes 24 lines at 80×24 with no factor | [risk.md](risk.md) §8 (i) (a probe of `MainMenuFixture`) | §5 (a) |
| `hostfake` keeps no setuid bit | `pkg/host/hostfake` package doc | §2.4 |

## 4. Written, not run (gate 5)

Nothing here runs on a host, and nothing needs to: the gallery runs no command (§2.7). Each line is what the real checks make of the model, which is a model: on a real host each line is its check's, whose host runs are on its page, written, not run ([check-ssh.md](check-ssh.md) §4 and the others). A VM run of the main menu needs a driver that calls `Engine.Check` on a real host, which is not this unit. **By hand, not done here:** `go run ./internal/tools/uigallery status -height 40` and `play status` on a terminal.

## 5. The status lines

Every line the hosts show, as the main menu shows them (the state's word, then its detail). **No wording is new here**: each detail is its check's, proposed on its page ([check-ssh.md](check-ssh.md) §5 and the others; AppArmor's eight lines are decided, P-43 (3)); each level, gloss and factor the score's ([risk.md](risk.md) §6), the bar and host block the probe's ([header.md](header.md) §5). This table is `TestStatusHosts`' (`wantStatuses`), which holds the code to it.

| Host | User | Firewall | SSH Login | SSH Auth | SSH Port | AppArmor | Auto Updates |
|---|---|---|---|---|---|---|---|
| `debian13` | Configured *non-root, sudo* | Configured *deny policy* | Partial *root key-only* | Not Configured *password auth on* | Not Configured *default (22)* | Configured *7 profiles enforced* | Not Configured *no unattended-upgrades* |
| `ubuntu2604` | Configured *non-root, sudo* | Configured *deny policy* | Partial *root key-only* | Not Configured *password auth on* | Not Configured *default (22)* | Configured *9 profiles enforced* | Configured *unattended-upgrades* |
| `alpine324` | Configured *non-root, doas* | Not Configured *ufw not installed* | Partial *root key-only* | Not Configured *password auth on* | Not Configured *default (22)* | Not Applicable *Alpine default* | Not Configured *no apk-cron* |
| `pve9` | Not Configured *root only* | Not Configured *disabled* | Not Configured *root allowed* | Not Configured *password auth on* | Configured *default (22)* | Configured *11 profiles enforced* | Not Configured *no unattended-upgrades* |
| `fresh` | Not Configured *root only* | Not Configured *ufw not installed* | Partial *root key-only* | Not Configured *password auth on* | Not Configured *default (22)* | Configured *7 profiles enforced* | Not Configured *no unattended-upgrades* |
| `hardened` | Configured *non-root, sudo* | Configured *deny policy* | Configured *root disallowed* | Configured *key-only auth* | Configured *non-default (2230)* | Configured *7 profiles enforced* | Configured *unattended-upgrades* |
| `figma` | Configured *non-root, sudo* | Configured *deny policy* | Configured *root disallowed* | Configured *key-only auth* | Configured *non-default (2230)* | Not Configured *no profiles loaded* | Not Configured *no unattended-upgrades* |
| `lockout` | Configured *non-root, sudo* | Partial *ssh blocked* | Configured *root disallowed* | Configured *key-only auth* | Configured *non-default (2230)* | Configured *7 profiles enforced* | Configured *unattended-upgrades* |
| `reboot` | Configured *non-root, sudo* | Configured *deny policy* | Partial *root key-only* | Not Configured *password auth on* | Not Configured *default (22)* | Configured *9 profiles enforced* | Configured *unattended-upgrades* |
| `apparmor-logging` | as `debian13` | | | | | Partial *logging only* | as `debian13` |
| `apparmor-restart` | as `debian13` | | | | | Partial *off after restart* | as `debian13` |
| `apparmor-kernel` | as `debian13` | | | | | Not Configured *off in kernel* | as `debian13` |
| `apparmor-unread` | as `debian13` | | | | | Unknown *can't read* (`FILE_NOT_FOUND`) | as `debian13` |
| `container` | as `debian13` | | | | | Not Applicable *set by the host* | as `debian13` |

| Host | Risk level | Factors | Bar | Host block | Score |
|---|---|---|---|---|---|
| `debian13` | High ≈ weak security | none | Debian 13 (Trixie) | web-01, 192.0.2.20, 3 days, 5 hours, 12 minutes | 47.5 |
| `ubuntu2604` | Moderate ≈ medium security | none | Ubuntu 26.04 (Resolute) | as `debian13` | 67.5 |
| `alpine324` | Critical ≈ no security | none | Alpine 3.24.2 | as `debian13` | 19.4 |
| `pve9` | Critical ≈ no security | up 41 days, no auto updates | Proxmox VE 9.0.10 (Trixie) | web-01, 192.0.2.20, 41 days, 1 hour | 5 |
| `fresh` | Critical ≈ no security | none | Debian 13 (Trixie) | as `debian13` | 17.5 |
| `hardened` | Minimal ≈ hardened security | none | Debian 13 (Trixie) | as `debian13` | 100 |
| `figma` | Moderate ≈ medium security | up 182 days, no auto updates | Debian 13 (Trixie) | web-01, 192.0.2.20, 182 days, 2 hours, 26 minutes | 60 |
| `lockout` | Critical ≈ lockout risk | none | Debian 13 (Trixie) | as `debian13` | 90 |
| `reboot` | Moderate ≈ medium security | reboot pending | Ubuntu 26.04 (Resolute) | as `debian13`, and Reboot pending | 57.5 |
| `apparmor-logging` | High ≈ weak security | none | Debian 13 (Trixie) | as `debian13` | 37.5 |
| `apparmor-restart` | High ≈ weak security | none | Debian 13 (Trixie) | as `debian13` | 42.5 |
| `apparmor-kernel` | High ≈ weak security | none | Debian 13 (Trixie) | as `debian13` | 37.5 |
| `apparmor-unread` | High ≈ weak security | unknown counted as not configured | Debian 13 (Trixie) | as `debian13` | 37.5 |
| `container` | High ≈ weak security | none | Debian 13 (Trixie) | web-01, 192.0.2.20, 1 hour | 41.7 |

**What the goldens show, for the maintainer** (each the code of another unit, shown as it is):

- (a) **The compact main menu overflows 80×24.** From the bar to the prompt it takes 24 lines with no factor (`hardened`, `debian13`), 25 with one (`figma`, `pve9`, `apparmor-unread`) and 26 with a pending reboot (`reboot`: the host block's line and the factor), so on an 80×24 console the bar scrolls off: [risk.md](risk.md) §8 (i), a cap on the factors or none in the compact form, is a `pkg/ui/screen` change for slice 2.
- (b) **A pending reboot shows twice**, the host block's "Reboot pending" (P-21 (a), open) and the factor "reboot pending" ([risk.md](risk.md) §8 (k)).
- (c) **Figma's host reads as Figma draws it**, "Moderate ≈ medium security" with "up 182 days, no auto updates" ([risk.md](risk.md) §5.5).
- (d) **The stock platforms' levels**: Debian 13 High, Ubuntu 26.04 Moderate, Alpine 3.24 Critical ≈ no security (no ufw, passwords, port 22, no apk-cron; AppArmor left out), a Proxmox VE node Critical ≈ no security (score 5: root alone, pve-firewall disabled, `PermitRootLogin yes`, passwords, no unattended-upgrades, up 41 days): the weights' and bands' ([risk.md](risk.md) §8 (a) to (d)).
- (e) **A fresh Debian 13 is Critical**, "no security", though its root has no password login over SSH: Partial "root key-only" earns half of SSH Login, the rest nothing.

## 6. API

The gallery is a main package; its interface is the command line:

```text
go run ./internal/tools/uigallery status [flags] [host | all]...   # the listing; -list for the names
go run ./internal/tools/uigallery play [flags] status              # the hosts' menu
go run ./internal/tools/uigallery play [flags] status/<host>       # one host's main menu
  hosts: debian13, ubuntu2604, alpine324, pve9, fresh, hardened, figma, lockout, reboot,
         apparmor-logging, apparmor-restart, apparmor-kernel, apparmor-unread, container
  flags: -theme, -profile, -ascii, -width, -height, -override; in play -sequence and -fail, for the mocks
```

```go
package main // internal/tools/uigallery, added
const statusPrefix = "status"
type statusHost struct { name, label, about, note string; host func() *fakeHost }
func statusHosts() []statusHost; func findStatusHost(name string) (statusHost, bool); func statusControls() []engine.Control
type hostStatus struct { facts detect.Facts; reports []engine.Report; header probe.Header; level risk.Level }
func (*session) readStatus(*fakeHost) (hostStatus, error); func (hostStatus) mainMenu() screen.MainMenu
func (*session) openStatus(name string) func() error; func statusList(args []string, size sizer, stdout, stderr io.Writer) int
type readings struct { units map[string]unit; packages, addrs map[string]string; aptDump string; crond bool }
func statusOn(platform) *fakeHost; func statusWith(platform, apparmorState) *fakeHost; func (*fakeHost) sys() statusSys
// fakehost.go: platform gains doas bool and proxmox string; pve9Platform; fakeHost gains status *readings
```

## 7. Not in this unit; inputs to later units; size

- **Not shown**: every line of every check. The hosts show the lines a platform gives as installed, the hardened and Figma's hosts, the lockout, a pending reboot and AppArmor's eight; the other lines (each `Unknown`, ufw's other policies, the SSH lines of a move half done, the Auto Updates lines but three, the users' findings) are their checks' tests'. A host per line would be one more entry in `statusHosts()` and a golden.
- **The main menu, live** (slice 2): `cmd/hardn` reads a host as `readStatus` does, on `host.Real`; the gallery's reading is the model for it, and should follow it if that changes.
- **Each slice's sub-screens** read the same host when they move onto the status model; until then the hosts' items lead to the mocks, which show Figma's host.
- **The two models of a host** ([gallery-engine.md](gallery-engine.md) §7): the status's readings add to the gallery's model, which the checks' own fixtures do not share; a change to a check's format must reach its tests and this model alike, which `TestStatusHosts` catches as a wrong line, not a silent one.
- **Size:** about 830 lines of non-test Go added (`status.go` 400, of them about 65 whole-line comments and 50 the hosts' table; `statushost.go` 430, about 90 comments: the model of what five checks and the probe read on four platforms, mostly data), about 100 changed in `fakehost.go`, `play.go` and `main.go`, and 350 of tests, against the guideline of about 500 changed lines; testdata about 2,400 lines of goldens (sixteen files, three cells each). Over the guideline because the model is what seven controls and the probe read on four platforms, which no package can share with the checks' tests; it could have been split as the model and the listing first, then play.
