package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
)

var (
	// ErrFewerLoginPaths: a state the migration leads to has fewer working
	// login paths than the installed configuration (*NarrowError).
	ErrFewerLoginPaths = errors.New("fewer working login paths than the installed configuration")
	// ErrListenerOverlap: the migrated configuration has two listeners one
	// host cannot bind together that the installed one does not have.
	ErrListenerOverlap = errors.New("listeners that cannot be bound together")
	// ErrChanged: a file the plan changes is not as the plan read it.
	ErrChanged = errors.New("changed since the plan was made")
)

// Narrowing is one way a configuration accepts fewer logins than another.
type Narrowing struct {
	Keyword       string // sshd -T's keyword; listenaddress for a listener
	Before, After string
}

func (n Narrowing) String() string {
	return fmt.Sprintf("%s: %q before, %q after", n.Keyword, n.Before, n.After)
}

// NarrowError is a state Apply refused, and why.
type NarrowError struct {
	State      string // "the migrated configuration", or the file a step changes
	Narrowings []Narrowing
}

func (e *NarrowError) Error() string {
	ns := make([]string, len(e.Narrowings))
	for i, n := range e.Narrowings {
		ns[i] = n.String()
	}
	return fmt.Sprintf("migrate: %s: %s: %s", e.State, ErrFewerLoginPaths, strings.Join(ns, "; "))
}

// Is makes errors.Is(err, ErrFewerLoginPaths) hold.
func (e *NarrowError) Is(target error) bool { return target == ErrFewerLoginPaths }

// Options says how Apply checks and records the migration.
type Options struct {
	// RunDir, Platform and Binary are sshd.Validate's (Request).
	RunDir   string
	Platform detect.Platform
	Binary   string
	// Backup is the run's backup store; every file a step writes or removes
	// is backed up first, RollbackRequired.
	Backup *backup.Run
}

// Report is what Apply did.
type Report struct {
	Done     []Op            // the steps carried out, in order
	Backups  []backup.Result // one per file, before any step ran
	After    sshd.Effective  // the migrated configuration, by sshd -T over the copy
	Findings []sshd.Finding  // sshd.Validate's, about the migrated configuration
	// Kept is Before's listeners and the plan's Hold, all of which After
	// keeps: reload.Change's Before for the reload that follows (docs/safety/sshd-migrate.md §6).
	Kept []sshd.Listener
}

