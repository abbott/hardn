# The Auto Updates control, both ways (slice 7, unit (b))

`pkg/engine/controls/updates`, `next/s7-control`: the check of [check-updates.md](check-updates.md) gains a `Plan` that turns updates on or off, the record, and the reading the Auto Updates screen draws. It builds what [gallery-updates.md](gallery-updates.md) §4 adopted (2026-11-03, "Adopt all, start (b)") and §5's words, on the mock's behaviour (`internal/tools/uigallery/updates.go`, `updateshost.go`). Status: tested on macOS on `hostfake` only; nothing ran on a VM.

## 1. What it does

- **On, Debian and Ubuntu** (decision 3): unattended-upgrades installed when missing (`packages.PlanInstall`; the change backs up `20auto-upgrades` and `50unattended-upgrades`, so a rolled-back install also removes them), then hardn's own `/etc/apt/apt.conf.d/52hardn-auto-upgrades` holding only the lines whose value is `"0"` or unset, set to `"1"`, read back with `apt-config dump`. Which file sets each value is `aptconf.Read`'s. It refuses `Enable "0"`, a timer off, a masked or missing job, no origin, and an install that changes other packages. Debconf, `20auto-upgrades` and the timers are never touched.
- **On, Alpine**: `alpine.PlanUpdates` through `steps.Alpine`, one change. The record holds crond's state before and the other jobs (`alpine.OtherJobs`), by path.
- **The record** (decision 4): `/var/lib/hardn/controls/updates.auto.json`, written by the last change (`StageVerify`) through `revert.EncodeRecord`, `PrepareRecordDir` and `WriteFileAtomic` (0600). Its path is in that change's `Backups`, so a rollback puts the old record back or removes it.
- **Off** (decisions 2, 5, 6, R2): the inverse of each recorded step, last first, in `StageFiles`. Hardn's file is deleted. The package is purged only where nothing purge deletes holds what hardn would lose: `20auto-upgrades` and `50unattended-upgrades` were both absent before hardn's install and still hold the recorded bytes; every dpkg conffile of the package (`dpkg-query -W -f='${Conffiles}\n'`, " <path> <md5>" a line; one ending `obsolete` or `newconffile` never matches) still holds the MD5 dpkg lists; and no `20auto-upgrades.ucf-*` or `50unattended-upgrades.ucf-*` side file exists. Otherwise it is removed and its files kept, said ("<file> Kept, changed since installed"). Every file the purge would delete is in the change's `Backups`. Dependencies are kept and said. crond is stopped and taken out of the runlevel only where hardn started it (`StepStart`; after a `StepRestart` crond was running before hardn, so it stays, "crond Kept, running before hardn") and added it, and no job has appeared since. With no record, only hardn's own file is deleted, while it holds hardn's bytes and no other file keeps updates on (`APT::Periodic::Enable` read as on's rule: only a decimal of zeros turns the jobs off, so `""` does not). On apt and on Alpine, the last step reads the setting back, failing, rolled back, where the check still reads updates on. Then `enableUnattendedUpgrades: false` is saved through `steps.PlanSave` where the loaded hardn.yml says true, and the record is deleted.
- **Checked again at apply.** The engine plans again under the run lock before applying and refuses a plan that differs (`ErrPlanChanged`, `pkg/engine/apply.go:214-274`), so a host changed between the review and the apply is caught there; that re-plan runs the simulations (`apt-get … -s`, `apk del --simulate`) again too. Each off change also checks, just before its own step, what its plan read, for the window inside the apply: the purge the same set as its rule (both files' hashes, the conffiles' MD5s, no ucf side file); `ChangeFile` hardn's file's hash; `apk del` apk-cron's script's hash, each failing with the changed-since refusal before anything is removed; crond's stop and runlevel removal `alpine.OtherJobs` again, failing where a job appeared since ("crond: <job> added since").
- **The check** (decisions 7, 9): `Applies` is false on Proxmox VE, "updated by hand"; a stale record adds the finding `UPDATES_CHANGED_SINCE_HARDN`, "changed since hardn".

## 2. API

```go
type AutoAction uint8 // AutoNone (Plan: engine.ErrNotMigrated), AutoOn, AutoOff
type Auto struct {
	Action  AutoAction
	Records revert.Options // the record's Dir and Now; zero is /var/lib/hardn, time.Now
	Version string         // hardn's, in the record
}
func (c Auto) Plan(ctx, sys, in engine.Input) ([]engine.Change, error)
func (c Auto) Read(ctx, sys, f detect.Facts, cfg *config.Config) (Reading, error) // error: ctx's only
func (c Auto) Notes(ctx, sys, in engine.Input) ([]Note, error) // the review's lines for Action
func (c Auto) RecordPath() (string, error)
type Note struct{ Key, Value, Note string; Warn bool }
type Timer struct{ Timer, Service, Active, Enabled, ServiceEnabled string; Err error }
type Reading struct {
	Facts; Config; Status engine.Status; StatusErr; PVE, Apk bool
	Apt aptconf.Config; AptErr; Package detect.Tri; PackageErr; Upgrade, Lists Timer
	Origins, Own bool; Present []string; StayOn string
	Updates alpine.Updates; UpdatesErr; Jobs []alpine.Job; JobsErr
	Record *revert.ControlRecord; RecordErr; Changed string; PackageGone bool
}
func (r Reading) On() bool; ByHardn() bool; Unread() bool
func (r Reading) Blocked(off bool) string // the item's R1 description, "" for none
func IsHardnFile(data []byte) bool
const HardnFile, AutoUpgrades, UnattendedConfig, Package, Setting
const ChangeInstall, ChangeFile, ChangeApk, ChangeRecord, ChangePackage, ChangeCrondStop, ChangeCrondBoot, ChangeSave
const CodeChangedSince = "UPDATES_CHANGED_SINCE_HARDN"; DetailByHand, DetailChangedSince
var ErrNotRead, ErrPeriodicOff, ErrTimerOff, ErrJobMasked, ErrJobMissing, ErrNoOrigins, ErrProxmox,
	ErrOtherPackages, ErrNotInRepos, ErrNotInEffect, ErrRecordUnreadable, ErrChangedSince, ErrNothingToUndo, ErrStaysOn
```

