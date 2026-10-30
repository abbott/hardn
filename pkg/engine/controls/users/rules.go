package users

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// rule is one line of an elevation tool's configuration that may decide
// who becomes root, in the order the tool reads them.
type rule struct {
	who    string // a user, or a group: "%name" (sudo), ":name" (doas); "" when not read
	nopass bool   // NOPASSWD (sudo), nopass (doas)
	deny   bool   // doas's deny
	known  bool   // false: a line in a form hardn does not read, which may decide for who
	// A known rule's runas: as any user (sudo's "(ALL)" or "(ALL:ALL)",
	// doas with no "as root") or root alone, and with any group (sudo's
	// ":ALL"; doas has no group to choose) or the target user's own
	// (covers).
	anyUser, anyGroup bool
	at                string // file:line
	text              string // the line, its white space collapsed
}

// tool is one elevation tool as the host has it.
type tool struct {
	name  string     // "sudo" (sudo or sudo-rs) or "doas"
	state detect.Tri // Yes: installed, its rules read; No: it makes no one root (why); Unknown: err says why
	rules []rule
	// auth are sudo's lines that may change whose password it asks, or
	// whether it asks, wherever they are: such a Defaults, or a file
	// included other than the drop-ins.
	auth   []rule
	why    string // with No: "not installed", or its path not setuid root
	err    error
	detail string // the row's detail when err decides it
}

// readTools reads sudo's rules, then doas's, for the tools detect found.
func readTools(ctx context.Context, sys host.System, f detect.Facts) []tool {
	return []tool{
		readTool(ctx, sys, f, sudoers.ToolSudo, f.Elevation.Sudo),
		readTool(ctx, sys, f, sudoers.ToolDoas, f.Elevation.Doas),
	}
}

// Elevation is the host's elevation rules as Check reads them, for another
// reader of the same question, the System Details report (pkg/engine/details;
// slice 1): the smallest Check exposes, so that the row and the report cannot
// read the rules differently. It says nothing of shells, locks or findings,
// which are the row's. The zero Elevation has no tool.
type Elevation struct{ tools []tool }

// ReadElevation reads sudo's rules, then doas's, for the tools detect found,
// as Check does: each tool's own check of its files runs first (visudo -c,
// doas -C). It changes nothing.
func ReadElevation(ctx context.Context, sys host.System, f detect.Facts) Elevation {
	return Elevation{tools: readTools(ctx, sys, f)}
}

// NotRead is a tool whose rules are not established, and why: its rules
// unreadable to a user other than root, a configuration its tool rejects,
// whether it is installed not established.
type NotRead struct {
	Tool string // "sudo" (sudo or sudo-rs) or "doas"
	Err  error
}

// NotRead are the tools whose rules are not established, in the order read.
func (e Elevation) NotRead() []NotRead {
	var out []NotRead
	for _, t := range e.tools {
		if t.state == detect.Unknown {
			out = append(out, NotRead{Tool: t.name, Err: t.err})
		}
	}
	return out
}

// Grant is what one tool's rules decide for one account: Yes, the rule that
// decides lets it run every command as root; No, the tool makes no one root
// or no rule that decides does; Unknown, a rule hardn does not read may decide,
// or the tool's rules are not read (Elevation.NotRead). Passwords are not said.
type Grant struct {
	Tool   string // "sudo" (sudo or sudo-rs) or "doas"
	Grants detect.Tri
}

// GrantsOf is what each tool decides for u, by the rules Check decides by, in
// the order read: sudo, then doas.
func (e Elevation) GrantsOf(db accounts.DB, u accounts.User) []Grant {
	var out []Grant
	for _, t := range e.tools {
		out = append(out, Grant{Tool: t.name, Grants: t.decide(db, u).tri})
	}
	return out
}

