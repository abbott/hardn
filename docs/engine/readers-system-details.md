# The readers of System Details and Logs (`pkg/engine/details`, `pkg/engine/logview`, `host.System`'s `ScanLines` and `RunInteractive`, `pkg/output`'s `details`)

> **Unit:** refactor plan slice 1 (System Details + Logs, both read only), unit (b), `next/s1-readers`, from `next/s1-gallery` at 7bc756c7. It builds on unit (a) ([gallery-system-details.md](gallery-system-details.md)), whose eleven decisions the maintainer adopted on 2026-10-30 ("Adopt all eleven"). Decisions implemented here: 1 (a)'s document (the `details` field, no flag), 2 (a) (the seven sections), 3 (a) (Last Login, its blank guard), 6 (a) (the log's file), 7 (a) (GNU `less` alone), and 8 (a)'s and 10 (a)'s needs (the last lines, the pipe form). The gallery moves onto both readers in this unit's last part. No V-number holds the defects it fixes (gallery-system-details.md §7: the slice's "Logs dump" and "Broken log parser", the reader's side of rows 21, 22 and 26, and rows 7 to 16 of the report).
> **Status:** built and tested on macOS, in process, on `hostfake` and on real files, real children and pseudo-terminals in `t.TempDir()` (run). Each form the report parses is typed from a VM's log with synthetic values, or labelled. **Nothing has run on Linux or as root**, and hardn itself has not run: §4. There is no control on the engine, no `Check`, no plan and no flow, as neither screen has a main-menu row or a change. Nothing is wired into `cmd/hardn`, `pkg/menu`, `pkg/system`, `pkg/cmd`, `pkg/logging`, `pkg/adapter` or `pkg/config`; no `--output` flag and no screen package here (units (c) and (d)). Goldens: `pkg/output/testdata/codes.golden` gains `DETAILS_UNREADABLE`'s line, as a registered code does; `json/details.json` and `details-unreadable.json` are new; nothing else (`git diff --name-only 7bc756c7 -- '*testdata*'`).

The screens of unit (c) and the wiring of unit (d) should only draw and wire. So the host gains two seams, `ScanLines` and `RunInteractive`; `pkg/engine/logview` reads hardn's log through them, and `pkg/engine/details` the report's seven sections, both as data, with nothing rendered, as `pkg/engine/header` reads the header's facts; and `pkg/output` gains the report's JSON form.

## 1. Why

origin/main prints the whole log at once, 20,000 of 20,000 lines, read whole into memory, its bytes raw, from `cfg.LogFile`, which hardn has never written (gallery-system-details.md §2.7, §2.8). Its parser stops silently at a line over 64 KiB and reads the clock as the level, and nothing live calls it (§2.10). Unit (a) drew the replacement on the gallery's stand-ins, `readLog`, `safeLine`, `parseLogLine` and `pagerFor` (§8), which read the whole file and go by a link to tell BusyBox's `less`. `host.System` had no call for the end of a file and none that gives a child the terminal: `Run` pipes output (`pkg/host/real.go`, `start`).

origin/main's report stops at its first collector's error and loses all it read (§2.3), asks `zfs` about a hard-coded dataset (§2.5), reads Last Login from a `lastlog` stock Debian 13 and Ubuntu 26.04 do not have (§2.5), and says "Bare Metal", "1 socket" and "0.00 GHz" for what it does not know. Unit (a) drew the replacement on `readDetails`, which groups read and any failure loses a section (§8).

## 2. Guarantees

### The two seams

[system.md](../safety/system.md) has each in full, "The line scan" and "The interactive child".

