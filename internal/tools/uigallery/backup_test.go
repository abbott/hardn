package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"syscall"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// asciiColour is the core set's cell under LANG=C: colour, the ASCII
// glyph set, 80 columns (term.Resolve takes ASCII glyphs where the locale
// is not UTF-8).
var asciiColour = uitest.Cell{Name: "truecolor-dark-ascii-80", Env: term.Fixed(80, colorprofile.TrueColor, false, true)}

// backupCells are the Backup goldens' cells: the core set, and LANG=C's.
func backupCells() []uitest.Cell { return append(uitest.Core(), asciiColour) }

// TestBackupGolden pins each listing mock (backup.go) on the core set and
// under LANG=C: the screen on each host, decision 1's alternative,
// Verify's reports, the runs, a run and its directory. The golden files
// are testdata/golden/backup/<mock>.golden, which make golden-update
// rewrites.
func TestBackupGolden(t *testing.T) {
	for _, m := range backupMocks() {
		uitest.Golden(t, "backup/"+m.name, backupCells(), func(env term.Env) string {
			var out, errs bytes.Buffer
			args := append(lookArgs(t, env), m.name)
			if code := run(append([]string{backupPrefix}, args...), nil, offTerminal, &out, &errs); code != exitOK || errs.Len() > 0 {
				t.Errorf("%s: exit %d, stderr %q", m.name, code, errs.String())
			}
			return out.String()
		})
	}
}

// TestPlayBackupGolden plays the Backup mocks end to end and pins each
// transcript (gallery-backup.md §3): both ways of item 1 on the default
// host, the second with backups off, said taken anyway; item 2's inline
// errors, then a path saved, also under LANG=C; backups off: Verify, and
// turning them on declined; the unsafe and relative paths, every change
// refused by the engine, the path change too; the read-only host, Verify
// passing and the change failing at its backup; the runs and a restore
// (P-19); plan only, stopping at the plan; and the hosts' menu.
func TestPlayBackupGolden(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"backup-toggle", []string{"backup/default"}, []string{"1", "y", "1", "y", "0"}, nil},
		{"backup-path", []string{"backup/default"}, []string{"2", "backups", "/var/backups//hardn/", "/tmp/hardn", "/etc/os-release",
			"/var/backups/hardn", "/srv/backups/hardn", "y", "0"}, backupCells()},
		{"backup-off", []string{"backup/off"}, []string{"3", "1", "n", "0"}, nil},
		{"backup-unsafe", []string{"backup/unsafe"}, []string{"3", "1", "2", "/var/backups/hardn", "0"}, nil},
		{"backup-relative", []string{"backup/relative"}, []string{"3", "1", "0"}, nil},
		{"backup-readonly", []string{"backup/readonly"}, []string{"3", "1", "y", "0"}, nil},
		{"backup-runs", []string{"backup/runs"}, []string{"4", "2", "1", "y", "0", "0", "0"}, nil},
		{"backup-dry-run", []string{"-sequence", "dry-run", "backup/default"}, []string{"1", "2", "/srv/backups/hardn", "3", "0"}, nil},
		{"backup-hosts", []string{"backup"}, []string{"7", "4", "0", "0", "0"}, nil},
	}
	for _, tt := range tests {
		cells := tt.cells
		if cells == nil {
			cells = uitest.Core()
		}
		uitest.Golden(t, "play/"+tt.name, cells, func(env term.Env) string {
			args := append(lookArgs(t, env), tt.args...)
			code, out, errs, unread := playGallery(tt.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread", tt.name, args, code, errs, unread)
			}
			if strings.Contains(out, galleryNow.Format("20060102T150405Z")+"-") {
				t.Errorf("%s: a change id of the session, whose suffix is random, is in the transcript", tt.name)
			}
			return out
		})
	}
}

// readHost reads the backups of the host named name, as the screen does.
func readHost(t *testing.T, name string) (backupReading, *fakeHost, *config.Config) {
	t.Helper()
	bh, ok := findBackupHost(name)
	if !ok {
		t.Fatalf("no backup host %q", name)
	}
	h := bh.host()
	cfg, err := config.Read(context.Background(), h.f, hardnYML)
	if err != nil {
		t.Fatal(err)
	}
	return readBackups(context.Background(), h.system(), cfg), h, cfg
}

