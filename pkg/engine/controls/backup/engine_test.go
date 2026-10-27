package backup_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/backup"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
)

// TestRefusedPathMoved (decision 7 (a)): on a host whose backupPath is
// refused, under /tmp or relative, the engine as cmd/hardn builds it, its
// backups as hardn.yml was loaded, plans and applies a change of the path
// to one backups can go in, under the path it saves: hardn.yml's
// backupPath line alone changes, and the change's own backup is under the
// new path, nothing under the old. Every other change is still refused
// there, the DNS control's and turning backups off among them, Check's
// refusal of the path as loaded, with its code; and the DNS control's
// change on a host whose path is not refused takes its backups as loaded,
// whatever backupPath the configuration it plans with gives.
func TestRefusedPathMoved(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		path string
		want error
		code string
	}{{unsafePath, hostbackup.ErrUnsafeDir, "UNSAFE_DIRECTORY"}, {"backups", hostbackup.ErrSettings, "BACKUP_SETTINGS_INVALID"}} {
		t.Run(tt.path, func(t *testing.T) {
			h, loaded := newHost(t, true, tt.path)
			off := edited(loaded, func(c *config.Config) { c.EnableBackups = false })
			for _, c := range []struct {
				what    string
				control engine.Control
				cfg     *config.Config
				backups hostbackup.Settings // the plan's backups
			}{
				{"Configure DNS", dnsctl.Nameservers{Config: loaded}, loaded, loaded.BackupSettings()},
				{"turn backups off", backup.Settings{Config: loaded}, off, off.BackupSettings()},
			} {
				p, err := engineOn(h, loaded.BackupSettings(), nil).Plan(ctx, []engine.Control{c.control}, c.cfg)
				if err != nil || !errors.Is(p.Err(), tt.want) || output.CodeOf(p.Err()) != tt.code || p.Backups != c.backups {
					t.Errorf("%s: %v, %v (%s), backups %+v; want refused, %s", c.what, err, p.Err(), output.CodeOf(p.Err()), p.Backups, tt.code)
				}
			}
			moved := edited(loaded, func(c *config.Config) { c.BackupPath = hostbackup.DefaultPath })
			before := content(h, hardnYML)
			e, p := planOn(t, h, loaded, moved)
			if p.Err() != nil || p.Backups != moved.BackupSettings() {
				t.Fatalf("the path change: %v, backups %+v", p.Err(), p.Backups)
			}
			n := len(h.Calls())
			res, err := e.Apply(ctx, p)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			want := strings.Replace(before, `backupPath: "`+tt.path+`"`, `backupPath: "/var/backups/hardn"`, 1)
			if got := content(h, hardnYML); got != want {
				t.Errorf("hardn.yml:\n%s\nwant\n%s", got, want)
			}
			dir := hostbackup.DefaultPath + "/" + p.ID
			if m := res.Backup; m == nil || m.Dir != dir || len(m.Entries) != 1 || m.Entries[0].Path != hardnYML {
				t.Errorf("Result.Backup %+v, want hardn.yml's in %s", res.Backup, dir)
			}
			for _, w := range writesAfter(h, n) {
				if !strings.HasPrefix(w, "write-atomic "+hardnYML) && !strings.Contains(w, " "+hostbackup.DefaultPath) && !strings.Contains(w, " /var/lib/hardn") {
					t.Errorf("a write outside hardn.yml, the new path's run and the journal: %s", w)
				}
			}
		})
	}

	// On a host whose path is not refused, another change's backups are
	// hardn.yml's as loaded, not the configuration's it plans with.
	h, loaded := newHost(t, true, hostbackup.DefaultPath)
	other := edited(loaded, func(c *config.Config) { c.BackupPath = elsewhere })
	p, err := engineOn(h, loaded.BackupSettings(), nil).Plan(ctx, []engine.Control{dnsctl.Nameservers{Config: loaded}}, other)
	if err != nil || p.Err() != nil || p.Empty() || p.Backups != loaded.BackupSettings() {
		t.Errorf("Configure DNS: %v, %v, backups %+v; want planned under %+v", err, p.Err(), p.Backups, loaded.BackupSettings())
	}
}

// failing is a control whose change fails after the save, so that the
// apply is rolled back.
type failing struct{}

func (failing) ID() string                          { return "example.check" }
func (failing) Title() string                       { return "Example check" }
func (failing) Applies(detect.Facts) (bool, string) { return true, "" }
func (failing) Check(context.Context, host.System, detect.Facts) (engine.Status, error) {
	return engine.Status{}, nil
}
func (failing) Plan(context.Context, host.System, engine.Input) ([]engine.Change, error) {
	return []engine.Change{{ID: "example.check/run", Summary: "Check the example", Stage: engine.StageVerify,
		Apply: func(context.Context, host.System, engine.Run) (engine.Ran, error) {
			return engine.Ran{}, errors.New("fixture: the check failed")
		}}}, nil
}

// TestRollbackFromNewPath (decision 7 (a)): a path change from a refused
// path, rolled back when a later change of its plan fails, puts hardn.yml
// back from the change's own backup, read under the new path, the one
// the journal names; nothing is read or written under the path refused.
func TestRollbackFromNewPath(t *testing.T) {
	ctx := context.Background()
	h, loaded := newHost(t, true, unsafePath)
	before := content(h, hardnYML)
	e, p := planOn(t, h, loaded, edited(loaded, func(c *config.Config) { c.BackupPath = hostbackup.DefaultPath }), failing{})
	if p.Err() != nil {
		t.Fatal(p.Err())
	}
	n := len(h.Calls())
	res, err := e.Apply(ctx, p)
	if !errors.Is(err, engine.ErrRolledBack) || res.Backup != nil {
		t.Fatalf("Apply: %v, backup %+v; want it rolled back", err, res.Backup)
	}
	if got := content(h, hardnYML); got != before {
		t.Errorf("hardn.yml after the rollback:\n%s\nwant\n%s", got, before)
	}
	manifest := hostbackup.DefaultPath + "/" + p.ID + "/" + hostbackup.ManifestName
	read := false
	for _, c := range h.Calls()[n:] {
		read = read || c.Op == "read-nofollow" && c.Path == manifest
		if strings.HasPrefix(c.Path, unsafePath) {
			t.Errorf("the path refused was used: %s", c)
		}
	}
	if !read {
		t.Errorf("the rollback did not read %s", manifest)
	}
}

// TestTakenAnyway (decision 7 (a), §3.16): turning backups off is applied
// with them off, so its own backup is taken anyway, and the result says
// so (Result.Backup, which pkg/output words); turning them on is applied
// with them on, and its backup is taken as any.
func TestTakenAnyway(t *testing.T) {
	ctx := context.Background()
	for _, on := range []bool{false, true} {
		h, loaded := newHost(t, !on, hostbackup.DefaultPath)
		e, p := planOn(t, h, loaded, edited(loaded, func(c *config.Config) { c.EnableBackups = on }))
		res, err := e.Apply(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		m := res.Backup
		if m == nil || m.BackupsEnabled != on || len(m.Entries) != 1 || m.Entries[0].TakenAnyway == on {
			t.Errorf("turning backups on %v: %+v", on, m)
		}
		disk, err := hostbackup.Load(ctx, h, m.Dir)
		if err != nil || disk.BackupsEnabled != on || disk.Entries[0].TakenAnyway == on {
			t.Errorf("turning backups on %v: the manifest on disk %+v, %v", on, disk, err)
		}
	}
}