// readTool reads one tool's rules: where the tool reads them is
// sudoers.Read's (sudo-rs's /etc/sudoers-rs, the drop-ins it skips), and
// doas only on Alpine, whose doas reads /etc/doas.d (sudoers.Choose). The
// tool must be setuid root, as sudo and sudo-rs refuse to run otherwise,
// and its files must pass its own check first (sudoers.CheckInstalled): a
// file it cannot parse may stop it applying any rule [I], which the lines
// hardn reads would not show.
func readTool(ctx context.Context, sys host.System, f detect.Facts, want sudoers.Tool, present detect.Tool) tool {
	t := tool{name: want.String(), detail: "can't read", why: "not installed"}
	switch present.Present {
	case detect.No:
		t.state = detect.No
		return t
	case detect.Unknown:
		t.err, t.detail = present.Err, "can't tell if "+t.name+" installed"
		if t.err == nil {
			t.err = detect.ErrNoSignal
		}
		return t
	}
	fi, err := sys.Stat(ctx, present.Path)
	if err != nil {
		t.err = fmt.Errorf("users: %w", err)
		return t
	}
	switch uid, _, ok := host.Owner(fi); {
	case !ok:
		t.err = fmt.Errorf("users: owner of %s: %w", present.Path, detect.ErrNoSignal)
		return t
	case uid != 0 || fi.Mode()&fs.ModeSetuid == 0:
		t.state, t.why = detect.No, present.Path+" not setuid root"
		return t
	}
	c := sudoers.Choose(f, want)
	if c.Tool != want {
		t.err, t.detail = c.Err, "can't tell who has "+t.name
		return t
	}
	st, err := sudoers.Read(ctx, sys, c, nil)
	if err == nil {
		err = sudoers.CheckInstalled(ctx, sys, st)
	}
	switch {
	case errors.Is(err, sudoers.ErrRejected):
		t.err, t.detail = err, t.name+" config invalid"
		return t
	case err == nil && want == sudoers.ToolSudo:
		t.rules, t.auth, err = readSudo(ctx, sys, st)
	case err == nil:
		t.rules, err = readDoas(ctx, sys, st)
	}
	if err != nil {
		t.err = err
		return t
	}
	t.state = detect.Yes
	return t
}

// readSudo is sudo's rules in its order, and its auth lines: the main
// file, with the drop-ins read where it includes /etc/sudoers.d.
func readSudo(ctx context.Context, sys host.System, st sudoers.State) (rs, auth []rule, err error) {
	main, err := sys.ReadFile(ctx, st.Main)
	if err != nil {
		return nil, nil, fmt.Errorf("users: %w", err)
	}
	keep := func(l line) {
		r, isRule, isAuth := sudoLine(l)
		if isRule {
			rs = append(rs, r)
		}
		if isAuth {
			auth = append(auth, r)
		}
	}
	for _, l := range lines(main, st.Main) {
		f := strings.Fields(l.text)
		if l.joined || len(f) != 2 || f[0] != "@includedir" && f[0] != "#includedir" || strings.TrimSuffix(f[1], "/") != sudoers.DropInDir {
			keep(l)
			continue
		}
		for _, e := range st.Entries {
			p := sudoers.DropInDir + "/" + e
			data, err := sys.ReadFile(ctx, p)
			if err != nil {
				return nil, nil, fmt.Errorf("users: %w", err)
			}
			for _, l := range lines(data, p) {
				keep(l)
			}
		}
	}
	return rs, auth, nil
}

// readDoas is doas's rules in its order: /etc/doas.conf, then the files
// of /etc/doas.d it reads.
func readDoas(ctx context.Context, sys host.System, st sudoers.State) ([]rule, error) {
	files := []string{sudoers.DoasConfig}
	for _, e := range st.Entries {
		files = append(files, sudoers.DoasDir+"/"+e)
	}
	var rs []rule
	for _, p := range files {
		data, err := sys.ReadFile(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("users: %w", err)
		}
		for _, l := range lines(data, p) {
			if r, ok := doasRule(l); ok {
				rs = append(rs, r)
			}
		}
	}
	return rs, nil
}

