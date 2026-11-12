# Run All's planner (slice 8, unit (b))

*Since `next/s8-replace`: main menu 7 and `hardn -r` plan through it; the legacy Run All is deleted, and the keys it leaves unused warn at load ([cmd-engine.md](cmd-engine.md) §2.19).*

`pkg/engine/runall`, `next/s8-control`: what Run All plans, as real code that unit (c)'s screen (main menu 7) and unit (d)'s `hardn -r` both call, so the two never differ. It builds what [gallery-runall.md](gallery-runall.md) §5 adopted (2026-11-04, "Adopt all, start (b)") on the mock's behaviour (`internal/tools/uigallery/runall.go`: `raAsk`, `raReading.pick`, `alone`, `port`, `userThere`). Status: tested on macOS on `hostfake` only; nothing ran on a VM, no hardn binary ran.

*Since `next/remove-environment` (2026-11-12): the Environment screen, `-e` and `setup-sudo-env` are removed ([control-environment.md](control-environment.md)); since `next/config-screen` Configuration is main menu 8 ([screen-config.md](screen-config.md)), and System Details and Logs are 9 and 10 again.*

## 1. What it does

- **The mapping** (`Asked`, decision 4 (a)): `username` and `sshKeys`, Create with admin rights and the keys, the grant asking no password, as `-c`'s (`sudoNoPassword` unused; *since `next/sudo-password-opt-in`, 2026-11-11, the grant asks the user's password unless `adminNoPassword` is true: [control-users.md](control-users.md) §8*); `disableRootSSH: true`, root login off (`sshchange.Root{Want: "no"}`), "Root key-only" on Proxmox VE; `sshPort`, compared with sshd's listeners only, never planned; `enableUfwSshPolicy: true`, Enable (on Rocky Linux, AlmaLinux and RHEL left, "Status only": [el-firewalld.md](el-firewalld.md) §1.7); `configureDns: true` with `nameservers`, the nameservers; `enableUnattendedUpgrades: true`, on (`updates.AutoOn`, never off). SSH Auth and Environment: nothing asked; *since 2026-11-08 (the maintainer; [alpine.md](../safety/alpine.md) §7.1)*, SSH Auth on Alpine where sshd lacks CVE-2026-123158's fix is the openssh upgrade (`sshchange.Upgrade`, `Ask.Unfixed`: asked by the host, Nothing asked where the fix is there or not established), which claims sshd, so `afterUpgrade` makes root login off and Enable Later ("After the openssh upgrade: run Run All again"), and a Create refused for the fix (`users.ErrPasswordBypass`) too, before `afterFirewall`; tested: `TestUpgradeRow`, `TestUpgradeAsked`, `TestAfterUpgrade`. AppArmor ("Status only") and Backup ("Used, not changed") are left alone.
- **`Read`**, writing nothing: the revert state first (`revert.ReadState`), then each row's check, then each row in the screen's order (`IDs`: the main menu's, then Backup and Environment). A row is left, nothing asked, Not Applicable by its check (its reason), the port compared (`Refused` where sshd cannot be read), `Pending` where a change is journalled (every row asked of but the port and Not Applicable ones, none planned: the SSH Login screen's words where an applied lockout-risk change waits for `hardn confirm`, revert's own, with its `hardn rollback <id>`, for an unfinished rollback or a change that did not finish), else its control planned alone: a refusal is `Refused` with its words; nothing to change is "Already as asked"; a user there is "Already as asked" only as an admin with every asked key, else "User Management's: not an admin, 1 key missing" (a key that does not parse is missing; `Refused` where the users cannot be read).
- **Decision 5 (a)**: where Enable and root login off both plan changes (ufw installed and off), the engine refuses the two as one plan (`ErrConflict`), so the firewall is planned and root login off is `Later`, "After the firewall: run Run All again".
- **The one plan** (`combine`): the planned rows' controls in one `Engine.Plan`; its first refusal, worded, is `Reading.Refusal`, the plan kept. Decision 5 (a) leaves no gallery host a refusal there; the path stays, a guard for a conflict the rule does not cover. The engine orders it (accounts, firewall, files, reload) and sets its risk. `Read` never applies and never calls `engine.New`: every engine is the caller's (`NewEngine`).

## 2. API

```go
type Input struct {
	System    host.System
	Facts     detect.Facts
	Config    *config.Config        // hardn.yml as loaded
	Session   revert.Session        // the applying session, as the engine's
	Getenv    func(string) string   // os.Getenv in hardn; nil is empty
	Revert    revert.Options        // the engine's: the state read, Auto Updates' record; zero in hardn
	Version   string                // hardn's, in Auto Updates' record
	NewEngine func(progress func(engine.Event)) *engine.Engine // as the screens'
}
func Read(ctx context.Context, in Input) (Reading, error) // error: ctx's, the journal's, detection's; fs.ErrInvalid without System, Config, NewEngine

type Reading struct {
	Config  *config.Config
	Facts   detect.Facts
	Reports []engine.Report // each row's check, in Rows' order: the states the main menu maps
	Rows    []Row
	Busy    bool            // revert.State.Busy, read before any plan
	Plan    *engine.Plan    // nil where nothing is planned or a change is pending
	Refusal string          // refuses Apply all before asking: a change journalled's words, or the plan's refusal
	Err     error
}
func (Reading) Planned() []Row; Refused() []Row; Pending() bool; Lockout() bool
func (Reading) Controls() []engine.Control; Row(id string) (Row, bool)

type Row struct {
	ID, Title string
	Report    engine.Report
	Kind      Kind   // Planned, AsAsked, NothingAsked, Left, NotApplicable, Refused, Pending, Elsewhere, Later
	Words     string // the outcome: the change, or why none
	Refusal   string // Refused and Pending: why, in full
	Err       error
	Control   engine.Control // Planned only
}

type Ask struct { Control engine.Control; Words string; Port uint16 }
func Asked(in Input, id string) Ask
func IDs() []string
func Checked(cfg *config.Config, getenv func(string) string) []engine.Control
const Title, Question, WordsAsked, WordsNothing, WordsStatus, WordsUnchanged, WordsRefused, WordsPending, WordsAfterFirewall, PendingWords
```

## 3. For units (c) and (d)

- Build `Input` as the screens build their options; `NewEngine` as `cmd/hardn`'s `engineOptions` (the backups hardn.yml's). Read again after each apply: a lockout-risk plan not yet confirmed reads as `Pending`.
- Draw each row's label from `output.StatusViewOf(Reading.Reports, …)` (decision 10 (a)'s rows, the main menu's labels); a `Refused` row as "Refused:" with `Refusal` cut to the row; before the review, "<label> skipped: <Refusal>" for each of `Refused()`; nothing planned and `Refusal` set, say it and go back.
- Apply `Reading.Controls()` with `Reading.Config` through `screenkit.Planning`, the hostname typed whatever the risk (decision 6 (a); the mock's `raTyped`), confirm-or-revert where `Lockout()`.

## 4. Made here

- `Input` is a struct, not the brief's seven arguments: the state read and Auto Updates' record need the revert options, and the record needs hardn's version, which the mock took from the gallery's globals.
- A refusal's words are a copy of `screenkit.RefusalWords` (the engine may not import `pkg/screens`; `.golangci.yml`'s `runall-through-host`), held equal by `TestWords`.
- A change journalled is worded `PendingWords` only where an applied lockout-risk change waits for `hardn confirm` (`revert.State.Waiting`); otherwise revert's own words, which name its `hardn rollback <id>`. The same rule is in SSH Login's `planWords`. A change journalled after the state read is the engine's refusal: that row is `Refused` in those words.
- Each asked control is planned alone and then together, so `Read` reads the host more than once (each control's reads once per plan, sshd's and the accounts' among them). Counted on `hostfake` with the checks: about 670 host calls on the debian13 and new-admin hosts against 160 to 200 for their one plan alone, 3 to 4 times; all read-only and local, for words that name the control that refuses.
- The checks run on the caller's engine (the mock's had its detection fixed to the facts).
- Decision 5 (a) is new behaviour: the mock's `ufw-inactive` drew the engine's refusal; here it plans the firewall alone.

