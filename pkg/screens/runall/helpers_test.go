package runall_test

import (
	"bytes"
	"context"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	runallscreen "github.com/abbott/hardn/pkg/screens/runall"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The flow's words a test looks for: the typed confirmation's label and
// its refusal, plan only's end, an apply's, the timer's.
const (
	typeIt      = "Type the hostname"
	cantConfirm = "Can't confirm with the hostname:"
	planned     = "No actual changes were made"
	applied     = "Operation Successful"
	timer       = "This change reverts automatically in"
)

// played is one run of the screen: what it wrote, with the answers echoed,
// what Apply all did (Options.Ran), how many engines it made, what Run
// returned and how many lines were left unread.
type played struct {
	out     string
	runs    []output.Run
	engines int
	err     error
	unread  int
}

// play runs the screen on h, plain at 120 columns, the user typing lines;
// a dry run with dryRun.
func play(h *runHost, dryRun bool, lines ...string) played {
	return playWith(h, dryRun, nil, lines...)
}

// playWith is play with the options set changes, when not nil. Each
// engine is built as cmd/hardn builds it (runHost.newEngine).
func playWith(h *runHost, dryRun bool, set func(*runallscreen.Options), lines ...string) played {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, lines...)
	var p played
	o := options(h, dryRun, &p)
	o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)}
	if set != nil {
		set(&o)
	}
	p.err = runallscreen.Run(context.Background(), o)
	p.out, p.unread = out.String(), ty.Unread()
	return p
}

// options are the screen's options on h, as cmd/hardn gives them, its
// Prompter left to the caller; p counts the engines and keeps the runs.
func options(h *runHost, dryRun bool, p *played) runallscreen.Options {
	return runallscreen.Options{
		System: h, Facts: h.facts(), Session: h.sess, Getenv: h.getenv, Config: h.cfg, DryRun: dryRun,
		LogPath: output.LegacyLogPath, Now: clock, Version: "v0.4.0", Revert: revert.Options{Now: clock, Hardn: hardnBinary},
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			p.engines++
			return h.newEngine(progress)
		},
		Ran: func(r output.Run) { p.runs = append(p.runs, r) },
	}
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

// at is where out first shows words, compared flat; -1 for nowhere.
func at(out, words string) int { return strings.Index(flat(out), flat(words)) }

// asked reports whether out asked the question: y/N or the hostname.
func asked(out string) bool {
	return strings.Contains(out, "[y/N") || strings.Contains(out, typeIt)
}