// Apply carries out p. Before writing anything it validates the state the
// whole plan leads to and compares its login paths with p.Before and
// p.Hold; then it backs up every file the steps change, and checks that
// each is still as the plan read it; then, for each step, it validates the
// state that step leaves and compares again, and only then writes or
// removes. It stops at the first error; the steps in Report.Done have been
// carried out, and backup.Restore of Report.Backups' entries undoes them
// (under context.WithoutCancel after Ctrl-C). It reloads nothing: the
// running sshd keeps its configuration until the caller's reload. Errors:
// ctx's, fs.ErrInvalid (no Backup), ErrNotEstablished, *NarrowError
// (ErrFewerLoginPaths), ErrListenerOverlap, ErrChanged, sshd.Validate's
// (wrapped), backup's and the host's.
func Apply(ctx context.Context, sys host.System, p Plan, opts Options) (Report, error) {
	var rep Report
	if err := ctx.Err(); err != nil {
		return rep, fmt.Errorf("migrate: %w", err)
	}
	if p.Empty() {
		return rep, nil
	}
	if opts.Backup == nil {
		return rep, fmt.Errorf("migrate: no backup run: %w", fs.ErrInvalid)
	}
	if err := established(p.Before); err != nil {
		return rep, fmt.Errorf("migrate: the installed configuration: %w", err)
	}
	var all []sshd.Change
	for _, op := range p.Ops {
		all = append(all, op.changes()...)
	}
	after, found, err := check(ctx, sys, all, p, opts, "the migrated configuration")
	if err != nil {
		return rep, err
	}
	was := overlaps(p.Before.Listeners.SSHD)
	if ov := slices.DeleteFunc(overlaps(after.Listeners.SSHD), func(o string) bool { return slices.Contains(was, o) }); len(ov) > 0 {
		return rep, fmt.Errorf("migrate: the migrated configuration listens on %s: %w", strings.Join(ov, "; "), ErrListenerOverlap)
	}
	rep.After, rep.Findings = after, found
	for _, l := range slices.Concat(p.Before.Listeners.SSHD, p.Hold) {
		if !slices.Contains(rep.Kept, l) {
			rep.Kept = append(rep.Kept, l)
		}
	}
	for _, op := range p.Ops {
		for _, name := range op.paths() {
			r, err := opts.Backup.Backup(ctx, name, backup.RollbackRequired)
			if err != nil {
				return rep, fmt.Errorf("migrate: %w", err)
			}
			rep.Backups = append(rep.Backups, r)
		}
	}
	for _, r := range rep.Backups {
		if err := p.unchanged(r.Entry); err != nil {
			return rep, err
		}
	}
	for _, op := range p.Ops {
		// The write comes first and the removals after, so each state
		// configures what the last one does.
		if ch := op.changes(); len(ch) > 0 {
			if _, _, err := check(ctx, sys, ch, p, opts, op.Path); err != nil {
				return rep, err
			}
		}
		if err := op.do(ctx, sys); err != nil {
			return rep, fmt.Errorf("migrate: %s %s: %w", op.Kind, op.Path, err)
		}
		rep.Done = append(rep.Done, op)
	}
	return rep, nil
}

// changes are the step's files, as sshd.Validate overlays them.
func (op Op) changes() []sshd.Change {
	switch op.Kind {
	case OpWriteDropIn:
		return []sshd.Change{{Path: sshd.HardnDropIn, Data: op.Data}}
	case OpRemove:
		return []sshd.Change{{Path: op.Path, Remove: true}}
	case OpRestoreMain:
		return []sshd.Change{{Path: sshd.HardnDropIn, Data: op.DropIn}, {Path: sshd.MainConfig, Data: op.Data}}
	}
	return nil
}

// paths are the files the step writes or removes, in that order.
func (op Op) paths() []string {
	switch op.Kind {
	case OpWriteDropIn:
		return []string{sshd.HardnDropIn}
	case OpRestoreMain:
		return []string{sshd.HardnDropIn, sshd.MainConfig}
	}
	return []string{op.Path}
}

// do carries out the step: atomic writes that keep an existing file's mode
// and owner (a new 00-hardn.conf is root's, 0644, as the stock drop-ins
// are), and guarded removals.
func (op Op) do(ctx context.Context, sys host.System) error {
	switch op.Kind {
	case OpWriteDropIn:
		return host.WriteFileAtomicKeep(ctx, sys, sshd.HardnDropIn, op.Data, 0o644, 0, 0)
	case OpRestoreMain:
		if err := host.WriteFileAtomicKeep(ctx, sys, sshd.HardnDropIn, op.DropIn, 0o644, 0, 0); err != nil {
			return err
		}
		return host.WriteFileAtomicKeep(ctx, sys, sshd.MainConfig, op.Data, 0o644, 0, 0)
	case OpRemove, OpRemoveSocketDropIn:
		return sys.Remove(ctx, op.Path)
	}
	return fs.ErrInvalid
}

// unchanged checks that a backup found the file as the plan read it.
func (p Plan) unchanged(e backup.Entry) error {
	i := slices.IndexFunc(p.Sources, func(s Source) bool { return s.Path == e.Path })
	if i < 0 {
		return fmt.Errorf("migrate: %s: not one of the plan's files: %w", e.Path, fs.ErrInvalid)
	}
	s := p.Sources[i]
	sum := sha256.Sum256(s.Data)
	switch {
	case s.Absent && e.Kind == backup.KindAbsent:
		return nil
	case !s.Absent && e.Kind == backup.KindFile && e.File != nil && e.File.SHA256 == hex.EncodeToString(sum[:]):
		return nil
	}
	return fmt.Errorf("migrate: %s (%s now): %w", e.Path, e.Kind, ErrChanged)
}

