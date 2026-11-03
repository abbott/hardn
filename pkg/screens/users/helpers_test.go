package users_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	usrscreen "github.com/abbott/hardn/pkg/screens/users"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// yml is where a host keeps hardn.yml when a test gives it one.
const yml = "/etc/hardn/hardn.yml"

// clock is the hosts' clock.
var clock = func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }

// session is alice's connection to port 22 that applies a change.
var session = revert.Session{Remote: true, Conn: "192.0.2.10 50022 192.0.2.20 22", Source: "SSH_CONNECTION"}

// loadYML writes data as hardn.yml on h, root's 0600, and reads it as hardn
// reads it.
func loadYML(t *testing.T, h *usrHost, data string) *config.Config {
	t.Helper()
	h.AddDir("/etc/hardn", 0o755, 0, 0)
	h.AddFile(yml, []byte(data), 0o600, 0, 0)
	cfg, err := config.Read(context.Background(), h, yml)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// played is one run of the screen: what it wrote, with the answers echoed,
// what each change did (Options.Ran), how many engines it made, what Run
// returned and how many lines were left unread.
type played struct {
	out     string
	runs    []output.Run
	engines int
	err     error
	unread  int
}

// play runs the screen on h, plain at 120 columns, the user typing lines; a
// dry run with dryRun; hardn.yml none.
func play(h *usrHost, dryRun bool, lines ...string) played {
	return playWith(h, dryRun, nil, lines...)
}

// playWith is play with the options set changes, when not nil. Each change's
// engine is built as cmd/hardn builds it, on alice's session.
func playWith(h *usrHost, dryRun bool, set func(*usrscreen.Options), lines ...string) played {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, lines...)
	var p played
	o := usrscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   h, Facts: h.facts, Getenv: h.getenv, DryRun: dryRun, LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			p.engines++
			return newEngine(h)(progress)
		},
		Ran: func(r output.Run) { p.runs = append(p.runs, r) },
	}
	if set != nil {
		set(&o)
	}
	p.err = usrscreen.Run(context.Background(), o)
	p.out, p.unread = out.String(), ty.Unread()
	return p
}

// newEngine is the engine on h as cmd/hardn builds it: its clock, the
// facts, the default backups, alice's session.
func newEngine(h *usrHost) func(progress func(engine.Event)) *engine.Engine {
	return func(progress func(engine.Event)) *engine.Engine {
		return engine.New(h, engine.Options{
			Revert:   revert.Options{Now: clock, Hardn: hardnBinary},
			Detect:   func(context.Context, host.System) (detect.Facts, error) { return h.facts, nil },
			Hold:     func() func() { return func() {} },
			PID:      5000,
			Session:  session,
			Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
			Progress: progress,
		})
	}
}

// readOnlyRuns are the commands that only read: the tools' checks and
// sshd -T.
func readOnlyRun(argv []string) bool {
	cmd := strings.Join(argv, " ")
	return cmd == "visudo -c" || strings.HasPrefix(cmd, "doas -C ") || (argv[0] == "sshd" || argv[0] == "/usr/sbin/sshd") && strings.Contains(cmd, " -T")
}

// writes are the calls on h, from the nth on, that could change the host
// outside /run/hardn, the engine's run directory: every call but a read or a
// lookup, every command but those that only read (readOnlyRun).
func writes(h *usrHost, n int) []string {
	var out []string
	for _, c := range h.Calls()[n:] {
		switch c.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group":
		case "run", "start-detached":
			if !readOnlyRun(c.Argv) {
				out = append(out, c.Op+" "+host.FormatArgv(c.Argv))
			}
		default:
			if !strings.HasPrefix(c.Path, "/run/hardn") {
				out = append(out, c.Op+" "+c.Path)
			}
		}
	}
	return out
}

// ran reports whether h ran a command starting with prefix from its nth call
// on.
func ran(h *usrHost, n int, prefix string) bool {
	for _, c := range h.Calls()[n:] {
		if c.Op == "run" && strings.HasPrefix(host.FormatArgv(c.Argv), prefix) {
			return true
		}
	}
	return false
}

// flat is out with each callout's continued lines joined ("\n# "), a word
// broken after its hyphen made whole again, its white space as single
// spaces, so that a wrapped sentence reads whole.
func flat(out string) string {
	out = strings.ReplaceAll(out, "-\n# ", "-")
	return strings.Join(strings.Fields(strings.ReplaceAll(out, "\n# ", " ")), " ")
}

// shows reports whether out shows words, each compared flat.
func shows(out, words string) bool { return strings.Contains(flat(out), flat(words)) }

// asked reports whether out asked a change's question: y/N or the hostname.
func asked(out string) bool {
	return strings.Contains(out, "[y/N") || strings.Contains(out, "Type the hostname")
}

// checkHost fails t for a command h did not answer.
func checkHost(t *testing.T, name string, h *usrHost) {
	t.Helper()
	if got := h.unanswered(); len(got) > 0 {
		t.Errorf("%s: commands the fake does not answer: %q", name, got)
	}
}
