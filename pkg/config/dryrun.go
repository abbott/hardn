package config

import "os"

// The session's mode, "Plan only" or live (refactor plan §3.19, approved:
// "dryRun precedence: explicit flag wins, otherwise config"; slice 2b,
// decisions 1, 2 and 8, adopted 2026-10-23, docs/engine/gallery-dryrun.md
// §4). DryRun is the mode the session runs in, which every legacy menu
// reads; the dryRun the file gave, as loaded, is kept beside it, so that
// the legacy saves write that one (SaveConfig) and the session's mode never
// lands in a file.

// DryRunSource is what set the session's mode (ResolveDryRun): what the
// mode indicator names after "Plan only", and what the refusal of plan only
// with an operation flag names.
type DryRunSource int

const (
	// DryRunDefault: neither the command line nor a file set the mode, so
	// it is live, the default; or the session has set it itself since
	// (SetDryRun: item 6, Run All's option 2), which names no source either.
	DryRunDefault DryRunSource = iota
	// DryRunFlag: -n/--dry-run given on the command line, true or false.
	DryRunFlag
	// DryRunFile: the dryRun of the hardn.yml loaded (Path), as loaded.
	DryRunFile
)

// ResolveDryRun sets the session's mode, DryRun, by §3.19's precedence:
// flag when -n/--dry-run was given on the command line (given), true or
// false; otherwise the dryRun of the file c was loaded from, as loaded;
// otherwise live. DryRunSource names which.
func (c *Config) ResolveDryRun(given, flag bool) {
	switch {
	case given:
		c.DryRun, c.loaded.source = flag, DryRunFlag
	case c.loaded.path != "":
		c.DryRun, c.loaded.source = c.loaded.dryRun, DryRunFile
	default:
		c.DryRun, c.loaded.source = false, DryRunDefault
	}
	c.loaded.resolved = c.DryRun
}

// SetDryRun sets the session's mode from inside the session (item 6, Run
// All's option 2, until slice 2b's unit (d) removes them): DryRun on, with
// no source (decision 2). Nothing saves it (SaveConfig).
func (c *Config) SetDryRun(on bool) {
	c.DryRun, c.loaded.source = on, DryRunDefault
}

// DryRunSource is what set the session's mode as it is now: ResolveDryRun's
// source while DryRun still holds the value it set, and DryRunDefault once
// anything else has set it (SetDryRun, or a write to DryRun).
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
