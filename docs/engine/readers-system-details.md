# The readers of System Details and Logs (`pkg/engine/details`, `pkg/engine/logview`, `host.System`'s `ScanLines` and `RunInteractive`, `pkg/output`'s `details`)

> **Unit:** refactor plan slice 1 (System Details + Logs, both read only), unit (b), `next/s1-readers`, from `next/s1-gallery` at 7bc756c7. It builds on unit (a) ([gallery-system-details.md](gallery-system-details.md)), whose eleven decisions the maintainer adopted on 2026-10-30 ("Adopt all eleven"). Decisions implemented here: 1 (a)'s document (the `details` field, no flag), 2 (a) (the seven sections), 3 (a) (Last Login, its blank guard), 6 (a) (the log's file), 7 (a) (GNU `less` alone), and 8 (a)'s and 10 (a)'s needs (the last lines, the pipe form). The gallery runs on both readers (§2, items 27 to 30), and the mocks of the options not adopted are gone (git keeps them at 701449ae). No V-number holds the defects it fixes (gallery-system-details.md §7: the slice's "Logs dump" and "Broken log parser", the reader's side of rows 21, 22 and 26, and rows 7 to 16 of the report).
> **Status:** built and tested on macOS, in process, on `hostfake` and on real files, real children and pseudo-terminals in `t.TempDir()` (run). Each form the report parses is typed from a VM's log with synthetic values, or labelled. The gallery's mocks run those readers on simulated hosts. **The new tests have run on Linux, on the three VMs, as a user and as root** (`vm-runs/s1/13-gotest-*.log`), and the forms the readers read and `less` as root by hand, read only (`10-forms.log`, `11-less3-*.log`, `12-less4.log`). Both readers have read the three real hosts, as a user and as root, and agree with what each says (`15-readers-*.log`, `16-truth-*.log`). **hardn itself has not run:** §4. There is no control on the engine, no `Check`, no plan and no flow, as neither screen has a main-menu row or a change. Nothing is wired into `cmd/hardn`, `pkg/menu`, `pkg/system`, `pkg/cmd`, `pkg/logging`, `pkg/adapter` or `pkg/config`; no `--output` flag and no screen package here (units (c) and (d)). Goldens: `pkg/output/testdata/codes.golden` gains `DETAILS_UNREADABLE`'s line, as a registered code does; `json/details.json` and `details-unreadable.json` are new; 20 of the gallery's retired go and 32 change, each named (item 30); `pkg/characterization` and `pkg/menu` unchanged (`git diff --name-status 7bc756c7 -- '*testdata*'`).

The screens of unit (c) and the wiring of unit (d) should only draw and wire. So the host gains two seams, `ScanLines` and `RunInteractive`; `pkg/engine/logview` reads hardn's log through them, and `pkg/engine/details` the report's seven sections, both as data, with nothing rendered, as `pkg/engine/header` reads the header's facts; and `pkg/output` gains the report's JSON form.

## 1. Why

In this section a bare § is [gallery-system-details.md](gallery-system-details.md)'s.

origin/main prints the whole log at once, 20,000 of 20,000 lines, read whole into memory, its bytes raw, from `cfg.LogFile`, which hardn has never written (gallery-system-details.md §2.7, §2.8). Its parser stops silently at a line over 64 KiB and reads the clock as the level, and nothing live calls it (§2.10). Unit (a) drew the replacement on the gallery's stand-ins, `readLog`, `safeLine`, `parseLogLine` and `pagerFor` (§8), which read the whole file and go by a link to tell BusyBox's `less`. `host.System` had no call for the end of a file and none that gives a child the terminal: `Run` pipes output (`pkg/host/real.go`, `start`).

origin/main's report stops at its first collector's error and loses all it read (§2.3), asks `zfs` about a hard-coded dataset (§2.5), reads Last Login from a `lastlog` stock Debian 13 and Ubuntu 26.04 do not have (§2.5), and says "Bare Metal", "1 socket" and "0.00 GHz" for what it does not know. Unit (a) drew the replacement on `readDetails`, which groups read and any failure loses a section (§8).

## 2. Guarantees

### The two seams

[system.md](../safety/system.md) has each in full, "The line scan" and "The interactive child".

1. **`ScanLines(ctx, name, yield)`** serves the three later uses with one call: the screen's last lines and total (`Read`, item 7), `-p`'s pipe form, every line written as it reads (`logview.Each`, run: `TestEveryLineAsItReads`; unit (d) writes it), and "can this file be read and does it hold anything" for Last Login's blank guard (run: `TestHoldsAnything`, a 1 MiB binary file looked at for one line). Memory is bounded whatever the file holds (run: `TestScanReaderIsBounded`). It refuses anything not a regular file without blocking: a real FIFO with no writer and `/dev/null`, `fs.ErrInvalid`; a directory, `EISDIR` (run: `TestScanLinesRefusesWithoutBlocking`, `TestContract`). A line past `host.MaxLine` is cut, `Line.Cut` says so, and the read goes on, with no newline at the end, CRLF and empty lines each handled (run: `TestContract`, on `host.Real` and the fake alike). It honours ctx between reads (run: `TestScanLinesContextDuringTheRead`) and adds no run-log line (run: `TestScanLinesNoRunLog`).
2. **It does not follow a symlink at the path, and is guarded,** as `System`'s doc and `pkg/host/guard.go` have it: `ErrSymlink`, and a link on the way is followed only from a trusted directory (run: `TestContract`, "scan lines: guarded"). What it reads is shown to whoever asked, as root from the menu, so a link at `/var/log/hardn.log` must not show them `/etc/shadow`. A `/var/log/hardn.log` an admin linked elsewhere reads "Can't read it: refusing a symlink".
3. **`RunInteractive(ctx, env, name, args...)`** gives one named program hardn's stdin, stdout and stderr and waits for it. **Where stdin or stdout is not a terminal it runs nothing and returns an error:** `less` given a pipe acts as `cat`, passing a log's bytes through raw (run by hand on 581.2, and GNU's 668, `10-forms.log`), and the pipe can end at a terminal, `hardn | tee` (run: `TestRunInteractiveNeedsATerminal`, on hardn's real files: neither, each alone, both). Exactly `env` and `Run`'s `PATH` reach it, nothing inherited (run: `TestRunInteractiveExactEnvironment`, `env(1)` as a real child), no shell, the name as `Run` resolves it (run: `TestRunInteractiveCommand`, `TestRunInteractiveRefuses`), the exit status as `Run` gives one (run: `TestRunInteractiveExit`). The fake records the call and runs nothing (run: hostfake's `TestRunInteractive`).
4. **Signals.** Read of today's handling: `signal.Notify` in `ReadLine` for its own call alone (`pkg/ui/prompt/reader.go:319`, `:348`), `signal.NotifyContext` around an apply and `hardn confirm`'s clean-up (`cmd/hardn/engine.go:45`, `cmd/hardn/confirm.go:42`), a `SIGHUP` watch around the same (`cmd/hardn/engine.go:78`), and `signal.Ignore` in the hold around an in-process rollback alone (`pkg/engine/hold/hold.go:117`); so outside those, Ctrl-C ends hardn by Go's default (`cmd/hardn/status.go`'s `readStatus`). What it does: while the child runs, SIGINT and SIGQUIT are caught on a channel nothing reads, as exec gives a caught signal its default action in the child and keeps an ignored one ignored; one hardn started with ignored, or that the hold ignores, stays ignored; `signal.Stop` puts each back, another registration untouched (run, on a pseudo-terminal, typing at its master: `TestRunInteractiveCtrlC`, `TestRunInteractiveCtrlBackslash`, `TestRunInteractiveKeepsRegistrations`). Run on the VMs too, as a user and as root (`vm-runs/s1/13-gotest-*.log`). **What could not be tested:** a person's real terminal, an ssh session, sudo or doas between hardn and the terminal; Ctrl-Z and a SIGTERM to hardn alone are read, not run (system.md, "Known limits").
5. **ctx:** done on entry runs nothing; done during the run sends SIGTERM, then SIGKILL after 5 s, an error wrapping `ctx.Err()` (run: `TestRunInteractiveContext`). A child that ends with the terminal's modes changed has them put back (run: `TestRunInteractiveTerminalModes`). The prompt reader needs nothing else of the seam: no read of its outlives its call (`pkg/ui/prompt/doc.go`, "No read outlives its call"), so nothing is reading the terminal while the child has it. Whether unit (c) calls `Flush` after the pager is unit (c)'s.

### The reader (`pkg/engine/logview`)