// TestBackupReadings holds each host's screen to what real code reads of
// it: hardn.yml's settings as config.Read loads them, Check's refusal
// (none, ErrUnsafeDir naming /tmp, ErrSettings), whether the directory is
// there, and the runs backup.Load reads; and the path row's note to them.
func TestBackupReadings(t *testing.T) {
	tests := []struct {
		host    string
		enabled bool
		dir     string
		refused error
		made    bool
		runs    int
		note    string
	}{
		{"default", true, backup.DefaultPath, nil, false, 0, "Not created yet"},
		{"off", false, backup.DefaultPath, nil, false, 0, "Not created yet"},
		{"elsewhere", true, "/srv/backups/hardn", nil, true, 0, "Root only"},
		{"unsafe", true, "/tmp/hardn-backups", backup.ErrUnsafeDir, false, 0, "Unsafe"},
		{"relative", true, "backups", backup.ErrSettings, false, 0, "Not absolute"},
		{"readonly", true, backup.DefaultPath, nil, false, 0, "Not created yet"},
		{"runs", true, backup.DefaultPath, nil, true, 2, "Root only"},
	}
	if len(tests) != len(backupHosts()) {
		t.Fatalf("%d hosts tested, %d hosts", len(tests), len(backupHosts()))
	}
	for _, tt := range tests {
		r, _, _ := readHost(t, tt.host)
		switch {
		case r.settings != (backup.Settings{Path: tt.dir, Enabled: tt.enabled}):
			t.Errorf("%s: settings %+v", tt.host, r.settings)
		case tt.refused == nil && r.check != nil, tt.refused != nil && !errors.Is(r.check, tt.refused):
			t.Errorf("%s: Check %v, want %v", tt.host, r.check, tt.refused)
		case r.made != tt.made || len(r.runs) != tt.runs:
			t.Errorf("%s: made %v, %d runs", tt.host, r.made, len(r.runs))
		}
		if got := r.subMenu().Sections[0].Rows.Items[1].Note; got != tt.note {
			t.Errorf("%s: the path's note %q, want %q", tt.host, got, tt.note)
		}
	}
	if r, _, _ := readHost(t, "unsafe"); unsafeDir(r.check) != "/tmp" {
		t.Errorf("unsafe: Check names %q, want /tmp: %v", unsafeDir(r.check), r.check)
	}
}

// TestPathCheckIsCheck holds the path field's check to backup.Check on
// the default host: it refuses what Check refuses, in the words of its
// kind, and takes what Check takes, but a path not clean (the mock's own
// rule, which Open would clean unsaid) and the path in use (a warning).
func TestPathCheckIsCheck(t *testing.T) {
	_, h, _ := readHost(t, "default")
	ctx, sys := context.Background(), h.system()
	check := pathCheck(ctx, sys, backup.DefaultPath)
	tests := []struct {
		entry, want string
		checked     error // what backup.Check says of the entry
	}{
		{"", "Backup path cannot be empty", backup.ErrSettings},
		{"backups", "Not an absolute path", backup.ErrSettings},
		{"./var/backups", "Not an absolute path", backup.ErrSettings},
		{"/var/backups//hardn/", "Write it as /var/backups/hardn", nil},
		{"/var/backups/../tmp/hardn", "Write it as /var/tmp/hardn", nil},
		{"/tmp/hardn", "Other users can change /tmp", backup.ErrUnsafeDir},
		{"/tmp", "Other users can change /tmp", backup.ErrUnsafeDir},
		{"/etc/os-release", "Not a directory", syscall.ENOTDIR},
		{"/etc/os-release/backups", "Not a directory", syscall.ENOTDIR},
		{backup.DefaultPath, backup.DefaultPath + " is the backup path already", nil},
		{"/srv/backups/hardn", "", nil},
		{"/var/backups", "", nil},
		{"/root/backups", "", nil},
	}
	for _, tt := range tests {
		err := check(tt.entry)
		got := ""
		if err != nil {
			got = err.Error()
		}
		real := backup.Check(ctx, sys, backup.Settings{Path: tt.entry, Enabled: true}, backup.NewRunID(clock()), nil)
		switch {
		case got != tt.want:
			t.Errorf("%q: %q, want %q", tt.entry, got, tt.want)
		case tt.checked == nil && real != nil, tt.checked != nil && !errors.Is(real, tt.checked):
			t.Errorf("%q: backup.Check says %v, want %v", tt.entry, real, tt.checked)
		}
	}
}

