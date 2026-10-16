package nav

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

var (
	// ErrBack reports that the user went back with 0 or b, at any prompt;
	// on a menu 0 is also the Exit or Return item's number. The caller
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
// The first is origin/main's (pkg/menu main_menu.go:540); the others are
// new.
const (
	invalidOption = "Invalid option. Please try again."
	invalidAnswer = "Invalid answer. Please try again."
	unavailable   = "%s is unavailable."
	noMatch       = "That does not match '%s'. Please try again."
)

// Prompter asks the questions of a run. It writes each prompt to Out and
// reads the answer through In, the one line reader over stdin, and it
// renders with Ctx. Every field is required:
//
//	p := nav.Prompter{
//		Ctx: component.NewCtx(env),
//		In:  prompt.New(os.Stdin, env.InputTTY),
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

// Navigate returns ErrBack when line is 0 or b and ErrQuit when it is q,
// as keys.Set.Match compares lines (the white space around it and case
// ignored), and nil for any other line. Every prompt applies it before
// its own answers, so 0, b and q navigate wherever they are typed.
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
// text (pkg/menu input.go), possibly empty; or ErrBack or ErrQuit for 0,
// b or q (Navigate), so a value can never be 0, b or q (P-25 (a)). The
// hint, "0, b or q" (keys.Navigation), shows where that rule applies.
// Text checks nothing else: a form (next/p1c-form) validates the value
// and asks again.
func (p Prompter) Text(q Question) (string, error) {
	var value string
	err := p.ask(q.render(p.Ctx, keys.Navigation()), invalidAnswer, func(line string) error {
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
// hint shows the default in capitals, "[y/N, 0, b or q]" or
// "[Y/n, 0, b or q]" (brief 1C). Confirm returns ErrBack or ErrQuit for
// 0, b or q, so 0 is never taken for "no", and asks again for any other
// line.
func (p Prompter) Confirm(q Question, defaultYes bool) (bool, error) {
	answers := keys.YesNo(defaultYes)
	var yes bool
	err := p.ask(q.render(p.Ctx, append(answers, keys.Navigation()...)), invalidAnswer, func(line string) error {
		if err := Navigate(line); err != nil {
			return err
		}
		i, ok := answers.Match(line)
		if !ok {
			return refused(invalidAnswer)
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
// offers, "[web-01, 0, b or q]", the part of the line that is cut last
// when it does not fit. Typed returns nil once the phrase is typed,
// ErrBack or ErrQuit for 0, b or q, and asks again for any other line, an
// Enter alone
// included, naming the phrase in the error, whose prose wraps rather than
// being cut. A phrase that CheckPhrase refuses returns its error before
// anything is written.
func (p Prompter) Typed(q Question, phrase string) error {
	if err := CheckPhrase(phrase); err != nil {
		return err
	}
	again := fmt.Sprintf(noMatch, phrase)
	accepts := append(keys.Set{{Keys: []string{phrase}}}, keys.Navigation()...)
	return p.ask(q.render(p.Ctx, accepts), again, func(line string) error {
		if err := Navigate(line); err != nil {
			return err
		}
		if strings.TrimSpace(line) != phrase {
			return refused(again)
		}
		return nil
	})
}

// CheckPhrase returns nil when phrase can be typed to confirm, and else an
// error wrapping ErrPhrase. The phrase must be printable ASCII, as a
// hostname or a user name is, so that it shows as it must be typed:
// components drop control characters and show typographic quotes as ASCII
// ones (C12). It must have no white space at either end, which Typed
// trims from the line typed. And it must not be 0, b or q in any case:
// those navigate at every prompt, so the phrase could never be typed. The
// caller then asks for another phrase, such as a host's fully qualified
// name.
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
	}
	return nil
}

// refused is a line a prompt does not accept, and why: ask writes the
// reason under the prompt and asks again.
type refused string

func (r refused) Error() string { return string(r) }

// ask writes question and reads lines until resolve accepts one. resolve
// returns nil to accept the line; ErrBack or ErrQuit to navigate; or a
// refused reason, which ask writes under the prompt as an error Message
// before it asks again, leaving the screen above as it is. A line longer
// than prompt.MaxLine is refused for the reason tooLong. Any other error,
// from resolve or from the reader, is returned: the end of the input and
// Ctrl-C (prompt.ErrAborted) abort, and never go back. An empty
// question, as a Question with no label renders, is an error.
func (p Prompter) ask(question, tooLong string, resolve func(line string) error) error {
	if question == "" {
		return errors.New("nav: the question has no label")
	}
	for {
		line, err := p.In.Ask(p.Out, question)
		var why refused
		switch {
		case errors.Is(err, prompt.ErrLineTooLong):
			why = refused(tooLong)
		case err != nil:
			return err
		default:
			if err := resolve(line); !errors.As(err, &why) {
				return err
			}
		}
		msg := component.Message{Kind: component.MessageBad, Text: string(why)}.Render(p.Ctx)
		if _, err := io.WriteString(p.Out, msg+"\n"); err != nil {
			return fmt.Errorf("nav: write: %w", err)
		}
	}
}
