# The SSH Login screen on the SSH Login control (`pkg/screens/ssh`, `screen.SSH`)

> **Unit:** refactor plan slice 6, unit (c), `next/s6-screen`, from `aca6a977`. **Status:** built and tested on macOS, in process: the screen through `Run` on `host_test.go`'s fake (the gallery's SSH Login hosts, its `sshSim` and engine fake copied), the control's plans on the real engine. No real terminal, no hardn binary, no VM. *Wired in `next/s6-replace` (unit (d)): main menu 2 runs `Run`, and `-d` plans the control's Disallow root login ([cmd-engine.md](cmd-engine.md) §2.16, §2.17).*

## What it does

Main menu 2 as the maintainer adopted its mocks on 2026-11-03 ([gallery-ssh.md](gallery-ssh.md) §3 and §5, "Adopt all, start (b)", §6's words), drawn from `sshchange.Read` and the SSH Login, SSH Auth and SSH Port rows (each check through `output.StatusViewOf` and `risk.Assess`, as the gallery mapped them; no engine), writing nothing, under plan only too. Never from hardn.yml.

- **The screen:** the three rows; SSH Configuration (Root SSH Login, Password Login, SSH Port, Allowed Users, Match Block, Old Settings, Logged in as, each value's deciding file its note). Items by their result (P-12): 1 Disallow root login, Allow root key-only where root login is disallowed, Root key-only on Proxmox VE; 2 Key-only login, or Password login on where passwords don't log in; 3 Change SSH port; 4 Move old settings, only with hardn's old files; *since 2026-11-08*, 5 Upgrade openssh, "Fix CVE-2026-123158", only on Alpine where sshd lacks the fix (`sshchange.Upgrade`, [alpine.md](../safety/alpine.md) §7.1), a plain y/N, its review row the server package, "10.2_p1-r0, to 10.2_p1-r1 or later" (proposed; `TestUpgradeItem`). Every description "Can't read" where sshd -T is not read, but 5's, which reads no sshd setting. With item 5 the alert stands between the rows and the section (the maintainer's drawing and words), "Warning: CVE-2026-123158" over "Select option: 5) Upgrade openssh", the key and label in the menu's styles (`SubMenu.Alert`, DEV-075; `TestSSHAlert`, golden `screen/alpine-unfixed`): SSH Auth with no line of the CVE's, as on the main menu (`output.AlertOf`; *since 2026-11-09*, the maintainer): 27 lines at 80 columns, so at 80×24 the title bar, its blank line and the SSH Login row scroll off.
- **Refusals** are the control's or the engine's, said before asking: each item is planned by the engine first (`NewEngine`, the gallery's `preplan`), its first refusal said in `screenkit.RefusalWords`, a change waiting for hardn confirm in §6's words. Change SSH port reads the host again when chosen and plans the port sshd has, before its field, so that every refusal of any port (Proxmox VE, ufw's source, old files, sshd not read, an Include not first) is said before the field.
- **Each change** is the control's plan (`sshchange.Root`, `Auth`, `Port`, `Migrate`) through `screenkit.Session.Change`, planned again by the engine from a fresh reading: a change whose reading moved since the draw plans nothing or is refused, never applied to the old picture. The review, the plan's findings, the hostname typed (every SSH change is lockout-risk), confirm or revert as the engine arms it. A live change whose hostname can't be read says so before asking; plan only needs none.
- **The port form:** "New SSH port", refused at the field outside 1 to 65535; then the port typed is planned by the engine (`sshchange.Port{To: n}`, as `preplan`), its refusal said at the field in `planWords` (in use, a change pending, ufw's source, Proxmox VE, old files, not read), so "Pick another" returns to the field; an empty plan, a port sshd has, is the warning "sshd listens on <port> already." Its default hardn.yml's `sshPort` where set and sshd does not listen on it (decision 9 (a)), said in its description; a nil `Config` has none. The ports and the default are from item 3's own reading, not the draw's. The change then plans again, fresh, under the run lock.
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

Required: the Prompter's In and Out, System, Getenv, NewEngine; else an error wrapping `fs.ErrInvalid`, nothing written. `NewEngine` is called once per item chosen (the plan before asking), once per port the port field checks, and once per change; drawing calls it never. No change saves hardn.yml. `pkg/ui/screen/ssh.go` draws only: `SSH`, `SSHRoot`, `SSHChange` and its makers, the item keys, the section's keys and the port form's words; `rows.go` maps the control's reading to the rows.

## Built in `next/s6-replace` (unit (d))

- **Main menu 2** runs `Run` (`MainMenu.SetSSH`, `cmd/hardn/ssh.go`), with `os.Getenv`, the session `engineOptions` holds, and the loaded hardn.yml; under plan only too, every change stopping at its plan. Its label and description are unchanged.
- **`-d`** plans `sshchange.Root{Want: "no"}` through `runControls`, under the control's refusals (exit 1, nothing written). On a terminal: the plan, "Disallow root login?", the hostname typed, confirm or revert. Off one: refused, `PLAN_REFUSED`. `-n` or `dryRun: true`: the plan alone, 3 with changes; no longer refused. Root login already off, on Proxmox VE as elsewhere, or Proxmox VE's root already key-only, is nothing to change, 0: `-d` never loosens root login (Proxmox VE's key-only is the screen's Root key-only item).
- **`-c -d`** is back, `-c` first, so that `-d`'s proof sees the user `-c` created (decision 8 (a)); `hardncmd.RefuseCreateDisableRoot` is gone.
- **hardn.yml** (decision 9 (a)): `sshPort` is the port form's default, `sshConfigFile` Move old settings'. `permitRootLogin`, `sshAllowedUsers`, `sshListenAddress`, `disableRootSSH` and `sshKeyPath` set to a value other than the default warn once each. Run All and `-r` still read `sshAllowedUsers` and `sshListenAddress` until slice 8.
- **Deleted** (hard cutover, the maintainer, 2026-11-03): the legacy SSH Login screen (`pkg/menu/disable_root_menu.go`), `-d`'s legacy path and the `DisableRootSSH` chain with origin/main's stub rewrite (P0.8 gate 2), `pkg/characterization`'s `disable_root_ssh` case and Proxmox VE's `hardn -d` record. No freeze, no parity record.

## Decisions

- Every refusal is a control's or the engine's plan; none is decided here. The engine's pending and in-use refusals are said in the screen's words (`planWords`), the rest as the control words them.
- Items are planned by the engine, not the control alone, before asking: the pending change is the engine's refusal (`revert.ErrPending`), which a control's `Plan` never makes.
- The rows' assessment is `risk.Assess` over the three reports with a zero header: an Unknown row's "Counted as not configured" as the main menu draws it; the header's factors belong to other rows.
- The port's review holds the ports sshd listens on now, as `steps.PlanSSH` holds them (`reload.HoldPorts`): the mock's "22 held until confirmed" was 22 on every host.
- The pending refusal of a port comes at its field, not before it: the engine reads its journal only for a plan with changes, and the port sshd has plans none. On the confirm-window host the port is so refused at the field, where it was refused after it, at the change's plan.
- The facts are detected once per run by the caller, and the engine detects the same host: a host does not become Proxmox VE during a session, and the plan shows the real change.
- The three rows are the main menu's, from the checks, as User Management's User row is: a read apart from the section's, so the two may differ for a moment if sshd changes between them.
- Item 3's change is typed whatever the port: no plan before its field can say its risk, and `PlanSSH` makes every port change lockout-risk.
- A failed reading's rows are the three Can't read rows, as the gallery drew them.

## Words

Decided and used exactly: gallery-ssh.md §6's, the form's two refusals and the pending refusal among them. The adopted mock's, not in §6, kept: the questions ("Disallow root login?", "Allow root to log in by key?", "Make root key-only?", "Turn password login off?", "Turn password login on?", "Change the SSH port?", "Move hardn's old settings?"), "<ports> held until confirmed", "<files>, into 00-hardn.conf", "Now not read", "A port the change adds is in use. Pick another." Proposed here: the port field's "Default: <port>", as Create User's (screen-users.md). Unit (d)'s, proposed: `-d` off a terminal, "Turning root login off here risks a lockout", "run hardn -d from a terminal, which asks you to confirm" (on `-w`'s pattern); the load warning "hardn.yml's <key> isn't used by SSH Login or -d". Approved by the maintainer, 2026-11-11 (`next/alpine-lxc-ssh`): the host-key refusal (`steps.NoHostKeys`, `SSHD_ENV_NO_HOSTKEYS`), said by items 1 to 4, `-d` and Run All's SSH parts where sshd loads no host key: "sshd has no host keys, so hardn can't read its settings." then, where sshd runs or is set to start, "Run ssh-keygen -A, then try again."; where it is off, on Alpine "Run rc-service sshd start, or apk del <packages>, then try again." (the `/etc/apk/world` entries that keep sshd installed, such as "openssh openssh-server-pam"; without them "Run rc-service sshd start, then try again."), on Debian and Ubuntu "Run ssh-keygen -A and systemctl start ssh, or apt remove openssh-server, then try again.", on Rocky Linux, AlmaLinux and RHEL "Run ssh-keygen -A and systemctl start sshd, or dnf remove openssh-server, then try again.", on Proxmox VE "Run ssh-keygen -A and systemctl start ssh, then try again."; with the configuration's own `HostKey` lines, "sshd's HostKey files aren't there, so hardn can't read its settings. Fix the HostKey lines, then try again." Tested: `TestNoHostKeys` (Alpine and Debian, sshd off: the rows "sshd off, no host keys", items 1 to 3 refused before asking, one engine, nothing written). The gallery's unused "Port %d is in use. Pick another." and "Nothing to move." are not carried: an empty plan says "Nothing needs changing." through the flow.

## Not run

Nothing on Linux, as root, or on a terminal: sshd's `-T -C`, its log, ss, ufw, systemctl and OpenRC are the fake's answers, as gallery-ssh.md §8 says of the mock.
