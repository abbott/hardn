# Rocky Linux and AlmaLinux: Auto Updates through dnf-automatic (`next/el-readonly`, unit 5)

> **Unit:** the fifth Rocky/AlmaLinux unit (the maintainer, 2026-11-10: "continue"). The fourth, the SELinux port label, went into 3a; dnf itself came with 3b ([el-firewalld.md](el-firewalld.md) §1.1).
> - Security updates only: "Security only" (2026-11-10).
>
> **Status:** built, tested against the recording fake, and run on rocky10-test-110 (§4).

## 1. What it does

### 1.1 The row

`dnf-automatic` (rpm -q); its install and plain timers (`systemctl is-active`, `is-enabled`) wherever rpm answered (not installed: `not-found` and `inactive`, exit 4, on rocky10-test-110); where installed, `/etc/dnf/automatic.conf`'s `[commands]` `upgrade_type`, `apply_updates` and `reboot`. dnf matches a key's name case-sensitively and ignores one it doesn't know (VM-read 2026-11-10), so one of these three in another case is `UPDATES_DNF_CONF`, as is a key twice; `upgrade_type` and `reboot` take their values exactly, `apply_updates` a boolean in any case. The words are proposed.

| Host | Row |
|---|---|
| Not installed | Not Configured, "no dnf-automatic" |
| No timer that installs updates: `dnf-automatic-install.timer` off, and `dnf-automatic.timer` off or with `apply_updates` false | Not Configured, "install timer off" |
| Such a timer active and enabled | Configured, "dnf-automatic, security" or "dnf-automatic, all updates" (`upgrade_type`) |
| Active, not enabled | Partial, "off after restart" |
| Enabled, not active | Partial, "starts after restart" |
| A read failed | Unknown, "can't read" |

`-download` and `-notifyonly` install nothing, so they don't count.

### 1.2 On

Refused as elsewhere: the record unreadable, or a fact unread. Also refused:
- `dnf-automatic-install.timer` masked, before the install too (`UPDATES_JOB_MASKED`);
- `automatic.conf` there before `dnf-automatic`: rpm would keep it, and Off's remove would move it aside (`UPDATES_DNF_CONF`);
- installed, `automatic.conf` missing or with no `[commands]` (`UPDATES_DNF_CONF`, read at plan and again at apply).

Where the row is Configured already and not by hardn, nothing is planned: the screen says it is on outside hardn.

The steps:
1. **Install** `dnf-automatic` where it is missing (`packages.Dnf`: 1 package, 37 KB on rocky10-test-110). Its preset leaves its timers disabled (`90-default.preset` names none of them).
2. **`upgrade_type = security`** in `/etc/dnf/automatic.conf`'s `[commands]`, where it isn't already.
   - The line is replaced, or added under `[commands]`, at apply: the file may come with step 1.
   - The file is backed up, written atomically, then read back.
   - The record keeps the line hardn wrote and the one it replaced (`Lines`), and the file's hashes before and after.
3. **The install timer:** `systemctl enable dnf-automatic-install.timer` where it is disabled, and `systemctl start` where it is inactive. Undone with `disable` and `stop`, each where the change made it.
4. **The record**, last, as for apt and Alpine.

`reboot` stays as `automatic.conf` sets it (never, as shipped): kernel updates then wait for a restart, which hardn's header says is pending. Where it restarts the host, the review says so (Restarts, a warning).

### 1.3 Off

Only what the record says hardn did, last first, each where the host is still as recorded:
1. The timer stopped and disabled, each where hardn started or enabled it.
2. The line put back:
   - only where the file's hash is the recorded after-hash, else `UPDATES_CHANGED_SINCE`;
   - the bytes written back must hash to the recorded before-hash, else refused the same way.
3. `dnf-automatic` removed, where hardn installed it: `dnf remove -y --noautoremove`, simulated first (`dnf remove --assumeno`), which must remove it alone. It goes last, so the file is the package's own again and no `.rpmsave` is left.
4. A read-back (the row no longer on), then the record deleted.

The line's hashes and lines come from one entry: the latest that edited the file.

Where a timer that was on before hardn still installs updates after Off (worked out at plan), Off goes ahead: the review says updates stay on (a warning), and the read-back checks hardn's own steps undone instead.

An Off rolled back after its remove leaves a stock `automatic.conf.rpmnew`: backups are restored before the reinstall, and rpm keeps the file (accepted).

With no record, Off has nothing to undo (`UPDATES_NOTHING_TO_UNDO`), as on Alpine.

### 1.4 Elsewhere

- **The screen:** rows dnf-automatic, Install Timer (the timer the row reads: `dnf-automatic.timer` where it installs updates), Updates ("Security only" or "All updates"), Restarts (where `reboot` isn't never, a warning), Turned on by. Item words: "Daily security updates, by dnf-automatic" and "Install dnf-automatic" (proposed).
- **Run All:** its Auto Updates row turns on dnf-automatic there when `enableUnattendedUpgrades` asks, as it turns on unattended-upgrades elsewhere. The key names a policy, not a tool. That is my call; say if you'd rather it left the row to the screen.
- **p4status:** allows `rpm -q dnf-automatic` and the two timers' `is-active` and `is-enabled`.

## 2. API

```go
// pkg/host/dnfauto
const Package, Conf, InstallTimer, PlainTimer, Security, Line
type Settings struct{ UpgradeType string; Apply bool; Reboot string }
type Updates struct{ Package detect.Tri; Install, Plain Timer; Settings; ... }
func Read(ctx, sys) Updates
func Parse(data) (Settings, error)
func SetSecurity(data) (out []byte, replaced string, err error)
func Revert(data []byte, replaced string) ([]byte, error)
var ErrConf error // UPDATES_DNF_CONF
// pkg/host/packages
func PlanRemove(ctx, sys, Dnf, name) (Removal, error) // the simulation must remove name alone
func ExecuteRemove(ctx, sys, Removal) error
```

## 3. Tested (fake)

- dnfauto: the stock `automatic.conf` (rocky10-test-110's rpm) parsed, set to security and reverted to its very bytes; a `[commands]` without the key; each form of the three keys and every refusal (case, twice, values); Read installed and not, a timer unread, and which timer installs updates.
- packages: the removal's simulation (alone, a dependent package, another package, the lock, nothing listed, rpm unread), and the remove checked (rpm unread after, a cancelled ctx, other commands).
- The control: the row on each line of §1.1; On from not installed and Off undoing each (no `.rpmsave` left); On with the package there, Off keeping it; on outside hardn plans nothing; Off with no record has nothing to undo; each refusal of §1.2 and of the install's simulation; the Restarts note; On rolled back where the start, the enable or the write doesn't take; Off where updates stay on; On twice, then Off; Off with the timer, the package or the file changed outside hardn; the row's findings.
- The screen: each host's rows and goldens, Restarts included (`rocky-reboot`).

## 4. Run on rocky10-test-110 (Rocky 10.2, 2026-11-10)

- `go vet` and `go test` on Linux: 74 packages pass. p4status: 29 commands, none refused.
- No preset names dnf-automatic: its timers stay disabled after the install.
- On from not installed, through the screen: installed, the install timer active and enabled, `upgrade_type = security`, the record's four steps. Off: the host as before, no `automatic.conf`, `.rpmsave` or `.rpmnew` left.
- Installed by hand, the install timer enabled, not started (Partial): On set the line and started the timer. Off said updates stay on, stopped the timer, put the line back (`rpm -V`: the file's bytes are the package's) and kept the package.
