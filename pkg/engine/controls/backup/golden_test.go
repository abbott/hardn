package backup_test

import (
	"bytes"
	"context"
	"io/fs"
	"strings"
	"syscall"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/backup"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// stableID stands for a plan's id in the goldens: the id's last part is
// random (revert.NewID).
const stableID = "20261027T120000Z-1a2b3c4d"

// stable is r with stableID for its change's id wherever the document and
// the report take it from: the plan, the result, its backup run, the
// apply's error.
func stable(r output.Run) output.Run {
	if p := r.Plan; p != nil {
		r.Plan = &engine.Plan{ID: stableID, Facts: p.Facts, Items: p.Items, Changes: p.Changes, Risk: p.Risk, Findings: p.Findings,
			Refused: p.Refused, Backups: p.Backups}
	}
	if r.Result == nil {
		return r
	}
	res := *r.Result
	if res.ID != "" {
		res.ID = stableID
	}
	if m := res.Backup; m != nil {
		mc := *m
		mc.RunID, mc.Dir = stableID, strings.TrimSuffix(m.Dir, m.RunID)+stableID
		res.Backup = &mc
	}
	if f := res.Failed; f != nil {
		fc := *f
		fc.ID = stableID
		if r.Err == error(f) {
			r.Err = &fc
		}
		res.Failed = &fc
	}
	r.Result = &res
	return r
}

// goldens pins r through the human renderer on the core set, and as the
// JSON document when json is set.
func goldens(t *testing.T, name string, r output.Run, json bool) {
	t.Helper()
	r = stable(r)
	uitest.Golden(t, "human/"+name, uitest.Core(), func(env term.Env) string { return output.Human(env, r, now) })
	if json {
		var b bytes.Buffer
		if err := output.WriteJSON(&b, output.Build(r)); err != nil {
			t.Fatal(err)
		}
		golden(t, "json/"+name+".json", b.Bytes())
	}
}

// applyOn plans controls with cfg on h, through the engine as cmd/hardn
// builds it (its backups as loaded), applies the plan when it is not
// refused, and returns the run as the output contract takes it.
func applyOn(t *testing.T, h host.System, loaded *config.Config, cfg *config.Config, controls ...engine.Control) output.Run {
	t.Helper()
	e := engineOn(h, loaded.BackupSettings(), nil)
	p, err := e.Plan(context.Background(), controls, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p.Err() != nil {
		r := run(output.ModePlan)
		r.Plan, r.Err = p, p.Err()
		return r
	}
	res, err := e.Apply(context.Background(), p)
	r := run(output.ModeApply)
	r.Plan, r.Result, r.Err = p, &res, err
	return r
}

// readOnly is a host whose /var/backups is on a read-only file system: a
// directory under it cannot be made.
type readOnly struct{ *hostfake.System }

func (h readOnly) MkdirAll(ctx context.Context, name string, perm fs.FileMode) error {
	if strings.HasPrefix(name, "/var/backups/") {
		return &fs.PathError{Op: "mkdir", Path: name, Err: syscall.EROFS}
	}
	return h.System.MkdirAll(ctx, name, perm)
}

// TestGoldens pins what a reviewer reads, as pkg/output renders it from
// the real engine on hostfake: the row on each host; the plan of turning
// backups off, and its apply, which says what it kept and that it was
// taken anyway; turning them on, which says what it kept alone; a path
// changed from one refused, its backup under the new path; the DNS
// control's change refused on that host, in the refused path's words; a
// new path refused by the control; and a change that fails at its backup
// on a read-only file system.
func TestGoldens(t *testing.T) {
	ctx := context.Background()
	t.Run("check", func(t *testing.T) {
		r := run(output.ModeCheck)
		for _, host := range []struct {
			enabled bool
			path    string
		}{{true, hostbackup.DefaultPath}, {false, hostbackup.DefaultPath}, {true, elsewhere}, {true, unsafePath}, {true, "backups"}, {true, "/etc/os-release"}} {
			h, cfg := newHost(t, host.enabled, host.path)
			reps, err := engineOn(h, cfg.BackupSettings(), nil).Check(ctx, backup.Controls(cfg))
			if err != nil {
				t.Fatal(err)
			}
			r.Reports = append(r.Reports, reps...)
		}
		goldens(t, "check", r, true)
	})
	off := func(c *config.Config) { c.EnableBackups = false }
	t.Run("plan-off", func(t *testing.T) {
		h, loaded := newHost(t, true, hostbackup.DefaultPath)
		_, p := planOn(t, h, loaded, edited(loaded, off))
		r := run(output.ModePlan)
		r.Plan = p
		goldens(t, "plan-off", r, true)
	})
	t.Run("applied-off", func(t *testing.T) {
		h, loaded := newHost(t, true, hostbackup.DefaultPath)
		goldens(t, "applied-off", applyOn(t, h, loaded, edited(loaded, off), backup.Settings{Config: loaded}), true)
	})
	t.Run("applied-on", func(t *testing.T) {
		h, loaded := newHost(t, false, hostbackup.DefaultPath)
		goldens(t, "applied-on", applyOn(t, h, loaded, edited(loaded, func(c *config.Config) { c.EnableBackups = true }), backup.Settings{Config: loaded}), false)
	})
	t.Run("applied-path-from-unsafe", func(t *testing.T) {
		h, loaded := newHost(t, true, unsafePath)
		moved := edited(loaded, func(c *config.Config) { c.BackupPath = hostbackup.DefaultPath })
		goldens(t, "applied-path-from-unsafe", applyOn(t, h, loaded, moved, backup.Settings{Config: loaded}), true)
	})
	t.Run("refused-dns-unsafe", func(t *testing.T) {
		h, loaded := newHost(t, true, unsafePath)
		goldens(t, "refused-dns-unsafe", applyOn(t, h, loaded, loaded, dnsctl.Nameservers{Config: loaded}), true)
	})
	t.Run("refused-path", func(t *testing.T) {
		h, loaded := newHost(t, true, hostbackup.DefaultPath)
		goldens(t, "refused-path", applyOn(t, h, loaded, edited(loaded, func(c *config.Config) { c.BackupPath = "/tmp/hardn" }), backup.Settings{Config: loaded}), false)
	})
	t.Run("failed-readonly", func(t *testing.T) {
		f, loaded := newHost(t, true, hostbackup.DefaultPath)
		r := applyOn(t, readOnly{f}, loaded, edited(loaded, off), backup.Settings{Config: loaded})
		if output.CodeOf(r.Err) != "BACKUP_WRITE_FAILED" {
			t.Errorf("failed with %v (%s)", r.Err, output.CodeOf(r.Err))
		}
		if content(f, hardnYML) != string(yml(true, hostbackup.DefaultPath)) {
			t.Error("hardn.yml changed")
		}
		goldens(t, "failed-readonly", r, true)
	})
}
