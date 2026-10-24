package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestDryRunGolden pins each mock of the listing on the core set: the
// gallery's header, the mock's note, then its drawing.
func TestDryRunGolden(t *testing.T) {
	for _, m := range dryMocks() {
		uitest.Golden(t, dryRunPrefix+"/"+m.name, uitest.Core(), func(env term.Env) string {
			args := append(append([]string{dryRunPrefix}, lookArgs(t, env)...), m.name)
			code, out, errs := runGallery(args...)
			if code != exitOK || errs != "" {
				t.Errorf("%v: exit %d, stderr %q; want exit 0", args, code, errs)
			}
			return out
		})
	}
}

// TestPlayDryRunGolden plays the mocks on the core set and pins each
// transcript: hardn's main menu on its layout, 6 turning dry run on for
// the session ("Mode Plan only"), then DNS Add stopping at its plan with
// the footer, then 4, origin/main's Firewall screen under dry run, then
// 1, the note of what origin/main does there (V2) above Figma's mock,
// then out; and the toggle saved through the canonical flow.
func TestPlayDryRunGolden(t *testing.T) {
	for _, r := range []struct {
		name  string
		args  []string
		lines []string
	}{
		{"dryrun", []string{dryRunPrefix}, []string{"6", "1", "0", "3", "2", "192.0.2.53", "0", "4", "1", "0", "0"}},
		{"dryrun-saved", []string{dryRunPrefix + "/" + variantSaved}, []string{"6", "1", "y", "0", "0"}},
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
// chooses 1, 2 and 5, which lead to Figma's mocks: each comes after the
// note of what origin/main does there (V2 for 1); the main menu says
// "Mode Plan only (-n)".
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
		if !strings.Contains(text, "Select an option Mode Plan only (-n)") {
			t.Errorf("%s: the main menu does not say plan only from -n:\n%s", r.variant, text)
		}
	}
}

// TestPlayDryRunSavedUnderFlag plays the saved toggle started as hardn -n:
// turning plan only off finds hardn.yml already live and saves nothing,
// and the flag keeps plan only (plan §3.19, decision 4), the main menu
// still naming it, "Mode Plan only (-n)".
func TestPlayDryRunSavedUnderFlag(t *testing.T) {
	args := append(lookArgs(t, term.Fixed(80, colorprofile.TrueColor, true, true)), "-sequence", "dry-run", dryRunPrefix+"/"+variantSaved)
	code, out, errs, unread := playGallery([]string{"6", "1", "0", "0"}, args...)
	if code != exitOK || errs != "" || unread != 0 {
		t.Fatalf("exit %d, stderr %q, %d lines unread; want exit 0", code, errs, unread)
	}
	text := strings.Join(strings.Fields(ansi.Strip(out)), " ")
	last := text[strings.LastIndex(text, "Select an option"):]
	if !strings.Contains(text, "Nothing needs changing.") || !strings.Contains(text, flagHolds) || strings.Contains(text, "+dryRun") ||
		!strings.Contains(text, "Nothing was applied.") || !strings.HasPrefix(last, "Select an option Mode Plan only (-n)") {
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
