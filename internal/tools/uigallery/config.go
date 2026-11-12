package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	configscreen "github.com/abbott/hardn/pkg/screens/config"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Configuration screen's mocks (docs/roadmap.md, "Configuration";
// docs/engine/gallery-config.md, docs/engine/screen-config.md): read only.
// "uigallery config" lists them and "play config" runs hardn's screen
// (pkg/screens/config) on them. Each mock is a hardn.yml, loaded by the real loader
// (config.Read) from a fake host, and how hardn found it, which the mock says
// itself: config.FindConfigFile reads this machine and is never called.

// configPrefix names the listing, its goldens and play's screens.
const configPrefix = "config"

// configMainMock is the mock the main menu's item leads to: the typical file.
const configMainMock = configPrefix + "/etc"

// configLinks lead from a menu mock's item to the Configuration screen on a mock,
// by the menu mock's name and the item's key, beside links: the main menu's 8.
var configLinks = map[string]string{mainMenuMock + "/8": configMainMock}

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
}

// configTypical is a typical hardn.yml: a user and a key, SSH moved, ufw's SSH
// policy and one profile, DNS and auto updates on; the rest left out.
func configTypical() string {
	return "username: george\nsshKeys:\n  - \"" + synthKey(1, "george@laptop-1") + "\"\n" +
		"sshPort: 2222\ndisableRootSSH: true\n" +
		"enableUfwSshPolicy: true\nufwAppProfiles:\n  - name: tailscale\n    title: Tailscale\n    description: Mesh VPN\n    ports: [\"41641/udp\"]\n" +
		"configureDns: true\nnameservers:\n  - 1.1.1.1\n  - 1.0.0.1\nenableUnattendedUpgrades: true\n"
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

// load is the mock's hardn.yml as hardn loads it, by the real loader from a fake
// host that holds the file alone; the defaults where there is no file.
func (m configMock) load(ctx context.Context) (*config.Config, error) {
	if !m.found().Exists {
		return config.DefaultConfig(), nil
	}
	f := hostfake.New(host.Options{})
	f.AddFile(m.path, []byte(m.yml), 0o600, 0, 0)
	return config.Read(ctx, f, m.path)
}

// view is the mock's screen, by the real screen's mapping (configscreen.View).
func (m configMock) view(ctx context.Context) (screen.Config, error) {
	cfg, err := m.load(ctx)
	if err != nil {
		return screen.Config{}, err
	}
	return configscreen.View(cfg, m.found()), nil
}

// openConfig returns the action that opens what name names: the mocks' menu for
// configPrefix alone, the screen on one mock for "config/<mock>".
func (s *session) openConfig(name string) func() error {
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
// it: its note above it; the file as hardn loads it, and how it was found the
// mock's own word. Nothing is read from a host, written or run; the plan-only
// sequence draws the same screen, as Options has no mode.
func (s *session) configScreen(ctx context.Context, m configMock) error {
	if err := s.writeNote(configPrefix+"/"+m.name, m.note); err != nil {
		return err
	}
	cfg, err := m.load(ctx)
	if err != nil {
		return err
	}
	return configscreen.Run(ctx, configscreen.Options{Prompter: s.p, Config: cfg, Found: m.found()})
}

// configStart returns the mocks' menu, or the mock, that args name ("config",
// "config/<mock>"), and whether they name one; an error for a mock that is not one.
func configStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != configPrefix && !strings.HasPrefix(args[0], configPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], configPrefix+"/"); args[0] != configPrefix {
		if _, ok := findConfigMock(name); !ok {
			var names []string
			for _, m := range configMocks() {
				names = append(names, configPrefix+"/"+m.name)
			}
			return "", false, fmt.Errorf("no config mock %q; the mocks are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}

// configNote is play's note for the Configuration screen.
const configNote = "Play mode: hardn's Configuration screen (pkg/screens/config), proposed with no Figma frame, on a hardn.yml loaded by the real loader (pkg/config). " +
	"It only shows: no plan, no y/N, no journal, no host read. Nothing on this machine changes or is read."

// configListMocks are the listing's mocks: each mock's screen.
func configListMocks() []listMock {
	var out []listMock
	for _, m := range configMocks() {
		out = append(out, listMock{name: m.name, note: m.note, draw: func(ctx context.Context, c component.Ctx) (string, error) {
			v, err := m.view(ctx)
			if err != nil {
				return "", err
			}
			return v.SubMenu().Render(c), nil
		}})
	}
	return out
}

// configUsage is the listing's usage line.
const configUsage = "usage: go run ./internal/tools/uigallery config [flags] [mock | all]..."

// configListing is the Configuration listing.
var configListing = listing{prefix: configPrefix, usage: configUsage, label: configLabel, mocks: configListMocks}
