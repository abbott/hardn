package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// sized is a truecolor Unicode cell width columns wide and height rows
// high, as pkg/ui/screen's tests name it.
func sized(width, height int, dark bool) uitest.Cell {
	bg := "light"
	if dark {
		bg = "dark"
	}
	env := term.Fixed(width, colorprofile.TrueColor, true, dark)
	env.Height = height
	return uitest.Cell{Name: fmt.Sprintf("truecolor-%s-unicode-%dx%d", bg, width, height), Env: env}
}

// mainCells are the cells of the mocks that draw the main menu or its
// mode, as pkg/ui/screen pins its main menu: the core set, whose
// terminals are 24 rows high, so its 80- and 60-column cells show the
// compact form and plain mode, no terminal, the full one; the full form at
// 80x40 on both themes; 60x40, where the compact form is drawn; and the
// compact form in ASCII at 80x24.
func mainCells() []uitest.Cell {
	return append(uitest.Core(), sized(80, 40, true), sized(80, 40, false), sized(60, 40, true),
		uitest.Cell{Name: "truecolor-dark-ascii-80", Env: term.Fixed(80, colorprofile.TrueColor, false, true)})
}

// cellArgs returns the flags that pick env: lookArgs's, and -height when
// env is not term.DefaultHeight rows high.
func cellArgs(t *testing.T, env term.Env) []string {
	t.Helper()
	base := env
	base.Height = term.DefaultHeight
	args := lookArgs(t, base)
	if env.Height != term.DefaultHeight {
		args = append(args, "-height", strconv.Itoa(env.Height))
	}
	return args
}

// TestDryRunGolden pins each mock of the listing on the core set, a main
// menu and the mode on mainCells: the gallery's header, the mock's note,
// then its drawing.
func TestDryRunGolden(t *testing.T) {
	for _, m := range dryMocks() {
		cells := uitest.Core()
		if m.main {
			cells = mainCells()
		}
		uitest.Golden(t, dryRunPrefix+"/"+m.name, cells, func(env term.Env) string {
			args := append(append([]string{dryRunPrefix}, cellArgs(t, env)...), m.name)
			code, out, errs := runGallery(args...)
			if code != exitOK || errs != "" {
				t.Errorf("%v: exit %d, stderr %q; want exit 0", args, code, errs)
			}
			return out
		})
	}
}

// TestPlayDryRunGolden plays the mocks on the core set and pins each
// transcript: origin/main's main menu, 6 turning dry run on for the
// session, then DNS Add stopping at its plan with the footer, then 4,
// origin/main's Firewall screen under dry run, then 1, the note of what
// origin/main does there (V2) above Figma's mock, then out; the toggle
// saved through the canonical flow; and the proposed main menu started in
// dry run (-n), its 6 refused as no item's key, its 1 and 5 refused under
// plan only (decision 6), then DNS Add.
func TestPlayDryRunGolden(t *testing.T) {
	for _, r := range []struct {
		name  string
		args  []string
		lines []string
	}{
		{"dryrun", []string{dryRunPrefix}, []string{"6", "1", "0", "3", "2", "192.0.2.53", "0", "4", "1", "0", "0"}},
		{"dryrun-saved", []string{dryRunPrefix + "/" + variantSaved}, []string{"6", "1", "y", "0", "0"}},
		{"dryrun-proposed", []string{"-sequence", "dry-run", dryRunPrefix + "/" + variantProposed}, []string{"6", "1", "5", "3", "2", "192.0.2.53", "0", "0"}},
	} {
		uitest.Golden(t, "play/"+r.name, uitest.Core(), func(env term.Env) string {
			args := append(lookArgs(t, env), r.args...)
			code, out, errs, unread := playGallery(r.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread; want exit 0", r.name, args, code, errs, unread)
			}
			return out
		})
	}
}