// check validates the host's configuration with changes applied and
// compares its login paths with p.Before; p.Hold are listeners it must
// keep besides Before's.
func check(ctx context.Context, sys host.System, changes []sshd.Change, p Plan, opts Options, state string) (sshd.Effective, []sshd.Finding, error) {
	rep, err := sshd.Validate(ctx, sys, sshd.Request{Changes: changes, RunDir: opts.RunDir, Platform: opts.Platform, Binary: opts.Binary})
	if err != nil {
		return sshd.Effective{}, nil, fmt.Errorf("migrate: validating %s: %w", state, err)
	}
	after, _ := sshd.Evaluate(sshd.EffectiveInputs{Effective: rep.Effective})
	if err := established(after); err != nil {
		return sshd.Effective{}, nil, fmt.Errorf("migrate: %s: %w", state, err)
	}
	if ns := narrowings(p.Before, after, p.Hold); len(ns) > 0 {
		return after, rep.Findings, &NarrowError{State: state, Narrowings: ns}
	}
	return after, rep.Findings, nil
}

// established requires what narrowings compares: the four typed settings,
// the user and group sets, AuthorizedKeysFile and AuthenticationMethods, and
// every listenaddress line read as a listener.
func established(e sshd.Effective) error {
	if e.Keywords == nil {
		return fmt.Errorf("sshd -T's output was not read: %w", ErrNotEstablished)
	}
	var missing []string
	for k, s := range map[string]sshd.Setting{"permitrootlogin": e.PermitRootLogin, "passwordauthentication": e.PasswordAuthentication,
		"kbdinteractiveauthentication": e.KbdInteractiveAuthentication, "pubkeyauthentication": e.PubkeyAuthentication} {
		if !s.Known {
			missing = append(missing, k)
		}
	}
	for k, s := range map[string]sshd.Set{"allowusers": e.AllowUsers, "allowgroups": e.AllowGroups, "denyusers": e.DenyUsers, "denygroups": e.DenyGroups} {
		if !s.Known {
			missing = append(missing, k)
		}
	}
	for _, k := range []string{"authorizedkeysfile", "authenticationmethods"} {
		if len(e.Keywords[k]) != 1 {
			missing = append(missing, k)
		}
	}
	if n := len(e.Keywords["listenaddress"]); n == 0 || len(e.Listeners.SSHD) != n {
		missing = append(missing, "listenaddress")
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return fmt.Errorf("sshd -T gives no value hardn reads for %s: %w", strings.Join(missing, ", "), ErrNotEstablished)
	}
	return nil
}

// fixed are the keywords whose values a migration must leave as they are:
// a client whose cipher, key type or pinned host key a new list drops, or
// whose key a new source of authorized keys or of revocations rules out,
// may no longer log in. sshd -T prints each (one line, or one per value).
var fixed = []string{"ciphers", "macs", "kexalgorithms", "hostkeyalgorithms", "pubkeyacceptedalgorithms", "casignaturealgorithms",
	"hostkey", "authorizedkeyscommand", "authorizedkeyscommanduser", "authorizedprincipalsfile", "authorizedprincipalscommand",
	"authorizedprincipalscommanduser", "trustedusercakeys", "revokedkeys", "pubkeyauthoptions", "maxauthtries", "forcecommand", "chrootdirectory"}

// methods are the authentication switches, besides the typed ones, a
// migration must not turn off.
var methods = []string{"permitemptypasswords", "usepam", "hostbasedauthentication", "gssapiauthentication", "kerberosauthentication"}

// rootRank orders PermitRootLogin's values by what they let root do.
var rootRank = map[string]int{"no": 0, "forced-commands-only": 1, "prohibit-password": 2, "yes": 3}

