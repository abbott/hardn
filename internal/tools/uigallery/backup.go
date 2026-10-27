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
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
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
// backups taken as engine.Options.Backups says, from hardn.yml as loaded,
// as cmd/hardn's engineOptions takes them. The mock's own, labelled so in
// the page: the screen's words, the path field's check that a path is
// clean, the line saying a backup was taken anyway (§3.16), and the
// restore (P-19), whose apply is backup.Restore outside the engine.
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
	// takenAnyway is said after a change applied with backups off whose
	// rollback backup was taken (§3.16: "and says so").
	takenAnyway = "Backed up anyway: the rollback needs it."
	// nothingWritten closes Verify's report.
	nothingWritten = "Nothing was written."
	// byHand is how to go on where Check refuses the path in use: the
	// screen's path change is refused too (gallery-backup.md decision 7).
	byHand = "Set backupPath in " + hardnYML + " by hand."
)

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
		{name: "elsewhere", label: "Another path", about: "/srv/backups/hardn", host: elsewhereHost,
			note: deb + "hardn.yml says backupPath: /srv/backups/hardn, a directory root made 0700." + each},
		{name: "unsafe", label: "Unsafe path", about: "/tmp/hardn-backups", host: backupOn(true, "/tmp/hardn-backups"),
			note: deb + "hardn.yml says backupPath: /tmp/hardn-backups; /tmp is world-writable, so pkg/host/backup refuses it, and every change with it."},
		{name: "relative", label: "Relative path", about: "backupPath: backups", host: backupOn(true, "backups"),
			note: deb + "hardn.yml says backupPath: backups, as origin/main's Backup screen saves a relative path; pkg/host/backup refuses it, and every change with it."},
		{name: "readonly", label: "Read-only", about: "/var/backups read-only", host: readOnlyHost,
			note: deb + "/var/backups is on a read-only file system. Check cannot see that without writing, so a change fails at its backup, and nothing changes."},
		{name: "runs", label: "Earlier runs", about: "Two changes backed up", host: runsHost,
			note: deb + "Two earlier changes were backed up to /var/backups/hardn, the second with backups off; hardn.yml and resolv.conf have changed since." + each},
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
	h := backupOn(true, "/srv/backups/hardn")()
	h.f.AddDir("/srv/backups/hardn", 0o700, 0, 0)
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
// before the changes since.
const (
	runOldYML = "# hardn's configuration (" + hostFQDN + ")\nusername: alice\nnameservers:\n  - \"192.0.2.54\"\n"
	runOldDNS = "# Generated by dhcpcd from eth0.dhcp\nsearch example.net\nnameserver 192.0.2.1\n"
	runNewDNS = "# Generated by dhcpcd from eth0.dhcp\nsearch example.net\nnameserver 192.0.2.54\n"
)

// runFile is one file an earlier run backed up: its path, and its
// content and mode as they were.
type runFile struct {
	path, data string
	mode       backup.Perm
}

// earlierRun is one run runsHost holds: its id and when, whether
// backups were on, and each file backed up.
type earlierRun struct {
	id      string
	at      time.Time
	enabled bool
	files   []runFile
}

// earlierRuns are runsHost's runs: a DNS change with backups on, then a
// change of hardn.yml with backups off, whose rollback backup was taken
// anyway.
func earlierRuns() []earlierRun {
	return []earlierRun{
		{id: "20261018T091500Z-1a2b3c4d", at: time.Date(2026, 10, 18, 9, 15, 0, 0, time.UTC), enabled: true,
			files: []runFile{{dns.ResolvConf, runOldDNS, 0o644}, {hardnYML, runOldYML, 0o600}}},
		{id: "20261019T143000Z-5e6f7a8b", at: time.Date(2026, 10, 19, 14, 30, 0, 0, time.UTC),
			files: []runFile{{hardnYML, runOldYML, 0o600}}},
	}
}

