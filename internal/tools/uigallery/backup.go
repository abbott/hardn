package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"syscall"
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
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The Backup mocks (refactor plan slice 3, unit (a), next/s3-gallery;
// docs/engine/gallery-backup.md): a proposal of the Backup screen, main
// menu 5, for the maintainer's review, nothing of it wired into hardn.
// It is drawn on the design system from what real code reads of a
// simulated host (fakehost.go): the backup settings as config.Read loads
// hardn.yml (Config.BackupSettings), what pkg/host/backup's Check refuses
// of them, the runs under backupPath as backup.Load reads their
// manifests. Each change is the canonical flow on the real engine, as the
// DNS screen runs it (output.FlowEngine): its plan the save of the one
// setting it changes (steps.PlanSave, config.Edit in place), the engine's
// backups taken as the settings it saves say (gallery-backup.md decision
// 7 (a): cmd/hardn's engineOptions takes hardn.yml as loaded); hardn.yml
// adopted as saved and the results set apart with the glue the screens
// share (pkg/screens/screenkit: Adopt, Apart). The mock's
// own, labelled so in the page: the screen's words, the path field's
// check that a path is clean, and the restore's control (P-19), whose
// apply is backup.Restore inside an engine change. The lines saying what
// a change backed up (§3.16) are pkg/output's since next/s3-control.
// "backup" lists the mocks with no input; "play backup" is the menu of
// the hosts and "play backup/<host>" the screen on one.

// backupPrefix names the mocks: "backup <mock>" in the listing, "backup"
// and "backup/<host>" in play.
const backupPrefix = "backup"

// The screen's words (gallery-backup.md §5): Figma's where the
// recommended answers take them (P-31), origin/main's title and prompt,
// the rest proposed.
const (
	backupTitle   = "Backup Settings"
	backupSection = "Backup Configuration"
	backupPrompt  = "Enter selection"
	backupHeading = "Select an option"
	stateKey      = "Backup State"
	pathKey       = "Backup Path"
	// rollbackOnly is the muted line under the rows with backups off
	// (§3.16: rollback-required backups are taken anyway).
	rollbackOnly = "Rollback backups are still taken"
	// pathTitle is the path field's question (origin/main's, without its
	// colon).
	pathTitle = "Enter new backup path"
	// nothingWritten closes Verify's report.
	nothingWritten = "Nothing was written."
)

func init() {
	// The elsewhere host's backup path, and the path the sequences move
	// to, is hardn's own state, as /var/backups/hardn is: the record play
	// ends with leaves its writes out (stateDirs).
	stateDirs = append(stateDirs, elsewherePath)
}

// elsewherePath is the elsewhere host's backup path.
const elsewherePath = "/srv/backups/hardn"

// The menu's keys.
const (
	keyState   = "1"
	keyPath    = "2"
	keyVerify  = "3"
	keyRuns    = "4"
	keyRestore = "1"
)

// backupHost is one simulated host the Backup mocks run on.
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

// backupHosts are the hosts, in the menu's order.
func backupHosts() []backupHost {
	const (
		deb  = hostFQDN + " runs Debian 13. "
		each = " Each change is planned, reviewed, confirmed y/N and applied on the real engine; then the screen is read again."
	)
	return []backupHost{
		{name: "default", label: "Default", about: "Backups on, /var/backups/hardn", host: backupOn(true, backup.DefaultPath),
			note: deb + "hardn.yml keeps the defaults: backups on, in /var/backups/hardn, which no change has made yet." + each},
		{name: "off", label: "Backups off", about: "enableBackups: false", host: backupOn(false, backup.DefaultPath),
			note: deb + "hardn.yml says enableBackups: false. A change still backs up what its rollback needs (plan §3.16)." + each},
		{name: "elsewhere", label: "Another path", about: elsewherePath, host: elsewhereHost,
			note: deb + "hardn.yml says backupPath: /srv/backups/hardn, a directory root made 0700." + each},
		{name: "unsafe", label: "Unsafe path", about: "/tmp/hardn-backups", host: backupOn(true, "/tmp/hardn-backups"),
			note: deb + "hardn.yml says backupPath: /tmp/hardn-backups; /tmp is world-writable, so pkg/host/backup refuses it, and with it every change but a new path (decision 7)."},
		{name: "relative", label: "Relative path", about: "backupPath: backups", host: backupOn(true, "backups"),
			note: deb + "hardn.yml says backupPath: backups, as origin/main's Backup screen saves a relative path; pkg/host/backup refuses it, and with it every change but a new path (decision 7)."},
		{name: "readonly", label: "Read-only", about: "/var/backups read-only", host: readOnlyHost,
			note: deb + "/var/backups is on a read-only file system. Check cannot see that without writing, so a change fails at its backup, and nothing changes."},
		{name: "runs", label: "Earlier runs", about: "Three changes backed up", host: runsHost,
			note: deb + "Three earlier changes were backed up to /var/backups/hardn: an SSH port, a DNS change, then a save with backups off; hardn.yml and resolv.conf have changed since." + each},
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

// backupReading is one reading of a host's backups: the settings
// hardn.yml gives, what Check makes of them, whether the directory is
// there, and the runs in it.
type backupReading struct {
	settings backup.Settings
	// check is backup.Check's error for the settings, with no path: what
	// every change's plan refuses before anything else.
	check error
	// made: backupPath is a directory; when not, Check passed on the
	// directory above it, and the first change makes it.
	made bool
	// runs are the runs under backupPath whose manifests backup.Load
	// reads, newest first.
	runs []backup.Manifest
}

// readBackups reads the backups of sys as cfg sets them.
func readBackups(ctx context.Context, sys host.System, cfg *config.Config) backupReading {
	r := backupReading{settings: cfg.BackupSettings()}
	r.check = backup.Check(ctx, sys, r.settings, backup.NewRunID(clock()), nil)
	if r.check != nil {
		return r
	}
	dir := path.Clean(r.settings.Path)
	if fi, err := sys.Lstat(ctx, dir); err == nil && fi.IsDir() {
		r.made = true
		r.runs = readRuns(ctx, sys, dir)
	}
	return r
}

// readRuns returns the runs under dir whose manifests backup.Load reads,
// newest first: a run id starts with its time, so its name sorts them.
func readRuns(ctx context.Context, sys host.System, dir string) []backup.Manifest {
	entries, err := sys.ReadDir(ctx, dir)
	if err != nil {
		return nil
	}
	var runs []backup.Manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if m, err := backup.Load(ctx, sys, path.Join(dir, e.Name())); err == nil {
			runs = append(runs, m)
		}
	}
	slices.SortFunc(runs, func(a, b backup.Manifest) int { return strings.Compare(b.RunID, a.RunID) })
	return runs
}

// refusalWord is the path row's note for err, Check's refusal: a word or
// two, Verify saying the rest (verifyWords).
func refusalWord(err error) string {
	switch {
	case errors.Is(err, backup.ErrSettings):
		return "Not absolute"
	case errors.Is(err, backup.ErrUnsafeDir):
		return "Unsafe"
	case errors.Is(err, syscall.ENOTDIR):
		return "Not a directory"
	}
	return "Can't tell"
}

// unsafeDir is the directory Check names in an ErrUnsafeDir, "backup:
// <dir>: directory another user can change"; "" when err names none.
// Read from the error's text: pkg/host/backup has no typed error that
// carries it (gallery-backup.md §8).
func unsafeDir(err error) string {
	s, ok := strings.CutPrefix(err.Error(), "backup: ")
	if !ok {
		return ""
	}
	dir, ok := strings.CutSuffix(s, ": "+backup.ErrUnsafeDir.Error())
	if !ok || !path.IsAbs(dir) {
		return ""
	}
	return dir
}

// subMenu is the screen of r: the title bar; the section, its state row,
// with backups off the line under it saying rollback backups are still
// taken, and the path row, the path's note from Check; the items, item 1
// naming the way it turns, item 4 the runs (P-19); "Return"; no status
// rows (the frame's two are other screens': gallery-backup.md decision 1).
func (r backupReading) subMenu() screen.SubMenu {
	state := component.KeyValue{Key: stateKey, Value: "Enabled", State: component.ValueOK}
	turn := "Turn backups off"
	if !r.settings.Enabled {
		state.Value, state.State, turn = "Disabled", component.ValueWarn, "Turn backups on"
	}
	where := component.KeyValue{Key: pathKey, Value: r.settings.Path, State: component.ValueOK, Note: "Root only"}
	switch {
	case r.check != nil:
		where.State, where.Note = component.ValueBad, refusalWord(r.check)
		if where.Note == "Can't tell" {
			where.State = component.ValueUnknown
		}
	case !r.made:
		where.Note = "Not created yet"
	}
	rows := []component.KeyValue{state, where}
	if !r.settings.Enabled {
		rows = []component.KeyValue{state, {Key: rollbackOnly, State: component.ValueNone}, where}
	}
	runs := "None yet"
	switch n := len(r.runs); {
	case n == 1:
		runs = "1 run"
	case n > 1:
		runs = fmt.Sprintf("%d runs", n)
	}
	return screen.SubMenu{
		Title:    component.TitleBar{Title: backupTitle},
		Sections: []screen.Section{{Header: component.SectionHeader{Title: backupSection}, Rows: component.BulletList{Items: rows}}},
		Heading:  backupHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: keyState, Label: stateKey, Description: turn},
			{Key: keyPath, Label: pathKey, Description: "Change backup path"},
			{Key: keyVerify, Label: "Verify Directory", Description: "Check the path, writing nothing"},
			{Key: keyRuns, Label: "Backup Runs", Description: runs},
		}, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt: backupPrompt,
	}
}

