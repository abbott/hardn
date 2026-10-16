// Command uigallery is hardn's dev-only design-system gallery. It renders
// the states of pkg/ui/component, the ones the component goldens pin
// (pkg/ui/component/fixture.States), in a render environment chosen by
// flags, so that the maintainer can review the components and try new
// colours without editing code (brief 1D).
//
// It is never part of a release: every build target of the makefile, the
// goreleaser configs and the workflows builds ./cmd/hardn alone, and a
// main package cannot be imported. Run it from the repository root:
//
//	go run ./internal/tools/uigallery [flags] [component | component/state | all]...
//
// With no argument, or "all", it renders every state; "menu" renders every
// state of Menu and "menu/compact" one. States come out in the catalogue's
// order, each under a header with its whole name, which on a terminal too
// narrow for the name runs past its width. Flags:
//
//	-list             print the names of the states it would render, and exit
//	-theme dark|light the palette (default dark)
//	-profile name     truecolor, 256, 16, none (no colour; bold and
//	                  underline kept) or plain (no escape sequences, ASCII
//	                  glyphs) (default truecolor)
//	-ascii            the ASCII glyph set
//	-width N          the terminal width in cells (default 80)
//	-matrix           render each state in every cell of the golden matrix,
//	                  uitest.Full's 48, then plain mode at 60, 80 and 120,
//	                  each under its cell's name, in place of -theme,
//	                  -profile, -ascii and -width
//	-override FILE    a colour override file (JSON; see
//	                  uigallery.local.json.example)
//
// The override file maps token names to new values for each palette and
// is applied through theme.NewWithOverrides, which only this package may
// call (theme's TestOverrideReach). Copy the example to
// uigallery.local.json, which the repository's allow-list .gitignore
// keeps out of git, and pass its path.
//
// The gallery's code reads nothing but its flags and the override file:
// no environment variable and no terminal query (it builds each
// environment with term.Fixed, never term.Detect), no root, nothing under
// /etc. The package initialisation of Lip Gloss and x/ansi still runs, as
// in every binary that links Lip Gloss (today the pkg/ui test binaries
// and this gallery; cmd/hardn links none): an ioctl on stdout and, on a
// terminal, a read-only terminfo lookup and "tmux info" under tmux, and
// x/ansi's read of RUNEWIDTH_EASTASIAN
// (docs/refactor/phase0/side-effects.md §13.1). The gallery ignores the
// profile Lip Gloss detects. A true RUNEWIDTH_EASTASIAN makes x/ansi
// measure ambiguous-width glyphs such as "─" as two cells, which moves
// every layout off its golden, so the gallery then refuses to render
// (exit status 1), as the golden harness refuses to run. It writes to
// stdout only, through each environment's term.Env.Writer, one write per
// state and environment: what the state's golden file holds for the same
// cell.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/component/fixture"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// Exit statuses.
const (
	exitOK    = 0
	exitError = 1 // a write or the override file failed, or RUNEWIDTH_EASTASIAN is true
	exitUsage = 2 // a flag or an argument is wrong
)

// profile is one value of -profile: its name and colour profile.
type profile struct {
	name    string
	profile colorprofile.Profile
}

// profiles are -profile's values: the golden matrix's colour axis
// (uitest.Full) and plain mode (uitest.Core).
var profiles = []profile{
	{"truecolor", colorprofile.TrueColor},
	{"256", colorprofile.ANSI256},
	{"16", colorprofile.ANSI},
	{"none", colorprofile.ASCII},
	{"plain", colorprofile.NoTTY},
}

// cell is one render environment and the name it is shown under.
type cell struct {
	name string
	env  term.Env
}

// newCell returns the environment the flags describe, named as the
// golden matrix names its cells: "truecolor-dark-unicode-80", or
// "plain-80" for plain mode, where term.Fixed always uses ASCII glyphs.
func newCell(p profile, dark, unicode bool, width int) cell {
	env := term.Fixed(width, p.profile, unicode, dark)
	if env.Plain {
		return cell{fmt.Sprintf("plain-%d", width), env}
	}
	bg, glyphs := "light", "ascii"
	if dark {
		bg = "dark"
	}
	if unicode {
		glyphs = "unicode"
	}
	return cell{fmt.Sprintf("%s-%s-%s-%d", p.name, bg, glyphs, width), env}
}

// matrix returns -matrix's cells: uitest.Full's 48, in its order, then
// plain mode at each width.
func matrix() []cell {
	widths := []int{60, 80, 120}
	var cells []cell
	for _, p := range profiles[:4] {
		for _, dark := range []bool{true, false} {
			for _, unicode := range []bool{true, false} {
				for _, w := range widths {
					cells = append(cells, newCell(p, dark, unicode, w))
				}
			}
		}
	}
	for _, w := range widths {
		cells = append(cells, newCell(profiles[4], true, false, w))
	}
	return cells
}

// catalogue returns the states the gallery shows: fixture.States, the
// component goldens' states.
func catalogue() []fixture.State {
	return fixture.States()
}

// selectStates returns the states args name, in the catalogue's order:
// every state for no argument or "all", a component's states for its
// name, and one state for its full name. An argument that starts with
// "-" is a flag the flag package left unparsed, after a name.
func selectStates(states []fixture.State, args []string) ([]fixture.State, error) {
	if len(args) == 0 || slices.Contains(args, "all") {
		return states, nil
	}
	want := map[string]bool{}
	for _, a := range args {
		found := false
		for _, s := range states {
			if s.Name == a || strings.HasPrefix(s.Name, a+"/") {
				want[s.Name], found = true, true
			}
		}
		switch {
		case !found && strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("flag %s comes after a component or state name; flags go before them", a)
		case !found:
			return nil, fmt.Errorf("no component or state %q; -list shows them", a)
		}
	}
	var out []fixture.State
	for _, s := range states {
		if want[s.Name] {
			out = append(out, s)
		}
	}
	return out, nil
}