// line is one line of a file, trimmed, where it starts; a line ending in
// '\' is continued, and the lines it joins are one, never of a known form.
type line struct {
	text, at string
	joined   bool
}

func lines(data []byte, file string) []line {
	var out []line
	cont := false
	for i, l := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(l)
		if cont {
			out[len(out)-1].text += " " + t
			out[len(out)-1].joined = true
		} else {
			out = append(out, line{text: t, at: steps.SourceAt(file, i+1)})
		}
		cont = strings.HasSuffix(t, `\`)
	}
	return out
}

const name = `[a-z_][a-z0-9_.-]*\$?`

var (
	// sudoGrant is the one form of a sudo rule read: a user or %group,
	// on every host, as ALL or root, with ":ALL" or not (the runas is
	// kept: covers), every command, with or without NOPASSWD. Debian's
	// and Ubuntu's root and %sudo lines, origin/main's and hardn's grants
	// and cloud-init's are in it.
	sudoGrant = regexp.MustCompile(`^(%?` + name + `)\s+ALL\s*=\s*\(\s*(ALL|root)\s*(:\s*ALL\s*)?\)\s*(NOPASSWD\s*:\s*)?ALL$`)
	// sudoAlias: alias definitions, which grant no one anything (a rule
	// that uses an alias is not read).
	sudoAlias    = regexp.MustCompile(`^(User_Alias|Runas_Alias|Host_Alias|Cmnd_Alias|Cmd_Alias)\b`)
	sudoDefaults = regexp.MustCompile(`^Defaults\b`)
	// sudoWho is a rule's or a Defaults' one user or %group.
	sudoWho      = regexp.MustCompile(`^%?` + name + `$`)
	doasIdentity = regexp.MustCompile(`^:?` + name + `$`)
)

// sudoLine reads one line of sudo's. It is a rule (in sudoGrant's form,
// known; any other is kept as not known, its user read where it is one
// name or %group), an auth line (a Defaults that may change how sudo asks
// a password, its scope read as a rule's user), both (a file included
// other than the drop-ins; a continued line with a comment in it), or
// neither: blank, a comment, any other Defaults, an alias.
func sudoLine(l line) (r rule, isRule, isAuth bool) {
	r = rule{at: l.at, text: strings.Join(strings.Fields(l.text), " ")}
	t := l.text
	if i := commentAt(t, true); i >= 0 && !strings.HasPrefix(t, "#include") {
		if l.joined {
			return r, true, true // whether sudo continues a comment is not read
		}
		t = strings.TrimSpace(t[:i])
	}
	f := strings.Fields(t)
	switch {
	case len(f) == 0, sudoAlias.MatchString(t):
		return r, false, false
	case sudoDefaults.MatchString(t):
		if s, ok := strings.CutPrefix(f[0], "Defaults:"); ok && sudoWho.MatchString(s) {
			r.who = s
		}
		return r, false, l.joined || defaultsAuth(strings.TrimSpace(t[len(f[0]):]))
	case f[0] == "@include" || f[0] == "#include" || f[0] == "@includedir" || f[0] == "#includedir":
		return r, true, true
	}
	if m := sudoGrant.FindStringSubmatch(t); m != nil && !l.joined {
		r.who, r.anyUser, r.anyGroup, r.nopass, r.known = m[1], m[2] == "ALL", m[3] != "", m[4] != "", true
	} else if sudoWho.MatchString(f[0]) && !strings.HasPrefix(strings.TrimSpace(t[len(f[0]):]), ",") {
		r.who = f[0]
	}
	return r, true, false
}

// commentAt is where t's comment starts, or -1: the first '#' that starts
// a word with no quote or backslash before it (a line with one there is
// kept whole, not read); for sudo (uid), not before a digit, a user ID
// (sudoers(5); sudoers.md §3). doas.conf(5): a comment runs from '#' to
// the end of the line.
func commentAt(t string, uid bool) int {
	for i := 0; i < len(t); i++ {
		switch {
		case t[i] == '"' || t[i] == '\\':
			return -1
		case t[i] != '#' || i > 0 && t[i-1] != ' ' && t[i-1] != '\t':
		case uid && i+1 < len(t) && t[i+1] >= '0' && t[i+1] <= '9':
		default:
			return i
		}
	}
	return -1
}

// defaultsAuth is whether a Defaults line's parameters may change whose
// password sudo asks, or whether it asks: rootpw, targetpw or runaspw set,
// or authenticate turned off (sudoers(5)). A parameter hardn does not read
// counts, and so does a flag after more than one '!', whose reading is
// not established.
func defaultsAuth(params string) bool {
	for _, p := range splitParams(params) {
		p = strings.TrimSpace(p)
		neg := 0
		for strings.HasPrefix(p, "!") {
			neg, p = neg+1, strings.TrimSpace(p[1:])
		}
		i := strings.IndexFunc(p, func(c rune) bool { return (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' })
		key, op := p, ""
		if i >= 0 {
			key, op = p[:i], strings.TrimSpace(p[i:])
		}
		switch {
		case key == "", op != "" && !strings.HasPrefix(op, "=") && !strings.HasPrefix(op, "+=") && !strings.HasPrefix(op, "-="):
			return true
		case key == "authenticate" && (op != "" || neg > 0):
			return true
		case (key == "rootpw" || key == "targetpw" || key == "runaspw") && (op != "" || neg != 1):
			return true
		}
	}
	return false
}

// splitParams splits a Defaults' parameters at the commas outside quotes.
func splitParams(s string) []string {
	var out []string
	quoted, escaped, start := false, false, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case escaped:
			escaped = false
		case c == '\\':
			escaped = true
		case c == '"':
			quoted = !quoted
		case c == ',' && !quoted:
			out, start = append(out, s[start:i]), i+1
		}
	}
	return append(out, s[start:])
}

// doasRule reads one line of doas's: permit or deny, options (nopass,
// persist, nolog, keepenv, setenv { … }), a user or :group, and nothing
// after it but "as root" (doas.conf(5)); any other rule is kept as not
// known, its user or group read where the options are. A comment
// (commentAt) is passed over, and a line that starts with one is all
// comment, whatever it holds; a line with a quote is not read otherwise.
func doasRule(l line) (rule, bool) {
	t := l.text
	if i := commentAt(t, false); i == 0 && !l.joined || i > 0 {
		t = strings.TrimSpace(t[:i])
	}
	if t == "" && !l.joined {
		return rule{}, false
	}
	r := rule{at: l.at, text: strings.Join(strings.Fields(t), " ")}
	f := strings.Fields(t)
	if l.joined || strings.Contains(t, `"`) || len(f) < 2 || f[0] != "permit" && f[0] != "deny" {
		return r, true
	}
	i := 1
opts:
	for ; i < len(f); i++ {
		switch f[i] {
		case "nopass":
			r.nopass = true
		case "persist", "nolog", "keepenv":
		case "setenv":
			end := slices.Index(f[i:], "}")
			if end < 2 || f[i+1] != "{" {
				return r, true
			}
			i += end
		default:
			break opts
		}
	}
	rest := f[i:]
	if len(rest) == 0 || !doasIdentity.MatchString(rest[0]) || slices.Contains(doasKeywords, rest[0]) {
		return r, true
	}
	r.who, r.deny = rest[0], f[0] == "deny"
	r.known = len(rest) == 1 || slices.Equal(rest[1:], []string{"as", "root"})
	r.anyUser, r.anyGroup = len(rest) == 1, true // no "as": every target (doas.conf(5))
	return r, true
}

