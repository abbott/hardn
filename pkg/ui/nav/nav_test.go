package nav

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// record returns an action that appends name to ran.
func record(ran *[]string, name string) func() error {
	return func() error {
		*ran = append(*ran, name)
		return nil
	}
}

// mainMenu is Figma's main menu (00-TUI-MAIN), each action recording its
// key in ran.
func mainMenu(ran *[]string) Menu {
	items := []component.MenuItem{
		{Key: "1", Label: "User Management", Description: "Create, Configure", Aside: "sudo, SSH keys"},
		{Key: "2", Label: "SSH Login", Description: "Toggle root SSH access"},
		{Key: "3", Label: "DNS", Description: "Configure Nameservers"},
		{Key: "4", Label: "Firewall", Description: "Configure UFW rules"},
		{Key: "5", Label: "Backup", Description: "Configure Hardn backup settings"},
		{Key: "6", Label: "Dry-Run", Description: "Simulate changes"},
		{Key: "7", Label: "Run All", Description: "Execute hardening operations"},
		{Key: "8", Label: "Environment", Description: "Configure environment variable"},
		{Key: "9", Label: "System Details", Description: "View system information"},
		{Key: "10", Label: "Logs", Description: "View log file"},
	}
	m := Menu{Exit: component.MenuItem{Key: "0", Label: "Exit", Description: keys.Set{keys.Quit()}.Help()}}
	for _, it := range items {
		m.Options = append(m.Options, Option{Item: it, Action: record(ran, it.Key)})
	}
	return m
}

// sshMenu is Figma's SSH Login sub-menu (02-SSH_LOGIN) on a Proxmox host,
// where root login stays key-only, so its first item is unavailable.
func sshMenu(ran *[]string) Menu {
	return Menu{Options: []Option{
		{Item: component.MenuItem{Key: "1", Label: "Root SSH Login", Description: "Toggle root SSH access", Aside: "Proxmox", Disabled: true}},
		{Item: component.MenuItem{Key: "2", Label: "View SSH Settings", Description: "Show SSH configuration"}, Action: record(ran, "2")},
	}, Exit: component.MenuItem{Key: "0", Label: "Return"}}
}

// Fixture words: the prompt's label is Figma's; the questions are
// origin/main's (pkg/menu user_options.go), and the typed confirmation's
// label is a stand-in for next/p1c-flow's (P-10).
const (
	choice   = "Enter your choice"
	hostname = "bos-san-l01" // Figma's mock host
)

var (
	username   = Question{Number: 1, Label: "Username"}
	createUser = Question{Label: "Create user 'george'?"}
	typeHost   = Question{Label: "Type the hostname"}
)

// newPrompter returns a Prompter for env whose user types lines, and the
// transcript it writes, the echoed answers included. The reader is not a
// terminal's, so Ask ends each answered line as the terminal's echoed
// Enter would.
func newPrompter(env term.Env, lines []string) (Prompter, *strings.Builder) {
	out := new(strings.Builder)
	return Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(out, lines...), false), Out: out}, out
}

// play runs ask in ASCII at 80 columns with the user typing lines, and
// returns its error and the transcript without escape sequences.
func play(lines []string, ask func(Prompter) error) (string, error) {
	p, out := newPrompter(term.Fixed(80, colorprofile.NoTTY, false, true), lines)
	err := ask(p)
	return ansi.Strip(out.String()), err
}

// The prompts of the grammar, as the tests ask them.
var (
	chooseMain = func(p Prompter) error { _, err := p.Choose(mainMenu(new([]string)), choice); return err }
	chooseSSH  = func(p Prompter) error { _, err := p.Choose(sshMenu(new([]string)), choice); return err }
	confirm    = func(p Prompter) error { _, err := p.Confirm(createUser, false); return err }
	typed      = func(p Prompter) error { return p.Typed(typeHost, hostname) }
	text       = func(p Prompter) error { _, err := p.Text(username); return err }
)

