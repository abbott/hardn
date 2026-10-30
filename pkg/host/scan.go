package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
)

// MaxLine is the most of one line ScanLines keeps, in bytes: 64 KiB, where
// origin/main's scanner stops and loses the file's rest
// (file_logs_repository.go:47-70). A longer line is cut (Line.Cut) and the
// read goes on at the next line.
const MaxLine = 64 << 10

// scanChunk is how much ScanReader reads at a time, and so how much it reads
// past the point where ctx is done or yield stops it.
const scanChunk = 32 << 10

// Line is one line of a file ScanLines reads.
type Line struct {
	// Text is the line without its ending ("\n" or "\r\n"), at most MaxLine
	// bytes. It is valid only during the call to yield: copy what is kept.
	Text []byte
	// Cut says the line went on past MaxLine and the rest is not in Text.
	Cut bool
}

// ScanLines opens name as ReadFileNoFollow does, and scans it (see System).
// Its FileInfo is from the open file, and so are the lines: it reads no
// further than the size that FileInfo gives, so a writer that keeps
// appending cannot keep hardn reading, and the size and the lines describe
// one moment. It adds nothing to the run log.
func (r *Real) ScanLines(ctx context.Context, name string, yield func(Line) bool) (fs.FileInfo, error) {
	if err := ctxErr(ctx, "read", name); err != nil {
		return nil, err
	}
	f, fi, err := r.openRegular(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return fi, ScanReader(ctx, io.LimitReader(f, fi.Size()), yield)
}

// ScanReader calls yield with each line of in, in order, until in ends or
// yield returns false, holding at most scanChunk and MaxLine bytes and a
// little more whatever in holds. An error from in is returned; so is ctx's,
// asked before each read. A final line with no ending is a line, an empty
// line is a line, and "\r" is taken off only where it ends a line. A line
// past MaxLine is given to yield as soon as that is known, Cut set, so a file
// with no newline in it is given up on at its first MaxLine bytes when yield
// returns false; what is left of it is skipped. ScanLines and the fake's
// share it.
func ScanReader(ctx context.Context, in io.Reader, yield func(Line) bool) error {
	buf := make([]byte, scanChunk)
	line := make([]byte, 0, 4<<10)
	skipping := false // the rest of a line already given to yield, Cut
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("host: scan: %w", err)
		}
		n, err := in.Read(buf)
		for data := buf[:n]; len(data) > 0; {
			end := bytes.IndexByte(data, '\n')
			part := data
			if end >= 0 {
				part, data = data[:end], data[end+1:]
			} else {
				data = nil
			}
			if !skipping {
				line = append(line, part[:min(len(part), scanHeld-len(line))]...)
			}
			switch {
			case end >= 0 && skipping:
				skipping = false
			case end >= 0:
				if !yield(endLine(line)) {
					return nil
				}
				line = line[:0]
			case !skipping && len(line) == scanHeld:
				if !yield(endLine(line)) {
					return nil
				}
				line, skipping = line[:0], true
			}
		}
		switch {
		case errors.Is(err, io.EOF):
			if len(line) > 0 {
				yield(endLine(line))
			}
			return nil
		case err != nil:
			return err
		}
	}
}

// scanHeld is how much of a line ScanReader holds: past MaxLine and a "\r"
// by one byte, which is what says the line is longer than MaxLine, whatever
// comes next.
const scanHeld = MaxLine + 2

// endLine is the Line of held, what ScanReader holds of a line, its ending
// taken off.
func endLine(held []byte) Line {
	held = bytes.TrimSuffix(held, []byte{'\r'})
	if len(held) > MaxLine {
		return Line{Text: held[:MaxLine], Cut: true}
	}
	return Line{Text: held}
}