1. **`ScanLines(ctx, name, yield)`** serves the three later uses with one call: the screen's last lines and total (`Read`, item 7), `-p`'s pipe form, every line written as it reads (run: `TestEveryLineAsItReads`; unit (d) writes it), and "can this file be read and does it hold anything" for Last Login's blank guard (run: `TestHoldsAnything`, a 1 MiB binary file looked at for one line). Memory is bounded whatever the file holds (run: `TestScanReaderIsBounded`). It refuses anything not a regular file without blocking: a real FIFO with no writer and `/dev/null`, `fs.ErrInvalid`; a directory, `EISDIR` (run: `TestScanLinesRefusesWithoutBlocking`, `TestContract`). A line past `host.MaxLine` is cut, `Line.Cut` says so, and the read goes on, with no newline at the end, CRLF and empty lines each handled (run: `TestContract`, on `host.Real` and the fake alike). It honours ctx between reads (run: `TestScanLinesContextDuringTheRead`) and adds no run-log line (run: `TestScanLinesNoRunLog`).
2. **It does not follow a symlink at the path, and is guarded,** as `System`'s doc and `pkg/host/guard.go` have it: `ErrSymlink`, and a link on the way is followed only from a trusted directory (run: `TestContract`, "scan lines: guarded"). What it reads is shown to whoever asked, as root from the menu, so a link at `/var/log/hardn.log` must not show them `/etc/shadow`. A `/var/log/hardn.log` an admin linked elsewhere reads "Can't read it: refusing a symlink".
3. **`RunInteractive(ctx, env, name, args...)`** gives one named program hardn's stdin, stdout and stderr and waits for it. Exactly `env` and `Run`'s `PATH` reach it, nothing inherited (run: `TestRunInteractiveExactEnvironment`, `env(1)` as a real child), no shell, the name as `Run` resolves it (run: `TestRunInteractiveCommand`, `TestRunInteractiveRefuses`), the exit status as `Run` gives one (run: `TestRunInteractiveExit`). The fake records the call and runs nothing (run: hostfake's `TestRunInteractive`).
4. **Signals.** Read of today's handling: `signal.Notify` in `ReadLine` for its own call alone (`pkg/ui/prompt/reader.go:319`, `:348`), `signal.NotifyContext` around an apply and `hardn confirm`'s clean-up (`cmd/hardn/engine.go:45`, `cmd/hardn/confirm.go:42`), a `SIGHUP` watch around the same (`cmd/hardn/engine.go:78`), and `signal.Ignore` in the hold around an in-process rollback alone (`pkg/engine/hold/hold.go:117`); so outside those, Ctrl-C ends hardn by Go's default (`cmd/hardn/status.go`'s `readStatus`). What it does: while the child runs, SIGINT and SIGQUIT are caught on a channel nothing reads, as exec gives a caught signal its default action in the child and keeps an ignored one ignored; one hardn started with ignored, or that the hold ignores, stays ignored; `signal.Stop` puts each back, another registration untouched (run, on a pseudo-terminal, typing at its master: `TestRunInteractiveCtrlC`, `TestRunInteractiveCtrlBackslash`, `TestRunInteractiveKeepsRegistrations`). **What could not be tested here:** a person's real terminal, an ssh session, sudo or doas between hardn and the terminal, Linux, root; Ctrl-Z and a SIGTERM to hardn alone are read, not run (system.md, "Known limits").
5. **ctx:** done on entry runs nothing; done during the run sends SIGTERM, then SIGKILL after 5 s, an error wrapping `ctx.Err()` (run: `TestRunInteractiveContext`). A child that ends with the terminal's modes changed has them put back (run: `TestRunInteractiveTerminalModes`). The prompt reader needs nothing else of the seam: no read of its outlives its call (`pkg/ui/prompt/doc.go`, "No read outlives its call"), so nothing is reading the terminal while the child has it. Whether unit (c) calls `Flush` after the pager is unit (c)'s.

### The reader (`pkg/engine/logview`)

