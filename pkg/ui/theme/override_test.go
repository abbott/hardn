package theme

import (
	"fmt"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/term"
)

// renders returns every slot of th rendered on the same text, by path.
func renders(th *Theme) map[string]string {
	out := map[string]string{}
	for path, s := range slots(th) {
		out[path] = s.Render("x")
	}
	return out
}

// TestNoOverrides checks that NewWithOverrides with no overrides builds
// exactly New's theme, in every environment.
func TestNoOverrides(t *testing.T) {
	for _, env := range envs() {
		for _, o := range []Overrides{nil, {}, {"text.muted": {}}} {
			th, err := NewWithOverrides(env, o)
			if err != nil {
				t.Fatalf("%s, %v: %v", envName(env), o, err)
			}
			if got, want := renders(th), renders(New(env)); !maps.Equal(got, want) || th.Glyphs != New(env).Glyphs {
				t.Errorf("%s, %v: the theme differs from New's", envName(env), o)
			}
		}
	}
}

// TestOverrides checks that each value of an override reaches the slots
// of its token at its own profile, that a value left out keeps the
// palette's, that other tokens keep theirs, that the override applies to
// the palette the Env selects, and that the palettes do not change.
func TestOverrides(t *testing.T) {
	i256, i16 := 200, 5
	o := Overrides{"text.muted": {Hex: "#123456", ANSI256: &i256, ANSI: &i16}}
	hexOnly := Overrides{"text.muted": {Hex: "#123456"}}
	fg := func(c ansi.Color) string { return lipgloss.NewStyle().Foreground(c).Render("x") }
	tests := []struct {
		profile colorprofile.Profile
		o       Overrides
		want    string // Text.Muted, or "" for New's
	}{
		{colorprofile.TrueColor, o, fg(lipgloss.Color("#123456"))},
		{colorprofile.ANSI256, o, fg(ansi.IndexedColor(200))},
		{colorprofile.ANSI, o, fg(ansi.BasicColor(5))},
		{colorprofile.ASCII, o, ""},
		{colorprofile.ANSI256, hexOnly, ""},
		{colorprofile.ANSI, hexOnly, ""},
	}
	for _, dark := range []bool{true, false} {
		for _, tt := range tests {
			env := term.Fixed(80, tt.profile, true, dark)
			th, err := NewWithOverrides(env, tt.o)
			if err != nil {
				t.Fatal(err)
			}
			got, base := renders(th), renders(New(env))
			want := tt.want
			if want == "" {
				want = base["Text.Muted"]
			}
			if got["Text.Muted"] != want {
				t.Errorf("%s, %v: Text.Muted renders %q, want %q", envName(env), tt.o, got["Text.Muted"], want)
			}
			if got["Text.Primary"] != base["Text.Primary"] {
				t.Errorf("%s: Text.Primary changed with text.muted", envName(env))
			}
		}
	}
	if darkPalette.TextMuted.hex != "#81869C" || lightPalette.TextMuted.hex != "#575C6E" {
		t.Errorf("an override changed the palettes: dark %s, light %s", darkPalette.TextMuted.hex, lightPalette.TextMuted.hex)
	}
}

// TestOverrideErrors checks that an unknown token and each value out of
// range is an error that names it, and that no theme comes with it.
func TestOverrideErrors(t *testing.T) {
	neg, big, sixteen := -1, 256, 16
	tests := []struct {
		o    Overrides
		want []string
	}{
		{Overrides{"text.mute": {Hex: "#123456"}}, []string{`unknown token "text.mute"`, "text.muted"}},
		{Overrides{"divider": {Hex: "123456"}}, []string{"token divider", `hex "123456"`}},
		{Overrides{"divider": {Hex: "#FFF"}}, []string{`hex "#FFF"`}},
		{Overrides{"divider": {Hex: "#12345G"}}, []string{`hex "#12345G"`}},
		{Overrides{"divider": {ANSI256: &neg}}, []string{"256-colour index -1"}},
		{Overrides{"divider": {ANSI256: &big}}, []string{"256-colour index 256"}},
		{Overrides{"divider": {ANSI: &sixteen}}, []string{"16-colour index 16"}},
		{Overrides{"divider": {Hex: "x", ANSI: &neg}, "nope": {}}, []string{`hex "x"`, "16-colour index -1", `unknown token "nope"`}},
	}
	for _, tt := range tests {
		th, err := NewWithOverrides(term.Fixed(80, colorprofile.TrueColor, true, true), tt.o)
		if err == nil || th != nil {
			t.Errorf("%v: got a theme and error %v, want only an error", tt.o, err)
			continue
		}
		for _, w := range tt.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%v: error %q does not say %q", tt.o, err, w)
			}
		}
	}
}

// themePath is this package's import path.
const themePath = "github.com/abbott/hardn/pkg/ui/theme"

// overrideAPI is the gallery's colour hook, which only pkg/ui/theme and
// internal/tools/uigallery may name.
var overrideAPI = []string{"NewWithOverrides", "Overrides", "TokenOverride"}

