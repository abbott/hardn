# The SSH Login screen on the SSH Login control (`pkg/screens/ssh`, `screen.SSH`)

> **Unit:** refactor plan slice 6, unit (c), `next/s6-screen`, from `aca6a977`. **Status:** built and tested on macOS, in process: the screen through `Run` on `host_test.go`'s fake (the gallery's SSH Login hosts, its `sshSim` and engine fake copied), the control's plans on the real engine. No real terminal, no hardn binary, no VM. Nothing is wired in: main menu 2 is unit (d)'s.

## What it does

Main menu 2 as the maintainer adopted its mocks on 2026-11-03 ([gallery-ssh.md](gallery-ssh.md) §3 and §5, "Adopt all, start (b)", §6's words), drawn from `sshchange.Read` and the SSH Login, SSH Auth and SSH Port rows (each check through `output.StatusViewOf` and `risk.Assess`, as the gallery mapped them; no engine), writing nothing, under plan only too. Never from hardn.yml.

- **The screen:** the three rows; SSH Configuration (Root SSH Login, Password Login, SSH Port, Allowed Users, Match Block, Old Settings, Logged in as, each value's deciding file its note). Items by their result (P-12): 1 Disallow root login, Allow root key-only where root login is disallowed, Root key-only on Proxmox VE; 2 Key-only login, or Password login on where passwords don't log in; 3 Change SSH port; 4 Move old settings, only with hardn's old files. Every description "Can't read" where sshd -T is not read.
- **Refusals** are the control's or the engine's, said before asking: each item is planned by the engine first (`NewEngine`, the gallery's `preplan`), its first refusal said in `screenkit.RefusalWords`, a change waiting for hardn confirm in §6's words. Change SSH port plans the port sshd has, before its field, so that every refusal of any port (Proxmox VE, ufw's source, old files, sshd not read, an Include not first) is said before the field.
- **Each change** is the control's plan (`sshchange.Root`, `Auth`, `Port`, `Migrate`) through `screenkit.Session.Change`, planned again by the engine from a fresh reading: a change whose reading moved since the draw plans nothing or is refused, never applied to the old picture. The review, the plan's findings, the hostname typed (every SSH change is lockout-risk), confirm or revert as the engine arms it. A live change whose hostname can't be read says so before asking; plan only needs none.
- **The port form:** "New SSH port", refused at the field outside 1 to 65535 and, as a warning, on a port sshd listens on; its default hardn.yml's `sshPort` where set and sshd does not listen on it (decision 9 (a)), said in its description; a nil `Config` has none.
- **A reading that fails** is a row (Can't read) and every item's refusal, in the control's words: never an empty screen.
- **0** returns (nil), **q** quits (`nav.ErrQuit`), the end of the input and Ctrl-C abort (`prompt.ErrAborted`), from the screen, each question and the form. After a change applies, the screen is drawn again from a fresh reading.

## API

```go
ssh.Run(ctx, ssh.Options{
	Prompter, System, Facts /* detect's */, Session /* revert.ReadSession's */,
	Getenv /* os.Getenv: SUDO_USER, DOAS_USER */, Config /* hardn.yml: sshPort, sshConfigFile; nil for none */,
	NewEngine, ApplyContext, DryRun, LogPath, Now, Ran,
})
```

Required: the Prompter's In and Out, System, Getenv, NewEngine; else an error wrapping `fs.ErrInvalid`, nothing written. `NewEngine` is called once per item chosen (the plan before asking) and once per change; drawing calls it never. No change saves hardn.yml. `pkg/ui/screen/ssh.go` draws only: `SSH`, `SSHRoot`, `SSHChange` and its makers, the item keys, the section's keys and the port form's words; `rows.go` maps the control's reading to the rows.

## For unit (d)

- **Main menu 2** calls `Run`, with `os.Getenv`, `revert.ReadSession`'s session (the one the engine holds), and the loaded hardn.yml; under plan only too.
- **`-d`** plans `sshchange.Root{Want: "no"}`, refused under the same rules, typed on a terminal and refused off one, as `-w`; **`-c -d`** comes back, its guard `-d`'s (decision 8 (a)).
- **hardn.yml's SSH keys** (decision 9 (a)): `sshPort` is the form's default, `sshConfigFile` `Migrate`'s alone; `permitRootLogin`, `sshAllowedUsers`, `sshListenAddress`, `disableRootSSH`, `sshKeyPath` unused, one load warning where any is set.
- **Deleted** (hard cutover, the maintainer, 2026-11-03): the legacy screen `pkg/menu/disable_root_menu.go` and `-d`'s legacy path, with no freeze and no parity record.

## Decisions

- Every refusal is a control's or the engine's plan; none is decided here. The engine's pending and in-use refusals are said in the screen's words (`planWords`), the rest as the control words them.
- Items are planned by the engine, not the control alone, before asking: the pending change is the engine's refusal (`revert.ErrPending`), which a control's `Plan` never makes.
- The rows' assessment is `risk.Assess` over the three reports with a zero header: an Unknown row's "Counted as not configured" as the main menu draws it; the header's factors belong to other rows.
- The port's review holds the ports sshd listens on now, as `steps.PlanSSH` holds them (`reload.HoldPorts`): the mock's "22 held until confirmed" was 22 on every host.
- Item 3's change is typed whatever the port: no plan before its field can say its risk, and `PlanSSH` makes every port change lockout-risk.
- A failed reading's rows are the three Can't read rows, as the gallery drew them.

## Words

Decided and used exactly: gallery-ssh.md §6's, the form's two refusals and the pending refusal among them. The adopted mock's, not in §6, kept: the questions ("Disallow root login?", "Allow root to log in by key?", "Make root key-only?", "Turn password login off?", "Turn password login on?", "Change the SSH port?", "Move hardn's old settings?"), "<ports> held until confirmed", "<files>, into 00-hardn.conf", "Now not read", "A port the change adds is in use. Pick another." Proposed here: the port field's "Default: <port>", as Create User's (screen-users.md). The gallery's unused "Port %d is in use. Pick another." and "Nothing to move." are not carried: an empty plan says "Nothing needs changing." through the flow.

## Not run

Nothing on Linux, as root, or on a terminal: sshd's `-T -C`, its log, ss, ufw, systemctl and OpenRC are the fake's answers, as gallery-ssh.md §8 says of the mock.
