package revert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/abbott/hardn/pkg/host"
)

// A control's record (P-43 (4), revert.md §10): what hardn changed for one
// control, so that undoing it later counts only what hardn did (R2). It is
// <Options.Dir>/controls/<control id>.json, root's, 0600, in a 0700
// directory, under the journal's trust checks. revert never writes it: a
// control's last engine change does (EncodeRecord gives the bytes and the
// path), with the path in its Backups, so the engine backs it up and
// journals it, and a rollback puts the old record back or removes it.

// RecordVersion is a record's format version; another is refused.
const RecordVersion = 1

// ControlsName is the records' directory under the state directory.
const ControlsName = "controls"

// ErrRecord is a record that cannot be read: not JSON, a field this
// version does not know, another schemaVersion, or a value out of form.
var ErrRecord = errors.New("control record not valid")

// ControlRecord is one control's record: an entry per apply, oldest first.
type ControlRecord struct {
	SchemaVersion int           `json:"schemaVersion"`
	Entries       []RecordEntry `json:"entries"`
}

// RecordEntry is one apply of the control.
type RecordEntry struct {
	Direction string    `json:"direction"` // DirectionOn or DirectionOff
	Time      time.Time `json:"time"`
	Hardn     string    `json:"hardnVersion"`
	PlanID    string    `json:"planId"` // the plan's id: its journal, backup run and timer
	// Steps are what ran, in order, in the control's words.
	Steps []string `json:"steps"`
	// Packages are those the install's simulation listed.
	Packages []string `json:"packages,omitempty"`
	// Before is each file the apply writes or a package brings, as it was
	// before the apply (R2); Files the same files after it.
	Before []FileHash `json:"before,omitempty"`
	Files  []FileHash `json:"files,omitempty"`
	// Lines are the setting lines hardn wrote.
	Lines []string `json:"lines,omitempty"`
	// ReadBack is the setting as read back after the apply.
	ReadBack string `json:"readBack,omitempty"`
	// Services are their state before the apply (crond on Alpine).
	Services []ServiceState `json:"servicesBefore,omitempty"`
	// Jobs are what else ran by the services before the apply: crond's
	// other jobs, by path.
	Jobs []string `json:"jobsBefore,omitempty"`
}

// The directions of an entry.
const (
	DirectionOn  = "on"
	DirectionOff = "off"
)

// FileHash is a file and its SHA-256, in lower-case hex; "" is absent.
type FileHash struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	By     string `json:"by"` // "hardn" or the package's name
}

// ServiceState is a service as it was: State in the init system's word
// ("started", "stopped"), AtBoot whether it was in a runlevel or enabled
// ("yes", "no", "" unknown).
type ServiceState struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	AtBoot string `json:"atBoot,omitempty"`
}

// RecordPath is the record of control under opts' state directory. A
// control id is 1 to 64 letters, digits, '-', '_' or '.', starting with a
// letter or digit, with no ".." ("updates.auto"); another is ErrID.
func RecordPath(opts Options, control string) (string, error) {
	if !validControl(control) {
		return "", fmt.Errorf("revert: record of %q: %w", control, ErrID)
	}
	return filepath.Join(opts.dir(), ControlsName, control+".json"), nil
}

func validControl(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case (c == '-' || c == '_' || c == '.') && i > 0:
			if c == '.' && id[i-1] == '.' {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Check refuses a record EncodeRecord would not write or ReadRecord would
// not return (ErrRecord): another schemaVersion, no entries, an entry with
// no known direction, no time, a plan id that is not a change id, no
// steps, a path not clean and absolute, a hash not SHA-256's form, or a
// service with no name or state.
func (r ControlRecord) Check() error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("revert: record: %s: %w", fmt.Sprintf(format, a...), ErrRecord)
	}
	if r.SchemaVersion != RecordVersion {
		return bad("schemaVersion %d", r.SchemaVersion)
	}
	if len(r.Entries) == 0 {
		return bad("no entries")
	}
	for i, e := range r.Entries {
		switch {
		case e.Direction != DirectionOn && e.Direction != DirectionOff:
			return bad("entry %d: direction %q", i+1, e.Direction)
		case e.Time.IsZero():
			return bad("entry %d: no time", i+1)
		case !ValidID(e.PlanID):
			return bad("entry %d: plan id %q", i+1, e.PlanID)
		case len(e.Steps) == 0:
			return bad("entry %d: no steps", i+1)
		}
		for _, f := range append(append([]FileHash(nil), e.Before...), e.Files...) {
			if !clean(f.Path) || !sha256Hex(f.SHA256) || f.By == "" {
				return bad("entry %d: file %q", i+1, f.Path)
			}
		}
		for _, j := range e.Jobs {
			if !clean(j) {
				return bad("entry %d: job %q", i+1, j)
			}
		}
		for _, s := range e.Services {
			if s.Name == "" || s.State == "" {
				return bad("entry %d: service %q", i+1, s.Name)
			}
		}
	}
	return nil
}

// sha256Hex reports whether s is "" (absent) or 64 lower-case hex digits.
func sha256Hex(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// EncodeRecord returns the record of control's path and its bytes, checked
// (Check) and indented as the journal is. The change that writes them
// names the path in its Backups and calls PrepareRecordDir first.
func EncodeRecord(opts Options, control string, r ControlRecord) (string, []byte, error) {
	name, err := RecordPath(opts, control)
	if err != nil {
		return "", nil, err
	}
	if err := r.Check(); err != nil {
		return "", nil, err
	}
	data, err := encode(r)
	if err != nil {
		return "", nil, fmt.Errorf("revert: record: %w", err)
	}
	return name, data, nil
}

// PrepareRecordDir creates the records' directory, 0700, when missing, and
// checks it and every directory above it as the journal's (ErrUnsafe).
func PrepareRecordDir(ctx context.Context, sys host.System, opts Options) error {
	return stateDir(ctx, sys, filepath.Join(opts.dir(), ControlsName), true)
}

// ReadRecord reads control's record: nil when there is none (no file, or
// no directory). The directory and every one above it must be root's
// alone, and the file root's and writable by root alone, read without
// following a link (ErrUnsafe); it is decoded strictly, an unknown field,
// data after the document or another schemaVersion refusing (ErrRecord).
func ReadRecord(ctx context.Context, sys host.System, opts Options, control string) (*ControlRecord, error) {
	name, err := RecordPath(opts, control)
	if err != nil {
		return nil, err
	}
	if err := stateDir(ctx, sys, filepath.Dir(name), false); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	data, err := readState(ctx, sys, name)
	if data == nil || err != nil {
		return nil, err
	}
	r, err := decodeRecord(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &r, nil
}

func decodeRecord(data []byte) (ControlRecord, error) {
	var r ControlRecord
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return ControlRecord{}, fmt.Errorf("revert: record: %w: %w", ErrRecord, err)
	}
	if dec.More() {
		return ControlRecord{}, fmt.Errorf("revert: record: data after the document: %w", ErrRecord)
	}
	return r, r.Check()
}
