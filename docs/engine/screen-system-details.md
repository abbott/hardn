# The System Details and Logs screens on their readers (`pkg/screens/systemdetails`, `pkg/screens/logs`, `screen.SystemDetails`, `screen.Logs`)

> **Unit:** refactor plan slice 1 (System Details + Logs), unit (c), `next/s1-screen`, from `next/s1-parity` at 3d58430a: "Screen on the design system and prompt kit" (plan §2, "Phases 3–5 together", §3.22); [gallery-system-details.md](gallery-system-details.md) §9: "the screens on the design system, retiring these mocks, with the off-terminal forms". It builds what the maintainer adopted as the gallery's mocks on 2026-10-30 (gallery-system-details.md §5, the eleven decisions and the Made here calls, with §6's words), drawn from the readers ([readers-system-details.md](readers-system-details.md), its words adopted 2026-10-30), and makes `TestParity` drive them ([parity-system-details.md](parity-system-details.md) §7). Decisions implemented: 2 (a), 3 (a), 4 (a), 5 (a) on the report; 6 (a), 7 (a), 8 (a), 10 (a) on the log; 11 (a), the words; every Made here call. Decisions 1 and 9 are unit (d)'s.
> **Status:** built and tested on macOS, in process: both screens through their entry points on the recording fake (`hostfake`), every host state the readers' tests and the gallery cover, every item, refusal, redraw, plan only and navigation; `TestParity` through the screens; the gallery's playable mode on its simulated hosts. No real terminal, no hardn binary and no host were used; what was not run is written, not run (gate 5; §4). *Since `next/s1-replace`, §7:* both screens are wired into `cmd/hardn` and the main menu; origin/main's two screens and their path are deleted, their goldens frozen under `pkg/screens/systemdetails/testdata/legacy/`. Words: the decided ones used exactly; two rows proposed, a pager's failure as `host` says it and the invalid key's under P-25 (c) (§5, §11). Renders for review: `_support/hardn-audit-2026-10-16/s1-details-logs/screen/` (not committed).

## 1. Why

origin/main's System Details screen loses the whole report at one collector's error, reads everything four times a draw and recurses at each option, draws through a renderer that does I/O, writes an export as root; its Logs screen prints the whole file at once, from a path hardn has never written, and sends a line's escape to the terminal raw (gallery-system-details.md §2, §7). Unit (a) drew the replacement, which the maintainer adopted; unit (b) built the readers; unit (b2) recorded the parity table against their facts. This unit draws the adopted screens from the readers, so that one System Details screen and one Logs screen exist, on the real code, for unit (d) to call from hardn's main menu, `hardn system-details` and `-p`.

## 2. Guarantees

### Where the screens live

1. **The drawing in `pkg/ui`, the reading and the running outside it.** `pkg/ui/screen` holds the view-models: `screen.SystemDetails` (seven sections of rows; `SubMenu`, `Report`) and `screen.Logs` (the reading in words; `SubMenu`, `Terminal`, `OpenKey`). `pkg/screens/systemdetails` fills the first from `details.Read`, `pkg/screens/logs` the second from `logview`, binds the items and runs Open. **Two entry points each:** `Run`, the screen; `Print`, the form with no menu (§6).
2. **Through `host.System` only.** Both packages reach the host through the `host.System` they are given and the readers, hardn's environment and hardn.yml only through what their caller gives (`Options.Term`, `Options.LogFile`), and draw through `pkg/ui`: depguard's `screens-through-host` covers them (run: lint 0 issues). `pkg/ui/screen`'s two files import nothing of the host (`ui-renders-only`; run). No engine, no plan, no journal: there is nothing to apply. `TestPlayTouchesNothing` names the four packages (run).

### The System Details screen (decisions 2 (a), 3 (a), 4 (a), 5 (a))

