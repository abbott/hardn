# The output contract (`pkg/output`, `host.System.AppendFile`)

> **Unit:** refactor plan Phase 3, `next/p3-output` (plan §2, row `p3-output`; brief §2, the decision "A fixed exit-code set and versioned JSON output"; brief Phase 3: non-interactive use, the exit codes, the JSON contract, the audit log and its [Verify]; decision §3.17, approved 2026-10-17: the deprecation warning for a bool `permitRootLogin`; §3.11 is **not** approved, so no flag is added). It takes up the "`p3-output` and `p3-startup`" items of [docs/safety/README.md](../safety/README.md) that are this unit's, and the `p3-output` inputs of [engine.md](engine.md) §7, [steps.md](steps.md) §7, [engine-boot.md](engine-boot.md) §7, [revert.md](../safety/revert.md) §7 and §8.5, [revert-boot.md](../safety/revert-boot.md) §6 and every Phase 2 page's §6. V-numbers: none; the one behaviour change is the escape codes the VM runs found in the journal ([vmtest.md](../safety/vmtest.md) §4.9), and the warning §3.17 approved.
> **Status:** built and tested on macOS: the documents and human reports as goldens, the exit statuses, the registry against a scan of the sources, the flow adapter over the real engine on the recording fake (`hostfake`), the audit log on the fake, `AppendFile` on the fake and on real files in `t.TempDir()`, `cmd/hardn`'s two wirings through its seams with no binary run. Nothing has run on Linux: everything host-specific is "written, not run (gate 5)" (§4).

The public contract, what a script may rely on, is [docs/reference/output.md](../reference/output.md). This page says what the package guarantees, on what, and how it is tested.

## 1. Why

The engine returns `Plan`, `Report` and `Result` as data, and the Phase 2 primitives return sentinels and typed errors, each page listing the codes it hands on. Nothing turned those into what the brief decided: one versioned JSON document on stdout, stable codes a script can match, five exit statuses, an audit record per run. `Plan`, `Report` and `Result` are not a wire format: their errors marshal as `{}` and their words follow the constants (engine.md §7). And two things were owed to the command line already: §3.17's deprecation warning, which nothing printed ([sshd-migrate.md](../safety/sshd-migrate.md) §2.20), and `hardn confirm` and `hardn rollback`'s colour codes in the journal (vmtest.md §4.9).

## 2. Guarantees

### The document (`Document`, `Build`)

