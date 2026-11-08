package nav

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

var (
	// ErrBack reports that the user went back with 0, at any prompt; on
	// a menu 0 is also the Exit or Return item's number. The caller
	// returns to the level above; at the top level, back exits.
	ErrBack = errors.New("nav: back")
	// ErrQuit reports that the user quit with q. Each level returns it to
	// the one above, so that it ends hardn from any depth.
	ErrQuit = errors.New("nav: quit")
	// ErrMenu reports a Menu that breaks the grammar (Menu.Check).
	ErrMenu = errors.New("nav: menu breaks the grammar")
	// ErrPhrase reports a phrase that cannot be typed to confirm
	// (CheckPhrase).
	ErrPhrase = errors.New("nav: phrase cannot be typed")
)

// The words written under a prompt that refused a line (proposal P-25).
// InvalidOption is origin/main's (pkg/menu main_menu.go:540); the others
// are new. The two exported ones are for prompts asked with Ask, such as
// a form's (pkg/ui/form): InvalidOption for a line that chooses none of
// a numbered list's items, InvalidAnswer for any other line a question
// does not take.
const (
	InvalidOption = "Invalid option. Please try again."
	InvalidAnswer = "Invalid answer. Please try again."
	unavailable   = "%s is unavailable."
	noMatch       = "That does not match exactly. Please try again."
)

// Prompter asks the questions of a run. It writes each prompt to Out and
// reads the answer through In, the one line reader over stdin
// (prompt.Stdin, which every prompt in hardn shares), and it renders
// with Ctx. Every field is required:
//
//	p := nav.Prompter{
//		Ctx: component.NewCtx(env),
//		In:  prompt.Stdin(),
//		Out: env.Writer(os.Stdout),
//	}
type Prompter struct {
	Ctx component.Ctx
	In  *prompt.Reader
	Out io.Writer
}

// Question is what a question prompt asks. It is asked as an
// AnsweredField with no answer yet, so that once answered the line reads
// as Figma draws it (DEV-036), its hint generated from what the prompt
// accepts.
type Question struct {
	// Number is the question's place in a form, shown as "1."; zero
	// shows none, as for a confirmation.
	Number int
	// Label is the question, such as "Username" or "Create user
	// 'george'?"; the hint and the colon are added. A question with no
	// label is not asked: there is nothing to show its hint after.
	Label string
}

// render renders the question as asked, with the hint of accepts.
func (q Question) render(c component.Ctx, accepts keys.Set) string {
	return component.AnsweredField{Number: q.Number, Label: q.Label, Accepts: accepts}.Render(c)
}

// Navigate returns ErrBack when line is 0 and ErrQuit when it is q, as
// keys.Set.Match compares lines (the white space around it and case
// ignored), and nil for any other line, b included. Every prompt applies
// it before its own answers, so 0 and q navigate wherever they are
// typed.
func Navigate(line string) error {
	if _, ok := (keys.Set{keys.Back()}).Match(line); ok {
		return ErrBack
	}
	if _, ok := (keys.Set{keys.Quit()}).Match(line); ok {
		return ErrQuit
	}
	return nil
}

// Text asks a free-text question, such as a user name, once. It returns
// the line typed without the white space around it, as origin/main reads
// text (pkg/menu input.go), possibly empty, b included; or ErrBack or
// ErrQuit for 0 or q (Navigate), so a value can never be 0 or q (P-25
// (a)). The hint, "0 or q" (keys.Navigation), shows where that rule
// applies.
// Text checks nothing else: a form (pkg/ui/form) asks its own text
// fields with Ask, and refuses there a value its validation rejects.
func (p Prompter) Text(q Question) (string, error) {
	var value string
	err := p.Ask(q.render(p.Ctx, keys.Navigation()), InvalidAnswer, func(line string) error {
		value = strings.TrimSpace(line)
		return Navigate(line)
	})
	if err != nil {
		return "", err
	}
	return value, nil
}

// Confirm asks a y/N question and returns the answer: true for y or yes,
// false for n or no, in any case, and defaultYes for an empty line. The
// hint shows the default in capitals, "[y/N, 0 or q]" or "[Y/n, 0 or q]"
// (brief 1C). Confirm returns ErrBack or ErrQuit for 0 or q, so 0 is
// never taken for "no", and asks again for any other line, b included.
func (p Prompter) Confirm(q Question, defaultYes bool) (bool, error) {
	answers := keys.YesNo(defaultYes)
	var yes bool
	err := p.Ask(q.render(p.Ctx, append(answers, keys.Navigation()...)), InvalidAnswer, func(line string) error {
		if err := Navigate(line); err != nil {
			return err
		}
		i, ok := answers.Match(line)
		if !ok {
			return Refuse(InvalidAnswer)
		}
		yes = i == 0 // YesNo lists yes first
		return nil
	})
	return yes, err
}

