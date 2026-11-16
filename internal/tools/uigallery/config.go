package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	configscreen "github.com/abbott/hardn/pkg/screens/config"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Configuration screen's mocks (docs/roadmap.md, "Configuration";
// docs/engine/gallery-config.md, docs/engine/screen-config.md), and, since
// 2026-11-15, its editing's (docs/engine/design-config-edit.md): "Edit a
// setting", a value's flow, a list's screen.
// "uigallery config" lists them and "play config" runs hardn's screen
// (pkg/screens/config) on them. The listing's first mock, "menu", is the
// Configuration group, main menu 8 (configscreen.RunGroup), which leads to the
// screen on the typical file, to DNS Settings, NTP Settings and Syslog Settings; "play
// config/menu" runs it. Each mock is a hardn.yml, loaded by the real loader
// (config.Read) from a simulated host (Ubuntu 24.04, or Alpine 3.23 for the
// logger's note), which a save writes to, and how hardn found it, which the
// mock says itself: config.FindConfigFile reads this machine and is never called.

// configPrefix names the listing, its goldens and play's screens.
const configPrefix = "config"

// configMainMock is the mock the group's hardn.yml leads to: the typical file.
const configMainMock = configPrefix + "/etc"

// configGroupName names the Configuration group in the listing and in play,
// after configPrefix; configGroupMock is its whole name.
const (
	configGroupName = "menu"
	configGroupMock = configPrefix + "/" + configGroupName
	configGroupNote = "Main menu 8: hardn.yml, DNS Settings, NTP Settings and Syslog Settings. It reads nothing."
)

// configLinks lead from a menu mock's item to the Configuration group, by the
// menu mock's name and the item's key, beside links: the main menu's 8.
var configLinks = map[string]string{mainMenuMock + "/8": configGroupMock}

// configLabel is the source label every mock's note opens with.
const configLabel = "Proposed: no Figma frame."

// configMock is one hardn.yml and how hardn found it.
type configMock struct {
	// name is the mock's, after configPrefix; label and about its item in play's
	// menu of the mocks; note what it shows.
	name, label, about, note string
	// path is the file and by how it was looked for; missing, that -f or
	// HARDN_CONFIG names it and it is not there.
	path    string
	by      config.FoundBy
	missing bool
	// yml is the file's content, read only where there is a file.
	yml string
	// alpine: the host is Alpine 3.23, whose logger is busybox syslogd; else
	// Ubuntu 24.04, rsyslog's.
	alpine bool
}

// configTypical is a typical hardn.yml: a user and a key, SSH moved, ufw's SSH
// policy and one profile, DNS and auto updates on; the rest left out.
func configTypical() string {
	return "username: george\nsshKeys:\n  - \"" + synthKey(1, "george@laptop-1") + "\"\n" +
		"sshPort: 2222\ndisableRootSSH: true\n" +
		"enableUfwSshPolicy: true\nufwAppProfiles:\n  - name: tailscale\n    title: Tailscale\n    description: Mesh VPN\n    ports: [\"41641/udp\"]\n" +
		"configureDns: true\nnameservers:\n  - 1.1.1.1\n  - 1.0.0.1\nenableAutoUpdates: true\n"
}

// configUnusedYML is configTypical with keys nothing reads. permitRootLogin is
// "yes": hardn says the key isn't used only where it differs from the default
// (cmd/hardn/output.go, notUsed), and its bool is no Deprecation (rootlogin.go).
const configUnusedYML = "sudoNoPassword: false\nenableLynis: true\nsshListenAddress: 192.0.2.10\n" +
	"logFile: /srv/log/hardn.log\npermitRootLogin: \"yes\"\n"

