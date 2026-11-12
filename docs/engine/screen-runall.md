# The Run All screen on the planner (`pkg/screens/runall`, `screen.RunAll`)

> **Unit:** refactor plan slice 8, unit (c), `next/s8-screen`, from `346e2fb9`. **Status:** built and tested on macOS, in process: the screen through `Run` on its own fake (`host_test.go` and `hosts_test.go` copied from `pkg/engine/runall`'s tests, Create's accounts model from `pkg/screens/users`'), the planner's reads and the plans on the real engine. No real terminal, no hardn binary, no VM. Not yet wired: main menu 7 still runs the legacy Run All until unit (d). *Since `next/s8-replace`: main menu 7 opens this screen, and `hardn -r` runs its plan ([cmd-engine.md](cmd-engine.md) §2.19).*

*Since `next/remove-environment` (2026-11-12): the Environment screen, `-e` and `setup-sudo-env` are removed ([control-environment.md](control-environment.md)); since `next/config-screen` Configuration is main menu 8 ([screen-config.md](screen-config.md)), and System Details and Logs are 9 and 10 again.*

## What it does

Main menu 7 as the maintainer adopted its mock on 2026-11-04 ([gallery-runall.md](gallery-runall.md) §5 and §6), drawn from Run All's planner ([control-runall.md](control-runall.md)): each round `runall.Read`, writing nothing, live and under plan only.

- **The screen:** "Run All"; the "Controls" section, each row the main menu's label and Run All's outcome alone (decision 10 (a)), a refused row "Refused:" and its words cut to the row; one item, 1) Apply all: "Plan, review, confirm" where something is planned (marked "Lockout risk" where the plan is lockout-risk), "Nothing to change" where nothing is (unavailable, or, with rows refused, saying them), "Refused" where the one plan refuses, "Change pending" where a change waits.
- **A reading that fails** draws one row, Run All "Refused:" in the error's words, and the item Refused, which says them: never an empty screen.
- **Apply all** reads the host again, then before asking anything: a change pending or the one plan's refusal is said and the screen comes back; each refused row is said whole, "<Row> skipped: <refusal>", as one warning; nothing planned goes back there. Then the planned rows' controls through `screenkit.Session.Change` with `Typed`: the review, one line per control; the plan from the engine; "Apply this plan?" above "Type the hostname", whatever the plan's risk (decision 6 (a)); the apply, confirmed from a new connection or reverted where the engine finds it lockout-risk. A live lockout-risk plan whose hostname can't be read says so before asking (as SSH Login); a plain one says so in place of the question. Plan only stops at the plan.
- **After an apply** the screen is drawn from a fresh reading: a lockout-risk change not yet confirmed reads "Change pending" on every row asked of and on the item.
- **0** returns (nil), **q** quits (`nav.ErrQuit`), the end of the input and Ctrl-C abort (`prompt.ErrAborted`), from the screen and the question.

## API

```go
runall.Run(ctx, runall.Options{
	Prompter, System, Facts /* detect's */, Session /* revert.ReadSession's */,
	Getenv /* os.Getenv */, Config /* hardn.yml as loaded; nil asks nothing */,
	NewEngine, ApplyContext, DryRun, LogPath, Now, Ran,
	Version /* hardn's, Auto Updates' record */, Revert /* revert.Options: zero in hardn */,
})
```

Required: the Prompter's In and Out, System, Getenv, NewEngine; else an error wrapping `fs.ErrInvalid`, nothing written. `Run` never calls `engine.New`: every engine is `NewEngine`'s, the planner's reads included. No plan saves hardn.yml.

- `screenkit.Change.Typed`: the hostname asked whatever the plan's risk; confirm-or-revert stays the engine's. It replaces the gallery's `raTyped`.
- `pkg/ui/screen/runall.go` draws only: `RunAll` (`Rows`, `State`, `Lockout`), `RunAllRow`, `RunAllState`, `RunAllItem`, `RunAllChange` and `RunAllApplyAll` (the step, the question, the review by control).

