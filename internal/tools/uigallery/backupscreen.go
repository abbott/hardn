package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/output"
	backupscreen "github.com/abbott/hardn/pkg/screens/backup"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The Backup screen on the simulated hosts (refactor plan slice 3, unit
// (c), next/s3-screen): hardn's Backup screen itself (pkg/screens/backup),
// drawn from each host's hardn.yml and backup directory through Backup's
// control, every change on the real engine, on the hosts the Backup mocks
// were drawn for, which it replaces (docs/engine/screen-backup.md). The
// main menu's 5 leads to it on the default host, as hardn's main menu 5
// runs it since next/s3-replace, under plan only too (-sequence dry-run);
// "play backup" is the menu of the hosts and "play backup/<host>" one.
//
// "play backup-restore" is the one mock kept: the restore of a backup run
// (P-19, decision 9), a later unit's, whose control is the gallery's
// (restoreControl), on the runs host. The screen's Backup Runs is read
// only.

// backupPrefix names the Backup screen's hosts in play: "backup" is the
// menu of them and "backup/<host>" one.
const backupPrefix = "backup"

// restoreMock names the restore's mock in play.
const restoreMock = "backup-restore"

// backupMainHost is the host the main menu's 5 leads to.
const backupMainHost = backupPrefix + "/default"

// backupLinks lead from a menu mock's item to the Backup screen on a host,
// by the mock's name and the item's key, beside links: the main menu's 5,
// which opened Figma's 05-BACKUP frame (sub-menu/05-backup) until this
// unit, kept among the mocks as Figma's record.
var backupLinks = map[string]string{mainMenuMock + "/5": backupMainHost}

// figmaRecords are the mocks kept as Figma's record of a screen hardn now
// draws itself, by name, each with the item that leads to hardn's screen
// in its place: no item leads to them, and play starts at them by name
// ("play sub-menu/05-backup"), as -screens draws them.
var figmaRecords = map[string]string{"sub-menu/05-backup": mainMenuMock + "/5"}

func init() {
	// The elsewhere host's backup path is hardn's own state, as
	// /var/backups/hardn is: the record play ends with leaves its writes
	// out (stateDirs).
	stateDirs = append(stateDirs, elsewherePath)
}

// elsewherePath is the elsewhere host's backup path.
const elsewherePath = "/srv/backups/hardn"

// backupHost is one simulated host the Backup screen runs on.
type backupHost struct {
	// name is the host's, after backupPrefix: "default".
	name string
	// label and about are its item in the menu of the hosts.
	label, about string
	// note says what the host is.
	note string
	// host returns the simulated host, fresh, with its hardn.yml.
	host func() *fakeHost
}

// backupYML is hardn.yml with the backup keys as hardn.yml.example writes
// them (pkg/config embed.go), set to enabled and dir.
func backupYML(enabled bool, dir string) string {
	return fmt.Sprintf("username: alice\nenableBackups: %t               # Backup files before modifying them\nbackupPath: %q  # Path to store backups\n", enabled, dir)
}

// backupOn returns a Debian 13 host whose hardn.yml sets enabled and dir,
// with a /tmp anyone can write to, as Linux's is (its sticky bit no
// exception to Check: atomic.md §2.14).
func backupOn(enabled bool, dir string) func() *fakeHost {
	return func() *fakeHost {
		h := simulate(debian13Platform).withHardnYML(backupYML(enabled, dir))
		h.f.AddDir("/tmp", 0o777, 0, 0)
		return h
	}
}

// backupHosts are the hosts, in the menu's order: the mocks' seven.
func backupHosts() []backupHost {
	const deb = hostFQDN + " runs Debian 13. "
	return []backupHost{
		{name: "default", label: "Default", about: "Backups on, /var/backups/hardn", host: backupOn(true, backup.DefaultPath),
			note: deb + "hardn.yml keeps the defaults: backups on, in /var/backups/hardn, not made yet."},
		{name: "off", label: "Backups off", about: "enableBackups: false", host: backupOn(false, backup.DefaultPath),
			note: deb + "hardn.yml says enableBackups: false."},
		{name: "elsewhere", label: "Another path", about: elsewherePath, host: elsewhereHost,
			note: deb + "hardn.yml says backupPath: /srv/backups/hardn, a directory root made 0700."},
		{name: "unsafe", label: "Unsafe path", about: "/tmp/hardn-backups", host: backupOn(true, "/tmp/hardn-backups"),
			note: deb + "hardn.yml says backupPath: /tmp/hardn-backups, under /tmp, which anyone can write to."},
		{name: "relative", label: "Relative path", about: "backupPath: backups", host: backupOn(true, "backups"),
			note: deb + "hardn.yml says backupPath: backups, a relative path, as origin/main's Backup screen saves one."},
		{name: "readonly", label: "Read-only", about: "/var/backups read-only", host: readOnlyHost,
			note: deb + "/var/backups is on a read-only file system, which Check cannot see without writing."},
		{name: "runs", label: "Earlier runs", about: "Three changes backed up", host: runsHost,
			note: deb + "Three earlier changes were backed up to /var/backups/hardn: an SSH port, a DNS change, then a save with backups off."},
	}
}

// findBackupHost returns the host named name, after backupPrefix.
func findBackupHost(name string) (backupHost, bool) {
	i := slices.IndexFunc(backupHosts(), func(h backupHost) bool { return h.name == name })
	if i < 0 {
		return backupHost{}, false
	}
	return backupHosts()[i], true
}

// elsewhereHost is Debian 13 backing up to /srv/backups/hardn, made 0700
// by root.
func elsewhereHost() *fakeHost {
	h := backupOn(true, elsewherePath)()
	h.f.AddDir(elsewherePath, 0o700, 0, 0)
	return h
}

// readOnlyHost is Debian 13 whose /var/backups is on a read-only file
// system (fakeHost.readOnly).
func readOnlyHost() *fakeHost {
	h := backupOn(true, backup.DefaultPath)()
	h.readOnly = []string{"/var/backups"}
	return h
}

// The earlier runs' contents: hardn.yml and resolv.conf as they were,
// before the changes since, and hardn's sshd drop-in as the port change
// wrote it.
const (
	runOldYML  = "# hardn's configuration (" + hostFQDN + ")\nusername: alice\nnameservers:\n  - \"192.0.2.54\"\n"
	runOldDNS  = "# Generated by dhcpcd from eth0.dhcp\nsearch example.net\nnameserver 192.0.2.1\n"
	runNewDNS  = "# Generated by dhcpcd from eth0.dhcp\nsearch example.net\nnameserver 192.0.2.54\n"
	runDropIn  = "Port 2230\n"
	sshRunPath = sshd.HardnDropIn
)

// runFile is one file an earlier run backed up: its path, and its
// content and mode as they were, or absent, not there before the change.
type runFile struct {
	path, data string
	mode       backup.Perm
	absent     bool
}

// earlierRun is one run runsHost holds: its id and when, whether
// backups were on, and each file backed up.
type earlierRun struct {
	id      string
	at      time.Time
	enabled bool
	files   []runFile
}

// earlierRuns are runsHost's runs: an SSH port change, which made
// hardn's sshd drop-in; a DNS change with backups on; then a change of
// hardn.yml with backups off, whose rollback backup was taken anyway.
func earlierRuns() []earlierRun {
	return []earlierRun{
		{id: "20261017T083000Z-9c0d1e2f", at: time.Date(2026, 10, 17, 8, 30, 0, 0, time.UTC), enabled: true,
			files: []runFile{{path: sshRunPath, absent: true}}},
		{id: "20261018T091500Z-1a2b3c4d", at: time.Date(2026, 10, 18, 9, 15, 0, 0, time.UTC), enabled: true,
			files: []runFile{{dns.ResolvConf, runOldDNS, 0o644, false}, {hardnYML, runOldYML, 0o600, false}}},
		{id: "20261019T143000Z-5e6f7a8b", at: time.Date(2026, 10, 19, 14, 30, 0, 0, time.UTC),
			files: []runFile{{hardnYML, runOldYML, 0o600, false}}},
	}
}

// runsHost is Debian 13 with backups on, whose /var/backups/hardn holds
// earlierRuns, each as pkg/host/backup writes a run (runFiles), its
// copies 0600 and its directory 0700, root's; resolv.conf lists the
// nameserver the DNS run's change set, and the drop-in the port change
// wrote is there.
func runsHost() *fakeHost {
	h := backupOn(true, backup.DefaultPath)()
	h.f.AddFile(dns.ResolvConf, []byte(runNewDNS), 0o644, 0, 0)
	h.f.AddFile(sshRunPath, []byte(runDropIn), 0o644, 0, 0)
	h.f.AddDir(backup.DefaultPath, 0o700, 0, 0)
	for _, r := range earlierRuns() {
		dir := path.Join(backup.DefaultPath, r.id)
		h.f.AddDir(dir, 0o700, 0, 0)
		for name, data := range runFiles(r) {
			h.f.AddFile(path.Join(dir, name), data, 0o600, 0, 0)
		}
	}
	return h
}

// runFiles are the files of run r's directory, by name: each copy,
// numbered as Run.Backup numbers it, and manifest.json, a backup.Manifest
// marshalled as Run.save writes it, with r's time as its creation time.
func runFiles(r earlierRun) map[string][]byte {
	m := backup.Manifest{SchemaVersion: backup.SchemaVersion, RunID: r.id, Created: r.at, BackupsEnabled: r.enabled, Entries: []backup.Entry{}}
	files := map[string][]byte{}
	for i, f := range r.files {
		e := backup.Entry{Seq: i + 1, Path: f.path, Kind: backup.KindAbsent, RollbackRequired: true, TakenAnyway: !r.enabled}
		if !f.absent {
			sum := sha256.Sum256([]byte(f.data))
			e.Kind = backup.KindFile
			e.File = &backup.Content{Backup: fmt.Sprintf("%04d-%s", i+1, path.Base(f.path)), Mode: f.mode, Size: int64(len(f.data)), SHA256: hex.EncodeToString(sum[:])}
			files[e.File.Backup] = []byte(f.data)
		}
		m.Entries = append(m.Entries, e)
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		panic(err) // not reached: a Manifest marshals
	}
	files[backup.ManifestName] = append(b, '\n')
	return files
}

// backupStart returns the hosts' menu, the host, or the restore's mock
// that args name ("backup", "backup/<host>", "backup-restore"), and
// whether they name one; an error for a host that is not one.
func backupStart(args []string) (string, bool, error) {
	if len(args) == 1 && args[0] == restoreMock {
		return restoreMock, true, nil
	}
	if len(args) != 1 || (args[0] != backupPrefix && !strings.HasPrefix(args[0], backupPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], backupPrefix+"/"); args[0] != backupPrefix {
		if _, ok := findBackupHost(name); !ok {
			var names []string
			for _, h := range backupHosts() {
				names = append(names, backupPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no backup host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}

// backupHostMenu is the menu of the Backup screen's hosts: the sub-menu
// layout, an item per host.
func backupHostMenu() screen.SubMenu {
	var items []component.MenuItem
	for i, h := range backupHosts() {
		items = append(items, component.MenuItem{Key: fmt.Sprint(i + 1), Label: h.label, Description: h.about})
	}
	return screen.SubMenu{
		Title:   component.TitleBar{Title: "Backup hosts"},
		Heading: "Select a host",
		Menu:    component.Menu{Items: items, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:  "Enter your choice",
	}
}

// openBackup returns the action that opens what name names: the hosts'
// menu for backupPrefix alone, the Backup screen on one host for
// "backup/<host>", the restore's mock for restoreMock.
func (s *session) openBackup(name string) func() error {
	if name == restoreMock {
		return func() error { return s.restoreMock(context.Background()) }
	}
	if bh, ok := findBackupHost(strings.TrimPrefix(name, backupPrefix+"/")); ok && name != backupPrefix {
		return func() error { return s.backupScreen(context.Background(), bh, bh.host()) }
	}
	return func() error {
		vm, all := backupHostMenu(), backupHosts()
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, it := range vm.Menu.Items {
			bh := all[i]
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.backupScreen(context.Background(), bh, bh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// writeNote writes the note on what play runs, titled title, set apart as
// a scenario's is (runScenario).
func (s *session) writeNote(title, note string) error {
	c, gap := s.p.Ctx, strings.Repeat("\n", theme.SectionGap)
	text := component.Callout{Kind: component.MessageInfo, Title: title, Body: []string{note}}.Render(c) + "\n" + gap
	if s.p.In.Asked() {
		text = gap + strings.TrimSuffix(text, gap)
	}
	_, err := io.WriteString(s.p.Out, text)
	return err
}

// backupScreen runs the Backup screen on h, bh's host, until the user goes
// back from it: its note above it; hardn.yml read as hardn reads it
// (config.Read); each change's engine the gallery's, over the host as the
// engine reaches it, its backups hardn.yml's as the screen was opened, as
// cmd/hardn's are assembled once per visit (engineOptions), with the
// gallery's hold, its apply recorded for the end of play. A dry run when
// play plays the dry-run sequence.
func (s *session) backupScreen(ctx context.Context, bh backupHost, h *fakeHost) error {
	sys, title := h.system(), backupPrefix+"/"+bh.name
	cfg, err := config.Read(ctx, sys, hardnYML)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	if err := s.writeNote(title, bh.note); err != nil {
		return err
	}
	settings := cfg.BackupSettings()
	var rec *recorder
	return backupscreen.Run(ctx, backupscreen.Options{
		Prompter: s.p, System: sys, Config: cfg, DryRun: s.sequence == "dry-run",
		LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			r := &recorder{host: h, f: h.f, title: title}
			rec = r
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, r)
			s.mu.Unlock()
			r.begin()
			return engine.New(sys, engine.Options{
				Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
				Session:  applySession,
				Backups:  settings,
				Hold:     s.hold,
				PID:      applyPID,
				Progress: func(ev engine.Event) { r.event(ev); progress(ev) },
			})
		},
		Ran: func(run output.Run) {
			rec.end()
			rec.setPlan(run.Plan)
			rec.failedWith(run.Err)
		},
	})
}

// The restore's mock (P-19; gallery-backup.md decision 9): not hardn's.
const (
	// restoreNote is play's note for it.
	restoreNote = "Play mode, a mock: the restore of a backup run (P-19), a later unit's, on the real plan/apply engine " +
		"against a simulated host; its control is the gallery's. hardn's Backup Runs is read only. " +
		"Nothing on this machine changes: every command and file is the fake's, and its record is shown when play ends."
	// restoreHostNote is the note above it.
	restoreHostNote = hostFQDN + " is the runs host: three earlier changes backed up to /var/backups/hardn. " +
		"Each run leads to its files and a Restore, which this mock adds to hardn's run screen."
	// lockoutRestore is what the mock says of a run that holds SSH or
	// firewall files, where the restore is chosen: P-19's risk, not
	// planned here.
	lockoutRestore = "Restoring SSH or firewall files needs the typed confirmation and the revert timer. This mock does not plan it (P-19)."
)

// restoreMock plays the restore's mock on a fresh runs host: Backup Runs,
// as hardn draws it (screen.BackupRunsMenu), each run as hardn draws it
// (screen.BackupRun) with the mock's item 1, Restore; read again at each
// draw, a restore being a change whose own backups are a run.
func (s *session) restoreMock(ctx context.Context) error {
	h := runsHost()
	sys := h.system()
	cfg, err := config.Read(ctx, sys, hardnYML)
	if err != nil {
		return err
	}
	if err := s.writeNote(restoreMock, restoreHostNote); err != nil {
		return err
	}
	return s.p.Run(func() nav.Screen {
		runs, _ := backup.Runs(ctx, sys, cfg.BackupSettings().Path)
		views := make([]screen.BackupRun, len(runs))
		for i, m := range runs {
			views[i] = runView(m)
		}
		vm := screen.BackupRunsMenu(views)
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, it := range vm.Menu.Items {
			m, v := runs[i], views[i]
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.restoreRun(ctx, h, cfg, m, v) }})
		}
		return vm.Screen(s.p.Ctx, menu)
	})
}

// runView is run m as hardn's Backup Runs shows it (the mock's copy of
// pkg/screens/backup's, for its list): its id, when it was taken, from the
// id, and each path and what it was.
func runView(m backup.Manifest) screen.BackupRun {
	v := screen.BackupRun{ID: m.RunID}
	if t, err := time.Parse("20060102T150405Z", strings.SplitN(m.RunID, "-", 2)[0]); err == nil {
		v.Taken = t
	}
	for _, e := range m.Entries {
		f := screen.BackupFile{Path: e.Path, Absent: e.Kind == backup.KindAbsent, Link: e.SymlinkTarget, TakenAnyway: e.TakenAnyway}
		if e.File != nil {
			f.Size = e.File.Size
		}
		v.Files = append(v.Files, f)
	}
	return v
}

// lockoutRun reports whether run m backed up a file whose change can lock
// a user out: sshd's or ufw's.
func lockoutRun(m backup.Manifest) bool {
	return slices.ContainsFunc(m.Entries, func(e backup.Entry) bool {
		return strings.HasPrefix(e.Path, "/etc/ssh/") || strings.HasPrefix(e.Path, "/etc/ufw/")
	})
}

// restoreRun is one run's screen as hardn draws it, with the mock's item
// 1, Restore, refused for a run of SSH or firewall files (lockoutRun).
func (s *session) restoreRun(ctx context.Context, h *fakeHost, cfg *config.Config, m backup.Manifest, v screen.BackupRun) error {
	vm := v.SubMenu()
	vm.Heading = "Select an option"
	vm.Menu.Items = []component.MenuItem{{Key: "1", Label: "Restore", Description: "Put these files back"}}
	action := func() error { return s.restore(ctx, h, cfg, m) }
	if lockoutRun(m) {
		action = s.say(component.Message{Kind: component.MessageWarn, Text: lockoutRestore})
	}
	menu := nav.Menu{Exit: vm.Menu.Exit, Options: []nav.Option{{Item: vm.Menu.Items[0], Action: action}}}
	return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
}

// restore is the restore of run m (P-19), through the flow: the review;
// the plan, the engine's, of restoreControl, each file put back that
// differs from its copy, last taken first, as backup.Restore goes;
// "Restore this run?"; the apply, on the engine, which backs up and
// journals each file first, so that the restore is itself a change hardn
// rollback can undo. Then hardn.yml read again, which the restore may
// have put back, and what the flow does not draw (output.AfterFlow: what
// it kept, and where).
func (s *session) restore(ctx context.Context, h *fakeHost, cfg *config.Config, m backup.Manifest) error {
	dryRun := s.sequence == "dry-run"
	rec := &recorder{host: h, f: h.f, title: restoreMock + ", " + m.RunID}
	s.mu.Lock()
	s.engineRuns = append(s.engineRuns, rec)
	s.mu.Unlock()
	fe := &output.FlowEngine{DryRun: dryRun, LogPath: output.LegacyLogPath}
	plans := func() ([]engine.Control, *config.Config) {
		fe.Engine = engine.New(h.system(), engine.Options{
			Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
			Session:  applySession,
			Backups:  cfg.BackupSettings(),
			Hold:     s.hold,
			PID:      applyPID,
			Progress: func(ev engine.Event) { rec.event(ev); fe.Progress(ev) },
		})
		return []engine.Control{restoreControl{Manifest: m}}, cfg
	}
	taken := runView(m)
	f := flow.Flow{
		Review: func() flow.Review {
			return flow.Review{Title: "Backup Run", Items: []component.KeyValue{{Key: "Run", Value: m.RunID},
				{Key: "Taken", Value: taken.Taken.Format("2006-01-02 15:04") + " UTC"}}}
		},
		Engine:  recording{fe, rec, plans},
		Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: "Restore this run?"} },
		DryRun:  dryRun,
	}
	out, err := screen.WorkflowStep{Title: component.TitleBar{Title: "Restore Backup Run"}}.Run(ctx, s.p, f)
	if err != nil {
		return err
	}
	rec.ended(out)
	if out == flow.Applied && !dryRun {
		// The restore may have put hardn.yml back: read it again.
		if c, rerr := config.Read(ctx, h.f, hardnYML); rerr == nil {
			*cfg = *c
		}
	}
	if after := output.AfterFlow(s.p.Ctx.Env, fe.Run(out, err), galleryNow); after != "" {
		_, err = io.WriteString(s.p.Out, after)
	}
	return err
}

// restoreControl is the restore's control (P-19), the gallery's: it plans
// one change per entry of a run's manifest whose file differs from its
// copy, last taken first as backup.Restore puts them back, each with the
// diff of what is there against the copy, or for a file the run found
// absent, its removal. The engine backs each file up before its change
// and journals it; the change's apply is backup.Restore of that entry.
type restoreControl struct {
	Manifest backup.Manifest
}

func (restoreControl) ID() string                          { return "backup.restore" }
func (restoreControl) Title() string                       { return "Restore" }
func (restoreControl) Applies(detect.Facts) (bool, string) { return true, "" }

// Check is not used: a restore is an action, not a state.
func (restoreControl) Check(context.Context, host.System, detect.Facts) (engine.Status, error) {
	return engine.Status{State: engine.NotApplicable}, nil
}

// Plan reads each entry's copy and the file as it is now, and plans the
// change where they differ.
func (c restoreControl) Plan(ctx context.Context, sys host.System, _ engine.Input) ([]engine.Change, error) {
	var out []engine.Change
	for i := len(c.Manifest.Entries) - 1; i >= 0; i-- {
		e := c.Manifest.Entries[i]
		var cur, was *string
		switch now, _, err := sys.ReadFileNoFollow(ctx, e.Path); {
		case err == nil:
			s := string(now)
			cur = &s
		case !errors.Is(err, fs.ErrNotExist):
			return nil, err
		}
		if e.File != nil {
			data, _, err := sys.ReadFileNoFollow(ctx, path.Join(c.Manifest.Dir, e.File.Backup))
			if err != nil {
				return nil, fmt.Errorf("restore %s: %w", e.Path, err)
			}
			s := string(data)
			was = &s
		}
		if (cur == nil) == (was == nil) && (cur == nil || *cur == *was) {
			continue // as it was
		}
		entry, verb := e, "Restore "
		if was == nil {
			verb = "Remove "
		}
		out = append(out, engine.Change{
			ID: fmt.Sprintf("backup.restore/%d", e.Seq), Summary: verb + e.Path,
			Title: verb + e.Path, Target: e.Path, Diff: engine.FileDiff(e.Path, cur, was),
			Stage: engine.StageFiles, Backups: []string{e.Path}, Resources: []string{"restore:" + e.Path},
			Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
				m := c.Manifest
				m.Entries = []backup.Entry{entry}
				res, err := backup.Restore(ctx, sys, m)
				if err != nil {
					return engine.Ran{}, &steps.Failure{Text: "Restoring " + entry.Path + " failed", Err: err}
				}
				return engine.Ran{Detail: res[0].Action.String() + " " + entry.Path}, nil
			},
		})
	}
	return out, nil
}
