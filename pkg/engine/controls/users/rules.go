package users

import (
	"context"
	"errors"
	"fmt"
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
	who    string // a user, or a group: "%name" (sudo), ":name" (doas)
	nopass bool   // NOPASSWD (sudo), nopass (doas)
	deny   bool   // doas's deny
	known  bool   // false: a line in a form hardn does not read, which may match anyone
	at     string // file:line
	text   string // the line, its white space collapsed
}

// tool is one elevation tool as the host has it.
type tool struct {
	name   string     // "sudo" (sudo or sudo-rs) or "doas"
	state  detect.Tri // Yes: installed, its rules read; No: not installed; Unknown: err says why
	rules  []rule
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

// readTool reads one tool's rules: where the tool reads them is
// sudoers.Read's (sudo-rs's /etc/sudoers-rs, the drop-ins it skips), and
// doas only on Alpine, whose doas reads /etc/doas.d (sudoers.Choose). The
// files must pass the tool's own check first (sudoers.CheckInstalled): a
// file it cannot parse may stop it applying any rule [I], which the lines
// hardn reads would not show.
func readTool(ctx context.Context, sys host.System, f detect.Facts, want sudoers.Tool, present detect.Tool) tool {
	t := tool{name: want.String(), detail: "can't read"}
	switch present.Present {
	case detect.No:
		t.state = detect.No
		return t
	case detect.Unknown:
		cause := present.Err
		if cause == nil {
			cause = detect.ErrNoSignal
		}
		t.err, t.detail = fmt.Errorf("users: %s: %w", t.name, cause), "can't tell if "+t.name+" installed"
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
		t.rules, err = readSudo(ctx, sys, st)
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

// readSudo is sudo's rules in its order: the main file, with the drop-ins
// read where it includes /etc/sudoers.d.
func readSudo(ctx context.Context, sys host.System, st sudoers.State) ([]rule, error) {
	main, err := sys.ReadFile(ctx, st.Main)
	if err != nil {
		return nil, fmt.Errorf("users: %w", err)
	}
	var rs []rule
	for _, l := range lines(main, st.Main) {
		f := strings.Fields(l.text)
		if len(f) != 2 || f[0] != "@includedir" && f[0] != "#includedir" || strings.TrimSuffix(f[1], "/") != sudoers.DropInDir {
			rs = keep(rs, l, sudoRule)
			continue
		}
		for _, e := range st.Entries {
			p := sudoers.DropInDir + "/" + e
			data, err := sys.ReadFile(ctx, p)
			if err != nil {
				return nil, fmt.Errorf("users: %w", err)
			}
			for _, l := range lines(data, p) {
				rs = keep(rs, l, sudoRule)
			}
		}
	}
	return rs, nil
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
			rs = keep(rs, l, doasRule)
		}
	}
	return rs, nil
}

// keep appends what read makes of l, unless l is no rule.
func keep(rs []rule, l line, read func(line) (rule, bool)) []rule {
	if r, ok := read(l); ok {
		rs = append(rs, r)
	}
	return rs
}

// line is one line of a file, trimmed, where it starts; a line ending in
// '\' is continued, and the lines it joins are one, never read.
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
	// on every host, as root (ALL or root, any group), every command,
	// with or without NOPASSWD. Debian's and Ubuntu's root and %sudo
	// lines, origin/main's and hardn's grants and cloud-init's are in it.
	sudoGrant = regexp.MustCompile(`^(%?` + name + `)\s+ALL\s*=\s*\(\s*(?:ALL|root)\s*(?::\s*ALL\s*)?\)\s*(NOPASSWD\s*:\s*)?ALL$`)
	// sudoNoRule: lines that grant no one anything themselves: Defaults
	// (not read: targetpw and the like are not modelled) and alias
	// definitions (a rule that uses an alias is not read).
	sudoNoRule   = regexp.MustCompile(`^(Defaults|User_Alias|Runas_Alias|Host_Alias|Cmnd_Alias|Cmd_Alias)\b`)
	doasIdentity = regexp.MustCompile(`^:?` + name + `$`)
)

// sudoRule reads one line of sudo's: ok is false for a line that is no
// rule (blank, a comment, Defaults, an alias); a rule not of sudoGrant's
// form is kept as not known. A '#' starts a comment except in #include
// and before a digit, a user ID (sudoers(5); sudoers.md §3).
func sudoRule(l line) (rule, bool) {
	t := l.text
	comment := strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "#include") && (len(t) == 1 || t[1] < '0' || t[1] > '9')
	if !l.joined && (t == "" || comment || sudoNoRule.MatchString(t)) {
		return rule{}, false
	}
	r := rule{at: l.at, text: strings.Join(strings.Fields(t), " ")}
	if m := sudoGrant.FindStringSubmatch(t); m != nil && !l.joined {
		r.who, r.nopass, r.known = m[1], m[2] != "", true
	}
	return r, true
}

// doasRule reads one line of doas's: permit or deny, options (nopass,
// persist, nolog, keepenv, setenv { … }), a user or :group, and nothing
// after it but "as root" (doas.conf(5)); any other rule is kept as not
// known. A '#' starts a comment; a line with a quote is not read.
func doasRule(l line) (rule, bool) {
	t := l.text
	if i := strings.IndexByte(t, '#'); i >= 0 && !strings.Contains(t, `"`) {
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
	if len(rest) == 0 || !doasIdentity.MatchString(rest[0]) || len(rest) != 1 && !slices.Equal(rest[1:], []string{"as", "root"}) {
		return r, true
	}
	r.who, r.deny, r.known = rest[0], f[0] == "deny", true
	return r, true
}

// grant is what one tool's rules decide for one user. The last rule that
// matches decides (sudoers(5), doas.conf(5)); a rule not read after it
// may match too, so the answer is then Unknown, by that rule.
type grant struct {
	tri    detect.Tri
	nopass bool
	by     rule  // the rule that decided, or the last not read; zero for no rule
	err    error // the tool's, when it decides Unknown
}

func (t tool) decide(db accounts.DB, u accounts.User) grant {
	switch t.state {
	case detect.No:
		return grant{tri: detect.No}
	case detect.Unknown:
		return grant{err: t.err}
	}
	g := grant{tri: detect.No}
	for _, r := range t.rules {
		switch {
		case !r.known:
			g = grant{by: r}
		case r.who == u.Name, (r.who[0] == '%' || r.who[0] == ':') && db.Member(u, r.who[1:]):
			g = grant{tri: detect.Yes, nopass: r.nopass, by: r}
			if r.deny {
				g = grant{tri: detect.No, by: r}
			}
		}
	}
	return g
}