// doasKeywords are doas.conf(5)'s words after the options, never a user.
var doasKeywords = []string{"as", "cmd", "args"}

// grant is what one tool's rules decide for one user. The last rule that
// matches decides (sudoers(5), doas.conf(5)); a rule not read after it
// that may match too leaves the answer Unknown, by that rule. A rule
// before it still decides the requests it matches that no later rule for
// the user does (covers): with Yes, such a rule read without a password
// makes the grant nopass, and one not read leaves it open.
type grant struct {
	tri      detect.Tri
	nopass   bool
	nopassAt string // the rule without a password: by, or one before it
	openAt   string // with Yes: the last rule before by, not read, that may still decide
	by       rule   // the rule that decided, or the last not read; zero for no rule
	err      error  // the tool's, when it decides Unknown
}

func (t tool) decide(db accounts.DB, u accounts.User) grant {
	switch t.state {
	case detect.No:
		return grant{tri: detect.No}
	case detect.Unknown:
		return grant{err: t.err}
	}
	g, at := grant{tri: detect.No}, -1
	ms := make([]detect.Tri, len(t.rules))
	for i, r := range t.rules {
		switch ms[i] = r.names(db, u); {
		case ms[i] == detect.No:
		case ms[i] == detect.Unknown || !r.known:
			g = grant{by: r}
		case r.deny:
			g = grant{tri: detect.No, by: r}
		default:
			g, at = grant{tri: detect.Yes, nopass: r.nopass, nopassAt: r.at, by: r}, i
		}
	}
	if g.tri != detect.Yes {
		return g
	}
	for _, r := range t.auth {
		if r.names(db, u) != detect.No {
			return grant{by: r}
		}
	}
	// The rules before the deciding one that may be for u: each decides
	// the requests no later rule for u that covers it matches.
	for i, e := range t.rules[:at] {
		covered := false
		for j := i + 1; j <= at && !covered; j++ {
			covered = ms[j] == detect.Yes && t.rules[j].known && t.rules[j].covers(e)
		}
		switch {
		case ms[i] == detect.No, covered, e.known && (e.deny || !e.nopass):
		case e.known && ms[i] == detect.Yes:
			if !g.nopass {
				g.nopass, g.nopassAt = true, e.at
			}
		default:
			g.openAt = e.at
		}
	}
	return g
}

