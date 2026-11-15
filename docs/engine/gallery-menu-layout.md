# The main menu's layout

`next/menu-layout`: the main menu's rows and items, as the maintainer adopted them on 2026-11-14, built in hardn.

**Status: adopted and built. Not run on a Linux host.** The maintainer answered the design step's decisions on 2026-11-14 ("sounds good"; "we can show them when there are problems"; "perhaps we should use '~'"; §6), then, asked "Do you adopt my recommendations for these eight, with \"baseline default\" for Kernel?", wrote: **"yes, all approved. proceed"**. `make check` passes on macOS. Dates are the maintainer's local ones (EST).

On a Debian 12 host with everything configured, with no colour:

```
  [*] Risk Level           Low ~ strong security

  [+] User                 + non-root, sudo
                             ~ No password to become root
  [+] SSH Login            + root disallowed, key-only auth
  [+] SSH Port             + non-default (2230)
  [+] Firewall             + deny policy
  [+] Kernel               + 6 items applied
  [+] Auto Updates         + unattended-upgrades
  [+] AppArmor             + 10 profiles enforced

----------------------------------------------------------------

  Select an option         Mode Live

   1) User                 Create, manage (sudo, SSH keys)
   2) SSH Login            Disable root and passwords, set port
   3) Firewall             Manage rules, port listening
   4) Kernel               Harden sysctl (network, kernel)
   5) Auto Updates         Enable automatic updates
   6) Run All              Queue hardening actions
   7) Backup               Configure hardn backup
   8) Configuration        hardn.yml, dns, ntp, syslog
   9) System Details       View system info
  10) Logs                 View hardn logs

   0) Exit                 q to quit
```

## 1. What to run

```
go run ./internal/tools/uigallery menu-layout [-ascii] [-profile plain] [-width 60] [mock | all]
go run ./internal/tools/uigallery menu-layout -list
```

The mocks are `internal/tools/uigallery/menulayout.go`; the goldens `internal/tools/uigallery/testdata/golden/menu-layout/`.

## 2. The mocks

Each mock is a set of reports, written in the mock and not read from a host, drawn through hardn's own code: `risk.Assess`, `output.StatusViewOf`, `screen.MainMenu` and the main menu's items (`screen.MainMenuItems`). No field is the mocks' alone any more.

**The mocks of the forms that were not adopted are gone**, not kept as rejected: brackets and bold brackets, a sign for every state, option A (no Kernel row), the three info rows always, SSH Auth in the User row, "today". git keeps none: the design step was never committed. §6 keeps what they showed, in words.

| Mock | What it shows |
| --- | --- |
| `menu` | The whole menu on the configured host: the text above (`TestMenuLayoutMenu`, cell for cell) |
| `mixed` | Partial, Not Configured, Unknown and Not Applicable rows beside Configured ones, with colour and without |
| `ssh` | The SSH Login row with SSH Auth in it: nine pairs of states |
| `kernel` | The Kernel row: 6 items applied, 1 item, baseline default, 3 to set, never applied, in a container |
| `problems` | DNS, NTP and Syslog: no row, one row, all three, no value, not read |
| `long-detail` | Details of 35 and 40 cells after "+" |
| `config-menu` | Main menu 8: hardn.yml, DNS, NTP, Syslog |
| `el` | Rocky Linux: the rows, and item 3 |
| `pve` | Proxmox VE: the rows, and items 3 and 5 |

## 3. What was adopted

D1 to D9 are the orchestrating request's list; each names the decision of §6.2 it answers.

- **D1. A Kernel row** between Firewall and Auto Updates, on every host, Not Applicable in a container (1 (a), 5 (a)). Run All still applies Kernel only with hardn.yml's `kernel: true`.
- **D2. Kernel is scored**: weight 5, taken from Auto Updates, 20 to 15 (4 (a)). A host hardened with v0.10.0 scores 95 until Kernel is applied.
- **D3. "+" for "Configured"** (13 (a), 8 (a), 15 (a)): a Configured row with a detail shows "+", in the Configured colour, then the detail. A Configured row with no detail keeps the word, and every other state keeps its word. On every status row of every screen. The Markdown report, `hardn check`, the JSON and the logs keep the word.
- **D4. SSH Auth is in the SSH Login row** (14 (a), 16 (a)), display only: the control `ssh.auth`, its weight 20, its forced Critical, factors, findings, codes and JSON entry stay.
- **D5. The Kernel row's detail** (18 (a), §6.5): "+ 6 items applied", the settings hardn's file sets; "+ baseline default" where the host meets the baseline and hardn's file sets none; the other states as before.
- **D6. Main menu 4 is Kernel; DNS is under Configuration** (3 (a)): 1 hardn.yml, 2 DNS Settings, 3 NTP Settings, 4 Syslog Settings.
- **D7. DNS, NTP and Syslog have a row only with a problem** (17 (a), 11 (a), 12 (a), §6.5), after a blank line under AppArmor, in that order: the bullet as its mark, no state word, the host's value, and the problem on a line under it in the warning colour. They never count in the risk level. "not reached" is "unreachable" everywhere.
- **D8. A line under a status row starts with "~"** (19, §6.5), two cells in from the value column.
- **D9. The items' words** (20 (a)), above. On Rocky Linux, AlmaLinux, RHEL, CentOS Stream and Oracle Linux item 3 is "Manage firewalld, port listening"; on Proxmox VE items 3 and 5 keep v0.10.0's words, "View pve-firewall" and "Configure automatic updates".

