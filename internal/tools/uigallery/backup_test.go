package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/output"
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
// host, each saying what it backed up, turning backups off that it was
// taken anyway; item 2's inline errors, then a path saved, its backup
// under the new path, also under LANG=C; backups off: Verify, and turning
// them on declined; the unsafe path: Verify and item 1 refused, then a new
// path, planned under itself, and item 1 again (decision 7 (a)); the
// relative path, Verify and item 1 refused; the read-only host, Verify
// passing and the change failing at its backup; the runs, the SSH run's
// restore refused, and a restore through the engine (P-19); plan only,
// stopping at the plan; and the hosts' menu. Each
// change id's random suffix is written xxxxxxxx (sameIDs).
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
		{"backup-path-from-unsafe", []string{"backup/unsafe"}, []string{"3", "1", "2", "/var/backups/hardn", "y", "1", "y", "0"}, nil},
		{"backup-relative", []string{"backup/relative"}, []string{"3", "1", "0"}, nil},
		{"backup-readonly", []string{"backup/readonly"}, []string{"3", "1", "y", "0"}, nil},
		{"backup-runs", []string{"backup/runs"}, []string{"4", "3", "1", "0", "2", "1", "y", "0", "0", "0"}, nil},
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
			return sessionIDs(t, out)
		})
	}
}

// sessionID is a change id of the session, as backup.NewRunID makes it on
// the gallery's clock, and as a line may break it (changeID); the seeded
// runs' ids, of other days, are not.
var sessionID = regexp.MustCompile(`(` + galleryNow.Format("20060102T150405Z") + `-)((?:\x1b\[[0-9;]*m)*(?:\n[ ]*(?:\x1b\[[0-9;]*m)*)?)[0-9a-f]{8}`)

// sessionIDs is sameIDs for the session's change ids alone: each random
// suffix written xxxxxxxx, and a failure where one was cut elsewhere.
func sessionIDs(t *testing.T, out string) string {
	t.Helper()
	whole := len(sessionID.FindAllString(out, -1))
	out = sessionID.ReplaceAllString(out, "${1}${2}xxxxxxxx")
	if n := strings.Count(out, galleryNow.Format("20060102T150405Z")+"-"); n != whole {
		t.Errorf("%d change ids, %d of them whole", n, whole)
	}
	return out
}