3. **Each round reads the host once and draws the report whole** (`nav.Prompter.Run`'s draw; `details.Read`): one `read` of each file, one `ip`, one `df`, one `lslogins` a draw, where origin/main's reads four times a draw (run: `TestDrawsOnce`, `TestParity`'s `holdOneRead`). **Drawing writes and runs nothing but reads, live and under plan only**, which draws the same, as `Options` has no mode (run: `TestScreenHosts`, nine hosts; `TestPlanOnly`; `TestParity`, 22 cells).
4. **P-17's seven sections and the rows of §5:** OS and Kernel, Host and IPs, Users, CPU and Load, Disk, Memory, Last Login; 44 lines to the prompt, 43 on Alpine, 78 on `many`, at 80×24 and 80×40 alike, its top scrolling off (run: `TestScreenHosts`). No Export item (decision 5 (a)); no pause.
5. **A section that cannot be read says which and what, and the rest stays** (Made here): "Can't read /proc/meminfo" in Memory, the other six kept (run: `TestSDUnreadableKeepsTheRest`, `TestParity`'s `sd-error`, `holdKept`). A user's row with the rules unread is "Not known", and "Not root: /etc/sudoers, /etc/sudoers.d not read" under them (run: `TestSDNotRootNamesTheRules`). Logins on Alpine: "Logins  Not known: lslogins isn't installed" (run: `TestSDLastLogin`). Where `ip` answers only lines hardn cannot read, "Can't read ip addr" alone: "No addresses" is not established (run: `TestSDRows`, `TestSDReadsAddrs`).
6. **Report values go through `logview.Safe` before a row**, on the off-terminal path too (readers-system-details.md §12 decision 32): the rows' `clean` leaves a format character such as U+202E. One pass, `safeRows`, over every key, value and note (run: `TestSafe`, on the screen and in `Print`; mutation-checked).
7. **Item 1, Refresh, draws again** from a fresh reading, under what the last round left (run: `TestRefresh`). 0 returns (`Run` returns nil); q quits (`nav.ErrQuit`); the end of the input and Ctrl-C abort (`prompt.ErrAborted`), writing nothing (run: `TestNavigation`; `TestInterrupt`, SIGINT sent while the screen reads a pipe, on macOS; it builds for Linux, not run there). An invalid key is refused under the prompt, no draw, no read (`TestParity`'s `sd-invalid`).

### `hardn system-details`, `Print` (decision 4 (a), Made here)

8. **To a terminal, the report as the screen draws it, with no menu and no prompt** (`cli-terminal`, byte for byte the mock's). **Off one, plain: no colour, no clear, no prompt, each value whole however long** (`cli-pipe`, `cli-pipe-cut`; run: `TestPrint`, `TestLongValue`, `TestSystemDetailsWhole`; `TestParity`'s `holdWhole`, in every report cell). This is the new path through the components, `component.OffTerminal` and `Ctx.Whole` (DEV-067). One read, no write (run: `TestPrint`). `Print` takes what `Run` takes, with `Out` and `Env`; `main()`'s two lines and the flags are unit (d)'s (§7).

### The Logs screen (decisions 6 (a), 7 (a), 8 (a))

9. **The file hardn writes, whatever hardn.yml says:** "Path /var/log/hardn.log", and "hardn.yml's logFile, <path>, isn't used" where `Options.LogFile` names another (run: `TestElsewhere`; `TestParity`'s `logs-other-path*`).
10. **Opens on the last lines as written, as many as the terminal holds:** each round reads the log once (`logview.Read`), and only where there are lines asks `logview.Pager` once; "Last N of M lines", "All N lines" or "1 line", measured as they draw, a wrapped line counting each of its rows, the newest line always shown; the prompt within the terminal's rows (run: `TestHeights`, `TestLogsFit`; `TestParity`'s `holdFitted`, at 80×24). The lines are code (DEV-066): no bullet, from the label column, typographic characters kept in both glyph sets, never cut, breaking after the wrap glyph; WARNING and ERROR in the warning's colour, the rest muted (run: `TestCodeBlock`). A blank line is a row: where it is all the tail holds, its header stays (run: `TestBlankNewestLine`, `TestLogsBlankLine`). A line lays out in time its length: 64 KiB of bytes that are not UTF-8, 393,216 cells, draws in about 0.12 s (run: `TestCodeLongLine`, on hostfake).
11. **The missing, empty and unreadable states have their words and Refresh alone:** "No log yet", "Empty", "Can't read it: <reason>" (run: `TestScreen`, nine hosts). No `less --version` where there is nothing to open (readers §12 decision 37).
12. **Open, GNU less alone, by its key alone, no Open on BusyBox's or with no less:** `logview.Pager`'s command, `less -- /var/log/hardn.log`, `LESSSECURE=1 LESSHISTFILE=- TERM=<term> LC_ALL=C.UTF-8`, `Options.Term` checked by `Pager`, the program and the rest of the environment none of hardn's (run: `TestOpen`, with `PAGER`, `LESSOPEN` and `LESS` set; `TestTerm`, `TestOpenByItsKeyAlone`; `TestParity`'s pager row, the command Open ran on a twin host, `PAGER` set). `Command.Run` asks `less --version` again before it runs (readers §12 decision 45). A refusal or the pager's failure is said, and the screen goes on: "stdin and stdout must both be a terminal", "less is not GNU less" (readers §9), each in those words alone; another error as `host` says it, made safe, which is proposed (§5, §11) (run: `TestOpenRefused`). The words stay on the terminal: that draw fits the lines under them (run: `TestOpenWordsStayVisible`). The prompt's `Flush` is not called by Open: `prompt.Reader.Ask` flushes at the next prompt, so a second q typed at less's end does not reach the menu (run: `TestOpenFlushes`, mutation-checked; decision 5).
13. **Refresh and leaving** as on System Details (run: `TestRefresh`, `TestNavigation`, `TestInterrupt`). Plan only draws the same, Open too, as nothing is written: `Options` has no mode (read; `TestPlanOnly` holds only that no field is named for one). That the screen opens under plan only is unit (d)'s hook (§7).

### `hardn -p`, `Print` (decision 10 (a))

14. **To a terminal:** title, File and the last lines the terminal holds, no menu and no prompt, two rows left for the command above it and the shell's prompt (`cli-terminal-tail`; run: `TestPrintTerminal`, `TestLogsTerminal`); `LogFile` is said in a row where its caller gives it, and `-p` alone gives none (§7). No log or unreadable: nothing on stdout. **Off one:** every line as it reads, safe to show, no heading, no style (`cli-pipe`, `cli-pipe-control`; run: `TestPrintOffTerminal`). **`Printed`** gives the caller what stderr and the exit status need without a second read: `Stderr(tool)`, "hardn: no log yet: <path>", "hardn: can't read <path>: <reason>", "Run it with <tool>." where the read was refused and `tool` is given; `ExitCode()`, 1 where unreadable, else 0 (run: `TestPrintStates`). Writing stderr, setting the status and choosing `<tool>` are unit (d)'s (§7).

### Misuse and proof

15. `Run` and `Print` without a Prompter's In and Out, or Out, or a System, wrap `fs.ErrInvalid`, writing nothing (run: `TestRunNeeds`, `TestPrint`, `TestPrintStates`). These are developer text, not words of §5.
16. **Mutation-checked** with `go test -overlay` (run, not committed). The first draw of the code: a draw or `Print` writing fails `TestScreenHosts` and `TestInterrupt` or `TestPrint`; on Logs `TestScreen` and four more, `TestPrintOffTerminal`, `TestPrintTerminal`; two `details.Read` a draw `TestDrawsOnce`, `TestRefresh`, `TestSDLsloginsCall`; two `logview.Read` a draw `TestScreen`, `TestOpen`, `TestRefresh`, `TestNavigation`; the pager asked with no lines `TestScreen`, `TestRefresh`, `TestOpenByItsKeyAlone`; Refresh running the pager `TestOpenByItsKeyAlone`, `TestRefresh`; Open at each draw seven tests; the second check skipped `TestOpen`, `TestOpenRefused`; no `--` and no `LESSSECURE` `TestOpen` and `TestParity`; raw TERM `TestTerm`; `Safe` off for rows, pager errors and Elsewhere, `TestSafe`, `TestOpenRefused`, `TestElsewhere`; 19 others, on exit codes, `Whole`, the code section, the fit, the shell's rows and the flush. Since the unit's review, each of which survived before it: `Safe` off for stderr's reason and tool `TestPrintStates`, for the screen's reason `TestSafeOnATerminal`, for the Disk rows `TestSafe`; `$PAGER` or `LESSOPEN` taken from hardn's environment `TestOpen`; a failed write not ending the pass `TestPrintStates`; `truncate` ignoring `Whole` `TestWhole`; a refusal as root saying "Not root" `TestSDRootRefusedSaysCantRead`; KiB from 1000 bytes `TestSizeWords`. On `TestParity`: parity-system-details.md §2's list.

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| `details.Read` keeps each fact of a section whatever else fails, writes nothing, runs only reads, each command under a bound | readers-system-details.md items 13 to 24; its `TestReadWritesNothing`, `TestBounds` (run there) | §2.3, §2.5 |
| The forms the fake answers are the VMs' | `vm-runs/s1/01-tools.log`, `02-login.log`, `08-lslogins.log`, `10-forms.log`; ZFS's [unverified], no ZFS VM; `inet6` [I] | every host of the tests |
| `logview.Read` keeps at most `MaxLast` lines, safe to show, on a host with no run log; `Each` gives every line in bounded memory | readers-system-details.md items 1, 7, 8; run there | §2.10, §2.14 |
| `Pager` is GNU less alone, failing closed, TERM checked; `Command.Run` checks again | readers-system-details.md items 10, 11; decision 45; `TestPagerBound` (run there) | §2.12 |
| `RunInteractive` refuses off a terminal, and nothing reads the terminal while the child has it | readers-system-details.md items 3 to 5; `TestRunInteractiveNeedsATerminal`, `pty_*_test.go` (run there, Linux and macOS) | §2.12 |
| `prompt.Reader.Ask` flushes typeahead before it asks | `pkg/ui/prompt` (read); `TestOpenFlushes` here (run) | §2.12, decision 5 |
| `hardn system-details` has no root check, the main menu and `-p` differ | `cmd/hardn/main.go:84`, `:148` (read); gallery-system-details.md §2.1, decision 9 | `cli-not-root`, §7 |

## 4. Written, not run (gate 5)

Nothing here has run on Linux, as root, or at a person's terminal. For the maintainer's VM runs, now unit (d) has wired it, on Debian 13 (sudo), Ubuntu 26.04 (sudo-rs) and Alpine 3.23 (doas), and by hand on Proxmox VE 9 and a ZFS root where there is one, each host put back after:

1. **hardn itself:** main menu 9 and 10 open the screens under `sudo hardn`, `doas hardn` and at a root login, under `-n` too; 0 back to the main menu; `find /etc /var/backups /var/lib/hardn -newer <stamp>` finds nothing after each (`lslogins`' `/etc/.pwd.lock` aside, which is not hardn's: readers-system-details.md item 24).
2. **`less` under hardn:** Open on Debian 13 and Ubuntu 26.04: the whole log; `!`, `|`, `E`, `:e`, `v` not available, `s` not a pipe; q back to the screen, the terminal as it was, a second q typed at its end not reaching the menu; Ctrl-C inside it; `TERM` absent or odd; the log removed between the draw and the key, where `less` exits 1 [I] and the screen says so (§5, §11). On Alpine with BusyBox's, no Open item.
3. **A read that waits:** `lslogins` holding the password lock, 15 s (`lckpwdf`, readers-system-details.md item 23; read, not run), with the screen waiting on its draw: the screen says nothing while it waits; the next Refresh tries again; Ctrl-C while it waits ends hardn by Go's default (read: readers-system-details.md item 4), no terminal mode to put back.
4. **A person's real terminal:** the report's top scrolling off at 24 rows; the last lines fitted at 24 and 40 rows and at 60 columns; the wrap glyph in both glyph sets; colour on a dark and a light theme; resize between two draws, which is expected not to be tracked: the height is the `Prompter.Ctx`'s, read on entering the screen and fixed for the visit (§7; read), so after a shrink the top scrolls off with the prompt still last, and after a grow fewer lines show than fit until the screen is left and entered again.
5. **`hardn system-details > file`** as root and as a user other than root (`main()`'s lines on stderr): the file holds the report alone, each value whole, no escape; as a user, "Not root: ...", "Not known".
6. **`hardn -p`** to a terminal, to a pipe, with no log (as a user other than root; as root it is empty, §7), a directory there, and tighter than 0644 as a user other than root: unit (d)'s forms and statuses, decision 10 (a). The first run must confirm the lines origin/main's `-p` printed ahead of the log are gone: the config line, the clear sequence, the `~~~` line, the OS line. They were read, not run, and nothing but gallery-system-details.md §2.9 and git now holds them (§10).
7. **ZFS:** `zpool` and `zfs` on a real pool, DEGRADED drawn in the warning's state: [unverified], no VM.

## 5. Words

The decided words used exactly: gallery-system-details.md §6, decided with decision 11 (adopted 2026-10-30), though some of its Source cells say "proposed" (its §5 says every word is decided unless its row says otherwise); readers-system-details.md §9 (adopted 2026-10-30). **Two rows below are proposed,** neither decided by those pages: a pager's failure, new here and open in §11, and the invalid key's, nav's under P-25 (c). Misuse and a failed write are developer text.

| Where | Words | Source |
|---|---|---|
| Title bars | "System Details"; "Logs" | **decided**; the main menu's labels |
| Heading, prompt | "Select an option"; "Enter selection" | **decided**, Backup's |
| System Details sections | "OS and Kernel", "Host and IPs", "Users", "CPU and Load", "Disk", "Memory", "Last Login" | **decided**, P-17's, capitalised |
| Rows | "OS", "Kernel", "Uptime", "Hostname", `<interface>`, "Model", "CPUs" ("4, 1 socket", "48, 2 sockets", "4"), "Load (1, 5, 15 min)", "/" ("/dev/vda1 ext4"), "Used" ("4.2 of 38.6 GiB (11%)"), "Pool rpool" (ONLINE, DEGRADED), `<user>` (sudo, doas, "No sudo", "No doas"), `<account>` ("Oct 20 11:42 from 192.0.2.10", "Oct 20 11:42 on tty1", "Oct 20 2025 from 192.0.2.10", "None recorded", "Not known"), "Logins" | **decided** (§6) |
| States | "No addresses", "None but root", "None recorded", "Not known", "Not known: lslogins isn't installed", "Can't read <source>", "Not root: <file>, <file> not read" | **decided** (§6; readers §9) |
| Items | "Refresh" "Read the host again"; "Open" "The whole log in less"; "Refresh" "Read the log again"; "Return" | **decided** (§6) |
| Logs sections | "File"; "Last N of M lines", "All N lines", "1 line" | **decided** (§6) |
| Logs rows | "Path", "Size" ("49 KiB", "<n> bytes", "<n> MiB"), "No log yet", "Empty", "Can't read it: <reason>", "hardn.yml's logFile, <path>, isn't used" | **decided** (§6) |
| The seam's and pager's refusals | "stdin and stdout must both be a terminal"; "less is not GNU less" | **decided** (readers §9) |
| `-p`'s stderr | "hardn: no log yet: <path>"; "hardn: can't read <path>: <reason>"; "Run it with <tool>." | **decided** (§6) |
| A pager's failure | `host`'s own text, made safe: "host: less -- /var/log/hardn.log: exit status 1" (`ExitError.Error`, `pkg/host/host.go:361`; run on hostfake, the real seam's exit path read) | **proposed:** readers §9 words only the two refusals above; the maintainer's to word (§11) |
| A line past the cap; a reason | `<cut>`; "not a regular file", "refusing a symlink", ... | **decided** (readers §9) |
| Invalid key | "Invalid option. Please try again." | origin/main's, nav's (P-25 (c), proposed there) |
| The gallery's, dev-only | "System Details hosts", "Logs hosts", "Select a host", "Enter your choice", each host's label, about and note; "The gallery runs no pager. hardn would run ..." | the gallery's |

## 6. API

```go
package systemdetails // pkg/screens/systemdetails (new; its tests were its only files)

type Options struct {
	Prompter nav.Prompter     // the one line reader, the writer, the render context
	System   host.System      // a host with no run log (readers-system-details.md item 24)
	Facts    detect.Facts     // detect's: the OS and the elevation tool
	Now      func() time.Time // a login's year; nil is time.Now
}
// Run shows the screen until 0 (nil) or q (nav.ErrQuit); prompt.ErrAborted on the end
// of the input or Ctrl-C; fs.ErrInvalid, writing nothing, without a Prompter's In and
// Out or a System.
func Run(ctx context.Context, o Options) error

type PrintOptions struct {
	Out    io.Writer
	Env    term.Env // Env.TTY picks the form
	System host.System
	Facts  detect.Facts
	Now    func() time.Time
}
func Print(ctx context.Context, o PrintOptions) error

package logs // pkg/screens/logs (new)

type Options struct {
	Prompter nav.Prompter // its Ctx's height fits the lines
	System   host.System  // the log, and the pager; a host with no run log
	LogFile  string       // hardn.yml's logFile, said in a row where it names another path
	Term     string       // hardn's TERM, for the pager, which checks it
}
func Run(ctx context.Context, o Options) error

type PrintOptions struct {
	Out     io.Writer
	Env     term.Env // Env.TTY picks the form
	System  host.System
	LogFile string
}
type Printed struct{ logview.Reading }
func (Printed) Stderr(tool string) []string
func (Printed) ExitCode() int
func Print(ctx context.Context, o PrintOptions) (Printed, error)

package screen // pkg/ui/screen, added
const SystemDetailsRefreshItem = "1"
type SystemDetails struct{ OS, Host, Users, CPU, Disk, Memory, Logins []component.KeyValue }
func (SystemDetails) SubMenu() SubMenu
func (SystemDetails) Report(c component.Ctx) string
type LogsState uint8 // LogsLines, LogsNoLog, LogsEmpty, LogsUnreadable
type Logs struct {
	Path string; State LogsState; Reason string; Size int64; Total int
	Last []component.CodeLine; Elsewhere, Pager string
}
func (Logs) SubMenu(c component.Ctx) SubMenu
func (Logs) Terminal(c component.Ctx) string
func (Logs) OpenKey() string
// Section gains Code component.CodeBlock.

package component // pkg/ui/component, added
type CodeLine struct{ Text string; Warn bool }
type CodeBlock struct{ Lines []CodeLine }
func (CodeBlock) Render(c Ctx) string
func (CodeBlock) Rows(c Ctx) []string
func OffTerminal() Ctx
// Ctx gains Whole bool.

package logview // pkg/engine/logview, changed
func ParseLine(line string) string // Entry, LevelInfo, LevelSuccess and LevelInstalled gone
```

No new error, code or sentinel. No change to `pkg/engine` (`logview` aside), `pkg/host`, `pkg/output`, `pkg/config`, `pkg/screens/screenkit` or `pkg/ui/nav`. `pkg/ui/term`'s change is `Fixed`'s comment.

## 7. What unit (d) must do

*Built in `next/s1-replace`, in `cmd/hardn/readers.go`. Done differently: `pkg/cmd` keeps the five flags and returns a form or a refusal, which `cmd/hardn` runs. The hook passes detect's error over, as the gallery's Print did, so the report says what is not read. `<tool>` asks `detect.FindElevation`, which runs nothing, and only where the read failed. `-E`, `-S`, `-O`, `--json` and `--yaml` are hidden from `--help`. `-O` and `--output` refuse any other value, exit 2. `InitLogging`'s directory line moved to stderr with the two named. The warnings' words are proposed: `pkg/cmd/system_details_cmd.go`; readers-system-details.md §13 has none. A flag counts as given whatever its value (`--yaml=false` is refused, `--json=false` warns). `-p`, `-c`, `-d`, `-g`, `-w`, `-r` and `-e` with `hardn system-details` are refused, exit 2, its reason proposed too. As root `main()` still creates the log, and `/etc/hardn/hardn.yml.example` where absent, ahead of both `-p` and `hardn system-details`; what only reads is the command. The "Check the log file" lines name `/var/log/hardn.log` (`TestRunAllFlagNamesHardnsLog`, `TestRunAllNamesHardnsLog`).*

- **Main menu 9 and 10 call `Run`** in place of `system_details_menu.go`'s and `logs_menu.go`'s, through hooks `cmd/hardn` sets, as `SetEnvironment` and `withHooks` do (screen-environment.md §7), so that `pkg/menu` builds no reader. The hooks' `Options`: `Prompter` on `engineInput()` and `stdoutEnv()`; `System`, a host with no run log, the shape of `statusHost` (`cmd/hardn/status.go:29`; readers-system-details.md items 12, 24); `Facts`, `detect.Detect` through `details.Bounded`, per visit; `Now` nil; `LogFile`, `cfg.LogFile`; **`Term`, `os.Getenv("TERM")`**. They open under plan only as live. **Map what `Run` returns** as DNS's and Environment's: nil back to the main menu; `nav.ErrQuit` ends hardn; `prompt.ErrAborted` and any other as at every prompt.
- **`hardn system-details`:** `Print`, `Env` from `stdoutEnv()`, on the same host and facts; the five flags of decision 1 (a), `--output json` on `output.Build`'s document with `Run{Mode: ModeCheck, Hostname, Facts, Details}` (readers-system-details.md §13; `cli-json`, `cli-json-unreadable`, `cli-flags`, the gallery's reference, §10, which retire once this is built), with the warnings' words proposed there; a run log none.
- **`-p`:** `logs.Print`. **Where `-p` is the only operation flag, ahead of the root check** (`cmd/hardn/main.go:148`), so ahead of the config load (`:155`) and `detectOS` (`:184`), that its pipe form holds the log alone and a user other than root reads what `cat` would (decisions 9 (a), 10 (a)); `-n -p`'s row in `TestDryRunAloneIsNotRefused` (`cmd/hardn/main_test.go:325`) changes on purpose. **As root `main()` has already created the log** (`cmd/hardn/main.go:57`, `pkg/logging/logging.go:38`, `O_CREATE`), so where there was none it reads as empty: the File rows to a terminal, nothing off one, no words on stderr, exit 0. "hardn: no log yet" is reached only where that open failed, a user other than root, until the start-up unit moves it (read). **With another operation flag it stays where it prints today,** after the others (`cmd/hardn/main.go:416`; gallery-system-details.md §5, Made here), that operation's lines staying on stdout. **`PrintOptions.LogFile`:** "" where `-p` is alone, as nothing has loaded hardn.yml and `FindConfigFile` writes to the log (`pkg/config/config.go`; decision 10 (a)), so that form has no "isn't used" row, as `cli-terminal-tail` draws it; `cfg.LogFile` where it runs after a load (§11). Write `Printed.Stderr(tool)` on stderr, `<tool>` from detect's elevation tool or sudo, **"" as root**, as it prints "Run it with <tool>." on any `fs.ErrPermission`, root on a root-squashed NFS path among them; **set the exit status from `Printed.ExitCode()`**; a failed write as a write's. `-p` to a terminal's blank-line form is as the screen's.
- **The `logFile` warning** (decision 6 (a)), the "Check the log file" lines, and the key dropped from `hardn.yml.example`, `pkg/config/embed.go:19` and `docs/configuration.md:131`. To a terminal `Print` says "hardn.yml's logFile, <path>, isn't used" in a row, and off one nothing, and decision 6 (a) puts the warning where hardn.yml loads, only where the key names another path, once: `logview.Elsewhere` has the path.
- **`main()`'s two lines to stderr** (`pkg/logging/logging.go:40`, `cmd/hardn/main.go:62`), so that as a user other than root `> file` and `--output json` hold only the report (defect 28; `cli-not-root` draws them above the report until then).
- **The freeze first, then the legacy code deleted** (parity-system-details.md §8): `git mv` unchanged under `pkg/screens/systemdetails/testdata/legacy/menu/`, `legacyDir` pointed there, `TestLegacyMenuSystemDetails`, `TestLegacySystemDetailsFlags`, `TestLegacyMenuLogs` and `TestLegacyLogsParser` retired in the same commit, `TestLegacyRecord` holding each file's SHA-256; prove it (`cmp`, `go test -run TestParity -update` clean). Then `pkg/menu/system_details_menu.go`, `pkg/menu/logs_menu.go`, `pkg/system`, the log manager, service, model, port and repository, the lastlog adapter, `utils.PrintLogs`, each with its grep, `HostInfoManager` where the main menu uses it. Exit statuses, `-p`'s stderr and `system_details_flags`' eleven command lines (against decision 1 (a)) gain aspects there: no aspect here compares them.

## 8. Decisions of this unit

None is the maintainer's; each is for review, and the alternative is what it would change.

1. **The rows' words are in `pkg/screens/systemdetails`, a view-model of rows and one `Safe` pass.** *Alternative:* a semantic view-model in `pkg/ui/screen` duplicating the report, with its words there; two types to move together, and `pkg/ui` may not take the report's.
2. **Off a terminal is a flag on the context, `Ctx.Whole`, with `OffTerminal`** (DEV-067): the same components, nothing cut. A bullet item does not wrap either. *Alternative:* a second render path for the report, which every later plain form would copy.
3. **`Now` is an option:** a login's form turns on the year, and a test or the gallery needs a clock. *Alternative:* `time.Now` inside, the year in the goldens moving.
4. **`Term` and `LogFile` are given, not read:** depguard denies `os` and hardn.yml to `pkg/screens`. *Alternative:* a `Getenv`, as Environment's, for one variable.
5. **Open does not call `Flush`:** `prompt.Reader.Ask` flushes at the next prompt (read, and `TestOpenFlushes`). *Alternative:* a second flush at less's end, with no case it changes.
6. **The two refusals are matched by the end of their text,** neither error being exported and `logview` frozen but for decision 50; `TestOpenRefused` holds `logview`'s always, and the seam's only where the test's stdio is no terminal. *Alternative:* export both, a change to `pkg/host` and `logview`, which `errors.Is` then reads, once the freeze lifts (§11).
7. **`-p` to a terminal measures its rows,** two left for the shell. It differs from the mock's constant 11 at 60 columns alone, where the mock left the shell no room (`cli-terminal-tail`).
8. **`-p` with an empty log:** the File rows to a terminal, nothing off one, exit 0. The mocks drew neither. It is also what root sees where there was no log (§7). *Alternative:* stderr's words, as for no log; an empty file is not a failed read.
9. **System Details' tests are in the package,** `package systemdetails`, and parity's in `systemdetails_test`, as their many small names would collide.
10. **A drawn row beside its fact in `TestParity`:** where origin/main's form holds figures the words round (Cores, Volume, Memory) the aspect stays the fact in that form, read on a twin host so that the screen's host holds one read a draw, and the row drawn is its own aspect (`CPUs`, `Disk Used`, `Memory Used`), held to draw that fact by the test's own sum. The other aspects are the screen's output. *Alternative:* each aspect the drawn words alone, every plain-root `Volume` then differing, and a wrong figure failing only `parity.golden` (the harness's limit 1).
11. **Logs' cells draw at 80×552,** so that every line `Read` keeps is compared, and `holdFitted` holds the same screen at 80×24. *Alternative:* 80×24 alone, whose `lines` are three of the golden's five.
12. **`-p`'s cells run `logs.Print`,** not `Run`; `logs-p-missing`'s `logFile elsewhere` stays `logview.Elsewhere`'s path, as `Print` off a terminal holds the log alone and the warning is unit (d)'s.
13. **The export rows type 2, then q,** so that the key's refusal is compared; the renderer rows type 0, what stands around the report compared only where the golden has a menu.
14. **What moves on a Refresh is a `System` wrapper in the gallery** (`sdMoving`, `logsMoving`), as the screens have no Refresh to hook, and the pager's note its `RunInteractive`. *Alternative:* an option on the screens for the gallery alone.
15. **Listing forms stay where they draw real code or are unit (d)'s reference** (§10); Environment's listing went whole because nothing remained for a later unit.
16. **`ParseLine` returns a string,** `Entry` gone. *Alternative:* `Entry{Level}`, a struct of one field.
17. **No gallery host for a pager's refusal:** the nine hosts are the mocks'; `TestOpenRefused` holds it, and its render is made from a test host (renders' `built-from.txt`).

## 9. Tests

- `pkg/screens/systemdetails` (through `Run` and `Print`, on `hostfake`): `TestScreenHosts`, `TestDrawsOnce`, `TestRefresh`, `TestNavigation`, `TestRunNeeds`, `TestPlanOnly`, `TestPrint`, `TestLongValue`, `TestSafe`, `TestInterrupt`; moved from the gallery, `TestSDLastLogin`, `TestSDLsloginsCall`, `TestSDBlankCPU`, `TestSDNotRootNamesTheRules`, `TestSDRootRefusedSaysCantRead` (added), `TestSDUnreadableKeepsTheRest`, `TestSDRows`, `TestSDZFSDatasetIsRead`, `TestSDReadsAddrs`.
- `pkg/screens/logs`: `TestScreen`, `TestBlankNewestLine`, `TestHeights`, `TestElsewhere`, `TestSafeOnATerminal`, `TestOpen`, `TestOpenByItsKeyAlone`, `TestTerm`, `TestOpenWordsStayVisible`, `TestOpenRefused`, `TestOpenFlushes`, `TestNavigation`, `TestRefresh`, `TestRunNeeds`, `TestPlanOnly`, `TestPrintOffTerminal`, `TestPrintTerminal`, `TestPrintStates`, `TestInterrupt`.
- `pkg/ui/screen`: `TestSystemDetailsScreen`, `TestSystemDetailsWhole`, `TestSizeWords`, `TestLogsStates`, `TestLogsFit`, `TestLogsBlankLine`, `TestLogsTerminal`, `TestDetailsScreens`. `pkg/ui/component`: `TestCodeBlock`, `TestCodeLongLine`, `TestWhole`.
- `TestParity` (`pkg/screens/systemdetails`): drives the screens; 32 rows, 34 cells, 412 aspects agree, 586 differ, no UNEXPLAINED; `TestParityGoldens`, `TestTiesCiteTheirGoldens`, `TestReaderRefuses` (six more), `TestDrawnReaderRefuses` (new). parity-system-details.md §2, §7.
- `pkg/engine/logview`: `TestParseLine` on the level.
- The gallery: `TestPlaySystemDetailsGolden` (8), `TestPlayLogsGolden` (12), `TestSystemDetailsGolden` (7 forms), `TestLogsGolden` (6 forms), `TestSDHosts`, `TestSDLsloginsCall`, `TestSDReadsAddrs`, `TestSDZFSDatasetIsRead`, `TestSDJSONIsTheDocument`, `TestSystemDetailsUsage`, `TestSystemDetailsWritesNothing`, `TestLogHosts`, `TestControlHostIsSafe`, `TestPipeIsEveryLine`, `TestNotesOpenWithTheirSource`, `TestPagerOnTheHosts`, `TestLogsUsage`, `TestLogsWriteNothing`, `TestPlayTouchesNothing` (names the four packages). Retired with the mocks, each held where it moved: `TestSDLastLogin`, `TestSDBlankCPU`, `TestSDNotRootNamesTheRules`, `TestSDUnreadableKeepsTheRest`, `TestSDRows` (moved), `TestSystemDetailsHeights` (`TestScreenHosts`), `TestLogRowsKeepTheCharacters` (`TestCodeBlock`), `TestLogsHeights` (`logs.TestHeights`).
- **Transcripts** (run, a tool outside the tree, not committed): 1,536 plays at 3d58430a against the tip, 48 key sequences (all nine hosts of each screen, Refresh, an invalid key, Open, q; both hosts' menus; the main menu's 9 and 10), at 80 and 60 columns, 24 and 40 rows, normal and dry-run, under truecolor Unicode, truecolor LANG=C, plain, and no colour with LANG=C. 32 identical; 1,440 differ in the opening note alone; 64 (`logs/alpine323`) in that and the host's note line, which named `pager-commands`. Every other line byte for byte.

## 10. Goldens

- **Added (4):** `pkg/ui/component/testdata/golden/codeblock/{empty,lines,long,shown}.golden`: `CodeBlock`'s states, in its catalogue.
- **Changed:**
  - `pkg/screens/systemdetails/testdata/golden/parity.golden` and the region of parity-system-details.md §4: the screens' rows (parity-system-details.md §7); then logs-parser's "new path:" line, `ParseLine` reduced; then `sd-error`'s `title` entry, which cited defect 7 and cites what says it.
  - `play/logs-alpine`, `-control`, `-elsewhere`, `-empty`, `-hosts`, `-missing`, `-no-pager`, `-open`, `-refresh`, `-unreadable`; `play/system-details-alpine`, `-hosts`, `-refresh`, `-sudo-rs`, `-unreadable`, `-zfs` (16): the opening note, which said "mocks" and "unit (c)'s to build"; `play/logs-alpine` also the host's note, which named `pager-commands`.
  - `system-details/cli-pipe-cut`: a value whole off a terminal, the purpose of DEV-067; its note; no bold in the colour cells.
  - `system-details/cli-pipe`: no bold in the colour cells, as `Print` writes through a plain environment's writer; its note.
  - `logs/cli-pipe`: its note names no retired mock.
  - `logs/cli-terminal-tail`: at 60 columns "Last 7", two rows for the shell (decision 7); the other cells byte for byte.
- **Removed (27):** `system-details/screen-*` (9), `plan-only`, `origin`, `origin-error`; `logs/screen-*` (9), `open`, `pager-commands`, `parser`, `origin`, `origin-p`, `origin-elsewhere`: the mocks the real code replaces. origin/main's screens stay recorded by `pkg/characterization`'s `menu/system_details_*` and `menu/logs_*`, which unit (d) freezes; git keeps the mocks (`next/s1-parity`), and gallery-system-details.md §3 records them. **`origin-p` is the exception:** `logs_print_flag` calls `PrintLogs` alone, so the lines ahead of the log on origin/main's `-p`, the config line, the clear sequence, the `~~~` line and the OS line, were read, not run, and had that mock as their only drawing. They are kept in gallery-system-details.md §2.9 and git alone; no golden or aspect holds them, and §4 check 6 is where they are confirmed gone. `logs/cli-pipe`'s note lost its sentence naming them.
- **Kept, drawn through real code, golden unchanged:** `system-details/cli-terminal`, `cli-not-root` (the real `Print`); `logs/cli-pipe-control` (the real `Print`); `logs/cli-missing`, `cli-unreadable`, `cli-unreadable-not-root` (`Printed.Stderr`). **Kept for unit (d), unchanged:** `system-details/cli-json`, `cli-json-unreadable` (the real `output.Build`), `cli-flags`.
- **Unchanged:** every other golden: the legacy and characterization goldens, DNS's, Backup's and Environment's (screens, parity goldens, pages' regions), the controls', `pkg/output`'s, `pkg/ui/screen`'s, and the gallery's others (run: `git diff --name-status 3d58430a` lists the files above alone).

## 11. Not in this unit; open items

- **Unit (d):** §7. **Run (unit (d) and the VMs):** §4.
- **For the maintainer, none blocking:**
  - **P-28 stays proposed:** both screens draw its (a), a sub-menu with no pause, as the adopted Made here call says for these two alone; SSH_LOGIN-SSH_SETTINGS_DISPLAY's choice stays open (README §9.12).
  - **Not closed, known (readers-system-details.md §4):** `Read` refuses a link at the path, but `less` opens the path itself, as root, so a link planted after the screen's read is followed by `less` [I, not run]; that needs write access in `/var/log`. Ctrl-Z and a SIGTERM to hardn alone while `less` runs.
  - **A pager's failure is shown as `host` says it,** "host: less -- /var/log/hardn.log: exit status 1", under "Warning", then the screen draws again: proposed (§5), as no adopted source words it, and built as §2.12 has it. Reachable where the log goes between the draw and the key [I, not run]. Word it in the code only if a wording is adopted.
  - **Any failure of the key's second check says "less is not GNU less",** a done context or a timed-out `less --version` too (`gnuLess`, `pkg/engine/logview/pager.go:91`; read; run on a done context). It fails closed correctly and the words are then untrue; readers §9's Why says Open reaches them "only off a terminal or after a package changed". `logview` is frozen but for decision 50, so it is reported here. Kept, the orchestrator's call: it fails closed in a rare case, readers §9's since-note says so, and a separate outcome waits until it is seen in practice.
  - **`-p` alone has no "isn't used" row,** as nothing has loaded hardn.yml (§7), as `cli-terminal-tail` draws it. Kept, the orchestrator's call: it is what decisions 9 (a) and 10 (a) adopted. *Alternative:* a read of the one key that writes no log line, for unit (d) to add.
  - **Once the freeze lifts, export the pager's two refusals,** so that `errors.Is` replaces the copied strings in `pkg/screens/logs/logs.go:44`, `pkg/host/interactive.go:29` and `pkg/engine/logview/pager.go:21` (§8 decision 6).
  - **`details.HostIPs`' doc says Addresses is "empty, not nil, when it answered none",** and it is empty too where `ip` answered only lines not read (`pkg/engine/details/sections.go:85-99`), with an Unread beside it. The screen reads both (§2.5); `pkg/engine/details` is frozen here, so the comment is corrected by the next unit that touches the reader.
  - **7e483a40 fails the gallery's `TestSmoke`,** mended by ac25ddad, the next commit, and history is not rewritten here. Merged as is, the orchestrator's call: `next/v0.4`'s first-parent line is merges only, so `git bisect --first-parent` passes over it.
  - **A screen's wait says nothing:** a draw on a hung host waits up to the readers' bounds with nothing on the screen (readers-system-details.md item 23, about 51 s at the worst on a ZFS root), as every draw of hardn does; Ctrl-C ends it. §4 check 3 is where it is seen first.

## 12. Size

`git diff --numstat 3d58430a 1d0ec99b` (run): non-test Go 1,229 lines added and 987 removed: `pkg/screens/systemdetails` 362 (66 of them whole-line comments), `pkg/screens/logs` 234 (71), `pkg/ui/screen/systemdetails.go` 73 (23) and `logs.go` 197 (50), `pkg/ui/component` 94, `pkg/engine/logview` 16 and 25; the gallery's `logs.go` 106 and 475 (now 379 lines), `systemdetails.go` 101 and 443 (now 769 lines), `main.go` and `play.go` 25 and 28. Tests 3,283 added and 621 removed: the screens' 1,828 new, `pkg/ui`'s 347, `TestParity`'s `parity_test.go`, `drive_test.go` and `legacy_read_test.go` 1,019 and 258, the gallery's 74 and 347. Testdata 2,015 added and 5,569 removed. Over the guideline of about 500 changed lines, as Environment's and Backup's: two screens and their off-terminal forms, their parity driver and their gallery play, retiring the mocks in the same unit; net of the removals, about 240 lines of non-test Go. `go.mod` and `go.sum` unchanged. The unit's review then added, by `git diff --numstat 32cbbc2c` (run): non-test Go 42 lines and 13 removed, tests 181 and 24.