6. **The file** is `logview.File`, `/var/log/hardn.log`, always (decision 6 (a); run: `TestFile`, against `output.LegacyLogPath`). `Elsewhere(logFile)` returns the path, safe to show, where hardn.yml's `logFile` names another; the empty value and the same path, however spelled (`path.Clean`), are no difference (run: `TestElsewhere`). For "hardn.yml's logFile, \<path\>, isn't used" and unit (d)'s warning. It does not read hardn.yml: the caller has the configuration.
7. **`Read(ctx, sys)`** gives `Path`, `State`, `Size`, `Total` and `Last`, oldest first, at most `MaxLast` (256); the screen fits what it has room for from their end (run: `TestReadLong`, 785 lines). `State` is `Lines`, `NoLog`, `Empty` or `Unreadable`, the last with `Reason`, in the words after "Can't read it: " (gallery `reasonOf`), and `Err`, so that `-p` can tell `fs.ErrPermission` for decision 9 (a)'s "Run it with sudo." (run: `TestRead` on hostfake, 9 cases; `TestReadRealFiles` on real files, a FIFO, a directory, a symlink, mode 0000, a file on the way; each without blocking). A read that fails part way gives no `Total`. A done ctx is `Unreadable`, in ctx's words (run: `TestReadDoneContext`). One scan and nothing else, no run-log line, on a host with a run log (run: `TestReadWritesNothing`).
8. **`Safe`** (and `Show`, `Safe` with the cut's mark) is exported: `-p` and the screen both use it, `Read` already has for `Last`. What a terminal would act on is made safe and shown, not stripped: C0 as `^[`, DEL as `^?`, a tab as 4 spaces; C1 as `<U+009B>`; bidi overrides and isolates, what draws as nothing, blanks other than the space, as their code point; non-UTF-8 bytes as `<0xe9>`, each alone. Typographic characters are kept (run: `TestSafeKeepsTypography`). **Tried to break it** (run: `TestSafe`): an OSC with a BEL and with ST, a clipboard write, a link; a C1 CSI as U+009B and as the raw byte 0x9B; an invalid sequence next to an escape, before and after it; overlong encodings of ESC, CSI and `/`; a surrogate. **The property** (run: `TestSafeWhole`, every code point, every pair of bytes, on its own and beside an escape): the result is valid UTF-8, holds no control or format character, and `Safe` leaves it as it is.
9. **`ParseLine`** reads `DATE TIME LEVEL: message`, on `log.LstdFlags`' exact shape and logging's five levels, for the colour alone; anything else stays whole, with no level (run: `TestParseLine`, on the forms of `_support/hardn-audit-2026-10-16/vm-runs/s3/57-log-ubuntu26-vm.log`). A command's own "ERROR:" in a run-log line stays that line's INFO.
10. **`Pager(ctx, sys, term)`** returns the command or none (decision 7 (a)). `less` must be an executable file on `host.SearchPath` AND `less --version` must exit 0 with a first line `less <digits>` (`less 668 (GNU regular expressions)`; BusyBox prints `less: unrecognized option: version`, `vm-runs/s1/01-tools.log`). Anything else, any error, is no pager: it fails closed (run: `TestPager`, 21 host shapes; `TestPagerDoneContext`). It never reads `$PAGER` or any other variable naming a program.
11. **What would run**, exactly (run: `TestPager`, `TestCommandRun`):

    ```text
    argv: less -- /var/log/hardn.log
    env:  LESSSECURE=1 LESSHISTFILE=- TERM=<checked> LC_ALL=C.UTF-8 PATH=/usr/sbin:/usr/bin:/sbin:/bin
    ```

    - `LESSSECURE=1`: `!`, `|`, `E`, `:e` and `v` each say "Command not available" (run, as a user: `vm-runs/s1/04-less2-deb13-vm.log`, `04-less2-ubuntu26-vm.log`).
    - `LESSHISTFILE=-`: no history file, so what is typed at less's prompts is not left in a file as root. The secure mode may already stop it; this says so whatever that version does [I: less(1), not run].
    - `TERM`: from a value the caller passes, as it comes from whoever started a root process and names a file in the terminfo database. A letter or digit, then letters, digits, `-`, `_`, `+`, `.`, no `..`, at most 64 bytes; otherwise `vt100`, under which the VMs' `less` ran (run: `TestSafeTerm`; the VMs' `04-less2-*.log`).
    - `LC_ALL=C.UTF-8`, so a log's non-ASCII shows as itself [I until a VM runs it: built into glibc from 2.35; any name is UTF-8 to musl]. If the locale is missing, `less` shows bytes as `<C3><A9>`, which is safe.
    - `--` before the path: run on this Mac's `less` 581.2 (`TestRunInteractiveLess`, and a pipe by hand); written, not run on GNU `less` 668.
    - No `LESS`, `LESSOPEN`, `LESSCLOSE`, `LESSKEY`, `EDITOR`, `VISUAL`, `SHELL` or `HOME` gets through: `RunInteractive` passes nothing it is not given (run: `TestRunInteractiveExactEnvironment`, with each set in the parent).
12. **All on `host.System`, no line for a draw.** `Read` runs nothing. `Pager` runs `less --version`, a command, which a host with a run log logs (run: `TestPagerCheckIsACommand`): a draw reads on a host with none, the shape of `statusHost` (`cmd/hardn/status.go:29`).

### The report's reader (`pkg/engine/details`)