// TestDryRunHeights measures the main menu on its layout over the
// hardened host's eight rows, no line under any, Unicode and ASCII: its
// lines and whether the risk level is on screen, the prompt being the
// terminal's last line; origin/main's items and the proposed ones (item
// 6 out); and hardn's live main menu today, the status blocks over
// pkg/menu's 17-line menu (docs/engine/menu-live.md §6).
func TestDryRunHeights(t *testing.T) {
	const legacyMenu = 17
	sh, ok := findStatusHost(dryRunHost)
	if !ok {
		t.Fatalf("no status host %q", dryRunHost)
	}
	st, err := (&session{}).readStatus(context.Background(), sh.host())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		w, h             int
		layout, proposed int
		live             int
		risk, riskLive   bool
	}{
		{80, 24, 25, 25, 32, true, false},
		{80, 40, 36, 35, 37, true, true},
		{60, 24, 36, 36, 42, false, false},
	} {
		for _, unicode := range []bool{true, false} {
			env := term.Fixed(tc.w, colorprofile.TrueColor, unicode, true)
			env.Height = tc.h
			c := component.NewCtx(env)
			shown := func(out string) (int, bool) {
				ls := strings.Split(ansi.Strip(out), "\n")
				for i, l := range ls {
					if strings.Contains(l, "Risk Level") {
						return len(ls), i >= len(ls)-tc.h
					}
				}
				t.Fatalf("%dx%d: no risk line:\n%s", tc.w, tc.h, out)
				return 0, false
			}
			layout, risk := shown(mainMenu(st, false, false).Render(c))
			proposed, err := modeView{proposed: true}.drawMenu(c, mainMenu(st, false, true))
			if err != nil {
				t.Fatal(err)
			}
			nProposed, riskProposed := shown(proposed)
			blocks := mainMenu(st, false, false).StatusBlocks(c, legacyMenu)
			live, riskLive := shown(blocks + strings.Repeat("\n", theme.SectionGap+legacyMenu))
			if layout != tc.layout || nProposed != tc.proposed || live != tc.live || risk != tc.risk || riskProposed != tc.risk || riskLive != tc.riskLive {
				t.Errorf("%dx%d, unicode %t: layout %d lines (risk shown %t), proposed %d (%t), live %d (%t); want %d (%t), %d, %d (%t)",
					tc.w, tc.h, unicode, layout, risk, nProposed, riskProposed, live, riskLive, tc.layout, tc.risk, tc.proposed, tc.live, tc.riskLive)
			}
		}
	}
}

// TestDryRunHeightsLine adds a line under a row: the gallery's Figma
// host, "Up 182 days, no updates" under Auto Updates, at 80x24 takes 26
// lines on the layout, its risk level still on screen.
func TestDryRunHeightsLine(t *testing.T) {
	sh, ok := findStatusHost("figma")
	if !ok {
		t.Fatal("no status host figma")
	}
	st, err := (&session{}).readStatus(context.Background(), sh.host())
	if err != nil {
		t.Fatal(err)
	}
	env := term.Fixed(80, colorprofile.TrueColor, true, true)
	ls := strings.Split(ansi.Strip(mainMenu(st, false, false).Render(component.NewCtx(env))), "\n")
	risk := slices.IndexFunc(ls, func(l string) bool { return strings.Contains(l, "Risk Level") })
	if len(ls) != 26 || risk < len(ls)-env.Height || !slices.ContainsFunc(ls, func(l string) bool { return strings.Contains(l, "Up 182 days, no updates") }) {
		t.Errorf("Figma's host at 80x24: %d lines, the risk level on line %d; want 26, on screen, with its line under Auto Updates:\n%s", len(ls), risk, strings.Join(ls, "\n"))
	}
}