6. **The file** is `logview.File`, `/var/log/hardn.log`, always (decision 6 (a); run: `TestFile`, against `output.LegacyLogPath`). `Elsewhere(logFile)` returns the path, safe to show, where hardn.yml's `logFile` names another; the empty value and the same path, however spelled (`path.Clean`), are no difference (run: `TestElsewhere`). For "hardn.yml's logFile, \<path\>, isn't used" and unit (d)'s warning. It does not read hardn.yml: the caller has the configuration.
7. **`Read(ctx, sys)`** gives `Path`, `State`, `Size`, `Total` and `Last`, oldest first, at most `MaxLast` (256); the screen fits what it has room for from their end (run: `TestReadLong`, 785 lines). `State` is `Lines`, `NoLog`, `Empty` or `Unreadable`, the last with `Reason`, in the words after "Can't read it: " (gallery `reasonOf`), and `Err`, so that `-p` can tell `fs.ErrPermission` for decision 9 (a)'s "Run it with sudo." (run: `TestRead` on hostfake, 9 cases; `TestReadRealFiles` on real files, a FIFO, a directory, a symlink, mode 0000, a file on the way; each without blocking). A read that fails part way gives no `Total` (run: `TestEachFails`). A done ctx is `Unreadable`, in ctx's words (run: `TestReadDoneContext`). One scan and nothing else, no run-log line, on a host with a run log (run: `TestReadWritesNothing`). **`Each(ctx, sys, yield)`** is the same pass for `-p` off a terminal: every line as it reads, safe, in memory bounded by the line, and the same `Reading` but for `Last`, so that "can't read <path>: <reason>" has Read's words whenever it fails, part way too (run: `TestEachFails`, `TestEachStops`, `TestEachMissing`).
8. **`Safe`** (and `Show`, `Safe` with the cut's mark) is exported: `-p` and the screen both use it, `Read` already has for `Last`. What a terminal would act on is made safe and shown, not stripped: C0 as `^[`, DEL as `^?`, a tab as 4 spaces; C1 as `<U+009B>`; bidi overrides and isolates, what draws as nothing, blanks other than the space, as their code point; non-UTF-8 bytes as `<0xe9>`, each alone. Typographic characters are kept (run: `TestSafeKeepsTypography`). **Tried to break it** (run: `TestSafe`): an OSC with a BEL and with ST, a clipboard write, a link; a C1 CSI as U+009B and as the raw byte 0x9B; an invalid sequence next to an escape, before and after it; overlong encodings of ESC, CSI and `/`; a surrogate. **The property** (run: `TestSafeWhole`, every code point, every pair of bytes, on its own and beside an escape): the result is valid UTF-8, holds no control or format character, and `Safe` leaves it as it is.
9. **`ParseLine`** reads `DATE TIME LEVEL: message`, on `log.LstdFlags`' exact shape and logging's five levels, for the colour alone; anything else stays whole, with no level (run: `TestParseLine`, on the forms of `_support/hardn-audit-2026-10-16/vm-runs/s3/57-log-ubuntu26-vm.log`). A command's own "ERROR:" in a run-log line stays that line's INFO.
10. **`Pager(ctx, sys, term)`** returns the command or none (decision 7 (a)). `less --version`, which `Run` finds on `host.SearchPath` as an executable file, must exit 0 with a first line `less <digits>` (`less 668 (GNU regular expressions)`; BusyBox prints `less: unrecognized option: version`, `vm-runs/s1/01-tools.log`). Anything else, any error, is no pager: it fails closed (run: `TestPager`, 16 answers; `TestPagerDoneContext`; `TestPagerBound`). `Command.Run` asks again at the key press, as `RunInteractive` finds `less` again by its name, so a package changed under an open menu does not run BusyBox's (run: `TestCommandRunChecksAgain`). It never reads `$PAGER` or any other variable naming a program.
11. **What would run**, exactly (run: `TestPager`, `TestCommandRun`):

    ```text
    argv: less -- /var/log/hardn.log
    env:  LESSSECURE=1 LESSHISTFILE=- TERM=<checked> LC_ALL=C.UTF-8 PATH=/usr/sbin:/usr/bin:/sbin:/bin
    ```

    - `LESSSECURE=1`: `!`, `|`, `E`, `:e` and `v` each say "Command not available" (run, as a user: `vm-runs/s1/04-less2-deb13-vm.log`, `04-less2-ubuntu26-vm.log`; as root, this command, through sudo and sudo-rs: `11-less3-*.log`).
    - `LESSHISTFILE=-`: no history file, so what is typed at less's prompts is not left in a file as root. The secure mode may already stop it; this says so whatever that version does [I: less(1), not run].
    - `TERM`: from a value the caller passes, as it comes from whoever started a root process and names a file in the terminfo database. A letter or digit, then letters, digits, `-`, `_`, `+`, `.`, no `..`, at most 64 bytes; otherwise `vt100`, under which the VMs' `less` ran (run: `TestSafeTerm`, and through `Pager`, `TestPagerTerm`; the VMs' `04-less2-*.log`).
    - `LC_ALL=C.UTF-8`, so a log's non-ASCII shows as itself (run, GNU 668: `11-less3-*.log`; `C.utf8` listed on both, `10-forms.log`; any name is UTF-8 to musl [I]). If the locale is missing, `less` shows bytes as `<C3><A9>`, which is safe.
    - `--` before the path: run on this Mac's `less` 581.2 (`TestRunInteractiveLess`, and a pipe by hand) and GNU `less` 668 (`11-less3-*.log`).
    - No `LESS`, `LESSOPEN`, `LESSCLOSE`, `LESSKEY`, `EDITOR`, `VISUAL`, `SHELL` or `HOME` gets through: `RunInteractive` passes nothing it is not given (run: `TestRunInteractiveExactEnvironment`, with each set in the parent).
12. **All on `host.System`, no line for a draw.** `Read` runs nothing. `Pager` runs `less --version`, a command, which a host with a run log logs (run: `TestPagerCheckIsACommand`): a draw reads on a host with none, the shape of `statusHost` (`cmd/hardn/status.go:29`).

### The report's reader (`pkg/engine/details`)

