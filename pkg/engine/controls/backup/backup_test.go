package backup_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"syscall"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/backup"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
)

// lstatFails is a host whose Lstat of one path fails with err.
type lstatFails struct {
	*hostfake.System
	path string
	err  error
}

func (h lstatFails) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	if name == h.path {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: h.err}
	}
	return h.System.Lstat(ctx, name)
}

// TestCheck is every note the path can have (decision 3), with its state,
// on the gallery's hosts: Configured, "Root only" or "Not created yet",
// with backups on or off; Not Configured, "Unsafe" (naming the directory
// in Read's DirError), "Not absolute", "Not a directory"; Unknown, "Can't
// tell", with Check's error. Check writes nothing.
func TestCheck(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name    string
		enabled bool
		path    string
		lstat   string // a path whose Lstat fails: permission denied
		nilCfg  bool
		state   engine.State
		detail  string
		dir     string // the directory a DirError names
		err     bool
	}{
		{name: "the default, not made yet", enabled: true, path: hostbackup.DefaultPath, state: engine.Configured, detail: "Not created yet"},
		{name: "backups off", path: hostbackup.DefaultPath, state: engine.Configured, detail: "Not created yet"},
		{name: "no hardn.yml: the defaults", nilCfg: true, state: engine.Configured, detail: "Not created yet"},
		{name: "elsewhere, root's", enabled: true, path: elsewhere, state: engine.Configured, detail: "Root only"},
		{name: "under /tmp", enabled: true, path: unsafePath, state: engine.NotConfigured, detail: "Unsafe", dir: "/tmp"},
		{name: "relative", enabled: true, path: "backups", state: engine.NotConfigured, detail: "Not absolute"},
		{name: "a file", enabled: true, path: "/etc/os-release", state: engine.NotConfigured, detail: "Not a directory", dir: "/etc/os-release"},
		{name: "under a file", enabled: true, path: "/etc/os-release/backups", state: engine.NotConfigured, detail: "Not a directory", dir: "/etc/os-release"},
		{name: "can't tell", enabled: true, path: elsewhere, lstat: elsewhere, state: engine.Unknown, detail: "Can't tell", err: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, cfg := newHost(t, tt.enabled, tt.path)
			var sys host.System = f
			if tt.lstat != "" {
				sys = lstatFails{f, tt.lstat, fs.ErrPermission}
			}
			if tt.nilCfg {
				cfg = nil
			}
			n := len(f.Calls())
			st, err := backup.Settings{Config: cfg}.Check(ctx, sys, debian13)
			if st.State != tt.state || st.Detail != tt.detail || (err != nil) != tt.err {
				t.Errorf("%v %q, %v; want %v %q, an error %v", st.State, st.Detail, err, tt.state, tt.detail, tt.err)
			}
			r := backup.Read(ctx, sys, cfg)
			var de *hostbackup.DirError
			if tt.dir != "" && (!errors.As(r.Err, &de) || de.Dir != tt.dir) {
				t.Errorf("Read: %v, want a DirError naming %s", r.Err, tt.dir)
			}
			if len(st.Evidence) == 0 {
				t.Error("no evidence")
			}
			if w := writesAfter(f, n); len(w) > 0 {
				t.Errorf("Check wrote: %v", w)
			}
		})
	}
}

