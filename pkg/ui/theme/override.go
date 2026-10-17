package theme

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/term"
)

// TokenOverride is a new value for one token of one palette. A zero field
// keeps the palette's value: an empty Hex, a nil ANSI256 or a nil ANSI.
type TokenOverride struct {
	// Hex is the truecolor value, "#RRGGBB".
	Hex string
	// ANSI256 is an xterm-256 index, 0 to 255. The palettes use 16 to 255
	// only (palette.go); an override may try any.
	ANSI256 *int
	// ANSI is one of the 16 terminal colours, 0 to 15.
	ANSI *int
}

// Overrides maps token names, as the design-system docs and the palette's
// token tags name them ("text.muted", "status.ok", ...), to new values for
// one palette.
type Overrides map[string]TokenOverride

// hexColour is the one form of Hex the palettes use.
var hexColour = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// NewWithOverrides returns the theme New returns for env, built from a copy
// of the palette env.Dark selects with o's values in place of its own. It
// returns an error, and no theme, for a token name the palette does not
// have or a value out of range. The palettes themselves never change.
//
// It is the dev-only gallery's colour hook (internal/tools/uigallery):
// colour iteration without editing palette.go. Production code builds its
// theme with New and must never call it, nor name Overrides or
// TokenOverride; TestOverrideReach fails if any Go file outside
// pkg/ui/theme and internal/tools/uigallery does.
func NewWithOverrides(env term.Env, o Overrides) (*Theme, error) {
	p := lightPalette
	if env.Dark {
		p = darkPalette
	}
	tokens := map[string]*token{}
	var names []string
	v := reflect.ValueOf(&p).Elem()
	for i := range v.NumField() {
		f := v.Type().Field(i)
		t, ok := v.Field(i).Addr().Interface().(*token)
		if !ok {
			return nil, fmt.Errorf("palette.%s is not a token", f.Name)
		}
		name := f.Tag.Get("token")
		tokens[name] = t
		names = append(names, name)
	}
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(o)) {
		t, ok := tokens[name]
		if !ok {
			errs = append(errs, fmt.Errorf("unknown token %q (the tokens are %s)", name, strings.Join(names, ", ")))
			continue
		}
		if err := o[name].apply(t); err != nil {
			errs = append(errs, fmt.Errorf("token %s: %w", name, err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return build(env, &p), nil
}

// apply sets t's values from o, after checking them all.
func (o TokenOverride) apply(t *token) error {
	var errs []error
	if o.Hex != "" && !hexColour.MatchString(o.Hex) {
		errs = append(errs, fmt.Errorf("hex %q is not #RRGGBB", o.Hex))
	}
	if o.ANSI256 != nil && (*o.ANSI256 < 0 || *o.ANSI256 > 255) {
		errs = append(errs, fmt.Errorf("256-colour index %d is not 0 to 255", *o.ANSI256))
	}
	if o.ANSI != nil && (*o.ANSI < 0 || *o.ANSI > 15) {
		errs = append(errs, fmt.Errorf("16-colour index %d is not 0 to 15", *o.ANSI))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if o.Hex != "" {
		t.hex = o.Hex
	}
	if o.ANSI256 != nil {
		t.ansi256 = ansi.IndexedColor(*o.ANSI256)
	}
	if o.ANSI != nil {
		t.ansi = ansi.BasicColor(*o.ANSI)
	}
	return nil
}
