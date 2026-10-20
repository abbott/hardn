package output_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
)

// TestAppend: each run is one line of the audit log, its Record: the same
// document stdout gets, with the time; the directory is made root's alone,
// the file 0600, and a second run appends after the first.
func TestAppend(t *testing.T) {
	ctx := context.Background()
	sys := hostfake.New(host.Options{})
	sys.AddDir("/var/log", 0o755, 0, 0)
	fx := fixtures()
	for i, at := range []time.Time{fixtureNow, fixtureNow.Add(time.Minute)} {
		if err := output.Append(ctx, sys, output.AuditPath, output.NewRecord(fx[3+i].run, at)); err != nil {
			t.Fatal(err)
		}
	}
	dir, _ := sys.Entry("/var/log/hardn")
	file, _ := sys.Entry(output.AuditPath)
	if dir.Mode.Perm() != output.AuditDirMode || !dir.Mode.IsDir() || file.Mode.Perm() != output.AuditFileMode || file.UID != 0 {
		t.Errorf("directory %v, file %v (uid %d)", dir.Mode, file.Mode, file.UID)
	}
	sc := bufio.NewScanner(bytes.NewReader(file.Data))
	sc.Buffer(nil, 1<<20)
	var outcomes []output.Outcome
	for sc.Scan() {
		var rec output.Record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("line %d: %v", len(outcomes)+1, err)
		}
		if rec.SchemaVersion != output.SchemaVersion || rec.Time.IsZero() || rec.Result.ID != fixtureID {
			t.Errorf("record %+v", rec)
		}
		outcomes = append(outcomes, rec.Result.Outcome)
	}
	if len(outcomes) != 2 || outcomes[0] != output.OutcomeAwaiting || outcomes[1] != output.OutcomeRolledBack {
		t.Errorf("lines %v, want the awaiting run, then the rolled-back one", outcomes)
	}
	appends := 0
	for _, c := range sys.Calls() {
		if c.Op == "append" {
			appends++
		}
		if c.Op == "write-atomic" {
			t.Errorf("the log was rewritten: %v", c)
		}
	}
	if appends != 2 {
		t.Errorf("%d appends, want 2", appends)
	}
}

// TestAppendRefuses: a directory another user owns or can write, and a
// symlink at the log's path, are refused, and nothing is written.
func TestAppendRefuses(t *testing.T) {
	ctx := context.Background()
	rec := output.NewRecord(fixtures()[0].run, fixtureNow)
	for _, tc := range []struct {
		name  string
		setup func(*hostfake.System)
		want  error
	}{
		{"writable by others", func(s *hostfake.System) { s.AddDir("/var/log/hardn", 0o777, 0, 0) }, output.ErrAuditDir},
		{"another user's", func(s *hostfake.System) { s.AddDir("/var/log/hardn", 0o750, 1000, 1000) }, output.ErrAuditDir},
		{"a symlink at the log", func(s *hostfake.System) {
			s.AddDir("/var/log/hardn", 0o750, 0, 0)
			s.AddFile("/etc/shadow", []byte("root:x\n"), 0o640, 0, 0)
			s.AddSymlink(output.AuditPath, "/etc/shadow")
		}, host.ErrSymlink},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sys := hostfake.New(host.Options{})
			sys.AddDir("/var/log", 0o755, 0, 0)
			tc.setup(sys)
			if err := output.Append(ctx, sys, output.AuditPath, rec); !errors.Is(err, tc.want) {
				t.Errorf("Append = %v, want %v", err, tc.want)
			}
			if e, ok := sys.Entry("/etc/shadow"); ok && string(e.Data) != "root:x\n" {
				t.Errorf("written through the link: %q", e.Data)
			}
			if e, ok := sys.Entry(output.AuditPath); ok && e.Mode.IsRegular() {
				t.Errorf("a log was written: %q", e.Data)
			}
		})
	}
}
