// Package prompt is hardn's line reader: every prompt in hardn reads what
// the user types through a Reader, so input has one buffer and one set of
// rules.
//
// # Lines
//
// Every input is a line, typed and confirmed with Enter (brief §2). The
// terminal stays in its normal (cooked) mode: nothing here switches it to
// raw or cbreak mode, so the terminal's own echo, line editing and Ctrl-C
// keep working over SSH, serial consoles, Proxmox's browser console and
// piped input alike, and a crash cannot leave the terminal broken. This
// replaces the legacy pkg/menu input, which ran stty raw or cbreak around
// each key (docs/refactor/phase0/interaction.md §3).
//
// ReadLine returns one line without its line ending, "\n" or "\r\n", and
// otherwise as typed: it trims nothing else and checks nothing. Matching a
// line to a choice is next/p1c-nav's, validating it next/p1c-form's. A
// line longer than MaxLine is an error, ErrLineTooLong, never cut short.
//
// # Aborting
//
// ReadLine returns an error wrapping ErrAborted at the end of the input
// (Ctrl-D on an empty line of a terminal, or the end of a pipe or file)
// and when Ctrl-C interrupts it. A last line with no line ending before
// the end of the input is still returned, and the call after it reports
// the abort. Nothing is applied after an abort (brief 1C): the caller
// stops, and ExitCode gives the CLI its exit status, 130.
//
// In cooked mode the terminal turns Ctrl-C into SIGINT. While ReadLine
// waits for input from a file, such as os.Stdin or a pipe, it relays
// os.Interrupt with signal.Notify and wakes at once on it; signal.Stop
// gives SIGINT back its default action, which ends hardn, before ReadLine
// returns, so Ctrl-C at any other time is not caught here. If SIGINT is
// ignored, as for a background job started by a non-interactive shell, it
// is left ignored. An interrupt that arrives during a call wins over a
// line read in the same call: ReadLine reports the abort and keeps the
// line in its buffer.
//
// No read outlives its call. ReadLine reads only once select(2) reports
// the input readable, so a call that a signal interrupts has read nothing
// and leaves no goroutine reading that could take a later line from the
// next call or from a process hardn runs. Two other designs were
// rejected:
//
//   - A goroutine reading ahead into a channel loses nothing, but its
//     read stays blocked after an interrupt: it takes the next line typed
//     before anything else reading the terminal can, and a line it reads
//     just before a typeahead flush (Flush) can slip past the flush.
//   - github.com/muesli/cancelreader v0.2.2, already in the module graph,
//     cancels a read of a pipe or a terminal, but on Linux epoll rejects
//     a regular file (epoll_ctl(2), EPERM), so stdin redirected from a
//     file fails (cancelreader_linux.go:44-51); its fallback for other
//     readers drops the data of a read it cancels (cancelreader.go:53-62);
//     a cancelled reader is spent (cancelreader.go:49-51), so each call
//     would need a new one; and it takes the descriptor with File.Fd,
//     which sets the file to blocking mode.
//
// select is used on Linux and macOS alike: macOS's poll(2) does not
// support devices such as a terminal (poll(2), BUGS). It is limited to
// descriptors below FD_SETSIZE, 1024: stdin is 0. Elsewhere, and for a
// reader that is not a file (a strings.Reader in a test), ReadLine reads
// directly and Ctrl-C keeps its default action.
//
// # Typeahead
//
// A key typed while a long step ran, such as a package install, must not
// answer the next prompt (brief 1C). On a terminal Reader (New's tty),
// Flush discards what the user typed that nothing has read yet: the
// terminal's input queue (TCFLSH with TCIFLUSH on Linux, TIOCFLUSH with
// FREAD on macOS, nothing elsewhere) and the Reader's own buffer. Ask
// flushes before it writes the prompt. Piped and scripted input is never
// flushed: each line queued there is an answer.
//
// # Without a terminal
//
// Plain mode needs no input handling of its own. When stdin is not a
// terminal (term.Env.InputTTY false) the Reader reads the same lines and
// never flushes, the end of the input aborts as on a terminal, and Ask
// ends each prompt's line itself, since no terminal echoed the Enter.
// Whether hardn prompts at all without a terminal is next/p3-output's
// decision (brief Phase 3: never prompt without a TTY).
//
// On a terminal, Ask takes stdout to be the same terminal. When stdin is
// a terminal and stdout is not, as in hardn | tee hardn.log, the terminal
// echoes the answer and its Enter to itself, not to stdout, so the
// captured output holds neither and each prompt runs into what follows
// it. Ending the line in stdout there would put an empty line on the
// screen tee writes to; where hardn writes its prompts when stdout is not
// a terminal is next/p3-output's to decide.
//
// # Writing a prompt
//
// component.PromptLine renders up to its colon, with no trailing space
// and no cursor (C8, DEV-029). Ask writes it with exactly one space after
// it, where the terminal's own cursor then stands, and reads the answer.
// The terminal echoes what is typed in its own colours (DEV-043).
//
// This package imports only the standard library and golang.org/x/sys, so
// next/p1c-legacy-input can route the legacy menus through it without
// linking Lip Gloss, whose import runs a colour profile detection
// (docs/design-system/README.md §2.3).
package prompt