// statusRowScreen is decision 1's alternative (b): the screen with one
// status row of its own, Backups, in place of the frame's two copied
// rows, saying the state: Enabled; Partial, rollback backups only, with
// backups off (§3.16); Not Configured, in Check's word, where the path is
// refused. The section keeps the path alone, so that nothing is said
// twice.
func (r backupReading) statusRowScreen() screen.SubMenu {
	vm := r.subMenu()
	row := component.StatusRow{Label: "Backups", State: component.StatusConfigured, Word: "Enabled"}
	switch {
	case r.check != nil:
		row.State, row.Word, row.Detail = component.StatusNotConfigured, "", strings.ToLower(refusalWord(r.check))
	case !r.settings.Enabled:
		row.State, row.Word, row.Detail = component.StatusPartial, "", "rollback backups only"
	}
	vm.Status = component.StatusList{Rows: []component.StatusRow{row}}
	rows := &vm.Sections[0].Rows.Items
	*rows = slices.DeleteFunc(*rows, func(kv component.KeyValue) bool { return kv.Key != pathKey })
	return vm
}

// figmaMock is the gallery's mock of the 05-BACKUP frame as Figma draws
// it (pkg/ui/screen's sub-menu/05-backup): its two status rows, copied
// from other frames, among them.
func figmaMock() (screen.SubMenu, error) {
	for _, m := range screen.Mocks() {
		if m.Name == "sub-menu/05-backup" {
			if vm, ok := m.Screen.(screen.SubMenu); ok {
				return vm, nil
			}
		}
	}
	return screen.SubMenu{}, errors.New("no mock sub-menu/05-backup")
}

