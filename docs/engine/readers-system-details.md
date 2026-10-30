# The readers of System Details and Logs (`pkg/engine/logview`, `host.System`'s `ScanLines` and `RunInteractive`)

> **Unit:** refactor plan slice 1 (System Details + Logs, both read only), unit (b), `next/s1-readers`, from `next/s1-gallery` at 7bc756c7. It builds on unit (a) ([gallery-system-details.md](gallery-system-details.md)), whose eleven decisions the maintainer adopted on 2026-10-30 ("Adopt all eleven"). Decisions implemented here: 6 (a) (the log's file), 7 (a) (GNU `less` alone), and 8 (a)'s and 10 (a)'s needs (the last lines, the pipe form). This page holds the log side; System Details' reader is the unit's next part, and the gallery moves onto both after it. No V-number holds the defects it fixes (gallery-system-details.md §7: the slice's "Logs dump" and "Broken log parser", and the reader's side of rows 21, 22 and 26).
> **Status:** built and tested on macOS, in process, on `hostfake` and on real files, real children and pseudo-terminals in `t.TempDir()` (run). **Nothing has run on Linux or as root**, and hardn itself has not run: §4. There is no control on the engine, no `Check`, no plan and no flow, as neither screen has a main-menu row or a change. Nothing is wired into `cmd/hardn`, `pkg/menu`, `pkg/system`, `pkg/cmd`, `pkg/logging`, `pkg/adapter` or `pkg/config`; no `--output` flag and no screen package here (units (c) and (d)). No golden changed (`git diff --name-only 7bc756c7 -- '*testdata*'` is empty at this page's commit).

The screens of unit (c) and the wiring of unit (d) should only draw and wire. So the host gains two seams, `ScanLines` and `RunInteractive`, and `pkg/engine/logview` reads hardn's log through them as data, with nothing rendered, as `pkg/engine/header` reads the header's facts.

## 1. Why

origin/main prints the whole log at once, 20,000 of 20,000 lines, read whole into memory, its bytes raw, from `cfg.LogFile`, which hardn has never written (gallery-system-details.md §2.7, §2.8). Its parser stops silently at a line over 64 KiB and reads the clock as the level, and nothing live calls it (§2.10). Unit (a) drew the replacement on the gallery's stand-ins, `readLog`, `safeLine`, `parseLogLine` and `pagerFor` (§8), which read the whole file and go by a link to tell BusyBox's `less`. `host.System` had no call for the end of a file and none that gives a child the terminal: `Run` pipes output (`pkg/host/real.go`, `start`).

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

## 3. Facts relied on

| Fact | Source |
|---|---|
| GNU `less` 668 on Debian 13 and Ubuntu 26.04; BusyBox's through a link on Alpine 3.23, refusing `--version` | run: `_support/hardn-audit-2026-10-16/vm-runs/s1/01-tools.log` (its command merges stderr, so the check reads `Result.Combined`) |
| `LESSSECURE=1` turns off `!`, `\|`, `E`, `:e`, `v` on GNU's and changes nothing on BusyBox's | run, as a user: `vm-runs/s1/04-less2-*.log` |
| hardn's lines are `DATE TIME LEVEL: message` | read: `pkg/logging/logging.go:46`, `:78-133`; run: `vm-runs/s3/57-log-ubuntu26-vm.log` |
| exec gives a caught signal its default action and keeps an ignored one ignored | run here: the pseudo-terminal tests above; POSIX exec |
| `signal.Notify` on a signal started ignored stops ignoring it; `signal.Stop` puts it back | read: os/signal's doc; `signal.Ignored`'s use, `pkg/ui/prompt/reader.go:311` |
| `less` shows a log's OSC as text with no `-r` | run: `TestRunInteractiveLess`, this Mac's 581.2; on GNU 668 [I] |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root. On a VM, read only:

- the new tests in `pkg/host`, `pkg/host/hostfake` and `pkg/engine/logview`, `pty_linux_test.go` among them (vetted by `make check-cross` alone), and `TestRunInteractiveLess` on GNU `less` 668;
- `less --` and non-ASCII under `LC_ALL=C.UTF-8` on Debian 13 and Ubuntu 26.04; Alpine's less package (item 10's row is [I]);
- `less` as root under hardn: `LESSSECURE`, `q`, Ctrl-C at its prompt, the terminal after it, through sudo, sudo-rs and doas;
- `ScanLines` on `/var/log/hardn.log` and `/var/log/wtmp` as root and as a user; a read that fails as root.

**Not closed, known:** `Read` refuses a link at the path, but `less` opens the path itself, as root, so a link planted after the screen's read is followed by `less` [I, not run]; that needs write access in `/var/log`. `less`'s handling of bidi characters is its own: `Safe` does not reach it [I, not run]. Ctrl-Z and a SIGTERM to hardn alone: system.md, "Known limits".

## 5. Words

gallery-system-details.md §6 is used as it stands; the words themselves are unit (c)'s and (d)'s to draw, and this unit draws none. New here, **proposed**:

| Where | Words | Why |
|---|---|---|
| A line past the cap, after its 64 KiB | `<cut>` | The sanitiser's own notation, `<0xe9>`, `<U+202E>` |
| `Reading.Reason` for a FIFO or a device, after "Can't read it: " | "not a regular file" | `fs.ErrInvalid`'s own text is "invalid argument", which says neither |
| `Reading.Reason` for a link at the path, and on the way | "refusing a symlink"; "refusing a symlink in a directory another user can change" | `host.ErrSymlink`'s and `ErrSymlinkParent`'s text, as `reasonOf` gives other errors' |

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
```

## 8. Not in this unit's log side

- **The gallery:** its `readLog`, `safeLine`, `parseLogLine` and `pagerFor` stay until the gallery moves onto this package (this unit's last part). `p4status`' guard alone changed for the new methods (`ScanLines` passed, `RunInteractive` refused; [vmcheck.md](vmcheck.md) §2).
- **Unit (c):** the screen, its "Last N of M lines" and the size's words, Open, what it does with a pager's error, `Flush`.
- **Unit (d):** `-p`'s forms and exit statuses, the `logFile` warning, reading `TERM` from hardn's environment for `Pager`.