13. **`Read(ctx, sys, facts)`** gives a `Report` of seven sections, facts and states and no words: `OS`, `Host`, `Users`, `CPU`, `Disk`, `Memory`, `Logins`. A section that fails leaves the others, and a fact leaves its section's others: what could not be read is that section's `Unread`, by source and why (run: `TestOneSectionLeavesTheRest`, each source failing alone, the others whole). A done ctx leaves every fact with ctx's error (run: `TestDoneContext`). A zero fact is not established, and a slice is `nil` only where it was not read (`[]`, none).
14. **OS, kernel, uptime, hostname:** `header.OSOf`, `ReadUptime` and `ReadHostname`, the header's own reads, and `/proc/sys/kernel/osrelease`, on the caller's detect facts (run: `TestRead`). Not the whole of `header.Read`: its route files, `ip -4 addr show dev`, `dpkg-query --show` and reboot facts are not the report's (run: `TestReadWritesNothing`).
15. **Addresses:** `ip -o addr show scope global`, `metric 100` (Debian, Ubuntu) and `noprefixroute` (Alpine) passed over alike, IPv4 and IPv6 ([I] for `inet6`: no VM has a global address) (run: `TestAddresses`, typed from `vm-runs/s1/01-tools.log`:50, :115, :178). A line in no form known is said once in `Unread` and the others stay.
16. **Users:** uid 1000 and up, not `nobody` or `nfsnobody`, each name once, as the users check takes the first (`getpwnam`'s [I]). What lets each administer is `users.Elevation`, `readTools` and `decide` exposed and not copied, so a rule such as cloud-init's counts and a group a rule does not name does not (run: `TestUsers`, `TestElevation`). `User.Admin()` gives the first tool that grants, or No, or Unknown, as the check's `elevates`. `Users.Tool` names the tool a No is said of, doas where detect found doas and not sudo, else sudo, so the row's "No sudo" or "No doas" needs the report alone (run: `TestUsers`). Root alone reads no rule and runs no `visudo` (run: `TestUsers`).
17. **As a user other than root** the report says so and does not guess. `Users.Root` is `/proc/self/status`'s effective uid, the second number of its `Uid:` line, read through `ReadFile`, so on the seam and with no new method on `System` (the form: `10-forms.log`; the second number, run: `TestNotRoot`, each number apart). Unknown where it cannot tell, which takes nothing else with it. `/etc/sudoers.d` is `drwxr-x---` on all three VMs and Alpine's `/etc/doas.conf` 0640 (`vm-runs/s1/01-tools.log`:65, :130, :191-193). The rules' read stops at the first refusal, and `NotRead()` holds every file of that tool's that refuses: `/etc/sudoers` and `/etc/sudoers.d`, or `/etc/doas.conf` and `/etc/doas.d` (run: `TestNotRoot`; `/etc/sudoers` 0440 on all three, `10-forms.log`). Each user's grant is then Unknown, never a group's guess, and no `visudo` runs.
18. **CPU and load:** `processor` lines are logical CPUs; the first `model name`; distinct `physical id`s. Where those two are absent, as on arm64 [I, not run], `Model` is "" and `Sockets` 0, and none is made up. No `processor` line at all is not a form known. `/proc/loadavg`'s form is proc(5)'s, as `10-forms.log` has it (run: `TestCPU`).
19. **Disk:** `/proc/self/mounts`' last line for `/`, through `detect.ParseMounts`, then `df -kP /`, header passed over (`10-forms.log`), figures in KiB (run: `TestDF`, on `01-tools.log`:51-52, :116-117, :179-180). **On ZFS, every form [unverified], no ZFS VM** (run: `TestZFS`): the dataset is the mount's, never one of hardn's; `zpool list -H -o health <pool>`; `zfs get -Hp -o value used,available <dataset>`, total their sum. No `zpool`, `Health` "" beside its `Pool`, so not known, and not a finding. No `zfs`, `df` gives the figures. One that fails is in `Unread`, and `df` still answers.
20. **Memory:** `MemTotal` and `MemAvailable`. No `MemAvailable` is `AvailableKnown` false, and `Used()` says not known (run: `TestMemory`). `Used` never counts the kernel's caches, as origin/main's does.
21. **Last Login (decision 3 (a)):** one call, `lslogins --time-format=iso --noheadings --raw --output=USER,LAST-LOGIN,LAST-TTY,LAST-HOSTNAME -l root,<each listed user>`, its lines keyed by name, in the order asked, split at single spaces (run: `TestLogins`, on `02-login.log`, `08-lslogins.log`). A name with no line is that account's `LoginUnknown`; the others stay. A line that does not parse, a line for a name not asked, a second for a name, or a failed run is `LoginsUnread` with source `lslogins`, never no login. Not found is its own state, `LoginsNoTool`. A name `-l` would split or the raw form would escape (`,`, `\`, a blank, what does not print: libsmartcols `fputs_nonblank`, v2.41) is not asked, and reads as a name with no line.
22. **The blank guard:** a blank is `LoginNone` only where no source refuses and one holds records; otherwise `LoginUnknown`. `lslogins` answers blank, exit 0, with a source it cannot read (`vm-runs/s1/09-lslogins-blank.log`), so a login held in wtmp alone answers blank where wtmp refuses, whatever `lastlog` holds. That stays inside the adopted "only where a source can be read and holds records", a necessary condition (decision 25). The sources, in `lslogins`' order: `/var/log/wtmp`, `/var/lib/lastlog/lastlog2.db` where it exists, `/var/log/lastlog` (read: `lslogins.c` v2.41:827, :601; both VMs' builds link lastlog2, `08-lslogins.log`; the path, `lastlog2.h`:35 and `01-tools.log`:24). They are asked only where a blank needs it, and each is truly opened with `ScanLines`, no mode looked at: a symlink, a FIFO and a directory read as not read, without blocking. A file of NUL bytes alone holds no record, and the VMs' sparse `lastlog`, whose one record lies past the first `MaxLine`, does: past a cut it reads the whole file up to 4 MiB, uids to about 14,000, and longer than that with nothing in its first look is the safe side, no records (run: `TestBlankGuard`, 17 cases, among them all readable with records (None), wtmp refused and lastlog holding records (Unknown), a real login beside them; `TestReadSourceRealFiles`, real files, mode 0000, a FIFO, a link; `TestGuardBound`, its 5 s and the draw's ctx).
23. **Bounds.** Every command `Read` runs is under a bound, those of the packages it reads through too, by a `System` wrapping `Run` (`host.RunBounded`; run: `TestBounds`): **5 s**, as `Pager`'s `less --version` (`lessCheck`; run: `TestPagerBound`); **`lslogins` 20 s**, one wait for the password lock (15 s: `lslogins.c` v2.41:831, `lckpwdf`; read from glibc, not run) and about five of the VMs' wtmp read (0.9 s on 34 to 36 MB, `08-lslogins.log`). So the worst `Read` waits, on every command hanging, is about 51 s on a ZFS root with sudo, by adding the bounds and a second's grace for each of its six commands (`ip`, `visudo -c`, `zpool list`, `zfs get`, `df`, `lslogins`; counted on the fake); `lslogins`, the one timed, took 0.9 s on the VMs. A read of a file has no bound but ctx between reads. **The bound is `Read`'s:** `detect.Detect`, which comes first on `hardn system-details`' path, runs `systemctl`, `sudo --version` and `dpkg-query` with none, so `details.Bounded(sys)` gives it the same (run: `TestBounded`; the gallery detects through it, as unit (d) must). The screens' path takes the main menu's facts and detects nothing. At the bound the section says its source and `errors.Is(err, context.DeadlineExceeded)`, so "Can't read lslogins", and the rest stays. **A command that will not die is left behind:** `host.Real` kills it at the bound (run: `TestBoundOnARealChild`, a real `sleep`), but `os/exec`'s `Wait` waits on the process first (`os/exec/exec.go`, `c.Process.Wait()`), and one stuck in the kernel on a hung pool or mount cannot go. So `RunBounded` returns one second later regardless, its goroutine waiting on (run: `TestHungCommand`, `TestPagerBound`, a host whose command never returns; no real process has hung).
24. **Writes nothing, runs only reads, no line of its own in a run log** (run: `TestReadWritesNothing`, every call on a host with a run log, both a ZFS and a plain root): its file calls are `read`, `read-nofollow`, `scan-lines`, `stat`, `lstat` and `readdir`; its commands `ip`, `df`, `zpool list`, `zfs get`, `visudo -c`, `doas -C` and `lslogins`. Each command is the host's to log, so a draw takes a host with no run log, the shape of `statusHost` (`cmd/hardn/status.go:29`). **The one effect is not hardn's:** `lslogins` as root takes the password lock, and `lckpwdf` makes `/etc/.pwd.lock` where it is absent [I, from glibc]; it exists on both VMs (`08-lslogins.log`:4, :58).

### The JSON form (`pkg/output`)

25. **`details`, on the document, not beside it** ([output.md](../reference/output.md) §1.10): `Run.Details`, `Document.Details`, omitted otherwise. Every other document holds the nine, and an audit line ten with `time` (run: `TestDetailsOnThisFormAlone`, `TestStdoutIsOneDocument`, the old goldens unchanged). `Build` stays pure (run: `TestDetailsPure`). No flag reaches it until unit (d).
26. **A null is a fact here:** a section or a fact not read or not known is null, `[]` read and none. One finding per section with anything not read, `DETAILS_UNREADABLE`, registered, `detail` `<section> not read`, `source` what was not read; exit 0 (run: `TestDetailsUnread`, 14 cases; `TestDetailsNotKnown`; goldens `json/details.json`, `json/details-unreadable.json`). `logins` as `cli-json` draws them, `unknown: true` on an account with no line, or on a blank the guard cannot back (run: `TestDetailsRead`).

### The gallery on the readers