// refusal is Check's refusal err of the backup path p in words: what is
// wrong, then what to do, as one warning. Verify says it, and item 1
// before it asks anything, on a host whose path Check refuses.
func refusal(err error, p string) component.Message {
	var text string
	switch {
	case errors.Is(err, backup.ErrSettings):
		text = fmt.Sprintf("%q is not an absolute path. Start it with /.", p)
	case errors.Is(err, backup.ErrUnsafeDir):
		dir := unsafeDir(err)
		if dir == "" {
			dir = p
		}
		text = "Others can change " + dir + ", so backups can't go in " + p + ". Choose a directory only root can change."
	case errors.Is(err, syscall.ENOTDIR):
		text = p + " is not a directory. Choose another path."
	default:
		text = "Can't check " + p + ": " + err.Error() + "."
	}
	return component.Message{Kind: component.MessageWarn, Text: text}
}

// verifyWords are Verify's report of r: what Check found and that
// nothing was written, as prose; when Check refuses, the refusal, then
// the prose.
func verifyWords(r backupReading) component.Messages {
	p := r.settings.Path
	switch {
	case r.check != nil:
		return component.Messages{refusal(r.check, p), {Text: nothingWritten}}
	case r.made:
		return component.Messages{{Text: "Only root can change " + p + ". " + nothingWritten}}
	}
	return component.Messages{{Text: "Only root can change " + path.Dir(path.Clean(p)) + ", where the first backup makes " + p + ". " + nothingWritten}}
}

// runTime is when run id was taken, to the minute, UTC, from the time it
// starts with (backup.NewRunID); id itself when it starts with none.
func runTime(id string) string {
	t, err := time.Parse("20060102T150405Z", strings.SplitN(id, "-", 2)[0])
	if err != nil {
		return id
	}
	return t.Format("2006-01-02 15:04")
}

// runAbout is a run's item description: its files, and whether any was
// taken with backups off.
func runAbout(m backup.Manifest) string {
	s := fmt.Sprintf("%d files", len(m.Entries))
	if len(m.Entries) == 1 {
		s = "1 file"
	}
	if slices.ContainsFunc(m.Entries, func(e backup.Entry) bool { return e.TakenAnyway }) {
		s += ", taken anyway"
	}
	return s
}

