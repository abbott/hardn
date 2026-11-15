// Package configscreen runs hardn's Configuration screen (docs/roadmap.md,
// "Configuration"): hardn.yml, item 1 of main menu 8's group (RunGroup, group.go),
// as the maintainer adopted the mock on 2026-11-12 (docs/engine/gallery-config.md
// §3, all (a), with §4's words). It shows which hardn.yml hardn read and each
// key's value, drawn by pkg/ui/screen (screen.Config), and, since 2026-11-15,
// edits the keys no other screen saves, one a save (edit.go;
// docs/engine/design-config-edit.md, docs/engine/screen-config.md).
//
// The package reads no hardn.yml and looks for none: its caller gives the
// configuration as loaded and how the file was found (Options), and it renders only
// through pkg/ui. A save goes through the engine (steps.PlanSave).
package configscreen

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The values' words (gallery-config.md §4).
const (
	none = "None"
	// notValid is a syslogServers or an ntpServers that names a server hardn
	// does not set.
	notValid = "Not valid"
	oneKey   = "1 key"
	manyKey  = "%d keys"
)

// Options is what the Configuration screen runs with: what cmd/hardn has when its
// Configuration menu item is chosen.
type Options struct {
	// Prompter asks and writes the screen through the one line reader, rendering
	// with its Ctx.
	Prompter nav.Prompter
	// Config is hardn.yml as hardn loaded it, or the defaults where it loaded none.
	// A save edits it as saved.
	Config *config.Config
	// Found is how the file was looked for when hardn loaded Config
	// (config.FindConfigFile, then): its path, by -f, HARDN_CONFIG or the search
	// path, and whether a file was there.
	Found config.Found
	// System is the host: hardn.yml is saved there, and its logger read for a
	// syslogServers it can't use. Facts are detect's.
	System host.System
	Facts  detect.Facts
	// NewEngine returns the engine one save plans and applies with, progress
	// as its Options.Progress.
	NewEngine func(progress func(engine.Event)) *engine.Engine
	// ApplyContext, when set, returns the context a save runs under and its
	// stop (screenkit.Planning.ApplyContext).
	ApplyContext func(ctx context.Context) (context.Context, context.CancelFunc)
	// DryRun stops each save at its plan: nothing is written.
	DryRun bool
	// LogPath is the log a failure's report names; "" names none.
	LogPath string
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Ran, when set, is given what each save did.
	Ran func(output.Run)
}

// Run shows the Configuration screen until 0 (nil) or q (nav.ErrQuit); the end of
// the input and Ctrl-C abort (prompt.ErrAborted). The screen is View of the
// configuration as it is now, drawn again after each action; where a file was
// read, its item edits one key and saves it (edit.go). The host is read only for
// a syslogServers with a TCP or TLS server, for its logger. Run returns an error
// wrapping fs.ErrInvalid, writing nothing, without a Prompter's In and Out, a
// Config, a System or NewEngine.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.Config == nil || o.System == nil || o.NewEngine == nil {
		return fmt.Errorf("configuration screen: %w: it needs a Prompter with In and Out, a Config, a System and NewEngine", fs.ErrInvalid)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	r := &runner{o: o}
	r.s = screenkit.Session{Name: "configuration screen", Prompter: o.Prompter, System: o.System, Config: o.Config,
		NewEngine: o.NewEngine, ApplyContext: o.ApplyContext, DryRun: o.DryRun, LogPath: o.LogPath, Now: o.Now, Ran: r.ran}
	return o.Prompter.Run(func() nav.Screen { return r.draw(ctx) })
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
		"syslogServers":            logServers(cfg),
		"ntpServers":               timeServers(cfg),
		"kernel":                   strconv.FormatBool(cfg.KernelSettings),
		"enableBackups":            strconv.FormatBool(cfg.EnableBackups), "backupPath": cfg.BackupPath,
		"dryRun": strconv.FormatBool(cfg.FileDryRun()), "confirmTimeout": timeout, "checkReleases": strconv.FormatBool(cfg.CheckReleases),
	}
}

// logServers is syslogServers as its row says the servers, addresses
// alone, "logs.example.net, 192.0.2.51"; None with none; notValid where
// they do not pass config's check, which Set servers refuses.
func logServers(cfg *config.Config) string {
	servers, err := cfg.SyslogServers.Resolve()
	switch {
	case err != nil:
		return notValid
	case len(servers) == 0:
		return none
	}
	var names []string
	for _, s := range servers {
		names = append(names, s.Address)
	}
	return strings.Join(names, ", ")
}

// timeServers is ntpServers as the NTP Settings screen says the servers,
// "192.0.2.10, 192.0.2.11"; None with none; notValid where they do not pass
// config's check, which Set servers refuses.
func timeServers(cfg *config.Config) string {
	servers, err := cfg.NTPServers.Resolve()
	switch {
	case err != nil:
		return notValid
	case len(servers) == 0:
		return none
	}
	return strings.Join(servers, ", ")
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