// runsHost is Debian 13 with backups on, whose /var/backups/hardn holds
// earlierRuns, each as pkg/host/backup writes a run (runFiles), its
// copies 0600 and its directory 0700, root's; resolv.conf lists the
// nameserver the first run's change set.
func runsHost() *fakeHost {
	h := backupOn(true, backup.DefaultPath)()
	h.f.AddFile(dns.ResolvConf, []byte(runNewDNS), 0o644, 0, 0)
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
		sum := sha256.Sum256([]byte(f.data))
		e := backup.Entry{Seq: i + 1, Path: f.path, Kind: backup.KindFile, RollbackRequired: true, TakenAnyway: !r.enabled,
			File: &backup.Content{Backup: fmt.Sprintf("%04d-%s", i+1, path.Base(f.path)), Mode: f.mode, Size: int64(len(f.data)), SHA256: hex.EncodeToString(sum[:])}}
		m.Entries = append(m.Entries, e)
		files[e.File.Backup] = []byte(f.data)
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
// carries it (gallery-backup.md §7).
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

// subMenu is the screen of r: the title bar; the section, its state and
// path rows, the path's note from Check, and with backups off the line
// saying rollback backups are still taken; the items, item 1 naming the
// way it turns, item 4 the runs (P-19); "Return"; no status rows (the
// frame's two are another screen's: gallery-backup.md decision 1).
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
		rows = append(rows, component.KeyValue{Key: rollbackOnly, State: component.ValueNone})
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
// rows.
func (r backupReading) statusRowScreen() screen.SubMenu {
	vm := r.subMenu()
	row := component.StatusRow{Label: "Backups", State: component.StatusConfigured, Word: "Enabled", Detail: r.settings.Path}
	if !r.settings.Enabled {
		row.State, row.Word, row.Detail = component.StatusPartial, "", "rollback backups only"
	}
	vm.Status = component.StatusList{Rows: []component.StatusRow{row}}
	return vm
}

// verifyWords are Verify's report of r: what Check found and that
// nothing was written, as prose; when Check refuses, a warning saying
// why, then the prose.
func verifyWords(r backupReading) component.Messages {
	p := r.settings.Path
	var says string
	kind := component.MessageInfo
	switch {
	case errors.Is(r.check, backup.ErrSettings):
		kind, says = component.MessageWarn, fmt.Sprintf("%q is not an absolute path, so hardn refuses every change. %s", p, byHand)
	case errors.Is(r.check, backup.ErrUnsafeDir):
		dir := unsafeDir(r.check)
		if dir == "" {
			dir = p
		}
		kind, says = component.MessageWarn, fmt.Sprintf("Other users can write to %s, so hardn refuses every change. %s", dir, byHand)
	case errors.Is(r.check, syscall.ENOTDIR):
		kind, says = component.MessageWarn, fmt.Sprintf("%s is not a directory, so hardn refuses every change. %s", p, byHand)
	case r.check != nil:
		kind, says = component.MessageWarn, "Can't check "+p+": "+r.check.Error()+"."
	case r.made:
		says = "Only root can change " + p + "."
	default:
		says = "Only root can change " + path.Dir(path.Clean(p)) + ", where the first backup makes " + p + "."
	}
	if kind == component.MessageInfo {
		return component.Messages{{Text: says + " " + nothingWritten}}
	}
	return component.Messages{{Kind: kind, Text: says}, {Text: nothingWritten}}
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
				return errors.New("Other users can write to " + dir)
			}
			return errors.New("Other users can write to it")
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
	// cfg is hardn.yml as loaded, which adopts each save (adopt).
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

// runBackup runs the Backup screen on bh's host, fresh, until the user
// goes back from it: its note above it, as the DNS screen's, then each
// round the screen read again.
func (s *session) runBackup(ctx context.Context, bh backupHost) error {
	h := bh.host()
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
	actions := map[string]func() error{
		keyState:  b.toggle(ctx, r),
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
// is (nav.Prompter.Run), and goes back to the screen.
func (b *backupScreen) say(msgs component.Messages) func() error {
	return func() error {
		return b.write(strings.Repeat("\n", theme.SectionGap) + msgs.Render(b.s.p.Ctx) + "\n")
	}
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

// change runs one change of the screen under its title bar: the form;
// the review; the plan, the engine's, of the save of setting from a copy
// of hardn.yml that edit changes, the engine taking its backups as
// hardn.yml as loaded says (cmd/hardn's engineOptions); question, y/N;
// the apply. Then what the flow does not draw (output.AfterFlow), the
// line saying a backup was taken anyway, and hardn.yml adopted as saved.
func (b *backupScreen) change(ctx context.Context, title string, fields []form.Field, rv func() flow.Review, setting string,
	edit func(*config.Config), question func() string) error {
	s, sys := b.s, b.h.system()
	dryRun := s.sequence == "dry-run"
	rec := &recorder{host: b.h, f: b.h.f, title: b.title}
	fe := &output.FlowEngine{DryRun: dryRun, LogPath: output.LegacyLogPath}
	settings := b.cfg.BackupSettings()
	fe.Engine = engine.New(sys, engine.Options{
		Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
		Session:  applySession,
		Backups:  settings,
		Hold:     s.hold,
		PID:      applyPID,
		Progress: func(ev engine.Event) { rec.event(ev); fe.Progress(ev) },
	})
	s.mu.Lock()
	s.engineRuns = append(s.engineRuns, rec)
	s.mu.Unlock()
	edited := *b.cfg
	plans := func() ([]engine.Control, *config.Config) {
		edited = *b.cfg
		edit(&edited)
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
		if anyway(ctx, sys, settings, run.Plan) {
			after += strings.Repeat("\n", theme.SectionGap) + component.Message{Text: takenAnyway}.Render(s.p.Ctx) + "\n"
		}
		b.adopt(ctx, &edited)
	}
	if after != "" {
		return b.write(after)
	}
	return nil
}

// anyway reports whether the run of plan p, under settings, took a
// backup with backups off: its manifest (backup.Load) says so.
func anyway(ctx context.Context, sys host.System, settings backup.Settings, p *engine.Plan) bool {
	if p == nil || settings.Enabled {
		return false
	}
	m, err := backup.Load(ctx, sys, path.Join(path.Clean(settings.Path), p.ID))
	return err == nil && slices.ContainsFunc(m.Entries, func(e backup.Entry) bool { return e.TakenAnyway })
}

// adopt makes the screen's configuration edited once hardn.yml holds the
// bytes edited saved, as the DNS screen adopts a save
// (pkg/screens/dns adopt).
func (b *backupScreen) adopt(ctx context.Context, edited *config.Config) {
	data, _, err := b.h.system().ReadFileNoFollow(ctx, hardnYML)
	if err != nil || b.cfg.LoadedFrom(data) || !edited.LoadedFrom(data) {
		return
	}
	*b.cfg = *edited
}

// openRuns is item 4 (P-19): the runs under backupPath, each leading to
// its files and the restore; with none, a line saying so.
func (b *backupScreen) openRuns(ctx context.Context, r backupReading) func() error {
	if len(r.runs) == 0 {
		return b.say(component.Messages{{Text: "No change has been backed up yet."}})
	}
	return func() error {
		vm := runsScreen(r.runs)
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, it := range vm.Menu.Items {
			m := r.runs[i]
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return b.openRun(ctx, m) }})
		}
		return b.s.p.Run(func() nav.Screen { return vm.Screen(b.s.p.Ctx, menu) })
	}
}

// openRun is one run's screen, its item 1 the restore.
func (b *backupScreen) openRun(ctx context.Context, m backup.Manifest) error {
	vm := runScreen(m)
	menu := nav.Menu{Exit: vm.Menu.Exit, Options: []nav.Option{{Item: vm.Menu.Items[0], Action: func() error { return b.restore(ctx, m) }}}}
	return b.s.p.Run(func() nav.Screen { return vm.Screen(b.s.p.Ctx, menu) })
}

// restore is the restore of run m (P-19), through the flow: the review;
// the plan, each file put back that differs from its copy, last taken
// first, as backup.Restore goes; "Restore this run?"; the apply,
// backup.Restore on the host, outside the engine: no journal, lock or
// rollback (gallery-backup.md §7). Then hardn.yml read again, which the
// restore may have put back.
func (b *backupScreen) restore(ctx context.Context, m backup.Manifest) error {
	sys := b.h.system()
	rec := &recorder{host: b.h, f: b.h.f, title: b.title + ", restore " + m.RunID}
	b.s.mu.Lock()
	b.s.engineRuns = append(b.s.engineRuns, rec)
	b.s.mu.Unlock()
	dryRun := b.s.sequence == "dry-run"
	e := &restoring{sys: sys, m: m, rec: rec}
	f := flow.Flow{
		Review: func() flow.Review {
			return flow.Review{Title: "Backup Run", Items: []component.KeyValue{{Key: "Run", Value: m.RunID}, {Key: "Taken", Value: runTime(m.RunID) + " UTC"}}}
		},
		Engine:  e,
		Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: "Restore this run?"} },
		DryRun:  dryRun,
	}
	out, err := screen.WorkflowStep{Title: component.TitleBar{Title: "Restore Backup Run"}}.Run(ctx, b.s.p, f)
	if err != nil {
		return err
	}
	rec.ended(out)
	if cfg, err := config.Read(ctx, b.h.f, hardnYML); err == nil {
		b.cfg = cfg
	}
	return nil
}