27. **The mocks read with the readers**, where the stand-ins stood (gallery-system-details.md §8): `readDetails` is `detect.Detect`, through `details.Bounded`, and `details.Read`; `detailsDocument` and `detailsJSON` are `output.Build`'s document, printed by `output.WriteJSON`; `readLog` is `logview.Read`, `logview.Elsewhere` and, only where there are lines to open, `logview.Pager`; `safeLine` is `logview.Safe`; `parseLogLine` is `logview.ParseLine`; `pagerFor` and `pagerChoice` are `logview.Pager` and `logview.Command`; `cli-pipe`'s lines are `logview.Each` (`pipeLines`). **Stand-ins left behind: none.** The sections, rows, layout and words stay in the gallery until unit (c): `sectionRows`, `fileSection`, `logRows`, `codeRows`, `logScreen`, `report`, `offTerminalCtx`. Each host is held to the reader's reading (run: `TestSDHosts`, `TestSDRows`, `TestSDLastLogin`, `TestSDNotRootNamesTheRules`, `TestLogHosts`, `TestPagerOnTheHosts`, `TestPipeIsEveryLine`, `TestControlHostIsSafe`). Nothing is written and nothing runs but reads, `less --version` the one new command, `run-interactive` never (run: `TestSystemDetailsWritesNothing`, `TestLogsWriteNothing`).
28. **The simulated hosts answer what the readers ask,** and nothing else changed in them: `/proc/self/status`, root's on every host; wtmp holding a record where `lslogins` is there (`01-tools.log`:30, :96), so a blank is backed; `less --version` as each VM printed it (`01-tools.log`:22, :88, :153; BusyBox's exits 1, `15-readers-alpine323-vm-*.log`; the less package's line is [I]); and for `cli-not-root`, `asUserSys`, the rules refused as on the VMs. `detailsModel` is still the gallery's: what a fake host answers.
29. **Retired, with their goldens, tables and words; git keeps them at 701449ae:** `screen-report-sections`, `report-sections-open`, `screen-report-pager`, `report-pager-open`, `report-wide` and its drawing, `screen-export-command`, `export-command-open`, `screen-export-file`, `export-file-open`, `plan-only-indicator`, `screen-read-pager`, `screen-read-pager-none`, `cli-terminal-pager`, the plays `system-details-sections`, `-pager`, `-export-command`, `-export-file`, `-export-file-plan-only`, `logs-read-pager` and `-none`, the "pager-env" half of `pager-commands`, "Until 3.11 is decided" in `cli-flags`, and the fields and constants only they used. `origin*`, `plan-only` and the adopted forms stay: 19 System Details and 21 Logs listing mocks, 8 and 12 plays. The retirement is its own commit, so each golden's diff has one cause.
30. **Every surviving golden that changed, and why** (run: `TestSystemDetailsGolden`, `TestPlaySystemDetailsGolden`, `TestLogsGolden`, `TestPlayLogsGolden`; `pkg/characterization` and `pkg/menu` testdata unchanged):

    | Golden | Why |
    |---|---|
    | `system-details/cli-not-root` | **The one adopted screen whose drawing differs from what unit (a) drew: two lines.** The row `alice  sudo`, its bullet `-`, is now `alice  Not known`, with the unknown marker (`[?]`, `?` in the Unicode cells); and "Not root: /etc/sudoers.d not read" is now "Not root: /etc/sudoers, /etc/sudoers.d not read". The reader does not guess from a group, and the report carries both files. It is what the gallery page's §6 state says, the two files' form is adopted (§9), and the note above it now says so too (decisions 23 and 38 below). Stated as a finding, as the adopted mock's row was the stand-in's guess. The commit that made it, 434d58fd, says "the one drawn change" of the row alone |
    | `logs/pager-commands` | The "pager-env" half gone; each row the argv, `less -- /var/log/hardn.log`, under "pager-less, adopted"; a section "Its environment" holds `Pager`'s four variables, as the whole command would be cut at the row |
    | `logs/open`, `play/logs-open` | The command in the gallery's note: `LESSSECURE=1 LESSHISTFILE=- TERM=xterm-256color LC_ALL=C.UTF-8 less -- /var/log/hardn.log`, `Pager`'s, for the stand-in's `LESSSECURE=1 less <path>` |
    | `logs/parser` | The heading "logview.ParseLine" for "parseLogLine, the stand-in", and its note |
    | `system-details/cli-flags` | "Until 3.11 is decided" gone; the note says decision 1 (a) |
    | `play/system-details-hosts`, `play/logs-hosts` | The hosts' menus, nine each; play's note |
    | `system-details/cli-json`, `cli-json-unreadable` | Notes alone: decision 1 (a), the code registered. The JSON is byte for byte what the stand-in printed on these two hosts, where every section it holds is read |
    | `system-details/screen-debian13`; `logs/screen-lines`, `screen-long`, `screen-alpine323`, `cli-terminal-tail` | Notes alone: the retired treatment's name out; on Alpine, `less --version` |
    | `play/system-details`, `-plan-only`, `-refresh`, `-alpine`, `-sudo-rs`, `-unreadable`, `-zfs`; `play/logs`, `-plan-only`, `-refresh`, `-alpine`, `-control`, `-elsewhere`, `-empty`, `-missing`, `-no-pager`, `-unreadable` | Notes alone: play's note, and each host's, the stand-ins and the treatment's name out |

    Every other adopted screen draws what it drew, line for line. `cli-pipe` and `cli-pipe-control` did not change: `logview.Show` prints what `safeLine` did on those lines.

## 3. Facts relied on

| Fact | Source |
|---|---|
| GNU `less` 668 on Debian 13 and Ubuntu 26.04; BusyBox's through a link on Alpine 3.23, refusing `--version` | run: `_support/hardn-audit-2026-10-16/vm-runs/s1/01-tools.log` (its command merges stderr, so the check reads `Result.Combined`) |
| `LESSSECURE=1` turns off `!`, `\|`, `E`, `:e`, `v` on GNU's and changes nothing on BusyBox's | run, as a user: `vm-runs/s1/04-less2-*.log` |
| hardn's lines are `DATE TIME LEVEL: message` | read: `pkg/logging/logging.go:46`, `:78-133`; run: `vm-runs/s3/57-log-ubuntu26-vm.log` |
| exec gives a caught signal its default action and keeps an ignored one ignored | run here: the pseudo-terminal tests above; POSIX exec |
| `signal.Notify` on a signal started ignored stops ignoring it; `signal.Stop` puts it back | read: os/signal's doc; `signal.Ignored`'s use, `pkg/ui/prompt/reader.go:311` |
| `less` shows a log's OSC as text with no `-r`, on a terminal | run: `TestRunInteractiveLess`, this Mac's 581.2; GNU 668, `12-less4.log` |
| `less` with stdout a pipe acts as `cat`: an OSC 52 and a C1 CSI pass through raw | run by hand, this Mac's 581.2, `LESSSECURE=1`, through `od -c`; GNU 668, the OSC, `10-forms.log` |
| `/proc/self/status`'s `Uid:` line; `/proc/loadavg`; `df -kP /`'s header; `/etc/sudoers` 0440, which a user cannot read; `visudo -c` and `doas -C` as root, exit 0; `lslogins` as root for two names; the `lastlog` slot, 292 bytes (292292 at uid 1000) | run: `vm-runs/s1/10-forms.log`, `12-less4.log` (`visudo` on Debian) |
| `ip -o addr`'s two shapes; `df -kP /`'s line; `/proc/self/mounts`' line; `/proc/cpuinfo`'s three keys; `/proc/meminfo`'s lines | run: `vm-runs/s1/01-tools.log`, "reader forms" |
| `/etc/sudoers.d` `drwxr-x---` on the three VMs; Alpine's `/etc/doas.conf` 0640, `/etc/doas.d` `drwxr-x---` | run: `vm-runs/s1/01-tools.log`:65, :130, :191-193 |
| `lslogins`' one call: a line per name it knows, the name and three spaces for no login, no line and exit 0 for a name it does not know | run: `vm-runs/s1/02-login.log`, `08-lslogins.log` |
| It answers blank, exit 0, with its sources missing, empty or unreadable | run: `vm-runs/s1/09-lslogins-blank.log` |
| Its order: wtmp, then lastlog2, then lastlog; the password lock per account; lastlog read at uid times the slot | read: `login-utils/lslogins.c` v2.41:827, :601, :605, :831 |
| The raw form escapes a blank, `\`, what does not print: so a field holds no space, and a control character in a host name reaches hardn as `\x1b` | read: `libsmartcols/src/print.c` v2.41:620, `include/carefulputc.h`:109 |
| lastlog2's database is `/var/lib/lastlog/lastlog2.db`, and its table is made in the call that writes a row | read: `liblastlog2/src/lastlog2.h`:35, `lastlog2.c`:239 |
| `Cmd.Wait` waits on the process before anything else, whatever `WaitDelay` says | read: Go's `os/exec/exec.go`, `(*Cmd).Wait` |
| `/var/log/wtmp` holds records on Debian 13 and Ubuntu 26.04, as the simulated hosts' does | run: `vm-runs/s1/01-tools.log`:30, :96; `05-wtmp-*.tail` |

## 4. Written, not run (gate 5)

**Run on Linux, 2026-10-30, on the maintainer's go-ahead ("Yes, run them"), at 996659ae** (`vm-runs/s1/gotest.sh`, `13-gotest-*.log`): the tests of `pkg/host`, `pkg/host/hostfake`, `pkg/engine/logview` and `pkg/engine/details`, on the three VMs, as the user and as root through sudo, sudo-rs and doas: 24 runs, all exit 0, the binaries removed and nothing left (`14-gotest-after.log`). `pty_linux_test.go`'s terminal and `TestRunInteractiveLess` on GNU `less` 668 are among them; Alpine skips the latter, BusyBox's being no `less` of that kind. Skipped as root: `TestRealGuardedWalk/another_user's`, `TestRealWriteFileAtomicFailureCleansUp`, `TestReadRealFiles/closed`; as the user: `TestRealWriteFileAtomicOwner`. No other package ran there.

**The first run failed 7 checks** (`13a-gotest-*.log`), as the user on Debian and Ubuntu alone: `TestRealAppendRefuses` and `TestContract`'s real side. Those VMs' umask is 0002, so `t.TempDir`'s directories were group-writable, and the guard refused them, as it should. The same 7 fail on macOS under 0002, with `pkg/host/revert`'s `TestStateRealFiles`, so `TestMain` pins the umask in the three packages, tests alone. As root all passed with no pin, so sudo, sudo-rs and doas each give a umask that keeps a directory trusted. And `ScanLines` reads in a group-writable directory, as `/var/log` is on Ubuntu 26.04 (`drwxrwxr-x root syslog`, `10-forms.log`): its other cases passed there.