// runsScreen is the list of runs (P-19): one item per run, newest first.
func runsScreen(runs []backup.Manifest) screen.SubMenu {
	var items []component.MenuItem
	for i, m := range runs {
		items = append(items, component.MenuItem{Key: fmt.Sprint(i + 1), Label: runTime(m.RunID), Description: runAbout(m)})
	}
	return screen.SubMenu{
		Title:   component.TitleBar{Title: "Backup Runs"},
		Heading: "Select a run",
		Menu:    component.Menu{Items: items, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:  backupPrompt,
	}
}

// runScreen is one run (P-19): its id, each file backed up and what it
// was, and the restore.
func runScreen(m backup.Manifest) screen.SubMenu {
	var rows []component.KeyValue
	for _, e := range m.Entries {
		kv := component.KeyValue{Key: e.Path}
		switch {
		case e.Kind == backup.KindAbsent:
			kv.Value = "Not there before"
		case e.Kind == backup.KindSymlink:
			kv.Value = "Link to " + e.SymlinkTarget
		case e.File != nil:
			kv.Value = fmt.Sprintf("%d bytes", e.File.Size)
		}
		if e.TakenAnyway {
			kv.State, kv.Note = component.ValueWarn, "taken anyway"
		}
		rows = append(rows, kv)
	}
	return screen.SubMenu{
		Title:    component.TitleBar{Title: "Backup Run"},
		Sections: []screen.Section{{Header: component.SectionHeader{Title: "Run " + m.RunID}, Rows: component.BulletList{Items: rows}}},
		Heading:  backupHeading,
		Menu: component.Menu{Items: []component.MenuItem{{Key: keyRestore, Label: "Restore", Description: "Put these files back"}},
			Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt: backupPrompt,
	}
}

// backupSave is the control a change of the screen plans with: the save
// of the one hardn.yml setting it changes, setting, through
// steps.PlanSave (config.Edit in place, §3.19 as amended). No control of
// hardn has it: unit (b)'s.
func backupSave(setting string) engine.Control {
	return galleryControl{id: "backup.settings", title: "Backups", plan: func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		return steps.PlanSave(ctx, sys, "backup.settings/save", in.Config, []string{setting})
	}}
}

// pathCheck is the path field's check of the entry typed, with current
// the path in use: none; Check's refusals of it, as Open and the plan
// refuse them (backup.Check, real); not clean, which Open would clean
// unsaid (the mock's own); the path in use, a warning, as the DNS
// screen's "already configured".
func pathCheck(ctx context.Context, sys host.System, current string) func(string) error {
	return func(entry string) error {
		if entry == "" {
			return errors.New("Backup path cannot be empty")
		}
		err := backup.Check(ctx, sys, backup.Settings{Path: entry, Enabled: true}, backup.NewRunID(clock()), nil)
		switch {
		case errors.Is(err, backup.ErrSettings):
			return errors.New("Not an absolute path")
		case path.Clean(entry) != entry:
			return errors.New("Write it as " + path.Clean(entry))
		case entry == current:
			return nav.Warn(entry + " is the backup path already")
		case errors.Is(err, backup.ErrUnsafeDir):
			if dir := unsafeDir(err); dir != "" {
				return errors.New("Others can change " + dir)
			}
			return errors.New("Others can change it")
		case errors.Is(err, syscall.ENOTDIR):
			return errors.New("Not a directory")
		case err != nil:
			return errors.New("Can't check it: " + err.Error())
		}
		return nil
	}
}

// backupScreen is one run of the Backup screen on a simulated host.
type backupScreen struct {
	s     *session
	h     *fakeHost
	title string
	// cfg is hardn.yml as loaded, which adopts each save (screenkit.Adopt).
	cfg *config.Config
}

// openBackup returns the action that opens what name names: the hosts'
// menu for backupPrefix alone, the screen on one host for "backup/<host>".
func (s *session) openBackup(name string) func() error {
	if bh, ok := findBackupHost(strings.TrimPrefix(name, backupPrefix+"/")); ok && name != backupPrefix {
		return func() error { return s.runBackup(context.Background(), bh) }
	}
	return func() error {
		all := backupHosts()
		vm := screen.SubMenu{Title: component.TitleBar{Title: "Backup hosts"}, Heading: "Select a host", Prompt: "Enter your choice",
			Menu: component.Menu{Exit: component.MenuItem{Key: "0", Label: "Return"}}}
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, bh := range all {
			it := component.MenuItem{Key: fmt.Sprint(i + 1), Label: bh.label, Description: bh.about}
			vm.Menu.Items = append(vm.Menu.Items, it)
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.runBackup(context.Background(), bh) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// runBackup runs the Backup screen on bh's host, fresh (runBackupOn).
func (s *session) runBackup(ctx context.Context, bh backupHost) error {
	return s.runBackupOn(ctx, bh, bh.host())
}

// runBackupOn runs the Backup screen on h, bh's host, until the user goes
// back from it: its note above it, as the DNS screen's, then each round
// the screen read again.
func (s *session) runBackupOn(ctx context.Context, bh backupHost, h *fakeHost) error {
	cfg, err := config.Read(ctx, h.f, hardnYML)
	if err != nil {
		return err
	}
	b := &backupScreen{s: s, h: h, title: backupPrefix + "/" + bh.name, cfg: cfg}
	c, gap := s.p.Ctx, strings.Repeat("\n", theme.SectionGap)
	note := component.Callout{Kind: component.MessageInfo, Title: b.title, Body: []string{bh.note}}.Render(c) + "\n" + gap
	if s.p.In.Asked() {
		note = gap + strings.TrimSuffix(note, gap)
	}
	if err := b.write(note); err != nil {
		return err
	}
	return s.p.Run(func() nav.Screen { return b.draw(ctx) })
}

// draw reads the host and returns the screen, its items bound.
func (b *backupScreen) draw(ctx context.Context) nav.Screen {
	r := readBackups(ctx, b.h.system(), b.cfg)
	vm := r.subMenu()
	toggle := b.toggle(ctx, r)
	if r.check != nil {
		// Every plan is refused while Check refuses the path: say so
		// before asking anything, as the DNS screen does where every plan
		// is refused. A new path (2) is planned under itself, the way out
		// (decision 7 (a)).
		toggle = b.say(component.Messages{refusal(r.check, r.settings.Path)})
	}
	actions := map[string]func() error{
		keyState:  toggle,
		keyPath:   b.changePath(ctx, r),
		keyVerify: b.say(verifyWords(r)),
		keyRuns:   b.openRuns(ctx, r),
	}
	menu := nav.Menu{Exit: vm.Menu.Exit}
	for _, it := range vm.Menu.Items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: actions[it.Key]})
	}
	return vm.Screen(b.s.p.Ctx, menu)
}

// write writes s to play's output.
func (b *backupScreen) write(s string) error {
	_, err := io.WriteString(b.s.p.Out, s)
	return err
}

// say returns an action that writes msgs, set apart as an action's result
// is (screenkit.Apart), and goes back to the screen.
func (b *backupScreen) say(msgs component.Messages) func() error {
	return func() error { return b.write(screenkit.Apart(b.s.p.Ctx, msgs)) }
}

// review is a change's review: the section, with the rows the change
// makes, and with backups turned off the line that rollback backups are
// still taken.
func review(rows ...component.KeyValue) func() flow.Review {
	return func() flow.Review { return flow.Review{Title: backupSection, Items: rows} }
}

// toggle is item 1: backups turned the other way, through the flow.
func (b *backupScreen) toggle(ctx context.Context, r backupReading) func() error {
	on := !r.settings.Enabled
	title, question := "Turn Backups Off", "Turn backups off?"
	rows := []component.KeyValue{{Key: stateKey, Value: "Disabled", State: component.ValueWarn}, {Key: rollbackOnly, State: component.ValueNone}}
	if on {
		title, question = "Turn Backups On", "Turn backups on?"
		rows = []component.KeyValue{{Key: stateKey, Value: "Enabled", State: component.ValueOK}}
	}
	return func() error {
		return b.change(ctx, title, nil, review(rows...), "enableBackups", func(c *config.Config) { c.EnableBackups = on },
			func() string { return question })
	}
}

// changePath is item 2: the path asked and checked (pathCheck), then
// saved through the flow.
func (b *backupScreen) changePath(ctx context.Context, r backupReading) func() error {
	return func() error {
		var entry string
		field := &form.Text{Title: pathTitle, Validate: pathCheck(ctx, b.h.system(), r.settings.Path), Value: &entry}
		rows := func() flow.Review {
			kv := component.KeyValue{Key: pathKey, Value: entry, State: component.ValueOK, Note: "Root only"}
			if fi, err := b.h.system().Lstat(ctx, entry); err != nil || !fi.IsDir() {
				kv.Note = "Not created yet"
			}
			return flow.Review{Title: backupSection, Items: []component.KeyValue{kv}}
		}
		return b.change(ctx, "Change Backup Path", []form.Field{field}, rows, "backupPath", func(c *config.Config) { c.BackupPath = entry },
			func() string { return "Use " + entry + "?" })
	}
}

// newEngine is the engine of one change of the screen, over the host as
// the engine reaches it, as the DNS screen's is: its backups taken as
// settings say, the gallery's hold, its events recorded by rec and drawn
// by fe.
func (b *backupScreen) newEngine(settings backup.Settings, rec *recorder, fe *output.FlowEngine) *engine.Engine {
	return engine.New(b.h.system(), engine.Options{
		Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
		Session:  applySession,
		Backups:  settings,
		Hold:     b.s.hold,
		PID:      applyPID,
		Progress: func(ev engine.Event) { rec.event(ev); fe.Progress(ev) },
	})
}

// change runs one change of the screen under its title bar: the form;
// the review; the plan, the engine's, of the save of setting from a copy
// of hardn.yml that edit changes, the engine taking its backups as the
// settings saved say (decision 7 (a)), so that a new path takes the
// change's own backup, and turning backups off takes it with them off;
// question, y/N; the apply. Then what the flow does not draw
// (output.AfterFlow, which says what the run backed up since
// next/s3-control), and hardn.yml adopted as saved.
func (b *backupScreen) change(ctx context.Context, title string, fields []form.Field, rv func() flow.Review, setting string,
	edit func(*config.Config), question func() string) error {
	s, sys := b.s, b.h.system()
	dryRun := s.sequence == "dry-run"
	rec := &recorder{host: b.h, f: b.h.f, title: b.title}
	fe := &output.FlowEngine{DryRun: dryRun, LogPath: output.LegacyLogPath}
	s.mu.Lock()
	s.engineRuns = append(s.engineRuns, rec)
	s.mu.Unlock()
	edited := *b.cfg
	plans := func() ([]engine.Control, *config.Config) {
		edited = *b.cfg
		edit(&edited)
		fe.Engine = b.newEngine(edited.BackupSettings(), rec, fe)
		return []engine.Control{backupSave(setting)}, &edited
	}
	f := flow.Flow{
		Form:    form.Form{Fields: fields},
		Review:  rv,
		Engine:  recording{fe, rec, plans},
		Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: question()} },
		DryRun:  dryRun,
	}
	out, err := screen.WorkflowStep{Title: component.TitleBar{Title: title}}.Run(ctx, s.p, f)
	if err != nil {
		return err
	}
	rec.ended(out)
	run := fe.Run(out, err)
	after := output.AfterFlow(s.p.Ctx.Env, run, galleryNow)
	if out == flow.Applied && !dryRun {
		screenkit.Adopt(ctx, sys, b.cfg, &edited)
	}
	if after != "" {
		return b.write(after)
	}
	return nil
}