// TestModeHeading checks the proposed indicator's place against the
// component's, in each state: beside the heading at the value column from
// theme.StackBelow columns, under it at the stack column below; no line
// wider than the terminal.
func TestModeHeading(t *testing.T) {
	for _, w := range []int{120, 80, 64, 63, 60, 40, 20} {
		c := component.NewCtx(term.Fixed(w, colorprofile.ASCII, false, true))
		for _, s := range modeStates {
			ls := strings.Split(ansi.Strip(s.mv.heading(c, "Select an option")), "\n")
			stacked := w < theme.StackBelow
			at, col := ls[0], theme.ValueCol
			if stacked {
				at, col = ls[len(ls)-1], theme.StackCol
			}
			if len(ls) != map[bool]int{false: 1, true: 2}[stacked] || len(at) <= col || strings.TrimSpace(at[:col]) != map[bool]string{false: "Select an option", true: ""}[stacked] || at[col] == ' ' {
				t.Errorf("%d columns, %s: %q; want the mode from col %d, stacked %t", w, s.label, ls, col, stacked)
			}
			for _, l := range ls {
				if n := ansi.StringWidth(l); n > w {
					t.Errorf("%d columns, %s: a line of %d cells: %q", w, s.label, n, l)
				}
			}
		}
	}
}

// TestLegacyFirewallIsReal holds the Firewall transcript to what pkg/menu
// prints: pkg/characterization's golden, its colours and clears stripped.
func TestLegacyFirewallIsReal(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "pkg", "characterization", "testdata", "golden", "menu", "firewall_configure_dry_run.golden"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(data), "# ----\n")
	if len(parts) != 3 {
		t.Fatalf("the golden has %d parts, want the header, the screen and the trailer", len(parts))
	}
	var screen []string
	for _, l := range strings.Split(strings.TrimSuffix(parts[1], "\n"), "\n") {
		screen = append(screen, ansi.Strip(strings.ReplaceAll(strings.TrimSuffix(l, "$"), `\x1b`, "\x1b")))
	}
	if strings.Join(screen, "\n") != strings.Join(legacyFirewall, "\n") {
		t.Errorf("the mock's transcript is not pkg/menu's:\n%s\nwant:\n%s", strings.Join(legacyFirewall, "\n"), strings.Join(screen, "\n"))
	}
}