13. **`Read(ctx, sys, facts)`** gives a `Report` of seven sections, facts and states and no words: `OS`, `Host`, `Users`, `CPU`, `Disk`, `Memory`, `Logins`. A section that fails leaves the others, and a fact leaves its section's others: what could not be read is that section's `Unread`, by source and why (run: `TestOneSectionLeavesTheRest`, each source failing alone, the others whole). A done ctx leaves every fact with ctx's error (run: `TestDoneContext`). A zero fact is not established, and a slice is `nil` only where it was not read (`[]`, none).
14. **OS, kernel, uptime, hostname:** `header.Read` and `/proc/sys/kernel/osrelease`, on the caller's detect facts (run: `TestRead`).
15. **Addresses:** `ip -o addr show scope global`, `metric 100` (Debian, Ubuntu) and `noprefixroute` (Alpine) passed over alike, IPv4 and IPv6 ([I] for `inet6`: no VM has a global address) (run: `TestAddresses`, typed from `vm-runs/s1/01-tools.log`:50, :115, :178). A line in no form known is said once in `Unread` and the others stay.
16. **Users:** uid 1000 and up, not `nobody` or `nfsnobody`, each name once, as the users check takes the first (`getpwnam`'s [I]). What lets each administer is `users.Elevation`, `readTools` and `decide` exposed and not copied, so a rule such as cloud-init's counts and a group a rule does not name does not (run: `TestUsers`, `TestElevation`). `User.Admin()` gives the first tool that grants, or No, or Unknown, as the check's `elevates`. Root alone reads no rule and runs no `visudo` (run: `TestUsers`).
17. **As a user other than root** the report says so and does not guess. `Users.Root` is `/proc/self/status`'s effective uid, the second number of its `Uid:` line, read through `ReadFile`, so on the seam and with no new method on `System` ([I] for the form: no VM printed it). Unknown, with `RootErr`, where it cannot tell, which takes nothing else with it. `/etc/sudoers.d` is `drwxr-x---` on all three VMs and Alpine's `/etc/doas.conf` 0640 (`vm-runs/s1/01-tools.log`:65, :130, :191-193). The rules' read stops at the first refusal, and `NotRead()` holds every file of that tool's that refuses: `/etc/sudoers` and `/etc/sudoers.d`, or `/etc/doas.conf` and `/etc/doas.d` (run: `TestNotRoot`; `/etc/sudoers` 0440 is [I], no log lists it). Each user's grant is then Unknown, never a group's guess, and no `visudo` runs.
18. **CPU and load:** `processor` lines are logical CPUs; the first `model name`; distinct `physical id`s. Where those two are absent, as on arm64 [I, not run], `Model` is "" and `Sockets` 0, and none is made up. No `processor` line at all is not a form known. `/proc/loadavg`'s form is proc(5)'s; no VM printed it [I] (run: `TestCPU`).
19. **Disk:** `/proc/self/mounts`' last line for `/`, through `detect.ParseMounts`, then `df -kP /`, header passed over (no VM's log kept it [I]), figures in KiB (run: `TestDF`, on `01-tools.log`:51-52, :116-117, :179-180). **On ZFS, every form [unverified], no ZFS VM** (run: `TestZFS`): the dataset is the mount's, never one of hardn's; `zpool list -H -o health <pool>`; `zfs get -Hp -o value used,available <dataset>`, total their sum. No `zpool`, `Health` "" beside its `Pool`, so not known, and not a finding. No `zfs`, `df` gives the figures. One that fails is in `Unread`, and `df` still answers.
20. **Memory:** `MemTotal` and `MemAvailable`. No `MemAvailable` is `AvailableKnown` false, and `Used()` says not known (run: `TestMemory`). `Used` never counts the kernel's caches, as origin/main's does.
21. **Last Login (decision 3 (a)):** one call, `lslogins --time-format=iso --noheadings --raw --output=USER,LAST-LOGIN,LAST-TTY,LAST-HOSTNAME -l root,<each listed user>`, its lines keyed by name, in the order asked, split at single spaces (run: `TestLogins`, on `02-login.log`, `08-lslogins.log`). A name with no line is that account's `LoginUnknown`; the others stay. A line that does not parse, a line for a name not asked, a second for a name, or a failed run is `LoginsUnread` with source `lslogins`, never no login. Not found is its own state, `LoginsNoTool`. A name `-l` would split or the raw form would escape (`,`, `\`, a blank, what does not print: libsmartcols `fputs_nonblank`, v2.41) is not asked, and reads as a name with no line.
22. **The blank guard, as adopted:** a blank is `LoginNone` only where a source can be read and holds records; otherwise `LoginUnknown` (`vm-runs/s1/09-lslogins-blank.log`). The sources, in `lslogins`' order: `/var/log/wtmp`, `/var/lib/lastlog/lastlog2.db` where it exists, `/var/log/lastlog` (read: `lslogins.c` v2.41:827, :601; both VMs' builds link lastlog2, `08-lslogins.log`; the path, `lastlog2.h`:35 and `01-tools.log`:24). They are asked only where a blank needs it, and each is truly opened with `ScanLines`, no mode looked at: a symlink, a FIFO and a directory read as not read, without blocking. A file of NUL bytes alone holds no record, and the VMs' sparse `lastlog`, whose one record lies past the first `MaxLine`, does: past a cut it reads the whole file up to 4 MiB, uids to about 14,000, and longer than that with nothing in its first look is the safe side, no records (run: `TestBlankGuard`, 13 cases, among them all readable with records (None), wtmp unreadable and lastlog empty (Unknown), a real login beside them; `TestReadSourceRealFiles`, real files, mode 0000, a FIFO, a link). `Logins.Sources` carries what it found.
23. **Bounds.** Every command runs under a bound, those of the packages it reads through too, by a `System` wrapping `Run` (run: `TestBounds`): **5 s**, as `Pager`'s `less --version` (`pkg/engine/logview/pager.go`:25); **`lslogins` 20 s**, one wait for the password lock (15 s: `lslogins.c` v2.41:831, `lckpwdf`; read from glibc, not run) and about five of the VMs' wtmp read (0.9 s on 34 to 36 MB, `08-lslogins.log`). So the worst a draw waits, on every command hanging, is about 60 s, by adding the bounds; `lslogins`, the one timed, took 0.9 s on the VMs. At the bound the section says its source and `errors.Is(err, context.DeadlineExceeded)`, so "Can't read lslogins", and the rest stays. **A command that will not die is left behind:** `host.Real` kills it at the bound (run: `TestBoundOnARealChild`, a real `sleep`), but `os/exec`'s `Wait` waits on the process first (`os/exec/exec.go`, `c.Process.Wait()`), and one stuck in the kernel on a hung pool or mount cannot go. So `Run` returns one second later regardless, its goroutine waiting on (run: `TestHungCommand`, a host whose command never returns; no real process has hung).
24. **Writes nothing, runs only reads, no line of its own in a run log** (run: `TestReadWritesNothing`, every call on a host with a run log, both a ZFS and a plain root): its file calls are `read`, `read-nofollow`, `scan-lines`, `stat`, `lstat` and `readdir`; its commands `ip`, `df`, `zpool list`, `zfs get`, `visudo -c`, `doas -C`, `lslogins` and the header probe's `ip -4 addr show dev` and `dpkg-query --show`. Each command is the host's to log, so a draw takes a host with no run log, the shape of `statusHost` (`cmd/hardn/status.go:29`). **The one effect is not hardn's:** `lslogins` as root takes the password lock, and `lckpwdf` makes `/etc/.pwd.lock` where it is absent [I, from glibc]; it exists on both VMs (`08-lslogins.log`:4, :58).