1. **Wire types of their own** for the nine top-level fields (`schemaVersion`, `hardnVersion`, `host`, `mode`, `result`, `changes`, `status`, `findings`, `errors`), shared with the audit log's `Record`. `Build(Run) Document` is pure: it maps a `Run` (the mode, `Engine.Check`'s reports, `Engine.Plan`'s plan, `Engine.Apply`'s result, the run's error, the configuration's deprecations) into them. Every word comes from a table of this package (`stateWords`, `riskWords`, `stageWords`, `stepWords`, and detect's `Tri`, `InitKind`, `SSHMode`, `Support`, `Distro` and `SudoFlavor`), never from a `String` method, so a change of an engine or detect constant is not a change of the schema; detect's facts are mapped to the schema's `Host`, not written as they are ([detect.md](../safety/detect.md) §6). Arrays are never `null`. Tested: `TestDocuments`, a golden per document the plan row names (a check, a plan, a refused plan, an applied lockout-risk change waiting for confirmation with its deadline approximate, a failed apply rolled back: `testdata/json/*.json`), and `TestHostAndConfirm` (Alpine's words, no facts at all).
2. **`schemaVersion` 1** and the documented rule for bumping it ([reference](../reference/output.md) §1.1): additions and new values of open fields keep it; anything that would break a script reading version 1 bumps it. The goldens pin the documents, the codes and each sentinel's code, so such a change cannot land unseen.
3. **Each change's status** comes from the result, as engine.md §7 asks: `planned` for a check or a plan; for an apply, `applied` for a change in `Result.Applied` (and for the boot unit, from `Result.Boot`, since it is never in `Applied`: engine-boot.md §2.1), `failed` for `Failed.Change`, `rolled-back` for one applied before an `ApplyError` that `Is(ErrRolledBack)`, `unjournalled` for `Failed.Unjournalled.Change`, `not-run` for the rest.
4. **Each error once**, with its code ([reference](../reference/output.md) §1.8): a failed check's; a plan's refusals one by one (from `Plan.Refused`, or split out of `Plan.Err()`'s join when only the error is given, as `Apply` returns it); an `ApplyError` as its step's error, its rollback's error, and a change the journal did not cover (`CHANGE_UNJOURNALLED`); the boot unit's failure; the run's own error unless it is one of those. A refusal with a report (`steps.Refusal`) carries its `Why` and `Fix`; an error about a change carries the change's control. Errors are compared by identity only for comparable types, so no error value can make `Build` panic.

### The code registry (`codes.go`, one file)

5. **One ordered list**, the first entry that matches an error by `errors.Is` or `errors.As` giving its code (`CodeOf`), with steps.md §7's precedence: the more specific first ([reference](../reference/output.md) §2 states the order). An `*engine.ApplyError` takes its step's code; anything unmatched is `UNCLASSIFIED_ERROR`, a code like the others. Each entry carries its kind (`error`, `finding`, `boot`), the exit status an error of it gives a run, and a line saying what it means, which the reference page lists.
6. **What it covers:**
   - **Every exported `Err*` sentinel of `pkg/host/...` and `pkg/engine/...`** (143; `hostfake.ErrUnscripted`, a test double's, is exempt, with its reason): the Phase 2 pages' codes (`SSHD_CONFIG_INVALID`, `SSHD_ENV_NO_RUNDIR`, `UFW_VERIFY_FAILED`, …), `plans.ErrPlan`, `reload.ErrRestoredListeners` and `ErrNotInEffect` ([revert-plans.md](../safety/revert-plans.md) §6), the engine's sentinels with `ErrBootUnit` and the steps' with `SSH_LISTEN_CONF_NOT_IN_EFFECT`, the locks' (`revert.ErrNoLock`), and `pkg/config`'s two ([sshd-migrate.md](../safety/sshd-migrate.md) §6). One code for the three "directory another user can change" sentinels, as [sudoers.md](../safety/sudoers.md) §6 asks (`UNSAFE_DIRECTORY`).
   - **The typed errors told apart by a field:** `*sshd.EnvError` by `Kind` (`SSHD_ENV_NO_RUNDIR`, `SSHD_ENV_NO_HOSTKEYS`, `SSHD_ENV_NO_PRIVSEP_USER`; [sshd-validate.md](../safety/sshd-validate.md) §6); `*revert.LockedError` by `Run` (`RUN_LOCK_HELD`, `JOURNAL_LOCK_HELD`; engine-boot.md §7); `*revert.NoRollbackError` by `NoRollbackKind`, seven codes, so that none is read as its sentinel's "nothing to confirm" (revert.md §8.5); `*revert.NotPendingError` (the change rolled back, or its rollback given up); `*sudoers.CheckError` by stage (`SUDOERS_INVALID` for the candidate, `_BEFORE`, `_AFTER`, `_RESTORED`, and `SUDOERS_CHECK_FAILED` for a check that could not run); `*ufw.RollbackError`; `*host.ExitError` (`COMMAND_FAILED`). The other exported error types match through the sentinel they `Is` (`HoldError`, `BlockError`, `NarrowError`, `ConfigError`, `PendingError`, …).
   - **Every finding** the primitives' kinds give, under the prefix `pkg/engine/steps` derives (steps.md §2.23), for the six packages with finding kinds, `SSH_MIGRATE_LINE_DROPPED`, and `CONFIG_DEPRECATED_VALUE` for a configuration value read in a deprecated form.
   - **`hardn rollback --boot`'s** four `BootAction`s and 21 `BootReason`s (revert-boot.md §6), by their stable values (`BootCodes`).
   - This package's own (`USAGE`, `CONFIRMATION_REQUIRED`, `AUDIT_DIR_UNSAFE`), the interruption (`CANCELLED`: `context.Canceled`, `prompt.ErrAborted`) and three of the standard library's.
7. **Tested:** `TestSentinelsRegistered` parses every non-test Go file under `pkg/host` and `pkg/engine` (`go/parser`) and fails for an exported `Err*` the test's table does not name, for a name the scan does not find, for a table value whose text is not its `errors.New` literal (a value paired with the wrong name), and for a sentinel no entry matches, alone or wrapped; `TestTypedErrors` does the same for exported error types (a name ending in `Error`, and `steps.Refusal`), each with a sample the registry must code; `TestSentinelCodes` and `TestCodes` pin each sentinel's code and the whole list with kinds and exit statuses (`testdata/sentinels.golden`, `codes.golden`), and `TestCodes` checks that codes are unique SCREAMING_SNAKE; `TestPrecedence` (38 rows: each wrapping case above), `TestAbandonCodes`, `TestFindingCodes` (each kind enumerated through its `String`, derived as steps does, and no registered finding left over), `TestBootCodes` (read from `boot.go`'s constants), `TestReferenceListsEveryCode` (the reference page names every code).

### The exit statuses (`ExitCode`)

8. **One function over a run's results and errors**, `ExitCode(Run) int`, the same that writes `result.exitCode`: 130 when anything was interrupted; else 2 for a usage error (a code whose `Exit` is 2); else 1 for any other error (an apply that failed, a refusal, an error before anything was armed, a failed check); else 3 when a check found a control not as asked, a plan has changes, or the user declined them; else 0. `ExitOK` … `ExitInterrupted` are constants, equal to `pkg/cmd`'s `ExitUsage` and `pkg/ui/prompt`'s `ExitInterrupted` (`TestExitStatuses`). **Decisions of this unit**, none the maintainer's:
   - a check's `unknown` state is 3, not 0: a state not established is not taken as hardened (brief §3: prefer Unknown to guessing; Phase 4 counts Unknown as not configured);
   - a refusal is 1 (the run did not do what was asked, and changed nothing), which also registers `hardn rollback --boot`'s 1 for `BootLeft`, as revert-boot.md §6 asked: no code of its own;
   - a confirmation the user declines is 3: the plan found changes, and none were made;
   - an interrupted apply is 130 even though it was rolled back: the shell's convention says what happened, and `result.rollback` what was undone;
   - a lockout-risk change waiting for confirmation is 0.

   Tested: `TestExitCode`, 29 rows, each also checking the outcome word and `result.exitCode`; and each fixture's.

### The renderers (`WriteJSON`, `Emit`, `Human`, `FlowEngine`)

9. **Exactly one document on stdout.** `WriteJSON` writes one value and a newline in one write (`encoding/json`'s `Encoder.Encode`, §3), HTML not escaped. `Emit` routes a run: as `FormatJSON`, stdout gets the document and nothing else, and everything for a person (the configuration's deprecated values) goes to stderr; as `FormatHuman`, stdout gets `Human`'s report through its `Env.Writer`. Tested: `TestStdoutIsOneDocument` decodes stdout once, then expects `io.EOF`, and checks the nine fields and that the warning went to stderr, for every fixture; `TestEmitHuman`.
10. **The human report through `pkg/ui` only** (design-system README §9.15): `StatusList`, `PlanView` (with the dry-run footer), `StepProgress`, `Callout`, `ConfirmCountdown`, `Message` and `ErrorReport`, composed by a pure function of the run, the `Env` and the clock (`Human`). A change waiting for confirmation shows the countdown, its port when every listener it adds has one, and the command in the host's elevation tool (`doas hardn confirm` with doas and no sudo: engine.md §7, "doas on Alpine"), then when it is rolled back unless confirmed, **"at about"** when the deadline is approximate, and how to undo it now. A refusal is shown by its `Why`, with its code, and its `Fix` as the next step ("To go on: …", steps' words); an apply's failure by its change and code, the changes rolled back (last first, as the engine undid them), and the failing command with its output; the planners' and checks' findings as warnings with their codes and sources. Tested: `TestHuman`, a golden per fixture on the core cells (`testdata/golden/human/*.golden`), which `uitest` checks for width and determinism.
11. **`pkg/ui/flow`'s `Engine`** over the engine, as engine.md §7 describes (`FlowEngine`): `Plan` is `Engine.Plan`, each change a `component.PlanChange` in `Plan.Changes`' order (summary, target, diff, states, commands, lockout risk), the plan `RiskLockout` when the engine's is; a refused plan a `*flow.Failure`. `Apply` is `Engine.Apply` of that plan, with `Options.Progress` (the adapter's `Progress`) the one source of the steps' states: `EventRunning`, `EventDone`, `EventFailed` (its detail the error's code) and `EventRolledBack` to `StepRunning`, `StepDone`, `StepFailed` and `StepRolledBack`, `Index` -1 and events outside an apply ignored. The shown plan's `Change.Apply` functions are never wrapped, and a plan is never applied twice. A failure is a `*flow.Failure` whose text is the change's summary, the error and its code, which still unwraps to the engine's error (`errors.Is(err, engine.ErrRolledBack)`), with the failing command and its output, the log, and the next step (finishing an unfinished rollback). `Run(outcome, err)` gives the `Run` for `Build`, `ExitCode`, the report and the audit log. Tested over the real engine on the fake, with a control that writes a file: `TestFlowEngineApplies` (the reports in order; none after `Apply` returns), `TestFlowEngineFailure` (the rollback reported, the file restored, the document), `TestFlowEngineRefused`, and `TestFlowEngineInAFlow`, which runs the canonical flow on it: applied (0), the same action again with nothing to change (0), aborted at the confirmation by the end of the input (130), declined (3, nothing changed).