// TestToggleSaves checks the saved toggle's plan, steps.PlanSave's: one
// change, hardn.yml's dryRun line alone, each way; and none where the
// file already says what the toggle sets, as when -n set plan only and
// the toggle turns it off.
func TestToggleSaves(t *testing.T) {
	for _, tc := range []struct{ file, want bool }{{false, true}, {true, false}, {false, false}} {
		_, f, err := toggleFlow(context.Background(), tc.file, tc.want)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := f.Engine.Plan(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ch := plan.View.Changes
		if tc.file == tc.want {
			if len(ch) != 0 {
				t.Errorf("file %t, want %t: %+v; want no change", tc.file, tc.want, ch)
			}
			continue
		}
		var changed []string
		for _, l := range strings.Split(ch[0].Diff, "\n") {
			if (strings.HasPrefix(l, "-") || strings.HasPrefix(l, "+")) && !strings.HasPrefix(l, "---") && !strings.HasPrefix(l, "+++") {
				changed = append(changed, l)
			}
		}
		want := "-dryRun: " + strconv.FormatBool(tc.file) + " +dryRun: " + strconv.FormatBool(tc.want)
		if len(ch) != 1 || ch[0].Title != "Save the configuration" || ch[0].Target != hardnYML || strings.Join(changed, " ") != want {
			t.Errorf("file %t, want %t: %+v; want one save of %s changing the dryRun line alone", tc.file, tc.want, ch, hardnYML)
		}
	}
}

// TestPlayDryRunNotes plays each variant started in dry run (-n) and
// chooses 1, 2 and 5, which lead to Figma's mocks: in origin/main's
// menus, each mock comes after the note of what origin/main does there
// (V2 for 1); in the proposed one, 1 and 5 are refused and 2 is noted.
func TestPlayDryRunNotes(t *testing.T) {
	titles := map[string]string{}
	for _, m := range screen.Mocks() {
		if sm, ok := m.Screen.(screen.SubMenu); ok {
			titles[m.Name] = sm.Title.Title
		}
	}
	flat := func(out string) string { return strings.Join(strings.Fields(ansi.Strip(out)), " ") }
	for _, r := range []struct {
		variant string
		lines   []string
		noted   []string
	}{
		{dryRunPrefix, []string{"1", "0", "2", "0", "5", "0", "0"}, []string{"1", "2", "5"}},
		{dryRunPrefix + "/" + variantSaved, []string{"1", "0", "2", "0", "5", "0", "0"}, []string{"1", "2", "5"}},
		{dryRunPrefix + "/" + variantProposed, []string{"1", "5", "2", "0", "0"}, []string{"2"}},
	} {
		args := append(lookArgs(t, term.Fixed(80, colorprofile.TrueColor, true, true)), "-sequence", "dry-run", r.variant)
		code, out, errs, unread := playGallery(r.lines, args...)
		if code != exitOK || errs != "" || unread != 0 {
			t.Fatalf("%s: exit %d, stderr %q, %d lines unread; want exit 0", r.variant, code, errs, unread)
		}
		text := flat(out)
		for _, key := range r.noted {
			title := titles[links[mainMenuMock+"/"+key]]
			at := strings.Index(text, flat(legacyDryRun[key]))
			if at < 0 || title == "" || !strings.Contains(text[at:], title) {
				t.Errorf("%s, %s: want the note %q, then the mock titled %q:\n%s", r.variant, key, legacyDryRun[key], title, text)
			}
		}
		refused := strings.Count(text, planOnlyRefused)
		if want := map[bool]int{false: 0, true: len(refusedKeys)}[r.variant == dryRunPrefix+"/"+variantProposed]; refused != want {
			t.Errorf("%s: %q said %d times, want %d", r.variant, planOnlyRefused, refused, want)
		}
	}
}

// TestPlayDryRunSavedUnderFlag plays the saved toggle started as hardn -n:
// turning plan only off finds hardn.yml already live and saves nothing,
// and the flag keeps plan only (plan §3.19, decision 4).
func TestPlayDryRunSavedUnderFlag(t *testing.T) {
	args := append(lookArgs(t, term.Fixed(80, colorprofile.TrueColor, true, true)), "-sequence", "dry-run", dryRunPrefix+"/"+variantSaved)
	code, out, errs, unread := playGallery([]string{"6", "1", "0", "0"}, args...)
	if code != exitOK || errs != "" || unread != 0 {
		t.Fatalf("exit %d, stderr %q, %d lines unread; want exit 0", code, errs, unread)
	}
	text := strings.Join(strings.Fields(ansi.Strip(out)), " ")
	last := text[strings.LastIndex(text, "Select an option"):]
	if !strings.Contains(text, "Nothing needs changing.") || !strings.Contains(text, flagHolds) || strings.Contains(text, "+dryRun") ||
		!strings.Contains(text, "Nothing was applied.") || !strings.Contains(last, "Dry-run Mode Enabled") {
		t.Errorf("want nothing saved, %q, and the main menu still in dry run:\n%s", flagHolds, text)
	}
}

// TestDryRunUsage checks the mode's usage errors: a mock or a variant
// that is not one, and a sequence play does not take there.
func TestDryRunUsage(t *testing.T) {
	if code, _, errs := runGallery(dryRunPrefix, "nothing"); code != exitUsage || !strings.Contains(errs, `no mock "nothing"`) {
		t.Errorf("listing an unknown mock: exit %d, %q", code, errs)
	}
	if code, out, _ := runGallery(dryRunPrefix, "-list"); code != exitOK || strings.Count(out, "\n") != len(dryMocks()) {
		t.Errorf("-list: exit %d, %q", code, out)
	}
	for _, args := range [][]string{{"dryrun/nothing"}, {"-sequence", "failure", "dryrun"}} {
		if code, _, errs, _ := playGallery(nil, args...); code != exitUsage || errs == "" {
			t.Errorf("play %v: exit %d, %q; want a usage error", args, code, errs)
		}
	}
}