// openRuns is item 4 (P-19): the runs under backupPath, each leading to
// its files and the restore; with none, a line saying so.
func (b *backupScreen) openRuns(ctx context.Context, r backupReading) func() error {
	if len(r.runs) == 0 {
		return b.say(component.Messages{{Text: "No change has been backed up yet."}})
	}
	return func() error {
		return b.s.p.Run(func() nav.Screen {
			// Read again at each draw: a restore is a change, its own
			// backups a run of their own.
			runs := readBackups(ctx, b.h.system(), b.cfg).runs
			vm := runsScreen(runs)
			menu := nav.Menu{Exit: vm.Menu.Exit}
			for i, it := range vm.Menu.Items {
				m := runs[i]
				menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return b.openRun(ctx, m) }})
			}
			return vm.Screen(b.s.p.Ctx, menu)
		})
	}
}

// lockoutRestore is what the mock says of a run that holds SSH or
// firewall files, where the restore is chosen: P-19's risk, not planned
// here.
const lockoutRestore = "Restoring SSH or firewall files needs the typed confirmation and the revert timer. This mock does not plan it (P-19)."

// lockoutRun reports whether run m backed up a file whose change can lock
// a user out: sshd's or ufw's.
func lockoutRun(m backup.Manifest) bool {
	return slices.ContainsFunc(m.Entries, func(e backup.Entry) bool {
		return strings.HasPrefix(e.Path, "/etc/ssh/") || strings.HasPrefix(e.Path, "/etc/ufw/")
	})
}

