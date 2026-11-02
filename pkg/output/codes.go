package output

// The code registry: every stable code hardn's output carries, in one file
// (refactor plan P3, next/p3-output). Errors get theirs by errors.Is and
// errors.As, in the order of errorCodes: the first entry that matches wins,
// so the more specific comes first (docs/engine/steps.md §7: a steps.Refusal
// before the primitive's refusal it wraps; a typed error told apart by a
// field before the sentinel it Is; a sentinel that wraps another before the
// one it wraps; the engine's before the primitives' its errors wrap; the
// standard library's, and the engine's whole-plan wrappers, last).
//
// A code never changes or goes once released (docs/reference/output.md):
// testdata/codes.golden pins the list and testdata/sentinels.golden each
// exported sentinel's code, and TestSentinelsRegistered fails for an
// exported Err* of pkg/host/... or pkg/engine/... that no entry matches.

import (
	"context"
	"errors"
	"io/fs"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/apparmor"
	"github.com/abbott/hardn/pkg/engine/controls/environment"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/revert/plans"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// Kind is what a code names.
type Kind string

// The kinds of code.
const (
	KindError   Kind = "error"   // an error's (errors[].code)
	KindFinding Kind = "finding" // a finding's (findings[].code)
	KindBoot    Kind = "boot"    // what hardn rollback --boot did, and why (revert.BootAction, revert.BootReason)
)

// CodeUnclassified is the code of an error no entry matches. It is a code
// like the others, so a script can match it, but it says only that the
// error is not one hardn knows by name; its message says what it is.
const CodeUnclassified = "UNCLASSIFIED_ERROR"

// Entry is one code of the registry.
type Entry struct {
	Code string
	Kind Kind
	// Exit is the exit status an error of this code gives a run (ExitCode):
	// ExitError, ExitUsage or ExitInterrupted; 0 for the other kinds.
	Exit int
	// What says what the code means, in one line; for an entry that
	// matches sentinels and has none of its own, it is the first
	// sentinel's text.
	What string

	sentinels []error
	match     func(error) bool // nil: errors.Is against sentinels
}

// matches reports whether err is of this entry's code.
func (e Entry) matches(err error) bool {
	if e.match != nil {
		return e.match(err)
	}
	for _, s := range e.sentinels {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

// is is an error entry matched by errors.Is against sentinels.
func is(code string, sentinels ...error) Entry {
	return Entry{Code: code, Kind: KindError, Exit: ExitError, What: sentinels[0].Error(), sentinels: sentinels}
}

// typed is an error entry matched by its own function, for a typed error
// told apart by a field.
func typed(code, what string, match func(error) bool) Entry {
	return Entry{Code: code, Kind: KindError, Exit: ExitError, What: what, match: match}
}

// exit is e with exit status code.
func (e Entry) exit(code int) Entry { e.Exit = code; return e }

// what is e saying what, for a sentinel whose own text says too little.
func (e Entry) what(what string) Entry { e.What = what; return e }

// as is a matcher for an error of type T for which ok holds.
func as[T error](ok func(T) bool) func(error) bool {
	return func(err error) bool {
		var t T
		return errors.As(err, &t) && ok(t)
	}
}

func envKind(k sshd.EnvKind) func(error) bool {
	return as(func(e *sshd.EnvError) bool { return e.Kind == k })
}

func noRollback(k revert.NoRollbackKind) func(error) bool {
	return as(func(e *revert.NoRollbackError) bool { return e.Kind == k })
}

func sudoersRejected(s sudoers.Stage) func(error) bool {
	return as(func(e *sudoers.CheckError) bool { return e.Stage == s && errors.Is(e, sudoers.ErrRejected) })
}

// errorCodes are the error codes, in precedence order: the first that
// matches is an error's code.
var errorCodes = []Entry{
	// An interruption, whatever it interrupted: Ctrl-C, SIGTERM or a hangup,
	// which the caller turns into a cancelled context (docs/engine/engine.md
	// §2.14), or a prompt the user aborted (Ctrl-C, the end of the input).
	is("CANCELLED", context.Canceled, prompt.ErrAborted).exit(ExitInterrupted).what("interrupted: Ctrl-C, SIGTERM or a hangup, or a prompt aborted (Ctrl-C, the end of the input)"),

	// The engine's change kinds (pkg/engine/steps): before the primitives'
	// refusals they wrap (steps.md §7).
	is(steps.CodeListenConfNotInEffect, steps.ErrListenConfNotInEffect).what("v0.3.x's listen.conf never took effect (D9): refused, with a report (P-39 (a))"),
	typed(steps.CodeNoRunDir, "sshd -T cannot run: its privilege separation directory (/run/sshd) is missing or not root's alone",
		func(err error) bool {
			return errors.Is(err, steps.ErrNoRunDir) || envKind(sshd.EnvPrivsepDir)(err)
		}),
	is("SSHD_NO_WRITE_STRATEGY", steps.ErrNoWriteStrategy),
	is("SSH_MIGRATE_FIRST", steps.ErrMigrateFirst),
	is("SSH_PORT_NO_NEW_LISTENER", steps.ErrNoNewListener),
	is("SSHD_CHANGE_NOT_IN_EFFECT", steps.ErrNotInEffect),
	is("SSHD_FILE_CHANGED", steps.ErrChanged).what("a file the SSH change writes is not as the plan read it"),
	is("PVE_ROOT_LOGIN_NOT_KEPT", steps.ErrProxmoxRoot),
	is("SSH_CLEANUP_SESSION_NOT_KEPT", steps.ErrSessionNotKept),
	// The save of hardn.yml (steps.PlanSave, next/s2-control).
	is(steps.CodeConfigChanged, steps.ErrConfigChanged, config.ErrChanged).what("hardn.yml changed since it was read: saving would undo what changed it"),

	// The engine's own, before the primitives' errors they wrap.
	is("CONFIRM_WINDOW_CLOSING", engine.ErrWindow),
	is("BOOT_UNIT_NOT_INSTALLED", engine.ErrBootUnit),
	is("PLATFORM_UNSUPPORTED", engine.ErrUnsupported),
	is("ENGINE_CHANGE_INVALID", engine.ErrChange),
	is("ENGINE_ORDER", engine.ErrOrder),
	is("ENGINE_CONFLICT", engine.ErrConflict),
	is("PLAN_CHANGED", engine.ErrPlanChanged),
	is("PLAN_USED", engine.ErrPlanUsed),
	is("CONTROL_NOT_MIGRATED", engine.ErrNotMigrated), // a check-only control's Plan (next/p4-check-ssh)

	// This package's: the non-interactive policy, and usage errors.
	is("CONFIRMATION_REQUIRED", ErrConfirmationRequired).exit(ExitUsage),
	is("USAGE", ErrUsage).exit(ExitUsage).what("a usage error: bad flags or configuration"),
	is("AUDIT_DIR_UNSAFE", ErrAuditDir),

	// The configuration (pkg/config).
	is("CONFIG_ROOT_LOGIN_NOT_APPLIED", config.ErrRootLoginNotApplied).exit(ExitUsage),
	is("CONFIG_NOT_LOADED", config.ErrNotLoaded),
	is("CONFIG_FILE_INVALID", config.ErrInvalidFile), // next/s2-control
	// The edit of hardn.yml, the save's (config.Edit, next/s2-config-edit).
	is("CONFIG_NOT_EDITABLE", config.ErrNotEditable),
	is("CONFIG_EDIT_UNSAFE", config.ErrEditUnsafe),

	// Confirm-or-revert (pkg/host/revert): the typed refusals first.
	typed("RUN_LOCK_HELD", "another hardn holds the run lock: an apply, or hardn rollback --boot",
		as(func(e *revert.LockedError) bool { return e.Run })),
	typed("JOURNAL_LOCK_HELD", "another hardn has held the journal lock past its wait",
		as(func(e *revert.LockedError) bool { return !e.Run })),
	typed("ABANDON_OTHER_ROLLBACK_RUNNING", "nothing given up: another change's rollback runs now", noRollback(revert.NoRollbackOtherRunning)),
	typed("ABANDON_OTHER_ROLLBACK_UNFINISHED", "nothing given up: another change's rollback has not finished", noRollback(revert.NoRollbackOtherUnfinished)),
	typed("ABANDON_NOT_STARTED", "nothing given up: the change is journalled and its rollback has not started", noRollback(revert.NoRollbackNotStarted)),
	typed("ABANDON_OTHER_PENDING", "nothing given up: another change is journalled", noRollback(revert.NoRollbackOtherPending)),
	typed("ABANDON_GIVEN_UP", "nothing given up: the change's rollback was given up already", noRollback(revert.NoRollbackGivenUp)),
	typed("ABANDON_ROLLED_BACK", "nothing given up: the timer rolled the change back", noRollback(revert.NoRollbackRolledBack)),
	typed("ABANDON_NOT_JOURNALLED", "nothing given up: the change is not journalled", noRollback(revert.NoRollbackNotJournalled)),
	typed("CHANGE_ROLLBACK_GIVEN_UP", "nothing to confirm: the change was not confirmed in time, and its rollback did not finish and was given up",
		as(func(e *revert.NotPendingError) bool { return e.Abandoned != "" })),
	typed("CHANGE_ROLLED_BACK", "nothing to confirm: the change was not confirmed in time and was rolled back",
		as(func(e *revert.NotPendingError) bool { return e.RolledBack() })),
	is("CONFIRM_TIMEOUT_INVALID", revert.ErrTimeout).exit(ExitUsage).what("the confirm timeout is not accepted (confirmTimeout: a duration of at least 60 s)"),
	is("CHANGE_ID_INVALID", revert.ErrID).exit(ExitUsage),
	is("JOURNAL_INVALID", revert.ErrJournal).what("the change journal is not valid"),
	is("JOURNAL_UNSAFE", revert.ErrUnsafe).what("the change journal, the run directory, or a directory above them, can be changed by a user other than root"),
	is("CHANGE_PENDING", revert.ErrPending),
	is("NOTHING_PENDING", revert.ErrNothingPending),
	is("NOT_CONFIRMABLE", revert.ErrNotConfirmable),
	is("ROLLBACK_RUNNING", revert.ErrRollingBack),
	is("CONFIRM_SAME_SESSION", revert.ErrSameSession),
	is("CONFIRM_OLD_CONNECTION", revert.ErrOldConnection),
	is("CONFIRM_LOCAL_CONNECTION", revert.ErrLocalConn),
	is("SESSION_UNKNOWN", revert.ErrSessionUnknown),
	is("CONFIRM_WRONG_LISTENER", revert.ErrWrongListener),
	is("CHANGE_APPLIED_ALREADY", revert.ErrApplied),
	is("REVERT_NO_TIMER", revert.ErrNoTimer),
	is("HARDN_EXECUTABLE_UNSAFE", revert.ErrExecutable),
	is("JOURNAL_CLAIM_RACE", revert.ErrClaim),
	is("ROLLBACK_INCOMPLETE", revert.ErrIncomplete),
	is("PLAN_ROLLBACK_UNREGISTERED", revert.ErrNoPlanRollback),
	is("BOOT_UNIT_NOT_AS_PLANNED", revert.ErrBootUnit),
	is("LOCK_UNAVAILABLE", revert.ErrNoLock).what("a lock cannot be taken: its directory is not root's alone, the file is not a regular file, or the disk is full"),
	is("PLAN_INVALID", plans.ErrPlan),

	// sshd (pkg/host/sshd, pkg/host/sshd/migrate).
	is("SSHD_CONFIG_INVALID", sshd.ErrConfig),
	typed("SSHD_ENV_NO_HOSTKEYS", "sshd -t loaded no host key (as without root, or from the configuration's HostKey files)", envKind(sshd.EnvHostKeys)),
	typed("SSHD_ENV_NO_PRIVSEP_USER", "sshd's privilege separation user does not exist", envKind(sshd.EnvPrivsepUser)),
	is("SSHD_ENVIRONMENT", sshd.ErrEnvironment),
	is("SSHD_UNEXPECTED", sshd.ErrUnexpected),
	is("SSHD_INCLUDE_UNSUPPORTED", sshd.ErrUnsupportedInclude),
	is("SSHD_DROPIN_NOT_INCLUDED", sshd.ErrNotIncluded),
	is("SSHD_CONFIG_NOT_REGULAR", sshd.ErrNotRegular).what("an sshd configuration file is not a regular file"),
	is("SSH_MIGRATE_INCLUDE_OTHER", migrate.ErrIncludeOther),
	is("SSH_MIGRATE_NOT_ESTABLISHED", migrate.ErrNotEstablished),
	is("SSH_MIGRATE_FEWER_LOGIN_PATHS", migrate.ErrFewerLoginPaths),
	is("SSH_MIGRATE_LISTENER_OVERLAP", migrate.ErrListenerOverlap),
	is("SSH_MIGRATE_CHANGED", migrate.ErrChanged).what("a legacy SSH file changed since the migration was planned"),

	// The reload (pkg/host/reload).
	is("RELOAD_MODE_UNKNOWN", reload.ErrModeUnknown),
	is("RELOAD_NO_GENERATOR", reload.ErrNoGenerator),
	is("RELOAD_SOCKET_DROPIN", reload.ErrSocketDropIn),
	is("RELOAD_LISTENERS_UNKNOWN", reload.ErrListenersUnknown),
	is("RELOAD_DROPS_LISTENER", reload.ErrDropsListener),
	is("RELOAD_ADDS_LISTENER", reload.ErrAddsListener),
	is("RELOAD_FLAVOUR_SWITCH", reload.ErrFlavour),
	is("RELOAD_UNIT_STATE_UNKNOWN", reload.ErrUnitState),
	is("RELOAD_UNIT_DOWN", reload.ErrUnitDown),
	is("RELOAD_LISTENER_IN_USE", reload.ErrInUse),
	is("RELOAD_SOCKET_CONFIG", reload.ErrSocketConfig),
	is("RELOAD_NOT_BOUND", reload.ErrNotBound),
	is("RELOAD_RESTORED_LISTENERS", reload.ErrRestoredListeners),
	is("RELOAD_NOT_IN_EFFECT", reload.ErrNotInEffect),
	is("RELOAD_OPENRC_CONFD", reload.ErrConfd),

	// ufw (pkg/host/ufw).
	typed("UFW_ROLLBACK_FAILED", "an undo of ufw's rules failed; those after it did not run",
		as(func(*ufw.RollbackError) bool { return true })),
	is("UFW_NOT_APPLICABLE", ufw.ErrNotApplicable),
	is("UFW_PROXMOX_UNKNOWN", ufw.ErrProxmoxUnknown).what("ufw: whether the host runs Proxmox VE (no ufw there) is not established"),
	is("UFW_NOT_INSTALLED", ufw.ErrNotInstalled),
	is("UFW_LISTENERS_UNKNOWN", ufw.ErrListenersUnknown),
	is("UFW_STATE_UNKNOWN", ufw.ErrStateUnknown),
	is("UFW_WOULD_BLOCK", ufw.ErrBlocks),
	is("UFW_VERIFY_FAILED", ufw.ErrVerify),
	is("UFW_ENABLED_MISMATCH", ufw.ErrEnabledMismatch),
	is("UFW_INACTIVE", ufw.ErrInactive),
	is("UFW_BAD_PROFILE", ufw.ErrBadProfile),

	// Proxmox VE (pkg/host/pve).
	is("PVE_NOT_APPLICABLE", pve.ErrNotApplicable),
	is("PVE_UNKNOWN", pve.ErrProxmoxUnknown).what("whether the host runs Proxmox VE is not established, so its rules cannot be applied"),
	is("PVE_PORT_CHANGE_REFUSED", pve.ErrPortChange),
	is("PVE_ROOT_EXCLUDED", pve.ErrRootExcluded),
	is("PVE_ROOT_PASSWORD_KEPT", pve.ErrRootPassword),
	is("PVE_CLUSTER_FS_DOWN", pve.ErrClusterFSDown),
	is("PVE_KEYS_LINK", pve.ErrKeysLink),
	is("PVE_KEYS_CHANGED", pve.ErrKeysChanged),
	is("PVE_BAD_KEY", pve.ErrBadKey),
	is("PVE_CLUSTER_KEY", pve.ErrClusterKey),
	is("PVE_NO_BACKUP", pve.ErrNoBackup).what("Proxmox VE's keys edit needs a backup run"),
	is("PVE_PLAN_MISMATCH", pve.ErrPlanMismatch),
	is("PVE_FIREWALL_UNKNOWN", pve.ErrStateUnknown),

	// sudo, sudo-rs and doas (pkg/host/sudoers): a rejection by its stage.
	typed("SUDOERS_INVALID_BEFORE", "the installed elevation configuration does not check, before anything is written", sudoersRejected(sudoers.StageBefore)),
	typed("SUDOERS_INVALID_AFTER", "the elevation configuration does not check with the plan installed (put back)", sudoersRejected(sudoers.StageAfter)),
	typed("SUDOERS_INVALID_RESTORED", "the elevation configuration does not check after the old files were put back", sudoersRejected(sudoers.StageRestored)),
	is("SUDOERS_INVALID", sudoers.ErrRejected),
	typed("SUDOERS_CHECK_FAILED", "the elevation tool's check could not run",
		as(func(*sudoers.CheckError) bool { return true })),
	is("SUDOERS_NO_TOOL", sudoers.ErrNoTool),
	is("SUDOERS_TOOL_AMBIGUOUS", sudoers.ErrAmbiguous),
	is("SUDOERS_DOAS_UNSUPPORTED", sudoers.ErrUnsupported),
	is("SUDOERS_USER_NAME", sudoers.ErrUserName),
	is("SUDOERS_VAR_NAME", sudoers.ErrVarName),
	is("SUDOERS_NOT_INCLUDED", sudoers.ErrNotIncluded),
	is("SUDOERS_ENV_NEEDS_GRANT", sudoers.ErrEnvNeedsGrant),
	is("SUDOERS_PERMS", sudoers.ErrPerms).what("a doas file is not root's, or group or others can write it"),
	is("SUDOERS_CHANGED", sudoers.ErrChanged).what("an elevation file changed since it was read"),
	is("SUDOERS_NOT_RESTORED", sudoers.ErrNotRestored),
	// A file of hardn's that PolicyOf cannot read back as a Policy, named
	// (sudoers.EditedError; refactor plan slice 9, unit (b)).
	is("SUDOERS_FILE_EDITED", sudoers.ErrEdited),
	// The Environment control's refusal where no user ran hardn through the
	// tool, a root login (environment.ErrNoUser; refactor plan slice 9,
	// unit (b), decision 2 (a)).
	is("ENVIRONMENT_NO_USER", environment.ErrNoUser).what("no user ran hardn through sudo or doas (a root login), so there is no user to keep HARDN_CONFIG for"),

	// Alpine's updates (pkg/host/alpine).
	is("ALPINE_NOT_ALPINE", alpine.ErrNotAlpine),
	is("ALPINE_UNKNOWN", alpine.ErrUnknown).what("Alpine's updates: a fact the answer depends on is not established"),
	is("ALPINE_NO_OPENRC", alpine.ErrNoOpenRC),
	is("ALPINE_NO_CROND", alpine.ErrNoCrond),
	is("ALPINE_SCRIPT_MISMATCH", alpine.ErrScript),
	is("ALPINE_NO_BACKUP", alpine.ErrNoBackup),
	is("ALPINE_CHANGED", alpine.ErrChanged).what("Alpine's update files changed since they were read"),
	is("ALPINE_OTHER_PACKAGES", alpine.ErrOtherPackages),
	is("ALPINE_NOT_EFFECTIVE", alpine.ErrNotEffective),
	is("ALPINE_NOT_RESTORED", alpine.ErrNotRestored),

	// DNS (pkg/host/dns): the wrapping sentinels before ErrInvalid.
	is("DNS_UNKNOWN", dns.ErrUnknown).what("DNS: a fact the answer depends on is not established"),
	is("DNS_UNSUPPORTED", dns.ErrUnsupported),
	is("DNS_CONTAINER", dns.ErrContainer),
	is("DNS_MANAGED", dns.ErrManaged),
	is("DNS_RESOLVED_NOT_RUNNING", dns.ErrNotRunning),
	is("DNS_OVERRIDDEN", dns.ErrOverridden),
	is("DNS_DROPIN_BLOCKED", dns.ErrDropInBlocked),
	is("DNS_NO_NAMESERVERS", dns.ErrNoNameservers),
	is("DNS_OPTION_NOT_APPLICABLE", dns.ErrNotApplicable),
	is("DNS_INVALID", dns.ErrInvalid),
	is("DNS_NO_BACKUP", dns.ErrNoBackup),
	is("DNS_CHANGED", dns.ErrChanged).what("the resolver files changed since they were read"),
	is("DNS_NOT_EFFECTIVE", dns.ErrNotEffective),
	is("DNS_NOT_RESTORED", dns.ErrNotRestored),

	// Backups (pkg/host/backup). One code for a directory another user can
	// change, whichever unit checked it: sudoers.md §6 asks for one.
	is("UNSAFE_DIRECTORY", backup.ErrUnsafeDir, sshd.ErrUnsafeRunDir, sudoers.ErrUnsafeRunDir, host.ErrUntrustedDir).what("a directory hardn writes in (a run, backup or log directory) that another user can change, or reached through a symlink"),
	is("BACKUP_SETTINGS_INVALID", backup.ErrSettings),
	is("BACKUP_RUN_EXISTS", backup.ErrRunExists),
	is("BACKUP_NOT_REGULAR", backup.ErrNotRegular).what("a file to back up is neither a regular file nor a symlink"),
	is("BACKUP_MANIFEST_INVALID", backup.ErrManifest),
	is("BACKUP_CORRUPT", backup.ErrCorrupt),
	is("BACKUP_CHANGED", backup.ErrChanged).what("a file changed since its backup in a way restore does not undo"),
	// A backup write that failed (a read-only file system), whatever the
	// write's own error: before the seam's and the standard library's,
	// which it wraps (refactor plan slice 3, unit (b)).
	is("BACKUP_WRITE_FAILED", backup.ErrWrite).what("a backup could not be written (its directory, its copy or its manifest), so the change did not go ahead"),
	// A file on a backup path, which a DirError names: ENOTDIR, the other
	// of its two errors, UNSAFE_DIRECTORY's above (next/s3-control).
	typed("BACKUP_PATH_NOT_DIRECTORY", "a backup path, or a path above it, is a file, not a directory",
		as(func(e *backup.DirError) bool { return !errors.Is(e.Err, backup.ErrUnsafeDir) })),

	// Detection (pkg/host/detect).
	is("DETECT_SIGNALS_DISAGREE", detect.ErrConflict).what("detection: two signals for one fact disagree, so it is Unknown"),
	is("DETECT_UNRECOGNISED", detect.ErrUnrecognised).what("detection: a file or a command's output is not in a form hardn knows"),
	is("DETECT_NO_SIGNAL", detect.ErrNoSignal).what("detection: no signal established the fact"),
	// The account databases (pkg/host/accounts, next/p4-check-users).
	is("ACCOUNTS_MALFORMED", accounts.ErrMalformed).what("a line of /etc/passwd or /etc/group is not in a form hardn knows"),
	// next/p4-locked: /etc/tcb, read before /etc/shadow.
	is("ACCOUNTS_TCB", accounts.ErrTCB).what("/etc/tcb exists, whose files getspnam may read before /etc/shadow: an account's lock is not established"),

	// The seam (pkg/host): ErrSymlinkParent wraps ErrSymlink, and a
	// *revert.LockedError wraps ErrLocked, so both come first above.
	is("SYMLINK_IN_UNSAFE_DIRECTORY", host.ErrSymlinkParent),
	is("SYMLINK_REFUSED", host.ErrSymlink),
	is("HARD_LINK_REFUSED", host.ErrHardLink),
	is("WRITE_NOT_DURABLE", host.ErrNotDurable),
	is("LOCKED", host.ErrLocked),
	is("COMMAND_NOT_FOUND", host.ErrNotFound),
	is("UNKNOWN_USER", host.ErrUnknownUser),
	is("UNKNOWN_GROUP", host.ErrUnknownGroup),
	typed("COMMAND_FAILED", "a command hardn ran exited with a status other than 0",
		as(func(*host.ExitError) bool { return true })),

	// The standard library's.
	is("PERMISSION_DENIED", fs.ErrPermission).what("permission denied"),
	is("FILE_NOT_FOUND", fs.ErrNotExist).what("a file or directory does not exist"),
	is("TIMED_OUT", context.DeadlineExceeded).what("a step ran out of time"),

	// The engine's wrappers of a whole plan or apply, which hold the
	// specific error above: last.
	is("CHANGE_UNJOURNALLED", engine.ErrUnjournalled),
	is("ROLLED_BACK", engine.ErrRolledBack).what("the apply failed and was rolled back (an apply error with no error of its step)"),
	is("PLAN_REFUSED", engine.ErrRefused).what("the plan was refused (a refusal with no code of its own)"),
}

// finding is a finding's code.
func finding(code, what string) Entry { return Entry{Code: code, Kind: KindFinding, What: what} }

// findingCodes are the findings' codes: each primitive's stable kind code,
// upper case under its prefix, as pkg/engine/steps derives them
// (steps.md §2.23), and this package's own.
var findingCodes = []Entry{
	finding("SSHD_CLOBBERABLE", "no Include: hardn's block at the top of sshd_config, which cloud-init and Proxmox VE rewrite"),
	finding("SSHD_INCLUDE_NOT_FIRST", "sshd_config's first directive is not the stock Include"),
	finding("SSHD_MATCH_UNTARGETED", "a Match block that could not be evaluated for the connections it may match"),
	finding("SSHD_WARNING", "sshd -t passed, and complained about a line"),
	finding("SSHD_OVERRIDDEN", "a line of another file is read before hardn's and sets the same keyword"),
	finding("SSHD_SHADOWED", "a line of another file sets a keyword hardn sets, read after hardn's: no effect while hardn's is there"),
	finding("SSHD_ACCUMULATES", "another file adds values to a keyword whose values accumulate"),
	finding("SSHD_MATCH_OVERRIDE", "a Match block sets a keyword hardn sets, for the connections it matches"),
	finding("SSHD_SOURCES_DISAGREE", "two sources of an effective value disagree, so it is Unknown"),
	finding("SSHD_EFFECTIVE_UNKNOWN", "an effective value is Unknown"),
	finding("SSH_MIGRATE_LINE_DROPPED", "a legacy line the migration leaves out, with why"),
	finding("PVE_ROOT_KEY_ONLY", "PermitRootLogin set to prohibit-password: root key-only, as Proxmox VE clustering needs"),
	finding("PVE_ROOT_ALLOWED", "root added to AllowUsers, so that it stays reachable by key"),
	finding("PVE_PROXMOX_ROOT_LOGIN", "Proxmox VE's own PermitRootLogin yes in sshd_config, which hardn's drop-in shadows: not drift"),
	finding("PVE_PORT_NOT_22", "sshd is configured for a port other than 22, or not for 22"),
	finding("PVE_ROOT_UNREACHABLE", "a setting keeps, or may keep, root from logging in by key"),
	finding("PVE_CLUSTER_WIDE_KEYS", "the authorized_keys edit applies to every node of the cluster"),
	finding("PVE_RULE_MAY_BLOCK", "a pve-firewall rule may drop the port before its management rule accepts it"),
	finding("PVE_BLACKLIST", "pve-firewall's blacklist IP set has entries"),
	finding("PVE_FIREWALL_NOT_MODELLED", "proxmox-firewall (nftables) filters the host; its rules are not read"),
	finding("PVE_ROOT_NOT_KEY_ONLY", "PermitRootLogin is not prohibit-password, globally or in a Match block"),
	finding("UFW_SSH_NOT_ALLOWED", "ufw is active and blocks or denies an SSH listener"),
	finding("UFW_SSH_RESTRICTED", "ufw accepts an SSH listener for some sources, addresses or interfaces only"),
	finding("UFW_UNKNOWN", "ufw's verdict, or its status, could not be read"),
	finding("UFW_SSH_ALLOWED_EXCEPT", "ufw accepts an SSH listener from everywhere but some sources, addresses or interfaces"),
	finding("UFW_SSH_NOT_ALLOWED_AT_START", "ufw is inactive, and its saved rules would not let an SSH listener through once it starts"),
	finding("UFW_SSH_NEW_PORT_WIDER", "the plan lets a new port through from anywhere while a listener is restricted now (P-33)"),
	finding("SUDOERS_LATER_FILE", "a file the tool reads after hardn's, whose matching rule wins"),
	finding("SUDOERS_AFTER_INCLUDE", "the main sudoers file has lines after its includedir"),
	finding("SUDOERS_LEGACY_FILE", "origin/main's /etc/sudoers.d/<user>, reported, not changed"),
	finding("ALPINE_LEGACY_SCRIPT", "origin/main's own apk-upgrade script, which a plan removes once apk-cron is in"),
	finding("ALPINE_FOREIGN_SCRIPT", "something else at the legacy script's path, which hardn leaves alone"),
	finding("DNS_PER_LINK", "systemd-resolved queries per-link servers beside the global ones hardn sets"),
	finding("DNS_DHCP_CLIENT", "a DHCP client that may rewrite /etc/resolv.conf is installed"),
	finding("DNS_UDHCPC", "Alpine's udhcpc, the DHCP client in use, rewrites /etc/resolv.conf on each lease"),
	finding("DNS_PROXMOX_NODE", "Proxmox VE's node DNS settings rewrite /etc/resolv.conf"),
	finding("DNS_UDHCPC_UNKNOWN", "whether Alpine's udhcpc is the DHCP client is not established"),
	finding(CodeDeprecated, "a configuration value in a form that is going away; it still works (§3.17)"),
	finding(engine.CodeBestEffort, "a best-effort release: its vendor still supports it and hardn changes it, but hardn is not tested on it (P-41)"),
	// The SSH controls' own (next/p4-check-ssh).
	finding(ssh.CodeMatchAllowsMore, "a Match block lets in more than the global value does (root's login, a password), for the connections it matches"),
	finding(ssh.CodeNotInEffect, "a listener sshd's configuration names is not bound: the configuration is not in effect"),
	finding(ssh.CodeMatchValueUnknown, "a Match block sets a value hardn does not rank, so what it lets in is not established"),
	finding(ssh.Code22Bound, "a socket on port 22 is bound while no configured SSH listener is on 22: sshd not reloaded, or another program"),
	// The firewall control's own (next/p4-check-firewall).
	finding(firewall.CodeSSHBlocked, "an active firewall refuses new connections to an SSH listener: lockout risk, scored critical"),
	finding(firewall.CodeSSHUnchecked, "the SSH listeners are not established, so whether the firewall lets them through is not either"),
	finding(firewall.CodeLocalOnly, "on Proxmox VE no management IP set is written: pve-firewall's management rule accepts SSH and the web UI from the local network only"),
	// The users control's own (next/p4-check-users).
	finding(users.CodeNoPassword, "a user account (uid 1000 or more) with a login shell becomes root without a password (sudo's NOPASSWD, doas's nopass)"),
	finding(users.CodeRuleNotRead, "an elevation rule or sudo Defaults hardn does not read may decide whether a user account becomes root"),
	finding(users.CodeUID0, "an account other than root has uid 0, so it is root"),
	// The AppArmor control's own (next/p4-check-apparmor).
	finding(apparmor.CodeGlobalModeUnknown, "AppArmor's global mode is kill or unconfined, which hardn does not rank, so what the profiles enforce is not established"),
	finding(apparmor.CodeBootUnknown, "AppArmor's service is in a state that does not say whether it loads the profiles at the next boot"),
	// The Auto Updates control's own (next/p4-check-updates).
	finding(updates.CodeAptValueUnknown, "an APT::Periodic value in a form hardn does not rank, so whether apt's daily job runs is not established"),
	finding(updates.CodeScheduleUnknown, "whether anything starts the daily update job is not established: an apt timer in a state that does not say or that could not be read, a host systemd did not boot, or an Alpine OpenRC did not boot"),
	finding(updates.CodeProxmoxNotUpgraded, "Proxmox VE: no unattended-upgrades origin matches Proxmox VE's repository, so Proxmox VE's own packages are not upgraded"),
	finding(updates.CodeOriginsUnknown, "a distribution hardn does not list, built from Debian or Ubuntu: whether unattended-upgrades' origins match its own archives is not established"),
	// The header probe's own (next/p4-header): why a reboot is pending.
	finding(header.CodeRebootRequired, "/var/run/reboot-required exists: a package asks for a reboot"),
	finding(header.CodeNewerKernel, "a kernel newer than the one running is installed in /boot"),
	finding(header.CodeModulesMissing, "Alpine: the running kernel's own modules are not installed, while another kernel's are"),
	finding(header.CodeKernelReplaced, "a kernel Debian built runs, and dpkg has its release's package installed as another version than the build running"),
	// The risk level's own (next/p4-risk): its cross-check of the uptime.
	finding(risk.CodeUptimeNoUpdates, "the host has been up 30 days or more and does not upgrade its packages by itself (on Proxmox VE, not Proxmox VE's own)"),
	// The users control's, for locked accounts (next/p4-locked).
	finding(users.CodeLocked, "a user account's password is locked (!) and sshd does not use PAM, so sshd refuses it, keys included: the User row does not count it"),
	// The System Details form's own (slice 1, next/s1-readers).
	finding(CodeDetailsUnreadable, "a section of the System Details report, or a fact of it, was not read: source says what; the rest was read"),
}

// CodeDeprecated is the finding of a configuration value read in a
// deprecated form (config.Deprecation).
const CodeDeprecated = "CONFIG_DEPRECATED_VALUE"

// boot is a boot code.
func boot(code, what string) Entry { return Entry{Code: code, Kind: KindBoot, What: what} }

// bootActions and bootReasons are hardn rollback --boot's codes, by
// revert.BootAction and revert.BootReason: the stable values, so that no
// consumer reads BootResult.Why (revert-boot.md §6).
var (
	bootActions = map[revert.BootAction]Entry{
		revert.BootNothing:    boot("BOOT_NOTHING", "nothing to do"),
		revert.BootRearmed:    boot("BOOT_REARMED", "the revert timer runs again, for what remains of the window"),
		revert.BootRolledBack: boot("BOOT_ROLLED_BACK", "the change was rolled back at once"),
		revert.BootLeft:       boot("BOOT_LEFT", "a change or a rollback left to the person: exit 1"),
	}
	bootReasons = map[revert.BootReason]Entry{
		revert.BootReasonNoChange:           boot("BOOT_NO_CHANGE", "nothing is journalled"),
		revert.BootReasonNoTimer:            boot("BOOT_NO_TIMER", "a change with no timer, left to the next run's notice"),
		revert.BootReasonTimerRuns:          boot("BOOT_TIMER_RUNS", "its timer was started in this boot, and is there"),
		revert.BootReasonRollbackRuns:       boot("BOOT_ROLLBACK_RUNS", "a rollback of it runs"),
		revert.BootReasonConfirmedMeanwhile: boot("BOOT_CONFIRMED_MEANWHILE", "confirmed after the journal was read"),
		revert.BootReasonClaimedMeanwhile:   boot("BOOT_CLAIMED_MEANWHILE", "a confirmation or a rollback claimed it after the journal was read"),
		revert.BootReasonApplyRuns:          boot("BOOT_APPLY_RUNS", "another hardn's apply holds the run lock, the change not applied yet"),
		revert.BootReasonInWindow:           boot("BOOT_IN_WINDOW", "re-armed for what remains of the window"),
		revert.BootReasonClockUntrusted:     boot("BOOT_CLOCK_UNTRUSTED", "re-armed for the whole window: the clock reads before the change"),
		revert.BootReasonDeadlinePassed:     boot("BOOT_DEADLINE_PASSED", "rolled back: past its deadline"),
		revert.BootReasonApplyInterrupted:   boot("BOOT_APPLY_INTERRUPTED", "rolled back: its apply did not finish"),
		revert.BootReasonRearmFailed:        boot("BOOT_REARM_FAILED", "rolled back: its timer could not be re-armed"),
		revert.BootReasonJournalUnreadable:  boot("BOOT_JOURNAL_UNREADABLE", "left: the journal cannot be read"),
		revert.BootReasonRollbackUnfinished: boot("BOOT_ROLLBACK_UNFINISHED", "left: a rollback of it did not finish"),
		revert.BootReasonNoBinaryRecorded:   boot("BOOT_NO_BINARY_RECORDED", "left: the journal does not name the binary its timer runs"),
		revert.BootReasonOtherBinary:        boot("BOOT_OTHER_BINARY", "left: its timer runs another path"),
		revert.BootReasonOtherContent:       boot("BOOT_OTHER_CONTENT", "left: the binary at the path is not the one that wrote the journal"),
		revert.BootReasonBinaryUnreadable:   boot("BOOT_BINARY_UNREADABLE", "left: this hardn's binary cannot be read"),
		revert.BootReasonNoBootID:           boot("BOOT_NO_BOOT_ID", "left: no boot id recorded, or this boot's cannot be read"),
		revert.BootReasonTimerUnknown:       boot("BOOT_TIMER_UNKNOWN", "left: whether its timer of this boot is still there cannot be told"),
		revert.BootReasonRunLockHeld:        boot("BOOT_RUN_LOCK_HELD", "left: another hardn holds the run lock, the change applied"),
	}
)

// CodeOf returns err's code: the first entry of the registry that matches
// it, or CodeUnclassified; "" for nil. An *engine.ApplyError is its step's
// error (ApplyError.Err): the rollback's error, and a change it did not
// cover, are entries of their own in a document's errors[] (Build).
func CodeOf(err error) string {
	return entryOf(err).Code
}

// entryOf is CodeOf's entry; the zero Entry for nil.
func entryOf(err error) Entry {
	if err == nil {
		return Entry{}
	}
	var ae *engine.ApplyError
	if errors.As(err, &ae) && ae.Err != nil {
		err = ae.Err
	}
	for _, e := range errorCodes {
		if e.matches(err) {
			return e
		}
	}
	return Entry{Code: CodeUnclassified, Kind: KindError, Exit: ExitError, What: "an error hardn does not know by name; its message says what it is"}
}

// BootCodes returns the codes of what hardn rollback --boot did and why;
// "" for a value the registry does not know.
func BootCodes(a revert.BootAction, r revert.BootReason) (action, reason string) {
	return bootActions[a].Code, bootReasons[r].Code
}

// Codes returns every entry of the registry: the error codes in their
// precedence order, then the findings', then the boot codes by their
// values' order in pkg/host/revert, then CodeUnclassified.
func Codes() []Entry {
	out := append([]Entry(nil), errorCodes...)
	out = append(out, findingCodes...)
	for _, a := range []revert.BootAction{revert.BootNothing, revert.BootRearmed, revert.BootRolledBack, revert.BootLeft} {
		out = append(out, bootActions[a])
	}
	for _, r := range bootReasonOrder {
		out = append(out, bootReasons[r])
	}
	return append(out, entryOf(errUnclassified))
}

// bootReasonOrder is the order of revert.BootReason's constants.
var bootReasonOrder = []revert.BootReason{
	revert.BootReasonNoChange, revert.BootReasonNoTimer, revert.BootReasonTimerRuns, revert.BootReasonRollbackRuns,
	revert.BootReasonConfirmedMeanwhile, revert.BootReasonClaimedMeanwhile, revert.BootReasonApplyRuns,
	revert.BootReasonInWindow, revert.BootReasonClockUntrusted, revert.BootReasonDeadlinePassed,
	revert.BootReasonApplyInterrupted, revert.BootReasonRearmFailed, revert.BootReasonJournalUnreadable,
	revert.BootReasonRollbackUnfinished, revert.BootReasonNoBinaryRecorded, revert.BootReasonOtherBinary,
	revert.BootReasonOtherContent, revert.BootReasonBinaryUnreadable, revert.BootReasonNoBootID,
	revert.BootReasonTimerUnknown, revert.BootReasonRunLockHeld,
}

// errUnclassified stands for an error no entry matches.
var errUnclassified = errors.New("unclassified")

// byCode indexes Codes by code.
var byCode = func() map[string]Entry {
	m := map[string]Entry{}
	for _, e := range Codes() {
		m[e.Code] = e
	}
	return m
}()

// Lookup returns the entry of code.
func Lookup(code string) (Entry, bool) {
	e, ok := byCode[code]
	return e, ok
}