// TestField holds the path field's check (decisions 5 and 6) to
// backup.Check on the default host: it refuses what Check refuses, in the
// words of its kind, and takes what Check takes, but a path not written
// clean and the path in use, a warning.
func TestField(t *testing.T) {
	ctx := context.Background()
	f, _ := newHost(t, true, hostbackup.DefaultPath)
	for _, tt := range []struct {
		entry, words string
		warn         bool
		checked      error // what backup.Check says of the entry
	}{
		{"", "Backup path cannot be empty", false, hostbackup.ErrSettings},
		{"backups", "Not an absolute path", false, hostbackup.ErrSettings},
		{"./var/backups", "Not an absolute path", false, hostbackup.ErrSettings},
		{"/var/backups//hardn/", "Write it as /var/backups/hardn", false, nil},
		{"/var/backups/../tmp/hardn", "Write it as /var/tmp/hardn", false, nil},
		{"/tmp/hardn", "Others can change /tmp", false, hostbackup.ErrUnsafeDir},
		{"/tmp", "Others can change /tmp", false, hostbackup.ErrUnsafeDir},
		{"/etc/os-release", "Not a directory", false, syscall.ENOTDIR},
		{"/etc/os-release/backups", "Not a directory", false, syscall.ENOTDIR},
		{hostbackup.DefaultPath, hostbackup.DefaultPath + " is the backup path already", true, nil},
		{elsewhere, "", false, nil},
		{"/var/backups", "", false, nil},
		{"/root/backups", "", false, nil},
	} {
		n := len(f.Calls())
		err := backup.Field(ctx, f, hostbackup.DefaultPath, tt.entry)
		var fe *backup.FieldError
		switch {
		case tt.words == "" && err != nil:
			t.Errorf("%q: %v, want it taken", tt.entry, err)
		case tt.words != "" && (!errors.As(err, &fe) || fe.Words != tt.words || fe.Error() != tt.words || fe.Warn != tt.warn):
			t.Errorf("%q: %#v, want %q, warn %v", tt.entry, err, tt.words, tt.warn)
		}
		real := hostbackup.Check(ctx, f, hostbackup.Settings{Path: tt.entry, Enabled: true}, "20261027T120000Z-0a1b2c3d", nil)
		if tt.checked == nil && real != nil || tt.checked != nil && !errors.Is(real, tt.checked) {
			t.Errorf("%q: backup.Check says %v, want %v", tt.entry, real, tt.checked)
		}
		if w := writesAfter(f, n); len(w) > 0 {
			t.Errorf("%q: the field wrote: %v", tt.entry, w)
		}
	}
	if err := backup.Field(ctx, lstatFails{f, "/srv/other", fs.ErrPermission}, hostbackup.DefaultPath, "/srv/other/hardn"); err == nil ||
		!strings.HasPrefix(err.Error(), "Can't check it: ") || !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Check failing: %v", err)
	}
}

// TestRefusal is the words of each of Check's refusals of a path, with its
// code (the registry's, by the error the report wraps), and none for an
// error that is not one.
func TestRefusal(t *testing.T) {
	ctx := context.Background()
	f, _ := newHost(t, true, hostbackup.DefaultPath)
	for _, tt := range []struct {
		path, why, fix, code string
	}{
		{"backups", `"backups" is not an absolute path`, "set an absolute path with Backup's item 2", "BACKUP_SETTINGS_INVALID"},
		{unsafePath, "Others can change /tmp, so backups can't go in /tmp/hardn-backups", "choose a directory only root can change, with Backup's item 2", "UNSAFE_DIRECTORY"},
		{"/etc/os-release/backups", "/etc/os-release is not a directory", "choose another path with Backup's item 2", "BACKUP_PATH_NOT_DIRECTORY"},
	} {
		err := hostbackup.Check(ctx, f, hostbackup.Settings{Path: tt.path, Enabled: true}, "20261027T120000Z-0a1b2c3d", nil)
		r := backup.Refusal(tt.path, err)
		if r == nil || r.Why != tt.why || r.Fix != tt.fix || r.Code != tt.code || output.CodeOf(r) != tt.code || !errors.Is(r, err) {
			t.Errorf("%s: %+v (%s), want %q, %q, %s", tt.path, r, output.CodeOf(r), tt.why, tt.fix, tt.code)
		}
	}
	for _, err := range []error{nil, errors.New("something else"), hostbackup.ErrRunExists, &fs.PathError{Op: "lstat", Path: "/x", Err: fs.ErrPermission}} {
		if r := backup.Refusal("/x", err); r != nil {
			t.Errorf("%v: %+v, want none", err, r)
		}
	}
}