// TestGrammar checks that the navigation inputs do the same at every
// prompt, and that every hint ends with them alike.
func TestGrammar(t *testing.T) {
	prompts := []struct {
		name string
		ask  func(Prompter) error
		hint string
	}{
		{"main menu", chooseMain, "[0-10 or q]:"},
		{"sub-menu", chooseSSH, "[0, 2 or q]:"},
		{"y/N question", confirm, "[y/N, 0 or q]:"},
		{"typed confirmation", typed, "[bos-san-l01, 0 or q]:"},
		{"free text", text, "[0 or q]:"},
	}
	inputs := []struct {
		lines []string
		want  error
	}{
		{[]string{"0"}, ErrBack},
		{[]string{" 0\t"}, ErrBack},
		{[]string{"q"}, ErrQuit},
		{[]string{"Q\t"}, ErrQuit},
		{nil, prompt.ErrEndOfInput}, // the end of the input aborts: never back
	}
	for _, pr := range prompts {
		for _, in := range inputs {
			out, err := play(in.lines, pr.ask)
			if !errors.Is(err, in.want) {
				t.Errorf("%s: %q = %v, want %v", pr.name, in.lines, err, in.want)
			}
			if first, _, _ := strings.Cut(out, "\n"); !strings.Contains(first, pr.hint+" ") {
				t.Errorf("%s: the prompt %q does not end with the hint %q", pr.name, first, pr.hint)
			}
		}
	}
}

// TestBIsALine checks that b, which brief 1C had go back as 0 does, is an
// ordinary line: back is 0 only, by the maintainer's decision of
// 2026-10-17 (P-25). A menu and a y/N question refuse it as any input
// they do not take, a typed confirmation compares it with the phrase, and
// free text takes it as a value.
func TestBIsALine(t *testing.T) {
	prompts := []struct {
		name    string
		ask     func(Prompter) error
		refusal string
	}{
		{"main menu", chooseMain, InvalidOption},
		{"sub-menu", chooseSSH, InvalidOption},
		{"y/N question", confirm, InvalidAnswer},
		{"typed confirmation", typed, noMatch},
	}
	for _, pr := range prompts {
		for _, b := range []string{"b", " B "} {
			out, err := play([]string{b, "0"}, pr.ask)
			if !errors.Is(err, ErrBack) || strings.Count(out, pr.refusal) != 1 {
				t.Errorf("%s: %q, then 0: %v, %d refusals; want ErrBack after 1 refusal", pr.name, b, err, strings.Count(out, pr.refusal))
			}
		}
	}
	var got string
	if _, err := play([]string{" b "}, func(p Prompter) error {
		var err error
		got, err = p.Text(username)
		return err
	}); got != "b" || err != nil {
		t.Errorf("Text(%q) = %q, %v; want b, nil", " b ", got, err)
	}
}

func TestNavigate(t *testing.T) {
	for line, want := range map[string]error{
		"0": ErrBack, " 0 ": ErrBack, "\tq": ErrQuit, "Q": ErrQuit,
		"": nil, "00": nil, "10": nil, "b": nil, " B ": nil, "bq": nil, "back": nil, "quit": nil, "q q": nil,
	} {
		if err := Navigate(line); err != want {
			t.Errorf("Navigate(%q) = %v, want %v", line, err, want)
		}
	}
}

func TestCheckPhrase(t *testing.T) {
	for phrase, ok := range map[string]bool{
		"bos-san-l01": true, "web-01.example.net": true, "BOS-SAN-L01": true, "2230": true, "disable root": true,
		"": false, "0": false, "q": false, "Q": false, "web,01": false, "web/01": false, "[web]": false, "web-01 or 02": false,
		"b": true, "B": true, // back is 0 only (P-25)
		"order": true, " web-01": false, "web-01 ": false,
		"wéb-01": false, "web\x1b[1m": false, "‘root’": false, "web\t01": false,
	} {
		err := CheckPhrase(phrase)
		if (err == nil) != ok || err != nil && !errors.Is(err, ErrPhrase) {
			t.Errorf("CheckPhrase(%q) = %v, want ok %v", phrase, err, ok)
		}
	}
}

