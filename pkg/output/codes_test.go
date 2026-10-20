package output_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	hardncmd "github.com/abbott/hardn/pkg/cmd"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
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
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// sentinels are the exported Err* sentinels of pkg/host/... and
// pkg/engine/..., by package and name. TestSentinelsRegistered fails for one
// missing here: add it, with an entry in codes.go that matches it.
var sentinels = map[string]error{
	"alpine.ErrChanged":              alpine.ErrChanged,
	"alpine.ErrNoBackup":             alpine.ErrNoBackup,
	"alpine.ErrNoCrond":              alpine.ErrNoCrond,
	"alpine.ErrNoOpenRC":             alpine.ErrNoOpenRC,
	"alpine.ErrNotAlpine":            alpine.ErrNotAlpine,
	"alpine.ErrNotEffective":         alpine.ErrNotEffective,
	"alpine.ErrNotRestored":          alpine.ErrNotRestored,
	"alpine.ErrOtherPackages":        alpine.ErrOtherPackages,
	"alpine.ErrScript":               alpine.ErrScript,
	"alpine.ErrUnknown":              alpine.ErrUnknown,
	"backup.ErrChanged":              backup.ErrChanged,
	"backup.ErrCorrupt":              backup.ErrCorrupt,
	"backup.ErrManifest":             backup.ErrManifest,
	"backup.ErrNotRegular":           backup.ErrNotRegular,
	"backup.ErrRunExists":            backup.ErrRunExists,
	"backup.ErrSettings":             backup.ErrSettings,
	"backup.ErrUnsafeDir":            backup.ErrUnsafeDir,
	"detect.ErrConflict":             detect.ErrConflict,
	"detect.ErrNoSignal":             detect.ErrNoSignal,
	"detect.ErrUnrecognised":         detect.ErrUnrecognised,
	"dns.ErrChanged":                 dns.ErrChanged,
	"dns.ErrContainer":               dns.ErrContainer,
	"dns.ErrDropInBlocked":           dns.ErrDropInBlocked,
	"dns.ErrInvalid":                 dns.ErrInvalid,
	"dns.ErrManaged":                 dns.ErrManaged,
	"dns.ErrNoBackup":                dns.ErrNoBackup,
	"dns.ErrNoNameservers":           dns.ErrNoNameservers,
	"dns.ErrNotApplicable":           dns.ErrNotApplicable,
	"dns.ErrNotEffective":            dns.ErrNotEffective,
	"dns.ErrNotRestored":             dns.ErrNotRestored,
	"dns.ErrNotRunning":              dns.ErrNotRunning,
	"dns.ErrOverridden":              dns.ErrOverridden,
	"dns.ErrUnknown":                 dns.ErrUnknown,
	"dns.ErrUnsupported":             dns.ErrUnsupported,
	"engine.ErrBootUnit":             engine.ErrBootUnit,
	"engine.ErrChange":               engine.ErrChange,
	"engine.ErrConflict":             engine.ErrConflict,
	"engine.ErrOrder":                engine.ErrOrder,
	"engine.ErrPlanChanged":          engine.ErrPlanChanged,
	"engine.ErrPlanUsed":             engine.ErrPlanUsed,
	"engine.ErrRefused":              engine.ErrRefused,
	"engine.ErrRolledBack":           engine.ErrRolledBack,
	"engine.ErrUnjournalled":         engine.ErrUnjournalled,
	"engine.ErrUnsupported":          engine.ErrUnsupported,
	"engine.ErrWindow":               engine.ErrWindow,
	"host.ErrHardLink":               host.ErrHardLink,
	"host.ErrLocked":                 host.ErrLocked,
	"host.ErrNotDurable":             host.ErrNotDurable,
	"host.ErrNotFound":               host.ErrNotFound,
	"host.ErrSymlink":                host.ErrSymlink,
	"host.ErrSymlinkParent":          host.ErrSymlinkParent,
	"host.ErrUnknownGroup":           host.ErrUnknownGroup,
	"host.ErrUnknownUser":            host.ErrUnknownUser,
	"host.ErrUntrustedDir":           host.ErrUntrustedDir,
	"migrate.ErrChanged":             migrate.ErrChanged,
	"migrate.ErrFewerLoginPaths":     migrate.ErrFewerLoginPaths,
	"migrate.ErrIncludeOther":        migrate.ErrIncludeOther,
	"migrate.ErrListenerOverlap":     migrate.ErrListenerOverlap,
	"migrate.ErrNotEstablished":      migrate.ErrNotEstablished,
	"plans.ErrPlan":                  plans.ErrPlan,
	"pve.ErrBadKey":                  pve.ErrBadKey,
	"pve.ErrClusterFSDown":           pve.ErrClusterFSDown,
	"pve.ErrClusterKey":              pve.ErrClusterKey,
	"pve.ErrKeysChanged":             pve.ErrKeysChanged,
	"pve.ErrKeysLink":                pve.ErrKeysLink,
	"pve.ErrNoBackup":                pve.ErrNoBackup,
	"pve.ErrNotApplicable":           pve.ErrNotApplicable,
	"pve.ErrPlanMismatch":            pve.ErrPlanMismatch,
	"pve.ErrPortChange":              pve.ErrPortChange,
	"pve.ErrProxmoxUnknown":          pve.ErrProxmoxUnknown,
	"pve.ErrRootExcluded":            pve.ErrRootExcluded,
	"pve.ErrRootPassword":            pve.ErrRootPassword,
	"pve.ErrStateUnknown":            pve.ErrStateUnknown,
	"reload.ErrAddsListener":         reload.ErrAddsListener,
	"reload.ErrConfd":                reload.ErrConfd,
	"reload.ErrDropsListener":        reload.ErrDropsListener,
	"reload.ErrFlavour":              reload.ErrFlavour,
	"reload.ErrInUse":                reload.ErrInUse,
	"reload.ErrListenersUnknown":     reload.ErrListenersUnknown,
	"reload.ErrModeUnknown":          reload.ErrModeUnknown,
	"reload.ErrNoGenerator":          reload.ErrNoGenerator,
	"reload.ErrNotBound":             reload.ErrNotBound,
	"reload.ErrNotInEffect":          reload.ErrNotInEffect,
	"reload.ErrRestoredListeners":    reload.ErrRestoredListeners,
	"reload.ErrSocketConfig":         reload.ErrSocketConfig,
	"reload.ErrSocketDropIn":         reload.ErrSocketDropIn,
	"reload.ErrUnitDown":             reload.ErrUnitDown,
	"reload.ErrUnitState":            reload.ErrUnitState,
	"revert.ErrApplied":              revert.ErrApplied,
	"revert.ErrBootUnit":             revert.ErrBootUnit,
	"revert.ErrClaim":                revert.ErrClaim,
	"revert.ErrExecutable":           revert.ErrExecutable,
	"revert.ErrID":                   revert.ErrID,
	"revert.ErrIncomplete":           revert.ErrIncomplete,
	"revert.ErrJournal":              revert.ErrJournal,
	"revert.ErrLocalConn":            revert.ErrLocalConn,
	"revert.ErrNoLock":               revert.ErrNoLock,
	"revert.ErrNoPlanRollback":       revert.ErrNoPlanRollback,
	"revert.ErrNoTimer":              revert.ErrNoTimer,
	"revert.ErrNotConfirmable":       revert.ErrNotConfirmable,
	"revert.ErrNothingPending":       revert.ErrNothingPending,
	"revert.ErrOldConnection":        revert.ErrOldConnection,
	"revert.ErrPending":              revert.ErrPending,
	"revert.ErrRollingBack":          revert.ErrRollingBack,
	"revert.ErrSameSession":          revert.ErrSameSession,
	"revert.ErrSessionUnknown":       revert.ErrSessionUnknown,
	"revert.ErrTimeout":              revert.ErrTimeout,
	"revert.ErrUnsafe":               revert.ErrUnsafe,
	"revert.ErrWrongListener":        revert.ErrWrongListener,
	"sshd.ErrConfig":                 sshd.ErrConfig,
	"sshd.ErrEnvironment":            sshd.ErrEnvironment,
	"sshd.ErrNotIncluded":            sshd.ErrNotIncluded,
	"sshd.ErrNotRegular":             sshd.ErrNotRegular,
	"sshd.ErrUnexpected":             sshd.ErrUnexpected,
	"sshd.ErrUnsafeRunDir":           sshd.ErrUnsafeRunDir,
	"sshd.ErrUnsupportedInclude":     sshd.ErrUnsupportedInclude,
	"steps.ErrChanged":               steps.ErrChanged,
	"steps.ErrListenConfNotInEffect": steps.ErrListenConfNotInEffect,
	"steps.ErrMigrateFirst":          steps.ErrMigrateFirst,
	"steps.ErrNoNewListener":         steps.ErrNoNewListener,
	"steps.ErrNoRunDir":              steps.ErrNoRunDir,
	"steps.ErrNoWriteStrategy":       steps.ErrNoWriteStrategy,
	"steps.ErrNotInEffect":           steps.ErrNotInEffect,
	"steps.ErrProxmoxRoot":           steps.ErrProxmoxRoot,
	"sudoers.ErrAmbiguous":           sudoers.ErrAmbiguous,
	"sudoers.ErrChanged":             sudoers.ErrChanged,
	"sudoers.ErrEnvNeedsGrant":       sudoers.ErrEnvNeedsGrant,
	"sudoers.ErrNoTool":              sudoers.ErrNoTool,
	"sudoers.ErrNotIncluded":         sudoers.ErrNotIncluded,
	"sudoers.ErrNotRestored":         sudoers.ErrNotRestored,
	"sudoers.ErrPerms":               sudoers.ErrPerms,
	"sudoers.ErrRejected":            sudoers.ErrRejected,
	"sudoers.ErrUnsafeRunDir":        sudoers.ErrUnsafeRunDir,
	"sudoers.ErrUnsupported":         sudoers.ErrUnsupported,
	"sudoers.ErrUserName":            sudoers.ErrUserName,
	"sudoers.ErrVarName":             sudoers.ErrVarName,
	"ufw.ErrBlocks":                  ufw.ErrBlocks,
	"ufw.ErrEnabledMismatch":         ufw.ErrEnabledMismatch,
	"ufw.ErrListenersUnknown":        ufw.ErrListenersUnknown,
	"ufw.ErrNotApplicable":           ufw.ErrNotApplicable,
	"ufw.ErrNotInstalled":            ufw.ErrNotInstalled,
	"ufw.ErrProxmoxUnknown":          ufw.ErrProxmoxUnknown,
	"ufw.ErrStateUnknown":            ufw.ErrStateUnknown,
	"ufw.ErrVerify":                  ufw.ErrVerify,
}

