package config

import "os"

// The session's mode, "Plan only" or live (refactor plan §3.19, approved:
// "dryRun precedence: explicit flag wins, otherwise config"; slice 2b,
// decisions 1, 2 and 8, adopted 2026-10-23, docs/engine/gallery-dryrun.md
// §4). DryRun is the mode the session runs in, which every legacy menu
// reads; the dryRun the file gave, as loaded, is kept beside it, so that
// the legacy saves write that one (SaveConfig) and the session's mode never
// lands in a file.

// DryRunSource is what set plan only (ResolveDryRun): what the mode
// indicator names after "Plan only", and what the refusal of plan only with
// an operation flag names. A live session has none, DryRunDefault: live
// names nothing (decision 2).
type DryRunSource int

const (
	// DryRunDefault: no source. The session is live, whatever made it so
	// (no file, a file's false, --dry-run=false); or plan only was set
	// inside the session (SetDryRun: item 6, Run All's option 2).
	DryRunDefault DryRunSource = iota
	// DryRunFlag: plan only from -n/--dry-run given on the command line.
	DryRunFlag
	// DryRunFile: plan only from the dryRun of the hardn.yml loaded (Path),
	// as loaded.
	DryRunFile
)

// ResolveDryRun sets the session's mode, DryRun, by §3.19's precedence:
// flag when -n/--dry-run was given on the command line (given), true or
// false; otherwise the dryRun of the file c was loaded from, as loaded;
// otherwise live. DryRunSource names which set plan only, and nothing for
// live.
func (c *Config) ResolveDryRun(given, flag bool) {
	source := DryRunDefault
	switch {
	case given:
		c.DryRun, source = flag, DryRunFlag
	case c.loaded.path != "":
		c.DryRun, source = c.loaded.dryRun, DryRunFile
	default:
		c.DryRun = false
	}
	if !c.DryRun {
		source = DryRunDefault // live names no source (decision 2)
	}
	c.loaded.source, c.loaded.resolved = source, c.DryRun
}

// SetDryRun sets the session's mode from inside the session (item 6, Run
// All's option 2, until slice 2b's unit (d) removes them): DryRun on, with
// no source (decision 2). No legacy save writes it: SaveConfig writes
// dryRun as loaded.
func (c *Config) SetDryRun(on bool) {
	c.DryRun, c.loaded.source = on, DryRunDefault
}

// DryRunSource is what set plan only as the session is now: ResolveDryRun's
// source while DryRun still holds the value it set, and DryRunDefault once
// anything else has set it (SetDryRun, or a write to DryRun), or for live.
func (c *Config) DryRunSource() DryRunSource {
	if c.DryRun != c.loaded.resolved {
		return DryRunDefault
	}
	return c.loaded.source
}

// FileDryRun is the dryRun of the hardn.yml LoadConfig reads for
// explicitPath, and that file's absolute path, found and read as LoadConfig
// finds and reads it (-f, else HARDN_CONFIG, else the search path), but
// quietly: it logs nothing, asks nothing and creates nothing, so hardn can
// read it before its root check (decision 9). "" and false when there is no
// file; an error when the file does not load, which LoadConfig reports too.
func FileDryRun(explicitPath string) (path string, dryRun bool, err error) {
	for _, p := range ConfigFileSearchPath(explicitPath) {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		c, err := readConfigFile(p)
		if err != nil {
			return "", false, err
		}
		return c.Path(), c.DryRun, nil
	}
	return "", false, nil
}