func TestCheck(t *testing.T) {
	act := func() error { return nil }
	item := func(key, label string) Option {
		return Option{Item: component.MenuItem{Key: key, Label: label}, Action: act}
	}
	tests := []struct {
		name string
		menu Menu
		ok   bool
	}{
		{"Figma's main menu", mainMenu(new([]string)), true},
		{"a disabled item needs no action", sshMenu(new([]string)), true},
		{"no options", Menu{}, true},
		{"an item with no label is not shown", Menu{Options: []Option{{Item: component.MenuItem{Key: "x"}}}}, true},
		{"nor one whose label is invisible", Menu{Options: []Option{{Item: component.MenuItem{Label: "\u200b"}}}}, true},
		{"nor an exit item whose label is an escape sequence", Menu{Exit: component.MenuItem{Key: "9", Label: "\x1b[0m"}}, true},
		{"a key that is not a number", Menu{Options: []Option{item("a", "Add")}}, false},
		{"a key that navigates", Menu{Options: []Option{item("q", "Quit")}}, false},
		{"a key not in canonical form", Menu{Options: []Option{item("01", "DNS")}}, false},
		{"a key with spaces", Menu{Options: []Option{item(" 1", "DNS")}}, false},
		{"an item numbered 0", Menu{Options: []Option{item("0", "DNS")}}, false},
		{"no key", Menu{Options: []Option{item("", "DNS")}}, false},
		{"a key used twice", Menu{Options: []Option{item("1", "DNS"), item("1", "Logs")}}, false},
		{"an item with no action", Menu{Options: []Option{{Item: component.MenuItem{Key: "1", Label: "DNS"}}}}, false},
		{"an exit item numbered 9", Menu{Exit: component.MenuItem{Key: "9", Label: "Exit"}}, false},
		{"an exit item with no key", Menu{Exit: component.MenuItem{Label: "Return"}}, false},
		{"no exit item", Menu{Options: []Option{item("1", "DNS")}}, true},
	}
	for _, tt := range tests {
		err := tt.menu.Check()
		if (err == nil) != tt.ok || err != nil && !errors.Is(err, ErrMenu) {
			t.Errorf("%s: Check() = %v, want ok %v", tt.name, err, tt.ok)
		}
	}
}

func TestResolve(t *testing.T) {
	main, ssh := mainMenu(new([]string)), sshMenu(new([]string))
	// hidden has an action under a key it does not show: origin/main's
	// hidden options (interaction.md §1 item 4) must stay unreachable.
	hidden := Menu{Options: []Option{{Item: component.MenuItem{Key: "4"}, Action: func() error { return nil }}}}
	tests := []struct {
		name string
		menu Menu
		line string
		want int   // the option chosen, when err is nil
		err  error // ErrBack, ErrQuit or a refusal
	}{
		{"a number", main, "1", 0, nil},
		{"a two-digit number", main, "10", 9, nil},
		{"white space around it", main, " 7\t", 6, nil},
		{"the exit item", main, "0", 0, ErrBack},
		{"b is no input", main, "b", 0, Refuse(InvalidOption)},
		{"q", main, "Q", 0, ErrQuit},
		{"a number not offered", main, "11", 0, Refuse(InvalidOption)},
		{"a number not in canonical form", main, "01", 0, Refuse(InvalidOption)},
		{"an empty line", main, "", 0, Refuse(InvalidOption)},
		{"a word", main, "dns", 0, Refuse(InvalidOption)},
		{"a disabled item", ssh, "1", 0, Refuse("Root SSH Login is unavailable.")},
		{"the item after it", ssh, "2", 1, nil},
		{"Return", ssh, "0", 0, ErrBack},
		{"an item not shown", hidden, "4", 0, Refuse(InvalidOption)},
		{"a disabled item not shown", Menu{Options: []Option{{Item: component.MenuItem{Key: "1", Label: "\x1b[0m", Disabled: true}}}}, "1", 0, Refuse(InvalidOption)},
		{"0 with no exit item", hidden, "0", 0, ErrBack},
	}
	for _, tt := range tests {
		got, err := tt.menu.resolve(tt.line)
		if err != tt.err || err == nil && got != tt.want {
			t.Errorf("%s: resolve(%q) = %d, %v; want %d, %v", tt.name, tt.line, got, err, tt.want, tt.err)
		}
	}
}