## 4. Built

### 4.1 What, and where

| | Where | Tests |
| --- | --- | --- |
| D1, D2 | `pkg/engine/risk/risk.go` (`rows`: Kernel 5, Auto Updates 15, no DNS); `pkg/engine/controls/status.go` (`Status`: the scored controls, then DNS; `Menu`: then NTP and Syslog; `Info`) | `TestRows`, `TestAssess` (Kernel never applied 95, Partial 97.5, a container, not read, no report; every bound and example rescored), `TestStatus`, `TestMenu`; `pkg/screens/mainmenu`'s `TestKernelSettingsRow` |
| D3, D8 | `pkg/ui/component/status.go` (`StatusRow.Render`, `ConfiguredSign`, `LineMark`, `LineIndent`, `KeepWord`); `pkg/output/human.go` sets `KeepWord` for `hardn check` | `TestLayout` (the sign, a row with no detail, `KeepWord`, no other state takes a sign, lines side by side and stacked), `TestStatusSignStyles`, `TestLineStyles` |
| D4 | `pkg/output/status.go` (`StatusViewOf`, `mergeSSH`); the report keeps two rows (`ReportRows`) | `TestMergeSSH` (20 pairs, the lines, a line both have once), `TestStatusRows`, `TestAlertOf`, `TestReportRows`, `TestMarkdownDocuments`; `pkg/screens/ssh`'s `TestSSHHosts` (19 hosts) |
| D5 | `pkg/engine/controls/kernelsettings` (`Status`, `Applied`, `DetailBaseline`); `pkg/host/sysctl/own.go` (`Own.Settings`, a count by setting) | `TestApplied`, `TestApplyAndRemove`, `TestOwnSettings` |
| D6 | `pkg/ui/screen/configgroup.go`, `pkg/screens/config/group.go`, `cmd/hardn/readers.go` (`runConfigGroup`, `withReaderScreens`), `pkg/screens/mainmenu/run.go` (`Options.Kernel` for `Options.DNS`) | `TestConfigGroup`, `TestGroupKeys`, `TestGroupEnds`, `TestGroupNeeds`, `TestMainMenuKeys`, `TestKernelItem`, `TestMenuOptions`, `TestConfigScreenHook` |
| D7 | `engine.Status.Problem` and `Value`, set by the three checks (`pkg/engine/controls/dns`, `timesync`, `logforward`); `output.StatusViewOf` (`StatusView.Info`, `infoRow`); `StatusRow.Info` and `Problem`; `screen.MainMenu.Info` | each control's `TestCheck`, `TestStatus` or `TestProblem`; `TestInfoRows`, `TestMainMenuOf`; `TestMainMenuInfo`; `pkg/screens/mainmenu`'s `TestProblemRows`, `TestNothingRead`; `cmd/hardn`'s `TestReadStatus` |
| D9 | `pkg/ui/screen/main.go` (`MainMenuItems`), bound to the screens by `pkg/screens/mainmenu/run.go` (`items`) | `TestMainMenuItems` (every item's words), `TestFirewallItemOnPVE`, `TestMenuOf` (the five EL distributions), `TestMenuLayoutFits` |

The gallery: every status host has kernel values, Debian's, Alpine's or Rocky Linux's as read on 2026-11-14 (`statusWith`), and `hardened` has them applied; `play`'s main menu 4 leads to Kernel Settings and its 8, then 2, to DNS. `internal/tools/p4status`'s five host fixtures were written again from the gallery. 231 goldens changed; §5 says how they were checked.

Docs: README, `docs/usage.md` (a section "The status rows"), `configuration.md`, `platforms.md`, `reference/output.md` §8, `hardn.yml.example` and its embedded copy, `docs/engine/risk.md` (§5.1, §5.5, §6.2), a dated note on each affected design page, DEV-076, and `docs/releases/unreleased.md`.

### 4.2 Choices made where the list was silent

1. **The SSH Login screen has the merged row too**: two rows, SSH Login and SSH Port. It is what the main menu shows, so the row reads the same in both (8 (a)); the section's Root SSH Login and Password Login rows say each part. The alternative, three rows there, would make the screen and the menu disagree about what "SSH Login" is.
2. **The merged row's detail is the details of the parts in the row's state**, SSH Login's first. Both Configured: both. One off: that one. Both off, one worse (root key-only, Partial; passwords on, Not Configured): the worse one alone, "Not Configured password auth on", so the word and the detail are about the same part and the row fits. Both off alike: both, and at 64 columns the pair is cut: "Not Configured root allowed, passw...". That is a Proxmox VE node as installed. A part read as off outranks one not read (Not Configured, then Partial, then Unknown): sshd's passwords on and root's keys not read is "Not Configured password auth on" with "~ Counted as not configured" under it, where the row first built said "Unknown can't read" and hid the passwords. One part Configured and the other not read is still Unknown. On Proxmox VE the Configured pair leaves out "(Proxmox)" to fit: "+ root key-only, key-only auth" (28 cells; with it the pair was cut); the SSH Login detail alone, `hardn check`, the JSON and the report keep "root key-only (Proxmox)". *The pair's words are proposed.* A line both controls have ("Counted as not configured") is under the row once.
3. **Not Applicable is the least bad state in the merge**: one part that does not apply says nothing of the other. With no SSH server both are Not Applicable and the row is too.
4. **"1 item applied"**, in the singular, for one setting.
5. **hardn's file with no setting in it** reads "baseline default", as no file does.
6. **Run All's rows are unchanged**, v0.10.0's: User, SSH Login, SSH Auth, SSH Port, Firewall, DNS, Auto Updates, AppArmor, then Syslog, NTP and Kernel where hardn.yml asks, then Backup. Run All's rows are outcomes, not status rows: none says "Configured", and SSH Auth keeps its own, the openssh upgrade on Alpine. Its list is its own now (`runall.IDs`), no longer the risk level's rows.
7. **The host block's reboot line starts with "~" too**, two cells in from the value column: "~ Reboot pending", "~ Can't tell if reboot pending" (the maintainer, 2026-11-15: "yes, make the change please"). Stacked, it is two cells in from the stacked value, as a row's line is. It first kept "-", D8 being the lines under a status row.
8. **A problem's words**: DNS "Not hardn.yml's", "Not valid in hardn.yml", "resolved not running"; NTP "Not in sync", "Not hardn.yml's", "<service> not running" (the service's name as it is, "chrony not running"), "No time service"; Syslog "Unreachable", "N unreachable", "Not hardn.yml's", "Not forwarding", "Connection not read"; each "Can't read" where nothing is read.
9. **The host's value**: DNS, the nameservers in use, comma-separated; NTP, the server the service uses, or "N servers" for several; Syslog, the server, or "N servers". With no value the problem is the row's value, in the warning colour, with no line under it.
10. **A Syslog row for settings hardn can't read shows on every host**, asked or not: D7's rule. v0.10.0 left it out where hardn.yml named no server.
11. **A hardn.yml list that is not valid counts as naming servers**, for "Not forwarding" and "No time service".
12. **The report to hand over** lists the scored controls in the main menu's order, SSH Login and SSH Auth each its own, then DNS, fine or not, the state in words. DNS was between Firewall and Auto Updates.
13. **`internal/tools/p4status` reads the scored controls and DNS**, not NTP or Syslog, as before: it draws no row for a problem of theirs.
14. **The Configuration item is "DNS Settings", "Configure nameservers"**; the screen's title stays "DNS Configuration".
15. **The gallery's sysctl "menu" mock draws the main menu's items**, where it drew the Configuration group.
16. **`hostfake.System.ResetCalls`** is new: the gallery's hardened host applies its kernel settings through the host, and the fixtures' exporter refuses a host that was written to.
17. **`controls.MenuReports` and the three `Shown` functions for the main menu are gone**: `output.StatusViewOf` draws a row only for a report with a problem. `kernelsettings.Shown` stays, Run All's.
18. **The words are `pkg/ui/screen`'s** (`MainMenuItems`), so that the gallery draws hardn's own items without importing `pkg/screens/mainmenu`, which builds an engine.

