package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Configuration screen's mocks (docs/ROADMAP.md, "View and manage hardn.yml";
// unit (a), next/config-gallery; docs/engine/gallery-config.md): a proposal, read
// only. "uigallery config" lists them. Each mock is a hardn.yml, loaded by the real
// loader (config.Read) from a fake host, and how hardn found it, which the mock
// says itself: config.FindConfigFile reads this machine and is never called.
// Nothing in hardn draws the screen yet.

// configPrefix names the listing and its goldens.
const configPrefix = "config"

// configLabel is the source label every mock's note opens with.
const configLabel = "Proposed: no Figma frame."

// configMock is one hardn.yml and how hardn found it.
type configMock struct {
	name, note string
	// path is the file and by how it was looked for; missing, that -f or
	// HARDN_CONFIG names it and it is not there.
	path    string
	by      screen.ConfigFound
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
		{name: "etc", path: hardnYML, by: screen.ConfigBySearch, yml: configTypical(),
			note: "A typical file, the first on the search path. A key the file leaves out says default."},
		{name: "flag", path: "/root/site.yml", by: screen.ConfigByFlag, yml: configTypical(),
			note: "The same file, named by -f /root/site.yml."},
		{name: "env", path: "/home/george/.config/hardn/hardn.yml", by: screen.ConfigByEnv, yml: configTypical(),
			note: "The same file, named by HARDN_CONFIG."},
		{name: "none", by: screen.ConfigNoFile,
			note: "No file anywhere: every key at its default."},
		{name: "minimal", path: hardnYML, by: screen.ConfigBySearch, yml: "username: george\nnameservers: [192.0.2.53]\n",
			note: "A two-line file: username and nameservers, everything else default."},
		{name: "unused", path: hardnYML, by: screen.ConfigBySearch, yml: configTypical() + configUnusedYML,
			note: "The file also sets sudoNoPassword, enableLynis, sshListenAddress, logFile and permitRootLogin, which nothing reads: the Not used section."},
		{name: "missing", path: "/home/george/hardn.yml", by: screen.ConfigByEnv, missing: true,
			note: "HARDN_CONFIG names a file that is not there: hardn looks nowhere else."},
	}
}

// load is the mock's hardn.yml as hardn loads it, by the real loader from a fake
// host that holds the file alone, with the file's top-level keys; the defaults and
// no key where there is no file.
func (m configMock) load(ctx context.Context) (*config.Config, map[string]any, error) {
	if m.by == screen.ConfigNoFile || m.missing {
		return config.DefaultConfig(), nil, nil
	}
	f := hostfake.New(host.Options{})
	f.AddFile(m.path, []byte(m.yml), 0o600, 0, 0)
	cfg, err := config.Read(ctx, f, m.path)
	if err != nil {
		return nil, nil, err
	}
	var keys map[string]any
	if err := yaml.Unmarshal([]byte(m.yml), &keys); err != nil {
		return nil, nil, err
	}
	return cfg, keys, nil
}

// view is the mock's screen.
func (m configMock) view(ctx context.Context) (screen.Config, error) {
	cfg, keys, err := m.load(ctx)
	if err != nil {
		return screen.Config{}, err
	}
	return configView(cfg, keys, m.path, m.by, m.missing), nil
}

// configView is the Configuration screen's view of cfg, a hardn.yml as loaded:
// keys are the file's top-level keys, by which a value is the file's or a default;
// path, by and missing say how the file was found.
//
// STAND-IN: this is the one mapping from a loaded *config.Config to the screen.
// The screen unit moves it into pkg/screens, with the file's keys and how it was
// found read from hardn itself (gallery-config.md, "For unit (b)").
func configView(cfg *config.Config, keys map[string]any, path string, by screen.ConfigFound, missing bool) screen.Config {
	list := func(items []string) string {
		if len(items) == 0 {
			return "None"
		}
		return fmt.Sprintf("%d: %s", len(items), strings.Join(items, ", "))
	}
	sshKeys := "None"
	switch n := len(cfg.SshKeys); {
	case n == 1:
		sshKeys = "1 key" // never the key's text
	case n > 1:
		sshKeys = fmt.Sprintf("%d keys", n)
	}
	profiles := "None"
	if len(cfg.UfwAppProfiles) > 0 {
		var names []string
		for _, p := range cfg.UfwAppProfiles {
			names = append(names, p.Name)
		}
		profiles = strings.Join(names, ", ")
	}
	timeout := cfg.ConfirmTimeout
	if timeout == "" {
		timeout = strings.TrimSuffix(revert.DefaultTimeout.String(), "0s") // "3m"
	}
	text := map[string]string{
		"username": cfg.Username, "adminNoPassword": strconv.FormatBool(cfg.AdminNoPassword), "sshKeys": sshKeys,
		"sshPort": strconv.Itoa(cfg.SshPort), "disableRootSSH": strconv.FormatBool(cfg.DisableRootSSH), "sshConfigFile": cfg.SshConfigFile,
		"enableUfwSshPolicy": strconv.FormatBool(cfg.EnableUfwSshPolicy), "ufwAppProfiles": profiles, "ufwTrustedInterfaces": list(cfg.UfwTrustedInterfaces),
		"configureDns": strconv.FormatBool(cfg.ConfigureDns), "nameservers": list(cfg.Nameservers),
		"enableUnattendedUpgrades": strconv.FormatBool(cfg.EnableUnattendedUpgrades),
		"enableBackups":            strconv.FormatBool(cfg.EnableBackups), "backupPath": cfg.BackupPath,
		"dryRun": strconv.FormatBool(cfg.DryRun), "confirmTimeout": timeout,
	}
	v := screen.Config{Path: path, By: by, Missing: missing, Values: map[string]screen.ConfigValue{}}
	// A value is the file's where the file has its key with a value: a null
	// keeps the default, as the loader reads it, and so does an empty
	// confirmTimeout; disableRoot is disableRootSSH's old name
	// (pkg/config/rootlogin.go). Every string of the file's is made safe to
	// show (logview.Safe): a control character in one never reaches the
	// terminal.
	for k, t := range text {
		set := keys[k] != nil
		switch {
		case k == "disableRootSSH" && keys["disableRoot"] != nil:
			set = true
		case k == "confirmTimeout" && cfg.ConfirmTimeout == "":
			set = false
		}
		v.Values[k] = screen.ConfigValue{Text: logview.Safe(t), Set: set}
	}
	// The keys nothing reads, in the order of hardn's start-up warning
	// (cmd/hardn's loadWarnings): logFile where it names another path
	// (logview.Elsewhere), permitRootLogin where it is not the default, then
	// the retired keys.
	if p := logview.Elsewhere(cfg.LogFile); p != "" {
		v.Unused = append(v.Unused, screen.ConfigUnused{Key: "logFile", Value: logview.Safe(p)})
	}
	if r := cfg.RootLogin(); r != config.DefaultConfig().RootLogin() {
		v.Unused = append(v.Unused, screen.ConfigUnused{Key: "permitRootLogin", Value: string(r)})
	}
	for _, k := range cfg.Retired() {
		u := screen.ConfigUnused{Key: k}
		switch val := keys[k].(type) {
		case string, bool, int:
			u.Value = logview.Safe(fmt.Sprint(val))
		}
		v.Unused = append(v.Unused, u)
	}
	return v
}

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