func TestChoose(t *testing.T) {
	var ran []string
	var chosen Option
	out, err := play([]string{"11", "3"}, func(p Prompter) error {
		var err error
		chosen, err = p.Choose(mainMenu(&ran), choice)
		return err
	})
	want := "  > Enter your choice [0-10 or q]: 11\n" +
		"  (x) Invalid option. Please try again.\n" +
		"  > Enter your choice [0-10 or q]: 3\n"
	if err != nil || chosen.Item.Label != "DNS" || out != want {
		t.Errorf("Choose() = %q, %v, transcript\n%s\nwant DNS, nil, transcript\n%s", chosen.Item.Label, err, out, want)
	}
	if len(ran) != 0 {
		t.Errorf("Choose ran %q; it only chooses", ran)
	}
	t.Run("a disabled item", func(t *testing.T) {
		out, _ := play([]string{"1", "0"}, chooseSSH)
		if !strings.Contains(out, "  (x) Root SSH Login is unavailable.\n") {
			t.Errorf("transcript\n%s\nsays nothing of the item", out)
		}
	})
	t.Run("a line too long", func(t *testing.T) {
		out, err := play([]string{strings.Repeat("1", prompt.MaxLine+1), "0"}, chooseMain)
		if !errors.Is(err, ErrBack) || strings.Count(out, InvalidOption) != 1 {
			t.Errorf("Choose() = %v, %d refusals; want ErrBack, 1", err, strings.Count(out, InvalidOption))
		}
	})
	t.Run("a menu that breaks the grammar writes nothing", func(t *testing.T) {
		out, err := play([]string{"1"}, func(p Prompter) error {
			_, err := p.Choose(Menu{Options: []Option{{Item: component.MenuItem{Key: "1", Label: "DNS"}}}}, choice)
			return err
		})
		if !errors.Is(err, ErrMenu) || out != "" {
			t.Errorf("Choose() = %v, wrote %q; want ErrMenu, nothing", err, out)
		}
	})
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		lines      []string
		defaultYes bool
		want       bool
		err        error
		refusals   int
	}{
		{[]string{"y"}, false, true, nil, 0},
		{[]string{"YES"}, false, true, nil, 0},
		{[]string{" no "}, true, false, nil, 0},
		{[]string{"N"}, true, false, nil, 0},
		{[]string{""}, false, false, nil, 0},
		{[]string{"  "}, true, true, nil, 0},
		{[]string{"maybe", "y"}, false, true, nil, 1},
		{[]string{"1", "nope", "n"}, false, false, nil, 2},
		{[]string{"0"}, false, false, ErrBack, 0}, // never "no"
		{[]string{"yes please", "b", "0"}, false, false, ErrBack, 2},
		{[]string{"q"}, true, false, ErrQuit, 0},
		{nil, true, false, prompt.ErrEndOfInput, 0},
	}
	for _, tt := range tests {
		var yes bool
		out, err := play(tt.lines, func(p Prompter) error {
			var err error
			yes, err = p.Confirm(createUser, tt.defaultYes)
			return err
		})
		if yes != tt.want || !errors.Is(err, tt.err) || strings.Count(out, InvalidAnswer) != tt.refusals {
			t.Errorf("Confirm(%q, default yes %v) = %v, %v, %d refusals; want %v, %v, %d",
				tt.lines, tt.defaultYes, yes, err, strings.Count(out, InvalidAnswer), tt.want, tt.err, tt.refusals)
		}
	}
	out, _ := play([]string{"y"}, func(p Prompter) error { _, err := p.Confirm(createUser, true); return err })
	if want := "  Create user 'george'? [Y/n, 0 or q]: y\n"; out != want {
		t.Errorf("transcript %q, want %q", out, want)
	}
}

