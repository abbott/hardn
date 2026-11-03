# The User Management screen on the User control (`pkg/screens/users`, `screen.Users`)

> **Unit:** refactor plan slice 5, unit (c), `next/s5-screen`, from `9ac4bfa0`. **Status:** built and tested on macOS, in process: the screen through `Run` on `host_test.go`'s fake (the gallery's users hosts, its `userSim` copied), the control's plans on the real engine. No real terminal, no hardn binary, no VM. Nothing is wired into `cmd/hardn` or `pkg/menu` (unit (d)); the gallery still draws its own copy (the next stage).

## What it does

Main menu 1 as the maintainer adopted its mocks on 2026-11-03 ([gallery-users.md](gallery-users.md) §3 and §5, "Adopt all, start (b)", §6's words), drawn from `users.Read` and the User row (`Reading.Status` through `output.StatusViewOf`, no engine), writing nothing, under plan only too. Never from hardn.yml.

- **The users' screen:** the User row; a section per user (Sudo or doas, Granted by, SSH Keys, Password); with none, "No user besides root". Item 1, Manage User, only with a user; item 2, Create User, always.
- **Manage User:** Select a user, then the user's screen: Grant or Remove admin rights; for an admin, Ask for password or Don't ask password; 3, SSH Keys, always. SSH Keys lists the keys by fingerprint: Add, and with a key Remove.
- **Refusals** are the control's, said before asking: `Reading.Refusal` for every item, `ToolRefusal`, `KeysRefusal`, `CheckName`, `CheckNew` and `ParseKey` at Create's fields and the key's, each in `screenkit.RefusalWords`. Remove SSH key's Select refuses a key whose own plan refuses (another file sshd reads holds it).
- **Each change** is `users.Admin`'s plan through `screenkit.Session.Change`, from a fresh reading: a user by name, a key by fingerprint. The review, the plan's findings, y/N, or the hostname typed where the plan is (`p.Typed()`), confirm or revert as the engine arms it. The admin actions are planned once before asking, for their refusal and their risk; Remove SSH key plans each key's removal. A lockout-risk plan, live, whose hostname can't be read says so before asking; one that turns lockout-risk after says so in place of the question. Plan only stops every item at its plan and needs no hostname.
- **Create's form:** Username; Grant admin rights?; Require password for admin rights?, asked after yes and refused at yes (decision 3); Add SSH key? and the key. Defaults from `Config`'s `username`, `sudoNoPassword` and first `sshKeys` alone (decision 7); a nil `Config` has none.
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

## For unit (d)

- Main menu 1 calls `Run`, with `os.Getenv` and the loaded hardn.yml.
- `-c` and `-u` move to the control, applied as `-g` is (decision 6 (a)): `-u <name>` plans Create with admin rights and no key, under the same refusals.
- hardn.yml's `username`, `sudoNoPassword` and `sshKeys` set and unused: one warning (decision 7 (a)).
- The legacy User Management screen and its `pkg/characterization` record are deleted, not frozen (the maintainer, 2026-11-03: hard cutover).
- The gallery's stage: `internal/tools/uigallery/users.go`'s rows, menus, forms and runner retire; its listing and play call `Run` on its hosts; its `users_test.go` checks that moved here go.

## Decisions

- Every refusal is a control call; none is decided here.
- Add SSH key for a locked user: the plan's finding says it, the mock's review notice is dropped (no repeats).
- The admin actions are planned before asking (a read), so that their refusal is said and their risk known before anything is asked; the flow's plan is made again by the engine.
- `Getenv` is required: a nil one would read every run as a root login, with that login's lockout rules.
- A failed reading's line is the control's words, muted, under "Users" (or the sub-screen's section).

## Words proposed

- Create's defaults: "Default: <username>" and "Default: hardn.yml's key".
- The mock's questions and titles, kept, are not in §6: "Grant <user> admin rights?", "Remove <user>'s admin rights?", "Ask <user>'s password for admin rights?", "Stop asking <user>'s password for admin rights?", "Add the key for <user>?", "Remove the key from <user>?", "Create user '<user>'?".

## Not run

Nothing on Linux, as root, or on a terminal: the plans' commands (`useradd`, `userdel --remove`, `install -d`, `visudo -cf`, `doas -C`) are the fake's answers, as control-users.md §6 says of the control.