## 5. Tests

`hostfake`, the gallery's hosts copied through `pkg/screens/ssh`'s fake, with ufw inactive, dhcpcd's resolv.conf and Auto Updates' check added: every outcome kind and each row held to its control alone (`TestHosts`, 16 hosts), every gate of the mapping (`TestAsked`, table-driven; `sshPort` 65536, 70000 and -1 out of range), the port row with sshd unread and the session reaching its read (`TestPortRow`), the one plan's refusal with decision 5's rule not applied (`TestCombinedRefused`, through `Combine`, `export_test.go`), Auto Updates planned on Alpine, apk-cron to install (`TestAutoUpdatesAlpine`), each journalled state's words (`TestWords`), the pending read and the read after an unconfirmed apply (`TestPending`), decision 5 and its premise (`TestAfterFirewall`), the plan's order and risk and no hardn.yml save (`TestCombined`), nothing written (`TestWritesNothing`), the user there (`TestUserThere`: also a system account's name, a key that does not parse, the users read failing).

## 6. Not run

No VM, no hardn binary. Auto Updates' apt plan is not modelled here (its apt model is not copied): on Debian its row is exercised refused (apt's syntax error) and Not Applicable (Proxmox VE); planned only on Alpine (apk-cron's install, its simulation and crond's status answered). No gallery host refuses the one plan once decision 5 applies: the refusal is exercised only with the rule undone (`TestCombinedRefused`).