## For unit (d)

- **Main menu 7** calls `Run` as main menu 2 calls `ssh.Run`, with `os.Getenv`, the session `engineOptions` holds, the loaded hardn.yml, `NewEngine` as `engineOptions` builds it (hardn.yml's backups), `Version` hardn's; under plan only too. Its label and description stay ("Run All", "Execute hardening operations": decision 7 (a)).
- **`hardn -r`** on the planner (decision 8 (a)): on a terminal (stdin and stdout), the plan, skips said on stderr, "Apply this plan?" above "Type the hostname", then the timer and `sudo hardn confirm` where lockout-risk, as `-w` and `-d`; off a terminal any plan with changes refused, `PLAN_REFUSED`, nothing run, in §6's words (*amended 2026-11-04 for plain plans:* applied with `--yes`, without it `CONFIRMATION_REQUIRED`, 2; [gallery-runall.md](gallery-runall.md) decision 8); `-n -r` the plan alone, 3 with changes, 0 without; `-r` with `-c`, `-d`, `-w`, `-g` or `-e` a usage error, 2, nothing run; `-u` still names the user. `cmd/hardn`'s `typedEngine` already asks the hostname whatever the risk.
- **The legacy Run All and `HardenSystem` path deleted**, by trial deletion to a fixpoint: the list in `git show next/s8-survey:docs/engine/runall-removal.md`, §1 (rounds 0 to 8) in the order of §6.
- **hardn.yml:** the keys decision 4 leaves unused (`sshAllowedUsers`, `sshListenAddress`, `sshKeyPath`, `ufwAllowedPorts`, `enableAppArmor`, `enableLynis`, `sudoNoPassword`) warned once at load, as slice 6's; `permitRootLogin` keeps its deprecation; the examples lose them (`sshAllowedUsers: [george]` among them).
- **The gallery's next stage:** `uigallery`'s Run All mock to call `Run` (or draw `screen.RunAll`), retiring `raTyped`, `raReading.item`, `outcomes`, `screen`, `review`, `skipped` and `raRunner.apply`; its `runall_test.go` checks of rows and words now here and in the planner's tests.

## Decisions

- **The item** "Refused" where the one plan refuses, though rows are planned (the brief); the gallery's mock said "Plan, review, confirm" there. With nothing planned and rows refused it reads "Nothing to change", available, and says the refusals (gallery-runall.md §5, Made here).
- **The hostname typed whatever the risk** is a `screenkit.Change` option, not the screen's own engine, so Run All keeps `Session.Change`'s `AfterFlow`, `Ran` and `Adopt`.
- **A reading that fails** is one row titled Run All, the planner's title, in the error's words.
- **Labels** are each row's check's title (`planner.Row.Title`), which the main menu's labels are too: `output.StatusViewOf` labels every row with its report's `Title`.
- **The fake** answers `useradd`, `userdel` and `install -d` (copied from `pkg/screens/users`), which the planner's tests never apply.
- **Auto Updates** is asked where the gallery's hosts ask it (debian13, ubuntu2604, alpine323, fresh, partial, port, container, ufw-inactive, new-admin), over the gallery's apt and apk model copied into the fake (`updates_test.go`, from `internal/tools/uigallery/updateshost.go`): fresh and alpine323 read "Turn on", the rest "Already as asked".

## Words

§6's, decided: "Run All", "Controls", "1) Apply all", "Plan, review, confirm", "Nothing to change", "Refused", "Change pending", "Refused: <words>", "<Row> skipped: <refusal>", "Apply this plan?" above "Type the hostname". The title, the question and two outcomes are also the planner's constants, which the tests hold equal.

## Not run

No VM, no hardn binary, no real terminal. No host here refuses the one plan: the Refused item is tested on a reading built in the package (`refused_internal_test.go`).
