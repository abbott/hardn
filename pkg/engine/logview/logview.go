// Package logview is the reader of hardn's log (refactor plan slice 1, unit
// (b), next/s1-readers): what the Logs screen and hardn -p show of it, read
// through host.System and returned as data, with nothing rendered. It sits
// beside pkg/engine/header, as a reader, and is no control: neither has a
// Check or a plan, as the log has no main-menu row and no change.
//
//   - The file is /var/log/hardn.log, whatever hardn.yml's logFile says
//     (decision 6 (a), gallery-system-details.md §5); Elsewhere says when
//     logFile names another path.
//   - Read gives the path, the size, the number of lines and the last lines,
//     or why there are none, in the words the row shows. Each gives every line
//     as it reads, and the same words: hardn -p off a terminal.
//   - Safe and Show make a line of the log safe to show, on the screen and
//     off it (-p): what a terminal would act on is shown, not stripped.
//   - ParseLine reads a line's level, for its colour alone.
//   - Pager is the command that shows the whole log, GNU less alone, or none
//     (decision 7 (a)).
//
// Nothing here writes, and Read adds no line to a run log. hardn's status
// host, which has no run log (cmd/hardn/status.go:29), is the host it is for:
// the less --version Pager asks would be logged on one that has one.
// docs/engine/readers-system-details.md has each form and what has not run.
package logview

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path"
	"regexp"
	"slices"

	"github.com/abbott/hardn/pkg/host"
)

// File is the file hardn writes (cmd/hardn/main.go:57), the one this package
// reads.
const File = "/var/log/hardn.log"

// MaxLast is the most lines Read keeps, oldest dropped. A screen fits what it
// has room for from their end; each takes a row at least, so no terminal needs
// more. With host.MaxLine it bounds what Read holds: 16 MiB of the file's bytes,
// and at worst about 96 MiB once shown, Safe making a bad byte 6, so about 112
// MiB at the moment it maps one to the other. Writing such a log needs root.
const MaxLast = 256

// State is what Read found.
type State uint8

const (
	// Lines: the file holds lines (Reading.Last).
	Lines State = iota + 1
	// NoLog: no file at the path.
	NoLog
	// Empty: the file holds nothing.
	Empty
	// Unreadable: the path cannot be read (Reading.Reason, Reading.Err).
	Unreadable
)

// Reading is what Read read of the log.
type Reading struct {
	Path  string
	State State
	// Reason says why it is Unreadable, in the words the row shows after
	// "Can't read it: ", without the path, which the row names. Err is the
	// error behind it, for a caller that tells them apart (fs.ErrPermission:
	// decision 9 (a)'s "Run it with sudo.").
	Reason string
	Err    error
	// Size is the file's size when it was opened, in bytes, and Total the
	// lines it then held, a cut line and an empty one counting as one each.
	// Total is 0 when a read failed part way, as a part is no total.
	Size  int64
	Total int
	// Last is the last lines, oldest first, at most MaxLast.
	Last []Line
}

// Line is one line of the log, safe to show (Show).
type Line struct {
	Text string
	// Level is the level logging.go wrote, "" for a line of any other shape.
	Level string
}

// Read reads the log through sys's ScanLines, in memory bounded whatever the
// file holds, and changes nothing. A symlink at the path, a directory, a FIFO
// or a device is Unreadable, without blocking (host.System).
func Read(ctx context.Context, sys host.System) Reading {
	return read(ctx, sys, File)
}

func read(ctx context.Context, sys host.System, file string) Reading {
	var last tail
	r := scan(ctx, sys, file, func(l host.Line) bool {
		last.add(l)
		return true
	})
	if r.State == Lines {
		for _, l := range last.lines() {
			r.Last = append(r.Last, lineOf(l))
		}
	}
	return r
}

