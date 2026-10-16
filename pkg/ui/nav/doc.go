// Package nav is hardn's navigation grammar (brief 1C): what each typed
// line does at a prompt, the same at every level, and the loops that ask
// until a line is accepted. Its inputs are data in pkg/ui/keys, read
// through pkg/ui/prompt and rendered by pkg/ui/component; nav binds each
// input to what it does.
//
// # The grammar
//
// Every input is a line, typed and confirmed with Enter (brief §2). A line
// is matched without the white space around it, and letters in any case
// (keys.Set.Match).
//
//   - Numbers select a menu's items.
//   - 0 or b goes back one level from any prompt (ErrBack). At the top
//     level, back exits: Run returns, and its caller ends hardn. On a
//     menu 0 is also the number of the Exit or Return item, shown as
//     "0)".
//   - q quits hardn from any prompt (ErrQuit): each level returns it to
//     the one above.
//   - A y/N question (Confirm) takes y or yes, n or no, and Enter alone
//     for its default, the capital in the hint.
//   - A typed confirmation (Typed) takes its phrase, such as the
//     hostname, exactly, case included. An Enter alone never confirms.
//   - A free-text question (Text) takes any other line.
//
// So 0, b and q navigate wherever they are typed, as brief 1C and the
// refactor plan have it (the same at every level), and are never an
// answer: not a "no" at a y/N question, not a free-text value (a user
// named b or q cannot be typed in, P-25 (a)), nor a phrase (CheckPhrase
// refuses one).
//
// Each prompt's hint is generated from what it accepts (keys.Set.Hint):
// the prompt's own inputs, then the navigation inputs, so that these end
// every hint alike: "[0-10, b or q]" on the main menu, where 0 is the
// Exit item's number too, "[0-2, b or q]" on a sub-menu,
// "[y/N, 0, b or q]" for a y/N question, "[web-01, 0, b or q]" for a
// typed confirmation and "[0, b or q]" for free text (DEV-044, DEV-045;
// P-25 puts the free-text and typed forms, and how the navigation inputs
// are explained, to the maintainer).
//
// # Asking again
//
// A line a prompt does not accept is answered under the prompt by an
// error Message, then the prompt is asked again; the screen above is not
// drawn again (brief 1C: re-ask only that field, keeping context). The
// words are origin/main's "Invalid option. Please try again." on a menu,
// and P-25's for the rest: a disabled item's key says the item is
// unavailable, a y/N question says "Invalid answer", and so does a
// free-text question for the one line it refuses, one longer than
// prompt.MaxLine (which every prompt refuses with its own words); a typed
// confirmation says the line does not match exactly, and the prompt under
// it shows the phrase whole (Typed).
//
// Prompter.Ask is that loop. Other packages ask with it too: a form
// (pkg/ui/form) asks each field with it, hands each line to Navigate
// first, and refuses a value its validation rejects with Refuse, so a
// validation error shows under the prompt as every refused line does.
//
// # Aborting
//
// The end of the input and Ctrl-C are returned as the reader reports them
// (prompt.ErrAborted): they abort hardn, exit 130 (prompt.ExitCode), and
// never go back. A script that runs out of answers must not unwind to the
// top as though the user had chosen to leave, and exit 0 there.
//
// # Menus bound to actions
//
// A Menu pairs each item it shows (component.MenuItem, plain data) with
// the action that choosing it runs (Option). The one list gives the items
// the menu renders, the inputs its prompt accepts and the action each
// runs, so no input reaches an action the menu does not show and no item
// it shows lacks one: origin/main's menus keep their options as data but
// dispatch with a separate switch, and hidden options stay selectable
// (docs/refactor/phase0/interaction.md §1 item 4). The pairing follows
// arch3's option ↔ action binding (refactor plan §6), as an idea; nothing
// was copied. A disabled item is shown, not accepted, and its key is left
// out of the hint (P-22).
//
// # Writing and reading
//
// A Prompter holds the render context, the one line reader over stdin and
// the writer around stdout. It writes only to that writer and reads only
// through that reader; the reader discards typeahead before each prompt
// on a terminal (prompt.Reader.Ask).
package nav
