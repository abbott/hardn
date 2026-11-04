// Command uigallery is hardn's dev-only design-system gallery. It renders
// the states of pkg/ui/component, the ones the component goldens pin
// (pkg/ui/component/fixture.States), and with -screens the screen mocks,
// the ones the screen goldens pin (pkg/ui/screen.Mocks), in a render
// environment chosen by flags, so that the maintainer can review the
// components and screens and try new colours without editing code (brief
// 1D).
//
// It is never part of a release: every build target of the makefile, the
// goreleaser configs and the workflows builds ./cmd/hardn alone, and a
// main package cannot be imported. Run it from the repository root:
//
//	go run ./internal/tools/uigallery [flags] [component | component/state | all]...
//	go run ./internal/tools/uigallery -screens [flags] [layout | layout/screen | ... | all]...
//	go run ./internal/tools/uigallery play [flags] [screen | workflow/sequence | engine | engine/scenario | status | status/host | dns | dns/host | backup | backup/host | backup-restore | environment | environment/host | system-details | system-details/host | logs | logs/host | firewall | firewall/host | users | users/host | ssh | ssh/host | updates | updates/host | runall | runall/host]
//	go run ./internal/tools/uigallery status [flags] [host | all]...
//	go run ./internal/tools/uigallery glyphs [flags] [set | strip | all]...
//	go run ./internal/tools/uigallery system-details [flags] [form | all]...
//	go run ./internal/tools/uigallery logs [flags] [form | all]...
//
// With no argument, or "all", it renders every state; "menu" renders every
// state of Menu and "menu/compact" one. States come out in the catalogue's
// order, each under a header with its whole name, which on a terminal too
// narrow for the name runs past its width.
//
// With -screens it renders the screen mocks instead, by layout, then by
// screen: "sub-menu" is every sub-menu and "sub-menu/01-user" one. Each
// mock comes under a header with its name and a note of its source: its
// Figma frame, or what it is inferred from, how origin/main's screen
// differs from v0.3.2's, and where it waits on a proposal. A workflow's
// mock is its sequences, each step printed in order under a header naming
// the sequence and the step, so that the steps read as the transcript of
// one run: "workflow-step/user-manage-sudo-confirmation/failure" is one
// sequence, ".../failure/2-failed" its last step. A sequence that ends
// otherwise at the width than it says, as one whose typed confirmation
// the terminal is too narrow to ask (below 27 columns in Unicode glyphs,
// 29 in ASCII), which the flow cancels, is printed whole under its name,
// after a note saying so, and the other mocks still show.
//
// "play" is the playable mode, which runs the real screens and prompts on
// a simulated system: the user types the answers, and sees what hardn does
// with them. It starts at the screen it names (a mock's name, or a layout
// that has no other mock, which only main-menu is; the main menu when none
// is named), or at a workflow's sequence, "<workflow>/<sequence>". A
// menu's items lead where origin/main's do, to the other mocks: User
// Management, Manage User and george's menu to the sudo change, SSH Login
// to the root-login toggle and its typed confirmation, DNS to hardn's own
// DNS screen on a simulated host (below); an item with no mock says so. 0
// goes back and q quits at every prompt, as the navigation grammar has it;
// going back from the first screen, or quitting, exits with status 0, and
// the end of the input or Ctrl-C at a prompt with 130. Each workflow runs
// its flow (pkg/ui/flow) on its sequence's fixture plan, and applies it to
// the simulated system, which only records what each step would do: the
// record is shown when play ends. A sequence plays for its screen and
// fixture, the answers being the user's, so one that ends cancelled, as
// create-user's does, cannot be named: declining the confirmation plays it.
//
// "play engine" opens the engine's scenarios (next/p3-gallery-engine), and
// "play engine/<scenario>" one: the real plan/apply engine (pkg/engine),
// its change kinds (pkg/engine/steps) and pkg/output's flow adapter, on a
// simulated host: hostfake's recording host.System, whose commands a model
// of the host answers (fakehost.go). The 2230 change on Debian 13's
// ssh.service, Ubuntu 26.04's ssh.socket and Alpine 3.24's OpenRC;
// key-only login; ufw enabled; a sudo grant; a refused plan (the D9
// migration's report, P-39); an apply that fails and is rolled back; the
// 2230 change confirmed from a new connection and cleaned up by hardn
// confirm (P-40); a plan on a best-effort release, with its warning
// (P-41); and the DNS control (pkg/engine/controls/dns) on the resolver
// model (resolver.go): Configure DNS on Ubuntu 26.04 and 24.04, Alpine
// 3.24 and Proxmox VE 9, a preset with hardn.yml's save on Debian 13, as
// a dry run too, two refusals, a host with nothing to change, and a
// failed reload and a failed save, both rolled back
// (next/s2-engine-gallery). Each shows the real plan, the confirmation,
// the steps, the confirm countdown and the outcome, and play ends with
// what the fake recorded of each apply. -sequence and -fail are the
// fixture plans', and are refused with a scenario. Its flags, besides
// -theme, -profile, -ascii and -override, as below:
//
//	-sequence name    the sequence each workflow plays where it has one,
//	                  such as dry-run, failure or typed (default normal)
//	-fail N           fail step N of each plan applied, to show the
//	                  rollback (default 0: none)
//	-width N          the terminal width in cells (default: the
//	                  terminal's own, when stdout is a terminal, else 80)
//	-height N         the terminal height in lines (default: the
//	                  terminal's own, when stdout is a terminal, else 24)
//
// "play dns" opens the DNS screen's hosts (next/s2-screen), and "play
// dns/<host>" one: hardn's own DNS screen (pkg/screens/dns), drawn from a
// simulated host's resolver and hardn.yml through the DNS control, each
// item a change on the real engine, the screen drawn again from the host
// under the result: Debian 13, Ubuntu 26.04, Alpine 3.24, Proxmox VE 9,
// NetworkManager, hardn.yml listing none or one, a configured host, and a
// reload and a save that fail. -sequence dry-run makes each change a dry
// run; -fail is refused there.
//
// Play sizes itself to the terminal: where -width or -height is not given
// and stdout is a terminal, it takes the terminal's size, as term.Detect,
// which builds the render environment, reads it, once at the start, so
// that the main menu is drawn as hardn draws it there: its full form at
// every height, as 00-TUI-MAIN.png shows it, its top scrolling off a
// terminal shorter than it (its compact form is gated,
// screen.CompactMainMenu). A flag given wins, each on its own; with
// stdout not a terminal, as in the tests and through a pipe, play keeps
// 80 and 24, so that its goldens stay deterministic.
//
// "play status" opens the status hosts (next/p4-gallery-status), and
// "play status/<host>" one: hardn's main menu, its header, distribution
// bar, host block, risk level and status rows read from a simulated host
// by the real checks (pkg/engine/controls), header probe
// (pkg/engine/header) and risk level (pkg/engine/risk), through
// Engine.Check, and mapped by pkg/output, on the model of fakehost.go with
// what the checks read of each platform (statushost.go): Debian 13,
// Ubuntu 26.04 and Alpine 3.24 as the engine's scenarios start from them,
// hosts as installed (a Proxmox VE 9 node, Debian 13 with root alone,
// Ubuntu 26.04's cloud image and an install from its ISO), and host states
// that show the other lines (a hardened host, Figma's, the lockout now and
// at ufw's next start, a pending reboot, AppArmor's lines, a container).
// The rest of the main menu is its mock's, whose links its items follow,
// to mocks that show Figma's host, so -sequence and -fail are taken as for
// the mocks. "status" lists the same main menus with no input, each under a
// header naming its host and a note saying what it is, down to its
// prompt; -list prints the hosts' names. Its flags are -list, -theme,
// -profile, -ascii, -override, and -width and -height, sized as play's.
//
// The compare mode, P-42's mock of Figma's row grammar beside the pattern
// the components drew, was removed once the components drew the grammar
// (next/s2-row-grammar, the maintainer's decision of 2026-10-22): its two
// sides were then alike. Git keeps it (compare.go, next/p3-gallery-engine).
//
// "glyphs" shows the status rows' symbols (glyphs.go,
// docs/engine/gallery-glyphs.md): Figma's ⛛ ⭘ ⛬, of which ⛬ is the risk
// level's, beside candidates for the states Figma draws no symbol for,
// Partial, Unknown and Not Applicable. Each set is the risk row and
// 00-TUI-MAIN's seven rows in all five states, with a legend naming its
// glyphs' code points and colours: current (⭘ ? –, the trial set the
// maintainer adopted on 2026-10-21, drawn as RiskSummary and StatusRow draw
// them), and the candidates set aside: power (⏼ ? –), figma (the three
// states folded into Figma's glyphs and colours), diamond (⬙ ⁇ –),
// diamond-dot (◈ ? ⊟) and square (◧ ⍰, no marker), each with ⛬ on the risk
// row alone; side by side as many as the terminal holds, else one above the
// next. strip sets each shortlisted glyph alone beside ⛛ ⭘ ⛬, in one
// colour; it has no ASCII form. None, or "all", is every set and the strip.
// Its flags are -list, -theme, -profile, -ascii, -override, and -width and
// -height sized as play's; it draws nothing by -height.
//
// "play backup" is the menu of the hosts the Backup screen plays on and
// "play backup/<host>" hardn's Backup screen itself on one
// (pkg/screens/backup; backupscreen.go, docs/engine/screen-backup.md),
// each change on the real engine; -sequence dry-run stops each at its
// plan, as plan only would; -fail is refused there. "play backup-restore"
// is the restore of a backup run (P-19), a mock, its control the
// gallery's, kept for its later unit. The Backup listing, "backup", the
// mocks of refactor plan slice 3, unit (a) (docs/engine/gallery-backup.md),
// was removed when the real screen replaced them (next/s3-screen): git
// keeps it (backup.go, next/s3-parity).
//
// "play environment" is the menu of the hosts the Environment screen
// plays on and "play environment/<host>" hardn's Environment screen itself
// on one (pkg/screens/environment; environment.go,
// docs/engine/screen-environment.md), its change on the real engine;
// -sequence dry-run stops it at its plan, as plan only would; -fail is
// refused there. The main menu's 8 leads to it on Debian 13. The
// Environment listing, "environment", the mocks of refactor plan slice 9,
// unit (a) (docs/engine/gallery-environment.md), was removed when the
// real screen replaced them (next/s9-screen): git keeps it
// (environment.go, next/s9-parity).
//
// "play system-details" is the menu of the hosts the System Details screen
// plays on and "play system-details/<host>" hardn's System Details screen itself on
// one (pkg/screens/systemdetails; systemdetails.go,
// docs/engine/screen-system-details.md); the main menu's 9 leads to it on Debian 13.
// "play logs" and "play logs/<host>" are the same of the Logs screen
// (pkg/screens/logs; logs.go); the main menu's 10 leads to it on a log of lines. Each
// only reads: no plan, no y/N, no journal. -sequence dry-run draws the same screen, as
// plan only would; -fail is refused there. No pager runs: Open says what hardn would
// run.
//
// "system-details" lists hardn system-details' non-interactive forms and "logs" hardn
// -p's, each under a note, what unit (c) built through the real Print, and for
// System Details what unit (d) has not built and takes as its reference (the JSON
// form, the five flags). The listing's other mocks, of refactor plan slice 1, unit (a)
// (docs/engine/gallery-system-details.md), were removed when the real screens replaced
// them (next/s1-screen): git keeps them (systemdetails.go, logs.go, next/s1-parity).
// Their flags are -list, -theme, -profile, -ascii, -override, and -width and -height
// sized as play's.
//
// "play firewall" is the menu of the Firewall screen's hosts and "play
// firewall/<host>" the real screen on one (pkg/screens/firewall, firewall.go),
// every change on the real engine; the main menu's 4 leads to it on Debian 13.
// -sequence dry-run stops each change at its plan, as plan only would; -fail is
// refused there. The listing of its mocks, "firewall", of refactor plan slice 4,
// unit (a) (docs/engine/gallery-firewall.md), was removed when the real screen
// replaced them (next/s4-screen): git keeps it (firewall.go, next/s4-control).
//
// "play users" is the menu of the User Management screen's hosts and "play
// users/<host>" the real screen on one (pkg/screens/users, users.go), every
// change on the real engine; the main menu's 1 leads to it on Debian 13.
// -sequence dry-run stops each change at its plan, as plan only would; -fail is
// refused there. The listing of its mock, "users", of refactor plan slice 5,
// unit (a) (docs/engine/gallery-users.md), was removed when the real screen
// replaced it (next/s5-screen): git keeps it (users.go, next/s5-control).
//
// "play ssh" is the menu of the SSH Login screen's hosts and "play
// ssh/<host>" the real screen on one (pkg/screens/ssh, ssh.go), every change
// on the real engine; the main menu's 2 leads to it on Debian 13. -sequence
// dry-run stops each change at its plan, as plan only would; -fail is
// refused there. The listing of its mock, "ssh", of refactor plan slice 6,
// unit (a) (docs/engine/gallery-ssh.md), was removed when the real screen
// replaced it (next/s6-screen): git keeps it (ssh.go, next/s6-control).
//
// "play updates" is the menu of the Auto Updates screen's hosts and "play
// updates/<host>" the real screen on one (pkg/screens/updates, updates.go),
// every change on the real engine; the main menu's 6 leads to it on Debian
// 13. -sequence dry-run stops each change at its plan, as plan only would;
// -fail is refused there. The listing of its mock, "updates", of refactor
// plan slice 7, unit (a) (docs/engine/gallery-updates.md), was removed when
// the real screen replaced it (next/s7-screen): git keeps it (updates.go,
// next/s7-control).
//
// The Dry-Run mode, "dryrun" and "play dryrun", the mocks of refactor
// plan slice 2b (docs/engine/gallery-dryrun.md), was removed with the
// Dry-Run item it designed around (slice 2b, unit (d), next/s2b-replace):
// gallery-dryrun.md §11 records what each mock showed, and git keeps it
// (dryrun.go, next/s2b-menu).
//
// The flags of the other modes, whose environment is the flags' alone:
//
//	-list             print the names of the states, or with -screens the
//	                  screen drawings, it would render, and exit
//	-theme dark|light the palette (default dark)
//	-profile name     truecolor, 256, 16, none (no colour; bold and
//	                  underline kept) or plain (no escape sequences, ASCII
//	                  glyphs) (default truecolor)
//	-ascii            the ASCII glyph set
//	-width N          the terminal width in cells (default 80)
//	-height N         the terminal height in lines (default 24); the main
//	                  menu takes its full form at every height, its
//	                  compact form gated (screen.CompactMainMenu)
//	-screens          the screen mocks, in place of the component states
//	-matrix           render each state in every cell of the golden matrix,
//	                  uitest.Full's 48, then plain mode at 60, 80 and 120,
//	                  each under its cell's name, in place of -theme,
//	                  -profile, -ascii, -width and -height; not with
//	                  -screens, whose goldens are on the core set
//	-override FILE    a colour override file (JSON; see
//	                  uigallery.local.json.example)
//
// The override file maps token names to new values for each palette and
// is applied through theme.NewWithOverrides, which only this package may
// call (theme's TestOverrideReach). Copy the example to
// uigallery.local.json, which the repository's allow-list .gitignore
// keeps out of git, and pass its path.
//
// The gallery's code reads nothing but its flags, the override file and,
// in the playable mode, stdin and the terminal's size, and in status and
// glyphs the terminal's size: no root, nothing under /etc. It builds each
// environment with term.Fixed. Only to size itself, where -width or
// -height is not given, the playable mode, status or glyphs calls
// term.Detect, given the dark theme so that it never queries the
// terminal's background: Detect asks whether stdin and stdout are
// terminals, reads the size of the terminal stdout is and os.Environ
// (COLUMNS and LINES, for a terminal whose size it cannot read), and of
// what it resolves either takes the size alone. The other modes read no
// environment variable and query no terminal for their output. The
// playable mode reads stdin through prompt.Stdin, the line reader every
// hardn prompt uses, which asks whether stdin is a terminal by reading
// its attributes (TCGETS or TIOCGETA), on one discards typeahead before
// each prompt (TCFLSH or TIOCFLUSH), and while it waits for a line takes
// Ctrl-C, giving SIGINT back its own handling before it returns; it never
// changes the terminal's mode. Nothing the playable mode runs writes a
// file or starts a process (TestPlayTouchesNothing): the engine's
// scenarios link host.Real, the engine's signal hold and pkg/config, and
// never reach them, every host.System being hostfake's and every hold the
// gallery's, which holds nothing; the DNS scenarios read and save
// hardn.yml through the fake alone (config.Read, config.Save). Of this
// machine the engine's code reads only the process's effective uid, the
// clock and crypto/rand, and fatih/color, which it links, reads NO_COLOR
// and TERM and asks whether stdout is a terminal at its initialisation.
// The package initialisation of Lip Gloss and x/ansi still runs, as in
// every binary that links Lip Gloss (today the pkg/ui test binaries and
// this gallery; cmd/hardn links none): an ioctl on stdout and, on a
// terminal, a read-only terminfo lookup and "tmux info" under tmux, and
// x/ansi's read of RUNEWIDTH_EASTASIAN
// (docs/refactor/phase0/side-effects.md §13.1). The gallery ignores the
// profile Lip Gloss detects. A true RUNEWIDTH_EASTASIAN makes x/ansi
// measure ambiguous-width glyphs such as "─" as two cells, which moves
// every layout off its golden, so the gallery then refuses to render
// (exit status 1), as the golden harness refuses to run. It writes to
// stdout only, through each environment's term.Env.Writer, one write per
// state and environment, or per screen mock: what the golden file holds
// for the same cell; the playable mode writes as hardn does, a block or a
// prompt at a time; status and glyphs write their note, then each host or
// set, a write each. A workflow's sequences play on scripted answers and
// fixture plans (screen.Sequence.Play, flow.Fixture), which read no
// terminal and change nothing.
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
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

