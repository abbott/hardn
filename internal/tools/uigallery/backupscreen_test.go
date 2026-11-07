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
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	backupctl "github.com/abbott/hardn/pkg/engine/controls/backup"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The Backup screen on the simulated hosts (next/s3-screen): hardn's own
// screen (pkg/screens/backup), its changes on the real engine, and the
// restore's mock. Nothing here runs a command or writes a file outside
// the fake.

// asciiColour is the core set's cell under LANG=C: colour, the ASCII
// glyph set, 80 columns (term.Resolve takes ASCII glyphs where the locale
// is not UTF-8).
var asciiColour = uitest.Cell{Name: "truecolor-dark-ascii-80", Env: term.Fixed(80, colorprofile.TrueColor, false, true)}

// TestPlayBackupGolden plays the Backup screen end to end and pins each
// transcript, the sequences the mocks had (gallery-backup.md §3.1) on the
// real screen: both ways of item 1 on the default host, each saying what
// it backed up, turning backups off that it was taken anyway; item 2's
// inline refusals, then a path saved, its backup under the new path, also
// under LANG=C; backups off: Verify, and turning them on declined; the
// unsafe path: Verify and item 1 refused, then a new path, planned under
// itself, and item 1 again (decision 7 (a)); the relative path, Verify
// and item 1 refused; the read-only host, Verify passing and the change
// failing at its backup; the runs, read only; plan only, stopping at the
// plan; the hosts' menu; the main menu's 7, live and under plan only (as
// hardn's main menu opens it since next/s3-replace, decision 10: the
// gallery's main menu is Figma's mock, its indicator "Mode Live" either
// way), a change stopping at its plan; and the restore's mock. Each
// change id's random suffix is written xxxxxxxx (sessionIDs).
func TestPlayBackupGolden(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"backup", nil, []string{"7", "3", "0", "0"}, nil},
		{"backup-plan-only", []string{"-sequence", "dry-run"}, []string{"7", "1", "0", "0"}, nil},
		{"backup-toggle", []string{"backup/default"}, []string{"1", "y", "1", "y", "0"}, nil},
		{"backup-path", []string{"backup/default"}, []string{"2", "backups", "/var/backups//hardn/", "/tmp/hardn", "/etc/os-release",
			"/var/backups/hardn", "/srv/backups/hardn", "y", "0"}, append(uitest.Core(), asciiColour)},
		{"backup-off", []string{"backup/off"}, []string{"3", "1", "n", "0"}, nil},
		{"backup-path-from-unsafe", []string{"backup/unsafe"}, []string{"3", "1", "2", "/var/backups/hardn", "y", "1", "y", "0"}, nil},
		{"backup-relative", []string{"backup/relative"}, []string{"3", "1", "0"}, nil},
		{"backup-readonly", []string{"backup/readonly"}, []string{"3", "1", "y", "0"}, nil},
		{"backup-runs", []string{"backup/runs"}, []string{"4", "3", "0", "1", "0", "0", "0"}, nil},
		{"backup-dry-run", []string{"-sequence", "dry-run", "backup/default"}, []string{"1", "2", "/srv/backups/hardn", "3", "0"}, nil},
		{"backup-hosts", []string{"backup"}, []string{"7", "4", "0", "0", "0"}, nil},
		{"backup-restore", []string{restoreMock}, []string{"3", "1", "0", "2", "1", "y", "0", "0"}, nil},
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

// playBackup plays the Backup screen on h, bh's host, plain, the user
// typing lines, a dry run with dryRun, and returns the session, the
// transcript and the exit status.
func playBackup(t *testing.T, bh backupHost, h *fakeHost, dryRun bool, lines ...string) (*session, string, int) {
	t.Helper()
	return playWith(t, backupPrefix+"/"+bh.name, func(s *session) func() error {
		if dryRun {
			s.sequence = "dry-run"
		}
		return func() error { return s.backupScreen(context.Background(), bh, h) }
	}, lines...)
}