### The JSON form (`pkg/output`)

25. **`details`, on the document, not beside it** ([output.md](../reference/output.md) §1.10): `Run.Details`, `Document.Details`, omitted otherwise. Every other document holds the nine, and an audit line ten with `time` (run: `TestDetailsOnThisFormAlone`, `TestStdoutIsOneDocument`, the old goldens unchanged). `Build` stays pure (run: `TestDetailsPure`). No flag reaches it until unit (d).
26. **A null is a fact here:** a section or a fact not read or not known is null, `[]` read and none. One finding per section with anything not read, `DETAILS_UNREADABLE`, registered, `detail` `<section> not read`, `source` what was not read; exit 0 (run: `TestDetailsUnread`, 14 cases; `TestDetailsNotKnown`; goldens `json/details.json`, `json/details-unreadable.json`). `logins` as `cli-json` draws them, `unknown: true` on an account with no line, or on a blank the guard cannot back (run: `TestDetailsRead`).

## 3. Facts relied on

| Fact | Source |
|---|---|
| GNU `less` 668 on Debian 13 and Ubuntu 26.04; BusyBox's through a link on Alpine 3.23, refusing `--version` | run: `_support/hardn-audit-2026-10-16/vm-runs/s1/01-tools.log` (its command merges stderr, so the check reads `Result.Combined`) |
| `LESSSECURE=1` turns off `!`, `\|`, `E`, `:e`, `v` on GNU's and changes nothing on BusyBox's | run, as a user: `vm-runs/s1/04-less2-*.log` |
| hardn's lines are `DATE TIME LEVEL: message` | read: `pkg/logging/logging.go:46`, `:78-133`; run: `vm-runs/s3/57-log-ubuntu26-vm.log` |
| exec gives a caught signal its default action and keeps an ignored one ignored | run here: the pseudo-terminal tests above; POSIX exec |
| `signal.Notify` on a signal started ignored stops ignoring it; `signal.Stop` puts it back | read: os/signal's doc; `signal.Ignored`'s use, `pkg/ui/prompt/reader.go:311` |
| `less` shows a log's OSC as text with no `-r` | run: `TestRunInteractiveLess`, this Mac's 581.2; on GNU 668 [I] |
| `ip -o addr`'s two shapes; `df -kP /`'s line; `/proc/self/mounts`' line; `/proc/cpuinfo`'s three keys; `/proc/meminfo`'s lines | run: `vm-runs/s1/01-tools.log`, "reader forms" |
| `/etc/sudoers.d` `drwxr-x---` on the three VMs; Alpine's `/etc/doas.conf` 0640, `/etc/doas.d` `drwxr-x---` | run: `vm-runs/s1/01-tools.log`:65, :130, :191-193 |
| `lslogins`' one call: a line per name it knows, the name and three spaces for no login, no line and exit 0 for a name it does not know | run: `vm-runs/s1/02-login.log`, `08-lslogins.log` |
| It answers blank, exit 0, with its sources missing, empty or unreadable | run: `vm-runs/s1/09-lslogins-blank.log` |
| Its order: wtmp, then lastlog2, then lastlog; the password lock per account; lastlog read at uid times the slot | read: `login-utils/lslogins.c` v2.41:827, :601, :605, :831 |
| The raw form escapes a blank, `\`, what does not print: so a field holds no space, and a control character in a host name reaches hardn as `\x1b` | read: `libsmartcols/src/print.c` v2.41:620, `include/carefulputc.h`:109 |
| lastlog2's database is `/var/lib/lastlog/lastlog2.db`, and its table is made in the call that writes a row | read: `liblastlog2/src/lastlog2.h`:35, `lastlog2.c`:239 |
| `Cmd.Wait` waits on the process before anything else, whatever `WaitDelay` says | read: Go's `os/exec/exec.go`, `(*Cmd).Wait` |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root. For the log side, on a VM, read only:

- the new tests in `pkg/host`, `pkg/host/hostfake` and `pkg/engine/logview`, `pty_linux_test.go` among them (vetted by `make check-cross` alone), and `TestRunInteractiveLess` on GNU `less` 668;
- `less --` and non-ASCII under `LC_ALL=C.UTF-8` on Debian 13 and Ubuntu 26.04; Alpine's less package (item 10's row is [I]);
- `less` as root under hardn: `LESSSECURE`, `q`, Ctrl-C at its prompt, the terminal after it, through sudo, sudo-rs and doas;
- `ScanLines` on `/var/log/hardn.log` and `/var/log/wtmp` as root and as a user; a read that fails as root.

**The report:** nothing has run on a host, `pkg/engine/details` being on `hostfake` but for `TestReadSourceRealFiles` and `TestBoundOnARealChild`. On a VM, read only: `/proc/self/status`'s form, as root and as a user; `/proc/loadavg`'s; `df`'s header; `visudo -c` and `doas -C` under the bound; `lslogins` as root for more than one account, on a usual host, with the password lock held, and through a real rotation; `ScanLines` on `/var/log/wtmp` and `/var/log/lastlog`, and the `lastlog` slot size; a global IPv6 address; a real hung process. **No VM exists for:** `zpool` and `zfs` on a ZFS root, in an LXC guest; arm64's `/proc/cpuinfo`; a lastlog2 database that exists.

**Not closed, known:** `Read` refuses a link at the path, but `less` opens the path itself, as root, so a link planted after the screen's read is followed by `less` [I, not run]; that needs write access in `/var/log`. `less`'s handling of bidi characters is its own: `Safe` does not reach it [I, not run]. Ctrl-Z and a SIGTERM to hardn alone: system.md, "Known limits".

## 5. Words

gallery-system-details.md §6 is used as it stands; the words themselves are unit (c)'s and (d)'s to draw, and this unit draws none. New here, **proposed**:

| Where | Words | Why |
|---|---|---|
| A line past the cap, after its 64 KiB | `<cut>` | The sanitiser's own notation, `<0xe9>`, `<U+202E>` |
| `Reading.Reason` for a FIFO or a device, after "Can't read it: " | "not a regular file" | `fs.ErrInvalid`'s own text is "invalid argument", which says neither |
| `Reading.Reason` for a link at the path, and on the way | "refusing a symlink"; "refusing a symlink in a directory another user can change" | `host.ErrSymlink`'s and `ErrSymlinkParent`'s text, as `reasonOf` gives other errors' |
| `DETAILS_UNREADABLE`'s `<section>`, §6 giving the form and "memory" alone | "os and kernel", "host and ips", "users", "cpu and load", "disk", "memory", "last login" | P-17's words in lower case, as the stand-in has them |
| `DETAILS_UNREADABLE`'s `source` | each source, joined by ", "; "ip addr", "df", "zpool list", "zfs get", "lslogins" | The stand-in's sources, and "Can't read <source>" uses them alike |
| JSON: `users[].tool`, `disk.health` | "unknown" | §6's word, as `host`'s facts use it, where the rules are not read and where no `zpool` answers |

**For unit (c):** "Not root: <file> not read" has one `<file>`, and the report can carry two; a user's row where the rules are not read is "Not known" (§6's state), since the report no longer says what a group says (`cli-not-root` draws `alice  sudo`, the stand-in's guess).

## 6. Decisions of this unit (engineering, stated; say if you'd rather otherwise)

1. `pkg/engine/logview` keeps the name: depguard's `engine-through-host` already fits a reader beside `header`, and a block of its own names the rule, as `header-through-host` does.
2. `yield` returns a bool, not an error: no sentinel joins `pkg/output`'s registry, and a caller's write error stays the caller's.
3. `host.MaxLine` is 64 KiB, origin/main's own limit: `MaxLast` such lines bound `Read` at 16 MiB, and a line that long is not read in a log.
4. A cut line reaches `yield` when its first `MaxLine` bytes are known, with no count of what went: a count would need the whole line first, which costs a binary file's whole length for one look.
5. `Line.Cut` is a field and the mark is `logview`'s, so the host says no words and no text can pass as the mark to `host`'s callers.
6. A cut inside a character shows that character's bytes: true, and rare.
7. It reads no further than the size at the open, so a writer that keeps appending cannot keep `-p` reading; so a `/proc` file reads as empty, and `ReadFile` is for those.
8. `RunInteractive` returns an error alone: its output went to the terminal, so a `Result` would hold nothing.
9. It logs the argv and the exit where a host has a run log, as every command is logged, and not `env`.
10. SIGQUIT is caught with SIGINT, as Go's default ends hardn with a stack dump on Ctrl-\ too; `less` ignores it [I].
11. ctx done sends SIGTERM first, as SIGKILL leaves the terminal raw; no caller today holds a context that fires.
12. The terminal's modes are put back after the child, which the brief did not ask: the seam's own SIGKILL can leave them raw, and the next prompt would then read blind.
13. `Read` does not call `Pager`: `-p` needs no `less --version`, and `TERM` is the caller's at the time.
14. `Pager` reads `Result.Combined`, as the VMs' log merged the streams, so the check holds to what ran; BusyBox's words match on neither.
15. `less 581.2`, macOS's, is refused: the digits end at a space or the line. It is no release target, and fails closed.
16. `less --version` is bounded at 5 s, as a draw waits for it; a timeout is no pager.
17. `Safe` also marks noncharacters, which the stand-in kept; no mock holds one.
18. `Elsewhere` returns the path `Safe`, as hardn.yml may be `./hardn.yml` (`pkg/config/config.go:210`) and both its callers show it.
19. `pkg/engine/details` is the name, beside `header`: depguard's `engine-through-host` fits, and a block names its own rule, `details-through-host`, which keeps it off `pkg/ui` and `pkg/output`.
20. `users.Elevation` is exposed, not `tool` and `decide`: three methods, and the row's shells, locks and findings stay private.
21. Facts keep their section's others, as the header's do, past the stand-in's whole-section rule: decision 2 (a) has "each row reads a fact or says it could not".
22. `/proc/self/status` tells root from another user, not a new `System` method: a method means p4status' guard, the fake and every other implementer, for one read of a file `ReadFile` reaches.
23. A user's grant with rules unread is Unknown, not a group's guess: a group names nothing a rule does not, and the stand-in's "sudo" there would be the guess decision 2's rows avoid.
24. A rule file is reported only where a tool that matters read it: on Alpine, whose `/etc/sudoers.d` also refuses (`01-tools.log`:193), sudo is not installed, so it is not named.
25. The guard's "a source" is any one, as adopted: wtmp holding records with `lastlog` unreadable reads None. `Logins.Sources` keeps what each said, should that tighten.
26. A lastlog2 database with any bytes counts as holding records: hardn has no SQLite reader, and the table is made by the write of a row (`lastlog2.c`:239), so empty after a removal is the false None left [I]. No VM has the file.
27. A blank's guard costs one look per source, only where there is a blank: a host where all have logged in reads nothing more.
28. A name not askable is not asked, so one odd account in `/etc/passwd` does not turn the whole section unread.
29. A failed `zfs get` or `zpool list` stays a finding though `df` answered, as the pool's figures were not what was asked; not installed is no finding, only "unknown".
30. `lslogins`' 20 s is the section's, not per account: a held lock past one wait reads "Can't read lslogins", and the next Refresh tries again.
31. Commands run one after another: running them at once would save time on a hung host alone.
32. Report values are as the host gave them, not `logview.Safe`d: the rows clean what they draw (`pkg/ui/component/ctx.go`, `clean`), JSON escapes it, and `lslogins`' raw form has already escaped `From` and `TTY`. Unit (c)'s uncapped off-terminal path must clean too.
33. `DETAILS_UNREADABLE`'s code is `pkg/output`'s, beside `CONFIG_DEPRECATED_VALUE`: `Build` makes the finding.
34. `hostname` and `os` stay in `host`, as the gallery has them, and `Run.Hostname` stays the caller's to set from `Report.Host.Hostname`.

## 7. API

```go
package host