// openRun is one run's screen, its item 1 the restore, refused for a run
// of SSH or firewall files (lockoutRun).
func (b *backupScreen) openRun(ctx context.Context, m backup.Manifest) error {
	vm := runScreen(m)
	action := func() error { return b.restore(ctx, m) }
	if lockoutRun(m) {
		action = b.say(component.Messages{{Kind: component.MessageWarn, Text: lockoutRestore}})
	}
	menu := nav.Menu{Exit: vm.Menu.Exit, Options: []nav.Option{{Item: vm.Menu.Items[0], Action: action}}}
	return b.s.p.Run(func() nav.Screen { return vm.Screen(b.s.p.Ctx, menu) })
}

// restore is the restore of run m (P-19), through the flow: the review;
// the plan, the engine's, of restoreControl, each file put back that
// differs from its copy, last taken first, as backup.Restore goes;
// "Restore this run?"; the apply, on the engine, which backs up and
// journals each file first, so that the restore is itself a change
// hardn rollback can undo. Then what the flow does not draw
// (output.AfterFlow: what it kept, and where), and hardn.yml read again,
// which the restore may have put back.
func (b *backupScreen) restore(ctx context.Context, m backup.Manifest) error {
	dryRun := b.s.sequence == "dry-run"
	rec := &recorder{host: b.h, f: b.h.f, title: b.title + ", restore " + m.RunID}
	b.s.mu.Lock()
	b.s.engineRuns = append(b.s.engineRuns, rec)
	b.s.mu.Unlock()
	fe := &output.FlowEngine{DryRun: dryRun, LogPath: output.LegacyLogPath}
	settings := b.cfg.BackupSettings()
	plans := func() ([]engine.Control, *config.Config) {
		fe.Engine = b.newEngine(settings, rec, fe)
		return []engine.Control{restoreControl{Manifest: m}}, b.cfg
	}
	f := flow.Flow{
		Review: func() flow.Review {
			return flow.Review{Title: "Backup Run", Items: []component.KeyValue{{Key: "Run", Value: m.RunID}, {Key: "Taken", Value: runTime(m.RunID) + " UTC"}}}
		},
		Engine:  recording{fe, rec, plans},
		Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: "Restore this run?"} },
		DryRun:  dryRun,
	}
	out, err := screen.WorkflowStep{Title: component.TitleBar{Title: "Restore Backup Run"}}.Run(ctx, b.s.p, f)
	if err != nil {
		return err
	}
	rec.ended(out)
	run := fe.Run(out, err)
	after := output.AfterFlow(b.s.p.Ctx.Env, run, galleryNow)
	if out == flow.Applied && !dryRun {
		if cfg, err := config.Read(ctx, b.h.f, hardnYML); err == nil {
			b.cfg = cfg
		}
	}
	if after != "" {
		return b.write(after)
	}
	return nil
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