func TestTyped(t *testing.T) {
	mismatch := "does not match exactly"
	tests := []struct {
		lines    []string
		err      error
		refusals int
	}{
		{[]string{hostname}, nil, 0},
		{[]string{" " + hostname + "\t"}, nil, 0},
		{[]string{"BOS-SAN-L01", hostname}, nil, 1},
		{[]string{"", "y", "yes", hostname}, nil, 3},
		{[]string{"bos-san", "B", "0"}, ErrBack, 2},
		{[]string{"q"}, ErrQuit, 0},
		{nil, prompt.ErrEndOfInput, 0},
	}
	for _, tt := range tests {
		out, err := play(tt.lines, typed)
		if !errors.Is(err, tt.err) || strings.Count(out, mismatch) != tt.refusals {
			t.Errorf("Typed(%q) = %v, %d refusals; want %v, %d", tt.lines, err, strings.Count(out, mismatch), tt.err, tt.refusals)
		}
	}
	out, _ := play([]string{hostname}, typed)
	if want := "  Type the hostname [bos-san-l01, 0 or q]: bos-san-l01\n"; out != want {
		t.Errorf("transcript %q, want %q", out, want)
	}
	// b is a phrase: back is 0 only (P-25).
	out, err := play([]string{"B", "b"}, func(p Prompter) error { return p.Typed(typeHost, "b") })
	if want := "  Type the hostname [b, 0 or q]: B\n"; err != nil || strings.Count(out, mismatch) != 1 || !strings.HasPrefix(out, want) {
		t.Errorf("Typed(phrase b) = %v, transcript %q; want nil, 1 refusal, starting %q", err, out, want)
	}
	for _, phrase := range []string{"q", "Q", "0", ""} {
		out, err := play([]string{phrase}, func(p Prompter) error { return p.Typed(typeHost, phrase) })
		if !errors.Is(err, ErrPhrase) || out != "" {
			t.Errorf("Typed(phrase %q) = %v, wrote %q; want ErrPhrase, nothing", phrase, err, out)
		}
		if err := (Prompter{}).CheckTyped(typeHost, phrase); !errors.Is(err, ErrPhrase) {
			t.Errorf("CheckTyped(phrase %q) = %v, want ErrPhrase", phrase, err)
		}
	}
}

// TestTypedWhole checks that a typed confirmation shows its phrase whole,
// after some of its label, or refuses it before writing anything, at
// every width and in both glyph sets, and the longest phrase it takes;
// and that CheckTyped, which writes nothing, refuses exactly what Typed
// refuses.
func TestTypedWhole(t *testing.T) {
	tests := []struct {
		width   int
		unicode bool
		longest int // 14 cells of hint around the phrase and the indent, and the label cut to a letter and the ellipsis
	}{
		{40, true, 24}, {60, true, 44}, {64, true, 48}, {80, true, 48}, {120, true, 48},
		{40, false, 22}, {60, false, 42}, {64, false, 46}, {80, false, 46},
	}
	for _, tt := range tests {
		for n := 1; n <= 64; n++ {
			phrase := strings.Repeat("h", n)
			p, out := newPrompter(term.Fixed(tt.width, colorprofile.ASCII, tt.unicode, true), []string{phrase})
			checked := p.CheckTyped(typeHost, phrase)
			if out.Len() != 0 {
				t.Errorf("%d columns, unicode %v, %d cells: CheckTyped() wrote %q, want nothing", tt.width, tt.unicode, n, out)
			}
			err := p.Typed(typeHost, phrase)
			if (checked == nil) != (err == nil) {
				t.Errorf("%d columns, unicode %v, %d cells: CheckTyped() = %v, Typed() = %v; want both nil or both ErrPhrase", tt.width, tt.unicode, n, checked, err)
			}
			first, _, _ := strings.Cut(ansi.Strip(out.String()), "\n")
			switch {
			case n > tt.longest:
				if !errors.Is(err, ErrPhrase) || out.Len() != 0 {
					t.Errorf("%d columns, unicode %v, %d cells: Typed() = %v, wrote %q; want ErrPhrase, nothing", tt.width, tt.unicode, n, err, out)
				}
			case err != nil || !strings.HasPrefix(first, "  T") || !strings.HasSuffix(first, " ["+phrase+", 0 or q]: "+phrase):
				t.Errorf("%d columns, unicode %v, %d cells: Typed() = %v, prompt %q; want the phrase whole after the label", tt.width, tt.unicode, n, err, first)
			}
		}
	}
}

func TestNoLabel(t *testing.T) {
	out, err := play([]string{"y"}, func(p Prompter) error { _, err := p.Confirm(Question{}, false); return err })
	if err == nil || out != "" {
		t.Errorf("Confirm(no label) = %v, wrote %q; want an error, nothing", err, out)
	}
}