### 4.3 What a host now shows differently, beyond the list

- Its score, where Auto Updates is off (5 more) or Kernel is not Configured (5 fewer): the same host with both off scores as before.
- A line fewer for SSH Auth, DNS and NTP each, and a line more for Kernel: 37 lines at 80 columns on the hardened host, where v0.10.0 had 39.

## 5. Not verified

- **Nothing ran on Linux.** No row here was read from a host: every test is in process, on macOS, over the fake host.
- **The 231 changed goldens were not read one by one.** In 218 the plain 80-column cell was compared, old against new, by a script: every changed line is a Configured row's "+", a line's "~", "unreachable", a menu item, an SSH Auth, DNS, NTP or Syslog row gone, the merged SSH Login row, a Kernel row, or a gallery note. Eight lines were none of these and were read: six "Counted as not configured" said once under the merged row, Kernel's "Configured by hardn", and a syslog server's name cut eight cells later. No row that is not Configured lost its word. The other 13 have no such cell (`pkg/ui/component`'s eight, the live main menu's four, System Details' parity record): three were read. The Unicode cells and the 60-column cells were read in two files only.
- **The kernel values of the gallery's Ubuntu and Proxmox VE hosts** are Debian's: neither was read.
- **`Own.Settings` on busybox**: the count by setting is tested on the model's file, a line an interface, not on Alpine.
- **The README's screenshot** is v0.10.0's menu.
- **Whether "Can't read" shows on a host that works**: a logger or a time service hardn does not read would now show a row on every main menu. None of the gallery's hosts has one, once Alpine's host had its stock `/etc/conf.d/syslog`.
- **The levels of §6.2's decision 4** were worked out by reading; `TestAssess` now runs them.

## 6. The record: what was proposed, and the answers

The design step's page, as it stood before the build. The mocks it names (`today`, `option-a`, `option-b`, `brackets`, `plus`, `plus-user` and the rest) are gone (§2).

- **Option A:** seven scored rows, a blank line, then DNS, NTP and Syslog as info rows: a "-" mark, no state word. Main menu 4 is Kernel; DNS goes under Configuration.
- **Option B:** as A, with a Kernel row between Firewall and Auto Updates.
- **Brackets:** as B, a Configured row without its word, its detail in green brackets.
- **Plus** (later the same day): as B, "+" for "Configured", SSH Auth in the User row, no info rows.

### 6.1 The Plus alternative (was §3)

The maintainer's text of 2026-11-14, after the scenarios, `plus`:

```
   ⛬  Risk Level           Low ≈ strong security

   ⛛  User                 + non-root, sudo, key-only auth
                             ⭘ No password to become root
   ⛛  SSH Login            + root disallowed
   ⛛  SSH Port             + non-default (2230)
   ⛛  Firewall             + deny policy
   ⛛  Kernel               + 6 items applied
   ⛛  Auto Updates         + unattended-upgrades
   ⛛  AppArmor             + 10 profiles enforced

╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌

  Select an option         Mode Live

   1) User                 Create, manage (sudo, SSH keys)
   2) SSH Login            Disable root and passwords, set port
   3) Firewall             Manage rules, port listening
   4) Kernel               Harden sysctl (network, kernel)
   5) Auto Updates         Enable automatic updates
   6) Run All              Queue hardening actions
   7) Backup               Configure hardn backup
   8) Configuration        hardn.yml, dns, ntp, syslog
   9) System Details       View system info
  10) Logs                 View hardn logs

   0) Exit                 q to quit
```

What differs from B and Brackets:

- "+" stands before a Configured row's detail, for the word or the brackets.
- No SSH Auth row: "key-only auth" joins the User row's detail.
- The line under a row has "⭘" for "-", two cells further in: under the detail of a row with a "+".
- No DNS, NTP or Syslog rows.
- Kernel reads "6 items applied".
- Item 1 is "User", for "User Management".
- Items 1, 2, 3, 4, 5, 6, 9 and 10 have new descriptions.

What his text does not say, and the mocks found:

- **Rows that are not Configured** (`plus-mixed`). With a sign for each, "-" for Not Configured is also the bullet and Not Applicable's ASCII mark "[-]".
- **The merged User row** (`plus-user`). "Not Configured non-root, sudo, password auth on" is 47 cells of 37 and is cut. So is every pair with a part off, while the words are kept. The longest Configured pair hardn has words for, "non-root, sudo, key and password", is 32 cells and fits after "+".
- **"6 items"** (`plus-kernel`). The Kernel screen has 19 settings (`sysctl.Specs`) and judges 18: Forwarding is shown only. A router or a Proxmox VE node has 2 more left alone, and a host with IPv6 off lacks IPv6 redirects. hardn reads three numbers:
  - what hardn's file sets (`Reading.Own.Entries`). These are lines, not settings: Source routing is up to two, and on busybox a setting is a line for each interface. A count by setting is new code.
  - how many are at the baseline (`Reading.Count(Pass)`).
  - how many are judged: 18, less those left alone or absent.
  A host at the baseline as installed has no file, and its row is a bare "Configured".
- **Items 3 and 5 elsewhere** (`plus-el`, `plus-pve`). On Rocky Linux and AlmaLinux the Firewall screen installs, enables or disables firewalld and has no rules to manage. On Proxmox VE it is read only and has no Ports section, and the Auto Updates screen's one item is disabled, "Not on Proxmox VE".
- **With no info rows** NTP not in sync, the syslog server not reached and DNS not hardn.yml's are no longer on the main menu. Each is still its own screen's row under Configuration. None is in the risk level, today or here.

Plus and the earlier decisions:

| Decision | Plus |
| --- | --- |
| 1, Kernel row | Adopted: (a). His text has it |
| 2, brackets | Restated as 13, a choice of three |
| 3, item 4 Kernel | His text has (a)'s items 4 and 8. The order inside Configuration is not shown: open. "Configure sysctl" becomes "Harden sysctl" (20) |
| 4, 5 | Adopted |
| 6, "applied" | Replaced by 18; asked only with 18 (c) |
| 7, bold brackets | Asked only with 13 (b), brackets |
| 8, where the form applies | Open, and asked of "+" too |
| 9, other state words | Restated as 15 for "+"; asked only with 13 (b) |
| 10, 11, 12, info rows | 10 is asked only with 17 (c); 11 and 12 with 17 (a) or (c); none with 17 (b) |

### 6.2 Decisions for the maintainer (was §4)

Gates first. In each, (a) is recommended. Decisions 13 to 20 came later and restate or replace some of 1 to 12: such a decision says so first, has no recommendation of its own where the later one decides it, and is answered there.

1. **A or B: a Kernel status row.** *Adopted: (a). The maintainer, 2026-11-14, "sounds good", to: "every host shows a Kernel row, and one that hasn't applied it loses 5 points."* Gates 4, 5 and 6. Drawn: `option-a`, `option-b`, `kernel`.
   - (a) B. Kernel becomes main menu 4 (decision 3), and items 1, 2, 3 and 5 each have a row; Kernel alone would have none. One line more than A.
   - (b) A: no row. 4, 5 and 6 fall away; the Kernel screen's own row is the only state shown. Hosts with `kernel: true` lose the Kernel row the main menu gives them today, before AppArmor.
2. **Drop "Configured": brackets.** *Restated as 13: answer there. (a) here is 13 (b), (b) is 13 (c).* Drawn: `brackets`, `mixed`, `brackets-no-colour`, `long-detail`.
   - (a) Brackets, as drawn. A Configured row shows `[detail]`, the brackets green, the detail as today. Hardened rows go quiet and the rows that need work keep their words. A detail gets 35 cells where it has 26; a longer one is cut before the closing bracket. A Configured row with no detail keeps the word. With no colour the mark alone says Configured: `[+] SSH Login  [root disallowed]`. On a plain terminal the mark and the hostname are in brackets too.
   - (b) Keep the word, as today. Nothing changes.
3. **Main menu 4 is Kernel; DNS moves to Configuration.** Gates nothing, but every reference below. Drawn: `option-a`, `config-menu`.
   - (a) *Recommended:* his order. Main menu 4 "Kernel  Configure sysctl (network, kernel)"; 8 "Configuration  hardn.yml, dns, ntp, syslog"; inside it 1 hardn.yml, 2 DNS Settings, 3 NTP Settings, 4 Syslog Settings. It matches the info rows' order. Syslog moves from 2 to 4, NTP stays 3, Kernel leaves.
   - (b) Today's order inside: 1 hardn.yml, 2 Syslog, 3 NTP, 4 DNS. Syslog's references stay as they are. Item 8 then reads "hardn.yml, syslog, ntp, dns", the items' order, not his words.
   - What changes, by what each reference means. Release notes are history and stay. The prompt's hint stays "[0-10 or q]".
     - **DNS at main menu 4** becomes "main menu 8, then 2" ((b): "then 4"). As "the main menu's 4": `internal/tools/uigallery/play.go:30`, `dnsscreen.go:27,34`, `dnsscreen_test.go:213,229`, `pkg/screens/mainmenu/run_items_test.go:16`. As "4) DNS": `docs/usage.md:12`, `docs/design-system/README.md:765`, `internal/tools/p4status/main_test.go:198`, `pkg/ui/screen/screen_test.go:576` and 51 goldens (the gallery's status, eol, release-check, release-notice and play; `pkg/ui/screen`'s main-menu). As "main menu 4": `docs/engine/parity-system-details.md:252-253`, `pkg/screens/systemdetails/parity_test.go:1127-1128` and its golden.
     - **Every other "main menu 4" means Firewall**, origin/main's item 4: history, and stays. gallery-firewall.md, screen-firewall.md, control-firewall.md, parity-firewall.md, dryrun-mode.md, cmd-engine.md, `docs/design-system/README.md:18`, `pkg/ui/screen/firewall.go:11`, `pkg/screens/firewall/firewall.go:2` and 49 legacy goldens of `pkg/screens/firewall`.
     - **Kernel at "main menu 8, then 4"** becomes "main menu 4": `hardn.yml.example:71` and its copy `pkg/config/embed.go:82`, `docs/platforms.md:54`, `cmd/hardn/kernelsettings.go:5`, `pkg/ui/screen/kernelsettings.go:13`, `pkg/screens/kernelsettings/kernelsettings.go:11` ("item 4 (main menu 8)"), the gallery's `sysctl.go:39,218` and `sysctl_test.go:112`, and gallery-sysctl.md.
     - **Syslog at "main menu 8, then 2"**, with (a), becomes "then 4": `hardn.yml.example:51` and `pkg/config/embed.go:62`, `docs/platforms.md:21`, `cmd/hardn/logforward.go:5`, `pkg/screens/logforward/logforward.go:11` ("item 2 (main menu 8)"), the gallery's `logforward.go:33,43`, and gallery-logforward.md:18. NTP's "then 3" stays.
     - The Configuration group's own goldens change with its items.
4. **Is the Kernel row scored?** *Adopted: (a), with 5 (a). The maintainer, 2026-11-14, "sounds good", to: "every host shows a Kernel row, and one that hasn't applied it loses 5 points."* Needs 1 (a). Today the rows' weights are User 10, SSH Login 15, SSH Auth 20, SSH Port 5, Firewall 20, DNS 0, Auto Updates 20, AppArmor 10 (`pkg/engine/risk/risk.go`), and Kernel is never counted ([gallery-sysctl.md](gallery-sysctl.md), decision 18 (a)).
   - (a) Scored, weight 5, taken from Auto Updates (20 to 15), which also costs 10 through "Up 182 days, no updates". A host hardened by v0.10.0, Kernel not applied, scores 95: still Minimal when no line is under a row.
   - (b) Weight 10: 5 from Auto Updates and 5 from Firewall, 15 each. The same host scores 90: Low until Kernel is applied.
   - (c) Shown, not weighed: weight 0, as DNS is today. No host's level changes, but an unscored row sits among the scored ones.
   - A fresh host (root alone, no ufw, sshd's defaults, AppArmor enforcing) earns 17.5 of 100 today and in (a), (b) and (c): Critical. In a container the row is Not Applicable and left out of the score. Partial earns half. Worked out by reading risk.go, not run.
5. **Do the Kernel row and Run All still need `kernel: true`?** *Adopted: (a), with 4 (a), in the same words.* Needs 1 (a). Today both do.
   - (a) The row on every host, Not Applicable in a container (today: no row there); Run All still needs the key, as every Run All step needs its own. With 4 (a) or (b), a host that never applied Kernel loses the points until it does.
   - (b) Both need it, as today: main menu 4 has no row until the key is set.
   - (c) Neither: Run All applies Kernel on every host, which changes hosts whose hardn.yml never asked, and the key is no longer read.
6. **"Configured applied".** *Replaced by 18. Asked only with 18 (c), no number.* Today the row reads "Configured by hardn".
   - (a) *Recommended:* "applied", his word: "[applied]" in brackets. It matches the screen's "Apply settings".
   - (b) "by hardn", as today: "[by hardn]". No change to the control or its goldens.
7. **Bold brackets.** *Asked only with 13 (b), brackets.* Drawn: `brackets`, `brackets-bold`.
   - (a) *Recommended:* not bold. Bold draws the eye to the rows that need nothing.
   - (b) Bold.
8. **Where the form applies: "+" or brackets.** Needs 13 (a) or (b).
   - (a) *Recommended:* every status row on a screen: the main menu's and each screen's own, so a row reads the same in both. A row that shows a value as its word stays: the SSH Port row's "2230/tcp", a Figma frame; no shipped row sets a word today. The Markdown report keeps "Configured" in its State column, and `hardn check` and the JSON keep their words: no colour or mark is there to carry it.
   - (b) The main menu only: SSH Login reads "+ root disallowed" (or "[root disallowed]") there and "Configured root disallowed" on its screen.
9. **The other state words.** *Restated as 15 for "+". Asked only with 13 (b), brackets.* Drawn: `mixed`.
   - (a) *Recommended:* kept: "Partial", "Not Configured", "Not Applicable" and "Unknown", then the detail, as today.
   - (b) Brackets in the state's colour for all. Partial and Not Configured share a mark, so with no colour they cannot be told apart: the rule of 2026-10-21 forbids it.
10. **Info rows: what they show, and when.** *Asked only with 17 (c), the three rows always. Falls away with 17 (a) or (b).* Drawn: `option-a`, `info-none`, `long-detail`. Today NTP has a row on every host but a container, and Syslog where hardn.yml names a server or the host forwards; DNS always.
    - (a) *Recommended:* the host's value, hardn.yml or not. A row with no value is left out, and with no row the group and its blank line. No state word; none counts in the risk level, as today. Today's rows with no value are NTP's "Not Configured no time service" and "<service> not running", Syslog's "Not Configured not forwarding" where hardn.yml names servers, and "Unknown can't read": decision 11 says which of them stay. On busybox ntpd, which names no server, NTP's value is "In sync" (today "Configured in sync").
    - (b) Every row always, "None" for no value: three lines on every host.
11. **A problem in an info row.** *Asked with 17 (a) or (c). Falls away with 17 (b).* Drawn: `info-problem` (DNS not hardn.yml's, NTP not in sync, Syslog not reached; the rest are not drawn). Today these are Partial, Not Configured or Unknown rows.
    - (a) *Recommended:* the value stays, the problem is a line under it in the warning colour: "Not hardn.yml's", "Not in sync", "Not reached". With no colour the words say it. With no value the problem is the value, in the warning colour, and the row is not left out: "Not forwarding" or "No time service" where hardn.yml names servers for it, "<service> not running", and "Can't read" for an Unknown row. Where hardn.yml names none, no forwarding and no time service are no problem, and the row is left out (10 (a), or 17 (a)).
    - (b) Today's row in the info group: mark, state word and detail, the value gone. The rows with no value and the Unknown ones stay as today.
12. **Info rows: mark, order, several servers.** *Asked with 17 (a) or (c). Falls away with 17 (b).* Drawn: `option-a`, `info-syslog`.
    - (a) *Recommended:* the bullet "-" as the mark; his order, DNS, NTP, Syslog; several syslog servers as a count, "3 servers", with "1 not reached" under it.
    - (b) Each server on its own line: the value is always shown, and the menu grows by a line a server.
    - The mark's cost: with Unicode glyphs Not Applicable's mark is "–", nearly the same. No mark at all is the other form.

Decisions 13 to 20 are the Plus alternative's (§3). Gates first.

13. **The form of a Configured row.** *Gates 7, 8, 9 and 15. Restates 2.* Drawn: `option-b`, `brackets`, `plus`.
    - (a) *Recommended:* "+", his latest text: `+ root disallowed`, the "+" green. The quietest of the three, and a detail gets 35 cells, as in brackets. With ASCII glyphs the row says it twice: `[+] SSH Login  + root disallowed`. A Configured row with no detail keeps the word. 7 and 9 fall away.
    - (b) Brackets, decision 2 (a): `[root disallowed]`. 15 falls away; 7 and 9 are asked.
    - (c) The word, as today. 7, 8, 9 and 15 fall away.
14. **SSH Auth: its own row, or in another.** *Gates 16, and item 2's words in 20.* Drawn: `plus`, `plus-user`. sshd's password setting is changed on the SSH Login screen, item 2.
    - (a) *Recommended:* in the SSH Login row: "+ root disallowed, key-only auth". The row and item 2, "Disable root and passwords", are then about the same things, and SSH Auth's lines ("sshd unpatched: upgrade openssh") sit under SSH Login. The User row stays as today. Not his text.
    - (b) In the User row, his text: "+ non-root, sudo, key-only auth". With passwords on, the User row is Not Configured and item 1 cannot fix it; item 2 does.
    - (c) Its own row, as today. 16 falls away; the menu is a line longer.
15. **Rows that are not Configured.** Needs 13 (a). *Restates 9.* Drawn: `plus-mixed`. His text shows none.
    - (a) *Recommended:* their words, as today: "Partial", "Not Configured", "Not Applicable", "Unknown". The rows that need work stay the loud ones.
    - (b) A sign each: "~" Partial, "-" Not Configured, "/" Not Applicable, "?" Unknown. Every detail gets 35 cells and no colour is needed to tell them. But "-" is the bullet too, and "[-]" is Not Applicable's ASCII mark.
16. **What merges, and what the one row says.** Needs 14 (a) or (b). Drawn: `plus-user`. Today SSH Auth weighs 20 of 100 and User 10 (`pkg/engine/risk/risk.go`). SSH Auth alone forces Critical (`SSH_PASSWORD_BYPASS`) and carries `SSH_SSHD_UNFIXED`, `SSH_SSHD_UNFIXED_LATENT` and `SSH_FIX_UNKNOWN`.
    - (a) *Recommended:* the row alone. The control `ssh.auth` stays, with its weight, its forced Critical, its factors, findings and codes; `hardn check`, the JSON and the report keep two entries; no host's score changes. The row shows the worse state of the two, and the detail of what is off alone: "Not Configured password auth on". Both details when both are Configured. SSH Auth's lines go under the row.
    - (b) The same, always both details. With the words kept, any pair with a part off is cut at 37 cells.
    - (c) One control of weight 30 (14 (b)) or 35 (14 (a)). A host with one part off earns 0 or half of the whole, where today it earns the other part's weight; the JSON loses `ssh.auth`. Scores change on existing hosts.
17. **Info rows.** **Adopted: (a)**, in the maintainer's words (2026-11-14): "we can show them when there are problems". *Gates 10, 11 and 12.* Drawn: `plus`, `plus-problem`, `plus-info`. Today DNS, NTP and Syslog are main-menu rows.
    - (a) *Recommended:* none while all is fine, as his text; a row only for what has a problem, in decision 11's form. A syslog server not reached is then still seen without opening Configuration. 10 falls away; 11 and 12 stand.
    - (b) None, ever. A problem is seen on its screen alone: main menu 8, then DNS, NTP or Syslog Settings. 10, 11 and 12 fall away.
    - (c) The three rows of A and B, always. 10, 11 and 12 stand.
18. **The Kernel row's number.** *Gates 6. Replaces it unless (c).* Drawn: `plus-kernel`.
    - (a) *Recommended:* what hardn's file sets, his words: "6 items applied". Counted by setting, which is new code (§3). The rows with keys to set keep "3 to set". A host at the baseline with no file stays a bare "Configured". The screen says "settings" where he says "items".
    - (b) How many are at the baseline: "18 of 18 at baseline", "Not Configured 12 of 18 at baseline" (35 cells). The same form on every host. The second number is 16 on a router or a Proxmox VE node.
    - (c) No number. 6 is then asked: "applied" or "by hardn".
19. **The line under a row.** Drawn: `plus`, `plus-mixed`, `plus-problem`. Today it is "- " at the value column. Only the main menu's rows have such lines; the host block's "- Reboot pending" is the same kind. "-" is also the bullet of every key-value row and plan line, which stay.
    - (a) *Recommended with 13 (a):* "⭘" ("[ ]" in ASCII), the warning glyph, two cells in from the value column. A "-" under "+ non-root" reads as a minus. The reboot line takes it too.
      - Under a row with a "+" or a sign, that is under the detail. Under a row that keeps its word (15 (a)), it is under the word's third cell, not the detail: `plus-mixed`, "Words kept". Every such line starts at the same cell.
      - Under an info row it is two cells in from the value.
      - "⭘" is also Not Configured's and Partial's row mark, in both glyph sets, as the rule of 2026-10-21 chose: the colour and the words tell them apart. Key-value warning lines have it today.
    - (b) "- ", as today.
20. **The items' words.** Drawn: `plus`, `plus-el`, `plus-pve`. Every description fits 37 cells; item 2's is the longest, 36 (`TestMenuLayoutPlusFits`).
    - (a) *Recommended:* his words, and where they do not hold:
      - Item 1 "User": the item alone. The screen's title stays "User Management", as Firewall's is "Firewall Configuration". It changes `pkg/screens/mainmenu/run.go`, the fixtures, `docs/usage.md`, `docs/design-system/README.md` and about 60 goldens.
      - Item 3 on Rocky Linux and AlmaLinux: "Manage firewalld, port listening" (32). On Proxmox VE: "View pve-firewall", as today.
      - Item 5 on Proxmox VE: "Not on Proxmox VE", the screen's own words. Elsewhere "Enable automatic updates", though with them on the screen's item is "Disable automatic updates".
      - Item 4 "Harden sysctl", for decision 3's "Configure sysctl".
    - (b) His words on every host. On Proxmox VE items 3 and 5 then promise what the screens refuse.
    - (c) Item 1 "User" and its screen's title, everywhere: the phrase "User Management" is in 106 files and 120 goldens, release notes and Figma's frame among them.
    - (d) Today's words.

### 6.3 Assumed (was §5)

- "Brackets bold" means the brackets, not the detail.
- The info rows' values are in the detail's colour, their mark in the muted one.
- The DNS item under Configuration is "DNS Settings  Configure nameservers", after its siblings. The screen's title is "DNS Configuration" today.
- Kernel moves out of Configuration when it becomes main menu 4.
- "Before the user ever opened the screen" is a host as installed: the row reads the host, so it says "6 to set" with no visit. The count is the mock's.
- The mock host's risk level is written as Low, for "No password to become root".
- The long details are made up: no Configured row's detail in the gallery's hosts reaches 35 cells.
- Plus: the "+" is in the state's colour and the detail as today.
- Plus: "⭘" is the warning glyph (`Glyphs.Warn`), in the line's colour, two cells in from the value column under every row, as it is under his "+" row.
- Plus: the header, bar and host are the fixture's (v0.4.0, BOS-SAN-L01), not his v0.10.0 and "util".
- Plus: no DNS, NTP or Syslog row means the rows are gone, not that his host had none.
- Plus: "6" is what hardn's file sets, as the `kernel` mock's host has "6 to set". He did not say.
- Plus: "port listening" is the Firewall screen's Ports section.
- Plus: the prompt and item 7 are unchanged; item 8 is decision 3 (a)'s.
- The signs "~", "-", "/" and "?", the merged details' ", ", and the Rocky Linux and Proxmox VE rows and counts are the mock's.

### 6.4 Not verified at the design step (was §6)

- The levels in decision 4: read from risk.go, not run with other weights.
- What a real host shows: nothing ran on Linux, and no row here was read from a host.
- The references in decision 3: by search for "main menu 4", "main menu's 4", "4) DNS", "main menu 8" and "main menu's 8", 2026-11-14. Other wordings were not looked for, and the docs/engine pages that say "main menu 8" without an item were not sorted.
- Run All's own lines and `hardn check`'s output were not read for state words.
- The brackets below 64 columns (`-width 60`): in the goldens, not looked at row by row. The same for "+" and the marked lines.
- Decision 16's scores: read from risk.go, not run.
- That "non-root, sudo, key and password" is the longest Configured pair: from the details in `pkg/engine/controls/users` and `ssh/auth.go`, by reading; doas and the Unknown details were not paired.
- Whether `hardn check` reports NTP and Syslog: not read. Decision 17 (b) says "its screen alone" of the menu only.
- Run All against "Queue hardening actions": the screen lists each control's outcome, then "Apply all"; its flow was not read further.
- The Firewall screen's Ports section on Rocky Linux: its field is there (`screen.Firewalld.Ports`); what fills it was not read.
- The counts in decision 20: by search for "User Management" and "1) User Management", 2026-11-14.

### 6.5 The maintainer's answers of 2026-11-14, after the Plus mocks (was §7)

Not drawn again; the mocks above keep the earlier words.

- **18, the Kernel row at the baseline with nothing applied.** It names that, and does not read "Configured" alone. His candidates: "baseline default", "system baseline", "system default", "baseline inherited". Open: which.
- **19, the line under a row.** "~" in place of the indented "⭘" ("perhaps we should use '~'"). With no Unicode the Risk Level's "≈" is also drawn "~"; and 15 (b) would give Partial rows a "~" too.
- **Syslog's problem words.** "Unreachable" and "server unreachable", not "Not reached" and "server not reached".

## On hosts

2026-11-15, a build of this branch on centos10-test-120 (CentOS Stream 10), oracle10-test-118 (Oracle Linux 10.1) and alpine324-test-116 (Alpine 3.24, a container). The menu and screens were opened and nothing was applied; the hosts were left as they started.

| Seen | Result |
|---|---|
| Configured rows | "+ non-root, sudo", "+ firewalld running"; other states keep their words |
| The SSH row | one row, "Partial root key-only", no SSH Auth row |
| Kernel | "Not Configured 4 to set" on both Red Hat hosts; "Not Applicable in a container" on Alpine |
| DNS, NTP, Syslog | no row on any of the three |
| The line under User | "~ No password to become root" |
| Items | his words; item 3 "Manage firewalld, port listening" on the Red Hat hosts |
| Main menu 4 | opens Kernel Settings |
| Main menu 8 | hardn.yml, DNS Settings, NTP Settings, Syslog Settings; 2 opens DNS |

Not seen on a host: a problem row, "N items applied", "baseline default", a Configured merged SSH row, Debian, Ubuntu, Proxmox VE, arm64.
