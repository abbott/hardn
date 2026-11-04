# Auto Updates mock (slice 7, unit (a))

Unit (a) of slice 7, `next/s7-gallery`: a proposal for review. Nothing reaches hardn; the mock is in `internal/tools/uigallery` (`updates.go`, `updateshost.go`).
Status: built and tested on macOS in process (`go test ./internal/tools/uigallery/`); no hardn binary ran, nothing ran on a VM. The hosts' shapes are the VMs' read of 2026-11-03 (`vm-runs/s7/00-inspect-*.log`), their values synthetic.

## 1. What to run

```
go run ./internal/tools/uigallery updates [-ascii] [mock | all]                # the listing, no input
go run ./internal/tools/uigallery play updates                                   # the menu of the hosts
go run ./internal/tools/uigallery play updates/<host>                            # one host
go run ./internal/tools/uigallery play -sequence dry-run updates/<host>          # plan only
go run ./internal/tools/uigallery play                                           # main menu 6 leads to updates/debian13
```

The main menu in play now has item 6, "Auto Updates", between Backup and Run All, and its hint reads `[0-10 or q]`. That changes 14 existing play goldens, each only by that line and the hint, because each draws the main menu: `backup`, `backup-plan-only`, `dns`, `environment`, `environment-main-plan-only`, `logs`, `logs-plan-only`, `ssh-toggle`, `status/lockout`, `status/menu`, `system-details`, `system-details-plan-only`, `user-management`, `user-management-typed`. `TestPlaySize` draws the same item. Figma's main-menu fixture (`pkg/ui/screen/frames.go`) and its goldens are unchanged.

## 2. The mocks

The row is the main menu's own: `Engine.Check` over `controls.Status`, `risk.Assess` and `output.StatusViewOf`, with the Auto Updates row kept. The section comes from what the host has: `apt-config dump`, `dpkg-query` and `systemctl` on apt hosts, `alpine.ReadUpdates` on Alpine; never hardn.yml. Each change runs on the real engine as a plain y/N (no lockout risk), rolled back on failure. Alpine's on is the real `alpine.PlanUpdates` through `steps.Alpine`, and the package install is the real `packages.PlanInstall`. The rest are stand-ins (§6): the rest of each direction, the record, and which file sets each value. The hosts marked "by hardn" are built by running the mock's own Enable on the engine.

| Host | What it shows |
|---|---|
| `debian13` | The cloud VM: unattended-upgrades 2.12 on by the package, `20auto-upgrades` the only APT::Periodic file, both timers on. Not hardn's, so the item is read-only |
| `ubuntu2604` | The VM: `10periodic` and `20auto-upgrades`, Allowed-Origins. Read-only |
| `alpine323` | The VM: no apk-cron, crond stopped and in no runlevel. Enable plans the install, the runlevel and the start |
| `debian-installer` | No package and no APT::Periodic file. Enable installs the package (its postinst turns updates on by itself) and writes hardn's file |
| `debian-off` | `20auto-upgrades-disabled`'s two `"0"` lines. Enable writes hardn's file alone |
| `lists-stale` | Partial: the list refresh is `"0"`. Enable writes `Update-Package-Lists` alone |
| `timer-off` | `apt-daily-upgrade.timer` disabled. Enable refuses with words, because hardn leaves apt's timers alone |
| `by-hardn-debian` | `debian-installer` after Enable. Disable deletes hardn's file, then purges the package |
| `by-hardn-alpine` | `alpine323` after Enable. Disable stops crond, takes it out of the runlevel and removes apk-cron |
| `edited` | `by-hardn-debian` with hardn's file edited. Disable refuses and names the file |
| `crond-shared` | logrotate's daily job, then Enable. Disable removes apk-cron and keeps crond, saying why |
| `legacy-alpine` | origin/main's `apk-upgrade`, which crond never ran. Enable also removes it (§3.20) |
| `pve9` | No package. The row is today's check's; the item is unavailable (decision 7) |
| `not-read` | A syntax error in `apt.conf.d`, so `apt-config` fails: the row is Unknown and both directions refuse |