// covers is whether l matches every request r matches, so that r, before
// it, decides none: host and command are ALL in every rule read, so the
// runas alone. sudo's "(ALL)" or "(root)" lets -g name only the target
// user's own groups (sudoers(5), Runas_Spec): an earlier "(ALL:ALL)"
// rule decides "sudo -u root -g bob". A rule not read may match any.
func (l rule) covers(r rule) bool {
	if !r.known {
		return l.anyUser && l.anyGroup
	}
	return (l.anyUser || !r.anyUser) && (l.anyGroup || !r.anyGroup)
}

// names is whether r is for u, by its user or group. Unknown: r's user
// is not read; a name u may have under another case (sudo [I]) or through
// another account of its uid (doas compares uids, sudo matches the name
// of the uid's first account [I]); or, in a line hardn does not read, a
// group /etc/group lacks (NSS may have it) or has under another case.
func (r rule) names(db accounts.DB, u accounts.User) detect.Tri {
	switch {
	case r.who == "":
		return detect.Unknown
	case r.who[0] == '%' || r.who[0] == ':':
		g, found := r.who[1:], false
		if db.Member(u, g) {
			return detect.Yes
		}
		for _, x := range db.Groups {
			if !r.known && strings.EqualFold(x.Name, g) && db.Member(u, x.Name) {
				return detect.Unknown
			}
			found = found || x.Name == g
		}
		if !found && !r.known {
			return detect.Unknown
		}
		return detect.No
	}
	same, spelt := 0, false
	for _, a := range db.Users {
		if a.UID == u.UID {
			same++
			spelt = spelt || strings.EqualFold(a.Name, r.who)
		}
	}
	switch {
	case r.who == u.Name && same == 1:
		return detect.Yes
	case spelt:
		return detect.Unknown
	}
	return detect.No
}