// Typed asks for a typed confirmation, which brief 1C requires for
// changes with lockout risk and for Run All: the user must type phrase,
// such as the hostname, exactly, case included; only the white space
// around the line is ignored. The phrase is shown as the answer the hint
// offers, "[web-01, 0 or q]", and always whole: the prompt cuts the
// question's label first, and Typed asks only when the whole hint fits
// beside some of the label (shows). The phrase's binding is there for the
// hint; the line is compared with the phrase itself. Typed returns nil
// once the phrase is typed, ErrBack or ErrQuit for 0 or q, and asks
// again for any other line, b and an Enter alone included, under an
// error that does not repeat the phrase: the prompt under it shows it
// whole, while the error's prose would wrap at a hyphen inside it (x/ansi
// wrap.go 270-276: a hyphen is always a breakpoint).
//
// Before anything is written, Typed returns an error wrapping ErrPhrase
// for a phrase that CheckPhrase refuses, and for one too long to show
// whole at the width of p.Ctx: with the label "Type the hostname", a
// phrase of up to 48 cells from 64 columns up, and 44 at 60, in Unicode
// glyphs (46 and 42 in ASCII, whose ellipsis is wider). The caller then
// asks with a shorter phrase or a shorter label, as the flow words them
// (P-10). CheckTyped tells so without asking.
func (p Prompter) Typed(q Question, phrase string) error {
	question, err := p.typed(q, phrase)
	if err != nil {
		return err
	}
	return p.Ask(question, noMatch, func(line string) error {
		if err := Navigate(line); err != nil {
			return err
		}
		if strings.TrimSpace(line) != phrase {
			return Refuse(noMatch)
		}
		return nil
	})
}

// CheckTyped returns the error Typed returns before it asks, and asks
// nothing and writes nothing: nil when Typed would ask q with phrase,
// else an error wrapping ErrPhrase, for a phrase CheckPhrase refuses or
// one too long to show whole beside q's label at the width of p.Ctx. A
// caller that writes something above the prompt checks so first, so that
// it writes nothing for a prompt that is never asked.
func (p Prompter) CheckTyped(q Question, phrase string) error {
	_, err := p.typed(q, phrase)
	return err
}

// typed returns q rendered as Typed asks it with phrase, or the error
// with which Typed refuses before asking.
func (p Prompter) typed(q Question, phrase string) (string, error) {
	if err := CheckPhrase(phrase); err != nil {
		return "", err
	}
	accepts := append(keys.Set{{Keys: []string{phrase}}}, keys.Navigation()...)
	question := q.render(p.Ctx, accepts)
	if question != "" && !shows(question, q.Number, "["+accepts.Hint()+"]:") {
		return "", fmt.Errorf("%w: %q does not show whole beside the question at %d columns", ErrPhrase, phrase, p.Ctx.Env.Width)
	}
	return question, nil
}

// shows reports whether question, an AnsweredField rendered with no
// answer and the field number number, shows hint whole after some of its
// label: PromptLine's layout, which AnsweredField shares, cuts the label
// first, then leaves it out, then cuts the hint.
func shows(question string, number int, hint string) bool {
	before, _, ok := strings.Cut(ansi.Strip(question), hint)
	if !ok {
		return false
	}
	before = strings.TrimSpace(before)
	if number > 0 {
		before = strings.TrimSpace(strings.TrimPrefix(before, strconv.Itoa(number)+"."))
	}
	return before != ""
}

