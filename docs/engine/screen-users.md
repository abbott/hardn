# The User Management screen on the User control (`pkg/screens/users`, `screen.Users`)

> **Unit:** refactor plan slice 5, unit (c), `next/s5-screen`, from `9ac4bfa0`. **Status:** built and tested on macOS, in process: the screen through `Run` on `host_test.go`'s fake (the gallery's users hosts, its `userSim` copied), the control's plans on the real engine. No real terminal, no hardn binary, no VM. *Wired in `next/s5-replace` (unit (d)): main menu 1 runs `Run`, and `-c` plans the control's Create ([cmd-engine.md](cmd-engine.md) §2.14, §2.15); the gallery's `play users` runs `Run` too.*

## What it does

Main menu 1 as the maintainer adopted its mocks on 2026-11-03 ([gallery-users.md](gallery-users.md) §3 and §5, "Adopt all, start (b)", §6's words), drawn from `users.Read` and the User row (`Reading.Status` through `output.StatusViewOf`, no engine), writing nothing, under plan only too. Never from hardn.yml.

- **The users' screen:** the User row; a section per user (Sudo or doas, Granted by, SSH Keys, Password); with none, "No user besides root". Item 1, Manage User, only with a user; item 2, Create User, always.
- **Manage User:** the user list, then the user's screen: Grant or Remove admin rights; for an admin, Ask for password or Don't ask password; 3, SSH Keys, always. SSH Keys lists the keys by fingerprint: Add, and with a key Remove.
- **Refusals** are the control's, said before asking: `Reading.Refusal` for every item, `ToolRefusal`, `KeysRefusal`, `CheckName`, `CheckNew` and `ParseKey` at Create's fields, `CheckKey` at Add's key (a key the user has already: a warning, as the mock drew it), each in `screenkit.RefusalWords`. Remove SSH key's Select refuses a key whose own plan refuses (another file sshd reads holds it), planned again at the pick. A key drawn that has left `~/.ssh/authorized_keys` since (`USERS_KEY_GONE`) is said before asking, and the keys are drawn again from a fresh reading; gone between the pick and the engine's plan, the flow reports the refused plan in the same words.
- **Each change** is `users.Admin`'s plan through `screenkit.Session.Change`, from a fresh reading: a user by name, a key by fingerprint. The review, the plan's findings, y/N, or the hostname typed where the plan is (`p.Typed()`), confirm or revert as the engine arms it. The admin actions are planned once before asking, for their refusal and their risk (the plan's error is `Reading.Refusal`'s, said before asking); Grant's review row says the grant the control plans (`User.NewGrant`); Remove SSH key plans each key's removal. A lockout-risk plan, live, whose hostname can't be read says so before asking; one that turns lockout-risk after says so in place of the question. Plan only stops every item at its plan and needs no hostname.
- **Install shadow first:** on Alpine where `useradd` is missing (the control's `UseraddRefusal`, looked up as the screen is drawn and again at the choice), item 2 reads "Create User", "Installs shadow first". Chosen, the install is planned first (`ActionInstallShadow`), its refusal said before asking; then its review, the notice saying why above Package shadow, and "Install shadow?" (y/N). Yes installs it; the host is read again and Create's own form and plan follow. No, plan only, or `useradd` still missing ends there, the screen drawn again. As Firewall's Enable firewall installs iproute2 first ([screen-firewall.md](screen-firewall.md)); no hostname is asked: the install is a plain y/N.
- **Create's form:** Username; Grant admin rights?; Require password for admin rights?, asked after yes and refused at yes (decision 3); Add SSH key? and the key. Defaults from `Config`'s `username` and first `sshKeys` alone (decision 7); a nil `Config` has none. Grant admin rights? defaults to no, as the mocks drew it.
- **A reading that fails** is said where the users would be, in the control's words, and Create says it before asking; a user no longer listed says so, Return alone.
- **0** returns (nil), **q** quits (`nav.ErrQuit`), the end of the input and Ctrl-C abort (`prompt.ErrAborted`), from every screen, question and the form. After a change applies, the screen is drawn again from a fresh reading.

## API

```go
users.Run(ctx, users.Options{
	Prompter, System, Facts /* detect's */, Getenv /* os.Getenv: SUDO_USER, DOAS_USER */,
	Config /* hardn.yml, Create's defaults; nil for none */,
	NewEngine, ApplyContext, DryRun, LogPath, Now, Ran,
})
```

Required: the Prompter's In and Out, System, Getenv, NewEngine; else an error wrapping `fs.ErrInvalid`, nothing written. No change saves hardn.yml. `pkg/ui/screen/users.go` draws only: `Users`, `UsersSelect`, `UsersManage`, `UsersKeys`, `UsersChange` and its makers, `UsersKeysRow`, the item keys and the form's titles; `rows.go` maps the control's types to its rows.

## Built in `next/s5-replace` (unit (d))

- **Main menu 1** runs `Run` (`MainMenu.SetUsers`, `cmd/hardn/users.go`), with `os.Getenv` and the loaded hardn.yml; under plan only too, every change stopping at its plan.
- **`-c [-u <name>]`** plans Create with admin rights, no password and every key in hardn.yml's `sshKeys` (the maintainer, 2026-11-03: "(a) Add sshKeys", in place of decision 6 (a)'s no key), applied without a question as `-g` is (decision 6 (a)); `-n` or `dryRun: true` shows the plan; refusals exit 1. It no longer configures sshd (slice 6's). The screen's Create still takes one key, `sshKeys`' first its default.
- **hardn.yml:** `username` and `sshKeys` are Create's defaults alone and never warn; `sudoNoPassword: false` warns once (decision 7 (a)). Run All still reads it until slice 8.
- **Deleted** (hard cutover, the maintainer, 2026-11-03): the legacy User Management screen, what only it called (`UserManager.CreateUser` stays for `-r` and Run All), Run All's offer to set a username, and the `pkg/characterization` record (`menu_users_test.go`, 42 goldens; `create_user_c_path`). `configure_sudo` stays, reached by `-r`.
- **Not a bisect point for main menu 1:** `0162f1b0` built alone panics when item 1 is chosen, as nothing calls `SetUsers` until `8d0e8c74`.

## Decisions

- Every refusal is a control call; none is decided here.
- Create's "Grant admin rights?" stays [y/N], as the mocks drew it (the maintainer, 2026-11-03): hardn.yml's `sudoNoPassword` is about a password for admin rights, and a new user has none to ask (decision 3), so the form never uses it; unit (d) warns where it is set to false (decision 7).
- A key the user has already is refused by the control (`USERS_KEY_THERE`), at the field; a key gone since the draw too (`USERS_KEY_GONE`), never `FILE_NOT_FOUND`.
- Add SSH key for a locked user: the plan's finding says it, the mock's review notice is dropped (no repeats).
- The admin actions are planned before asking (a read), so that their refusal is said and their risk known before anything is asked; the flow's plan is made again by the engine.
- `Getenv` is required: a nil one would read every run as a root login, with that login's lockout rules.
- A failed reading's line is the control's words, muted, under "Users" (or the sub-screen's section).
- **Install shadow, 2026-11-11 (the maintainer):** Create User on the screen offers to install shadow on Alpine where useradd is missing; `-c`, `-u` and Run All keep the refusal; plus the combined shadow-and-doas refusal ([control-users.md](control-users.md) §1, §5).

## Words proposed

- Create's defaults: "Default: <username>" and "Default: hardn.yml's key".
- `USERS_KEY_GONE`: "That key isn't in <file> any more. Nothing changed." (`USERS_KEY_THERE`'s are the mock's `usrWordsDupKey`, decided.)
- Unit (d)'s: "hardn.yml's sudoNoPassword: false isn't used by User Management or -c"; `-c`'s refusal of a lockout-risk plan, "Creating this user here risks a lockout", "create it from main menu 1, User Management, which asks you to confirm" (on `-e`'s pattern).
- Install shadow (approved by the maintainer, 2026-11-11): item 2's description "Installs shadow first"; the review's notice "hardn needs useradd, from shadow, to create users.", its row "Package" "shadow", under "User Configuration" and Create User's title bar; the question "Install shadow?". Tested: `TestCreateInstallsShadow`, `TestCreateShadowDeclined`, `TestCreateShadowRefused`, `TestShadowGoldens` (`testdata/golden/screen/no-shadow.golden`, `no-shadow-review.golden`).
- The mock's questions and titles, kept, are not in §6: "Grant <user> admin rights?", "Remove <user>'s admin rights?", "Ask <user>'s password for admin rights?", "Stop asking <user>'s password for admin rights?", "Add the key for <user>?", "Remove the key from <user>?", "Create user '<user>'?".

## Not run

Nothing on Linux, as root, or on a terminal: the plans' commands (`useradd`, `userdel --remove`, `install -d`, `visudo -cf`, `doas -C`) are the fake's answers, as control-users.md §6 says of the control.
