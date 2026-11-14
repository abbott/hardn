package output

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/abbott/hardn/pkg/host"
)

// AuditPath is the audit log, the path the refactor plan proposes (row
// p3-output): one JSON line per run, a Record, appended. Append rotates it
// (AuditMaxSize, AuditKeep).
const AuditPath = "/var/log/hardn/audit.jsonl"

// LegacyLogPath is the log hardn has written since v0.3.x, as the brief's
// [Verify] asked it recorded (docs/refactor/phase0-findings.md §6, [V]):
// main() opens it before flags are parsed (V26), O_CREATE|O_APPEND, 0644,
// in a directory made 0755; each line is Go's log.LstdFlags date and time,
// then "ERROR: ", "WARNING: ", "INFO: ", "SUCCESS: " or "INSTALLED: " and
// the message. The command that opens it rotates it first (LogMaxSize,
// LogKeep). The logFile key is only read (the Logs screen, -p), so the two
// differ when it is changed. Unchanged here: the audit log is new, beside
// it.
const LegacyLogPath = "/var/log/hardn.log"

// The logs' rotation (docs/engine/design-log-rotation.md, decision 3): a
// log over its size is renamed to <path>.1 before the next line, the
// older copies moved up and the oldest deleted (host.System.RotateFile).
// Fixed: no hardn.yml key.
const (
	LogMaxSize   = 10 << 20 // hardn.log: 10 MiB
	LogKeep      = 4        // hardn.log.1 to .4
	AuditMaxSize = 5 << 20  // audit.jsonl: 5 MiB
	AuditKeep    = 9        // audit.jsonl.1 to .9
)

// RotateError is Append's error where the record was appended and the
// log could not be rotated first (decision 7): a warning for its caller,
// as the record is written.
type RotateError struct {
	Path string
	Err  error
}

func (e *RotateError) Error() string { return NotRotated(e.Path, e.Err) }
func (e *RotateError) Unwrap() error { return e.Err }

// NotRotated is the one warning for the log at path that err kept from
// being rotated: "<path> was not rotated: <why>". The path is said once:
// where err is a *fs.PathError, why is its Err alone when it names the
// log, and "<copy>: <Err>" when it names a copy. A rename's error is said
// as it is.
func NotRotated(path string, err error) string {
	why := err.Error()
	if pe := (*fs.PathError)(nil); errors.As(err, &pe) {
		if why = pe.Err.Error(); pe.Path != path {
			why = pe.Path + ": " + why
		}
	}
	return path + " was not rotated: " + why
}

// The audit log's modes: the directory and the file root's alone, since a
// record holds the plan, its diffs included.
const (
	AuditDirMode  = 0o750
	AuditFileMode = 0o600
)

// ErrAuditDir: the audit log's directory is not a directory root alone can
// change, so nothing is appended to it.
var ErrAuditDir = errors.New("the audit log's directory is not root's alone")

// NewRecord is run r's audit record, at time at (UTC): its document, the
// same that stdout gets as JSON.
func NewRecord(r Run, at time.Time) Record {
	return Record{Time: at.UTC(), Document: Build(r)}
}

// AuditTimeout bounds Append, which runs without the caller's cancellation.
const AuditTimeout = 10 * time.Second

// Append appends rec to the audit log at path (AuditPath in production)
// through sys, as one line: its directory made when missing (AuditDirMode)
// and refused unless it is a directory owned by root that neither its group
// nor others can write (ErrAuditDir), then sys.RotateFile where the log is
// over AuditMaxSize, which checks the directory it opens as AppendFile
// does, so nothing is renamed in one that is not root's alone, and whose
// failure never keeps the record out (a *RotateError, once the record is
// appended), then sys.AppendFile (AuditFileMode
// when it creates the file), which checks the directory again on the one
// it opens (host.ErrUntrustedDir, wrapped in ErrAuditDir), so that a
// directory swapped in after the check is refused too; it refuses a
// symlink and a file that is not regular, and appends with O_APPEND, so
// two runs never overwrite each other's lines.
//
// It runs without ctx's cancellation, bounded by AuditTimeout: a run ended
// by Ctrl-C, SIGTERM or a hangup (signal.NotifyContext's context done) is
// the run its record matters most for, and it is written all the same.
func Append(ctx context.Context, sys host.System, path string, rec Record) error {
	line, err := marshal(rec, "")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), AuditTimeout)
	defer cancel()
	dir := filepath.Dir(path)
	if err := sys.MkdirAll(ctx, dir, AuditDirMode); err != nil {
		return fmt.Errorf("output: audit log: %w", err)
	}
	fi, err := sys.Lstat(ctx, dir)
	if err != nil {
		return fmt.Errorf("output: audit log: %w", err)
	}
	if uid, _, ok := host.Owner(fi); !fi.IsDir() || !ok || uid != 0 || fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("output: audit log %s: %w", dir, ErrAuditDir)
	}
	_, notRotated := sys.RotateFile(ctx, path, AuditMaxSize, AuditKeep, true)
	switch err := sys.AppendFile(ctx, path, line, AuditFileMode); {
	case errors.Is(err, host.ErrUntrustedDir):
		return fmt.Errorf("output: audit log %s: %w: %w", dir, ErrAuditDir, err)
	case err != nil:
		return fmt.Errorf("output: audit log: %w", err)
	}
	if notRotated != nil {
		return &RotateError{Path: path, Err: notRotated}
	}
	return nil
}
