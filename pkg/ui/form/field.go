package form

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// Field is one question of a Form: a *Text, a *YesNo or a *Select. Its
// methods are the form's own, so these are the only kinds of field.
type Field interface {
	// view returns the field as it is asked now, its TitleFunc and
	// OptionsFunc called afresh, or why it cannot be asked.
	view() (view, error)
	// resolve takes a line typed to the field as view last returned it.
	// It binds the value and returns the answer as the echo shows it, or
	// refuses the line (nav.Refuse) when the field does not take it or
	// Validate rejects its value.
	resolve(line string) (string, error)
	// skipped reports whether Skip leaves the field out now.
	skipped() bool
	// save keeps the bound value as it is before the form runs, and
	// restore puts it back.
	save() error
	restore()
}

// view is a field as it is asked.
type view struct {
	title       string
	description string
	options     []component.MenuItem // a Select's, keyed from 1
	label       string               // its own prompt line's: a Select's choice; empty for the rest
	accepts     keys.Set             // the field's own inputs: a y/N's answers, a Select's keys
	echo        keys.Set             // the hint its answered echo shows: a y/N's answers
	invalid     string               // nav's words for a line the field does not take
}

// Text is a free-text field, such as a user name. Its value is the line
// typed without the white space around it, as nav.Text reads it: 0 and
// q navigate there too, so the value is never one of them (P-25 (a)); b
// is a value as any other line is.
type Text struct {
	// Title is the question, such as "Username". TitleFunc, when set,
	// gives it instead each time the field is asked, from earlier answers.
	Title     string
	TitleFunc func() string
	// Description says what to type, under the title; empty shows none.
	Description string
	// Default is the value an empty line gives. Nothing shows it before
	// the answer unless the screen's words say so, best in the
	// description, which wraps, since a title is cut to one line; the
	// answer's echo shows it after (P-26 (c)).
	Default string
	// Validate, when set, checks the value. Its error is shown under the
	// prompt, and the field is asked again: as a failed inline result, or
	// as a warning when the error is nav.Warn's, for a value refused
	// though valid, such as one already configured. It may read the
	// system, such as whether a user exists, but must change nothing.
	Validate func(string) error
	// Skip, when set, leaves the field out each time it returns true.
	Skip func() bool
	// Value is where the answer is bound. It must not be nil.
	Value *string

	before string
}

func (f *Text) view() (view, error) {
	return view{title: pick(f.Title, f.TitleFunc), description: f.Description, invalid: nav.InvalidAnswer}, nil
}

func (f *Text) resolve(line string) (string, error) {
	v := strings.TrimSpace(line)
	if v == "" {
		v = f.Default
	}
	if err := check(f.Validate, v); err != nil {
		return "", err
	}
	*f.Value = v
	return v, nil
}

func (f *Text) skipped() bool { return f.Skip != nil && f.Skip() }
func (f *Text) restore()      { *f.Value = f.before }

func (f *Text) save() error {
	if f == nil {
		return errNil
	}
	return save(f.Value, &f.before)
}

// YesNo is a y/N question (brief 1C), such as "Grant sudo privilege?".
// It takes y or yes for true and n or no for false, in any case, and an
// empty line for its Default, which the hint shows in capitals: "[y/N]",
// or "[Y/n]" when Default is true. Any other line is refused with nav's
// words, b included; 0 and q navigate, so a 0 is never taken for "no".
type YesNo struct {
	// Title is the question, such as "Grant sudo privilege?". TitleFunc,
	// when set, gives it instead each time the field is asked, from
	// earlier answers, such as "Create user 'george'?".
	Title     string
	TitleFunc func() string
	// Description says what the question decides, under the title; empty
	// shows none.
	Description string
	// Default is the answer an empty line gives: false for "[y/N]", true
	// for "[Y/n]".
	Default bool
	// Validate, when set, checks the answer, as Text's does.
	Validate func(bool) error
	// Skip, when set, leaves the field out each time it returns true.
	Skip func() bool
	// Value is where the answer is bound. It must not be nil.
	Value *bool

	before bool
}

func (f *YesNo) view() (view, error) {
	answers := keys.YesNo(f.Default)
	return view{
		title: pick(f.Title, f.TitleFunc), description: f.Description,
		accepts: answers, echo: answers, invalid: nav.InvalidAnswer,
	}, nil
}

func (f *YesNo) resolve(line string) (string, error) {
	answers := keys.YesNo(f.Default)
	i, ok := answers.Match(line)
	if !ok {
		return "", nav.Refuse(nav.InvalidAnswer)
	}
	v := i == 0 // keys.YesNo lists yes first
	if err := check(f.Validate, v); err != nil {
		return "", err
	}
	*f.Value = v
	return answers[i].Keys[0], nil
}

