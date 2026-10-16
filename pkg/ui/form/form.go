package form

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/theme"
)

var (
	// ErrAborted is prompt.ErrAborted: the end of the input or Ctrl-C
	// ended the form (brief 1C, after huh's ErrUserAborted). Run returns
	// it as the reader reports it, so errors.Is tells prompt.ErrEndOfInput
	// from prompt.ErrInterrupted too.
	ErrAborted = prompt.ErrAborted
	// ErrField reports a field that cannot be asked: one with no Value,
	// with no title, or a Select with no options or an option with no
	// label.
	ErrField = errors.New("form: field cannot be asked")
)

// Form is fields asked one after another, in order (brief 1C, after huh's
// group): the Collect step of the canonical flow. See the package doc for
// the layout and the navigation.
type Form struct {
	// Fields are the questions, in order. A field whose Skip returns true
	// when its turn comes is left out.
	Fields []Field
}

// Run asks the fields of f in order through p, binding each answer to
// its field's Value, and returns nil once the last field is answered.
// 0 or b goes back to the field asked before, and from the first field
// asked returns nav.ErrBack; q returns nav.ErrQuit; the end of the input
// and Ctrl-C return ErrAborted. On any error, ErrField and a failed write
// included, every Value is put back as it was before Run, so the caller
// has nothing partial to act on. A form with no fields returns nil at
// once.
func (f Form) Run(p nav.Prompter) (err error) {
	for i, field := range f.Fields {
		if err := field.save(); err != nil {
			return fmt.Errorf("%w: field %d %v", ErrField, i+1, err)
		}
	}
	defer func() {
		if err != nil {
			for _, field := range f.Fields {
				field.restore()
			}
		}
	}()
	numbered := len(f.Fields) > 1
	var trail []int // the fields answered, in order: back returns to the last
	for i := 0; i < len(f.Fields); {
		field := f.Fields[i]
		if field.skipped() {
			field.restore()
			i++
			continue
		}
		number := 0
		if numbered {
			number = len(trail) + 1
		}
		err := ask(p, field, number)
		var broken fieldError
		switch {
		case errors.Is(err, nav.ErrBack) && len(trail) > 0:
			i, trail = trail[len(trail)-1], trail[:len(trail)-1]
		case errors.As(err, &broken):
			return fmt.Errorf("%w: field %d %v", ErrField, i+1, broken)
		case err != nil:
			return err
		default:
			trail = append(trail, i)
			i++
		}
	}
	return nil
}

// fieldError is why a field cannot be asked; Run reports it as ErrField.
type fieldError struct{ error }

// ask asks field, numbered number (none for 0), until it takes a line,
// as the package doc lays it out, and writes its echo when one is due.
func ask(p nav.Prompter, field Field, number int) error {
	v, err := field.view()
	if err != nil {
		return fieldError{err}
	}
	accepts := append(slices.Clone(v.accepts), keys.Navigation()...)
	asked := component.AnsweredField{Number: number, Label: v.title, Accepts: accepts}
	question := asked.Render(p.Ctx)
	if question == "" {
		// An AnsweredField, as a FieldHead, renders nothing only for a
		// title that shows nothing.
		return fieldError{errors.New("has no title")}
	}
	var out string
	if p.In.Asked() {
		out = strings.Repeat("\n", theme.SectionGap)
	}
	// The field gets a prompt line of its own when its head shows more
	// than the title, as FieldHead decides what shows, or when the one
	// line would cut the question.
	head := component.FieldHead{Number: number, Title: v.title, Description: v.description, Options: v.options}.Render(p.Ctx)
	own := strings.Contains(head, "\n") || !asked.ShowsWhole(p.Ctx)
	if own {
		out += head + strings.Repeat("\n", theme.PromptGap+1)
		question = component.PromptLine{Label: v.label, Accepts: accepts}.Render(p.Ctx)
	}
	if err := write(p.Out, out); err != nil {
		return err
	}
	var shown string
	var blank bool
	err = p.Ask(question, v.invalid, func(line string) error {
		if err := nav.Navigate(line); err != nil {
			return err
		}
		var err error
		shown, err = field.resolve(line)
		blank = strings.TrimSpace(line) == ""
		return err
	})
	if err != nil || shown == "" || !own && !blank {
		return err
	}
	echo := component.AnsweredField{Number: number, Label: v.title, Accepts: v.echo, Answer: shown}
	return write(p.Out, echo.Render(p.Ctx)+"\n")
}

// write writes s to w in one write; an empty s writes nothing.
func write(w io.Writer, s string) error {
	if s == "" {
		return nil
	}
	if _, err := io.WriteString(w, s); err != nil {
		return fmt.Errorf("form: write: %w", err)
	}
	return nil
}