// Each gives yield every line of the log as it reads, safe to show, until yield
// returns false: hardn -p off a terminal, a pass over a log nothing rotates, in
// memory bounded by the line. What it returns is Read's, Last empty: a failure
// part way, after lines have gone, is Unreadable in Read's words, so -p's "can't
// read <path>: <reason>" is one form whenever it fails. A caller's write error
// is the caller's: it returns false, and keeps the error.
func Each(ctx context.Context, sys host.System, yield func(Line) bool) Reading {
	return scan(ctx, sys, File, func(l host.Line) bool { return yield(lineOf(l)) })
}

// scan is one pass over file, and what became of it.
func scan(ctx context.Context, sys host.System, file string, yield func(host.Line) bool) Reading {
	r := Reading{Path: file}
	fi, err := sys.ScanLines(ctx, file, func(l host.Line) bool {
		r.Total++
		return yield(l)
	})
	if fi != nil {
		r.Size = fi.Size()
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		r.State, r.Total = NoLog, 0
	case err != nil:
		r.State, r.Total, r.Err, r.Reason = Unreadable, 0, err, reasonOf(ctx, err)
	case r.Total == 0:
		r.State = Empty
	default:
		r.State = Lines
	}
	return r
}

// lineOf is a line of the file as Read and Each give it.
func lineOf(l host.Line) Line {
	text := Show(l)
	return Line{Text: text, Level: ParseLine(text)}
}

// tail keeps the last MaxLast lines it is given, reusing their memory: a log
// of any length costs one pass and no more than they hold.
type tail struct {
	held []host.Line
	next int // the oldest, once held is full
}

func (t *tail) add(l host.Line) {
	if len(t.held) < MaxLast {
		t.held = append(t.held, host.Line{Text: bytes.Clone(l.Text), Cut: l.Cut})
		return
	}
	oldest := &t.held[t.next]
	oldest.Text, oldest.Cut = append(oldest.Text[:0], l.Text...), l.Cut
	t.next = (t.next + 1) % MaxLast
}

// lines are the lines kept, oldest first.
func (t *tail) lines() []host.Line { return slices.Concat(t.held[t.next:], t.held[:t.next]) }

// reasonOf is err's own words, without the path, which the row names, and
// fs.ErrInvalid's in the user's terms: a FIFO or a device is why ScanLines
// gives it, and "invalid argument" says neither. A done ctx is ctx's words.
func reasonOf(ctx context.Context, err error) string {
	var pe *fs.PathError
	switch {
	case ctx.Err() != nil:
		return ctx.Err().Error()
	case errors.Is(err, fs.ErrInvalid):
		return notRegular
	case errors.As(err, &pe):
		return pe.Err.Error()
	}
	return err.Error()
}

// Elsewhere returns hardn.yml's logFile, safe to show, where it names a path
// other than File, for "hardn.yml's logFile, <path>, isn't used" and unit
// (d)'s warning; "" where it is empty or names File. Two spellings of one path
// are one path, as path.Clean has it. The file is not looked at: a link to File
// is another path.
func Elsewhere(logFile string) string {
	if logFile == "" || path.Clean(logFile) == File {
		return ""
	}
	return Safe(logFile)
}

// The levels a line takes the warning's colour for, of those logging.go writes after
// the date and time; it writes INFO, SUCCESS and INSTALLED too.
const (
	LevelError   = "ERROR"
	LevelWarning = "WARNING"
)

// logLine is Go's log.LstdFlags date and time (pkg/logging/logging.go:46), a
// level and its message.
var logLine = regexp.MustCompile(`^[0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} (ERROR|WARNING|INFO|SUCCESS|INSTALLED): `)

// ParseLine reads the level of "DATE TIME LEVEL: message", where origin/main's
// GetLogs took the date for the time and the clock for the level
// (gallery-system-details.md §2.10). A line of any other shape has none, as the
// second line of a message has none. For a line's colour alone: nothing else depends
// on it.
func ParseLine(line string) string {
	if m := logLine.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}
