// Package configscreen runs hardn's Configuration screen (docs/roadmap.md,
// "Configuration"; unit (b), next/config-screen): main menu 8, as the maintainer
// adopted the mock on 2026-11-12 (docs/engine/gallery-config.md §3, all (a), with
// §4's words). It shows which hardn.yml hardn read and each key's value, drawn by
// pkg/ui/screen (screen.Config). It only shows: no engine, no host, nothing written,
// under plan only too (docs/engine/screen-config.md).
//
// The package reads no hardn.yml and looks for none: its caller gives the
// configuration as loaded and how the file was found (Options), and it renders only
// through pkg/ui.
package configscreen

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The values' words (gallery-config.md §4).
const (
	none    = "None"
	oneKey  = "1 key"
	manyKey = "%d keys"
)

// Options is what the Configuration screen runs with: what cmd/hardn has when its
// Configuration menu item is chosen.
type Options struct {
	// Prompter asks and writes the screen through the one line reader, rendering
	// with its Ctx.
	Prompter nav.Prompter
	// Config is hardn.yml as hardn loaded it, or the defaults where it loaded none.
	Config *config.Config
	// Found is how the file was looked for when hardn loaded Config
	// (config.FindConfigFile, then): its path, by -f, HARDN_CONFIG or the search
	// path, and whether a file was there.
	Found config.Found
}

// Run shows the Configuration screen until 0 (nil) or q (nav.ErrQuit); the end of
// the input and Ctrl-C abort (prompt.ErrAborted). It reads and writes nothing: the
// screen is View of what Options holds, the same on every draw, and under plan only,
// as Options has no mode. Run returns an error wrapping fs.ErrInvalid, writing
// nothing, without a Prompter's In and Out or a Config.
func Run(_ context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.Config == nil {
		return fmt.Errorf("configuration screen: %w: it needs a Prompter with In and Out, and a Config", fs.ErrInvalid)
	}
	sub := View(o.Config, o.Found).SubMenu()
	menu := nav.Menu{Exit: sub.Menu.Exit}
	return o.Prompter.Run(func() nav.Screen { return sub.Screen(o.Prompter.Ctx, menu) })
}

// PrintOptions is what Print writes with: hardn config's.
type PrintOptions struct {
	// Out takes the report; Env is its render environment.
	Out io.Writer
	Env term.Env
	// Config and Found are Options'.
	Config *config.Config
	Found  config.Found
}

// Print writes what the screen shows, View of Config and Found, with no menu and no
// prompt, and nothing else. To a terminal it is as the screen draws it; off one,
// plain, each value whole, as systemdetails.Print's. It returns an error wrapping
// fs.ErrInvalid, writing nothing, without Out or a Config, and a failed write.
func Print(o PrintOptions) error {
	if o.Out == nil || o.Config == nil {
		return fmt.Errorf("configuration report: %w: it needs Out and a Config", fs.ErrInvalid)
	}
	c, env := component.NewCtx(o.Env), o.Env
	if !o.Env.TTY {
		c = component.OffTerminal()
		env = c.Env
	}
	if _, err := io.WriteString(env.Writer(o.Out), View(o.Config, o.Found).Report(c)+"\n"); err != nil {
		return fmt.Errorf("configuration report: write: %w", err)
	}
	return nil
}

