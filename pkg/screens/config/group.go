package configscreen

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// GroupOptions is what the Configuration group runs with: what cmd/hardn has
// when the main menu's Configuration item (8) is chosen. Each item's screen is
// cmd/hardn's, which assembles it, as the main menu's are (mainmenu.Options).
type GroupOptions struct {
	// Prompter asks and writes the group through the one line reader, rendering
	// with its Ctx.
	Prompter nav.Prompter
	// File is item 1, hardn.yml: the Configuration screen (Run).
	File func() error
	// DNS is item 2: pkg/screens/dns, which edits hardn.yml's list.
	DNS func() error
	// TimeSync is item 3: pkg/screens/timesync.
	TimeSync func() error
	// LogForwarding is item 4: pkg/screens/logforward.
	LogForwarding func() error
}

// RunGroup shows the Configuration group (screen.ConfigGroup) until 0 (nil) or
// q (nav.ErrQuit); the end of the input and Ctrl-C abort (prompt.ErrAborted).
// Each item runs its screen on a cleared screen (nav.Prompter.Open), and the
// group is drawn again on a cleared screen when that returns nil or
// nav.ErrBack; its q, an abort or any other error ends the group
// with it. The group reads nothing, and writes nothing but those clears. RunGroup returns an error
// wrapping fs.ErrInvalid, writing nothing, without a Prompter's In and Out or
// one of the four screens.
func RunGroup(_ context.Context, o GroupOptions) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.File == nil || o.DNS == nil || o.TimeSync == nil || o.LogForwarding == nil {
		return fmt.Errorf("configuration menu: %w: it needs a Prompter with In and Out, and its four screens", fs.ErrInvalid)
	}
	sub := screen.ConfigGroup{}.SubMenu()
	actions := map[string]func() error{
		screen.ConfigFileItem:       o.File,
		screen.ConfigDNSItem:        o.DNS,
		screen.ConfigTimeSyncItem:   o.TimeSync,
		screen.ConfigLogForwardItem: o.LogForwarding,
	}
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		opt := nav.Option{Item: it}
		if run := actions[it.Key]; run != nil {
			opt.Action = func() error { return o.Prompter.Open(run) }
		}
		menu.Options = append(menu.Options, opt)
	}
	return o.Prompter.Run(func() nav.Screen { return sub.Screen(o.Prompter.Ctx, menu) })
}