The listing adds `refusals`, nine of them. Three come from variants the menu does not list: `edited-alpine` (apk's script changed), `enable-off` (`Enable "0"` in a file of the administrator's) and `own-file` (hardn's file with no record, which counts as on by hardn). 16 transcripts play the flows (`play/updates-*`). Here is `by-hardn-debian`, drawn:

```
# Auto Updates #################################################

  [+] Auto Updates         Configured unattended-upgrades

# Update Configuration -----------------------------------------

   -  unattended-upgrades  Installed 2.12
   -  Upgrades             Daily by 52hardn-auto-upgrades
   -  Package Lists        Daily by 52hardn-auto-upgrades
   -  Upgrade Timer        On apt-daily-upgrade.timer
   -  Lists Timer          On apt-daily.timer
   -  Turned on by         hardn record of 2026-10-20

  Select an option

   1) Disable automatic updates
      Undo hardn's changes

   0) Return

  > Enter your choice [0-1 or q]:
```

How it departs from the other screens: there is no Figma frame, so it is built on the sub-menu layout from existing components. It has one item, and that item's label changes with the host's state (P-12). Where updates are on but not by hardn, the item is shown with P-22's `Unavailable` and the screen has nothing to choose but Return.

## 3. Heights

Every screen has as many lines at 80x24 as at 80x40, in Unicode and ASCII, and no line is over 80 cells. Every section is drawn in full at every height (gallery-firewall.md decision 9).

| Lines | Hosts |
|---|---|
| 17, 18 | alpine323, legacy-alpine, not-read |
| 19 to 21 | by-hardn-alpine, debian-installer, debian-off, timer-off, crond-shared, lists-stale, by-hardn-debian, edited |
| 22 | debian13, ubuntu2604, pve9 |

## 4. Decisions for the maintainer

Drawn: every recommendation below except 2, 8 and 9, which change no screen. Not mocked: apt's cron job (`apt-compat`) off systemd, a host using cronie or dcron, rolling back an off, a record with two entries, the audit log.

1. **Scope and the item.** *Gates 2 to 10.* hardn's main menu has an Auto Updates status row but no item or screen for it. (a) *Recommended, drawn:* an "Auto Updates" screen with one item named by its result: "Enable automatic updates" while off or partial; "Disable automatic updates" while on by hardn; the same label shown as unavailable ("Turned on outside hardn") while on but not by hardn, as on both apt VMs. The main menu item is key 6, "Auto Updates", "Configure automatic updates". Slice 2b removed Dry-Run (6) and left its key unused "so no key changes meaning", because renumbering would have put Run All on the key that once turned dry run on. Reusing 6 does give it a new meaning, but no harm follows: the screen asks y/N before it changes anything, and a gap at 6 would last forever. (b) Key 11, after Logs: keeps 2b's rule, and the menu keeps its gap. (c) Renumber 7 to 10: rejected by 2b.
2. **How off is asked (P-43 decision 1).** *Gates 3 and 8.* hardn.yml's `enableUnattendedUpgrades` defaults to false, and Run All reads false as "skip", so false cannot mean "turn off". (a) *Recommended:* off is a screen action only. Run All never turns anything off. An off saves the key as false through §3.19's one save function (`steps.PlanSave`, `config.Edit`), so the next Run All does not turn updates back on. (b) A key with string values (`on`/`off`/`skip`), as §3.17 did for `permitRootLogin`: a change to the config's surface.
3. **What on writes on Debian and Ubuntu.** *Gated by 2.* (a) *Recommended, drawn:* install unattended-upgrades when it is missing, then write hardn's own `/etc/apt/apt.conf.d/52hardn-auto-upgrades`. The file holds only the lines whose effective value is `"0"` or unset, set to `"1"`. It is read back with `apt-config dump APT::Periodic`, and a mismatch (a file read later wins) fails the change, which is rolled back. hardn never touches debconf, `20auto-upgrades` or the timers. `Enable "0"`, a timer that is off and a masked job each refuse, naming the fix. Where updates are on but not by hardn, the item is unavailable. (b) Turn the timers on too: that writes the administrator's units, which off could not tell apart from theirs.
4. **The record.** *Gates 5 and 6.* Without a record, off cannot tell hardn's changes from anyone else's. (a) *Recommended, drawn:* `/var/lib/hardn/controls/updates.auto.json` (0600, in a 0700 directory), holding `schemaVersion`, then one entry per apply: its steps in order, the packages the simulation listed, each file's SHA-256 (hardn's, and the package's files after the install), the lines written, and the state read back. The control's last change writes it, backed up and journalled, so a rollback puts the old record back or removes it. With no record, "by hardn" means hardn's own file holding bytes hardn writes. (b) No record, only hardn's own file: then off cannot undo the install, and decisions 5 and 6 go.
5. **The package hardn installed, at off (P-43 decision 3).** *Gated by 4.* Installing the package turns updates on through its postinst (debconf's default is true), so deleting hardn's file is not enough. (a) *Recommended, drawn:* purge while `20auto-upgrades` and `50unattended-upgrades` hold the recorded bytes, because purge removes both even when edited (the VM's postrm). If either changed, remove the package instead and keep the file, saying so in the review. (b) Remove: the upgrades stop, but `20auto-upgrades` stays, and so does the daily list refresh. (c) Keep: `20auto-upgrades` keeps updates on, so off does not turn them off.
6. **crond on Alpine, at off (P-43 decision 4).** *Gated by 4.* (a) *Recommended, drawn:* stop crond and take it out of the runlevel only where hardn put it there and nothing else needs it: no other executable under `/etc/periodic/*` and only root's stock crontab. Otherwise keep it, and the review says why (`crond-shared`). (b) Never touch crond at off: crond then keeps running, even if hardn alone started it.
7. **Proxmox VE (P-43 decision 5, the Auto Updates half).** (a) *Recommended, drawn:* not offered. Debian's origins never match Proxmox's packages, and Proxmox's guide says to update with `apt full-upgrade`. The item is unavailable with "Not on Proxmox VE", and the section says "Updates by hand". The check's `Applies` must then return false, so the row reads Not Applicable and leaves the risk level (unit (b); today the row is "Not Configured"). (b) On, with a warning. (c) On, with Proxmox's origin added.
8. **hardn.yml's `enableUnattendedUpgrades` and a CLI form.** (a) *Recommended:* the key stays Run All's until slice 8, and no flag is added. The screen reads the host, never the key, except for decision 2's save. (b) A flag (`--auto-updates on|off`): one more surface that needs decision 2's rule.
9. **The row's detail when hardn turned updates on, and a stale record (P-43 (7)).** (a) *Recommended:* the detail stays the tool's ("unattended-upgrades", "apk-cron"), and the section's "Turned on by hardn" carries the source. A record the host no longer matches (a file changed or the package gone) is a finding on the row, "changed since hardn". (b) "On, by hardn" as the detail: this hides the tool and changes check-updates.md §5's agreed lines.
10. **The words.** (a) *Recommended:* §5's words, drawn. P-43 (5)'s are kept where they fit: the two item labels, "Install unattended-upgrades", "Turn on daily upgrades", "Turn off daily upgrades", "Remove apk-cron", "Stop crond", "Nothing to undo: hardn did not turn this on.", and "Not undone: /etc/periodic/daily/apk changed since hardn installed it.". (b) Other words, named.

**Made here:**
- The lines are chosen at plan time, before the install, so on `debian-installer` hardn's file repeats what the postinst then writes. The record holds both.
- The install change backs up the postinst's files (absent before), so a rolled-back install also removes `20auto-upgrades`, which `apt-get remove` keeps.
- Off's steps run in the files stage, chained last first. The record is in the verify stage.
- Partial (lists not refreshed) offers Enable.
- The Alpine on is one change, as `steps.Alpine` makes it.
- Refusals are found before asking, from the stand-in planner (`preplan`).

## 5. Words

All proposed:
- Title "Auto Updates"; section "Update Configuration".
- Rows: "unattended-upgrades" (Installed, Not installed); "Upgrades" and "Package Lists" (Daily, Off, "Every 7 days", "Each run"; notes "by <file>", "unset"); "Periodic Jobs" (Off); "Upgrade Timer" and "Lists Timer" (On, Off, "Off after restart", Masked; the unit as the note); "apk-cron"; "Daily Script" (In place, Not run); "crond" (Running, Stopped; notes "starts at boot", "not at boot"); "Old Script" (origin/main's); "Other Jobs" (note "use crond"); "Proxmox VE" (Updates by hand, note "apt full-upgrade"); "Turned on by" (hardn with "record of <date>" or the file; Not hardn).
- Items: "Enable automatic updates" ("Install unattended-upgrades", "Daily, by unattended-upgrades", "Daily, by apk-cron"); "Disable automatic updates" ("Undo hardn's changes", "Turned on outside hardn"); "Not on Proxmox VE"; "Can't read".
- Questions: "Turn automatic updates on?", "Turn automatic updates off?".
- Steps: "Install unattended-upgrades", "Turn on daily upgrades", "Turn off daily upgrades", "Remove unattended-upgrades" (purged, with 20auto-upgrades; removed, its files kept), "Stop crond", "Stop crond at boot", "Remove apk-cron", "Record hardn's changes", "Delete hardn's record"; review lines "Kept, changed since", "crond Kept, <job> needs it", "Old Script Stays removed".
- Refusals:
  - "<timer> is off, and hardn leaves apt's timers to you. Run systemctl enable --now <timer>, then try again."
  - "<service> is masked, …"
  - "APT::Periodic::Enable "0", in <file>, turns apt's daily jobs off. Remove that line, then try again."
  - "50unattended-upgrades allows no origin, …"
  - "apt can't read its settings: <apt's error>. Fix that, then try again."
  - "Not undone: <file> changed since hardn wrote it (installed it). Put it back as it was or undo the rest by hand."
  - "hardn can't read its record of what it changed (<path>). …"
  - "Nothing to undo: hardn did not turn this on. Turn it off by hand."
  - "Proxmox VE is updated with apt full-upgrade, …"
  - "Installing unattended-upgrades would also change other packages: …"
  - "apt reads <key> as "<v>" after the change, …"

## 6. For unit (b)

Stand-ins, each labelled in `updates.go`:

1. `updControl2`, `updOn`, `updOff`: `updates.Auto` with a `Plan` in both directions, given by an engine `Input` field (decision 2); unit (b) moves it off `engine.CheckOnly`.
2. `planAptOn`, `readBack`: Debian's and Ubuntu's on (decision 3), with codes registered in `pkg/output`; its read-back with `APT_CONFIG` unset.
3. `planOff`, `planAptOff`, `planApkOff`, `otherJobs`: the inverses (decisions 5, 6), the purge's simulation, and the drift check by hash.
4. `updRecord`, `recordOn`, `readRecord`: the record through revert's helpers (`WriteFileAtomic`, `ReadFileNoFollow`, trust checks), decoded strictly.
5. `aptSetBy`: which file sets each value. `apt-config` never says, so unit (b) parses apt.conf itself (blocks, `#clear`, `#include`), which the model's one-line files do not exercise.
6. The fake (`updSim`): apt's files in the dump's own form, the postinst and ucf, purge's `rm -f`, apk's kept edited file, crond and its runlevel.

Findings:
- `packages.PlanInstall`'s undo is `apt-get remove`, which keeps `20auto-upgrades`, so a rolled-back install would leave the list refresh on. The mock backs those files up in the install change; unit (b) must do the same.
- Rolling back a purge reinstalls the package, whose postinst writes `20auto-upgrades` from a debconf answer the purge dropped (the default, true). It is not mocked.
- Off reads nothing back where hardn's install wrote `20auto-upgrades`: there is no "before" to compare with until after the purge.
- The check reads Proxmox VE as Debian, so decision 7 (a) needs `Applies` changed.
- Off's package removal sits outside `StagePackages`, so that it runs last first.

## 7. Not run

No hardn binary ran, and nothing ran on a VM. These are untested:
- the postinst and ucf on a real host
- `apt-get purge` of an edited `50unattended-upgrades`
- `apt-config dump` after hardn's file
- `apk del` keeping an edited script
- crond's runlevel undo
- unattended-upgrade actually running from hardn's file