// otherSentinels are the sentinels of other packages that have codes of
// their own.
var otherSentinels = map[string]error{
	"config.ErrRootLoginNotApplied":  config.ErrRootLoginNotApplied,
	"config.ErrNotLoaded":            config.ErrNotLoaded,
	"output.ErrUsage":                output.ErrUsage,
	"output.ErrConfirmationRequired": output.ErrConfirmationRequired,
	"output.ErrAuditDir":             output.ErrAuditDir,
	"prompt.ErrAborted":              prompt.ErrAborted,
	"context.Canceled":               context.Canceled,
	"context.DeadlineExceeded":       context.DeadlineExceeded,
	"fs.ErrPermission":               fs.ErrPermission,
	"fs.ErrNotExist":                 fs.ErrNotExist,
}

// exempt are exported sentinels that are not hardn's errors: a test
// double's.
var exempt = map[string]string{
	"hostfake.ErrUnscripted": "the recording fake's: a test that forgot to script a command, never a run's",
}

// scanned are the exported Err* package-level variables of the non-test Go
// files under dirs, by package name and name, with the string literal of
// an errors.New initializer ("" for any other).
func scanned(t *testing.T, dirs ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			for _, decl := range f.Decls {
				g, ok := decl.(*ast.GenDecl)
				if !ok || g.Tok != token.VAR {
					continue
				}
				for _, spec := range g.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, n := range vs.Names {
						if !n.IsExported() || !strings.HasPrefix(n.Name, "Err") {
							continue
						}
						lit := ""
						if i < len(vs.Values) {
							lit = newLiteral(vs.Values[i])
						}
						out[f.Name.Name+"."+n.Name] = lit
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// newLiteral is the string literal of errors.New("..."), or "".
func newLiteral(e ast.Expr) string {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" {
		return ""
	}
	if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "errors" {
		return ""
	}
	bl, ok := call.Args[0].(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(bl.Value)
	if err != nil {
		return ""
	}
	return s
}

// TestSentinelsRegistered scans pkg/host/... and pkg/engine/... for
// exported Err* sentinels: each must be in sentinels (or exempt, with a
// reason), the value there must be the one its name says (its errors.New
// text), and a registry entry must match it: none is CodeUnclassified.
func TestSentinelsRegistered(t *testing.T) {
	found := scanned(t, filepath.Join("..", "host"), filepath.Join("..", "engine"))
	if len(found) < 100 {
		t.Fatalf("scanned %d sentinels: the scan is broken", len(found))
	}
	for name, lit := range found {
		if _, ok := exempt[name]; ok {
			continue
		}
		err, ok := sentinels[name]
		switch {
		case !ok:
			t.Errorf("%s is not registered: add it to sentinels, and an entry that matches it to codes.go", name)
		case lit != "" && err.Error() != lit:
			t.Errorf("sentinels[%q] is %q, but its errors.New says %q", name, err, lit)
		}
	}
	for name := range sentinels {
		if _, ok := found[name]; !ok {
			t.Errorf("sentinels names %s, which the scan did not find: removed or renamed?", name)
		}
	}
	for name, err := range all() {
		if code := output.CodeOf(err); code == output.CodeUnclassified || code == "" {
			t.Errorf("%s: no entry matches it", name)
		}
		if code := output.CodeOf(fmt.Errorf("wrapped: %w", err)); code != output.CodeOf(err) {
			t.Errorf("%s wrapped: %s, unwrapped %s", name, code, output.CodeOf(err))
		}
	}
}

func all() map[string]error {
	m := map[string]error{}
	for k, v := range sentinels {
		m[k] = v
	}
	for k, v := range otherSentinels {
		m[k] = v
	}
	return m
}

// TestSentinelCodes pins each sentinel's code: a code never changes or
// disappears (testdata/sentinels.golden).
func TestSentinelCodes(t *testing.T) {
	var lines []string
	for name, err := range all() {
		lines = append(lines, name+" "+output.CodeOf(err))
	}
	sort.Strings(lines)
	golden(t, "sentinels.golden", []byte(strings.Join(lines, "\n")+"\n"))
}

// TestCodes pins the registry: every code, its kind and exit status, in
// precedence order (testdata/codes.golden). A code is never removed or
// changed once released; new ones are added, and the golden with them.
// Codes are unique, upper case with underscores.
func TestCodes(t *testing.T) {
	var lines []string
	seen := map[string]bool{}
	for _, e := range output.Codes() {
		if seen[e.Code] {
			t.Errorf("%s twice", e.Code)
		}
		seen[e.Code] = true
		if e.Code == "" || strings.Trim(e.Code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" || strings.HasPrefix(e.Code, "_") || strings.HasSuffix(e.Code, "_") {
			t.Errorf("%q is not SCREAMING_SNAKE", e.Code)
		}
		if e.What == "" || strings.Contains(e.What, "\n") {
			t.Errorf("%s: what %q", e.Code, e.What)
		}
		if got, ok := output.Lookup(e.Code); !ok || got.Code != e.Code {
			t.Errorf("Lookup(%s) = %v, %v", e.Code, got, ok)
		}
		lines = append(lines, fmt.Sprintf("%s %s %d", e.Code, e.Kind, e.Exit))
	}
	golden(t, "codes.golden", []byte(strings.Join(lines, "\n")+"\n"))
}

// TestReferenceListsEveryCode: docs/reference/output.md names every code.
func TestReferenceListsEveryCode(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "reference", "output.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range output.Codes() {
		if !strings.Contains(string(doc), "`"+e.Code+"`") {
			t.Errorf("docs/reference/output.md does not name `%s`", e.Code)
		}
	}
}

// TestPrecedence: an error that matches two entries gets the more
// specific one's code (steps.md §7).
func TestPrecedence(t *testing.T) {
	privsep := &sshd.EnvError{Kind: sshd.EnvPrivsepDir, Path: "/run/sshd", Err: errors.New("exit status 255")}
	refusalD9 := &steps.Refusal{Code: steps.CodeListenConfNotInEffect, Why: "why", Fix: "fix", Err: fmt.Errorf("x: %w", reload.ErrRestoredListeners)}
	refusalRunDir := &steps.Refusal{Code: steps.CodeNoRunDir, Why: "why", Fix: "fix", Err: privsep}
	window := fmt.Errorf("%w (stopped): %w", engine.ErrWindow, context.DeadlineExceeded)
	planErr := (&engine.Plan{Refused: []error{fmt.Errorf("engine: %w", engine.ErrUnsupported), refusalD9}}).Err()
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"the D9 refusal, not the primitive's it wraps", refusalD9, "SSH_LISTEN_CONF_NOT_IN_EFFECT"},
		{"reload's refusal alone", fmt.Errorf("x: %w", reload.ErrRestoredListeners), "RELOAD_RESTORED_LISTENERS"},
		{"/run/sshd through steps", refusalRunDir, "SSHD_ENV_NO_RUNDIR"},
		{"/run/sshd from sshd.Validate", privsep, "SSHD_ENV_NO_RUNDIR"},
		{"no host keys", &sshd.EnvError{Kind: sshd.EnvHostKeys, HostKeysSet: true}, "SSHD_ENV_NO_HOSTKEYS"},
		{"no privsep user", &sshd.EnvError{Kind: sshd.EnvPrivsepUser, User: "sshd"}, "SSHD_ENV_NO_PRIVSEP_USER"},
		{"another environment error", &sshd.EnvError{Err: errors.New("x")}, "SSHD_ENVIRONMENT"},
		{"a config sshd rejects", &sshd.ConfigError{Err: errors.New("exit status 255")}, "SSHD_CONFIG_INVALID"},
		{"a symlink in an untrusted directory, not a plain symlink", fmt.Errorf("x: %w", host.ErrSymlinkParent), "SYMLINK_IN_UNSAFE_DIRECTORY"},
		{"the run lock", &revert.LockedError{Run: true, Err: host.ErrLocked}, "RUN_LOCK_HELD"},
		{"the journal lock", &revert.LockedError{Err: host.ErrLocked}, "JOURNAL_LOCK_HELD"},
		{"another lock", fmt.Errorf("x: %w", host.ErrLocked), "LOCKED"},
		{"a change journalled", &revert.PendingError{}, "CHANGE_PENDING"},
		{"no change to confirm", &revert.NotPendingError{}, "NOTHING_PENDING"},
		{"confirm after the timer's rollback", &revert.NotPendingError{Reverted: &revert.Outcome{ID: "x", Reason: revert.ReasonTimeout}}, "CHANGE_ROLLED_BACK"},
		{"confirm after a rollback given up", &revert.NotPendingError{Reverted: &revert.Outcome{ID: "x"}, Abandoned: "/var/lib/hardn/abandoned-x.json"}, "CHANGE_ROLLBACK_GIVEN_UP"},
		{"dns: no nameservers, not just invalid", fmt.Errorf("x: %w", dns.ErrNoNameservers), "DNS_NO_NAMESERVERS"},
		{"the window, not a timeout", window, "CONFIRM_WINDOW_CLOSING"},
		{"Ctrl-C inside a step's error", fmt.Errorf("ufw: %w: %w", ufw.ErrVerify, context.Canceled), "CANCELLED"},
		{"a prompt aborted", prompt.ErrInterrupted, "CANCELLED"},
		{"the boot unit's install, not revert's cause", fmt.Errorf("%w: %w", engine.ErrBootUnit, revert.ErrBootUnit), "BOOT_UNIT_NOT_INSTALLED"},
		{"a plan's refusals joined: the first entry any matches (Build lists each)", planErr, "SSH_LISTEN_CONF_NOT_IN_EFFECT"},
		{"an apply error is its step's", &engine.ApplyError{Err: fmt.Errorf("x: %w", ufw.ErrVerify), RollbackErr: revert.ErrIncomplete}, "UFW_VERIFY_FAILED"},
		{"an apply error with no step error", &engine.ApplyError{}, "ROLLED_BACK"},
		{"a command failed", &host.ExitError{Argv: []string{"ufw", "status"}, ExitCode: 1}, "COMMAND_FAILED"},
		{"sudoers rejected before", &sudoers.CheckError{Stage: sudoers.StageBefore, Err: &host.ExitError{ExitCode: 1}}, "SUDOERS_INVALID_BEFORE"},
		{"sudoers rejected: the candidate", &sudoers.CheckError{Stage: sudoers.StageCandidate, Err: &host.ExitError{ExitCode: 1}}, "SUDOERS_INVALID"},
		{"sudoers rejected after", &sudoers.CheckError{Stage: sudoers.StageAfter, Err: sudoers.ErrPerms}, "SUDOERS_INVALID_AFTER"},
		{"sudoers rejected restored", &sudoers.CheckError{Stage: sudoers.StageRestored, Err: &host.ExitError{ExitCode: 1}}, "SUDOERS_INVALID_RESTORED"},
		{"sudoers check could not run", &sudoers.CheckError{Stage: sudoers.StageAfter, Err: host.ErrNotFound}, "SUDOERS_CHECK_FAILED"},
		{"a ufw undo failed", &ufw.RollbackError{Argv: []string{"ufw", "delete", "allow", "2230/tcp"}, Err: &host.ExitError{ExitCode: 1}}, "UFW_ROLLBACK_FAILED"},
		{"ufw blocks", &ufw.BlockError{}, "UFW_WOULD_BLOCK"},
		{"fewer login paths", &migrate.NarrowError{}, "SSH_MIGRATE_FEWER_LOGIN_PATHS"},
		{"a dropped listener", &reload.HoldError{}, "RELOAD_DROPS_LISTENER"},
		{"not bound", &reload.ListenerError{Err: reload.ErrNotBound}, "RELOAD_NOT_BOUND"},
		{"one code for an unsafe run directory", sudoers.ErrUnsafeRunDir, "UNSAFE_DIRECTORY"},
		{"nil", nil, ""},
		{"unknown", errors.New("something else"), output.CodeUnclassified},
	} {
		if got := output.CodeOf(tc.err); got != tc.want {
			t.Errorf("%s: CodeOf(%v) = %s, want %s", tc.name, tc.err, got, tc.want)
		}
	}
}

