package fixture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/component/fixture"
)

// stateName is a golden name: a component's type name in lower case, a
// slash and the state's name.
var stateName = regexp.MustCompile(`^([a-z]+)/([a-z0-9]+(?:-[a-z0-9]+)*)$`)

// typeName returns the component type of a state's view-model, looking
// through fixture.Animated.
func typeName(vm fixture.Renderer) string {
	if a, ok := vm.(fixture.Animated); ok {
		vm = a.VM
	}
	return reflect.TypeOf(vm).Name()
}

// TestStates checks the catalogue's names: unique, of the form
// <component>/<state> with the view-model's type name in lower case as
// the component, and each component's states together, as the gallery
// lists them.
func TestStates(t *testing.T) {
	seen := map[string]bool{}
	var order []string
	for _, s := range fixture.States() {
		if s.VM == nil {
			t.Errorf("%s: no view-model", s.Name)
			continue
		}
		m := stateName.FindStringSubmatch(s.Name)
		if m == nil {
			t.Errorf("%q is not <component>/<state> in lower case", s.Name)
			continue
		}
		if want := strings.ToLower(typeName(s.VM)); m[1] != want {
			t.Errorf("%s: the view-model is a %s, so the name should start %s/", s.Name, typeName(s.VM), want)
		}
		if seen[s.Name] {
			t.Errorf("%s: listed twice", s.Name)
		}
		seen[s.Name] = true
		if len(order) == 0 || order[len(order)-1] != m[1] {
			if slices.Contains(order, m[1]) {
				t.Errorf("%s: %s's states are not together", s.Name, m[1])
			}
			order = append(order, m[1])
		}
	}
}

// TestEveryComponent checks that every component of pkg/ui/component, a
// type with a value method Render(Ctx) string, has at least one state.
func TestEveryComponent(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var components []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			if name, ok := renderMethod(d); ok {
				components = append(components, name)
			}
		}
	}
	if len(components) == 0 {
		t.Fatal("found no components in pkg/ui/component")
	}
	has := map[string]bool{}
	for _, s := range fixture.States() {
		has[typeName(s.VM)] = true
	}
	for _, c := range components {
		if !has[c] {
			t.Errorf("component %s has no state in fixture.States", c)
		}
	}
}

// renderMethod reports whether d is a component's method
// Render(c Ctx) string with a value receiver, and returns the receiver's
// type name.
func renderMethod(d ast.Decl) (string, bool) {
	fn, ok := d.(*ast.FuncDecl)
	if !ok || fn.Recv == nil || fn.Name.Name != "Render" || len(fn.Recv.List) != 1 {
		return "", false
	}
	recv, ok := fn.Recv.List[0].Type.(*ast.Ident)
	if !ok || !ast.IsExported(recv.Name) {
		return "", false
	}
	params, results := fn.Type.Params.List, fn.Type.Results
	if len(params) != 1 || len(params[0].Names) > 1 || results == nil || len(results.List) != 1 {
		return "", false
	}
	if p, ok := params[0].Type.(*ast.Ident); !ok || p.Name != "Ctx" {
		return "", false
	}
	if r, ok := results.List[0].Type.(*ast.Ident); !ok || r.Name != "string" {
		return "", false
	}
	return recv.Name, true
}

// TestFresh checks that each call returns a value of its own: changing a
// fixture's slice changes no later call's.
func TestFresh(t *testing.T) {
	m := fixture.FigmaMenu()
	m.Items[0].Label = "changed"
	s := fixture.States()
	s[0].Name = "changed"
	if fixture.FigmaMenu().Items[0].Label == "changed" || fixture.States()[0].Name == "changed" {
		t.Error("a change to one call's value shows in the next call's")
	}
}
