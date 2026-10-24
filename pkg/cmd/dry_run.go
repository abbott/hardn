package cmd

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/output"
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
// still opens the menus in dry-run mode, as before. It gates hardn.yml's
// dryRun: true with an operation flag the same way
// (RefuseConfigDryRunOperations; slice 2b, decision 9).
const dryRunFlagApplies = false

// RefuseDryRunOperations returns the usage error for -n (dryRun) combined
// with the named operation flags, or nil when there is nothing to refuse:
// dryRun unset, no operation named, or dryRunFlagApplies set.
func RefuseDryRunOperations(dryRun bool, operations []string) error {
	if dryRunFlagApplies || !dryRun || len(operations) == 0 {
		return nil
	}
	return usageError(fmt.Sprintf("hardn: -n/--dry-run only previews changes, so it can't be combined with %s, which %s them. Nothing was changed.",
		joinOperations(operations), applyVerb(operations)))
}

// RefuseConfigDryRunOperations returns the usage error for plan only set by
// the dryRun of the hardn.yml at path (dryRun), no -n/--dry-run having been
// given, combined with the named operation flags, as RefuseDryRunOperations
// refuses -n with them (refactor plan slice 2b, decision 9): it names the
// file, and --dry-run=false, which the precedence of §3.19 lets win over it.
// nil when there is nothing to refuse, as there.
func RefuseConfigDryRunOperations(dryRun bool, path string, operations []string) error {
	if dryRunFlagApplies || !dryRun || len(operations) == 0 {
		return nil
	}
	return usageError(fmt.Sprintf("hardn: %s sets plan only (dryRun: true), so %s can't run. Nothing was changed. To apply, add --dry-run=false.",
		path, joinOperations(operations)))
}

// usageError is a refusal of the command line: its words alone, and
// output.ErrUsage to errors.Is (code USAGE, exit status ExitUsage). It does
// not wrap ErrUsage with fmt.Errorf("%w: ...") as pkg/output suggests: that
// would put "usage: " before words the user reads on stderr.
type usageError string

func (e usageError) Error() string { return string(e) }

// Unwrap returns output.ErrUsage.
func (e usageError) Unwrap() error { return output.ErrUsage }

// applyVerb is "applies" for one operation, "apply" for several.
func applyVerb(operations []string) string {
	if len(operations) > 1 {
		return "apply"
	}
	return "applies"
}

// joinOperations lists names as "a", "a and b" or "a, b and c".
func joinOperations(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