func main() {
	os.Exit(run(os.Args[1:], prompt.Stdin, stdoutSize, os.Stdout, os.Stderr))
}

// sizer returns the size of the terminal stdout is, in cells and lines,
// and whether stdout is a terminal at all. The playable mode, status and
// glyphs size themselves with it where -width or -height is not given
// (look.sizeTo); the tests pass their own, so that no test depends on its
// own terminal.
type sizer func() (width, height int, tty bool)

// stdoutSize is the gallery's sizer: the size of the terminal stdout is,
// as term.Detect reads it for the render environment (the terminal's own,
// else COLUMNS and LINES, else 80x24), and whether stdout is a terminal.
// Detect is given the dark theme, so that it never queries the terminal's
// background; the modes take the size alone of what Detect resolves.
func stdoutSize() (width, height int, tty bool) {
	env := term.Detect(term.Options{Theme: term.ThemeDark}, os.Stdin, os.Stdout)
	return env.Width, env.Height, env.TTY
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

// look holds the flags that pick the render environment and its colours,
// which every mode of the gallery takes.
type look struct {
	theme, profile, override *string
	ascii                    *bool
	width, height            *int
}

// mainForm is what -height's help says of the main menu's form: its full
// form at every height while its compact form is gated
// (screen.CompactMainMenu), and otherwise when it takes its compact form.
var mainForm = func() string {
	if !screen.CompactMainMenu {
		return "The main menu takes its full form at every height, its top scrolling off a shorter terminal (its compact form is gated: screen.CompactMainMenu)"
	}
	return fmt.Sprintf("The main menu takes its compact form under %d rows, or when its full form does not fit, so comparing it with 00-TUI-MAIN.png needs -height %d", theme.CompactBelow, theme.CompactBelow)
}()

// lookFlags defines the look flags on fs. With sized set, as in the
// playable mode, status and glyphs, -width and -height default to the
// terminal's size (look.sizeTo), 0 standing for it until then, and their
// help says so; otherwise they default to term.DefaultWidth and
// term.DefaultHeight. -height's help speaks of the main menu, which
// glyphs, drawing nothing by the height, replaces with its own.
func lookFlags(fs *flag.FlagSet, sized bool) look {
	width, height := term.DefaultWidth, term.DefaultHeight
	widthHelp := "the terminal width in cells"
	heightHelp := "the terminal height in lines. " + mainForm
	if sized {
		width, height = 0, 0
		widthHelp = fmt.Sprintf("the terminal width in cells: by default the terminal's own when stdout is a terminal, else %d", term.DefaultWidth)
		heightHelp = fmt.Sprintf("the terminal height in lines: by default the terminal's own when stdout is a terminal, else %d. ", term.DefaultHeight) + mainForm
	}
	return look{
		theme:    fs.String("theme", "dark", "the palette: dark or light"),
		profile:  fs.String("profile", "truecolor", "the colour profile: truecolor, 256, 16, none or plain"),
		ascii:    fs.Bool("ascii", false, "use the ASCII glyph set"),
		width:    fs.Int("width", width, widthHelp),
		height:   fs.Int("height", height, heightHelp),
		override: fs.String("override", "", "a colour override `file` (JSON; see uigallery.local.json.example)"),
	}
}

// sizeTo sets -width and -height where the command line did not give
// them, as the playable mode, status and glyphs do before they build
// their environment: to the size of the terminal stdout is (size), so that
// play draws what the screens draw on that terminal, the main menu's full
// form on one tall enough, and glyphs sets its sets side by side on one
// wide enough; and where stdout is not a terminal, as in the tests and through
// a pipe, to term.DefaultWidth and term.DefaultHeight, so that their
// goldens stay deterministic. A flag given wins, each on its own, even
// one that look.cell refuses; with both given, size is not called.
func (l look) sizeTo(fs *flag.FlagSet, size sizer) {
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if given["width"] && given["height"] {
		return
	}
	w, h, tty := size()
	pick := func(measured, fallback int) int {
		if tty && measured > 0 {
			return measured
		}
		return fallback
	}
	if !given["width"] {
		*l.width = pick(w, term.DefaultWidth)
	}
	if !given["height"] {
		*l.height = pick(h, term.DefaultHeight)
	}
}

// cell returns the environment the look flags describe, or the usage
// error that says why they describe none.
func (l look) cell() (cell, error) {
	i := slices.IndexFunc(profiles, func(p profile) bool { return p.name == *l.profile })
	switch {
	case i < 0:
		return cell{}, fmt.Errorf("-profile %q is not truecolor, 256, 16, none or plain", *l.profile)
	case *l.theme != "dark" && *l.theme != "light":
		return cell{}, fmt.Errorf("-theme %q is not dark or light", *l.theme)
	case *l.width < 1:
		return cell{}, fmt.Errorf("-width %d is not a positive number of cells", *l.width)
	case *l.height < 1:
		return cell{}, fmt.Errorf("-height %d is not a positive number of lines", *l.height)
	}
	c := newCell(profiles[i], *l.theme == "dark", !*l.ascii, *l.width)
	c.env.Height = *l.height
	return c, nil
}

// gallery returns the gallery with the override file's colours, if the
// flags name one, once it has checked that the glyphs measure as the
// components assume. On a failure it writes why to stderr and returns
// false: the exit status is then exitError.
func (l look) gallery(stderr io.Writer) (gallery, bool) {
	// x/ansi reads RUNEWIDTH_EASTASIAN at init (method.go:20-25); when it
	// is true, ambiguous-width glyphs measure two cells and no layout
	// matches its golden. uitest.Golden makes the same check.
	if w := ansi.StringWidth("─"); w != 1 {
		fmt.Fprintf(stderr, "uigallery: x/ansi measures the ambiguous-width ─ as %d cells, and the components assume 1; unset RUNEWIDTH_EASTASIAN\n", w)
		return gallery{}, false
	}
	g := gallery{}
	if *l.override != "" {
		var err error
		if g.colours, err = readOverride(*l.override); err != nil {
			fmt.Fprintf(stderr, "uigallery: %v\n", err)
			return gallery{}, false
		}
	}
	return g, true
}

// run is the gallery: it parses args, renders to stdout and reports
// errors on stderr, and returns the exit status. With "play" first it is
// the playable mode (play), which reads the user's answers through the
// line reader stdin returns and sizes itself with size; with "status",
// "glyphs", "system-details" or "logs" first, the status listing
// (statusList), the glyphs mode (glyphs), the System Details listing
// (sdListing) or the Logs listing (logsListing), which size themselves with
// size too and read no input. No other mode calls either.
func run(args []string, stdin func() *prompt.Reader, size sizer, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "play" {
		return play(args[1:], stdin, size, stdout, stderr)
	}
	if len(args) > 0 && args[0] == statusPrefix {
		return statusList(args[1:], size, stdout, stderr)
	}
	if len(args) > 0 && args[0] == "glyphs" {
		return glyphs(args[1:], size, stdout, stderr)
	}
	if len(args) > 0 && args[0] == sdPrefix {
		return sdListing.run(args[1:], size, stdout, stderr)
	}
	if len(args) > 0 && args[0] == logsPrefix {
		return logsListing.run(args[1:], size, stdout, stderr)
	}
	fs := flag.NewFlagSet("uigallery", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: go run ./internal/tools/uigallery [flags] [component | component/state | all]...")
		fmt.Fprintln(stderr, "       go run ./internal/tools/uigallery -screens [flags] [layout | layout/screen | ... | all]...")
		fmt.Fprintln(stderr, "       "+strings.TrimPrefix(playUsage, "usage: "))
		fmt.Fprintln(stderr, "       go run ./internal/tools/uigallery status [flags] [host | all]...")
		fmt.Fprintln(stderr, "       go run ./internal/tools/uigallery glyphs [flags] [set | strip | all]...")
		fmt.Fprintln(stderr, "       "+strings.TrimPrefix(sdUsage, "usage: "))
		fmt.Fprintln(stderr, "       "+strings.TrimPrefix(logsUsage, "usage: "))
		fs.PrintDefaults()
	}
	list := fs.Bool("list", false, "print the names of the states, or with -screens the screen drawings, it would render, and exit")
	look := lookFlags(fs, false)
	screens := fs.Bool("screens", false, "render the screen mocks, in place of the component states")
	inMatrix := fs.Bool("matrix", false, "render each state in every cell of the golden matrix and plain mode, in place of -theme, -profile, -ascii, -width and -height")
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

	var (
		states []fixture.State
		names  []string
		err    error
	)
	if *screens {
		names, err = selectDrawings(screen.Mocks(), fs.Args())
	} else {
		states, err = selectStates(catalogue(), fs.Args())
		for _, s := range states {
			names = append(names, s.Name)
		}
	}
	if err != nil {
		return usage("%v", err)
	}
	if *list {
		for _, n := range names {
			fmt.Fprintln(stdout, n)
		}
		return exitOK
	}

	var cells []cell
	if *inMatrix {
		var set []string
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "theme" || f.Name == "profile" || f.Name == "ascii" || f.Name == "width" || f.Name == "height" {
				set = append(set, "-"+f.Name)
			}
		})
		switch {
		case *screens:
			return usage("-matrix renders the component states; for -screens pick one cell with -profile, -theme, -ascii, -width and -height")
		case len(set) > 0:
			return usage("-matrix renders every environment; leave out %s", strings.Join(set, ", "))
		}
		cells = matrix()
	} else {
		c, err := look.cell()
		if err != nil {
			return usage("%v", err)
		}
		cells = []cell{c}
	}

	g, ok := look.gallery(stderr)
	if !ok {
		return exitError
	}
	if *screens {
		for _, m := range screen.Mocks() {
			out, err := g.renderMock(m, names, cells[0])
			if err == nil {
				_, err = io.WriteString(cells[0].env.Writer(stdout), out)
			}
			if err != nil {
				fmt.Fprintf(stderr, "uigallery: %s in %s: %v\n", m.Name, cells[0].name, err)
				return exitError
			}
		}
		return exitOK
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