func TestText(t *testing.T) {
	tests := []struct {
		lines []string
		want  string
		err   error
	}{
		{[]string{" george "}, "george", nil},
		{[]string{""}, "", nil},
		{[]string{"0"}, "", ErrBack},
		{[]string{"bob"}, "bob", nil},
		{[]string{"b"}, "b", nil}, // back is 0 only (P-25)
		{[]string{"Q"}, "", ErrQuit},
		{nil, "", prompt.ErrEndOfInput},
	}
	for _, tt := range tests {
		var got string
		_, err := play(tt.lines, func(p Prompter) error {
			var err error
			got, err = p.Text(username)
			return err
		})
		if got != tt.want || !errors.Is(err, tt.err) {
			t.Errorf("Text(%q) = %q, %v; want %q, %v", tt.lines, got, err, tt.want, tt.err)
		}
	}
	out, _ := play([]string{"george"}, text)
	if want := "   1. Username [0 or q]: george\n"; out != want {
		t.Errorf("transcript %q, want %q", out, want)
	}
}

// TestAsk checks the loop other packages ask with: a reason from Refuse
// is written under the prompt, and the prompt again; a reason that shows
// nothing, and a line longer than prompt.MaxLine, are refused for the
// reason invalid.
func TestAsk(t *testing.T) {
	question := component.PromptLine{Accepts: keys.Navigation()}
	resolve := func(line string) error {
		switch line {
		case "a b":
			return Refuse("Username cannot contain spaces") // origin/main's
		case "blank":
			return Refuse("\x1b[0m ")
		}
		return Navigate(line)
	}
	long := strings.Repeat("x", prompt.MaxLine+1)
	out, err := play([]string{"a b", "blank", long, "george"}, func(p Prompter) error {
		return p.Ask(question.Render(p.Ctx), InvalidAnswer, resolve)
	})
	out = strings.ReplaceAll(out, long, "<long line>")
	want := "  > [0 or q]: a b\n" +
		"  (x) Username cannot contain spaces\n" +
		"  > [0 or q]: blank\n" +
		"  (x) " + InvalidAnswer + "\n" +
		"  > [0 or q]: <long line>\n" +
		"  (x) " + InvalidAnswer + "\n" +
		"  > [0 or q]: george\n"
	if err != nil || out != want {
		t.Errorf("Ask: %v, transcript\n%s\nwant nil and\n%s", err, out, want)
	}
}

// TestAskKinds checks the kind a refusal is written in, read in colour:
// Refuse's reason as a failed InlineResult, Warn's as a warning one; and
// RefuseFor, which keeps the refusal an error holds and makes any other
// error a failed one in its words.
func TestAskKinds(t *testing.T) {
	env := term.Fixed(80, colorprofile.TrueColor, true, true)
	const twice = "Nameserver 192.0.2.54 is already configured" // origin/main's warning
	for _, tc := range []struct {
		name string
		err  error
		kind component.MessageKind
		text string
	}{
		{"Refuse", Refuse(InvalidAnswer), component.MessageBad, InvalidAnswer},
		{"Warn", Warn(twice), component.MessageWarn, twice},
		{"RefuseFor an error", RefuseFor(errors.New("Invalid IP address format")), component.MessageBad, "Invalid IP address format"},
		{"RefuseFor a warning", RefuseFor(fmt.Errorf("dns: %w", Warn(twice))), component.MessageWarn, twice},
	} {
		p, out := newPrompter(env, []string{"x", "george"})
		err := p.Ask("  > [0 or q]: ", InvalidAnswer, func(line string) error {
			if line == "x" {
				return tc.err
			}
			return nil
		})
		want := component.InlineResult{Kind: tc.kind, Text: tc.text}.Render(p.Ctx) + "\n"
		if err != nil || strings.Count(out.String(), want) != 1 || tc.err.Error() != tc.text {
			t.Errorf("%s: Ask: %v, transcript\n%q\nwant once\n%q", tc.name, err, out.String(), want)
		}
	}
}

