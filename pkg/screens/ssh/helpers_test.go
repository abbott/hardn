package ssh_test

import (
	"bytes"
	"context"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	sshscreen "github.com/abbott/hardn/pkg/screens/ssh"
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

// loadYML writes data as hardn.yml on h, root's 0600, and reads it as hardn
// reads it.
func loadYML(t *testing.T, h *sshHost, data string) *config.Config {
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
func play(h *sshHost, dryRun bool, lines ...string) played {
	return playWith(h, dryRun, nil, lines...)
}

// playWith is play with the options set changes, when not nil. Each
// engine is built as cmd/hardn builds it, on the host's session.
func playWith(h *sshHost, dryRun bool, set func(*sshscreen.Options), lines ...string) played {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, lines...)
	var p played
	o := sshscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   h, Facts: h.facts(), Session: h.sess, Getenv: h.getenv, DryRun: dryRun, LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			p.engines++
			return newEngine(h)(progress)
		},
		Ran: func(r output.Run) { p.runs = append(p.runs, r) },
	}
	if set != nil {
		set(&o)
	}
	p.err = sshscreen.Run(context.Background(), o)
	p.out, p.unread = out.String(), ty.Unread()
	return p
}

// newEngine is the engine on h as cmd/hardn builds it: its clock, the
// facts, the default backups, the host's session.
func newEngine(h *sshHost) func(progress func(engine.Event)) *engine.Engine {
	return func(progress func(engine.Event)) *engine.Engine {
		return engine.New(h, engine.Options{
			Revert:   revert.Options{Now: clock, Hardn: hardnBinary},
			Detect:   func(context.Context, host.System) (detect.Facts, error) { return h.facts(), nil },
			Hold:     func() func() { return func() {} },
			PID:      5000,
			Session:  h.sess,
			Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
			Progress: progress,
		})
	}
}

// confirmWindow applies the port change to 2230 on h, not confirmed: 22
// and 2230 held, hardn confirm pending, as the gallery's confirm-window.
func confirmWindow(t *testing.T, h *sshHost) {
	t.Helper()
	ctx := context.Background()
	e := newEngine(h)(nil)
	p, err := e.Plan(ctx, []engine.Control{sshchange.Port{To: 2230}}, &config.Config{})
	if err != nil || len(p.Refused) > 0 {
		t.Fatalf("confirm-window: plan: %v, refused %v", err, p.Refused)
	}
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatalf("confirm-window: apply: %v", err)
	}
}

// ready is the host name names: confirm-window's change applied.
func ready(t *testing.T, name string) *sshHost {
	t.Helper()
	h := newHost(t, name)
	if name == "confirm-window" {
		confirmWindow(t, h)
	}
	return h
}

// readOnlyRun reports whether argv only reads: sshd -T and -t, journalctl, ss,
// ufw status, the tools' checks and systemctl's questions.
func readOnlyRun(argv []string) bool {
	cmd := strings.Join(argv, " ")
	switch {
	case path.Base(argv[0]) == "sshd" && (slices.Contains(argv, "-T") || slices.Contains(argv, "-t")), argv[0] == "journalctl", argv[0] == "ss",
		cmd == "ufw status verbose", cmd == "visudo -c", strings.HasPrefix(cmd, "doas -C "),
		strings.HasPrefix(cmd, "systemctl is-"), strings.HasPrefix(cmd, "systemctl show "), strings.HasPrefix(cmd, "systemd-analyze verify "):
		return true
	}
	return false
}

// writes are the calls on h, from the nth on, that could change the host
// outside /run/hardn, the engine's run directory: every call but a read or a
// lookup, every command but those that only read (readOnlyRun).
func writes(h *sshHost, n int) []string {
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