// planOn plans Backup's control with cfg, the copy a change edits, on h,
// through the engine as cmd/hardn builds it, its backups as loaded.
func planOn(t *testing.T, h *hostfake.System, loaded *config.Config, cfg *config.Config, more ...engine.Control) (*engine.Engine, *engine.Plan) {
	t.Helper()
	e := engineOn(h, loaded.BackupSettings(), nil)
	p, err := e.Plan(context.Background(), append([]engine.Control{backup.Settings{Config: loaded}}, more...), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e, p
}

// TestPlan: the save of the keys that changed alone, edited in place
// (config.Edit), the rest of hardn.yml and its comments as they were;
// nothing when neither changed; a new path refused first, with a report,
// where backups cannot go; the save's refusals; nothing written by any.
func TestPlan(t *testing.T) {
	for _, tt := range []struct {
		name    string
		enabled bool
		path    string
		edit    func(c *config.Config)
		diff    []string // the diff's changed lines
		code    string   // a refusal's code
		why     string
	}{
		{name: "backups turned off", enabled: true, path: hostbackup.DefaultPath, edit: func(c *config.Config) { c.EnableBackups = false },
			diff: []string{"-enableBackups: true               # Backup files before modifying them", "+enableBackups: false               # Backup files before modifying them"}},
		{name: "backups turned on", path: hostbackup.DefaultPath, edit: func(c *config.Config) { c.EnableBackups = true },
			diff: []string{"-enableBackups: false               # Backup files before modifying them", "+enableBackups: true               # Backup files before modifying them"}},
		{name: "a new path", enabled: true, path: hostbackup.DefaultPath, edit: func(c *config.Config) { c.BackupPath = elsewhere },
			diff: []string{"-backupPath: \"/var/backups/hardn\"  # Path to store backups", "+backupPath: \"/srv/backups/hardn\"  # Path to store backups"}},
		{name: "a new path, from one refused", enabled: true, path: unsafePath, edit: func(c *config.Config) { c.BackupPath = hostbackup.DefaultPath },
			diff: []string{"-backupPath: \"/tmp/hardn-backups\"  # Path to store backups", "+backupPath: \"/var/backups/hardn\"  # Path to store backups"}},
		{name: "nothing changed", enabled: true, path: hostbackup.DefaultPath, edit: func(*config.Config) {}},
		{name: "a relative path", enabled: true, path: hostbackup.DefaultPath, edit: func(c *config.Config) { c.BackupPath = "backups" },
			code: "BACKUP_SETTINGS_INVALID", why: `"backups" is not an absolute path`},
		{name: "an empty path", enabled: true, path: hostbackup.DefaultPath, edit: func(c *config.Config) { c.BackupPath = "" },
			code: "BACKUP_SETTINGS_INVALID", why: `"" is not an absolute path`},
		{name: "a path not written clean", enabled: true, path: hostbackup.DefaultPath, edit: func(c *config.Config) { c.BackupPath = "/srv/backups//hardn/" },
			code: "BACKUP_SETTINGS_INVALID", why: `"/srv/backups//hardn/" is not written as /srv/backups/hardn`},
		{name: "a path others can change", enabled: true, path: hostbackup.DefaultPath, edit: func(c *config.Config) { c.BackupPath = "/tmp/hardn" },
			code: "UNSAFE_DIRECTORY", why: "Others can change /tmp, so backups can't go in /tmp/hardn"},
		{name: "a file", enabled: true, path: hostbackup.DefaultPath, edit: func(c *config.Config) { c.BackupPath = "/etc/os-release" },
			code: "BACKUP_PATH_NOT_DIRECTORY", why: "/etc/os-release is not a directory"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, loaded := newHost(t, tt.enabled, tt.path)
			n := len(h.Calls())
			_, p := planOn(t, h, loaded, edited(loaded, tt.edit))
			if w := writesAfter(h, n); len(w) > 0 {
				t.Errorf("the plan wrote: %v", w)
			}
			if tt.code != "" {
				var r *steps.Refusal
				if !errors.As(p.Err(), &r) || r.Why != tt.why || output.CodeOf(p.Err()) != tt.code || len(p.Refused) != 1 {
					t.Errorf("refused with %v (%s), want %q, %s", p.Err(), output.CodeOf(p.Err()), tt.why, tt.code)
				}
				return
			}
			if p.Err() != nil {
				t.Fatalf("refused: %v", p.Err())
			}
			if len(tt.diff) == 0 {
				if !p.Empty() {
					t.Errorf("planned %+v, want nothing", p.Changes)
				}
				return
			}
			if len(p.Changes) != 1 || p.Changes[0].ID != backup.ChangeSave || p.Changes[0].Title != "Save the configuration" {
				t.Fatalf("changes %+v", p.Changes)
			}
			var changed []string
			for _, l := range strings.Split(p.Changes[0].Diff, "\n") {
				if (strings.HasPrefix(l, "-") || strings.HasPrefix(l, "+")) && !strings.HasPrefix(l, "---") && !strings.HasPrefix(l, "+++") {
					changed = append(changed, l)
				}
			}
			if strings.Join(changed, "\n") != strings.Join(tt.diff, "\n") {
				t.Errorf("the diff's lines:\n%s\nwant\n%s", strings.Join(changed, "\n"), strings.Join(tt.diff, "\n"))
			}
		})
	}
}