// planOn plans controls on h with cfg on the real engine, its backups as
// loaded sets them, as the screen's changes and cmd/hardn's engineOptions
// take them from hardn.yml as loaded.
func planOn(t *testing.T, h *fakeHost, loaded, cfg *config.Config, controls ...engine.Control) (*engine.Engine, *engine.Plan) {
	t.Helper()
	e := engine.New(h.system(), engine.Options{Revert: revert.Options{Now: clock, Hardn: galleryHardn}, Session: applySession,
		Backups: loaded.BackupSettings(), Hold: (&session{}).hold, PID: applyPID})
	p, err := e.Plan(context.Background(), controls, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e, p
}

// TestBackupRefusesEveryChange: on the unsafe and relative hosts, the
// engine refuses every change at its plan, Check's refusal, whatever the
// change: the screen's own saves, a path change to a safe path among them
// (its own backup goes to the path refused), and the DNS control's
// Configure DNS (the relative path is what origin/main's Backup screen
// saves, so the DNS screen's next change in that session is refused).
func TestBackupRefusesEveryChange(t *testing.T) {
	for _, tt := range []struct {
		host string
		want error
	}{{"unsafe", backup.ErrUnsafeDir}, {"relative", backup.ErrSettings}} {
		_, h, cfg := readHost(t, tt.host)
		h.withHardnYML(backupYML(true, cfg.BackupPath) + "nameservers:\n  - \"192.0.2.54\"\n")
		loaded, err := config.Read(context.Background(), h.f, hardnYML)
		if err != nil {
			t.Fatal(err)
		}
		off, moved := *loaded, *loaded
		off.EnableBackups, moved.BackupPath = false, backup.DefaultPath
		for _, c := range []struct {
			what    string
			cfg     *config.Config
			control engine.Control
		}{
			{"turn backups off", &off, backupSave("enableBackups")},
			{"a path change", &moved, backupSave("backupPath")},
			{"Configure DNS", loaded, dnsctl.Nameservers{Config: loaded}},
		} {
			if _, p := planOn(t, h, loaded, c.cfg, c.control); !errors.Is(p.Err(), tt.want) {
				t.Errorf("%s, %s: plan refused with %v, want %v", tt.host, c.what, p.Err(), tt.want)
			}
		}
	}
}

// TestBackupTakenAnyway: a change applied with backups off takes its
// rollback backup anyway, and its manifest says so (§3.16), which the
// screen says after the result (anyway); with backups on it is taken, and
// nothing is said.
func TestBackupTakenAnyway(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		host   string
		anyway bool
	}{{"off", true}, {"default", false}} {
		_, h, cfg := readHost(t, tt.host)
		edited := *cfg
		edited.EnableBackups = !cfg.EnableBackups
		e, p := planOn(t, h, cfg, &edited, backupSave("enableBackups"))
		if p.Err() != nil {
			t.Fatalf("%s: plan: %v", tt.host, p.Err())
		}
		if _, err := e.Apply(ctx, p); err != nil {
			t.Fatalf("%s: apply: %v", tt.host, err)
		}
		m, err := backup.Load(ctx, h.system(), path.Join(backup.DefaultPath, p.ID))
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Entries) != 1 || m.Entries[0].Path != hardnYML || m.Entries[0].TakenAnyway != tt.anyway || m.BackupsEnabled == tt.anyway {
			t.Errorf("%s: manifest %+v", tt.host, m)
		}
		if got := anyway(ctx, h.system(), cfg.BackupSettings(), p); got != tt.anyway {
			t.Errorf("%s: anyway %v, want %v", tt.host, got, tt.anyway)
		}
	}
}