// pathRow is the path's row of the screen r draws.
func pathRow(t *testing.T, r backupReading) component.KeyValue {
	t.Helper()
	for _, kv := range r.subMenu().Sections[0].Rows.Items {
		if kv.Key == pathKey {
			return kv
		}
	}
	t.Fatalf("no %s row", pathKey)
	return component.KeyValue{}
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
		{"runs", true, backup.DefaultPath, nil, true, 3, "Root only"},
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
		if got := pathRow(t, r).Note; got != tt.note {
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
		{"/tmp/hardn", "Others can change /tmp", backup.ErrUnsafeDir},
		{"/tmp", "Others can change /tmp", backup.ErrUnsafeDir},
		{"/etc/os-release", "Not a directory", syscall.ENOTDIR},
		{"/etc/os-release/backups", "Not a directory", syscall.ENOTDIR},
		{backup.DefaultPath, backup.DefaultPath + " is the backup path already", nil},
		{elsewherePath, "", nil},
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

// planOn plans controls on h with cfg on the real engine, its backups
// taken as settings say: hardn.yml's as loaded, as cmd/hardn's
// engineOptions takes them, or the edited configuration's, as the
// screen's changes take them (decision 7 (a)).
func planOn(t *testing.T, h *fakeHost, settings backup.Settings, cfg *config.Config, controls ...engine.Control) (*engine.Engine, *engine.Plan) {
	t.Helper()
	e := engine.New(h.system(), engine.Options{Revert: revert.Options{Now: clock, Hardn: galleryHardn}, Session: applySession,
		Backups: settings, Hold: (&session{}).hold, PID: applyPID})
	p, err := e.Plan(context.Background(), controls, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e, p
}

// TestBackupRefusesEveryChange: on the unsafe and relative hosts, the
// engine as cmd/hardn builds it, its backups as hardn.yml was loaded,
// refuses every change at its plan, Check's refusal, whatever the change:
// the screen's own saves, a path change to a safe path among them (its
// own backup goes to the path refused), and the DNS control's Configure
// DNS (the relative path is what origin/main's Backup screen saves, so
// the DNS screen's next change in that session is refused), with Check's
// error as it is and its code, no words of its own. The mock's path
// change, planned under the path it saves (decision 7 (a)), is not.
func TestBackupRefusesEveryChange(t *testing.T) {
	for _, tt := range []struct {
		host string
		want error
		code string
	}{{"unsafe", backup.ErrUnsafeDir, "UNSAFE_DIRECTORY"}, {"relative", backup.ErrSettings, "BACKUP_SETTINGS_INVALID"}} {
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
			_, p := planOn(t, h, loaded.BackupSettings(), c.cfg, c.control)
			if !errors.Is(p.Err(), tt.want) {
				t.Errorf("%s, %s: plan refused with %v, want %v", tt.host, c.what, p.Err(), tt.want)
			}
			// The refusal is Check's error as it is, with no Why or Fix
			// (gallery-backup.md §7, defect 12), and its code.
			var words *steps.Refusal
			if errors.As(p.Err(), &words) || output.CodeOf(p.Err()) != tt.code {
				t.Errorf("%s, %s: refused with words %+v, code %s, want none and %s", tt.host, c.what, words, output.CodeOf(p.Err()), tt.code)
			}
		}
		if _, p := planOn(t, h, moved.BackupSettings(), &moved, backupSave("backupPath")); p.Err() != nil {
			t.Errorf("%s: the path change planned under its path: %v", tt.host, p.Err())
		}
	}
}

// TestBackupTakenAnyway: item 1, planned under the settings it saves,
// takes its rollback backup either way; turning backups off takes it with
// them off, and its manifest says so (§3.16), which pkg/output says
// under what it kept (AfterFlow, since next/s3-control); turning them on
// takes it with them on, and says only what it kept.
func TestBackupTakenAnyway(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		host   string
		anyway bool
	}{{"default", true}, {"off", false}} {
		_, h, cfg := readHost(t, tt.host)
		edited := *cfg
		edited.EnableBackups = !cfg.EnableBackups
		e, p := planOn(t, h, edited.BackupSettings(), &edited, backupSave("enableBackups"))
		if p.Err() != nil {
			t.Fatalf("%s: plan: %v", tt.host, p.Err())
		}
		res, err := e.Apply(ctx, p)
		if err != nil {
			t.Fatalf("%s: apply: %v", tt.host, err)
		}
		dir := path.Join(backup.DefaultPath, p.ID)
		m, err := backup.Load(ctx, h.system(), dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Entries) != 1 || m.Entries[0].Path != hardnYML || m.Entries[0].TakenAnyway != tt.anyway || m.BackupsEnabled == tt.anyway {
			t.Errorf("%s: manifest %+v", tt.host, m)
		}
		after := flatText(output.AfterFlow(uitest.Core()[1].Env, output.Run{Mode: output.ModeApply, Plan: p, Result: &res}, galleryNow))
		want := "Backup of " + hardnYML + " kept in " + dir
		if tt.anyway {
			want += " Backed up anyway: the rollback needs it."
		}
		if !strings.Contains(after, want) || !tt.anyway && strings.Contains(after, "anyway") {
			t.Errorf("%s: after the change %q, want %q", tt.host, after, want)
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
		f.AddDir(sshd.DropInDir, 0o755, 0, 0)
		for _, file := range r.files {
			if !file.absent {
				f.AddFile(file.path, []byte(file.data), file.mode.FileMode(), 0, 0)
			}
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
			if e.File == nil {
				continue
			}
			copied, _, err := f.ReadFileNoFollow(ctx, path.Join(run.Dir(), e.File.Backup))
			if err != nil || !bytes.Equal(copied, seeded[e.File.Backup]) {
				t.Errorf("%s: %s: %v, %q", r.id, e.File.Backup, err, seeded[e.File.Backup])
			}
		}
	}
}

// playBackup plays the Backup screen on h, bh's host, plain, the user
// typing lines, a dry run with dryRun, and returns the session, the
// transcript and the exit status.
func playBackup(t *testing.T, bh backupHost, h *fakeHost, dryRun bool, lines ...string) (*session, string, int) {
	t.Helper()
	return playWith(t, backupPrefix+"/"+bh.name, func(s *session) func() error {
		if dryRun {
			s.sequence = "dry-run"
		}
		return func() error { return s.runBackupOn(context.Background(), bh, h) }
	}, lines...)
}

// TestBackupRestore: the restore (P-19) is an engine change: it puts the
// DNS run's files back as backup.Restore does, the engine backing up what
// it replaces first, in a run of its own, so that the restore can be
// undone as any change is; the SSH run's restore is refused before
// anything is planned; a dry run stops at the plan and writes nothing.
func TestBackupRestore(t *testing.T) {
	ctx := context.Background()
	bh, _ := findBackupHost("runs")
	h := bh.host()
	s, out, code := playBackup(t, bh, h, false, "4", "3", "1", "0", "2", "1", "y", "0", "0", "0")
	if code != exitOK || len(s.engineRuns) != 1 {
		t.Fatalf("exit %d, %d engines:\n%s", code, len(s.engineRuns), out)
	}
	if got := h.content(dns.ResolvConf); got != runOldDNS {
		t.Errorf("resolv.conf after the restore:\n%s", got)
	}
	if got := h.content(sshRunPath); got != runDropIn {
		t.Errorf("the drop-in, whose run's restore is refused:\n%s", got)
	}
	if !strings.Contains(flatText(out), lockoutRestore) {
		t.Errorf("the SSH run's restore is not refused:\n%s", out)
	}
	runs := readRuns(ctx, h.system(), backup.DefaultPath)
	if len(runs) != 4 || !strings.HasPrefix(runs[0].RunID, galleryNow.Format("20060102T150405Z")) {
		t.Fatalf("%d runs; the newest %+v", len(runs), runs[0])
	}
	var backedUp []string
	for _, e := range runs[0].Entries {
		backedUp = append(backedUp, e.Path)
	}
	if want := []string{hardnYML, dns.ResolvConf}; !slices.Equal(backedUp, want) {
		t.Errorf("the restore's own run backed up %q, want %q", backedUp, want)
	}
	h2 := bh.host()
	_, out2, _ := playBackup(t, bh, h2, true, "4", "2", "1", "0", "0", "0")
	if h2.content(dns.ResolvConf) != runNewDNS || !strings.Contains(flatText(out2), "No actual changes were made") {
		t.Errorf("the dry run:\n%s", out2)
	}
}

// TestBackupHeights: the screen on every host, Figma's frame, the
// alternative with a status row, the runs and a run, in their one form (a
// sub-menu has no compact form, and none is proposed), rendered at 80x24
// and at 80x40, in Unicode and ASCII: each is as many lines at either
// height, the prompt on its last, and fits 24 rows; the counts are
// gallery-backup.md §4's.
func TestBackupHeights(t *testing.T) {
	want := map[string]int{"default": 17, "off": 18, "elsewhere": 17, "unsafe": 17, "relative": 17, "readonly": 17, "runs": 17,
		"figma": 19, "status-row": 18, "runs list": 11, "run": 13}
	draws := map[string]func(component.Ctx) string{}
	for _, bh := range backupHosts() {
		r, _, _ := readHost(t, bh.name)
		draws[bh.name] = func(c component.Ctx) string { return r.subMenu().Render(c) }
	}
	figma, err := figmaMock()
	if err != nil {
		t.Fatal(err)
	}
	draws["figma"] = func(c component.Ctx) string { return figma.Render(c) }
	r, _, _ := readHost(t, "default")
	draws["status-row"] = func(c component.Ctx) string { return r.statusRowScreen().Render(c) }
	runs, _, _ := readHost(t, "runs")
	draws["runs list"] = func(c component.Ctx) string { return runsScreen(runs.runs).Render(c) }
	draws["run"] = func(c component.Ctx) string { return runScreen(runs.runs[0]).Render(c) }
	if len(draws) != len(want) {
		t.Fatalf("%d drawings, %d heights", len(draws), len(want))
	}
	for name, draw := range draws {
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
				env.Height = rows
				text := ansi.Strip(draw(component.NewCtx(env)))
				lines := strings.Split(text, "\n")
				if len(lines) != want[name] || len(lines) > 24 || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s at 80x%d, unicode %v: %d lines, the last %q; want %d, the prompt", name, rows, unicode, len(lines), lines[len(lines)-1], want[name])
				}
			}
		}
	}
}

