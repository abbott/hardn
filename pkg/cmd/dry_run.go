package cmd

import (
	"fmt"
	"strings"
)

// ExitUsage is the exit status for a usage error, bad flags or config: the
// brief's decided exit-code set (brief §2; the codes are listed in Phase 3).
const ExitUsage = 2

// The operation flags of the root command: each one changes the host. -p
// (print logs) and -v (version) only read, so they are not listed. The
// install and sources flags are registered nowhere on origin/main
// (cmd/hardn/main.go comments them out), but the dispatch still reads their
// variables, so they are named here in case they return.
const (
	FlagRunAll           = "-r/--run-all"
	FlagCreateUser       = "-c/--create-user"
	FlagDisableRoot      = "-d/--disable-root"
	FlagConfigureUFW     = "-w/--configure-ufw"
	FlagConfigureDNS     = "-g/--configure-dns"
	FlagSetupSudoEnv     = "-e/--setup-sudo-env"
	FlagInstallLinux     = "-l/--install-linux"
	FlagInstallPython    = "-i/--install-python"
	FlagInstallAll       = "-a/--install-all"
	FlagConfigureSources = "-s/--configure-sources"

	// CommandSetupSudoEnv is the subcommand that does what -e does.
	CommandSetupSudoEnv = "setup-sudo-env"
)

// dryRunFlagApplies: DISABLED 2026-10-17. Flip to true to restore.
//
// With it true, hardn -n combined with an operation flag runs the operation
// for real: cmd/hardn/main.go only sets cfg.DryRun, and no layer below the
// menus reads it, so -r, -c, -d, -w and -e (and setup-sudo-env) apply
// every command and file write they would apply without -n (V1; Phase 0
// side-effects §1, harness A1). That path is the unchanged flag dispatch in
// cmd/hardn/main.go, which runs when RefuseDryRunOperations returns nil.
//
// With it false, -n keeps its documented meaning, preview only: an operation
// flag under -n is a usage error, refused before the root check, the config
// load and OS detection (exit status ExitUsage). -n with no operation flag
// still opens the menus in dry-run mode, as before.
//
// -g is not among the flags cmd/hardn names here since slice 2's unit (d)
// (the maintainer, 2026-10-23): it plans and applies the DNS control
// through the engine, and under -n shows the plan and writes nothing.
const dryRunFlagApplies = false

// RefuseDryRunOperations returns the usage error for -n (dryRun) combined
// with the named operation flags, or nil when there is nothing to refuse:
// dryRun unset, no operation named, or dryRunFlagApplies set.
func RefuseDryRunOperations(dryRun bool, operations []string) error {
	if dryRunFlagApplies || !dryRun || len(operations) == 0 {
		return nil
	}
	verb := "applies"
	if len(operations) > 1 {
		verb = "apply"
	}
	return fmt.Errorf("hardn: -n/--dry-run only previews changes, so it can't be combined with %s, which %s them. Nothing was changed.",
		joinOperations(operations), verb)
}

// joinOperations lists names as "a", "a and b" or "a, b and c".
func joinOperations(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