// overrideUses returns where the Go source src (or the file filename, when
// src is nil) names overrideAPI through an import of this package: as
// theme.X, as X under another import name, or as any identifier X when
// the package is dot-imported.
func overrideUses(filename string, src any) ([]string, error) {
	fset := gotoken.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	local := ""
	for _, imp := range f.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err == nil && path == themePath {
			local = "theme"
			if imp.Name != nil {
				local = imp.Name.Name
			}
		}
	}
	if local == "" || local == "_" {
		return nil, nil
	}
	var uses []string
	ast.Inspect(f, func(n ast.Node) bool {
		var name string
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if x, ok := n.X.(*ast.Ident); ok && x.Name == local {
				name = n.Sel.Name
			}
		case *ast.Ident:
			if local == "." {
				name = n.Name
			}
		}
		if slices.Contains(overrideAPI, name) {
			uses = append(uses, fmt.Sprintf("%s names %s", fset.Position(n.Pos()), name))
		}
		return true
	})
	return uses, nil
}

// TestOverrideUses checks that overrideUses finds the hook under each
// form of import, and nothing else.
func TestOverrideUses(t *testing.T) {
	tests := []struct {
		src  string
		uses int
	}{
		{`package p; import "` + themePath + `"; var _, _ = theme.NewWithOverrides(term.Env{}, nil)`, 1},
		{`package p; import th "` + themePath + `"; var o th.Overrides; var _ = th.TokenOverride{}`, 2},
		{`package p; import . "` + themePath + `"; var _, _ = NewWithOverrides(term.Env{}, Overrides{})`, 2},
		{`package p; import "` + themePath + `"; var _ = theme.New(term.Env{})`, 0},
		{`package p; import _ "` + themePath + `"; var NewWithOverrides = 1`, 0},
		{`package p; var theme struct{ NewWithOverrides int }; var _ = theme.NewWithOverrides`, 0},
	}
	for _, tt := range tests {
		uses, err := overrideUses("src.go", tt.src)
		if err != nil {
			t.Fatal(err)
		}
		if len(uses) != tt.uses {
			t.Errorf("%s: found %d uses %v, want %d", tt.src, len(uses), uses, tt.uses)
		}
	}
}

// hookAllowed are the module paths that may name the colour hook: the
// package that defines it and the gallery.
var hookAllowed = []string{"pkg/ui/theme/", "internal/tools/uigallery/"}

// strayUses walks the Go files of the module at root, test files
// included, and returns every use of the colour hook outside hookAllowed,
// and the files it scanned, as slash paths relative to root. It skips
// vendor, testdata and hidden directories, which the go command ignores
// or which hold no module code.
func strayUses(root string) (uses, scanned []string, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || slices.ContainsFunc(hookAllowed, func(a string) bool { return strings.HasPrefix(rel, a) }) {
			return nil
		}
		scanned = append(scanned, rel)
		found, err := overrideUses(path, nil)
		uses = append(uses, found...)
		return err
	})
	return uses, scanned, err
}

// TestOverrideReach enforces that the colour hook is the gallery's alone:
// no Go file in the module outside pkg/ui/theme and
// internal/tools/uigallery names NewWithOverrides, Overrides or
// TokenOverride, so no production code can apply an override, and the
// linker leaves the hook out of every binary but the gallery's.
func TestOverrideReach(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	if b, err := os.ReadFile(filepath.Join(root, "go.mod")); err != nil || !strings.Contains(string(b), "module github.com/abbott/hardn\n") {
		t.Fatalf("%s is not the module root: %v", root, err)
	}
	uses, scanned, err := strayUses(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range uses {
		t.Errorf("%s: only the gallery may use the colour hook", u)
	}
	if !slices.Contains(scanned, "cmd/hardn/main.go") {
		t.Errorf("the walk did not reach cmd/hardn/main.go; it scanned %d files", len(scanned))
	}
}

// TestStrayUses checks the walk on a module made up for the test: it
// reports a use in production code and in a test file, and none in the
// allowed packages, vendor or testdata.
func TestStrayUses(t *testing.T) {
	root := t.TempDir()
	use := "package p\n\nimport \"" + themePath + "\"\n\nvar _, _ = theme.NewWithOverrides(term.Env{}, nil)\n"
	files := map[string]string{
		"go.mod":                               "module example.net/m\n",
		"cmd/tool/main.go":                     use,
		"pkg/feature/feature_test.go":          use,
		"pkg/feature/feature.go":               "package feature\n",
		"pkg/ui/theme/override.go":             use,
		"internal/tools/uigallery/override.go": use,
		"vendor/example.net/v/v.go":            use,
		"pkg/feature/testdata/t.go":            use,
	}
	for name, src := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	uses, scanned, err := strayUses(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cmd/tool/main.go", "pkg/feature/feature.go", "pkg/feature/feature_test.go"}; !slices.Equal(scanned, want) {
		t.Errorf("scanned %v, want %v", scanned, want)
	}
	if len(uses) != 2 || !strings.Contains(uses[0], "main.go") || !strings.Contains(uses[1], "feature_test.go") {
		t.Errorf("found %v, want the uses in cmd/tool/main.go and pkg/feature/feature_test.go", uses)
	}
}
