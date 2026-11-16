package nav

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// cleared stands for a clear in a transcript (playSub).
const cleared = "<clear>"

// measured is a terminal whose height was measured, at profile.
func measured(profile colorprofile.Profile) term.Env {
	env := term.Fixed(80, profile, false, true)
	env.Plain, env.TTY, env.HeightMeasured = false, true, true
	return env
}

// playSub plays a main menu whose item 1 opens a Firewall sub-menu (Sub):
// its item 1 a question flow's result, written under it, its item 2 a
// sub-menu of its own. It writes through env's Writer, as hardn does, and
// returns the transcript, each clear as cleared, the rest stripped.
func playSub(t *testing.T, env term.Env, lines []string) (string, error) {
	t.Helper()
	raw := new(strings.Builder)
	p := Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(raw, lines...), false), Out: env.Writer(raw)}
	logs := Menu{Exit: component.MenuItem{Key: "0", Label: "Return"}}
	firewall := Menu{Exit: logs.Exit, Options: []Option{
		{Item: component.MenuItem{Key: "1", Label: "Allow"}, Action: func() error {
			_, err := io.WriteString(p.Out, "\n  done\n")
			return err
		}},
		{Item: component.MenuItem{Key: "2", Label: "Logs"}, Action: func() error {
			return p.Sub(func() Screen { return Screen{Above: "  Logs", Menu: logs, Label: choice} })
		}},
	}}
	top := Menu{Options: []Option{{Item: component.MenuItem{Key: "1", Label: "Firewall"}, Action: func() error {
		return p.Sub(func() Screen { return Screen{Above: "  Firewall", Menu: firewall, Label: choice} })
	}}}}
	err := p.Run(func() Screen { return Screen{Above: "  Main", Menu: top, Label: choice} })
	var clear strings.Builder
	_ = env.Clear(&clear)
	out := raw.String()
	if clear.Len() > 0 {
		out = strings.ReplaceAll(out, clear.String(), cleared)
	}
	return ansi.Strip(out), err
}

// The screens playSub draws, stripped.
const (
	subMain     = "  Main\n\n   1) Firewall\n\n    > Option [1, 0 or q]: "
	subFirewall = "  Firewall\n\n   1) Allow\n   2) Logs\n\n   0) Return\n\n    > Option [0-2 or q]: "
	subLogs     = "  Logs\n\n   0) Return\n\n    > Option [0 or q]: "
)

// TestSub checks that a sub-menu opens and closes on a cleared screen on
// a terminal, and that its drawing and the one after its back start on
// row 3, theme.TopRows blank rows under the clear; that a question flow's
// result, q and a Plain Env clear nothing; and at every profile not Plain,
// NoTTY too, that the clear reaches the stream whole.
func TestSub(t *testing.T) {
	top := strings.Repeat("\n", theme.TopRows)
	tests := []struct {
		name  string
		env   term.Env
		lines []string
		want  string
	}{
		{"open, a result, a sub-menu, back twice", measured(colorprofile.TrueColor), []string{"1", "1", "2", "0", "0", "0"},
			subMain + "1\n" + cleared + top + subFirewall + "1\n\n  done\n\n" + subFirewall + "2\n" + cleared + top + subLogs +
				"0\n" + cleared + top + subFirewall + "0\n" + cleared + top + subMain + "0\n"},
		{"q from a sub-menu", measured(colorprofile.ANSI), []string{"1", "2", "q"},
			subMain + "1\n" + cleared + top + subFirewall + "2\n" + cleared + top + subLogs + "q\n"},
		{"plain", term.Fixed(80, colorprofile.NoTTY, false, true), []string{"1", "1", "0", "0"},
			subMain + "1\n\n" + subFirewall + "1\n\n  done\n\n" + subFirewall + "0\n\n" + subMain + "0\n"},
	}
	for _, p := range []colorprofile.Profile{colorprofile.NoTTY, colorprofile.ASCII, colorprofile.ANSI256} {
		tests = append(tests, struct {
			name  string
			env   term.Env
			lines []string
			want  string
		}{"a terminal at " + p.String(), measured(p), []string{"1", "0", "0"},
			subMain + "1\n" + cleared + top + subFirewall + "0\n" + cleared + top + subMain + "0\n"})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := playSub(t, tt.env, tt.lines)
			want := ErrQuit
			if !strings.HasSuffix(tt.want, "q\n") {
				want = nil
			}
			if err != want || out != tt.want {
				t.Errorf("Run() = %v, transcript\n%q\nwant\n%q", err, out, tt.want)
			}
		})
	}
	t.Run("the clear is exactly Clear and the gap", func(t *testing.T) {
		env := measured(colorprofile.TrueColor)
		raw := new(strings.Builder)
		p := Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(raw, "0"), false), Out: env.Writer(raw)}
		_, _ = p.Text(username) // a prompt answered, as one opens every sub-menu
		raw.Reset()
		_ = p.Open(func() error { return nil })
		want := strings.Repeat("\n", env.Height-1) + "\x1b[H" + strings.Repeat("\n", theme.TopRows-theme.SectionGap)
		if got := raw.String(); got != want+want {
			t.Errorf("Open() wrote %q, want %q twice", got, want)
		}
	})
	t.Run("the clear measures the terminal now", func(t *testing.T) {
		env, now := measured(colorprofile.TrueColor), measured(colorprofile.TrueColor)
		env.Height, now.Height = 24, 50
		raw := new(strings.Builder)
		p := Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(raw, "0"), false), Out: env.Writer(raw),
			Terminal: func() term.Env { return now }}
		_, _ = p.Text(username)
		raw.Reset()
		_ = p.Open(func() error { return nil })
		want := strings.Repeat("\n", now.Height-1) + "\x1b[H" + strings.Repeat("\n", theme.TopRows-theme.SectionGap)
		if got := raw.String(); got != want+want {
			t.Errorf("Open() wrote %q, want %q twice, at the window's 50 rows", got, want)
		}
	})
}