// narrowings lists how after accepts fewer logins than before: an
// authentication method or PAM turned off, a lower PermitRootLogin, a user
// or group no longer allowed or newly denied, a key file no longer read, a
// stricter AuthenticationMethods, a fixed keyword changed, or a listener
// of before (or of hold, a socket's: [::] is both families) no longer
// configured.
func narrowings(b, a sshd.Effective, hold []sshd.Listener) []Narrowing {
	var ns []Narrowing
	add := func(k, bv, av string) { ns = append(ns, Narrowing{k, bv, av}) }
	one := func(e sshd.Effective, k string) string {
		if vs := e.Keywords[k]; len(vs) > 0 {
			return strings.ToLower(vs[0])
		}
		return ""
	}
	for _, s := range []struct {
		k    string
		b, a sshd.Setting
	}{
		{"pubkeyauthentication", b.PubkeyAuthentication, a.PubkeyAuthentication},
		{"passwordauthentication", b.PasswordAuthentication, a.PasswordAuthentication},
		{"kbdinteractiveauthentication", b.KbdInteractiveAuthentication, a.KbdInteractiveAuthentication},
	} {
		if s.b.Value == "yes" && s.a.Value != "yes" {
			add(s.k, s.b.Value, s.a.Value)
		}
	}
	for _, k := range methods {
		if one(b, k) == "yes" && one(a, k) != "yes" {
			add(k, one(b, k), one(a, k))
		}
	}
	if rootRank[a.PermitRootLogin.Value] < rootRank[b.PermitRootLogin.Value] {
		add("permitrootlogin", b.PermitRootLogin.Value, a.PermitRootLogin.Value)
	}
	for _, s := range []struct {
		k    string
		b, a sshd.Set
	}{{"allowusers", b.AllowUsers, a.AllowUsers}, {"allowgroups", b.AllowGroups, a.AllowGroups}} {
		if len(s.a.Values) > 0 && (len(s.b.Values) == 0 || !subset(s.b.Values, s.a.Values)) {
			add(s.k, strings.Join(s.b.Values, " "), strings.Join(s.a.Values, " "))
		}
	}
	for _, s := range []struct {
		k    string
		b, a sshd.Set
	}{{"denyusers", b.DenyUsers, a.DenyUsers}, {"denygroups", b.DenyGroups, a.DenyGroups}} {
		if !subset(s.a.Values, s.b.Values) {
			add(s.k, strings.Join(s.b.Values, " "), strings.Join(s.a.Values, " "))
		}
	}
	bk, ak := strings.Fields(b.Keywords["authorizedkeysfile"][0]), strings.Fields(a.Keywords["authorizedkeysfile"][0])
	if !slices.Equal(bk, []string{"none"}) && !subset(bk, ak) {
		add("authorizedkeysfile", strings.Join(bk, " "), strings.Join(ak, " "))
	}
	if bm, am := one(b, "authenticationmethods"), one(a, "authenticationmethods"); am != "any" && am != bm {
		add("authenticationmethods", bm, am)
	}
	for _, k := range fixed {
		if bv, av := strings.Join(b.Keywords[k], " "), strings.Join(a.Keywords[k], " "); bv != av {
			add(k, bv, av)
		}
	}
	for _, l := range b.Listeners.SSHD {
		if !covered(a.Listeners.SSHD, l) {
			add("listenaddress", l.String(), "not covered")
		}
	}
	for _, l := range hold {
		if slices.ContainsFunc(families(l), func(f sshd.Listener) bool { return !covered(a.Listeners.SSHD, f) }) {
			add("listenaddress", l.String()+" (a socket's)", "not covered")
		}
	}
	return ns
}

// subset reports whether every value of a is in b.
func subset(a, b []string) bool {
	return !slices.ContainsFunc(a, func(v string) bool { return !slices.Contains(b, v) })
}