// CheckPhrase returns nil when phrase can be typed to confirm, and else an
// error wrapping ErrPhrase. The phrase must be printable ASCII, as a
// hostname or a user name is, so that it shows as it must be typed:
// components drop control characters and show typographic quotes as ASCII
// ones (C12). It must have no white space at either end, which Typed
// trims from the line typed. It must not be 0 or q, in any case: those
// navigate at every prompt (Navigate), so the phrase could never be
// typed; b, which does not, is a phrase. And it must hold none of what a
// hint lists its inputs with, a comma, a slash, a bracket or " or ", so
// that "[web-01, 0 or q]" reads one way; no hostname holds them. A
// number other than 0, such as a port, is a phrase. CheckPhrase does not
// know the width: Typed also refuses a phrase too long to show whole.
func CheckPhrase(phrase string) error {
	for _, r := range phrase {
		if r < ' ' || r > '~' {
			return fmt.Errorf("%w: %q holds a character that is not printable ASCII", ErrPhrase, phrase)
		}
	}
	switch {
	case phrase == "":
		return fmt.Errorf("%w: it is empty", ErrPhrase)
	case strings.TrimSpace(phrase) != phrase:
		return fmt.Errorf("%w: %q starts or ends with a space", ErrPhrase, phrase)
	case Navigate(phrase) != nil:
		return fmt.Errorf("%w: %q is a navigation input", ErrPhrase, phrase)
	case strings.ContainsAny(phrase, ",/[]") || strings.Contains(phrase, " or "):
		return fmt.Errorf("%w: %q holds a comma, a slash, a bracket or \" or \", which a hint lists its inputs with", ErrPhrase, phrase)
	}
	return nil
}

// refused is a line a prompt does not accept: why, and the kind of
// InlineResult Ask writes it as under the prompt before it asks again,
// failed (Refuse) or a warning (Warn).
type refused struct {
	why  string
	kind component.MessageKind
}

func (r refused) Error() string { return r.why }

// Refuse returns the error with which a resolve function passed to Ask
// refuses a line: Ask writes reason under the prompt as a failed
// InlineResult and asks again.
func Refuse(reason string) error { return refused{reason, component.MessageBad} }

// Warn is Refuse for a line refused with a warning rather than as a
// failure, such as a value already configured: Ask writes reason as a
// warning InlineResult, in status.warn (DEV-062).
func Warn(reason string) error { return refused{reason, component.MessageWarn} }

// RefuseFor returns the refusal of a line for err: err's own when err is
// or wraps one (Refuse, Warn), else a failed refusal in err's words. A
// form refuses with it a value its field's validation rejects, so that
// the validation may refuse with Warn.
func RefuseFor(err error) error {
	var r refused
	if errors.As(err, &r) {
		return r
	}
	return Refuse(err.Error())
}

// Ask writes question, a prompt rendered by the caller, and reads lines
// until resolve accepts one; every prompt of the grammar asks so, and a
// form (pkg/ui/form) asks its fields so. The question is an AnsweredField
// with no answer or a PromptLine, its hint generated from what resolve
// accepts, ending with keys.Navigation's inputs, which resolve hands to
// Navigate first.
//
// resolve returns nil to accept the line; ErrBack or ErrQuit to navigate;
// or a reason from Refuse, which Ask writes under the prompt as a failed
// component.InlineResult (⨳ and the reason in status.bad, no word; P-42,
// DEV-062), or from Warn, which it writes as a warning one (status.warn),
// before it writes question again, leaving the screen above as it is
// (brief 1C: re-ask only that field, keeping context). A line longer than
// prompt.MaxLine is refused for the reason invalid, and so is a line
// refused for a reason that shows nothing. Any other error, from resolve
// or from the reader, is returned: the end of the input and Ctrl-C
// (prompt.ErrAborted) abort, and never go back. An empty question, as a
// Question with no label renders, is an error.
func (p Prompter) Ask(question, invalid string, resolve func(line string) error) error {
	return p.ask(question, invalid, false, resolve)
}

// ask is Ask; with choice set the answer is read as a menu choice
// (prompt.Reader.AskChoice), key by key, where stdin and stdout are both a
// terminal, so that a key no answer has, such as an arrow, shows nothing.
func (p Prompter) ask(question, invalid string, choice bool, resolve func(line string) error) error {
	if question == "" {
		return errors.New("nav: the question has no label")
	}
	read := p.In.Ask
	if choice && p.Ctx.Env.TTY && p.Ctx.Env.InputTTY {
		read = p.In.AskChoice
	}
	for {
		line, err := read(p.Out, question)
		var why refused
		switch {
		case errors.Is(err, prompt.ErrLineTooLong):
			why = refused{invalid, component.MessageBad}
		case err != nil:
			return err
		default:
			if err := resolve(line); !errors.As(err, &why) {
				return err
			}
		}
		msg := component.InlineResult{Kind: why.kind, Text: why.why}.Render(p.Ctx)
		if msg == "" {
			msg = component.InlineResult{Kind: component.MessageBad, Text: invalid}.Render(p.Ctx)
		}
		if _, err := io.WriteString(p.Out, msg+"\n"); err != nil {
			return fmt.Errorf("nav: write: %w", err)
		}
	}
}
