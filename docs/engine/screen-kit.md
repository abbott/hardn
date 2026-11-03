# The screens' shared glue and the parity harness (`pkg/screens/screenkit`, `internal/parity`)

> **Unit:** refactor plan slice 3 (Backup), unit (b), its first part, `next/s3-shared`, from `next/s3-gallery` at 2695315. The plan: "The parity harness stays in pkg/screens/dns's tests until slice 3 is a second user, then moves to a shared package"; [gallery-backup.md](gallery-backup.md) §9: unit (b) "begins by extracting the DNS screen's shared glue … and the parity harness … into shared packages"; [screen-dns.md](screen-dns.md) §11 named the pieces.
> **Status:** built and tested on macOS, in process, on `hostfake`. **No behaviour change**: no golden and no testdata changed (`git diff --stat 2695315 -- '*testdata*' '*.golden'` is empty, run), and [parity-dns.md](parity-dns.md)'s generated region is unchanged, `-update` included (run). Nothing here ran on a host, and nothing here claims anything of one.

## 1. What moved, and why

Backup's screen (unit (c)) and its parity table (b2) are the second users of what the DNS screen held to itself. Each piece does what it did. What changed is its name, exported, and, where a DNS detail was built in, a parameter: the screen's name in a failed write's error, a change's controls, the directories of origin/main's record.

| Piece | Was | Is |
|---|---|---|
| The flow's engine, built once the form is answered, its apply under its own context | `pkg/screens/dns` `planning` | `screenkit.Planning` |
| A change: the flow, then `output.AfterFlow`, `Ran`, and hardn.yml adopted as saved | `pkg/screens/dns` `runner.change` (its tail) | `screenkit.Session.Change` |
| hardn.yml adopted once the change saved it | `pkg/screens/dns` `adopt`; the gallery's Backup mocks' copy | `screenkit.Adopt` |
| A warning before asking, set apart under the prompt | `say`, `write`; the gallery's copy | `Session.Say`, `Session.Write`, `Warnings`, `Apart` |
| A refusal's words; the refusal with no hardn.yml read | `refusalWords`, `fullStop`, `notLoaded` | `RefusalWords`, `FullStop`, `NotLoaded` |
| `TestAdopt` | `pkg/screens/dns` | `pkg/screens/screenkit` |
| The parity harness | `pkg/screens/dns/parity_harness_test.go` | `internal/parity` |

**Why `pkg/screens/screenkit`.** Under `pkg/screens`, depguard's `screens-through-host` guards it as it guards every screen's glue: the host only through `host.System` and the engine. Not `pkg/ui`, which `ui-renders-only` keeps from the engine, hardn.yml and `pkg/output`; not `pkg/output`, the output contract, which renders a run and runs no screen; not `pkg/config` for `Adopt`, which reads the file back through `host.System` after a flow, a screen's step. A kit: what each area's screen is built with.

**Why `internal/parity`.** A `_test.go` file cannot be imported. The harness reads goldens and writes the page with `os`, which `screens-through-host` denies under `pkg/screens`; `internal/tools` may not be imported at all (`dev-tools`), and tests must import it. Under `internal`, nothing outside the module takes it; depguard's `test-only` rule lets its files take `pkg/ui/uitest` and `hostfake`, and its own rule, `parity-tests-only`, denies it to every non-test file, the dev tools' included (probed: a non-test file under `pkg/screens` or `internal/tools` importing it fails lint, a `_test.go` file passes). It links `pkg/ui/uitest`, which defines `-update`, so that `make golden-update` reaches every package whose tests use it.

## 2. API

```go
package screenkit // pkg/screens/screenkit

type Session struct { // one run of a screen: what its caller gave it
	Name         string // in a failed write's error: "dns screen"
	Prompter     nav.Prompter
	System       host.System
	Config       *config.Config // adopts each change's saved copy
	NewEngine    func(progress func(engine.Event)) *engine.Engine
	ApplyContext func(ctx context.Context) (context.Context, context.CancelFunc)
	DryRun       bool
	LogPath      string
	Now          func() time.Time // nil is time.Now
	Ran          func(output.Run)
}
func (Session) Change(ctx context.Context, c Change) error
func (Session) Say(msgs component.Messages) func() error
func (Session) Write(text string) error

type Change struct {
	Step     screen.WorkflowStep
	Flow     flow.Flow // Form, Review, Confirm; Engine and DryRun are the session's
	Controls func(edited *config.Config) []engine.Control // edits the copy, returns the controls
}

type Planning struct { *output.FlowEngine; Build func(); ApplyContext func(context.Context) (context.Context, context.CancelFunc) }
func (*Planning) Plan(ctx context.Context) (flow.Plan, error)
func (*Planning) Apply(ctx context.Context, report flow.Report) error
func (*Planning) Planned() bool

func Adopt(ctx context.Context, sys host.System, c, edited *config.Config)
func NotLoaded(ctx context.Context, sys host.System, cfg *config.Config, id, setting string, more ...string) []string
func RefusalWords(err error) string
func FullStop(s string) string
func Apart(c component.Ctx, msgs component.Messages) string
func Warnings(paragraphs ...string) component.Messages
```