**Run by hand on the three VMs, read only, 2026-10-30, with no hardn and no test binary** (`vm-runs/s1/check7.sh`, `check8.sh`): the forms below; GNU `less` 668 as root on item 11's exact command, through sudo and sudo-rs: `--` taken, `!`, `|`, `E`, `:e` and `v` "not available", `s` "not a pipe", Ctrl-C at its prompt then `q` exit 0; non-ASCII as itself under `LC_ALL=C.UTF-8`; an OSC as text on a terminal; `cat` on a pipe, the OSC raw.

**Both readers on the real hosts, 2026-10-30, on the maintainer's go-ahead, at 4d0082a6** (`vm-runs/s1/probe.go`, `readers.sh`, `15-readers-*.log`, compared by hand with `16-truth-*.log`, what each host says with no hardn code): `detect.Detect` through `details.Bounded`, `details.Read`, the JSON document, `logview.Read`, `Each` and `Pager`, on the three VMs, as the user and as root through sudo, sudo-rs and doas. A throwaway program, in no commit, whose guard allows the readers' reads alone and refused nothing; `Pager`'s command was asked for, never run; removed after, the log's size and time unchanged (`17-readers-after.log`). All agree with the host: OS, kernel, uptime, hostname, addresses; each user's grant, as root, against the rules and `sudo -l`; as a user, each user Unknown and "Not root" naming `/etc/sudoers, /etc/sudoers.d` or `/etc/doas.conf, /etc/doas.d`; CPU and load; disk as `df` says, Alpine's Used holding the probe's own 6,502 KiB; memory; Last Login, the same as a user and as root; the log's size, lines, last lines and levels. `/var/log` group-writable on Ubuntu stopped nothing. A `details.Read` took about 1 s on Debian and Ubuntu, `lslogins` nearly all of it, and 5 ms on Alpine. Debian's log shows 20,001 bytes more than it holds, less its newlines: 6,667 tabs, each 4 spaces (`18-log-bytes.log`).

**Still on a VM, the log side:**

- a read that fails as root, which read only cannot make;
- Alpine's less package (item 10's row is [I]);
- `less` under hardn, on `RunInteractive`: the terminal after it, Ctrl-C, through sudo, sudo-rs and doas.

**A bisect:** `TestPlayTouchesNothing` fails at 42fa0423, 594a0300, f23e3263 and 35722788, and so `make check` there (run: `go test` on a `git archive` of each), as their new `pkg/host` files are not yet in `internal/tools/uigallery/play_test.go`'s `realSide`; 7c0a8721 adds them. Each passes its own packages' tests.

**The report, still on a VM, read only:** `lslogins` on a usual host, with the password lock held, and through a real rotation; a global IPv6 address; a real hung process. **No VM exists for:** `zpool` and `zfs` on a ZFS root, in an LXC guest; arm64's `/proc/cpuinfo`; a lastlog2 database that exists.

**The gallery:** on `hostfake` alone, on macOS, as ever: `asUserSys` models a refusal, it does not exercise one. `play system-details` and `play logs` were run by their tests, not by a person at a terminal.

**Not closed, known:** `Read` refuses a link at the path, but `less` opens the path itself, as root, so a link planted after the screen's read is followed by `less` [I, not run]; that needs write access in `/var/log`, which on Ubuntu 26.04 the group `syslog` has (`drwxrwxr-x`, `10-forms.log`). `less`'s handling of bidi characters is its own: `Safe` does not reach it [I, not run]. Ctrl-Z and a SIGTERM to hardn alone: system.md, "Known limits".

## 5. The `host.System` additions, and why

| Method | Why `System` did not have it | Who calls it | Who else changed |
|---|---|---|---|
| `ScanLines(ctx, name, yield)` | `ReadFile` reads a file whole, and the log is never rotated (`pkg/output/audit.go:22`); `TailLines` needs the file in memory. One call serves the last lines and total, `-p`'s pipe form, and the blank guard's "does it hold anything". Items 1, 2 | `logview.Read`, `details`' `readSource`; unit (d)'s `-p` | `host.Real`, `hostfake`; `p4status`' guard passes it, as a read; `realSide` in `internal/tools/uigallery/play_test.go` (§4) |
| `RunInteractive(ctx, env, name, args...)` | `Run` pipes output, gives no terminal, and its environment is exactly `PATH` and `LC_ALL=C` (`pkg/host/real.go`). A pager needs the terminal and a chosen environment. Items 3 to 5 | `logview.Command.Run`; unit (c)'s Open | `host.Real`, `hostfake` (runs nothing); `p4status`' guard refuses it; `realSide` in `internal/tools/uigallery/play_test.go` (§4) |

No other method is new: `/proc/self/status` tells root from another user through `ReadFile` (decision 22). [system.md](../safety/system.md) has each in full.

## 6. The pager's exact command, and its safety

The command is item 11's: `less -- /var/log/hardn.log`, under `LESSSECURE=1 LESSHISTFILE=- TERM=<checked> LC_ALL=C.UTF-8 PATH=/usr/sbin:/usr/bin:/sbin:/bin`. Decision 7 (a) accepts an interactive root program on this screen, asked for by a key. What is closed, and how far it has run:

| Risk, as root | Closed by | Evidence |
|---|---|---|
| A shell, another file or an editor from inside it | `LESSSECURE=1`: `!`, `\|`, `E`, `:e`, `v` say "Command not available" | run as a user: `vm-runs/s1/04-less2-deb13-vm.log`, `04-less2-ubuntu26-vm.log`; as root, through sudo and sudo-rs: `11-less3-*.log` |
| A log written over any file | `s` says "Input is not a pipe" on a file, with or without `LESSSECURE`, and hardn passes a file | run as a user: `04-less2-*.log`; as root: `11-less3-*.log` |
| BusyBox's `less`, whose `s` writes and `E` opens any path, `LESSSECURE` changing nothing | Not run at all: `less --version` must print `less <digits>`, and BusyBox's refuses it; any other answer, any error, is no pager; asked at the draw and again at the key press | run: `TestPager`, 16 answers; `TestCommandRunChecksAgain`; `01-tools.log`:153; `04-less2-alpine323-vm.log`; the write itself read, not run |
| A program named by the environment | `$PAGER` is not read: `Pager` takes no environment | run: `TestPager`; the type |
| `LESS`, `LESSOPEN`, `LESSKEY`, `EDITOR`, `SHELL`, `HOME` reaching it | `RunInteractive` passes `env` and `PATH` alone | run: `TestRunInteractiveExactEnvironment`, each set in the parent |
| A `TERM` that names a path | `safeTerm`, else `vt100` | run: `TestSafeTerm`, `TestPagerTerm` |
| The path taken for an option | `--` | run: this Mac's less 581.2 (`TestRunInteractiveLess`); GNU 668, `11-less3-*.log` |
| What is typed left in root's home | `LESSHISTFILE=-` | [I]: less(1), not run |
| A log's escapes reaching the terminal | no `-r`, so `less` shows them, on a terminal | run: 581.2; GNU 668, `12-less4.log` |
| The same, stdout a pipe, as `hardn \| tee`: `less` acts as `cat`, the bytes raw | `RunInteractive` runs nothing unless stdin and stdout are terminals | run: `TestRunInteractiveNeedsATerminal`, this Mac and the VMs |
| Ctrl-C ending hardn under the pager | SIGINT and SIGQUIT caught for the call | run on pseudo-terminals, macOS and the VMs: item 4 |
| The terminal left raw | modes put back after the child | run: `TestRunInteractiveTerminalModes`, macOS and the VMs |

**What stays open** is §4's "Not closed": a link planted after the read, which needs write access in `/var/log`, the group `syslog` having it on Ubuntu 26.04; `less` under hardn itself, not run. No run of a draw reaches `less` itself but `less --version`, and it is logged, argv and exit, where a host has a run log (item 12).

## 7. Each row's source

`details.Report` is the source of the System Details rows and `logview.Reading` of the Logs rows; the words map them in the gallery until unit (c). "Form" is where the form was seen: a log under `vm-runs/s1/`, or [I] where none printed it.