**For unit (c), the screen:** draw from `Read` alone: the row is `Reading.Status`; the main menu's row lines are the risk level's factors alone (`output.StatusViewOf`), so decision 9's "changed since hardn" line is the Auto Updates screen's to draw, from the control's finding (`CodeChangedSince` in `Reading.Status.Findings`); the rows read `Apt.Value`, `Package`, the `Timer`s, `Updates`, `Jobs`, `Record`. The item is Enable or Disable by `ByHardn`, `On` and `Unread`, as the mock's `item` does; its description is `Blocked`. `Notes` are the review's rows past the change. Build the control as `Auto{Action: AutoOn | AutoOff, Version: …}`. **For unit (d), Run All:** use `AutoOn` only, never `AutoOff` (decision 2).

## 3. Codes

Errors: `UPDATES_NOT_READ`, `UPDATES_PERIODIC_OFF`, `UPDATES_TIMER_OFF`, `UPDATES_JOB_MASKED`, `UPDATES_JOB_MISSING`, `UPDATES_NO_ORIGINS`, `UPDATES_PROXMOX`, `UPDATES_OTHER_PACKAGES`, `UPDATES_NOT_IN_REPOS`, `UPDATES_NOT_IN_EFFECT`, `UPDATES_RECORD_UNREADABLE`, `UPDATES_CHANGED_SINCE`, `UPDATES_NOTHING_TO_UNDO`, `UPDATES_STAY_ON`. Each is a `*steps.Refusal` wrapping its sentinel, in the mock's Why and Fix, and is registered before the readers' errors (`pkg/output/codes.go`, `codes.golden`, `sentinels.golden`, [output.md](../reference/output.md)). Finding: `UPDATES_CHANGED_SINCE_HARDN`.

## 4. Made here

- The direction is on the control (`Auto.Action`), as `sshchange.Auth{Action}` has it, not a new `engine.Input` field (decided before this unit). The zero value plans nothing, so the status list stays check-only.
- With the record unreadable, **on** refuses at plan (`UPDATES_RECORD_UNREADABLE`). The mock planned and then failed at apply.
- If aptconf refuses the files (an entry it does not read, or a disagreement with `apt-config dump`; a name apt doesn't take is skipped, [aptconf.md](../safety/aptconf.md)), the result is `UPDATES_NOT_READ`, "hardn can't read how updates run here: <error>". These are the mock's Alpine words; the mock had no such case.
- If `alpine.OtherJobs` fails, on and off refuse `UPDATES_NOT_READ`. The mock read that as no jobs.
- crond's state before is `ServiceState{crond, <state>, yes|no}` and the jobs are paths, as `revert.ControlRecord` holds them. The words use the job's name.
- Decision 9's finding covers a package hardn installed that is now gone, as well as a changed file. The decision names both; the mock had only the file.
- The install takes its tool from `packages.ToolFor`, so a Debian derivative gets `ErrNoTool`. The mock used apt.
- Origins are read as the check reads them, named children included.
- The record's time is UTC.

## 5. Departures and consequences

- **Proxmox VE's refusals cannot be reached through the engine.** With `Applies` false, the engine never calls `Plan`, so "Proxmox VE is updated with apt full-upgrade, …" appears only when `Plan` is called directly. The gallery's refusals golden now shows "Nothing needs changing." for on, pve9. The item is unavailable there anyway (P-22).
- On Proxmox VE the row leaves the risk level, so `UPTIME_NO_AUTO_UPDATES` is not raised there. The gallery's pve9 score goes from 5.0 to 18.8 (still Critical). The status goldens, `p4status`'s evidence test and `pkg/menu`'s pass count (23 to 15) are updated, and [risk.md](risk.md) §5.5 and [gallery-status.md](gallery-status.md) §2 and §5 now say so.
- Off on Alpine reads back after its last step, as apt's does (gallery-updates.md, "Every off reads the setting back"); the mock had none. Its words are "updates still run after the change (<detail>)", since §5's name apt. The gallery's three Alpine off transcripts (`play/updates-disable-alpine`, `updates-crond-shared`, `updates-dormant-jobs`) gained the read-back's `rc-service crond status`.
- The gallery's model ships no dpkg conffile (its `dpkg-query -W` answers none), so its purge rule is the two files' and the ucf side files' alone; the control's tests model `/etc/logrotate.d/unattended-upgrades`.

## 6. Not run

Nothing ran on a VM. These are untested: the install and the postinst through ucf; `apt-config dump` after hardn's file; the purge of an edited `50unattended-upgrades`; `dpkg-query`'s `${Conffiles}` for an obsolete or `newconffile` line; `apk del` keeping an edited script; crond's runlevel undo; the record's trust checks on a real `/var/lib/hardn`; and `aptconf.Read` over the VMs' block-form files. The tests' apt files are one-line forms and one-line blocks.