// backupHostNamed is the Backup screen's host named name.
func backupHostNamed(t *testing.T, name string) backupHost {
	t.Helper()
	bh, ok := findBackupHost(name)
	if !ok {
		t.Fatalf("no backup host %q", name)
	}
	return bh
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

// TestBackupScreenHosts: every host the Backup screen plays on draws there,
// reading only, live and under plan only, and Verify writes nothing
// either (decision 3): no engine built, no hold taken, nothing changed;
// its Backup Path row is what Backup's control reads of the host's
// hardn.yml (backupctl.Read: Check's note), and its item 4 the runs
// backup.Runs lists.
func TestBackupScreenHosts(t *testing.T) {
	want := map[string]struct{ path, runs string }{
		"default":   {"/var/backups/hardn Not created yet", "None yet"},
		"off":       {"/var/backups/hardn Not created yet", "None yet"},
		"elsewhere": {"/srv/backups/hardn Root only", "None yet"},
		"unsafe":    {"/tmp/hardn-backups Unsafe", ""},
		"relative":  {"backups Not absolute", ""},
		"readonly":  {"/var/backups/hardn Not created yet", "None yet"},
		"runs":      {"/var/backups/hardn Root only", "3 runs"},
	}
	if len(backupHosts()) != len(want) {
		t.Errorf("%d hosts, want %d", len(backupHosts()), len(want))
	}
	for _, bh := range backupHosts() {
		for _, dryRun := range []bool{false, true} {
			h := bh.host()
			cfg, err := config.Read(context.Background(), h.f, hardnYML)
			if err != nil {
				t.Fatal(err)
			}
			r := backupctl.Read(context.Background(), h.system(), cfg)
			if got := r.Settings.Path + " " + r.Note(); got != want[bh.name].path {
				t.Errorf("%s: the control reads %q, want %q", bh.name, got, want[bh.name].path)
			}
			s, out, code := playBackup(t, bh, h, dryRun, "3", "0")
			if code != exitOK || len(s.engineRuns) != 0 || s.holds != 0 || !strings.Contains(out, "Info: "+backupPrefix+"/"+bh.name) {
				t.Errorf("%s, plan only %t: exit %d, %d engines, the hold taken %d times:\n%s", bh.name, dryRun, code, len(s.engineRuns), s.holds, out)
			}
			flat := flatText(out)
			if !strings.Contains(flat, "Backup Path "+want[bh.name].path) || !strings.Contains(flat, strings.Join(strings.Fields("4) Backup Runs "+want[bh.name].runs+" 0) Return"), " ")) {
				t.Errorf("%s: the screen does not show %+v:\n%s", bh.name, want[bh.name], out)
			}
			if !strings.Contains(flat, "Nothing was written.") {
				t.Errorf("%s: Verify does not say that nothing was written:\n%s", bh.name, out)
			}
			if got := writesAfter(h, 0); len(got) > 0 {
				t.Errorf("%s, plan only %t: drawing the screen and Verify changed the host: %q", bh.name, dryRun, got)
			}
		}
	}
}

// TestPlayBackup plays each change of the Backup screen on its host and
// checks what the transcript shows, in order, what changed on the host
// outside hardn's own directories, and how often the gallery's hold was
// taken: once by the failed backup's rollback. A refusal before asking, a
// declined change and a dry run change nothing.
func TestPlayBackup(t *testing.T) {
	const yml = "write-atomic " + hardnYML
	for _, tc := range []struct {
		name, host string
		dryRun     bool
		lines      []string
		holds      int
		screen     []string // what the transcript shows, in order
		order      []string // what changed on the host, outside hardn's directories, in order
	}{
		{"toggle", "default", false, []string{"1", "y", "1", "y", "0"}, 0,
			[]string{"Turn backups off? [y/N, 0 or q]: y", "Backed up anyway: the rollback needs it.", "Backup State Disabled",
				"Turn backups on? [y/N, 0 or q]: y", "Backup State Enabled", "4) Backup Runs 2 runs"},
			[]string{yml, yml}},
		{"path", "default", false, []string{"2", "/srv/backups/hardn", "y", "0"}, 0,
			[]string{"Use /srv/backups/hardn? [y/N, 0 or q]: y", "kept in /srv/backups/hardn/", "Backup Path /srv/backups/hardn Root only"},
			[]string{yml}},
		{"declined", "off", false, []string{"1", "n", "0"}, 0, []string{"Turn backups on? [y/N, 0 or q]: n", "Operation Cancelled"}, nil},
		{"moved", "unsafe", false, []string{"1", "2", "/var/backups/hardn", "y", "1", "y", "0"}, 0,
			[]string{"Warning Others can change /tmp, so backups can't go in /tmp/hardn-", "backups. Choose a directory only root can change.",
				"Use /var/backups/hardn? [y/N, 0 or q]: y", "Operation Successful", "Turn backups off? [y/N, 0 or q]: y", "Operation Successful"},
			[]string{yml, yml}},
		{"relative", "relative", false, []string{"1", "0"}, 0, []string{`Warning "backups" is not an absolute path. Start it with /.`}, nil},
		{"read-only", "readonly", false, []string{"1", "y", "0"}, 1,
			[]string{"Failed BACKUP_WRITE_FAILED", "Can't back up /etc/hardn/hardn.yml in /var/backups/hardn: read-only file system", "Nothing was changed."}, nil},
		{"dry run", "default", true, []string{"1", "2", "/srv/backups/hardn", "0"}, 0,
			[]string{"Turn Backups Off", "No actual changes were made to your system.", "Backup Path /srv/backups/hardn Not created yet",
				"No actual changes were made to your system.", "Backup Path /var/backups/hardn Not created yet"}, nil},
		{"runs", "runs", false, []string{"4", "2", "0", "0", "0"}, 0,
			[]string{"1) 2026-10-19 14:30 1 file, taken anyway", "Run 20261018T091500Z-1a2b3c4d", "/etc/resolv.conf 77 bytes"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bh := backupHostNamed(t, tc.host)
			h := bh.host()
			s, out, code := playBackup(t, bh, h, tc.dryRun, tc.lines...)
			if w := notShown(out, tc.screen); w != "" {
				t.Errorf("the transcript does not show %q after what came before it:\n%s", w, out)
			}
			if code != exitOK || s.holds != tc.holds {
				t.Errorf("exit %d, the gallery's hold taken %d times; want %d", code, s.holds, tc.holds)
			}
			if all := hostChanges(h); !slices.Equal(all, tc.order) {
				t.Errorf("the host changed %q, want %q", all, tc.order)
			}
		})
	}
}

// planOn plans controls on h with cfg on the real engine, as cmd/hardn's
// engineOptions builds it: its backups as hardn.yml was loaded (settings).
// A plan of Backup's control takes the settings it saves (decision 7
// (a)).
func planOn(t *testing.T, h *fakeHost, settings backup.Settings, cfg *config.Config, controls ...engine.Control) *engine.Plan {
	t.Helper()
	e := engine.New(h.system(), engine.Options{Revert: revert.Options{Now: clock, Hardn: galleryHardn}, Session: applySession,
		Backups: settings, Hold: (&session{}).hold, PID: applyPID})
	p, err := e.Plan(context.Background(), controls, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestBackupRefusesEveryChange: on the unsafe and relative hosts, the
// engine as cmd/hardn builds it, its backups as hardn.yml was loaded,
// refuses every change at its plan, Check's refusal: turning backups
// off, and the DNS control's Configure DNS (the relative path is what
// origin/main's Backup screen saves, so the DNS screen's next change in
// that session is refused), each with its code, and, since
// next/s3-control, a refused path's words, as pkg/output reports a
// refusal (gallery-backup.md §7, defect 12). A path change to a safe path
// is planned under the path it saves (decision 7 (a)).
func TestBackupRefusesEveryChange(t *testing.T) {
	for _, tt := range []struct {
		host, why string
		want      error
		code      string
	}{
		{"unsafe", "Others can change /tmp, so backups can't go in /tmp/hardn-backups", backup.ErrUnsafeDir, "UNSAFE_DIRECTORY"},
		{"relative", `"backups" is not an absolute path`, backup.ErrSettings, "BACKUP_SETTINGS_INVALID"},
	} {
		h := backupHostNamed(t, tt.host).host()
		cfg, err := config.Read(context.Background(), h.f, hardnYML)
		if err != nil {
			t.Fatal(err)
		}
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
			{"turn backups off", &off, backupctl.Settings{Config: loaded}},
			{"Configure DNS", loaded, dnsctl.Nameservers{Config: loaded}},
		} {
			p := planOn(t, h, loaded.BackupSettings(), c.cfg, c.control)
			if !errors.Is(p.Err(), tt.want) || output.CodeOf(p.Err()) != tt.code {
				t.Errorf("%s, %s: plan refused with %v (%s), want %v, %s", tt.host, c.what, p.Err(), output.CodeOf(p.Err()), tt.want, tt.code)
			}
			d := output.Build(output.Run{Mode: output.ModePlan, Plan: p, Err: p.Err()})
			if len(d.Errors) != 1 || d.Errors[0].Why != tt.why || !strings.HasSuffix(d.Errors[0].Fix, "with Backup's item 2") {
				t.Errorf("%s, %s: reported as %+v, want %q", tt.host, c.what, d.Errors, tt.why)
			}
		}
		if p := planOn(t, h, loaded.BackupSettings(), &moved, backupctl.Settings{Config: loaded}); p.Err() != nil || p.Backups != moved.BackupSettings() {
			t.Errorf("%s: the path change, planned under its path: %v, %+v", tt.host, p.Err(), p.Backups)
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

// TestBackupRestore: the restore's mock (P-19) is an engine change: it
// puts the DNS run's files back as backup.Restore does, the engine backing
// up what it replaces first, in a run of its own, so that the restore can
// be undone as any change is; the SSH run's restore is refused before
// anything is planned; a dry run stops at the plan and writes nothing.
// hardn's own Backup Runs offers no restore (decision 9).
func TestBackupRestore(t *testing.T) {
	var h *fakeHost
	s, out, code := playWith(t, restoreMock, func(s *session) func() error {
		return func() error { return s.restoreMock(context.Background()) }
	}, "3", "1", "0", "2", "1", "y", "0", "0")
	if code != exitOK || len(s.engineRuns) != 1 {
		t.Fatalf("exit %d, %d engines:\n%s", code, len(s.engineRuns), out)
	}
	h = s.engineRuns[0].host
	if got := h.content(dns.ResolvConf); got != runOldDNS {
		t.Errorf("resolv.conf after the restore:\n%s", got)
	}
	if got := h.content(sshRunPath); got != runDropIn {
		t.Errorf("the drop-in, whose run's restore is refused:\n%s", got)
	}
	if !strings.Contains(flatText(out), lockoutRestore) || !strings.Contains(out, "Info: "+restoreMock) {
		t.Errorf("the SSH run's restore is not refused, or the mock not labelled:\n%s", out)
	}
	runs, err := backup.Runs(context.Background(), h.system(), backup.DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
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
	s2, out2, _ := playWith(t, restoreMock, func(s *session) func() error {
		s.sequence = "dry-run"
		return func() error { return s.restoreMock(context.Background()) }
	}, "2", "1", "0", "0")
	if len(s2.engineRuns) != 1 || s2.engineRuns[0].host.content(dns.ResolvConf) != runNewDNS || !strings.Contains(flatText(out2), "No actual changes were made") {
		t.Errorf("the dry run:\n%s", out2)
	}
	_, out3, _ := playBackup(t, backupHostNamed(t, "runs"), runsHost(), false, "4", "2", "0", "0", "0")
	if strings.Contains(out3, "Restore") {
		t.Errorf("hardn's Backup Runs offers a restore:\n%s", out3)
	}
}

// TestPlayBackupUsage: a host that is not one, -fail and a sequence other
// than normal or dry-run stop play with exit status 2 before stdin is
// read; the Backup listing is gone; the main menu's 7 leads to the screen
// on the default host, and Figma's frame is played by name.
func TestPlayBackupUsage(t *testing.T) {
	for _, args := range [][]string{
		{"play", backupPrefix + "/nope"},
		{"play", "-fail", "1", backupPrefix + "/default"},
		{"play", "-sequence", "failure", backupPrefix + "/default"},
		{"play", "-fail", "1", restoreMock},
		{backupPrefix},
	} {
		var out, errs bytes.Buffer
		read := false
		code := run(args, func() *prompt.Reader { read = true; return noInput() }, offTerminal, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 || read {
			t.Errorf("%v: exit %d, stdout %q, stderr %q, stdin read %t; want exit %d, a message, no output", args, code, out.String(), errs.String(), read, exitUsage)
		}
	}
	if backupLinks[mainMenuMock+"/7"] != backupMainHost {
		t.Errorf("the main menu's 7 leads to %q", backupLinks[mainMenuMock+"/7"])
	}
	code, out, errs, unread := playGallery([]string{"7", "0", "0"}, "-profile", "plain")
	if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, "Info: "+backupMainHost) || !strings.Contains(out, "# Backup Settings #") {
		t.Errorf("play, 7: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	code, out, _, unread = playGallery([]string{"0"}, "-profile", "plain", "sub-menu/05-backup")
	if code != exitOK || unread != 0 || !strings.Contains(out, "# Manage Hardn Backups #") {
		t.Errorf("play sub-menu/05-backup: exit %d, %d unread:\n%s", code, unread, out)
	}
}