// TestPlanSaveRefusals: the save's own refusals, with their reports: no
// hardn.yml read (CONFIG_NOT_LOADED) and hardn.yml changed since it was
// read (CONFIG_CHANGED).
func TestPlanSaveRefusals(t *testing.T) {
	h, loaded := newHost(t, true, hostbackup.DefaultPath)
	defaults := config.DefaultConfig()
	defaults.EnableBackups = false
	e := engineOn(h, loaded.BackupSettings(), nil)
	p, err := e.Plan(context.Background(), []engine.Control{backup.Settings{Config: defaults}}, defaults)
	if err != nil || output.CodeOf(p.Err()) != "CONFIG_NOT_LOADED" {
		t.Errorf("no hardn.yml: %v, %v", err, p.Err())
	}
	h.AddFile(hardnYML, append(yml(true, hostbackup.DefaultPath), "# edited since\n"...), 0o600, 0, 0)
	_, p = planOn(t, h, loaded, edited(loaded, func(c *config.Config) { c.EnableBackups = false }))
	if output.CodeOf(p.Err()) != "CONFIG_CHANGED" {
		t.Errorf("hardn.yml changed: %v", p.Err())
	}
	_, p = planOn(t, h, loaded, edited(loaded, func(c *config.Config) { c.BackupPath = elsewhere }))
	if output.CodeOf(p.Err()) != "CONFIG_CHANGED" {
		t.Errorf("hardn.yml changed, a new path: %v", p.Err())
	}
}

// TestSavedBackups: the settings the plan saves are the edited copy's,
// which the engine takes the plan's backups under; none with no
// configuration.
func TestSavedBackups(t *testing.T) {
	_, loaded := newHost(t, true, hostbackup.DefaultPath)
	cfg := edited(loaded, func(c *config.Config) { c.BackupPath, c.EnableBackups = elsewhere, false })
	if got := (backup.Settings{Config: loaded}).SavedBackups(engine.Input{Config: cfg}); got != (hostbackup.Settings{Path: elsewhere}) {
		t.Errorf("SavedBackups %+v", got)
	}
	if got := (backup.Settings{}).SavedBackups(engine.Input{}); got != (hostbackup.Settings{}) {
		t.Errorf("no configuration: %+v", got)
	}
}