// restoring is the restore's flow.Engine (P-19): its plan the files of m
// that differ from their copies, its apply backup.Restore.
type restoring struct {
	sys     host.System
	m       backup.Manifest
	rec     *recorder
	changed []backup.Entry // the entries the plan puts back, in its order
}

// Plan returns a change per entry whose file differs from its copy, last
// taken first: the copy's bytes against the file's, or for a file the run
// found absent, its removal.
func (r *restoring) Plan(ctx context.Context) (flow.Plan, error) {
	var p flow.Plan
	r.changed = nil
	for i := len(r.m.Entries) - 1; i >= 0; i-- {
		e := r.m.Entries[i]
		now, _, err := r.sys.ReadFileNoFollow(ctx, e.Path)
		var cur *string
		switch {
		case err == nil:
			s := string(now)
			cur = &s
		case !errors.Is(err, fs.ErrNotExist):
			return flow.Plan{}, err
		}
		var was *string
		if e.File != nil {
			data, _, err := r.sys.ReadFileNoFollow(ctx, path.Join(r.m.Dir, e.File.Backup))
			if err != nil {
				return flow.Plan{}, err
			}
			s := string(data)
			was = &s
		}
		if (cur == nil) == (was == nil) && (cur == nil || *cur == *was) {
			continue
		}
		title := "Restore " + e.Path
		if was == nil {
			title = "Remove " + e.Path
		}
		p.View.Changes = append(p.View.Changes, component.PlanChange{Title: title, Target: e.Path, Diff: engine.FileDiff(e.Path, cur, was)})
		r.changed = append(r.changed, e)
	}
	return p, nil
}