// writesAfter are the calls h took after its first n that change
// something, each as the fake records it.
func writesAfter(h *fakeHost, n int) []string {
	var out []string
	for _, c := range h.f.Calls()[n:] {
		switch c.Op {
		case "write-atomic", "write-in-place", "append", "remove", "removeall", "rename", "mkdir", "chmod", "chown":
			out = append(out, c.String())
		}
	}
	return out
}

// TestBackupDrawAndVerifyWriteNothing: drawing the screen on every host,
// and Verify on it, live and under plan only, change nothing there, where
// origin/main's make the directory and write and remove a test file in
// either mode; no engine is built and no hold taken, and Verify says
// that nothing was written.
func TestBackupDrawAndVerifyWriteNothing(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		for _, bh := range backupHosts() {
			h := bh.host()
			n := len(h.f.Calls())
			s, out, code := playBackup(t, bh, h, dryRun, "3", "0")
			if code != exitOK || len(s.engineRuns) != 0 || s.holds != 0 {
				t.Errorf("%s, plan only %v: exit %d, %d engines, the hold taken %d times:\n%s", bh.name, dryRun, code, len(s.engineRuns), s.holds, out)
			}
			if !strings.Contains(flatText(out), nothingWritten) {
				t.Errorf("%s, plan only %v: Verify does not say that nothing was written:\n%s", bh.name, dryRun, out)
			}
			if got := writesAfter(h, n); len(got) > 0 {
				t.Errorf("%s, plan only %v: drawing the screen and Verify changed the host: %q", bh.name, dryRun, got)
			}
		}
	}
}

