package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The backup settings a plan saves (refactor plan slice 3, decision 7
// (a), next/s3-control): a control that saves them (BackupsSaver) has its
// change planned and applied under them.

// saverControl is fileControl whose plan saves settings, as Backup's
// control saves backupPath and enableBackups.
type saverControl struct {
	fileControl
	settings backup.Settings
}

func (c saverControl) SavedBackups(Input) backup.Settings { return c.settings }

const (
	unsafePath = "/srv/shared/hardn" // under a directory anyone can write to
	newPath    = "/srv/backups/hardn"
	otherConf  = "/etc/example/other.conf"
)

// backupsHost is newHost with /srv/shared, which anyone can write to, and
// /srv/backups, root's.
func backupsHost(t *testing.T) *testHost {
	t.Helper()
	h := newHost(t)
	h.AddDir("/srv", 0o755, 0, 0)
	h.AddDir("/srv/shared", 0o777, 0, 0)
	h.AddDir("/srv/backups", 0o755, 0, 0)
	h.AddFile(confPath, []byte("old\n"), 0o644, 0, 0)
	h.AddFile(otherConf, []byte("old\n"), 0o644, 0, 0)
	return h
}

// TestPlanBackups: a plan's backups are taken under Options.Backups,
// hardn.yml as loaded, unless a control that saves the backup settings
// plans a change: then under the settings it saves, checked at plan time,
// for every change of that plan; a saver that plans nothing changes
// nothing of it; an empty path is the default; two savers that disagree
// conflict, two that agree do not; the plan made again under the lock
// must have the same settings.
func TestPlanBackups(t *testing.T) {
	ctx := context.Background()
	refused := backup.Settings{Path: unsafePath, Enabled: true}
	moved := backup.Settings{Path: newPath, Enabled: true}
	save := func(id string, s backup.Settings) saverControl {
		return saverControl{fileControl{id: id, path: confPath, want: "new\n"}, s}
	}
	other := fileControl{id: "example.other", path: otherConf, want: "new\n"}
	for _, tc := range []struct {
		name     string
		options  backup.Settings
		controls []Control
		want     backup.Settings
		refused  error
	}{
		{"hardn.yml's, refused", refused, []Control{other}, refused, backup.ErrUnsafeDir},
		{"the saver's, on a host whose own are refused", refused, []Control{save("example.saver", moved)}, moved, nil},
		{"the saver's, for every change of its plan", refused, []Control{save("example.saver", moved), other}, moved, nil},
		{"the saver's, refused", moved, []Control{save("example.saver", refused)}, refused, backup.ErrUnsafeDir},
		{"an empty path is the default", refused, []Control{save("example.saver", backup.Settings{})}, backup.Settings{Path: backup.DefaultPath}, nil},
		{"a saver that plans nothing", moved, []Control{saverControl{fileControl{id: "example.saver", path: confPath, want: "old\n"}, refused}, other}, moved, nil},
		{"two savers that agree", refused, []Control{save("example.saver", moved), saverControl{fileControl{id: "example.saver2", path: otherConf, want: "new\n"}, moved}}, moved, nil},
		{"two savers that disagree", refused, []Control{save("example.saver", moved), saverControl{fileControl{id: "example.saver2", path: otherConf, want: "new\n"}, refused}}, moved, ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := backupsHost(t)
			o := testOptions(h, &holds{}, nil, debian13)
			o.Backups = tc.options
			e := New(h, o)
			p, err := e.Plan(ctx, tc.controls, nil)
			if err != nil {
				t.Fatal(err)
			}
			if p.Backups != tc.want {
				t.Errorf("Plan.Backups %+v, want %+v", p.Backups, tc.want)
			}
			if tc.refused != nil {
				if !errors.Is(p.Err(), tc.refused) {
					t.Errorf("refused with %v, want %v", p.Err(), tc.refused)
				}
				return
			}
			if p.Err() != nil {
				t.Fatalf("refused: %v", p.Err())
			}
			res, err := e.Apply(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			dir := tc.want.Path + "/" + p.ID
			if res.Backup == nil || res.Backup.Dir != dir {
				t.Fatalf("Result.Backup %+v, want its run in %s", res.Backup, dir)
			}
			for _, c := range h.Calls() {
				if c.Op == "write-atomic" && strings.HasPrefix(c.Path, "/srv/") && !strings.HasPrefix(c.Path, dir+"/") {
					t.Errorf("a backup outside %s: %s", dir, c)
				}
			}
		})
	}
}

// TestPlanBackupsUnderTheLock: the plan made again under the run lock
// with other backup settings is not the plan shown.
func TestPlanBackupsUnderTheLock(t *testing.T) {
	ctx := context.Background()
	h := backupsHost(t)
	c := &switching{saverControl{fileControl{id: "example.saver", path: confPath, want: "new\n"}, backup.Settings{Path: newPath, Enabled: true}}}
	e := New(h, testOptions(h, &holds{}, nil, debian13))
	p, err := e.Plan(ctx, []Control{c}, nil)
	if err != nil || p.Err() != nil {
		t.Fatalf("Plan: %v, %v", err, p.Err())
	}
	c.settings.Path = backup.DefaultPath
	if _, err := e.Apply(ctx, p); !errors.Is(err, ErrPlanChanged) {
		t.Errorf("Apply: %v, want ErrPlanChanged", err)
	}
}

// switching is a saver whose settings the test changes between the plan
// and the apply.
type switching struct{ saverControl }

func (c *switching) SavedBackups(Input) backup.Settings { return c.settings }

// TestResultBackup: what an apply backed up, and where, is in its result
// once every change applied: the run's manifest, its entries taken anyway
// with backups off; nothing when the run took no backup, or the apply
// failed.
func TestResultBackup(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		enabled bool
		fail    bool
		backups bool
	}{
		{"backups on", true, false, true},
		{"backups off: taken anyway", false, false, true},
		{"a change with nothing to back up", true, false, false},
		{"a failed apply", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := backupsHost(t)
			o := testOptions(h, &holds{}, nil, debian13)
			o.Backups = backup.Settings{Path: newPath, Enabled: tc.enabled}
			var cs []Change
			if tc.backups {
				before := "old\n"
				cs = append(cs, writeChange("example/write", confPath, &before, "new\n", RiskPlain))
			}
			cs = append(cs, command("example/run", StageVerify, RiskPlain, []string{"example-check"}, nil))
			if tc.fail {
				h.failOn("example-check")
			}
			e := New(h, o)
			p, err := e.Plan(ctx, []Control{&stub{id: "example", title: "Example", changes: cs}}, nil)
			if err != nil || p.Err() != nil {
				t.Fatalf("Plan: %v, %v", err, p.Err())
			}
			res, _ := e.Apply(ctx, p)
			if !tc.backups || tc.fail {
				if res.Backup != nil {
					t.Errorf("Result.Backup %+v, want none", res.Backup)
				}
				return
			}
			m := res.Backup
			if m == nil || m.Dir != newPath+"/"+p.ID || m.RunID != p.ID || m.BackupsEnabled != tc.enabled || len(m.Entries) != 1 ||
				m.Entries[0].Path != confPath || m.Entries[0].TakenAnyway == tc.enabled {
				t.Fatalf("Result.Backup %+v", m)
			}
			disk, err := backup.Load(ctx, h, m.Dir)
			if err != nil || !slices.EqualFunc(disk.Entries, m.Entries, func(a, b backup.Entry) bool { return a.Path == b.Path && a.Seq == b.Seq }) {
				t.Errorf("the manifest on disk %+v (%v), the result's %+v", disk.Entries, err, m.Entries)
			}
		})
	}
}