func TestRun(t *testing.T) {
	// run plays the main menu, whose item 1 opens the SSH sub-menu, and
	// returns Run's error, what ran, and how often each screen was drawn.
	run := func(lines []string, action func() error) (err error, ran []string, mains, subs int) {
		_, err = play(lines, func(p Prompter) error {
			sub := func() Screen { subs++; return Screen{Menu: sshMenu(&ran), Label: choice} }
			return p.Run(func() Screen {
				mains++
				m := mainMenu(&ran)
				m.Options[0].Action = func() error { ran = append(ran, "1"); return p.Run(sub) }
				if action != nil {
					m.Options[2].Action = action
				}
				return Screen{Above: "Select an option", Menu: m, Label: choice}
			})
		})
		return err, ran, mains, subs
	}
	tests := []struct {
		name        string
		lines       []string
		action      func() error // item 3's
		err         error
		ran         string
		mains, subs int
	}{
		{"back at the top level exits", []string{"0"}, nil, nil, "", 1, 0},
		{"b is refused, not back", []string{"b", "0"}, nil, nil, "", 1, 0},
		{"q quits", []string{"q"}, nil, ErrQuit, "", 1, 0},
		{"a refused line draws nothing", []string{"11", "x", "0"}, nil, nil, "", 1, 0},
		{"an action runs, and the screen is drawn again", []string{"4", "5", "0"}, nil, nil, "4 5", 3, 0},
		{"back from a sub-menu returns to the menu", []string{"1", "2", "0", "4", "q"}, nil, ErrQuit, "1 2 4", 3, 2},
		{"b at a sub-menu stays there", []string{"1", "b", "2", "0", "q"}, nil, ErrQuit, "1 2", 2, 2},
		{"q quits from a sub-menu", []string{"1", "q"}, nil, ErrQuit, "1", 1, 1},
		{"an action that went back returns to the menu", []string{"3", "0"}, func() error { return ErrBack }, nil, "", 2, 0},
		{"an action that quit quits", []string{"3", "0"}, func() error { return ErrQuit }, ErrQuit, "", 1, 0},
		{"an action's error ends Run", []string{"3", "0"}, func() error { return io.ErrClosedPipe }, io.ErrClosedPipe, "", 1, 0},
		{"the end of the input aborts", []string{"1"}, nil, prompt.ErrEndOfInput, "1", 1, 1},
	}
	for _, tt := range tests {
		err, ran, mains, subs := run(tt.lines, tt.action)
		if !errors.Is(err, tt.err) || strings.Join(ran, " ") != tt.ran || mains != tt.mains || subs != tt.subs {
			t.Errorf("%s: Run() = %v, ran %q, drawn %d and %d times; want %v, %q, %d and %d",
				tt.name, err, ran, mains, subs, tt.err, tt.ran, tt.mains, tt.subs)
		}
	}
	t.Run("a menu that breaks the grammar draws nothing", func(t *testing.T) {
		out, err := play([]string{"1"}, func(p Prompter) error {
			return p.Run(func() Screen {
				return Screen{Above: "Select an option", Menu: Menu{Exit: component.MenuItem{Key: "9", Label: "Exit"}}}
			})
		})
		if !errors.Is(err, ErrMenu) || out != "" {
			t.Errorf("Run() = %v, wrote %q; want ErrMenu, nothing", err, out)
		}
	})
}

// TestRunGap checks that each drawing but the first stands apart from what
// is above it: a sub-menu from the answer that opened it, and the menu
// drawn again from what an action wrote or from a sub-menu's last answer.
func TestRunGap(t *testing.T) {
	out, err := play([]string{"1", "2", "0", "0"}, func(p Prompter) error {
		sub := Menu{Options: []Option{{Item: component.MenuItem{Key: "1", Label: "DNS"}, Action: func() error { return nil }}}}
		sub.Options = append(sub.Options, Option{Item: component.MenuItem{Key: "2", Label: "Logs"}, Action: func() error {
			_, err := io.WriteString(p.Out, "\n  done\n")
			return err
		}})
		top := Menu{Options: []Option{{Item: component.MenuItem{Key: "1", Label: "Firewall"}, Action: func() error {
			return p.Run(func() Screen { return Screen{Above: "  Firewall", Menu: sub, Label: choice} })
		}}}}
		return p.Run(func() Screen { return Screen{Above: "  Main", Menu: top, Label: choice} })
	})
	main := "  Main\n\n   1) Firewall\n\n  > Enter your choice [1, 0 or q]: "
	firewall := "  Firewall\n\n   1) DNS\n   2) Logs\n\n  > Enter your choice [1-2, 0 or q]: "
	want := main + "1\n\n" + firewall + "2\n\n  done\n\n" + firewall + "0\n\n" + main + "0\n"
	if err != nil || out != want {
		t.Errorf("Run() = %v, transcript\n%s\nwant\n%s", err, out, want)
	}
}