// configMocks are the mocks, in the listing's order.
func configMocks() []configMock {
	return []configMock{
		{name: "etc", label: "Search path", about: "A typical file", path: hardnYML, by: config.FoundSearch, yml: configTypical(),
			note: "A typical file, the first on the search path. A key the file leaves out says default."},
		{name: "flag", label: "-f", about: "The same file, by -f", path: "/root/site.yml", by: config.FoundFlag, yml: configTypical(),
			note: "The same file, named by -f /root/site.yml."},
		{name: "env", label: "HARDN_CONFIG", about: "The same file, by HARDN_CONFIG", path: "/home/george/.config/hardn/hardn.yml", by: config.FoundEnv, yml: configTypical(),
			note: "The same file, named by HARDN_CONFIG."},
		{name: "none", label: "No file", about: "Every key a default", by: config.FoundNone,
			note: "No file anywhere: every key at its default."},
		{name: "minimal", label: "Minimal", about: "A two-line file", path: hardnYML, by: config.FoundSearch, yml: "username: george\nnameservers: [192.0.2.53]\n",
			note: "A two-line file: username and nameservers, everything else default."},
		{name: "syslog", label: "Syslog servers", about: "Three, the row stacked", path: hardnYML, by: config.FoundSearch, yml: configTypical() + lfYMLThree,
			note: "The typical file with three syslogServers: addresses alone, over the row's width, so under the key."},
		{name: "alpine-tcp", label: "Alpine, TCP syslog", about: "The logger can't use it", path: hardnYML, by: config.FoundSearch, yml: configTypical() + lfYMLTCP, alpine: true,
			note: "The typical file with a TCP syslog server, on Alpine 3.23, whose busybox syslogd forwards over UDP only: a line under syslogServers says so."},
		{name: "unused", label: "Not used", about: "Keys nothing reads", path: hardnYML, by: config.FoundSearch, yml: configTypical() + configUnusedYML,
			note: "The file also sets sudoNoPassword, enableLynis, sshListenAddress, logFile and permitRootLogin, which nothing reads: the Not used section."},
		{name: "missing", label: "Missing", about: "HARDN_CONFIG's file is not there", path: "/home/george/hardn.yml", by: config.FoundEnv, missing: true,
			note: "HARDN_CONFIG names a file that is not there: hardn looks nowhere else."},
	}
}

// findConfigMock returns the mock named name, after configPrefix.
func findConfigMock(name string) (configMock, bool) {
	for _, m := range configMocks() {
		if m.name == name {
			return m, true
		}
	}
	return configMock{}, false
}

// found is how hardn found the mock's file, as config.FindConfigFile says it.
func (m configMock) found() config.Found {
	return config.Found{Path: m.path, By: m.by, Exists: m.by != config.FoundNone && !m.missing}
}

// host is the mock's host: Ubuntu 24.04 with rsyslog, or Alpine 3.23 with
// busybox syslogd, holding the mock's file where there is one.
func (m configMock) host() *fakeHost {
	h := lfUbuntu("")
	if m.alpine {
		h = lfAlpine("")
	}
	if m.found().Exists {
		h.f.AddFile(m.path, []byte(m.yml), 0o600, 0, 0)
	}
	return h
}

// loadFrom is the mock's hardn.yml as hardn loads it, by the real loader from
// h, the mock's host; the defaults where there is no file.
func (m configMock) loadFrom(ctx context.Context, h *fakeHost) (*config.Config, error) {
	if !m.found().Exists {
		return config.DefaultConfig(), nil
	}
	return config.Read(ctx, h.sys(), m.path)
}

// view is the mock's screen, by the real screen's mapping (configscreen.View),
// with the logger's note where the mock's host raises it (decision 13), as the
// screen itself reads it.
func (m configMock) view(ctx context.Context) (screen.Config, error) {
	h := m.host()
	cfg, err := m.loadFrom(ctx, h)
	if err != nil {
		return screen.Config{}, err
	}
	v := configscreen.View(cfg, m.found())
	facts, err := detect.Detect(ctx, h.sys())
	if err != nil {
		return screen.Config{}, err
	}
	if note := configscreen.LoggerNote(ctx, h.sys(), facts, cfg); note != "" {
		v.Notes = map[string]string{"syslogServers": note}
	}
	return v, nil
}