// TestBackupFigmaDrift: the three treatments of the frame's two copied
// status rows (decision 1). Figma's draws them as the frame has them; the
// screen proposed (screen-default) draws none, and says the state and
// the path once each in its section; the alternative (status-row) draws
// one of its own, the state, the section keeping the path, so that
// nothing is said twice. The two proposed differ above the menu alone.
func TestBackupFigmaDrift(t *testing.T) {
	c := component.NewCtx(term.Fixed(80, colorprofile.TrueColor, false, true))
	vm, err := figmaMock()
	if err != nil {
		t.Fatal(err)
	}
	figma := ansi.Strip(vm.Render(c))
	for _, want := range []string{"Backups              Enabled UFW installed, deny policy", "SSH Port             2230/tcp non-default"} {
		if !strings.Contains(figma, want) {
			t.Errorf("Figma's frame lacks %q:\n%s", want, figma)
		}
	}
	r, _, _ := readHost(t, "default")
	bare, own := ansi.Strip(r.subMenu().Render(c)), ansi.Strip(r.statusRowScreen().Render(c))
	for name, text := range map[string]string{"screen-default": bare, "status-row": own} {
		if strings.Contains(text, "UFW") || strings.Contains(text, "SSH Port") {
			t.Errorf("%s keeps a copied row:\n%s", name, text)
		}
		if strings.Count(text, "Enabled") != 1 || strings.Count(text, backup.DefaultPath) != 1 {
			t.Errorf("%s says the state or the path more than once:\n%s", name, text)
		}
	}
	if strings.Contains(bare, "Backups ") || !strings.Contains(own, "Backups              Enabled") {
		t.Errorf("screen-default has a status row, or status-row none:\n%s\n%s", bare, own)
	}
	_, bareMenu, _ := strings.Cut(bare, backupHeading)
	_, ownMenu, _ := strings.Cut(own, backupHeading)
	if bareMenu == "" || bareMenu != ownMenu {
		t.Errorf("the two proposed differ below their section:\n%s\n%s", bare, own)
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