| Row | Reader's fact | Read from | Form |
|---|---|---|---|
| OS | `OS.OS` | detect's `/etc/os-release`, by `header.OSOf` | [header.md](header.md) |
| Kernel | `OS.Kernel` | `/proc/sys/kernel/osrelease` | [header.md](header.md) |
| Uptime | `OS.Uptime` | `/proc/uptime`, by `header.ReadUptime` | [header.md](header.md) |
| Hostname | `Host.Hostname` | `/proc/sys/kernel/hostname`, by `header.ReadHostname` | [header.md](header.md) |
| `<interface>` | `Host.Addresses` | `ip -o addr show scope global` | `01-tools.log`:50, :115, :178; `inet6` [I] |
| `<user>` | `Users.List`, `User.Admin()`, `Users.Tool` | `/etc/passwd`, `/etc/group`; `users.Elevation`: `/etc/sudoers`, `/etc/sudoers.d`, `visudo -c`, or `/etc/doas.conf`, `/etc/doas.d`, `doas -C` | [check-users.md](check-users.md); modes `01-tools.log`:65, :130, :191-193 |
| "Not root: ..." | `Users.Root`, `Users.NotRead()` | `/proc/self/status`'s `Uid:`, the refusals | `10-forms.log`; the refusals as above |
| Model, CPUs | `CPU.Model`, `CPUs`, `Sockets` | `/proc/cpuinfo` | `01-tools.log`:53-56, :118-121, :181-182; arm64 [I] |
| Load | `CPU.Load` | `/proc/loadavg` | `10-forms.log` |
| `/` | `Disk.Source`, `Type` | `/proc/self/mounts` | `01-tools.log`:52, :117, :180 |
| Used (Disk) | `Disk.Used`, `Total` | `df -kP /`; on ZFS `zfs get -Hp -o value used,available <dataset>` | `01-tools.log`:51, :116, :179; header `10-forms.log`; ZFS [unverified] |
| Pool `<name>` | `Disk.Pool`, `Health` | the mount; `zpool list -H -o health <pool>` | [unverified], no ZFS VM |
| Used (Memory) | `Memory.Used()` | `/proc/meminfo`: `MemTotal`, `MemAvailable` | `01-tools.log`:57, :59, :122, :124 |
| `<account>`, Logins | `Logins.List`, `State` | `lslogins`, one call; the guard: `/var/log/wtmp`, `/var/lib/lastlog/lastlog2.db`, `/var/log/lastlog` | `02-login.log`, `08-lslogins.log`, `09-lslogins-blank.log` |
| Path | `Reading.Path` | `logview.File` | `pkg/logging`'s open, `cmd/hardn/main.go:57` |
| Size | `Reading.Size` | the file at its open, `ScanLines` | `s3/57-log-ubuntu26-vm.log` for the lines |
| Last N of M lines, All N lines, 1 line | `Reading.Total`, `Last` | `ScanLines` | as above |
| Each line, and its colour | `Line.Text`, `Line.Level` | `logview.Safe`, `Show`, `ParseLine` | `s3/57-log-ubuntu26-vm.log` |
| "hardn.yml's logFile, ..." | `logview.Elsewhere` | the caller's `cfg.LogFile` | `pkg/config` |
| Open | `logview.Pager`'s `ok` | `less --version` | `01-tools.log`:22, :88, :153 |

## 8. States

| Where | State | Drawn | Mock or test | JSON |
|---|---|---|---|---|
| Any section | A thing not read (`Unread`) | "Can't read <source>", after the facts read; the rest stays | `screen-unreadable`, `TestSDRows` | null, and one `DETAILS_UNREADABLE` |
| Host and IPs | `ip` answered none (`Addresses` empty) | "No addresses" | `TestSDRows` | `[]` |
| Users | No user listed | "None but root" | `screen-pve9` | `[]` |
| Users | A tool grants it; none does; not established | "sudo" or "doas"; "No sudo" or "No doas"; "Not known" | `screen-debian13`, `screen-alpine323`, `screen-many`, `cli-not-root` | `tool` the tool, `""`, `"unknown"` |
| Users | Not root, rules refused | "Not root: <file>, <file> not read" | `cli-not-root` | one finding |
| CPU | No `model name`, no `physical id` | Model "Not known"; CPUs alone | `TestSDBlankCPU` | null |
| Disk | Not read, or no sizes | "/" alone, and "Can't read df" | `TestSDRows` | null where not read |
| Disk | A pool ONLINE; other; no `zpool` | its health; in the warning's state; "Not known" | `screen-pve9`, `screen-zfs-degraded`, `TestSDRows` | `health` as it says, or `"unknown"` |
| Memory | No `MemAvailable` | Used "Not known" | `TestSDRows` | `usedBytes` null |
| Last Login | `LoginsNoTool` | "Logins  Not known: lslogins isn't installed" | `screen-alpine323` | null, a finding |
| Last Login | `LoginsUnread` | "Can't read lslogins" | `TestSDLastLogin` | null, a finding |
| Last Login | `LoginAt`, a host; none | "Oct 20 11:42 from <host>"; "Oct 20 11:42 on <tty>" | `screen-debian13`, `screen-login-console` | `when`, `tty`, `from` |
| Last Login | `LoginNone`: a blank, no source refusing and one holding records | "None recorded" | `screen-no-logins` | blanks |
| Last Login | `LoginUnknown`: no line, or a blank where a source refuses or none holds records | "Not known" | `TestSDLastLogin` | `unknown: true` |
| Logs | `Lines`, with a pager; with none | the last lines, Open and Refresh; Refresh alone | `screen-long`, `screen-alpine323`, `screen-no-pager` | none |
| Logs | `NoLog`, `Empty`, `Unreadable` | "No log yet", "Empty", "Can't read it: <reason>"; Refresh alone | `screen-missing`, `screen-empty`, `screen-unreadable` | none |
| `-p`, off a terminal | Every line, `Show`n | no heading, no style | `cli-pipe`, `cli-pipe-control` | none |

## 9. Words

gallery-system-details.md §6 is used as it stands, and "§6" in this section and the next is that page's; the words themselves are unit (c)'s and (d)'s to draw, and the gallery still draws them. New here, **decided**:

**Adopted (the maintainer, 2026-10-30):** "Adopt, start (b2)", answering "Adopt the not-root row and the proposed words?": `cli-not-root` as drawn, a user's row "Not known" and "Not root: /etc/sudoers, /etc/sudoers.d not read", and every row of this table. Then, "Adopt, start (c)", answering "Should a user's sudo mark come from the rules alone?": item 16's reading, as the source of [parity-system-details.md](parity-system-details.md)'s `sd-group-no-rule`.

| Where | Words | Why |
|---|---|---|
| A line past the cap, after its 64 KiB | `<cut>` | The sanitiser's own notation, `<0xe9>`, `<U+202E>` |
| `Reading.Reason` for a FIFO or a device, after "Can't read it: " | "not a regular file" | `fs.ErrInvalid`'s own text is "invalid argument", which says neither |
| `Reading.Reason` for a done ctx, after "Can't read it: " | "context canceled", "context deadline exceeded" | ctx's own words; no caller today holds a context that fires |
| `Reading.Reason` for a link at the path, and on the way | "refusing a symlink"; "refusing a symlink in a directory another user can change" | `host.ErrSymlink`'s and `ErrSymlinkParent`'s text, as `reasonOf` gives other errors' |
| `DETAILS_UNREADABLE`'s `<section>`, §6 giving the form and "memory" alone | "os and kernel", "host and ips", "users", "cpu and load", "disk", "memory", "last login" | P-17's words in lower case, as the stand-in has them |
| `DETAILS_UNREADABLE`'s `source` | each source, joined by ", "; "ip addr", "df", "zpool list", "zfs get", "lslogins"; a file's path | The stand-in's sources, and "Can't read <source>" uses them alike |
| The same, and "Can't read <source>", where a tool's rules stopped at no file | "sudo", "doas" | `readTool`'s word for a tool not established, as "Can't read sudo" |
| Errors of the seam and the pager, unit (c)'s to word or show | "stdin and stdout must both be a terminal"; "less is not GNU less" | What each refused; Open reaches them only off a terminal or after a package changed |
| JSON: `users[].tool`, `disk.health` | "unknown" | §6's word, as `host`'s facts use it, where the rules are not read and where no `zpool` answers |
| Users, as a user other than root, two files refused | "Not root: /etc/sudoers, /etc/sudoers.d not read"; "Not root: /etc/doas.conf, /etc/doas.d not read" | §6's form has one `<file>`, the report carries both, and the finding's source joins by ", " too |
| The gallery's, dev-only | "pager-less, adopted", "Its environment", "logview.ParseLine" | Headings of `pager-commands` and `parser`; retire with them |

**For unit (c):** a user's row where the rules are not read is "Not known" (§6's state), since the report no longer says what a group says (`cli-not-root`). `cli-flags` keeps §6's heading "Proposed, if 3.11 is approved", though §3.11's change is approved, narrowed: the words are decided, and unit (d) writes the warnings. "Until 3.11 is decided" and "Parsed and ignored, as today" are retired from §6.

## 10. Defects fixed (gallery-system-details.md §7)

On the new path. origin/main's code keeps them until unit (d) replaces it, as that page's §7 says of 4 to 27.

