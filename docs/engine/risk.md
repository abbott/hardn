# The risk level (`pkg/engine/risk`) and the main menu's status blocks (`pkg/output`)

> **Unit:** refactor plan Phase 4, `next/p4-risk` (plan §2, row `p4-risk`: "Weighted scoring documented in one place: the port weighs less than auth; the firewall counts once; the top band is reachable; NotApplicable excluded from the score; Unknown counts as not configured and adds a factor. Factors are shown in RiskSummary. Cross-checks: default-deny + SSH listener not allowed = critical; pending reboot; long uptime without auto updates. AppArmor counts in the risk level"; brief Phase 4, "Risk level" and "Cross-checks"; [P-04](../refactor/proposals.md#p-04), [P-06](../refactor/proposals.md#p-06) and [P-14](../refactor/proposals.md#p-14), proposed, whose scoring the plan row asks for; the maintainer's decision of 2026-10-20 that AppArmor counts, [P-43](../refactor/proposals.md#p-43) (3)). It takes up the inputs the five checks' pages and [header.md](header.md) §7 hand to `p4-risk`. No V-number: origin/main's scoring is an interaction finding ([interaction.md](../refactor/phase0/interaction.md) item 7 and its risk-level row; [phase0-findings.md](../refactor/phase0-findings.md) "Worse than brief §7 assumes").
> **Status:** built and tested on macOS over synthetic reports and headers. The package is pure: it reads no host. Nothing is wired into `cmd/hardn` or the live main menu (plan, "Phases 3–5 together"). **Every weight, threshold and word below is this unit's, proposed** for the maintainer's review, except the level words and glosses, which are origin/main's and Figma's. Fix after review (`next/p4-users-grants`): a grant the User row does not establish was not seen, so a host whose grant hardn cannot read ranked above the same host with the grant read (Minimal against Low); the users check now reports it, and it costs here as a grant without a password (§5.3). Its review: such a grant before a later one for the account as root alone (sudo's `(ALL)`, hardn's own line) was still silent and scored Minimal; the users check now reports it too, and the claims below are limited to the accounts the User row looks at (§8).

**This page is the one place the scoring is documented.** `risk.Assess(reports, header)` scores the host from `Engine.Check`'s reports of the seven status rows and the header probe's uptime and pending reboot, and returns a `Level`: a band (origin/main's five, or Unknown), the score, the factors that explain it, and the cross-checks' own findings. `output.StatusViewOf` maps the reports and the level to the main menu's `RiskSummary` and `StatusList`, the rows in Figma's order; `output.Build` puts the cross-checks' findings in the document's `findings[]`.

## 1. Why

origin/main's risk level (`GetSecurityRiskLevel`, `pkg/security/status.go:177-230`) counts eight equally weighted booleans: root login, the firewall enabled, the firewall configured, secure users, AppArmor, unattended upgrades, a non-default port, password auth disabled. So the firewall counts twice, the port weighs as much as password authentication, and the top band, "Minimal ≈ hardened security", needs more than 8 of 8 and is never shown ([interaction.md](../refactor/phase0/interaction.md) item 7, by harness). Nothing says why a level is what it is, a read that fails counts as "not set" (check-apparmor.md §1), and the SSH rows come from files and the configuration rather than the running host ([P-05](../refactor/proposals.md#p-05)). The checks of Phase 4 now give each row a state read from the host, `Unknown` when it is not established; this unit weighs them.

## 2. Guarantees

1. **Pure, in one place.** `risk.Assess` reads no host, runs nothing, renders nothing (no `host.System` argument); the weights, the credit of each state, the cross-checks' costs and the bands are constants and tables of `pkg/engine/risk/risk.go`, each documented in §5. The level words and glosses are `pkg/output`'s (`levelWords`, `LockoutGloss` in `status.go`); the factors' words and the finding's are `risk.go`'s (each `Factor.Detail`); §6 lists them all.
2. **Seven rows, Figma's**, with weights that sum to 100 (§5.1): User, Firewall, SSH Login, SSH Auth, SSH Port, AppArmor, Auto Updates, as the five areas' `Controls()` give them. **The port weighs less than either auth setting** (5 against 15 and 20); **the firewall is one row** (20), and its lockout forces the band once however many listeners it refuses; **AppArmor counts** (10). Each weight is paired with its row in one table (`rows`), which reads each control for its ID and title alone: `Engine.Check` runs the areas' own `Controls()`, and over them in this order (users, firewall, ssh, apparmor, updates) its reports, and with them the document's `status[]`, come in Figma's order too (`Build` keeps the reports' order), so a slice that gives a control configuration does not edit this package. Tested: `TestRows` (every control of the five packages has its row, by ID and title, in Figma's order; the sum; the port's weight against each auth setting's).
3. **A row earns its weight by its state** (§5.2): Configured all of it, Partial half, NotConfigured none, a Partial line that protects nothing now none (`noCredit`: a firewall with an allow or accept policy or every port open, AppArmor logging only, upgrades on lists nothing refreshes), and none for a Partial line that carries a fact its check left open (`openCodes`: the Firewall's `FIREWALL_SSH_UNCHECKED`, Auto Updates' `UPDATES_APT_VALUE_UNKNOWN` and `UPDATES_SCHEDULE_UNKNOWN`), as Unknown: half would score a firewall off after restart above the same firewall kept on at boot, which is Unknown. The score is what the applicable rows earn, as a share of their weight, in points of 100.
4. **NotApplicable is left out** of both the share and the whole (AppArmor inside a container, or on Alpine without it: the other rows' share of 90). That rescales the other rows, a shortfall with them: with AppArmor Not Applicable, the port on 22 alone scores 94.4, Low, where with AppArmor Configured it scores 95, Minimal (§8 (c)). **Unknown counts as not configured and adds a factor**, one for every such row, "unknown counted as not configured" (P-06's words); so do a row with no report, a report with an error (as `Engine.Check` reports one) and a state outside the five. A grant the User row does not establish costs as one without a password (§5.3): of the accounts the row looks at (uid 1000 or more, that may log in), no grant is taken as hardened that was not established; the others are not modelled (§8). A control's first report counts; a report for a control the score does not weigh is left out. Tested: `TestAssess`, the rows "NotApplicable…", "Unknown…", "… a fact left open" and "a grant not read…"; `TestUsersFromTheCheck` (the users check itself, on `hostfake`).
5. **The cross-checks** (§5.3): a firewall that refuses SSH (`FIREWALL_SSH_BLOCKED`, check-firewall.md §2.6) **forces Critical**, whatever the score, with its own gloss and no factor; ufw set to start at boot with saved rules that may refuse SSH costs 10; a second uid 0 account costs 15; a reboot pending costs 10, and so does one not established outside a container; up 30 days or more without automatic updates costs 10, and so does either fact not established; a user account that becomes root without a password, 5, and so does a grant the User row does not establish. A firewall whose SSH check was not made, and Auto Updates with a fact left open, are factors that cost nothing of their own (their rows earn nothing, item 3). The score after the costs is at least 0. Tested: `TestAssess`, `TestFactors`, `TestFindings`, `TestUpdatesFromTheCheck` and `TestUsersFromTheCheck` (the Auto Updates and users checks themselves, on `hostfake`).
6. **The bands** (§5.4): below 25 Critical, from 25 High, from 50 Moderate, from 75 Low, from 95 Minimal **when no factor is left to explain**, else Low; Unknown when no row's state is established. **Every band is reachable**, the top one by a host with every row Configured and nothing pending, and Figma's host is Moderate (§5.5). Tested: `TestAssess` (each band, each bound: 25, 50, 75 and 95 exactly), `TestFigma`.
7. **The factors** say what the rows below the level do not (§6.2): ufw's next start, the Unknown rows (one line for them all), the users' exposure, the firewall's SSH check not made, Auto Updates' fact left open, the pending reboot, the long uptime. A row that is Not Configured, Partial or Unknown is not repeated as a factor, nor the lockout, which the gloss says: their markers and words are on the lines under the level (design-system README §9.15: repeated words are dropped). They come in a fixed order, each in at most the 35 cells a factor has beside the label at 64 columns (DEV-022). Tested: `TestFactors`, `TestFactorsFit` (every kind, with the longest uptime the header shows).
8. **Findings for the document.** `Level.Findings` holds the cross-checks' own findings, which no report carries: the header's on a pending reboot (`REBOOT_*`) and `UPTIME_NO_AUTO_UPDATES` (new, registered), each only when its facts are established. `output.Run.RiskLevel` puts them in `findings[]` after the reports', with no control ([reference](../reference/output.md) §1.7); the lockout is already there, from the Firewall row. The level changes no exit status: a check's exit is its rows' (output.md §2.8). Tested: `TestFindings`, `TestRiskFindings`; `TestFindingCodes` and `codes.golden` (one line added).
9. **The main menu's blocks** (`output.StatusViewOf`, `RiskSummaryOf`, pure): the rows in Figma's order whatever the reports' order, a row with no report "Unknown" (as the score counts it), a report the score does not weigh after them; each row's title, state and detail, Unknown for a failed check. The band's tone is DEV-016's (Critical and High `RiskHigh`, Moderate `RiskModerate`, Low and Minimal `RiskLow`, Unknown `RiskUnknown`); the level word and gloss origin/main's, "lockout risk" for a forced Critical; the factors one line each. Tested: `TestStatusViewOfFigma`, `TestStatusRows`, `TestRiskSummaryOf`, and the golden `human/status` (`TestStatusBlocks`: Figma's host, a hardened one, a reboot pending, the lockout, a container with most rows Unknown, nothing established; the core cells).

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| origin/main's levels, glosses and bounds: 0–2 Critical "no security", 3–4 High "weak security", 5–6 Moderate "medium security", 7–8 Low "strong security", more than 8 Minimal "hardened security"; eight booleans, the firewall twice | `pkg/security/status.go:177-230` (by reading); [interaction.md](../refactor/phase0/interaction.md) item 7 and its risk-level row (by harness) | §1, §5.4, §6.1 |
| Figma's main menu shows "Moderate ≈ medium security" for a host whose User, Firewall and SSH rows are Configured and whose AppArmor and Auto Updates rows are Not Configured, up 182 days | `_design/hardn-v040/screens/00-TUI-MAIN.png` (reference, not committed) | §5.5 |
| The components' bands: origin/main's Critical and High share `RiskHigh`, Low and Minimal `RiskLow` | [deviations.md](../design-system/deviations.md) DEV-016 | §2.9 |
| A factor has 35 cells beside the label at 64 columns | DEV-022 | §2.7, §6.2 |
| A row's states and details, and which findings ride on which row | [check-ssh.md](check-ssh.md), [check-firewall.md](check-firewall.md), [check-users.md](check-users.md), [check-apparmor.md](check-apparmor.md), [check-updates.md](check-updates.md), each §5 and §6 | §5 |
| `FIREWALL_SSH_BLOCKED` is the row's only critical code; `UFW_SSH_NOT_ALLOWED` and `UFW_SSH_RESTRICTED` are per-listener detail, not scored; `FIREWALL_SSH_UNCHECKED` and `FIREWALL_PVE_LOCAL_ONLY` say the cross-check was not made (the second on an Unknown row) | check-firewall.md §6 | §5.3 |
| ufw with `ENABLED=yes` starts at boot; `UFW_SSH_NOT_ALLOWED_AT_START` is made for each SSH listener its saved rules would not let through from everywhere then: any verdict but OK, so refused, let through from some sources only or from all but some, or not read; it is also made for a socket on 22 that may not be sshd's | [ufw.md](../safety/ufw.md) §2.16; `ufw.Check`'s `atStart` (`pkg/host/ufw/cover.go`) and `pkg/engine/controls/firewall/ufw.go` (by reading) | §5.3 |
| A Partial Firewall line carries `FIREWALL_SSH_UNCHECKED` only where the listeners are not established ("off after restart", "allow policy", "all ports allowed"), and the same firewall with `ENABLED=yes` is Unknown ("ss not installed"); a Partial Auto Updates line carries `UPDATES_APT_VALUE_UNKNOWN` or `UPDATES_SCHEDULE_UNKNOWN` for a fact of its tier left open, which may be whether the lists are refreshed or whether a timer starts at boot, and the same host with that tier's `No` absent is Unknown | check-firewall.md §5, `ufw.go`; check-updates.md §2.7, §5.2, §5.4; `updates.go` `decide` (by reading; and by the review's probes of both checks on `hostfake`) | §5.2, §5.3 |
| AppArmor in complain mode logs and confines nothing | P-43 (3), apparmor(7) as cited there | §5.2 |
| Which Auto Updates lines upgrade the host: Configured yes; Partial "off after restart" yes until the next boot; Partial "lists not refreshed" no (it finds nothing new, **[I]** there); on Proxmox VE `UPDATES_PROXMOX_NOT_UPGRADED`: the kernel and Proxmox VE's packages not upgraded. Partial "off after restart" with `UPDATES_APT_VALUE_UNKNOWN` or `UPDATES_SCHEDULE_UNKNOWN`: check-updates.md says count it as no; here it is not established (the open fact may be a timer's next boot, with upgrades and lists running now), which costs the same and makes no finding | check-updates.md §6 | §5.2, §5.3 |
| `USERS_UID0` is a second root, heavier than `USERS_NOPASSWD`. With the row Configured, every account that may log in is looked at: a grant without a password is `USERS_NOPASSWD`, a grant not established (a rule or `Defaults` not read, a continued line, an alias, a list, a command rule; a tool not read) `USERS_RULE_NOT_READ`; a rule before the deciding one that it does not cover by its runas (sudo's `-g`), one of the two by whether it is read; on an Unknown row `USERS_RULE_NOT_READ` says why it is Unknown; a Partial row has every grant established, and neither | check-users.md §2.9, §2.11; `pkg/engine/controls/users/users.go` `exposure` (by reading; `TestUsersFromTheCheck` runs the check on `hostfake`) | §5.3 |
| `Reboot.HostKernel` is true inside a container (detect's `Container.Detected` Yes) of a distribution the reboot check reads (Debian's family, Alpine); there the kernel is the host's, `Pending` No is a package's request alone, and an Alpine container is Unknown. In a container of another distribution it is false, with `Pending` Unknown and the check's error, so the reboot and uptime cross-checks are made as on a host (the worse score) | [header.md](header.md) §2.7, §5.5; `pkg/engine/header/reboot.go` `readReboot` (by reading; and by the review's probe) | §5.3 |
| Inside a container the uptime may be the host's (LXCFS virtualises it; Docker does not) | header.md §2.6 (**[I]** for Docker) | §5.3 |
| Security fixes for the packages a server runs are published on each supported distribution more often than once a month | **[unverified]** (DSA, USN and Alpine's security tracker not counted for this unit) | §5.3: "long" is 30 days |
| A uid 0 account other than root is root to the kernel and to sshd's `PermitRootLogin` | **[I]** (not read for this unit) | §5.3: why a second root costs more than a password-free grant |

## 4. Written, not run (gate 5)

Nothing here reads a host: the scoring runs where its tests run. Its inputs are the checks' and the header probe's, "written, not run" on their pages; until gate 5 a level is only as right as those readings on a real host. A VM run should compare each host's level and factors with what the host is: the Debian 13 and Ubuntu 26.04 VMs before and after `hardn` hardens them, the Alpine 3.24 VM, and a Proxmox VE 9 node by hand ([vmtest.md](../safety/vmtest.md) §5).

## 5. The scoring

### 5.1 The rows and their weights (points of 100)

| Row | Control | Weight | Why |
|---|---|---|---|
| User | `users.admin` | 10 | An account other than root to log in with and become root from: it lets root's own login be refused, and leaves who did what in the logs. Below the SSH settings, which decide who gets in at all |
| Firewall | `firewall.incoming` | 20 | Whether every service on the host, not only sshd, is reachable from anywhere. **Once**: origin/main counted it twice (enabled, configured) |
| SSH Login | `ssh.login` | 15 | Root's login over SSH: one password or key away from the whole host. Below SSH Auth: with password authentication off, root by key alone resists guessing |
| SSH Auth | `ssh.auth` | 20 | Password or keyboard-interactive authentication open to guessing for every account: the commonest attack on an exposed sshd |
| SSH Port | `ssh.port` | 5 | A non-default port cuts the noise of scans; it stops no one who looks. **The lightest row**, under the brief's rule that it weighs less than the auth settings |
| AppArmor | `apparmor.enforce` | 10 | Confinement of what is already running: defence in depth, after an attacker is in. It counts (the maintainer, 2026-10-20), though hardn does not change it in v0.4 |
| Auto Updates | `updates.auto` | 20 | Fixes for every package, installed without anyone remembering to: as heavy as the firewall and password authentication |

### 5.2 What each state earns

| The row | Earns | Why |
|---|---|---|
| Configured | its weight | the baseline (checks.md §1.3) |
| Partial | half | some of it (checks.md §1.3): root key-only, an SSH allowlist, the firewall or AppArmor off after the next restart, a user with no sudo, 22 beside 2230 |
| Partial, a line that protects nothing now: Firewall "allow policy", "all ports allowed" (ufw), "accept policy" (pve-firewall); AppArmor "logging only"; Auto Updates "lists not refreshed" | nothing | the firewall refuses only what a rule denies, complain mode confines nothing, and upgrades on lists nothing refreshes install nothing new: half would call them half safe |
| Partial, a line that carries a fact its check left open: Firewall with `FIREWALL_SSH_UNCHECKED` ("off after restart"); Auto Updates "off after restart" with `UPDATES_APT_VALUE_UNKNOWN` or `UPDATES_SCHEDULE_UNKNOWN` | nothing, and a factor (§5.3) | as Unknown, by the plan's rule: the open fact may put the row on a line that earns less (SSH refused, the lists not refreshed), and half would score it above the same host with that fact's tier otherwise read, which is Unknown (a firewall kept on at boot, `ss` missing: 80; off after restart with half: 90) |
| Not Configured | nothing | |
| Unknown, no report, a report with an error, a state outside the five | nothing, and one factor for all such rows, "unknown counted as not configured" | the plan's rule: not established is not hardened, and the factor says it was counted so |
| Not Applicable | left out of the share and the whole | the plan's rule: the row is not the host's (AppArmor in a container: the host sets it; on Alpine without it, Alpine's default) |

The score is `100 × earned ÷ applicable weight`, less each factor's cost (§5.3), at least 0.

### 5.3 The cross-checks

| The host | Factor | Costs | Why |
|---|---|---|---|
| the Firewall row carries `FIREWALL_SSH_BLOCKED`: an active firewall refuses new connections to an SSH listener, by its policy or a rule, or lets it through from loopback alone | none: the gloss "lockout risk" and the Firewall row's "ssh blocked" say it | **forces Critical**, whatever the score | the brief's critical cross-check: the next SSH session cannot log in. Counted once, whatever the number of listeners or findings. A failed firewall check that carries it forces it too |
| the Firewall row carries `UFW_SSH_NOT_ALLOWED_AT_START`: ufw inactive, enabled at boot, and its saved rules would not let an SSH listener through from everywhere | "ssh may be blocked after restart" | 10 | **decision of this unit**: SSH gets in now, and `ufw.Check` makes the finding for any verdict at start but OK (refused, from some sources only, from all but some, not read) and for a socket on 22 that may not be sshd's, which the firewall row keeps out of `FIREWALL_SSH_BLOCKED` on purpose; so a lockout at the next boot is possible, not established. It costs as much as a pending reboot, the other fact about the next boot. Whether it should force Critical, or cap the band when a reboot is pending too, is the maintainer's (§8 (f)) |
| the Users row carries `USERS_UID0` | "second root account" | 15 | an account other than root with uid 0 is root by another name, outside every rule about root's login |
| the Firewall row carries `FIREWALL_SSH_UNCHECKED` (not with the lockout) | "can't tell if firewall blocks ssh" | nothing of its own (a Partial line with it earns nothing, §5.2) | the cross-check was not made: the row's state does not depend on it (check-firewall.md §6). Counting it as made would force Critical on every host where `ss` is missing (a stock Alpine) |
| the Auto Updates row Partial "off after restart" with `UPDATES_APT_VALUE_UNKNOWN` or `UPDATES_SCHEDULE_UNKNOWN` | "can't tell if updates fully on" | nothing of its own (the row earns nothing, §5.2) | the fact left open may be whether the lists are refreshed (then nothing new is installed) or whether a timer starts at boot; the code does not say which (§3) |
| a reboot pending (the header's `Reboot.Pending` Yes), the header's findings with it | "reboot pending" | 10 | the running kernel, or what a package asked to restart, is not what is installed: published fixes not in effect |
| the pending reboot not established, outside a container | "can't tell if reboot pending" | 10 | the plan's rule for Unknown |
| the same inside a container (`Reboot.HostKernel`: a container of a distribution the reboot check reads, §3) | none | | the kernel is the host's; only a package's request (`reboot-required`, Yes) counts there |
| up 30 days or more (`LongUptime`) and the host does not upgrade itself: Auto Updates Not Configured, or Partial "lists not refreshed" | "up 182 days, no auto updates" | 10 | brief Phase 4 and P-04: a month of fixes not installed unless someone installs them by hand, which hardn cannot see. **Why 30 days:** security fixes come out more often than monthly on every supported distribution (**[unverified]**, §3), so by a month the running software almost surely lacks some; a shorter span would flag a fresh install that is merely unconfigured, a longer one Figma's own host only at half a year |
| the same, on Proxmox VE with upgrades running but `UPDATES_PROXMOX_NOT_UPGRADED` | "up 182 days, Proxmox not upgraded" | 10 | Proxmox VE's kernel and packages are not among the upgrades (check-updates.md §6) |
| up 30 days or more, and whether the host upgrades itself not established: Auto Updates Unknown, with no report, or Partial "off after restart" with a fact left open | "up 182 days" | 10 | the plan's rule for Unknown; no finding, as nothing is established. The other factor ("unknown counted as not configured", or "can't tell if updates fully on") says why, so this one does not repeat it |
| the uptime not read, and the host does not upgrade itself or that is not established | "uptime unknown" | 10 | the same |
| Auto Updates Configured, or Partial "off after restart" with nothing open; Not Applicable; inside a container | none | | the host upgrades itself; or the uptime may be the host's (§3) |
| the Users row carries `USERS_NOPASSWD` | "no password to become root" | 5 | a stolen session of that account is root at once; lighter than a second root, since the account's own login still guards it. One factor for any number of accounts |
| the Users row, not Unknown, carries `USERS_RULE_NOT_READ` and not `USERS_NOPASSWD`: a grant of an account that may log in that the row does not establish (§3) | "maybe no password to become root" | 5 | **as if found** (`next/p4-users-grants`), by the plan's rule for Unknown, as a reboot or an uptime not established costs what it costs when found: the grant may be one without a password, which is what costs, so the words say so; the account may be the deciding one (a rule before alice's own grant) or able to become root already (`Defaults:bob !authenticate`), where only the password is open. One step milder (no cost, a factor alone) would still keep the host out of Minimal, but would score a grant hardn cannot read above the same grant read (100 against 95), the review's inversion in the score. With `USERS_NOPASSWD` too, that factor alone: the cost counts once for any number of accounts. On an Unknown row it is not scored: the Unknown rows' factor says it, and the row earns nothing |

`UFW_SSH_NOT_ALLOWED`, `UFW_SSH_RESTRICTED`, `SSH_PORT_NOT_IN_EFFECT`, `SSH_PORT_22_BOUND`, `USERS_RULE_NOT_READ` on an Unknown row and every other finding are not scored on their own: they explain a row's state, which is scored (check-firewall.md §6, check-users.md §6). [check-ssh.md](check-ssh.md) §6's hand-off, P-05's cross-check made against what `ss` shows bound, with `SSH_PORT_NOT_IN_EFFECT` and `SSH_PORT_22_BOUND` as lockout signals where the firewall denies a bound port, is met by the Firewall row: `FIREWALL_SSH_BLOCKED` is decided over the bound sockets (check-firewall.md §2.3), and a refused socket on 22 that no configured listener names makes the row Unknown, "can't tell if 22 is ssh" (§2.7 there); so neither code is scored again here.

### 5.4 The bands

| Score (after the costs) | Band | The main menu (§6.1) | Tone (DEV-016) |
|---|---|---|---|
| any, with the lockout | Critical | Critical ≈ lockout risk | `RiskHigh` |
| below 25 | Critical | Critical ≈ no security | `RiskHigh` |
| 25 to below 50 | High | High ≈ weak security | `RiskHigh` |
| 50 to below 75 | Moderate | Moderate ≈ medium security | `RiskModerate` |
| 75 to below 95; 95 or more with a factor | Low | Low ≈ strong security | `RiskLow` |
| 95 or more, no factor | Minimal | Minimal ≈ hardened security | `RiskLow` |
| no row's state established (every applicable row Unknown or with no report; no applicable row) | Unknown | Unknown | `RiskUnknown` |

**Why these bounds:** origin/main's are at a quarter, a half and three quarters of its eight points (at 2, 4 and 6 of 8 a host was still in the lower band, here it is in the upper one: a host that earns exactly half is Moderate). **Minimal** is reachable (P-14): 95 lets the port alone stay on 22, the lightest row, or one 10-point row stand at half (a user with no sudo, AppArmor off after restart), and nothing heavier; and a factor keeps a host out of it, so an Unknown row, a password-free grant of an account the User row looks at (read or not, §5.3; the others, §8) or a pending reboot is never "hardened". The share is of the applicable rows, so with a row Not Applicable the port on 22 alone is below 95 (§2.4, §8 (c)). The integers are compared exactly (`100 × earned − cost × whole ≥ bound × whole`).

### 5.5 Worked examples

Synthetic hosts, each row's state as its check would report it; uptime 3 days and no reboot pending unless the row says otherwise.

| The host | Rows earning (of 100) | Costs | Score | Level |
|---|---|---|---|---|
| **Figma's 00-TUI-MAIN**: User, Firewall, SSH Login, SSH Auth, SSH Port Configured; AppArmor, Auto Updates Not Configured; up 182 days | 10 + 20 + 15 + 20 + 5 = 70 | 10 (up 182 days, no auto updates) | 60 | **Moderate ≈ medium security**, "- up 182 days, no auto updates": **as Figma shows** |
| the same, up 3 days | 70 | 0 | 70 | Moderate |
| every row Configured | 100 | 0 | 100 | Minimal ≈ hardened security |
| every row Configured, SSH on 22 | 95 | 0 | 95 | Minimal |
| every row Configured, SSH Login Partial "root key-only" | 92.5 | 0 | 92.5 | Low |
| every row Configured, AppArmor Not Configured | 90 | 0 | 90 | Low |
| every row Configured, a reboot pending | 100 | 10 | 90 | Low, "- reboot pending" |
| every row Configured, the firewall refusing SSH (Partial "ssh blocked") | 90 | | 90 | **Critical ≈ lockout risk**, no factor |
| every row Configured, ufw off after restart (Partial "off after restart"), whether SSH gets through not read (no `ss`) | 80 (the Firewall's Partial earns nothing) | 0 | 80 | Low, "- can't tell if firewall blocks ssh" |
| the same, ufw kept on at boot (Unknown "ss not installed") | 80 | 0 | 80 | Low, "- unknown counted as not configured": **never below the one above** |
| every row Configured, ufw inactive and set to start at boot, its saved rules refusing SSH (Not Configured "enabled, not running") | 80 | 10 | 70 | Moderate, "- ssh may be blocked after restart" |
| the same, a reboot pending | 80 | 20 | 60 | Moderate, "- ssh may be blocked after restart", "- reboot pending" |
| every row Configured, Auto Updates Partial "off after restart" with `apt-daily-upgrade.timer` "enabled-runtime" (`UPDATES_SCHEDULE_UNKNOWN`), upgrades and lists running now; up 182 days | 80 | 10 | 70 | Moderate, "- can't tell if updates fully on", "- up 182 days"; no finding (`TestUpdatesFromTheCheck`, the check's own row) |
| every row Configured, bob's `NOPASSWD: ALL` read (`USERS_NOPASSWD`) | 100 | 5 | 95 | Low, "- no password to become root" |
| the same grant on a continued line, by an alias or in a list, which the User row does not read (`USERS_RULE_NOT_READ`) | 100 | 5 | 95 | Low, "- maybe no password to become root": **never above the one above** (`TestUsersFromTheCheck`, the check's own row) |
| bob's `ALL=(ALL:ALL) NOPASSWD: ALL` in `/etc/sudoers.d/10-bob`, then hardn's own `bob ALL=(ALL) ALL` (`USERS_NOPASSWD`: the first still decides `sudo -u root -g bob`) | 100 | 5 | 95 | Low, "- no password to become root" (`TestUsersFromTheCheck`) |
| a container: AppArmor Not Applicable, the rest Configured | 90 of 90 | 0 | 100 | Minimal |
| the same, SSH on 22 | 85 of 90 | 0 | 94.4 | Low (Not Applicable raises the other rows' share) |
| a Proxmox VE node: root only, pve-firewall disabled, root key-only (Proxmox), key-only auth, port 22 (its baseline), AppArmor enforcing, no unattended-upgrades; up 41 days | 0 + 0 + 15 + 20 + 5 + 10 + 0 = 50 | 10 | 40 | High, "- up 41 days, no auto updates" |
| SSH Auth "password auth on", Firewall "inactive", AppArmor and Auto Updates Not Configured | 10 + 15 + 5 = 30 | 0 | 30 | High ≈ weak security |
| nothing configured | 0 | 0 | 0 | Critical ≈ no security |
| SSH Port Unknown, the rest Configured | 95 | 0 | 95 | Low, "- unknown counted as not configured" |
| every row Unknown (not read) | 0 | 0 | 0 | Unknown, "- unknown counted as not configured" (and the header's, when it read nothing) |

## 6. The lines (all *proposed*, but the level words and glosses)

### 6.1 The level line

`RiskSummary`'s level and gloss: origin/main's words, Figma's "Moderate ≈ medium security" among them. The one new gloss is "lockout risk", for a Critical the lockout forced: "no security" would describe the score, which may be high. The lockout has no factor of its own: the gloss and the Firewall row's "ssh blocked" say it (design-system README §9.15). [P-06](../refactor/proposals.md#p-06) proposes dropping the gloss for the factors; the gloss is one table in `pkg/output/status.go` (`levelWords`), so that is a change of words and goldens.

| Band | Level | Gloss |
|---|---|---|
| Critical, the lockout | Critical | lockout risk (*proposed*) |
| Critical | Critical | no security |
| High | High | weak security |
| Moderate | Moderate | medium security |
| Low | Low | strong security |
| Minimal | Minimal | hardened security |
| Unknown | Unknown (the component's word) | none |

### 6.2 Every factor, in the order shown

The words are `risk.go`'s (each `Factor.Detail`). Lower case, as the rows' details, but proper names; a factor names what it is about in plain words ("ssh", "firewall", "updates"), never a row's label as the row shows it, and repeats no row's state word or detail. Each is at most 35 cells (`TestFactorsFit`); a day count is whole days.

| # | The host | Factor | Cells |
|---|---|---|---|
| 1 | ufw may refuse SSH once it starts | ssh may be blocked after restart | 32 |
| 2 | a row Unknown or with no report: one line for them all | unknown counted as not configured | 33 |
| 3 | a second uid 0 account | second root account | 19 |
| 4 | the firewall's SSH check not made | can't tell if firewall blocks ssh | 33 |
| 5 | Auto Updates Partial with a fact left open | can't tell if updates fully on | 30 |
| 6 | a reboot pending; not established | reboot pending; can't tell if reboot pending | 14; 28 |
| 7 | up long without automatic updates; on Proxmox VE without its own; whether it upgrades not established (factor 2 or 5 says so); the uptime not read | up 182 days, no auto updates; up 182 days, Proxmox not upgraded; up 182 days; uptime unknown | up to 33 (9999 days) |
| 8 | a user account becomes root without a password; a grant the User row does not establish | no password to become root; maybe no password to become root (*proposed*, `next/p4-users-grants`) | 26; 32 |

Alternatives for the maintainer: "ssh blocked after restart" (right only for a refusal, which the code does not tell from a restricted or unread verdict); "3 rows unread" or "3 counted as off" for factor 2 (a count adds nothing the rows' markers do not show, and "unread" is not always right: check-ssh's "can't tell what listens"); "can't tell if lists refreshed" (exact for `UPDATES_APT_VALUE_UNKNOWN`, wrong for a timer's next boot); "up 182 days, can't tell if updated" for the uptime with the updates not established; "uid 0 besides root"; "sudo or doas without password" (the tool is in the finding, `USERS_NOPASSWD`); for factor 8's second line, "can't tell who can become root" (30, the first proposal: it leaves out the password, which is what costs, and beside "non-root, sudo" reads as if that grant were in doubt) or "can't tell who needs no password" (32), as "can't tell who can become root without a password" is 49 cells and "can't tell if password to become root" 37, over the 35; "kernel update pending" (wrong for a package's request, which is any package's); "reboot pending" is P-21's word for the host block's line too, so a pending reboot shows twice on the main menu until P-21 settles that line (§8). The lockout's former factor, "firewall blocks ssh", repeated the gloss and the Firewall row; it is listed in §8 (h).

### 6.3 The finding

| The host | Code | Detail | Source |
|---|---|---|---|
| up 30 days or more; Auto Updates says the host does not upgrade itself | `UPTIME_NO_AUTO_UPDATES` | up 182 days, no auto updates | `/proc/uptime` |
| the same on Proxmox VE, Proxmox VE's own not upgraded | `UPTIME_NO_AUTO_UPDATES` | up 182 days, Proxmox not upgraded | `/proc/uptime` |

None when either fact is not established (the factor stands, with no finding): the uptime not read; Auto Updates Unknown, with no report, or Partial "off after restart" with a fact left open. None inside a container (`Reboot.HostKernel`).

## 7. API; changes to other units' code

```go
package risk // pkg/engine/risk (new)

type Band uint8 // Unknown, Critical, High, Moderate, Low, Minimal; String
const HighFrom, ModerateFrom, LowFrom, MinimalFrom = 25, 50, 75, 95
const PenaltyAtStart, PenaltySecondRoot, PenaltyReboot, PenaltyUptime, PenaltyNoPassword = 10, 15, 10, 10, 5
const LongUptime = 30 * 24 * time.Hour
const CodeUptimeNoUpdates = "UPTIME_NO_AUTO_UPDATES"
type Row struct { ID, Title string; Weight int }
func Rows() []Row // Figma's order, each with its weight; a copy
type Kind uint8   // KindLockoutAtStart, KindUnknown, KindSecondRoot, KindSSHUnchecked, KindUpdatesOpen, KindReboot, KindUptime, KindNoPassword (USERS_NOPASSWD, or USERS_RULE_NOT_READ on a row not Unknown); String
type Factor struct { Kind Kind; Control, Detail string; Penalty int }
type Level struct { Band Band; Forced bool; Score float64; Factors []Factor; Findings []engine.Finding }
func Assess(reports []engine.Report, h header.Header) Level

package output // added
const LockoutGloss = "lockout risk"
type StatusView struct { Risk component.RiskSummary; Status component.StatusList }
func StatusViewOf(reports []engine.Report, l risk.Level) StatusView
func RiskSummaryOf(l risk.Level) component.RiskSummary
// Run.RiskLevel *risk.Level: its Findings go in findings[] (control "")

package firewall // pkg/engine/controls/firewall, added
const DetailAllowPolicy, DetailAllPorts, DetailAcceptPolicy = "allow policy", "all ports allowed", "accept policy"
```

- **`pkg/engine/controls/firewall`**: the three details as exported constants, used where the literals were (`ufw.go`, `pve.go`), as apparmor's and updates' details are exported for this unit; no line changes, and the tests keep the literal strings. [check-firewall.md](check-firewall.md), that unit's page, is not edited.
- **`pkg/output`**: `status.go` (new), `Run.RiskLevel` and its findings in `Build`, `UPTIME_NO_AUTO_UPDATES` appended to the registry and to `TestFindingCodes`' list, `codes.golden` regenerated (one line), the golden `human/status` added. [output.md](output.md)'s API list predates `StatusViewOf`; that page is `next/p3-output`'s.
- **[docs/reference/output.md](../reference/output.md)**: §1.7 says which findings a risk level adds; §2.2 a row for the code, at the end of the table.
- **[docs/safety/README.md](../safety/README.md)**: an item at the end of "Phase 4 and the slices".
- No depguard rule: the package reaches no host (it imports the controls for their IDs, titles and exported words, and the header for its types). Its tests run the Auto Updates check itself on `hostfake` (`TestUpdatesFromTheCheck`: no command runs). `go.mod` and `go.sum` are unchanged.

## 8. Not modelled; for the maintainer; size

- **Not modelled:** which fact an Auto Updates finding leaves open (a timer's next boot, `enabled-runtime`, is not told apart from the lists' refresh, which only the finding's detail names): both earn nothing and cost the uptime's 10, the worse score; the firewall's other services (only SSH's reachability is checked; a deny policy with a rule opening a database to everyone is Configured); the time since the last upgrade by hand (dpkg's and apk's logs are not read), so a host upgraded by hand every day without unattended-upgrades costs the uptime's 10 after 30 days ([P-44](../refactor/proposals.md#p-44), deferred); the kernel pinned for the next boot (header.md §7); how many profiles AppArmor enforces (any enforcing profile is Configured: the count depends on what is installed, not on how well it is confined; `apparmor.Enforced` is not read); what the User row does not look at, which scores as if no account had the grant (check-users.md §6): a passwordless grant to an account below uid 1000 with a login shell (`useradd -r -s /bin/bash`), or to an account of uid 1000 or more that cannot log in (`nologin`), through which a service running as it becomes root; NSS accounts and groups (a rule hardn reads for a group `/etc/group` lacks is no one's), and sudo rules from LDAP or sssd; weights per distribution (Proxmox VE's root key-only and port 22 are Configured there, by its baseline).
- **For the maintainer** (every item is a proposal):
  - (a) the weights (§5.1), and SSH Login below SSH Auth;
  - (b) Partial as half, and the five Partial lines that earn nothing (§5.2); the alternative, Partial as nothing, makes a stock root key-only login count as "root allowed";
  - (c) the bounds (§5.4), Minimal from 95 with no factor, and a host whose port alone is 22 "hardened"; and with a row Not Applicable (a container, Alpine without AppArmor), the same port on 22 Low (94.4 of the 90 points' share), since the share rescales the shortfall: comparing Minimal on the points lost (at most 5) rather than on the share would treat both alike;
  - (d) the costs (§5.3), and 30 days as "long";
  - (e) the not-established cross-checks costing as if found (a reboot or uptime not read), by the plan's rule for Unknown, but not the firewall's SSH check, which would force Critical on a stock Alpine; a Partial line with a fact left open earning nothing, as Unknown (§5.2); and inside a container, no uptime cross-check and only a package's reboot request;
  - (f) `UFW_SSH_NOT_ALLOWED_AT_START` a factor that costs 10 (§5.3), not a forced Critical; the alternatives, with a reboot pending too (the host asked to reboot into the lockout): force Critical with the gloss "lockout risk", or cap the band at High; the code alone does not tell a refusal from a restricted or unread verdict, which `ufw.Check` gives the same finding; and on Proxmox VE with `UPDATES_PROXMOX_NOT_UPGRADED`, Auto Updates still earning its weight (the row is Configured by its check), the cost being the uptime's;
  - (g) a factor only for what the rows do not show; the alternative lists every row below Configured too ("AppArmor not configured", as the Phase 1 fixture `risksummary/factors` does);
  - (h) every word of §6, and "lockout risk"; the lockout with no factor of its own, the gloss and the Firewall row saying it (the alternative keeps the factor "firewall blocks ssh" and the gloss "no security"); one factor for every Unknown row (the alternative names each, "SSH Port unknown", which repeats the row's label and state word); and "Critical ≈ no security" for a host whose rows are mostly Unknown, a gloss that states what was not established (an alternative: no gloss when the Unknown rows' factor is shown);
  - (i) **the main menu's height** (for `next/p4-gallery-status` and slice 2): each factor is a line, and the compact main menu is exactly its 24-line budget without one (checked with a probe of `screen.MainMenuFixture` at 80×24: 24 lines with no factor, 25 with one, 27 with three), so Figma's own host, with its one factor, overflows an 80×24 console by a line; a host with nothing read shows three factors. A cap, or no factors in the compact form, is a change to `pkg/ui/screen`, not made here;
  - (j) the risk level in the JSON document: not added; the brief's nine fields carry its findings, and the level is the main menu's. A `risk` object (band, score, factors) would be an addition `schemaVersion` 1 allows (reference §1.1);
  - (k) the pending reboot shown twice, the host block's "Reboot pending" (P-21 (a), open) and this factor; P-21 says nothing sets the host block's line before P-04 is approved, so until then the factor is the one place it shows;
  - (l) a host with no sshd: its SSH rows are Unknown ("sshd not installed", check-ssh.md §5), so they count as not configured (55 points) and add the Unknown rows' factor, though no SSH is exposed; NotApplicable there is the SSH check's to decide, not the score's;
  - (m) for the orchestrator: this unit builds [P-06](../refactor/proposals.md#p-06)'s scoring and [P-14](../refactor/proposals.md#p-14)'s reachable bands, as the plan row asks; their wording parts (the gloss, the factors' words) stay proposed.
  - (n) from `next/p4-users-grants`: a grant the User row does not establish costing 5, as found, rather than nothing of its own; its words, "maybe no password to become root"; and what makes it: any grant of an account that may log in not read, a command rule included (`bob ALL=(root) NOPASSWD: /usr/bin/systemctl restart app`: what a command allows is not read), and a tool not read (doas installed off Alpine), so such a host is Low at best.
- **Size:** about 530 lines of non-test Go (the package about 420, of them about 105 whole-line comments; the mapping about 100, with `Build`'s and the registry's 14; the firewall's constants 13) and about 760 of tests, against the plan's guideline of about 500 changed lines: one test row per rule of §5 and per band bound, the Auto Updates check run on `hostfake`, and the mapping's golden. The review's fixes (the open facts, one Unknown factor, the lockout's factor dropped, the cost of ufw's next start, the rows' one table) are about 35 of the package's lines and 110 of the tests'. `next/p4-users-grants`: about +32 −10 of the package and +114 of tests (`TestUsersFromTheCheck` builds the users check's host on `hostfake`), with the users check's +50 −16 and +70 −3; its review fixes about +8 −4 of the package and +60 −25 of tests (the host with several drop-ins, hardn's own grant from `sudoers.PlanFor`).