// TestAbandonCodes: each of Abandon's refusals by its NoRollbackKind
// (revert.md §8.5), not by its sentinel, whose words are about confirming.
func TestAbandonCodes(t *testing.T) {
	for kind, want := range map[revert.NoRollbackKind]string{
		revert.NoRollbackOtherRunning: "ABANDON_OTHER_ROLLBACK_RUNNING", revert.NoRollbackOtherUnfinished: "ABANDON_OTHER_ROLLBACK_UNFINISHED",
		revert.NoRollbackNotStarted: "ABANDON_NOT_STARTED", revert.NoRollbackOtherPending: "ABANDON_OTHER_PENDING",
		revert.NoRollbackGivenUp: "ABANDON_GIVEN_UP", revert.NoRollbackRolledBack: "ABANDON_ROLLED_BACK",
		revert.NoRollbackNotJournalled: "ABANDON_NOT_JOURNALLED",
	} {
		if got := output.CodeOf(&revert.NoRollbackError{ID: "x", Kind: kind}); got != want {
			t.Errorf("%s: %s, want %s", kind, got, want)
		}
	}
	if got := output.CodeOf(&revert.NoRollbackError{Kind: "a kind not known"}); got != "NOTHING_PENDING" {
		t.Errorf("an unknown kind: %s, want its sentinel's", got)
	}
}