// configGroup runs hardn's Configuration group (configscreen.RunGroup) until the
// user goes back from it: its note above it; its hardn.yml the screen on the
// typical file, its DNS Settings, NTP Settings and Syslog Settings the screens
// on dnsMainHost, tsMainHost and lfMainHost.
func (s *session) configGroup(ctx context.Context) error {
	if err := s.writeNote(configGroupMock, configGroupNote); err != nil {
		return err
	}
	return configscreen.RunGroup(ctx, configscreen.GroupOptions{
		Prompter:      s.p,
		File:          s.openConfig(configMainMock),
		DNS:           s.openDNS(dnsMainHost),
		TimeSync:      s.openTimeSync(tsMainHost),
		LogForwarding: s.openLogForward(lfMainHost),
	})
}

// openConfig returns the action that opens what name names: the mocks' menu for
// configPrefix alone, the Configuration group for configGroupMock, the screen on
// one mock for "config/<mock>".
func (s *session) openConfig(name string) func() error {
	if name == configGroupMock {
		return func() error { return s.configGroup(context.Background()) }
	}
	if m, ok := findConfigMock(strings.TrimPrefix(name, configPrefix+"/")); ok && name != configPrefix {
		return func() error { return s.configScreen(context.Background(), m) }
	}
	return func() error {
		menu := hostsMenu("Configuration files", func(add func(label, about string, action func() error)) {
			for _, m := range configMocks() {
				add(m.label, m.about, func() error { return s.configScreen(context.Background(), m) })
			}
		})
		return s.p.Run(func() nav.Screen { return menu.vm.Screen(s.p.Ctx, menu.menu) })
	}
}

// configScreen runs hardn's Configuration screen on m until the user goes back from
// it, as lfScreen runs Syslog Settings: its note above it; the file as hardn loads
// it from the mock's host, and how it was found the mock's own word; the gallery's
// engine over that host, each save recorded for the end of play. A dry run when
// play plays the dry-run sequence: a save stops at its plan.
func (s *session) configScreen(ctx context.Context, m configMock) error {
	h, title := m.host(), configPrefix+"/"+m.name
	sys := h.sys()
	facts, err := detect.Detect(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	cfg, err := m.loadFrom(ctx, h)
	if err != nil {
		return err
	}
	if err := s.writeNote(title, m.note); err != nil {
		return err
	}
	var rec *recorder
	return configscreen.Run(ctx, configscreen.Options{
		Prompter: s.p, Config: cfg, Found: m.found(), System: sys, Facts: facts,
		DryRun: s.sequence == "dry-run", LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			r := &recorder{host: h, f: h.f, title: title}
			rec = r
			r.begin()
			return updEngine(h, func(ev engine.Event) { r.event(ev); progress(ev) }, s)
		},
		Ran: func(run output.Run) {
			rec.end()
			rec.setPlan(run.Plan)
			rec.failedWith(run.Err)
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, rec)
			s.mu.Unlock()
		},
	})
}

// configEdits are the editing's mocks: a mock's file, the lines typed from its
// hardn.yml screen, and what the listing then shows, the screen itself left out.
// None saves: a save's backup has a random id, which play's goldens mask
// (config_test.go) and a listing's do not.
var configEdits = []struct {
	name, mock, note string
	lines            []string
}{
	{"edit", "etc", "Edit a setting: the keys Configuration edits, numbered, each with its value. The keys another screen saves, and dryRun, adminNoPassword and sshConfigFile, are not here.", []string{"1"}},
	{"edit-port", "etc", "sshPort: 99999 refused under the field and asked again; then the save's plan, the line that changes, and the question.", []string{"1", "3", "99999", "2022"}},
	{"edit-yesno", "etc", "A yes/no key: nothing typed. The plan with the key turned over, and the question naming the value.", []string{"1", "9"}},
	{"edit-username", "etc", "username: a name useradd would refuse is refused and asked again; then the plan. Whether the user exists is not asked.", []string{"1", "1", "George", "bob"}},
	{"edit-timeout", "etc", "confirmTimeout: under the 60 s floor refused.", []string{"1", "10", "30s", "5m"}},
	{"edit-keys", "etc", "sshKeys: each key by type, comment and fingerprint, never whole. A line that is no key is refused.", []string{"1", "2", "1", "not a key"}},
	{"edit-ntp", "etc", "ntpServers: none yet, so Add alone. An IPv6 address refused, then the plan.", []string{"1", "8", "1", "2001:db8::1", "192.0.2.10"}},
	{"edit-syslog", "syslog", "syslogServers: three servers. Add asks the address, the protocol, the port and, for tls, the CA file; the same address and port again is refused by the whole list's check.", []string{"1", "7", "1", "192.0.2.51", "udp", "", "1514"}},
	{"edit-syslog-remove", "syslog", "syslogServers: Remove by number, then the plan.", []string{"1", "7", "2", "2"}},
	{"edit-changed", "etc", "A save refused: the file changed since hardn read it. Nothing is saved.", []string{"1", "3", "2022"}},
}