func (f *YesNo) skipped() bool { return f.Skip != nil && f.Skip() }
func (f *YesNo) restore()      { *f.Value = f.before }

func (f *YesNo) save() error {
	if f == nil {
		return errNil
	}
	return save(f.Value, &f.before)
}

// choice is the label of a Select's prompt line, "Option", which every
// menu's prompt says too (P-29 (a); nav.Prompter.Choose), so that a
// numbered choice is asked alike wherever it is asked. Figma's frames say
// "Enter your choice".
const choice = "Option"

// Option is one choice of a Select: the label shown, an optional
// description beside it, as a menu item's, and the value it gives.
type Option[T any] struct {
	Label       string
	Description string
	Value       T
}

// Select is a numbered choice whose answer the form collects, such as
// the SSH key to remove (origin/main's "Select a key to remove", pkg/menu
// user_options.go:502): its options are listed as a menu's items, keyed
// from 1 in order, under its title, and its prompt line asks "Enter your
// choice", as a menu's does; it takes the number of one. Any other line
// is refused with nav's words for a menu, b included; 0 and q navigate.
// A Select has no Default: a numbered list's hint has no way to mark one,
// and an empty line chooses nothing, as on a menu. Its echo names the
// option chosen.
//
// A choice that opens a screen of its own, such as Figma's "Select a
// user" (USER-MANAGE_USER-SELECT_USER), whose user's management menu
// follows, is a menu (nav.Menu), not a form field (DEV-046).
type Select[T any] struct {
	// Title is the question, such as "Select a user". TitleFunc, when
	// set, gives it instead each time the field is asked.
	Title     string
	TitleFunc func() string
	// Description says what the choice decides, under the title; empty
	// shows none.
	Description string
	// Options are the choices, in order. OptionsFunc, when set, gives
	// them instead each time the field is asked, from earlier answers or
	// the host. Each needs a label, and there must be at least one.
	Options     []Option[T]
	OptionsFunc func() []Option[T]
	// Validate, when set, checks the value chosen, as Text's does.
	Validate func(T) error
	// Skip, when set, leaves the field out each time it returns true.
	Skip func() bool
	// Value is where the chosen option's value is bound. It must not be
	// nil.
	Value *T

	before T
	asked  []Option[T] // the options as last asked
	inputs keys.Set    // their keys
}

func (f *Select[T]) view() (view, error) {
	options := f.Options
	if f.OptionsFunc != nil {
		options = f.OptionsFunc()
	}
	if len(options) == 0 {
		return view{}, errors.New("has no options")
	}
	items := make([]component.MenuItem, len(options))
	for i, o := range options {
		items[i] = component.MenuItem{Key: strconv.Itoa(i + 1), Label: o.Label, Description: o.Description}
		if !items[i].Shown() {
			return view{}, fmt.Errorf("has an option, %d, with no label", i+1)
		}
	}
	f.asked, f.inputs = options, component.Menu{Items: items}.Bindings()
	return view{
		title: pick(f.Title, f.TitleFunc), description: f.Description,
		options: items, label: choice, accepts: f.inputs, invalid: nav.InvalidOption,
	}, nil
}

func (f *Select[T]) resolve(line string) (string, error) {
	i, ok := f.inputs.Match(line)
	if !ok {
		return "", nav.Refuse(nav.InvalidOption)
	}
	o := f.asked[i]
	if err := check(f.Validate, o.Value); err != nil {
		return "", err
	}
	*f.Value = o.Value
	return o.Label, nil
}

func (f *Select[T]) skipped() bool { return f.Skip != nil && f.Skip() }
func (f *Select[T]) restore()      { *f.Value = f.before }

func (f *Select[T]) save() error {
	if f == nil {
		return errNil
	}
	return save(f.Value, &f.before)
}

// pick returns fn's title when fn is set, else title.
func pick(title string, fn func() string) string {
	if fn != nil {
		return fn()
	}
	return title
}

// check runs validate, when set, on v, and turns its error into a
// refusal that shows the error's words under the prompt: failed, or a
// warning when the error is nav.Warn's.
func check[T any](validate func(T) error, v T) error {
	if validate == nil {
		return nil
	}
	if err := validate(v); err != nil {
		return nav.RefuseFor(err)
	}
	return nil
}

// save keeps *value in *before, or reports that there is no value.
func save[T any](value, before *T) error {
	if value == nil {
		return errors.New("has no Value to bind its answer to")
	}
	*before = *value
	return nil
}