// TestTypedErrors scans pkg/host/... and pkg/engine/... for exported
// error types (a name ending in Error, and steps.Refusal): each has a
// sample in typedSamples that the registry gives a code.
func TestTypedErrors(t *testing.T) {
	samples := map[string]error{
		"host.ExitError":         &host.ExitError{ExitCode: 1},
		"revert.NoRollbackError": &revert.NoRollbackError{Kind: revert.NoRollbackNotStarted},
		"revert.PendingError":    &revert.PendingError{},
		"revert.LockedError":     &revert.LockedError{Run: true, Err: host.ErrLocked},
		"revert.NotPendingError": &revert.NotPendingError{},
		"sshd.ConfigError":       &sshd.ConfigError{},
		"sshd.EnvError":          &sshd.EnvError{Kind: sshd.EnvHostKeys},
		"sshd.UnexpectedError":   &sshd.UnexpectedError{ExitCode: 3},
		"migrate.NarrowError":    &migrate.NarrowError{},
		"sudoers.CheckError":     &sudoers.CheckError{Stage: sudoers.StageCandidate, Err: sudoers.ErrPerms},
		"reload.HoldError":       &reload.HoldError{},
		"reload.ListenerError":   &reload.ListenerError{Err: reload.ErrSocketConfig},
		"ufw.BlockError":         &ufw.BlockError{},
		"ufw.RollbackError":      &ufw.RollbackError{Err: errors.New("x")},
		"engine.ApplyError":      &engine.ApplyError{Err: engine.ErrWindow},
		"steps.Refusal":          &steps.Refusal{Code: steps.CodeNoRunDir},
	}
	found := map[string]bool{}
	for _, dir := range []string{filepath.Join("..", "host"), filepath.Join("..", "engine")} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			for _, decl := range f.Decls {
				g, ok := decl.(*ast.GenDecl)
				if !ok || g.Tok != token.TYPE {
					continue
				}
				for _, spec := range g.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if ts.Name.IsExported() && (strings.HasSuffix(ts.Name.Name, "Error") || f.Name.Name+"."+ts.Name.Name == "steps.Refusal") {
						found[f.Name.Name+"."+ts.Name.Name] = true
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for name := range found {
		err, ok := samples[name]
		switch {
		case !ok:
			t.Errorf("error type %s has no sample here, nor perhaps an entry in codes.go", name)
		case output.CodeOf(err) == output.CodeUnclassified:
			t.Errorf("%s: no entry matches %v", name, err)
		}
	}
	for name := range samples {
		if !found[name] {
			t.Errorf("samples names %s, which the scan did not find", name)
		}
	}
}

// TestFindingCodes: every finding the primitives' kinds give, under the
// prefix pkg/engine/steps puts it (steps.md §2.23: upper case, the prefix
// said once), is registered as a finding.
func TestFindingCodes(t *testing.T) {
	derive := func(prefix, kind string) string {
		k := strings.ToUpper(strings.ReplaceAll(kind, "-", "_"))
		if strings.HasPrefix(k, prefix+"_") {
			return k
		}
		return prefix + "_" + k
	}
	kinds := func(s func(int) string) []string {
		var out []string
		for k := 1; k < 64; k++ {
			w := s(k)
			if w == "unknown" || strings.HasPrefix(w, "FindingKind(") {
				break
			}
			out = append(out, w)
		}
		return out
	}
	want := []string{steps.CodeMigrateDropped, output.CodeDeprecated}
	for prefix, words := range map[string][]string{
		"SSHD":    kinds(func(k int) string { return sshd.FindingKind(k).String() }),
		"PVE":     kinds(func(k int) string { return pve.FindingKind(k).String() }),
		"UFW":     kinds(func(k int) string { return ufw.FindingKind(k).String() }),
		"SUDOERS": kinds(func(k int) string { return sudoers.FindingKind(k).String() }),
		"ALPINE":  kinds(func(k int) string { return alpine.FindingKind(k).String() }),
		"DNS":     kinds(func(k int) string { return dns.FindingKind(k).String() }),
	} {
		if len(words) < 2 {
			t.Errorf("%s: %d kinds: the enumeration is broken", prefix, len(words))
		}
		for _, w := range words {
			want = append(want, derive(prefix, w))
		}
	}
	n := 0
	for _, e := range output.Codes() {
		if e.Kind == output.KindFinding {
			n++
		}
	}
	for _, code := range want {
		if e, ok := output.Lookup(code); !ok || e.Kind != output.KindFinding {
			t.Errorf("finding %s: not registered as a finding (%v)", code, e)
		}
	}
	if n != len(want) {
		t.Errorf("%d finding codes registered, %d derived: a registered one no kind gives", n, len(want))
	}
}

// TestBootCodes: every action and reason of hardn rollback --boot has a
// code (revert-boot.md §6), read from boot.go's constants.
func TestBootCodes(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "host", "revert", "boot.go"))
	if err != nil {
		t.Fatal(err)
	}
	actions, reasons := 0, 0
	sc := bufio.NewScanner(strings.NewReader(string(src)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[2] != "=" {
			continue
		}
		v, err := strconv.Unquote(f[3])
		if err != nil {
			continue
		}
		switch f[1] {
		case "BootAction":
			actions++
			if a, _ := output.BootCodes(revert.BootAction(v), ""); a == "" {
				t.Errorf("action %s has no code", v)
			}
		case "BootReason":
			reasons++
			if _, r := output.BootCodes("", revert.BootReason(v)); r == "" {
				t.Errorf("reason %s has no code", v)
			}
		}
	}
	if actions != 4 || reasons < 20 {
		t.Errorf("read %d actions and %d reasons from boot.go: the scan is broken", actions, reasons)
	}
}

// TestExitStatuses: the exit statuses are the brief's, and pkg/cmd's and
// pkg/ui/prompt's equal them.
func TestExitStatuses(t *testing.T) {
	if output.ExitOK != 0 || output.ExitError != 1 || output.ExitUsage != 2 || output.ExitChanges != 3 || output.ExitInterrupted != 130 {
		t.Fatal("the exit statuses changed")
	}
	if hardncmd.ExitUsage != output.ExitUsage || prompt.ExitInterrupted != output.ExitInterrupted {
		t.Errorf("pkg/cmd ExitUsage %d, pkg/ui/prompt ExitInterrupted %d", hardncmd.ExitUsage, prompt.ExitInterrupted)
	}
	if steps.CodeNoRunDir != "SSHD_ENV_NO_RUNDIR" || steps.CodeListenConfNotInEffect != "SSH_LISTEN_CONF_NOT_IN_EFFECT" {
		t.Error("steps' codes changed")
	}
}