const MaxLine = 64 << 10
type Line struct { Text []byte; Cut bool }
func ScanReader(ctx context.Context, in io.Reader, yield func(Line) bool) error
func InteractiveEnv(env []string) []string
// System gains:
ScanLines(ctx context.Context, name string, yield func(Line) bool) (fs.FileInfo, error)
RunInteractive(ctx context.Context, env []string, name string, args ...string) error

package logview

const File = "/var/log/hardn.log"
const MaxLast = 256
const LevelError, LevelWarning, LevelInfo, LevelSuccess, LevelInstalled // "ERROR", ...
type State uint8 // Lines, NoLog, Empty, Unreadable
type Reading struct { Path string; State State; Reason string; Err error; Size int64; Total int; Last []Line }
type Line struct { Text, Level string }
type Entry struct { Time, Level, Message string }
type Command struct { Argv, Env []string }
func Read(ctx context.Context, sys host.System) Reading
func Elsewhere(logFile string) string
func Safe(s string) string
func Show(l host.Line) string
func ParseLine(line string) Entry
func Pager(ctx context.Context, sys host.System, term string) (Command, bool)
func (c Command) Run(ctx context.Context, sys host.System) error

package details

const CPUInfoFile, LoadAvgFile, MemInfoFile, SelfStatusFile // the kernel's files
const WtmpFile, LastlogFile, Lastlog2File                   // lslogins' sources
const SourceIP, SourceDF, SourceZpool, SourceZFS, SourceLslogins // as Unread names them
type Report struct { OS OSKernel; Host HostIPs; Users Users; CPU CPU; Disk Disk; Memory Memory; Logins Logins }
type Unread struct { Source string; Err error }
type OSKernel struct { OS header.OS; Kernel string; Uptime header.Uptime; Unread []Unread }
type HostIPs struct { Hostname string; Addresses []Address; Unread []Unread }
type Address struct { Interface, Family, Address string }
type Users struct { List []User; Root detect.Tri; RootErr error; Unread []Unread }
func (u Users) NotRead() []string
type User struct { Name string; UID int; Grants []users.Grant }
func (u User) Admin() (tool string, t detect.Tri)
type CPU struct { Model string; CPUs, Sockets int; Load [3]string; Unread []Unread }
type Disk struct { Source, Type string; Used, Total uint64; Known bool; Pool, Health string; Unread []Unread }
type Memory struct { Total, Available uint64; AvailableKnown bool; Unread []Unread }
func (m Memory) Used() (used uint64, ok bool)
type LoginsState uint8 // LoginsUnread, LoginsNoTool, LoginsRead
type LoginState uint8  // LoginUnknown, LoginNone, LoginAt
type Logins struct { State LoginsState; List []Login; Sources []Source; Unread []Unread }
type Login struct { User string; State LoginState; When, TTY, From string; At time.Time }
type SourceState uint8 // SourceUnread, SourceMissing, SourceNoRecords, SourceRecords
type Source struct { Path string; State SourceState; Err error }
func Read(ctx context.Context, sys host.System, f detect.Facts) Report