// View is the screen's view of cfg, a hardn.yml as loaded, and found, how the file
// was looked for when hardn loaded it: the one mapping from the two to screen.Config.
//
// The file is the one cfg was loaded from (Config.Path), whatever is there since,
// and "From" how hardn found it then; with a found that says no file was there, as
// for one the first run's offer created off the search path (cmd/hardn's
// loadedFound), it has no "From". A cfg loaded from no file reads as none read:
// found's path not found where -f or HARDN_CONFIG names it.
//
// A value is the file's where the file gives its key a value (Config.FileSets): a
// null keeps the default, as the loader reads it, and so does an empty
// confirmTimeout; disableRoot sets disableRootSSH. sshKeys is a count, never a key.
// Every string of the file's is made safe to show (logview.Safe).
func View(cfg *config.Config, found config.Found) screen.Config {
	v := screen.Config{Values: map[string]screen.ConfigValue{}}
	by := map[config.FoundBy]screen.ConfigFound{
		config.FoundFlag: screen.ConfigByFlag, config.FoundEnv: screen.ConfigByEnv, config.FoundSearch: screen.ConfigBySearch,
	}
	switch path := cfg.Path(); {
	case path != "" && found.Exists && found.By != config.FoundNone:
		v.Path, v.By = path, by[found.By]
	case path != "":
		v.Path, v.By = path, screen.ConfigRead
	case found.By == config.FoundFlag || found.By == config.FoundEnv:
		v.Path, v.By, v.Missing = found.Path, by[found.By], true
	}
	v.Path = logview.Safe(v.Path)
	for k, t := range texts(cfg) {
		set := cfg.FileSets(k) && (k != "confirmTimeout" || cfg.ConfirmTimeout != "")
		v.Values[k] = screen.ConfigValue{Text: logview.Safe(t), Set: set}
	}
	v.Unused = unused(cfg)
	return v
}

// texts are cfg's values as the screen words them, by key (screen.ConfigKeys). Two
// are the file's own, not the session's: dryRun, which -n sets for the session
// (Config.FileDryRun), and username, which -u does (Config.FileUsername).
func texts(cfg *config.Config) map[string]string {
	list := func(items []string) string {
		if len(items) == 0 {
			return none
		}
		return fmt.Sprintf("%d: %s", len(items), strings.Join(items, ", "))
	}
	sshKeys := none
	switch n := len(cfg.SshKeys); {
	case n == 1:
		sshKeys = oneKey
	case n > 1:
		sshKeys = fmt.Sprintf(manyKey, n)
	}
	profiles := none
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
	return map[string]string{
		"username": cfg.FileUsername(), "adminNoPassword": strconv.FormatBool(cfg.AdminNoPassword), "sshKeys": sshKeys,
		"sshPort": strconv.Itoa(cfg.SshPort), "disableRootSSH": strconv.FormatBool(cfg.DisableRootSSH), "sshConfigFile": cfg.SshConfigFile,
		"enableUfwSshPolicy": strconv.FormatBool(cfg.EnableUfwSshPolicy), "ufwAppProfiles": profiles, "ufwTrustedInterfaces": list(cfg.UfwTrustedInterfaces),
		"configureDns": strconv.FormatBool(cfg.ConfigureDns), "nameservers": list(cfg.Nameservers),
		"enableUnattendedUpgrades": strconv.FormatBool(cfg.EnableUnattendedUpgrades),
		"enableBackups":            strconv.FormatBool(cfg.EnableBackups), "backupPath": cfg.BackupPath,
		"dryRun": strconv.FormatBool(cfg.FileDryRun()), "confirmTimeout": timeout,
	}
}

// unused are the keys of cfg's file that nothing reads, in the order of hardn's
// start-up warning (cmd/hardn's loadWarnings): logFile where it names another path
// (logview.Elsewhere), permitRootLogin where it is not the default, then the retired
// keys (Config.Retired), each with its value where that is one word or number.
func unused(cfg *config.Config) []screen.ConfigUnused {
	var out []screen.ConfigUnused
	if p := logview.Elsewhere(cfg.LogFile); p != "" {
		out = append(out, screen.ConfigUnused{Key: "logFile", Value: logview.Safe(p)})
	}
	if r := cfg.RootLogin(); r != config.DefaultConfig().RootLogin() {
		out = append(out, screen.ConfigUnused{Key: "permitRootLogin", Value: string(r)})
	}
	for _, k := range cfg.Retired() {
		out = append(out, screen.ConfigUnused{Key: k, Value: logview.Safe(cfg.FileWord(k))})
	}
	return out
}