| # | Defect | Here | Evidence now |
|---|---|---|---|
| Slice | No-op `system-details` flags | **Not here:** unit (d), decision 1 (a). The document it prints is built | `TestDetailsRead`, `json/details.json`, `cli-flags` |
| Slice | Logs dump | The reader's side: the last lines, bounded; every line to a pipe; the whole log in GNU `less`. Unit (c) draws, unit (d) wires | `TestReadLong`, `TestEveryLineAsItReads`, `TestPipeIsEveryLine`, `TestPager` |
| Slice | Broken log parser | `ParseLine`, for the colour alone; a line past 64 KiB is cut and the read goes on | `TestParseLine`, `TestContract`, `parser` |
| 4 | Box fixed at 50 columns | Not here: the screen's. The gallery's layout has no box | `screen-debian13` |
| 5 | The renderer does I/O | `Read` returns data and draws nothing; depguard holds it off `pkg/ui` | `TestRead` in both packages; `.golangci.yml`, `details-through-host`, `logview-through-host` |
| 6 | The screen clears itself; `-p` and the command clear in a pipe | Not here: the readers print nothing. Unit (c) and (d) | `cli-pipe`, drawn |
| 7 | One error loses the report | A section keeps going, and a fact its section's others; "Can't read <source>" says which | `TestOneSectionLeavesTheRest`, `TestSDRows`, `screen-unreadable` |
| 8 | Hard-coded ZFS dataset | The mount's dataset | `TestZFS`, `TestSDZFSDatasetIsRead`; [unverified] |
| 9 | The ZFS branch may never run | Avoided: `zpool status -x` is not asked; the mount's type says ZFS | `TestZFS`; [unverified] |
| 10 | ZFS "Volume" is used/available | Total is used plus available | `TestZFS`, `screen-pve9`; [unverified] |
| 11 | "CPU Freq: 0.00 GHz" | The row is gone (decision 2 (a)) | `screen-debian13` |
| 12 | Memory counts page cache | `MemAvailable`; not known where absent | `TestMemory` |
| 13 | Disk says GB, dividing by 1024³ | Bytes from the reader; GiB in the gallery's words, unit (c)'s | `TestDF`, `screen-debian13` |
| 14 | Last Login root's under sudo; blank with no `lastlog` or a dotless From | `lslogins` for root and each user; a dotless host read; a blank guarded | `TestLogins`, `TestBlankGuard`, `TestSDLastLogin` |
| 15 | No `lscpu`: "Bare Metal", 1 socket | `lscpu` not asked, Hypervisor gone; Model "Not known", no socket count made up | `TestCPU`, `TestSDBlankCPU` |
| 16 | IPv4 alone | Both families | `TestAddresses`; `inet6` [I] |
| 17, 18, 19 | Export's write, its overwrite, its own text | No item (decision 5 (a)); the old code goes with unit (d) | `screen-debian13` |
| 20 | Each draw reads everything again, four times, and recurses | One read of each section's source a draw (the kernel release and `/proc/meminfo` once each, run: a count on the fake, `TestReadWritesNothing`), one `lslogins` call, nothing the report does not show; `/etc/sudoers` twice, as the check reads it (`sudoers.Read`, then `readSudo` in `pkg/engine/controls/users/rules.go`); the loop is unit (c)'s | `TestSDLsloginsCall`, `TestLogins`, `TestReadWritesNothing` |
| 21 | `logFile` diverges from the file hardn writes | `logview.File` always; `Elsewhere` says so. "Check the log file" and the warning are unit (d)'s | `TestFile`, `TestElsewhere`, `screen-elsewhere` |
| 22 | An escape reaches the terminal raw | `Safe`, on and off the screen; no `less` off a terminal; the JSON escapes DEL, C1 and format characters | `TestSafe`, `TestSafeWhole`, `TestControlHostIsSafe`, `TestRunInteractiveNeedsATerminal`, `TestJSONEscapesWhatATerminalActsOn`, `screen-control`, `cli-pipe-control` |
| 23 | `-p` exits 0 on a read failure | Not here: unit (d). `Reading.State` and `Err` give it what it needs | `TestRead`, `TestReadRealFiles`, `cli-unreadable` |
| 24 | A failed read writes an ERROR line | `Read` writes nothing, no run-log line; `-p`'s path is unit (d)'s | `TestReadWritesNothing` |
| 25 | `-p` needs root and `DetectOS` | Not here: unit (d), decision 9 (a). `Read` needs neither | `TestRead` |
| 26 | Empty log no words; missing, the path three times | `NoLog`, `Empty`, and §6's words | `TestRead`, `screen-missing`, `screen-empty` |
| 27 | Three copies of the print | Not here: unit (d) deletes them | none yet |
| 28 | `main()`'s two lines on stdout | Not here: unit (d) | `cli-not-root`, which draws them |
| 29 | The log is 0644 and holds commands' output | **Not corrected by this slice**, as that page's §7 says | none |

## 11. API