// TestRunCompact checks that a compact screen draws its menu in two
// columns (component.Menu.Compact), on the line under Above with no blank
// line between, and still accepts every item.
func TestRunCompact(t *testing.T) {
	var ran []string
	out, err := play([]string{"10", "0"}, func(p Prompter) error {
		return p.Run(func() Screen {
			return Screen{Above: "  Select an option", Menu: mainMenu(&ran), Label: choice, Compact: true}
		})
	})
	view := mainMenu(nil).View()
	view.Compact = true
	drawing := ansi.Strip(view.Render(component.NewCtx(term.Fixed(80, colorprofile.NoTTY, false, true))))
	want := "  Select an option\n" + drawing + "\n\n  > Enter your choice [0-10 or q]: "
	if err != nil || !strings.HasPrefix(out, want+"10\n") || strings.Join(ran, " ") != "10" ||
		!strings.Contains(drawing, "   5) Backup               10) Logs") {
		t.Errorf("Run() = %v, ran %q, transcript\n%s\nwant it to start\n%s", err, ran, out, want)
	}
}

// failing fails every Write.
type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestWriteFails(t *testing.T) {
	p, _ := newPrompter(term.Fixed(80, colorprofile.NoTTY, false, true), []string{"x", "y"})
	p.Out = failing{}
	if _, err := p.Confirm(createUser, false); !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("Confirm() = %v, want the write's error", err)
	}
	if err := p.Run(func() Screen { return Screen{Menu: mainMenu(new([]string)), Label: choice} }); !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("Run() = %v, want the write's error", err)
	}
}

// TestGolden records the grammar on the core set (brief Phase 6): a main
// menu, a sub-menu, a workflow step and a typed confirmation, each asked
// a line it refuses, then left with 0 (back, which exits at the top) or q
// (quit), so that both are on record at the menus and the workflow step
// alike; and rounds of the main menu, which show 0 on a sub-menu, q on
// the main menu and the gap before each drawing. The answers are echoed
// as a terminal shows them.
func TestGolden(t *testing.T) {
	heading := func(p Prompter, mode bool) string {
		h := component.MenuHeading{Title: "Select an option"}
		if mode {
			h.Mode = &component.ModeIndicator{}
		}
		return h.Render(p.Ctx)
	}
	tests := []struct {
		name  string
		lines []string
		want  error
		ask   func(Prompter) error
	}{
		{"main-menu", []string{"11", "0"}, nil, func(p Prompter) error {
			return p.Run(func() Screen { return Screen{Above: heading(p, true), Menu: mainMenu(new([]string)), Label: choice} })
		}},
		{"sub-menu", []string{"1", "q"}, ErrQuit, func(p Prompter) error {
			return p.Run(func() Screen { return Screen{Above: heading(p, false), Menu: sshMenu(new([]string)), Label: choice} })
		}},
		{"workflow-step", []string{"george", "maybe", "0"}, ErrBack, func(p Prompter) error {
			if _, err := p.Text(username); err != nil {
				return err
			}
			_, err := p.Confirm(createUser, false)
			return err
		}},
		{"typed-confirmation", []string{"BOS-SAN-L01", "q"}, ErrQuit, typed},
		// rounds: an action's result, then the menu again; the SSH Login
		// sub-menu (item 2), left with 0; then q from the main menu.
		{"rounds", []string{"3", "2", "0", "q"}, ErrQuit, func(p Prompter) error {
			sub := func() Screen { return Screen{Above: heading(p, false), Menu: sshMenu(new([]string)), Label: choice} }
			return p.Run(func() Screen {
				m := mainMenu(new([]string))
				m.Options[1].Action = func() error { return p.Run(sub) }
				m.Options[2].Action = func() error {
					done := component.Message{Kind: component.MessageOK, Text: "DNS configured successfully"}.Render(p.Ctx)
					_, err := io.WriteString(p.Out, strings.Repeat("\n", theme.SectionGap)+done+"\n")
					return err
				}
				return Screen{Above: heading(p, true), Menu: m, Label: choice}
			})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uitest.Golden(t, "nav/"+tt.name, uitest.Core(), func(env term.Env) string {
				p, out := newPrompter(env, tt.lines)
				if err := tt.ask(p); !errors.Is(err, tt.want) {
					t.Errorf("%s: %v, want %v", env.Profile, err, tt.want)
				}
				return strings.TrimSuffix(out.String(), "\n")
			})
		})
	}
}