// backupMock is one mock the listing shows: its name, after backupPrefix,
// its note, and its drawing.
type backupMock struct {
	name, note string
	draw       func(ctx context.Context, c component.Ctx) (string, error)
}

// onHost returns a drawing of the host named name's reading.
func onHost(name string, view func(backupReading, *fakeHost) (string, error)) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, _ component.Ctx) (string, error) {
		bh, ok := findBackupHost(name)
		if !ok {
			return "", fmt.Errorf("no backup host %q", name)
		}
		h := bh.host()
		cfg, err := config.Read(ctx, h.f, hardnYML)
		if err != nil {
			return "", err
		}
		return view(readBackups(ctx, h.system(), cfg), h)
	}
}

// backupMocks are the listing's mocks, in order: the screen on each host,
// decision 1's alternative, Verify's reports, and a run (P-19).
func backupMocks() []backupMock {
	var mocks []backupMock
	for _, bh := range backupHosts() {
		mocks = append(mocks, backupMock{name: "screen-" + bh.name, note: "Mock: the Backup screen. " + bh.note,
			draw: func(ctx context.Context, c component.Ctx) (string, error) {
				return onHost(bh.name, func(r backupReading, _ *fakeHost) (string, error) { return r.subMenu().Render(c), nil })(ctx, c)
			}})
	}
	return append(mocks,
		backupMock{name: "figma", note: "05-BACKUP as Figma draws it (sub-menu/05-backup), decision 1's alternative (c): \"Backups Enabled UFW installed, deny policy\", a Firewall detail, and \"SSH Port 2230/tcp non-default\", 02-SSH_LOGIN's row whole, copied from other frames. screen-default is (a), none.",
			draw: func(_ context.Context, c component.Ctx) (string, error) {
				vm, err := figmaMock()
				if err != nil {
					return "", err
				}
				return vm.Render(c), nil
			}},
		backupMock{name: "status-row", note: "Mock (decision 1, alternative (b)): one status row of the screen's own, the state, in place of the frame's two copied rows, the section keeping the path; on the default host.",
			draw: func(ctx context.Context, c component.Ctx) (string, error) {
				return onHost("default", func(r backupReading, _ *fakeHost) (string, error) { return r.statusRowScreen().Render(c), nil })(ctx, c)
			}},
		backupMock{name: "verify", note: "Mock: Verify Directory (3) on the default, elsewhere, unsafe and relative hosts, each report under the screen's prompt; nothing written.",
			draw: func(ctx context.Context, c component.Ctx) (string, error) {
				var out []string
				for _, name := range []string{"default", "elsewhere", "unsafe", "relative"} {
					s, err := onHost(name, func(r backupReading, _ *fakeHost) (string, error) { return verifyWords(r).Render(c), nil })(ctx, c)
					if err != nil {
						return "", err
					}
					out = append(out, header(c, "verify on "+name), s)
				}
				return apart(out...), nil
			}},
		backupMock{name: "runs", note: "Mock (P-19): Backup Runs (4) on the runs host, newest first.",
			draw: func(ctx context.Context, c component.Ctx) (string, error) {
				return onHost("runs", func(r backupReading, _ *fakeHost) (string, error) { return runsScreen(r.runs).Render(c), nil })(ctx, c)
			}},
		backupMock{name: "run", note: "Mock (P-19): one run, the newest on the runs host, taken with backups off.",
			draw: func(ctx context.Context, c component.Ctx) (string, error) {
				return onHost("runs", func(r backupReading, _ *fakeHost) (string, error) { return runScreen(r.runs[0]).Render(c), nil })(ctx, c)
			}},
		backupMock{name: "run-dir", note: "That run's directory, <backupPath>/<run-id>/, as pkg/host/backup lays one out: each copy, then manifest.json. The gallery seeds it, the manifest a backup.Manifest marshalled as Run writes one; backup.Load reads it.",
			draw: func(ctx context.Context, c component.Ctx) (string, error) {
				return onHost("runs", func(r backupReading, h *fakeHost) (string, error) { return runDir(ctx, c, h, r.runs[0]) })(ctx, c)
			}},
	)
}