```go
package parity // internal/parity: tests only

type Slice struct {
	Scenarios []Scenario; Distros []Distro; Table []Tie
	Header, Golden, Page, Pkg, Setting string
	Legacy, Fixtures string // origin/main's goldens and their fixtures, from the package
	V032Sections     string // v032-reference.md's sections the page cites
	Shipped, Exists func(state HostState, d Distro) string
	Probes  []string
	Run     func(t *testing.T, c Cell) NewRun
	Content func(add func(name, legacy, new string), l, n []FileWrite)
	LegacyPaths     []string // since next/s3-parity: origin/main's temp-dir paths as the new path's ("$TMP/home" as "$HOME", "$TMP" as the root)
	UnderBackupPath bool   // since next/s3-parity: compare what is written under the backup path
	Check func(argv string) bool // since next/s9-parity: the commands that check a write, compared as "checks"
	Temps bool                   // since next/s9-parity: compare the temp files each path wrote and removed
	Either bool                  // since next/s9-parity: compare content, backup and journal where either path wrote
	CellNote func(c Cell) string // since next/s9-parity: a cell's own note
	Reader func(data []byte, rel string) (LegacyRead, error) // since next/s1-parity: a slice's own reader of its goldens
}
func Run(t *testing.T, sl Slice) // a slice's TestParity
type Scenario, Distro, HostState, Tie, Cell, NewRun, EndState, FileWrite // Scenario.Setting since next/s3-parity; Tie.Pending, EndState.Checks and .Temps since next/s9-parity; Scenario.Only, EndState.Rows since next/s1-parity
type Row, LegacyRead // since next/s1-parity
func NewState(t *testing.T, h *hostfake.System, n int, readOnly []string, yml string) EndState
func NewStateUnder(t *testing.T, h *hostfake.System, n int, readOnly []string, yml, dir string) EndState // since next/s3-parity
func NewStateChecked(t *testing.T, h *hostfake.System, n int, check func(argv string) bool, yml string) EndState // since next/s9-parity
func (Cell) LegacyStart() string                                                                    // since next/s3-parity
func Planned(s EndState, p *engine.Plan) EndState
func HostText(s EndState) string
func Joined(l []string) string
func OrElse(s, none string) string
```

`Slice.Legacy` and `Slice.Fixtures` are new: the harness read DNS's frozen record at a fixed path, and Backup's (b2) is to read `pkg/characterization`'s goldens in place until its unit (d) freezes them, once the harness reads their layout (§7). `checkTable` now reports what `tableProblems` returns, and `readLegacy` parses through `parseLegacy`, which returns its failures, each in the same words, so that they can be tested. `parseLegacy` also fails on a golden with no `== cfg.DryRun=false` or `== result` section, the run it compares, which before read as a run that did nothing.

## 3. What stays per screen