### The audit log (`Append`, `AuditPath`)

12. **One JSON line per run**, a `Record`: the run's time (UTC) and its document, the same nine fields as stdout's, so the plan, the results and any rollback (`result.rollback`) are there. `Append(ctx, sys, path, rec)` makes the directory when missing (`0750`), refuses it unless it is a directory owned by root that neither group nor others can write (`ErrAuditDir`, `AUDIT_DIR_UNSAFE`: a record holds the plan's diffs), then appends the line through `host.System.AppendFile`, creating the file `0600`. The path is a constant, `AuditPath`, `/var/log/hardn/audit.jsonl`, as the plan proposes. **Not wired** into `cmd/hardn`: its first writer is `next/p3-startup`'s (§7). Tested: `TestAppend` (two runs, two lines, each a `Record`; the modes; appended, never rewritten), `TestAppendRefuses` (a directory others can write, another user's, a symlink at the log's path: refused, nothing written, the link's target untouched).
13. **`host.System.AppendFile`**, new: the seam had no append, and the audit log needs one (§5). One write on a descriptor opened with `O_APPEND`, so two runs appending at once never overwrite each other's lines, which a read-and-rewrite with `WriteFileAtomic` would (the second rename wins, and the first run's record is lost), at a cost that grows with the log; then `fsync`. It is guarded like `WriteFileAtomic`: a symlink at the path refused (`O_NOFOLLOW`, after `lstat`), a directory or anything else that is not a regular file refused (`O_NONBLOCK`, so a FIFO swapped in does not block the open), a file with a second hard link in a directory another user can change refused (`ErrHardLink`, the rule `Chmod` and `Chown` keep), symlinks on the way followed only in trusted directories. Tested: the contract test runs the create, the append and every refusal on `host.Real` in `t.TempDir()` and on the fake alike; `TestRealAppendRefuses` (a FIFO, a hard link in an untrusted directory and then in a trusted one, a done context).
14. **The legacy log's path and format**, as the brief's [Verify] asks, recorded as `LegacyLogPath` and in the reference (§4 there), from [phase0-findings.md](../refactor/phase0-findings.md) §6 (**[V]**). It is unchanged. The run log (`host.Real`'s record of each command, [system.md](../safety/system.md) §2.6) goes where its caller sends it: `cmd/hardn`'s `revertLog` sends it to the legacy log, and **that is the run log's path, a decision of this unit**, until `next/p3-startup` makes the startup writes lazy and can move both.

### The non-interactive policy (`Confirmation`)

15. **Never prompt without a terminal; apply non-interactively only with an explicit yes**, as a library function of what decides it (`Interaction{InputTTY, JSON, Yes}`): yes is `Apply`, with or without a terminal; no terminal on stdin, or JSON output, without yes is `PlanOnly`, with `ErrConfirmationRequired` (exit 2) naming `--yes`, which is the plan row's "print the plan and exit 2"; otherwise `Ask`. **A decision of this unit:** a JSON run never prompts, even with a terminal, since its stdout is a script's and a prompt there would break the one document. It is not wired (§7). Tested: `TestConfirmation`.

### What `cmd/hardn` now does (`cmd/hardn/output.go`)

16. **§3.17's deprecation warning**: right after the root command loads the configuration, each `Config.Deprecations()` entry is printed to **stderr**, once, as a warning `Message` through `pkg/ui`, after the file it was read from, with `pkg/ui/term`'s detection for stderr (`warnDeprecations`, `output.Deprecations`). The run goes on: the value still works. Stdout, where the legacy logger writes, does not repeat it. Tested: `TestDeprecationWarning` (a `hardn.yml` in `t.TempDir()` with `permitRootLogin: false`, loaded by `pkg/config`, through the real root command with the provider mocked: the warning once, with the line and what to write instead, no escape codes; none for a configuration without it).
17. **No colour codes from `hardn confirm`, `hardn rollback <id>`, `--abandon` and `--boot`** unless both stdout and stderr are colour terminals by `pkg/ui/term`'s detection (`term.Detect` with the stream as its output: a terminal, `TERM`, `NO_COLOR`, `HARDN_PLAIN`, and `--no-color` as `--color=never`; the theme fixed, so the terminal's background is never queried). `setRevertColour` runs first in both commands and sets `fatih/color`'s `NoColor`, which the root command's `initializeColor` sets to false before every run. **Why both streams, where the task named stderr:** the legacy logger these commands print through writes to stdout (fatih/color's `Output`, §3), the notices to stderr, and the journal takes both. Tested: `TestRevertColour`, each of the four commands with colour off (no `ESC[` in what it printed) and on.

### Through `host.System`

18. Depguard rule `output-through-host`, the last of the rules, denies `os`, `io/ioutil`, `golang.org/x/sys`, `syscall`, `net` (but `net/netip`) and `plugin` to `pkg/output`'s non-test files: it reaches the host only through `host.System`, and writes only to the writers it is given. It renders through `pkg/ui`, which it may import; the `charm` rule keeps Lip Gloss out of it.

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| With `O_APPEND`, "the file offset shall be set to the end of the file prior to each write and no intervening file modification operation shall occur between changing the file offset and the write operation" | POSIX [write](https://pubs.opengroup.org/onlinepubs/9799919799/functions/write.html) | §2.13: concurrent appends never overwrite each other |
| A write to a regular file may write fewer bytes than asked (a full disk) | POSIX write | the reference §4: a line cut short; Go's `os.File.Write` loops until all is written or an error |
| `encoding/json`'s `Encoder.Encode` builds the value and its newline, then calls the writer's `Write` once | Go 1.27.2 `src/encoding/json/stream.go` (read here) | §2.9: one write per document |
| fatih/color v1.19.0 sets `NoColor` at init from stdout being a terminal (and `NO_COLOR`, `TERM=dumb`), and prints through `Output`, colorable stdout | `color.go`, lines 15-60, the version in `go.mod` (read here) | §2.17: the legacy logger writes to stdout; `initializeColor`'s `color.NoColor = false` (`cmd/hardn/main.go`) undoes its own check, hence the codes in the journal |
| The Debian 13 and Ubuntu 26.04 runs found the legacy logger's colour codes in the systemd journal | [vmtest.md](../safety/vmtest.md) §4.9 (**run**, 2026-10-19) | §2.17 |
| The OpenRC sleeper runs with its streams on the null device | [revert.md](../safety/revert.md) §5 (`StartDetached`) | §2.17: no terminal there either |
| The legacy log: `/var/log/hardn.log`, opened before flags are parsed, `O_CREATE|O_APPEND` `0644`, `log.LstdFlags` and `LEVEL:`, no rotation; `logFile` only read | [phase0-findings.md](../refactor/phase0-findings.md) §6 (**[V]**), `pkg/logging/logging.go` | §2.14 |
| `pkg/ui/term`'s detection and precedence | [design-system README](../design-system/README.md) §3 | §2.16, §2.17 |
| The engine's events, `Result`, `ApplyError` and the boot unit's place outside `Result.Applied` | [engine.md](engine.md) §2.15, §6, §7; [engine-boot.md](engine-boot.md) §2.1, §5 | §2.3, §2.11 |
| A `steps.Refusal` matches its code's sentinel by `errors.Is` and wraps the primitive's refusal | [steps.md](steps.md) §6, §7 | §2.5 |
| Each primitive's finding kind's `String` is its stable code, pinned by its own `TestFindingCodes` or `TestCodes` | [steps.md](steps.md) §2.23; [sudoers.md](../safety/sudoers.md), [alpine.md](../safety/alpine.md), [dns.md](../safety/dns.md) §6 | §2.6 |
| Ubuntu's `/var/log` is group-writable by `syslog`, so its entries are not root's alone | **[unverified]** | §2.12: why the directory is checked rather than trusted |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root. On the VMs of [vmtest.md](../safety/vmtest.md):

- **The colour rule:** on Debian 13 and Ubuntu 26.04, a change left unconfirmed so that the timer's `hardn rollback <id>` runs, then `journalctl -u hardn-revert-<id>.service -o cat | grep -c $'\x1b\['` is 0; `sudo hardn confirm` over SSH still in colour; `sudo hardn rollback <id> | cat` and `2>/dev/null` without; on Alpine 3.24, the sleeper's rollback leaves no escape codes in `/var/log/hardn.log` (the file had none before either: the logger writes plain lines there) and `hardn rollback --boot` at boot none on the console log.
- **The deprecation warning:** `sudo hardn -g` with a v0.3.x `hardn.yml` whose `permitRootLogin` is `false` warns once on stderr (`2>/dev/null` hides it), and DNS is configured as before.
- **`AppendFile` as root**, through a test build only (nothing writes the audit log yet): `/var/log/hardn` made `0750 root:root` on each distribution, `audit.jsonl` `0600`; refused for a directory made group-writable; two appends from two processes at once, each line whole.
- The JSON document and the audit log run on a host only once `next/p3-startup` and §3.11 wire them.

## 5. Changes to other units' code

- **`pkg/host`** (`next/p2-system`'s): `System.AppendFile(ctx, name, data, perm) error`, with `Real`'s (`append.go`) and the fake's (`hostfake/append.go`, recorded as `append`), §2.13. [system.md](../safety/system.md)'s API list predates it. Every implementation of `System` embeds `Real` or the fake, so none else changed. The contract test gains two cases; the fake's `Call` documents the new op.
- **`cmd/hardn`**: `output.go` (new), one line in `main.go` (the warning after the configuration loads) and one in each of `runConfirm` and `runRollback` (the colour); `main_test.go` gains a host mode whose configuration file holds the bool, and `stderrEnv` set to plain in `runHardn`. No flag, subcommand, key or default changes (§3.11 is not approved).
- **`.golangci.yml`**: `output-through-host`, appended last (§2.18).
- No dependency changes: `go.mod` and `go.sum` are unchanged.

## 6. API

```go
package output // pkg/output (new)

const SchemaVersion = 1
type Document struct { SchemaVersion int; HardnVersion string; Host Host; Mode Mode; Result Result
	Changes []Change; Status []Status; Findings []Finding; Errors []Error }
type Mode string // ModeCheck "check", ModePlan "plan", ModeApply "apply"
type Host struct { Hostname string; OS OS; Platform Platform; Init, SSH string; Proxmox Proxmox; Container Container; Elevation Elevation }
type Result struct { Outcome Outcome; ExitCode int; ID, Risk string; Confirm *Confirm; Rollback *Rollback; Boot *Boot }
type Outcome string // OutcomeNothingToDo, OutcomeApplied, OutcomeAwaiting, OutcomeChangesNeeded, OutcomeRefused,
                    // OutcomeFailed, OutcomeRolledBack, OutcomeRollbackIncomplete, OutcomeUsage, OutcomeCancelled; Exit() int
type Confirm struct { Deadline time.Time; Approximate bool; Via []string; Command, Undo string }
type Rollback struct { Finished, ByAnotherProcess bool; Step, Change string; Restored, Undone []string; Unjournalled, Command string }
type Boot struct { Installed bool; Detail string }
type Change struct { ID, Control, Summary, Target, Risk, Stage string; After []string; Diff, StateBefore, StateAfter string
	Commands, Backups []string; Status, Detail string } // StatusPlanned … StatusUnjournalled
type Status struct { Control, Title, State, Detail string; Evidence []string }
type Finding struct { Code, Detail, Source, Control, Change string }
type Error struct { Code, Message, Control, Change, Step, Why, Fix string }
type Record struct { Time time.Time; Document } // one audit line

type Run struct { Mode Mode; HardnVersion, Hostname string; Facts *detect.Facts; Reports []engine.Report
	Plan *engine.Plan; Result *engine.Result; Declined bool; Err error; Deprecations []config.Deprecation; ConfigPath string }
func Build(r Run) Document

// The registry (codes.go)
type Kind string // KindError, KindFinding, KindBoot
type Entry struct { Code string; Kind Kind; Exit int; What string /* matchers unexported */ }
const CodeUnclassified = "UNCLASSIFIED_ERROR"; const CodeDeprecated = "CONFIG_DEPRECATED_VALUE"
func CodeOf(err error) string
func Codes() []Entry
func Lookup(code string) (Entry, bool)
func BootCodes(a revert.BootAction, r revert.BootReason) (action, reason string)

const ExitOK, ExitError, ExitUsage, ExitChanges, ExitInterrupted = 0, 1, 2, 3, 130
func ExitCode(r Run) int

func WriteJSON(w io.Writer, d Document) error
type Format uint8 // FormatHuman, FormatJSON
type Streams struct { Stdout io.Writer; Out term.Env; Stderr io.Writer; Err term.Env }
func Emit(s Streams, f Format, r Run, now time.Time) error
func Human(env term.Env, r Run, now time.Time) string
func Deprecations(env term.Env, path string, deps []config.Deprecation) string

type FlowEngine struct { Engine *engine.Engine; Controls []engine.Control; Config *config.Config; LogPath string /* … */ }
func (*FlowEngine) Progress(engine.Event)                              // engine.Options.Progress
func (*FlowEngine) Plan(ctx context.Context) (flow.Plan, error)        // flow.Engine
func (*FlowEngine) Apply(ctx context.Context, report flow.Report) error // flow.Engine
func (*FlowEngine) Run(o flow.Outcome, err error) Run

const AuditPath = "/var/log/hardn/audit.jsonl"; const LegacyLogPath = "/var/log/hardn.log"
const AuditDirMode, AuditFileMode = 0o750, 0o600
var ErrAuditDir error
func NewRecord(r Run, at time.Time) Record
func Append(ctx context.Context, sys host.System, path string, rec Record) error

var ErrUsage, ErrConfirmationRequired error
type Decision uint8 // Ask, Apply, PlanOnly
type Interaction struct { InputTTY, JSON, Yes bool }
func Confirmation(in Interaction) (Decision, error)

package host // added
// in System: AppendFile(ctx context.Context, name string, data []byte, perm fs.FileMode) error

package main // cmd/hardn, added
// warnDeprecations(*config.Config), setRevertColour(); seams stderrEnv, revertColour
```

## 7. Not in this unit; inputs to later units; decisions; size

- **`next/p3-startup`** (after `next/p7-install`): wire the output into `cmd/hardn`: `Emit` and `ExitCode` for each engine run, and `Append(…, AuditPath, NewRecord(…))` at its end, after the report, whatever its outcome; the audit records of `hardn confirm`, `hardn rollback <id>` (the timer's included: revert.md §7's "audit record of a rollback"), `--abandon` and `--boot`, which need `Mode` values of their own and a result object each (`BootCodes` for `--boot`): additions the schema's rule allows without a new version; the run log moved with the lazy startup writes, if it moves; the timer's `hardn rollback <id>` through `main()` (V26) stays that unit's.
- **For the maintainer, §3.11 (not approved):** `--output json` (then `FormatJSON`, and `-n` as `ModePlan`), `--yes` (then `Confirmation` at each operation flag, as its slice moves it onto the engine, starting with slice 2, the DNS pilot: no flag consults it before its operation runs on the engine). The policy is ready; nothing calls it.
- **`--confirm-timeout`** (§3.14, approved): not added here, as this unit's task says; `engine.ConfirmTimeout` takes the flag, so the unit that wires the first lockout-risk apply (slice 6, or `p3-startup`) adds it with the engine. The README's item stays open.
- **The words** of the human report that are this unit's (provisional, as the Phase 2 pages' are): "Unless confirmed, change <id> is rolled back at [about] <time>. To undo it now, run <command>."; "To go on: <fix>."; "The rollback did not finish: run <command> to finish it."; "Another hardn process is rolling the change back; wait for it to end."; "What <change> did may be left on the host: check it, as the error says." Each finding and error shows its code in parentheses. P-24 (the countdown's words) and P-27 (the flow's) are where the maintainer would decide them.
- **Not given codes:** `reload.Report.Reason` is a stable code already ([reload.md](../safety/reload.md) §6) and is not an error; `revert.NoticeKind` is the main menu slice's (Phase 4); `hostfake.ErrUnscripted` (a test double's).
- **Decisions of this unit**, none the maintainer's: the exit statuses' five readings (§2.8); a JSON run never prompts (§2.15); colour for `confirm` and `rollback` only when both streams are colour terminals (§2.17); one code for the three unsafe-directory sentinels, as sudoers.md asks; `result` an object, so that the nine top-level fields stay nine; the run log stays on the legacy log (§2.14); the audit log's directory checked to be root's alone (§2.12).
- **Size:** about 1,970 lines of non-test Go in `pkg/output` (with its doc comments; the registry alone about 510, one line per code), 115 in `pkg/host` and 65 in `cmd/hardn`, and about 1,590 lines of tests, against the plan's guideline of about 500, excluding testdata (the goldens: five documents, five human reports on three cells each, the code and sentinel lists, about 1,500 lines). The unit is the whole of the plan row: wire types and their mapping, a registry of 232 codes with the source scans that keep it complete, the exit statuses, three renderers, the flow adapter, the audit log with a new seam operation, the policy, and two wirings. It could have been split as the registry and exit codes first, then the renderers and the flow adapter, then the audit log; the commits split it as the seam's operation, the package, the command line and the docs, because the package's tests tie the registry to the reference page and the documents to the renderers.