// gallery renders states with an optional colour override.
type gallery struct {
	colours *overrideFile // nil: the palettes as they are
}

// ctx returns the render context for env, with the override's values for
// env's palette.
func (g gallery) ctx(env term.Env) (component.Ctx, error) {
	if g.colours == nil {
		return component.NewCtx(env), nil
	}
	th, err := theme.NewWithOverrides(env, g.colours.forPalette(env.Dark))
	if err != nil {
		return component.Ctx{}, err
	}
	return component.Ctx{Env: env, Theme: th}, nil
}

// headerFrame is the cells a SectionHeader adds to its title: the stub, a
// cell either side of the title and one cell of rule.
const headerFrame = 4

// header returns the gallery's header for title in ctx: a SectionHeader.
// On a terminal too narrow for the whole title it is rendered as if the
// terminal were just wide enough, so that no state's name is cut and
// states stay told apart at any width; that line, the gallery's chrome
// and no component's output, then runs past the terminal's width. The
// content width (theme.ContentWidth) still caps it, which every title
// fits (TestNarrow).
func header(ctx component.Ctx, title string) string {
	ctx.Env.Width = max(ctx.Env.Width, ansi.StringWidth(title)+headerFrame)
	return component.SectionHeader{Title: title}.Render(ctx)
}

// render returns state s rendered in cell c under the gallery's header
// titled title, and a blank line after it. An Animated state sets
// Env.Animate itself, as the goldens render it.
func (g gallery) render(s fixture.State, c cell, title string) (string, error) {
	ctx, err := g.ctx(c.env)
	if err != nil {
		return "", err
	}
	gap := strings.Repeat("\n", theme.SectionGap+1)
	return header(ctx, title) + gap + s.VM.Render(ctx) + gap, nil
}

// run is the gallery: it parses args, renders to stdout and reports
// errors on stderr, and returns the exit status.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uigallery", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: go run ./internal/tools/uigallery [flags] [component | component/state | all]...")
		fs.PrintDefaults()
	}
	list := fs.Bool("list", false, "print the names of the states it would render, and exit")
	themeName := fs.String("theme", "dark", "the palette: dark or light")
	profileName := fs.String("profile", "truecolor", "the colour profile: truecolor, 256, 16, none or plain")
	ascii := fs.Bool("ascii", false, "use the ASCII glyph set")
	width := fs.Int("width", term.DefaultWidth, "the terminal width in cells")
	inMatrix := fs.Bool("matrix", false, "render each state in every cell of the golden matrix and plain mode, in place of -theme, -profile, -ascii and -width")
	overridePath := fs.String("override", "", "a colour override `file` (JSON; see uigallery.local.json.example)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	usage := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "uigallery: "+format+"\n", a...)
		return exitUsage
	}

	states, err := selectStates(catalogue(), fs.Args())
	if err != nil {
		return usage("%v", err)
	}
	if *list {
		for _, s := range states {
			fmt.Fprintln(stdout, s.Name)
		}
		return exitOK
	}

	var cells []cell
	if *inMatrix {
		var set []string
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "theme" || f.Name == "profile" || f.Name == "ascii" || f.Name == "width" {
				set = append(set, "-"+f.Name)
			}
		})
		if len(set) > 0 {
			return usage("-matrix renders every environment; leave out %s", strings.Join(set, ", "))
		}
		cells = matrix()
	} else {
		i := slices.IndexFunc(profiles, func(p profile) bool { return p.name == *profileName })
		switch {
		case i < 0:
			return usage("-profile %q is not truecolor, 256, 16, none or plain", *profileName)
		case *themeName != "dark" && *themeName != "light":
			return usage("-theme %q is not dark or light", *themeName)
		case *width < 1:
			return usage("-width %d is not a positive number of cells", *width)
		}
		cells = []cell{newCell(profiles[i], *themeName == "dark", !*ascii, *width)}
	}

	// x/ansi reads RUNEWIDTH_EASTASIAN at init (method.go:20-25); when it
	// is true, ambiguous-width glyphs measure two cells and no layout
	// matches its golden. uitest.Golden makes the same check.
	if w := ansi.StringWidth("─"); w != 1 {
		fmt.Fprintf(stderr, "uigallery: x/ansi measures the ambiguous-width ─ as %d cells, and the components assume 1; unset RUNEWIDTH_EASTASIAN\n", w)
		return exitError
	}

	g := gallery{}
	if *overridePath != "" {
		if g.colours, err = readOverride(*overridePath); err != nil {
			fmt.Fprintf(stderr, "uigallery: %v\n", err)
			return exitError
		}
	}
	for _, s := range states {
		for _, c := range cells {
			title := s.Name
			if *inMatrix {
				title += " " + c.name
			}
			out, err := g.render(s, c, title)
			if err == nil {
				_, err = io.WriteString(c.env.Writer(stdout), out)
			}
			if err != nil {
				fmt.Fprintf(stderr, "uigallery: %s in %s: %v\n", s.Name, c.name, err)
				return exitError
			}
		}
	}
	return exitOK
}
