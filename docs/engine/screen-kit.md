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
}
func Run(t *testing.T, sl Slice) // a slice's TestParity
type Scenario, Distro, HostState, Tie, Cell, NewRun, EndState, FileWrite
func NewState(t *testing.T, h *hostfake.System, n int, readOnly []string, yml string) EndState
func Planned(s EndState, p *engine.Plan) EndState
func HostText(s EndState) string
func Joined(l []string) string
func OrElse(s, none string) string
```

`Slice.Legacy` and `Slice.Fixtures` are new: the harness read DNS's frozen record at a fixed path, and Backup's (b2) is to read `pkg/characterization`'s goldens in place until its unit (d) freezes them, once the harness reads their layout (§7). `checkTable` now reports what `tableProblems` returns, and `readLegacy` parses through `parseLegacy`, which returns its failures, each in the same words, so that they can be tested. `parseLegacy` also fails on a golden with no `== cfg.DryRun=false` or `== result` section, the run it compares, which before read as a run that did nothing.

## 3. What stays per screen

- **DNS screen:** `Options` and `Run`, unchanged; the reading (`dns.Read`, the control's `Check`); the items and their words; `hostWords`, which says what failed for `DNS_UNKNOWN`; Add's check. Its change is a `screenkit.Change`: Alpine's udhcpc question added to the form, the review, the question, and `Controls` setting the copy's list and returning the DNS control, which reads the session's configuration, not the copy, as before.
- **DNS parity:** `parity_test.go`: the scenarios, distributions and table, the header, the run of the new path (`configure`, `menuItem`, `beforeAsking`), the file aspects (`servers`, `search`, `others`), the record's directories and the v0.3.2 sections the page cites (`V032Sections`; the moved harness had DNS's built in). Its table keeps its rows through a local positional `tie`, converted by `ties`, because go vet takes unkeyed fields only for a package's own types.
- **Backup** (unit (c), and its (b2)): its reading, items, words and view-model; its control and refusals (unit (b)'s second part); its scenarios and table.

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

- **The Backup mocks' change** keeps the gallery's own flow engine (`recording`, which marks the fake's record around the apply) rather than `Session.Change`: it is no copy of the DNS screen's, and unit (c) retires the mocks.
- **Decision 7 (a)** (a change of Backup's settings planned under the settings it saves): `Session.NewEngine` takes no settings, and `Change.Controls` edits the configuration the engine plans with. Whether that is enough depends on how unit (b)'s second part gives the engine its per-plan input; not decided here.
- **Backup's goldens are in a layout the parser does not read** (run): `pkg/characterization`'s `menu/backup_*` record the run under "The repository's file system calls" and the saved file whole under "./hardn.yml as written", with no `== result` section. Read through the harness, `backup_toggle_on`, `backup_toggle_off` and `backup_path_relative` came back as runs that did nothing; they now fail. Backup's (b2) needs a reader for that layout, comparing its `remove` (the write test's `.write_test`) too.
- **`NewState` leaves out every write under `/var/backups/hardn`** as the new path's own. Backup's (b2) compares what is written under `backupPath`, so it will need that directory as a slice's parameter; not built here.
- **`golden` and `updating`** are still copied in `pkg/output`'s and `pkg/engine/controls/dns`'s tests ([parity-dns.md](parity-dns.md) §8); not touched.

## 8. Size

`git diff --numstat 2695315`: `pkg/screens/screenkit` 269 lines of Go and 349 of tests, new; `pkg/screens/dns` 42 added and 163 removed in its code, its tests 138 added and 162 removed; `internal/parity` 332 added and 320 removed (moved), its tests 181 new; the gallery 11 added and 21 removed; `.golangci.yml` 7 and 1, the makefile 2. No testdata. Docs: this page, and a note each in screen-dns.md, parity-dns.md, gallery-backup.md and the design-system README.