// runDir draws run m's directory on h: each file, its mode and size,
// then manifest.json as written.
func runDir(ctx context.Context, c component.Ctx, h *fakeHost, m backup.Manifest) (string, error) {
	sys := h.system()
	entries, err := sys.ReadDir(ctx, m.Dir)
	if err != nil {
		return "", err
	}
	var rows []component.KeyValue
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			return "", err
		}
		rows = append(rows, component.KeyValue{Key: e.Name(), Value: fmt.Sprintf("%04o, %d bytes", fi.Mode().Perm(), fi.Size())})
	}
	data, _, err := sys.ReadFileNoFollow(ctx, path.Join(m.Dir, backup.ManifestName))
	if err != nil {
		return "", err
	}
	files := screen.Section{Header: component.SectionHeader{Title: m.Dir}, Rows: component.BulletList{Items: rows}}.Render(c)
	return apart(files, verbatim(c, strings.TrimSuffix(string(data), "\n"))), nil
}

// verbatim renders text line by line as it is, muted, from theme.Indent,
// each line cut to the content width (cut).
func verbatim(c component.Ctx, text string) string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		out = append(out, draw(pad(theme.Indent), piece{cut(c, l, contentWidth(c.Env)-theme.Indent), c.Theme.Text.Muted.Render}))
	}
	return strings.Join(out, "\n")
}

// backupUsage is the listing's usage line.
const backupUsage = "usage: go run ./internal/tools/uigallery backup [flags] [mock | all]..."

// backupList is the listing: each mock args name (none, or "all", is
// every mock) under the gallery's header, "backup <mock>", its note, then
// its drawing, a write each; -list prints their names. It sizes itself as
// play does (look.sizeTo).
func backupList(args []string, size sizer, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uigallery backup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, backupUsage)
		fs.PrintDefaults()
	}
	list := fs.Bool("list", false, "print the names of the mocks it would list, and exit")
	look := lookFlags(fs, true)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	mocks, err := selectBackupMocks(fs.Args())
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", backupPrefix, err)
		return exitUsage
	}
	if *list {
		for _, m := range mocks {
			fmt.Fprintln(stdout, m.name)
		}
		return exitOK
	}
	look.sizeTo(fs, size)
	c, err := look.cell()
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", backupPrefix, err)
		return exitUsage
	}
	g, ok := look.gallery(stderr)
	if !ok {
		return exitError
	}
	rc, err := g.ctx(c.env)
	if err == nil {
		err = listBackup(context.Background(), rc, c.env.Writer(stdout), mocks)
	}
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", backupPrefix, err)
		return exitError
	}
	return exitOK
}

// listBackup writes each of mocks to w, rendered in rc, a write each: the
// gallery's header, the mock's note, then its drawing.
func listBackup(ctx context.Context, rc component.Ctx, w io.Writer, mocks []backupMock) error {
	for _, m := range mocks {
		out, err := m.draw(ctx, rc)
		if err == nil {
			_, err = io.WriteString(w, apart(header(rc, backupPrefix+" "+m.name),
				component.Callout{Kind: component.MessageInfo, Body: []string{m.note}}.Render(rc), out)+sectionBreak)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
	}
	return nil
}

// selectBackupMocks returns the mocks args name, in order: every mock for
// none or "all". An argument that starts with "-" is a flag the flag
// package left unparsed, after a name.
func selectBackupMocks(args []string) ([]backupMock, error) {
	all := backupMocks()
	if len(args) == 0 || slices.Contains(args, "all") {
		return all, nil
	}
	for _, a := range args {
		switch found := slices.ContainsFunc(all, func(m backupMock) bool { return m.name == a }); {
		case !found && strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("flag %s comes after a mock's name; flags go before them", a)
		case !found:
			return nil, fmt.Errorf("no mock %q; -list shows them", a)
		}
	}
	return slices.DeleteFunc(all, func(m backupMock) bool { return !slices.Contains(args, m.name) }), nil
}

// backupStart returns the hosts' menu, or the host, that args name
// ("backup", "backup/<host>"), and whether they name one; an error for a
// host that is not one.
func backupStart(args []string) (string, bool, error) {
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

// backupNote is play's note for the Backup mocks.
const backupNote = "Play mode, mocks: the Backup screen proposed for slice 3, drawn from what real code reads of a simulated host, " +
	"each change on the real plan/apply engine. Nothing on this machine changes: every command and file is the fake's, " +
	"and its record is shown when play ends."
