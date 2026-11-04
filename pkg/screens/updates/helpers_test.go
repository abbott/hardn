package updates_test

import (
	"bytes"
	"context"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	updscreen "github.com/abbott/hardn/pkg/screens/updates"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

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

// play runs the screen on m, plain at 120 columns, the user typing lines; a
// dry run with dryRun; hardn.yml none.
func play(m *model, dryRun bool, lines ...string) played {
	return playWith(m, dryRun, nil, lines...)
}

// playWith is play with the options set changes, when not nil. Each
// engine is built as cmd/hardn builds it.
func playWith(m *model, dryRun bool, set func(*updscreen.Options), lines ...string) played {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, lines...)
	var p played
	o := updscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   m.f, Facts: m.facts, DryRun: dryRun, LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			p.engines++
			return m.engine(progress)
		},
		Ran:     func(r output.Run) { p.runs = append(p.runs, r) },
		Version: version, Records: revert.Options{Now: clock},
	}
	if set != nil {
		set(&o)
	}
	p.err = updscreen.Run(context.Background(), o)
	p.out, p.unread = out.String(), ty.Unread()
	return p
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

// asked reports whether out asked the change's question.
func asked(out string) bool { return strings.Contains(out, "[y/N") }

// screens are the screens out drew, each from its title bar.
func screens(out string) []string {
	parts := strings.Split(out, "# Auto Updates #")
	return parts[1:]
}