package users // pkg/engine/controls/users

type Elevation struct{ /* the tools */ }
type NotRead struct { Tool string; Err error }
type Grant struct { Tool string; Grants detect.Tri }
func ReadElevation(ctx context.Context, sys host.System, f detect.Facts) Elevation
func (e Elevation) NotRead() []NotRead
func (e Elevation) GrantsOf(db accounts.DB, u accounts.User) []Grant

package output

const CodeDetailsUnreadable = "DETAILS_UNREADABLE"
type Details struct { Kernel, Uptime *string; Addresses []DetailAddress; Users []DetailUser; CPU *DetailCPU; Disk *DetailDisk; Memory *DetailMemory; Logins []DetailLogin }
// Run gains Details *details.Report; Document gains Details *Details, omitted when nil.
```

## 8. Not in these parts

- **The gallery:** its `readLog`, `safeLine`, `parseLogLine`, `pagerFor`, `readDetails`, `parseLogins` and `detailsDocument` stay until the gallery moves onto these packages (this unit's last part). It will then differ from what it draws today where the stand-in guessed: a user's row with the rules unread, `details.addresses` and `details.users` null where not read (the stand-in has `[]`), `cpu.model` and `cpu.sockets` null where not known (the stand-in has `""` and `0`). `p4status`' guard alone changed for the new methods (`ScanLines` passed, `RunInteractive` refused; [vmcheck.md](vmcheck.md) §2).
- **Unit (c):** the screen, its "Last N of M lines" and the size's words, Open, what it does with a pager's error, `Flush`; the report's rows, each fact's or its "Can't read <source>" from `Unread`, and its sizes' GiB and percentages.
- **Unit (d):** `-p`'s forms and exit statuses, the `logFile` warning, reading `TERM` from hardn's environment for `Pager`; `--output json`, its `Run{Mode: ModeCheck, Hostname, Facts, Details}`; `hardn system-details`' host with no run log.
