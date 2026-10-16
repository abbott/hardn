package nav

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// Option is one choice of a Menu: the item the menu shows and the action
// that choosing it runs. The item stays plain data, as every view-model
// is; the action lives here, beside it, so that one list holds both.
type Option struct {
	Item component.MenuItem
	// Action runs when the item is chosen. A sub-menu's action runs its
	// own screen (Run) and returns when the user goes back from it.
	Action func() error
}

// Menu is a menu bound to its actions. Its one list of options gives the
// items it shows (View), the inputs its prompt accepts and their hint
// (Accepts), and the action each input runs (Choose), so that no input
// reaches an action the menu does not show and no item it shows lacks
// one (see the package doc).
type Menu struct {
	// Options are the numbered items, in order; each key is a whole
	// number from 1 (Check).
	Options []Option
	// Exit is the Exit or Return item shown after the options, such as
	// Figma's "0) Exit" on the main menu and "0) Return" on a sub-menu.
	// Its key is 0, which goes back at every prompt, as b does; at the
	// top level that exits. With no label it is not shown, and 0 and b
	// still go back.
	Exit component.MenuItem
}

// View returns the menu's view-model, which a screen renders.
func (m Menu) View() component.Menu {
	items := make([]component.MenuItem, len(m.Options))
	for i, o := range m.Options {
		items[i] = o.Item
	}
	return component.Menu{Items: items, Exit: m.Exit}
}

// Accepts returns the inputs the menu's prompt accepts, from which its
// hint is generated: the key of each item shown that can be chosen and
// the exit item's (component.Menu.Bindings), then 0, b and q
// (keys.Navigation), as "0-10, b or q". A disabled item's key is left
// out, so the hint lists only what is accepted ("0-2, 4-10, b or q");
// typed anyway, it is answered with the item's being unavailable.
func (m Menu) Accepts() keys.Set {
	return append(m.View().Bindings(), keys.Navigation()...)
}

// Check returns nil when m keeps the grammar, and else an error wrapping
// ErrMenu that names the first item breaking it:
//   - every item with a label has a key that is a whole number from 1,
//     written as such ("1", not "01" or " 1"), and no other item has it:
//     digits select, each one item;
//   - every item with a label that is not disabled has an Action;
//   - an exit item with a label has the key 0.
//
// An item with no label is not shown and accepts nothing
// (component.Menu), so it is not checked. Choose and Run check the menu
// before they write anything.
func (m Menu) Check() error {
	seen := map[string]bool{}
	for i, o := range m.Options {
		it := o.Item
		if strings.TrimSpace(it.Label) == "" {
			continue
		}
		n, err := strconv.Atoi(it.Key)
		switch {
		case err != nil || n < 1 || strconv.Itoa(n) != it.Key:
			return fmt.Errorf("%w: item %d, %q, has the key %q, not a whole number from 1", ErrMenu, i+1, it.Label, it.Key)
		case seen[it.Key]:
			return fmt.Errorf("%w: item %d, %q, has the key %s, as an item before it does", ErrMenu, i+1, it.Label, it.Key)
		case !it.Disabled && o.Action == nil:
			return fmt.Errorf("%w: item %d, %q, has no action", ErrMenu, i+1, it.Label)
		}
		seen[it.Key] = true
	}
	if strings.TrimSpace(m.Exit.Label) != "" && m.Exit.Key != "0" {
		return fmt.Errorf("%w: the exit item, %q, has the key %q, not 0", ErrMenu, m.Exit.Label, m.Exit.Key)
	}
	return nil
}

// resolve returns the index of the option that line chooses, or ErrBack,
// ErrQuit or the refusal to write. Past the navigation inputs, among them
// 0, the exit item's key, it matches the line against the item bindings
// Accepts generates the hint from, so the menu accepts exactly what its
// hint shows. m has passed Check.
func (m Menu) resolve(line string) (int, error) {
	if err := Navigate(line); err != nil {
		return -1, err
	}
	typed := strings.TrimSpace(line)
	shown := func(it component.MenuItem) bool { return strings.TrimSpace(it.Label) != "" && it.Key == typed }
	if _, ok := m.View().Bindings().Match(typed); ok {
		for i, o := range m.Options {
			if shown(o.Item) && !o.Item.Disabled {
				return i, nil
			}
		}
	}
	for _, o := range m.Options {
		if shown(o.Item) && o.Item.Disabled {
			return -1, refused(fmt.Sprintf(unavailable, strings.TrimSpace(o.Item.Label)))
		}
	}
	return -1, refused(invalidOption)
}

// Choose asks label, such as Figma's "Enter your choice", as a PromptLine
// whose hint is generated from Accepts, and returns the option chosen;
// the caller runs its Action, or Run does. It returns ErrBack for 0 or b
// and ErrQuit for q, and asks again for any other line: "Invalid option",
// or that a disabled item is unavailable. A menu that fails Check returns
// its error before anything is written.
func (p Prompter) Choose(m Menu, label string) (Option, error) {
	if err := m.Check(); err != nil {
		return Option{}, err
	}
	question := component.PromptLine{Label: label, Accepts: m.Accepts()}.Render(p.Ctx)
	var chosen Option
	err := p.ask(question, invalidOption, func(line string) error {
		i, err := m.resolve(line)
		if err == nil {
			chosen = m.Options[i]
		}
		return err
	})
	return chosen, err
}

// Screen is one drawing of a menu screen.
type Screen struct {
	// Above is what the screen shows above the menu, rendered: on the
	// main menu, the header, the status rows and the menu heading. Empty
	// shows the menu alone.
	Above string
	// Menu is the screen's menu, rendered under Above and asked.
	Menu Menu
	// Label is the prompt's question, such as "Enter your choice".
	Label string
}

// Run shows a menu screen and runs what the user chooses until they go
// back or quit. Each round it calls draw, so that the screen shows the
// state the last action left, and writes Above, theme.SectionGap blank
// lines, the menu and theme.PromptGap blank lines; then it asks (Choose)
// and runs the option's Action. The next round draws the screen again
// under what the action wrote, clearing nothing (brief 1C: never wipe a
// result). A line the prompt refuses does not draw the screen again.
//
// Run returns nil when the user goes back with 0 or b: the caller returns
// to the screen above, and at the top level exits. It returns ErrQuit
// when they quit with q, or when an action returns it, so that q quits
// from any depth. An action that returns ErrBack, such as a flow left at
// its first step, returns to this menu. Any other error from an action,
// the prompt or the writer ends Run with that error.
func (p Prompter) Run(draw func() Screen) error {
	for {
		s := draw()
		if err := s.Menu.Check(); err != nil {
			return err
		}
		var parts []string
		for _, part := range []string{s.Above, s.Menu.View().Render(p.Ctx)} {
			if part != "" {
				parts = append(parts, part)
			}
		}
		if len(parts) > 0 {
			out := strings.Join(parts, strings.Repeat("\n", theme.SectionGap+1)) + strings.Repeat("\n", theme.PromptGap+1)
			if _, err := io.WriteString(p.Out, out); err != nil {
				return fmt.Errorf("nav: write: %w", err)
			}
		}
		opt, err := p.Choose(s.Menu, s.Label)
		switch {
		case errors.Is(err, ErrBack):
			return nil
		case err != nil:
			return err
		}
		if err := opt.Action(); err != nil && !errors.Is(err, ErrBack) {
			return err
		}
	}
}