// configEditDraw draws one of configEdits: the real screen run on the mock's
// host with the lines typed and nothing after them (reviewOf).
func configEditDraw(mock string, lines []string, changed bool) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		m, _ := findConfigMock(mock)
		h := m.host()
		sys := h.sys()
		facts, err := detect.Detect(ctx, sys)
		if err != nil {
			return "", err
		}
		cfg, err := m.loadFrom(ctx, h)
		if err != nil {
			return "", err
		}
		v, err := m.view(ctx)
		if err != nil {
			return "", err
		}
		if changed {
			h.f.AddFile(m.path, []byte(m.yml+"kernel: true\n"), 0o600, 0, 0)
		}
		return reviewOf(c, strings.Join(lines, "\n"), v.SubMenu().Render(c), func(p nav.Prompter) error {
			return configscreen.Run(ctx, configscreen.Options{
				Prompter: p, Config: cfg, Found: m.found(), System: sys, Facts: facts, LogPath: output.LegacyLogPath, Now: clock,
				NewEngine: func(progress func(engine.Event)) *engine.Engine { return updEngine(h, progress, &updQuiet) },
			})
		})
	}
}

// configStart returns the mocks' menu, the group or the mock that args name
// ("config", "config/menu", "config/<mock>"), and whether they name one; an error
// for a mock that is not one.
func configStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != configPrefix && !strings.HasPrefix(args[0], configPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], configPrefix+"/"); args[0] != configPrefix && args[0] != configGroupMock {
		if _, ok := findConfigMock(name); !ok {
			names := []string{configGroupMock}
			for _, m := range configMocks() {
				names = append(names, configPrefix+"/"+m.name)
			}
			return "", false, fmt.Errorf("no config mock %q; the mocks are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}

// configNote is play's note for the Configuration screen.
const configNote = "Play mode: hardn's Configuration screen (pkg/screens/config), proposed with no Figma frame, on a hardn.yml loaded by the real loader (pkg/config) from a simulated host. " +
	"Edit a setting saves one key to that host's file through the real engine. Nothing on this machine changes or is read."

// configListMocks are the listing's mocks: the group, then each mock's screen.
func configListMocks() []listMock {
	out := []listMock{{name: configGroupName, note: configGroupNote, draw: func(_ context.Context, c component.Ctx) (string, error) {
		return screen.ConfigGroup{}.SubMenu().Render(c), nil
	}}}
	for _, m := range configMocks() {
		out = append(out, listMock{name: m.name, note: m.note, draw: func(ctx context.Context, c component.Ctx) (string, error) {
			v, err := m.view(ctx)
			if err != nil {
				return "", err
			}
			return v.SubMenu().Render(c), nil
		}})
	}
	for _, e := range configEdits {
		out = append(out, listMock{name: e.name, note: e.note, draw: configEditDraw(e.mock, e.lines, e.name == "edit-changed")})
	}
	return out
}

// configUsage is the listing's usage line.
const configUsage = "usage: go run ./internal/tools/uigallery config [flags] [mock | all]..."

// configListing is the Configuration listing.
var configListing = listing{prefix: configPrefix, usage: configUsage, label: configLabel, mocks: configListMocks}