```go
package host

const MaxLine = 64 << 10
type Line struct { Text []byte; Cut bool }
func ScanReader(ctx context.Context, in io.Reader, yield func(Line) bool) error
func InteractiveEnv(env []string) []string
func RunBounded(ctx context.Context, sys System, limit, grace time.Duration, name string, args ...string) (Result, error)
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
func Each(ctx context.Context, sys host.System, yield func(Line) bool) Reading
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
type Users struct { List []User; Root detect.Tri; Tool string; Unread []Unread }
func (u Users) NotRead() []string
type User struct { Name string; Grants []users.Grant }
func (u User) Admin() (tool string, t detect.Tri)
type CPU struct { Model string; CPUs, Sockets int; Load [3]string; Unread []Unread }
type Disk struct { Source, Type string; Used, Total uint64; Known bool; Pool, Health string; Unread []Unread }
type Memory struct { Total, Available uint64; AvailableKnown bool; Unread []Unread }
func (m Memory) Used() (used uint64, ok bool)
type LoginsState uint8 // LoginsUnread, LoginsNoTool, LoginsRead
type LoginState uint8  // LoginUnknown, LoginNone, LoginAt
type Logins struct { State LoginsState; List []Login; Unread []Unread }
type Login struct { User string; State LoginState; When, TTY, From string; At time.Time }
func Read(ctx context.Context, sys host.System, f detect.Facts) Report
func Bounded(sys host.System) host.System

package header // pkg/engine/header, exported

func OSOf(f detect.Facts) OS
func ReadHostname(ctx context.Context, sys host.System) Hostname
func ReadUptime(ctx context.Context, sys host.System) Uptime

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

## 12. Decisions of this unit (engineering, stated; say if you'd rather otherwise)

1. `pkg/engine/logview` keeps the name: depguard's `engine-through-host` already fits a reader beside `header`, and a block of its own names the rule, as `header-through-host` does.
2. `yield` returns a bool, not an error: no sentinel joins `pkg/output`'s registry, and a caller's write error stays the caller's.
3. `host.MaxLine` is 64 KiB, origin/main's own limit: `MaxLast` such lines bound `Read` at 16 MiB of the file's bytes and about 96 MiB once shown, `Safe` making a bad byte 6, so about 112 MiB while both are held (run: `TestShowBound`, `TestSafeWhole`). A line that long is not read in a log, and writing such a log needs root, so the figure is stated and not capped.
4. A cut line reaches `yield` when its first `MaxLine` bytes are known, with no count of what went: a count would need the whole line first, which costs a binary file's whole length for one look.
5. `Line.Cut` is a field and the mark is `logview`'s, so the host says no words and no text can pass as the mark to `host`'s callers.
6. A cut inside a character shows that character's bytes: true, and rare.
7. It reads no further than the size at the open, so a writer that keeps appending cannot keep `-p` reading; so a `/proc` file reads as empty, and `ReadFile` is for those.
8. `RunInteractive` returns an error alone: its output went to the terminal, so a `Result` would hold nothing.
9. It logs the argv and the exit where a host has a run log, as every command is logged, and not `env`.
10. SIGQUIT is caught with SIGINT, as Go's default ends hardn with a stack dump on Ctrl-\ too; `less` ignores it [I].
11. ctx done sends SIGTERM first, as SIGKILL leaves the terminal raw; no caller today holds a context that fires.
12. The terminal's modes are put back after the child, which the brief did not ask: the seam's own SIGKILL can leave them raw, and the next prompt would then read blind. So `pkg/host` asks termios itself, through `x/sys/unix`, beside the guarded walk's use, and past "only `pkg/ui/term` looks at the terminal": to give a child the terminal, not to draw (`.golangci.yml`, both rules' comments).
13. `Read` does not call `Pager`: `-p` needs no `less --version`, and `TERM` is the caller's at the time.
14. `Pager` reads `Result.Combined`, as the VMs' log merged the streams, so the check holds to what ran; BusyBox's words match on neither.
15. `less 581.2`, macOS's, is refused: the digits end at a space or the line. It is no release target, and fails closed.
16. `less --version` is bounded at 5 s and left behind a second past it, as a draw waits for it; a timeout is no pager.
17. `Safe` also marks noncharacters, which the stand-in kept; no mock holds one.
18. `Elsewhere` returns the path `Safe`, as hardn.yml may be `./hardn.yml` (`pkg/config/config.go:210`) and both its callers show it.
19. `pkg/engine/details` is the name, beside `header`: depguard's `engine-through-host` fits, and a block names its own rule, `details-through-host`, which keeps it off `pkg/ui` and `pkg/output`.
20. `users.Elevation` is exposed, not `tool` and `decide`: three methods, and the row's shells, locks and findings stay private.
21. Facts keep their section's others, as the header's do, past the stand-in's whole-section rule: decision 2 (a) has "each row reads a fact or says it could not".
22. `/proc/self/status` tells root from another user, not a new `System` method: a method means p4status' guard, the fake and every other implementer, for one read of a file `ReadFile` reaches.
23. A user's grant with rules unread is Unknown, not a group's guess: a group names nothing a rule does not, and the stand-in's "sudo" there would be the guess decision 2's rows avoid.
24. A rule file is reported only where a tool that matters read it: on Alpine, whose `/etc/sudoers.d` also refuses (`01-tools.log`:193), sudo is not installed, so it is not named.
25. The guard is stricter than the adopted words' letter: any source that exists and refuses leaves a blank Unknown, whatever another holds, since `lslogins` answers blank for a login held only in the one it cannot read. So wtmp holding records with `lastlog` refused reads Unknown too, the safe direction of a guard. No adopted mock depends on it: the simulated hosts' sources open, and the VMs' wtmp and lastlog are 0664 (`01-tools.log`).
26. A lastlog2 database with any bytes counts as holding records: hardn has no SQLite reader, and the table is made by the write of a row (`lastlog2.c`:239), so empty after a removal is the false None left [I]. No VM has the file.
27. A blank's guard costs one look per source, only where there is a blank: a host where all have logged in reads nothing more.
28. A name not askable is not asked, so one odd account in `/etc/passwd` does not turn the whole section unread.
29. A failed `zfs get` or `zpool list` stays a finding though `df` answered, as the pool's figures were not what was asked; not installed is no finding, only "unknown".
30. `lslogins`' 20 s is the section's, not per account: a held lock past one wait reads "Can't read lslogins", and the next Refresh tries again.
31. Commands run one after another: running them at once would save time on a hung host alone.
32. Report values are as the host gave them, not `logview.Safe`d, as a script should read what the host holds. **JSON:** `encoding/json` escapes C0 alone, so `marshal` writes DEL, C1 and format characters as `\uXXXX`, the string's value unchanged (run: `TestJSONEscapesWhatATerminalActsOn`), for every document, one flag having one contract. **The screen:** the rows' `clean` (`pkg/ui/component/ctx.go:283`) drops controls and leaves a format character such as U+202E, and changing it moves every screen, so unit (c) draws report values through `logview.Safe`, on its off-terminal path too. `lslogins`' raw form has already escaped `From` and `TTY`.
33. `DETAILS_UNREADABLE`'s code is `pkg/output`'s, beside `CONFIG_DEPRECATED_VALUE`: `Build` makes the finding.
34. `hostname` and `os` stay in `host`, as the gallery has them, and `Run.Hostname` stays the caller's to set from `Report.Host.Hostname`.
35. The retirement comes before the moves, each its own commit, so that a golden's diff has one cause.
36. The gallery's rows put each fact first and `Can't read <source>` after, so a fact keeps its place in the section and unit (c) can move the rest; the row is the source's, not one row per section as the stand-in had.
37. `Pager` is asked only where there are lines: a draw with nothing to open runs no `less --version`.
38. Two refused files are one row, joined by ", " (§9, adopted): the row names what a user cannot read, and two rows would repeat "Not root".
39. The gallery's `TERM` is a constant, `xterm-256color`, as a golden's must be; unit (d) passes hardn's.
40. `asUserSys` refuses in the gallery: `hostfake` has no user to refuse, and the reader's own tests refuse the same way.
41. `pager-commands` draws the argv in its rows and `Env` in its own section, as the whole command would be cut at a row; a Callout, which wraps, draws it whole in `open`.
42. The blank no source backs has a test, not a mock (`TestSDLastLogin`): no adopted host has one, and unit (c) retires the mocks.
43. No new command is taught to `reads` (`engine.go`) for `less --version`: `assertOnlyReads` allows that one argv, and `run-interactive` fails it, so a pager that ran would fail the test.
44. `RunInteractive` refuses off a terminal, not `Pager` taking a flag: a caller's flag can be wrong, the seam reads hardn's own files, and it holds for every caller.
45. `Command.Run` repeats `less --version`, not unit (c) calling `Pager` at the key press: the check then cannot be forgotten. It leaves a moment between the check and the exec, which needs root and is what a package manager's own run would be.
46. `Each` is exported, not the error's mapping: the pass, its count and the words stay together, and `-p` needs no second read. `Reading.Reason` stays the reader's, with `notRegular`, as the row and `-p`'s stderr are one set of words, where `details` leaves its words to the screen.
47. `details.Read` takes the header's three reads, not the probe: `TestReadWritesNothing` holds that the rest is gone, and the header's own output does not change.
48. `RunBounded` is in `pkg/host`, beside `Run`: two readers use it, and neither should import the other.
49. `Users.Tool` is doas where detect found doas and not sudo, else sudo: where both are installed a No means neither grants, so "No sudo" is true, and it is the rarer form's word kept short. No mock has both; the gallery's stand-in rule said "No doas" there.
50. Kept with no live consumer past the gallery: `logview.Entry`'s `Time` and `Message`, for the `parser` mock, and `LevelInfo`, `LevelSuccess` and `LevelInstalled`, the values `Line.Level` can hold. Unit (c) reduces `ParseLine` to the level when that mock retires. Gone: `Logins.Sources`, `User.UID`, `Users.RootErr` and `onSearchPath`, with no use a later unit is named for.

## 13. Not in this unit

- **Unit (c):** the screens on `screenkit`: the rows and words now in the gallery; "Last N of M lines" and the size's words; Open, what it does with a pager's error, a refusal off a terminal or a less no longer GNU's among them (§9), `Flush`; a section that holds code; report values through `logview.Safe` before a row, and on the uncapped off-terminal path (decision 32); `ParseLine` reduced to the level with the `parser` mock (decision 50).
- **Unit (d):** main menu 9 and 10; `-p`'s forms and exit statuses on `logview.Each`, the `logFile` warning, reading `TERM` from hardn's environment for `Pager`; `--output json`, its `Run{Mode: ModeCheck, Hostname, Facts, Details}`; `hardn system-details`' host with no run log, and its `detect.Detect` through `details.Bounded`; the legacy code's deletion.
- **(b2) Parity,** gallery-system-details.md §9. *Built in `next/s1-parity`: [parity-system-details.md](parity-system-details.md), each row against §7's fact.*
- **On a Linux VM:** §4's lists. `p4status`' guard alone changed for the new methods (`ScanLines` passed, `RunInteractive` refused; [vmcheck.md](vmcheck.md) §2).

## 14. Size

`git diff --numstat 7bc756c7` (run): non-test Go outside the gallery 2,085 lines added and 31 removed: `pkg/engine/details` 911 (192 of them whole-line comments), `pkg/engine/logview` 422 (142), `pkg/host` 382, `pkg/host/hostfake` 49, `pkg/output` 244, `pkg/engine/controls/users` 52, `pkg/engine/header` 9, `internal/tools/p4status` 16. The gallery 471 added and 1,104 removed (`systemdetails.go` 1,598 to 1,112 lines, `logs.go` 895 to 748). Tests 3,535 lines added outside the gallery (`details_test.go` 1,384, `interactive_test.go` 498, `logview`'s 818), the gallery's 211 added and 180 removed. Testdata: 2 new JSON goldens and one line in `codes.golden`; 20 of the gallery's deleted, 32 changed (item 30).

**Against Backup's unit (b)** (lines added, a proxy, not time or cost): its control unit added 705 non-test lines outside the gallery after its review's fixes ([control-backup.md](control-backup.md) §12), 776 in gallery-environment.md §10's count from the merges, and about 1,600 of tests. So this unit is about **2.96** of the first and **2.69** of the second, and about **2.21** of its tests. Far over the guideline of about 500 changed lines, and larger than gallery-system-details.md §10 expected of units (b) to (d) together, which it called smaller than Backup's: that section measured the stand-ins, 431 lines, and named the seams and the login source as unmeasured. What the stand-ins did not hold is the size: two seams on every `System`, a blank guard, bounds on hung commands, the rules as Check reads them, and the JSON form. The commits split it so.