// Apply runs backup.Restore over the entries planned, then reports each
// step by what Restore did to its path.
func (r *restoring) Apply(ctx context.Context, report flow.Report) error {
	m := r.m
	m.Entries = nil
	for i := len(r.changed) - 1; i >= 0; i-- {
		m.Entries = append(m.Entries, r.changed[i])
	}
	r.rec.begin()
	defer r.rec.end()
	for i, e := range r.changed {
		r.rec.event(engine.Event{Index: i, Change: "Restore " + e.Path, State: engine.EventRunning})
		report(i, component.StepRunning, "")
	}
	done, err := backup.Restore(ctx, r.sys, m)
	for i, e := range r.changed {
		state, ev := component.StepDone, engine.EventDone
		if j := slices.IndexFunc(done, func(d backup.Restored) bool { return d.Path == e.Path }); j < 0 || done[j].Err != nil {
			state, ev = component.StepFailed, engine.EventFailed
		}
		r.rec.event(engine.Event{Index: i, Change: "Restore " + e.Path, State: ev})
		report(i, state, "")
	}
	if err != nil {
		return &flow.Failure{Err: err}
	}
	return nil
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
		backupMock{name: "status-row", note: "Mock (decision 1, alternative (b)): one status row of the screen's own in place of the frame's two copied rows; on the default host.",
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
