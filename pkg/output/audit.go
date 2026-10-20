package output

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/abbott/hardn/pkg/host"
)

// AuditPath is the audit log, the path the refactor plan proposes (row
// p3-output): one JSON line per run, a Record, appended.
const AuditPath = "/var/log/hardn/audit.jsonl"

// LegacyLogPath is the log hardn has written since v0.3.x, as the brief's
// [Verify] asked it recorded (docs/refactor/phase0-findings.md §6, [V]):
// main() opens it before flags are parsed (V26), O_CREATE|O_APPEND, 0644,
// in a directory made 0755; each line is Go's log.LstdFlags date and time,
// then "ERROR: ", "WARNING: ", "INFO: ", "SUCCESS: " or "INSTALLED: " and
// the message; nothing rotates it. The logFile key is only read (the Logs
// screen, -p), so the two differ when it is changed. Unchanged here: the
// audit log is new, beside it.
const LegacyLogPath = "/var/log/hardn.log"

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
// nor others can write (ErrAuditDir), then sys.AppendFile (AuditFileMode
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
	switch err := sys.AppendFile(ctx, path, line, AuditFileMode); {
	case errors.Is(err, host.ErrUntrustedDir):
		return fmt.Errorf("output: audit log %s: %w: %w", dir, ErrAuditDir, err)
	case err != nil:
		return fmt.Errorf("output: audit log: %w", err)
	}
	return nil
}
