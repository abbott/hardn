// Package form collects answers line by line: the Collect step of brief
// 1C's canonical flow. It borrows huh's patterns without importing huh:
// declarative fields with a title, a description, a default, a
// validation and a skip rule, each bound to a typed value; a form of
// ordered fields, some skipped on conditions such as the distribution;
// titles and options computed from earlier answers; a validation error
// shown under the prompt, the field asked again; and one sentinel for an
// abort. A form only collects: it runs nothing, so no prompt is ever
// interleaved with a change to the system, and the caller acts on the
// answers only once Run returns nil.
//
// # Fields
//
// Three kinds of field cover the known screens: Text, a free-text value
// such as a user name; YesNo, a y/N question (keys.YesNo); and Select, a
// numbered choice among options. Each is a struct of declarative fields,
// passed to the form by pointer:
//
//	var user string
//	var sudo, password bool
//	f := form.Form{Fields: []form.Field{
//		&form.Text{Title: "Username", Validate: validUsername, Value: &user},
//		&form.YesNo{Title: "Grant sudo privilege?", Value: &sudo},
//		&form.YesNo{Title: "Require sudo password?", Value: &password,
//			Skip: func() bool { return !sudo }},
//	}}
//	err := f.Run(p)
//
// Value binds the answer: the form writes it as soon as the field takes
// a line, so that a later field's TitleFunc, OptionsFunc or Skip reads
// the earlier answers from the caller's own variables. If Run returns an
// error, every Value is put back as it was before Run: nothing partial is
// left to act on. A field that is skipped is put back too, so an answer
// given on a path the user then left does not stay behind.
//
// # Layout
//
// Every field has one structure (DEV-046), the same in plain mode, where
// it shows without colour, Unicode or animation (term.Env): its title,
// its description, its options, the prompt, an error under the prompt,
// and the answer's echo.
//
//   - A field with no description and no options is asked on one line, an
//     AnsweredField with no answer: "1. Username [0 or q]:", as nav
//     asks its questions and as Figma draws a form's fields, numbered;
//     but only while that line shows the question and its hint whole
//     (component.AnsweredField.ShowsWhole).
//   - A field with a description or options, and one whose one line
//     would cut its question, on a narrow terminal or for a long title,
//     shows a FieldHead (the title, the description, then the options as
//     a menu's rows, set apart by a blank line), then theme.PromptGap
//     blank lines, then a PromptLine of its own. A Select's asks "Enter
//     your choice", Figma's words for a numbered choice, as a menu's
//     prompt does: "> Enter your choice [1-2, 0 or q]:". A text or y/N
//     field's has no label, since the title right above is the question:
//     "> [0 or q]:". The head's title has its line to itself, so it
//     shows whole at widths where the one line would not; it is still one
//     line, cut with the ellipsis when it must be, so words a user has to
//     read whole, such as what a default is, belong in the description,
//     which wraps.
//   - A line the field does not take, and a value its Validate rejects,
//     is answered under the prompt by a failed InlineResult (≫, the words
//     in status.bad; DEV-062), the words being nav's or the validation's;
//     a value Validate refuses with nav.Warn's error, valid but not
//     taken, such as one already configured, by a warning one, in
//     status.warn. Then the prompt is asked again alone; nothing above it
//     is drawn again (nav.Prompter.Ask).
//   - Once answered, the field's echo, an AnsweredField with the answer,
//     is written under the answer when the line typed does not already
//     read as it: after a prompt line of its own, where a Select's echo
//     names the option chosen; and after an empty line that took a
//     default, which the terminal shows as nothing (DEV-047).
//
// The fields of a form of more than one field are numbered by their
// place among the fields asked, so a skipped field takes no number and
// the numbers run on (DEV-047). They are numbered even when Skip leaves
// only one to ask: a field's number is shown as it is asked, before the
// fields after it can tell whether they are skipped, since their Skip
// may read its answer.
//
// Each field is set apart from what is above it by theme.SectionGap blank
// lines once any prompt has been asked (prompt.Reader.Asked), as Figma
// spaces its form and as nav.Run spaces a menu from the answer that
// opened it. Before any prompt, Run cannot tell whether anything stands
// above its first field, so it writes no gap there: a caller that draws
// something above a form, such as a workflow step's title bar, ends it
// with theme.SectionGap blank lines itself when Asked reports false, and
// leaves them to Run when it reports true.
//
// # Navigation
//
// Every prompt of a form keeps the navigation grammar (pkg/ui/nav): 0
// goes back to the field asked before this one, asked again from the
// start, and from the first field leaves the form with nav.ErrBack; q
// quits with nav.ErrQuit (P-26 (a)). Back is 0 only (P-25): b is a line
// as any other, a text field's value, which the other fields refuse. The
// end of the input and Ctrl-C abort with ErrAborted, from any field, and
// never go back.
package form