// TestRunFilesAreBackups holds the runs host's seeded runs to what
// pkg/host/backup writes for the same files: each copy, and manifest.json
// byte for byte but its creation time, which Open takes from the clock.
func TestRunFilesAreBackups(t *testing.T) {
	ctx := context.Background()
	for _, r := range earlierRuns() {
		f := hostfake.New(host.Options{})
		f.AddDir("/var/backups", 0o755, 0, 0)
		for _, file := range r.files {
			f.AddFile(file.path, []byte(file.data), file.mode.FileMode(), 0, 0)
		}
		run, err := backup.Open(f, backup.Settings{Path: backup.DefaultPath, Enabled: r.enabled}, r.id)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range r.files {
			if _, err := run.Backup(ctx, file.path, backup.RollbackRequired); err != nil {
				t.Fatal(err)
			}
		}
		written, _, err := f.ReadFileNoFollow(ctx, path.Join(run.Dir(), backup.ManifestName))
		if err != nil {
			t.Fatal(err)
		}
		var m backup.Manifest
		if err := json.Unmarshal(written, &m); err != nil {
			t.Fatal(err)
		}
		m.Created = r.at
		want, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		seeded := runFiles(r)
		if got := string(seeded[backup.ManifestName]); got != string(want)+"\n" {
			t.Errorf("%s: manifest.json\n%s\nwant\n%s", r.id, got, want)
		}
		for _, e := range m.Entries {
			copied, _, err := f.ReadFileNoFollow(ctx, path.Join(run.Dir(), e.File.Backup))
			if err != nil || !bytes.Equal(copied, seeded[e.File.Backup]) {
				t.Errorf("%s: %s: %v, %q", r.id, e.File.Backup, err, seeded[e.File.Backup])
			}
		}
	}
}

// TestBackupHeights: the screen on every host, the runs and a run, in
// their one form (a sub-menu has no compact form), fit 80x24 down to the
// prompt, in Unicode and ASCII; the counts are gallery-backup.md §4's.
func TestBackupHeights(t *testing.T) {
	want := map[string]int{"default": 17, "off": 18, "runs": 17, "status-row": 19}
	for _, unicode := range []bool{true, false} {
		c := component.NewCtx(term.Fixed(80, colorprofile.TrueColor, unicode, true))
		for _, bh := range backupHosts() {
			r, _, _ := readHost(t, bh.name)
			lines := strings.Count(ansi.Strip(r.subMenu().Render(c)), "\n") + 1
			if lines > 24 {
				t.Errorf("%s, unicode %v: %d lines", bh.name, unicode, lines)
			}
			if n, ok := want[bh.name]; ok && lines != n {
				t.Errorf("%s, unicode %v: %d lines, want %d", bh.name, unicode, lines, n)
			}
		}
		r, _, _ := readHost(t, "default")
		if n := strings.Count(ansi.Strip(r.statusRowScreen().Render(c)), "\n") + 1; n != want["status-row"] {
			t.Errorf("status-row, unicode %v: %d lines", unicode, n)
		}
	}
}

// TestBackupUsage: the listing and play refuse what they do not take,
// with status 2 and nothing on stdout.
func TestBackupUsage(t *testing.T) {
	for _, args := range [][]string{
		{backupPrefix, "nope"},
		{backupPrefix, "screen-default", "-width", "60"},
		{"play", "backup/nope"},
		{"play", "-fail", "1", "backup/default"},
		{"play", "-sequence", "failure", "backup/default"},
	} {
		var out, errs bytes.Buffer
		if code := run(args, nil, offTerminal, &out, &errs); code != exitUsage || out.Len() > 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errs.String())
		}
	}
	var out bytes.Buffer
	if code := run([]string{backupPrefix, "-list"}, nil, offTerminal, &out, &bytes.Buffer{}); code != exitOK || len(strings.Fields(out.String())) != len(backupMocks()) {
		t.Errorf("-list: exit %d, %q", code, out.String())
	}
}