// TestRollbackFromSavedPath: a change that saves a new backup path, then
// fails, is rolled back from the backups it took under that path: the
// journal names that run directory, and the rollback reads its manifest
// there.
func TestRollbackFromSavedPath(t *testing.T) {
	ctx := context.Background()
	h := backupsHost(t)
	o := testOptions(h, &holds{}, nil, debian13)
	o.Backups = backup.Settings{Path: unsafePath, Enabled: true}
	h.failOn("example-check")
	saver := saverControl{fileControl{id: "example.saver", path: confPath, want: "new\n"}, backup.Settings{Path: newPath, Enabled: true}}
	check := &stub{id: "example.check", title: "Example check", changes: []Change{command("example.check/run", StageVerify, RiskPlain, []string{"example-check"}, nil)}}
	e := New(h, o)
	p, err := e.Plan(ctx, []Control{saver, check}, nil)
	if err != nil || p.Err() != nil {
		t.Fatalf("Plan: %v, %v", err, p.Err())
	}
	n := len(h.Calls())
	_, err = e.Apply(ctx, p)
	if !errors.Is(err, ErrRolledBack) {
		t.Fatalf("Apply: %v, want it rolled back", err)
	}
	if got, _ := h.content(t, confPath); got != "old\n" {
		t.Errorf("%s after the rollback: %q", confPath, got)
	}
	manifest := newPath + "/" + p.ID + "/" + backup.ManifestName
	read := false
	for _, c := range h.Calls()[n:] {
		read = read || c.Op == "read-nofollow" && c.Path == manifest
		if strings.HasPrefix(c.Path, unsafePath) {
			t.Errorf("the refused path was used: %s", c)
		}
	}
	if !read {
		t.Errorf("the rollback did not read %s", manifest)
	}
	if _, err := revert.ReadState(ctx, h, revert.Options{}); err != nil {
		t.Errorf("journal: %v", err)
	}
}