- **DNS screen:** `Options` and `Run`, unchanged; the reading (`dns.Read`, the control's `Check`); the items and their words; `hostWords`, which says what failed for `DNS_UNKNOWN`; Add's check. Its change is a `screenkit.Change`: Alpine's udhcpc question added to the form, the review, the question, and `Controls` setting the copy's list and returning the DNS control, which reads the session's configuration, not the copy, as before.
- **DNS parity:** `parity_test.go`: the scenarios, distributions and table, the header, the run of the new path (`configure`, `menuItem`, `beforeAsking`), the file aspects (`servers`, `search`, `others`), the record's directories and the v0.3.2 sections the page cites (`V032Sections`; the moved harness had DNS's built in). Its table keeps its rows through a local positional `tie`, converted by `ties`, because go vet takes unkeyed fields only for a package's own types.
- **Backup** (unit (c), and its (b2)): its reading, items, words and view-model; its control and refusals (unit (b)'s second part); its scenarios and table. *Built in `next/s3-screen` ([screen-backup.md](screen-backup.md)): `pkg/screens/backup` runs each change with `Session.Change` and says its refusals with `Session.Say`, `Warnings`, `NotLoaded` and `FullStop`; no change to this package.*
- **Environment** (slice 9, unit (c), and its (b2)): its reading, item 1, words and view-model; its scenarios and table. *Built in `next/s9-screen` ([screen-environment.md](screen-environment.md)): `pkg/screens/environment` runs its one change with `Session.Change`, its `Ran` wrapped to say after an applied change that it takes effect at the next sudo hardn, its session's configuration one read from no file (the control saves no setting, so `Adopt` leaves it, and `NotLoaded` does not apply), and says its refusals with `Session.Say`, `Warnings`, `RefusalWords` and `FullStop`; no change to this package.*
- **The typed confirmation** moved here from Environment in slice 4, unit (c) (`next/s4-screen`), as Firewall is its second user and slices 5 and 6 follow: `Hostname(ctx, sys) (phrase, short, err)`, `TypeTheHostname`, `Session.CantConfirm(err)`. Environment's words and goldens are unchanged.

## 4. Tests

- `pkg/screens/screenkit`: `TestChange` (applied and adopted, the next change's save then not refused; declined, a dry run and a quit at the question planned, given to `Ran`, nothing written or adopted; back from the form, nothing planned, no engine), `TestApplyContext`, `TestAdopt` (moved), `TestNotLoaded` (one setting or two), `TestRefusalWords`, `TestSay`.
- `internal/parity`: `TestReadLegacy` (a synthetic golden), `TestParseLegacyRefuses` (a golden in another layout, a removal), `TestNewState`, `TestTableProblems` (listed, unlisted, stale, a V-number not named, twice), `TestWhere`, `TestPageCites`.
- Unchanged and passing: every DNS screen test, `TestParity` (108 subtests), the gallery's.
- Mutation-checked (run, with `go test -overlay`): with the adoption removed from `Session.Change`, DNS's `TestAdd`, `TestRemove` and `TestPresets` fail, and `TestChange`; with it removed from the Backup mocks, `TestPlayBackupGolden` fails; through the moved harness, a DNS table entry removed fails UNEXPLAINED, and a stale entry and one tied to V12 fail.

## 5. Words

None new. No user-facing word was added or changed: `screenkit` says only what its callers give it.

## 6. Goldens

None changed.

## 7. Not done; open

- **The Backup mocks' change** keeps the gallery's own flow engine (`recording`, which marks the fake's record around the apply) rather than `Session.Change`: it is no copy of the DNS screen's, and unit (c) retires the mocks. *Retired in `next/s3-screen`: the gallery plays the real screen, whose changes run through `Session.Change`; only the restore's mock keeps `recording`.*
- **Decision 7 (a)** (a change of Backup's settings planned under the settings it saves) goes through the engine, not `Session`. A control that saves them implements `engine.BackupsSaver`, and the engine plans, checks, backs up and journals that plan under the settings it saves (`Plan.Backups`; `pkg/engine/plan.go:263`, `apply.go:303`). A plan that mixes it with another control's change is refused (`ErrConflict`). `Session`, `Planning` and `NewEngine` are unchanged: `NewEngine` is still called before `Change.Controls` edits the copy, with no configuration ([control-backup.md](control-backup.md) §10, decision 1).
- **`Slice.Setting` is one hardn.yml key per slice** (read): the `hardn.yml <setting>`, `settings` and `rest` aspects key on it (`settingOf`, `settingsVs`, `rest`, `outside`). Backup's toggle saves `enableBackups` and its path item `backupPath` (gallery-backup.md §9: "hardn.yml's two keys"), so its (b2) needs a key per scenario; not built here. The page's own section numbers (§3, §4) assume a page laid out as parity-dns.md. *Built in `next/s3-parity` ([parity-backup.md](parity-backup.md)): `Scenario.Setting`, a scenario's own key, `Slice.Setting` when empty (DNS's); a cell where both paths saved and neither names a key fails; parity-backup.md is laid out as parity-dns.md.*
- **Backup's goldens are in a layout the parser does not read** (run): `pkg/characterization`'s `menu/backup_*` record the run under "The repository's file system calls" and the saved file whole under "./hardn.yml as written", with no `== result` section. Read through the harness, `backup_toggle_on`, `backup_toggle_off` and `backup_path_relative` came back as runs that did nothing; they now fail. Backup's (b2) needs a reader for that layout, comparing its `remove` (the write test's `.write_test`) too. *Built in `next/s3-parity`: `parseLegacy` hands that layout to `parseRepository`, which reads `Start:`, the screen's own write test on each draw (its Backup Path row and its result line, marked `(screen)`), the calls between the first draw and the second (`mkdir`, `write`, `remove`; a `mkdir` or `write` that failed is the run's refusal, one at most), the after line and `./hardn.yml` whole, its mode not recorded (`FileWrite.NoMode`, not compared), with `$TMP/home` read as `$HOME` and `$TMP` as the root (`Slice.LegacyPaths`), and fails on any other line or call, a failed `remove`, a second failure or a trailer section twice; a V-number anywhere in a `KNOWN-DEFECT` or `FIXED` line is named (DNS's goldens put it first, and read as before).*
- **`NewState` leaves out every write under `/var/backups/hardn`** as the new path's own. Backup's (b2) compares what is written under `backupPath`, so it will need that directory as a slice's parameter; not built here. *Built in `next/s3-parity`: `NewStateUnder` takes the backup path from the slice, Backup's the one the change saves (decision 7 (a)), and records what was written there (`EndState.Under`, each run as `<run id>`), compared with `Slice.UnderBackupPath` as `backupPath dirs`, `backupPath files` and `backupPath after`; `NewState`, DNS's, leaves `/var/backups/hardn` out as before. A backup taken anyway reads so.*
- **Environment's goldens are in a third layout** (`pkg/characterization`'s `environment/keep_*`: `== / before`, `== result`, `== calls`, `== / afterwards`), which the parser read as a run that did nothing, and its operation writes and removes a temp file and runs visudo, which the ops reader failed on and took for a change. *Built in `next/s9-parity` ([parity-environment.md](parity-environment.md)): `parseLegacy` hands that layout to `parseRoot`, which reads the environment and `/ before` as the start, the outcome, the calls (`stat`, `read`, `write`, `exec`, `remove`) and `/ afterwards` as the end state, each change explained by a write (or by a temp file's write through a symlink planted at its name, its target shown changed and the link gone) and each write shown, a failed call the refusal, one at most, and fails on any other line, section or operation. With `Slice.Temps` a removal of a file the run made, written then removed, is a temp file, in both layouts, any other removal still failing, of a file `/ before` holds or the fixture seeded too; with `Slice.Check` the commands it names are checks. The ops layout's seed lines are its start (DNS's goldens have none). `NewStateChecked` records the new path's checks and what it wrote under the engine's run directory. With `Slice.Either` the content is compared where either path wrote a file, and the backup and the journal where either wrote, backed up or journalled; `Slice.CellNote` gives a cell a note of its own. A `Tie` may be `Pending`: its difference UNEXPLAINED in the golden and the page, its tie proposed. DNS's and Backup's tables, goldens and pages are unchanged, `-update` included (run).*
- *Since `next/s1-screen`,* Logs' screen uses `screenkit.Session.Say` and `Warnings` alone (a pager's refusal under the prompt), and System Details' nothing of screenkit, as neither has a change, and `TestParity` reads their rows from what they draw (`drive_test.go`; [screen-system-details.md](screen-system-details.md)); `internal/parity` did not change.
- **System Details' and Logs' goldens are in four layouts of their own, and neither screen changes the host.** *Built in `next/s1-parity` ([parity-system-details.md](parity-system-details.md)): `Slice.Reader`, a slice's own reader of its goldens, beside its test, the harness adding the V-numbers; `EndState.Rows`, each compared as an aspect, none where a side lacks it, a row the golden masks recorded and not compared, a row named twice or for an aspect failing; `Scenario.Only`, a row run on some distributions alone, one naming no distribution failing; `scan-lines` among `NewState`'s reads. 183 lines of the harness and 115 of its tests. DNS's, Backup's and Environment's tables, goldens and pages are unchanged, `-update` included (run).*
- **`golden` and `updating`** are still copied in `pkg/output`'s and `pkg/engine/controls/dns`'s tests ([parity-dns.md](parity-dns.md) §8); not touched.

## 8. Size

`git diff --numstat 2695315`, after review: `pkg/screens/screenkit` 270 lines of Go and 352 of tests, new; `pkg/screens/dns` 42 added and 163 removed in its code, its tests 140 added and 162 removed; `internal/parity` 371 added and 335 removed (moved), its tests 224 new; the gallery 11 added and 21 removed; `.golangci.yml` 14 and 1, the makefile 2. No testdata. Docs: this page, and a note each in screen-dns.md, parity-dns.md, gallery-backup.md and the design-system README.
